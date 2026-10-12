// Real-browser check of P1-a (docs/commercial-p1a.md): the host page with
// an apa.pay.v1 catalog. Act as A (the fake-only simulated approver),
// confirm the delegation, the Agent asks, look at the one-purchase summary,
// approve (simulated) and see 「已核准（模擬），未啟用付款」 with one simulated
// grant; then a deny, an expiry, B's isolation and the keyboard. The demo
// host with the fake CLI (no model, no network, no key), a test HOME, the
// Unity Web build, Chrome over the DevTools protocol. The token file stands
// in for pairing; no token, code or capability is logged, and every
// screenshot is taken with the token fields empty.
//
//   node scripts/verify/p1a-ui.mjs [--unity-build DIR] [--out DIR] [--preview]
//
// --preview stops after the main line's screenshots (the first look).

import { execFileSync } from "node:child_process";
import { mkdtempSync, mkdirSync, readFileSync, writeFileSync, chmodSync } from "node:fs";
import { tmpdir } from "node:os";
import { join, resolve } from "node:path";
import { startDemo, startChrome, sleep } from "./harness.mjs";

const repo = resolve(new URL("../..", import.meta.url).pathname);
const arg = (k, d) => { const i = process.argv.indexOf(k); return i > 0 ? process.argv[i + 1] : d; };
const preview = process.argv.includes("--preview");
const unityBuild = resolve(arg("--unity-build", join(repo, "unity/AgentPaymentAuthority/Build/WebGL/Build")));
const stamp = new Date().toISOString().replace(/[-:]/g, "").replace(/\.\d+Z$/, "Z");
const commit = execFileSync("git", ["-C", repo, "rev-parse", "HEAD"]).toString().trim();
const dirty = execFileSync("git", ["-C", repo, "status", "--porcelain", "--untracked-files=no"]).toString().trim() !== "";
const out = resolve(arg("--out", join(repo, "artifacts/p1a", "p1a-ui-" + commit.slice(0, 12) + (dirty ? "-dirty" : "") + "-" + stamp)));
mkdirSync(out, { recursive: true });

const lines = [];
let failures = 0;
const started = Date.now();
function log(s) { lines.push(s); console.log(s); }
const mark = (name) => log("  [" + Math.round((Date.now() - started) / 1000) + " s] " + name);
function check(name, ok, detail = "") { log((ok ? "PASS " : "FAIL ") + name + (detail ? "  [" + detail + "]" : "")); if (!ok) failures++; }

const tmp = mkdtempSync(join(tmpdir(), "apa-p1a-"));
chmodSync(tmp, 0o700);
const home = join(tmp, "home");
mkdirSync(home);
const bin = join(tmp, "bin");
mkdirSync(bin);
execFileSync("go", ["build", "-o", join(bin, "demo"), "./cmd/demo"], { cwd: repo, stdio: "inherit" });
execFileSync("go", ["build", "-o", join(bin, "fake-claude"), "./cmd/fake-claude"], { cwd: repo, stdio: "inherit" });

// A and B: the P1-a catalog's main line (asks, then the Agent ends). A
// second entry of A with a 5 s approval deadline, for the expiry.
const tool = (name, product) => ({ do: "tool", tool: name, ...(product ? { args: { product_id: product } } : {}) });
const scenario = (name, steps) => { const p = join(tmp, name + ".json"); writeFileSync(p, JSON.stringify({ steps })); return p; };
const main = scenario("main", [tool("list_products"), tool("get_quote", "kr-market"), tool("request_purchase", "kr-market"), { do: "result", text: "done" }]);
const terms = (deadline) => ({ purpose: "買一份韓國市場簡報（數位資料）", task_budget: "5000000", allow_max: "100000", approval_deadline_seconds: deadline, valid_seconds: 1800 });
const catalog = join(tmp, "scenarios.json");
writeFileSync(catalog, JSON.stringify({ description: "p1a-ui", scenarios: [
  { id: "pay-a", consumer_id: "consumer-a", contract: "apa.pay.v1", scenario: main, label: "委託後選購：韓國市場簡報", delegation: terms(60) },
  { id: "pay-b", consumer_id: "consumer-b", contract: "apa.pay.v1", scenario: main, label: "委託後選購：韓國市場簡報", delegation: terms(60) },
  { id: "pay-a-short", consumer_id: "consumer-a", contract: "apa.pay.v1", scenario: main, label: "期限 5 秒", delegation: terms(5) },
] }));

let demo, b;
const missing = new Set();
try {
  log("commit " + commit + (dirty ? " (working tree has uncommitted changes)" : ""));
  const state = join(tmp, "state");
  demo = startDemo({ repo, bin, catalog, unityBuild, state, home });
  const addr = await demo.ready();
  const t = demo.tokens();
  const tokenFile = join(tmp, "tokens.json");
  writeFileSync(tokenFile, readFileSync(join(state, "tokens.json")));
  b = await startChrome(join(tmp, "chrome-profile"));
  await b.send("Emulation.setFocusEmulationEnabled", { enabled: true });
  // a 404's path (no query: nothing secret is in a URL here anyway)
  await b.send("Network.enable");
  b.on("Network.responseReceived", (p) => { if (p.response.status === 404) missing.add(new URL(p.response.url).pathname); });
  const size = (w, h) => b.send("Emulation.setDeviceMetricsOverride", { width: w, height: h, deviceScaleFactor: 1, mobile: w < 500 });
  const hostTask = async (id) => (await fetch("http://" + addr + "/api/tasks/" + id, { headers: { Authorization: "Bearer " + t.host } })).json();
  const hostEvents = async (id) => (await fetch("http://" + addr + "/api/tasks/" + id + "/events", { headers: { Authorization: "Bearer " + t.host } })).json();
  const card = (c) => `document.querySelector('.agent-card[data-consumer="${c}"]')`;
  const text = (sel) => b.evaluate(`(document.querySelector(${JSON.stringify(sel)}) || {}).textContent || ""`);
  const click = (sel) => b.evaluate(`(() => { const e = document.querySelector(${JSON.stringify(sel)}); if (!e || e.disabled) return false; e.click(); return true; })()`);
  async function shot(name) {
    await b.evaluate(`(window.scrollTo(0, 0), true)`);
    await sleep(1000);
    check("screenshot " + name + ": token fields empty", await b.evaluate(`document.getElementById("token").value === "" && document.getElementById("pair-code").value === ""`));
    const png = await b.send("Page.captureScreenshot", { format: "png" });
    writeFileSync(join(out, name + ".png"), Buffer.from(png.data, "base64"));
    log("  screenshot " + name + ".png");
  }
  async function loadPage() {
    await b.send("Page.navigate", { url: "http://" + addr + "/host" });
    await b.waitFor(`document.readyState === "complete"`);
    await b.waitFor(`["ready","rendered","failed","scene","syncing"].includes(document.getElementById("unity-status").dataset.state)`, 90000);
    const doc = await b.send("DOM.getDocument", {});
    const q = await b.send("DOM.querySelector", { nodeId: doc.root.nodeId, selector: "#token-file" });
    await b.send("DOM.setFileInputFiles", { nodeId: q.nodeId, files: [tokenFile] });
    return b.waitFor(`document.getElementById("creds").dataset.mode === "file"`, 20000);
  }
  // shows waits for Unity's report of a customer's phase (drawing the page
  // every 2 s, as ux1-ui.mjs does for headless Chrome).
  const shows = async (c, task, phase) => {
    const want = `(() => { const e = document.querySelector('#scene-status li[data-consumer="${c}"]'); return !!e && e.dataset.task === "${task}" && e.dataset.phase === "${phase}" && e.dataset.shown === "true"; })()`;
    for (const end = Date.now() + 30000; Date.now() < end; await sleep(2000)) {
      if (await b.waitFor(want, 2000)) return true;
      await b.send("Page.captureScreenshot", { format: "png" });
    }
    return false;
  };

  // ---- 1 the entry ----
  mark("load");
  await size(1440, 900);
  check("the token file loads; the header says fake, P1-a, simulated approval, no payment", await loadPage() && await b.waitFor(`document.querySelector("header .badge").dataset.mode === "fake"
    && document.querySelector("header .badge").textContent === "P1-a／fake agent／模擬核准／不付款"`, 15000));
  check("the scene owns the shop", await b.waitFor(`!document.getElementById("scene-status").hidden`, 90000));
  check("each card offers 以 X 模擬 and 建立委託 (disabled until simulated); no plain start for apa.pay.v1",
    await b.waitFor(`!!${card("consumer-a")}?.querySelector(".agent-sim") && ${card("consumer-a")}.querySelector('.agent-delegate[data-scenario="pay-a"]').disabled
      && !${card("consumer-a")}.querySelector(".agent-start") && !!${card("consumer-b")}?.querySelector(".agent-sim")`, 15000));
  check("需要你處理 asks to choose a customer to act as", await b.waitFor(`!document.getElementById("approval").hidden && document.getElementById("approval-body").textContent.includes("以 A 模擬")`, 10000));

  // ---- 2 act as A, confirm the delegation, the Agent asks ----
  mark("act as A");
  check("以 A 模擬: the header chip says it is the host playing A", await click(`.agent-card[data-consumer="consumer-a"] .agent-sim`)
    && await b.waitFor(`!document.getElementById("sim-chip").hidden && document.getElementById("sim-chip").textContent.includes("角色模擬：客人 A（主持人扮演，不是本人驗證）")`, 10000));
  check("建立委託 opens the host's terms; nothing is editable", await click(`.agent-card[data-consumer="consumer-a"] .agent-delegate[data-scenario="pay-a"]`)
    && await b.waitFor(`(() => { const d = document.querySelector('.delegation[data-scenario="pay-a"]'); return !!d && d.textContent.includes("5.00 test USDC") && d.textContent.includes("超過 0.10 一律要你核准")
      && d.querySelectorAll("input, select, textarea").length === 0; })()`, 5000), await text(`.delegation`));
  await shot("p1a-1440x900-1-delegation");
  check("確認委託並開始: the run starts with its delegation", await click(`.delegation[data-scenario="pay-a"] .delegation-confirm`)
    && await b.waitFor(`document.getElementById("run-status").textContent.includes("已開始 show-a-01")`, 20000), await text("#run-status"));
  check("A's Ask appears in 需要你處理 with the one-purchase summary", await b.waitFor(`(() => { const x = document.getElementById("approval-body").textContent;
    return x.includes("核准這一筆付款？") && x.includes("0.50") && x.includes("Base Sepolia") && x.includes("預算 5.00；超過 0.10 一律要你核准") && x.includes("期限"); })()`, 30000),
    await text("#approval-body"));
  check("the summary says simulated, not a person's verification, no payment", (await text("#approval-body")).includes("模擬核准") && (await text("#approval-body")).includes("不是本人驗證") && (await text("#approval-body")).includes("不會付款"));
  check("the countdown comes from the server's deadline", await b.waitFor(`/剩約 \\d+ 秒/.test(document.getElementById("approval-countdown").textContent)`, 5000), await text("#approval-countdown"));
  check("A's card: the Agent ended, the request still waits (apart)", await b.waitFor(`${card("consumer-a")}.dataset.state === "ended"
    && ${card("consumer-a")}.querySelector(".agent-business").textContent.includes("等你核准")`, 30000), await b.evaluate(`${card("consumer-a")}.textContent`));
  check("the scene shows A waiting at the counter", await shows("consumer-a", "show-a-01", "waiting"));
  await shot("p1a-1440x900-2-ask-summary");

  // ---- 3 approve (simulated) ----
  mark("approve");
  check("核准此筆（模擬）: 已核准（模擬），未啟用付款", await click("#approval .approval-approve")
    && await b.waitFor(`(document.querySelector("#approval .approval-head[data-state=approved] h3") || {}).textContent === "已核准（模擬），未啟用付款"`, 15000), await text("#approval-body"));
  check("the result says the stub refuses the simulated grant", (await text("#approval-body")).includes("GRANT_METHOD_NOT_ACCEPTED"));
  check("focus moves to 需要你處理's heading", await b.evaluate(`document.activeElement && document.activeElement.id === "approval-title"`));
  await b.waitFor(`false`, 500);
  const ta = await hostTask("show-a-01");
  const pa = ta.purchases && ta.purchases[0];
  check("the Store: apa.pay.v1, approved, one simulated grant, payment not_enabled", ta.contract_version === "apa.pay.v1" && ta.payment_capability === "not_enabled"
    && pa && pa.approval && pa.approval.state === "approved" && pa.approval.grant && pa.approval.grant.authorization_method === "simulated" && pa.decision.payment_status === "not_enabled",
    JSON.stringify(pa && pa.approval));
  const ea = (await hostEvents("show-a-01")).events.map((e) => e.event_type);
  check("events: DELEGATION_BOUND, APPROVAL_SESSION_OPENED, APPROVED, GRANT_ISSUED once each; no payment or delivery",
    ["DELEGATION_BOUND", "APPROVAL_SESSION_OPENED", "APPROVED", "GRANT_ISSUED"].every((x) => ea.filter((y) => y === x).length === 1)
    && !ea.some((x) => /SETTLED|DELIVERED|PAID/.test(x)), ea.join(","));
  check("the scene: A stays at the counter with no payment (Unity's M1 phase allowed_unpaid)", await shows("consumer-a", "show-a-01", "allowed_unpaid"));
  check("the panel card says approved, payment not enabled", await b.waitFor(`document.getElementById("card").textContent.includes("已核准（模擬），未啟用付款")`, 15000), await text("#card"));
  await shot("p1a-1440x900-3-approved");
  await size(390, 844);
  await shot("p1a-390x844-3-approved");
  await size(1440, 900);
  if (preview) throw new Error("preview: stopped after the main line");

  // ---- 4 a second purchase of A, denied with the keyboard only ----
  mark("deny by keyboard");
  const key = async (k) => {
    const code = { Tab: 9, Enter: 13 }[k];
    await b.send("Input.dispatchKeyEvent", { type: "keyDown", key: k, code: k, windowsVirtualKeyCode: code, nativeVirtualKeyCode: code, ...(k === "Enter" ? { text: "\r" } : {}) });
    await b.send("Input.dispatchKeyEvent", { type: "keyUp", key: k, code: k, windowsVirtualKeyCode: code, nativeVirtualKeyCode: code });
    await sleep(150);
  };
  const active = () => b.evaluate(`(document.activeElement && (document.activeElement.className || document.activeElement.id)) || ""`);
  check("建立委託 → 確認 again: A's second run", await click(`.agent-card[data-consumer="consumer-a"] .agent-delegate[data-scenario="pay-a"]`)
    && await click(`.delegation[data-scenario="pay-a"] .delegation-confirm`) && await b.waitFor(`document.getElementById("run-status").textContent.includes("已開始 show-a-02")`, 20000), await text("#run-status"));
  check("A's new Ask is the one 需要你處理 shows", await b.waitFor(`!!document.querySelector("#approval .approval-head[data-state=pending]")`, 30000), await text("#approval-body"));
  await b.evaluate(`(document.getElementById("approval-title").focus(), true)`);
  await key("Tab");
  check("Tab from the heading reaches 核准此筆（模擬） first", (await active()).startsWith("approval-approve"), await active());
  await key("Tab");
  check("the next Tab reaches 拒絕", (await active()).startsWith("approval-deny"), await active());
  await key("Enter");
  check("Enter on 拒絕: 已拒絕, no grant", await b.waitFor(`(document.querySelector("#approval .approval-head[data-state=denied] h3") || {}).textContent === "已拒絕"`, 15000), await text("#approval-body"));
  const td = await hostTask("show-a-02");
  check("the Store: denied, no grant", td.purchases[0].approval.state === "denied" && !td.purchases[0].approval.grant, JSON.stringify(td.purchases[0].approval));
  check("the scene: A leaves with the denial's code (Unity's M1 phase denied)", await shows("consumer-a", "show-a-02", "denied"));
  await shot("p1a-1440x900-4-denied");

  // ---- 5 an Ask whose deadline passes (5 s): the backend expires it ----
  mark("expiry");
  check("A's 5 s run asks", await click(`.agent-card[data-consumer="consumer-a"] .agent-delegate[data-scenario="pay-a-short"]`)
    && await click(`.delegation[data-scenario="pay-a-short"] .delegation-confirm`) && await b.waitFor(`!!document.querySelector("#approval .approval-head[data-state=pending]")`, 30000));
  check("at the deadline: 已過期，無法核准 (the backend's word), no buttons", await b.waitFor(`(document.querySelector("#approval .approval-head[data-state=expired] h3") || {}).textContent === "已過期，無法核准"
    && !document.querySelector("#approval .approval-approve")`, 20000), await text("#approval-body"));
  const te = await hostTask("show-a-03");
  check("the Store: expired, no grant", te.purchases[0].approval.state === "expired" && !te.purchases[0].approval.grant);
  await shot("p1a-1440x900-5-expired");

  // ---- 6 act as B: A's capability goes; B sees only B's ----
  mark("act as B");
  check("以 B 模擬: the chip says B; A's card offers 以 A 模擬 again", await click(`.agent-card[data-consumer="consumer-b"] .agent-sim`)
    && await b.waitFor(`document.getElementById("sim-chip").textContent.includes("客人 B") && !!${card("consumer-a")}.querySelector(".agent-sim")`, 10000));
  check("B has nothing to approve; A's sessions are not shown", await b.waitFor(`document.getElementById("approval-body").textContent.includes("客人 B 目前沒有需要你處理的請購")`, 10000), await text("#approval-body"));
  check("B's delegation and run: B's Ask, B's wallet", await click(`.agent-card[data-consumer="consumer-b"] .agent-delegate[data-scenario="pay-b"]`)
    && await click(`.delegation[data-scenario="pay-b"] .delegation-confirm`)
    && await b.waitFor(`!!document.querySelector("#approval .approval-head[data-state=pending]") && document.getElementById("approval-body").textContent.includes("客人 B 的測試錢包 0x2222")`, 30000), await text("#approval-body"));
  const ra = await fetch("http://" + addr + "/api/sim/approvals", { headers: { Authorization: "Bearer " + t.op } });
  check("the operator token does not read approvals", ra.status === 403);
  await shot("p1a-1440x900-6-b-ask");
  for (const [w, h] of [[1280, 900], [390, 844]]) { await size(w, h); await shot("p1a-" + w + "x" + h + "-6-b-ask"); }
  await size(1440, 900);
  check("approve B: B's own grant; A's results untouched", await click("#approval .approval-approve")
    && await b.waitFor(`(document.querySelector("#approval .approval-head[data-state=approved] h3") || {}).textContent === "已核准（模擬），未啟用付款"`, 15000)
    && (await hostTask("show-a-02")).purchases[0].approval.state === "denied" && (await hostTask("show-b-01")).purchases[0].approval.grant.authorization_method === "simulated");

  // ---- 7 review of f95415d R1: the focus never moves to another item's button ----
  mark("focus stays with its item");
  check("以 A 模擬 again", await click(`.agent-card[data-consumer="consumer-a"] .agent-sim`) && await b.waitFor(`document.getElementById("sim-chip").textContent.includes("客人 A")`, 10000));
  const runA = async (scenario, task) => click(`.agent-card[data-consumer="consumer-a"] .agent-delegate[data-scenario="${scenario}"]`)
    && await click(`.delegation[data-scenario="${scenario}"] .delegation-confirm`) && await b.waitFor(`document.getElementById("run-status").textContent.includes("已開始 ${task}")`, 20000)
    && await b.waitFor(`(document.querySelector("#approval .approval-approve") || {}).dataset?.task === "${task}"`, 30000);
  check("A's 60 s request (show-a-04), then A's 5 s request (show-a-05): the panel shows the newer", await runA("pay-a", "show-a-04") && await runA("pay-a-short", "show-a-05"),
    await text("#approval-body"));
  await b.evaluate(`(document.getElementById("approval-title").focus(), true)`);
  await key("Tab");
  check("the focus is on show-a-05's 核准此筆（模擬）", await b.evaluate(`document.activeElement.classList.contains("approval-approve") && document.activeElement.dataset.task === "show-a-05"`), await active());
  check("show-a-05 expires; the panel now shows show-a-04 pending", await b.waitFor(`(document.querySelector("#approval .approval-approve") || {}).dataset?.task === "show-a-04"`, 20000), await text("#approval-body"));
  check("the focus went to the heading, not to show-a-04's buttons", await b.evaluate(`document.activeElement && document.activeElement.id === "approval-title"`), await active());
  await key("Enter");
  await sleep(1500);
  check("Enter after the switch approved nothing: show-a-04 still pending, no grant", (await hostTask("show-a-04")).purchases[0].approval.state === "pending"
    && !(await hostTask("show-a-04")).purchases[0].approval.grant, JSON.stringify((await hostTask("show-a-04")).purchases[0].approval));
  check("another 5 s request (show-a-06) is the newer one", await runA("pay-a-short", "show-a-06"), await text("#approval-body"));
  await b.evaluate(`(document.getElementById("approval-title").focus(), true)`);
  for (let i = 0; i < 3; i++) await key("Tab");
  check("the focus is on show-a-06's 查看技術證據", await b.evaluate(`document.activeElement.classList.contains("approval-evidence") && document.activeElement.dataset.task === "show-a-06"`), await active());
  check("R1 rebuild: show-a-06 expires, the panel shows show-a-04, the focus left for the heading", await b.waitFor(`(document.querySelector("#approval .approval-approve") || {}).dataset?.task === "show-a-04"`, 20000)
    && await b.evaluate(`document.activeElement && document.activeElement.id === "approval-title"`), await active());
  await key("Enter");
  await sleep(1500);
  check("show-a-04 still pending after that Enter", (await hostTask("show-a-04")).purchases[0].approval.state === "pending");
  check("the flow ran to its end", true);
} catch (e) {
  if (!(preview && String(e.message).startsWith("preview"))) check("the flow ran to its end", false, String(e && e.stack || e).slice(0, 400));
} finally {
  if (b) {
    const errs = b.consoleText.filter((x) => !/favicon/.test(x));
    log("page errors: " + (errs.length ? errs.join(" | ") : "none"));
    log("404 paths: " + ([...missing].join(", ") || "none"));
    b.close();
  }
  if (demo) await demo.stop();
  log("result: " + (failures ? "FAIL (" + failures + ")" : "PASS"));
  writeFileSync(join(out, "p1a-ui.txt"), lines.join("\n") + "\n");
  console.log("evidence: " + out);
  process.exit(failures ? 1 : 0);
}
