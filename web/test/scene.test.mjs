// Unit tests of M-B's version 2 scene (web/scene.js and adapter.js's version
// 2 helpers; docs/multi-agent-demo-design.md 5): a slot per customer, read
// with its own viewer token, the six-field completion rule, and one
// customer's slow, failed or late reads never touching the other.
import { afterEach, test } from "node:test";
import assert from "node:assert/strict";
import * as A from "../adapter.js";
import { createScene } from "../scene.js";

let n = 0;
const ev = (seq, type, extra = {}) => ({ event_id: "evt-" + seq + "-" + n++, event_type: type, sequence: seq, evidence_mode: "fixture", source: "host", observed_at: 1790726450 + seq, ...extra });
const askEvents = () => [ev(1, "TASK_STARTED"), ev(2, "PURCHASE_PROPOSED", { purchase_order_id: "po-1" }),
  ev(3, "POLICY_DECIDED", { purchase_order_id: "po-1", decision: { policy_verdict: "Ask", reason: "HUMAN_APPROVAL_REQUIRED", approval_status: "pending", payment_status: "unavailable" } })];

function reply(taskId, consumer, events) {
  const seq = events.length ? events[events.length - 1].sequence : 0;
  return {
    task: { task_id: taskId, consumer_id: consumer, agent_id: "agent", wallet_id: "w", payer: "0x1", wallet_evidence_mode: "fixture", status: "running",
      as_of_sequence: seq, served_at: 1790726460, purchases: [], payment_capability: "unavailable" },
    events: { task_id: taskId, as_of_sequence: seq, events },
  };
}

// A fake host: per task, the reply to serve, or a promise that never
// settles (slow), or a failure. It records which token read which task.
function fakeHost() {
  const data = new Map();
  const slow = new Set();
  const held = new Map(); // task -> the releases of its held replies
  const heldBody = new Map(); // task -> the releases of its events bodies: the reply has arrived, its JSON not yet
  const failing = new Set();
  const reads = [];
  const fetchFn = (path, init) => {
    const m = /^\/api\/tasks\/([^/]+)(\/events)?$/.exec(path);
    const task = decodeURIComponent(m[1]);
    reads.push({ task, token: init.headers.Authorization });
    if (slow.has(task)) return new Promise(() => {});
    if (held.has(task)) return new Promise((resolve) => held.get(task).push(() => {
      const r = data.get(task);
      resolve({ status: 200, ok: true, json: async () => (m[2] ? r.events : r.task) });
    }));
    if (failing.has(task)) return Promise.reject(new TypeError("offline"));
    if (m[2] && heldBody.has(task)) {
      const r = data.get(task);
      return Promise.resolve({ status: 200, ok: true, json: () => new Promise((resolve) => heldBody.get(task).push(() => resolve(r.events))) });
    }
    const r = data.get(task);
    if (!r) return Promise.resolve({ status: 404, ok: false, json: async () => ({}) });
    return Promise.resolve({ status: 200, ok: true, json: async () => (m[2] ? r.events : r.task) });
  };
  return { data, slow, held, heldBody, failing, reads, fetchFn };
}

function fakeUnity() {
  return { sent: [], selects: [], owned: false, own(on) { this.owned = on; }, send2(m) { if (this.owned) this.sent.push(m); }, select2(m) { if (this.owned) this.selects.push(m); } };
}

const tick = () => new Promise((r) => setTimeout(r, 0));
async function settle() { for (let i = 0; i < 10; i++) await tick(); }

const runs = (...pairs) => pairs.map(([consumer, task, run]) => ({ consumer_id: consumer, task_id: task, run_id: run || "run-" + task }));

// Every scene is stopped after its test, passed or failed: a failed
// assertion skips the test's own stop, and a live poll timer would keep
// node --test from ever ending.
const scenes = [];
afterEach(() => { while (scenes.length) scenes.pop().stop(); });

function setup() {
  const host = fakeHost();
  const unity = fakeUnity();
  const tokens = { "consumer-a": "token-a", "consumer-b": "token-b" };
  const changes = { n: 0 };
  const scene = createScene({ unity, token: (c) => tokens[c], onChange: () => { changes.n++; }, fetchFn: host.fetchFn });
  scenes.push(scene);
  return { host, unity, scene, changes };
}

test("the six-field rule: only the exact projection last sent completes", () => {
  const sent = { v: 2, scene: 3, consumer: "consumer-a", slot: 2, task_id: "show-a-02", seq: 3, phase: "waiting" };
  const ok = { scene: 3, consumer: "consumer-a", slot: 2, task: "show-a-02", seq: 3, phase: "waiting" };
  assert.equal(A.sameShown2(sent, ok), true);
  for (const [name, bad] of [
    ["another consumer", { ...ok, consumer: "consumer-b" }], ["another slot", { ...ok, slot: 1 }], ["another scene", { ...ok, scene: 2 }],
    ["another task", { ...ok, task: "show-a-01" }], ["another phase", { ...ok, phase: "quoted" }], ["an older seq", { ...ok, seq: 2 }],
    ["a seq never sent", { ...ok, seq: 4 }], ["no scene", { ...ok, scene: undefined }],
  ]) assert.equal(A.sameShown2(sent, bad), false, name);
  assert.equal(A.sameShown2(null, ok), false);
  assert.equal(A.sameShown2(sent, null), false);
});

test("version 2 reports, bridge reports and messages", () => {
  assert.deepEqual(A.parseReport2(3, 2, "consumer-b|1|show-b-01|quoted||"), { scene: 3, seq: 2, consumer: "consumer-b", slot: 1, task: "show-b-01", phase: "quoted", verdict: "", reason: "" });
  for (const bad of ["quoted|show-b-01||", "consumer-b|x|show-b-01|quoted||", "|1|show-b-01|quoted||", "consumer-b|1||quoted||", "a|1|t|p|v|r|extra"])
    assert.equal(A.parseReport2(3, 2, bad), null, bad);
  const b = A.parseBridge("versions=1,2;customers=consumer-a,consumer-b");
  assert.deepEqual([...b.versions], [1, 2]);
  assert.deepEqual(b.customers, ["consumer-a", "consumer-b"]);
  assert.equal(A.parseBridge("limezu"), null);
  assert.equal(b.select, false, "an older build takes no Select");
  assert.equal(A.parseBridge("versions=1,2;customers=consumer-a,consumer-b;select=1").select, true);
  assert.equal(A.parseBridge("versions=1,2;customers=consumer-a;select=2"), null);
  assert.deepEqual(A.sceneClear2(4), { v: 2, scene: 4, clear: true });
  assert.deepEqual(A.slotClear2(4, "consumer-a", 2), { v: 2, scene: 4, consumer: "consumer-a", slot: 2, clear: true });
  const st = A.newState();
  A.reset(st, "consumer-a", "show-a-01");
  const r = reply("show-a-01", "consumer-a", askEvents());
  A.commit(st, st.gen, r.task, r.events);
  const m = A.projection2(st, 5, 2, true);
  assert.deepEqual(Object.keys(m).sort(), ["animate", "cause_seq", "clear", "consumer", "payments", "phase", "reason", "scene", "seq", "slot", "task_end", "task_id", "v", "verdict"]);
  assert.equal(m.v, 2);
  assert.equal(m.phase, "waiting");
});

test("each consumer's scene task is its newest run; the scene says what it cannot show", () => {
  const t = A.sceneTasks(runs(["consumer-b", "show-b-02"], ["consumer-a", "show-a-03"], ["consumer-b", "show-b-01"]));
  assert.equal(t.get("consumer-b").task, "show-b-02");
  assert.equal(t.get("consumer-a").task, "show-a-03");
  assert.deepEqual(A.notShown(["consumer-a", "consumer-b"], ["consumer-a", "consumer-b"], 2), { consumers: [], overCap: false });
  assert.deepEqual(A.notShown(["consumer-a", "consumer-c"], ["consumer-a", "consumer-b"], 3), { consumers: ["consumer-c"], overCap: true });
});

test("two consumers at once, each read with its own token and sent in its own slot", async () => {
  const { host, unity, scene } = setup();
  host.data.set("show-a-01", reply("show-a-01", "consumer-a", askEvents()));
  host.data.set("show-b-01", reply("show-b-01", "consumer-b", askEvents().slice(0, 2)));
  scene.start();
  scene.setRuns(runs(["consumer-a", "show-a-01"], ["consumer-b", "show-b-01"]));
  await settle();
  for (const r of host.reads) assert.equal(r.token, r.task.startsWith("show-a") ? "Bearer token-a" : "Bearer token-b", "a consumer's task read with another's token");
  const a = unity.sent.find((m) => m.consumer === "consumer-a" && !m.clear);
  const b = unity.sent.find((m) => m.consumer === "consumer-b" && !m.clear);
  assert.equal(a.phase, "waiting");
  assert.equal(b.phase, "quoted");
  assert.equal(a.scene, b.scene);
  assert.equal(a.animate, false, "a first load is not news");
  const slots = scene.slots();
  assert.deepEqual(slots.map((x) => [x.consumer, x.task, x.read]), [["consumer-a", "show-a-01", "ok"], ["consumer-b", "show-b-01", "ok"]]);
  scene.stop();
});

test("A slow or failing never holds B", async () => {
  for (const kind of ["slow", "failing"]) {
    const { host, unity, scene } = setup();
    host[kind].add("show-a-01");
    host.data.set("show-b-01", reply("show-b-01", "consumer-b", askEvents()));
    scene.start();
    scene.setRuns(runs(["consumer-a", "show-a-01"], ["consumer-b", "show-b-01"]));
    await settle();
    assert.ok(unity.sent.some((m) => m.consumer === "consumer-b" && m.phase === "waiting"), kind + ": B was held up");
    assert.ok(!unity.sent.some((m) => m.consumer === "consumer-a" && !m.clear), kind + ": A sent something");
    const a = scene.slots().find((x) => x.consumer === "consumer-a");
    assert.equal(a.read, kind === "slow" ? "loading" : "offline");
    scene.stop();
  }
});

test("a new run of A changes A's slot only; A's late reply of its old task acts on nothing", async () => {
  const { host, unity, scene } = setup();
  host.data.set("show-a-01", reply("show-a-01", "consumer-a", askEvents()));
  host.held.set("show-a-01", []); // the old task's reply comes only after A's new run
  host.data.set("show-b-01", reply("show-b-01", "consumer-b", askEvents()));
  scene.start();
  scene.setRuns(runs(["consumer-a", "show-a-01"], ["consumer-b", "show-b-01"]));
  await settle();
  const bSlot = scene.slots().find((x) => x.consumer === "consumer-b").slot;
  host.data.set("show-a-02", reply("show-a-02", "consumer-a", askEvents().slice(0, 1)));
  scene.setRuns(runs(["consumer-a", "show-a-02"], ["consumer-a", "show-a-01"], ["consumer-b", "show-b-01"]));
  await settle();
  for (const release of host.held.get("show-a-01")) release(); // the old reply arrives late
  await settle();
  const a = scene.slots().find((x) => x.consumer === "consumer-a");
  assert.equal(a.task, "show-a-02");
  assert.equal(scene.slots().find((x) => x.consumer === "consumer-b").slot, bSlot, "B's slot changed");
  const clears = unity.sent.filter((m) => m.clear && m.consumer === "consumer-a").map((m) => m.slot);
  assert.deepEqual(clears, [1, 2], "A's slot cleared once per task");
  assert.ok(unity.sent.every((m) => m.consumer !== "consumer-a" || m.clear || m.task_id === "show-a-02"), "a late reply of A's old task reached the shop");
  scene.stop();
});

// R1 (review of eed0f90): a reply whose response arrived before the scene
// was dropped, and whose JSON is read only after a new scene began, acts on
// nothing: no state, no projection, no notice, no next read of the old slot.
// The old task gains an event meanwhile, so an old slot that went on would
// have something new to send.
for (const [name, drop] of [
  ["reset (new credentials)", (scene) => scene.reset()],
  ["stop, then start", (scene) => { scene.stop(); scene.start(); }],
]) test("a reply that arrived before " + name + " and is read after it acts on nothing", async () => {
  const { host, unity, scene, changes } = setup();
  host.data.set("show-a-01", reply("show-a-01", "consumer-a", askEvents().slice(0, 2)));
  host.data.set("show-a-02", reply("show-a-02", "consumer-a", askEvents().slice(0, 1)));
  host.heldBody.set("show-a-01", []);
  scene.start();
  scene.setRuns(runs(["consumer-a", "show-a-01"]));
  await settle();
  assert.equal(host.heldBody.get("show-a-01").length, 1, "the old events reply is not held");
  host.data.set("show-a-01", reply("show-a-01", "consumer-a", askEvents()));
  drop(scene);
  scene.setRuns(runs(["consumer-a", "show-a-02"]));
  await settle();
  const sent = unity.sent.length;
  const notices = changes.n;
  const reads = host.reads.length;
  const newScene = unity.sent.filter((m) => m.clear && !m.consumer).pop().scene;
  assert.ok(unity.sent.some((m) => m.scene === newScene && m.task_id === "show-a-02"), "the new scene did not show A's new task");
  for (const release of host.heldBody.get("show-a-01")) release(); // the old JSON is read now
  await settle();
  assert.deepEqual(unity.sent.slice(sent), [], "the old reply sent a projection");
  assert.equal(changes.n, notices, "the old reply notified the page");
  assert.deepEqual(scene.slots().map((x) => [x.consumer, x.task, x.slot]), [["consumer-a", "show-a-02", 1]]);
  await new Promise((r) => setTimeout(r, 1300)); // past one poll interval
  assert.ok(!host.reads.slice(reads).some((x) => x.task === "show-a-01"), "the old slot read again");
  host.data.set("show-a-02", reply("show-a-02", "consumer-a", askEvents().slice(0, 2)));
  await new Promise((r) => setTimeout(r, 1300));
  assert.ok(unity.sent.slice(sent).some((m) => m.scene === newScene && m.task_id === "show-a-02" && m.seq === 2), "the new scene stopped updating");
  assert.ok(unity.sent.every((m) => m.clear || m.task_id !== "show-a-01" || m.scene < newScene), "the old task reached the new scene");
  scene.stop();
});

test("a report completes only its own customer's exact projection", async () => {
  const { host, unity, scene } = setup();
  host.data.set("show-a-01", reply("show-a-01", "consumer-a", askEvents()));
  host.data.set("show-b-01", reply("show-b-01", "consumer-b", askEvents()));
  scene.start();
  scene.setRuns(runs(["consumer-a", "show-a-01"], ["consumer-b", "show-b-01"]));
  await settle();
  const a = unity.sent.find((m) => m.consumer === "consumer-a" && !m.clear);
  const rep = { scene: a.scene, consumer: "consumer-a", slot: a.slot, task: "show-a-01", seq: a.seq, phase: a.phase };
  scene.rendered({ ...rep, consumer: "consumer-b", task: "show-b-01", slot: 99 }); // B's report of another slot
  scene.rendered({ ...rep, seq: a.seq + 1 }); // a sequence never sent
  assert.deepEqual(scene.slots().map((x) => x.shown), [false, false]);
  scene.rendered(rep);
  assert.deepEqual(scene.slots().map((x) => x.shown), [true, false], "A's report completed B, or A's exact one did not complete A");
  const v = scene.view("consumer-a");
  assert.equal(v.task, "show-a-01");
  assert.equal(v.shown, true);
  assert.equal(v.sent.slot, a.slot);
  scene.stop();
});

test("a replay pauses the scene; afterwards a new scene shows every slot again", async () => {
  const { host, unity, scene } = setup();
  host.data.set("show-a-01", reply("show-a-01", "consumer-a", askEvents()));
  scene.start();
  scene.setRuns(runs(["consumer-a", "show-a-01"]));
  await settle();
  const before = unity.sent.filter((m) => m.clear && !m.consumer).map((m) => m.scene);
  scene.pause();
  assert.equal(unity.owned, false);
  assert.equal(scene.active, false);
  assert.equal(scene.started, true);
  scene.setRuns(runs(["consumer-a", "show-a-01"])); // nothing while paused
  scene.resume();
  await settle();
  const scenes = unity.sent.filter((m) => m.clear && !m.consumer).map((m) => m.scene);
  assert.equal(scenes.length, before.length + 1);
  const again = unity.sent.filter((m) => m.consumer === "consumer-a" && !m.clear).pop();
  assert.equal(again.scene, scenes[scenes.length - 1]);
  assert.equal(again.animate, false);
  scene.stop();
});

// UX1 (docs/commercial-ux1.md): the page's choice goes to Unity for the
// current scene, again after a new one; a click Unity reports names a
// customer only for the current scene and that customer's current slot.
test("the choice is sent for the current scene, and again after a new scene", async () => {
  const { host, unity, scene } = setup();
  host.data.set("show-a-01", reply("show-a-01", "consumer-a", askEvents()));
  host.data.set("show-b-01", reply("show-b-01", "consumer-b", askEvents()));
  scene.select("consumer-b"); // before the scene: kept, sent once it starts
  assert.deepEqual(unity.selects, []);
  scene.start();
  scene.setRuns(runs(["consumer-a", "show-a-01"], ["consumer-b", "show-b-01"]));
  await settle();
  const first = unity.sent.find((m) => m.clear && !m.consumer).scene;
  assert.deepEqual(unity.selects, [{ scene: first, consumer: "consumer-b" }]);
  scene.select("consumer-b");
  assert.equal(unity.selects.length, 1, "the same choice is not sent again");
  scene.select("consumer-a");
  assert.deepEqual(unity.selects.at(-1), { scene: first, consumer: "consumer-a" });
  scene.reset(); // new credentials: a new scene, the choice sent for it
  assert.deepEqual(unity.selects.at(-1), { scene: first + 1, consumer: "consumer-a" });
  scene.select(null);
  assert.deepEqual(unity.selects.at(-1), { scene: first + 1, consumer: "" });
  scene.stop();
  assert.equal(scene.selected, null, "a stopped scene has no choice");
});

test("a click names a customer only for the current scene and its slot", async () => {
  const { host, scene } = setup();
  host.data.set("show-a-01", reply("show-a-01", "consumer-a", askEvents()));
  host.data.set("show-b-01", reply("show-b-01", "consumer-b", askEvents()));
  scene.start();
  scene.setRuns(runs(["consumer-a", "show-a-01"], ["consumer-b", "show-b-01"]));
  await settle();
  const [a, b] = scene.slots();
  assert.equal(scene.picked({ scene: a.scene, slot: a.slot, consumer: "consumer-a" }), true);
  assert.equal(scene.picked({ scene: b.scene, slot: b.slot, consumer: "consumer-b" }), true);
  assert.equal(scene.picked({ scene: a.scene - 1, slot: a.slot, consumer: "consumer-a" }), false, "an older scene's click");
  assert.equal(scene.picked({ scene: a.scene, slot: a.slot + 1, consumer: "consumer-a" }), false, "another slot's click");
  assert.equal(scene.picked({ scene: a.scene, slot: 1, consumer: "consumer-z" }), false, "a customer the scene has no slot for");
  host.data.set("show-a-02", reply("show-a-02", "consumer-a", askEvents().slice(0, 1)));
  scene.setRuns(runs(["consumer-a", "show-a-02"], ["consumer-b", "show-b-01"]));
  await settle();
  assert.equal(scene.picked({ scene: a.scene, slot: a.slot, consumer: "consumer-a" }), false, "A's click of its old slot after A's new run");
  scene.pause();
  assert.equal(scene.picked({ scene: b.scene, slot: b.slot, consumer: "consumer-b" }), false, "during a replay");
  scene.stop();
});

// An Agent's card (UX1): only a running run is stopped; a count is the
// host's or it is unconfirmed, never 0 or guessed.
test("a card stops only a running run and never makes up a count", () => {
  const run = (live, extra = {}) => ({ run_id: "r1", task_id: "show-a-01", consumer_id: "consumer-a", live, state: live === "ended" ? "ended" : "accepted", ...extra });
  assert.deepEqual(A.runCard(null), { state: "idle", text: "未開始", calls: "", kind: "", stoppable: false });
  const r = A.runCard(run("running", { tool_calls: 3 }));
  assert.equal(r.stoppable, true);
  assert.equal(r.calls, "已回覆工具呼叫 3 次");
  assert.equal(A.runCard(run("running", { tool_calls: 0 })).calls, "已回覆工具呼叫 0 次", "a real 0 is the host's 0");
  assert.equal(A.runCard(run("running")).kind, "unconfirmed", "no count from the host is unconfirmed, not 0");
  assert.equal(A.runCard(run("running", { tool_calls: "3" })).kind, "unconfirmed", "not a count");
  assert.equal(A.runCard(run("running", { tool_calls: 3 }), "status_503").kind, "unconfirmed", "the runs list could not be read");
  const s = A.runCard(run("stopping", { tool_calls: 2 }));
  assert.equal(s.stoppable, false, "a stopping run is not stopped again");
  assert.equal(s.text, "停止中（仍占名額）");
  const e = A.runCard(run("ended", { outcome: "ok" }));
  assert.equal(e.stoppable, false, "an ended run has nothing to stop");
  assert.equal(e.kind, "not_provided");
  assert.equal(A.runCard(run(undefined, { state: "interrupted" })).stoppable, false, "an interrupted run");
});

// UX3 (docs/commercial-scenes.md): the page picks a scene first; the shop
// shows that scene's room and only its runs, and every message of a scene
// names the same room.
test("UX3: bridge layouts, rooms in messages, the scenes in order", () => {
  const b = A.parseBridge("versions=1,2;customers=consumer-a,consumer-b;select=1;layouts=cafe,data,mart");
  assert.deepEqual(b.layouts, ["cafe", "data", "mart"]);
  assert.equal(b.select, true);
  assert.deepEqual(A.parseBridge("versions=1,2;customers=consumer-a;select=1").layouts, [], "an older build names no rooms");
  assert.equal(A.parseBridge("versions=1,2;customers=consumer-a;layouts=Cafe"), null);
  // T2 (docs/commercial-town.md): a build with the town says so last; an older one does not.
  assert.equal(A.parseBridge("versions=1,2;customers=consumer-a;select=1;layouts=cafe,data;town=1").town, true);
  assert.equal(b.town, false);
  assert.equal(A.parseBridge("versions=1,2;customers=consumer-a;select=1;layouts=data;town=2"), null);
  // The game (Aaron 10/29): after the town; only with it.
  assert.equal(A.parseBridge("versions=1,2;customers=consumer-a;select=1;layouts=data;town=1;play=1").play, true);
  assert.equal(b.play, false);
  assert.equal(A.parseBridge("versions=1,2;customers=consumer-a;select=1;layouts=data;play=1"), null, "no game without the town");
  assert.deepEqual(A.sceneClear2(4, "cafe"), { v: 2, scene: 4, clear: true, layout: "cafe" });
  assert.deepEqual(A.slotClear2(4, "consumer-a", 2, "mart"), { v: 2, scene: 4, consumer: "consumer-a", slot: 2, clear: true, layout: "mart" });
  assert.deepEqual(A.venues([{ scene: "mart" }, { scene: "zoo" }, {}, { scene: "cafe" }, { scene: "mart" }]), ["data", "cafe", "mart", "zoo"]);
  assert.equal(A.venue("cafe").demo, true);
  assert.equal(A.venue("data").demo, false);
  assert.match(A.venue("mart").delivery, /付款後才交付/);
  const rs = [{ consumer_id: "consumer-a", task_id: "show-a-02", scene: "mart" }, { consumer_id: "consumer-b", task_id: "show-b-01", scene: "cafe" },
    { consumer_id: "consumer-a", task_id: "show-a-01", scene: "cafe" }, { consumer_id: "consumer-b", task_id: "show-b-00" }];
  const cafe = A.sceneTasks(rs, "cafe");
  assert.equal(cafe.get("consumer-a").task, "show-a-01", "the newest run of this scene, not of another");
  assert.equal(cafe.get("consumer-b").task, "show-b-01");
  assert.equal(A.sceneTasks(rs, "data").size, 0, "a run that names no scene is in none");
  assert.equal(A.sceneTasks(rs).get("consumer-a").task, "show-a-02", "no venue: every run, as before");
});

test("UX3: a scene change is a new scene with its room, and only its runs", async () => {
  const { host, unity, scene } = setup();
  host.data.set("show-a-01", reply("show-a-01", "consumer-a", askEvents()));
  host.data.set("show-b-01", reply("show-b-01", "consumer-b", askEvents()));
  scene.start();
  scene.setVenue("cafe", "cafe");
  scene.setRuns([{ consumer_id: "consumer-a", task_id: "show-a-01", run_id: "r1", scene: "mart" }, { consumer_id: "consumer-b", task_id: "show-b-01", run_id: "r2", scene: "cafe" }]);
  await settle();
  const cafeScene = unity.sent.at(-1).scene;
  assert.ok(unity.sent.filter((m) => m.scene === cafeScene).every((m) => m.layout === "cafe"), "every message of the scene names its room");
  assert.deepEqual(scene.slots().map((x) => x.consumer), ["consumer-b"], "A's run is in the mart: not in the cafe");
  scene.setVenue("mart", "mart");
  await settle();
  const mart = unity.sent.filter((m) => m.scene > cafeScene);
  assert.ok(mart.length > 0 && mart[0].clear && !mart[0].consumer && mart.every((m) => m.layout === "mart"), "a new scene, all in the mart's room");
  assert.deepEqual(scene.slots().map((x) => x.consumer), ["consumer-a"]);
  assert.ok(mart.some((m) => m.consumer === "consumer-a" && m.phase === "waiting"));
  assert.ok(!mart.some((m) => m.consumer === "consumer-b"), "B's cafe slot is gone");
});

// T2 (docs/commercial-town.md): the page's View waits for a ready build with
// the town and goes before the scene's messages; a venue click is passed on
// only from such a build, and only as a plain key.
test("T2: View waits for a town build; venue clicks only from one", async () => {
  globalThis.window = globalThis.window || {};
  const { UnityHost } = await import("../unity.js");
  const seen = [];
  const u = new UnityHost(null, (s, d) => seen.push([s, d]));
  const sent = [];
  u.instance = { SendMessage: (o, m, json) => sent.push(m + " " + json) };
  u.view("town", null);
  assert.equal(u.pendingView, null, "a build that has not said town=1 gets no View");
  u.report("venue", 0, 0, "mart");
  assert.deepEqual(seen, [], "nor are its venue reports taken");
  u.report("bridge", 0, 0, "versions=1,2;customers=consumer-a;select=1;layouts=data,mart;town=1");
  u.own(true);
  u.send2({ v: 2, scene: 1, clear: true, layout: "mart" });
  u.view("venue", { x: 392, y: 0, width: 1116, height: 1080 });
  assert.deepEqual(sent, [], "nothing before ready");
  u.report("ready", -1, -1, "limezu");
  assert.equal(sent[0], 'View {"mode":"venue","safe":{"x":392,"y":0,"width":1116,"height":1080}}');
  assert.match(sent[1], /^Receive /);
  u.view("town", null);
  assert.equal(sent[2], 'View {"mode":"town","safe":{"x":0,"y":0,"width":0,"height":0}}', "null: all of the canvas, in JsonUtility's Rect shape");
  u.report("venue", 1, 0, "mart");
  u.report("venue", 1, 0, "Mart; drop");
  assert.deepEqual(seen.filter(([s]) => s === "venue"), [["venue", "mart"]]);
});

// Pacing (簡報模式, Aaron 10/27: 畫面依序播放): steps that arrive in one read are shown one after another, at least paceMs
// apart; the cards' snapshot follows the screen and behind() says the real task is ahead. Off, the newest shows at once.
test("pacing: one step at a time, the real task ahead said so", async () => {
  for (const paced of [true, false]) {
    const host = fakeHost();
    const unity = fakeUnity();
    const scene = createScene({ unity, token: () => "token-a", onChange: () => {}, fetchFn: host.fetchFn, paced: () => paced, paceMs: 150 });
    scenes.push(scene);
    host.data.set("show-a-01", reply("show-a-01", "consumer-a", askEvents().slice(0, 1)));
    scene.start();
    scene.setRuns(runs(["consumer-a", "show-a-01"]));
    await settle();
    const phases = () => unity.sent.filter((m) => m.consumer === "consumer-a" && !m.clear).map((m) => m.phase);
    assert.deepEqual(phases(), ["entered"], "a first load at once");
    host.data.set("show-a-01", reply("show-a-01", "consumer-a", askEvents())); // proposed and decided (Ask) arrive together
    for (const end = Date.now() + 3000; Date.now() < end && phases().length < 2;) await new Promise((r) => setTimeout(r, 20));
    if (!paced) {
      assert.deepEqual(phases(), ["entered", "waiting"], "unpaced: the newest at once");
      assert.equal(scene.behind("consumer-a"), false);
      scene.stop();
      continue;
    }
    assert.deepEqual(phases(), ["entered", "quoted"], "paced: the quote first");
    assert.equal(A.scene(scene.snapshot("consumer-a")).phase, "quoted", "the cards say what the screen shows");
    assert.equal(A.scene(scene.latest("consumer-a")).phase, "waiting", "the real task is ahead");
    assert.equal(scene.behind("consumer-a"), true);
    await new Promise((r) => setTimeout(r, 60));
    assert.deepEqual(phases(), ["entered", "quoted"], "held for its time");
    await new Promise((r) => setTimeout(r, 160));
    assert.deepEqual(phases(), ["entered", "quoted", "waiting"], "then the decision");
    assert.equal(scene.behind("consumer-a"), false, "caught up");
    const last = unity.sent[unity.sent.length - 1];
    assert.equal(last.animate, true, "a paced step moves the customer");
    // A run started now: its first read is news, paced from its start (a page's first list is not).
    host.data.set("show-a-02", reply("show-a-02", "consumer-a", askEvents()));
    scene.setRuns(runs(["consumer-a", "show-a-02"]));
    await settle();
    const fresh = () => unity.sent.filter((m) => m.task_id === "show-a-02" && !m.clear).map((m) => m.phase);
    assert.deepEqual(fresh(), ["entered"], "a new run starts at its first step");
    for (const end = Date.now() + 2000; Date.now() < end && fresh().length < 3;) await new Promise((r) => setTimeout(r, 20));
    assert.deepEqual(fresh(), ["entered", "quoted", "waiting"], "then each step in turn");
    scene.stop();
  }
});

// Review of f548706: a paced step is the latest task with a prefix of its events; the window's evidence says only
// what the step's own events say, though the purchase's record already holds approval, payment and receipt.
test("pacing: a step's evidence is its own events', not the record's", () => {
  const purchase = { purchase_order_id: "po-1", amount: "500000", approval: { state: "approved", grant: { authorization_method: "webauthn" } },
    payment: { reserve_tx: "0x" + "a".repeat(64), settle_tx: "0x" + "b".repeat(64), receipt_id: "rcpt-1" } };
  const full = reply("t-1", "consumer-a", [...askEvents(), ev(4, "APPROVED", { purchase_order_id: "po-1", authorization_method: "webauthn" }),
    ev(5, "BUDGET_RESERVED", { purchase_order_id: "po-1" }), ev(6, "PAYMENT_SETTLED", { purchase_order_id: "po-1" }), ev(7, "RECEIPT_ISSUED", { purchase_order_id: "po-1" })]);
  full.task.purchases = [purchase];
  full.task.payment_capability = "local_anvil";
  const step = (n) => ({ task: { ...full.task, as_of_sequence: n }, events: full.events.events.filter((e) => e.sequence <= n) });
  const labels = (n) => A.showEvidence(step(n)).map((x) => x.label);
  assert.deepEqual(labels(3), ["委託"], "waiting: no approval, no payment, no receipt yet");
  assert.deepEqual(labels(4), ["委託", "核准"]);
  assert.deepEqual(labels(5), ["委託", "核准", "預算預留"]);
  assert.deepEqual(labels(7), ["委託", "核准", "預算預留", "付款", "收據"], "the whole read: all of it");
});

test("pacing: a read with no new phase takes no extra hold; pacing turned off shows the newest at once", async () => {
  let paced = true;
  const host = fakeHost();
  const unity = fakeUnity();
  const scene = createScene({ unity, token: () => "token-a", onChange: () => {}, fetchFn: host.fetchFn, paced: () => paced, paceMs: 600 });
  scenes.push(scene);
  host.data.set("show-a-01", reply("show-a-01", "consumer-a", askEvents().slice(0, 1)));
  scene.start();
  scene.setRuns(runs(["consumer-a", "show-a-01"]));
  await settle();
  const sent = () => unity.sent.filter((m) => m.consumer === "consumer-a" && !m.clear).map((m) => m.phase + "#" + m.seq);
  host.data.set("show-a-01", reply("show-a-01", "consumer-a", askEvents())); // quoted (2), waiting (3)
  for (const end = Date.now() + 3000; Date.now() < end && sent().length < 2;) await new Promise((r) => setTimeout(r, 20));
  assert.deepEqual(sent(), ["entered#1", "quoted#2"]);
  host.data.set("show-a-01", reply("show-a-01", "consumer-a", [...askEvents(), ev(4, "APPROVAL_SESSION_OPENED", { purchase_order_id: "po-1" })])); // still waiting
  const t0 = Date.now();
  for (const end = Date.now() + 3000; Date.now() < end && !sent().includes("waiting#4");) await new Promise((r) => setTimeout(r, 20));
  const phases = sent().map((x) => x.split("#")[0]).filter((p, i, a) => p !== a[i - 1]);
  assert.deepEqual(phases, ["entered", "quoted", "waiting"], "no extra step for a read in the same phase");
  assert.ok(sent().includes("waiting#4") && Date.now() - t0 < 1700, "the newer read shown without another hold: " + sent());
  // Off mid-queue: the next read shows the newest at once.
  host.data.set("show-a-01", reply("show-a-01", "consumer-a", [...askEvents(), ev(4, "APPROVAL_SESSION_OPENED", { purchase_order_id: "po-1" }),
    ev(5, "APPROVAL_EXPIRED", { purchase_order_id: "po-1" })]));
  await new Promise((r) => setTimeout(r, 50));
  paced = false;
  for (const end = Date.now() + 2500; Date.now() < end && !sent().some((x) => x.startsWith("expired"));) await new Promise((r) => setTimeout(r, 20));
  assert.equal(sent()[sent().length - 1], "expired#5", "unpaced: the newest at once");
  assert.equal(scene.behind("consumer-a"), false);
  scene.stop();
});
