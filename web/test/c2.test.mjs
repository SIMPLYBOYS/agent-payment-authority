// Tests of C2 in the page (docs/commercial-c2-design.md 10 step 8): an
// apa.pay.v2 task uses the pay family's words, shows its verification
// record on the card and in the drawer (live and replayed), and an Ask
// withdrawn for a credential reason is worded by that reason, never as the
// task's cancellation; HOST_CANCELLED keeps its words.
// Run: node --test web/test/c2.test.mjs
import { test } from "node:test";
import assert from "node:assert/strict";
import * as A from "../adapter.js";

let n = 0;
const ev = (seq, type, extra = {}) => ({ event_id: "e" + n++, event_type: type, sequence: seq, evidence_mode: "fixture", source: "host", observed_at: 1790726450 + seq, purchase_order_id: "po-1", version: A.V2, ...extra });
const decision = (verdict, approval, reason) => ({ version: A.V2, policy_verdict: verdict, reason, approval_status: approval, payment_status: "not_enabled" });
const check = { result: "valid", challenge_id: "chl-0123456789abcdef", credential_schema: "apa.credential.v1", issuer: "issuer-demo", holder: "ed25519:" + "ab".repeat(32),
  status_index: "0", expiry: 1790812850, status_list_version: 2, status_list_issued_at: 1790726400, status_list_sha256: "cd".repeat(32), proof_sha256: "ef".repeat(32), verifier: "apa-demo-host" };
const snap = (events, purchases = []) => ({
  task: { task_id: "show-a-01", consumer_id: "consumer-a", status: "running", as_of_sequence: events.length, served_at: 1790726470, purchases, contract_version: A.V2, payment_capability: "not_enabled" },
  events,
});
const asked = [ev(1, "TASK_STARTED", { purchase_order_id: "" }), ev(2, "PURCHASE_PROPOSED"), ev(3, "CREDENTIAL_VERIFIED", { credential: check }),
  ev(4, "POLICY_DECIDED", { decision: decision("Ask", "pending", "HUMAN_APPROVAL_REQUIRED") })];
const purchase = { purchase_order_id: "po-1", decision: decision("Ask", "pending", "HUMAN_APPROVAL_REQUIRED"), deadline_at: 1790726600, amount: "500000", credential: check };

test("apa.pay.v2 is the pay family: the simulated approval's words, and the credential row", () => {
  assert.ok(A.isPay(A.V2) && A.isPay(A.V1) && !A.isPay("apa.m1.v2"));
  const c = A.card(snap(asked, [purchase]), null, { mode: "live" });
  assert.match(c.status, /等你核准（模擬核准；不會付款）/);
  assert.ok(c.rows.some(([k, v, src]) => k === "憑證檢查" && v.startsWith("通過") && v.includes("非 W3C bbs-2023") && src.includes("CREDENTIAL_VERIFIED")), JSON.stringify(c.rows));
  assert.equal(A.scene(snap(asked)).label, "等你核准（模擬核准；不會付款）");
});

test("a credential Deny is worded by its reason and detail", () => {
  const denied = { ...purchase, decision: decision("Deny", "not_required", "CREDENTIAL_REVOKED"), credential: { ...check, result: "revoked" } };
  const c = A.card(snap(asked, [denied]), null, { mode: "live" });
  assert.match(c.status, /Deny：CREDENTIAL_REVOKED（憑證已撤銷）/);
  assert.ok(c.rows.some(([k, v]) => k === "憑證檢查" && v === "已撤銷"));
  const bad = { ...purchase, decision: decision("Deny", "not_required", "CREDENTIAL_INVALID"), credential: { result: "invalid", detail: "CHALLENGE_EXPIRED", challenge_id: "chl-1", verifier: "apa-demo-host" } };
  assert.ok(A.card(snap(asked, [bad]), null, { mode: "live" }).rows.some(([k, v]) => k === "憑證檢查" && v === "未通過（CHALLENGE_EXPIRED）"));
});

test("withdrawn for a credential reason is the credential's; HOST_CANCELLED keeps 'task 取消'", () => {
  for (const [reason, text] of [["CREDENTIAL_REVOKED", "憑證已撤銷，這筆已關閉"], ["CREDENTIAL_STATUS_UNAVAILABLE", "憑證狀態無法確認，這筆已關閉"], ["AUTHORIZATION_EXPIRED", "憑證已過期，這筆已關閉"]]) {
    const events = [...asked, ev(5, "APPROVAL_WITHDRAWN", { decision: decision("Ask", "withdrawn", reason) })];
    const p = { ...purchase, approval_closed: "withdrawn", closed_reason: reason };
    assert.equal(A.scene(snap(events)).label, text + "：離店", reason);
    const c = A.card(snap(events, [p]), null, { mode: "live" });
    assert.equal(c.status, text, reason);
    assert.ok(!JSON.stringify(c).includes("task 取消"), reason);
  }
  const events = [...asked, ev(5, "APPROVAL_WITHDRAWN", { decision: decision("Ask", "withdrawn", "HOST_CANCELLED") }), ev(6, "TASK_CANCELLED", { purchase_order_id: "" })];
  const c = A.card(snap(events, [{ ...purchase, approval_closed: "withdrawn", closed_reason: "HOST_CANCELLED" }]), null, { mode: "live" });
  assert.equal(c.status, "已撤回（task 取消）");
  assert.equal(A.scene(snap(events)).label, "Ask 已撤回（task 取消）：離店");
});

test("a replay step shows the verification and the closing reason from their own events", () => {
  const events = [...asked, ev(5, "APPROVAL_WITHDRAWN", { decision: decision("Ask", "withdrawn", "CREDENTIAL_REVOKED") })];
  const s = snap(events, [{ ...purchase, approval_closed: "withdrawn", closed_reason: "CREDENTIAL_REVOKED" }]);
  assert.equal(A.stepView(s, 2).task.purchases[0].credential, undefined);
  assert.equal(A.stepView(s, 3).task.purchases[0].credential.result, "valid");
  assert.equal(A.stepView(s, 4).task.purchases[0].closed_reason, undefined);
  assert.equal(A.stepView(s, 5).task.purchases[0].closed_reason, "CREDENTIAL_REVOKED");
});
