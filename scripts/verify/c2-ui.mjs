// Real-browser check of C2 (docs/commercial-c2-design.md 10 step 8): the
// host page with an apa.pay.v2 catalog and a credential directory of test
// keys. Act as A, confirm the delegation, the Agent asks: the card and the
// drawer show the verification (passed, old BBS+, not W3C bbs-2023);
// revoke A's credential with apa-issuer while the Ask waits, approve: the
// session closes as 「憑證已撤銷，這筆已關閉」, no grant, never as the task's
// cancellation. Then B, still valid, is approved. The demo host with the
// fake CLI (no model, no network, no key but synthetic test keys), a test
// HOME, the Unity Web build, Chrome over the DevTools protocol. The token
// file stands in for pairing; no token, code or capability is logged, and
// every screenshot is taken with the token fields empty.
//
//   node scripts/verify/c2-ui.mjs [--unity-build DIR] [--out DIR]

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
const out = resolve(arg("--out", join(repo, "artifacts/c2", "c2-ui-" + commit.slice(0, 12) + (dirty ? "-dirty" : "") + "-" + stamp)));
mkdirSync(out, { recursive: true });

const lines = [];
let failures = 0;
const started = Date.now();
function log(s) { lines.push(s); console.log(s); }
const mark = (name) => log("  [" + Math.round((Date.now() - started) / 1000) + " s] " + name);
function check(name, ok, detail = "") { log((ok ? "PASS " : "FAIL ") + name + (detail ? "  [" + detail + "]" : "")); if (!ok) failures++; }

const tmp = mkdtempSync(join(tmpdir(), "apa-c2-"));
chmodSync(tmp, 0o700);
const home = join(tmp, "home");
mkdirSync(home);
const bin = join(tmp, "bin");
mkdirSync(bin);
for (const c of ["demo", "fake-claude", "apa-issuer"]) execFileSync("go", ["build", "-o", join(bin, c), "./cmd/" + c], { cwd: repo, stdio: "inherit" });
const cred = join(tmp, "cred");
const issuer = (...a) => execFileSync(join(bin, "apa-issuer"), [a[0], "--dir", cred, ...a.slice(1)]).toString().trim();
log(issuer("init", "--issuer", "issuer-demo", "--consumers", "consumer-a,consumer-b"));

const tool = (name, product) => ({ do: "tool", tool: name, ...(product ? { args: { product_id: product } } : {}) });
const main = join(tmp, "main.json");
writeFileSync(main, JSON.stringify({ steps: [tool("list_products"), tool("get_quote", "kr-market"), tool("request_purchase", "kr-market"), { do: "result", text: "done" }] }));
const terms = { purpose: "買一份韓國市場簡報（數位資料）", task_budget: "5000000", allow_max: "100000", approval_deadline_seconds: 120, valid_seconds: 1800 };
const catalog = join(tmp, "scenarios.json");
writeFileSync(catalog, JSON.stringify({ description: "c2-ui", scenarios: [
  { id: "v2-a", consumer_id: "consumer-a", contract: "apa.pay.v2", scenario: main, label: "憑證＋委託後選購：韓國市場簡報", delegation: terms },
  { id: "v2-b", consumer_id: "consumer-b", contract: "apa.pay.v2", scenario: main, label: "憑證＋委託後選購：韓國市場簡報", delegation: terms },
] }));

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
  const shows = async (c, task, phase) => {
    const want = `(() => { const e = document.querySelector('#scene-status li[data-consumer="${c}"]'); return !!e && e.dataset.task === "${task}" && e.dataset.phase === "${phase}" && e.dataset.shown === "true"; })()`;
    for (const end = Date.now() + 30000; Date.now() < end; await sleep(2000)) {
      if (await b.waitFor(want, 2000)) return true;
      await b.send("Page.captureScreenshot", { format: "png" });
    }
    return false;
  };

  // ---- 1 the entry: the header says the credential step ----
  mark("load");
  await size(1440, 900);
  check("the header says C2: fake, BBS+ credential, simulated approval, no payment", await loadPage() && await b.waitFor(`document.querySelector("header .badge").dataset.mode === "fake"
    && document.querySelector("header .badge").textContent === "C2／fake agent／BBS+ 憑證／模擬核准／不付款"`, 15000), await text("header .badge"));
  check("the capabilities line says old BBS+, not bbs-2023, test issuer in the host process; ZK not implemented", (await text("header .caps")).includes("憑證 BBS+ 舊版（非 W3C bbs-2023；測試 issuer，與 host 同一程序）")
    && (await text("header .caps")).includes("ZK 未實作") && !(await text("header .caps")).includes("憑證／ZK 未實作"), await text("header .caps"));
  check("the scene owns the shop", await b.waitFor(`!document.getElementById("scene-status").hidden`, 90000));

  // ---- 2 act as A, confirm, the Agent asks; the verification passed ----
  mark("act as A");
  check("以 A 模擬, 建立委託, 確認: A's run starts", await click(`.agent-card[data-consumer="consumer-a"] .agent-sim`)
    && await b.waitFor(`document.getElementById("sim-chip").textContent.includes("客人 A")`, 10000)
    && await click(`.agent-card[data-consumer="consumer-a"] .agent-delegate[data-scenario="v2-a"]`)
    && await click(`.delegation[data-scenario="v2-a"] .delegation-confirm`)
    && await b.waitFor(`document.getElementById("run-status").textContent.includes("已開始 show-a-01")`, 20000), await text("#run-status"));
  check("A's Ask waits in 需要你處理", await b.waitFor(`!!document.querySelector("#approval .approval-head[data-state=pending]")`, 30000), await text("#approval-body"));
  const ea = await hostEvents("show-a-01");
  const cv = ea.events.find((e) => e.event_type === "CREDENTIAL_VERIFIED");
  check("the Store: CREDENTIAL_VERIFIED between PURCHASE_PROPOSED and POLICY_DECIDED, valid, this host's verifier",
    ea.events.map((e) => e.event_type).join(",").includes("PURCHASE_PROPOSED,CREDENTIAL_VERIFIED,POLICY_DECIDED") && cv && cv.credential.result === "valid" && cv.credential.verifier === "apa-demo-host",
    JSON.stringify(cv && cv.credential));
  check("the panel card shows 憑證檢查: passed, old BBS+", await b.waitFor(`document.getElementById("card").textContent.includes("憑證檢查") && document.getElementById("card").textContent.includes("通過：BBS+（舊版，非 W3C bbs-2023）")`, 15000), await text("#card"));
  check("the drawer shows the verification and its details", await click("#approval .approval-evidence")
    && await b.waitFor(`(() => { const d = document.getElementById("evidence-drawer"), x = d.textContent; return !d.hidden && x.includes("憑證檢查") && x.includes("憑證細節") && x.includes("issuer issuer-demo") && x.includes("verifier apa-demo-host"); })()`, 10000),
    await text("#evidence-drawer"));
  await shot("c2-1440x900-1-drawer");
  await b.evaluate(`(document.getElementById("drawer-close").click(), true)`);
  check("the scene shows A waiting at the counter", await shows("consumer-a", "show-a-01", "waiting"));
  await shot("c2-1440x900-2-ask");

  // ---- 3 revoke A's credential while the Ask waits; approve: closed by the credential ----
  mark("revoke, approve");
  log("  " + issuer("revoke", "--task", "show-a-01"));
  check("核准此筆（模擬） after the revocation: 憑證已撤銷，這筆已關閉, never the task's cancellation", await click("#approval .approval-approve")
    && await b.waitFor(`(document.querySelector("#approval .approval-head[data-state=withdrawn] h3") || {}).textContent === "憑證已撤銷，這筆已關閉"`, 15000)
    && !(await text("#approval-body")).includes("task 取消"), await text("#approval-body"));
  const ta = await hostTask("show-a-01");
  const pa = ta.purchases[0];
  check("the Store: withdrawn with CREDENTIAL_REVOKED, no grant", pa.approval_closed === "withdrawn" && pa.closed_reason === "CREDENTIAL_REVOKED" && !pa.approval.grant, JSON.stringify(pa));
  const types = (await hostEvents("show-a-01")).events.map((e) => e.event_type);
  check("events: APPROVAL_WITHDRAWN, no APPROVED or GRANT_ISSUED, no payment", types.includes("APPROVAL_WITHDRAWN") && !types.includes("APPROVED") && !types.includes("GRANT_ISSUED")
    && !types.some((x) => /SETTLED|DELIVERED|PAID/.test(x)), types.join(","));
  check("the panel card says the credential closed it", await b.waitFor(`document.getElementById("card").textContent.includes("憑證已撤銷，這筆已關閉")`, 15000), await text("#card"));
  check("the scene: A leaves as withdrawn (Unity's M1 phase)", await shows("consumer-a", "show-a-01", "withdrawn"));
  await shot("c2-1440x900-3-revoked");
  await size(390, 844);
  await shot("c2-390x844-3-revoked");
  await size(1440, 900);

  // ---- 4 B, still valid: approved ----
  mark("act as B");
  check("以 B 模擬, 建立委託, 確認: B's Ask", await click(`.agent-card[data-consumer="consumer-b"] .agent-sim`)
    && await b.waitFor(`document.getElementById("sim-chip").textContent.includes("客人 B")`, 10000)
    && await click(`.agent-card[data-consumer="consumer-b"] .agent-delegate[data-scenario="v2-b"]`)
    && await click(`.delegation[data-scenario="v2-b"] .delegation-confirm`)
    && await b.waitFor(`!!document.querySelector("#approval .approval-head[data-state=pending]")`, 30000), await text("#approval-body"));
  check("approve B: 已核准（模擬），未啟用付款, a simulated grant", await click("#approval .approval-approve")
    && await b.waitFor(`(document.querySelector("#approval .approval-head[data-state=approved] h3") || {}).textContent === "已核准（模擬），未啟用付款"`, 15000)
    && (await hostTask("show-b-01")).purchases[0].approval.grant.authorization_method === "simulated", await text("#approval-body"));
  await shot("c2-1440x900-4-b-approved");
  check("the flow ran to its end", true);
} catch (e) {
  check("the flow ran to its end", false, String(e && e.stack || e).slice(0, 400));
} finally {
  if (b) {
    const errs = b.consoleText.filter((x) => !/favicon/.test(x));
    log("page errors: " + (errs.length ? errs.join(" | ") : "none"));
    log("404 paths: " + ([...missing].join(", ") || "none"));
    b.close();
  }
  if (demo) await demo.stop();
  log("result: " + (failures ? "FAIL (" + failures + ")" : "PASS"));
  writeFileSync(join(out, "c2-ui.txt"), lines.join("\n") + "\n");
  console.log("evidence: " + out);
  process.exit(failures ? 1 : 0);
}
