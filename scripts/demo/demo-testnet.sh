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
# alive: a process of this run is still there (ps: a sudo process cannot be signalled by this user, not even kill -0).
alive() { ps -p "$1" >/dev/null 2>&1; }
voice_ok() { (cd / && sudo -n -u apa-signer /usr/bin/curl -s --max-time 2 --unix-socket "$vsock" http://voice/v1/health) 2>/dev/null | grep -q '"ok": *true'; }
[ -t 0 ] || stuck "啟動前：要在終端機執行（密語從終端機輸入）"

case $mode in
fake) ;;
live) echo "$label" | grep -Eq '^[a-z0-9]{1,12}$' || stuck "參數：live 要一個標籤（小寫英數，例如 t3），task 會是 show-e1a-標籤-…" ;;
*) stuck "參數：fake 或 live 標籤" ;;
esac

# Before anything starts.
if lsof -nP -iTCP:8402 -sTCP:LISTEN >/dev/null 2>&1; then stuck "啟動前：127.0.0.1:8402 已有程式在用（上一個 Host？）"; fi
[ -f "$tn/relayer.json" ] || stuck "啟動前：沒有 $tn/relayer.json（P2-b 的部署）"
if [ -S "$ssock" ]; then
  if answers "$ssock"; then stuck "啟動前：/opt 的 Signer 已在跑（$ssock）"; fi
  stuck "啟動前：$ssock 是上一次留下的；請先執行：sudo -u apa-signer /bin/rm $ssock"
fi
if [ -S "$rsock" ]; then
  if answers "$rsock"; then stuck "啟動前：relayer 已在跑（$rsock）"; fi
  rm -f "$rsock" # a leftover nothing answers on: apa-relayer refuses an existing socket
fi
mkdir -p "$logs"
chmod 700 "$root/var/demo-testnet" "$logs"

say "需要 sudo 的密碼（這是你的登入密碼，不是錢包或 relayer 的密語；語音服務與 Signer 以 apa-signer 身分執行）"
sudo -v || stuck "第 0 步：sudo 沒有通過"
if voice_ok; then stuck "啟動前：/opt 的語音服務已在跑（上一次沒停？）"; fi

tty=$(stty -g 2>/dev/null || true)
vpid=; spid=
stopping=no
stop() {
  set +e # a closed window's terminal cannot be written to: the rest still runs
  [ $stopping = no ] || return 0
  stopping=yes
  trap '' INT TERM HUP
  say "停下中…"
  # The services under /opt are stopped as root, by what they are (review:
  # this user cannot signal sudo, and sudo may not pass a signal from its
  # own group on); the preflight saw neither running before this run.
  if [ -n "$vpid$spid" ]; then
    if sudo -n true 2>/dev/null; then
      sudo -n /usr/bin/pkill -TERM -u apa-signer -f "$opt/voice/sidecar.py" 2>/dev/null || true
      sudo -n /usr/bin/pkill -TERM -u apa-signer -f "$opt/bin/apa-signer serve" 2>/dev/null || true
    else
      say "sudo 已過期，/opt 的服務可能還在跑；請執行：sudo /usr/bin/pkill -TERM -u apa-signer -f $opt/voice/sidecar.py; sudo /usr/bin/pkill -TERM -u apa-signer -f '$opt/bin/apa-signer serve'"
    fi
  fi
  kill -TERM 0 2>/dev/null || true
  wait 2>/dev/null || true
  [ -z "$tty" ] || stty "$tty" 2>/dev/null || true # a passphrase prompt stopped mid-way leaves echo off
  [ ! -S "$rsock" ] || answers "$rsock" || rm -f "$rsock"
  if [ -S "$ssock" ] && ! answers "$ssock"; then say "（$ssock 留下了：下次啟動前請執行 sudo -u apa-signer /bin/rm $ssock）"; fi
  say "已停下"
}
trap stop EXIT
trap 'exit 130' INT TERM HUP
# sudo's time stamp kept the whole run, so stop() can stop the /opt services as root.
(while sleep 60; do sudo -n -v 2>/dev/null || exit 0; done) &

# 1 the voice service: its models load in 2-3 minutes.
say "1/4 語音服務啟動中（模型載入約 2～3 分鐘；紀錄 $logs/voice.log）"
(cd / && exec sudo -u apa-signer /usr/bin/env -i HOME=/var/empty PATH=/usr/bin:/bin "$opt/voice/python/bin/python3.12" -I "$opt/voice/sidecar.py" \
  --socket "$vsock" --models "$opt/voice/models") >"$logs/voice.log" 2>&1 &
vpid=$!
i=0
until voice_ok; do
  alive $vpid || { tail -5 "$logs/voice.log" >&2; stuck "第 1 步：語音服務沒有起來（上面是它最後的訊息）"; }
  i=$((i + 1)); [ $i -lt 120 ] || stuck "第 1 步：語音服務 6 分鐘內沒有就緒（$logs/voice.log）"
  sudo -n -v 2>/dev/null || true # keep sudo's time stamp while the models load
  sleep 3
done
say "1/4 語音服務就緒"

# 2 apa-relayer: its passphrase on this terminal.
say "2/4 relayer：請輸入 relayer 的密語"
sh scripts/demo/p2-testnet.sh relayer &
rpid=$!
i=0
until [ -S "$rsock" ]; do
  alive $rpid || stuck "第 2 步：relayer 停了（密語不對、或上面有錯誤訊息）"
  i=$((i + 1)); [ $i -lt 900 ] || stuck "第 2 步：relayer 15 分鐘內沒有就緒"
  sudo -n -v 2>/dev/null || true
  sleep 1
done
say "2/4 relayer 就緒"
# C4 (10/24: a show ran into the relayer's daily cap unseen): enough for a
# show of two customers, before the Signer and the Host start (read-only).
pre=0
python3 -I scripts/demo/testnet-preflight.py "$rsock" "$tn/host" >&2 || pre=$?
case $pre in
0) ;;
1) stuck "開場前檢查：上面「不夠」的項目（交易數在 UTC 午夜、台北 08:00 歸零）" ;;
*) say "開場前檢查沒有完成（上面是原因）；繼續啟動，請看主持人頁就緒表的 relayer 今天、商店今天" ;;
esac

# 3 the Signer under /opt: sudo first, here and on its own, so the wallet
# passphrase is never typed into a sudo prompt (review).
say "3/4 Signer：sudo 若再問密碼，是你的登入密碼"
sudo -v || stuck "第 3 步：sudo 沒有通過"
say "3/4 Signer：請輸入錢包的密語"
(cd / && exec sudo -n -u apa-signer "$opt/bin/apa-signer" serve --dir "$opt/data") &
spid=$!
i=0
until [ -S "$ssock" ]; do
  alive $spid || stuck "第 3 步：Signer 停了（密語不對、或上面有錯誤訊息）"
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
