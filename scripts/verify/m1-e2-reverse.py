#!/usr/bin/env python3
# M1-E2 reverse checks (docs/m1-evidence.md section 15): remove one key
# check of the read-only verifier at a time (where two checks guard the same
# thing, both), run the verifier's tests, require an assertion failure (a
# build failure does not count), restore the file. Run from the repository
# root:
#   python3 scripts/verify/m1-e2-reverse.py
import subprocess, sys

V = "internal/verification/verify.go"
I = "internal/verification/integration.go"
C = "internal/verification/contract.go"
Q = "internal/verification/requirements.go"
F = "internal/verification/files.go"
R = "internal/verification/report.go"
M = "cmd/verify-demo/main.go"

# (name, [(file, old, new), ...])
MUTATIONS = [
    # V1
    ("V1 evidence digest", [(V, "if sha(raw) != e.Digest {", "if _ = raw; false {")]),
    ("V1 unindexed file", [(V, 'if _, ok := v.paths[p]; !ok && p != "manifest.json" && p != "run-end.json" {', "if false {")]),
    ("V1 inventory violations", [(V, 'f.fail("V1", nil, "the run directory holds what may not be read: %s", x)', "_ = x")]),
    ("V1 unsafe reads", [(F, "if !cleanRel(rel) {", "if false {"), (V, 'case errors.Is(err, errUnsafe):\n\t\tf.fail("V1", []string{p}, "%v", err)', 'case false:')]),
    ("V1 end marker digest", [(V, "case end.ManifestDigest != v.r.Input.ManifestDigest:", "case false:")]),
    ("V1 end marker missing", [(V, 'v.readErr(f, "V1", "run-end.json", err)', "_ = err")]),
    ("V1 config digest", [(V, "if sha([]byte(b.String())) != v.m.ConfigDigest {", "if false {")]),
    ("V1 case list version", [(V, "if m.CaseManifestDigest != v.tr.CasesDigest {", "if false {")]),
    ("V1 command exit codes", [(V, "if c.ExitCode != 0 {", "if false {")]),
    ("V1 claims once each", [(V, "if n > 1 {", "if false {"), (V, "if err := m.ValidateAgainst(v.tr.Cases); err != nil {", "if err := error(nil); err != nil {")]),
    # V2
    ("V2 contract inputs vs fixture", [(C, "func changes(f *findings, ev []string, name string, got, base contracts.PurchaseOrder, allowed map[string]func(string) bool) {",
                                        "func changes(f *findings, ev []string, name string, got, base contracts.PurchaseOrder, allowed map[string]func(string) bool) {\n\treturn")]),
    ("V2 purchase validation", [(I, "if err := order.Validate(); err != nil {", "if err := order.Validate(); err != nil && false {")]),
    ("V2 case parameters", [(I, "v.parameters(f, ev, layer, params, ts[0], s)", "_ = layer")]),
    ("V2/V4 E2E sources", [(I, "v.e2eSources(f, p, rec)", "_ = rec")]),
    # V3
    ("V3 expected outcome", [(I, "if cmp[1] != cmp[2] {", "if false {")]),
    ("V3 necessary conditions", [(I, "v.necessaryConditions(f, ev, t, t.orders[po], e.Payload.Decision, e.ObservedAt)", "_ = po")]),
    ("V3 settings present/absent", [(I, 'if state != "present" && state != "absent" || (state == "present") != has {', "if _ = has; false {")]),
    ("V3 profile valid", [(I, "} else if err := t.profile.Validate(); err != nil {", "} else if err := t.profile.Validate(); err != nil && false {")]),
    ("V3 credential owner", [(I, '} else if c := t.credential; c.ConsumerID != row.ConsumerID || c.EvidenceMode != "fixture" || c.ValidUntil == 0 {', "} else if false {")]),
    ("V3 budget", [(I, "deducted.Add(deducted, i.Sub(i, r))", "_, _ = i, r")]),
    ("V3 expiry timing", [(I, 'case s.closure == "expired" && (advance < deadline || s.closedAt != s.decidedAt+advance):', "case false:")]),
    ("V3 contract result vs expected", [(C, "if res != c.Expected.Result {", "if false {"), (C, 'if res == "reject" && code != c.Expected.Reason {', "if false {")]),
    ("V3 contract record vs rerun", [(C, "if rec.Observed.Result != res || rec.Observed.Code != code {", "if false {")]),
    # V4
    ("V4 event hashes and chain", [(I, 'case err != nil || h != s.Hash:\n\t\t\tf.fail("V4", ev, "task %s event %d: the stored hash', 'case false && (err != nil || h != s.Hash):\n\t\t\tf.fail("V4", ev, "task %s event %d: the stored hash'),
                                   (I, "case e.Sequence != uint64(n+1):", "case false && e.Sequence != uint64(n+1):"),
                                   (I, "case e.PrevEventHash != prev:", "case false && e.PrevEventHash != prev:"),
                                   (I, "if err := contracts.ValidateEventOrder(t.events); err != nil {", "if err := error(nil); err != nil {")]),
    # V5
    ("V5 forbidden and unknown events", [(I, "if !slices.Contains(m1Events, e.EventType) {", "if false {"), (I, "if len(o.ForbiddenFound) > 0 {", "if false {")]),
    ("V5 payment status", [(I, 'if o.PaymentStatus != x.PaymentStatus || o.PaymentStatus != "unavailable" {', "if false {"),
                           (I, 'if d := e.Payload.Decision; d != nil && d.PaymentStatus != "unavailable" {', 'if d := e.Payload.Decision; d != nil && d.PaymentStatus != "unavailable" && false {')]),
    # V6
    ("V6 source labels", [(V, "} else if e.Source != want {", "} else if false {")]),
    ("V6 evidence mode", [(V, 'if e.EvidenceMode != "fixture" {', "if false {")]),
    ("V6 record layer", [(C, 'if rec.CaseID != c.CaseID || rec.Layer != "contracts_test" || rec.Operation != c.Operation {', "if false {")]),
    ("V6 live not supported", [(V, 'if mode != "fixture" {', "if false {")]),
    # V7
    ("V7 task binding", [(I, "case b.ConsumerID != row.ConsumerID || b.AgentID != row.AgentID || w.WalletID != row.WalletID || w.From != row.Payer:", "case false && b.ConsumerID != row.ConsumerID && w.From != row.Payer:"),
                         (I, "case who != identity{row.ConsumerID, row.AgentID, row.WalletID, row.Payer}:", "case false && who != identity{}:")]),
    ("V7 event scope", [(I, "if sc := e.Scope; sc.ConsumerID != row.ConsumerID || sc.AgentID != row.AgentID || sc.WalletID != row.WalletID {", "if false {"),
                        (I, 'claim = "V7"\n\t\t\t}\n\t\t\tf.fail(claim, ev, "task %s event', 'claim = "V4"\n\t\t\t}\n\t\t\tf.fail(claim, ev, "task %s event')]),
    # R
    ("R index vs record", [(Q, 'if said, ok := claimed[c]; ok && said == "pass" && st != "pass" {', 'if said, ok := claimed[c]; ok && said == "pass" && st != "pass" && false {')]),
    ("R failing test", [(Q, 'f.fail("R", []string{c[0]}, "%s: test %s failed", r.name, quote(c[1]))', "_ = r")]),
    # exit codes and reporting
    ("exit code FAIL", [(R, "case Fail:\n\t\treturn 1", "case Fail:\n\t\treturn 2")]),
    ("exit code INCONCLUSIVE", [(R, "\t\treturn 1\n\t}\n\treturn 2\n}", "\t\treturn 1\n\t}\n\treturn 0\n}")]),
    ("report not written", [(M, "\tif err != nil {\n\t\tfmt.Fprintf(stderr, \"verify-demo: the report could not be written", "\tif false {\n\t\tfmt.Fprintf(stderr, \"verify-demo: the report could not be written")]),
    ("worst keeps FAIL", [(R, "case a == Fail || b == Fail:\n\t\treturn Fail", "case a == Fail || b == Fail:\n\t\treturn Inconclusive")]),
]

failed = 0
for name, edits in MUTATIONS:
    originals = {}
    try:
        ok = True
        for path, old, new in edits:
            src = originals.get(path) or open(path).read()
            originals.setdefault(path, src)
            cur = open(path).read()
            if cur.count(old) != 1:
                print(f"ANCHOR {name}: {old[:60]!r} found {cur.count(old)} times"); ok = False; break
            open(path, "w").write(cur.replace(old, new))
        if not ok:
            failed += 1
            continue
        r = subprocess.run(["go", "test", "-count=1", "./internal/verification/", "./cmd/verify-demo/"], capture_output=True, text=True)
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
