# P1-b OS 隔離驗收：指令筆記（給 Codex 操作與審閱）

> 2026-10-13，Claude Code 撰寫。依據 [commercial-p1b-os-isolation.md](commercial-p1b-os-isolation.md)（方案）第 2、3、5 節；Aaron 已核准 D1～D7（方案第 8 節）。程式基準：`f047ad6`（含 `apa-signer keygen`）之後的 commit，目前 HEAD 以 `git rev-parse HEAD` 為準。
> **重要：本筆記的命令沒有任何一條被執行或驗證過。**撰寫時沒有 sudo，也沒有試跑或語法檢查。已驗證的只有程式行為（`serve`、`keygen`、peer uid 與 403），而且都是同一個 uid 的測試，見 [commercial-p1b.md](commercial-p1b.md) 第 8、9 節與方案第 8.2 節。所以第一次執行時，參數或輸出可能和這裡寫的不同。

## 0 角色與規則

| 誰 | 做什麼 |
|---|---|
| Codex | 先做第 1 節的預演（不用 sudo、不改系統）；之後在每一步讀輸出、判讀、提出建議；不輸入、不接收、不保存任何密碼或密語 |
| Aaron | 在自己的終端機輸入 sudo 密碼與所有密語；執行第 2～6 節帶 sudo 的步驟（Codex 可以準備命令、看輸出，但 sudo 密碼與密語只由 Aaron 在鍵盤上輸入） |

規則：
- **停止條件**：任何一步的輸出與「預期」不同就停止。不自動修權限、不重試、不換名稱或路徑，把輸出交給 Codex 判讀，再由 Aaron 決定。
- **三個視窗**：W-admin 只做 sudo；W-signer 只前景執行 Signer，**不錄製**；W-host 是 Aaron 的一般帳號，不用 sudo。Host、demo、Claude CLI 不在 W-admin 或 W-signer 執行。
- **密語**：只在 W-signer 的提示出現時由 Aaron 輸入。不放在參數、環境變數、管線、檔案或錄製裡，不貼給 Codex 或 Claude Code。
- **不做**：不呼叫 `/v1/sign`、不連 RPC 或外部服務、不改全機防火牆、不用 A／B 展示金鑰、不接進展示流程。
- **證據**：W-admin 與 W-host 用 `script` 錄到同一個證據目錄（第 2.0 步），W-signer 不錄。

## 1 預演（Codex，不用 sudo、不改系統）

目的是在動系統前抓出參數錯誤或預期寫錯的地方。只用 Codex 自己的 uid，所有檔案放在新建的暫存目錄，結束後刪除。預演用的密語與金鑰只是丟棄式材料，之後不再使用。

| # | 做什麼 | 命令 | 判讀 |
|---|---|---|---|
| R1 | 建置 | `cd <repo>; git status --short; git rev-parse HEAD; go build -o "$TMPDIR/apa-signer-rehearsal" ./cmd/apa-signer` | 只有 `?? demo`；建置成功 |
| R2 | `ps`、`pgrep`、`curl` 的參數在這台 macOS 合法 | `ps -o pid,uid,gid,user,command -p $$`；`ps -E -o command -p $$ \| head -c 300`；`sleep 30 & P=$!; pgrep -u "$(id -un)" -f '^sleep 30'; kill "$P"`；`curl --version \| head -1` | 不能有「illegal option」或「unknown keyword」；`pgrep` 印出一個 PID；記下 `ps -E` 的輸出格式 |
| R3 | 本筆記的 shell 語法 | 把第 2～6 節的每個程式碼區塊另存成檔案，各跑一次 `zsh -n <檔>` | 沒有語法錯誤（`zsh -n` 只檢查語法，不執行） |
| R4 | 同一 uid 起一個 Signer，看 health 與 403 的實際輸出 | 見下方 R4 程式碼區塊 | health：`{"state":"unlocked"}`、`HTTP=200`；callers 不含自己時：`{"error":"CALLER_NOT_ALLOWED"}`、`HTTP=403`；連到不存在的 socket 的 curl 退出碼（記下，當第 4 步 `nobody` 那一行的參考） |
| R5 | keygen 的實際輸出 | 見下方 R5 程式碼區塊 | 印出 `apa-signer: wallet wallet-rehearsal address 0x…`；檔案 `-rw-------`；之後用該設定 `serve` 能啟動 |
| R6 | 清理 | 先 `echo "$R"` 確認它在 `$TMPDIR` 底下、是 R4 建的那個目錄，再 `rm -r -- "$R"`；`rm -f "$TMPDIR/apa-signer-rehearsal"` | 暫存目錄消失 |

R4（`serve` 一定要開 `/dev/tty`；Codex 的工具若沒有終端機，就用 `script` 提供一個 pty，再把一行**非秘密**的預演輸入從管線送進去。這只限預演）：
```sh
R=$(mktemp -d "$TMPDIR/apa-r.XXXX"); chmod 700 "$R"; mkdir -m 700 "$R/keys"; echo "R=$R"
S=$(mktemp -u /tmp/apa-r-sock.XXXX)    # socket 路徑要短（上限 104 bytes）
printf '{"version":1,"socket":"%s","callers":[%s],"max_window_seconds":600,"wallets":[],"assets":[],"delegations":[]}\n' "$S" "$(id -u)" > "$R/signer.json"; chmod 600 "$R/signer.json"
(printf 'rehearsal\n'; sleep 20) | script -q /dev/null "$TMPDIR/apa-signer-rehearsal" serve --dir "$R" &
sleep 3   # 只在預演用：等它印出 serving
curl -sS --unix-socket "$S" -w '\nHTTP=%{http_code}\n' http://signer/v1/health; echo "exit=$?"
curl -sS --unix-socket "$S.none" http://signer/v1/health; echo "exit=$?"
# 停掉這個 Signer：只比對這次預演的完整命令列（不依程序名稱廣泛終止）
pkill -u "$(id -un)" -f "apa-signer-rehearsal serve --dir $R"; wait
# 403：callers 改成一個不是自己的 uid 再啟動一次，重做第一個 curl
printf '{"version":1,"socket":"%s","callers":[%s],"max_window_seconds":600,"wallets":[],"assets":[],"delegations":[]}\n' "$S" "$(( $(id -u) + 1 ))" > "$R/signer.json"
(printf 'rehearsal\n'; sleep 20) | script -q /dev/null "$TMPDIR/apa-signer-rehearsal" serve --dir "$R" &
sleep 3
curl -sS --unix-socket "$S" -w '\nHTTP=%{http_code}\n' http://signer/v1/health; echo "exit=$?"
pkill -u "$(id -un)" -f "apa-signer-rehearsal serve --dir $R"; wait; ls -la "$S" 2>&1   # 正常結束時 socket 會被移除；kill 後若留下，記下來
```

R5（同樣用 `script` 與非秘密的預演密語）：
```sh
(printf 'rehearsal passphrase\nrehearsal passphrase\n') | script -q /dev/null "$TMPDIR/apa-signer-rehearsal" keygen --dir "$R" --wallet wallet-rehearsal
ls -le "$R/keys"
```

**預演無法回答的**：跨使用者的行為（sudo、`apa-signer`、`nobody`、群組、socket 的群組繼承、`/dev/tty` 經 sudo 的可讀性）。這些只能在第 2～5 節第一次執行時得知。

預演結束後，Codex 回報 R1～R6 的實際輸出，以及它建議修改本筆記哪些命令或預期。若需要修改，先改本筆記，再開始第 2 節。

## 2 建置、查核與證據目錄

**W-host**（Aaron）
```sh
cd /Users/mac/Documents/OpenSourceProjects/agent-payment-authority
E="$PWD/artifacts/p1b/os-isolation-$(date -u +%Y%m%dT%H%M%SZ)"; mkdir -p "$E"; echo "E=$E"
script -q "$E/w-host.log"
# 以下在 script 裡
git status --short; git rev-parse HEAD
B=$(mktemp -d); go build -o "$B/apa-signer" ./cmd/apa-signer && shasum -a 256 "$B/apa-signer"; echo "B=$B"
```
預期：只有 `?? demo`；印出 hash 與 `B=…`。

**W-admin**（Aaron；把 `E=…`、`B=…` 貼進來）
```sh
E=<貼上>; B=<貼上>
script -q "$E/w-admin.log"
# 以下在 script 裡
cd /
sw_vers; id -un; id -u
sudo -l | sed -n '1,25p'
dscl . -read /Users/apa-signer 2>&1 | head -1
dscl . -read /Groups/apa-signer 2>&1 | head -1
dscl . -read /Groups/apa-signer-clients 2>&1 | head -1
ls -ld /opt/apa-signer 2>&1
ls -led /opt
USED=$( (dscl . -list /Users UniqueID; dscl . -list /Groups PrimaryGroupID) | awk '{print $2}' | sort -n -u)
FREE=$(for i in $(seq 400 499); do echo "$USED" | grep -qx "$i" || echo "$i"; done | head -2)
SIGNER_ID=$(echo "$FREE" | sed -n 1p); CLIENTS_GID=$(echo "$FREE" | sed -n 2p); HOSTUSER=$(id -un)
echo "SIGNER_ID=$SIGNER_ID CLIENTS_GID=$CLIENTS_GID HOSTUSER=$HOSTUSER"
```
預期：
- 三個 `dscl -read` 都是「查不到」類的錯誤（實際字樣未驗證，記下）；
- `/opt/apa-signer` 不存在；`/opt` 是 `root wheel drwxr-xr-x`，沒有 ACL 行；
- `SIGNER_ID`、`CLIENTS_GID` 都是 400–499 的數字；
- `sudo -l` 的 Defaults 記下 `use_pty`、`timestamp_type`、`umask`。

停止：任一名稱或路徑已存在、兩個 ID 有空值、看到不認識的帳號。

## 3 帳號、群組、目錄（W-admin，Aaron 輸入 sudo 密碼）

```sh
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
sudo install -d -o root -g wheel -m 0755 /opt/apa-signer /opt/apa-signer/bin
sudo install -d -o apa-signer -g apa-signer -m 0700 /opt/apa-signer/data /opt/apa-signer/data/keys
sudo install -d -o apa-signer -g apa-signer-clients -m 0750 /opt/apa-signer/run
sudo install -o root -g wheel -m 0755 "$B/apa-signer" /opt/apa-signer/bin/apa-signer
ls -led /opt /opt/apa-signer /opt/apa-signer/bin /opt/apa-signer/bin/apa-signer /opt/apa-signer/run
sudo ls -led /opt/apa-signer/data /opt/apa-signer/data/keys
shasum -a 256 /opt/apa-signer/bin/apa-signer
id apa-signer
sudo -k
```
預期：
- 每個命令 exit 0；
- 擁有者、群組、權限同方案第 1.2 節：`root:wheel 0755`、`apa-signer:apa-signer 0700`、`apa-signer:apa-signer-clients 0750`；
- 沒有 ACL 行；hash 等於第 2 步。

停止：任一命令失敗（照方案第 5 節撤回已建立的部分）、權限或擁有者不符、出現 ACL。

**群組生效**：W-host 先 `exit` 離開 script，再關閉視窗；開**新的** W-host，重新 `E=<貼上>; script -q -a "$E/w-host.log"`，然後：
```sh
id -Gn | tr ' ' '\n' | grep -x apa-signer-clients
```
預期印出 `apa-signer-clients`。沒有的話登出再登入一次後再看；仍沒有就停止。

## 4 A 階段：公開測試檔

**W-admin**
```sh
cd /
sudo -u apa-signer /bin/sh -c 'umask 077; for f in /opt/apa-signer/data/probe.txt /opt/apa-signer/data/keys/probe.txt; do printf "%s\n" "apa-signer isolation probe, public test text" > "$f"; done'
sudo -u apa-signer /bin/sh -c 'id; for f in /opt/apa-signer/data/probe.txt /opt/apa-signer/data/keys/probe.txt; do test -f "$f" && test -r "$f" && test -w "$f" && echo "ok $f"; done'
```
預期：`id` 是 `apa-signer`，兩行 `ok`。這是前置條件；沒有兩行 `ok`，後面的「被拒」都不算數。

**W-host**
```sh
id
for F in /opt/apa-signer/data/probe.txt /opt/apa-signer/data/keys/probe.txt; do
  cat "$F"; echo "read exit=$?"; printf x >> "$F"; echo "append exit=$?"
  cp /etc/hosts "$F"; echo "replace exit=$?"; mv "$F" "$F.x"; echo "rename exit=$?"
done
ls /opt/apa-signer/data; echo "list exit=$?"
cp /usr/bin/true /opt/apa-signer/bin/apa-signer; echo "exit=$?"
touch /opt/apa-signer/bin/x; echo "exit=$?"
mv /opt/apa-signer/bin /opt/apa-signer/bin.x; echo "exit=$?"
touch /opt/apa-signer/run/x; echo "exit=$?"
shasum -a 256 /opt/apa-signer/bin/apa-signer
```
預期：每一個都是 Permission denied、exit 非 0；hash 不變。只要有一個成功就是 FAIL。

**W-admin**
```sh
cd /
sudo -u nobody /bin/sh -c 'id; cat /opt/apa-signer/data/probe.txt; echo "exit=$?"; ls /opt/apa-signer/run; echo "exit=$?"'
sudo -k
```
預期：`id` 是 `nobody`，兩者都 Permission denied。

## 5 B 階段：真正的 Signer

### 5.1 B0（不帶金鑰）

**W-host**
```sh
printf '{"version":1,"socket":"/opt/apa-signer/run/signer.sock","callers":[%s],"max_window_seconds":600,"wallets":[],"assets":[],"delegations":[]}\n' "$(id -u)" > /tmp/apa-signer-b0.json
cat /tmp/apa-signer-b0.json
```

**W-admin**
```sh
cd /
sudo -u apa-signer /bin/sh -c 'umask 077; cat > /opt/apa-signer/data/signer.json' < /tmp/apa-signer-b0.json
sudo -k
```

**W-signer**（不錄製；出現 `apa-signer passphrase:` 時隨便輸入一行）
```sh
cd /
sudo -u apa-signer /bin/sh -c 'umask 077; exec /opt/apa-signer/bin/apa-signer serve --dir /opt/apa-signer/data'
```
預期：`apa-signer: serving on /opt/apa-signer/run/signer.sock (reservation verification unavailable until P2: nothing is signed)`。
停止：`there is none`（讀不到終端機）或其他錯誤。這是方案標為待實測的項目，請 Codex 判讀並提出選項（方案 D3 備註），不要自行改程式或改輸入方式。

**W-host**
```sh
PID=$(pgrep -u apa-signer -f '^/opt/apa-signer/bin/apa-signer serve'); echo "PID=$PID"
ls -led /opt/apa-signer/run/signer.sock
ps -o pid,uid,gid,user,command -p "$PID"
curl -sS --unix-socket /opt/apa-signer/run/signer.sock -w '\nHTTP=%{http_code}\n' http://signer/v1/health; echo "exit=$?"
ls /opt/apa-signer/data; echo "exit=$?"
cat /opt/apa-signer/data/signer.json; echo "exit=$?"
cat /opt/apa-signer/data/signer.db; echo "exit=$?"
```
預期：
- 恰好一個 PID；socket 是 `srw-rw----`、`apa-signer apa-signer-clients`，沒有 ACL；
- 程序的使用者是 `apa-signer`，命令列只有 `serve --dir …`；
- `{"state":"unlocked"}`、`HTTP=200`；
- 三個讀取都 Permission denied。

**W-admin**
```sh
cd /
PID=$(pgrep -u apa-signer -f '^/opt/apa-signer/bin/apa-signer serve'); echo "PID=$PID"
sudo -u apa-signer curl -sS --unix-socket /opt/apa-signer/run/signer.sock -w '\nHTTP=%{http_code}\n' http://signer/v1/health; echo "exit=$?"
sudo -u nobody curl -sS --unix-socket /opt/apa-signer/run/signer.sock http://signer/v1/health; echo "exit=$?"
ps -o command -p "$PID"
sudo ps -E -o command -p "$PID"
sudo -k
```
預期：
- `apa-signer`：`{"error":"CALLER_NOT_ALLOWED"}`、`HTTP=403`，W-signer 多一行 `caller  CALLER_NOT_ALLOWED`。這是**已連線、由 peer uid 拒絕**。
- `nobody`：連線失敗（Permission denied），沒有 HTTP 回覆，W-signer 沒有新行。這是**檔案權限擋住**；退出碼以 R4 記下的為參考。
- 參數與環境變數裡沒有密語。看到密語就立即停止 Signer，並把該密語視為已洩漏。

**W-signer**：Ctrl-C。**W-host**：`ls -la /opt/apa-signer/run`，預期沒有 `signer.sock`。

### 5.2 B1（一次性測試金鑰）

**W-signer**（不錄製；兩次提示輸入同一個新的測試密語，至少 12 字元，只用於這把金鑰）
```sh
cd /
sudo -u apa-signer /bin/sh -c 'umask 077; exec /opt/apa-signer/bin/apa-signer keygen --dir /opt/apa-signer/data --wallet wallet-isolation-test'
```
預期：`apa-signer: wallet wallet-isolation-test address 0x…`。

**W-host**（`ADDR` 換成上面的地址）
```sh
ADDR=0x...
printf '{"version":1,"socket":"/opt/apa-signer/run/signer.sock","callers":[%s],"max_window_seconds":600,"wallets":[{"wallet_id":"wallet-isolation-test","consumer_id":"consumer-isolation-test","chain_id":"84532","address":"%s"}],"assets":[],"delegations":[]}\n' "$(id -u)" "$ADDR" > /tmp/apa-signer-b1.json
cat /tmp/apa-signer-b1.json
```

**W-admin**
```sh
cd /
sudo -u apa-signer /bin/sh -c 'umask 077; cat > /opt/apa-signer/data/signer.json' < /tmp/apa-signer-b1.json
sudo ls -le /opt/apa-signer/data/keys
sudo -k
```
預期：`wallet-isolation-test.key` 是 `-rw-------`，擁有者 `apa-signer`。

**W-signer**：再跑一次 5.1 的 `serve`，輸入**測試金鑰的密語**，預期出現 serving。這證明金鑰在 `apa-signer` 下解鎖。設定沒有任何委託，所以不可能簽出授權。

然後重做 5.1 W-host 與 W-admin 的檢查。W-host 多加：
```sh
cat /opt/apa-signer/data/keys/wallet-isolation-test.key; echo "exit=$?"
```
預期 Permission denied。最後在 W-signer 按 Ctrl-C。

## 6 撤回測試資料（D5：保留帳號、群組與目錄）

**W-admin**
```sh
cd /
sudo ls -lea /opt/apa-signer/data /opt/apa-signer/data/keys
sudo rm /opt/apa-signer/data/keys/wallet-isolation-test.key /opt/apa-signer/data/keys/probe.txt /opt/apa-signer/data/probe.txt
sudo rm /opt/apa-signer/data/signer.json /opt/apa-signer/data/signer.db
sudo ls -lea /opt/apa-signer/data /opt/apa-signer/data/keys
sudo -k
rm /tmp/apa-signer-b0.json /tmp/apa-signer-b1.json
exit
```
- 第一個 `ls` 出現清單以外的檔案就停止。若有 `signer.db-wal` 或 `signer.db-shm`，各自用 `sudo rm <完整路徑>` 指名刪除。
- 第二個 `ls` 應該是空的。
- 最後的 `exit` 結束 script。W-host 也 `exit` 結束 script。

## 7 判讀表（Codex 填）

| 條件 | 步驟 | PASS 規則 | 結果 | 證據行 |
|---|---|---|---|---|
| 帳號、群組、目錄如方案 | 3 | 擁有者、群組、權限、沒有 ACL、hash 相同 | | |
| 群組生效 | 3 | 新視窗 `id -Gn` 含 `apa-signer-clients` | | |
| Host 讀不到、改不了私有資料與執行檔 | 4、5.1、5.2 | 前置的兩行 `ok` 存在，且 Host 的每一項都 Permission denied | | |
| `nobody` 被檔案權限擋住 | 4、5.1 | Permission denied；沒有 HTTP 回覆 | | |
| Signer 以 `apa-signer` 前景執行並讀到 `/dev/tty` | 5.1 | serving；PID 的使用者是 `apa-signer` | | |
| socket 擁有者、群組、權限 | 5.1 | `apa-signer apa-signer-clients`、`0660`、沒有 ACL | | |
| Host 呼叫 health | 5.1 | HTTP 200 `unlocked` | | |
| peer uid 拒絕 | 5.1 | `apa-signer` 得到 HTTP 403 `CALLER_NOT_ALLOWED` 與 log 行 | | |
| 密語不在參數、環境、輸出 | 5.1、5.2 | 都沒有 | | |
| 金鑰在 `apa-signer` 下解鎖、Host 讀不到 | 5.2 | serving；金鑰檔 Permission denied | | |
| 撤回 | 6 | 只刪清單內的檔案；目錄為空 | | |

不在本驗收範圍、維持未驗證：「Signer 不能連網」（D7）；「Signer 不能改自己的設定」不成立於 OS 層（D6，已採 C1）。

## 8 請 Codex 給建議的地方

1. 第 1 節預演的結果，以及本筆記命令或預期需要修改的地方。
2. 第 5.1 節若讀不到 `/dev/tty`，有哪些選項、各自需要改什麼。
3. `sudo ps -E` 是否足以觀測另一個使用者程序的環境變數，有沒有更好的單一程序觀測方式。
4. 證據（`script` 錄下的 W-admin、W-host）是否足以支持第 7 節的判讀；有沒有遺漏的停止條件。
5. 撤回步驟是否只動到本次建立的東西。
