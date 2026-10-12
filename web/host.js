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
//
// UX1 (docs/commercial-ux1.md): an operation card per Agent (its run's
// state, the calls the host has answered, the business state, start, stop
// and a confirmed cancel), the same choice from a card or a click on a
// customer in the shop, and the evidence in a drawer.

import { nextModeSeq, confirmMode, setStatus, forget, useViewer, onUnauthorized, onTypedSignIn, unityInfo, openTask, viewKey, refreshNow, currentTask, currentConsumer, setAlert,
  shop, onSceneReport, onScenePick, setSceneView, onReplayMode, refreshPanels } from "./app.js";
import { createHostCreds } from "./hostcreds.js";
import { createScene } from "./scene.js";
import * as A from "./adapter.js";

const $ = (id) => document.getElementById(id);
const READY_MS = 5000; // readiness is read every 5 s while the page is visible

async function call(token, path, signal, post) {
  const headers = token ? { Authorization: "Bearer " + token } : {}; // pairing sends no token
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
let lastTasks = null;
let lastRunsError = null; // the runs read of this generation failed: the list shown is the last good one // { gen, list: Map(task_id -> summary), details: Map(task_id -> task) } or { gen, error }
const RUNS_MS = 2000;
const DETAILS_MAX = 5; // ponytail: task reads per poll for the Ask notes; more runs waiting would read in turn

const creds = createHostCreds({ call, onChange: render });

// M-B (docs/multi-agent-demo-design.md 5): with a Unity build that shows
// version 2 and a host that starts runs, the shop is the scene: every
// consumer's newest run at once, each read with its own viewer token. The
// panels (app.js) follow the chosen consumer and task only.
const scene = createScene({ unity: shop(), token: (c) => creds.viewerToken(c), onChange: () => { renderScene(); refreshPanels(); } });
onSceneReport((r) => scene.rendered(r));
// A click on a customer in the shop: taken only when it names a customer of
// the current scene and slot (scene.picked); otherwise counted and dropped.
let picksDropped = 0;
onScenePick((r) => {
  if (scene.picked(r)) return selectConsumer(r.consumer);
  picksDropped++;
  $("unity-canvas").dataset.picksDropped = String(picksDropped);
});
setSceneView((c) => scene.view(c));
onReplayMode((on) => (on ? scene.pause() : scene.resume()));
const short = (c) => (c === "consumer-a" ? "A" : c === "consumer-b" ? "B" : c);

// hosting: this host starts runs (not history mode) and the page holds its credentials.
const hosting = () => !!(creds.view().hasHost && lastReady && lastReady.status && lastReady.status.runs && !lastReady.status.history);

// ensureScene starts the scene once the shop and the host allow it, and
// stops it when they no longer do (the shop goes back to the Dashboard).
function ensureScene() {
  const u = shop();
  if (hosting() && u.ready && u.supports2) {
    if (!scene.started) {
      scene.start();
      if (lastRuns && lastRuns.gen === creds.gen) scene.setRuns(lastRuns.runs);
    }
  } else if (scene.started) scene.stop();
  renderScene();
}

// renderScene: one line per customer of the scene, its summary, and what
// the scene cannot show (never a run left out without a word).
function renderScene() {
  const u = shop();
  const st = lastReady && lastReady.status;
  let cap = "";
  if (hosting() && u.ready && !u.supports2) {
    cap = "此 Unity 建置只支援單一客人：場景只呈現選取的 " + (currentConsumer() || "客人") + "；其他位的資料仍在面板與場次列表";
  } else if (hosting() && u.supports2) {
    const consumers = [...new Set(((st.runs && st.runs.scenarios) || []).map((x) => x.consumer_id))];
    const ns = A.notShown(consumers, u.bridge.customers, st.capacity && st.capacity.max_runs);
    if (ns.consumers.length || ns.overCap) {
      cap = "此場景最多同時呈現 " + u.bridge.customers.length + " 位（" + u.bridge.customers.join("、") + "）"
        + (ns.consumers.length ? "；" + ns.consumers.join("、") + " 的場次只在場次列表與面板中" : "")
        + (ns.overCap ? "；同時執行上限 " + st.capacity.max_runs + " 高於場景能呈現的人數" : "");
    }
  }
  const capBox = $("scene-cap");
  capBox.hidden = !cap;
  capBox.textContent = cap;
  setAlert("scene-cap", cap, "warn");
  const slots = scene.started ? scene.slots() : [];
  const list = $("scene-status");
  list.hidden = !scene.started;
  list.replaceChildren(...slots.map((x) => {
    const li = document.createElement("li");
    Object.assign(li.dataset, { consumer: x.consumer, task: x.task || "", seq: String(x.seq), phase: x.phase, slot: String(x.slot), scene: String(x.scene),
      shown: String(x.shown), read: x.read });
    const sync = !scene.active ? "回放中：場景暫停" : x.read !== "ok" && x.read !== "loading" ? "讀取失敗（" + x.read + "），保留上次資料" : x.seq < 0 ? "同步中"
      : x.shown ? "Unity 已呈現" : x.moving ? "Unity 補間中" : "Unity 尚未回報";
    li.textContent = "客人 " + short(x.consumer) + "：" + (x.task || "—") + (x.seq >= 0 ? " #" + x.seq : "") + (x.label ? " · " + x.label : "") + " · " + sync;
    return li;
  }));
  // UX1: each card says its customer's state; the header chip would repeat it.
  $("scene-summary").hidden = true;
  renderControl();
}

// A 401 the Dashboard saw for its current viewer: the whole file is stale.
onUnauthorized(() => { if (!creds.unauthorized(viewerGen)) forget(); });

function render(v) {
  if (v.gen !== shownGen) {
    // New generation: the previous identity leaves the panels and the shop.
    shownGen = v.gen;
    forget(v.message || "請輸入啟動終端機上的配對碼（或在「進階」載入 token 檔）");
    lastReady = null;
    lastRuns = null;
    lastTasks = null;
    lastRunsError = null;
    starting = false;
    cancelling = false;
    confirming = null;
    clearTimeout(readyTimer);
    clearTimeout(runsTimer);
    setRunStatus("", "idle");
    scene.reset(); // the old identity's slots and reads go too
  }
  // D1: the credentials fold once a file is loaded or the page paired; the
  // status bar keeps a way to load one (section 4.1). Pairing (docs/cli-poc.md
  // 56) is the usual way; the token file is the advanced and recovery way.
  const held = v.mode === "file" || v.mode === "paired";
  $("creds-box").open = !held && v.mode !== "pairing";
  $("load-file").hidden = held;
  $("creds-summary").textContent = "憑證與就緒（進階：token 檔）：" + (v.mode === "paired" ? "已配對" : v.mode === "file" ? "已載入 token 檔" : v.mode === "manual" ? "手動 viewer 模式" : "尚未配對或載入");
  $("pair-form").hidden = held;
  // A pairing adopted in this generation: the status bar says so once
  // (it said 「配對中…」 when the generation began).
  if (v.mode === "paired" && pairedGen !== v.gen) {
    pairedGen = v.gen;
    setStatus(v.message + "；按「開始」執行一場", "ok");
  }
  $("pair-note").textContent = v.mode === "pairing" || (v.mode === "none" && pairTried && v.message) ? v.message : PAIR_NOTE;
  $("pair-note").dataset.kind = v.mode === "none" && pairTried && v.message ? "error" : "";
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
      const mseq = nextModeSeq(); // the host's own status says the mode too (docs/cli-poc.md 56.8)
      const r = await creds.readiness();
      if (r && g === creds.gen) {
        lastReady = r;
        if (r.status) confirmMode(mseq, r.status.history && r.status.history.record === "live_history" ? r.status.history : r.status.live && r.status.live.record === "live_demo" ? r.status.live : null);
        renderReadiness();
        renderControl();
        ensureScene();
      }
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
    rows.push(["就緒狀態", "unconfirmed", v.mode === "manual" ? "需要 token 檔或配對（手動 viewer 模式不讀就緒狀態）" : "未確認：請先配對或載入 token 檔"]);
  } else if (!lastReady) {
    rows.push(["就緒狀態", "unconfirmed", "未確認：讀取中"]);
  } else if (lastReady.error) {
    rows.push(["就緒狀態", "error", "無法取得就緒狀態（" + lastReady.error + "），未確認"]);
  } else {
    const st = lastReady.status;
    const missing = ["WebGL.data", "WebGL.framework.js", "WebGL.loader.js", "WebGL.wasm"].filter((f) => !st.unity_build.files.includes(f));
    rows.push(["HTTP", st.http === "ok" ? "ok" : "error", st.http === "ok" ? "就緒" : "異常"]);
    // History mode (docs/cli-poc.md 54): a read-only copy of a recorded
    // run; nothing expires and the time is the record's, not the present.
    const hist = st.history && st.history.record === "live_history";
    const live = st.live && st.live.record === "live_demo" ? st.live : null; // live mode (docs/cli-poc.md 55)
    rows.push(hist ? ["Store", "info", "歷史模式：唯讀副本，不執行到期檢查"] : ["Store", st.store.state, storeText(st.store)]);
    rows.push(["fake CLI", st.fake_cli.state, st.fake_cli.state === "ok" ? "就緒（fake-claude，不是 Claude Code）" : "未啟用"]);
    rows.push(live ? ["真實 CLI", "info", "啟用：按「開始」才執行真實 Claude Code CLI（" + live.cli_version + "；啟動時已核對，每場再核對）；"
        + (live.dev_demo_plugins ? "開發展示例外（plugins 內容與行為未確認）" : "嚴格模式")]
      : ["真實 CLI", "off", hist ? "未執行（歷史模式：顯示已保存的真實執行紀錄）" : "未啟用（fake 模式）"]);
    rows.push(["Unity 建置", st.unity_build.state, st.unity_build.state === "ok" ? "就緒（4 個建置檔）" : "缺少 " + missing.join("、") + "：請先完成首次準備"]);
    if (st.capacity) rows.push(["同時執行上限", "info", st.capacity.max_runs + " 場（部署設定 --max-runs；目錄中 " + st.capacity.consumers + " 位客人，每位一次一場）"]);
    rows.push(["付款", "off", "無付款能力"]);
    // The binary's own record of its source (docs/cli-poc.md 56): a dirty
    // build is shown as such (Go counts untracked files as modified too).
    const bi = st.build || {};
    rows.push(["建置", "info", bi.revision ? bi.revision.slice(0, 12) + (bi.modified === "true" ? "（modified=true：建置時工作區有變更或未追蹤檔案，不是乾淨建置）"
      : bi.modified === "false" ? "（modified=false）" : "") : "未記錄來源版本"]);
    rows.push(hist ? ["紀錄時間", "info", time(st.served_at) + "（凍結於最後事件，不是現在的伺服器時間）"] : ["檢查時間", "info", time(st.served_at) + "（Store 時間）"]);
  }
  rows.push(["Unity 場景", scene[0], scene[1]]);
  const bad = rows.filter(([, state]) => !["ok", "off", "info"].includes(state));
  setAlert("ready", bad.length ? "就緒：" + bad.length + " 項未確認或錯誤（" + bad.map(([k]) => k).join("、") + "）" : "",
    bad.some(([, state]) => ["error", "failed", "missing"].includes(state)) ? "error" : "warn");
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
  consumer_busy: "這位客人的場次尚未結束；請先等它結束或停止它",
  capacity_full: "同時執行的場次已達上限（部署設定）；請先等一場結束或停止一場",
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

// confirming: the task whose cancel waits for its confirmation (with the
// generation); a new generation or a done cancel drops it.
let confirming = null;
let shownControl = "";
let shownRuns = "";
let advancedGen = -1; // the generation 進階 was opened for by itself (history mode, the manual viewer)

// agentModel: what one customer's card says, from its newest run (the runs
// list, newest first), its scene slot and the task reads. Unknown is said
// to be unknown, never 0 or "none".
function agentModel(c, runs, slots, ctx) {
  const run = runs.find((r) => r.consumer_id === c) || null;
  const slot = slots.find((x) => x.consumer === c) || null;
  const rc = A.runCard(run, ctx.runsError);
  const { state, calls } = rc;
  let business = "";
  if (slot && slot.task) {
    if (slot.read !== "ok" && slot.read !== "loading") business = "讀取失敗（" + slot.read + "），保留上次資料" + (slot.label ? "：" + slot.label : "");
    else if (slot.seq < 0) business = "同步中";
    else business = (state === "ended" && slot.phase === "waiting" ? "執行已結束／請購待核准（M1 無核准功能；到期前不會付款）" : "") || slot.label || slot.phase;
  } else business = run ? "同步中" : "尚無場次";
  const t = run && ctx.tasks && ctx.tasks.get(run.task_id);
  const sync = !slot || !slot.task || slot.seq < 0 ? "" : !scene.active ? "回放中：場景暫停" : slot.shown ? "Unity 已呈現" : slot.moving ? "Unity 補間中" : "Unity 尚未回報";
  return {
    c, state, stateText: rc.text, calls, callsKind: rc.kind, business, sync, selected: ctx.chosen === c,
    task: run ? run.task_id + (slot && slot.task === run.task_id && slot.seq >= 0 ? " #" + slot.seq : "") : "",
    run: run && run.run_id, stoppable: rc.stoppable, cancel: run && cancellable(run) ? run.task_id : "",
    taskStatus: t ? t.status : "",
  };
}

function renderControl() {
  const v = creds.view();
  const box = $("control");
  // History mode has no control: the server refuses it (409), the page
  // does not offer it. The task picker and the replay are then the way in.
  const history = !!(lastReady && lastReady.status && lastReady.status.history);
  box.hidden = !v.hasHost || history;
  if ((history || v.mode === "manual") && advancedGen !== creds.gen) { advancedGen = creds.gen; $("advanced").open = true; }
  const pick = $("run-scenarios");
  const scenarios = (lastReady && lastReady.status && lastReady.status.runs && lastReady.status.runs.scenarios) || [];
  const live = !!(lastReady && lastReady.status && lastReady.status.live);
  $("live-warning").hidden = !live;
  const runs = (lastRuns && lastRuns.gen === creds.gen && lastRuns.runs) || [];
  // M-B: one running or stopping run per consumer, at most max_runs in all
  // (a deployment setting; the server decides again).
  const activeRuns = runs.filter((r) => r.live === "running" || r.live === "stopping");
  const busy = new Set(activeRuns.map((r) => r.consumer_id));
  const maxRuns = (lastReady && lastReady.status && lastReady.status.capacity && lastReady.status.capacity.max_runs) || 1;
  const full = activeRuns.length >= maxRuns;
  const consumers = [...new Set(scenarios.map((sc) => sc.consumer_id))].sort();
  const slots = scene.started ? scene.slots() : [];
  const chosen = currentConsumer();
  // The ring in the shop follows the customer the panels show.
  if (scene.started && scene.selected !== (chosen || null)) scene.select(chosen);
  // No customer is chosen by itself (a reload or new credentials switch to
  // no one, section 13): a start opens its run, a card or the shop chooses.
  if (confirming && confirming.gen !== creds.gen) confirming = null;
  // A confirmation whose task can no longer be cancelled (it ended, or the
  // customer's newest run is another task) closes: never moved to another task.
  if (confirming) {
    const r = runs.find((x) => x.task_id === confirming.task);
    const newest = r && runs.find((x) => x.consumer_id === r.consumer_id);
    if (!r || !cancellable(r) || (confirming.where === "card" && newest !== r)) confirming = null;
  }
  const ctx = { chosen, runsError: lastRunsError, tasks: lastTasks && lastTasks.gen === creds.gen && lastTasks.list };
  const models = consumers.map((c) => agentModel(c, runs, slots, ctx));
  // Rebuilt only when what the cards say changed: a poll that changes
  // nothing keeps the focus and an open confirmation where they are.
  const key = JSON.stringify([models, scenarios, live, starting, cancelling, full, [...busy], confirming]);
  if (key !== shownControl) {
    shownControl = key;
    keepFocus(pick, () => pick.replaceChildren(...models.map((m) => agentCard(m, scenarios.filter((x) => x.consumer_id === m.c), { live, busy, full }))));
  }
  // 一起開始: each consumer's first scenario, as separate requests; not one
  // atomic start (docs/multi-agent-demo-design.md 3.5).
  const pair = consumers.map((c) => scenarios.find((x) => x.consumer_id === c));
  const all = $("start-all");
  all.hidden = pair.length < 2 || maxRuns < 2;
  all.textContent = "一起開始：" + pair.map((sc) => sc.id + "（" + short(sc.consumer_id) + "）").join(" ＋ ") + "（各自送出，不保證都成功）";
  all.disabled = starting || pair.every((sc) => busy.has(sc.consumer_id)) || full;
  all.onclick = () => startAll(pair);
  renderNext(runs);
  // What the folded runs list would hide stays in its summary and in the
  // status bar: the runs' history (interrupted, a record not written) apart
  // from what cannot be read now (review of 7ffc45d, R2).
  const odd = runs.filter((r) => r.state === "interrupted" || r.record_error || r.store_end_error).length;
  const issues = [];
  if (v.hasHost) {
    if (lastRunsError) issues.push("場次清單讀取失敗（" + lastRunsError + "），顯示上次資料");
    if (lastTasks && lastTasks.gen === creds.gen && lastTasks.error) issues.push("task 狀態讀取失敗（" + lastTasks.error + "），未確認");
    else if (lastTasks && lastTasks.gen === creds.gen && lastTasks.list) {
      const unknown = runs.filter((r) => r.live !== "running" && r.live !== "stopping" && lastTasks.list.has(r.task_id)
        && ["running", "failed"].includes(lastTasks.list.get(r.task_id).status) && !lastTasks.details.has(r.task_id)).length;
      if (unknown) issues.push(unknown + " 場的 Ask 狀態未確認");
    }
  }
  $("runs-summary").textContent = "最近場次（" + runs.length + " 場" + (odd ? "；歷史：" + odd + " 場中斷或紀錄寫入失敗" : "") + (issues.length ? "；目前：" + issues.join("、") : "") + "）";
  setAlert("runs-history", odd ? "場次歷史：" + odd + " 場中斷或紀錄寫入失敗" : "", "warn");
  setAlert("runs-now", issues.length ? "最近場次目前：" + issues.join("、") : "", "error");
  const runsKey = JSON.stringify([runs, ctx.tasks ? [...ctx.tasks.keys()].map((k) => [k, ctx.tasks.get(k).status]) : null, lastTasks && lastTasks.details ? [...lastTasks.details.keys()] : null, lastTasks && lastTasks.error, cancelling, confirming]);
  if (runsKey === shownRuns) return;
  shownRuns = runsKey;
  keepFocus($("runs"), () => $("runs").replaceChildren(...runs.map((r) => {
    const li = document.createElement("li");
    li.dataset.run = r.run_id;
    li.dataset.state = r.state;
    li.dataset.live = r.live || "";
    li.dataset.gen = String(creds.gen);
    const now = taskNow(r);
    li.textContent = r.task_id + "（" + r.consumer_id + "，" + r.scenario + "）：" + runText(r) + (now ? "；" + now : "");
    if (cancellable(r)) {
      li.append(" ", cancelButton(r.task_id, "task-cancel", "取消 task " + r.task_id));
      if (confirming && confirming.task === r.task_id && confirming.where === "runs") li.append(confirmBox(r.task_id));
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
  })));
}

// keepFocus rebuilds el and gives the focus back to the same control, when
// the focus was in el (review of 382a4b5, R1): the same kind of button for
// the same customer or run and the same task, run or scenario, in the same
// credentials generation. A button that names its run (停止 Agent) comes
// back only for that run: a newer run's stop never gets it (review of
// c646c26). A control that is gone hands the focus to a safe
// one of the same customer (a closed confirmation: its 取消 entry; else 選取),
// never to another task's cancel, and nothing is clicked. A focus the user
// moved elsewhere stays where it is.
const FOCUS_KINDS = ["cancel-confirm", "cancel-back", "row-cancel", "task-cancel", "row-stop", "run-stop", "agent-select", "agent-start"];
function keepFocus(el, rebuild) {
  const a = document.activeElement;
  const kind = a && el.contains(a) ? FOCUS_KINDS.find((k) => a.classList.contains(k)) : null;
  const card = kind && a.closest(".agent-card"), li = kind && a.closest("li[data-run]");
  const built = card || li; // the generation the control was made in, not the one now
  const was = kind && { gen: Number(built ? built.dataset.gen : -1), kind, consumer: card ? card.dataset.consumer : "", run: li ? li.dataset.run : "", ownRun: a.dataset.run || "", task: a.dataset.task || "", scenario: a.dataset.scenario || "" };
  rebuild();
  if (!was || (document.activeElement && document.activeElement !== document.body && document.activeElement !== a)) return;
  const usable = (x) => !!x && !x.disabled && x.offsetParent !== null;
  const scope = was.consumer ? el.querySelector('.agent-card[data-consumer="' + CSS.escape(was.consumer) + '"]') : was.run ? el.querySelector('li[data-run="' + CSS.escape(was.run) + '"]') : null;
  if (was.gen === creds.gen && scope) {
    const attr = (k, v) => (v ? "[data-" + k + '="' + CSS.escape(v) + '"]' : "");
    const same = scope.querySelector("." + was.kind + attr("task", was.task) + attr("scenario", was.scenario) + attr("run", was.ownRun));
    if (usable(same)) return same.focus();
    // A confirmation closed (返回, or its task ended): its own entry, if the task still has one.
    const entry = was.task && scope.querySelector('.row-cancel[data-task="' + CSS.escape(was.task) + '"], .task-cancel[data-task="' + CSS.escape(was.task) + '"]');
    if (usable(entry)) return entry.focus();
    const sel = scope.querySelector(".agent-select");
    if (usable(sel)) return sel.focus();
  }
  // Another generation, or nothing of that customer left: a control that only shows.
  const safe = ["pair-code", "load-file", "show-evidence"].map((id) => $(id)).find(usable);
  if (safe) safe.focus();
}

// agentCard draws one customer's card. Clicking the card (not a button)
// chooses the customer, as does its 選取 button and a click on it in the shop.
function agentCard(m, scs, { live, busy, full }) {
  const el = (tag, cls, text) => { const e = document.createElement(tag); if (cls) e.className = cls; if (text !== undefined) e.textContent = text; return e; };
  const card = el("section", "consumer-row agent-card");
  Object.assign(card.dataset, { consumer: m.c, state: m.state, selected: String(m.selected), gen: String(creds.gen) });
  card.setAttribute("aria-label", "客人 " + short(m.c) + " 的操作");
  const head = el("div", "agent-head");
  head.append(el("span", "agent-dot", short(m.c)), el("span", "agent-name", "客人 " + short(m.c)), el("span", "agent-id", m.c));
  const st = el("span", "agent-state", m.stateText);
  st.dataset.state = m.state;
  head.append(st);
  card.append(head);
  if (m.task) card.append(el("p", "agent-meta", "task " + m.task + (m.sync ? " · " + m.sync : "")));
  card.append(el("p", "agent-business", m.business));
  if (m.calls) {
    const c = el("p", "agent-calls", m.calls);
    c.dataset.calls = m.callsKind;
    card.append(c);
  }
  const acts = el("div", "agent-actions");
  const sel = el("button", "agent-select", m.selected ? "已選取" : "選取");
  sel.type = "button";
  sel.setAttribute("aria-pressed", String(m.selected));
  sel.addEventListener("click", () => selectConsumer(m.c));
  acts.append(sel);
  for (const sc of scs) {
    const b = el("button", "agent-start", (live ? "開始（真實 CLI）" : "開始（fake，不執行模型）") + (scs.length > 1 ? "：" + sc.label : ""));
    b.type = "button";
    b.dataset.scenario = sc.id;
    b.disabled = starting || busy.has(m.c) || full;
    b.title = sc.label + "（" + sc.consumer_id + "）" + (busy.has(m.c) ? "：這位客人的場次尚未結束" : full ? "：同時執行的場次已達上限" : "");
    b.addEventListener("click", () => start(sc));
    acts.append(b);
  }
  if (m.run) {
    // 停止 Agent: only a running run; a stopping one still holds its place,
    // an ended one has nothing to stop.
    const b = el("button", m.stoppable ? "row-stop" : "row-stop-off", m.state === "stopping" ? "停止中…" : "停止 Agent");
    b.type = "button";
    if (m.stoppable) b.dataset.run = m.run;
    b.disabled = !m.stoppable;
    b.title = m.stoppable ? "停止這位客人執行中的場次（只停這一場）" : m.state === "stopping" ? "停止請求已受理，等待程序結束" : "場次已結束，沒有可停止的執行";
    b.addEventListener("click", () => stop(m.run));
    acts.append(b);
  }
  if (m.cancel) acts.append(cancelButton(m.cancel, "row-cancel", "取消請購／task"));
  card.append(acts);
  if (m.cancel && confirming && confirming.task === m.cancel && confirming.where === "card") card.append(confirmBox(m.cancel));
  if (m.state === "ended" && !m.cancel) card.append(el("p", "agent-note", "場次已結束；停止不適用。"));
  card.addEventListener("click", (ev) => { if (!ev.target.closest("button")) selectConsumer(m.c); });
  return card;
}

// cancelButton asks first: the cancel ends the whole task (section 6.1).
function cancelButton(taskId, cls, text) {
  const b = document.createElement("button");
  b.type = "button";
  b.className = cls;
  b.dataset.task = taskId;
  b.textContent = text;
  b.disabled = cancelling;
  const where = cls === "task-cancel" ? "runs" : "card";
  b.addEventListener("click", () => {
    confirming = { task: taskId, gen: creds.gen, where };
    renderControl();
    const yes = document.querySelector((where === "runs" ? "#runs" : "#run-scenarios") + ' .agent-confirm[data-task="' + CSS.escape(taskId) + '"] .cancel-confirm');
    if (yes) yes.focus();
  });
  return b;
}

const CANCEL_SCOPE = "取消整個 task：未到期的 Ask 會撤回、已到期的記為到期，task 記錄為已取消（TASK_CANCELLED）；不付款，無法復原。";

function confirmBox(taskId) {
  const box = document.createElement("div");
  box.className = "agent-confirm";
  box.dataset.task = taskId;
  box.setAttribute("role", "group");
  box.setAttribute("aria-label", "確認取消 task " + taskId);
  const p = document.createElement("p");
  p.textContent = "確認取消 task " + taskId + "？" + CANCEL_SCOPE;
  const yes = document.createElement("button");
  yes.type = "button";
  yes.className = "cancel-confirm";
  yes.dataset.task = taskId;
  yes.textContent = "確認取消 task";
  yes.disabled = cancelling;
  yes.addEventListener("click", () => { confirming = null; cancel(taskId); });
  const no = document.createElement("button");
  no.type = "button";
  no.className = "cancel-back";
  no.dataset.task = taskId;
  no.textContent = "返回，不取消";
  no.addEventListener("click", () => { confirming = null; renderControl(); });
  box.append(p, yes, " ", no);
  return box;
}

// renderNext: the next operation the chosen customer's task has, said in
// the summary (no approval and no payment exist in M1).
function renderNext(runs) {
  const p = $("next-action");
  const task = currentTask();
  const run = task && runs.find((r) => r.task_id === task);
  let text = "";
  if (run) {
    if (run.live === "running") text = "可用操作：停止 Agent（只停這一場）";
    else if (run.live === "stopping") text = "停止中：等待程序結束（仍占名額）";
    else if (cancellable(run)) text = "可用操作：取消請購／task。M1 沒有核准功能，Ask 不會付款，到期後由後端記錄。";
    else if (lastTasks && lastTasks.gen === creds.gen && lastTasks.list && lastTasks.list.get(run.task_id)) text = "沒有可用操作：task 已收尾。";
    else text = "task 狀態讀取中（未確認）";
  }
  p.hidden = !text;
  p.textContent = text;
}

// selectConsumer: the panels show this customer's scene task (its viewer
// token, as always); the ring follows (renderControl). A UI choice only:
// nothing is started, stopped or cancelled.
let selecting = 0;
async function selectConsumer(c) {
  const sv = scene.view(c);
  const task = sv && sv.task;
  if (currentConsumer() === c) {
    if (task && currentTask() !== task) openTask(task);
    return;
  }
  const token = creds.viewerToken(c);
  if (!token) return;
  const g = creds.gen;
  const n = ++selecting;
  viewerGen = g;
  const signedIn = await useViewer(token);
  if (signedIn !== true || g !== creds.gen || n !== selecting) return;
  if (task) openTask(task);
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

// startAll sends one start per consumer, one after the other, each with its
// own request ID; each reply is shown as it is, and a refused one is not
// retried. The panels open the first one accepted.
async function startAll(pair) {
  if (starting) return;
  const g = creds.gen;
  starting = true;
  renderControl();
  setRunStatus("一起開始：送出 " + pair.length + " 個獨立的開始請求…", "busy");
  const results = [];
  for (const sc of pair) {
    const r = await creds.startRun(sc.id);
    if (g !== creds.gen || !r || r.gen !== creds.gen) return; // cleared or replaced: nothing acts
    results.push([sc, r]);
  }
  starting = false;
  renderControl();
  const ok = results.filter(([, r]) => !r.error && (r.status === 202 || r.status === 200));
  setRunStatus("一起開始（各自送出，不是一次成功或一次失敗）：" + results.map(([sc, r]) => short(sc.consumer_id) + " "
    + (r.error ? "沒有收到回覆，未確認" : r.status === 202 ? "已開始 " + r.body.task_id : r.status === 200 ? "已受理（同一請求）" + r.body.task_id
      : ERRORS[r.body && r.body.error] || "未開始（" + r.status + "）")).join("；"), ok.length === results.length ? "ok" : ok.length ? "warn" : "error");
  if (ok.length) await openRun(ok[0][1].body, g);
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
        if (g === creds.gen) { lastRuns = r; lastRunsError = null; if (t) lastTasks = t; renderControl(); scene.setRuns(r.runs); }
      } else if (r && r.gen === creds.gen && r.error) {
        lastRunsError = r.error;
        renderControl();
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
$("clear-all").addEventListener("click", () => { pairTried = false; creds.clearAll("已清除全部憑證"); });
// Pairing: the typed code is read once and the field emptied before
// anything is sent; a late answer after a clear, a new file, the manual
// mode or pagehide signs nothing in (a new generation, hostcreds.js).
let pairTried = false;
let pairedGen = -1;
const PAIR_NOTE = "配對只取得本次服務的主持人憑證，不會執行模型；按「開始」才執行一場。";
$("pair-form").addEventListener("submit", (ev) => {
  ev.preventDefault();
  const input = $("pair-code");
  const typed = input.value;
  input.value = "";
  pairTried = true;
  creds.pair(typed);
});
// 查看證據 (UX1): a drawer over the right side. The scene, the choice and
// the polling go on underneath; Esc or 關閉 closes it and the focus goes
// back to where it came from.
const drawer = $("evidence-drawer");
let drawerFrom = null;
function openDrawer() {
  drawerFrom = document.activeElement;
  drawer.hidden = false;
  $("show-evidence").setAttribute("aria-expanded", "true");
  $("evidence-box").open = true;
  $("drawer-close").focus();
}
function closeDrawer() {
  if (drawer.hidden) return;
  drawer.hidden = true;
  $("show-evidence").setAttribute("aria-expanded", "false");
  const back = drawerFrom && document.contains(drawerFrom) && drawerFrom !== document.body ? drawerFrom : $("show-evidence");
  back.focus();
}
$("show-evidence").addEventListener("click", openDrawer);
$("drawer-close").addEventListener("click", closeDrawer);
document.addEventListener("keydown", (ev) => { if (ev.key === "Escape" && !drawer.hidden) closeDrawer(); });
$("load-file").addEventListener("click", () => $("token-file").click());
document.addEventListener("apa:view", renderControl);
// Manual viewer mode: app.js has read and emptied the typed token; every
// loaded token goes before it signs in with it.
onTypedSignIn(() => {
  creds.manual();
  viewerGen = creds.gen;
});
// 「清除目前 viewer」 keeps the loaded file; app.js clears the viewer only.
new MutationObserver(() => { renderReadiness(); ensureScene(); }).observe($("unity-status"), { attributes: true, attributeFilter: ["data-state"] });
// 回到場景這筆: the panels go back to the chosen consumer's scene task.
$("view-back").addEventListener("click", () => { const t = $("view-note").dataset.scene; if (t) openTask(t); });
document.addEventListener("visibilitychange", () => { if (!document.hidden && creds.view().hasHost) { pollReadiness(); pollRuns(); } });
// Leaving the page: drop every credential, stop polling, end every step in
// flight; app.js clears the Dashboard and the shop. Nothing returns on a
// restored page: the file has to be chosen again.
window.addEventListener("pagehide", () => { clearTimeout(readyTimer); clearTimeout(runsTimer); creds.clearAll("已離開頁面：憑證已清除，請重新載入 token 檔"); });

render(creds.view());
