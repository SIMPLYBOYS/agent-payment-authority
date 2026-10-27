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
// other's. reset() and stop() drop every slot: a dropped slot is never
// current again (current()), so a reply that had already arrived when it
// was dropped changes no state, sends nothing, notifies nothing and
// schedules nothing, whatever scene came after.

import * as A from "./adapter.js";

const POLL_MS = 1000;
const MAX_REREAD = 3;
export const PACE_MS = 3000;

// Pacing (簡報模式, Aaron 10/27: "畫面依序播放"): when paced() says so, a slot
// shows each step it reads one after another, at least paceMs apart, instead
// of jumping to the newest: a step is the snapshot after an event that
// changes the scene's phase (the same task, the events up to it). Display
// only: the reads, the Store and every decision go on at their own pace;
// latest() and behind() say how far the real task is when the display trails.
export function createScene({ unity, token, onChange, fetchFn = (...a) => fetch(...a), paced = () => false, paceMs = PACE_MS }) {
  let scene = 0;
  let active = false;
  let paused = false;
  let selected = null; // UX1: the consumer the page has chosen (the ring), or null
  // UX3: the scene's venue (whose runs it shows; "" all) and room (the
  // layout sent to Unity; "" the build's default), and the last runs list.
  let venue = "";
  let layout = "";
  let lastRuns = null;
  const slots = new Map(); // consumer -> slot

  const newSlot = (consumer) => ({ consumer, slot: 0, task: null, st: A.newState(), timer: null, abort: null, inFlight: false,
    shownSeq: -1, sent: null, report: null, target: null, motion: null, read: "idle", shown: null, queue: [], queuedSeq: -1, pace: null });

  // current: the slot is still its consumer's slot and gen still its
  // generation; only then may a read's work go on.
  const current = (s, gen) => slots.get(s.consumer) === s && gen === s.st.gen;

  function stopSlot(s) {
    clearTimeout(s.timer);
    s.timer = null;
    clearTimeout(s.pace);
    s.pace = null;
    s.queue = [];
    if (s.abort) s.abort.abort();
    s.abort = null;
    s.inFlight = false;
  }

  async function call(s, gen, path) {
    if (!current(s, gen)) throw Object.assign(new Error("stale"), { code: "stale" });
    const t = token(s.consumer);
    if (!t) throw Object.assign(new Error("no_token"), { code: "no_token" });
    const res = await fetchFn(path, { headers: { Authorization: "Bearer " + t }, cache: "no-store", credentials: "omit", signal: s.abort.signal });
    if (!current(s, gen)) throw Object.assign(new Error("stale"), { code: "stale" });
    if (res.status === 401) throw Object.assign(new Error("unauthenticated"), { code: "unauthenticated" });
    if (res.status === 404) throw Object.assign(new Error("not_found"), { code: "not_found" });
    if (!res.ok) throw Object.assign(new Error("offline"), { code: "offline" });
    return res.json();
  }

  // poll reads one slot's task and events until they agree; only the
  // current slot of its own generation acts on the reply.
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
        if (!current(s, gen)) return; // the body came after a drop or a newer generation
        result = A.commit(s.st, gen, task, events);
      }
      if (current(s, gen)) s.read = result === "ok" || result === "older" ? "ok" : "updating";
    } catch (e) {
      if (!current(s, gen) || e.code === "stale") return; // dropped, or a newer slot generation owns the slot now
      s.read = e.code || "offline";
    } finally {
      if (current(s, gen)) s.inFlight = false;
    }
    if (!current(s, gen) || !active || paused) return;
    show(s, first);
    s.timer = setTimeout(() => poll(s, false), POLL_MS);
  }

  function show(s, first) {
    const fresh = A.fresh(s.st, first);
    const snap = s.st.snapshot;
    // Pacing turned off (簡報模式 left) with steps still to show: the newest at once (review).
    if (!paced() && snap && (s.queue.length || s.pace)) {
      clearTimeout(s.pace);
      s.pace = null;
      s.queue = [];
      if (snap.task.as_of_sequence !== s.shownSeq) display(s, snap, true);
    }
    if (snap && snap.task.as_of_sequence !== s.queuedSeq) {
      s.queuedSeq = snap.task.as_of_sequence;
      if ((first && !s.news) || !paced()) {
        // At once: a first load (not news), or no pacing; anything still queued is dropped for the newest.
        clearTimeout(s.pace);
        s.pace = null;
        s.queue = [];
        display(s, snap, !first && fresh.length > 0);
      } else {
        const parts = steps(s, snap);
        const last = s.queue.length ? s.queue[s.queue.length - 1] : s.shown;
        if (last && parts.length === 1 && A.scene(parts[0]).phase === A.scene(last).phase) {
          // No new step (payment events within one phase, a binding): the newer read stands in for the last, no extra hold (review).
          if (s.queue.length) s.queue[s.queue.length - 1] = parts[0];
          else display(s, parts[0], true);
        } else {
          s.queue.push(...parts);
          if (!s.pace) advance(s);
        }
      }
    }
    onChange();
  }

  // steps: the snapshots between the last one queued and snap, one per change of the scene's phase, ending with snap itself.
  function steps(s, snap) {
    const before = s.queue.length ? s.queue[s.queue.length - 1] : s.shown;
    let phase = A.scene(before).phase;
    const out = [];
    for (const e of snap.events) {
      if (before && e.sequence <= before.task.as_of_sequence) continue;
      const part = { task: { ...snap.task, as_of_sequence: e.sequence }, events: snap.events.filter((x) => x.sequence <= e.sequence) };
      const ph = A.scene(part).phase;
      if (ph !== phase) { out.push(part); phase = ph; }
    }
    if (out.length && A.scene(out[out.length - 1]).phase === A.scene(snap).phase) out[out.length - 1] = snap; // the newest read stands for its own phase
    else out.push(snap);
    return out;
  }

  // advance: the next queued step now, and the one after no sooner than paceMs.
  function advance(s) {
    s.pace = null;
    if (!slots.has(s.consumer) || slots.get(s.consumer) !== s || !s.queue.length) return;
    display(s, s.queue.shift(), true);
    s.pace = setTimeout(() => { advance(s); onChange(); }, paceMs); // the last step is held too: a newer read waits its turn
  }

  function display(s, snap, animate) {
    s.shown = snap;
    s.shownSeq = snap.task.as_of_sequence;
    const msg = A.projection2({ ...s.st, snapshot: snap }, scene, s.slot, animate, layout);
    const tr = A.transition(s.target, msg);
    s.target = tr.target;
    if (tr.rule <= 2 || !s.motion) s.motion = { rule: tr.rule, text: tr.text, first: !msg.animate, done: false };
    s.sent = msg;
    unity.send2(msg);
  }

  // follow points a slot at a task (a new slot generation), or keeps it.
  function follow(consumer, task, news = false) {
    let s = slots.get(consumer);
    if (!s) slots.set(consumer, (s = newSlot(consumer)));
    if (s.task === task) return;
    stopSlot(s);
    s.slot += 1;
    s.task = task;
    A.reset(s.st, consumer, task);
    s.shownSeq = s.queuedSeq = -1;
    s.shown = null;
    s.news = news;
    s.sent = s.report = s.target = s.motion = null;
    s.read = "loading";
    s.abort = new AbortController();
    unity.send2(A.slotClear2(scene, consumer, s.slot, layout));
    poll(s, true);
  }

  // restart sends a new scene and shows every slot again from its stored
  // snapshot, without animation (a first load).
  function restart() {
    scene += 1;
    unity.send2(A.sceneClear2(scene, layout));
    for (const s of slots.values()) {
      stopSlot(s);
      const task = s.task;
      s.task = null;
      if (task) follow(s.consumer, task);
    }
    sendSelect();
    onChange();
  }

  // sendSelect: the choice, for the current scene (a new scene starts
  // without a ring in Unity).
  function sendSelect() {
    if (active && !paused && unity.select2) unity.select2({ scene, consumer: selected || "" });
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
      selected = null;
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
      lastRuns = null; // the old identity's runs are never followed again
      if (active && !paused) {
        scene += 1;
        unity.send2(A.sceneClear2(scene, layout));
        sendSelect();
      }
      onChange();
    },
    // select: the page's choice (UX1); the ring only, sent when it changed.
    select(consumer) {
      const c = consumer || null;
      if (c === selected) return;
      selected = c;
      sendSelect();
    },
    get selected() { return selected; },
    // picked: whether a click Unity reported names a customer of this very
    // scene and slot; an older scene's or slot's click, or a customer the
    // scene has no slot for, names nobody.
    picked(r) {
      if (!active || paused || !r || r.scene !== scene) return false;
      const s = slots.get(r.consumer);
      return !!s && !!s.task && s.slot === r.slot;
    },
    // setRuns: each consumer's scene task is its newest run's task (of the venue, UX3).
    setRuns(runs) {
      // News: a task the last list did not have (a run started now), whose first read is paced too; a page's
      // first list, a restart or another venue only shows what is already there.
      const known = lastRuns && new Set(lastRuns.map((r) => r && r.task_id));
      lastRuns = runs;
      if (!active || paused) return;
      for (const [consumer, r] of A.sceneTasks(runs, venue)) follow(consumer, r.task, !!known && !known.has(r.task));
    },
    // setVenue (UX3): another scene the page chose, shown in room layout. A
    // change is a new scene: no slot is kept; the venue's runs come back.
    setVenue(v, room) {
      if (v === venue && room === layout) return;
      venue = v || "";
      layout = room || "";
      for (const s of slots.values()) stopSlot(s);
      slots.clear();
      if (active && !paused) {
        scene += 1;
        unity.send2(A.sceneClear2(scene, layout));
        sendSelect();
        for (const [consumer, r] of A.sceneTasks(lastRuns, venue)) follow(consumer, r.task);
      }
      onChange();
    },
    get venue() { return venue; },
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
      const shown = s.shown || s.st.snapshot;
      const sent = s.sent && { gen: s.sent.scene, consumer, slot: s.sent.slot, task: s.sent.task_id, seq: s.sent.seq, phase: s.sent.phase,
        po: shown ? A.scene(shown).po : null };
      const report = s.report && { gen: s.report.scene, consumer: s.report.consumer, slot: s.report.slot, task: s.report.task, seq: s.report.seq, phase: s.report.phase,
        verdict: s.report.verdict, reason: s.report.reason };
      return { task: s.task, sent, report, shown: A.sameShown2(s.sent, s.report), motion: s.motion && { rule: s.motion.rule, text: s.motion.text, first: s.motion.first } };
    },
    // snapshot: what one consumer's slot shows now (paced: the step on screen; else the last committed read),
    // for the cards and the audience window, so they say what the scene shows.
    snapshot(consumer) {
      const s = slots.get(consumer);
      return (s && s.task && (s.shown || s.st.snapshot)) || null;
    },
    // latest: the last committed read, whatever the display shows.
    latest(consumer) {
      const s = slots.get(consumer);
      return (s && s.task && s.st.snapshot) || null;
    },
    // behind: the display trails the real task (steps still to show).
    behind(consumer) {
      const s = slots.get(consumer);
      return !!(s && s.task && s.st.snapshot && s.shown && s.shown.task.as_of_sequence !== s.st.snapshot.task.as_of_sequence);
    },
    // slots: every slot's state for the page, by consumer.
    slots() {
      return [...slots.values()].sort((a, b) => a.consumer.localeCompare(b.consumer)).map((s) => {
        const shown = s.shown || s.st.snapshot;
        const sc = shown ? A.scene(shown) : null;
        return { consumer: s.consumer, task: s.task, slot: s.slot, scene, seq: s.sent ? s.sent.seq : -1, phase: s.sent ? s.sent.phase : "",
          label: sc ? sc.label : "", shown: A.sameShown2(s.sent, s.report), moving: !!(s.motion && s.motion.rule <= 2 && !s.motion.done && !s.motion.first), read: s.read };
      });
    },
  };
}
