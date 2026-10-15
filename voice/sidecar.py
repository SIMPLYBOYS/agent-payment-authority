"""Voice sidecar (V1, docs/commercial-v1.md): scores a spoken challenge for
the Signer and decides nothing.

HTTP over a Unix socket; the only caller is the Signer.

  GET  /v1/health   200 {"ok": true, "asr": ..., "speaker": ...} once both models are loaded; 503 before
  POST /v1/analyze  body: a WAV, 16 kHz mono 16-bit PCM, at most MAX_SECONDS
                    200 {"transcript", "pinyin", "embedding": [192 floats], "duration_ms"}
                    400 {"error": "AUDIO_FORMAT" | "AUDIO_TOO_LONG" | "AUDIO_EMPTY"}

The two models run in two worker processes: ctranslate2 (ASR) and torch
(speaker) each bring an OpenMP runtime, and one process cannot hold both
on an Intel Mac. This process imports neither.

Audio and transcripts stay in memory: nothing here writes them to disk or
to the log. Pretrained models only, loaded from local directories
(HF_HUB_OFFLINE); nothing leaves the machine.

  var/voice/.venv/bin/python voice/sidecar.py --socket PATH --models var/voice/models
"""
import argparse
import io
import json
import multiprocessing as mp
import os
import socketserver
import sys
import threading
import wave
from http.server import BaseHTTPRequestHandler

MAX_SECONDS = 10
RATE = 16000
MAX_BODY = 44 + MAX_SECONDS * RATE * 2

ASR_MODEL = "faster-whisper-small"
SPEAKER_MODEL = "spkrec-ecapa-voxceleb"


def asr_worker(models, conn):
    from faster_whisper import WhisperModel
    import numpy as np
    from pypinyin import lazy_pinyin

    m = WhisperModel(os.path.join(models, ASR_MODEL), device="cpu", compute_type="int8")
    conn.send("ready")
    while True:
        try:
            pcm = conn.recv_bytes()
        except EOFError:  # the sidecar is gone: so is this worker, quietly
            return
        try:
            audio = np.frombuffer(pcm, dtype="<i2").astype("float32") / 32768.0
            # zh: the challenge phrase is Chinese; digits come back as digits or numerals.
            segs, _ = m.transcribe(audio, language="zh", beam_size=5, vad_filter=False, condition_on_previous_text=False)
            text = "".join(s.text for s in segs).strip()
            conn.send({"transcript": text, "pinyin": " ".join(lazy_pinyin(text))})
        except Exception as e:  # the worker lives on; the request fails (500)
            conn.send({"error": type(e).__name__})


def speaker_worker(models, conn):
    import numpy as np
    import torch
    from speechbrain.inference.speaker import EncoderClassifier

    local = os.path.join(models, SPEAKER_MODEL)
    enc = EncoderClassifier.from_hparams(source=local, savedir=local, overrides={"pretrained_path": local}, run_opts={"device": "cpu"})
    conn.send("ready")
    while True:
        try:
            pcm = conn.recv_bytes()
        except EOFError:  # the sidecar is gone: so is this worker, quietly
            return
        try:
            audio = torch.from_numpy(np.frombuffer(pcm, dtype="<i2").astype("float32") / 32768.0).unsqueeze(0)
            with torch.no_grad():
                e = enc.encode_batch(audio).squeeze()
            conn.send([float(x) for x in e])
        except Exception as e:  # the worker lives on; the request fails (500)
            conn.send({"error": type(e).__name__})


class Workers:
    """One pipe per model, each used by one request at a time."""

    def __init__(self, models):
        ctx = mp.get_context("spawn")
        self.pipes = {}
        for name, fn in (("asr", asr_worker), ("speaker", speaker_worker)):
            parent, child = ctx.Pipe()
            p = ctx.Process(target=fn, args=(models, child), daemon=True)
            p.start()
            self.pipes[name] = (parent, threading.Lock(), p)
        self.ready = threading.Event()
        threading.Thread(target=self._wait, daemon=True).start()

    def _wait(self):
        for parent, _, _ in self.pipes.values():
            if parent.recv() != "ready":
                return
        self.ready.set()

    def alive(self):
        return self.ready.is_set() and all(p.is_alive() for _, _, p in self.pipes.values())

    def ask(self, name, pcm):
        parent, lock, _ = self.pipes[name]
        with lock:
            try:
                parent.send_bytes(pcm)
                out = parent.recv()
            except (EOFError, OSError):
                # A worker is gone: exit, so the supervisor starts a whole new
                # sidecar (meanwhile the Signer reads it as unavailable).
                print("voice sidecar: a model worker died; exiting", file=sys.stderr, flush=True)
                os._exit(1)
        if isinstance(out, dict) and "error" in out:
            raise RuntimeError(out["error"])
        return out


def read_wav(body):
    """The PCM frames of a 16 kHz mono 16-bit WAV, or an error code."""
    try:
        with wave.open(io.BytesIO(body)) as w:
            if w.getframerate() != RATE or w.getnchannels() != 1 or w.getsampwidth() != 2 or w.getcomptype() != "NONE":
                return None, "AUDIO_FORMAT"
            n = w.getnframes()
            if n > MAX_SECONDS * RATE:
                return None, "AUDIO_TOO_LONG"
            pcm = w.readframes(n)
    except Exception:  # anything unreadable is the recording's fault, never the service's (review S5)
        return None, "AUDIO_FORMAT"
    if len(pcm) < RATE // 2 * 2:  # under half a second
        return None, "AUDIO_EMPTY"
    return pcm, ""


def handler_for(workers, versions):
    class H(BaseHTTPRequestHandler):
        def log_message(self, *a):  # no request lines: nothing of the audio or its text is logged
            pass

        def reply(self, code, obj):
            raw = json.dumps(obj).encode()
            self.send_response(code)
            self.send_header("Content-Type", "application/json")
            self.send_header("Content-Length", str(len(raw)))
            self.end_headers()
            self.wfile.write(raw)

        def do_GET(self):
            if self.path != "/v1/health":
                return self.reply(404, {"error": "NOT_FOUND"})
            if not workers.alive():
                return self.reply(503, {"ok": False})
            self.reply(200, {"ok": True, **versions})

        def do_POST(self):
            if self.path != "/v1/analyze":
                return self.reply(404, {"error": "NOT_FOUND"})
            if not workers.alive():
                return self.reply(503, {"error": "NOT_READY"})
            n = int(self.headers.get("Content-Length") or 0)
            if n <= 0 or n > MAX_BODY:
                return self.reply(400, {"error": "AUDIO_TOO_LONG" if n > MAX_BODY else "AUDIO_EMPTY"})
            pcm, err = read_wav(self.rfile.read(n))
            if err:
                return self.reply(400, {"error": err})
            out = {"duration_ms": len(pcm) * 1000 // (RATE * 2)}
            try:
                out.update(workers.ask("asr", pcm))
                out["embedding"] = workers.ask("speaker", pcm)
            except RuntimeError as e:
                return self.reply(500, {"error": "ANALYSIS_FAILED:" + str(e)})
            self.reply(200, out)

    return H


class Server(socketserver.ThreadingMixIn, socketserver.UnixStreamServer):
    daemon_threads = True

    def get_request(self):  # BaseHTTPRequestHandler wants an address pair
        req, _ = super().get_request()
        return req, ("unix", 0)


def main():
    ap = argparse.ArgumentParser()
    ap.add_argument("--socket", required=True)
    ap.add_argument("--models", required=True)
    args = ap.parse_args()
    os.environ["HF_HUB_OFFLINE"] = "1"
    rev = {}
    for name in (ASR_MODEL, SPEAKER_MODEL):  # the pinned commit, as snapshot_download recorded it
        meta = os.path.join(args.models, name, ".cache", "huggingface", "download")
        rev[name] = "local"
        for f in sorted(os.listdir(meta)) if os.path.isdir(meta) else []:
            if f.endswith(".metadata"):
                rev[name] = open(os.path.join(meta, f)).readline().strip()[:7]
                break
    versions = {"asr": ASR_MODEL + "@" + rev[ASR_MODEL], "speaker": SPEAKER_MODEL + "@" + rev[SPEAKER_MODEL]}
    workers = Workers(args.models)
    if os.path.exists(args.socket):
        os.unlink(args.socket)
    old = os.umask(0o077)  # the socket is the Signer's user's only
    srv = Server(args.socket, handler_for(workers, versions))
    os.umask(old)
    print("voice sidecar: listening on " + args.socket, file=sys.stderr, flush=True)
    try:
        srv.serve_forever()
    finally:
        os.unlink(args.socket)


if __name__ == "__main__":
    main()
