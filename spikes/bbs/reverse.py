#!/usr/bin/env python3
"""C1 spike reverse checks: remove one verification in a disposable copy
of this module and require the named test to fail with its expected
assertion (a build error or any other failure does not count).

  python3 -I spikes/bbs/reverse.py OUT_DIR [name regex]
"""
import os, re, shutil, subprocess, sys, tempfile

HERE = os.path.dirname(os.path.abspath(__file__))
SRC = "credential.go"
ITEMS = [  # (name, test filter, expected regex, old, new)
    ("expiry", "^TestRefusals$/^expired$", r"expired: <nil>, want CREDENTIAL_EXPIRED", "\tif now >= exp {", "\tif false {"),
    ("holder binding", "^TestRefusals$/^holder_signature_by_another_key$", r"holder signature by another key: <nil>",
     "\tif !ed25519.Verify(holder, hp, p.HolderSig) {", "\tif false && !ed25519.Verify(holder, hp, p.HolderSig) {"),
    ("nonce binds the intent", "^TestRefusals$/^other_intent$", r"other intent: <nil>",
     '"agent-payment-authority/c1-spike/presentation/v1", r.VerifierID, r.Challenge, r.IntentHash}',
     '"agent-payment-authority/c1-spike/presentation/v1", r.VerifierID, r.Challenge}'),
    ("replay", "^TestReplay$", r"replay: <nil>", "\tdelete(v.outstanding, req.Challenge)\n", "\n"),
    ("revocation", "^TestRefusals$/^revoked$", r"revoked: <nil>, want CREDENTIAL_REVOKED", '\tif v.Revoked[d["status"]] {', "\tif false {"),
    ("exact disclosure", "^TestRefusals$/^extra_field_disclosed$", r"extra field disclosed: <nil>", "\tif len(p.Disclosed) != len(idx) {", "\tif false {"),
    ("issuer id", "^TestRefusals$/^untrusted_issuer_id$", r"untrusted issuer id: <nil>", '\tif d["issuer"] != v.IssuerID {', "\tif false {"),
    # Review must-fix: the proof's revealed set and message count are the request's.
    ("revealed set bound to the request", "^TestRefusals$/^proof_reveals_a_subset;_forged_holder_and_status$", r"proof reveals a subset; forged holder and status: <nil>",
     "err != nil || pl.MessagesCount != len(Fields) || !slices.Equal(pl.Revealed, idx) {",
     "false && (err != nil || pl.MessagesCount != len(Fields) || !slices.Equal(pl.Revealed, idx)) {"),
    ("mandatory fields", "^TestRefusals$/^mandatory_field_hidden$", r"mandatory field hidden: STATUS_INVALID, want FIELD_MISSING",
     "\t\tif _, ok := p.Disclosed[f]; !ok {", "\t\tif _, ok := p.Disclosed[f]; false && !ok {"),
    ("expiry format", "^TestRefusals$/^expiry_with_a_plus$", r"expiry with a plus: <nil>", '\tif !format["expiry"].MatchString(d["expiry"]) {', "\tif false {"),
    ("status format", "^TestRefusals$/^status_with_a_leading_zero$", r"status with a leading zero: <nil>", '\tif !format["status"].MatchString(d["status"]) {', "\tif false {"),
]


def main():
    out = sys.argv[1]
    os.makedirs(out, exist_ok=True)
    env = dict(os.environ, GOFLAGS="-mod=readonly", GOPROXY="off", GOCACHE=os.environ.get("GOCACHE") or os.path.join(tempfile.gettempdir(), "gocache"))
    caught = 0
    only = re.compile(sys.argv[2]) if len(sys.argv) > 2 else None
    items = [(n, it) for n, it in enumerate(ITEMS, 1) if not only or only.search(it[0])]
    for n, (name, run, rx, old, new) in items:
        tmp = tempfile.mkdtemp(prefix="c1rev-")
        copy = os.path.join(tmp, "bbs")
        shutil.copytree(HERE, copy, ignore=shutil.ignore_patterns("reverse.py", "__pycache__"))
        path = os.path.join(copy, SRC)
        text = open(path).read()
        if text.count(old) != 1:
            result, why = "not-caught", f"anchor found {text.count(old)} times"
            log = why
        else:
            open(path, "w").write(text.replace(old, new))
            p = subprocess.run(["go", "test", "-count=1", "-run", run, "."], cwd=copy, env=env, capture_output=True, text=True, timeout=600)
            log = p.stdout + p.stderr
            if "[build failed]" in log or "[setup failed]" in log:
                result, why = "not-caught", "build failed"
            elif p.returncode != 1 or not re.search(rx, log):
                result, why = "not-caught", f"exit {p.returncode}; expected /{rx}/ not found"
            else:
                result, why = "caught", re.search(rx, log).group(0)
                caught += 1
        open(os.path.join(out, f"{n:02d}-{name.replace(' ', '-')}.log"), "w").write(f"item: {name}\nrun: {run}\nexpects: {rx}\nmutation: {old!r} -> {new!r}\n\n{log}")
        print(f"RESULT {result} | {name} | {why}")
    print(f"{caught}/{len(items)} caught by the expected assertion")
    return 0 if caught == len(items) else 1


if __name__ == "__main__":
    sys.exit(main())
