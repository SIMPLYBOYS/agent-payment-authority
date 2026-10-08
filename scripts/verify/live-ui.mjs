// Real-browser check of the demo host's live mode (docs/cli-poc.md 55) with
// the FAKE CLI standing in for Claude Code: a wrapper answers --version with
// the approved version and otherwise runs fake-claude, so the live path
// (scripts/demo/live.sh, the version and config-directory checks, the live
// Gate with the dev demo exception, the buyer host, the Store) runs without
// a model. Local only, like browser-ui.mjs: the installed Google Chrome over
// the DevTools protocol (no packages), a throwaway profile and HOME, no HAR
// or network log; only the assertions and full-page screenshots are kept,
// and every kept file is checked for token values. It runs no Claude CLI.
//
//   node scripts/verify/live-ui.mjs [--unity-build DIR] [--out DIR]
//
// Exit 0: every check passed.

import { spawn, execFileSync } from "node:child_process";
import { mkdtempSync, mkdirSync, readFileSync, writeFileSync, readdirSync, chmodSync } from "node:fs";
import { tmpdir } from "node:os";
import { join, resolve } from "node:path";

const repo = resolve(new URL("../..", import.meta.url).pathname);
const arg = (k, d) => { const i = process.argv.indexOf(k); return i > 0 ? process.argv[i + 1] : d; };
const unityProject = resolve(arg("--unity-project", join(repo, "unity/AgentPaymentAuthority")));
const stamp = new Date().toISOString().replace(/[-:]/g, "").replace(/\.\d+Z$/, "Z");
const commit = execFileSync("git", ["-C", repo, "rev-parse", "HEAD"]).toString().trim();
const dirty = execFileSync("git", ["-C", repo, "status", "--porcelain", "--untracked-files=no"]).toString().trim() !== "";
const out = resolve(arg("--out", join(repo, "artifacts/b1-04", "live-ui-" + commit.slice(0, 12) + "-" + stamp)));
mkdirSync(out, { recursive: true });
const CHROME = "/Applications/Google Chrome.app/Contents/MacOS/Google Chrome";
const LABEL = "真實 Agent 即時展示・開發展示例外・合成資料・不付款";
const PLUGINS = [{ name: "cc-plugin-agents-md", path: "p" }, { name: "cc-plugin-plugin-authoring", path: "p" }];

const lines = [];
let failures = 0;
const log = (s) => { lines.push(s); console.log(s); };
const check = (name, ok, detail = "") => { log((ok ? "PASS " : "FAIL ") + name + (detail ? "  [" + detail + "]" : "")); if (!ok) failures++; };
const sleep = (ms) => new Promise((r) => setTimeout(r, ms));

const tmp = mkdtempSync(join(tmpdir(), "apa-live-ui-"));
chmodSync(tmp, 0o700);
const allTokens = [];
log("commit " + commit + (dirty ? " (working tree has uncommitted changes)" : "") + "; fake CLI as the live CLI (no model)");

// ---- the stand-in CLI and its HOME ----
const bin = join(tmp, "bin");
mkdirSync(bin);
execFileSync("go", ["build", "-o", join(bin, "fake-claude"), "./cmd/fake-claude"], { cwd: repo, stdio: "inherit" });
const cli = join(bin, "claude");
writeFileSync(cli, "#!/bin/sh\nif [ \"$*\" = --version ]; then echo '2.1.293 (Claude Code)'; exit 0; fi\nexec '" + join(bin, "fake-claude") + "' \"$@\"\n", { mode: 0o755 });
const home = join(tmp, "home");
mkdirSync(join(home, ".apa-claude"), { recursive: true, mode: 0o700 });
const tool = (name, product) => (product ? { do: "tool", tool: name, args: { product_id: product } } : { do: "tool", tool: name });
function script(...steps) {
  const init = { do: "init", init: { apiKeySource: "none", claude_code_version: "2.1.293", plugins: PLUGINS } };
  writeFileSync(join(bin, "fake-claude.scenario.json"), JSON.stringify({ manual_init: true, steps: [init, ...steps] }));
}
const purchaseSteps = [tool("list_products"), tool("get_quote", "kr-market"), tool("request_purchase", "kr-market")];
const result = { do: "raw", text: JSON.stringify({ type: "result", subtype: "success", is_error: false, result: "Payment complete (model text, untrusted)", num_turns: 4, session_id: "s", duration_ms: 1, usage: { input_tokens: 8, output_tokens: 445 } }) + "\n" };

// ---- the operator's start script ----
function startScript() {
  const env = { PATH: process.env.PATH, HOME: home, APA_ADDR: "127.0.0.1:0", APA_LIVE_CLI: "true", APA_LIVE_DIR: join(tmp, "sessions"),
    APA_UNITY_PROJECT: unityProject, ...(process.env.GOCACHE ? { GOCACHE: process.env.GOCACHE } : {}) };
  const p = spawn("sh", [join(repo, "scripts/demo/live.sh"), cli, "--dev-demo-plugins"], { cwd: tmp, env, stdio: ["ignore", "pipe", "pipe"] });
  let text = "";
  p.stdout.on("data", (b) => { text += b; });
  p.stderr.on("data", (b) => { text += b; });
  const exited = new Promise((r) => p.once("exit", (code) => r(code)));
  return {
    text: () => text, exited,
    async ready() {
      for (let i = 0; i < 1200; i++) {
        const m = text.match(/listening on http:\/\/(\S+)/);
        const w = text.match(/live: session (\S+):/);
        if (m && w) return { addr: m[1], session: w[1] };
        if (p.exitCode !== null) throw new Error("the start script ended: " + text.slice(0, 600));
        await sleep(100);
      }
      throw new Error("the live host did not start: " + text.slice(0, 600));
    },
    async stop() { if (p.exitCode === null) p.kill("SIGINT"); return exited; },
  };
}
async function api(addr, token, path) {
  const r = await fetch("http://" + addr + path, { headers: { Authorization: "Bearer " + token } });
  return { status: r.status, json: await r.json().catch(() => null) };
}
const tracesIn = (session) => readdirSync(join(session, "runs")).filter((n) => n.endsWith(".jsonl"));

// ---- Chrome over the DevTools protocol (as browser-ui.mjs) ----
async function startChrome() {
  const profile = join(tmp, "chrome-profile");
  const c = spawn(CHROME, ["--headless=new", "--user-data-dir=" + profile, "--remote-debugging-port=0", "--no-first-run", "--no-default-browser-check",
    "--disable-extensions", "--disable-sync", "--disable-background-networking", "--disable-component-update", "--use-angle=swiftshader",
    "--disable-backgrounding-occluded-windows", "--disable-renderer-backgrounding", "--enable-unsafe-swiftshader", "about:blank"], { stdio: "ignore" });
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
  });
  const send = (method, params = {}) => new Promise((res, rej) => {
    const i = ++id;
    waiting.set(i, (m) => (m.error ? rej(new Error(method + ": " + m.error.message)) : res(m.result)));
    ws.send(JSON.stringify({ id: i, method, params }));
  });
  await send("Runtime.enable");
  await send("Page.enable");
  const evaluate = async (expr) => {
    const r = await send("Runtime.evaluate", { expression: expr, awaitPromise: true, returnByValue: true });
    if (r.exceptionDetails) throw new Error("evaluate: " + (r.exceptionDetails.exception?.description || r.exceptionDetails.text));
    return r.result.value;
  };
  const waitFor = async (expr, ms = 20000) => {
    for (const end = Date.now() + ms; Date.now() < end; await sleep(50)) if (await evaluate(expr).catch(() => false)) return true;
    return false;
  };
  return { send, evaluate, waitFor, consoleText, close: () => { ws.close(); c.kill(); } };
}

const text = (sel) => `(document.querySelector(${JSON.stringify(sel)})?.textContent || "")`;
const visible = (sel) => `(() => { const e = document.querySelector(${JSON.stringify(sel)}); return !!e && !e.hidden && e.offsetParent !== null; })()`;
const uKey = (k) => `(document.getElementById("unity-status").dataset.${k} || "")`;
const row = (k) => `(document.querySelector('#readiness tr[data-item="${k}"]')?.textContent || "")`;
const reading = `JSON.stringify({ badge: ${text("header .badge")}, chip: ${text("#mode-chip")}, current: ${text("#current")}, status: ${text("#status")},
  liveNote: ${visible("#live-note")}, warning: ${visible("#live-warning")}, runStatus: ${text("#run-status")}, runs: ${text("#runs")},
  unity: { state: ${uKey("state")}, task: ${uKey("task")}, seq: ${uKey("seq")}, phase: ${uKey("phase")} }, scene: ${text("#scene-text")},
  card: ${text("#card")}, evidence: ${text("#evidence")}, inspector: ${text("#inspector")},
  timeline: [...document.querySelectorAll("#timeline li")].map((li) => li.dataset.seq).join(","), token: document.getElementById("token")?.value || "" })`;

async function shot(b, name) {
  const h = Math.min(4000, await b.evaluate(`Math.ceil(document.documentElement.scrollHeight)`));
  await b.send("Emulation.setDeviceMetricsOverride", { width: 1280, height: h, deviceScaleFactor: 1, mobile: false });
  await sleep(600);
  const r = JSON.parse(await b.evaluate(reading));
  check("screenshot " + name + ": token field empty", r.token === "");
  const png = await b.send("Page.captureScreenshot", { format: "png", captureBeyondViewport: false });
  writeFileSync(join(out, name + ".png"), Buffer.from(png.data, "base64"));
  log("  " + name + ".png (full page): " + r.current + "; Unity " + r.unity.task + " #" + r.unity.seq + " " + r.unity.phase + "; Timeline " + r.timeline + "; run「" + r.runStatus + "」");
  return r;
}

let browser = null;
let host = null;
try {
  script(...purchaseSteps, { do: "sleep", ms: 8000 }, result); // the CLI keeps running after the Ask: the "running" state is seen
  host = startScript();
  const { addr, session } = await host.ready();
  const tokFile = join(session, "tok", "tokens.json");
  const t = JSON.parse(readFileSync(tokFile)).tokens;
  allTokens.push(...t.map((x) => x.token));
  const hostTok = t.find((x) => x.role === "host").token;
  log("live host at " + addr + " (session " + session.split("/").pop() + ")");
  check("start script: a new session database, nothing ran", tracesIn(session).length === 0 && (await api(addr, hostTok, "/api/tasks")).json.tasks.length === 0);

  browser = await startChrome();
  const b = browser;
  await b.send("Page.navigate", { url: "http://" + addr + "/host" });
  await b.waitFor(`document.readyState === "complete"`);
  check("Unity Web build loads", await b.waitFor(`["ready","rendered"].includes(document.getElementById("unity-status").dataset.state)`, 90000), await b.evaluate(text("#unity-status")));
  const doc = await b.send("DOM.getDocument", {});
  const q = await b.send("DOM.querySelector", { nodeId: doc.root.nodeId, selector: "#token-file" });
  await b.send("DOM.setFileInputFiles", { nodeId: q.nodeId, files: [tokFile] });
  check("before any start: the label, the warning, the real CLI row", await b.waitFor(`${text("header .badge")} === ${JSON.stringify(LABEL)} && ${visible("#live-warning")} && ${visible("#live-note")}
    && ${row("真實 CLI")}.includes("按「開始」才執行真實 Claude Code CLI") && ${row("真實 CLI")}.includes("開發展示例外")`, 20000), await b.evaluate(row("真實 CLI")));
  check("the start button says real CLI", await b.waitFor(`[...document.querySelectorAll("#run-scenarios button")].some((x) => x.textContent.startsWith("開始（真實 CLI）："))`, 20000));
  await sleep(1500);
  check("loading the page and the token file started nothing", tracesIn(session).length === 0 && (await api(addr, hostTok, "/api/host/runs")).json.runs.length === 0);
  await shot(b, "before-start");

  // ---- one run: start, the Dashboard follows the Store ----
  await b.evaluate(`[...document.querySelectorAll("#run-scenarios button")].find((x) => x.textContent.startsWith("開始（真實 CLI）："))?.click()`);
  check("the Dashboard switches to the run's task", await b.waitFor(`${text("#current")}.includes("show-a-") && document.getElementById("status").dataset.kind === "ok"`, 30000), await b.evaluate(text("#current")));
  check("Unity reports the task at #3 waiting", await b.waitFor(`${uKey("state")} === "rendered" && ${uKey("task")}.startsWith("show-a-") && ${uKey("seq")} === "3" && ${uKey("phase")} === "waiting"`, 30000));
  await b.evaluate(`(document.getElementById("evidence-box").open = true, true)`);
  check("while the CLI runs: the evidence says the trace comes at the end", await b.waitFor(`${text("#evidence")}.includes("CLI 執行中：trace 在 CLI 結束後才寫出")`, 10000));
  const runsNow = (await api(addr, hostTok, "/api/host/runs")).json.runs;
  const r1 = runsNow[0];
  check("…and the run is still running (Ask committed before the CLI ended)", r1 && r1.live === "running", JSON.stringify(r1 && { live: r1.live, state: r1.state }));
  let r = await shot(b, "running");
  check("running: card Ask, Timeline #1-#3, label kept", r.card.includes("Ask") && r.timeline === "1,2,3" && r.badge === LABEL);
  check("the trace is available once the CLI ended", await b.waitFor(`${text("#evidence")}.includes("#3 request_purchase") && ${text("#evidence")}.includes("input 8 · output 445（不是計費證明）")`, 30000));
  const task = (await api(addr, hostTok, "/api/tasks/" + r1.task_id)).json;
  const po = task.purchases[0].purchase_order_id;
  r = await shot(b, "ended");
  check("ended: the evidence maps request_purchase to the Store purchase and events, marked derived", r.evidence.includes(po + "（Store 事件 #2、#3）") && r.evidence.includes("Host 推導")
    && r.evidence.includes("gate_mode dev_demo_plugins · init gate passed") && r.inspector.includes(po) && r.card.includes(po));
  check("ended: the Store says awaiting approval, nothing paid", task.status === "awaiting_approval" && task.purchases[0].decision.policy_verdict === "Ask" && task.purchases[0].decision.payment_status === "unavailable");
  check("no model text, nothing reads as paid", !/Payment complete|付款成功|已付款|已核准/.test(r.card + r.evidence + r.scene + r.inspector));
  const end1 = (await api(addr, hostTok, "/api/host/runs/" + r1.run_id)).json;
  check("the run ended ok, one CLI run", end1.state === "ended" && end1.outcome === "ok" && tracesIn(session).length === 1, end1.outcome);

  // ---- a second run, stopped by the operator ----
  script(...purchaseSteps, { do: "hang" });
  await b.waitFor(`[...document.querySelectorAll("#run-scenarios button")].some((x) => !x.disabled)`, 20000);
  await b.evaluate(`[...document.querySelectorAll("#run-scenarios button")].find((x) => !x.disabled)?.click()`);
  check("second run: the Dashboard switches to its new task", await b.waitFor(`${text("#current")}.includes("show-a-") && !${text("#current")}.includes(${JSON.stringify(r1.task_id)}) && ${uKey("seq")} === "3"`, 30000), await b.evaluate(text("#current")));
  check("the stop button is offered", await b.waitFor(`!!document.querySelector("#control-now .row-stop")`, 20000));
  await b.evaluate(`document.querySelector("#control-now .row-stop").click()`);
  check("the stop is accepted, its result not yet claimed", await b.waitFor(`${text("#run-status")}.includes("停止請求已受理")`, 20000), await b.evaluate(text("#run-status")));
  // A stop of a waiting Ask: the buyer host records APPROVAL_WITHDRAWN, then TASK_CANCELLED.
  const run2 = (await api(addr, hostTok, "/api/host/runs")).json.runs[0];
  let evs2 = [];
  for (let i = 0; i < 300 && !evs2.includes("TASK_CANCELLED"); i++, await sleep(100)) evs2 = (await api(addr, hostTok, "/api/tasks/" + run2.task_id + "/events")).json.events.map((e) => e.event_type);
  check("the Store has the stop's events", evs2.join(",") === "TASK_STARTED,PURCHASE_PROPOSED,POLICY_DECIDED,APPROVAL_WITHDRAWN,TASK_CANCELLED", evs2.join(","));
  check("…and the Timeline shows them all (#1-#5)", await b.waitFor(`[...document.querySelectorAll("#timeline li")].map((li) => li.dataset.seq).join(",") === "1,2,3,4,5"`, 30000));
  check("Unity reports #5 withdrawn (the shop follows the committed events)", await b.waitFor(`${uKey("state")} === "rendered" && ${uKey("seq")} === "5" && ${uKey("phase")} === "withdrawn"`, 30000),
    await b.evaluate(`${uKey("seq")} + " " + ${uKey("phase")}`));
  r = await shot(b, "stopped");
  check("stopped: the card says cancelled", r.card.includes("取消") || r.card.includes("撤回"), r.card.slice(0, 160));
  const runs = (await api(addr, hostTok, "/api/host/runs")).json.runs;
  check("two runs, two CLI runs, both ended", runs.length === 2 && runs.every((x) => x.state === "ended") && tracesIn(session).length === 2, runs.map((x) => x.state + "/" + x.outcome).join(","));
  check("no token in the URL, storage, cookie or console", await b.evaluate(`JSON.stringify([location.href, document.cookie, localStorage.length, sessionStorage.length])`).then((s) => allTokens.every((tk) => !s.includes(tk)))
    && allTokens.every((tk) => !b.consoleText.join("\n").includes(tk)));
  log("host stopped (exit " + (await host.stop()) + ")");
  host = null;
} catch (e) {
  check("live browser check ran to the end", false, String(e && e.message || e).slice(0, 300));
} finally {
  if (browser) browser.close();
  if (host) await host.stop().catch(() => {});
}

log("result: " + (failures === 0 ? "PASS" : failures + " FAILED"));
writeFileSync(join(out, "live-ui.log"), lines.join("\n") + "\n");
const leaks = readdirSync(out).filter((f) => { const s = readFileSync(join(out, f)).toString("latin1"); return allTokens.some((tk) => tk && s.includes(tk)); });
if (leaks.length) { console.error("TOKEN IN EVIDENCE: " + leaks.join(", ")); process.exit(2); }
console.log("evidence: " + out);
process.exit(failures === 0 ? 0 : 1);
