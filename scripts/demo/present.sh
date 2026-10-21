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
# APA_APPROVALS_SIGNER (fake only; make present-a2): the Unix socket of a
# running apa-signer serve-approvals; the person's approval then runs on
# http://localhost:<port>/host, which is the page opened (WebAuthn needs
# localhost, not 127.0.0.1; docs/commercial-a2.md). Its database and traces
# are then kept across starts, one place per Signer socket,
# var/a2-host-<12 hex of the socket's path>: the Signer keeps every
# session it decided and a session's ID is derived from its task, so a new
# database would start again at show-a-01 and be refused SESSION_DECIDED
# (V1 device round). Its 20 show-* tasks per consumer then last for that
# Signer's lifetime; removing that directory brings the refusals back.
# APA_AUTHENTICATOR_SOURCE (required with it): device or virtual, what the
# page declares the authenticator to be (review N17); E1-a labels the
# approvals live (device) or fixture (virtual) by it.
# APA_PRESENT_REGISTRY (fake only; default testdata/demo/registry.json): another
# demo registry, e.g. V1 development mode's var/v1-dev/registry.json (the
# same plus more show-* tasks: a kept A2 database uses them up).
# APA_PRESENT_PAYMENTS (fake only, with APA_APPROVALS_SIGNER; make present-p2):
# the local anvil payment configuration (cmd/p2-local's payments.json); the
# host then pays through that Signer, which must be apa-signer serve with a
# reservation section (docs/commercial-p2-host.md 12.8).
# APA_PRESENT_CREDENTIALS=1 (fake only; make present-c2): build apa-issuer,
# make a credential directory of test keys in the session directory (holders
# for consumer-a and consumer-b) and start the host with --credential-dir.
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
signer=${APA_APPROVALS_SIGNER:-}
[ -z "$signer" ] || [ "$mode" = fake ] || stop "APA_APPROVALS_SIGNER 只用於 fake 展示（A2 使用 fake Agent）"
authsrc=${APA_AUTHENTICATOR_SOURCE:-}
[ -z "$signer" ] || case $authsrc in device|virtual) ;; *) stop "AUTH=${authsrc}：有 Signer 時要寫明 AUTH=device（實體裝置）或 AUTH=virtual；它決定核准事件標成 live 或 fixture（docs/commercial-e1a.md E1a-D4）" ;; esac
creds=${APA_PRESENT_CREDENTIALS:-}
registry=${APA_PRESENT_REGISTRY:-testdata/demo/registry.json}
[ "$registry" = testdata/demo/registry.json ] || [ "$mode" = fake ] || stop "APA_PRESENT_REGISTRY 只用於 fake 展示"
[ -f "$registry" ] || stop "APA_PRESENT_REGISTRY=${registry} 不是檔案"
[ -z "$creds" ] || [ "$mode" = fake ] || stop "APA_PRESENT_CREDENTIALS 只用於 fake 展示（C2 的 v2 場景只用 fake Agent）"
payments=${APA_PRESENT_PAYMENTS:-}
# APA_PRESENT_PROFILE (fake only; make present-testnet): the consumer profile
# (its merchants are the testnet shop's address); default the demo's own.
profile=${APA_PRESENT_PROFILE:-}
[ -z "$profile" ] || [ -f "$profile" ] || stop "APA_PRESENT_PROFILE=${profile} 不是檔案"
[ -z "$payments" ] || [ -n "$signer" ] || stop "APA_PRESENT_PAYMENTS 需要 APA_APPROVALS_SIGNER（同一個 Signer 也簽付款）"
[ -z "$payments" ] || [ -f "$payments" ] || stop "APA_PRESENT_PAYMENTS=${payments} 不是檔案"
[ -z "$payments" ] || [ -S "$signer" ] || stop "APA_APPROVALS_SIGNER=${signer} 不是 socket：先啟動 apa-signer serve（本機 anvil：scripts/demo/p2-dev.sh；testnet：/opt 的 serve，docs/commercial-p2b.md 6.1），或檢查它是否還在執行"
[ -z "$signer" ] || [ -S "$signer" ] || stop "APA_APPROVALS_SIGNER=${signer} 不是 socket：請先啟動 apa-signer serve-approvals（docs/commercial-a2.md）"

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
db=$w/db
runs=$w/runs
if [ -n "$signer" ]; then
	sabs=$(cd "$(dirname "$signer")" && pwd)/$(basename "$signer") || stop "cannot resolve ${signer}"
	keep=$base/a2-host-$(printf %s "$sabs" | shasum -a 256 | cut -c1-12)
	{ mkdir -p "$keep/db" "$keep/runs" && chmod 700 "$keep" "$keep/db" "$keep/runs"; } || stop "cannot make ${keep}"
	db=$keep/db
	runs=$keep/runs
	echo "present: A2 的 Host 資料庫沿用 ${keep}（Signer ${sabs}；task 不重複使用）" >&2
fi
"$GO" build -o "$w/demo" ./cmd/demo || stop "建置失敗；沒有啟動任何程式（也不用舊的）"
if [ "$mode" = fake ]; then
	"$GO" build -o "$w/fake-claude" ./cmd/fake-claude || stop "fake CLI 建置失敗；沒有啟動任何程式"
fi
if [ -n "$creds" ]; then
	"$GO" build -o "$w/apa-issuer" ./cmd/apa-issuer || stop "apa-issuer 建置失敗；沒有啟動任何程式"
	"$w/apa-issuer" init --dir "$w/cred" --issuer issuer-demo --consumers consumer-a,consumer-b >&2 || stop "憑證目錄建立失敗；沒有啟動任何程式"
	echo "present: 撤銷某個 task 的憑證：${w}/apa-issuer revoke --dir ${w}/cred --task <task_id>（測試金鑰；host 每 300 秒自動更新清單）" >&2
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
	set --
	[ -z "$signer" ] || set -- --approvals-signer "$signer" --authenticator-source "$authsrc"
	[ -z "$creds" ] || set -- "$@" --credential-dir "$w/cred"
	[ -z "$payments" ] || set -- "$@" --payments "$payments"
	[ -z "$profile" ] || set -- "$@" --profile "$profile"
	APA_ADDR=$addr APA_DB_PATH=$db/fake.db APA_DEMO_TOKEN_FILE=$w/tok/tokens.json "$w/demo" \
		--fake-cli "$w/fake-claude" --scenarios "${APA_PRESENT_SCENARIOS:-testdata/demo/scenarios.json}" --registry "$registry" \
		--trace-dir "$runs" --unity-build "$proj/Build/WebGL/Build" --pairing --ready-file "$w/ready.json" "$@" &
fi
pid=$!
trap 'kill -INT "$pid" 2>/dev/null' INT TERM

# Ready means this process says so: its ready file, with its own pid.
# With payments the host may first ask the shop key's passphrase on this
# terminal (docs/commercial-p2b.md B3): five minutes then.
limit=1200
[ -z "$payments" ] || limit=3000
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
	if [ "$i" -ge "$limit" ]; then
		kill -INT "$pid" 2>/dev/null
		wait "$pid"
		stop "$((limit / 10)) 秒內沒有就緒；已停止本次啟動的 host"
	fi
	sleep 0.1
done
url="http://$(sed -n 's/.*"addr":"\([^"]*\)".*/\1/p' "$w/ready.json")/host"
[ -z "$signer" ] || url="http://localhost:${url##*:}" # the same port, by the trusted name (A2)
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
