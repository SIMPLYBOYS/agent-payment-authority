#!/usr/bin/env python3
# M1-E1 R1/R2 reverse checks (docs/m1-evidence.md): remove one key check at a
# time, run the tests that cover it, require an assertion failure (not a
# build failure), restore the file. Run from the repository root:
#   python3 scripts/verify/m1-e1-reverse.py
import subprocess, sys

D = "cmd/m1-fixture/main.go"
E = "internal/m1evidence/m1evidence.go"
DRV = ("./cmd/m1-fixture/", "^(TestRunSaving|TestSecretScanAndEndMarker|TestAssembleReverse|TestPassingLogsAreNotEvidence)$")
TAMPER = ("./internal/store/", "^TestM1EvidenceRefusesAlteredExports$")
CONTRACT = ("./internal/m1evidence/", "^TestCheckContract$")

M = [
    # R2: saving and provenance
    ("R2 end marker error ignored", D, 'if err := writeEnd(dir, runID, body, "complete"); err != nil {', 'if err := writeEnd(dir, runID, body, "complete"); err != nil && false {', DRV),
    ("R2 step output save ignored", D, "if err := save(s.name, b); err != nil {", "if err := save(s.name, b); err != nil && false {", DRV),
    ("R2 provenance query error ignored", D, 'if err != nil {\n\t\t\treturn incomplete("provenance_failed", err)\n\t\t}\n\t\tq[c[0]] = v', 'if err != nil && false {\n\t\t\treturn incomplete("provenance_failed", err)\n\t\t}\n\t\tq[c[0]] = v', DRV),
    ("R2 writeEnd hides its error", D, "return os.WriteFile(filepath.Join(dir, \"run-end.json\"), append(b, '\\n'), 0o600)", "os.WriteFile(filepath.Join(dir, \"run-end.json\"), append(b, '\\n'), 0o600)\n\treturn nil", DRV),
    ("R2 scan read error ignored", D, "b, err := os.ReadFile(p)\n\t\tif err != nil {\n\t\t\treturn err\n\t\t}\n\t\tif tokenLike", "b, err := os.ReadFile(p)\n\t\tif err != nil {\n\t\t\treturn nil\n\t\t}\n\t\tif tokenLike", DRV),
    ("R1 driver grades from logs only", D, "summary, problems, err := n.check(raw)", "summary, problems, err := \"\", []string(nil), error(nil)\n\t\t\t\t_ = raw", DRV),
    ("R1 missing record not INCONCLUSIVE", D, 'r.Verdict = worst(r.Verdict, "INCONCLUSIVE")\n\t\t\t\treasons = append(reasons, n.file+": missing")', 'reasons = append(reasons, n.file+": missing")', DRV),
    # R1: recomputation
    ("R1 snapshot digest", E, "if digest(s.Body) != s.Hash {", "if false && digest(s.Body) != s.Hash {", TAMPER),
    ("R1 row vs registry binding", E, "if task.ConsumerID != t.ConsumerID || task.AgentID != t.AgentID || w.WalletID != t.WalletID || w.From != t.Payer {", "if _, _ = task, w; false {", TAMPER),
    ("R1 stored intent digest", E, "if err != nil || h != s.Hash {", "if _ = h; err != nil {", TAMPER),
    ("R1 stored event hash", E, "if h, err := contracts.EventHash(e); err != nil || h != s.Hash {", "if h, err := contracts.EventHash(e); err != nil || false && h != s.Hash {", TAMPER),
    ("R1 event rules and binding", E, "if err := r.ValidateEvent(e, order); err != nil {", "if err := error(nil); err != nil {", TAMPER),
    ("R1 event order and chain", E, "if err := contracts.ValidateEventOrder(c.events); err != nil {", "if err := error(nil); err != nil {", TAMPER),
    ("R1 budget", E, "deducted.Add(deducted, i.Sub(i, r))", "_, _ = i, r", TAMPER),
    ("R1 expected comparison", E, "if f[1] != f[2] {", "if false {", TAMPER),
    ("R1 contract rerun vs recorded", E, "if o != x.Observed {", "if false {", CONTRACT),
    ("R1 contract result vs expected", E, "if o.Result != c.Expected.Result {", "if false {", CONTRACT),
]

failed = 0
for name, path, old, new, (pkg, run) in M:
    src = open(path).read()
    if src.count(old) != 1:
        print(f"ANCHOR {name}: found {src.count(old)} times"); failed += 1; continue
    try:
        open(path, "w").write(src.replace(old, new))
        r = subprocess.run(["go", "test", "-count=1", "-run", run, pkg], capture_output=True, text=True)
        out = r.stdout + r.stderr
        if "build failed" in out or "setup failed" in out or "[no test files]" in out:
            print(f"BUILD  {name}"); failed += 1
        elif r.returncode == 0:
            print(f"MISSED {name}"); failed += 1
        else:
            print(f"caught {name}")
    finally:
        open(path, "w").write(src)
print(f"{len(M) - failed}/{len(M)} caught by an assertion")
sys.exit(1 if failed else 0)
