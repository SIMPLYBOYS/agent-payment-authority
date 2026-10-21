#!/bin/sh
# V1 development mode (docs/commercial-v1.md 13): the voice sidecar and
# apa-signer serve-approvals under your own user, in var/v1-dev (git
# ignored, 0700). No /opt/apa-signer, no sudo, no payment key.
#
#   sh scripts/demo/v1-dev.sh        start both and wait; Ctrl-C stops them
#
# The Signer's configuration: the demo registry's two public wallets, one
# approver each, the relying party http://localhost:8402, and the voice
# section (threshold 365/1000 from the evaluation; the model versions the
# sidecar reports). Its data (signer.db, voice.key, the sealed voice
# template) stays in var/v1-dev/data until you remove it. An A2-era
# approvals.db there is moved into signer.db by you, once (P2-D1).
set -eu
root=$(cd "$(dirname "$0")/../.." && pwd)
dir="$root/var/v1-dev"
mkdir -p "$dir/data" "$dir/run" "$dir/bin"
chmod 700 "$dir" "$dir/data" "$dir/run" "$dir/bin"
py="$root/var/voice/.venv/bin/python"
[ -x "$py" ] || { echo "v1-dev: var/voice/.venv is missing (docs/commercial-v1.md 2)" >&2; exit 1; }
(cd "$root" && go build -o "$dir/bin/apa-signer" ./cmd/apa-signer)
[ ! -e "$dir/data/approvals.db" ] || { echo "v1-dev: $dir/data/approvals.db is from before P2: move it into signer.db once with
  $dir/bin/apa-signer approver migrate --dir $dir/data
(the old file stays as approvals.db.migrated)" >&2; exit 1; }

vsock="$dir/run/voice.sock"
ssock="$dir/run/signer.sock"
if [ -e "$ssock" ]; then
  # Nothing listening (curl exit 7: connection refused) is a stop that skipped
  # the cleanup, e.g. a closed terminal: that file alone is removed.
  rc=0
  curl -s -o /dev/null --max-time 2 --unix-socket "$ssock" http://signer/ </dev/null || rc=$? # set -e would stop at a bare failure
  [ $rc -eq 7 ] && [ -S "$ssock" ] || { echo "v1-dev: $ssock exists and may be in use (curl $rc): another Signer may be running; stop it first" >&2; exit 1; }
  echo "v1-dev: removing $ssock (nothing listens on it: a previous stop left it)" >&2
  rm -f "$ssock"
fi

pids=""
cleanup() { for p in $pids; do kill "$p" 2>/dev/null || true; done; wait 2>/dev/null || true; rm -f "$ssock"; }
trap cleanup EXIT
trap 'exit 130' INT TERM

"$py" -I "$root/voice/sidecar.py" --socket "$vsock" --models "$root/var/voice/models" 2>"$dir/run/sidecar.log" &
pids="$pids $!"
health=""
i=0
while [ $i -lt 120 ]; do
  health=$(curl -s --unix-socket "$vsock" http://voice/v1/health 2>/dev/null || true)
  case $health in *'"ok": true'*) break ;; esac
  sleep 1
  i=$((i + 1))
done
case $health in *'"ok": true'*) ;; *) echo "v1-dev: the voice sidecar did not start (see $dir/run/sidecar.log)" >&2; exit 1 ;; esac

HEALTH="$health" SSOCK="$ssock" VSOCK="$vsock" OUT="$dir/data/signer.json" UID_="$(id -u)" python3 -I - <<'EOF'
import json, os
h = json.loads(os.environ["HEALTH"])
cfg = {"version": 1, "socket": os.environ["SSOCK"], "callers": [int(os.environ["UID_"])], "max_window_seconds": 600,
       "wallets": [{"wallet_id": "wallet-a", "consumer_id": "consumer-a", "chain_id": "84532", "address": "0x1111111111111111111111111111111111111111"},
                   {"wallet_id": "wallet-b", "consumer_id": "consumer-b", "chain_id": "84532", "address": "0x2222222222222222222222222222222222222222"}],
       "assets": [{"chain_id": "84532", "asset_contract": "0x4444444444444444444444444444444444444444", "eip712_name": "USDC", "eip712_version": "2"}],
       "delegations": [],
       "approvers": [{"approver_id": "approver-a", "consumer_id": "consumer-a", "wallet_ids": ["wallet-a"], "signer_max": "1000000", "signer_total": "5000000", "signer_window_seconds": 86400, "version": 1},
                     {"approver_id": "approver-b", "consumer_id": "consumer-b", "wallet_ids": ["wallet-b"], "signer_max": "1000000", "signer_total": "5000000", "signer_window_seconds": 86400, "version": 1}],
       "webauthn": {"rp_id": "localhost", "origins": ["http://localhost:8402"], "challenge_ttl_seconds": 120, "approval_ttl_seconds": 300},
       "voice": {"socket": os.environ["VSOCK"], "timeout_ms": 10000, "speaker_threshold_milli": 365, "asr_model": h["asr"], "speaker_model": h["speaker"],
                 "decimals": 6, "merchants": [{"pay_to": "0x3333333333333333333333333333333333333333", "name": "測試商店", "pinyin": "ce shi shang dian"}]}}
fd = os.open(os.environ["OUT"], os.O_WRONLY | os.O_CREAT | os.O_TRUNC, 0o600)
os.write(fd, json.dumps(cfg, ensure_ascii=False, indent=1).encode())
os.close(fd)
EOF

# The demo registry plus show-a/b-21..100: present-a2 keeps its database
# (one per Signer), and the 20 show-* tasks of each customer run out.
python3 -I - "$root/testdata/demo/registry.json" "$dir/registry.json" <<'EOF'
import json, sys
r = json.load(open(sys.argv[1]))
have = {t["task_id"] for t in r["tasks"]}
for c in ("a", "b"):
    last = next(t for t in r["tasks"] if t["task_id"] == "show-" + c + "-20")
    for n in range(21, 101):  # ponytail: 80 more each; the Host reads at most 64 KiB of registry
        t = dict(last, task_id=f"show-{c}-{n:02d}", created_at=last["created_at"] + (n - 20) * 30)
        if t["task_id"] not in have:
            r["tasks"].append(t)
json.dump(r, open(sys.argv[2], "w"), separators=(",", ":"))
EOF

"$dir/bin/apa-signer" serve-approvals --dir "$dir/data" 2>"$dir/run/signer.log" &
pids="$pids $!"
i=0
while [ $i -lt 50 ] && [ ! -S "$ssock" ]; do sleep 0.2; i=$((i + 1)); done
[ -S "$ssock" ] || { echo "v1-dev: serve-approvals did not start (see $dir/run/signer.log)" >&2; exit 1; }

cat <<EOF
v1-dev: ready (sidecar $(echo "$health" | sed -E 's/.*"asr": "([^"]+)", "speaker": "([^"]+)".*/\1, \2/'); threshold 365/1000)
  Signer socket:  $ssock
  Signer data:    $dir/data
  admin command:  $dir/bin/apa-signer approver ... --dir $dir/data
  host (another terminal):  make present-a2 SIGNER=$ssock AUTH=device REGISTRY=$dir/registry.json
Leave this running; Ctrl-C stops the sidecar and the Signer.
EOF
wait
