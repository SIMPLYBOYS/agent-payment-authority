// app.js: the Dashboard (docs/demo-ui.md). One viewer at a time: the token
// is typed by hand, kept in this module's memory only, sent only as
// Authorization: Bearer, never stored, logged, put in a URL or given to
// Unity. Every switch of viewer or task starts a new generation: old
// requests are aborted and late replies dropped.
//
// Two modes. live: polling keeps the shop and the panels on the latest
// consistent snapshot. replay: polling is stopped and the shop replays the
// stored events (read-only); ending or stopping it starts a new generation
// and restores the latest snapshot in full. A new replay, a switch of task
// or viewer, or leaving the page ends a replay run; its late timers and
// Unity reports do nothing.
//
// The data-* attributes on #status and #unity-status are read-only
// observations for the browser test: counts, mode, generation, sequence,
// phase, task, verdict and reason codes; never a token.
//
// The host's page /host (host.js) uses the same module: it hands a viewer
// token from its loaded file to useViewer, clears with forget, and is told
// of a 401 of the current generation through onUnauthorized.
//
// On /host with a Unity build that shows version 2, the shop belongs to the
// scene (scene.js: every consumer's newest run at once; M-B,
// docs/multi-agent-demo-design.md 5.4): this module's panels then follow the
// chosen consumer and task only and send nothing to the shop; a replay hands
// the shop back to this module until it ends.

import * as A from "./adapter.js";
import { renderInspector, renderTimeline, renderSceneText, renderCard, renderEvidence } from "./render.js";
import { UnityHost } from "./unity.js";

const $ = (id) => document.getElementById(id);
const POLL_MS = 1000;   // a target, not a latency promise
const MAX_REREAD = 3;   // task and events must agree; then show "updating"

let token = null;       // the viewer token, memory only
let history = null;     // the host's history mode (a recorded real run), from /api/viewer
let live = null;        // the host's live mode (a real Agent run the operator starts), from /api/viewer
let liveTrace = null;   // live mode: { gen, task, reply, failed } of GET /api/tasks/{id}/trace for the open task
let traceInFlight = 0;  // the generation whose trace read is in flight (0: none)
const TRACE_MS = 5000;  // a trace read waits at most this long, then it is "未確認"
let abort = null;       // aborts the current generation's requests
let timer = null;
let inFlight = false;
const state = A.newState();
let shownSeq = -1;
let mode = "live";      // live | replay
let replayRun = 0;      // bumped to end the current replay run
let polls = 0;          // completed polls, for the browser test
const expanded = new Set(); // event_ids whose JSON the viewer opened (R3)
const LIST_MS = 5000;   // the task list is read again every 5 s (section 14)
let idGen = 0;          // bumped whenever the viewer identity goes: an old list reply is dropped
let listSeq = 0;        // bumped by every list request: only the latest one of this identity acts
let listAt = "";        // local time of this identity's last good list read; "" before one
let listTimer = null;
let listAbort = null;
const rendered = [];    // waiting for Unity's report of (gen, seq)

const SHOWN = ["gen", "seq", "phase", "task", "verdict", "reason"];

// D1 (docs/demo-presentation-design.md 4.4, 4.5, 6.2, 7): the purchase card
// and the evidence panel. chosenPo is a purchase picked by hand (live only;
// null follows the scene); replayPos the replay step { n, m }; sent the last
// projection sent to Unity with the purchase the page derived for it;
// lastReport what Unity last reported (it carries no purchase_order_id).
let chosenPo = null;
let replayPos = null;
let sent = null;
let lastReport = null;
let catalog = null;   // the shop's /catalog (fixture prices), read once, no token
let catalogAt = "";   // the browser's time of that read
let shownCard = "";   // what the card and the evidence panel last showed
let shownEvidence = "";
// D2 (16.4, 16.5): target is what the shop was last given in this
// generation (A.transition's target); motion the last real move (rules 1, 2)
// or first load, in words, with the newest projection sent since (gen, task,
// seq, phase): done once Unity reports exactly that one (A.sameShown).
// replaySc is the replay step's scene.
let target = null;
let motion = null;
let replaySc = null;
const FIRST_LOAD = "首次載入為保存的快照，未觀測進店過程";
const art = () => unity.art === "limezu" ? "LimeZu 素材" : "placeholder，LimeZu 待接入";

// Unity's WebGL input hears keys on the whole page and cancels their default
// action, so Enter, Space and Tab did nothing on the page's own buttons and
// fields (review of 382a4b5, R1: a keyboard user could not open or answer
// a cancel confirmation). A key whose target is a page element other than
// the shop's canvas keeps its default; Unity still hears it, and the page
// itself cancels no key's default.
for (const type of ["keydown", "keypress", "keyup"]) {
  window.addEventListener(type, (e) => {
    if (e.target instanceof Element && e.target.id !== "unity-canvas") e.preventDefault = () => {};
  }, true);
}

const unity = new UnityHost($("unity-canvas"), (s, detail) => {
  const box = $("unity-status");
  if (s === "rendered2") { // the scene's (version 2): one customer's slot
    if (sceneReport) sceneReport(detail);
    return;
  }
  if (s === "select2") { // UX1: a click on a customer; /host decides
    if (scenePick) scenePick(detail);
    return;
  }
  if (s === "rendered") {
    // detail: what the shop says it shows, for the generation last sent
    // (unity.js drops older reports, so a late one cannot bring back a
    // previous task, verdict or reason).
    box.dataset.state = s;
    Object.assign(box.dataset, { gen: String(detail.gen), seq: String(detail.seq), phase: detail.phase, task: detail.task, verdict: detail.verdict, reason: detail.reason });
    box.textContent = "Unity 已呈現 " + detail.task + " #" + detail.seq + " " + detail.phase + (detail.reason ? "（" + detail.reason + "）" : "");
    for (let i = rendered.length - 1; i >= 0; i--) if (rendered[i].gen === detail.gen && rendered[i].seq === detail.seq) rendered.splice(i, 1)[0].done(true);
    lastReport = { gen: detail.gen, seq: detail.seq, phase: detail.phase, task: detail.task };
    if (motion && A.sameShown(motion, detail)) motion.done = true; // only the projection last sent ends "補間中"
    drawScene();
    renderPanels();
    return;
  }
  if (s === "ready") return shopReset(state.taskId ? "syncing" : "ready");
  box.dataset.state = s;
  box.textContent = s === "loading" ? "Unity 場景載入中…" : "Unity 場景未就緒";
});

// viewKey names what the Dashboard shows (identity and task): /host keeps
// it with a control step and drops the step's reply once it changed.
export const viewKey = () => idGen + ":" + (state.taskId || "");

// refreshNow reads the list and the open task again at once (after a
// cancel on /host); the usual polling goes on as before.
export function refreshNow() {
  if (!token) return;
  refreshList();
  if (mode === "live") poll(false);
}

// unityInfo: whether the shop is ready and which art it uses, for /host.
export const unityInfo = () => ({ ready: unity.ready, art: unity.art });

// For /host's scene (scene.js): the shop, the scene's reports, what the
// panels compare with for a consumer (its scene task's projection and
// report), and the replay's hand-over of the shop.
export const shop = () => unity;
let sceneReport = null;
let sceneView = null;
let replayMode = null;
let scenePick = null;
export function onSceneReport(fn) { sceneReport = fn; }
export function onScenePick(fn) { scenePick = fn; }
export function setSceneView(fn) { sceneView = fn; }
// UX3: the host page tells the evidence panel a task's scene ("" none).
let venueOf = null;
export function setVenueOf(fn) { venueOf = fn; }
export function onReplayMode(fn) { replayMode = fn; }
export function refreshPanels() { drawScene(); renderPanels(); }

// shopReset drops what the shop last reported (generation, sequence, task,
// phase, verdict, reason) and says what it waits for now: ready (no task
// chosen) or syncing (the current generation is not shown yet). Loading and
// failed stay as they are.
function shopReset(waiting) {
  const box = $("unity-status");
  for (const k of SHOWN) delete box.dataset[k];
  if (!unity.ready) return;
  if (unity.sceneOwned) { // the scene shows every consumer; its own lines say what each shows
    mirrored = ""; // the next mirror writes again
    box.dataset.state = "scene";
    box.textContent = "Unity 場景：每位客人各自跟隨自己最新的場次（見下方場景狀態）";
    return;
  }
  box.dataset.state = waiting;
  box.textContent = waiting === "ready" ? "Unity 場景就緒（" + art() + "）：等待選擇 task" : "Unity 同步中（等待最新快照）";
}

// renderedBy waits for Unity's report of (gen, seq), at most ms.
function renderedBy(gen, seq, ms) {
  return new Promise((done) => {
    const w = { gen, seq, done };
    rendered.push(w);
    setTimeout(() => { const i = rendered.indexOf(w); if (i >= 0) { rendered.splice(i, 1); done(false); } }, ms);
  });
}

export function setStatus(text, kind) {
  const s = $("status");
  s.textContent = text;
  s.dataset.kind = kind;
  s.dataset.mode = mode;
}

function stopPolling() {
  clearTimeout(timer);
  timer = null;
  if (abort) abort.abort();
  abort = new AbortController();
}

function setMode(m) {
  mode = m;
  if (m !== "replay") replayPos = null;
  $("replay-note").hidden = m !== "replay";
  $("replay-stop").hidden = m !== "replay";
  $("status").dataset.mode = m;
}

async function call(path, gen, signal = abort.signal) {
  const res = await fetch(path, { headers: { Authorization: "Bearer " + token }, cache: "no-store", signal, credentials: "omit" });
  if (gen !== state.gen) throw new Error("stale");
  if (res.status === 401) throw Object.assign(new Error("unauthenticated"), { code: "unauthenticated" });
  if (res.status === 404) throw Object.assign(new Error("not_found"), { code: "not_found" });
  if (!res.ok) throw Object.assign(new Error("http"), { code: "offline" });
  return res.json();
}

let unauthorized = null; // set by /host: a 401 of the current generation clears all its credentials
let beforeTyped = null;   // set by /host: a typed viewer token replaces its loaded file

export function onUnauthorized(fn) { unauthorized = fn; }
export function onTypedSignIn(fn) { beforeTyped = fn; }

// forget drops the viewer: token (also one typed but not yet submitted),
// task, panels, the shop's customer.
export function forget(message) {
  token = null;
  idGen++;
  clearTimeout(listTimer);
  if (listAbort) listAbort.abort();
  $("token").value = "";
  replayRun++;
  if (mode === "replay" && replayMode) replayMode(false); // a replay ended this way gives the shop back too
  setMode("live");
  stopPolling();
  expanded.clear();
  const gen = A.reset(state);
  shownSeq = -1;
  unity.send(A.clearMessage(gen));
  shopReset("ready");
  $("viewer").textContent = "未驗證";
  $("task-pick").replaceChildren();
  delete $("task-pick").dataset.state;
  listAt = "";
  listNote("", "");
  $("task-form").hidden = true;
  $("replay").disabled = true;
  chosenPo = null;
  sent = null;
  lastReport = null;
  liveTrace = null;
  target = motion = null;
  if (document.body.dataset.page === "viewer") $("auth-box").open = true;
  draw();
  setStatus(message || "請輸入 viewer token", "idle");
  viewChanged();
}

function draw() {
  const rp = mode === "replay" ? replayPos : null;
  renderInspector($("inspector"), A.inspector(state.snapshot));
  $("inspector-note").hidden = !rp;
  $("inspector-note").textContent = rp ? "完整紀錄，非回放當步（回放位置 #" + rp.n + "／共 " + rp.m + "）" : "";
  renderTimeline($("timeline"), A.timeline(state.snapshot), expanded, rp);
  drawScene();
  renderPanels();
}

// sendShop sends a projection and keeps the page's account of the move:
// the same transition table as Unity (A.transition), in words.
function sendShop(msg) {
  const tr = A.transition(target && target.gen === msg.gen ? target : null, msg);
  target = { ...tr.target, gen: msg.gen };
  if (tr.rule <= 2 || !motion || motion.gen !== msg.gen) {
    motion = { gen: msg.gen, task: msg.task_id, seq: msg.seq, phase: msg.phase, rule: tr.rule, browse: tr.browse, text: tr.text, first: !msg.animate && mode === "live", done: false };
  } else if (!motion.done) {
    // still on its way: Unity reports only the newest projection, once there
    Object.assign(motion, { task: msg.task_id, seq: msg.seq, phase: msg.phase });
  }
  unity.send(msg);
  drawScene();
}

// drawScene: the scene text (live: the latest snapshot; replay: the step),
// with what the shop is doing now. A display segment is called one.
function drawScene() {
  const sc = mode === "replay" ? replaySc : A.scene(state.snapshot);
  if (!sc) return;
  let mo = motion && motion.gen === state.gen ? motion : null;
  const notes = [...sc.notes];
  if (unity.sceneOwned && mode !== "replay") {
    // The scene's (version 2): what the shop does for the chosen consumer's
    // scene task; an older task chosen is said to be one.
    const sv = sceneView && state.consumer ? sceneView(state.consumer) : null;
    if (!sv || sv.task !== state.taskId) {
      mo = null;
      notes.unshift(sv ? "商店場景呈現的是 " + sv.task + "（你正在查看較早的 task）" : "這位客人尚無場次");
    } else mo = sv.motion ? { rule: sv.motion.rule, text: sv.motion.text, first: sv.motion.first, done: sv.shown } : null;
  }
  if (history && sc.phase === "waiting") notes.push("歷史快照：執行結束時待人工核准；期限早已過，不是即時等待");
  let now = "";
  if (mo && mo.first) { now = "first"; notes.unshift(FIRST_LOAD); }
  else if (mo && mo.rule <= 2 && !mo.done) { now = "moving"; notes.unshift((unity.ready ? "Unity 補間中：" : "Unity 未就緒；過場說明：") + mo.text); }
  else if (mo) now = mo.done ? "shown" : "waiting";
  renderSceneText($("scene-text"), { ...sc, notes }, now);
}

// renderPanels draws the purchase card, the evidence panel and the status
// bar. In a replay the card is the replay step (A.stepView: only the events
// replayed so far), and the evidence panel is the full record, labelled.
function renderPanels() {
  const snap = state.snapshot;
  const rp = mode === "replay" ? replayPos : null;
  const view = snap ? (rp ? A.stepView(snap, rp.n) : snap) : null;
  const ctx = { mode: rp ? "replay" : "live", n: rp && rp.n, m: rp && rp.m, catalog, catalogAt, history, live,
    trace: live && liveTrace && liveTrace.gen === state.gen ? liveTrace.reply : null, traceFailed: !!(live && liveTrace && liveTrace.gen === state.gen && liveTrace.failed) };
  const c = A.card(view, rp ? null : chosenPo, ctx);
  const mo = motion && motion.gen === state.gen ? motion : null;
  let shopCtx = { sent, report: lastReport, motion: mo && { rule: mo.rule, text: mo.first ? FIRST_LOAD + "（不播放過場）" : mo.text } };
  // The scene's (version 2): the chosen consumer's scene task, compared by
  // the six-field rule; an older task chosen is said to be one, not a failure.
  const sv = !rp && unity.sceneOwned && sceneView && state.consumer ? sceneView(state.consumer) : null;
  const older = sv && state.taskId && sv.task !== state.taskId ? sv.task : "";
  if (unity.sceneOwned && !rp) shopCtx = older ? { sent: null, report: null, motion: null, viewOlder: older } : { sent: sv && sv.sent, report: sv && sv.report, motion: sv && sv.motion };
  const e = snap ? A.evidence(snap, c && c.po, { ...ctx, ...shopCtx, venue: venueOf && state.taskId ? venueOf(state.taskId) : "" }) : null;
  if (unity.sceneOwned && !rp) mirrorScene(sv, older);
  const vn = $("view-note");
  if (vn) {
    vn.hidden = !older;
    vn.dataset.scene = older;
    $("view-note-text").textContent = older ? "正在查看較早的 task（" + state.taskId + "）；場景呈現的是 " + older + "（查看與場景不同，不是同步失敗）" : "";
  }
  // Rebuilt only when what they say changed: a poll or a Unity report that
  // changes nothing leaves the DOM (and an open menu or a scroll) alone.
  const cKey = JSON.stringify(c), eKey = JSON.stringify(e);
  if (cKey !== shownCard) {
    shownCard = cKey;
    renderCard($("card"), c, {
      pick: (po) => { chosenPo = c && po === c.scenePo ? null : po; renderPanels(); },
      back: () => { chosenPo = null; renderPanels(); },
    });
  }
  if (eKey !== shownEvidence) {
    shownEvidence = eKey;
    renderEvidence($("evidence"), e);
  }
  const chip = $("mode-chip");
  chip.dataset.mode = rp ? "replay" : "live";
  chip.textContent = rp ? "回放中 #" + rp.n + "／共 " + rp.m + "（唯讀）" : mode === "replay" ? "回放中（唯讀）" : history ? "歷史紀錄（非即時）" : live ? "即時（真實 Agent）" : "即時";
  $("current").textContent = unity.sceneOwned && !rp
    ? (c ? "正在查看：" + c.title.replace(/^客人 /, "") + " 的請購" : "正在查看：未選擇 task")
    : c ? "目前：" + c.title.replace(/^客人 /, "") + " · " + (c.scenePo || "尚無請購") + (rp ? "（回放當步）" : "") : "目前：未選擇 task";
}

// mirrorScene shows on #unity-status what the shop shows for the chosen
// consumer: its scene slot, "rendered" only after Unity's exact report of
// the projection last sent for it (the six-field rule); otherwise syncing,
// no run yet, or that an older task is being viewed. Written only when it
// changed.
let mirrored = "";
function mirrorScene(sv, older) {
  const box = $("unity-status");
  if (!unity.ready) return;
  let st = "scene", text, data = {};
  if (!sv) text = "Unity 場景：這位客人尚無場次（每位客人各自跟隨自己最新的場次）";
  else if (older) text = "Unity 場景呈現 " + older + "（你正在查看較早的 task）";
  else if (sv.shown && sv.report) {
    const r = sv.report;
    st = "rendered";
    data = { gen: String(r.gen), seq: String(r.seq), phase: r.phase, task: r.task, verdict: r.verdict || "", reason: r.reason || "" };
    text = "Unity 已呈現 " + r.task + " #" + r.seq + " " + r.phase + (r.reason ? "（" + r.reason + "）" : "");
  } else { st = "syncing"; text = "Unity 同步中（等待最新快照）"; }
  const key = st + "|" + text + "|" + JSON.stringify(data);
  if (key === mirrored) return;
  mirrored = key;
  for (const k of SHOWN) delete box.dataset[k];
  Object.assign(box.dataset, data);
  box.dataset.state = st;
  box.textContent = text;
}

// applyHistory labels the page for the host's history mode (a recorded real
// run, docs/cli-poc.md 54): the fixed label replaces the fake-agent badge
// and capability line, and a note by the scene says the shop shows a
// replay of that record. Nothing else changes: the data are the Store's.
function applyHistory(seq, h, l) {
  history = h && h.record === "live_history" ? h : null;
  live = !history && l && l.record === "live_demo" ? l : null;
  confirmMode(seq, history || live);
  shownCard = shownEvidence = ""; // relabel the card and the evidence panel
}

// The host's mode (docs/cli-poc.md 56.8, review R2) is unknown until a
// successful answer says fake, live or history: GET /api/mode, /api/viewer
// or /api/host/status. Every such request takes a number when it starts;
// an answer counts only if it started after the one shown, so a late or
// older answer never relabels the page, and a failure never does once the
// mode is known. Before that the header says 「模式未確認」 (a failure says
// so too), never the fake label. Nothing here starts a run.
let modeSeq = 0;     // the last mode request started
let modeShown = 0;   // the request whose answer the header shows (0: none)
export function nextModeSeq() { return ++modeSeq; }
export function confirmMode(seq, info) {
  if (seq <= modeShown) return;
  modeShown = seq;
  labelPage(info);
}
function modeUnconfirmed(seq) {
  if (modeShown > 0 || seq < modeSeq) return; // known, or a newer request is out
  labelPage({ record: "unknown", failed: true });
}

// labelPage sets the header and the note by the scene for history mode (h)
// or the fake demo (null). /host calls it from its readiness poll too, so
// the label is there before a viewer is chosen.
export function labelPage(h) {
  const history = h && h.record === "live_history" ? h : null;
  const live = h && h.record === "live_demo" ? h : null; // live mode (docs/cli-poc.md 55)
  const unknown = !!h && h.record === "unknown"; // not confirmed (docs/cli-poc.md 56.8)
  const p1a = !!h && h.record === "fake_p1a"; // the fake demo with an apa.pay.v1 catalog (docs/commercial-p1a.md)
  const a2 = !!h && h.record === "fake_a2"; // the same with the person's approval by device (docs/commercial-a2.md)
  const badge = document.querySelector("header .badge"), caps = document.querySelector("header .caps");
  // C2: a fake catalog with apa.pay.v2 runs the BBS+ credential step (old BBS+, test issuer in the host process).
  const cred = (s) => (!!h && h.credential ? s.replace("憑證／ZK 未實作", "憑證 BBS+ 舊版（非 W3C bbs-2023；測試 issuer，與 host 同一程序）· ZK 未實作") : s);
  if (badge) {
    badge.textContent = unknown ? (h.failed ? "模式未確認：讀取失敗（重試中）" : "模式未確認（讀取中）")
      : history ? history.label : live ? live.label : a2 ? (h.credential ? "C2＋A2／fake agent／BBS+ 憑證／本人核准（裝置）／不付款" : "A2／fake agent／本人核准（裝置）／不付款")
      : p1a ? (h.credential ? "C2／fake agent／BBS+ 憑證／模擬核准／不付款" : "P1-a／fake agent／模擬核准／不付款") : "M1／fake agent／不付款";
    badge.dataset.mode = unknown ? (h.failed ? "failed" : "unknown") : history ? "history" : live ? "live" : "fake";
  }
  if (caps) caps.textContent = unknown ? "能力：模式未確認（不代表 fake 或真實 Agent）· 付款 未實作 · 憑證／ZK 未實作 · B1-07 獨立驗證 未執行"
    : history ? "能力：真實 Agent 執行紀錄（開發展示例外，非即時）· 付款 未實作 · 憑證／ZK 未實作 · B1-07 獨立驗證 未執行"
    : live ? "能力：真實 Agent（" + (live.dev_demo_plugins ? "開發展示例外：plugins 內容與行為未確認，不是嚴格零 plugin 模式" : "嚴格模式") + "）· 付款 未實作 · 憑證／ZK 未實作 · B1-07 獨立驗證 未執行"
    : a2 ? cred("能力：真實 Agent 未啟用 · 核准 本人以已登記的裝置（Signer 驗證；委託確認仍是模擬） · 付款 未啟用 · 憑證／ZK 未實作 · B1-07 獨立驗證 未執行")
    : p1a ? cred("能力：真實 Agent 未啟用 · 核准 僅 fake 模擬（不是本人驗證） · 付款 未啟用 · 憑證／ZK 未實作 · B1-07 獨立驗證 未執行")
    : "能力：真實 Agent 未啟用 · 付款 未實作 · 憑證／ZK 未實作 · B1-07 獨立驗證 未執行";
  const note = $("history-note");
  if (note) note.hidden = !history;
  const ln = $("live-note");
  if (ln) ln.hidden = !live;
}

async function signIn(ev) {
  ev.preventDefault();
  const input = $("token");
  const typed = input.value.trim();
  input.value = ""; // read once, then cleared; only then may /host clear its file (forget empties the field)
  if (beforeTyped) beforeTyped();
  return useViewer(typed);
}

// useViewer signs in as the viewer of token, typed or handed over by /host;
// true when it did (and nothing newer replaced it meanwhile).
export async function useViewer(typed) {
  forget("驗證中…");
  if (!typed) return setStatus("請輸入 viewer token", "idle");
  token = typed;
  const gen = state.gen;
  const mseq = nextModeSeq();
  try {
    const res = await fetch("/api/viewer", { headers: { Authorization: "Bearer " + token }, cache: "no-store", signal: abort.signal, credentials: "omit" });
    if (gen !== state.gen) return;
    if (res.status === 401 && unauthorized) return unauthorized();
    if (res.status === 403) return forget("此 token 不是 viewer token（主持人 token 不用於此頁）");
    if (!res.ok) return forget("token 無效或已過期（每次啟動 demo host 都會換新）");
    const v = await res.json();
    if (gen !== state.gen) return; // the body came after a switch, a clear or pagehide: shows nothing
    state.consumer = v.consumer_id;
    applyHistory(mseq, v.history || null, v.live || null);
    $("task-pick").dataset.state = "loading";
    listNote("清單讀取中…", "");
    // The viewer is verified: signed in. The list has its own state; its
    // first read is tried before the sign-in is reported, and a failure
    // leaves manual task IDs open while the timer tries again.
    const listed = await refreshList();
    if (gen !== state.gen) return;
    $("viewer").textContent = v.consumer_id + "（demo viewer，不是使用者登入）";
    $("task-form").hidden = false;
    setStatus(listed ? "請選擇或輸入 task" : "清單尚未取得，可手動輸入 task ID", listed ? "idle" : "warn");
    return true; // signed in as v.consumer_id, in this generation
  } catch {
    if (gen === state.gen) forget("無法連線到 demo host");
  }
}

// refreshList reads GET /api/tasks for this viewer (its stored tasks,
// newest first) and shows them; true when it did. Only the current request
// acts: the identity is the same (idGen) and no newer list request started
// (listSeq). A superseded or aborted request updates nothing, handles no
// error or 401 and schedules no timer, so there is one list timer at most.
// A choice in the list is not an authorization: the server decides by the
// token and the task's owner.
async function refreshList() {
  clearTimeout(listTimer);
  if (!token) return false;
  const g = idGen;
  const seq = ++listSeq;
  const current = () => g === idGen && seq === listSeq;
  if (listAbort) listAbort.abort();
  listAbort = new AbortController();
  let ok = false;
  try {
    if (!document.hidden) {
      const res = await fetch("/api/tasks", { headers: { Authorization: "Bearer " + token }, cache: "no-store", signal: listAbort.signal, credentials: "omit" });
      if (!current()) return false;
      if (res.status === 401) { unauthorized ? unauthorized() : forget("token 已失效，請重新輸入"); return false; }
      if (res.ok) {
        const body = await res.json();
        if (!current()) return false;
        renderList(body.tasks || []);
        ok = true;
      } else {
        listFailed();
      }
    }
  } catch {
    if (!current()) return false;
    listFailed();
  }
  if (current() && token) listTimer = setTimeout(refreshList, LIST_MS);
  return ok;
}

// The list's own visible note (#list-note), apart from #status, the
// Inspector and replay: it never touches them.
function listNote(text, kind) {
  const n = $("list-note");
  n.textContent = text;
  if (kind) n.dataset.kind = kind; else delete n.dataset.kind;
  setAlert("list", kind === "warn" ? text : "");
}

// setAlert keeps a short warning in the status bar, which never folds:
// what a folded section would otherwise hide (section 4.1). "" removes it.
const alerts = new Map();
export function setAlert(key, text, kind = "warn") {
  if (text) alerts.set(key, [text, kind]); else alerts.delete(key);
  $("alerts").replaceChildren(...[...alerts.values()].map(([t, k]) => {
    const n = document.createElement("span");
    n.textContent = t;
    n.dataset.kind = k;
    return n;
  }));
}

// currentTask is the Dashboard's task, for /host's row of controls; a view
// change is announced with the apa:view event.
export const currentTask = () => state.taskId;
export const currentConsumer = () => state.consumer;
const viewChanged = () => document.dispatchEvent(new Event("apa:view"));

// listFailed marks the list as not loaded; the next refresh tries again.
// After a good read the shown list stays, marked with that read's time;
// before one there is no list, and task IDs can still be typed.
function listFailed() {
  const pick = $("task-pick");
  pick.dataset.state = "error";
  if (listAt) return listNote("更新失敗，以下為上次成功資料（" + listAt + "）", "warn");
  pick.replaceChildren();
  listNote("清單尚未取得，可手動輸入 task ID", "warn");
}

function renderList(tasks) {
  const pick = $("task-pick");
  pick.dataset.state = "ok";
  listAt = new Date().toTimeString().slice(0, 8); // local HH:MM:SS
  listNote("清單更新於 " + listAt, "");
  if (tasks.length === 0) {
    const none = document.createElement("span");
    none.className = "empty";
    none.textContent = "尚無 task";
    return pick.replaceChildren(none);
  }
  pick.replaceChildren(...tasks.map((t) => {
    const b = document.createElement("button");
    b.type = "button";
    b.dataset.task = t.task_id;
    b.dataset.status = t.status;
    b.textContent = t.task_id + "（" + (A.STATUS_LABELS[t.status] || t.status) + "）";
    b.addEventListener("click", () => openTask(t.task_id));
    return b;
  }));
}

// openTask opens a task for the signed-in viewer (also used by /host after
// a run it started: section 13.1).
export function openTask(id) {
  if (!token || !/^[A-Za-z0-9._-]{1,64}$/.test(id)) return setStatus("task ID 格式不正確", "error");
  replayRun++;
  if (mode === "replay" && replayMode) replayMode(false);
  setMode("live");
  stopPolling();
  expanded.clear();
  const consumer = state.consumer;
  const gen = A.reset(state, consumer, id);
  shownSeq = -1;
  liveTrace = null;
  chosenPo = null;
  sent = null;
  lastReport = null;
  target = motion = null;
  unity.send(A.clearMessage(gen));
  shopReset("syncing");
  draw();
  setStatus("讀取 " + id + "…", "loading");
  viewChanged();
  poll(true);
}

// refreshTrace reads the open task's run trace in live mode (the buyer
// host writes it when the CLI ends) beside the main poll, never in its
// way: one read per generation at a time, at most TRACE_MS, and only a
// reply of the same generation and task acts. A failed or timed-out read
// says so; the last reply stays, shown as the last one, never as now.
async function refreshTrace(gen, taskId) {
  const known = liveTrace && liveTrace.gen === gen && liveTrace.task === taskId ? liveTrace : null;
  if (traceInFlight === gen || (known && !known.failed && known.reply && known.reply.state === "available")) return;
  traceInFlight = gen;
  let reply = null;
  let failed = false;
  try {
    reply = await call("/api/tasks/" + encodeURIComponent(taskId) + "/trace", gen, AbortSignal.any([abort.signal, AbortSignal.timeout(TRACE_MS)]));
  } catch (e) {
    failed = true;
  }
  if (traceInFlight === gen) traceInFlight = 0;
  if (gen !== state.gen || state.taskId !== taskId) return; // switched, cleared or left: nothing acts
  const last = liveTrace && liveTrace.gen === gen && liveTrace.task === taskId ? liveTrace.reply : null;
  liveTrace = failed ? { gen, task: taskId, reply: last, failed: true } : { gen, task: taskId, reply, failed: false };
  renderPanels();
}

// poll reads task, then events, until they agree (as_of_sequence), at most
// MAX_REREAD times; then keeps the last good snapshot and says "updating".
async function poll(first) {
  clearTimeout(timer);
  if (!token || !state.taskId || mode !== "live") return; // a replay owns the shop; it restores when it ends
  if (document.hidden || inFlight) { timer = setTimeout(() => poll(false), POLL_MS); return; }
  inFlight = true;
  const gen = state.gen;
  const id = encodeURIComponent(state.taskId);
  let result = "inconsistent";
  try {
    for (let i = 0; i < MAX_REREAD && result === "inconsistent"; i++) {
      const task = await call("/api/tasks/" + id, gen);
      const events = await call("/api/tasks/" + id + "/events", gen);
      result = A.commit(state, gen, task, events);
    }
    if (result === "inconsistent") A.fail(state, gen, "updating");
    if (live) refreshTrace(gen, state.taskId); // beside the main poll, never awaited by it
  } catch (e) {
    if (gen === state.gen) {
      if (e.code === "unauthenticated") { inFlight = false; return unauthorized ? unauthorized() : forget("token 已失效，請重新輸入"); }
      A.fail(state, gen, e.code === "not_found" ? "not_found" : "offline");
    }
  } finally {
    inFlight = false;
  }
  if (gen !== state.gen || mode !== "live") return;
  polls++;
  $("status").dataset.polls = String(polls);
  show(first);
  clearTimeout(timer); // one loop only
  timer = setTimeout(() => poll(false), POLL_MS);
}

function show(first) {
  draw();
  const t = state.snapshot && state.snapshot.task;
  switch (state.status) {
    case "ok":
      setStatus((history ? "歷史紀錄：" : "已同步：") + t.task_id + "，快照序號 " + t.as_of_sequence
        + (history ? "（紀錄時間 " + new Date(t.served_at * 1000).toISOString() + "，凍結於最後事件，不是現在）" : "（Store 時間 " + new Date(t.served_at * 1000).toISOString() + "）")
        + (first ? "，首次載入為保存的快照" : ""), "ok");
      break;
    case "updating":
      setStatus("更新中：task 與事件序號不一致，保留最後一致快照", "warn");
      break;
    case "not_found":
      setStatus("找不到這個 task，或你沒有查看權限", "error");
      shopReset("ready");
      break;
    default:
      setStatus("離線／更新失敗；最後快照時間 " + (state.lastGoodServedAt ? new Date(state.lastGoodServedAt * 1000).toISOString() : "無"), "error");
  }
  $("replay").disabled = !state.snapshot;
  const fresh = A.fresh(state, first);
  if (state.snapshot && t.as_of_sequence !== shownSeq) {
    shownSeq = t.as_of_sequence;
    const msg = A.projection(state, !first && fresh.length > 0);
    sent = { gen: msg.gen, task: msg.task_id, seq: msg.seq, phase: msg.phase, po: A.scene(state.snapshot).po };
    sendShop(msg);
    renderPanels();
  }
}

// replay plays the stored events again, read-only: polling stops, the shop
// gets a new generation and one step per stored event, each with that step's
// verdict and reason; the panels keep the latest snapshot and say so. Each
// step waits for Unity's report of it (or a short limit when Unity is not
// ready). Nothing is fetched, submitted or written for it.
async function replay() {
  if (!state.snapshot || !token) return;
  const run = ++replayRun;
  if (replayMode) replayMode(true); // the scene hands the shop over for the replay
  setMode("replay");
  stopPolling();
  inFlight = false;
  const snap = state.snapshot;
  const gen = A.bump(state);
  chosenPo = null; // a replay follows its own step (section 4.5)
  target = motion = null;
  unity.send(A.clearMessage(gen));
  shopReset("syncing");
  setStatus("歷史展示回放（唯讀，不重跑 task、不改事件）", "replay");
  const last = snap.events.length ? snap.events[snap.events.length - 1].sequence : 0;
  for (let i = 0; i < snap.events.length; i++) {
    if (run !== replayRun) return;
    const seq = snap.events[i].sequence;
    // The step view has only the events replayed so far and no served_at:
    // nothing of a later verdict, closing or the present time reaches it.
    const step = A.stepView(snap, seq);
    const sc = A.scene(step);
    replayPos = { n: seq, m: last };
    replaySc = { ...sc, label: "回放 #" + seq + "：" + sc.label };
    const msg = A.projection({ gen, snapshot: step }, true);
    sent = { gen, task: snap.task.task_id, seq, phase: msg.phase, po: sc.po }; // Unity's phase, as it reports it
    sendShop(msg);
    draw();
    await renderedBy(gen, seq, 6000);
    await new Promise((r) => setTimeout(r, 400));
  }
  if (run === replayRun) restore(run);
}

// restore ends a replay: a new generation for the shop and a fresh read of
// the latest consistent snapshot, shown without animation. A stopped or
// superseded run does not restore.
function restore(run) {
  if (run !== replayRun || mode !== "replay") return;
  replayRun++;
  setMode("live");
  if (replayMode) replayMode(false); // the scene takes the shop back: a new scene, every slot again
  A.bump(state);
  shownSeq = -1;
  chosenPo = null;
  sent = null;
  target = motion = replaySc = null;
  draw();
  unity.send(A.clearMessage(state.gen));
  shopReset("syncing");
  setStatus("回放結束，讀取最新快照…", "loading");
  poll(true);
}

$("auth-form").addEventListener("submit", signIn);
$("task-form").addEventListener("submit", (ev) => { ev.preventDefault(); const f = $("task-id"); openTask(f.value.trim()); });
$("switch").addEventListener("click", () => forget("已清除，請輸入另一位 consumer 的 viewer token"));
$("replay").addEventListener("click", replay);
$("replay-stop").addEventListener("click", () => restore(replayRun));
document.addEventListener("visibilitychange", () => { if (!document.hidden && mode === "live") poll(false); if (!document.hidden && token) refreshList(); });
// Leaving the page drops the token and the panels and stops polling;
// nothing comes back on return (a restored page asks again).
window.addEventListener("pagehide", () => forget());

// The host's mode and label (GET /api/mode, no token, docs/cli-poc.md 56):
// the header says live or history before any credentials, never the fake
// default on a live host. A failed read leaves the default; the viewer's own
// /api/viewer answer labels the page again when it signs in.
// A failed, malformed or timed-out read says so and is tried again every
// 3 s until some answer has confirmed the mode.
async function queryMode() {
  if (modeShown > 0) return;
  const seq = nextModeSeq();
  try {
    const r = await fetch("/api/mode", { cache: "no-store", credentials: "omit", signal: AbortSignal.timeout(5000) });
    const m = r.ok ? await r.json() : null;
    if (m && m.mode === "fake") return confirmMode(seq, null);
    if (m && ((m.mode === "live" && m.record === "live_demo") || (m.mode === "history" && m.record === "live_history")) && typeof m.label === "string") return confirmMode(seq, m);
  } catch { /* below */ }
  modeUnconfirmed(seq);
  setTimeout(queryMode, 3000);
}
queryMode();

// The shop's catalog: fixture prices, informational, read once without a
// token; shown with the time of this read, never as the bound quote.
fetch("/catalog", { cache: "no-store", credentials: "omit" }).then((r) => (r.ok ? r.json() : null)).then((c) => {
  catalog = c;
  catalogAt = c ? new Date().toTimeString().slice(0, 8) : "";
  renderPanels();
}).catch(() => {});

forget();
unity.load();
