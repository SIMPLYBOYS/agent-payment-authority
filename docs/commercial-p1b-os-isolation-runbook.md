# P1-b OS 隔離驗收：指令筆記（給 Aaron 逐關卡操作、Codex 陪同判讀）

> 2026-10-13，Claude Code。第三版：依 vault〈Claude-Code-P1-bOS隔離驗收指令第二輪限定補正派工-c7744be-20261013〉補正第二輪 O1～O4（修訂對照見第 9 節）。第二版（`c7744be`，第一輪 O1～O3）與第一版（`0ef6172`）保留在 Git 歷史。
> **狀態：P1-b 既有實作缺口已限定複核；A／B0／B1 與測試資料清理完成，限定結案紀錄待最後核對；正式付款未啟用。**（10/13 這一次的 A／B0／B1 與清理紀錄見 [方案](commercial-p1b-os-isolation.md) 第 8.6、8.7 節；本筆記第 6.5 節後附 B0 的補充判讀，第 8.2 節後附這次的拆段說明。）
> 這一次用的 `RUN=/tmp/apa-iso.I0Wtz3` 已由 Aaron 清除，不是下一次操作的入口；下一次若要再操作，從第 2 節重新建立新的 `RUN`。
> 以下為 `b7cc85d` 時的狀態，保留原文：keygen 通過 B1 一次性未注資測試金鑰用途的限定技術複核；A／B0 由 Aaron 依 Route B 操作並完成安全停止，Codex 依回報判讀。B1 金鑰隔離仍待 Aaron 操作；P1-b 未結案，正式付款未啟用。
> 以下為 `8fb5f8c` 時的狀態，保留原文：OS 隔離驗收指令第二輪補正完成，待 Codex 限定複核；預演與跨使用者驗收未完成，正式付款未啟用。
> 依據：[commercial-p1b-os-isolation.md](commercial-p1b-os-isolation.md)（方案，Aaron 已核准 D1～D7，第 8 節）；程式 `cmd/apa-signer`（`serve`、`keygen`）。
> **命令都還沒有執行過。**本版只做了 `zsh -f -n` 語法解析（第 10 節），沒有執行任何區塊，也沒有建置、啟動 Signer、keygen、sudo 或改系統。語法通過不等於參數合法或操作已驗證。跨使用者、sudo、`/dev/tty` 經 sudo、群組生效、socket 群組繼承，只能在第一次執行時得知。
> 歸屬：`serve`、`keygen` 的程式行為由 Claude Code 測試（同一 uid；`keygen` 見方案第 8.2 節），keygen 尚未經 Codex 技術複核。Codex 前一輪只做文件、原始碼、本機手冊與語法預檢，沒有建置或啟動 Signer，沒有 keygen、sudo 或跨使用者測試，也沒有 repo 證據 log。

## 0 角色與通則

| 誰 | 做什麼 |
|---|---|
| Codex | 路徑 B：在每個關卡讀錄製視窗的非敏感輸出、判讀、建議（路徑 A 的受控預演目前不可用，第 1 節）。不輸入、不接收、不保存任何密碼或密語 |
| Aaron | 選擇路徑，執行第 2～8 節；所有 sudo 密碼與密語只由 Aaron 在鍵盤上輸入 |

- **一律用乾淨的 zsh**：每個視窗先執行 `exec /bin/zsh -f`，不載入個人啟動設定。本筆記所有區塊都是 zsh 語法。
- **三個視窗**（每段都標明在哪個視窗執行；`need`、`mark`、`denied` 等只在錄製中的 W-host／W-admin 載入，貼到 W-signer 會得到 `command not found`，而且這種錯誤不能用來推定任何狀態）：
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

## 1 操作路徑（開始前由 Aaron 選擇）

| 路徑 | 是什麼 | 現況 |
|---|---|---|
| **A：Codex 受控預演** | Codex 用自己的 uid、在強制隔離下跑一次 `serve`／`keygen`，修正本筆記 | **未執行，目前不可用**（見下） |
| **B：Aaron 逐關卡操作，Codex 陪同判讀** | Aaron 在自己的終端機、於已核准的 D1～D7 範圍內，依第 2～8 節逐關卡操作；Codex 只讀錄製視窗裡的非敏感輸出、判讀並建議 | 可用。**這不是 Codex 的隔離預演**，任何結果都不能寫成「預演 PASS」 |

**路徑 A 的前置條件**：下列控制**全部**都能被強制執行，才可以啟動。缺任一項就記為「未執行」，不用自我檢查或風險說明代替控制。
- OS 強制斷網；
- 讀取隔離：不能讀日常 HOME，不能接觸本機其他 socket；
- 寫入只限本次目錄；
- 乾淨的白名單環境、暫存 HOME；
- 資源上限：記憶體、程序數、磁碟、整體牆鐘時間與 CPU 時間。

目前的狀況：
- 第二版的 `sandbox-exec` 設定檔用了 `allow default`，沒有限制讀取 HOME 或接觸其他 socket；`env -i` 與暫存 HOME 也不等於讀取隔離。
- macOS 的 `ulimit` 不能強制記憶體上限；`ulimit -u` 算的是整個 uid 的程序數；沒有磁碟配額；也沒有整體牆鐘時間上限。

因此路徑 A **目前不可用**。本輪不實作新的 macOS 沙箱、VM 或驗證框架。第二版的預演腳本已從本筆記移除（它的寫入自我檢查會截斷真實 repo 裡的固定路徑，而且失敗時不保證停止已啟動的程序），只保留在 Git 歷史（`c7744be` 第 1 節），**不可執行**。之後若要採用路徑 A，須先能提供上列全部控制，再另行提出。

**已經可以先完成的**（不需要路徑 A）：語法解析（第 10 節）、本機手冊與原始碼核對。Linux 容器裡的既有驗證不能充當 macOS 的 TTY、跨使用者或部署驗收。

**選擇方式**：第 2 節開始時，Aaron 在 W-host 輸入 `B` 選擇路徑 B。只接受 `B`；選擇會寫進交接檔與錄製紀錄。

## 2 建立本次目錄並建置（W-host，錄製前）

```zsh
exec /bin/zsh -f
```
```zsh
read -r "ROUTE?operating route (only B: Aaron step by step, Codex judges the output): "
```
```zsh
RUN=$(mktemp -d /tmp/apa-iso.XXXXXX) && chmod 700 "$RUN" && print -r -- "RUN=$RUN"
```
```zsh
( setopt ERR_EXIT NO_UNSET PIPE_FAIL
  [[ ${ROUTE:-} == B ]] || { print -r -- "STOP: choose route B (route A is not available)"; exit 1 }
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
    print -r -- "SOCK=/opt/apa-signer/run/signer.sock"; print -r -- "ROUTE=B" } >| "$RUN/vars"
  cat >| "$RUN/lib.zsh" <<'EOF'
# Helpers of the P1-b OS isolation runbook (non-secret).
stop() { print -r -- "STOP: $*" >&2; return 1 }
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
# A connect refused by file permissions: no HTTP status and a permission error. The wording is not yet
# observed on this machine; any other wording stops for judgement.
expect_fs_block() { health "$@"; [[ $HRC != 0 && $HOUT == *'HTTP=000'* && $HOUT == *'ermission denied'* ]] || stop "not a file-permission refusal; judge this output before going on" }
# signer_pid: exactly one apa-signer process with exactly the serve command line.
# pgrep: 0 found, 1 none matched, anything else is an error (never "none").
signer_pid() {
  local out rc pids
  out=$(pgrep -u apa-signer -f '^/opt/apa-signer/bin/apa-signer serve --dir /opt/apa-signer/data$' 2>&1) && rc=0 || rc=$?
  case $rc in
    0) pids=(${=out}) ;;
    1) stop "no Signer process"; return 1 ;;
    *) stop "pgrep failed (rc=$rc): $out"; return 1 ;;
  esac
  (( ${#pids} == 1 )) || { stop "want exactly one Signer process, got ${#pids}"; return 1 }
  [[ "$(ps -o user= -p "${pids[1]}" | tr -d ' ')" == apa-signer ]] || { stop "the process is not apa-signer's"; return 1 }
  [[ "$(ps -o command= -p "${pids[1]}" | sed 's/ *$//')" == '/opt/apa-signer/bin/apa-signer serve --dir /opt/apa-signer/data' ]] || { stop "unexpected command line"; return 1 }
  PID=${pids[1]}
}
# no_signer_process: succeeds only when pgrep reports no apa-signer process (exit 1).
no_signer_process() {
  local out rc
  out=$(pgrep -u apa-signer 2>&1) && rc=0 || rc=$?
  case $rc in
    1) print -r -- "no apa-signer process" ;;
    0) stop "apa-signer has a process: ${out//$'\n'/ }"; return 1 ;;
    *) stop "pgrep failed (rc=$rc): $out; cannot tell whether apa-signer runs"; return 1 ;;
  esac
}
# sock_absent [PREFIX...]: succeeds only when the socket path is confirmed absent.
sock_absent() {
  local out rc
  out=$("$@" /usr/bin/stat -f %HT "$SOCK" 2>&1) && rc=0 || rc=$?
  if (( rc == 0 )); then stop "$SOCK exists ($out)"; return 1
  elif [[ $out == *'No such file or directory'* ]]; then print -r -- "socket absent"
  else stop "cannot tell whether $SOCK exists (rc=$rc): $out"; return 1; fi
}
# pid_state PID: prints running or gone; an unreadable answer stops.
pid_state() {
  local out rc
  out=$(ps -o pid= -p "$1" 2>&1) && rc=0 || rc=$?
  out=${out//[[:space:]]/}
  if (( rc == 0 )) && [[ $out == "$1" ]]; then print running
  elif (( rc == 1 )) && [[ -z $out ]]; then print gone
  else stop "cannot tell whether $1 runs (rc=$rc): $out"; return 1; fi
}
EOF
  : >| "$RUN/ok.2"; print -r -- "OK 2  route=B  HEAD=$HEAD  sha256=$SUM  E=$E"
)
```
預期：`OK 2`、HEAD、hash 與證據目錄。記下 `RUN=…`，其他視窗會用到。

**進入錄製**（W-host）：
```zsh
[[ -e $RUN/ok.2 ]] && source "$RUN/vars" && script -q "$E/w-host.log" /usr/bin/env -i HOME="$RUN/home" USER="$USER" LOGNAME="$LOGNAME" TERM="$TERM" PATH=/usr/bin:/bin:/usr/sbin:/sbin RUN="$RUN" /bin/zsh -f
```
**錄製中**（W-host，第一件事）：
```zsh
source "$RUN/lib.zsh" && source "$RUN/vars" && print -r -- "recording W-host  RUN=$RUN  HEAD=$HEAD  route=$ROUTE"
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
    out=$(dscl . -read "$r" RecordName 2>&1) && rc=0 || rc=$?
    if (( rc == 0 )); then stop "$r already exists"
    elif [[ $out == *eDSRecordNotFound* ]]; then print -r -- "absent  $r  (rc=$rc)"
    else stop "dscl could not tell whether $r exists (rc=$rc): ${out%%$'\n'*}"; fi
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
  sock_absent sudo
  no_signer_process
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

**10/13 第一次操作的補充（B0）**：
- `nobody` 的 curl 回 7、HTTP 000，訊息只有無法連線、沒有權限字樣，所以 `expect_fs_block` 正確停在 STOP。
- 這時**保留這個 STOP**，不重跑、不放寬判斷，交 Codex 依兩件事判讀：服務當時是否可用（同一段的 Host `expect_ok`），以及有沒有明確的 errno。
- 這次是 Aaron 依 Codex 指示，以 `nobody` 補做一次 AF_UNIX 直接 connect，得到 errno 13（Permission denied）；只嘗試連線，不送 HTTP 或任何請求。之後再取得一次 Host health 200，記下補充說明，才完成 `OK 6.5`。
- 這不是原始區塊一次通過。檔案不存在、連線被拒、逾時、工具不存在或其他錯誤，都不算權限阻擋。helper 不改成「非零就通過」，也不加自動重試。
- 補充探測的命令是 Codex 當場提供的，本筆記沒有收錄成可執行區塊；若之後要收錄，先交 Codex 核對。

緊接在 `nobody` 之前的 `expect_ok` 證明 socket 當時活著。`nobody` 只靠退出碼不算數：必須同時沒有 HTTP 狀態、有權限錯誤字樣。curl 在這台機器上的實際字樣尚未觀察過（路徑 A 未執行）；若不同，`expect_fs_block` 會停下讓 Codex 判讀，不自行放寬。

**6.6 W-signer 觀察**（W-admin，驗收關卡；只有回答 `yes` 才通過）
```zsh
( setopt ERR_EXIT NO_UNSET PIPE_FAIL
  need 6.5
  OBS=''
  read -r "OBS?W-signer: exactly one new 'caller  CALLER_NOT_ALLOWED' line, no line for nobody, and no passphrase anywhere (yes/no): " || stop "no answer was read"
  print -r -- "W-signer observation (B0): [$OBS]"
  [[ $OBS == yes ]] || stop "the B0 observation did not pass: stop the Signer (6.7), do not go on to B1"
  mark 6.6
)
```
- 回答 `no`、空值、輸入中斷或其他回答都不會寫入 `ok.6.6`，7.1 起的 B1 都會被擋住。
- **若在任何地方看見密語**：回答 `no`，在 W-signer 按 Ctrl-C 停止；把本次密語視為已洩漏。不要把密語或含密語的輸出貼給 Codex 或 Claude Code，不自動清除證據，也不改密語重試。後續處置由 Aaron 決定。
**6.7 停止**（安全收尾，不看 6.4～6.6 是否通過）：在 W-signer 按 Ctrl-C。然後（W-host）：
```zsh
( setopt ERR_EXIT NO_UNSET PIPE_FAIL
  need 6.2
  if [[ -f $RUN/pid ]]; then
    source "$RUN/pid"
    for i in {1..10}; do st=$(pid_state "$PID"); [[ $st == gone ]] && break; sleep 1; done
    [[ $st == gone ]] || stop "the Signer (PID $PID) is still running"
  fi
  no_signer_process
  sock_absent
  mark 6.7
)
```
`ok.6.7` 只表示「已確認停止」，不表示 B0 驗收通過。B0 通過要看 6.4、6.5、6.6 的標記（第 9 節）。無法確認停止時（`pgrep`／`ps`／`stat` 回錯誤、程序仍在、socket 還在），不往下做，也不刪任何資料，交 Codex 判讀。

## 7 B1：一次性測試金鑰（D3、D4）

**7.1 建金鑰**（W-signer，不錄製；兩次提示輸入同一個**新的**測試密語，至少 12 字元，只用於這把金鑰）
```zsh
if [[ -e $RUN/ok.6.6 && -e $RUN/ok.6.7 ]]; then cd / && sudo -u apa-signer /bin/sh -c 'umask 077; exec /opt/apa-signer/bin/apa-signer keygen --dir /opt/apa-signer/data --wallet wallet-isolation-test'; else print -r -- "STOP: B0 has not passed (6.6) or has not been stopped (6.7)"; fi
```
預期：`apa-signer: wallet wallet-isolation-test address 0x…`（地址是公開資訊）。

**7.2 設定**（W-host；輸入上面印出的地址）
```zsh
read -r "ADDR?address printed by keygen: "
```
```zsh
( setopt ERR_EXIT NO_UNSET PIPE_FAIL
  need 6.6 6.7
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
  sock_absent sudo
  no_signer_process
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
**7.6**（W-admin；`expect_fs_block` 若像 B0 一樣停在 STOP，照第 6.5 節的補充處理：保留 STOP，交 Codex 判讀，不機械式重跑）
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
**7.6o W-signer 觀察**（W-admin，驗收關卡；只有 `yes` 通過，規則同 6.6）
```zsh
( setopt ERR_EXIT NO_UNSET PIPE_FAIL
  need 7.6
  OBS=''
  read -r "OBS?W-signer: one new 'caller  CALLER_NOT_ALLOWED' line, none for nobody, no passphrase anywhere (yes/no): " || stop "no answer was read"
  print -r -- "W-signer observation (B1): [$OBS]"
  [[ $OBS == yes ]] || stop "the B1 observation did not pass: stop the Signer (7.7); B1 has not passed"
  mark 7.6o
)
```
**7.7 停止**（安全收尾，不看 7.5～7.6o 是否通過）：W-signer 按 Ctrl-C，然後（W-host）：
```zsh
( setopt ERR_EXIT NO_UNSET PIPE_FAIL
  need 7.3
  if [[ -f $RUN/pid ]]; then
    source "$RUN/pid"
    for i in {1..10}; do st=$(pid_state "$PID"); [[ $st == gone ]] && break; sleep 1; done
    [[ $st == gone ]] || stop "the Signer (PID $PID) is still running"
  fi
  no_signer_process
  sock_absent
  mark 7.7
)
```
`ok.7.7` 只表示已確認停止；B1 通過要看 7.5、7.6、7.6o。
W-signer 最後執行 `sudo -k`，然後關閉視窗。

## 8 撤回測試資料（D5：保留帳號、群組與目錄）

**8.1 確認已停止並列清單**（W-admin）
```zsh
( setopt ERR_EXIT NO_UNSET PIPE_FAIL
  need 4.2; cd /
  no_signer_process
  sock_absent sudo
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
8.1 不看驗收是否通過（安全收尾），但要求這一刻確認 `apa-signer` 沒有程序、socket 不存在；`pgrep` 或 `stat` 回錯誤就停止，不刪任何資料。Aaron 看過清單，確定只有上面已知的項目，再做 8.2。

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

**10/13 這一次的操作差異**：訊息截斷，Aaron 把 8.2 拆成三段操作：
1. 重新核對清單未變；
2. 刪除具名項目；
3. 確認保留清單並保存證據。

結果與本區塊的預期相同（`OK 8.2`），但不是原始區塊一次執行。8.3 也由 Aaron 完成：先把 `probe-hashes` 複製到證據目錄，再清除本次暫存。紀錄見方案第 8.7 節。本筆記不為這次的拆段新增清理腳本。

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
| B0 通過 | 6.4、6.5、6.6 | `OK 6.4`、`OK 6.5`、`OK 6.6`（觀察 `yes`）；`OK 6.7` 只代表已停止 |
| B1 通過 | 7.5、7.6、7.6o | `OK 7.5`、`OK 7.6`、`OK 7.6o`（觀察 `yes`）；`OK 7.7` 只代表已停止 |
| Signer 以 `apa-signer` 前景執行並讀到 `/dev/tty` | 6.3、6.4 | W-signer 印出 serving；`signer_pid` 通過 |
| socket 擁有者、群組、權限 | 6.4、7.5 | `apa-signer:apa-signer-clients:660`，沒有 ACL |
| Host 呼叫 health | 6.4、7.5 | `expect_ok` |
| peer uid 拒絕 | 6.5、7.6 | `expect_403`，加上 6.6／7.6o 的觀察通過 |
| 金鑰在 `apa-signer` 下解鎖；Host 讀不到金鑰檔 | 7.4、7.5 | serving；`denied "read the test key"` |
| 撤回 | 8.1、8.2 | 沒有未知項目；剩餘等於保留清單 |

**未觀測範圍**（只能下有限結論）：
- 密語：可支持的結論是「這次觀測的 Signer 命令列只有 `serve --dir …`（`signer_pid` 逐字比對）；W-signer 的提示關閉回顯；錄製的視窗沒有輸入密語」。
- 環境變數：**未觀測**。本機 `ps(1)`：`-E`「does not reflect changes in the environment after process launch」，而且要列出完整環境才能檢查，本筆記不這麼做。程式碼只從 `/dev/tty` 讀密語、不寫進環境變數（原始碼依據，不是本次觀測）。
- 終端機捲動緩衝、其他管理者工具、記憶體：不在觀測範圍。觀測不到不等於不存在。
- 「Signer 不能連網」（D7）：未驗證。「Signer 不能改自己的設定」不成立於 OS 層（D6，已採 C1）。

**第二輪修訂對照**（第二版 `c7744be` → 本版；第二輪 O1～O4 是第一輪 O1～O3 之後的再補正，不表示第二版當時已驗收）：

| 項目 | 第二版的問題 | 本版 |
|---|---|---|
| 第二輪 O1 預演資格 | 第 1 節設定檔 `allow default`，沒有讀取隔離，也缺記憶體、程序數、磁碟與牆鐘時間上限，卻寫成「必須先完成」的前置 | 第 1 節改為路徑分流：路徑 A（Codex 受控預演）只有全部控制都能強制時才可啟動，目前**不可用、未執行**；路徑 B（Aaron 逐關卡、Codex 陪同判讀）由 Aaron 在第 2 節明確選擇，並寫進交接檔與紀錄；路徑 B 的結果不是預演 PASS |
| 第二輪 O2 自檢與收尾 | 寫入自檢對真實 repo 的固定路徑 `>|` 再刪除；失敗時不保證停止已啟動的程序；FIFO、`wait` 沒有期限；keygen 退出碼被吞 | 依第二輪 O1 不提供可執行的預演，整個預演腳本從現行入口移除，只留在 Git 歷史（`c7744be`），標明不可執行；不為未採用的路徑實作控制器 |
| 第二輪 O3 查詢失敗 ≠ 不存在 | 3.1 的 `dscl` 非零就印 `absent`；6.2、7.3、8.1 的 `pgrep` 錯誤會被當成沒有程序；6.7、7.7 的 `ps -p` 觀測失敗會被當成已停止；`[[ -e ]]` 看不到也當成不存在 | `dscl`：退出碼 0 是存在，有 `eDSRecordNotFound` 才算查無，其他一律停止；`pgrep` 以退出碼分「找到（0）／沒有（1）／錯誤（其他）」（`no_signer_process`、`signer_pid`）；`ps` 以 `pid_state` 分 running／gone／無法判讀（停止）；socket 以 `stat` 分存在／`No such file`／錯誤（`sock_absent`）。都立即保存退出碼，不交給 `ERR_EXIT` 猜 |
| 第二輪 O4 人工觀察 | 6.6、7.6 只印出回答，`no` 或空值也能往下走 | 6.6、7.6o 變成驗收關卡：只有 `yes` 寫入標記；B1（7.1、7.2）需要 `ok.6.6`。安全收尾 6.7、7.7、8.1 不看觀察或驗收標記，只要求當下確認已停止；`ok.6.7`／`ok.7.7` 只代表已停止。看見密語時：答 `no`、停止、視為已洩漏、不貼出、不自動清除或重試 |

**第一輪修訂對照**（第一版 `0ef6172` → 第二版 `c7744be`；其中「預演」相關各列已在第二輪隨預演腳本移出現行入口）：

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
1. 預演結果與修正：第二版改成受控預演腳本；第二輪因無法提供必要控制，改為路徑分流，路徑 A 未執行（第 1 節）。
2. 讀不到 `/dev/tty`：6.3 停止並交判讀；可能的選項（程式在 stdin 是終端機時改讀 stdin，或調整 sudo 的 pty 設定）都需另行核准，本輪不改。
3. `ps -E`：不足以觀測，已移除，列為未觀測（第 9 節）。
4. 證據與停止條件：錄製兩個視窗；所有關卡有 `STOP`／`UNEXPECTED`；清單複製到證據目錄。
5. 撤回範圍：8.1 未知就停止，8.2 只刪具名項目，保留 D5 決定的資源。

**仍需第一次執行（路徑 B）才能確認**（路徑 A 未執行，以下都沒有事前觀測）：
- 本機 `ps`、`pgrep`、`curl` 的實際輸出格式與退出碼；
- `dscl` 查無紀錄時是否含 `eDSRecordNotFound`（不是就會停止，交判讀）；
- 被檔案權限擋住時 curl 的訊息（影響 `expect_fs_block`；不同就會停止）；
- 跨使用者、sudo、`/dev/tty`、群組生效、socket 群組繼承（第 4～7 節）。

## 10 語法解析與靜態分支推演（本版提交前，Claude Code）

**語法解析**：
- 方法：本文件每個 ```` ```zsh ```` 區塊依序另存成檔案（43 塊），各跑一次 `/bin/zsh -f -n <檔案>`（zsh 5.8.1，只解析、不執行）。第 2 節區塊內以 here-document 寫出的 `lib.zsh`（74 行）在區塊解析時只被當成文字，所以另外抽出來單獨解析。本文件沒有其他 shell 的區塊；區塊裡沒有需要替換的佔位值（交接只讀 `RUN`）。
- 結果：43 塊與 `lib.zsh` 的退出碼都是 0，stderr 都是空的（在 Claude Code 工具沙箱外解析）。

| 區塊 | 章節 | 退出碼 | stderr |
|---|---|---|---|
| 01～06 | 2 選路徑、建立本次目錄、建置、進入錄製 | 0 | 無 |
| 07～12 | 3 管理視窗與查核 | 0 | 無 |
| 13～19 | 4 帳號、群組、目錄、群組生效 | 0 | 無 |
| 20～22 | 5 A 階段 | 0 | 無 |
| 23～31 | 6 B0（含 6.6 觀察關卡、6.7 安全收尾） | 0 | 無 |
| 32～40 | 7 B1（含 7.6o 觀察關卡、7.7 安全收尾） | 0 | 無 |
| 41～43 | 8 撤回 | 0 | 無 |
| `lib.zsh` | 第 2 節的 here-document | 0 | 無 |

上一版（`c7744be`）的 44 塊解析結果仍歸 Claude Code 當時的紀錄，不是 Codex 的獨立驗證。語法通過不代表參數合法、路徑存在或行為如預期。

**靜態分支推演**：依文字逐一推演，不是實測，也不是 PASS。

| 情況 | 能否往下 | 是否仍能安全停止 |
|---|---|---|
| 3.1 `dscl` 退出碼 0（紀錄存在） | 否，停止 | 不涉及（尚未改系統） |
| 3.1 `dscl` 非零且含 `eDSRecordNotFound` | 是，記為 `absent` | — |
| 3.1 `dscl` 非零但沒有 `eDSRecordNotFound`（查詢失敗或字樣不同） | 否，停止並保留錯誤與退出碼 | 不涉及 |
| `pgrep` 找到程序（0） | 6.2／7.3／8.1 停止；`signer_pid` 繼續核對唯一性與命令列 | 6.7／7.7 會停在「仍有程序」，不刪資料 |
| `pgrep` 沒有符合（1） | `no_signer_process` 通過；`signer_pid` 停止（沒有 Signer） | 是 |
| `pgrep` 其他退出碼（錯誤） | 否，停止 | 6.7／7.7／8.1 停止，不宣稱已停、不刪資料 |
| `ps -o pid= -p` 回 running／gone／無法判讀 | running：等待到上限仍在就停止；gone：繼續；無法判讀：停止 | 無法判讀時不宣稱已停 |
| socket `stat`：存在／`No such file`／其他錯誤 | 存在：停止；不存在：繼續；錯誤：停止 | 錯誤時不刪資料 |
| 6.6／7.6o 回答 `yes` | 寫入觀察標記；B1 需要 `ok.6.6` | 是（6.7／7.7 與觀察無關） |
| 回答 `no`、空值、其他文字 | 否；不寫標記，B1 被 `need 6.6` 與 7.1 的條件擋住 | 是：照常 Ctrl-C，6.7／7.7 只確認已停止 |
| `read` 失敗（輸入中斷） | 否（`stop "no answer was read"`） | 是 |
| 看見密語 | 否；答 `no`、停止、視為洩漏，不貼出 | 是 |
| Signer 啟動後失敗（6.4／7.5 任一處停止） | 否；後續驗收步驟的 `need` 擋住 | 是：6.7 只需要 `ok.6.2`、7.7 只需要 `ok.7.3`；沒有 `pid` 檔也以 `pgrep`、`stat` 確認 |
| 收尾失敗（仍有程序、socket 還在、查詢錯誤） | 否；不寫 `ok.6.7`／`ok.7.7`／`ok.8.1` | 交 Codex 判讀，不刪 DB、設定或金鑰 |
| 8.1 清單有未知項目 | 否，不寫 `ok.8.1`，8.2 不會執行 | 不刪任何東西 |

**Aaron 的下一步**：
1. 等 Codex 限定複核本版。
2. 開始時在 W-host 依第 2 節執行 `exec /bin/zsh -f`，在 `operating route` 提示輸入 `B`。這是路徑 B，不是預演；路徑 A 目前不可用，也沒有任何隱含的預演前置。
3. 建立本次目錄並建置，看到 `OK 2  route=B …`；之後每一步都以前一步的 `OK` 為入口。
4. 看到任一 `STOP` 或 `UNEXPECTED` 就停下，把該段輸出交 Codex。
