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
  check("the window: A waits for the person's approval (done done now todo todo)", await s.waitFor(`!!${card} && ${states} === "done done now todo todo"`, 40000),
    await s.evaluate(`${card} ? ${card}.textContent : "no card"`));
  check("the steps in plain words: the delegation, the quote, the approval", await s.evaluate(`${card}.textContent.includes("客人交代 Agent：預算 5.00 USDC，超過 0.10 USDC 要問本人")
    && ${card}.textContent.includes("報價") && ${card}.textContent.includes("超過自動範圍，等本人核准")`), await s.evaluate(`${card}.textContent`));
  check("the caption under the scene", (await s.evaluate(`document.getElementById("show-captions").textContent`)) === "客人 A・核准：超過自動範圍，等本人核准");
  check("Unity's report comes back to the host page (the slot shown)", await drawn(s, hostShown("consumer-a")));
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

  // ---- 5 the window holds nothing: no token, no API ----
  const body = await s.evaluate(`document.documentElement.outerHTML`);
  check("the window calls no API", showApi.length === 0, showApi.join(", "));
  check("no token in the window", !Object.values(t).some((x) => typeof x === "string" && x.length > 8 && body.includes(x)));

  // ---- 6 the window closes: the scene comes back ----
  await s.send("Page.close");
  check("the host page shows its shop again", await b.waitFor(`document.getElementById("show-note").hidden && !document.querySelector(".canvas-frame").hidden`, 15000));
  check("the host page's Unity shows A again", await drawn(b, hostShown("consumer-a")));
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
