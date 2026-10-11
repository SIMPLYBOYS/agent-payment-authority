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
import { request as httpRequest } from "node:http";

const repo = resolve(new URL("../..", import.meta.url).pathname);
const arg = (k, d) => { const i = process.argv.indexOf(k); return i > 0 ? process.argv[i + 1] : d; };
const unityBuild = resolve(arg("--unity-build", join(repo, "unity/AgentPaymentAuthority/Build/WebGL/Build")));
const stamp = new Date().toISOString().replace(/[-:]/g, "").replace(/\.\d+Z$/, "Z");
const commit = execFileSync("git", ["-C", repo, "rev-parse", "HEAD"]).toString().trim();
const out = resolve(arg("--out", join(repo, "artifacts/b1-06", "ui-" + commit.slice(0, 12) + "-" + stamp)));
mkdirSync(out, { recursive: true });
const CHROME = "/Applications/Google Chrome.app/Contents/MacOS/Google Chrome";
// --smoke-plain: a short flow only, in a Chrome started WITHOUT the two flags
// added for D1 (review of 7ffc45d, C1): whether Chrome's own backgrounding
// hides the page in that flow is recorded, not assumed.
const SMOKE = process.argv.includes("--smoke-plain");
const PAGEHIDE = process.argv.includes("--pagehide-only"); // targeted run of the 3.1 pagehide check
const PAGEHIDE_ROUNDS = 5;
const cases = JSON.parse(readFileSync(join(repo, "testdata/contracts/cases.json")));
const expect = (id) => cases.cases.find((c) => c.case_id === id).expected;

const lines = [];
let failures = 0;
function log(s) { lines.push(s); console.log(s); }
const started = Date.now();
const mark = (name) => log("  [" + Math.round((Date.now() - started) / 1000) + " s] " + name); // where the time goes
function check(name, ok, detail = "") { log((ok ? "PASS " : "FAIL ") + name + (detail ? "  [" + detail + "]" : "")); if (!ok) failures++; }
const sleep = (ms) => new Promise((r) => setTimeout(r, ms));

const tmp = mkdtempSync(join(tmpdir(), "apa-ui-"));
chmodSync(tmp, 0o700);
const bin = join(tmp, "bin");
mkdirSync(bin);
execFileSync("go", ["build", "-o", join(bin, "demo"), "./cmd/demo"], { cwd: repo, stdio: "inherit" });
execFileSync("go", ["build", "-o", join(bin, "fake-claude"), "./cmd/fake-claude"], { cwd: repo, stdio: "inherit" });

// ---- the demo host ----
function startDemo(state, plan, build, extra = [], addr = "127.0.0.1:0") {
  mkdirSync(state, { recursive: true, mode: 0o700 });
  const env = { PATH: process.env.PATH, HOME: process.env.HOME, APA_ADDR: addr, APA_DB_PATH: join(state, "demo.db"), APA_DEMO_TOKEN_FILE: join(state, "tokens.json") };
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
      return { a: get("viewer", "consumer-a"), b: get("viewer", "consumer-b"), host: get("host", ""), op: get("operator", "") };
    },
    async stop() { p.kill("SIGINT"); await new Promise((r) => p.once("exit", r)); },
  };
}

// ---- Chrome over the DevTools protocol ----
async function startChrome() {
  const profile = join(tmp, "chrome-profile");
  const c = spawn(CHROME, ["--headless=new", "--user-data-dir=" + profile, "--remote-debugging-port=0", "--no-first-run", "--no-default-browser-check",
    "--disable-extensions", "--disable-sync", "--disable-background-networking", "--disable-component-update", "--use-angle=swiftshader",
    ...(SMOKE ? [] : ["--disable-backgrounding-occluded-windows", "--disable-renderer-backgrounding"]),
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
  const listeners = new Map(); // CDP event -> handlers (Fetch.requestPaused)
  ws.addEventListener("message", (ev) => {
    const m = JSON.parse(ev.data);
    if (m.method && listeners.has(m.method)) for (const fn of listeners.get(m.method)) fn(m.params);
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
  // on adds a handler and returns its removal.
  const on = (method, fn) => {
    listeners.set(method, [...(listeners.get(method) || []), fn]);
    return () => listeners.set(method, (listeners.get(method) || []).filter((f) => f !== fn));
  };
  return { c, send, evaluate, waitFor, on, consoleText, close: () => { ws.close(); c.kill(); } };
}

const text = (sel) => `(document.querySelector(${JSON.stringify(sel)})?.textContent || "")`;

async function signIn(b, token) {
  // D1: the viewer form may sit in a folded section; a user opens it first.
  await b.evaluate(`(document.getElementById("token").closest("details")?.setAttribute("open", ""), document.getElementById("token").focus())`);
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

// shotFull keeps the whole page in one image (review of 667e933): the
// viewport is made as tall as the page, so the Unity canvas, Inspector and
// Timeline are in it, then set back. It logs what the shop, the status line
// and the Timeline say, so the image can be matched to the task's events.
async function shotFull(b, name) {
  const h = Math.min(4000, await b.evaluate(`Math.ceil(document.documentElement.scrollHeight)`));
  await shot(b, name, 1280, h, false);
  const said = await b.evaluate(`JSON.stringify({ unity: document.getElementById("unity-status").textContent, status: document.getElementById("status").textContent,
    timeline: [...document.querySelectorAll("#timeline li")].map((li) => li.dataset.seq).join(","), runStatus: (document.getElementById("run-status") || {}).textContent || "" })`);
  log("  " + name + ".png (full page): " + said);
  await b.send("Emulation.setDeviceMetricsOverride", { width: 1280, height: 900, deviceScaleFactor: 1, mobile: false });
}

async function hostEvents(addr, host, task) {
  const r = await fetch("http://" + addr + "/api/tasks/" + task + "/events", { headers: { Authorization: "Bearer " + host } });
  return (await r.json()).as_of_sequence;
}

// hostPost is a same-origin JSON POST as the page sends it (node:http, so
// Origin can be set); the token stays in this process.
function hostPost(addr, token, path, body) {
  return new Promise((resolve, reject) => {
    const [hostname, port] = addr.split(":");
    const data = JSON.stringify(body);
    const req = httpRequest({ hostname, port, path, method: "POST", headers: { Authorization: "Bearer " + token, Origin: "http://" + addr,
      "Content-Type": "application/json", "Content-Length": Buffer.byteLength(data) } }, (res) => {
      let text = "";
      res.on("data", (c) => { text += c; });
      res.on("end", () => resolve({ status: res.statusCode, json: JSON.parse(text || "{}") }));
    });
    req.on("error", reject);
    req.end(data);
  });
}

async function hostGet(addr, host, path) {
  const r = await fetch("http://" + addr + path, { headers: { Authorization: "Bearer " + host } });
  return { text: await r.clone().text(), json: await r.json() };
}

// What Unity itself reported it shows (read-only data-* on #unity-status).
const uKey = (k) => `(document.getElementById("unity-status").dataset.${k} || "")`;
function unityShows(b, w, ms = 20000) {
  const cond = [`document.getElementById("unity-status").dataset.state === "rendered"`];
  for (const k of ["task", "phase", "verdict", "reason"]) if (w[k] !== undefined) cond.push(`${uKey(k)} === ${JSON.stringify(w[k])}`);
  if (w.seq !== undefined) cond.push(`${uKey("seq")} === "${w.seq}"`);
  if (w.afterGen !== undefined) cond.push(`Number(${uKey("gen")}) > ${w.afterGen}`);
  return b.waitFor(cond.join(" && "), ms);
}
// M-B (docs/multi-agent-demo-design.md 5.4): on /host the shop follows each
// consumer's newest run; a chosen older task is in the panels, with a note
// that the scene shows the newer one, never in the shop.
const viewsTask = (b, task, ms = 15000) => b.waitFor(`(document.getElementById("unity-status").dataset.state === "rendered" && document.getElementById("unity-status").dataset.task === ${JSON.stringify(task)})
  || (!document.getElementById("view-note").hidden && (document.getElementById("inspector").textContent || "").includes(${JSON.stringify(task)}))`, ms);
// Nothing of a previous task on #unity-status: no data-*, none of words in its text.
const unityCleared = (words) => `(() => { const e = document.getElementById("unity-status"), d = e.dataset;
  return !d.gen && !d.seq && !d.task && !d.phase && !d.verdict && !d.reason && ${JSON.stringify(words)}.every((w) => !e.textContent.includes(w)); })()`;
const unityText = (b) => b.evaluate(`document.getElementById("unity-status").dataset.state + "|" + document.getElementById("unity-status").textContent`);
// A late "rendered" report of an old generation, delivered as the jslib does.
const lateReport = (gen, seq, detail) => `(window.apaUnityReport("rendered", ${gen}, ${seq}, ${JSON.stringify(detail)}), true)`;
const unityGen = (b) => b.evaluate(`Number(${uKey("gen")} || -1)`);
const pollCount = (b) => b.evaluate(`Number(document.getElementById("status").dataset.polls || 0)`);
async function waitPolls(b, n) { const p0 = await pollCount(b); return b.waitFor(`Number(document.getElementById("status").dataset.polls || 0) >= ${p0 + n}`, 20000); }
const visible = (sel) => `(() => { const e = document.querySelector(${JSON.stringify(sel)}); return !!e && !e.hidden && e.offsetParent !== null && getComputedStyle(e).display !== "none"; })()`;
async function openFixture(b, id) {
  await b.evaluate(`[...document.querySelectorAll("#task-pick button")].find(x => x.textContent.startsWith(${JSON.stringify(id + "（")}))?.click()`);
  return b.waitFor(`document.getElementById("status").dataset.kind === "ok" && document.getElementById("status").dataset.mode === "live"`);
}
async function replayAndRestore(b, label) {
  const g0 = await unityGen(b);
  await b.evaluate(`document.getElementById("replay").click()`);
  check(label + ": replay mode", await b.waitFor(`document.getElementById("status").dataset.mode === "replay" && ${visible("#replay-note")}`));
  check(label + ": Unity reports a replay step in a new generation", await b.waitFor(`Number(${uKey("gen")}) > ${g0}`, 20000));
  const replayGen = await unityGen(b);
  check(label + ": replay ends and polling resumes", await b.waitFor(`document.getElementById("status").dataset.mode === "live" && document.getElementById("status").dataset.kind === "ok"`, 40000));
  return replayGen;
}

// pagehideCheck (3.1; review of d7070af, C1): a readiness request really in
// flight, then the page is left, then the request is let go: nothing comes
// back. The order is set by conditions, not by waiting: a readiness poll is
// started on purpose (visibilitychange), the browser holds that one GET
// /api/host/status, and the request's end is seen on the network (its reply
// delivered, or cancelled by the page's abort) before anything is checked.
//
// Two ways (review of 78cb2cc's own run: the page's abort cancels a request
// held by the browser, so its reply can never be let go):
// - "network": the browser holds the request itself; after pagehide it must
//   end (cancelled by the page's abort, or its reply delivered);
// - "reply": the reply has come, but its body is held by a test-only fetch
//   wrapper in the page (like 3.1-R1's) and handed over after pagehide.
// Either way nothing may come back.
async function pagehideCheck(b, label, way = "network") {
  if (way === "reply") return pagehideReply(b, label);
  const paused = [], ended = new Map();
  let hold = true, polled = 0;
  const offPause = b.on("Fetch.requestPaused", (p) => {
    if (hold && p.request.url.includes("/api/host/status")) { hold = false; paused.push(p); return; }
    if (!hold && paused.length) polled++;
    b.send("Fetch.continueRequest", { requestId: p.requestId }).catch(() => {});
  });
  const offDone = b.on("Network.loadingFinished", (p) => ended.set(p.requestId, "reply delivered"));
  const offFail = b.on("Network.loadingFailed", (p) => ended.set(p.requestId, "ended: " + (p.canceled ? "cancelled by the page" : p.errorText)));
  await b.send("Network.enable");
  await b.send("Fetch.enable", { patterns: [{ urlPattern: "*/api/host/*" }, { urlPattern: "*/api/viewer*" }, { urlPattern: "*/api/tasks*" }] });
  await b.evaluate(`(document.dispatchEvent(new Event("visibilitychange")), true)`); // polls at once, or the running poll's timer follows
  for (const end = Date.now() + 15000; Date.now() < end && paused.length === 0; ) await sleep(50);
  const req = paused[0];
  check(label + ": one readiness request (GET /api/host/status) is held in flight", !!req && req.request.method === "GET", req ? req.request.url.replace(/^https?:\/\/[^/]+/, "") : "none paused within 15 s");
  if (req) {
    const shownBefore = await b.evaluate(`document.getElementById("creds").dataset.mode + "|" + ${text("#viewer")}`);
    await b.evaluate(`window.dispatchEvent(new PageTransitionEvent("pagehide", { persisted: true })), true`);
    const let_go = await b.send("Fetch.continueRequest", { requestId: req.requestId }).then(() => "continued", (e) => "not continued (" + e.message + ")");
    for (const end = Date.now() + 10000; Date.now() < end && !ended.has(req.networkId); ) await sleep(50);
    check(label + ": the held request then ended", ended.has(req.networkId), "before pagehide " + shownBefore + "; " + let_go + "; " + (ended.get(req.networkId) || "still open after 10 s"));
    const gone = await b.evaluate(`document.getElementById("creds").dataset.mode === "none" && ${text("#viewer")} === "未驗證" && ${text("#inspector")} === "尚無資料" && ${text("#timeline")} === "尚無事件" && ${unityCleared(["task-a", "task-b", "HUMAN_APPROVAL_REQUIRED", "INSUFFICIENT_BUDGET"])}`);
    check(label + ": tokens dropped, Dashboard and shop cleared; the late request restores nothing", gone,
      await b.evaluate(`JSON.stringify({ mode: document.getElementById("creds").dataset.mode, viewer: ${text("#viewer")}, unity: document.getElementById("unity-status").textContent })`));
    polled = 0;
    await sleep(6000); // longer than the 5 s readiness period: any poll would show
    check(label + ": no more requests after leaving (readiness, viewer, tasks)", polled === 0, polled + " requests in 6 s");
    await b.evaluate(`window.dispatchEvent(new PageTransitionEvent("pageshow", { persisted: true })), true`);
    await sleep(500);
    check(label + ": pageshow: nothing comes back by itself", await b.evaluate(`document.getElementById("creds").dataset.mode === "none" && ${text("#viewer")} === "未驗證"`));
  }
  offPause(); offDone(); offFail();
  await b.send("Fetch.disable");
  await b.send("Network.disable");
}

async function pagehideReply(b, label) {
  await b.evaluate(`(() => { if (!window.__statusHold) { window.__statusHold = { arm: false, held: [], let: [] }; const orig = window.fetch.bind(window);
    window.fetch = async (url, opts) => { const res = await orig(url, opts);
      if (String(url).includes("/api/host/status") && window.__statusHold.arm) { window.__statusHold.arm = false; const body = await res.json();
        return { status: res.status, ok: res.ok, json: () => new Promise((r) => window.__statusHold.held.push(() => { window.__statusHold.let.push(true); r(body); })) }; }
      return res; }; } window.__statusHold.arm = true; window.__statusHold.held = []; window.__statusHold.let = []; return true; })()`);
  await b.evaluate(`(document.dispatchEvent(new Event("visibilitychange")), true)`); // polls at once, or the running poll's timer follows
  check(label + ": a readiness reply (GET /api/host/status) has come and its body is held", await b.waitFor(`window.__statusHold.held.length === 1`, 15000));
  await b.evaluate(`window.dispatchEvent(new PageTransitionEvent("pagehide", { persisted: true })), true`);
  await b.evaluate(`(window.__statusHold.held.splice(0).forEach((f) => f()), true)`);
  check(label + ": after pagehide the held reply was handed to the page", await b.waitFor(`window.__statusHold.let.length === 1`, 5000));
  await b.evaluate(`new Promise((r) => setTimeout(r, 0)).then(() => true)`); // the page's await on the body has run
  const gone = await b.evaluate(`document.getElementById("creds").dataset.mode === "none" && ${text("#viewer")} === "未驗證" && ${text("#inspector")} === "尚無資料" && ${text("#timeline")} === "尚無事件" && document.getElementById("readiness").textContent.includes("未確認")`);
  check(label + ": the late reply restores no credential, picture or readiness", gone,
    await b.evaluate(`JSON.stringify({ mode: document.getElementById("creds").dataset.mode, viewer: ${text("#viewer")}, readiness: document.getElementById("readiness").textContent.slice(0, 120) })`));
  let polled = 0;
  const off = b.on("Fetch.requestPaused", (p) => { polled++; b.send("Fetch.continueRequest", { requestId: p.requestId }).catch(() => {}); });
  await b.send("Fetch.enable", { patterns: [{ urlPattern: "*/api/host/*" }, { urlPattern: "*/api/viewer*" }, { urlPattern: "*/api/tasks*" }] });
  await sleep(6000); // longer than the 5 s readiness period: any poll would show
  check(label + ": no more requests after leaving (readiness, viewer, tasks)", polled === 0, polled + " requests in 6 s");
  off();
  await b.send("Fetch.disable");
}

let demo, browser;
const allTokens = [];
try {
full: {
  if (SMOKE) {
    log("commit " + commit + " (smoke, Chrome without --disable-backgrounding-occluded-windows / --disable-renderer-backgrounding)");
    const st = join(tmp, "state-smoke");
    demo = startDemo(st, null, unityBuild, ["--fake-cli", join(bin, "fake-claude"), "--scenarios", join(repo, "testdata/demo/scenarios-acceptance.json"), "--registry", join(repo, "testdata/demo/registry.json")]);
    const sAddr = await demo.ready();
    const stok = demo.tokens();
    allTokens.push(stok.a, stok.b, stok.host, stok.op);
    browser = await startChrome();
    const b = browser;
    await b.send("Emulation.setDeviceMetricsOverride", { width: 1280, height: 900, deviceScaleFactor: 1, mobile: false });
    await b.send("Page.navigate", { url: "http://" + sAddr + "/host" });
    await b.waitFor(`document.readyState === "complete"`);
    check("smoke: the Unity build loads", await b.waitFor(`["ready","rendered"].includes(document.getElementById("unity-status").dataset.state)`, 90000));
    const vis = [];
    const sample = async (tag) => vis.push(tag + ":" + (await b.evaluate("document.visibilityState")));
    await sample("loaded");
    const sFile = join(tmp, "tokens-smoke.json");
    writeFileSync(sFile, readFileSync(join(st, "tokens.json")));
    const doc = await b.send("DOM.getDocument", {});
    const q = await b.send("DOM.querySelector", { nodeId: doc.root.nodeId, selector: "#token-file" });
    await b.send("DOM.setFileInputFiles", { nodeId: q.nodeId, files: [sFile] });
    check("smoke: the token file loads", await b.waitFor(`document.getElementById("creds").dataset.mode === "file"`, 20000));
    await b.waitFor(`!!document.querySelector('#run-scenarios button[data-scenario="ask"]:not([disabled])')`, 20000);
    await b.evaluate(`document.querySelector('#run-scenarios button[data-scenario="ask"]').click()`);
    check("smoke: an Ask opens; card, Unity and Timeline agree", await b.waitFor(`document.getElementById("card").dataset.po !== "" && (document.querySelector(".card-status") || {}).innerText === "Ask：等待人工核准（M1 無核准能力）"
      && document.getElementById("unity-status").dataset.phase === "waiting" && document.querySelectorAll("#timeline li").length === 3`, 30000), await b.evaluate(text("#unity-status")));
    await sample("ask");
    const p0 = await pollCount(b);
    for (let i = 0; i < 20; i++) { await sleep(1000); if (i % 5 === 4) await sample("t" + (i + 1)); }
    const polled = (await pollCount(b)) - p0;
    await b.evaluate(`document.getElementById("replay").click()`);
    const replayed = await b.waitFor(`document.getElementById("status").dataset.mode === "replay"`, 5000) && await b.waitFor(`document.getElementById("status").dataset.mode === "live" && document.getElementById("status").dataset.kind === "ok"`, 60000);
    await sample("after-replay");
    log("  smoke visibility samples: " + vis.join(" ") + "; task polls over 20 s: " + polled);
    check("smoke: the page stayed visible and kept polling (about one poll a second), the replay returned to live", vis.every((x) => x.endsWith(":visible")) && polled >= 12 && replayed, vis.join(" ") + " / " + polled + " polls");
    break full;
  }
  if (PAGEHIDE) {
    // Targeted (review of d7070af, C1): only the 3.1 pagehide check, five
    // rounds on one fake-plan host; its result is "targeted", not the suite's.
    log("commit " + commit + " (targeted: 3.1 pagehide only, " + PAGEHIDE_ROUNDS + " rounds each way)");
    const st = join(tmp, "state-pagehide");
    demo = startDemo(st, join(repo, "testdata/demo/fake-plan.json"), unityBuild);
    const pAddr = await demo.ready("task-b");
    const ptok = demo.tokens();
    allTokens.push(ptok.a, ptok.b, ptok.host, ptok.op);
    const pFile = join(tmp, "tokens-pagehide.json");
    writeFileSync(pFile, readFileSync(join(st, "tokens.json")));
    browser = await startChrome();
    const b = browser;
    await b.send("Emulation.setDeviceMetricsOverride", { width: 1280, height: 900, deviceScaleFactor: 1, mobile: false });
    for (let i = 1; i <= 2 * PAGEHIDE_ROUNDS; i++) {
      const way = i <= PAGEHIDE_ROUNDS ? "network" : "reply", r = way + " " + (i <= PAGEHIDE_ROUNDS ? i : i - PAGEHIDE_ROUNDS);
      await b.send("Page.navigate", { url: "http://" + pAddr + "/host" });
      await b.waitFor(`document.readyState === "complete"`);
      await b.waitFor(`["ready","rendered","failed"].includes(document.getElementById("unity-status").dataset.state)`, 90000);
      const doc = await b.send("DOM.getDocument", {});
      const q = await b.send("DOM.querySelector", { nodeId: doc.root.nodeId, selector: "#token-file" });
      await b.send("DOM.setFileInputFiles", { nodeId: q.nodeId, files: [pFile] });
      await b.waitFor(`document.getElementById("creds").dataset.mode === "file"`, 20000);
      await b.evaluate(`document.querySelector('#consumer-pick button[data-consumer="consumer-a"]').click()`);
      await b.waitFor(`${text("#viewer")}.startsWith("consumer-a")`);
      await openFixture(b, "task-a");
      check("pagehide round " + r + ": task-a shown before leaving", await unityShows(b, { task: "task-a" }));
      await pagehideCheck(b, "pagehide round " + r, way);
    }
    break full;
  }
  log("commit " + commit);
  log("unity build " + unityBuild + (existsSync(join(unityBuild, "WebGL.loader.js")) ? "" : " (MISSING)"));
  const state = join(tmp, "state");
  demo = startDemo(state, join(repo, "testdata/demo/fake-plan.json"), unityBuild);
  const addr = await demo.ready("task-b");
  let tok = demo.tokens();
  allTokens.push(tok.a, tok.b, tok.host, tok.op);
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
  check("Unity shows task-a #3 waiting Ask " + ask.reason, await unityShows(b, { task: "task-a", seq: 3, phase: "waiting", verdict: "Ask", reason: ask.reason }), await b.evaluate(text("#unity-status")));
  await b.evaluate(`document.querySelector('#timeline li[data-seq="3"] .event-head').click()`);
  const json = await b.evaluate(`document.querySelector('#timeline li[data-seq="3"] .event-json')?.textContent || ""`);
  check("event JSON is the API whitelist, labelled as not raw output", json.includes("intent_hash") && json.includes("不是 CLI 原始輸出"));
  check("nothing says paid or delivered", !/付款成功|已付款|取貨成功|Settled|RESOURCE_DELIVERED/.test(await b.evaluate(`document.body.innerText`)));
  await shot(b, "desktop-task-a-ask", 1280, 900, false);
  log("  task-a seq 3 Ask (consumer-a) shown in the desktop screenshot");

  // R3: an opened event stays open through polls.
  await b.evaluate(`document.querySelector('#timeline li[data-seq="2"] .event-head').click()`);
  check("R3: event #2 opened", await b.waitFor(visible('#timeline li[data-seq="2"] .event-json')));
  check("R3: two polls completed", await waitPolls(b, 2));
  check("R3: event #2 still open (visible) after two polls", await b.evaluate(visible('#timeline li[data-seq="2"] .event-json')));

  // R2: a replay that starts right after a poll (its timer pending) and
  // spans task-a's approval deadline, so the backend's expiry adds event #4
  // during the replay. The replay keeps its mode; the restore shows #4.
  // B1-06 3.5: the order is made explicit, not left to timing. The replay
  // starts 4–8 s before the deadline (the server's clock), with task-a at
  // #3; from replay step 2 on, Unity's reports are held by a test-only page
  // hook, so the steps wait (6 s each at most) and the replay is still on
  // when the expiry worker commits #4. Each readiness condition fails the
  // check when it does not hold; nothing is skipped or slept away.
  const taskA = async () => (await hostGet(addr, tok.host, "/api/tasks/task-a")).json;
  const pa = (await taskA()).purchases[0];
  let nowA = (await taskA()).served_at;
  while (nowA < pa.deadline_at - 8) { await sleep(250); nowA = (await taskA()).served_at; }
  const ready = nowA <= pa.deadline_at - 4 && (await hostEvents(addr, tok.host, "task-a")) === 3;
  check("R2 order: before the replay, task-a is at #3 and its deadline is 4–8 s ahead (server time)", ready, "served_at " + nowA + ", deadline " + pa.deadline_at);
  await b.evaluate(`(() => { const orig = window.apaUnityReport; window.__heldReports = []; window.__holdFrom = Infinity;
    window.apaUnityReport = (kind, gen, seq, detail) => { if (kind === "rendered" && seq >= window.__holdFrom) { window.__heldReports.push([kind, gen, seq, detail]); return; } return orig(kind, gen, seq, detail); };
    window.__releaseReports = () => { window.__holdFrom = Infinity; window.apaUnityReport = orig; window.__heldReports.splice(0).forEach((a) => orig(...a)); return true; };
    return true; })()`);
  await waitPolls(b, 1); // a poll just completed: its next timer is pending
  const g0A = await unityGen(b);
  await b.evaluate(`(window.__holdFrom = 2, document.getElementById("replay").click(), true)`);
  const inReplay = await b.waitFor(`document.getElementById("status").dataset.mode === "replay"`, 5000);
  const seqAtStart = await hostEvents(addr, tok.host, "task-a");
  const replayAt = (await taskA()).served_at;
  check("R2 order: the page is in replay while task-a is still #3 (no expiry committed yet)", inReplay && seqAtStart === 3 && replayAt < pa.deadline_at, "seq " + seqAtStart + " at " + replayAt + ", deadline " + pa.deadline_at);
  check("R2: Unity reports a replay step in a new generation", await b.waitFor(`Number(${uKey("gen")}) > ${g0A}`, 20000));
  const replayGenA = await unityGen(b);
  let replaySamples = [];
  let seqDuring = seqAtStart, modeAtCommit = "", commitAt = 0;
  for (let i = 0; i < 80 && seqDuring === seqAtStart; i++) {
    await sleep(250);
    replaySamples.push(await b.evaluate(`document.getElementById("status").dataset.mode + "|" + document.getElementById("status").textContent`));
    seqDuring = await hostEvents(addr, tok.host, "task-a");
    if (seqDuring !== seqAtStart) { modeAtCommit = await b.evaluate(`document.getElementById("status").dataset.mode`); commitAt = (await taskA()).served_at; }
  }
  check("R2 order: the backend commits #4 while the page is still in replay", seqDuring === 4 && modeAtCommit === "replay", "seq " + seqDuring + " seen at " + commitAt + " in mode " + modeAtCommit);
  log("  replay order (server time): replay entered at " + replayAt + " with task-a #" + seqAtStart + "; deadline " + pa.deadline_at + "; #" + seqDuring + " (APPROVAL_EXPIRED) committed, seen at " + commitAt + " in mode " + modeAtCommit + "; " + replaySamples.length + " samples during the replay");
  check("R2: during the replay nothing switches back to 已同步", replaySamples.length > 0 && replaySamples.every((x) => x.startsWith("replay|") && !x.includes("已同步")), replaySamples[replaySamples.length - 1]);
  check("R2: replay note says the panels are not the shop's moment", await b.evaluate(visible("#replay-note")));
  await b.evaluate(`window.__releaseReports()`);
  check("R2: replay ends and polling resumes", await b.waitFor(`document.getElementById("status").dataset.mode === "live" && document.getElementById("status").dataset.kind === "ok"`, 40000));
  const seqAfter = await hostEvents(addr, tok.host, "task-a");
  check("R2: the backend added an event during the replay", seqAtStart === 3 && seqAfter === 4, "as_of_sequence " + seqAtStart + " -> " + seqAfter);
  check("R2: restore shows the latest snapshot in Unity (new generation, task-a #4 expired, Ask " + ask.reason + ")",
    await unityShows(b, { task: "task-a", seq: seqAfter, phase: "expired", verdict: "Ask", reason: ask.reason, afterGen: replayGenA }), await b.evaluate(text("#unity-status")));
  check("R2: restore shows the latest snapshot in the Dashboard", await b.waitFor(`document.getElementById("scene-text").dataset.phase === "expired" && [...document.querySelectorAll("#timeline li")].length === 4`));

  // R3: the new event did not close the opened ones; a closed one stays closed.
  check("R3: event #2 still open after the new event #4", await b.evaluate(visible('#timeline li[data-seq="2"] .event-json')));
  await b.evaluate(`document.querySelector('#timeline li[data-seq="2"] .event-head').click()`);
  await waitPolls(b, 2);
  check("R3: a closed event is not reopened by later polls", !(await b.evaluate(visible('#timeline li[data-seq="2"] .event-json'))) && (await b.evaluate(visible('#timeline li[data-seq="3"] .event-json'))));

  // A cannot read B's task. Switching task clears what Unity showed of task-a.
  check("before the task switch Unity shows task-a #4", await unityShows(b, { task: "task-a", seq: 4, phase: "expired" }));
  const genTaskA = await unityGen(b);
  const atTaskSwitch = await b.evaluate(`(() => { document.getElementById("task-id").value = "task-b"; document.querySelector("#task-form button[type=submit]").click();
    return document.getElementById("unity-status").dataset.state + "|" + ${unityCleared(["task-a", "HUMAN_APPROVAL_REQUIRED", "expired"])}; })()`);
  check("task switch: at once Unity says 同步中 and holds nothing of task-a", atTaskSwitch === "syncing|true", atTaskSwitch);
  check("viewer A on task-b: same not-found answer", await b.waitFor(`${text("#status")}.includes("找不到這個 task，或你沒有查看權限")`));
  check("viewer A on task-b: nothing of B shown", !(await b.evaluate(`document.body.innerText`)).includes("wallet-b"));
  check("task switch: Unity waits for a task, nothing of task-a", await b.evaluate(unityCleared(["task-a", "HUMAN_APPROVAL_REQUIRED", "expired"])) && (await unityText(b)).startsWith("ready|") && (await unityText(b)).includes("等待選擇 task"), await unityText(b));
  await b.evaluate(lateReport(genTaskA, 4, "expired|task-a|Ask|HUMAN_APPROVAL_REQUIRED"));
  check("task switch: a late report of task-a's generation does not bring it back", await b.evaluate(unityCleared(["task-a", "HUMAN_APPROVAL_REQUIRED", "expired"])), await unityText(b));
  await openFixture(b, "task-a");
  check("R3: opening another task cleared the opened events", (await b.evaluate(`[...document.querySelectorAll("#timeline .event-json")].filter(e => !e.hidden).length`)) === 0);

  // Switch to B: everything of A goes at once, the shop's status too.
  check("before the consumer switch Unity shows task-a #4", await unityShows(b, { task: "task-a", seq: 4, phase: "expired" }));
  const genA = await unityGen(b);
  await b.evaluate(`document.getElementById("switch").click()`);
  check("switch clears viewer, Inspector and Timeline", await b.waitFor(`${text("#viewer")} === "未驗證" && ${text("#inspector")} === "尚無資料" && ${text("#timeline")} === "尚無事件"`));
  check("consumer switch: Unity waits for a task, nothing of task-a", await b.evaluate(unityCleared(["task-a", "HUMAN_APPROVAL_REQUIRED", "expired"])) && (await unityText(b)).startsWith("ready|") && (await unityText(b)).includes("等待選擇 task"), await unityText(b));
  await b.evaluate(lateReport(genA, 4, "expired|task-a|Ask|HUMAN_APPROVAL_REQUIRED"));
  check("consumer switch: a late report of A's generation does not bring task-a back", await b.evaluate(unityCleared(["task-a", "HUMAN_APPROVAL_REQUIRED", "expired"])), await unityText(b));
  await signIn(b, tok.b);
  await b.waitFor(`${text("#viewer")}.startsWith("consumer-b")`);
  await b.evaluate(`[...document.querySelectorAll("#task-pick button")].find(x => x.textContent.startsWith("task-b（"))?.click()`);
  await b.waitFor(`document.getElementById("status").dataset.kind === "ok"`);
  const deny = expect("deny-budget");
  const insB = await b.evaluate(text("#inspector"));
  check("Inspector: Deny " + deny.reason + ", wallet-b", insB.includes("Deny") && insB.includes(deny.reason) && insB.includes("wallet-b") && !insB.includes("wallet-a"));
  check("Unity shows task-b #3 denied Deny " + deny.reason, await unityShows(b, { task: "task-b", seq: 3, phase: "denied", verdict: "Deny", reason: deny.reason }), await b.evaluate(text("#unity-status")));
  await shot(b, "narrow-task-b-deny", 390, 844, true);
  log("  task-b seq 3 Deny (consumer-b) shown in the narrow screenshot");
  await b.send("Emulation.setDeviceMetricsOverride", { width: 1280, height: 900, deviceScaleFactor: 1, mobile: false });

  // R2: a Deny replay restores the reason; the replay writes nothing.
  const eventsBefore = (await hostGet(addr, tok.host, "/api/tasks/task-b/events")).text;
  const replayGenB = await replayAndRestore(b, "R2 Deny");
  check("R2: restore shows task-b #3 denied Deny " + deny.reason + " (new generation)",
    await unityShows(b, { task: "task-b", seq: 3, phase: "denied", verdict: "Deny", reason: deny.reason, afterGen: replayGenB }), await b.evaluate(text("#unity-status")));
  check("R2: replay wrote nothing (task-b events unchanged)", (await hostGet(addr, tok.host, "/api/tasks/task-b/events")).text === eventsBefore);

  // R2: a replay interrupted by a switch of viewer leaves nothing behind.
  const gR0 = await unityGen(b);
  await b.evaluate(`document.getElementById("replay").click()`);
  await b.waitFor(`document.getElementById("status").dataset.mode === "replay"`);
  check("R2: a replay step of task-b is shown before the switch", await b.waitFor(`Number(${uKey("gen")}) > ${gR0} && ${uKey("task")} === "task-b"`, 20000));
  const genReplay = await unityGen(b);
  await b.evaluate(`document.getElementById("switch").click()`);
  await sleep(4000); // an observation window longer than a replay step (at most 6 s plus 0.4 s; steps here take about 1 s)
  check("R2: after a switch mid-replay: live, no replay note, signed out", (await b.evaluate(`document.getElementById("status").dataset.mode === "live" && !${visible("#replay-note")} && ${text("#viewer")} === "未驗證"`)));
  check("R2: after the switch the Unity status holds nothing of task-b (no late replay step)", await b.evaluate(unityCleared(["task-b", "INSUFFICIENT_BUDGET", "Deny", "denied"])) && (await unityText(b)).startsWith("ready|"), await unityText(b));
  await b.evaluate(lateReport(genReplay, 3, "denied|task-b|Deny|INSUFFICIENT_BUDGET"));
  check("R2: a late replay report of the old generation does not restore task-b", await b.evaluate(unityCleared(["task-b", "INSUFFICIENT_BUDGET", "Deny", "denied"])), await unityText(b));

  // R2: replay started twice, and visibility changes: one polling loop.
  await signIn(b, tok.a);
  await b.waitFor(`${text("#viewer")}.startsWith("consumer-a")`);
  await openFixture(b, "task-a");
  await b.evaluate(`document.getElementById("replay").click()`);
  await sleep(300);
  await b.evaluate(`document.getElementById("replay").click()`);
  check("R2: a second replay replaces the first and restores", await b.waitFor(`document.getElementById("status").dataset.mode === "live" && document.getElementById("status").dataset.kind === "ok"`, 40000)
    && await unityShows(b, { task: "task-a", seq: 4, phase: "expired" }));
  for (let i = 0; i < 3; i++) await b.evaluate(`(() => { Object.defineProperty(document, "hidden", { configurable: true, get: () => true }); document.dispatchEvent(new Event("visibilitychange")); delete document.hidden; document.dispatchEvent(new Event("visibilitychange")); return true; })()`);
  const p0 = await pollCount(b);
  await sleep(5000);
  const rate = (await pollCount(b)) - p0;
  check("R2: one polling loop (about one poll a second over 5 s)", rate >= 3 && rate <= 7, rate + " polls");

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

  await signIn(b, tok.a);
  await b.waitFor(`${text("#viewer")}.startsWith("consumer-a")`);
  await openFixture(b, "task-a");

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
  allTokens.push(tok.a, tok.b, tok.host, tok.op);
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

  mark("delivery 2 R1 seeded two purchases");
  // R1: a task with two purchases, written through the real Store
  // (internal/store TestSeedDisplayDB): purchase 1 Ask then expired,
  // purchase 2 an open Ask. The scene follows purchase 2.
  const seeded = join(tmp, "state-seeded");
  mkdirSync(seeded, { recursive: true, mode: 0o700 });
  execFileSync("go", ["test", "./internal/store", "-run", "TestSeedDisplayDB", "-count=1"], { cwd: repo, env: { ...process.env, APA_SEED_DB: join(seeded, "demo.db") }, stdio: "ignore" });
  const sd = startDemo(seeded, null, unityBuild);
  const addr4 = await sd.ready();
  const st = sd.tokens();
  allTokens.push(st.a, st.b, st.host, st.op);
  await b.send("Page.navigate", { url: "http://" + addr4 + "/" });
  await b.waitFor(`document.readyState === "complete"`);
  await b.waitFor(`["ready","rendered"].includes(document.getElementById("unity-status").dataset.state)`, 90000);
  await signIn(b, st.a);
  await b.waitFor(`${text("#viewer")}.startsWith("consumer-a")`);
  await openFixture(b, "task-a");
  const seededSeq = await hostEvents(addr4, st.host, "task-a");
  const insS = await b.evaluate(text("#inspector"));
  check("R1: Inspector lists both purchases (one expired, one pending)", (insS.match(/— 請購/g) || []).length === 2 && insS.includes("expired") && insS.includes("pending"));
  check("R1: Dashboard scene follows the newer Ask (waiting)", (await b.evaluate(`document.getElementById("scene-text").dataset.phase`)) === "waiting");
  check("R1: Unity shows task-a #" + seededSeq + " waiting Ask, not the old expiry",
    await unityShows(b, { task: "task-a", seq: seededSeq, phase: "waiting", verdict: "Ask", reason: ask.reason }), await b.evaluate(text("#unity-status")));
  await shot(b, "desktop-r1-two-purchases", 1280, 900, false);
  log("  task-a seq " + seededSeq + " (two purchases; the newer an open Ask) shown in desktop-r1-two-purchases.png");
  // ---- B1-06 D1 (docs/demo-presentation-design.md 4.4, 4.5, 6.2, 7, 8 D1) on the seeded two-purchase task ----
  mark('B1-06 D1');
  // task-a: #1 TASK_STARTED, #2 po-A proposed, #3 po-A Ask, #4 po-A expired, #5 po-B proposed, #6 po-B Ask (open).
  // Assertions read what a viewer sees (innerText of shown elements).
  {
    const shownText = (sel) => `((document.querySelector(${JSON.stringify(sel)}) || {}).innerText || "")`;
    const pv = (await hostGet(addr4, st.host, "/api/tasks/task-a")).json.purchases;
    const poA = pv.find((p) => p.approval_closed === "expired").purchase_order_id, poB = pv.find((p) => !p.approval_closed).purchase_order_id;
    const lastSeq = await hostEvents(addr4, st.host, "task-a");
    await b.evaluate(`(window.scrollTo(0, 0), true)`);
    const inView = (sel) => `(() => { const r = document.querySelector(${JSON.stringify(sel)}).getBoundingClientRect(); return r.top >= 0 && r.bottom <= innerHeight && r.width > 0; })()`;
    check("D1: on / at 1280×900 the Unity shop is wholly in the first viewport", await b.evaluate(inView("#unity-canvas")));
    // Default: the card follows the scene's purchase; every panel names the same one.
    await b.evaluate(`(document.getElementById("evidence-box").open = true, true)`);
    const api = pv.find((p) => p.purchase_order_id === poB);
    check("D1 default: the card shows the scene's purchase (po-B, the open Ask)", await b.waitFor(`document.getElementById("card").dataset.po === ${JSON.stringify(poB)} && ${shownText(".card-status")} === "Ask：等待人工核准（M1 無核准能力）"`, 10000), await b.evaluate(shownText("#card")));
    check("D1 default: Inspector, evidence and the card name the same purchase and intent hash",
      (await b.evaluate(shownText("#inspector"))).includes(poB) && (await b.evaluate(shownText('.evidence-page[data-page="請購"]'))).includes(api.intent_hash)
      && (await b.evaluate(shownText('.evidence-page[data-page="請購"]'))).includes(api.payment_requirement_hash));
    check("D1 default: Unity's report matches the projection sent; the purchase link says it is the page's own",
      await b.waitFor(`${shownText('.evidence-page[data-page="場景對應"]')}.includes("Unity 已呈現 task-a #${lastSeq}") && ${shownText('.evidence-page[data-page="場景對應"]')}.includes(${JSON.stringify(poB + "（網頁推導）")})`, 15000),
      await b.evaluate(shownText('.evidence-page[data-page="場景對應"]')));
    check("D1 default: the bound amount is its own row, apart from any catalog price", (await b.evaluate(shownText("#card"))).includes("402 綁定金額") && (await b.evaluate(shownText("#card"))).includes("目錄標示"));
    // By hand: the older purchase; the scene stays, and that is no sync failure.
    const unityBefore = await b.evaluate(`document.getElementById("unity-status").dataset.seq + "|" + document.getElementById("unity-status").dataset.phase`);
    await b.evaluate(`(() => { const s = document.querySelector("#card .po-pick"); s.value = ${JSON.stringify(poA)}; s.dispatchEvent(new Event("change")); return true; })()`);
    check("D1 by hand: the card shows po-A and says the scene shows po-B",
      await b.waitFor(`${shownText(".card-differs")}.includes(${JSON.stringify("你正在查看 " + poA + "；商店場景呈現的是 " + poB)}) && ${shownText(".card-status")} === "Ask 已到期（後端已記錄）"`, 5000), await b.evaluate(shownText("#card")));
    check("D1 by hand: Unity unchanged, no 尚未一致, and the evidence says it is a different pick, not a sync failure",
      (await b.evaluate(`document.getElementById("unity-status").dataset.seq + "|" + document.getElementById("unity-status").dataset.phase`)) === unityBefore
      && !(await b.evaluate(shownText('.evidence-page[data-page="場景對應"]'))).includes("尚未一致") && (await b.evaluate(shownText('.evidence-page[data-page="場景對應"]'))).includes("選取不同，不是同步失敗"));
    await b.evaluate(`document.querySelector("#card .card-back").click()`);
    check("D1 by hand: 回到場景這筆 returns to the scene's purchase", await b.waitFor(`document.getElementById("card").dataset.po === ${JSON.stringify(poB)} && !document.querySelector(".card-differs")`, 5000));
    // Replay, held step by step (Unity's reports are held by a test-only page hook, so each step lasts its 6 s limit).
    await b.evaluate(`(() => { window.__d1Orig = window.__d1Orig || window.apaUnityReport; window.__d1Held = []; window.apaUnityReport = (kind, gen, seq, detail) => { if (kind === "rendered") { window.__d1Held.push([kind, gen, seq, detail]); return; } return window.__d1Orig(kind, gen, seq, detail); }; return true; })()`);
    const releaseD1 = () => b.evaluate(`(() => { window.apaUnityReport = window.__d1Orig; window.__d1Held.splice(0).forEach((a) => window.__d1Orig(...a)); return true; })()`);
    await b.evaluate(`(() => { window.__d1Writes = 0; if (!window.__d1Fetch) { window.__d1Fetch = window.fetch.bind(window); window.fetch = (u, o) => { if (o && o.method && o.method !== "GET") window.__d1Writes++; return window.__d1Fetch(u, o); }; } return true; })()`);
    await b.evaluate(`document.getElementById("replay").click()`);
    const at = (n) => b.waitFor(`${shownText(".card-replay")} === ${JSON.stringify("回放當步 #" + n + "／共 " + lastSeq + "（唯讀）")}`, 15000);
    const fullNote = (n) => "完整紀錄，非回放當步（回放位置 #" + n + "／共 " + lastSeq + "）";
    const labelled = async (n) => (await b.evaluate(shownText("#inspector-note"))) === fullNote(n) && (await b.evaluate(shownText("#timeline .full-note"))) === fullNote(n)
      && (await b.evaluate(shownText(".evidence-banner"))) === fullNote(n) && (await b.evaluate(`document.querySelectorAll('#timeline li[data-ahead="1"]').length`)) === lastSeq - n
      && (await b.evaluate(shownText("#mode-chip"))) === "回放中 #" + n + "／共 " + lastSeq + "（唯讀）" && !(await b.evaluate(`!!document.querySelector("#card .po-pick")`));
    check("D1 replay #1: no purchase yet; the full record says it is not the step", await at(1) && (await b.evaluate(shownText(".card-status"))) === "此 task 尚無請購" && await labelled(1), await b.evaluate(shownText("#card")));
    check("D1 replay #2: po-A and its fixed fields only: no Ask, reason or expiry", await at(2) && (await b.evaluate(`document.getElementById("card").dataset.po`)) === poA
      && (await b.evaluate(shownText(".card-status"))) === "已提出請購，尚無裁決" && !/Ask|HUMAN_APPROVAL|到期|期限/.test(await b.evaluate(shownText("#card"))) && await labelled(2), await b.evaluate(shownText("#card")));
    check("D1 replay #3: Ask waiting, though the record says expired and the browser's clock is long past the deadline", await at(3)
      && (await b.evaluate(shownText(".card-status"))) === "Ask：等待人工核准（M1 無核准能力）" && !/已到期|已達期限/.test(await b.evaluate(shownText("#card")))
      && (await b.evaluate(shownText("#card"))).includes("依已回放事件推導") && await labelled(3), await b.evaluate(shownText("#card")));
    check("D1 replay #4: only now expired; po-B is not in the step", await at(4) && (await b.evaluate(shownText(".card-status"))) === "Ask 已到期（後端已記錄）"
      && !(await b.evaluate(shownText("#card"))).includes(poB) && await labelled(4), await b.evaluate(shownText("#card")));
    check("D1 replay #6: po-B's Ask", await at(6) && (await b.evaluate(`document.getElementById("card").dataset.po`)) === poB && await labelled(6), await b.evaluate(shownText("#card")));
    await releaseD1();
    check("D1 replay end: back to live, the latest snapshot, no replay labels", await b.waitFor(`document.getElementById("status").dataset.mode === "live" && document.getElementById("status").dataset.kind === "ok" && document.getElementById("card").dataset.mode === "live"`, 40000)
      && (await b.evaluate(`document.getElementById("card").dataset.po`)) === poB && (await b.evaluate(`document.getElementById("inspector-note").hidden && !document.querySelector("#timeline .full-note")`)));
    check("D1 replay: nothing written, no request other than reads", (await hostEvents(addr4, st.host, "task-a")) === lastSeq && (await b.evaluate(`window.__d1Writes`)) === 0);
    // A switch mid-replay: the old replay's reports, released late, restore no card.
    await b.evaluate(`(() => { window.__d1Held = []; window.apaUnityReport = (kind, gen, seq, detail) => { if (kind === "rendered") { window.__d1Held.push([kind, gen, seq, detail]); return; } return window.__d1Orig(kind, gen, seq, detail); }; return true; })()`);
    await b.evaluate(`document.getElementById("replay").click()`);
    await at(2);
    await b.evaluate(`document.getElementById("switch").click()`);
    await releaseD1();
    await sleep(1500);
    check("D1 replay switched away: the late reports bring back no card, no replay", (await b.evaluate(shownText("#card"))) === "尚未選擇 task" && (await b.evaluate(shownText("#mode-chip"))) === "即時"
      && (await b.evaluate(`document.getElementById("replay-note").hidden`)), await b.evaluate(shownText("#card")));
    // Everything folded: the status bar still says fake, unpaid and what is not built.
    await b.evaluate(`(document.querySelectorAll("details").forEach((d) => { d.open = false; }), true)`);
    const bar = await b.evaluate(shownText("header.top"));
    check("D1: with every section folded the status bar still says fake, no payment and what is not built",
      ["M1／fake agent／不付款", "真實 Agent 未啟用", "付款 未實作", "憑證／ZK 未實作", "B1-07 獨立驗證 未執行"].every((w) => bar.includes(w)) && !/付款成功|已驗證|通過/.test(bar), bar.slice(0, 200));
    // Narrow: the shop, then the card; no sideways scroll.
    await b.send("Emulation.setDeviceMetricsOverride", { width: 390, height: 844, deviceScaleFactor: 1, mobile: true });
    await sleep(800);
    check("D1 narrow (390×844): shop above the card, no horizontal scroll", await b.evaluate(`(() => { const s = document.querySelector(".shop").getBoundingClientRect(), c = document.querySelector("aside.card").getBoundingClientRect(); return s.top < c.top && document.documentElement.scrollWidth <= innerWidth; })()`));
    await b.send("Emulation.setDeviceMetricsOverride", { width: 1280, height: 900, deviceScaleFactor: 1, mobile: false });
  }
  await sd.stop();

  // ---- B1-06 D2: scene transitions (docs/demo-presentation-design.md 16.9) ----
  // Unity's own moves and labels are its PlayMode tests; here the page's
  // words, the data-first order and Unity's reports in a real browser. A
  // recorder in the page keeps every scene-text change and Unity report with
  // the page's clock, so "no move" and "a walk" are told apart by when Unity
  // reported, not by a fixed wait.
  mark('B1-06 D2');
  {
    const shownText = (sel) => `((document.querySelector(${JSON.stringify(sel)}) || {}).innerText || "")`;
    const FIRST = "首次載入為保存的快照，未觀測進店過程";
    const record = `(() => { window.__d2 = []; const s = document.getElementById("scene-text");
      if (!window.__d2Obs) { window.__d2Obs = new MutationObserver(() => window.__d2.push({ t: performance.now(), motion: s.dataset.motion || "", text: s.innerText }));
        window.__d2Obs.observe(s, { childList: true, subtree: true, characterData: true, attributes: true }); }
      if (!window.__d2Orig) { window.__d2Orig = window.apaUnityReport;
        window.apaUnityReport = (kind, gen, seq, detail) => { window.__d2.push({ t: performance.now(), kind: String(kind), gen: Number(gen), seq: Number(seq), detail: String(detail) }); return window.__d2Orig(kind, gen, seq, detail); }; }
      return true; })()`;
    const recorded = async () => JSON.parse(await b.evaluate(`JSON.stringify(window.__d2)`));
    const snapNow = async (name) => { // the viewport as it is, at once (shot() waits 1.5 s)
      const png = await b.send("Page.captureScreenshot", { format: "png", captureBeyondViewport: false });
      writeFileSync(join(out, name + ".png"), Buffer.from(png.data, "base64"));
      log("  screenshot " + name + ".png (at once): " + JSON.stringify(await b.evaluate(`[document.getElementById("unity-status").textContent, document.getElementById("scene-text").innerText]`)));
    };
    const sceneEvidence = () => b.evaluate(shownText('.evidence-page[data-page="場景對應"]'));

    // Store-built: purchase 1 Ask, purchase 2 Ask, then purchase 1 expires
    // (internal/store TestSeedDisplayDB, APA_SEED_ORDER=ask_ask_then_expire).
    const seeded2 = join(tmp, "state-seeded-d2");
    mkdirSync(seeded2, { recursive: true, mode: 0o700 });
    execFileSync("go", ["test", "./internal/store", "-run", "TestSeedDisplayDB", "-count=1"], { cwd: repo, env: { ...process.env, APA_SEED_DB: join(seeded2, "demo.db"), APA_SEED_ORDER: "ask_ask_then_expire" }, stdio: "ignore" });
    const s2 = startDemo(seeded2, null, unityBuild);
    const addr5 = await s2.ready();
    const t2 = s2.tokens();
    allTokens.push(t2.a, t2.b, t2.host, t2.op);
    await b.send("Page.navigate", { url: "http://" + addr5 + "/" });
    await b.waitFor(`document.readyState === "complete"`);
    await b.waitFor(`["ready","rendered"].includes(document.getElementById("unity-status").dataset.state)`, 90000);
    await b.evaluate(record);
    await signIn(b, t2.a);
    await b.waitFor(`${text("#viewer")}.startsWith("consumer-a")`);
    const events2 = (await hostGet(addr5, t2.host, "/api/tasks/task-a/events")).json.events.map((e) => e.sequence + " " + e.event_type);
    check("D2 seeded: the Store already has all six events before the task is opened (#3 first: the decided Ask is stored)",
      events2.join(",") === "1 TASK_STARTED,2 PURCHASE_PROPOSED,3 POLICY_DECIDED,4 PURCHASE_PROPOSED,5 POLICY_DECIDED,6 APPROVAL_EXPIRED", events2.join(","));
    await b.evaluate(`(window.__d2 = [], document.getElementById("evidence-box").open = true, true)`);
    await openFixture(b, "task-a");
    check("D2 item 1: a first snapshot shows Unity at the counter (task-a #6 waiting)", await unityShows(b, { task: "task-a", seq: 6, phase: "waiting" }, 15000));
    {
      const r = await recorded();
      const firstAt = (r.find((x) => x.text !== undefined && x.text.includes(FIRST)) || {}).t, renderedAt = (r.find((x) => x.kind === "rendered" && x.seq === 6) || {}).t;
      check("D2 item 1: no move: Unity reported #6 within 800 ms of the first-load text", firstAt !== undefined && renderedAt !== undefined && renderedAt - firstAt < 800, "first-load text " + firstAt + " ms, rendered " + renderedAt + " ms");
      check("D2 item 1: the scene text says it is a stored snapshot, not an observed walk-in",
        (await b.evaluate(`document.getElementById("scene-text").dataset.motion`)) === "first" && (await b.evaluate(shownText("#scene-text"))).includes(FIRST));
      check("D2 item 1: the evidence's scene page says so too, derived by the page",
        await b.waitFor(`${shownText('.evidence-page[data-page="場景對應"]')}.includes(${JSON.stringify(FIRST + "（不播放過場）（規則 1）")})`, 5000), (await sceneEvidence()).slice(0, 300));
    }
    // Replay: one step per stored event through the page's own projection.
    await b.evaluate(`(window.__d2 = [], true)`);
    await b.evaluate(`document.getElementById("replay").click()`);
    const rule2 = "Unity 補間中：新的一筆請購：展示動畫：選商品 → 到櫃檯（沒有獨立事件）；裁決已在 #5 記錄";
    check("D2 item 3 (replay #5): a new purchase is said to browse again, while it walks",
      await b.waitFor(`${shownText("#scene-text")}.includes(${JSON.stringify(rule2)}) && document.getElementById("scene-text").dataset.motion === "moving"`, 40000), await b.evaluate(shownText("#scene-text")));
    check("D2 item 3 (replay #5): the evidence names this move and where it comes from",
      (await sceneEvidence()).includes("新的一筆請購：展示動畫：選商品 → 到櫃檯（沒有獨立事件）；裁決已在 #5 記錄（規則 2）") && (await sceneEvidence()).includes("網頁依轉換表推導"), (await sceneEvidence()).slice(0, 400));
    check("D2 replay ends and the latest snapshot is back", await b.waitFor(`document.getElementById("status").dataset.mode === "live" && document.getElementById("status").dataset.kind === "ok"`, 60000));
    {
      const r = await recorded();
      const gen = Math.max(...r.filter((x) => x.kind === "rendered" && x.seq === 1).map((x) => x.gen));
      const sentAt = (n) => (r.find((x) => x.text !== undefined && x.text.startsWith("回放 #" + n + "：")) || {}).t;
      const doneAt = (n) => (r.find((x) => x.kind === "rendered" && x.gen === gen && x.seq === n) || {}).t;
      const took = Object.fromEntries([1, 2, 3, 4, 5, 6].map((n) => [n, Math.round(doneAt(n) - sentAt(n))]));
      log("  D2 replay: ms from each step's text to Unity's report: " + JSON.stringify(took));
      const texts = (n) => r.filter((x) => x.text !== undefined && x.text.startsWith("回放 #" + n + "：")).map((x) => x.text).join(" / ");
      check("D2 item 8 (replay #2): the browse is called a display animation, with the request's event", texts(2).includes("Unity 補間中：展示動畫：選商品（沒有獨立事件）；請購已在 #2 記錄"), texts(2).slice(0, 200));
      check("D2 item 8 (replay #3): to the counter, display animation, the decision's event", texts(3).includes("Unity 補間中：展示動畫：到櫃檯（沒有獨立事件）；裁決已在 #3 記錄"), texts(3).slice(0, 200));
      check("D2 item 3 (replay #4, #6): another purchase's event: no move, Unity reports at once", took[4] < 600 && took[6] < 600 && !texts(4).includes("補間中") && !texts(6).includes("補間中"), JSON.stringify(took));
      check("D2 item 3 (replay #5): the new purchase walks (Unity reports after a move, within 2.5 s + 700 ms)", took[5] > 1500 && took[5] < 3200, JSON.stringify(took));
      check("D2 item 7: no replay step's move took over 2.5 s (+ 700 ms for a slow frame and the report)", Object.values(took).every((v) => v < 3200), JSON.stringify(took));
    }
    await s2.stop();

    // Live: a task opened while the Store has only #1 (ask-slow pauses 8 s
    // after the task starts), then #2 and #3 arrive together.
    const d2State = join(tmp, "state-d2");
    const d2 = startDemo(d2State, null, unityBuild, ["--fake-cli", join(bin, "fake-claude"), "--scenarios", join(repo, "testdata/demo/scenarios-d2.json"), "--registry", join(repo, "testdata/demo/registry.json")]);
    const addr6 = await d2.ready();
    const t3 = d2.tokens();
    allTokens.push(t3.a, t3.b, t3.host, t3.op);
    await b.send("Page.navigate", { url: "http://" + addr6 + "/" });
    await b.waitFor(`document.readyState === "complete"`);
    await b.waitFor(`["ready","rendered"].includes(document.getElementById("unity-status").dataset.state)`, 90000);
    await b.evaluate(record);
    await signIn(b, t3.a);
    await b.waitFor(`${text("#viewer")}.startsWith("consumer-a")`);
    const run = await hostPost(addr6, t3.op, "/api/host/runs", { request_id: "req-d2-slow-00000001", scenario: "ask-slow" });
    const tid = run.json.task_id;
    check("D2 live: the slow run started (host side)", run.status === 202 && typeof tid === "string", JSON.stringify(run.json));
    let atOpen = 0;
    for (let i = 0; i < 100 && atOpen < 1; i++) { atOpen = (await hostEvents(addr6, t3.host, tid).catch(() => 0)) || 0; if (atOpen < 1) await sleep(100); }
    await b.evaluate(`(window.__d2 = [], document.getElementById("evidence-box").open = true, document.getElementById("task-id").value = ${JSON.stringify(tid)}, document.querySelector("#task-form button[type=submit]").click(), true)`);
    const firstSeq1 = await b.waitFor(`document.getElementById("status").dataset.kind === "ok" && ${text("#status")}.includes("快照序號 1（")`, 5000);
    const storeStill1 = (await hostEvents(addr6, t3.host, tid)) === 1;
    check("D2 item 2: the Store had only #1 when the task was opened and the page's first snapshot is #1 (else this check fails; it is never skipped)",
      atOpen === 1 && firstSeq1 && storeStill1, "Store at open " + atOpen + ", page: " + (await b.evaluate(text("#status"))));
    check("D2 item 2: Unity shows #1 entered, as a first load", await unityShows(b, { task: tid, seq: 1, phase: "entered" }, 10000)
      && (await b.evaluate(`document.getElementById("scene-text").dataset.motion`)) === "first");
    // From here Unity's real "rendered" reports are held at a test-only page
    // hook (with the time they came), so "Unity is still on its way" is a fact
    // while reports that are not the projection sent are fed in.
    await b.evaluate(`(() => { window.__d2 = []; window.__d2Held = []; window.__d2Pass = window.apaUnityReport;
      window.apaUnityReport = (kind, gen, seq, detail) => { if (kind === "rendered") { window.__d2Held.push({ t: performance.now(), args: [kind, gen, seq, detail] }); return; } return window.__d2Pass(kind, gen, seq, detail); };
      return true; })()`);
    check("D2 item 2: data first: the card says Ask while Unity is still on its way from #1",
      await b.waitFor(`${shownText(".card-status")} === "Ask：等待人工核准（M1 無核准能力）" && document.getElementById("scene-text").dataset.motion === "moving" && ${uKey("seq")} === "1"`, 20000),
      await b.evaluate(shownText(".card-status")) + " | " + await b.evaluate(uKey("seq")));
    await snapNow("d2-live-on-the-way");
    // Review of d7070af: another task, another phase, a sequence never sent:
    // each is fed as Unity's report would come; none ends the transition.
    const fed = await b.evaluate(`(() => { const g = Number(document.getElementById("unity-status").dataset.gen), out = [];
      for (const [seq, d] of [[3, "waiting|task-other|Ask|HUMAN_APPROVAL_REQUIRED"], [3, "denied|${tid}|Deny|INSUFFICIENT_BUDGET"], [4, "waiting|${tid}|Ask|HUMAN_APPROVAL_REQUIRED"]]) {
        window.__d2Pass("rendered", g, seq, d); out.push(document.getElementById("scene-text").dataset.motion); }
      return out.join(","); })()`);
    check("D2 (review of d7070af): reports of another task, another phase or a sequence never sent leave the page saying Unity is on its way",
      fed === "moving,moving,moving" && (await b.evaluate(shownText("#scene-text"))).includes("Unity 補間中："), fed);
    check("D2 item 2: Unity's own report of #3 comes (held by the hook)", await b.waitFor(`window.__d2Held.some((h) => h.args[2] === 3)`, 10000));
    const heldAt = await b.evaluate(`window.__d2Held.find((h) => h.args[2] === 3).t`);
    await b.evaluate(`(() => { window.apaUnityReport = window.__d2Pass; window.__d2Held.splice(0).forEach((h) => window.__d2Pass(...h.args)); return true; })()`);
    check("D2 item 2: released, the exact report (#3 waiting of this task) ends the transition",
      await unityShows(b, { task: tid, seq: 3, phase: "waiting" }, 10000) && await b.waitFor(`document.getElementById("scene-text").dataset.motion === "shown"`, 5000));
    {
      const r = await recorded();
      const moving = r.find((x) => x.motion === "moving") || {};
      // Read from the recorder: what the page said while Unity walked, not after a fixed wait.
      check("D2 item 2: while Unity walked, the scene text said browse and counter are a display animation, the decision in #3",
        (moving.text || "").includes("Unity 補間中：展示動畫：選商品 → 到櫃檯（沒有獨立事件）；裁決已在 #3 記錄"), (moving.text || "").slice(0, 200));
      check("D2 item 7: the walk to the counter took over 1 s and at most 2.5 s (+ 700 ms for a slow frame and the report)", heldAt - moving.t > 1000 && heldAt - moving.t < 3200, Math.round(heldAt - moving.t) + " ms");
    }
    await shot(b, "d2-live-at-counter", 1280, 900, false);
    await d2.stop();
  }

  // ---- B1-06 slice 3.1: the host's page /host (docs/demo-operation-design.md 6.3) ----
  mark('B1-06 slice 3.1');
  // A fresh host with the fixed plan (task-a Ask, task-b Deny) on a fixed
  // address, so a restart on the same address makes the page's tokens 401.
  const hostState = join(tmp, "state-host");
  let hd = startDemo(hostState, join(repo, "testdata/demo/fake-plan.json"), unityBuild);
  const hAddr = await hd.ready("task-b");
  let ht = hd.tokens();
  allTokens.push(ht.a, ht.b, ht.host, ht.op);
  const goodFile = join(tmp, "tokens-current.json");
  writeFileSync(goodFile, readFileSync(join(hostState, "tokens.json")));
  const staleFile = join(tmp, "tokens-stale.json"); // a previous start's tokens (the seeded host above)
  writeFileSync(staleFile, readFileSync(join(seeded, "tokens.json")));
  const bigFile = join(tmp, "tokens-big.json");
  writeFileSync(bigFile, JSON.stringify({ note: "x".repeat(17000), tokens: [] }));
  const badFile = join(tmp, "tokens-bad.json");
  writeFileSync(badFile, "{ not json");

  // The viewer page / has no way to load the token file.
  await b.send("Page.navigate", { url: "http://" + hAddr + "/" });
  await b.waitFor(`document.readyState === "complete"`);
  check("3.1: the viewer page / has no token file input", (await b.evaluate(`document.querySelector('input[type=file]') === null && !document.getElementById("token-file")`)));

  await b.send("Page.navigate", { url: "http://" + hAddr + "/host" });
  await b.waitFor(`document.readyState === "complete"`);
  await b.waitFor(`["ready","rendered","failed"].includes(document.getElementById("unity-status").dataset.state)`, 90000);
  const row = (k) => `(document.querySelector('#readiness tr[data-item="${k}"]') || {}).dataset?.state`;
  const rowText = (k) => `(document.querySelector('#readiness tr[data-item="${k}"] td') || {}).textContent || ""`;
  const credsMode = () => b.evaluate(`document.getElementById("creds").dataset.mode`);
  const credsView = () => b.evaluate(`JSON.stringify({ mode: document.getElementById("creds").dataset.mode, consumers: document.getElementById("creds").dataset.consumers, buttons: document.querySelectorAll("#consumer-pick button").length, viewer: document.getElementById("viewer").textContent, text: document.getElementById("creds").textContent })`);
  check("3.1: before loading, readiness is unconfirmed (never ok)", (await b.evaluate(row("就緒狀態"))) === "unconfirmed" && (await b.evaluate(rowText("就緒狀態"))).includes("未確認"));

  async function chooseFile(path) {
    const doc = await b.send("DOM.getDocument", {});
    const q = await b.send("DOM.querySelector", { nodeId: doc.root.nodeId, selector: "#token-file" });
    await b.send("DOM.setFileInputFiles", { nodeId: q.nodeId, files: [path] });
  }
  const noTaskShown = () => b.evaluate(`${text("#inspector")} === "尚無資料" && ${text("#timeline")} === "尚無事件" && ${unityCleared(["task-a", "task-b", "HUMAN_APPROVAL_REQUIRED", "INSUFFICIENT_BUDGET"])}`);

  // R1 (review of 0252ceb): a viewer's /api/viewer JSON that completes late.
  // The page's fetch is wrapped so the body of the next /api/viewer reply is
  // held until released; the real app.js then reads it after a switch.
  const holdJson = `(() => { if (!window.__jsonHold) { window.__jsonHold = { n: 0, held: [] }; const orig = window.fetch.bind(window);
    window.fetch = async (url, opts) => { const res = await orig(url, opts);
      if (String(url) === "/api/viewer" && window.__jsonHold.n > 0) { window.__jsonHold.n--; const body = await res.json();
        return { status: res.status, ok: res.ok, json: () => new Promise((r) => window.__jsonHold.held.push(() => r(body))) }; }
      return res; }; }
    window.__jsonHold.n = 1; return true; })()`;
  const jsonHeld = () => b.waitFor(`window.__jsonHold && window.__jsonHold.held.length === 1`, 10000);
  const releaseJson = async () => { await b.evaluate(`(window.__jsonHold.held.splice(0).forEach((f) => f()), true)`); await sleep(500); };
  const noViewer = () => b.evaluate(`${text("#viewer")} === "未驗證" && document.getElementById("task-form").hidden && document.querySelectorAll("#task-pick button").length === 0`);
  // R2: a token typed but not submitted.
  const typeOnly = async (t) => { await b.evaluate(`document.getElementById("token").focus()`); await b.send("Input.insertText", { text: t }); };
  const tokenFieldEmpty = () => b.evaluate(`document.getElementById("token").value === ""`);

  // Load the current file: the field is emptied, both consumers offered.
  await chooseFile(goodFile);
  check("3.1: the token file loads (host + two viewers confirmed by the server)", await b.waitFor(`document.getElementById("creds").dataset.mode === "file" && document.getElementById("creds").dataset.consumers === "consumer-a,consumer-b"`), await credsView());
  check("3.1: the file field is empty after reading", (await b.evaluate(`document.getElementById("token-file").value`)) === "");
  check("3.1: readiness: Store ok after a completed expiry check", await b.waitFor(`${row("Store")} === "ok"`, 15000), await b.evaluate(rowText("Store")));
  check("3.1: readiness: Unity build ok, fake CLI ok, real CLI off, no payment", (await b.evaluate(`${row("Unity 建置")} === "ok" && ${row("fake CLI")} === "ok" && ${row("真實 CLI")} === "off" && ${row("付款")} === "off"`)));
  check("3.1: readiness: the Unity scene says LimeZu", (await b.evaluate(rowText("Unity 場景"))).includes("LimeZu"));

  // Switch consumers: A's task, then B; nothing of A stays.
  await b.evaluate(`document.querySelector('#consumer-pick button[data-consumer="consumer-a"]').click()`);
  await b.waitFor(`${text("#viewer")}.startsWith("consumer-a")`);
  await openFixture(b, "task-a");
  check("3.1: viewer A from the file shows task-a in Unity", await unityShows(b, { task: "task-a", phase: "waiting" }));
  await shot(b, "desktop-host-ready", 1280, 900, false);
  const afterSwitch = await b.evaluate(`(() => { document.querySelector('#consumer-pick button[data-consumer="consumer-b"]').click();
    return ${text("#inspector")} === "尚無資料" && ${unityCleared(["task-a", "HUMAN_APPROVAL_REQUIRED"])}; })()`);
  check("3.1: switching to B clears A's task and shop at once", afterSwitch);
  await b.waitFor(`${text("#viewer")}.startsWith("consumer-b")`);
  await openFixture(b, "task-b");
  check("3.1: viewer B shows task-b, A's task-a is not readable", await unityShows(b, { task: "task-b", phase: "denied" }) && !(await b.evaluate(`document.body.innerText`)).includes("wallet-a"));
  await b.evaluate(`document.getElementById("switch").click()`);
  check("3.1: 「清除目前 viewer」 keeps the loaded file", await b.waitFor(`${text("#viewer")} === "未驗證" && document.getElementById("creds").dataset.mode === "file"`));
  const keptHost = await b.evaluate(`JSON.stringify({ ls: localStorage.length, ss: sessionStorage.length, cookie: document.cookie, url: location.href })`);
  const htmlHost = await b.evaluate(`document.documentElement.outerHTML`);
  check("3.1: no token in storage, cookie, URL, page or console", !/"ls":[1-9]|"ss":[1-9]|"cookie":"[^"]/.test(keptHost) && [ht.a, ht.b, ht.host].every((t) => !keptHost.includes(t) && !htmlHost.includes(t) && !b.consoleText.join("\n").includes(t)), keptHost);

  // 清除全部憑證.
  await b.evaluate(`document.querySelector('#consumer-pick button[data-consumer="consumer-a"]').click()`);
  await b.waitFor(`${text("#viewer")}.startsWith("consumer-a")`);
  await openFixture(b, "task-a");
  await unityShows(b, { task: "task-a" });
  await b.evaluate(`document.getElementById("clear-all").click()`);
  check("3.1: 清除全部憑證 drops every token and the old picture", (await credsMode()) === "none" && (await b.evaluate(`document.querySelectorAll("#consumer-pick button").length === 0 && ${text("#viewer")} === "未驗證"`)) && (await noTaskShown()) && (await b.evaluate(row("就緒狀態"))) === "unconfirmed", await credsView());

  // Files that are refused keep nothing.
  await chooseFile(bigFile);
  check("3.1: a file over 16 KiB is refused unread", await b.waitFor(`document.getElementById("creds").textContent.includes("過大")`) && (await credsMode()) === "none");
  await chooseFile(badFile);
  check("3.1: a file that is not the token file is refused", await b.waitFor(`document.getElementById("creds").textContent.includes("格式不符")`) && (await credsMode()) === "none");
  await chooseFile(staleFile);
  check("3.1: a previous start's token file is refused (server check)", await b.waitFor(`document.getElementById("creds").textContent.includes("與目前服務不符")`) && (await credsMode()) === "none");

  // Interleavings, with the verification requests really held by the browser.
  let holdNext = 0;
  const held = [];
  const offHold = b.on("Fetch.requestPaused", (p) => {
    if (holdNext > 0) { holdNext--; held.push(p.requestId); } else b.send("Fetch.continueRequest", { requestId: p.requestId }).catch(() => {});
  });
  await b.send("Fetch.enable", { patterns: [{ urlPattern: "*/api/host/status*" }, { urlPattern: "*/api/viewer*" }] });
  const release = async () => { while (held.length) await b.send("Fetch.continueRequest", { requestId: held.shift() }).catch(() => {}); await sleep(800); };
  const holdOne = async (ms = 5000) => { for (const end = Date.now() + ms; Date.now() < end && held.length === 0; ) await sleep(50); return held.length === 1; };

  holdNext = 1;
  await chooseFile(goodFile);
  check("3.1 async: A's verification held", await holdOne());
  await b.evaluate(`document.getElementById("clear-all").click()`);
  await release();
  check("3.1 async: A loading, then 清除全部: A's late reply restores nothing", (await credsMode()) === "none" && (await b.evaluate(`document.querySelectorAll("#consumer-pick button").length === 0`)), await credsView());

  holdNext = 1;
  await chooseFile(staleFile); // A: a previous start's file, its reply will be a 401
  check("3.1 async: stale A's verification held", await holdOne());
  await chooseFile(goodFile); // B
  check("3.1 async: B loads while A is held", await b.waitFor(`document.getElementById("creds").dataset.mode === "file"`));
  await release();
  check("3.1 async: A's late 401 neither clears nor replaces B", (await credsMode()) === "file" && (await b.evaluate(`document.getElementById("creds").dataset.consumers`)) === "consumer-a,consumer-b" && (await b.evaluate(`document.getElementById("creds").textContent.startsWith("已載入")`)), await credsView());

  await b.evaluate(`document.getElementById("clear-all").click()`);
  holdNext = 1;
  await chooseFile(goodFile);
  check("3.1 async: A's verification held before manual mode", await holdOne());
  await signIn(b, ht.b); // manual viewer mode on /host
  await b.waitFor(`${text("#viewer")}.startsWith("consumer-b")`);
  await release();
  check("3.1 async: manual viewer mode, then A's verification completes: only the typed viewer, no file tokens",
    (await credsMode()) === "manual" && (await b.evaluate(`document.querySelectorAll("#consumer-pick button").length === 0 && ${text("#viewer")}.startsWith("consumer-b")`)) && (await b.evaluate(rowText("就緒狀態"))).includes("需要 token 檔"), await credsView());

  // pagehide: a readiness poll in flight, then the page is left.
  await b.evaluate(`document.getElementById("clear-all").click()`);
  await chooseFile(goodFile);
  await b.waitFor(`document.getElementById("creds").dataset.mode === "file"`);
  await b.evaluate(`document.querySelector('#consumer-pick button[data-consumer="consumer-a"]').click()`);
  await b.waitFor(`${text("#viewer")}.startsWith("consumer-a")`);
  await openFixture(b, "task-a");
  await unityShows(b, { task: "task-a" });
  await b.send("Fetch.disable");
  offHold();
  await pagehideCheck(b, "3.1 pagehide");
  await chooseFile(goodFile);
  await b.waitFor(`document.getElementById("creds").dataset.mode === "file"`);
  await b.evaluate(`document.querySelector('#consumer-pick button[data-consumer="consumer-a"]').click()`);
  await b.waitFor(`${text("#viewer")}.startsWith("consumer-a")`);
  await openFixture(b, "task-a");
  await unityShows(b, { task: "task-a" });
  await pagehideCheck(b, "3.1 pagehide (late reply)", "reply");
  await b.evaluate(`document.getElementById("clear-all").click()`);

  // R1 on /host: the old viewer's JSON completes after a switch, a clear, pagehide.
  await chooseFile(goodFile);
  await b.waitFor(`document.getElementById("creds").dataset.mode === "file"`);
  await b.evaluate(holdJson);
  await b.evaluate(`document.querySelector('#consumer-pick button[data-consumer="consumer-a"]').click()`);
  check("3.1-R1: viewer A's JSON held", await jsonHeld());
  await b.evaluate(`document.querySelector('#consumer-pick button[data-consumer="consumer-b"]').click()`);
  await b.waitFor(`${text("#viewer")}.startsWith("consumer-b")`);
  await releaseJson();
  check("3.1-R1: A's late JSON after switching to B: identity and task list stay B's",
    (await b.evaluate(`${text("#viewer")}.startsWith("consumer-b") && [...document.querySelectorAll("#task-pick button")].every((x) => !x.textContent.startsWith("task-a"))`)), await b.evaluate(text("#viewer")));
  await b.evaluate(holdJson);
  await b.evaluate(`document.querySelector('#consumer-pick button[data-consumer="consumer-a"]').click()`);
  await jsonHeld();
  await b.evaluate(`document.getElementById("clear-all").click()`);
  await releaseJson();
  check("3.1-R1: A's late JSON after 清除全部: no identity, no task list", await noViewer(), await b.evaluate(text("#viewer")));
  await chooseFile(goodFile);
  await b.waitFor(`document.getElementById("creds").dataset.mode === "file"`);
  await b.evaluate(holdJson);
  await b.evaluate(`document.querySelector('#consumer-pick button[data-consumer="consumer-a"]').click()`);
  await jsonHeld();
  await b.evaluate(`window.dispatchEvent(new PageTransitionEvent("pagehide", { persisted: true })), true`);
  await releaseJson();
  check("3.1-R1: A's late JSON after pagehide: no identity, no task list", await noViewer(), await b.evaluate(text("#viewer")));

  // R2 on /host: a typed, unsubmitted token goes with 清除全部 and pagehide;
  // a typed token still signs in (manual mode reads the field first).
  await typeOnly(ht.a);
  await b.evaluate(`document.getElementById("clear-all").click()`);
  check("3.1-R2: 清除全部 empties a typed, unsubmitted token", await tokenFieldEmpty());
  await typeOnly(ht.a);
  await b.evaluate(`window.dispatchEvent(new PageTransitionEvent("pagehide", { persisted: true })), true`);
  check("3.1-R2: pagehide empties a typed, unsubmitted token", await tokenFieldEmpty());
  await chooseFile(goodFile);
  await b.waitFor(`document.getElementById("creds").dataset.mode === "file"`);
  await signIn(b, ht.a);
  check("3.1-R2: a typed token still signs in on /host (manual mode after reading the field)",
    await b.waitFor(`${text("#viewer")}.startsWith("consumer-a") && document.getElementById("creds").dataset.mode === "manual"`) && (await tokenFieldEmpty()));
  await b.evaluate(`document.getElementById("clear-all").click()`);

  // Leave for real and come back (bfcache or reload): still nothing held.
  await chooseFile(goodFile);
  await b.waitFor(`document.getElementById("creds").dataset.mode === "file"`);
  await b.send("Page.navigate", { url: "about:blank" });
  // Ready when the page is at about:blank and its history can be read (the
  // renderer may be swapped meanwhile); a fixed pause was not a condition.
  let hist = null;
  for (let i = 0; i < 100 && !hist; i++) {
    hist = await b.send("Page.getNavigationHistory").catch(() => null);
    if (hist && hist.entries[hist.currentIndex].url !== "about:blank") hist = null;
    if (!hist) await sleep(100);
  }
  if (!hist) throw new Error("about:blank never became the current history entry");
  await b.send("Page.navigateToHistoryEntry", { entryId: hist.entries[hist.currentIndex - 1].id });
  await sleep(1500);
  check("3.1: after leaving and returning, no credentials and no identity", (await b.evaluate(`location.pathname`)) === "/host" && (await credsMode()) === "none" && (await b.evaluate(`${text("#viewer")} === "未驗證" && document.querySelectorAll("#consumer-pick button").length === 0`)), await credsView());

  // The host restarts on the same address: every token of the file is now a 401.
  await chooseFile(goodFile);
  await b.waitFor(`document.getElementById("creds").dataset.mode === "file"`);
  await b.evaluate(`document.querySelector('#consumer-pick button[data-consumer="consumer-a"]').click()`);
  await b.waitFor(`${text("#viewer")}.startsWith("consumer-a")`);
  await hd.stop();
  hd = startDemo(hostState, null, unityBuild, [], hAddr);
  await hd.ready();
  ht = hd.tokens();
  allTokens.push(ht.a, ht.b, ht.host, ht.op);
  check("3.1: after a restart the page's 401 clears every credential", await b.waitFor(`document.getElementById("creds").dataset.mode === "none" && document.getElementById("creds").textContent.includes("服務已重啟")`, 20000) && (await noTaskShown()), await credsView());
  await chooseFile(goodFile); // the file as it was before the restart
  check("3.1: the pre-restart file is refused", await b.waitFor(`document.getElementById("creds").textContent.includes("與目前服務不符")`));
  writeFileSync(goodFile, readFileSync(join(hostState, "tokens.json")));
  await chooseFile(goodFile);
  check("3.1: the new file loads", await b.waitFor(`document.getElementById("creds").dataset.mode === "file"`));
  // R1 and R2 on the viewer page /.
  await b.send("Page.navigate", { url: "http://" + hAddr + "/" });
  await b.waitFor(`document.readyState === "complete"`);
  await b.evaluate(holdJson);
  await signIn(b, ht.a);
  check("3.1-R1 /: viewer A's JSON held", await jsonHeld());
  await b.evaluate(`document.getElementById("switch").click()`);
  await releaseJson();
  check("3.1-R1 /: A's late JSON after 清除／切換: no identity, no task list", await noViewer(), await b.evaluate(text("#viewer")));
  await typeOnly(ht.a);
  await b.evaluate(`document.getElementById("switch").click()`);
  check("3.1-R2 /: 清除／切換 empties a typed, unsubmitted token", await tokenFieldEmpty());
  await typeOnly(ht.a);
  await b.evaluate(`window.dispatchEvent(new PageTransitionEvent("pagehide", { persisted: true })), true`);
  check("3.1-R2 /: pagehide empties a typed, unsubmitted token", await tokenFieldEmpty());
  await signIn(b, ht.b);
  check("3.1-R2 /: a typed token still signs in", await b.waitFor(`${text("#viewer")}.startsWith("consumer-b")`));

  await hd.stop();

  // ---- B1-06 slice 3.2: host control (docs/demo-operation-design.md section 13) ----
  mark('B1-06 slice 3.2');
  // A host in scenario mode on a new database: the demo registry, a catalog
  // with ask, deny-budget and hang. Nothing runs at start.
  const hangScenario = join(tmp, "hang.json");
  writeFileSync(hangScenario, JSON.stringify({ steps: [{ do: "tool", tool: "request_purchase", args: { product_id: "kr-market" } }, { do: "hang" }] }));
  const catalog = join(tmp, "scenarios.json");
  writeFileSync(catalog, JSON.stringify({ description: "browser test", scenarios: [
    { id: "ask", consumer_id: "consumer-a", scenario: join(repo, "testdata/cli/ask-mainline.json"), label: "Ask" },
    { id: "deny-budget", consumer_id: "consumer-b", scenario: join(repo, "testdata/cli/ask-mainline.json"), budget_initial: "250000", label: "Deny" },
    { id: "hang", consumer_id: "consumer-a", scenario: hangScenario, label: "Hang" },
  ] }));
  const ctlState = join(tmp, "state-control");
  const ctlArgs = ["--fake-cli", join(bin, "fake-claude"), "--scenarios", catalog, "--registry", join(repo, "testdata/demo/registry.json")];
  let cd = startDemo(ctlState, null, unityBuild, ctlArgs);
  const cAddr = await cd.ready();
  let ct = cd.tokens();
  allTokens.push(ct.a, ct.b, ct.host, ct.op);
  check("3.2: nothing runs at start", !cd.log().includes("demo: run "));
  const ctlFile = join(tmp, "tokens-control.json");
  writeFileSync(ctlFile, readFileSync(join(ctlState, "tokens.json")));
  await b.send("Page.navigate", { url: "http://" + cAddr + "/host" });
  await b.waitFor(`document.readyState === "complete"`);
  await b.waitFor(`["ready","rendered","failed"].includes(document.getElementById("unity-status").dataset.state)`, 90000);
  await chooseFile(ctlFile);
  check("3.2: the token file with the operator loads", await b.waitFor(`document.getElementById("creds").dataset.mode === "file" && document.getElementById("creds").textContent.includes("operator")`));
  check("3.2: the control panel offers the catalog's scenarios", await b.waitFor(`!document.getElementById("control").hidden && document.querySelectorAll("#run-scenarios button").length === 3`, 15000));
  const runsOf = async () => (await hostGet(cAddr, ct.op, "/api/host/runs")).json.runs;
  // clickScenario waits until the scenario's button is enabled (no run active, no start in flight), then clicks it.
  const clickScenario = async (id) => {
    const sel = `document.querySelector('#run-scenarios button[data-scenario="${id}"]')`;
    if (!(await b.waitFor(`!!${sel} && !${sel}.disabled`, 20000))) throw new Error("scenario button " + id + " never enabled");
    return b.evaluate(`${sel}.click()`);
  };
  const runItem = (task) => `([...document.querySelectorAll("#runs li")].find((l) => l.textContent.startsWith(${JSON.stringify(task)}))?.textContent || "")`;

  // Normal flow: start Ask, the page switches to consumer-a and opens the new task.
  await clickScenario("ask");
  check("3.2: start Ask opens show-a-01 for consumer-a", await b.waitFor(`${text("#viewer")}.startsWith("consumer-a") && document.getElementById("status").dataset.kind === "ok"`, 20000)
    && await unityShows(b, { task: "show-a-01", phase: "waiting", verdict: "Ask" }), await b.evaluate(text("#unity-status")));
  check("3.2: the run ends and the list says so without claiming payment", await b.waitFor(`${runItem("show-a-01")}.includes("場次完成")`, 20000) && !(await b.evaluate(`document.body.innerText`)).match(/付款成功|已付款|取貨成功/));
  await shot(b, "desktop-host-control", 1280, 900, false);

  // Double click: one run.
  const n0 = (await runsOf()).length;
  await b.evaluate(`(() => { const x = document.querySelector('#run-scenarios button[data-scenario="ask"]'); x.click(); x.click(); return true; })()`);
  await b.waitFor(`${runItem("show-a-02")}.includes("場次完成")`, 20000);
  await sleep(1000);
  check("3.2: a double click starts one run", (await runsOf()).length === n0 + 1, String((await runsOf()).length - n0));

  // A lost reply: the server accepts, the browser loses the response; the
  // page resends the same request ID and gets the same run, not a new one.
  let failNextStart = 0, holdNextStart = 0;
  const heldStarts = [];
  const offStarts = b.on("Fetch.requestPaused", (p) => {
    if (p.request.method === "POST" && p.request.url.endsWith("/api/host/runs") && p.responseStatusCode) {
      if (failNextStart > 0) { failNextStart--; b.send("Fetch.failRequest", { requestId: p.requestId, errorReason: "ConnectionReset" }).catch(() => {}); return; }
      if (holdNextStart > 0) { holdNextStart--; heldStarts.push(p.requestId); return; }
    }
    b.send("Fetch.continueRequest", { requestId: p.requestId }).catch(() => {});
  });
  await b.send("Fetch.enable", { patterns: [{ urlPattern: "*/api/host/runs", requestStage: "Response" }] });
  const n1 = (await runsOf()).length;
  failNextStart = 1;
  await clickScenario("deny-budget");
  check("3.2: after a lost reply the same request ID returns the same run", await b.waitFor(`document.getElementById("run-status").textContent.includes("已受理（同一請求）")`, 20000)
    && (await runsOf()).length === n1 + 1, await b.evaluate(text("#run-status")));
  check("3.2: and the page opens it (consumer-b, show-b-01 Deny)", await unityShows(b, { task: "show-b-01", phase: "denied" }));

  // Held reply, then 清除全部: the run stands on the server, the page does nothing with it.
  holdNextStart = 1;
  await clickScenario("ask");
  for (let i = 0; i < 200 && heldStarts.length === 0; i++) await sleep(50);
  check("3.2: a start reply is held (the server accepted it)", heldStarts.length === 1);
  await b.evaluate(`document.getElementById("clear-all").click()`);
  for (const id of heldStarts.splice(0)) await b.send("Fetch.continueRequest", { requestId: id }).catch(() => {});
  await sleep(1500);
  check("3.2: the late start reply after 清除全部 signs nobody in and opens nothing", (await b.evaluate(`${text("#viewer")} === "未驗證" && document.getElementById("creds").dataset.mode === "none" && document.getElementById("control").hidden`)), await b.evaluate(text("#viewer")));
  const accepted = (await runsOf()).find((r) => r.task_id === "show-a-03");
  check("3.2: clearing the page did not withdraw the accepted run", !!accepted, JSON.stringify((await runsOf()).map((r) => r.task_id)));
  await chooseFile(ctlFile);
  await b.waitFor(`document.getElementById("creds").dataset.mode === "file"`);
  check("3.2: reloading the file lists that run without switching to it", await b.waitFor(`${runItem("show-a-03")} !== ""`, 10000) && (await b.evaluate(`${text("#viewer")} === "未驗證"`)));

  // Held reply, then the same file loaded again: a new generation holding
  // the same tokens. The old reply must still switch nothing and open nothing.
  holdNextStart = 1;
  await clickScenario("ask");
  for (let i = 0; i < 200 && heldStarts.length === 0; i++) await sleep(50);
  check("3.2: a second start reply is held", heldStarts.length === 1);
  await chooseFile(ctlFile);
  await b.waitFor(`document.getElementById("creds").dataset.mode === "file"`);
  for (const id of heldStarts.splice(0)) await b.send("Fetch.continueRequest", { requestId: id }).catch(() => {});
  await sleep(1500);
  check("3.2: the old reply after loading the file again switches no consumer and opens no task",
    (await b.evaluate(`${text("#viewer")} === "未驗證" && ${text("#inspector")} === "尚無資料"`)), await b.evaluate(text("#viewer")));
  check("3.2: that run stands on the server too", await b.waitFor(`${runItem("show-a-04")} !== ""`, 10000));
  await b.send("Fetch.disable");
  offStarts();

  // Stop a hanging run: stopping, then stopped with the cancel recorded.
  await clickScenario("hang");
  check("3.2: the hanging run is running with a stop button", await b.waitFor(`${runItem("show-a-05")}.includes("進行中") && !!document.querySelector('#runs li[data-live="running"] .run-stop')`, 20000), await b.evaluate(runItem("show-a-05")));
  // M-B (docs/multi-agent-demo-design.md 3.3): one run per consumer, not one
  // in all: consumer-a's starts wait, consumer-b's do not.
  check("3.2: no other start of the same consumer while its run is active; the other consumer may start", await b.evaluate(`[...document.querySelectorAll('#run-scenarios .consumer-row[data-consumer="consumer-a"] button')].every((x) => x.disabled)
    && [...document.querySelectorAll('#run-scenarios .consumer-row[data-consumer="consumer-b"] button')].every((x) => !x.disabled)`));
  await b.evaluate(`document.querySelector('#runs li[data-live="running"] .run-stop').click()`);
  check("3.2: stopped only after the cancel was recorded", await b.waitFor(`${runItem("show-a-05")}.includes("已停止，取消已記錄")`, 20000), await b.evaluate(runItem("show-a-05")));

  // Nothing of the operator leaves page memory.
  const keptCtl = await b.evaluate(`JSON.stringify({ ls: localStorage.length, ss: sessionStorage.length, cookie: document.cookie, url: location.href })`);
  const htmlCtl = await b.evaluate(`document.documentElement.outerHTML`);
  check("3.2: no token (operator included) in storage, cookie, URL, page or console", !/"ls":[1-9]|"ss":[1-9]|"cookie":"[^"]/.test(keptCtl) && [ct.op, ct.host, ct.a, ct.b].every((t) => !keptCtl.includes(t) && !htmlCtl.includes(t) && !b.consoleText.join("\n").includes(t)));

  // Restart on the same database and address: the runs stay, nothing reruns.
  const before = (await runsOf()).map((r) => r.run_id + r.state).sort().join();
  log("  runs before the restart: " + (await runsOf()).map((r) => r.task_id + "/" + r.state + "/" + (r.live || "-") + "/" + (r.outcome || "-")).join(" "));
  await cd.stop();
  cd = startDemo(ctlState, null, unityBuild, ctlArgs, cAddr);
  await cd.ready();
  ct = cd.tokens();
  allTokens.push(ct.a, ct.b, ct.host, ct.op);
  check("3.2: after a restart the page's credentials are cleared (401)", await b.waitFor(`document.getElementById("creds").dataset.mode === "none"`, 20000));
  const after = (await runsOf()).map((r) => r.run_id + r.state).sort().join();
  check("3.2: after a restart the runs are kept and nothing reruns", after === before && !cd.log().includes("demo: run "), before + " -> " + after + (cd.log().includes("demo: run ") ? " (a run started)" : ""));
  // ---- B1-06 slice 3.3: the task list comes from the Store (docs/demo-operation-design.md section 14) ----
  mark('B1-06 slice 3.3');
  const listIds = () => b.evaluate(`JSON.stringify([...document.querySelectorAll("#task-pick button")].map((x) => x.dataset.task))`).then(JSON.parse);
  await b.send("Page.navigate", { url: "http://" + cAddr + "/" });
  await b.waitFor(`document.readyState === "complete"`);
  await signIn(b, ct.a);
  await b.waitFor(`${text("#viewer")}.startsWith("consumer-a")`);
  const aList = await listIds();
  check("3.3: viewer A's list is A's stored tasks, newest first, none of B's", aList.join() === "show-a-05,show-a-04,show-a-03,show-a-02,show-a-01", aList.join());
  const started = await hostPost(cAddr, ct.op, "/api/host/runs", { request_id: "req-list-0000000001", scenario: "ask" });
  check("3.3: a run started meanwhile (host side)", started.status === 202 && started.json.task_id === "show-a-06", JSON.stringify(started.json));
  check("3.3: the open page lists the new task without a reload", await b.waitFor(`[...document.querySelectorAll("#task-pick button")].some((x) => x.dataset.task === "show-a-06")`, 15000), (await listIds()).join());
  // Review of 843ca4a: only the latest list request of an identity acts.
  // The page's fetch is wrapped: list requests are counted and one reply's
  // JSON can be held, so an older reply really arrives after a newer one.
  await b.evaluate(`(() => { if (!window.__listProbe) { window.__listProbe = { calls: 0, hold: 0, held: [] }; const orig = window.fetch.bind(window);
    window.fetch = async (url, opts) => { const isList = String(url) === "/api/tasks"; if (isList) window.__listProbe.calls++;
      const res = await orig(url, opts);
      if (isList && window.__listProbe.hold > 0) { window.__listProbe.hold--; const body = await res.json();
        return { status: res.status, ok: res.ok, json: () => new Promise((r) => window.__listProbe.held.push(() => r(body))) }; }
      return res; }; } return true; })()`);
  const wake = `(document.dispatchEvent(new Event("visibilitychange")), true)`;
  await b.evaluate(`(window.__listProbe.hold = 1, true)`);
  await b.evaluate(wake); // refresh 1: its JSON is held
  check("3.3 seq: an older list reply is held", await b.waitFor(`window.__listProbe.held.length === 1`, 10000));
  const newer = await hostPost(cAddr, ct.op, "/api/host/runs", { request_id: "req-list-0000000002", scenario: "ask" });
  for (let i = 0; i < 100 && (await hostGet(cAddr, ct.host, "/api/host/runs")).json.runs.some((r) => r.live === "running"); i++) await sleep(100);
  await b.evaluate(wake); // refresh 2: sees the newer task
  check("3.3 seq: the newer list shows the new task", await b.waitFor(`[...document.querySelectorAll("#task-pick button")].some((x) => x.dataset.task === ${JSON.stringify(newer.json.task_id)})`, 10000), (await listIds()).join());
  await b.evaluate(`(window.__listProbe.held.splice(0).forEach((f) => f()), true)`);
  await sleep(1000);
  check("3.3 seq: the older reply, released last, does not overwrite the newer list", (await listIds()).includes(newer.json.task_id), (await listIds()).join());
  await b.evaluate(`(window.__listProbe.calls = 0, true)`);
  await sleep(11000);
  const listCalls = await b.evaluate(`window.__listProbe.calls`);
  check("3.3 seq: one list timer (about one read per 5 s over 11 s)", listCalls >= 1 && listCalls <= 3, listCalls + " reads");

  // A failed list read does not fail the sign-in: the list has its own
  // state, manual task IDs stay open, and the next refresh recovers.
  // Assertions read the text a viewer sees (innerText of a shown element).
  const seen = (sel) => `(${visible(sel)} ? document.querySelector(${JSON.stringify(sel)}).innerText.trim() : "")`;
  let failLists = 0, failHow = "503";
  const offFail = b.on("Fetch.requestPaused", (p) => {
    if (failLists > 0 && p.request.url.endsWith("/api/tasks")) {
      failLists--;
      const done = failHow === "503"
        ? b.send("Fetch.fulfillRequest", { requestId: p.requestId, responseCode: 503, body: Buffer.from("{}").toString("base64") })
        : b.send("Fetch.failRequest", { requestId: p.requestId, errorReason: "InternetDisconnected" });
      done.catch(() => {});
      return;
    }
    b.send("Fetch.continueRequest", { requestId: p.requestId }).catch(() => {});
  });
  await b.send("Fetch.enable", { patterns: [{ urlPattern: "*/api/tasks", requestStage: "Request" }] });
  const noteBefore = await b.evaluate(seen("#list-note"));
  await b.evaluate(`document.getElementById("switch").click()`);
  check("3.3 note: a switch clears the list note", noteBefore.startsWith("清單更新於 ") && (await b.evaluate(`document.getElementById("list-note").textContent === ""`)), noteBefore);
  failLists = 1;
  await signIn(b, ct.a);
  check("3.3 fallback: the viewer is signed in although the list failed", await b.waitFor(`${text("#viewer")}.startsWith("consumer-a")`, 10000)
    && (await b.evaluate(`document.getElementById("task-pick").dataset.state === "error"`)), await b.evaluate(text("#status")));
  check("3.3 fallback: a visible note says no list yet (not 尚無 task)", await b.evaluate(`${seen("#list-note")} === "清單尚未取得，可手動輸入 task ID" && !document.getElementById("task-pick").innerText.includes("尚無 task") && ${seen("#status")}.includes("清單尚未取得")`), await b.evaluate(seen("#list-note")));
  await b.evaluate(`document.getElementById("task-id").value = "show-a-01"; document.querySelector("#task-form button[type=submit]").click()`);
  check("3.3 fallback: a task ID typed by hand still opens", await b.waitFor(`document.getElementById("status").dataset.kind === "ok" && ${text("#inspector")}.includes("show-a-01")`, 15000));
  check("3.3 fallback: the next refresh loads the list", await b.waitFor(`document.getElementById("task-pick").dataset.state === "ok" && document.querySelectorAll("#task-pick button").length > 0`, 12000));
  check("3.3 fallback: the recovered list shows its update time", await b.waitFor(`/^清單更新於 \\d{2}:\\d{2}:\\d{2}$/.test(${seen("#list-note")})`, 5000), await b.evaluate(seen("#list-note")));

  // Review of 0818db8 (R1): after a good, non-empty list, a later failed
  // update (503, then a dropped connection) is visible as text, with the
  // time of the data still shown; the next good read clears it. The open
  // task's status and Inspector are left alone.
  for (const how of ["503", "disconnect"]) {
    const good = await b.evaluate(seen("#list-note"));
    const at = good.replace("清單更新於 ", "");
    const shownIds = await listIds();
    // The task's own polling keeps running: its Store time may move, the rest may not.
    const steady = (sel) => b.evaluate(seen(sel)).then((t) => t.replace(/\d{4}-\d\d-\d\dT[\d:.]+Z/g, "<time>"));
    const statusBefore = await steady("#status"), inspBefore = await steady("#inspector");
    failHow = how === "503" ? "503" : "net";
    failLists = 1;
    await b.evaluate(wake);
    check(`3.3 R1 ${how}: a later failed update is visible, with the shown data's time`,
      await b.waitFor(`${seen("#list-note")} === ${JSON.stringify("更新失敗，以下為上次成功資料（" + at + "）")}`, 10000), await b.evaluate(seen("#list-note")));
    check(`3.3 R1 ${how}: the last good list stays visible`, JSON.stringify(await listIds()) === JSON.stringify(shownIds) && shownIds.length > 0
      && (await b.evaluate(`${seen("#task-pick")}.includes(${JSON.stringify(shownIds[0])})`)), (await listIds()).join());
    check(`3.3 R1 ${how}: the open task's status and Inspector are untouched`,
      (await steady("#status")) === statusBefore && (await steady("#inspector")) === inspBefore && statusBefore.includes("show-a-01"), statusBefore);
    await b.evaluate(wake);
    check(`3.3 R1 ${how}: the next good read clears the error and shows a new time`,
      await b.waitFor(`/^清單更新於 \\d{2}:\\d{2}:\\d{2}$/.test(${seen("#list-note")}) && document.getElementById("task-pick").dataset.state === "ok"`, 10000), await b.evaluate(seen("#list-note")));
  }
  await b.send("Fetch.disable");
  offFail();

  const evBefore = (await hostGet(cAddr, ct.host, "/api/tasks/show-a-01/events")).text;
  await b.evaluate(`document.querySelector('#task-pick button[data-task="show-a-01"]').click()`);
  check("3.3: an older run's task opens from the list", await b.waitFor(`document.getElementById("status").dataset.kind === "ok" && ${text("#inspector")}.includes("show-a-01")`, 15000));
  await b.evaluate(`document.getElementById("replay").click()`);
  await b.waitFor(`document.getElementById("status").dataset.mode === "replay"`);
  check("3.3: its replay ends and the list stays", await b.waitFor(`document.getElementById("status").dataset.mode === "live" && document.getElementById("status").dataset.kind === "ok"`, 40000) && (await listIds()).includes("show-a-06"));
  check("3.3: the replay wrote nothing", (await hostGet(cAddr, ct.host, "/api/tasks/show-a-01/events")).text === evBefore);
  // A's list reply arrives after the switch to B: it shows nothing.
  let holdList = false;
  const heldList = [];
  const offList = b.on("Fetch.requestPaused", (p) => {
    if (holdList && p.request.url.endsWith("/api/tasks") && p.responseStatusCode) { holdList = false; heldList.push(p.requestId); return; }
    b.send("Fetch.continueRequest", { requestId: p.requestId }).catch(() => {});
  });
  await b.send("Fetch.enable", { patterns: [{ urlPattern: "*/api/tasks", requestStage: "Response" }] });
  await b.evaluate(`document.getElementById("switch").click()`);
  holdList = true;
  await signIn(b, ct.a);
  for (let i = 0; i < 200 && heldList.length === 0; i++) await sleep(50);
  check("3.3: viewer A's list reply is held", heldList.length === 1);
  await b.evaluate(`document.getElementById("switch").click()`);
  await signIn(b, ct.b);
  await b.waitFor(`${text("#viewer")}.startsWith("consumer-b")`);
  for (const id of heldList.splice(0)) await b.send("Fetch.continueRequest", { requestId: id }).catch(() => {});
  await sleep(1000);
  const bList = await listIds();
  check("3.3: A's late list reply leaves B's list and identity", bList.join() === "show-b-01" && (await b.evaluate(`${text("#viewer")}.startsWith("consumer-b")`)), bList.join());
  await b.send("Fetch.disable");
  offList();
  check("3.3: no fixed fixture names remain on the page", !(await b.evaluate(`document.body.innerText`)).includes("demo fixture"));

  await cd.stop();

  // ---- B1-06 slice 3.4: cancel a task, errors, runs of a previous host (docs/demo-operation-design.md section 15) ----
  mark('B1-06 slice 3.4');
  // A database written through the host's own runs (cmd/demo
  // TestSeedCancelDisplay), ten minutes back so every Ask is past its
  // deadline: show-a-01 CLI timeout, show-a-02 Ask, show-b-01 Deny,
  // show-a-03 a failed cancel record, show-a-04 both records failed.
  const seedState = join(tmp, "state-cancel");
  mkdirSync(seedState, { recursive: true, mode: 0o700 });
  execFileSync("go", ["test", "./cmd/demo", "-run", "TestSeedCancelDisplay", "-count=1"], { cwd: repo, env: { ...process.env, APA_SEED_CANCEL_DIR: seedState }, stdio: "ignore" });
  const xd = startDemo(seedState, null, unityBuild, ctlArgs);
  const xAddr = await xd.ready();
  const xt = xd.tokens();
  allTokens.push(xt.a, xt.b, xt.host, xt.op);
  check("3.4: the previous host's open run is marked interrupted; nothing reruns", xd.log().includes("1 run(s) of a previous host marked interrupted") && !xd.log().includes("demo: run "));
  const xFile = join(tmp, "tokens-cancel.json");
  writeFileSync(xFile, readFileSync(join(seedState, "tokens.json")));
  await b.send("Page.navigate", { url: "http://" + xAddr + "/host" });
  await b.waitFor(`document.readyState === "complete"`);
  await b.waitFor(`["ready","rendered","failed"].includes(document.getElementById("unity-status").dataset.state)`, 90000);
  await chooseFile(xFile);
  await b.waitFor(`document.getElementById("creds").dataset.mode === "file"`);
  // What the host sees: a run's line (innerText of a shown element) and its buttons.
  const seenLine = (task) => `(([...document.querySelectorAll("#runs li")].find((l) => l.textContent.startsWith(${JSON.stringify(task + "（")})) || {}).innerText || "")`;
  const cancelBtn = (task) => `document.querySelector('#runs .task-cancel[data-task="${task}"]')`;
  const runSeen = () => b.evaluate(`document.getElementById("run-status").innerText`);
  const lineHas = (task, parts, button, ms = 15000) => b.waitFor(`(() => { const l = ${seenLine(task)}; return ${JSON.stringify(parts)}.every((p) => l.includes(p)) && !!${cancelBtn(task)} === ${button}; })()`, ms);
  const xRuns = async () => (await hostGet(xAddr, xt.op, "/api/host/runs")).json.runs;
  const xEvents = async (task) => (await hostGet(xAddr, xt.host, "/api/tasks/" + task + "/events")).json.events;
  check("3.4: CLI timeout: its history, the Ask left to its deadline, no cancel offered", await lineHas("show-a-01", ["CLI 已逾時（已記錄）", "Ask 已依原到期機制到期"], false), await b.evaluate(seenLine("show-a-01")));
  check("3.4: Deny: its original reason, no cancel offered", await lineHas("show-b-01", ["場次完成", "Deny：INSUFFICIENT_BUDGET"], false), await b.evaluate(seenLine("show-b-01")));
  check("3.4: an expired Ask: the task can still be cancelled", await lineHas("show-a-02", ["場次完成", "Ask 已到期；task 仍可由主持人取消"], true), await b.evaluate(seenLine("show-a-02")));
  check("3.4: a failed cancel record: that history, and the task can be cancelled", await lineHas("show-a-03", ["程序已停止；當時取消紀錄寫入失敗（CLOCK_REGRESSION）", "Ask 已到期；task 仍可由主持人取消"], true), await b.evaluate(seenLine("show-a-03")));
  check("3.4: an interrupted run: that history, and the task can be cancelled", await lineHas("show-a-04", ["場次曾中斷（上一個 host 未正常結束）", "Ask 已到期；task 仍可由主持人取消"], true), await b.evaluate(seenLine("show-a-04")));
  const runsHistory = JSON.stringify(await xRuns());

  // Review of 7ffc45d (R2): with the runs list folded, its history and what
  // cannot be read now stay visible, apart, in its summary and the status bar.
  {
    const vis = (sel) => `((document.querySelector(${JSON.stringify(sel)}) || {}).innerText || "")`;
    await b.evaluate(`(document.getElementById("runs-box").open = false, true)`);
    check("R2 folded: the runs summary and the status bar keep the history (2 runs interrupted or with a failed record)",
      await b.waitFor(`${vis("#runs-summary")}.includes("歷史：2 場中斷或紀錄寫入失敗") && ${vis("header.top")}.includes("場次歷史：2 場中斷或紀錄寫入失敗")`, 10000),
      await b.evaluate(vis("#runs-summary")) + " / " + await b.evaluate(vis("#alerts")));
    let fail503 = false;
    const off503 = b.on("Fetch.requestPaused", (p) => {
      if (fail503 && p.request.url.endsWith("/api/tasks") && p.request.headers.Authorization === "Bearer " + xt.host) {
        b.send("Fetch.fulfillRequest", { requestId: p.requestId, responseCode: 503, responseHeaders: [{ name: "Content-Type", value: "application/json" }], body: Buffer.from('{"error":"store_unavailable"}').toString("base64") }).catch(() => {});
        return;
      }
      b.send("Fetch.continueRequest", { requestId: p.requestId }).catch(() => {});
    });
    await b.send("Fetch.enable", { patterns: [{ urlPattern: "*/api/tasks", requestStage: "Request" }] });
    fail503 = true;
    check("R2 folded: a failed task read shows now, apart from the history", await b.waitFor(`${vis("#runs-summary")}.includes("目前：task 狀態讀取失敗（status_503），未確認") && ${vis("header.top")}.includes("最近場次目前：task 狀態讀取失敗")`, 10000)
      && (await b.evaluate(vis("#runs-summary"))).includes("歷史：2 場"), await b.evaluate(vis("#runs-summary")));
    fail503 = false;
    check("R2 folded: after recovery the current warning goes, the history stays", await b.waitFor(`!${vis("header.top")}.includes("最近場次目前") && !${vis("#runs-summary")}.includes("目前：") && ${vis("header.top")}.includes("場次歷史")`, 10000),
      await b.evaluate(vis("#alerts")));
    await b.send("Fetch.disable");
    off503();
    await b.evaluate(`document.getElementById("clear-all").click()`);
    check("R2: 清除全部 leaves no runs warning behind", await b.waitFor(`!${vis("header.top")}.includes("場次歷史") && !${vis("header.top")}.includes("最近場次目前")`, 5000), await b.evaluate(vis("#alerts")));
    await chooseFile(xFile);
    await b.waitFor(`document.getElementById("creds").dataset.mode === "file"`);
    check("R2: loading the file again brings the history back from the server", await b.waitFor(`${vis("header.top")}.includes("場次歷史：2 場中斷或紀錄寫入失敗")`, 10000));
    await b.evaluate(`(document.getElementById("runs-box").open = true, true)`);
    await lineHas("show-a-02", ["Ask 已到期；task 仍可由主持人取消"], true);
  }

  // Review of d61771c (R2): a task whose details were not read is not a
  // task without Asks. Its detail read answers 503 here (show-a-01's
  // expired Ask is in the Store); the line says it is not confirmed.
  let failDetail = false;
  const offDetail = b.on("Fetch.requestPaused", (p) => {
    if (failDetail && p.request.url.endsWith("/api/tasks/show-a-01") && p.request.headers.Authorization === "Bearer " + xt.host) {
      b.send("Fetch.fulfillRequest", { requestId: p.requestId, responseCode: 503, responseHeaders: [{ name: "Content-Type", value: "application/json" }], body: Buffer.from('{"error":"store_unavailable"}').toString("base64") }).catch(() => {});
      return;
    }
    b.send("Fetch.continueRequest", { requestId: p.requestId }).catch(() => {});
  });
  await b.send("Fetch.enable", { patterns: [{ urlPattern: "*/api/tasks/*", requestStage: "Request" }] });
  failDetail = true;
  check("3.4 R2: a failed detail read shows the Asks unconfirmed, not none", await lineHas("show-a-01", ["CLI 已逾時（已記錄）；Ask 狀態未確認（task 詳情未取得）"], false)
    && !(await b.evaluate(seenLine("show-a-01"))).includes("無未決 Ask"), await b.evaluate(seenLine("show-a-01")));
  failDetail = false;
  check("3.4 R2: the next good read confirms the expiry again", await lineHas("show-a-01", ["Ask 已依原到期機制到期"], false), await b.evaluate(seenLine("show-a-01")));
  await b.send("Fetch.disable");
  offDetail();

  // The button is only a hint: a forged task list offers cancel for the Deny
  // and the timed-out task; the server refuses both and writes nothing.
  let forge = false;
  const offForge = b.on("Fetch.requestPaused", async (p) => {
    if (forge && p.responseStatusCode === 200 && p.request.url.endsWith("/api/tasks") && p.request.headers.Authorization === "Bearer " + xt.host) {
      try {
        const r = await b.send("Fetch.getResponseBody", { requestId: p.requestId });
        const doc = JSON.parse(r.base64Encoded ? Buffer.from(r.body, "base64").toString() : r.body);
        for (const t of doc.tasks) if (t.task_id === "show-b-01" || t.task_id === "show-a-01") t.status = "running";
        await b.send("Fetch.fulfillRequest", { requestId: p.requestId, responseCode: 200, responseHeaders: [{ name: "Content-Type", value: "application/json" }], body: Buffer.from(JSON.stringify(doc)).toString("base64") });
      } catch { b.send("Fetch.continueRequest", { requestId: p.requestId }).catch(() => {}); }
      return;
    }
    b.send("Fetch.continueRequest", { requestId: p.requestId }).catch(() => {});
  });
  await b.send("Fetch.enable", { patterns: [{ urlPattern: "*/api/tasks", requestStage: "Response" }] });
  const refused = { "show-b-01": "task 已是 Deny（終態），未取消；Deny 原因保留", "show-a-01": "task 已因 CLI 逾時結束，未取消；原紀錄保留" };
  for (const [task, words] of Object.entries(refused)) {
    const evs = (await xEvents(task)).length;
    forge = true;
    await b.waitFor(`!!${cancelBtn(task)} && !${cancelBtn(task)}.disabled`, 15000);
    await b.evaluate(`${cancelBtn(task)}.click()`);
    check("3.4: a forged hint for " + task + ": the server refuses, the page says so", await b.waitFor(`document.getElementById("run-status").innerText === ${JSON.stringify("task " + task + "：" + words)}`, 10000), await runSeen());
    forge = false;
    check("3.4: " + task + " unchanged, its line back to the Store's state", (await xEvents(task)).length === evs
      && await lineHas(task, task === "show-b-01" ? ["Deny：INSUFFICIENT_BUDGET"] : ["CLI 已逾時（已記錄）"], false), await b.evaluate(seenLine(task)));
  }
  await b.send("Fetch.disable");
  offForge();

  // Cancel the expired Ask, the interrupted run's task and the one whose cancel record failed.
  const cancelBy = async (task) => { await b.waitFor(`!!${cancelBtn(task)} && !${cancelBtn(task)}.disabled`, 15000); await b.evaluate(`${cancelBtn(task)}.click()`); return b.waitFor(`document.getElementById("run-status").innerText === ${JSON.stringify("task " + task + " 已取消")}`, 15000); };
  check("3.4: an expired Ask's task is cancelled", await cancelBy("show-a-02") && await lineHas("show-a-02", ["場次完成", "task 現已取消"], false), await runSeen());
  let ev2 = (await xEvents("show-a-02")).map((e) => e.event_type);
  check("3.4: its events: the expiry, then the cancel, no withdrawal", ev2.at(-1) === "TASK_CANCELLED" && ev2.filter((t) => t === "APPROVAL_EXPIRED").length === 1 && !ev2.includes("APPROVAL_WITHDRAWN"), ev2.join());
  check("3.4: the interrupted run: 場次曾中斷；task 現已取消", await cancelBy("show-a-04") && await lineHas("show-a-04", ["場次曾中斷（上一個 host 未正常結束）；task 現已取消"], false), await b.evaluate(seenLine("show-a-04")));
  check("3.4: the failed record: 當時取消紀錄寫入失敗；後續已成功取消 task", await cancelBy("show-a-03") && await lineHas("show-a-03", ["當時取消紀錄寫入失敗（CLOCK_REGRESSION）；後續已成功取消 task"], false), await b.evaluate(seenLine("show-a-03")));
  check("3.4: the cancels left every run's history as it was", JSON.stringify(await xRuns()) === runsHistory);
  await shot(b, "desktop-host-cancel", 1280, 900, false);

  // A live Ask: start, cancel; the Dashboard, its Timeline and Inspector and Unity agree.
  const newRun = async (scenario) => {
    const n = (await xRuns()).length;
    await clickScenario(scenario);
    for (let i = 0; i < 200 && (await xRuns()).length === n; i++) await sleep(100);
    return (await xRuns())[0];
  };
  const askEnded = async () => { const r = await newRun("ask"); await lineHas(r.task_id, ["場次完成", "Ask 等待中；task 可由主持人取消"], true, 20000); return r.task_id; };
  const live = await askEnded();
  check("3.4: a new Ask waits, the Dashboard shows it", await unityShows(b, { task: live, phase: "waiting", verdict: "Ask" }), await b.evaluate(text("#unity-status")));
  check("3.4: cancel while the Ask waits", await cancelBy(live) && await lineHas(live, ["task 現已取消"], false), await runSeen());
  check("3.4: Unity shows the same task withdrawn", await unityShows(b, { task: live, phase: "withdrawn", verdict: "Ask" }), await b.evaluate(text("#unity-status")));
  const tl = await b.evaluate(`document.getElementById("timeline").innerText`), insp = await b.evaluate(`document.getElementById("inspector").innerText`);
  check("3.4: Timeline and Inspector show the same cancel", tl.includes("APPROVAL_WITHDRAWN") && tl.includes("TASK_CANCELLED") && insp.includes(live) && insp.includes("cancelled") && insp.includes("withdrawn"),
    (tl.match(/TASK_CANCELLED|APPROVAL_WITHDRAWN/g) || []).join() + " / " + (insp.match(/cancelled|withdrawn/g) || []).join());
  check("3.4: nothing claims payment or delivery", !(await b.evaluate(`document.body.innerText`)).match(/付款成功|已付款|取貨成功|交付成功/));
  await shot(b, "desktop-host-cancel-live", 1280, 900, false);

  // The replay after a cancel is read only: no cancel is sent, no event added.
  await b.evaluate(`(() => { if (!window.__cancelProbe) { window.__cancelProbe = { posts: 0, hold: 0, held: [] }; const orig = window.fetch.bind(window);
    window.fetch = async (url, opts) => { const isCancel = String(url).endsWith("/cancel") && opts && opts.method === "POST"; if (isCancel) window.__cancelProbe.posts++;
      const res = await orig(url, opts);
      if (isCancel && window.__cancelProbe.hold > 0) { window.__cancelProbe.hold--; const body = await res.json();
        return { status: res.status, ok: res.ok, json: () => new Promise((r) => window.__cancelProbe.held.push(() => r(body))) }; }
      return res; }; } return true; })()`);
  const liveEvents = (await xEvents(live)).length;
  await b.evaluate(`document.getElementById("replay").click()`);
  await b.waitFor(`document.getElementById("status").dataset.mode === "replay"`);
  await b.waitFor(`document.getElementById("status").dataset.mode === "live" && document.getElementById("status").dataset.kind === "ok"`, 60000);
  check("3.4: a replay after the cancel sends no cancel and adds no event", (await b.evaluate(`window.__cancelProbe.posts`)) === 0 && (await xEvents(live)).length === liveEvents);

  // A lost cancel reply: the server committed; the page says "not confirmed", resends nothing.
  let failCancel = 0, failHostRead = 0, cancel503 = 0, refuse503 = 0;
  const offLost = b.on("Fetch.requestPaused", (p) => {
    if (refuse503 > 0 && !p.responseStatusCode && p.request.url.endsWith("/cancel")) { // never reaches the server
      refuse503--;
      b.send("Fetch.fulfillRequest", { requestId: p.requestId, responseCode: 503, responseHeaders: [{ name: "Content-Type", value: "application/json" }], body: Buffer.from('{"error":"store_unavailable","code":"STORE_ERROR"}').toString("base64") }).catch(() => {});
      return;
    }
    if (cancel503 > 0 && p.responseStatusCode && p.request.url.endsWith("/cancel")) {
      cancel503--;
      b.send("Fetch.fulfillRequest", { requestId: p.requestId, responseCode: 503, responseHeaders: [{ name: "Content-Type", value: "application/json" }], body: Buffer.from('{"error":"store_unavailable","code":"STORE_ERROR"}').toString("base64") }).catch(() => {});
      return;
    }
    if (failCancel > 0 && p.responseStatusCode && p.request.url.endsWith("/cancel")) { failCancel--; b.send("Fetch.failRequest", { requestId: p.requestId, errorReason: "ConnectionReset" }).catch(() => {}); return; }
    if (failHostRead > 0 && !p.responseStatusCode && /\/api\/tasks\/show-a-\d+$/.test(p.request.url) && p.request.headers.Authorization === "Bearer " + xt.host) { failHostRead--; b.send("Fetch.failRequest", { requestId: p.requestId, errorReason: "ConnectionRefused" }).catch(() => {}); return; }
    b.send("Fetch.continueRequest", { requestId: p.requestId }).catch(() => {});
  });
  await b.send("Fetch.enable", { patterns: [{ urlPattern: "*/cancel", requestStage: "Response" }, { urlPattern: "*/cancel", requestStage: "Request" }, { urlPattern: "*/api/tasks/*", requestStage: "Request" }] });
  const lost = await askEnded();
  failCancel = 1;
  await b.evaluate(`${cancelBtn(lost)}.click()`);
  check("3.4: a lost reply: 結果尚未確認, neither success nor failure", await b.waitFor(`document.getElementById("run-status").innerText.includes("結果尚未確認")`, 15000)
    && !(await runSeen()).includes("已取消") && !(await runSeen()).includes("未取消"), await runSeen());
  const lostEv = (await xEvents(lost)).map((e) => e.event_type);
  check("3.4: the server did cancel it once; the page sent one request", lostEv.filter((t) => t === "TASK_CANCELLED").length === 1 && (await b.evaluate(`window.__cancelProbe.posts`)) === 1, lostEv.join());
  check("3.4: the runs list then reads the Store: task 現已取消", await lineHas(lost, ["task 現已取消"], false), await b.evaluate(seenLine(lost)));
  // Committed, then the follow-up read fails: 取消已記錄；畫面更新失敗.
  const reread = await askEnded();
  failHostRead = 1;
  await b.evaluate(`${cancelBtn(reread)}.click()`);
  check("3.4: committed but the follow-up read fails: 取消已記錄；畫面更新失敗", await b.waitFor(`document.getElementById("run-status").innerText.startsWith("取消已記錄；畫面更新失敗")`, 15000)
    && (await xEvents(reread)).at(-1).event_type === "TASK_CANCELLED", await runSeen());
  // Review of d61771c (R3): the server committed the cancel, the page gets
  // a 503 (as when a re-read fails): not confirmed, never "not cancelled".
  const late503 = await askEnded();
  cancel503 = 1;
  await b.evaluate(`${cancelBtn(late503)}.click()`);
  check("3.4 R3: a 503 after a committed cancel: 取消結果尚未確認, not 未取消", await b.waitFor(`document.getElementById("run-status").innerText.includes("取消結果尚未確認")`, 15000)
    && !/未取消|取消未完成/.test(await runSeen()) && (await xEvents(late503)).filter((e) => e.event_type === "TASK_CANCELLED").length === 1, await runSeen());
  check("3.4 R3: the re-read shows the committed state: task 現已取消", await lineHas(late503, ["task 現已取消"], false), await b.evaluate(seenLine(late503)));
  // A 503 with nothing committed (the request never reaches the server): the same words, and the task is still waiting.
  const none503 = await askEnded();
  refuse503 = 1;
  await b.evaluate(`${cancelBtn(none503)}.click()`);
  check("3.4 R3: a 503 with nothing committed: the same 取消結果尚未確認, no claim either way", await b.waitFor(`document.getElementById("run-status").innerText.includes("取消結果尚未確認")`, 15000)
    && !/未取消|取消未完成|已取消/.test(await runSeen()) && !(await xEvents(none503)).some((e) => e.event_type === "TASK_CANCELLED"), await runSeen());
  check("3.4 R3: the re-read shows the true state: the Ask still waits, cancel still offered", await lineHas(none503, ["Ask 等待中；task 可由主持人取消"], true), await b.evaluate(seenLine(none503)));
  await b.send("Fetch.disable");
  offLost();

  // Held replies (the server committed; the page's fetch holds the JSON, so
  // an abort cannot end it): a switch of task or consumer, 清除全部 or a new
  // file before the reply. The old reply opens, signs in and shows nothing.
  const holdCancel = async (task) => {
    await b.evaluate(`(window.__cancelProbe.hold = 1, true)`);
    await b.evaluate(`${cancelBtn(task)}.click()`);
    return b.waitFor(`window.__cancelProbe.held.length === 1`, 15000);
  };
  const releaseCancel = async () => { await b.evaluate(`(window.__cancelProbe.held.splice(0).forEach((f) => f()), true)`); await sleep(1500); };
  const notOld = (task) => `!document.getElementById("run-status").textContent.includes("task ${task} 已取消") && !document.getElementById("run-status").textContent.includes("取消已記錄")`;
  // A switch of task.
  const sw = await askEnded();
  check("3.4 held: a cancel reply is held after the server committed", await holdCancel(sw) && (await xEvents(sw)).at(-1).event_type === "TASK_CANCELLED");
  await b.evaluate(`document.getElementById("task-id").value = "show-a-02"; document.querySelector("#task-form button[type=submit]").click()`);
  await b.waitFor(`document.getElementById("status").dataset.kind === "ok" && ${text("#inspector")}.includes("show-a-02")`, 15000);
  await releaseCancel();
  check("3.4 held: after a switch of task the old reply shows no result and opens nothing", await b.evaluate(notOld(sw)) && (await b.evaluate(`${text("#inspector")}.includes("show-a-02") && !${text("#inspector")}.includes(${JSON.stringify(sw)})`))
    && await viewsTask(b, "show-a-02", 5000), await runSeen());
  check("3.4 held: the runs list still reads the Store: that task 現已取消", await lineHas(sw, ["task 現已取消"], false), await b.evaluate(seenLine(sw)));
  // A switch of consumer.
  const swc = await askEnded();
  await holdCancel(swc);
  await b.evaluate(`document.querySelector('#consumer-pick button[data-consumer="consumer-b"]').click()`);
  await b.waitFor(`${text("#viewer")}.startsWith("consumer-b")`, 10000);
  await releaseCancel();
  check("3.4 held: after a switch of consumer the old reply shows no result and switches nothing back", await b.evaluate(notOld(swc)) && (await b.evaluate(`${text("#viewer")}.startsWith("consumer-b")`)) && !(await b.evaluate(text("#inspector"))).includes(swc), await runSeen());
  // 清除全部.
  const clr = await askEnded();
  await holdCancel(clr);
  await b.evaluate(`document.getElementById("clear-all").click()`);
  await releaseCancel();
  check("3.4 held: after 清除全部 the old reply restores nothing", await b.evaluate(`${notOld(clr)} && document.getElementById("creds").dataset.mode === "none" && document.getElementById("control").hidden && ${text("#viewer")} === "未驗證"`), await b.evaluate(text("#run-status")));
  check("3.4 held: clearing did not withdraw the server's cancel", (await xEvents(clr)).at(-1).event_type === "TASK_CANCELLED");
  await chooseFile(xFile);
  await b.waitFor(`document.getElementById("creds").dataset.mode === "file"`);
  check("3.4 held: loading the file again shows that task cancelled, from the Store", await lineHas(clr, ["task 現已取消"], false), await b.evaluate(seenLine(clr)));
  // A new file (the same tokens, a new generation).
  const nf = await askEnded();
  await holdCancel(nf);
  await chooseFile(xFile);
  await b.waitFor(`document.getElementById("creds").dataset.mode === "file"`);
  await releaseCancel();
  check("3.4 held: after loading the file again the old reply shows nothing and opens nothing",
    await b.evaluate(`${notOld(nf)} && ${text("#viewer")} === "未驗證" && ${text("#inspector")} === "尚無資料"`), await runSeen());

  // Review of d61771c (R2): beyond DETAILS_MAX (5) a task's details are
  // not read in that poll; a forged list marks every task failed, and each
  // line past the fifth says unconfirmed, never 無未決 Ask.
  let forgeAll = false;
  const offAll = b.on("Fetch.requestPaused", async (p) => {
    if (forgeAll && p.responseStatusCode === 200 && p.request.url.endsWith("/api/tasks") && p.request.headers.Authorization === "Bearer " + xt.host) {
      try {
        const r = await b.send("Fetch.getResponseBody", { requestId: p.requestId });
        const doc = JSON.parse(r.base64Encoded ? Buffer.from(r.body, "base64").toString() : r.body);
        for (const t of doc.tasks) t.status = "failed";
        await b.send("Fetch.fulfillRequest", { requestId: p.requestId, responseCode: 200, responseHeaders: [{ name: "Content-Type", value: "application/json" }], body: Buffer.from(JSON.stringify(doc)).toString("base64") });
      } catch { b.send("Fetch.continueRequest", { requestId: p.requestId }).catch(() => {}); }
      return;
    }
    b.send("Fetch.continueRequest", { requestId: p.requestId }).catch(() => {});
  });
  await b.send("Fetch.enable", { patterns: [{ urlPattern: "*/api/tasks", requestStage: "Response" }] });
  forgeAll = true;
  const nRuns = (await xRuns()).length;
  const capOk = await b.waitFor(`[...document.querySelectorAll("#runs li")].filter((l) => l.innerText.includes(${JSON.stringify("Ask 狀態未確認（task 詳情未取得）")})).length === ${nRuns - 5}`, 15000);
  const capLines = await b.evaluate(`JSON.stringify([...document.querySelectorAll("#runs li")].map((l) => l.innerText))`);
  check("3.4 R2: past the 5 detail reads the lines say unconfirmed (" + (nRuns - 5) + " of " + nRuns + ")", capOk
    && JSON.parse(capLines).filter((l) => l.includes("Ask 狀態未確認（task 詳情未取得）")).every((l) => !l.includes("無未決 Ask")), capLines.slice(0, 300));
  forgeAll = false;
  await b.send("Fetch.disable");
  offAll();

  // A running run offers its stop, never 取消 task.
  const hang = await newRun("hang");
  check("3.4: a running run offers 停止, not 取消 task", await b.waitFor(`${seenLine(hang.task_id)}.includes("進行中") && !!document.querySelector('#runs li[data-live="running"] .run-stop') && !${cancelBtn(hang.task_id)}`, 20000), await b.evaluate(seenLine(hang.task_id)));
  await b.evaluate(`document.querySelector('#runs li[data-live="running"] .run-stop').click()`);
  check("3.4: stopped with its own cancel; no 取消 task afterwards", await lineHas(hang.task_id, ["已停止，取消已記錄"], false, 20000), await b.evaluate(seenLine(hang.task_id)));
  const keptX = await b.evaluate(`JSON.stringify({ ls: localStorage.length, ss: sessionStorage.length, cookie: document.cookie, url: location.href })`);
  check("3.4: no token in storage, cookie, URL, page or console", !/"ls":[1-9]|"ss":[1-9]|"cookie":"[^"]/.test(keptX) && [xt.op, xt.host, xt.a, xt.b].every((t) => !keptX.includes(t) && !b.consoleText.join("\n").includes(t)));
  await xd.stop();

  // ---- B1-06 slice 3.5: the whole fake demo, in order, on one host (docs/demo-operation-design.md section 16) ----
  mark('B1-06 slice 3.5');
  // The binary and catalog the manual acceptance uses (testdata/demo/
  // scenarios-acceptance.json: ask, deny-budget, ask-wait), a new database,
  // then a restart on that same database. Each scenario is a new run and
  // task; every one is checked in Unity, the Dashboard, Inspector and
  // Timeline, and logged with its run, task and event sequence.
  {
    const accState = join(tmp, "state-acceptance");
    const accArgs = ["--fake-cli", join(bin, "fake-claude"), "--scenarios", join(repo, "testdata/demo/scenarios-acceptance.json"), "--registry", join(repo, "testdata/demo/registry.json")];
    let ad = startDemo(accState, null, unityBuild, accArgs);
    const aAddr = await ad.ready();
    let at = ad.tokens();
    allTokens.push(at.a, at.b, at.host, at.op);
    log("  3.5 acceptance host at " + aAddr + ", new database " + join(accState, "demo.db") + " (throwaway), catalog testdata/demo/scenarios-acceptance.json");
    check("3.5: a new database, nothing runs at start", !ad.log().includes("demo: run ") && !ad.log().includes("marked interrupted"));
    const aFile = join(tmp, "tokens-acceptance.json");
    writeFileSync(aFile, readFileSync(join(accState, "tokens.json")));
    const aRuns = async () => (await hostGet(aAddr, at.op, "/api/host/runs")).json.runs;
    const aEvents = async (task) => ((await hostGet(aAddr, at.host, "/api/tasks/" + task + "/events")).json.events || []).map((e) => e.event_type); // [] before the task is stored
    const aTask = async (task) => (await hostGet(aAddr, at.host, "/api/tasks/" + task)).json;
    const mapping = [];
    const record = async (what, task) => { const r = (await aRuns()).find((x) => x.task_id === task); const ev = await aEvents(task); mapping.push(what + ": run " + (r ? r.run_id : "-") + " task " + task + " #" + ev.length + " [" + ev.join(",") + "]"); };
    const startNew = async (scenario) => {
      const n = (await aRuns()).length;
      await clickScenario(scenario);
      for (let i = 0; i < 200 && (await aRuns()).length === n; i++) await sleep(100);
      return (await aRuns())[0];
    };
    // views: the four panels show this task with this phase, verdict and last event.
    const views = async (label, task, phase, verdict, lastEvent, inspectorWords) => {
      // Ready when the Store has the expected last event (a run's task is stored a moment after it starts).
      let ev = await aEvents(task);
      for (let i = 0; i < 100 && ev.at(-1) !== lastEvent; i++) { await sleep(200); ev = await aEvents(task); }
      // M-B: a task older than its consumer's newest run is not in the shop:
      // the page says the scene shows the newest one, and the scene line does.
      const consumer = task.startsWith("show-b") ? "consumer-b" : "consumer-a";
      const newest = ((await aRuns()).find((r) => r.consumer_id === consumer) || {}).task_id;
      const u = newest && newest !== task
        ? await b.waitFor(`!document.getElementById("view-note").hidden && document.getElementById("view-note-text").textContent.includes("場景呈現的是 ${newest}")
            && (document.querySelector('#scene-status li[data-consumer="${consumer}"]') || { dataset: {} }).dataset.task === "${newest}"`, 30000)
        : await unityShows(b, { task, seq: ev.length, phase, verdict }, 30000);
      const d = await b.waitFor(`document.getElementById("scene-text").dataset.phase === ${JSON.stringify(phase)} && document.querySelectorAll("#timeline li").length === ${ev.length}`, 15000);
      const tl = await b.evaluate(`document.getElementById("timeline").innerText`);
      const ins = await b.evaluate(`document.getElementById("inspector").innerText`);
      check("3.5 " + label + ": Unity, Dashboard, Inspector and Timeline agree on " + task + " #" + ev.length + " " + phase,
        u && d && ev.at(-1) === lastEvent && tl.includes(lastEvent) && ins.includes(task) && inspectorWords.every((w) => ins.includes(w)),
        "unity " + (await b.evaluate(text("#unity-status"))) + " / events " + ev.join(","));
    };

    // 1–2. Start, readiness, the token file in page memory only.
    await b.send("Page.navigate", { url: "http://" + aAddr + "/host" });
    await b.waitFor(`document.readyState === "complete"`);
    await b.waitFor(`["ready","rendered","failed"].includes(document.getElementById("unity-status").dataset.state)`, 90000);
    const shown = (sel) => `((document.querySelector(${JSON.stringify(sel)}) || {}).innerText || "")`;
    const inView = (sel) => `(() => { const e = document.querySelector(${JSON.stringify(sel)}); if (!e) return false; const r = e.getBoundingClientRect(); return r.top >= 0 && r.bottom <= innerHeight && r.width > 0; })()`;
    // D1: everything folded, before a file: the status bar still says readiness is not confirmed.
    await b.evaluate(`(window.__wasOpen = [...document.querySelectorAll("details")].map((d) => d.open), document.querySelectorAll("details").forEach((d) => { d.open = false; }), true)`);
    const barA = await b.evaluate(shown("header.top"));
    await b.evaluate(`([...document.querySelectorAll("details")].forEach((d, i) => { d.open = window.__wasOpen[i]; }), true)`); // opened again, as a user would
    check("D1 /host folded: the status bar says fake, no payment, readiness not confirmed, and offers the file", barA.includes("M1／fake agent／不付款") && barA.includes("就緒：") && barA.includes("未確認")
      && barA.includes("付款 未實作") && (await b.evaluate(`!document.getElementById("load-file").hidden`)), barA.slice(0, 200));
    await chooseFile(aFile);
    const rowState = (k) => `(document.querySelector('#readiness tr[data-item="${k}"]')?.dataset.state || "")`;
    check("3.5: readiness: HTTP, Store, fake CLI and the Unity build ready; real CLI and payment off",
      await b.waitFor(`${rowState("HTTP")} === "ok" && ${rowState("Store")} === "ok" && ${rowState("fake CLI")} === "ok" && ${rowState("Unity 建置")} === "ok" && ${rowState("真實 CLI")} === "off" && ${rowState("付款")} === "off"`, 20000),
      await b.evaluate(`[...document.querySelectorAll("#readiness tr")].map((r) => r.dataset.item + "=" + r.dataset.state).join(" ")`));
    const keptA = await b.evaluate(`JSON.stringify({ ls: localStorage.length, ss: sessionStorage.length, cookie: document.cookie, url: location.href, field: document.getElementById("token-file").value })`);
    check("3.5: the token file is in page memory only (field emptied; no storage, cookie or URL)", /"ls":0,"ss":0,"cookie":"","url":"[^"]*\/host","field":""/.test(keptA) && [at.op, at.host, at.a, at.b].every((t) => !keptA.includes(t)), keptA.slice(0, 120));
    await b.evaluate(`(window.scrollTo(0, 0), true)`);
    check("D1 /host at 1280×900: the shop and the start buttons are in the first viewport", await b.evaluate(inView("#unity-canvas")) && await b.evaluate(`[...document.querySelectorAll("#run-scenarios button")].every((x) => { const r = x.getBoundingClientRect(); return r.top >= 0 && r.bottom <= innerHeight; })`));
    await shot(b, "desktop-acceptance-ready", 1280, 900, false);
    await shotFull(b, "full-acceptance-ready");

    // 3. Ask: a new run, the page switches to consumer-a and opens its task.
    const askRun = await startNew("ask");
    check("3.5 Ask: the page switches to consumer-a and opens the new task", await b.waitFor(`${text("#viewer")}.startsWith("consumer-a") && ${text("#inspector")}.includes(${JSON.stringify(askRun.task_id)})`, 20000), askRun.task_id);
    await views("Ask", askRun.task_id, "waiting", "Ask", "POLICY_DECIDED", ["Ask", "HUMAN_APPROVAL_REQUIRED", "pending", "未付款"]);
    check("3.5 Ask: its line: 場次完成；Ask 等待中, cancel offered", await lineHas(askRun.task_id, ["場次完成", "Ask 等待中；task 可由主持人取消"], true, 20000), await b.evaluate(seenLine(askRun.task_id)));
    const askDeadline = (await aTask(askRun.task_id)).purchases[0].deadline_at;
    const cardAsk = await b.evaluate(shown("#card"));
    check("D1 card: the 402 amount and the catalog's fixture price are two rows; the catalog is marked not bound", cardAsk.includes("402 綁定金額") && cardAsk.includes("500000（基本單位，fixture-usdc）")
      && cardAsk.includes("700 KRW") && cardAsk.includes("不綁定此請購") && !/付款成功|已驗證|通過/.test(cardAsk), cardAsk.slice(0, 300));
    await b.evaluate(`(window.scrollTo(0, 0), true)`);
    check("D1 host row: 取消 task for the open task is in the first viewport", await b.waitFor(`!!document.querySelector('.row-cancel[data-task="${askRun.task_id}"]')`, 10000) && await b.evaluate(inView(`.row-cancel[data-task="${askRun.task_id}"]`)));
    await record("Ask", askRun.task_id);
    await shot(b, "desktop-acceptance-ask", 1280, 900, false);
    await shotFull(b, "full-acceptance-ask");

    // 4. Deny: consumer-b, the reason kept apart from the run's history.
    const denyRun = await startNew("deny-budget");
    check("3.5 Deny: the page switches to consumer-b", await b.waitFor(`${text("#viewer")}.startsWith("consumer-b") && ${text("#inspector")}.includes(${JSON.stringify(denyRun.task_id)})`, 20000), denyRun.task_id);
    await views("Deny", denyRun.task_id, "denied", "Deny", "POLICY_DECIDED", ["Deny", "INSUFFICIENT_BUDGET"]);
    check("3.5 Deny: its line: 場次完成；Deny：INSUFFICIENT_BUDGET, no cancel", await lineHas(denyRun.task_id, ["場次完成", "Deny：INSUFFICIENT_BUDGET"], false, 20000), await b.evaluate(seenLine(denyRun.task_id)));
    await record("Deny", denyRun.task_id);
    await shot(b, "desktop-acceptance-deny", 1280, 900, false);
    await shotFull(b, "full-acceptance-deny");

    // 4. Stop a running run (ask-wait): 停止 only, then its own cancel.
    const waitRun = await startNew("ask-wait");
    check("3.5 stop: a running run offers 停止, not 取消 task", await b.waitFor(`${seenLine(waitRun.task_id)}.includes("進行中") && !!document.querySelector('#runs li[data-live="running"] .run-stop') && !${cancelBtn(waitRun.task_id)}`, 20000), await b.evaluate(seenLine(waitRun.task_id)));
    await views("running", waitRun.task_id, "waiting", "Ask", "POLICY_DECIDED", ["Ask", "pending"]);
    await b.evaluate(`document.querySelector('#runs li[data-live="running"] .run-stop').click()`);
    check("3.5 stop: 已停止，取消已記錄 (after the record was written)", await lineHas(waitRun.task_id, ["已停止，取消已記錄"], false, 20000), await b.evaluate(seenLine(waitRun.task_id)));
    // Review of 31cc6c7: after the 202 the line above is a neutral notice, not a lasting 停止中 nor a claim of the result.
    const stopNote = await b.evaluate(`JSON.stringify({ text: document.getElementById("run-status").innerText, state: document.getElementById("run-status").dataset.state })`);
    check("3.5 stop: after the 202 a neutral notice (accepted; see the runs list), not 停止中 and not a claimed result",
      stopNote === JSON.stringify({ text: "場次 " + waitRun.run_id + " 的停止請求已受理；最終結果請看最近場次。", state: "idle" }) && !/停止中|已停止|已取消|取消已記錄/.test(JSON.parse(stopNote).text), stopNote);
    await views("stop", waitRun.task_id, "withdrawn", "Ask", "TASK_CANCELLED", ["cancelled", "withdrawn"]);
    await record("stop", waitRun.task_id);
    await shot(b, "desktop-acceptance-stop", 1280, 900, false);
    await shotFull(b, "full-acceptance-stop");

    // 4. Cancel a task after its run ended.
    const cRun = await startNew("ask");
    await lineHas(cRun.task_id, ["場次完成", "Ask 等待中；task 可由主持人取消"], true, 20000);
    await b.evaluate(`${cancelBtn(cRun.task_id)}.click()`);
    check("3.5 cancel: task 已取消, its line task 現已取消", await b.waitFor(`document.getElementById("run-status").innerText === ${JSON.stringify("task " + cRun.task_id + " 已取消")}`, 15000) && await lineHas(cRun.task_id, ["task 現已取消"], false), await runSeen());
    await views("cancel", cRun.task_id, "withdrawn", "Ask", "TASK_CANCELLED", ["cancelled", "withdrawn"]);
    await record("cancel", cRun.task_id);
    // D1 replay of the cancelled task (#4 withdrawn, #5 cancelled), held step by step: nothing early.
    await b.evaluate(`(() => { window.__d1Orig = window.__d1Orig || window.apaUnityReport; window.__d1Held = []; window.apaUnityReport = (kind, gen, seq, detail) => { if (kind === "rendered") { window.__d1Held.push([kind, gen, seq, detail]); return; } return window.__d1Orig(kind, gen, seq, detail); }; return true; })()`);
    await b.evaluate(`document.getElementById("replay").click()`);
    const stepAt = (n) => b.waitFor(`${shown(".card-replay")} === "回放當步 #${n}／共 5（唯讀）"`, 15000);
    check("D1 replay #3 (cancelled task): Ask waiting; no withdrawal, no cancel yet", await stepAt(3) && (await b.evaluate(shown(".card-status"))) === "Ask：等待人工核准（M1 無核准能力）"
      && !/撤回|已取消/.test(await b.evaluate(shown("#card"))), await b.evaluate(shown("#card")));
    check("D1 replay #4: withdrawn; the task not yet cancelled", await stepAt(4) && (await b.evaluate(shown(".card-status"))) === "Ask 已撤回（task 取消）"
      && !(await b.evaluate(shown(".card-task"))).includes("task 已取消"), await b.evaluate(shown("#card")));
    check("D1 replay #5: now the task is cancelled", await stepAt(5) && (await b.evaluate(shown(".card-task"))).includes("task 已取消"), await b.evaluate(shown("#card")));
    await b.evaluate(`(() => { window.apaUnityReport = window.__d1Orig; window.__d1Held.splice(0).forEach((a) => window.__d1Orig(...a)); return true; })()`);
    await b.waitFor(`document.getElementById("status").dataset.mode === "live" && document.getElementById("status").dataset.kind === "ok"`, 40000);
    await shot(b, "desktop-acceptance-cancel", 1280, 900, false);
    await shotFull(b, "full-acceptance-cancel");

    // 4. The first Ask reaches its deadline (30 s, the server's clock): the backend records the expiry.
    let expired = false;
    for (let i = 0; i < 200 && !expired; i++) { expired = (await aEvents(askRun.task_id)).includes("APPROVAL_EXPIRED"); if (!expired) await sleep(250); }
    const expiredAt = (await aTask(askRun.task_id)).served_at;
    check("3.5 expiry: the backend recorded APPROVAL_EXPIRED at or after the deadline", expired && expiredAt >= askDeadline, "deadline " + askDeadline + ", seen at " + expiredAt);
    check("3.5 expiry: its line: Ask 已到期；task 仍可由主持人取消", await lineHas(askRun.task_id, ["Ask 已到期；task 仍可由主持人取消"], true, 15000), await b.evaluate(seenLine(askRun.task_id)));
    // 5. Switch consumer and task, then a replay.
    await b.evaluate(`document.querySelector('#consumer-pick button[data-consumer="consumer-a"]').click()`);
    await b.waitFor(`${text("#viewer")}.startsWith("consumer-a")`, 10000);
    await b.evaluate(`document.getElementById("task-id").value = ${JSON.stringify(askRun.task_id)}; document.querySelector("#task-form button[type=submit]").click()`);
    await views("expiry", askRun.task_id, "expired", "Ask", "APPROVAL_EXPIRED", ["expired"]);
    await record("expiry", askRun.task_id);
    await shot(b, "desktop-acceptance-expired", 1280, 900, false);
    await shotFull(b, "full-acceptance-expired");
    await b.evaluate(`document.querySelector('#consumer-pick button[data-consumer="consumer-b"]').click()`);
    check("3.5 switch: consumer-b shows nothing of consumer-a", await b.waitFor(`${text("#viewer")}.startsWith("consumer-b") && !${text("#inspector")}.includes(${JSON.stringify(askRun.task_id)})`, 10000)
      && !(await b.evaluate(`[...document.querySelectorAll("#task-pick button")].some((x) => x.dataset.task.startsWith("show-a"))`)));
    await b.evaluate(`document.querySelector('#consumer-pick button[data-consumer="consumer-a"]').click()`);
    await b.waitFor(`${text("#viewer")}.startsWith("consumer-a")`, 10000);
    await b.evaluate(`document.getElementById("task-id").value = ${JSON.stringify(askRun.task_id)}; document.querySelector("#task-form button[type=submit]").click()`);
    await b.waitFor(`document.getElementById("status").dataset.kind === "ok" && ${text("#inspector")}.includes(${JSON.stringify(askRun.task_id)})`, 15000);
    const beforeReplay = (await aEvents(askRun.task_id)).length;
    await b.evaluate(`document.getElementById("replay").click()`);
    check("3.5 replay: read only, then back to the latest snapshot", await b.waitFor(`document.getElementById("status").dataset.mode === "replay"`, 5000)
      && await b.waitFor(`document.getElementById("status").dataset.mode === "live" && document.getElementById("status").dataset.kind === "ok"`, 60000)
      && (await aEvents(askRun.task_id)).length === beforeReplay);
    await views("after replay", askRun.task_id, "expired", "Ask", "APPROVAL_EXPIRED", ["expired"]);

    // 6. Errors: a failed detail read is unconfirmed (CDP: the browser's network layer answers 503;
    // a simulation, not a Store fault), then the real state; a cancel answered 503 before it reaches
    // the server claims nothing; the next cancel reads the committed state.
    let failAccDetail = false, refuseAcc = 0;
    const offAcc = b.on("Fetch.requestPaused", (p) => {
      const fake503 = () => b.send("Fetch.fulfillRequest", { requestId: p.requestId, responseCode: 503, responseHeaders: [{ name: "Content-Type", value: "application/json" }], body: Buffer.from('{"error":"store_unavailable","code":"STORE_ERROR"}').toString("base64") }).catch(() => {});
      if (failAccDetail && p.request.url.endsWith("/api/tasks/" + askRun.task_id) && p.request.headers.Authorization === "Bearer " + at.host) return fake503();
      if (refuseAcc > 0 && p.request.url.endsWith("/cancel")) { refuseAcc--; return fake503(); }
      b.send("Fetch.continueRequest", { requestId: p.requestId }).catch(() => {});
    });
    await b.send("Fetch.enable", { patterns: [{ urlPattern: "*/api/tasks/*", requestStage: "Request" }, { urlPattern: "*/cancel", requestStage: "Request" }] });
    failAccDetail = true;
    check("3.5 error: a failed detail read: Ask 狀態未確認", await lineHas(askRun.task_id, ["Ask 狀態未確認（task 詳情未取得）"], true, 15000), await b.evaluate(seenLine(askRun.task_id)));
    failAccDetail = false;
    check("3.5 error: recovered: the real state again", await lineHas(askRun.task_id, ["Ask 已到期；task 仍可由主持人取消"], true, 15000), await b.evaluate(seenLine(askRun.task_id)));
    refuseAcc = 1;
    await b.evaluate(`${cancelBtn(askRun.task_id)}.click()`);
    check("3.5 error: a cancel answered 503: 取消結果尚未確認, nothing claimed, nothing written", await b.waitFor(`document.getElementById("run-status").innerText.includes("取消結果尚未確認")`, 15000)
      && !(await aEvents(askRun.task_id)).includes("TASK_CANCELLED") && await lineHas(askRun.task_id, ["Ask 已到期；task 仍可由主持人取消"], true), await runSeen());
    await b.send("Fetch.disable");
    offAcc();
    await b.evaluate(`${cancelBtn(askRun.task_id)}.click()`);
    check("3.5 error: then a cancel through: task 已取消, read from the Store", await b.waitFor(`document.getElementById("run-status").innerText === ${JSON.stringify("task " + askRun.task_id + " 已取消")}`, 15000) && await lineHas(askRun.task_id, ["task 現已取消"], false), await runSeen());
    await views("expired then cancelled", askRun.task_id, "expired", "Ask", "TASK_CANCELLED", ["expired", "cancelled"]);
    await record("expired then cancelled", askRun.task_id);

    // Review of 7ffc45d (C1): a page that stays hidden polls nothing (task,
    // readiness, runs); made visible again it reads at once and shows what
    // changed meanwhile. document.hidden is overridden by the test (the
    // browser's own visibility is not what is under test here).
    await b.evaluate(`(() => { window.__hid = false; Object.defineProperty(document, "hidden", { configurable: true, get: () => window.__hid === true }); window.__hc = { status: 0, runs: 0 }; const f = window.fetch.bind(window);
      window.fetch = (u, o) => { const s = String(u); if (s === "/api/host/status") window.__hc.status++; if (s === "/api/host/runs" && !(o && o.method === "POST")) window.__hc.runs++; return f(u, o); }; return true; })()`);
    await b.evaluate(`(window.__hid = true, document.dispatchEvent(new Event("visibilitychange")), true)`);
    await sleep(2500); // anything in flight ends
    const h0 = JSON.parse(await b.evaluate(`JSON.stringify({ polls: Number(document.getElementById("status").dataset.polls || 0), ...window.__hc, checked: (document.querySelector('#readiness tr[data-item="檢查時間"] td') || {}).textContent })`));
    const whileHidden = await hostPost(aAddr, at.op, "/api/host/runs", { request_id: "req-hidden-0000000001", scenario: "ask" });
    await sleep(7000);
    const h1 = JSON.parse(await b.evaluate(`JSON.stringify({ polls: Number(document.getElementById("status").dataset.polls || 0), ...window.__hc })`));
    check("C1 hidden: 7 s hidden, no task poll, no readiness read, no runs read", h1.polls === h0.polls && h1.status === h0.status && h1.runs === h0.runs, JSON.stringify([h0, h1]));
    check("C1 hidden: the run started meanwhile is not shown yet", !(await b.evaluate(`[...document.querySelectorAll("#runs li")].some((l) => l.textContent.startsWith(${JSON.stringify(whileHidden.json.task_id + "（")}))`)));
    await b.evaluate(`(window.__hid = false, document.dispatchEvent(new Event("visibilitychange")), true)`);
    check("C1 visible again: task, readiness and runs are read at once", await b.waitFor(`Number(document.getElementById("status").dataset.polls || 0) > ${h0.polls} && window.__hc.status > ${h0.status} && window.__hc.runs > ${h0.runs}`, 3000),
      await b.evaluate(`JSON.stringify(window.__hc)`));
    check("C1 visible again: what changed meanwhile shows (the new run, a new readiness time)", await b.waitFor(`[...document.querySelectorAll("#runs li")].some((l) => l.textContent.startsWith(${JSON.stringify(whileHidden.json.task_id + "（")})) && (document.querySelector('#readiness tr[data-item="檢查時間"] td') || {}).textContent !== ${JSON.stringify(h0.checked)}`, 5000));
    await b.waitFor(`${seenLine(whileHidden.json.task_id)}.includes("場次完成")`, 20000);

    // 7. Restart on the same database: one Ask left waiting, one run still running (the orderly
    // shutdown cancels it). Afterwards the history and tasks stay, nothing reruns, and the waiting
    // Ask is cancelled under the usual rules.
    const pending = await startNew("ask");
    await lineHas(pending.task_id, ["Ask 等待中；task 可由主持人取消"], true, 20000);
    const running = await startNew("ask-wait");
    await b.waitFor(`${seenLine(running.task_id)}.includes("進行中")`, 20000);
    await b.waitFor(`document.getElementById("status").dataset.kind === "ok" && ${text("#inspector")}.includes(${JSON.stringify(running.task_id)})`, 20000);
    const beforeRestart = {};
    for (const r of await aRuns()) beforeRestart[r.task_id] = await aEvents(r.task_id);
    const runsBefore = (await aRuns()).map((r) => r.run_id).join();
    await ad.stop();
    ad = startDemo(accState, null, unityBuild, accArgs, aAddr);
    await ad.ready();
    at = ad.tokens();
    allTokens.push(at.a, at.b, at.host, at.op);
    check("3.5 restart: the page's credentials are cleared (old tokens 401)", await b.waitFor(`document.getElementById("creds").dataset.mode === "none"`, 20000));
    check("3.5 restart: nothing reruns, no run interrupted (an orderly shutdown)", !ad.log().includes("demo: run ") && !ad.log().includes("marked interrupted"), ad.log().split("\n").filter((l) => l.includes("run")).join(" | ").slice(0, 200));
    writeFileSync(aFile, readFileSync(join(accState, "tokens.json")));
    await chooseFile(aFile);
    await b.waitFor(`document.getElementById("creds").dataset.mode === "file"`);
    const runsAfter = (await aRuns()).map((r) => r.run_id).join();
    let kept = runsAfter === runsBefore;
    // The running run's task gets its shutdown cancel; the waiting Ask may meet its deadline: both only append.
    for (const [task, ev] of Object.entries(beforeRestart)) {
      const now = await aEvents(task);
      if (task === running.task_id || task === pending.task_id || task === whileHidden.json.task_id ? now.slice(0, ev.length).join() !== ev.join() : now.join() !== ev.join()) kept = false;
    }
    check("3.5 restart: the same runs and the same events (only the running run's cancel or the waiting Ask's expiry appended)", kept, runsBefore + " -> " + runsAfter);
    check("3.5 restart: the run that was running ended with its cancel at the shutdown", await lineHas(running.task_id, ["已停止，取消已記錄"], false, 20000) && (await aEvents(running.task_id)).at(-1) === "TASK_CANCELLED", await b.evaluate(seenLine(running.task_id)));
    check("3.5 restart: the history lines read as before", await lineHas(denyRun.task_id, ["場次完成", "Deny：INSUFFICIENT_BUDGET"], false) && await lineHas(waitRun.task_id, ["已停止，取消已記錄"], false)
      && await lineHas(cRun.task_id, ["task 現已取消"], false) && await lineHas(askRun.task_id, ["task 現已取消"], false));
    await b.evaluate(`${cancelBtn(pending.task_id)}.click()`);
    check("3.5 restart: the Ask left waiting is cancelled afterwards", await b.waitFor(`document.getElementById("run-status").innerText === ${JSON.stringify("task " + pending.task_id + " 已取消")}`, 15000) && await lineHas(pending.task_id, ["task 現已取消"], false), await runSeen());
    await b.evaluate(`document.querySelector('#consumer-pick button[data-consumer="consumer-a"]').click()`);
    await b.waitFor(`${text("#viewer")}.startsWith("consumer-a")`, 10000);
    await b.evaluate(`document.getElementById("task-id").value = ${JSON.stringify(pending.task_id)}; document.querySelector("#task-form button[type=submit]").click()`);
    const pendingEv = await aEvents(pending.task_id);
    await views("after restart", pending.task_id, pendingEv.includes("APPROVAL_EXPIRED") ? "expired" : "withdrawn", "Ask", "TASK_CANCELLED", ["cancelled"]);
    await record("restart: shutdown cancel", running.task_id);
    await record("restart: cancelled afterwards", pending.task_id);
    await shot(b, "desktop-acceptance-restart", 1280, 900, false);
    await shotFull(b, "full-acceptance-restart");
    check("3.5: nothing on the page claims payment or delivery", !(await b.evaluate(`document.body.innerText`)).match(/付款成功|已付款|取貨成功|交付成功|Settled|RESOURCE_DELIVERED/));
    const keptEnd = await b.evaluate(`JSON.stringify({ ls: localStorage.length, ss: sessionStorage.length, cookie: document.cookie, url: location.href })`);
    check("3.5: no token in storage, cookie, URL or console", !/"ls":[1-9]|"ss":[1-9]|"cookie":"[^"]/.test(keptEnd) && allTokens.every((t) => !keptEnd.includes(t) && !b.consoleText.join("\n").includes(t)));
    log("  3.5 run / task / events:");
    for (const m of mapping) log("    " + m);
    await ad.stop();
  }

  // No Unity build: the page says so and shows no other shop.
  const bare = startDemo(join(tmp, "state-bare"), null, join(tmp, "no-build"));
  const addr3 = await bare.ready();
  await b.send("Page.navigate", { url: "http://" + addr3 + "/" });
  check("without a build: Unity 場景未就緒", await b.waitFor(`document.getElementById("unity-status").dataset.state === "failed" && ${text("#unity-status")} === "Unity 場景未就緒"`, 20000));
  await shot(b, "desktop-unity-missing", 1280, 900, false);
  await bare.stop();
  allTokens.push(...Object.values(bare.tokens()));
}
} catch (e) {
  check("browser test ran to the end", false, String(e && e.message || e).slice(0, 300));
} finally {
  if (browser) browser.close();
  if (demo) await demo.stop().catch(() => {});
}

// Evidence: the assertions and screenshots. No token may be in any of them.
mark("end");
log("result: " + (failures === 0 ? "PASS" : failures + " FAILED"));
writeFileSync(join(out, "browser-test.log"), lines.join("\n") + "\n");
const leaks = readdirSync(out).filter((f) => { const s = readFileSync(join(out, f)).toString("latin1"); return allTokens.some((t) => t && s.includes(t)); });
if (leaks.length) { console.error("TOKEN IN EVIDENCE: " + leaks.join(", ")); process.exit(2); }
console.log("evidence: " + out);
process.exit(failures === 0 ? 0 : 1);
