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
DRIVER = (
    "import importlib.util,json,sys\n"
    "s=importlib.util.spec_from_file_location('plugin_id_query',sys.argv[1])\n"
    "m=importlib.util.module_from_spec(s);s.loader.exec_module(m)\n"
    "[setattr(m,k,v) for k,v in json.loads(sys.argv[2]).items()]\n"
    "sys.exit(m.main(sys.argv[3:]))\n"
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
        self.assertIn("result=ok\ncli=exit_0\ncleanup=ok", out)
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
            self.assertIn("reason=usage_or_cli\nresult=precheck_failed\ncli=not_started\ncleanup=none", out)
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
        self.assertIn("result=interrupted\ncli=exit_signal_15", out)
        self.check_common(out, err, p.returncode)

    # Cleanup: a failed removal is visible; it never hides the main result;
    # a directory that is not this run's own is never removed.
    def test_cleanup(self):
        lock = 'touch keep; chmod 500 .\n'
        rc, out, err = self.run_q(["--cli", self.fake(lock + '/bin/cat "$D/out.json"', [rec("cc-plugin-agents-md@m")])])
        self.assertEqual(rc, 9, out)
        self.assertIn("result=cleanup_failed\ncli=exit_0\ncleanup=failed", out)
        self.setUp()
        rc, out, err = self.run_q(["--cli", self.fake(lock + "exit 4", None)])
        self.assertEqual(rc, 4, out)
        self.assertIn("result=cli_failed\ncli=exit_4\ncleanup=failed", out)
        import importlib.util
        s = importlib.util.spec_from_file_location("q", SCRIPT)
        m = importlib.util.module_from_spec(s)
        s.loader.exec_module(m)
        tempfile.tempdir = self.tmp
        try:
            self.assertFalse(m.remove_own(self.home))
            self.assertFalse(m.remove_own(self.tmp))
            other = os.path.join(self.d, "apa-plist.elsewhere")
            os.mkdir(other)
            self.assertFalse(m.remove_own(other))
            self.assertTrue(os.path.isdir(self.home) and os.path.isdir(other))
        finally:
            tempfile.tempdir = None

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


if __name__ == "__main__":
    unittest.main()
