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
