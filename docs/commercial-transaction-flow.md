# 核准到交易完成：最小商業流程設計

> 2026-10-12，Claude Code。依 vault〈Claude-Code-核准到交易完成最小商業流程派工-20261012〉。基準 `ee9bf0e`（分支 `commercial-ux1`）。
> **狀態：設計草案，待 Codex 設計 review 與 Aaron 決定第 5 節的阻擋事項。** Aaron 核准的是「核准到交易完成的最小商業流程」這個**方向**；這不是付款帳戶模型、真實簽署、金鑰、faucet、部署或送交易的核准。付款帳戶 ADR（`docs/adr/payment-account-model.md`）仍是 Proposed。
> 本輪只讀程式與文件、寫設計：沒有改產品、沒有跑測試或展示、沒有執行真實 CLI。M1、M-B、UX1 維持完成。

依據（位置都在 repo 或 vault 的同一專案資料夾）：PRD-第一期（§3.1–3.2、§4–8、§10–12、FR-8、FR-15）、執行計畫（§1-sexies、§1-septies、§3.5、§7、§9.1、§10）、〈真人進店與授權Agent消費-後續體驗概念〉、`docs/adr/payment-account-model.md`、`docs/contracts.md`、`docs/persistence-design.md`、`docs/policy.md`、`docs/merchant.md`、`docs/commercial-demo-roadmap.md`（現行進度、§4–5、§9.2）。現行程式入口見第 2 節。

## 1 一筆真正完成的購物

首個案例沿用現有商店的數位資料商品 `kr-market`（0.50 test USDC，`allow_max` 0.10，所以一定走 Ask），不新建商店、不加虛構交易。首筆走 Ask 與本人核准；資料結構以 consumer 與消費意圖為鍵，B 的隔離沿用同一套驗證，付款可先一次一筆（序列）。

| 步 | 使用者看到 | 可操作 | 誰負責 | 可信狀態（誰說了算） | 證據 | 失敗呈現 |
|---|---|---|---|---|---|---|
| 1 委託 | 目前身分（consumer、錢包、驗證模式）、商品需求、商家範圍、資產與網路、任務預算、期限；「合規的小額自動通過、其他都要你核准」 | 只能縮小範圍；**確認委託並啟動**（與「核准付款」是兩個動作） | 本人；Host 寫入 | Store 的 `DELEGATION_CONFIRMED`（新契約） | 委託內容與 digest、確認者、時間 | 範圍不合法：不啟動、不建 task；重按不會建第二個 task |
| 2 請購 | 客人進店、選購；櫃台上的 402 報價 | 停止 Agent | Agent（沿用現有 Host 與 MCP 三個工具，不另寫 runner）；Policy 判定 | `PURCHASE_PROPOSED`、`POLICY_DECIDED`（Ask／Allow／Deny） | 報價、`intent_hash`、Policy 理由 | Deny：照現狀離店；報價不在白名單：拒絕 |
| 3 本人核准 | **這一筆**的商品、收款人、金額、資產、網路、期限（期限由後端決定） | 核准（WebAuthn＋摘要確認，語音三態）、拒絕 | 本人；Approval 服務 | `APPROVED`／`APPROVAL_DENIED`／`APPROVAL_EXPIRED`／`APPROVAL_WITHDRAWN` | 摘要、核准會話、authenticator、`amr`、語音結果 | 過期、取消、報價改變：此會話關閉，**不會**轉成核准；主持人配對與 demo token 不能當本人核准 |
| 4 grant | 「已核准，準備付款」 | — | Approval 服務發 grant（模型、wrapper、CLI 都不能發，也不持有金鑰） | `GRANT_ISSUED`（一次性、短效、綁同一個 `intent_hash`） | grant 的 jti、exp、audience=signer | grant 過期或已用過：Signer 拒絕 |
| 5 付款 | 「付款中」→「已送出，等待鏈上確認」 | 不能取消已送出的付款（取消只停止新動作） | Policy gate 送預留（tx#1）；Signer 在 grant＋鏈上 `intent_hash` 都在時才簽；buyer 帶簽章重送；facilitator verify／settle（tx#2） | `BUDGET_RESERVED`、`PAYMENT_SUBMITTED`；兩筆交易各自的 receipt | 兩筆 tx 的 RPC receipt 與 explorer 連結，明寫「不是同一筆交易」；ZK proof、憑證 presentation | 報價變了：回到 Ask；結果未知：「確認中」，不自動再付 |
| 6 結算與交付 | 「已付款」只在對帳後；「已取貨」只在商店真的交付後 | — | Reconciler 以鏈上 Transfer（看 token 的 `from`，不看 facilitator 的 tx sender）確認；商店交付 | `PAYMENT_SETTLED`（對帳後）、`RESOURCE_DELIVERED`、`RECEIPT_ISSUED` | 鏈上事件、`PAYMENT-RESPONSE`、交付內容的 hash | 已結算未交付：「已付款，交付失敗」，進入人工處理；不把交付失敗說成付款失敗 |
| 7 查看 | 收據、交付結果、Agent 依交付資料寫的結果；任務預算、餘額、gas、模型用量分開列（拿不到就寫未知） | 開證據入口 | Store、Reconciler | 同上 | 鏈、hash、proof、grant、交易明細都在證據入口 | 來源失聯：未知／過期，不顯示 0 |
| 7′ Admin | 已請購、待核准、已付款、已交付、失敗、未知 | 從統計展開到這一筆 | 具權限的 Admin（唯讀） | 依 `intent_hash` 去重 | 同上 | 同上 |

日常畫面用購物、核准、付款、取貨的話；鏈、hash、proof、grant、交易明細在證據入口。測試網、合成資料、開發展示例外照舊明標，不能隱藏風險或假稱真實營收。

## 2 現在有什麼、缺什麼

M1 刻意沒有核准、grant、付款、結算或交付：`contracts.md` 第 87 行與 `internal/contracts/events.go` 的白名單拒絕 `APPROVED`、`GRANT_ISSUED`、`SETTLED`、`RESOURCE_DELIVERED`；`PolicyDecision.Validate` 強制 `payment_status=unavailable`、Ask 只能是 pending／expired／withdrawn；商店收到合法的 `PAYMENT-SIGNATURE` 也回 402 `M1_PAYMENT_UNAVAILABLE`、從不交付。所以後面每一步都是新程式與**新契約**，不是在 M1 上加欄位。

可以直接沿用的入口：

| 用途 | 現有入口 |
|---|---|
| 開 task、綁 consumer／錢包、預算與期限快照 | `store.StartTask`（`internal/store/ops.go:318`）、`TaskSetup`（`store.go:574`）、`contracts.RegistryConfig.Binding` |
| 請購 | `toolbridge.Shop.Call` → `assemble` → `Store.Submit`（`ops.go:443`）→ `policy.Decide`（`internal/policy/policy.go:98`） |
| Ask 的期限、取消、到期 | `expire`、`Cancel`、`ExpireDue`／`RunExpiry`（`ops.go:296–659`）；期限從 Ask 的 `observed_at` 起算，`elapsed >= deadline` 即到期 |
| 冪等 | 重試鍵 `(consumer_id, task_id, wallet_id, request_id)`；`(chain, asset, payer, eip3009_nonce)` 唯一 |
| 主持人頁與 viewer 權限 | `cmd/demo/api.go` 的角色（viewer 只看自己、host 唯讀、operator 控制）；`web/host.js` 的操作卡 |
| 商店報價 | `internal/merchant`（`kr-market`，`eip155:84532`，`exact`，60 秒） |

注意 `cmd/demo/api.go:23` 已有一個叫 `grant` 的型別（HTTP 角色），新設計的付款 grant 一律叫 `AuthorizationGrant`，程式裡另取名，不重用。

## 3 最小狀態與介面

### 3.1 正式付款契約另立名稱

- 名稱 `apa.pay.v1`；digest 網域 `agent-payment-authority/pay/v1/<kind>`。M1 的 `apa.m1.v2` 與網域 `…/m1-fixture/v2/verifier-not-deployed` 不動，**也不拿來做鏈上、EIP-712 或 ZK 值**。
- `PurchaseIntentV1`（業務意圖：consumer、agent、wallet、task、`purchase_order_id`、商家、`resource_hash`、方法與網址、商品、金額、資產、網路、`pay_to`、有效期）＋ `PaymentBinding{method, chain_id, asset_contract, from, pay_to, amount, valid_after, valid_before, nonce}`；`intent_hash = H(網域 ‖ canonical(業務意圖 ‖ payment_binding))`，兩者都蓋到（`contracts.md` 233–237 的遷移要求）。
- `intent_hash` 是 256 位元 SHA-256，超過 BN254 欄位：ZK 端以 limbs 或欄位 hash 對映（ADR 開放問題 4，Z1 處理）。
- 新的 golden vectors 與回歸測試；新理由碼一律隨契約版本增加。
- 委託、核准會話、grant 與付款事件都屬於 `apa.pay.v1` 的 task；M1 task 的事件規則不變（以 task 的契約版本選驗證規則，不放寬舊版白名單）。

### 3.2 新物件

| 物件 | 欄位（最小） | 誰寫 | 一次性／冪等 |
|---|---|---|---|
| `Delegation` | `delegation_id`、consumer、需求、商家範圍、資產與網路、任務預算、`allow_max`（只能縮小 profile 的值）、期限、確認者與確認方式、狀態 | 本人確認，Host 寫入 | `delegation_id` 由確認請求的 request id 決定；同一請求重送回同一筆 |
| `ApprovalSession` | `approval_session_id`、`intent_hash`、canonical 摘要、challenge（`H("payment-approval/v1" ‖ intent_hash ‖ voice_nonce ‖ approval_session_id)`）、期限（=Ask 的期限）、狀態 | Approval 服務 | 每個 Ask 一個；收到核准後就關閉 |
| `AuthorizationGrant` | `intent_hash`、`decision`、`authorization_method`（`webauthn`／`policy`／`simulated`）、`issuer`、`audience=signer`、`issued_at`、`expires_at`、`jti`；Ask 另有 `approver_id`、`device_id`、`credential_thumbprint`、`amr` | Approval 服務（Ask）；Policy gate（Allow） | `jti` 只能用一次；Signer 用過即記錄 |
| `PaymentAttempt` | `intent_hash`、grant `jti`、預留 tx、付款簽章參數、送出時間、facilitator 回覆、對帳結果 | Policy gate、Signer、Reconciler | 以 `intent_hash` 為冪等鍵：同一意圖最多一個進行中的嘗試 |
| `Delivery`、`Receipt` | `intent_hash`、`PAYMENT-RESPONSE`、交付內容 hash、時間 | 商店、Host | 以 `intent_hash` 為鍵 |

### 3.3 狀態（後端決定；前端倒數不決定任何事）

```text
購物委託：draft → confirmed → (task 結束時) closed
核准：    pending → approved | denied | expired | withdrawn | superseded（報價或意圖改變）
付款：    not_started → reserved（tx#1）→ submitted → settled（對帳後）
                                  ↘ unknown（送出後結果不明）→ settled | not_settled_expired | needs_review
          reserve_failed、not_settled_expired 都不花預算以外的錢；needs_review 只靠人工
交付：    not_due → delivered | delivery_failed（已付款）
```

## 4 必要依賴

| 能力 | 對應切片 | 首片需要？ |
|---|---|---|
| 付款帳戶決定（EOA＋EIP-3009 或候選） | P0 | 契約的 `payment_binding` 需要 |
| 正式契約、委託、核准會話、grant 格式 | P1（前半） | 是（首片本身） |
| 金鑰庫與獨立 Signer（本機測試金鑰、不上鏈） | P1（後半） | 否 |
| WebAuthn（實機 spike，再接 Approval） | A1 → A2 | 否；首片明標「模擬核准」 |
| 語音三態 | V1（需 A2） | 否；明列未完成 |
| BBS+ 憑證 | C1 → C2 | 否 |
| ZK 預算證明與鏈上 verifier | Z1 → Z2 | 否 |
| Base Sepolia 兩筆交易、對帳、交付 | P2（需 P0、P1、Z2、testnet 核准） | 否 |
| 部分整合、最終驗收、營運聚合 | E1-a ＋ M-D、E1-b | 否 |

## 5 決策表

| # | 項目 | 既有規則 | 推薦 | 影響 | Aaron 決定？ | 最晚 |
|---|---|---|---|---|---|---|
| 1 | 付款帳戶 | ADR Proposed：A（每位 consumer 一個 EOA＋EIP-3009，隔離 Signer）是預設路徑；B1／B2／C 需 S4-B 證據 | **保留 A 作為基線，S4-B 列為選做**；由 Aaron 決定是否把 ADR 改為 Accepted（本文件不代改） | A：沒有鏈上錢包政策的說法，限制只在鏈下；改 B 要同步 PRD、契約、Signer、狀態機與圖 | **是（阻擋首片的契約）** | 首片開工前 |
| 2 | 本人核准 | WebAuthn＋摘要確認必要；語音是附加訊號，三態；registered authenticator 用語 | 首片用**明標的模擬核准**跑通狀態與 grant 格式，`authorization_method=simulated` 的 grant Signer 一律拒絕；A1→A2 接 WebAuthn 後才可能付款；語音三態留在 V1，未接入前明列未完成 | 一般按鈕永遠不算正式本人核准；demo token 不能核准 | 是（同意首片用模擬核准且不啟用付款） | 首片開工前 |
| 3 | 契約 | M1 digest 不得上鏈；正式契約另命名、版本化 | `apa.pay.v1`、網域 `agent-payment-authority/pay/v1`；業務意圖＋`payment_binding` 一起進 `intent_hash`；以 task 的契約版本選事件規則 | Store 要加資料表與事件（schema 3）、UI 新增狀態、checker 認得新版本；M1 證據與檢查器不變 | 是（核准契約與 Store 遷移的範圍） | 首片開工前 |
| 4 | 核准與取消、到期、報價變動 | 期限 `>=` 即到期；取消先撤回未決 Ask | 一律在 Store 的寫入交易裡判：核准請求到達時會話必須仍是 pending、`intent_hash` 相同、未過期；同一時刻到期優先；報價改變使會話 `superseded`，重新 Ask | 前端只顯示；晚到的核准回 `APPROVAL_CLOSED`，不會變成付款 | 否（推薦即可） | — |
| 5 | 付款恢復 | 不自動重付；facilitator 逾時不是失敗 | 區分未送出、已送出結果未知、已結算、交付失敗；冪等鍵 `intent_hash`＋grant `jti`；重啟時 `submitted` 一律轉 `unknown` 再對帳；授權過期且鏈上無結算才標 `not_settled_expired` | 永不因逾時自動再付；作廢（void）留到第二版 | 否 | P2 前 |
| 6 | 兩筆交易不具原子性 | tx#1 預留與 tx#2 結算各自一筆 | 預留成功、付款失敗：預算已預留未付款 → `needs_review`；不宣稱整個流程原子完成，UI 明寫「不是同一筆交易」 | 預留的預算不自動釋放 | 否 | P2 前 |
| 7 | 容量 | 三種數量分開；20 不是硬上限 | 資料一律以 consumer／`intent_hash` 為鍵；首片驗收單筆，但不寫死一位使用者（測 B 的隔離）；付款同時執行數是另一個部署設定，P2 先設 1 | 不影響已完成的雙 Agent | 否 | — |
| 8 | 誰送預留交易（tx#1） | 計畫預設 Policy gate；PRD 寫未決 | Policy gate（專用 gas key），Signer 不送 | P2 的金鑰與 gas 帳戶 | 否 | P2 前 |
| 9 | ZK proof 位置 | 計畫第 10 節固定「先核准、再鏈上預留、最後簽」 | 照第 10 節：核准後產 proof 並上鏈（預留），再簽 | 計畫 §1-sexies「拍 5」的寫法要同步 | 否 | Z2 前 |
| 10 | 建立委託是否要 WebAuthn | 概念文件未決 | 首片不需要（委託不等於付款核准）；A2 後可加 | 委託的 assertion 不能拿來當單筆付款 assertion | 否 | A2 前 |
| 11 | testnet、faucet、facilitator、測試錢包 | roadmap §9.2 #13 阻擋付款 | 留到 P2 時集中申請（範圍、執行者、次數、帳戶、資產與 gas 上限、停止條件） | 本輪與首片都不建立任何金鑰、不碰 faucet 或鏈 | 是 | P2 前 |
| 12 | Admin 讀取範圍 | roadmap §9.2 #20 | 新增唯讀 `admin` 角色（token 檔，與 host／operator 分開），只看統計與單筆證據，不提供公開聚合端點 | M-D 時實作 | 否（推薦） | M-D 前 |

**真正阻擋首片的只有 1、2、3**：付款帳戶（決定 `payment_binding` 的方法）、首片以模擬核准跑通且不啟用付款、契約與 Store 遷移的範圍。其他都有推薦答案，不必現在決定。

ZK、BBS+、WebAuthn、語音、AI 評測與多場景仍是商業展示版的既定出口；首筆流程分片開發，缺任何必要能力就明標「部分整合」。本文件不改變最終要求。

## 6 切片順序

沿用 roadmap 的編號，不用 B2。

| 順序 | 切片 | 產品成果 | 依賴 | 測試 | 停止點 |
|---|---|---|---|---|---|
| 0 | **P0** | ADR 決定（推薦保留 A） | — | 無 | Aaron 決定 |
| 1 | **P1-a 委託與單筆核准入口**（首片） | 正式契約 `apa.pay.v1`；委託、核准會話、grant 格式；主持人頁的委託面板與核准面板（明標模擬核准，不付款） | P0 | 契約向量、Store 狀態機、競態與權限反例、UI 定向流程 | 模擬核准產生的 grant 被 Signer 樁拒絕 |
| 2 | P1-b 金鑰庫與獨立 Signer | 本機測試金鑰；Signer 只接 `webauthn`／`policy` grant，檢查 jti、exp、`allow_max`、錢包所屬 | P1-a | 正例與錯 consumer、錯意圖、重播、過期等反例 | 不上鏈 |
| 3 | A1 → A2 | WebAuthn 實機 spike → 接到核准會話與 grant | A1 獨立；A2 需 P1-a | 實機與 virtual authenticator（明標 fixture/simulated） | 正式本人核准可產生 grant |
| 4 | Z1 → Z2 | 電路、`intent_hash` 對映、本機鏈的預算合約 | Z1 獨立 | 電路與合約測試 | 本機鏈 |
| 5 | C1 → C2 | BBS+ 選型 → 接 Policy | — | 撤銷 fail-closed 等 | — |
| 6 | P2 | Base Sepolia：預留、簽署、verify／settle、對帳、交付、收據 | P0、P1、Z2、testnet 核准 | 結果未知、重啟恢復、交付失敗 | 每次真實執行另行核准 |
| 7 | V1 | 語音三態 | A2 | 通過、失敗、不可用 | — |
| 8 | E1-a ＋ M-D | 部分整合；最小 Admin（已請購、待核准、已付款、已交付、失敗、未知；依 `intent_hash` 去重；按鏈、資產與證據模式分列；來源失聯顯示未知／過期） | P2 | Admin 權限與去重 | — |
| 9 | E1-b | 最終驗收（加 C2、A2、V1、AE1） | 全部 | — | — |

不先做空白多場景選單、通用流程引擎、影片或大量 Agent 壓測。

## 7 首片派工草案：P1-a 委託與單筆核准入口

**為什麼先做這片**：付款主線每一步都要綁同一個 `intent_hash` 與一次性 grant；先把正式契約、委託、核准會話與 grant 格式定下來，P1-b 的 Signer、A2 的 WebAuthn、P2 的付款都只是接上去，不必回頭改資料結構。它不碰金鑰、不碰鏈，風險低。

**範圍**（待第 5 節 1–3 決定後開工）：

| 部分 | 檔案與介面（預計） |
|---|---|
| 契約 | `docs/contracts.md` 新增 `apa.pay.v1` 一節；新套件 `internal/paycontract`（`PurchaseIntentV1`、`PaymentBinding`、`AuthorizationGrant`、canonical 與 digest、`testdata/paycontract/vectors.json`） |
| Store | schema 3：`delegations`、`approval_sessions`、`grants` 三張表；事件 `DELEGATION_CONFIRMED`、`APPROVAL_SESSION_OPENED`、`APPROVED`、`APPROVAL_DENIED`、`APPROVAL_SUPERSEDED`、`GRANT_ISSUED`（只在 `apa.pay.v1` task 允許）；遷移照現有 schema 1→2 的方式（驗證、備份、遷移、再驗證） |
| 服務 | 新套件 `internal/approval`：`ConfirmDelegation`、`OpenSession`（Ask 時）、`Approve`（同一 Store 交易內判斷 pending、`intent_hash`、期限）、`Deny`、`IssueGrant`；`authorization_method=simulated` 只在 fake 模式可用 |
| Signer 樁 | `internal/signer` 只有 `Accept(grant)`：檢查 audience、exp、jti 一次性；`simulated` 一律拒絕（`GRANT_METHOD_NOT_ACCEPTED`）。不持有金鑰、不簽 |
| Host API | consumer 本人的委託與核准端點，與 operator 的開始／停止分開；權限：只有該 consumer 的身分能確認委託與核准；operator、host、別的 consumer 一律 403 |
| 網頁 | 主持人頁：開始前的委託面板（確認委託並啟動）；Ask 時的核准面板（摘要、模擬核准、拒絕），明標「模擬核准・不會付款・不是正式本人核准」；操作卡新增「待你核准」「已核准（模擬）」狀態 |
| 不做 | 金鑰、簽署、預留、鏈上任何東西；WebAuthn、語音、BBS+、ZK；Admin |

**最小驗收**（fake CLI、合成資料；不跑真實 CLI）：
- 正例：A 確認委託 → Agent 請購 → Ask → A 模擬核准 → grant 產生（method=simulated）→ Signer 樁拒絕 → 畫面寫「已核准（模擬），未啟用付款」。
- 反例：B 核准 A 的請購（403）；operator 或 host token 核准（403）；摘要中的金額或收款人與 `intent_hash` 不符（拒絕）；過期後核准（`APPROVAL_CLOSED`）；取消後核准（`APPROVAL_CLOSED`）；同一核准重送（回同一結果，不產生第二個 grant）；grant 重播（jti 已用）；報價改變後的核准（`superseded`）；重啟後會話與 grant 狀態不變。
- 反向：拿掉 consumer 比對、`intent_hash` 比對、期限判斷、jti 一次性，各自讓對應斷言失敗（一次性副本）。
- 回歸：契約與 Store 的定向測試、M1 檢查器在 M1 證據上照舊通過（不重新產包）、UX1 的焦點與 ux1-ui 相鄰流程。

**fake 操作步驟草案**：
1. `make present-fake`（或新的彩排目錄），輸入配對碼。
2. 在 A 的卡按「建立委託」：看商品需求、商家範圍、預算 5.00 test USDC、期限；按「確認委託並啟動」。
3. A 進店請購，卡上變「待你核准」；按「查看」打開核准面板：商品 `kr-market`、收款人、0.50 test USDC、Base Sepolia、期限。
4. 按「模擬核准」：卡上寫「已核准（模擬），未啟用付款」；證據入口看到 `GRANT_ISSUED`（simulated）與 Signer 拒絕的理由。
5. 另開一筆，等期限過後再按核准：看到「已過期，無法核准」。
6. 換 B：B 的核准面板看不到 A 的請購。

## 8 本輪沒有做的

- 沒有改產品、契約、Store 或測試；沒有跑測試、展示、容器、M1 產包或真實 CLI。
- 沒有建立錢包或金鑰、沒有 faucet、部署、簽署或任何交易；沒有改 ADR 狀態。
- 沒有重開 plugins 調查或延伸診斷工具。
