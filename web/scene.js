// scene.js: the host page's scene with a slot per customer
// (docs/multi-agent-demo-design.md 5.2-5.5). Each consumer's slot follows the
// task of its newest run (the runs list), read with that consumer's own
// viewer token, and is sent to Unity as version 2. What the Dashboard's
// panels show (a chosen consumer, an older task) never changes the scene:
// that is app.js's. No DOM here; host.js renders.
//
// A slot changes its slot generation when its scene task changes; the scene
// generation changes on new credentials and around a replay. A reply, a
// timer or a Unity report of an older slot or scene acts on nothing, and
// one customer's reads, failures and reports never wait for or touch the
// other's.

import * as A from "./adapter.js";

const POLL_MS = 1000;
const MAX_REREAD = 3;

export function createScene({ unity, token, onChange, fetchFn = (...a) => fetch(...a) }) {
  let scene = 0;
  let active = false;
  let paused = false;
  const slots = new Map(); // consumer -> slot

  const newSlot = (consumer) => ({ consumer, slot: 0, task: null, st: A.newState(), timer: null, abort: null, inFlight: false,
    shownSeq: -1, sent: null, report: null, target: null, motion: null, read: "idle" });

  function stopSlot(s) {
    clearTimeout(s.timer);
    s.timer = null;
    if (s.abort) s.abort.abort();
    s.abort = null;
    s.inFlight = false;
  }

  async function call(s, gen, path) {
    const t = token(s.consumer);
    if (!t) throw Object.assign(new Error("no_token"), { code: "no_token" });
    const res = await fetchFn(path, { headers: { Authorization: "Bearer " + t }, cache: "no-store", credentials: "omit", signal: s.abort.signal });
    if (gen !== s.st.gen) throw Object.assign(new Error("stale"), { code: "stale" });
    if (res.status === 401) throw Object.assign(new Error("unauthenticated"), { code: "unauthenticated" });
    if (res.status === 404) throw Object.assign(new Error("not_found"), { code: "not_found" });
    if (!res.ok) throw Object.assign(new Error("offline"), { code: "offline" });
    return res.json();
  }

  // poll reads one slot's task and events until they agree; only this
  // slot's own generation acts on the reply.
  async function poll(s, first) {
    clearTimeout(s.timer);
    if (!active || paused || !s.task || s.inFlight) return;
    s.inFlight = true;
    const gen = s.st.gen;
    const id = encodeURIComponent(s.task);
    let result = "inconsistent";
    try {
      for (let i = 0; i < MAX_REREAD && result === "inconsistent"; i++) {
        const task = await call(s, gen, "/api/tasks/" + id);
        const events = await call(s, gen, "/api/tasks/" + id + "/events");
        result = A.commit(s.st, gen, task, events);
      }
      if (gen === s.st.gen) s.read = result === "ok" || result === "older" ? "ok" : "updating";
    } catch (e) {
      if (gen !== s.st.gen || e.code === "stale") return; // a newer slot generation owns the slot now
      s.read = e.code || "offline";
    } finally {
      if (gen === s.st.gen) s.inFlight = false;
    }
    if (gen !== s.st.gen || !active || paused) return;
    show(s, first);
    s.timer = setTimeout(() => poll(s, false), POLL_MS);
  }

  function show(s, first) {
    const fresh = A.fresh(s.st, first);
    const snap = s.st.snapshot;
    if (snap && snap.task.as_of_sequence !== s.shownSeq) {
      s.shownSeq = snap.task.as_of_sequence;
      const msg = A.projection2(s.st, scene, s.slot, !first && fresh.length > 0);
      const tr = A.transition(s.target, msg);
      s.target = tr.target;
      if (tr.rule <= 2 || !s.motion) s.motion = { rule: tr.rule, text: tr.text, first: !msg.animate, done: false };
      s.sent = msg;
      unity.send2(msg);
    }
    onChange();
  }

  // follow points a slot at a task (a new slot generation), or keeps it.
  function follow(consumer, task) {
    let s = slots.get(consumer);
    if (!s) slots.set(consumer, (s = newSlot(consumer)));
    if (s.task === task) return;
    stopSlot(s);
    s.slot += 1;
    s.task = task;
    A.reset(s.st, consumer, task);
    s.shownSeq = -1;
    s.sent = s.report = s.target = s.motion = null;
    s.read = "loading";
    s.abort = new AbortController();
    unity.send2(A.slotClear2(scene, consumer, s.slot));
    poll(s, true);
  }

  // restart sends a new scene and shows every slot again from its stored
  // snapshot, without animation (a first load).
  function restart() {
    scene += 1;
    unity.send2(A.sceneClear2(scene));
    for (const s of slots.values()) {
      stopSlot(s);
      const task = s.task;
      s.task = null;
      if (task) follow(s.consumer, task);
    }
    onChange();
  }

  return {
    // start: take the shop for the version 2 scene.
    start() {
      if (active) return;
      active = true;
      paused = false;
      unity.own(true);
      restart();
    },
    // stop: give the shop back to the Dashboard (version 1).
    stop() {
      if (!active) return;
      active = false;
      for (const s of slots.values()) stopSlot(s);
      slots.clear();
      unity.own(false);
      onChange();
    },
    get active() { return active && !paused; },
    get started() { return active; }, // started, paused for a replay or not
    // reset: new credentials: a new scene, no slot kept.
    reset() {
      for (const s of slots.values()) stopSlot(s);
      slots.clear();
      if (active && !paused) {
        scene += 1;
        unity.send2(A.sceneClear2(scene));
      }
      onChange();
    },
    // setRuns: each consumer's scene task is its newest run's task.
    setRuns(runs) {
      if (!active || paused) return;
      for (const [consumer, r] of A.sceneTasks(runs)) follow(consumer, r.task);
    },
    // pause and resume a replay: the Dashboard's version 1 replay owns the
    // shop meanwhile; afterwards a new scene shows every slot again.
    pause() {
      if (!active || paused) return;
      paused = true;
      for (const s of slots.values()) stopSlot(s);
      unity.own(false);
      onChange();
    },
    resume() {
      if (!active || !paused) return;
      paused = false;
      unity.own(true);
      restart();
    },
    // rendered: a version 2 report; it completes only its own customer's
    // projection, and only an exact one (A.sameShown2).
    rendered(r) {
      const s = slots.get(r.consumer);
      if (!s || !active || paused) return;
      s.report = r;
      if (s.motion && A.sameShown2(s.sent, r)) s.motion.done = true;
      onChange();
    },
    // view: what the panels need of one consumer's slot: its scene task, the
    // projection sent and the last report (the evidence panel's 場景對應).
    view(consumer) {
      const s = slots.get(consumer);
      if (!s || !s.task) return null;
      const sent = s.sent && { gen: s.sent.scene, consumer, slot: s.sent.slot, task: s.sent.task_id, seq: s.sent.seq, phase: s.sent.phase,
        po: s.st.snapshot ? A.scene(s.st.snapshot).po : null };
      const report = s.report && { gen: s.report.scene, consumer: s.report.consumer, slot: s.report.slot, task: s.report.task, seq: s.report.seq, phase: s.report.phase,
        verdict: s.report.verdict, reason: s.report.reason };
      return { task: s.task, sent, report, shown: A.sameShown2(s.sent, s.report), motion: s.motion && { rule: s.motion.rule, text: s.motion.text, first: s.motion.first } };
    },
    // slots: every slot's state for the page, by consumer.
    slots() {
      return [...slots.values()].sort((a, b) => a.consumer.localeCompare(b.consumer)).map((s) => {
        const sc = s.st.snapshot ? A.scene(s.st.snapshot) : null;
        return { consumer: s.consumer, task: s.task, slot: s.slot, scene, seq: s.sent ? s.sent.seq : -1, phase: s.sent ? s.sent.phase : "",
          label: sc ? sc.label : "", shown: A.sameShown2(s.sent, s.report), moving: !!(s.motion && s.motion.rule <= 2 && !s.motion.done && !s.motion.first), read: s.read };
      });
    },
  };
}
