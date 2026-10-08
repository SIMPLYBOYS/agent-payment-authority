// Real-browser check of the history replay (docs/cli-poc.md 54): the
// operator's start script (scripts/demo/history.sh) on a copy, the real
// Unity Web build in Chrome, the page, the host page and the APIs. Local
// only, like browser-ui.mjs: the installed Google Chrome over the DevTools
// protocol (no packages), a throwaway profile, no HAR or network log; only
// the assertions and full-page screenshots are kept, and every kept file is
// checked for token values.
//
//   node scripts/verify/history-ui.mjs [--evidence DIR] [--unity-build DIR] [--out DIR]
//
// Without --evidence: synthetic data only. A fake run (task-a Ask, task-b
// Deny) is recorded as an evidence directory of its own, the start script's
// refusals are tried on throwaway copies of it, then it is served. With
// --evidence: that directory (db/apa-live.db and one runs/*.jsonl) is served
// through the start script; only those two files and the names in db/ and
// runs/ are read, before and after, to show nothing changed. It runs no
// Claude CLI and makes no payment. Exit 0: every check passed.

import { spawn, execFileSync } from "node:child_process";
import { mkdtempSync, mkdirSync, readFileSync, writeFileSync, readdirSync, statSync, chmodSync, copyFileSync, existsSync } from "node:fs";
import { createHash } from "node:crypto";
import { tmpdir } from "node:os";
import { join, resolve, basename } from "node:path";
import { request as httpRequest } from "node:http";

const repo = resolve(new URL("../..", import.meta.url).pathname);
const arg = (k, d) => { const i = process.argv.indexOf(k); return i > 0 ? process.argv[i + 1] : d; };
const unityBuild = resolve(arg("--unity-build", join(repo, "unity/AgentPaymentAuthority/Build/WebGL/Build")));
const given = arg("--evidence", null);
const stamp = new Date().toISOString().replace(/[-:]/g, "").replace(/\.\d+Z$/, "Z");
const commit = execFileSync("git", ["-C", repo, "rev-parse", "HEAD"]).toString().trim();
const dirty = execFileSync("git", ["-C", repo, "status", "--porcelain", "--untracked-files=no"]).toString().trim() !== "";
const out = resolve(arg("--out", join(repo, "artifacts/b1-04", "history-ui-" + commit.slice(0, 12) + "-" + stamp + (given ? "-evidence" : "-synthetic"))));
mkdirSync(out, { recursive: true });
const CHROME = "/Applications/Google Chrome.app/Contents/MacOS/Google Chrome";
const LABEL = "真實執行紀錄・歷史回放・未付款・開發展示例外";

const lines = [];
let failures = 0;
const log = (s) => { lines.push(s); console.log(s); };
const check = (name, ok, detail = "") => { log((ok ? "PASS " : "FAIL ") + name + (detail ? "  [" + detail + "]" : "")); if (!ok) failures++; };
const sleep = (ms) => new Promise((r) => setTimeout(r, ms));
const sha = (p) => createHash("sha256").update(readFileSync(p)).digest("hex");

const tmp = mkdtempSync(join(tmpdir(), "apa-history-ui-"));
chmodSync(tmp, 0o700);
const copies = join(tmp, "copies"); // APA_HISTORY_DIR: the start script's copy directories
const allTokens = [];
log("commit " + commit + (dirty ? " (working tree has uncommitted changes)" : "") + "; " + (given ? "evidence " + given : "synthetic data"));

// ---- the operator's start script ----
function startScript(ev) {
  const env = { PATH: process.env.PATH, HOME: process.env.HOME, APA_ADDR: "127.0.0.1:0", APA_HISTORY_DIR: copies, GOCACHE: process.env.GOCACHE || "" };
  if (!env.GOCACHE) delete env.GOCACHE;
  const p = spawn("sh", [join(repo, "scripts/demo/history.sh"), ev], { cwd: tmp, env, stdio: ["ignore", "pipe", "pipe"] });
  let text = "";
  p.stdout.on("data", (b) => { text += b; });
  p.stderr.on("data", (b) => { text += b; });
  const exited = new Promise((r) => p.once("exit", (code) => r(code)));
  return {
    p, exited, text: () => text,
    async ready() {
      for (let i = 0; i < 1200; i++) {
        const m = text.match(/listening on http:\/\/(\S+)/);
        const w = text.match(/history: copy in (\S+);/);
        if (m && w) return { addr: m[1], copy: w[1] };
        if (p.exitCode !== null) throw new Error("the start script ended: " + text.slice(0, 600));
        await sleep(100);
      }
      throw new Error("the history host did not start: " + text.slice(0, 600));
    },
    async stop() { if (p.exitCode === null) { p.kill("SIGINT"); } return exited; },
  };
}
const tokensOf = (copy) => {
  const t = JSON.parse(readFileSync(join(copy, "tok", "tokens.json"))).tokens;
  allTokens.push(...t.map((x) => x.token));
  const get = (role, c) => (t.find((x) => x.role === role && (x.consumer_id || "") === c) || {}).token;
  return { a: get("viewer", "consumer-a"), b: get("viewer", "consumer-b"), host: get("host", ""), op: get("operator", "") };
};

// What may not change: the two files and the names (with size and time) in
// db/ and runs/. Nothing else of the evidence directory is read.
function stateOf(ev) {
  const names = (d) => readdirSync(join(ev, d)).sort().map((n) => { const s = statSync(join(ev, d, n)); return d + "/" + n + " " + s.size + " " + s.mtimeMs; });
  const trace = readdirSync(join(ev, "runs")).filter((n) => n.endsWith(".jsonl"));
  return JSON.stringify({ db: sha(join(ev, "db", "apa-live.db")), trace: trace.map((n) => sha(join(ev, "runs", n))), names: [...names("db"), ...names("runs")] });
}

async function api(addr, token, path) {
  const r = await fetch("http://" + addr + path, { headers: { Authorization: "Bearer " + token } });
  return { status: r.status, json: await r.json().catch(() => null) };
}
function post(addr, token, path, body) {
  return new Promise((res, rej) => {
    const [hostname, port] = addr.split(":");
    const data = JSON.stringify(body);
    const req = httpRequest({ hostname, port, path, method: "POST", headers: { Authorization: "Bearer " + token, Origin: "http://" + addr,
      "Content-Type": "application/json", "Content-Length": Buffer.byteLength(data) } }, (r) => {
      let t = "";
      r.on("data", (c) => { t += c; });
      r.on("end", () => res({ status: r.statusCode, json: JSON.parse(t || "{}") }));
    });
    req.on("error", rej);
    req.end(data);
  });
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
// One reading of everything the page shows, taken at one instant.
const reading = `JSON.stringify({ badge: ${text("header .badge")}, chip: ${text("#mode-chip")}, status: ${text("#status")},
  note: ${visible("#history-note")}, replayNote: ${visible("#replay-note")}, inspectorNote: ${visible("#inspector-note")} ? ${text("#inspector-note")} : "",
  unity: { state: ${uKey("state")}, task: ${uKey("task")}, seq: ${uKey("seq")}, phase: ${uKey("phase")} }, scene: ${text("#scene-text")},
  card: ${text("#card")}, evidence: ${text("#evidence")}, inspector: ${text("#inspector")},
  timeline: [...document.querySelectorAll("#timeline li")].map((li) => li.dataset.seq).join(","), token: document.getElementById("token")?.value || "" })`;

async function shot(b, name, r) {
  check("screenshot " + name + ": token field empty", r.token === "");
  const png = await b.send("Page.captureScreenshot", { format: "png", captureBeyondViewport: false });
  writeFileSync(join(out, name + ".png"), Buffer.from(png.data, "base64"));
  log("  " + name + ".png (full page): task " + r.unity.task + ", Unity #" + r.unity.seq + " " + r.unity.phase + ", Timeline " + r.timeline + ", status「" + r.status + "」");
}
async function tall(b) {
  const h = Math.min(4000, await b.evaluate(`Math.ceil(document.documentElement.scrollHeight)`));
  await b.send("Emulation.setDeviceMetricsOverride", { width: 1280, height: h, deviceScaleFactor: 1, mobile: false });
  await sleep(800);
}
async function signIn(b, token) {
  await b.evaluate(`(document.getElementById("token").closest("details")?.setAttribute("open", ""), document.getElementById("token").focus())`);
  await b.send("Input.insertText", { text: token }); // into the masked field; never in an expression or a log
  await b.evaluate(`document.querySelector("#auth-form button[type=submit]").click()`);
}
async function openTask(b, id) {
  await b.waitFor(`[...document.querySelectorAll("#task-pick button")].some(x => x.textContent.startsWith(${JSON.stringify(id + "（")}))`);
  await b.evaluate(`[...document.querySelectorAll("#task-pick button")].find(x => x.textContent.startsWith(${JSON.stringify(id + "（")}))?.click()`);
  return b.waitFor(`document.getElementById("status").dataset.kind === "ok"`);
}

// ---- synthetic evidence: a fake run recorded like the real one ----
function recordSynthetic() {
  const bin = join(tmp, "bin");
  mkdirSync(bin);
  execFileSync("go", ["build", "-o", join(bin, "demo"), "./cmd/demo"], { cwd: repo, stdio: "inherit" });
  execFileSync("go", ["build", "-o", join(bin, "fake-claude"), "./cmd/fake-claude"], { cwd: repo, stdio: "inherit" });
  const st = join(tmp, "fake-state");
  mkdirSync(st, { mode: 0o700 });
  return new Promise((res, rej) => {
    const env = { PATH: process.env.PATH, HOME: process.env.HOME, APA_ADDR: "127.0.0.1:0", APA_DB_PATH: join(st, "demo.db"), APA_DEMO_TOKEN_FILE: join(st, "tokens.json") };
    const p = spawn(join(bin, "demo"), ["--trace-dir", join(st, "runs"), "--fake-cli", join(bin, "fake-claude"), "--plan", join(repo, "testdata/demo/fake-plan.json"),
      "--unity-build", join(tmp, "no-build")], { cwd: repo, env, stdio: ["ignore", "ignore", "pipe"] });
    let t = "";
    p.stderr.on("data", (c) => { t += c; if (t.includes("fake run task-b")) p.kill("SIGINT"); });
    p.once("exit", () => {
      if (!t.includes("fake run task-b") || existsSync(join(st, "demo.db-wal"))) return rej(new Error("the fake run was not recorded: " + t.slice(0, 400)));
      const ev = join(tmp, "synthetic-evidence");
      mkdirSync(join(ev, "db"), { recursive: true, mode: 0o700 });
      mkdirSync(join(ev, "runs"), { mode: 0o700 });
      copyFileSync(join(st, "demo.db"), join(ev, "db", "apa-live.db"));
      const tr = readdirSync(join(st, "runs")).filter((n) => n.startsWith("task-a-") && n.endsWith(".jsonl"));
      copyFileSync(join(st, "runs", tr[0]), join(ev, "runs", tr[0]));
      res(ev);
    });
  });
}

// A throwaway variant of the evidence for one refusal.
function variant(ev, name, change) {
  const v = join(tmp, "variant-" + name);
  mkdirSync(join(v, "db"), { recursive: true, mode: 0o700 });
  mkdirSync(join(v, "runs"), { mode: 0o700 });
  copyFileSync(join(ev, "db", "apa-live.db"), join(v, "db", "apa-live.db"));
  for (const n of readdirSync(join(ev, "runs"))) copyFileSync(join(ev, "runs", n), join(v, "runs", n));
  change(v);
  return v;
}

let browser = null;
let host = null;
try {
  const ev = given ? resolve(given) : await recordSynthetic();
  const before = stateOf(ev);

  if (!given) {
    // The start script stops on each of these before anything is served;
    // the host refuses the last three. Only throwaway copies are changed.
    const traceName = readdirSync(join(ev, "runs"))[0];
    const tr = readFileSync(join(ev, "runs", traceName), "utf8");
    const refusals = {
      "no database": (v) => execFileSync("mv", [join(v, "db", "apa-live.db"), join(v, "db", "gone.db")]),
      "a -wal beside the database": (v) => writeFileSync(join(v, "db", "apa-live.db-wal"), ""),
      "two traces": (v) => writeFileSync(join(v, "runs", "second.jsonl"), tr),
      "no trace": (v) => execFileSync("mv", [join(v, "runs", traceName), join(v, "runs", "trace.txt")]),
      "trace and database do not match": (v) => writeFileSync(join(v, "runs", traceName), tr.replaceAll('"task_id":"task-a"', '"task_id":"task-z"')),
      "a tool outside the whitelist": (v) => writeFileSync(join(v, "runs", traceName), tr.replace('"tool":"list_products"', '"tool":"Bash"')),
      "a damaged database": (v) => { const p = join(v, "db", "apa-live.db"); const raw = readFileSync(p); raw.fill(0, raw.length >> 1); writeFileSync(p, raw); },
    };
    for (const [name, change] of Object.entries(refusals)) {
      const v = variant(ev, name.replace(/\W+/g, "-"), change);
      const s = startScript(v);
      const code = await Promise.race([s.exited, sleep(120000).then(() => "timeout")]);
      if (code === "timeout") await s.stop();
      check("refused: " + name, code !== 0 && code !== "timeout" && !s.text().includes("listening on"), "exit " + code + ": " + s.text().trim().split("\n").pop().slice(0, 160));
    }
  }

  host = startScript(ev);
  const { addr, copy } = await host.ready();
  const tok = tokensOf(copy);
  log("history host at " + addr + " serving a copy in " + basename(copy));
  const evs = await api(addr, tok.host, "/api/tasks/task-a/events");
  const types = evs.json.events.map((e) => e.event_type).join(",");
  check("the copy holds the three recorded events of task-a", types === "TASK_STARTED,PURCHASE_PROPOSED,POLICY_DECIDED", types);
  const task = (await api(addr, tok.host, "/api/tasks/task-a")).json;
  const po = task.purchases[0].purchase_order_id;
  const intent = task.purchases[0].intent_hash;
  check("task-a: awaiting_approval, one Ask pending, nothing paid", task.status === "awaiting_approval" && task.purchases.length === 1
    && task.purchases[0].decision.policy_verdict === "Ask" && task.purchases[0].decision.approval_status === "pending" && task.purchases[0].decision.payment_status === "unavailable");

  // Server: no control writes, whoever asks; the trace only to its task's readers.
  for (const [who, t] of [["operator", tok.op], ["host", tok.host]]) {
    const r1 = await post(addr, t, "/api/host/runs", { request_id: "req-history-ui-0001", scenario: "ask" });
    const r2 = await post(addr, t, "/api/host/tasks/task-a/cancel", {});
    const r3 = await post(addr, t, "/api/host/runs/run-1/stop", {});
    check("server refuses start, cancel and stop (" + who + ")", [r1, r2, r3].every((r) => r.status === 409 && r.json.error === "history_read_only"), [r1, r2, r3].map((r) => r.status).join(","));
  }
  const vb = await api(addr, tok.b, "/api/viewer");
  check("consumer B's viewer gets the label, not the trace", vb.status === 200 && vb.json.history.label === LABEL && !vb.json.history.trace);
  const va = await api(addr, tok.a, "/api/viewer");
  // The frozen clock is the record's last event (of any task), never now.
  check("served_at is the record's time: history.as_of, at or after task-a's last event", task.served_at === va.json.history.as_of
    && task.served_at >= evs.json.events[2].observed_at && task.served_at < Date.now() / 1000 - 1, task.served_at + " / as_of " + va.json.history.as_of + " / task-a #3 " + evs.json.events[2].observed_at);
  const calls = va.json.history.trace.tool_calls;
  check("consumer A's viewer gets the trace: three tool calls, the purchase mapped to events #2 and #3",
    calls.map((c) => c.tool).join(",") === "list_products,get_quote,request_purchase" && calls[2].purchase_order_id === po && calls[2].event_sequences.join(",") === "2,3");
  check("after the refused writes: still three events", (await api(addr, tok.host, "/api/tasks/task-a/events")).json.events.length === 3);

  // ---- the page ----
  browser = await startChrome();
  const b = browser;
  await b.send("Page.navigate", { url: "http://" + addr + "/" });
  await b.waitFor(`document.readyState === "complete"`);
  check("Unity Web build loads", await b.waitFor(`["ready","rendered"].includes(document.getElementById("unity-status").dataset.state)`, 90000), await b.evaluate(text("#unity-status")));
  await signIn(b, tok.a);
  check("task-a opens", await openTask(b, "task-a"));
  check("Unity reports task-a #3 waiting", await b.waitFor(`${uKey("state")} === "rendered" && ${uKey("task")} === "task-a" && ${uKey("seq")} === "3" && ${uKey("phase")} === "waiting"`, 30000));
  await b.evaluate(`(document.getElementById("evidence-box").open = true, true)`);
  await tall(b);
  let r = JSON.parse(await b.evaluate(reading));
  check("label at the top and by the scene", r.badge === LABEL && r.note, r.badge);
  check("mode chip: history, not live", r.chip === "歷史紀錄（非即時）", r.chip);
  check("status line: the record's time, not now", r.status.includes("紀錄時間") && r.status.includes("不是現在") && !r.status.includes("Store 時間"), r.status);
  check("card: the Ask as it was at the end of the run", r.card.includes("執行結束時待人工核准（歷史快照；不是仍有效的核准）"));
  check("scene text: a snapshot, not a live wait", r.scene.includes("歷史快照") && !r.scene.includes("已達期限"), r.scene);
  check("no countdown, no expiry made up, nothing paid", !/倒數中|剩餘\s*\d|已達期限，等待|APPROVAL_EXPIRED|付款成功|已付款/.test(r.card + r.evidence + r.scene + r.inspector + r.status));
  check("evidence: three tool calls, the derived mapping marked", r.evidence.includes("工具呼叫（真實執行 trace）") && r.evidence.includes("#1 list_products")
    && r.evidence.includes("#2 get_quote") && r.evidence.includes("#3 request_purchase") && r.evidence.includes("Host 推導") && r.evidence.includes("#2、#3"));
  check("evidence: purchase ID, intent_hash and the Policy reason", r.evidence.includes(po) && r.evidence.includes(intent) && r.evidence.includes("HUMAN_APPROVAL_REQUIRED"));
  check("Timeline #1-#3; Inspector the same purchase", r.timeline === "1,2,3" && r.inspector.includes(po), r.timeline);
  await shot(b, "end-history", r);

  // ---- replay #1..#3: the card follows the step; the full record says so ----
  // Each step is read inside the page the moment Unity reports it rendered
  // while the page says that step (a replay step lasts about 0.4 s after the
  // report, too short to read reliably from here).
  await b.evaluate(`(() => { window.__steps = {}; const read = () => JSON.parse(${reading});
    new MutationObserver(() => { const r = read(); const m = r.chip.match(/^回放中 #(\\d+)／/);
      if (m && r.unity.state === "rendered" && r.unity.seq === m[1] && !window.__steps[m[1]]) window.__steps[m[1]] = r; })
      .observe(document.getElementById("unity-status"), { attributes: true, childList: true, characterData: true, subtree: true });
    return true; })()`);
  // Test-only hold (like browser-ui.mjs's fetch hold): the replay's pause
  // after step #2 (its 400 ms timer) waits until released, so the
  // screenshot is of step #2 and nothing moves while it is taken.
  await b.evaluate(`(() => { const orig = window.setTimeout.bind(window); window.__held = null;
    window.setTimeout = (fn, ms, ...rest) => (ms === 400 && !window.__held && document.getElementById("mode-chip").textContent.startsWith("回放中 #2／")
      ? (window.__held = () => orig(fn, ms, ...rest), 0) : orig(fn, ms, ...rest));
    return true; })()`);
  await b.evaluate(`document.getElementById("replay").click()`);
  const seen2 = await b.waitFor(`!!window.__held && ${uKey("state")} === "rendered" && ${uKey("seq")} === "2"`, 20000);
  const at2 = JSON.parse(await b.evaluate(reading));
  const png = await b.send("Page.captureScreenshot", { format: "png", captureBeyondViewport: false });
  const after2 = JSON.parse(await b.evaluate(reading));
  writeFileSync(join(out, "mid-replay-2.png"), Buffer.from(png.data, "base64"));
  await b.evaluate(`(window.__held(), true)`);
  check("mid-replay screenshot at step #2: page and Unity at #2 before and after it was taken", seen2 && [at2, after2].every((x) => x.chip.startsWith("回放中 #2／")
    && x.unity.seq === "2" && x.unity.phase === "quoted" && x.card.startsWith("回放當步 #2／") && x.token === ""), after2.chip + " Unity #" + after2.unity.seq);
  log("  mid-replay-2.png (full page): task " + at2.unity.task + ", Unity #" + at2.unity.seq + " " + at2.unity.phase + ", chip「" + at2.chip + "」, card「" + at2.card.slice(0, 80) + "…」");
  check("replay ends back in history mode", await b.waitFor(`document.getElementById("status").dataset.mode === "live" && document.getElementById("status").dataset.kind === "ok"`, 40000));
  const steps = JSON.parse(await b.evaluate(`JSON.stringify(window.__steps)`));
  for (const n of [1, 2, 3]) {
    const rr = steps[n];
    check("replay #" + n + ": the page and Unity at the same step", !!rr && rr.unity.task === "task-a", rr ? JSON.stringify(rr.unity) + " " + rr.chip : "not seen");
  }
  steps[1] = steps[1] || { card: "", evidence: "", scene: "" };
  steps[2] = steps[2] || { card: "", evidence: "", scene: "" };
  steps[3] = steps[3] || { card: "", evidence: "", scene: "" };
  check("replay #1, #2: no Ask on the card yet", !steps[1].card.includes("Ask") && !steps[2].card.includes("Ask") && steps[2].card.includes("已提出請購，尚無裁決"), steps[2].card.slice(0, 120));
  check("replay #2: the full record is marked, later events not reached", steps[2].replayNote && steps[2].inspectorNote.includes("完整紀錄，非回放當步")
    && steps[2].evidence.includes("回放尚未到此"));
  check("replay #3: the Ask as it was, no countdown", steps[3].card.includes("執行結束時待人工核准（歷史快照") && !/倒數中|已達期限/.test(steps[3].card + steps[3].scene));
  // Leave a replay part way and replay again.
  await b.evaluate(`document.getElementById("replay").click()`);
  await b.waitFor(`${text("#mode-chip")}.startsWith("回放中 #2／")`, 20000);
  await b.evaluate(`document.getElementById("replay-stop").click()`);
  check("a stopped replay returns to history mode", await b.waitFor(`document.getElementById("status").dataset.mode === "live" && document.getElementById("status").dataset.kind === "ok"`, 40000));
  check("after two replays: still three events", (await api(addr, tok.host, "/api/tasks/task-a/events")).json.events.length === 3);
  check("Unity back at task-a #3 waiting", await b.waitFor(`${uKey("state")} === "rendered" && ${uKey("seq")} === "3" && ${uKey("phase")} === "waiting"`, 30000));

  // ---- the page opened again ----
  await b.send("Page.reload", {});
  await b.waitFor(`document.readyState === "complete"`);
  check("reopened page asks for the token again", await b.waitFor(`${text("#viewer")} === "未驗證"`));
  await b.waitFor(`["ready","rendered"].includes(document.getElementById("unity-status").dataset.state)`, 90000);
  await signIn(b, tok.a);
  check("reopened: task-a opens", await openTask(b, "task-a"));
  check("reopened: Unity at task-a #3", await b.waitFor(`${uKey("state")} === "rendered" && ${uKey("seq")} === "3"`, 30000));
  r = JSON.parse(await b.evaluate(reading));
  check("reopened: label and Timeline as before", r.badge === LABEL && r.timeline === "1,2,3");

  // ---- the host page: no control offered, the record's time labelled ----
  await b.send("Page.navigate", { url: "http://" + addr + "/host" });
  await b.waitFor(`document.readyState === "complete"`);
  const doc = await b.send("DOM.getDocument", {});
  const q = await b.send("DOM.querySelector", { nodeId: doc.root.nodeId, selector: "#token-file" });
  await b.send("DOM.setFileInputFiles", { nodeId: q.nodeId, files: [join(copy, "tok", "tokens.json")] });
  const row = (k) => `(document.querySelector('#readiness tr[data-item="${k}"]')?.textContent || "")`;
  check("host page: Store row says history, read-only copy, no expiry", await b.waitFor(`${row("Store")}.includes("歷史模式：唯讀副本，不執行到期檢查")`, 20000), await b.evaluate(row("Store")));
  check("host page: the time row is the record's time, not now", (await b.evaluate(row("紀錄時間"))).includes("不是現在的伺服器時間") && (await b.evaluate(row("檢查時間"))) === "");
  check("host page: no start, stop or cancel offered", await b.waitFor(`document.getElementById("control").hidden && document.querySelectorAll("#run-scenarios button, #control-now button").length === 0`));
  check("host page: the label", await b.waitFor(`${text("header .badge")} === ${JSON.stringify(LABEL)}`, 20000));
  await b.waitFor(`[...document.querySelectorAll("#consumer-pick button")].some((x) => x.textContent.includes("consumer-a"))`, 20000);
  await b.evaluate(`[...document.querySelectorAll("#consumer-pick button")].find((x) => x.textContent.includes("consumer-a"))?.click()`);
  check("host page: consumer A's task-a opens", await openTask(b, "task-a"));
  check("host page: Unity at task-a #3", await b.waitFor(`${uKey("state")} === "rendered" && ${uKey("task")} === "task-a" && ${uKey("seq")} === "3"`, 30000));
  await tall(b);
  await b.evaluate(`(document.getElementById("evidence-box") && (document.getElementById("evidence-box").open = true), true)`);
  r = JSON.parse(await b.evaluate(reading));
  await shot(b, "host-history", r);
  check("no token in the URL, storage, cookie or console", await b.evaluate(`JSON.stringify([location.href, document.cookie, localStorage.length, sessionStorage.length])`).then((s) => allTokens.every((t) => !s.includes(t)))
    && allTokens.every((t) => !b.consoleText.join("\n").includes(t)));

  const code = await host.stop();
  host = null;
  log("history host stopped (exit " + code + ")");
  const copyDb = join(copy, "db", "apa-live.db");
  check("the copy is unchanged byte for byte, with nothing beside it", sha(copyDb) === JSON.parse(before).db && readdirSync(join(copy, "db")).join(",") === "apa-live.db");
  check("the evidence is unchanged (two files, names, sizes, times)", stateOf(ev) === before);
} catch (e) {
  check("history browser check ran to the end", false, String(e && e.message || e).slice(0, 300));
} finally {
  if (browser) browser.close();
  if (host) await host.stop().catch(() => {});
}

log("result: " + (failures === 0 ? "PASS" : failures + " FAILED"));
writeFileSync(join(out, "history-ui.log"), lines.join("\n") + "\n");
const leaks = readdirSync(out).filter((f) => { const s = readFileSync(join(out, f)).toString("latin1"); return allTokens.some((t) => t && s.includes(t)); });
if (leaks.length) { console.error("TOKEN IN EVIDENCE: " + leaks.join(", ")); process.exit(2); }
console.log("evidence: " + out);
process.exit(failures === 0 ? 0 : 1);
