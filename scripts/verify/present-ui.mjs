// Real-browser check of the demo launcher and host pairing (docs/cli-poc.md
// 56): scripts/demo/present.sh runs as the host would run it, under a
// pseudo-terminal (script(1)) so the host can show its pairing code there;
// this script reads the code from that terminal and never writes it down.
// The live mode runs the FAKE CLI behind a wrapper that answers the
// approved version; HOME is a test directory. It runs no Claude CLI.
// Local only, like browser-ui.mjs: the installed Google Chrome over the
// DevTools protocol, a throwaway profile, no HAR or network log; only the
// assertions and screenshots are kept, and every kept file is checked for
// tokens and pairing codes.
//
//   node scripts/verify/present-ui.mjs [--unity-project DIR] [--out DIR]
//
// Exit 0: every check passed.

import { spawn, execFileSync } from "node:child_process";
import { mkdtempSync, mkdirSync, readFileSync, writeFileSync, readdirSync, chmodSync, existsSync } from "node:fs";
import { createServer } from "node:http";
import { tmpdir } from "node:os";
import { join, resolve } from "node:path";

const repo = resolve(new URL("../..", import.meta.url).pathname);
const arg = (k, d) => { const i = process.argv.indexOf(k); return i > 0 ? process.argv[i + 1] : d; };
const unityProject = resolve(arg("--unity-project", join(repo, "unity/AgentPaymentAuthority")));
const stamp = new Date().toISOString().replace(/[-:]/g, "").replace(/\.\d+Z$/, "Z");
const commit = execFileSync("git", ["-C", repo, "rev-parse", "HEAD"]).toString().trim();
const dirty = execFileSync("git", ["-C", repo, "status", "--porcelain", "--untracked-files=no"]).toString().trim() !== "";
const out = resolve(arg("--out", join(repo, "artifacts/b1-04", "present-ui-" + commit.slice(0, 12) + "-" + stamp)));
mkdirSync(out, { recursive: true });
const CHROME = "/Applications/Google Chrome.app/Contents/MacOS/Google Chrome";
const LIVE_LABEL = "真實 Agent 即時展示・開發展示例外・合成資料・不付款";
const PLUGINS = [{ name: "cc-plugin-agents-md", path: "p" }, { name: "cc-plugin-plugin-authoring", path: "p" }];
const CODE = /[A-Z2-7]{4}-[A-Z2-7]{4}-[A-Z2-7]{4}/;

const lines = [];
let failures = 0;
const log = (s) => { lines.push(s); console.log(s); };
const check = (name, ok, detail = "") => { log((ok ? "PASS " : "FAIL ") + name + (detail ? "  [" + String(detail).replace(new RegExp(CODE, "g"), "<code>") + "]" : "")); if (!ok) failures++; };
const sleep = (ms) => new Promise((r) => setTimeout(r, ms));

const tmp = mkdtempSync(join(tmpdir(), "apa-present-ui-"));
chmodSync(tmp, 0o700);
const secrets = []; // tokens and codes: none may reach a kept file
log("commit " + commit + (dirty ? " (working tree has uncommitted changes)" : "") + "; fake CLI behind the live path (no model)");

// ---- the stand-in CLI, its HOME and the one-time settings ----
const bin = join(tmp, "bin");
mkdirSync(bin);
execFileSync("go", ["build", "-o", join(bin, "fake-claude"), "./cmd/fake-claude"], { cwd: repo, stdio: "inherit" });
const wrapper = (name, version) => {
  const p = join(bin, name);
  writeFileSync(p, "#!/bin/sh\nif [ \"$*\" = --version ]; then echo '" + version + "'; exit 0; fi\nexec '" + join(bin, "fake-claude") + "' \"$@\"\n", { mode: 0o755 });
  return p;
};
const cli = wrapper("claude", "2.1.293 (Claude Code)");
const oldCli = wrapper("claude-old", "2.1.292 (Claude Code)");
const home = join(tmp, "home");
mkdirSync(join(home, ".apa-claude"), { recursive: true, mode: 0o700 });
const conf = (name, text) => { const p = join(tmp, name); writeFileSync(p, text); return p; };
const confOk = conf("present.conf", "# one-time setup\ncli=" + cli + "\ndev_demo_plugins=true\n");
const confOld = conf("present-old.conf", "cli=" + oldCli + "\ndev_demo_plugins=true\n");
const confBad = conf("present-bad.conf", "cli = " + cli + "\n");
const tool = (name, product) => (product ? { do: "tool", tool: name, args: { product_id: product } } : { do: "tool", tool: name });
function script(...steps) {
  const init = { do: "init", init: { apiKeySource: "none", claude_code_version: "2.1.293", plugins: PLUGINS } };
  writeFileSync(join(bin, "fake-claude.scenario.json"), JSON.stringify({ manual_init: true, steps: [init, ...steps] }));
}
const purchase = [tool("list_products"), tool("get_quote", "kr-market"), tool("request_purchase", "kr-market")];
const result = { do: "raw", text: JSON.stringify({ type: "result", subtype: "success", is_error: false, result: "model text", num_turns: 4, session_id: "s", duration_ms: 1, usage: { input_tokens: 8, output_tokens: 445 } }) + "\n" };
// The open stub records the address it was given and whether that host
// answered then (the page is opened only once the host serves).
const opened = join(tmp, "opened.txt");
const openStub = join(bin, "open-stub");
writeFileSync(openStub, "#!/bin/sh\necho \"$1 $(curl -s -o /dev/null -w '%{http_code}' \"$1\")\" >> '" + opened + "'\n", { mode: 0o755 });
const sessions = join(tmp, "sessions");
mkdirSync(sessions, { mode: 0o700 });
const sessionCount = () => readdirSync(sessions).filter((n) => n.startsWith("present-")).length;
const openedLines = () => (existsSync(opened) ? readFileSync(opened, "utf8").trim().split("\n").filter(Boolean) : []);

// ---- the launcher, under a pseudo-terminal ----
function launch(mode, env = {}) {
  const e = { PATH: process.env.PATH, HOME: home, APA_ADDR: "127.0.0.1:0", APA_PRESENT_DIR: sessions, APA_PRESENT_CONF: confOk, APA_PRESENT_OPEN: openStub,
    APA_UNITY_PROJECT: unityProject, ...(process.env.GOCACHE ? { GOCACHE: process.env.GOCACHE } : {}), ...env };
  const p = spawn("script", ["-q", "/dev/null", "sh", join(repo, "scripts/demo/present.sh"), mode], { cwd: tmp, env: e, stdio: ["pipe", "pipe", "pipe"] });
  let term = "";
  p.stdout.on("data", (b) => { term += b; });
  p.stderr.on("data", (b) => { term += b; });
  const exited = new Promise((r) => p.once("exit", (code) => r(code)));
  return {
    exited, term: () => term,
    async ready(before) {
      for (let i = 0; i < 1800; i++) {
        const o = openedLines();
        const code = term.match(CODE);
        const s = term.match(/present: 場次 (\S+)；/);
        if (o.length > before && code && s) {
          const ready = JSON.parse(readFileSync(join(s[1], "ready.json"), "utf8"));
          secrets.push(code[0]);
          const tok = JSON.parse(readFileSync(join(s[1], "tok", "tokens.json"), "utf8")).tokens;
          secrets.push(...tok.map((t) => t.token));
          return { url: o[o.length - 1].split(" ")[0], answered: o[o.length - 1].split(" ")[1], code: code[0], session: s[1], pid: ready.pid, addr: ready.addr, tokens: tok };
        }
        if (p.exitCode !== null) throw new Error("the launcher ended: " + term.replace(new RegExp(CODE, "g"), "<code>").slice(-600));
        await sleep(100);
      }
      throw new Error("the launcher did not get ready");
    },
    stopHost: async (pid) => { try { process.kill(pid, "SIGINT"); } catch {} return Promise.race([exited, sleep(20000).then(() => "timeout")]); },
  };
}
const ended = (l, ms = 180000) => Promise.race([l.exited, sleep(ms).then(() => "timeout")]);
const lastLine = (l) => l.term().replace(/\r/g, "").trim().split("\n").filter((x) => x.includes("present:")).pop() || "";
async function api(addr, token, path) {
  const r = await fetch("http://" + addr + path, { headers: token ? { Authorization: "Bearer " + token } : {} });
  return { status: r.status, json: await r.json().catch(() => null) };
}

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
  const listeners = new Map(); // CDP event -> handlers (Fetch.requestPaused)
  ws.addEventListener("message", (ev) => {
    const m = JSON.parse(ev.data);
    if (m.method && listeners.has(m.method)) for (const fn of listeners.get(m.method)) fn(m.params);
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
  const on = (method, fn) => { listeners.set(method, [...(listeners.get(method) || []), fn]); };
  return { send, evaluate, waitFor, on, consoleText, close: () => { ws.close(); c.kill(); } };
}

const text = (sel) => `(document.querySelector(${JSON.stringify(sel)})?.textContent || "")`;
// Shown on screen, by the computed style: not only the hidden attribute.
const visible = (sel) => `(() => { const e = document.querySelector(${JSON.stringify(sel)}); return !!e && !e.hidden && e.offsetParent !== null && getComputedStyle(e).display !== "none"; })()`;
const inView = (sel) => `(() => { const e = document.querySelector(${JSON.stringify(sel)}); if (!e || e.offsetParent === null) return false; const r = e.getBoundingClientRect();
  return r.width > 0 && r.top >= 0 && r.left >= 0 && r.bottom <= innerHeight && r.right <= innerWidth; })()`;
const uKey = (k) => `(document.getElementById("unity-status").dataset.${k} || "")`;
const viewport = (b, w, h) => b.send("Emulation.setDeviceMetricsOverride", { width: w, height: h, deviceScaleFactor: 1, mobile: false });
async function shot(b, name, full) {
  if (full) {
    const h = Math.min(4000, await b.evaluate(`Math.ceil(document.documentElement.scrollHeight)`));
    await viewport(b, 1280, h);
    await sleep(500);
  }
  const fields = await b.evaluate(`JSON.stringify([document.getElementById("pair-code")?.value || "", document.getElementById("token")?.value || ""])`);
  check("screenshot " + name + ": the code and token fields are empty", fields === '["",""]');
  const png = await b.send("Page.captureScreenshot", { format: "png", captureBeyondViewport: false });
  writeFileSync(join(out, name + ".png"), Buffer.from(png.data, "base64"));
  log("  " + name + ".png: " + (await b.evaluate(text("#current"))) + "; run「" + (await b.evaluate(text("#run-status"))) + "」");
  if (full) { await viewport(b, 1280, 900); await sleep(300); }
}
async function typeCode(b, code) {
  await b.evaluate(`(document.getElementById("pair-code").focus(), true)`);
  await b.send("Input.insertText", { text: code }); // into the masked field; never in an expression or a log
  await b.evaluate(`(document.querySelector("#pair-form button[type=submit]").click(), true)`);
}
const paired = `document.getElementById("creds").dataset.mode === "paired" && !document.getElementById("control").hidden`;
const notPaired = `document.getElementById("creds").dataset.mode !== "paired" && document.getElementById("control").hidden`;

let browser = null;
const running = [];
try {
  // ---- the launcher refuses before anything runs or opens ----
  const n0 = sessionCount();
  let l = launch("live", { APA_PRESENT_CONF: join(tmp, "missing.conf") });
  check("no one-time setting: stopped before anything, no session", (await ended(l)) === 2 && sessionCount() === n0 && openedLines().length === 0, lastLine(l));
  l = launch("live", { APA_PRESENT_CONF: confBad });
  check("a setting that is not plain cli=/…: stopped before anything", (await ended(l)) === 2 && sessionCount() === n0 && openedLines().length === 0, lastLine(l));
  l = launch("live", { APA_PRESENT_GO: "/usr/bin/false" });
  check("a failed build: stopped, nothing started, nothing opened", (await ended(l)) === 2 && openedLines().length === 0 && lastLine(l).includes("建置失敗"), lastLine(l));
  l = launch("live", { APA_PRESENT_CONF: confOld });
  check("a CLI of another version: the host stops before ready; the launcher stops, opens nothing, retries nothing",
    (await ended(l)) === 2 && openedLines().length === 0 && lastLine(l).includes("就緒前結束") && !CODE.test(l.term()), lastLine(l));
  // A bystander on the port: refused, never touched.
  const bystander = createServer((q, s) => { s.end("bystander"); });
  await new Promise((r) => bystander.listen(0, "127.0.0.1", r));
  const bport = bystander.address().port;
  l = launch("live", { APA_ADDR: "127.0.0.1:" + bport });
  const rc = await ended(l);
  const still = await (await fetch("http://127.0.0.1:" + bport + "/")).text();
  check("the address in use: stopped, the bystander still serves, nothing opened", rc === 2 && still === "bystander" && openedLines().length === 0, lastLine(l));
  bystander.close();
  // The browser cannot be opened: the address is said, the host keeps serving; nothing new starts.
  const nBefore = sessionCount();
  l = launch("live", { APA_PRESENT_OPEN: "/usr/bin/false" });
  let s = null;
  for (let i = 0; i < 1800 && !s; i++, await sleep(100)) { const m = l.term().match(/present: 無法自動開啟瀏覽器；請手動開啟 (http:\/\/\S+\/host)/); if (m) s = m[1]; }
  const failedOpen = l.term().match(/present: 場次 (\S+)；/);
  const pidOpen = failedOpen ? JSON.parse(readFileSync(join(failedOpen[1], "ready.json"), "utf8")).pid : 0;
  check("the browser not opened: the address is printed (no secret in it) and the host serves", !!s && !CODE.test(s) && (await fetch(s)).status === 200 && sessionCount() === nBefore + 1, s);
  const code0 = (l.term().match(CODE) || [""])[0];
  secrets.push(code0);
  await l.stopHost(pidOpen);
  check("…Ctrl-C to that host: it ends, its session is kept", existsSync(join(failedOpen[1], "db")));

  // ---- session A (live): wrong code, a late answer, a used code, recovery by file ----
  script(...purchase, { do: "sleep", ms: 6000 }, result);
  const A = launch("live");
  const a = await A.ready(0);
  running.push({ stopHost: () => A.stopHost(a.pid) });
  check("A: the page opened once the host served (it answered then), no secret in the address", a.answered === "200" && a.url === "http://" + a.addr + "/host" && !a.url.includes(a.code.slice(0, 4)), a.answered);
  browser = await startChrome();
  const b = browser;
  await viewport(b, 1280, 900);
  await b.send("Page.navigate", { url: a.url });
  await b.waitFor(`document.readyState === "complete"`);
  check("A: before pairing, the header says live (GET /api/mode), the code field is offered, nothing to control",
    await b.waitFor(`${text("header .badge")} === ${JSON.stringify(LIVE_LABEL)} && ${visible("#pair-form")} && ${visible("#live-note")} && document.getElementById("control").hidden`, 20000));
  await b.evaluate(`(() => { const orig = window.fetch.bind(window); window.__pr = { hold: false, held: [] };
    window.fetch = (url, opts = {}) => {
      if (!String(url).endsWith("/api/host/pair") || !window.__pr.hold) return orig(url, opts);
      // Sent now (the server answers and uses the code); only the answer is
      // held, past the page's abort: an answer that arrives late.
      const sent = orig(url, { ...opts, signal: undefined });
      return new Promise((res, rej) => window.__pr.held.push(() => sent.then(res, rej)));
    }; return true; })()`);
  await typeCode(b, "AAAA-AAAA-AAAA");
  check("A: a wrong code: refused, said, field emptied", await b.waitFor(`${text("#pair-note")}.includes("配對碼不正確") && ${notPaired} && document.getElementById("pair-code").value === ""`, 10000));
  await b.evaluate(`(window.__pr.hold = true, true)`);
  await typeCode(b, a.code);
  await b.waitFor(`window.__pr.held.length === 1`, 10000);
  await b.evaluate(`(document.getElementById("creds-box").open = true, document.getElementById("clear-all").click(), true)`);
  await b.evaluate(`(window.__pr.hold = false, window.__pr.held.splice(0).forEach((f) => f()), true)`);
  await sleep(2000);
  check("A: the answer that came after 清除全部 signed nothing in", await b.evaluate(notPaired) && (await b.evaluate(`document.getElementById("creds").dataset.mode`)) === "none"
    && (await b.evaluate(text("#creds"))).includes("已清除全部憑證"));
  await typeCode(b, a.code);
  check("A: the used code: closed, with the recovery way", await b.waitFor(`${text("#pair-note")}.includes("已使用、逾時或失效") && ${notPaired}`, 10000));
  const doc = await b.send("DOM.getDocument", {});
  const q = await b.send("DOM.querySelector", { nodeId: doc.root.nodeId, selector: "#token-file" });
  await b.send("DOM.setFileInputFiles", { nodeId: q.nodeId, files: [join(a.session, "tok", "tokens.json")] });
  check("A: recovery by the advanced token file", await b.waitFor(`document.getElementById("creds").dataset.mode === "file" && !document.getElementById("control").hidden`, 20000));
  check("A: nothing ran meanwhile", (await api(a.addr, a.tokens.find((t) => t.role === "operator").token, "/api/host/runs")).json.runs.length === 0
    && readdirSync(join(a.session, "runs")).length === 0);
  // ---- review R2: the mode is unknown until an answer confirms it ----
  // GET /api/mode is intercepted in the browser (CDP Fetch): held, 503,
  // dropped, an old "fake" answer, or passed on.
  const modeReq = { how: "pass", held: [] };
  b.on("Fetch.requestPaused", (p) => {
    const id = p.requestId;
    if (modeReq.how === "hold") modeReq.held.push(id);
    else if (modeReq.how === "503") b.send("Fetch.fulfillRequest", { requestId: id, responseCode: 503, body: Buffer.from('{"error":"store_unavailable"}').toString("base64") }).catch(() => {});
    else if (modeReq.how === "drop") b.send("Fetch.failRequest", { requestId: id, errorReason: "ConnectionFailed" }).catch(() => {});
    else b.send("Fetch.continueRequest", { requestId: id }).catch(() => {});
  });
  await b.send("Fetch.enable", { patterns: [{ urlPattern: "*/api/mode*" }] });
  const header = `(${text("header.top")})`;
  const loadFile = async () => {
    const d = await b.send("DOM.getDocument", {});
    const f = await b.send("DOM.querySelector", { nodeId: d.root.nodeId, selector: "#token-file" });
    await b.send("DOM.setFileInputFiles", { nodeId: f.nodeId, files: [join(a.session, "tok", "tokens.json")] });
  };
  modeReq.how = "hold";
  await b.send("Page.navigate", { url: a.url });
  await b.waitFor(`document.readyState === "complete"`);
  check("R2: the mode answer held: the header says 模式未確認, not fake", await b.waitFor(`${text("header .badge")} === "模式未確認（讀取中）"`, 10000) && modeReq.held.length >= 1
    && !(await b.evaluate(header)).includes("M1／fake agent") && !(await b.evaluate(header)).includes("真實 Agent 未啟用") && !(await b.evaluate(visible("#live-note"))), (await b.evaluate(header)).replace(/\s+/g, " ").slice(0, 160));
  await shot(b, "r2-mode-held");
  for (const [how, name] of [["503", "503"], ["drop", "a dropped connection"]]) {
    modeReq.how = how;
    await b.send("Page.navigate", { url: a.url });
    await b.waitFor(`document.readyState === "complete"`);
    check("R2: " + name + ": the viewer reads 讀取失敗, never fake", await b.waitFor(`${text("header .badge")} === "模式未確認：讀取失敗（重試中）"`, 10000)
      && !(await b.evaluate(header)).includes("M1／fake agent") && !(await b.evaluate(header)).includes("真實 Agent 未啟用") && !(await b.evaluate(visible("#live-note"))),
      (await b.evaluate(header)).replace(/\s+/g, " ").slice(0, 160));
    if (how === "503") await shot(b, "r2-mode-failed");
    modeReq.how = "pass"; // the page tries again by itself
    check("R2: after " + name + ", a good answer: the live label and its warnings", await b.waitFor(`${text("header .badge")} === ${JSON.stringify(LIVE_LABEL)} && ${visible("#live-note")}
      && ${text("header .caps")}.includes("開發展示例外")`, 15000));
  }
  for (const [late, name] of [[{ responseCode: 200, body: '{"mode":"fake"}' }, "an older fake answer"], [{ responseCode: 503, body: "{}" }, "an older failure"]]) {
    modeReq.how = "hold";
    modeReq.held = [];
    await b.send("Page.navigate", { url: a.url });
    await b.waitFor(`document.readyState === "complete"`);
    await b.waitFor(`${text("header .badge")} === "模式未確認（讀取中）"`, 10000);
    await loadFile(); // the host status confirms live meanwhile
    await b.waitFor(`${text("header .badge")} === ${JSON.stringify(LIVE_LABEL)}`, 20000);
    for (const id of modeReq.held.splice(0)) await b.send("Fetch.fulfillRequest", { requestId: id, responseCode: late.responseCode, body: Buffer.from(late.body).toString("base64") }).catch(() => {});
    modeReq.how = "pass";
    await sleep(2500);
    check("R2: " + name + " after the mode was confirmed changes nothing", (await b.evaluate(text("header .badge"))) === LIVE_LABEL && await b.evaluate(visible("#live-note")),
      await b.evaluate(text("header .badge")));
  }
  await b.send("Fetch.disable", {});
  check("R2: reading the mode, failing and recovering ran nothing", (await api(a.addr, a.tokens.find((t) => t.role === "operator").token, "/api/host/runs")).json.runs.length === 0
    && readdirSync(join(a.session, "runs")).length === 0);
  await A.stopHost(a.pid);

  // ---- session B (live): the usual way ----
  const B = launch("live");
  const bb = await B.ready(openedLines().length);
  running.push({ stopHost: () => B.stopHost(bb.pid) });
  const op = bb.tokens.find((t) => t.role === "operator").token;
  await b.send("Page.navigate", { url: bb.url });
  await b.waitFor(`document.readyState === "complete"`);
  await b.waitFor(`["ready","rendered"].includes(document.getElementById("unity-status").dataset.state)`, 90000);
  await b.waitFor(`${text("header .badge")} === ${JSON.stringify(LIVE_LABEL)}`, 20000);
  check("B (1280×900): before pairing, the shop, the code field and the live note are in the first screen",
    await b.evaluate(inView("#unity-canvas")) && await b.evaluate(inView("#pair-code")) && await b.evaluate(inView("#pair-form button[type=submit]")) && await b.evaluate(inView("#live-note")));
  await shot(b, "1-before-pairing");
  await typeCode(b, a.code); // the old service's code
  check("B: the old service's code is no code here", await b.waitFor(`${text("#pair-note")}.includes("配對碼不正確") && ${notPaired}`, 10000));
  await typeCode(b, bb.code);
  check("B: one pairing: credentials held, the field emptied, no file chosen, the status bar says paired", await b.waitFor(`${paired} && ${text("#status")}.startsWith("已配對")`, 20000) && (await b.evaluate(`document.getElementById("pair-code").value`)) === ""
    && (await b.evaluate(`document.getElementById("token-file").value`)) === "" && !(await b.evaluate(visible("#pair-form"))));
  check("B: the live warning, the real-CLI start, the stop slot and 查看證據 in the first screen",
    await b.waitFor(`${visible("#live-warning")} && [...document.querySelectorAll("#run-scenarios button")].some((x) => x.textContent.startsWith("開始（真實 CLI）"))`, 20000)
    && await b.evaluate(inView("#live-warning")) && await b.evaluate(inView("#run-scenarios button")) && await b.evaluate(inView("#show-evidence")) && await b.evaluate(inView("#unity-canvas")));
  const buildRow = await b.evaluate(`(document.querySelector('#readiness tr[data-item="建置"]')?.textContent || "")`);
  check("B: the readiness says the build as recorded (revision and modified)", /[0-9a-f]{12}（modified=(true|false)/.test(buildRow), buildRow);
  await sleep(2000);
  check("B: pairing ran nothing", (await api(bb.addr, op, "/api/host/runs")).json.runs.length === 0 && readdirSync(join(bb.session, "runs")).length === 0);
  await shot(b, "2-paired");
  const kept = await b.evaluate(`JSON.stringify([location.href, document.cookie, localStorage.length, sessionStorage.length])`);
  check("B: nothing of the code or a token in the URL, cookies or storage", kept === JSON.stringify([bb.url, "", 0, 0]), kept);

  // Start (one click), the Dashboard follows; 查看證據 opens the task's evidence.
  await b.evaluate(`[...document.querySelectorAll("#run-scenarios button")].find((x) => x.textContent.startsWith("開始（真實 CLI）"))?.click()`);
  check("B: one click starts one run; the Dashboard switches to its task", await b.waitFor(`${text("#current")}.includes("show-a-01") && ${uKey("seq")} === "3" && ${uKey("state")} === "rendered"`, 30000));
  check("B: running: the stop button and the run status in the first screen", await b.waitFor(`!!document.querySelector("#run-scenarios .row-stop")`, 10000) && await b.evaluate(inView("#run-scenarios .row-stop")) && await b.evaluate(inView("#run-status")));
  await shot(b, "3-running");
  await b.evaluate(`(document.getElementById("show-evidence").click(), true)`);
  check("B: 查看證據 opens the task's evidence where it is", await b.waitFor(`document.getElementById("evidence-box").open && ${text("#evidence")}.includes("po-show-a-01-kr-market")`, 10000));
  check("B: after the CLI ended, the tool calls are there", await b.waitFor(`${text("#evidence")}.includes("#3 request_purchase") && ${text("#evidence")}.includes("input 8 · output 445")`, 30000));
  await shot(b, "4-evidence", true);
  // A second run, stopped.
  script(...purchase, { do: "hang" });
  await b.evaluate(`(window.scrollTo(0, 0), true)`);
  await b.waitFor(`[...document.querySelectorAll("#run-scenarios button[data-scenario]")].some((x) => !x.disabled)`, 30000);
  await b.evaluate(`[...document.querySelectorAll("#run-scenarios button[data-scenario]")].find((x) => !x.disabled)?.click()`);
  check("B: the second run's task opens", await b.waitFor(`${text("#current")}.includes("show-a-02") && ${uKey("seq")} === "3"`, 30000));
  await b.waitFor(`!!document.querySelector("#run-scenarios .row-stop")`, 20000);
  await b.evaluate(`document.querySelector("#run-scenarios .row-stop").click()`);
  check("B: 停止 accepted, the result not yet claimed", await b.waitFor(`${text("#run-status")}.includes("停止請求已受理")`, 20000));
  check("B: the cancel reaches the Timeline (#4 withdrawn, #5 cancelled)", await b.waitFor(`[...document.querySelectorAll("#timeline li")].map((li) => li.dataset.seq).join(",") === "1,2,3,4,5"`, 30000));
  await shot(b, "5-stopped");
  // Narrow: still operable.
  await viewport(b, 420, 900);
  await sleep(800);
  check("B (420 wide): the start and 查看證據 buttons fit the width", await b.evaluate(`[...document.querySelectorAll("#run-scenarios button, #show-evidence")].every((x) => { const r = x.getBoundingClientRect(); return r.width > 0 && r.left >= 0 && r.right <= innerWidth; })`));
  await shot(b, "6-narrow");
  await viewport(b, 1280, 900);
  // Reload: the credentials are gone; the used code stays used; nothing runs.
  const runsBefore = (await api(bb.addr, op, "/api/host/runs")).json.runs.length;
  await b.send("Page.reload", {});
  await b.waitFor(`document.readyState === "complete"`);
  check("B: reloaded: not paired, the code field again", await b.waitFor(`${notPaired} && ${visible("#pair-form")}`, 20000));
  await typeCode(b, bb.code);
  check("B: the used code after a reload: closed", await b.waitFor(`${text("#pair-note")}.includes("已使用、逾時或失效")`, 10000));
  await sleep(1500);
  // Each run leaves its trace (.jsonl) and, since M-B, its timing record
  // (.timing.json) beside it: the traces count the runs.
  const runFiles = readdirSync(join(bb.session, "runs"));
  const runsAfter = (await api(bb.addr, op, "/api/host/runs")).json.runs.length;
  check("B: no run from loading, pairing, reloading", runsAfter === runsBefore && runsBefore === 2 && runFiles.filter((f) => f.endsWith(".jsonl")).length === 2,
    "runs " + runsBefore + " -> " + runsAfter + "; files " + runFiles.map((f) => f.replace(/^.*\./, ".")).join(","));
  check("B: no token or code in the console", secrets.every((x) => x && !b.consoleText.join("\n").includes(x)));
  await B.stopHost(bb.pid);

  // ---- session C (fake): the rehearsal says fake ----
  const C = launch("fake");
  const cc = await C.ready(openedLines().length);
  running.push({ stopHost: () => C.stopHost(cc.pid) });
  await b.send("Page.navigate", { url: cc.url });
  await b.waitFor(`document.readyState === "complete"`);
  await typeCode(b, cc.code);
  check("C (fake): after pairing, the header says fake and the start says no model", await b.waitFor(`${paired} && ${text("header .badge")} === "M1／fake agent／不付款"
    && [...document.querySelectorAll("#run-scenarios button[data-scenario]")].length > 0 && [...document.querySelectorAll("#run-scenarios button[data-scenario]")].every((x) => x.textContent.startsWith("開始（fake，不執行模型）"))`, 20000));
  check("C (fake): /api/mode says fake", (await api(cc.addr, null, "/api/mode")).json.mode === "fake");
  await C.stopHost(cc.pid);
} catch (e) {
  check("present browser check ran to the end", false, String(e && e.message || e).slice(0, 300));
} finally {
  if (browser) browser.close();
  for (const r of running) if (r.stopHost) await r.stopHost().catch(() => {});
}

log("result: " + (failures === 0 ? "PASS" : failures + " FAILED"));
writeFileSync(join(out, "present-ui.log"), lines.join("\n").replace(new RegExp(CODE, "g"), "<code>") + "\n");
const leaks = readdirSync(out).filter((f) => { const s = readFileSync(join(out, f)).toString("latin1"); return secrets.some((x) => x && s.includes(x)) || (f.endsWith(".log") && CODE.test(s)); });
if (leaks.length) { console.error("SECRET IN EVIDENCE: " + leaks.join(", ")); process.exit(2); }
console.log("evidence: " + out);
process.exit(failures === 0 ? 0 : 1);
