# M1 fixture 證據包（M1-E1，`make m1-fixture`）

> 2026-10-09；依 `docs/m1-closeout-gaps.md` 第 4 節的 E1。只用 fake CLI：不執行模型、不連 testnet、不付款。
>
> **10/9 修訂（Codex review R1、R2）**：
> - R1：每個 claim 改以「可只靠證據包重算」的結構化紀錄為依據（第 5 節），測試 log 只作輔助；
> - R2：任何保存或來源查詢失敗，都以結束碼 2 表示證據包不完整，不印完成結論（第 1、8 節）。
> - 第 11 節是修訂前的交付紀錄，原樣保留；修訂後的交付見第 12 節。
>
> **這是產出端，不是 B1-07**。包內的 PASS／FAIL／INCONCLUSIVE 是產生程式自己的判定；`RunManifest.ValidateAgainst` 只檢查索引。獨立核對由 B1-07（`make verify-m1`，M1-E2）負責。

## 1 用法

```sh
make m1-fixture                                   # 含 browser-ui（需要 Chrome，約 8 分鐘）
make m1-fixture M1_FIXTURE_FLAGS=-skip-browser    # 不跑瀏覽器；3.2 的瀏覽器檢查記為 not_run，結束碼 1
```

- 從 repo 根目錄執行，每次建立新的 `artifacts/m1/<run-id>/`（0700）。不會覆寫或重用舊目錄。
- run ID 是 `m1-fixture-<UTC 時間>-<6 個隨機 hex>`。
- 結束碼（`go run ./cmd/m1-fixture` 的結束碼；經 `make` 時，非 0 一律變成 make 自己的 `Error N` 與結束碼 2，N 是本程式的結束碼）：
  - 0：證據包完整，每個命令的 exit code 都是 0，31 個案例都 PASS，3.2 的檢查全部 pass；
  - 1：證據包完整，但其中有失敗（案例 FAIL／INCONCLUSIVE、命令非 0 或 3.2 未全過）；
  - 2：**證據包不完整**：目錄、證據檔、manifest 或結束標記寫入失敗，或 commit、diff、工作區狀態、go／node 版本的查詢失敗，或掃到疑似 token、索引不通過。
- 結束碼 2 時：
  - 不印完成摘要；
  - stderr 寫明 `INCOMPLETE` 與原因；
  - 盡量寫出 `run-end.json`（state 見第 8 節）。連它都寫不出時，stderr 另外寫明，結束碼仍是 2；
  - 已寫出的資料保留，不刪除，也不自動重跑。
- 來源查詢失敗不會被當成「沒有差異」或「乾淨工作樹」：空的結果只在查詢本身成功時才採用。

## 2 沿用的契約

格式沿用 `internal/contracts/evidence.go`，沒有新增或修改欄位：
- `manifest.json` 是一份 `RunManifest`，記錄：
  - `run_id`、`code_commit`；
  - `dirty_patch_digest`：`git diff HEAD --binary` 的 sha256，**不含 untracked 檔**，untracked 檔另見 `provenance/git-status.txt`；
  - `config_digest`：第 3 節所列設定檔逐檔 digest 再合成，逐檔值見 `provenance/config-files.json`；
  - `case_manifest_digest`：`CaseManifest.Hash()`；
  - `versions`：go、node、os、case manifest ID、fake CLI；
  - `commands`：每個命令與它的 exit code；
  - `evidence`、`results`、`not_evaluated`。
- `evidence[]` 每一筆是 `EvidenceRef`：
  - 相對路徑；
  - 原始 bytes 的 `sha256:` digest；
  - `evidence_mode` 一律是 `fixture`；
  - `source` 標示執行層（第 4 節）。
- `results[]` 是 31 個案例各一筆 `ClaimResult`。七個 capability 全部列在 `not_evaluated`。
- 寫出之前會先通過 `ValidateAgainst`；不通過就不寫 manifest，`run-end.json` 的 state 記為 `index_invalid`。

## 3 目錄內容

| 路徑 | 內容 | source |
|---|---|---|
| `provenance/git-status.txt` | `git status --porcelain` | `provenance` |
| `provenance/config-files.json` | 基礎設定檔逐檔 digest：`testdata/contracts/cases.json`、`testdata/contracts/registry.json`、`testdata/contracts/purchase-input.json`、`testdata/demo/registry.json`、`testdata/policy/m1-demo-profile.json`、`testdata/cli/ask-mainline.json`。每個 task 實際用的設定（含各案例的變體）在它的結構化紀錄裡（第 5 節） | `provenance` |
| `contracts/<case_id>.json` | 每個契約案例的結構化紀錄（第 5.3 節） | `contracts_test` |
| `store/<case_id>.json` | 每個整合案例的 Store 層結構化紀錄（第 5.2 節） | `store_integration` |
| `tests/contracts.jsonl` | `go test -json` 的 `TestContractCasesFromManifest`（15 個契約案例） | `contracts_test` |
| `tests/policy.jsonl` | `TestAcceptedPolicyCases`（policy 類案例的裁決） | `policy_test` |
| `tests/store.jsonl` | `TestAcceptedIntegrationCases`（16 個整合案例，Store 層） | `store_integration` |
| `tests/e2e.jsonl` | `cmd/demo` 的 `TestM1FixtureCases`（13 個 E2E 案例） | `host_e2e` |
| `e2e/<case_id>.json` | 每個 E2E 案例的證據檔（第 5.1 節） | `host_e2e` |
| `tests/requirements-go.jsonl` | `TestIntegrationAskDenyAndAccess`、`TestCancel*`、`TestLiveRun*` | `host_e2e` |
| `tests/requirements-web.tap` | `node --test web/test/adapter.test.mjs` | `web_adapter_test` |
| `browser/` | `browser-ui.mjs` 的輸出（`browser-ui.out`、`browser-test.log`、截圖） | `browser_ui` |
| `requirements/index.json` | 3.2 整合要求對應的測試與狀態（第 7 節） | `requirements_index` |
| `manifest.json` | `RunManifest` | （不列入自己的索引） |
| `run-end.json` | 結束標記（第 8 節），最後寫出 | （不列入索引） |

`source` 是封閉清單（`cmd/m1-fixture` 的 `sources`）：`provenance`、`contracts_test`、`policy_test`、`store_integration`、`host_e2e`、`web_adapter_test`、`browser_ui`、`requirements_index`。`live_cli` 保留給 M1-E3。
- run 目錄裡如果有清單以外的檔案，或有 symlink，就不產生證據包。

## 4 案例與執行層（對照 `docs/m1-closeout-gaps.md` 3.1）

每個 claim 只引用它實際執行到的最高層級的證據。

| 層級 | 案例 | claim 引用的證據 |
|---|---|---|
| `host_e2e`（13） | `ask`、`deny-budget`、`deny-merchant`、`deny-asset`、`deny-unattended`、`cancel-pending`、`cancel-no-purchase`、`cli-timeout`、`cli-timeout-pending`、`approval-timeout`、`approval-deadline-before`、`approval-deadline-exact`、`restart` | `e2e/<case>.json`、`store/<case>.json`；輔助：`tests/e2e.jsonl`、`tests/store.jsonl` |
| `store_integration`（3） | `deny-expired`、`deny-missing-config`、`allow-unavailable` | `store/<case>.json`；輔助：`tests/store.jsonl`、`tests/policy.jsonl` |
| `contracts_test`（15） | `retry-*`、`consumer-*`、`cross-consumer*`、`altered-*`、`no-premature-settlement`、`reject-*` | `contracts/<case>.json`；輔助：`tests/contracts.jsonl`。**不當成端到端證據** |

E2E 的路徑是：
- host 在測試程序內啟動，fake CLI 是子程序；
- 經過 wrapper、MCP／toolbridge、Store／Policy；
- 再經主持人以 token 驗證的 HTTP API 開始、停止、取消，並讀回結果。
- 預期值只取自 `cases.json`。

與 3.1 的差異（E1 決定並列明）：
- **`deny-merchant`、`deny-asset`、`deny-unattended`**：
  - host 以可信的 `--profile` 設定，因此走 E2E。測試只改 `cases.json` 參數所指的欄位：
    - `deny-merchant`：profile 的 merchant 名單不含商家；
    - `deny-unattended`：profile 的 mode 改為 unattended；
    - `deny-asset`：另以可信的 `--registry` 只列另一個 asset，profile 也指向該 asset，因此商家報價的 asset 不受信任。只改 profile 的 asset_id 不行：Store 會以 `INVALID_TASK_SETUP` 拒絕 registry 沒列的 profile asset，task 根本不會建立；
  - 這些設定由測試從第 3 節的設定檔衍生。實際傳給 host 的檔案內容存在 E2E 證據檔的 `host_settings`；Store 實際採用的 registry／profile／credential／terms 存在每個 task 的快照（第 5 節）；
  - `deny-unattended` 在 3.1 原列為「有設定入口才走 E2E」，host 確實有這個入口。
- **`deny-expired`、`deny-missing-config`**：host 沒有對應的可信設定入口（過期憑證、缺少設定），所以只到 Store 層。
- **`allow-unavailable`**：
  - 案例的參數是金額 50000、上限 100000；
  - host 唯一的商品是 500000；
  - 改上限來湊出 Allow，就不是同一個案例，所以只到 Store 層。
- **`approval-timeout`、`approval-deadline-before`、`approval-deadline-exact`**：
  - 3.1 列為 Store 層（`approval-timeout` 可另走 E2E）。這裡三個都走 E2E；
  - host 的時鐘是測試注入的**可控時鐘**：Ask 之後設定為 `decided_at + advance_seconds`，之後就固定不動，到期處理每 20 ms 執行一次；
  - 這是 host 的到期處理經 HTTP 的結果，**不是真實時間的等待**。

判定（每個 claim 引用的每一份結構化紀錄都要重算，第 5.4 節）：
- **PASS**：每份紀錄重算都沒有問題、與 expected 相符，對應的測試也都通過。E2E 案例另需 E2E 測試自己記錄 PASS。
- **INCONCLUSIVE**：缺少紀錄、讀不了、layer 標示不符，或測試沒跑或略過。
- **FAIL**：重算發現問題（digest、綁定、事件鏈、與 expected 不符等），或測試失敗。
- **測試 log 寫 PASS 不能補足缺少或有問題的紀錄**。log 只能讓 claim 失敗，不能單獨支撐 claim。

## 5 結構化紀錄

紀錄由產生端的測試在既有邊界取得。沒有新增 HTTP API，也沒有改 Policy、Store 或事件契約。
- **Store 紀錄**：`internal/m1evidence.Export` 在 Store 寫完後，以唯讀方式讀資料庫，**照原樣**匯出每個 task：
  - task 列：consumer、agent、wallet、payer、status、deny_reason、budget_remaining；
  - 它啟動時的 registry、profile、credential、terms 快照：原文與快照 hash；
  - 每筆請購的原文（完整 `PurchaseOrder`：intent、quote、selected requirement、extensions、nonce、有效期間）與存的 intent hash；
  - 每個事件的原文（完整 `AuditEvent`：版本、scope、payload、序號、prev_event_hash）與存的 event hash。
- 原文以字串保存，不在 hash 計算後改動任何欄位。

### 5.1 E2E 證據檔（`e2e/<case_id>.json`）

| 欄位 | 內容 |
|---|---|
| `expected` | 取自 `cases.json` |
| `observed` | result、reason、approval_status、payment_status、budget_deducted、forbidden_events_found、purchases。對應方式與 Store 的 `purchaseOutcome` 相同 |
| `tasks` | `GET /api/tasks/{id}` 的回覆（host token） |
| `events` | `GET /api/tasks/{id}/events` 的回覆（host token）：API 自己的白名單投影 |
| `traces` | 每個 task 的 run trace，經歷史回放用的白名單載入（`loadTrace`，並與 Store 交叉核對）；讀不到時記為 `not_available` 與原因 |
| `host_runs` | `GET /api/host/runs`（operator token） |
| `budget` | 每個 task 的 `initial`（profile 的 task_cap，或目錄的 budget_initial）與 `remaining`（host 停止後，以唯讀方式讀資料庫） |
| `host_settings` | 實際傳給 host 的 registry 與 profile 檔案內容（含案例變體） |
| `store` | 這個案例 task 的 Store 紀錄（上述匯出；`restart` 是重啟前後兩個 task） |
| `recomputed`、`problems` | 測試以 `internal/m1evidence` 重算的結果與問題；有問題時測試失敗 |
| `verdict` | 測試自己的判定：API 讀回的結果相符，且重算沒有問題，才是 PASS |

`tasks`、`events`、`traces`、`host_runs`、`budget` 是 API 的白名單投影，**只作輔助**：它們不足以重算 digest 或事件鏈。claim 依據的是 `store`。

### 5.2 Store 紀錄（`store/<case_id>.json`）

- `internal/store` 的 `TestAcceptedIntegrationCases` 每個案例結束後寫出，內容是：
  - `case_id`、`layer`（`store_integration`）、`operation`；
  - `parameters`：defaults 加上案例參數；
  - `test_observed`：測試自己讀到的 result、reason、approval；
  - `tasks`：Store 紀錄。
- 測試本身也會重算；有問題就失敗。

### 5.3 契約紀錄（`contracts/<case_id>.json`）

- `internal/contracts` 的 `TestContractCasesFromManifest` 寫出實際呼叫的輸入與觀測結果：
  - `binding`：registry 設定、session、purchase；
  - `compare-retry`：既有與新的 purchase；
  - `validate-decision`：decision；
  - `validate-purchase`：變體後的 purchase；
  - `observed`：accept／reject／replay／new，以及拒絕碼。
- 契約案例沒有 Store 或 CLI trace，也不提升它的執行層。

### 5.4 重算（`internal/m1evidence`）

只用證據包與凍結的 `cases.json`。共用 canonical／schema 與契約程式，**不呼叫 `policy.Decide`**。

**整合案例**：
1. 快照：每份快照原文的 sha256 等於存的快照 hash；registry 能解析；terms 屬於這個 task。
2. 綁定：task 列的 consumer、agent、wallet、payer 與 registry 快照中這個 task 的綁定相符。
3. 請購：
   - 原文解析後重算 intent digest，等於存的 hash；
   - 請購本身（requirement digest、欄位與 quote 一致、綁定）在它的 `PURCHASE_PROPOSED` 事件驗證（`ValidateEvent`）；
   - 每筆存的請購都必須恰好被提出一次。
4. 事件：
   - 每個事件重算 `EventHash` 等於存的 hash；
   - scope 綁定與事件規則（`ValidateEvent`）；
   - 序號、`prev_event_hash` 鏈與轉移規則（`ValidateEventOrder`）。
5. 觀測結果**從事件推得**，對應方式與 `cases.json` 的寫法相同：
   - `policy`：決策事件的 verdict、reason、approval；
   - `advance-clock`：Ask 後的 `APPROVAL_EXPIRED`／`APPROVAL_WITHDRAWN`；
   - `cancel`／`cli-timeout`：task 狀態與 `TASK_CANCELLED`／`TASK_TIMED_OUT` 的 code。預期在請購前結束的案例，必須沒有請購；
   - `restart`：同一位 consumer 的兩個 task 是否同一 wallet 與 payer。
6. 預算：terms 快照的 `budget_initial` 減去 task 列的 `budget_remaining`，加總必須是 `"0"`。
7. 與 expected 逐欄比對：result、reason、approval_status、payment_status、budget_deducted；也不能出現 forbidden 事件。

**契約案例**：用紀錄的輸入重新呼叫契約程式：
- 結果要等於紀錄的 observed，也要等於 expected；
- 被拒絕時，拒絕碼要等於 expected 的 reason；
- 被接受時的 reason（`VALID_BINDING`、`SAME_PAYLOAD`、`NEW_REQUEST`）是案例清單的命名，沒有可觀測的 code。

### 5.5 不扣預算與 forbidden 事件

**不扣預算**：
- 重算時取 terms 快照的 initial 減 task 列的 remaining，各 task 加總必須等於 expected 的 `"0"`；
- E2E 證據檔的 `budget` 欄位是輔助的前後快照。

**沒有 forbidden 事件**：
- 每個 task 的事件中，不能出現 expected 的 `forbidden_events` 任何一項；
- M1 的事件契約本來就沒有這些事件型別，所以這項檢查無法用「合法地注入一個 forbidden 事件」來反向驗證：注入的事件會先被 `ValidateEvent` 拒絕；
- 預期在請購前結束的案例（`cancel-no-purchase`、`cli-timeout`），另外要求 `purchases == 0`。

## 6 遮罩與不含的內容

- **不含 CLI 原文**：raw trace 是 CLI 的輸出，只保留白名單投影（`traces`）。
- **不含 token**：
  - 測試寫出證據檔之前，會比對本次 host 的每一個 token，出現就失敗；
  - 證據包寫完後，`cmd/m1-fixture` 再掃描整個目錄（PNG 除外）：只要出現**不帶前綴的 64 位 hex**（demo token 的形狀），就不標為完成（state `secret_found`），結束碼 2；
  - 掃描排除 `sha256:` digest 和 `0x` 鏈上值；
  - browser-ui 自己的 token 檔放在暫存目錄，它也會掃描自己的輸出目錄。
- **不含配對碼**：fixture 執行不開 `--pairing`，所以沒有配對碼。配對碼是 12 個 Base32 字元，無法以形狀通用掃描。這是已知限制，live 包（E3）要另行處理。
- 截圖只有頁面畫面。browser-ui 有檢查「token 欄位為空」。

## 7 3.2 整合要求（`requirements/index.json`）

每項要求列出對應的測試與狀態（`pass`、`fail`、`not_found`、`not_run`）：
- Go 測試看 `go test -json` 的結果；
- node 看 TAP 的 `ok N - name`；
- browser-ui 看 `PASS name`；
- 名稱以 `*` 結尾的是前綴比對：至少要有一個相符，且沒有失敗。

| 要求 | 測試 |
|---|---|
| 頁面重新整理後由 Store 還原 | browser「refresh + sign-in restores task-a from the Store」 |
| 惡意字串只當文字顯示 | web「hostile strings stay strings; nothing reads as paid」、browser「hostile strings render as text only」 |
| 模型自稱已付款，畫面仍不顯示成功 | browser「nothing says paid or delivered」、`TestLiveRun` |
| 事件重送不重複動畫 | web「duplicate events are shown once and animated once」 |
| A／B 切換不串資料 | web「a switch drops every late reply of the old viewer」、`TestIntegrationAskDenyAndAccess` |
| 取消與 CLI 逾時顯示獨立狀態 | `TestCancel*`、browser「3.4:*」 |

這些是整合要求，不是案例 claim，所以不列入 `results`，只以 `requirements_index` 這個 source 列在索引中。

## 8 結束標記（`run-end.json`）

最後寫出，內容是 `run_id`、`state`，以及 complete 時 `manifest.json` 檔案 bytes 的 digest：

| state | 意義 |
|---|---|
| `complete` | 掃描通過，索引驗證通過，manifest 已寫出 |
| `secret_found` | 掃到疑似 token；沒有寫 manifest |
| `index_invalid` | `ValidateAgainst` 不通過；沒有寫 manifest |
| `save_failed` | 目錄、證據檔、索引需要的檔案或 manifest 寫入或讀回失敗 |
| `provenance_failed` | commit、diff、工作區狀態或 go／node 版本的查詢失敗，或 commit 不是 40 位 hex |

只有 state 是 `complete` 且 digest 與 `manifest.json` 相符，證據包才算完整。沒有 `run-end.json`（例如產生到一半中斷，或連失敗標記都寫不出），也是不完整。

## 9 檢查

**重算與竄改**（真實資料）：
- `internal/store` 的 `TestAcceptedIntegrationCases`：16 個整合案例各自匯出並重算，必須沒有問題。
- `cmd/demo` 的 `TestM1FixtureCases`：13 個 E2E 案例各自匯出並重算，必須沒有問題。
- `internal/store` 的 `TestM1EvidenceRefusesAlteredExports`：拿一個真實 Ask 的匯出，逐項竄改，每一項都必須被抓到：
  - 缺少 canonical 欄位（nonce）；
  - 金額或收款人改動；
  - 事件 scope 改動（未重算，以及重算 hash 後）；
  - 決策改動並重算；
  - 缺少最後一個、第一個或全部事件；
  - 快照消失；
  - payer 改動；
  - 預算被扣；
  - terms 改動；
  - 存的 intent digest 改動；
  - 完整重算並重新串鏈的偽造請購（只剩 intent 與 quote 不符）；
  - 最後一個事件的時間改動；
  - 另外，同一份 Ask 紀錄拿去對照 `deny-budget` 的 expected 必須不符，從未啟動的 task 也匯出不了。
- `internal/m1evidence` 的 `TestCheckContract`：
  - 契約紀錄的正反向：紀錄的結果與輸入不符、把 purchase 改成 A 自己的、缺少 purchase 或 registry、別的案例的紀錄、參數不符、不是契約層的紀錄；
  - `TestCheckIntegrationNeedsItsTasks`：task 數不符、沒有快照。
- `internal/m1evidence` 的 `TestBundle`（`APA_M1_BUNDLE=<run 目錄>`）：只讀證據包與凍結的 `cases.json`，核對每個證據檔的 digest 與結束標記，重算 31 個案例，並比對包內的判定。

**產生程式**（`cmd/m1-fixture/main_test.go`）不跑真正的命令。

`TestRunSaving` 以命令替身執行整個 `run()`，不碰任何舊的證據目錄：
- 完整且全過 → 0，結束標記 complete，digest 相符；
- 完整但有一案失敗 → 1；
- 結束標記寫不出 → 2，沒有完成摘要，已寫的 manifest 保留；
- 一般證據檔寫不出 → 2，沒有 manifest，state 是 `save_failed`，已寫的檔案保留；
- commit、diff、工作區狀態或 node 版本查詢失敗 → 2，state 是 `provenance_failed`，不執行任何測試；
- 失敗後連結束標記也寫不出 → 2，stderr 寫明。

`TestPassingLogsAreNotEvidence`：用真正的重算，每個 log 都寫 PASS 但紀錄沒有資料時，沒有任何案例 PASS。

其餘以合成的 run 目錄檢查：
- **索引**：全部通過時 `ValidateAgainst` 通過，`not_evaluated` 恰好是七個 capability；
- **確定性**：同一份輸入組兩次，除了 run ID 之外內容一致；
- **層級**：每個 claim 引用的 source 與第 4 節一致（13／3／15）；
- **反向檢查**：
  - E2E、Store 或契約紀錄缺少、標錯層級或讀不了 → INCONCLUSIVE；
  - 重算發現問題（E2E、只到 Store 層、契約各一）、E2E 測試自判 FAIL、測試失敗 → FAIL；
  - 測試缺少或略過 → INCONCLUSIVE；
  - 清單外的檔案、symlink → 拒絕；
- **掃描**：
  - 不帶前綴的 64 位 hex 會被抓到；`sha256:`、`0x`、40 位的 commit 不會被當成 token；
  - 讀不了的檔案不會被當成乾淨；
  - 結束標記帶 manifest digest；不完整的結束標記不帶 digest；寫不出時回報錯誤；
- **3.2**：全部 pass；反向檢查失敗、缺少、前綴無相符、瀏覽器未執行。

**拿掉關鍵檢查的反向驗證**（`python3 scripts/verify/m1-e1-reverse.py`，從 repo 根目錄執行）：
- 一次拿掉一項檢查，跑涵蓋它的測試，必須以**斷言失敗**抓到；編譯失敗不算。跑完把檔案還原。
- 共 17 項：
  - R2 的 5 項：結束標記錯誤、證據檔保存錯誤、來源查詢錯誤、`writeEnd` 吞掉錯誤、掃描的讀檔錯誤；
  - 產生程式的 2 項：只看 log 判定、缺紀錄不判 INCONCLUSIVE；
  - 重算的 10 項：快照 digest、task 列與 registry 綁定、存的 intent digest、存的 event hash、`ValidateEvent`、`ValidateEventOrder`、預算、與 expected 比對、契約重跑與紀錄比對、契約結果與 expected 比對。

## 10 已知限制與剩餘風險

- 第 5.4 節的重算與產生端共用 `internal/m1evidence`，所以仍是產生端的判定。B1-07 必須獨立實作重算：
  - 開檔防 symlink；
  - 核對 digest；
  - 對照 expected；
  - 檢查 hash chain 與 A／B 綁定（V1～V7）。
- 測試輸出含耗時，host 回覆含時間與 ID，所以兩次執行的證據檔只在這些欄位以外相同。
- `dirty_patch_digest` 不含 untracked 檔。
- 契約案例被接受時的 reason，是案例清單的命名，沒有可觀測的 code（第 5.4 節）。
- forbidden 事件的檢查無法以合法事件反向驗證（第 5.5 節）。
- 請購本身的驗證在它的 `PURCHASE_PROPOSED` 事件做，不另外重複一次。原本重複的那次呼叫，在反向驗證時證實是多餘的，已刪除；改以「完整重算的偽造請購」測試涵蓋。
- `cancel-no-purchase` 曾在一次完整 `-race` 執行中，host 停止時 HTTP shutdown 超過 5 秒（`context deadline exceeded`）。
  - 隔離重跑 5 次沒有重現。
  - 假設（**未證實**）：停止發生在工具的 HTTP 呼叫進行中。
  - 處理：改為不呼叫任何工具的 `idle` 情境。
  - 原因未確認，列為剩餘風險。
- `approval-deadline-before` 在第一次 `make m1-fixture` 判為 FAIL（觀察到 expired）。
  - 原因：測試的時鐘是「秒級牆鐘 + 位移」，等待的 300 ms 內跨過一秒，時鐘就從期限前 1 秒變成期限本身。
  - 這是**測試設計的錯**，不是產品行為。
  - 修正：改成設定後固定不動的時鐘。三個時鐘案例以 `-race` 各重跑 5 次都通過。
  - 失敗的那次證據包原樣保留。

## 11 交付紀錄（10/9，Claude Code；待 Codex 限定 review）

程式 `83428a4`（`cmd/m1-fixture`、`Makefile`、本文件），測試 `cc8f698`（`cmd/m1-fixture/main_test.go`、`cmd/demo/m1_test.go`）。

**證據包**：`artifacts/m1/m1-fixture-20261009T033121Z-96999c/`
- 在 `cc8f698` 的乾淨工作樹上產生：dirty patch digest 是空 diff；untracked 只有 repo 根目錄的 `demo`，未動。
- 7 個命令的 exit code 全部是 0。
- 31 PASS、0 FAIL、0 INCONCLUSIVE；七個 capability 都在 not_evaluated。
- 3.2 的檢查全部 pass；`run-end.json` 的 state 是 complete。
- 共 48 個證據檔。
- 另以獨立的 Python 重算：每個證據檔的 digest 都相符，結束標記的 manifest digest 也相符。
- 層級：13 個案例是 host_e2e＋store_integration，3 個是 store_integration＋policy_test，15 個是 contracts_test。

**全部 `make m1-fixture` 執行**（目錄都保留，只讀，不刪改）：

| run 目錄 | 程式狀態 | 結果 |
|---|---|---|
| `…030831Z-c9a6dd` | 未提交 | 30 PASS、1 FAIL（`approval-deadline-before`）。測試時鐘的設計錯誤，見第 10 節；end 標記為 complete，判定原樣保留 |
| `…031813Z-f871bc` | 未提交，時鐘已修正，當時 E2E 是 10 案 | 31 PASS |
| `…033048Z-346e32` | 未提交 | **主動中止**：發現新檔仍是 untracked，`dirty_patch_digest` 涵蓋不到。沒有 `run-end.json`，所以是不完整的包 |
| `…033121Z-96999c` | `cc8f698` | 交付的證據包（上述） |

**另外執行的檢查**：
- `go vet ./...`、`go test -count=1 ./...`：全部通過；
- `TestM1FixtureCases` 以 `-race` 跑 13 案：通過；
- 三個時鐘案例以 `-race` 重跑 5 次：通過；
- `mutation-anchors.py`：311 項 anchor 都找到；
- 第 2 包與交付包共有的 10 個 E2E 案例：expected、observed、budget、verdict 與 trace 工具序列都一致。

**沒有執行**：
- 真實 CLI、完整容器驗證、完整突變重跑；
- B1-07（M1-E2）。

任務狀態未改，B1-04／B1-06 維持 InProgress。
