# P1-b：OS 隔離部署最小方案

> 2026-10-13，Claude Code。依 vault〈Claude-Code-P1-bOS隔離部署最小方案派工-76c3673-20261013〉（Aaron 核准撰寫本方案）。分支 `commercial-p1b`，基準 `76c3673`；產品與驗證基準 `07b16fd`、`935de2f`（R1～R3 限定技術複核已通過，本方案不重開）。
> **狀態：P1-b 既有實作缺口已限定複核；A／B0／B1 與測試資料清理完成，限定結案紀錄待最後核對；正式付款未啟用。**（第 8.7 節；P1-b 限定結案候選見 [commercial-p1b.md](commercial-p1b.md) 第 10 節）
> 以下為 `b7cc85d` 時的狀態，保留原文：keygen 通過 B1 一次性未注資測試金鑰用途的限定技術複核；A／B0 由 Aaron 依 Route B 操作並完成安全停止，Codex 依回報判讀。B1 金鑰隔離仍待 Aaron 操作；P1-b 未結案，正式付款未啟用。（第 8.5、8.6 節）
> 以下為 `8fb5f8c` 時的狀態，保留原文：Aaron 已核准 D1～D7（10/13，第 8 節）；D4 的 keygen 工具與 D6 的設計措辭已完成；OS 隔離驗收指令第二輪補正完成，待 Codex 限定複核；預演與跨使用者驗收未完成，正式付款未啟用。（第 8.3 節）
> 以下為 `62b5037` 時的狀態，保留原文：Aaron 已核准 D1～D7；D4 的 keygen 工具與 D6 的設計措辭已完成；D1～D3、D5 待 Aaron 親自執行；尚未部署，正式付款未啟用。
> 以下為方案交付時（`27b6e96`）的狀態，保留原文：方案完成，待 Codex review 與 Aaron 執行核准；尚未部署，正式付款未啟用。本文件的所有命令都**待核准後執行**：撰寫本方案時沒有試跑任何命令（包括 dry-run、語法檢查或測試替身），沒有 sudo，沒有建立使用者、群組、目錄或金鑰，沒有改權限。
> 撰寫時只做了：讀本 repo 的程式與文件、讀本機手冊（`unix(4)`、`sudoers(5)`、`dseditgroup(8)`）、讀 `/opt`、`/usr/local`、`/usr/local/bin`、`/var/db`、`/private/var/run` 五個目錄的擁有者與權限（`ls -ld`）。沒有列舉帳號、讀登入資料或探測服務。

目的：補上 [commercial-p1b.md](commercial-p1b.md) 第 6 節尚未驗收的 macOS 跨使用者隔離，只涵蓋設計第 3.1 節前兩個保證（付款私鑰不可讀、服務呼叫者可識別）。第三個保證（這筆付款確實被授權）已由程式與測試涵蓋，本方案不重新證明。

## 1 推薦配置

### 1.1 身分

| 身分 | 是什麼 | 用途 | 執行時查核 |
|---|---|---|---|
| Host 使用者 | Aaron 的一般登入帳號（目前 `mac`，本機管理員群組成員） | 執行 Host、demo 與 Agent（Claude CLI、bridge）；**Host 與 Agent 同一個 uid，OS 分不出兩者** | `id -u`、`id -Gn` |
| `apa-signer` | 新建，非管理員、不能登入、沒有 shell、home `/var/empty`、隱藏帳號 | 只執行 Signer；擁有私有資料 | uid 與 gid 在執行時選定，不在本文寫死 |
| `apa-signer`（群組） | 新建，`apa-signer` 的主要群組 | 私有資料的群組 | 同上 |
| `apa-signer-clients`（群組） | 新建，成員只有 Host 使用者 | 讓 Host 使用者能進 socket 目錄、連 socket | 加入後開新終端機確認 `id -Gn` |
| 未授權一般 uid | 既有的 `nobody`（不新增帳號） | 驗收「檔案權限擋住」 | `id nobody` |
| 管理者 | Aaron 用 sudo | 建帳號、放置檔案、以其他身分啟動程序 | **不在防禦範圍**；管理者提升權限後看得到的東西，不代表一般 Host 讀得到 |

名稱或路徑已存在就停止（第 5 節）：不覆寫、不沿用不明帳號或資料。

### 1.2 檔案與目錄

全部放在新建的 `/opt/apa-signer` 之下，好處是擁有者單純、撤回範圍明確。

| 路徑 | 擁有者:群組 | 權限 | 用途 | 誰能做什麼 |
|---|---|---|---|---|
| `/opt` | `root:wheel`（既有） | `0755` | 上層 | 一般使用者只能進入 |
| `/opt/apa-signer` | `root:wheel` | `0755` | 樹根 | 同上；Host 使用者不能新增、改名、刪除其中項目 |
| `/opt/apa-signer/bin` | `root:wheel` | `0755` | 執行檔目錄 | 同上 |
| `/opt/apa-signer/bin/apa-signer` | `root:wheel` | `0755` | Signer 執行檔（從已 review 的 commit 建置） | 所有人可執行；只有 root 能替換 |
| `/opt/apa-signer/data` | `apa-signer:apa-signer` | `0700` | `serve --dir` 的私有目錄（程式要求擁有者是執行者、`0700`） | 只有 `apa-signer` 能進入 |
| `data/signer.json` | `apa-signer:apa-signer` | `0600` | 可信設定 | 只有 `apa-signer`；**`apa-signer` 自己能改**（第 1.4 節） |
| `data/keys` | `apa-signer:apa-signer` | `0700` | 金鑰目錄（程式要求同上） | 只有 `apa-signer` |
| `data/keys/<wallet_id>.key` | `apa-signer:apa-signer` | `0600` | 加密金鑰檔（程式要求一般檔且 `0600` 或更嚴） | 只有 `apa-signer` |
| `data/signer.db`（與 `-wal`、`-shm`） | `apa-signer:apa-signer` | 依 umask；啟動時設 `umask 077` 會是 `0600` | Signer DB | 上層 `0700` 已擋住其他人 |
| `/opt/apa-signer/run` | `apa-signer:apa-signer-clients` | `0750` | socket 目錄，與私有資料分開 | 群組成員（Host 使用者）可進入，不能新增或刪除；其他人不能進入 |
| `run/signer.sock` | `apa-signer:apa-signer-clients` | `0660` | Unix socket | Host 使用者可讀寫（能 `connect`）；`nobody` 連路徑都進不去 |

**不放 `/usr/local/bin`**：本機的 `/usr/local/bin` 是 `mac:admin 0775`（Homebrew 的常見配置），Host 使用者能替換其中的檔案，等於 Host 或 Agent 能在下次啟動前換掉 Signer。**不放 `/var/run`**：本機 `/private/var/run` 是 `root:daemon 0775`，而且是否在開機時被清空沒有在本機確認。

### 1.3 對各項要求的回答

- **Host／Agent 能連 socket，但不能讀或改私有資料**：`run/` 給 `apa-signer-clients` 進入（`0750`），socket `0660`；`data/` 是 `apa-signer` 的 `0700`，Host 使用者連列出內容都不行。兩個目錄分開，所以為了連線開放的只有 `run/`。
- **socket 的擁有者、群組與權限怎麼成立**：
  - 程式（`cmd/apa-signer/main.go` `run`）在 `net.Listen` 建立 socket 後才 `chmod 0660`。擁有者是執行者 `apa-signer`。
  - 群組：macOS 依 BSD 規則，新檔的群組取自上層目錄，所以應是 `apa-signer-clients`（**待實測**，第 2 節 B4 驗）。若不是，停止；可選的修正（如啟動後由管理者 `chgrp`，或改程式在建立後設定群組）都另行核准。
  - `Listen` 到 `chmod` 之間，socket 的權限由 umask 決定（`umask 077` 時為 `0700`），群組成員在這段時間連不上；`run/` 本身 `0750`，其他人一直進不去。
  - `unix(4)`（本機手冊）：`connect(2)` 的目標必須可寫，一般的檔案系統存取控制適用。所以 socket 權限是實際的門檻，不是裝飾。
  - Host 使用者還需要能進入每一層上層目錄：`/opt`、`/opt/apa-signer` 都是 `0755`，`run/` 靠群組。
  - **ACL**：每一層都要用 `ls -led` 確認沒有 ACL；有任何 ACL 就停止，不自行移除。
- **Host 不能替換執行檔或放寬可信設定**：執行檔與它的上層目錄全部 `root:wheel 0755`；可信設定在 `apa-signer` 的 `0700` 目錄裡。Host 使用者雖是管理員群組成員，沒有 sudo 也不能寫這些位置。
- **「Signer 不能修改自己的設定」**：見第 1.4 節，目前**不成立於 OS 層**。

### 1.4 與設計承諾的差異：Signer 能改自己的設定

設計第 3.2 節寫 Signer「不能修改自己的設定」，第 3.5 節寫設定「`apa-signer` 擁有，只有管理者能改」。兩句互相矛盾，現行程式也做不到：`assemble` 從 `--dir` 讀 `signer.json`，而 `private()` 要求這個目錄由執行者擁有、`0700`，所以 `apa-signer` 對設定檔與它的目錄都有寫入權（即使檔案改成 root 擁有，`apa-signer` 仍能在自己的目錄裡改名或刪除再重建）。目前成立的只有：Host／Agent 不能改設定；Signer 的程式沒有任何寫設定的路徑。

最小選項，交 Codex review 與 Aaron 決定，本輪不改程式、不改承諾：

| 選項 | 做法 | 影響 |
|---|---|---|
| C1（推薦先採） | 不改程式；把承諾改寫成「Host／Agent 不能改；Signer 程式沒有寫入路徑；`apa-signer` 被攻破時設定可被改」 | 與 T3 同一個前提：`apa-signer` 被攻破時金鑰已經暴露，改設定不會擴大損失 |
| C2 | 改程式：`serve --config PATH`，設定放 `/opt/apa-signer/etc/signer.json`（`root:apa-signer 0640`，目錄 `root:wheel 0755`），啟動時檢查設定檔與每層上層目錄都不是 `apa-signer` 可寫 | 需要另行核准實作與測試；部署多一個目錄 |

### 1.5 不改變的邊界

- Agent 與 Host 同一個 uid：OS 分不出兩者，本方案**不**宣稱「Host 能連、Agent 不能連」（設計第 3.4 節）。
- 已接受的同 uid 常設委託風險不變。本方案不防本機管理員，也不是完整安全稽核。
- **網路**：設計寫 Signer「不能連網」。目前沒有任何 OS 機制強制這一點：程式沒有網路用戶端，但 `apa-signer` uid 本身可以連網。本方案把它標為**未驗證**，列為待決（第 6 節 D7）；不自行修改全機防火牆。
- **sudo 的時間戳**：本機 `sudoers(5)` 預設每個終端機各自記錄，5 分鐘內同一終端機再用 sudo 不必輸入密碼（本機設定可能不同，第 2 節 P0 查核）。同一終端機裡的其他程式也可能沿用。所以所有 sudo 步驟都在**專用的管理終端機視窗**執行，結束時 `sudo -k`；Host、demo、Claude CLI 永遠不在這個視窗或 Signer 視窗裡執行。

## 2 操作清單

除非另外註明，每一步都由 **Aaron 在自己的終端機**親自執行。有三個視窗：
- **W-admin**：管理終端機，只做 sudo 步驟；
- **W-signer**：前景執行 Signer；
- **W-host**：Host 使用者的一般視窗。

密語只在 W-signer 的提示出現時由 Aaron 直接輸入，不放在參數、環境變數、管線、檔案或任何錄製裡，也不貼給 Claude Code 或 Codex。W-signer 不要用 `script` 或終端機錄影。

證據：W-admin 與 W-host 的輸出由 Aaron 存到 `artifacts/p1b/os-isolation-<UTC 時間>/`（git 忽略），只含命令、退出碼、`id`、`ls -led` 與固定訊息。

### 2.0 前置（P）

| # | 執行者／權限 | 操作（**待核准後執行**） | 預期 | 停止條件 | 留下 |
|---|---|---|---|---|---|
| P0 | Aaron，W-admin | `sudo -l` 看 Defaults（只看 `use_pty`、`timestamp_type`、`timestamp_timeout`、`umask`、`env_reset`）；`sw_vers`；`id -u`；`id -Gn` | 記下實際值 | 輸出看不懂或與第 1.5 節假設不同 → 停止，交 review | 文字紀錄 |
| P1 | Aaron，W-host | 從已 review 的 commit 建置：`git -C <repo> rev-parse HEAD`；`go build -o <暫存目錄>/apa-signer ./cmd/apa-signer`；`shasum -a 256 <暫存目錄>/apa-signer` | 得到執行檔與 sha256 | commit 不是核准的那一個 → 停止 | hash |
| P2 | Aaron，W-admin | 確認名稱與路徑都不存在：`dscl . -read /Users/apa-signer`、`dscl . -read /Groups/apa-signer`、`dscl . -read /Groups/apa-signer-clients`（三者都應回「not found」類錯誤）；`ls -ld /opt/apa-signer`（應不存在）；`ls -led /opt`（應為 `root:wheel 0755`、沒有 ACL） | 都不存在 | 任一已存在、或 `/opt` 不符 → 停止 | 文字紀錄 |
| P3 | Aaron，W-admin | 選 uid 與 gid：`dscl . -list /Users UniqueID` 與 `dscl . -list /Groups PrimaryGroupID`，在 400–499 之間（macOS 不在登入畫面顯示 500 以下）各挑一個**兩邊都未使用**的值；`apa-signer` 群組與使用者用同一個值，`apa-signer-clients` 另一個值 | 兩個未使用的值 | 找不到、或看到不認識的相近帳號 → 停止 | 選定的值 |

### 2.1 帳號與目錄（S）

| # | 執行者／權限 | 操作（**待核准後執行**；`<UID>`、`<GID_C>`、`<HOSTUSER>` 用 P0、P3 的值） | 預期 | 停止條件 | 留下 |
|---|---|---|---|---|---|
| S1 | Aaron，sudo | `sudo dseditgroup -o create -i <UID> -r "APA Signer" apa-signer`；`sudo dseditgroup -o create -i <GID_C> -r "APA Signer clients" apa-signer-clients` | 兩個群組 | 任一命令非 0 → 停止 | 兩個群組 |
| S2 | Aaron，sudo | `sudo dscl . -create /Users/apa-signer`，再分別設定 `UniqueID <UID>`、`PrimaryGroupID <UID>`、`UserShell /usr/bin/false`、`NFSHomeDirectory /var/empty`、`RealName "APA Signer"`、`IsHidden 1`、`Password '*'` | 使用者存在、不能登入 | 任一命令非 0 → 停止，照第 5 節撤回已建立的部分 | 使用者 |
| S3 | Aaron，sudo | `sudo dseditgroup -o edit -a <HOSTUSER> -t user apa-signer-clients` | Host 使用者成為成員 | 非 0 → 停止 | 群組成員變更 |
| S4 | Aaron，**新開**的 W-host | `id -Gn` 含 `apa-signer-clients`；`id apa-signer` 顯示選定的 uid／gid | 生效 | 新視窗仍沒有該群組 → 登出再登入一次後再看；仍沒有 → 停止（群組生效方式**待實測**） | — |
| S5 | Aaron，sudo | `sudo install -d -o root -g wheel -m 0755 /opt/apa-signer /opt/apa-signer/bin`；`sudo install -d -o apa-signer -g apa-signer -m 0700 /opt/apa-signer/data /opt/apa-signer/data/keys`；`sudo install -d -o apa-signer -g apa-signer-clients -m 0750 /opt/apa-signer/run`；`sudo install -o root -g wheel -m 0755 <暫存目錄>/apa-signer /opt/apa-signer/bin/apa-signer` | 目錄與執行檔就位 | 任一非 0 → 停止 | 目錄、執行檔 |
| S6 | Aaron，W-admin | `ls -led /opt /opt/apa-signer /opt/apa-signer/bin /opt/apa-signer/bin/apa-signer /opt/apa-signer/run`；`sudo ls -led /opt/apa-signer/data /opt/apa-signer/data/keys`；`shasum -a 256 /opt/apa-signer/bin/apa-signer` | 擁有者、群組、權限同第 1.2 節；沒有 ACL；hash 等於 P1 | 任何不符或有 ACL → 停止，不自行修權限 | 文字紀錄 |
| S7 | Aaron，W-admin | `sudo -k` | 清除時間戳 | — | — |

### 2.2 A 階段：非敏感檔案的權限（A）

只用公開的測試文字，**不產生付款金鑰**。

| # | 執行者／權限 | 操作（**待核准後執行**） | 預期 |
|---|---|---|---|
| A1 | Aaron 以 sudo 切成 `apa-signer` | `sudo -u apa-signer /bin/sh -c 'umask 077; printf "%s\n" "apa-signer isolation probe, public test text" > /opt/apa-signer/data/probe.txt; printf "%s\n" "apa-signer isolation probe, public test text" > /opt/apa-signer/data/keys/probe.txt'` | 兩個檔案建立，exit 0 |
| A2 | 同上（前置：檔案存在而且 Signer 可讀） | `sudo -u apa-signer /bin/sh -c 'id; for f in /opt/apa-signer/data/probe.txt /opt/apa-signer/data/keys/probe.txt; do test -f "$f" && test -r "$f" && test -w "$f" && echo "ok $f"; done'` | `apa-signer` 的 uid／gid，兩行 `ok` |
| A3 | Aaron，W-host（Host 使用者，**不用 sudo**） | `id`；對兩個檔案各試：讀（`cat F`）、附加（`printf x >> F`）、替換（`cp /etc/hosts F`）、改名（`mv F F.x`）、列目錄（`ls /opt/apa-signer/data`）；每個命令後 `echo "exit=$?"` | 每一個都 exit 非 0，錯誤是「Permission denied」 |
| A4 | Aaron，W-host | 對執行檔與它的目錄：`cp /usr/bin/true /opt/apa-signer/bin/apa-signer`、`touch /opt/apa-signer/bin/x`、`mv /opt/apa-signer/bin /opt/apa-signer/bin.x`、`touch /opt/apa-signer/run/x`；每個後 `echo "exit=$?"` | 都被拒；再做一次 S6 的 `shasum`，hash 不變 |
| A5 | Aaron 以 sudo 切成 `nobody` | `sudo -u nobody /bin/sh -c 'id; cat /opt/apa-signer/data/probe.txt; echo "exit=$?"; ls /opt/apa-signer/run; echo "exit=$?"'` | `nobody` 的 uid；兩者都「Permission denied」、exit 非 0 |
| A6 | Aaron，W-admin | `sudo -k` | — |

判讀：只有 A2 先通過（檔案存在且 Signer 可用），A3～A5 的拒絕才算數；「No such file」、命令沒執行、路徑打錯都不算拒絕成功。A 階段只證明這些路徑的檔案權限，不代表 Signer 的 peer uid、解鎖、socket 服務或付款授權已驗收。

### 2.3 B 階段：真正 Signer 的跨使用者 IPC（B）

**啟動條件（核對現行程式）**：
- `serve` 一定先開 `/dev/tty` 讀密語，再讀設定；
- 設定裡的每個錢包都必須有能用該密語解密的金鑰檔；
- `ParseConfig` 允許 `wallets` 為空：這時不需要任何金鑰檔，但仍會提示並讀一行密語（不使用）。

因此有兩種做法，都另行核准（第 6 節 D3）：

- **B0（不需金鑰，推薦先做）**：設定只有 `socket`、`callers` 與空的 `wallets`／`assets`／`delegations`。證明 `apa-signer` 能從 Aaron 的終端機前景啟動並讀 `/dev/tty`、socket 的擁有者／群組／權限、跨使用者 peer uid 判定、私有目錄不可讀。**不**證明金鑰檔在真正 Signer 下的解鎖。
- **B1（一次性未注資測試金鑰）**：另需一個建立金鑰檔的工具（目前程式沒有，第 6 節 D4），以 `apa-signer` 身分在 `data/keys/` 建立一把只用於本驗收的金鑰（不是 A／B 展示金鑰，不注資），設定一個測試錢包（不設委託，所以 sign 一定被拒）。多證明：金鑰檔在 `apa-signer` 下解密成功、Host 使用者讀不到它。

兩者都不呼叫 sign、不產生可用授權、不連 RPC 或外部服務；正式組裝的預留檢查保持不可用（`RESERVATION_UNAVAILABLE`），沒有任何繞過開關。

| # | 執行者／權限 | 操作（**待核准後執行**） | 預期 | 停止條件 |
|---|---|---|---|---|
| B0k | （只在 B1 做法）Aaron，W-signer | `cd /`；`sudo -u apa-signer /bin/sh -c 'umask 077; exec /opt/apa-signer/bin/apa-signer keygen --dir /opt/apa-signer/data --wallet wallet-isolation-test'`；兩次提示時由 Aaron 輸入同一個新的測試密語（至少 12 字元，只用於這把測試金鑰） | 印出「apa-signer: wallet wallet-isolation-test address 0x…」；`data/keys/wallet-isolation-test.key` 為 `0600` | 讀不到 `/dev/tty`、檔案已存在、或兩次不同 → 停止，不重試成其他名稱 |
| B1 | Aaron，W-admin | 寫設定（不是秘密）：先在 W-host 準備 `signer.json`（`socket` = `/opt/apa-signer/run/signer.sock`、`callers` = `[<Host 使用者的 uid>]`、`max_window_seconds` = 600；B0：`wallets`、`assets`、`delegations` 都是空陣列；B1：`wallets` 只有 `{wallet_id: wallet-isolation-test, consumer_id: consumer-isolation-test, chain_id: "84532", address: <B0k 印出的地址>}`，`assets`、`delegations` 空陣列，所以沒有任何委託、sign 一定被拒），再 `sudo -u apa-signer /bin/sh -c 'umask 077; cat > /opt/apa-signer/data/signer.json' < <該檔>` | 檔案 `0600`，`apa-signer` 擁有 | 非 0 → 停止 |
| B2 | Aaron，W-signer | `cd /`；`sudo -u apa-signer /bin/sh -c 'umask 077; exec /opt/apa-signer/bin/apa-signer serve --dir /opt/apa-signer/data'`；出現「apa-signer passphrase:」時由 Aaron 直接輸入（B0：任意一行；B1：測試金鑰的密語） | 印出「serving on /opt/apa-signer/run/signer.sock (reservation verification unavailable until P2: nothing is signed)」 | 「there is none」（讀不到 `/dev/tty`）、權限錯誤、socket 已存在 → 停止（第 5 節）。**以「不能登入、沒有 shell」的帳號經 sudo 前景啟動並讀 `/dev/tty`，是待實測項目**：sudo 直接執行指定的命令，不使用帳號的 shell；`use_pty` 若開啟，終端機會是 sudo 另配的 pty。讀不到時不改程式、不改用其他輸入方式，交 review（可能的選項見第 6 節 D3 備註） |
| B3 | Aaron，W-host | `pgrep -u apa-signer -f '^/opt/apa-signer/bin/apa-signer serve'` | 恰好一個 PID | 0 或多個 → 停止 |
| B4 | Aaron，W-host | `ls -led /opt/apa-signer/run/signer.sock`；`ps -o pid,user,group,command -p <PID>` | socket `srw-rw----`、`apa-signer apa-signer-clients`、沒有 ACL；程序的使用者是 `apa-signer`，命令列只有 `serve --dir …` | 群組不是 `apa-signer-clients` 或權限不同 → 停止 |
| B5 | Aaron，W-host（Host 使用者） | `curl -sS --unix-socket /opt/apa-signer/run/signer.sock -w '\nHTTP=%{http_code}\n' http://signer/v1/health; echo "exit=$?"` | `{"state":"unlocked"}`、`HTTP=200`、exit 0 | 其他結果 → 停止 |
| B6 | Aaron 以 sudo 切成 `apa-signer`（**已連線、由 peer uid 拒絕**） | `sudo -u apa-signer curl -sS --unix-socket /opt/apa-signer/run/signer.sock -w '\nHTTP=%{http_code}\n' http://signer/v1/health; echo "exit=$?"` | `{"error":"CALLER_NOT_ALLOWED"}`、`HTTP=403`、exit 0；W-signer 的 log 有一行「caller  CALLER_NOT_ALLOWED」 | 不是 403 → 停止 |
| B7 | Aaron 以 sudo 切成 `nobody`（**檔案權限擋住**） | `sudo -u nobody curl -sS --unix-socket /opt/apa-signer/run/signer.sock http://signer/v1/health; echo "exit=$?"` | curl 回連線失敗（`Permission denied`），exit 7，沒有 HTTP 回覆；W-signer 沒有新的 log 行 | 收到任何 HTTP 回覆 → 停止 |
| B8 | Aaron，W-host | 讀私有資料：`ls /opt/apa-signer/data`、`cat /opt/apa-signer/data/signer.json`、（B1）`cat /opt/apa-signer/data/keys/<wallet_id>.key`、`cat /opt/apa-signer/data/signer.db`；每個後 `echo "exit=$?"` | 都「Permission denied」（前置：B2 已啟動，代表 `apa-signer` 讀得到這些檔案） | — |
| B9 | Aaron，W-admin | 只看這一個 PID：`ps -o command -p <PID>`（參數裡沒有密語）；`sudo ps -E -o command -p <PID>`（環境變數裡沒有密語；以管理者觀察，只看這個程序）；W-signer 的輸出沒有密語 | 都沒有 | 看到密語 → 立即停止 Signer、記錄，並視為該密語已洩漏 |
| B10 | Aaron，W-signer | Ctrl-C | 程序結束；`ls /opt/apa-signer/run` 是空的（程式結束時移除 socket） | socket 留下 → 第 5 節 |
| B11 | Aaron，W-admin | `sudo -k`；關閉 W-signer | — | — |

**為什麼 403 不需要第三個帳號**：B6 用 `apa-signer` 自己當未授權的呼叫者。它擁有 socket，所以檔案權限讓它連得上；它不在 `callers`，所以由 Signer 的 peer uid 檢查回 403。B7 用既有的 `nobody`，在檔案權限就被擋住。兩者分別證明兩件事，互不代替。正式 socket 的權限沒有為測試放寬。

**觀測的限制**：B9 只看這一個程序的參數、環境與輸出。沒看到不等於所有洩漏管道都不存在（例如終端機捲動緩衝、shell 歷史不會記錄提示下的輸入，但管理者的其他工具不在觀測範圍）。

## 3 驗收表

| 條件 | 執行身分 | 可觀測結果 | PASS | FAIL | 未驗證 | 證據 | 需核准 |
|---|---|---|---|---|---|---|---|
| 帳號與目錄如方案建立 | 管理者 | S6 的 `ls -led`、`id`、hash | 全部符合第 1.2 節、沒有 ACL | 任一不符 | 沒做 S6 | S6 輸出 | D1 |
| Host 使用者讀不到私有資料 | Host 使用者 | A3、B8 的退出碼與訊息 | A2 先通過，且 A3、B8 全部 Permission denied | 任一成功 | A2 沒通過、或錯誤是 No such file | A、B 輸出 | D2、D3 |
| Host 使用者不能替換執行檔、改設定 | Host 使用者 | A4 | 全部被拒、hash 不變 | 任一成功 | — | A4 輸出 | D2 |
| 未授權 uid 在檔案權限被擋 | `nobody` | A5、B7 | Permission denied；B7 exit 7、沒有 HTTP 回覆 | 收到 HTTP 回覆或讀到內容 | — | A5、B7 | D2、D3 |
| Signer 以專用 uid 前景執行，讀得到 `/dev/tty` | `apa-signer` | B2、B3、B4 | 提示出現、serving、PID 的使用者是 `apa-signer` | 無法啟動 | 只在同一 uid 做過 | B2～B4 | D3 |
| socket 的擁有者、群組與權限 | — | B4 | `apa-signer:apa-signer-clients`、`0660`、沒有 ACL | 不同 | — | B4 | D3 |
| Host uid 能呼叫不涉及簽署的端點 | Host 使用者 | B5 | HTTP 200 `unlocked` | 其他 | — | B5 | D3 |
| peer uid 判定（已連線、uid 不在 `callers`） | `apa-signer` | B6 | HTTP 403 `CALLER_NOT_ALLOWED` 與 log 行 | 200 或連不上 | — | B6 | D3 |
| 密語不在參數、環境、輸出 | 管理者觀察單一 PID | B9 | 都沒有 | 看到 | — | B9（不含密語本身） | D3 |
| 金鑰檔在 `apa-signer` 下解鎖 | `apa-signer` | B2（B1 做法） | B1 做法下 serving | 解密失敗 | 只做 B0 | B2 | D3、D4 |
| Signer 不能連網 | — | — | — | — | **未驗證**（目前沒有 OS 機制） | — | D7 |
| Signer 不能改自己的設定 | — | — | — | — | **不成立於 OS 層**（第 1.4 節） | — | D6 |

證據只保留前後權限、程序身分、退出碼與固定訊息；不保存私鑰、密語或完整授權。

**各階段能關閉什麼**：
- A 階段：只關閉「被測路徑的檔案權限」。
- B0：再關閉「專用 uid 執行、socket 權限、跨使用者 peer uid 判定、私有目錄不可讀」。
- B1：再補「金鑰檔在真正 Signer 下解鎖、且 Host 讀不到」。
- 以上都完成，也只補設計第 3.1 節前兩個保證的部署證據。網路限制、設定不可改、管理者、同 uid 的 Agent 都仍在剩餘風險內。方案寫完或執行完，都**不**代表 P1-b 完成。

## 4 剩餘風險

- **三件事分開**：私有資料不可讀（OS）、呼叫者 uid 可信（OS＋Signer）、付款是否獲授權（Signer 規則，已由程式測試涵蓋）。本方案只補前兩者的部署證據。
- **單獨的 uid 與檔案權限不等於網路隔離**：「Signer 不能連網」目前只是程式沒有網路用戶端，沒有 OS 強制，標為未驗證。
- **管理者**：Aaron 是管理員群組成員，sudo 後能讀、改一切。本方案不防管理者，也提醒：Host 使用者本身是管理員帳號，sudo 時間戳若被同一終端機的程式沿用，就等於管理者（第 1.5 節的操作紀律只能降低、不能消除這個風險）。
- **同 uid 的 Host 與 Agent**：可在常設委託範圍內要求付款（決策 3 已接受）；OS 分不出 Host 與 Agent。
- **`apa-signer` 被攻破**：金鑰暴露（T3），也能改自己的設定（第 1.4 節）。
- **備份**：`/opt/apa-signer/data` 若在 Time Machine 等備份範圍內，加密金鑰檔與 DB 會進備份（仍受密語保護）。是否排除，執行時由 Aaron 決定。
- 這不是完整安全稽核。

## 5 停止與撤回

**停止**：遇到帳號／路徑衝突、權限或擁有者不符、出現 ACL、看到不明既有資料、程序沒停、無法確認執行身分、任一預期不符時，立刻停止。不自動修權限、不重試、不換路徑，記錄後交 review。

**留下的 socket**（Signer 非正常結束）：
1. 確認沒有程序：`pgrep -u apa-signer -f '^/opt/apa-signer/bin/apa-signer serve'` 沒有輸出；
2. 確認是 socket：`test -S /opt/apa-signer/run/signer.sock`；
3. 才移除該檔：`sudo rm /opt/apa-signer/run/signer.sock`。

不依程序名稱廣泛終止任何程序。需要停止時只對 B3 那一個 PID 在 W-signer 按 Ctrl-C；不得已才 `sudo kill <PID>`，用 B3 的 PID。

**撤回**（只針對本次新建、已確認的項目，依序；每一步前先 `ls -lea` 該目錄，看到清單以外的檔案就停止）：

| 順序 | 項目 | 命令（**待核准後執行**） | 確認 |
|---|---|---|---|
| 1 | 測試檔 | `sudo rm /opt/apa-signer/data/probe.txt /opt/apa-signer/data/keys/probe.txt` | 先列出確認存在 |
| 2 | （B1）測試金鑰檔 | `sudo rm /opt/apa-signer/data/keys/<wallet_id>.key` | 只刪這一個名稱 |
| 3 | 設定與 DB | `sudo rm /opt/apa-signer/data/signer.json /opt/apa-signer/data/signer.db`，以及存在的話 `signer.db-wal`、`signer.db-shm` | 逐一列名 |
| 4 | 目錄 | `sudo rmdir /opt/apa-signer/data/keys /opt/apa-signer/data /opt/apa-signer/run` | `rmdir` 只刪空目錄；失敗代表還有東西 → 停止 |
| 5 | 執行檔與樹 | `sudo rm /opt/apa-signer/bin/apa-signer`；`sudo rmdir /opt/apa-signer/bin /opt/apa-signer` | 同上 |
| 6 | 群組成員 | `sudo dseditgroup -o edit -d <HOSTUSER> -t user apa-signer-clients` | — |
| 7 | 帳號與群組 | 先確認 `apa-signer` 沒有程序（`pgrep -u apa-signer` 沒有輸出），再 `sudo dscl . -delete /Users/apa-signer`、`sudo dseditgroup -o delete apa-signer-clients`、`sudo dseditgroup -o delete apa-signer` | `dscl . -read` 回 not found |

不使用遞迴刪除，不對 HOME、repo 根目錄或未解析的變數執行任何刪除；`/opt` 本身與其他既有資料保持原樣。

**保留或撤回**：建議 A＋B0 完成後**保留**帳號、群組與目錄（B1、之後的部署會再用），**撤回**測試檔與 B0 的設定、DB；B1 的測試金鑰檔在 B1 結束時撤回。最後由 Aaron 決定（D5）。

## 6 核准清單（下一輪由 Aaron 決定；都尚未核准）

| # | 事項 | 推薦 | 影響 | 核准後的出口 |
|---|---|---|---|---|
| D1 | 系統帳號與權限變更（第 2.0、2.1 節：使用者 `apa-signer`、兩個群組、Host 使用者加入 `apa-signer-clients`、`/opt/apa-signer` 樹與執行檔） | 照第 1 節配置；由 Aaron 在 W-admin 親自執行 | 新增一個隱藏帳號與兩個群組；Host 使用者多一個群組 | S6 的輸出符合第 1.2 節 |
| D2 | A 階段：非敏感檔案權限驗收 | 做 | 只寫兩個公開測試檔 | A 階段的判讀（第 2.2 節） |
| D3 | B 階段：前景啟動、`/dev/tty` 解鎖、IPC 驗收 | 先做 B0（不需金鑰）；B2 讀不到 `/dev/tty` 時停止，另議（例如改程式在 stdin 是終端機時從 stdin 讀，或調整 sudo 的 pty 設定，都需另行核准與測試） | 啟動一次真正的 Signer，不簽署 | B 階段各列 PASS／FAIL／未驗證 |
| D4 | 一次性測試金鑰與所需工具 | 如要做 B1，另行核准一個最小的建金鑰工具：`apa-signer keygen --dir DIR --wallet ID`，以執行者身分在記憶體產生金鑰，從 `/dev/tty` 讀兩次密語，寫 `keys/<ID>.key`（`0600`、已存在就拒絕），只印出錢包 ID 與地址，附單元測試；之後才建立、使用與撤回一把未注資的測試金鑰 | 新的產品程式碼（一個子命令）與一把測試金鑰 | B1 的 B2 serving，B8 讀不到金鑰檔 |
| D5 | 保留或撤回資源 | A＋B0 後保留帳號、群組、目錄；撤回測試檔、B0 設定與 DB | 留下一個隱藏帳號與空目錄 | 第 5 節撤回表照決定執行 |
| D6 | 「Signer 不能改自己的設定」的承諾 | 先採 C1（改寫承諾，不改程式）；C2 列為之後的選項 | 設計文件措辭 | Codex review 後更新設計第 3.2、3.5 節 |
| D7 | 「Signer 不能連網」的強制機制 | 暫標未驗證，不改全機防火牆；之後可評估以 `sandbox-exec` 設定檔只限制 Signer 程序（需本機實測與另行核准） | 無 | 決定接受或另開評估 |

本方案不需要其他新工具：A、B 階段的檢查都用 macOS 內建命令與 `curl --unix-socket`。設計與第 6 節提到的 `scripts/verify/p1b-isolation.sh` 尚不存在，本方案不使用它。

## 7 依據與待實測

| 項目 | 依據 | 狀態 |
|---|---|---|
| `connect(2)` 需要 socket 可寫 | 本機 `unix(4)`：「the destination of a connect(2) or sendto(2) must be writable」 | 手冊；B5～B7 實測 |
| 新檔群組取自上層目錄 | BSD 慣例 | **待實測**（B4） |
| sudo 預設不配 pty、時間戳每個終端機各自記錄、預設 5 分鐘 | 本機 `sudoers(5)`（Sudo 1.9.5p2）：`use_pty`「off by default」、`timestamp_type` 的 `tty`、`timestamp_timeout` 預設 5 | 本機 sudoers 可能覆寫；P0 查核 |
| `sudo -u` 直接執行命令，不用帳號的 shell；能否讀 `/dev/tty` | sudo 的一般行為 | **待實測**（B2） |
| 群組成員變更對新的終端機或要重新登入才生效 | macOS 行為 | **待實測**（S4） |
| `dseditgroup` 建群組與加成員的參數 | 本機 `dseditgroup(8)`：`-o create`、`-i gid`、`-r`、`-o edit -a … -t user` | 手冊 |
| `dscl` 建使用者的欄位 | macOS 慣例 | 執行時以 `dscl . -read` 核對 |
| `/usr/local/bin` 可被 Host 使用者寫入 | 本機 `ls -ld`：`mac admin 0775` | 已讀（所以不放這裡） |

Linux 容器裡的結果（前幾輪的定向 run）不是 macOS 跨使用者實測，本方案不引用為證據。

## 8 核准與執行紀錄

**10/13 Aaron 核准 D1～D7**（原話：「關於 d1 到 d7 我都核准」），即第 6 節各項的推薦做法：

| # | 核准內容 | 現況 |
|---|---|---|
| D1 | 帳號、群組、Host 使用者加入 `apa-signer-clients`、`/opt/apa-signer` 樹與執行檔 | 待 Aaron 在 W-admin 親自執行（需要 sudo 密碼）；執行檔從 D4 之後的 commit 建置 |
| D2 | A 階段 | 待 Aaron 執行 |
| D3 | B 階段，先 B0；讀不到 `/dev/tty` 時停止另議 | 待 Aaron 執行 |
| D4 | 建金鑰工具與一次性未注資測試金鑰 | 工具 `apa-signer keygen` 已實作於 `f047ad6`（定向驗證見第 8.1 節）；測試金鑰的建立、使用與撤回待 Aaron 在 B1 執行（步驟 B0k） |
| D5 | A＋B0 後保留帳號、群組與目錄；撤回測試檔、B0 設定與 DB；B1 的測試金鑰在 B1 結束時撤回 | 照第 5 節撤回表執行 |
| D6 | 採 C1 | 設計第 3.2、3.5 節措辭已改（本次提交） |
| D7 | 「Signer 不能連網」暫標未驗證，不改全機防火牆；`sandbox-exec` 之後另行評估與核准 | 維持未驗證 |

此方案與 keygen 工具尚未經 Codex review；Aaron 的核准不代表 Codex 已複核。

**驗收指令筆記**：[commercial-p1b-os-isolation-runbook.md](commercial-p1b-os-isolation-runbook.md)，給 Codex 先做不用 sudo 的預演、再陪同 Aaron 執行並判讀。其中的命令都還沒有執行或驗證過。

### 8.1 D4 的定向驗證計畫（執行前）

- 計畫：`scripts/verify/p1b-d4-check-plan.json`（4 步）；只列：`go run ./cmd/m1-check --plan scripts/verify/p1b-d4-check-plan.json --list`。
- 執行：`sh scripts/verify/container-plan.sh scripts/verify/p1b-d4-check-plan.json artifacts/p1b/d4-<UTC 時間>`（同一個釘選映像與隔離方式，見 commercial-p1b.md 第 8.1 節）。預估 3–5 分鐘（scrypt 2^17 在測試裡跑數次）。

| # | 步驟 | 範圍 | 上限 |
|---|---|---|---|
| 1 | vet | `go vet ./...` | 10 m |
| 2 | keygen-assembly | `cmd/apa-signer` 全部：`TestKeygen`、`TestKeygenRefuses`、`TestAssemblyRefusesEveryOrder`、`TestAssemblyRefusesItsInputs` | 10 m |
| 3 | keystore | `TestKeyFile`、`TestEIP712SpecVector`、`TestTypeHashes`、`TestSignOne`（`EncryptKey` 多了錢包 ID 檢查） | 10 m |
| 4 | reverse | `P1b-D4:` 3 項 | 20 m |

| 反向項目 | 拿掉的防護 | 測試 → 預期斷言 |
|---|---|---|
| 不覆寫 | 存在檢查改成恆假、`O_EXCL` 改 `O_TRUNC` | `TestKeygen` →「an existing key file was replaced」 |
| 兩次密語一致 | 比對改成恆假 | `TestKeygenRefuses/passphrases_differ` →「passphrases differ: accepted」 |
| 檔案權限 | `0600` 改 `0644` | `TestKeygen` →「key file mode」 |

### 8.2 D4 的定向驗證結果

| 執行 | 基準 | 結果 |
|---|---|---|
| 未啟動（沒有證據目錄） | `0ce52ac` | `docker image inspect apa-verify:b1-05` 回「No such image」，包裝命令接著以空的輸出路徑呼叫 runner，在 `mkdir` 就停止；沒有建立證據目錄、沒有啟動容器、沒有測試執行 |
| 正式容器 run：`artifacts/p1b/d4-20261012T184045Z/` | `0ce52ac`（`tracked_clean: true`，證據完整） | 4 步全部 PASS，exit 0，1 m 17 s |

映像：用標籤查詢時斷續回「No such image」，`docker images` 卻列得出它；以完整 ID 查詢與執行都正常。所以本次用 `APA_VERIFY_IMAGE=sha256:78258368de09…`（與前幾輪 run 同一個映像，未重建、未更新）。標籤查詢失敗的根因沒有確認（推測是 Docker Desktop 的標籤解析問題）；第 9.3 節 `r2b-20261012T172925Z` 那次未啟動，可能也是同一原因。

| # | 步驟 | 結果 | exit | 耗時 | 內容 |
|---|---|---|---|---|---|
| 1 | vet | PASS | 0 | 47.1 s | `go vet ./...` |
| 2 | keygen-assembly | PASS | 0 | 11.2 s | 4 個測試 |
| 3 | keystore | PASS | 0 | 1.6 s | 4 個測試 |
| 4 | reverse | PASS | 0 | 17.6 s | 3／3 被預期斷言抓到（每項的未突變基準先通過） |

實際命中的斷言：「an existing key file was replaced: <nil>, 2 questions」「passphrases differ: accepted」「key file mode: …」（`0644`）。

隔離紀錄（本次容器）：外網拒、寫 `/src`／工具鏈／模組快取拒、只有 `lo` 上線、uid 10001。證據掃描沒有 64 bytes 的 hex 或測試密語。

這只驗證 keygen 的程式行為（在 Linux 容器、同一個 uid）。它在 macOS 以 `apa-signer` 身分從 `/dev/tty` 讀密語、寫進 `/opt/apa-signer/data/keys`，要在 B1（步驟 B0k）實測。

### 8.3 驗收指令筆記的補正（O1～O3）

依 vault〈Claude-Code-P1-bOS隔離驗收指令補正派工-0ef6172-20261013〉。Codex 審閱第一版指令筆記（`0ef6172`）後指出 O1～O3：
- O1：命令能執行，而且失敗會停下；
- O2：權限測試失敗時，也不洩漏或破壞真實目標；
- O3：暫存資源與撤回只動本次建立的東西。

這些是操作文件的補正，不是產品漏洞；R1～R3 維持關閉；D1～D7 的核准不變。

- **證據歸屬**：Codex 前一輪只做文件、原始碼、本機手冊與語法預檢；沒有建置或啟動 Signer，沒有 keygen、sudo 或跨使用者測試，沒有 repo 證據 log。D4 的既有測試仍歸 Claude Code（第 8.2 節），不是 Codex 獨立重跑。
- **本輪**：重寫 [commercial-p1b-os-isolation-runbook.md](commercial-p1b-os-isolation-runbook.md)（修訂對照在其第 9 節），並以 `zsh -f -n` 逐塊做語法解析（44 塊都是 0，紀錄在其第 10 節）。沒有建置、沒有啟動 Signer、沒有 keygen、沒有 sudo、沒有改系統。
- **下一步**：
  1. Codex 限定複核補正後的筆記，並依其第 1 節做受控預演；
  2. 之後由 Aaron 從第 2 節開始操作，第一個關卡是在 W-host 建立本次目錄並建置，看到 `OK 2`。

### 8.4 驗收指令筆記的第二輪補正（O1～O4）

依 vault〈Claude-Code-P1-bOS隔離驗收指令第二輪限定補正派工-c7744be-20261013〉。Codex 對 `c7744be` 做限定唯讀複核（只核對 Git、文件與相關原始碼；沒有重新解析語法、建置、sudo、keygen 或啟動 Signer；沒有新的 repo 證據 log），指出四項：
- 第二輪 O1：預演的執行資格；
- 第二輪 O2：自檢與失敗收尾不得影響未知資源；
- 第二輪 O3：查詢失敗不能解讀為不存在；
- 第二輪 O4：人工觀察失敗要阻止驗收前進。

D1～D7 的核准不變，R1～R3 維持關閉。

- **本輪**：[指令筆記](commercial-p1b-os-isolation-runbook.md)改為第三版（修訂對照在其第 9 節）：
  - 第 1 節改成路徑分流：Codex 受控預演（路徑 A）缺讀取隔離、記憶體、程序數、磁碟與牆鐘上限，目前**不可用、未執行**，預演腳本移出現行入口；Aaron 逐關卡、Codex 陪同判讀（路徑 B）由 Aaron 在開始時明確選擇，結果不是預演 PASS。
  - `dscl`、`pgrep`、`ps`、socket 的查詢錯誤一律停止。
  - 觀察關卡只有 `yes` 通過；安全收尾不依賴驗收標記。
- **語法解析**：43 塊與內嵌的 `lib.zsh` 以 `zsh -f -n` 解析，都是 0 且無 stderr（其第 10 節，另附靜態分支推演）。沒有執行任何區塊、沒有建置、沒有啟動 Signer、沒有 keygen、沒有 sudo。
- **下一步**：Codex 限定複核；之後 Aaron 從第 2 節選擇路徑 B 開始。

### 8.5 keygen 的限定技術複核（Codex，10/13）

依 vault〈Claude-Code-P1-b金鑰工具限定複核與B0驗收同步派工-8fb5f8c-20261013〉。

> keygen 通過本次 B1「一次性、未注資測試金鑰」使用範圍的限定技術複核，可以繼續第 7 節。這不代表正式錢包或整個 P1-b 已驗收。

**Codex 核對的內容**：
- 隨機產生金鑰、兩次密語一致檢查，以及 scrypt 與 AES-GCM 加密；
- 寫入前的解密自檢；以 `0600`、`O_EXCL` 建立，拒絕覆寫既有金鑰；
- 正常輸出只有錢包 ID 與公開地址，不輸出私鑰或密語；
- 正式 `serve` 仍使用 `Unavailable` 預留驗證器，建立金鑰不會啟用付款；
- 第 8.2 節的 D4 四步測試紀錄與三項反向檢查的原始輸出：反向項目命中預期斷言，不是編譯或 panic 失敗。

**基準**：keygen 程式 `f047ad6`；D4 測試與計畫 `0ce52ac`。Codex 確認其後到 `8fb5f8c` 的 `cmd/apa-signer`、`internal/signer` 沒有變更（本輪以 `git diff --stat 0ce52ac..8fb5f8c -- cmd/apa-signer internal/signer` 再看一次，沒有輸出）。

**證據歸屬**：D4 測試由 Claude Code 執行，紀錄在 `artifacts/p1b/d4-20261012T184045Z/`。Codex 本輪只核對 Git、程式、測試、原始紀錄與操作文件；沒有獨立重跑、建置、建立金鑰或啟動 Signer，也沒有新增 repo 證據 log。

**非阻擋的改善點**（只列追蹤，本輪不改程式）：
1. `cmd/apa-signer/main.go` 印出地址時沒有檢查 `fmt.Fprintf` 的錯誤。所以沒看到完整地址時就停止：退出碼不能單獨證明完成，也不能因此重新 keygen。看到完整地址也不能取代正常退出，以及之後的檔案權限與解鎖確認。
2. 解密自檢產生的私鑰副本沒有明確清零，不能宣稱所有記憶體副本都已清除。

**限制**：測試用的是密語替身，不是實際的 macOS TTY。B1 還沒有證明測試金鑰能在 `apa-signer` 下解鎖，也還沒有「Host 讀不到該金鑰」的實測結果。

### 8.6 A 階段與 B0 的操作紀錄（Aaron，Route B，10/13）

**歸屬**：實際操作與終端機觀察是 Aaron 做的；Codex 依 Aaron 貼回的輸出陪同判讀，不是 Codex 在機器上執行或事後獨立重查。Claude Code 本輪只同步文件，沒有讀取 `/opt/apa-signer/data`、金鑰、密語或錄製檔。

**識別資訊**（Aaron 回報）：

| 項目 | 值 |
|---|---|
| 路徑 | Route B（不是 Codex 受控預演；Route A 仍未執行） |
| `RUN` | `/tmp/apa-iso.I0Wtz3` |
| 建置 HEAD | `8fb5f8c1f03f5674222b88e842d0def6b4751e47` |
| 執行檔 SHA-256 | `3f29a2d54c71a74f52840f34bede4b3c1439218c4400b9eee0b8617f243e0683` |
| 錄製目錄 | `artifacts/p1b/os-isolation-20261013T020819Z/`（Codex 本輪沒有核對錄製檔的完整性或雜湊，不是已獨立驗證的完整證據包） |
| 系統與身分 | macOS 12.7.6；Host uid 501；`apa-signer` uid／gid 401；`apa-signer-clients` gid 402 |

**已回報並判讀的結果**：
- **A 階段**：
  - Host 不用 sudo：對 `data/`、`data/keys/` 的公開探測檔，讀取、寫入、取代、改名都被拒；受保護目錄與 `bin/` 的探測也被拒。
  - `nobody`：指定的讀取與目錄探測被拒。
  - 這是公開探測檔的證據，不是付款私鑰的測試。
- **B0**：
  - 空錢包設定下 Signer 啟動。
  - Host health 回 200／`unlocked`；Host 讀取 `signer.json`、`signer.db` 與列出 `data/` 都被拒。
  - `apa-signer` 當作不在 `callers` 的呼叫者：已連線，回 403／`CALLER_NOT_ALLOWED`。
  - `nobody`：直接連 socket 回 `connect_errno=13 reason=Permission denied`（補充判讀見下）。
  - W-signer 的三項人工觀察：Aaron 回覆「三項都符合」，之後回報 `6.6 observation recorded`。Codex 沒有看過未錄製的 W-signer 畫面。
  - 安全停止：`no apa-signer process`、`socket absent`、`OK 6.7`。

**6.5 的原始 STOP 與補充判讀**：
- `nobody` 的 curl 回 7、HTTP 000，訊息只說無法連線，沒有明確的權限拒絕。
- 原本的 `expect_fs_block` 正確停止：`STOP: not a file-permission refusal; judge this output before going on`。
- Aaron 依 Codex 指示補做一次 `nobody` 的 AF_UNIX 直接連線探測，得到 errno 13／Permission denied。該探測只嘗試 connect，沒有送 HTTP、sign 或任何付款請求。
- 之後再取得一次 Host health 200，並記下補充說明，才完成 `OK 6.5`。
- 所以：原始 curl 區塊**沒有**一次通過；curl 7 不能一概當成權限拒絕。

**操作錯誤（不是產品失敗）**：Aaron 曾把需要 `need` 的區塊放到 W-signer，得到 `zsh: command not found: need`；之後在正確的視窗確認了觀察標記與停止狀態。這個錯誤本身不能用來推定程序已停止。

**結論**：A 階段與 B0 無金鑰 Signer，在本次 Route B 的限定操作範圍內完成，並已安全停止。這不是整體 OS 隔離或 P1-b 的結案。環境變數沒有觀測；sudo／管理員能力不在「一般 Host 權限被擋住」的保證範圍內。

**下一步**：B1，由 Aaron 在既有 D1～D7 核准內操作（runbook 第 7 節），Codex 陪同判讀：
- 只建一把 `wallet-isolation-test`，用全新的測試密語；它不是正式或展示錢包，不注資。
- 密語不進命令、環境變數、檔案或對話；W-signer 不錄製。
- 任一步錯誤就停止：不換名稱、不自動重建、不改密語重試。
- 設定仍然沒有 `assets`／`delegations`；不 sign、不連鏈、不呼叫 RPC 或 faucet、不交易。
- 建立後，依序確認檔案權限、Signer 解鎖、Host 讀不到金鑰、IPC、人工觀察與安全停止，才能判讀 B1。
- 清理照 D5 與 runbook 第 8 節，在確認程序與 socket 已停止、清單一致之後，由 Aaron 在另一道關卡操作。

### 8.7 B1、D5 清理與本次暫存清理（Aaron，Route B，10/13）

依 vault〈Claude-Code-P1-bB1隔離驗收與清理同步派工-b7cc85d-20261013〉。

> 依 Aaron 實際執行與回報，A／B0／B1 的限定 OS 隔離驗收及本次測試資料清理已完成；Signer 已停止，socket 與本次暫存已清理。這不代表管理員防護、網路隔離或全面安全稽核，也不代表正式付款已啟用。

**基準**：執行時的建置基準仍是 `8fb5f8c1f03f5674222b88e842d0def6b4751e47`（不是文件提交 `b7cc85d`）；執行檔 SHA-256 `3f29a2d54c71a74f52840f34bede4b3c1439218c4400b9eee0b8617f243e0683`（同第 8.6 節）。

**B1**（全部由 Aaron 執行，Codex 依貼回的輸出陪同判讀）：
1. 在不錄製的 W-signer 建立一次性測試金鑰 `wallet-isolation-test`，回報完整公開地址與 `keygen exit=0`。地址 `0xef4d86f298b5020ff083e6414f24a11571bb11b9` 只用於本次測試識別，不是可用的付款帳戶；金鑰已刪除，不得注資。
2. 7.2 的設定只加入這個測試錢包：`callers=[501]`、`assets=[]`、`delegations=[]`。7.3 核對金鑰與設定檔都是 `apa-signer:apa-signer:600`，啟動前沒有 Signer 程序或 socket。
3. W-signer 用剛建立的測試密語啟動，回報 `serving on /opt/apa-signer/run/signer.sock (reservation verification unavailable until P2: nothing is signed)`。依已複核的 `assemble` 行為，這表示設定裡的測試金鑰成功載入並解鎖；這不是簽署或付款成功。
4. 7.5：Host health 回 200；不用 sudo 讀測試金鑰、`signer.json`、`signer.db` 都得到 `Permission denied`，回報 `OK 7.5`。讀取探測的內容丟棄，沒有印出私鑰。
5. 7.6：未授權的 `apa-signer` 呼叫者回 403；`nobody` 直接 AF_UNIX connect 得到 `connect_errno=13 reason=Permission denied`，回報 `OK 7.6`。這次直接用 B0 已判讀過的補充方法（第 8.6 節），不是 curl helper 通過，B1 也沒有再發生一次 curl STOP。
6. Aaron 貼回 W-signer 的兩行 `health unlocked` 與一行 `caller  CALLER_NOT_ALLOWED`，並明確回答「沒有顯示密語」；之後確認 `7.6o observation recorded`。人工觀察歸 Aaron，Codex 沒有看過未錄製的畫面。
7. Ctrl-C 後回報 `no apa-signer process`、`socket absent`、`OK 7.7`。

沒有執行 sign、注資、鏈上或外部交易。

**D5 清理**（Aaron；證據目錄 `artifacts/p1b/os-isolation-20261013T020819Z/`）：
- 8.1：先確認程序與 socket 不存在，再列清單；只有已知項目，回報 `OK 8.1`。
- 8.2：因為訊息截斷，**拆成三段**操作：重新核對清單未變、刪除具名項目、確認保留清單並保存證據。這不是原始區塊一次執行。
- 刪除六個實際存在的檔案：測試金鑰、`data/keys/probe.txt`、`data/probe.txt`、`data/signer.json`、`data/signer.db`、`bin/probe.txt`；另以非遞迴的 `rmdir` 移除空的 `probe-dir`。這次清單沒有 DB 的 WAL／SHM，所以沒有刪它們。
- 保留項目精確為 `bin/`、`bin/apa-signer`、`data/`、`data/keys/`、`run/`，以及已建立的帳號與群組（D5）。三個私有／socket 目錄的權限核對正確，回報 `OK 8.2`。
- `sudo-l.txt`、`ids`、`inventory`、`remaining` 複製到證據目錄；Aaron 回報 W-admin、W-host 都已退出錄製。
- 測試金鑰已刪除：不宣稱有可恢復的備份，也不宣稱這是對底層儲存媒體的安全抹除。

**本次暫存清理**（Aaron）：先把 `probe-hashes` 複製到證據目錄，再刪除本次暫存的建置、設定副本、交接檔、標記與空目錄（原 `RUN=/tmp/apa-iso.I0Wtz3`）。回報：`DONE: 本次暫存已清理，證據保留於 .../os-isolation-20261013T020819Z`。這個 `RUN` 已不存在，不是下一次操作的入口。

**證據歸屬**：
- **Aaron**：sudo、密語輸入、keygen、Signer 啟動與停止、權限與 IPC 探測、人工觀察、錄製與清理。
- **Codex**：
  - 原始碼與既有測試紀錄的限定複核；提供逐關卡指示，並依 Aaron 的輸出判讀。
  - 退出錄製後只讀核對過檔案 metadata：兩份 log 存在且非空、清理清單存在、暫存目錄項目符合本次範圍。
  - 沒有讀完整錄製內容，沒有全面核對錄製檔的完整性或雜湊，沒有獨立重新執行 B1。
- **Claude Code**：先前的程式實作與 D4 測試，以及本輪的文件同步。本輪沒有讀錄製檔、沒有掃描個人資料、沒有複製或上傳 log。
- **未核對項**：錄製檔的完整性與雜湊沒有證明；若之後需要，另列處理，不因此重跑部署或金鑰驗收。

**界線**（不變）：
- 只驗證這台 macOS、本次設定與非提權使用者的限定行為。不能抵抗 sudo／管理員，也不能辨識同一 UID 下是哪一個 Host 程式。
- 環境變數未觀測；Route A 未執行；沒有 OS 強制的網路隔離。
- keygen 的地址輸出錯誤未處理、解密自檢的私鑰副本未明確清零，維持非阻擋追蹤（第 8.5 節），本輪不改程式。
- 正式付款仍未啟用：沒有 A／B 正式金鑰、正式本人核准、WebAuthn、鏈上預留、付款或交付的驗收。
- CLI timeout 後是否禁止新付款，仍是另列決策。
