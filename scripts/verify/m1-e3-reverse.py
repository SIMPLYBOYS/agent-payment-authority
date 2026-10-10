#!/usr/bin/env python3
# M1-E3 reverse checks (docs/m1-evidence.md section 16): remove one key check
# of the live checker or the live producer at a time (where two checks guard
# the same thing, both), run the tests that cover it, require an assertion
# failure (a build failure does not count), restore the file. Run from the
# repository root:
#   python3 scripts/verify/m1-e3-reverse.py [name filter]
import subprocess, sys

L = "internal/verification/live.go"
V = "internal/verification/verify.go"
R = "internal/verification/report.go"
P = "cmd/m1-live/main.go"
CHECKER = ("./internal/verification/", "^(TestLive|TestModesAndLayers)")
PRODUCER = ("./cmd/m1-live/", "")

# (name, tests, [(file, old, new), ...])
MUTATIONS = [
    # the live checker
    ("trace gate mode vs the bundle", CHECKER, [(L, 'case start.GateMode != v.m.Versions["gate_mode"] || runOK && start.GateMode != run.GateMode:', "case false:")]),
    ("run record gate mode vs the bundle", CHECKER, [(L, 'if run.GateMode != v.m.Versions["gate_mode"] {', "if false {"),
                                                     (L, 'case start.GateMode != v.m.Versions["gate_mode"] || runOK && start.GateMode != run.GateMode:', 'case start.GateMode != v.m.Versions["gate_mode"]:')]),
    ("residual risk stated", CHECKER, [(L, 'if run.GateMode == "dev_demo_plugins" && !slices.Contains(run.ResidualRisks, "strict zero-plugin mode not verified") {', "if false {")]),
    ("command gate flag", CHECKER, [(L, 'case strings.Contains(c.Command, " --dev-demo-plugins") != (gate == "dev_demo_plugins"):', "case false:")]),
    ("tool result vs the Store", CHECKER, [(L, "case c.Outcome != pe.decision.PolicyVerdict:", "case false:")]),
    ("trace calls vs the run record", CHECKER, [(L, "case !slices.Equal(calls, r.ToolCalls) && !(len(calls) == 0 && len(r.ToolCalls) == 0):", "case false:")]),
    ("trace task and consumer", CHECKER, [(L, "if start.TaskID != t.row.TaskID || start.ConsumerID != t.row.ConsumerID {", "if false {")]),
    ("snapshot bounds", CHECKER, [(L, "if run.Snapshot.LastSequence != last.Sequence || run.Snapshot.LastObservedAt != last.ObservedAt || run.Snapshot.ExportedAt < last.ObservedAt {", "if false {")]),
    ("no withdrawn Ask in an ok run", CHECKER, [(L, 'case "withdrawn":\n\t\t\tf.fail("V3", ev, "the Ask was withdrawn', 'case "withdrawn-x":\n\t\t\tf.fail("V3", ev, "the Ask was withdrawn')]),
    ("expiry not before the deadline", CHECKER, [(L, "if pe.closedAt < pe.decidedAt+t.terms.ApprovalDeadlineSeconds {", "if false {")]),
    ("approved limits", CHECKER, [(L, "case start.Limits.TotalSeconds != lim.Total.Seconds() || start.Limits.AssistantMessages != lim.Assistant || start.Limits.ToolCalls != toolbridge.MaxCalls:", "case false:")]),
    ("the fixed prompt", CHECKER, [(L, "case start.PromptSHA256 != hex.EncodeToString(prompt[:]):", "case false && start.PromptSHA256 != hex.EncodeToString(prompt[:]):")]),
    ("gate passed", CHECKER, [(L, 'if init.Gate != "passed" || !init.TaskStarted || init.RequestedModel != buyer.Model || init.PrepareError != "" {', "if false {")]),
    ("trace members strict", CHECKER, [(L, "end = new(traceEnd)\n\t\t\terr = strictJSON(line, end)", "end = new(traceEnd)\n\t\t\terr = json.Unmarshal(line, end)")]),
    ("clean build", CHECKER, [(L, 'case b.VCSModified != "false":', "case false:")]),
    ("approved CLI version", CHECKER, [(L, 'if m.Versions["cli"] != buyer.ApprovedCLIVersion {', "if false {")]),
    ("the live run exited 0", CHECKER, [(L, 'if c.ExitCode != 0 {\n\t\t\tf.fail("V1", ev, "the live run exited', 'if false {\n\t\t\tf.fail("V1", ev, "the live run exited')]),
    ("ask alone claimed, the rest not evaluated", CHECKER, [(V, "case !required && count[c.CaseID] > 0:", "case false:"), (V, "if !slices.Equal(ne, want) {", "if false {")]),
    ("evidence mode is the mode", CHECKER, [(V, "if e.EvidenceMode != mode {", "if false {")]),
    ("R not evaluated in live", CHECKER, [(R, 'r.Claims["R"] = NotEvaluated', "_ = r")]),
    # the live producer
    ("producer opt-in", PRODUCER, [(P, 'if on, err := strconv.ParseBool(getenv("APA_LIVE_CLI")); err != nil || !on || !*live {', 'if on, err := strconv.ParseBool(getenv("APA_LIVE_CLI")); false && (err != nil || !on || !*live) {')]),
    ("producer clean tracked tree", PRODUCER, [(P, 'return stop("the tracked tree has changes (%s); not a clean build", l)', "_ = l")]),
    ("producer reviewed base", PRODUCER, [(P, 'err != nil || diff != "" {', 'false && (err != nil || diff != "") {')]),
    ("producer clean build", PRODUCER, [(P, 'if rev != head || modified != "false" {', "if false {")]),
    ("producer end marker error", PRODUCER, [(P, 'if err := writeEnd(dir, runID, body, "complete"); err != nil {', 'if err := writeEnd(dir, runID, body, "complete"); err != nil && false {')]),
    ("producer scan exemption exact", PRODUCER, [(P, "tokenLike.Match(bytes.ReplaceAll(raw, promptDigest(), nil))", "tokenLike.Match(bytes.ReplaceAll(raw, []byte(`\"prompt_sha256\":\"`), nil)) && false")]),
]

only = sys.argv[1] if len(sys.argv) > 1 else ""
MUTATIONS = [m for m in MUTATIONS if only in m[0]]
failed = 0
for name, (pkg, run), edits in MUTATIONS:
    originals = {}
    try:
        ok = True
        for path, old, new in edits:
            cur = open(path).read()
            originals.setdefault(path, cur)
            if cur.count(old) != 1:
                print(f"ANCHOR {name}: {old[:60]!r} found {cur.count(old)} times"); ok = False; break
            open(path, "w").write(cur.replace(old, new))
        if not ok:
            failed += 1
            continue
        cmd = ["go", "test", "-count=1", pkg] + (["-run", run] if run else [])
        r = subprocess.run(cmd, capture_output=True, text=True)
        out = r.stdout + r.stderr
        if "build failed" in out or "setup failed" in out:
            print(f"BUILD  {name}"); failed += 1
        elif r.returncode == 0:
            print(f"MISSED {name}"); failed += 1
        else:
            print(f"caught {name}")
    finally:
        for path, src in originals.items():
            open(path, "w").write(src)
print(f"{len(MUTATIONS) - failed}/{len(MUTATIONS)} caught by an assertion")
sys.exit(1 if failed else 0)
