// A2 in the page (docs/commercial-human-approval-design.md 6.3, 8; layer W):
// the challenge check before any prompt, the WebAuthn conversions and the
// fixed texts of each outcome. No DOM, no browser.
import test from "node:test";
import assert from "node:assert/strict";
import * as AP from "../approve.js";
import { b64u } from "../adapter.js";

const op = { consumer: "consumer-a", session: "aps-1", intent: "sha256:" + "a".repeat(64), decision: "approve" };
const ch = { challenge_id: "chl-1", consumer_id: "consumer-a", approval_session_id: "aps-1", intent_hash: op.intent, decision: "approve",
  options: { publicKey: { challenge: "AAEC", allowCredentials: [{ type: "public-key", id: "BAUG" }], userVerification: "required" } } };

test("a challenge is checked to be this click's before the device is asked", () => {
  assert.equal(AP.sameChallenge(ch, op), true);
  for (const [k, v] of [["consumer_id", "consumer-b"], ["approval_session_id", "aps-2"], ["intent_hash", "sha256:" + "b".repeat(64)], ["decision", "deny"]]) {
    assert.equal(AP.sameChallenge({ ...ch, [k]: v }, op), false, k);
  }
  assert.equal(AP.sameChallenge({ ...ch, options: null }, op), false);
  assert.equal(AP.sameChallenge(null, op), false);
});

test("options and the credential convert as WebAuthn needs", () => {
  const p = AP.requestOptions(ch.options);
  assert.deepEqual([...p.challenge], [0, 1, 2]);
  assert.deepEqual([...p.allowCredentials[0].id], [4, 5, 6]);
  assert.equal(ch.options.publicKey.challenge, "AAEC"); // the reply is not changed
  const buf = (a) => new Uint8Array(a).buffer;
  const c = { id: "x", rawId: buf([1, 2]), type: "public-key", authenticatorAttachment: "platform", getClientExtensionResults: () => ({}),
    response: { clientDataJSON: buf([3]), authenticatorData: buf([4]), signature: buf([5]), userHandle: buf([6]) } };
  const j = AP.assertionJSON(c);
  assert.equal(j.rawId, b64u(buf([1, 2])));
  assert.equal(j.response.signature, b64u(buf([5])));
  assert.equal(j.response.userHandle, b64u(buf([6])));
});

test("every outcome has its own words; unknown is never not-approved, a refusal never success", () => {
  assert.deepEqual(AP.outcomeText(200, { outcome: "approved" }, "approve"), ["ok", "本人核准已驗證，付款未啟用。"]);
  assert.equal(AP.outcomeText(200, { outcome: "denied" }, "deny")[1], "你已拒絕這筆（已驗證裝置）。");
  assert.match(AP.outcomeText(200, { attempt: { state: "lost" } }, "approve")[1], /沒有送達/);
  assert.match(AP.outcomeText(409, { error: "decision_conflict", attempt: { decision: "approve" } }, "deny")[1], /另一個視窗正在核准這筆/);
  const unknown = AP.outcomeText(503, { error: "result_unknown" }, "approve")[1];
  assert.match(unknown, /確認中/);
  assert.doesNotMatch(unknown, /沒有核准|未核准/);
  const refused = AP.outcomeText(409, { error: "signer_refused", code: "ASSERTION_INVALID_SIGNATURE" }, "approve");
  assert.equal(refused[0], "error");
  assert.doesNotMatch(refused[1], /已驗證/);
  assert.match(AP.outcomeText(409, { error: "signer_refused", code: "CREDENTIAL_REVOKED" }, "approve")[1], /已停用/);
  assert.match(AP.outcomeText(502, { error: "signer_mismatch" }, "approve")[1], /沒有要求裝置驗證/);
});

test("a stored attempt says where it stands; past the deadline an unknown one says it cannot be confirmed", () => {
  assert.match(AP.attemptText({ state: "unknown" }, "pending")[1], /確認中/);
  assert.match(AP.attemptText({ state: "unknown" }, "expired")[1], /無法確認/);
  assert.equal(AP.attemptText({ state: "verified" }, "approved"), null);
  assert.equal(AP.openAttempt({ state: "refused", challenge_id: "chl-1" }), true);
  assert.equal(AP.openAttempt({ state: "refused" }), false);
  assert.equal(AP.openAttempt({ state: "lost" }), false);
});

test("the three sources are named apart", () => {
  assert.equal(AP.sourceText({ agent: "fake", authenticator: "virtual", payment: "disabled" }), "Agent：fake・驗證器：virtual authenticator（測試用）・付款：未啟用");
  assert.match(AP.sourceText({ agent: "fake", authenticator: "device", payment: "disabled" }), /已登記的裝置/);
});

// Review R4/R5: a decided record is worded by its own method, whatever the
// host's mode; the drawer's grant too. Only what still waits follows the mode.
test("a decided record is worded by its own method, never the host's mode", async () => {
  const A = await import("../adapter.js");
  const dec = (status, reason) => ({ version: A.V1, policy_verdict: "Ask", reason, approval_status: status, payment_status: "not_enabled" });
  const ev = (seq, type, extra = {}) => ({ event_id: "e" + seq, event_type: type, sequence: seq, evidence_mode: "fixture", source: "host", observed_at: 1790726450 + seq, purchase_order_id: "po-1", ...extra });
  const snap = (method) => {
    const by = method === "webauthn" ? "webauthn:approver-a" : "sim-approver-a";
    const events = [ev(1, "TASK_STARTED", { purchase_order_id: "" }), ev(2, "PURCHASE_PROPOSED"), ev(3, "POLICY_DECIDED", { decision: dec("pending", "HUMAN_APPROVAL_REQUIRED") }),
      ev(4, "APPROVED", { decision: dec("approved", method === "webauthn" ? "WEBAUTHN_APPROVAL" : "SIMULATED_APPROVAL"), authorization_method: method })];
    const p = { purchase_order_id: "po-1", decision: dec("approved", "x"), decided_at: 1790726453, approval_closed: "approved", closed_at: 1790726454,
      approval: { approval_session_id: "aps-1", state: "approved", resolved_by: by, grant: { jti: "grant-1", authorization_method: method, audience: "a", expires_at: 1790726999 } } };
    return { task: { task_id: "t-1", consumer_id: "consumer-a", status: "running", as_of_sequence: 4, served_at: 1790726470, purchases: [p], contract_version: A.V1, payment_capability: "not_enabled" }, events };
  };
  try {
    for (const mode of ["webauthn", "simulated"]) {
      A.setApprovalMode(mode);
      const sim = snap("simulated"), dev = snap("webauthn");
      assert.equal(A.card(sim, null, { mode: "live" }).status, "已核准（模擬），未啟用付款", mode);
      assert.equal(A.card(dev, null, { mode: "live" }).status, "本人核准已驗證，未啟用付款", mode);
      assert.equal(A.scene(sim).label, "已核准（模擬）：未啟用付款，不會取貨", mode);
      assert.equal(A.scene(dev).label, "本人核准已驗證：未啟用付款，不會取貨", mode);
      assert.ok(A.card(dev, null, { mode: "live" }).rows.some(([k, v]) => k === "授權" && v.startsWith("本人核准 grant ")), mode);
      assert.ok(A.card(sim, null, { mode: "live" }).rows.some(([k, v]) => k === "授權" && v.startsWith("模擬 grant ")), mode);
      const rows = (s) => A.evidence(s, "po-1", { mode: "live", catalog: null }).sections.find((x) => x.title === "裁決");
      const g = (s) => rows(s).rows.find(([k]) => k === "grant")[1];
      assert.match(g(dev), /本人核准；付款未啟用/, mode);
      assert.match(g(sim), /模擬：Signer 樁拒絕/, mode);
      assert.match(rows(dev).note, /Signer 驗證本人已登記的裝置/, mode);
      assert.match(rows(sim).note, /fake 模擬/, mode);
    }
  } finally {
    A.setApprovalMode("simulated");
  }
});

test("an expired session's unknown attempt and a mismatched result say no approval (N9, N10)", () => {
  const [kind, text] = AP.attemptText({ state: "unknown" }, "expired");
  assert.equal(kind, "closed");
  assert.match(text, /沒有核准/);
  const [k2, t2] = AP.outcomeText(200, { outcome: "mismatch" }, "approve");
  assert.equal(k2, "error");
  assert.match(t2, /沒有採用/);
});
