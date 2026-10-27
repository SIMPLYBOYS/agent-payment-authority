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
//
// P1-a (docs/commercial-p1a.md): for an apa.pay.v1 scenario the card offers
// 「以 A 模擬」 (the fake-only simulated approver: the host plays the
// customer; not a person's verification), then 「建立委託」: the host's
// terms, confirmed by the simulated customer, start the run. Its Ask shows in
// 「需要你處理」: the purchase's summary, the backend's deadline and one main
// action. An approval leaves a simulated grant and enables no payment.

import { nextModeSeq, confirmMode, setStatus, forget, useViewer, onUnauthorized, onTypedSignIn, unityInfo, openTask, viewKey, refreshNow, currentTask, currentConsumer, setAlert,
  shop, onSceneReport, onScenePick, onVenuePick, setSceneView, setVenueOf, onReplayMode, refreshPanels } from "./app.js";
import { createHostCreds } from "./hostcreds.js";
import { createScene } from "./scene.js";
import { createRelay } from "./showlink.js";
import * as A from "./adapter.js";
import * as AP from "./approve.js";
import * as V from "./voice.js";

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
let lastApprovals = null; // P1-a: { gen, consumer, servedAt, at (ms), items } of the simulated customer, or { gen, consumer, error }
let delegating = null;    // P1-a: { gen, consumer, scenario } whose delegation terms are open on its card
let simulating = false;   // a 「以 X 模擬」 request is in flight
let resolving = false;    // an approve or deny is in flight
let approvalNote = null;  // { gen, kind, text } the last approval action's outcome
let a2Waiting = null;    // batch 3: { gen, consumer, items: [{ consumer, product, amount }] }, the other customers' Asks waiting
let lastA2 = null;        // A2: { gen, consumer, servedAt, at, items, signer, sources } of the selected customer, or { gen, consumer, error }
let a2Op = null;          // A2: the operation in flight { gen, consumer, session, intent, decision, phase }
let a2Note = null;        // A2: { target, kind, text } the last operation's outcome, for its own item only
let a2Recovering = false; // A2: a status query by the same IDs is in flight
const RUNS_MS = 2000;
const DETAILS_MAX = 5; // ponytail: task reads per poll for the Ask notes; more runs waiting would read in turn

// checker (GD4-A): the report of `verify-demo --mode e1a` the host chose after
// the show, kept in this page's memory only and shown in the audience window.
let checker = null;
let checkerGen = -1; // new credentials drop the report: it may be of another show
const creds = createHostCreds({ call, onChange: render });

// M-B (docs/multi-agent-demo-design.md 5): with a Unity build that shows
// version 2 and a host that starts runs, the shop is the scene: every
// consumer's newest run at once, each read with its own viewer token. The
// panels (app.js) follow the chosen consumer and task only.
// The audience window (docs/commercial-gap2.md, GD1-A, GD5): while it is
// open, the scene's Unity is the window's (relay) and this page's own shop
// is hidden; when it goes, the scene comes back here.
const relay = createRelay((kind, d) => {
  if (kind === "rendered2") scene.rendered(d);
  else if (kind === "select2") pick(d);
  else if (kind === "show") useShow(!!(d && d.ready && d.supports2));
});
const sceneUnity = {
  target: shop(),
  get ready() { return this.target.ready; },
  get supports2() { return this.target.supports2; },
  own(on) { this.target.own(on); },
  send2(m) { this.target.send2(m); },
  select2(m) { this.target.select2(m); },
};
// 簡報模式 paces the scene (Aaron 10/27: 畫面依序播放): each step holds a few seconds; the operator's page stays live.
const scene = createScene({ unity: sceneUnity, token: (c) => creds.viewerToken(c), onChange: () => { renderScene(); refreshPanels(); publishView(); renderPresent(); },
  paced: () => presenting && creds.view().hasHost });
onSceneReport((r) => { if (sceneUnity.target !== relay) scene.rendered(r); });
// A click on a customer in the shop: taken only when it names a customer of
// the current scene and slot (scene.picked); otherwise counted and dropped.
let picksDropped = 0;
function pick(r) {
  if (scene.picked(r)) return selectConsumer(r.consumer);
  picksDropped++;
  $("unity-canvas").dataset.picksDropped = String(picksDropped);
}
onScenePick((r) => { if (sceneUnity.target !== relay) pick(r); });
// useShow moves the scene to the audience window (on) or back here; either
// way the scene starts over on its new Unity, every slot shown again.
function useShow(on) {
  const want = on ? relay : shop();
  if (sceneUnity.target === want) { if (on) restartScene(); return; } // the window loaded again
  if (scene.started) scene.stop();
  sceneUnity.target = want;
  $("show-note").hidden = !on;
  document.querySelector(".canvas-frame").hidden = on;
  $("unity-status").hidden = on; // it speaks of this page's own (hidden) shop
  ensureScene();
}
function restartScene() {
  if (scene.started) scene.stop();
  ensureScene();
}
// publishView: what the audience window shows beside the scene: the host's
// payment and approval traits and each slot's read (task and events, no token).
function publishView() {
  if (sceneUnity.target !== relay) return;
  if (checker && checkerGen !== creds.gen) { setChecker(null); return; }
  const st = (lastReady && lastReady.status) || {};
  const ha = st.human_approval;
  relay.view({ payments: A.paysOn(st.payment_capability) ? st.payment_capability : "", authenticator: (ha && ha.sources && ha.sources.authenticator) || "",
    venue: venue || "", checker: checker && { ...checker, findings: checker.findings.map(({ claim, verdict, noChain }) => ({ claim, verdict, noChain })) }, customers: scene.slots().map((s) => ({ consumer: s.consumer, snapshot: scene.snapshot(s.consumer), delegation: delegationOf(s.task), chain: chainOf(s) })) });
}
// chainOf (docs/commercial-ops.md O2): what the chain says of a slot's
// payments, read by the Host (GET /api/tasks/{id}/chain) with that
// customer's own viewer token, again every few seconds while the window is open.
const chains = new Map(); // task -> { at, gen, data, busy }
const CHAIN_MS = 5000;
function chainOf(s) {
  const snap = scene.snapshot(s.consumer);
  const pays = snap && (snap.task.purchases || []).some((p) => p.payment && (p.payment.reserve_tx || p.payment.settle_tx));
  if (!pays) return null;
  const c = chains.get(s.task);
  // New credentials (a Host started again) read again at once (review).
  if (!c || c.gen !== creds.gen || (Date.now() - c.at > CHAIN_MS && !c.busy)) readChain(s.consumer, s.task);
  return c && c.gen === creds.gen ? c.data : null;
}
async function readChain(consumer, task) {
  const t = creds.viewerToken(consumer);
  if (!t) return;
  const g = creds.gen; // a reply under older credentials is dropped
  const prev = chains.get(task);
  const kept = prev && prev.gen === g ? prev.data : null;
  chains.set(task, { at: Date.now(), gen: g, data: kept, busy: true });
  let data = kept;
  try {
    const r = await fetch("/api/tasks/" + encodeURIComponent(task) + "/chain", { headers: { Authorization: "Bearer " + t }, cache: "no-store", credentials: "omit" });
    if (r.ok) data = await r.json();
  } catch { /* the last read stays */ }
  if (g !== creds.gen) return;
  chains.set(task, { at: Date.now(), gen: g, data, busy: false });
  publishView();
}
$("checker-file").addEventListener("change", async (ev) => {
  const f = ev.target.files && ev.target.files[0];
  if (!f) return;
  let r;
  try { r = f.size > 1 << 20 ? { error: "檔案太大" } : A.parseChecker(await f.text()); } catch { r = { error: "讀不到檔案" }; }
  ev.target.value = "";
  if (r.error) { $("checker-note").textContent = "沒有載入：" + r.error; return; }
  setChecker(r.report);
});
$("checker-clear").addEventListener("click", () => setChecker(null));
// setChecker: the report shown (null: none); the findings' reasons are this page's only.
function setChecker(r) {
  checker = r;
  checkerGen = creds.gen;
  $("checker-clear").hidden = !r;
  $("checker-note").textContent = !r ? "" : "已載入：證據包 " + r.run_id + "，總結果 " + r.verdict + "（只在這一頁的記憶體，不寫任何檔案）"
    + r.findings.map((x) => "；" + x.claim + " " + x.verdict + (x.reason ? "：" + x.reason : "")).join("");
  publishView();
}
// delegationOf: the delegation of a task's run's scenario (the catalog's), or null.
function delegationOf(task) {
  const r = lastRuns && lastRuns.gen === creds.gen && (lastRuns.runs || []).find((x) => x.task_id === task);
  const sc = r && allScenarios().find((x) => x.id === r.scenario);
  return (sc && sc.delegation) || null;
}
setSceneView((c) => scene.view(c));
// UX3 (docs/commercial-scenes.md): the scene the page shows, chosen first
// (UX2-D3); the customers' cards and the shop follow it. null until the
// catalog is known; only a catalog of more than one scene offers a choice.
let venue = null;
const allScenarios = () => (lastReady && lastReady.status && lastReady.status.runs && lastReady.status.runs.scenarios) || [];
const multiVenue = () => A.venues(allScenarios()).length > 1;
// runVenue: a task's scene: the catalog's only one ("" for the data shop
// alone, as before UX3), else its run's; null when there is a choice and
// the run is not known (not among the newest runs, or its entry changed).
const runVenue = (task) => {
  const vs = A.venues(allScenarios());
  if (vs.length < 2) return vs[0] && vs[0] !== "data" ? vs[0] : "";
  const r = lastRuns && lastRuns.gen === creds.gen && (lastRuns.runs || []).find((x) => x.task_id === task);
  return (r && r.scene) || null;
};
// The approval's goods and its scene rows; an unknown scene says so, never 數位資料 (review S2).
const goodsOf = (vk) => (vk ? A.venue(vk).goods : vk === null ? "商品・情境未確認" : "數位資料");
const venueRows = (vk) => (vk ? [["情境", A.venue(vk).name + (A.venue(vk).demo ? "（示範情境）" : "")], ["交付方式", A.venue(vk).delivery]]
  : vk === null ? [["情境", "未確認（不在最近的場次中，或目錄已改）"]] : []);
setVenueOf(runVenue);
onReplayMode((on) => {
  if (on) scene.pause();
  else scene.resume();
  // A replay plays on this page's own shop: shown while it plays, even with the audience window open (review 5).
  document.querySelector(".canvas-frame").hidden = sceneUnity.target === relay && !on;
  $("unity-status").hidden = sceneUnity.target === relay && !on;
});
const short = (c) => (c === "consumer-a" ? "A" : c === "consumer-b" ? "B" : c);

// hosting: this host starts runs (not history mode) and the page holds its credentials.
const hosting = () => !!(creds.view().hasHost && lastReady && lastReady.status && lastReady.status.runs && !lastReady.status.history);

// ensureScene starts the scene once the shop and the host allow it, and
// stops it when they no longer do (the shop goes back to the Dashboard).
function ensureScene() {
  const u = sceneUnity;
  if (hosting() && u.ready && u.supports2) {
    if (!scene.started) {
      scene.start();
      renderVenues(A.venues(allScenarios())); // the room before the runs
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
  if (scene.started && scene.venue && !(u.bridge && u.bridge.layouts.includes(scene.venue))) {
    cap = (cap ? cap + "；" : "") + "此 Unity 建置沒有「" + A.venue(scene.venue).name + "」的場景佈置：場景沿用預設房間";
  }
  $("unity-canvas").dataset.room = u.room ? u.room.scene + ":" + u.room.key : "";
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
  if (checker && checkerGen !== creds.gen) setChecker(null); // new credentials: the report may be of another show
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
    lastApprovals = null;
    delegating = null;
    simulating = false;
    resolving = false;
    approvalNote = null;
    lastA2 = null;
    a2Waiting = null;
    a2Op = null;
    a2Note = null;
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
        A.setApprovalMode(r.status && r.status.human_approval ? "webauthn" : "simulated");
        const scs = (r.status && r.status.runs && r.status.runs.scenarios) || [];
        const credential = scs.some((x) => x.contract === A.V2); // C2: a BBS+ credential catalog
        const payments = r.status && r.status.payment_capability;
        const authenticator = r.status && r.status.human_approval && r.status.human_approval.sources && r.status.human_approval.sources.authenticator;
        const p1a = r.status && r.status.human_approval ? { record: "fake_a2", credential, payments, authenticator }
          : scs.some((x) => A.isPay(x.contract)) ? { record: "fake_p1a", credential } : null;
        if (r.status) confirmMode(mseq, r.status.history && r.status.history.record === "live_history" ? r.status.history
          : r.status.live && r.status.live.record === "live_demo" ? { ...r.status.live, payments, credential, authenticator } : p1a);
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
    if (A.paysOn(st.payment_capability)) {
      rows.push(["付款", "info", st.payment_capability === "base_sepolia" ? "Base Sepolia testnet（測試 USDC；apa-relayer、每日上限；不是 mainnet）"
        : "本機 anvil（P2-a：測試金鑰、測試 USDC；不是 testnet）"]);
      rows.push(A.reconciliationRow(lastReady.reconciliation));
    } else rows.push(["付款", "off", "無付款能力"]);
    rows.push(...A.serviceRows(st.services)); // gap review C2: the Signer, its voice service, the relayer
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
  consumer_busy: "這位客人的場次尚未結束；請先等它結束，或按 Agent 卡片上的「停止 Agent」",
  capacity_full: "同時執行的場次已達上限（部署設定）；請先等一場結束或停止一場",
  no_free_task: "展示 task 已用完。下一步：找開發者擴充 registry；不要換新的資料庫（Signer 記得做過的 task，會拒絕重複的）", // C3 review: never a new APA_DB_PATH
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
    else business = (state === "ended" && slot.phase === "waiting" ? (run && A.isPay(run.contract_version) ? (A.simulated() ? "Agent 已結束；請購仍等你核准（模擬）" : "Agent 已結束；請購仍等你核准（本人裝置）")
      : "執行已結束／請購待核准（M1 無核准功能；到期前不會付款）") : "") || slot.label || slot.phase;
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

// renderVenues: the scene choice, first (UX2-D3): one button per scene the
// catalog offers; a demo scene says so. Choosing one changes the cards and
// the shop's room; it starts or stops nothing.
let shownVenues = "";
function renderVenues(vs) {
  // ponytail: the choice's buttons are rebuilt only when the scenes change; a
  // choice only moves aria-pressed, so the focus stays on the button (review S5).
  const box = $("venue-picker");
  box.hidden = vs.length < 2;
  const chip = $("venue-chip");
  chip.hidden = box.hidden;
  chip.textContent = box.hidden ? "" : "情境：" + A.venue(venue).name + (A.venue(venue).demo ? "（示範情境・示意商品）" : "");
  const u = shop();
  const room = venue && u.bridge && u.bridge.layouts.includes(venue) ? venue : "";
  // One scene: no filter, but its room (review S3).
  if (scene.started) scene.setVenue(vs.length > 1 ? venue : "", room);
  for (const b of box.querySelectorAll(".venue-pick")) b.setAttribute("aria-pressed", String(b.dataset.venue === venue));
  const key = JSON.stringify(vs);
  if (box.hidden || key === shownVenues) return;
  shownVenues = key;
  keepFocus(box, () => box.replaceChildren(...vs.map((k) => {
    const v = A.venue(k);
    const b = document.createElement("button");
    b.type = "button";
    b.className = "venue-pick";
    b.dataset.venue = k;
    b.setAttribute("aria-pressed", String(k === venue));
    b.append(v.name);
    if (v.demo) {
      const chip = document.createElement("span");
      chip.className = "venue-demo";
      chip.textContent = "示範情境";
      b.append(" ", chip);
    }
    b.addEventListener("click", () => {
      if (venue === k) return;
      venue = k;
      shownControl = "";
      renderControl();
      refreshPanels();
    });
    return b;
  })));
}

// ---- 簡報模式 (docs/commercial-town.md T1) ----
// The scene fills the screen; the brand, the venues (numbered, with their
// state), the selected customers' five steps and the approval float over it.
// A venue is chosen by its row; its own button starts it, as 一起開始 does
// for that venue (each customer's delegation confirmed first). Nothing here
// is a new way in: the same operations, the same credentials.
// T2: with a build that has the town, presenting starts in the town (every
// venue roofed); a venue's row or a click on its building enters it (its
// roof lifts, the camera steps in) and 回到城鎮 goes back. Unity only shows.
let presenting = (() => { try { return localStorage.getItem("apa-present") === "1"; } catch { return false; } })();
let inTown = true;
const hasTown = () => !!(shop().bridge && shop().bridge.town);
function setPresenting(on) {
  presenting = on;
  inTown = true;
  try { localStorage.setItem("apa-present", on ? "1" : "0"); } catch { /* not kept */ }
  renderPresent();
  dispatchEvent(new Event("resize")); // the scene refits to the whole window (pixelscale.js), and sendView follows
}
// enterVenue: the venue's room, as its row in the list chooses it.
function enterVenue(k) {
  inTown = false;
  if (venue !== k) { venue = k; shownControl = ""; renderControl(); refreshPanels(); }
  renderPresent();
}
onVenuePick((k) => { if (presenting && inTown && A.venues(allScenarios()).includes(k)) enterVenue(k); });
// sendView: the camera Unity should show, and the canvas's part the cards
// leave uncovered (between the two columns); sent when either changes.
let sentView = "";
function sendView() {
  if (!hasTown()) return; // a build without the town: its one camera, as before
  const on = presenting && creds.view().hasHost;
  let safe = null;
  if (on) {
    const c = $("unity-canvas").getBoundingClientRect(), l = document.querySelector(".present-left").getBoundingClientRect(), r = document.querySelector(".present-right").getBoundingClientRect();
    // In the canvas's own pixels (unity.js renders at the screen's density); the caption keeps the bottom.
    const d = window.devicePixelRatio || 1, x = Math.max(0, Math.round(l.right + 12 - c.left)), right = Math.min(c.width, Math.round(r.left - 12 - c.left));
    const bottom = Math.min(c.height, 140);
    if (right - x >= 120) safe = { x: Math.round(x * d), y: 0, width: Math.round((right - x) * d), height: Math.round((c.height - bottom) * d) };
  }
  // A venue whose room the build lacks would show the default room's roof lifting: the town stays (review).
  const room = shop().bridge.layouts.includes(venue);
  const v = { mode: on && (inTown || !room) ? "town" : "venue", safe };
  const key = JSON.stringify(v);
  if (key === sentView) return;
  sentView = key;
  shop().view(v.mode, v.safe);
}
addEventListener("resize", () => requestAnimationFrame(() => renderPresent())); // renderPresent sends the view; on ready the list learns of the town too
$("present-toggle").addEventListener("click", () => setPresenting(true));
$("present-exit").addEventListener("click", () => setPresenting(false));
$("present-town").addEventListener("click", () => { inTown = true; renderPresent(); });
addEventListener("keydown", (e) => { if (e.key === "Escape" && presenting && !document.querySelector("dialog[open]")) setPresenting(false); });

// renderCaption: the narration under the scene: each customer's step on screen, in plain words (the cards'
// caption), and, while the screen trails the real task, that it is playing the steps in order. A new step
// flashes once so the eye catches it.
let shownCaption = "";
function renderCaption(models) {
  const box = $("present-caption");
  const lines = presenting ? models.filter((m) => m.steps[0].state !== "now" || m.steps[0].text !== "等待開始") : [];
  box.hidden = !lines.length;
  const key = JSON.stringify(lines.map((m) => [m.caption, !!m.ahead]));
  if (key === shownCaption) return;
  shownCaption = key;
  box.replaceChildren(...lines.map((m) => {
    const p = document.createElement("p");
    p.dataset.consumer = m.consumer;
    const step = m.steps.findIndex((s) => s.state === "now");
    const n = document.createElement("span");
    n.className = "n";
    n.textContent = String((step >= 0 ? step : m.steps.reduce((i, s, j) => (s.state !== "todo" && s.state !== "off" ? j : i), 0)) + 1).padStart(2, "0");
    const t = document.createElement("span");
    t.textContent = m.caption;
    p.append(n, t);
    if (m.ahead) { const a = document.createElement("small"); a.textContent = "依序播放中"; p.append(a); }
    return p;
  }));
  box.classList.remove("flash");
  void box.offsetWidth; // restart the animation
  box.classList.add("flash");
}

// pairFor: a venue's first scenario per customer, as 一起開始 takes them.
function pairFor(k) {
  const scs = multiVenue() ? allScenarios().filter((x) => (x.scene || "data") === k) : allScenarios();
  return [...new Set(scs.map((sc) => sc.consumer_id))].sort()
    .map((c) => scs.find((x) => x.consumer_id === c && !A.isPay(x.contract)) || scs.find((x) => x.consumer_id === c)).filter(Boolean);
}

let shownPresent = "";
function renderPresent(runs = (lastRuns && lastRuns.gen === creds.gen && lastRuns.runs) || []) {
  const v = creds.view();
  $("present-toggle").hidden = !v.hasHost;
  const on = presenting && v.hasHost;
  document.body.dataset.present = on ? "on" : "off";
  $("present").hidden = !on;
  // The approval panel stacks under the venues while presenting, and goes back to its side after.
  const ap = $("approval"), right = document.querySelector(".present-right"), side = document.querySelector(".side");
  if (on && ap.parentElement !== right) right.append(ap);
  else if (!on && ap.parentElement !== side) side.prepend(ap);
  sendView();
  if (!on) return;
  const st = (lastReady && lastReady.status) || {};
  const ha = st.human_approval;
  const traits = { payments: A.paysOn(st.payment_capability) ? st.payment_capability : "", authenticator: (ha && ha.sources && ha.sources.authenticator) || "" };
  const vs = A.venues(allScenarios());
  const active = runs.filter((r) => r.live === "running" || r.live === "stopping");
  const venues = vs.map((k, i) => {
    const scs = allScenarios().filter((x) => (x.scene || "data") === k);
    const first = scs[0];
    return { k, n: String(i + 1).padStart(2, "0"), name: A.venue(k).name, demo: !!A.venue(k).demo,
      sub: first ? String(first.label || "").replace(/^[^：]*：/, "") : "", busy: active.some((r) => (r.scene || "data") === k) };
  });
  const slots = scene.started ? scene.slots().filter((s) => s.task) : [];
  const custs = A.showQueue(slots.map((s) => ({ ...A.showCustomer(s.consumer, scene.snapshot(s.consumer), delegationOf(s.task), traits),
    receipt: A.showReceipt(scene.snapshot(s.consumer), traits),
    // The screen plays the steps in order: when it trails, where the real task already is.
    ahead: scene.behind(s.consumer) ? A.showCustomer(s.consumer, scene.latest(s.consumer), delegationOf(s.task), traits).caption : "" })), traits.payments);
  renderCaption(custs.models);
  const town = hasTown() && inTown;
  const key = JSON.stringify([venues, venue, town, hasTown(), custs, starting, simulating, $("run-status").textContent]);
  if (key === shownPresent) return;
  shownPresent = key;
  const el = (tag, cls, text) => { const e = document.createElement(tag); if (cls) e.className = cls; if (text !== undefined) e.textContent = text; return e; };
  keepFocus($("present-venue-list"), () => $("present-venue-list").replaceChildren(...venues.map((x) => {
    const li = el("li");
    const b = el("button", "venue-item");
    b.type = "button";
    b.dataset.venue = x.k;
    b.setAttribute("aria-pressed", String(x.k === venue && !town));
    const name = el("span", "name", x.name);
    name.append(el("small", "", (x.demo ? "示範情境・" : "") + x.sub));
    const state = el("span", "state", x.busy ? "交易中" : "待命");
    state.dataset.state = x.busy ? "busy" : "idle";
    b.append(el("span", "n", x.n), name, state);
    b.addEventListener("click", () => enterVenue(x.k));
    li.append(b);
    if (x.k === venue && !town) {
      const go = el("button", "venue-go", "開始這個場域的交易");
      go.type = "button";
      go.disabled = starting || simulating || x.busy;
      go.title = x.busy ? "這個場域的交易還在進行" : "每位客人先確認委託，再各自開始";
      go.addEventListener("click", () => startAll(pairFor(x.k)));
      li.append(go);
    }
    return li;
  })));
  $("present-town").hidden = !(hasTown() && !inTown);
  $("present-status").textContent = $("run-status").textContent;
  const steps = $("present-steps");
  steps.hidden = !custs.models.length;
  steps.replaceChildren(el("p", "eyebrow", "THE TRANSACTION"), ...custs.models.map((m) => {
    const box = el("div", "present-cust");
    box.dataset.consumer = m.consumer;
    box.append(el("h3", "", m.name + " 的 Agent" + (m.amount ? "・" + m.amount : "")));
    if (m.ahead) box.append(el("p", "present-ahead", "依序播放中（實際已到：" + m.ahead.replace(/^[^・]*・/, "") + "）"));
    const ol = el("ol", "present-step-list");
    m.steps.forEach((s, i) => {
      const li = el("li");
      li.dataset.state = s.state;
      const body = el("span", "", s.label);
      if (s.text) body.append(el("small", "", s.text));
      li.append(el("span", "n", String(i + 1).padStart(2, "0")), body);
      ol.append(li);
    });
    box.append(ol);
    if (m.receipt) {
      const r = el("div", "present-receipt");
      for (const row of m.receipt.rows) {
        const chip = row.href ? el("a", "", row.label + " ↗") : el("span", "", row.label + "：" + row.text);
        if (row.href) { chip.href = row.href; chip.target = "_blank"; chip.rel = "noopener noreferrer"; }
        r.append(chip);
      }
      box.append(r);
    }
    return box;
  }), ...(custs.note ? [el("p", "present-queue", custs.note)] : []));
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
  const vs = A.venues(allScenarios());
  if (!vs.includes(venue)) venue = vs[0] || null;
  // With a choice of scenes, the cards, 一起開始 and the shop are the chosen scene's.
  const scenarios = vs.length > 1 ? allScenarios().filter((x) => (x.scene || "data") === venue) : allScenarios();
  renderVenues(vs);
  const live = !!(lastReady && lastReady.status && lastReady.status.live);
  $("live-warning").hidden = !live;
  for (const x of $("live-warning").querySelectorAll("summary, p")) {
    if (!x.dataset.plain) x.dataset.plain = x.textContent;
    x.textContent = live && payingNow() ? x.dataset.plain.replace(/合成資料([・、])不付款/, "合成資料$1付款以" + A.PAYING[lastReady.status.payment_capability] + "（測試 USDC）實際進行$1" + (deviceDelegations() ? "委託由客人本人以裝置確認" : "委託是模擬")) : x.dataset.plain;
  }
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
  if (delegating && delegating.gen !== creds.gen) delegating = null;
  const simC = v.simConsumer;
  const key = JSON.stringify([models, scenarios, live, starting, cancelling, full, [...busy], confirming, simC, delegating, simulating, deviceDelegations()]);
  if (key !== shownControl) {
    shownControl = key;
    keepFocus(pick, () => pick.replaceChildren(...models.map((m) => agentCard(m, scenarios.filter((x) => x.consumer_id === m.c), { live, busy, full, simC }))));
  }
  const pay = scenarios.some((x) => A.isPay(x.contract));
  $("control-note").textContent = !pay ? "M1 無核准能力：Ask 30 秒後由後端記錄到期。開始只建立新的 task。"
    : deviceDelegations() ? "委託由客人用已登記的裝置確認（本人；Signer 驗證），Ask 由" + approver() + "核准；" + (payingNow() ? "Allow 與本人核准的請購由 Host 付款（" + A.PAYING[lastReady.status.payment_capability] + "、測試 USDC），Signer 只在客人確認的委託範圍內簽。" : "未啟用付款。") + "開始只建立新的 task。"
    : payingNow() ? "委託是 fake 模擬：主持人扮演客人確認委託（不是本人驗證）。Ask 由" + approver() + "核准；Allow 與本人核准的請購由 Host 付款（" + A.PAYING[lastReady.status.payment_capability] + "、測試 USDC）。開始只建立新的 task。"
    : humanNow() ? "委託是 fake 模擬：主持人扮演客人確認委託（不是本人驗證）。Ask 由" + approver() + "核准；未啟用付款。開始只建立新的 task。"
    : "委託與核准是 fake 模擬：主持人扮演客人，不是本人驗證；核准只留下模擬授權，未啟用付款。開始只建立新的 task。";
  const chip = $("sim-chip");
  chip.hidden = !simC;
  chip.textContent = simC ? "角色模擬：客人 " + short(simC) + "（主持人扮演，不是本人驗證）" : "";
  renderApproval(runs);
  // 一起開始: each consumer's first scenario, as separate requests; not one
  // atomic start (docs/multi-agent-demo-design.md 3.5).
  // An apa.pay.v1 scenario starts only with its own confirmed delegation: the
  // host plays each customer in turn and confirms it first (batch 3, Aaron 10/24).
  const pair = consumers.map((c) => scenarios.find((x) => x.consumer_id === c && !A.isPay(x.contract)) || scenarios.find((x) => x.consumer_id === c)).filter(Boolean);
  const all = $("start-all");
  all.hidden = pair.length < 2 || maxRuns < 2;
  all.textContent = "一起開始：" + pair.map((sc) => sc.id + "（" + short(sc.consumer_id) + "）").join(" ＋ ")
    + (!pair.some((sc) => A.isPay(sc.contract)) ? "（各自送出，不保證都成功）"
      : deviceDelegations() ? "（依序：每位客人用自己的裝置確認委託，再開始；各自送出，不保證都成功）" : "（依序：主持人扮演每位客人確認委託（模擬）再開始；各自送出，不保證都成功）");
  all.disabled = starting || simulating || pair.every((sc) => busy.has(sc.consumer_id)) || full;
  all.onclick = () => startAll(pair);
  renderNext(runs);
  renderPresent(runs);
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
const FOCUS_KINDS = ["cancel-confirm", "cancel-back", "row-cancel", "task-cancel", "row-stop", "run-stop", "agent-select", "agent-start",
  "agent-sim", "agent-delegate", "delegation-confirm", "delegation-back"];
function keepFocus(el, rebuild) {
  const a = document.activeElement;
  const kind = a && el.contains(a) ? FOCUS_KINDS.find((k) => a.classList.contains(k)) : null;
  const card = kind && a.closest(".agent-card"), li = kind && a.closest("li[data-run]");
  const built = card || li; // the generation the control was made in, not the one now
  const was = kind && { gen: Number(built ? built.dataset.gen : -1), kind, consumer: card ? card.dataset.consumer : "", run: li ? li.dataset.run : "", ownRun: a.dataset.run || "", task: a.dataset.task || "", scenario: a.dataset.scenario || "" };
  rebuild();
  // The focus goes back where it was without moving the page: a scroll to the
  // Agent card hid the phrase to read (V1 device round).
  if (!was || (document.activeElement && document.activeElement !== document.body && document.activeElement !== a)) return;
  const usable = (x) => !!x && !x.disabled && x.offsetParent !== null;
  const scope = was.consumer ? el.querySelector('.agent-card[data-consumer="' + CSS.escape(was.consumer) + '"]') : was.run ? el.querySelector('li[data-run="' + CSS.escape(was.run) + '"]') : null;
  if (was.gen === creds.gen && scope) {
    const attr = (k, v) => (v ? "[data-" + k + '="' + CSS.escape(v) + '"]' : "");
    const same = scope.querySelector("." + was.kind + attr("task", was.task) + attr("scenario", was.scenario) + attr("run", was.ownRun));
    if (usable(same)) return same.focus({ preventScroll: true });
    // A confirmation closed (返回, or its task ended): its own entry, if the task still has one.
    const entry = was.task && scope.querySelector('.row-cancel[data-task="' + CSS.escape(was.task) + '"], .task-cancel[data-task="' + CSS.escape(was.task) + '"]');
    if (usable(entry)) return entry.focus({ preventScroll: true });
    const sel = scope.querySelector(".agent-select");
    if (usable(sel)) return sel.focus({ preventScroll: true });
  }
  // Another generation, or nothing of that customer left: a control that only shows.
  const safe = ["pair-code", "load-file", "show-evidence"].map((id) => $(id)).find(usable);
  if (safe) safe.focus({ preventScroll: true });
}

// agentCard draws one customer's card. Clicking the card (not a button)
// chooses the customer, as does its 選取 button and a click on it in the shop.
function agentCard(m, scs, { live, busy, full, simC }) {
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
  const payScs = scs.filter((sc) => A.isPay(sc.contract));
  if (payScs.length) {
    if (simC !== m.c && !deviceDelegations()) { // gap 4: with the person's device, nobody plays the customer
      const b = el("button", "agent-sim", live ? "以 " + short(m.c) + " 確認委託（模擬）" : "以 " + short(m.c) + " 模擬（fake）");
      b.type = "button";
      b.disabled = simulating;
      b.title = A.paysOn(lastReady && lastReady.status && lastReady.status.payment_capability)
        ? "主持人扮演客人 " + short(m.c) + " 確認委託（不是本人驗證；Ask 仍由" + approver() + "核准）；一次扮演一位"
        : "主持人扮演客人 " + short(m.c) + "：確認委託與核准都只在 fake 模式，不是本人驗證；一次扮演一位";
      b.addEventListener("click", () => simulateAs(m.c));
      acts.append(b);
    }
    for (const sc of payScs) {
      const b = el("button", "agent-delegate", "建立委託" + (payScs.length > 1 ? "：" + sc.label : ""));
      b.type = "button";
      b.dataset.scenario = sc.id;
      const needSim = !deviceDelegations() && simC !== m.c;
      b.disabled = starting || busy.has(m.c) || full || needSim;
      b.title = needSim ? "先「以 " + short(m.c) + " 模擬」" : busy.has(m.c) ? "這位客人的場次尚未結束" : full ? "同時執行的場次已達上限" : "看委託內容，確認後開始";
      b.addEventListener("click", () => { delegating = { gen: creds.gen, consumer: m.c, scenario: sc.id }; renderControl(); focusIn('.delegation[data-scenario="' + CSS.escape(sc.id) + '"] .delegation-confirm'); });
      acts.append(b);
    }
  }
  for (const sc of scs.filter((x) => !A.isPay(x.contract))) {
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
  if (simC === m.c) {
    // The role play is the delegation's confirmation; on a paying host the payment is real.
    card.append(el("p", "agent-sim-on", "模擬中：主持人扮演客人 " + short(m.c) + (payingNow() ? " 確認委託（不是本人驗證；付款仍由 Host 實際進行）" : "（不是本人驗證；不會付款）")));
  }
  const open = delegating && delegating.consumer === m.c && payScs.find((sc) => sc.id === delegating.scenario);
  if (open) card.append(delegationBox(open, { busy, full, simC }));
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
const cancelScope = () => (payingNow() ? CANCEL_SCOPE.replace("；不付款，", "；之後不再付款，已完成的付款不會撤回；") : CANCEL_SCOPE);

function confirmBox(taskId) {
  const box = document.createElement("div");
  box.className = "agent-confirm";
  box.dataset.task = taskId;
  box.setAttribute("role", "group");
  box.setAttribute("aria-label", "確認取消 task " + taskId);
  const p = document.createElement("p");
  p.textContent = "確認取消 task " + taskId + "？" + cancelScope();
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
    else if (cancellable(run)) text = A.isPay(run.contract_version) ? "可用操作：在「需要你處理」核准或拒絕（" + (humanNow() ? approver() : "模擬") + "）；或取消請購／task。"
      + (payingNow() ? "放行與核准的請購由 Host 付款（測試 USDC）。" : "不會付款。")
      : "可用操作：取消請購／task。M1 沒有核准功能，Ask 不會付款，到期後由後端記錄。";
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

// start: one start operation; only a reply of this generation acts. An
// apa.pay.v1 scenario starts with its confirmed delegation.
async function start(sc, delegationId) {
  if (starting) return; // one start operation at a time (a second click, even on a re-rendered button, does nothing)
  const g = creds.gen;
  starting = true;
  renderControl();
  setRunStatus("開始中…", "busy");
  const r = await creds.startRun(sc.id, delegationId);
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

// ---- P1-a: the simulated customer, the delegation and the approval ----

const focusIn = (sel) => { const x = document.querySelector(sel); if (x && !x.disabled) x.focus(); };
const units = (a) => (typeof a === "string" && /^[0-9]+$/.test(a) ? (Number(a) / 1e6).toFixed(2) : "（金額未提供）"); // fixture-usdc: 6 decimals
// payingNow, humanNow: the host pays (P2), the person approves by device (A2), by its own status.
const payingNow = () => A.paysOn(lastReady && lastReady.status && lastReady.status.payment_capability);
const humanNow = () => !!(lastReady && lastReady.status && lastReady.status.human_approval);
// approver: who approves, by the authenticator the operator says verifies (never a virtual one as a device).
const approver = () => A.approverText(humanNow() && lastReady.status.human_approval.sources && lastReady.status.human_approval.sources.authenticator);
// network names the chain by the host's own payment capability: the
// testnet only when the host pays on it (a local anvil also runs as 84532).
const network = (id) => {
  const cap = lastReady && lastReady.status && lastReady.status.payment_capability;
  return cap === "base_sepolia" ? "Base Sepolia testnet（" + id + "）" : cap === "local_anvil" ? "本機 anvil（chain " + id + "）" : "chain " + id + "（不付款）";
};
const addr = (x) => (typeof x === "string" && x.length > 12 ? x.slice(0, 6) + "…" + x.slice(-4) : String(x || "（未提供）"));
// productOf: the item's name from its resource URL's last segment.
const productOf = (p) => { try { return new URL(p.resource_url).pathname.split("/").pop() || "（未命名）"; } catch { return "（未命名）"; } };
const clock = (sec) => (typeof sec === "number" ? new Date(sec * 1000).toLocaleTimeString("zh-TW", { hour12: false }) : "（時間未提供）");

// simDelegationOnly: the simulated customer may confirm delegations only
// (E1-a: a live host that pays); its approvals are never read.
let simDelegationOnly = false;

// simulateAs asks the host to let this page act as consumer c (fake mode; on
// a live host that pays, for its delegations only).
async function simulateAs(c) {
  if (simulating) return;
  const g = creds.gen;
  simulating = true;
  renderControl();
  const r = await creds.simulate(c);
  if (g !== creds.gen) return;
  simulating = false;
  if (!r || r.error) setRunStatus("沒有收到回覆：角色模擬未確認", "error");
  else if (r.status === 200) {
    simDelegationOnly = !!(r.body && r.body.delegation_only); // E1-a: a live host that pays
    setRunStatus(simDelegationOnly ? "角色模擬：客人 " + short(c) + "（主持人扮演，只能確認委託，不是本人驗證；Ask 由" + approver() + "核准）"
      : "角色模擬：客人 " + short(c) + "（主持人扮演，不是本人驗證；只在 fake 模式）", "ok");
    lastApprovals = null;
    approvalNote = null;
    pollRuns();
  } else setRunStatus(r.body && r.body.error === "simulation_unavailable" ? "這個服務沒有角色模擬（只有 fake 模式有）" : "角色模擬未開始（" + r.status + "）", "error");
  renderControl();
  focusIn('.agent-card[data-consumer="' + CSS.escape(c) + '"] .agent-delegate');
}

// deviceDelegations (gap 4; docs/commercial-gap4.md 2.3): on a host with the
// person's approval by device (A2), a delegation is confirmed by the
// customer's own registered device too, through the Host and the Signer.
const deviceDelegations = () => a2Mode();

// deviceDelegation: the customer confirms sc's delegation with their device.
// The challenge is checked to be this one's (the consumer, the delegation
// it names, approve) before the device is asked; a verify left without an
// answer is sent again as it was (the Signer answers what it kept).
// { id } of the delegation, { fail } saying what is known, or null once the
// credentials changed.
async function deviceDelegation(sc, g) {
  if (!a2OnOrigin()) return { fail: "要從 " + a2Info().origin + "/host 開啟這個頁面才能用裝置確認（WebAuthn 只接受 localhost）" };
  const c = sc.consumer_id;
  const requestId = "dlg-" + crypto.randomUUID();
  const r = await a2Call(c, "/api/delegations/challenge", { request_id: requestId, scenario: sc.id });
  if (g !== creds.gen) return null;
  if (r.status !== 200) return { fail: delegationFail(r) };
  const ch = r.body && r.body.challenge, t = r.body && r.body.terms;
  if (!ch || !t || ch.consumer_id !== c || t.consumer_id !== c || ch.approval_session_id !== t.delegation_id || ch.decision !== "approve" || !ch.options || !ch.options.publicKey) {
    return { fail: "回傳的資料和這次確認不一致，已停止，沒有要求裝置驗證" };
  }
  let cred;
  try {
    cred = await navigator.credentials.get({ publicKey: AP.requestOptions(ch.options) });
  } catch (e) {
    if (g !== creds.gen) return null;
    return { fail: e && e.name === "NotAllowedError" ? "裝置驗證沒有完成（取消、逾時或沒有可用的裝置）" : "瀏覽器拒絕這次驗證（" + (e && e.name) + "）" };
  }
  if (g !== creds.gen) return null;
  const body = { request_id: requestId, scenario: sc.id, challenge_id: ch.challenge_id, verify_request_id: "dver-" + crypto.randomUUID(), assertion: AP.assertionJSON(cred) };
  let v;
  for (let i = 0; i < 4; i++) { // the same verify again on no answer (review: about 7 s in all)
    if (i) await new Promise((res) => setTimeout(res, 1000 * 2 ** (i - 1)));
    v = await a2Call(c, "/api/delegations/assertion", body);
    if (g !== creds.gen) return null;
    if ((v.status === 201 || v.status === 200) && v.body && v.body.delegation) return { id: v.body.delegation.delegation_id };
    if (v.status !== 0 && v.status !== 503 || v.body && v.body.error === "no_viewer") break;
  }
  return { fail: delegationFail(v) + "（不會自動開始）" };
}

// delegationFail: a refused confirmation in the host's words.
function delegationFail(r) {
  const b = r.body || {};
  if (r.status === 0 && !(r.body && r.body.error === "no_viewer")) return "沒有收到回覆，結果未確認";
  if (b.error === "signer_refused") return AP.CODES[b.code] || "Signer 拒絕（" + b.code + "）";
  return { delegation_unavailable: "這個主機沒有這位客人的錢包或資產設定", forbidden_consumer: "不是這位客人的情境", signer_unknown: "Signer 沒有回覆，結果未確認",
    store_unavailable: "裝置已驗證，但主機沒能記下；請再確認一次（會是新的一份委託）", no_viewer: "這位客人沒有 viewer token" }[b.error] || "未確認（" + (b.error || r.status) + "）";
}

// delegationBox shows the host's terms of an apa.pay.v1 scenario: the
// customer only confirms them (nothing here is editable).
function delegationBox(sc, { busy, full, simC }) {
  const el = (tag, cls, text) => { const e = document.createElement(tag); if (cls) e.className = cls; if (text !== undefined) e.textContent = text; return e; };
  const d = sc.delegation || {};
  const box = el("div", "delegation");
  box.dataset.scenario = sc.id;
  box.setAttribute("role", "group");
  box.setAttribute("aria-label", "委託內容：" + sc.label);
  box.append(el("p", "delegation-title", "委託內容（主持人設定，客人確認）"));
  const dl = el("dl", "delegation-rows");
  for (const [k, v] of [["需求", d.purpose], ["商家範圍", (d.merchants || []).map(addr).join("、") + "（示意商店）"],
    ["資產與網路", "test USDC" + (d.asset_id ? "（" + d.asset_id + "）" : "") + " · " + network(d.chain_id || "84532")], ["任務預算", units(d.task_budget) + " test USDC"],
    ["自動範圍", "超過 " + units(d.allow_max) + " 一律要你核准"], ["核准期限", (d.approval_deadline_seconds ? d.approval_deadline_seconds + " 秒（後端時間為準）" : "依後端設定")],
    ["委託有效", Math.round((d.valid_seconds || 0) / 60) + " 分鐘內開始"]]) {
    dl.append(el("dt", "", k), el("dd", "", String(v)));
  }
  const device = deviceDelegations();
  box.append(dl, el("p", "delegation-note", "委託不是付款核准：每一筆超過範圍的請購仍要你單筆核准。"
    + (device ? "客人用自己已登記的裝置確認這份委託（Signer 驗證，本人確認）；Signer 只在這份委託的範圍內簽付款。" + (payingNow() ? "範圍內與你核准的請購由 Host 實際付款（測試 USDC）。" : "")
      : payingNow() ? "委託確認是 fake 模擬（主持人扮演客人）；範圍內與你核准的請購由 Host 實際付款（測試 USDC）。" : "這是 fake 模擬，不會付款。")));
  const yes = el("button", "delegation-confirm", device ? "用已登記的裝置確認委託並開始" : "確認委託並開始（模擬）");
  yes.type = "button";
  yes.dataset.scenario = sc.id;
  yes.disabled = starting || simulating || busy.has(sc.consumer_id) || full || !device && simC !== sc.consumer_id;
  yes.addEventListener("click", () => confirmAndStart(sc));
  const no = el("button", "delegation-back", "返回");
  no.type = "button";
  no.dataset.scenario = sc.id;
  no.addEventListener("click", () => { delegating = null; renderControl(); focusIn('.agent-card[data-consumer="' + CSS.escape(sc.consumer_id) + '"] .agent-delegate'); });
  box.append(yes, " ", no);
  return box;
}

// confirmAndStart: the simulated customer confirms the delegation (one
// request ID, resent unchanged), then the operator starts the run with it.
async function confirmAndStart(sc) {
  if (starting) return;
  const g = creds.gen;
  if (deviceDelegations()) { // gap 4: the customer's own device, not the host
    if (simulating) return; // one confirmation at a time (review: a second prompt, or 一起開始 meanwhile)
    simulating = true;
    renderControl();
    setRunStatus("客人 " + short(sc.consumer_id) + "：請用已登記的裝置確認委託…", "busy");
    let d;
    try { d = await deviceDelegation(sc, g); } finally { if (g === creds.gen) { simulating = false; renderControl(); } }
    if (!d) return;
    if (d.fail) return setRunStatus("客人 " + short(sc.consumer_id) + " 的委託未確認：" + d.fail + "（沒有開始場次）", "error");
    delegating = null;
    return start(sc, d.id);
  }
  setRunStatus("確認委託中…", "busy");
  const r = await creds.confirmDelegation(sc.id);
  if (g !== creds.gen || !r) return;
  // Another customer is simulated now (or the same one again): the old
  // confirmation starts nothing; whatever the host recorded stays there.
  if (r.superseded || r.simId !== creds.view().simId) {
    setRunStatus("客人 " + short(r.consumer) + " 的委託確認：模擬身分已更換，結果未確認，沒有開始場次（不會用新身分補送）", "warn");
    return;
  }
  if (r.error || !(r.status === 201 || r.status === 200) || !r.body || !r.body.delegation) {
    setRunStatus(r.error ? "沒有收到回覆：委託未確認（不會自動開始）" : "委託未確認（" + ((r.body && r.body.error) || r.status) + "）", "error");
    return;
  }
  delegating = null;
  await start(sc, r.body.delegation.delegation_id);
}

// asCustomer: the host plays sc's customer and confirms sc's delegation, as
// 「以 A 模擬」 and 「確認委託並開始（模擬）」 do: { id } of the delegation, or
// { fail } saying what is known; null once the credentials changed.
async function asCustomer(sc, g) {
  if (deviceDelegations()) return deviceDelegation(sc, g); // gap 4: each customer's own device, in turn
  const s = await creds.simulate(sc.consumer_id);
  if (g !== creds.gen) return null;
  if (!s || s.error || s.status !== 200) return { fail: s && s.error ? "角色模擬沒有收到回覆，未確認" : "角色模擬未開始（" + ((s && s.status) || "無回覆") + "）" };
  // The page must now act as this customer (review): never confirm under another one's capability.
  if (creds.view().simConsumer !== sc.consumer_id) return { fail: "角色模擬沒有換成這位客人，委託未確認" };
  simDelegationOnly = !!(s.body && s.body.delegation_only);
  lastApprovals = null;
  approvalNote = null;
  const r = await creds.confirmDelegation(sc.id);
  if (g !== creds.gen) return null;
  if (!r) return { fail: "模擬身分已失效，委託未確認" };
  if (r.superseded) return { fail: "模擬身分已更換，委託結果未確認" };
  if (r.error || !(r.status === 201 || r.status === 200) || !r.body || !r.body.delegation) return { fail: r.error ? "委託沒有收到回覆，未確認" : "委託未確認（" + ((r.body && r.body.error) || r.status) + "）" };
  return { id: r.body.delegation.delegation_id };
}

// pollApprovals reads the simulated customer's approval sessions (its own
// capability); a reply of an older generation or customer is dropped.
async function pollApprovals() {
  const { simConsumer: c, simId } = creds.view();
  if (!c || simDelegationOnly) { if (lastApprovals) { lastApprovals = null; renderControl(); } return; }
  const g = creds.gen;
  const r = await creds.approvals();
  if (!r || r.superseded || g !== creds.gen || creds.view().simId !== simId) return;
  lastApprovals = r.error || r.status !== 200 ? { gen: g, simId, consumer: c, error: r.error || "status_" + r.status }
    : { gen: g, simId, consumer: c, servedAt: r.body.served_at, at: Date.now(), items: r.body.approvals || [] };
  renderControl();
}

// Closures; approved and denied are worded by the record (AP.decidedText).
// C2: a withdrawal for a credential reason is the credential's, not the task's.
const closedResult = (table, state, p) => (state === "withdrawn" && A.CREDENTIAL_CLOSED[p.closed_reason]
  ? ["closed", A.CREDENTIAL_CLOSED[p.closed_reason], "核准時重新檢查憑證未通過；沒有發出授權，不會付款。"] : table[state]);
const RESULT = {
  expired: ["closed", "已過期，無法核准", "期限由後端決定；需要重新請購。"],
  withdrawn: ["closed", "已撤回（task 取消）", "這筆請購已隨 task 取消。"],
  superseded: ["closed", "報價已改變：這次核准作廢", "需要重新請購；沒有付款。"],
};

// pick: the item 「需要你處理」 shows: the newest pending one; else the
// newest item of the customer's newest run (its result); else none.
function pickItem(runs) {
  const a = lastApprovals;
  if (!a || a.gen !== creds.gen || a.simId !== creds.view().simId || !a.items) return null;
  // the newest pending one a person can still decide (an ended task's Ask cannot be)
  const pending = a.items.find((x) => x.purchase.approval && x.purchase.approval.state === "pending" && !x.task_ended);
  if (pending) return pending;
  const run = runs.find((r) => r.consumer_id === a.consumer);
  return (run && a.items.find((x) => x.task_id === run.task_id)) || null;
}

let shownApproval = "";
function renderApproval(runs) {
  if (a2Mode()) return renderA2();
  const box = $("approval");
  const v = creds.view();
  const simC = v.simConsumer;
  const scenarios = (lastReady && lastReady.status && lastReady.status.runs && lastReady.status.runs.scenarios) || [];
  const pay = scenarios.some((x) => A.isPay(x.contract));
  box.hidden = !v.hasHost || !pay;
  if (box.hidden) return;
  const item = simC ? pickItem(runs) : null;
  const target = A.approvalTarget(creds.gen, v.simId, item);
  // A note belongs to the item and capability it was made for, never to another.
  if (approvalNote && !A.sameTarget(approvalNote.target, target, false)) approvalNote = null;
  const a = lastApprovals && lastApprovals.gen === creds.gen && lastApprovals.simId === v.simId ? lastApprovals : null;
  const key = JSON.stringify([simC, v.simId, item, !!a, a && a.error, approvalNote, resolving]);
  if (key === shownApproval) return updateCountdown();
  shownApproval = key;
  const body = $("approval-body");
  const el = (tag, cls, text) => { const e = document.createElement(tag); if (cls) e.className = cls; if (text !== undefined) e.textContent = text; return e; };
  // The focused control and what it acted on (R1): it comes back only to
  // the same control of the same item, capability and generation.
  const act = document.activeElement && box.contains(document.activeElement) ? document.activeElement : null;
  const had = act ? { kind: act.className.split(" ")[0], target: act.dataset.session ? { ...act.dataset, pending: true } : null } : null;
  const parts = [];
  if (!simC) {
    parts.push(el("p", "approval-empty", "選一位客人按「以 A 模擬」或「以 B 模擬」，才能替他確認委託與核准（fake 模擬，不是本人驗證）。"));
  } else if (a && a.error && !item) {
    parts.push(el("p", "approval-empty", "客人 " + short(simC) + " 的核准清單讀取失敗（" + a.error + "），未確認"));
  } else if (!item) {
    parts.push(el("p", "approval-empty", a ? "客人 " + short(simC) + " 目前沒有需要你處理的請購。" : "讀取客人 " + short(simC) + " 的待核准…"));
  } else {
    const p = item.purchase, ap = p.approval, d = item.delegation;
    const mode = el("p", "approval-mode");
    const other = AP.recordTag(p, "simulated");
    mode.append(el("span", "approval-tag", other ? other[0] : "模擬核准"), other ? other[1] : "fake 展示・主持人扮演客人 " + short(item.consumer_id) + "（不是本人驗證）・不會付款");
    parts.push(mode);
    // The task ended (a CLI timeout, a Deny): its open Ask is decided by nobody (cli-timeout-pending).
    const ended = ap.state === "pending" && !!item.task_ended;
    const pending = ap.state === "pending" && !ended;
    const res = ended ? ENDED : AP.decidedText(ap.state, p) || closedResult(RESULT, ap.state, p);
    const head = el("div", "approval-head");
    head.dataset.state = ended ? "ended" : ap.state;
    head.append(el("h3", "", pending ? "核准這一筆付款？" : res ? res[1] : ap.state),
      el("p", "", pending ? "客人 " + short(item.consumer_id) + " 的 Agent 想買下面這一項。你只核准這一筆，不是之後所有付款。" : res ? res[2] : ""));
    parts.push(head);
    const product = productOf(p);
    const it = el("div", "approval-item");
    const amt = el("p", "approval-amount");
    amt.append(el("b", "", units(p.amount)), el("span", "", " test USDC"));
    const vk = runVenue(item.task_id);
    it.append(el("p", "approval-product", "商品 " + product + "（" + goodsOf(vk) + "・付款確認後交付；本片不付款）"), amt);
    parts.push(it);
    const dl = el("dl", "approval-rows");
    for (const [k, val] of [...venueRows(vk),
      ["收款人", addr(p.pay_to) + " · 與報價相同（payTo）"], ["付款帳戶", "客人 " + short(item.consumer_id) + " 的測試錢包 " + addr(item.payer) + " · " + item.wallet_id],
      ["網路與資產", network(p.chain_id) + " · test USDC" + (p.asset_id ? "（" + p.asset_id + "）" : "") + "，不是真錢"],
      ["委託範圍", d ? "預算 " + units(d.task_budget) + "；超過 " + units(d.allow_max) + " 一律要你核准" : "未取得"],
      ["intent", (p.intent_hash || "").slice(0, 23) + "…（後端比對這一份）"],
      ["語音", "未評估（模擬核准不含語音挑戰）"]]) { // V1 D9: the simulated path asks for no voice
      dl.append(el("dt", "", k), el("dd", "", val));
    }
    parts.push(dl);
    if (pending) {
      const dl2 = el("div", "approval-deadline");
      const cd = el("b", "", "");
      cd.id = "approval-countdown";
      dl2.append(cd, el("span", "", "期限 " + clock(ap.expires_at) + "（後端時間為準；畫面倒數只是提示）"));
      parts.push(dl2);
      parts.push(el("p", "approval-note", "按下核准後：這是模擬，只留下不能付款的模擬授權，畫面會標示未啟用付款。過期、取消或報價改變時，核准會被拒絕，需要重新請購。"));
      const acts = el("div", "approval-actions");
      const yes = el("button", "approval-approve", "核准此筆（模擬）");
      const no = el("button", "approval-deny", "拒絕");
      for (const [b, dec] of [[yes, "approve"], [no, "deny"]]) {
        b.type = "button";
        Object.assign(b.dataset, target, { pending: "" });
        b.disabled = resolving;
        b.addEventListener("click", () => resolveItem(item, dec));
        acts.append(b, " ");
      }
      parts.push(acts);
    } else if (ap.grant) {
      parts.push(el("p", "approval-grant", AP.grantLine(ap.grant, !!(lastA2 && lastA2.pays))));
    }
    const ev = el("button", "approval-evidence", "查看技術證據 ›");
    ev.type = "button";
    Object.assign(ev.dataset, target, { pending: "" });
    ev.addEventListener("click", async () => { await selectConsumer(item.consumer_id); if (currentTask() !== item.task_id) openTask(item.task_id); openDrawer(); });
    parts.push(ev);
  }
  if (approvalNote) {
    const n = el("p", "approval-msg", approvalNote.text);
    n.dataset.kind = approvalNote.kind;
    n.setAttribute("role", "status");
    parts.push(n);
  }
  body.replaceChildren(...parts);
  updateCountdown();
  // The focus stays on the same control of the same item; any other change
  // (another item, a closed session, another capability or generation)
  // gives it to the heading, which submits nothing. Nothing is clicked.
  if (had) {
    const same = had.target && body.querySelector("." + CSS.escape(had.kind));
    const submits = had.kind === "approval-approve" || had.kind === "approval-deny";
    if (same && !same.disabled && A.sameTarget(had.target, target, submits)) same.focus();
    else if (document.activeElement === document.body || !document.activeElement || box.contains(document.activeElement)) $("approval-title").focus();
  }
}

// updateCountdown: the remaining time from the server's clock at the last
// read, advanced by the page's own clock as a hint only.
function updateCountdown() {
  const cd = $("approval-countdown");
  const a = lastApprovals;
  if (!cd || !a || !a.items) return;
  const item = a.items.find((x) => x.purchase.approval && x.purchase.approval.state === "pending");
  if (!item) return;
  const left = item.purchase.approval.expires_at - a.servedAt - Math.floor((Date.now() - a.at) / 1000);
  cd.textContent = left > 0 ? "剩約 " + left + " 秒" : "已達期限，等待後端確認";
  for (const b of document.querySelectorAll("#approval .approval-approve, #approval .approval-deny")) {
    const off = resolving || left <= 0;
    // A focused button about to be disabled hands the focus to the heading
    // (which submits nothing), not to the page (review of f95415d, R1).
    if (off && !b.disabled && document.activeElement === b) $("approval-title").focus();
    b.disabled = off;
  }
}
setInterval(updateCountdown, 1000);

// resolveItem: one approve or deny; the server decides in one transaction.
async function resolveItem(item, decision) {
  if (resolving) return;
  const g = creds.gen;
  const target = A.approvalTarget(g, creds.view().simId, item);
  resolving = true;
  approvalNote = null;
  renderControl();
  const r = await creds.resolve(item.purchase.approval.approval_session_id, item.purchase.intent_hash, decision);
  if (g !== creds.gen) return;
  resolving = false;
  // R2: the operation was another capability's (the customer changed
  // meanwhile): its reply, error or silence is not this panel's. It may
  // have been accepted; the panel reads the state again as the current
  // customer and says nothing of the old item; the focus stays put.
  if (r && (r.superseded || r.simId !== creds.view().simId)) {
    setRunStatus("客人 " + short(r.consumer) + " 的" + (decision === "approve" ? "核准" : "拒絕") + "：模擬身分已更換，結果未確認（可能已被後端記錄）；以目前身分重新讀取", "warn");
    await pollApprovals();
    refreshNow();
    return renderControl();
  }
  const note = (kind, text) => ({ gen: g, target, kind, text });
  const code = r && r.body && r.body.error;
  if (!r) approvalNote = { gen: g, kind: "warn", text: "角色模擬已失效：請重新「以 " + short(item.consumer_id) + " 模擬」，結果未確認" };
  else if (r.error) approvalNote = { gen: g, kind: "warn", text: "沒有收到回覆：結果未確認；畫面會重新讀取（同一請求重送不會多發授權）" };
  else if (r.status === 200) approvalNote = { gen: g, kind: "ok", text: decision === "approve" ? "已記錄：已核准（模擬），未啟用付款" : "已記錄：已拒絕" };
  else if (code === "approval_closed") {
    const rp = r.body.result && r.body.result.purchase;
    const cred = rp && A.CREDENTIAL_CLOSED[rp.closed_reason]; // C2: closed by the recheck at approval, not by the task
    approvalNote = { gen: g, kind: "error", text: cred ? "未核准：" + cred + "（後端判定）"
      : "無法" + (decision === "approve" ? "核准" : "拒絕") + "：這一筆已" + ({ expired: "過期", withdrawn: "撤回", approved: "核准", denied: "拒絕", superseded: "作廢" }[rp && rp.approval && rp.approval.state] || "關閉") + "（後端判定）" };
  }
  else if (code === "approval_superseded") approvalNote = { gen: g, kind: "error", text: "報價已改變：這次核准作廢，需要重新請購" };
  else if (code === "intent_mismatch") approvalNote = { gen: g, kind: "error", text: "摘要與後端不符：未核准（請重新讀取這一筆）" };
  else if (code === "not_found") approvalNote = { gen: g, kind: "error", text: "找不到這一筆（可能不是目前模擬的客人）" };
  else approvalNote = { gen: g, kind: "error", text: "未處理（" + (code || r.status) + "）" };
  if (approvalNote) approvalNote = note(approvalNote.kind, approvalNote.text);
  await pollApprovals();
  refreshNow();
  renderControl();
  if (g === creds.gen && target.sim === creds.view().simId) $("approval-title").focus();
}

// ---- A2: the person's approval with a registered device (docs/commercial-human-approval-design.md 8) ----
//
// The selected customer's viewer token reads and acts; the Signer decides.
// Nothing is prompted automatically: only a click on 「用已登記的裝置核准」
// or 「用裝置拒絕這筆」 asks the device, after the challenge is checked to be
// this click's (AP.sameChallenge). Every reply is bound to the generation
// and the customer it was asked for; a switch drops it.

const a2Info = () => (lastReady && lastReady.status && lastReady.status.human_approval) || null;
const a2Mode = () => !!a2Info() && creds.view().hasHost;
const a2OnOrigin = () => { const i = a2Info(); return !!i && location.origin === i.origin; };

async function a2Call(consumer, path, body) {
  const token = creds.viewerToken(consumer);
  if (!token) return { status: 0, body: { error: "no_viewer" } };
  try { return await call(token, path, AbortSignal.timeout(15000), body === undefined ? undefined : { body }); }
  catch { return { status: 0, body: { error: "unreachable" } }; }
}

// pollA2 reads the selected customer's approvals; a reply of another
// generation or customer is dropped. An attempt whose result is not known
// is asked about again by its own IDs (never a new challenge or prompt).
async function pollA2() {
  if (!a2Mode() || !a2OnOrigin()) { if (lastA2 || a2Waiting) { lastA2 = null; a2Waiting = null; renderControl(); } return; }
  const c = currentConsumer();
  if (!c || !creds.viewerToken(c)) { if (lastA2) { lastA2 = null; renderControl(); } return pollA2Waiting(c); }
  const g = creds.gen;
  const r = await a2Call(c, "/api/approvals");
  if (g !== creds.gen || currentConsumer() !== c) return;
  lastA2 = r.status === 200 ? { gen: g, consumer: c, servedAt: r.body.served_at, at: Date.now(), items: r.body.approvals || [], signer: r.body.signer, sources: r.body.sources,
    pays: A.paysOn(r.body.payment_capability) }
    : { gen: g, consumer: c, error: (r.body && r.body.error) || "status_" + r.status };
  renderControl();
  await pollA2Waiting(c); // after the selected customer's own read (review)
  if (g !== creds.gen || currentConsumer() !== c) return;
  const it = a2Item();
  const at = it && it.attempt;
  if (at && !a2Op && !a2Recovering && (at.state === "unknown" || at.state === "verifying" || at.state === "requested")) {
    a2Recovering = true;
    try {
      const q = await a2Call(c, "/api/approvals/" + encodeURIComponent(at.approval_session_id) + "/attempts/" + encodeURIComponent(at.request_id));
      if (g === creds.gen && currentConsumer() === c && q.status !== 0) {
        const [kind, text] = q.status === 200 && q.body.challenge ? ["idle", "可以繼續驗證：按下按鈕才會要求裝置。"] : AP.outcomeText(q.status, q.body, at.decision, !!(lastA2 && lastA2.pays));
        a2Note = { target: a2Target(it), kind, text };
      }
    } finally { a2Recovering = false; }
    renderControl();
  }
}

// pollA2Waiting (batch 3): the other customers' Asks waiting for the person,
// read with each one's own viewer token. They are only named, with a switch:
// an Ask is acted on from its own customer's panel, as always.
async function pollA2Waiting(c) {
  const g = creds.gen;
  const items = [];
  for (const o of creds.view().consumers.filter((x) => x !== c)) {
    const r = await a2Call(o, "/api/approvals");
    if (g !== creds.gen || currentConsumer() !== c) return;
    const it = r.status === 200 && (r.body.approvals || []).find((x) => x.purchase.approval && x.purchase.approval.state === "pending" && !x.task_ended);
    if (it) items.push({ consumer: o, product: productOf(it.purchase), amount: it.purchase.amount });
  }
  const next = { gen: g, consumer: c, items };
  if (JSON.stringify(next) !== JSON.stringify(a2Waiting)) { a2Waiting = next; renderControl(); }
}

// a2Item: the newest pending item of the selected customer; else the
// newest item of its newest run (its result).
function a2Item() {
  const a = lastA2;
  if (!a || a.gen !== creds.gen || !a.items || a.consumer !== currentConsumer()) return null;
  const pending = a.items.find((x) => x.purchase.approval && x.purchase.approval.state === "pending" && !x.task_ended);
  if (pending) return pending;
  const runs = (lastRuns && lastRuns.gen === creds.gen && lastRuns.runs) || [];
  const run = runs.find((r) => r.consumer_id === a.consumer);
  return (run && a.items.find((x) => x.task_id === run.task_id)) || a.items[0] || null;
}

const a2Target = (item) => A.approvalTarget(creds.gen, "a2", item);

// ENDED: a pending Ask of a task that ended (a CLI timeout, or another
// purchase denied): not decided by anyone now; it expires at its deadline.
const ENDED = ["closed", "task 已結束：這筆不能核准", "這個 task 已結束（CLI 逾時，或另一筆請購被拒絕），這一筆不能再核准，也不會付款；到期後由後端記錄。下一步：" + A.AGAIN + "。"];

const A2_RESULT = {
  // Review N-b, C: a closed session's heading never says "not approved"
  // (an attempt's result may be unknown; design 7, 8); the panel's message
  // says what is known.
  expired: ["closed", "已過期（後端時間為準）。", "沒有付款。下一步：" + A.AGAIN + "。"],
  withdrawn: ["closed", "主持人已取消這個任務；這筆不是你拒絕的。", "不會付款。"],
  superseded: ["closed", "報價已改變：這筆已關閉", "需要重新請購；沒有付款。"],
};

let shownA2 = "";
// V1: the microphone the recording uses ("" is Chrome's default), in memory
// only like the rest of the page; the list follows Chrome's devices.
let micChoice = "";
let micList = [];
async function refreshMics() {
  micList = await V.microphones(); // a choice that went away stays chosen (review 1)
  renderControl();
}
if (navigator.mediaDevices) {
  navigator.mediaDevices.addEventListener("devicechange", refreshMics);
  refreshMics();
}
function renderA2() {
  const box = $("approval");
  box.hidden = false;
  const item = a2Item();
  const target = a2Target(item);
  if (a2Note && !A.sameTarget(a2Note.target, target, false)) a2Note = null;
  const waiting = a2Waiting && a2Waiting.gen === creds.gen && a2Waiting.consumer === currentConsumer() ? a2Waiting.items : [];
  const key = JSON.stringify([currentConsumer(), item, waiting, lastA2 && lastA2.error, a2Note, a2Op && a2Op.phase, a2Op && !!a2Op.stop, a2Op && a2Op.mic, micChoice, micList, a2OnOrigin(), lastA2 && lastA2.signer]); // V1: the recording's stop and microphone, and the microphone list, redraw it
  if (key === shownA2) return updateA2Countdown();
  shownA2 = key;
  const body = $("approval-body");
  const el = (tag, cls, text) => { const e = document.createElement(tag); if (cls) e.className = cls; if (text !== undefined) e.textContent = text; return e; };
  const act = document.activeElement && box.contains(document.activeElement) ? document.activeElement : null;
  const had = act ? { kind: act.dataset.kind || "", target: act.dataset.session ? { ...act.dataset, pending: true } : null } : null;
  const parts = [];
  const info = a2Info();
  if (!a2OnOrigin()) {
    parts.push(el("p", "approval-empty", "本人核准需要從 " + info.origin + "/host 開啟這個頁面（WebAuthn 只接受 localhost），在那裡配對後再操作。"));
  } else if (!currentConsumer()) {
    parts.push(el("p", "approval-empty", "選一位客人（Agent 卡片或商店裡的客人），這裡會顯示他需要處理的請購。"));
  } else if (lastA2 && lastA2.error && !item) {
    parts.push(el("p", "approval-empty", "客人 " + short(currentConsumer()) + " 的核准清單讀取失敗（" + lastA2.error + "），未確認"));
  } else if (!item) {
    parts.push(el("p", "approval-empty", lastA2 ? "客人 " + short(currentConsumer()) + " 目前沒有需要你處理的請購。" : "讀取客人 " + short(currentConsumer()) + " 的待核准…"));
  } else {
    const p = item.purchase, ap = p.approval, d = item.delegation;
    const mode = el("p", "approval-mode");
    const other = AP.recordTag(p, "device");
    mode.append(el("span", "approval-tag", other ? other[0] : "本人核准"), other ? other[1] : AP.sourceText(lastA2 && lastA2.sources) + (lastA2 && lastA2.signer !== "approvals-only" && lastA2.signer !== "ready" ? "・Signer 目前無法連線" : ""));
    parts.push(mode);
    const ended = ap.state === "pending" && !!item.task_ended; // nobody decides it now (cli-timeout-pending)
    const pending = ap.state === "pending" && !ended;
    const res = ended ? ENDED : AP.decidedText(ap.state, p, !!(lastA2 && lastA2.pays)) || closedResult(A2_RESULT, ap.state, p);
    const head = el("div", "approval-head");
    head.dataset.state = ended ? "ended" : ap.state;
    head.append(el("h3", "", pending ? "核准這一筆付款？" : res ? res[1] : ap.state),
      el("p", "", pending ? "客人 " + short(item.consumer_id) + " 的 Agent 想買下面這一項。核准會要求你的裝置驗證；你只核准這一筆。" : res ? res[2] : ""));
    parts.push(head);
    const product = productOf(p);
    const it = el("div", "approval-item");
    const amt = el("p", "approval-amount");
    amt.append(el("b", "", units(p.amount)), el("span", "", " test USDC"));
    const vk = runVenue(item.task_id);
    it.append(el("p", "approval-product", "商品 " + product + "（" + goodsOf(vk) + (lastA2 && lastA2.pays ? "・付款確認後交付" : "・本片不付款") + "）"), amt);
    parts.push(it);
    const dl = el("dl", "approval-rows");
    for (const [k, val] of [...venueRows(vk),
      ["收款人", addr(p.pay_to) + " · 與報價相同（payTo）"], ["付款帳戶", "客人 " + short(item.consumer_id) + " 的測試錢包 " + addr(item.payer) + " · " + item.wallet_id],
      ["網路與資產", network(p.chain_id) + " · test USDC" + (p.asset_id ? "（" + p.asset_id + "）" : "") + "，不是真錢"],
      ["委託範圍", d ? "預算 " + units(d.task_budget) + "；超過 " + units(d.allow_max) + " 一律要你核准" : "未取得"],
      ["intent", (p.intent_hash || "").slice(0, 23) + (other ? "…（後端比對這一份）" : "…（Signer 依完整請購重算比對）")]]) { // review S-A: a simulated record never reached the Signer
      dl.append(el("dt", "", k), el("dd", "", val));
    }
    parts.push(dl);
    if (!other) parts.push(el("p", "approval-note", "瀏覽器的裝置提示只會顯示網站（localhost），不會顯示金額：請以這裡的摘要為準。這個頁面由 demo host 提供，不是獨立的可信顯示。"));
    const at = item.attempt;
    const stored = ended ? null : AP.attemptText(at, ap.state); // ended: no button to use; the heading says why
    if (pending) {
      const dl2 = el("div", "approval-deadline");
      const cd = el("b", "", "");
      cd.id = "approval-countdown";
      dl2.append(cd, el("span", "", "期限 " + clock(ap.expires_at) + "（後端時間為準；畫面倒數只是提示）"));
      parts.push(dl2);
      const acts = el("div", "approval-actions");
      const busy = !!a2Op;
      const open = AP.openAttempt(at);
      const lostOrClosed = at && (at.state === "lost" || at.state === "closed" || at.state === "expired");
      const yes = el("button", "approval-approve", lostOrClosed && at.decision === "approve" ? "重新核准（用已登記的裝置）" : "用已登記的裝置核准");
      const no = el("button", "approval-deny", "用裝置拒絕這筆");
      for (const [b, dec, kind] of [[yes, "approve", "a2-approve"], [no, "deny", "a2-deny"]]) {
        b.type = "button";
        Object.assign(b.dataset, target, { pending: "", kind });
        b.disabled = busy;
        b.addEventListener("click", () => a2Act(item, dec));
        acts.append(b, " ");
      }
      if (open && at) {
        const cancel = el("button", "approval-cancel", "取消這次驗證");
        cancel.type = "button";
        Object.assign(cancel.dataset, target, { pending: "", kind: "a2-cancel" });
        cancel.disabled = busy;
        cancel.addEventListener("click", () => a2Cancel(item, at));
        acts.append(cancel);
      }
      parts.push(acts);
      const micRow = el("label", "approval-mic", "錄音用的麥克風（Signer 要求唸句子時才會錄音）：");
      const sel = el("select");
      for (const [id, name] of AP.micOptions(micList, micChoice)) {
        const o = el("option", "", name);
        o.value = id;
        sel.append(o);
      }
      sel.value = micChoice;
      Object.assign(sel.dataset, target, { pending: "", kind: "a2-mic" });
      sel.disabled = busy;
      sel.addEventListener("change", () => { micChoice = sel.value; });
      micRow.append(sel);
      parts.push(micRow);
    } else if (ap.grant) {
      parts.push(el("p", "approval-grant", AP.grantLine(ap.grant, !!(lastA2 && lastA2.pays))));
    }
    const shown = a2Op && a2Op.session === ap.approval_session_id ? null : AP.panelNote(a2Note, stored, pending);
    if (a2Op && a2Op.session === ap.approval_session_id) {
      if (a2Op.phase === "voice") { // V1: the phrase to read, and the end of the recording
        const box = el("div", "approval-voice");
        // The recording starts only once the microphone is open (stop is set then):
        // until then, words read are lost (V1 device round). The phrase comes
        // first and alone in large type, and the box goes right under the
        // heading: at the panel's end it was out of sight and the label above it
        // was read instead (「…最多10秒…」, V1 device round).
        box.append(el("p", "approval-voice-label", a2Op.stop ? "錄音中，請唸這一句：" : "麥克風準備中，還不要唸；先看這一句："), el("p", "approval-voice-phrase", a2Op.phrase),
          el("p", "approval-voice-note", "只唸上面這一句；唸完按「唸完了」。最多 10 秒，錄音只用來比對，不會保存。"));
        if (a2Op.mic) box.append(el("p", "approval-voice-mic", "麥克風：" + a2Op.mic));
        const stop = el("button", "approval-voice-stop", "唸完了");
        stop.type = "button";
        stop.disabled = !a2Op.stop;
        stop.addEventListener("click", () => a2Op && a2Op.stop && a2Op.stop());
        box.append(stop);
        parts.splice(2, 0, box); // after the mode line and the heading
      }
      const n = el("p", "approval-msg", a2Op.phase === "voice" ? (a2Op.stop ? "錄音中…" : "麥克風準備中…") : a2Op.phase === "device" ? "請在裝置上完成驗證…" : a2Op.phase === "verify" ? "結果確認中，請不要重複操作。" : "準備驗證中…");
      n.dataset.kind = "busy";
      n.setAttribute("role", "status");
      parts.push(n);
    } else if (shown) {
      const n = el("p", "approval-msg", shown.text);
      n.dataset.kind = shown.kind;
      n.setAttribute("role", "status");
      parts.push(n);
    }
    const ev = el("button", "approval-evidence", "查看技術證據 ›");
    ev.type = "button";
    Object.assign(ev.dataset, target, { pending: "", kind: "a2-evidence" });
    ev.addEventListener("click", async () => { if (currentTask() !== item.task_id) openTask(item.task_id); openDrawer(); });
    parts.push(ev);
  }
  if (a2OnOrigin() && waiting.length) {
    // Batch 3: another customer's Ask is named here, with a switch; nothing is decided from this line.
    const others = el("div", "approval-others");
    for (const w of waiting) {
      const row = el("p", "approval-other", "客人 " + short(w.consumer) + (currentConsumer() ? " 也有" : " 有") + "一筆請購等你核准：" + w.product + " " + units(w.amount) + " test USDC ");
      const sw = el("button", "approval-switch", "切換到客人 " + short(w.consumer));
      sw.type = "button";
      sw.dataset.kind = "a2-switch-" + w.consumer;
      sw.addEventListener("click", () => selectConsumer(w.consumer));
      row.append(sw);
      others.append(row);
    }
    parts.unshift(others);
  }
  body.replaceChildren(...parts);
  updateA2Countdown();
  // The focus comes back to the same control of the same item only; else
  // to the heading, which submits nothing (P1-a R1). Nothing is clicked.
  if (had) {
    const same = had.kind && body.querySelector('[data-kind="' + had.kind + '"]');
    const submits = had.kind !== "a2-evidence" && had.kind !== "a2-mic";
    if (same && !same.disabled && A.sameTarget(had.target, target, submits)) same.focus();
    else if (document.activeElement === document.body || !document.activeElement || box.contains(document.activeElement)) $("approval-title").focus();
  }
}

function updateA2Countdown() {
  const cd = $("approval-countdown");
  const a = lastA2;
  const item = a2Item();
  if (!cd || !a || !item || !item.purchase.approval || item.purchase.approval.state !== "pending") return;
  const left = item.purchase.approval.expires_at - a.servedAt - Math.floor((Date.now() - a.at) / 1000);
  cd.textContent = left > 0 ? "剩約 " + left + " 秒" : "已達期限，等待後端確認";
  for (const b of document.querySelectorAll('#approval [data-kind="a2-approve"], #approval [data-kind="a2-deny"]')) {
    const off = !!a2Op || left <= 0;
    if (off && !b.disabled && document.activeElement === b) $("approval-title").focus();
    b.disabled = off;
  }
}
setInterval(() => { if (a2Mode()) updateA2Countdown(); }, 1000);

// a2Act: one approve or deny. The challenge (the Signer's, via the host)
// is checked to be this click's before the device is asked; the assertion
// goes with the authoritative attempt's request ID and a new verify
// request ID. A switch of credentials or customer drops every later step.
async function a2Act(item, decision) {
  if (a2Op) return;
  const g = creds.gen, consumer = item.consumer_id;
  const p = item.purchase, session = p.approval.approval_session_id;
  const target = a2Target(item);
  const stale = () => g !== creds.gen || currentConsumer() !== consumer;
  const done = (kind, text) => { a2Op = null; if (!stale()) a2Note = { target, kind, text }; };
  a2Op = { gen: g, consumer, session, intent: p.intent_hash, decision, phase: "challenge" };
  a2Note = null;
  renderControl();
  try {
    const r = await a2Call(consumer, "/api/approvals/" + encodeURIComponent(session) + "/challenge", { request_id: crypto.randomUUID(), decision });
    if (stale()) return;
    if (r.status === 0) return done("warn", "沒有收到回覆：結果未確認；畫面會重新讀取（不會自動要求裝置）");
    if (r.status !== 200 || !r.body.challenge) {
      const at = r.body && r.body.attempt;
      // Review 5-1: a shared attempt resolved now has an outcome (closed,
      // superseded, ...) that outcomeText words; only without one is it
      // simply already decided.
      if (r.status === 200 && at && at.state === "verified" && !r.body.outcome) return done("ok", "這筆已經有本人決定（以後端為準）。");
      const [kind, text] = AP.outcomeText(r.status, r.body, decision, !!(lastA2 && lastA2.pays));
      return done(kind, text);
    }
    if (!AP.sameChallenge(r.body.challenge, { consumer, session, intent: p.intent_hash, decision })) return done("error", "回傳的資料和你這次的操作不一致，已停止，沒有要求裝置驗證。");
    const requestId = r.body.attempt.request_id; // the authoritative one when shared
    // V1: an approve challenge may ask for its phrase to be read first; the
    // recording goes with the assertion. No microphone: sent as such (degraded).
    const prompt = decision === "approve" && r.body.challenge.display && r.body.challenge.display.voice;
    let voice;
    if (prompt) {
      Object.assign(a2Op, { phase: "voice", phrase: prompt.phrase, stop: null });
      renderControl();
      voice = await V.record(prompt.max_seconds || 10, (stop, mic) => { if (a2Op) { a2Op.stop = stop; a2Op.mic = mic; renderControl(); } }, micChoice);
      refreshMics(); // the names show once the microphone has been allowed
      if (stale()) return;
      if (voice.missing) return done("idle", "選擇的麥克風現在找不到或無法使用，沒有送出；這筆仍在等你決定。請重新選擇麥克風，再按核准。");
      if (voice.notRecorded) return done("idle", "錄音沒有成功（麥克風：" + (voice.mic || "未知") + "），沒有送出；這筆仍在等你決定，請再按核准。");
      if (voice.short) return done("idle", "錄音不到 1 秒，沒有送出；這筆仍在等你決定，請再按核准並唸完整句。");
      if (voice.quiet) return done("idle", "沒有收到聲音（麥克風：" + (voice.mic || "未知") + "），沒有送出；這筆仍在等你決定。請確認 Chrome 用的是正確的麥克風，再按核准。");
    }
    a2Op.phase = "device";
    renderControl();
    let cred;
    try {
      cred = await navigator.credentials.get({ publicKey: AP.requestOptions(r.body.challenge.options) });
    } catch (e) {
      if (stale()) return;
      // NotAllowedError covers a cancel, a timeout and no usable device:
      // the page cannot tell them apart and does not claim which.
      return done("idle", e && e.name === "NotAllowedError" ? "裝置驗證沒有完成（取消、逾時或沒有可用的裝置）；這筆仍在等你決定，期限內可以再按一次核准。" : "瀏覽器拒絕這次驗證（" + (e && e.name) + "）；沒有" + (decision === "approve" ? "核准" : "拒絕") + "。");
    }
    if (stale()) return; // never sent for another customer or generation
    a2Op.phase = "verify";
    renderControl();
    const v = await a2Call(consumer, "/api/approvals/" + encodeURIComponent(session) + "/assertion",
      { request_id: requestId, verify_request_id: crypto.randomUUID(), assertion: AP.assertionJSON(cred), ...(voice ? { voice } : {}) });
    if (stale()) return;
    if (v.status === 0) return done("warn", "結果確認中，請不要重複操作。");
    const [kind, text] = AP.outcomeText(v.status, v.body, decision, !!(lastA2 && lastA2.pays));
    done(kind, text);
  } finally {
    if (a2Op && a2Op.gen === g && a2Op.session === session) a2Op = null;
    if (!stale()) { await pollA2(); refreshNow(); renderControl(); if (g === creds.gen) $("approval-title").focus(); }
  }
}

// a2Cancel: 「取消這次驗證」, by the Signer's close; the session stays as it is
// unless the Signer says the challenge was already used.
async function a2Cancel(item, at) {
  if (a2Op) return;
  const g = creds.gen, consumer = item.consumer_id, session = at.approval_session_id;
  const target = a2Target(item);
  a2Op = { gen: g, consumer, session, phase: "cancel" };
  renderControl();
  try {
    const r = await a2Call(consumer, "/api/approvals/" + encodeURIComponent(session) + "/attempts/" + encodeURIComponent(at.request_id) + "/cancel", {});
    if (g !== creds.gen || currentConsumer() !== consumer) return;
    const [kind, text] = r.status === 0 ? ["warn", "這次驗證的結果還無法確認。"] : AP.outcomeText(r.status, r.body, at.decision, !!(lastA2 && lastA2.pays));
    a2Note = { target, kind, text };
  } finally {
    a2Op = null;
    if (g === creds.gen && currentConsumer() === consumer) { await pollA2(); renderControl(); $("approval-title").focus(); }
  }
}

// startAll sends one start per consumer, one after the other, each with its
// own request ID; each reply is shown as it is, and a refused one is not
// retried. An apa.pay.v1 scenario's customer confirms its delegation first
// (asCustomer); one not confirmed is not started. The panels open the first
// one accepted.
async function startAll(pair) {
  if (starting || simulating) return;
  const g = creds.gen;
  starting = true;
  simulating = true; // the cards' role play waits: a switch midway would supersede a confirmation
  try { await startEach(pair, g); } finally {
    if (g === creds.gen && (starting || simulating)) { starting = false; simulating = false; renderControl(); } // review: never left stuck
  }
}
async function startEach(pair, g) {
  renderControl();
  setRunStatus("一起開始：送出 " + pair.length + " 個獨立的開始請求…", "busy");
  // Every customer's delegation is confirmed first, one after the other; only
  // then are the runs started (Aaron 10/24: 兩位都確認後各自開始). One not
  // confirmed is not started; the others still are, each on its own.
  const delegations = new Map();
  const results = [];
  for (const sc of pair.filter((x) => A.isPay(x.contract))) {
    setRunStatus("一起開始：客人 " + short(sc.consumer_id) + (deviceDelegations() ? "：請用已登記的裝置確認委託…" : " 確認委託中（模擬）…"), "busy");
    const d = await asCustomer(sc, g);
    if (!d) return; // cleared or replaced: nothing acts
    if (d.fail) results.push([sc, d]);
    else delegations.set(sc.id, d.id);
  }
  for (const sc of pair) {
    if (A.isPay(sc.contract) && !delegations.has(sc.id)) continue; // not confirmed: said above, not started
    const r = await creds.startRun(sc.id, delegations.get(sc.id));
    if (g !== creds.gen || !r || r.gen !== creds.gen) return; // cleared or replaced: nothing acts
    results.push([sc, r]);
  }
  starting = false;
  simulating = false;
  renderControl();
  const ok = results.filter(([, r]) => !r.fail && !r.error && (r.status === 202 || r.status === 200));
  setRunStatus("一起開始（各自送出，不是一次成功或一次失敗）：" + results.map(([sc, r]) => short(sc.consumer_id) + " "
    + (r.fail ? "未開始：" + r.fail : r.error ? "沒有收到回覆，未確認" : r.status === 202 ? "已開始 " + r.body.task_id : r.status === 200 ? "已受理（同一請求）" + r.body.task_id
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
        await pollApprovals();
        await pollA2();
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
// The audience window: a second window of this origin, put on the projector.
// An open window is only brought forward, never loaded again (its Unity would start over).
$("show-open").addEventListener("click", () => {
  const w = window.open("", "apa-show");
  if (!w) return;
  if (w.location.href === "about:blank") w.location.href = "/show";
  w.focus();
});
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
