#!/usr/bin/env python3
"""Mutation check for the B1-02 and B1-03 regression tests.

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
    # B1-03: the shop never delivers, states only configured URLs, and the
    # x402 codec fails closed. Tests run in the mutated file's own package.
    ("B1-03 content served for a well-formed signature", "internal/merchant/merchant.go",
     "\tw.WriteHeader(status)\n\tjson.NewEncoder(w).Encode(body)",
     "\tif body.Error == \"M1_PAYMENT_UNAVAILABLE\" {\n\t\tw.WriteHeader(200)\n\t\tw.Write([]byte(o.content))\n\t\treturn\n\t}\n\tw.WriteHeader(status)\n\tjson.NewEncoder(w).Encode(body)",
     "TestPaymentSignatureNeverDelivers"),
    ("B1-03 resource URL taken from the Host header", "internal/merchant/merchant.go",
     "\tpr := o.required\n", "\tpr := o.required\n\tpr.Resource.URL = \"http://\" + r.Host + o.Path + \"?\" + o.Query\n", "TestPaymentRequired"),
    ("B1-03 any accepted requirement allowed", "internal/merchant/merchant.go",
     "if !bytes.Equal(p.Accepted, o.accepts) {", "if false && !bytes.Equal(p.Accepted, o.accepts) {", "TestPaymentSignatureNeverDelivers"),
    ("B1-03 payload for another resource allowed", "internal/merchant/merchant.go",
     "p.Resource != nil && p.Resource.URL != o.url", "false", "TestPaymentSignatureNeverDelivers"),
    ("B1-03 query not matched", "internal/merchant/merchant.go",
     'o, ok := m.offers[r.URL.Path+"?"+r.URL.RawQuery]', 'o, ok := m.offers[r.URL.Path+"?edition=1"]', "TestWrongResourceIsNotFound"),
    ("B1-03 payload version not checked", "internal/merchant/merchant.go",
     "if p.X402Version != x402.Version {", "if false {", "TestPaymentSignatureNeverDelivers"),
    ("B1-03 non-object payload allowed", "internal/merchant/merchant.go",
     "if p.Payload[0] != '{' {", "if false {", "TestPaymentSignatureNeverDelivers"),
    ("B1-03 non-object extensions allowed", "internal/merchant/merchant.go",
     "if p.Extensions != nil && p.Extensions[0] != '{' {", "if false {", "TestPaymentPayloadExtensions"),
    ("B1-03 repeated PAYMENT-SIGNATURE allowed", "internal/merchant/merchant.go",
     "if len(values) != 1 {", "if len(values) == 0 {", "TestPaymentSignatureNeverDelivers"),
    ("B1-03 fixture price drifts from its golden and display", "internal/merchant/merchant.go",
     'Amount:            "500000",', 'Amount:            "400000",', "TestCatalog|TestPaymentRequired|TestFixtureMatchesRegistry"),
    ("B1-03 lenient base64", "internal/x402/x402.go",
     "base64.StdEncoding.Strict().DecodeString(header)", "base64.StdEncoding.DecodeString(header)", "TestDecodeFailsClosed"),
    ("B1-03 extensions dropped from the quote", "internal/x402/x402.go",
     "Extensions: ext}", "Extensions: json.RawMessage(`{}`)}", "TestQuote"),
    ("B1-03 offer version not checked", "internal/x402/x402.go",
     "if p.X402Version != Version {", "if false {", "TestQuote"),
    # B1-05 node 1: deterministic Policy rules, precedence, boundaries and refusals.
    ("B1-05 missing credential not treated as missing config", "internal/policy/policy.go",
     "case p == nil || in.Credential == nil:", "case p == nil:", "TestDenyPrecedence"),
    ("B1-05 asset not checked", "internal/policy/policy.go",
     "case !trusted || asset.AssetID != p.AssetID:", "case false && (!trusted || asset.AssetID != p.AssetID):", "TestAcceptedPolicyCases"),
    ("B1-05 trusted asset outside the profile allowed", "internal/policy/policy.go",
     "case !trusted || asset.AssetID != p.AssetID:", "case !trusted || false && asset.AssetID != p.AssetID:", "TestBoundaries"),
    ("B1-05 merchant not checked", "internal/policy/policy.go",
     "case !slices.Contains(p.Merchants, i.PayTo):", "case false:", "TestAcceptedPolicyCases"),
    ("B1-05 credential expiry not checked", "internal/policy/policy.go",
     "case in.Now >= in.Credential.ValidUntil || ", "case ", "TestAcceptedPolicyCases"),
    ("B1-05 credential expiry off by one", "internal/policy/policy.go",
     "in.Now >= in.Credential.ValidUntil", "in.Now > in.Credential.ValidUntil", "TestBoundaries"),
    ("B1-05 intent window not checked", "internal/policy/policy.go",
     " || in.Now <= i.ValidAfter || in.Now >= i.ValidBefore:", ":", "TestBoundaries"),
    ("B1-05 budget not checked", "internal/policy/policy.go",
     "case amount > remaining:", "case false:", "TestAcceptedPolicyCases"),
    ("B1-05 budget off by one", "internal/policy/policy.go",
     "case amount > remaining:", "case amount >= remaining:", "TestBoundaries"),
    ("B1-05 Allow ceiling off by one", "internal/policy/policy.go",
     "case amount <= allowMax:", "case amount < allowMax:", "TestBoundaries"),
    ("B1-05 Allow decided before the budget check", "internal/policy/policy.go",
     'case amount > remaining:\n\t\td.Reason = "INSUFFICIENT_BUDGET"\n\tcase amount <= allowMax:\n\t\td.PolicyVerdict, d.Reason = "Allow", "PAYMENT_CAPABILITY_UNAVAILABLE"', 'case amount <= allowMax:\n\t\td.PolicyVerdict, d.Reason = "Allow", "PAYMENT_CAPABILITY_UNAVAILABLE"\n\tcase amount > remaining:\n\t\td.Reason = "INSUFFICIENT_BUDGET"', "TestBoundaries"),
    ("B1-05 unattended profile asks", "internal/policy/policy.go",
     'case p.Mode == "unattended":', "case false:", "TestAcceptedPolicyCases"),
    ("B1-05 ended task still decided", "internal/policy/policy.go",
     "if err := contracts.CheckTaskActive(in.TaskStatus); err != nil {", "if err := contracts.CheckTaskActive(in.TaskStatus); false && err != nil {", "TestRefusalsWithoutDecision"),
    ("B1-05 ownership not checked", "internal/policy/policy.go",
     "if err := reg.ValidatePurchase(in.Session, in.Purchase); err != nil {", "if err := in.Purchase.Validate(); err != nil {", "TestRefusalsWithoutDecision"),
    ("B1-05 budget of another task used", "internal/policy/policy.go",
     "if in.Budget.TaskID != in.Session.TaskID {", "if false {", "TestRefusalsWithoutDecision"),
    ("B1-05 credential of another consumer used", "internal/policy/policy.go",
     "c.ConsumerID != in.Session.ConsumerID || ", "", "TestRefusalsWithoutDecision"),
    ("B1-05 remaining above the task cap accepted", "internal/policy/policy.go",
     "remaining > limit {", "remaining > 2*limit {", "TestRefusalsWithoutDecision"),
    ("B1-05 profile allow_max above task cap accepted", "internal/policy/policy.go",
     " || allow > limit:", " || false && allow > limit:", "TestLoadProfile"),
    # B1-05 node 2: the Store's transaction skeleton, replay and offer book,
    # expiry precedence, projection, refusals that echo nothing, and recovery.
    ("B1-05 store: clock sampled before the lock", "internal/store/store.go",
     '\tif err := s.at("before-begin"); err != nil {\n\t\treturn err\n\t}\n\ttx, err := s.w.Begin()\n\tif err != nil {\n\t\treturn err\n\t}\n\tdefer tx.Rollback()\n\tnow := s.clock()', '\tnow := s.clock()\n\tif err := s.at("before-begin"); err != nil {\n\t\treturn err\n\t}\n\ttx, err := s.w.Begin()\n\tif err != nil {\n\t\treturn err\n\t}\n\tdefer tx.Rollback()', "TestClockSampledAfterLock"),
    ("B1-05 store: clock regression allowed", "internal/store/store.go",
     "if high.Valid && now < uint64(high.Int64) {", "if false && high.Valid && now < uint64(high.Int64) {", "TestClockRegression"),
    ("B1-05 store: recovery skipped at Open", "internal/store/store.go",
     "if err := s.verify(); err != nil {", "if err := error(nil); err != nil {", "TestRecoveryRefusesCorruption"),
    ("B1-05 store: Open serves before expiring", "internal/store/store.go",
     "if err := s.ExpireDue(); err != nil {", "if err := error(nil); err != nil {", "TestRestartAcrossDeadline"),
    ("B1-05 store: replay judged against the current offer book", "internal/store/ops.go",
     'case kind == "replay":', 'case kind == "replay" && s.offered(po):', "TestReplayIgnoresCurrentOfferBook"),
    ("B1-05 store: replay decided again", "internal/store/ops.go",
     'case kind == "replay":', 'case false && kind == "replay":', "TestReplaySequentialAndConcurrent"),
    ("B1-05 store: new purchase skips the offer book", "internal/store/ops.go",
     "if !s.offered(po) {", "if false {", "TestQuoteSafety|TestReplayIgnoresCurrentOfferBook"),
    ("B1-05 store: ownership not checked before replay", "internal/store/ops.go",
     "if err := t.reg.ValidatePurchase(sess, po); err != nil {", "if err := t.reg.ValidatePurchase(sess, po); false && err != nil {", "TestReplayAfterTaskEnded|TestReplayIgnoresCurrentOfferBook"),
    ("B1-05 store: queries ignore task ownership", "internal/store/ops.go",
     't, err := s.openTask(tx, 0, sess.TaskID)\n\tif Code(err) == "UNKNOWN_TASK" || err == nil && (t.row.ConsumerID != sess.ConsumerID || t.row.AgentID != sess.AgentID) {', 't, err := s.openTask(tx, 0, sess.TaskID)\n\tif Code(err) == "UNKNOWN_TASK" || err == nil && false {', "TestIsolationAB"),
    ("B1-05 store: Submit skips its own expiry", "internal/store/ops.go",
     "\t\tif err := t.expire(); err != nil {\n\t\t\treturn err\n\t\t}\n\t\tkey, err := po.RetryKey()", "\t\tkey, err := po.RetryKey()", "TestExpiryAroundReplayAndConflict|TestClockSampledAfterLock"),
    ("B1-05 store: deadline off by one", "internal/store/ops.go",
     "t.now-p.DecidedAt >= t.terms.ApprovalDeadlineSeconds", "t.now-p.DecidedAt > t.terms.ApprovalDeadlineSeconds", "TestAskDeadline|TestAcceptedIntegrationCases"),
    ("B1-05 store: expiry recorded as a withdrawal", "internal/store/ops.go",
     't.close(p, "APPROVAL_EXPIRED", "expired", "APPROVAL_TIMEOUT")', 't.close(p, "APPROVAL_WITHDRAWN", "withdrawn", "HOST_CANCELLED")', "TestAskDeadline|TestAcceptedIntegrationCases"),
    ("B1-05 store: cancel withdraws before expiring", "internal/store/ops.go",
     "\t\tif err := t.expire(); err != nil {\n\t\t\treturn err\n\t\t}\n\t\tif withdraw {", "\t\tif withdraw {", "TestCloseAtDeadline"),
    ("B1-05 store: CLI timeout withdraws Asks", "internal/store/ops.go",
     's.end(taskID, "TASK_TIMED_OUT", "CLI_TIMEOUT", false)', 's.end(taskID, "TASK_TIMED_OUT", "CLI_TIMEOUT", true)', "TestAcceptedIntegrationCases|TestCLITimeoutThenExpiry"),
    ("B1-05 store: a later lifecycle event replaces the Deny", "internal/store/ops.go",
     'return "denied", e.Payload.Decision.Reason', 'status, denyReason = "denied", e.Payload.Decision.Reason', "TestDenyThenEnd|TestProjectionInterleavings"),
    ("B1-05 store: an expired Ask keeps the task awaiting approval", "internal/store/ops.go",
     'case "APPROVAL_EXPIRED", "APPROVAL_WITHDRAWN":\n\t\t\tdelete(open, po)', 'case "APPROVAL_WITHDRAWN":\n\t\t\tdelete(open, po)', "TestProjectionInterleavings"),
    ("B1-05 store: a moved wallet is followed", "internal/store/ops.go",
     "if err == nil && (prevWallet != wallet.WalletID || prevPayer != wallet.From) {", "if false && err == nil && (prevWallet != wallet.WalletID || prevPayer != wallet.From) {", "TestWalletBindingKept"),
    ("B1-05 store: a missing profile is inferred from nil", "internal/store/ops.go",
     "if (setup.Profile == nil) != setup.ProfileAbsent || (setup.Credential == nil) != setup.CredentialAbsent {", "if false {", "TestStartTask"),
    ("B1-05 store: contract refusal echoes its detail", "internal/store/ops.go",
     'return refuse(code, "refused by a contract check")', "return refuse(code, err.Error())", "TestQuoteSafety"),
    ("B1-05 store: decode refusal echoes the input", "internal/store/ops.go",
     'return Result{}, refuse("INVALID_PURCHASE", "purchase does not decode as an M1 purchase order")', 'return Result{}, refuse("INVALID_PURCHASE", err.Error())', "TestQuoteSafety"),
    ("B1-05 store: foreign_key_check skipped", "internal/store/verify.go",
     "violated := fk.Next()", "violated := fk.Next() && false", "TestRecoveryRefusesCorruption"),
    ("B1-05 store: budget not verified", "internal/store/verify.go",
     "if row.BudgetRemaining != string(tm.BudgetInitial) {", "if false {", "TestRecoveryRefusesCorruption"),
    ("B1-05 store: decision columns not verified", "internal/store/verify.go",
     "if !ok || *d.Payload.Decision != p.Decision || d.ObservedAt != p.DecidedAt {", "if !ok || false && (*d.Payload.Decision != p.Decision || d.ObservedAt != p.DecidedAt) {", "TestRecoveryRefusesCorruption"),
    ("B1-05 store: projection not verified", "internal/store/verify.go",
     'if status != row.Status || reason != row.DenyReason.String || (reason == "") == row.DenyReason.Valid {', 'if false && (status != row.Status || reason != row.DenyReason.String || (reason == "") == row.DenyReason.Valid) {', "TestRecoveryRefusesCorruption"),
    ("B1-05 store: chain head not verified", "internal/store/verify.go",
     "if row.ChainHead != head || row.NextSeq != last.Sequence+1 {", "if false && (row.ChainHead != head || row.NextSeq != last.Sequence+1) {", "TestRecoveryRefusesCorruption"),
    ("B1-05 store: stored event hash not verified", "internal/store/verify.go",
     "herr != nil || eh != hash ||", "herr != nil || false && eh != hash ||", "TestRecoveryRefusesCorruption"),
    # B1-05 node 2 fixes: REV1 read errors during recovery and task loads,
    # REV2 wallet and payer ownership across consumers at start and recovery.
    ("B1-05 store REV1: drained ignores Rows.Err", "internal/store/store.go",
     "func drained(rows *sql.Rows) error { return errors.Join(rows.Err(), rows.Close()) }", "func drained(rows *sql.Rows) error { return rows.Close() }", "TestRecoveryReadFailures"),
    ("B1-05 store REV1: foreign_key_check read error ignored", "internal/store/verify.go",
     "if err := drained(fk); err != nil {", "if err := fk.Close(); err != nil {", "TestRecoveryReadFailures"),
    ("B1-05 store REV1: snapshot scan read error ignored", "internal/store/verify.go",
     "if err := drained(snaps); err != nil {", "if err := snaps.Close(); err != nil {", "TestRecoveryReadFailures"),
    ("B1-05 store REV1: task scan read error ignored", "internal/store/verify.go",
     'if err := drained(rows); err != nil {\n\t\treturn readErr("task", "", err)', 'if err := rows.Close(); err != nil {\n\t\treturn readErr("task", "", err)', "TestRecoveryReadFailures"),
    ("B1-05 store REV1: purchase scan read error ignored", "internal/store/verify.go",
     'if err := drained(prow); err != nil {\n\t\t\treturn readErr("purchase", id, err)', 'if err := prow.Close(); err != nil {\n\t\t\treturn readErr("purchase", id, err)', "TestRecoveryReadFailures"),
    ("B1-05 store REV1: event scan read error ignored", "internal/store/verify.go",
     "if err := drained(erow); err != nil {", "if err := erow.Close(); err != nil {", "TestRecoveryReadFailures"),
    ("B1-05 store REV1: task-load event read error ignored", "internal/store/ops.go",
     'if err := drained(rows); err != nil {\n\t\treturn nil, readErr("task", taskID, err)', 'if err := rows.Close(); err != nil {\n\t\treturn nil, readErr("task", taskID, err)', "TestRecoveryReadFailures"),
    ("B1-05 store REV1: task-load purchase read error ignored", "internal/store/ops.go",
     'if err := drained(prow); err != nil {\n\t\treturn nil, readErr("task", taskID, err)', 'if err := prow.Close(); err != nil {\n\t\treturn nil, readErr("task", taskID, err)', "TestRecoveryReadFailures"),
    ("B1-05 store REV2: payer of another consumer accepted at start", "internal/store/ops.go",
     '\t\t\t{"payer", wallet.From, "PAYER_IN_USE"},\n', "", "TestWalletOwnership"),
    ("B1-05 store REV2: wallet_id of another consumer accepted at start", "internal/store/ops.go",
     '\t\t\t{"wallet_id", wallet.WalletID, "WALLET_ID_IN_USE"},\n', "", "TestWalletOwnership"),
    ("B1-05 store REV2: shared wallet_id accepted at recovery", "internal/store/verify.go",
     "if owner, seen := walletOwner[row.WalletID]; seen && owner != row.ConsumerID {", "if owner, seen := walletOwner[row.WalletID]; false && seen && owner != row.ConsumerID {", "TestWalletOwnership"),
    ("B1-05 store REV2: shared payer accepted at recovery", "internal/store/verify.go",
     "if owner, seen := payerOwner[row.Payer]; seen && owner != row.ConsumerID {", "if owner, seen := payerOwner[row.Payer]; false && seen && owner != row.ConsumerID {", "TestWalletOwnership"),
]


def pkg(rel):
    """The package whose tests must kill a mutation of rel. Fixture files
    (testdata/contracts/) have no package; the contracts tests read them."""
    if not rel.endswith(".go"):
        return "./internal/contracts/"
    return "./" + str(pathlib.PurePosixPath(rel).parent) + "/"


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
        rc, log = run(["go", "test", *sorted({pkg(m[1]) for m in MUTATIONS}), "-count=1"], tmp)
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
            rc, log = run(["go", "test", pkg(rel), "-run", test, "-count=1"], tmp)
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
