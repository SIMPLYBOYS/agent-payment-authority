// Real-browser check of A2 (docs/commercial-a2.md; layer B of
// docs/commercial-human-approval-design.md 9): apa-signer serve-approvals in
// a private test directory, the demo host with the fake CLI and
// --approvals-signer, and headless Chrome with a CDP virtual authenticator
// (a test fixture: never a real-device result). Enrollment through
// /approve/enroll with the administrator's begin and confirm (confirm
// answered on a pseudo-terminal, as a person types it), then on
// http://localhost:<port>/host: approve, deny by keyboard, the other
// decision while one is open, 取消這次驗證, the Signer down (result unknown, no
// prompt by itself), a customer switch while a reply is held, the
// 127.0.0.1 page, and 一起開始 with both customers' Asks named (batch 3). No
// key, no payment, no model; a new Chrome profile; no token, code or
// capability is logged, and screenshots are taken with the token fields empty.
//
//   node scripts/verify/a2-ui.mjs [--unity-build DIR] [--out DIR]

import { execFileSync, spawn, spawnSync } from "node:child_process";
import { mkdtempSync, mkdirSync, readFileSync, writeFileSync, chmodSync } from "node:fs";
import { createServer } from "node:net";
import { tmpdir } from "node:os";
import { join, resolve } from "node:path";
import { startDemo, startChrome, sleep } from "./harness.mjs";

const repo = resolve(new URL("../..", import.meta.url).pathname);
const arg = (k, d) => { const i = process.argv.indexOf(k); return i > 0 ? process.argv[i + 1] : d; };
const unityBuild = resolve(arg("--unity-build", join(repo, "unity/AgentPaymentAuthority/Build/WebGL/Build")));
const stamp = new Date().toISOString().replace(/[-:]/g, "").replace(/\.\d+Z$/, "Z");
const commit = execFileSync("git", ["-C", repo, "rev-parse", "HEAD"]).toString().trim();
const dirty = execFileSync("git", ["-C", repo, "status", "--porcelain", "--untracked-files=no"]).toString().trim() !== "";
const out = resolve(arg("--out", join(repo, "artifacts/human-approval", "a2-ui-" + commit.slice(0, 12) + (dirty ? "-dirty" : "") + "-" + stamp)));
mkdirSync(out, { recursive: true });

const lines = [];
let failures = 0;
const started = Date.now();
function log(s) { lines.push(s); console.log(s); }
const mark = (name) => log("  [" + Math.round((Date.now() - started) / 1000) + " s] " + name);
function check(name, ok, detail = "") { log((ok ? "PASS " : "FAIL ") + name + (detail ? "  [" + String(detail).slice(0, 300) + "]" : "")); if (!ok) failures++; }

const tmp = mkdtempSync(join(tmpdir(), "apa-a2-"));
chmodSync(tmp, 0o700);
const home = join(tmp, "home");
mkdirSync(home);
const bin = join(tmp, "bin");
mkdirSync(bin);
for (const [name, pkg] of [["demo", "./cmd/demo"], ["fake-claude", "./cmd/fake-claude"], ["apa-signer", "./cmd/apa-signer"]]) {
  execFileSync("go", ["build", "-o", join(bin, name), pkg], { cwd: repo, stdio: "inherit" });
}

// A port for the host, known before it starts: the Signer's origin names it.
const port = await new Promise((res) => { const s = createServer(); s.listen(0, "127.0.0.1", () => { const p = s.address().port; s.close(() => res(p)); }); });
const origin = "http://localhost:" + port;

// The Signer's private directory: wallets A and B (public information),
// one approver each, the relying party localhost with this origin; no
// keys/ and no signer.db.
const sdir = join(tmp, "signer");
mkdirSync(sdir, { mode: 0o700 });
const sock = join(sdir, "s.sock");
writeFileSync(join(sdir, "signer.json"), JSON.stringify({ version: 1, socket: sock, callers: [process.getuid()], max_window_seconds: 600,
  wallets: [{ wallet_id: "wallet-a", consumer_id: "consumer-a", chain_id: "84532", address: "0x1111111111111111111111111111111111111111" },
    { wallet_id: "wallet-b", consumer_id: "consumer-b", chain_id: "84532", address: "0x2222222222222222222222222222222222222222" }],
  assets: [{ chain_id: "84532", asset_contract: "0x4444444444444444444444444444444444444444", eip712_name: "USDC", eip712_version: "2" }], delegations: [], // gap 4: the asset a delegation names
  approvers: [{ approver_id: "approver-a", consumer_id: "consumer-a", wallet_ids: ["wallet-a"], signer_max: "1000000", signer_total: "2000000", signer_window_seconds: 86400, version: 1 },
    { approver_id: "approver-b", consumer_id: "consumer-b", wallet_ids: ["wallet-b"], signer_max: "1000000", signer_total: "2000000", signer_window_seconds: 86400, version: 1 }],
  webauthn: { rp_id: "localhost", origins: [origin], challenge_ttl_seconds: 120, approval_ttl_seconds: 300 } }), { mode: 0o600 });
let signer = null;
async function startSigner() {
  const p = spawn(join(bin, "apa-signer"), ["serve-approvals", "--dir", sdir], { stdio: ["ignore", "ignore", "pipe"] });
  let text = "";
  p.stderr.on("data", (b) => { text += b; });
  for (let i = 0; i < 100 && !text.includes("serving approvals only"); i++) await sleep(100);
  if (!text.includes("serving approvals only")) throw new Error("serve-approvals did not start");
  signer = p;
}
async function stopSigner() { if (!signer) return; const p = signer; signer = null; p.kill("SIGINT"); await new Promise((r) => p.once("exit", r)); }
// admin runs one administrator command; with input, on a pseudo-terminal
// (script), which answers the confirm question as a person types it.
const admin = (args, input) => {
  const r = spawnSync(input ? "script" : join(bin, "apa-signer"), input ? ["-q", "/dev/null", join(bin, "apa-signer"), ...args] : args, { input: input || "" });
  return String(r.stdout) + String(r.stderr);
};

const tool = (name, product) => ({ do: "tool", tool: name, ...(product ? { args: { product_id: product } } : {}) });
const scenario = (name, steps) => { const p = join(tmp, name + ".json"); writeFileSync(p, JSON.stringify({ steps })); return p; };
const main = scenario("main", [tool("list_products"), tool("get_quote", "kr-market"), tool("request_purchase", "kr-market"), { do: "result", text: "done" }]);
const terms = { purpose: "買一份韓國市場簡報（數位資料）", task_budget: "5000000", allow_max: "100000", approval_deadline_seconds: 120, valid_seconds: 1800 };
const catalog = join(tmp, "scenarios.json");
writeFileSync(catalog, JSON.stringify({ description: "a2-ui", scenarios: [
  { id: "pay-a", consumer_id: "consumer-a", contract: "apa.pay.v1", scenario: main, label: "委託後選購：韓國市場簡報", delegation: terms },
  { id: "pay-b", consumer_id: "consumer-b", contract: "apa.pay.v1", scenario: main, label: "委託後選購：韓國市場簡報", delegation: terms },
] }));

let demo, b;
try {
  log("commit " + commit + (dirty ? " (working tree has uncommitted changes)" : "") + "; authenticator: CDP virtual (test fixture, not a device)");
  await startSigner();
  const state = join(tmp, "state");
  demo = startDemo({ repo, bin, catalog, unityBuild, state, home, addr: "127.0.0.1:" + port,
    extra: ["--approvals-signer", sock, "--authenticator-source", "virtual", "--registry", "testdata/demo/registry.json"] });
  const addr = await demo.ready();
  const t = demo.tokens();
  const tokenFile = join(tmp, "tokens.json");
  writeFileSync(tokenFile, readFileSync(join(state, "tokens.json")));
  b = await startChrome(join(tmp, "chrome-profile"));
  await b.send("Emulation.setFocusEmulationEnabled", { enabled: true });
  await b.send("Emulation.setDeviceMetricsOverride", { width: 1440, height: 900, deviceScaleFactor: 1, mobile: false });
  await b.send("WebAuthn.enable");
  const { authenticatorId } = await b.send("WebAuthn.addVirtualAuthenticator", { options: { protocol: "ctap2", transport: "internal", hasResidentKey: true,
    hasUserVerification: true, isUserVerified: true, automaticPresenceSimulation: true } });
  const signs = async () => (await b.send("WebAuthn.getCredentials", { authenticatorId })).credentials.reduce((n, c) => n + c.signCount, 0);
  const text = (sel) => b.evaluate(`(document.querySelector(${JSON.stringify(sel)}) || {}).textContent || ""`);
  const click = (sel) => b.evaluate(`(() => { const e = document.querySelector(${JSON.stringify(sel)}); if (!e || e.disabled) return false; e.click(); return true; })()`);
  const card = (c) => `document.querySelector('.agent-card[data-consumer="${c}"]')`;
  const hostTask = async (id) => (await fetch("http://" + addr + "/api/tasks/" + id, { headers: { Authorization: "Bearer " + t.host } })).json();
  async function shot(name) {
    await b.evaluate(`(window.scrollTo(0, 0), true)`);
    await sleep(800);
    check("screenshot " + name + ": token and code fields empty", await b.evaluate(`["token", "pair-code", "enroll-code"].every((id) => !document.getElementById(id) || document.getElementById(id).value === "")`));
    const png = await b.send("Page.captureScreenshot", { format: "png" });
    writeFileSync(join(out, name + ".png"), Buffer.from(png.data, "base64"));
    log("  screenshot " + name + ".png");
  }
  async function loadPage(at) {
    await b.send("Page.navigate", { url: at + "/host" });
    await b.waitFor(`document.readyState === "complete"`);
    await b.waitFor(`["ready","rendered","failed","scene","syncing"].includes(document.getElementById("unity-status").dataset.state)`, 90000);
    const doc = await b.send("DOM.getDocument", {});
    const q = await b.send("DOM.querySelector", { nodeId: doc.root.nodeId, selector: "#token-file" });
    await b.send("DOM.setFileInputFiles", { nodeId: q.nodeId, files: [tokenFile] });
    return b.waitFor(`document.getElementById("creds").dataset.mode === "file"`, 20000);
  }
  // enroll: the administrator's begin, the person's page, the administrator's confirm.
  async function enroll(approver) {
    const begun = admin(["approver", "enroll", "begin", "--dir", sdir, "--approver", approver]);
    const m = begun.match(/enrollment (enr-[0-9a-f]+) for \S+; one-time code ([A-Z0-9]{8})/);
    if (!m) throw new Error("enroll begin printed nothing usable");
    await b.send("Page.navigate", { url: origin + "/approve/enroll" });
    await b.waitFor(`document.body.dataset.ready === "1"`); // enroll.js attached its handler (a module runs after parsing)
    await b.evaluate(`(document.getElementById("enroll-id").value = ${JSON.stringify(m[1])}, document.getElementById("enroll-code").value = ${JSON.stringify(m[2])}, document.getElementById("enroll-go").click(), true)`);
    const ok = await b.waitFor(`!document.getElementById("enroll-result").hidden`, 15000);
    const fp = await text("#enroll-fp");
    const confirmed = admin(["approver", "enroll", "confirm", "--dir", sdir, "--id", m[1]], "yes\n");
    return { ok: ok && confirmed.includes("credential " + fp) && confirmed.includes("is active") && /UV=1/.test(await text("#enroll-flags")), fp };
  }
  // Gap 4: with the person's device, the customer confirms the delegation by device too (nobody plays the customer).
  const startRun = async (c, scenarioId, task) => {
    await click(`.agent-card[data-consumer="${c}"] .agent-delegate[data-scenario="${scenarioId}"]`);
    await click(`.delegation[data-scenario="${scenarioId}"] .delegation-confirm`);
    return b.waitFor(`document.getElementById("run-status").textContent.includes("已開始 ${task}")`, 20000);
  };
  const pendingShown = () => b.waitFor(`!!document.querySelector("#approval .approval-head[data-state=pending]") && document.getElementById("approval-body").textContent.includes("用已登記的裝置核准")`, 30000);
  const headIs = (state, h3) => b.waitFor(`(document.querySelector("#approval .approval-head[data-state=${state}] h3") || {}).textContent === ${JSON.stringify(h3)}`, 20000);
  const msg = (part) => b.waitFor(`(document.querySelector("#approval .approval-msg") || {}).textContent?.includes(${JSON.stringify(part)})`, 20000);

  // ---- 1 enrollment (E1) ----
  mark("enroll");
  const ea = await enroll("approver-a");
  check("A's device: begin at the terminal, created on /approve/enroll, the same fingerprint confirmed at the terminal", ea.ok, ea.fp);
  await shot("a2-1440x900-0-enrolled");
  const eb = await enroll("approver-b");
  check("B's device enrolled the same way (another approver, its own credential)", eb.ok && eb.fp !== ea.fp, eb.fp);

  // ---- 2 the 127.0.0.1 page offers no device approval ----
  mark("127.0.0.1");
  check("on 127.0.0.1 the panel says to open the localhost origin, with no device button", await loadPage("http://" + addr)
    && await b.waitFor(`document.getElementById("approval-body").textContent.includes(${JSON.stringify(origin + "/host")}) && !document.querySelector("#approval .approval-approve")`, 20000),
    await text("#approval-body"));

  // ---- 3 on localhost: A's Ask, approved with the device ----
  mark("approve");
  check("the localhost page loads the token file; the header says A2, the person's approval by device, no payment", await loadPage(origin)
    && await b.waitFor(`document.querySelector("header .badge").textContent === "A2／fake agent／本人核准（virtual authenticator）／不付款"`, 15000), await text("header .badge"));
  const sd = await signs();
  check("A's run with its delegation, confirmed by A's own device (gap 4): the device asked once, no role play offered", await startRun("consumer-a", "pay-a", "show-a-01")
    && (await signs()) === sd + 1 && !(await b.evaluate(`!!document.querySelector(".agent-card .agent-sim")`)), await text("#run-status"));
  check("需要你處理: A's Ask with the summary, the sources apart, and the device actions", await pendingShown()
    && await b.waitFor(`(() => { const x = document.getElementById("approval-body").textContent; return x.includes("本人核准") && x.includes("Agent：fake") && x.includes("virtual authenticator（測試用）")
      && x.includes("付款：未啟用") && x.includes("0.50") && x.includes("用裝置拒絕這筆") && !x.includes("模擬核准"); })()`, 10000), await text("#approval-body"));
  check("the card and the scene word the wait as the person's device, never simulated", await b.waitFor(`${card("consumer-a")}.textContent.includes("請購仍等你核准（本人裝置）")`, 30000)
    && !(await b.evaluate(`${card("consumer-a")}.textContent + document.getElementById("approval-body").textContent`)).includes("（模擬）"), await b.evaluate(`${card("consumer-a")}.textContent`));
  const s0 = await signs();
  await shot("a2-1440x900-1-ask-summary");
  check("用已登記的裝置核准: 本人核准已驗證，付款未啟用。", await click("#approval .approval-approve") && await headIs("approved", "本人核准已驗證，付款未啟用。"), await text("#approval-body"));
  check("the device was asked once", (await signs()) === s0 + 1, String(await signs()));
  const a1 = await hostTask("show-a-01");
  check("the Store: approved, one webauthn grant mirroring the Signer, payment not enabled", a1.purchases[0].approval.state === "approved"
    && a1.purchases[0].approval.grant && a1.purchases[0].approval.grant.authorization_method === "webauthn" && a1.payment_capability === "not_enabled", JSON.stringify(a1.purchases[0].approval));
  check("focus is on 需要你處理's heading", await b.evaluate(`document.activeElement && document.activeElement.id === "approval-title"`));
  await shot("a2-1440x900-2-approved-unpaid");
  // Review R5: the drawer words A's grant by its own method (webauthn), never as simulated.
  check("the drawer: the person's grant, payment not enabled, nothing simulated", await click("#approval .approval-evidence")
    && await b.waitFor(`(() => { const d = document.getElementById("evidence-drawer"), x = d.textContent; return !d.hidden && x.includes("本人核准；付款未啟用")
      && x.includes("Signer 驗證本人已登記的裝置") && !x.includes("模擬 grant") && !x.includes("Signer 樁拒絕") && !document.body.textContent.includes("模擬 grant"); })()`, 15000),
    await text("#evidence-drawer"));
  await b.evaluate(`(document.getElementById("drawer-close").click(), true)`);

  // ---- 4 a second Ask of A, denied with the keyboard ----
  mark("deny by keyboard");
  const key = async (k) => {
    const code = { Tab: 9, Enter: 13 }[k];
    await b.send("Input.dispatchKeyEvent", { type: "keyDown", key: k, code: k, windowsVirtualKeyCode: code, nativeVirtualKeyCode: code, ...(k === "Enter" ? { text: "\r" } : {}) });
    await b.send("Input.dispatchKeyEvent", { type: "keyUp", key: k, code: k, windowsVirtualKeyCode: code, nativeVirtualKeyCode: code });
    await sleep(150);
  };
  const active = () => b.evaluate(`(document.activeElement && (document.activeElement.className || document.activeElement.id)) || ""`);
  check("A's second run asks", await startRun("consumer-a", "pay-a", "show-a-02") && await pendingShown());
  await b.evaluate(`(document.getElementById("approval-title").focus(), true)`);
  await key("Tab");
  check("Tab from the heading reaches 用已登記的裝置核准 first", (await active()).startsWith("approval-approve"), await active());
  await key("Tab");
  check("the next Tab reaches 用裝置拒絕這筆", (await active()).startsWith("approval-deny"), await active());
  await key("Enter");
  check("Enter on 用裝置拒絕這筆: 你已拒絕這筆（已驗證裝置）。, no grant", await headIs("denied", "你已拒絕這筆（已驗證裝置）。")
    && !(await hostTask("show-a-02")).purchases[0].approval.grant, await text("#approval-body"));

  // ---- 5 the other decision while one is open, then 取消這次驗證 ----
  mark("decision conflict");
  check("A's third run asks", await startRun("consumer-a", "pay-a", "show-a-03") && await pendingShown());
  const viewerA = t.a;
  const list = await (await fetch(origin + "/api/approvals", { headers: { Authorization: "Bearer " + viewerA } })).json();
  const sid = list.approvals.find((x) => x.task_id === "show-a-03").purchase.approval.approval_session_id;
  const other = await fetch(origin + "/api/approvals/" + sid + "/challenge", { method: "POST", headers: { Authorization: "Bearer " + viewerA, Origin: origin, "Content-Type": "application/json" },
    body: JSON.stringify({ request_id: "other-window-000000000001", decision: "approve" }) });
  check("another window asks to approve (a challenge, no device yet)", other.status === 200, String(other.status));
  const s1 = await signs();
  check("this page's 用裝置拒絕這筆: 另一個視窗正在核准這筆, and the device is not asked", await click("#approval .approval-deny") && await msg("另一個視窗正在核准這筆") && (await signs()) === s1,
    await text("#approval-body"));
  check("取消這次驗證 is offered for the open attempt", await b.waitFor(`!!document.querySelector("#approval .approval-cancel")`, 10000));
  check("取消這次驗證: closed by the Signer; the Ask still waits", await click("#approval .approval-cancel") && await msg("已取消這次驗證") && !!(await b.evaluate(`!!document.querySelector("#approval .approval-head[data-state=pending]")`)),
    await text("#approval-body"));
  check("now 用裝置拒絕這筆 is a new attempt: denied", await click("#approval .approval-deny") && await headIs("denied", "你已拒絕這筆（已驗證裝置）。"), await text("#approval-body"));

  // ---- 6 the Signer is down: unknown, never prompted by itself; back: continue ----
  mark("signer down");
  check("A's fourth run asks", await startRun("consumer-a", "pay-a", "show-a-04") && await pendingShown());
  await stopSigner();
  const s2 = await signs();
  check("用已登記的裝置核准 with the Signer down: 結果確認中, no device", await click("#approval .approval-approve") && await msg("確認中") && (await signs()) === s2, await text("#approval-body"));
  await shot("a2-1440x900-3-result-unknown");
  await sleep(5000);
  check("while it is down, polling never asks the device", (await signs()) === s2);
  await startSigner();
  check("the Signer back: the same IDs recover; the page offers to continue, still without a prompt", await msg("可以繼續驗證") && (await signs()) === s2, await text("#approval-body"));
  check("用已登記的裝置核准 continues the same attempt: approved", await click("#approval .approval-approve") && await headIs("approved", "本人核准已驗證，付款未啟用。") && (await signs()) === s2 + 1,
    await text("#approval-body"));

  // ---- 7 a customer switch while the challenge's reply is held ----
  mark("switch while held");
  check("B's run asks (B selected)", await startRun("consumer-b", "pay-b", "show-b-01") && await pendingShown());
  await b.send("Fetch.enable", { patterns: [{ urlPattern: "*/challenge", requestStage: "Response" }] });
  let held = null;
  b.on("Fetch.requestPaused", (p) => { held = p; });
  const s3 = await signs();
  await click("#approval .approval-approve");
  for (let i = 0; i < 50 && !held; i++) await sleep(100);
  check("B's challenge reply is held", !!held);
  check("choosing A meanwhile shows A's panel", await click(`.agent-card[data-consumer="consumer-a"] .agent-select`) || await b.evaluate(`(${card("consumer-a")}.click(), true)`));
  await b.waitFor(`document.getElementById("approval-body").textContent.includes("客人 A") || !!document.querySelector("#approval .approval-head")`, 10000);
  if (held) await b.send("Fetch.continueRequest", { requestId: held.requestId });
  await b.send("Fetch.disable");
  await sleep(3000);
  check("B's late reply asks no device and says nothing on A's panel", (await signs()) === s3 && !(await text("#approval-body")).includes("請在裝置上完成驗證"), await text("#approval-body"));
  await shot("a2-1440x900-4-main");

  // ---- 8 batch 3: 一起開始 with payment scenarios, both Asks named ----
  mark("start together");
  // B's Ask of 7 is still open: closed first (approved on B's own panel), so B has one Ask below.
  await click(`.agent-card[data-consumer="consumer-b"] .agent-select`);
  check("B's open Ask of 7 approved first, on B's panel", await b.waitFor(`document.getElementById("approval-body").textContent.includes("客人 B 的 Agent") && !!document.querySelector("#approval .approval-approve:not([disabled])")`, 30000)
    && await click("#approval .approval-approve") && await headIs("approved", "本人核准已驗證，付款未啟用。"), await text("#approval-body"));
  check("一起開始 offers both customers' scenarios, saying each confirms the delegation with their own device first",
    await b.waitFor(`!document.querySelector(".agent-card .row-stop") && (() => { const x = document.getElementById("start-all"); return !x.hidden && !x.disabled && x.textContent.includes("pay-a（A）") && x.textContent.includes("pay-b（B）") && x.textContent.includes("用自己的裝置確認委託"); })()`, 60000),
    await text("#start-all"));
  await click("#start-all");
  check("one click: A's and B's delegations confirmed, both runs started", await b.waitFor(`/A 已開始 show-a-\\d+；B 已開始 show-b-\\d+/.test(document.getElementById("run-status").textContent)`, 30000),
    await text("#run-status"));
  check("A's panel shows A's Ask and names B's, with a switch (nothing decided from that line)", await pendingShown()
    && await b.waitFor(`(document.querySelector("#approval .approval-other") || {}).textContent?.startsWith("客人 B 也有一筆請購等你核准：kr-market 0.50 test USDC")`, 30000)
    && await b.evaluate(`!document.querySelector("#approval .approval-other .approval-approve")`), await text("#approval-body"));
  await shot("a2-1440x900-5-together");
  const s4 = await signs();
  check("A approved on A's panel", await click("#approval .approval-approve") && await headIs("approved", "本人核准已驗證，付款未啟用。") && (await signs()) === s4 + 1, await text("#approval-body"));
  check("the switch shows B's own Ask, and names no one else now", await click("#approval .approval-switch")
    && await b.waitFor(`document.getElementById("approval-body").textContent.includes("客人 B 的 Agent") && !document.querySelector("#approval .approval-others")`, 30000), await text("#approval-body"));
  const s5 = await signs();
  check("B approved on B's own panel", await click("#approval .approval-approve") && await headIs("approved", "本人核准已驗證，付款未啟用。") && (await signs()) === s5 + 1, await text("#approval-body"));
} catch (e) {
  check("the run completed", false, e.message);
} finally {
  if (b) b.close();
  if (demo) await demo.stop();
  await stopSigner();
}
log(failures ? failures + " FAILED" : "ALL PASS (CDP virtual authenticator; not a real-device result)");
writeFileSync(join(out, "a2-ui.log"), lines.join("\n") + "\n");
console.log("evidence " + out);
process.exit(failures ? 1 : 0);
