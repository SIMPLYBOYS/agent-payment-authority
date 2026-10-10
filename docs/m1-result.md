# M1 結果（已結案，2026-10-11）

> 2026-10-11，Claude Code 整理（M1-E4）。基準 HEAD `ca9bde5`，本文件的 commit 見 git log。
> **M1 已結案（2026-10-11）**：M1-E4 最後核對通過，B1-04、B1-06、B1-07 與 M1 標為 Done，結案紀錄見第 10 節。本文件原為結案候選報告；第 2、4.2、9 節的候選與待核對說法已由第 10 節取代，原文的歷史脈絡保留在第 10 節的說明中。
> 10/11 E4 限定核對通過（Codex 核對文件、Git 差異、三份 PASS 報告，並獨立重算 187 個檔案雜湊，全部相符；沒有重跑檢查器、瀏覽器或真實 CLI）；受控挑戰執行後（第 5.3 節）經最後核對通過。
> 本文件只放可公開的摘要與索引。`artifacts/` 下的路徑是本機證據的位置（被 Git 忽略）；列出位置不代表那些資料已核准公開。日後若做公開的刪減包，刪減後不足以驗證的 claim 不能沿用私有包的 PASS。

## 0 M1 要什麼（不用讀 vault 也能看懂）

**M1 的流程**：主持人啟動一個 task → 真實 Claude Code 選商品 → 本 repo 的資料商店回 HTTP 402 → wrapper 建立請購 → 確定性 Policy 判定 **Ask 或 Deny** → Unity 商店與 Dashboard 依持久化事件呈現「等待核准」或「拒絕」。

- Ask 是「等待真人核准」，不是核准；M1 沒有核准功能、不發 grant、不付款、不交付資料。
- A、B 兩位消費者各有不同的 fixture 錢包參照；憑證、餘額與部分身份資料是明標的 fixture。

**M1 完成門檻**：
- B1-00～B1-07 全部通過；
- B1-06 產出執行證據，再由 B1-07 只讀核對；必要 claim 有 FAIL 或 INCONCLUSIVE 就不能結案；
- M1 完成不等於第一期 PRD 驗收完成。

**這份報告對照的要求**：
- 任務卡 B1-04（真實 Claude Code Buyer wrapper）、B1-06（獨立展示層＋M1 整合）、B1-07（只讀驗收器與證據包）的勾選項；
- 統一驗收入口：`make check`、`make dev`、`make m1-fixture`、`make m1-live-cli`、`make verify-m1`；
- 執行計畫驗收 41～44 的 M1 子集：只讀獨立驗收入口、可重跑證據包與完整性邊界、正反案例與現場挑戰、消費者／Agent／錢包隔離。

## 1 摘要

**做到的事**：
- 真實 Claude Code（`2.1.293`）透過 wrapper、受限工具與本地商店的 402，提出一筆結構化請購，得到 **Ask／HUMAN_APPROVAL_REQUIRED／pending**。這筆 live 證據包經 B1-07 檢查器判 PASS，並通過 Codex 限定核對（第 3.2 節）。
- fake CLI 跑完凍結的 31 個案例（13 個到端對端、3 個到 Store、15 個到契約層），加上瀏覽器的整合要求。這份 fixture 證據包經 B1-07 判 PASS（31 案、V1～V7、R），並通過限定驗收（第 3.1 節）。
- 獨立的只讀檢查器（`make verify-m1`），fixture 與 live 兩種模式分開判定，三態結果與不同的結束碼。
- 本 repo 自己的 Unity＋LimeZu 商店、Dashboard、Inspector／Timeline、主持人頁、A／B 隔離、取消與重啟、歷史回放與單場真實展示。

**沒有做到、或不在 M1 的事**：
- 核准、grant、付款、交付；BBS+ 憑證、WebAuthn、語音、ZK、Signer、鏈上交易，都沒有，七個 capability 一律 `not_evaluated`。
- 嚴格零 plugin 模式沒有驗證；live 證據是在 Aaron 接受的開發展示例外（`dev_demo_plugins`）下取得的。
- 尚待確認的項目見第 4.2 節：受控挑戰的最後核對，以及任務卡勾選與結案由 Codex／Aaron 決定。

## 2 B1-00～B1-07 狀態總表

| 任務 | 現況 | 結案建議 |
|---|---|---|
| B1-00 派工口徑與最小邊界 | Done（9/30） | 沿用 |
| B1-01 Go 專案與開發骨架 | Done（9/30，`97ef4e9`） | 沿用 |
| B1-02 共用資料契約與測試向量 | Done（10/1，`2dc8d93`） | 沿用 |
| B1-03 資料商店：目錄＋402 | Done（10/1，`ebd3f44`） | 沿用 |
| B1-04 Claude Code Buyer wrapper | **Done（10/11）** | 8 項驗收要求依證據與已接受的範圍調整完成（第 5.1 節）；真實 trace 由 live 包取得 |
| B1-05 Policy 與持久化事件 | Done（10/2，`61b3652`、`cb33768`） | 沿用 |
| B1-06 獨立展示層＋M1 整合 | **Done（10/11）** | 10 項驗收要求依證據與已接受的範圍調整完成（第 5.2 節）；乾淨目錄 E2E 由 E4 補驗 |
| B1-07 只讀驗收器與證據包 | **Done（10/11）** | 8 項驗收要求完成（第 5.3 節）；受控挑戰經最後核對通過 |

B1-00～03、B1-05 這五張卡沿用既有的 Done 與驗收，本報告不重新判定。B1-04、B1-06、B1-07 於 10/11 結案（第 10 節）；M1 的 B1-00～B1-07 全部 Done。

## 3 證據包

fixture 與 live 是兩種模式，各自判定，**不能相互代替**：fixture 通過不代表 live 已驗證，live 的單筆 Ask 也不代表 31 案通過。

### 3.1 fixture 完整案例包

| 項目 | 內容 |
|---|---|
| run ID | `m1-fixture-20261009T162703Z-4ddb7a`（`artifacts/m1/`） |
| 產生端 | `make m1-fixture`（`cmd/m1-fixture`），fake CLI，不連模型 |
| 產生端基準 | 程式 `0d13255`，測試與產包 `0853b44`（`code_commit`），dirty patch 為空 |
| 案例 | `m1-cases-v2`，case manifest digest `sha256:d8b74074…b8d51d` |
| 命令 | 七個，結束碼都是 0：契約、Policy、Store、host E2E、host 整合（含取消與 live 模式的 fake）、web adapter、瀏覽器 |
| 結果 | 31 案 PASS；七個 capability `not_evaluated`；結束標記 `complete` |
| 結束標記的 manifest digest | `sha256:7f1537dd…211e427` |
| 檢查器 | B1-07 fixture 模式：程式 `48a43bc`、測試 `973a58d` 限定驗收（`docs/m1-evidence.md` §15.7）；判 PASS，V1～V7 與整合要求 R 都是 PASS |
| 驗收 | M1-E1 產生端限定驗收（§14）、M1-E2 檢查器限定驗收（§15.7）；Codex 獨立核對包內 81 個檔案的雜湊沒有改變 |

分層（§4）：E2E 13 案、只到 Store 3 案、只到契約 15 案。A／B 冒用（`cross-consumer*`、`altered-*`）只在契約層，入口拒絕另有 wrapper 與 HTTP 的測試；證據只宣稱它實際所在的層級。

### 3.2 live 單筆 Ask 包

| 項目 | 內容 |
|---|---|
| run ID | `m1-live-20261010T173831Z-9d32ef`（`artifacts/m1/`） |
| 產生端 | `make m1-live-cli`（`cmd/m1-live`），一次真實 Claude Code 執行，Aaron 10/11 核准、Claude Code 執行 |
| 執行基準 | HEAD `076f398`（`code_commit`），`BASE=4feb7df`；包內 apa-buyer 建置 `vcs.modified=false` |
| CLI 與 Gate | `2.1.293 (Claude Code)`；`gate_mode=dev_demo_plugins`、`passed`；要求的模型 `claude-sonnet-5-5` |
| 結果 | `ask` PASS（Ask／HUMAN_APPROVAL_REQUIRED／pending）；其他 30 案與七個 capability `not_evaluated`；`termination=completed`；結束標記 `complete` |
| 用量 | CLI 自報（不是帳單）：input 8、output 421、cache creation 3107、cache read 8100 tokens |
| 結束標記的 manifest digest | `sha256:b7257ece…dc7025a` |
| 檢查器 | B1-07 live 模式：程式 `c11138a`、測試 `4feb7df` 通過限定技術 review（§16.9）；判 PASS，V1～V7 PASS，R `NOT_EVALUATED`，0 個 finding |
| 驗收 | Codex 限定核對通過（§16.11）：斷網容器內重跑檢查器、唯讀查資料庫一致、8 個檔案與資料庫的雜湊沒有改變 |

live 包只宣稱 `ask`。其他 30 案、七個 capability 與整合要求 R 是這個模式的範圍外，維持 `not_evaluated`，不改成 PASS，也不因此要求 live 重做整份 fixture。

### 3.3 E4 的唯讀複查

整理本報告時，用已 review 的檢查器（由 `ca9bde5` 的乾淨 clone 建置，`vcs.modified=false`；程式碼與 `4feb7df` 相同）對兩個包各跑一次：
- fixture 模式：結束碼 0，PASS，31 案 PASS，0 個 finding；
- live 模式：結束碼 0，PASS，1 案 PASS，37 項 not_evaluated，0 個 finding；
- 兩個包的檔案（81 個、8 個）核對前後的 sha256 相同。

證據：`artifacts/m1-verify/e4-20261011-readonly/`。這是為本報告做的複查，不是新的驗收；兩個包仍各屬原本的基準與 review。

### 3.4 乾淨目錄的 fixture E2E（E4 補驗）

**缺口**：B1-06 要求「在沒有任何 sibling repo 的乾淨工作目錄，依本 repo README 啟動後端、商店與 UI，並完成 fixture E2E」。E4 之前只有 Go／Node 測試在乾淨 clone 跑過（例如 `scripts/verify-b1-01.sh`、斷網容器、E2／E3 的 race 回歸），沒有啟動商店與 UI 並完成瀏覽器 fixture E2E 的紀錄。

**做法**（一次，fake，不啟動模型）：
- 位置：新建的空目錄，裡面只有本 repo 的 `git clone --no-hardlinks`（`ca9bde5`，已追蹤的檔案沒有修改）。沒有搬移或刪除任何既有專案，沒有複製日常的 `.env`、`var/`、登入目錄或憑證。
- README 步驟：`cp .env.example .env`，然後 `make m1-fixture`（README「M1 證據包與唯讀檢查」）。它的瀏覽器步驟會建置並啟動 `cmd/demo`（後端、商店、Dashboard），以 Chrome 跑完 fixture 流程。fixture 產包的程式從 E1 基準 `0853b44` 至今沒有改變。
- Unity Web 建置不在 Git 裡。使用 APA 自己既有的建置副本：由本 repo 的 Unity 專案以 `scripts/unity/unity.sh` 建置（2026-10-08，對應 Unity 原始碼最後的 commit `578f2c3`），複製到 clone 中被忽略的 `unity/AgentPaymentAuthority/Build/`；17 個檔案的 sha256 與來源相同。沒有重建，也沒有從其他專案取檔。
- 前置條件：Go、Node、Google Chrome（固定路徑），以及本機共用的 Go 模組與建置快取。**這不是全新離線機器的驗證。**

**結果**：2026-10-10T18:10:58Z～18:18:41Z（UTC，7 分 43 秒，其中瀏覽器 424 秒），make 結束碼 0。
- 新的 fixture 包 `artifacts/m1-e4-clean/m1-fixture-20261010T181100Z-6a622b/`：`complete`，31 案 PASS，七個 capability `not_evaluated`，七個命令結束碼都是 0，整合要求全部通過；`code_commit` 是 `ca9bde5`，dirty patch 為空。
- 瀏覽器：317 項 PASS、0 FAIL；Unity Web 建置在瀏覽器中載入，使用 LimeZu 素材（不是 placeholder）。
- 已 review 的檢查器（同第 3.3 節）唯讀核對：結束碼 0，PASS；31 案、V1～V7 與 R 都是 PASS，0 個 finding；核對前後 81 個檔案的 sha256 相同。

證據：`artifacts/m1-verify/e4-clean-20261011/`（`NOTE.txt`、產包輸出、WebGL 建置的 sha256、檢查器報告、包內 sha256）。這是乾淨目錄補驗產生的新包，不取代、也沒有覆寫已驗收的 E1 包，也不是 live 包。

## 4 必要出口對照

### 4.1 對照表

| 出口 | 證據 | 判斷 |
|---|---|---|
| `make check` | B1-01 Done；之後各輪的 race 與完整容器紀錄（例如 `docs/cli-poc.md` §55.5） | 已有 |
| `make dev` | B1-01 Done（啟停、port、缺設定） | 已有 |
| `make m1-fixture` | 第 3.1 節；E4 乾淨目錄補驗見第 3.4 節 | 已有 |
| `make m1-live-cli` | 第 3.2 節 | 已有 |
| `make verify-m1`（fixture／live） | 第 3.1、3.2、3.3 節 | 已有 |
| B1-04 真實模型請購 trace | live 包（第 3.2 節） | 已有 |
| B1-06 執行證據經 B1-07 只讀核對 | fixture 包與 live 包都經檢查器 PASS | 已有 |
| B1-06 乾淨目錄依 README 完成 fixture E2E | 第 3.4 節 | 已有（E4 補驗，Codex 接受；前置條件見第 3.4 節） |
| 驗收 41 只讀獨立驗收入口（M1 子集） | `cmd/verify-demo`、`internal/verification`；不呼叫 Policy 作 oracle；§15 | 已有 |
| 驗收 42 可重跑證據包與完整性邊界 | run ID 對應 manifest、digest、命令與全部結果；另一終端離線重跑（§15.7、§16.11、本報告 3.3） | 已有；限制見第 7 節 |
| 驗收 43 正反案例與現場挑戰 | 凍結的正反案例都在 fixture 包；受控挑戰：Codex 選定 `deny-merchant`，Claude Code 執行 Ask → Deny → Ask（第 5.3 節） | 已有（M1 子集，fake） |
| 驗收 44 消費者／Agent／錢包隔離（M1 子集） | V7（A／B registry snapshot）、`cross-consumer*`、`altered-*`、`restart`；真 grant、Signer 選 key 與鏈上隔離為 `not_evaluated` | 已有（M1 子集） |
| `docs/m1-result.md` | 本文件 | 已有（結案紀錄第 10 節） |

### 4.2 未滿足或需要決定的項目

**現況（10/11 結案）**：沒有未滿足的 M1 必要項目，第 10 節的結案紀錄取代本節原本的候選清單。以下是當時待決各項的處理結果，保留作為依據：

1. **任務卡勾選**：依第 5 節的逐卡對照，B1-04、B1-06、B1-07 已接受的驗收項於 10/11 勾選並結案。
2. **B1-07 受控挑戰**（驗收 43）：Codex 選定 `deny-merchant`，Claude Code 執行（第 5.3 節），經最後核對通過，缺口關閉。
3. **B1-04 第 3 項「結構化輸出範例」**：Codex 接受白名單 trace 符合要求，不必保存可能含敏感內容的原始 stream-json。
4. **B1-06 第 6 項的標示文字**：Codex 接受「合成資料・不付款」搭配真實 Agent／開發展示例外的說明，符合來源揭露要求。
5. **B1-06 第 2 項的事件名稱**：任務卡的 `QuoteReceived` 不在事件契約裡，依已接受的對照以 `PURCHASE_PROPOSED` 呈現（第 5.2 節）；不新增事件。
6. **乾淨目錄補驗的充分程度**：Codex 接受符合「不依賴 sibling repo」的要求；既有的 APA WebGL 建置與快取是已揭露的前置條件，不必再測全新離線機器。

## 5 逐卡核對

分類：
- **(a)** 已有充分證據，建議勾選；
- **(b)** 已接受的範圍調整，附決定的位置；
- **(c)** 缺證據或尚待判定，列出缺口；
- **(d)** 不在 M1 範圍，附依據。

這是任務完成度的分類，和檢查器的 PASS／FAIL／INCONCLUSIVE 是兩回事。「cli-poc §N」指 `docs/cli-poc.md`，「evidence §N」指 `docs/m1-evidence.md`。

### 5.1 B1-04 Claude Code Buyer wrapper

| # | 要求（摘要） | 實作 | 證據與 review | 層級 | 剩餘限制 | 分類 |
|---|---|---|---|---|---|---|
| 1 | CLI 啟停、串流解析、取消、受限工具橋接 | `internal/buyer`（runner、host、gate）、`internal/toolbridge`（MCP 只用標準函式庫） | fake 首個交付點 `b3c66f6`、live 階段 A `e8f6719` 通過 Codex review；live：cli-poc §53、evidence §16.10／16.11 | fake＋live | 不是作業系統沙箱；同一使用者的程序可讀 capability（cli-poc §7.5、§8）；live 沒有實測取消 | (a) |
| 2 | 專用工作目錄、明確工具與設定，只有三個工具 | 空的私有 cwd、`--restricted`、`--tools ""`、`--strict-mcp-config`；專用設定目錄 `~/.apa-claude`（cli-poc §47） | cli-poc §47.5、§43.6、§36（L1-a `418f0de`）通過；live init 只有 3 個工具（§49、§52） | fake＋live | 真實 init 仍列出 2 個 plugin，嚴格零 plugin 模式未驗證 | (b)：Aaron 10/10 接受 `dev_demo_plugins`（`f0adfb3`；`docs/m1-closeout-gaps.md` §2） |
| 3 | CLI 版本、模型、auth 模式與結構化輸出留檔；本人官方登入 | 版本釘選 `2.1.293`；環境白名單移除 API key 與 OAuth token；認證只接受 `none`（cli-poc §50.1） | 本人登入的確認（cli-poc §48.1）、runtime `apiKeySource=none`（§30、§41、§49）、模型（§53、evidence §16.10）；§50.7 `21090e3` 通過；`none` 的依據見 `docs/live-purchase-execution-decision.md` §9.3、§9.6 | live | 本人登入、runtime `auth_source` 與計費是三份不同的資料，`none` 不證明登入或計費來源；trace 只留白名單，不留原始 stream-json | (b) 認證只接受 `none`（決定稿 §9.6）；白名單 trace 滿足「結構化輸出範例」（Codex 10/11 接受） |
| 4 | 任務 → 目錄 → 402 → 結構化請購；報價來自商家 | `toolbridge/shop.go`（402 嚴格解碼、URL 原樣比對、報價簿） | `Test402Tampering`、`TestPurchaseIntegrity`、`TestRestartAndRepricing` 等；live 的工具鏈 list_products → get_quote → request_purchase 得到 Ask（cli-poc §53.1、evidence §16.10） | fake＋live | 付款不在 M1；Ask 維持 pending | (a) |
| 5 | 工具輸入經契約驗證；非法工具、任意檔案或 URL、注入、「已付款」都不能變成付款事件 | `tools.go` 只接受 `product_id`（固定格式）；請購經 `Store.Submit` | `testdata/cli/forged-args.json`（身份、價格、URL、路徑、shell、`bash`、「付款完成」）；`b3c66f6`、`e8f6719` | fake | live 沒有做對抗測試（預期如此） | (a) |
| 6 | consumer／agent／task 由 wrapper 的已認證 context 固定；A 冒用 B 被拒；切到 B 不沿用 A 的 capability | `--task` 解析到 registry；每場 32 位元組的 capability，結束即撤銷；Store `OWNERSHIP_MISMATCH` | `TestIdentityIsolation`、`TestRelayIdentity`、`TestHostCapabilityLifetime`；`b3c66f6` | fake | 模型根本不能傳 wallet／from，比「與可信映射比對」更嚴格 | (a) |
| 7 | fake runner 測未安裝、未登入、無效 JSON、限流、逾時、取消 | `internal/buyer/fakecli`、`cmd/fake-claude`、`testdata/cli/` 的情境 | `TestRunnerNotInstalled`、`TestRunnerOutcomes`、`TestRunnerTimeoutCancelAndCleanup`、`TestProbeAPIRetry`；`b3c66f6`、`5f34a7b` | fake | 對真實未登入、限流輸出的分類沒有實測 | (a) |
| 8 | live 明確 opt-in；任務數、輪次、時間上限；不無限重試；usage 不可得記 unknown | `--live` 加 `APA_LIVE_CLI=true`、絕對 CLI 路徑、版本檢查；180 秒、10 則回覆、8 次工具呼叫；沒有重試路徑 | `TestLiveOptIn`、`TestAssistantLimit`、`TestHostRetryAndCallBudget`；live 實際紀錄 usage 為 unknown（cli-poc §49、§52）或自報（§53、evidence §16.10） | fake＋live | CLI 沒有 `--max-turns`，回覆上限是事後檢查；usage 是自報、不是帳單 | (a) |
| 驗收 | `go test ./internal/buyer/... ./internal/toolbridge/...`，加一次真實模型請購 trace | — | `go test -race -count=1 ./...` 於 `145eb86` 全部通過（cli-poc §55.5，Claude Code 執行），這三個套件之後沒有再改；真實 trace：live 包（第 3.2 節） | fake＋live | 包沒有簽章；外層產包器與當時的檢查器帶 modified 標記（evidence §16.11） | (a) |

另外：任務卡寫明 M1 沒有付款鑰匙，不能因此宣稱 Signer 隔離（驗收 6／9／33）已通過，這部分是 (d)。cli-poc §53、§55.11、§56.9 的真實執行是較早的紀錄，不是由 `make m1-live-cli` 產生，其中兩次是 modified 建置，只作輔助。

### 5.2 B1-06 獨立展示層＋M1 整合

| # | 要求（摘要） | 實作 | 證據與 review | 層級 | 剩餘限制 | 分類 |
|---|---|---|---|---|---|---|
| 1 | 本 repo 自己的 Unity＋LimeZu 商店與 Dashboard，同一份投影 | `unity/AgentPaymentAuthority/`、`web/`（adapter、app、unity） | 交付點 2 `6aee7f0`、D1 `6338fd2`、D2 `78cb2cc`／`6137708` 通過限定 review；瀏覽器測試比對 Unity 與 Dashboard 的序號與階段 | fake、瀏覽器、Unity、人工目視 | 都是 fake 展示範圍 | (a) |
| 2 | A／B profile、場景對照、同人沿用錢包、不演成功取貨 | `docs/demo-ui.md` §1 的場景表、SceneRules | 已接受的對照：TASK_STARTED → 進場、PURCHASE_PROPOSED → 報價、POLICY_DECIDED Ask／Deny → 等待／離場（`docs/demo-presentation-design.md` §11、§16.11；D1 設計 §14.3、D2 §17.7）；`task-a-next` 沿用 wallet-a | fake、瀏覽器 | 任務卡的 `QuoteReceived` 不在事件契約中，沒有為了勾選新增事件；獨立的報價時刻屬 D6，未核准 | (b)：場景／事件對照（上列位置） |
| 3 | Inspector 欄位；單一 sanitized JSON，沒有搜尋或匯出 | `docs/demo-ui.md` §1、`web/adapter.js` | 交付點 2、D1；餘額與剩餘額度顯示 unknown（API 不提供） | fake、瀏覽器 | D1 卡片是摘要，完整欄位在「技術詳情」 | (a) |
| 4 | 事件重送不重複動畫；刷新由 Store 還原；取消與 CLI 失敗有獨立狀態 | adapter 的去重、3.4 的狀態 | 3.4 `3a6af59`；瀏覽器「refresh + sign-in restores task-a from the Store」與 3.4 段；fixture 包的整合要求 R | fake、瀏覽器 | CLI 逾時的畫面只由 3.4／3.5 觸發 | (a) |
| 5 | 契約排序與事件 ID、adapter 測試、只讀端點、認證 | `web/test/adapter.test.mjs`、`hostcreds.test.mjs`；`cmd/demo` 的 Host／Origin、viewer 與 host token、MCP 401 | 交付點 1 `f2d63fb`、3.1 `b2b7daa` | fake | 認證是本機展示用的 viewer capability，不是真實使用者認證 | (a) |
| 6 | 核心 fixture E2E、UI adapter 測試、至少一次 opt-in live trace；混用來源標示 | fixture 包（第 3.1 節）、live 包（第 3.2 節） | evidence §14、§15.7、§16.11 | fixture＋live | 畫面文字是「合成資料・不付款」「開發展示例外、未付款」，不是任務卡原文「部分 fixture／未付款」 | (a)；標示文字經 Codex 10/11 接受 |
| 7 | 缺 LimeZu 時標示 placeholder；記錄素材來源、匯入清單與授權；不提交原始素材 | `docs/demo-ui.md` §2；`unity/AgentPaymentAuthority/limezu-assets.txt`（16 個檔）；每檔 sha256 寫到被忽略的 `Assets/LimeZu/IMPORTED.txt` | 交付點 2 `6aee7f0` | — | 來源是 Aaron 10/5 核准的一次性複製例外（素材包本身取自本機另一個專案的目錄）；能否發布建置仍待決（demo-ui §8、§11） | (a) 紀錄；(b) 一次性複製例外 |
| 8 | 主持人事件 HTTP 端點：認證、白名單投影、A／B 隔離、HTTP 與 UI 整合測試 | `cmd/demo`；`docs/demo-integration.md` §3 | 交付點 1 `f2d63fb` | fake | — | (a) |
| 9 | `cmd/demo` 的 Store 接線、設定載入、RunExpiry 啟停、關閉順序 | `docs/demo-integration.md` §1、§8.3 | `TestIntegrationStartRefusals`；`f2d63fb` | fake | — | (a) |
| 10 | 沒有 sibling repo 的乾淨目錄，依 README 啟動後端、商店與 UI，完成 fixture E2E | 第 3.4 節 | E4 補驗（第 3.4 節）：乾淨 clone、`cp .env.example .env`、`make m1-fixture` 結束碼 0；新包 31 案 PASS，檢查器 PASS；瀏覽器 317 PASS | fake、瀏覽器 | 用了本機快取與既有 WebGL 建置副本，不是全新離線機器；沒有重建 Unity | (a)（Codex 10/11 接受） |
| 必測 | 主商品 Ask、額度不足、非白名單、CLI 逾時／取消、重複請購、重啟／刷新、惡意字串、LLM 自稱已付款；A／B fixture E2E | fixture 包的案例與整合要求（evidence §7） | 瀏覽器 log 記錄 viewport（1280×900、390×844）、截圖並比對序號 | fixture＋瀏覽器 | A／B 冒用只在契約層；分層見第 3.1 節 | (a) |

**真實展示與主持人體驗**（沿用 Aaron 原話與已核對紀錄，不擴大成「人工清單逐項完成」）：
- 歷史回放：「以目前階段來講，算是合格的」（cli-poc §54.3.3）；技術 review `b81f749`／`231f70d`（§54.3.2）。
- 第一次真實即時展示：「目前執行過程順利，我認為現階段是達到既定標準」（cli-poc §55.11；Codex 核對紀錄 §55.11.1）。沒有截圖，建置是 `vcs.modified=true`，在開發展示例外下執行。
- `make present-live`：「以手動確認過目前的版本，大致操作順利」（cli-poc §56.9）。四個確認點沒有逐項回報，紀錄也寫明不代填（§56.9.1）。
- 3.5 的 `docs/demo-operation-design.md` §16.3 人工清單沒有逐項確認。

其他未結項目（不影響上表分類，但照實列出）：`R2 order` 測試穩定性待派工；D3～D6 未開始；Chrome 長流程的背景可見性未確認；Allow 無法用 fixture 呈現。

### 5.3 B1-07 只讀驗收器與證據包

| # | 要求（摘要） | 實作 | 證據與 review | 剩餘限制 | 分類 |
|---|---|---|---|---|---|
| 1 | 只讀輸入，沒有私鑰，不連模型或 testnet，不改證據；結果寫 stdout 或另外的路徑 | `cmd/verify-demo`、`internal/verification`；路徑不得逃出 run 目錄、只讀一般檔案 | evidence §15.1～15.4；E2／E3 與本報告 3.3 的前後 sha256 相同 | 讀取上限（檔案數與大小）列在報告的 limits | (a) |
| 2 | 與執行器分開，不以 `policy.Decide` 類函式作 oracle；以事先確認的 golden vectors 驗檢查器 | `internal/verification`（不 import policy）；`testdata/verification/` 的 golden 與 expected | evidence §15.2；`48a43bc`／`973a58d` 限定驗收（§15.7） | 共用 schema 與 canonical parsing | (a) |
| 3 | 對照輸入與請購、digest、Ask／Deny 原因、task／事件關聯、序號與 hash chain、evidence mode；有限 run 內沒有 Approved／Settled／交付 | V2～V6 | fixture 包 V1～V7 PASS（§15.7）；live 包 V1～V7 PASS（§16.11） | 只驗 M1 的紀錄與狀態約束，不能推論有 Signer 時也安全，也不能證明紀錄外沒有副作用 | (a) |
| 4 | 驗收 44 的 M1 子集：A／B registry snapshot；錯 payer、A 的事件歸到 B、fixture 餘額冒充 live | V7、V6 | 負例：錯 payer、跨 consumer、fixture 冒充 live（§15.3、§16.2） | 真實選 key、跨消費者 grant／proof、鏈上隔離維持 `not_evaluated` | (a) |
| 5 | 測檢查器本身：正例通過；預期 Deny 卻回 Allow、金額或收款人不符、假造 Settled、中段竄改、fixture 冒充 live 判 FAIL；缺 trace 或結束標記判 INCONCLUSIVE | `internal/verification` 的測試 | fixture 負例與 46 項反向驗證（§15.3、§15.6）；live 負例 25 個（§16.2、§16.11） | 尾端截斷沒有獨立 checkpoint 時可能無法辨識（已列限制） | (a) |
| 6 | 每個 claim 輸出 PASS／FAIL／INCONCLUSIVE、expected／observed 與證據引用；結束碼 0／1／2；能力列 not_evaluated | 報告格式、`cmd/verify-demo` 結束碼 0／1／2／3 | §15.1、§16.2 | — | (a) |
| 7 | 保存 commit、dirty digest、依賴／CLI／模型版本、config digest、案例版本、run ID、命令與全部結果；fake 與 live 分開驗 | `RunManifest`；`live/run.json` | 第 3.1、3.2 節；兩種模式分別判定 | live 的模型記在 run 紀錄與 trace，不在 manifest 的 `versions` | (a) |
| 8 | 從另一終端重跑：合法 Ask → reviewer 選擇受控的非白名單或超額輸入 → 合法 Ask；不改 policy | `TestM1FixtureCases`（fake host E2E） | **10/11 完成**：Codex 以 reviewer 身分選定 `deny-merchant` 並事先固定預期，Claude Code 在獨立的終端依序執行三次，結果 Ask → Deny／`MERCHANT_NOT_ALLOWED` → Ask，三次結束碼都是 0（見下方「受控挑戰紀錄」） | 只用 fake；這是凍結案例的挑戰，不是任意輸入的對抗測試；Codex 本輪尚未獨立執行 | (a) |
| 驗收 | `go test ./internal/verification/...` 加正反與缺證據 fixtures；另一終端分別對 fixture 與 live 執行 `make verify-m1` | — | §15.4、§15.6、§16.5～16.8；fixture 與 live 的報告（第 3 節） | Codex 重跑整個 `internal/verification` 時曾碰到 150 秒上限，不能記為通過（§15.7） | (a) |

**供 reviewer 選擇的兩個凍結案例**（只用 fake，不消耗模型額度；選擇與操作都由 reviewer 決定並記錄）：
- **超額**：`deny-budget`（有效剩餘額度 0.25，主商品 0.50）。可在主持人頁直接選：`make present-fake` 開啟主持人頁，依序開始情境 `ask` → `deny-budget` → `ask`。預期：Ask／pending → Deny（額度不足）→ Ask／pending；Deny 不發 grant、不扣預算。
- **非白名單**：`deny-merchant`。只在 fixture 案例集裡（`TestM1FixtureCases`），主持人頁的情境目錄（`testdata/demo/scenarios.json`）沒有這一項；若 reviewer 選這個，就是在另一終端以 分三次執行 `go test -count=1 -v -run '^TestM1FixtureCases$/^<案例>$' ./cmd/demo/`，`<案例>` 依序是 `ask`、`deny-merchant`、`ask`（子測試名稱就是案例 ID）。

**受控挑戰紀錄**（10/11；Codex 選擇案例，Claude Code 執行；Codex 本輪尚未執行這項挑戰）：
- 選定：`deny-merchant`（非白名單）。只用 fake，不啟動模型。
- 事前固定的預期（取自凍結的 `testdata/contracts/cases.json`，sha256 `429a5235…615f2c1`，沒有修改）：Ask／HUMAN_APPROVAL_REQUIRED／pending → Deny／MERCHANT_NOT_ALLOWED／not_requested → Ask；三步都不付款、不扣預算、沒有 forbidden 事件。Policy 與 expected 沒有修改。
- 基準：`03fe750`（已追蹤的檔案沒有修改；程式碼與 `4feb7df` 相同）。
- 依序執行，任一步失敗就停止、不重試：

| 步 | 命令 | 時間（UTC） | 結束碼 | 結果 |
|---|---|---|---|---|
| 1 | `go test -count=1 -v -run '^TestM1FixtureCases$/^ask$' ./cmd/demo/` | 18:26:03～18:26:05 | 0 | `TestM1FixtureCases/ask` PASS |
| 2 | `go test -count=1 -v -run '^TestM1FixtureCases$/^deny-merchant$' ./cmd/demo/` | 18:26:05～18:26:07 | 0 | `TestM1FixtureCases/deny-merchant` PASS |
| 3 | `go test -count=1 -v -run '^TestM1FixtureCases$/^ask$' ./cmd/demo/` | 18:26:07～18:26:09 | 0 | `TestM1FixtureCases/ask` PASS |

- PASS 的意思：測試把觀察到的 result、reason、approval_status、payment_status、扣款金額與 forbidden 事件，逐項和凍結的 expected 比對，並確認 Store 紀錄顯示該案例（`cmd/demo/m1_test.go` 第 374～389 行）；任何不符都會讓子測試失敗。通過時的輸出不另外印出觀察值。
- 證據：`artifacts/m1-verify/e4-challenge-20261011/`（事前預期、三次的命令、基準、完整輸出、時間與結束碼）。

B1-07 卡的 Owner 欄寫「未指派」：實際上 E2、E3 由 Claude Code 依派工實作，Codex review（第 3 節）。這只是同步現況，不影響勾選或整卡狀態。

## 6 重跑說明

**只讀核對**（不啟動模型、不付款、不改證據）：

```sh
make verify-m1 RUN_DIR=artifacts/m1/m1-fixture-20261009T162703Z-4ddb7a              # fixture 模式
make verify-m1 RUN_DIR=artifacts/m1/m1-live-20261010T173831Z-9d32ef MODE=live       # live 模式
```

- 結束碼：0 PASS、1 FAIL、2 INCONCLUSIVE、3 無法檢查。經 `make` 時非 0 會變成 make 自己的 `Error N`，要看報告的 `verdict`。
- run 目錄的名稱必須與 run ID 相同（V1 會檢查），複製或掛載時要保留原名。

**會重新執行、但不啟動模型的命令**（fake，產生新的資料或新的證據包，不覆寫舊包）：

```sh
make check                 # 編譯、靜態檢查、單元測試
make m1-fixture            # fake CLI 跑 31 案並產生新的 fixture 包（約 8 分鐘，含瀏覽器；需要 Unity Web 建置與 Chrome）
make check-m1-e3           # E3 修正輪的目標驗證（fake）
make present-fake          # 主持人頁，fake CLI
```

**會啟動真實模型的命令**（**需要 Aaron 另行核准**，不屬於預設的驗證流程）：

```sh
APA_LIVE_CLI=true make m1-live-cli CLI=<CLI 絕對路徑> BASE=<review 過的 commit> GATE=dev_demo_plugins
make present-live
```

- 會用到本人訂閱（`~/.apa-claude`）的額度；每次核准只跑一次，不自動重試。
- CLI 路徑執行前要確認：`~/.local/bin/claude` 會自動更新，可能不再是釘選的 `2.1.293`（evidence §16.10）。

## 7 限制與證據歸屬

**限制**：
- 包內的 hash 與事件鏈可以發現包內的缺失與矛盾，但**不能防止整包被一致地重造**；沒有外部錨點，不是第三方稽核，也不是模型來源的密碼學證明。
- live 是在 `dev_demo_plugins` 下取得：嚴格零 plugin 模式沒有驗證；plugin 名稱、Gate 與專用設定目錄不證明 plugin 的內容、沒有背景活動或作業系統隔離。
- live 包內 apa-buyer 的建置是 `vcs.modified=false`，但外層產包器與當時的檢查器帶 modified 標記，不能概括成「全部都是乾淨建置」。
- Ask／pending 是匯出當時的裁決與快照，不代表現在仍可核准；沒有任何核准、grant、付款或交付。
- CLI usage 是自報，不是帳單。
- fixture 證明的層級（E2E、Store、契約）、live 單筆 Ask、瀏覽器紀錄與人工目視判定各自不同，不相互替代。

**證據歸屬**：
- 產包、測試、反向驗證、真實執行與本報告的複查，都由 Claude Code 執行。
- Codex 的獨立執行與核對寫在各節的驗收紀錄：evidence §14.1、§15.7、§16.9、§16.11。Codex 的輸出只在它的工具紀錄。
- 人工的展示判定是 Aaron 的原話（第 5.2 節）。

## 8 不算入 M1 的項目

以下是後續商業展示版或第二期的要求，不列為 M1 的新增功能，也不因 M1 結案而宣稱已完成：
- 多 Agent 同時購物、最多 20 個付款帳號、Admin 營運數據；
- 真實核准與 grant、BBS+、ZK、Signer、鏈上付款、委託與語音；
- 真實 testnet 錢包與資產隔離（驗收 44 的付款階段）。

## 9 結案建議（候選階段；已由第 10 節取代）

候選階段的建議是：B1-04、B1-06 可結案；B1-07 在受控挑戰經最後核對後結案；三張卡結案後 M1 符合「B1-00～B1-07 全部通過」的門檻。這些建議已依第 10 節的最後核對結論執行。

## 10 結案紀錄（2026-10-11）

> M1-E4 最後核對通過。B1-07 的受控挑戰缺口關閉；B1-04、B1-06、B1-07 的 M1 驗收要求已依既有證據及明確接受的範圍調整完成，M1 結案。

**狀態**：B1-04、B1-06、B1-07 與 M1 為 **Done（2026-10-11）**；B1-00～B1-03、B1-05 沿用原有的 Done。

**受控挑戰的最後核對**（B1-07，驗收 43）：
- Codex 以 reviewer 身分選定 `deny-merchant`，事前固定預期為 Ask／HUMAN_APPROVAL_REQUIRED → Deny／MERCHANT_NOT_ALLOWED → Ask。
- Claude Code 在 `03fe750` 依序各執行一次三個定向 fake 測試；指定的子測試確實執行，三次結束碼都是 0。
- 測試逐項比對裁決、原因、核准狀態、付款狀態、扣款與禁止事件，也核對 Store 匯出；通過時的 log 沒有另外列出所有觀察值，本報告不宣稱保存了這些原始觀察值。
- Codex 核對命令、輸出、結束碼、凍結案例的 SHA-256、測試斷言與 Git 差異；沒有重跑挑戰或真實 CLI。Codex 這次核對只有對話工具紀錄，repo 沒有對應的 log。
- 執行基準 `03fe750` 的相關程式與已 review 的 `4feb7df` 相同；`d609652` 只有文件變更。
- 證據：`artifacts/m1-verify/e4-challenge-20261011/`（事前預期與三次執行紀錄）。

**已接受的範圍調整與語意對照**（結案時保留，不抹除原要求）：
- live 證據在 `dev_demo_plugins` 下取得（Aaron 10/10 決定，`f0adfb3`）；
- trace 以白名單保存，不留原始 stream-json（B1-04 第 3 項）；
- 畫面的來源標示是「合成資料・不付款」與真實 Agent／開發展示例外的說明（B1-06 第 6 項）；
- 乾淨目錄補驗以既有的 APA WebGL 建置與本機快取為已揭露的前置條件（B1-06 第 10 項）；
- 任務卡的 `QuoteReceived` 依既有的事件對照以 `PURCHASE_PROPOSED` 呈現，沒有新增事件（B1-06 第 2 項）。

**證據歸屬**：E4 前輪的 187 個檔案雜湊核對，以及 E1～E3 的產包、測試、真實執行與 review，各自保留原基準與歸屬（第 3 節、`docs/m1-evidence.md` §14、§15.7、§16.9～16.11），不改掛到本次的文件 commit，也不能概括成 Codex 重跑了全部驗證。

**結案不包含的事**：
- 3.5 的人工清單（`docs/demo-operation-design.md` §16.3）沒有逐項確認，不因結案而算完成；
- D3～D6、`R2 order` 測試穩定性與其他後續工作不一併結案；
- 第 7 節的限制全部保留：嚴格零 plugin 模式未驗證、部分歷史建置帶 modified 標記、Ask／pending 是歷史快照且沒有任何核准或付款、CLI 用量是自報、證據沒有外部錨點；
- 第 8 節的後續項目（多 Agent、20 個付款帳號、Admin、真實核准與 grant、BBS+、ZK、Signer、鏈上付款、委託與語音）屬商業展示版，沒有開工，也不提前標完成。

M1 完成僅代表真實 Agent 請購、確定性 Policy 裁決、展示與證據查核（沿用已接受的開發展示例外）；**不等於第一期 PRD 或商業展示版完成**。下一個切片待另行派工。
