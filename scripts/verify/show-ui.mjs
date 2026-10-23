// Real-browser check of the audience window (docs/commercial-gap2.md): the
// host page with the shipped three-scene catalog and test keys, and /show in
// a second tab of the same browser. While the window is open the scene is
// the window's: the host page hides its own shop, Unity's reports come back
// to it, and the window shows each customer's five steps in plain words. It
// calls no API and holds no token. When it closes, the scene comes back to
// the host page. The demo host with the fake CLI (no model, no network,
// synthetic test keys), the Unity Web build, Chrome over the DevTools
// protocol. Nothing pays.
//
//   node scripts/verify/show-ui.mjs [--unity-build DIR] [--out DIR]

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
const out = resolve(arg("--out", join(repo, "artifacts/gap2", "show-ui-" + commit.slice(0, 12) + (dirty ? "-dirty" : "") + "-" + stamp)));
mkdirSync(out, { recursive: true });

const lines = [];
let failures = 0;
function log(s) { lines.push(s); console.log(s); }
function check(name, ok, detail = "") { log((ok ? "PASS " : "FAIL ") + name + (detail ? "  [" + detail + "]" : "")); if (!ok) failures++; }

const tmp = mkdtempSync(join(tmpdir(), "apa-show-"));
chmodSync(tmp, 0o700);
const home = join(tmp, "home");
mkdirSync(home);
const bin = join(tmp, "bin");
mkdirSync(bin);
for (const c of ["demo", "fake-claude", "apa-issuer"]) execFileSync("go", ["build", "-o", join(bin, c), "./cmd/" + c], { cwd: repo, stdio: "inherit" });
const cred = join(tmp, "cred");
log(execFileSync(join(bin, "apa-issuer"), ["init", "--dir", cred, "--issuer", "issuer-demo", "--consumers", "consumer-a,consumer-b"]).toString().trim());
const catalog = join(repo, "testdata/demo/scenarios-ux3.json");

let demo, b, s;
const showApi = [];
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
  await b.send("Emulation.setDeviceMetricsOverride", { width: 1440, height: 900, deviceScaleFactor: 1, mobile: false });
  const click = (sel) => b.evaluate(`(() => { const e = document.querySelector(${JSON.stringify(sel)}); if (!e || e.disabled) return false; e.click(); return true; })()`);
  const hostShown = (c) => `(() => { const e = document.querySelector('#scene-status li[data-consumer="${c}"]'); return !!e && e.dataset.shown === "true"; })()`;
  // drawn: headless Chrome draws a canvas only when asked for a picture.
  async function drawn(page, expr, ms = 30000) {
    for (const end = Date.now() + ms; Date.now() < end; await sleep(1000)) {
      if (await b.waitFor(expr, 1000)) return true;
      await page.send("Page.captureScreenshot", { format: "png" });
    }
    return false;
  }
  async function shot(page, name, w, h) {
    await page.send("Emulation.setDeviceMetricsOverride", { width: w, height: h, deviceScaleFactor: 1, mobile: false });
    await sleep(1000);
    const png = await page.send("Page.captureScreenshot", { format: "png" });
    writeFileSync(join(out, name + ".png"), Buffer.from(png.data, "base64"));
    log("  screenshot " + name + ".png");
  }

  // ---- 1 the host page owns the scene ----
  await b.send("Page.navigate", { url: "http://" + addr + "/host" });
  await b.waitFor(`document.readyState === "complete"`);
  const doc = await b.send("DOM.getDocument", {});
  const q = await b.send("DOM.querySelector", { nodeId: doc.root.nodeId, selector: "#token-file" });
  await b.send("DOM.setFileInputFiles", { nodeId: q.nodeId, files: [tokenFile] });
  check("the host page loads with the token file; its scene owns its shop", await b.waitFor(`document.getElementById("creds").dataset.mode === "file"`, 20000)
    && await b.waitFor(`!document.getElementById("scene-status").hidden`, 90000));

  // ---- 2 the audience window takes the scene ----
  s = await b.openPage("http://" + addr + "/show");
  await s.send("Network.enable");
  s.on("Network.requestWillBeSent", (p) => { const u = new URL(p.request.url); if (u.pathname.startsWith("/api/")) showApi.push(u.pathname); });
  await s.send("Emulation.setDeviceMetricsOverride", { width: 1920, height: 1080, deviceScaleFactor: 1, mobile: false });
  check("the window's Unity is ready", await s.waitFor(`document.getElementById("unity-status").dataset.state === "ready"`, 90000), await s.evaluate(`document.getElementById("unity-status").textContent`));
  check("the host page hides its shop and says the scene is in the window", await b.waitFor(`!document.getElementById("show-note").hidden && document.querySelector(".canvas-frame").hidden`, 15000));
  check("the window's header: this demo does not pay", await s.waitFor(`document.getElementById("show-network").textContent === "這場展示不付款"`, 15000), await s.evaluate(`document.getElementById("show-network").textContent`));

  // ---- 3 A's data run asks: the window's five steps ----
  check("以 A 模擬, 建立委託, 確認: A's data run starts", await click(`.agent-card[data-consumer="consumer-a"] .agent-sim`)
    && await b.waitFor(`document.getElementById("sim-chip").textContent.includes("客人 A")`, 10000)
    && await click(`.agent-card[data-consumer="consumer-a"] .agent-delegate[data-scenario="data-a"]`)
    && await click(`.delegation[data-scenario="data-a"] .delegation-confirm`)
    && await b.waitFor(`document.getElementById("run-status").textContent.includes("已開始 show-a-01")`, 20000));
  const card = `document.querySelector('.show-card[data-consumer="consumer-a"]')`;
  const states = `[...${card}.querySelectorAll(".show-step")].map((e) => e.dataset.state).join(" ")`;
  check("the window: A waits for the approval (done done now todo todo)", await s.waitFor(`!!${card} && ${states} === "done done now todo todo"`, 40000),
    await s.evaluate(`${card} ? ${card}.textContent : "no card"`));
  check("the steps in plain words: the delegation, the quote, the approval", await s.evaluate(`${card}.textContent.includes("客人交代 Agent：預算 5.00 USDC，超過 0.10 USDC 要問本人")
    && ${card}.textContent.includes("報價") && ${card}.textContent.includes("超過自動範圍，等核准（示範：由主持人代為核准）")`), await s.evaluate(`${card}.textContent`));
  check("the caption under the scene", (await s.evaluate(`document.getElementById("show-captions").textContent`)) === "客人 A・核准：超過自動範圍，等核准（示範：由主持人代為核准）");
  check("Unity's report comes back to the host page (the slot shown)", await drawn(s, hostShown("consumer-a")));
  // A reloaded host page counts its scenes from 0 again: the window's Unity still takes them (review 1).
  await b.send("Page.reload");
  await b.waitFor(`document.readyState === "complete"`);
  const doc2 = await b.send("DOM.getDocument", {});
  const q2 = await b.send("DOM.querySelector", { nodeId: doc2.root.nodeId, selector: "#token-file" });
  await b.send("DOM.setFileInputFiles", { nodeId: q2.nodeId, files: [tokenFile] });
  check("after a reload of the host page, the window shows A again and Unity reports back", await b.waitFor(`document.getElementById("creds").dataset.mode === "file"`, 20000)
    && await b.waitFor(`!document.getElementById("show-note").hidden`, 20000) && await drawn(s, hostShown("consumer-a"), 40000));
  check("act as A again (a reload chooses no one)", await b.waitFor(`!!document.querySelector('.agent-card[data-consumer="consumer-a"] .agent-sim')`, 20000)
    && await click(`.agent-card[data-consumer="consumer-a"] .agent-sim`) && await b.waitFor(`document.getElementById("sim-chip").textContent.includes("客人 A")`, 10000));
  await shot(s, "show-1920x1080-1-waiting", 1920, 1080);

  // ---- 4 approved (simulated): never worded as the person's ----
  check("核准此筆（模擬） on the host page", await b.waitFor(`!!document.querySelector("#approval .approval-head[data-state=pending]")`, 30000) && await click("#approval .approval-approve"));
  check("the window: 主持人代為核准（示範，不是本人）; this demo does not pay", await s.waitFor(`${states} === "done done done off off"`, 20000)
    && await s.evaluate(`${card}.textContent.includes("主持人代為核准（示範，不是本人）") && ${card}.textContent.includes("這場展示不付款") && !${card}.textContent.includes("已登記的裝置")`),
    await s.evaluate(`${card}.textContent`));
  check("nothing for the audience is a code or base units", await s.evaluate(`!/[A-Z][A-Z_]{5,}|500000/.test(document.querySelector(".show-main").textContent)`), await s.evaluate(`document.querySelector(".show-main").textContent`));
  await shot(s, "show-1920x1080-2-approved", 1920, 1080);
  await shot(s, "show-1280x720-2-approved", 1280, 720);
  check("at 1280×720 nothing scrolls sideways", await s.evaluate(`document.documentElement.scrollWidth <= 1280`));

  // ---- 4b the checker's report, chosen on the host page after the show (GD4-A) ----
  const report = join(tmp, "report.json");
  const claims = Object.fromEntries(["E1", "E2", "E3", "E4", "E5", "E6", "E7", "E8", "E9", "E10"].map((k) => [k, "PASS"]));
  writeFileSync(report, JSON.stringify({ checker: { name: "verify-demo", version: "test" }, mode: "e1a", run_id: "e1a-test-run", verdict: "PASS", claims, not_evaluated: ["voice FAR/FRR (V1's evaluation)"] }));
  const doc3 = await b.send("DOM.getDocument", {});
  const q3 = await b.send("DOM.querySelector", { nodeId: doc3.root.nodeId, selector: "#checker-file" });
  await b.send("DOM.setFileInputFiles", { nodeId: q3.nodeId, files: [report] });
  check("the host page loads the report (memory only)", await b.waitFor(`document.getElementById("checker-note").textContent.startsWith("已載入：證據包 e1a-test-run")`, 10000),
    await b.evaluate(`document.getElementById("checker-note").textContent`));
  check("the window shows it: 獨立驗證, all ten passed, what was outside it", await s.waitFor(`(() => { const c = document.getElementById("show-checker"); return !c.hidden
    && c.textContent.includes("獨立驗證：證據包 e1a-test-run") && c.textContent.includes("逐項檢查，全部通過") && c.querySelectorAll("li[data-state=PASS]").length === 10
    && c.textContent.includes("不在這次驗證範圍：語音的誤判率"); })()`, 10000), await s.evaluate(`document.getElementById("show-checker").textContent`));
  await shot(s, "show-1920x1080-2b-checker", 1920, 1080);

  // ---- 5 the window holds nothing: no token, no API ----
  const body = await s.evaluate(`document.documentElement.outerHTML`);
  check("the window calls no API", showApi.length === 0, showApi.join(", "));
  check("no token in the window", !Object.values(t).some((x) => typeof x === "string" && x.length > 8 && body.includes(x)));

  // ---- 6 the window closes: the scene comes back ----
  await s.send("Page.close");
  check("the host page shows its shop again", await b.waitFor(`document.getElementById("show-note").hidden && !document.querySelector(".canvas-frame").hidden`, 15000));
  check("the host page's Unity shows A again", await drawn(b, hostShown("consumer-a")));

  // ---- 7 the paying layout, from a synthetic view (no chain, no payment) ----
  // The fake host does not pay. With the host page gone, a view of a paid
  // and delivered purchase on Base Sepolia is sent to a new window the way
  // the host page would, to see the evidence, the links and the receipt.
  await b.send("Page.navigate", { url: "about:blank" });
  await sleep(500);
  s = await b.openPage("http://" + addr + "/show");
  await s.send("Emulation.setDeviceMetricsOverride", { width: 1920, height: 1080, deviceScaleFactor: 1, mobile: false });
  await s.waitFor(`document.getElementById("unity-status").dataset.state === "ready"`, 90000);
  const tx1 = "0x" + "a1".repeat(32), tx2 = "0x" + "b2".repeat(32);
  const view = { payments: "base_sepolia", authenticator: "device", venue: "data", customers: [{ consumer: "consumer-a", delegation: { task_budget: "5000000", allow_max: "100000" },
    snapshot: { task: { task_id: "show-a-09", consumer_id: "consumer-a", status: "running", as_of_sequence: 12, served_at: 1, contract_version: "apa.pay.v2", payment_capability: "base_sepolia",
      purchases: [{ purchase_order_id: "po-1", amount: "500000", resource_url: "https://shop.example/resources/kr-market", credential: { result: "valid" },
        approval: { state: "approved", resolved_by: "webauthn:approver-a", grant: { authorization_method: "webauthn", voice: "verified" } },
        payment: { status: "paid", state: "delivered", reserve_tx: tx1, settle_tx: tx2, settle_block: 48979135, content_sha256: "sha256:" + "c".repeat(64), receipt_id: "rcpt-show-a-09" } }] },
    events: ["TASK_STARTED", "PURCHASE_PROPOSED", "POLICY_DECIDED", "APPROVED", "PAYMENT_REQUESTED", "BUDGET_RESERVED", "PAYMENT_SIGNED", "PAYMENT_SUBMITTED", "PAYMENT_SETTLED", "RESOURCE_DELIVERED", "RECEIPT_ISSUED"]
      .map((type, i) => ({ event_id: "e" + i, event_type: type, sequence: i + 1, purchase_order_id: type === "TASK_STARTED" ? "" : "po-1", authorization_method: type === "APPROVED" ? "webauthn" : undefined,
        decision: type === "POLICY_DECIDED" ? { policy_verdict: "Ask", reason: "HUMAN_APPROVAL_REQUIRED" } : undefined })) } }] };
  await s.evaluate(`(() => { const c = new BroadcastChannel("apa-show-v1"); c.postMessage({ from: "host", kind: "own", on: true }); c.postMessage({ from: "host", kind: "view", view: ${JSON.stringify(view)} }); return true; })()`);
  const sc7 = `document.querySelector('.show-card[data-consumer="consumer-a"]')`;
  check("synthetic paid view: five steps done", await s.waitFor(`!!${sc7} && [...${sc7}.querySelectorAll(".show-step")].map((e) => e.dataset.state).join(" ") === "done done done done done"`, 10000),
    await s.evaluate(`${sc7} ? ${sc7}.textContent : "no card"`));
  check("why trustworthy: the device and the voice, both transactions linked to basescan, the receipt", await s.evaluate(`(() => {
    const t = document.getElementById("show-trust"); const hrefs = [...t.querySelectorAll("a")].map((a) => a.href);
    return t.textContent.includes("本人以已登記的裝置核准，聲音也相符") && hrefs.length === 2 && hrefs[0].endsWith("${tx1}") && hrefs[1].endsWith("${tx2}")
      && [...t.querySelectorAll("a")].every((a) => a.target === "_blank" && a.rel === "noopener noreferrer") && t.textContent.includes("rcpt-show-a-09"); })()`),
    await s.evaluate(`document.getElementById("show-trust").textContent`));
  check("the receipt card: 韓國市場簡報, 0.50 USDC, two transactions, not one", await s.evaluate(`(() => { const r = document.querySelector("#show-receipts .show-receipt"); return !!r
    && r.textContent.includes("韓國市場簡報") && r.textContent.includes("0.50 USDC") && r.textContent.includes("不是同一筆，也不是一次完成"); })()`),
    await s.evaluate(`document.getElementById("show-receipts").textContent`));
  await shot(s, "show-1920x1080-3-paid-synthetic", 1920, 1080);
  await shot(s, "show-1280x720-3-paid-synthetic", 1280, 720);
  // The scene's size and the receipt inside it (review S3-1): the scene keeps its share; the card never leaves it.
  const geo = await s.evaluate(`(() => { const c = document.getElementById("unity-canvas").getBoundingClientRect(), r = document.querySelector(".show-receipt").getBoundingClientRect();
    return JSON.stringify({ w: c.width, inside: r.bottom <= c.bottom + 1 && r.right <= c.right + 1 && r.left >= c.left - 1 }); })()`);
  check("at 1280×720 the scene is at least 540 px wide and the receipt card stays inside it", JSON.parse(geo).w >= 540 && JSON.parse(geo).inside, geo);
  check("at 1280×720 nothing scrolls down (paid, with the evidence)", await s.evaluate(`document.documentElement.scrollHeight <= 720`), await s.evaluate(`String(document.documentElement.scrollHeight)`));
  check("at 1280×720 nothing scrolls sideways (paid)", await s.evaluate(`document.documentElement.scrollWidth <= 1280`));
  await s.send("Emulation.setDeviceMetricsOverride", { width: 1920, height: 1080, deviceScaleFactor: 1, mobile: false });
  await sleep(500);
  check("at 1920×1080 nothing scrolls down (paid, with the evidence)", await s.evaluate(`document.documentElement.scrollHeight <= 1080`), await s.evaluate(`String(document.documentElement.scrollHeight)`));
  check("the flow ran to its end", true);
} catch (e) {
  check("the flow ran to its end", false, String(e && e.stack || e).slice(0, 400));
} finally {
  for (const [name, p] of [["host page", b], ["window", s]]) {
    if (!p) continue;
    const errs = p.consoleText.filter((x) => !/favicon|Failed to load resource/.test(x));
    check("no page errors on the " + name, errs.length === 0, errs.join(" | "));
  }
  if (b) b.close();
  if (demo) await demo.stop();
  log("result: " + (failures ? "FAIL (" + failures + ")" : "PASS"));
  writeFileSync(join(out, "show-ui.txt"), lines.join("\n") + "\n");
  process.exit(failures ? 1 : 0);
}
