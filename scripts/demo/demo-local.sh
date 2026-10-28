#!/bin/sh
# The local demo in one terminal (Aaron 10/23: the chain and the Signer may
# be off between takes, but must start again any time and work): the kept
# local chain (p2-local.sh, resumed when it was kept), the development
# Signer (p2-dev.sh; it asks its passphrase on this terminal, as always),
# then the Host with the fake Agent (make present-p2), which opens the page;
# with live L, the town on the real Agent instead (docs/commercial-live-town.md
# L3: make present-e1a with the town's catalog, host files made once for L
# from the chain's directory, task IDs show-e1a-L-…; var/present.conf names the CLI).
# Each step waits for the one before and says where it is stuck. Ctrl-C
# stops all three; the chain is kept for the next time.
#
#   sh scripts/demo/demo-local.sh        (or: make demo-local)
#   sh scripts/demo/demo-local.sh live L the town on the real Agent (L: a label, a new one when the tasks are used up)
#
# DEMO_LOCAL_DIR (default var/demo-local) holds the chain, the Host's
# session and the logs, and is kept between runs. P2_DEV_DIR (default
# var/p2-dev-e1a) is the Signer's directory with its two test wallets;
# DEMO_WALLET_A/B are their addresses. No key file is read here.
set -eu
root=$(cd "$(dirname "$0")/../.." && pwd)
cd "$root"
base=${DEMO_LOCAL_DIR:-$root/var/demo-local}
dev=${P2_DEV_DIR:-$root/var/p2-dev-e1a}
A=${DEMO_WALLET_A:-0x833405a796c3300afcc582a3e251adf1a4d3c477}
B=${DEMO_WALLET_B:-0x42c318751644a1be96f102dc46c828400fe58bd6}
chain=$base/chain
logs=$base/logs
sock=$dev/run/signer.sock
mode=${1:-fake}
label=${2:-}
say() { printf 'demo-local: %s\n' "$*" >&2; }
stuck() { printf 'demo-local: 卡在%s\n' "$*" >&2; exit 1; }
answers() { python3 -I -c 'import socket,sys; s=socket.socket(socket.AF_UNIX); s.settimeout(1); s.connect(sys.argv[1])' "$1" 2>/dev/null; }
[ -t 0 ] || stuck "啟動前：要在終端機執行（Signer 的密語從終端機輸入）"
case $mode in
fake) ;;
live) echo "$label" | grep -Eq '^[a-z0-9]{1,12}$' || stuck "參數：live 要一個標籤（小寫英數，例如 r1），task 會是 show-e1a-標籤-…" ;;
*) stuck "參數：不寫（fake Agent）或 live 標籤（城鎮・真實 Agent）" ;;
esac

# Before anything starts: the ports free and no Signer left over.
for p in 8545 8402; do
  if lsof -nP -iTCP:$p -sTCP:LISTEN >/dev/null 2>&1; then stuck "啟動前：127.0.0.1:$p 已有程式在用（上一次沒停乾淨？lsof -nP -iTCP:$p 看是誰）"; fi
done
if [ -e "$sock" ]; then
  if answers "$sock"; then stuck "啟動前：Signer 已在跑（$sock）"; fi
  rm -f "$sock" # a leftover nothing answers on (a crash): the Signer refuses an existing socket
fi
[ -f "$dev/data/keys/wallet-a.key" ] && [ -f "$dev/data/keys/wallet-b.key" ] || stuck "啟動前：$dev 沒有兩個錢包的金鑰檔（P2_DEV_DIR）"
mkdir -p "$base" "$logs"
chmod 700 "$base" "$logs"

# Ctrl-C or any stop: every process of this run, the chain saved on its way out.
tty=$(stty -g 2>/dev/null || true)
stopping=no
stop() {
  set +e # a closed window's terminal cannot be written to: the rest still runs
  [ $stopping = no ] || return 0
  stopping=yes
  trap '' INT TERM HUP
  say "停下中…"
  kill -TERM 0 2>/dev/null || true
  wait 2>/dev/null || true
  [ -z "$tty" ] || stty "$tty" 2>/dev/null || true # a passphrase prompt stopped mid-way leaves echo off
  # A socket nothing answers on is a leftover of this run: gone, so the next run starts.
  if [ -S "$sock" ] && ! answers "$sock"; then rm -f "$sock"; fi
  say "已停下；鏈保存在 $chain/anvil-state.json，下次同一個指令會接著用"
}
trap stop EXIT
trap 'exit 130' INT TERM HUP

# 1 the chain (kept, or set up the first time).
say "1/3 本機鏈啟動中（紀錄 $logs/chain.log）"
P2_LOCAL_DIR=$chain sh scripts/demo/p2-local.sh "$A" "$B" >"$logs/chain.log" 2>&1 &
cpid=$!
i=0
until grep -q "anvil is running" "$logs/chain.log" 2>/dev/null; do
  kill -0 $cpid 2>/dev/null || { tail -5 "$logs/chain.log" >&2; stuck "第 1 步：本機鏈沒有起來（上面是它最後的訊息）"; }
  i=$((i + 1)); [ $i -lt 300 ] || stuck "第 1 步：本機鏈 5 分鐘內沒有就緒（$logs/chain.log）"
  sleep 1
done
say "1/3 本機鏈就緒：$(grep -E '^p2-local: (resumed|wrote)' "$logs/chain.log" | tail -1 | sed 's/^p2-local: //')"

# 3 the Host, once the Signer's socket is there (it starts after the passphrase).
(
  i=0
  until [ -S "$sock" ]; do
    i=$((i + 1)); [ $i -lt 900 ] || { say "卡在第 3 步：15 分鐘內沒有 Signer，Host 沒有啟動；全部停下"; kill -TERM 0; }
    sleep 1
  done
  if [ "$mode" = live ]; then
    # The town on the real Agent: host files made once for the label from the chain's directory (the shop's own products).
    host=$base/live-$label
    # Made once for the label, and the evidence's start recorded before any run (docs/commercial-live-town.md L4);
    # each on its own, so a start that failed is tried again.
    [ -d "$host" ] || go run ./cmd/p2-testnet e1a-host --from "$chain" --out "$host" --label "$label" ||
      { say "卡在第 3 步：建不出城鎮的 host 檔（$host）；全部停下"; kill -TERM 0; }
    [ -f "$host/start.json" ] || go run ./cmd/e1-evidence start --rpc http://127.0.0.1:8545 --host "$host" --out "$host/start.json" ||
      { say "卡在第 3 步：記不下證據的起點（$host/start.json；task 已登記過就換一個標籤）；全部停下"; kill -TERM 0; }
    say "3/3 一個標籤就是一次彩排：做完先出證據包，之後再從這兩個錢包付款（包括 make demo-local 的 fake 城鎮）就換一個標籤"
    say "3/3 Host 啟動中（城鎮・真實 Agent：按「開始」才執行 Claude Code CLI，一個場域兩位客人；頁面會自動打開；配對碼在下面）"
    make present-e1a SIGNER="$sock" HOST="$host" AUTH=device SCENARIOS=testdata/demo/scenarios-town-live.json ||
      { say "卡在第 3 步：Host 停了（上面是它的訊息）；全部停下"; kill -TERM 0; }
  else
    say "3/3 Host 啟動中（fake Agent；頁面會自動打開；配對碼在下面）"
    APA_PRESENT_DIR=$base/host make present-p2 SIGNER="$sock" AUTH=device PAYMENTS="$chain/payments.json" REGISTRY="$chain/registry.json" ||
      { say "卡在第 3 步：Host 停了（上面是它的訊息）；全部停下"; kill -TERM 0; }
  fi
) &

# 2 the Signer, here in the foreground: its passphrase is typed on this terminal.
say "2/3 Signer：請輸入錢包的密語（Ctrl-C 會把三個都停下）"
P2_DEV_DIR=$dev P2_LOCAL_DIR=$chain sh scripts/demo/p2-dev.sh "$A" "$B" || stuck "第 2 步：Signer 停了（密語不對、或上面有錯誤訊息）"
