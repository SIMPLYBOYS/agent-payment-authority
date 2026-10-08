# 商業展示版交付路線（提案）

> **狀態：路線提案，待 Codex review 與 Aaron 核准；未核准實作。** 本文件只做現況盤點與交付順序設計。派工草案（§8）不等於已核准開工；任何切片、真實 CLI、憑證存取、testnet、金鑰或資金操作都須各自核准。
>
> - 撰寫：Claude Code，2026-10-07。依據：vault〈Claude-Code-商業展示版交付路線規劃指示-c3fcc94-20261007〉。
> - 開工核對：分支 `b1-06-demo-integration`，HEAD `c3fcc94`，main `5391373`；工作區只有未追蹤的 `demo` 執行檔（未動）。
> - 本輪未改程式、測試、契約或驗收門檻；未跑任何測試；未執行真實 CLI、RPC 或 testnet。
> - **修訂（2026-10-07）**：依 Codex 對 `638c9f7` 的 review（vault〈Claude-Code-商業展示版路線限定修訂指示-638c9f7-20261007〉）補正 R1–R4 與兩處文字，對照見 §11。仍是提案；兩位真實 Agent 的門檻、ADR 選項與新切片仍待 Aaron 核准。

## 0 摘要

- **現在有什麼**：fake 資料鏈與展示已相當完整（Unity＋LimeZu 商店、主持人操作、D1 證據面板、D2 場景動畫，各切片通過限定 review）。真實技術主線方面：
  - 真實 AI：CLI 的 live 開關與 init 閘門已驗收（fake 驗證），真實 CLI 只取得過一次 init；**還沒有任何真實請購 trace**。
  - 授權／憑證／ZK：確定性 Policy 已驗收；**人的核准、grant、BBS+、gnark 都還沒有實作**（S1–S3 是 Ready、未領取）。
  - 鏈上付款：只有商店的 402 報價；**Signer、預留、結算、交付都未實作**，付款帳戶 ADR 仍是 Proposed。
- **差距的形狀**：已有可操作的 fake 展示基礎；商業視覺、多 Agent／Admin 體驗與真實技術整合仍有缺口。D1／D2 通過不代表已達到 WareTrack 參考體驗；真實技術方面，缺的是三條線本身，以及把它們綁到同一個 `intent_hash` 的整合。
- **建議第一切片**：**L1「B1-04 live 解阻與一次真實請購 trace」**（§8）。理由：程式面已備妥（階段 A 已驗收），剩下的是有界的決定與一次受控執行；它同時是 M1 收尾（B1-06 live trace、B1-07）與後續所有綁定的前提。
- **多 Agent 與 Admin 營運視角**（§6）：與現行「一次一個 active task、Host 同時一場」的基線不同，須先修訂範圍。建議先完成單筆真實流程再擴為兩位真實 Agent，並在 M1 收尾後就提出範圍修訂與投影設計（M-A），避免之後重做畫面與場次協調。
- **L1 的決定項與最晚決定時點**見 §9.1：只派 L1-a 時只需決定派工與分支兩項，不連帶核准設定讀取、認證清單變更或真實執行；付款線開工前必須有的 ADR 決定列在 §9.2。

## 1 依據、讀取範圍與固定邊界

### 1.1 讀過的資料（vault 為現行工作區文字，含 Codex 10/7 新增、尚未提交的段落）

| 資料 | 用途 |
|---|---|
| vault `執行計畫.md`（含 L9 的 10/7 多 Agent 營運體驗補充、L11 的 10/7 最終交付要求） | 範圍與驗收的唯一來源：週次 W1–W15、§5 驗收 1–44、電路與合約設計、順序規則、停損 |
| vault `PRD-第一期.md` | 角色、§4 情境（成功 Ask、Allow、Hard Deny、攻擊、pending）、§5 畫面、§6 狀態、§8 證據標示、§9 FR-1～25 |
| vault `第一批任務清單-M1-請購與裁決.md` | B1-00～07、S1～S4 狀態與 M1 完成門檻（L34、L61–75） |
| vault `商店互動展示設計-WareTrack動畫參考.md`「商業展示版完成條件」 | 最終交付的六個成分與不得替代的做法 |
| vault `真人進店與授權Agent消費-後續體驗概念.md` | 區分既定必做（WebAuthn／語音在第一期）與後續體驗（委託頁、語音下單、人臉） |
| repo `README.md`、`docs/m1-boundaries.md`、`docs/cli-poc.md`、`docs/demo-operation-design.md`、`docs/demo-presentation-design.md`、`docs/contracts.md`、`docs/adr/payment-account-model.md`、`docs/spikes/payment-policy-compatibility.md`，以及程式目錄盤點 | 實際狀態與證據位置 |

以下引用 vault 檔案時用 `計畫 L<n>`、`PRD §<n>`、`任務清單 L<n>`。行號以 2026-10-07 工作區版本為準（含 Codex 同日新增的多 Agent 補充；這些檔案仍在修改中，行號可能再位移，引用處同時寫出章節或內容）。

### 1.2 固定邊界（已定案，不在本文件重新討論）

- 展示層是 **Unity＋LimeZu**，HTML／CSS 只做 Dashboard（PRD §2，10/5）。
- 最終作品必須有**真實 AI、授權／憑證與真 ZK、鏈上付款與商店交付**，接成同一條可查證的消費流程（計畫 L11、WareTrack「商業展示版完成條件」）。
- 視覺約 60％、證據約 40％ 是展示重心，**不是完成率、工時或可互相抵銷的分數**。
- 付款基線：Base Sepolia、USDC 的 EIP-3009、獨立 Signer、**預留與結算是兩筆不同的 tx**（計畫 L481、L563；PRD G1）。改基線須先有 Aaron 核准的 ADR（計畫 L671）。
- 不碰 mainnet 與真錢；不宣稱 production、不宣稱有使用者、不稱第三方稽核（計畫 L791–792；PRD §11）。
- 商業展示版 ≠ production 系統（計畫 L11）。

### 1.3 用語

- **狀態**：已驗收（有 review 結論）／已實作待驗收／僅 PoC／僅設計／未實作／尚未查明。
- **模式**：fake（fake CLI 或 fixture 資料）／live（真實外部系統）／replay（重播既有紀錄）。
- **證據歸屬**：「Codex 獨立執行」「Claude Code 執行、Codex 核對紀錄」「Claude Code 執行、待 review」分開寫。
- 本文件不以任務完成數推算整體完成率。

## 2 現況與缺口

分列八條能力。「驗收編號」指計畫 §5 的 1–44 與 PRD §9 的 FR。repo 目前**沒有** `tests/` 目錄、沒有憑證／ZK／Signer／付款程式（盤點依 `git ls-files` 與全文搜尋，2026-10-07）。

| # | 能力 | 任務／驗收對應 | 位置 | 狀態（模式） | 證據與歸屬 | 尚缺 |
|---|---|---|---|---|---|---|
| 1 | Unity＋LimeZu 商店、主持人操作、D1／D2 展示與證據對應 | B1-06（交付點 1、2、3.1–3.5、D1、D2）；FR-17、FR-20；驗收 37–40 | `unity/`、`web/`、`cmd/demo`；`docs/demo-ui.md`、`docs/demo-operation-design.md`、`docs/demo-presentation-design.md` | 各切片**已驗收**，範圍是 fake 資料鏈與 replay；B1-06 整卡 InProgress | 交付點 1 `f2d63fb`、2 `6aee7f0`、3.1 `b2b7daa`、3.2 `69ae9b2`、3.3 `2c55835`、3.4 `3a6af59`、3.5 通過技術 review（自動化 `667e933`，最近產品修正 `d18efa2`）、D1 `6338fd2`、D2 產品 `78cb2cc`／測試 `6137708`。最近完整容器七步與 247 突變屬 `d7070af`（Claude Code 執行、Codex 核對紀錄） | opt-in live trace、`tests/m1/`、3.5 人工清單（16.3）未逐項確認、`R2 order` 測試穩定性；畫面目前只能呈現 Ask／Deny／到期／撤回，**沒有 proof、grant、tx、交付可呈現**；D3–D6 未開始 |
| 2 | 多 Agent 流程重疊、Admin 活動總覽與消費聚合、從角色逐筆查證 | WareTrack「多 Agent 店內活動與營運查證體驗」、計畫 L9（10/7，待範圍 review）；現行基線為 PRD §3.2、§5 與計畫 L444 的「一次一個 active task」 | `cmd/demo/control.go`（L197–199 `run_active`）、`cmd/demo/api.go`（角色）、`web/app.js`（一次一位 viewer）、`Projection.cs`（L156 每世代一個 task） | **僅設計**（本文件 §6 提案）；現行程式**刻意**限制為單場、單 task 投影 | 單場控制與恢復已驗收（3.2 `69ae9b2`、3.4 `3a6af59`） | 範圍修訂與 Aaron 確認門檻；多場協調、Store 並行驗證、多角色投影、Admin 聚合端點與權限；兩位真實 Agent 的重疊執行（每次另核准） |
| 3 | 真實 Buyer CLI、認證邊界、完整 live 請購 trace | B1-04；計畫 W1、W5；PRD §3.1 | `cmd/apa-buyer`、`internal/buyer`（`gate.go`、`host.go`、`probe.go`、`runner.go`）；`docs/cli-poc.md` | fake **已驗收**；live 階段 A（開關、init 閘門）**已驗收**（以 fake 驗證）；真實執行只有 C0（30 秒未取得 init）與 C0′（取得 init）；**階段 B、C 未執行**，沒有 live 請購 trace | fake `b3c66f6`、階段 A `e8f6719`、C0′ 程式 `05e03e2`、身分觀測 `b46e332`（Codex review）；真實 C0′ 證據 `artifacts/b1-04/c0prime-real-20261004T054735Z/`（Claude Code 執行、Codex 核對） | init 列出 plugins 2、agents 5，**來源未確認**，閘門拒絕非空清單；`apiKeySource=none`，live 認證接受清單為空；階段 B 本人確認；一次階段 C；遮罩後的工具呼叫證據（D6） |
| 4 | 真人委託與付款核准：WebAuthn／語音、Policy 與 grant | S1；計畫 W2、W7–8；FR-5、7、8、9；驗收 4、5、9、20、29、36 | `internal/policy`（`docs/policy.md`）；WebAuthn 只保留 port 8123 | 確定性 Policy **已驗收**（用 fixture 授權條件）；**核准、grant、WebAuthn、語音都未實作**（契約明定 M1 無 `APPROVED`、`GRANT_ISSUED`）；真人委託頁**僅設計** | Policy `61b3652`、Store `cb33768`（B1-05 Done） | S1 Ready、未領取；Approval 服務、grant 格式與簽章鑰、Signer 接收路徑；語音三態與評測；委託頁設計（M1 後） |
| 5 | 憑證發證／出示／驗證（BBS+） | S2；計畫 W1（選庫）、W3；FR-1–3；驗收 1、2、3、12、26、28、29、35 | 無程式；Policy 以 fixture 結構代替（`internal/policy/policy.go` 註明 "nothing here verifies a BBS+ proof"） | **未實作**；S2 Ready、未領取 | — | 選庫（計畫只定 W3C Data Integrity BBS cryptosuite，未指定 Go 庫）；發證、derived proof、撤銷、holder binding、跨 verifier 重播；計畫的停損（選庫卡關改 SD-JWT VC，計畫 L637）屬改變必要能力，須 Aaron 核准 |
| 6 | gnark 額度證明、狀態更新與重播防護、鏈上 verifier | S3；計畫 W4（v1）、W9（v2＋MPC）、W10（有狀態合約）；FR-10–12；驗收 10、11、15、16、17、27 | 無程式（全文搜尋 `gnark`／`circuit`／`nullifier` 在程式中無結果） | **未實作**；S3 Ready、未領取 | — | 電路 v1／v2、phase-2 setup、Solidity verifier 與 `registerTask`／預算合約；`intent_hash`（SHA-256）超過 BN254 scalar field，作 public input 須拆 limb 或改 field hash（ADR 未解問題 4） |
| 7 | 獨立 Signer、付款授權、x402 結算、商店交付與對帳 | S4-A、S4-B；計畫 W2、W11、W12；FR-4、13–15、19、25；驗收 7、9、24、25、30、31、39、44 | `internal/merchant`（目錄與 402）、`internal/x402`（只有 header 編解碼，"verifies and settles nothing"）；`docs/adr/payment-account-model.md`；`docs/spikes/payment-policy-compatibility.md` | 商店目錄與 402 **已驗收**（B1-03）；Signer、預留、結算、交付、對帳**未實作**；付款帳戶 ADR **Proposed**（研究順序已接受，未採用候選）；S4-B Waiting | B1-03 `ebd3f44`；S4-A 契約影響審查 Accepted（`2dc8d93`） | ADR 決定；keystore 與服務隔離（W2）；testnet 錢包、資金、RPC、facilitator；正式付款契約（M1 的 fixture digest 不得作鏈上、EIP-712 或 ZK 值）；交付路徑；reconciler |
| 8 | `tests/m1/`、B1-07、端到端證據與安全驗收 | B1-06（`tests/m1/`）、B1-07；FR-23、24；驗收 41–43 | `scripts/verify/`（容器驗證、證據匯入、突變）、`scripts/verify-b1-01.sh` | `tests/` **不存在**；B1-07 **未實作**（Waiting）；驗證基礎設施**已實作**並用於各切片 | 容器七步＋247 突變（`d7070af`） | `tests/m1/` 案例與執行證據；`cmd/verify-demo` 只讀 checker（M1 子集）；第一期完整證據包（41–43）隨各模組補齊 |

**另兩項第一期必要能力**（不在指示列出的七項內，但影響最終驗收）：AI 評測（FR-21 injection 對抗、FR-22 兩模型比較、正常任務成功率等門檻）與模型用量分列——**未實作**，依附在第 3 項的 live 路徑之後。

**文件現況不一致**（本輪不改，列出供同步）：vault 任務清單表格的 B1-06 列停在 3.2，之後的 3.3–3.5、D1、D2 只記在紀錄區；`docs/m1-boundaries.md` 狀態表中交付點 2 仍寫「待 Codex review」，README 已記為 `6aee7f0` 通過。

## 3 一條完整消費主線（目標，依既定架構）

以韓國數位資料商店（KRW 700 ↔ 0.50 test USDC，fixture 匯率）為唯一主場景。步驟與責任依計畫 L829–836（八步腳本）、L562（付款路徑）、L958（順序定死：先 Ask／核准，再鏈上 Reserved，最後簽）與 PRD §4.1；**不為敘事改順序或權限**。「現況」欄是目前 repo 的狀態。

### 3.1 主線步驟（成功 Ask）

| # | 觀眾看見 | 執行元件 | 可信輸入 → 輸出 | 可查證證據 | 失敗時畫面 | 何時才可進下一步 | 現況 |
|---|---|---|---|---|---|---|---|
| 0 | （展示前）A 的錢包、憑證、task 已登記 | Issuer、keystore、預算合約 | 可信設定 → BBS+ 憑證（`task_id, C₀, expiry, holder binding`，計畫 L303）；`registerTask`（issuer 簽章）在鏈上登記 `C₀` | 憑證 metadata（不揭露全文）、`registerTask` tx receipt | **未核准的未來入口提案**：憑證缺失／過期時不啟動 task。現行語意不同：Store 接受明確缺少設定的 task，交 Policy 得到 Deny `CONFIG_MISSING`（`docs/persistence-design.md` §5.2、§9.7）；本文件不改寫此語意，要改須另行設計與核准 | 憑證與 `C₀` 登記都可查 | 未實作（M1 用 fixture 授權條件） |
| 1 | 真人確認 consumer、錢包與委託範圍 | Host（後續委託頁） | 已認證 session → task 委託紀錄 | 委託摘要與版本 | 未確認不啟動模型 | 委託確認；**不產生通用 human grant**（計畫 L471–473） | 僅設計（真人進店概念；M1 由主持人選情境） |
| 2 | 客人 A 進店，顯示任務與 live／fake | Buyer wrapper → 官方 Claude Code CLI | 可信 registry 綁 consumer／agent／wallet／task → CLI `system/init` 過閘門 → Store 開檔、`TASK_STARTED` | init 診斷白名單、run trace、`TASK_STARTED` 事件 | init 未過閘門：不開 Store、不寫 task（階段 A 規則） | 閘門通過且 `TASK_STARTED` 已提交 | fake 已驗收；live 只取得過 init，閘門因 plugins／agents／認證停下 |
| 3 | 客人看商品、到櫃檯，顯示報價 | 模型 → MCP bridge → 商店 | `list_products`、`get_quote` → 商店回 **HTTP 402**（x402 v2 `PAYMENT-REQUIRED`） | 402 白名單內容、`payment_requirement_hash` | 402 讀取失敗：停在選購，不顯示價格 | 收到真實 402 | 商店 402 已驗收（fake CLI 觸發）；live 模型未跑過 |
| 4 | 櫃檯顯示請購 | 模型 `request_purchase` → Host | Host 依可信設定組裝 canonical `AuthorizationIntent`（計畫 L291–295）→ `intent_hash`、`purchase_order_id`；`PURCHASE_PROPOSED` | 請購與 `intent_hash`（目前是 fixture domain） | 請購不成立：顯示原因 | 請購已提交 | fake 已驗收；`intent_hash` 為 fixture domain，非真實付款綁定 |
| 5 | 客人等待，顯示 Ask／Deny／Allow 與原因 | Policy（確定性） | BBS+ derived proof＋鏈下規則 → `POLICY_DECIDED`（Deny＞Ask＞Allow） | 裁決、原因代碼、policy 版本、憑證驗證結果 | Deny：顯示原因、離店，不開核准、不預留、不簽 | Ask → 6；Allow → policy grant 直送 Signer（受 `allow_max`）→ 7 | Policy 已驗收；**BBS+ 驗證未實作**（用 fixture 條件） |
| 6 | Approval Panel：摘要 → WebAuthn → 一次性語音挑戰 | Approval 服務（獨立） | approver 權限＋摘要確認＋assertion（challenge 綁 `approval_session_id, intent_hash, amount, asset, pay_to, network, nonce, expiry, policy_version`）＋語音三態 → **human grant**（`intent_hash, decision, method, issuer, audience=signer, iat, exp, jti`），直送 Signer | 核准子事件序列（PRD §5）、grant 的非敏感驗證結果 | 拒絕／逾時：Denied，不預留；語音失敗不得降級；不可用時明標降級 | grant 已簽且未過期 | 未實作（S1 Ready 未領取） |
| 7 | 「額度預留中」→ 已預留 | Prover＋預算合約 | private：`balance_old, r_old, r_new`；public：`task_id, C_old, amount, C_new, N, intent_hash` → Groth16 proof → **tx#1**：驗 proof、`commitment[task_id]==C_old`、nullifier 未用、原子寫 `C_new` 並 emit `intent_hash`（計畫 L175–182） | proof 驗證結果、tx#1 receipt／事件、explorer | 證明產不出（額度不足）或鏈上拒絕：停在預留，不簽 | tx#1 確認且事件中的 `intent_hash` 相符 | 未實作（S3 Ready 未領取） |
| 8 | 「簽署中」→ 已簽 | 獨立 Signer（唯一持 USDC 簽名鑰） | 兩件都要：grant＋鏈上 `intent_hash`；核對 consumer／wallet 歸屬、`from`、`payment_requirement_hash`、`allow_max`；簽前重抓報價 → EIP-3009 authorization，回傳給買方 | 簽章事件（不含金鑰）、核對結果 | 任一不符 fail closed；報價變動 → 重新 Ask | 簽章已產生 | 未實作；帳戶 ADR Proposed |
| 9 | 「結算中」 | 買方重送 `PAYMENT-SIGNATURE` → 商店 `/verify` → `/settle`（facilitator） | signed payload → **tx#2**（test USDC `transferWithAuthorization`） | tx#2 receipt、`PAYMENT-RESPONSE` | verify 失敗：`PAYMENT_VERIFICATION_FAILED`、不 settle；結果不明：`SETTLEMENT_PENDING`／`NeedsReview`，**不重付** | 獨立對帳確認 | 未實作（商店只有 402） |
| 10 | 已付款、取貨 | Reconciler（以 RPC 鏈上事件）→ 商店交付 | 鏈上 Transfer（付款來源＝消費者錢包，不是 facilitator sender）→ `Settled` → `RESOURCE_DELIVERED`、`RECEIPT_ISSUED` | 對帳結果、交付紀錄 | 未確認不得演取貨 | 交付已記錄 | 未實作 |
| 11 | 客人完成摘要、離店 | 模型使用取得的資料 | 交付內容 → 任務結果 | 任務結果紀錄（白名單）、模型用量（CLI 回報，否則 unknown） | 交付失敗：無摘要，標未完成 | — | 未實作（M1 禁止交付與基於未交付資料的摘要） |
| 12 | 稽核帳攤開 | 只讀 checker（`cmd/verify-demo`） | 事前規格＋原始事件＋鏈上 receipt → 逐 claim PASS／FAIL／INCONCLUSIVE | 證據包與 manifest | 缺證據：INCONCLUSIVE，不標綠 | — | 未實作（B1-07 Waiting） |

### 3.2 同一筆的關聯方式

- 共同索引是 `intent_hash`（PRD §5「跨所有證據的共同索引」）；`task_id`、`purchase_order_id`、`consumer_id`、`wallet_id` 一起出現在 canonical intent 中。
- 憑證帶 `task_id` 與 `C₀`；proof 的 public input 帶 `task_id, C_old, intent_hash`；grant 帶 `intent_hash, audience=signer, jti`；tx#1 emit `intent_hash`；Signer 簽的 EIP-3009 authorization 與 audit event 記同一個 `intent_hash`，任一不符 fail closed（計畫 L307）；tx#1、tx#2 以 `intent_hash` 關聯，**不是 atomic**（計畫 L563）。
- 畫面只沿這些 ID 連結；**不以時間相近推定同一筆**。缺關聯時標「未驗證」。
- 現有 `apa.m1.v2` 是 fixture 契約，`intent_hash` 的 domain 標明 verifier 未部署；正式付款契約另行命名、升版（`docs/adr/payment-account-model.md` 契約審查一節）。

### 3.3 必須保留的其他路徑（同一間商店，不另造流程）

| 路徑 | 依據 | 要看到的結果 |
|---|---|---|
| Allow | PRD §4.2、FR-7 | policy grant 直送 Signer，不叫人；超過 Signer 的 `allow_max` 拒簽 |
| Hard Deny | PRD §4.3、計畫 L320 | 不開核准、不簽、不預留、不付款；稽核帳照樣多一筆；人不能覆蓋 |
| prompt injection | PRD §4.4、FR-21 | 依實測顯示模型是否被騙；授權層 Deny；未授權新增支出對帳為 0 |
| 並行花費 | PRD §4.4、驗收 16 | 同 task 第二筆在鏈上預留被拒 |
| 跨消費者冒用 | PRD §4.4、驗收 44 | A 的 grant／proof／錢包不能付 B 的單 |
| ZK 反例 | 計畫 L84；驗收 10、11、15、16、27 | 錯 witness、超額、竄改 public input、stale commitment、重播都產不出可接受的 proof 或被鏈上拒絕 |
| 核准重播 | 驗收 20、29、36 | 舊 assertion／grant／語音不能用於另一筆 |
| verify 失敗、pending | PRD §4.5、驗收 25、31 | 不 settle、不交付、不重付；對帳收斂 |

**開發模式**：真實技術未接妥時，可保留明標 fake 的模式繼續開發畫面；fallback、fixture 與 replay 一律標示，**不算 live 成功**（PRD §8）。

## 4 依賴順序

### 4.1 五條線與會合點

沿用計畫的週次順序（W1–W13；「進下一週的條件是該週里程碑驗收綠，不是日曆」，計畫 L687）與任務清單的 B1／S 編號。**S 是 Spike，不是階段；M1 是里程碑；W 是排程週次**（計畫 L645）。

```text
真實 AI   ： L1 B1-04 live ─→ L2 B1-06 live trace＋tests/m1 ─→ L3 B1-07 checker ─→ M1 收尾
                    └────────────→ AE1 AI 評測 FR-21／22（付款對帳部分等 P2）；D6 工具呼叫觀測
授權      ： A1 S1 WebAuthn spike ─┐
                                  ├→ P1 keystore＋獨立 Signer＋Approval／grant 骨架（W2）
付款      ： P0 帳戶 ADR 決定 ───────┘        │
                                            ├→ A2 WebAuthn 接入 Approval／grant ─→ V1 語音三態與評測（W7–8）
憑證／ZK  ： C1 S2 BBS+ spike ─→ C2 正式發證／出示／驗證接入 Policy（W2–W3）
           Z1 S3 電路 v1 ─→ Z2 電路 v2＋setup＋合約（W9–10，本機鏈）
                                            ↓
                    P2 x402＋testnet 真簽名、結算、對帳、交付（W11）
                                            ↓
整合      ： E1-a 可開始整合（L2、P1、Z2、P2）─→ E1-b 最終驗收（再加 C2、A2、V1、AE1、checker 擴充）─→ 展示收尾（W13）
多 Agent  ： M-A 範圍修訂 ─→ M-B 多 task 重疊（fake）─→ M-C 兩位真實 Agent ─→ M-D 付款與營運聚合（隨 E1）
```

- **M1 不阻擋研究**：S1–S3 本來就是與 M1 同批的高風險驗證，未通過不妨礙 M1 展示，但會阻塞第二批相應模組（任務清單 L34）。研究可以先做；**整合與真實執行必須等前置驗收**。
- **付款線的硬前提**：正式付款模組（wallet、Signer、execution adapter、settlement）開工前，必須有 Aaron 核准的「採用候選」或「保留 EOA＋EIP-3009 基線」ADR（計畫 L671）。
- **研究不等於整合**：Z1、C1、A1 是 spike；憑證、WebAuthn、語音、AI 評測各有自己的整合切片（C2、A2、V1、AE1），都是最終驗收的必要條件（§7）。
- **D 系列**是展示切片，不能代替 AI、ZK 或付款任務；它在真實資料出現後才有實質內容可呈現（§5.5）。

### 4.2 原編號／原驗收 → 缺口 → 建議工作

| 原編號／原驗收 | 現況 | 缺口 | 建議工作（§5 切片，皆為提案） |
|---|---|---|---|
| B1-04（完成門檻含 live CLI → 402 → 結構化請購 trace，任務清單 L67） | fake、階段 A 已驗收 | 閘門因 plugins／agents／認證停下；階段 B、C 未執行 | **L1** |
| B1-06（opt-in live trace、`tests/m1/`，任務清單 L69） | fake 展示各切片已驗收 | live trace 未收錄；`tests/m1/` 不存在 | **L2** |
| B1-07（只讀 checker、PASS／FAIL／INCONCLUSIVE） | Waiting | 未實作 | **L3** |
| M1 完成門檻（B1-00～07 全部通過，任務清單 L34） | 00、01、02、03、05 Done | 04、06、07 | L1 → L2 → L3 |
| S1 WebAuthn（Ready） | 未領取 | 實機註冊、assertion、錯 challenge | **A1**（spike） |
| FR-8；驗收 20、36（WebAuthn 部分） | 未實作 | WebAuthn 接入 Approval／grant | **A2**（必要） |
| S2 BBS+（Ready） | 未領取 | 選庫、derived proof 正反測試 | **C1** |
| S3 gnark（Ready） | 未領取 | 真 proof 與 witness／public input 負向測試 | **Z1** |
| S4-A（契約影響 Accepted；ADR Proposed） | 研究順序已接受 | 採用或保留基線的決定 | **P0**（文件決定） |
| S4-B（Waiting；不阻塞 M1） | 未開始 | 候選 PoC | 只有在 P0 決定「採用候選」時才需要；保留基線則不阻擋 P1／P2 |
| W2 keystore 與分離服務；驗收 6、9、33 | 未實作 | Signer、Approval、grant 鑰隔離與服務間認證 | **P1** |
| W2「憑證存進 keystore；CLI 出示」、W3 verifier 與選擇性揭露；FR-1–3；驗收 1–3、12、26、28、29、35 | 未實作 | 發證、出示、驗證、撤銷，接入 Policy | **C2**（必要；C1 之後） |
| W4、W9、W10 電路 v1／v2、MPC、有狀態合約；驗收 10、11、15、16、17、27 | 未實作 | 全部 | Z1 → **Z2** |
| W7–8 語音核准；FR-9；驗收 20、36；PRD §7 三態 | 未實作 | 全部 | **V1**（必要；A2 之後） |
| W11 x402＋testnet 真簽名；驗收 7、24、25、30、31、39、44 | 只有 402 | Signer 簽章、verify／settle、兩筆 tx、對帳、交付 | **P2** |
| W12 E2E、恢復、對帳、UI 骨架；W13 素材、37–40、公開 | 展示骨架先做了（fake） | 端到端真實證據與三區呈現 proof／grant／tx | **E1-a**（部分整合）→ **E1-b**（最終驗收） |
| FR-21、22（AI 評測）；驗收 18、19、34 | 未實作 | 評測集與報告 | **AE1**（必要；L2 之後，付款對帳部分等 P2） |
| 一次一個 active task、Host 同時一場（PRD §3.2、§5；計畫 L444；3.2 設計） | 刻意的單場基線 | 多 Agent 體驗與基線不同 | **M-A** 範圍修訂 → M-B、M-C、M-D（§6.8） |
| `R2 order`（`docs/demo-operation-design.md` 16.1 的回放時序檢查） | 高負載下偶發失敗 | 前置條件受前面步驟耗時影響 | **T1**（獨立測試維護） |
| D3–D6 | 未開始 | — | §5.5 |

### 4.3 研究可以先做 vs. 必須等待

| 可以先做（本機、無外部執行） | 必須等待 |
|---|---|
| C1 選庫 | **C2** 接入 Policy：等 C1 結論與契約 review |
| A1 實機 | **A2** 接入 Approval／grant：等 A1 與 P1；**V1**：等 A2 |
| AE1 評測設計 | AE1 含付款對帳的部分：等 P2 |
| E1-a 的投影與 checker 設計 | **E1-b** 最終驗收：等 C2、A2、V1、AE1 全部通過 |
| Z1 電路 v1 與負向測試（gnark 本機） | Z2 合約部署到 testnet：等 P0、測試網權限與資金 |
| C1 BBS+ 選庫與正反測試 | 發證接入 Policy：等 C1 結論與契約升版 review |
| A1 WebAuthn 實機（本機 localhost） | Approval／grant 接 Signer：等 P1 |
| P0 ADR 決定（文件） | P1、P2 正式付款模組：等 P0 |
| L1-a 停用內建 agents 的環境變數與 fake 驗證 | L1 的真實觀測與階段 C：等 Aaron 各自核准 |
| T1 測試維護 | E1：等 L2、P2、Z2、P1 |

## 5 建議切片

以下切片都是**提案**，沿用既有任務編號；括號內是對應的原任務。沒有一項是 Ready 或已領取。每片欄位：①目標與不做 ②前置（研究可先做／整合須等待） ③影響模組與契約 review ④成功與反向驗收、觀眾可見增量、證據位置 ⑤新依賴與外部資源 ⑥核准種類與停止點。

### 5.1 真實 AI 線

**L1 B1-04 live 解阻與一次真實請購 trace**（B1-04）——完整派工草案見 §8。

**L2 live trace 收錄與 `tests/m1/`**（B1-06）
1. 目標：把 L1 的 live run 以唯讀方式收進 demo（Dashboard、Inspector、Timeline、Unity 依同一份投影呈現，標 live）；建立 `tests/m1/` 的 M1 案例執行與證據。不做：付款、核准、重跑 live 來湊證據。
2. 前置：L1 通過 review。`tests/m1/` 的 fake 部分可先做。
3. 影響：`cmd/demo`（開啟 live DB 的唯讀方式）、`tests/m1/`；不改契約。
4. 驗收：live 事件標 `evidence_mode=live`，fixture 部分（授權條件、匯率）分開標；切換 live／fake 不混資料；`tests/m1/` 案例對 `cases.json`。觀眾增量：**第一次在商店裡看到真實模型提出的請購**。證據：`artifacts/b1-06/`。
5. 依賴：無新依賴；不需再執行模型（重用 L1 的紀錄）。
6. 設計核准即可；不得為此重跑 live（重跑另需核准）。停止點：收錄完成交 review。

**L3 B1-07 只讀 checker（M1 子集）**（B1-07）
1. 目標：`cmd/verify-demo`、`internal/verification/`、`make verify-m1 RUN_DIR=...`（任務清單 L223），逐 claim PASS／FAIL／INCONCLUSIVE；M1 不評估的能力列 `not_evaluated`。不做：付款、ZK 的核對（後續模組補）。
2. 前置：B1-06 產出執行證據（L2）。設計可先做。
3. 影響：新增 checker；**不能呼叫被測的 Policy 函式**（計畫 L87–89）；讀取 `cases.json`、run manifest。
4. 驗收：正例 PASS；缺證據、symlink 逃逸、digest 不符、提前 Approved／Settled 為 FAIL 或 INCONCLUSIVE。證據：`artifacts/b1-07/`。
5. 無新依賴。
6. 設計核准。停止點：checker 對 L2 證據的結果交 review，**M1 結案由 Aaron 決定**。

**AE1 AI 評測**（原任務：FR-21、FR-22；驗收 18、19、34；PRD §13 的正常任務成功率、false Ask／Deny、人工介入率門檻）——**最終商業展示驗收的必要條件**
1. 目標：prompt injection 對抗評測（對帳驗未授權新增支出為 0；模型被騙率與 crafted-input 分列）；一次固定語料、兩個模型的可重現比較；正常任務成功率等指標依事前門檻。不做：自動 multi-model release gate（已移出第一期）。
2. 前置：L2（live 路徑可用）；未授權支出對帳要等 P2。**評測設計可先做；含付款對帳的部分必須等 P2**。
3. 影響：評測腳本與報告；不改 Policy。
4. 驗收出口：上列驗收編號；報告保留全部嘗試。
5. 模型用量：多次 live run，逐 run 記錄 CLI 可提供的用量，無法取得則 unknown；每批執行另行核准。
6. 設計核准＋各批 live 執行核准。停止點：評測報告交 review。

### 5.2 授權、憑證與 ZK 線

**Z1 gnark 額度電路 v1 與負向測試**（S3，計畫 W4）
1. 目標：本機 gnark Groth16（BN254）電路 v1「額度 ≥ 金額＋承諾開啟」（計畫 L700），產生並驗證真 proof；錯 witness、超額、竄改 public input 的負向測試；量測 proving／verify 時間；提出 `intent_hash` 進 public input 的映射（拆 limb 或 field hash）。不做：合約、testnet、與 Policy 整合。
2. 前置：無；研究可先做。
3. 影響：新增 `spikes/zk/`（不進產品路徑）；`intent_hash` 映射會影響**正式付款契約**，結論須另行 review。
4. 驗收：正例 proof 通過、每個反例都產不出可接受的 proof；「自寫 verifier 接受」不等於電路規格完整（計畫 L84）。觀眾增量：無（研究）。證據：`artifacts/s3/`。
5. 新依賴：gnark（Go module，版本與授權記錄）；無外部服務、無金鑰。
6. 設計核准（新增依賴）。停止點：spike 報告與結論交 review。

**C1 BBS+ 選庫與正反測試**（S2，計畫 W1、W3）
1. 目標：選定 Go 可用的 BBS（W3C Data Integrity BBS cryptosuite）實作，驗發證、derived proof、選擇性揭露、錯簽章與竄改的負向測試。不做：接入 Policy。
2. 前置：無；研究可先做。
3. 影響：`spikes/bbs/`（任務清單 S2）；結論影響正式授權條件格式。
4. 驗收：可重跑的正反測試與選型結論（固定版本與授權）。若 4 小時內選不到可用庫，計畫的停損是改 SD-JWT VC（L637）——**這會改變必要能力，須 Aaron 核准，不能自行改用**。證據：`artifacts/s2/`。
5. 新依賴：BBS 函式庫（授權須確認）。
6. 設計核准。停止點：選型結論交 review。**C1 通過只代表選型與 spike 成立，不代表憑證已參與准駁**（那是 C2）。

**C2 正式 BBS 發證、出示、驗證接入 Policy**（原任務：計畫 W2「憑證存進本地 keystore；CLI 出示」、W3「verifier 與選擇性揭露」；FR-1–3；驗收 1、2、3、12、26、28、29、35）
1. 目標：issuer 依 task 發 BBS+ 憑證（`task_id, C₀, expiry, holder binding`）存入 keystore；請購時出示 derived proof；Policy 以驗證結果參與裁決（取代 M1 的 fixture 授權條件）；撤銷、過期、holder binding、跨 verifier 與舊 challenge 重播都被拒。fixture 憑證路徑只保留為明標 fake 的開發模式。不做：eKYC 核發閘門（第二期）。
2. 前置：C1 結論；正式授權條件格式與契約升版 review。研究可先做；**接入 Policy 必須等 C1 與契約 review**。
3. 影響：Policy 輸入、Store 的授權條件快照、正式授權契約——**契約須 review**；既有 M1 語意（例如缺設定得 `CONFIG_MISSING`）若要改，另行設計。
4. 驗收出口：上列驗收編號逐項；Policy 的裁決紀錄可追到該次出示的驗證結果。證據：`artifacts/c2/`（暫定）。
5. 依 C1 的函式庫；revocation 服務的部署方式待設計。
6. 設計與契約核准。停止點：接入後的正反測試交 review。

**A1 WebAuthn 實機驗證**（S1）
1. 目標：本機 `http://localhost:8123`、`rp_id=localhost` 的註冊與 assertion；challenge 綁 `approval_session_id、intent_hash、amount、asset、pay_to、network、nonce、expiry、policy_version`；錯 challenge、跨付款重播被拒。不做：grant、Signer。
2. 前置：無；研究可先做。實機需要 Aaron 操作 authenticator。
3. 影響：`spikes/webauthn/`；不改契約。
4. 驗收：註冊／assertion／錯 challenge 的結果；authenticator 依實測決定（手機 cross-device 或外接 FIDO2 → Touch ID → 明標的 virtual fixture，PRD §5.1）。
5. 新依賴：WebAuthn 函式庫；**可能需要外接 FIDO2 key（採購由 Aaron 決定）**。
6. 設計核准；實機操作由 Aaron 執行。停止點：實測結論交 review。**A1 只是 spike；WebAuthn 正式接線是 A2。**

**A2 WebAuthn 接入 Approval／grant**（原任務：S1 之後的 Approval 模組，計畫 W2 獨立服務骨架之上；FR-8；驗收 20、36）
1. 目標：Ask 時 Approval Panel 依序「approver 權限 → 摘要確認 → WebAuthn assertion（challenge 綁同一 `intent_hash` 與核准 session）」，通過後才發 human grant 直送 Signer；核准子事件依 PRD §5 逐筆出現。不做：語音（V1）。
2. 前置：A1 結論、P1（Approval 與 Signer 的獨立服務與 grant 格式）。**必須等 A1 與 P1**。
3. 影響：Approval 服務、grant、事件類型（M1 契約禁止 `APPROVED`、`GRANT_ISSUED`，正式契約須升版 review）。
4. 驗收出口：驗收 20、36 中與 WebAuthn 相關的部分；錯 challenge、跨付款重播、撤銷裝置、只有複製的 `device_id` 都被拒。
5. 依 A1 的 authenticator 結論。
6. 設計與契約核准。停止點：接線後的正反測試交 review。
**Z2 電路 v2、phase-2 setup 與有狀態預算合約（本機鏈）**（計畫 W9、W10）
1. 目標：v2（`C = H(balance, r)`、守恆、`balance_new ≥ 0`、nullifier `N = H(r_old, task_id, domain)`，public `task_id, C_old, amount, C_new, N, intent_hash`）；三位自己的 phase-2 貢獻者（README 照寫）；Solidity verifier＋`registerTask`＋預算合約（驗 proof、`commitment[task_id]==C_old`、nullifier 未用、原子寫入並 emit `intent_hash`），先在**本機鏈**驗。不做：testnet 部署。
2. 前置：Z1 結論；`intent_hash` 映射 review。
3. 影響：新合約與 prover 服務；正式付款契約。
4. 驗收：驗收 10、11、15、16、17、27——錯 witness、超額、竄改 public input、stale commitment、重播、繞過政策層直接送都被拒。
5. 新依賴：合約工具鏈與本機鏈（例如 Foundry／anvil，選型待核准）。
6. 設計核准（新依賴）。testnet 部署併入 P2 的執行核准。停止點：本機鏈驗證交 review。停損（退回 v1＋政策層明文累計）屬改變必要能力，須 Aaron 核准。

**V1 語音三態與既定評測**（原任務：計畫 W7–8；FR-9；驗收 20、36；PRD §7）——**最終商業展示驗收的必要條件，不是依進度可略過的項目**
1. 目標：一次性語音挑戰綁同一 `intent_hash`；三態依 PRD §7：通過才記通過（`amr=["webauthn","voice"]`）；失敗（唸錯、重放、他人、判定合成）不發 grant、進 Denied，不得改走降級；不可用（服務掛、逾時）時，摘要確認＋WebAuthn 仍可核准並明標降級（`voice=unavailable`）。基本 FAR／FRR、replay 與降級率依計畫評測並揭露。
2. 前置：A2。**必須等 A2**。
3. 影響：Approval 服務、語音 sidecar、事件。
4. 驗收出口：三態各自的案例與評測報告。**不是要求每次語音都成功；但「從未實作語音」不能當成已驗證的 unavailable 降級**——降級路徑本身要實作並驗證。
5. 新依賴：預訓練 ASR／聲紋模型與資料集；樣本同意、保存與刪除規則；開工前另核准模型與資料。
6. 設計核准＋模型與資料核准。停止點：三態與評測交 review。停損（砍抗 spoof）屬改變必要能力，須 Aaron 核准。

### 5.3 付款線

**P0 付款帳戶 ADR 決定**（S4-A）
1. 目標：Aaron 決定「保留 EOA＋EIP-3009 基線」或「採用候選（需 S4-B 證據）」，ADR 由 Proposed 轉為 Accepted。不做：PoC、部署。
2. 前置：無（ADR 第三版已在 repo）。
3. 影響：`docs/adr/payment-account-model.md`；保留基線時 README 與說法不得宣稱鏈上錢包政策保障（ADR「後果」）。
4. 驗收：ADR 狀態與理由、信任假設明載。
5. 無。
6. Aaron 決定（§9.2）。停止點：ADR 更新交 review。

**P1 keystore、獨立 Signer、Approval／grant 骨架（不上鏈；骨架不等於核准接線完成）**（計畫 W2；驗收 6、9、33）
1. 目標：Signer 與 Approval 成為獨立行程，agent／wrapper 讀不到簽章鑰；grant 格式（`intent_hash, decision, method, issuer, audience=signer, iat, exp, jti`）直送 Signer；Signer 檢查 grant、`allow_max`、錢包歸屬，**本機測試金鑰、不上鏈**。不做：真資金、testnet。
2. 前置：P0。P1 只建 Approval／grant 的骨架與 Signer 隔離；**WebAuthn 正式接線屬 A2**，不在 P1 完成條件內。
3. 影響：新服務、正式付款契約（另行命名，不沿用 M1 fixture digest）——**契約須 review**。
4. 驗收：agent 行程環境與檔案系統找不到金鑰（實測 OS 權限）；重播、過期、跨消費者 grant 被拒；Deny 不發 grant。
5. 依賴：本機測試金鑰（不注資）。
6. 設計與契約核准。停止點：隔離與負向測試交 review。

**P2 x402＋Base Sepolia 真簽名、兩筆 tx、對帳、交付**（計畫 W11；驗收 7、24、25、30、31、39、44）
1. 目標：Z2 合約部署到 Base Sepolia；tx#1 預留、Signer 簽 EIP-3009、買方重送 `PAYMENT-SIGNATURE`、商店 verify／settle（tx#2）、reconciler 以鏈上事件對帳、確認後交付；A、B 各自錢包。外部相容性：test402.com 一筆真 402（PRD §8）。不做：mainnet、真錢。
2. 前置：P0、P1、Z2；testnet 權限。
3. 影響：商店交付路徑、reconciler、正式付款契約、狀態機。
4. 驗收：兩筆 tx 各有 receipt／事件與 explorer；pending／不明不重付、不交付；verify 失敗不 settle；跨消費者冒用被拒；未授權新增支出對帳為 0。
5. 外部：RPC、facilitator（x402.org）、faucet 測試資金、A／B／M 測試錢包與私鑰保管。
6. **執行核准**（建立私鑰、索取資金、部署、送交易各自核准）。停止點：一條成功 Ask 與 Deny 的 testnet 證據交 review。

### 5.4 整合與展示線

**E1 端到端與證據呈現**（計畫 W12；驗收 37–39、41–43）——分「可開始整合」與「可完成最終驗收」兩個出口
1. 目標：同一個 `intent_hash` 串起憑證、裁決、核准、proof、tx#1、簽章、tx#2、交付與任務結果；Inspector／Timeline 顯示 proof、grant、tx、交付欄位；Unity 的付款與取貨轉場由實際狀態驅動；成功 Ask、Hard Deny、一條攻擊路徑可從乾淨狀態重跑；B1-07 checker 補付款、ZK、憑證、核准的 claim。
2. 前置：
   - **E1-a 可開始整合**：L2、P1、Z2、P2。此時只能記為「部分整合」，缺的能力在畫面與 checker 標 `not_evaluated`。
   - **E1-b 可完成最終驗收**：E1-a 加上 C2、A2、V1、AE1 全部通過，以及 L3 的 checker 擴充到這些 claim。**任一項未完成，E1 不得宣告通過，也不代表成品完成**；研究 spike（Z1、C1、A1）通過不會自動算成整合通過。
3. 影響：投影與 bridge 欄位（新增須 review）、checker。
4. 驗收：E1-a 交出部分整合的證據；E1-b 依 §7 全部條件與 WareTrack 完成條件表六列逐項。
5. 依 P2。
6. 設計核准；任何重跑 testnet 依 P2 的執行核准。

**T1 `R2 order` 測試穩定性**（測試維護，不重開 D2）：在該測試段開始時才建立專用的新 task，避免期限被前面步驟耗掉；仍須證明回放開始時只有 #3、#4 在回放期間提交、回放結束後 Unity 與 Dashboard 恢復到 #4；不得只加長等待、刪除前置條件或重跑到通過。**影響**：瀏覽器全套在高負載下的可信度——L2、L3 會依賴這套證據，所以建議排在 L2 之前。設計核准；只改測試。

### 5.5 D3–D6 怎麼服務同一主線

| D 切片 | 服務主線的哪一步 | 建議時機 |
|---|---|---|
| D6 工具呼叫觀測 | 第 3、4 步：遮罩後的真實工具呼叫是「真實 AI」證據的一部分（WareTrack 完成條件第一列） | **等 L1 有 live 資料後**，與 L2 一起或緊接著設計 |
| D4 請購當時報價的白名單欄位 | 第 3、4 步：報價與 `payment_requirement_hash` 相符 | 需 API 擴充；建議與 P2 前的正式付款契約一起設計，避免改兩次 |
| D5 Allow 路徑可示範 | §3.3 Allow：policy grant 與 `allow_max` | M1 階段只能演「政策允許，未付款」；真正的價值在 P1／P2 之後 |
| D3 點選客人 | 第 0–12 步的查證入口（從場景查到同一筆） | 可隨時做、不依賴真實資料；優先度低於真實技術線 |

多人同時購物改依 §6 的具體路線（M-A～M-D），不再只列 backlog；人臉／eKYC（第二期 W14–15）與語音下單（未排程 backlog）維持原期別。D3 點選客人在多位客人同時作用時更重要，建議與 M-B 一起設計。

### 5.6 建議順序

1. **L1**（§8）
2. T1
3. L2
4. L3 → M1 收尾（由 Aaron 決定結案）
5. M-A 多 Agent 範圍修訂提案（文件）
6. P0（文件決定，可在任何時點做；P1 前必須完成）
7. Z1
8. A1
9. C1
10. M-B（多 task 重疊，fake；時機由 Aaron 決定，可與 P1 前後對調）
11. P1
12. C2（C1 之後即可，與 P1 無依賴）
13. A2
14. M-C（兩位真實 Agent；需執行核准）
15. Z2
16. P2
17. AE1（設計可早；含付款對帳的部分在 P2 之後）
18. V1（A2 之後）
19. E1-a＋M-D（部分整合；D6 隨 L2；D4 隨正式付款契約；D5 隨 P1／P2；D3 隨 M-B）
20. E1-b 最終驗收（C2、A2、V1、AE1 全部通過後）

一次只開一條實作；研究 spike（Z1、A1、C1）若 Aaron 要並行，可在 L1 等待核准的空檔進行，但仍逐片派工、逐片 review。

## 6 多 Agent 與 Admin 營運視角

依據：計畫 L9、vault WareTrack 筆記「多 Agent 店內活動與營運查證體驗」、規劃指示第 3 節（10/7 同日補充）。Aaron 希望店內有多位顧客 Agent 同時選購、等待與消費，Admin 及時看到活動與消費累積，並從某筆消費查到鏈上與其他技術證據。**本節是提案，不是並行或付款實作的授權**；「至少兩位真實 Agent」是 Codex 建議的門檻，待 review 與 Aaron 確認。

### 6.1 四種「多人」不能互相冒充

| 層級 | 意思 | 能證明什麼 | 不能證明什麼 |
|---|---|---|---|
| 畫面多人 | Unity 同時顯示多位客人 | 呈現能力 | 背後有多個 task 或多個模型在工作 |
| 多 task 重疊 | 兩個 task 的生命週期在 Store 中重疊（可以是 fake） | Host、Store、投影能處理並存的 task | 真實模型同時在工作 |
| 真實模型工作重疊 | 兩個 live run（不同 consumer）的有效工作區間重疊，各有真實工具呼叫與時間紀錄 | 多位真實 Agent 同時運作 | 付款並行或高併發能力 |
| 付款並行 | 兩筆付款的預留／簽章／結算時間重疊 | 付款路徑的並行隔離 | 吞吐量或 production 容量 |

歷史回放與多角色動畫只屬第一層，不能當作真實並行。

### 6.2 建議的最小可驗收體驗（待確認）

- **門檻**：兩位真實 Agent，A 代表 consumer-a、B 代表 consumer-b，各有自己的授權、錢包與 task；兩個 live run 的**有效工作區間**重疊。
- **重疊要看到的證據**（路線層的定義；量測方式由 M-C 設計）：
  1. Host 能對應到兩個不同 consumer 的 run 與 task；
  2. 兩個 run 各自的真實工具呼叫紀錄（白名單、含時間）；
  3. 可核對的有效工作區間：該 run 的程序仍在 `running`，且區間內有該 run 自己的真實工具呼叫或模型輸出。兩段區間有交集才算重疊。
- **不能當作重疊證據**：只有 Ask 期限重疊（Ask 未決時程序可能早已結束）；閒置的程序；動畫；回放。也不宣稱能觀測模型供應商內部是否同時推論。
- **付款不必並行**：兩筆付款是否也要重疊，另行設計；流程重疊不宣稱高併發支付。
- **腳本**（只有在下列條件都成立時，才算通過 M-C）：
  1. 開始 A；A 開始真實工作（有工具呼叫）。
  2. 在 A **仍有真實工作可進行**時開始 B；兩者的有效工作區間依上面的定義重疊。M-C 的正式設計須提出可控制的工作或同步條件，確認兩場都還有真實工作；本路線文件不先新增睡眠、卡住工具或新的執行機制。
  3. **停止隔離案例**：先確認 A 的程序仍可停止、B 仍有工作，才停止 A；B 之後仍可完成自己的結果。A 已結束時，不能對不存在的活躍程序演示「停止 A」。
  4. 兩人的結果分開呈現；Admin 總覽與各自明細一致。
- **只達到 task 重疊時**：若 A 在 B 開始前已結束（例如 A 已提交請購並得到 Ask，程序隨即結束），這次只能回報「多 task 重疊」（§6.1 第二層），**不得通過 M-C**。
- **成本與核准**：兩個 live run 可能增加用量；逐 run 記錄 CLI 可提供的用量，無法取得則記 unknown。兩個 run 不代表精確兩倍成本，也不代表只有兩次模型請求。兩個 CLI 同時執行可能遇到限流。每次 live 執行都須另行核准；本輪不實跑。

### 6.3 與現行基線的範圍差異（須修訂才能成立，不能靜默覆寫）

| 現行規則 | 出處 | 要改成什麼（提案） | 影響的驗收 |
|---|---|---|---|
| 第一期兩個消費者 profile「依序操作、一次一個 active task」 | PRD §3.2（L102）、§5（L188）；計畫 L444；任務清單 L32 | 改為「至少兩位不同 consumer 的 task 可重疊；同一 consumer 一次仍只一個 active task」 | 驗收 37（三區畫面）、44（A／B 隔離）需補並行案例；PRD §13「三條路徑可重跑」不變 |
| Host 同時只跑一場（新場次遇到 running／stopping 回 `409 run_active`） | `docs/demo-operation-design.md` §4（L103）、§13；`cmd/demo/control.go` L197–199 | 每位 consumer 最多一場，總數有上限（建議 2）；停止只作用於指定場次 | 3.2 的場次協調驗收需擴充 |
| 網頁一次只看一位 viewer；Unity 每個世代只顯示一個 task | `web/app.js` 開頭註解；`Projection.cs` L156（one task per generation） | Admin 視角的投影改為「每位客人一份狀態」，各自的世代與序號；viewer 視角維持只看自己 | D1／D2 的投影規則與 bridge 需升版並 review |
| 角色：viewer 讀自己的 consumer、host 讀全部 task、operator 只控制場次不讀內容 | `cmd/demo/api.go` L18–24、L164 | Admin 以 host 角色為基礎加上聚合讀取；**operator 不因此擴權**；viewer 隔離不變 | 3.1／3.2 的權限驗收需補聚合端點 |

### 6.4 並行影響盤點（只列要評估的事，不在此設計完整多租戶平台）

| 面向 | 現況 | 要評估與處理的事 |
|---|---|---|
| Host 場次協調 | 單一 `active`；持久化的 `host_runs.state` 只有 `accepted／interrupted／ended`（`internal/store/runs.go` L79），`running／stopping／ended` 是 Host 程序內的 `liveRun.state`（`cmd/demo/control.go` L31），API 分成 `state` 與 `live` 兩欄（L59） | 多場的配發、上限、停止與中斷恢復；不能只移除 `run_active` 判斷 |
| Store 交易與鎖 | 單一 SQLite；寫入以交易為單位（請購與裁決同一交易）、每個 task 一條 hash chain；到期 worker | 兩個 run 同時寫入的鎖與恢復驗證；到期 worker 與多 task；重啟時多個未完成場次 |
| task 配發 | 依 consumer 配發 `show-a-NN` 等 ID | 並發申請不重號；同 consumer 的第二場被拒 |
| consumer 與錢包隔離 | Store 以 session 限制查詢；fixture 錢包 | 兩個 live CLI、兩個 MCP bridge、兩組 capability 互不可用 |
| 預算、grant、nonce | 尚無（付款未實作） | 每 task 獨立預算承諾（ZK 以 `task_id` 區分）；grant `jti` 與 EIP-3009 nonce 不衝突；同一錢包的簽章序列化 |
| Signer | 尚無 | 依錢包選 key；同時兩筆時的排隊與拒絕規則 |
| 停止與重啟恢復 | 單場的停止與恢復已驗收 | 停 A 不影響 B；重啟後兩場各自收斂 |
| Unity 多角色與選取 | 兩位客人存在，但只有一位作用中 | 兩位同時作用、各自狀態與標籤；選取（D3）帶 consumer 與 task |
| CLI 程序 | 每 run 一個 CLI 程序 | 兩個同時執行的資源上限、限流與用量分列 |

### 6.5 Admin 指標（每項附單位、來源、去重鍵、時間、更新與明細入口）

共同規則：每個數字都能展開成對應清單；畫面寫出資料時間；按 **鏈別、資產、`evidence_mode`（live／fixture／replay）** 分列；replay 不計入即時統計；**Ask、報價、額度預留都不算成交；testnet 金額不是真實營收**。

**來源讀不到時**：不沿用舊數字冒充即時，也不顯示 0。可以保留最後一次觀測值，但必須同時顯示「stale／未確認」與觀測時間；總數視為未知。

| 指標 | 單位 | 權威來源 | 去重鍵 | 時間範圍 | 更新與過期 | 明細入口 |
|---|---|---|---|---|---|---|
| 活躍 run | run 數；`running` 與 `stopping`（停止中）分開列 | **Host 目前程序的可觀測狀態**（`cmd/demo/control.go` 的 `liveRun.state`，API 的 `live` 欄位）。**不是** `host_runs` 的持久化紀錄：`accepted` 不代表程序還活著，重啟後未收尾的場次是 `interrupted`，不算活躍 | `run_id` | 當下快照 | 輪詢；Host 狀態讀不到時保留最後觀測值並標 stale 與時間 | 場次清單 → task |
| 活躍 Agent | 不同 consumer／agent 的數目 | 由「活躍 run」對應到各 run 的 consumer 與 agent | `consumer_id`＋`agent_id` | 當下快照 | 同上 | 各 Agent 的 run |
| 執行中 task | task 數 | Store（依事件推導的 task 狀態，未終止） | `task_id` | 當下快照 | 讀取失敗標 stale | task 明細 |
| 等待核准 | 請購數 | Store（`POLICY_DECIDED` Ask 且未收尾）；**與程序是否還活著無關** | `purchase_order_id` | 當下快照 | 同上；期限依伺服器時間 | 請購明細 |
| Deny | 請購數 | Store（`POLICY_DECIDED` Deny） | `purchase_order_id` | 統計區間（例如本次展示起） | 增量 | 請購明細與原因 |
| 已結算數量／金額 | **唯一消費意圖**的筆數；金額依鏈別與資產分列 | reconciler 以鏈上事件確認的結算（不是 facilitator 回報） | 消費意圖（`intent_hash`）。每筆意圖須以「鏈別＋資產＋可唯一定位的鏈上結算證據」建立映射；同一意圖的重送、重複 receipt 或重複事件不增加筆數；**tx 數不等於消費筆數**，也不假設一筆 tx 只代表一筆消費。可唯一定位的欄位由正式付款契約設計決定 | 統計區間 | 對帳完成才計入 | 結算證據 → explorer |
| 已交付數量 | 消費意圖筆數 | 商店交付紀錄（`RESOURCE_DELIVERED`） | 消費意圖（對應的 `purchase_order_id`） | 統計區間 | 增量 | 交付紀錄 |
| 未知狀態 | 分兩種，不合併：①已知且可枚舉的未收斂意圖（`SETTLEMENT_PENDING`／`NeedsReview`）以意圖筆數計；②**整個來源讀不到**時，總數未知——不憑空計一筆，也不回 0，只顯示最後觀測值、時間與「不完整／過期」 | ① Store／reconciler；② 讀取狀態 | ① `intent_hash` | 當下快照 | ①持續顯示直到收斂；②恢復讀取後重算 | 對帳明細 |

重送、輪詢、回放、重啟都不得重複計數（依去重鍵）；聚合與明細來源對不上時標「未確認」。

**四個預期**（候選驗收，本輪不寫程式或測試）：

| 情況 | 預期 |
|---|---|
| 程序已結束而 Ask 仍未決 | 「活躍 run／Agent」不計這一場；「等待核准」照計這筆請購 |
| 服務重啟後，場次變成 `interrupted` | 不算活躍；明細顯示 `interrupted` 與時間，不顯示成 running |
| 整個來源讀不到（例如 Store 或 Host 狀態） | 相關指標顯示「未知／stale」與最後觀測時間，不顯示 0、不加一筆 |
| 同一筆結算被重複觀測（重送、重複 receipt 或事件） | 已結算筆數與金額不增加 |

### 6.6 證據鑽取與權限

- **路徑**：角色或聚合明細 → 請購 → canonical intent（`intent_hash`）→ 授權與 Policy 裁決 → 憑證驗證與 proof／verifier 結果 → tx#1 預留 receipt → Signer 簽章 → tx#2 結算 receipt → 商店交付 → 任務結果。每一段用明確 ID 連結，不靠時間接近推定。
- **證據邊界**：Explorer 只證明它涵蓋的鏈上事實；AI 行為（工具呼叫 trace）、人的核准（核准子事件）與交付各有自己的證據。
- **權限**（提案，待 review）：
  - Admin：建議以現有 host 角色為基礎，可讀所有 consumer 的聚合與白名單明細；是否可讀他人的完整請購明細，由 Aaron 決定（§9.2）。
  - operator：仍只控制場次，不讀 task 內容。
  - viewer：仍只讀自己的 consumer。
  - 所有視角都不顯示 token、capability、完整憑證、完整 grant、私鑰或私密 witness。

### 6.7 反向驗收（候選）

- A／B 事件交錯時，各自的 Timeline、卡片與 Unity 客人不混用。
- 停止 A（A 確實仍在 `running` 且 B 仍有工作時），B 繼續到完成；B 的計數不受影響。
- **反例：只有 task 重疊**（A 的程序已結束、只剩未決 Ask）時，不得判定為兩位真實 Agent 工作重疊。
- **反例：A 已結束**時，不得演示或記錄「停止 A」。
- 跨 consumer 的請求、grant、proof、錢包被拒並留證。
- 重送、輪詢、回放、重啟不重複計數。
- 未決交易不算成功、不計入已結算金額。
- 聚合與明細來源對不上時標「未確認」。
- 斷線或讀取失敗時，總覽標 stale，不繼續顯示即時。

### 6.8 路線位置與建議

**建議先完成單筆真實流程，再擴為兩位**，但提前保留會影響介面的設計：

- 理由：並行的難點在 Host、Store、Signer 與投影；若在付款與 ZK 都還沒實作時先做並行付款，會設計兩次。但投影與 Host 協調若一直假設單場，之後要大改畫面與 3.2 的協調。
- 因此拆成四片（皆為提案、未編號；正式派工時沿用 B 系列編號體系，不另起一套）：

| 片 | 內容 | 前置 | 核准 | 建議里程碑 |
|---|---|---|---|---|
| M-A 範圍修訂提案 | 6.3 的 PRD／計畫／任務清單修訂草案、Admin 角色與權限設計、投影升版設計 | L1 完成（有真實資料可對照） | **Aaron 確認門檻與範圍修訂**；Codex review | M1 收尾後立即 |
| M-B 多 task 重疊（fake） | Host 每 consumer 一場、上限 2；Store 並行寫入與恢復；Unity 兩位客人同時作用；Admin 總覽（只含 fake 能計的指標，標 fake） | M-A 核准 | 設計核准 | M1 之後、P1 之前或並行（由 Aaron 決定，§9.2） |
| M-C 兩位真實 Agent 重疊 | 依 §6.2 的有效工作區間重疊（Host 對應、各自真實工具呼叫、區間交集）；提出可控制的工作／同步條件的正式設計；停止隔離只在 A 仍可停止、B 仍有工作時驗；只達到 task 重疊時不通過 | L1、L2、M-B | **每次 live 執行另行核准**；用量逐 run 記錄，無法取得則 unknown | M-B 之後 |
| M-D 付款與營運聚合 | 已結算、已交付、未知狀態的聚合與鑽取；付款是否並行另行設計 | P2、E1 | 依 P2 的執行核准 | 計畫 W12（E2E）一併驗收 |

不擴充大量 NPC、複雜尋路或其他商店；人臉與 eKYC 維持第二期。

## 7 最終交付與證據條件

對照原有完整驗收（計畫 §5 驗收 1–44、PRD §9、§13），不降級為「有一個 proof／一筆 tx 就算完成」。下列每項都要有證據；缺任一項就列**未完成／未確認**，不以畫面、測試總數或其他項目通過抵銷。最後一欄的「整合位置」是該條件的工作與驗收出口；**研究 spike 通過不等於整合通過**，E1-b 必須在這些整合都通過後才能宣告。

| 條件 | 要看到的證據 | 不算數的做法 | 對應與整合位置 |
|---|---|---|---|
| 真實模型完成請購 | live run 的白名單 trace：模型的工具呼叫（遮罩）→ 402 → `request_purchase` → `PURCHASE_PROPOSED` | 只取得 CLI init；fake CLI；模型自述 | B1-04、FR-23 |
| 模型工具許可 ≠ 付款核准 | grant 只由 Policy（Allow）或人（Ask）發出，直送 Signer | CLI 的工具 allowlist、`dontAsk` 權限當成核准 | PRD §3.1、FR-7；P1 |
| 人的核准綁這一筆 | Ask 時摘要確認＋WebAuthn assertion（challenge 綁 `intent_hash` 與核准 session）通過才發 human grant；錯 challenge、跨付款重播、撤銷裝置被拒 | WebAuthn spike 通過；demo token 或按鈕代替核准 | FR-8；驗收 20、36；**A2** |
| 語音三態 | 通過、失敗（不發 grant、不得降級）、不可用（明標降級 `voice=unavailable`）三態都實作並驗證；基本 FAR／FRR、replay 與降級率評測 | 從未實作語音就當成「不可用降級」；只測成功一次 | FR-9；驗收 20、36；PRD §7；**V1** |
| AI 評測 | injection 對抗評測（未授權新增支出對帳為 0、模型被騙率與 crafted-input 分列）、兩模型比較、正常任務成功率等門檻 | 只跑一次成功的 live run | FR-21、22；驗收 18、19、34；**AE1** |
| BBS+ 真正參與准駁 | derived proof 驗證結果進入裁決；撤銷、過期、holder binding、跨 verifier 與舊 challenge 重播的反例 | fixture 授權條件、hash、「verified」標籤；BBS spike 通過 | FR-1–3；驗收 1–3、12、26、28、29、35；**C2** |
| gnark 真正參與付款 | 證明命題：存在 `balance_old, r_old, r_new` 使 `C_old` 開得起、守恆、`balance_new ≥ 0`、`C_new` 正確、`N` 正確；public input `task_id, C_old, amount, C_new, N, intent_hash`；不揭露 `balance`、`r`；信任根是 issuer 的 `registerTask` 簽章與 verifier 合約（phase-2 貢獻者都是自己，照寫）；錯 witness、超額、竄改 public input、stale commitment、重播的反例 | 與消費無關的玩具 proof；只有本機 verifier 接受；電路 spike 通過 | FR-10–12；驗收 10、11、15、16、17、27；**Z2**＋**P2** |
| 鏈上預留與結算 | tx#1（預留）與 tx#2（結算）各有 RPC receipt、chain ID、contract、tx hash、explorer，明標「不是同一筆」；提交、pending、結算確認、商品交付分開顯示 | 只有一筆 tx；把 submit 當 settled；explorer 未索引就判失敗 | G1、FR-13–15、19；驗收 7、25、39 |
| 未知結果 | `SETTLEMENT_PENDING`／`NeedsReview`，不顯示成功、不自動重付 | 逾時當失敗再買一次 | 驗收 25、31 |
| 同一付款意圖 | 憑證、裁決、核准、proof、tx#1、簽章、tx#2、交付都對到同一個 `intent_hash` | 以時間相近推定 | FR-4；驗收 24、30 |
| A／B 不混用 | 身分、錢包、資料、grant、proof 各自獨立；冒用被拒 | 兩個 profile 共用錢包或預算 | FR-25；驗收 44 |
| 未授權新增支出為 0 | 事前限定帳戶／資產／區塊範圍的對帳；未決或缺證據為 INCONCLUSIVE | 只看 UI 或 facilitator 回報 | G4；PRD §13 |
| 展示可操作 | 明確入口啟動、重跑、恢復；從 Unity 場景查到同一筆的證據；一般展示不需改設定、重建或跑測試 | 每次展示前重建或手動改資料 | FR-17、23；WareTrack 完成條件 |
| 模式誠實 | 每個證據帶 `evidence_mode=live｜fixture｜replay`、`source`、`observed_at`；replay 全程明標 | fallback 或 replay 算 live 成功 | FR-17；PRD §8 |
| 證據安全 | 只保存白名單或遮罩後資料；不收原始 CLI 輸出、token、私鑰、完整 grant、私密 witness、生物特徵 | 為方便除錯保存原始輸出 | FR-18；驗收 38 |
| 多 Agent 與營運（候選，待確認） | 兩位真實 Agent 的有效工作區間重疊（§6.2 的定義）、各自真實工具呼叫；A 仍在執行時停止 A，B 仍完成；Admin 聚合可展開成唯一明細，去重、stale 與未知規則成立；從聚合追到單筆的 `intent_hash` 證據鏈 | 多角色動畫、歷史回放、只靠 Ask 期限重疊的 task 重疊、只移除單場 guard；把流程重疊說成高併發支付 | §6；與單 active task 基線的差異待 Aaron 確認，**目前不是已核准的要求** |
| 獨立核對 | 只讀 checker 逐 claim PASS／FAIL／INCONCLUSIVE；必要 claim 只有 PASS 才通過 | 自報成功；只看測試總數 | FR-24；驗收 41–43 |

## 8 下一批建議：L1 派工草案

**名稱**：L1　B1-04 live 解阻與一次真實請購 trace（沿用 B1-04 卡；不新增任務編號）

> **進度（10/7）**：Aaron 核准 L1-a；已在分支 `b1-04-live` 實作（`79a4a5f`、測試修正 `418f0de`；`docs/cli-poc.md` 第 35 節）；**10/8 於 `418f0de` 通過 Codex 限定範圍技術 review，L1-a 關閉**（第 36 節），只證明變數傳遞與 fake 回歸，不證明真實 CLI 的 agents 已清空。L1-b～L1-e 未核准、未開始。
>
> **進度（10/8，L1-b）**：plugin 來源調查依 Aaron 決定暫緩，來源未確認保留為已知限制。正式 Gate 維持不變。下一步為整理一次診斷型 C0′ 清單，確認目前 plugins／agents 是否仍非空；待 Codex review 與 Aaron 另行核准，尚未執行（`docs/cli-poc.md` 第 39.7 節）。L1-b 不標通過或 Done。**診斷型 C0′ 清單於 `d12adcc` 通過限定文件 review，R1／R2 關閉**（Codex 靜態核對，不是測試通過；第 39.10 節）；真實執行仍待 Aaron 另行核准。plugin 觀測以 APA wrapper 當次 runtime 的 init 為準，pixel-office 名稱僅為顯示候選，不作為允許依據。
>
> **進度（10/8，診斷型 C0′）**：核准版本改為 `2.1.293`（`bd29a4e`）後執行一次，取得 init：**plugins 非空（2 個）**、agents 明確為空。依結果分支記錄執行阻擋並停止；最小隔離或停用方案待另提與核准（`docs/cli-poc.md` 第 41 節）。**Codex 核對完成（第 41.1 節）**：agents 結果可作為 L1-c 對應觀測項目的證據；L1-b、L1-c 整體不標通過；正式請購阻擋仍在。下一步建議為 plugins 最小隔離或停用方案的設計提案，待 Aaron 核准派工。
>
> **進度（10/8，plugins 方案）**：plugins 方案於 `14eb7b5` 通過限定設計複核，R1、R2 關閉（`docs/cli-poc.md` 第 42 節，42.9）。通過範圍是受控實驗的設計，不代表 plugins 已停用或正式請購可放行。等待 Aaron 核准方案 A 的實作、fake 測試與一次完整斷網容器驗證；真實診斷須在實作 review 通過後另行核准。方案 A 在 APA 子程序設專用暫存 plugin 根目錄（`CLAUDE_CODE_PLUGIN_CACHE_DIR`）；它改的是儲存根目錄，不是停用開關，可能在背景被重新填入，也不涵蓋 skills-dir 或 managed 強制啟用的來源。結果非空只代表未達目標，不推論來源；未達目標時，須 Aaron 在「提供完整 id／調整受管設定／以 `--safe-mode` 只驗 MCP」中擇一。
>
> **進度（10/8，方案 A 實作）**：已實作（`6caba93`；測試補正 `086f640`、`40bd263`），本機 fake 測試通過。核准的一次完整斷網容器驗證（`086f640`）因測試在 Linux 上的權限讀法失敗；已修正，只改測試。經 Aaron 核准補跑：`40bd263` 完整斷網容器驗證通過（基準線 PASS、255／255 突變、隔離 17 項；待 Codex 核對；`docs/cli-poc.md` 第 43.5 節）。plugins 是否真的停用仍未驗證。
>
> **進度（10/8，方案 A 驗收）**：方案 A 於 `40bd263` 通過限定範圍技術 review，關閉這項實作與 fake 驗證。只證明 APA 子程序的專用 plugin 根目錄設定、失敗處理與 fake 回歸符合要求，不代表真實 plugins 已停用，也不代表正式請購可放行。下一次診斷型 C0′ 的執行確認清單已整理（`docs/cli-poc.md` 第 44 節）。`d46ca0b` 通過 Codex 限定文件複核（Codex 只做文件差異與 Git 唯讀核對）；方案 A 的實作驗收基準為 `40bd263`；下一次診斷型 C0′ 清單已通過文件複核，尚待 Aaron 另行核准執行（第 44.7 節）。
>
> **進度（10/8，方案 A 後診斷）**：依 Aaron 核准執行一次診斷型 C0′，取得 init，**plugins 仍為 2 個**；agents、skills 為 0，APA MCP 與工具清單符合。方案 A 本次未達目標，正式請購阻擋仍在；不推論來源、不自動換方案（`docs/cli-poc.md` 第 45 節，待 Codex review）。下一步須 Aaron 在「提供完整 id／調整受管設定／以 `--safe-mode` 只驗 MCP」中擇一。
>
> **進度（10/8，plugin ID 查詢工具）**：plugin 查詢工具第二輪 R1～R3（目錄身分、中斷期間的程序清理、包裝退出碼）已修訂（`1acbf01`；`scripts/b1-04/plugin_id_query.py`、`plugin-id-query.sh`），定向 fake 驗證完成。之後補齊一般例外下的程序收尾（`5bab248`），macOS 定向 fake 驗證完成。**`5bab248` 通過限定範圍技術複核，一般例外下的程序收尾問題關閉**（`docs/cli-poc.md` 第 46.9 節）。Linux 目錄清理差異、最後 `rmdir` 的時窗、脫離 process group 的程序仍是限制。真實查詢須 Aaron 另行指定執行者並核准一次嘗試。真實查詢尚未核准、未執行；Gate、認證清單及產品行為不變。
>
> **進度（10/8，真實 plugin ID 查詢）**：依 Aaron 核准在 macOS 執行一次，結果 `no_match`（列出 1 筆，非兩個候選）。依規則停止，完整 id 仍未取得；`no_match` 不代表 plugin 不存在（`docs/cli-poc.md` 第 46.10 節）。Codex 核對通過（46.11），但沒有解除正式請購的阻擋。Codex 建議的下一步是「最小擴大 ID 輸出範圍」的修訂方案，是否進行須 Aaron 決定。
>
> **進度（10/8，現行）**：**擴大 plugin ID 查詢暫停，改為真實請購執行條件評估**（`docs/live-purchase-execution-decision.md`；路線一決策稿 R1／R2 文件補正完成，待 Codex 限定複核；尚未核准實作或真實執行）。**Aaron 10/8 已選定路線一**：維持零 plugins，保留方案 A，另加 APA 專用 `CLAUDE_CONFIG_DIR`（`~/.apa-claude`，須 Aaron 另外登入）；階段 B 排在 C0′ 之前。C0′ 須符合 §44.4 全部條件；plugins 仍非空則停止，由 Aaron 決定是否評估路線二（目前尚不可採用，不會自動生效）。認證接受清單為空，是另一個獨立阻擋。實作、C0′、認證清單修改、正式請購各自另行核准，順序依該文件 §4.1。

**為什麼現在做**
- 三條真實技術線中，只有真實 AI 的程式已大致備妥：live 開關與 init 閘門已驗收，真實 CLI 也取得過 init。剩下的是**有界的決定與一次受控執行**，不是新系統。
- 它是 M1 收尾的唯一外部前提（B1-04 → B1-06 live trace → B1-07）。
- 之後的每一條整合（`intent_hash` 綁憑證、grant、proof、tx）都需要一筆真實的請購作為起點。
- 研究 spike（Z1、A1、C1）不依賴它，可在它等待核准時另行派工。

**基準（待確認）**：從 `b1-06-demo-integration` 當時的 HEAD 開新分支（建議名 `b1-04-live`）。原因：`b1-04-buyer`（`05bb1fe`）是現在 HEAD 的祖先，之後 B1-06 的 `4599d9c`、`7267f91`、`b161e79`、`718aa91` 也改過 `cmd/apa-buyer`／`internal/buyer`；從舊分支做會分叉。是否沿用這個做法由 Aaron／Codex 決定（§9.1 第 2 題）。

**範圍與分段停止點**（每段結束都停下交 review；下一段要等各自的核准。**首次可派工的只有 L1-a**；核准 L1-a 不連帶授權設定讀取、認證清單變更或任何真實執行）

| 段 | 內容 | 需要的核准 | 停止點 |
|---|---|---|---|
| L1-a | 只在 CLI 固定環境加入 `CLAUDE_AGENT_SDK_DISABLE_BUILTIN_AGENTS=1`（`docs/cli-poc.md` §32.5 已提出），以 fake 測試、突變、容器驗證。**不猜 plugin ID、不改認證接受值、不執行真實 CLI**；閘門不變 | 設計核准 | 程式與 fake 證據交 review |
| L1-b | 釐清 2 個 plugins 的來源：Aaron 提供只含名稱的摘要，或核准 Claude Code 只讀 §32.5 第 4 點列出的鍵名。來源明確後若需要 `--settings`（flag 來源）或其他參數，**實作與 fake 驗證另成一個停止點**；若是受管設定強制啟用，由 Aaron 決定是否調整本機受管設定（Claude Code 不改） | Aaron 提供資訊，或核准讀取指定鍵名；若需實作，另有設計核准 | 來源結論交 review；如有實作，實作與 fake 證據另交 review |
| L1-c | 一次真實 C0′ 觀測，確認 plugins、agents 為空 | **執行核准**（一次 CLI 執行嘗試） | 觀測結果交 review；不空則停，不放寬閘門 |
| L1-d | 階段 B：Aaron 本人確認版本、登入與非敏感的認證方式。**經 Aaron 明確核准後**，才把特定的認證值加入接受清單（例外變更）；程式只改接受清單，fake 測試覆蓋接受與拒絕 | Aaron 確認＋明確核准這項清單變更 | 交 review |
| L1-e | 階段 C：`docs/cli-poc.md` §7.2 的一條命令、一個 task | **執行核准**（一次 CLI 執行嘗試） | 白名單 trace 與 Store 事件交 review；**L1 到此結束** |

若想先釐清 plugin 來源再做 L1-a，也可以對調 L1-a 與 L1-b 的資訊收集；但**不能在來源不明時先實作 plugin 的停用方式**。

**L1-b 現況（10/8 更新；上表 L1-b 列保留原規劃）**
- **前置條件改變**：來源調查暫緩，不再是主線前置工作；不再讀取受管設定、macOS 設定描述檔、HOME、keychain，不追查完整 plugin ID、安裝來源或 pixel-office 設定，也不為查來源擴大 probe 輸出。這不代表允許未知 plugins：真實 CLI 若仍載入 plugins，現有閘門的執行阻擋仍在。
- **下一步**：一次診斷型 C0′，只確認 plugins、agents 是否為空，並記錄其他既有白名單觀測值；須 Codex review 清單後由 Aaron 另行核准執行。它與 L1-c 的目的重疊，但結果能否當作 L1-c 的依據，仍由 review 決定。
- **結果分支**（細節見 `docs/cli-poc.md` §39.7.3）：
  - 均明確為空：交 Codex 核對；其他閘門條件仍須確認，再進 L1-d（階段 B 與認證接受值）；不直接啟動真實請購。
  - plugins 或 agents 非空：記錄執行阻擋並停止；另提最小隔離或停用方案，不追查來源、不改允許清單、不重試。方案的實作與真實驗證另行核准；核准前上段「來源不明時不先實作停用方式」仍適用。
  - 資料缺失、型別錯誤或無法判定：記為未確認，交 Codex 核對；未知不當作空。
  - 版本不符、逾時、解析錯誤或其他失敗：保存白名單證據與固定代碼後停止，不重跑、不改版本或參數。

**兩種規則分開**
- **不放寬**：plugins／agents 必須是空清單，以及版本、工具、MCP、權限模式、hook 等其他閘門檢查。任何一段都不放寬。
- **例外變更**：只有 L1-d、且經 Aaron 明確核准，才可把特定認證值加入接受清單。`none` 本身不能證明訂閱或計費來源；階段 B 的本人確認只是有邊界的佐證，不寫成程式已證明。

**執行次數與用量**：L1-c、L1-e 各是**一次 CLI 執行嘗試**；模型請求次數沒有硬保證，用量可能 unknown。版本不符或命中任何停止條件（`docs/cli-poc.md` §7.4）即停，不自動重試，也不改版本。

**登入資料**：CLI 一經核准執行，仍可能透過 HOME／keychain 取用登入資料。「Claude Code 不直接讀憑證檔」不等於 CLI 子程序不使用憑證。沿用 `docs/cli-poc.md` §7.5 的既有隔離限制，本路線不另造沙箱。

**不做**：放寬閘門的空清單或其他檢查；未經 L1-d 明確核准的任何清單變更；讀取 HOME、keychain、登入憑證或 L1-b 核准範圍外的設定檔；超出核准次數的 live 執行；付款、核准、grant；改 B1-06 展示（屬 L2）；標 Done 或合併。

**測試**
- L1-a、L1-b 的實作段、L1-d：Go 測試與 race、突變（閘門仍拒絕非空 plugins／agents、錯版本、不在清單的認證；init 未通過時不開 Store）、容器完整驗證、`scripts/verify-b1-01.sh`。
- L1-c、L1-e：只執行核准的那一次 CLI 執行嘗試；結果依 `docs/cli-poc.md` §7.4 停止條件判讀。

**成功條件**
- 真實 CLI 的 init 通過閘門，閘門未放寬。
- 一筆 live 請購：模型呼叫 `list_products`／`get_quote`／`request_purchase` → 商店 402 → `PURCHASE_PROPOSED` → `POLICY_DECIDED`（預期 Ask）寫入 `var/apa-live.db`（不碰既有展示資料庫）。
- trace 只含白名單欄位；模型用量為 CLI 回報或 unknown。

**觀眾可見增量**：L1 本身只產生證據；畫面上的呈現由 L2 接上。

**證據位置**：`artifacts/b1-04/`（各段分開目錄；失敗也保留）。

**新依賴與資源**：無新套件。使用 Aaron 本人的 Claude Code 訂閱登入；L1-c、L1-e 各一次 CLI 執行嘗試（模型請求次數無硬保證，用量可能 unknown）。

## 9 待 Aaron 決定

不重問已定案的事：Unity＋LimeZu、必須有真 AI／ZK／鏈上付款、60／40 原則、Base Sepolia 兩筆 tx 基線。

### 9.1 L1 的決定項與最晚決定時點

**只派 L1-a 時，只需決定第 1、2 項**；其餘各項在對應段開始前才需決定，核准前一段不連帶核准後一段。

| # | 決定項 | 建議 | 最晚決定時點 | 影響 |
|---|---|---|---|---|
| 1 | 是否派工 L1-a（只含停用內建 agents 的環境變數與 fake 驗證） | 派工 | L1-a 開工前 | 不派則真實 AI 線停在 init，M1 無法收尾 |
| 2 | 工作分支基準 | 從 `b1-06-demo-integration` 開 `b1-04-live` | L1-a 開工前 | 從 `b1-04-buyer` 做會與 B1-06 對 buyer 的修改分叉 |
| 3 | plugins 來源怎麼確認 | **10/8 已決定：暫緩來源調查**，來源未確認保留為已知限制，閘門不變（原建議：核准 Claude Code 只讀 `docs/cli-poc.md` §32.5 第 4 點列出的鍵名（不讀其他內容）；或由 Aaron 提供名稱摘要） | L1-b 開始前 | 不確認來源就無法判斷能否在 session 層停用 |
| 4 | 來源明確後，是否實作對應的停用參數；或受管設定強制啟用時，是否由 Aaron 調整受管設定 | 依 L1-b 結論決定；不改成名稱白名單。**10/8 起改依診斷型 C0′ 結果**：非空時另提最小隔離或停用方案，實作與真實驗證另行核准 | L1-b 的實作段開始前 | 逐一允許要改閘門，需另行 review |
| 5 | 一次 C0′ 觀測（L1-c） | 單獨核准一次 CLI 執行嘗試 | L1-c 執行前 | 可能產生用量；結果不空則停 |
| 6 | 階段 B 本人確認，以及是否把特定認證值（建議只考慮 `none`）加入接受清單 | 確認後再明確核准這項例外變更 | L1-d 開始前 | 維持空清單則 live 必停在閘門；`none` 不能證明計費來源 |
| 7 | 一次階段 C（L1-e） | 單獨核准一次 CLI 執行嘗試 | L1-e 執行前 | 可能產生用量 |

### 9.2 可以之後再決定

| # | 問題 | 建議 | 何時必須決定 |
|---|---|---|---|
| 8 | 付款帳戶 ADR | **保留 EOA＋EIP-3009 基線**（ADR 已列為研究期間的預設路徑）；S4-B 改為可選研究，不阻擋 P1／P2 | P1 開工前 |
| 9 | spike 是否在 L1 等待核准時並行 | 可並行一片（建議先 Z1），仍逐片派工與 review | L1-a 交出後 |
| 10 | gnark、BBS、WebAuthn 函式庫與合約工具鏈等新依賴 | 各 spike 提出選型與授權紀錄後核准 | 各 spike 開工時 |
| 11 | 外接 FIDO2 key 是否採購 | 依 A1 實測決定 | A1 結論後 |
| 12 | BBS+ 選庫卡關時是否接受改 SD-JWT VC | 先不接受；C1 若卡關再提出證據與影響 | C1 結論時 |
| 13 | testnet：RPC、facilitator、faucet 資金、A／B／M 錢包建立與私鑰保管 | 分項核准；不碰 mainnet | P2 開工前 |
| 14 | T1 的排程 | 排在 L2 之前 | L1 完成後 |
| 15 | D3–D6 的順序 | 依 §5.5 | L2 設計時 |
| 16 | 語音模型與樣本同意 | V1 開工前另提 | A2 之後、V1 開工前 |
| 17 | 真人委託頁的設計時機 | 依原規劃在 M1 完成後設計 review | M1 結案後 |
| 18 | 多 Agent 的最小門檻 | 接受「兩位不同 consumer 的真實 Agent、工作區間重疊」；付款不必並行 | M-A 開工前 |
| 19 | 是否修訂「一次一個 active task」基線（PRD §3.2、§5；計畫 L444；任務清單 L32） | 同意由 M-A 提出修訂草案，核准後才改 | M-A 交出時 |
| 20 | Admin 可讀範圍 | 以 host 角色為基礎，看全部聚合與白名單明細；他人完整請購明細是否可讀由 Aaron 定；operator 不擴權 | M-A 設計時 |
| 21 | M-B 的時機 | M1 收尾後、P1 前（避免投影與場次協調重做）；若要優先付款，可延到 P1 之後 | M-A 核准後 |
| 22 | 兩筆付款是否必須並行 | 先不要求；另行設計 | P2／E1 前 |

## 10 本輪做了什麼、沒做什麼

- 做了：閱讀 §1.1 的資料（規劃指示與四份 vault 筆記在本輪進行中又新增「多 Agent 與 Admin 營運視角」，已重新讀取，補寫 §6 並校正行號）；以 `git ls-files` 與全文搜尋盤點 repo；寫本文件；README 補入口與「規劃待 review」狀態；vault 任務清單補現行摘要與紀錄。
- 沒做：修改程式、測試、契約、`cases.json`、資料表或驗收門檻；跑任何測試、建置或容器；執行真實 CLI（含版本、登入、help）、讀 HOME／keychain／受管設定；建立私鑰、索取資金、RPC、部署或交易；操作既有展示資料庫；開始 `R2 order`、D3–D6 或本文件任何切片；合併、推送、標 Done。
- 盤點限制：狀態以 repo 檔案、git 與文件紀錄為準；未重跑任何證據。其他專案的 PoC 不算本 repo 的實作。

## 11 修訂紀錄：依 Codex 對 `638c9f7` 的 review（2026-10-07）

依據：vault〈Claude-Code-商業展示版路線限定修訂指示-638c9f7-20261007〉。Codex 接受路線方向（先取得單筆真實 AI 請購證據，再接授權、憑證、ZK、鏈上預留與結算、交付；多 Agent 與 Admin 已有具體切片），要求補正四項與兩處文字。本輪只改文件；證據歸屬是**文件與程式來源的交叉核對**，未執行測試、CLI、容器、Unity、RPC 或交易，也不拿舊測試當本輪結果。

| 項目 | 原寫法（`638c9f7`） | 現行 | 位置 |
|---|---|---|---|
| R1 必要依賴 | C1 不接 Policy，正式 BBS 只寫「併入 P1 或另開」；WebAuthn 未列接線條件；V1「依進度」；AI 評測「依進度插入」；E1 只有一個出口 | 新增 **C2**（正式 BBS 接入 Policy）、**A2**（WebAuthn 接入 Approval／grant）、**AE1**（AI 評測），各附原任務對應與驗收出口；**V1** 改為必要，保留 PRD §7 的 unavailable 降級規則且「從未實作」不算已驗證降級；E1 分成 **E1-a 可開始整合**與 **E1-b 最終驗收**，後者須 C2、A2、V1、AE1 全部通過；研究 spike 通過不算整合通過 | §4.1、§4.2、§4.3、§5.1（AE1）、§5.2（C1、C2、A1、A2、V1）、§5.3（P1）、§5.4（E1）、§5.6、§7 |
| R2 Admin 指標 | 活躍 Agent 的來源寫成 `host_runs`（狀態 running）；未知狀態只有一種；已結算的去重鍵寫成 `intent_hash`（或 tx#2 hash） | 活躍 run 改以 Host 程序內 `liveRun.state`（API 的 `live`）為來源，`running`／`stopping` 分列，並與活躍 Agent 數分開；持久化狀態只有 `accepted／interrupted／ended`，`accepted` 與 `interrupted` 都不算活躍；來源讀不到時標 stale 與時間，不回 0；未知狀態分「可枚舉的未收斂意圖」與「整個來源讀不到」；已結算以唯一消費意圖為單位，須以鏈別＋資產＋可唯一定位的鏈上結算證據映射，tx 數不等於消費筆數；補四個預期 | §6.4（Host 列）、§6.5 |
| R3 有效工作重疊 | 劇本為「A 得到 Ask 後等待，再開始 B」；停止 A 未設前提；「模型用量加倍」 | 路線層定義重疊證據（Host 對應、各自真實工具呼叫、有效工作區間交集）；Ask 期限重疊、閒置程序、動畫、回放不算；M-C 須提出可控制的工作／同步條件（本文件不新增睡眠或卡住工具）；A 已結束時只能回報 task 重疊、不得通過 M-C，也不得演示停止 A；用量改為逐 run 記錄、可能 unknown | §6.2、§6.7、§6.8（M-C）、§7（多 Agent 列） |
| R4 L1 順序與核准邊界 | L1-a 先實作可能的 plugin 停用參數；L1-d 改認證清單卻寫「不放寬任何允許清單」；§9.1 把後段決定都列為阻擋 | L1-a 只含停用內建 agents 的環境變數與 fake 驗證；plugin 來源明確後才有 L1-b 的實作停止點；「不放寬」與「L1-d 經明確核准的認證值例外變更」分開寫，`none` 不能證明計費來源；L1-c、L1-e 改為各一次 CLI 執行嘗試；載明核准執行後 CLI 仍可能經 HOME／keychain 使用登入資料；§9.1 改為「決定項與最晚決定時點」，只派 L1-a 時只需決定兩項 | §0、§4.3、§8、§9.1、§9.2（編號順延） |
| 同步一 | 摘要「差的不是畫面」 | 「已有可操作的 fake 展示基礎；商業視覺、多 Agent／Admin 體驗與真實技術整合仍有缺口」；D1／D2 通過不代表已達 WareTrack 參考體驗 | §0 |
| 同步二 | §3.1 第 0 步「憑證缺失／過期：task 不啟動」 | 標為**未核准的未來入口提案**，並寫出現行語意：Store 接受明確缺設定的 task，Policy 得 Deny `CONFIG_MISSING`（`docs/persistence-design.md` §5.2、§9.7）；本文件不改寫此語意 | §3.1 |

本輪沒有開始 L1 或任何切片；B1-04／B1-06 維持 InProgress；不合併、不推送、不標 Done。
