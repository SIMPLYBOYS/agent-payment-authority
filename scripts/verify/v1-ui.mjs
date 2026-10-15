// Real-browser check of V1 (docs/commercial-v1.md): the voice sidecar
// (pretrained models, local), apa-signer serve-approvals with a voice
// section, the demo host with the fake CLI and --approvals-signer, and
// headless Chrome with a CDP virtual authenticator (a test fixture, never a
// device). The approver's voice is a macOS TTS voice (synthetic, local),
// enrolled with apa-signer approver voice-enroll; at each approval the page
// shows the phrase, this script has the TTS voice read it now and gives it
// to the page as its microphone (getUserMedia replaced by a stream of that
// recording); the page records, encodes and sends it as it would a
// person's. Cases: pass; another speaker (the template re-enrolled from a
// LibriSpeech speaker, a human recording: fail, denied); the first
// recording played again (fail: its code is the old one's); no microphone (degraded); the sidecar
// stopped (degraded). Two TTS voices are not "another speaker": the
// evaluation found they pass for each other. No key, no payment, no
// person's voice; no token, code or capability logged; nothing of a
// recording kept.
//
//   node scripts/verify/v1-ui.mjs --threshold-milli N [--unity-build DIR] [--out DIR]

import { execFileSync, spawn, spawnSync } from "node:child_process";
import { mkdtempSync, mkdirSync, readFileSync, readdirSync, writeFileSync, chmodSync, existsSync } from "node:fs";
import { createServer, connect } from "node:net";
import { tmpdir } from "node:os";
import { join, resolve } from "node:path";
import { startDemo, startChrome, sleep } from "./harness.mjs";

const repo = resolve(new URL("../..", import.meta.url).pathname);
const arg = (k, d) => { const i = process.argv.indexOf(k); return i > 0 ? process.argv[i + 1] : d; };
const threshold = Number(arg("--threshold-milli", "0"));
if (!(threshold > 0 && threshold < 1000)) { console.error("--threshold-milli N (from voice/eval.py) is required"); process.exit(2); }
const unityBuild = resolve(arg("--unity-build", join(repo, "unity/AgentPaymentAuthority/Build/WebGL/Build")));
const stamp = new Date().toISOString().replace(/[-:]/g, "").replace(/\.\d+Z$/, "Z");
const commit = execFileSync("git", ["-C", repo, "rev-parse", "HEAD"]).toString().trim();
const dirty = execFileSync("git", ["-C", repo, "status", "--porcelain", "--untracked-files=no"]).toString().trim() !== "";
const out = resolve(arg("--out", join(repo, "artifacts/v1", "v1-ui-" + commit.slice(0, 12) + (dirty ? "-dirty" : "") + "-" + stamp)));
mkdirSync(out, { recursive: true });

const lines = [];
let failures = 0;
const started = Date.now();
function log(s) { lines.push(s); console.log(s); }
const mark = (name) => log("  [" + Math.round((Date.now() - started) / 1000) + " s] " + name);
function check(name, ok, detail = "") { log((ok ? "PASS " : "FAIL ") + name + (detail ? "  [" + String(detail).slice(0, 300) + "]" : "")); if (!ok) failures++; }

const tmp = mkdtempSync(join(tmpdir(), "apa-v1-"));
chmodSync(tmp, 0o700);
const home = join(tmp, "home");
mkdirSync(home);
const bin = join(tmp, "bin");
mkdirSync(bin);
for (const [name, pkg] of [["demo", "./cmd/demo"], ["fake-claude", "./cmd/fake-claude"], ["apa-signer", "./cmd/apa-signer"]]) {
  execFileSync("go", ["build", "-o", join(bin, name), pkg], { cwd: repo, stdio: "inherit" });
}

// 16 kHz mono 16-bit WAVs: the TTS voice reading a text, or a LibriSpeech file.
const toWav = (src, name) => { const wav = join(tmp, name + ".wav"); spawnSync("ffmpeg", ["-loglevel", "error", "-y", "-i", src, "-ar", "16000", "-ac", "1", "-sample_fmt", "s16", wav]); return wav; };
const tts = (voice, text, name) => { const aiff = join(tmp, name + ".aiff"); spawnSync("say", ["-v", voice, "-o", aiff, text]); return toWav(aiff, name); };
const APPROVER = "Mei-Jia";
const libri = join(repo, "var/voice/data/LibriSpeech/test-clean");
const human = readdirSync(libri).sort((a, b) => a - b)[1]; // the evaluation half's first speaker
const humanWavs = readdirSync(join(libri, human)).flatMap((ch) => readdirSync(join(libri, human, ch)).filter((f) => f.endsWith(".flac")).map((f) => join(libri, human, ch, f)))
  .sort().slice(0, 3).map((f, i) => toWav(f, "human-" + i));

// The sidecar, on a short socket path.
const vdir = mkdtempSync(join("/tmp", "v1v-"));
const vsock = join(vdir, "v.sock");
let sidecar = spawn(join(repo, "var/voice/.venv/bin/python"), ["-I", join(repo, "voice/sidecar.py"), "--socket", vsock, "--models", join(repo, "var/voice/models")], { stdio: ["ignore", "ignore", "ignore"] });
const health = () => new Promise((res) => {
  const c = connect(vsock, () => c.write("GET /v1/health HTTP/1.0\r\n\r\n"));
  let s = "";
  c.on("data", (d) => { s += d; });
  c.on("end", () => { try { res(JSON.parse(s.slice(s.indexOf("\r\n\r\n") + 4))); } catch { res(null); } });
  c.on("error", () => res(null));
});
let h = null;
for (let i = 0; i < 120 && !(h && h.ok); i++) { await sleep(500); h = await health(); }
if (!h || !h.ok) { console.error("the sidecar did not start"); process.exit(1); }

const port = await new Promise((res) => { const s = createServer(); s.listen(0, "127.0.0.1", () => { const p = s.address().port; s.close(() => res(p)); }); });
const origin = "http://localhost:" + port;
const sdir = join(tmp, "signer");
mkdirSync(sdir, { mode: 0o700 });
const sock = join(sdir, "s.sock");
writeFileSync(join(sdir, "signer.json"), JSON.stringify({ version: 1, socket: sock, callers: [process.getuid()], max_window_seconds: 600,
  wallets: [{ wallet_id: "wallet-a", consumer_id: "consumer-a", chain_id: "84532", address: "0x1111111111111111111111111111111111111111" }],
  assets: [{ chain_id: "84532", asset_contract: "0x4444444444444444444444444444444444444444", eip712_name: "USDC", eip712_version: "2" }], delegations: [],
  approvers: [{ approver_id: "approver-a", consumer_id: "consumer-a", wallet_ids: ["wallet-a"], signer_max: "1000000", version: 1 }],
  webauthn: { rp_id: "localhost", origins: [origin], challenge_ttl_seconds: 120, approval_ttl_seconds: 300 },
  voice: { socket: vsock, timeout_ms: 10000, speaker_threshold_milli: threshold, asr_model: h.asr, speaker_model: h.speaker, decimals: 6,
    merchants: [{ pay_to: "0x3333333333333333333333333333333333333333", name: "示意商家", pinyin: "shi yi shang jia" }] } }), { mode: 0o600 });
let signer = null;
async function startSigner() {
  const p = spawn(join(bin, "apa-signer"), ["serve-approvals", "--dir", sdir], { stdio: ["ignore", "ignore", "pipe"] });
  let text = "";
  p.stderr.on("data", (d) => { text += d; });
  for (let i = 0; i < 100 && !text.includes("serving approvals only"); i++) await sleep(100);
  if (!text.includes("serving approvals only")) throw new Error("serve-approvals did not start");
  signer = p;
}
const admin = (args, input) => {
  const r = spawnSync(input ? "script" : join(bin, "apa-signer"), input ? ["-q", "/dev/null", join(bin, "apa-signer"), ...args] : args, { input: input || "" });
  return String(r.stdout) + String(r.stderr);
};
const enrollVoice = (wavs) => admin(["approver", "voice-enroll", "--dir", sdir, "--approver", "approver-a", ...wavs]);

const tool = (name, product) => ({ do: "tool", tool: name, ...(product ? { args: { product_id: product } } : {}) });
const main = join(tmp, "main.json");
writeFileSync(main, JSON.stringify({ steps: [tool("list_products"), tool("get_quote", "kr-market"), tool("request_purchase", "kr-market"), { do: "result", text: "done" }] }));
const terms = { purpose: "買一份韓國市場簡報（數位資料）", task_budget: "5000000", allow_max: "100000", approval_deadline_seconds: 120, valid_seconds: 1800 };
const catalog = join(tmp, "scenarios.json");
writeFileSync(catalog, JSON.stringify({ description: "v1-ui", scenarios: [{ id: "pay-a", consumer_id: "consumer-a", contract: "apa.pay.v1", scenario: main, label: "委託後選購：韓國市場簡報", delegation: terms }] }));

// The page's microphone: getUserMedia waits for the script to hand it a
// recording (or a refusal), then plays it as the stream the page records.
const MIC = `(() => {
  window.__voice = null;
  navigator.mediaDevices.getUserMedia = async () => {
    for (let i = 0; i < 600 && !window.__voice; i++) await new Promise((r) => setTimeout(r, 100));
    const v = window.__voice; window.__voice = null;
    if (!v || v.deny) throw new DOMException("no microphone", "NotAllowedError");
    const ctx = new AudioContext();
    const bytes = Uint8Array.from(atob(v.b64), (c) => c.charCodeAt(0));
    const buf = await ctx.decodeAudioData(bytes.buffer);
    const src = ctx.createBufferSource(); src.buffer = buf;
    const dst = ctx.createMediaStreamDestination(); src.connect(dst); src.start();
    window.__voiceSeconds = buf.duration;
    return dst.stream;
  };
})();`;

let demo, b;
const recordings = [];
try {
  log("commit " + commit + (dirty ? " (working tree has uncommitted changes)" : "") + "; authenticator: CDP virtual; approver voice: macOS TTS " + APPROVER
    + "; another speaker: LibriSpeech speaker " + human + " (human recordings, CC BY 4.0); sidecar " + h.asr + ", " + h.speaker + "; threshold " + threshold + "/1000");
  const approverWavs = ["我是核准者，這是登記用的第一句話。", "今天天氣很好，我們去買咖啡豆。", "請確認金額與商家，再按下核准。"].map((t, i) => tts(APPROVER, t, "enroll-" + i));
  await startSigner();
  const ve = enrollVoice(approverWavs);
  check("apa-signer approver voice-enroll: the approver's template from three recordings (sealed)", ve.includes("voice template of approver-a from 3 recordings"), ve.trim());
  const state = join(tmp, "state");
  demo = startDemo({ repo, bin, catalog, unityBuild, state, home, addr: "127.0.0.1:" + port,
    extra: ["--approvals-signer", sock, "--authenticator-source", "virtual", "--registry", "testdata/demo/registry.json"] });
  const addr = await demo.ready();
  const t = demo.tokens();
  const tokenFile = join(tmp, "tokens.json");
  writeFileSync(tokenFile, readFileSync(join(state, "tokens.json")));
  b = await startChrome(join(tmp, "chrome-profile"), ["--autoplay-policy=no-user-gesture-required"]);
  await b.send("Emulation.setFocusEmulationEnabled", { enabled: true });
  await b.send("Emulation.setDeviceMetricsOverride", { width: 1440, height: 900, deviceScaleFactor: 1, mobile: false });
  await b.send("Page.addScriptToEvaluateOnNewDocument", { source: MIC });
  await b.send("WebAuthn.enable");
  await b.send("WebAuthn.addVirtualAuthenticator", { options: { protocol: "ctap2", transport: "internal", hasResidentKey: true, hasUserVerification: true, isUserVerified: true, automaticPresenceSimulation: true } });
  const text = (sel) => b.evaluate(`(document.querySelector(${JSON.stringify(sel)}) || {}).textContent || ""`);
  const click = (sel) => b.evaluate(`(() => { const e = document.querySelector(${JSON.stringify(sel)}); if (!e || e.disabled) return false; e.click(); return true; })()`);
  const hostTask = async (id) => (await fetch("http://" + addr + "/api/tasks/" + id, { headers: { Authorization: "Bearer " + t.host } })).json();
  const hostEvents = async (id) => (await (await fetch("http://" + addr + "/api/tasks/" + id + "/events", { headers: { Authorization: "Bearer " + t.host } })).json()).events;
  async function shot(name) {
    await b.evaluate(`(window.scrollTo(0, 0), true)`);
    await sleep(800);
    check("screenshot " + name + ": token and code fields empty", await b.evaluate(`["token", "pair-code", "enroll-code"].every((id) => !document.getElementById(id) || document.getElementById(id).value === "")`));
    const png = await b.send("Page.captureScreenshot", { format: "png" });
    writeFileSync(join(out, name + ".png"), Buffer.from(png.data, "base64"));
    log("  screenshot " + name + ".png");
  }
  // WebAuthn enrollment, as in a2-ui.
  const begun = admin(["approver", "enroll", "begin", "--dir", sdir, "--approver", "approver-a"]);
  const m = begun.match(/enrollment (enr-[0-9a-f]+) for \S+; one-time code ([A-Z0-9]{8})/);
  await b.send("Page.navigate", { url: origin + "/approve/enroll" });
  await b.waitFor(`document.body.dataset.ready === "1"`);
  await b.evaluate(`(document.getElementById("enroll-id").value = ${JSON.stringify(m[1])}, document.getElementById("enroll-code").value = ${JSON.stringify(m[2])}, document.getElementById("enroll-go").click(), true)`);
  await b.waitFor(`!document.getElementById("enroll-result").hidden`, 15000);
  const confirmed = admin(["approver", "enroll", "confirm", "--dir", sdir, "--id", m[1]], "yes\n");
  check("the virtual device enrolled for approver-a", confirmed.includes("is active"));
  await b.send("Page.navigate", { url: origin + "/host" });
  await b.waitFor(`document.readyState === "complete"`);
  await b.waitFor(`["ready","rendered","failed","scene","syncing"].includes(document.getElementById("unity-status").dataset.state)`, 90000);
  const doc = await b.send("DOM.getDocument", {});
  const q = await b.send("DOM.querySelector", { nodeId: doc.root.nodeId, selector: "#token-file" });
  await b.send("DOM.setFileInputFiles", { nodeId: q.nodeId, files: [tokenFile] });
  check("the host page loads the token file", await b.waitFor(`document.getElementById("creds").dataset.mode === "file"`, 20000));
  await click(`.agent-card[data-consumer="consumer-a"] .agent-sim`);
  await b.waitFor(`document.getElementById("sim-chip").textContent.includes("客人 A")`, 10000);

  let n = 0;
  async function ask() {
    n++;
    const task = "show-a-0" + n;
    await click(`.agent-card[data-consumer="consumer-a"] .agent-delegate[data-scenario="pay-a"]`);
    await click(`.delegation[data-scenario="pay-a"] .delegation-confirm`);
    const ok = await b.waitFor(`document.getElementById("run-status").textContent.includes("已開始 ${task}")`, 20000)
      && await b.waitFor(`!!document.querySelector("#approval .approval-head[data-state=pending]") && document.getElementById("approval-body").textContent.includes("用已登記的裝置核准")`, 30000);
    return { task, ok };
  }
  // approveWith: 核准, then the microphone answers as given (mic.voice reads the phrase now; mic.wav is a recording; mic.deny refuses).
  async function approveWith(mic, shotName) {
    if (!(await click("#approval .approval-approve"))) return "no approve button";
    if (!(await b.waitFor(`!!document.querySelector("#approval .approval-voice-phrase")`, 15000))) return "no phrase shown";
    const phrase = await text("#approval .approval-voice-phrase");
    let rec;
    if (mic.deny) await b.evaluate(`(window.__voice = { deny: true }, true)`);
    else {
      rec = mic.wav || tts(mic.voice, phrase, "phrase-" + n);
      recordings.push(rec);
      await b.evaluate(`(window.__voice = { b64: ${JSON.stringify(readFileSync(rec).toString("base64"))} }, true)`);
      await b.waitFor(`typeof window.__voiceSeconds === "number"`, 10000);
      if (shotName) await shot(shotName);
      await sleep(Math.ceil((await b.evaluate(`window.__voiceSeconds`)) * 1000) + 400);
      await b.evaluate(`(window.__voiceSeconds = undefined, true)`);
      const stopped = await click("#approval .approval-voice-stop");
      log("  stop clicked: " + stopped);
    }
    // What the panel says after the recording, second by second (until it settles).
    for (let i = 0; i < 25; i++) {
      const m = await b.evaluate(`(() => { const h = document.querySelector("#approval .approval-head"); const x = document.querySelector("#approval .approval-msg");
        return (h ? h.dataset.state : "-") + " | " + (x ? x.textContent : "-"); })()`);
      log("    t+" + i + "s " + m);
      if (!m.startsWith("pending")) break;
      await sleep(1000);
    }
    return { phrase, rec };
  }
  const head = () => b.evaluate(`(() => { const x = document.querySelector("#approval .approval-head"); return x ? x.dataset.state + "|" + x.textContent : ""; })()`);
  const settled = () => b.waitFor(`!!document.querySelector("#approval .approval-head") && document.querySelector("#approval .approval-head").dataset.state !== "pending"`, 60000);

  // ---- 1 pass: the approver reads the phrase ----
  mark("pass");
  let a = await ask();
  check("A's Ask waits for the person", a.ok, await text("#approval-body"));
  let r = await approveWith({ voice: APPROVER }, "v1-1440x900-1-phrase");
  check("the phrase names the amount, the merchant and a four-digit code", typeof r === "object" && /^核准 0\.5 USDC，付給示意商家，代碼 \d \d \d \d$/.test(r.phrase), r.phrase || r);
  check("approved with the device and the voice", await settled() && (await head()).startsWith("approved|本人核准已驗證（裝置＋語音），付款未啟用。"), await head());
  let tk = await hostTask(a.task);
  let types = (await hostEvents(a.task)).map((e) => e.event_type);
  check("the Store: grant voice verified; VOICE_CHALLENGE_VERIFIED just before APPROVED", tk.purchases[0].approval.grant && tk.purchases[0].approval.grant.voice === "verified"
    && types.slice(-3).join() === "VOICE_CHALLENGE_VERIFIED,APPROVED,GRANT_ISSUED", JSON.stringify(tk.purchases[0].approval.grant) + " " + types.join());
  await shot("v1-1440x900-2-approved-voice");
  const passRecording = r.rec;

  // ---- 2 fail: the template is another person's (a human recording); the TTS voice reads the phrase ----
  mark("another speaker");
  const ve2 = enrollVoice(humanWavs);
  check("the template replaced by LibriSpeech speaker " + human + "'s three recordings", ve2.includes("from 3 recordings"), ve2.trim());
  a = await ask();
  r = await approveWith({ voice: APPROVER });
  await settled();
  check("another speaker: denied, worded as the voice's failure", (await head()).startsWith("denied|語音挑戰未通過，這筆已拒絕。") && (await text("#approval-body")).includes("聲音不是已登記的核准者"),
    (await head()) + " / " + (await text("#approval .approval-msg")));
  types = (await hostEvents(a.task)).map((e) => e.event_type);
  tk = await hostTask(a.task);
  check("the Store: VOICE_CHALLENGE_FAILED then APPROVAL_DENIED (VOICE_CHALLENGE_FAILED), no grant", types.slice(-2).join() === "VOICE_CHALLENGE_FAILED,APPROVAL_DENIED"
    && tk.purchases[0].approval_closed === "denied" && !tk.purchases[0].approval.grant, types.join());
  await shot("v1-1440x900-3-denied-speaker");
  check("the approver's template back", enrollVoice(approverWavs).includes("from 3 recordings"));

  // ---- 3 fail: the first recording again (a replay) ----
  mark("replay");
  a = await ask();
  r = await approveWith({ wav: passRecording });
  await settled();
  // Played into the microphone again, the old recording is new bytes (no
  // hash match: that is the Signer's check of a byte-identical resend,
  // tested in Go); its old code does not match this challenge's.
  check("the approved recording played again into the microphone: denied, its code is the old challenge's", (await head()).startsWith("denied|語音挑戰未通過")
    && (await text("#approval-body")).includes("唸的內容和這筆不符"), (await head()) + " / " + (await text("#approval .approval-msg")));

  // ---- 4 unavailable: no microphone ----
  mark("no microphone");
  a = await ask();
  r = await approveWith({ deny: true });
  await settled();
  check("no microphone: approved, degraded and marked", (await head()).startsWith("approved|本人核准已驗證（語音降級）") && (await text("#approval-body")).includes("沒有麥克風，或沒有允許使用麥克風"), await head());
  tk = await hostTask(a.task);
  check("the Store: grant voice unavailable, VOICE_DEVICE_UNAVAILABLE", tk.purchases[0].approval.grant && tk.purchases[0].approval.grant.voice === "unavailable"
    && tk.purchases[0].approval.grant.voice_reason === "VOICE_DEVICE_UNAVAILABLE", JSON.stringify(tk.purchases[0].approval.grant));
  await shot("v1-1440x900-4-degraded-device");

  // ---- 5 unavailable: the sidecar stopped ----
  mark("sidecar stopped");
  sidecar.kill("SIGTERM");
  await new Promise((res) => sidecar.once("exit", res));
  sidecar = null;
  a = await ask();
  r = await approveWith({ voice: APPROVER });
  await settled();
  tk = await hostTask(a.task);
  check("the sidecar stopped: approved, degraded (VOICE_SERVICE_UNAVAILABLE)", (await head()).startsWith("approved|") && tk.purchases[0].approval.grant
    && tk.purchases[0].approval.grant.voice_reason === "VOICE_SERVICE_UNAVAILABLE" && (await head()).startsWith("approved|本人核准已驗證（語音降級）") && (await text("#approval-body")).includes("語音服務無法使用"), JSON.stringify(tk.purchases[0].approval.grant) + " " + (await head()));
  await b.send("Emulation.setDeviceMetricsOverride", { width: 390, height: 844, deviceScaleFactor: 1, mobile: true });
  check("390 px: no horizontal page scroll", await b.waitFor(`document.documentElement.scrollWidth <= 390`, 5000));
  await shot("v1-390x844-5-degraded-service");

  // ---- 6 nothing of a recording is kept ----
  mark("scan");
  const places = [];
  for (const f of ["demo.db", "demo.db-wal"]) if (existsSync(join(state, f))) places.push([f, readFileSync(join(state, f))]);
  for (const f of ["approvals.db", "approvals.db-wal"]) if (existsSync(join(sdir, f))) places.push(["signer " + f, readFileSync(join(sdir, f))]);
  places.push(["host log", Buffer.from(demo.log())]);
  const found = [];
  for (const rec of recordings) {
    const pcm = readFileSync(rec).subarray(1000, 1064); // 64 bytes of sound, raw and base64
    for (const [where, bytes] of places) if (bytes.includes(pcm) || bytes.includes(Buffer.from(pcm.toString("base64")))) found.push(rec + " in " + where);
  }
  check("no recording in the Host's or the Signer's database or the Host's log (" + recordings.length + " recordings, " + places.length + " places)", found.length === 0, found.join("; "));
  check("the flow ran to its end", true);
} catch (e) {
  check("the flow ran to its end", false, String(e && e.stack || e).slice(0, 400));
} finally {
  if (b) {
    const errs = b.consoleText.filter((x) => !/favicon|Failed to load resource/.test(x));
    check("no page errors (exceptions, console errors)", errs.length === 0, errs.join(" | "));
    b.close();
  }
  if (demo) await demo.stop();
  if (signer) { signer.kill("SIGINT"); await new Promise((res) => signer.once("exit", res)); }
  if (sidecar) sidecar.kill("SIGTERM");
  log("result: " + (failures ? "FAIL (" + failures + ")" : "PASS"));
  writeFileSync(join(out, "v1-ui.txt"), lines.join("\n") + "\n");
  console.log("evidence: " + out);
  process.exit(failures ? 1 : 0);
}
