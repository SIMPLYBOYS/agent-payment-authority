#!/bin/sh
# P2-a local payments (docs/commercial-p2-host.md 12.3): a fresh anvil on
# 127.0.0.1:8545 as chain 84532, set up by cmd/p2-local for the two payer
# wallets you made with apa-signer keygen. Anvil's public keys only; no
# payment key is read here.
#
#   sh scripts/demo/p2-local.sh 0x<wallet-a> 0x<wallet-b>
#
# It writes var/p2-local (0700; P2_LOCAL_DIR names another): payments.json, registry.json, the
# zkbudget binary and, once the demo runs, the relayer's and shop's
# databases. Ctrl-C stops anvil; its chain is gone with it (start this
# again, and the demo with a new APA_DB_PATH, for a new one).
set -eu
[ $# -eq 2 ] || { echo "usage: sh scripts/demo/p2-local.sh 0x<wallet-a> 0x<wallet-b>" >&2; exit 2; }
root=$(cd "$(dirname "$0")/../.." && pwd)
cd "$root"
anvil=var/tools/foundry-v1.8.3/bin/anvil
[ -x "$anvil" ] || { echo "no local anvil at $anvil" >&2; exit 1; }
out=${P2_LOCAL_DIR:-var/p2-local}
mkdir -p "$out" && chmod 700 "$out"
out=$(cd "$out" && pwd)
(cd zk && go build -o "$out/zkbudget" ./cmd/zkbudget)
"$anvil" --host 127.0.0.1 --port 8545 --chain-id 84532 --silent &
pid=$!
trap 'kill $pid 2>/dev/null' INT TERM EXIT
i=0
until curl -s -o /dev/null -X POST -H 'Content-Type: application/json' --data '{"jsonrpc":"2.0","id":1,"method":"eth_chainId","params":[]}' http://127.0.0.1:8545; do
  i=$((i + 1)); [ $i -lt 50 ] || { echo "anvil did not answer" >&2; exit 1; }; sleep 0.2
done
go run ./cmd/p2-local --wallet-a "$1" --wallet-b "$2" --out "$out" --zkbudget "$out/zkbudget"
echo "p2-local: anvil is running (pid $pid); Ctrl-C stops it"
wait $pid
