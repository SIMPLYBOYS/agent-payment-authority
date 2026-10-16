// voice.js: V1's recording (docs/commercial-v1.md), in the browser only.
// The microphone's sound goes into a 16 kHz WAV in memory and is sent with
// the assertion; nothing is stored. No microphone, or no permission, is
// answered as such (D4 a: the approval goes on, degraded and marked).

import * as AP from "./approve.js";

// microphones: the audio inputs Chrome offers, [{ id, label }] (labels are
// empty until the page has been allowed the microphone once).
export async function microphones() {
  try {
    return (await navigator.mediaDevices.enumerateDevices()).filter((d) => d.kind === "audioinput" && d.deviceId).map((d) => ({ id: d.deviceId, label: d.label }));
  } catch {
    return [];
  }
}

// record: up to maxSeconds, until stop() (given to onStart, with the
// microphone's name) is called, from the chosen microphone (deviceId) or
// Chrome's default. Resolves { audio } (base64 WAV),
// { unavailable: "device" }, { missing: true } (the chosen microphone is
// gone or cannot be used), { notRecorded: true } (the recording did not
// start or cannot be read), { short: true } (under a second) or
// { quiet: true } (almost no sound: the wrong microphone, or none in use);
// all but the first two are not sent, the purchase keeps waiting.
export async function record(maxSeconds, onStart, deviceId) {
  let stream;
  try {
    stream = await navigator.mediaDevices.getUserMedia({ audio: { channelCount: 1, echoCancellation: false, noiseSuppression: false, autoGainControl: false, ...(deviceId ? { deviceId: { exact: deviceId } } : {}) } });
  } catch (e) {
    return AP.micFailure(e, deviceId);
  }
  try {
    return await take(stream, maxSeconds, onStart);
  } finally {
    for (const tr of stream.getTracks()) tr.stop(); // whatever happened, the microphone is let go
  }
}

async function take(stream, maxSeconds, onStart) {
  // MediaRecorder captures and encodes off the page's main thread: a
  // ScriptProcessor ran on it, and a busy page (the Unity shop) garbled the
  // audio while keeping its length (V1 device round). The browser decodes
  // and resamples the recording to 16 kHz itself (OfflineAudioContext).
  const mic = (stream.getAudioTracks()[0] || {}).label || "";
  const parts = [];
  let rec;
  try {
    rec = new MediaRecorder(stream, { audioBitsPerSecond: 128000 });
    rec.ondataavailable = (e) => { if (e.data.size) parts.push(e.data); };
    // It starts, fails, or is given up after 3 s: never a panel left waiting.
    const started = await new Promise((r) => { rec.onstart = () => r(true); rec.onerror = () => r(false); setTimeout(() => r(false), 3000); rec.start(); });
    if (!started) return { notRecorded: true, mic };
  } catch {
    return { notRecorded: true, mic };
  }
  const stopped = new Promise((r) => { rec.onstop = r; rec.onerror = r; });
  await new Promise((resolve) => {
    const t = setTimeout(resolve, maxSeconds * 1000);
    onStart(() => { clearTimeout(t); resolve(); }, mic);
  });
  if (rec.state !== "inactive") rec.stop();
  await stopped;
  let all = new Float32Array(0);
  const rate = 16000;
  try {
    const bytes = await new Blob(parts).arrayBuffer();
    const buf = await new OfflineAudioContext(1, rate, rate).decodeAudioData(bytes);
    all = new Float32Array(buf.length);
    for (let c = 0; c < buf.numberOfChannels; c++) { const d = buf.getChannelData(c); for (let i = 0; i < d.length; i++) all[i] += d[i] / buf.numberOfChannels; }
  } catch {
    return { notRecorded: true, mic }; // nothing decodable
  }
  const wav = AP.wav16k(all, rate, maxSeconds);
  if (AP.seconds(wav) < 1) return { short: true, mic };
  if (AP.peak(all) < AP.QUIET) return { quiet: true, mic };
  return { audio: AP.base64(wav) };
}
