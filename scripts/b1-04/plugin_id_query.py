#!/usr/bin/env python3
"""One bounded `claude plugin list --json` for two plugin names (docs/cli-poc.md 46).

Usage: python3 -I scripts/b1-04/plugin_id_query.py --cli /absolute/path/to/claude
(operators run it through plugin-id-query.sh, which keeps the evidence).

Starts the CLI at most once, never retries, and prints only a fixed report:
for each candidate record its full id, origin, scope, enabled and error count;
then the match count and fixed status lines. The CLI's raw stdout stays in
memory and its stderr goes to /dev/null. The CLI gets the environment that
internal/buyer CLIEnv builds (allowlist, non-empty values only, three fixed
values) plus an empty plugin root of this run, and runs in an empty working
directory. Exit codes are listed in EXIT below.
"""
import json
import os
import re
import select
import signal
import stat
import subprocess
import sys
import tempfile
import time

CANDIDATES = ("cc-plugin-agents-md", "cc-plugin-plugin-authoring")
SCOPES = ("user", "project", "local", "managed", "session", "synced")
# The origin after "@": a marketplace name or a reserved word. No path,
# space or control character; anything else is refused, not repaired.
ORIGIN = re.compile(r"[A-Za-z0-9][A-Za-z0-9._-]{0,99}\Z")
MAX_ID = 200

TIMEOUT = 30.0       # seconds from the CLI's start until its output must be complete
GRACE = 3.0          # after SIGTERM, before SIGKILL
MAX_OUTPUT = 1 << 20  # bytes of stdout read; more stops the CLI
TICK = 0.1           # how often the read loop looks for a recorded signal

# internal/buyer/host.go envAllowlist and fixedEnv, in the same order.
ALLOWLIST = ("HOME", "USER", "LOGNAME", "PATH", "TMPDIR", "LANG", "LC_ALL", "TERM")
FIXED = (("DISABLE_AUTOUPDATER", "1"), ("CLAUDE_CODE_DISABLE_NONESSENTIAL_TRAFFIC", "1"),
         ("CLAUDE_AGENT_SDK_DISABLE_BUILTIN_AGENTS", "1"))
PLUGIN_ROOT = "CLAUDE_CODE_PLUGIN_CACHE_DIR"
PREFIX = "apa-plist."
DIR_FLAGS = os.O_RDONLY | os.O_DIRECTORY | os.O_NOFOLLOW

EXIT = {
    "ok": 0, "precheck_failed": 2, "no_match": 3, "cli_failed": 4, "timeout": 5,
    "output_overflow": 6, "parse_error": 7, "format_rejected": 8, "cleanup_failed": 9,
    "interrupted": 10, "report_failed": 11, "internal_error": 12,
}

# A signal only records itself; the control flow below finishes every step
# it has started (stop, wait, reap, close, remove), so a second signal
# cannot skip any of them, and no deadline is ever reset.
SIGNALS = []


def on_signal(signum, frame):
    SIGNALS.append(signum)


def build_env(parent, root):
    env = {}
    for k in ALLOWLIST:
        v = parent.get(k, "")
        if v:
            env[k] = v
    env.update(FIXED)
    env[PLUGIN_ROOT] = root
    return env


def no_duplicates(pairs):
    keys = [k for k, _ in pairs]
    if len(set(keys)) != len(keys):
        raise ValueError("duplicate member")
    return dict(pairs)


def no_constant(name):
    raise ValueError("non-standard constant")


def judge(raw):
    """Return (result, lines). Lines are printed only when result is ok or no_match."""
    try:
        rows = json.loads(raw.decode("utf-8"), object_pairs_hook=no_duplicates, parse_constant=no_constant)
    except Exception:
        return "parse_error", ["reason=not_json"]
    if not isinstance(rows, list):
        return "parse_error", ["reason=not_a_list"]
    out = []
    for r in rows:
        # Every record must be an object with a string id; otherwise its
        # candidacy cannot be decided and the whole output is refused.
        if not isinstance(r, dict) or not isinstance(r.get("id"), str):
            return "parse_error", ["reason=record_shape"]
        pid = r["id"]
        name, _, origin = pid.partition("@")
        if name not in CANDIDATES:
            continue  # not a candidate: skipped whole, never printed
        if len(pid) > MAX_ID or not ORIGIN.match(origin):
            return "format_rejected", ["reject=id"]
        scope, enabled = r.get("scope"), r.get("enabled")
        if not isinstance(scope, str) or scope not in SCOPES:
            return "format_rejected", ["reject=scope"]
        if type(enabled) is not bool:
            return "format_rejected", ["reject=enabled"]
        if "errors" in r:
            errs = r["errors"]
            if not isinstance(errs, list) or not all(isinstance(e, str) for e in errs):
                return "format_rejected", ["reject=errors"]
            nerr = len(errs)
        else:
            nerr = 0
        out.append("candidate " + json.dumps(
            {"id": pid, "origin": origin, "scope": scope, "enabled": enabled, "errors": nerr},
            separators=(",", ":")))
    matched = len(out)
    out.append("matched=%d entries=%d" % (matched, len(rows)))
    return ("ok" if matched else "no_match"), out


def kill_group(pgid, sig):
    try:
        os.killpg(pgid, sig)
    except (ProcessLookupError, PermissionError):
        pass


def run_cli(cli, env, work):
    """Start the CLI once. Return (reason or None, stdout, status, process state)."""
    try:
        proc = subprocess.Popen([cli, "plugin", "list", "--json"], cwd=work, env=env,
                                stdin=subprocess.DEVNULL, stdout=subprocess.PIPE,
                                stderr=subprocess.DEVNULL, start_new_session=True)
    except OSError:
        return "cli_failed", b"", "start_failed", "not_started"
    pgid = proc.pid
    deadline = time.monotonic() + TIMEOUT
    buf = bytearray()
    reason = None
    fd = proc.stdout.fileno()
    while True:
        if SIGNALS:
            reason = "interrupted"
            break
        left = deadline - time.monotonic()
        if left <= 0:
            reason = "timeout"
            break
        ready, _, _ = select.select([fd], [], [], min(left, TICK))
        if not ready:
            continue
        chunk = os.read(fd, 65536)
        if not chunk:
            break
        buf += chunk
        if len(buf) > MAX_OUTPUT:
            reason = "output_overflow"
            break
    while reason is None and proc.poll() is None:  # stdout closed; wait for the exit
        if SIGNALS:
            reason = "interrupted"
        elif time.monotonic() >= deadline:
            reason = "timeout"
        else:
            time.sleep(TICK)
    if proc.poll() is None:
        # Bounded stop: SIGTERM, at most GRACE, then SIGKILL. A signal that
        # arrives meanwhile is only recorded; it neither cuts nor extends this.
        kill_group(pgid, signal.SIGTERM)
        end = time.monotonic() + GRACE
        while proc.poll() is None and time.monotonic() < end:
            time.sleep(TICK)
        if proc.poll() is None:
            kill_group(pgid, signal.SIGKILL)
            proc.wait()
    # ponytail: one SIGKILL to the group for children left behind; the pgid
    # stays ours while any member lives (POSIX does not reuse it). Processes
    # that left the group are out of reach of this.
    kill_group(pgid, signal.SIGKILL)
    proc.stdout.close()
    rc = proc.returncode
    status = "signal_%d" % -rc if rc < 0 else str(rc)
    return reason, bytes(buf), status, "reaped"


class Scratch:
    """This run's mkdtemp directory, held by file descriptors from creation:
    pfd for its parent, fd for the directory itself, ident its (dev, ino)."""

    def __init__(self):
        parent = tempfile.gettempdir()
        self.pfd = os.open(parent, os.O_RDONLY | os.O_DIRECTORY)
        try:
            path = tempfile.mkdtemp(prefix=PREFIX, dir=parent)
            self.name = os.path.basename(path)
            self.fd = os.open(self.name, DIR_FLAGS, dir_fd=self.pfd)
        except OSError:
            os.close(self.pfd)
            raise
        st = os.fstat(self.fd)
        self.ident = (st.st_dev, st.st_ino)
        self.path = os.path.join(parent, self.name)

    def is_ours(self):
        """The name in the parent is a real directory, and it is ours."""
        st = os.stat(self.name, dir_fd=self.pfd, follow_symlinks=False)
        return stat.S_ISDIR(st.st_mode) and (st.st_dev, st.st_ino) == self.ident


def clear(fd):
    """Remove everything inside the directory open as fd, through fds only:
    no path is resolved again, symlinks are removed, never followed."""
    for e in list(os.scandir(fd)):
        if e.is_dir(follow_symlinks=False):
            sub = os.open(e.name, DIR_FLAGS, dir_fd=fd)
            try:
                clear(sub)
            finally:
                os.close(sub)
            os.rmdir(e.name, dir_fd=fd)
        else:
            os.unlink(e.name, dir_fd=fd)


def remove_own(sc):
    """Remove this run's directory, or nothing. The contents go through the
    directory's own fd, so a renamed or swapped path is never followed. The
    one step by name, the final non-recursive rmdir, runs only after the name
    is checked again to be this directory; rmdir removes only an empty one.
    Refused or failed: False, and whatever is left stays."""
    try:
        if not sc.is_ours():
            return False
        clear(sc.fd)
        if not sc.is_ours():
            return False
        os.rmdir(sc.name, dir_fd=sc.pfd)
        return True
    except OSError:
        return False
    finally:
        os.close(sc.fd)
        os.close(sc.pfd)


def main(argv):
    signal.signal(signal.SIGTERM, on_signal)
    signal.signal(signal.SIGINT, on_signal)
    report = ["query=plugin_list v2"]
    result, cli_state, proc_state, dir_state, sc = "precheck_failed", "not_started", "not_started", "none", None
    try:
        if len(argv) != 2 or argv[0] != "--cli" or not os.path.isabs(argv[1]) \
                or not os.path.isfile(argv[1]) or not os.access(argv[1], os.X_OK):
            report.append("reason=usage_or_cli")
        else:
            try:
                sc = Scratch()
                root, work = os.path.join(sc.path, "plugins"), os.path.join(sc.path, "work")
                os.mkdir(root, 0o700)
                os.mkdir(work, 0o700)
            except OSError:
                report.append("reason=scratch")
            else:
                if SIGNALS:
                    result = "interrupted"  # before the start: nothing was started
                else:
                    env = build_env(os.environ, root)
                    cli_state, proc_state = "started", "unknown"
                    reason, raw, status, proc_state = run_cli(argv[1], env, work)
                    cli_state = "start_failed" if status == "start_failed" else "exit_" + status
                    if reason:
                        result = reason
                    elif status != "0":
                        result = "cli_failed"  # valid JSON does not hide a failed CLI
                    else:
                        result, lines = judge(raw)
                        report += lines if result in ("ok", "no_match") else lines[:1]
                    del raw
    except Exception:
        result = "internal_error"  # a fixed code, never a traceback
    finally:
        if sc is not None:
            dir_state = "ok" if remove_own(sc) else "failed"
    if SIGNALS and result in ("ok", "no_match"):
        result = "interrupted"  # a signal during the run: not reported as a clean result
    if result == "ok" and dir_state == "failed":
        result = "cleanup_failed"
    report += ["result=" + result, "cli=" + cli_state, "cli_proc=" + proc_state,
               "dir_cleanup=" + dir_state, "signals=%d" % len(SIGNALS)]
    try:
        sys.stdout.write("\n".join(report) + "\n")
        sys.stdout.flush()
    except OSError:
        return EXIT["report_failed"]
    return EXIT[result]


if __name__ == "__main__":
    sys.exit(main(sys.argv[1:]))
