#!/bin/sh
# The history replay of one recorded run (docs/cli-poc.md 54), started from
# a copy. Usage, from anywhere:
#   APA_ADDR=127.0.0.1:8402 sh scripts/demo/history.sh <evidence directory>
# The evidence directory holds db/apa-live.db and exactly one runs/*.jsonl
# (as artifacts/b1-04/live-purchase-20261008T091741Z/ does). Only those two
# files are read: nothing else in it is opened, listed or copied.
#
# Every step stops the start on failure, before the host runs: a missing or
# linked file, a -wal or -shm beside the database (it may still be open),
# the private directory, a copy, a copy that differs, the build. The copy
# goes into a new var/history-XXXXXX (0700) and is never overwritten; the
# evidence is never written, moved, deleted or chmod-ed, and a directory
# left by a stopped start is left for the operator. The host serves the copy
# read only (no CLI, no expiry, no control, no payment). This script prints
# no token; they are in <copy>/tok/tokens.json.
#
# Environment: APA_ADDR (required by the host), APA_UNITY_PROJECT (default
# unity/AgentPaymentAuthority), APA_HISTORY_DIR (where the copy directory is
# made; default var/ of this repository).
set -u
stop() { echo "history: STOP（未啟動）：$1" >&2; exit 2; }

[ $# -eq 1 ] || stop "usage: history.sh <evidence directory>"
ev=$(cd "$1" && pwd) || stop "no evidence directory $1"
root=$(cd "$(dirname "$0")/../.." && pwd) || stop "no repository directory"
cd "$root" || stop "no repository directory"
db=$ev/db/apa-live.db
[ -f "$db" ] && [ ! -L "$db" ] || stop "${db} is not a regular file"
[ ! -e "$db-wal" ] && [ ! -e "$db-shm" ] || stop "${db} has a -wal or -shm beside it: it may still be open"
set -- "$ev"/runs/*.jsonl
[ $# -eq 1 ] && [ -f "$1" ] && [ ! -L "$1" ] || stop "${ev}/runs must hold exactly one .jsonl trace, a regular file"
trace=$1

base=${APA_HISTORY_DIR:-$root/var}
[ -d "$base" ] || mkdir -m 700 "$base" || stop "cannot make ${base}"
w=$(mktemp -d "$base/history-XXXXXX") || stop "cannot make a private directory under ${base}"
mkdir -m 700 "$w/db" "$w/tok" || stop "cannot make ${w}/db and ${w}/tok"
cp "$db" "$w/db/apa-live.db" || stop "cannot copy the database into ${w}"
cp "$trace" "$w/trace.jsonl" || stop "cannot copy the trace into ${w}"
cmp -s "$db" "$w/db/apa-live.db" && cmp -s "$trace" "$w/trace.jsonl" || stop "a copy in ${w} differs from its original"
go build -o "$w/demo" ./cmd/demo || stop "the demo host does not build"

proj=${APA_UNITY_PROJECT:-unity/AgentPaymentAuthority}
echo "history: copy in ${w}; the evidence is only read. Viewer page http://${APA_ADDR:-?}/ ; tokens in ${w}/tok/tokens.json (not printed)" >&2
APA_DEMO_TOKEN_FILE=$w/tok/tokens.json exec "$w/demo" --history-db "$w/db/apa-live.db" --history-trace "$w/trace.jsonl" \
	--registry testdata/demo/registry.json --unity-build "$proj/Build/WebGL/Build"
