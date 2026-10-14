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
# copied in, so the editor does not rebuild them. The copies get new file
# times (shutil.copy, as cp -R): with the original times kept, the editor
# trusts the copied Library's script entries and PlayMode finds the scene's
# scripts missing. Output lines as
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
J = "cmd/timing-judge/main.go"
BR = "unity/AgentPaymentAuthority/Assets/APA/Scripts/ShopBridge.cs"
SG = "internal/signer/core.go"
AS = "cmd/apa-signer/main.go"
PM = "internal/store/payment.go"
CL = "internal/signerclient/client.go"
SA = "internal/signer/approvals.go"
SS = "internal/signer/server.go"
STA = "internal/store/approvals.go"
HA = "cmd/demo/approvals.go"
PC = "internal/paycontract/paycontract.go"
PE = "internal/paycontract/events.go"
SPY = "internal/store/pay.go"
PP = "internal/paypolicy/paypolicy.go"
PV = "internal/paycontract/version.go"
CR = "internal/credential/credential.go"
CD = "internal/credential/dir.go"
SCR = "internal/store/credential.go"
SOP = "internal/store/ops.go"


def node(path, test, regex):
    return {"kind": "node", "path": path, "expect": [(test, regex)]}


def unity(test, regex):
    return {"kind": "unity", "expect": [(test, regex)]}


def ui(script, check):
    """A browser check script: caught when the named check FAILs and the flow
    ran to its end (a crash or a setup failure is not caught)."""
    return {"kind": "ui", "script": script, "expect": [(check, "")]}


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
    # The review of eed0f90 (R1, R2, C1); run them alone with "^(R1|R2|C1):".
    ("R1: a reply read after reset() acts on nothing", node("web/test/scene.test.mjs", "a reply that arrived before reset (new credentials) and is read after it acts on nothing", r"the old reply sent a projection"),
     [(SC, "const current = (s, gen) => slots.get(s.consumer) === s && gen === s.st.gen;", "const current = (s, gen) => gen === s.st.gen;")]),
    ("R1: a reply read after stop() and start() acts on nothing", node("web/test/scene.test.mjs", "a reply that arrived before stop, then start and is read after it acts on nothing", r"the old reply sent a projection"),
     [(SC, "const current = (s, gen) => slots.get(s.consumer) === s && gen === s.st.gen;", "const current = (s, gen) => gen === s.st.gen;")]),
    ("R2: a claimed Store end needs the task's end event", go("./cmd/timing-judge/", "^TestRecordsAgainstTheDatabase$/^A's_record_says_TASK_CANCELLED,_the_database_has_no_such_event$",
                                                         ("TestRecordsAgainstTheDatabase/A's_record_says_TASK_CANCELLED,_the_database_has_no_such_event", r"want not judged, A's cancel has no event in the Store")),
     [(T, 'case r.StoreEnd != "" && row.TaskEnd != r.StoreEnd:', "case false:")]),
    ("R2: the outcome and Store end must be host_runs'", go("./cmd/timing-judge/", "^TestRecordsAgainstTheDatabase$/^B's_record_has_another_outcome_than_host_runs$",
                                                         ("TestRecordsAgainstTheDatabase/B's_record_has_another_outcome_than_host_runs", r"want not judged, B's outcome differs from host_runs")),
     [(T, "case row.Outcome != r.Outcome || row.StoreEnd != r.StoreEnd:", "case false:")]),
    ("R2: host_runs must say the run ended", go("./internal/timing/", "^TestRecordVsDatabase$/^B's_run_not_ended_in_host_runs$",
                                              ("TestRecordVsDatabase/B's_run_not_ended_in_host_runs", r"want invalid: host_runs says it is accepted")),
     [(T, 'case row.State != "ended":', "case false:")]),
    ("R2: stop isolation fails when B was cancelled in the Store", go("./cmd/timing-judge/", "^TestRecordsAgainstTheDatabase$/^B_was_cancelled_in_the_database,_its_record_says_it_ended_on_its_own$",
                                                                    ("TestRecordsAgainstTheDatabase/B_was_cancelled_in_the_database,_its_record_says_it_ended_on_its_own", r"want stop isolation not held, B cancelled in the Store")),
     [(T, 'if otherRow == nil || otherRow.TaskEnd == "TASK_CANCELLED" {', "if false {")]),
    ("C1: a report write error is no verdict", go("./cmd/timing-judge/", "^TestReportWrite$/^a_write_error$", ("TestReportWrite/a_write_error", r"want exit 3 and the reason")),
     [(J, "if n, err := stdout.Write(raw); err != nil || n != len(raw) {", "if n, err := stdout.Write(raw); false && (err != nil || n != len(raw)) {")]),
    ("C1: a short report write is no verdict", go("./cmd/timing-judge/", "^TestReportWrite$/^a_short_write_without_an_error$", ("TestReportWrite/a_short_write_without_an_error", r"want exit 3 and the reason")),
     [(J, "if n, err := stdout.Write(raw); err != nil || n != len(raw) {", "if n, err := stdout.Write(raw); err != nil {")]),
    # UX1 (docs/commercial-ux1.md): selection kept apart from late clicks, and
    # the card's stop and count; run them alone with "^UX1:".
    ("UX1: an older scene's click names nobody", node("web/test/scene.test.mjs", "a click names a customer only for the current scene and its slot", r"an older scene's click"),
     [(SC, "if (!active || paused || !r || r.scene !== scene) return false;", "if (!active || paused || !r) return false;")]),
    ("UX1: another slot's click names nobody", node("web/test/scene.test.mjs", "a click names a customer only for the current scene and its slot", r"another slot's click"),
     [(SC, "return !!s && !!s.task && s.slot === r.slot;", "return !!s && !!s.task;")]),
    ("UX1: unity: an older scene's Select shows no ring", unity("SelectionTests.OnlyTheCurrentScenesSelectShowsTheRing", r"an older scene's Select"),
     [(BR, 'else if (m.scene != scene.Scene) outcome = "Stale";', 'else if (false) outcome = "Stale";')]),
    ("UX1: only a running run can be stopped", node("web/test/scene.test.mjs", "a card stops only a running run and never makes up a count", r"a stopping run is not stopped again"),
     [(AD, 'return { state, text, calls, kind, stoppable: state === "running" };', 'return { state, text, calls, kind, stoppable: state !== "idle" };')]),
    ("UX1: no count from the host is not 0", node("web/test/scene.test.mjs", "a card stops only a running run and never makes up a count", r"no count from the host is unconfirmed, not 0"),
     [(AD, 'else [calls, kind] = ["已回覆工具呼叫：未確認", "unconfirmed"];', 'else [calls, kind] = ["已回覆工具呼叫 " + (run.tool_calls || 0) + " 次", "known"];')]),
    # Review of 382a4b5 (R1): the focus kept across a card rebuild; alone with "^UX1-R1:".
    ("UX1-R1: a card rebuild keeps the confirmation's focus", ui("scripts/verify/ux1-focus-ui.mjs", "B's confirm keeps the focus while A's card updates"),
     [("web/host.js", "function keepFocus(el, rebuild) {", "function keepFocus(el, rebuild) { return rebuild();")]),
    ("UX1-R1: a newer run's stop never gets the old run's focus", ui("scripts/verify/ux1-focus-ui.mjs", "a new run's stop does not get the old run's focus: A's 選取 has it"),
     [("web/host.js", ' + attr("run", was.ownRun));', ");")]),
    # P1-a (docs/commercial-p1a.md): delegation and the single simulated
    # approval; run them alone with "^P1a:".
    ("P1a: the approver is bound to the session's consumer", go("./internal/store/", "^TestPayDenyAndOtherConsumer$", ("TestPayDenyAndOtherConsumer", r"B approves A's session")),
     [("internal/store/pay.go", "err == nil && ss.ConsumerID != req.ConsumerID {", "err == nil && false {")]),
    ("P1a: the approved intent must be the session's", go("./internal/store/", "^TestPayDenyAndOtherConsumer$", ("TestPayDenyAndOtherConsumer", r"another intent")),
     [("internal/store/pay.go", "if req.IntentHash != p.Session.IntentHash {", "if false {")]),
    ("P1a: a reached deadline closes the session before any decision", go("./internal/store/", "^TestPayDeadline$", ("TestPayDeadline/at_the_deadline", r"Outcome:approved")),
     [("internal/store/pay.go", '\t\tif err := t.expire(); err != nil {\n\t\t\treturn err\n\t\t}\n\t\tif p.Session.State != "pending" {', '\t\tif p.Session.State != "pending" {')]),
    ("P1a: a resend of the deciding request returns its result", go("./internal/store/", "^TestPayAskApproveOnce$", ("TestPayAskApproveOnce", r"resend 0")),
     [("internal/store/pay.go", "if p.Session.ResolveRequestID.String == req.RequestID {", "if false {")]),
    ("P1a: a closed session is not decided again", go("./internal/store/", "^TestPayAskApproveOnce$", ("TestPayAskApproveOnce", r"another request")),
     [("internal/store/pay.go", '\t\tif p.Session.State != "pending" {\n\t\t\tout.Outcome, out.Purchase = "closed", t.view(p)', '\t\tif false {\n\t\t\tout.Outcome, out.Purchase = "closed", t.view(p)')]),
    ("P1a: the stored task's version picks the entry", go("./internal/store/", "^TestPayVersionBoundary$", ("TestPayVersionBoundary", r"M1 body, v1 task")),
     [("internal/store/ops.go", "if (v1 != nil) != t.pay() {", "if false {")]),  # C2b: t.v1() is t.pay()
    ("P1a: a delegation is no wider than the profile", go("./internal/store/", "^TestPayDelegationGuards$", ("TestPayDelegationGuards", r"wider than the profile")),
     [("internal/store/pay.go", "if budget > limit || narrow > allow ||", "if false && budget > limit && narrow > allow ||")]),
    ("P1a: the payment binding is in the intent hash", go("./internal/paycontract/", "^TestBindingInTheHash$", ("TestBindingInTheHash", r"the intent hash did not change")),
     [("internal/paycontract/paycontract.go", "}{p.Intent, p.PaymentBinding})", "}{p.Intent, PaymentBinding{Amount: \"0\"}})")]),
    ("P1a: the shared rules keep the judgement order", go("./internal/policy/", "^TestRulesOrder$", ("TestRulesOrder", r"want Deny ASSET_NOT_ALLOWED")),
     [("internal/policy/policy.go", '\tcase !in.AssetTrusted || in.AssetID != p.AssetID: // budgets are in the profile\'s asset only\n\t\treturn RuleOutcome{Denied, "ASSET_NOT_ALLOWED"}\n\tcase !slices.Contains(p.Merchants, in.PayTo):\n\t\treturn RuleOutcome{Denied, "MERCHANT_NOT_ALLOWED"}',
       '\tcase !slices.Contains(p.Merchants, in.PayTo):\n\t\treturn RuleOutcome{Denied, "MERCHANT_NOT_ALLOWED"}\n\tcase !in.AssetTrusted || in.AssetID != p.AssetID: // budgets are in the profile\'s asset only\n\t\treturn RuleOutcome{Denied, "ASSET_NOT_ALLOWED"}')]),
    ("P1a: M1 evidence refuses an apa.pay.v1 task", go("./internal/store/", "^TestM1EvidenceRefusesPayTask$", ("TestM1EvidenceRefusesPayTask", r"export of the v1 task")),
     [("internal/m1evidence/m1evidence.go", "err == nil && version != contracts.Version {", "err == nil && false {")]),
    ("P1a: an apa.pay.v1 run starts with a delegation", go("./cmd/demo/", "^TestP1aPermissions$", ("TestP1aPermissions", r"apa.pay.v1 without a delegation")),
     [("cmd/demo/control.go", 'if paycontract.Supported(sc.version()) != (req.DelegationID != "") {', "if false {")]),  # C2b: the pay family
    ("P1a: viewer, host and operator never approve", go("./cmd/demo/", "^TestP1aPermissions$", ("TestP1aPermissions", r"(viewer|host|operator) approves: 200")),
     [("cmd/demo/sim.go", "\tconsumer := a.sim.holder(tok)\n", '\tconsumer := a.sim.holder(tok)\n\tif consumer == "" && tok != "" {\n\t\tconsumer = "consumer-a"\n\t}\n')]),
    ("P1a: a live host has no simulated approver", go("./cmd/demo/", "^TestP1aLiveHostHasNoSimulation$", ("TestP1aLiveHostHasNoSimulation", r"simulate on a live host")),
     [("cmd/demo/main.go", "if !live { // the simulated approver exists in fake mode only", "if true { // the simulated approver exists in fake mode only")]),
    ("P1a: web: a new generation holds no capability", node("web/test/p1a.test.mjs", "the simulated customer: one at a time, per generation; a late reply after a switch is dropped", r"a new generation holds no capability"),
     [("web/hostcreds.js", "    held = null;\n    sim = null;\n", "    held = null;\n")]),
    ("P1a: web: a reply after a switch of customer is dropped", node("web/test/p1a.test.mjs", "the simulated customer: one at a time, per generation; a late reply after a switch is dropped", r"after the switch to B is dropped"),
     [("web/hostcreds.js", "    if (sim !== cap) return { gen: g, simId: cap.id, consumer: cap.consumer, superseded: true };\n", "")]),
    ("P1a: web: Unity never shows an approval as still waiting", node("web/test/p1a.test.mjs", "apa.pay.v1: each closure is its own phase; Unity gets the M1 phase that is true of it", r"APPROVED"),
     [("web/adapter.js", 'export const UNITY_PHASE = { approved: "allowed_unpaid",', 'export const UNITY_PHASE = { approved: "waiting",')]),
    ("P1a: ui: 建立委託 waits for the customer's simulation", ui("scripts/verify/p1a-ui.mjs", "each card offers 以 X 模擬 and 建立委託 (disabled until simulated); no plain start for apa.pay.v1"),
     [("web/host.js", "b.disabled = starting || busy.has(m.c) || full || simC !== m.c;", "b.disabled = starting || busy.has(m.c) || full;")]),
    # Review of f95415d (R1-R3); run them alone with "^P1a-R:".
    ("P1a-R: R3 recovery recomputes the session deadline", go("./internal/store/", "^TestPayRecoveryDeadlines$", ("TestPayRecoveryDeadlines/a_pending_session's_deadline_moved_later", r"recovery accepted it")),
     [("internal/store/pay.go", "\t\tif ss.ExpiresAt != t.sessionDeadline(p) {", "\t\tif false {")]),
    ("P1a-R: R3 recovery checks the grant's times", go("./internal/store/", "^TestPayRecoveryDeadlines$", ("TestPayRecoveryDeadlines/the_grant's_expiry_moved_later", r"recovery accepted it")),
     [("internal/store/pay.go", "if string(c) != ss.grantBody ||", "if false && (string(c) != ss.grantBody ||"),
      ("internal/store/pay.go", "g.ApproverID != ss.ResolvedBy.String {", "g.ApproverID != ss.ResolvedBy.String) {")]),
    ("P1a-R: R3 expiry uses the recomputed deadline", go("./internal/store/", "^TestPayExpiryIgnoresStoredDeadline$", ("TestPayExpiryIgnoresStoredDeadline", r"at the recomputed deadline")),
     [("internal/store/ops.go", "due = t.now >= t.sessionDeadline(p)", "due = t.now >= p.Session.ExpiresAt")]),
    ("P1a-R: R2 an operation never resends after its capability changed", node("web/test/p1a.test.mjs", "R2 (throw): after a failed attempt, A→B and A→B→A send nothing with the new capability", r"no resend after the switch"),
     [("web/hostcreds.js", "      if (sim !== cap) return gone();\n      try {", "      try {")]),
    ("P1a-R: R1 the target compares the item, capability and generation", node("web/test/p1a.test.mjs", "R1: sameTarget: the same item of the same capability and generation; never another", r"another session"),
     [("web/adapter.js", 'const TARGET_KEYS = ["gen", "sim", "consumer", "task", "session", "intent"];', 'const TARGET_KEYS = ["gen", "consumer"];')]),
    ("P1a-R: R1 ui: a rebuilt panel gives the focus back to the same item only", ui("scripts/verify/p1a-ui.mjs", "R1 rebuild: show-a-06 expires, the panel shows show-a-04, the focus left for the heading"),
     [("web/host.js", "if (same && !same.disabled && A.sameTarget(had.target, target, submits)) same.focus();", "if (same && !same.disabled) same.focus();")]),
    # P1-b (docs/commercial-p1b-design.md 8): the Signer, schema 4 and the
    # Host client; run them alone with "^P1b:".
    ("P1b: R1 a delegation's version must match", go("./internal/signer/", "^TestRefusalsConsumeNothing$", ("TestRefusalsConsumeNothing", r"another version: <nil>")),
     [(SG, "\tcase d.Version != r.Authorization.DelegationVersion:", "\tcase false:")]),
    ("P1b: R1 the cumulative cap", go("./internal/signer/", "^TestTotalCap$", ("TestTotalCap", r"over the cap")),
     [(SG, "if total, _ := units(d.TotalCap); spent+amount > total {", "if false {")]),
    ("P1b: R1 A's delegation never signs B's wallet", go("./internal/signer/", "^TestRefusalsConsumeNothing$", ("TestRefusalsConsumeNothing", r"A's delegation for B: <nil>")),
     [(SG, "\tcase d.ConsumerID != i.ConsumerID || d.WalletID != i.WalletID:", "\tcase false:")]),
    ("P1b: simulated is never accepted", go("./internal/signer/", "^TestRefusalsConsumeNothing$", ("TestRefusalsConsumeNothing", r"simulated: .*want GRANT_METHOD_NOT_ACCEPTED")),
     [(SG, '\tcase "delegation":\n', '\tcase "delegation", "simulated":\n')]),
    ("P1b: the production assembly signs nothing before P2", go("./cmd/apa-signer/", "^TestAssemblyRefusesEveryOrder$", ("TestAssemblyRefusesEveryOrder", r"the production assembly signed")),
     [(AS, '\t"context"\n', '\t"context"\n\t"encoding/json"\n'),
      (AS, "clock, signer.Unavailable{})", "clock, open{})"),
      (AS, "// private refuses a directory", "type open struct{}\n\nfunc (open) Verify(string, json.RawMessage) error { return nil }\n\n// private refuses a directory")]),
    ("P1b: R2 recovery finishes from the snapshot before serving", go("./internal/signer/", "^TestInterruptions$", ("TestInterruptions/after-accept", r"recovery used the request, not the snapshot")),
     [(SG, "\tfor _, p := range pending {", "\tfor _, p := range pending[:0] {")]),
    ("P1b: R2 the same request ID with other content is refused", go("./internal/signer/", "^TestResendConflictReplay$", ("TestResendConflictReplay", r"same ID, other content")),
     [(SG, "\t\tif a.fingerprint != fp {", "\t\tif false {")]),
    ("P1b: R2 the key is chosen by the snapshot's address", go("./internal/signer/", "^TestKeyVersionMismatch$", ("TestKeyVersionMismatch", r"signed with another key")),
     [(SG, "if key == nil || Address(key.PubKey()) != a.snap.Address {", "if key == nil {")]),
    ("P1b: R2 a saved result is answered after expiry", go("./internal/signer/", "^TestFinalDeadline$", ("TestFinalDeadline", r"saved result refused after expiry")),
     [(SG, "\t}\n\treturn reply(a), nil\n}\n\n// Abandon gives up", "\t}\n\tif c.clock() >= a.deadline {\n\t\treturn Reply{}, Refusal(\"ATTEMPT_EXPIRED\")\n\t}\n\treturn reply(a), nil\n}\n\n// Abandon gives up")]),
    ("P1b: R3-a accepting takes its own time after the write lock", go("./internal/signer/", "^TestAcceptDeadline$", ("TestAcceptDeadline", r"accepted at the deadline")),
     [(SG, "\tif now >= s.AuthDeadline {", "\tif false {")]),
    ("P1b: R3-a the final decision checks the deadline", go("./internal/signer/", "^TestFinalDeadline$", ("TestFinalDeadline/reached_before_saving", r"signed after the deadline")),
     [(SG, "WHERE attempt_id = ? AND state = 'accepted' AND ? < auth_deadline`,\n\t\tsig, now, id, now)", "WHERE attempt_id = ? AND state = 'accepted' AND ? > 0`,\n\t\tsig, now, id, now)")]),
    ("P1b: R3-a the deadline is strict", go("./internal/signer/", "^TestFinalDeadline$", ("TestFinalDeadline/exactly_at_the_deadline", r"signed after the deadline")),
     [(SG, "WHERE attempt_id = ? AND state = 'accepted' AND ? < auth_deadline`,\n\t\tsig, now, id, now)", "WHERE attempt_id = ? AND state = 'accepted' AND ? <= auth_deadline`,\n\t\tsig, now, id, now)")]),
    ("P1b: R3-a the final decision reads the time after the write lock", go("./internal/signer/", "^TestFinalDeadline$", ("TestFinalDeadline/reached_before_saving", r"signed after the deadline")),
     [(SG, "\tdefer tx.Rollback()\n\tnow := c.clock()\n\tif now < a.acceptedAt {", "\tdefer tx.Rollback()\n\tnow := a.acceptedAt\n\tif now < a.acceptedAt {")]),
    ("P1b: R3-b no signature after abandon", go("./internal/signer/", "^TestAbandonRaces$", ("TestAbandonRaces/abandon_first", r"signature returned after abandon")),
     [(SG, "UPDATE attempts SET state = 'signed', signature = ?, signed_at = ? WHERE attempt_id = ? AND state = 'accepted' AND ? < auth_deadline`",
       "UPDATE attempts SET state = 'signed', signature = ?, signed_at = ?, closed_at = NULL WHERE attempt_id = ? AND ? < auth_deadline`")]),
    ("P1b: R3-b abandon never waits for the signing lock", go("./internal/signer/", "^TestAbandonRaces$", ("TestAbandonRaces/abandon_first", r"abandon blocked by the signing lock")),
     [(SG, "func (c *Core) Abandon(ctx context.Context, attemptID string) (Reply, error) {\n", "func (c *Core) Abandon(ctx context.Context, attemptID string) (Reply, error) {\n\tif a, ok, _ := c.byID(ctx, attemptID); ok {\n\t\tif unlock, err := c.lockWallet(ctx, a.snap.WalletID); err == nil {\n\t\t\tdefer unlock()\n\t\t}\n\t}\n")]),
    ("P1b: R3-b abandon never overwrites signed", go("./internal/signer/", "^TestAbandonRaces$", ("TestAbandonRaces/signed_first", r"abandon overwrote signed")),
     [(SG, "UPDATE attempts SET state = 'abandoned', closed_at = ? WHERE attempt_id = ? AND state = 'accepted' AND ? < auth_deadline`, now, attemptID, now)",
       "UPDATE attempts SET state = 'abandoned', closed_at = ?, signature = NULL, signed_at = NULL WHERE attempt_id = ? AND ? < auth_deadline`, now, attemptID, now)")]),
    ("P1b: R3-b abandon after expiry changes nothing", go("./internal/signer/", "^TestAbandonRaces$", ("TestAbandonRaces/expired,_then_abandon", r"abandon after expiry")),
     [(SG, "UPDATE attempts SET state = 'abandoned', closed_at = ? WHERE attempt_id = ? AND state = 'accepted' AND ? < auth_deadline`, now, attemptID, now)",
       "UPDATE attempts SET state = 'abandoned', closed_at = ? WHERE attempt_id = ? AND ? > 0`, now, attemptID, now)")]),
    ("P1b: crypto: v follows r and s and is 27 or 28", go("./internal/signer/", "^TestEIP712SpecVector$", ("TestEIP712SpecVector", r"signature [0-9a-f]+")),
     [("internal/signer/eip712.go", "c[1:65]...), c[0])", "c[1:65]...), c[0]-27)")]),
    ("P1b: crypto: key files of another version are refused", go("./internal/signer/", "^TestKeyFile$", ("TestKeyFile", r"other version: accepted")),
     [("internal/signer/keystore.go", "if f.Version != 1 || f.KDF", "if f.KDF")]),
    ("P1b: crypto: the scrypt work factor is bounded", go("./internal/signer/", "^TestKeyFile$", ("TestKeyFile", r"weak scrypt: accepted")),
     [("internal/signer/keystore.go", "f.N < 1<<14 ||", "f.N < 1 ||")]),
    ("P1b: keystore: key files private to their owner", go("./cmd/apa-signer/", "^TestAssemblyRefusesItsInputs$", ("TestAssemblyRefusesItsInputs", r"group-readable key file")),
     [("internal/signer/keystore.go", "if !fi.Mode().IsRegular() || fi.Mode().Perm()&0o077 != 0 {", "if !fi.Mode().IsRegular() {")]),
    ("P1b: IPC: a caller uid not configured is refused", go("./internal/signer/", "^TestServerCaller$", ("TestServerCaller", r"a uid not configured")),
     [("internal/signer/server.go", "if !ok || uid < 0 || !core.cfg.allows(uint32(uid)) {", "if !ok || uid < 0 {")]),
    ("P1b: R4 Store: a purchase keeps its one request ID", go("./internal/store/", "^TestPaymentDelegationPath$", ("TestPaymentDelegationPath", r"resend 0")),
     [(PM, "paymentWhere(`a.task_id = ? AND a.purchase_order_id = ?`)", "paymentWhere(`a.task_id = ? AND a.purchase_order_id = ? AND 0`)")]),
    ("P1b: R4 Store: schema 4 accepts delegation only", go("./internal/store/", "^TestPaymentSchemaChecks$", ("TestPaymentSchemaChecks/method_simulated", r"non-delegation payment attempt accepted")),
     [(PM, "CHECK (method = 'delegation'),", "CHECK (method IN ('delegation', 'simulated', 'webauthn')),")]),
    ("P1b: R4 Store: recovery refuses a row of an Ask", go("./internal/store/", "^TestPaymentSchemaChecks$", ("TestPaymentSchemaChecks/an_Ask's_purchase", r"want a payment corruption")),
     [(PM, " OR p.policy_verdict != 'Allow'", "")]),
    ("P1b: R4 Store: a simulated approval is never paid", go("./internal/store/", "^TestUpgradeSchema3$", ("TestUpgradeSchema3", r"a simulated approval was paid")),
     [(PM, '\t\tcase p.Decision.PolicyVerdict != "Allow":', "\t\tcase false:")]),
    ("P1b: R4 Store: the Host writes no webauthn attempt", go("./internal/store/", "^TestPaymentRefusals$", ("TestPaymentRefusals", r"webauthn: ")),
     [(PM, '\tcase req.Method == "webauthn":\n\t\treturn PaymentAttempt{}, false, refuse("HUMAN_APPROVAL_UNAVAILABLE", "webauthn payments are A2\'s")\n\tcase req.Method != "delegation":',
       '\tcase req.Method != "delegation" && req.Method != "webauthn":')]),
    ("P1b: R1 Store: B reads nothing of A's", go("./internal/store/", "^TestPaymentDelegationPath$", ("TestPaymentDelegationPath", r"B read A's attempt")),
     [(PM, "err == nil && consumer != consumerID {", "err == nil && len(consumer) < 0 {")]),
    ("P1b: client: no reply is recorded unknown", go("./internal/signerclient/", "^TestUnknownThenResume$", ("TestUnknownThenResume", r"no Signer")),
     [(CL, 'updated, uerr := c.ledger.UpdatePayment(a.RequestID, "unknown", a.SignerAttemptID, "")', "updated, uerr := a, error(nil)")]),
    # Review of 22e45ed (R1-R3); run them alone with "^P1b-R:".
    ("P1b-R: R1 a cancelled task gets no payment attempt", go("./internal/store/", "^TestPaymentAfterCancel$", ("TestPaymentAfterCancel/a_purchase_never_paid", r"cancelled task accepted a new payment request")),
     [(PM, "case slices.ContainsFunc(t.events,", "case false && slices.ContainsFunc(t.events,")]),
    ("P1b-R: R1 a cancelled task's unknown attempt is not handed out again", go("./internal/store/", "^TestPaymentAfterCancel$", ("TestPaymentAfterCancel/an_unknown_attempt", r"handed out its unknown attempt for sending")),
     [(PM, "case slices.ContainsFunc(t.events,", "case false && slices.ContainsFunc(t.events,")]),
    ("P1b-R: R1 client and real Store send no sign after cancel", go("./internal/signerclient/", "^TestRealStoreCancel$", ("TestRealStoreCancel", r"sign sent after cancel")),
     [(PM, "case slices.ContainsFunc(t.events,", "case false && slices.ContainsFunc(t.events,")]),
    ("P1b-R: R2 the wallet lock wait ends with the request", go("./internal/signer/", "^TestLockWaitHonorsContext$", ("TestLockWaitHonorsContext", r"lock wait ignored its cancellation")),
     [(SG, "\tcase <-ctx.Done():\n\t\treturn nil, ctx.Err()\n", "")]),
    ("P1b-R: R2 the DB wait ends with the request", go("./internal/signer/", "^TestHandlerDeadline$", ("TestHandlerDeadline", r"handler outlived its deadline")),
     [(SG, "return scanAttempt(c.db.QueryRowContext(ctx, `SELECT `+attemptCols+` FROM attempts WHERE request_id = ?`, id))",
       "return scanAttempt(c.db.QueryRow(`SELECT `+attemptCols+` FROM attempts WHERE request_id = ?`, id))")]),
    ("P1b-R: R3 a failed unknown save is reported", go("./internal/signerclient/", "^TestUnknownSaveFailure$", ("TestUnknownSaveFailure", r"save failure hidden")),
     [(CL, 'return Result{Attempt: a}, errors.Join(Unknown, fmt.Errorf("%w: %w", NotSaved, uerr))', "return Result{Attempt: a}, Unknown")]),
    ("P1b-R: R3 the saved unknown attempt is returned", go("./internal/signerclient/", "^TestUnknownThenResume$", ("TestUnknownThenResume", r"no Signer")),
     [(CL, "\t\t\ta = updated\n", "\t\t\t_ = updated\n")]),
    # Review of fcc5095 (R2 reply classification); run them alone with "^P1b-R2b:".
    ("P1b-R2b: Handler: an accepted attempt is not answered unknown after a cancel", go("./internal/signer/", "^TestHandlerAfterAccept$", ("TestHandlerAfterAccept/cancel", r"accepted attempt answered 404 ATTEMPT_UNKNOWN")),
     [(SG, "\ta, found, err := c.byID(ctx, id)\n\tif err != nil { // the read failed (the request's deadline, the DB): not \"unknown\"\n\t\treturn Reply{}, err\n\t}\n\tif !found {\n\t\treturn Reply{}, Refusal(\"ATTEMPT_UNKNOWN\")\n\t}",
       "\ta, found, err := c.byID(ctx, id)\n\tif err != nil || !found {\n\t\treturn Reply{}, errors.Join(err, Refusal(\"ATTEMPT_UNKNOWN\"))\n\t}")]),
    ("P1b-R2b: Handler: an accepted attempt is not answered unknown after the deadline", go("./internal/signer/", "^TestHandlerAfterAccept$", ("TestHandlerAfterAccept/deadline", r"accepted attempt answered 404 ATTEMPT_UNKNOWN")),
     [(SG, "\ta, found, err := c.byID(ctx, id)\n\tif err != nil { // the read failed (the request's deadline, the DB): not \"unknown\"\n\t\treturn Reply{}, err\n\t}\n\tif !found {\n\t\treturn Reply{}, Refusal(\"ATTEMPT_UNKNOWN\")\n\t}",
       "\ta, found, err := c.byID(ctx, id)\n\tif err != nil || !found {\n\t\treturn Reply{}, errors.Join(err, Refusal(\"ATTEMPT_UNKNOWN\"))\n\t}")]),
    ("P1b-R2b: client: an accepted attempt is recorded unknown, not refused", go("./internal/signer/", "^TestClientAfterAccept$", ("TestClientAfterAccept", r"client took an accepted attempt for never accepted")),
     [(SG, "\ta, found, err := c.byID(ctx, id)\n\tif err != nil { // the read failed (the request's deadline, the DB): not \"unknown\"\n\t\treturn Reply{}, err\n\t}\n\tif !found {\n\t\treturn Reply{}, Refusal(\"ATTEMPT_UNKNOWN\")\n\t}",
       "\ta, found, err := c.byID(ctx, id)\n\tif err != nil || !found {\n\t\treturn Reply{}, errors.Join(err, Refusal(\"ATTEMPT_UNKNOWN\"))\n\t}")]),
    ("P1b-R2b: Core: a failed read is not ATTEMPT_UNKNOWN", go("./internal/signer/", "^TestFinishReadError$", ("TestFinishReadError", r"a failed read became a refusal")),
     [(SG, "\ta, found, err := c.byID(ctx, id)\n\tif err != nil { // the read failed (the request's deadline, the DB): not \"unknown\"\n\t\treturn Reply{}, err\n\t}\n\tif !found {\n\t\treturn Reply{}, Refusal(\"ATTEMPT_UNKNOWN\")\n\t}",
       "\ta, found, err := c.byID(ctx, id)\n\tif err != nil || !found {\n\t\treturn Reply{}, errors.Join(err, Refusal(\"ATTEMPT_UNKNOWN\"))\n\t}")]),
    # A2's core protections (docs/commercial-human-approval-design.md 9, the
    # reverse list); run them alone with "^A2:".
    ("A2: the Signer compares the challenge", go("./internal/signer/", "^TestAssertionRules$", ("TestAssertionRules", r"other challenge: ")),
     [(SA, "\ts.UserVerification = protocol.VerificationRequired\n", "\ts.UserVerification = protocol.VerificationRequired\n\ts.Challenge = parsed.Response.CollectedClientData.Challenge\n")]),
    ("A2: the Signer requires UV", go("./internal/signer/", "^TestAssertionRules$", ("TestAssertionRules", r"no UV: ")),
     [(SA, "\ts.UserVerification = protocol.VerificationRequired\n", "\ts.UserVerification = protocol.VerificationDiscouraged\n")]),
    ("A2: the Signer compares the origin", go("./internal/signer/", "^TestAssertionRules$", ("TestAssertionRules", r"(127\.0\.0\.1 origin|other port): ")),
     [(SA, "RPOrigins: w.Origins,", "RPOrigins: append(w.Origins, \"http://127.0.0.1:8402\", \"http://localhost:8403\"),")]),
    ("A2: a used challenge is not used again (replay)", go("./internal/signer/", "^TestApproveAndResend$", ("TestApproveAndResend", r"replay: ")),
     [(SA, "\tcase a.state == \"used\":", "\tcase false && a.state == \"used\":"),
      (SA, "WHERE challenge_id = ? AND state = 'issued' AND ? < expires_at`, a.id, now)", "WHERE challenge_id = ? AND ? < expires_at`, a.id, now)"),
      (SA, "\t\tif state != \"issued\" || now >= expiresAt {", "\t\tif false && (state != \"issued\" || now >= expiresAt) {")]),
    ("A2: the verify commit needs the challenge still issued (close first)", go("./internal/signer/", "^TestCloseBeatsVerify$", ("TestCloseBeatsVerify", r"late verify: ")),
     [(SA, "WHERE challenge_id = ? AND state = 'issued' AND ? < expires_at`, a.id, now)", "WHERE challenge_id = ? AND ? < expires_at`, a.id, now)"),
      (SA, "\t\tif state != \"issued\" || now >= expiresAt {", "\t\tif false && (state != \"issued\" || now >= expiresAt) {")]),
    ("A2: R1 the commit answers by the challenge before the counter", go("./internal/signer/", "^TestTwoAssertionsInFlight$", ("TestTwoAssertionsInFlight", r"the late assertion: ASSERTION_COUNTER")),
     [(SA, "\t\tif state != \"issued\" || now >= expiresAt {", "\t\tif false && (state != \"issued\" || now >= expiresAt) {")]),
    ("A2: R2 flags are compared only after the signature", go("./internal/signer/", "^TestFlagsNeedTheSignature$", ("TestFlagsNeedTheSignature", r"an unsigned BE change: ASSERTION_INVALID_FLAGS")),
     [(SA, "\tvar s webauthn.SessionData\n", "\tif stored.Flags.BackupEligible != parsed.Response.AuthenticatorData.Flags.HasBackupEligible() {\n\t\treturn \"ASSERTION_INVALID_FLAGS:suspect\", stored, nil\n\t}\n\tvar s webauthn.SessionData\n")]),
    ("A2: Store: a simulated grant is never raised to webauthn", go("./internal/store/", "^TestApprovalSchemaChecks$", ("TestApprovalSchemaChecks", r"a simulated grant became webauthn")),
     [(STA, "  CHECK ((authorization_method = 'webauthn') = (signer_approval_id IS NOT NULL))", "  CHECK (1)")]),
    # Review N14: without the Store's ownership check the Host's comparison
    # of the Signer's reply (consumer_id) still refuses (SIGNER_MISMATCH,
    # 502; review N-d corrected the layer); the regex names that status.
    ("A2: R1 the routes keep a viewer to its consumer", go("./cmd/demo/", "^TestA2Permissions$", ("TestA2Permissions", r"B's viewer: 502, want 404")),
     [(STA, "err == nil && ss.ConsumerID != consumer {", "err == nil && false {")]),
    ("A2: R1 a viewer token cannot replace the assertion", go("./cmd/demo/", "^TestA2Permissions$", ("TestA2Permissions", r"forged: 200")),
     [(HA, "return h.onVerify(consumer, st, h.signer.Verify(signer.VerifyRequest{VerifyRequestID: verifyID, ChallengeID: b.ChallengeID, Order: st.Order, Assertion: assertion}))",
       "return h.resolve(consumer, st, signer.ApprovalReply{State: \"verified\", Decision: b.Decision, ApprovalID: \"hap-forged\", ApproverID: \"approver-a\", "
       "CredentialRef: \"sha256:0000000000000000000000000000000000000000000000000000000000000000\", IntentHash: st.IntentHash, ExpiresAt: 1 << 40})")]),
    ("A2: R2 serve-approvals answers PAYMENT_MODE_OFF on every payment route", go("./internal/signer/", "^TestApprovalsHandler$", ("TestApprovalsHandler", r"/v1/sign")),
     [(SS, "Refusal(\"PAYMENT_MODE_OFF\")", "Refusal(\"HUMAN_APPROVAL_UNAVAILABLE\")")]),
    ("A2: R3 a failed verify lookup is not not-found", go("./cmd/demo/", "^TestA2VerifyLookupFails$", ("TestA2VerifyLookupFails", r"a failed read was taken for not found")),
     [(HA, "\tcase signerclient.KindNotFound:\n\t\treturn h.closeChallenge(consumer, st, true)", "\tcase signerclient.KindNotFound, signerclient.KindUnknown:\n\t\treturn h.closeChallenge(consumer, st, true)")]),
    ("A2: the other decision gets no challenge", go("./cmd/demo/", "^TestA2TwoPagesAndChangingTheDecision$", ("TestA2TwoPagesAndChangingTheDecision", r"other decision: ")),
     [(STA, "\t\t\tcase cur.Decision != req.Decision:", "\t\t\tcase false:")]),
    ("A2: R3 a closed session's open attempt is not shared", go("./cmd/demo/", "^TestA2CancelledTask$", ("TestA2CancelledTask", r"shared after cancel: 200")),
     [(STA, "\t\t\tcase p.Session.State != \"pending\": // the Host closes it (review R3); nothing is shared", "\t\t\tcase false:"),
      (HA, "\tcase st.SessionState != \"pending\":\n\t\treturn h.closeOnSession(consumer, st)\n\tcase a.State == \"verifying\"", "\tcase false:\n\t\treturn h.closeOnSession(consumer, st)\n\tcase a.State == \"verifying\"")]),
    ("A2: R3 a closed session's assertion is not sent", go("./cmd/demo/", "^TestA2AssertionAfterCancel$", ("TestA2AssertionAfterCancel", r"assertion after cancel: 200")),
     [(HA, "\tcase a.Open() && st.SessionState != \"pending\": // nothing is sent to the Signer (review R3)", "\tcase false:")]),
    ("A2: R6 CHALLENGE_USED resolves by the challenge's approval", go("./cmd/demo/", "^TestA2ChallengeUsedElsewhere$", ("TestA2ChallengeUsedElsewhere/assertion", r"assertion: 503")),
     [(HA, "c.Kind == signerclient.KindOK && c.Reply.State == \"used\" {", "false && c.Kind == signerclient.KindOK {")]),
    ("A2: R6 a close answering used resolves", go("./cmd/demo/", "^TestA2ChallengeUsedElsewhere$", ("TestA2ChallengeUsedElsewhere/cancel", r"cancel: 502"),
                                                     ("TestA2ChallengeUsedElsewhere/status_after_a_lost_verify", r"status after a lost verify: 502")),
     [(HA, "\tcase \"used\":\n\t\treturn h.resolveUsed(consumer, st, ans.Reply.ApprovalID)", "\tcase \"used-never\":\n\t\treturn h.resolveUsed(consumer, st, ans.Reply.ApprovalID)")]),
    ("A2: S2 a used challenge of a closed session is abandoned", go("./cmd/demo/", "^TestA2CancelledAfterSignerVerified$", ("TestA2CancelledAfterSignerVerified", r"Signer approval after cancel: .*State:verified")),
     [(HA, "\t\tab := h.signer.Abandon(r.ApprovalID)\n", "\t\tab := signerclient.Answer{}\n")]),
    ("A2: the approver's version alone is checked", go("./internal/signer/", "^TestChangedAndLateBranches$", ("TestChangedAndLateBranches", r"approver version only: ")),
     [(SA, "ap.Version != a.approverVersion || c.cfg.hash != a.configHash {", "c.cfg.hash != a.configHash {")]),
    ("A2: the configuration hash alone is checked", go("./internal/signer/", "^TestChangedAndLateBranches$", ("TestChangedAndLateBranches", r"config hash only: ")),
     [(SA, "ap.Version != a.approverVersion || c.cfg.hash != a.configHash {", "ap.Version != a.approverVersion {")]),
    # C2a: the pay family parameterised by version (docs/commercial-c2-design.md 3.1); run them alone with "^C2a:".
    ("C2a: an order takes only its own version's domain", go("./internal/paycontract/", "^TestVersionsApart$", ("TestVersionsApart", r"v2 order, v1 domain: validated")),
     [(PC, "if i.Version != p.Version || i.Domain != sp.Domain {", "if i.Version != p.Version {")]),
    ("C2a: one version per event stream", go("./internal/paycontract/", "^TestEventStreamOneVersion$", ("TestEventStreamOneVersion", r"a mixed stream: <nil>")),
     [(PE, "\t\tif e.Version != st.version {", "\t\tif false && e.Version != st.version {")]),
    ("C2a: the credential reasons are v2's only", go("./internal/paycontract/", "^TestDecisionReasonsByVersion$", ("TestDecisionReasonsByVersion", r"apa.pay.v1 CREDENTIAL_REVOKED: <nil>")),
     [(PC, "slices.Contains(sp.deny, d.Reason)", "slices.Contains(append(slices.Clone(sp.deny), specs[V2].deny...), d.Reason)")]),
    ("C2a: the Signer derives the session by the order's version", go("./internal/signer/", "^TestV2OrderChallenge$", ("TestV2OrderChallenge", r"v1 session ID for a v2 order: <nil>")),
     [(SA, "if r.ApprovalSessionID != sp.SessionID(i.TaskID, i.PurchaseOrderID) {", "if r.ApprovalSessionID != paycontract.SessionID(i.TaskID, i.PurchaseOrderID) && r.ApprovalSessionID != sp.SessionID(i.TaskID, i.PurchaseOrderID) {")]),
    # C2a review should-fixes.
    ("C2a: an event's order is of the event's version", go("./internal/paycontract/", "^TestEventCrossVersion$", ("TestEventCrossVersion", r"a v1 event took a v2 order")),
     [(PE, "order.Version != e.Version || ", "")]),
    ("C2a: an event's decision is of the event's version", go("./internal/paycontract/", "^TestEventCrossVersion$", ("TestEventCrossVersion", r"a v1 event took a v2 decision")),
     [(PE, "d.Version != e.Version || ", "")]),
    ("C2a: a grant event's ID is its version's", go("./internal/paycontract/", "^TestEventCrossVersion$", ("TestEventCrossVersion", r"a v2 grant event took v1's grant ID")),
     [(PE, "if p.GrantID != sp.GrantID(p.ApprovalSessionID) {", "if p.GrantID != GrantID(p.ApprovalSessionID) && sp.Domain != \"\" {")]),
    ("C2a: the Signer derives the jti by the order's version", go("./internal/signer/", "^TestV2OrderChallenge$", ("TestV2OrderChallenge", r"stored jti")),
     [(SA, "JTI: sp.GrantID(r.ApprovalSessionID),", "JTI: paycontract.GrantID(r.ApprovalSessionID),")]),
    ("C2a: a stored v1 delegation stays v1", go("./internal/store/", "^TestUnboundDelegationVersion$", ("TestUnboundDelegationVersion", r"Open: <nil>")),
     [(SPY, "id != specs(d.Version).DelegationID(req)", "id != paycontract.DelegationID(req)")]),
    ("C2a: a purchase is decided only in a task of its version", go("./internal/paypolicy/", "^TestSameFieldsBothEntries$", ("TestSameFieldsBothEntries", r"a v2 purchase in a v1 task: <nil>")),
     [(PP, "!paycontract.Supported(in.ContractVersion) || in.Purchase.Version != in.ContractVersion {", "!paycontract.Supported(in.ContractVersion) {")]),
    ("C2a: one request ID never names orders of two versions", go("./internal/paycontract/", "^TestCompareRetryAcrossVersions$", ("TestCompareRetryAcrossVersions", r"v1 and v2 orders of one request: <nil>")),
     [(PC, "if existing.Version != incoming.Version {", "if false {")]),
    # C2b: the BBS+ credential in apa.pay.v2 (docs/commercial-c2-design.md); run them alone with "^C2b:".
    ("C2b: only v2 withdraws an Ask for a credential reason", go("./internal/paycontract/", "^TestAskClosuresByVersion$", ("TestAskClosuresByVersion", r"apa.pay.v1 withdrawn CREDENTIAL_REVOKED: <nil>")),
     [(PV, "Version: {Version, Domain, ReasonCodes, denyReasons, askV1},", "Version: {Version, Domain, ReasonCodes, denyReasons, askV2},")]),
    ("C2b: the proof's revealed set and count are the request's", go("./internal/credential/", "^TestRefusals$", ("TestRefusals/subset_proof_with_forged_holder_and_status", r"subset proof with forged holder and status: invalid/SNAPSHOT_MISMATCH")),
     [(CR, "if pl, err := bbs.ParsePoKPayload(p.Proof); err != nil || pl.MessagesCount != len(Fields) || !slices.Equal(pl.Revealed, idx) {",
       "if pl, err := bbs.ParsePoKPayload(p.Proof); false && (err != nil || pl.MessagesCount != len(Fields) || !slices.Equal(pl.Revealed, idx)) {")]),
    ("C2b: disclosed values are the task's snapshot", go("./internal/credential/", "^TestRefusals$", ("TestRefusals/another_credential_of_the_task", r"another credential of the task: valid/")),
     [(CR, 'if d["task_id"] != e.TaskID || d["expiry"] != e.Expiry || d["status"] != e.Status {', "if false {")]),
    ("C2b: the holder is the consumer's registered key", go("./internal/credential/", "^TestRefusals$", ("TestRefusals/not_the_registered_holder", r"not the registered holder: valid/")),
     [(CR, 'case d["holder"] != e.Holder:', "case false:")]),
    ("C2b: a revoked status is refused", go("./internal/credential/", "^TestRefusals$", ("TestRefusals/revoked", r"revoked: valid/")),
     [(CR, 'case slices.Contains(st.List.Revoked, d["status"]):', "case false:")]),
    ("C2b: a stale status list is not read", go("./internal/credential/", "^TestStatusListWriter$", ("TestStatusListWriter", r"read a stale list")),
     [(CD, "case now >= l.NextUpdate:", "case false:")]),
    ("C2b: a missing counter refuses to issue", go("./internal/credential/", "^TestCounterRefuses$", ("TestCounterRefuses", r"issued from a missing counter")),
     [(CD, 'return fmt.Errorf("status counter: refuse to issue: %w", err)', 'raw = []byte("0")\n\t\t\t_ = err')]),
    ("C2b: v1 refuses the credential payload field", go("./internal/paycontract/", "^TestCredentialPayload$", ("TestCredentialPayload", r"a v1 event took a credential field")),
     [(PE, '"credential": p.Credential != nil}', '"credential": p.Credential != nil && e.Version == V2}')]),
    ("C2b: a v2 decision follows its credential result", go("./internal/paycontract/", "^TestCredentialVerifiedOrder$", ("TestCredentialVerifiedOrder", r"invalid, Ask: <nil>")),
     [(PE, 'case deny && (d.PolicyVerdict != "Deny" || d.Reason != want):', 'case false && deny && (d.PolicyVerdict != "Deny" || d.Reason != want):')]),
    ("C2b: a v2 decision needs its verification first", go("./internal/paycontract/", "^TestCredentialVerifiedOrder$", ("TestCredentialVerifiedOrder", r"no verification: <nil>")),
     [(PE, '\t\t\tcase r == "":', '\t\t\tcase false && r == "":')]),
    ("C2b: a v2 task never takes the fixture credential", go("./internal/store/", "^TestStartV2Refuses$", ("TestStartV2Refuses/a_fixture_credential", r"a fixture credential: <nil>")),
     [(SOP, "if v2 || c.ConsumerID != task.ConsumerID", "if c.ConsumerID != task.ConsumerID")]),
    ("C2b: SubmitV2 uses the Store's challenge, not the presentation's", go("./internal/store/", "^TestSubmitV2Denies$", ("TestSubmitV2Denies/a_presentation_for_another_challenge", r"verification invalid/PRESENTATION_FORMAT")),
     [(SCR, "case pr.Presentation.ChallengeID != ch.ID:", "case false && pr.Presentation.ChallengeID != ch.ID:")]),
    ("C2b: an expired challenge decides nothing valid", go("./internal/store/", "^TestSubmitV2Denies$", ("TestSubmitV2Denies/challenge_expired", r"challenge expired: \{.*PolicyVerdict:Allow")),
     [(SCR, "case t.now >= ch.ExpiresAt:", "case false && t.now >= ch.ExpiresAt:")]),
    ("C2b: an older status list than one seen is not used", go("./internal/store/", "^TestSubmitV2StatusRollback$", ("TestSubmitV2StatusRollback", r"rolled back: \{")),
     [(SCR, "case list.ListVersion < max || list.ListVersion == max && sum != seen:", "case false && (list.ListVersion < max || list.ListVersion == max && sum != seen):")]),
    ("C2b: recovery holds a record to its event", go("./internal/store/", "^TestV2CredentialRecovery$", ("TestV2CredentialRecovery/a_record's_result", r"Open: <nil>")),
     [(SCR, "case !ok || e != c:", "case !ok || false && e != c:")]),
    ("C2b: a simulated approval rechecks the credential", go("./internal/store/", "^TestV2ApprovalRecheck$", ("TestV2ApprovalRecheck/revoked", r"revoked: \{Outcome:approved")),
     [(SPY, '} else if reason != "" { // design 7.1: withdrawn, no grant', '} else if false && reason != "" { // design 7.1: withdrawn, no grant')]),
    ("C2b: a webauthn approval rechecks the credential", go("./internal/store/", "^TestV2WebAuthnRecheck$", ("TestV2WebAuthnRecheck", r"\{Outcome:approved")),
     [(STA, 'if reason != "" { // design 7.1: withdrawn, no grant', 'if false && reason != "" { // design 7.1: withdrawn, no grant')]),
    ("C2b: a payment rechecks the credential", go("./internal/store/", "^TestV2PaymentRecheck$", ("TestV2PaymentRecheck", r"revoked: <nil>")),
     [(PM, '} else if reason != "" {', '} else if false && reason != "" {')]),
    ("C2b: a corrupt stored credential is holder material, not a retry", go("./internal/credential/", "^TestPresentHolderMaterial$", ("TestPresentHolderMaterial/a_corrupt_signature", r"a corrupt signature: ")),
     [(CD, "pk); err != nil { // a corrupt credential, not a failed proof", "pk); false && err != nil { // a corrupt credential, not a failed proof")]),
    ("C2b: an M1 view is what it always was", go("./internal/store/", "^TestDisplayHistories$", ("TestDisplayHistories", r"is not what the Store writes")),
     [(SOP, "case !t.pay() || e.Scope == nil || e.Scope.PurchaseOrderID != k.PO:", "case false && !t.pay() || e.Scope == nil || e.Scope.PurchaseOrderID != k.PO:")]),
    ("C2b: the Shop verifies a v2 purchase, never submits it plainly", go("./cmd/demo/", "^TestC2MainLine$", ("TestC2MainLine", r"events TASK_STARTED,DELEGATION_BOUND, want")),
     [("internal/toolbridge/shop.go", "if task.ContractVersion == paycontract.V2 {", "if false && task.ContractVersion == paycontract.V2 {")]),
    ("C2b: a v2 catalog needs the credential directory", go("./cmd/demo/", "^TestC2NeedsCredentialDir$", ("TestC2NeedsCredentialDir", r"no credential directory: ")),
     [("cmd/demo/main.go", "if sc.version() == paycontract.V2 && creds == nil {", "if false && sc.version() == paycontract.V2 && creds == nil {")]),
    ("C2b: web: a credential withdrawal is not the task's cancellation", node("web/test/c2.test.mjs", "withdrawn for a credential reason is the credential's; HOST_CANCELLED keeps 'task 取消'", r"CREDENTIAL_REVOKED"),
     [(AD, 'const withdrawnText = (p) => CREDENTIAL_CLOSED[p && p.closed_reason] || "已撤回（task 取消）";', 'const withdrawnText = (p) => false && CREDENTIAL_CLOSED[p && p.closed_reason] || "已撤回（task 取消）";')]),
    ("C2b: ui: the approval panel says the credential closed it", ui("scripts/verify/c2-ui.mjs", "核准此筆（模擬） after the revocation: 憑證已撤銷，這筆已關閉, never the task's cancellation"),
     [("web/host.js", 'const closedResult = (table, state, p) => (state === "withdrawn" && A.CREDENTIAL_CLOSED[p.closed_reason]', 'const closedResult = (table, state, p) => (false && state === "withdrawn" && A.CREDENTIAL_CLOSED[p.closed_reason]')]),
    ("C2b: a v2 credential denies before the rules", go("./internal/paypolicy/", "^TestV2CredentialStep$", ("TestV2CredentialStep", r"revoked: \{")),
     [(PP, 'case credentialDeny != "":', 'case false && credentialDeny != "":')]),
    # D4 of the OS isolation plan (keygen); run them alone with "^P1b-D4:".
    ("P1b-D4: keygen never replaces a key file", go("./cmd/apa-signer/", "^TestKeygen$", ("TestKeygen", r"an existing key file was replaced")),
     [(AS, "if _, err := os.Lstat(path); !errors.Is(err, os.ErrNotExist) {", "if _, err := os.Lstat(path); false && !errors.Is(err, os.ErrNotExist) {"),
      (AS, "os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)", "os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0o600)")]),
    ("P1b-D4: keygen needs the same passphrase twice", go("./cmd/apa-signer/", "^TestKeygenRefuses$", ("TestKeygenRefuses/passphrases_differ", r"passphrases differ: accepted")),
     [(AS, "case !bytes.Equal(pass, again):", "case false && !bytes.Equal(pass, again):")]),
    ("P1b-D4: keygen writes the key file 0600", go("./cmd/apa-signer/", "^TestKeygen$", ("TestKeygen", r"key file mode")),
     [(AS, "os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)", "os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o644)")]),
]

ITEM_TIMEOUT = 900


def command(t):
    if t["kind"] == "node":
        return ["node", "--test", t["path"]]
    if t["kind"] == "ui":
        # The clone has no Unity build (ignored by git): the repository's own.
        build = os.path.join(os.path.dirname(os.path.dirname(here)), "unity/AgentPaymentAuthority/Build/WebGL/Build")
        return ["node", t["script"], "--unity-build", build, "--out", "rev-ui-out"]
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
    if t["kind"] == "ui":
        if re.search(r"^FAIL the flow ran to its end", text, re.M):
            return False, "the flow did not run to its end"
        if re.search(r"^FAIL " + re.escape(test) + r"(  \[|$)", text, re.M):
            return True, "FAIL " + test
        return False, f"the expected check did not fail: {test}"
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
    if t["kind"] == "ui":
        return re.search(r"^result: PASS$", text, re.M) is not None
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
                    shutil.copytree(os.path.join(root, u, d), os.path.join(copy, u, d), symlinks=True, dirs_exist_ok=True, copy_function=shutil.copy)
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
