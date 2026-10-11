// Unit tests of M-B's version 2 scene (web/scene.js and adapter.js's version
// 2 helpers; docs/multi-agent-demo-design.md 5): a slot per customer, read
// with its own viewer token, the six-field completion rule, and one
// customer's slow, failed or late reads never touching the other.
import { test } from "node:test";
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
    const r = data.get(task);
    if (!r) return Promise.resolve({ status: 404, ok: false, json: async () => ({}) });
    return Promise.resolve({ status: 200, ok: true, json: async () => (m[2] ? r.events : r.task) });
  };
  return { data, slow, held, failing, reads, fetchFn };
}

function fakeUnity() {
  return { sent: [], owned: false, own(on) { this.owned = on; }, send2(m) { if (this.owned) this.sent.push(m); } };
}

const tick = () => new Promise((r) => setTimeout(r, 0));
async function settle() { for (let i = 0; i < 10; i++) await tick(); }

const runs = (...pairs) => pairs.map(([consumer, task, run]) => ({ consumer_id: consumer, task_id: task, run_id: run || "run-" + task }));

function setup() {
  const host = fakeHost();
  const unity = fakeUnity();
  const tokens = { "consumer-a": "token-a", "consumer-b": "token-b" };
  const scene = createScene({ unity, token: (c) => tokens[c], onChange: () => {}, fetchFn: host.fetchFn });
  return { host, unity, scene };
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
  assert.deepEqual(A.sceneClear2(4), { v: 2, scene: 4, clear: true });
  assert.deepEqual(A.slotClear2(4, "consumer-a", 2), { v: 2, scene: 4, consumer: "consumer-a", slot: 2, clear: true });
  const st = A.newState();
  A.reset(st, "consumer-a", "show-a-01");
  const r = reply("show-a-01", "consumer-a", askEvents());
  A.commit(st, st.gen, r.task, r.events);
  const m = A.projection2(st, 5, 2, true);
  assert.deepEqual(Object.keys(m).sort(), ["animate", "cause_seq", "clear", "consumer", "phase", "reason", "scene", "seq", "slot", "task_end", "task_id", "v", "verdict"]);
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
