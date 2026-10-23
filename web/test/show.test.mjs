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

test("two customers: a payment ready while another is in progress is behind it; the window says payments go in order", () => {
  const at = (c, k) => A.showCustomer(c, snap([...base("Allow", "WITHIN_ALLOW_MAX"), ...paid.slice(0, k)]), DLG);
  const q = A.showQueue([at("consumer-a", 2), at("consumer-b", 0)], "local_anvil");
  assert.equal(q.note, "客人同時選購；付款由 Host 依序處理，一次處理一筆；一筆等鏈上確認太久時，會先處理下一筆。");
  assert.equal(q.models[0].steps[3].text, "預留預算中（第 1 筆鏈上交易）");
  assert.equal(q.models[1].steps[3].text, "排在前一筆付款之後（Host 依序處理付款）");
  assert.equal(q.models[1].caption, "客人 B・付款：排在前一筆付款之後（Host 依序處理付款）");
  // A confirming, or A delivering: B is started only at the worker's next round (reviews 2, 3).
  assert.equal(A.showQueue([at("consumer-a", 4), at("consumer-b", 0)], "local_anvil").models[1].steps[3].text, "排在前一筆付款之後（Host 依序處理付款）");
  assert.equal(A.showQueue([at("consumer-a", 5), at("consumer-b", 0)], "local_anvil").models[1].steps[3].text, "排在前一筆付款之後（Host 依序處理付款）");
  // Nothing on its way (the other one is paid, or both only ready): "準備付款" as it is.
  assert.equal(A.showQueue([at("consumer-a", 7), at("consumer-b", 0)], "local_anvil").models[1].steps[3].text, "準備付款");
  assert.equal(A.showQueue([at("consumer-a", 0), at("consumer-b", 0)], "local_anvil").models[1].steps[3].text, "準備付款");
  // One customer shopping (the other's slot has no task yet), or a host that does not pay: no note, nothing changed.
  assert.deepEqual(A.showQueue([at("consumer-b", 0)], "local_anvil"), { models: [at("consumer-b", 0)], note: "" });
  const idle = A.showCustomer("consumer-b", null, DLG);
  assert.deepEqual(A.showQueue([at("consumer-a", 2), idle], "local_anvil"), { models: [at("consumer-a", 2), idle], note: "" });
  assert.equal(A.showQueue([at("consumer-a", 2), at("consumer-b", 0)], "unavailable").note, "");
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
  assert.equal(review.steps[3].text, "付款結果未確認"); // no code: not known
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

const TX1 = "0x" + "1".repeat(64), TX2 = "0x" + "2".repeat(64);
const delivered = { status: "paid", state: "delivered", reserve_tx: TX1, settle_tx: TX2, settle_block: 12, content_sha256: "sha256:" + "c".repeat(64), receipt_id: "rcpt-1" };

test("why trustworthy: each item only what a record says, links only on a public testnet", () => {
  const p = { resource_url: "https://shop.example/resources/kr-market", credential: { result: "valid" }, payment: delivered };
  const ev = A.showEvidence(snap([...base("Allow", "WITHIN_ALLOW_MAX"), ...paid], { purchase: p }), {});
  assert.deepEqual(ev.map((x) => x.label), ["委託", "憑證", "核准", "預算預留", "付款", "收據"]);
  assert.equal(ev[0].text, "主持人代客人確認（示範，不是本人）");
  assert.equal(ev[1].text, "Agent 出示了客人的憑證，檢查通過（測試發行者）");
  assert.equal(ev[3].href, "https://sepolia.basescan.org/tx/" + TX1);
  assert.equal(ev[3].text, "0x11111111…111111");
  assert.match(ev[4].note, /和第 1 筆不是同一筆/);
  assert.ok(!/驗證通過|已驗證|ZK 已/.test(JSON.stringify(ev)), "no 'verified' claim of the page's own");
  const local = A.showEvidence(snap([...base("Allow", "WITHIN_ALLOW_MAX"), ...paid], { cap: "local_anvil", purchase: p }), {});
  assert.equal(local[3].href, "");
  assert.match(local[3].text, /本機鏈，無法公開查詢/);
  const bad = A.showEvidence(snap(base("Allow", "WITHIN_ALLOW_MAX"), { purchase: { credential: { result: "revoked" } } }), {});
  assert.equal(bad[1].text, "憑證檢查沒通過");
  assert.deepEqual(A.showEvidence(null), []);
  const deny = A.showEvidence(snap(base("Deny", "INSUFFICIENT_BUDGET")), {});
  assert.equal(deny.at(-1).text, "超出預算，已拒絕");
  const sim = A.showEvidence(snap([...base("Ask", "HUMAN_APPROVAL_REQUIRED"), ev4()], { purchase: { approval: { state: "approved", resolved_by: "sim-approver-a" } } }), {});
  assert.equal(sim.at(-1).text, "主持人代為核准（示範，不是本人）");
  assert.equal(A.txLink("base_sepolia", "0x12").text, "交易編號未提供");
  const same = A.showEvidence(snap([...base("Allow", "WITHIN_ALLOW_MAX"), ...paid], { purchase: { ...p, payment: { ...delivered, settle_tx: TX1 } } }), {});
  assert.match(same[4].note, /未確認/); // "not one" only when the two hashes differ
});
const ev4 = () => ev(4, "APPROVED", { authorization_method: "simulated" });

test("the receipt card: only once delivered with a receipt; two transactions, not one", () => {
  const p = { resource_url: "https://shop.example/resources/kr-market", payment: delivered };
  assert.equal(A.showReceipt(snap([...base("Allow", "WITHIN_ALLOW_MAX"), ...paid.slice(0, 5)], { purchase: { ...p, payment: { ...delivered, state: "settled", receipt_id: "" } } })), null);
  const r = A.showReceipt(snap([...base("Allow", "WITHIN_ALLOW_MAX"), ...paid], { purchase: p }), {});
  const rows = Object.fromEntries(r.rows.map((x) => [x.label, x]));
  assert.equal(rows["商品"].text, "韓國市場簡報");
  assert.equal(rows["金額"].text, "0.50 USDC（測試幣）");
  assert.equal(rows["網路"].text, "Base Sepolia 測試網路");
  assert.equal(rows["核准"].text, "在委託範圍內，自動放行");
  assert.equal(rows["付款"].href, "https://sepolia.basescan.org/tx/" + TX2);
  assert.equal(rows["兩筆交易"].text, "不是同一筆，也不是一次完成");
  assert.equal(rows["內容摘要"].text, "sha256 cccccccccccc…");
  assert.equal(A.productName("https://x/scenes/cafe/lunch-set"), "午餐套餐（12:30 取餐）");
  assert.equal(A.productName("https://x/eval/q-17?v=1"), "評測資料（q-17）");
  assert.equal(A.productName("not a url"), "商品");
});

const REPORT = { checker: { name: "verify-demo", version: "0.4.0", build: "x" }, mode: "e1a", run_id: "e1a-20261023T011857Z-ace831", manifest_sha256: "sha256:00", verdict: "PASS",
  claims: Object.fromEntries(Object.keys(A.CLAIMS).map((k) => [k, "PASS"])), findings: [], injections: [],
  not_evaluated: ["the BBS+ proof re-verified by the checker (the pack keeps its sha256 only)", "voice FAR/FRR (V1's evaluation)", "Admin aggregation (M-D)",
    "the delegation confirmed by the person (E1a-D1 A: the host plays the customer)"], limits: ["x"] };

test("the checker's report: read as data, shown only as it says", () => {
  const { report } = A.parseChecker(JSON.stringify(REPORT));
  assert.deepEqual(Object.keys(report), ["run_id", "verdict", "claims", "checker", "not_evaluated", "fooled", "findings"]); // nothing else of the file goes on
  const c = A.showChecker(report);
  assert.equal(c.title, "獨立驗證：證據包 e1a-20261023T011857Z-ace831 的結果（不一定是畫面上這一場）");
  assert.equal(c.summary, "逐項檢查，全部通過");
  assert.equal(c.items.length, 10);
  assert.equal(c.items[3].label, "兩筆鏈上交易與 ZK 預算證明（向鏈查證）");
  assert.equal(c.by, "由 verify-demo 0.4.0 在場次後產生；本頁只顯示它的報告");
  assert.equal(c.outside, "不在這次驗證範圍：憑證證明由 checker 重新驗算、語音的誤判率、Admin 彙總、委託由本人確認");
  const failed = A.showChecker(A.parseChecker(JSON.stringify({ ...REPORT, verdict: "FAIL", claims: { ...REPORT.claims, E8: "FAIL", E4: "INCONCLUSIVE" } })).report);
  assert.equal(failed.summary, "有 2 項未通過（主持人頁有細節）");
  assert.equal(failed.items[7].text, "未通過");
  const unsure = A.showChecker(A.parseChecker(JSON.stringify({ ...REPORT, verdict: "INCONCLUSIVE", claims: { ...REPORT.claims, E4: "INCONCLUSIVE" } })).report);
  assert.equal(unsure.summary, "有 1 項無法判定（主持人頁有細節）");
  for (const [bad, why] of [["nope", "不是 JSON"], [JSON.stringify({ ...REPORT, mode: "m1" }), "不是 e1a 的 checker 報告"], [JSON.stringify({ ...REPORT, run_id: "../x y" }), "缺少證據包編號（run_id）"],
    [JSON.stringify({ ...REPORT, verdict: "OK" }), "總結果不明"], [JSON.stringify({ ...REPORT, claims: { ...REPORT.claims, E10: undefined } }), "E10 的結果不明"]]) {
    assert.equal(A.parseChecker(bad).error, why);
  }
  assert.equal(A.showChecker(null), null);
  // E10 passes when the injection run is recorded; whether the model was fooled is said apart.
  const inj = (f) => A.showChecker(A.parseChecker(JSON.stringify({ ...REPORT, injections: [{ task_id: "t", case: "c", fooled: f, requested: [] }] })).report).injection;
  assert.equal(inj(false), "商店誘導文字的測試：模型沒有被騙（有沒有多付錢見 E8）");
  assert.equal(inj(true), "商店誘導文字的測試：模型被騙了 1 次（有沒有多付錢見 E8）");
  assert.equal(c.injection, "這個證據包沒有商店誘導文字的測試"); // E10 passes with no injection run: nothing reads as recorded
  assert.equal(c.items[9].label, "商店誘導文字的測試");
  // Only every claim PASS is all passed (review S3-5); a run without --rpc says why it could not tell.
  const ne = A.showChecker(A.parseChecker(JSON.stringify({ ...REPORT, claims: { ...REPORT.claims, E4: "NOT_EVALUATED" } })).report);
  assert.equal(ne.summary, "有 1 項未評估（主持人頁有細節）");
  const norpc = A.parseChecker(JSON.stringify({ ...REPORT, verdict: "INCONCLUSIVE", claims: { ...REPORT.claims, E4: "INCONCLUSIVE", E8: "INCONCLUSIVE" },
    findings: [{ claim: "E4", verdict: "INCONCLUSIVE", reason: "no --rpc: the transactions were not read" }, { claim: "E8", verdict: "INCONCLUSIVE", reason: "no --rpc: no chain scan" }, { claim: "E1", verdict: "PASS", reason: "x" }] })).report;
  assert.deepEqual(norpc.findings.map((f) => f.claim + f.noChain), ["E4true", "E8true"]);
  assert.equal(A.showChecker(norpc).items[3].text, "無法判定（沒有向鏈查詢）");
  assert.equal(A.parseChecker(JSON.stringify({ ...REPORT, verdict: ["PASS"] })).error, "總結果不明");
  assert.equal(A.parseChecker(JSON.stringify({ ...REPORT, claims: { ...REPORT.claims, E2: ["PASS"] } })).error, "E2 的結果不明");
});

test("the scene's whole multiples: the largest that fits, at most six; a phone gets the fraction that fits", async () => {
  const { fitScale } = await import("../pixelscale.js");
  assert.equal(fitScale(1250, 680), 2);
  assert.equal(fitScale(800, 400), 1);
  assert.equal(fitScale(360, 600), 0.75); // a phone: the fraction of its width, never cut off at the side
  assert.equal(fitScale(1000, 200), 1); // a short screen keeps its own pixels
  assert.equal(fitScale(5000, 5000), 6);
});

test("on the chain (O2): each transaction as the Host read it, only for the hash the Store recorded", () => {
  const p = { purchase_order_id: "po-1", resource_url: "https://shop.example/resources/kr-market", payment: delivered };
  const payer = "0x" + "4d".repeat(20), shop = "0x" + "33".repeat(20);
  const chain = { chain: "local_anvil", head: 12, purchases: { "po-1": {
    reserve: { hash: TX1, status: "success", block: 9, time: 1790726400, spent: "500000" },
    settle: { hash: TX2, status: "success", block: 10, time: 1790726412, transfer: { from: payer, to: shop, amount: "500000", to_shop: true } } } } };
  const ev = A.showEvidence(snap([...base("Allow", "WITHIN_ALLOW_MAX"), ...paid], { cap: "local_anvil", purchase: p }), {}, chain);
  assert.equal(ev[2].chain, "鏈上：區塊 #9・4 個確認・合約記下 0.50 USDC");
  assert.equal(ev[3].chain, "鏈上：區塊 #10・3 個確認・0.50 USDC 客人錢包→商店");
  const other = { ...chain, purchases: { "po-1": { ...chain.purchases["po-1"], reserve: { ...chain.purchases["po-1"].reserve, hash: "0x" + "9".repeat(64) } } } };
  assert.equal(A.showEvidence(snap([...base("Allow", "WITHIN_ALLOW_MAX"), ...paid], { purchase: p }), {}, other)[2].chain, ""); // not the recorded hash: nothing said
  assert.equal(A.chainLine({ status: "not_found" }, 12, "reserve"), "鏈上：還查不到這筆交易");
  assert.match(A.chainLine({ status: "success", block: 10, transfer: { amount: "500000", to_shop: false } }, 12, "settle"), /收款人不是這筆請購的商店/); // never "→商店" unchecked
  assert.equal(A.chainLine({ status: "reverted", block: 9 }, 12, "reserve"), "鏈上：區塊 #9，執行失敗");
  assert.equal(A.showEvidence(snap([...base("Allow", "WITHIN_ALLOW_MAX"), ...paid], { purchase: p }), {})[2].chain, ""); // no chain read: nothing
});

test("a payment under review says what is known by its code: two never moved money, the shop's refusal is the shop's", () => {
  const evs = [...base("Allow", "WITHIN_ALLOW_MAX"), pay(4, "PAYMENT_REQUESTED"), pay(5, "BUDGET_RESERVED"), pay(6, "PAYMENT_NEEDS_REVIEW")];
  const at = (code) => A.showCustomer("consumer-a", snap(evs, { purchase: { payment: { status: "needs_review", state: "needs_review", code } } }), DLG).steps[3];
  assert.deepEqual([at("RESERVED_NOT_PAID").state, at("RESERVED_NOT_PAID").text], ["fail", "沒有付款（錢沒有動）：預算已預留，但付款沒有送出"]);
  assert.deepEqual([at("VERIFICATION_FAILED").state, at("VERIFICATION_FAILED").text], ["unknown", "商店回覆沒有接受、沒有送出（商店的說法，鏈上未確認）"]);
  assert.equal(at("NOT_SETTLED_EXPIRED").text, "沒有付款（錢沒有動）：付款授權已過期，鏈上沒有用過");
  assert.deepEqual([at("RECONCILE_NEEDS_REVIEW").state, at("RECONCILE_NEEDS_REVIEW").text], ["unknown", "鏈上的付款結果對不起來，待人工確認"]);
});
