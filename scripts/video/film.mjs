// The demo video's simulation (docs/demo-video-script.md §7, Aaron 11/6: plan A): the real Web build and its
// Unity scene, the fake Agent on a local test chain that pays, driven by this script in headless Chrome; every
// frame it writes is a simulation and is labelled so in the cut. Throwaway test keys and a throwaway passphrase
// in a new 0700 temporary directory, removed at the end; nothing under var/ is read or written but the Foundry
// tools (var/tools); the chain is a fresh anvil on its own port; the approver is a CDP virtual authenticator.
//
//   node scripts/video/film.mjs [--stage up|sample|full|fooled|transit] [--out DIR]
//
// Stage up: the local paying stack (chain, Signer, facilitator, Host), both approvers enrolled, the host page
// with its tokens; a screenshot, then everything stopped. Stage sample (the 20-30 s sample Aaron sees first):
// then the audience window (/show, 1920x1080) filmed while the host page presents: the town's tour, the
// bookshop's start, the parent's Ask approved with the virtual authenticator, the deliveries; sample.mp4,
// labelled 示意動畫 on every frame.
import { execFileSync, spawn, spawnSync } from "node:child_process";
import { randomBytes } from "node:crypto";
import { chmodSync, existsSync, mkdirSync, mkdtempSync, readFileSync, rmSync, writeFileSync } from "node:fs";
import { tmpdir } from "node:os";
import { join, resolve } from "node:path";
import { startDemo, startChrome, sleep, SUBJECTS } from "../verify/harness.mjs";

const repo = resolve(new URL("../..", import.meta.url).pathname);
const arg = (k, d) => { const i = process.argv.indexOf(k); return i > 0 ? process.argv[i + 1] : d; };
const stage = arg("--stage", "up");
const out = resolve(arg("--out", join(repo, "artifacts/video/film-" + new Date().toISOString().replace(/[-:]/g, "").replace(/\.\d+Z$/, "Z"))));
const CHAIN_PORT = 8546; // its own anvil: a kept local chain (8545) is never touched
const HOST = "127.0.0.1:8402", ORIGIN = "http://localhost:8402", FAC = "127.0.0.1:8403"; // the Signer's WebAuthn origin is localhost:8402
mkdirSync(out, { recursive: true });
const log = (s) => console.log(s);

const tmp = mkdtempSync(join(tmpdir(), "apa-film-"));
chmodSync(tmp, 0o700);
const bin = join(tmp, "bin"), home = join(tmp, "home"), chain = join(tmp, "chain"), dev = join(tmp, "dev");
for (const d of [bin, home]) mkdirSync(d);
const pass = randomBytes(18).toString("base64url"); // the throwaway keys' passphrase: never printed
const env = { ...process.env, GOCACHE: process.env.GOCACHE || join(tmpdir(), "gocache") };
const procs = [];

// tty: a command on a pseudo-terminal (script), its /dev/tty fed one line after each passphrase prompt, as a
// person types it (a line sent before its prompt could be flushed when the prompt turns echo off).
function tty(cmd, args, lines, opts = {}) {
  const p = spawn("script", ["-q", "/dev/null", cmd, ...args], { cwd: repo, env, stdio: ["pipe", "pipe", "pipe"], ...opts });
  let text = "";
  p.stdout.on("data", (b) => { text += b; });
  p.stderr.on("data", (b) => { text += b; });
  (async () => {
    for (let i = 0; i < lines.length; i++) {
      for (let t = 0; t < 6000 && (text.match(/passphrase/gi) || []).length <= i; t++) await sleep(100);
      await sleep(200);
      p.stdin.write(lines[i] + "\n");
    }
  })();
  return { p, text: () => text };
}
const free = (port) => spawnSync("lsof", ["-nP", "-iTCP:" + port, "-sTCP:LISTEN"]).status !== 0;
async function until(what, ok, s = 120) {
  for (let i = 0; i < s * 10; i++) { if (await ok()) return; await sleep(100); }
  throw new Error(what + ": not ready in " + s + " s");
}

// capture: a page's screencast (a frame each time it paints) into dir as JPEGs, each with its time, until the
// returned stop(). Screenshots one by one ran at about 1 fps on the WebGL scene.
function capture(page, dir) {
  mkdirSync(dir, { recursive: true });
  const frames = [];
  let on = true;
  page.on("Page.screencastFrame", (p) => {
    if (!on) return;
    const f = join(dir, String(frames.length).padStart(5, "0") + ".jpg");
    writeFileSync(f, Buffer.from(p.data, "base64"));
    frames.push({ f, t: p.metadata.timestamp });
    page.send("Page.screencastFrameAck", { sessionId: p.sessionId }).catch(() => {});
  });
  page.send("Page.startScreencast", { format: "jpeg", quality: 88, maxWidth: 1920, maxHeight: 1080, everyNthFrame: 1 });
  return async () => { on = false; await page.send("Page.stopScreencast").catch(() => {}); return frames; };
}
const LABEL = "示意動畫：fake Agent・本機測試鏈（不是實跑）";
// encode: the frames at their own times as a 30 fps H.264 file, the simulation's label on every frame (this
// ffmpeg has no drawtext: the label is a PNG drawn here, overlaid).
function encode(frames, dir, file) {
  const list = join(dir, "frames.txt");
  const lines = [];
  frames.forEach((x, i) => { lines.push("file '" + x.f + "'"); lines.push("duration " + Math.max(0.001, ((frames[i + 1] || x).t - x.t) || 1 / 30).toFixed(4)); });
  lines.push("file '" + frames[frames.length - 1].f + "'");
  writeFileSync(list, lines.join("\n") + "\n");
  const label = join(dir, "label.png");
  execFileSync("python3", ["-I", "-c", `
import sys
from PIL import Image, ImageDraw, ImageFont
f = ImageFont.truetype("/System/Library/Fonts/PingFang.ttc", 26)
t = sys.argv[1]
w = int(ImageDraw.Draw(Image.new("RGBA", (1, 1))).textlength(t, font=f)) + 28
im = Image.new("RGBA", (w, 46), (0, 0, 0, 140))
ImageDraw.Draw(im).text((14, 6), t, font=f, fill=(255, 255, 255, 255))
im.save(sys.argv[2])`, LABEL, label]);
  execFileSync("ffmpeg", ["-v", "error", "-y", "-f", "concat", "-safe", "0", "-i", list, "-i", label,
    "-filter_complex", "[0:v]scale=1920:1080,fps=30[v];[v][1:v]overlay=28:H-h-28", "-c:v", "libx264", "-pix_fmt", "yuv420p", file], { stdio: "inherit" });
}

// full: the whole round filmed as sample films the bookshop: the tour, then each venue (entered from its row,
// started once, an Ask approved when it shows, held until its customers end), back to the town between them;
// full.mp4 and marks.json (each moment's second in it, for the cut).
// Receipts a venue ends with: the mart's B is denied before paying; the hotel's A too (its partner's payee); the post
// office's A pays and gets no label (docs/commercial-post-hotel.md). --venues: which, in order (default the three).
const ENDS = { books: 2, cafe: 2, mart: 1, hotel: 1, post: 1 };
async function full(b, unityUp, demo) {
  const venues = arg("--venues", "books,cafe,mart").split(",").filter(Boolean); // "": the town only
  const click = (sel) => b.evaluate(`(() => { const e = document.querySelector(${JSON.stringify(sel)}); if (!e || e.disabled) return false; e.click(); return true; })()`);
  const view = arg("--view", "present");
  let s = b;
  if (view === "show") {
    s = await b.openPage(ORIGIN + "/show");
    await s.send("Emulation.setDeviceMetricsOverride", { width: 1920, height: 1080, deviceScaleFactor: 1, mobile: false });
    log("film: window unity " + (await unityUp(s)));
  }
  log("film: presenting " + (await click("#present-toggle")));
  await sleep(4000);
  const frames = join(out, "full-frames");
  const stop = capture(s, frames);
  const t0 = Date.now() / 1000;
  const marks = {};
  const mark = (k) => { marks[k] = +(Date.now() / 1000 - t0).toFixed(2); log("film: " + k + " at " + marks[k] + " s"); };
  const receipts = () => view === "show" ? s.evaluate(`document.querySelectorAll("#show-receipts > *").length`) : b.evaluate(`document.querySelectorAll(".present-receipt").length`);
  await sleep(1500);
  // 20 s: the tour reaches the food court about 11 s in and the east end about 20 s in (202 tiles at 8 a second since
  // 11/8); at 9 s it stopped short of it (Aaron 11/10: 主片請再重拍, the food court's clerks and customers to be seen).
  mark("tour"); await click("#present-tour"); await sleep(20000);
  const town = +arg("--town", "0"); // seconds in the town before any venue (Aaron 11/6: the buses at their stops)
  if (town > 0) { mark("town"); await sleep(town * 1000); }
  for (const venue of venues) {
    mark(venue + "-enter");
    await b.evaluate(`(() => { const x = document.querySelector('button.venue-item[data-venue="${venue}"]'); if (x) x.click(); return !!x; })()`);
    await sleep(3500);
    mark(venue + "-start");
    if (!(await click("button.venue-go"))) throw new Error(venue + ": no start button");
    const end = Date.now() + 150000;
    while (Date.now() < end) {
      // 需要你處理 follows the customer chosen: another's Ask is a line with 切換到客人 X (the cafe: B's cake while A is chosen).
      if (await b.evaluate(`(() => { const x = [...document.querySelectorAll("#approval button")].find((e) => e.textContent.startsWith("切換到客人")); if (!x) return false; x.click(); return true; })()`)) {
        mark(venue + "-switch");
        await sleep(1500);
      }
      if (await b.evaluate(`!!document.querySelector("#approval .approval-head[data-state=pending]") && !!document.querySelector("#approval .approval-approve:not([disabled])")`)) {
        mark(venue + "-ask");
        await sleep(3000); // the audience sees the ask before it is approved
        if (await click("#approval .approval-approve")) mark(venue + "-approved");
      }
      if ((await receipts()) >= ENDS[venue]) break;
      await sleep(400);
    }
    mark(venue + "-done");
    await sleep(6000); // the ends held a moment
    if (venue === "post") { // docs/commercial-refund.md: the presenter, a person, has the shop refund A's parcel
      const shows = (js) => b.evaluate(js);
      for (const t = Date.now() + 60000; Date.now() < t && !(await shows(`!!document.querySelector("button.present-refund:not([disabled])")`));) await sleep(300);
      mark("post-refund");
      if (!(await click("button.present-refund"))) throw new Error("post: no refund button");
      for (const t = Date.now() + 120000; Date.now() < t && !(await shows(`[...document.querySelectorAll(".present-step-list small")].some((e) => e.textContent.includes("商店已退款"))`));) await sleep(300);
      mark("post-refunded");
      await sleep(6000);
    }
    if (venue !== venues[venues.length - 1]) { await click("#present-town"); await sleep(4000); }
  }
  mark("end");
  // What the Store says of each run: the decision and the payment (the check of a new venue's rule, not a picture).
  const host = demo.tokens().host;
  const get = async (path) => (await fetch("http://" + HOST + path, { headers: { Authorization: "Bearer " + host } })).json();
  const runs = await get("/api/host/runs");
  for (const r of (runs.runs || runs || [])) {
    const t = await get("/api/tasks/" + r.task_id);
    for (const p of t.purchases || []) {
      const d = p.decision || {}, pay = p.payment || {};
      log("film: outcome " + r.scenario + " " + r.task_id + " " + (p.resource_url || "").replace(/^.*\/scenes\//, "") + ": " + (d.policy_verdict || "-") + " " + (d.reason || "") + "; payment " + (pay.state || pay.status || "-") + (pay.code ? " " + pay.code : "") + (pay.refund_tx ? "; refunded " + pay.refund_tx : ""));
    }
  }
  const got = await stop();
  writeFileSync(join(out, "marks.json"), JSON.stringify(marks, null, 1));
  log("film: " + got.length + " frames");
  encode(got, frames, join(out, "full.mp4"));
  log("film: " + join(out, "full.mp4"));
}

// fooled: the simulation of a fooled Agent (books-fooled-a: it believes the guide's 系統通知 and asks for the 2.00
// premium edition), started from A's card as the presenter does (a venue's start takes its first scenarios
// only), filmed presenting in the bookshop until its Deny (over the task budget, nothing paid); fooled.mp4.
async function fooled(b) {
  const click = (sel) => b.evaluate(`(() => { const e = document.querySelector(${JSON.stringify(sel)}); if (!e || e.disabled) return false; e.click(); return true; })()`);
  log("film: presenting " + (await click("#present-toggle")));
  await sleep(4000);
  await b.evaluate(`(() => { const x = document.querySelector('button.venue-item[data-venue="books"]'); if (x) x.click(); return !!x; })()`);
  await sleep(4000);
  const frames = join(out, "fooled-frames");
  const stop = capture(b, frames);
  await sleep(2000);
  log("film: fooled delegate " + (await click(`.agent-card[data-consumer="consumer-a"] .agent-delegate[data-scenario="books-fooled-a"]`)));
  await sleep(500);
  log("film: fooled confirm " + (await click(`.delegation[data-scenario="books-fooled-a"] .delegation-confirm`)));
  const denied = await b.waitFor(`/超出|預算不足|已拒絕/.test((document.querySelector(".present-steps") || document.body).innerText)`, 90000);
  log("film: denied " + denied);
  await sleep(8000);
  const got = await stop();
  encode(got, frames, join(out, "fooled.mp4"));
  log("film: " + join(out, "fooled.mp4"));
}

// transit (docs/commercial-transit.md, docs/commercial-atm.md, Aaron 11/9): customer A walked in game mode, each walk
// a click there (Unity's Walk): to the street's ATM (test USDC minted), down the subway's stairs, on through the
// shopping street's gate to the train (the first time the pass is opened on the device, then the first fare), the
// train to the beach, out through its gate (free) and back in (the second fare, nothing to confirm); full.mp4 and
// marks.json, each moment as the host's status line tells it.
async function transit(b) {
  const click = (sel) => b.evaluate(`(() => { const e = document.querySelector(${JSON.stringify(sel)}); if (!e || e.disabled) return false; e.click(); return true; })()`);
  const says = (text, s) => b.waitFor(`document.getElementById("run-status").textContent.includes(${JSON.stringify(text)})`, s * 1000);
  // Unity's walk reports, kept for this script (the page ignores the kind).
  await b.evaluate(`(() => { const f = window.apaUnityReport; window.apaWalks = []; window.apaUnityReport = (k, g, q, d) => { if (k === "walk") window.apaWalks.push(d); return f(k, g, q, d); }; return true; })()`);
  log("film: presenting " + (await click("#present-toggle")));
  await sleep(4000);
  log("film: walking A " + (await click('#present-play button[data-play="consumer-a"]') && await b.waitFor(`document.getElementById("present-play-hint").hidden === false`, 10000)));
  await sleep(3000);
  const frames = join(out, "full-frames");
  const stop = capture(b, frames);
  const t0 = Date.now() / 1000;
  const marks = {};
  const mark = (k) => { marks[k] = +(Date.now() / 1000 - t0).toFixed(2); log("film: " + k + " at " + marks[k] + " s"); };
  // walk: a click at (x, y), tried again until Unity has a way there (none while the stairs or a train move it).
  const walk = async (k, x, y, to, s = 30) => {
    const msg = JSON.stringify(JSON.stringify({ x, y, to: to || "" }));
    for (const end = Date.now() + s * 1000; Date.now() < end; await sleep(500)) {
      if (await b.evaluate(`import("/ui/app.js").then((m) => { m.shop().instance.SendMessage("Bridge", "Walk", ${msg}); return window.apaWalks.at(-1) === "Applied"; })`)) return mark(k);
    }
    throw new Error(k + ": no way to " + x + "," + y + " in " + s + " s");
  };
  const step = async (k, text, s) => { if (!(await says(text, s))) throw new Error(k + ": the status line never said " + text); mark(k); };
  await sleep(1500);
  mark("start");
  await walk("atm-go", 18, 18.6); // a click on the ATM itself: A stops on the pavement in front of it
  await step("atm", "在 ATM 領到", 60);
  await sleep(5000); // the ATM's words held
  await walk("stairs", 15.6, 15.5, "subway");
  await walk("gate-go", 814.5, 24.15, "", 30); // the car's aisle, through the gate: tried until A is down in the station
  await step("opening", "開通捷運自動扣款", 60);
  await step("ride1", "（第 1 趟", 90);
  await walk("out", 1122.5, 13.1, "", 90); // the beach's hall, out through its gate: tried until the train has stopped there
  await sleep(11000); // out through the gate (free), to the hall
  await walk("in-go", 1114.5, 24.15); // back in, to the car's aisle
  await step("ride2", "（第 2 趟", 60);
  await sleep(7000);
  mark("end");
  const got = await stop();
  writeFileSync(join(out, "marks.json"), JSON.stringify(marks, null, 1));
  log("film: " + got.length + " frames");
  encode(got, frames, join(out, "full.mp4"));
  log("film: " + join(out, "full.mp4"));
}

// sample: the town and the bookshop filmed while the host page presents. --view present (default): the host
// page itself, its scene filling the screen under the cards (the main picture); --view show: the audience
// window (/show), for 為什麼可信 and the receipts (its scene stays at whole multiples: 1x at 1920x1080).
async function sample(b, unityUp) {
  const click = (sel) => b.evaluate(`(() => { const e = document.querySelector(${JSON.stringify(sel)}); if (!e || e.disabled) return false; e.click(); return true; })()`);
  const view = arg("--view", "present");
  let s = b;
  if (view === "show") {
    s = await b.openPage(ORIGIN + "/show"); // the audience window, as 開啟展示視窗 opens it (one only: a second says so)
    await s.send("Emulation.setDeviceMetricsOverride", { width: 1920, height: 1080, deviceScaleFactor: 1, mobile: false });
    log("film: window unity " + (await unityUp(s)));
  }
  log("film: presenting " + (await click("#present-toggle")));
  await sleep(4000);
  const stop = capture(s, join(out, "sample-frames"));
  await sleep(1500);
  log("film: tour " + (await click("#present-tour")));
  await sleep(9000);
  log("film: books " + (await b.evaluate(`(() => { const x = document.querySelector('button.venue-item[data-venue="books"]'); if (!x) return false; x.click(); return true; })()`)));
  await sleep(3500);
  log("film: start " + (await click("button.venue-go")));
  // The parent's Ask: approved with the device (the virtual authenticator) when it shows; then both delivered.
  let approved = 0;
  const end = Date.now() + 120000;
  while (Date.now() < end) {
    if (await b.evaluate(`!!document.querySelector("#approval .approval-head[data-state=pending]") && !!document.querySelector("#approval .approval-approve:not([disabled])")`)) {
      await sleep(2500); // the audience sees the ask before it is approved
      if (await click("#approval .approval-approve")) { approved++; log("film: approved an Ask"); }
    }
    if (view === "show" ? await s.evaluate(`document.querySelectorAll("#show-receipts > *").length >= 2`)
      : await b.evaluate(`document.querySelectorAll(".present-receipt").length >= 2`)) break;
    await sleep(500);
  }
  await sleep(5000); // the receipts held a moment
  const frames = await stop();
  const secs = frames.length > 1 ? frames[frames.length - 1].t - frames[0].t : 0;
  log("film: " + frames.length + " frames over " + secs.toFixed(1) + " s (" + (frames.length / Math.max(secs, 1)).toFixed(1) + " fps); " + approved + " approved");
  encode(frames, join(out, "sample-frames"), join(out, "sample.mp4"));
  log("film: " + join(out, "sample.mp4"));
}

let demo, b;
try {
  for (const p of [CHAIN_PORT, 8402, 8403]) if (!free(p)) throw new Error("127.0.0.1:" + p + " is in use: stop it first");
  log("film: building");
  for (const c of ["demo", "fake-claude", "apa-issuer", "apa-signer", "apa-facilitator"]) execFileSync("go", ["build", "-o", join(bin, c), "./cmd/" + c], { cwd: repo, env, stdio: "inherit" });

  // 1 two throwaway wallets (keygen asks the passphrase twice).
  mkdirSync(join(dev, "data", "keys"), { recursive: true });
  for (const d of [dev, join(dev, "data"), join(dev, "data", "keys")]) chmodSync(d, 0o700);
  const wallets = [];
  for (const w of ["wallet-a", "wallet-b"]) {
    const k = tty(join(bin, "apa-signer"), ["keygen", "--dir", join(dev, "data"), "--wallet", w], [pass, pass]);
    await new Promise((r) => k.p.once("exit", r));
    const m = k.text().match(/0x[0-9a-f]{40}/);
    if (!m) throw new Error("keygen " + w + " printed no address");
    wallets.push(m[0]);
  }
  log("film: wallets " + wallets.map((a) => a.slice(0, 8) + "…").join(" "));

  // 2 the chain: a fresh anvil on its own port, set up for the two wallets.
  const c = spawn("sh", ["scripts/demo/p2-local.sh", ...wallets], { cwd: repo, env: { ...env, P2_LOCAL_DIR: chain, P2_LOCAL_PORT: String(CHAIN_PORT) }, stdio: ["ignore", "pipe", "pipe"] });
  procs.push(c);
  let ctext = "";
  c.stdout.on("data", (x) => { ctext += x; }); c.stderr.on("data", (x) => { ctext += x; });
  await until("the chain", () => /anvil is running/.test(ctext), 300);
  log("film: chain " + (ctext.match(/p2-local: (wrote|resumed)[^\n]*/) || [""])[0]);

  // 3 the Signer (p2-dev.sh: its config from the chain's payments.json; serve asks one passphrase).
  const s = tty("sh", ["scripts/demo/p2-dev.sh", ...wallets], [pass], { env: { ...env, P2_DEV_DIR: dev, P2_LOCAL_DIR: chain } });
  procs.push(s.p);
  const sock = join(dev, "run", "signer.sock");
  await until("the Signer", () => existsSync(sock), 120);
  log("film: Signer ready");

  // 4 the facilitator (FA2), from the chain's payments file naming it.
  const pay = JSON.parse(readFileSync(join(chain, "payments.json")));
  pay.facilitator_url = "http://" + FAC;
  const payments = join(tmp, "payments.json");
  writeFileSync(payments, JSON.stringify(pay, null, 2), { mode: 0o600 });
  const f = spawn(join(bin, "apa-facilitator"), ["serve", "--payments", payments, "--listen", FAC], { cwd: repo, env, stdio: ["ignore", "pipe", "pipe"] });
  procs.push(f);
  await until("the facilitator", async () => { try { return (await fetch("http://" + FAC + "/supported")).ok; } catch { return false; } }, 60);
  log("film: facilitator ready");

  // 5 the credentials (a family: B under 18) and the Host with the fake Agent, paying through the Signer.
  const cred = join(tmp, "cred");
  execFileSync(join(bin, "apa-issuer"), ["init", "--dir", cred, "--issuer", "issuer-demo", "--consumers", "consumer-a,consumer-b"]);
  execFileSync(join(bin, "apa-issuer"), ["subjects", "--dir", cred, "--subjects", SUBJECTS]);
  const state = join(tmp, "state");
  demo = startDemo({ repo, bin, catalog: join(repo, "testdata/demo/scenarios-p2.json"), unityBuild: join(repo, "unity/AgentPaymentAuthority/Build/WebGL/Build"), state, home, addr: HOST,
    extra: ["--registry", join(chain, "registry.json"), "--payments", payments, "--approvals-signer", sock, "--authenticator-source", "virtual", "--credential-dir", cred] });
  await demo.ready();
  log("film: Host ready");

  // 6 the browser: a virtual authenticator, both approvers enrolled, the host page with its tokens.
  // WebGL on the GPU (Metal): the harness's --use-angle=swiftshader (the checks' steady CPU drawing) gave 2-4 frames a
  // second; Chrome takes the last of a repeated switch. --headful: a visible Chrome instead of headless. Never throttled.
  b = await startChrome(join(tmp, "chrome-profile"), ["--use-angle=metal", "--disable-background-timer-throttling"],
    { headless: !process.argv.includes("--headful") });
  await b.send("Emulation.setFocusEmulationEnabled", { enabled: true });
  await b.send("Emulation.setDeviceMetricsOverride", { width: 1920, height: 1080, deviceScaleFactor: 1, mobile: false });
  await b.send("WebAuthn.enable");
  await b.send("WebAuthn.addVirtualAuthenticator", { options: { protocol: "ctap2", transport: "internal", hasResidentKey: true, hasUserVerification: true, isUserVerified: true, automaticPresenceSimulation: true } });
  const admin = (args, input) => { const r = spawnSync(input ? "script" : join(bin, "apa-signer"), input ? ["-q", "/dev/null", join(bin, "apa-signer"), ...args] : args, { input: input || "", env }); return String(r.stdout) + String(r.stderr); };
  for (const approver of ["approver-a", "approver-b"]) {
    const begun = admin(["approver", "enroll", "begin", "--dir", join(dev, "data"), "--approver", approver]);
    const m = begun.match(/enrollment (enr-[0-9a-f]+) for \S+; one-time code ([A-Z0-9]{8})/);
    if (!m) throw new Error("enroll begin for " + approver + " printed nothing usable");
    await b.send("Page.navigate", { url: ORIGIN + "/approve/enroll" });
    await b.waitFor(`document.body.dataset.ready === "1"`);
    await b.evaluate(`(document.getElementById("enroll-id").value = ${JSON.stringify(m[1])}, document.getElementById("enroll-code").value = ${JSON.stringify(m[2])}, document.getElementById("enroll-go").click(), true)`);
    if (!(await b.waitFor(`!document.getElementById("enroll-result").hidden`, 15000))) throw new Error("enrollment page for " + approver + " did not finish");
    const confirmed = admin(["approver", "enroll", "confirm", "--dir", join(dev, "data"), "--id", m[1]], "yes\n");
    if (!confirmed.includes("is active")) throw new Error("enroll confirm for " + approver + ": " + confirmed.slice(0, 200));
  }
  log("film: approvers enrolled (virtual authenticator)");
  const tokenFile = join(tmp, "tokens.json");
  writeFileSync(tokenFile, readFileSync(join(state, "tokens.json")), { mode: 0o600 });
  await b.send("Page.navigate", { url: ORIGIN + "/host" });
  await b.waitFor(`document.readyState === "complete"`);
  const doc = await b.send("DOM.getDocument", {});
  const q = await b.send("DOM.querySelector", { nodeId: doc.root.nodeId, selector: "#token-file" });
  await b.send("DOM.setFileInputFiles", { nodeId: q.nodeId, files: [tokenFile] });
  log("film: tokens " + (await b.waitFor(`document.getElementById("creds").dataset.mode === "file"`, 20000)));
  const unityUp = (page) => page.waitFor(`["ready","rendered","scene","syncing"].includes((document.getElementById("unity-status") || {}).dataset?.state)`, 120000);
  log("film: unity " + (await unityUp(b)));
  await sleep(2000);
  const png = await b.send("Page.captureScreenshot", { format: "png" });
  writeFileSync(join(out, "up-host.png"), Buffer.from(png.data, "base64"));
  log("film: up done; " + join(out, "up-host.png"));
  if (stage === "sample") await sample(b, unityUp);
  else if (stage === "full") await full(b, unityUp, demo);
  else if (stage === "fooled") await fooled(b);
  else if (stage === "transit") await transit(b);
  else if (stage !== "up") throw new Error("stage " + stage + " is not written yet");
} finally {
  if (b) b.close();
  if (demo) await demo.stop().catch(() => {});
  for (const p of procs.reverse()) { try { p.kill("SIGINT"); } catch { /* gone */ } }
  await sleep(1500);
  rmSync(tmp, { recursive: true, force: true });
}
process.exit(0);
