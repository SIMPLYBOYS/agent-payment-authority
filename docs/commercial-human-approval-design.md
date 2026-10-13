# 本人核准最小流程（A1 實機可行性、A2 正式接線）：設計

> 2026-10-13，Claude Code。依 vault〈Claude-Code-本人核准最小流程設計派工-d7cbf9f-20261013〉（Aaron 已核准「本人核准最小流程」的設計）。分支 `commercial-p1b`，基準 `d7cbf9f`（工作區只有未追蹤的 `demo`）。
> **狀態：本人核准最小流程設計通過限定技術複核；Aaron 已決定 D1～D7（採推薦方案與明列限制），D8 延後到 P2（A2 用獨立 approvals.db）；已核准另行派工 A1 最小 spike（測試用途、不接付款、不用真實 Agent），A1 spike 已實作（`20677b0`）；A1 spike R1／R2 通過限定技術複核並關閉（`7bb874f`）；A1 可行性切片完成（Done，10/13，限 Chrome＋Google 密碼管理工具／PIN 路徑；Codex 限定核對通過、Aaron 批准）；Touch ID 未驗證、另列，不作為 A2 必要阻擋；A2 已實作（分支 `commercial-a2`）；Codex 訂閱到期後改由 Claude Code 獨立上下文 subagent review，判定 not pass；R1～R6 已修正；第二至五次 review 的 should-fix 都是面板文字，已修正並重跑受影響部分（第五次為全組合掃描，見 commercial-a2.md 第 11 節）；實機登記與操作未驗收；A2 實作、付款金鑰、簽署與交易仍需另行核准；付款未啟用。**（第 23 節）
> 以下為 `f2bf224` 時的狀態，保留原文：本人核准最小流程設計通過限定技術複核；Aaron 已決定 D1～D7（採推薦方案與明列限制），D8 延後到 P2（A2 用獨立 approvals.db）；已核准另行派工 A1 最小 spike（測試用途、不接付款、不用真實 Agent），A1 spike 已實作（`20677b0`）；A1 spike R1／R2 通過限定技術複核並關閉（`7bb874f`）；A1 可行性切片完成（Done，10/13，限 Chrome＋Google 密碼管理工具／PIN 路徑；Codex 限定核對通過、Aaron 批准）；Touch ID 未驗證、另列，不作為 A2 必要阻擋；A2 接線需另行派工、未開工；A2 實作、付款金鑰、簽署與交易仍需另行核准；付款未啟用。（第 22 節）
> 以下為 `7ba7184` 時的狀態，保留原文：本人核准最小流程設計通過限定技術複核；Aaron 已決定 D1～D7（採推薦方案與明列限制），D8 延後到 P2（A2 用獨立 approvals.db）；已核准另行派工 A1 最小 spike（測試用途、不接付款、不用真實 Agent），A1 spike 已實作（`20677b0`）；A1 spike R1／R2 通過限定技術複核並關閉（`7bb874f`）；Chrome＋Google 密碼管理工具／PIN 的 A1 實機操作已完成回報，完整證據待 Codex 最後核對；Touch ID 未驗證，A1 未標 Done；A2 未開工；A2 實作、付款金鑰、簽署與交易仍需另行核准；付款未啟用。（第 21 節）
> 以下為 `33a5bf4` 時的狀態，保留原文：本人核准最小流程設計通過限定技術複核；Aaron 已決定 D1～D7（採推薦方案與明列限制），D8 延後到 P2（A2 用獨立 approvals.db）；已核准另行派工 A1 最小 spike（測試用途、不接付款、不用真實 Agent），A1 spike 已實作（`20677b0`）；A1 spike R1／R2 通過限定技術複核並關閉（`7bb874f`）；Aaron 核准先做 Chrome＋Touch ID 實機操作；第一關登記時 Aaron 選了 Google 密碼管理工具（PIN 驗證，不是 Touch ID），登記成功，本機 Touch ID 尚未試，待 Codex 判讀；實機未驗收，A2 未開工；A2 實作、付款金鑰、簽署與交易仍需另行核准；付款未啟用。（第 20.4 節）
> 以下為 `53601e1` 時的狀態，保留原文：本人核准最小流程設計通過限定技術複核；Aaron 已決定 D1～D7（採推薦方案與明列限制），D8 延後到 P2（A2 用獨立 approvals.db）；已核准另行派工 A1 最小 spike（測試用途、不接付款、不用真實 Agent），A1 spike 已實作（`20677b0`）；A1 spike R1／R2 通過限定技術複核並關閉（`7bb874f`）；Aaron 核准先做 Chrome＋Touch ID 實機操作，尚待操作結果；實機未驗收，A2 未開工；A2 實作、付款金鑰、簽署與交易仍需另行核准；付款未啟用。（第 20 節）
> 以下為 `f8b59b9` 時的狀態，保留原文：本人核准最小流程設計通過限定技術複核；Aaron 已決定 D1～D7（採推薦方案與明列限制），D8 延後到 P2（A2 用獨立 approvals.db）；已核准另行派工 A1 最小 spike（測試用途、不接付款、不用真實 Agent），A1 spike 已實作（`20677b0`）；A1 spike R1／R2 補正完成（`7bb874f`），待 Codex 限定複核；實機未驗收，A2 未開工；A2 實作、付款金鑰、簽署與交易仍需另行核准；付款未啟用。（第 19 節）
> 以下為 `20677b0` 時的狀態，保留原文：本人核准最小流程設計通過限定技術複核；Aaron 已決定 D1～D7（採推薦方案與明列限制），D8 延後到 P2（A2 用獨立 approvals.db）；已核准另行派工 A1 最小 spike（測試用途、不接付款、不用真實 Agent），A1 spike 已實作並通過定向驗證（軟體與 virtual authenticator，不是實機結果），實機登記／驗證待 Aaron 操作；A2 實作、付款金鑰、簽署與交易仍需另行核准；付款未啟用。（第 18 節）
> 以下為 `35cbf3f` 時的狀態，保留原文：本人核准最小流程設計通過限定技術複核；Aaron 已決定 D1～D7（採推薦方案與明列限制），D8 延後到 P2（A2 用獨立 approvals.db）；已核准另行派工 A1 最小 spike（測試用途、不接付款、不用真實 Agent），A1 尚未開工；A2 實作、付款金鑰、簽署與交易仍需另行核准；付款未啟用。（第 17 節）
> 以下為 `e4153ca` 時的狀態，保留原文：本人核准最小流程設計通過限定技術複核（設計一致性核對；R1～R3、decision 綁定與到期規則缺口關閉）；不是實作、全面安全稽核或實機驗收；D1～D8 的決策與執行核准另行處理；A1／A2 未執行，付款未啟用。（第 16 節）
> 以下為 `c798bad` 時的狀態，保留原文：本人核准設計另開條件與 close 到期判斷補正完成，待 Codex 限定複核；R1 原存取權限與 R2 維持關閉；整份設計尚未通過；A1／A2 未執行，付款未啟用。（第 15 節）
> 以下為 `287f340` 時的狀態，保留原文：本人核准設計 close／verify 競爭與 decision 綁定補正完成，待 Codex 限定複核；R1 原存取權限與 R2 維持關閉；A1／A2 未執行，付款未啟用。（第 14 節）
> 以下為 `031c4c7` 時的狀態，保留原文：本人核准設計 R1～R3 與規格補正完成，待 Codex 限定複核；A1 實機與 A2 接線未執行，付款未啟用。（第 13 節）
> 以下為初版（`6c49619`）時的狀態，保留原文：本人核准最小流程設計完成，待 Codex review；A1 實機與 A2 接線未執行，付款未啟用。
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
3. **登記是管理者儀式**：管理者在 Signer 的終端機開始與確認，瀏覽器只負責建立 credential；確認前的 credential 一律不能用。E1 的登記期間必須信任 Host 與其頁面（第 3.2 節，待 Aaron 決定是否接受）。
4. **RP ID `localhost`，唯一 origin `http://localhost:8402`**：現行主持人頁在 `http://127.0.0.1:8402`，IP 不能當 RP ID（第 4.1 節）。
5. **Go 函式庫 `github.com/go-webauthn/webauthn` v0.18.2**（BSD-3-Clause），只放在 Signer。
6. **核准專用的 Signer 組裝**（`apa-signer serve-approvals`）：不讀、不建立付款私鑰，不開付款資料庫，付款入口一律拒絕（第 6.4 節）。
7. **逐路由權限**：看資料與發起核准要有該 consumer 的讀取憑證，核准成功另需 Signer 驗過的 assertion；本人拒絕要以 WebAuthn 驗證裝置，主持人只能「取消」（第 6.3 節）。

**真正阻擋下一步的決策**（第 10 節）：
- D1：A1 的實機 authenticator 路徑；
- D2：首次登記的方式；
- D3：RP／origin 與入口；
- D4：新依賴；
- D5：Signer 介面改成兩步；
- D6：UV、同步 passkey 與計數器的政策；
- D7：核准路由的逐路由權限與本人拒絕的主體；
- D8：核准資料庫與付款資料庫在 P2 的合併方式。

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
| 同上 §7.2 第 7 步之後、§13.4.3 Cryptographic Challenges、§6.1.3 Credential Backup State | Claude Code 初版時擷取工具在這幾段截斷，沒有讀到原文；**Codex 在 `6c49619` 的 review 補讀並核對了 §7.2 與 §13.4.3 的原文**（不是 Claude Code 當輪讀完）。第 4.4、4.5 節的規則依規格推導，並依 Codex 的核對修正 BE／BS、計數器與 `topOrigin`（第 13 節） | 第 4.4、4.5 節 |
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

**A2 的實機驗收入口**：**fake Agent＋合成請購＋真實 authenticator＋付款停用**。
- 現行 `cmd/demo/scenarios.go` 的 `apa.pay.v1` 情境只能在 fake 模式；這只決定 Agent 的來源，不決定驗證器的來源。
- 三個來源各自標示，在畫面與紀錄裡都看得到：
  - Agent：`fake`（不開真實 CLI）；
  - 驗證器：`real-device`（A1 選定的實機）或 `virtual`（CDP，只給自動化）；
  - 付款：`disabled`（核准專用的 Signer 組裝，第 6.4 節）。
- 初版寫「fake 模式下只能用 virtual authenticator」，那會讓 A2 沒有實機入口，已更正。

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

**E1 的待接受風險**（D2，交 Aaron 決定，本文件不替他接受）：
- **E1 的登記期間必須信任 Host 與其頁面。**
- Host 轉送登記選項與 attestation，看得到 challenge，也能替換登記內容；瀏覽器畫面也由 Host 提供。
- 所以「兩邊顯示相同指紋」**不是獨立的可信通道**：被控制的 Host 可以登記自己的軟體 authenticator，再讓頁面顯示同一個指紋。
- 確認碼只能擋住「沒有看到 Signer 終端機」的程式，擋不住控制了 Host 與頁面的程式。
- 「乾淨啟動」不能證明 Host 沒有被控制，本文件不以它作為保證。
- 懷疑 Host 被控制時能做的是：撤銷並重新登記，而重新登記同樣要信任當時的 Host。

**E2 的最小差異與仍依賴的條件**（列給 D2 比較，本輪不實作）：
- 差異：`apa-signer approver enroll serve` 在儀式期間由 Signer 自己開一個只綁 loopback 的登記頁（例如 `http://localhost:8124`；RP ID 仍是 `localhost`，credential 不綁 port），attestation 直接送進 Signer，Host 不經手。儀式結束就關閉。
- 仍依賴：
  - 瀏覽器本身（它以 Host 使用者的身分執行，同 uid 的程式可能透過擴充功能、除錯介面或檔案干預它）；
  - 同機其他程式可以連這個暫時的 loopback 頁（要靠一次性確認碼擋）；
  - 管理者終端機的可信度；
  - Signer 在儀式期間多一個網路入口，與「Signer 不能連網」的方向衝突。
- 所以 **E2 不會自動解決所有本機風險**，只是把「信任 Host 的頁面與轉送」換成「信任瀏覽器與這段時間的本機環境」。

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
- **Signer 也要驗 origin**：Signer 設定 `webauthn.origins = ["http://localhost:8402"]`，驗 client data 的 `origin` 精確相等。本片是**非跨來源**：`crossOrigin` 必須不存在或為 `false`，`topOrigin` 必須**不存在**（出現就拒絕）。
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

第 1～6 步由 Claude Code 在初版核對原文；第 7 步之後與 §13.4.3、§6.1.3 的原文由 **Codex 在 `6c49619` 的 review 補讀核對**（不是 Claude Code 初版讀完），本節的規則已依那次核對修正（第 13 節）：

1. credential ID 在本次 challenge 的 `allowCredentials` 之內，對應一筆 `active` credential，屬於這次 challenge 的 approver。若回應帶 `userHandle`，必須等於該 approver 的 `user.id`。
2. client data：`type` 是 `webauthn.get`；`challenge` 等於本次 challenge；`origin` 在允許清單；`topOrigin` 不存在；`crossOrigin` 不是 `true`。
3. authenticator data：`rpIdHash` 等於 SHA-256("localhost")；**UP 與 UV 都必須設**。備份旗標（W3C §6.1.3）：
   - **BE 是登記時固定的性質**：登記時保存原值；之後每次 assertion 的 BE 必須等於保存值，不同就拒絕（`ASSERTION_INVALID_FLAGS`），並把 credential 標為 `suspect`。
   - **BS 可以合法改變**：BE=0 時 BS 必須是 0；BE=1 時 BS 可以在 0 與 1 之間變化，每次驗證成功後寫回 credential 紀錄，並記在該筆核准裡。
   - BE／BS 都不作為接受或拒絕同步 passkey 的依據（D6）。
4. 簽章：用保存的公鑰，驗證對 `authenticatorData ‖ SHA-256(clientDataJSON)` 的簽章。
5. 計數器（**本產品的政策**，不是規格的唯一要求，D6）：
   - 保存值與收到值都是 0：接受（同步 passkey 常見）；
   - 任一不為 0 而收到值 ≤ 保存值：**拒絕**這份 assertion（`ASSERTION_COUNTER`），並把 credential 標為 `suspect`，交管理者處理；
   - 收到值較大：接受並寫回。
   - 計數器倒退**不一定是金鑰被複製**：同一把 credential 並行簽了兩份 assertion，而後簽的先送到時，先簽的那份到達時就會看起來倒退（第 10 節 D6 列出影響）。
   - **合法重送已完成的結果不經過這一步**：Signer 先以 verify 的 request ID 查到已完成的結果就直接回覆，不重新比計數器（第 7 節）。
6. 以上全部通過，才在同一個寫入交易裡「消耗 challenge＋寫入已驗證核准」（第 6.1 節）。

### 4.5 challenge：誰產生、綁什麼、何時消耗

**產生**：Signer，在 `POST /v1/approvals/challenge` 時：

```text
record = canonical({
  version: 1, challenge_id, random: 32 bytes（crypto/rand）,
  approval_session_id, decision（approve／deny）, consumer_id, wallet_id, intent_hash,
  jti（= paycontract.GrantID(approval_session_id)）, approver_id, approver_version,
  config_hash, expires_at
})
webauthn_challenge = SHA-256("agent-payment-authority/pay/v1/webauthn-challenge" ‖ 0x00 ‖ record)
```

- challenge 由可信的 Signer 產生、保存，驗證時精確比對保存值（§13.4.3 的隨機與不可預測要求由 Codex 核對原文；32 bytes 隨機數是本設計的選擇）；hash 讓 challenge 本身可以對回這一筆。
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
- **DB**（A2 的核准表全部放在 **`approvals.db`**，由 `serve-approvals` 開啟，第 6.4 節；既有付款模式的 `signer.db` 不變）：

| 表 | 欄位（最小） | 唯一性與檢查 |
|---|---|---|
| `enrollments` | `enrollment_id` PK、`approver_id`、`code_hash`、`challenge`、`expires_at`、`state`（pending／finished／expired） | 每個 approver 同時最多一筆 pending |
| `credentials` | `credential_id` PK、`approver_id`、`user_handle`、`public_key`（COSE）、`alg`、`sign_count`、`uv_initialized`、`backup_eligible`、`backup_state`、`aaguid`、`state`（pending／active／suspect／revoked）、`created_at`、`activated_at`、`revoked_at` | `credential_id` 唯一；只有管理者命令改 `state` |
| `approval_challenges` | `challenge_id` PK、`request_id` UNIQUE、`fingerprint`、`challenge` UNIQUE、`record`（canonical）、`approval_session_id`、`decision`、`intent_hash`、`jti`、`approver_id`、`expires_at`、`failures`、`state`（issued／used／expired／closed） | 一個 `jti` 同時最多一筆 issued；`request_id` 冪等 |
| `human_approvals` | `approval_id` PK、`challenge_id` UNIQUE、`verify_request_id` UNIQUE、`jti` UNIQUE、`decision`（approve／deny）、`approval_session_id`、`intent_hash`、`approver_id`、`approver_version`、`credential_id`、`flags`（UP／UV／BE／BS）、`sign_count`、`client_data_hash`、`authenticator_data`、`verified_at`、`expires_at`、`state`（verified／consumed／expired／revoked／abandoned） | 一個 `jti` 最多一筆；`decision=deny` 的列永遠不能被付款消耗；`consumed` 只在 P2 的付款接受交易裡發生 |

- **socket 路由**（固定，沿用 P1-b 的 caller uid、64 KiB、期限）：
  - `POST /v1/approvals/challenge {request_id, approval_session_id, decision, order, host_deadline}` → `{challenge_id, options, display, expires_at}`。`decision` 是 `approve` 或 `deny`（第 6.3 節），進入 challenge 的 `record`；
  - `GET /v1/approvals/challenge?request_id=`：**依 Host 的 request ID 查 challenge**（第 7 節斷點 1）；
  - `POST /v1/approvals/challenges/{challenge_id}/close {close_request_id}`：用於 assertion 遺失後的收尾與改變決定（第 7 節）。規則見下面「close 與 verify 的競爭」；
  - `GET /v1/approvals/challenge?challenge_id=`：查 challenge 的目前狀態；`used` 時一併回 `approval_id` 與 `verify_request_id`；
  - `POST /v1/approvals/verify {verify_request_id, challenge_id, order, assertion}` → `{approval_id, state, decision, intent_hash, approver_id, expires_at}`；
  - `GET /v1/approvals?verify_request_id=`：**依 verify 的 request ID 查結果**；`GET /v1/approvals/{approval_id}`；
  - `POST /v1/approvals/{approval_id}/abandon`（只在 `verified` 時，改成 `abandoned`）；
  - `GET /v1/enrollments/{id}?code=`、`POST /v1/enrollments/{id}`。
- **冪等鍵與指紋**：
  - challenge：鍵是 Host 的 `request_id`；指紋是 `SHA-256("agent-payment-authority/pay/v1/approval-challenge-request" ‖ 0x00 ‖ canonical({approval_session_id, decision, order, host_deadline}))`。同鍵同指紋回同一個 challenge；同鍵不同指紋 `REQUEST_ID_CONFLICT`。
  - verify：鍵是 Host 的 `verify_request_id`（每次送出 assertion 前先寫入 Host）；指紋是 `SHA-256("…/approval-verify-request" ‖ 0x00 ‖ canonical({challenge_id, order_fingerprint, assertion_sha256}))`。同鍵同指紋回保存的結果（成功或拒絕），**不重新驗證**；同鍵不同指紋 `REQUEST_ID_CONFLICT`。
  - 這兩組鍵與付款 attempt 的 request ID 是三套，彼此不共用。
- **close 與 verify 的競爭**（同一列、同一個狀態，條件式寫入決勝）：
  - **verify 的提交**：密碼學驗證可以在交易外先做，但最後的寫入交易必須以 `UPDATE approval_challenges SET state='used' … WHERE challenge_id=? AND state='issued' AND ? < expires_at`（時間在交易內取）影響**恰好一列**，才在**同一個交易**裡寫 `human_approvals` 與 `verify_requests`。影響 0 列時在同一交易重讀：`closed` → 記 `verify_requests(outcome=CHALLENGE_CLOSED)`，**不寫核准**；`used` → 若是同一個 `verify_request_id` 就回保存的結果，否則 `CHALLENGE_USED`；`issued` 但已到期 → 改 `expired`，`CHALLENGE_EXPIRED`。不在驗證前讀一次狀態就當成依據。
  - **close 的提交**：在**同一個寫入交易**裡取一次可信時間 `now`，只處理仍是 `issued` 的紀錄，並區分未到期與已到期（**等於期限也算到期**）：
    - `UPDATE approval_challenges SET state='expired', closed_at=? WHERE challenge_id=? AND state='issued' AND ? >= expires_at`；
    - `UPDATE approval_challenges SET state='closed', closed_at=? WHERE challenge_id=? AND state='issued' AND ? < expires_at`。
    - 兩句的條件互斥，最多一句影響一列；已經是 `used`、`closed` 或 `expired` 的紀錄都不受影響，**`used` 不會因為時間經過而被覆寫**。
    - 不論影響幾列，都在同一交易重讀並**回 challenge 的目前狀態**：
    - `closed`：關閉成功（這次或先前的 close 都一樣，close 冪等）；
    - `used`：verify 已先提交，**close 不覆寫**，回 `approval_id`、`verify_request_id` 與該筆的 `decision`；
    - `expired`：已到期（包括這次 close 才把「時間已到、還是 `issued`」的紀錄寫成 `expired`），沒有 verify 能再提交；
    - challenge 不存在：`CHALLENGE_UNKNOWN`。
  - **close 的冪等與查回**：`close_request_id` 由 Host 在呼叫前寫入；同一個 challenge 可以重送 close，結果都由上面的重讀決定。close 的回覆遺失時，以 `GET /v1/approvals/challenge?challenge_id=` 查回同一個權威狀態。這個查詢遇到「還是 `issued` 但 `now >= expires_at`」時，以受狀態條件保護的短寫入改成 `expired` 再回覆（同 P1-b 查詢記錄到期的做法），不回 `issued`。
  - 因此 `closed` 之後不可能再出現核准；`used` 之後 close 一定看得到那筆核准。
- **新增 `verify_requests` 表**：`verify_request_id` PK、`fingerprint`、`challenge_id`、`outcome`（`verified` 或固定拒絕碼）、`approval_id` NULL、`received_at`。拒絕也記錄，所以同一個請求的重送回同一個拒絕。拒絕不消耗 challenge；同一個 challenge 最多 3 次失敗，之後改成 `closed`（`CHALLENGE_CLOSED`）。
- **新錯誤碼**：`APPROVER_UNKNOWN`、`CREDENTIAL_REVOKED`、`CREDENTIAL_PENDING`、`CHALLENGE_UNKNOWN`、`CHALLENGE_EXPIRED`、`CHALLENGE_USED`、`CHALLENGE_CLOSED`、`PAYMENT_MODE_OFF`、`ASSERTION_INVALID`（細分 `_TYPE`、`_ORIGIN`、`_RPID`、`_FLAGS`、`_SIGNATURE`、`_COUNTER`）、`APPROVER_CHANGED`、`ENROLLMENT_CODE`；沿用 `CONSUMER_MISMATCH`、`ORDER_INVALID`、`REQUEST_ID_CONFLICT`、`SIGNER_TIMEOUT`。
- **付款入口在兩種模式的回覆**（分開列，測試也分開）：
  - `serve`（既有付款模式，不變）：`/v1/sign` 收到 `webauthn` 仍回 `HUMAN_APPROVAL_UNAVAILABLE`；其他付款規則照 P1-b（正式組裝 `RESERVATION_UNAVAILABLE`）。P2 才改成接受 `{method: webauthn, approval_id}`，並在接受交易裡重驗與消耗（第 5 節）。
  - `serve-approvals`（A2 新增）：`/v1/sign` 與所有 attempts 路由一律回 409 `PAYMENT_MODE_OFF`，不看授權方法。
  - 兩者都不加繞過預留的開關，正式預留驗證器維持 fail-closed。
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
| `approval_attempts`（新） | `request_id` PK（Host 在呼叫 Signer 前先寫入）、`approval_session_id` FK、`decision`、`challenge_id` UNIQUE NULL、`verify_request_id` UNIQUE NULL（送出 assertion 前先寫入）、`signer_approval_id` UNIQUE NULL、`state`（requested／challenged／verifying／verified／refused／unknown／lost／expired／closed）、`error_code`、時間；同一個會話同時最多一筆非終態的 attempt | 新表 |
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

### 6.3 Host 的核准路由與逐路由權限（D7）

Host 與 Origin 必須精確等於 `localhost:8402`；POST 要 JSON；限速。**這些檢查不是身分驗證**：本機程式可以自行填 Host 與 Origin。身分來自下表的憑證；核准成功的唯一依據仍是 Signer 驗過的 assertion。

| 路由 | 呼叫者 | 身分來源 | consumer／session 綁定 | 可取得的資料 | 可造成的狀態變化 | 未授權時 |
|---|---|---|---|---|---|---|
| `GET /api/approvals?consumer=` 列出待核准、`GET /api/approvals/{session_id}` 看摘要與狀態 | 該 consumer 的頁面 | **viewer 憑證**（`hostcreds.js` 依 consumer 持有，既有角色）；或 host 憑證（全域唯讀，沿用既有規則） | 會話的 consumer 由 Store 決定；viewer 只看得到自己 consumer 的會話 | 摘要（Signer 的 `display` 若已取得）、會話與 attempt 狀態；**不含** credential 選項 | 無 | 沒有憑證 401；別的 consumer 一律 404（不洩漏存在與否，同 `record`） |
| `POST /api/approvals/{session_id}/challenge {request_id, decision}` | 同上 | **該 consumer 的 viewer 憑證**；host、operator、模擬核准者憑證都不接受 | 會話必須屬於 viewer 的 consumer，且仍是 `pending`；`decision` 必須與目前非終態 attempt 相容（第 7 節） | 權威的 attempt ID、challenge 選項（`allowCredentials`）、`display`，以及 Signer 回傳的 `consumer_id`、`approval_session_id`、`intent_hash`、`decision`、期限 | 寫 `approval_attempts`（`requested` → `challenged`）或共用相容的 attempt；**不改會話狀態** | 401／404；會話不是 `pending` 409；相反的 `decision` 409 `DECISION_CONFLICT` |
| `POST /api/approvals/{session_id}/attempts/{request_id}/cancel`（取消這次驗證） | 同上 | 該 consumer 的 viewer 憑證 | 同上；attempt 必須屬於這個會話 | close 的權威結果 | Host 以 close 規則結束舊 challenge；不改會話狀態（會話仍 `pending`，除非 close 回 `used` 而那筆已完成） | 401／404 |
| `POST /api/approvals/{session_id}/assertion {verify_request_id, assertion}` | 同上 | viewer 憑證**加上** Signer 對 assertion 的驗證 | 同上；assertion 必須屬於這個會話目前 attempt 的 challenge | 結果 | Host 只依 **Signer 回傳的 `decision`**，而且它必須等於 attempt 的 `decision`（不一致就拒絕並記錄，不落地），Signer 驗證通過才寫：`approve` → 會話 `approved`＋webauthn grant＋事件；`deny` → 會話 `denied`（`APPROVAL_DENIED`，`resolved_by=webauthn:<approver_id>`）。**viewer 憑證單獨不能造成任何核准或拒絕** | 401／404；assertion 被拒時照 Signer 的代碼，不改會話 |
| `GET /api/approvals/{session_id}/attempts/{request_id}` 恢復查詢 | 同上 | viewer 憑證 | 同上 | attempt 狀態；Host 依需要向 Signer 查（第 7 節） | 只依 Signer 的權威結果推進 attempt，不推進會話以外的東西 | 401／404 |
| 取消 task（既有 `POST /api/host/tasks/{id}/cancel`） | 主持人 | operator 憑證（既有） | task | — | 照 P1-a：關閉會話為 `withdrawn`，事件寫取消，**不寫成本人拒絕** | 既有規則 |

**WebAuthn 啟動前的核對**（頁面）：challenge 回覆的 `consumer_id`、`approval_session_id`、`intent_hash`、`decision` 必須與這次使用者操作一致（沿用 `approvalTarget`／`sameTarget`，再加上 `decision`）；不一致就停止，**不彈出裝置提示**。

**落地**：`approve` 才寫 webauthn grant；`deny` 只把會話改成 `denied`，**不發付款 grant**。後端結果不由前端按鈕文字決定。

**最小原則**：
- 看資料沿用既有、以 consumer 為範圍的讀取憑證。這**不**讓 viewer 有核准權限：viewer 憑證只能發起 challenge 與轉送 assertion，結果完全取決於 Signer 的驗證。
- **本人拒絕**也要以 WebAuthn 驗證裝置：challenge 的 `decision=deny`，進入 `record`，Signer 驗證後記為一筆 `decision=deny` 的核准紀錄，永遠不能被付款使用。拒絕不使用「核准付款」的 assertion，兩者的 challenge 不同。
- 本人無法驗證時，主持人只能以 operator 憑證**取消** task，名稱與事件照實寫取消。
- 模擬核准者（P1-a，fake only）維持原樣，只能產生 `simulated` 的核准或拒絕，與本人核准分開。

**未來測試**（第 9 節）要證明：
- 沒有憑證或跨 consumer：讀不到也動不了；
- 正確的讀取憑證不能代替 assertion；
- 未授權的拒絕不改變會話狀態。

### 6.4 核准專用的 Signer 組裝（`apa-signer serve-approvals`）

現行 `serve` 的 `assemble` 會呼叫 `signer.LoadKeys`，讀每個設定錢包的加密私鑰；`signer.NewCore` 缺任何一把就拒絕啟動。設計又要求 approver 對應設定裡的 wallet，所以 A2 需要一條**不碰付款私鑰**的組裝路徑：

| 項目 | `serve`（付款，不變） | `serve-approvals`（A2 新增） |
|---|---|---|
| 入口 | `apa-signer serve --dir DIR` | `apa-signer serve-approvals --dir DIR` |
| 設定 | `signer.json` | 同一個 `signer.json`：`wallets` 只取公開資訊（`wallet_id`、`consumer_id`、`chain_id`、`address`），加上 `approvers`、`webauthn`；不需要 `delegations` |
| 金鑰 | `LoadKeys` 讀 `keys/*.key`，`/dev/tty` 讀密語；缺 key 拒絕 | **不讀** `keys/`、不問密語、不解鎖金鑰庫、不建立金鑰 |
| 組裝函式 | `assemble` → `NewCore`（付款） | `assembleApprovals` → `NewApprovalCore`：只有 approver 登記、challenge、assertion 驗證與查詢 |
| 資料庫 | `signer.db`（attempts；啟動時恢復未完成的付款） | **另一個檔案** `approvals.db`（同一個 `0700` 目錄，`0600`）；**不開** `signer.db`，所以不會觸發付款恢復 |
| 路由 | P1-b 的付款路由 | 第 6.1 節的核准與登記路由；`POST /v1/sign` 與 attempts 路由回 409 `PAYMENT_MODE_OFF` |
| health | `{"state":"unlocked"}` | `{"state":"approvals-only","payment":"unavailable"}`；畫面據此顯示「付款未啟用」，不顯示「付款 Signer 已解鎖」 |
| 預留 | `Unavailable`（P2 前） | 不涉及 |

- `serve` 的行為完全不變，仍然缺 key 就拒絕；不新增任何能略過預留驗證的設定。
- 管理命令（`approver enroll|revoke|list`）直接開 `approvals.db`（SQLite WAL，`busy_timeout`），可以在 `serve-approvals` 執行時使用。
- **P2 的合併**（D8，P2 時決定）：付款要在接受交易裡消耗 `human_approvals`。推薦屆時讓 `serve` 同時開 `approvals.db`，在同一個程序裡以兩階段順序處理；或把核准表搬進 `signer.db`。兩者都要另行設計，A2 不先做。
- 部署沿用 P1-b 的 `/opt/apa-signer`：只多 `approvals.db` 與設定段落，帳號與權限不變。

## 7 錯誤、競爭與恢復

**持久化順序**（Signer 與 Host 是兩個資料庫，**沒有**跨程序的原子交易）：
1. Host 寫 `approval_attempts(requested, request_id, decision)`，然後呼叫 Signer 的 challenge。
2. Signer 以 `request_id` 冪等，存 challenge。
3. Host 寫入 `challenge_id`（`challenged`）。頁面在使用者按下主要按鈕時才呼叫 `navigator.credentials.get()`，不自動彈出。
4. 收到 assertion 後，Host 先寫 `verify_request_id`（`verifying`），再呼叫 verify。
5. Signer 在一個交易裡：記 `verify_requests`；成功時消耗 challenge＋寫 `human_approvals`。
6. Host 在一個交易裡寫會話結果＋grant（只有 approve）＋事件，attempt 改 `verified`。

**權威結果**：challenge 與 verify 的結果在 Signer；會話的結果在 Host，只依 Signer 的結果推進。

**恢復表**：

| # | 中斷位置 | Host 已存 | Signer 已存 | 權威狀態 | 允許的查詢／冪等重送 | 畫面 | 如何結束 |
|---|---|---|---|---|---|---|---|
| 1 | Host 寫了 `requested`，還沒呼叫或呼叫沒有回應 | `request_id`、`decision` | 不確定 | 不確定（**不是**「確定未受理」） | `GET /v1/approvals/challenge?request_id=`：查到就補寫 `challenge_id`；**確定查無**（讀取成功、404）就以同一個 `request_id`、同內容重送 POST；讀取失敗就有界重試 | 「準備驗證中…」 | 成功 → 第 3 步；到會話期限仍無法確認 → `expired` |
| 2 | **斷點 1**：Signer 存了 challenge，回覆遺失 | `request_id` | challenge `issued` | Signer | 依 `request_id` 查到同一個 challenge，Host 補寫 `challenge_id`；不產生第二個 challenge | 「可以繼續驗證」（使用者按主要按鈕才彈出） | 繼續第 3 步 |
| 3 | 頁面拿到 challenge，使用者沒完成或關閉頁面 | `challenge_id` | `issued` | Signer | 查 challenge 狀態 | 「驗證未完成」 | challenge 到期 → attempt `expired`；使用者按「重新核准」開新的 attempt（新 `request_id`） |
| 4 | **斷點 2**：Host 存了 `verifying`，之後當機，不知道 assertion 有沒有送到 | `challenge_id`、`verify_request_id`；**沒有** assertion（Host 不保存原文） | 不確定：可能沒收到、可能 verify 還在途中、可能已提交 | 不確定。**查無不等於確定未收到**：查詢當下查無之後，verify 仍可能提交 | 先依 `verify_request_id` 查；查到就照第 6 或 7 列。讀取成功且查無時，Host 先寫 `close_request_id`，再送 close，**依 close 回的權威狀態處理**（見下表）；讀取失敗就有界重試，不當成查無 | 等 close 的結果時：「正在確認這次驗證…」 | 見下面「close 與 verify 的先後」 |
| 5 | assertion 送到、Signer 受理狀態不明（逾時） | `verify_request_id` | 可能有 | 不確定 | 依 `verify_request_id` 查；讀取失敗就有界重試；**不重送新的 assertion** | 「結果確認中，請不要重複操作。」 | 查到 → 第 6 或第 7 列；到會話期限仍不明 → 會話 `expired`，attempt `unknown`；之後查到 `verified` 時 Host 呼叫 `abandon` |
| 6 | Signer 已驗證，回覆遺失 | `verify_request_id` | `verify_requests` 與 `human_approvals(verified)` | Signer | 依 `verify_request_id` 查到結果；Host 有原 assertion 時，以同鍵同內容重送也回同一結果，**不重新驗證、不再比計數器** | 成功文案 | Host 寫會話結果＋grant＋事件 |
| 7 | Signer 拒絕，回覆遺失 | 同上 | `verify_requests(outcome=拒絕碼)` | Signer | 同上，回同一個拒絕 | 失敗文案 | attempt `refused`；challenge 未達失敗上限時可以重試同一個 challenge（新 `verify_request_id`）。**`refused` 只是 Host 這次 attempt 的終態，不表示 Signer 的 challenge 已失效**：challenge 仍可能是 `issued` |
| 8 | Signer 已驗證，Host 寫結果的交易失敗 | `verifying` | `verified` | Signer | 重試 Host 的交易；必要時依第 6 列再查一次 | 「結果確認中」 | 寫入成功 → 完成；會話已到期或已取消 → Host 呼叫 `abandon` |
| 9 | 已完成 | `verified` | `verified` | 兩邊一致 | 任何重送回同一結果 | 成功或拒絕文案 | — |
| 10 | 已過期 | 會話 `expired` 或 attempt `expired` | challenge `expired` | 各自 | 查詢回原狀態 | 「已過期（後端時間為準）」 | 終態 |

- **查詢失敗不等於查無**：Signer 與 Host 的查詢都區分「讀取成功且查無」與「讀取失敗」；後者只能重試或維持 `unknown`，不能據以重送或關閉。
- **保存失敗**：沿用 P1-b 的 R3 規則。Signer 結果不明時記 `unknown`；連 `unknown` 都存不進去時，同時回報遠端結果不明與本地未保存，不重試付款、不換 ID。
- **「確認中」有終點**：最晚到會話期限（`sessionDeadline`）就轉成 `expired` 或 `unknown`，畫面顯示已結束，不會永遠「確認中」，也不會自動再彈出 WebAuthn。
- **不為恢復保存可重放的材料**：Host 不保存 assertion 原文，也不把它或登記確認碼寫進 log 或證據。所以斷點 2 的恢復方式是關閉舊 challenge、請使用者重新核准，而不是重送。推薦**不新增**私有保存：若之後需要，最小資料是 assertion 原文（加密），只放 Host 的 `0700` 目錄、`0600`，在 verify 成功、關閉或會話期限到達時刪除；本設計不採用。

**close 與 verify 的先後**（第 4 列與改變決定時都用這張表）：

| 先後結果 | Signer 規則 | Host 與畫面 |
|---|---|---|
| close 先成功 | 條件式寫入把 `issued` 改成 `closed`；晚到的 verify 在自己的寫入交易裡看到 `closed`，記 `CHALLENGE_CLOSED`，**不寫核准** | close 回 `closed` 之後，Host 才把 attempt 標 `lost`，畫面：「驗證結果沒有送達，這次沒有核准。請按『重新核准』。」不自動彈出；使用者按了才開新的 attempt |
| verify 先成功 | 核准、challenge 改 `used` 與 `verify_requests` 在同一個交易提交；close 不覆寫 `used`，回 `approval_id`、`verify_request_id` 與 `decision` | Host 依回傳的 `approval_id` 查回並核對該筆（會話、`intent_hash`、`decision` 與 attempt 一致），照第 6 列落地；**不標 `lost`、不顯示「這次沒有核准」、不開第二筆**。若 Host 會話此時已取消或過期，不把結果寫回已關閉的會話，approve 的核准呼叫 `abandon` |
| challenge 已到期 | close 回 `expired`；verify 在期限後不能提交 | attempt 標 `expired`：「已過期（後端時間為準）；這次驗證沒有完成。」 |
| close 回覆遺失或查詢失敗 | close 冪等，可以重送；`GET /v1/approvals/challenge?challenge_id=` 查回權威狀態 | 結果未知時**不開新的 attempt**，畫面：「這次驗證的結果還無法確認。」有界重試；到會話期限就結束等待，畫面：「無法確認這次驗證的結果；這筆已依後端期限關閉。」**不寫成「未核准」**。之後查到 `used` 時照「verify 先成功」核對並 `abandon` |

| 其他情況 | 規則 |
|---|---|
| 核准與拒絕 | 兩者都要 WebAuthn（第 6.3 節）。拒絕或期限先發生後，Host 不再接受 verify 的結果；若 Signer 已 `verified` 一筆 approve，Host 呼叫 `abandon` 並記錄 |
| 到期 | Host 的會話期限照 `sessionDeadline`（決策時間＋terms 期限與 `valid_before` 取小）；Signer 的 challenge 期限只能更短。兩邊都由可信時鐘決定，呼叫者不能延長 |
| 取消 task | 會話 `withdrawn`；已驗證未用的核准由 Host 呼叫 `abandon`。Signer 沒有可驗證的撤回清單（沿用 P1-b），曝險以 `approval_ttl` 與 `valid_before` 為界 |
| 兩個分頁與決定綁定 | 同一個會話同時最多一筆非終態 attempt，attempt 帶自己的 `decision`。第二個要求的處理：**同一個 consumer、會話、`intent_hash` 與同一個 `decision`** 才共用目前的 attempt，Host 回 200 與**權威的 attempt ID**（目前那筆的 `request_id`）與它的 challenge，不另建 attempt；這和「同一個 `request_id`、同內容的冪等重送」分開記錄。**相反的 `decision`**：回 409 `DECISION_CONFLICT`，附目前 attempt 的 `decision` 與狀態，**不回另一種 challenge、不改使用者的選擇**。要改變決定（或在任何舊 attempt 之後另開），**依 Signer 對舊 challenge 的權威結果**，不依 Host attempt 的狀態：使用者先按「取消這次驗證」，Host 以第 6.1 節的 close 規則處理舊 challenge——close 回 `closed` 或 `expired`，而且會話仍是 `pending` 且未到期，才能開新的 attempt；回 `used` 就照既有結果；結果未知就不能另開。Host attempt 已是終態（例如 `refused`）也一樣要先完成 close |
| 不同 ID 重用 assertion | assertion 的 client data 帶的是原 challenge，對不上別的 challenge（`ASSERTION_INVALID_CHALLENGE`）；原 challenge 已 `used` 或 `closed` 就是 `CHALLENGE_USED`／`CHALLENGE_CLOSED` |
| 晚到的舊回覆 | Host 只接受目前 attempt 的 `challenge_id` 與 `verify_request_id`；網頁沿用 `hostcreds` 的世代與 `approvalTarget`／`sameTarget`，舊世代的回覆丟棄 |
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

**OS 部署**：Signer 只多設定段落與 `approvals.db`（第 6.4 節），沿用 P1-b 的 `/opt/apa-signer` 配置，不重開 OS 部署。新的 `webauthn` 設定與 approvers 由管理者寫入，未來驗收只看設定檔與 `approvals.db` 的權限，以及 `serve-approvals` 沒有開 `keys/` 或 `signer.db`。

## 8 使用者體驗

場景仍是主畫面，技術證據留在抽屜。沿用 P1-a 的「需要你處理」卡與已接受的核准概念稿（`mockups/commerce/2-approval.html` 的正式模式）。

**核准表**（Signer 的 `display`，不只顯示 hash）：客人、商品、收款人（可信名稱與地址）、付款帳戶、金額與資產、網路、委託範圍（剩餘預算、`allow_max`）、後端期限（倒數只是提示）。

**動作**：
- 主要按鈕「用已登記的裝置核准」，啟動 WebAuthn；
- 次要按鈕「用裝置拒絕這筆」，同樣啟動 WebAuthn（`decision=deny`），不使用核准付款的 challenge（第 6.3 節）；
- 「取消這次驗證」：只在有進行中的 attempt 時出現，用第 7 節的 close 規則結束它；要改變決定（核准改拒絕或反過來）必須先做這一步；
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
| 結果尚未確認 | 「結果確認中，請不要重複操作。」 | 只查詢同一個 ID，不再發 challenge；最晚到後端期限就結束（第 7 節） |
| 驗證結果沒有送達 | 「驗證結果沒有送達，這次沒有核准。請按『重新核准』。」 | **只在 close 回 `closed` 之後**才顯示；終態 `lost`；不自動彈出，使用者按了才開新的 attempt |
| 正在確認這次驗證 | 「正在確認這次驗證…」 | 等 close 或查詢的權威結果；不開新的 attempt |
| 這次驗證的結果無法確認 | 「這次驗證的結果還無法確認。」；到期後：「無法確認這次驗證的結果；這筆已依後端期限關閉。」 | 不寫成「未核准」；不開新的 attempt |
| 另一個視窗選了不同的決定 | 「另一個視窗正在{核准／拒絕}這筆。要改變決定，請先『取消這次驗證』。」 | `DECISION_CONFLICT`；不彈出裝置提示 |
| 回覆與這次操作不符 | 「回傳的資料和你這次的操作不一致，已停止，沒有要求裝置驗證。」 | 頁面核對失敗；不彈出 |
| 已拒絕 | 「你已拒絕這筆（已驗證裝置）。」 | 會話 `denied` |
| 主持人取消 | 「主持人已取消這個任務；這筆沒有核准，也不是你拒絕的。」 | 會話 `withdrawn` |
| 成功 | 「本人核准已驗證，付款未啟用。」 | 不顯示已付款或已交付 |
| 已在另一個視窗核准 | 「這筆已在另一個視窗核准。」 | 依 Store |

- **身分與焦點**：切換客人、刷新或晚到的回覆都不能把 A 或舊會話的操作轉到 B 或新會話。沿用 `hostcreds.js` 的世代、`adapter.js` 的 `approvalTarget`／`sameTarget`，以及 P1-a R1 的焦點規則（重畫後只把焦點還給同一筆，否則交給標題）。不另造沒有身分綁定的核准元件。
- **來源標示**（三個來源分開，第 2 節）：
  - Agent：fake 或 live；fake Agent 不等於模擬 WebAuthn，也不強迫使用 virtual authenticator；
  - 驗證器：實機寫「已登記的裝置」，virtual 在畫面與紀錄都寫「virtual authenticator（測試用）」，不能冒稱真人實機；
  - 付款：`serve-approvals` 時寫「付款未啟用」；
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
| 主持人或角色憑證假冒本人 | 帶 host／operator 或模擬核准者憑證打 challenge 與 assertion 路由 | 這些憑證不被接受；viewer 憑證被接受但沒有 assertion 就不可能 `verified` | I | 20 s |
| 錯 origin、RP ID、type | `http://127.0.0.1:8402`、其他 port、`webauthn.create` | `ASSERTION_INVALID_ORIGIN`／`_RPID`／`_TYPE` | U | 20 s |
| 錯 challenge | 另一個 challenge 的 assertion | `ASSERTION_INVALID_CHALLENGE` | U、I | 10 s |
| UV 沒設、UP 沒設 | 改旗標 | `ASSERTION_INVALID_FLAGS` | U | 10 s |
| 計數器倒退 | 保存 5、收到 3 | `ASSERTION_COUNTER`，credential 變 `suspect` | U | 10 s |
| 跨 consumer、跨付款、內容變更 | 請購的任一付款綁定欄位改一個 | `intent_hash` 不同：challenge 對不上 | U、I | 30 s |
| 重放 | 同一 challenge 第二份 assertion；同一份 assertion 給另一個 challenge | `CHALLENGE_USED`／`_CHALLENGE` | U、I | 20 s |
| 到期邊界 | `now == expires_at` | 拒絕（嚴格 `<`） | U | 10 s |
| 取消競爭 | verify 成功與 Host 取消先後兩種 | 取消先：不核准、Signer 核准被 `abandon`；核准先：取消照 P1-a 規則 | S、I | 1 m |
| 兩個分頁，同一個決定 | 兩個頁面都按「核准」 | 共用同一個 attempt（回權威的 attempt ID），只有一筆核准；另一份 assertion `CHALLENGE_USED` | I、B | 1 m |
| 核准進行中按拒絕 | attempt 是 `approve`，另一頁按拒絕 | 409 `DECISION_CONFLICT`；沒有回 approve 的 challenge 給拒絕頁；沒有彈出 | I、B | 30 s |
| 拒絕進行中按核准 | attempt 是 `deny`，另一頁按核准 | 409 `DECISION_CONFLICT`；同上 | I、B | 30 s |
| 回覆的決定與按鈕不符 | 測試替身讓 challenge 回覆的 `decision` 與按鈕相反 | 頁面停止、不彈出；Host 落地時也因 `decision` 不一致而拒絕 | W、I | 30 s |
| 已 refused、challenge 仍 issued 時改變決定 | 舊 attempt 是 `refused`（assertion 被拒），Signer 的 challenge 仍是 `issued`；使用者改按相反的決定 | 不直接另開：先 close；close 回 `closed` 後才開新的 attempt；之後同一份舊 challenge 的任何 verify 都是 `CHALLENGE_CLOSED` | I | 30 s |
| close 在期限前一刻 | `now = expires_at − 1` | 改成 `closed` | U | 10 s |
| close 剛好在期限 | `now == expires_at` | 改成 `expired`、回 `expired`（不是 `closed`） | U | 10 s |
| `used` 之後才到期 | verify 已提交（`used`），之後 `now >= expires_at` 再送 close 或查詢 | 仍回 `used` 與 `approval_id`，不被改成 `expired` 或 `closed` | U | 10 s |
| 改變決定時舊 verify 晚到 | 按「取消這次驗證」，close 先成功後舊 assertion 才送到 | 舊 verify `CHALLENGE_CLOSED`，沒有核准；新的 attempt 照常 | I | 30 s |
| 查無後 close 先成功 | verify 查無 → close → 晚到的 verify | close 回 `closed`；verify `CHALLENGE_CLOSED`；沒有核准紀錄；attempt `lost` | I | 30 s |
| 查無後 verify 先成功 | verify 查無 → verify 提交 → close | close 回 `used` 與 `approval_id`；Host 不標 `lost`、不開第二筆，照第 6 列落地 | I | 30 s |
| close 回覆遺失 | close 已提交，回覆中斷 | 重送 close 或依 `challenge_id` 查詢，得到同一個 `closed` | I | 30 s |
| close 或查詢失敗、期限到達 | Signer 讀取失敗直到會話期限 | 畫面「無法確認……已依後端期限關閉」，不寫「未核准」；沒有新的 attempt；之後查到 `used` 就 `abandon` | I、W | 1 m |
| deny 不發付款 grant | `decision=deny` 驗證通過 | 會話 `denied`，沒有 grant 列；那筆 Signer 紀錄不能被付款消耗 | S、I | 20 s |
| 相同請求重送 | 同 `request_id`、同內容 | 同一個 challenge／核准 | U、I | 20 s |
| 保存失敗與回覆遺失 | 注入 Store 寫入失敗、Signer 回覆中斷 | `unknown`，同 ID 恢復；保存失敗同時回報 | I | 1 m |
| 模擬 grant 仍不能付款 | simulated grant 送 `/v1/sign` | `GRANT_METHOD_NOT_ACCEPTED`（不變） | U | 10 s |
| `serve` 的付款規則不變 | `serve` 收到 webauthn 的 `/v1/sign` | `HUMAN_APPROVAL_UNAVAILABLE`；正式組裝 `RESERVATION_UNAVAILABLE`（不變） | U | 10 s |
| `serve-approvals` 沒有付款入口 | `serve-approvals` 收到 `/v1/sign`（任何授權方法）與 attempts 路由 | 409 `PAYMENT_MODE_OFF` | I | 10 s |
| 舊資料不升權 | schema 4 檔（含 simulated grant、delegation attempt）升 5 | simulated 不能變 webauthn（CHECK）；復原檢查通過；delegation 路徑不建會話 | S | 1 m |
| 畫面與綁定一致 | Signer 的 `display` 與請購一致；改請購後重開 | 顯示新的；舊核准不適用 | W、B | 1 m |
| 焦點與身分世代 | 切換客人、刷新、晚到回覆 | 舊回覆丟棄；焦點規則同 P1-a R1 | W、B | 1 m |
| 錯誤與逾時不誤報成功 | 使用者取消、失敗、逾時 | 各自的文案；沒有成功字樣 | W、B | 1 m |
| 實機路徑（A1） | 一次註冊、一次 assertion、錯 challenge、錯 origin | 記錄實際結果；**不是 A2 的 PASS** | M | 30～60 m |
| **R1** 沒有憑證或跨 consumer | 不帶憑證、帶 B 的 viewer 憑證讀或操作 A 的會話 | 401／404；會話與 attempt 不變 | I | 30 s |
| **R1** 讀取憑證不能代替 assertion | 正確 viewer 憑證、假造或缺少 assertion | Signer 拒絕；會話不變 | I | 20 s |
| **R1** 未授權的拒絕 | 沒有憑證、別人的憑證、host／operator 憑證送拒絕 | 不改狀態；主持人只能以取消結束，事件寫取消 | I | 30 s |
| **R1** 本人拒絕 | `decision=deny` 的 assertion | 會話 `denied`；那筆 deny 紀錄不能被付款使用 | U、I | 20 s |
| **R2** 沒有任何付款 key 仍可核准 | `keys/` 不存在或為空、`serve-approvals` | 啟動、登記、challenge、verify、查詢都可用 | I | 1 m |
| **R2** 沒有讀 key、沒有付款恢復 | 同上，`signer.db` 不存在或有未完成的 attempt | 沒有開 `keys/` 或 `signer.db`（以檔案存取紀錄或測試替身確認）；`signer.db` 原樣 | I | 1 m |
| **R2** 付款路由拒絕 | `serve-approvals` 收到 `/v1/sign` 與 attempts 路由 | 409 `PAYMENT_MODE_OFF`；health 是 `approvals-only` | I | 20 s |
| **R2** 原付款模式不放寬 | `serve` 缺一把 key | 照舊拒絕啟動 | U | 10 s |
| **R2** 來源標示不混用 | fake Agent＋virtual、fake Agent＋實機 | 畫面與紀錄的三個來源標示各自正確 | W、B、M | 1 m |
| **R3** 斷點 1 | challenge 存了、回覆遺失 | 依 `request_id` 查回同一個 challenge，沒有第二個 | I | 30 s |
| **R3** 斷點 2 | `verifying` 後 assertion 沒送到就當機 | 確定查無 → challenge `closed`、attempt `lost`；畫面要求使用者重新核准；舊 assertion 之後被拒 | I | 1 m |
| **R3** 狀態查詢失敗 | 查詢時 Signer 讀取失敗 | 維持 `unknown` 並有界重試，不當成查無、不重送 | I | 30 s |
| **R3** 保存失敗 | Host 寫 `unknown` 失敗 | 同時回報兩件事，不換 ID | I | 20 s |
| **R3** 合法重送 | 同 `verify_request_id`、同 assertion | 回保存的結果，不重新比計數器、不建第二筆核准 | U、I | 20 s |
| **R3** 明確重新操作 | `lost` 之後按「重新核准」 | 新 attempt、新 challenge；沒有自動彈出 | W、I | 30 s |
| BE 改變 | 登記 BE=1，assertion BE=0 | `ASSERTION_INVALID_FLAGS`，credential `suspect` | U | 10 s |
| BS 合法改變 | BE=1，BS 0 → 1 | 接受並寫回 | U | 10 s |
| `topOrigin` 出現 | client data 帶 `topOrigin` | 拒絕 | U | 10 s |

**反向檢查**（少量、可指認）：
- 拿掉 challenge 比對 → 命中「錯 challenge」；
- 拿掉 UV 要求 → 命中「UV 沒設」；
- 拿掉 origin 比對 → 命中「錯 origin」；
- 拿掉 challenge 的 `used` 條件 → 命中「重放」；
- 讓 grants 的 CHECK 接受沒有 `signer_approval_id` 的 webauthn → 命中「舊資料不升權」；
- 讓 Host 的核准路由不檢查 viewer 憑證的 consumer → 命中「R1 跨 consumer」；
- 讓 viewer 憑證就能把會話改成 `approved`（略過 Signer 的結果）→ 命中「R1 讀取憑證不能代替 assertion」；
- 讓 `serve-approvals` 註冊付款路由 → 命中「R2 付款路由拒絕」；
- 讓 Host 把「verify 查詢讀取失敗」當成查無 → 命中「R3 狀態查詢失敗」；
- 拿掉 verify 寫入交易的 `state='issued'` 條件 → 命中「查無後 close 先成功」；
- 讓 Host 對相反的 `decision` 回目前的 challenge → 命中「核准進行中按拒絕」。

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
| D2 | 首次登記（**交 Aaron 決定，不替他接受**） | E1：管理者在 Signer 終端機開始與確認，瀏覽器經 Host 轉送 attestation，Signer 依 §7.1 驗證（第 3.2 節）。接受 E1 就是接受「登記期間必須信任 Host 與其頁面」 | E2：儀式期間由 Signer 自己開只綁 loopback 的登記頁，Host 不經手；仍要信任瀏覽器、這段時間的本機環境與一次性確認碼，且 Signer 暫時多一個網路入口 | E1：Host 被控制時可以登記它自己的 authenticator，兩邊相同的指紋也擋不住；E2：不會自動解決所有本機風險 | A2 開工前 |
| D3 | RP、origin、入口 | `rp_id=localhost`、唯一 origin `http://localhost:8402`：Host 多一個可信名稱，核准路由只接受它 | 獨立的核准 origin（例如 `:8123`，多一個元件） | 現行 `127.0.0.1` 不能用；要改 Host 的 Host／Origin 檢查 | A2 開工前 |
| D4 | 新依賴 | `go-webauthn/webauthn` v0.18.2，只在 Signer | 只用 `fxamacker/cbor` 加標準庫自寫驗證 | Signer 的可信程式範圍擴大（第 4.8 節） | A1 開工前（A1 也要用） |
| D5 | Signer 介面 | 兩步：challenge 加 verify（A2），付款引用 `approval_id`（P2）；A2 期間 `serve` 的 `/v1/sign` 對 webauthn 仍回 `HUMAN_APPROVAL_UNAVAILABLE`，`serve-approvals` 的付款入口回 `PAYMENT_MODE_OFF` | P1-b 設計第 4.2 節的單步（assertion 直接放進 sign） | 單步的話，A2 就沒辦法在不付款的前提下取得 Signer 驗證的結果 | A2 開工前 |
| D6 | UV、同步 passkey、BE／BS、計數器 | UV 必須；ES256；`attestation=none`；BE 以登記時的值為準、改變就拒絕並標 `suspect`；BS 可變、寫回；計數器倒退就拒絕並標 `suspect`（都是 0 時接受）——這是**本產品的政策** | 只接受裝置綁定的 credential（BE=0）；要求 attestation；計數器倒退只拒絕、不標 `suspect` | 同步 passkey 的私鑰可能存在雲端帳號。**並行影響**：同一把 credential 同時核准兩筆（兩個待核准的 Ask），後簽的先送到時，先簽的那份會被當成倒退而拒絕，並把 credential 標 `suspect`，要管理者處理；同一個會話不受影響（同時最多一筆 attempt） | A2 開工前 |
| D7 | 核准路由的逐路由權限與本人拒絕的主體 | 看資料、發起 challenge、轉送 assertion 都要該 consumer 的 **viewer 憑證**（既有、以 consumer 為範圍）；核准或本人拒絕的結果只取決於 Signer 驗過的 assertion（拒絕用 `decision=deny` 的 challenge）；主持人只能以 operator 憑證「取消」（第 6.3 節） | 拒絕只要 viewer 憑證、不要 WebAuthn（等於讓 viewer 多一個寫入動作，擴大既有角色）；或拒絕交給主持人並照實記成取消 | viewer 憑證被盜時，可以看該 consumer 的摘要、發起 challenge（只能造成打擾），但不能核准或拒絕；Host／Origin 檢查不是身分驗證 | A2 開工前 |
| D8 | 核准資料庫與付款資料庫（P2） | A2 用獨立的 `approvals.db`，`serve-approvals` 不開 `signer.db`；P2 時再決定 `serve` 同時開兩個檔案，或把核准表搬進 `signer.db` | A2 就把核准表放進 `signer.db`（`serve-approvals` 也要開它，會碰到付款恢復的路徑） | 合併方式影響 P2 消耗核准的交易邊界 | P2 開工前 |

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
- 初版時 §7.2 第 7 步之後與 §13.4.3 的原文沒有讀到（擷取工具截斷）；之後由 Codex 在 review 中補讀核對（第 1、13 節），不是 Claude Code 當輪讀完。

## 13 R1～R3 與規格補正（Codex 對 `6c49619` 的 review）

**Codex 的結論**（原文）：

> 方向可沿用，但設計暫不通過，先補三項接線缺口。這些是文件與實作規劃的缺口，不是已重現的產品漏洞；不需要重做 P1-b 或重跑既有驗收。

**證據歸屬**：Codex 做了 Git、文件、相關原始碼與官方規格（含 W3C §6.1.3、§7.2、§13.4.3）的唯讀核對；沒有執行產品、測試、Signer、註冊或交易。這不是全面安全稽核，也沒有新的產品 PASS；前輪核對結果在對話工具紀錄中，沒有 repo 驗證 log。Claude Code 本輪只讀程式（`cmd/demo/api.go` 的 `authorize`、`record` 與 viewer 的 consumer 範圍，`cmd/demo/scenarios.go` 的 `apa.pay.v1` 只限 fake，`cmd/apa-signer` 的 `assemble`、`signer.LoadKeys`、`signer.NewCore` 缺 key 就拒絕）並改文件。

| 項目 | 原規則（`6c49619`） | 新規則 | 理由 | 位置 | 未來驗收案例 |
|---|---|---|---|---|---|
| R1 逐路由權限 | 核准相關路由「不需要也不接受任何角色 token」，只靠 Host／Origin 與 Signer 的驗證 | 逐路由表：看資料、發起 challenge、轉送 assertion 都要該 consumer 的 viewer 憑證；核准與本人拒絕的結果只取決於 Signer 驗過的 assertion；拒絕用 `decision=deny` 的 challenge；主持人只能取消，照實記成取消；Host／Origin、JSON、限速保留但不當成身分 | 本機程式能自行填 Host 與 Origin；摘要、狀態與 credential 選項仍要有 consumer 範圍；P1-a 的 `simResolve` 也先經 `simGuard` | 第 0、6.3、10 節 D7 | 第 9 節 R1 四列與兩項反向 |
| R2 不持付款金鑰的組裝 | 「Signer 只多設定與 DB 表」；「fake 模式只能用 virtual authenticator」 | `apa-signer serve-approvals`：同一份設定只取 wallet 的公開資訊，不讀 `keys/`、不問密語、不開 `signer.db`，用獨立的 `approvals.db`，付款路由回 `PAYMENT_MODE_OFF`，health 寫 `approvals-only`；`serve` 不變。A2 的實機入口是「fake Agent＋合成請購＋真實 authenticator＋付款停用」，三個來源各自標示 | 現行 `assemble` 會讀每個錢包的私鑰，`NewCore` 缺 key 就拒絕；`apa.pay.v1` 情境只限 fake，原規則會讓 A2 沒有實機入口 | 第 2、6.4、7、8 節，D8 | 第 9 節 R2 五列與一項反向 |
| R3 當機與回覆遺失 | 持久化順序只寫「以同一個 ID 查詢」；查詢路由只有依 `challenge_id` 與 `approval_id` | 新增依 `request_id` 查 challenge、依 `verify_request_id` 查結果、關閉 challenge 的路由；`verify_requests` 記錄成功與拒絕；兩組冪等鍵與指紋；10 列恢復表（含斷點 1、2）；查詢失敗不等於查無；`lost` 終態與使用者明確的「重新核准」；「確認中」最晚到後端期限結束；合法重送不重新比計數器；不保存可重放的 assertion | 回覆遺失時 Host 只有 `request_id`；`verifying` 後當機時 Host 沒有 assertion、Signer 也沒有結果，原規則無法結束 | 第 6.1、6.2、7、8 節 | 第 9 節 R3 六列與一項反向 |
| 規格：BE／BS | 「BE／BS 寫回 credential 紀錄」 | BE 以登記時的值為準，改變就拒絕並標 `suspect`；BS 可以合法改變，寫回；BE=0 時 BS 必須是 0 | W3C §6.1.3：BE 是固定的性質，BS 可以改變 | 第 4.4 節、D6 | 第 9 節 BE、BS 兩列 |
| 規格：計數器 | 倒退就拒絕並標 `suspect`，敘述像是規格要求 | 改寫為本產品的政策；倒退不一定是金鑰被複製；D6 列出並行核准的影響；合法重送不經過計數器 | W3C §7.2 把處理方式交給 RP | 第 4.4 節、D6 | 第 9 節計數器倒退、R3 合法重送 |
| 規格：`topOrigin` | 第 4.1 節允許「不存在或相等」，第 4.4 節要求不存在 | 統一：本片非跨來源，`crossOrigin` 不存在或為 `false`，`topOrigin` 必須不存在 | 兩節矛盾 | 第 4.1、4.4 節 | 第 9 節 `topOrigin` 一列 |
| 規格：challenge | 「§13.4.3 的隨機與長度要求，待核對原文」 | challenge 由可信的 Signer 產生、保存、精確比對；保留 32 bytes 隨機數（設計選擇）；原文核對歸 Codex | 原文核對與設計推導分開 | 第 1、4.5 節 | 既有「錯 challenge」「重放」 |
| 登記 E1 的措辭 | 「兩邊的指紋比對可以降低風險」「登記必須在乾淨啟動的 Host 上進行」 | 兩邊相同的指紋不是獨立可信通道；「乾淨啟動」不能證明 Host 未被控制；E1 明寫為「登記期間必須信任 Host 與其頁面」的待接受風險；E2 列最小差異與仍依賴的條件；D2 交 Aaron 決定 | Host 可以替換登記內容並控制瀏覽器的顯示 | 第 3.2 節、D2 | — |

**仍待決策**：D1～D8（第 10 節），其中 D2（E1 或 E2）與 D7（逐路由權限與拒絕的主體）依本輪修正後的內容交 Aaron 決定；D8 到 P2 才決定。

## 14 close／verify 競爭與 decision 綁定（Codex 對 `91a7204／031c4c7` 的限定複核）

**Codex 的限定結論**（原文）：

> R1 原本的存取權限缺口、R2 無付款金鑰的組裝缺口可以關閉；整份設計仍有兩處需要補齊。都是局部狀態規則，不需要重做架構或執行測試。

- **維持關閉**：R1 原本的存取權限（consumer 範圍的資料權限、核准結果只來自 Signer 驗過的 assertion）、R2（`serve-approvals` 不讀付款 key、不開付款 DB；fake Agent＋合成請購＋實機 authenticator＋付款停用的入口）。BE／BS、計數器政策、`topOrigin` 與 E1 的信任限制不重開。**這不表示整份設計已通過。**
- **證據歸屬**：Codex 依 security-audit 的限定檢視方式，唯讀核對 `91a7204／031c4c7` 的 Git、文件差異與相關既有程式；沒有執行測試、Signer、驗證器或交易，也沒有修改 repo 文件；這次沒有重新查閱官方網站（規格原文核對歸前輪）。兩項問題是依文字規則推演出的設計缺口，不是執行重現的產品漏洞。Claude Code 本輪只改文件，分支推演是紙上核對，不是實測。

| 項目 | 原規則（`91a7204`） | 新規則 | 理由 | 位置 | 未來驗收案例 |
|---|---|---|---|---|---|
| R3 剩餘：查無與關閉的競爭 | 恢復表第 4 列「查無＝確定未收到」，查無就送 close 並把 attempt 標 `lost`；close 只寫「只在 `issued` 時改成 `closed`」 | 查無不等於確定未收到；verify 的最後提交以 `state='issued'` 條件式寫入，與 close 競爭同一列，同一交易寫核准與 `verify_requests`；close 冪等、重讀並回權威狀態（`closed`／`used`＋`approval_id`／`expired`／不存在），不覆寫 `used`；Host 只在 close 回 `closed` 後標 `lost`；`used` 時照既有結果落地（會話已關閉就 `abandon`）；結果未知時不開新 attempt，到期結束等待但不寫成「未核准」；新增依 `challenge_id` 查詢 | 合法時序：查無 → verify 提交 → close 才送到 | 第 6.1、7、8 節 | 第 9 節「查無後 close 先成功」「查無後 verify 先成功」「close 回覆遺失」「close 或查詢失敗、期限到達」與一項反向 |
| 新增 decision 後的雙分頁綁定 | 第二個分頁一律拿回目前 attempt 的 challenge | 只有同一 consumer、會話、`intent_hash`、`decision` 才共用 attempt，回權威的 attempt ID（與同鍵冪等分開）；相反的 `decision` 回 409 `DECISION_CONFLICT`；改變決定要先「取消這次驗證」並照 close 規則結束舊 challenge，未確認前不得另開；WebAuthn 啟動前頁面核對 consumer、會話、intent、`decision`，不一致就不彈出；Host 只依 Signer 回傳且與 attempt 一致的 `decision` 落地，deny 不發 grant | 加入 `decision=deny` 後，原規則會讓按拒絕的頁面拿到核准的 challenge，或反過來 | 第 6.3、7、8 節 | 第 9 節「核准進行中按拒絕」「拒絕進行中按核准」「兩個分頁，同一個決定」「回覆的決定與按鈕不符」「改變決定時舊 verify 晚到」「deny 不發付款 grant」與一項反向 |
| 殘留文字：DB 位置 | 第 6.1 節寫「`signer.db` 新表」 | A2 的核准表全部在 `approvals.db`；付款模式的 `signer.db` 不變 | 對齊第 6.4 節 | 第 6.1 節 | — |
| 殘留文字：規格核對歸屬 | 第 4.4 節寫「待 Codex 對照原文核對」 | 第 7 步之後與 §13.4.3、§6.1.3 的原文由 Codex 在 `6c49619` 的 review 補讀核對；不是 Claude Code 初版讀完 | 對齊前輪歸屬 | 第 4.4 節 | — |
| 殘留文字：付款拒絕碼依模式 | 第 6.1 節、D5 與驗收表只寫 `HUMAN_APPROVAL_UNAVAILABLE` | `serve`：webauthn 仍 `HUMAN_APPROVAL_UNAVAILABLE`、其餘照 P1-b；`serve-approvals`：所有付款入口 `PAYMENT_MODE_OFF`；驗收分成兩列 | 兩個入口的預期不同 | 第 6.1、9、10 節 | 第 9 節「`serve` 的付款規則不變」「`serve-approvals` 沒有付款入口」 |

**仍待決策**：D1～D8 不變（第 10 節）；技術 review 不代替 Aaron 的決策或執行核准。

## 15 另開條件與 close 的到期判斷（Codex 對 `287f340` 的核對）

**Codex 的結論**：主要修正方向成立（verify／close 競爭同一列、核准與結果原子提交；相反 decision 不共用 challenge、頁面啟動前核對、Host 落地核對與 deny 不發 grant），但還有兩處狀態規則要對齊，**整份設計暫不標通過**；都是局部文件補正，不需要重做架構或跑測試。原 R1 存取權限、R2 核准專用模式維持關閉。

**證據歸屬**：Codex 依 security-audit 的限定文件檢視核對 Git 與文字，沒有執行重現或產生測試 PASS。Claude Code 本輪只改文件（Aaron 指示「請照做」），沒有執行任何東西。

| 項目 | 原規則（`287f340`） | 新規則 | 理由 | 位置 | 未來驗收案例 |
|---|---|---|---|---|---|
| 另開新 attempt 的條件 | 「close 回 `closed`（或舊 attempt 已是終態）之後才能開新的 attempt」 | 刪除「或舊 attempt 已是終態」；改依 Signer 對舊 challenge 的權威結果：`closed`／`expired` 且會話仍有效才能另開，`used` 照既有結果，未知不能另開；恢復表第 7 列註明 `refused` 不表示 challenge 已失效 | 第 7 列允許 attempt 已是 `refused` 而 challenge 仍可重試，所以 Host 的終態不保證舊驗證已失效 | 第 7 節 | 第 9 節「已 refused、challenge 仍 issued 時改變決定」 |
| close 的到期判斷 | close 只以 `state='issued'` 更新成 `closed`，會把「時間已到、還沒改 `expired`」的紀錄寫成 `closed` | 同一交易內取時間，對仍是 `issued` 的紀錄以互斥條件分成未到期（`closed`）與已到期（`expired`，等於期限也算）；`used` 不因時間經過被覆寫；依 `challenge_id` 的查詢遇到過期的 `issued` 也以條件式短寫入記成 `expired` | 結果表要求回 `expired`，原 SQL 與它不一致 | 第 6.1 節 | 第 9 節「close 在期限前一刻」「close 剛好在期限」「`used` 之後才到期」 |

**不變**：D1～D8 不代選；A1／A2 不開工；其餘章節不重開。

## 16 限定技術複核通過（10/13）

Aaron 轉達的結論（原文）：

> 本人核准最小流程設計通過限定技術複核，已提出的 R1～R3、decision 綁定與到期規則缺口關閉。這是設計一致性核對，不是實作、安全全面稽核或實機驗收；D1～D8 的決策與執行核准仍分開處理。

- **基準**：設計文件到 `c798bad` 為止的內容（第 0～15 節）。
- **關閉**：R1～R3（第 13 節）、decision 綁定（第 14 節）、另開條件與 close 到期規則（第 15 節）。
- **不代表**：
  - 沒有實作或實機驗收；
  - 不是全面安全稽核；
  - D1～D8 都還沒有決定（第 10 節）。A1 spike 與 A2 接線要等這些決策與另行派工的執行核准。
- **付款仍未啟用。**

## 17 Aaron 的決策（10/13）

**原話**：

> 我接受上述 D1～D7 的推薦方案及明列限制。D8 的 P2 整合方式延後決定，A2 使用獨立 approvals.db。
> 核准另行派工 A1 最小 spike 的實作、定向驗證與由我操作的實機登記／驗證；只用測試用途，不接付款，不使用真實 Agent。
> 裝置不支援時保存結果並停止，不自動採購或擴大方案。A2 實作、付款金鑰、簽署與交易仍需另行核准。

「上述」指 Codex 的建議表。逐項記錄如下，限制與該表相同：

| # | 決定 | 接受的限制 |
|---|---|---|
| D1 實機驗證器 | 先試這台 Mac 的 Touch ID（若有），再試手機；都不支援才決定是否買 FIDO2 key | 不保證 macOS 12 能完成；virtual authenticator 不算實機通過，也不自動核准採購 |
| D2 首次登記 | 這版 demo 採 **E1**：經 Host 轉送，在 Signer 終端機確認 | 明確接受「登記期間信任 Host 與其頁面」；指紋相同不能證明 Host 安全；E2 留作後續強化 |
| D3 網址與入口 | A1 用 `localhost:8123`，A2 用 `localhost:8402`，RP ID 都是 `localhost` | A2 重新登記，不沿用 spike 的 credential |
| D4 新依賴 | 接受釘選的 `go-webauthn/webauthn` v0.18.2 與必要依賴 | 只用於核准驗證；實作若需要額外升級既有依賴，先列差異，不自行擴大 |
| D5 Signer 介面 | challenge → verify 兩步 | A2 只證明本人核准已驗證；付款引用留到 P2 |
| D6 驗證政策 | 必須 UV、ES256、`attestation=none`；接受同步 passkey；BE 固定、BS 可變；計數器倒退就拒絕並標 `suspect`，兩者皆 0 可接受 | 同步 passkey 涉及雲端帳號的信任；同一 credential 並行核准可能觸發計數器政策，這版人工核准先逐筆操作 |
| D7 路由權限 | consumer 範圍的 viewer 憑證；核准與本人拒絕都要裝置驗證；主持人只能取消 task | viewer 憑證不是本人核准證明；主持人取消不能寫成本人拒絕 |
| D8 資料庫整合 | **延後到 P2**；A2 沿用獨立的 `approvals.db` | 不讓未來的付款整合阻擋目前的核准流程 |

**核准範圍**：
- **另行派工的 A1 最小 spike**：
  - 實作與定向驗證；
  - 由 Aaron 操作的實機登記與驗證；
  - 只用於測試，不接付款，不使用真實 Agent。
- **裝置不支援時**：保存結果並停止，不自動採購或擴大方案。

**仍需另行核准**：A2 實作、付款金鑰、簽署與交易。

**現況**（`35cbf3f` 時）：A1 尚未開工，等派工筆記。之後 Aaron 以「核准 A1 派工」指示直接開工，見第 18 節。

## 18 A1 spike 實作與定向驗證（10/13）

**依據**：Aaron 10/13「核准 A1 派工」。vault 沒有另寫 A1 派工筆記，範圍取第 2 節 A1 列、roadmap A1 與第 17 節的條件。

**交付**：`spikes/webauthn/`，操作與規則見該目錄的 `README.md`。
- 一個獨立的 Go module（go-webauthn v0.18.2，D4），不進主 `go.mod`，也不 import 產品程式。新增的間接依賴與第 4.8 節列的相同；`x/crypto v0.57.0`、`x/sys v0.48.0` 與主 module 一致，沒有升級既有依賴。
- 本機小伺服器：只綁 loopback，入口是 `http://localhost:8123`（D3），RP ID 是 `localhost`。
- 請購是固定的一筆合成資料，challenge 依第 4.5 節的形式由 record 雜湊產生（簡化：兩個合成會話、jti 由會話推出）。狀態只在記憶體。
- 驗證政策照 D6：
  - UV 必須；
  - 只接受 ES256 與 `attestation=none`；
  - BE 固定、BS 可變；
  - 計數器倒退就拒絕並標 `suspect`，兩邊都是 0 時接受；
  - `topOrigin` 與 `crossOrigin` 都拒絕；
  - challenge 只能用一次、120 秒到期。
- 結果寫成 JSONL：每行都標 `real-device` 或 `virtual`；credential ID 只留雜湊；瀏覽器端的拒絕另記為 `browser:…`。

**定向驗證（Claude Code 執行；都不是實機結果）**：

| 項目 | 方式 | 結果 |
|---|---|---|
| 伺服器規則 | 軟體 authenticator（P-256，`none`／packed）直接呼叫 handler，4 個測試 | PASS |
| 反向 | `reverse.py`：在副本裡逐條拿掉 13 條規則（已用、到期、計數器、BE、強制 UV、suspect 兩處、Host、Origin、一次登記、challenge 來自 record、credential ID 雜湊、`fmt=none`） | 13／13 被抓到 |
| 整頁 | `virtual.mjs`：headless Chrome＋CDP virtual authenticator（ctap2、internal、UV） | 登記、核准、重送 `CHALLENGE_USED`、錯 challenge `ASSERTION_INVALID`、`127.0.0.1` 頁面瀏覽器回 `SecurityError`；這個 virtual authenticator 在不要求 UV 時回 `UV=0`，伺服器拒絕 |
| 啟動 | 綁 `127.0.0.1:8123` | 頁面 200、錯 Host 421、沒有 Origin 的 API 404；`--authenticator` 不對或 `--addr` 不是 loopback 時拒絕啟動 |

**這台機器**：MacBookPro13,3（2016 年 15 吋、有 Touch Bar，應有 Touch ID），macOS 12.7.6，有 Chrome 與 Safari。Touch ID 能不能用於 WebAuthn，以實測為準。

**下一步**：
- Aaron 依 `spikes/webauthn/README.md` 操作實機：Chrome＋Touch ID → Safari＋Touch ID → Chrome＋手機。
- 結果檔放 `var/a1/`（不提交），交 review 判讀。
- 裝置不支援時保存結果並停止，不自動採購或擴大方案。

**不做**：A2、Signer、Store、Host 接線、付款金鑰、簽署、交易、真實 Agent。

## 19 A1 spike R1／R2 限定補正（10/13）

**依據**：vault〈Claude-Code-A1-spike反向驗證與結果保存限定補正派工-20677b0-20261013〉。
- Codex 對 `20677b0` 只做 security-audit 限定唯讀核對：讀了 Git、原始碼、操作文件與本機 go-webauthn 原始碼。
- Codex 沒有重跑測試、沒有啟動伺服器，也沒有登記驗證器。

**狀態**：A1 spike R1／R2 補正完成，待 Codex 限定複核；實機未驗收，A2 未開工，付款未啟用。A1 不標 Done。

### 19.1 原因與修正

| # | 原問題 | 修正（`7bb874f`） |
|---|---|---|
| R1 | `reverse.py` 只排除兩種建置錯誤字樣，其他非零退出（panic、逾時等）都算抓到；不足 13 項也可能 exit 0 | 每項明列目標測試與預期斷言。先確認未突變的基準通過且確實跑到目標測試；只有「指定測試以預期斷言失敗」才算抓到。建置或環境錯誤、panic、逾時、訊號終止、沒跑到測試、其他斷言、存活都不算。每項 120 秒、整體 900 秒上限，逐項印出進度、耗時與分類理由。每項的基準與突變輸出和 `summary.json` 都保存。腳本自己建立一次性副本，中斷也不會寫到工作區。全數抓到才 exit 0。另附 `--self-test`，用合成輸出檢查分類器 |
| R2 結果檔 | `OpenFile(..., 0600)` 只替新檔設權限；既有較寬權限的檔案或 symlink 仍會被追加，與 README 的 0600 不一致 | `--out` 改成 `--out-dir`：父目錄必須已經存在、屬於自己、不是 symlink、權限 0700。每次啟動以 `mkdir` 建新的 `run-<UTC>-<隨機>/`（0700），再以 `O_EXCL\|O_NOFOLLOW` 建 `results.jsonl`（0600）。既有檔案、目錄或 symlink 一律拒絕，不改旁觀檔案。啟動時印出結果檔路徑 |
| R2 錯誤輸出 | `libDetail` 保存函式庫的 `Details`／`DevInfo`；challenge 不符時會帶出預期與收到的 challenge（Codex 依依賴原始碼確認；屬於輸出超出最小紀錄範圍，不是已證明的外洩） | 函式庫錯誤只轉成固定的白名單原因（`challenge_mismatch`、`origin_mismatch`、`top_origin`、`cross_origin`、`uv_missing`、`up_missing`、`rp_id_mismatch`、`backup_eligible_changed`；其餘記 `other:<類型>`／`other`），結果檔與 HTTP 回覆都一樣。我們自己的拒絕也改成固定字詞。`fmt`、attachment、transports 只留已知值；步驟名稱只接受頁面的五個步驟。瀏覽器端只保存錯誤名稱（固定清單），不保存訊息 |

**操作文件**（`spikes/webauthn/README.md`）：
- 「沒有金鑰」改成「不建立或使用付款金鑰」。
- 寫明實機登記會在驗證器裡建立 credential，重啟 spike 只清伺服器記憶體。
- A2 重新登記；裝置端 credential 是否清理，由 Aaron 辨認後自己決定與操作，不自動刪除。
- 補上關卡 A／B／C 與停止條件：「不支援」不算通過；全部不支援就保存並停止，不採購也不擴大方案。
- 補上授權來源表：
  - 決策接受：Aaron 10/13 原話，第 17 節逐字保存；
  - 實作派工：同一段原話，加上對話中的「核准 A1 派工」，再加本補正派工；
  - 實機執行：原話寫明由 Aaron 操作，依本補正派工，要等核對完成、Aaron 確認後才進行。
- 兩句原話都可在 Claude Code 對話中核對；Codex 的建議表不是核准。

### 19.2 本輪驗證（Claude Code 執行，待 Codex 核對；都不是實機結果）

基準 `7bb874f`。證據在 `artifacts/a1/r1r2-20261013T062322Z/`（git 忽略；原始執行目錄在 Claude Code 的暫存區，已複製過來，不含 binary 與臨時 Chrome profile）。

| 步驟 | 命令 | exit | 耗時 | 結果 |
|---|---|---|---|---|
| 1 | `go test -count=1 -v ./...`（`GOPROXY=off`） | 0 | 2 s | 6／6 PASS，含新增的 `TestResultsDir`、`TestEvidenceKeepsFixedWords`；拒絕案例另外核對結果檔記到的原因（錯 origin、topOrigin、crossOrigin、缺 UV） |
| 2 | `python3 reverse.py --self-test` | 0 | 1 s | 11／11：正例，以及建置錯誤、panic、逾時（兩種）、訊號、未執行（兩種）、其他斷言（兩種）、存活 |
| 3 | `python3 reverse.py <證據>/reverse` | 0 | 23 s | 13／13 都以預期斷言命中；4 個目標測試的基準都通過；工作區事後無變更 |
| 4 | `node spikes/webauthn/virtual.mjs <binary> <證據>/virtual`（工具沙箱外，綁 loopback） | 0 | 4 s | 整頁 PASS：錯 challenge 記 `challenge_mismatch`、不要求 UV 記 `uv_missing`（virtual 回 `UV=0`）、錯 origin 記 `SecurityError`；每行都是 `virtual`；`detail` 只有固定字詞；`results.jsonl` 0600，在新的 0700 run 目錄裡 |

**執行範圍**：
- 四步都在 `7bb874f` 上各跑一次，沒有重跑。
- 開發期間（提交前）有一次失敗：缺 UV 的原因被記成 `other:verification_error`，因為那個錯誤的 `Details` 不是空的。已改成依 `DevInfo` 的固定字串判斷，再提交與正式執行。
- 環境：
  - 測試只用合成資料、軟體與 virtual authenticator，加上臨時的 Chrome profile；
  - 沒有用個人瀏覽器 profile 或真實 credential；
  - 沒有下載或升級依賴。

**沿用與未跑**：
- 第 18 節的舊紀錄（`20677b0` 的軟體測試、virtual 流程、13／13）保留，歸 Claude Code。
- 舊的 13／13 是用舊分類器算的，不足以證明每項都命中預期斷言；本輪以新分類器重跑，不把兩輪合寫。
- 沒有跑主專案全套、完整容器、全部突變、M1 產包、Unity 或 P1-b 隔離流程。
- 沒有實機登記、沒有讀 keychain、沒有啟動 Signer、沒有碰 `/opt/apa-signer`。
- 沒有 A2、真實 Agent、付款金鑰、簽署或連鏈。

**下一步**：交 Codex 限定複核；實機操作要等核對完成、Aaron 確認後，才依 `spikes/webauthn/README.md` 進行。

## 20 A1 R1／R2 限定複核結論與 Chrome＋Touch ID 實機核准（10/13）

**依據**：vault〈Claude-Code-A1限定複核同步與Chrome-Touch-ID實機陪同派工-f8b59b9-20261013〉。開工基準 HEAD `f8b59b9`（程式與測試基準 `7bb874f`，之後只有文件）。

### 20.1 限定複核結論（原文）

> 7bb874f 的 A1 spike R1／R2 通過限定技術複核，可以關閉。A1 可以進入由 Aaron 操作的實機驗證準備，不需要再重跑這輪自動化。這不代表 A1 已完成，也不是全面安全稽核。

**關閉理由**（依派工筆記轉記）：

| 項目 | 理由 |
|---|---|
| R1 | 分類器綁定目標測試與預期斷言；Codex 逐項核對保存的基準與突變輸出，13 項都有對應斷言，不只看 13／13 摘要 |
| R2 結果檔 | 新建私人 run 目錄、排他建立結果檔；拒絕既有路徑與 symlink 的測試，涵蓋旁觀檔案不變 |
| R2 錯誤輸出 | 函式庫與瀏覽器錯誤已轉成固定分類；測試涵蓋原始 challenge、credential ID、assertion 片段與合成標記不進入結果 |

**證據歸屬**：
- `artifacts/a1/r1r2-20261013T062322Z/` 的 Go 6 項、分類器 11 項、反向 13 項與 virtual 流程，都由 Claude Code 執行（第 19.2 節）。
- Codex 唯讀核對原始碼、測試與保存的輸出，沒有重跑、沒有啟動 spike，也沒有操作實機。
- 自動化結果不是實機 PASS。這次同步沒有重跑任何測試。

### 20.2 Chrome＋Touch ID 實機操作的核准與範圍

Aaron 原話：

> 我拍板先做 Chrome＋Touch ID 這部分

**範圍**：
- 只做 Chrome 加這台 Mac 的 Touch ID。
- 先完成啟動與「1 登記」，回報後等判讀；按鈕 2～5、錯 origin 探測之後再逐步指引。

**不包含**：Safari、手機、外接安全金鑰或其採購、A2、付款。這次核准不追認、也不改寫先前的授權紀錄。

**分工**：
- Aaron 自己在終端機啟動、自己選裝置與驗證；
- Claude Code 負責指引與整理回報；
- Codex 負責限定判讀。
- 三者分開記錄；Aaron 的目視回報不寫成助手親眼所見。

**停止條件**：
- 沒有本機 Touch ID 選項：記下實際提示並停止。
- 改要求系統密碼：先回報提示文字，等 Aaron 決定；不索取密碼。
- 取消、建置或保存失敗、權限錯誤、port 被占用或其他非預期結果：先保存並停止。不自動重試，不換下一關，也不終止其他程序。
- 只有明確的「未提供該裝置」或不支援訊息，才分類為不支援；登記或核准失敗不能一律寫成裝置不支援。
- `spikes/webauthn/README.md` 原本的 A／B／C 全流程與「誤按取消可再試」，不適用於這次較窄的操作。

**資料**：
- 這次登記會建立測試用的 WebAuthn credential，不是付款金鑰。
- spike 重啟只清伺服器記憶體，不刪驗證器或同步帳號裡的 credential；A2 不沿用它。
- 結果檔留在程式印出的新 run 目錄（`/tmp` 底下，只是暫存）。要複製到 `artifacts/a1/` 時，先確認來源與不覆寫的新目的地，只複製測試結果，並核對前後雜湊；複製完成前不清除暫存。

### 20.3 現況

- 已核准 Chrome＋Touch ID，尚待 Aaron 的操作結果。
- 實機未驗收，A1 未標 Done；A2 未開工；付款未啟用。
- 這次只同步文件，沒有改程式、沒有重跑自動化。

### 20.4 第一關結果：Chrome 登記（部分完成）

**來源與分工**：
- 操作與目視由 Aaron 執行，下面「Aaron 回報」一欄是他在對話中的口述，不是助手親眼所見。
- 伺服器紀錄取自 spike 寫出的結果檔。
- Claude Code 只整理紀錄、複製證據；尚未經 Codex 判讀。

| 項目 | 內容 |
|---|---|
| Aaron 回報：Chrome 提供的選項 | 有「這台 Mac」與「Google 密碼管理工具」（其他選項未回報） |
| Aaron 回報：所選 | Google 密碼管理工具 |
| Aaron 回報：驗證本人 | 輸入 Google 密碼管理工具的 PIN，**不是 Touch ID** |
| 伺服器紀錄 | `step=register`、`ok=true`、`authenticator=real-device`、`flags=UP=1 UV=1 BE=1 BS=1`、`sign_count=0`、`fmt=none`、`attachment=platform`、`transports=[hybrid, internal]`、AAGUID `ea9b8d664d011d213ce4b6b48cb575d4`、credential 雜湊 `867b5d6c`；Chrome 150（user agent） |
| 證據 | 來源 `/tmp/apa-a1.rUI7UP/results/run-20261013T064844Z-c200573f/results.jsonl`（Aaron 確認是他終端機印出的路徑；1 行、448 bytes、0600，run 目錄 0700），複製到 `artifacts/a1/real-device-chrome-20261013T064844Z/results.jsonl`（新目錄、不覆寫、git 忽略）；兩邊 SHA-256 都是 `f1990ebb93b27b1a5170fc3f6adcec11135c471ebc16f3699d872c078865dbb6`。暫存目錄保留，未清除 |

**整理**（不是判讀結論）：
- 這次登記的是「Chrome＋Google 密碼管理工具」的同步 passkey：Aaron 的選項、`BE=1 BS=1` 都指向這一點。
  - Claude Code 記得這個 AAGUID 在公開的 passkey AAGUID 對照表裡對應 Google Password Manager，但沒有在本輪查表核實，由 Codex 判讀時確認。
  - D6 接受同步 passkey；這把 credential 可能已經同步到 Aaron 的 Google 帳號。
- **「Chrome＋本機 Touch ID」尚未試**：「這台 Mac」的選項有出現但沒有選，所以不算通過，也不算不支援。
- `UV=1` 不推定為 Touch ID；這次實際用的是 PIN。
- 只完成了按鈕 1；按鈕 2～5 與錯 origin 都沒有做。spike 仍在 Aaron 的終端機執行中。

**待判讀**：
1. Google 密碼管理工具這條路徑，要不要繼續做按鈕 2～5？
2. 要不要另開新的 run，選「這台 Mac」用本機 Touch ID 重新登記？這仍在 Aaron 核准的範圍內，但屬於重新操作，要等判讀與 Aaron 確認。

**裝置端 credential**：Google 密碼管理工具裡已經有一把 `localhost`／`a1-spike-tester` 的測試 passkey。要不要清理，由 Aaron 辨認後自己決定與操作，不自動刪除。

**現況**：實機未驗收，A1 未標 Done；A2 未開工；付款未啟用。

## 21 A1 Chrome＋Google 密碼管理工具／PIN 實機證據收尾（10/13）

**依據**：vault〈Claude-Code-A1Chrome-Google密碼管理工具實機證據收尾派工-33a5bf4-20261013〉。開工基準 HEAD `33a5bf4`；`7bb874f` 之後程式沒有變動。第 20.4 節第一關的原始紀錄與證據保持不變。

**範圍調整**：Aaron 原話：

> 同意先完成 Chrome＋Google 密碼管理工具／PIN 的 A1 測試，Touch ID 另行驗證。

所以這次記為「Chrome＋Google 密碼管理工具／PIN」，不記為 Chrome＋Touch ID。本機 Touch ID 未驗證，不寫成通過或不支援。

**證據歸屬**：
- Aaron：操作驗證器與瀏覽器，目視並回報。
- Claude Code：確認停止、複製、唯讀整理與文件同步。沒有建置、啟動服務或操作驗證器。
- Codex：依 Aaron 貼回的內容逐步判讀；核對第一關副本（重算單行雜湊）；查閱 [Google 官方說明](https://web.dev/articles/webauthn-aaguid)，核實 AAGUID `ea9b8d66-4d01-1d21-3ce4-b6b48cb575d4` 對應 Google Password Manager。
  - Codex 尚未核對下面的完整 JSONL，也沒有親自操作驗證器。
  - AAGUID 不是獨立的安全認證，也不能單憑它或 UV 旗標推定具體的本人驗證手段。

### 21.1 停止確認與保存

**停止確認**：
- Aaron 在對話中確認已在原 spike 終端機按下 Ctrl-C，這是他的回報。
- Claude Code 另外以 `lsof -nP -iTCP:8123 -sTCP:LISTEN` 唯讀檢查，8123 沒有監聽；沒有終止或碰任何程序。

**來源**：`/tmp/apa-a1.rUI7UP/results/run-20261013T064844Z-c200573f/results.jsonl`。
- 是 Aaron 先前確認的同一路徑，沒有用搜尋找到的其他 run 代替。
- 一般檔案、不是 symlink；6577 bytes；`-rw-------`，uid 501；14 行；最後修改 15:36:29（+08:00）。
- run 目錄 `drwx------`。

**保存**：
- 目的地：`artifacts/a1/real-device-chrome-gpm-complete-20261013T074258Z/results.jsonl`。新目錄 0700、副本 0600，git 忽略；目的地事先不存在，沒有覆寫第一關證據或任何舊紀錄。
- 命令：`test -f`／`! -L`、`stat`、`wc -l`、`shasum -a 256`（複製前）、`mkdir -m 700`、`cp`、`chmod 600`（只改副本）、`shasum -a 256`（複製後的來源與副本）。

| 對象 | SHA-256 |
|---|---|
| 來源，複製前 | `908cbbe2bdd717895c0390ed6ffb834074167207c718384457cba6890110a5cc` |
| 來源，複製後 | `908cbbe2bdd717895c0390ed6ffb834074167207c718384457cba6890110a5cc` |
| 副本 | `908cbbe2bdd717895c0390ed6ffb834074167207c718384457cba6890110a5cc` |
| 第一關副本（未動） | `f1990ebb93b27b1a5170fc3f6adcec11135c471ebc16f3699d872c078865dbb6`（仍是第 20.4 節的單行；來源後來繼續追加，不要求符合這個雜湊） |

`/tmp/apa-a1.rUI7UP` 保留，沒有清理；裝置端 credential 也沒有動。

### 21.2 完整 JSONL 逐行（14 行，全部保留，順序照檔案）

共通欄位：
- 每行都是 `authenticator=real-device`，user agent 都是 Chrome 150（只有一種）。
- 除了第 14 行，origin 都是 `http://localhost:8123`。
- 有 credential 的行，都是 `867b5d6c`、AAGUID `ea9b8d66…`、`attachment=platform`。

時間是 UTC。

| 行 | 時間 | step | 結果 | 會話／challenge | UV 要求 | flags | sign_count |
|---|---|---|---|---|---|---|---|
| 1 | 06:50:01 | register | ok；`fmt=none`；transports hybrid、internal | — | — | UP=1 UV=1 BE=1 BS=1 | 0 |
| 2 | 07:29:55 | browser:approve | `BROWSER_REPORTED`、`NotAllowedError` | — | — | — | — |
| 3 | 07:30:10 | approve | ok（verified） | s1／`ch-8944f23601d8cafdf94c2c95` | required | UP=1 UV=1 BE=1 BS=1 | 0 |
| 4 | 07:30:22 | approve | ok（verified） | s1／`ch-0bedd6f09bea6ed6fa2e955f` | required | UP=1 UV=1 BE=1 BS=1 | 0 |
| 5 | 07:30:55 | replay | `CHALLENGE_USED` | s1／`ch-0bedd6f0…`（第 4 行的） | required | UP=1 UV=1 BE=1 BS=1 | 0 |
| 6 | 07:31:21 | wrong-challenge | `ASSERTION_INVALID`、`challenge_mismatch` | s2／`ch-c694774c14ed45ace7f079aa` | required | UP=1 UV=1 BE=1 BS=1 | 0 |
| 7 | 07:31:53 | uv-discouraged | `ASSERTION_INVALID`、`uv_missing` | s1／`ch-915eb092efef44df51c82b2a` | discouraged | UP=1 **UV=0** BE=1 BS=1 | 0 |
| 8 | 07:33:24 | uv-discouraged | `ASSERTION_INVALID`、`uv_missing` | s1／`ch-19b5bed241a5a4db351ac36e` | discouraged | UP=1 **UV=0** BE=1 BS=1 | 0 |
| 9 | 07:34:16 | uv-discouraged | `ASSERTION_INVALID`、`uv_missing` | s1／`ch-9c45635904c31176d616574e` | discouraged | UP=1 **UV=0** BE=1 BS=1 | 0 |
| 10 | 07:34:18 | replay | `CHALLENGE_USED` | s1／`ch-0bedd6f0…` | required | UP=1 UV=1 BE=1 BS=1 | 0 |
| 11 | 07:34:20 | replay | `CHALLENGE_USED` | s1／`ch-0bedd6f0…` | required | UP=1 UV=1 BE=1 BS=1 | 0 |
| 12 | 07:34:23 | replay | `CHALLENGE_USED` | s1／`ch-0bedd6f0…` | required | UP=1 UV=1 BE=1 BS=1 | 0 |
| 13 | 07:34:58 | browser:register | `BROWSER_REPORTED`、`InvalidStateError` | — | — | — | — |
| 14 | 07:36:29 | browser:register（origin `http://127.0.0.1:8123`） | `BROWSER_REPORTED`、`SecurityError` | — | — | — | — |

- 每行都是完整的 JSON，時間遞增，沒有出現非預期的 origin 或錯誤碼。
- 「重送」那幾行裡的 flags 與 sign_count，是重送的那份 assertion 本身的值（伺服器先解析再判定 `CHALLENGE_USED`），不是一次新的驗證。

### 21.3 與 Aaron 回報對照

| 回報項目 | 對應的行 | 對照 |
|---|---|---|
| 1 登記：ok；UP=1 UV=1 BE=1 BS=1；`fmt=none`；`sign_count=0`；platform；hybrid、internal；選 Google 密碼管理工具、用 PIN | 1 | 欄位一致。選項與 PIN 是 Aaron 的回報，結果檔不記驗證手段 |
| 2 核准：verified；s1；`ch-8944f23601d8cafdf94c2c95`；UP=1 UV=1 BE=1 BS=1；`sign_count=0`；要求 PIN | 3 | 欄位一致 |
| 2 核准回報的 `intent_hash` `c9f7485c…4e537ba5` | — | 結果檔依設計不記 `intent_hash`。Claude Code 依 `main.go` 的固定合成請購，以 Python 重算 `json.Marshal` 等價的 SHA-256，得到同一個值（沒有建置或執行 spike） |
| 3 重送：`CHALLENGE_USED`，detail 空字串；沒有裝置提示 | 5（另有 10～12） | 代碼一致；被重送的是第 4 行的 challenge，不是回報裡的 `ch-8944…` |
| 4 錯 challenge：`ASSERTION_INVALID`、`challenge_mismatch` | 6 | 一致；s1 的回覆送到 s2 的 challenge |
| 5 不要求 UV：`ASSERTION_INVALID`、`uv_missing`；只出現 passkey 選擇提示，沒有重新輸入 PIN | 7（另有 8、9） | 代碼一致；**flags 是 `UV=0`**，和「沒有輸入 PIN」相符，三行都一樣 |
| 誤操作的重複登記：localhost 上 `InvalidStateError` | 13 | 一致。這是在 localhost 對已登記的驗證器再登記一次，不是錯 origin 測試；不算該項通過，也不是防護失效 |
| 錯 origin：`http://127.0.0.1:8123/`，`SecurityError`，訊息「This is an invalid domain.」 | 14 | 名稱一致；結果檔依設計只存固定名稱，訊息來自 Aaron 貼回的頁面，沒有補造。這是瀏覽器端的拒絕，不是伺服器 WebAuthn 驗證的拒絕 |

Aaron 先回覆第 3、5 步「有提示」，後來附上畫面並明確說「第 5 步才有」；那個畫面是選擇或確認 passkey，不是 PIN 輸入框。這裡以澄清後的內容為準，並保留澄清的過程。

### 21.4 回報裡沒有的行（照實列出，交 Codex 判讀）

| 行 | 內容 | 說明 |
|---|---|---|
| 2 | `browser:approve`、`NotAllowedError`，在第 3 行之前 15 秒 | 只能確認那次瀏覽器操作沒有完成，不能判定是取消、逾時或其他原因（結果檔不記原因，Aaron 沒有回報）（第 22 節修訂） |
| 4 | 第二次 `approve` 成功（`ch-0bedd6f0…`），第 3 行之後 12 秒 | 紀錄顯示兩次核准請求，兩次都 verified，用的是不同的 challenge；兩次 `sign_count` 都是 0（0/0，依 D6 接受）（第 22 節修訂） |
| 5、10～12 | 共 4 次 `replay`，都是 `CHALLENGE_USED` | 重送的是第 4 行那份；4 次都被拒，沒有任何一次被接受 |
| 7～9 | 共 3 次 `uv-discouraged`，都是 `uv_missing`、`UV=0` | 紀錄顯示三次不要求 UV 的請求，結果都一樣（第 22 節修訂） |

**影響（整理，不是判讀）**：
- 多出來的行，都和回報的類型與結果一致，沒有任何一行出現非預期的接受。
- 和回報的差異在於：
  - 次數：核准成功 2 次、重送 4 次、不要求 UV 3 次，另有 1 次瀏覽器端的 `NotAllowedError`；
  - 被重送的 challenge 是第二次核准的那一個。
- 這些是不是「異常重試」，由 Codex 判讀。Claude Code 沒有重跑或刪行來消除差異。

### 21.5 現況

- Chrome＋Google 密碼管理工具／PIN 的 A1 實機操作已完成回報，完整證據待 Codex 最後核對。
- Touch ID 未驗證，A1 未標 Done；A2 未開工；付款未啟用。
- 這次沒有改程式、沒有建置、沒有啟動服務，也沒有重跑自動化或實機步驟。
- 沒有試 Touch ID、Safari 或手機。
- 沒有清理暫存或裝置端 credential（Google 密碼管理工具裡那把 `localhost`／`a1-spike-tester` 測試 passkey，由 Aaron 辨認後自行決定）。

## 22 A1 限定核對結論與可行性切片完成（10/13）

### 22.1 Codex 限定核對（Aaron 轉貼）

> 核對完成。Chrome＋Google 密碼管理工具／PIN 這條 A1 實機路徑通過限定核對，不需要重跑。 Touch ID 不包含在這個結論內。

**核對範圍**：
- Codex 唯讀核對了完整 14 行、相關程式與 Git 差異。
- 來源與完整副本的 SHA-256 相同；第一關的舊副本也維持原雜湊。
- Codex 沒有重新啟動服務，也沒有操作驗證器。

**多出紀錄的判讀**（依 Codex 原意轉記）：

| 行 | 判讀與限制 |
|---|---|
| 2 `NotAllowedError` | 只能確認那次瀏覽器操作沒有完成，不能判定是取消、逾時或其他原因 |
| 4 第二次核准成功 | 用的是另一個 challenge，不是舊 assertion 重放成功；符合 spike 的行為，但**不能拿來證明 A2「同一請購只核准一次」** |
| 5、10～12 共四次 replay | 全部重送第二次核准的 challenge，也全部被拒；**不能寫成第一個 `ch-8944…` 的重送證據** |
| 7～9 共三次不要求 UV | 全部 `UV=0` 並以 `uv_missing` 拒絕；出現 passkey 選擇提示，不等於完成使用者驗證 |
| 13 `InvalidStateError` | localhost 上的重複登記，不算錯 origin 驗證 |
| 14 `SecurityError` | `127.0.0.1` 頁面的瀏覽器端拒絕，**不能寫成伺服器拒絕錯 origin** |

**流程偏離**：
- 實際次數超過原本的逐步安排。紀錄顯示：
  - 核准請求 2 次（都 verified）；
  - 重送 4 次（都 `CHALLENGE_USED`）；
  - 不要求 UV 3 次（都 `uv_missing`）；
  - 另有瀏覽器端的 `NotAllowedError` 1 次。
- 這些次數是請求紀錄，不推定為點擊次數。
- 保留為流程偏離。目前沒有看到它造成防護繞過，不刪除，也不重做紀錄。
- 第 21.4 節原本寫的「被按了兩次／三次」，已依此改成「紀錄顯示兩次／三次」；第 2 行的原因推測也一併刪除。

### 22.2 Aaron 的決定

原話（依序）：

> 請照 codex 指示做事
>
> 以這條路徑完成 A1 可行性切片也是我批准的
>
> Touch ID 不作為 A2 必要阻擋 也是批准的項目之一

**A1 可行性切片完成（Done，10/13）**：
- 範圍只限「Chrome＋Google 密碼管理工具（同步 passkey）／PIN」這一條路徑。
- 結論的依據：
  - Aaron 的實機操作與回報（第 20.4、21 節）；
  - 保存的完整證據 `artifacts/a1/real-device-chrome-gpm-complete-20261013T074258Z/`；
  - Codex 的限定核對；
  - Aaron 的批准。
- 這條路徑的意思（對照 D1）：這台 macOS 12.7.6 上，Chrome 150 透過 Google 密碼管理工具，能在 `rp_id=localhost` 以 PIN 完成登記與核准，並且 `fmt=none`、`BE=1 BS=1`、計數器 0/0。

**這個結論不包含**：
- **Touch ID**：未驗證，另列。依 Aaron 批准，不作為 A2 的必要阻擋。
- Safari、手機與外接安全金鑰。
- A2 的任何保證，例如同一請購只核准一次。
- 伺服器端的錯 origin 拒絕（第 14 行是瀏覽器端的拒絕）。

**歸屬**：
- Aaron：實機操作與目視回報。
- Claude Code：保存、整理與文件同步。
- Codex：唯讀核對與判讀。

**不做**：
- 不清理 Google 密碼管理工具裡的測試 passkey；要不要清理，由 Aaron 辨認後自己決定。
- 不清理 `/tmp/apa-a1.rUI7UP`。
- 不重跑，也不自動開始 A2。

**A2 與後續**：
- A2 接線仍需另行派工。依第 4.1 節，A2 會在 `localhost:8402` 重新登記，不沿用 spike 的 credential。
- 付款未啟用；付款金鑰、簽署與交易都沒有核准。

## 23 A2 實作（10/13）

依 vault〈Claude-Code-A2本人核准產品接線實作派工-f2bf224-20261013〉，在分支 `commercial-a2` 實作。實作差異、驗證證據、沒有做的事與實機部署清單，都記在 [commercial-a2.md](commercial-a2.md)，本文件不重複。

**狀態**：A2 已實作並完成本輪定向自動化驗證，待 Codex 技術 review；實機登記與操作未驗收；付款未啟用。A2 不自行標 Done。

**10/13 補記**：Codex 訂閱已到期。A2 的 review 改由 Claude Code 另開的獨立上下文 subagent 執行（commercial-a2.md 第 6 節），判定 not pass，R1～R6 已修正（commercial-a2.md 第 7 節）；第二至五次 review 的 should-fix 已修正（第 8～11 節）；之後的 review 也循此方式，紀錄一律寫明不是 Codex。
