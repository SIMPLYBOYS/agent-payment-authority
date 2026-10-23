#!/bin/sh
# The Base Sepolia demo in one terminal (docs/commercial-ops.md O3; the
# steps of docs/commercial-e1a.md 6.2): the voice service and the Signer
# under /opt (as apa-signer, by sudo), apa-relayer, then the Host, which
# opens the page. You type sudo's password once, then the relayer's and the
# Signer's passphrases, each when asked; nothing here stores or passes them.
# Each step waits for the one before and says where it is stuck. Ctrl-C
# stops everything this run started.
#
#   sh scripts/demo/demo-testnet.sh            the fake Agent (make present-testnet)
#   sh scripts/demo/demo-testnet.sh live L     the real Agent, E1-a's host files for label L
#                                              (made once from var/p2-testnet; task IDs show-e1a-L-…)
#
# The real Agent runs Claude Code CLI only when the host presses 開始.
set -eu
root=$(cd "$(dirname "$0")/../.." && pwd)
cd "$root"
mode=${1:-fake}
label=${2:-}
opt=/opt/apa-signer
ssock=$opt/run/signer.sock
vsock=$opt/voice-run/voice.sock
tn=$root/var/p2-testnet
rsock=$tn/run/relayer.sock
logs=$root/var/demo-testnet/logs
say() { printf 'demo-testnet: %s\n' "$*" >&2; }
stuck() { printf 'demo-testnet: 卡在%s\n' "$*" >&2; exit 1; }
answers() { python3 -I -c 'import socket,sys; s=socket.socket(socket.AF_UNIX); s.settimeout(1); s.connect(sys.argv[1])' "$1" 2>/dev/null; }

case $mode in
fake) ;;
live) echo "$label" | grep -Eq '^[a-z0-9]{1,12}$' || stuck "參數：live 要一個標籤（小寫英數，例如 t3），task 會是 show-e1a-標籤-…" ;;
*) stuck "參數：fake 或 live 標籤" ;;
esac

# Before anything starts.
if lsof -nP -iTCP:8402 -sTCP:LISTEN >/dev/null 2>&1; then stuck "啟動前：127.0.0.1:8402 已有程式在用（上一個 Host？）"; fi
[ -f "$tn/relayer.json" ] || stuck "啟動前：沒有 $tn/relayer.json（P2-b 的部署）"
for s in "$ssock" "$vsock"; do
  if [ -S "$s" ] && answers "$s"; then stuck "啟動前：$s 已有服務在跑（上一次沒停？）"; fi
done
if [ -S "$rsock" ]; then
  if answers "$rsock"; then stuck "啟動前：relayer 已在跑（$rsock）"; fi
  rm -f "$rsock" # a leftover nothing answers on: apa-relayer refuses an existing socket
fi
mkdir -p "$logs"
chmod 700 "$root/var/demo-testnet" "$logs"

say "需要 sudo 的密碼一次（語音服務與 Signer 以 apa-signer 身分執行）"
sudo -v || stuck "第 0 步：sudo 沒有通過"

stopping=no
stop() {
  [ $stopping = no ] || return 0
  stopping=yes
  trap '' INT TERM
  say "停下中…"
  kill -TERM 0 2>/dev/null || true
  wait 2>/dev/null || true
  [ ! -S "$rsock" ] || answers "$rsock" || rm -f "$rsock"
  say "已停下"
}
trap stop EXIT
trap 'exit 130' INT TERM

# 1 the voice service: its models load in 2-3 minutes.
say "1/4 語音服務啟動中（模型載入約 2～3 分鐘；紀錄 $logs/voice.log）"
(cd / && exec sudo -u apa-signer /usr/bin/env -i HOME=/var/empty PATH=/usr/bin:/bin "$opt/voice/python/bin/python3.12" -I "$opt/voice/sidecar.py" \
  --socket "$vsock" --models "$opt/voice/models") >"$logs/voice.log" 2>&1 &
vpid=$!
i=0
until (cd / && sudo -n -u apa-signer /usr/bin/curl -s --max-time 2 --unix-socket "$vsock" http://voice/v1/health) 2>/dev/null | grep -q '"ok": *true'; do
  kill -0 $vpid 2>/dev/null || { tail -5 "$logs/voice.log" >&2; stuck "第 1 步：語音服務沒有起來（上面是它最後的訊息）"; }
  i=$((i + 1)); [ $i -lt 360 ] || stuck "第 1 步：語音服務 6 分鐘內沒有就緒（$logs/voice.log）"
  sudo -n -v 2>/dev/null || true # keep sudo's time stamp while the models load
  sleep 1
done
say "1/4 語音服務就緒"

# 2 apa-relayer: its passphrase on this terminal.
say "2/4 relayer：請輸入 relayer 的密語"
sh scripts/demo/p2-testnet.sh relayer &
rpid=$!
i=0
until [ -S "$rsock" ]; do
  kill -0 $rpid 2>/dev/null || stuck "第 2 步：relayer 停了（密語不對、或上面有錯誤訊息）"
  i=$((i + 1)); [ $i -lt 900 ] || stuck "第 2 步：relayer 15 分鐘內沒有就緒"
  sleep 1
done
say "2/4 relayer 就緒"

# 3 the Signer under /opt: its passphrase on this terminal.
say "3/4 Signer：請輸入錢包的密語"
(cd / && exec sudo -u apa-signer "$opt/bin/apa-signer" serve --dir "$opt/data") &
spid=$!
i=0
until [ -S "$ssock" ]; do
  kill -0 $spid 2>/dev/null || stuck "第 3 步：Signer 停了（密語不對、或上面有錯誤訊息）"
  i=$((i + 1)); [ $i -lt 900 ] || stuck "第 3 步：Signer 15 分鐘內沒有就緒"
  sleep 1
done
say "3/4 Signer 就緒"

# 4 the Host (the page opens; the pairing code below).
if [ "$mode" = live ]; then
  host=$root/var/e1a-testnet/host-$label
  [ -d "$host" ] || go run ./cmd/p2-testnet e1a-host --dir "$tn" --out "$host" --label "$label" --shop-eval data || stuck "第 4 步：建不出 E1-a 的 host 檔（$host）"
  say "4/4 Host 啟動中（真實 Agent：按「開始」才執行 Claude Code CLI；task 是 show-e1a-$label-…）"
  make present-e1a SIGNER="$ssock" HOST="$host" AUTH=device || stuck "第 4 步：Host 停了"
else
  say "4/4 Host 啟動中（fake Agent）"
  make present-testnet SIGNER="$ssock" AUTH=device || stuck "第 4 步：Host 停了"
fi
