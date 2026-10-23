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
[ $resume = no ] || cp "$state" "$state.bak.tmp" # the saved chain before anvil writes again (review: no half-written copy)
# Closing the window sends HUP to everything: anvil ignores it and stops only
# by this script's TERM, which waits for its last save (review).
(trap '' HUP; exec "$anvil" --host 127.0.0.1 --port "$port" --chain-id 84532 --silent --state "$state" --state-interval 1) &
pid=$!
# Stopping waits for anvil's last save (review): a run started right after never reads a half-written state.
trap 'kill $pid 2>/dev/null; wait $pid 2>/dev/null' INT TERM HUP EXIT
i=0
until curl -s --max-time 2 -o /dev/null -X POST -H 'Content-Type: application/json' --data '{"jsonrpc":"2.0","id":1,"method":"eth_chainId","params":[]}' $rpc; do
  kill -0 $pid 2>/dev/null || { echo "anvil exited" >&2; exit 1; }
  i=$((i + 1)); [ $i -lt 50 ] || { echo "anvil did not answer" >&2; exit 1; }; sleep 0.2
done
if [ $resume = yes ]; then
  # The Host and the Signer ask payments.json's node: it must be this one.
  kept_rpc=$(python3 -I -c 'import json,sys; print(json.load(open(sys.argv[1]))["rpc"])' "$out/payments.json")
  [ "$kept_rpc" = "$rpc" ] || { echo "p2-local: $out's chain was on $kept_rpc, not $rpc (P2_LOCAL_PORT)" >&2; exit 1; }
  # The kept chain has a budget contract where payments.json says (anvil's
  # addresses are the same on every new chain, so this is a sanity check only).
  contract=$(python3 -I -c 'import json,sys; print(json.load(open(sys.argv[1]))["budget_contract"])' "$out/payments.json")
  code=$(curl -s --max-time 5 -X POST -H 'Content-Type: application/json' --data '{"jsonrpc":"2.0","id":1,"method":"eth_getCode","params":["'"$contract"'","latest"]}' $rpc | python3 -I -c 'import json,sys; print(json.load(sys.stdin).get("result",""))')
  [ ${#code} -gt 4 ] || { echo "p2-local: the kept chain has no budget contract at $contract (anvil-state.json is not this directory's chain, or was not saved)" >&2; exit 1; }
  # Anvil resumes its clock at the saved block's time, so a chain that was off
  # for an hour runs an hour behind; the Signer's authorization (valid after
  # now - 1, by this machine's clock) would then be "not yet valid" on chain
  # and the shop refuses it (VERIFICATION_FAILED, 10/24). Set to now + 1 (a
  # second ticking over during the call must not leave it behind), and one
  # block mined at now: the shop's check is an eth_call at "latest", which runs
  # at the last mined block's time (review).
  for m in '"evm_setTime","params":['"$(($(date +%s) + 1))"']' '"evm_mine","params":[]'; do
    curl -s --max-time 5 -X POST -H 'Content-Type: application/json' --data '{"jsonrpc":"2.0","id":1,"method":'"$m"'}' $rpc | grep -q '"result"' ||
      { echo "p2-local: the kept chain's clock could not be set to now ($m)" >&2; exit 1; }
  done
  mv "$state.bak.tmp" "$state.bak" # the chain as it resumed: a later half-written save still leaves this one
  echo "p2-local: resumed the kept chain of $out (budget $contract, block $(curl -s -X POST -H 'Content-Type: application/json' --data '{"jsonrpc":"2.0","id":1,"method":"eth_blockNumber","params":[]}' $rpc | python3 -I -c 'import json,sys; print(int(json.load(sys.stdin)["result"],16))'))"
else
  go run ./cmd/p2-local --rpc "$rpc" --wallet-a "$1" --wallet-b "$2" --out "$out" --zkbudget "$out/zkbudget"
fi
echo "p2-local: anvil is running (pid $pid); Ctrl-C stops it and keeps the chain in $state"
wait $pid
