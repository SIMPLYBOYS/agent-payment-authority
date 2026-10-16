// V1 development mode: why the page's recording is recognised worse than a
// terminal one (docs/commercial-v1.md 13). One reading of the phrase, taken
// at the same time several ways, each sent to the running voice sidecar:
//
//   page        web/voice.js as the approval page runs it (Chrome's microphone)
//   chrome-raw  the same Chrome stream, unprocessed (both channels), resampled by ffmpeg
//   chrome-L/R  that stream's left / right channel alone
//   ffmpeg      ffmpeg's own capture of avfoundation device :0 (the terminal way)
//
// It prints, on this terminal only, each one's level and transcript, and the
// microphone Chrome used. The audio lives in a 0700 temporary directory that
// is removed at the end; nothing goes to a log or to the Signer.
//
//   node scripts/demo/v1-mic-check.mjs        (with sh scripts/demo/v1-dev.sh running)
//
// A visible Chrome window with its own temporary profile opens; it is allowed
// the microphone for this run only (--use-fake-ui-for-media-stream).
import { spawn, execFileSync } from "node:child_process";
import { createServer } from "node:http";
import { mkdtempSync, writeFileSync, readFileSync, rmSync, chmodSync } from "node:fs";
import { join, dirname } from "node:path";
import { tmpdir } from "node:os";
import { fileURLToPath } from "node:url";
import { startChrome, sleep } from "../verify/harness.mjs";

const repo = join(dirname(fileURLToPath(import.meta.url)), "../..");
const sock = join(repo, "var/v1-dev/run/voice.sock");
const PHRASE = "核准 0.5 USDC，付給測試商店，代碼 3 5 4 0";
const SECONDS = 8;

const tmp = mkdtempSync(join(tmpdir(), "v1-mic-"));
chmodSync(tmp, 0o700);
const web = join(repo, "web");
const srv = createServer((req, res) => {
  const name = req.url === "/" ? "approve.js" : req.url.slice(1);
  if (!/^[a-z0-9]+\.js$/.test(name)) return res.writeHead(404).end();
  try { res.writeHead(200, { "Content-Type": "text/javascript" }).end(readFileSync(join(web, name))); } catch { res.writeHead(404).end(); }
}).listen(0, "127.0.0.1");
await new Promise((r) => srv.once("listening", r));
const origin = "http://localhost:" + srv.address().port;

// The raw stream beside voice.js: the same constraints, both channels kept.
const RAW = `(async () => {
  const s = await navigator.mediaDevices.getUserMedia({ audio: { channelCount: 1, echoCancellation: false, noiseSuppression: false, autoGainControl: false } });
  const ctx = new AudioContext(); await ctx.resume();
  const src = ctx.createMediaStreamSource(s);
  const node = ctx.createScriptProcessor(4096, 2, 2);
  node.channelCountMode = "explicit"; node.channelCount = 2;
  const L = [], R = [];
  node.onaudioprocess = (e) => { L.push(new Float32Array(e.inputBuffer.getChannelData(0))); R.push(new Float32Array(e.inputBuffer.getChannelData(1))); };
  src.connect(node); node.connect(ctx.destination);
  window.__raw = { stop: async () => {
    node.disconnect(); src.disconnect(); for (const t of s.getTracks()) t.stop();
    const join = (cs) => { const a = new Float32Array(cs.reduce((n, c) => n + c.length, 0)); let o = 0; for (const c of cs) { a.set(c, o); o += c.length; } return a; };
    const b64 = (a) => { const u = new Uint8Array(a.buffer); let x = ""; for (let i = 0; i < u.length; i += 0x8000) x += String.fromCharCode(...u.subarray(i, i + 0x8000)); return btoa(x); };
    const out = { rate: ctx.sampleRate, settings: s.getAudioTracks()[0].getSettings(), L: b64(join(L)), R: b64(join(R)) };
    await ctx.close(); return out;
  } };
})()`;

function floatWav(path, rate, chans) {
  const n = chans[0].length, c = chans.length, buf = Buffer.alloc(44 + n * c * 4);
  buf.write("RIFF", 0); buf.writeUInt32LE(36 + n * c * 4, 4); buf.write("WAVEfmt ", 8); buf.writeUInt32LE(16, 16);
  buf.writeUInt16LE(3, 20); buf.writeUInt16LE(c, 22); buf.writeUInt32LE(rate, 24); buf.writeUInt32LE(rate * c * 4, 28);
  buf.writeUInt16LE(c * 4, 32); buf.writeUInt16LE(32, 34); buf.write("data", 36); buf.writeUInt32LE(n * c * 4, 40);
  for (let i = 0; i < n; i++) for (let k = 0; k < c; k++) buf.writeFloatLE(chans[k][i], 44 + (i * c + k) * 4);
  writeFileSync(path, buf);
}
const to16k = (input, output, filter = []) =>
  execFileSync("ffmpeg", ["-loglevel", "error", "-y", "-i", input, ...filter, "-ar", "16000", "-ac", "1", "-c:a", "pcm_s16le", output]);
const dbfs = (x) => (x > 0 ? (20 * Math.log10(x)).toFixed(1) : "-inf") + " dBFS";
function level(wav) {
  const b = readFileSync(wav);
  let peak = 0, sum = 0;
  const n = (b.length - 44) / 2;
  for (let i = 0; i < n; i++) { const v = b.readInt16LE(44 + i * 2) / 32768; peak = Math.max(peak, Math.abs(v)); sum += v * v; }
  return { seconds: (n / 16000).toFixed(1), peak: dbfs(peak), rms: dbfs(Math.sqrt(sum / Math.max(n, 1))) };
}
const analyze = (wav) => {
  try { return JSON.parse(execFileSync("curl", ["-s", "--max-time", "30", "--unix-socket", sock, "--data-binary", "@" + wav, "http://voice/v1/analyze"]).toString()); }
  catch (e) { return { error: String(e.message || e).split("\n")[0] }; }
};

let b, ff;
// Ctrl-C ends here too: the recordings never stay behind.
process.once("SIGINT", () => {
  try { if (ff) ff.kill(); if (b) b.close(); } catch {}
  // Chrome may still be writing its profile: retry, and say so if it stays.
  try { rmSync(tmp, { recursive: true, force: true, maxRetries: 10, retryDelay: 200 }); } catch { console.error("v1-mic-check: remove " + tmp + " yourself"); }
  process.exit(130);
});
try {
  b = await startChrome(join(tmp, "chrome"), ["--use-fake-ui-for-media-stream", "--window-size=700,400"], { headless: false });
  await b.send("Page.navigate", { url: origin + "/" });
  await sleep(1000);
  await b.evaluate(`import("/voice.js").then((V) => { window.__V = V; })`);
  console.log("\n請準備唸這一句（倒數後開始，錄 " + SECONDS + " 秒）：\n\n    " + PHRASE + "\n");
  for (const n of [3, 2, 1]) { console.log("  " + n + "…"); await sleep(1000); }
  ff = spawn("ffmpeg", ["-loglevel", "error", "-y", "-f", "avfoundation", "-i", ":0", "-t", String(SECONDS), "-ar", "16000", "-ac", "1", join(tmp, "ffmpeg.wav")], { stdio: "ignore" });
  await b.evaluate(RAW);
  const page = b.evaluate(`window.__V.record(${SECONDS}, (stop, mic) => { window.__mic = mic; })`);
  console.log("  錄音中，請唸…");
  const got = await page;
  const raw = await b.evaluate(`window.__raw.stop()`);
  const mic = await b.evaluate(`window.__mic || ""`);
  await new Promise((r) => (ff.exitCode !== null ? r() : ff.once("exit", r)));
  console.log("  錄音結束。\n\nChrome 用的麥克風：" + (mic || "（未知）"));
  console.log("Chrome 串流：" + JSON.stringify({ sampleRate: raw.settings.sampleRate, channelCount: raw.settings.channelCount, contextRate: raw.rate }));

  const f32 = (s) => new Float32Array(Uint8Array.from(Buffer.from(s, "base64")).buffer);
  const L = f32(raw.L), R = f32(raw.R);
  let lr = 0, ll = 0, rr = 0;
  for (let i = 0; i < L.length; i++) { lr += L[i] * R[i]; ll += L[i] * L[i]; rr += R[i] * R[i]; }
  console.log("左右聲道相關係數：" + (ll && rr ? (lr / Math.sqrt(ll * rr)).toFixed(3) : "（有一邊是靜音）") + "（1 = 兩邊相同；接近 -1 = 相位相反，合成單聲道會互相抵消）\n");

  floatWav(join(tmp, "raw.wav"), raw.rate, [L, R]);
  const ways = { page: join(tmp, "page.wav"), "chrome-raw": join(tmp, "raw16.wav"), "chrome-L": join(tmp, "l16.wav"), "chrome-R": join(tmp, "r16.wav"), ffmpeg: join(tmp, "ffmpeg.wav") };
  if (got.audio) writeFileSync(ways.page, Buffer.from(got.audio, "base64"));
  to16k(join(tmp, "raw.wav"), ways["chrome-raw"]);
  to16k(join(tmp, "raw.wav"), ways["chrome-L"], ["-af", "pan=mono|c0=c0"]);
  to16k(join(tmp, "raw.wav"), ways["chrome-R"], ["-af", "pan=mono|c0=c1"]);
  for (const [name, wav] of Object.entries(ways)) {
    if (name === "page" && !got.audio) { console.log("page        頁面沒有送出：" + JSON.stringify(got)); continue; }
    let lv;
    try { lv = level(wav); } catch { console.log(name.padEnd(12) + "沒有錄到"); continue; }
    const a = analyze(wav);
    console.log(name.padEnd(12) + lv.seconds + " 秒  峰值 " + lv.peak + "  平均 " + lv.rms + "\n            辨識：" + (a.transcript ?? JSON.stringify(a)));
  }
  console.log("\n要回報時，只需要每一行的「辨識」結果和音量，以及上面的麥克風名稱。");
} finally {
  if (ff && ff.exitCode === null) ff.kill();
  if (b) b.close();
  srv.close();
  await sleep(500);
  rmSync(tmp, { recursive: true, force: true, maxRetries: 10, retryDelay: 200 });
}
