// Real-browser test of M-B's two customers at once on /host
// (docs/multi-agent-demo-design.md 2, 5, 10.1): Chrome over the DevTools
// protocol, the demo host with the fake CLI (no model, no network), the
// Unity Web build. Handshake files order the fake runs (wait_file); nothing
// waits on a fixed sleep to make two runs overlap. Masked screenshots and
// the checks go to --out; no token is logged.
//
//   node scripts/verify/dual-ui.mjs [--unity-build DIR] [--out DIR]
//
// ponytail: its small harness repeats browser-ui.mjs's (start the host,
// drive Chrome); share it if a third script needs it.

import { spawn, execFileSync } from "node:child_process";
import { mkdtempSync, mkdirSync, readFileSync, writeFileSync, chmodSync } from "node:fs";
import { tmpdir } from "node:os";
import { join, resolve } from "node:path";

const repo = resolve(new URL("../..", import.meta.url).pathname);
const arg = (k, d) => { const i = process.argv.indexOf(k); return i > 0 ? process.argv[i + 1] : d; };
const unityBuild = resolve(arg("--unity-build", join(repo, "unity/AgentPaymentAuthority/Build/WebGL/Build")));
const stamp = new Date().toISOString().replace(/[-:]/g, "").replace(/\.\d+Z$/, "Z");
const commit = execFileSync("git", ["-C", repo, "rev-parse", "HEAD"]).toString().trim();
const out = resolve(arg("--out", join(repo, "artifacts/m-b", "dual-ui-" + commit.slice(0, 12) + "-" + stamp)));
mkdirSync(out, { recursive: true });
const CHROME = "/Applications/Google Chrome.app/Contents/MacOS/Google Chrome";

const lines = [];
let failures = 0;
const started = Date.now();
function log(s) { lines.push(s); console.log(s); }
const mark = (name) => log("  [" + Math.round((Date.now() - started) / 1000) + " s] " + name);
function check(name, ok, detail = "") { log((ok ? "PASS " : "FAIL ") + name + (detail ? "  [" + detail + "]" : "")); if (!ok) failures++; }
const sleep = (ms) => new Promise((r) => setTimeout(r, ms));

const tmp = mkdtempSync(join(tmpdir(), "apa-dual-"));
chmodSync(tmp, 0o700);
const bin = join(tmp, "bin");
mkdirSync(bin);
execFileSync("go", ["build", "-o", join(bin, "demo"), "./cmd/demo"], { cwd: repo, stdio: "inherit" });
execFileSync("go", ["build", "-o", join(bin, "fake-claude"), "./cmd/fake-claude"], { cwd: repo, stdio: "inherit" });

// The fake runs: A asks and waits; B waits for the gate file before its
// quote, then asks and waits; A's second run asks and ends.
const gate = join(tmp, "gate-b");
const tool = (name, product) => ({ do: "tool", tool: name, ...(product ? { args: { product_id: product } } : {}) });
const scenario = (name, steps) => { const p = join(tmp, name + ".json"); writeFileSync(p, JSON.stringify({ steps })); return p; };
const hang = { do: "hang" };
const catalog = join(tmp, "scenarios.json");
writeFileSync(catalog, JSON.stringify({ description: "dual-ui", scenarios: [
  { id: "a-hang", consumer_id: "consumer-a", label: "A：Ask 後等待", scenario: scenario("a-hang", [tool("list_products"), tool("get_quote", "kr-market"), tool("request_purchase", "kr-market"), hang]) },
  { id: "b-hang", consumer_id: "consumer-b", label: "B：Ask 後等待", scenario: scenario("b-hang", [tool("list_products"), { do: "wait_file", text: gate }, tool("get_quote", "kr-market"), tool("request_purchase", "kr-market"), hang]) },
  { id: "z-ask-a", consumer_id: "consumer-a", label: "A：主商品 Ask", scenario: scenario("z-ask-a", [tool("list_products"), tool("get_quote", "kr-market"), tool("request_purchase", "kr-market"), { do: "result", text: "done" }]) },
] }));

function startDemo(state, extra = []) {
  mkdirSync(state, { recursive: true, mode: 0o700 });
  const env = { PATH: process.env.PATH, HOME: process.env.HOME, APA_ADDR: "127.0.0.1:0", APA_DB_PATH: join(state, "demo.db"), APA_DEMO_TOKEN_FILE: join(state, "tokens.json") };
  const args = ["--trace-dir", join(state, "runs"), "--unity-build", unityBuild, "--fake-cli", join(bin, "fake-claude"), "--scenarios", catalog,
    "--registry", join(repo, "testdata/demo/registry.json"), ...extra];
  const p = spawn(join(bin, "demo"), args, { cwd: repo, env, stdio: ["ignore", "ignore", "pipe"] });
  let text = "";
  p.stderr.on("data", (b) => { text += b; });
  return {
    log: () => text,
    async ready() {
      for (let i = 0; i < 600; i++) { const m = text.match(/listening on http:\/\/(\S+)/); if (m) return m[1]; await sleep(100); }
      throw new Error("demo did not start: " + text.slice(0, 400));
    },
    tokens() {
      const t = JSON.parse(readFileSync(join(state, "tokens.json"))).tokens;
      const get = (role, c) => t.find((x) => x.role === role && (x.consumer_id || "") === c).token;
      return { a: get("viewer", "consumer-a"), b: get("viewer", "consumer-b"), host: get("host", ""), op: get("operator", "") };
    },
    async stop() { p.kill("SIGINT"); await new Promise((r) => p.once("exit", r)); },
  };
}

async function startChrome() {
  const profile = join(tmp, "chrome-profile");
  const c = spawn(CHROME, ["--headless=new", "--user-data-dir=" + profile, "--remote-debugging-port=0", "--no-first-run", "--no-default-browser-check",
    "--disable-extensions", "--disable-sync", "--disable-background-networking", "--disable-component-update", "--use-angle=swiftshader",
    "--disable-backgrounding-occluded-windows", "--disable-renderer-backgrounding", "--enable-unsafe-swiftshader", "about:blank"], { stdio: "ignore" });
  let port;
  for (let i = 0; i < 100 && !port; i++) { await sleep(100); try { port = readFileSync(join(profile, "DevToolsActivePort"), "utf8").split("\n")[0]; } catch {} }
  const targets = await (await fetch("http://127.0.0.1:" + port + "/json/list")).json();
  const ws = new WebSocket(targets.find((t) => t.type === "page").webSocketDebuggerUrl);
  await new Promise((r) => ws.addEventListener("open", r, { once: true }));
  let id = 0;
  const waiting = new Map();
  const listeners = new Map();
  const consoleText = [];
  ws.addEventListener("message", (ev) => {
    const m = JSON.parse(ev.data);
    if (m.method && listeners.has(m.method)) for (const fn of listeners.get(m.method)) fn(m.params);
    if (m.id && waiting.has(m.id)) { waiting.get(m.id)(m); waiting.delete(m.id); }
    if (m.method === "Runtime.exceptionThrown") consoleText.push("EXCEPTION " + (m.params.exceptionDetails.exception?.description || m.params.exceptionDetails.text));
    if (m.method === "Log.entryAdded" && m.params.entry.level === "error") consoleText.push("LOG " + m.params.entry.text);
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
  const on = (method, fn) => { listeners.set(method, [...(listeners.get(method) || []), fn]); };
  return { send, evaluate, waitFor, on, consoleText, close: () => { ws.close(); c.kill(); } };
}

let demo, browser;
const tokens = [];
try {
  log("commit " + commit);
  const state = join(tmp, "state");
  demo = startDemo(state);
  const addr = await demo.ready();
  const t = demo.tokens();
  tokens.push(t.a, t.b, t.host, t.op);
  const tokenFile = join(tmp, "tokens.json");
  writeFileSync(tokenFile, readFileSync(join(state, "tokens.json")));
  const b = (browser = await startChrome());
  await b.send("Emulation.setDeviceMetricsOverride", { width: 1280, height: 900, deviceScaleFactor: 1, mobile: false });
  const hostGet = async (path) => (await fetch("http://" + addr + path, { headers: { Authorization: "Bearer " + t.op } })).json();
  const hostTask = async (id) => (await fetch("http://" + addr + "/api/tasks/" + id, { headers: { Authorization: "Bearer " + t.host } })).json();
  const runs = async () => (await hostGet("/api/host/runs")).runs;
  const li = (c) => `document.querySelector('#scene-status li[data-consumer="${c}"]')`;
  const slot = (c) => b.evaluate(`JSON.stringify((${li(c)} || {}).dataset || null)`).then(JSON.parse);
  const shows = (c, task, phase) => b.waitFor(`(() => { const e = ${li(c)}; return !!e && e.dataset.task === "${task}" && e.dataset.phase === "${phase}" && e.dataset.shown === "true"; })()`, 30000);
  async function shot(name) {
    await sleep(1000);
    check("screenshot " + name + ": token field empty", await b.evaluate(`document.getElementById("token").value === ""`));
    const png = await b.send("Page.captureScreenshot", { format: "png" });
    writeFileSync(join(out, name + ".png"), Buffer.from(png.data, "base64"));
    log("  screenshot " + name + ".png (1280x900): " + await b.evaluate(`[...document.querySelectorAll("#scene-status li")].map((l) => l.textContent).join(" / ")`));
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

  mark("load");
  check("the host page loads the token file", await loadPage(), b.consoleText.join(" | ").slice(0, 400));
  check("Unity says it shows version 2: the scene owns the shop", await b.waitFor(`!document.getElementById("scene-status").hidden && document.getElementById("unity-status").dataset.state !== "failed"`, 90000),
    await b.evaluate(`document.getElementById("unity-status").textContent`));
  check("nothing runs at start", !demo.log().includes("demo: run ") && (await runs()).length === 0);
  check("the capacity is shown: 2, a deployment setting", await b.waitFor(`(document.querySelector('#readiness tr[data-item="同時執行上限"]') || {}).textContent?.includes("2 場（部署設定 --max-runs")`, 15000));
  check("the start buttons are grouped by consumer", await b.waitFor(`document.querySelectorAll("#run-scenarios .consumer-row").length === 2 && !!document.querySelector('.consumer-row[data-consumer="consumer-a"] button[data-scenario="a-hang"]')`, 15000));

  // 一起開始: two separate requests; both runs at once.
  mark("start both");
  check("一起開始 names each consumer's scenario and says it is not one atomic start", await b.waitFor(`!document.getElementById("start-all").hidden && !document.getElementById("start-all").disabled
    && document.getElementById("start-all").textContent.includes("a-hang（A）") && document.getElementById("start-all").textContent.includes("b-hang（B）") && document.getElementById("start-all").textContent.includes("不保證都成功")`, 15000));
  await b.evaluate(`document.getElementById("start-all").click()`);
  check("both starts answered on their own", await b.waitFor(`document.getElementById("run-status").textContent.includes("A 已開始 show-a-01") && document.getElementById("run-status").textContent.includes("B 已開始 show-b-01")`, 20000),
    await b.evaluate(`document.getElementById("run-status").textContent`));
  check("A asks while B waits at its gate: both customers shown at once", await shows("consumer-a", "show-a-01", "waiting") && await shows("consumer-b", "show-b-01", "entered"),
    JSON.stringify([await slot("consumer-a"), await slot("consumer-b")]));
  const r1 = await runs();
  check("two runs running, one per consumer", r1.filter((r) => r.live === "running").length === 2);
  check("each consumer's start is disabled while its run runs", await b.waitFor(`[...document.querySelectorAll('.consumer-row button')].every((x) => x.disabled) && document.getElementById("start-all").disabled`, 10000));
  await shot("dual-a-asks-b-entered");
  writeFileSync(gate, "");
  check("B goes on to its own Ask; A stays", await shows("consumer-b", "show-b-01", "waiting") && await shows("consumer-a", "show-a-01", "waiting"));
  await shot("dual-both-ask");
  const before = { a: await slot("consumer-a"), b: await slot("consumer-b") };

  // Stop A; B goes on.
  mark("stop A");
  const runA = r1.find((r) => r.consumer_id === "consumer-a").run_id;
  await b.evaluate(`document.querySelector('#control-now .row-stop[data-run="${runA}"]').click()`);
  check("A is withdrawn and leaves; B still waits", await shows("consumer-a", "show-a-01", "withdrawn") && await shows("consumer-b", "show-b-01", "waiting"));
  const r2 = await runs();
  check("A's run ended cancelled; B's run still running", r2.find((r) => r.run_id === runA).store_end === "TASK_CANCELLED" && r2.find((r) => r.consumer_id === "consumer-b").live === "running");
  check("B's task is still awaiting approval", (await hostTask("show-b-01")).status === "awaiting_approval");
  await shot("dual-a-stopped-b-waits");

  // Choosing B in the panels changes no slot of the scene.
  mark("choose B");
  await b.evaluate(`document.querySelector('#consumer-pick button[data-consumer="consumer-b"]').click()`);
  check("the panels show B", await b.waitFor(`document.getElementById("viewer").textContent.startsWith("consumer-b")`, 15000));
  const afterChoose = { a: await slot("consumer-a"), b: await slot("consumer-b") };
  check("choosing B changed no slot and no scene", afterChoose.a.slot === before.a.slot && afterChoose.b.slot === before.b.slot && afterChoose.a.scene === before.a.scene
    && afterChoose.a.phase === "withdrawn" && afterChoose.b.phase === "waiting");

  // A's new run: A's slot only.
  mark("A again");
  await b.waitFor(`!document.querySelector('#run-scenarios button[data-scenario="z-ask-a"]').disabled`, 15000);
  await b.evaluate(`document.querySelector('#run-scenarios button[data-scenario="z-ask-a"]').click()`);
  check("A's new run shows in A's slot", await shows("consumer-a", "show-a-02", "waiting"));
  const afterA2 = { a: await slot("consumer-a"), b: await slot("consumer-b") };
  check("only A's slot generation changed", Number(afterA2.a.slot) === Number(before.a.slot) + 1 && afterA2.b.slot === before.b.slot);

  // Viewing A's older task: panels only, said so, and back.
  mark("older task");
  await b.evaluate(`document.querySelector('#consumer-pick button[data-consumer="consumer-a"]').click()`);
  await b.waitFor(`!!document.querySelector('#task-pick button[data-task="show-a-01"]')`, 15000);
  await b.evaluate(`document.querySelector('#task-pick button[data-task="show-a-01"]').click()`);
  check("viewing A's older task says the scene shows show-a-02", await b.waitFor(`!document.getElementById("view-note").hidden && document.getElementById("view-note-text").textContent.includes("場景呈現的是 show-a-02")`, 15000));
  check("the scene still shows A's newest task and B unchanged", (await slot("consumer-a")).task === "show-a-02" && (await slot("consumer-a")).slot === afterA2.a.slot && (await slot("consumer-b")).slot === before.b.slot);
  check("the evidence panel says it is viewing, not a failed sync", await b.waitFor(`document.getElementById("evidence").textContent.includes("你正在查看較早的 task")`, 15000));
  await shot("dual-viewing-older");
  await b.evaluate(`document.getElementById("view-back").click()`);
  check("回到場景這筆 brings the panels back", await b.waitFor(`document.getElementById("view-note").hidden && document.getElementById("current").textContent.includes("show-a-02")`, 15000));

  // A's reads held: B goes on.
  mark("A held");
  await b.send("Fetch.enable", { patterns: [{ urlPattern: "*/api/tasks/show-a-*", requestStage: "Request" }] });
  const held = [];
  b.on("Fetch.requestPaused", (p) => held.push(p.requestId));
  await b.waitFor(`true`);
  for (let i = 0; i < 50 && held.length === 0; i++) await sleep(100);
  const runB = r2.find((r) => r.consumer_id === "consumer-b").run_id;
  await fetch("http://" + addr + "/api/host/runs/" + runB + "/stop", { method: "POST", headers: { Authorization: "Bearer " + t.op, Origin: "http://" + addr, "Content-Type": "application/json" }, body: "{}" });
  check("with A's reads held, B's stop still shows", held.length > 0 && await shows("consumer-b", "show-b-01", "withdrawn"), "held " + held.length);
  for (const id of held) await b.send("Fetch.continueRequest", { requestId: id }).catch(() => {});
  await b.send("Fetch.disable");
  check("A's slot recovers after its reads are released", await shows("consumer-a", "show-a-02", "waiting"));

  // A reload: nothing starts; the scene is restored from the Store.
  mark("reload");
  const n = (await runs()).length;
  check("after a reload and the token file again, the scene is restored", await loadPage() && await shows("consumer-a", "show-a-02", "waiting") && await shows("consumer-b", "show-b-01", "withdrawn"));
  check("the reload started no run", (await runs()).length === n && (await runs()).every((r) => r.live !== "running"));
  await shot("dual-after-reload");

  // An old Unity build (no bridge report): one customer only, said so.
  mark("old Unity");
  await b.send("Page.addScriptToEvaluateOnNewDocument", { source: `Object.defineProperty(window, "apaUnityReport", { configurable: true, set(fn) { this.__r = (k, g, s, d) => (k === "bridge" ? undefined : fn(k, g, s, d)); }, get() { return this.__r; } });` });
  await loadPage();
  check("an old Unity build: the page says it shows the chosen customer only", await b.waitFor(`!document.getElementById("scene-cap").hidden && document.getElementById("scene-cap").textContent.includes("只支援單一客人")`, 30000)
    && await b.evaluate(`document.getElementById("scene-status").hidden`), await b.evaluate(`document.getElementById("scene-cap").textContent`));
  await shot("dual-old-unity");

  // A 404 is a read the page itself handles (a task not yet in the Store
  // just after its start); only a thrown exception fails the flow.
  const exceptions = b.consoleText.filter((x) => x.startsWith("EXCEPTION"));
  log("  browser error logs (not exceptions): " + (b.consoleText.length - exceptions.length));
  check("no page exception", exceptions.length === 0, exceptions.join(" | ").slice(0, 300));
  const pageText = await b.evaluate(`document.body.innerText`);
  check("nothing says paid or delivered", !/付款成功|已付款|取貨成功/.test(pageText));
} catch (e) {
  check("the flow ran to its end", false, String(e && e.stack || e).slice(0, 400));
} finally {
  if (browser) browser.close();
  if (demo) await demo.stop();
}
const body = lines.join("\n");
check("no token in the log", !tokens.some((x) => x && body.includes(x)));
log("result: " + (failures ? "FAIL (" + failures + ")" : "PASS"));
log("evidence: " + out);
writeFileSync(join(out, "dual-ui.out"), lines.join("\n") + "\n");
process.exit(failures ? 1 : 0);
