// Tests of P1-a in the page (docs/commercial-p1a.md): the adapter's
// apa.pay.v1 phases, words and Unity mapping (an M1 task unchanged), and
// the simulated approver's capability in hostcreds.js: held per generation,
// replaced by the next consumer, never answered for after a switch.
// Run: node --test web/test/p1a.test.mjs
import { test } from "node:test";
import assert from "node:assert/strict";
import * as A from "../adapter.js";
import { createHostCreds } from "../hostcreds.js";

let n = 0;
const ev = (seq, type, extra = {}) => ({ event_id: "e" + n++, event_type: type, sequence: seq, evidence_mode: "fixture", source: "host", observed_at: 1790726450 + seq, purchase_order_id: "po-1", ...extra });
const decision = (approval, reason, version = A.V1) => ({ version, policy_verdict: "Ask", reason, approval_status: approval, payment_status: version === A.V1 ? "not_enabled" : "unavailable" });
const snap = (version, events, purchases = []) => ({
  task: { task_id: "show-a-01", consumer_id: "consumer-a", status: "running", as_of_sequence: events.length, served_at: 1790726470, purchases, contract_version: version,
    payment_capability: version === A.V1 ? "not_enabled" : "unavailable" },
  events,
});
const asked = (version) => [ev(1, "TASK_STARTED", { purchase_order_id: "" }), ev(2, "PURCHASE_PROPOSED"), ev(3, "POLICY_DECIDED", { decision: decision("pending", "HUMAN_APPROVAL_REQUIRED", version) })];

test("apa.pay.v1: each closure is its own phase; Unity gets the M1 phase that is true of it", () => {
  for (const [type, approval, reason, phase, unity] of [
    ["APPROVED", "approved", "SIMULATED_APPROVAL", "approved", "allowed_unpaid"],
    ["APPROVAL_DENIED", "denied", "APPROVER_DENIED", "approval_denied", "denied"],
    ["APPROVAL_SUPERSEDED", "superseded", "QUOTE_CHANGED", "superseded", "withdrawn"],
    ["APPROVAL_EXPIRED", "expired", "APPROVAL_TIMEOUT", "expired", "expired"],
  ]) {
    const s = snap(A.V1, [...asked(A.V1), ev(4, type, { decision: decision(approval, reason) })]);
    const sc = A.scene(s);
    assert.equal(sc.phase, phase, type);
    assert.equal(sc.cause, 4, type);
    const m = A.projection({ snapshot: s, gen: 1 }, false);
    assert.equal(m.phase, unity, type);
    assert.ok(!/paid|deliver|pickup/i.test(sc.label), sc.label);
    assert.equal(A.project(s.events).status, "running", type);
  }
  const approved = A.scene(snap(A.V1, [...asked(A.V1), ev(4, "APPROVED", { decision: decision("approved", "SIMULATED_APPROVAL") })]));
  assert.equal(approved.label, "已核准（模擬）：未啟用付款，不會取貨");
  const denied = A.projection({ snapshot: snap(A.V1, [...asked(A.V1), ev(4, "APPROVAL_DENIED", { decision: decision("denied", "APPROVER_DENIED") })]), gen: 1 }, false);
  assert.equal(denied.reason, "APPROVER_DENIED");
});

test("apa.pay.v1 words: waiting is the simulated approval, never M1's 'cannot approve'; M1 keeps its words", () => {
  const p = { purchase_order_id: "po-1", decision: decision("pending", "HUMAN_APPROVAL_REQUIRED"), deadline_at: 1790726500, amount: "500000" };
  const v1 = A.card(snap(A.V1, asked(A.V1), [p]), null, { mode: "live" });
  assert.match(v1.status, /等你核准（模擬核准；不會付款）/);
  assert.ok(!JSON.stringify(v1).includes("M1 無核准能力"));
  assert.equal(A.scene(snap(A.V1, asked(A.V1))).label, "等你核准（模擬核准；不會付款）");
  const m1p = { ...p, decision: decision("pending", "HUMAN_APPROVAL_REQUIRED", "apa.m1.v2") };
  const m1 = A.card(snap("apa.m1.v2", asked("apa.m1.v2"), [m1p]), null, { mode: "live" });
  assert.match(m1.status, /M1 無核准能力/);
  assert.equal(A.scene(snap("apa.m1.v2", asked("apa.m1.v2"))).label, "Ask：等待人工核准（M1 無法核准）");
  const done = A.card(snap(A.V1, [...asked(A.V1), ev(4, "APPROVED", { decision: decision("approved", "SIMULATED_APPROVAL") })],
    [{ ...p, approval_closed: "approved", approval: { approval_session_id: "aps-1", state: "approved", grant: { jti: "grant-1", authorization_method: "simulated" } } }]), null, { mode: "live" });
  assert.equal(done.status, "已核准（模擬），未啟用付款");
  assert.ok(done.rows.some(([k, v]) => k === "授權" && v.includes("不能付款")));
});

test("a replay step closes a v1 Ask from its own event, never from the final record", () => {
  const s = snap(A.V1, [...asked(A.V1), ev(4, "APPROVED", { decision: decision("approved", "SIMULATED_APPROVAL") })],
    [{ purchase_order_id: "po-1", decision: decision("pending", "HUMAN_APPROVAL_REQUIRED"), approval_closed: "approved" }]);
  assert.equal(A.stepView(s, 3).task.purchases[0].approval_closed, undefined);
  assert.equal(A.stepView(s, 4).task.purchases[0].approval_closed, "approved");
});

// A fake host: the operator mints a capability; /api/sim/* answers only the
// newest one. Replies can be held to arrive late.
function fakeHost() {
  let current = null, nextTok = 0;
  const held = [];
  let hold = false;
  const answer = (token, path, post) => {
    if (path === "/api/host/status") return { status: 200, body: { caller_role: token === "c".repeat(64) ? "host" : "operator" } };
    if (path === "/api/viewer") return { status: 200, body: { role: "viewer", consumer_id: token === "a".repeat(64) ? "consumer-a" : "consumer-b" } };
    if (path === "/api/host/simulate") { current = { token: String(++nextTok).padStart(64, "0"), consumer: post.body.consumer_id }; return { status: 200, body: { token: current.token, consumer_id: current.consumer } }; }
    if (path.startsWith("/api/sim/")) return current && token === current.token ? { status: 200, body: { consumer_id: current.consumer, approvals: [], served_at: 1 } } : { status: 401, body: { error: "unauthenticated" } };
    return { status: 404, body: null };
  };
  return {
    call: (token, path, signal, post) => new Promise((resolve) => {
      const r = answer(token, path, post);
      if (hold && path.startsWith("/api/sim/")) held.push(() => resolve(r)); else resolve(r);
    }),
    hold(on) { hold = on; },
    release() { while (held.length) held.shift()(); },
  };
}
const tokens = { note: "x", tokens: [{ role: "viewer", consumer_id: "consumer-a", token: "a".repeat(64) }, { role: "viewer", consumer_id: "consumer-b", token: "b".repeat(64) },
  { role: "host", token: "c".repeat(64) }, { role: "operator", token: "d".repeat(64) }] };
const file = () => { const text = JSON.stringify(tokens); return { name: "t.json", size: text.length, text: async () => text }; };

test("the simulated customer: one at a time, per generation; a late reply after a switch is dropped", async () => {
  const host = fakeHost();
  const c = createHostCreds({ call: host.call, onChange: () => {} });
  await c.load(file());
  assert.equal(c.view().simConsumer, null);
  assert.equal(await c.approvals(), null, "nothing without a capability");
  assert.equal((await c.simulate("consumer-a")).status, 200);
  assert.equal(c.view().simConsumer, "consumer-a");
  assert.equal((await c.approvals()).consumer, "consumer-a");
  host.hold(true);
  const late = c.approvals();
  host.hold(false);
  await c.simulate("consumer-b"); // the host replaced A's capability with B's
  host.release();
  const old = await late;
  assert.ok(old && old.superseded === true && old.status === undefined && old.body === undefined, "A's reply arriving after the switch to B is dropped");
  assert.equal(c.view().simConsumer, "consumer-b");
  c.clearAll("x");
  assert.equal(c.view().simConsumer, null, "a new generation holds no capability");
  assert.equal(await c.approvals(), null);
});

// ---- review of f95415d, R2: an operation keeps its capability ----

// A host whose /api/sim/* calls are scripted per attempt: "throw",
// "timeout" (rejects as a timed-out fetch would) or a reply, optionally
// held until released. It records every call's capability and request ID.
function scriptedHost() {
  let current = null, nextTok = 0;
  const sent = [], script = [], held = [];
  const call = (token, path, signal, post) => {
    if (path === "/api/host/status") return Promise.resolve({ status: 200, body: { caller_role: token === "c".repeat(64) ? "host" : "operator" } });
    if (path === "/api/viewer") return Promise.resolve({ status: 200, body: { role: "viewer", consumer_id: token === "a".repeat(64) ? "consumer-a" : "consumer-b" } });
    if (path === "/api/host/simulate") {
      current = { token: String(++nextTok).padStart(64, "0"), consumer: post.body.consumer_id };
      return Promise.resolve({ status: 200, body: { token: current.token, consumer_id: current.consumer } });
    }
    sent.push({ token, path, request: post && post.body.request_id });
    const step = script.shift() || { status: 200, body: { ok: true } };
    const answer = () => (step === "throw" ? Promise.reject(new TypeError("network"))
      : step === "timeout" ? Promise.reject(new DOMException("timed out", "TimeoutError")) : Promise.resolve(step));
    if (step.hold) return new Promise((res, rej) => held.push(() => answer().then(res, rej)));
    return answer();
  };
  return { call, sent, script, release() { while (held.length) held.shift()(); }, token: () => current && current.token };
}

async function signedIn(host, waits) {
  const c = createHostCreds({ call: host.call, onChange: () => {}, wait: (ms) => (waits ? waits(ms) : Promise.resolve()) });
  await c.load(file());
  return c;
}

for (const path of ["throw", "timeout"]) {
  test(`R2 (${path}): after a failed attempt, A→B and A→B→A send nothing with the new capability`, async () => {
    for (const switches of [["consumer-b"], ["consumer-b", "consumer-a"]]) {
      const host = scriptedHost();
      let c;
      const waits = async () => { for (const x of switches) await c.simulate(x); }; // the customer changes while the resend waits
      c = await signedIn(host, waits);
      await c.simulate("consumer-a");
      const aToken = host.token();
      host.script.push(path);
      const r = await c.resolve("aps-1", "sha256:" + "0".repeat(64), "approve");
      assert.equal(r.superseded, true, switches.join("→"));
      assert.equal(r.consumer, "consumer-a");
      assert.notEqual(r.simId, c.view().simId, "the old operation's capability is not the current one");
      assert.equal(host.sent.length, 1, "no resend after the switch: " + switches.join("→"));
      assert.equal(host.sent[0].token, aToken);
    }
  });
}

test("R2: within one capability a resend keeps the request ID and the capability", async () => {
  const host = scriptedHost();
  const c = await signedIn(host);
  await c.simulate("consumer-a");
  host.script.push("throw", "timeout", { status: 200, body: { result: { outcome: "approved" } } });
  const r = await c.resolve("aps-1", "sha256:" + "0".repeat(64), "approve");
  assert.equal(r.status, 200);
  assert.equal(host.sent.length, 3, "the existing bound: three attempts");
  assert.equal(new Set(host.sent.map((x) => x.request)).size, 1, "one request ID");
  assert.equal(new Set(host.sent.map((x) => x.token)).size, 1, "one capability");
});

test("R2: a late success or a late error of A after the switch to B comes back superseded", async () => {
  for (const reply of [{ status: 200, body: { result: { outcome: "approved" } } }, { status: 409, body: { error: "approval_closed" } }]) {
    const host = scriptedHost();
    const c = await signedIn(host);
    await c.simulate("consumer-a");
    host.script.push({ ...reply, hold: true });
    const pending = c.resolve("aps-1", "sha256:" + "0".repeat(64), "approve");
    await flush();
    await c.simulate("consumer-b");
    host.release();
    const r = await pending;
    assert.equal(r.superseded, true, "late " + reply.status);
    assert.equal(r.status, undefined, "the old reply's status is not handed on");
    assert.equal(c.view().simConsumer, "consumer-b");
  }
});
const flush = () => new Promise((r) => setTimeout(r, 0));

// ---- review of f95415d, R1: the focus comes back to the same item only ----

test("R1: sameTarget: the same item of the same capability and generation; never another", () => {
  const item = (session, state = "pending", task = "show-a-01") => ({ consumer_id: "consumer-a", task_id: task,
    purchase: { intent_hash: "sha256:1", approval: { approval_session_id: session, state } } });
  const was = { ...A.approvalTarget(3, 7, item("aps-1")) };
  const asData = Object.fromEntries(Object.entries(was).map(([k, v]) => [k, String(v)])); // as a button's dataset keeps it
  assert.equal(A.sameTarget(asData, A.approvalTarget(3, 7, item("aps-1")), true), true, "the same pending item");
  assert.equal(A.sameTarget(asData, A.approvalTarget(3, 7, item("aps-2", "pending", "show-a-02")), true), false, "another session");
  assert.equal(A.sameTarget(asData, A.approvalTarget(3, 7, item("aps-1", "expired")), true), false, "closed: a submit does not come back");
  assert.equal(A.sameTarget(asData, A.approvalTarget(3, 7, item("aps-1", "expired")), false), true, "closed: the evidence button may");
  assert.equal(A.sameTarget(asData, A.approvalTarget(3, 8, item("aps-1")), true), false, "another capability (A again)");
  assert.equal(A.sameTarget(asData, A.approvalTarget(4, 7, item("aps-1")), true), false, "another generation");
  assert.equal(A.sameTarget(asData, A.approvalTarget(3, 7, null), true), false, "no item");
});
