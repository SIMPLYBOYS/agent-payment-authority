#!/bin/sh
# P2-a local payments (docs/commercial-p2-host.md 12.3): a fresh anvil on
# 127.0.0.1:8545 as chain 84532, set up by cmd/p2-local for the two payer
# wallets you made with apa-signer keygen. Anvil's public keys only; no
# payment key is read here.
#
#   sh scripts/demo/p2-local.sh 0x<wallet-a> 0x<wallet-b>
#
# It writes var/p2-local (0700; P2_LOCAL_DIR names another): payments.json, registry.json, the
# zkbudget binary, anvil-state.json and, once the demo runs, the relayer's
# and shop's databases. The chain is kept in anvil-state.json (on exit and
# every second; Aaron 10/23: the chain must start again any time and work):
# the same directory again resumes the same chain, its contracts, balances
# and transactions, so the relayer's, shop's and Host's databases still hold.
# A directory of a chain that was not kept (no anvil-state.json) is
# refused; give a new P2_LOCAL_DIR for a new chain.
set -eu
port=${P2_LOCAL_PORT:-8545} # another port runs a second local chain beside the first (tests)
rpc=http://127.0.0.1:$port
[ $# -eq 2 ] || { echo "usage: sh scripts/demo/p2-local.sh 0x<wallet-a> 0x<wallet-b>" >&2; exit 2; }
root=$(cd "$(dirname "$0")/../.." && pwd)
cd "$root"
anvil=var/tools/foundry-v1.8.3/bin/anvil
[ -x "$anvil" ] || { echo "no local anvil at $anvil" >&2; exit 1; }
if curl -s --max-time 2 -o /dev/null $rpc; then
  echo "something already listens on 127.0.0.1:$port; stop it first" >&2; exit 1
fi
out=${P2_LOCAL_DIR:-var/p2-local}
mkdir -p "$out" && chmod 700 "$out"
out=$(cd "$out" && pwd)
state="$out/anvil-state.json"
resume=no
if [ -f "$out/payments.json" ]; then
  [ -f "$state" ] || { echo "p2-local: $out holds an earlier chain that was not kept (no anvil-state.json); name a new P2_LOCAL_DIR" >&2; exit 1; }
  resume=yes
fi
[ -x "$out/zkbudget" ] || (cd zk && go build -o "$out/zkbudget" ./cmd/zkbudget)
"$anvil" --host 127.0.0.1 --port "$port" --chain-id 84532 --silent --state "$state" --state-interval 1 &
pid=$!
trap 'kill $pid 2>/dev/null' INT TERM EXIT
i=0
until curl -s --max-time 2 -o /dev/null -X POST -H 'Content-Type: application/json' --data '{"jsonrpc":"2.0","id":1,"method":"eth_chainId","params":[]}' $rpc; do
  kill -0 $pid 2>/dev/null || { echo "anvil exited" >&2; exit 1; }
  i=$((i + 1)); [ $i -lt 50 ] || { echo "anvil did not answer" >&2; exit 1; }; sleep 0.2
done
if [ $resume = yes ]; then
  # The kept chain must be this directory's: its budget contract has code.
  contract=$(python3 -I -c 'import json,sys; print(json.load(open(sys.argv[1]))["budget_contract"])' "$out/payments.json")
  code=$(curl -s --max-time 5 -X POST -H 'Content-Type: application/json' --data '{"jsonrpc":"2.0","id":1,"method":"eth_getCode","params":["'"$contract"'","latest"]}' $rpc | python3 -I -c 'import json,sys; print(json.load(sys.stdin).get("result",""))')
  [ ${#code} -gt 4 ] || { echo "p2-local: the kept chain has no budget contract at $contract: anvil-state.json is not this directory's chain" >&2; exit 1; }
  echo "p2-local: resumed the kept chain of $out (budget $contract, block $(curl -s -X POST -H 'Content-Type: application/json' --data '{"jsonrpc":"2.0","id":1,"method":"eth_blockNumber","params":[]}' $rpc | python3 -I -c 'import json,sys; print(int(json.load(sys.stdin)["result"],16))'))"
else
  go run ./cmd/p2-local --rpc "$rpc" --wallet-a "$1" --wallet-b "$2" --out "$out" --zkbudget "$out/zkbudget"
fi
echo "p2-local: anvil is running (pid $pid); Ctrl-C stops it and keeps the chain in $state"
wait $pid
