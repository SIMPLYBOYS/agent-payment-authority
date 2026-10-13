# 本人核准最小流程（A1 實機可行性、A2 正式接線）：設計

> 2026-10-13，Claude Code。依 vault〈Claude-Code-本人核准最小流程設計派工-d7cbf9f-20261013〉（Aaron 已核准「本人核准最小流程」的設計）。分支 `commercial-p1b`，基準 `d7cbf9f`（工作區只有未追蹤的 `demo`）。
> **狀態：本人核准最小流程設計完成，待 Codex review；A1 實機與 A2 接線未執行，付款未啟用。**
> 本輪只讀程式與文件、查官方資料、寫本文件：
> - 沒有改產品、沒有原型、沒有安裝依賴、沒有建置或測試；
> - 沒有在瀏覽器註冊、沒有建立 WebAuthn credential、沒有讀 keychain 或個人 passkey；
> - 沒有動 `/opt/apa-signer`、沒有付款金鑰、沒有啟動 Signer、沒有簽署、沒有連鏈。

## 0 一頁摘要

**做完 A1＋A2 之後，使用者可以**：
- 在主持人頁的「需要你處理」打開這一筆 Ask，看到客人、商品、收款人、付款帳戶、金額與資產、網路、委託範圍與後端期限；
- 按一個主要按鈕，用**已由管理者登記**的 authenticator 完成 WebAuthn；
- 由 **Signer** 驗證 assertion 是否綁定這一筆的 `intent_hash`、核准會話與一次性 challenge。Host 只把 Signer 的驗證結果記為「本人核准已驗證」。

**仍然不能**：
- 付款：預留驗證器在 P2 前一律 `RESERVATION_UNAVAILABLE`，A2 不改這點；
- 自助登記或把 credential 綁到任意 consumer；
- 用主持人配對碼、viewer／host／operator token、`device_id` 或 P1-a 模擬身分取得本人核准權限；
- 語音（V1，未實作）；保證「本人看見並理解了正確金額」（第 4.7 節）。

**推薦方案**：
1. **WebAuthn 由 Signer 驗證，Host 只轉送**。Host 不持任何簽發金鑰（沿用 P1-b 的邊界）。
2. **兩步**：Signer 先發一次性 challenge（綁 session、consumer、wallet、`intent_hash`、jti、期限、approver 登記版本），再驗 assertion 並保存一筆 Signer 端的「已驗證核准」。之後（P2）付款請求只引用這筆核准，由 Signer 在接受付款的交易裡消耗它，並重新驗一次。A2 只做到「已驗證」，不消耗、不簽。
3. **登記是管理者儀式**：管理者在 Signer 的終端機開始與確認，瀏覽器只負責建立 credential；確認前的 credential 一律不能用。
4. **RP ID `localhost`，唯一 origin `http://localhost:8402`**：現行主持人頁在 `http://127.0.0.1:8402`，IP 不能當 RP ID（第 4.1 節）。
5. **Go 函式庫 `github.com/go-webauthn/webauthn` v0.18.2**（BSD-3-Clause），只放在 Signer。

**真正阻擋下一步的決策**（第 10 節）：
- D1：A1 的實機 authenticator 路徑；
- D2：首次登記的方式；
- D3：RP／origin 與入口；
- D4：新依賴；
- D5：Signer 介面改成兩步；
- D6：UV、同步 passkey 與計數器的政策；
- D7：Host 核准路由不加角色 token。

## 1 基準與來源

**已讀的程式與文件**：
- `docs/commercial-demo-roadmap.md`：A1、A2、V1 與最終驗收對照；
- `docs/commercial-transaction-flow.md`：第 1、3、8、9、10、11、11.1 節；
- `docs/commercial-p1a.md`；`docs/mockups/commerce/2-approval.html`；
- `docs/commercial-p1b-design.md`：第 3～6 節；`docs/commercial-p1b.md`：第 4、10 節；
- 程式（以符號為準，不照抄行號）：
  - 契約：`internal/paycontract`（`AuthorizationGrant`、`SessionID`、`GrantID`、`Delegation`）；
  - Store：`internal/store`（`approval_sessions`、`grants`、`payment_attempts` 的 DDL；`ResolveApproval`、`sessionDeadline`、`verifyPay`、`verifyPayTables`、`verifyPayments`）；
  - Signer：`internal/signer`（`verify` 對 `webauthn` 回 `HUMAN_APPROVAL_UNAVAILABLE`；`Authorization`；`Handler` 的固定路由；`Config`）；`internal/signerclient`；
  - Host：`cmd/demo/sim.go`（`simGuard`、`simResolve`）、`cmd/demo/api.go`（`authorize` 的 Host／Origin 檢查）、`cmd/demo/main.go`（只綁 loopback；`addr` 是實際綁定的位址）、`Makefile`（`APA_ADDR` 預設 `127.0.0.1:8402`）；
  - 網頁：`web/hostcreds.js`（世代 `gen`）、`web/adapter.js`（`approvalTarget`、`sameTarget`）、`web/host.js`（核准面板）；
  - 驗證工具：`scripts/verify/harness.mjs`（以 CDP 驅動 Chrome，沒有額外依賴）。

**官方資料**（2026-10-13 查閱）：

| 來源 | 內容 | 本設計用到的 |
|---|---|---|
| W3C, *Web Authentication Level 3*, Recommendation 2026-08-25，<https://www.w3.org/TR/webauthn-3/> | §5.1.4.3：`rp.id` 必須等於或是 effective domain 的 registrable domain suffix，effective domain 不是有效網域就是 `SecurityError`；§6.1：authenticator data 的 `rpIdHash`、UP、UV、BE、BS、`signCount`；§6.1.1：計數器；§7.1：註冊的 29 步；§7.2：驗證 assertion（第 1～6 步已核對原文：allowCredentials、使用者與 credential record 的對應） | 第 4 節的規則 |
| 同上 §7.2 第 7 步之後、§13.4.3 Cryptographic Challenges | 擷取工具在這兩段截斷，**本輪沒有讀到原文**。第 4.4 節依規格的已知結構列出步驟（client data 的 `type`、`challenge`、`origin`、`topOrigin`、`rpIdHash`、UP、UV、BE／BS、簽章、計數器）並標「待核對原文」 | 第 4.4、4.5 節，**待 Codex 對照原文** |
| go-webauthn 的 releases，<https://github.com/go-webauthn/webauthn/releases> | v0.18.2（2026-09-19）、v0.18.1（2026-09-10，只更新依賴）、v0.18.0（2026-08-27，破壞性變更：`id` 與 `rawId` 不一致或缺 `rawId` 就拒絕；**RP ID 設成 IP 位址會在設定驗證時失敗**；`HasherFromCOSEAlg` 不再預設 SHA-256；OKP 演算法限縮；`Verify` 多了 `rpOpaqueOrigins` 參數） | 第 4.8 節 |
| go-webauthn v0.18.2 的 go.mod | `go 1.26.0`；直接依賴：`fxamacker/cbor/v2 v2.9.4`、`go-viper/mapstructure/v2`、`go-webauthn/x v0.3.1`、`golang-jwt/jwt/v5`、`google/go-tpm`、`google/uuid v1.6.0`、`tinylib/msgp`、`stretchr/testify`、`go.uber.org/mock`；間接：`golang.org/x/crypto v0.57.0`、`golang.org/x/sys v0.48.0`（**與本 repo 現在的釘選相同**） | 第 4.8 節 |
| pkg.go.dev 上的 go-webauthn | BSD-3-Clause；`Flags.BackupEligible`、`BackupState`；`Authenticator` 的 clone warning、sign count 須寫回 | 第 4.4 節 |

**macOS 12.7.6 的瀏覽器支援**：
- 沒有找到 Apple 官方對「Monterey 上的 passkey」的明確頁面；第三方來源說法不一（例如 eclecticlight.co 2022-09-14、mojoauth.com 的版本表）。
- 所以本設計**不假設**任何一條實機路徑可用：Touch ID 平台 authenticator、外接 FIDO2 key、手機 cross-device 都列入 A1 實測（第 2 節）。

## 2 A1／A2 邊界與最短順序

| 切片 | 證明什麼 | 不證明什麼 | 內容 |
|---|---|---|---|
| **A1** 實機可行性（spike） | 這台 macOS 12.7.6 上，哪個瀏覽器加哪種 authenticator，能在 `rp_id=localhost` 完成一次註冊與一次 assertion；錯 challenge、錯 origin、未要求 UV 時的實際行為 | Approval、grant、Signer 接線；任何付款能力 | 一個只在本機的 spike（建議 `spikes/webauthn/`，不進產品程式）：一個 Go 小伺服器，綁 `localhost:8123`，用 go-webauthn 驗證；固定一筆合成的 `intent_hash`；Aaron 操作 |
| **A2** 正式接線 | Ask 時本人以已登記 credential 核准，assertion 由 Signer 驗證並綁定這一筆；Host 只記 Signer 的結果；重放、錯身分、撤銷、到期、取消、重送都被拒或正確恢復 | 付款、結算、交付；語音；本人確實看懂了金額 | Signer 的登記與驗證、Store schema 5、Host 的核准路由與頁面、`apa.pay.v1` 的 webauthn grant 記錄與事件 |

**A1 通過不等於 A2 通過**：A1 只決定實機路徑（第 10 節 D1），程式與資料都不沿用到產品。

**最短順序**：
1. A1 spike：實作約半天到一天（Claude Code），Aaron 實機操作約 30～60 分鐘。
2. A1 結論交 review，決定 D1。
3. A2 實作：Signer 登記與驗證 → Store schema 5 → Host 路由與頁面。約 2～3 天，含定向測試。
4. A2 以 virtual authenticator 跑定向自動化（第 9 節）。
5. Aaron 用 A1 決定的實機做一次本人操作確認。
6. 交 review。V1 在 A2 之後另行設計與核准。

## 3 身分與登記的最小可信路徑

### 3.1 誰有權登記、撤銷、替換

| 動作 | 誰 | 怎麼做 | 權威資料在哪 |
|---|---|---|---|
| 新增 approver（approver_id ↔ consumer ↔ 可付款的 wallet） | 管理者（Aaron，本機管理員） | 寫進 Signer 的可信設定 `signer.json` 的 `approvers`（`approver_id`、`consumer_id`、`wallet_ids`、`signer_max`、`version`），重啟 Signer | Signer 設定（`apa-signer` 擁有，`0600`） |
| 登記 credential | 管理者發起並確認，本人在瀏覽器操作 authenticator | 第 3.2 節的儀式 | Signer DB 的 `credentials`（只有 Signer 寫） |
| 撤銷 credential | 管理者 | `apa-signer approver revoke --credential <id>`（Signer 終端機） | 同上，`state=revoked`、`revoked_at` |
| 替換 | 管理者 | 先登記新的，確認可用後撤銷舊的 | 同上 |
| 恢復（裝置遺失） | 管理者 | 撤銷遺失的；重新登記 | 同上 |

第一片採人工管理流程，不建帳號中心，也**沒有**任何讓頁面、Host 或 Agent 新增或綁定 consumer 的入口。approver 與 consumer、wallet 的對應只在 Signer 設定裡，由管理者改；Signer 啟動時載入，每筆有 `version`。

### 3.2 登記儀式（推薦 D2＝E1）

1. **開始**（管理者，在 W-signer 以 `apa-signer` 身分）：`apa-signer approver enroll begin --dir … --approver approver-a`。
   - Signer DB 寫一筆 `enrollments`：隨機 32 bytes 的 challenge、5 分鐘期限、`pending`。
   - 終端機印出登記 ID，以及一個 8 碼的一次性確認碼。
2. **建立**（本人，瀏覽器 `http://localhost:8402/approve/enroll`）：
   - 頁面請本人輸入登記 ID 與確認碼。
   - Host 經 socket 向 Signer 取登記選項（`GET /v1/enrollments/{id}?code=`，碼錯就拒絕）。
   - 瀏覽器 `navigator.credentials.create()`：`rp.id=localhost`、`user.id`＝Signer 為這位 approver 產生的隨機 16 bytes（不含 consumer ID 或個資）、`userVerification=required`、`residentKey=preferred`、`attestation=none`、`pubKeyCredParams=[ES256]`（第 10 節 D6）。
   - Host 把結果原樣轉給 Signer（`POST /v1/enrollments/{id}`）。
3. **驗證**（Signer）：依 §7.1 驗證（第 4.3 節），通過後存成 `credentials` 的 `pending` 列，**還不能用來核准**。
4. **確認**（管理者，在 W-signer）：`apa-signer approver enroll confirm --id …`。
   - 終端機顯示這把 credential 的 ID 指紋（前 8 bytes 的 hex）、AAGUID（`none` attestation 時常是全 0）、UV／BE／BS 旗標與建立時間。
   - 頁面也顯示同一個 ID 指紋。管理者核對兩邊一致後輸入 `yes`，列才變成 `active`。

**為什麼不讓 Host 直接送公鑰**：Host 與 Agent 同 uid，被控制時可以送任何公鑰，所以 credential 一律由 Signer 依 §7.1 驗證，而且要管理者在 Signer 終端機確認。

**照實記的剩餘風險**：登記時 Host 會轉送登記選項，看得到 challenge。若 Host 在登記的那幾分鐘已被控制，它能用自己的軟體 authenticator 搶先完成第 2 步。確認碼與第 4 步的指紋比對可以降低這個風險，但不能消除，因為管理者的瀏覽器畫面也由 Host 提供。所以：登記必須在乾淨啟動的 Host 上進行；若懷疑 Host 被控制，撤銷並重新登記。替代做法 E2（Signer 在儀式期間自己開一個 loopback 登記頁）列在 D2。

### 3.3 不能升格為本人核准的東西

- 主持人配對碼、viewer／host／operator token、P1-a 的模擬核准者憑證：Host 的核准路由**不接受**它們作為核准依據（第 6.3 節）。核准的唯一依據是 Signer 驗證過的 assertion。
- `device_id` 或複製來的 credential ID：沒有私鑰就產生不了有效簽章；Signer 只接受 `active` 的 credential，並驗簽章與 challenge。
- 前端切換 A／B：只改顯示；每次 challenge 由 Signer 依請購的 consumer 找該 consumer 的 approver，`allowCredentials` 只放該 approver 的 `active` credential。

### 3.4 撤銷之後

| 時點 | 處理 |
|---|---|
| 新的核准 | Signer 不再為它發 challenge（`CREDENTIAL_REVOKED`／`APPROVER_UNKNOWN`） |
| 已發出、未完成的 challenge | 驗證時再查一次 credential 狀態，不是 `active` 就拒絕 |
| 已驗證、未用於付款的核准 | 付款（P2）接受時再查一次，不是 `active` 就拒絕，並把核准標為 `revoked` |
| 已受理的付款 attempt（P2） | 照 P1-b 的規則：已接受、未簽的可以 abandon；已簽的不能撤回，記為待對帳 |

### 3.5 「本人」的意思

- 本設計證明的是：**持有一把已登記的 authenticator，並通過了它要求的 user verification**。
- 這不是現實世界的身分保證，也沒有 KYC。
- 若 Aaron 代操作 A 與 B，就是兩個展示角色、各自登記的 credential（可以是同一台裝置上的兩把），畫面與紀錄都寫「展示角色：Aaron 操作」，不宣稱是兩位不同自然人。

## 4 WebAuthn 與交易意圖的驗證規則

### 4.1 RP ID、origin 與 secure context（D3）

- **RP ID**：`localhost`。
  - IP 位址不是有效網域，不能當 RP ID（§5.1.4.3；go-webauthn v0.18.0 起也在設定驗證時拒絕 IP）。
  - 現行主持人頁在 `http://127.0.0.1:8402`（`Makefile` 的 `APA_ADDR` 預設；`cmd/demo` 以實際綁定的位址當唯一接受的 Host／Origin），**不能直接做 WebAuthn**。
- **origin**：推薦唯一的 `http://localhost:8402`。
  - Host 加一個設定：可信的對外名稱 `localhost`。仍只綁 loopback，接受的 Host／Origin 從單一值改成精確的允許清單，只多 `localhost:8402` 一個；不使用萬用允許、不接受 `null` origin。
  - 核准相關路由（第 6.3 節）**只**接受 `http://localhost:8402`。
- **Signer 也要驗 origin**：Signer 設定 `webauthn.origins = ["http://localhost:8402"]`，驗 client data 的 `origin` 精確相等，`topOrigin` 不存在或相等，`crossOrigin` 不是 `true`。
- **secure context**：`http://localhost` 在 W3C Secure Contexts 裡算 potentially trustworthy，但各瀏覽器在 macOS 12 上的實際行為列入 A1 實測。
- **既有 `localhost:8123` 提案**：只給 A1 spike 用（獨立小伺服器）。A2 不另開 origin。credential 綁的是 RP ID，不綁 port，所以 A1 登記的 credential 在 A2 的 `:8402` 上技術上可以用；但 A2 一律重新走第 3.2 節的儀式，不沿用 spike 的 credential。

### 4.2 不需要跨 origin

主持人頁、核准頁與 WebAuthn 都在同一個 origin，所以不需要 iframe、`postMessage` 或 CORS。若之後真的需要跨 origin（例如手機頁），另行設計，屆時要驗 `topOrigin` 與訊息來源，並用精確的允許清單。

### 4.3 登記的驗證規則（§7.1）

Signer 依 §7.1 全部 29 步，重點如下：
- `type` 是 `webauthn.create`；challenge 等於該 `enrollments` 列的 challenge（base64url）；`origin` 在允許清單；`rpIdHash` 等於 SHA-256("localhost")；
- **UP 與 UV 都必須設**（`userVerification=required`）；BE 沒設則 BS 也不能設；
- 公鑰演算法在 `pubKeyCredParams` 之內（ES256）；`fmt` 只接受 `none`（`attestation=none`，D6）；
- credential ID ≤ 1023 bytes，且不能已登記；
- 通過後存成 `pending`（第 3.2 節第 3 步），保存公鑰、`signCount`、UV 是否初始化、BE、BS、AAGUID、transports 與建立時間。

### 4.4 assertion 的驗證規則（§7.2）

第 1～6 步已核對原文；第 7 步之後依規格結構列出，**待 Codex 對照原文核對**：

1. credential ID 在本次 challenge 的 `allowCredentials` 之內，對應一筆 `active` credential，屬於這次 challenge 的 approver。若回應帶 `userHandle`，必須等於該 approver 的 `user.id`。
2. client data：`type` 是 `webauthn.get`；`challenge` 等於本次 challenge；`origin` 在允許清單；`topOrigin` 不存在；`crossOrigin` 不是 `true`。
3. authenticator data：`rpIdHash` 等於 SHA-256("localhost")；**UP 與 UV 都必須設**；BE 沒設則 BS 不能設。BE／BS 寫回 credential 紀錄並記在核准紀錄裡（D6）。
4. 簽章：用保存的公鑰，驗證對 `authenticatorData ‖ SHA-256(clientDataJSON)` 的簽章。
5. 計數器（D6 推薦）：
   - 保存值與收到值都是 0：接受（同步 passkey 常見）；
   - 任一不為 0 而收到值 ≤ 保存值：**拒絕**（`ASSERTION_COUNTER`），並把 credential 標為 `suspect`，交管理者處理；
   - 收到值較大：接受並寫回。
6. 以上全部通過，才在同一個寫入交易裡「消耗 challenge＋寫入已驗證核准」（第 6.2 節）。

### 4.5 challenge：誰產生、綁什麼、何時消耗

**產生**：Signer，在 `POST /v1/approvals/challenge` 時：

```text
record = canonical({
  version: 1, challenge_id, random: 32 bytes（crypto/rand）,
  approval_session_id, consumer_id, wallet_id, intent_hash,
  jti（= paycontract.GrantID(approval_session_id)）, approver_id, approver_version,
  config_hash, expires_at
})
webauthn_challenge = SHA-256("agent-payment-authority/pay/v1/webauthn-challenge" ‖ 0x00 ‖ record)
```

- 隨機數讓 challenge 不可預測（§13.4.3 的隨機與長度要求，待核對原文）；hash 讓 challenge 本身可以對回這一筆。
- `record` 原文與 challenge 都存在 Signer DB 的 `approval_challenges`。
- `canonical` 用本 repo 的 `contracts.Canonical`。

**誰綁什麼、誰核對**：

| 欄位 | 從哪裡來（可信度） | 綁在哪 | 誰核對、何時 |
|---|---|---|---|
| `intent_hash` | Signer 從 Host 送來的**完整 v1 請購**重算（`paycontract.PurchaseOrder.Validate`＋`IntentHash`），不接受 Host 給的 hash | `record`、`human_approvals` | Signer：發 challenge、驗 assertion、（P2）付款接受時各重算一次，必須相同 |
| consumer、wallet | 請購的 intent；Signer 設定的 approver 與 wallet 對應 | `record` | Signer：approver 的 `consumer_id` 必須等於請購的、wallet 必須在 `wallet_ids` 且註冊給同一 consumer |
| `approval_session_id` | Host 的核准會話（Host 依 `SessionID(task, purchase_order_id)` 決定） | `record` | Signer：驗 assertion 時請求必須帶同一個；Host：只把結果寫回這個會話 |
| `jti` | `GrantID(approval_session_id)`，決定性 | `record`、`human_approvals` | Signer：一個 jti 最多一筆已驗證核准；Host：一個會話最多一筆 grant（既有 `grants.approval_session_id UNIQUE`） |
| 期限 | Signer 自己決定：`expires_at = min(now + 120 s, valid_before, Host 給的會話期限)`；Host 的值只能讓它更短 | `record` | Signer：驗 assertion 時要求 `now < expires_at`（時間在寫入交易內取） |
| 核准的有效期 | Signer 設定：`approval_ttl`（推薦 300 s） | `human_approvals.expires_at = min(verified_at + approval_ttl, valid_before)` | Signer：（P2）付款接受時要求未到期 |
| policy 版本 | approver 的 `version` 與 Signer 設定 hash | `record` | Signer：驗 assertion 與付款接受時，版本不同就拒絕（`APPROVER_CHANGED`） |

**消耗**：
- challenge：驗 assertion 成功的那一個交易裡由 `issued` 改成 `used`。之後同一個 challenge 的任何 assertion 都是 `CHALLENGE_USED`，同一份 assertion 的**正常重送**除外（第 7 節）。
- 已驗證核准：只在（P2）付款接受的交易裡消耗。A2 不消耗它。

**付款綁定改變時**：`intent_hash` 涵蓋業務意圖與 `PaymentBinding`（商品、收款人、金額、資產、網路、付款 nonce、時間窗）。任一欄改變都會得到不同的 hash，舊 challenge 與舊核准都對不上。所以 challenge 不另外重複列這些欄位，避免兩份值不一致。Host 也照 P1-a 的規則，在報價改變時把會話 `superseded`。

### 4.6 與既有草案的差異

舊的 challenge 公式有兩種，本設計取代兩者：
- `commercial-transaction-flow.md` 第 3.2 節：`H("payment-approval/v1" ‖ intent_hash ‖ voice_nonce ‖ approval_session_id)`；
- `commercial-p1b-design.md` 第 4.3 節：`H("agent-payment-authority/pay/v1/approval" ‖ approval_session_id ‖ intent_hash ‖ jti ‖ expires_at)`。

差異：
- 加了隨機數與 approver 版本，期限由 Signer 決定；
- `voice_nonce` 在 V1 另行加入，不放在 A2。

### 4.7 「綁定」不等於「本人看見並理解」

- WebAuthn 簽章證明 authenticator 對這個 challenge 簽了名，而 challenge 對回這一筆 `intent_hash`。它**不**證明本人看到的金額或收款人就是這一筆：瀏覽器的系統提示只顯示網站（localhost），不顯示交易內容。
- 摘要由 Signer 從請購產生：challenge 回覆帶 `display`，包含客人、商品、收款人、付款帳戶、金額與資產、網路、委託範圍、期限。Host 頁面顯示這份 `display`。
- 但頁面仍是 Host 提供的。**Host 或頁面被控制時，可以顯示 A 的摘要、卻替 B 的 challenge 要求簽章**，本設計擋不住這種「看到的不是簽的」攻擊。
- 能做的限制有兩個：每次核准只綁一筆、期限短；Signer 的每筆與累計硬上限（`signer_max`）。可信交易顯示（獨立裝置或 Signer 自己的畫面）列為後續選項，不在 A2。

### 4.8 Go 函式庫（D4）

**推薦 `github.com/go-webauthn/webauthn` v0.18.2**（2026-09-19，BSD-3-Clause），只放在 Signer：
- 它完整實作 §7.1 與 §7.2、COSE 公鑰、各種 attestation 格式與 BE／BS，不需要自行實作協定或密碼學；
- 它的 `x/crypto v0.57.0`、`x/sys v0.48.0` 與本 repo 現在的釘選相同，不必連帶升級。

**影響**：Signer 多出這些模組（含間接）：
- `fxamacker/cbor/v2`、`go-webauthn/x`、`golang-jwt/jwt/v5`、`google/go-tpm`、`go-viper/mapstructure/v2`、`tinylib/msgp`、`philhofer/fwd`、`x448/float16`、`go.yaml.in/yaml/v3`；
- 測試用的 `stretchr/testify`、`go.uber.org/mock` 只進 go.sum。

這擴大了 Signer 的可信程式範圍，是 D4 的取捨。替代做法：只用 `fxamacker/cbor` 加標準庫自己驗 ES256 assertion，依賴少但要自寫協定解析，不推薦。實際依賴圖在實作時以 `go mod` 再核對一次，並單獨提交。

## 5 完整資料流與信任邊界

```text
登記（一次）  管理者 ─(W-signer)→ Signer: enroll begin ─→ Signer DB enrollments
              本人 ─(瀏覽器 localhost:8402)→ Host ─(socket)→ Signer: 取選項／送 attestation ─→ credentials(pending)
              管理者 ─(W-signer)→ Signer: enroll confirm ─→ credentials(active)

單筆核准      Agent 請購 → Host/Policy: Ask → Store: approval_sessions(pending)
              本人按「用已登記的裝置核准」→ Host: 寫 approval_attempts(requested, request_id)
              Host ─(socket)→ Signer: challenge(完整請購, session) ─→ Signer DB approval_challenges(issued)
              Host: approval_attempts(challenged, challenge_id) → 頁面: navigator.credentials.get()
              瀏覽器 → Host: assertion → approval_attempts(verifying)
              Host ─(socket)→ Signer: verify(challenge_id, 完整請購, assertion)
                                 ─→ Signer DB: challenge used ＋ human_approvals(verified)（同一交易）
              Host: 同一交易 approval_sessions(approved) ＋ grants(method=webauthn, signer_approval_id)
                    ＋ 事件 APPROVED、GRANT_ISSUED ＋ approval_attempts(verified)

查詢          頁面 → Host: 這一筆的狀態（Store）；Host ─(socket)→ Signer: GET /v1/approvals/{id}（Signer 的權威結果）

付款（P2）    Host → Signer: /v1/sign {authorization: {method: webauthn, approval_id}, order}
              Signer: 重算意圖、查核准 verified 未到期、credential 仍 active、approver 版本相同、signer_max
                      → 同一個接受交易消耗核准＋建立 attempt
```

| 層 | 權威資料 | 信任 | 不能信的 |
|---|---|---|---|
| 瀏覽器與 authenticator | 私鑰在 authenticator 裡；UV 由 authenticator 執行 | 簽章只能由持有者產生 | 頁面內容（Host 提供）、前端回報的「成功」 |
| Host（與 Agent 同 uid） | 核准會話與期限、取消、報價、顯示 | 照 P1-b：Host 決定會話期限與取消 | Host 回報的 `approved=true` 或 `method=webauthn` 都不是本人核准的依據 |
| Signer（`apa-signer` uid） | approver 設定、credential、challenge、已驗證核准 | 本人核准是否成立，由 Signer 判定 | Host 送來的 hash、摘要、期限（只能讓它更短） |
| 管理者 | 設定、登記確認、撤銷 | 本設計不防管理者 | — |

## 6 資料、介面與 schema 對照

### 6.1 Signer 端（新）

- **設定**（`signer.json`）：
  - `approvers: [{approver_id, consumer_id, wallet_ids, signer_max, version}]`；
  - `webauthn: {rp_id: "localhost", origins: ["http://localhost:8402"], challenge_ttl_seconds: 120, approval_ttl_seconds: 300}`。
  - `ParseConfig` 嚴格驗證：approver 的 consumer 與 wallet 對得上 `wallets`，origins 必須是精確的 `http://localhost:<port>`。
- **DB**（`signer.db` 新表）：

| 表 | 欄位（最小） | 唯一性與檢查 |
|---|---|---|
| `enrollments` | `enrollment_id` PK、`approver_id`、`code_hash`、`challenge`、`expires_at`、`state`（pending／finished／expired） | 每個 approver 同時最多一筆 pending |
| `credentials` | `credential_id` PK、`approver_id`、`user_handle`、`public_key`（COSE）、`alg`、`sign_count`、`uv_initialized`、`backup_eligible`、`backup_state`、`aaguid`、`state`（pending／active／suspect／revoked）、`created_at`、`activated_at`、`revoked_at` | `credential_id` 唯一；只有管理者命令改 `state` |
| `approval_challenges` | `challenge_id` PK、`request_id` UNIQUE、`fingerprint`、`challenge` UNIQUE、`record`（canonical）、`approval_session_id`、`intent_hash`、`jti`、`approver_id`、`expires_at`、`state`（issued／used／expired） | 一個 `jti` 同時最多一筆 issued；`request_id` 冪等 |
| `human_approvals` | `approval_id` PK、`challenge_id` UNIQUE、`jti` UNIQUE、`approval_session_id`、`intent_hash`、`approver_id`、`approver_version`、`credential_id`、`flags`（UP／UV／BE／BS）、`sign_count`、`client_data_hash`、`authenticator_data`、`verified_at`、`expires_at`、`state`（verified／consumed／expired／revoked／abandoned） | 一個 `jti` 最多一筆；`consumed` 只在 P2 的付款接受交易裡發生 |

- **socket 路由**（固定，沿用 P1-b 的 caller uid、64 KiB、期限）：
  - `POST /v1/approvals/challenge {request_id, approval_session_id, order, host_deadline}` → `{challenge_id, options, display, expires_at}`；
  - `POST /v1/approvals/verify {request_id, challenge_id, order, assertion}` → `{approval_id, state, intent_hash, approver_id, expires_at}`；
  - `GET /v1/approvals/{approval_id}`、`GET /v1/approvals?challenge_id=`；
  - `POST /v1/approvals/{approval_id}/abandon`（只在 `verified` 時，改成 `abandoned`）；
  - `GET /v1/enrollments/{id}?code=`、`POST /v1/enrollments/{id}`。
- **新錯誤碼**：`APPROVER_UNKNOWN`、`CREDENTIAL_REVOKED`、`CREDENTIAL_PENDING`、`CHALLENGE_UNKNOWN`、`CHALLENGE_EXPIRED`、`CHALLENGE_USED`、`ASSERTION_INVALID`（細分 `_TYPE`、`_ORIGIN`、`_RPID`、`_FLAGS`、`_SIGNATURE`、`_COUNTER`）、`APPROVER_CHANGED`、`ENROLLMENT_CODE`；沿用 `CONSUMER_MISMATCH`、`ORDER_INVALID`、`REQUEST_ID_CONFLICT`、`SIGNER_TIMEOUT`。
- **`/v1/sign` 的 `webauthn`**：A2 **維持** `HUMAN_APPROVAL_UNAVAILABLE`。P2 才改成接受 `{method: webauthn, approval_id}`，並在接受交易裡重驗與消耗（第 5 節）。這樣 A2 的驗證入口不會變成付款入口，正式預留驗證器維持 fail-closed，不加繞過開關。
- **管理命令**（`cmd/apa-signer`，只在終端機）：`approver enroll begin|confirm`、`approver revoke`、`approver list`。

**verify 入口不消耗什麼、不能代替什麼**：
- verify 只消耗 challenge，不消耗核准、不建立付款 attempt、不動錢包鎖或累計用量。
- 付款時 Signer 會重算意圖、重查 credential 與 approver 版本、檢查 `signer_max`、預留與期限，所以 verify 的結果不能代替最後的 sign 檢查。

### 6.2 Host 端：Store schema 5

以現行 schema 4 為基準：

| 表 | 變更 | 舊資料 |
|---|---|---|
| `approval_sessions` | 不改欄位；`resolved_by` 在 webauthn 時寫 `webauthn:<approver_id>` | 原樣 |
| `grants` | `authorization_method` 的 CHECK 放寬為 `simulated`／`webauthn`；新增 `signer_approval_id TEXT UNIQUE`，並 `CHECK ((authorization_method = 'webauthn') = (signer_approval_id IS NOT NULL))` | `simulated` 列原樣保留，**不能**改成 `webauthn`：沒有 Signer 的核准 ID，CHECK 不允許 |
| `approval_attempts`（新） | `request_id` PK（Host 在呼叫 Signer 前先寫入）、`approval_session_id` FK、`challenge_id` UNIQUE NULL、`signer_approval_id` UNIQUE NULL、`state`（requested／challenged／verifying／verified／refused／unknown）、`error_code`、時間 | 新表 |
| `payment_attempts` | **A2 不改**（仍只有 `delegation`）。webauthn 付款與 `grant_jti` 欄位移到 P2，才不會在 A2 先開出用不到的付款路徑 | — |

**`paycontract.AuthorizationGrant`**：
- `authorization_method` 可以是 `webauthn`，這時必填 `signer_approval_id`、`approver_id`、`credential_ref`（credential ID 的 SHA-256）、`amr: ["webauthn"]`（V1 再加 `voice`）。
- `Issuer` 改名為「記錄者」。webauthn 的 grant 是 **Signer 驗證結果在 Host 的鏡像**，不是 Host 簽發的授權。

**事件**：`APPROVED`、`GRANT_ISSUED` 帶 `authorization_method=webauthn` 與 `signer_approval_id`；`paycontract.ValidateEvent` 依方法驗證。

**復原檢查**（`verifyPay`、`verifyPayTables`）：
- webauthn grant 的會話必須是 `approved`、`resolved_by` 前綴是 `webauthn:`；
- `signer_approval_id` 對得上 `approval_attempts` 的 `verified` 列；
- `simulated` grant 的規則不變；delegation 路徑不建會話（不變）。

**遷移**：照現行的 `upgradeFrom`（驗證、`VACUUM INTO` 備份、單一交易、再驗證），1／2／3／4 都能升到 5。

### 6.3 Host 的核准路由（D7）

- `POST /api/approvals/{session_id}/challenge {request_id}`、`POST /api/approvals/{session_id}/assertion {request_id, assertion}`、`GET /api/approvals/{session_id}`。
- Host 與 Origin 都必須是 `localhost:8402`；POST 要 JSON；限速。
- **不需要也不接受任何角色 token**。核准的唯一依據是 Signer 的驗證，所以這些路由不擴大既有角色的權限。viewer 仍然唯讀，模擬核准者仍只在 fake。
- live 與 fake 都開放這些路由。fake 模式下只能用明標的 virtual authenticator（第 8 節），畫面與紀錄都寫「virtual」。
- 會話的 consumer 由 Store 決定，請求不能指定。

## 7 錯誤、競爭與恢復

**持久化順序**（Signer 與 Host 是兩個資料庫，**沒有**跨程序的原子交易）：
1. Host 先寫 `approval_attempts(requested)`，`request_id` 由 Host 產生。
2. 呼叫 Signer 的 challenge。Signer 用 `request_id` 冪等，存 challenge。
3. Host 寫入 `challenge_id`。
4. 收到 assertion 後，Host 先寫 `verifying`，再呼叫 verify。Signer 在一個交易裡消耗 challenge 並寫 `human_approvals`。
5. Host 在一個交易裡寫 `approved`＋grant＋事件。

**權威結果在 Signer**。Host 回覆遺失或當機時，一律以同一個 `request_id`／`challenge_id` 向 Signer 查詢，不重發 challenge、不換 ID。

| 情況 | 規則 |
|---|---|
| 核准與拒絕 | 拒絕不需要 WebAuthn（沿用 P1-a，Host 本機寫 `denied`）。拒絕或期限先發生後，Host 不再接受 verify 的結果；若 Signer 已 `verified`，Host 呼叫 `abandon` 並記錄。核准只在 Host 會話仍 `pending` 且未到期時寫入 |
| 到期 | Host 的會話期限照 `sessionDeadline`（決策時間＋terms 期限與 `valid_before` 取小）；Signer 的 challenge 期限只能更短。兩邊都由可信時鐘決定，呼叫者不能延長 |
| 取消 task | 同到期：Host 關閉會話；已驗證未用的核准由 Host 呼叫 `abandon`。Signer 沒有可驗證的撤回清單（沿用 P1-b），曝險以 `approval_ttl` 與 `valid_before` 為界 |
| 兩個分頁 | Host 對同一個會話同時最多一筆進行中的 `approval_attempts`。第二個分頁拿到同一個 challenge；先驗證成功的那個生效，另一個得到 `CHALLENGE_USED`，畫面依 Store 顯示「已在另一個視窗核准」 |
| 相同請求重送 | 同一個 `request_id`＋相同內容：Signer 回同一個 challenge 或同一筆核准。同一個 `request_id`、不同內容：`REQUEST_ID_CONFLICT` |
| 不同 ID 重用 assertion | assertion 的 client data 帶的是原 challenge，對不上別的 challenge（`ASSERTION_INVALID_CHALLENGE`）；原 challenge 已 `used` 就是 `CHALLENGE_USED` |
| 晚到的舊回覆 | Host 只接受目前 `approval_attempts` 列的 `challenge_id`；網頁沿用 `hostcreds` 的世代與 `approvalTarget`／`sameTarget`，舊世代的回覆丟棄 |
| 保存失敗與回覆遺失 | 沿用 P1-b 的 R3 規則：遠端結果不明就記 `unknown`。保存 `unknown` 也失敗時，同時回報兩件事，不重試、不換 ID |
| 裝置撤銷 | 第 3.4 節 |

**三種一次性分開**：
- challenge：一次 verify；
- 已驗證核准：一次付款接受（P2）；
- 付款 attempt：既有的 request ID 冪等。

合法重送回原結果；重放（別的請求用同一份東西）就拒絕。

**原始 assertion 的保存**：
- assertion 只在 Host 轉送時經過記憶體，Host **不保存**原文。
- Signer 只保存驗證所需的最小欄位：`authenticator_data`、`client_data_hash`、簽章的 SHA-256。完整原文不落地。
- 普通 log 與證據匯出只記 `approval_id`、`challenge_id`、`intent_hash`、credential ID 的 hash 與錯誤碼。
- challenge 用過即作廢，所以保存下來的欄位也不能被重放。

**OS 部署**：Signer 只多設定與 DB 表，沿用 P1-b 的 `/opt/apa-signer` 配置，不重開 OS 部署。新的 `webauthn` 設定與 approvers 由管理者寫入，未來驗收只看設定檔權限不變。

## 8 使用者體驗

場景仍是主畫面，技術證據留在抽屜。沿用 P1-a 的「需要你處理」卡與已接受的核准概念稿（`mockups/commerce/2-approval.html` 的正式模式）。

**核准表**（Signer 的 `display`，不只顯示 hash）：客人、商品、收款人（可信名稱與地址）、付款帳戶、金額與資產、網路、委託範圍（剩餘預算、`allow_max`）、後端期限（倒數只是提示）。

**動作**：
- 主要按鈕「用已登記的裝置核准」，啟動 WebAuthn；
- 次要按鈕「拒絕這筆」，不需要 WebAuthn；
- 「返回」只關閉面板；
- 「取消任務」維持在主持人的場次控制，不放在核准表。

| 狀態 | 文案 | 行為 |
|---|---|---|
| 等你核准 | 「請確認這一筆。核准會要求你的裝置驗證。」 | 主要按鈕可按 |
| 驗證中 | 「請在裝置上完成驗證…」 | 按鈕停用；不自動重試 |
| 你取消了系統提示 | 「你取消了驗證；這筆仍在等你核准。」 | 回到等你核准，不自動再彈出 |
| 驗證失敗 | 「驗證沒有通過（{原因}）；沒有核准。」 | 原因用固定代碼對應的中文 |
| 裝置已撤銷 | 「這個裝置已停用，請找管理者重新登記。」 | 不能核准 |
| 已過期 | 「已過期（後端時間為準）；沒有核准。」 | 關閉 |
| 結果尚未確認 | 「結果確認中，請不要重複操作。」 | 只查詢同一個 ID，不再發 challenge |
| 成功 | 「本人核准已驗證，付款未啟用。」 | 不顯示已付款或已交付 |
| 已在另一個視窗核准 | 「這筆已在另一個視窗核准。」 | 依 Store |

- **身分與焦點**：切換客人、刷新或晚到的回覆都不能把 A 或舊會話的操作轉到 B 或新會話。沿用 `hostcreds.js` 的世代、`adapter.js` 的 `approvalTarget`／`sameTarget`，以及 P1-a R1 的焦點規則（重畫後只把焦點還給同一筆，否則交給標題）。不另造沒有身分綁定的核准元件。
- **來源標示**：
  - fake Agent 不等於模擬 WebAuthn；
  - virtual authenticator 在畫面與紀錄都寫「virtual authenticator（測試用）」，不能冒稱真人實機；
  - Aaron 代操作 A／B 時寫「展示角色」。
- **不接**：語音（V1）標為「未實作」，不能記成已驗證的 `voice=unavailable` 降級；ZK、憑證、語音與多場景仍在商業展示版的後續範圍，本片不取消這些要求。

## 9 定向驗收矩陣與驗證計畫（未來執行；本輪不執行）

**測試層**：
- U：Go 單元測試。Signer 用程式產生的測試 ES256 金鑰組 authenticator data 與簽章。
- S：Store 測試。
- I：Signer socket 加 Host 的整合測試，用真的 Handler。
- W：node 網頁單元測試。
- B：Chrome 經 CDP 的 `WebAuthn.addVirtualAuthenticator`，沿用 `scripts/verify/harness.mjs`，不加新依賴。
- M：Aaron 實機。

| 保護規則 | 正例／反例 | 預期 | 層 | 預期耗時 |
|---|---|---|---|---|
| 正確登記與核准 | active credential、UV 通過 | `verified`；Host 會話 `approved`、webauthn grant、事件 | U、I、B | 1 m |
| 未登記／pending／已撤銷 credential | 各一 | `ASSERTION_INVALID`／`CREDENTIAL_PENDING`／`CREDENTIAL_REVOKED`；不寫核准 | U、I | 30 s |
| 錯身分 | B 的 credential 核准 A 的請購 | 不在 `allowCredentials`，或 approver 的 consumer 不符：拒絕 | U、I | 20 s |
| 只有複製的 device ID／credential ID | 有 ID、沒有私鑰（錯簽章） | `ASSERTION_INVALID_SIGNATURE` | U | 10 s |
| 主持人或角色 token 假冒本人 | 帶 viewer／host／operator 或模擬 token 打核准路由 | 路由不接受 token；沒有 assertion 就不可能 `verified` | I | 20 s |
| 錯 origin、RP ID、type | `http://127.0.0.1:8402`、其他 port、`webauthn.create` | `ASSERTION_INVALID_ORIGIN`／`_RPID`／`_TYPE` | U | 20 s |
| 錯 challenge | 另一個 challenge 的 assertion | `ASSERTION_INVALID_CHALLENGE` | U、I | 10 s |
| UV 沒設、UP 沒設 | 改旗標 | `ASSERTION_INVALID_FLAGS` | U | 10 s |
| 計數器倒退 | 保存 5、收到 3 | `ASSERTION_COUNTER`，credential 變 `suspect` | U | 10 s |
| 跨 consumer、跨付款、內容變更 | 請購的任一付款綁定欄位改一個 | `intent_hash` 不同：challenge 對不上 | U、I | 30 s |
| 重放 | 同一 challenge 第二份 assertion；同一份 assertion 給另一個 challenge | `CHALLENGE_USED`／`_CHALLENGE` | U、I | 20 s |
| 到期邊界 | `now == expires_at` | 拒絕（嚴格 `<`） | U | 10 s |
| 取消競爭 | verify 成功與 Host 取消先後兩種 | 取消先：不核准、Signer 核准被 `abandon`；核准先：取消照 P1-a 規則 | S、I | 1 m |
| 兩個分頁 | 兩個頁面同時核准 | 只有一筆核准；另一個 `CHALLENGE_USED` | I、B | 1 m |
| 相同請求重送 | 同 `request_id`、同內容 | 同一個 challenge／核准 | U、I | 20 s |
| 保存失敗與回覆遺失 | 注入 Store 寫入失敗、Signer 回覆中斷 | `unknown`，同 ID 恢復；保存失敗同時回報 | I | 1 m |
| 模擬 grant 仍不能付款 | simulated grant 送 `/v1/sign` | `GRANT_METHOD_NOT_ACCEPTED`（不變） | U | 10 s |
| A2 不開付款路徑 | webauthn 送 `/v1/sign` | `HUMAN_APPROVAL_UNAVAILABLE`；正式組裝 `RESERVATION_UNAVAILABLE`（不變） | U | 10 s |
| 舊資料不升權 | schema 4 檔（含 simulated grant、delegation attempt）升 5 | simulated 不能變 webauthn（CHECK）；復原檢查通過；delegation 路徑不建會話 | S | 1 m |
| 畫面與綁定一致 | Signer 的 `display` 與請購一致；改請購後重開 | 顯示新的；舊核准不適用 | W、B | 1 m |
| 焦點與身分世代 | 切換客人、刷新、晚到回覆 | 舊回覆丟棄；焦點規則同 P1-a R1 | W、B | 1 m |
| 錯誤與逾時不誤報成功 | 使用者取消、失敗、逾時 | 各自的文案；沒有成功字樣 | W、B | 1 m |
| 實機路徑（A1） | 一次註冊、一次 assertion、錯 challenge、錯 origin | 記錄實際結果；**不是 A2 的 PASS** | M | 30～60 m |

**反向檢查**（少量、可指認）：
- 拿掉 challenge 比對 → 命中「錯 challenge」；
- 拿掉 UV 要求 → 命中「UV 沒設」；
- 拿掉 origin 比對 → 命中「錯 origin」；
- 拿掉 challenge 的 `used` 條件 → 命中「重放」；
- 讓 grants 的 CHECK 接受沒有 `signer_approval_id` 的 webauthn → 命中「舊資料不升權」；
- 讓 Host 的核准路由接受角色 token → 命中「token 假冒」。

**計畫**：
- 沿用 `go run ./cmd/m1-check --plan …`（`--list` 只列）與 `scripts/verify/container-plan.sh`（斷網、唯讀、白名單），加上 B 層的 Chrome headless；
- 基準是實作提交後的 HEAD，每步都有上限、心跳與證據路徑；
- 預估約 20～30 分鐘，不含 A1 與 Aaron 的實機確認；
- 完整容器七步、全部突變、M1 產包、Unity 重建都不預設執行。

**證據**：
- 放 `artifacts/human-approval/<run>/`（git 忽略）；
- 只保存 ID、hash、旗標、錯誤碼與退出碼；
- 不保存 assertion 原文、credential 私密材料或可重放的完整資料；
- virtual 與實機的結果分開保存，virtual 的結果不能記成實機 PASS。

## 10 決策表（下一片開工前要 Aaron 決定或確認）

| # | 項目 | 推薦 | 替代 | 風險 | 最晚 |
|---|---|---|---|---|---|
| D1 | A1 的實機 authenticator | 先試這台 Mac 的 Touch ID（若有）配 Safari 與 Chrome，再試手機 cross-device；都不行就外接 FIDO2 key（採購由 Aaron 決定）；virtual 只當測試 fixture | 直接採購 FIDO2 key；只用 virtual（A2 就不能宣稱實機） | macOS 12.7.6 對 passkey 與 cross-device 的支援沒有官方確認 | A1 開工前 |
| D2 | 首次登記 | E1：管理者在 Signer 終端機開始與確認，瀏覽器經 Host 轉送 attestation，Signer 驗證（第 3.2 節） | E2：Signer 在儀式期間開一個只在 loopback 的登記頁（Host 不經手，但 Signer 多一個暫時的網路入口） | E1 的剩餘風險：登記當下 Host 若已被控制 | A2 開工前 |
| D3 | RP、origin、入口 | `rp_id=localhost`、唯一 origin `http://localhost:8402`：Host 多一個可信名稱，核准路由只接受它 | 獨立的核准 origin（例如 `:8123`，多一個元件） | 現行 `127.0.0.1` 不能用；要改 Host 的 Host／Origin 檢查 | A2 開工前 |
| D4 | 新依賴 | `go-webauthn/webauthn` v0.18.2，只在 Signer | 只用 `fxamacker/cbor` 加標準庫自寫驗證 | Signer 的可信程式範圍擴大（第 4.8 節） | A1 開工前（A1 也要用） |
| D5 | Signer 介面 | 兩步：challenge 加 verify（A2），付款引用 `approval_id`（P2）；A2 的 `/v1/sign` 仍拒絕 webauthn | P1-b 設計第 4.2 節的單步（assertion 直接放進 sign） | 單步的話，A2 就沒辦法在不付款的前提下取得 Signer 驗證的結果 | A2 開工前 |
| D6 | UV、同步 passkey、計數器 | UV 必須；ES256；`attestation=none`；BE／BS 只記錄、不拒絕；計數器倒退就拒絕並標 `suspect`（都是 0 時接受） | 只接受裝置綁定的 credential（BE=0）；要求 attestation | 同步 passkey 的私鑰可能存在雲端帳號 | A2 開工前 |
| D7 | Host 核准路由的權限 | 不用、也不接受任何角色 token；依據只有 Signer 的驗證 | 要求 viewer token（等於擴大 viewer 的權限） | 任何本機程式都能要求 challenge（只能造成打擾，不能核准）；要限速 | A2 開工前 |

不重問：EOA＋EIP-3009、P1-b 的邊界與結案範圍、本輪設計核准本身。

## 11 修訂對照

| 文件 | 原文 | 本設計 |
|---|---|---|
| roadmap A2 | 「通過後才發 human grant 直送 Signer」 | Host **不簽發**；Signer 自己驗 assertion 並保存已驗證核准；Host 的 grant 是 Signer 結果的鏡像（第 5、6.2 節） |
| roadmap A1 | 本機 `http://localhost:8123`、`rp_id=localhost`；challenge 綁 `approval_session_id`、`intent_hash`、金額、資產、收款人、網路、nonce、期限、`policy_version` | A1 仍用 `:8123`；A2 的 origin 是 `:8402`。challenge 綁 `intent_hash`（它已涵蓋金額、資產、收款人、網路、nonce、時間窗），再加 session、jti、approver 版本、期限與隨機數（第 4.5 節） |
| 交易流程第 3.2 節 | `ApprovalSession` 的 challenge 公式；`AuthorizationGrant` 由 Approval 服務發 | 由第 4.5、4.6 節取代；grant 是 Signer 結果的鏡像 |
| 交易流程第 9 節 | 正式 approver 的登記「由管理流程寫入設定，不由頁面建立」 | 一致；具體化為第 3.2 節的儀式 |
| P1-b 設計第 4.2、4.3 節 | `webauthn` 授權單步帶 assertion；challenge 公式 | 兩步（D5）；公式見第 4.6 節 |
| P1-b 設計第 6.2、6.4 節 | A2 的遷移在 `payment_attempts` 加 `grant_jti` 並放寬方法 CHECK | 移到 P2；A2 只改 `grants` 並新增 `approval_attempts`（第 6.2 節） |

## 12 本輪沒有做的

- 沒有改產品、契約、schema、Signer、Host 或網頁；沒有原型；沒有安裝依賴；沒有建置或測試。
- 沒有在瀏覽器註冊、沒有建立 WebAuthn credential、沒有讀 keychain 或個人 passkey。
- 沒有動 `/opt/apa-signer`、沒有付款金鑰、沒有啟動 Signer、沒有簽署、沒有連鏈。
- WebAuthn credential 與付款私鑰是兩種不同的材料，之後的核准也分開。
- §7.2 第 7 步之後與 §13.4.3 的原文沒有讀到（擷取工具截斷），已在第 1 與第 4.4 節標為待核對。
