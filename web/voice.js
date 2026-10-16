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
// gone), { short: true } (under a second) or { quiet: true } (almost no
// sound: the wrong microphone, or none in use); the last three are not
// sent, the purchase keeps waiting.
export async function record(maxSeconds, onStart, deviceId) {
  let stream;
  try {
    stream = await navigator.mediaDevices.getUserMedia({ audio: { channelCount: 1, echoCancellation: false, noiseSuppression: false, autoGainControl: false, ...(deviceId ? { deviceId: { exact: deviceId } } : {}) } });
  } catch (e) {
    // A chosen microphone that is gone is a choice to make again, not a
    // device-less approval (D4 a stays for no microphone or no permission).
    if (deviceId && e && (e.name === "OverconstrainedError" || e.name === "NotFoundError")) return { missing: true };
    return { unavailable: "device" };
  }
  // MediaRecorder captures and encodes off the page's main thread: a
  // ScriptProcessor ran on it, and a busy page (the Unity shop) garbled the
  // audio while keeping its length (V1 device round). The browser decodes
  // and resamples the recording to 16 kHz itself (OfflineAudioContext).
  const mic = (stream.getAudioTracks()[0] || {}).label || "";
  const parts = [];
  const rec = new MediaRecorder(stream, { audioBitsPerSecond: 128000 });
  rec.ondataavailable = (e) => { if (e.data.size) parts.push(e.data); };
  const stopped = new Promise((r) => { rec.onstop = r; });
  await new Promise((r) => { rec.onstart = r; rec.start(); });
  await new Promise((resolve) => {
    const t = setTimeout(resolve, maxSeconds * 1000);
    onStart(() => { clearTimeout(t); resolve(); }, mic);
  });
  rec.stop();
  await stopped;
  for (const tr of stream.getTracks()) tr.stop();
  let all = new Float32Array(0);
  const rate = 16000;
  try {
    const bytes = await new Blob(parts).arrayBuffer();
    const buf = await new OfflineAudioContext(1, rate, rate).decodeAudioData(bytes);
    all = new Float32Array(buf.length);
    for (let c = 0; c < buf.numberOfChannels; c++) { const d = buf.getChannelData(c); for (let i = 0; i < d.length; i++) all[i] += d[i] / buf.numberOfChannels; }
  } catch {
    return { short: true, mic }; // nothing decodable: as a recording too short to send
  }
  const wav = AP.wav16k(all, rate, maxSeconds);
  if (AP.seconds(wav) < 1) return { short: true, mic };
  if (AP.peak(all) < AP.QUIET) return { quiet: true, mic };
  return { audio: AP.base64(wav) };
}
