# M1 收尾差距盤點（短清單）

> 2026-10-09；Claude Code 只讀核對，待 Codex review。基準 HEAD `7c853d4`。
>
> **修訂（依 Codex review 的 R1、R2）**：第 3 節改寫，新增 3.1、3.2 兩張表；第 4 節的 E1 改為沿用既有契約。原版內容見 git `2b4dcb7`。
>
> 本文件只對照文件與 repo 內容，**本輪沒有執行任何測試、CLI 或展示**。
>
> 依據：
> - vault 任務清單 §1（M1 完成門檻）、§4 B1-04／B1-06／B1-07 任務卡、§6 統一驗收入口；
> - 執行計畫 §5 驗收 41–44；
> - repo `docs/cli-poc.md` 第 53～56 節、`docs/commercial-demo-roadmap.md`。

## 現行摘要（10/11，M1-E4；以本節為準）

E1～E3 都已驗收，E4 的結案候選報告在 [m1-result.md](m1-result.md)，待 Codex／Aaron 最終確認。下面第 0～6 節是 **10/9 盤點當時的狀態**，加上當時陸續的現況註記，原文保留作為歷史；其中「B1-07 未實作」「E2 未實作」「E3 未執行」「`docs/m1-result.md` 不存在」等說法都已過時。

| 出口 | 現況 | 依據 |
|---|---|---|
| E1 `make m1-fixture` 與 fixture 包 | 限定驗收通過（`0d13255`／`0853b44`；`m1-fixture-20261009T162703Z-4ddb7a`） | `m1-evidence.md` §14 |
| E2 B1-07 檢查器 fixture 模式 | 限定技術複核通過（`48a43bc`／`973a58d`） | §15.7 |
| E3 產包入口、live 檢查器與進度工具 | 限定技術 review 通過（`c11138a`／`4feb7df`） | §16.9 |
| E3 一次真實 live 單筆 Ask | 通過 Codex 限定核對（`m1-live-20261010T173831Z-9d32ef`） | §16.10、§16.11 |
| 乾淨目錄依 README 完成 fixture E2E | E4 補驗：新 clone、`make m1-fixture` 結束碼 0、31 案 PASS、檢查器 PASS；用了本機快取與既有 Unity Web 建置副本 | `m1-result.md` §3.4 |
| `tests/m1/` 的案例對照 | `tests/m1/README.md` | — |
| `docs/m1-result.md` | 已建立（結案候選） | — |
| 任務卡逐項核對 | 已列建議，待 reviewer 判定 | `m1-result.md` §5 |
| B1-07 reviewer 選擇現場挑戰 | **沒有紀錄，待 reviewer 選擇與操作** | `m1-result.md` §5.3 |

B1-04／B1-06／B1-07 仍是 InProgress，本文件不改任務狀態。

## 0 結論（10/9 盤點當時；現況見上方「現行摘要」）

- **M1 完成門檻**：B1-00～B1-07 全部通過。B1-06 產出執行證據，再由 B1-07 只讀核對；任何必要 claim 為 FAIL 或 INCONCLUSIVE，都不能結案。
- **目前狀態**：
  - B1-00～B1-03、B1-05 已 Done。
  - B1-04、B1-06 的能力大多已做出並經限定 review，但都**還沒有依任務卡的形式交出出口證據**。
  - **B1-07 完全未實作**，所以 M1 目前不能結案。**10/10 更新**：fixture 子範圍已實作（M1-E2，`make verify-m1`），並通過限定技術複核（`48a43bc`／`973a58d`）；live 包驗收尚未完成，M1 仍不能結案。
- **最短路徑**：
  1. 沿用既有的證據契約（`RunManifest` 等），補齊證據檔的內容與產生流程，以 `make m1-fixture` 產生 fixture 證據（B1-06 的出口）。**10/10 已通過限定驗收**（M1-E1，見第 4 節）；
  2. B1-07 只讀檢查器 `make verify-m1`；
  3. 一次 `make m1-live-cli` 真實證據包，並以 B1-07 驗證；
  4. `docs/m1-result.md` 與正式結案。

## 1 對照 M1 門檻（10/9 當時的狀態）

| 任務 | 判定 | 依據與缺口 |
|---|---|---|
| B1-00～B1-03、B1-05 | **已完成** | 任務清單第 3 節為 Done（Aaron 或 Codex 驗收）。 |
| B1-04 真實 Buyer wrapper | **能力已完成，缺證據形式** | 真實 CLI → wrapper → MCP → Ask → Store 已走通三次（第 53、55.11、56.9 節，Codex 核對紀錄）；live 開關、版本釘選、init Gate、fake runner 的錯誤案例都有測試與 review。缺：任務卡勾選項未逐項核對；live trace 不是以 B1-06 定義的 `make m1-live-cli` 證據包產生，也沒有經 B1-07 核對。 |
| B1-06 展示層與整合 | **展示能力已完成，缺 M1 出口** | 已完成：Unity＋LimeZu 商店、Dashboard、Inspector／Timeline、主持人 HTTP、Store 接線、A／B 隔離、取消與重啟、歷史回放、單一真實即時展示、啟動簡化（D1、D2、3.x、第 54～56 節）。**10/10 更新**：`make m1-fixture` 與 fixture 證據包（`artifacts/m1/<run-id>/`）已實作，並通過限定驗收（M1-E1，見第 4 節）。**未實作**：`make m1-live-cli`、`tests/m1/`、`docs/m1-result.md`。**缺證據**：在沒有 sibling repo 的乾淨目錄依 README 完成 fixture E2E。 |
| B1-07 只讀驗收器 | **未實作**（10/10 更新：fixture 子範圍已實作並通過限定技術複核，live 未接，見第 4 節） | `cmd/verify-demo/`、`internal/verification/`、`testdata/verification/`、`make verify-m1` 都不存在。Owner 未指派。 |
| S1～S3、S4-B | 不列入 M1 門檻 | 依任務清單分列；S4-A 已 Accepted。 |

## 2 B1-04 與 B1-06 的必要出口（10/9 當時列出；完成情形見「現行摘要」）

**B1-04**
1. 逐項核對任務卡的 8 個勾選項並附證據位置；大多已有 review 紀錄，交 Codex 判定能否勾選。
2. 以 `make m1-live-cli`（下面的 B1-06 第 2 項）跑出一次有上限的真實請購證據包，並經 `make verify-m1` 核對。
3. **需要 Aaron 決定**：M1 的 live 證據是否接受「開發展示 plugin 例外」下的結果。嚴格零 plugin 模式在 Aaron 本機至今沒有通過（決策稿 9.1）；這是範圍決定，不是工程缺陷。Codex 建議接受它作為 M1 開發展示的 live 證據條件，保留 `gate_mode=dev_demo_plugins`、剩餘風險與「嚴格模式未驗證」的註記，不再把清空 plugins 拉回主線。**這仍需要 Aaron 明確決定，Codex 沒有代為核准。**
   - **10/10 Aaron 決定：接受。** M1 的 live 證據可以是開發展示 plugin 例外（`gate_mode=dev_demo_plugins`）下的結果，條件如下：
     - live 包必須記錄 `gate_mode=dev_demo_plugins`；
     - 明列「嚴格零 plugin 模式未驗證」與剩餘風險；
     - 不再把清空 plugins 拉回主線。
   - 這是 M1 範圍的決定。它不改 Gate、認證清單、版本釘選或 plugin 設定，也不代表已核准執行 E3：真實 CLI 的執行仍需另行派工，並由 Aaron 另行核准。

**B1-06**
1. `make m1-fixture`：用 fake CLI 跑凍結的案例集，產生 `artifacts/m1/<run-id>/`，內容包含：
   - 程式 commit 與 dirty patch digest、依賴／CLI／模型版本、非敏感設定 digest；
   - case manifest 版本（`testdata/contracts/cases.json` 的 digest）、命令與 exit code；
   - 遮罩後的事件、每個案例的結果、結束標記。
   - 不含 token、配對碼或 CLI 原文。
2. `make m1-live-cli`：明確 opt-in 才跑一個有上限的真實任務，輸出相同格式的證據包（可重用 `present.sh` 或 `live.sh` 的路徑，不另寫 runner）。
3. `tests/m1/` 的案例對照任務卡的必測清單：
   - 主商品 Ask（`ask`）、額度不足 Deny（`deny-budget`）、非白名單 Deny（`deny-merchant`／`deny-asset`）；
   - CLI timeout／cancel（`cli-timeout`、`cancel-*`）、重複請購（`retry-*`）、重啟／刷新（`restart`）；
   - 惡意字串、LLM 自稱已付款（`no-premature-settlement`）、A／B 冒用（`cross-consumer*`、`altered-*`）。

   多數已有 Go 或瀏覽器測試，缺的是**以同一個 run ID 匯出成證據包**。
4. `docs/m1-result.md`：只放可公開的摘要與證據索引，並說明 hash／manifest 無法防止作者重製整包。
5. 乾淨目錄的 README 啟動與 fixture E2E 證據。

## 3 B1-07 的最小範圍

**輸入**：`artifacts/m1/<run-id>/` 的 `RunManifest`（既有契約，見 3.3）與凍結的 `testdata/contracts/cases.json`，只讀。
- 不呼叫 `policy.Decide` 等執行器函式作為判準；可以共用 schema 與 canonical parsing；不連模型或 testnet，也不改寫輸入。
- 要防 symlink 逃出 run 目錄、驗證每個證據檔的 bytes digest，並對照事前的 expected outcome。
- 既有的 `RunManifest.ValidateAgainst` 只檢查索引（案例覆蓋與 capability 規則），**不能取代 B1-07**。

**必要 claim**（每項輸出 PASS／FAIL／INCONCLUSIVE、expected／observed 與證據引用）：

| # | claim | FAIL 的例子 | INCONCLUSIVE 的例子 |
|---|---|---|---|
| V1 | 證據包有來源資訊（commit、dirty digest、config digest、case manifest digest、版本、run ID、命令與 exit code），證據檔 digest 相符 | digest 不符、路徑逃出 run 目錄 | 缺少任一項 |
| V2 | **依該案例的 expected 判定**（3.1）：該有請購的，請購對得上輸入（金額、收款人、asset、requirement／intent digest）；預期在入口或請購前結束的，**必須沒有請購或沒有落帳** | 金額或收款人不符；不該有請購卻有 | 該有請購的案例缺少請購資料 |
| V3 | result、reason、approval_status 等於凍結的 expected；`payment_status=unavailable`、`budget_deducted="0"`（不扣預算） | 預期 Deny 卻出現 Allow 或 Ask；預算被扣 | 缺少可判定的證據 |
| V4 | 事件序號連續、hash chain 相連、綁定同一 task | 中段被竄改 | 缺少結束標記或 trace |
| V5 | 每個案例的 `forbidden_events`（提前核准、grant、付款、交付）都沒有出現；不發 grant | 出現假造的 Settled 或 grant | — |
| V6 | evidence mode 與執行層一致：fixture 不冒充 live；契約層證據不冒充端到端證據（3.1 的「執行層」） | fixture 標成 live；以契約測試當成 HTTP 或 UI 證據 | live 證據缺失 |
| V7 | A／B registry snapshot：consumer、agent、task、wallet、from 一致 | 錯誤的 payer、A 的事件歸到 B | — |

**正確拒絕且沒有落帳，可以 PASS**：不能因為沒有請購就一律判為證據不足。必要證據是「入口拒絕的結果碼」加上「Store 沒有對應的請購或事件」（3.1）。

### 3.1 案例對照（case → 執行層 → 預期 → 必要證據）

「執行層」是 claim 可以宣稱的最高層級：
- **E2E**：fake CLI → wrapper → MCP／toolbridge → Store／Policy → 主持人 HTTP；
- **Store**：Store 與 host 的整合測試；
- **契約**：`internal/contracts` 的測試。

證據只能宣稱它實際所在的層級。

| 案例 | 執行層 | 預期 | 必要證據 |
|---|---|---|---|
| `ask` | E2E | Ask／`HUMAN_APPROVAL_REQUIRED`／pending | 請購對得上輸入；事件 #1～#3；未扣預算；沒有 forbidden 事件 |
| `deny-budget`、`deny-merchant`、`deny-asset` | E2E（host 以可信 plan／目錄設定預算、商家或資產） | Deny 與各自的 reason | 請購與裁決事件；沒有核准、grant 或付款；未扣預算 |
| `deny-expired`、`deny-missing-config`、`deny-unattended`、`allow-unavailable` | 需要可信設定的變體（過期憑證、缺設定、無核准者、允許額度）。host 有對應的設定入口就走 E2E，**沒有就以 Store 層證據並標示 Store**，由 E1 決定並列明 | Deny／Allow 與各自的 reason；Allow 是 `PAYMENT_CAPABILITY_UNAVAILABLE`、不付款 | 同上；Allow 另需「沒有 grant 或付款」 |
| `cancel-pending` | E2E（主持人取消） | cancelled／`HOST_CANCELLED`；approval withdrawn | `APPROVAL_WITHDRAWN`、`TASK_CANCELLED`；Ask 不被改成核准 |
| `cancel-no-purchase` | E2E | cancelled，**沒有請購** | `TASK_CANCELLED`，Store 沒有請購 |
| `cli-timeout` | E2E（fake CLI 卡住） | failed／`CLI_TIMEOUT`，**請購前結束** | `TASK_TIMED_OUT`，沒有請購 |
| `cli-timeout-pending` | E2E | failed／`CLI_TIMEOUT`；既有的 Ask 保持 pending | `TASK_TIMED_OUT`，Ask 的紀錄不變 |
| `approval-timeout`、`approval-deadline-before`、`approval-deadline-exact` | Store（可控時鐘的到期處理）；`approval-timeout` 另可以真實時間跑 E2E | denied／pending 與各自的 reason | `APPROVAL_EXPIRED` 恰好在期限發生（或尚未發生） |
| `restart` | E2E（重啟 host，同一位消費者開新 task） | same-wallet／`PERSISTED_BINDING` | 兩個 task 的 wallet／from 相同；重啟前後的資料庫 |
| `retry-same`、`retry-conflict`、`retry-new-request-same-order`、`retry-same-request-id-a-b` | 契約（去重規則）；`retry-same` 另有 toolbridge 的重複請購 | replay／reject／new | 契約測試結果；在 E2E 宣稱時，要另有「沒有第二筆請購或事件」的證據 |
| `consumer-a`、`consumer-b`、`cross-consumer*`、`altered-*` | 契約（綁定規則）；入口拒絕另有 wrapper 與 HTTP 的測試 | accept／reject（`OWNERSHIP_MISMATCH`） | 契約測試結果；E2E 宣稱另需「入口拒絕、Store 沒有落帳」 |
| `no-premature-settlement`、`reject-erc7710-quote`、`reject-permit2-quote`、`reject-case-variant-key` | 契約（裁決與請購的驗證） | reject 與各自的 reason | 契約測試結果；**不能**當成 UI 不顯示付款成功的證據（見 3.2） |

### 3.2 整合要求（不是案例 ID，要對應實際的測試）

任務卡 B1-06 的必測項目中，以下各項**不能只套一個相近的 case ID**，要對應實際的測試輸出：

| 要求 | 對應的既有測試（E1 匯出時列入證據） |
|---|---|
| 頁面重新整理後由 Store 還原 | `browser-ui.mjs`「refresh + sign-in restores task-a from the Store」 |
| 惡意字串只當文字顯示 | `adapter.test.mjs`「hostile strings stay strings; nothing reads as paid」、`browser-ui.mjs`「hostile strings render as text only」 |
| LLM 自稱已付款，畫面仍不顯示成功 | `browser-ui.mjs`「nothing says paid or delivered」、`TestLiveRun`（trace 回覆不含模型文字） |
| 事件重送不重複動畫 | `adapter.test.mjs`「duplicate events are shown once and animated once」 |
| A／B 切換不串資料 | `adapter.test.mjs`「a switch drops every late reply of the old viewer」、`TestIntegrationAskDenyAndAccess` |
| 取消與 CLI 逾時顯示獨立狀態 | `TestCancel*`、`TestCancelAfterCLITimeout`、`browser-ui.mjs` 的 3.4 段 |

### 3.3 fixture 證據包與 live 證據包分開定義

- **fixture 完整案例包**（`make m1-fixture`）：
  - 31 個 case claim 每個恰好出現一次，在 results（PASS／FAIL／INCONCLUSIVE）或 not_evaluated；
  - 七個 capability（`credential_verification`、`webauthn`、`voice_approval`、`zk_budget_proof`、`signer_key_selection`、`cross_consumer_grant_proof`、`onchain_settlement`）全部列為 not_evaluated；
  - 每個 claim 的證據都標明執行層（3.1）。
- **live 單筆 Ask 包**（`make m1-live-cli`）：
  - 只有 `ask` 一個 case claim 必須有結果，其餘 30 個列為 not_evaluated；
  - 必須含 `gate_mode`（M1 預期是 `dev_demo_plugins`，10/10 經 Aaron 決定接受；要明列「嚴格模式未驗證」與剩餘風險）、CLI 版本、模型、usage 為自報或 unknown；
  - **不得稱為全案例通過**。
- 這兩個包分別以 `make verify-m1` 驗證；fixture 包通過不能掩蓋 live 包缺少證據。

## 4 建議的下一個切片（10/9 當時；E1～E3 已完成，E4 見 m1-result.md）

**M1-E1：沿用既有證據契約，補齊證據檔內容並做 `make m1-fixture`**（B1-06 出口的第 1、3 項）。

> **現況（10/10）**：**M1-E1 通過限定範圍技術複核**（程式 `0d13255`、測試與產包基準 `0853b44`；`docs/m1-evidence.md` 第 14 節）。
> - R1-1（實際案例輸入綁定）、R1-2（必要設定快照）關閉；R2（證據保存失敗）維持前輪關閉。
> - claim 以可只靠證據包重算的 Store／契約結構化紀錄為依據，測試 log 只作輔助；任何保存或來源查詢失敗都標為不完整（結束碼 2）。
> - 3.1 的實際分層：E2E 13 案、只到 Store 層 3 案、契約層 15 案。與本節原表的差異列在該文件第 4 節。
> - 這是 fixture 產生端的驗收，不代替 B1-07，也不代表 M1、B1-04 或 B1-06 整體完成。
> - 下一步：E2（B1-07 獨立證據檢查器）待另行派工；E3、E4 的要求不變（見下方「之後的順序」）。
> - 10/9 的現況（「待 Codex 限定複核」）由本段取代。

**不另造格式**。既有契約（`internal/contracts/evidence.go` 的 `RunManifest`、`EvidenceRef`、`ClaimResult`、`CommandResult`；`docs/contracts.md`「Claim 分兩類」一節）已經定義：
- run ID、code commit、dirty patch digest、config digest、case manifest digest、工具版本、命令與 exit code；
- 證據索引（相對路徑、原始 bytes 的 `sha256:` digest、evidence mode、source）；
- 每個 case 的 PASS／FAIL／INCONCLUSIVE 與 not_evaluated、七個必須列為 not_evaluated 的 capability。

**E1 要補的部分**（逐項列明。若需要改動契約欄位，回報最小差異與理由，不另開平行格式）：
1. **證據檔內容**：每個 task 的遮罩事件匯出（Store 的事件與請購白名單投影）、run trace 白名單、host_runs 紀錄、各層測試輸出（只取對應的案例），以及**結束標記**。
2. **執行層標示**：用既有的 `EvidenceRef.source` 定一組封閉詞彙（例如 `host_e2e`、`store_integration`、`contracts_test`、`browser_ui`、`live_cli`）。如果現有欄位不夠用，列為契約擴充並說明理由。
3. **「不扣預算」與「沒有 forbidden 事件」的證據**：例如任務預算的前後快照。
4. **產生流程**：`make m1-fixture` 以 fake CLI 跑 3.1 的 E2E 案例，並收進 Store 層與契約層的測試結果；每次一個新的 run 目錄；不含 token、配對碼或 CLI 原文。
5. **3.2 的整合要求**：把對應的測試結果一併列為證據。

**驗收**：
- 以既有的 `ValidateAgainst` 通過索引檢查（**只是索引，不是 B1-07**）；
- Go 測試：同一份輸入跑兩次，除了時間與 ID 以外內容一致；
- 每個案例的層級與 3.1 一致；
- 證據包中掃不到 token 或配對碼；
- 對遮罩、結束標記與層級標示做反向驗證；
- 交 Codex 限定 review。

**之後的順序**：
1. M1-E2：B1-07 檢查器（第 3 節）。E1 與 E2 維持分開，先把產出格式與案例對照做紮實。
   - **現況（10/10）**：fixture 子範圍已實作——`cmd/verify-demo`、`internal/verification`、`testdata/verification`、`make verify-m1 RUN_DIR=...`。對 E1 驗收包唯讀判定為 PASS（31 案、V1～V7 與 3.2 要求），38 項反向驗證都以斷言抓到（`docs/m1-evidence.md` 第 15 節）。
   - **10/10 通過限定技術複核**（fixture 子範圍；程式 `48a43bc`、測試 `973a58d`；`docs/m1-evidence.md` 第 15.6、15.7 節）。B1-07 整體仍是 InProgress；live 包（3.3）在 E3 接上，目前檢查器對 live 回報 INCONCLUSIVE（不支援）；E3 待另行派工。
2. M1-E3：`make m1-live-cli` 一次真實的 live 單筆 Ask 包（3.3），需 Aaron 另行核准。
   - **現況（10/10）**：產包入口 `cmd/m1-live` 與檢查器的 live 模式（`make verify-m1 MODE=live`）已實作，只做了 fake 驗證（`docs/m1-evidence.md` 第 16 節），**實作待 Codex review**。
   - 真實執行：待 review 通過後由 Aaron 另行核准一次；至今沒有執行任何真實 CLI。
3. M1-E4：`docs/m1-result.md`、乾淨目錄 E2E、逐卡結案。

## 5 不算入 M1 的項目（後續商業展示的要求）

以下保留為商業展示版或第二期的要求，**不混算成 M1 已完成或 M1 的缺口**：
- 多 Agent 同時購物；
- 最多 20 個付款帳號；
- Admin 即時活動與累積數據；
- 真實核准與 grant；
- BBS+ 憑證、真 ZK（gnark）；
- Signer、鏈上預留與結算（兩筆 tx）、商店交付；
- 真實 testnet 錢包與資產隔離（計畫驗收 44 的付款階段）；
- 委託介面與語音。

M1 完成也不等於第一期 PRD 驗收完成。

## 6 本盤點的限制

- 只讀文件與目錄結構，沒有執行測試；第 1 節「已完成」的判定沿用既有的 review 與驗收紀錄。
- 任務卡的勾選項由 Codex 或 Aaron 判定；本文件只提供對照與建議，不改任何任務狀態。B1-04／B1-06 維持 InProgress，B1-07 維持 Waiting。
