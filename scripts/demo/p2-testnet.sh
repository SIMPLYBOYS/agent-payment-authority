#!/bin/sh
# P2 on the Base Sepolia testnet (docs/commercial-p2b.md 6): builds
# apa-relayer, p2-testnet and zkbudget into var/p2-testnet (0700; P2_TESTNET_DIR
# names another) and runs one step. Each step that reaches the testnet runs
# only once Aaron approves it (§6); nothing here reads a key file except
# p2-testnet deploy (the issuer's, with its passphrase on this terminal) and
# apa-relayer serve (its three, likewise).
#
#   sh scripts/demo/p2-testnet.sh keygen                       # §6 step 2: the four Host keys (relayer-gas, issuer and policy: one passphrase)
#   sh scripts/demo/p2-testnet.sh preflight 0x<wallet-a> 0x<wallet-b> [--test402 URL]   # step 1 (read only)
#   sh scripts/demo/p2-testnet.sh deploy 0x<wallet-a> 0x<wallet-b>                      # step 4 (2 transactions)
#   sh scripts/demo/p2-testnet.sh relayer                      # apa-relayer serve (Ctrl-C stops it)
#
# P2_TESTNET_RPC names another node (P2b-D1: the public sepolia.base.org).
set -eu
root=$(cd "$(dirname "$0")/../.." && pwd)
cd "$root"
dir=${P2_TESTNET_DIR:-$root/var/p2-testnet}
mkdir -p "$dir/keys" "$dir/bin" && chmod 700 "$dir" "$dir/keys" "$dir/bin"
dir=$(cd "$dir" && pwd) # absolute: zkbudget is built from zk/
build() {
  go build -o "$dir/bin/apa-relayer" ./cmd/apa-relayer
  go build -o "$dir/bin/p2-testnet" ./cmd/p2-testnet
  (cd zk && go build -o "$dir/zkbudget" ./cmd/zkbudget)
}
rpc=${P2_TESTNET_RPC:-https://sepolia.base.org}
[ $# -ge 1 ] || { sed -n '9,12p' "$0" >&2; exit 2; }
cmd=$1; shift
build
case $cmd in
keygen)
  for role in relayer-gas issuer policy shop; do
    if [ -f "$dir/keys/$role.key" ]; then echo "p2-testnet: $role.key exists; kept" >&2; continue; fi
    "$dir/bin/apa-relayer" keygen --dir "$dir" --role "$role"
  done ;;
preflight|deploy)
  [ $# -ge 2 ] || { echo "usage: sh scripts/demo/p2-testnet.sh $cmd 0x<wallet-a> 0x<wallet-b>" >&2; exit 2; }
  # The verifier deployed must be zk/setup's, from a build newer than its sources.
  cmp -s contracts/src/Verifier.sol zk/setup/Verifier.sol || { echo "p2-testnet: contracts/src/Verifier.sol is not zk/setup's" >&2; exit 1; }
  for c in Verifier Budget; do
    [ "var/contracts/out/$c.sol/$c.json" -nt "contracts/src/$c.sol" ] || { echo "p2-testnet: var/contracts/out/$c.sol is older than contracts/src/$c.sol: build the contracts first (forge build in contracts/)" >&2; exit 1; }
  done
  a=$1; b=$2; shift 2
  "$dir/bin/p2-testnet" "$cmd" --dir "$dir" --rpc "$rpc" --wallet-a "$a" --wallet-b "$b" "$@" ;;
relayer)
  [ -f "$dir/relayer.json" ] || { echo "p2-testnet: no $dir/relayer.json: run deploy first" >&2; exit 1; }
  exec "$dir/bin/apa-relayer" serve --dir "$dir" ;;
*) echo "p2-testnet: keygen, preflight, deploy or relayer" >&2; exit 2 ;;
esac
