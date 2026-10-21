// approve.js: A2's person's approval in /host, without any DOM, so the
// rules can be tested in Node (web/test/approve.test.mjs;
// docs/commercial-human-approval-design.md 4.7, 6.3, 7, 8). The Signer
// decides; this only checks that a challenge is this operation's before
// the device is asked, converts WebAuthn's binary fields, and words each
// state with fixed texts (never "approved" for something unknown).

import { b64u, unb64u, byDevice, VOICE, VOICE_FAILURES, voiceText, paymentText, paysOn, PAYING } from "./adapter.js";

export { VOICE, VOICE_FAILURES };

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
  // V1 (docs/commercial-v1.md)
  VOICE_REQUIRED: "這次核准需要語音挑戰",
  VOICE_NOT_ENROLLED: "核准者還沒有登記聲紋，無法核准（請找管理者）",
  VOICE_MERCHANT_UNKNOWN: "Signer 沒有這個收款人的唸法設定，無法核准",
  VOICE_SIGNER_UNAVAILABLE: "Signer 讀不到聲紋設定，這次沒有核准（請找管理者）",
};


// voiceGrant: what a webauthn grant says of its voice ("" when none was asked).
export function voiceGrant(g) {
  if (!g || !g.voice) return "";
  return g.voice === "verified" ? "語音挑戰通過" : "降級：語音不可用（" + voiceText(g.voice_reason) + "）";
}
export const codeText = (c) => CODES[c] || "未通過（" + (c || "?") + "）";

const DECISION = { approve: "核准", deny: "拒絕" };

// outcomeText: what one operation's reply says, as [kind, text]. Unknown
// is never worded as "not approved"; a refusal never as success.
// pays: a paying capability (adapter.js PAYING): an approval is not yet paid,
// rather than never paid.
export function outcomeText(status, body, decision, pays = false) {
  // On a paying host the operation's line stays on the panel while the payment
  // goes on: it names where that is told, not a "not paid" that turns false.
  const off = pays ? "Host 隨後付款；付款進度以上方與請購卡片為準。" : "付款未啟用。";
  const b = body || {};
  const a = b.attempt || {};
  const grant = b.item && b.item.purchase && b.item.purchase.approval && b.item.purchase.approval.grant;
  if (status === 200 && b.outcome === "approved" && grant && grant.voice === "verified") return ["ok", "本人核准已驗證（裝置＋語音挑戰），" + off];
  if (status === 200 && b.outcome === "approved" && grant && grant.voice === "unavailable") return ["warn", "本人核准已驗證（裝置）；" + voiceGrant(grant) + "。" + off];
  if (status === 200 && b.outcome === "approved") return ["ok", "本人核准已驗證，" + off];
  if (status === 200 && b.outcome === "denied" && VOICE_FAILURES.includes(b.code)) return ["deny", "語音挑戰未通過（" + voiceText(b.code) + "），這筆已拒絕；不會付款。"];
  if (status === 200 && b.outcome === "denied") return ["deny", "你已拒絕這筆（已驗證裝置）。"];
  if (status === 200 && b.outcome === "mismatch") return ["error", "Signer 的結果和這次操作不一致，沒有採用。"];
  if (status === 200 && (b.outcome === "closed" || b.outcome === "superseded")) return ["closed", "裝置的驗證沒有被採用（這筆已關閉、task 已結束，或 Signer 的核准已過期）；不會付款。"];
  if (status === 200 && a.state === "lost") return lostText(a);
  if (status === 200 && a.state === "closed") return ["idle", "已取消這次驗證；這筆仍在等你決定。"];
  if (status === 200 && a.state === "expired") return ["closed", "已過期（後端時間為準）；這次驗證沒有完成。"];
  if (b.error === "decision_conflict") return ["warn", "另一個視窗正在" + (DECISION[a.decision] || "處理") + "這筆。要改變決定，請先「取消這次驗證」。"];
  if (b.error === "signer_refused") return ["error", "驗證沒有通過（" + codeText(b.code) + "）；沒有" + (DECISION[decision] || "決定") + "。"];
  if (b.error === "result_unknown") return ["warn", "結果確認中，請不要重複操作。"];
  if (b.error === "store_unavailable" && b.state_not_saved) return ["warn", "後端已收到，但這次結果還沒有存好；畫面會以同一個編號確認。"];
  if (b.error === "task_ended") return ["closed", "task 已結束（CLI 逾時，或另一筆請購被拒絕）：這筆不能核准，也不會付款。"];
  if (b.error === "session_closed") return ["closed", "這筆已經結束，不能再" + (DECISION[decision] || "處理") + "。"];
  if (b.error === "signer_mismatch") return ["error", "回傳的資料和你這次的操作不一致，已停止，沒有要求裝置驗證。"];
  if (b.error === "not_found") return ["error", "找不到這一筆（可能不是這位客人的）"];
  if (b.error === "attempt_busy") return ["warn", "另一次驗證的結果還在確認中，請稍候。"];
  return ["error", "未完成（" + (b.error || status) + "）"];
}

// lostText (review 5-2): a lost attempt by its own decision, pointing to
// the button the panel shows for it.
const lostText = (a) => (a.decision === "deny" ? ["warn", "驗證結果沒有送達，這次沒有拒絕。請再按「用裝置拒絕這筆」。"]
  : ["warn", "驗證結果沒有送達，這次沒有核准。請按「重新核准」。"]);

// attemptText: what a stored attempt says when the page (re)opens.
export function attemptText(a, sessionState) {
  if (!a) return null;
  if (sessionState && sessionState !== "pending") {
    // A closed session's open attempt has an end (design 7; review S-B):
    // unknown is never worded as not approved (N-b); one never sent to the
    // Signer did not finish. A decided session's heading says it all.
    const why = { expired: "這筆已依後端期限關閉", withdrawn: "這筆已關閉（任務取消）", superseded: "這筆已關閉（報價改變）" }[sessionState];
    if (!why || !openAttempt(a)) return null;
    return a.state === "unknown" || a.state === "verifying" ? ["warn", "無法確認這次驗證的結果；" + why + "。"] : ["closed", why + "；這次驗證沒有完成。"];
  }
  switch (a.state) {
    case "requested": case "unknown": case "verifying": return ["warn", "結果確認中，請不要重複操作。"];
    case "challenged": return ["idle", "有一個進行中的" + (DECISION[a.decision] || "") + "驗證；可以繼續，或先「取消這次驗證」。"];
    case "refused": return a.challenge_id ? ["error", "上一次驗證沒有通過（" + codeText(a.error_code) + "）；可以再試，或先「取消這次驗證」。"] : ["error", "上一次沒有開始驗證（" + codeText(a.error_code) + "）。"];
    case "lost": return lostText(a);
    case "expired": return ["closed", "上一次驗證已過期（後端時間為準）。"];
  }
  return null;
}

// openAttempt: the attempt still holds the session (a click shares it or
// must be cancelled first).
export const openAttempt = (a) => !!a && (["requested", "challenged", "verifying", "unknown"].includes(a.state) || (a.state === "refused" && !!a.challenge_id));

// decidedText (review S1): an approved or denied record in either panel,
// worded by its own method, never the host's mode; null for the rest
// (each panel words its closures).
const DONE = {
  device: { approved: ["ok", "本人核准已驗證，付款未啟用。", "這筆只留下 Signer 驗證的本人核准紀錄；付款未啟用，不會交付商品。"],
    denied: ["deny", "你已拒絕這筆（已驗證裝置）。", "Agent 不會取得這一筆；沒有付款。"] },
  simulated: { approved: ["ok", "已核准（模擬），未啟用付款", "這筆只留下模擬授權（grant）；它不能付款，也不會交付商品。"],
    denied: ["deny", "已拒絕", "Agent 不會取得這一筆；沒有付款。"] }, // P1-a's words
};
const PAID_NOTE = "task 仍進行時，Host 會為核准的這一筆開始付款（測試 USDC）；是否已付款、是否交付，以請購卡片為準。";
// V1: a voice outcome words its record: denied by the voice (never as the
// approver's own deny), approved with the voice, or approved degraded.
// pays: the host pays a person's approval (a paying capability, adapter.js
// PAYING); a simulated one never pays.
export const decidedText = (state, p, pays = false) => {
  const g = p && p.approval && p.approval.grant;
  // With payments, the payment as the card says it once there is one; "not yet paid" before.
  const now = () => (p && p.payment ? "；" + paymentText(p.payment) + "。" : "，尚未付款。");
  const paid = (t) => (pays || p && p.payment ? t.replace("，付款未啟用。", now()) : t); // a recorded payment is said even on a host without --payments
  if (state === "denied" && p && p.closed_reason === "VOICE_CHALLENGE_FAILED") return ["deny", "語音挑戰未通過，這筆已拒絕。", "沒有發出授權，不會付款；這筆不能再核准。"];
  if (state === "approved" && byDevice(p) && g && g.voice === "unavailable") {
    return ["warn", paid("本人核准已驗證（語音降級），付款未啟用。"), "語音不可用（" + voiceText(g.voice_reason) + "）：只憑摘要確認與裝置驗證核准，這筆記為降級。"];
  }
  if (state === "approved" && byDevice(p) && g && g.voice === "verified") return ["ok", paid("本人核准已驗證（裝置＋語音），付款未啟用。"), pays || p.payment ? PAID_NOTE : DONE.device.approved[2]];
  if (state === "approved" && byDevice(p) && (pays || p.payment)) return ["ok", "本人核准已驗證" + now(), PAID_NOTE];
  return DONE[byDevice(p) ? "device" : "simulated"][state] || null;
};

// recordTag (review A): the tag and source line of a record decided by the
// other method than the panel's ("simulated" or "device"); null for a
// pending record, a record nobody decided, or one of the panel's method.
export function recordTag(p, panel) {
  const ap = p && p.approval;
  if (!ap || ap.state === "pending" || !ap.resolved_by || (byDevice(p) ? "device" : "simulated") === panel) return null;
  return byDevice(p) ? ["本人核准", "這筆由 Signer 驗證已登記的裝置・付款：未啟用"] : ["模擬核准", "這筆是 fake 模擬核准（主持人扮演，不是本人驗證）・付款：未啟用"];
}

// panelNote (review B): a closed session's stored attempt text (design 7:
// the unknown has an end) wins over the last operation's note.
// Otherwise, on a closed session only a final note (ok, deny, closed)
// stays: one written while it was pending ("仍在等你決定") no longer holds.
export function panelNote(note, stored, pending) {
  const s = stored && { kind: stored[0], text: stored[1] };
  if (pending) return note || s;
  return s || (note && ["ok", "deny", "closed"].includes(note.kind) ? note : null);
}

// grantLine: a grant by its own method (pays: a paying capability).
export function grantLine(g, pays = false) {
  const id = String(g.jti).slice(0, 18) + "…";
  return g.authorization_method === "webauthn"
    ? "本人核准紀錄 " + id + "（authorization_method webauthn，由 Signer 驗證；Host 只記錄）" + (g.voice ? "・" + voiceGrant(g) : "")
      + (pays ? "・付款依據（是否已付款見請購卡片）" : "・付款未啟用")
    : "模擬授權 " + id + "（authorization_method " + g.authorization_method + "，audience " + g.audience + "）· Signer 樁：拒絕模擬授權（GRANT_METHOD_NOT_ACCEPTED）· 未啟用付款";
}

// sourceText: the three sources, apart (design 2, 8).
export function sourceText(s) {
  const auth = s && s.authenticator === "virtual" ? "virtual authenticator（測試用）" : "已登記的裝置";
  const pay = s && s.payment === "disabled" ? "未啟用" : s && paysOn(s.payment) ? PAYING[s.payment] + "（測試 USDC）" : "?";
  return "Agent：" + (s && s.agent === "fake" ? "fake" : "?") + "・驗證器：" + auth + "・付款：" + pay;
}

// V1: a recording as the Signer takes it: a WAV, 16 kHz mono 16-bit PCM,
// at most maxSeconds. samples are the microphone's floats at rate; they
// are resampled by linear interpolation.
export function wav16k(samples, rate, maxSeconds = 10) {
  const n = Math.min(Math.floor(samples.length * 16000 / rate), 16000 * maxSeconds);
  const out = new DataView(new ArrayBuffer(44 + n * 2));
  const str = (o, s) => { for (let i = 0; i < s.length; i++) out.setUint8(o + i, s.charCodeAt(i)); };
  str(0, "RIFF"); out.setUint32(4, 36 + n * 2, true); str(8, "WAVE"); str(12, "fmt ");
  out.setUint32(16, 16, true); out.setUint16(20, 1, true); out.setUint16(22, 1, true); out.setUint32(24, 16000, true);
  out.setUint32(28, 32000, true); out.setUint16(32, 2, true); out.setUint16(34, 16, true); str(36, "data"); out.setUint32(40, n * 2, true);
  for (let i = 0; i < n; i++) {
    const x = i * rate / 16000, j = Math.floor(x), f = x - j;
    const v = (samples[j] || 0) * (1 - f) + (samples[j + 1] || 0) * f;
    out.setInt16(44 + i * 2, Math.max(-1, Math.min(1, v)) * 0x7fff, true);
  }
  return new Uint8Array(out.buffer);
}

export const seconds = (wav) => (wav.length - 44) / 32000;

export function base64(bytes) {
  let s = "";
  for (let i = 0; i < bytes.length; i += 0x8000) s += String.fromCharCode(...bytes.subarray(i, i + 0x8000));
  return btoa(s);
}

// peak: the loudest sample's magnitude (0 to 1). Below QUIET a recording
// holds no speech worth sending (an unused or muted microphone).
export const QUIET = 0.02;
export function peak(samples) {
  let p = 0;
  for (const x of samples) p = Math.max(p, Math.abs(x));
  return p;
}

// micOptions: the microphone choices, Chrome's default first; an input
// without a label (no permission yet) is numbered. A choice no longer in the
// list stays (review 1): the recording still asks for it and is not sent.
export function micOptions(list, choice = "") {
  const opts = [["", "Chrome 預設"], ...list.map((m, i) => [m.id, m.label || "麥克風 " + (i + 1)])];
  if (choice && !list.some((m) => m.id === choice)) opts.push([choice, "之前選的麥克風（目前找不到）"]);
  return opts;
}

// micFailure: what a getUserMedia failure means. With Chrome's default
// microphone, any failure is no microphone (D4 a: degraded). With a chosen
// one, only a refused permission is (D4 a); anything else (gone, busy,
// unreadable) is a choice to make again, never a degraded approval (review 4).
export function micFailure(e, deviceId) {
  const refused = e && (e.name === "NotAllowedError" || e.name === "SecurityError");
  return !deviceId || refused ? { unavailable: "device" } : { missing: true };
}
