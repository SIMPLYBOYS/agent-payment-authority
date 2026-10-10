# M1 fixture 證據包（M1-E1，`make m1-fixture`）

> 2026-10-09；依 `docs/m1-closeout-gaps.md` 第 4 節的 E1。只用 fake CLI：不執行模型、不連 testnet、不付款。
>
> **10/9 修訂（Codex review R1、R2）**：
> - R1：每個 claim 改以「可只靠證據包重算」的結構化紀錄為依據（第 5 節），測試 log 只作輔助；
> - R2：任何保存或來源查詢失敗，都以結束碼 2 表示證據包不完整，不印完成結論（第 1、8 節）。
> - 第 11 節是修訂前的交付紀錄，原樣保留；修訂後的交付見第 12 節。
>
> **10/10 補正（Codex 限定複核：R2 關閉，R1 兩個局部缺口）**：
> - R1-1：契約紀錄的實際輸入必須是凍結案例指定的那一組（第 5.4 節）；
> - R1-2：terms 說 present 的 profile／credential 必須有快照，且快照有效（第 5.4 節）。
> - 第 12 節保留原樣；本輪交付見第 13 節。
>
> **現行狀態（10/10）**：M1-E1 於程式 `0d13255`、測試與產包基準 `0853b44` 通過限定範圍技術複核（第 14 節）。R1-1、R1-2 關閉，R2 維持關閉。這是 fixture 產生端的驗收，不代替 B1-07，也不代表 M1、B1-04 或 B1-06 整體完成。下一步 E2（B1-07 獨立證據檢查器）待另行派工。
>
> **E2（10/10）**：唯讀檢查器 `make verify-m1 RUN_DIR=...` 的 fixture 子範圍，於程式 `48a43bc`、測試 `973a58d` **通過 Codex 限定技術複核**（R1～R3 關閉，第 15.7 節）。B1-07 整體仍是 InProgress：live 包（E3）尚未支援，E3 待另行派工。
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
1. 快照：
   - 每份快照原文的 sha256 等於存的快照 hash；registry 能解析；terms 屬於這個 task；
   - 設定狀態，依 Store 自己的復原檢查（`internal/store/verify.go`）：terms 的 `profile_state`／`credential_state` 是 `present` 時，對應快照必須存在；是 `absent` 時必須不存在；其他值不接受；
   - 存在的 profile 要能嚴格解碼，並通過 `Profile.Validate`；terms 的預算不超過它的 task cap，資產與它相同；
   - 存在的 credential 要是這個 consumer 的 M1 fixture（consumer 相符、evidence_mode 是 fixture、有 valid_until）；
   - terms 的資產要在 registry 快照中；
   - 合法的缺設定只有 terms 明說 absent 的情況（`deny-missing-config` 的 profile）。不另造規則，也不做真實憑證、BBS+、簽章或 ZK 驗證（這些仍是 not_evaluated）。
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

**契約案例**：

先核對**實際輸入就是這個案例的輸入**：
- 依凍結案例的 operation 與 parameters（取自 `cases.json`，不取紀錄自己填的），對照可信 fixture（`testdata/contracts/purchase-input.json`、`registry.json`），逐欄比較；
- 只有案例指定的欄位可以不同，而且要是指定的值；其他欄位必須與 fixture 相同。各類案例的對照：

| operation | 核對 |
|---|---|
| `binding` | session 是 parameters 指定的 A 或 B（registry 中 `task-a`／`task-b` 的 consumer、agent、task）；purchase 是 fixture，指定為 B 時換成 B 的 consumer、agent、wallet、task、payer 與另一個 purchase_order_id；mutation 指定的那一欄是 B 的值；紀錄的 registry 等於可信 registry |
| `compare-retry` | existing 等於 fixture；incoming 依 mutation：`none` 完全相同；`nonce` 只有 nonce 不同；`request_id` 只有 request_id 不同；`other_consumer` 只有 B 的身分欄位不同，request_id 相同 |
| `validate-decision` | decision 綁定 fixture 的 intent hash，payment_status 是指定值；把它改回 `unavailable` 後其餘部分有效，表示指定欄位是唯一的缺陷 |
| `validate-purchase` | 只有 quote 的指定變體不同（`extra.assetTransferMethod` 是 erc7710 或 permit2；或多了大小寫變體鍵 `payto`），requirement digest 隨 quote 變（能計算時等於 quote 的 digest，不能計算時維持 fixture 的） |

- 缺少核對所需的資料（session、registry、purchase、decision，或可信 fixture 讀不到），不會 PASS：產生程式在 fixture 讀不到時判 INCONCLUSIVE，紀錄缺資料時判 FAIL；
- 不以 expected 產生替代輸入來驗證。

再用紀錄的輸入重新呼叫契約程式：
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
- 10/10 補正：
  - `TestCheckContractBindsInputsToTheCase`：15 個契約案例各以依 fixture 建出的紀錄通過自己的案例；每一份紀錄改標成其他 14 案中的任一案（case ID、parameters、observed 都改成那一案的），共 210 組，全部被拒；
  - `TestCheckContractReviewCounterexamples`：複核提出的兩個反例（A 的輸入標成 `consumer-b`；nonce 衝突標成 `retry-new-request-same-order`），先確認契約程式對它們給出被標案例的結果與 code，再確認被拒；
  - `TestCheckContract` 另加：registry 改動、缺 session、purchase 的其他欄位改動、真實紀錄對照不同的 expected 結果或 code、讀不到可信 fixture；
  - `internal/store`：Ask 缺 profile、缺 credential、兩者都缺；terms 改成 absent 並重算快照 digest；無效的 profile 快照、別的 consumer 的 credential（都重算 digest），全部被拒。`TestM1EvidenceAcceptsADeclaredMissingConfig` 確認 `deny-missing-config` 的 profile 確實宣告為 absent，且被接受；
  - `cmd/m1-fixture`：`TestUnreadableFixturesLeaveContractsInconclusive`。
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
- 10/10 起共 22 項：原 17 項，加上契約拒絕碼與 expected 比對、實際輸入與案例綁定、present 必有快照、profile 快照有效、credential 屬於該 consumer。
- 10/9 的 17 項：
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
- 匯出若被**整份一致地**重造（快照、terms、請購、事件都重算 hash 並重新串鏈，且彼此一致），單靠證據包本身無法分辨。這需要外部的錨點（例如 B1-07 對照的另一份紀錄），和 `ValidateEventOrder` 註明的限制相同。例如把 terms 的 profile 改為 absent、同時刪掉 profile 快照並重算 terms digest：設定狀態彼此一致，但 Ask 的決策與缺設定的語意矛盾；檢查這點需要 Policy 的語意，本重算刻意不呼叫 Policy。
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

## 12 R1／R2 修訂交付紀錄（10/9，Claude Code；待 Codex 限定複核）

**Commit**（依時間順序）：

| commit | 內容 |
|---|---|
| `fd818a9` | 程式：`internal/m1evidence`，產生程式依結構化紀錄判定，保存與來源查詢錯誤處理 |
| `3881a0a` | 測試：Store、E2E、契約的紀錄與重算，竄改測試，保存失敗測試，`scripts/verify/m1-e1-reverse.py` |
| `3a8b9d2` | 測試：`TestBundle` |
| `c705bad` | 程式：修正 E2E 紀錄的解析（見下表第 1 列） |
| `ebf0fac` | 測試：合成的 E2E 紀錄帶有 API 形狀的 `tasks` |
| `fb20b7a` | 文件：本文件第 1～10 節修訂、`docs/m1-closeout-gaps.md` 第 4 節的現況 |

**這次的 `make m1-fixture` 執行**（目錄都保留，只讀，不刪改；第 11 節的舊包也不動）：

| run 目錄 | 基準 | 結果 |
|---|---|---|
| `…153139Z-d5d34a` | `3881a0a`，tracked 乾淨 | 證據包完整，**18 PASS、13 INCONCLUSIVE**；程式結束碼 1（make 顯示成它自己的 2）。原因：產生程式以 Store 匯出的形狀讀 E2E 紀錄的 `tasks`（那是 API 回覆），解析失敗，13 個 E2E 案例因此判為 INCONCLUSIVE，沒有誤判成 PASS。`TestBundle` 在唯讀副本上指出同樣這 13 案：紀錄重算沒有問題，但包內判定不同。已在 `c705bad` 修正，`ebf0fac` 加回歸；拿掉修正時，回歸以斷言失敗抓到 |
| `…154053Z-2e985f` | `fb20b7a`，tracked 乾淨（dirty patch digest 是空 diff；untracked 只有 repo 根目錄的 `demo`，不在 digest 內，未動） | **本次交付的證據包** |

**交付包** `artifacts/m1/m1-fixture-20261009T154053Z-2e985f/`：
- 7 個命令的 exit code 全部是 0；`make m1-fixture` 結束碼 0。
- 31 PASS、0 FAIL、0 INCONCLUSIVE；七個 capability 都在 not_evaluated；3.2 的檢查全部 pass；結束標記是 complete。
- 79 個證據檔，其中結構化紀錄：e2e 13、store 16、contracts 15。
- 層級：
  - 13 案是 host_e2e＋store_integration（各為紀錄＋log）；
  - 3 案是 store_integration＋policy_test；
  - 15 案是 contracts_test（紀錄＋log）。
- 另以 Python 重算 79 個證據檔的 digest 與結束標記的 manifest digest，全部相符。
- `APA_M1_BUNDLE=<唯讀副本> go test -run '^TestBundle$' ./internal/m1evidence/`：通過。只讀證據包與凍結的 `cases.json`，重算 31 案，與包內判定一致。

**另外執行的檢查**：
- `go vet ./...`、`go test -count=1 ./...`：全部通過；
- 以 `-race` 跑：`TestM1FixtureCases`（13 案）、`TestAcceptedIntegrationCases`、`TestM1EvidenceRefusesAlteredExports`、`internal/m1evidence`、`cmd/m1-fixture`，都通過；
- `python3 scripts/verify/m1-e1-reverse.py`：17／17 以斷言失敗抓到。在 `3881a0a` 與 `ebf0fac` 各跑一次；
- 第一次跑反向驗證時，有一項沒被抓到：拿掉 `verify` 裡的 `ValidatePurchase` 呼叫。查證後確認是重複檢查（每筆請購都必須恰好被提出一次，並在 `ValidateEvent` 驗證），已刪除，改以「完整重算的偽造請購」與「存的 intent digest 改動」兩項竄改測試涵蓋。另有 4 項因反向驗證腳本本身的變數未使用而編譯失敗，修正腳本後重跑。

**沒有執行**：
- 真實 CLI、版本查詢、登入、C0′、真實請購、`make present-live`；
- 完整斷網容器、311 項突變重跑、重建 Unity；
- B1-07（M1-E2）。

任務狀態未改，B1-04／B1-06 維持 InProgress。

## 13 R1-1／R1-2 補正交付紀錄（10/10，Claude Code；待 Codex 限定複核）

**開工基準**：`ee2a574`，工作區只有未追蹤的 `demo`，沒有既有的相關修正。R2 已在複核中關閉，本輪沒有改動它的處理，`TestRunSaving` 維持通過。

**Commit**：
- `0d13255`：程式（`internal/m1evidence` 的輸入綁定與設定快照檢查；產生程式讀可信 fixture）；
- `0853b44`：測試（契約輸入矩陣與兩個反例、設定快照竄改、合法缺設定、fixture 讀不到；反向驗證增為 22 項）；
- 文件：本節與第 5.4、9、10 節。

**新證據包**：`artifacts/m1/m1-fixture-20261009T162703Z-4ddb7a/`（run ID 的時間是 UTC）
- 在 `0853b44` 產生：tracked 乾淨，dirty patch digest 是空 diff；untracked 只有 repo 根目錄的 `demo`，不在 digest 內，未動。
- 7 個命令的 exit code 全部是 0；`make m1-fixture` 結束碼 0。
- 31 PASS；七個 capability 都在 not_evaluated；3.2 全部 pass；結束標記 complete。
- 79 個證據檔；層級 13／3／15 不變。
- 另以 Python 重算 79 個 digest 與結束標記的 manifest digest，全部相符。
- `TestBundle` 在唯讀副本上通過：31 案以新的檢查重算，與包內判定一致。仍是產生端判定，不是 B1-07。
- 之前的證據包（第 11、12 節所列，包含 `…154053Z-2e985f` 與 `…153139Z-d5d34a`）都保持原樣，沒有以新判定改寫。

**定向回歸**（fake CLI）：
- `go vet ./...` 通過；
- 以 `-race` 跑：`internal/m1evidence`、`cmd/m1-fixture`（含 `TestRunSaving`、`TestBundle` 未設定時略過）、`internal/store` 的 `TestM1Evidence*` 與 `TestAcceptedIntegrationCases`、`cmd/demo` 的 `TestM1FixtureCases`，都通過；
- `internal/contracts`、`internal/store`、`internal/policy` 全套通過。

**反例與反向驗證的結果**：

| 檢查 | 結果 |
|---|---|
| 複核的兩個反例（`TestCheckContractReviewCounterexamples`） | 先確認契約程式對它們給出被標案例的結果與 code，接著都被拒 |
| 15 個契約案例 × 其他 14 案的改標 | 210 組全部被拒；15 案各以自己的紀錄通過 |
| Ask 缺 profile／缺 credential／兩者都缺；terms 改 absent（重算 digest）；無效 profile、別人的 credential | 全部被拒；`deny-missing-config` 被接受 |
| `python3 scripts/verify/m1-e1-reverse.py` | 22／22 以斷言失敗抓到，結束碼 0 |

- 反向驗證第一次跑時，「契約結果與 expected 比對」沒被抓到：新的輸入綁定先擋下了原本只靠它擋的竄改。改為以「真實紀錄對照不同的 expected 結果或 code」單獨涵蓋，另加「拒絕碼與 expected 比對」一項，重跑 22／22。
- **沒有做**：直接拿舊包 `…154053Z-2e985f` 的 `contracts/consumer-a.json`、`retry-conflict.json` 改標重跑。暫時在 repo 內建立探測檔再刪除的做法被權限擋下，所以沒有執行。兩個反例改由單元測試重現，所用的紀錄與契約測試的建法相同。

**沒有執行**：
- 真實 CLI、版本查詢、登入、C0′、真實請購；
- 完整斷網容器、311 項突變、Unity 重建；
- E2～E4。

任務狀態未改，B1-04／B1-06 維持 InProgress。

## 14 限定驗收紀錄（10/10，Codex 限定技術複核；Claude Code 文件同步）

**結論**（依驗收同步派工原文寫入）：

> M1-E1 於程式 `0d13255`、測試與產包基準 `0853b44` 通過本次限定範圍技術複核。R1-1 實際案例輸入綁定、R1-2 必要設定快照檢查均關閉；R2 證據保存失敗處理維持前輪關閉。這是 fixture 產生端的驗收，不代替 B1-07 獨立證據檢查器，也不代表 M1、B1-04 或 B1-06 整體完成。

**基準**：
- 產品修正 `0d13255`，測試與產包基準 `0853b44`，交付文件 `6f9c338`；`0853b44..6f9c338` 只有本文件的差異；
- 本節的文件同步不改變產品驗收基準；
- 交付包：`artifacts/m1/m1-fixture-20261009T162703Z-4ddb7a/`。

**關閉理由**：
1. **R1-1**：實際輸入會依凍結案例與可信 fixture 核對。Codex 直接讀上一輪交付包，確認 `consumer-a` 改標 `consumer-b`、nonce 衝突改標 request_id 衝突，現在都被拒絕；兩者即使仍得到被冒充案例的契約結果，也不能冒充該案例。
2. **R1-2**：terms 宣告 present 時不得缺對應快照。Codex 直接以舊包的 Ask 紀錄測試缺 profile、缺 credential、兩者都缺，均被拒絕；合法的 `deny-missing-config` 仍接受。
3. **R2**：本輪沒有重開；`TestRunSaving` 隨定向回歸通過。

### 14.1 證據歸屬

**Codex 本輪獨立執行**：
- 唯讀核對新包 `…162703Z-4ddb7a` 和舊包 `…154053Z-2e985f`：各 79 個證據檔的 digest，以及結束標記中的 manifest digest，均相符。
- 讀取新包紀錄：31 PASS、七個命令 exit 0、七個 capability 為 `not_evaluated`、結束標記 complete。這部分是**核對紀錄**，不是獨立重跑這七個命令。
- 從 `0853b44` 匯出暫存來源，在斷網容器中執行定向 race 測試。來源、新舊證據包與探測檔都以唯讀掛載，測試只在容器的 scratch 副本運作：

```text
go test -race -p 1 -count=1 ./cmd/m1-fixture ./internal/m1evidence -skip '^TestReview' -timeout 120s
go test -race -count=1 ./internal/m1evidence -run '^TestReview' -v -timeout 60s
go test -race -count=1 ./internal/store -run '^TestM1Evidence' -timeout 90s
```

- 第一組：兩個套件通過，包含 `TestRunSaving`、`TestBundle`；`TestBundle` 對新包唯讀重算 31 案，與包內判定一致。
- 第二組：Codex 的**臨時探測**，直接讀舊包、只在記憶體改值，涵蓋上述兩個改標反例、三個缺快照反例，以及合法缺設定的正向對照，全部通過。`TestReview` 不在 repo 的正式測試中，無法直接從 repo 執行。
- 第三組：Store 的定向測試通過。
- 容器最終回報 `BASELINE_EXIT=0 PROBE_EXIT=0 STORE_EXIT=0`，整體 exit 0。
- 映像 `sha256:78258368de0907e58498095c327a6ee4edbe75421d18e524bd00661c3749b6d0`；網路關閉、來源與證據唯讀、只有 scratch 可寫；沒有掛登入環境、憑證或 Docker socket。
- 以上是「斷網容器內的定向複核」，**不是完整七步容器驗證**。測試輸出只在 Codex 的工具紀錄，沒有另存 repo log。

**Claude Code 執行，維持原歸屬**：第 13 節的產包、22 項反向驗證、210 組改標測試、`go vet` 與各套件測試，保留原命令、基準與證據路徑。Codex 本輪重跑的是上述套件範圍，**不代表獨立重跑了原交付的全部命令或反向驗證流程**。

**Codex 本輪沒有執行**：重新產包、瀏覽器、完整容器七步、全部 311 項突變、Unity 建置、真實 CLI。

### 14.2 限制

- 31 PASS 是 fixture 證據與凍結案例在本切片範圍內的判定，不是 B1-07 已完成。
- hash 與事件鏈的重算不提供外部真實性錨點；整份匯出若被一致地重造，單靠該包不能分辨（第 10 節）。不擴張成簽章或鏈上驗證。
- 七個 capability 維持 `not_evaluated`；不新增 Policy 語意檢查，也不呼叫 `policy.Decide`。E2 的獨立核對留給後續派工。
- M1 live 是否接受開發展示 plugin 例外，仍待 Aaron 決定；不阻擋本次 E1 驗收，也不因本節自動核准。

**本節是文件同步**，不是新的技術驗證：沒有改程式、`cases.json`、可信 fixture 或證據包，沒有重新產包，也沒有跑任何測試。任務狀態未改，B1-04／B1-06 維持 InProgress，B1-07 未開工。

## 15 M1-E2 唯讀證據檢查器（B1-07 的 fixture 子範圍；Claude Code 實作，待 Codex 限定 review）

> 依 `Claude-Code-M1-E2唯讀證據檢查器實作派工-ce23a3b-20261010`。開工基準 `ce23a3b`（E1 驗收基準：產品 `0d13255`、測試與產包 `0853b44`，不回填）。

**介面**：
- `go run ./cmd/verify-demo --run-dir DIR [--repo .] [--mode fixture]`，或 `make verify-m1 RUN_DIR=DIR`；
- 機器可讀報告（JSON）寫到 stdout，診斷寫到 stderr；不寫任何檔案，不執行包內命令，不開 Store，不連網；
- 結束碼（`verify-demo` 程式本身）：
  - 0：所有必要 claim PASS；
  - 1：有 FAIL；
  - 2：沒有 FAIL，但有 INCONCLUSIVE（缺資料、版本不支援）；
  - 3：用法錯誤、內部錯誤或報告輸出失敗。
- 經 `make` 或 `go run` 時，外層會改寫非零結束碼，必須看報告的 `verdict`，或直接執行編好的程式。

**可信輸入**（取自 repo，不接受包內提供的替代品）：
- `testdata/contracts/cases.json`（expected 與 parameters）；
- `testdata/contracts/purchase-input.json`、`registry.json`（契約案例的輸入 fixture、A／B 身分）；
- 第 3 節列的設定檔（重算 config digest）。
- 這些與包內的 digest 不符時，回報 INCONCLUSIVE（不支援的版本），不換版本，也不要求重產舊包。

**共用的純函式**（只用來解析與驗證，不用來決定 verdict）：
- `contracts.Decode`、`Canonical`；
- `CaseManifest.Hash`、`RequireAccepted`、`RunManifest.ValidateAgainst`（只當作索引檢查）；
- `AuthorizationIntent.Hash`、`PurchaseOrder.Validate`、`Quote.Hash`；
- `NewRegistry`、`Registry.Binding`／`ValidatePurchase`／`ValidateEvent`／`TrustedAsset`；
- `EventHash`、`GenesisHash`、`ValidateEventOrder`、`PolicyDecision.Validate`、`CompareRetry`、`ErrorCode`；
- `policy.Profile.Validate`。

**不使用**：`policy.Decide`、Store、Host、CLI runner、`internal/m1evidence` 的任何判定函式、包內的 `observed`／`verdict`／`problems`／log 中的成功字樣。

**由檢查器自己判定**：
- 路徑與檔案安全、索引；
- 案例參數與實際輸入的綁定（整合案例：可信設定與請購；契約案例：與可信 fixture 逐欄比對）；
- 事件鏈與 Store／API 兩個來源的對照；
- 從事件推導結果、各 reason 的必要條件、expected 比對；
- forbidden 事件、模式與層級、A／B 綁定、整合要求的測試紀錄、最終 verdict。

**判定表**：

| Claim | 檢查 | FAIL | INCONCLUSIVE |
|---|---|---|---|
| V1 | 索引、digest、結束標記、來源資訊、命令、可信輸入的版本 | digest 不符、索引外檔案、路徑違規、資訊互相矛盾 | 缺檔、缺欄位、版本不支援 |
| V2 | 輸入與請購：參數綁定、請購 digest、金額、資產、收款人、task | 與案例或 fixture 不符 | 缺必要紀錄 |
| V3 | 結果、裁決、設定、預算、必要條件 | 推導結果與 expected 不同、條件不成立 | 缺快照或事件 |
| V4 | 事件鏈 | hash、序號、prev、關係或兩個來源不一致 | 缺事件 |
| V5 | 無提前核准或付款 | forbidden 或未知事件、付款狀態不是 unavailable | — |
| V6 | 模式與層級 | fixture 以外的標籤、層級冒充 | live 模式（E3 之前不支援） |
| V7 | A／B 綁定 | 身分不一致、跨用戶 | 缺 registry |

### 15.1 實作與 V1～V7 對照

程式：`cmd/verify-demo/`（CLI）、`internal/verification/`（`files.go` 唯讀存取、`verify.go` V1／V6 與分派、`integration.go` 整合案例、`contract.go` 契約案例、`requirements.go` 整合要求、`report.go` 報告與三態、`trusted.go` 可信輸入）。

| Claim | 實作（檢查器自己判定的部分） | 正例 | 負例（測試名稱 → 判定） |
|---|---|---|---|
| V1 | 盤點（lstat，symlink、FIFO、裝置一律違規）；只讀 `os.Root` 下的普通檔，以 O_NOFOLLOW＋O_NONBLOCK 開檔並比對 inode；上限 4096 檔、每檔 64 MiB、總計 1 GiB；manifest／結束標記嚴格解碼（重複成員、未知欄位、多份文件都拒）；run ID、commit、digest 格式、versions、case list digest、config digest 以 repo 的可信檔案重算；七個命令各一次且 exit 0；索引唯一、每檔 bytes digest、索引外的檔案、claim 的引用；31 案各一次、七個 capability 恰為 not_evaluated | `TestGoldenBundle` | 檔案在建索引後改動、索引沒列的檔、沒人引用的 symlink、往上爬的索引路徑（`TestSingleChecks`）→ FAIL；symlink、連到外面的目錄、FIFO（`TestPaths`）→ FAIL，且 FIFO 不會卡住；另一份 case list → INCONCLUSIVE；沒有結束標記 → INCONCLUSIVE；結束標記指向別的 manifest、claim 重複、非 0 的命令（`TestMissingAndContradictory`）→ FAIL；必要案例改列 not_evaluated、capability 自稱 PASS（`TestRequirementsAndClaims`）→ FAIL |
| V2 | 整合案例：凍結參數（`remaining`、`allow_max`、`mode`、`required_config`、`approval_deadline_seconds`、Store 層的 `clock`、`amount`、`merchant`、`asset`、`credential`）對照 task 實際的設定與請購；請購的 intent digest、`PurchaseOrder.Validate`（requirement digest 與欄位一致）；E2E 的 host 設定檔對照 Store 快照、API 回覆的請購對照 Store。契約案例：與可信 fixture 逐欄比對，只有案例指定的欄位可以不同 | 同上 | 金額、收款人改動（`TestRecordsThatContradictTheirCase`）、Store 時鐘不是案例的（`TestSingleChecks`）→ FAIL；`consumer-a` 冒充 `consumer-b`、nonce 衝突冒充 request_id 衝突（`TestContractRecordsRelabelled`）→ FAIL，且確認它們仍得到被冒充案例的結果；重複 JSON 成員 → FAIL；截斷的紀錄 → INCONCLUSIVE |
| V3 | 從事件推導結果（各 operation 照 `cases.json` 的寫法）；到期時間等於決策時間加 `advance_seconds`；預算（terms 初值減剩餘）；設定 present／absent 與快照一致、profile 有效、credential 屬於該 consumer、terms 的預算與資產在 profile 內；**各 reason 的必要條件**（Deny 的 reason 本身成立；Allow／Ask 需要所有 Deny 條件都不成立，再以 allow 上限與核准者決定）；契約案例以紀錄的輸入重跑純契約函式，對照凍結的 expected 與紀錄自己的 observed | 同上 | Deny 被報成 Allow、缺 profile／credential／兩者都缺、terms 與快照矛盾、**設定被一致地宣告為 absent 卻仍是 Ask**、無效 profile、別人的 credential、預算被扣、merchant 其實允許的 deny-merchant（`TestRecordsThatContradictTheirCase`）→ FAIL；別案的 Store 紀錄冒充、terms 把缺少的 profile 說成 present、到期時間不對（`TestSingleChecks`）→ FAIL；凍結 expected 不同、紀錄與自己的輸入矛盾（`TestContractOutcomes`）→ FAIL |
| V4 | 每個事件的 `EventHash` 等於存的 hash、序號連續、`prev_event_hash` 從 genesis 串起、scope 屬於該 task、引用的請購有存、`ValidateEvent`、`ValidateEventOrder`、每筆請購恰好被提出一次；E2E 另外核對 API 的 task／events 回覆（狀態、`as_of_sequence`、事件 ID、型別、序號、prev、時間）與 Store 一致 | 同上 | 中段事件改動、序號缺口（`TestRecordsThatContradictTheirCase`）、API 的事件不是存的事件（`TestSingleChecks`）→ FAIL |
| V5 | 只接受 M1 的事件型別；expected 的 forbidden 事件；決策與 API 的付款狀態必須是 unavailable | 同上 | 插入 SETTLED（`TestRecordsThatContradictTheirCase`）、決策記錄付款（`TestSingleChecks`）→ FAIL |
| V6 | evidence mode 必須是 fixture；每個路徑的 source 依檢查器自己的表；紀錄的 layer；claim 必須引用它那一層的紀錄（存在卻沒引用 → FAIL；不存在 → INCONCLUSIVE）；`--mode live` 回報不支援 | 同上 | 未知 source、fixture 標成 live、契約證據標成 E2E、契約紀錄標成 E2E（`TestModesAndLayers`）→ FAIL；live 模式 → INCONCLUSIVE（exit 2） |
| V7 | task 列、registry 快照與可信 registry 的身分一致；請購與事件的 consumer、agent、wallet、task、payer 屬於該 task；契約綁定案例的 session 與 A／B 身分 | 同上 | 錯的 payer、A 的事件放到 B（`TestRecordsThatContradictTheirCase`）、沒有請購的 task 換 payer（`TestSingleChecks`）→ FAIL |
| R（3.2） | 檢查器自己的六項要求與測試名稱；自己解析 go test JSON、TAP、browser-ui 的行；失敗 → FAIL，缺少或略過 → INCONCLUSIVE，索引說 pass 但紀錄不是 → FAIL | 同上 | 索引 pass 但紀錄沒有該測試、索引 pass 但測試被略過（`TestRequirementsAndClaims`）、測試失敗（`TestFailingRequirementTest`）→ FAIL |
| 結束碼 | 0／1／2／3（`cmd/verify-demo`） | `TestExitCodes`：golden → 0 | FAIL → 1、INCONCLUSIVE → 2；未給 run 目錄、目錄不存在、沒有 case list、報告寫不出、未知旗標 → 3 |

- **負例的做法**：在 golden 的暫存副本上改，**同步重算**內層的 hash（快照 digest、intent digest、事件 hash 與 prev 鏈）與外層的索引 digest、結束標記；所以每個負例只能被它自己那一項檢查抓到。`TestRecordsThatContradictTheirCase` 另外確認 V1 維持 PASS。
- **產生端的判定只拿來比對**：把正確案例的 manifest 判定改成 FAIL，檢查器仍判 PASS 並列出差異；把錯誤紀錄的 manifest 判定留在 PASS，檢查器判 FAIL（`TestProducerVerdicts`）。
- **golden**：`testdata/verification/`，取自 E1 驗收包 `…162703Z-4ddb7a`，拿掉 24 張截圖並重建索引；預期寫在 `expected.json`，由凍結的 `cases.json` 與 E1 的層級表寫成，**不是**執行檢查器或產生端的判定得到的（見該目錄的 README）。

### 15.2 共用與獨立

- **共用的純函式**：如本節開頭所列。全部只用來解析與驗證：
  - `ValidateAgainst` 只當索引檢查；
  - `ValidateEvent`、`ValidateEventOrder` 只當事件的結構與轉移規則。
- **檢查器自己寫的部分**：
  - 讀檔與路徑安全、嚴格 JSON（重複成員）；
  - 輸入綁定（整合案例的參數對照、契約案例逐欄比對，比對程式不沿用 `internal/m1evidence`）；
  - 事件鏈的序號與 prev 串接（與 `ValidateEventOrder` 雙重檢查）、Store 與 API 的對照；
  - 從事件推導結果、各 reason 的必要條件、expected 比對、forbidden、模式與層級、A／B 身分、整合要求的測試紀錄解析；
  - 三態判定與結束碼。
- **不使用**：
  - `policy.Decide`、Store、Host、CLI runner；
  - `internal/m1evidence` 的 `CheckIntegration`、`CheckContract`、`TestBundle`；
  - 包內的 `observed`、`recomputed`、`problems`、`verdict`、`test_observed` 與 log 的成功字樣。
  - 例外：E2E 紀錄自己的 `verdict` 若不是 PASS，視為矛盾而判 FAIL；它只能讓案例失敗，不能讓案例通過。

### 15.3 反向驗證

`python3 scripts/verify/m1-e2-reverse.py`（從 repo 根目錄執行）：
- 一次拿掉一項關鍵核對；兩個檢查守同一件事時，兩個一起拿掉。跑 `internal/verification` 與 `cmd/verify-demo` 的測試，必須以**斷言失敗**抓到（編譯失敗不算），跑完還原檔案。
- 共 **38 項**：
  - V1 的 10 項：digest、索引外檔案、盤點違規、不安全讀取、結束標記 digest、缺結束標記、config digest、case list 版本、命令結束碼、claim 唯一；
  - V2 的 4 項：契約輸入比對、請購驗證、案例參數、E2E 兩個來源；
  - V3 的 9 項：expected、必要條件、設定 present／absent、profile、credential、預算、到期時間、契約結果、契約紀錄與重跑；
  - V4 的 1 項：hash、序號、prev、事件規則；
  - V5 的 2 項：forbidden 與未知事件、付款狀態；
  - V6 的 4 項：source、mode、紀錄 layer、live 不支援；
  - V7 的 2 項：task 綁定、事件 scope；
  - R 的 2 項：索引與紀錄矛盾、測試失敗；
  - 結束碼與報告的 4 項：FAIL→1、INCONCLUSIVE→2、報告寫不出、`worst` 保留 FAIL。
- **結果：38／38 以斷言失敗抓到**。
- 過程：
  - **第一次**：30／38。5 項是腳本本身的問題（錨點重複、替換造成編譯錯誤），3 項是真的缺少單獨的測試（契約結果對照 expected、契約紀錄對照重跑、整合要求測試失敗而索引不說 pass）。補了 `TestContractOutcomes`、`TestFailingRequirementTest`，修好腳本。
  - **第二次**：37／38，剩 1 項是腳本的編譯錯誤。
  - **第三次**：38／38。
- 在這之前，也先依各檢查補了單獨涵蓋的負例（`TestSingleChecks` 的 13 項），否則拿掉某項檢查時，會被其他檢查遮蔽。

### 15.4 測試與驗收紀錄（10/10，Claude Code 執行，待 Codex review）

**Commit**：
- 開工基準 `ce23a3b`（只有未追蹤的 `demo`）；
- 程式 `21d9d38`、測試 `296fd91`、README 與本節設計簡記 `ff776bd`；
- 本小節的驗收紀錄另行提交。

**定向回歸**：
- `go vet ./...` 通過；
- `go test -race -count=1 ./internal/verification/... ./cmd/verify-demo/...`：兩個套件通過。

**既有包驗收**（唯讀，沒有執行任何產包命令）：
- 證據目錄：`artifacts/m1-verify/e2-20261009T181043Z/`。這是包外的新目錄，`NOTE.txt` 說明每個檔案。
- 輸入：`artifacts/m1/m1-fixture-20261009T162703Z-4ddb7a/`，81 個檔（79 個索引證據、manifest、結束標記），讀了 7,070,611 bytes。
- 原包每個檔的 sha256 在執行前、兩次執行後都相同（`bundle-before.sha256`、`bundle-after.sha256`、`bundle-after-2.sha256`）。
- 結果：

  | 命令 | 結束碼 | 報告 |
  |---|---|---|
  | `make verify-m1 RUN_DIR=artifacts/m1/m1-fixture-20261009T162703Z-4ddb7a` | 0 | `report.json`：**被本機環境截斷**，見下方說明 |
  | 同一個 make 目標，經 `rtk proxy make`（不過濾） | 0 | `report-make-raw.json` |
  | `go build -o <scratch>/verify-demo-ff776bd ./cmd/verify-demo` 後直接執行 | 0（程式本身的結束碼） | `report-binary.json` |

  - `report.json` 的截斷：本機 shell 掛鉤會把 `make` 改寫成 `rtk make`，長輸出被縮短（「lines truncated」），所以不是完整的 JSON。原樣保留，沒有覆寫；完整報告是後兩份。
  - 後兩份報告除了 checker 的 build 欄位之外完全相同。
- 報告內容：
  - verdict **PASS**；V1～V7 與 R 都是 PASS；
  - 31 案全部 PASS（13 案 host_e2e 各兩份紀錄、3 案 Store、15 案契約）；
  - 七個 capability 為 not_evaluated；
  - 與產生端沒有差異；六項整合要求都是 PASS；
  - checker 的 build 是 `ff776bd…`，標為 `(modified)`，照實保留，不是乾淨建置。原因說明：工作區有未追蹤的 `demo`（`git-status.txt`），Go 把它也算成修改。

**判定與歸屬**：以上全部是 Claude Code 執行，待 Codex 限定 review。這不是 E1 的驗收，也不代表 B1-07 或 M1 已完成：
- live 包（E3）尚未支援；
- reviewer 的挑戰尚未進行。

### 15.5 限制

- 檢查只能看證據包裡有的東西：
  - hash 與事件鏈從包本身重算，整份一致重造的包無法分辨，需要外部錨點；
  - 被一致截斷的尾端也只能部分偵測：E2E 有 API 的 `as_of_sequence` 可以對照，Store 層的紀錄沒有 `chain_head`；
  - 包裡沒有記錄的副作用，無法證明它不存在。
- 來源資訊齊全，不等於證明了該 commit 產生了這些資料。
- 必要條件只檢查每個 reason 所需的條件，不是完整的 Policy 引擎。時間相關的條件（credential 的 `valid_until`、intent 的有效期間），以決策事件的 `observed_at` 作為當下時間。
- 契約案例被接受時的 reason 是案例清單的命名，沒有可觀測的 code。
- 整合要求的 PASS 只表示包內有那些測試的通過紀錄，檢查器沒有自己操作瀏覽器。
- live 模式回報 INCONCLUSIVE（不支援），E3 時再接上；檢查器沒有假設開發展示 plugin 例外已獲 M1 核准。
- 經 `make` 或 `go run` 時，外層會改寫非零的結束碼。
- 在這個環境，`make` 還會被 shell 掛鉤改寫並截斷輸出；需要完整報告時，直接執行編好的程式，或用不過濾的方式執行。

### 15.6 Codex review R1～R3 補正（10/10，Claude Code；待 Codex 限定複核）

> 依 `Claude-Code-M1-E2檢查器R1-R3補正派工-fd3cc90-20261010`。E2 前輪暫不通過。15.1～15.5 是前輪的原始紀錄，原樣保留。E1 包沒有重產。

| 項目 | 問題 | 修正 | 證據 |
|---|---|---|---|
| R1 golden 不完整 | golden 的 manifest 索引了 `browser/browser-test.log`，但它被 `.gitignore` 的 `*.log` 忽略，沒有進 Git；乾淨匯出時 golden 判 INCONCLUSIVE，`TestExitCodes` 回 2 | `.gitignore` 只為這一個路徑加例外（`!testdata/verification/m1-fixture-20261009T162703Z-4ddb7a/browser/browser-test.log`），其他 `.log` 仍忽略；提交該檔（內容掃描沒有 token 形狀的值；有一行本機的 Unity 建置路徑，是證據原文，沒有改） | `git archive 973a58d` 匯出到新的暫存目錄，`go test -race -count=1 ./internal/verification/... ./cmd/verify-demo/...` 結束碼 0，golden 的 57 個檔都在（55 個索引證據，加上 manifest 與結束標記；`testdata/verification` 另有 README 與 expected.json，共 59 個。原 E1 包是 81 個檔，兩者不同）（`artifacts/m1-verify/e2-r1r3-20261010T032429Z/clean-export-*`） |
| R2 E2E 執行紀錄沒有實質核對 | `traces`、`host_runs` 讀入卻沒核對；API 與 Store 只比部分欄位 | E2E 案例必須有 host run 與 run trace（缺 → INCONCLUSIVE V4）。逐項對照 Store（矛盾 → FAIL）：<br>• host run：task、consumer、state、結束方式；<br>• trace：task、gate（fixture 必須 strict／passed）、結束方式、工具呼叫編號、每個 `request_purchase` 的 purchase、裁決與事件序號；<br>• API 的每筆請購：訂單欄位（request_id、method、URL、chain、asset、asset_id、pay_to、amount、intent／requirement digest）、原始裁決、決策時間、期限、核准關閉狀態與時間，以及 API task 的身分。<br>結束方式的規則：CLI 逾時只由 run 記錄，所以必須是 timeout；沒有生命週期事件必須是 ok；主持人取消可能停掉進行中的 run（cancelled，且不早於取消），也可能在 run 結束後（ok，取消不早於 run 結束）。沒有請購的合法流程不要求 `request_purchase` | `TestE2ERunRecords`：複核的五例——刪 `traces`、刪 `host_runs`（INCONCLUSIVE）；API 裁決改為 Deny、API 金額改為 1、trace 的 `request_purchase` 改為 Deny（FAIL）。另外五例：trace 指向別的事件、host run 說 timeout、cancelled 的 run 早於取消就結束、API 少了核准關閉、restart 第二個 task 沒有 trace。每例都重算索引與結束標記，並斷言 V1 PASS。正例：ask、cancel-no-purchase、cli-timeout、cancel-pending、cli-timeout-pending、restart 都 PASS |
| R3 掃描中止被當成通過 | 整合要求的紀錄解析沒有檢查 `Scanner.Err()`，前段的 PASS 會掩蓋超長行之後的失敗 | 掃描錯誤（超過有界的 16 MiB token，或讀取錯誤）回報為 `incomplete`：INCONCLUSIVE，不 PASS；錯誤之前已讀到的失敗仍判 FAIL。上限不放寬。索引說 pass 而紀錄讀不完時判 INCONCLUSIVE，不判矛盾 | `TestRequirementLogReadToTheEnd`：只有超長行 → INCONCLUSIVE；超長行之後才有失敗（複核的反例）→ INCONCLUSIVE；失敗在超長行之前 → FAIL。三例都重算索引，V1 PASS |

**結束碼與 verdict**：沒有變。解析不完整時是 INCONCLUSIVE（結束碼 2），不會是 PASS；已有明確 FAIL 時是 FAIL（結束碼 1），不會被不完整掩蓋。

**反向驗證**：`python3 scripts/verify/m1-e2-reverse.py [名稱過濾]`，增為 46 項，新增 9 項：
- R2：E2E 紀錄整體、必須存在、API 請購、trace 裁決、trace 事件、run 結束方式、取消時間；
- R3：掃描錯誤；
- 另外，R index 的錨點隨條件更新。

過程：
- **第一次**被我中斷；中斷時「V1 盤點違規」這一項的 mutation 留在 `verify.go` 沒有還原。
- **第二次**因此在這一項報告錨點找不到，結果 45／46。發現後以 `git restore` 把 `verify.go` 還原成已提交的版本（這一輪本來就沒有要改它），兩個套件重測通過。
- **第三次**完整重跑：45／46。沒抓到的是「run 結束方式對照生命週期」：同一條規則也套用在 trace 上，只拿掉一處會被另一處抓到。腳本把兩處一起拿掉後，只重跑這一項，1／1 以斷言抓到。程式沒有改動。
- 合計 **46／46 以斷言失敗抓到**。

**定向回歸**：
- `go vet ./...` 通過；
- `go test -race -count=1 ./internal/verification/... ./cmd/verify-demo/...` 在開發副本與乾淨匯出都通過。

**既有包唯讀驗收**：證據在 `artifacts/m1-verify/e2-r1r3-20261010T032429Z/`，`NOTE.txt` 說明每個檔案。
- checker 是 `973a58d`；build 標 `(modified)`，照實保留，不是乾淨建置。原因說明：工作區有未追蹤的 `demo`，Go 把它也算成修改。
- `make verify-m1`（經 `rtk proxy`，不截斷）結束碼 0；編好的程式直接執行，結束碼 0；兩份報告除了 build 欄位外相同。
- 結果：verdict PASS；V1～V7 與 R 都是 PASS；31 案全部 PASS；七個 capability 為 not_evaluated；沒有 finding；與產生端沒有差異。
- 原包 81 個檔的 sha256 前後相同。

**Commit**：修正 `48a43bc`，測試、`.gitignore` 與 golden log `973a58d`，文件為本小節。

**限制**：
- 這些是包內可見資料的一致性檢查，不是簽章或外部錨點；整份一致重造的包仍然無法分辨。
- host run 與取消的時間只有秒級，兩者同一秒時，兩種結束方式都接受。
- checker 的規則版本字串仍是 `m1-e2.1`，請以 build 的 commit 辨識版本。
- 沒有執行：完整容器七步、311 項突變、瀏覽器、Unity、真實 CLI、`make m1-fixture`、E3／E4。

### 15.7 限定驗收紀錄（10/10，Codex 限定技術複核；Claude Code 文件同步）

**結論**：R1～R3 關閉；M1-E2 的 fixture 子範圍通過限定技術複核。基準為程式 `48a43bc`、測試 `973a58d`（之後的 `817eba2` 只有文件）。這不代表 B1-07 或 M1 完成：
- B1-07 只完成 fixture 子範圍，整體仍是 InProgress；
- E3（live 單筆 Ask）待另行派工；
- M1 live 證據是否接受開發展示 plugin 例外，仍待 Aaron 決定。

**本輪確認**：
- **R1**：缺少的 golden log 已納入 Git，乾淨匯出包含該檔。
- **R2**：缺 trace 或 host run 判 INCONCLUSIVE；API 裁決、金額或 trace 結果矛盾判 FAIL。
- **R3**：超長行造成解析中止時，不再把成功的前段當成完整 PASS。

**證據歸屬**：
- **Codex 獨立執行**：
  - 在斷網、來源唯讀的容器中，前輪的六個反例全部通過；
  - 原 E1 包經獨立核對，31 案、V1～V7 與整合要求都是 PASS；
  - 原包 81 個檔案的雜湊沒有變。
- **Claude Code 執行、Codex 核對紀錄**：
  - 乾淨匯出的兩個套件以 `-race` 通過；
  - 兩份完整驗收報告一致（第 15.6 節，`artifacts/m1-verify/e2-r1r3-20261010T032429Z/`）。
- **保留的限制**：
  - Codex 重跑整個 `internal/verification` 套件時碰到 150 秒的時間上限，不能記為通過；`cmd/verify-demo` 通過。
  - 46 項反向驗證沒有由 Codex 重跑，維持 Claude Code 的執行歸屬。

**文字修正**（不改證據，只修文件的說法）：
- golden 是 **57 個檔案**（55 個索引證據，加上 manifest 與結束標記），不是 59 個。59 是 `testdata/verification` 的總數（另含 README 與 expected.json），也不能和原 E1 包的 81 個檔案混淆。第 15.6 節已更正；證據目錄裡的檔案維持原樣。
- `build=(modified)` 照實保留；未追蹤的 `demo` 只是原因說明，不能據此稱為乾淨建置。第 15.4、15.6 節的說法已調整。證據目錄中 `NOTE.txt` 的原文維持原樣，以本節為準。

**本節是文件同步**：沒有重跑測試，沒有產包，沒有改程式或證據。
