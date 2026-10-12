#!/bin/sh
# One way to start a demo and open the host's page (docs/cli-poc.md 56).
#   sh scripts/demo/present.sh live    the real Claude Code CLI, one run per 「開始」 (make present-live)
#   sh scripts/demo/present.sh fake    the fake CLI, no model (make present-fake)
#
# Each start makes a new private session directory var/present-XXXXXX
# (database, traces, tokens), builds the host into it, starts it with
# --pairing and opens http://<addr>/host once the host itself says it serves
# (its ready file, with its own pid). The host shows a one-time pairing code
# on this terminal; the page trades it once for this run's credentials. No
# URL carries a secret; this script prints no token or code itself.
#
# Starting runs no model. In live mode the host does run `<cli> --version`
# once before it serves (no model request is sent; the CLI does start).
# A run starts only when the host presses 「開始」 on the page.
#
# Every failure stops here: a bad setting, the build (no older binary is
# started), the host stopping before it is ready (version or config
# refused, address in use: nothing is killed or retried). If the browser
# cannot be opened, the address is printed and the host keeps serving.
# Ctrl-C stops the host this script started, and only it; the session
# directory is kept.
#
# One-time setup for live mode, var/present.conf (plain lines, read, never
# sourced):
#   cli=/absolute/path/of/claude          (nothing searches for it)
#   dev_demo_plugins=true                 (the development demo exception; false: the strict gate)
#
# Environment: APA_ADDR (default 127.0.0.1:8402; 127.0.0.1:0 picks a free
# port), APA_UNITY_PROJECT, APA_PRESENT_CONF (default var/present.conf),
# APA_PRESENT_DIR (where the session directory is made; default var/),
# APA_PRESENT_OPEN (the command that opens the page; default open),
# APA_PRESENT_GO (default go). APA_PRESENT_SCENARIOS (fake only; default
# testdata/demo/scenarios.json): another trusted fake catalog, e.g. the
# rehearsal one (make present-rehearsal). The live catalog never changes.
set -u
stop() { echo "present: STOP（未啟動）：$1" >&2; exit 2; }

[ $# -eq 1 ] || stop "usage: present.sh live | fake"
mode=$1
case $mode in live | fake) ;; *) stop "usage: present.sh live | fake" ;; esac
root=$(cd "$(dirname "$0")/../.." && pwd) || stop "no repository directory"
cd "$root" || stop "no repository directory"
GO=${APA_PRESENT_GO:-go}
OPEN=${APA_PRESENT_OPEN:-open}
addr=${APA_ADDR:-127.0.0.1:8402}

if [ "$mode" = live ]; then
	conf=${APA_PRESENT_CONF:-$root/var/present.conf}
	[ -f "$conf" ] || stop "沒有 ${conf}：一次性準備見 docs/cli-poc.md 第 56 節（cli=<Claude Code 絕對路徑>、dev_demo_plugins=true|false）"
	bad=$(grep -nvE '^([[:space:]]*(#.*)?|cli=/[A-Za-z0-9_./@+-]+|dev_demo_plugins=(true|false))$' "$conf" | cut -d: -f1 | tr '\n' ' ')
	[ -z "$bad" ] || stop "${conf} 第 ${bad}行不是 cli=<絕對路徑> 或 dev_demo_plugins=true|false"
	[ "$(grep -c '^cli=' "$conf")" -eq 1 ] || stop "${conf} 需要恰好一行 cli="
	[ "$(grep -c '^dev_demo_plugins=' "$conf")" -eq 1 ] || stop "${conf} 需要恰好一行 dev_demo_plugins="
	cli=$(sed -n 's/^cli=//p' "$conf")
	dev=$(sed -n 's/^dev_demo_plugins=//p' "$conf")
	[ -f "$cli" ] && [ -x "$cli" ] || stop "cli=${cli} 不是可執行檔（不會自動尋找或安裝）"
fi

base=${APA_PRESENT_DIR:-$root/var}
[ -d "$base" ] || mkdir -m 700 "$base" || stop "cannot make ${base}"
w=$(mktemp -d "$base/present-XXXXXX") || stop "cannot make a session directory under ${base}"
mkdir -m 700 "$w/db" "$w/runs" "$w/tok" || stop "cannot make ${w}/db, runs and tok"
"$GO" build -o "$w/demo" ./cmd/demo || stop "建置失敗；沒有啟動任何程式（也不用舊的）"
if [ "$mode" = fake ]; then
	"$GO" build -o "$w/fake-claude" ./cmd/fake-claude || stop "fake CLI 建置失敗；沒有啟動任何程式"
fi
# What the binary says of its source, as Go recorded it (a modified tree says so).
"$GO" version -m "$w/demo" 2>/dev/null | grep -E 'vcs\.(revision|modified|time)' | tr -s '\t ' ' ' >"$w/build.txt"
echo "present: 場次 ${w}；建置：$(tr '\n' ' ' <"$w/build.txt")" >&2

proj=${APA_UNITY_PROJECT:-unity/AgentPaymentAuthority}
if [ "$mode" = live ]; then
	extra=""
	[ "$dev" = true ] && extra=--dev-demo-plugins
	echo "present: 真實 Agent 即時展示（開發展示例外：${dev}）。啟動時 host 執行一次 ${cli} --version（不送出模型請求）；按「開始」才執行模型。" >&2
	APA_ADDR=$addr APA_LIVE_CLI=true APA_DB_PATH=$w/db/live.db APA_DEMO_TOKEN_FILE=$w/tok/tokens.json "$w/demo" \
		--live-cli "$cli" $extra --scenarios testdata/demo/scenarios-live.json --registry testdata/demo/registry.json \
		--trace-dir "$w/runs" --unity-build "$proj/Build/WebGL/Build" --pairing --ready-file "$w/ready.json" &
else
	echo "present: fake 展示（不執行模型）。" >&2
	APA_ADDR=$addr APA_DB_PATH=$w/db/fake.db APA_DEMO_TOKEN_FILE=$w/tok/tokens.json "$w/demo" \
		--fake-cli "$w/fake-claude" --scenarios "${APA_PRESENT_SCENARIOS:-testdata/demo/scenarios.json}" --registry testdata/demo/registry.json \
		--trace-dir "$w/runs" --unity-build "$proj/Build/WebGL/Build" --pairing --ready-file "$w/ready.json" &
fi
pid=$!
trap 'kill -INT "$pid" 2>/dev/null' INT TERM

# Ready means this process says so: its ready file, with its own pid.
i=0
while :; do
	if ! kill -0 "$pid" 2>/dev/null; then
		wait "$pid"
		stop "host 在就緒前結束（exit $?）；沒有開啟頁面，也沒有重試（場次目錄 ${w} 保留）"
	fi
	if [ -f "$w/ready.json" ] && grep -q "\"pid\":${pid}[,}]" "$w/ready.json"; then
		break
	fi
	i=$((i + 1))
	if [ "$i" -ge 1200 ]; then
		kill -INT "$pid" 2>/dev/null
		wait "$pid"
		stop "120 秒內沒有就緒；已停止本次啟動的 host"
	fi
	sleep 0.1
done
url="http://$(sed -n 's/.*"addr":"\([^"]*\)".*/\1/p' "$w/ready.json")/host"
if ! "$OPEN" "$url" >/dev/null 2>&1; then
	echo "present: 無法自動開啟瀏覽器；請手動開啟 ${url}（host 仍在服務，不需要重新啟動）" >&2
fi
echo "present: 就緒 ${url}；在頁面輸入終端機上的配對碼。停止展示：Ctrl-C（本次資料保留於 ${w}）" >&2
# Wait for the host; a trap interrupts wait, so wait until it has ended.
rc=0
while kill -0 "$pid" 2>/dev/null; do
	wait "$pid"
	rc=$?
done
exit "$rc"
