// Tests of the audience window (docs/commercial-gap2.md): each customer's
// five steps in plain words, and the host page's link to the window.
// Run: node --test web/test/show.test.mjs
import { test } from "node:test";
import assert from "node:assert/strict";
import * as A from "../adapter.js";
import { createRelay, listen } from "../showlink.js";

let n = 0;
const ev = (seq, type, extra = {}) => ({ event_id: "e" + n++, event_type: type, sequence: seq, evidence_mode: "fixture", source: "host", observed_at: 1790726450 + seq, purchase_order_id: "po-1", ...extra });
const decided = (verdict, reason) => ev(3, "POLICY_DECIDED", { decision: { version: A.V1, policy_verdict: verdict, reason, approval_status: "", payment_status: "" } });
const base = (verdict, reason) => [ev(1, "TASK_STARTED", { purchase_order_id: "" }), ev(2, "PURCHASE_PROPOSED"), decided(verdict, reason)];
const snap = (events, { cap = "base_sepolia", purchase = {} } = {}) => ({
  task: { task_id: "t-1", consumer_id: "consumer-a", status: "running", as_of_sequence: events.length, served_at: 1790726470, contract_version: A.V1, payment_capability: cap,
    purchases: [{ purchase_order_id: "po-1", amount: "500000", ...purchase }] }, events });
const DLG = { task_budget: "5000000", allow_max: "100000" };
const steps = (m) => m.steps.map((s) => s.state).join(" ");
const texts = (m) => m.steps.map((s) => s.text);
const pay = (seq, type) => ev(seq, type);
const paid = [pay(4, "PAYMENT_REQUESTED"), pay(5, "BUDGET_RESERVED"), pay(6, "PAYMENT_SIGNED"), pay(7, "PAYMENT_SUBMITTED"), pay(8, "PAYMENT_SETTLED"), pay(9, "RESOURCE_DELIVERED"), pay(10, "RECEIPT_ISSUED")];

test("an Allow paid and delivered: five steps done, in plain words", () => {
  const m = A.showCustomer("consumer-a", snap([...base("Allow", "WITHIN_ALLOW_MAX"), ...paid]), DLG, { authenticator: "device" });
  assert.equal(m.name, "客人 A");
  assert.equal(steps(m), "done done done done done");
  assert.deepEqual(texts(m), ["客人交代 Agent：預算 5.00 USDC，超過 0.10 USDC 要問本人", "Agent 選好商品，報價 0.50 USDC", "在委託範圍內，自動放行",
    "已付款：預留與付款，兩筆鏈上交易", "已交付，附收據"]);
  assert.equal(m.caption, "客人 A・交付：已交付，附收據");
  assert.equal(m.amount, "0.50 USDC");
  assert.ok(!/[A-Z_]{6,}|500000/.test(JSON.stringify(m)), "no code and no base units for the audience");
});

test("in flight: paying, then confirming; never paid before the chain says so", () => {
  const at = (k) => A.showCustomer("consumer-a", snap([...base("Allow", "WITHIN_ALLOW_MAX"), ...paid.slice(0, k)]), DLG);
  assert.equal(steps(at(0)), "done done done now todo");
  assert.equal(at(0).steps[3].text, "準備付款");
  assert.equal(at(2).steps[3].text, "預留預算中（第 1 筆鏈上交易）");
  assert.equal(at(4).steps[3].text, "已送出付款，等鏈上確認（第 2 筆）");
  assert.equal(steps(at(5)), "done done done done now");
  assert.equal(at(5).steps[4].text, "付款完成，正在交付");
});

test("an Ask: waiting, approved by device with the voice, a virtual authenticator as such", () => {
  const ask = base("Ask", "HUMAN_APPROVAL_REQUIRED");
  const w = A.showCustomer("consumer-a", snap(ask), DLG, { authenticator: "device" });
  assert.equal(steps(w), "done done now todo todo");
  assert.equal(w.caption, "客人 A・核准：超過自動範圍，等本人核准");
  // Who approves, as the host can (review 4): the host plays the approver, or nobody can.
  assert.equal(A.showCustomer("consumer-a", snap(ask), DLG, {}).steps[2].text, "超過自動範圍，等核准（示範：由主持人代為核准）");
  const m1 = snap(ask, { cap: "unavailable" });
  m1.task.contract_version = "";
  assert.equal(A.showCustomer("consumer-a", m1, DLG, {}).steps[2].text, "超過自動範圍；這場展示無法核准");
  const approval = { state: "approved", resolved_by: "webauthn:approver-a", grant: { authorization_method: "webauthn", voice: "verified" } };
  const ok = [...ask, ev(4, "APPROVED", { authorization_method: "webauthn" })];
  const dev = A.showCustomer("consumer-a", snap(ok, { purchase: { approval } }), DLG, { authenticator: "device" });
  assert.equal(dev.steps[2].text, "本人以已登記的裝置核准了，聲音也相符");
  assert.equal(steps(dev), "done done done now todo");
  const virt = A.showCustomer("consumer-a", snap(ok, { purchase: { approval } }), DLG, { authenticator: "virtual" });
  assert.match(virt.steps[2].text, /^本人以 virtual authenticator（測試用，不是實體裝置）核准了/);
  const degraded = A.showCustomer("consumer-a", snap(ok, { purchase: { approval: { ...approval, grant: { ...approval.grant, voice: "unavailable" } } } }), DLG);
  assert.match(degraded.steps[2].text, /語音無法使用，記為降級/);
  const sim = A.showCustomer("consumer-a", snap(ok, { cap: "unavailable", purchase: { approval: { state: "approved", resolved_by: "sim-approver-a", grant: { authorization_method: "simulated" } } } }), DLG, { authenticator: "device" });
  assert.equal(sim.steps[2].text, "主持人代為核准（示範，不是本人）");
  const paidAsk = A.showCustomer("consumer-a", snap([...ok, ...paid.map((e) => ({ ...e, sequence: e.sequence + 1 }))], { purchase: { approval } }), DLG);
  assert.equal(steps(paidAsk), "done done done done done");
});

test("a refusal stops at the approval step; nothing is paid", () => {
  const deny = A.showCustomer("consumer-a", snap(base("Deny", "INSUFFICIENT_BUDGET")), DLG);
  assert.equal(steps(deny), "done done fail off todo");
  assert.deepEqual(texts(deny).slice(2, 4), ["超出預算，已拒絕", "這筆沒有付款（沒有花錢）"]);
  assert.equal(A.showCustomer("consumer-a", snap(base("Deny", "SOMETHING_NEW")), DLG).steps[2].text, "其他原因（主持人頁有細節），已拒絕");
  const ask = base("Ask", "HUMAN_APPROVAL_REQUIRED");
  const voice = A.showCustomer("consumer-a", snap([...ask, ev(4, "APPROVAL_DENIED")], { purchase: { closed_reason: "VOICE_CHALLENGE_FAILED" } }), DLG);
  assert.equal(voice.steps[2].text, "唸的內容或聲音不符，已拒絕");
  assert.equal(A.showCustomer("consumer-a", snap([...ask, ev(4, "APPROVAL_DENIED")]), DLG).steps[2].text, "本人拒絕了");
  assert.equal(A.showCustomer("consumer-a", snap([...ask, ev(4, "APPROVAL_EXPIRED")]), DLG).steps[2].text, "核准期限已過");
  const cancelled = A.showCustomer("consumer-a", snap([...ask, ev(4, "TASK_CANCELLED", { purchase_order_id: "" })]), DLG);
  assert.equal(cancelled.steps[2].state, "fail");
});

test("a task's end stops what had not started; a payment under way goes on", () => {
  const end = (k) => ev(20, "TASK_TIMED_OUT", { purchase_order_id: "" });
  const sent = A.showCustomer("consumer-a", snap([...base("Allow", "WITHIN_ALLOW_MAX"), ...paid.slice(0, 4), end()]), DLG);
  assert.equal(steps(sent), "done done done now todo");
  assert.equal(sent.steps[3].text, "已送出付款，等鏈上確認（第 2 筆）（任務已結束，付款照常完成）");
  const delivering = A.showCustomer("consumer-a", snap([...base("Allow", "WITHIN_ALLOW_MAX"), ...paid.slice(0, 5), end()]), DLG);
  assert.equal(steps(delivering), "done done done done now");
  assert.equal(delivering.steps[4].text, "付款完成，正在交付（任務已結束，交付照常進行）");
  const before = A.showCustomer("consumer-a", snap([...base("Allow", "WITHIN_ALLOW_MAX"), end()]), DLG);
  assert.deepEqual([before.steps[3].state, before.steps[3].text], ["fail", "任務逾時結束"]);
});

test("amounts are never rounded; any consumer has a name", () => {
  assert.equal(A.usdc("500000"), "0.50 USDC");
  assert.equal(A.usdc("104000"), "0.104 USDC");
  assert.equal(A.usdc("5000000"), "5.00 USDC");
  assert.equal(A.usdc("1"), "0.000001 USDC");
  assert.equal(A.usdc("x"), "金額未提供");
  assert.equal(A.showCustomer("consumer-c", null, null).name, "客人 C");
  assert.equal(A.plainReason("AUTHORIZATION_EXPIRED"), "授權或報價已過期");
});

test("an unknown result is never shown as done", () => {
  const review = A.showCustomer("consumer-a", snap([...base("Allow", "WITHIN_ALLOW_MAX"), pay(4, "PAYMENT_REQUESTED"), pay(5, "BUDGET_RESERVED"), pay(6, "PAYMENT_NEEDS_REVIEW")]), DLG);
  assert.equal(steps(review), "done done done unknown todo");
  assert.equal(review.steps[3].text, "付款結果未確認，待人工處理");
  const notPaid = A.showCustomer("consumer-a", snap([...base("Allow", "WITHIN_ALLOW_MAX"), pay(4, "PAYMENT_REQUESTED"), pay(5, "PAYMENT_NOT_SETTLED")]), DLG);
  assert.equal(notPaid.steps[3].state, "fail");
  const failedDelivery = A.showCustomer("consumer-a", snap([...base("Allow", "WITHIN_ALLOW_MAX"), ...paid.slice(0, 5), pay(9, "PAYMENT_NEEDS_REVIEW")]), DLG);
  assert.equal(steps(failedDelivery), "done done done done fail");
  const odd = A.showCustomer("consumer-a", snap([...base("Maybe", "")]), DLG);
  assert.equal(odd.steps[2].state, "unknown");
});

test("a host that does not pay says so; before the start, the first step waits", () => {
  const m = A.showCustomer("consumer-a", snap(base("Allow", "WITHIN_ALLOW_MAX"), { cap: "unavailable" }), DLG);
  assert.equal(steps(m), "done done done off off");
  assert.equal(m.steps[3].text, "這場展示不付款");
  const none = A.showCustomer("consumer-b", null, null);
  assert.equal(none.name, "客人 B");
  assert.equal(steps(none), "now todo todo todo todo");
  assert.equal(A.showCustomer("consumer-a", snap(base("Allow", "WITHIN_ALLOW_MAX")), null).steps[0].text, "客人已交代 Agent 預算與範圍");
  assert.equal(A.networkText("base_sepolia"), "Base Sepolia 測試網路・測試 USDC（不是真錢）");
  assert.equal(A.networkText("local_anvil"), "本機測試鏈・測試 USDC（不是真錢）");
  assert.equal(A.networkText(""), "這場展示不付款");
});

// A channel pair that delivers at once, as two windows' BroadcastChannels would.
function channels() {
  const all = [];
  const make = () => {
    const c = { onmessage: null, postMessage: (m) => { for (const o of all) if (o !== c && o.onmessage) o.onmessage({ data: structuredClone(m) }); } };
    all.push(c);
    return c;
  };
  return make;
}

test("the host page's relay follows one audience window; reports only while it owns the scene", () => {
  const make = channels();
  let t = 1000;
  const timers = [];
  const got = [];
  const relay = createRelay((k, d) => got.push([k, d]), { channel: make(), now: () => t, setTimer: (f) => timers.push(f) });
  const seen = { own: [], send2: [], select2: [], view: [] };
  const show = listen({ own: (on) => seen.own.push(on), send2: (m) => seen.send2.push(m), select2: (m) => seen.select2.push(m), view: (v) => seen.view.push(v) },
    { channel: make(), setTimer: (f) => timers.push(f), onLeave: () => {}, now: () => t });
  assert.equal(relay.connected, true); // the window said who it is
  assert.equal(relay.ready, false);
  assert.equal(show.hostThere, true); // the relay answered
  show.status({ ready: true, supports2: true, select: true });
  assert.equal(relay.ready, true);
  assert.deepEqual(got.map(([k, d]) => k + ":" + (d && d.ready)), ["show:false", "show:true"]);
  relay.send2({ x: 1 });
  assert.equal(seen.send2.length, 0); // not owned yet: nothing goes
  relay.own(true);
  relay.send2({ x: 2 });
  relay.select2({ consumer: "consumer-a" });
  assert.deepEqual([seen.own, seen.send2, seen.select2.length], [[true], [{ x: 2 }], 1]);
  show.report("rendered2", { consumer: "consumer-a" });
  assert.deepEqual(got.at(-1), ["rendered2", { consumer: "consumer-a" }]);
  relay.own(false);
  show.report("rendered2", { consumer: "consumer-a" });
  assert.equal(got.filter(([k]) => k === "rendered2").length, 1); // not owned: dropped
  relay.view({ a: 1 });
  relay.view({ a: 1 });
  assert.equal(seen.view.length, 1); // unchanged: not sent again
  t += 7000;
  for (const f of timers.slice(0, 1)) f(); // the relay's check: no word for too long
  assert.equal(relay.connected, false);
  assert.deepEqual(got.at(-1), ["show", null]);
  assert.equal(show.hostThere, false);
});

test("a window that says goodbye is gone; a report of another window is dropped", () => {
  const make = channels();
  const got = [];
  const relay = createRelay((k, d) => got.push([k, d]), { channel: make(), setTimer: () => {} });
  let leave = null;
  const show = listen({ own() {}, send2() {}, select2() {}, view() {} }, { channel: make(), setTimer: () => {}, onLeave: (f) => { leave = f; } });
  show.status({ ready: true, supports2: true });
  relay.own(true);
  const other = make();
  other.postMessage({ from: "show", id: "someone-else", kind: "rendered2", r: { consumer: "consumer-a" } });
  assert.equal(got.filter(([k]) => k === "rendered2").length, 0);
  leave();
  assert.equal(relay.connected, false);
  assert.deepEqual(got.at(-1), ["show", null]);
});

test("one window at a time: a second one is told the host page follows another", () => {
  const make = channels();
  let t = 1000;
  const got = [];
  const relay = createRelay((k, d) => got.push([k, d && d.id]), { channel: make(), now: () => t, setTimer: () => {} });
  const beats = [];
  const open = () => listen({ own() {}, send2() {}, select2() {}, view() {} }, { channel: make(), setTimer: (f) => beats.push(f), onLeave: () => {}, now: () => t });
  const first = open();
  first.status({ ready: true, supports2: true });
  const shows = got.filter(([k]) => k === "show").length;
  const second = open();
  second.status({ ready: true, supports2: true });
  for (let i = 0; i < 3; i++) { t += 2000; for (const f of beats) f(); } // both beat; the first stays followed
  assert.equal(got.filter(([k]) => k === "show").length, shows, "no restart while the followed window is alive");
  assert.equal(second.otherWindow, true);
  assert.equal(first.otherWindow, false);
});
