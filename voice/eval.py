"""V1 evaluation (docs/commercial-v1.md, D5 and D6): pretrained models only,
synthetic and public data only; nothing leaves the machine.

  1 speaker: LibriSpeech test-clean (CC BY 4.0), its 40 speakers split by
    speaker into a tuning half and an evaluation half (no speaker or
    recording in both). Each speaker's template is the mean of three
    recordings, as the Signer makes it; trials are its other recordings
    (genuine) and every other speaker's of the same half (impostor). The
    threshold is the lowest cosine with FAR <= 1% on the tuning half; FAR
    and FRR are then measured on the evaluation half (Wilson 95% intervals).
  2 phrase: Mandarin challenge phrases spoken by macOS TTS voices (local),
    random amounts and codes, through the running sidecar, compared by the
    Signer's own code (go test -run TestVoicePhraseEval); the time each
    analysis takes against the 10 s limit (the degradation rate).
  3 replay: each recorded phrase compared with another challenge's code.
  4 synthetic speakers: TTS voices as speakers against each other's
    templates (a TTS of the enrolled person is a known boundary: no
    anti-spoof in scope B).

Results are numbers only: no recording, transcript or embedding is
written to the report. The TTS files are synthetic and stay in var/.

  var/voice/.venv/bin/python voice/eval.py --data var/voice/data/LibriSpeech/test-clean \
      --models var/voice/models --socket var/voice/run/voice.sock --out DIR
"""
import argparse
import glob
import http.client
import json
import math
import os
import random
import socket
import subprocess
import sys
import time

REPO = os.path.dirname(os.path.dirname(os.path.abspath(__file__)))
RATE = 16000
MAX = 10 * RATE


def wilson(k, n, z=1.96):
    if n == 0:
        return (0.0, 0.0)
    p = k / n
    d = 1 + z * z / n
    c = (p + z * z / (2 * n)) / d
    h = z * math.sqrt(p * (1 - p) / n + z * z / (4 * n * n)) / d
    return (max(0.0, c - h), min(1.0, c + h))


def speaker_eval(data, models, rng):
    import numpy as np
    import soundfile as sf
    import torch
    from speechbrain.inference.speaker import EncoderClassifier

    local = os.path.join(models, "spkrec-ecapa-voxceleb")
    enc = EncoderClassifier.from_hparams(source=local, savedir=local, overrides={"pretrained_path": local}, run_opts={"device": "cpu"})

    def emb(path):
        pcm, sr = sf.read(path, dtype="float32")
        assert sr == RATE
        with torch.no_grad():
            e = enc.encode_batch(torch.from_numpy(pcm[:MAX]).unsqueeze(0)).squeeze().numpy()
        return e / np.linalg.norm(e)

    speakers = sorted(os.listdir(data), key=int)
    halves = {"tuning": speakers[0::2], "evaluation": speakers[1::2]}
    out = {"speakers": {k: len(v) for k, v in halves.items()}}
    scores = {}
    for half, spk in halves.items():
        templates, tests = {}, {}
        for s in spk:
            files = sorted(glob.glob(os.path.join(data, s, "*", "*.flac")))
            files = [f for f in files if sf.info(f).duration >= 2.0]
            rng.shuffle(files)
            enroll, test = files[:3], files[3:13]
            t = np.mean([emb(f) for f in enroll], axis=0)
            templates[s] = t / np.linalg.norm(t)
            tests[s] = [emb(f) for f in test]
        genuine = [float(e @ templates[s]) for s in spk for e in tests[s]]
        impostor = [float(e @ templates[o]) for s in spk for e in tests[s] for o in spk if o != s]
        scores[half] = (genuine, impostor)
        print(f"speaker {half}: {len(genuine)} genuine, {len(impostor)} impostor trials", file=sys.stderr, flush=True)
    gen, imp = scores["tuning"]
    cands = sorted(set(imp + gen))
    thr = next(c for c in cands if sum(x >= c for x in imp) <= 0.01 * len(imp))
    milli = math.ceil(thr * 1000)  # the Signer's setting is in thousandths: rounded up, never looser
    for half, (g, i) in scores.items():
        far, frr = sum(x >= milli / 1000 for x in i), sum(x < milli / 1000 for x in g)
        out[half] = {"genuine_trials": len(g), "impostor_trials": len(i), "far": far / len(i), "far_ci95": wilson(far, len(i)),
                     "frr": frr / len(g), "frr_ci95": wilson(frr, len(g)), "eer_approx": None}
        # EER: where FAR and FRR cross (scan)
        best = min(sorted(set(g + i)), key=lambda c: abs(sum(x >= c for x in i) / len(i) - sum(x < c for x in g) / len(g)))
        out[half]["eer_approx"] = (sum(x >= best for x in i) / len(i) + sum(x < best for x in g) / len(g)) / 2
    out["threshold_milli"] = milli
    return out, milli


class Sidecar:
    def __init__(self, path):
        self.path = path

    def request(self, method, url, body=None):
        c = http.client.HTTPConnection("voice", timeout=30)
        c.sock = socket.socket(socket.AF_UNIX, socket.SOCK_STREAM)
        c.sock.connect(self.path)
        c.request(method, url, body=body, headers={"Content-Type": "audio/wav"} if body else {})
        r = c.getresponse()
        return r.status, json.loads(r.read())


def tts(voice, text, path):
    aiff = path + ".aiff"
    subprocess.run(["say", "-v", voice, "-o", aiff, text], check=True)
    subprocess.run(["ffmpeg", "-loglevel", "error", "-y", "-i", aiff, "-ar", str(RATE), "-ac", "1", "-sample_fmt", "s16", path], check=True)
    os.remove(aiff)


def phrase_eval(side, work, rng, n_per_voice):
    from pypinyin import lazy_pinyin

    voices = ["Mei-Jia", "Ting-Ting"]
    merchants = ["示意商家", "巷口便利商店", "街角咖啡"]
    amounts = ["0.08", "0.36", "0.5", "1.25", "2", "0.75", "3.6", "0.12"]
    cases, timings = [], []
    os.makedirs(work, exist_ok=True)
    for v in voices:
        for k in range(n_per_voice):
            amount, merchant = rng.choice(amounts), rng.choice(merchants)
            code = f"{rng.randrange(10000):04d}"
            text = f"核准 {amount} USDC，付給{merchant}，代碼 {' '.join(code)}"
            wav = os.path.join(work, f"{v}-{k}.wav")
            tts(v, text, wav)
            t0 = time.monotonic()
            status, a = side.request("POST", "/v1/analyze", open(wav, "rb").read())
            timings.append(time.monotonic() - t0)
            cases.append({"voice": v, "amount": amount, "code": code, "merchant_pinyin": " ".join(lazy_pinyin(merchant)),
                          "transcript": a.get("transcript", ""), "pinyin": a.get("pinyin", ""), "status": status})
    print(f"phrase: {len(cases)} TTS phrases analysed", file=sys.stderr, flush=True)
    return cases, timings


def go_match(cases, work):
    path = os.path.join(work, "phrases.json")
    json.dump([{k: c[k] for k in ("transcript", "pinyin", "amount", "code", "merchant_pinyin")} for c in cases], open(path, "w"), ensure_ascii=False)
    env = dict(os.environ, APA_VOICE_PHRASES=path)
    subprocess.run(["go", "test", "./internal/signer/", "-run", "^TestVoicePhraseEval$", "-count=1"], cwd=REPO, env=env, check=True, capture_output=True)
    res = json.load(open(path + ".out"))
    os.remove(path)  # the transcripts are not kept
    os.remove(path + ".out")
    return res


def main():
    ap = argparse.ArgumentParser()
    ap.add_argument("--data", required=True)
    ap.add_argument("--models", required=True)
    ap.add_argument("--socket", required=True)
    ap.add_argument("--out", required=True)
    ap.add_argument("--phrases", type=int, default=25, help="TTS phrases per voice")
    ap.add_argument("--seed", type=int, default=20261015)
    args = ap.parse_args()
    os.environ["HF_HUB_OFFLINE"] = "1"
    os.makedirs(args.out, exist_ok=True)
    rng = random.Random(args.seed)
    report = {"seed": args.seed, "data": "LibriSpeech test-clean (openslr 12, CC BY 4.0); macOS TTS (local, synthetic)",
              "scope": "B: phrase + ASR + speaker; no anti-spoof"}
    side = Sidecar(args.socket)
    status, health = side.request("GET", "/v1/health")
    report["sidecar"] = health

    # 2 and 3 first: the sidecar holds both models; the speaker evaluation loads its own in this process.
    work = os.path.join(REPO, "var", "voice", "eval-tts")
    cases, timings = phrase_eval(side, work, rng, args.phrases)
    res = go_match(cases, work)
    ok = [all(r) for r in res]
    by = {}
    for c, r in zip(cases, res):
        b = by.setdefault(c["voice"], {"n": 0, "pass": 0, "amount": 0, "merchant": 0, "code": 0})
        b["n"] += 1
        b["pass"] += all(r)
        b["amount"] += r[0]
        b["merchant"] += r[1]
        b["code"] += r[2]
    timings.sort()
    report["phrase"] = {"n": len(ok), "pass": sum(ok), "pass_rate": sum(ok) / len(ok), "pass_ci95": wilson(sum(ok), len(ok)), "by_voice": by,
                        "latency_s": {"median": timings[len(timings) // 2], "p95": timings[int(len(timings) * 0.95) - 1], "max": timings[-1]},
                        "over_10s": sum(t > 10 for t in timings), "degradation_rate": sum(t > 10 for t in timings) / len(timings)}
    # 3 replay: each recording against the next case's code (another challenge).
    replay_cases = [dict(c, code=cases[(i + 1) % len(cases)]["code"]) for i, c in enumerate(cases) if cases[(i + 1) % len(cases)]["code"] != c["code"]]
    rres = go_match(replay_cases, work)
    report["replay"] = {"n": len(rres), "refused": sum(not all(r) for r in rres), "refused_rate": sum(not all(r) for r in rres) / len(rres),
                        "note": "a byte-identical recording is refused before the models (its SHA-256 is kept); this is a re-recorded or re-encoded old phrase against a new code"}

    # 4 synthetic speakers: one TTS voice's template against the other's phrases.
    import numpy as np  # noqa: E402  (after the sidecar work)
    embs = {}
    for i, c in enumerate(cases):
        _, a = side.request("POST", "/v1/analyze", open(os.path.join(work, f"{c['voice']}-{i % args.phrases}.wav"), "rb").read())
        embs.setdefault(c["voice"], []).append(np.array(a["embedding"]))

    out, milli = speaker_eval(args.data, args.models, rng)
    report["speaker"] = out
    tpl = {v: (lambda m: m / np.linalg.norm(m))(np.mean([e / np.linalg.norm(e) for e in es[:3]], axis=0)) for v, es in embs.items()}
    cross = {}
    for v, es in embs.items():
        for o in embs:
            if o != v:
                s = [float(e / np.linalg.norm(e) @ tpl[o]) for e in es[3:]]
                cross[f"{v} vs template of {o}"] = {"n": len(s), "accepted": sum(x >= milli / 1000 for x in s)}
        s = [float(e / np.linalg.norm(e) @ tpl[v]) for e in es[3:]]
        cross[f"{v} vs own template"] = {"n": len(s), "accepted": sum(x >= milli / 1000 for x in s)}
    report["synthetic_speakers"] = cross
    json.dump(report, open(os.path.join(args.out, "v1-eval.json"), "w"), indent=2, ensure_ascii=False)
    print(json.dumps(report, indent=2, ensure_ascii=False))


if __name__ == "__main__":
    main()
