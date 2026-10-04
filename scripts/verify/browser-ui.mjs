// Real-browser test of the B1-06 Dashboard and Unity shop (docs/demo-ui.md
// section 6). Local only: it drives the installed Google Chrome over the
// DevTools protocol with Node's own WebSocket and fetch (no packages), a
// throwaway profile, a scratch database and the fake CLI. No HAR, no trace,
// no network log: only the assertions, masked screenshots and the demo's
// own log are kept, and every kept file is checked for token values.
//
//   node scripts/verify/browser-ui.mjs [--unity-build DIR] [--out DIR]
//
// Exit 0: every check passed. Needs a Unity Web build (default
// unity/AgentPaymentAuthority/Build/WebGL/Build) for the shop checks.

import { spawn, execFileSync } from "node:child_process";
import { mkdtempSync, mkdirSync, readFileSync, writeFileSync, existsSync, readdirSync, chmodSync } from "node:fs";
import { tmpdir } from "node:os";
import { join, resolve } from "node:path";

const repo = resolve(new URL("../..", import.meta.url).pathname);
const arg = (k, d) => { const i = process.argv.indexOf(k); return i > 0 ? process.argv[i + 1] : d; };
const unityBuild = resolve(arg("--unity-build", join(repo, "unity/AgentPaymentAuthority/Build/WebGL/Build")));
const stamp = new Date().toISOString().replace(/[-:]/g, "").replace(/\.\d+Z$/, "Z");
const commit = execFileSync("git", ["-C", repo, "rev-parse", "HEAD"]).toString().trim();
const out = resolve(arg("--out", join(repo, "artifacts/b1-06", "ui-" + commit.slice(0, 12) + "-" + stamp)));
mkdirSync(out, { recursive: true });
const CHROME = "/Applications/Google Chrome.app/Contents/MacOS/Google Chrome";
const cases = JSON.parse(readFileSync(join(repo, "testdata/contracts/cases.json")));
const expect = (id) => cases.cases.find((c) => c.case_id === id).expected;

const lines = [];
let failures = 0;
function log(s) { lines.push(s); console.log(s); }
function check(name, ok, detail = "") { log((ok ? "PASS " : "FAIL ") + name + (detail ? "  [" + detail + "]" : "")); if (!ok) failures++; }
const sleep = (ms) => new Promise((r) => setTimeout(r, ms));

const tmp = mkdtempSync(join(tmpdir(), "apa-ui-"));
chmodSync(tmp, 0o700);
const bin = join(tmp, "bin");
mkdirSync(bin);
execFileSync("go", ["build", "-o", join(bin, "demo"), "./cmd/demo"], { cwd: repo, stdio: "inherit" });
execFileSync("go", ["build", "-o", join(bin, "fake-claude"), "./cmd/fake-claude"], { cwd: repo, stdio: "inherit" });

// ---- the demo host ----
function startDemo(state, plan, build, extra = []) {
  mkdirSync(state, { recursive: true, mode: 0o700 });
  const env = { PATH: process.env.PATH, HOME: process.env.HOME, APA_ADDR: "127.0.0.1:0", APA_DB_PATH: join(state, "demo.db"), APA_DEMO_TOKEN_FILE: join(state, "tokens.json") };
  const args = ["--trace-dir", join(state, "runs"), "--unity-build", build, ...extra];
  if (plan) args.push("--fake-cli", join(bin, "fake-claude"), "--plan", plan);
  const p = spawn(join(bin, "demo"), args, { cwd: repo, env, stdio: ["ignore", "ignore", "pipe"] });
  let log = "";
  p.stderr.on("data", (b) => { log += b; });
  return {
    p, state,
    log: () => log,
    async ready(runsTo) {
      for (let i = 0; i < 600; i++) {
        const m = log.match(/listening on http:\/\/(\S+)/);
        if (m && (!runsTo || log.includes("fake run " + runsTo))) return m[1];
        await sleep(100);
      }
      throw new Error("demo did not start: " + log.slice(0, 400));
    },
    tokens() {
      const t = JSON.parse(readFileSync(join(state, "tokens.json"))).tokens;
      const get = (role, c) => t.find((x) => x.role === role && (x.consumer_id || "") === c).token;
      return { a: get("viewer", "consumer-a"), b: get("viewer", "consumer-b"), host: get("host", "") };
    },
    async stop() { p.kill("SIGINT"); await new Promise((r) => p.once("exit", r)); },
  };
}

// ---- Chrome over the DevTools protocol ----
async function startChrome() {
  const profile = join(tmp, "chrome-profile");
  const c = spawn(CHROME, ["--headless=new", "--user-data-dir=" + profile, "--remote-debugging-port=0", "--no-first-run", "--no-default-browser-check",
    "--disable-extensions", "--disable-sync", "--disable-background-networking", "--disable-component-update", "--use-angle=swiftshader",
    "--enable-unsafe-swiftshader", "about:blank"], { stdio: "ignore" });
  let port;
  for (let i = 0; i < 100 && !port; i++) {
    await sleep(100);
    try { port = readFileSync(join(profile, "DevToolsActivePort"), "utf8").split("\n")[0]; } catch {}
  }
  const targets = await (await fetch("http://127.0.0.1:" + port + "/json/list")).json();
  const ws = new WebSocket(targets.find((t) => t.type === "page").webSocketDebuggerUrl);
  await new Promise((r) => ws.addEventListener("open", r, { once: true }));
  let id = 0;
  const waiting = new Map();
  const consoleText = [];
  ws.addEventListener("message", (ev) => {
    const m = JSON.parse(ev.data);
    if (m.id && waiting.has(m.id)) { waiting.get(m.id)(m); waiting.delete(m.id); }
    if (m.method === "Runtime.consoleAPICalled") consoleText.push(m.params.args.map((a) => a.value ?? a.description ?? "").join(" "));
    if (m.method === "Runtime.exceptionThrown") consoleText.push("EXCEPTION " + (m.params.exceptionDetails.exception?.description || m.params.exceptionDetails.text));
    if (m.method === "Log.entryAdded") consoleText.push("LOG " + m.params.entry.text);
  });
  const send = (method, params = {}) => new Promise((res, rej) => {
    const i = ++id;
    waiting.set(i, (m) => (m.error ? rej(new Error(method + ": " + m.error.message)) : res(m.result)));
    ws.send(JSON.stringify({ id: i, method, params }));
  });
  await send("Runtime.enable");
  await send("Log.enable");
  await send("Page.enable");
  const evaluate = async (expr) => {
    const r = await send("Runtime.evaluate", { expression: expr, awaitPromise: true, returnByValue: true });
    if (r.exceptionDetails) throw new Error("evaluate: " + (r.exceptionDetails.exception?.description || r.exceptionDetails.text));
    return r.result.value;
  };
  const waitFor = async (expr, ms = 20000) => {
    for (const end = Date.now() + ms; Date.now() < end; await sleep(100)) if (await evaluate(expr).catch(() => false)) return true;
    return false;
  };
  return { c, send, evaluate, waitFor, consoleText, close: () => { ws.close(); c.kill(); } };
}

const text = (sel) => `(document.querySelector(${JSON.stringify(sel)})?.textContent || "")`;

async function signIn(b, token) {
  await b.evaluate(`document.getElementById("token").focus()`);
  await b.send("Input.insertText", { text: token }); // typed into the masked field; never in an expression or a log
  await b.evaluate(`document.querySelector("#auth-form button[type=submit]").click()`);
}

async function shot(b, name, width, height, mobile) {
  await b.send("Emulation.setDeviceMetricsOverride", { width, height, deviceScaleFactor: 1, mobile });
  await sleep(1500);
  const empty = await b.evaluate(`document.getElementById("token").value === ""`);
  check("screenshot " + name + ": token field empty", empty);
  const png = await b.send("Page.captureScreenshot", { format: "png", captureBeyondViewport: false });
  writeFileSync(join(out, name + ".png"), Buffer.from(png.data, "base64"));
  log("  screenshot " + name + ".png viewport " + width + "x" + height);
}

async function hostEvents(addr, host, task) {
  const r = await fetch("http://" + addr + "/api/tasks/" + task + "/events", { headers: { Authorization: "Bearer " + host } });
  return (await r.json()).as_of_sequence;
}

let demo, browser;
const allTokens = [];
try {
  log("commit " + commit);
  log("unity build " + unityBuild + (existsSync(join(unityBuild, "WebGL.loader.js")) ? "" : " (MISSING)"));
  const state = join(tmp, "state");
  demo = startDemo(state, join(repo, "testdata/demo/fake-plan.json"), unityBuild);
  const addr = await demo.ready("task-b");
  let tok = demo.tokens();
  allTokens.push(tok.a, tok.b, tok.host);
  log("demo at " + addr + " (fake plan: task-a Ask, task-b Deny)");
  browser = await startChrome();
  const b = browser;
  await b.send("Emulation.setDeviceMetricsOverride", { width: 1280, height: 900, deviceScaleFactor: 1, mobile: false });
  await b.send("Page.navigate", { url: "http://" + addr + "/" });
  await b.waitFor(`document.readyState === "complete"`);

  check("header says M1 / fake agent / no payment", (await b.evaluate(text(".badge"))) === "M1／fake agent／不付款");
  const unityOk = await b.waitFor(`["ready","rendered","failed"].includes(document.getElementById("unity-status").dataset.state)`, 90000);
  const unityState = await b.evaluate(`document.getElementById("unity-status").dataset.state`);
  check("Unity Web build loaded in the browser", unityOk && unityState !== "failed", await b.evaluate(text("#unity-status")));
  check("Unity uses LimeZu art (not placeholders)", (await b.evaluate(text("#unity-status"))).includes("LimeZu 素材"));

  // Viewer A, task-a: Ask, the same snapshot in the Dashboard and in Unity.
  await signIn(b, tok.a);
  check("viewer A signed in", await b.waitFor(`${text("#viewer")}.startsWith("consumer-a")`));
  check("token field cleared after reading", (await b.evaluate(`document.getElementById("token").value`)) === "");
  await b.evaluate(`[...document.querySelectorAll("#task-pick button")].find(x => x.textContent.startsWith("task-a（"))?.click()`);
  check("task-a synced", await b.waitFor(`document.getElementById("status").dataset.kind === "ok"`));
  const ask = expect("ask");
  const insA = await b.evaluate(text("#inspector"));
  check("Inspector: Ask " + ask.reason + " pending, unpaid", insA.includes("Ask") && insA.includes(ask.reason) && insA.includes("pending") && insA.includes("未付款") && insA.includes("wallet-a"));
  check("Dashboard scene phase waiting", (await b.evaluate(`document.getElementById("scene-text").dataset.phase`)) === "waiting");
  check("Timeline has events #1-#3", (await b.evaluate(`[...document.querySelectorAll("#timeline li")].map(x => x.dataset.seq).join(",")`)) === "1,2,3");
  check("Unity rendered the same sequence and phase (#3 waiting)", await b.waitFor(`${text("#unity-status")}.includes("#3 waiting")`, 15000), await b.evaluate(text("#unity-status")));
  await b.evaluate(`document.querySelector('#timeline li[data-seq="3"] .event-head').click()`);
  const json = await b.evaluate(`document.querySelector('#timeline li[data-seq="3"] .event-json')?.textContent || ""`);
  check("event JSON is the API whitelist, labelled as not raw output", json.includes("intent_hash") && json.includes("不是 CLI 原始輸出"));
  check("nothing says paid or delivered", !/付款成功|已付款|取貨成功|Settled|RESOURCE_DELIVERED/.test(await b.evaluate(`document.body.innerText`)));
  await shot(b, "desktop-task-a-ask", 1280, 900, false);
  log("  task-a seq 3 Ask (consumer-a) shown in the desktop screenshot");

  // A cannot read B's task.
  await b.evaluate(`document.getElementById("task-id").value = "task-b"; document.querySelector("#task-form button[type=submit]").click()`);
  check("viewer A on task-b: same not-found answer", await b.waitFor(`${text("#status")}.includes("找不到這個 task，或你沒有查看權限")`));
  check("viewer A on task-b: nothing of B shown", !(await b.evaluate(`document.body.innerText`)).includes("wallet-b"));

  // Switch to B: everything of A goes at once.
  await b.evaluate(`document.getElementById("switch").click()`);
  check("switch clears viewer, Inspector and Timeline", await b.waitFor(`${text("#viewer")} === "未驗證" && ${text("#inspector")} === "尚無資料" && ${text("#timeline")} === "尚無事件"`));
  await signIn(b, tok.b);
  await b.waitFor(`${text("#viewer")}.startsWith("consumer-b")`);
  await b.evaluate(`[...document.querySelectorAll("#task-pick button")].find(x => x.textContent.startsWith("task-b（"))?.click()`);
  await b.waitFor(`document.getElementById("status").dataset.kind === "ok"`);
  const deny = expect("deny-budget");
  const insB = await b.evaluate(text("#inspector"));
  check("Inspector: Deny " + deny.reason + ", wallet-b", insB.includes("Deny") && insB.includes(deny.reason) && insB.includes("wallet-b") && !insB.includes("wallet-a"));
  check("Unity rendered #3 denied for B", await b.waitFor(`${text("#unity-status")}.includes("#3 denied")`, 15000), await b.evaluate(text("#unity-status")));
  await shot(b, "narrow-task-b-deny", 390, 844, true);
  log("  task-b seq 3 Deny (consumer-b) shown in the narrow screenshot");
  await b.send("Emulation.setDeviceMetricsOverride", { width: 1280, height: 900, deviceScaleFactor: 1, mobile: false });

  // The host token is not a viewer token.
  await b.evaluate(`document.getElementById("switch").click()`);
  await signIn(b, tok.host);
  check("host token refused by the Dashboard", await b.waitFor(`${text("#status")}.includes("不是 viewer token")`) && (await b.evaluate(text("#viewer"))) === "未驗證");

  // Hostile strings are text.
  const evil = `<img src=x onerror="window.__pwned=1"><script>window.__pwned=2</script>`;
  await b.evaluate(`import("/ui/render.js").then(m => { const d = document.createElement("div"); d.id = "evil-box"; document.body.append(d);
    m.renderInspector(d, [["x", ${JSON.stringify(evil)}]]);
    const t = document.createElement("div"); d.append(t);
    m.renderTimeline(t, [{ seq: 1, type: ${JSON.stringify(evil)}, detail: "", time: "", source: "", json: ${JSON.stringify(evil)} }]); return true; })`);
  await sleep(500);
  check("hostile strings render as text only", (await b.evaluate(`window.__pwned === undefined && document.querySelectorAll("#evil-box img, #evil-box script").length === 0 && document.getElementById("evil-box").textContent.includes("<img")`)));
  await b.evaluate(`document.getElementById("evil-box").remove()`);

  // Replay is read-only.
  const before = await hostEvents(addr, tok.host, "task-a");
  await signIn(b, tok.a);
  await b.waitFor(`${text("#viewer")}.startsWith("consumer-a")`);
  await b.evaluate(`[...document.querySelectorAll("#task-pick button")].find(x => x.textContent.startsWith("task-a（"))?.click()`);
  await b.waitFor(`document.getElementById("status").dataset.kind === "ok"`);
  await b.evaluate(`document.getElementById("replay").click()`);
  check("replay labelled read-only", await b.waitFor(`${text("#status")}.includes("歷史展示回放")`));
  check("replay ends at the stored state in Unity", await b.waitFor(`${text("#unity-status")}.includes("#3 waiting")`, 20000));
  check("replay and polling wrote no event", (await hostEvents(addr, tok.host, "task-a")) === before, "as_of_sequence " + before);

  // Nothing kept outside page memory.
  const kept = await b.evaluate(`JSON.stringify({ ls: localStorage.length, ss: sessionStorage.length, cookie: document.cookie, url: location.href })`);
  check("no storage, cookie or URL holds anything", !/"ls":[1-9]|"ss":[1-9]|"cookie":"[^"]/.test(kept) && !kept.includes("token"), kept);
  const html = await b.evaluate(`document.documentElement.outerHTML`);
  check("no token in the page or the console", allTokens.every((t) => !html.includes(t) && !b.consoleText.join("\n").includes(t)));

  // Refresh: the token is gone; signing in again restores from the Store.
  await b.send("Page.reload");
  await sleep(1000);
  await b.waitFor(`document.readyState === "complete"`);
  check("after a refresh the viewer must sign in again", await b.waitFor(`${text("#viewer")} === "未驗證"`));
  await signIn(b, tok.a);
  await b.waitFor(`${text("#viewer")}.startsWith("consumer-a")`);
  await b.evaluate(`[...document.querySelectorAll("#task-pick button")].find(x => x.textContent.startsWith("task-a（"))?.click()`);
  check("refresh + sign-in restores task-a from the Store", await b.waitFor(`document.getElementById("status").dataset.kind === "ok" && ${text("#inspector")}.includes("Ask")`));

  // Restart the host on the same database, with a plan naming task-a again
  // and task-a-next: task-a is not rerun, task-a-next keeps wallet-a.
  await demo.stop();
  const restartPlan = join(tmp, "plan-restart.json");
  writeFileSync(restartPlan, JSON.stringify({ description: "restart", runs: [{ task_id: "task-a", scenario: "testdata/cli/ask-mainline.json" }, { task_id: "task-a-next", scenario: "testdata/cli/ask-mainline.json" }] }));
  demo = startDemo(state, restartPlan, unityBuild);
  const addr2 = await demo.ready("task-a-next");
  tok = demo.tokens();
  allTokens.push(tok.a, tok.b, tok.host);
  check("restart: recorded task-a not rerun", demo.log().includes('fake run task-a: skipped="task_already_recorded"'));
  await b.send("Page.navigate", { url: "http://" + addr2 + "/" });
  await b.waitFor(`document.readyState === "complete"`);
  await signIn(b, tok.a);
  await b.waitFor(`${text("#viewer")}.startsWith("consumer-a")`);
  await b.evaluate(`[...document.querySelectorAll("#task-pick button")].find(x => x.textContent.startsWith("task-a-next"))?.click()`);
  await b.waitFor(`document.getElementById("status").dataset.kind === "ok"`);
  const next = await b.evaluate(text("#inspector"));
  check("restart: task-a-next Ask on the same wallet-a", next.includes("task-a-next") && next.includes("wallet-a") && next.includes("Ask"));
  await demo.stop();
  demo = null;

  // No Unity build: the page says so and shows no other shop.
  const bare = startDemo(join(tmp, "state-bare"), null, join(tmp, "no-build"));
  const addr3 = await bare.ready();
  await b.send("Page.navigate", { url: "http://" + addr3 + "/" });
  check("without a build: Unity 場景未就緒", await b.waitFor(`document.getElementById("unity-status").dataset.state === "failed" && ${text("#unity-status")} === "Unity 場景未就緒"`, 20000));
  await shot(b, "desktop-unity-missing", 1280, 900, false);
  await bare.stop();
  allTokens.push(...Object.values(bare.tokens()));
} catch (e) {
  check("browser test ran to the end", false, String(e && e.message || e).slice(0, 300));
} finally {
  if (browser) browser.close();
  if (demo) await demo.stop().catch(() => {});
}

// Evidence: the assertions and screenshots. No token may be in any of them.
log("result: " + (failures === 0 ? "PASS" : failures + " FAILED"));
writeFileSync(join(out, "browser-test.log"), lines.join("\n") + "\n");
const leaks = readdirSync(out).filter((f) => { const s = readFileSync(join(out, f)).toString("latin1"); return allTokens.some((t) => t && s.includes(t)); });
if (leaks.length) { console.error("TOKEN IN EVIDENCE: " + leaks.join(", ")); process.exit(2); }
console.log("evidence: " + out);
process.exit(failures === 0 ? 0 : 1);
