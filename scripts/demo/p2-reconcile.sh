#!/bin/sh
# Saves the running P2 host's reconciliation (GET /api/host/reconciliation;
# docs/commercial-p2-host.md 12.7) to OUT (default
# var/p2-local/reconciliation-<time>.json) and prints its counts, for
# ae1-eval report --reconciliation. The host token is read from the newest
# present session's token file and never printed.
#
#   sh scripts/demo/p2-reconcile.sh [OUT]
set -eu
umask 077
root=$(cd "$(dirname "$0")/../.." && pwd)
tok=$(ls -td ${P2_PRESENT_DIR:-$root/var}/present-*/tok/tokens.json 2>/dev/null | head -1)
[ -n "$tok" ] || { echo "p2-reconcile: no var/present-*/tok/tokens.json: is make present-p2 running?" >&2; exit 1; }
out=${1:-$root/var/p2-local/reconciliation-$(date +%Y%m%d-%H%M%S).json}
host=$(python3 -I -c 'import json,sys; print(next(t["token"] for t in json.load(open(sys.argv[1]))["tokens"] if t["role"] == "host"))' "$tok")
# The token goes to curl on its standard input, never on its command line (ps).
# The host reads one reconciliation at a time and answers 503
# reconciliation_pending until the first is read (minutes on a public node).
i=0
while :; do
  code=$(printf 'Authorization: Bearer %s\n' "$host" | curl -s -o "$out" -w '%{http_code}' --max-time 60 -H @- http://127.0.0.1:8402/api/host/reconciliation) || code=000
  [ "$code" = 503 ] && grep -q reconciliation_pending "$out" 2>/dev/null || break
  i=$((i + 1)); [ $i -lt 70 ] || { echo "p2-reconcile: the host is still reading after 12 minutes; ask again later" >&2; exit 1; }
  echo "p2-reconcile: the host is still reading the chain; asking again in 10 s" >&2
  sleep 10
done
chmod 600 "$out" 2>/dev/null || true # an OUT that existed keeps its mode under umask
[ "$code" = 200 ] || { echo "p2-reconcile: the host answered $code (000: no answer: is make present-p2 running?): $(head -c 300 "$out" 2>/dev/null)" >&2; exit 1; }
python3 -I -c 'import json,sys; r=json.load(open(sys.argv[1])); print("p2-reconcile: %s, blocks %d-%d, %d wallets, %d recorded authorizations: %d unauthorized new spend(s); saved %s" % (r["environment"], r["from_block"], r["to_block"], len(r["wallets"]), r["known_authorizations"], len(r["unauthorized"]), sys.argv[1]))' "$out"
