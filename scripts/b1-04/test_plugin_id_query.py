"""Fake-CLI tests of plugin_id_query.py (docs/cli-poc.md 46).

Every case runs the real file: the driver loads it as a module, may lower
TIMEOUT, GRACE or MAX_OUTPUT so a case ends quickly, and calls its main();
one case runs the file itself from /bin/bash. The CLI is always a fake shell
script in a temporary directory; no case uses the real Claude path, a real
plugin list, or the caller's HOME.

Run: python3 -I -m unittest discover -s scripts/b1-04 -p 'test_*.py'
"""
import json
import os
import shutil
import signal
import stat
import subprocess
import sys
import tempfile
import time
import unittest

HERE = os.path.dirname(os.path.abspath(__file__))
SCRIPT = os.path.join(HERE, "plugin_id_query.py")
WRAPPER = os.path.join(HERE, "plugin-id-query.sh")
DRIVER = (
    "import importlib.util,json,sys\n"
    "s=importlib.util.spec_from_file_location('plugin_id_query',sys.argv[1])\n"
    "m=importlib.util.module_from_spec(s);s.loader.exec_module(m)\n"
    "[setattr(m,k,v) for k,v in json.loads(sys.argv[2]).items()]\n"
    "sys.exit(m.main(sys.argv[3:]))\n"
)
# The injection driver: like DRIVER, but it makes select(), os.read() or the
# stop's SIGTERM fail with a synthetic OSError, only once its readiness file
# exists (the fake CLI has started and written). It counts this process's
# open pipes before and after main() into argv[5]: the CLI's stdout is a
# pipe; other descriptors (Python may keep /dev/urandom open once tempfile
# has used it) are not what this checks.
INJECT = (
    "import importlib.util,json,os,signal,sys,time\n"
    "s=importlib.util.spec_from_file_location('plugin_id_query',sys.argv[1])\n"
    "m=importlib.util.module_from_spec(s);s.loader.exec_module(m)\n"
    "[setattr(m,k,v) for k,v in json.loads(sys.argv[2]).items()]\n"
    "mode,ready,fds=sys.argv[3],sys.argv[4],sys.argv[5]\n"
    "def wait_ready():\n"
    "    while not os.path.exists(ready): time.sleep(0.01)\n"
    "class Sel:\n"
    "    @staticmethod\n"
    "    def select(*a):\n"
    "        wait_ready(); raise OSError('MARK-INJECTED-SELECT')\n"
    "class Os:\n"
    "    def __getattr__(self,k): return getattr(os,k)\n"
    "    @staticmethod\n"
    "    def read(fd,n):\n"
    "        if mode!='read': return os.read(fd,n)\n"
    "        wait_ready(); raise OSError('MARK-INJECTED-READ')\n"
    "    @staticmethod\n"
    "    def killpg(pg,sig):\n"
    "        if mode=='killpg' and sig==signal.SIGTERM: raise OSError('MARK-INJECTED-KILLPG')\n"
    "        return os.killpg(pg,sig)\n"
    "if mode=='select': m.select=Sel\n"
    "elif mode in ('read','killpg'): m.os=Os()\n"
    "import stat\n"
    "def pipes():\n"
    "    n=0\n"
    "    for f in os.listdir('/dev/fd'):\n"
    "        try: n+=stat.S_ISFIFO(os.fstat(int(f)).st_mode)\n"
    "        except OSError: pass\n"
    "    return n\n"
    "before=pipes()\n"
    "rc=m.main(sys.argv[6:])\n"
    "open(fds,'w').write('%d %d'%(before,pipes()))\n"
    "sys.exit(rc)\n"
)
SHELL_ADDED = {"PWD", "SHLVL", "_", "OLDPWD"}  # set by sh itself, not passed in


def rec(pid, scope="user", enabled=True, **extra):
    r = {"id": pid, "version": "1.0.0", "scope": scope, "enabled": enabled,
         "installPath": "/Users/someone/.claude/plugins/cache/MARK-PATH"}
    r.update(extra)
    return r


class Case(unittest.TestCase):
    def setUp(self):
        self.d = tempfile.mkdtemp(prefix="pidq-test.")
        self.dirs = getattr(self, "dirs", []) + [self.d]  # loops call setUp again
        self.tmp = os.path.join(self.d, "tmp")  # the script's TMPDIR
        os.mkdir(self.tmp)
        self.bystander = os.path.join(self.tmp, "apa-plist.not-mine")
        os.mkdir(self.bystander)
        self.home = os.path.join(self.d, "home")  # synthetic, never read
        os.mkdir(self.home)

    def tearDown(self):
        for d in self.dirs:
            for root, dirs, _ in os.walk(d):
                for x in dirs:
                    os.chmod(os.path.join(root, x), 0o700)
            shutil.rmtree(d)

    def fake(self, body, stdout=None):
        """A fake CLI: records each call, its args, cwd and env, writes a
        stderr mark, then runs body."""
        if stdout is not None:
            with open(os.path.join(self.d, "out.json"), "wb") as f:
                f.write(stdout if isinstance(stdout, bytes) else json.dumps(stdout).encode())
        p = os.path.join(self.d, "fake-claude")
        with open(p, "w") as f:
            f.write("#!/bin/sh\nD='%s'\n" % self.d
                    + 'echo "$$" >> "$D/calls"\nprintf "%s\\n" "$@" > "$D/args"\npwd > "$D/cwd"\n'
                    + '/usr/bin/env > "$D/env"\necho MARK-STDERR >&2\n' + body + "\n")
        os.chmod(p, 0o755)
        return p

    def emit(self, stdout, code=0):
        return self.fake('/bin/cat "$D/out.json"\nexit %d' % code, stdout)

    def run_q(self, args, over=None, parent=None):
        env = {"PATH": "/usr/bin:/bin", "HOME": self.home, "USER": "u", "LANG": "",
               "TMPDIR": self.tmp}
        env.update(parent or {})
        p = subprocess.run([sys.executable, "-I", "-c", DRIVER, SCRIPT, json.dumps(over or {})] + args,
                           env=env, capture_output=True, timeout=60)
        return p.returncode, p.stdout.decode(), p.stderr.decode()

    def calls(self):
        try:
            with open(os.path.join(self.d, "calls")) as f:
                return len(f.read().split())
        except FileNotFoundError:
            return 0

    def check_common(self, out, err, rc):
        self.assertNotIn("Traceback", out + err)
        self.assertNotIn("MARK", out + err)
        self.assertEqual(err, "")
        self.assertLessEqual(self.calls(), 1)
        self.assertEqual(os.listdir(self.tmp), ["apa-plist.not-mine"])  # only own dir removed

    # R1: a valid candidate prints only the allowed fields; errors as a count.
    def test_valid_candidate(self):
        cli = self.emit([rec("cc-plugin-agents-md@market-1", errors=["MARK-ERR", "MARK-ERR2"],
                             mcpServers={"x": {"env": {"TOKEN": "MARK-MCP"}}}),
                         rec("cc-plugin-plugin-authoring@skills-dir", scope="project", enabled=False),
                         rec("other@market-1", scope={"MARK": 1})])
        rc, out, err = self.run_q(["--cli", cli])
        self.assertEqual(rc, 0, out)
        self.assertIn('candidate {"id":"cc-plugin-agents-md@market-1","origin":"market-1","scope":"user","enabled":true,"errors":2}', out)
        self.assertIn('candidate {"id":"cc-plugin-plugin-authoring@skills-dir","origin":"skills-dir","scope":"project","enabled":false,"errors":0}', out)
        self.assertIn("matched=2 entries=3", out)
        self.assertNotIn("other", out)
        self.assertIn("result=ok\ncli=exit_0\ncli_proc=reaped\ndir_cleanup=ok\nsignals=0", out)
        self.check_common(out, err, rc)

    # Non-candidates never print; zero matches is its own result.
    def test_no_match(self):
        cli = self.emit([rec("cc-plugin-agents-md-x@m"), rec("x@cc-plugin-agents-md"), rec("MARK@m")])
        rc, out, err = self.run_q(["--cli", cli])
        self.assertEqual(rc, 3, out)
        self.assertIn("matched=0 entries=3\nresult=no_match", out)
        self.assertNotIn("candidate", out)
        self.check_common(out, err, rc)

    def test_not_json_or_not_list(self):
        for body, reason in [(b"MARK not json", "not_json"), (b'{"id":"MARK"}', "not_a_list"),
                             (b"[1]", "record_shape"), (b'[{"id":5}]', "record_shape"),
                             (b'[{"id":"cc-plugin-agents-md@m","id":"x@m"}]', "not_json"),
                             (b"[NaN]", "not_json"), (b"\xff\xfe", "not_json")]:
            self.setUp()
            cli = self.emit(body)
            rc, out, err = self.run_q(["--cli", cli])
            self.assertEqual(rc, 7, (body, out))
            self.assertIn("reason=" + reason + "\nresult=parse_error", out)
            self.check_common(out, err, rc)

    def test_bad_types_and_ids(self):
        bad = [
            (rec("cc-plugin-agents-md@m", scope={"MARK": "/x"}), "scope"),
            (rec("cc-plugin-agents-md@m", scope="MARK-scope"), "scope"),
            (rec("cc-plugin-agents-md@m", enabled="true"), "enabled"),
            (rec("cc-plugin-agents-md@m", enabled=None), "enabled"),
            (rec("cc-plugin-agents-md@m", enabled=1), "enabled"),
            (rec("cc-plugin-agents-md@m", errors=None), "errors"),
            (rec("cc-plugin-agents-md@m", errors="MARK"), "errors"),
            (rec("cc-plugin-agents-md@m", errors=[{"MARK": 1}]), "errors"),
            (rec("cc-plugin-agents-md"), "id"),
            (rec("cc-plugin-agents-md@"), "id"),
            (rec("cc-plugin-agents-md@/Users/MARK"), "id"),
            (rec("cc-plugin-agents-md@m MARK"), "id"),
            (rec("cc-plugin-agents-md@m\nMARK"), "id"),
            (rec("cc-plugin-agents-md@m@MARK"), "id"),
            (rec("cc-plugin-agents-md@" + "M" * 101), "id"),
        ]
        for r, field in bad:
            self.setUp()
            rc, out, err = self.run_q(["--cli", self.emit([r])])
            self.assertEqual(rc, 8, (r, out))
            self.assertIn("reject=" + field + "\nresult=format_rejected", out)
            self.assertNotIn("candidate", out)
            self.check_common(out, err, rc)

    # A bad second candidate leaves no output of the first.
    def test_no_partial_output(self):
        cli = self.emit([rec("cc-plugin-agents-md@good"), rec("cc-plugin-plugin-authoring@m", enabled="MARK")])
        rc, out, err = self.run_q(["--cli", cli])
        self.assertEqual(rc, 8)
        self.assertNotIn("good", out)
        self.assertNotIn("matched", out)
        self.check_common(out, err, rc)

    # A failed CLI is a failure even with a valid list on stdout.
    def test_cli_nonzero(self):
        rc, out, err = self.run_q(["--cli", self.emit([rec("cc-plugin-agents-md@m")], code=3)])
        self.assertEqual(rc, 4, out)
        self.assertIn("result=cli_failed\ncli=exit_3", out)
        self.assertNotIn("candidate", out)
        self.check_common(out, err, rc)

    # Before the CLI: a bad argument or no scratch directory starts nothing.
    def test_precheck(self):
        cli = self.emit([rec("cc-plugin-agents-md@m")])
        noexec = os.path.join(self.d, "noexec")
        with open(noexec, "w") as f:
            f.write("#!/bin/sh\n")
        for args in ([], ["--cli"], ["--cli", "fake-claude"], ["--cli", noexec], ["--cli", self.d],
                     ["--cli", cli, "extra"], ["--x", cli]):
            rc, out, err = self.run_q(args)
            self.assertEqual(rc, 2, (args, out))
            self.assertIn("reason=usage_or_cli\nresult=precheck_failed\ncli=not_started\ncli_proc=not_started\ndir_cleanup=none", out)
        # mkdtemp itself fails (a prefix under a missing directory); an
        # unwritable TMPDIR would not do, as tempfile falls back to another.
        rc, out, err = self.run_q(["--cli", cli], over={"PREFIX": "no-such-dir/apa-plist."})
        self.assertEqual(rc, 2, out)
        self.assertIn("reason=scratch", out)
        self.assertEqual(self.calls(), 0)

    def wait_dead(self, pid):
        for _ in range(40):
            try:
                os.kill(pid, 0)
            except ProcessLookupError:
                return True
            time.sleep(0.05)
        return False

    # The deadline and the output cap hold while reading; the CLI's group is
    # stopped, its leftover child too, and nothing runs again.
    def test_timeout_and_overflow(self):
        hang = ('trap "" TERM\n( /bin/sleep 60 ) & echo $! > "$D/child"\n'
                'while :; do /bin/sleep 1; done')
        for body, over, rc_want, why in [
            (hang, {"TIMEOUT": 1.0, "GRACE": 0.3}, 5, "timeout"),
            ('( /bin/sleep 60 ) & echo $! > "$D/child"\n/usr/bin/yes MARK-FLOOD', {"MAX_OUTPUT": 4096}, 6, "output_overflow"),
        ]:
            self.setUp()
            cli = self.fake(body)
            start = time.monotonic()
            rc, out, err = self.run_q(["--cli", cli], over=over)
            self.assertLess(time.monotonic() - start, 10)
            self.assertEqual(rc, rc_want, out)
            self.assertIn("result=" + why + "\ncli=exit_signal_", out)
            with open(os.path.join(self.d, "calls")) as f:
                main_pid = int(f.read().split()[0])
            with open(os.path.join(self.d, "child")) as f:
                child = int(f.read())
            self.assertTrue(self.wait_dead(main_pid) and self.wait_dead(child), "a process of the run is alive")
            self.check_common(out, err, rc)

    # A CLI that exits normally but leaves a child behind (stdout closed):
    # the run still ends that child.
    def test_leftover_child_after_normal_exit(self):
        cli = self.fake('( /bin/sleep 60 >/dev/null 2>&1 ) & echo $! > "$D/child"\n/bin/cat "$D/out.json"',
                        [rec("cc-plugin-agents-md@m")])
        rc, out, err = self.run_q(["--cli", cli])
        self.assertEqual(rc, 0, out)
        with open(os.path.join(self.d, "child")) as f:
            self.assertTrue(self.wait_dead(int(f.read())), "the leftover child is alive")
        self.check_common(out, err, rc)

    # Interrupting the script stops the CLI and still cleans up.
    def test_interrupted(self):
        cli = self.fake('while :; do /bin/sleep 1; done')
        env = {"PATH": "/usr/bin:/bin", "HOME": self.home, "TMPDIR": self.tmp}
        p = subprocess.Popen([sys.executable, "-I", "-c", DRIVER, SCRIPT, "{}", "--cli", cli],
                             env=env, stdout=subprocess.PIPE, stderr=subprocess.PIPE)
        for _ in range(100):
            if self.calls():
                break
            time.sleep(0.05)
        p.send_signal(signal.SIGTERM)
        out, err = p.communicate(timeout=20)
        out, err = out.decode(), err.decode()
        self.assertEqual(p.returncode, 10, out)
        self.assertIn("result=interrupted\ncli=exit_signal_15\ncli_proc=reaped\ndir_cleanup=ok\nsignals=1", out)
        self.check_common(out, err, p.returncode)

    def proc_state(self, pid):
        """'' gone, 'Z..' exited but not reaped, anything else still running."""
        p = subprocess.run(["/bin/ps", "-o", "stat=", "-p", str(pid)], capture_output=True)
        return p.stdout.decode().strip()

    def wait_for(self, path, limit=20.0):
        end = time.monotonic() + limit
        while not os.path.exists(path):
            if time.monotonic() > end:
                self.fail("never reached: " + path)
            time.sleep(0.02)

    def reap_fake_later(self):
        """Whatever a case does, end the fake's group afterwards (only it)."""
        def end():
            try:
                with open(os.path.join(self.d, "calls")) as f:
                    pid = int(f.read().split()[0])
            except (OSError, ValueError, IndexError):
                return
            try:
                os.killpg(pid, signal.SIGKILL)
            except (ProcessLookupError, PermissionError):
                pass
        self.addCleanup(end)

    # Exception teardown: an OSError from select() or os.read() once the fake
    # has started and written, or a failing SIGTERM step of the stop itself.
    # The fake ignores SIGTERM, so only the SIGKILL of the stop ends it.
    def inject(self, mode, want_rc, want_result):
        self.reap_fake_later()
        cli = self.fake("trap '' TERM\necho '[' ; echo ready > \"$D/ready\"\nwhile :; do /bin/sleep 0.2; done")
        fds = os.path.join(self.d, "fds")
        env = {"PATH": "/usr/bin:/bin", "HOME": self.home, "TMPDIR": self.tmp}
        over = {"GRACE": 1.0, "TIMEOUT": 4.0} if mode == "killpg" else {"GRACE": 1.0}
        p = subprocess.run([sys.executable, "-I", "-c", INJECT, SCRIPT, json.dumps(over), mode,
                            os.path.join(self.d, "ready"), fds, "--cli", cli],
                           env=env, capture_output=True, timeout=60)
        out, err = p.stdout.decode(), p.stderr.decode()
        self.assertEqual(p.returncode, want_rc, out + err)
        self.assertIn("result=%s\ncli=exit_signal_9\ncli_proc=reaped\n" % want_result, out)
        self.assertEqual(self.calls(), 1)
        with open(os.path.join(self.d, "calls")) as f:
            self.assertEqual(self.proc_state(int(f.read().split()[0])), "", "the fake CLI is still there")
        with open(fds) as f:
            before, after = f.read().split()
        self.assertEqual(before, after, "the CLI's stdout pipe was left open")
        self.check_common(out, err, p.returncode)
        return out

    def test_select_error_stops_cli(self):
        out = self.inject("select", 12, "internal_error")
        self.assertIn("stop_errors=0", out)

    def test_read_error_stops_cli(self):
        out = self.inject("read", 12, "internal_error")
        self.assertIn("stop_errors=0", out)

    # A failing step of the stop does not skip the rest: SIGKILL, wait and
    # close still run, and the failure is counted, never reported as clean.
    def test_stop_step_error_counted(self):
        out = self.inject("killpg", 5, "timeout")
        self.assertIn("stop_errors=1", out)

    # Round 2 R2: an interrupt during the bounded stop, once or as a burst of
    # SIGTERM and SIGINT, skips nothing and does not stretch the stop. The
    # fake ignores SIGTERM (it only notes it), so only SIGKILL ends it.
    def interrupt_in_grace(self, extra):
        grace = 2.0
        cli = self.fake("trap 'echo t >> \"$D/term\"' TERM\nwhile :; do /bin/sleep 0.2; done")
        env = {"PATH": "/usr/bin:/bin", "HOME": self.home, "TMPDIR": self.tmp}
        p = subprocess.Popen([sys.executable, "-I", "-c", DRIVER, SCRIPT, json.dumps({"GRACE": grace}), "--cli", cli],
                             env=env, stdout=subprocess.PIPE, stderr=subprocess.PIPE)
        self.wait_for(os.path.join(self.d, "calls"))  # the fake has started
        p.send_signal(signal.SIGTERM)                  # interrupt while reading
        self.wait_for(os.path.join(self.d, "term"))   # the stop has begun: in the grace period
        t0 = time.monotonic()
        for sig in extra:
            p.send_signal(sig)
            time.sleep(0.05)
        out, err = p.communicate(timeout=30)
        took = time.monotonic() - t0
        out, err = out.decode(), err.decode()
        self.assertEqual(p.returncode, 10, out)
        self.assertIn("result=interrupted\ncli=exit_signal_9\ncli_proc=reaped\ndir_cleanup=ok\nsignals=%d" % (1 + len(extra)), out)
        self.assertLess(took, grace + 1.5, "the stop was stretched")
        with open(os.path.join(self.d, "calls")) as f:
            pid = int(f.read().split()[0])
        self.assertEqual(self.proc_state(pid), "", "the fake CLI is still there")
        self.check_common(out, err, p.returncode)

    def test_interrupt_in_grace(self):
        self.interrupt_in_grace([signal.SIGTERM])

    def test_interrupt_burst_in_grace(self):
        self.interrupt_in_grace([signal.SIGTERM, signal.SIGINT] * 3)

    # Cleanup: a failed removal is visible; it never hides the main result;
    # a directory that is not this run's own is never removed.
    def test_cleanup(self):
        lock = 'touch keep; chmod 500 .\n'
        rc, out, err = self.run_q(["--cli", self.fake(lock + '/bin/cat "$D/out.json"', [rec("cc-plugin-agents-md@m")])])
        self.assertEqual(rc, 9, out)
        self.assertIn("result=cleanup_failed\ncli=exit_0\ncli_proc=reaped\ndir_cleanup=failed", out)
        self.setUp()
        rc, out, err = self.run_q(["--cli", self.fake(lock + "exit 4", None)])
        self.assertEqual(rc, 4, out)
        self.assertIn("result=cli_failed\ncli=exit_4\ncli_proc=reaped\ndir_cleanup=failed", out)

    def load(self):
        import importlib.util
        spec = importlib.util.spec_from_file_location("q", SCRIPT)
        m = importlib.util.module_from_spec(spec)
        spec.loader.exec_module(m)
        tempfile.tempdir = self.tmp  # Scratch() makes its directory here
        self.addCleanup(setattr, tempfile, "tempdir", None)
        return m

    def scratch(self, m):
        sc = m.Scratch()
        with open(os.path.join(sc.path, "own-file"), "w") as f:
            f.write("x")
        return sc

    def keep_file(self, d):
        with open(os.path.join(d, "keep"), "w") as f:
            f.write("bystander")

    def assert_kept(self, d):
        with open(os.path.join(d, "keep")) as f:
            self.assertEqual(f.read(), "bystander")

    # Round 2 R1: cleanup removes this run's directory and nothing else, by
    # the directory's identity, not by the shape of a path.
    def test_cleanup_identity(self):
        m = self.load()
        sc = self.scratch(m)  # untouched: removed
        self.assertTrue(m.remove_own(sc))
        self.assertFalse(os.path.lexists(sc.path))
        self.keep_file(self.bystander)

        # The path is swapped for a symlink to a same-prefix bystander.
        sc = self.scratch(m)
        os.rename(sc.path, sc.path + ".moved")
        os.symlink(self.bystander, sc.path)
        self.assertFalse(m.remove_own(sc))
        self.assert_kept(self.bystander)
        self.assertTrue(os.path.islink(sc.path))

        # The path is replaced by another real directory of the same name.
        sc = self.scratch(m)
        os.rename(sc.path, sc.path + ".moved")
        os.mkdir(sc.path)
        self.keep_file(sc.path)
        self.assertFalse(m.remove_own(sc))
        self.assert_kept(sc.path)

        # A symlink to this run's own (moved) directory is refused before
        # anything is touched: a symlink is never taken for the directory.
        sc = self.scratch(m)
        os.rename(sc.path, sc.path + ".moved")
        os.symlink(sc.path + ".moved", sc.path)
        self.assertFalse(m.remove_own(sc))
        self.assertEqual(os.listdir(sc.path + ".moved"), ["own-file"])

        # Symlinks to HOME and to TMPDIR itself are refused too.
        self.keep_file(self.home)
        for target in (self.home, self.tmp):
            sc = self.scratch(m)
            os.rename(sc.path, sc.path + ".moved")
            os.symlink(target, sc.path)
            self.assertFalse(m.remove_own(sc))
        self.assert_kept(self.home)
        self.assertTrue(os.path.isdir(self.tmp))

        # Swapped right after a passing check: the contents still go through
        # this directory's own fd (the moved original is emptied, the
        # bystander kept), and the final rmdir is refused by the second check.
        sc = self.scratch(m)
        real_check, calls = sc.is_ours, []

        def check_then_swap():
            calls.append(1)
            if len(calls) == 1:
                ok = real_check()
                os.rename(sc.path, sc.path + ".moved")
                os.symlink(self.bystander, sc.path)
                return ok
            return real_check()
        sc.is_ours = check_then_swap
        self.assertFalse(m.remove_own(sc))
        self.assert_kept(self.bystander)
        self.assertEqual(os.listdir(sc.path + ".moved"), [])

        # Swapped for an empty directory right after a passing check: the
        # second check keeps the final rmdir off it.
        sc = self.scratch(m)
        real_check, calls = sc.is_ours, []

        def check_then_empty():
            calls.append(1)
            if len(calls) == 1:
                ok = real_check()
                os.rename(sc.path, sc.path + ".moved")
                os.mkdir(sc.path)
                return ok
            return real_check()
        sc.is_ours = check_then_empty
        self.assertFalse(m.remove_own(sc))
        self.assertTrue(os.path.isdir(sc.path))

    # Environment: CLIEnv's eight variables (empty ones dropped), the three
    # fixed values and the run's own plugin root; nothing else, whatever the
    # parent sets. One call, the fixed argv, an empty working directory.
    def test_environment(self):
        cli = self.emit([rec("cc-plugin-agents-md@m")])
        parent = {"LOGNAME": "lg", "LC_ALL": "C", "TERM": "xterm", "LANG": "",
                  "DISABLE_AUTOUPDATER": "0", "CLAUDE_AGENT_SDK_DISABLE_BUILTIN_AGENTS": "0",
                  "CLAUDE_CODE_PLUGIN_CACHE_DIR": "/elsewhere", "CLAUDE_CODE_OAUTH_TOKEN": "MARK",
                  "ANTHROPIC_API_KEY": "MARK", "CLAUDE_CONFIG_DIR": "/MARK", "HTTPS_PROXY": "MARK"}
        rc, out, err = self.run_q(["--cli", cli], parent=parent)
        self.assertEqual(rc, 0, out)
        with open(os.path.join(self.d, "env")) as f:
            env = dict(l.split("=", 1) for l in f.read().splitlines())
        for k in SHELL_ADDED:
            env.pop(k, None)
        root = env.pop("CLAUDE_CODE_PLUGIN_CACHE_DIR")
        self.assertEqual(env, {"HOME": self.home, "USER": "u", "LOGNAME": "lg", "PATH": "/usr/bin:/bin",
                               "TMPDIR": self.tmp, "LC_ALL": "C", "TERM": "xterm",
                               "DISABLE_AUTOUPDATER": "1", "CLAUDE_CODE_DISABLE_NONESSENTIAL_TRAFFIC": "1",
                               "CLAUDE_AGENT_SDK_DISABLE_BUILTIN_AGENTS": "1"})
        self.assertEqual(os.path.basename(root), "plugins")
        self.assertTrue(os.path.basename(os.path.dirname(root)).startswith("apa-plist."))
        with open(os.path.join(self.d, "args")) as f:
            self.assertEqual(f.read().split(), ["plugin", "list", "--json"])
        with open(os.path.join(self.d, "cwd")) as f:
            self.assertEqual(os.path.basename(f.read().strip()), "work")
        self.assertEqual(self.calls(), 1)
        self.check_common(out, err, rc)

    # The file itself, as the operator runs it, from bash 3.2 on macOS.
    def test_file_from_bash(self):
        cli = self.emit([rec("cc-plugin-agents-md@m")])
        res = os.path.join(self.d, "result.txt")
        p = subprocess.run(["/bin/bash", "-c", '"$1" -I "$2" --cli "$3" > "$4"; echo "exit=$?"',
                            "bash", sys.executable, SCRIPT, cli, res],
                           env={"PATH": "/usr/bin:/bin", "HOME": self.home, "TMPDIR": self.tmp},
                           capture_output=True, timeout=60)
        self.assertEqual(p.stdout.decode(), "exit=0\n")
        with open(res) as f:
            self.assertIn("result=ok", f.read())
        self.assertEqual(self.calls(), 1)


class Wrapper(Case):
    """Round 2 R3: the operator's wrapper itself, run from /bin/bash with an
    evidence parent of the test and a fake CLI; its exit code is the query's,
    or one of its own (20-23)."""

    def setUp(self):
        super().setUp()
        self.base = os.path.join(self.d, "evidence")
        os.mkdir(self.base)

    def wrap(self, cli, base=None):
        env = {"PATH": os.path.dirname(sys.executable) + ":/usr/bin:/bin", "HOME": self.home, "TMPDIR": self.tmp}
        p = subprocess.run(["/bin/bash", WRAPPER, cli, base or self.base], env=env, capture_output=True, timeout=60)
        evs = sorted(os.listdir(base or self.base)) if os.path.isdir(base or self.base) else []
        return p.returncode, p.stdout.decode(), p.stderr.decode(), evs

    def saved(self, ev, name):
        with open(os.path.join(self.base, ev, name)) as f:
            return f.read()

    def test_outcomes_kept(self):
        for stdout, code, want in [([rec("cc-plugin-agents-md@m")], 0, 0),  # found
                                   ([rec("other@m")], 0, 3),               # no_match stays 3
                                   ([rec("cc-plugin-agents-md@m")], 5, 4)]:  # started, then failed
            self.setUp()
            rc, out, err, evs = self.wrap(self.emit(stdout, code))
            self.assertEqual(rc, want, out + err)
            self.assertEqual(len(evs), 1)
            self.assertEqual(self.saved(evs[0], "exit-code.txt"), "exit=%d\n" % want)
            self.assertIn("result=", self.saved(evs[0], "result.txt"))
            self.assertIn("query exit=%d" % want, out)
            self.assertEqual(self.calls(), 1)
        self.setUp()  # not started: the CLI does not exist
        rc, out, err, evs = self.wrap(os.path.join(self.d, "no-such-cli"))
        self.assertEqual(rc, 2, out + err)
        self.assertEqual(self.saved(evs[0], "exit-code.txt"), "exit=2\n")
        self.assertEqual(self.calls(), 0)

    def test_evidence_not_saved(self):
        for i, body in enumerate(['/bin/rm "%s"/plugin-id-query-*/result.txt',  # result.txt lost
                                  '/bin/chmod 500 "%s"/plugin-id-query-*']):    # exit-code.txt cannot be written
            if i:
                self.setUp()  # a fresh evidence parent for each case
            rc, out, err, evs = self.wrap(self.fake(body % self.base + '\n/bin/cat "$D/out.json"', [rec("cc-plugin-agents-md@m")]))
            self.assertEqual(rc, 23, out + err)
            self.assertIn("查詢已嘗試，證據不完整（不重跑）：query exit=0", err)
            self.assertEqual(self.calls(), 1)

    def test_evidence_directory(self):
        cli = self.emit([rec("cc-plugin-agents-md@m")])
        rc, out, err, _ = self.wrap(cli, base=os.path.join(self.d, "missing"))
        self.assertEqual(rc, 21, out + err)
        now = time.time()  # every name the next seconds could take already exists
        for k in range(15):
            d = os.path.join(self.base, time.strftime("plugin-id-query-%Y%m%dT%H%M%SZ", time.gmtime(now + k)))
            os.mkdir(d)
            self.keep_file(d)
        rc, out, err, evs = self.wrap(cli)
        self.assertEqual(rc, 21, out + err)
        for ev in evs:
            self.assertEqual(os.listdir(os.path.join(self.base, ev)), ["keep"])
        rc, out, err, _ = self.wrap(cli, base=os.path.join(self.d, "fake-claude"))  # a file, not a directory
        self.assertEqual(rc, 21, out + err)
        self.assertEqual(self.calls(), 0)
        p = subprocess.run(["/bin/bash", WRAPPER], capture_output=True)
        self.assertEqual(p.returncode, 20)


# Wrapper reuses Case's helpers, not its tests.
for _name in dir(Case):
    if _name.startswith("test_") and _name not in vars(Wrapper):
        setattr(Wrapper, _name, None)


if __name__ == "__main__":
    unittest.main()
