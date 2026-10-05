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
  const on = (method, fn) => listeners.set(method, [...(listeners.get(method) || []), fn]);
  return { c, send, evaluate, waitFor, on, consoleText, close: () => { ws.close(); c.kill(); } };
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
  const pa = (await hostGet(addr, tok.host, "/api/tasks/task-a")).json.purchases[0];
  for (let t = (await hostGet(addr, tok.host, "/api/tasks/task-a")).json.served_at; t < pa.deadline_at - 1; t = (await hostGet(addr, tok.host, "/api/tasks/task-a")).json.served_at) await sleep(250);
  const seqAtStart = await hostEvents(addr, tok.host, "task-a");
  await waitPolls(b, 1); // a poll just completed: its next timer is pending
  const g0A = await unityGen(b);
  await b.evaluate(`document.getElementById("replay").click()`);
  let replaySamples = [];
  for (let i = 0; i < 12; i++) { await sleep(250); replaySamples.push(await b.evaluate(`document.getElementById("status").dataset.mode + "|" + document.getElementById("status").textContent`)); }
  check("R2: during the replay nothing switches back to 已同步", replaySamples.every((x) => x.startsWith("replay|") && !x.includes("已同步")), replaySamples[replaySamples.length - 1]);
  check("R2: replay note says the panels are not the shop's moment", await b.evaluate(visible("#replay-note")));
  check("R2: Unity reports a replay step in a new generation", await b.waitFor(`Number(${uKey("gen")}) > ${g0A}`, 20000));
  const replayGenA = await unityGen(b);
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

  // R1: a task with two purchases, written through the real Store
  // (internal/store TestSeedDisplayDB): purchase 1 Ask then expired,
  // purchase 2 an open Ask. The scene follows purchase 2.
  const seeded = join(tmp, "state-seeded");
  mkdirSync(seeded, { recursive: true, mode: 0o700 });
  execFileSync("go", ["test", "./internal/store", "-run", "TestSeedDisplayDB", "-count=1"], { cwd: repo, env: { ...process.env, APA_SEED_DB: join(seeded, "demo.db") }, stdio: "ignore" });
  const sd = startDemo(seeded, null, unityBuild);
  const addr4 = await sd.ready();
  const st = sd.tokens();
  allTokens.push(st.a, st.b, st.host);
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
  await sd.stop();

  // ---- B1-06 slice 3.1: the host's page /host (docs/demo-operation-design.md 6.3) ----
  // A fresh host with the fixed plan (task-a Ask, task-b Deny) on a fixed
  // address, so a restart on the same address makes the page's tokens 401.
  const hostState = join(tmp, "state-host");
  let hd = startDemo(hostState, join(repo, "testdata/demo/fake-plan.json"), unityBuild);
  const hAddr = await hd.ready("task-b");
  let ht = hd.tokens();
  allTokens.push(ht.a, ht.b, ht.host);
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
  b.on("Fetch.requestPaused", (p) => {
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
  holdNext = 1;
  check("3.1 pagehide: a readiness poll is held", await holdOne(8000)); // the next 5 s poll
  await b.evaluate(`window.dispatchEvent(new PageTransitionEvent("pagehide", { persisted: true })), true`);
  await release();
  check("3.1 pagehide: tokens dropped, Dashboard and shop cleared; the late poll restores nothing", (await credsMode()) === "none" && (await b.evaluate(`${text("#viewer")} === "未驗證"`)) && (await noTaskShown()), await credsView());
  holdNext = 0;
  let polled = 0;
  b.on("Fetch.requestPaused", () => { polled++; });
  await sleep(6000);
  check("3.1 pagehide: no more polling after leaving", polled === 0, polled + " requests");
  await b.evaluate(`window.dispatchEvent(new PageTransitionEvent("pageshow", { persisted: true })), true`);
  await sleep(500);
  check("3.1 pageshow: nothing comes back by itself", (await credsMode()) === "none" && (await b.evaluate(`${text("#viewer")} === "未驗證"`)));
  await b.send("Fetch.disable");

  // Leave for real and come back (bfcache or reload): still nothing held.
  await chooseFile(goodFile);
  await b.waitFor(`document.getElementById("creds").dataset.mode === "file"`);
  await b.send("Page.navigate", { url: "about:blank" });
  await sleep(500);
  const hist = await b.send("Page.getNavigationHistory");
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
  allTokens.push(ht.a, ht.b, ht.host);
  check("3.1: after a restart the page's 401 clears every credential", await b.waitFor(`document.getElementById("creds").dataset.mode === "none" && document.getElementById("creds").textContent.includes("服務已重啟")`, 20000) && (await noTaskShown()), await credsView());
  await chooseFile(goodFile); // the file as it was before the restart
  check("3.1: the pre-restart file is refused", await b.waitFor(`document.getElementById("creds").textContent.includes("與目前服務不符")`));
  writeFileSync(goodFile, readFileSync(join(hostState, "tokens.json")));
  await chooseFile(goodFile);
  check("3.1: the new file loads", await b.waitFor(`document.getElementById("creds").dataset.mode === "file"`));
  await hd.stop();

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
