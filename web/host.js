// host.js: the host's local page /host (docs/demo-operation-design.md
// section 6.3, slice 3.1). The same Dashboard and Unity shop as / (app.js),
// plus a token file loaded into this page's memory, consumer switching,
// 「清除全部憑證」, a manual viewer mode and the readiness panel.
//
// The host token is used for GET /api/host/status only; task and events
// are always read with the chosen consumer's viewer token (app.js). Every
// change of credentials starts a new generation in hostcreds.js, and the
// Dashboard is cleared with it, so no late step shows an old identity.
// Read-only: nothing here starts, stops or changes a task.

import { forget, useViewer, onUnauthorized, unityInfo } from "./app.js";
import { createHostCreds } from "./hostcreds.js";

const $ = (id) => document.getElementById(id);
const READY_MS = 5000; // readiness is read every 5 s while the page is visible

async function call(token, path, signal) {
  const res = await fetch(path, { headers: { Authorization: "Bearer " + token }, cache: "no-store", credentials: "omit", signal });
  let body = null;
  try { body = await res.json(); } catch { /* not JSON: status alone decides */ }
  return { status: res.status, body };
}

let shownGen = -1;  // the generation the Dashboard was last cleared for
let viewerGen = -1; // the generation whose viewer token the Dashboard uses
let readyTimer = null;
let lastReady = null;

const creds = createHostCreds({ call, onChange: render });

// A 401 the Dashboard saw for its current viewer: the whole file is stale.
onUnauthorized(() => { if (!creds.unauthorized(viewerGen)) forget(); });

function render(v) {
  if (v.gen !== shownGen) {
    // New generation: the previous identity leaves the panels and the shop.
    shownGen = v.gen;
    forget(v.message || "請載入 token 檔或手動輸入 viewer token");
    lastReady = null;
    clearTimeout(readyTimer);
  }
  const box = $("creds");
  box.dataset.mode = v.mode;
  box.dataset.gen = String(v.gen);
  box.dataset.consumers = v.consumers.join(",");
  box.textContent = v.message || (v.mode === "none" ? "尚未載入 token 檔" : "");
  const pick = $("consumer-pick");
  pick.replaceChildren();
  for (const c of v.consumers) {
    const b = document.createElement("button");
    b.type = "button";
    b.dataset.consumer = c;
    b.textContent = "以 " + c + " 查看";
    b.addEventListener("click", () => switchTo(c));
    pick.append(b);
  }
  renderReadiness();
  if (v.hasHost) pollReadiness();
}

// switchTo uses a loaded viewer token; app.js clears the previous viewer's
// task, panels, replay and shop first and drops its late replies.
function switchTo(consumer) {
  const token = creds.viewerToken(consumer);
  if (!token) return;
  viewerGen = creds.gen;
  useViewer(token);
}

// pollReadiness: one loop at a time. A poll of an older generation commits
// nothing; when it ends after a change, the current generation polls at once.
let polling = false;
async function pollReadiness() {
  clearTimeout(readyTimer);
  if (polling) return;
  polling = true;
  const g = creds.gen;
  try {
    if (!document.hidden) {
      const r = await creds.readiness();
      if (r && g === creds.gen) { lastReady = r; renderReadiness(); }
    }
  } finally {
    polling = false;
  }
  if (creds.view().hasHost) readyTimer = setTimeout(pollReadiness, g === creds.gen ? READY_MS : 0);
}

const time = (s) => (s ? new Date(s * 1000).toLocaleTimeString("zh-TW", { hour12: false }) : "");

function storeText(st) {
  const history = st.last_error ? "；最近一次錯誤 " + st.last_error + "（" + time(st.last_error_at) + "）" : "";
  if (st.state === "ok") return "正常：最近完成的到期檢查 " + time(st.checked_at) + (history ? history + "，之後已恢復" : "") + "（目標每 " + st.target_period_seconds + " 秒，不是延遲保證）";
  if (st.state === "error") return "錯誤：最近一次到期檢查失敗 " + st.last_error + "（" + time(st.checked_at) + "）";
  return "未確認：尚未完成到期檢查" + history;
}

function unitySceneText() {
  const u = unityInfo();
  const s = $("unity-status").dataset.state;
  if (s === "failed") return ["failed", "未就緒（載入或啟動失敗）"];
  if (!u.ready) return ["loading", "載入中（未確認）"];
  return ["ok", u.art === "limezu" ? "已載入（LimeZu 素材）" : "已載入（placeholder，LimeZu 待接入）"];
}

function renderReadiness() {
  const table = $("readiness");
  const rows = [];
  const v = creds.view();
  const scene = unitySceneText();
  if (!v.hasHost) {
    rows.push(["就緒狀態", "unconfirmed", v.mode === "manual" ? "需要 token 檔（手動 viewer 模式不讀就緒狀態）" : "未確認：請先載入 token 檔"]);
  } else if (!lastReady) {
    rows.push(["就緒狀態", "unconfirmed", "未確認：讀取中"]);
  } else if (lastReady.error) {
    rows.push(["就緒狀態", "error", "無法取得就緒狀態（" + lastReady.error + "），未確認"]);
  } else {
    const st = lastReady.status;
    const missing = ["WebGL.data", "WebGL.framework.js", "WebGL.loader.js", "WebGL.wasm"].filter((f) => !st.unity_build.files.includes(f));
    rows.push(["HTTP", st.http === "ok" ? "ok" : "error", st.http === "ok" ? "就緒" : "異常"]);
    rows.push(["Store", st.store.state, storeText(st.store)]);
    rows.push(["fake CLI", st.fake_cli.state, st.fake_cli.state === "ok" ? "就緒（fake-claude，不是 Claude Code）" : "未啟用"]);
    rows.push(["真實 CLI", "off", "未啟用（fake 模式）"]);
    rows.push(["Unity 建置", st.unity_build.state, st.unity_build.state === "ok" ? "就緒（4 個建置檔）" : "缺少 " + missing.join("、") + "：請先完成首次準備"]);
    rows.push(["付款", "off", "無付款能力"]);
    rows.push(["檢查時間", "info", time(st.served_at) + "（Store 時間）"]);
  }
  rows.push(["Unity 場景", scene[0], scene[1]]);
  table.replaceChildren(...rows.map(([k, state, text]) => {
    const tr = document.createElement("tr");
    tr.dataset.item = k;
    tr.dataset.state = state;
    const th = document.createElement("th");
    th.textContent = k;
    const td = document.createElement("td");
    td.textContent = text;
    tr.append(th, td);
    return tr;
  }));
}

$("token-file").addEventListener("change", (ev) => {
  const input = ev.target;
  const file = input.files && input.files[0];
  input.value = ""; // the field is emptied at once; the File object is read once below
  if (file) creds.load(file);
});
$("clear-all").addEventListener("click", () => creds.clearAll("已清除全部憑證"));
// Manual viewer mode: before app.js reads the typed token, every loaded
// token goes (capture phase on document runs before the form's handler).
document.addEventListener("submit", (ev) => {
  if (ev.target.id !== "auth-form") return;
  creds.manual();
  viewerGen = creds.gen;
}, true);
// 「清除目前 viewer」 keeps the loaded file; app.js clears the viewer only.
new MutationObserver(renderReadiness).observe($("unity-status"), { attributes: true, attributeFilter: ["data-state"] });
document.addEventListener("visibilitychange", () => { if (!document.hidden && creds.view().hasHost) pollReadiness(); });
// Leaving the page: drop every credential, stop polling, end every step in
// flight; app.js clears the Dashboard and the shop. Nothing returns on a
// restored page: the file has to be chosen again.
window.addEventListener("pagehide", () => { clearTimeout(readyTimer); creds.clearAll("已離開頁面：憑證已清除，請重新載入 token 檔"); });

render(creds.view());
