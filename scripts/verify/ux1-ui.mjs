// Real-browser check of UX1 (docs/commercial-ux1.md): the host page with
// the scene first, an operation card per Agent, the same choice from a
// card and from a click on a customer in the shop, the evidence drawer, stop
// and a confirmed cancel, an Ask that ends or expires, and the three sizes.
// The demo host with the fake CLI (no model, no network), a test HOME, the
// Unity Web build, Chrome over the DevTools protocol. The token file stands
// in for pairing (present-ui.mjs drives pairing on a terminal); no token or
// code is logged, and every screenshot is taken with the token fields empty.
//
//   node scripts/verify/ux1-ui.mjs [--unity-build DIR] [--out DIR]

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
const out = resolve(arg("--out", join(repo, "artifacts/ux1", "ux1-ui-" + commit.slice(0, 12) + "-" + stamp)));
mkdirSync(out, { recursive: true });

const lines = [];
let failures = 0;
const started = Date.now();
function log(s) { lines.push(s); console.log(s); }
const mark = (name) => log("  [" + Math.round((Date.now() - started) / 1000) + " s] " + name);
function check(name, ok, detail = "") { log((ok ? "PASS " : "FAIL ") + name + (detail ? "  [" + detail + "]" : "")); if (!ok) failures++; }

const tmp = mkdtempSync(join(tmpdir(), "apa-ux1-"));
chmodSync(tmp, 0o700);
const home = join(tmp, "home");
mkdirSync(home);
const bin = join(tmp, "bin");
mkdirSync(bin);
execFileSync("go", ["build", "-o", join(bin, "demo"), "./cmd/demo"], { cwd: repo, stdio: "inherit" });
execFileSync("go", ["build", "-o", join(bin, "fake-claude"), "./cmd/fake-claude"], { cwd: repo, stdio: "inherit" });

// A asks and waits (3 calls answered); B makes one call, waits for the gate,
// then asks and ends; A's second scenario asks and ends (its Ask expires).
const gate = join(tmp, "gate-b");
const tool = (name, product) => ({ do: "tool", tool: name, ...(product ? { args: { product_id: product } } : {}) });
const scenario = (name, steps) => { const p = join(tmp, name + ".json"); writeFileSync(p, JSON.stringify({ steps })); return p; };
const catalog = join(tmp, "scenarios.json");
writeFileSync(catalog, JSON.stringify({ description: "ux1-ui", scenarios: [
  { id: "a-ask", consumer_id: "consumer-a", label: "A：Ask 後等待", scenario: scenario("a-ask", [tool("list_products"), tool("get_quote", "kr-market"), tool("request_purchase", "kr-market"), { do: "hang" }]) },
  { id: "b-ask", consumer_id: "consumer-b", label: "B：Ask 後結束", scenario: scenario("b-ask", [tool("list_products"), { do: "wait_file", text: gate }, tool("get_quote", "kr-market"), tool("request_purchase", "kr-market"), { do: "result", text: "done" }]) },
  { id: "z-done-a", consumer_id: "consumer-a", label: "A：Ask 後結束", scenario: scenario("z-done-a", [tool("list_products"), tool("get_quote", "kr-market"), tool("request_purchase", "kr-market"), { do: "result", text: "done" }]) },
] }));

const SIZES = [[1440, 900], [1280, 900], [390, 844]];
let demo, b;
const tokens = [];
try {
  log("commit " + commit);
  const state = join(tmp, "state");
  demo = startDemo({ repo, bin, catalog, unityBuild, state, home });
  const addr = await demo.ready();
  const t = demo.tokens();
  tokens.push(t.a, t.b, t.host, t.op);
  const tokenFile = join(tmp, "tokens.json");
  writeFileSync(tokenFile, readFileSync(join(state, "tokens.json")));
  b = await startChrome(join(tmp, "chrome-profile"));
  // A headless page counts as unfocused; Unity's frames then may wait for
  // something else to draw. The page is focused, as a presenter's would be.
  await b.send("Emulation.setFocusEmulationEnabled", { enabled: true });
  const size = (w, h) => b.send("Emulation.setDeviceMetricsOverride", { width: w, height: h, deviceScaleFactor: 1, mobile: w < 500 });
  const hostGet = async (path) => (await fetch("http://" + addr + path, { headers: { Authorization: "Bearer " + t.op } })).json();
  const hostTask = async (id) => (await fetch("http://" + addr + "/api/tasks/" + id, { headers: { Authorization: "Bearer " + t.host } })).json();
  const runs = async () => (await hostGet("/api/host/runs")).runs;
  const card = (c) => `document.querySelector('.agent-card[data-consumer="${c}"]')`;
  const cardText = (c) => b.evaluate(`(${card(c)} || {}).textContent || ""`);
  const li = (c) => `document.querySelector('#scene-status li[data-consumer="${c}"]')`;
  const slot = (c) => b.evaluate(`JSON.stringify((${li(c)} || {}).dataset || null)`).then(JSON.parse);
  const shows = (c, task, phase) => b.waitFor(`(() => { const e = ${li(c)}; return !!e && e.dataset.task === "${task}" && e.dataset.phase === "${phase}" && e.dataset.shown === "true"; })()`, 30000);
  const chosen = () => b.evaluate(`[...document.querySelectorAll('.agent-card[data-selected="true"]')].map((x) => x.dataset.consumer).join(",")`);
  const reports = (kind) => b.evaluate(`JSON.stringify((window.__reports || []).filter((r) => r[0] === "${kind}"))`).then(JSON.parse);
  async function shot(name, keepScroll = false) {
    if (!keepScroll) await b.evaluate(`(window.scrollTo(0, 0), true)`);
    await sleep(1000);
    check("screenshot " + name + ": token fields empty", await b.evaluate(`document.getElementById("token").value === "" && document.getElementById("pair-code").value === ""`));
    const png = await b.send("Page.captureScreenshot", { format: "png" });
    writeFileSync(join(out, name + ".png"), Buffer.from(png.data, "base64"));
    log("  screenshot " + name + ".png");
  }
  // firstScreen: on a desktop the scene, both cards' states and the start
  // buttons are in the first screen; at every size nothing scrolls sideways.
  async function firstScreen(w, h, what) {
    await b.evaluate(`(window.scrollTo(0, 0), true)`);
    await sleep(300);
    const r = JSON.parse(await b.evaluate(`JSON.stringify((() => {
      const vis = (el) => { if (!el) return 0; const r = el.getBoundingClientRect(); return r.width > 0 && r.top >= 0 && r.bottom <= innerHeight && r.left >= 0 && r.right <= innerWidth ? 1 : 0; };
      const c = document.getElementById("unity-canvas").getBoundingClientRect();
      return { overflow: document.documentElement.scrollWidth > innerWidth, canvasTop: c.top, canvasShown: Math.max(0, Math.min(c.bottom, innerHeight) - Math.max(c.top, 0)) / c.height, canvasW: c.width,
        states: [...document.querySelectorAll(".agent-card .agent-state")].map(vis), starts: [...document.querySelectorAll(".agent-card .agent-start, #start-all:not([hidden])")].map(vis) };
    })())`));
    check(w + "x" + h + " " + what + ": no sideways scroll", !r.overflow);
    if (w >= 1000) {
      check(w + "x" + h + " " + what + ": the whole scene, both states and the starts in the first screen",
        r.canvasShown > 0.98 && r.canvasW > 700 && r.states.length === 2 && r.states.every(Boolean) && r.starts.length >= 2 && r.starts.every(Boolean), JSON.stringify(r));
    } else {
      check(w + "x" + h + " " + what + ": the scene first, full width", r.canvasTop < h / 3 && r.canvasShown > 0.98 && r.canvasW > w - 40, JSON.stringify(r));
    }
  }
  async function loadFile() {
    const doc = await b.send("DOM.getDocument", {});
    const q = await b.send("DOM.querySelector", { nodeId: doc.root.nodeId, selector: "#token-file" });
    await b.send("DOM.setFileInputFiles", { nodeId: q.nodeId, files: [tokenFile] });
    return b.waitFor(`document.getElementById("creds").dataset.mode === "file"`, 20000);
  }
  async function loadPage() {
    await b.send("Page.navigate", { url: "http://" + addr + "/host" });
    await b.waitFor(`document.readyState === "complete"`);
    await b.waitFor(`["ready","rendered","failed","scene","syncing"].includes(document.getElementById("unity-status").dataset.state)`, 90000);
    // Headless Chrome was seen to hold Unity's frames while nothing else on
    // the page draws (a walk then waits, ux1-ui-dev-1..5); a page that keeps
    // drawing, as a visible tab does, lets them run. Not seen in a real
    // browser; the manual check watches an Ask expire (docs/commercial-ux1.md).
    await b.evaluate(`(() => { const f = () => requestAnimationFrame(f); f(); return true; })()`);
    // Unity's reports, as they come (the page's own handler still runs).
    await b.evaluate(`(() => { window.__reports = []; const r = window.apaUnityReport; window.apaUnityReport = (k, g, q, d) => { window.__reports.push([String(k), Number(g), Number(q), String(d)]); return r(k, g, q, d); }; return true; })()`);
    return loadFile();
  }
  // clickShop clicks a point of the shop (Unity units; the camera of
  // ShopBuilder: centre 5.1, 4.1, half height 4.8), after a move as a hand would.
  async function clickShop(wx, wy) {
    const r = JSON.parse(await b.evaluate(`JSON.stringify(document.getElementById("unity-canvas").getBoundingClientRect())`));
    const half = 4.8 * r.width / r.height;
    const x = r.x + r.width * (wx - (5.1 - half)) / (2 * half), y = r.y + r.height * (4.1 + 4.8 - wy) / 9.6;
    await b.send("Input.dispatchMouseEvent", { type: "mouseMoved", x, y });
    await sleep(200);
    for (const type of ["mousePressed", "mouseReleased"]) { await b.send("Input.dispatchMouseEvent", { type, x, y, button: "left", buttons: 1, clickCount: 1 }); await sleep(100); }
  }

  // ---- 1 the entry: no consumer or task to pick; nothing runs by itself ----
  mark("load");
  await size(1440, 900);
  check("the token file loads; the header says fake", await loadPage() && await b.waitFor(`document.querySelector("header .badge").dataset.mode === "fake"`, 15000));
  check("the scene owns the shop (version 2 with selection)", await b.waitFor(`!document.getElementById("scene-status").hidden`, 90000));
  check("nothing runs at start", (await runs()).length === 0);
  check("a card per Agent, not started, with its start", await b.waitFor(`document.querySelectorAll(".agent-card").length === 2
    && [...document.querySelectorAll(".agent-card .agent-state")].every((x) => x.textContent === "未開始")
    && !!document.querySelector('.agent-card[data-consumer="consumer-a"] .agent-start[data-scenario="a-ask"]')`, 15000));
  check("the capacity is still there (deployment setting, 2)", await b.evaluate(`(document.querySelector('#readiness tr[data-item="同時執行上限"]') || {}).textContent?.includes("2 場（部署設定 --max-runs") || false`));
  check("Timeline and Inspector are not on the main screen (drawer closed)", await b.evaluate(`document.getElementById("evidence-drawer").hidden && document.getElementById("advanced").open === false`));
  for (const [w, h] of SIZES) { await size(w, h); await firstScreen(w, h, "ready"); await shot("ux1-" + w + "x" + h + "-ready"); }
  await size(1440, 900);

  // ---- 2 both at once; the cards say state, calls and business ----
  mark("start both");
  await b.evaluate(`document.getElementById("start-all").click()`);
  check("一起開始: two answers, each its own", await b.waitFor(`document.getElementById("run-status").textContent.includes("A 已開始 show-a-01") && document.getElementById("run-status").textContent.includes("B 已開始 show-b-01")`, 20000),
    await b.evaluate(`document.getElementById("run-status").textContent`));
  check("A's card: running, 3 calls answered, its Ask", await b.waitFor(`(() => { const c = ${card("consumer-a")}; return c.dataset.state === "running" && c.querySelector(".agent-calls").textContent === "已回覆工具呼叫 3 次"
    && c.querySelector(".agent-calls").dataset.calls === "known" && c.querySelector(".agent-business").textContent.startsWith("Ask"); })()`, 30000), await cardText("consumer-a"));
  check("B's card: running, 1 call answered (waiting at its gate)", await b.waitFor(`(() => { const c = ${card("consumer-b")}; return c.dataset.state === "running" && c.querySelector(".agent-calls").textContent === "已回覆工具呼叫 1 次"; })()`, 30000), await cardText("consumer-b"));
  check("the panels chose A by themselves (no pick by hand), and the ring follows", await b.waitFor(`document.getElementById("current").textContent.includes("show-a-01")`, 20000)
    && (await chosen()) === "consumer-a" && await b.waitFor(`(window.__reports || []).some((r) => r[0] === "selected" && r[3] === "consumer-a|Applied")`, 10000));
  check("both customers in the shop at once", await shows("consumer-a", "show-a-01", "waiting") && await shows("consumer-b", "show-b-01", "entered"));
  for (const [w, h] of SIZES) { await size(w, h); await firstScreen(w, h, "two at work"); await shot("ux1-" + w + "x" + h + "-two-at-work"); }
  await size(1440, 900);

  // ---- 3 the same choice from a card and from the shop; stale clicks drop ----
  mark("choose");
  const before = { a: await slot("consumer-a"), b: await slot("consumer-b") };
  await b.evaluate(`${card("consumer-b")}.querySelector(".agent-select").click()`);
  check("B's card button chooses B: the panels show B's task, the ring moves", await b.waitFor(`document.getElementById("current").textContent.includes("show-b-01")`, 15000)
    && (await chosen()) === "consumer-b" && await b.waitFor(`(window.__reports || []).some((r) => r[0] === "selected" && r[3] === "consumer-b|Applied")`, 10000),
    (await chosen()) + " / " + await b.evaluate(`document.getElementById("current").textContent`) + " / " + JSON.stringify(await reports("selected")));
  await clickShop(8.5, 2.6); // A at the counter (8.5, 1.6), a tile up its body
  check("a click on A in the shop chooses A, as its card does", await b.waitFor(`document.getElementById("current").textContent.includes("show-a-01")`, 15000) && (await chosen()) === "consumer-a",
    JSON.stringify(await reports("select")));
  const sc = Number(before.a.scene);
  await b.evaluate(`(window.apaUnityReport("select", ${sc - 1}, 1, "consumer-b"), window.apaUnityReport("select", ${sc}, 99, "consumer-b"), window.apaUnityReport("select", ${sc}, 1, "consumer-z"), true)`);
  await sleep(1500);
  check("an older scene's, another slot's and an unknown customer's click choose nobody", (await chosen()) === "consumer-a"
    && await b.evaluate(`document.getElementById("unity-canvas").dataset.picksDropped === "3" && document.getElementById("current").textContent.includes("show-a-01")`));
  const afterChoose = { a: await slot("consumer-a"), b: await slot("consumer-b") };
  check("choosing changed no slot and no scene", afterChoose.a.slot === before.a.slot && afterChoose.b.slot === before.b.slot && afterChoose.a.scene === before.a.scene);
  await shot("ux1-1440x900-a-chosen-in-the-shop");

  // ---- 4 the evidence drawer: nothing restarts, the polling goes on ----
  mark("evidence");
  const polls0 = Number(await b.evaluate(`document.getElementById("status").dataset.polls || 0`));
  await b.evaluate(`document.getElementById("show-evidence").click()`);
  check("查看證據 opens the drawer with the evidence and the Timeline; focus on 關閉", await b.waitFor(`!document.getElementById("evidence-drawer").hidden && document.activeElement.id === "drawer-close"
    && document.getElementById("evidence").textContent.length > 50 && document.querySelectorAll("#timeline li").length >= 3`, 10000));
  await sleep(2500);
  check("while it is open the scene and the panels go on (polls), the choice stays", Number(await b.evaluate(`document.getElementById("status").dataset.polls || 0`)) > polls0
    && (await chosen()) === "consumer-a" && (await slot("consumer-a")).slot === before.a.slot && (await slot("consumer-b")).slot === before.b.slot);
  await shot("ux1-1440x900-evidence-drawer");
  await size(390, 844);
  check("390 wide: the drawer covers the width, 關閉 in view, no sideways scroll", await b.evaluate(`(() => { const d = document.getElementById("evidence-drawer").getBoundingClientRect(), c = document.getElementById("drawer-close").getBoundingClientRect();
    return d.width >= innerWidth - 1 && c.top >= 0 && c.bottom <= innerHeight && c.right <= innerWidth && document.documentElement.scrollWidth <= innerWidth; })()`));
  await shot("ux1-390x844-evidence-drawer");
  await b.send("Input.dispatchKeyEvent", { type: "keyDown", key: "Escape", code: "Escape", windowsVirtualKeyCode: 27 });
  await b.send("Input.dispatchKeyEvent", { type: "keyUp", key: "Escape", code: "Escape", windowsVirtualKeyCode: 27 });
  check("Esc closes it and the focus goes back to 查看證據", await b.waitFor(`document.getElementById("evidence-drawer").hidden && document.activeElement.id === "show-evidence"`, 5000));
  await size(1440, 900);
  check("closed: the same scene and the same choice", (await chosen()) === "consumer-a" && (await slot("consumer-a")).scene === before.a.scene);

  // ---- 5 stop A: B goes on; A's stop is not offered again ----
  mark("stop A");
  const r1 = await runs();
  const runA = r1.find((r) => r.consumer_id === "consumer-a").run_id;
  check("A's card offers 停止 Agent for its running run", await b.evaluate(`!!${card("consumer-a")}.querySelector('.row-stop[data-run="${runA}"]:not(:disabled)')`));
  await b.evaluate(`${card("consumer-a")}.querySelector(".row-stop").click()`);
  check("A ends stopped: no stop offered, a reason given; its task cancelled by the stop", await b.waitFor(`(() => { const c = ${card("consumer-a")}; return c.dataset.state === "ended" && !c.querySelector(".row-stop")
    && c.querySelector(".row-stop-off").disabled && c.querySelector(".row-stop-off").title.includes("已結束") && c.querySelector(".agent-calls").dataset.calls === "not_provided"; })()`, 20000), await cardText("consumer-a"));
  const r2 = await runs();
  check("B still running, its calls still counted", r2.find((r) => r.run_id === runA).store_end === "TASK_CANCELLED" && r2.find((r) => r.consumer_id === "consumer-b").live === "running"
    && await b.evaluate(`${card("consumer-b")}.dataset.state === "running" && ${card("consumer-b")}.querySelector(".agent-calls").textContent === "已回覆工具呼叫 1 次"`));
  // A's Ask may have expired (30 s) before the stop: either way A leaves.
  check("A leaves the shop; B stays", await b.waitFor(`(() => { const e = ${li("consumer-a")}; return ["withdrawn", "expired"].includes(e.dataset.phase) && e.dataset.shown === "true"; })()`, 30000)
    && (await slot("consumer-b")).phase === "entered", JSON.stringify([await slot("consumer-a"), await slot("consumer-b")]));
  await shot("ux1-1440x900-a-stopped-b-running");

  // ---- 6 the panels' reads fail: the scene goes on; B ends with its Ask ----
  mark("panels fail, B ends");
  await b.evaluate(`${card("consumer-b")}.querySelector(".agent-select").click()`);
  await b.waitFor(`document.getElementById("current").textContent.includes("show-b-01")`, 15000);
  // The panels' own reads (app.js) fail from now; the scene's (scene.js) do not.
  await b.evaluate(`(() => { const of = window.fetch; window.__failPanels = true; window.fetch = (...a) => (window.__failPanels && String(a[0]).startsWith("/api/tasks/show-b") && (new Error().stack || "").includes("/ui/app.js") ? Promise.reject(new TypeError("offline")) : of(...a)); return true; })()`);
  await sleep(2500);
  writeFileSync(gate, "");
  check("with the panels failing, B's card and the shop still follow B to its Ask", await shows("consumer-b", "show-b-01", "waiting")
    && await b.waitFor(`${card("consumer-b")}.querySelector(".agent-business").textContent.startsWith("Ask") || ${card("consumer-b")}.querySelector(".agent-business").textContent.startsWith("執行已結束")`, 20000));
  check("the panels say they could not read, not that nothing happened", await b.waitFor(`["error","warn"].includes(document.getElementById("status").dataset.kind)`, 15000), await b.evaluate(`document.getElementById("status").textContent`));
  await b.evaluate(`(window.__failPanels = false, true)`);
  check("B's run ended, its Ask pending: 執行已結束／請購待核准, no count made up", await b.waitFor(`(() => { const c = ${card("consumer-b")}; return c.dataset.state === "ended"
    && c.querySelector(".agent-business").textContent.startsWith("執行已結束／請購待核准") && c.querySelector(".agent-calls").textContent === "工具呼叫數：場次結束後不提供"; })()`, 20000), await cardText("consumer-b"));
  check("the panels recover once the reads work", await b.waitFor(`document.getElementById("status").dataset.kind === "ok"`, 15000));
  await shot("ux1-1440x900-b-ended-ask-pending");

  // ---- 7 cancel B's task: asked first, then done, as said ----
  mark("cancel B");
  await b.evaluate(`${card("consumer-b")}.querySelector(".row-cancel").click()`);
  check("取消請購／task asks first and says it ends the whole task", await b.waitFor(`(() => { const x = ${card("consumer-b")}.querySelector(".agent-confirm"); return !!x && x.textContent.includes("取消整個 task") && x.textContent.includes("TASK_CANCELLED") && document.activeElement.classList.contains("cancel-confirm"); })()`, 10000));
  await b.evaluate(`${card("consumer-b")}.querySelector(".cancel-back").click()`);
  await sleep(1000);
  check("返回 cancels nothing", (await hostTask("show-b-01")).status === "awaiting_approval" && await b.evaluate(`!${card("consumer-b")}.querySelector(".agent-confirm")`));
  await b.evaluate(`${card("consumer-b")}.querySelector(".row-cancel").click()`);
  await b.waitFor(`!!${card("consumer-b")}.querySelector(".cancel-confirm")`, 5000);
  await b.evaluate(`${card("consumer-b")}.querySelector(".cancel-confirm").click()`);
  check("confirmed: the task is cancelled, as the page says", await b.waitFor(`document.getElementById("run-status").textContent.includes("task show-b-01 已取消")`, 20000) && (await hostTask("show-b-01")).status === "cancelled",
    await b.evaluate(`document.getElementById("run-status").textContent`));
  check("B's card and the shop: withdrawn, no cancel offered any more", await shows("consumer-b", "show-b-01", "withdrawn") && await b.waitFor(`!${card("consumer-b")}.querySelector(".row-cancel")`, 15000));
  await shot("ux1-1440x900-b-cancelled");

  // ---- 8 an Ask that expires: shown as expired; no approval, no payment ----
  mark("expiry");
  await b.evaluate(`${card("consumer-a")}.querySelector('.agent-start[data-scenario="z-done-a"]').click()`);
  check("A's new run ends with its Ask pending", await b.waitFor(`${card("consumer-a")}.dataset.state === "ended" && ${card("consumer-a")}.querySelector(".agent-business").textContent.startsWith("執行已結束／請購待核准")
    && ${card("consumer-a")}.textContent.includes("show-a-02")`, 30000), await cardText("consumer-a"));
  check("the Ask expires (30 s, recorded by the Store) and the card says so", await b.waitFor(`${card("consumer-a")}.querySelector(".agent-business").textContent.includes("到期")`, 50000)
    && await shows("consumer-a", "show-a-02", "expired"), JSON.stringify(await slot("consumer-a")) + " " + JSON.stringify((await reports("rendered")).slice(-3)) + " " + JSON.stringify((await reports("applied")).slice(-3)) + " " + await b.evaluate(`document.visibilityState + "/" + document.hasFocus()`));
  // Action buttons only: the Timeline's rows are buttons that open an event.
  const payLike = await b.evaluate(`JSON.stringify([...document.querySelectorAll("button:not(.event-head):not(#task-pick button)")].map((x) => x.textContent).filter((t) => /核准|付款|支付|交付|approve|\\bpay/i.test(t)))`);
  check("no approve, pay or deliver button anywhere", payLike === "[]", payLike);
  await shot("ux1-1440x900-a-expired");
  for (const [w, h] of SIZES) { await size(w, h); await firstScreen(w, h, "after the runs"); }
  await size(390, 844);
  await shot("ux1-390x844-after-the-runs");
  await size(1440, 900);

  // ---- 9 the mode not confirmed, a reload, the viewer page ----
  mark("mode unconfirmed, reload");
  const n = (await runs()).length;
  await b.send("Fetch.enable", { patterns: [{ urlPattern: "*/api/mode*" }] });
  b.on("Fetch.requestPaused", (p) => { b.send("Fetch.failRequest", { requestId: p.requestId, errorReason: "Failed" }).catch(() => {}); });
  await b.send("Page.navigate", { url: "http://" + addr + "/host" });
  check("the mode read fails: the header says not confirmed, never fake", await b.waitFor(`document.querySelector("header .badge").dataset.mode === "failed"`, 20000)
    && await b.evaluate(`!document.querySelector("header .badge").textContent.includes("fake")`), await b.evaluate(`document.querySelector("header .badge").textContent`));
  await b.waitFor(`["ready","rendered","failed","scene","syncing"].includes(document.getElementById("unity-status").dataset.state)`, 90000);
  check("with the credentials, the host's own status confirms fake", await loadFile() && await b.waitFor(`document.querySelector("header .badge").dataset.mode === "fake"`, 20000));
  await b.send("Fetch.disable");
  check("the reload started no run; the cards show the runs as they are", (await runs()).length === n && await b.waitFor(`${card("consumer-b")}.dataset.state === "ended" && ${card("consumer-a")}.dataset.state === "ended"`, 20000));
  await b.send("Page.navigate", { url: "http://" + addr + "/" });
  check("the viewer page / has no operation cards and its own layout", await b.waitFor(`document.readyState === "complete" && !document.querySelector(".agent-card") && !!document.getElementById("auth-form")`, 15000));

  const exceptions = b.consoleText.filter((x) => x.startsWith("EXCEPTION"));
  log("  browser error logs (not exceptions): " + (b.consoleText.length - exceptions.length));
  for (const x of [...new Set(b.consoleText.map((y) => y.slice(0, 160)))].slice(0, 8)) log("    " + x);
  check("no page exception", exceptions.length === 0, exceptions.join(" | ").slice(0, 300));
} catch (e) {
  check("the flow ran to its end", false, String(e && e.stack || e).slice(0, 400));
} finally {
  if (b) b.close();
  if (demo) await demo.stop();
}
const body = lines.join("\n");
check("no token in the log", !tokens.some((x) => x && body.includes(x)));
log("result: " + (failures ? "FAIL (" + failures + ")" : "PASS"));
log("evidence: " + out);
writeFileSync(join(out, "ux1-ui.out"), lines.join("\n") + "\n");
process.exit(failures ? 1 : 0);
