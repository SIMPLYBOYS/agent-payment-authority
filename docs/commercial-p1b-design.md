# P1-b：金鑰庫與獨立 Signer（最小設計）

> 2026-10-12，Claude Code。依 vault〈Claude-Code-P1-b金鑰庫與獨立Signer最小設計派工-1fb6225-20261012〉；Aaron 原話「我批准進行P1-b「金鑰庫與獨立 Signer」最小設計」。基準 `1fb6225`（分支 `commercial-p1a`）。
> **狀態：P1-b 金鑰庫與獨立 Signer 最小設計已核准；設計交付後待 Codex review 與 Aaron 的實作決策。尚未核准實作、建立金鑰、簽署或交易。**
> 本輪只讀程式與文件、寫設計：沒有改產品、契約、schema 或 Signer 樁，沒有寫原型、跑測試或建置，沒有產生或讀取任何金鑰，沒有連 RPC、faucet 或 facilitator。這是 Claude Code 的來源核對與設計交付，待 Codex review。

依據：[付款帳戶 ADR](adr/payment-account-model.md)（Accepted：每位 consumer 一個 EOA＋EIP-3009，由隔離的 Signer 保管金鑰；Signer 只在同時有一次性 grant 與鏈上已驗的 `intent_hash` 時才簽）、[commercial-transaction-flow.md](commercial-transaction-flow.md) 第 3、10、11 節、[commercial-demo-roadmap.md](commercial-demo-roadmap.md) 的 P1／A2／P2 切片（第 5.3 節），以及下列現行程式。沒有查外部資料：EIP-3009 的 typed-data 結構與 x402 `exact`／`eip3009` 路徑在本 repo 的 ADR 與相容性盤點（9/30）已有記錄，本設計沒有超出它們的協定問題。以下兩點是依既有知識寫的、本輪未查證，實作時要先以測試確認：所選 secp256k1 函式庫的簽章為 RFC 6979 決定性（第 5.3 節依賴它）；macOS 上以 `golang.org/x/sys/unix` 讀取 Unix socket 對端 uid（`LOCAL_PEERCRED`）的作法。

## 1 現況與差距

| 現況 | 程式位置 | 對 P1-b 的意義 |
|---|---|---|
| 正式契約 `apa.pay.v1`：業務意圖＋`PaymentBinding{method: eip3009, chain_id, asset_contract, from, pay_to, amount, valid_after, valid_before, nonce}` 一起算 `intent_hash` | `internal/paycontract/paycontract.go:87`、`:208` | Signer 可以自己重算；付款欄位只在綁定裡一份 |
| EIP-3009 nonce 由 task 與商品導出（同一 task、同一商品永遠同一個 nonce） | `internal/toolbridge/shop.go:276` | 同一筆請購不會產生第二個 nonce；放棄後要重付必須是新的請購（新 nonce） |
| 可信資產帶 EIP-712 名稱與版本；報價 `extra` 也帶 `name`／`version` | `internal/contracts/registry.go:29`、`internal/merchant/merchant.go` | typed-data domain 的來源是可信登記，報價只能與它相符 |
| `AuthorizationGrant` 未簽章，`Validate` 只接受 `simulated`；`jti` 由核准會話導出 | `internal/paycontract/grant.go:75`、`:92–99`、`:43` | 不能只把方法改成 `webauthn`／`policy` 就當正式來源：沒有完整性、沒有可驗證的發證者 |
| Host 的 `grants` 表由 CHECK 限定 `simulated` | `internal/store/store.go:428–432` | 正式 grant 需要 schema 4（第 5 節） |
| grant 的到期＝付款綁定的 `valid_before`；會話期限由決策時間、terms 與 `valid_before` 重算 | `internal/store/pay.go:497`、`:145` | 沿用；Signer 也以 `valid_before` 為上限 |
| Signer 樁：不持鑰、不存狀態，模擬 grant 一律 `GRANT_METHOD_NOT_ACCEPTED`，其他一律 `SIGNER_NOT_ENABLED` | `internal/signer/signer.go:17` | P1-b 以真正的 Signer 取代；`simulated` 的拒絕規則照舊 |
| `go.mod` 只有 SQLite（modernc）；沒有 secp256k1 或 Keccak | `go.mod` | 實作需要新依賴（決策 2） |

**差距**：沒有付款金鑰的保管與隔離；沒有能被 Signer 驗證的 grant；沒有一次性消耗的權威紀錄；沒有付款嘗試（attempt）的狀態與恢復；沒有鏈上預留的驗證（屬 Z2／P2）。

## 2 推薦方案（一句話）

**Signer 是一支獨立的 Go 程式，以專用的 macOS 一般使用者 `apa-signer` 執行，獨占付款金鑰（加密金鑰檔，啟動時在終端機輸入密語解鎖）、自己的 SQLite（jti、attempt、nonce）與自己的可信設定；Host 只能經由 Unix socket、以 OS 回報的呼叫者 uid 通過認證後，送「grant＋完整 v1 請購」請它簽一筆 EIP-3009 授權。Signer 自己重算並核對一切，限額只認自己的設定；鏈上預留驗證未完成前（P2 之前），正式入口一律拒絕。**

選這個方案的理由：它是本機 demo 能做到「Agent 與 Host 都讀不到付款私鑰」的最低成本作法（一個系統帳號，不需要容器、keychain 或服務安裝）；保管後端（金鑰檔）放在一個小介面後面，之後可換 keychain、HSM 或雲端 KMS，不改 Signer 的驗證與狀態規則。

## 3 A 金鑰與程序隔離

### 3.1 信任邊界與資料流

| 元件 | 執行身分 | 持有 | 能做 | 不能做 |
|---|---|---|---|---|
| Agent（Claude CLI）與 MCP bridge | Aaron 的使用者（Host 啟動） | 這一場的 MCP 能力 | 三個業務工具 | 讀付款金鑰（OS 權限擋）；連 Signer 也拿不到簽章（沒有有效 grant 與預留） |
| Go Host（含 Store、Policy、Approval 元件） | Aaron 的使用者 | Host DB、demo token、**policy grant 的簽發金鑰**（Ed25519，`0600`）、（A2 後）approval grant 的簽發金鑰 | 組請購、裁決、發 grant、向 Signer 請求簽署、保存 attempt 的鏡像 | 讀付款金鑰；讓 Signer 超出 Signer 自己的限額 |
| Signer | `apa-signer`（專用一般使用者，非管理員） | 付款金鑰（記憶體中解鎖）、Signer DB、可信設定（錢包、資產、限額、簽發者公鑰、核准者公鑰） | 驗證後簽一筆 EIP-3009 `TransferWithAuthorization` | 簽任意位元組或任意交易；送交易；連網 |
| 可信管理者（Aaron，本機管理員） | sudo | 一切 | 建帳號、改設定、解鎖 | — 本設計**不**防禦本機管理員 |

資料流：請購 → Host 裁決 → （Allow）Host 發 policy grant；（Ask）A2 之後由本人 WebAuthn 核准產生 human grant → Host 把 grant＋v1 請購＋預留參照送給 Signer → Signer 驗證、消耗 jti、簽、保存 → 回傳 EIP-3009 授權給 Host → （P2）買方帶 `PAYMENT-SIGNATURE` 重送給商店。

### 3.2 三種金鑰與憑證

| 項目 | 誰持有 | 存放 | 解鎖／取得 | 重啟 |
|---|---|---|---|---|
| 付款私鑰（每個錢包一把 secp256k1） | 只有 Signer | `apa-signer` 家目錄下的金鑰庫目錄（`0700`，擁有者 `apa-signer`），每把一個加密檔（scrypt＋AES-256-GCM；檔名是 `wallet_id`，內容不含明文） | Signer 啟動時在自己的終端機（`/dev/tty`，與配對碼同一作法）讀密語，解密到記憶體；密語不寫入檔案、環境變數、參數或 log | 重啟要再輸入一次密語；沒有自動解鎖 |
| grant 簽發金鑰（Ed25519：`policy-issuer`；A2 後另一把 `approval-issuer`） | Host | Host 的私有目錄（`0700`）下 `0600` 的檔 | Host 啟動時讀 | 同上；Signer 設定裡只放這些金鑰的**公鑰**與 kid |
| 呼叫 Signer 的憑證 | 不另發秘密 | — | Signer 以 socket 的對端 uid（macOS `LOCAL_PEERCRED`）判斷呼叫者，只接受設定中 Host 使用者的 uid | — |

**不能進入 log、錯誤、回覆以外的地方或證據包的**：付款私鑰與密語（任何形式）；完整的 EIP-3009 簽章（它在 `valid_before` 前就是一張可用的付款授權，只回傳給呼叫者一次，證據只記它的 hash）；grant 簽發私鑰；WebAuthn assertion 原文（證據只記 hash 與驗證結果）。可以記：attempt ID、jti、`intent_hash`、typed-data digest、錢包 ID、固定錯誤碼、時間。

### 3.3 為什麼要另一個 OS 使用者

只是另開一個 process、換環境變數，或在同一個使用者下放一個 `0700` 目錄，**都不算私鑰隔離**：Agent 與 Host 以 Aaron 的身分執行，同一使用者的任何程式都讀得到該目錄。專用使用者 `apa-signer` 讓檔案權限由核心執行：Aaron 的程式（Host、CLI、bridge）讀不到金鑰庫目錄，也無法附掛（attach）到 `apa-signer` 的 process。

需要的系統設定（**列為之後另行核准，本輪不執行**）：建立一般使用者 `apa-signer` 與群組 `apa-signer-clients`（Aaron 加入）；建立 socket 目錄（`apa-signer:apa-signer-clients`，`0750`；socket `0660`）。不需要 keychain、容器、launchd 或其他服務安裝（launchd 常駐可以之後再加）。

**剩下的風險**（照實寫）：
- **本機管理員**（Aaron 有 sudo）可以讀任何東西；本設計不防禦它，也不防禦 root 層的惡意程式。
- **與 Host 同使用者的程式**讀得到 Host 的 policy 簽發金鑰，能偽造 policy grant。後果有界：Signer 只認自己的 `allow_max`、商家、資產與錢包設定（grant 自報的值只能更嚴，不能放寬）；每個 nonce 只簽一次；P2 之後還要有鏈上預留（task 預算）。這類程式**不能**偽造 human grant，因為 A2 之後 human grant 必須附 WebAuthn assertion，Signer 用自己登記的核准者公鑰驗證。
- Agent 與 Host 同一個 uid，所以「Agent 連不上 Signer socket」靠的是 CLI 沒有任何內建工具（Gate 檢查），不是 OS 權限；即使連上，沒有有效 grant 也拿不到簽章。
- 記憶體中的付款金鑰：`apa-signer` 被攻破時，該錢包的資金全部暴露（ADR 的 T3）。

### 3.4 consumer、錢包與金鑰的對應

由 Signer 的可信設定決定（`apa-signer` 擁有、Aaron 以管理者身分維護），請求不能指定檔案路徑或金鑰：

```text
wallets:   wallet_id → { consumer_id, chain_id, address, key: <wallet_id>.key }
assets:    (chain_id, asset_contract) → { eip712_name, eip712_version, decimals }
limits:    consumer_id → { asset, merchants[], allow_max, signer_max }   # 每筆上限；signer_max 是 human 路徑的硬上限
issuers:   kid → { method: policy | webauthn, public_key }
approvers: (A2) consumer_id → [ { credential_id, public_key } ]
callers:   [ host_uid ]
```

Signer 啟動時核對：金鑰檔推出的地址＝設定的 `address`；同一地址不屬於兩個 consumer。錢包數不設上限（設定裡的列數）；帳號總數、Agent 並行數、同時付款數分開：Signer 每個錢包同時只處理一筆（第 5.4 節），不同錢包可以並行，總並行數是部署設定。驗收只用 A、B。

## 4 B Signer 的請求與授權驗證

### 4.1 最小介面

傳輸：HTTP over Unix socket（沿用 Go `net/http`），每個請求 ≤ 64 KiB，處理時間上限 5 秒，同時連線數有上限。只有下列路由：

| 路由 | 用途 |
|---|---|
| `POST /v1/sign` `{request_id, grant, order, reservation}` | 驗證並簽一筆。`grant`：簽章信封（第 4.3 節）；`order`：完整的 `apa.pay.v1` 請購（含報價）；`reservation`：P2 的鏈上預留參照（P2 前忽略並拒絕） |
| `GET /v1/attempts/{attempt_id}`、`GET /v1/attempts?request_id=` | 查一筆 attempt（恢復與重送用） |
| `POST /v1/attempts/{attempt_id}/abandon` | 已接受、尚未簽的 attempt 改成放棄（取消用；已簽的不能撤回） |
| `GET /v1/health` | 已解鎖、設定已載入；不含任何金鑰資訊 |

回覆：`{attempt_id, state, intent_hash, typed_digest, authorization?}`，`authorization` 是 `{from, to, value, valid_after, valid_before, nonce, signature}`，只在 `state=signed` 時出現；錯誤只回固定代碼。沒有「簽這段 bytes」「簽這筆交易」或「匯出公鑰以外的東西」的入口；Signer 唯一會簽的東西，是它自己從已驗證的請購重新組出的 EIP-3009 typed data。

錯誤碼（初版）：`CALLER_NOT_ALLOWED`、`BAD_REQUEST`、`ORDER_INVALID`、`GRANT_INVALID`（格式、版本、簽章）、`GRANT_ISSUER_UNKNOWN`、`GRANT_AUDIENCE`、`GRANT_METHOD_NOT_ACCEPTED`（含 `simulated`）、`HUMAN_APPROVAL_UNAVAILABLE`（A2 前的 `webauthn`）、`GRANT_EXPIRED`、`GRANT_INTENT_MISMATCH`、`WALLET_NOT_REGISTERED`、`CONSUMER_MISMATCH`、`ASSET_NOT_ALLOWED`、`MERCHANT_NOT_ALLOWED`、`OVER_LIMIT`、`WINDOW_INVALID`、`RESERVATION_UNAVAILABLE`、`RESERVATION_MISMATCH`、`GRANT_REPLAYED`、`NONCE_IN_USE`、`REQUEST_ID_CONFLICT`、`ATTEMPT_UNKNOWN`、`SIGNER_LOCKED`、`SIGNER_BUSY`。

### 4.2 兩件事分開：誰能呼叫、這筆是否被授權

- **呼叫者**：socket 對端 uid 在 `callers` 裡（第 3.2 節）。這只代表「可以把請求送進來」；Host 或 operator 的身分**不能**代替 grant。
- **授權**：每一筆都要一份有效 grant，而且 Signer 自己核對它與請購的每個綁定欄位（第 4.4 節）。

### 4.3 正式 grant：信封、發證者與方法

現行 `AuthorizationGrant`（未簽、只有 `simulated`）保留原樣給 P1-a 用；正式 grant 是新的簽章信封，同一個契約版本 `apa.pay.v1`，以新的 `type` 區分：

```text
{ "type": "apa.pay.v1/grant",
  "grant": { jti, intent_hash, consumer_id, wallet_id, task_id,
             authorization_method: "policy" | "webauthn",
             decision: "allow" | "approve",
             policy_version, limits?: { allow_max },        # 只能比 Signer 設定更嚴
             approval_session_id?, assertion?: {...},          # webauthn（A2）
             issuer, audience: "signer", issued_at, expires_at },
  "kid": "<issuer key id>",
  "sig": Ed25519(kid 的私鑰, H("agent-payment-authority/pay/v1/grant" ‖ canonical(grant))) }
```

| 方法 | 誰能發 | 附什麼可驗證的依據 | Signer 怎麼驗 | 何時能用 |
|---|---|---|---|---|
| `simulated` | P1-a 的模擬核准 | 無 | 一律 `GRANT_METHOD_NOT_ACCEPTED` | 永遠不能付款 |
| `policy` | Host 的 Policy gate（Allow 時），`policy-issuer` 金鑰 | 簽章信封；不另附依據 | 驗信封簽章與 kid 的方法＝`policy`；**再用 Signer 自己的 `limits` 重判**：資產、商家、`amount ≤ min(設定的 allow_max, grant.limits.allow_max)` | P1-b 可驗證；能簽出可用授權要等 P2 的預留驗證 |
| `webauthn` | Host 的 Approval 元件（本人核准後），`approval-issuer` 金鑰 | 信封＋WebAuthn assertion，challenge＝`H("agent-payment-authority/pay/v1/approval" ‖ approval_session_id ‖ intent_hash ‖ jti ‖ expires_at)` | 驗信封；以 `approvers` 裡該 consumer 的公鑰驗 assertion 與 challenge；`amount ≤ signer_max` | A2 之前一律 `HUMAN_APPROVAL_UNAVAILABLE`；本設計不宣稱任何人已完成本人核准 |

**型別、版本與資料的最小差異**：
- `paycontract`：新增 `SignedGrant`（信封）與 `GrantBody`；`Validate` 依方法分流；`simulated` 的舊型別不變。新增 golden vectors（信封 digest、challenge）。
- Host Store **schema 4**：`grants.authorization_method` 的 CHECK 放寬為 `simulated`／`policy`／`webauthn`，新增 `issuer_kid`；`body` 存整個信封。新增 `payment_attempts` 鏡像表（`attempt_id, request_id UNIQUE, jti UNIQUE, intent_hash, state, typed_digest, error_code, requested_at, updated_at`）。既有 simulated 列不動；遷移沿用 `--upgrade-db`（先驗證、備份、單一交易、再驗證）。
- 復原檢查新增：非 simulated grant 的信封簽章與 kid、與會話（human）或裁決（policy）的對應。

### 4.4 Signer 自己重算與核對的項目

依序檢查，任一不符就拒絕，而且**不消耗任何東西**：
1. `order` 以 `paycontract` 嚴格解碼並 `Validate`；重算 `intent_hash`，必須等於 `grant.intent_hash`（`GRANT_INTENT_MISMATCH`）。
2. grant：信封格式與簽章、kid 已登記且方法相符、`audience=signer`、`issued_at ≤ now < expires_at`、`expires_at ≤ binding.valid_before`；方法規則見上表。
3. 身分：`grant` 的 consumer／wallet／task＝請購的；`wallets[wallet_id].consumer_id`＝consumer；`binding.from`＝該錢包地址；`binding.chain_id`＝錢包的鏈。
4. 資產與報價：`(chain_id, asset_contract)` 在 `assets` 裡；報價的 `extra.name`／`version` 等於登記的 EIP-712 名稱與版本；報價 hash 與綁定一致（`paycontract.Validate` 已檢查）。
5. 限額：`pay_to` 在該 consumer 的 `merchants`；`asset` 符合；`amount` 依方法比對 `allow_max` 或 `signer_max`。
6. 時間窗：`valid_after < now < valid_before`，且 `valid_before − now ≤ 600` 秒（設定，防止長效授權）。
7. 預留：`reservation` 經預留驗證介面確認鏈上已預留、事件中的 `intent_hash` 相符（P2 前，介面的正式實作一律回 `RESERVATION_UNAVAILABLE`）。
8. nonce：`(chain_id, asset_contract, from, nonce)` 沒有被其他 attempt 用過。

**兩個 hash 分開**：`intent_hash` 是本契約的應用層 hash（SHA-256，網域 `agent-payment-authority/pay/v1/intent`），用來綁定核准、grant、預留與稽核；**實際簽署的是 EIP-712 digest**＝`keccak256(0x19 ‖ 0x01 ‖ domainSeparator ‖ hashStruct(TransferWithAuthorization{from, to, value, validAfter, validBefore, nonce}))`，domain＝`{name, version, chainId, verifyingContract=asset_contract}`，全部取自已驗證的付款綁定與可信資產登記。兩者都由同一份請購重算，attempt 同時保存兩個值，任何人拿到請購都能驗證對應。M1 的 fixture hash 與 v1 的 `intent_hash` 都不會被拿來簽。

## 5 C 一次性消耗與當機恢復

### 5.1 權威資料

一次性消耗的權威在 **Signer DB**（`apa-signer` 擁有的 SQLite，WAL，`synchronous=FULL`），不在 Host：

```text
attempts(attempt_id PK, request_id UNIQUE, jti UNIQUE, intent_hash, wallet_id,
         chain_id, asset_contract, from, nonce, typed_digest, state,
         signature?, created_at, signed_at?, closed_at?, error?)
UNIQUE(chain_id, asset_contract, from, nonce)
```

`jti` 與 `request_id` 的唯一性就是「消耗」：接受時在同一個寫入交易插入這一列。Host 的 `payment_attempts` 只是鏡像，用來在 Host 重啟後用同一個 `request_id` 查回結果。

### 5.2 狀態

| 狀態 | 意思 | 誰推進 | 下一步 |
|---|---|---|---|
| （拒絕） | 第 4.4 節任一項不符 | Signer | 沒有寫入；jti 沒被消耗；回錯誤碼 |
| `accepted` | 已通過驗證，jti、request_id、nonce 已在同一交易保留 | Signer（單一寫入交易） | 簽署 |
| `signed` | 簽章已算出**並已保存** | Signer（保存的寫入交易） | 回覆；之後任何查詢都回同一份 |
| `abandoned` | 已接受、未簽，Host 取消 | Host 呼叫 `abandon`，Signer 只在 `accepted` 時接受 | 終態；jti 與 nonce 仍算已用 |
| `expired_unsigned` | 已接受、未簽，但 `valid_before` 已過 | Signer（恢復或查詢時） | 終態；不簽 |

`signed` 是 Signer 這一側的終態。之後是否送出、是否結算，屬 Host 與 P2 的對帳（`submitted`、`unknown`、`settled`、`not_settled_expired`），不在 Signer。

### 5.3 三個中斷點

前提有兩個：**簽章採 RFC 6979 決定性 ECDSA**，同一把金鑰對同一個 digest 永遠得到同一組位元組；**簽章先保存，再回傳**。

| 中斷點 | 重啟或下一次查詢時 | 為什麼不會出現第二份付款授權 |
|---|---|---|
| 消耗已提交，還沒簽 | attempt 是 `accepted`：若 `now < valid_before`，Signer 依同一份已保存的 digest 完成簽署並保存；否則改成 `expired_unsigned` | 從未有簽章離開 Signer；完成簽署時用的是接受時保存的 digest，不重新取用任何輸入 |
| 已簽但結果沒保存 | 仍是 `accepted`，處理同上 | 決定性簽章，重算得到完全相同的位元組；前一次的簽章沒有離開 process（保存前不回傳） |
| 結果已保存，回覆遺失 | Host 以同一個 `request_id` 重送或查詢，拿到同一份 `signed` 結果 | 同一筆 attempt，同一份簽章 |

Host 這一側：送出前先把 `request_id` 寫進自己的 `payment_attempts`（`requested`）；Host 重啟後一律用同一個 `request_id` 查詢或重送，**不換新的 request_id 重試**。

### 5.4 唯一性與併發

- `jti`：一份 grant 只能被一筆 attempt 消耗。同一個 `request_id`、內容相同 → 回原 attempt；不同 `request_id` 用同一個 `jti` → `GRANT_REPLAYED`；同一個 `request_id`、內容不同 → `REQUEST_ID_CONFLICT`。
- `nonce`：每個 (鏈, 資產, 付款人) 下唯一。nonce 由 task 與商品導出，所以放棄或失敗後要重付，必須是新的請購、新的 nonce、新的核准；**不能**用新的 attempt 冒充同一筆的安全重試。
- 併發：寫入交易以 `BEGIN IMMEDIATE` 序列化；另外**每個錢包一把鎖**，同一錢包同時只有一筆在驗證、簽署與保存之間（`SIGNER_BUSY` 有界等待後回覆），不同錢包並行。
- A／B 隔離：attempt ID 是隨機 128 位元；查詢只回給同一個呼叫者 uid；grant、錢包、請購三者的 consumer 必須一致。A 的 grant 不能消耗 B 的任何東西（consumer 不符時在接受前就拒絕）。

### 5.5 取消、到期、報價改變

| 時點 | 取消 | 到期（grant 或 `valid_before`） | 報價改變 |
|---|---|---|---|
| 接受前 | Host 不送（核准會話已撤回）；送了也會因 grant 或預留不符被拒 | 拒絕（`GRANT_EXPIRED`／`WINDOW_INVALID`），不消耗 | 報價的重新取得由 **Host** 在送出前做（Signer 不連網）；變了就重新請購與核准 |
| 已接受、未簽 | `abandon` → `abandoned` | 恢復時 → `expired_unsigned` | 同上；已接受的不再改 |
| 已簽 | **不能撤回**。授權在 `valid_before` 前仍可被提交；Host 記錄為待對帳（`needs_review`），不宣稱已撤回 | 授權自然失效；有沒有被提交要靠鏈上對帳（P2） | 不重簽；需要時是新的請購 |

EIP-3009 的鏈上 `cancelAuthorization` 要由付款人送交易，屬 P2 以後的決定，本片不設計。

### 5.6 對第 11 節文字的最小修訂

第 11 節的語意（拒絕不消耗、接受時原子消耗並建立 attempt、同一請求回原 attempt、重播拒絕、當機不重簽）全部沿用，只補兩點：

| 第 11 節原文 | 建議補正 |
|---|---|
| 「崩潰恢復：jti 已消耗但還沒簽 → attempt 停在 `not_started`，重啟後依 attempt 繼續或交人工，不重新發 grant、不重簽第二次」 | 「……重啟後，若 `valid_before` 未過，以接受時保存的同一個 digest 完成簽署，否則記為 `expired_unsigned`。簽章採 RFC 6979 決定性 ECDSA，且保存後才回傳，所以同一 attempt 只會有一份簽章內容；『不重簽第二次』的意思是不產生第二份不同的授權。」 |
| （沒有「已接受未簽時取消」） | 補一列：「已接受、未簽的 attempt 可由 Host 放棄（`abandoned`）；已簽的不能撤回，只能等對帳。」 |

狀態名稱：第 11 節的 `not_started` 在 Signer 裡叫 `accepted`（意思相同：已消耗、尚未簽）。

## 6 D 本機驗收與正式付款依賴

- **正式入口的依賴**：Signer 的預留驗證是一個介面（`ReservationVerifier`）。正式程式裡唯一的實作，在 P2 之前對每一筆都回 `RESERVATION_UNAVAILABLE`。所以即使 grant 有效，正式 Signer 在 P2 之前**不會簽出任何授權**。
- **測試替身的隔離**：測試用的預留驗證只存在 `_test.go` 裡，在 Signer 套件內直接組裝核心；`main` 沒有任何旗標或環境變數可以換掉它。同理：沒有「跳過 grant 驗證」或「跳過鏈上確認」的開關；中斷點注入沿用 Store 的作法（套件內的測試 hook，正式程式裡是 nil）。
- **測試材料**：實作核准後，只用公開或一次性的測試金鑰（測試裡產生，或公開的測試向量）、合成 grant 與合成預留；不注資，不連任何鏈。這些不能稱為真人 WebAuthn、真實 Policy 授權或鏈上預留的證據。
- **出口分開**：

| 切片 | 出口 | 不代表 |
|---|---|---|
| P1-b | 隔離的 Signer：驗證、一次性消耗、恢復、拒絕路徑；以測試金鑰與替身在測試裡簽出正確的 EIP-3009 typed data | 付款、結算、交付；正式本人核准；鏈上預留 |
| A2 | human grant 附 WebAuthn assertion，Signer 能驗 | 付款 |
| Z1／Z2 | 鏈上預留與 `intent_hash` 事件 | 付款 |
| P2 | testnet 上：預留 → 簽署 → verify／settle → 對帳 → 交付 | 正式環境、真錢 |

本機簽署通過，只代表 Signer 的規則正確，**不代表**付款、結算或交付成功。

## 7 定向驗收計畫（只設計，不執行）

實作核准後，沿用 `go run ./cmd/m1-check --plan …` 的進度工具。每項關鍵防護都要有一組正例與反例；反向檢查只以預期的斷言命中計算，編譯失敗或 panic 不算抓到。

| 群組 | 入口草案 | 預期 | 估時 |
|---|---|---|---|
| 正例與 A／B | `go test ./internal/signer/ -run 'Sign|Wallet'` | 合法 policy grant＋替身預留 → `signed`；typed digest 等於獨立重算的值（含一組凍結向量）；同輸入兩次得到相同簽章（RFC 6979）；A 的 grant 不能簽 B 的錢包 | 1 分 |
| grant 反例 | `-run Grant` | 假 kid、竄改內容、錯 audience、錯方法、`simulated`、A2 前的 `webauthn`、過期、`expires_at > valid_before`、錯 consumer／wallet／intent → 各自的錯誤碼，且**沒有消耗** | 1 分 |
| 綁定反例 | `-run Binding` | `PaymentBinding` 每一欄改一次（收款人、金額、鏈、資產、付款人、nonce、時間窗）→ 拒絕；報價 `extra` 與登記不符 → 拒絕 | 1 分 |
| 限額與依據 | `-run Limit` | 超過 `allow_max`／`signer_max`；grant 自報較寬的 `allow_max` 無效、較嚴的生效；商家不在清單 | 1 分 |
| 預留 | `-run Reservation` | 正式組裝 → `RESERVATION_UNAVAILABLE`；替身說不符 → `RESERVATION_MISMATCH` | 1 分 |
| 一次性與併發 | `-run 'Replay|Concurrent' -race` | 同請求重送回原 attempt；不同請求重用 jti → `GRANT_REPLAYED`；兩個 goroutine 搶同一 jti／nonce → 恰一個成功 | 2 分 |
| 恢復 | `-run Recover` | 三個中斷點各以測試 hook 注入後重開：結果符合第 5.3 節；`valid_before` 已過 → `expired_unsigned` | 2 分 |
| 介面 | `go test ./cmd/apa-signer/` | 不在 `callers` 的 uid → `CALLER_NOT_ALLOWED`（以測試自己的 uid 進出清單模擬）；超大請求、超時；沒有其他路由 | 2 分 |
| 不外洩 | 同上，掃描 log、錯誤與回覆 | 不含私鑰、密語、完整簽章（log 與證據）、assertion 原文 | 包含在上列 |
| P1-a 不回歸 | `go test ./internal/store/ ./internal/paycontract/ ./cmd/demo/ -run 'Pay|P1a'`、`node --test web/test/p1a.test.mjs` | 模擬 grant 仍被拒；P1-a 行為不變 | 3 分 |
| OS 隔離（**需系統設定核准後**） | `scripts/verify/p1b-isolation.sh`（以 Aaron 身分執行） | 讀金鑰庫目錄與檔案 → 權限拒絕；Host 與 CLI 的環境中沒有金鑰材料；非 `callers` 的 uid 連 socket 被拒 | 2 分 |
| 反向 | `m-b-reverse.py '^P1b:'` | 拿掉：jti 唯一、nonce 唯一、kid 方法比對、`simulated` 拒絕、限額只能收緊、預留介面、保存後才回傳、呼叫者 uid → 各由預期斷言抓到 | 5 分 |

不預設：完整容器、全部突變、M1 產包、Unity 重建、任何鏈或網路。

## 8 決策表

| # | 項目 | 推薦 | 影響 | 現在要 Aaron 決定？ | 最晚 |
|---|---|---|---|---|---|
| 1 | P1-b **程式實作**核准 | 照本設計實作 Signer 核心、Unix socket 服務、Host 用的 client、policy grant 簽發、schema 4；只用測試金鑰與替身；不接進展示流程 | 新套件與 schema 4 | **是**（下一片） | 實作派工前 |
| 2 | 新依賴 | `github.com/decred/dcrd/dcrec/secp256k1/v4`（簽章，預期為 RFC 6979 決定性，實作時以凍結向量確認）與 `golang.org/x/crypto`（`sha3`、`scrypt`）；不引入 go-ethereum | 兩個依賴，版本釘選；若決定性不成立，第 5.3 節要改成「簽章前先保存 digest 與待簽狀態、只回傳已保存的簽章」並重新 review | **是**（與 1 一起） | 實作派工前 |
| 3 | 隔離的系統設定 | 建立一般使用者 `apa-signer` 與群組 `apa-signer-clients`、socket 目錄；不用 keychain、容器或 launchd | 本機系統設定；OS 隔離驗收需要它 | **是，但與 1 分開**：程式可先做，OS 隔離驗收要等這項核准並由 Aaron 執行 | OS 隔離驗收前 |
| 4 | 本機 demo 用的 A／B 付款金鑰建立與解鎖 | 之後另行核准；P1-b 測試只用測試金鑰 | — | 否（P2 前） | P2 申請時 |
| 5 | 金鑰解鎖方式 | 加密金鑰檔，啟動時在終端機輸入密語；不自動解鎖 | 每次重啟要輸入 | 否（推薦即可） | 實作時 |
| 6 | 報價在簽前重新取得 | 由 Host 做；Signer 不連網 | Signer 介面最小 | 否 | 實作時 |
| 7 | policy 簽發金鑰的位置 | 留在 Host（`0600`），風險照實記錄；之後可移到另一個使用者 | 第 3.3 節的剩餘風險 | 否 | 若要加強，P2 前 |
| 8 | 第 11 節的兩點補正（第 5.6 節） | 照表補 | 文件 | 否（Codex review 時確認） | 實作前 |

不重問：EOA＋EIP-3009（ADR Accepted）、P1-a 已限定結案、`simulated` 永遠不能取得付款能力。金鑰建立與解鎖、實際簽署、系統設定、外部交易的核准，都與程式實作核准分開，各自申請。
