#!/bin/sh
# The demo host's live mode (docs/cli-poc.md 55): a real Agent run that the
# operator starts from /host. Usage, from anywhere:
#   APA_ADDR=127.0.0.1:8402 APA_LIVE_CLI=true sh scripts/demo/live.sh <absolute claude path> [--dev-demo-plugins]
#
# It makes a new session directory var/live-XXXXXX (0700) with a new
# database, trace directory and token file, builds the host and starts it.
# Every step stops the start on failure (exit 2), before the host runs; no
# existing database is opened, nothing is deleted or overwritten. Starting
# the host runs no model: before it serves, the host checks the APA-only
# config directory and the approved `claude --version`; a run starts only
# when the operator presses start on /host, one at a time, never retried by
# itself. This script prints no token; they are in <session>/tok/tokens.json.
#
# After an interruption, start the same session again (its runs show as
# interrupted; nothing runs again by itself):
#   APA_DB_PATH=<session>/db/live.db APA_DEMO_TOKEN_FILE=<session>/tok/tokens.json \
#     <session>/demo --live-cli <claude> [--dev-demo-plugins] --scenarios testdata/demo/scenarios-live.json \
#     --registry testdata/demo/registry.json --trace-dir <session>/runs
#
# Environment: APA_ADDR and APA_LIVE_CLI=true (both required by the host),
# APA_UNITY_PROJECT (default unity/AgentPaymentAuthority), APA_LIVE_DIR
# (where the session directory is made; default var/ of this repository).
set -u
stop() { echo "live: STOP（未啟動）：$1" >&2; exit 2; }

[ $# -ge 1 ] && [ $# -le 2 ] || stop "usage: live.sh <absolute claude path> [--dev-demo-plugins]"
cli=$1
dev=${2:-}
[ -z "$dev" ] || [ "$dev" = --dev-demo-plugins ] || stop "the second argument may only be --dev-demo-plugins"
case $cli in /*) ;; *) stop "the Claude Code path must be absolute; no PATH lookup is done" ;; esac
[ "${APA_LIVE_CLI:-}" = true ] || stop "APA_LIVE_CLI=true is required"
root=$(cd "$(dirname "$0")/../.." && pwd) || stop "no repository directory"
cd "$root" || stop "no repository directory"

base=${APA_LIVE_DIR:-$root/var}
[ -d "$base" ] || mkdir -m 700 "$base" || stop "cannot make ${base}"
w=$(mktemp -d "$base/live-XXXXXX") || stop "cannot make a session directory under ${base}"
mkdir -m 700 "$w/db" "$w/runs" "$w/tok" || stop "cannot make ${w}/db, runs and tok"
go build -o "$w/demo" ./cmd/demo || stop "the demo host does not build"

proj=${APA_UNITY_PROJECT:-unity/AgentPaymentAuthority}
echo "live: session ${w}: new database db/live.db, traces runs/, tokens tok/tokens.json (not printed). Nothing runs until the operator starts a run on http://${APA_ADDR:-?}/host" >&2
APA_DB_PATH=$w/db/live.db APA_DEMO_TOKEN_FILE=$w/tok/tokens.json exec "$w/demo" --live-cli "$cli" $dev \
	--scenarios testdata/demo/scenarios-live.json --registry testdata/demo/registry.json --trace-dir "$w/runs" \
	--unity-build "$proj/Build/WebGL/Build"
