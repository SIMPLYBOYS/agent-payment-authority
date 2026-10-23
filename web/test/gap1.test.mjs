// Tests of the gap review's batch 1 (docs/commercial-gap-review.md 8): a
// host that pays or approves by a person says so on every page, also after
// a viewer signs in; a host that does not keeps its words.
// Run: node --test web/test/gap1.test.mjs
import { test } from "node:test";
import assert from "node:assert/strict";
import * as A from "../adapter.js";

// app.js runs in a page: a stub document, a fetch that never answers.
const el = () => ({ textContent: "", hidden: true, dataset: {}, style: {}, classList: { add() {}, remove() {}, toggle() {} }, addEventListener() {}, append() {},
  replaceChildren() {}, setAttribute() {}, querySelector: () => null, querySelectorAll: () => [] });
const els = {};
globalThis.document = { getElementById: (id) => (els[id] ||= el()), querySelector: (s) => (els[s] ||= el()), querySelectorAll: () => [], addEventListener() {},
  dispatchEvent() {}, createElement: el, hidden: false, body: el(), head: el() };
globalThis.window = { addEventListener() {}, location: { search: "", hash: "" } };
globalThis.location = globalThis.window.location;
globalThis.fetch = () => new Promise(() => {});
const app = await import("../app.js");
const NOTE = "真實 Agent 即時展示：主持人按「開始」才會實際執行 Claude Code CLI；合成資料、不付款。商店場景只呈現 Store 已提交的狀態。";
els["live-note"] = { ...el(), textContent: NOTE };
const header = (info) => {
  app.labelPage(info);
  return { badge: els["header .badge"].textContent, caps: els["header .caps"].textContent, note: els["live-note"].hidden ? "" : els["live-note"].textContent };
};
const live = { record: "live_demo", label: "LIVE", dev_demo_plugins: true };

test("a mode or viewer answer keeps the host's payments and approval", () => {
  assert.equal(app.hostInfo(null, { mode: "fake" }), null);
  assert.equal(A.simulated(), true);
  assert.deepEqual(app.hostInfo(null, { payments: "base_sepolia", approval: "webauthn", authenticator: "device" }),
    { record: "fake_a2", payments: "base_sepolia", authenticator: "device" });
  assert.equal(A.simulated(), false); // the viewer page words approvals as the host's (review 3)
  assert.deepEqual(app.hostInfo(live, { payments: "local_anvil", approval: "webauthn", authenticator: "virtual" }), { ...live, payments: "local_anvil", authenticator: "virtual" });
  assert.equal(app.hostInfo(null, { payments: "mainnet", approval: "webauthn" }).payments, ""); // not a paying capability this page knows
  const hist = { record: "live_history", label: "H" };
  assert.equal(app.hostInfo(hist, { payments: "base_sepolia" }), hist);
});

test("a paying host's header and live note never say it does not pay", () => {
  const fake = header(app.hostInfo(null, { payments: "base_sepolia", approval: "webauthn", authenticator: "device" }));
  assert.equal(fake.badge, "A2／fake agent／本人核准（裝置）／Base Sepolia testnet（測試 USDC）付款");
  assert.match(fake.caps, /付款 Base Sepolia testnet（測試 USDC）/);
  assert.match(fake.caps, /本人以已登記的裝置/);
  const l = header(app.hostInfo(live, { payments: "local_anvil", approval: "webauthn", authenticator: "device" }));
  assert.match(l.caps, /^能力：真實 Agent（開發展示例外/);
  assert.match(l.note, /合成資料、付款以本機 anvil（測試 USDC）實際進行，委託確認仍是模擬/);
  for (const h of [fake, l]) assert.ok(!/不付款|未實作|未啟用/.test(h.badge + h.caps + h.note), h.badge + h.caps + h.note);
});

test("a virtual authenticator is never worded as a device", () => {
  const v = header(app.hostInfo(null, { payments: "base_sepolia", approval: "webauthn", authenticator: "virtual" }));
  assert.match(v.badge, /本人核准（virtual authenticator）/);
  assert.match(v.caps, /virtual authenticator（測試用，不是實體裝置）/);
  assert.ok(!/裝置）|已登記的裝置/.test(v.badge + v.caps.replace("不是實體裝置", "")));
  const a2 = header(app.hostInfo(null, { approval: "webauthn", authenticator: "virtual" }));
  assert.match(a2.caps, /virtual authenticator/);
});

test("a host that does not pay keeps its words", () => {
  assert.equal(header(app.hostInfo(null, { approval: "webauthn", authenticator: "device" })).badge, "A2／fake agent／本人核准（裝置）／不付款");
  assert.equal(header(null).badge, "M1／fake agent／不付款");
  const l = header(app.hostInfo(live, {}));
  assert.match(l.caps, /付款 未實作/);
  assert.equal(l.note, NOTE);
});

test("a paid Allow's reason is not 'payment not enabled'", () => {
  const p = { purchase_order_id: "po-1", amount: "500000", resource_url: "http://x/r",
    decision: { version: A.V1, policy_verdict: "Allow", reason: "WITHIN_ALLOW_MAX", approval_status: "not_requested", payment_status: "not_enabled" } };
  const v = (cap) => ({ task: { task_id: "t", consumer_id: "consumer-a", status: "running", as_of_sequence: 0, served_at: 1, purchases: [p], contract_version: A.V1, payment_capability: cap }, events: [] });
  const reason = (cap) => Object.fromEntries(A.card(v(cap), "po-1", { mode: "live", catalog: null }).rows.map((r) => [r[0], r[1]]))["原因"];
  assert.match(reason("base_sepolia"), /在委託範圍內；由 Host 付款（測試 USDC）/);
  assert.match(reason("unavailable"), /在委託範圍內；未啟用付款/);
});
