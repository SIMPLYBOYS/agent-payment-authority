// Real-browser check of UX3 (docs/commercial-scenes.md): the host page with
// the shipped three-scene catalog (testdata/demo/scenarios-ux3.json) and a
// credential directory of test keys. The scene is chosen first; each scene
// shows its own room (Unity's "layout" report), only its scenarios and only
// its runs' customers. The cafe's lunch set is within the delegation: Allow,
// nothing to approve. The mart's coffee beans ask; the credential is revoked
// while it waits and the approval closes it. The data room's brief asks and
// is approved (simulated). Desktop and phone screenshots of each scene. The
// demo host with the fake CLI (no model, no network, synthetic test keys),
// the Unity Web build, Chrome over the DevTools protocol. The token file
// stands in for pairing; no token, code or capability is logged, and every
// screenshot is taken with the token fields empty. Nothing pays.
//
//   node scripts/verify/ux3-ui.mjs [--unity-build DIR] [--out DIR]

import { execFileSync } from "node:child_process";
import { mkdtempSync, mkdirSync, readFileSync, writeFileSync, chmodSync } from "node:fs";
import { tmpdir } from "node:os";
import { join, resolve } from "node:path";
import { startDemo, startChrome, sleep } from "./harness.mjs";

const repo = resolve(new URL("../..", import.meta.url).pathname);
const arg = (k, d) => { const i = process.argv.indexOf(k); return i > 0 ? process.argv[i + 1] : d; };
const unityBuild = resolve(arg("--unity-build", join(repo, "unity/AgentPaymentAuthority/Build/WebGL/Build")));
const stamp = new Date().toISOString().replace(/[-:]/g, "").replace(/\.\d+Z$/, "Z");
const commit = execFileSync("git", ["-C", repo, "rev-parse", "HEAD"]).toString().trim();
const dirty = execFileSync("git", ["-C", repo, "status", "--porcelain", "--untracked-files=no"]).toString().trim() !== "";
const out = resolve(arg("--out", join(repo, "artifacts/ux3", "ux3-ui-" + commit.slice(0, 12) + (dirty ? "-dirty" : "") + "-" + stamp)));
mkdirSync(out, { recursive: true });

const lines = [];
let failures = 0;
const started = Date.now();
function log(s) { lines.push(s); console.log(s); }
const mark = (name) => log("  [" + Math.round((Date.now() - started) / 1000) + " s] " + name);
function check(name, ok, detail = "") { log((ok ? "PASS " : "FAIL ") + name + (detail ? "  [" + detail + "]" : "")); if (!ok) failures++; }

const tmp = mkdtempSync(join(tmpdir(), "apa-ux3-"));
chmodSync(tmp, 0o700);
const home = join(tmp, "home");
mkdirSync(home);
const bin = join(tmp, "bin");
mkdirSync(bin);
for (const c of ["demo", "fake-claude", "apa-issuer"]) execFileSync("go", ["build", "-o", join(bin, c), "./cmd/" + c], { cwd: repo, stdio: "inherit" });
const cred = join(tmp, "cred");
const issuer = (...a) => execFileSync(join(bin, "apa-issuer"), [a[0], "--dir", cred, ...a.slice(1)]).toString().trim();
log(issuer("init", "--issuer", "issuer-demo", "--consumers", "consumer-a,consumer-b"));
const catalog = join(repo, "testdata/demo/scenarios-ux3.json");

let demo, b;
const missing = new Set();
try {
  log("commit " + commit + (dirty ? " (working tree has uncommitted changes)" : ""));
  const state = join(tmp, "state");
  demo = startDemo({ repo, bin, catalog, unityBuild, state, home, extra: ["--credential-dir", cred] });
  const addr = await demo.ready();
  const t = demo.tokens();
  const tokenFile = join(tmp, "tokens.json");
  writeFileSync(tokenFile, readFileSync(join(state, "tokens.json")));
  b = await startChrome(join(tmp, "chrome-profile"));
  await b.send("Emulation.setFocusEmulationEnabled", { enabled: true });
  await b.send("Network.enable");
  b.on("Network.responseReceived", (p) => { if (p.response.status === 404) missing.add(new URL(p.response.url).pathname); });
  const size = (w, h) => b.send("Emulation.setDeviceMetricsOverride", { width: w, height: h, deviceScaleFactor: 1, mobile: w < 500 });
  const hostTask = async (id) => (await fetch("http://" + addr + "/api/tasks/" + id, { headers: { Authorization: "Bearer " + t.host } })).json();
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
  async function phone(name) {
    await size(390, 844);
    check(name + " at 390 px: no horizontal page scroll", await b.waitFor(`document.documentElement.scrollWidth <= 390`, 5000), await b.evaluate(`String(document.documentElement.scrollWidth)`));
    await shot(name);
    await size(1440, 900);
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
  const room = () => b.evaluate(`document.getElementById("unity-canvas").dataset.room || ""`);
  // inRoom: Unity reported this room for the page's current scene (the canvas
  // dataset is refreshed with the page's scene line).
  const inRoom = async (key) => {
    for (const end = Date.now() + 20000; Date.now() < end; await sleep(500)) if ((await room()).endsWith(":" + key)) return true;
    return false;
  };
  const cards = () => b.evaluate(`[...document.querySelectorAll("#run-scenarios .agent-card")].map((e) => e.dataset.consumer).join(",")`);
  const sceneCustomers = () => b.evaluate(`[...document.querySelectorAll("#scene-status li")].map((e) => e.dataset.consumer).join(",")`);
  const shows = async (c, task, phase) => {
    const want = `(() => { const e = document.querySelector('#scene-status li[data-consumer="${c}"]'); return !!e && e.dataset.task === "${task}" && e.dataset.phase === "${phase}" && e.dataset.shown === "true"; })()`;
    for (const end = Date.now() + 30000; Date.now() < end; await sleep(2000)) {
      if (await b.waitFor(want, 2000)) return true;
      await b.send("Page.captureScreenshot", { format: "png" }); // headless Chrome draws the canvas only when asked
    }
    return false;
  };
  const choose = async (key) => click(`#venue-picker .venue-pick[data-venue="${key}"]`) && b.waitFor(`document.querySelector('#venue-picker .venue-pick[data-venue="${key}"]').getAttribute("aria-pressed") === "true"`, 5000);
  const start = async (c, id, task) => click(`.agent-card[data-consumer="${c}"] .agent-sim`)
    && await b.waitFor(`document.getElementById("sim-chip").textContent.includes("客人 ${c.slice(-1).toUpperCase()}")`, 10000)
    && await click(`.agent-card[data-consumer="${c}"] .agent-delegate[data-scenario="${id}"]`)
    && await click(`.delegation[data-scenario="${id}"] .delegation-confirm`)
    && await b.waitFor(`document.getElementById("run-status").textContent.includes("已開始 ${task}")`, 20000);

  // ---- 1 the entry: the scene first, the main scene chosen ----
  mark("load");
  await size(1440, 900);
  check("the page loads with the token file; the scene owns the shop", await loadPage() && await b.waitFor(`!document.getElementById("scene-status").hidden`, 90000));
  check("the scene choice: 資料研究室, 街角咖啡 示範情境, 巷口便利商店 示範情境, the main scene chosen",
    await b.waitFor(`(() => { const bs = [...document.querySelectorAll("#venue-picker .venue-pick")]; return !document.getElementById("venue-picker").hidden && bs.map((x) => x.dataset.venue).join() === "data,cafe,mart"
      && bs[0].getAttribute("aria-pressed") === "true" && !bs[0].querySelector(".venue-demo") && bs[1].textContent === "街角咖啡 示範情境" && bs[2].textContent === "巷口便利商店 示範情境"; })()`, 15000), await text("#venue-picker"));
  check("Unity shows the data room", await inRoom("data"), await room());
  check("the header names the scene", (await text("#venue-chip")) === "情境：資料研究室", await text("#venue-chip"));
  check("the data room's cards: A only (data-a)", (await cards()) === "consumer-a" && await b.evaluate(`!!document.querySelector('.agent-delegate[data-scenario="data-a"]')`), await cards());
  await shot("ux3-1440x900-1-data-entry");

  // ---- 2 the cafe: B's lunch set, within the delegation: Allow ----
  mark("cafe");
  check("choose 街角咖啡: Unity shows the cafe; the cards are B's (cafe-b)", await choose("cafe") && await inRoom("cafe") && (await cards()) === "consumer-b", (await room()) + " " + (await cards()));
  check("the header says the cafe is a demo scene", (await text("#venue-chip")) === "情境：街角咖啡（示範情境・示意商品）", await text("#venue-chip"));
  check("以 B 模擬, 建立委託, 確認: B's cafe run starts", await start("consumer-b", "cafe-b", "show-b-01"), await text("#run-status"));
  check("the scene: B at the counter, within the delegation (allowed_unpaid)", await shows("consumer-b", "show-b-01", "allowed_unpaid"));
  const tb = await hostTask("show-b-01");
  check("the Store: the cafe's lunch set, Allow WITHIN_ALLOW_MAX, payment not enabled",
    tb.purchases.length === 1 && tb.purchases[0].purchase_order_id === "po-show-b-01-cafe-lunch-set" && tb.purchases[0].decision.policy_verdict === "Allow"
      && tb.purchases[0].decision.reason === "WITHIN_ALLOW_MAX" && tb.purchases[0].decision.payment_status === "not_enabled", JSON.stringify(tb.purchases.map((p) => [p.purchase_order_id, p.decision])));
  check("nothing to approve for B", await b.waitFor(`!document.querySelector("#approval .approval-head[data-state=pending]")`, 5000));
  check("the drawer's 情境 row: 街角咖啡, a demo scene, delivery after payment", await click("#show-evidence")
    && await b.waitFor(`(() => { const x = document.getElementById("evidence-drawer").textContent; return x.includes("街角咖啡（示範情境・示意商品，沒有接 POS、庫存或物流）・交付：出餐口叫號（付款後才交付）"); })()`, 15000),
    (await text("#evidence")).slice(0, 300));
  await shot("ux3-1440x900-2-cafe-allow");
  await b.evaluate(`(document.getElementById("drawer-close").click(), true)`);
  await phone("ux3-390x844-2-cafe-allow");

  // ---- 3 the mart: A's coffee beans ask; revoked while waiting; closed ----
  mark("mart");
  check("choose 巷口便利商店: Unity shows the mart; B's cafe customer leaves the scene; the cards are A's (mart-a)",
    await choose("mart") && await inRoom("mart") && await b.waitFor(`![...document.querySelectorAll("#scene-status li")].some((e) => e.dataset.consumer === "consumer-b")`, 10000) && (await cards()) === "consumer-a",
    (await room()) + " scene " + (await sceneCustomers()) + " cards " + (await cards()));
  check("以 A 模擬, 建立委託, 確認: A's mart run asks", await start("consumer-a", "mart-a", "show-a-01")
    && await b.waitFor(`!!document.querySelector("#approval .approval-head[data-state=pending]")`, 30000), await text("#approval-body"));
  const sum = await text("#approval-body");
  check("the approval summary: 商品・示意商品, 情境 巷口便利商店（示範情境）, 交付方式 取貨櫃（付款後才交付）",
    sum.includes("商品 coffee-beans（商品・示意商品・付款確認後交付；本片不付款）") && sum.includes("情境巷口便利商店（示範情境）") && sum.includes("交付方式取貨櫃（付款後才交付）"), sum.slice(0, 300));
  check("the scene: A waiting at the mart's counter", await shows("consumer-a", "show-a-01", "waiting"));
  await shot("ux3-1440x900-3-mart-ask");
  await phone("ux3-390x844-3-mart-ask");
  log("  " + issuer("revoke", "--task", "show-a-01"));
  check("核准此筆（模擬） after the revocation: 憑證已撤銷，這筆已關閉", await click("#approval .approval-approve")
    && await b.waitFor(`(document.querySelector("#approval .approval-head[data-state=withdrawn] h3") || {}).textContent === "憑證已撤銷，這筆已關閉"`, 15000), await text("#approval-body"));
  const ta = await hostTask("show-a-01");
  check("the Store: the mart's beans, withdrawn with CREDENTIAL_REVOKED, no grant",
    ta.purchases[0].purchase_order_id === "po-show-a-01-mart-coffee-beans" && ta.purchases[0].approval_closed === "withdrawn" && ta.purchases[0].closed_reason === "CREDENTIAL_REVOKED" && !ta.purchases[0].approval.grant,
    JSON.stringify(ta.purchases[0].approval));
  check("the scene: A leaves as withdrawn", await shows("consumer-a", "show-a-01", "withdrawn"));
  await shot("ux3-1440x900-4-mart-revoked");

  // ---- 4 the data room: A's brief asks and is approved (simulated) ----
  mark("data");
  check("choose 資料研究室: Unity shows the data room; A's mart customer leaves the scene",
    await choose("data") && await inRoom("data") && await b.waitFor(`document.querySelectorAll("#scene-status li").length === 0`, 10000), (await room()) + " scene " + (await sceneCustomers()));
  check("建立委託, 確認: A's data run asks", await click(`.agent-card[data-consumer="consumer-a"] .agent-delegate[data-scenario="data-a"]`)
    && await click(`.delegation[data-scenario="data-a"] .delegation-confirm`) && await b.waitFor(`document.getElementById("run-status").textContent.includes("已開始 show-a-02")`, 20000)
    && await b.waitFor(`!!document.querySelector("#approval .approval-head[data-state=pending]")`, 30000), await text("#approval-body"));
  check("the summary: 數位資料, 情境 資料研究室, 交付方式 下載終端（付款後才交付）", (await text("#approval-body")).includes("數位資料") && (await text("#approval-body")).includes("情境資料研究室交付方式下載終端（付款後才交付）"), (await text("#approval-body")).slice(0, 300));
  check("approve: 已核准（模擬），未啟用付款", await click("#approval .approval-approve")
    && await b.waitFor(`(document.querySelector("#approval .approval-head[data-state=approved] h3") || {}).textContent === "已核准（模擬），未啟用付款"`, 15000)
    && (await hostTask("show-a-02")).purchases[0].approval.grant.authorization_method === "simulated", await text("#approval-body"));
  check("the scene: only A, in the data room", await b.waitFor(`[...document.querySelectorAll("#scene-status li")].map((e) => e.dataset.consumer).join() === "consumer-a"`, 15000), await sceneCustomers());
  await shot("ux3-1440x900-5-data-approved");
  await phone("ux3-390x844-5-data-approved");
  check("the flow ran to its end", true);
} catch (e) {
  check("the flow ran to its end", false, String(e && e.stack || e).slice(0, 400));
} finally {
  if (b) {
    // A network status is not a page error: the 404s of a task read before
    // the task exists and the 409 of the approval after the revocation are
    // expected; the paths are listed below.
    const errs = b.consoleText.filter((x) => !/favicon|Failed to load resource/.test(x));
    check("no page errors (exceptions, console errors)", errs.length === 0, errs.join(" | "));
    log("404 paths: " + ([...missing].join(", ") || "none"));
    b.close();
  }
  if (demo) await demo.stop();
  log("result: " + (failures ? "FAIL (" + failures + ")" : "PASS"));
  writeFileSync(join(out, "ux3-ui.txt"), lines.join("\n") + "\n");
  console.log("evidence: " + out);
  process.exit(failures ? 1 : 0);
}
