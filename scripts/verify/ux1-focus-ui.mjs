// Real-browser check of UX1's keyboard focus on the operation cards (review
// of 382a4b5, R1; docs/commercial-ux1.md 7): a cancel confirmation keeps the
// focus while another card updates from the polls, 返回 gives it back to its
// 取消 entry, a 停止 Agent keeps the focus while its own run updates and never
// hands it to a newer run's stop, a focus moved elsewhere is not taken back, a confirmation whose
// task or credentials changed hands the focus to a safe control and cancels
// nothing, and one explicit confirmation cancels the right task. Keys are
// the browser's own key events. The fake CLI with gate files decides when A's
// card changes; nothing waits on a fixed sleep for a race. Token file, test
// HOME; no token is logged.
//
//   node scripts/verify/ux1-focus-ui.mjs [--unity-build DIR] [--out DIR]

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
const out = resolve(arg("--out", join(repo, "artifacts/ux1", "ux1-focus-ui-" + commit.slice(0, 12) + "-" + stamp)));
mkdirSync(out, { recursive: true });

const lines = [];
let failures = 0;
const started = Date.now();
function log(s) { lines.push(s); console.log(s); }
const mark = (name) => log("  [" + Math.round((Date.now() - started) / 1000) + " s] " + name);
function check(name, ok, detail = "") { log((ok ? "PASS " : "FAIL ") + name + (detail ? "  [" + detail + "]" : "")); if (!ok) failures++; }

const tmp = mkdtempSync(join(tmpdir(), "apa-ux1-focus-"));
chmodSync(tmp, 0o700);
const home = join(tmp, "home");
mkdirSync(home);
const bin = join(tmp, "bin");
mkdirSync(bin);
execFileSync("go", ["build", "-o", join(bin, "demo"), "./cmd/demo"], { cwd: repo, stdio: "inherit" });
execFileSync("go", ["build", "-o", join(bin, "fake-claude"), "./cmd/fake-claude"], { cwd: repo, stdio: "inherit" });

// A answers one call, then waits for gate files between calls (its card's
// count changes when the test opens a gate); B asks and ends (its Ask can be
// cancelled); B's second scenario runs and waits (a newer run of B).
const gate = (n) => join(tmp, "gate-" + n);
const tool = (name, product) => ({ do: "tool", tool: name, ...(product ? { args: { product_id: product } } : {}) });
const scenario = (name, steps) => { const p = join(tmp, name + ".json"); writeFileSync(p, JSON.stringify({ steps })); return p; };
const catalog = join(tmp, "scenarios.json");
writeFileSync(catalog, JSON.stringify({ description: "ux1-focus-ui", scenarios: [
  { id: "a-gated", consumer_id: "consumer-a", label: "A：每次呼叫前等候", scenario: scenario("a-gated", [tool("list_products"), { do: "wait_file", text: gate("a1") }, tool("get_quote", "kr-market"), { do: "wait_file", text: gate("a2") }, tool("request_purchase", "kr-market"), { do: "wait_file", text: gate("a3") }, tool("list_products"), { do: "hang" }]) },
  { id: "b-ask", consumer_id: "consumer-b", label: "B：Ask 後結束", scenario: scenario("b-ask", [tool("list_products"), tool("get_quote", "kr-market"), tool("request_purchase", "kr-market"), { do: "result", text: "done" }]) },
  { id: "b-wait", consumer_id: "consumer-b", label: "B：進店後等待", scenario: scenario("b-wait", [tool("list_products"), { do: "hang" }]) },
] }));

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
  await b.send("Emulation.setFocusEmulationEnabled", { enabled: true });
  await b.send("Emulation.setDeviceMetricsOverride", { width: 1440, height: 900, deviceScaleFactor: 1, mobile: false });
  const hostTask = async (id) => (await fetch("http://" + addr + "/api/tasks/" + id, { headers: { Authorization: "Bearer " + t.host } })).json();
  const startRun = (sc) => fetch("http://" + addr + "/api/host/runs", { method: "POST", headers: { Authorization: "Bearer " + t.op, Origin: "http://" + addr, "Content-Type": "application/json" },
    body: JSON.stringify({ request_id: "req-focus-" + sc + "-0000000000", scenario: sc }) }).then((r) => r.json());
  const card = (c) => `document.querySelector('.agent-card[data-consumer="${c}"]')`;
  // focused: what has the focus: its classes, its task, its card's customer, and whether it is the body.
  const focused = () => b.evaluate(`JSON.stringify((() => { const a = document.activeElement; return { id: a.id, cls: a.className, task: a.dataset.task || "", consumer: a.closest(".agent-card")?.dataset.consumer || "", inRuns: !!a.closest("#runs"), body: a === document.body }; })())`).then(JSON.parse);
  const key = async (k) => {
    const keys = { Enter: { key: "Enter", code: "Enter", windowsVirtualKeyCode: 13, text: "\r" }, Tab: { key: "Tab", code: "Tab", windowsVirtualKeyCode: 9 }, Escape: { key: "Escape", code: "Escape", windowsVirtualKeyCode: 27 } }[k];
    await b.send("Input.dispatchKeyEvent", { type: "keyDown", ...keys });
    await b.send("Input.dispatchKeyEvent", { type: "keyUp", ...keys, text: undefined });
  };
  const calls = (c) => b.evaluate(`(${card(c)}?.querySelector(".agent-calls") || {}).textContent || ""`);
  async function loadFile() {
    const doc = await b.send("DOM.getDocument", {});
    const q = await b.send("DOM.querySelector", { nodeId: doc.root.nodeId, selector: "#token-file" });
    await b.send("DOM.setFileInputFiles", { nodeId: q.nodeId, files: [tokenFile] });
    return b.waitFor(`document.getElementById("creds").dataset.mode === "file"`, 20000);
  }

  mark("load");
  await b.send("Page.navigate", { url: "http://" + addr + "/host" });
  await b.waitFor(`document.readyState === "complete"`);
  await b.waitFor(`["ready","rendered","failed","scene","syncing"].includes(document.getElementById("unity-status").dataset.state)`, 90000);
  check("the token file loads", await loadFile());
  await b.waitFor(`!document.getElementById("start-all").hidden && !document.getElementById("start-all").disabled`, 20000);
  await b.evaluate(`document.getElementById("start-all").click()`);
  check("A runs and waits after 1 call; B ends with its Ask, which can be cancelled", await b.waitFor(`${card("consumer-a")}?.querySelector(".agent-calls")?.textContent === "已回覆工具呼叫 1 次"
    && ${card("consumer-b")}?.dataset.state === "ended" && !!${card("consumer-b")}.querySelector('.row-cancel[data-task="show-b-01"]')`, 30000), await calls("consumer-a"));

  // ---- the confirmation keeps the focus while A's card updates ----
  mark("confirm, then A updates");
  await b.evaluate(`${card("consumer-b")}.querySelector(".row-cancel").focus()`);
  await key("Enter");
  let f = await focused();
  check("Enter on B's 取消請購／task opens its confirmation with the focus on 確認", f.cls.includes("cancel-confirm") && f.task === "show-b-01" && f.consumer === "consumer-b", JSON.stringify(f));
  writeFileSync(gate("a1"), "");
  check("A's card updates from the polls (2 calls)", await b.waitFor(`${card("consumer-a")}.querySelector(".agent-calls").textContent === "已回覆工具呼叫 2 次"`, 20000), await calls("consumer-a"));
  f = await focused();
  check("B's confirm keeps the focus while A's card updates", f.cls.includes("cancel-confirm") && f.task === "show-b-01" && f.consumer === "consumer-b", JSON.stringify(f));
  await key("Tab");
  f = await focused();
  check("Tab goes on to 返回，不取消", f.cls.includes("cancel-back") && f.task === "show-b-01", JSON.stringify(f));
  await key("Enter");
  await b.waitFor(`!${card("consumer-b")}.querySelector(".agent-confirm")`, 5000);
  f = await focused();
  check("返回 closes it, cancels nothing, and gives the focus back to its 取消 entry", f.cls.includes("row-cancel") && f.task === "show-b-01" && (await hostTask("show-b-01")).status === "awaiting_approval", JSON.stringify(f));
  await b.evaluate(`(window.__was = document.activeElement, true)`);
  await sleep(3000); // polls that change nothing
  check("polls that change nothing leave the focus where it is", await b.evaluate(`document.activeElement === window.__was`));

  // ---- 停止 Agent: its own run's update keeps the focus (review of c646c26) ----
  mark("A's stop, same run");
  const runA1 = await b.evaluate(`${card("consumer-a")}.querySelector(".row-stop").dataset.run`);
  await b.evaluate(`${card("consumer-a")}.querySelector(".row-stop").focus()`);
  writeFileSync(gate("a2"), "");
  check("A's card updates (3 calls), same run", await b.waitFor(`${card("consumer-a")}.querySelector(".agent-calls").textContent === "已回覆工具呼叫 3 次"`, 20000), await calls("consumer-a"));
  f = await b.evaluate(`JSON.stringify({ cls: document.activeElement.className, run: document.activeElement.dataset.run || "", consumer: document.activeElement.closest(".agent-card")?.dataset.consumer || "" })`).then(JSON.parse);
  check("A's 停止 Agent keeps the focus while its own run updates", f.cls.includes("row-stop") && f.run === runA1 && f.consumer === "consumer-a", JSON.stringify(f));

  // ---- a focus moved elsewhere is not taken back ----
  mark("focus moved away");
  await b.evaluate(`${card("consumer-b")}.querySelector(".row-cancel").focus()`);
  await key("Enter");
  await key("Tab");
  await key("Tab");
  f = await focused();
  const away = !f.consumer && !f.body;
  check("Tab leaves the card (the user moved on)", away, JSON.stringify(f));
  writeFileSync(gate("a3"), "");
  check("A's card updates again (4 calls)", await b.waitFor(`${card("consumer-a")}.querySelector(".agent-calls").textContent === "已回覆工具呼叫 4 次"`, 20000), await calls("consumer-a"));
  const f2 = await focused();
  check("an update does not take the focus back", JSON.stringify(f2) === JSON.stringify(f), JSON.stringify(f2));
  await b.evaluate(`(${card("consumer-b")}.querySelector(".cancel-back")?.click(), true)`);
  await b.waitFor(`!${card("consumer-b")}.querySelector(".agent-confirm")`, 5000);

  // ---- 停止 Agent: a newer run of A never gets the old run's focus ----
  mark("A's stop, a new run");
  await b.evaluate(`${card("consumer-a")}.querySelector('.row-stop[data-run="${runA1}"]').focus()`);
  await fetch("http://" + addr + "/api/host/runs/" + runA1 + "/stop", { method: "POST", headers: { Authorization: "Bearer " + t.op, Origin: "http://" + addr, "Content-Type": "application/json" }, body: "{}" });
  let a2 = null;
  for (let i = 0; i < 100 && !(a2 && a2.task_id); i++) { await sleep(200); a2 = await startRun("a-gated"); } // once A's old run has ended
  check("A's card follows its newer run", !!a2 && a2.task_id === "show-a-02" && await b.waitFor(`${card("consumer-a")}.dataset.state === "running" && ${card("consumer-a")}.textContent.includes("show-a-02")
    && !!${card("consumer-a")}.querySelector('.row-stop[data-run="${a2 && a2.run_id}"]')`, 20000), JSON.stringify(a2));
  f = await b.evaluate(`JSON.stringify({ cls: document.activeElement.className, run: document.activeElement.dataset.run || "", consumer: document.activeElement.closest(".agent-card")?.dataset.consumer || "" })`).then(JSON.parse);
  check("a new run's stop does not get the old run's focus: A's 選取 has it", !f.cls.includes("row-stop") && f.cls.includes("agent-select") && f.consumer === "consumer-a", JSON.stringify(f));
  check("nothing stopped the new run", (await (await fetch("http://" + addr + "/api/host/runs/" + a2.run_id, { headers: { Authorization: "Bearer " + t.op } })).json()).live === "running");

  // ---- the confirmation's task is no longer B's: closed, nothing cancelled ----
  mark("B's task changes");
  await b.evaluate(`${card("consumer-b")}.querySelector(".row-cancel").focus()`);
  await key("Enter");
  check("the confirmation is open again, focused", (await focused()).cls.includes("cancel-confirm"));
  const r2 = await startRun("b-wait");
  check("B's newer run starts (the operator's other start)", r2.task_id === "show-b-02", JSON.stringify(r2));
  check("B's card follows its newer run; the old confirmation closes", await b.waitFor(`${card("consumer-b")}.dataset.state === "running" && !${card("consumer-b")}.querySelector(".agent-confirm")`, 20000));
  f = await focused();
  check("the focus goes to B's 選取, not to another task's cancel", f.cls.includes("agent-select") && f.consumer === "consumer-b", JSON.stringify(f));
  check("nothing was cancelled", (await hostTask("show-b-01")).status === "awaiting_approval");

  // ---- new credentials while a confirmation is open: nothing cancelled ----
  mark("credentials change");
  await b.evaluate(`(document.getElementById("advanced").open = true, document.getElementById("runs-box").open = true, true)`);
  await b.waitFor(`!!document.querySelector('#runs .task-cancel[data-task="show-b-01"]')`, 15000);
  await b.evaluate(`document.querySelector('#runs .task-cancel[data-task="show-b-01"]').focus()`);
  await key("Enter");
  f = await focused();
  check("in the runs list too: Enter opens the confirmation, focused", f.cls.includes("cancel-confirm") && f.task === "show-b-01" && f.inRuns, JSON.stringify(f));
  await b.evaluate(`document.getElementById("clear-all").click()`);
  await b.waitFor(`document.getElementById("control").hidden`, 10000);
  f = await focused();
  check("after 清除全部憑證: no cancel or confirm has the focus", !/cancel|confirm/.test(f.cls), JSON.stringify(f));
  check("after 清除全部憑證: loading the file again opens no confirmation and focuses no cancel", await loadFile() && await b.waitFor(`!!${card("consumer-b")}`, 20000)
    && await b.evaluate(`!document.querySelector(".agent-confirm")`) && !/cancel|confirm/.test((await focused()).cls));
  check("nothing was cancelled by the change", (await hostTask("show-b-01")).status === "awaiting_approval");

  // ---- one explicit confirmation cancels the right task ----
  mark("confirm");
  await b.waitFor(`!!document.querySelector('#runs .task-cancel[data-task="show-b-01"]')`, 15000);
  await b.evaluate(`document.querySelector('#runs .task-cancel[data-task="show-b-01"]').focus()`);
  await key("Enter");
  check("the confirmation is focused", (await focused()).cls.includes("cancel-confirm"));
  await key("Enter");
  check("Enter on 確認取消 task cancels show-b-01, as the page says", await b.waitFor(`document.getElementById("run-status").textContent.includes("task show-b-01 已取消")`, 20000) && (await hostTask("show-b-01")).status === "cancelled",
    await b.evaluate(`document.getElementById("run-status").textContent`));
  check("the other tasks are not cancelled", (await hostTask("show-a-02")).status !== "cancelled" && (await hostTask("show-b-02")).status !== "cancelled");

  // ---- the drawer's keys, next to it ----
  mark("drawer");
  await b.evaluate(`document.getElementById("show-evidence").focus()`);
  await key("Enter");
  check("Enter on 查看證據 opens the drawer, focus on 關閉", await b.waitFor(`!document.getElementById("evidence-drawer").hidden && document.activeElement.id === "drawer-close"`, 5000));
  await key("Escape");
  check("Esc closes it and the focus goes back to 查看證據", await b.waitFor(`document.getElementById("evidence-drawer").hidden && document.activeElement.id === "show-evidence"`, 5000));

  const exceptions = b.consoleText.filter((x) => x.startsWith("EXCEPTION"));
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
writeFileSync(join(out, "ux1-focus-ui.out"), lines.join("\n") + "\n");
process.exit(failures ? 1 : 0);
