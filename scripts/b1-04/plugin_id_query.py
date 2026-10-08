#!/usr/bin/env python3
"""One bounded `claude plugin list --json` for two plugin names (docs/cli-poc.md 46).

Usage: python3 -I scripts/b1-04/plugin_id_query.py --cli /absolute/path/to/claude

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
import shutil
import signal
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

# internal/buyer/host.go envAllowlist and fixedEnv, in the same order.
ALLOWLIST = ("HOME", "USER", "LOGNAME", "PATH", "TMPDIR", "LANG", "LC_ALL", "TERM")
FIXED = (("DISABLE_AUTOUPDATER", "1"), ("CLAUDE_CODE_DISABLE_NONESSENTIAL_TRAFFIC", "1"),
         ("CLAUDE_AGENT_SDK_DISABLE_BUILTIN_AGENTS", "1"))
PLUGIN_ROOT = "CLAUDE_CODE_PLUGIN_CACHE_DIR"
PREFIX = "apa-plist."

EXIT = {
    "ok": 0, "precheck_failed": 2, "no_match": 3, "cli_failed": 4, "timeout": 5,
    "output_overflow": 6, "parse_error": 7, "format_rejected": 8, "cleanup_failed": 9,
    "interrupted": 10, "report_failed": 11, "internal_error": 12,
}


class Interrupted(Exception):
    pass


def on_signal(signum, frame):
    raise Interrupted()


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
    """Start the CLI once. Return (reason or None, stdout bytes, exit status text)."""
    try:
        proc = subprocess.Popen([cli, "plugin", "list", "--json"], cwd=work, env=env,
                                stdin=subprocess.DEVNULL, stdout=subprocess.PIPE,
                                stderr=subprocess.DEVNULL, start_new_session=True)
    except OSError:
        return "cli_failed", b"", "start_failed"
    pgid = proc.pid
    deadline = time.monotonic() + TIMEOUT
    buf = bytearray()
    reason = None
    try:
        fd = proc.stdout.fileno()
        while True:
            left = deadline - time.monotonic()
            if left <= 0:
                reason = "timeout"
                break
            ready, _, _ = select.select([fd], [], [], left)
            if not ready:
                continue
            chunk = os.read(fd, 65536)
            if not chunk:
                break
            buf += chunk
            if len(buf) > MAX_OUTPUT:
                reason = "output_overflow"
                break
        if reason is None:
            try:
                proc.wait(timeout=max(deadline - time.monotonic(), 0.001))
            except subprocess.TimeoutExpired:
                reason = "timeout"
    except Interrupted:
        reason = "interrupted"
    finally:
        if proc.poll() is None:
            kill_group(pgid, signal.SIGTERM)
            try:
                proc.wait(timeout=GRACE)
            except subprocess.TimeoutExpired:
                kill_group(pgid, signal.SIGKILL)
                proc.wait()
        # ponytail: one SIGKILL to the group for children left behind; the
        # pgid stays ours while any member lives (POSIX does not reuse it).
        kill_group(pgid, signal.SIGKILL)
        proc.stdout.close()
    rc = proc.returncode
    status = "signal_%d" % -rc if rc < 0 else str(rc)
    return reason, bytes(buf), status


def remove_own(tmp):
    """Remove tmp only if it is this run's mkdtemp directory."""
    real = os.path.realpath(tmp)
    if (os.path.dirname(real) != os.path.realpath(tempfile.gettempdir())
            or not os.path.basename(real).startswith(PREFIX) or not os.path.isdir(real)):
        return False
    try:
        shutil.rmtree(real)
    except OSError:
        return False
    return not os.path.exists(real)


def main(argv):
    signal.signal(signal.SIGTERM, on_signal)
    signal.signal(signal.SIGINT, on_signal)
    report = ["query=plugin_list v1"]
    result, cli_state, cleanup, tmp = "precheck_failed", "not_started", "none", None
    try:
        if len(argv) != 2 or argv[0] != "--cli" or not os.path.isabs(argv[1]) \
                or not os.path.isfile(argv[1]) or not os.access(argv[1], os.X_OK):
            report.append("reason=usage_or_cli")
        else:
            try:
                tmp = tempfile.mkdtemp(prefix=PREFIX)
                root, work = os.path.join(tmp, "plugins"), os.path.join(tmp, "work")
                os.mkdir(root, 0o700)
                os.mkdir(work, 0o700)
            except OSError:
                report.append("reason=scratch")
            else:
                env = build_env(os.environ, root)
                cli_state = "started"
                reason, raw, status = run_cli(argv[1], env, work)
                cli_state = "start_failed" if status == "start_failed" else "exit_" + status
                if reason:
                    result = reason
                elif status != "0":
                    result = "cli_failed"  # valid JSON does not hide a failed CLI
                else:
                    result, lines = judge(raw)
                    report += lines if result in ("ok", "no_match") else lines[:1]
                del raw
    except Interrupted:
        result = "interrupted"
    except Exception:
        result = "internal_error"  # a fixed code, never a traceback
    finally:
        if tmp is not None:
            cleanup = "ok" if remove_own(tmp) else "failed"
    if result == "ok" and cleanup == "failed":
        result = "cleanup_failed"
    report += ["result=" + result, "cli=" + cli_state, "cleanup=" + cleanup]
    try:
        sys.stdout.write("\n".join(report) + "\n")
        sys.stdout.flush()
    except OSError:
        return EXIT["report_failed"]
    return EXIT[result]


if __name__ == "__main__":
    sys.exit(main(sys.argv[1:]))
