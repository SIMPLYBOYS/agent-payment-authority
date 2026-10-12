// Entry check of the fake rehearsal (docs/commercial-ux1.md 7.2): make
// present-rehearsal starts the fake demo with the rehearsal catalog, and the
// three manual scenarios' preconditions can be reached through the host's
// own API: two runs that keep running until stopped (a stop of A leaves B
// running), and for each customer an Ask that ends and stays open (it can
// expire, or be cancelled). It does not wait for an expiry and drives no
// page. The launcher runs in a pseudo-terminal (script(1)), because the host
// shows its pairing code on a terminal only; nothing here reads, keeps or
// logs that code or a token (the tokens come from the session's file).
//
//   node scripts/verify/rehearsal-entry.mjs [--out DIR]

import { spawn, execFileSync } from "node:child_process";
import { mkdtempSync, mkdirSync, readFileSync, readdirSync, writeFileSync, existsSync, chmodSync } from "node:fs";
import { tmpdir } from "node:os";
import { join, resolve } from "node:path";

const repo = resolve(new URL("../..", import.meta.url).pathname);
const arg = (k, d) => { const i = process.argv.indexOf(k); return i > 0 ? process.argv[i + 1] : d; };
const commit = execFileSync("git", ["-C", repo, "rev-parse", "HEAD"]).toString().trim();
const stamp = new Date().toISOString().replace(/[-:]/g, "").replace(/\.\d+Z$/, "Z");
const out = resolve(arg("--out", join(repo, "artifacts/ux1", "rehearsal-entry-" + commit.slice(0, 12) + "-" + stamp)));
mkdirSync(out, { recursive: true });
const lines = [];
let failures = 0;
const log = (s) => { lines.push(s); console.log(s); };
const check = (name, ok, detail = "") => { log((ok ? "PASS " : "FAIL ") + name + (detail ? "  [" + detail + "]" : "")); if (!ok) failures++; };
const sleep = (ms) => new Promise((r) => setTimeout(r, ms));

const base = mkdtempSync(join(tmpdir(), "apa-rehearsal-"));
chmodSync(base, 0o700);
const env = { ...process.env, APA_ADDR: "127.0.0.1:0", APA_PRESENT_DIR: base, APA_PRESENT_OPEN: "true" };
// script -q /dev/null CMD: a pseudo-terminal for the host's pairing code (macOS script(1)).
const p = spawn("script", ["-q", "/dev/null", "make", "present-rehearsal"], { cwd: repo, env, stdio: ["pipe", "ignore", "ignore"] });
const secrets = [];
let session = "", ready = null;
try {
  log("commit " + commit);
  for (let i = 0; i < 1800 && !ready; i++) {
    await sleep(100);
    const s = readdirSync(base).find((x) => x.startsWith("present-"));
    if (s && existsSync(join(base, s, "ready.json"))) { session = join(base, s); ready = JSON.parse(readFileSync(join(session, "ready.json"))); }
  }
  check("make present-rehearsal serves (its ready file)", !!ready, session);
  const tok = JSON.parse(readFileSync(join(session, "tok", "tokens.json"))).tokens;
  const host = tok.find((x) => x.role === "host").token, op = tok.find((x) => x.role === "operator").token;
  secrets.push(...tok.map((x) => x.token));
  const url = "http://" + ready.addr;
  const get = async (path) => (await fetch(url + path, { headers: { Authorization: "Bearer " + host } })).json();
  const post = async (path, body) => (await fetch(url + path, { method: "POST", headers: { Authorization: "Bearer " + op, Origin: url, "Content-Type": "application/json" }, body: JSON.stringify(body) })).json();
  const start = (sc) => post("/api/host/runs", { request_id: "req-rehearsal-" + sc + "-00000000", scenario: sc });
  const run = async (id) => (await (await fetch(url + "/api/host/runs/" + id, { headers: { Authorization: "Bearer " + op } })).json());
  const until = async (fn, ms = 30000) => { for (const end = Date.now() + ms; Date.now() < end; await sleep(200)) if (await fn()) return true; return false; };

  const st = await get("/api/host/status");
  const ids = (st.runs && st.runs.scenarios || []).map((x) => x.id).join(",");
  check("the rehearsal catalog: wait-a, wait-b, z-ask-a, z-ask-b, in that order (一起開始 takes the first of each); fake CLI", ids === "wait-a,wait-b,z-ask-a,z-ask-b" && st.fake_cli.state === "ok" && !st.live, ids);

  // Stop isolation: both wait; a stop of A leaves B running.
  const a = await start("wait-a"), bb = await start("wait-b");
  check("wait-a and wait-b both run, each with its Ask, and keep running", await until(async () => (await run(a.run_id)).tool_calls === 1 && (await run(bb.run_id)).tool_calls === 1)
    && (await sleep(3000), (await run(a.run_id)).live === "running" && (await run(bb.run_id)).live === "running"));
  await post("/api/host/runs/" + a.run_id + "/stop", {});
  check("a stop of A: A ends cancelled, B still runs", await until(async () => (await run(a.run_id)).state === "ended") && (await run(a.run_id)).store_end === "TASK_CANCELLED" && (await run(bb.run_id)).live === "running");
  await post("/api/host/runs/" + bb.run_id + "/stop", {});
  await until(async () => (await run(bb.run_id)).state === "ended");

  // An Ask that ends and stays open, for each customer: it can expire, or be cancelled.
  for (const sc of ["z-ask-a", "z-ask-b"]) {
    const r = await start(sc);
    const ok = await until(async () => (await run(r.run_id)).state === "ended");
    const t = await get("/api/tasks/" + r.task_id);
    check(sc + ": the run ends, its Ask stays open (awaiting approval)", ok && t.status === "awaiting_approval", t.status);
  }
} catch (e) {
  check("the check ran to its end", false, String(e && e.stack || e).slice(0, 300));
} finally {
  // The host by its own pid (the ready file), then the launcher.
  if (ready && ready.pid) { try { process.kill(ready.pid, "SIGINT"); } catch {} }
  for (let i = 0; i < 50 && ready && ready.pid; i++) { try { process.kill(ready.pid, 0); await sleep(100); } catch { break; } }
  p.kill("SIGINT");
  await new Promise((r) => { p.once("exit", r); setTimeout(r, 5000); });
}
const body = lines.join("\n");
check("no token in the log", !secrets.some((x) => x && body.includes(x)));
log("result: " + (failures ? "FAIL (" + failures + ")" : "PASS"));
writeFileSync(join(out, "rehearsal-entry.out"), lines.join("\n") + "\n");
process.exit(failures ? 1 : 0);
