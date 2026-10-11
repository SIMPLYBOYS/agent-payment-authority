#!/usr/bin/env python3
# M-B reverse checks (docs/multi-agent-demo-design.md 10.2): remove one key
# protection at a time in a disposable local clone of HEAD, run the one test
# that covers it, and count the item CAUGHT only when that test fails with
# its expected assertion; compiler errors, panics, timeouts, signals, a test
# that did not run or another failure are not caught. Each item first runs
# its test unmutated (it must pass).
#
#   python3 scripts/verify/m-b-reverse.py --out DIR [name regex]
#   python3 scripts/verify/m-b-reverse.py --list [name regex]
#
# Go items use m1-e3-reverse.py's classifier as it is; Node items read the
# TAP of node --test (an AssertionError of the named test); the Unity item
# reads the NUnit XML (the named test Failed with the expected message).
# The clone gets the Unity project's ignored Library and LimeZu import
# copied in, so the editor does not rebuild them. Output lines as
# m1-e3-reverse.py: PLAN, ITEM, RESULT; exit 0 all caught, 1 not, 2 setup.
import argparse, importlib.util, json, os, re, shutil, subprocess, sys, tempfile, time

here = os.path.dirname(os.path.abspath(__file__))
spec = importlib.util.spec_from_file_location("rev", os.path.join(here, "m1-e3-reverse.py"))
rev = importlib.util.module_from_spec(spec)
spec.loader.exec_module(rev)

C = "cmd/demo/control.go"
T = "internal/timing/timing.go"
AD = "web/adapter.js"
SC = "web/scene.js"
PR = "unity/AgentPaymentAuthority/Assets/APA/Scripts/Projection.cs"


def node(path, test, regex):
    return {"kind": "node", "path": path, "expect": [(test, regex)]}


def unity(test, regex):
    return {"kind": "unity", "expect": [(test, regex)]}


go = rev.go
ITEMS = [
    ("capacity: the cap holds", go("./cmd/demo/", "^TestCapacityAndStopping$", ("TestCapacityAndStopping", r"full: 202")),
     [(C, "if len(c.active) >= c.maxRuns { // a stopping run still holds its place", "if false { // a stopping run still holds its place")]),
    ("capacity: a stopping run holds its place", go("./cmd/demo/", "^TestCapacityAndStopping$", ("TestCapacityAndStopping", r"while stopping: 202")),
     [(C, "if len(c.active) >= c.maxRuns { // a stopping run still holds its place",
       'if func() int { n := 0; for _, x := range c.active { if x.state == "running" { n++ } }; return n }() >= c.maxRuns { // a stopping run still holds its place')]),
    ("one run per consumer", go("./cmd/demo/", "^TestTwoRunsAtOnce$", ("TestTwoRunsAtOnce", r"same consumer: 409")),
     [(C, "if busy := c.active[sc.ConsumerID]; busy != nil {", "if busy := c.active[sc.ConsumerID]; false && busy != nil {")]),
    ("a stop touches only its run", go("./cmd/demo/", "^TestStopIsolation$", ("TestStopIsolation", r"B after A's stop")),
     [(C, '''			lr.state, lr.stopAt = "stopping", time.Now()
			lr.cancel()''', '''			lr.state, lr.stopAt = "stopping", time.Now()
			for _, x := range c.active {
				x.cancel()
			}''')]),
    ("an illegal --max-runs is refused", go("./cmd/demo/", "^TestMaxRunsSetting$", ("TestMaxRunsSetting", r"more than consumers")),
     [(C, "if n < 1 || n > m {", "if n < 1 {")]),
    ("timing: two hosts are not compared", go("./internal/timing/", "^TestNotJudged$/^another_host$", ("TestNotJudged/another_host", r"want not_comparable")),
     [(T, "case a.HostInstance != b.HostInstance:", "case false:")]),
    ("timing: a call outside its process is invalid", go("./internal/timing/", "^TestNotJudged$/^call_before_process$", ("TestNotJudged/call_before_process", r"want invalid")),
     [(T, "case *c.StartedMs < *l.ProcessStartedMs || *c.StartedMs > *l.ProcessReapedMs:", "case false:")]),
    ("timing: interleaving needs A, B, A, B", go("./internal/timing/", "^TestNotInterleaved$/^spans_meet,_a_quiet_gap$", ("TestNotInterleaved/spans_meet,_a_quiet_gap", r"want spans_only")),
     [(T, "if n == 4 {", "if n == 2 {")]),
    ("timing: stop isolation needs B's own call after the stop", go("./internal/timing/", "^TestStopIsolation$/^no_call_of_B_after_the_stop$",
                                                                 ("TestStopIsolation/no_call_of_B_after_the_stop", r"Held:true")),
     [(T, "\n\t\tif !after {", "\n\t\tif false && !after {")]),
    ("web: the six-field rule compares the slot", node("web/test/scene.test.mjs", "the six-field rule: only the exact projection last sent completes", r"another slot"),
     [(AD, "\n  && rep.slot === sent.slot && rep.task === sent.task_id", "\n  && rep.task === sent.task_id")]),
    ("web: each slot is read with its own consumer's token", node("web/test/scene.test.mjs", "two consumers at once, each read with its own token and sent in its own slot",
                                                               r"a consumer's task read with another's token"),
     [(SC, "const t = token(s.consumer);", 'const t = token("consumer-a");')]),
    ("web: a scene that cannot show everyone says so", node("web/test/scene.test.mjs", "each consumer's scene task is its newest run; the scene says what it cannot show", r"deep-equal|deepStrictEqual"),
     [(AD, "overCap: Number.isInteger(maxRuns) && maxRuns > can.size", "overCap: false")]),
    ("unity: an older slot is stale (late messages)", unity("SceneStateTests.ASlotChangeResetsThatCustomerOnly", r"Expected: Stale"),
     [(PR, "if (m.slot < s.Gen) return Outcome.Stale;", "")]),
]

ITEM_TIMEOUT = 900


def command(t):
    if t["kind"] == "node":
        return ["node", "--test", t["path"]]
    if t["kind"] == "unity":
        # The editor's NUnit XML on stdout after the run; the exit code is
        # the editor's.
        return ["sh", "-c", "sh scripts/unity/unity.sh test .rev-unity >/dev/null 2>&1; e=$?; cat .rev-unity/unity-editmode.xml .rev-unity/unity-playmode.xml 2>/dev/null; rm -rf .rev-unity; exit $e"]
    return rev.command(t)


def tap_blocks(text):
    """The TAP blocks of failed tests: name -> the block's text."""
    out, name, buf = {}, None, []
    for line in text.splitlines():
        m = re.match(r"^not ok \d+ - (.*)$", line)
        if m or re.match(r"^ok \d+ - ", line) or line.startswith("# tests"):
            if name:
                out[name] = "\n".join(buf)
            name, buf = (m.group(1).strip() if m else None), []
        elif name:
            buf.append(line)
    if name:
        out[name] = "\n".join(buf)
    return out


def classify(t, rc, text, timed_out=False):
    if t["kind"] in ("go", "py"):
        return rev.classify(t, rc, text, timed_out)
    if timed_out:
        return False, "timed out"
    if rc < 0:
        return False, f"ended by signal {-rc}"
    test, regex = t["expect"][0]
    if t["kind"] == "node":
        blocks = tap_blocks(text)
        if test not in blocks:
            return False, f"the expected test did not fail (or did not run): {test}"
        b = blocks[test]
        if "ERR_ASSERTION" not in b:
            return False, "failed without an assertion (another error)"
        if not re.search(regex, b):
            return False, f"failed without the expected assertion /{regex}/"
        return True, test + ": " + re.search(regex, b).group(0)
    # unity: the named test-case Failed with the expected message
    m = re.search(r'<test-case [^>]*fullname="' + re.escape(test) + r'"[^>]*result="([A-Za-z]+)"[^>]*>(.*?)</test-case>', text, re.S)
    if not m:
        return False, "the expected test did not run (no NUnit result; a compile error leaves none)"
    if m.group(1) != "Failed":
        return False, "the expected test " + m.group(1)
    msg = re.search(r"<message><!\[CDATA\[(.*?)\]\]></message>", m.group(2), re.S)
    if not msg or not re.search(regex, msg.group(1)):
        return False, f"failed without the expected assertion /{regex}/"
    return True, test + ": " + re.search(regex, msg.group(1)).group(0)


def passed(t, rc, text):
    if t["kind"] in ("go", "py"):
        return rev.passed(t, rc, text)
    if rc != 0:
        return False
    if t["kind"] == "node":
        return re.search(r"^# fail 0$", text, re.M) is not None
    test = t["expect"][0][0]
    return re.search(r'<test-case [^>]*fullname="' + re.escape(test) + r'"[^>]*result="Passed"', text) is not None


def runs(cmd, cwd):
    start = time.monotonic()
    try:
        r = subprocess.run(cmd, cwd=cwd, capture_output=True, text=True, timeout=ITEM_TIMEOUT)
        return r.returncode, r.stdout + r.stderr, False, time.monotonic() - start
    except subprocess.TimeoutExpired as e:
        out = "".join(x.decode(errors="replace") if isinstance(x, bytes) else (x or "") for x in (e.stdout, e.stderr))
        return -9, out, True, time.monotonic() - start


def main():
    ap = argparse.ArgumentParser()
    ap.add_argument("only", nargs="?", default="")
    ap.add_argument("--out")
    ap.add_argument("--list", action="store_true")
    a = ap.parse_args()
    items = [m for m in ITEMS if re.compile(a.only).search(m[0])]
    if a.list:
        for i, (name, t, _) in enumerate(items, 1):
            print(f"    item {i}/{len(items)} {name}: {' '.join(command(t))}; expects {t['expect'][0][0]} /{t['expect'][0][1]}/")
        return 0
    if not a.out:
        print("--out DIR is required (or --list)", file=sys.stderr)
        return 2
    root = rev.git("rev-parse", "--show-toplevel").stdout.strip()
    head = rev.git("rev-parse", "HEAD", cwd=root).stdout.strip()
    if rev.git("status", "--porcelain", "--untracked-files=no", cwd=root).stdout.strip():
        print("SETUP the tracked tree has changes; the mutations run on a clone of HEAD, so commit first", flush=True)
        return 2
    os.makedirs(a.out, mode=0o700)
    tmp = tempfile.mkdtemp(prefix="m-b-reverse-")
    copy = os.path.join(tmp, "repo")
    try:
        if rev.git("clone", "--quiet", "--no-hardlinks", root, copy).returncode or rev.git("checkout", "--quiet", head, cwd=copy).returncode:
            print("SETUP the disposable clone could not be made", flush=True)
            return 2
        if any(t["kind"] == "unity" for _, t, _ in items):
            u = "unity/AgentPaymentAuthority"
            for d in ("Library", "Assets/LimeZu"):
                if os.path.isdir(os.path.join(root, u, d)):
                    shutil.copytree(os.path.join(root, u, d), os.path.join(copy, u, d), symlinks=True, dirs_exist_ok=True)
        print(f"BASE {head} (mutations in a disposable clone)", flush=True)
        print(f"PLAN {len(items)}", flush=True)
        baseline, results, failed = {}, [], 0
        for i, (name, t, edits) in enumerate(items, 1):
            print(f"ITEM {i}/{len(items)} {name}", flush=True)
            start = time.monotonic()
            cmd = command(t)
            log = [f"item: {name}", f"command: {' '.join(cmd)}", f"expects: {t['expect']}"]
            key = tuple(cmd) + (t["kind"],)
            if key not in baseline:
                rc, text, to, secs = runs(cmd, copy)
                baseline[key] = passed(t, rc, text) and not to
                log += [f"unmutated: exit {rc}, {secs:.1f}s, {'passes' if baseline[key] else 'DOES NOT PASS'}", "--- unmutated output ---", text]
            caught, reason, rc = False, "", None
            if not baseline[key]:
                reason = "the unmutated test does not pass"
            else:
                for path, old, new in edits:
                    f = os.path.join(copy, path)
                    cur = open(f).read()
                    if cur.count(old) != 1:
                        reason = f"anchor {old[:60]!r} found {cur.count(old)} times in {path}"
                        break
                    open(f, "w").write(cur.replace(old, new))
                    log.append(f"mutated {path}: {old!r} -> {new!r}")
                if not reason:
                    rc, text, to, secs = runs(cmd, copy)
                    caught, reason = classify(t, rc, text, to)
                    log += [f"mutated: exit {rc}, {secs:.1f}s", "--- mutated output ---", text]
                rev.git("checkout", "--quiet", "--", ".", cwd=copy)
                if rev.git("status", "--porcelain", "--untracked-files=no", cwd=copy).stdout.strip():
                    print("SETUP the clone could not be restored", flush=True)
                    return 2
            secs = time.monotonic() - start
            failed += not caught
            verdict = "caught" if caught else "not-caught"
            log.insert(1, f"result: {verdict}: {reason}")
            path = os.path.join(a.out, f"{i:02d}-{re.sub(r'[^A-Za-z0-9]+', '-', name).strip('-')}.log")
            with open(path, "x") as f:
                f.write("\n".join(log) + "\n")
            results.append({"item": name, "result": verdict, "reason": reason, "exit_code": rc, "seconds": round(secs, 1), "log": path})
            print(f"RESULT {verdict} | {secs:.1f}s | {name} | {reason}", flush=True)
        with open(os.path.join(a.out, "results.json"), "x") as f:
            json.dump({"base": head, "items": results}, f, indent=2)
        print(f"{len(items) - failed}/{len(items)} caught by the expected assertion", flush=True)
    finally:
        shutil.rmtree(tmp, ignore_errors=True)
    if rev.git("status", "--porcelain", "--untracked-files=no", cwd=root).stdout.strip() or rev.git("rev-parse", "HEAD", cwd=root).stdout.strip() != head:
        print("SETUP this working tree changed during the run", flush=True)
        return 2
    print("ORIGINAL this working tree unchanged", flush=True)
    return 1 if failed else 0


if __name__ == "__main__":
    sys.exit(main())
