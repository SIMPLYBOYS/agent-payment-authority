"""Reverse checks for the A1 spike (review R1 of 20677b0).

Each mutation drops one rule from main.go in a one-time copy of this
directory; it counts as caught only when the named test fails with its
expected assertion. A build or setup error, a panic, a timeout, a signal, a
test that did not run, or any other assertion is not caught. The tracked files
are never written.

    python3 reverse.py EVIDENCE_DIR      # baseline + mutations, logs per item
    python3 reverse.py --self-test       # the classifier on synthetic output

Exit 0 only when every item is caught; the classification of each item and
why, and every baseline and mutant output, are kept under EVIDENCE_DIR.
"""
import json, os, re, shutil, subprocess, sys, tempfile, time

ITEM_TIMEOUT = 120  # seconds per go test run
TOTAL_TIMEOUT = 900  # seconds for the whole run
NEVER = "len(page) < 0 && "  # keeps a disabled condition compiling

# name: (anchor in main.go, replacement, test, expected assertion text)
ITEMS = {
    "used check": ("if p.used {", "if " + NEVER + "p.used {", "TestApproveAndRefusals", "replay: "),
    "expiry": ("if !s.now().Before(", "if " + NEVER + "!s.now().Before(", "TestApproveAndRefusals", "expired: "),
    "counter": ("if cred.Authenticator.CloneWarning {", "if " + NEVER + "cred.Authenticator.CloneWarning {",
                "TestCounterAndBackupFlags", "counter equal: "),
    "BE fixed": ("if stored.Flags.BackupEligible !=", "if " + NEVER + "stored.Flags.BackupEligible !=",
                 "TestCounterAndBackupFlags", "BE change: "),
    "UV forced": ("session.UserVerification = protocol.VerificationRequired", "session.UserVerification = p.session.UserVerification",
                  "TestApproveAndRefusals", "no UV, discouraged: "),
    "suspect at finish": ("if s.suspect[string(stored.ID)] {", "if " + NEVER + "s.suspect[string(stored.ID)] {",
                          "TestCounterAndBackupFlags", "suspect credential on an earlier challenge: "),
    "suspect excluded": ("if !s.suspect[string(c.ID)] {", "if true {", "TestCounterAndBackupFlags", "new challenge for a suspect credential: "),
    "host check": ("if !s.hosts[r.Host] {", "if " + NEVER + "!s.hosts[r.Host] {", "TestRegistrationAndRequests", "host: served"),
    "origin check": ("!s.hosts[o[7:]] ||", "false ||", "TestRegistrationAndRequests", "origin: served"),
    "one finish per begin": ("s.reg = nil // one attempt per begin", "_ = s.reg", "TestRegistrationAndRequests", "second finish: "),
    "challenge from record": ("webauthn.WithChallenge(rec.challenge())", "webauthn.WithChallenge(rnd)",
                              "TestApproveAndRefusals", "challenge not derived from its record: "),
    "raw credential ID hidden": ('"cross-platform"), credTag(parsed.RawID)\n',
                                 '"cross-platform"), base64.RawURLEncoding.EncodeToString(parsed.RawID)\n',
                                 "TestResultsFile", "raw credential ID kept: "),
    "fmt none": ('if cred.AttestationFormat != "none" {', "if " + NEVER + 'cred.AttestationFormat != "none" {',
                 "TestRegistrationAndRequests", "packed attestation: "),
}


def classify(test, expect, returncode, output, timed_out):
    """Returns (caught, why). Only the expected assertion of the named test counts."""
    if timed_out:
        return False, "timeout"
    if returncode is None or returncode < 0:
        return False, "terminated by a signal"
    if "[build failed]" in output or "[setup failed]" in output or re.search(r"^# \S+\n", output, re.M):
        return False, "build or setup error"
    if "panic:" in output:
        return False, "panic"
    if "test timed out" in output:
        return False, "go test timeout"
    if "no tests to run" in output or ("=== RUN   " + test + "\n") not in output:
        return False, "the test did not run"
    if returncode == 0:
        return False, "survived (the test passed)"
    if ("--- FAIL: " + test + " ") not in output:
        return False, "another test failed"
    if not re.search(r"^\s+main_test\.go:\d+: " + re.escape(expect), output, re.M):
        return False, "another assertion failed"
    return True, "expected assertion: " + expect.strip()


def run_test(d, test, deadline):
    limit = min(ITEM_TIMEOUT, max(1, deadline - time.time()))
    cmd = ["go", "test", "-count=1", "-v", "-timeout", "%ds" % ITEM_TIMEOUT, "-run", "^" + test + "$", "."]
    try:
        r = subprocess.run(cmd, cwd=d, capture_output=True, text=True, timeout=limit)
        return r.returncode, r.stdout + r.stderr, False
    except subprocess.TimeoutExpired as e:
        out = (e.stdout or b"").decode() if isinstance(e.stdout, bytes) else (e.stdout or "")
        return None, out, True


def self_test():
    t, x = "TestX", "replay: "
    ran = "=== RUN   TestX\n"
    cases = [
        (ran + "    main_test.go:12: replay: verified\n--- FAIL: TestX (0.00s)\nFAIL\n", 1, False, True),
        ("# example.com/m\n./main.go:3:2: declared and not used: x\nFAIL\texample.com/m [build failed]\n", 1, False, False),
        (ran + "panic: boom\n\ngoroutine 1\nFAIL\texample.com/m\n", 2, False, False),
        (ran, None, True, False),  # our timeout
        (ran + "panic: test timed out after 1s\n", 2, False, False),
        (ran + "    main_test.go:12: replay: verified\n--- FAIL: TestX (0.00s)\n", -9, False, False),  # signal
        ("testing: warning: no tests to run\nPASS\n", 0, False, False),
        (ran + "    main_test.go:40: expired: CHALLENGE_USED\n--- FAIL: TestX (0.00s)\nFAIL\n", 1, False, False),
        (ran + "--- PASS: TestX (0.00s)\nPASS\n", 0, False, False),
        ("=== RUN   TestY\n    main_test.go:12: replay: x\n--- FAIL: TestY (0.00s)\nFAIL\n", 1, False, False),
        (ran + "    helper.go:12: replay: verified\n--- FAIL: TestX (0.00s)\nFAIL\n", 1, False, False),
    ]
    bad = 0
    for i, (out, rc, to, want) in enumerate(cases):
        got, why = classify(t, x, rc, out, to)
        print("self-test %2d: %-5s (%s)" % (i, "ok" if got == want else "WRONG", why))
        bad += got != want
    print("self-test: %d/%d" % (len(cases) - bad, len(cases)))
    return bad == 0


def main():
    if sys.argv[1:] == ["--self-test"]:
        sys.exit(0 if self_test() else 1)
    if len(sys.argv) != 2:
        sys.exit(__doc__)
    evidence = os.path.abspath(sys.argv[1])
    os.makedirs(evidence, mode=0o700, exist_ok=False)
    here = os.path.dirname(os.path.abspath(__file__))
    work = os.path.join(tempfile.mkdtemp(prefix="a1-reverse-"), "spike")
    shutil.copytree(here, work, ignore=shutil.ignore_patterns(".claude"))
    src = open(os.path.join(work, "main.go")).read()
    print("copy:", work, "| evidence:", evidence, flush=True)
    deadline = time.time() + TOTAL_TIMEOUT
    summary, baselines = [], {}
    for n, (name, (anchor, repl, test, expect)) in enumerate(ITEMS.items(), 1):
        slug = "%02d-%s" % (n, re.sub(r"[^a-z0-9]+", "-", name.lower()).strip("-"))
        start = time.time()
        item = {"item": name, "test": test, "expect": expect}
        if time.time() >= deadline:
            item.update(caught=False, why="total time limit reached before this item")
        elif src.count(anchor) != 1:
            item.update(caught=False, why="anchor found %d times" % src.count(anchor))
        else:
            if test not in baselines:  # the unmutated copy must pass and run the test
                rc, out, to = run_test(work, test, deadline)
                open(os.path.join(evidence, "baseline-%s.log" % test), "w").write(out)
                ok = not to and rc == 0 and ("--- PASS: " + test + " ") in out
                baselines[test] = (ok, "baseline rc=%s timeout=%s" % (rc, to))
            ok, why = baselines[test]
            item["baseline_log"] = "baseline-%s.log" % test
            if not ok:
                item.update(caught=False, why="baseline did not pass: " + why)
            else:
                open(os.path.join(work, "main.go"), "w").write(src.replace(anchor, repl))
                try:
                    rc, out, to = run_test(work, test, deadline)
                finally:
                    open(os.path.join(work, "main.go"), "w").write(src)
                open(os.path.join(evidence, slug + ".mutant.log"), "w").write(out)
                caught, why = classify(test, expect, rc, out, to)
                item.update(caught=caught, why=why, returncode=rc, mutant_log=slug + ".mutant.log")
        item["seconds"] = round(time.time() - start, 2)
        summary.append(item)
        print("[%2d/%d] %-26s %-10s %6.2fs  %s" % (n, len(ITEMS), name, "caught" if item["caught"] else "NOT CAUGHT", item["seconds"], item["why"]), flush=True)
    caught = sum(i["caught"] for i in summary)
    json.dump({"caught": caught, "total": len(ITEMS), "items": summary}, open(os.path.join(evidence, "summary.json"), "w"), indent=1, ensure_ascii=False)
    print("%d/%d caught; summary %s" % (caught, len(ITEMS), os.path.join(evidence, "summary.json")))
    sys.exit(0 if caught == len(ITEMS) else 1)


if __name__ == "__main__":
    main()
