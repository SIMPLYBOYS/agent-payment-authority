# P1-b OS 隔離驗收：指令筆記（給 Codex 預演、審閱與陪同判讀）

> 2026-10-13，Claude Code。第二版：依 vault〈Claude-Code-P1-bOS隔離驗收指令補正派工-0ef6172-20261013〉補正 O1～O3；第一版（`0ef6172`）不適合直接照貼，保留在 Git 歷史，修訂對照見第 9 節。
> 依據：[commercial-p1b-os-isolation.md](commercial-p1b-os-isolation.md)（方案，Aaron 已核准 D1～D7，第 8 節）；程式 `cmd/apa-signer`（`serve`、`keygen`）。
> **狀態：命令都還沒有執行過。**本版只做了 `zsh -f -n` 語法解析（第 10 節），沒有執行任何區塊，也沒有建置、啟動 Signer、keygen、sudo 或改系統。語法通過不等於參數合法或操作已驗證。跨使用者、sudo、`/dev/tty` 經 sudo、群組生效、socket 群組繼承，只能在第一次執行時得知。
> 歸屬：`serve`、`keygen` 的程式行為由 Claude Code 測試（同一 uid；`keygen` 見方案第 8.2 節），keygen 尚未經 Codex 技術複核。Codex 前一輪只做文件、原始碼、本機手冊與語法預檢，沒有建置或啟動 Signer，沒有 keygen、sudo 或跨使用者測試，也沒有 repo 證據 log。

## 0 角色與通則

| 誰 | 做什麼 |
|---|---|
| Codex | 第 1 節的受控預演；之後在每個關卡讀輸出、判讀、建議。不輸入、不接收、不保存任何密碼或密語 |
| Aaron | 第 2～8 節的操作；所有 sudo 密碼與密語只由 Aaron 在鍵盤上輸入 |

- **一律用乾淨的 zsh**：每個視窗先執行 `exec /bin/zsh -f`，不載入個人啟動設定。本筆記所有區塊都是 zsh 語法。
- **三個視窗**：
  - W-host：Host 使用者，不用 sudo，錄製。
  - W-admin：只做 sudo，錄製。
  - W-signer：前景 Signer，**不錄製**。
  - Host、demo、Claude CLI 不在 W-admin 或 W-signer 執行。
- **錄製**：W-host、W-admin 以 `script -q <log> /usr/bin/env -i … /bin/zsh -f` 進入一個**只有白名單環境變數**的子 shell。不加 `-r`／`-k`，所以只錄輸出，不錄輸入；sudo 密碼不回顯，所以不會進入 log。白名單不含個人憑證類變數。
- **三個階段**：
  - 錄製前：外層 `zsh -f`，用來建立本次目錄與交接檔。
  - 錄製中：子 shell，只從交接檔取值。外層的變數不會自動傳入，因為子 shell 用 `env -i` 啟動。
  - 錄製後：`exit` 回到外層。
- **本次目錄 `RUN`**：`mktemp -d /tmp/apa-iso.XXXXXX` 建立，Host 使用者擁有，`0700`。所有交接值寫在 `RUN/vars`（不含秘密）；其他視窗只輸入 `RUN` 這一個路徑，經檢查後才使用。
- **關卡**：
  - 會改東西或要判讀的區塊都寫成 `( setopt ERR_EXIT NO_UNSET PIPE_FAIL; need <前一步>; …; mark <本步> )`。
  - 任一命令失敗，子 shell 立刻結束，不會寫入本步的 `ok` 標記，下一步的 `need` 就擋住。
  - 預期被拒的探測用 `denied`：它保存退出碼，**只有**錯誤是「Permission denied／Operation not permitted」才算拒絕成功，否則印 `UNEXPECTED` 並停止。
  - 檔案不存在、命令找不到、路徑錯誤都不算拒絕成功。
- **不輸出內容**：讀取探測用 `dd … of=/dev/null bs=1 count=1`，真的開檔讀一個 byte，但內容不送到終端機。不用 `cat`，也不用 `test -r` 代替實際存取。不印任何程序或 shell 的完整環境。
- **停止時**：看到 `STOP` 或 `UNEXPECTED`，不修權限、不重試、不改名重跑。把該段輸出交 Codex 判讀，由 Aaron 決定。
- **不做**：不呼叫 `/v1/sign`、不連 RPC 或外部服務、不改全機防火牆或 sudo 政策、不用 A／B 展示金鑰、不接進展示流程。

## 1 受控預演（Codex，不用 sudo、不改系統）

目的：在動系統前，用 Codex 自己的 uid 實際跑一次 `serve`／`keygen` 的形狀，對照輸出與退出碼，修正本筆記。預演用的密語與金鑰是丟棄式材料（非秘密），只存在本次暫存目錄，結束後刪除，之後不再使用。

**執行邊界**（security-audit）：
- 網路：OS 強制斷網，以 `sandbox-exec` 設定檔拒絕 IP 進出，只留 Unix socket。
- 環境：`env -i` 白名單環境；暫存 HOME。
- 可寫區域：設定檔只允許寫本次目錄與必要的 `/dev` 節點。
- 資源：CPU 時間、檔案大小、開檔數的 `ulimit`。

**無法提供、請 Codex 確認或補安排的**：
- 記憶體上限：macOS 不強制 `RLIMIT_AS`。
- 程序數上限：`ulimit -u` 算的是整個 uid 的程序數，設低會擋到其他程式，所以不設。
- 磁碟配額。
- `sandbox-exec` 已被 Apple 標為不建議使用，設定檔的實際效果要由預演第一步自我檢查。

不足時不改成無隔離預演，回報缺口。Linux 容器結果不能當成 macOS 的 TTY 或跨使用者驗證。

**1.1 啟動（Codex 的終端機）**
```zsh
R=$(mktemp -d /tmp/apa-r.XXXXXX) && chmod 700 "$R" && RP=$(cd "$R" && pwd -P) && print -r -- "R=$R RP=$RP"
# 把 1.2 的內容存成 "$R/rehearse.zsh"，再執行：
PROFILE="(version 1)(allow default)(deny network-outbound (remote ip))(deny network-inbound (local ip))(deny file-write* (require-not (require-any (subpath \"$RP\") (literal \"/dev/null\") (literal \"/dev/tty\") (literal \"/dev/ptmx\") (regex #\"^/dev/ttys[0-9]+\$\") (literal \"/dev/dtracehelper\"))))"
ulimit -t 900; ulimit -f 2097152; ulimit -n 1024
/usr/bin/sandbox-exec -p "$PROFILE" /usr/bin/env -i HOME="$RP/home" TMPDIR="$RP/tmp" PATH=/usr/bin:/bin:/usr/sbin:/sbin TERM=xterm-256color \
  R="$RP" REPO=/Users/mac/Documents/OpenSourceProjects/agent-payment-authority GO=/usr/local/bin/go GOMODCACHE="$(go env GOMODCACHE)" \
  GOCACHE="$RP/gocache" GOPROXY=off GOSUMDB=off GOFLAGS=-mod=readonly GOTOOLCHAIN=local GIT_OPTIONAL_LOCKS=0 \
  /bin/zsh -f -eu -o pipefail "$R/rehearse.zsh" > "$R/rehearse.log" 2>&1; print -r -- "rehearse exit=$?"
```

**1.2 `rehearse.zsh`**
```zsh
# Same-uid rehearsal of the Signer commands. No sudo, no system change.
mkdir -m 700 "$R/home" "$R/tmp" "$R/data" "$R/data/keys"
step() { print -r -- "== $*" }

step R0 isolation self-check
if /usr/bin/curl -sS --connect-timeout 3 --max-time 5 -o /dev/null https://example.com 2>/dev/null; then print -r -- "STOP: the network is reachable"; exit 1; fi
if : 2>/dev/null >| "$REPO/.rehearsal-write-probe"; then /bin/rm -f -- "$REPO/.rehearsal-write-probe"; print -r -- "STOP: the repo is writable"; exit 1; fi
print -r -- "network: refused; repo: not writable"

step R1 build
cd "$REPO"
[[ "$(git status --porcelain)" == '?? demo' ]] || { print -r -- "STOP: the work tree is not clean"; exit 1 }
print -r -- "HEAD=$(git rev-parse HEAD)"
"$GO" build -o "$R/apa-signer" ./cmd/apa-signer
cd "$R"

step R2 ps, pgrep and curl flags
ps -o pid,uid,gid,user,command -p $$
print -r -- "user=[$(ps -o user= -p $$)] command=[$(ps -o command= -p $$)]"
/bin/sleep 30 & P=$!
pgrep -u "$(id -un)" -f '^/bin/sleep 30$' | grep -qx "$P"
kill "$P"; wait "$P" 2>/dev/null || true
/usr/bin/curl --version | head -1

SOCK="$R/sd/s"
mkdir -m 700 "$R/sd"
(( ${#SOCK} < 100 )) || { print -r -- "STOP: socket path too long"; exit 1 }
config() { # callers-uid [wallet-address]
  local wallets='[]'
  [[ -n ${2:-} ]] && wallets="[{\"wallet_id\":\"wallet-rehearsal\",\"consumer_id\":\"consumer-rehearsal\",\"chain_id\":\"84532\",\"address\":\"$2\"}]"
  print -r -- "{\"version\":1,\"socket\":\"$SOCK\",\"callers\":[$1],\"max_window_seconds\":600,\"wallets\":$wallets,\"assets\":[],\"delegations\":[]}" >| "$R/data/signer.json"
  chmod 600 "$R/data/signer.json"
}
start_serve() { # passphrase line (non-secret, rehearsal only)
  mkfifo "$R/in"
  /usr/bin/script -q /dev/null "$R/apa-signer" serve --dir "$R/data" < "$R/in" >| "$R/serve.out" 2>&1 &
  SPID=$!
  exec 3> "$R/in"
  print -u3 -r -- "$1"
  APID=''
  for i in {1..50}; do APID=$(pgrep -P "$SPID" -x apa-signer || true); [[ -n $APID ]] && break; sleep 0.2; done
  [[ -n $APID ]] || { print -r -- "STOP: no Signer process under script"; exit 1 }
  [[ "$(ps -o command= -p "$APID" | sed 's/ *$//')" == "$R/apa-signer serve --dir $R/data" ]] || { print -r -- "STOP: unexpected command line"; exit 1 }
  for i in {1..50}; do [[ -S $SOCK ]] && break; sleep 0.2; done
  [[ -S $SOCK ]] || { print -r -- "STOP: no socket within 10 s"; cat "$R/serve.out"; exit 1 }
  print -r -- "serving: pid $APID"
}
stop_serve() {
  kill -TERM "$APID"
  for i in {1..25}; do ps -p "$APID" >/dev/null || break; sleep 0.2; done
  ! ps -p "$APID" >/dev/null || { print -r -- "STOP: the Signer did not stop"; exit 1 }
  exec 3>&-
  wait "$SPID" || true
  /bin/rm -- "$R/in"
  [[ ! -e $SOCK ]] || { print -r -- "STOP: the socket was left"; exit 1 }
  print -r -- "stopped"
}
health() {
  HOUT=$(/usr/bin/curl -sS --connect-timeout 3 --max-time 5 --unix-socket "$SOCK" -w '\nHTTP=%{http_code}' http://signer/v1/health 2>&1) && HRC=0 || HRC=$?
  print -r -- "$HOUT"; print -r -- "curl_rc=$HRC"
}

step R4 serve: health 200, 403, and a refused connect
config "$(id -u)"
start_serve rehearsal
health; [[ $HRC == 0 && $HOUT == *'{"state":"unlocked"}'* && $HOUT == *'HTTP=200'* ]] || { print -r -- "STOP: health"; exit 1 }
chmod 000 "$R/sd"
health; chmod 700 "$R/sd"
print -r -- "^ a connect refused by file permissions: note this message and code for step 6"
stop_serve
config "$(( $(id -u) + 1 ))"
start_serve rehearsal
health; [[ $HRC == 0 && $HOUT == *'{"error":"CALLER_NOT_ALLOWED"}'* && $HOUT == *'HTTP=403'* ]] || { print -r -- "STOP: 403"; exit 1 }
stop_serve
print -r -- "--- serve output (no secret: the rehearsal input is not echoed)"; cat "$R/serve.out"

step R5 keygen, then unlock it with serve
mkfifo "$R/kin"
/usr/bin/script -q /dev/null "$R/apa-signer" keygen --dir "$R/data" --wallet wallet-rehearsal < "$R/kin" >| "$R/keygen.out" 2>&1 &
KPID=$!
exec 4> "$R/kin"
print -u4 -r -- 'rehearsal-only passphrase'
print -u4 -r -- 'rehearsal-only passphrase'
exec 4>&-
for i in {1..100}; do ps -p "$KPID" >/dev/null || break; sleep 0.2; done
! ps -p "$KPID" >/dev/null || { print -r -- "STOP: keygen did not finish within 20 s"; exit 1 }
wait "$KPID" || true
/bin/rm -- "$R/kin"
cat "$R/keygen.out"
ADDR=$(grep -o '0x[0-9a-f]\{40\}' "$R/keygen.out" | head -1)
[[ -n $ADDR ]] || { print -r -- "STOP: no address printed"; exit 1 }
ls -le "$R/data/keys"
config "$(id -u)" "$ADDR"
start_serve 'rehearsal-only passphrase'
health; [[ $HRC == 0 && $HOUT == *'HTTP=200'* ]] || { print -r -- "STOP: the new key did not unlock"; exit 1 }
stop_serve
print -r -- "keygen + unlock: the configured wallet's key decrypted under serve"

step R6 clean up the rehearsal files
for f in "$R/apa-signer" "$R/serve.out" "$R/keygen.out" "$R/data/signer.json" "$R/data/signer.db" "$R/data/signer.db-wal" "$R/data/signer.db-shm" "$R/data/keys/wallet-rehearsal.key"; do
  [[ -e $f ]] && /bin/rm -- "$f"
done
/bin/rmdir "$R/data/keys" "$R/data" "$R/sd"
print -r -- "rehearsal done; remaining under R:"; ls -la "$R"
```

預演結束後，Codex 回報 `rehearse.log`，特別是：R0 的自我檢查、R2 的 `ps` 欄位格式、R4「被檔案權限擋住的 connect」實際訊息與退出碼、R5 的輸出。並說明本筆記第 6、7 節 `expect_fs_block` 的判斷字樣要不要調整。最後由 Codex 確認 `$R` 只剩 `home`、`tmp`、`gocache`、`rehearse.zsh`、`rehearse.log`，再整個移除這個預演目錄（先 `print -r -- "$R"` 確認是 `/tmp/apa-r.` 開頭的那個）。

## 2 建立本次目錄並建置（W-host，錄製前）

```zsh
exec /bin/zsh -f
```
```zsh
RUN=$(mktemp -d /tmp/apa-iso.XXXXXX) && chmod 700 "$RUN" && print -r -- "RUN=$RUN"
```
```zsh
( setopt ERR_EXIT NO_UNSET PIPE_FAIL
  [[ -n $RUN && -d $RUN && -O $RUN && $(stat -f %Lp "$RUN") == 700 ]] || { print -r -- "STOP: RUN"; exit 1 }
  REPO=/Users/mac/Documents/OpenSourceProjects/agent-payment-authority
  cd "$REPO"
  [[ "$(git status --porcelain)" == '?? demo' ]] || { print -r -- "STOP: the work tree is not clean"; exit 1 }
  HEAD=$(git rev-parse HEAD)
  E="$REPO/artifacts/p1b/os-isolation-$(date -u +%Y%m%dT%H%M%SZ)"
  mkdir -m 700 "$E" "$RUN/home" "$RUN/build"
  GOFLAGS=-mod=readonly GOPROXY=off GOTOOLCHAIN=local go build -o "$RUN/build/apa-signer" ./cmd/apa-signer
  SUM=$(shasum -a 256 "$RUN/build/apa-signer" | awk '{print $1}')
  [[ ${#SUM} == 64 ]] || { print -r -- "STOP: hash"; exit 1 }
  B="$RUN/build/apa-signer"; HOSTUSER=$(id -un); HOSTUID=$(id -u)
  { print -r -- "REPO=${(q)REPO}"; print -r -- "HEAD=${(q)HEAD}"; print -r -- "E=${(q)E}"; print -r -- "B=${(q)B}"
    print -r -- "SUM=${(q)SUM}"; print -r -- "HOSTUSER=${(q)HOSTUSER}"; print -r -- "HOSTUID=${(q)HOSTUID}"
    print -r -- "SOCK=/opt/apa-signer/run/signer.sock" } >| "$RUN/vars"
  cat >| "$RUN/lib.zsh" <<'EOF'
# Helpers of the P1-b OS isolation runbook (non-secret).
stop() { print -r -- "STOP: $*"; return 1 }
mark() { : >| "$RUN/ok.$1" && print -r -- "OK $1" }
need() { local s; for s in "$@"; do [[ -e $RUN/ok.$s ]] || { stop "step $s has not completed"; return 1 }; done }
# denied LABEL CMD...: CMD must fail with a permission error. Its output is discarded.
denied() {
  local label=$1 err rc; shift
  err=$("$@" 2>&1 >/dev/null) && rc=0 || rc=$?
  err=${err%%$'\n'*}
  if (( rc != 0 )) && [[ $err == *'Permission denied'* || $err == *'Operation not permitted'* ]]; then
    print -r -- "denied      $label  rc=$rc  $err"
  else
    print -r -- "UNEXPECTED  $label  rc=$rc  $err"; return 1
  fi
}
# shape PATH OWNER:GROUP:MODE: the path has exactly this owner, group and mode, and no ACL.
shape() {
  local got lines
  got=$(sudo /usr/bin/stat -f '%Su:%Sg:%Lp' "$1") || { stop "cannot stat $1"; return 1 }
  lines=$(sudo /bin/ls -led "$1" | /usr/bin/wc -l | /usr/bin/tr -d ' ')
  if [[ $got == "$2" && $lines == 1 ]]; then print -r -- "shape ok    $1  $got"; else stop "$1 is $got with $lines ls lines; want $2 and no ACL"; fi
}
# health [PREFIX...]: GET /v1/health over the socket, bounded; sets HOUT and HRC.
health() {
  HOUT=$("$@" /usr/bin/curl -sS --connect-timeout 3 --max-time 5 --unix-socket "$SOCK" -w '\nHTTP=%{http_code}' http://signer/v1/health 2>&1) && HRC=0 || HRC=$?
  print -r -- "$HOUT"; print -r -- "curl_rc=$HRC"
}
expect_ok()  { health "$@"; [[ $HRC == 0 && $HOUT == *'{"state":"unlocked"}'* && $HOUT == *'HTTP=200'* ]] || stop "not 200 unlocked" }
expect_403() { health "$@"; [[ $HRC == 0 && $HOUT == *'{"error":"CALLER_NOT_ALLOWED"}'* && $HOUT == *'HTTP=403'* ]] || stop "not 403 CALLER_NOT_ALLOWED" }
# A connect refused by file permissions: no HTTP status and a permission error. The wording comes from rehearsal R4.
expect_fs_block() { health "$@"; [[ $HRC != 0 && $HOUT == *'HTTP=000'* && $HOUT == *'ermission denied'* ]] || stop "not a file-permission refusal; judge this output before going on" }
# signer_pid: exactly one apa-signer process with exactly the serve command line.
signer_pid() {
  local pids
  pids=($(pgrep -u apa-signer -f '^/opt/apa-signer/bin/apa-signer serve --dir /opt/apa-signer/data$' || true))
  (( ${#pids} == 1 )) || { stop "want exactly one Signer process, got ${#pids}"; return 1 }
  [[ "$(ps -o user= -p "${pids[1]}" | tr -d ' ')" == apa-signer ]] || { stop "the process is not apa-signer's"; return 1 }
  [[ "$(ps -o command= -p "${pids[1]}" | sed 's/ *$//')" == '/opt/apa-signer/bin/apa-signer serve --dir /opt/apa-signer/data' ]] || { stop "unexpected command line"; return 1 }
  PID=${pids[1]}
}
EOF
  : >| "$RUN/ok.2"; print -r -- "OK 2  HEAD=$HEAD  sha256=$SUM  E=$E"
)
```
預期：`OK 2`、HEAD、hash 與證據目錄。記下 `RUN=…`，其他視窗會用到。

**進入錄製**（W-host）：
```zsh
[[ -e $RUN/ok.2 ]] && source "$RUN/vars" && script -q "$E/w-host.log" /usr/bin/env -i HOME="$RUN/home" USER="$USER" LOGNAME="$LOGNAME" TERM="$TERM" PATH=/usr/bin:/bin:/usr/sbin:/sbin RUN="$RUN" /bin/zsh -f
```
**錄製中**（W-host，第一件事）：
```zsh
source "$RUN/lib.zsh" && source "$RUN/vars" && print -r -- "recording W-host  RUN=$RUN  HEAD=$HEAD"
```

## 3 管理視窗與查核（W-admin）

```zsh
exec /bin/zsh -f
```
```zsh
read -r "RUN?RUN (W-host printed it): "
```
```zsh
if [[ $RUN == /tmp/apa-iso.?????? && -d $RUN && -O $RUN && $(stat -f %Lp "$RUN") == 700 && -e $RUN/ok.2 && -f $RUN/vars && -f $RUN/lib.zsh ]]; then source "$RUN/vars" && print -r -- "RUN ok  E=$E"; else print -r -- "STOP: RUN is not this run's directory"; fi
```
```zsh
[[ -e $RUN/ok.2 ]] && script -q "$E/w-admin.log" /usr/bin/env -i HOME="$RUN/home" USER="$USER" LOGNAME="$LOGNAME" TERM="$TERM" PATH=/usr/bin:/bin:/usr/sbin:/sbin RUN="$RUN" /bin/zsh -f
```
錄製中：
```zsh
source "$RUN/lib.zsh" && source "$RUN/vars" && print -r -- "recording W-admin  RUN=$RUN"
```
**3.1 查核**（不改系統；`sudo -l` 會要求密碼）
```zsh
( setopt ERR_EXIT NO_UNSET PIPE_FAIL
  need 2
  sw_vers
  [[ $(id -un) == "$HOSTUSER" ]] || stop "W-admin is not the Host user"
  sudo -l >| "$RUN/sudo-l.txt"
  grep -E 'Defaults|use_pty|timestamp|umask|log_input|log_output|iolog' "$RUN/sudo-l.txt" || true
  if grep -qE 'log_input|log_output|iolog' "$RUN/sudo-l.txt"; then stop "the sudo policy records input or output: stop here"; fi
  for r in /Users/apa-signer /Groups/apa-signer /Groups/apa-signer-clients; do
    if dscl . -read "$r" RecordName >/dev/null 2>&1; then stop "$r already exists"; fi
    print -r -- "absent  $r"
  done
  [[ ! -e /opt/apa-signer ]] || stop "/opt/apa-signer already exists"
  ls -led /opt
  [[ $(stat -f '%Su:%Sg:%Lp' /opt) == root:wheel:755 && $(ls -led /opt | wc -l | tr -d ' ') == 1 ]] || stop "/opt is not root:wheel 0755 without ACL"
  [[ -f $B && $(stat -f %Su "$B") == "$HOSTUSER" && $(shasum -a 256 "$B" | awk '{print $1}') == "$SUM" ]] || stop "the built binary"
  users=$(dscl . -list /Users UniqueID | awk '{print $2}')
  groups=$(dscl . -list /Groups PrimaryGroupID | awk '{print $2}')
  (( $(print -r -- "$users" | wc -l) > 10 && $(print -r -- "$groups" | wc -l) > 10 )) || stop "the ID lists look incomplete"
  free=()
  for i in {400..499}; do
    if ! print -r -- "$users"$'\n'"$groups" | grep -qx "$i"; then free+=($i); fi
    (( ${#free} == 2 )) && break
  done
  (( ${#free} == 2 )) || stop "no two free IDs in 400-499"
  { print -r -- "SIGNER_ID=${free[1]}"; print -r -- "CLIENTS_GID=${free[2]}" } >| "$RUN/ids"
  print -r -- "SIGNER_ID=${free[1]}  CLIENTS_GID=${free[2]}"
  sudo -k
  mark 3.1
)
```
預期：`absent` 三行、`/opt` 符合、兩個 ID、`OK 3.1`。`sudo -l` 的 Defaults 記在 `RUN/sudo-l.txt`（第 8 節會複製到證據目錄）；若 sudo 政策會錄製輸入或輸出就停止，不改政策。

## 4 帳號、群組、目錄（W-admin，D1）

**4.1 群組與帳號**
```zsh
( setopt ERR_EXIT NO_UNSET PIPE_FAIL
  need 3.1; source "$RUN/ids"
  sudo dseditgroup -o create -i "$SIGNER_ID" -r "APA Signer" apa-signer
  sudo dseditgroup -o create -i "$CLIENTS_GID" -r "APA Signer clients" apa-signer-clients
  sudo dscl . -create /Users/apa-signer
  sudo dscl . -create /Users/apa-signer UniqueID "$SIGNER_ID"
  sudo dscl . -create /Users/apa-signer PrimaryGroupID "$SIGNER_ID"
  sudo dscl . -create /Users/apa-signer UserShell /usr/bin/false
  sudo dscl . -create /Users/apa-signer NFSHomeDirectory /var/empty
  sudo dscl . -create /Users/apa-signer RealName "APA Signer"
  sudo dscl . -create /Users/apa-signer IsHidden 1
  sudo dscl . -create /Users/apa-signer Password '*'
  sudo dseditgroup -o edit -a "$HOSTUSER" -t user apa-signer-clients
  [[ $(id -u apa-signer) == "$SIGNER_ID" && $(id -g apa-signer) == "$SIGNER_ID" ]] || stop "apa-signer's IDs"
  id apa-signer
  sudo -k
  mark 4.1
)
```
中途失敗：停止，不自行刪除已建立的部分；把輸出交 Codex，由 Aaron 決定（帳號撤回不在本輪流程內）。

**4.2 目錄、執行檔、權限探測物**
```zsh
( setopt ERR_EXIT NO_UNSET PIPE_FAIL
  need 4.1
  sudo install -d -o root -g wheel -m 0755 /opt/apa-signer /opt/apa-signer/bin
  sudo install -d -o apa-signer -g apa-signer -m 0700 /opt/apa-signer/data /opt/apa-signer/data/keys
  sudo install -d -o apa-signer -g apa-signer-clients -m 0750 /opt/apa-signer/run
  sudo install -o root -g wheel -m 0755 "$B" /opt/apa-signer/bin/apa-signer
  print -r -- 'apa-signer isolation probe, public test text' | sudo /usr/bin/tee /opt/apa-signer/bin/probe.txt >/dev/null
  sudo /bin/chmod 0644 /opt/apa-signer/bin/probe.txt
  sudo install -d -o root -g wheel -m 0755 /opt/apa-signer/probe-dir
  shape /opt root:wheel:755
  shape /opt/apa-signer root:wheel:755
  shape /opt/apa-signer/bin root:wheel:755
  shape /opt/apa-signer/bin/apa-signer root:wheel:755
  shape /opt/apa-signer/bin/probe.txt root:wheel:644
  shape /opt/apa-signer/probe-dir root:wheel:755
  shape /opt/apa-signer/data apa-signer:apa-signer:700
  shape /opt/apa-signer/data/keys apa-signer:apa-signer:700
  shape /opt/apa-signer/run apa-signer:apa-signer-clients:750
  [[ $(shasum -a 256 /opt/apa-signer/bin/apa-signer | awk '{print $1}') == "$SUM" ]] || stop "installed binary hash"
  sudo -k
  mark 4.2
)
```
**權限探測物與被測目標的對應**（只代表這些對應關係，不誇大）：
- `bin/probe.txt`（`root:wheel 0644`）和執行檔同目錄、同擁有者與群組，群組與其他人都沒有寫入位元。對它的追加、替換被拒，代表 Host 不能改寫這個目錄裡 root 擁有的檔案，但不是直接對執行檔的測試。
- 「在 `bin/` 建新檔」被拒，代表 Host 不能在執行檔旁放東西。
- `probe-dir/`（`root:wheel 0755`）和 `bin/` 同一個上層、同形狀。對它改名被拒，代表 Host 不能移動 `bin/`。
- 真正的執行檔與 `bin/` 不被拿來測試。

**4.3 群組生效**（W-host）：在錄製中 `exit`，關閉 W-host；開**新的** W-host：
```zsh
exec /bin/zsh -f
```
```zsh
read -r "RUN?RUN: "
```
```zsh
if [[ $RUN == /tmp/apa-iso.?????? && -d $RUN && -O $RUN && -e $RUN/ok.4.2 ]]; then source "$RUN/vars" && print -r -- "RUN ok"; else print -r -- "STOP: RUN"; fi
```
```zsh
( setopt ERR_EXIT NO_UNSET PIPE_FAIL
  source "$RUN/lib.zsh"; need 4.2
  id -Gn | tr ' ' '\n' | grep -qx apa-signer-clients || stop "this session is not in apa-signer-clients yet: log out and in once, then repeat 4.3"
  mark 4.3
)
```
```zsh
[[ -e $RUN/ok.4.3 ]] && script -a -q "$E/w-host.log" /usr/bin/env -i HOME="$RUN/home" USER="$USER" LOGNAME="$LOGNAME" TERM="$TERM" PATH=/usr/bin:/bin:/usr/sbin:/sbin RUN="$RUN" /bin/zsh -f
```
錄製中：`source "$RUN/lib.zsh" && source "$RUN/vars" && id -Gn`

## 5 A 階段：公開探測檔（D2）

**5.1 建立探測檔並確認 Signer 身分可讀寫**（W-admin）
```zsh
( setopt ERR_EXIT NO_UNSET PIPE_FAIL
  need 4.3; cd /
  sudo -u apa-signer /bin/sh -c 'umask 077; for f in /opt/apa-signer/data/probe.txt /opt/apa-signer/data/keys/probe.txt; do printf "%s\n" "apa-signer isolation probe, public test text" > "$f" || exit 1; done'
  for f in /opt/apa-signer/data/probe.txt /opt/apa-signer/data/keys/probe.txt; do
    sudo -u apa-signer /bin/dd if="$f" of=/dev/null bs=1 count=1 2>/dev/null || stop "apa-signer cannot read $f"
    sudo -u apa-signer /bin/test -w "$f" || stop "apa-signer cannot write $f"
    shape "$f" apa-signer:apa-signer:600
  done
  sudo /usr/bin/shasum -a 256 /opt/apa-signer/data/probe.txt /opt/apa-signer/data/keys/probe.txt /opt/apa-signer/bin/probe.txt >| "$RUN/probe-hashes"
  sudo -k
  mark 5.1
)
```
只有 5.1 通過，後面的「被拒」才算數。

**5.2 Host 使用者**（W-host，不用 sudo）
```zsh
( setopt ERR_EXIT NO_UNSET PIPE_FAIL
  need 5.1
  id
  for f in /opt/apa-signer/data/probe.txt /opt/apa-signer/data/keys/probe.txt; do
    denied "read $f" /bin/dd if="$f" of=/dev/null bs=1 count=1
    denied "append $f" /bin/sh -c 'printf x >> "$1"' sh "$f"
    denied "replace $f" /bin/cp /etc/hosts "$f"
    denied "rename $f" /bin/mv "$f" "$f.x"
  done
  denied "list data" /bin/ls /opt/apa-signer/data
  denied "append bin/probe.txt" /bin/sh -c 'printf x >> "$1"' sh /opt/apa-signer/bin/probe.txt
  denied "replace bin/probe.txt" /bin/cp /etc/hosts /opt/apa-signer/bin/probe.txt
  denied "create in bin" /usr/bin/touch /opt/apa-signer/bin/new-file
  denied "rename probe-dir" /bin/mv /opt/apa-signer/probe-dir /opt/apa-signer/probe-dir.x
  denied "create in run" /usr/bin/touch /opt/apa-signer/run/new-file
  mark 5.2
)
```
**5.3 `nobody` 與事後核對**（W-admin）
```zsh
( setopt ERR_EXIT NO_UNSET PIPE_FAIL
  need 5.2; cd /
  sudo -u nobody /usr/bin/id
  denied "nobody read data/probe.txt" sudo -u nobody /bin/dd if=/opt/apa-signer/data/probe.txt of=/dev/null bs=1 count=1
  denied "nobody list run" sudo -u nobody /bin/ls /opt/apa-signer/run
  sudo /usr/bin/shasum -a 256 /opt/apa-signer/data/probe.txt /opt/apa-signer/data/keys/probe.txt /opt/apa-signer/bin/probe.txt | diff "$RUN/probe-hashes" - || stop "a probe changed"
  extra=$(sudo /usr/bin/find /opt/apa-signer \( -name '*.x' -o -name new-file \) -print)
  [[ -z $extra ]] || stop "new paths appeared: $extra"
  sudo -k
  mark 5.3
)
```
**任何非預期成功**（5.2 或 5.3 出現 `UNEXPECTED`）：立刻停止，不做下一階段。由 Aaron 在 W-admin 檢查：
- 被測檔的身分與權限：`shape`；
- 內容雜湊：和 `RUN/probe-hashes` 比較；
- 有沒有新增路徑：上面的 `find`。

把輸出交 Codex 判讀。

## 6 B0：不帶金鑰的真正 Signer（D3）

**6.1 設定**（W-host）
```zsh
( setopt ERR_EXIT NO_UNSET PIPE_FAIL
  need 5.3
  print -r -- "{\"version\":1,\"socket\":\"$SOCK\",\"callers\":[$HOSTUID],\"max_window_seconds\":600,\"wallets\":[],\"assets\":[],\"delegations\":[]}" >| "$RUN/b0.json"
  cat "$RUN/b0.json"
  mark 6.1
)
```
**6.2 安裝設定**（W-admin）
```zsh
( setopt ERR_EXIT NO_UNSET PIPE_FAIL
  need 6.1; cd /
  [[ ! -e $SOCK ]] || stop "a socket already exists"
  if pgrep -u apa-signer >/dev/null; then stop "apa-signer already has a process"; fi
  sudo -u apa-signer /bin/sh -c 'umask 077; cat > /opt/apa-signer/data/signer.json' < "$RUN/b0.json"
  shape /opt/apa-signer/data/signer.json apa-signer:apa-signer:600
  sudo -k
  mark 6.2
)
```
**6.3 啟動**（W-signer，不錄製）
```zsh
exec /bin/zsh -f
```
```zsh
read -r "RUN?RUN: "
```
```zsh
if [[ $RUN == /tmp/apa-iso.?????? && -d $RUN && -e $RUN/ok.6.2 ]]; then cd / && sudo -u apa-signer /bin/sh -c 'umask 077; exec /opt/apa-signer/bin/apa-signer serve --dir /opt/apa-signer/data'; else print -r -- "STOP: step 6.2 has not completed"; fi
```
出現 `apa-signer passphrase:` 時輸入任意一行（B0 不使用它）。預期：`apa-signer: serving on /opt/apa-signer/run/signer.sock (reservation verification unavailable until P2: nothing is signed)`。
停止：出現 `there is none`（讀不到 `/dev/tty`）或其他錯誤。這是待實測項目，交 Codex 判讀，不改程式或輸入方式。

**6.4 Host 使用者**（W-host）
```zsh
( setopt ERR_EXIT NO_UNSET PIPE_FAIL
  need 6.2
  for i in {1..30}; do [[ -S $SOCK ]] && break; sleep 1; done
  [[ -S $SOCK ]] || stop "no socket within 30 s"
  signer_pid; print -r -- "PID=$PID" >| "$RUN/pid"; print -r -- "Signer PID=$PID"
  ls -led "$SOCK"
  [[ $(stat -f '%Su:%Sg:%Lp' "$SOCK") == apa-signer:apa-signer-clients:660 && $(ls -led "$SOCK" | wc -l | tr -d ' ') == 1 ]] || stop "the socket's owner, group, mode or ACL"
  expect_ok
  denied "read signer.json" /bin/dd if=/opt/apa-signer/data/signer.json of=/dev/null bs=1 count=1
  denied "read signer.db" /bin/dd if=/opt/apa-signer/data/signer.db of=/dev/null bs=1 count=1
  denied "list data" /bin/ls /opt/apa-signer/data
  mark 6.4
)
```
**6.5 peer uid 與檔案權限**（W-admin）
```zsh
( setopt ERR_EXIT NO_UNSET PIPE_FAIL
  need 6.4; cd /; source "$RUN/pid"
  signer_pid
  expect_ok
  expect_403 sudo -u apa-signer
  expect_fs_block sudo -u nobody
  print -r -- "arguments: only 'serve --dir /opt/apa-signer/data' (checked by signer_pid); environment: not observed (see section 7)"
  sudo -k
  mark 6.5
)
```
三種結果分開記：
- Host 的 `expect_ok`（200）；
- `apa-signer` 的 `expect_403`：已連線、由 peer uid 拒絕；
- `nobody` 的 `expect_fs_block`：檔案權限擋住，沒有 HTTP 狀態。

緊接在 `nobody` 之前的 `expect_ok` 證明 socket 當時活著。`nobody` 只靠退出碼不算數：必須同時沒有 HTTP 狀態、有權限錯誤字樣。字樣若和預演 R4 不同，`expect_fs_block` 會停下讓 Codex 判讀。

**6.6 W-signer 觀察**（W-admin，錄製中由 Aaron 回答）
```zsh
read -r "OBS?W-signer: exactly one new 'caller  CALLER_NOT_ALLOWED' line, no line for nobody, and no passphrase anywhere (yes/no): "; print -r -- "W-signer observation: $OBS"
```
**6.7 停止**：在 W-signer 按 Ctrl-C。然後（W-host）：
```zsh
( setopt ERR_EXIT NO_UNSET PIPE_FAIL
  need 6.5; source "$RUN/pid"
  for i in {1..10}; do ps -p "$PID" >/dev/null || break; sleep 1; done
  if ps -p "$PID" >/dev/null; then stop "the Signer is still running"; fi
  [[ ! -e $SOCK ]] || stop "the socket was left behind (do not delete anything; judge with Codex)"
  mark 6.7
)
```

## 7 B1：一次性測試金鑰（D3、D4）

**7.1 建金鑰**（W-signer，不錄製；兩次提示輸入同一個**新的**測試密語，至少 12 字元，只用於這把金鑰）
```zsh
if [[ -e $RUN/ok.6.7 ]]; then cd / && sudo -u apa-signer /bin/sh -c 'umask 077; exec /opt/apa-signer/bin/apa-signer keygen --dir /opt/apa-signer/data --wallet wallet-isolation-test'; else print -r -- "STOP: step 6.7 has not completed"; fi
```
預期：`apa-signer: wallet wallet-isolation-test address 0x…`（地址是公開資訊）。

**7.2 設定**（W-host；輸入上面印出的地址）
```zsh
read -r "ADDR?address printed by keygen: "
```
```zsh
( setopt ERR_EXIT NO_UNSET PIPE_FAIL
  need 6.7
  [[ $ADDR =~ '^0x[0-9a-f]{40}$' ]] || stop "not an address"
  print -r -- "{\"version\":1,\"socket\":\"$SOCK\",\"callers\":[$HOSTUID],\"max_window_seconds\":600,\"wallets\":[{\"wallet_id\":\"wallet-isolation-test\",\"consumer_id\":\"consumer-isolation-test\",\"chain_id\":\"84532\",\"address\":\"$ADDR\"}],\"assets\":[],\"delegations\":[]}" >| "$RUN/b1.json"
  cat "$RUN/b1.json"
  mark 7.2
)
```
**7.3 安裝設定**（W-admin）
```zsh
( setopt ERR_EXIT NO_UNSET PIPE_FAIL
  need 7.2; cd /
  shape /opt/apa-signer/data/keys/wallet-isolation-test.key apa-signer:apa-signer:600
  [[ ! -e $SOCK ]] || stop "a socket already exists"
  if pgrep -u apa-signer >/dev/null; then stop "apa-signer already has a process"; fi
  sudo -u apa-signer /bin/sh -c 'umask 077; cat > /opt/apa-signer/data/signer.json' < "$RUN/b1.json"
  shape /opt/apa-signer/data/signer.json apa-signer:apa-signer:600
  sudo -k
  mark 7.3
)
```
**7.4 啟動**（W-signer；這次輸入**測試金鑰的密語**）
```zsh
if [[ -e $RUN/ok.7.3 ]]; then cd / && sudo -u apa-signer /bin/sh -c 'umask 077; exec /opt/apa-signer/bin/apa-signer serve --dir /opt/apa-signer/data'; else print -r -- "STOP: step 7.3 has not completed"; fi
```
預期出現 serving，表示設定裡的錢包金鑰在 `apa-signer` 下解鎖。設定沒有任何委託，不可能簽出授權。

**7.5 Host 使用者**（W-host）
```zsh
( setopt ERR_EXIT NO_UNSET PIPE_FAIL
  need 7.3
  for i in {1..30}; do [[ -S $SOCK ]] && break; sleep 1; done
  [[ -S $SOCK ]] || stop "no socket within 30 s"
  signer_pid; print -r -- "PID=$PID" >| "$RUN/pid"; print -r -- "Signer PID=$PID"
  [[ $(stat -f '%Su:%Sg:%Lp' "$SOCK") == apa-signer:apa-signer-clients:660 && $(ls -led "$SOCK" | wc -l | tr -d ' ') == 1 ]] || stop "the socket's owner, group, mode or ACL"
  expect_ok
  denied "read the test key" /bin/dd if=/opt/apa-signer/data/keys/wallet-isolation-test.key of=/dev/null bs=1 count=1
  denied "read signer.json" /bin/dd if=/opt/apa-signer/data/signer.json of=/dev/null bs=1 count=1
  denied "read signer.db" /bin/dd if=/opt/apa-signer/data/signer.db of=/dev/null bs=1 count=1
  mark 7.5
)
```
**7.6**（W-admin）
```zsh
( setopt ERR_EXIT NO_UNSET PIPE_FAIL
  need 7.5; cd /
  signer_pid
  expect_ok
  expect_403 sudo -u apa-signer
  expect_fs_block sudo -u nobody
  sudo -k
  mark 7.6
)
```
```zsh
read -r "OBS?W-signer: one new 'caller  CALLER_NOT_ALLOWED' line, none for nobody, no passphrase anywhere (yes/no): "; print -r -- "W-signer observation: $OBS"
```
**7.7 停止**：W-signer 按 Ctrl-C，然後（W-host）：
```zsh
( setopt ERR_EXIT NO_UNSET PIPE_FAIL
  need 7.6; source "$RUN/pid"
  for i in {1..10}; do ps -p "$PID" >/dev/null || break; sleep 1; done
  if ps -p "$PID" >/dev/null; then stop "the Signer is still running"; fi
  [[ ! -e $SOCK ]] || stop "the socket was left behind (do not delete anything; judge with Codex)"
  mark 7.7
)
```
W-signer 最後執行 `sudo -k`，然後關閉視窗。

## 8 撤回測試資料（D5：保留帳號、群組與目錄）

**8.1 確認已停止並列清單**（W-admin）
```zsh
( setopt ERR_EXIT NO_UNSET PIPE_FAIL
  need 7.7; cd /
  if pgrep -u apa-signer >/dev/null; then stop "apa-signer still has a process"; fi
  [[ ! -e $SOCK ]] || stop "a socket is present: judge with Codex before removing anything"
  sudo /usr/bin/find /opt/apa-signer -mindepth 1 | sort >| "$RUN/inventory"
  cat "$RUN/inventory"
  print -l /opt/apa-signer/bin /opt/apa-signer/bin/apa-signer /opt/apa-signer/bin/probe.txt /opt/apa-signer/data \
    /opt/apa-signer/data/keys /opt/apa-signer/data/keys/probe.txt /opt/apa-signer/data/keys/wallet-isolation-test.key \
    /opt/apa-signer/data/probe.txt /opt/apa-signer/data/signer.db /opt/apa-signer/data/signer.db-shm /opt/apa-signer/data/signer.db-wal \
    /opt/apa-signer/data/signer.json /opt/apa-signer/probe-dir /opt/apa-signer/run | sort >| "$RUN/known"
  unknown=$(comm -23 "$RUN/inventory" "$RUN/known")
  [[ -z $unknown ]] || stop "unknown items, nothing will be removed: $unknown"
  sudo -k
  mark 8.1
)
```
Aaron 看過清單，確定只有上面已知的項目，再做 8.2。

**8.2 只移除本次建立的具名測試項目**（W-admin）
```zsh
( setopt ERR_EXIT NO_UNSET PIPE_FAIL
  need 8.1; cd /
  for f in /opt/apa-signer/data/keys/wallet-isolation-test.key /opt/apa-signer/data/keys/probe.txt /opt/apa-signer/data/probe.txt \
           /opt/apa-signer/data/signer.json /opt/apa-signer/data/signer.db /opt/apa-signer/data/signer.db-wal /opt/apa-signer/data/signer.db-shm \
           /opt/apa-signer/bin/probe.txt; do
    if sudo /bin/test -f "$f"; then sudo /bin/rm -- "$f"; print -r -- "removed  $f"; fi
  done
  sudo /bin/rmdir /opt/apa-signer/probe-dir; print -r -- "removed  /opt/apa-signer/probe-dir"
  sudo /usr/bin/find /opt/apa-signer -mindepth 1 | sort >| "$RUN/remaining"
  print -l /opt/apa-signer/bin /opt/apa-signer/bin/apa-signer /opt/apa-signer/data /opt/apa-signer/data/keys /opt/apa-signer/run | sort | diff "$RUN/remaining" - || stop "what remains is not the kept set"
  shape /opt/apa-signer/data apa-signer:apa-signer:700
  shape /opt/apa-signer/data/keys apa-signer:apa-signer:700
  shape /opt/apa-signer/run apa-signer:apa-signer-clients:750
  cp "$RUN/sudo-l.txt" "$RUN/ids" "$RUN/inventory" "$RUN/remaining" "$E/"
  sudo -k
  mark 8.2
)
```
預期：保留 `bin/`、執行檔、`data/`、`data/keys/`、`run/`，以及帳號與群組（D5）；只移除本次的測試檔、設定、DB 與兩個權限探測物。

**8.3 結束錄製並清理本次暫存**：W-admin 與 W-host 都在錄製中 `exit`。然後在 W-host（外層 `zsh -f`）：
```zsh
( setopt ERR_EXIT NO_UNSET PIPE_FAIL
  [[ $RUN == /tmp/apa-iso.?????? && -O $RUN && -e $RUN/ok.8.2 ]] || { print -r -- "STOP: RUN"; exit 1 }
  /bin/rm -- "$RUN/build/apa-signer" "$RUN/b0.json" "$RUN/b1.json" "$RUN/vars" "$RUN/ids" "$RUN/lib.zsh" "$RUN/pid" \
             "$RUN/probe-hashes" "$RUN/sudo-l.txt" "$RUN/inventory" "$RUN/known" "$RUN/remaining" "$RUN"/ok.*
  /bin/rmdir "$RUN/build" "$RUN/home" "$RUN"
  print -r -- "removed $RUN"
)
```
`rmdir` 失敗代表裡面還有其他檔案。這時停止，把清單交 Codex。

## 9 判讀表、未觀測範圍與修訂對照

**判讀**（Codex 依 `E/w-host.log`、`E/w-admin.log` 與複製的清單填）：

| 條件 | 關卡 | PASS 規則 |
|---|---|---|
| 帳號、群組、目錄 | 4.1、4.2 | `OK 4.1`、`OK 4.2`，所有 `shape ok`，hash 相同 |
| 群組生效 | 4.3 | `OK 4.3` |
| Host 讀不到、改不了私有資料；不能改寫 `bin/` 裡 root 擁有的檔案、不能在 `bin/`／`run/` 建檔、不能移動 `bin/` 的同形狀目錄 | 5.1～5.3、6.4、7.5 | 前置 `OK 5.1` 存在，且每一行都是 `denied` |
| `nobody` 被檔案權限擋住 | 5.3、6.5、7.6 | `denied`；`expect_fs_block` 通過 |
| Signer 以 `apa-signer` 前景執行並讀到 `/dev/tty` | 6.3、6.4 | W-signer 印出 serving；`signer_pid` 通過 |
| socket 擁有者、群組、權限 | 6.4、7.5 | `apa-signer:apa-signer-clients:660`，沒有 ACL |
| Host 呼叫 health | 6.4、7.5 | `expect_ok` |
| peer uid 拒絕 | 6.5、7.6 | `expect_403`，加上 6.6／7.6 的觀察為 yes |
| 金鑰在 `apa-signer` 下解鎖；Host 讀不到金鑰檔 | 7.4、7.5 | serving；`denied "read the test key"` |
| 撤回 | 8.1、8.2 | 沒有未知項目；剩餘等於保留清單 |

**未觀測範圍**（只能下有限結論）：
- 密語：可支持的結論是「這次觀測的 Signer 命令列只有 `serve --dir …`（`signer_pid` 逐字比對）；W-signer 的提示關閉回顯；錄製的視窗沒有輸入密語」。
- 環境變數：**未觀測**。本機 `ps(1)`：`-E`「does not reflect changes in the environment after process launch」，而且要列出完整環境才能檢查，本筆記不這麼做。程式碼只從 `/dev/tty` 讀密語、不寫進環境變數（原始碼依據，不是本次觀測）。
- 終端機捲動緩衝、其他管理者工具、記憶體：不在觀測範圍。觀測不到不等於不存在。
- 「Signer 不能連網」（D7）：未驗證。「Signer 不能改自己的設定」不成立於 OS 層（D6，已採 C1）。

**修訂對照**（第一版 `0ef6172` → 本版）：

| 項目 | 第一版的問題 | 本版 |
|---|---|---|
| O1-1 交接值 | `E=<貼上>` 之類的佔位字串 | 只輸入 `RUN` 一個路徑（`read -r`），檢查格式、擁有者、`0700`、標記與檔案；其他值從 `RUN/vars` 讀 |
| O1-2 錄製階段 | 外層變數以為會進 `script` 的子 shell | 錄製前、錄製中（`env -i` 白名單，只拿 `RUN`，再 `source vars`）、錄製後三階段分開；一律 `/bin/zsh -f` |
| O1-3 失敗即停 | 只在段落末尾寫「失敗就停止」 | 每段 `ERR_EXIT` 子 shell＋`need`／`mark` 標記；預期拒絕用 `denied` 另外判斷 |
| O1-4 退出碼 | `echo exit=$?` 會被管線或 `script` 蓋掉 | `denied`／`health` 立即保存退出碼；UID 清單不完整、PID 不唯一、檔案不存在都停止 |
| O1-5 時限與 PID | curl 沒時限、預演靠 `sleep 3`、用 `pkill -f` | curl `--connect-timeout 3 --max-time 5`；就緒等待有上限並核對 socket；預演以啟動時核對過的 PID 停止 |
| O1-6 R5 | 只列檔，不能支持「可解鎖」 | R5 把產出的地址寫進設定，再以 `serve` 解鎖後 health 200 |
| O2-1 環境列印 | 錄製中 `ps -E` 與目前 shell 的完整環境 | 全部移除；環境變數列為未觀測 |
| O2-2 `ps -E` 限制 | 宣稱密語不在參數、環境、輸出 | 改成有限結論並列出未觀測範圍 |
| O2-3 拒絕讀取 | 用 `cat` 把內容送到錄製終端機 | `dd … of=/dev/null bs=1 count=1`，內容不輸出；意外可讀就 `UNEXPECTED` 停止 |
| O2-4 二進位 | `cp` 覆寫真正的執行檔、`mv` 真正的 `bin/` | 改用 `bin/probe.txt` 與 `probe-dir/`，先 `shape` 核對並寫明代表範圍 |
| O2-5 非預期成功 | 沒有處理 | 立刻停止；以雜湊、權限與新路徑檢查後交判讀 |
| O2-6 三種結果 | 不存在的 socket 的錯誤碼被當成參考 | 200／403／檔案權限分開；`nobody` 必須同時沒有 HTTP 狀態、有權限字樣，緊接在 200 之後 |
| O2-7 sudo 錄製 | 沒查 | 3.1 讀 `sudo -l`，有 `log_input`／`log_output`／`iolog` 就停止 |
| O3-1 暫存檔 | `/tmp/apa-signer-b0.json`、`mktemp -u` | 本次 `mktemp -d` 目錄裡的檔案；預演 socket 在本次目錄、長度 < 100 |
| O3-2 建置產物 | 固定的 `$TMPDIR/apa-signer-rehearsal` | 本次目錄的 `build/`；hash 寫入交接檔 |
| O3-3 撤回前確認 | 以 Ctrl-C 推定已停止 | 以 PID 與 `pgrep -u apa-signer` 確認沒有程序、socket 不存在，否則不刪 |
| O3-4 先列再刪 | `ls` 與 `rm` 同一段 | 8.1 列清單並和已知清單比對（未知就停止），8.2 是另一個關卡 |
| O3-5 只刪本次 | — | 具名逐一刪除；DB sidecar 逐個確認；保留帳號、群組與目錄，不新增刪帳號流程 |
| O3-6 「data 應為空」 | 錯誤 | 驗證剩餘項目等於保留清單（含 `keys/`） |
| O3-7 遞迴刪除 | — | 沒有；本次暫存目錄以具名刪除加 `rmdir`。預演目錄由 Codex 先確認路徑再移除 |

**原第 8 節五個問題的處理**：
1. 預演結果與修正：第 1 節改成受控預演腳本，回報要點寫在第 1 節末尾。
2. 讀不到 `/dev/tty`：6.3 停止並交判讀；可能的選項（程式在 stdin 是終端機時改讀 stdin，或調整 sudo 的 pty 設定）都需另行核准，本輪不改。
3. `ps -E`：不足以觀測，已移除，列為未觀測（第 9 節）。
4. 證據與停止條件：錄製兩個視窗；所有關卡有 `STOP`／`UNEXPECTED`；清單複製到證據目錄。
5. 撤回範圍：8.1 未知就停止，8.2 只刪具名項目，保留 D5 決定的資源。

**仍需預演或第一次執行才能確認**：
- `sandbox-exec` 設定檔的效果（R0）；
- 本機 `ps`、`pgrep`、`curl` 的實際格式（R2、R4）；
- 被檔案權限擋住時 curl 的訊息（R4，影響 `expect_fs_block`）；
- `script` 以 FIFO 為輸入時的行為（R4、R5）；
- 跨使用者、sudo、`/dev/tty`、群組生效、socket 群組繼承（第 4～7 節）。

## 10 語法檢查紀錄（本版提交前，Claude Code）

- **方法**：把本文件每個 ```` ```zsh ```` 區塊依序另存成檔案（共 44 塊），各跑一次 `/bin/zsh -f -n <檔案>`（zsh 5.8.1，只解析、不執行），記錄退出碼與 stderr。本文件沒有其他 shell 的區塊。
- **結果**：44 塊的退出碼都是 0。

| 區塊 | 章節 | 退出碼 | stderr |
|---|---|---|---|
| 01～02 | 1 受控預演（1.1 啟動、1.2 `rehearse.zsh`） | 0 | 01 無；02 見下 |
| 03～07 | 2 建立本次目錄並建置、進入錄製 | 0 | 無 |
| 08～13 | 3 管理視窗與查核 | 0 | 無 |
| 14～20 | 4 帳號、群組、目錄、群組生效 | 0 | 無 |
| 21～23 | 5 A 階段 | 0 | 無 |
| 24～32 | 6 B0 | 0 | 無 |
| 33～41 | 7 B1 | 0 | 無 |
| 42～44 | 8 撤回 | 0 | 無 |

- **區塊 02 的 stderr**：在 Claude Code 的工具沙箱裡解析時，第 20、79 行（兩個 `&` 背景工作）各印一行 `nice(5) failed: operation not permitted`。在沙箱外重新解析同一個檔案，退出碼 0、stderr 為空；加 `-o NO_BG_NICE` 也一樣。判讀：這是 zsh 的 `BG_NICE` 在工具沙箱裡被拒絕調整優先權，不是 parser 診斷。
- **限制**：語法通過不代表參數合法、路徑存在或行為如預期。第 9 節列出的項目仍要預演或第一次執行才能確認。
