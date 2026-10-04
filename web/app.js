// app.js: the Dashboard (docs/demo-ui.md). One viewer at a time: the token
// is typed by hand, kept in this module's memory only, sent only as
// Authorization: Bearer, never stored, logged, put in a URL or given to
// Unity. Every switch of viewer or task starts a new generation: old
// requests are aborted and late replies dropped.

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

const unity = new UnityHost($("unity-canvas"), (s, detail) => {
  const box = $("unity-status");
  box.dataset.state = s;
  box.textContent = {
    loading: "Unity 場景載入中…",
    ready: detail === "limezu" ? "Unity 場景就緒（LimeZu 素材）" : "Unity 場景就緒（placeholder，LimeZu 待接入）",
    failed: "Unity 場景未就緒",
    rendered: "Unity 已呈現 " + detail,
  }[s] || "Unity 場景未就緒";
});

function setStatus(text, kind) {
  const s = $("status");
  s.textContent = text;
  s.dataset.kind = kind;
}

async function call(path, gen) {
  const res = await fetch(path, { headers: { Authorization: "Bearer " + token }, cache: "no-store", signal: abort.signal, credentials: "omit" });
  if (gen !== state.gen) throw new Error("stale");
  if (res.status === 401) throw Object.assign(new Error("unauthenticated"), { code: "unauthenticated" });
  if (res.status === 404) throw Object.assign(new Error("not_found"), { code: "not_found" });
  if (!res.ok) throw Object.assign(new Error("http"), { code: "offline" });
  return res.json();
}

// forget drops the viewer: token, task, panels, the shop's customer.
function forget(message) {
  token = null;
  if (abort) abort.abort();
  abort = new AbortController();
  clearTimeout(timer);
  const gen = A.reset(state);
  shownSeq = -1;
  unity.send(A.clearMessage(gen));
  $("viewer").textContent = "未驗證";
  $("task-pick").replaceChildren();
  $("task-form").hidden = true;
  $("replay").disabled = true;
  draw();
  setStatus(message || "請輸入 viewer token", "idle");
}

function draw() {
  renderInspector($("inspector"), A.inspector(state.snapshot));
  renderTimeline($("timeline"), A.timeline(state.snapshot));
  renderSceneText($("scene-text"), A.scene(state.snapshot));
}

async function signIn(ev) {
  ev.preventDefault();
  const input = $("token");
  const typed = input.value.trim();
  input.value = ""; // read once, then cleared
  forget("驗證中…");
  if (!typed) return setStatus("請輸入 viewer token", "idle");
  token = typed;
  const gen = state.gen;
  try {
    const res = await fetch("/api/viewer", { headers: { Authorization: "Bearer " + token }, cache: "no-store", signal: abort.signal, credentials: "omit" });
    if (gen !== state.gen) return;
    if (res.status === 403) return forget("此 token 不是 viewer token（主持人 token 不用於此頁）");
    if (!res.ok) return forget("token 無效或已過期（每次啟動 demo host 都會換新）");
    const v = await res.json();
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
  } catch {
    if (gen === state.gen) forget("無法連線到 demo host");
  }
}

function openTask(id) {
  if (!token || !/^[A-Za-z0-9._-]{1,64}$/.test(id)) return setStatus("task ID 格式不正確", "error");
  if (abort) abort.abort();
  abort = new AbortController();
  clearTimeout(timer);
  const consumer = state.consumer;
  const gen = A.reset(state, consumer, id);
  shownSeq = -1;
  unity.send(A.clearMessage(gen));
  draw();
  setStatus("讀取 " + id + "…", "loading");
  poll(true);
}

// poll reads task, then events, until they agree (as_of_sequence), at most
// MAX_REREAD times; then keeps the last good snapshot and says "updating".
async function poll(first) {
  clearTimeout(timer);
  if (!token || !state.taskId) return;
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
      if (e.code === "unauthenticated") { inFlight = false; return forget("token 已失效，請重新輸入"); }
      A.fail(state, gen, e.code === "not_found" ? "not_found" : "offline");
    }
  } finally {
    inFlight = false;
  }
  if (gen !== state.gen) return;
  show(first);
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

// replay plays the stored events again, read-only: a new generation for the
// shop, one step per event, nothing fetched or written for it.
async function replay() {
  if (!state.snapshot) return;
  const steps = A.replay(state.snapshot);
  const gen = A.bump(state);
  const snap = state.snapshot;
  unity.send(A.clearMessage(gen));
  setStatus("歷史展示回放（唯讀，不重跑 task、不改事件）", "replay");
  for (let i = 0; i < steps.length; i++) {
    if (gen !== state.gen) return;
    unity.send({ v: A.BRIDGE_VERSION, gen, clear: false, consumer: snap.task.consumer_id, task_id: snap.task.task_id,
      seq: snap.events[i].sequence, phase: steps[i], verdict: "", reason: "", animate: true });
    await new Promise((r) => setTimeout(r, 1500));
  }
  if (gen === state.gen) poll(false); // the polling loop (stopped by the new generation) resumes
}

$("auth-form").addEventListener("submit", signIn);
$("task-form").addEventListener("submit", (ev) => { ev.preventDefault(); const f = $("task-id"); openTask(f.value.trim()); });
$("switch").addEventListener("click", () => forget("已清除，請輸入另一位 consumer 的 viewer token"));
$("replay").addEventListener("click", replay);
document.addEventListener("visibilitychange", () => { if (!document.hidden) poll(false); });
window.addEventListener("pagehide", () => { clearTimeout(timer); if (abort) abort.abort(); token = null; });

forget();
unity.load();
