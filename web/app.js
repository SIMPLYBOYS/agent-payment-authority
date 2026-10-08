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

import * as A from "./adapter.js";
import { renderInspector, renderTimeline, renderSceneText, renderCard, renderEvidence } from "./render.js";
import { UnityHost } from "./unity.js";

const $ = (id) => document.getElementById(id);
const POLL_MS = 1000;   // a target, not a latency promise
const MAX_REREAD = 3;   // task and events must agree; then show "updating"

let token = null;       // the viewer token, memory only
let history = null;     // the host's history mode (a recorded real run), from /api/viewer
let live = null;        // the host's live mode (a real Agent run the operator starts), from /api/viewer
let liveTrace = null;   // live mode: { gen, reply } of GET /api/tasks/{id}/trace for the open task
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

const unity = new UnityHost($("unity-canvas"), (s, detail) => {
  const box = $("unity-status");
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

// shopReset drops what the shop last reported (generation, sequence, task,
// phase, verdict, reason) and says what it waits for now: ready (no task
// chosen) or syncing (the current generation is not shown yet). Loading and
// failed stay as they are.
function shopReset(waiting) {
  const box = $("unity-status");
  for (const k of SHOWN) delete box.dataset[k];
  if (!unity.ready) return;
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

function setStatus(text, kind) {
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

async function call(path, gen) {
  const res = await fetch(path, { headers: { Authorization: "Bearer " + token }, cache: "no-store", signal: abort.signal, credentials: "omit" });
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
  const mo = motion && motion.gen === state.gen ? motion : null;
  const notes = [...sc.notes];
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
    trace: live && liveTrace && liveTrace.gen === state.gen ? liveTrace.reply : null };
  const c = A.card(view, rp ? null : chosenPo, ctx);
  const mo = motion && motion.gen === state.gen ? motion : null;
  const e = snap ? A.evidence(snap, c && c.po, { ...ctx, sent, report: lastReport, motion: mo && { rule: mo.rule, text: mo.first ? FIRST_LOAD + "（不播放過場）" : mo.text } }) : null;
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
  $("current").textContent = c ? "目前：" + c.title.replace(/^客人 /, "") + " · " + (c.scenePo || "尚無請購") + (rp ? "（回放當步）" : "") : "目前：未選擇 task";
}

// applyHistory labels the page for the host's history mode (a recorded real
// run, docs/cli-poc.md 54): the fixed label replaces the fake-agent badge
// and capability line, and a note by the scene says the shop shows a
// replay of that record. Nothing else changes: the data are the Store's.
function applyHistory(h, l) {
  history = h && h.record === "live_history" ? h : null;
  live = !history && l && l.record === "live_demo" ? l : null;
  labelPage(history || live);
  shownCard = shownEvidence = ""; // relabel the card and the evidence panel
}

// labelPage sets the header and the note by the scene for history mode (h)
// or the fake demo (null). /host calls it from its readiness poll too, so
// the label is there before a viewer is chosen.
export function labelPage(h) {
  const history = h && h.record === "live_history" ? h : null;
  const live = h && h.record === "live_demo" ? h : null; // live mode (docs/cli-poc.md 55)
  const badge = document.querySelector("header .badge"), caps = document.querySelector("header .caps");
  if (badge) badge.textContent = history ? history.label : live ? live.label : "M1／fake agent／不付款";
  if (caps) caps.textContent = history
    ? "能力：真實 Agent 執行紀錄（開發展示例外，非即時）· 付款 未實作 · 憑證／ZK 未實作 · B1-07 獨立驗證 未執行"
    : live ? "能力：真實 Agent（" + (live.dev_demo_plugins ? "開發展示例外：plugins 內容與行為未確認，不是嚴格零 plugin 模式" : "嚴格模式") + "）· 付款 未實作 · 憑證／ZK 未實作 · B1-07 獨立驗證 未執行"
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
  try {
    const res = await fetch("/api/viewer", { headers: { Authorization: "Bearer " + token }, cache: "no-store", signal: abort.signal, credentials: "omit" });
    if (gen !== state.gen) return;
    if (res.status === 401 && unauthorized) return unauthorized();
    if (res.status === 403) return forget("此 token 不是 viewer token（主持人 token 不用於此頁）");
    if (!res.ok) return forget("token 無效或已過期（每次啟動 demo host 都會換新）");
    const v = await res.json();
    if (gen !== state.gen) return; // the body came after a switch, a clear or pagehide: shows nothing
    state.consumer = v.consumer_id;
    applyHistory(v.history || null, v.live || null);
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
    if (live && (!liveTrace || liveTrace.gen !== gen || liveTrace.reply.state !== "available")) {
      // live mode: the run's tool calls, once the CLI has ended (the trace
      // is written then); a failed read leaves the last reply, never more.
      try {
        const reply = await call("/api/tasks/" + id + "/trace", gen);
        if (gen === state.gen) { liveTrace = { gen, reply }; renderPanels(); }
      } catch (e) {
        if (e.code === "unauthenticated") throw e;
      }
    }
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
    sent = { gen, task: snap.task.task_id, seq, phase: sc.phase, po: sc.po };
    sendShop(A.projection({ gen, snapshot: step }, true));
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

// The shop's catalog: fixture prices, informational, read once without a
// token; shown with the time of this read, never as the bound quote.
fetch("/catalog", { cache: "no-store", credentials: "omit" }).then((r) => (r.ok ? r.json() : null)).then((c) => {
  catalog = c;
  catalogAt = c ? new Date().toTimeString().slice(0, 8) : "";
  renderPanels();
}).catch(() => {});

forget();
unity.load();
