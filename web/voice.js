// voice.js: V1's recording (docs/commercial-v1.md), in the browser only.
// The microphone's sound goes into a 16 kHz WAV in memory and is sent with
// the assertion; nothing is stored. No microphone, or no permission, is
// answered as such (D4 a: the approval goes on, degraded and marked).

import * as AP from "./approve.js";

// record: up to maxSeconds, until stop() (given to onStart) is called.
// Resolves { audio } (base64 WAV), { unavailable: "device" } or
// { short: true } (under a second: not sent, read it again).
export async function record(maxSeconds, onStart) {
  let stream;
  try {
    stream = await navigator.mediaDevices.getUserMedia({ audio: { channelCount: 1, echoCancellation: false, noiseSuppression: false, autoGainControl: false } });
  } catch {
    return { unavailable: "device" };
  }
  const ctx = new AudioContext();
  const src = ctx.createMediaStreamSource(stream);
  const node = ctx.createScriptProcessor(4096, 1, 1); // ponytail: deprecated but everywhere; an AudioWorklet if it goes
  const chunks = [];
  node.onaudioprocess = (e) => chunks.push(new Float32Array(e.inputBuffer.getChannelData(0)));
  src.connect(node);
  node.connect(ctx.destination);
  await new Promise((resolve) => {
    const t = setTimeout(resolve, maxSeconds * 1000);
    onStart(() => { clearTimeout(t); resolve(); });
  });
  node.disconnect();
  src.disconnect();
  for (const tr of stream.getTracks()) tr.stop();
  const rate = ctx.sampleRate;
  await ctx.close();
  const all = new Float32Array(chunks.reduce((n, c) => n + c.length, 0));
  let o = 0;
  for (const c of chunks) { all.set(c, o); o += c.length; }
  const wav = AP.wav16k(all, rate, maxSeconds);
  if (AP.seconds(wav) < 1) return { short: true };
  return { audio: AP.base64(wav) };
}
