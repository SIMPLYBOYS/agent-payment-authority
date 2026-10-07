#!/usr/bin/env python3
"""Mutation check for the B1-02 to B1-05 regression tests.

For one commit, export it with git archive into a throwaway directory per
mutation, re-introduce one review finding with an exact text replacement, and
require the finding's regression test to FAIL as a test failure ("--- FAIL"),
not a build error. The unmodified export must pass.

Usage: python3 scripts/verify/mutation-check.py [--root REPO] [--out DIR] [commit]
Writes <out>/<commit12>-<UTC time>/ (never reused). Exit 0 only if the
baseline passes and every mutation is killed.
"""
import argparse
import concurrent.futures
import datetime
import os
import pathlib
import shutil
import subprocess
import sys
import tempfile
import time

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
     "if err := s.verify(schemaVersion); err != nil {\n\t\ts.Close()", "if err := error(nil); err != nil {\n\t\ts.Close()", "TestRecoveryRefusesCorruption"),
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
    # B1-04 first delivery: 402 checks, purchase identity, relay and MCP,
    # process limits and cleanup, Store endings, trace hygiene, fake-only.
    ("B1-04 bridge: 402 resource.url not compared", "internal/toolbridge/shop.go",
     "pr.Resource.URL != p.URL || ", "", "Test402Tampering"),
    ("B1-04 bridge: several 402 offers accepted", "internal/toolbridge/shop.go",
     " || len(pr.Accepts) != 1 {", " {", "Test402Tampering"),
    ("B1-04 bridge: redirects followed", "internal/toolbridge/shop.go",
     "\t\tCheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse },\n", "", "Test402Tampering"),
    ("B1-04 bridge: 402 status not checked", "internal/toolbridge/shop.go",
     "if resp.StatusCode != http.StatusPaymentRequired || len(headers) != 1 {", "if len(headers) != 1 {", "Test402Tampering"),
    ("B1-04 bridge: repeated PAYMENT-REQUIRED accepted", "internal/toolbridge/shop.go",
     "if resp.StatusCode != http.StatusPaymentRequired || len(headers) != 1 {", "if resp.StatusCode != http.StatusPaymentRequired || len(headers) == 0 {", "Test402Tampering"),
    ("B1-04 bridge: oversized body accepted", "internal/toolbridge/shop.go",
     "if err != nil || len(body) > MaxBodyBytes {", "if err != nil {", "Test402Tampering"),
    ("B1-04 bridge: catalog entries not matched to trusted URL and method", "internal/toolbridge/shop.go",
     "if !ok || e.HTTPMethod != http.MethodGet || e.ResourceURL != p.URL {", "if !ok {", "TestCatalogFilteredAndCleaned"),
    ("B1-04 bridge: catalog text not cleaned", "internal/toolbridge/shop.go",
     "Title: Clean(e.Title, 200)", "Title: e.Title", "TestCatalogFilteredAndCleaned"),
    ("B1-04 bridge: stored purchase not looked up before submitting", "internal/toolbridge/shop.go",
     "if v, ok := s.stored(b, po); ok {", "if v, ok := s.stored(b, po); false && ok {", "TestPurchaseIntegrity"),
    ("B1-04 bridge: nonce not derived from task and product only", "internal/toolbridge/shop.go",
     "\"\\x00\" + p.ID))", "\"\\x00\" + p.ID + time.Now().String()))", "TestPurchaseIntegrity"),
    ("B1-04 relay: revoke leaves the run active", "internal/toolbridge/relay.go",
     "\tif b != nil {\n\t\tb.end()\n\t}\n", "\tif false && b != nil {\n\t\tb.end()\n\t}\n", "TestCommitGate|TestRevokeWithCallInFlight"),
    ("B1-04 relay: no call budget", "internal/toolbridge/relay.go",
     "if n > MaxCalls {", "if false {", "TestRelayIdentity"),
    ("B1-04 relay: unknown tool name passed on", "internal/toolbridge/relay.go",
     "\t\tname = \"unknown\"\n", "", "TestRelayIdentity"),
    ("B1-04 tools: extra argument members accepted", "internal/toolbridge/tools.go",
     "if len(args) != 1 || json.Unmarshal", "if json.Unmarshal", "TestParseArgs"),
    ("B1-04 mcp: tools served before initialize", "internal/toolbridge/mcp.go",
     "case !initialized:", "case false && !initialized:", "TestServeProtocol"),
    ("B1-04 mcp: unknown tool relayed", "internal/toolbridge/mcp.go",
     "if json.Unmarshal(req.Params, &p) != nil || !known(p.Name) {", "if json.Unmarshal(req.Params, &p) != nil {", "TestServeProtocol"),
    ("B1-04 runner: group not killed after the CLI exits", "internal/buyer/runner.go",
     "\t\t\tgroup(syscall.SIGKILL)\n\t\t\tabandon = time.After(s.Limits.Grace)", "\t\t\tabandon = time.After(s.Limits.Grace)", "TestRunnerTimeoutCancelAndCleanup"),
    ("B1-04 runner: exit 0 alone counted as success", "internal/buyer/runner.go",
     "failed := exit != 0 || st.result == nil || st.isError || st.subtype != \"success\"", "failed := exit != 0", "TestRunnerOutcomes"),
    ("B1-04 runner: second result accepted", "internal/buyer/runner.go",
     "if st.result != nil {\n\t\t\t\treturn errParse", "if false {\n\t\t\t\treturn errParse", "TestRunnerOutcomes"),
    ("B1-04 runner: total stdout unbounded", "internal/buyer/runner.go",
     "if total += len(line) + 1; total > lim.Stdout {", "if total += len(line) + 1; false {", "TestRunnerOutcomes"),
    ("B1-04 runner: stderr unbounded", "internal/buyer/runner.go",
     "if err == nil && n > int64(s.Limits.Stderr) {", "if false {", "TestRunnerOutcomes"),
    ("B1-04 runner: missing executable not classified", "internal/buyer/runner.go",
     "if errors.Is(err, fs.ErrNotExist) {", "if false && errors.Is(err, fs.ErrNotExist) {", "TestRunnerNotInstalled"),
    ("B1-04 host: CLI timeout not recorded in the Store", "internal/buyer/host.go",
     "res.StoreEnd, end = \"TASK_TIMED_OUT\", st.CLITimeout(taskID)", "res.StoreEnd, end = \"TASK_TIMED_OUT\", nil", "TestHostEndings"),
    ("B1-04 host: CLI result text written to the trace", "internal/buyer/host.go",
     "\"result_chars\": utf8.RuneCountInString(res.Report.Result),", "\"result_chars\": utf8.RuneCountInString(res.Report.Result), \"result\": res.Report.Result,", "TestHostTraceWhitelist|TestHostCapabilityLifetime"),
    ("B1-04 host: parent environment inherited", "internal/buyer/host.go",
     "return append(env, extra...)", "return append(append(env, os.Environ()...), extra...)", "TestArgvAndEnv"),
    ("B1-04 host: prompt written to the trace", "internal/buyer/host.go",
     "\"prompt_sha256\": hex.EncodeToString(sum[:]),", "\"prompt_sha256\": hex.EncodeToString(sum[:]), \"prompt\": prompt,", "TestHostMainLine"),
    ("B1-04 command: real CLI accepted as fake", "cmd/apa-buyer/main.go",
     "if err := checkFake(ctx, cli, getenv); err != nil {", "if err := checkFake(ctx, cli, getenv); false && err != nil {", "TestRefusals"),
    # B1-04 review fixes: R1 run end versus calls in flight and writes,
    # bounded call records, R2 version check under the runner's rules.
    ("B1-04 R1 relay: ending a run does not cancel calls in flight", "internal/toolbridge/relay.go",
     "\tb.state = ended\n\tif b.stop != nil {\n\t\tb.stop()\n\t}\n", "\tb.state = ended\n", "TestRevokeWithCallInFlight|TestCommitGate"),
    ("B1-04 R1 relay: ending a run does not wait for calls in flight", "internal/toolbridge/relay.go",
     "\tb.inflight.Wait()\n", "", "TestCommitGate"),
    ("B1-04 R1 relay: an ended run still commits", "internal/toolbridge/relay.go",
     "case stopped, ended:\n\t\treturn ErrRunEnded", "case -2:\n\t\treturn ErrRunEnded", "TestCommitGate"),
    ("B1-04 R1 relay: calls admitted after the run ended", "internal/toolbridge/relay.go",
     "case b.ctx == nil || b.state >= stopped:", "case b.ctx == nil:", "TestCommitGate"),
    ("B1-04 relay: refused calls recorded without bound", "internal/toolbridge/relay.go",
     "if n == MaxCalls+1 {", "if true {", "TestCallRecordsBounded"),
    ("B1-04 R2 version: nonzero exit accepted", "internal/buyer/runner.go",
     "case x.exit != 0:\n\t\treturn \"\", fmt.Errorf", "case false:\n\t\treturn \"\", fmt.Errorf", "TestVersion"),
    ("B1-04 R2 version: output unbounded", "internal/buyer/runner.go",
     "if len(b) > lim.Stdout {", "if false {", "TestVersion"),
    # B1-04 live stage A: arming, init gate, Store only after init, limits,
    # trace whitelist, live opt-in and version.
    ("B1-04 A relay: business tools run before the run is armed", "internal/toolbridge/relay.go",
     "\tcase b.state == idle:\n\t\tb.notReady++", "\tcase false:\n\t\tb.notReady++", "TestNotReadyUntilArmed"),
    ("B1-04 A relay: a stopped run can be armed again", "internal/toolbridge/relay.go",
     "if b.state != idle {\n\t\treturn false", "if b.state == ended {\n\t\treturn false", "TestNotReadyUntilArmed"),
    ("B1-04 A relay: a write commits before the run is armed", "internal/toolbridge/relay.go",
     "\tcase idle:\n\t\treturn ErrNotReady", "\tcase -1:\n\t\treturn ErrNotReady", "TestNotReadyUntilArmed"),
    ("B1-04 A gate: tools matched by count, not by name", "internal/buyer/gate.go",
     "if !slices.Contains(want, t) || seen[t] {", "if false {", "TestHostInitGate"),
    ("B1-04 A gate: a repeated tool fills a missing one", "internal/buyer/gate.go",
     "if !slices.Contains(want, t) || seen[t] {", "if !slices.Contains(want, t) {", "TestHostInitGate"),
    ("B1-04 A gate: extra MCP servers accepted", "internal/buyer/gate.go",
     "if len(servers) != 1 {", "if len(servers) == 0 {", "TestHostInitGate"),
    ("B1-04 A gate: server status not checked", "internal/buyer/gate.go",
     " || status == nil || *status != \"connected\" {", " {", "TestHostInitGate"),
    ("B1-04 A gate: permission mode not checked", "internal/buyer/gate.go",
     "if perm == nil || *perm != \"dontAsk\" {", "if false {", "TestHostInitGate"),
    ("B1-04 A gate: CLI version not checked", "internal/buyer/gate.go",
     "if version == nil || *version != g.CLIVersion {", "if false {", "TestHostInitGate"),
    ("B1-04 A gate: plugins, skills and agents not checked", "internal/buyer/gate.go",
     "\t\tif len(list) > 0 {", "\t\tif false {", "TestHostInitGate"),
    ("B1-04 A gate: an unlisted auth value accepted", "internal/buyer/gate.go",
     "if !slices.Contains(g.AcceptAuth, *auth) {", "if false {", "TestHostInitGate"),
    ("B1-04 A gate: repeated JSON members accepted", "internal/buyer/gate.go",
     "if ambiguous(raw) {\n\t\treturn nil, errAmbiguous", "if false {\n\t\treturn nil, errAmbiguous", "TestHostInitGate|TestHostAmbiguousMessages|TestObjectFields"),
    ("B1-04 A host: init gate ignored", "internal/buyer/host.go",
     "if gateCode = h.Gate.check(line); gateCode != \"\" {", "if gateCode = h.Gate.check(line); false {", "TestHostInitGate"),
    ("B1-04 A host: hook messages ignored", "internal/buyer/host.go",
     "case typ == \"system\" && strings.Contains(subtype, \"hook\"):", "case false:", "TestHostInitSequence|TestHostHookAfterPurchase"),
    ("B1-04 A host: a second init accepted", "internal/buyer/host.go",
     "case initSeen:\n\t\t\treturn ConfigMismatch, \"init_duplicate\"", "case false:\n\t\t\treturn ConfigMismatch, \"init_duplicate\"", "TestHostInitSequence"),
    ("B1-04 A host: Store prepared after the run began stopping", "internal/buyer/host.go",
     "if b.Stopped() {", "if false {", "TestHostInitSequence"),
    ("B1-04 A host: a stop does not disarm the run", "internal/buyer/host.go",
     "Watch: watch, Stopping: b.Disarm})", "Watch: watch})", "TestHostInitSequence"),
    ("B1-04 A host: a run without init counted as ok", "internal/buyer/host.go",
     "if res.Report.Outcome == OK {\n\t\t\tres.Report.Outcome = ConfigMismatch", "if false {\n\t\t\tres.Report.Outcome = ConfigMismatch", "TestHostInitSequence"),
    ("B1-04 A host: raw usage written to the trace", "internal/buyer/host.go",
     "\"usage\": usageCounts(res.Report.Usage),", "\"usage\": res.Report.Usage,", "TestHostTraceWhitelist"),
    ("B1-04 A host: the fixed CLI environment dropped", "internal/buyer/host.go",
     "\tenv = append(env, fixedEnv...)\n", "", "TestArgvAndEnv"),
    ("B1-04 A host: --restricted dropped from argv", "internal/buyer/host.go",
     "\t\t\"--restricted\", // ignores user, project and local settings files\n", "", "TestArgvAndEnv"),
    ("B1-04 A runner: assistant bound off", "internal/buyer/runner.go",
     "if st.assistant++; lim.Assistant > 0 && st.assistant > lim.Assistant {", "if st.assistant++; false {", "TestAssistantLimit"),
    ("B1-04 A runner: every message counted as assistant", "internal/buyer/runner.go",
     "if m.Type == \"assistant\" {", "if m.Type != \"\" {", "TestAssistantLimit"),
    ("B1-04 A command: Store opened before the CLI starts", "cmd/apa-buyer/main.go",
     "\tbridge, err := toolbridge.Listen(tools)\n", "\tprepare()\n\tbridge, err := toolbridge.Listen(tools)\n", "TestNoStoreBeforeInit"),
    ("B1-04 A command: live without APA_LIVE_CLI", "cmd/apa-buyer/main.go",
     "perr != nil || !on {\n\t\t\treturn errors.New(\"--live also needs", "false && (perr != nil || !on) {\n\t\t\treturn errors.New(\"--live also needs", "TestLiveOptIn"),
    ("B1-04 A command: relative --cli accepted", "cmd/apa-buyer/main.go",
     "if !filepath.IsAbs(*cliPath) {\n\t\t\treturn errors.New(\"--live needs", "if false {\n\t\t\treturn errors.New(\"--live needs", "TestLiveOptIn"),
    ("B1-04 A command: another CLI version accepted", "cmd/apa-buyer/main.go",
     "if got := strings.TrimSpace(out); got != want {", "if got := strings.TrimSpace(out); false && got != want {", "TestLiveOptIn"),
    # B1-04 stage A review fixes (ca46cab): R1 no CLI free text in the
    # trace, R2 one strict reading of every stream message.
    ("B1-04 A R2 parser: member names compared without case folding", "internal/buyer/gate.go",
     "\t\t\tlow = min(low, f)\n", "\t\t\t_ = f\n", "TestObjectFields|TestHostAmbiguousMessages"),
    ("B1-04 A R1 host: CLI model kept without the listed-name check", "internal/buyer/host.go",
     "field(f, \"model\", &model) == nil && slices.Contains(knownModels, model) {", "field(f, \"model\", &model) == nil && (true || slices.Contains(knownModels, model)) {", "TestHostTraceKeepsOnlyListedNames|TestHostTraceWhitelist"),
    # B1-04 stage A follow-up (59da5d9 review): only listed names in the trace.
    ("B1-04 A R1 bridge: a well-formed but unoffered product ID recorded", "internal/toolbridge/shop.go",
     "if _, ok := s.Products[product]; ok {\n\t\trec.ProductID = product", "if true {\n\t\trec.ProductID = product", "TestPurchaseIntegrity"),
    ("B1-04 A R1 host: requested model missing from the trace", "internal/buyer/host.go",
     "\"requested_model\": Model, ", "", "TestHostMainLine|TestHostTraceKeepsOnlyListedNames"),
    # B1-04 C0 probe: zero input, never armed, stops on init, whitelist,
    # opt-in and version, no Store, no CLI text in errors.
    ("B1-04 C0 runner: a byte written to the held stdin", "internal/buyer/runner.go",
     "\t\tiw = newStdinWriter(inW)", "\t\tinW.Write([]byte(\"\\n\"))\n\t\tiw = newStdinWriter(inW)", "TestProbeInitFirst|TestProbeWaitsForInput"),
    ("B1-04 C0 runner: stdin not held open (null device)", "internal/buyer/runner.go",
     "\tif s.HoldStdin {", "\tif false && s.HoldStdin {", "TestProbeWaitsForInput"),
    ("B1-04 C0 probe: the run is armed", "internal/buyer/probe.go",
     "// idle, and never armed\n", "// idle, and never armed\n\tb.Arm()\n", "TestProbeInitFirst"),
    ("B1-04 C0 probe: the init does not stop the probe", "internal/buyer/probe.go",
     "\t\t\treturn InitObserved, \"init\"", "\t\t\treturn \"\", \"\"", "TestProbeInitFirst"),
    ("B1-04 C0 probe: hook messages ignored", "internal/buyer/probe.go",
     "\t\t\treturn ConfigMismatch, \"hook_message\"", "\t\t\treturn \"\", \"\"", "TestProbeFailures"),
    ("B1-04 C0 probe: model output before the init ignored", "internal/buyer/probe.go",
     "\t\t\treturn ConfigMismatch, \"unexpected_message\"", "\t\t\treturn \"\", \"\"", "TestProbeFailures"),
    ("B1-04 C0 probe: an init of another version counted as observed", "internal/buyer/probe.go",
     "case rep.Outcome == InitObserved && obs != nil && obs.CLIVersion == \"approved\":", "case rep.Outcome == InitObserved && obs != nil:", "TestProbeFailures"),
    ("B1-04 C0 probe: no init reported as a failure", "internal/buyer/probe.go",
     "\tcase rep.Outcome == Timeout && res.Mode == ModeC0:\n\t\tres.Status, res.Code = ProbeNoInit", "\tcase false:\n\t\tres.Status, res.Code = ProbeNoInit", "TestProbeWaitsForInput"),
    ("B1-04 C0 probe: unlisted init strings shown as is", "internal/buyer/probe.go",
     "\treturn \"other\", len(v)", "\treturn v, len(v)", "TestProbeNoLeak|TestProbeUnknownValues"),
    ("B1-04 C0 command: probe without APA_LIVE_CLI", "cmd/apa-buyer/main.go",
     "perr != nil || !on {\n\t\treturn errors.New(\"probe-init needs APA_LIVE_CLI=true", "false && (perr != nil || !on) {\n\t\treturn errors.New(\"probe-init needs APA_LIVE_CLI=true", "TestProbeOptIn"),
    ("B1-04 C0 command: probe skips the version check", "cmd/apa-buyer/main.go",
     "if err := checkLive(ctx, *cliPath, env, want); err != nil {", "if err := checkLive(ctx, *cliPath, env, want); false && err != nil {", "TestProbeOptIn"),
    ("B1-04 C0 command: the probe touches the Store", "cmd/apa-buyer/main.go",
     "\tres, err := buyer.Probe(ctx, buyer.ProbeSpec{", "\tif d.openStore != nil {\n\t\td.openStore(store.Config{})\n\t}\n\tres, err := buyer.Probe(ctx, buyer.ProbeSpec{", "TestProbeCommand"),
    ("B1-04 C0 command: the version error repeats the CLI's text", "cmd/apa-buyer/main.go",
     "return fmt.Errorf(\"%s reports a version other than the approved %q; nothing was started or upgraded\", path, want)", "return fmt.Errorf(\"%s reports %q, a version other than the approved %q; nothing was started or upgraded\", path, got, want)", "TestLiveOptIn"),
    # B1-04 C0 review (71f222a): api_retry stops the probe; cancel checked before any start.
    ("B1-04 C0 R: api_retry does not stop the probe", "internal/buyer/probe.go",
     "\t\t\treturn CLIError, apiRetryCode(line)", "\t\t\treturn \"\", \"\"", "TestProbeAPIRetry"),
    ("B1-04 C0 R: authentication_failed retry not told apart", "internal/buyer/probe.go",
     "\t\treturn \"api_retry_auth\"", "\t\treturn \"api_retry\"", "TestProbeAPIRetry"),
    ("B1-04 C0 R: rate_limit retry not told apart", "internal/buyer/probe.go",
     "\t\treturn \"api_retry_rate_limit\"", "\t\treturn \"api_retry\"", "TestProbeAPIRetry"),
    ("B1-04 C1 runner: a cancelled run still starts its process", "internal/buyer/runner.go",
     "\tif ctx.Err() != nil { // cancelled or out of time before the start", "\tif false && ctx.Err() != nil { // cancelled or out of time before the start", "TestRunnerCancelledBeforeStart"),
    ("B1-04 C1 command: a cancelled probe still runs the version check", "cmd/apa-buyer/main.go",
     "\tif ctx.Err() != nil {\n\t\treturn errors.New(\"cancelled before the version check; the probe was not started\")", "\tif false && ctx.Err() != nil {\n\t\treturn errors.New(\"cancelled before the version check; the probe was not started\")", "TestProbeOptIn"),
    # B1-04 C0′: one fixed message, written once, never after a stop, reported as written.
    ("B1-04 C0′ writer: writes after the run began stopping", "internal/buyer/runner.go",
     "\tif iw.stopped {\n\t\tiw.status = InputNotStarted", "\tif false {\n\t\tiw.status = InputNotStarted", "TestStdinWriter"),
    ("B1-04 C0′ writer: a failed write reported complete", "internal/buyer/runner.go",
     "\tdefault:\n\t\tiw.status = InputFailed", "\tdefault:\n\t\tiw.status = InputComplete", "TestStdinWriter"),
    ("B1-04 C0′ writer: the message written twice", "internal/buyer/runner.go",
     "\tn, err := iw.w.Write(msg)", "\tiw.w.Write(msg)\n\tn, err := iw.w.Write(msg)", "TestProbeOneMessage"),
    ("B1-04 C0′ probe: C0 sends the message too", "internal/buyer/probe.go",
     "\tif p.SendOneMessage {", "\tif true {", "TestProbeInitFirst|TestProbeWaitsForInput"),
    ("B1-04 C0′ probe: no init after the message reported as C0's", "internal/buyer/probe.go",
     "\tcase rep.Outcome == Timeout && res.Mode == ModeC0:", "\tcase rep.Outcome == Timeout:", "TestProbeOneMessageNoInit"),
    ("B1-04 C0′ probe: planned size reported as written", "internal/buyer/probe.go",
     "res.InputStatus, res.StdinWritten = rep.InputStatus, rep.InputBytes", "res.InputStatus, res.StdinWritten = rep.InputStatus, res.InputSize", "TestProbeOneMessageInterleaving"),
    ("B1-04 C0′ probe: the message text changed", "internal/buyer/probe.go",
     "const OneMessageText = \"Reply only OK.\"", "const OneMessageText = \"Reply only OK!\"", "TestOneMessageLine|TestProbeOneMessage"),
    ("B1-04 C0′ command: --send-one-message ignored", "cmd/apa-buyer/main.go",
     "SendOneMessage: *oneMessage})", "SendOneMessage: false})", "TestProbeOneMessageCommand"),
    # B1-04 C0′ review (cf97c87): a failed write stops the run and stays a failure; init first stays valid.
    ("B1-04 C0′ R: a failed write does not stop the run", "internal/buyer/runner.go",
     "\t\t\tif st := iw.status; st == InputFailed || st == InputPartial {", "\t\t\tif st := iw.status; false && (st == InputFailed || st == InputPartial) {", "TestProbeOneMessageWriteFailure"),
    ("B1-04 C0′ R: an init read after the stop is kept", "internal/buyer/probe.go",
     "\tif rep.Outcome == InitObserved && !inputBroken {\n\t\tres.Observation = obs", "\tif true {\n\t\tres.Observation = obs", "TestProbeOneMessageWriteFailure"),
    ("B1-04 C0′ R: init before the write is not an observation", "internal/buyer/probe.go",
     "\tcase rep.Outcome == InitObserved && obs != nil && obs.CLIVersion == \"approved\":", "\tcase rep.Outcome == InitObserved && obs != nil && obs.CLIVersion == \"approved\" && (res.Mode == ModeC0 || rep.InputStatus == InputComplete):", "TestProbeOneMessageInitBeforeWrite"),
    # B1-04 C0′ review (38eb2ba): a broken write decides the result whatever stopped the run first.
    ("B1-04 C0′ R1: a broken write loses to an init stop", "internal/buyer/probe.go",
     "\tcase inputBroken:\n\t\tres.Status, res.Code = ProbeFailed", "\tcase false:\n\t\tres.Status, res.Code = ProbeFailed", "TestProbeOneMessageWriteFailureAfterInit"),
    ("B1-04 C0′ R1: the observation kept after a broken write", "internal/buyer/probe.go",
     "\tif rep.Outcome == InitObserved && !inputBroken {", "\tif rep.Outcome == InitObserved {", "TestProbeOneMessageWriteFailureAfterInit"),
    ("B1-04 pin 2.1.289: the probe's init version not taken from the pin", "cmd/apa-buyer/main.go",
     "Limits: lim, CLIVersion: strings.Fields(want)[0], TraceDir", "Limits: lim, CLIVersion: \"2.1.288\", TraceDir", "TestApprovedVersionInit"),
    ("B1-04 plugins/agents: an element's name copied out, not only a listed one", "internal/buyer/probe.go",
     "\t\te.Name, e.NameLength = pick(f, \"name\", names)\n", "\t\te.Name, e.NameLength = pick(f, \"name\", names)\n\t\tjson.Unmarshal(f[\"name\"], &e.Name)\n",
     "TestProbeNoLeak|TestProbeEntries"),
    ("B1-04 plugins/agents: a plugin's path copied out, not its class", "internal/buyer/probe.go",
     "\t\t\t\te.Path = pathClass(path, *r)", "\t\t\t\te.Path = path", "TestProbeNoLeak|TestProbeEntries"),
    ("B1-04 plugins/agents: a root matched as a bare prefix", "internal/buyer/probe.go",
     "return p == root || strings.HasPrefix(p, root+string(filepath.Separator))", "return strings.HasPrefix(p, root)", "TestPathClass"),
    ("B1-04 plugins/agents: a plugin path classed before cleaning", "internal/buyer/probe.go",
     "LINE:\tp = filepath.Clean(p)", "", "TestPathClass"),
    ("B1-04 plugins/agents: the per-element list not bounded", "internal/buyer/probe.go",
     "range list[:min(len(list), maxEntries)]", "range list", "TestProbeEntries"),
    ("B1-04 null: a null picked as an empty \"other\" string", "internal/buyer/probe.go",
     "\tif jsonType(raw) != \"string\" || json.Unmarshal(raw, &v) != nil {", "\tif json.Unmarshal(raw, &v) != nil {", "TestProbeNullStrings|TestProbeEntries"),
    ("B1-04 null: a null plugin path classed as a path", "internal/buyer/probe.go",
     "case jsonType(raw) != \"string\" || json.Unmarshal(raw, &path) != nil:", "case json.Unmarshal(raw, &path) != nil:", "TestProbeEntries"),
    ("B1-06 api: the Host header not checked", "cmd/demo/api.go",
     "if r.Host != a.addr {", "if false {", "TestIntegrationAskDenyAndAccess"),
    ("B1-06 api: a foreign Origin accepted", "cmd/demo/api.go",
     "o != \"\" && o != \"http://\"+a.addr", "o == \"never\"", "TestIntegrationAskDenyAndAccess"),
    ("B1-06 api: any bearer token accepted", "cmd/demo/api.go",
     "subtle.ConstantTimeCompare([]byte(tok), []byte(t.Token)) == 1", "subtle.ConstantTimeCompare([]byte(tok), []byte(t.Token)) >= 0", "TestIntegrationAskDenyAndAccess"),
    ("B1-06 api: a viewer sees every consumer", "cmd/demo/api.go",
     "return g.Role == \"host\" || g.Role == \"viewer\" && g.ConsumerID == consumerID", "return g.Role != \"\"", "TestIntegrationAskDenyAndAccess"),
    ("B1-06 api: the grant not applied to the task", "cmd/demo/api.go",
     "a.st.ReadTask(r.PathValue(\"task_id\"), g.sees)", "a.st.ReadTask(r.PathValue(\"task_id\"), func(string) bool { return g.Role != \"\" })", "TestIntegrationAskDenyAndAccess"),
    ("B1-06 api: a task not started answers apart from 404", "cmd/demo/api.go",
     "if store.Code(err) == \"OWNERSHIP_MISMATCH\" {", "if false {", "TestIntegrationAskDenyAndAccess"),
    ("B1-06 api: a GET runs the expiry", "cmd/demo/api.go",
     "\tb, v := rec.Binding, rec.Task", "\ta.st.ExpireDue()\n\trec, _ = a.st.ReadTask(rec.Binding.TaskID, func(string) bool { return true })\n\tb, v := rec.Binding, rec.Task", "TestIntegrationExpiryOnlyByWorker"),
    ("B1-06 lifecycle: the Store closes before the runs end", "cmd/demo/main.go",
     "LINE:\truns.Wait()", "", "TestIntegrationEndsRestartAndReplay"),
    ("B1-06 lifecycle: two hosts on one database", "cmd/demo/main.go",
     "syscall.Flock(int(f.Fd()), syscall.LOCK_EX|syscall.LOCK_NB); err != nil {", "syscall.Flock(int(f.Fd()), syscall.LOCK_EX|syscall.LOCK_NB); false && err != nil {", "TestIntegrationStartRefusals"),
    ("B1-06 plan: unknown plan fields accepted", "cmd/demo/fakeruns.go",
     "LINE:\tdec.DisallowUnknownFields()", "", "TestIntegrationStartRefusals"),
    ("B1-06 plan: a CLI that is not the fake accepted", "cmd/demo/fakeruns.go",
     "if err != nil || !strings.Contains(out, \"fake-claude\") {", "if err != nil || !strings.Contains(out, \"fake-claude\") && false {", "TestIntegrationStartRefusals"),
    ("B1-06 plan: a recorded task run again", "cmd/demo/fakeruns.go",
     "TaskID: task.TaskID}); err == nil {", "TaskID: task.TaskID}); false && err == nil {", "TestIntegrationEndsRestartAndReplay"),
    ("B1-06 R1: the token path not checked before the lock and the Store", "cmd/demo/main.go",
     "if err := checkTokenPath(dbPath, tokenPath); err != nil {", "if err := checkTokenPath(dbPath, tokenPath); false && err != nil {", "TestTokenPathConflictRefusedAtStart"),
    ("B1-06 R1: the token rename not checked against the database's files", "cmd/demo/api.go",
     "if err := checkTokenPath(db, abs); err != nil {", "if err := checkTokenPath(db, abs); false && err != nil {", "TestTokenPathConflictRefusedAtStart"),
    ("B1-06 R1: canonical paths not compared", "cmd/demo/main.go",
     "\t\tif c == t {", "\t\tif false && c == t {", "TestTokenPathMustNotBeADatabaseFile"),
    ("B1-06 R1: a symlinked parent not resolved", "cmd/demo/main.go",
     "if real, err := filepath.EvalSymlinks(dir); err == nil {", "if real, err := filepath.EvalSymlinks(dir); err == nil && dir == \"\" {", "TestTokenPathMustNotBeADatabaseFile"),
    ("B1-06 R1: an existing file under another name not caught", "cmd/demo/main.go",
     "terr == nil && os.SameFile(fi, ti)", "terr == nil && os.SameFile(fi, ti) && false", "TestTokenPathMustNotBeADatabaseFile"),
    ("B1-06 R2: the caller's grant not applied to the stored consumer", "internal/store/ops.go",
     "err == nil && !visible(t.row.ConsumerID)", "err == nil && visible == nil", "TestReadTask"),
    ("B1-06 R2: a stored binding its snapshot does not hold served", "internal/store/ops.go",
     "if err != nil || task.ConsumerID != t.row.ConsumerID || task.AgentID != t.row.AgentID || w.WalletID != t.row.WalletID || w.From != t.row.Payer {", "if false && (err != nil || task.ConsumerID != t.row.ConsumerID || task.AgentID != t.row.AgentID || w.WalletID != t.row.WalletID || w.From != t.row.Payer) {", "TestReadTask"),
    # B1-06 slice 3.2 (docs/demo-operation-design.md section 13): run
    # coordination, control, recovery, fixed codes and VerifyFile.
    ('B1-06 3.2: a resend accepted again inside the write', 'internal/store/runs.go',
     '\t\tif err == nil {\n\t\t\trun = w.r\n\t\t\treturn nil\n\t\t}', '\t\tif false {\n\t\t\trun = w.r\n\t\t\treturn nil\n\t\t}', 'TestAcceptRun'),
    ('B1-06 3.2: the read-only lookup never finds a request', 'internal/store/runs.go',
     'FROM host_runs WHERE request_id = ?`, requestID))', 'FROM host_runs WHERE request_id = ? AND 0`, requestID))', 'TestAcceptRun'),
    ('B1-06 3.2: a second run while one is active', 'cmd/demo/control.go',
     '\tif c.active != nil {\n\t\treply(w, http.StatusConflict', '\tif false && c.active != nil {\n\t\treply(w, http.StatusConflict', 'TestControlRuns'),
    ('B1-06 3.2: allocation ignores reserved tasks', 'internal/store/runs.go',
     '(SELECT count(*) FROM host_runs WHERE task_id = ?)', '(SELECT count(*) FROM host_runs WHERE task_id = ? AND 0)', 'TestAcceptRun'),
    ('B1-06 3.2: runs of a previous host not marked interrupted', 'cmd/demo/main.go',
     'if n, err := st.InterruptOpenRuns(); err != nil {', 'if n, err := int64(0), error(nil); err != nil {', 'TestControlFailuresAndRestart'),
    ('B1-06 3.2: the operator reads task content', 'cmd/demo/api.go',
     'if g.Role == "operator" { // controls runs, reads no task content', 'if false && g.Role == "operator" { // controls runs, reads no task content', 'TestControlAccess'),
    ('B1-06 3.2: detail free text accepted', 'internal/store/runs.go',
     'case !w.detail.Valid || !ValidDetail(r.Detail):', 'case !w.detail.Valid:', 'TestEndRun'),
    ("B1-06 3.2: VerifyFile creates the source's lock", 'internal/store/verifyfile.go',
     'os.OpenFile(abs+".lock", os.O_RDWR, 0)', 'os.OpenFile(abs+".lock", os.O_RDWR|os.O_CREATE, 0o600)', 'TestVerifyFileLocks'),
    ('B1-06 3.2: a failed end record shown as complete', 'cmd/demo/control.go',
     '\tif err != nil {\n\t\tlr.recordErr = store.CodeOf(err)\n\t}', '\tif false {\n\t\tlr.recordErr = store.CodeOf(err)\n\t}', 'TestControlFailuresAndRestart'),
    ('B1-06 3.2: an unlisted buyer detail kept', 'internal/buyer/host.go',
     '\tif !store.ValidDetail(res.Report.Detail) {', '\tif false {', 'TestNoUnlistedTextLeaves'),
    ('B1-06 3.2: a schema 1 database upgraded without --upgrade-db', 'internal/store/store.go',
     '\t\tif !upgrade {', '\t\tif false {', 'TestSchemaNewAndRefusedUpgrade|TestMigrateRefusesSchema1'),
    # B1-06 slice 3.2 review (R1, R2).
    ('B1-06 3.2 review R1: a refused schema 1 file opened before the version check', 'internal/store/store.go',
     '\tcase v == 1 && !cfg.UpgradeSchema1:\n', '\tcase false:\n', 'TestRefusedSchema1KeepsWAL'),
    ('B1-06 3.2 review R2: EndRun writes what the recovery would refuse', 'internal/store/runs.go',
     'if why, err := runVsStore(tx, w.r); err != nil {', 'if why, err := "", error(nil); err != nil {', 'TestEndRunChecksTheStore'),
    # B1-06 slice 3.2 review (answer's re-read).
    ('B1-06 3.2: answer shows the older row when the re-read fails', 'cmd/demo/control.go',
     '\tcase err != nil:\n\t\treply(w, http.StatusServiceUnavailable, map[string]string{"error": "store_unavailable", "code": store.CodeOf(err)})\n\t\treturn\n\tcase !found:\n\t\treply(w, http.StatusServiceUnavailable, map[string]string{"error": "store_unavailable", "code": "RUN_NOT_FOUND"})\n\t\treturn\n\t}\n\treply(w, http.StatusOK, c.view(fresh))', '\tcase err == nil && found:\n\t\trun = fresh\n\t}\n\treply(w, http.StatusOK, c.view(run))', 'TestAnswerRereadsOrFails'),
    # B1-06 slice 3.3 (docs/demo-operation-design.md section 14): the task list.
    ("B1-06 3.3: a viewer lists every consumer's tasks", 'internal/store/tasklist.go',
     "WHERE ? = '' OR consumer_id = ? ORDER BY", "WHERE ? = '' OR consumer_id = ? OR 1 ORDER BY", 'TestListTasks'),
    ('B1-06 3.3: the operator lists tasks', 'cmd/demo/api.go',
     '\tdefault:\n\t\tfail(w, http.StatusForbidden, "forbidden_role")\n\t\treturn\n\t}\n\tlist, err := a.st.ListTasks', '\tdefault:\n\t}\n\tlist, err := a.st.ListTasks', 'TestTaskList'),
    # B1-06 slice 3.4 (docs/demo-operation-design.md section 15): cancel a task.
    ('B1-06 3.4: a host token cancels a task', 'cmd/demo/cancel.go',
     '\tif !postGuard(w, r, a, &empty) {', '\tif _ = empty; !reader(w, r, a) {', 'TestCancelAccessAndGuards'),
    ('B1-06 3.4: a task cancelled while its run is active', 'cmd/demo/cancel.go',
     'if c.active != nil && c.active.run.TaskID == id {', 'if false {', 'TestCancelWhileRunning'),
    ('B1-06 3.4: a repeated cancel not answered from the stored cancel', 'cmd/demo/cancel.go',
     '\t\tcase "TASK_CANCELLED":\n', '\t\tcase "TASK_CANCELLED_X":\n', 'TestCancelRows|TestCancelConcurrent'),
    ('B1-06 3.4: a Deny cancelled', 'cmd/demo/cancel.go',
     'if rec.Task.Status == "denied" {', 'if false {', 'TestCancelRows'),
    ('B1-06 3.4: a refused cancel write answered as success', 'cmd/demo/cancel.go',
     '\tdefault:\n\t\tstoreDown(w, store.CodeOf(err))\n\t\treturn\n\t}\n\trec, err = c.readForCancel', '\tdefault:\n\t\treply(w, http.StatusOK, map[string]any{"state": "cancelled"})\n\t\treturn\n\t}\n\trec, err = c.readForCancel', 'TestCancelRows'),
    ('B1-06 3.4: TASK_ALREADY_ENDED taken as cancelled', 'cmd/demo/cancel.go',
     '\tcase "TASK_ALREADY_ENDED": // ended by another writer meanwhile: answer what was committed\n', '\tcase "TASK_ALREADY_ENDED":\n\t\treply(w, http.StatusOK, map[string]any{"state": "cancelled", "already": true})\n\t\treturn\n', 'TestCancelStoreFaults'),
    ('B1-06 3.4: a failed re-read answered as cancelled', 'cmd/demo/cancel.go',
     '\tcase err != nil:\n\t\tstoreDown(w, store.CodeOf(err))\n\tcase !answerEnded', '\tcase err != nil:\n\t\treply(w, http.StatusOK, map[string]any{"state": "cancelled", "already": true})\n\tcase !answerEnded', 'TestCancelStoreFaults'),
    # Review of d61771c, R1: a control body is one JSON object and nothing after it.
    ('B1-06 3.4 R1: a control body need not be an object (null cancels)', 'cmd/demo/control.go',
     ' || !bytes.HasPrefix(bytes.TrimLeft(body, " \\t\\r\\n"), []byte("{")) {', ' {', 'TestCancelBody|TestControlBodyShape'),
    ('B1-06 3.4 R1: a control body may go on after its object', 'cmd/demo/control.go',
     '\tif _, err := dec.Token(); err != io.EOF {', '\tif false {', 'TestCancelBody|TestControlBodyShape'),
    # B1-04 L1-a: the CLI's fixed environment disables the built-in agents.
    ('B1-04 L1-a: built-in agents switch dropped from the CLI environment', 'internal/buyer/host.go',
     ', "CLAUDE_AGENT_SDK_DISABLE_BUILTIN_AGENTS=1"}', '}', 'TestArgvAndEnv|TestCLIEnvReachesChild'),
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

    # Every run starts from a fresh export, always at the same path: Go's
    # build cache is keyed on the source directory too, so a new directory
    # per mutation rebuilt every package of the module each time.
    started = time.monotonic()
    work = tempfile.mkdtemp()

    def fresh(tree):
        shutil.rmtree(tree, ignore_errors=True)
        tree.mkdir()
        export(root, sha, tree)

    base = pathlib.Path(work) / "base"
    fresh(base)
    t0 = time.monotonic()
    rc, log = run(["go", "test", *sorted({pkg(m[1]) for m in MUTATIONS}), "-count=1"], base)
    (out / "baseline.log").write_text(log)
    lines.append(f"baseline (no mutation): {'PASS' if rc == 0 else 'FAIL'}  [{time.monotonic() - t0:.0f}s]")
    ok = ok and rc == 0

    def mutate(tree, n, finding, rel, old, new, test):
        """One mutation in a fresh tree: (summary line, killed)."""
        fresh(tree)
        t0 = time.monotonic()
        path = tree / rel
        text = path.read_text()
        if old.startswith("LINE:"):
            marker = old[5:]
            kept = [l for l in text.splitlines(keepends=True) if marker not in l]
            count, mutated = len(text.splitlines()) - len(kept), "".join(kept)
        else:
            count, mutated = text.count(old), text.replace(old, new)
        if count != 1:
            return f"{n:02d} ERROR   {finding}: mutation text found {count} times in {rel}", False
        path.write_text(mutated)
        # A hang fails this mutation in 5 minutes instead of using up the
        # whole step: a timed-out test prints no "--- FAIL", so it can only
        # read as BUILD?, never as KILLED.
        rc, log = run(["go", "test", pkg(rel), "-run", test, "-count=1", "-timeout", "300s"], tree)
        (out / f"mutation-{n:02d}.log").write_text(log)
        killed = rc != 0 and "--- FAIL" in log and "panic: test timed out" not in log
        verdict = "KILLED" if killed else ("BUILD?" if rc != 0 else "SURVIVED")
        return f"{n:02d} {verdict:8s}{finding}  [{rel}; -run {test}]  [{time.monotonic() - t0:.0f}s]", killed

    # Four lanes run at once (the container has 4 CPUs), each in its own
    # fixed tree: internal/buyer's mutations alternate between two lanes,
    # all others between the other two, so the groups stay balanced.
    def lane(name, items):
        tree = pathlib.Path(work) / name
        return [(n, *mutate(tree, n, *m)) for n, m in items]

    numbered = list(enumerate(MUTATIONS, 1))
    buyer = [x for x in numbered if pkg(x[1][1]) == "./internal/buyer/"]
    other = [x for x in numbered if pkg(x[1][1]) != "./internal/buyer/"]
    lanes = {"lane-buyer-1": buyer[0::2], "lane-buyer-2": buyer[1::2], "lane-other-1": other[0::2], "lane-other-2": other[1::2]}
    with concurrent.futures.ThreadPoolExecutor(max_workers=len(lanes)) as pool:
        done = [r for f in [pool.submit(lane, k, v) for k, v in lanes.items()] for r in f.result()]
    for n, line, killed in sorted(done):
        lines.append(line)
        ok = ok and killed

    shutil.rmtree(work, ignore_errors=True)
    lines.append(f"total: {time.monotonic() - started:.0f}s")
    lines.append("result: " + ("all mutations killed by a failing test" if ok else "NOT OK"))
    (out / "summary.txt").write_text("\n".join(lines) + "\n")
    print("\n".join(lines))
    print(f"run directory: {out}")
    sys.exit(0 if ok else 1)


if __name__ == "__main__":
    main()
