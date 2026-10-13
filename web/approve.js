// approve.js: A2's person's approval in /host, without any DOM, so the
// rules can be tested in Node (web/test/approve.test.mjs;
// docs/commercial-human-approval-design.md 4.7, 6.3, 7, 8). The Signer
// decides; this only checks that a challenge is this operation's before
// the device is asked, converts WebAuthn's binary fields, and words each
// state with fixed texts (never "approved" for something unknown).

import { b64u, unb64u } from "./adapter.js";

// sameChallenge: the challenge the host returned is the one this click
// asked for (consumer, session, intent and decision); otherwise the page
// stops and asks the device nothing.
export function sameChallenge(ch, op) {
  return !!ch && !!op && ch.consumer_id === op.consumer && ch.approval_session_id === op.session && ch.intent_hash === op.intent && ch.decision === op.decision
    && typeof ch.challenge_id === "string" && !!ch.options && !!ch.options.publicKey;
}

// requestOptions turns the Signer's options into navigator.credentials.get's.
export function requestOptions(options) {
  const p = { ...options.publicKey };
  p.challenge = unb64u(p.challenge);
  p.allowCredentials = (p.allowCredentials || []).map((c) => ({ ...c, id: unb64u(c.id) }));
  return p;
}

// assertionJSON is the browser's credential as the Signer reads it.
export function assertionJSON(c) {
  const r = c.response;
  const out = { id: c.id, rawId: b64u(c.rawId), type: c.type, clientExtensionResults: c.getClientExtensionResults ? c.getClientExtensionResults() : {},
    response: { clientDataJSON: b64u(r.clientDataJSON), authenticatorData: b64u(r.authenticatorData), signature: b64u(r.signature) } };
  if (c.authenticatorAttachment) out.authenticatorAttachment = c.authenticatorAttachment;
  if (r.userHandle) out.response.userHandle = b64u(r.userHandle);
  return out;
}

// CODES: the Signer's fixed codes in the page's words.
export const CODES = {
  ASSERTION_INVALID: "裝置的回應沒有通過驗證",
  ASSERTION_INVALID_CHALLENGE: "裝置簽的不是這一次的驗證",
  ASSERTION_INVALID_ORIGIN: "驗證來自不同的網址",
  ASSERTION_INVALID_RPID: "驗證的網站不符",
  ASSERTION_INVALID_TYPE: "驗證的種類不符",
  ASSERTION_INVALID_FLAGS: "裝置沒有完成本人驗證",
  ASSERTION_INVALID_SIGNATURE: "裝置的簽章不正確",
  ASSERTION_COUNTER: "裝置的計數器異常，已停用待管理者處理",
  CREDENTIAL_REVOKED: "這個裝置已停用，請找管理者重新登記",
  CREDENTIAL_SUSPECT: "這個裝置已停用，請找管理者重新登記",
  CREDENTIAL_PENDING: "這個裝置還沒有經管理者確認",
  CREDENTIAL_UNKNOWN: "這位客人還沒有登記核准裝置",
  APPROVER_UNKNOWN: "這位客人沒有設定核准者",
  APPROVER_CHANGED: "核准設定已變更，請重新核准",
  CHALLENGE_EXPIRED: "這次驗證已過期（後端時間為準）",
  CHALLENGE_CLOSED: "這次驗證已關閉",
  CHALLENGE_USED: "這次驗證已經用過",
  CHALLENGE_OPEN: "這筆還有一個進行中的驗證，請稍候或先取消",
  SESSION_DECIDED: "這筆已經有本人決定",
  AUTHORIZATION_EXPIRED: "已過期（後端時間為準）",
  INTENT_MISMATCH: "請購內容與這次驗證不一致",
};
export const codeText = (c) => CODES[c] || "未通過（" + (c || "?") + "）";

const DECISION = { approve: "核准", deny: "拒絕" };

// outcomeText: what one operation's reply says, as [kind, text]. Unknown
// is never worded as "not approved"; a refusal never as success.
export function outcomeText(status, body, decision) {
  const b = body || {};
  const a = b.attempt || {};
  if (status === 200 && b.outcome === "approved") return ["ok", "本人核准已驗證，付款未啟用。"];
  if (status === 200 && b.outcome === "denied") return ["deny", "你已拒絕這筆（已驗證裝置）。"];
  if (status === 200 && (b.outcome === "closed" || b.outcome === "superseded")) return ["closed", "這筆在驗證完成前已關閉（過期、取消或報價改變）；沒有核准。"];
  if (status === 200 && a.state === "lost") return ["warn", "驗證結果沒有送達，這次沒有核准。請按「重新核准」。"];
  if (status === 200 && a.state === "closed") return ["idle", "已取消這次驗證；這筆仍在等你決定。"];
  if (status === 200 && a.state === "expired") return ["closed", "已過期（後端時間為準）；這次驗證沒有完成。"];
  if (b.error === "decision_conflict") return ["warn", "另一個視窗正在" + (DECISION[a.decision] || "處理") + "這筆。要改變決定，請先「取消這次驗證」。"];
  if (b.error === "signer_refused") return ["error", "驗證沒有通過（" + codeText(b.code) + "）；沒有" + (DECISION[decision] || "決定") + "。"];
  if (b.error === "result_unknown") return ["warn", "結果確認中，請不要重複操作。"];
  if (b.error === "store_unavailable" && b.state_not_saved) return ["warn", "後端已收到，但這次結果還沒有存好；畫面會以同一個編號確認。"];
  if (b.error === "session_closed") return ["closed", "這筆已經結束，不能再" + (DECISION[decision] || "處理") + "。"];
  if (b.error === "signer_mismatch") return ["error", "回傳的資料和你這次的操作不一致，已停止，沒有要求裝置驗證。"];
  if (b.error === "not_found") return ["error", "找不到這一筆（可能不是這位客人的）"];
  if (b.error === "attempt_busy") return ["warn", "另一次驗證的結果還在確認中，請稍候。"];
  return ["error", "未完成（" + (b.error || status) + "）"];
}

// attemptText: what a stored attempt says when the page (re)opens.
export function attemptText(a, sessionState) {
  if (!a) return null;
  if (sessionState && sessionState !== "pending") {
    if (a.state === "unknown" || a.state === "verifying") return ["warn", "無法確認這次驗證的結果；這筆已依後端期限關閉。"];
    return null;
  }
  switch (a.state) {
    case "requested": case "unknown": case "verifying": return ["warn", "結果確認中，請不要重複操作。"];
    case "challenged": return ["idle", "有一個進行中的" + (DECISION[a.decision] || "") + "驗證；可以繼續，或先「取消這次驗證」。"];
    case "refused": return a.challenge_id ? ["error", "上一次驗證沒有通過（" + codeText(a.error_code) + "）；可以再試，或先「取消這次驗證」。"] : ["error", "上一次沒有開始驗證（" + codeText(a.error_code) + "）。"];
    case "lost": return ["warn", "驗證結果沒有送達，這次沒有核准。請按「重新核准」。"];
    case "expired": return ["closed", "上一次驗證已過期（後端時間為準）。"];
  }
  return null;
}

// openAttempt: the attempt still holds the session (a click shares it or
// must be cancelled first).
export const openAttempt = (a) => !!a && (["requested", "challenged", "verifying", "unknown"].includes(a.state) || (a.state === "refused" && !!a.challenge_id));

// sourceText: the three sources, apart (design 2, 8).
export function sourceText(s) {
  const auth = s && s.authenticator === "virtual" ? "virtual authenticator（測試用）" : "已登記的裝置";
  return "Agent：" + (s && s.agent === "fake" ? "fake" : "?") + "・驗證器：" + auth + "・付款：" + (s && s.payment === "disabled" ? "未啟用" : "?");
}
