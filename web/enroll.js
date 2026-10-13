// enroll.js: the person's side of an E1 enrollment (A2). The ID and code
// go to the host in a POST body, never a URL; the code is not kept or
// shown. Messages are fixed texts with the Signer's fixed codes.
import { b64u, unb64u } from "./adapter.js";

const $ = (id) => document.getElementById(id);
const say = (text, kind) => { $("enroll-status").textContent = text; $("enroll-status").dataset.kind = kind || "idle"; };
const CODES = {
  ENROLLMENT_CODE: "確認碼不正確（連續錯 5 次後這次登記失效）",
  ENROLLMENT_CLOSED: "這次登記已失效（過期、已完成或錯太多次）：請管理者重新 begin",
  ENROLLMENT_UNKNOWN: "找不到這個登記 ID",
  ATTESTATION_INVALID: "裝置的回應沒有通過驗證（例如沒有驗證本人）",
  ATTESTATION_FORMAT: "裝置送出了不接受的 attestation 格式（只接受 none）",
  CREDENTIAL_EXISTS: "這個裝置的 credential 已經登記過",
};

async function post(path, body) {
  const r = await fetch(path, { method: "POST", headers: { "Content-Type": "application/json" }, body: JSON.stringify(body) });
  let v = {};
  try { v = await r.json(); } catch {}
  return { status: r.status, body: v };
}

$("enroll-form").addEventListener("submit", async (ev) => {
  ev.preventDefault();
  const id = $("enroll-id").value.trim(), code = $("enroll-code").value.replace(/[\s-]/g, "").toUpperCase();
  $("enroll-result").hidden = true;
  if (!/^[A-Za-z0-9][A-Za-z0-9._:-]{0,127}$/.test(id) || !/^[A-Z0-9]{8}$/.test(code)) return say("登記 ID 或確認碼的格式不符，沒有送出", "error");
  $("enroll-go").disabled = true;
  try {
    say("讀取登記選項…", "busy");
    const o = await post("/api/enrollments/" + encodeURIComponent(id) + "/options", { code });
    if (o.status !== 200) return say(CODES[o.body.error] || "沒有取得登記選項（" + (o.body.error || o.status) + "）", "error");
    const p = o.body.options.publicKey;
    p.challenge = unb64u(p.challenge);
    p.user.id = unb64u(p.user.id);
    (p.excludeCredentials || []).forEach((c) => { c.id = unb64u(c.id); });
    say("請在裝置上完成驗證…", "busy");
    let c;
    try { c = await navigator.credentials.create({ publicKey: p }); } catch (e) {
      return say(e && e.name === "NotAllowedError" ? "裝置驗證沒有完成（取消、逾時或沒有可用的裝置）；沒有登記" : "瀏覽器拒絕建立 credential（" + (e && e.name) + "）；沒有登記", "error");
    }
    const r = c.response;
    const credential = { id: c.id, rawId: b64u(c.rawId), type: c.type, authenticatorAttachment: c.authenticatorAttachment || undefined,
      clientExtensionResults: c.getClientExtensionResults(),
      response: { clientDataJSON: b64u(r.clientDataJSON), attestationObject: b64u(r.attestationObject), transports: r.getTransports ? r.getTransports() : [] } };
    const f = await post("/api/enrollments/" + encodeURIComponent(id), { code, credential });
    if (f.status !== 200) return say(CODES[f.body.error] || "登記沒有完成（" + (f.body.error || f.status) + "）", "error");
    $("enroll-fp").textContent = f.body.fingerprint;
    $("enroll-flags").textContent = f.body.flags;
    $("enroll-aaguid").textContent = f.body.aaguid;
    $("enroll-result").hidden = false;
    say("已送出，等管理者確認：請在 Signer 終端機執行 enroll confirm，核對兩邊的指紋相同後輸入 yes。確認前這個裝置不能核准。", "ok");
  } finally {
    $("enroll-go").disabled = false;
    $("enroll-code").value = "";
  }
});
