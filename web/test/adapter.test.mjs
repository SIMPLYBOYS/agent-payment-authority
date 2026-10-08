// Unit tests of web/adapter.js (node --test web/test/). Expected reasons
// come from the accepted testdata/contracts/cases.json, never from the
// Policy under test.
import { test } from "node:test";
import assert from "node:assert/strict";
import { readFileSync } from "node:fs";
import vm from "node:vm";
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
const PO = { purchase_order_id: "po-1" };
const askEvents = () => [ev(1, "TASK_STARTED"), ev(2, "PURCHASE_PROPOSED", PO), decided(3, "Ask", ask.reason, ask.approval_status)];
const denyEvents = () => [ev(1, "TASK_STARTED"), ev(2, "PURCHASE_PROPOSED", PO), decided(3, "Deny", deny.reason, deny.approval_status)];

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
  const closed = snapshot("task-a", [...askEvents(), ev(4, "APPROVAL_EXPIRED", PO)], [{ ...p, approval_closed: "expired" }], { served_at: 1790726490 });
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
  const w = snapshot("task-a", [...askEvents(), ev(4, "APPROVAL_WITHDRAWN", PO), ev(5, "TASK_CANCELLED", { code: "HOST_CANCELLED" })]);
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
  assert.deepEqual(Object.keys(m).sort(), ["animate", "cause_seq", "clear", "consumer", "gen", "phase", "reason", "seq", "task_end", "task_id", "v", "verdict"]);
  assert.equal(m.cause_seq, 3, "the Ask's POLICY_DECIDED");
  assert.equal(m.task_end, "");
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

// R1: purchase events are tied to their own purchase. The two histories
// below were written through the real Store (internal/store/
// display_history_test.go): purchase 1 Ask then expired, purchase 2 Ask, in
// both orders. Purchase 2 is still waiting in both.
const histories = JSON.parse(readFileSync(new URL("../../testdata/demo/display-histories.json", import.meta.url)));
const fromStore = (name) => ({ task: histories[name].task, events: histories[name].events.events });

test("R1: an older purchase's expiry does not move the newer Ask (Store-built, both orders)", () => {
  for (const name of ["expired_then_ask", "ask_ask_then_expire"]) {
    const snap = fromStore(name);
    assert.equal(snap.task.status, "awaiting_approval", name);
    const sc = A.scene(snap);
    assert.equal(sc.phase, "waiting", name + ": the shown purchase is still an open Ask");
    assert.equal(sc.po, "po-task-a-2", name);
    assert.equal(sc.verdict, "Ask", name);
    assert.equal(sc.reason, ask.reason, name);
    const s = A.newState();
    const gen = A.reset(s, "consumer-a", "task-a");
    assert.equal(A.commit(s, gen, histories[name].task, histories[name].events), "ok");
    const m = A.projection(s, false);
    assert.deepEqual([m.phase, m.verdict, m.reason], ["waiting", "Ask", ask.reason], name + ": the projection is the shown purchase's");
    assert.equal(A.inspector(snap).filter(([k]) => k === "— 請購").length, 2, "the Inspector still lists both purchases");
  }
});

test("R1: the shown purchase's own closing decides; unknown links never close anything", () => {
  const base = fromStore("expired_then_ask");
  const last = base.events[base.events.length - 1].sequence;
  const withEvent = (e) => ({ task: base.task, events: [...base.events, e] });
  assert.equal(A.scene(withEvent(ev(last + 1, "APPROVAL_EXPIRED", { purchase_order_id: "po-task-a-2" }))).phase, "expired");
  assert.equal(A.scene(withEvent(ev(last + 1, "APPROVAL_WITHDRAWN", { purchase_order_id: "po-task-a-2" }))).phase, "withdrawn");
  assert.equal(A.scene(withEvent(ev(last + 1, "APPROVAL_EXPIRED"))).phase, "waiting", "a closing without a purchase id is ignored");
  assert.equal(A.scene(withEvent(ev(last + 1, "APPROVAL_EXPIRED", { purchase_order_id: "po-unknown" }))).phase, "waiting", "a closing of another purchase is ignored");
  const orphan = { task: base.task, events: [ev(1, "TASK_STARTED"), decided(2, "Allow", "x", "not_requested")] };
  orphan.events[1].purchase_order_id = undefined;
  assert.equal(A.scene(orphan).phase, "unknown", "a decision with no purchase is not guessed into a purchase");
});

// ---- D1 (docs/demo-presentation-design.md 4.4, 4.5, 6.2, 7) ----
const P = (id, extra = {}) => ({ purchase_order_id: id, request_id: "req-" + id, http_method: "GET", resource_url: "http://127.0.0.1:1/resources/kr-market?edition=1",
  chain_id: "eip155:84532", asset_id: "fixture-usdc", pay_to: "0x3333", amount: "500000", intent_hash: "sha256:i-" + id, payment_requirement_hash: "sha256:q-" + id, ...extra });
const askOf = (po) => ({ policy_verdict: "Ask", reason: "HUMAN_APPROVAL_REQUIRED", approval_status: "pending", payment_status: "unavailable" });
// A stored task whose one Ask later expired: the final record says expired.
function expiredAsk() {
  const evs = [ev(1, "TASK_STARTED"), ev(2, "PURCHASE_PROPOSED", { purchase_order_id: "po-1", intent_hash: "sha256:i-po-1" }),
    ev(3, "POLICY_DECIDED", { purchase_order_id: "po-1", decision: askOf() }), ev(4, "APPROVAL_EXPIRED", { purchase_order_id: "po-1", decision: { ...askOf(), approval_status: "expired", reason: "APPROVAL_TIMEOUT" } })];
  const p = P("po-1", { decision: askOf(), decided_at: 1790726453, deadline_at: 1790726483, approval_closed: "expired", closed_at: 1790726454 });
  // served_at far past the deadline: the present must not reach into a replay
  return { task: { ...snapshot("task-a", evs, [p]).task, status: "running", served_at: 1790999999 }, events: evs };
}
const ctxLive = { mode: "live", catalog: null };
const ctxAt = (n, m) => ({ mode: "replay", n, m, catalog: null });

test("D1 stepView: a replay step shows nothing the replay has not reached (R1)", () => {
  const snap = expiredAsk();
  const at = (n) => A.stepView(snap, n);
  assert.equal(at(1).task.purchases.length, 0, "#1: no purchase yet");
  const s2 = at(2).task.purchases[0];
  assert.equal(s2.intent_hash, "sha256:i-po-1");
  assert.ok(!("decision" in s2) && !("deadline_at" in s2) && !("approval_closed" in s2), "#2: fixed fields only");
  const s3 = at(3);
  assert.equal(s3.task.purchases[0].decision.policy_verdict, "Ask");
  assert.equal(s3.task.purchases[0].deadline_at, 1790726483);
  assert.ok(!("approval_closed" in s3.task.purchases[0]), "#3: not expired yet, though the final record is");
  assert.equal(s3.task.status, "awaiting_approval");
  assert.equal(s3.task.derived, true);
  assert.ok(!("served_at" in s3.task), "no present time in a replay step");
  const c3 = A.card(s3, null, ctxAt(3, 4));
  assert.equal(c3.status, "Ask：等待人工核准（M1 無核准能力）", "#3: no 已到期 and no 已達期限, the browser's time is ignored");
  assert.ok(c3.task.includes("依已回放事件推導"));
  assert.equal(c3.steps[5].state, "wait");
  assert.equal(A.scene(s3).phase, "waiting");
  assert.ok(!A.scene(s3).notes.includes("已達期限，等待後端確認"));
  const c4 = A.card(at(4), null, ctxAt(4, 4));
  assert.equal(c4.status, "Ask 已到期（後端已記錄）");
  assert.equal(c4.steps[5].note, "#4 APPROVAL_EXPIRED");
  const c2 = A.card(at(2), null, ctxAt(2, 4));
  assert.equal(c2.status, "已提出請購，尚無裁決");
  assert.equal(c2.steps[4].note, "尚未發生（回放位置 #2）");
  assert.ok(!c2.rows.some(([k]) => k === "裁決" || k === "原因" || k === "期限"));
  assert.equal(A.card(at(1), null, ctxAt(1, 4)).status, "此 task 尚無請購");
});

test("D1 stepView: a withdrawal and a task cancel appear only when replayed", () => {
  const evs = [ev(1, "TASK_STARTED"), ev(2, "PURCHASE_PROPOSED", { purchase_order_id: "po-1" }), ev(3, "POLICY_DECIDED", { purchase_order_id: "po-1", decision: askOf() }),
    ev(4, "APPROVAL_WITHDRAWN", { purchase_order_id: "po-1", decision: { ...askOf(), approval_status: "withdrawn", reason: "HOST_CANCELLED" } }), ev(5, "TASK_CANCELLED", { code: "HOST_CANCELLED" })];
  const snap = { task: { ...snapshot("task-a", evs, [P("po-1", { decision: askOf(), deadline_at: 9, approval_closed: "withdrawn", closed_at: 7 })]).task, status: "cancelled" }, events: evs };
  assert.equal(A.stepView(snap, 3).task.status, "awaiting_approval");
  assert.equal(A.card(A.stepView(snap, 3), null, ctxAt(3, 5)).status, "Ask：等待人工核准（M1 無核准能力）");
  assert.equal(A.card(A.stepView(snap, 4), null, ctxAt(4, 5)).status, "Ask 已撤回（task 取消）");
  assert.equal(A.stepView(snap, 4).task.status, "running");
  assert.equal(A.stepView(snap, 5).task.status, "cancelled");
  assert.ok(!A.card(A.stepView(snap, 4), null, ctxAt(4, 5)).task.includes("task 已取消"));
  assert.ok(A.card(A.stepView(snap, 5), null, ctxAt(5, 5)).task.includes("task 已取消"));
});

test("D1 stepView: a later purchase does not reach an earlier step", () => {
  const evs = [ev(1, "TASK_STARTED"), ev(2, "PURCHASE_PROPOSED", { purchase_order_id: "po-A" }), ev(3, "POLICY_DECIDED", { purchase_order_id: "po-A", decision: askOf() }),
    ev(4, "APPROVAL_EXPIRED", { purchase_order_id: "po-A", decision: { ...askOf(), approval_status: "expired" } }),
    ev(5, "PURCHASE_PROPOSED", { purchase_order_id: "po-B" }), ev(6, "POLICY_DECIDED", { purchase_order_id: "po-B", decision: { policy_verdict: "Deny", reason: "INSUFFICIENT_BUDGET", approval_status: "not_requested", payment_status: "unavailable" } })];
  const snap = { task: snapshot("task-a", evs, [P("po-A", { decision: askOf(), deadline_at: 9, approval_closed: "expired" }),
    P("po-B", { decision: { policy_verdict: "Deny", reason: "INSUFFICIENT_BUDGET" } })]).task, events: evs };
  const s4 = A.stepView(snap, 4);
  assert.deepEqual(s4.task.purchases.map((p) => p.purchase_order_id), ["po-A"]);
  const c4 = A.card(s4, "po-B", ctxAt(4, 6));
  assert.equal(c4.po, "po-A", "a replay follows the scene; a hand pick does not apply");
  assert.deepEqual(c4.options, [], "no hand pick in a replay");
  assert.ok(!JSON.stringify(c4).includes("po-B") && !JSON.stringify(c4).includes("INSUFFICIENT_BUDGET"));
  assert.equal(s4.task.status, "running");
  assert.equal(A.stepView(snap, 6).task.status, "denied");
});

test("D1 card (live): the scene's purchase by default; a hand pick is marked as different, not as a sync failure", () => {
  const evs = [ev(1, "TASK_STARTED"), ev(2, "PURCHASE_PROPOSED", { purchase_order_id: "po-A" }), ev(3, "POLICY_DECIDED", { purchase_order_id: "po-A", decision: askOf() }),
    ev(4, "APPROVAL_EXPIRED", { purchase_order_id: "po-A" }), ev(5, "PURCHASE_PROPOSED", { purchase_order_id: "po-B" }), ev(6, "POLICY_DECIDED", { purchase_order_id: "po-B", decision: askOf() })];
  const snap = { task: snapshot("task-a", evs, [P("po-A", { decision: askOf(), deadline_at: 1790726000, approval_closed: "expired", closed_at: 1 }), P("po-B", { decision: askOf(), deadline_at: 1790726460 })]).task, events: evs };
  const byDefault = A.card(snap, null, ctxLive);
  assert.equal(byDefault.po, "po-B");
  assert.equal(byDefault.differs, false);
  assert.deepEqual(byDefault.options, ["po-A", "po-B"]);
  assert.ok(byDefault.status.endsWith("已達期限，等待後端確認"), "live: served_at reached po-B's deadline, no closing yet");
  const picked = A.card(snap, "po-A", ctxLive);
  assert.equal(picked.po, "po-A");
  assert.equal(picked.scenePo, "po-B");
  assert.equal(picked.differs, true);
  assert.equal(picked.status, "Ask 已到期（後端已記錄）");
  assert.equal(A.card(snap, "po-unknown", ctxLive).po, "po-B", "an unknown pick follows the scene");
  const ev2 = A.evidence(snap, "po-A", { ...ctxLive, sent: { gen: 3, task: "task-a", seq: 6, phase: "waiting", po: "po-B" }, report: { gen: 3, task: "task-a", seq: 6, phase: "waiting" } });
  const sceneRows = ev2.sections.find((x) => x.title === "場景對應").rows;
  assert.ok(sceneRows.some(([k, v]) => k === "同步" && v.startsWith("Unity 已呈現")), "a different pick is not a sync failure");
  assert.ok(sceneRows.some(([k, v]) => k === "選取" && v.includes("選取不同，不是同步失敗")));
  assert.ok(sceneRows.some(([k, v, src]) => k === "對應請購" && v === "po-B（網頁推導）" && src === "網頁推導"));
});

test("D1 evidence: the full record in a replay says so and marks what is not reached; sources are named", () => {
  const snap = expiredAsk();
  const e = A.evidence(snap, "po-1", { ...ctxAt(2, 4), sent: { gen: 5, task: "task-a", seq: 2, phase: "quoted", po: "po-1" }, report: { gen: 5, task: "task-a", seq: 1, phase: "entered" } });
  assert.equal(e.banner, "完整紀錄，非回放當步（回放位置 #2／共 4）");
  const rows = e.sections.find((x) => x.title === "Store 事件").rows;
  assert.ok(rows.find(([k]) => k.startsWith("#4"))[1].includes("回放尚未到此"));
  assert.ok(!rows.find(([k]) => k.startsWith("#2"))[1].includes("回放尚未到此"));
  assert.ok(e.sections.find((x) => x.title === "場景對應").rows.some(([k, v]) => k === "同步" && v.startsWith("Unity 尚未一致：序號較舊") && v.endsWith("（不是同步完成）")));
  for (const sec of e.sections) for (const r of sec.rows) assert.equal(r.length, 3, "every row names its source");
  const cat = { products: [{ resource_url: "http://127.0.0.1:1/resources/kr-market?edition=1", price: { asset_display: "0.50 test USDC", fiat_amount: "700", fiat_currency: "KRW", fx_rate: "1400", fx_source: "fixture, not live FX" } }] };
  const c = A.card(snap, null, { mode: "live", catalog: cat, catalogAt: "12:00:00" });
  const amountRows = c.rows.filter(([k]) => k === "402 綁定金額" || k === "目錄標示");
  assert.equal(amountRows.length, 2, "the bound amount and the catalog's fixture price are two rows");
  assert.ok(amountRows[1][1].includes("700 KRW") && amountRows[1][2].includes("不綁定此請購"));
  assert.ok(!/付款成功|已付款|通過|已驗證/.test(JSON.stringify(c) + JSON.stringify(e)));
});

test("D1 project derives the status by the Store's rule: a Deny stays after a cancel", () => {
  const evs = [ev(1, "TASK_STARTED"), ev(2, "PURCHASE_PROPOSED", { purchase_order_id: "po-1" }), decided(3, "Deny", "INSUFFICIENT_BUDGET", "not_requested"), ev(4, "TASK_CANCELLED")];
  assert.deepEqual(A.project(evs), { status: "denied", reason: "INSUFFICIENT_BUDGET" });
  assert.deepEqual(A.project(evs.slice(0, 2)), { status: "running", reason: "" });
});

// Review of 7ffc45d, R1: in step only when Unity names the projection sent.
test("D1 evidence: Unity is in step only for the same generation, task, sequence and phase", () => {
  const snap = expiredAsk();
  const sent = { gen: 7, task: "task-a", seq: 4, phase: "expired", po: "po-1" };
  const sync = (report) => A.evidence(snap, "po-1", { mode: "live", catalog: null, sent, report }).sections.find((x) => x.title === "場景對應").rows.find(([k]) => k === "同步")[1];
  assert.equal(sync({ gen: 7, task: "task-a", seq: 4, phase: "expired" }), "Unity 已呈現 task-a #4（expired）");
  const wrongTask = sync({ gen: 7, task: "task-b", seq: 4, phase: "expired" });
  assert.ok(wrongTask.startsWith("Unity 尚未一致") && wrongTask.includes("task 不同（Unity task-b）") && wrongTask.endsWith("（不是同步完成）"), wrongTask);
  const wrongPhase = sync({ gen: 7, task: "task-a", seq: 4, phase: "waiting" });
  assert.ok(wrongPhase.startsWith("Unity 尚未一致") && wrongPhase.includes("phase 不同（Unity waiting）"), wrongPhase);
  const older = sync({ gen: 7, task: "task-a", seq: 3, phase: "waiting" });
  assert.ok(older.includes("序號較舊（Unity #3，送出 #4）") && !older.includes("已呈現"), older);
  assert.ok(sync({ gen: 6, task: "task-a", seq: 4, phase: "expired" }).includes("世代不同"));
  assert.equal(sync(null), "Unity 尚未回報：尚未一致");
  assert.ok(A.evidence(snap, "po-1", { mode: "live", catalog: null, sent, report: { gen: 7, task: "task-a", seq: 4, phase: "expired" } })
    .sections.find((x) => x.title === "場景對應").rows.some(([k, v, src]) => k === "對應請購" && v === "po-1（網頁推導）" && src === "網頁推導"), "the purchase link stays the page's own");
});

// ---- D2 (docs/demo-presentation-design.md 16.4, 16.9 items 3, 4, 5, 10, 11, 13) ----

const vectors = JSON.parse(readFileSync(new URL("../../testdata/demo/scene-transitions.json", import.meta.url)));

test("D2 transition: the shared vectors give the same rules and labels as Unity's", () => {
  let steps = 0;
  for (const c of vectors.cases) {
    let t = null;
    for (const st of c.steps) {
      const tr = A.transition(t, st.m);
      assert.equal(tr.rule, st.rule, c.name + " #" + st.m.seq + " rule");
      assert.equal(tr.browse, st.browse, c.name + " #" + st.m.seq + " browse");
      assert.equal(tr.target.label, st.label, c.name + " #" + st.m.seq + " label");
      t = tr.target;
      steps++;
    }
  }
  assert.ok(vectors.cases.length > 10 && steps > 20);
});

test("D2 projection: cause_seq names the event behind the phase; task_end the first ending", () => {
  const msg = (events) => {
    const s = A.newState();
    const gen = A.reset(s, "consumer-a", "task-a");
    const { task, events: e } = snapshot("task-a", events);
    assert.equal(A.commit(s, gen, task, e), "ok");
    return A.projection(s, true);
  };
  const at = (m) => [m.phase, m.cause_seq, m.task_end, m.seq];
  assert.deepEqual(at(msg([ev(1, "TASK_STARTED")])), ["entered", 1, "", 1]);
  assert.deepEqual(at(msg([ev(1, "TASK_STARTED"), ev(2, "PURCHASE_PROPOSED", PO)])), ["quoted", 2, "", 2]);
  assert.deepEqual(at(msg(denyEvents())), ["denied", 3, "", 3]);
  assert.deepEqual(at(msg([...askEvents(), ev(4, "TASK_TIMED_OUT")])), ["waiting", 3, "timed_out", 4], "11.1: the Ask stays pending");
  const expiredThenCancelled = [...askEvents(), ev(4, "APPROVAL_EXPIRED", PO), ev(5, "TASK_CANCELLED")];
  assert.deepEqual(at(msg(expiredThenCancelled)), ["expired", 4, "cancelled", 5], "item 4: the expiry's number, not the cancel's");
  assert.deepEqual(at(msg([...askEvents(), ev(4, "APPROVAL_WITHDRAWN", PO), ev(5, "TASK_CANCELLED")])), ["withdrawn", 4, "cancelled", 5]);
  assert.deepEqual(at(msg([...denyEvents(), ev(4, "TASK_TIMED_OUT"), ev(5, "TASK_CANCELLED")])), ["denied", 3, "timed_out", 5], "the first ending");
  const orphan = [ev(1, "TASK_STARTED"), ev(2, "POLICY_DECIDED", { decision: { policy_verdict: "Ask" } })];
  assert.deepEqual(at(msg(orphan)), ["unknown", 0, "", 2], "no purchase: no number");
  assert.equal(A.shopLabel("expired", 4, "cancelled", ""), "ask expired (#4) - leaving");
  assert.equal(A.shopLabel("waiting", 0, "", ""), "ASK - waiting for a human", "unknown: no number");
});

test("D2 projection: every replay step of the Store-built histories sends fields in range", () => {
  for (const name of Object.keys(histories)) {
    const snap = fromStore(name);
    for (const e of snap.events) {
      const m = A.projection({ gen: 1, snapshot: A.stepView(snap, e.sequence) }, true);
      assert.equal(m.seq, e.sequence, name);
      assert.ok(Number.isInteger(m.cause_seq) && m.cause_seq >= 0 && m.cause_seq <= m.seq, name + " #" + e.sequence + " cause " + m.cause_seq);
      assert.ok(["", "cancelled", "timed_out"].includes(m.task_end), name);
    }
  }
});

test("D2 replay of a Store-built history: a second decision browses again, another purchase's expiry does not", () => {
  const steps = (name) => {
    const snap = fromStore(name);
    let t = null;
    return snap.events.map((e) => {
      const m = A.projection({ gen: 1, snapshot: A.stepView(snap, e.sequence) }, true);
      const tr = A.transition(t, m);
      t = tr.target;
      return [e.sequence, m.phase, m.cause_seq, tr.rule, tr.browse];
    });
  };
  assert.deepEqual(steps("ask_ask_then_expire"), [
    [1, "entered", 1, 1, false], [2, "quoted", 2, 1, true], [3, "waiting", 3, 1, false],
    [4, "waiting", 3, 4, false],  // purchase 2 proposed: the scene still shows purchase 1's Ask
    [5, "waiting", 5, 2, true],   // purchase 2's Ask: a new purchase
    [6, "waiting", 5, 4, false],  // purchase 1 expires: another purchase's event
  ]);
  assert.deepEqual(steps("expired_then_ask"), [
    [1, "entered", 1, 1, false], [2, "quoted", 2, 1, true], [3, "waiting", 3, 1, false], [4, "expired", 4, 1, false],
    [5, "expired", 4, 4, false],  // purchase 2 proposed: the scene keeps the latest decision's purchase
    [6, "waiting", 6, 1, true],   // purchase 2's Ask: back in from outside, browsing
  ]);
});

test("D2 transition text: display segments say so and name the recorded event", () => {
  const ask3 = { phase: "waiting", cause_seq: 3, seq: 3, task_end: "", reason: "HUMAN_APPROVAL_REQUIRED" };
  assert.equal(A.transition({ phase: "entered", cause: 1, end: "" }, ask3).text, "展示動畫：選商品 → 到櫃檯（沒有獨立事件）；裁決已在 #3 記錄");
  assert.equal(A.transition({ phase: "quoted", cause: 2, end: "" }, ask3).text, "展示動畫：到櫃檯（沒有獨立事件）；裁決已在 #3 記錄");
  const t3 = A.transition(null, ask3).target;
  assert.equal(A.transition(t3, { ...ask3, cause_seq: 6, seq: 6 }).text, "新的一筆請購：展示動畫：選商品 → 到櫃檯（沒有獨立事件）；裁決已在 #6 記錄");
  assert.equal(A.transition(t3, { ...ask3, seq: 4, task_end: "timed_out" }).text, "不移動，只換標籤：CLI 逾時，同一筆 Ask 仍未決");
  assert.equal(A.transition(t3, { ...ask3, seq: 4 }).text, "不移動、不重播：場景的請購未變（快照 #4）");
  assert.equal(A.transition(t3, { phase: "expired", cause_seq: 4, seq: 4, task_end: "" }).text, "離店；到期已在 #4 記錄");
  assert.equal(A.transition(null, { phase: "entered", cause_seq: 1, seq: 1, task_end: "" }).text, "進店；進店事件已在 #1 記錄");
  assert.equal(A.transition(null, { ...ask3, cause_seq: 0 }).text, "展示動畫：選商品 → 到櫃檯（沒有獨立事件）；裁決已記錄（序號未知）");
});

test("D2 evidence: the scene page lists this move, derived by the page from the transition table", () => {
  const snap = { task: snapshot("task-a", askEvents(), [P("po-1", { decision: askOf("po-1") })]).task, events: askEvents() };
  const row = (motion) => A.evidence(snap, "po-1", { ...ctxLive, sent: null, report: null, motion }).sections.find((x) => x.title === "場景對應").rows.find(([k]) => k === "這次過場");
  assert.deepEqual(row(null), ["這次過場", "—", "網頁依轉換表推導"]);
  const tr = A.transition({ phase: "entered", cause: 1, end: "" }, A.projection({ gen: 1, snapshot: snap }, true));
  assert.deepEqual(row({ rule: tr.rule, text: tr.text }), ["這次過場", "展示動畫：選商品 → 到櫃檯（沒有獨立事件）；裁決已在 #3 記錄（規則 1）", "網頁依轉換表推導"]);
});

// Review of d7070af: "補間中" ends only on Unity's report of exactly the
// projection last sent. The second test runs app.js's own report handler
// (the UnityHost callback, cut from the source as the review's probe did).
const SENT = { gen: 4, task: "task-a", seq: 3, phase: "waiting" };
const NOT_IT = [
  ["another task", { ...SENT, task: "task-b" }], ["another phase", { ...SENT, phase: "denied" }],
  ["a later sequence not sent", { ...SENT, seq: 4 }], ["an older sequence", { ...SENT, seq: 2 }], ["another generation", { ...SENT, gen: 5 }],
];

test("D2 sameShown: only the same generation, task, sequence and phase", () => {
  assert.equal(A.sameShown(SENT, { ...SENT, verdict: "Ask", reason: "HUMAN_APPROVAL_REQUIRED" }), true);
  for (const [name, rep] of NOT_IT) assert.equal(A.sameShown(SENT, rep), false, name);
  assert.equal(A.sameShown(null, SENT), false);
  assert.equal(A.sameShown(SENT, null), false);
});

test("D2 app.js: a report that is not the projection sent does not end the transition", () => {
  const src = readFileSync(new URL("../app.js", import.meta.url), "utf8");
  const begin = src.indexOf("const unity = new UnityHost(");
  const handler = src.slice(begin, src.indexOf("\n});", begin) + 4);
  assert.ok(begin > 0 && handler.includes("A.sameShown(motion, detail)"));
  const run = (detail) => {
    const box = { dataset: {}, textContent: "" };
    const c = { A, $: () => box, motion: { ...SENT, done: false }, rendered: [], lastReport: null, drawScene() {}, renderPanels() {}, shopReset() {},
      UnityHost: class { constructor(_canvas, fn) { c.report = fn; } } };
    vm.runInNewContext(handler, c);
    c.report("rendered", { verdict: "", reason: "", ...detail });
    return c.motion.done;
  };
  for (const [name, rep] of NOT_IT) assert.equal(run(rep), false, name + ": still in transition");
  assert.equal(run(SENT), true, "the exact report ends it");
});

// History mode (docs/cli-poc.md 54): a recorded real run. The card and the
// evidence carry the fixed label; the open Ask is the end-of-run snapshot,
// never a live wait or countdown; the tool calls come from the trace with
// the Store events they led to; nothing reads as live, paid or approved.
test("history: a recorded real run is labelled, its open Ask is a snapshot, its tool calls map to Store events", () => {
  const label = "真實執行紀錄・歷史回放・未付款・開發展示例外";
  const po = "po-task-a-kr-market";
  const evs = [ev(1, "TASK_STARTED"), ev(2, "PURCHASE_PROPOSED", { purchase_order_id: po }),
    ev(3, "POLICY_DECIDED", { purchase_order_id: po, decision: { policy_verdict: "Ask", reason: ask.reason, approval_status: "pending", payment_status: "unavailable" } })];
  const purchase = { purchase_order_id: po, amount: "500000", asset_id: "fixture-usdc", decided_at: 1790726453, deadline_at: 1790726483,
    decision: { policy_verdict: "Ask", reason: ask.reason, approval_status: "pending", payment_status: "unavailable" } };
  const stored = snapshot("task-a", evs, [purchase], { served_at: 1790726453 }); // the host's frozen clock: the last event
  const snap = { task: stored.task, events: stored.events.events };
  const history = { record: "live_history", label, as_of: 1790726453, trace: { task_id: "task-a", gate_mode: "dev_demo_plugins", gate: "passed", outcome: "ok",
    tool_calls: [{ n: 1, tool: "list_products", product_id: "", outcome: "ok" }, { n: 2, tool: "get_quote", product_id: "kr-market", outcome: "ok" },
      { n: 3, tool: "request_purchase", product_id: "kr-market", outcome: "Ask", purchase_order_id: po, event_sequences: [2, 3] }] } };

  const c = A.card(snap, null, { mode: "live", history });
  assert.equal(c.history, label);
  assert.ok(c.status.includes("歷史快照") && !c.status.includes("已達期限"));
  assert.ok(c.steps.find((s) => s.label === "收尾").note.includes("歷史快照"));
  assert.ok(c.steps.find((s) => s.label === "選商品").note.includes("真實執行 trace"));
  assert.ok(!JSON.stringify(c).includes("fake 腳本"));

  const e = A.evidence(snap, po, { mode: "live", history });
  assert.ok(e.banner.startsWith(label) && e.banner.includes("非即時"));
  const tools = e.sections.find((x) => x.title === "工具呼叫（真實執行 trace）");
  assert.equal(tools.rows.length, 3);
  assert.ok(tools.rows[2][0] === "#3 request_purchase" && tools.rows[2][1].includes(po) && tools.rows[2][1].includes("#2、#3"));
  for (const sec of e.sections) for (const r of sec.rows) assert.equal(r.length, 3, "every row names its source");

  // In a replay, the calls whose events are not reached yet say so.
  const r = A.evidence(snap, po, { ...ctxAt(2, 3), history });
  assert.ok(r.sections.find((x) => x.title === "工具呼叫（真實執行 trace）").rows[2][1].includes("回放尚未到此"));

  // Without history mode nothing changes; a trace of another task is not shown.
  assert.ok(!A.evidence(snap, po, { mode: "live" }).sections.some((x) => x.title.startsWith("工具呼叫")));
  const other = A.evidence(snap, po, { mode: "live", history: { ...history, trace: { ...history.trace, task_id: "task-b" } } });
  assert.equal(other.sections.find((x) => x.title.startsWith("工具呼叫")).rows[0][1], "未提供 trace（host 未指定 --history-trace）");
  assert.ok(!/付款成功|已付款|已核准|即時倒數/.test(JSON.stringify(c) + JSON.stringify(e)));
});
