# P1-b：金鑰庫與獨立 Signer（最小設計）

> 2026-10-12，Claude Code。初版依 vault〈Claude-Code-P1-b金鑰庫與獨立Signer最小設計派工-1fb6225-20261012〉（Aaron：「我批准進行P1-b「金鑰庫與獨立 Signer」最小設計」），交付於 `3314e7c`；本版依〈Claude-Code-P1-b設計R1-R4補正派工-3314e7c-20261012〉修訂。分支 `commercial-p1a`。
> **狀態：P1-b 設計已通過 Codex 限定複核（`e725c44`：R3、R4 關閉，R1、R2 維持關閉）；這是設計通過，不是程式、OS 隔離或付款驗收。Aaron 已核准決策 1–3（第 9 節）：P1-b 程式實作與定向驗證、兩個新依賴、常設委託的信任邊界。實作依〈Claude-Code-P1-b金鑰庫與獨立Signer實作派工-e725c44-20261012〉在分支 `commercial-p1b` 完成：Codex 對 `22e45ed` 的技術 review 暫不通過（R1～R3）；P1-b 實作 R1～R3 補正完成，待 Codex 限定複核；OS 隔離部署尚未驗收；正式付款仍未啟用（實作紀錄、差異與驗收見 [commercial-p1b.md](commercial-p1b.md)）。系統設定（決策 4）、日常或展示用付款金鑰、以正式材料簽署、連鏈或交易仍未核准。**
> Aaron 原話：「這三件事我都核准，請提供派工筆記」。核准範圍：只用測試金鑰與替身，不接進展示付款流程；新依賴只取 `github.com/decred/dcrd/dcrec/secp256k1/v4` 與 `golang.org/x/crypto` 並釘選；接受 Host 同帳號程式被控制時，仍可能在 Signer 設定允許的商家、資產、每筆上限與累計上限內要求付款（這不是允許突破限額，也不是已證明程式能正確執行限額）。
> 以下為 R3、R4 補正交付時（`e725c44`）的狀態，保留原文：P1-b 設計 R1、R2 已通過限定複核；R3、R4 補正完成，待 Codex 限定複核。尚未核准實作、依賴安裝、系統設定、建立金鑰、簽署或交易。（第 0.2 節。常設委託的風險接受、實作與新依賴、OS 設定仍由 Aaron 另行決定；設計複核不是其中任何一項的核准。）
> 以下為 R1～R4 交付時（`19fe1f7`）的狀態，保留原文：P1-b 設計 R1～R4 補正完成，待 Codex 限定複核；尚未核准實作、依賴安裝、系統設定、建立金鑰、簽署或交易。
> （以下兩段是 R1～R4 交付時的說明，保留原文）Codex 對 `3314e7c` 的結論：獨立 Signer 的方向保留，設計暫不通過，先補 R1～R4。這些是實作前的規格缺口，不是已重現的產品漏洞。Codex 前輪做的是 Git、文件、原始碼與官方公開資料的唯讀核對；沒有跑測試、執行真實 CLI、簽署、改產品或驗證作業系統隔離，repo 裡沒有對應的測試 log。本版是 Claude Code 的設計修訂與來源核對，待 Codex 限定複核，**R1～R4 尚未驗收**。
> 本輪只讀程式與文件、修訂設計與驗收計畫：沒有改產品、契約、schema、Signer 樁，沒有原型、測試或建置，沒有建立系統帳號或 socket 目錄，沒有產生、匯入或讀取任何金鑰，沒有連 RPC、faucet 或 facilitator。

依據：[付款帳戶 ADR](adr/payment-account-model.md)（Accepted：每位 consumer 一個 EOA＋EIP-3009，金鑰由隔離的 Signer 保管；Signer 只在同時有一次性授權與鏈上已驗的 `intent_hash` 時才簽）、[commercial-transaction-flow.md](commercial-transaction-flow.md) 第 3、10、11 節、[commercial-demo-roadmap.md](commercial-demo-roadmap.md) 的 P1／A2／P2 切片，以及第 1 節列出的現行程式。

## 0 R1～R4 修訂對照

| # | 原規則（`3314e7c`） | 現行推薦 | 理由 | 章節 | 未來驗收 |
|---|---|---|---|---|---|
| R1 | Host 持有 policy grant 的簽發金鑰；與 Host 同 UID 的程式能偽造 policy grant，後果以「單筆限額、nonce 唯一、P2 預留」說成有界；又以「沒有有效 grant」說明 Agent 連上 Signer 也拿不到簽章 | 分開三個保證（私鑰不可讀、呼叫者可識別、這筆已授權）。**取消 Host 的 policy 簽發金鑰**。自動放行改由 **Signer 端的常設委託**授權：只有管理者能建立或修改（設定檔屬 `apa-signer`），Signer 自己執行每筆上限與**累計上限**。本人核准由 Signer 直接驗 WebAuthn（A2）。Host 自報的 Allow 與 P1-a 的任務委託都不是 Signer 的授權依據。同 UID 的程式仍可在常設委託的累計上限內，向允許的商家付款：這是需要 Aaron 明確接受的信任邊界（決策 3），不是已完成的授權隔離 | nonce 唯一只防重用；單筆上限不是總風險上限；授權依據必須是 Host 與 Agent 偽造不了的東西 | 2、3、4.1、5.6、9 | 第 8 節 R1 列 |
| R2 | `attempts` 只有 digest 等欄位；「完整簽章只回傳一次」，又寫重送回原結果 | 接受時原子保存不可變的 canonical 快照（付款內容、授權、期限、錢包與金鑰版本、驗證結果、請求指紋）；三個中斷點都只讀快照；查回既有結果與新請求分開；已保存的授權可以在授權範圍內重複回給同一個呼叫者 | hash 還原不出付款欄位；恢復時不能再相信呼叫者 | 5.1–5.5 | 第 8 節 R2 列 |
| R3 | 接受前看 grant 與付款時間窗，恢復時只看 `valid_before`；寫「Host 取消後送來也會被拒」 | 接受時固定 `auth_deadline = min(授權到期, valid_before)`；三個階段都依它，未簽的恢復不跨過它；取消的權威在 Host，Signer 沒有可驗證的撤回，刪除原保證；abandon 與簽署以同一個條件式狀態轉換決勝，簽章只在 `signed` 提交後才離開 | 規則前後一致；不以「Host 不送」代替「Signer 可拒」 | 5.3、5.7、5.8 | 第 8 節 R3 列 |
| R4 | schema 4 只放寬 `grants` 的方法 CHECK、加 `issuer_kid` | 三類授權的資料關係分開：`simulated` 與 `webauthn` 有核准會話與 `grants` 列；常設委託路徑**沒有** `grants` 列，記在新的 `payment_attempts`（`delegation_ref`）。P1-b 的 schema 4 只加 `payment_attempts`；`grants` 的方法放寬到 `webauthn` 留到 A2 | 現行 `grants.approval_session_id` 是 `NOT NULL UNIQUE REFERENCES approval_sessions`，不為自動放行偽造本人會話 | 6 | 第 8 節 R4 列 |

### 0.2 R3、R4 限定補正（Codex 對 `19fe1f7` 的限定複核）

Codex 的結論是：R1、R2 的設計缺口可以關閉；R3、R4 還有局部矛盾，補正前整份設計暫不通過，但不需要重做架構。
- R1 關閉，只代表設計補正通過。Aaron 還沒有接受常設委託的付款風險，OS 隔離也還沒實測。
- R2 關閉，不代表實作或當機恢復測試已通過。

Codex 這輪做的是限定安全設計複核：唯讀核對 Git、設計與相關原始碼，確認 `19fe1f7` 只改了四份文件；沒有執行測試、CLI、簽署或系統隔離驗證，也沒有修改產品或設計文件。R3、R4 是設計一致性缺口，不是已重現的產品漏洞；repo 裡沒有對應的測試 log。本輪是 Claude Code 的文件修訂與來源核對，**R3、R4 尚未通過**。

| 項目 | 原規則（`19fe1f7`） | 修訂後 | 理由 | 章節 | 未來驗收 |
|---|---|---|---|---|---|
| R3-a 最後狀態轉換的期限 | 第 5.3 節要求未簽的不能跨過 `auth_deadline`，但第 5.7 節的簽署更新只有 `WHERE state='accepted'` | 簽署的最後判定在「寫入 `signed`」的那個寫入交易裡：取得寫入鎖後取一次可信時間，條件式更新同時要求 `state='accepted'` 與 `now < auth_deadline`；不符就在同一交易把仍是 `accepted` 的轉成 `expired_unsigned`，丟棄尚未交出的簽章 | 計算簽章前有效、寫入前到期的情況原本沒有防護 | 5.3、5.4、5.7 | 第 8 節 R3-a 四列 |
| R3-b 鎖與取消競爭 | 錢包鎖涵蓋驗證、簽署與提交，abandon 也取同一把鎖；驗收卻要求 abandon 插在簽完、提交前（不可能到達） | 錢包鎖**只**序列化同一錢包的接受與簽署工作；abandon **不取**錢包鎖。勝負由同一列的條件式交易決定（SQLite 單一寫入者） | 保留「保存前仍可放棄」的設計意圖，讓驗收的插入點可到達 | 5.6、5.7 | 第 8 節 R3-b 三列 |
| R4 P1-b 與 A2 的分界 | 第 6.2 節說 P1-b 不改 `grants`、WebAuthn 留到 A2；第 6.4 節的 DDL 與第 8 節卻讓 schema 4 接受 `webauthn` | P1-b 的 schema 4：`payment_attempts.method` 只能是 `delegation`，**沒有** `grant_jti` 欄位。WebAuthn 的欄位、CHECK 與正例全部移到 A2 的遷移與驗收 | 本片不得形成有效的 WebAuthn 付款路徑；DDL、說明與驗收要一致 | 6.1–6.4、8 | 第 8 節 R4 四列 |

## 1 現況與差距

| 現況 | 程式位置 | 對 P1-b 的意義 |
|---|---|---|
| 正式契約 `apa.pay.v1`：業務意圖＋`PaymentBinding{method: eip3009, chain_id, asset_contract, from, pay_to, amount, valid_after, valid_before, nonce}` 一起算 `intent_hash` | `internal/paycontract/paycontract.go:87`、`:208` | Signer 可以自己重算；付款欄位只在綁定裡一份 |
| EIP-3009 nonce 由 task 與商品導出 | `internal/toolbridge/shop.go:276` | 同一筆請購只有一個 nonce；要重付必須是新的請購 |
| 可信資產帶 EIP-712 名稱與版本；報價 `extra` 帶 `name`／`version` | `internal/contracts/registry.go:29`、`internal/merchant/merchant.go` | typed-data domain 以可信登記為準 |
| `AuthorizationGrant` 未簽章，只接受 `simulated`；`jti` 由核准會話導出 | `internal/paycontract/grant.go:75`、`:92–99`、`:43` | P1-a 的型別，不能直接升級成正式授權 |
| `grants`：`approval_session_id NOT NULL UNIQUE REFERENCES approval_sessions`，方法 CHECK＝`simulated` | `internal/store/store.go:428–435` | 每筆 grant 都屬於一個本人核准會話（第 6 節） |
| grant 的建立、讀取與復原都沿核准會話：`ResolveApproval` 寫入，`loadSessions` 以 `grants JOIN approval_sessions` 讀，`verifyPay`／`verifyPayTables` 要求 approved 會話恰有一筆 grant | `internal/store/pay.go:433`、`:505`、`:116`、`:606`、`:682` | 第 6.2 節逐項說明要不要改 |
| Signer 樁：不持鑰、不存狀態，`simulated` 一律 `GRANT_METHOD_NOT_ACCEPTED` | `internal/signer/signer.go:17` | P1-b 取代它；`simulated` 的拒絕規則照舊 |
| `go.mod` 只有 SQLite（modernc）；沒有 secp256k1、Keccak | `go.mod` | 需要新依賴（決策 2） |

**差距**：付款金鑰沒有保管與隔離；沒有 Host 與 Agent 偽造不了的授權依據；沒有一次性消耗與累計用量的權威紀錄；沒有付款嘗試的狀態與恢復；沒有鏈上預留驗證（Z2／P2）。

## 2 推薦方案

Signer 是一支獨立的 Go 程式，以專用的 macOS 一般使用者 `apa-signer` 執行。它獨占付款金鑰（加密金鑰檔，啟動時在終端機輸入密語解鎖）、自己的 SQLite 與自己的可信設定；只接受 Unix socket 上、OS 回報為 Host 使用者 uid 的呼叫。

**授權只有兩種依據，兩者都不能由 Host 或 Agent 偽造**：
- **本人逐筆核准**（A2）：附 WebAuthn assertion，Signer 用自己登記的核准者公鑰驗證。
- **常設委託**：管理者寫在 Signer 設定裡的每位 consumer 的自動放行條件，包括商家、資產、每筆上限、累計上限與期限。累計用量由 Signer 自己記帳。

Signer 自己重算並核對一切；鏈上預留驗證完成之前（P2 之前），任何一筆都不簽。

保管後端（金鑰檔）放在一個小介面後面，之後可換 keychain、HSM 或雲端 KMS，不影響授權、狀態與恢復規則。

## 3 A 隔離與三個保證

### 3.1 三個保證分開

| 保證 | 可信資料 | 最後執行檢查的元件 | 不涵蓋 |
|---|---|---|---|
| **付款私鑰不可讀** | 金鑰庫目錄與檔案的擁有者與權限（`apa-signer`，`0700`／`0600`）；密語只在終端機輸入 | 作業系統核心（檔案權限、不同使用者之間不能附掛 process） | 本機管理員（sudo）；`apa-signer` 本身被攻破（ADR 的 T3） |
| **服務呼叫者可識別** | socket 對端 uid（`LOCAL_PEERCRED`）與設定中的 `callers` | Signer | 不能分辨同一 uid 裡是 Host、CLI 還是其他程式；**也不能分辨是哪位 consumer 的請求**（A、B 都經同一個 Host uid） |
| **這筆付款確實被授權** | 本人核准：核准者公鑰登記與 assertion（A2）。自動放行：Signer 設定裡的常設委託，以及 Signer DB 的累計用量 | Signer（第 4.3 節的檢查） | 常設委託範圍內，同 uid 程式發起的付款（決策 3 要接受的邊界）；單筆是否「合理」（不是風控平台） |

三者各自成立，彼此不能互相代替。能連上 socket 不代表被授權；有授權也不代表可以讀金鑰。

### 3.2 元件與信任邊界

| 元件 | 執行身分 | 持有 | 能做 | 不能做 |
|---|---|---|---|---|
| Agent（Claude CLI）與 MCP bridge | Aaron（Host 啟動） | 這一場的 MCP 能力 | 三個業務工具 | 讀付款金鑰（OS 權限）；要求超出常設委託的付款 |
| Go Host（Store、Policy、Approval 元件） | Aaron | Host DB、demo token；**沒有任何簽發金鑰** | 組請購、裁決、轉送本人核准的 assertion、向 Signer 請求簽署、保存 attempt 鏡像、在自己的 API 依 consumer 篩選 | 讀付款金鑰；建立或放寬常設委託；偽造本人核准 |
| Signer | `apa-signer`（一般使用者，非管理員） | 付款金鑰（記憶體）、Signer DB、可信設定 | 驗證後簽一筆 EIP-3009 `TransferWithAuthorization` | 簽任意 bytes 或交易；送交易；連網；修改自己的設定 |
| 管理者（Aaron，本機管理員） | sudo | 一切 | 建帳號、寫 Signer 設定（常設委託、核准者公鑰）、解鎖 | 本設計不防禦管理員 |

資料流：
1. 請購 → Host 裁決。
2. 分兩條路：
   - Ask：A2 之後由本人用 WebAuthn 核准，得到 assertion。
   - Allow：Host 以常設委託的 ID 請求簽署。
3. Host 把授權、完整的 v1 請購與預留參照送給 Signer。
4. Signer 驗證、保留 jti 與用量、簽署並保存，再把 EIP-3009 授權回給 Host。
5. （P2）買方帶 `PAYMENT-SIGNATURE` 重送給商店。

### 3.3 金鑰與憑證

| 項目 | 誰持有 | 存放 | 解鎖／取得 | 重啟 |
|---|---|---|---|---|
| 付款私鑰（每個錢包一把 secp256k1） | 只有 Signer | `apa-signer` 的金鑰庫目錄（`0700`），每把一個加密檔（scrypt＋AES-256-GCM），檔名＝`wallet_id` | 啟動時從 `/dev/tty` 讀密語解密到記憶體；密語不進檔案、環境變數、參數或 log | 每次重啟重新輸入 |
| 本人核准的依據（A2） | 驗證器在使用者的裝置上；**公鑰**登記在 Signer 設定 | 設定檔（`apa-signer` 擁有） | 管理者登記 | — |
| 常設委託 | 管理者寫在 Signer 設定 | 同上 | Signer 啟動時載入；每筆委託有版本與 hash | 修改要由管理者改檔並重啟 Signer |
| 呼叫 Signer 的憑證 | 不另發秘密 | — | 對端 uid | — |
| grant 簽發金鑰 | **不設**（初版的 Host `policy-issuer` 取消） | — | — | — |

**不能出現在 log、錯誤訊息與證據包裡的**：
- 付款私鑰與密語；
- 完整的 EIP-3009 簽章（只回給呼叫者，證據只記它的 hash）；
- WebAuthn assertion 原文（只記 hash 與驗證結果）。

**可以記**：attempt ID、jti、`intent_hash`、typed-data digest、錢包 ID、常設委託 ID 與版本、固定錯誤碼、時間。

### 3.4 為什麼要另一個 OS 使用者；剩下的風險

只另開一個 process、換環境變數，或在同一個使用者下放一個 `0700` 目錄，都**不算**私鑰隔離：Agent 與 Host 以 Aaron 執行，同一使用者的程式都讀得到。專用使用者 `apa-signer` 讓核心執行檔案權限，Aaron 的程式也無法附掛 `apa-signer` 的 process。

需要的系統設定（**之後另行核准，本輪不執行**）：一般使用者 `apa-signer`、群組 `apa-signer-clients`（Aaron 加入）、socket 目錄（`apa-signer:apa-signer-clients` `0750`，socket `0660`）。不需要 keychain、容器或 launchd。

剩下的風險：
- **本機管理員**能讀任何東西，也能改 Signer 的設定。
- **與 Host 同 uid 的程式**（含被攻破的 Host 或 CLI）：
  - 可以在常設委託的範圍內發起付款：允許的商家與資產、每筆上限之內，直到累計上限用完。
  - 可以查詢或放棄任何 consumer 的 attempt；查詢時可以讀到已保存的授權。這些授權本來也是它能要求的，所以沒有擴大支出；放棄只會造成拒絕服務，不會多花錢。
  - **不能**超出累計上限，不能付給清單以外的商家，不能偽造本人核准，不能讀付款金鑰。
- Agent 與 Host 同一個 uid，所以「Agent 連不上 Signer」靠的是 CLI 沒有內建工具（Gate 檢查），不是 OS 權限。授權本身不靠這一點，而是靠第 3.1 節第三列。
- `apa-signer` 被攻破時，該錢包的資金全部暴露（T3）。

### 3.5 Signer 的可信設定

`apa-signer` 擁有，只有管理者能改；請求不能指定檔案路徑、金鑰或委託內容：

```text
wallets:     wallet_id → { consumer_id, chain_id, address, key: <wallet_id>.key }
assets:      (chain_id, asset_contract) → { eip712_name, eip712_version, decimals }
delegations: delegation_id → { version, consumer_id, wallet_id, asset, merchants[],
                               per_tx_max, total_cap, valid_from, valid_until }   # 常設委託
approvers:   (A2) consumer_id → [ { credential_id, public_key, signer_max } ]   # 本人核准每筆硬上限
callers:     [ host_uid ]
limits:      { max_window_seconds: 600 }
```

啟動時核對：金鑰推出的地址＝`address`；一個地址只屬於一個 consumer；委託的錢包屬於它的 consumer。錢包數與委託數都不設上限。帳號總數、Agent 並行數、同時付款數分開：每個錢包同時只處理一筆，不同錢包可以並行。驗收只用 A、B。

## 4 B 請求與授權驗證

### 4.1 每筆、累計與預算各由誰負責

| 限制 | 負責 | 可信來源 | 備註 |
|---|---|---|---|
| 每筆金額、商家、資產、錢包 | Signer | 常設委託或本人核准登記 | 單筆上限**不是**總風險上限 |
| 常設委託的累計用量 | Signer | Signer DB：該委託下狀態為 `accepted` 或 `signed` 的 attempt 金額總和，**每次都由狀態推導**，不另設計數器（`abandoned`、`expired_unsigned` 自然不再計入，不會重複釋放；`signed` 一律計入，直到 P2 對帳能證明未結算） | 這才是自動放行的總風險上限 |
| 每個 task 的預算、P1-a 的任務委託 | Host（Policy、Store）；之後由鏈上預留（Z2／P2）在鏈上扣減 | Host 自報（同 uid 可偽造）；P2 起是預算合約 | Signer 不信任 Host 自報的任務委託，只當作「只能更嚴」的參考 |
| 鏈上預留 | 預算合約（Z2）；Signer 檢查預留事件 | 合約狀態與事件中的 `intent_hash` | 預留證明**預算已扣**，不證明**有人授權**；所以 Signer 要求「預留」**加上**（本人核准或常設委託），兩者缺一都不簽 |

建立與修改常設委託：只有管理者，透過 Signer 設定。之後（A2）可以改成由 consumer 用 WebAuthn 簽署委託，Signer 驗證後當作依據，屆時再決定是否取代管理者設定。P1-a 的「建立委託」頁面與 `delegations` 表是 Host 這邊的流程記錄，不是 Signer 的授權依據。

### 4.2 最小介面

傳輸：HTTP over Unix socket；每個請求 ≤ 64 KiB，處理上限 5 秒，連線數有上限。

| 路由 | 用途 |
|---|---|
| `POST /v1/sign` `{request_id, authorization, order, reservation}` | 新請求，或同一請求的重送（第 5.5 節） |
| `GET /v1/attempts/{attempt_id}`、`GET /v1/attempts?request_id=` | 查回既有結果；不建立 attempt、不重新驗證授權期限 |
| `POST /v1/attempts/{attempt_id}/abandon` | 放棄 `accepted`（未簽）的 attempt |
| `GET /v1/health` | 已解鎖、設定已載入；不含金鑰資訊 |

`authorization` 有兩種形狀：

```text
{ "method": "delegation", "delegation_id": "...", "delegation_version": 3 }                 # 常設委託
{ "method": "webauthn", "approval_session_id": "...", "jti": "...", "expires_at": ...,
  "approver": { "credential_id": "..." },
  "assertion": { "authenticator_data", "client_data_json", "signature" } }                # A2
```

`simulated` 或任何其他方法一律 `GRANT_METHOD_NOT_ACCEPTED`。`webauthn` 在 A2 之前一律回 `HUMAN_APPROVAL_UNAVAILABLE`。

回覆：`{attempt_id, state, intent_hash, typed_digest, auth_deadline, authorization?}`；`authorization` 是 `{from, to, value, valid_after, valid_before, nonce, signature}`，只在 `signed` 時出現。沒有簽任意 bytes 或交易的入口。

錯誤碼：
- 呼叫與格式：`CALLER_NOT_ALLOWED`、`BAD_REQUEST`、`ORDER_INVALID`。
- 授權：`GRANT_METHOD_NOT_ACCEPTED`、`HUMAN_APPROVAL_UNAVAILABLE`、`APPROVER_UNKNOWN`、`ASSERTION_INVALID`、`DELEGATION_UNKNOWN`、`DELEGATION_VERSION`、`DELEGATION_EXPIRED`、`AUTHORIZATION_EXPIRED`。
- 綁定與限額：`INTENT_MISMATCH`、`WALLET_NOT_REGISTERED`、`CONSUMER_MISMATCH`、`ASSET_NOT_ALLOWED`、`MERCHANT_NOT_ALLOWED`、`OVER_PER_TX`、`OVER_TOTAL_CAP`、`WINDOW_INVALID`。
- 預留：`RESERVATION_UNAVAILABLE`、`RESERVATION_MISMATCH`。
- 一次性與 attempt：`GRANT_REPLAYED`、`INTENT_IN_USE`、`NONCE_IN_USE`、`REQUEST_ID_CONFLICT`、`ATTEMPT_UNKNOWN`、`ATTEMPT_NOT_ACCEPTED`、`ATTEMPT_ABANDONED`、`ATTEMPT_EXPIRED`。
- 金鑰與狀態：`KEY_VERSION_MISMATCH`、`SIGNER_LOCKED`、`SIGNER_BUSY`、`CLOCK_REGRESSION`；`SIGNER_TIMEOUT`（實作 R2 補正加入：請求期限讓工作結束，503；接受前沒有保留任何東西，接受後的 attempt 維持 accepted，以同一 request ID 完成，見 commercial-p1b.md 第 8 節）。

### 4.3 新請求的檢查（任一不符即拒絕，不保留任何東西）

1. **請購**：以 `paycontract` 嚴格解碼並 `Validate`，重算 `intent_hash`。
2. **授權**：
   - `delegation`：委託存在、版本相符、`valid_from ≤ now < valid_until`，委託的 consumer 與錢包＝請購的。
   - `webauthn`（A2）：核准者已登記給該 consumer；assertion 的 challenge＝`H("agent-payment-authority/pay/v1/approval" ‖ approval_session_id ‖ intent_hash ‖ jti ‖ expires_at)`；簽章有效；`now < expires_at`。
3. **身分**：`wallets[wallet_id]` 的 consumer＝請購的；`binding.from`＝該錢包地址；`binding.chain_id`＝錢包的鏈。
4. **資產與報價**：資產已登記；報價 `extra.name`／`version`＝登記的 EIP-712 名稱與版本；報價 hash 與綁定一致。
5. **限額**：
   - `delegation`：`pay_to ∈ merchants`、`asset` 相符、`amount ≤ per_tx_max`，且累計用量加上 `amount` 不超過 `total_cap`。
   - `webauthn`：`amount ≤ signer_max`。
6. **時間**：`valid_after < now < valid_before`，且 `valid_before − now ≤ max_window_seconds`。`auth_deadline = min(授權到期, valid_before)` 必須晚於 `now`；授權到期在 `delegation` 是 `valid_until`，在 `webauthn` 是 `expires_at`。
7. **預留**：預留驗證介面確認鏈上已預留，且事件中的 `intent_hash` 相符。P2 前，正式實作一律回 `RESERVATION_UNAVAILABLE`。
8. **唯一性**：jti（`webauthn`）、`intent_hash`、`(chain, asset, from, nonce)` 都沒有被其他 attempt 用過。

**兩個 hash 分開**：
- `intent_hash`：本契約的應用層 hash，綁定核准、預留與稽核。
- 實際簽署的 EIP-712 digest：`keccak256(0x19 ‖ 0x01 ‖ domainSeparator ‖ hashStruct(TransferWithAuthorization{from, to, value, validAfter, validBefore, nonce}))`，domain 是 `{name, version, chainId, verifyingContract=asset_contract}`。

兩者都從同一份請購重算，快照同時保存兩者。M1 的 fixture hash 不會被拿來簽。

## 5 C 一次性消耗、快照與恢復

### 5.1 Signer DB 與接受時的快照

```text
attempts(attempt_id PK, request_id UNIQUE, fingerprint, jti UNIQUE NULL, intent_hash UNIQUE,
         wallet_id, chain_id, asset_contract, from, nonce, delegation_id NULL, amount,
         state, auth_deadline, typed_digest, snapshot, signature NULL,
         created_at, signed_at NULL, closed_at NULL)
UNIQUE(chain_id, asset_contract, from, nonce)
```

`snapshot` 是接受時寫入、之後不再改的 canonical JSON（`snapshot_version: 1`）：
- `request`：`request_id`、`fingerprint`；
- `order`：完整的 v1 請購（含報價），正是被驗證的那一份；
- `authorization`：方法與內容，例如委託 ID、版本與委託內容的 hash，或核准會話、jti、`expires_at` 與 assertion 的 hash；
- `verified`：`intent_hash`、完整的 typed data（domain 與 message 欄位）、`typed_digest`、`wallet_id`、`address`（金鑰版本）、`auth_deadline`、設定版本（Signer 設定的 hash）、預留參照與其驗證結果、`accepted_at`。

索引欄位只放查詢與唯一性需要的部分，其餘都在快照裡。恢復、期限核對與回傳授權**只讀快照**，不讀呼叫者這次送來的內容。

### 5.2 請求指紋

`fingerprint = sha256("agent-payment-authority/signer/v1/request" ‖ 0x00 ‖ canonical({fingerprint_version: 1, order, authorization, reservation}))`，canonical 用本 repo 的 `contracts.Canonical`。

- 同一個 `request_id`、相同指紋：重送（第 5.5 節）。
- 同一個 `request_id`、不同指紋：`REQUEST_ID_CONFLICT`。
- 不同 `request_id`、重用 jti：`GRANT_REPLAYED`。
- 不同 `request_id`、重用 `intent_hash`：`INTENT_IN_USE`；重用 nonce：`NONCE_IN_USE`。

一個 `intent_hash` 一生只有一筆 attempt；要重付必須是新的請購（新 nonce、新 `intent_hash`）。

### 5.3 狀態與期限

`auth_deadline` 在接受時固定，寫進快照。

| 狀態 | 意思 | 誰推進 | 規則 |
|---|---|---|---|
| （拒絕） | 第 4.3 節任一項不符 | Signer | 不寫入，不保留任何東西 |
| `accepted` | 已驗證；jti、`intent_hash`、nonce 與累計用量在同一個寫入交易保留 | Signer | 能不能進入 `signed`，在最後的寫入交易裡判定（下方「最後判定」） |
| `signed` | 簽章已算出，並以條件式更新（`WHERE state='accepted' AND ? < auth_deadline`）提交 | Signer | 終態；授權在 `valid_before` 前都可能被提交，**不能撤回** |
| `abandoned` | Host 在簽署提交前放棄 | Host 呼叫，Signer 以條件式更新 | 終態；不再計入累計用量（由狀態推導）；jti、`intent_hash`、nonce 仍算已用，不重用 |
| `expired_unsigned` | 最後判定時 `now ≥ auth_deadline` 而仍是 `accepted` | Signer（最後判定、恢復或查詢時） | 終態；不再計入累計用量；jti、`intent_hash`、nonce 不重用 |

**最後判定**（R3-a）：要進入 `signed`，必須在一個寫入交易裡完成下列步驟：
1. `BEGIN IMMEDIATE` 取得 SQLite 的寫入鎖後，取一次可信時間 `now`。這個時間不能早於快照裡的 `accepted_at`，否則回 `CLOCK_REGRESSION`，不寫入。
2. 執行 `UPDATE attempts SET state='signed', signature=?, signed_at=? WHERE attempt_id=? AND state='accepted' AND ? < auth_deadline`，最後一個參數就是這個 `now`。
3. 影響 1 列：提交，提交成功後才把簽章回給呼叫者。
4. 影響 0 列：在同一交易重讀這一列。如果仍是 `accepted`（也就是期限已到），執行 `UPDATE … SET state='expired_unsigned', closed_at=? WHERE attempt_id=? AND state='accepted'` 並提交，丟棄剛算出的簽章，回 `ATTEMPT_EXPIRED`；如果已是 `abandoned`、`expired_unsigned` 或 `signed`，回對應的狀態，不交出這次算出的簽章。

這裡判定的是「在取得寫入鎖後取時間的那一刻，授權仍然有效」。它**不**保證磁碟提交、回覆送達或之後的鏈上提交都發生在同一瞬間：提交與回覆會稍晚於 `now`。這個差距由 `auth_deadline ≤ valid_before` 承擔，授權在鏈上仍以 `valid_before` 為準。`now == auth_deadline` 算已到期，不用 `≤` 放行。

例子：grant 在 10:00:30 到期，`valid_before` 是 10:01:00。10:00:20 接受後當機，10:00:40 重啟。這時 `auth_deadline`（10:00:30）已過，結果是 `expired_unsigned`，**不簽**，即使 `valid_before` 還沒到。這是保守規則：未完成的簽署不跨過授權到期。如果要改成「接受時就固定授權、之後可以跨過授權到期完成」，那是語意變更，列為待決（決策 6，推薦不要）。

### 5.4 三個中斷點

前提有三個：
- 簽章採 RFC 6979 決定性 ECDSA，s 正規化為 low-S，編碼為 65 bytes（r ‖ s ‖ v，v＝27／28）；
- 金鑰以快照中的 `address` 選用；
- 簽章只在 `signed` 提交後才離開 Signer。

| 中斷點 | 從哪裡讀 | 是否再簽 | 回傳什麼 | 何時持久化 |
|---|---|---|---|---|
| 已接受，還沒簽 | 快照（typed data、`address`、`auth_deadline`） | 用快照中的 typed digest 與 `address` 對應的金鑰簽；載入的金鑰地址不等於快照時回 `KEY_VERSION_MISMATCH`，維持 `accepted`，交管理者處理。是否能保存為 `signed`，由第 5.3 節的最後判定決定 | 最後判定通過：`signed` 與授權。已到期：`expired_unsigned`，沒有授權 | 最後判定的寫入交易提交成功後才回傳 |
| 已簽，結果未保存 | 同上（狀態仍是 `accepted`） | 同上；決定性簽章得到完全相同的位元組。前一次的簽章沒有離開 process | 同上 | 同上 |
| 結果已保存，回覆遺失 | 快照與已保存的 `signature` | 否 | 同一份 `signed` 授權（同一個 `request_id` 重送，或查詢） | 已在前一次提交 |

Host 這一側：送出前先把 `request_id` 寫進自己的 `payment_attempts`（`requested`，第 6.4 節）。Host 重啟後一律用同一個 `request_id` 查詢或重送，不換新 ID。

### 5.5 新請求、重送與查詢

- **新請求**（沒看過的 `request_id`）：完整執行第 4.3 節的檢查；通過就接受。
- **重送**（同一個 `request_id`、相同指紋）：不重新驗證授權期限，也不建立新 attempt。依快照與狀態處理：`accepted` 照第 5.4 節完成或過期；`signed` 回已保存的授權；`abandoned`、`expired_unsigned` 回原狀態。
- **查詢**（`GET`）：同上，但不推進簽署；`accepted` 而且 `auth_deadline` 已過時，改成 `expired_unsigned`。

查詢與重送仍要通過呼叫者檢查（第 3.1 節第二列）。已保存的 `signed` 授權可以在這個範圍內**重複回給呼叫者**：要求是「一筆 attempt 只有一份授權，保存後才回傳」，不是「只回傳一次」。已過期的 grant 不會讓一筆已保存的 `signed` 結果查不到，也不會因此重新簽署。

### 5.6 唯一性、併發與 consumer 範圍

- **唯一性**：見第 5.2 節。累計用量檢查與保留在同一個寫入交易（`BEGIN IMMEDIATE`），所以兩筆並行的請求不會一起超過 `total_cap`。
- **併發**（R3-b）：
  - **錢包鎖**是 Signer 程序內每個錢包一把的互斥鎖，只序列化同一錢包的**接受與簽署工作**：新請求或恢復時取得，依序做第 4.3 節的檢查、接受的寫入交易、計算簽章、第 5.3 節最後判定的寫入交易，全部完成（或放棄）後釋放。取不到時有界等待，之後回 `SIGNER_BUSY`。不同錢包並行。
  - **abandon 與查詢不取錢包鎖**：abandon 只執行一個條件式寫入交易，不會被「計算簽章」這段較長的區段擋住；查詢不推進簽署；若需記錄到期，使用受狀態條件保護的短寫入交易，不取得錢包鎖。
  - **SQLite 寫入鎖**（`BEGIN IMMEDIATE`）讓接受、最後判定與 abandon 三種寫入交易彼此序列化。勝負由同一列的條件式更新決定，不由錢包鎖決定。
  - 正確性不依賴錢包鎖：鎖只避免同一錢包重複計算簽章，以及累計用量檢查的不必要競爭。即使沒有錢包鎖，累計檢查與保留也在同一個寫入交易裡。
- **consumer 範圍**：A、B 的請求都來自同一個 Host uid，所以 Signer **不能**以呼叫者分辨 consumer。
  - 簽署：consumer 的界線由授權依據執行。常設委託與核准者登記都綁定 consumer 與錢包；A 的委託或核准不能簽 B 的錢包，這一點不依賴 Host。
  - 查詢與 abandon：Signer 只檢查呼叫者是 Host。哪位 consumer 能看到或放棄哪一筆，由 **Host** 在自己的 API（viewer、模擬核准者能力與 Store 的 consumer 綁定）執行，所以依賴 Host 可信。
  - 隨機的 attempt ID 只防誤用，**不是**授權。

### 5.7 取消、到期與競爭

| 時點 | 取消 | 授權到期（grant 或委託） | `valid_before` 到期 | 報價改變 |
|---|---|---|---|---|
| 接受前 | 權威在 Host（核准會話撤回、task 取消）。Signer **沒有**可驗證的撤回清單；正常的 Host 不送出。若 grant 尚未到期而被送來，Signer 仍會接受。曝險以 grant 很短的期限（≤ 核准會話期限 ≤ `valid_before`）為界 | 拒絕（`AUTHORIZATION_EXPIRED`／`DELEGATION_EXPIRED`），不保留 | 拒絕（`WINDOW_INVALID`） | 報價由 Host 在送出前重新取得；變了就重新請購與核准（Signer 不連網） |
| 已接受、未簽 | Host 呼叫 abandon | 恢復或查詢時改成 `expired_unsigned` | 同左（`auth_deadline` 不晚於它） | 已接受的不再改 |
| 已簽 | **不能撤回**；Host 記為待對帳 | 不影響已簽的授權 | 授權自然失效；有沒有被提交要靠 P2 的鏈上對帳 | 不重簽 |

**abandon 與簽署的競爭**（R3-b）：
- **簽署**：持錢包鎖算出簽章，再進入第 5.3 節的最後判定寫入交易。
- **abandon**：不取錢包鎖，在自己的寫入交易裡執行 `UPDATE … SET state='abandoned', closed_at=? WHERE attempt_id=? AND state='accepted'`。

兩者都對同一列做條件式更新，SQLite 的寫入鎖讓它們一前一後：

| 先提交的一方 | 後到的一方得到 |
|---|---|
| abandon（例如落在簽章算出之後、最後判定交易之前） | 最後判定影響 0 列，重讀為 `abandoned`：丟棄簽章，回 `ATTEMPT_ABANDONED`，沒有授權離開 Signer |
| 最後判定（`signed`） | abandon 影響 0 列，回 `ATTEMPT_NOT_ACCEPTED`，附 `state=signed`：明確表示**無法放棄**，不能說成已撤回 |
| 期限已到（最後判定或查詢先寫入 `expired_unsigned`） | abandon 回 `ATTEMPT_NOT_ACCEPTED`，附 `state=expired_unsigned`。取消競爭不會略過期限 |

累計用量由狀態推導，不論哪一方先成功都只看最後狀態，不會重複釋放。回覆過 `abandoned` 的 attempt 絕不會再交出簽章；`signed` 從不被描述為已撤回。

EIP-3009 的鏈上 `cancelAuthorization` 要由付款人送交易，屬 P2 以後的決定。

### 5.8 對商業流程第 11 節的修訂

| 第 11 節原文 | 現行推薦 |
|---|---|
| 接受時原子消耗 jti 並建立 PaymentAttempt（`not_started`） | 不變；Signer 裡的狀態叫 `accepted`。保留項目擴大為 jti、`intent_hash`、nonce 與累計用量，並保存不可變快照 |
| 正常重送：同 jti、同 attempt 回原 attempt | 以 `request_id`＋指紋判定重送；同 `request_id`、不同內容拒絕 |
| 崩潰恢復：jti 已消耗未簽 → 停在 `not_started`，重啟後繼續或交人工，不重簽第二次 | 只讀快照；用同一個 digest 與同一把金鑰計算，是否保存為 `signed` 由最後判定（寫入交易內取時間，`now < auth_deadline`）決定，否則 `expired_unsigned`；決定性簽章、保存後才回傳，所以同一 attempt 只有一份授權內容 |
| （沒有取消規則） | 已接受未簽可 abandon（不取錢包鎖）；與最後判定以同一列的條件式交易決勝；已簽不能撤回 |
| 拒絕：simulated、過期等不消耗 jti | 不變 |
| AuthorizationGrant 的方法 `webauthn`／`policy`／`simulated`；Policy gate 發 policy grant | 自動放行改為 Signer 端常設委託（沒有 Host 簽發的 policy grant）；`webauthn` 在 A2；`simulated` 永遠拒絕 |

`commercial-transaction-flow.md` 第 11 節已加註指向本表。

## 6 R4 正式授權與 schema 接線（Host）

### 6.1 三類授權的資料關係

| 類別 | 來源依據 | Host 資料 | 必填 | 唯一性與外鍵 |
|---|---|---|---|---|
| `simulated`（P1-a） | 模擬核准者 | `grants` 一列，連到核准會話 | `approval_session_id`、`jti`、`body`（未簽 grant） | 現行：`jti` PK、`approval_session_id NOT NULL UNIQUE REFERENCES approval_sessions`、方法 CHECK `simulated`；**永遠不能出現在付款流程** |
| `webauthn`（**A2，本片不建立**） | 本人 WebAuthn assertion | A2 時：`grants` 一列連到核准會話，`payment_attempts` 以 A2 新增的 `grant_jti` 欄位引用它 | 同上，`body` 是 assertion 包 | 全部在 A2 的遷移與驗收裡加入；P1-b 的 schema 4 沒有任何 WebAuthn 欄位或允許值，**未實作、未驗收** |
| `delegation`（常設委託，P1-b） | Signer 設定裡的委託 | **沒有** `grants` 列；`payment_attempts` 一列帶 `delegation_ref`（委託 ID＋版本） | `delegation_ref` | 不建立、不偽造核准會話 |

### 6.2 現行程式要改的地方（本輪只寫設計，不改程式）

| 位置 | 現行規則 | P1-b | A2 |
|---|---|---|---|
| `store.go:428` `grants` DDL | 方法 CHECK＝`simulated`；會話必填 | 不改 | CHECK 放寬為 `simulated`／`webauthn` |
| `pay.go:433`、`:505` `ResolveApproval` 寫 grant | 只寫 simulated | 不改（自動放行不經過它） | 本人核准寫 `webauthn` grant |
| `pay.go:116` `loadSessions` 讀 grant | `grants JOIN approval_sessions` | 不改；另讀 `payment_attempts` 給檢視用 | 依方法解碼 |
| `pay.go:606` `verifyPay` | approved 會話恰一筆 grant 與 `GRANT_ISSUED` | 增加：每列 `payment_attempts` 的請購屬於 v1 task、`intent_hash` 與請購相符、方法是 `delegation`、`delegation_ref` 非空 | 加入 `grant_jti` 後：引用的 grant 必須是 `webauthn` 且會話已 approved |
| `pay.go:682` `verifyPayTables` | grant 的會話必須 approved | 不必改：schema 4 的 `payment_attempts` 沒有引用 grant 的欄位，結構上不可能引用 simulated grant | 加入 `grant_jti` 後：不能引用 simulated grant |
| `store.go:495` `upgradeFrom` | 1 或 2 → 3 | 加 schema 4（新增 `payment_attempts`），沿用驗證、備份、單一交易、再驗證；1／2／3 都能升到 4 | — |
| `paycontract` | `AuthorizationGrant` 只有 simulated | 新增 Signer 請求與回覆型別（`delegation` 授權、`SignRequest`、`SignedAuthorization`）；simulated 型別不變，兩者不共用型別；**沒有** webauthn 型別 | 新增 `webauthn` 授權型別與 challenge 向量 |
| Host 的 Signer client | — | 只會為 `delegation` 建立 `payment_attempts` 與送簽；其他方法在 Host 就拒絕，不寫入 | 加入 `webauthn` |

### 6.3 舊資料

- schema 3 的 simulated grant 遷移後原樣保留；復原檢查照舊。
- schema 4 的 `payment_attempts` 只接受 `delegation`，也沒有引用 grant 的欄位，所以 simulated 資料遷移後取得不到付款能力，結構上也無法被付款引用。
- 不符合的資料（方法不是 `delegation`、`delegation_ref` 為空、請購不屬於 v1 task、`intent_hash` 與請購不符）由 CHECK 或復原檢查拒絕開啟，不修補。
- M1 的 task、證據與檢查器不變；M1 檢查器照舊拒絕 v1 task。

### 6.4 Host 的 `payment_attempts`（鏡像）

```sql
payment_attempts(
  request_id         TEXT PRIMARY KEY,          -- Host 在呼叫 Signer 前產生並先寫入
  task_id, purchase_order_id                    -- 外鍵到 purchases
  intent_hash        TEXT NOT NULL UNIQUE,
  method             TEXT NOT NULL CHECK (method = 'delegation'),   -- P1-b 只有常設委託；webauthn 與 grant_jti 由 A2 的遷移加入
  delegation_ref     TEXT NOT NULL,
  signer_attempt_id  TEXT UNIQUE,                     -- Signer 回覆後才填，之前可空
  state              TEXT NOT NULL CHECK (state IN ('requested','accepted','signed','abandoned','expired_unsigned','rejected','unknown')),
  error_code TEXT, requested_at INTEGER NOT NULL, updated_at INTEGER NOT NULL
)
```

`webauthn` 不是本片的結構預留：schema 4 不含它的允許值或欄位，A2 以另一次遷移加入（`ADD COLUMN grant_jti … REFERENCES grants`、放寬 CHECK，以及「方法與 grant 必須相符」的檢查），並在 A2 驗收。恢復查詢的鍵是 `request_id`（Signer 的 `GET /v1/attempts?request_id=`）；`signer_attempt_id` 只在有回覆後才有。Host 不保存授權的簽章本身：需要時再向 Signer 查。

## 7 D 本機驗收與正式付款依賴

- **正式入口**：`ReservationVerifier` 的正式實作在 P2 前一律回 `RESERVATION_UNAVAILABLE`，所以 P2 前不會簽出任何授權。`webauthn` 在 A2 前一律拒絕，`simulated` 永遠拒絕。這些是**階段限制**，不是授權邊界的替代品；授權邊界是第 3.1 節第三列。
- **測試替身**：只存在 `_test.go`，在套件內組裝核心；`main` 沒有旗標或環境變數能換掉它；沒有「跳過授權」或「跳過鏈上確認」的開關；中斷點注入沿用 Store 的套件內 hook（正式程式為 nil）。
- **測試材料**：只用公開或一次性的測試金鑰、合成委託、合成 assertion 與合成預留；不注資，不連鏈。這些不能稱為真人 WebAuthn、真實授權或鏈上預留的證據。
- **出口分開**：

| 切片 | 出口 | 不代表 |
|---|---|---|
| P1-b | 隔離的 Signer；常設委託的每筆與累計限制；一次性、快照與恢復；拒絕路徑；以測試金鑰與替身在測試裡簽出正確的 typed data | 付款、結算、交付；本人核准；鏈上預留 |
| A2 | `webauthn` 授權能被 Signer 驗證 | 付款 |
| Z1／Z2 | 鏈上預留與 `intent_hash` 事件 | 付款 |
| P2 | testnet：預留 → 簽署 → verify／settle → 對帳 → 交付 | 正式環境、真錢 |

## 8 定向驗收計畫（只設計，不執行）

實作核准後沿用 `go run ./cmd/m1-check --plan …` 的進度工具。反向檢查只以預期斷言命中計算。

| 項目 | 輸入／前置 | 可信依據 | 預期 | 防護位置 | 拿掉防護後應命中的斷言 |
|---|---|---|---|---|---|
| R1 偽造自動放行 | 同 uid 的測試呼叫者，以不存在的委託、改過版本、改大 `per_tx_max` 的內容送簽 | Signer 設定 | `DELEGATION_UNKNOWN`／`DELEGATION_VERSION`；改大的值無效 | 委託查表與版本比對 | 「unknown delegation signed」 |
| R1 新 nonce 多筆 | 同一委託下連續送多筆新請購，每筆 ≤ `per_tx_max` | Signer DB 的累計用量 | `total_cap` 以內接受，超過後 `OVER_TOTAL_CAP`；**範圍內的都會被簽**（依賴可信 Host 的部分照實記） | 累計檢查與保留同一交易 | 「total cap exceeded」 |
| R1 並行超額 | 兩個 goroutine 同時送，合計超過 `total_cap` | 同上 | 恰一筆成功 | `BEGIN IMMEDIATE` 內的檢查 | 「both accepted over the cap」 |
| R1 跨 consumer 簽署 | A 的委託送 B 錢包的請購 | 委託綁 consumer 與錢包 | `CONSUMER_MISMATCH` | 第 4.3 節第 2、3 項 | 「A's delegation signed B's wallet」 |
| R1 跨 consumer 查詢與放棄 | Host 的 viewer／模擬能力為 B 時，查或放棄 A 的 attempt | Host 的 consumer 綁定 | Host API 回 404；Signer 本身只認 Host uid（照實記：此項依賴 Host） | Host API | 「B read A's attempt」 |
| R1 Host 不持簽發鑰 | 檢查 Host 的設定與檔案 | — | 沒有任何簽發私鑰 | 設計 | — |
| R2 接受後中斷 | hook 在接受提交後中止，重開 | 快照 | `now < auth_deadline` 時完成 `signed`，否則 `expired_unsigned` | 恢復只讀快照 | 「recovery used the request, not the snapshot」 |
| R2 簽後未保存 | hook 在簽完、提交前中止 | 快照 | 重開後簽章位元組與未中斷時相同 | 決定性簽章、提交後才回傳 | 「signature differs after recovery」 |
| R2 保存後回覆遺失 | 同一 `request_id` 重送與 `GET` | 已保存的 `signature` | 同一份授權，attempt 數不變 | 重送走查回 | 「a second attempt for the same request」 |
| R2 同 ID 不同內容 | 同一 `request_id` 改一欄 | 指紋 | `REQUEST_ID_CONFLICT` | 指紋比對 | 「conflicting request accepted」 |
| R2 金鑰版本不符 | 快照的 `address` 與載入的金鑰不同 | 快照 | `KEY_VERSION_MISMATCH`，維持 `accepted` | 以地址選鑰 | 「signed with another key」 |
| R2 過期後查回 | `signed` 之後 grant 已過期，`GET` | 已保存結果 | 回同一份授權，不重新簽 | 查詢不重驗期限 | 「saved result refused after expiry」 |
| R3 grant 先到期（恢復） | 接受後中斷；重開時 grant 已過、`valid_before` 未過 | `auth_deadline` | `expired_unsigned`，沒有授權 | 最後判定的期限條件 | 「signed after the authorization expired」 |
| R3-a 計算前有效、最後判定前到期 | hook 停在簽章算出後、最後判定交易前；把測試時鐘推到 `≥ auth_deadline` 再放行 | 寫入交易內取的 `now` | `expired_unsigned`，回 `ATTEMPT_EXPIRED`，沒有授權 | 最後判定的 `? < auth_deadline` | 拿掉期限條件 →「signed after the deadline」 |
| R3-a 剛好在期限 | 最後判定時 `now == auth_deadline` | 同上 | 拒絕，`expired_unsigned` | 嚴格 `<` | 改成 `<=` →「signed at the deadline」 |
| R3-a 未到期 | 最後判定時仍 `accepted` 且 `now < auth_deadline` | 同上 | `signed`，提交後回傳授權 | — | （正例） |
| R3-a 保存後才到期 | 已 `signed`，之後過了 `auth_deadline` 再 `GET` 或重送 | 已保存結果 | 回同一份授權，不再簽，不改狀態 | 查詢不重驗期限（R2 規則） | 查詢改成重驗期限 →「saved result refused after expiry」 |
| R3-b abandon 先成功 | hook 讓簽署持錢包鎖停在簽章算出後、最後判定前；此時呼叫 abandon | 條件式更新 | abandon 在有界時間內完成並回 `abandoned`；放行後簽署回 `ATTEMPT_ABANDONED`，沒有授權離開 | abandon 不取錢包鎖；最後判定的 `state='accepted'` | 拿掉最後判定的狀態條件 →「signature returned after abandon」；讓 abandon 取錢包鎖 →「abandon blocked by the signing lock」（測試以有界等待判定，不靠測試逾時） |
| R3-b signed 先成功 | hook 停在最後判定提交後、回覆前；此時呼叫 abandon | 同上 | abandon 回 `ATTEMPT_NOT_ACCEPTED`，附 `state=signed`；簽署照常回傳 | abandon 的 `state='accepted'` | 拿掉 abandon 的狀態條件 →「abandon overwrote signed」 |
| R3-b 期限已到時取消 | `accepted` 已過期，查詢先寫入 `expired_unsigned`，再 abandon | 同上 | abandon 回 `ATTEMPT_NOT_ACCEPTED`，附 `state=expired_unsigned`；累計用量為 0（由狀態推導，不重複釋放） | 同上 | 拿掉 abandon 的狀態條件 →「abandon after expiry」 |
| R3 Host 本地撤回、grant 尚有效 | Host 撤回會話但仍送出（模擬故障的 Host） | 無可驗證撤回 | **被接受**（照實記：取消保證依賴 Host）；曝險以 grant 期限為界 | 設計說明 | — |
| R4 舊 simulated 遷移與恢復（P1-b 正例） | schema 3 檔（含 simulated grant）升到 4 | 遷移與復原檢查 | simulated 資料原樣保留、照常開啟；沒有 `payment_attempts` 引用它 | `upgradeFrom`、`verifyPay` | 「migration changed simulated data」 |
| R4 常設委託路徑（P1-b 正例） | 自動放行一筆 | 委託參照 | 沒有核准會話、沒有 grant；`payment_attempts` 一列，方法 `delegation`、帶 `delegation_ref` | 第 6.1 節、schema 4 | 「session invented for a delegation payment」 |
| R4 方法與關係不符（P1-b 反例） | 寫入方法不是 `delegation`、或 `delegation_ref` 為空、或請購不屬於 v1 task 的 `payment_attempts` | schema 4 的 CHECK 與 `verifyPay` | 寫入被 CHECK 拒絕；直接改檔的列由復原檢查拒絕開啟 | CHECK、`verifyPay` | 放寬 CHECK →「non-delegation payment attempt accepted」 |
| R4 WebAuthn 尚未開放（P1-b 反例） | Host client 收到 `webauthn` 或 `simulated` 授權；或直接送給 Signer | Host client 的方法限制；Signer 的方法規則 | Host 拒絕、不寫入；Signer 回 `HUMAN_APPROVAL_UNAVAILABLE`／`GRANT_METHOD_NOT_ACCEPTED`；合成的 assertion 也不能繞過 schema 4 | Host client、Signer | 讓 Host client 接受 `webauthn` →「webauthn payment attempt written」 |
| R4（A2，**未實作、未驗收**） | WebAuthn grant 的合法保存與復原、本人核准正例、`grant_jti` 引用的 grant 必須是 `webauthn` 且會話已 approved | A2 的遷移與檢查 | 在 A2 定義與執行 | A2 | — |

另外沿用初版的群組：正例與 typed digest 凍結向量；綁定每欄改一次；預留（正式組裝回 `RESERVATION_UNAVAILABLE`）；介面（非 `callers` 的 uid、超大請求、逾時、沒有其他路由）；不外洩（log、錯誤與證據不含私鑰、密語、完整簽章、assertion 原文）；P1-a 不回歸（`go test ./internal/store/ ./internal/paycontract/ ./cmd/demo/ -run 'Pay|P1a'`、`node --test web/test/p1a.test.mjs`）；OS 隔離（**需系統設定核准後**，`scripts/verify/p1b-isolation.sh`）。估時合計約 20–30 分鐘。不預設：完整容器、全部突變、M1 產包、瀏覽器、Unity、任何鏈或網路。

## 9 決策表

| # | 項目 | 推薦 | 影響 | 現在要 Aaron 決定？ | 最晚 |
|---|---|---|---|---|---|
| 1 | P1-b 程式實作 | 照本設計實作 Signer 核心、socket 服務、Host client、`payment_attempts`（schema 4）；只用測試金鑰與替身；不接進展示流程 | 新套件、schema 4 | **是** | 實作派工前 |
| 2 | 新依賴 | `github.com/decred/dcrd/dcrec/secp256k1/v4` 與 `golang.org/x/crypto`（`sha3`、`scrypt`）；不引入 go-ethereum | 兩個依賴，版本釘選 | **是**（與 1 一起） | 實作派工前 |
| 3 | **自動放行的信任邊界** | 常設委託由管理者寫在 Signer 設定。Aaron 接受：Host uid 被控制時，最多可在常設委託的範圍內付款（允許的商家與資產、每筆上限、累計上限）；超出範圍、偽造本人核准、讀金鑰都不行 | 這是付款風險的接受，不是實作核准；設計 review 也不代替它 | **是**（明確接受或要求更嚴：例如 P2 起才允許自動放行，或自動放行一律改走本人核准） | 實作派工前 |
| 4 | 系統設定 | 建立 `apa-signer` 使用者、群組與 socket 目錄 | OS 隔離驗收需要；程式可以先寫 | **是，但與 1 分開** | OS 隔離驗收前 |
| 5 | A／B 本機 demo 付款金鑰的建立與解鎖 | 之後另行核准；P1-b 測試只用測試金鑰 | — | 否 | P2 申請時 |
| 6 | 未簽 attempt 能否跨過授權到期完成 | 不能（保守規則，第 5.3 節） | 若要改，是語意變更 | 否（推薦即可） | 實作前 |
| 7 | 金鑰解鎖方式 | 加密金鑰檔＋終端機密語 | 每次重啟要輸入 | 否 | 實作時 |

2026-10-12：Aaron 核准決策 1、2、3（「這三件事我都核准，請提供派工筆記」）；決策 4、5 仍待另行核准。

不重問：EOA＋EIP-3009（ADR Accepted）、P1-a 已限定結案、`simulated` 永遠不能取得付款能力。金鑰建立與解鎖、實際簽署、系統設定與外部交易的核准，都與程式實作核准分開。

## 10 官方依據與證據歸屬

- **decred secp256k1 的 `Sign`**：Codex 前輪已核對官方 API 文件（[pkg.go.dev ecdsa](https://pkg.go.dev/github.com/decred/dcrd/dcrec/secp256k1/v4/ecdsa)），文件說明它是決定性簽章。這是官方介面依據；將採用的版本、實際 API、Ethereum 的 65 bytes 與 low-S 編碼、凍結測試向量，都**尚未**在實作中驗證。若決定性在實作時不成立，第 5.4 節要改成「簽章前先保存待簽狀態、只回傳已保存的簽章」並重新 review。
- **macOS 的對端 uid**：Codex 前輪已核對 Go 官方原始碼（[golang/sys unix/syscall_darwin.go](https://github.com/golang/sys/blob/master/unix/syscall_darwin.go)）提供 `GetsockoptXucred`，註解列出 `LOCAL_PEERCRED`。這是官方介面依據；macOS 上的部署與帳號隔離實測**尚未**執行。
- 依賴版本：本版沒有改變推薦的依賴，所以沒有再查其他來源。
- **歸屬**：
  - Codex 對 `3314e7c`：Git、文件、原始碼與官方公開資料的唯讀核對；沒有跑測試、執行真實 CLI、簽署、改產品或驗證 OS 隔離，repo 裡沒有對應的測試 log。
  - Codex 對 `19fe1f7`：限定安全設計複核，唯讀核對 Git、設計與相關原始碼，確認該提交只改四份文件；沒有執行測試、CLI、簽署或系統隔離驗證，沒有修改產品或設計文件；R1、R2 關閉（設計層面），R3、R4 為一致性缺口。
  - Claude Code 對 `19fe1f7` 的 R3、R4 補正（交付於 `e725c44`）：文件修訂與來源核對。
  - Codex 對 `e725c44`：限定設計複核，唯讀核對 Git、文件差異、狀態規則與驗收表，確認該提交只改四份文件；沒有跑測試、建置、CLI、簽署或 OS 隔離驗證，repo 裡沒有對應的測試 log。結論：R3、R4 關閉，R1、R2 維持關閉；設計通過，不是程式、OS 隔離或付款驗收。
  - 實作輪（`commercial-p1b`）：所有執行結果歸 Claude Code，待 Codex 技術 review；Codex 的設計複核不寫成程式驗收或獨立重跑。
