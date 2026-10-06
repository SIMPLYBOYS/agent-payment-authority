// host.js: the host's local page /host (docs/demo-operation-design.md
// section 6.3, slice 3.1). The same Dashboard and Unity shop as / (app.js),
// plus a token file loaded into this page's memory, consumer switching,
// 「清除全部憑證」, a manual viewer mode and the readiness panel.
//
// The host token is used for GET /api/host/status only; task and events
// are always read with the chosen consumer's viewer token (app.js). Every
// change of credentials starts a new generation in hostcreds.js, and the
// Dashboard is cleared with it, so no late step shows an old identity.
//
// Slice 3.2 (section 13): the operator token starts and stops fake runs
// and reads the runs list. A start reply of the current generation opens
// the new task with that consumer's viewer token; a reply of an older one
// does nothing. Clearing the page does not withdraw a run the host accepted.
//
// Slice 3.4: a run's line keeps its history (what the process and its end
// records did then) apart from the task's current state, read with the
// read-only host token. 「取消 task」 is offered for a task that is not
// running and not ended; the server decides again. A cancel reply counts
// only while the credentials and the Dashboard's view are the ones of the
// click: a clear, a new file, the manual mode, pagehide, or a switch of
// consumer or task drops it; nothing is opened or signed in for it.

import { forget, useViewer, onUnauthorized, onTypedSignIn, unityInfo, openTask, viewKey, refreshNow } from "./app.js";
import { createHostCreds } from "./hostcreds.js";

const $ = (id) => document.getElementById(id);
const READY_MS = 5000; // readiness is read every 5 s while the page is visible

async function call(token, path, signal, post) {
  const headers = { Authorization: "Bearer " + token };
  const init = { headers, cache: "no-store", credentials: "omit", signal };
  if (post) { // a control request: same-origin JSON POST (the browser adds Origin)
    init.method = "POST";
    headers["Content-Type"] = "application/json";
    init.body = JSON.stringify(post.body);
  }
  const res = await fetch(path, init);
  let body = null;
  try { body = await res.json(); } catch { /* not JSON: status alone decides */ }
  return { status: res.status, body };
}

let shownGen = -1;  // the generation the Dashboard was last cleared for
let viewerGen = -1; // the generation whose viewer token the Dashboard uses
let readyTimer = null;
let lastReady = null;
let runsTimer = null;
let lastRuns = null; // { gen, runs } of the current generation
let starting = false;
let cancelling = false;
let lastTasks = null; // { gen, list: Map(task_id -> summary), details: Map(task_id -> task) } or { gen, error }
const RUNS_MS = 2000;
const DETAILS_MAX = 5; // ponytail: task reads per poll for the Ask notes; more runs waiting would read in turn

const creds = createHostCreds({ call, onChange: render });

// A 401 the Dashboard saw for its current viewer: the whole file is stale.
onUnauthorized(() => { if (!creds.unauthorized(viewerGen)) forget(); });

function render(v) {
  if (v.gen !== shownGen) {
    // New generation: the previous identity leaves the panels and the shop.
    shownGen = v.gen;
    forget(v.message || "請載入 token 檔或手動輸入 viewer token");
    lastReady = null;
    lastRuns = null;
    lastTasks = null;
    starting = false;
    cancelling = false;
    clearTimeout(readyTimer);
    clearTimeout(runsTimer);
    setRunStatus("", "idle");
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
  renderControl();
  if (v.hasHost) { pollReadiness(); pollRuns(); }
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
      if (r && g === creds.gen) { lastReady = r; renderReadiness(); renderControl(); }
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

// ---- host control (slice 3.2) ----

function setRunStatus(text, state) {
  const p = $("run-status");
  p.textContent = text;
  p.dataset.state = state;
}

const ERRORS = {
  run_active: "上一場尚未結束；請先等它結束或停止它",
  no_free_task: "展示 task 已用完：請設定新的 APA_DB_PATH 後重啟，或擴充 demo registry",
  request_id_conflict: "請求編號與另一個情境衝突，未開始",
  unknown_scenario: "沒有這個情境",
  store_unavailable: "Store 不可用，未開始",
  run_not_running: "這一場已不在執行中",
};

// CANCEL_ERRORS: a refused cancel; the task's record stays as it was.
const CANCEL_ERRORS = {
  not_found: "Store 沒有這個 task（未建立），未取消",
  run_active: "這個 task 的場次仍在執行：請用「停止」，未取消",
  already_ended: "task 已因 CLI 逾時結束，未取消；原紀錄保留",
  not_eligible: "task 已是 Deny（終態），未取消；Deny 原因保留",
  store_unavailable: "Store 不可用，取消結果尚未確認（可能已由其他寫入完成）；請看最近場次重新讀取的目前狀態",
};

// runText is section 13.5: the run's process, its end record and the
// failures, kept apart: the run's history, as it was then. "Complete"
// words only when every record was written. The task's state now is
// taskNow's.
function runText(r) {
  if (r.live === "running") return "進行中";
  if (r.live === "stopping") return "停止中…";
  if (r.record_error) return "程序已結束，但場次紀錄寫入失敗（" + r.record_error + "）";
  if (r.state === "interrupted") return "場次曾中斷（上一個 host 未正常結束）";
  if (r.state !== "ended") return "已受理";
  if (r.store_end_error) return "程序已停止；當時" + (r.outcome === "timeout" ? "逾時" : "取消") + "紀錄寫入失敗（" + r.store_end_error + "）";
  if (r.outcome === "cancelled" && r.store_end === "TASK_CANCELLED") return "已停止，取消已記錄";
  if (r.outcome === "timeout" && r.store_end === "TASK_TIMED_OUT") return "CLI 已逾時（已記錄）";
  if (r.outcome === "skipped") return "未啟動（" + r.detail + "）";
  if (r.detail && r.detail.startsWith("prepare_failed")) return "未建立 task（" + r.detail + "）";
  if (r.outcome === "ok") return "場次完成";
  return "場次結束：" + r.outcome + (r.detail ? "（" + r.detail + "）" : "");
}

// A task whose details were not read (a failed read, or beyond
// DETAILS_MAX): its Asks are unknown, never taken as none.
const UNCONFIRMED = "Ask 狀態未確認（task 詳情未取得）";
const asks = (d) => ((d && d.purchases) || []).filter((p) => p.decision && p.decision.policy_verdict === "Ask");

// taskNow is the task's state now, from the host's task reads; null when
// the run's line needs none (its process is running, or no task was made).
function taskNow(r) {
  if (r.live === "running" || r.live === "stopping") return null;
  if (!lastTasks || lastTasks.gen !== creds.gen) return "目前 task 狀態：讀取中（未確認）";
  if (lastTasks.error) return "目前 task 狀態：讀取失敗（" + lastTasks.error + "），未確認";
  const t = lastTasks.list.get(r.task_id);
  if (!t) return r.outcome === "skipped" || (r.detail || "").startsWith("prepare_failed") ? null : "目前 task：Store 沒有此 task";
  const d = lastTasks.details.get(r.task_id);
  const open = asks(d).some((p) => !p.approval_closed);
  const expired = asks(d).some((p) => p.approval_closed === "expired");
  const wasOpen = r.state === "interrupted" || r.record_error || r.store_end_error || r.store_end !== "TASK_CANCELLED";
  switch (t.status) {
    case "cancelled":
      if (!wasOpen) return null; // the stop's own cancel: the history says it
      return r.store_end_error ? "後續已成功取消 task" : "task 現已取消";
    case "denied": return "Deny：" + (t.deny_reason || "原因未知");
    case "failed":
      return (r.outcome === "timeout" && r.store_end === "TASK_TIMED_OUT" ? "" : "task 已結束（CLI 逾時）；") +
        (!d ? UNCONFIRMED : open ? "未決 Ask 仍依原到期機制處理" : expired ? "Ask 已依原到期機制到期" : "無未決 Ask");
    case "awaiting_approval": return "Ask 等待中；task 可由主持人取消";
    case "running": return d ? (expired ? "Ask 已到期；task 仍可由主持人取消" : "task 未收尾；可由主持人取消") : "task 未收尾（" + UNCONFIRMED + "）；可由主持人取消";
    default: return "目前 task：" + t.status;
  }
}

const cancellable = (r) => {
  const t = r.live !== "running" && r.live !== "stopping" && lastTasks && lastTasks.gen === creds.gen && lastTasks.list && lastTasks.list.get(r.task_id);
  return !!t && (t.status === "awaiting_approval" || t.status === "running");
};

function renderControl() {
  const v = creds.view();
  const box = $("control");
  box.hidden = v.mode !== "file";
  const pick = $("run-scenarios");
  const scenarios = (lastReady && lastReady.status && lastReady.status.runs && lastReady.status.runs.scenarios) || [];
  const runs = (lastRuns && lastRuns.gen === creds.gen && lastRuns.runs) || [];
  const active = runs.some((r) => r.live === "running" || r.live === "stopping");
  pick.replaceChildren(...scenarios.map((sc) => {
    const b = document.createElement("button");
    b.type = "button";
    b.dataset.scenario = sc.id;
    b.textContent = "開始：" + sc.label + "（" + sc.consumer_id + "）";
    b.disabled = starting || active;
    b.addEventListener("click", () => start(sc));
    return b;
  }));
  $("runs").replaceChildren(...runs.map((r) => {
    const li = document.createElement("li");
    li.dataset.run = r.run_id;
    li.dataset.state = r.state;
    li.dataset.live = r.live || "";
    const now = taskNow(r);
    li.textContent = r.task_id + "（" + r.consumer_id + "，" + r.scenario + "）：" + runText(r) + (now ? "；" + now : "");
    if (cancellable(r)) {
      const b = document.createElement("button");
      b.type = "button";
      b.className = "task-cancel";
      b.dataset.task = r.task_id;
      b.textContent = "取消 task " + r.task_id;
      b.disabled = cancelling;
      b.addEventListener("click", () => cancel(r.task_id));
      li.append(" ", b);
    }
    if (r.live === "running") {
      const b = document.createElement("button");
      b.type = "button";
      b.className = "run-stop";
      b.textContent = "停止";
      b.addEventListener("click", () => stop(r.run_id));
      li.append(" ", b);
    }
    return li;
  }));
}

// start: one start operation; only a reply of this generation acts.
async function start(sc) {
  if (starting) return; // one start operation at a time (a second click, even on a re-rendered button, does nothing)
  const g = creds.gen;
  starting = true;
  renderControl();
  setRunStatus("開始中…", "busy");
  const r = await creds.startRun(sc.id);
  if (g !== creds.gen || !r || r.gen !== creds.gen) return; // cleared or replaced: nothing acts
  starting = false;
  renderControl();
  if (r.error) setRunStatus("沒有收到回覆，無法確認是否已受理；請看最近場次（不會自動重新開始）", "error");
  else if (r.status === 202 || r.status === 200) {
    setRunStatus((r.status === 202 ? "已開始 " : "已受理（同一請求）") + r.body.task_id, "ok");
    await openRun(r.body, g);
  } else setRunStatus(ERRORS[r.body && r.body.error] || "未開始（" + r.status + "）", "error");
  pollRuns();
}

// openRun shows the new task with its consumer's viewer token, while the
// generation is still the one the start belonged to.
async function openRun(run, g) {
  const token = creds.viewerToken(run.consumer_id);
  if (!token || g !== creds.gen) return;
  viewerGen = g;
  const signedIn = await useViewer(token);
  if (signedIn !== true || g !== creds.gen) return;
  openTask(run.task_id);
}

async function stop(runId) {
  const g = creds.gen;
  setRunStatus("停止中…", "busy");
  const r = await creds.stopRun(runId);
  if (!r || r.gen !== creds.gen || g !== creds.gen) return;
  if (r.error) setRunStatus("沒有收到回覆；請看最近場次", "error");
  // 202 means accepted, not done: a neutral notice, no longer busy; the
  // committed result is the runs list's (review of 31cc6c7).
  else if (r.status === 202) setRunStatus("場次 " + runId + " 的停止請求已受理；最終結果請看最近場次。", "idle");
  else setRunStatus(ERRORS[r.body && r.body.error] || "未停止（" + r.status + "）", "error");
  pollRuns();
}

// cancel is one cancel operation (section 6.1); a lost reply is "not
// confirmed", never a failure or a success, and nothing is resent.
async function cancel(taskId) {
  if (cancelling) return;
  const g = creds.gen;
  const key = viewKey();
  const current = () => g === creds.gen && key === viewKey();
  cancelling = true;
  renderControl();
  setRunStatus("取消 task " + taskId + " 中…", "busy");
  const r = await creds.cancelTask(taskId);
  if (g !== creds.gen) return; // cleared or replaced: render() has reset everything
  cancelling = false;
  renderControl();
  if (!r) return;
  if (!current()) { // a switch of consumer or task: the old reply is not shown; the runs list reads the Store again
    setRunStatus("取消 task " + taskId + " 的回覆未套用（畫面已切換）；結果以最近場次的目前狀態為準", "idle");
    return pollRuns();
  }
  if (r.error) {
    setRunStatus("取消 task " + taskId + "：結果尚未確認（沒有收到回覆）；請看最近場次的目前狀態，需要時再按一次（伺服器不會重複寫入）", "warn");
  } else if (r.status === 200) {
    const already = r.body && r.body.already === true;
    setRunStatus("取消已記錄：task " + taskId + "，更新畫面中…", "busy");
    refreshNow();
    const t = await creds.hostRead("/api/tasks/" + encodeURIComponent(taskId));
    if (!current()) return;
    if (t && t.status === 200 && t.body && t.body.status === "cancelled") {
      setRunStatus("task " + taskId + " 已取消" + (already ? "（先前已取消，未重複寫入）" : ""), "ok");
    } else {
      setRunStatus("取消已記錄；畫面更新失敗（task " + taskId + " 未能重新讀取，畫面可能不是最新）", "warn");
    }
  } else {
    const code = r.body && r.body.error;
    setRunStatus("task " + taskId + "：" + (CANCEL_ERRORS[code] || "未取消（" + r.status + "）") + (code === "store_unavailable" && r.body.code ? "（" + r.body.code + "）" : ""), "error");
  }
  pollRuns();
}

// readTasks reads the task list with the host token, and each listed
// task of a run whose Ask matters to its line (running or failed).
async function readTasks(runs) {
  const g = creds.gen;
  const l = await creds.hostRead("/api/tasks");
  if (!l || g !== creds.gen) return null;
  if (l.error || l.status !== 200 || !l.body) return { gen: g, error: l.error || "status_" + l.status };
  const list = new Map(l.body.tasks.map((t) => [t.task_id, t]));
  const details = new Map();
  const want = [...new Set(runs.filter((r) => r.live !== "running" && r.live !== "stopping").map((r) => r.task_id))]
    .filter((id) => list.has(id) && ["running", "failed"].includes(list.get(id).status)).slice(0, DETAILS_MAX);
  for (const id of want) {
    const d = await creds.hostRead("/api/tasks/" + encodeURIComponent(id));
    if (!d || g !== creds.gen) return null;
    if (d.status === 200 && d.body) details.set(id, d.body);
  }
  return { gen: g, list, details };
}

// pollRuns: one loop at a time, like the readiness poll.
let runsPolling = false;
async function pollRuns() {
  clearTimeout(runsTimer);
  if (runsPolling) return;
  runsPolling = true;
  const g = creds.gen;
  try {
    if (!document.hidden) {
      const r = await creds.runs();
      if (r && r.gen === creds.gen && r.runs) {
        const t = await readTasks(r.runs);
        if (g === creds.gen) { lastRuns = r; if (t) lastTasks = t; renderControl(); }
      }
    }
  } finally {
    runsPolling = false;
  }
  if (creds.view().hasHost) runsTimer = setTimeout(pollRuns, g === creds.gen ? RUNS_MS : 0);
}

$("token-file").addEventListener("change", (ev) => {
  const input = ev.target;
  const file = input.files && input.files[0];
  input.value = ""; // the field is emptied at once; the File object is read once below
  if (file) creds.load(file);
});
$("clear-all").addEventListener("click", () => creds.clearAll("已清除全部憑證"));
// Manual viewer mode: app.js has read and emptied the typed token; every
// loaded token goes before it signs in with it.
onTypedSignIn(() => {
  creds.manual();
  viewerGen = creds.gen;
});
// 「清除目前 viewer」 keeps the loaded file; app.js clears the viewer only.
new MutationObserver(renderReadiness).observe($("unity-status"), { attributes: true, attributeFilter: ["data-state"] });
document.addEventListener("visibilitychange", () => { if (!document.hidden && creds.view().hasHost) { pollReadiness(); pollRuns(); } });
// Leaving the page: drop every credential, stop polling, end every step in
// flight; app.js clears the Dashboard and the shop. Nothing returns on a
// restored page: the file has to be chosen again.
window.addEventListener("pagehide", () => { clearTimeout(readyTimer); clearTimeout(runsTimer); creds.clearAll("已離開頁面：憑證已清除，請重新載入 token 檔"); });

render(creds.view());
