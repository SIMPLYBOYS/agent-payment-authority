#!/usr/bin/env python3
"""Mutation check for the B1-02 regression tests (round 2).

For one commit, export it with git archive into a throwaway directory per
mutation, re-introduce one review finding with an exact text replacement, and
require the finding's regression test to FAIL as a test failure ("--- FAIL"),
not a build error. The unmodified export must pass.

Usage: python3 scripts/verify/mutation-check.py [--root REPO] [--out DIR] [commit]
Writes <out>/<commit12>-<UTC time>/ (never reused). Exit 0 only if the
baseline passes and every mutation is killed.
"""
import argparse
import datetime
import os
import pathlib
import subprocess
import sys
import tempfile

HERE = pathlib.Path(__file__).resolve().parent

# (finding, file, old text, new text, test regex). "old" must occur exactly
# once; a line mutation uses old="LINE:<marker>" to delete that line.
MUTATIONS = [
    ("v2 version", "internal/contracts/canonical.go",
     'const Version = "apa.m1.v2"', 'const Version = "apa.m1.v1"', "TestV2SupersedesV1"),
    ("C1 decided event may record expired", "internal/contracts/events.go",
     '"POLICY_DECIDED":     {"pending", "not_requested"},', '"POLICY_DECIDED":     {"pending", "not_requested", "expired"},', "TestC1"),
    ("C1 closure of a non-Ask", "internal/contracts/events.go",
     'if st.verdict[po] != "Ask" || st.closed[po] {', 'if st.closed[po] {', "TestC1"),
    ("C2 purchase after task end", "internal/contracts/events.go",
     'if st.stopped {\n\t\t\treturn errors.New("no new purchase', 'if false {\n\t\t\treturn errors.New("no new purchase', "TestC2|TestDenyEndsTask"),
    ("C2 decision without proposal", "internal/contracts/events.go",
     "if !st.proposed[po] || e.Payload.Decision == nil {", "if e.Payload.Decision == nil {", "TestC2"),
    ("C2 history checked against current status", "internal/contracts/registry.go",
     "\ti := p.Intent\n", "\tif err := CheckTaskActive(t.Status); err != nil {\n\t\treturn err\n\t}\n\ti := p.Intent\n", "TestC2"),
    ("C3 purchase_order_id reuse", "internal/contracts/registry.go",
     "if e.TaskID == i.TaskID && e.PurchaseOrderID == i.PurchaseOrderID {", "if false {", "TestC3|TestContractCasesFromManifest"),
    ("C3 nonce reuse", "internal/contracts/registry.go",
     "if e.ChainID == i.ChainID && e.AssetContract == i.AssetContract && e.From == i.From && e.EIP3009Nonce == i.EIP3009Nonce {",
     "if false {", "TestC3"),
    ("C4 case-variant keys", "internal/contracts/types.go",
     "if strings.EqualFold(k, seen) {", "if false && strings.EqualFold(k, seen) {", "TestC4|TestContractCasesFromManifest"),
    ("C5 asset decimals bound", "internal/contracts/registry.go",
     "a.Decimals > 77 || ", "", "TestC5"),
    ("C5 registry without assets", "internal/contracts/registry.go",
     " || len(c.Assets) == 0 {", " {", "TestC5"),
    ("C6 deny-asset case removed", "testdata/contracts/cases.json",
     'LINE:"case_id":"deny-asset"', "", "TestC6"),
    ("C7 uncoded ownership error", "internal/contracts/registry.go",
     'coded("OWNERSHIP_MISMATCH", "session, task, wallet or payer do not belong together")',
     'fmt.Errorf("OWNERSHIP_MISMATCH")', "TestContractCasesFromManifest"),
    ("C8 forbidden events not required", "internal/contracts/evidence.go",
     "missing := slices.ContainsFunc(", "missing := false && slices.ContainsFunc(", "TestC8"),
    ("C8 capability claims not required", "internal/contracts/evidence.go",
     '\t\tif !seen[id] {\n\t\t\treturn coded("CAPABILITY_CLAIM_INVALID"',
     '\t\tif false {\n\t\t\treturn coded("CAPABILITY_CLAIM_INVALID"', "TestC8"),
    ("C8 unknown operation accepted", "internal/contracts/evidence.go",
     "if !ok || spec.layer != c.Layer {", "if ok && spec.layer != c.Layer {", "TestC8"),
    ("C9 empty optional string", "internal/contracts/canonical.go",
     'if optional && field.Type.Kind() == reflect.String && string(value) == `""` {', "if false {", "TestC9"),
    ("C10 URL fragment", "internal/contracts/types.go",
     'strings.Contains(i.ResourceURL, "#") || ', "", "TestC10"),
    ("C10 nesting limit off by one", "internal/contracts/canonical.go",
     "if depth >= MaxJSONDepth {", "if depth > MaxJSONDepth {", "TestC10"),
    ("SA1 transfer method", "internal/contracts/types.go",
     'if json.Unmarshal(m, &method) != nil || method != "eip3009" {', "if json.Unmarshal(m, &method) != nil {", "TestSA1|TestContractCasesFromManifest"),
    ("chain link not checked", "internal/contracts/events.go",
     "if e.PrevEventHash != st.prev {", "if false {", "TestEventChain"),
    ("genesis not bound to task", "internal/contracts/events.go",
     'map[string]string{"task_id": taskID}', 'map[string]string{"task_id": ""}', "TestEventChain"),
    # Round 2: intent pinned per purchase, and Deny ends the task.
    ("R2 intent not pinned across events", "internal/contracts/events.go",
     'if pinned := st.intent[po]; pinned != "" && e.Scope.IntentHash != pinned {',
     'if pinned := st.intent[po]; false && pinned != "" && e.Scope.IntentHash != pinned {', "TestIntentBindingAcrossEvents"),
    ("R2 intent pinned to the latest event, not the first proposal", "internal/contracts/events.go",
     "st.proposed[po], st.intent[po] = true, e.Scope.IntentHash",
     "st.proposed[po] = true", "TestIntentBindingAcrossEvents"),
    ("R2 Deny does not end the task", "internal/contracts/events.go",
     'if st.verdict[po] == "Deny" {\n\t\t\tst.stopped = true', 'if false {\n\t\t\tst.stopped = true', "TestDenyEndsTask"),
    ("R2 decision allowed after task end", "internal/contracts/events.go",
     'if st.stopped {\n\t\t\treturn errors.New("no decision', 'if false {\n\t\t\treturn errors.New("no decision', "TestDenyEndsTask"),
    ("R2 open Ask cannot close after task end (over-restriction)", "internal/contracts/events.go",
     'if st.verdict[po] != "Ask" || st.closed[po] {', 'if st.stopped || st.verdict[po] != "Ask" || st.closed[po] {', "TestDenyEndsTask"),
    ("R2 lifecycle end refused after Deny (over-restriction)", "internal/contracts/events.go",
     "if st.lifecycleEnded {", "if st.stopped {", "TestDenyEndsTask"),
    # Round 3: advance-clock cases must follow elapsed >= deadline.
    ("R3 deadline consistency not checked", "internal/contracts/evidence.go",
     'if c.Operation == "advance-clock" && e.Result != deadlineResult(c.Parameters) {', "if false {",
     "TestDeadlineManifestConsistency"),
    ("R3 deadline off by one (elapsed > deadline)", "internal/contracts/evidence.go",
     "if elapsed >= deadline {", "if elapsed > deadline {", "TestDeadlineManifestConsistency"),
]


def run(cmd, cwd):
    p = subprocess.run(cmd, cwd=cwd, capture_output=True, text=True)
    return p.returncode, p.stdout + p.stderr


def export(root, sha, dest):
    archive = subprocess.run(["git", "-C", str(root), "archive", sha], capture_output=True, check=True)
    subprocess.run(["tar", "-xf", "-", "-C", dest], input=archive.stdout, check=True)


def main():
    ap = argparse.ArgumentParser()
    ap.add_argument("commit", nargs="?", default="HEAD")
    ap.add_argument("--root", default=str(HERE.parent.parent))
    ap.add_argument("--out", default=str(HERE.parent.parent / "artifacts" / "verify-container" / "mutation"))
    args = ap.parse_args()
    root = pathlib.Path(args.root)
    sha = subprocess.run(["git", "-C", str(root), "rev-parse", args.commit + "^{commit}"],
                         capture_output=True, text=True, check=True).stdout.strip()
    stamp = datetime.datetime.now(datetime.timezone.utc).strftime("%Y%m%dT%H%M%SZ")
    outroot = pathlib.Path(args.out)
    outroot.mkdir(parents=True, exist_ok=True)
    out = outroot / f"{sha[:12]}-{stamp}"
    out.mkdir()  # fails if it exists: never overwrite evidence
    lines = [f"commit: {sha}", f"run_at: {stamp}", run(["go", "version"], root)[1].strip()]
    ok = True

    with tempfile.TemporaryDirectory() as tmp:
        export(root, sha, tmp)
        rc, log = run(["go", "test", "./internal/contracts/", "-count=1"], tmp)
        (out / "baseline.log").write_text(log)
        lines.append(f"baseline (no mutation): {'PASS' if rc == 0 else 'FAIL'}")
        ok = ok and rc == 0

    for n, (finding, rel, old, new, test) in enumerate(MUTATIONS, 1):
        with tempfile.TemporaryDirectory() as tmp:
            export(root, sha, tmp)
            path = pathlib.Path(tmp) / rel
            text = path.read_text()
            if old.startswith("LINE:"):
                marker = old[5:]
                kept = [l for l in text.splitlines(keepends=True) if marker not in l]
                count, mutated = len(text.splitlines()) - len(kept), "".join(kept)
            else:
                count, mutated = text.count(old), text.replace(old, new)
            if count != 1:
                lines.append(f"{n:02d} ERROR   {finding}: mutation text found {count} times in {rel}")
                ok = False
                continue
            path.write_text(mutated)
            rc, log = run(["go", "test", "./internal/contracts/", "-run", test, "-count=1"], tmp)
            (out / f"mutation-{n:02d}.log").write_text(log)
            killed = rc != 0 and "--- FAIL" in log
            verdict = "KILLED" if killed else ("BUILD?" if rc != 0 else "SURVIVED")
            ok = ok and killed
            lines.append(f"{n:02d} {verdict:8s}{finding}  [{rel}; -run {test}]")

    lines.append("result: " + ("all mutations killed by a failing test" if ok else "NOT OK"))
    (out / "summary.txt").write_text("\n".join(lines) + "\n")
    print("\n".join(lines))
    print(f"run directory: {out}")
    sys.exit(0 if ok else 1)


if __name__ == "__main__":
    main()
