// Unit tests of web/adapter.js (node --test web/test/). Expected reasons
// come from the accepted testdata/contracts/cases.json, never from the
// Policy under test.
import { test } from "node:test";
import assert from "node:assert/strict";
import { readFileSync } from "node:fs";
import * as A from "../adapter.js";

const cases = JSON.parse(readFileSync(new URL("../../testdata/contracts/cases.json", import.meta.url)));
const expected = (id) => cases.cases.find((c) => c.case_id === id).expected;

let n = 0;
function ev(seq, type, extra = {}) {
  return { event_id: "evt-" + seq + "-" + (n++), event_type: type, sequence: seq, evidence_mode: "fixture", source: "host", observed_at: 1790726450 + seq, ...extra };
}
const decided = (seq, verdict, reason, approval) =>
  ev(seq, "POLICY_DECIDED", { purchase_order_id: "po-1", decision: { policy_verdict: verdict, reason, approval_status: approval, payment_status: "unavailable" } });

function snapshot(taskId, events, purchases = [], extra = {}) {
  const seq = events.length ? events[events.length - 1].sequence : 0;
  return {
    task: { task_id: taskId, consumer_id: "consumer-a", agent_id: "agent-a", wallet_id: "wallet-a", payer: "0x1111", wallet_evidence_mode: "fixture",
      status: "awaiting_approval", as_of_sequence: seq, served_at: 1790726460, purchases, payment_capability: "unavailable", ...extra },
    events: { task_id: taskId, as_of_sequence: seq, events },
  };
}

const ask = expected("ask");
const deny = expected("deny-budget");
const askEvents = () => [ev(1, "TASK_STARTED"), ev(2, "PURCHASE_PROPOSED"), decided(3, "Ask", ask.reason, ask.approval_status)];
const denyEvents = () => [ev(1, "TASK_STARTED"), ev(2, "PURCHASE_PROPOSED"), decided(3, "Deny", deny.reason, deny.approval_status)];

test("commits only a consistent pair, for the current generation", () => {
  const s = A.newState();
  const gen = A.reset(s, "consumer-a", "task-a");
  const { task, events } = snapshot("task-a", askEvents());
  assert.equal(A.commit(s, gen - 1, task, events), "stale");
  assert.equal(A.commit(s, gen, task, { ...events, as_of_sequence: 2 }), "inconsistent");
  assert.equal(A.commit(s, gen, { ...task, task_id: "task-b" }, events), "wrong_task");
  assert.equal(A.commit(s, gen, task, events), "ok");
  const older = snapshot("task-a", askEvents().slice(0, 2));
  assert.equal(A.commit(s, gen, older.task, older.events), "older");
  assert.equal(s.snapshot.task.as_of_sequence, 3, "a late older snapshot does not replace a newer one");
});

test("a switch drops every late reply of the old viewer", () => {
  const s = A.newState();
  const g1 = A.reset(s, "consumer-a", "task-a");
  const g2 = A.reset(s, "consumer-b", "task-b");
  const a = snapshot("task-a", askEvents());
  assert.equal(A.commit(s, g1, a.task, a.events), "stale");
  assert.equal(s.snapshot, null);
  assert.equal(A.fail(s, g1, "offline"), "stale");
  assert.equal(s.status, "loading");
  assert.ok(g2 > g1);
});

test("duplicate events are shown once and animated once", () => {
  const s = A.newState();
  const gen = A.reset(s, "consumer-a", "task-a");
  const e = askEvents();
  const { task, events } = snapshot("task-a", e);
  events.events = [...e, e[2], e[0]];
  A.commit(s, gen, task, events);
  assert.deepEqual(s.snapshot.events.map((x) => x.sequence), [1, 2, 3]);
  assert.deepEqual(A.fresh(s, true), [], "a first load is a stored snapshot, not news");
  assert.deepEqual(A.fresh(s, false), [], "nothing new on the next poll");
});

test("Ask waits, Deny leaves, Allow says no payment capability", () => {
  const a = snapshot("task-a", askEvents());
  assert.equal(A.scene({ task: a.task, events: a.events.events }).phase, "waiting");
  const d = snapshot("task-b", denyEvents());
  const sd = A.scene({ task: d.task, events: d.events.events });
  assert.equal(sd.phase, "denied");
  assert.equal(sd.reason, deny.reason);
  const al = snapshot("task-x", [ev(1, "TASK_STARTED"), ev(2, "PURCHASE_PROPOSED"), decided(3, "Allow", expected("allow-unavailable").reason, "not_requested")]);
  const sa = A.scene({ task: al.task, events: al.events.events });
  assert.equal(sa.phase, "allowed_unpaid");
  assert.ok(sa.notes.includes("M1 無付款能力"));
});

test("a reached deadline with no closing event only says so; the closing event decides", () => {
  const p = { purchase_order_id: "po-1", decision: { policy_verdict: "Ask" }, deadline_at: 1790726480 };
  const before = snapshot("task-a", askEvents(), [p], { served_at: 1790726470 });
  assert.deepEqual(A.scene({ task: before.task, events: before.events.events }).notes, []);
  const after = snapshot("task-a", askEvents(), [p], { served_at: 1790726490 });
  const sc = A.scene({ task: after.task, events: after.events.events });
  assert.equal(sc.phase, "waiting", "the browser never decides an expiry");
  assert.ok(sc.notes.includes("已達期限，等待後端確認"));
  const closed = snapshot("task-a", [...askEvents(), ev(4, "APPROVAL_EXPIRED")], [{ ...p, approval_closed: "expired" }], { served_at: 1790726490 });
  assert.equal(A.scene({ task: closed.task, events: closed.events.events }).phase, "expired");
  assert.equal(expected("approval-deadline-exact").approval_status, "expired");
});

test("cancel and CLI timeout after a Deny keep the Deny and its reason", () => {
  for (const end of ["TASK_CANCELLED", "TASK_TIMED_OUT"]) {
    const s = snapshot("task-b", [...denyEvents(), ev(4, end, { code: end === "TASK_CANCELLED" ? "HOST_CANCELLED" : "CLI_TIMEOUT" })]);
    const sc = A.scene({ task: s.task, events: s.events.events });
    assert.equal(sc.phase, "denied");
    assert.equal(sc.reason, deny.reason);
  }
  const w = snapshot("task-a", [...askEvents(), ev(4, "APPROVAL_WITHDRAWN"), ev(5, "TASK_CANCELLED", { code: "HOST_CANCELLED" })]);
  assert.equal(A.scene({ task: w.task, events: w.events.events }).phase, "withdrawn");
  assert.equal(expected("cancel-pending").approval_status, "withdrawn");
});

test("hostile strings stay strings; nothing reads as paid", () => {
  const evil = "<img src=x onerror=alert(1)>";
  const s = snapshot("task-a", [ev(1, "TASK_STARTED"), ev(2, evil), decided(3, "Settled", evil, evil)], [{ purchase_order_id: evil, resource_url: "javascript:alert(1)", amount: "1e9", decision: { policy_verdict: "Settled" } }]);
  const snap = { task: s.task, events: s.events.events };
  assert.equal(A.scene(snap).phase, "unknown", "an unknown verdict is unknown, never paid or settled");
  const rows = Object.fromEntries(A.inspector(snap));
  assert.equal(rows["amount（基本單位）"], A.UNKNOWN, "a non-integer amount is not shown as an amount");
  assert.ok(rows["付款"].startsWith("未付款"));
  assert.ok(A.timeline(snap)[1].type === evil, "kept verbatim as text; render.js writes it with textContent");
});

test("the Unity projection carries no token and follows the committed snapshot", () => {
  const s = A.newState();
  const gen = A.reset(s, "consumer-a", "task-a");
  const { task, events } = snapshot("task-a", askEvents());
  A.commit(s, gen, task, events);
  const m = A.projection(s, false);
  assert.deepEqual(Object.keys(m).sort(), ["animate", "clear", "consumer", "gen", "phase", "reason", "seq", "task_id", "v", "verdict"]);
  assert.equal(m.v, A.BRIDGE_VERSION);
  assert.equal(m.gen, gen);
  assert.equal(m.seq, 3);
  assert.equal(m.phase, "waiting");
  assert.equal(m.verdict, "Ask");
  assert.equal(m.animate, false);
  assert.deepEqual(A.clearMessage(gen + 1), { v: A.BRIDGE_VERSION, gen: gen + 1, clear: true });
  const g2 = A.bump(s);
  assert.equal(A.projection(s, true).gen, g2, "a replay is a new generation for the shop");
  assert.ok(s.snapshot, "a replay keeps the snapshot");
});

test("amount keeps the integer string", () => {
  assert.equal(A.amount("500000"), "500000");
  assert.equal(A.amount("0.5"), A.UNKNOWN);
  assert.equal(A.amount(500000), A.UNKNOWN);
});
