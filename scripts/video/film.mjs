// The demo video's simulation (docs/demo-video-script.md §7, Aaron 11/6: plan A): the real Web build and its
// Unity scene, the fake Agent on a local test chain that pays, driven by this script in headless Chrome; every
// frame it writes is a simulation and is labelled so in the cut. Throwaway test keys and a throwaway passphrase
// in a new 0700 temporary directory, removed at the end; nothing under var/ is read or written but the Foundry
// tools (var/tools); the chain is a fresh anvil on its own port; the approver is a CDP virtual authenticator.
//
//   node scripts/video/film.mjs [--stage up|sample] [--out DIR]
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
  else if (stage !== "up") throw new Error("stage " + stage + " is not written yet");
} finally {
  if (b) b.close();
  if (demo) await demo.stop().catch(() => {});
  for (const p of procs.reverse()) { try { p.kill("SIGINT"); } catch { /* gone */ } }
  await sleep(1500);
  rmSync(tmp, { recursive: true, force: true });
}
process.exit(0);
