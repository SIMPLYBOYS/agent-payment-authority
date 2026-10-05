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
import { renderInspector, renderTimeline, renderSceneText } from "./render.js";
import { UnityHost } from "./unity.js";

const $ = (id) => document.getElementById(id);
const POLL_MS = 1000;   // a target, not a latency promise
const MAX_REREAD = 3;   // task and events must agree; then show "updating"

let token = null;       // the viewer token, memory only
let abort = null;       // aborts the current generation's requests
let timer = null;
let inFlight = false;
const state = A.newState();
let shownSeq = -1;
let mode = "live";      // live | replay
let replayRun = 0;      // bumped to end the current replay run
let polls = 0;          // completed polls, for the browser test
const expanded = new Set(); // event_ids whose JSON the viewer opened (R3)
const rendered = [];    // waiting for Unity's report of (gen, seq)

const SHOWN = ["gen", "seq", "phase", "task", "verdict", "reason"];
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
    return;
  }
  if (s === "ready") return shopReset(state.taskId ? "syncing" : "ready");
  box.dataset.state = s;
  box.textContent = s === "loading" ? "Unity 場景載入中…" : "Unity 場景未就緒";
});

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
  $("task-form").hidden = true;
  $("replay").disabled = true;
  draw();
  setStatus(message || "請輸入 viewer token", "idle");
}

function draw() {
  renderInspector($("inspector"), A.inspector(state.snapshot));
  renderTimeline($("timeline"), A.timeline(state.snapshot), expanded);
  if (mode === "live") renderSceneText($("scene-text"), A.scene(state.snapshot));
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
    $("viewer").textContent = v.consumer_id + "（demo viewer，不是使用者登入）";
    const pick = $("task-pick");
    pick.replaceChildren();
    for (const id of A.FIXTURE_TASKS[v.consumer_id] || []) {
      const b = document.createElement("button");
      b.type = "button";
      b.textContent = id + "（demo fixture）";
      b.addEventListener("click", () => openTask(id));
      pick.append(b);
    }
    $("task-form").hidden = false;
    setStatus("請選擇或輸入 task", "idle");
    return true; // signed in as v.consumer_id, in this generation
  } catch {
    if (gen === state.gen) forget("無法連線到 demo host");
  }
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
  unity.send(A.clearMessage(gen));
  shopReset("syncing");
  draw();
  setStatus("讀取 " + id + "…", "loading");
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
      setStatus("已同步：" + t.task_id + "，快照序號 " + t.as_of_sequence + "（Store 時間 " + new Date(t.served_at * 1000).toISOString() + "）" + (first ? "，首次載入為保存的快照" : ""), "ok");
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
    unity.send(A.projection(state, !first && fresh.length > 0));
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
  unity.send(A.clearMessage(gen));
  shopReset("syncing");
  setStatus("歷史展示回放（唯讀，不重跑 task、不改事件）", "replay");
  for (let i = 0; i < snap.events.length; i++) {
    if (run !== replayRun) return;
    const sc = A.scene({ task: snap.task, events: snap.events.slice(0, i + 1) });
    const seq = snap.events[i].sequence;
    unity.send({ v: A.BRIDGE_VERSION, gen, clear: false, consumer: snap.task.consumer_id, task_id: snap.task.task_id,
      seq, phase: sc.phase, verdict: sc.verdict, reason: sc.reason || "", animate: true });
    renderSceneText($("scene-text"), { ...sc, label: "回放 #" + seq + "：" + sc.label });
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
document.addEventListener("visibilitychange", () => { if (!document.hidden && mode === "live") poll(false); });
// Leaving the page drops the token and the panels and stops polling;
// nothing comes back on return (a restored page asks again).
window.addEventListener("pagehide", () => forget());

forget();
unity.load();
