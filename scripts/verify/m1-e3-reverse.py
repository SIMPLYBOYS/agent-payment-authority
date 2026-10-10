#!/usr/bin/env python3
# M1-E3 reverse checks (docs/m1-evidence.md 16.7): remove one key check of
# the live checker, the live producer, the progress helper or the runner at
# a time, run the one test that covers it, and count the item CAUGHT only
# when that test fails with its expected assertion.
#
#   python3 scripts/verify/m1-e3-reverse.py --out DIR [name regex]
#   python3 scripts/verify/m1-e3-reverse.py --list [name regex]
#
# Every mutation is made in a disposable local clone of HEAD (the tracked
# tree must be clean), never in this working tree; the clone is removed at
# the end and this tree is checked unchanged. Each item first runs its test
# unmutated (it must pass), then mutated. Not caught: a build or setup
# failure, a panic (a test timeout included), a timeout of this script, a
# signal, the expected test not run, or failing without the expected
# assertion. Each item's log in DIR keeps the command, the exit code, the
# reason and the raw output.
#
# It prints PLAN <n>, ITEM <i>/<n> <name> and
# RESULT caught|not-caught | <seconds>s | <name> | <reason> (flushed), for
# the progress runner (cmd/m1-check). Exit 0 all caught, 1 not, 2 setup.
import argparse, json, os, re, shutil, subprocess, sys, tempfile, time

L = "internal/verification/live.go"
V = "internal/verification/verify.go"
R = "internal/verification/report.go"
P = "cmd/m1-live/main.go"
G = "internal/progress/progress.go"
C = "cmd/m1-check/main.go"
S = "scripts/verify/m1-e3-reverse.py"
CHECKER = ("./internal/verification/", "^(TestLive|TestModesAndLayers)")
PRODUCER = ("./cmd/m1-live/", "")


def go(pkg, run, *expect):
    """A go test item: the -run filter and (test, assertion regex) pairs."""
    return {"kind": "go", "pkg": pkg, "run": run, "expect": list(expect)}


def py(path, regex):
    """A Python self-test item: the expected AssertionError message."""
    return {"kind": "py", "path": path, "expect": [("", regex)]}


def legacy(pkg_run):
    """An item from before 10/11: its test filter, no expected assertion yet.
    It cannot count as caught until one is defined."""
    return {"kind": "go", "pkg": pkg_run[0], "run": pkg_run[1], "expect": None}


# (name, test, [(file, old, new), ...])
MUTATIONS = [
    # the live checker (before 10/11; not part of the 10/11 round)
    ("trace gate mode vs the bundle", legacy(CHECKER), [(L, 'case start.GateMode != v.m.Versions["gate_mode"] || runOK && start.GateMode != run.GateMode:', "case false:")]),
    ("run record gate mode vs the bundle", legacy(CHECKER), [(L, 'if run.GateMode != v.m.Versions["gate_mode"] {', "if false {"),
                                                             (L, 'case start.GateMode != v.m.Versions["gate_mode"] || runOK && start.GateMode != run.GateMode:', 'case start.GateMode != v.m.Versions["gate_mode"]:')]),
    ("residual risk stated", legacy(CHECKER), [(L, 'if run.GateMode == "dev_demo_plugins" && !slices.Contains(run.ResidualRisks, "strict zero-plugin mode not verified") {', "if false {")]),
    ("command gate flag", legacy(CHECKER), [(L, 'case strings.Contains(c.Command, " --dev-demo-plugins") != (gate == "dev_demo_plugins"):', "case false:")]),
    ("tool result vs the Store", legacy(CHECKER), [(L, "case c.Outcome != pe.decision.PolicyVerdict:", "case false:")]),
    ("trace calls vs the run record", legacy(CHECKER), [(L, "case !slices.Equal(calls, r.ToolCalls) && !(len(calls) == 0 && len(r.ToolCalls) == 0):", "case false:")]),
    ("trace task and consumer", legacy(CHECKER), [(L, "if start.TaskID != t.row.TaskID || start.ConsumerID != t.row.ConsumerID {", "if false {")]),
    ("snapshot bounds", legacy(CHECKER), [(L, "if run.Snapshot.LastSequence != last.Sequence || run.Snapshot.LastObservedAt != last.ObservedAt || run.Snapshot.ExportedAt < last.ObservedAt {", "if false {")]),
    ("no withdrawn Ask in an ok run", legacy(CHECKER), [(L, 'case "withdrawn":\n\t\t\tf.fail("V3", ev, "the Ask was withdrawn', 'case "withdrawn-x":\n\t\t\tf.fail("V3", ev, "the Ask was withdrawn')]),
    ("expiry not before the deadline", legacy(CHECKER), [(L, "if pe.closedAt < pe.decidedAt+t.terms.ApprovalDeadlineSeconds {", "if false {")]),
    ("approved limits", legacy(CHECKER), [(L, "case start.Limits.TotalSeconds != lim.Total.Seconds() || start.Limits.AssistantMessages != lim.Assistant || start.Limits.ToolCalls != toolbridge.MaxCalls:", "case false:")]),
    ("the fixed prompt", legacy(CHECKER), [(L, "case start.PromptSHA256 != hex.EncodeToString(prompt[:]):", "case false && start.PromptSHA256 != hex.EncodeToString(prompt[:]):")]),
    ("gate passed", legacy(CHECKER), [(L, 'if init.Gate != "passed" || !init.TaskStarted || init.RequestedModel != buyer.Model || init.PrepareError != "" {', "if false {")]),
    ("trace members strict", legacy(CHECKER), [(L, "end = new(traceEnd)\n\t\t\terr = strictJSON(line, end)", "end = new(traceEnd)\n\t\t\terr = json.Unmarshal(line, end)")]),
    ("clean build", legacy(CHECKER), [(L, 'case b.VCSModified != "false":', "case false:")]),
    ("approved CLI version", legacy(CHECKER), [(L, 'if m.Versions["cli"] != buyer.ApprovedCLIVersion {', "if false {")]),
    ("the live run exited 0", legacy(CHECKER), [(L, 'if c.ExitCode != 0 {\n\t\t\tf.fail("V1", ev, "the live run exited', 'if false {\n\t\t\tf.fail("V1", ev, "the live run exited')]),
    ("ask alone claimed, the rest not evaluated", legacy(CHECKER), [(V, "case !required && count[c.CaseID] > 0:", "case false:"), (V, "if !slices.Equal(ne, want) {", "if false {")]),
    ("evidence mode is the mode", legacy(CHECKER), [(V, "if e.EvidenceMode != mode {", "if false {")]),
    ("R not evaluated in live", legacy(CHECKER), [(R, 'r.Claims["R"] = NotEvaluated', "_ = r")]),
    # the live producer (before 10/11)
    ("producer opt-in", legacy(PRODUCER), [(P, 'if on, err := strconv.ParseBool(getenv("APA_LIVE_CLI")); err != nil || !on || !*live {', 'if on, err := strconv.ParseBool(getenv("APA_LIVE_CLI")); false && (err != nil || !on || !*live) {')]),
    ("producer clean tracked tree", legacy(PRODUCER), [(P, 'return stop("the tracked tree has changes (%s); not a clean build", l)', "_ = l")]),
    ("producer reviewed base", legacy(PRODUCER), [(P, 'err != nil || diff != "" {', 'false && (err != nil || diff != "") {')]),
    ("producer clean build", legacy(PRODUCER), [(P, 'if rev != head || modified != "false" {', "if false {")]),
    ("producer end marker error", legacy(PRODUCER), [(P, 'if err := writeEnd(dir, runID, body, "complete"); err != nil {', 'if err := writeEnd(dir, runID, body, "complete"); err != nil && false {')]),
    ("producer scan exemption exact", legacy(PRODUCER), [(P, "tokenLike.Match(bytes.ReplaceAll(raw, promptDigest(), nil))", "tokenLike.Match(bytes.ReplaceAll(raw, []byte(`\"prompt_sha256\":\"`), nil)) && false")]),
    # R1: an interruption stops this run and its whole process group
    ("R1 signal handler cancels the run", go("./cmd/m1-live/", "^TestSignals$/^SIGINT$", ("TestSignals/SIGINT", r'termination "outer_bound"')),
     [(P, 'stopping this run (no retry); the wrapper gets SIGTERM and its bounded cleanup runs\\n", s)\n\t\t\t\tcancel()',
          'stopping this run (no retry); the wrapper gets SIGTERM and its bounded cleanup runs\\n", s)\n\t\t\t\t_ = cancel')]),
    ("R1 a second signal does not cut the cleanup short", go("./cmd/m1-live/", "^TestSignals$/^SIGINT,_then_SIGTERM_during_the_cleanup$",
                                                             ("TestSignals/SIGINT,_then_SIGTERM_during_the_cleanup", r"the second signal was not reported")),
     [(P, 'fmt.Fprintf(os.Stderr, "m1-live: %v again: already stopping; the cleanup continues\\n", s)', "os.Exit(130)")]),
    ("R1 interruption recorded", go("./cmd/m1-live/", "^TestInterruptInProcess$", ("TestInterruptInProcess", r'termination "outer_bound"')),
     [(P, 'case group.Stopped() && ctx.Err() != nil:\n\t\ttermination = "interrupted"', 'case false:\n\t\ttermination = "interrupted"')]),
    ("R1 the stop signals the group, not the leader", go("./internal/progress/", "^TestGroupStop$/^ends_on_SIGTERM$",
                                                         ("TestGroupStop/ends_on_SIGTERM", r"still had members|the grandchild outlived the stop")),
     [(G, "syscall.Kill(-pgid, syscall.SIGTERM)", "syscall.Kill(pgid, syscall.SIGTERM)"), (G, "syscall.Kill(-pgid, syscall.SIGKILL)", "syscall.Kill(pgid, syscall.SIGKILL)")]),
    ("R1 the stop outlasts the group leader", go("./internal/progress/", "^TestGroupMixed$", ("TestGroupMixed", r"the member is still running after Done")),
     [(G, "if !g.stopped.Load() {\n\t\treturn nil\n\t}\n\t<-g.ended", "if true {\n\t\treturn nil\n\t}\n\t<-g.ended")]),
    # R2 (closed 10/11; its items kept, each on its own test)
    ("R2 a normal end is completed", go("./cmd/m1-live/", "^TestLiveAsk$", ("TestLiveAsk", r'exit 1, end "complete"')),  # the producer judges the Ask failed
     [(P, 'case group.Stopped():\n\t\ttermination = "outer_bound"', 'case group.Stopped() || true:\n\t\ttermination = "outer_bound"')]),
    ("R2 checker termination and producer error", go("./internal/verification/", "^TestLiveRecordsThatContradict$/^(only_the_producer_error_says_the_wrapper_could_not_start|recorded_as_interrupted)$",
                                                     ("TestLiveRecordsThatContradict/only_the_producer_error_says_the_wrapper_could_not_start", r"no V4 FAIL finding"),
                                                     ("TestLiveRecordsThatContradict/recorded_as_interrupted", r"no V4 FAIL finding")),
     [(L, 'case t == "completed" && (code != 0 || perr != ""), t == "wrapper_exit_nonzero" && (code == 0 || perr != ""),', 'case false && (t == "completed" && (code != 0 || perr != "") || t == "wrapper_exit_nonzero" && (code == 0 || perr != "")),'),
      (L, 'case t != "completed" || perr != "":', 'case false:')]),
    ("R2 checker run record version", go("./internal/verification/", "^TestLiveRecordsThatContradict$/^a_version_1_run_record$",
                                         ("TestLiveRecordsThatContradict/a_version_1_run_record", r"no V4 FAIL finding")),
     [(L, 'case run.Version != "apa.m1.live.v2":', "case false:")]),
    # R3: the runner saves the whole output and does not claim an undelivered summary
    ("R3 lines after a long line are read", go("./cmd/m1-check/", "^TestLongLine$", ("TestLongLine", r"exit \d+, \d+ log bytes")),
     [(C, "o.line, o.skip = o.line[:0], false", "o.line = o.line[:0]")]),
    ("R3 a log over its limit is incomplete", go("./cmd/m1-check/", "^TestLogLimit$", ("TestLogLimit", r"exit \d+, 1000 log bytes")),
     [(C, "q, o.logErr = q[:room], errLogLimit", "q = q[:room]")]),
    ("R3 a failed log write is reported", go("./cmd/m1-check/", "^TestLogWriteFailure$", ("TestLogWriteFailure", r"exit 0, ")),
     [(C, "if err != nil || w != len(q) {", "if false {")]),
    ("R3 a failed summary write is reported", go("./cmd/m1-check/", "^TestSummaryNotDelivered$/^error$", ("TestSummaryNotDelivered/error", r"exit 0, 1 summaries")),
     [(C, "if n, err := stdout.Write(raw); err != nil || n != len(raw) {", "if n, err := stdout.Write(raw); false && (err != nil || n != len(raw)) {")]),
    ("R3 a short summary write is reported", go("./cmd/m1-check/", "^TestSummaryNotDelivered$/^short_write$", ("TestSummaryNotDelivered/short_write", r"exit 0, 1 summaries")),
     [(C, "if n, err := stdout.Write(raw); err != nil || n != len(raw) {", "if n, err := stdout.Write(raw); err != nil || n < 0 {")]),
    ("R3 a progress.log Close error is in the result", go("./cmd/m1-check/", "^TestProgressFile$/^close_fails$",
                                                         ("TestProgressFile/close_fails", r"exit 0, step PASS, stdout complete true, summary.json complete true")),
     [(C, 'saveErr("progress.log", progFile.Close())', "progFile.Close()")]),
    # R4: only the expected assertion counts
    ("R4 a panic is not caught", py("scripts/verify/m1_e3_reverse_test.py", r"a timeout panic counted as caught"),
     [(S, '\n    if "panic: " in text:', "\n    if False:")]),  # the newline and indent: the code, not this table
    ("R4 the expected assertion is required", py("scripts/verify/m1_e3_reverse_test.py", r"a failure without the expected assertion counted as caught"),
     [(S, "\n        if hit is None:", "\n        if False:")]),
]

ITEM_TIMEOUT = 300  # seconds for one run of one item's test (go test gets -timeout 240s)


def command(t):
    if t["kind"] == "py":
        return ["python3", "-I", t["path"]]
    cmd = ["go", "test", "-json", "-count=1", "-timeout", "240s"]
    if t["run"]:
        cmd += ["-run", t["run"]]
    return cmd + [t["pkg"]]


def events(text):
    """The go test -json events in text, and the lines that are not one."""
    evs, other = [], []
    for line in text.splitlines():
        try:
            e = json.loads(line)
            if isinstance(e, dict) and "Action" in e:
                evs.append(e)
                continue
        except ValueError:
            pass
        other.append(line)
    return evs, other


def classify(t, rc, text, timed_out=False):
    """(caught, reason) for one mutated run. Caught only when each expected
    test ran and failed with its expected assertion, and nothing else
    explains the failure."""
    if t["expect"] is None:
        return False, "no expected assertion defined for this item (before 10/11)"
    if timed_out:
        return False, f"timed out (stopped by the reverse script after {ITEM_TIMEOUT}s)"
    if rc < 0:
        return False, f"ended by signal {-rc}"
    if "panic: " in text:
        first = next(l for l in text.splitlines() if "panic: " in l)
        return False, "a panic, not an assertion: " + first.strip()[:120]
    if t["kind"] == "py":
        last = [l for l in text.splitlines() if l.strip()][-1:] or [""]
        if rc != 1 or not last[0].startswith("AssertionError"):
            return False, f"exit {rc}, last line {last[0][:120]!r}: not an assertion failure"
        if not re.search(t["expect"][0][1], last[0]):
            return False, f"a different assertion: {last[0][:120]!r}"
        return True, last[0][:160]
    evs, other = events(text)
    if any(e["Action"] == "build-fail" for e in evs) or "[build failed]" in text or "[setup failed]" in text:
        return False, "build or setup failed"
    if rc != 1:
        return False, f"exit {rc} (a test failure exits 1)"
    ran = {e["Test"] for e in evs if e.get("Test") and e["Action"] == "run"}
    failed = {e["Test"] for e in evs if e.get("Test") and e["Action"] == "fail"}
    output = {}  # each test's own output lines
    for e in evs:
        if e.get("Test") and e["Action"] == "output":
            output.setdefault(e["Test"], []).append(e.get("Output", ""))
    reasons = []
    for test, regex in t["expect"]:
        if test not in ran:
            return False, f"the expected test {test} did not run"
        if test not in failed:
            return False, f"the expected test {test} did not fail"
        rx = re.compile(regex)
        hit = next((line for line in output.get(test, []) if rx.search(line)), None)
        if hit is None:
            return False, f"{test} failed without the expected assertion /{regex}/"
        reasons.append(f"{test}: {(hit or '').strip()[:120]}")
    return True, "; ".join(reasons)


def passed(t, rc, text):
    """The unmutated run passes: exit 0 and each expected test passed."""
    if rc != 0:
        return False
    if t["kind"] == "py" or t["expect"] is None:
        return True
    evs, _ = events(text)
    ok = {e["Test"] for e in evs if e.get("Test") and e["Action"] == "pass"}
    return all(test in ok for test, _ in t["expect"])


def runs(cmd, cwd):
    start = time.monotonic()
    try:
        r = subprocess.run(cmd, cwd=cwd, capture_output=True, text=True, timeout=ITEM_TIMEOUT)
        return r.returncode, r.stdout + r.stderr, False, time.monotonic() - start
    except subprocess.TimeoutExpired as e:
        out = "".join(x.decode(errors="replace") if isinstance(x, bytes) else (x or "") for x in (e.stdout, e.stderr))
        return -9, out, True, time.monotonic() - start


def git(*args, cwd="."):
    return subprocess.run(["git", *args], cwd=cwd, capture_output=True, text=True)


def main():
    ap = argparse.ArgumentParser()
    ap.add_argument("only", nargs="?", default="")
    ap.add_argument("--out")
    ap.add_argument("--list", action="store_true")
    a = ap.parse_args()
    only = re.compile(a.only)
    items = [m for m in MUTATIONS if only.search(m[0])]
    if a.list:
        for i, (name, t, edits) in enumerate(items, 1):
            exp = "; ".join(f"{x or 'AssertionError'} /{r}/" for x, r in t["expect"]) if t["expect"] else "none defined"
            print(f"    item {i}/{len(items)} {name}: {' '.join(command(t))}; expects {exp}")
        return 0
    if not a.out:
        print("--out DIR is required (or --list)", file=sys.stderr)
        return 2
    root = git("rev-parse", "--show-toplevel").stdout.strip()
    head = git("rev-parse", "HEAD", cwd=root).stdout.strip()
    if git("status", "--porcelain", "--untracked-files=no", cwd=root).stdout.strip():
        print("SETUP the tracked tree has changes; the mutations run on a clone of HEAD, so commit first", flush=True)
        return 2
    os.makedirs(a.out, mode=0o700)  # new, never an old one
    tmp = tempfile.mkdtemp(prefix="m1-e3-reverse-")
    copy = os.path.join(tmp, "repo")
    try:
        if git("clone", "--quiet", "--no-hardlinks", root, copy).returncode or git("checkout", "--quiet", head, cwd=copy).returncode:
            print("SETUP the disposable clone could not be made", flush=True)
            return 2
        print(f"BASE {head} (mutations in a disposable clone)", flush=True)
        print(f"PLAN {len(items)}", flush=True)
        baseline, results, failed = {}, [], 0
        for i, (name, t, edits) in enumerate(items, 1):
            print(f"ITEM {i}/{len(items)} {name}", flush=True)
            start = time.monotonic()
            cmd = command(t)
            log = [f"item: {name}", f"command: {' '.join(cmd)}", f"expects: {t['expect']}"]
            key = tuple(cmd)
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
                git("checkout", "--quiet", "--", ".", cwd=copy)
                if git("status", "--porcelain", "--untracked-files=no", cwd=copy).stdout.strip():
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
    if git("status", "--porcelain", "--untracked-files=no", cwd=root).stdout.strip() or git("rev-parse", "HEAD", cwd=root).stdout.strip() != head:
        print("SETUP this working tree changed during the run", flush=True)
        return 2
    print("ORIGINAL this working tree unchanged", flush=True)
    return 1 if failed else 0


if __name__ == "__main__":
    sys.exit(main())
