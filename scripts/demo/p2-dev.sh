#!/bin/sh
# P2-a development Signer (docs/commercial-p2-host.md 12.8): apa-signer serve
# under your own user in var/p2-dev (0700), signing payments of the two
# wallets you made with apa-signer keygen and serving the person's approvals
# on the same socket. No /opt, no sudo. This script never reads a key file:
# it only checks they exist; the passphrase is asked by apa-signer itself on
# this terminal.
#
#   sh scripts/demo/p2-dev.sh 0x<wallet-a> 0x<wallet-b>
#
# The addresses are the ones apa-signer keygen printed (and p2-local.sh was
# given). The configuration: those wallets, test USDC at the fixture asset
# address, payments.json's delegations (0.10 test USDC a payment, 20.00 in
# all, for a week from now) to the fixture merchant, one approver each on
# http://localhost:8402, and the reservation section of the local anvil's
# budget contract (from var/p2-local/payments.json; P2_LOCAL_DIR names
# another). The addresses must be p2-local's. Ctrl-C stops the Signer.
set -eu
[ $# -eq 2 ] || { echo "usage: sh scripts/demo/p2-dev.sh 0x<wallet-a> 0x<wallet-b>" >&2; exit 2; }
for a in "$1" "$2"; do
  echo "$a" | grep -Eq '^0x[0-9a-f]{40}$' || { echo "p2-dev: $a is not a lowercase 0x address (as keygen printed it)" >&2; exit 2; }
done
root=$(cd "$(dirname "$0")/../.." && pwd)
dir="$root/var/p2-dev"
local_dir=${P2_LOCAL_DIR:-$root/var/p2-local}
mkdir -p "$dir/data/keys" "$dir/run" "$dir/bin"
chmod 700 "$dir" "$dir/data" "$dir/data/keys" "$dir/run" "$dir/bin"
for w in wallet-a wallet-b; do
  [ -f "$dir/data/keys/$w.key" ] || { echo "p2-dev: no $dir/data/keys/$w.key: make it first with
  $dir/bin/apa-signer keygen --dir $dir/data --wallet $w
(build it with: go build -o $dir/bin/apa-signer ./cmd/apa-signer)" >&2; exit 1; }
done
[ -f "$local_dir/payments.json" ] || { echo "p2-dev: no $local_dir/payments.json: run scripts/demo/p2-local.sh first" >&2; exit 1; }
[ -f "$local_dir/registry.json" ] || { echo "p2-dev: no $local_dir/registry.json: run scripts/demo/p2-local.sh first" >&2; exit 1; }
# The same addresses p2-local minted to and wrote into its registry.
python3 -I -c 'import json,sys; w={x["wallet_id"]: x["from"] for x in json.load(open(sys.argv[1]))["wallets"]}; sys.exit(0 if (w.get("wallet-a"), w.get("wallet-b")) == (sys.argv[2], sys.argv[3]) else 1)' \
  "$local_dir/registry.json" "$1" "$2" || { echo "p2-dev: $1, $2 are not wallet-a, wallet-b of $local_dir/registry.json (p2-local was given others)" >&2; exit 1; }
(cd "$root" && go build -o "$dir/bin/apa-signer" ./cmd/apa-signer)
ssock="$dir/run/signer.sock"
[ ! -e "$ssock" ] || { echo "p2-dev: $ssock exists: another Signer may be running; stop it (or remove the file if nothing runs)" >&2; exit 1; }

A="$1" B="$2" SSOCK="$ssock" PAY="$local_dir/payments.json" OUT="$dir/data/signer.json" UID_="$(id -u)" python3 -I "$root/scripts/demo/p2-signer-config.py"

cat <<EOF >&2
p2-dev: $dir/data/signer.json written (wallets $1, $2; reservations on $(python3 -I -c 'import json,sys; print(json.load(open(sys.argv[1]))["rpc"])' "$local_dir/payments.json"))
  Signer socket:  $ssock
  admin command:  $dir/bin/apa-signer approver ... --dir $dir/data
  host (another terminal):  make present-p2 SIGNER=$ssock AUTH=device
apa-signer asks for the wallets' passphrase now; Ctrl-C stops it.
EOF
exec "$dir/bin/apa-signer" serve --dir "$dir/data"
