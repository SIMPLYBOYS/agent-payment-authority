// Tests of P2 in the page (docs/commercial-p2-host.md 5): a purchase's
// payment on its card, from the Store's payment view and events. Nothing
// reads as paid before the Store says so.
// Run: node --test web/test/p2.test.mjs
import { test } from "node:test";
import assert from "node:assert/strict";
import * as A from "../adapter.js";

let n = 0;
const ev = (seq, type, extra = {}) => ({ event_id: "e" + n++, event_type: type, sequence: seq, evidence_mode: "fixture", source: "host", observed_at: 1790726450 + seq, purchase_order_id: "po-1", ...extra });
const allowed = () => [ev(1, "TASK_STARTED", { purchase_order_id: "" }), ev(2, "PURCHASE_PROPOSED"),
  ev(3, "POLICY_DECIDED", { decision: { version: A.V1, policy_verdict: "Allow", reason: "WITHIN_ALLOW_MAX", approval_status: "not_requested", payment_status: "not_enabled" } })];
const purchase = (payment) => ({ purchase_order_id: "po-1", amount: "500000", asset_id: "fixture-usdc", resource_url: "http://x/r",
  decision: { version: A.V1, policy_verdict: "Allow", reason: "WITHIN_ALLOW_MAX", approval_status: "not_requested", payment_status: "not_enabled" }, payment });
const view = (events, payment) => ({ task: { task_id: "show-a-01", consumer_id: "consumer-a", status: "running", as_of_sequence: events.length, served_at: 1790726470,
  purchases: [purchase(payment)], contract_version: A.V1 }, events });
const card = (events, payment) => A.card(view(events, payment), null, { mode: "live", catalog: null });
const labels = (c) => c.steps.map((s) => s.label + ":" + s.state).slice(5);
const tx1 = "0x" + "1".repeat(64), tx2 = "0x" + "2".repeat(64);

test("P2: an Allow paid and delivered: every payment step done, both transactions and the receipt", () => {
  const evs = [...allowed(), ev(4, "PAYMENT_REQUESTED"), ev(5, "BUDGET_RESERVED"), ev(6, "PAYMENT_SIGNED"), ev(7, "PAYMENT_SUBMITTED"), ev(8, "PAYMENT_SETTLED"),
    ev(9, "RESOURCE_DELIVERED"), ev(10, "RECEIPT_ISSUED")];
  const c = card(evs, { status: "paid", state: "delivered", reserve_tx: tx1, settle_tx: tx2, settle_block: 12, content_sha256: "c".repeat(64), receipt_id: "rcpt-1" });
  assert.equal(c.status, "已付款、已交付（區塊 12），附收據");
  assert.deepEqual(labels(c), ["收尾:none", "預留:done", "簽名:done", "送出:done", "鏈上確認:done", "交付:done"]);
  const rows = Object.fromEntries(c.rows.map((r) => [r[0], r[1]]));
  assert.ok(rows["預算預留 tx#1"].startsWith("0x1111"));
  assert.ok(rows["付款 tx#2"].includes("區塊 12"));
  assert.ok(rows["收據"].startsWith("rcpt-1"));
});

test("P2: in flight it is paying, then confirming; never paid before the chain", () => {
  const reserved = card([...allowed(), ev(4, "PAYMENT_REQUESTED"), ev(5, "BUDGET_RESERVED")], { status: "paying", state: "reserved" });
  assert.equal(reserved.status, "付款中：預算已預留");
  assert.deepEqual(labels(reserved), ["收尾:none", "預留:done", "簽名:future", "送出:future", "鏈上確認:future", "交付:future"]);
  const sent = card([...allowed(), ev(4, "PAYMENT_REQUESTED"), ev(5, "BUDGET_RESERVED"), ev(6, "PAYMENT_SIGNED"), ev(7, "PAYMENT_SUBMITTED")], { status: "confirming", state: "submitted" });
  assert.equal(sent.status, "已送出，等待鏈上確認（不會自動再付）");
  assert.ok(!/已付款/.test(sent.status + sent.rows.map((r) => r[1]).join()));
});

test("P2: an ending replaces the steps it cut off, with its code", () => {
  const review = card([...allowed(), ev(4, "PAYMENT_REQUESTED"), ev(5, "BUDGET_RESERVED"), ev(6, "PAYMENT_NEEDS_REVIEW")], { status: "needs_review", state: "needs_review", code: "RESERVED_NOT_PAID" });
  assert.match(review.status, /^待人工處理：預算已預留，沒有簽名或送出/);
  assert.deepEqual(labels(review), ["收尾:none", "預留:done", "待人工處理:done"]);
  const notPaid = card([...allowed(), ev(4, "PAYMENT_REQUESTED"), ev(5, "PAYMENT_NOT_SETTLED")], { status: "not_paid", state: "reserve_failed", code: "RESERVE_FAILED" });
  assert.equal(notPaid.status, "未付款：預算預留失敗，沒有花錢");
  assert.deepEqual(labels(notPaid), ["收尾:none", "未付款:done"]);
  const failed = card([...allowed(), ev(4, "PAYMENT_REQUESTED"), ev(5, "BUDGET_RESERVED"), ev(6, "PAYMENT_SIGNED"), ev(7, "PAYMENT_SUBMITTED"), ev(8, "PAYMENT_SETTLED"),
    ev(9, "PAYMENT_NEEDS_REVIEW")], { status: "paid", state: "needs_review", code: "DELIVERY_FAILED", settle_block: 12 });
  assert.equal(failed.status, "已付款（區塊 12），交付失敗，待人工處理");
  assert.deepEqual(labels(failed).slice(-2), ["鏈上確認:done", "待人工處理:done"]);
});

test("P2: without a payment the Allow card is as before", () => {
  const c = card(allowed(), undefined);
  assert.equal(c.status, "Allow：在委託範圍內，但未啟用付款（不會自動付款）");
  assert.equal(c.steps.length, 6);
});

test("P2: the scene follows the payment, and Unity is sent the same phase and its cause", () => {
  const base = allowed();
  for (const [extra, phase] of [
    [[ev(4, "PAYMENT_REQUESTED"), ev(5, "BUDGET_RESERVED")], "paying"],
    [[ev(4, "PAYMENT_REQUESTED"), ev(5, "BUDGET_RESERVED"), ev(6, "PAYMENT_SIGNED"), ev(7, "PAYMENT_SUBMITTED")], "confirming"],
    [[ev(4, "PAYMENT_REQUESTED"), ev(5, "BUDGET_RESERVED"), ev(6, "PAYMENT_SIGNED"), ev(7, "PAYMENT_SUBMITTED"), ev(8, "PAYMENT_SETTLED")], "paid"],
    [[ev(4, "PAYMENT_REQUESTED"), ev(5, "BUDGET_RESERVED"), ev(6, "PAYMENT_SIGNED"), ev(7, "PAYMENT_SUBMITTED"), ev(8, "PAYMENT_SETTLED"), ev(9, "RESOURCE_DELIVERED"), ev(10, "RECEIPT_ISSUED")], "delivered"],
    [[ev(4, "PAYMENT_REQUESTED"), ev(5, "BUDGET_RESERVED"), ev(6, "PAYMENT_SIGNED"), ev(7, "PAYMENT_SUBMITTED"), ev(8, "PAYMENT_SETTLED"), ev(9, "PAYMENT_NEEDS_REVIEW")], "delivery_failed"],
    [[ev(4, "PAYMENT_REQUESTED"), ev(5, "PAYMENT_NOT_SETTLED")], "not_paid"],
    [[ev(4, "PAYMENT_REQUESTED"), ev(5, "BUDGET_RESERVED"), ev(6, "PAYMENT_NEEDS_REVIEW")], "payment_review"],
  ]) {
    const evs = [...base, ...extra];
    const v = view(evs, undefined);
    const sc = A.scene(v);
    assert.equal(sc.phase, phase);
    assert.equal(sc.cause, evs[evs.length - 1].sequence, phase);
    assert.ok(sc.label && !sc.label.includes("未知"), phase);
    const m = A.projection({ snapshot: v, gen: 1 }, false);
    assert.equal(m.phase, phase);
    assert.equal(m.cause_seq, sc.cause, phase);
    assert.ok(!/不付款|不會付款/.test(A.shopLabel(m.phase, m.cause_seq, "", "")), phase);
  }
  assert.equal(A.scene(view(base, undefined)).phase, "allowed_unpaid");
  const pays = view(base, undefined);
  pays.task.payment_capability = "local_anvil";
  assert.equal(A.projection({ snapshot: pays, gen: 1 }, false).phase, "allowed"); // the host will pay it: never "no payment capability"
});

test("P2: with local_anvil an Allow and a person's approval say not paid yet; a simulated one never pays", () => {
  const v = view(allowed(), undefined);
  v.task.payment_capability = "local_anvil";
  assert.equal(A.card(v, null, { mode: "live", catalog: null }).status, "Allow：在委託範圍內，尚未付款");
  v.task.payment_capability = "not_enabled";
  assert.equal(A.card(v, null, { mode: "live", catalog: null }).status, "Allow：在委託範圍內，但未啟用付款（不會自動付款）");
  const asked = [ev(1, "TASK_STARTED", { purchase_order_id: "" }), ev(2, "PURCHASE_PROPOSED"),
    ev(3, "POLICY_DECIDED", { decision: { version: A.V1, policy_verdict: "Ask", reason: "HUMAN_APPROVAL_REQUIRED", approval_status: "pending", payment_status: "not_enabled" } }),
    ev(4, "APPROVED", { authorization_method: "webauthn", decision: { reason: "HUMAN_APPROVED" } })];
  const s = { task: { ...v.task, payment_capability: "local_anvil" }, events: asked };
  assert.equal(A.scene(s).label, "本人核准已驗證：尚未付款");
  assert.equal(A.projection({ snapshot: s, gen: 1 }, false).phase, "approved");
  s.task.payment_capability = "not_enabled";
  assert.equal(A.scene(s).label, "本人核准已驗證：未啟用付款，不會取貨");
  assert.equal(A.projection({ snapshot: s, gen: 1 }, false).phase, "allowed_unpaid");
});

test("P2: a person's approval says not paid yet with local_anvil", async () => {
  const AP = await import("../approve.js");
  const dev = { approval: { resolved_by: "webauthn:approver-a", grant: { authorization_method: "webauthn" } } };
  assert.equal(AP.decidedText("approved", dev, true)[1], "本人核准已驗證，尚未付款。");
  // H6: once paid, the panel says so (it said "not yet paid" after the payment).
  const paidDev = { ...dev, payment: { status: "paid", state: "delivered", settle_block: 11 } };
  assert.equal(AP.decidedText("approved", paidDev, true)[1], "本人核准已驗證；已付款、已交付（區塊 11），附收據。");
  assert.equal(AP.decidedText("approved", paidDev)[1], "本人核准已驗證；已付款、已交付（區塊 11），附收據。"); // a recorded payment, said on any host
  const review = { ...dev, payment: { status: "needs_review", state: "needs_review", code: "RESERVED_NOT_PAID" } };
  assert.match(AP.decidedText("approved", review, true)[1], /^本人核准已驗證；待人工處理：預算已預留，沒有簽名或送出/);
  const sim = { approval: { resolved_by: "simulated:consumer-a:host", grant: { authorization_method: "simulated" } }, payment: { status: "paid", state: "delivered" } };
  assert.equal(AP.decidedText("approved", sim, true)[1], "已核准（模擬），未啟用付款"); // a simulated approval never reads as paid
  assert.equal(AP.decidedText("approved", dev)[1], "本人核准已驗證，付款未啟用。");
  dev.approval.grant.voice = "verified";
  assert.equal(AP.decidedText("approved", dev, true)[1], "本人核准已驗證（裝置＋語音），尚未付款。");
  const voicePaid = AP.decidedText("approved", { ...dev, payment: paidDev.payment }); // a recorded payment: headline and note agree
  assert.equal(voicePaid[1], "本人核准已驗證（裝置＋語音）；已付款、已交付（區塊 11），附收據。");
  assert.equal(voicePaid[2], AP.decidedText("approved", paidDev)[2]);
});

test("P2: a person's approval, then its payment: Unity follows the payment, never browsing again", () => {
  const asked = [ev(1, "TASK_STARTED", { purchase_order_id: "" }), ev(2, "PURCHASE_PROPOSED"),
    ev(3, "POLICY_DECIDED", { decision: { version: A.V1, policy_verdict: "Ask", reason: "HUMAN_APPROVAL_REQUIRED", approval_status: "pending", payment_status: "not_enabled" } }),
    ev(4, "APPROVED", { authorization_method: "webauthn", decision: { reason: "HUMAN_APPROVED" } })];
  const task = { ...view(allowed(), undefined).task, payment_capability: "local_anvil" };
  let t = A.transition(null, A.projection({ snapshot: { task, events: asked }, gen: 1 }, false));
  assert.equal(t.target.phase, "approved");
  const rest = [[ev(5, "PAYMENT_REQUESTED"), "paying"], [ev(6, "BUDGET_RESERVED"), "paying"], [ev(7, "PAYMENT_SIGNED"), "paying"], [ev(8, "PAYMENT_SUBMITTED"), "confirming"],
    [ev(9, "PAYMENT_SETTLED"), "paid"], [ev(10, "RESOURCE_DELIVERED"), "paid"], [ev(11, "RECEIPT_ISSUED"), "delivered"]];
  const evs = [...asked];
  for (const [e, phase] of rest) {
    evs.push(e);
    const m = A.projection({ snapshot: { task, events: evs }, gen: 1 }, false);
    assert.equal(m.phase, phase, e.event_type);
    t = A.transition(t.target, m);
    assert.notEqual(t.rule, 2, e.event_type); // never a new purchase
    assert.equal(t.browse, false, e.event_type);
  }
  assert.equal(t.target.label, "已交付，帶著收據離店");
});

test("P2: an Allow never paid (the task ended first) says not paid yet, never that the host pays", () => {
  const v = view([...allowed(), ev(4, "TASK_CANCELLED", { purchase_order_id: "" })], undefined);
  v.task.payment_capability = "local_anvil";
  const m = A.projection({ snapshot: v, gen: 1 }, false);
  assert.equal(A.shopLabel(m.phase, m.cause_seq, m.task_end, ""), "在範圍內放行，尚未付款");
  const c = A.card(v, null, { mode: "live", catalog: null });
  assert.equal(c.status, "Allow：在委託範圍內，尚未付款");
  assert.ok(!c.steps.some((s) => /未啟用付款/.test(s.note)), "a step says payment is not enabled");
});

test("an Ask of an ended task: not approvable, its words say so; an approval says payment follows on a paying host, not enabled without one", async () => {
  const AP = await import("../approve.js");
  assert.equal(AP.outcomeText(409, { error: "task_ended" }, "approve")[0], "closed");
  assert.match(AP.outcomeText(409, { error: "task_ended" }, "approve")[1], /不能核准，也不會付款/);
  // On a paying host the operation's line outlives the payment's start: it points to the card, never "not paid".
  assert.equal(AP.outcomeText(200, { outcome: "approved" }, "approve", true)[1], "本人核准已驗證，Host 隨後付款；付款進度以上方與請購卡片為準。");
  const voiced = { outcome: "approved", item: { purchase: { approval: { grant: { voice: "verified" } } } } };
  assert.doesNotMatch(AP.outcomeText(200, voiced, "approve", true)[1], /尚未付款/);
  assert.equal(AP.outcomeText(200, { outcome: "approved" }, "approve")[1], "本人核准已驗證，付款未啟用。");
});

test("an Ask of an ended pay task (a CLI timeout, or another purchase denied) reads as not approvable on the scene and the card", () => {
  const askDecision = { version: A.V1, policy_verdict: "Ask", reason: "HUMAN_APPROVAL_REQUIRED", approval_status: "pending", payment_status: "not_enabled" };
  const asked = [ev(1, "TASK_STARTED", { purchase_order_id: "" }), ev(2, "PURCHASE_PROPOSED"), ev(3, "POLICY_DECIDED", { decision: askDecision })];
  const ask = { purchase_order_id: "po-1", amount: "500000", asset_id: "fixture-usdc", resource_url: "http://x/r", decision: askDecision };
  const task = (evs, purchases) => ({ task_id: "show-a-01", consumer_id: "consumer-a", status: "running", as_of_sequence: evs.length, served_at: 1790726470,
    purchases, contract_version: A.V1, payment_capability: "local_anvil" });
  // a CLI timeout
  const timed = [...asked, ev(4, "TASK_TIMED_OUT", { purchase_order_id: "" })];
  const v1 = { task: task(timed, [ask]), events: timed };
  assert.equal(A.scene(v1).label, "task 已結束：這筆不能核准，到期後關閉；不會付款");
  assert.equal(A.card(v1, null, { mode: "live", catalog: null }).status, "task 已結束：這筆不能核准，到期後關閉；不會付款");
  // another purchase denied: the Ask's card, picked by hand
  const denyDecision = { version: A.V1, policy_verdict: "Deny", reason: "INSUFFICIENT_BUDGET", approval_status: "not_requested", payment_status: "not_enabled" };
  const other = [...asked, ev(4, "PURCHASE_PROPOSED", { purchase_order_id: "po-2" }), ev(5, "POLICY_DECIDED", { purchase_order_id: "po-2", decision: denyDecision })];
  const v2 = { task: task(other, [ask, { ...ask, purchase_order_id: "po-2", decision: denyDecision }]), events: other };
  const c = A.card(v2, "po-1", { mode: "live", catalog: null });
  assert.equal(c.status, "task 已結束：這筆不能核准，到期後關閉；不會付款");
  assert.ok(!c.steps.some((s) => /等你核准/.test(s.note)), "a step still asks for approval");
  // a task still running: as before
  const v3 = { task: task(asked, [ask]), events: asked };
  assert.match(A.scene(v3).label, /^等你核准/);
});

test("with local_anvil a person's grant, a record by the other method and the sources do not say payment is off", async () => {
  const AP = await import("../approve.js");
  const g = { jti: "grant-0123456789abcdefghij", authorization_method: "webauthn" };
  assert.match(AP.grantLine(g, true), /付款依據/);
  assert.match(AP.grantLine(g), /付款未啟用$/);
  assert.match(AP.sourceText({ agent: "fake", authenticator: "virtual", payment: "local_anvil" }), /付款：本機 anvil/);
  assert.match(AP.sourceText({ agent: "fake", authenticator: "virtual", payment: "disabled" }), /付款：未啟用/);
});

test("the host page's reconciliation row: zero is ok, a spend is an error, unread is unconfirmed", () => {
  const rec = { version: "apa.reconciliation.v1", environment: "local_anvil", chain_id: 84532, from_block: 0, to_block: 40, wallets: ["0x1", "0x2"],
    known_authorizations: 3, unauthorized: [] };
  assert.deepEqual(A.reconciliationRow(rec), ["對帳", "ok", "未授權新增支出 0 筆（本機 anvil；區塊 0–40；錢包 2 個、已記錄授權 3 筆）"]);
  const bad = A.reconciliationRow({ ...rec, unauthorized: [{ from: "0x1", to: "0x9", value: "1000", tx: "0x" + "a".repeat(64), block: 39 }] });
  assert.equal(bad[1], "error");
  assert.match(bad[2], /^未授權新增支出 1 筆/);
  assert.equal(A.reconciliationRow(undefined)[1], "unconfirmed");
  assert.equal(A.reconciliationRow({ error: "reconciliation_pending" })[1], "unconfirmed"); // still reading: not a failure, never a green zero
  assert.deepEqual(A.reconciliationRow({ error: "chain_unavailable" }), ["對帳", "error", "無法對帳（chain_unavailable），未確認"]);
  for (const broken of [{ ...rec, unauthorized: null }, { ...rec, unauthorized: undefined }, { ...rec, version: "x" }]) {
    assert.equal(A.reconciliationRow(broken)[1], "error", JSON.stringify(broken)); // never a green zero
  }
});

test("base_sepolia pays as local_anvil does, and is named the testnet (docs/commercial-p2b.md B3)", async () => {
  const AP = await import("../approve.js");
  for (const [cap, want] of [["local_anvil", true], ["base_sepolia", true], ["not_enabled", false], ["unavailable", false], ["toString", false], [undefined, false]]) {
    assert.equal(A.paysOn(cap), want, String(cap));
  }
  const pays = view(allowed(), undefined);
  pays.task.payment_capability = "base_sepolia";
  const m = A.projection({ snapshot: pays, gen: 1 }, false);
  assert.equal(m.phase, "allowed"); // an Allow the host will pay, not "allowed, never paid"
  assert.equal(m.payments, "base_sepolia"); // Unity's banner
  const insp = Object.fromEntries(A.inspector(pays));
  assert.match(insp["付款"], /^Base Sepolia testnet 付款/);
  assert.match(AP.sourceText({ agent: "fake", authenticator: "device", payment: "base_sepolia" }), /付款：Base Sepolia testnet（測試 USDC）/);
  const rec = { version: "apa.reconciliation.v1", environment: "base_sepolia", chain_id: 84532, from_block: 100, to_block: 140, wallets: ["0x1", "0x2"],
    known_authorizations: 2, unauthorized: [] };
  assert.deepEqual(A.reconciliationRow(rec), ["對帳", "ok", "未授權新增支出 0 筆（Base Sepolia testnet；區塊 100–140；錢包 2 個、已記錄授權 2 筆）"]);
  const other = view(allowed(), undefined);
  other.task.payment_capability = "base_mainnet"; // an unknown capability never pays, never names a banner
  assert.equal(A.projection({ snapshot: other, gen: 1 }, false).payments, "");
});

test("E1-a: the Inspector shows the run conditions, both transactions with Base Sepolia's explorer, and makes no ZK claim; the Timeline shows a payment event's record", () => {
  const events = [ev(1, "TASK_STARTED", { purchase_order_id: "", run_conditions: { agent: "live", chain: "base_sepolia", authenticator: "device" } }),
    ev(2, "BUDGET_RESERVED", { payment: { method: "delegation", amount: "500000", tx_hash: tx1, block: 48394045 } })];
  const v = view(events, { status: "paid", state: "delivered", reserve_tx: tx1, reserve_block: 48394045, settle_tx: tx2, settle_block: 48394047, receipt_id: "rcpt-1", content_sha256: "sha256:ab" });
  v.task.payment_capability = "base_sepolia";
  v.task.budget_contract = "0xbudget";
  const rows = Object.fromEntries(A.inspector(v));
  assert.match(rows["執行條件"], /^真實 Agent（live CLI） · Base Sepolia · 實體裝置（主持人宣告）/);
  assert.equal(rows["預算合約（Z2，主機設定）"], "0xbudget");
  assert.ok(!A.inspector(v).some(([k, val]) => k !== "ZK proof" && /已驗證/.test(val)), "no row claims the proof verified");
  assert.equal(rows["tx#1 預留（Z2）"], tx1 + " · https://sepolia.basescan.org/tx/" + tx1 + " · 區塊 48394045");
  assert.equal(rows["tx#2 結算"], tx2 + " · https://sepolia.basescan.org/tx/" + tx2 + " · 區塊 48394047");
  assert.match(rows["ZK proof"], /不自行宣稱已驗證/);
  assert.match(A.timeline(v)[1].detail, new RegExp("tx " + tx1 + " · 區塊 48394045"));
  v.task.payment_capability = "local_anvil";
  assert.match(Object.fromEntries(A.inspector(v))["tx#1 預留（Z2）"], /本機鏈，沒有 explorer/);
});

// Gap review C2: the Signer, its voice service and the relayer in the readiness table.
test("the services' readiness rows", () => {
  assert.deepEqual(A.serviceRows(undefined), []);
  const rows = A.serviceRows({ signer: "ready", voice: "ready", relayer: "ready" });
  assert.deepEqual(rows.map(([k, s]) => k + ":" + s), ["Signer:ok", "語音服務:ok", "relayer:ok"]);
  const down = Object.fromEntries(A.serviceRows({ signer: "unavailable", voice: "unknown", relayer: "unavailable" }).map(([k, s, t]) => [k, [s, t]]));
  assert.equal(down.Signer[0], "error");
  assert.equal(down["語音服務"][0], "unconfirmed");
  assert.match(down.relayer[1], /apa-relayer/);
  assert.deepEqual(A.serviceRows({ signer: "approvals-only", voice: "off", relayer: "" }).map(([k, s]) => k + ":" + s), ["Signer:ok", "語音服務:off"]); // no --payments: no relayer row
  assert.equal(A.serviceRows({ signer: "new", voice: "ready" })[0][1], "unconfirmed"); // a state this page does not know is not ready
  assert.deepEqual(A.serviceRows({ signer: "timeout", voice: "unknown", relayer: "timeout" }).map(([k, s]) => k + ":" + s), ["Signer:error", "語音服務:unconfirmed", "relayer:error"]); // a hung service is an error
  assert.equal(A.voiceText(""), "原因未回報");
});
