# M1 fixture 證據包（M1-E1，`make m1-fixture`）

> 2026-10-09；依 `docs/m1-closeout-gaps.md` 第 4 節的 E1。只用 fake CLI：不執行模型、不連 testnet、不付款。
>
> **這是產出端，不是 B1-07**。包內的 PASS／FAIL／INCONCLUSIVE 是產生程式自己的判定；`RunManifest.ValidateAgainst` 只檢查索引。獨立核對由 B1-07（`make verify-m1`，M1-E2）負責。

## 1 用法

```sh
make m1-fixture                                   # 含 browser-ui（需要 Chrome，約 8 分鐘）
make m1-fixture M1_FIXTURE_FLAGS=-skip-browser    # 不跑瀏覽器；3.2 的瀏覽器檢查記為 not_run，結束碼 1
```

- 從 repo 根目錄執行，每次建立新的 `artifacts/m1/<run-id>/`（0700）。不會覆寫或重用舊目錄。
- run ID 是 `m1-fixture-<UTC 時間>-<6 個隨機 hex>`。
- 結束碼：
  - 0：每個命令的 exit code 都是 0，31 個案例都 PASS，3.2 的檢查全部 pass；
  - 1：證據包完整，但其中有失敗；
  - 2：無法產生證據包（見 `run-end.json` 的 state）。

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
| `provenance/config-files.json` | 設定檔逐檔 digest：`testdata/contracts/cases.json`、`testdata/contracts/registry.json`、`testdata/demo/registry.json`、`testdata/policy/m1-demo-profile.json`、`testdata/cli/ask-mainline.json` | `provenance` |
| `tests/contracts.jsonl` | `go test -json` 的 `TestContractCasesFromManifest`（15 個契約案例） | `contracts_test` |
| `tests/policy.jsonl` | `TestAcceptedPolicyCases`（policy 類案例的裁決） | `policy_test` |
| `tests/store.jsonl` | `TestAcceptedIntegrationCases`（16 個整合案例，Store 層） | `store_integration` |
| `tests/e2e.jsonl` | `cmd/demo` 的 `TestM1FixtureCases`（13 個 E2E 案例） | `host_e2e` |
| `e2e/<case_id>.json` | 每個 E2E 案例的證據檔（第 5 節） | `host_e2e` |
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
| `host_e2e`（13） | `ask`、`deny-budget`、`deny-merchant`、`deny-asset`、`deny-unattended`、`cancel-pending`、`cancel-no-purchase`、`cli-timeout`、`cli-timeout-pending`、`approval-timeout`、`approval-deadline-before`、`approval-deadline-exact`、`restart` | `e2e/<case>.json`、`tests/e2e.jsonl`，以及同一案例的 `tests/store.jsonl` |
| `store_integration`（3） | `deny-expired`、`deny-missing-config`、`allow-unavailable` | `tests/store.jsonl`；policy 類另加 `tests/policy.jsonl` |
| `contracts_test`（15） | `retry-*`、`consumer-*`、`cross-consumer*`、`altered-*`、`no-premature-settlement`、`reject-*` | `tests/contracts.jsonl`；**不當成端到端證據** |

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
  - 這些設定由測試從第 3 節的設定檔衍生，不另存成檔案；
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

E2E 的判定：
- 證據檔自己記錄的 verdict 是 PASS，對應的 E2E 測試通過，同一案例的 Store 測試也通過，才判為 PASS。
- 缺少證據檔或測試，或證據檔的 layer 不是 `host_e2e`：判為 INCONCLUSIVE。
- 其他情況：判為 FAIL。

## 5 E2E 證據檔（`e2e/<case_id>.json`）

| 欄位 | 內容 |
|---|---|
| `expected` | 取自 `cases.json` |
| `observed` | result、reason、approval_status、payment_status、budget_deducted、forbidden_events_found、purchases。對應方式與 Store 的 `purchaseOutcome` 相同 |
| `tasks` | `GET /api/tasks/{id}` 的回覆（host token） |
| `events` | `GET /api/tasks/{id}/events` 的回覆（host token）：API 自己的白名單投影 |
| `traces` | 每個 task 的 run trace，經歷史回放用的白名單載入（`loadTrace`，並與 Store 交叉核對）；讀不到時記為 `not_available` 與原因 |
| `host_runs` | `GET /api/host/runs`（operator token） |
| `budget` | 每個 task 的 `initial`（profile 的 task_cap，或目錄的 budget_initial）與 `remaining`（host 停止後，以唯讀方式讀資料庫） |
| `verdict` | 測試自己的判定 |

**不扣預算**：
- `budget_deducted` 是各 task 的 initial − remaining 之和，必須等於 expected 的 `"0"`；
- 前後快照就是 `budget` 欄位。

**沒有 forbidden 事件**：
- 每個 task 的事件回覆中，不能出現 expected 的 `forbidden_events` 任何一項；
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

沒有 `run-end.json`，或 digest 與 `manifest.json` 不符，就表示證據包不完整（例如產生到一半中斷）。

## 9 檢查

`cmd/m1-fixture/main_test.go` 不跑真正的命令，以合成的 run 目錄檢查：
- **索引**：全部通過時 `ValidateAgainst` 通過，`not_evaluated` 恰好是七個 capability；
- **確定性**：同一份輸入組兩次，除了 run ID 之外內容一致；
- **層級**：每個 claim 引用的 source 與第 4 節一致（13／3／15）；
- **反向檢查**：
  - 缺少 E2E 證據檔、證據檔標成 Store 層 → INCONCLUSIVE；
  - 證據檔自判 FAIL、E2E 測試失敗 → FAIL；E2E 測試缺少 → INCONCLUSIVE；
  - Store 測試失敗或略過、policy 測試失敗、E2E 案例的 Store 測試失敗 → FAIL 或 INCONCLUSIVE；
  - 契約測試缺少 → INCONCLUSIVE；
  - 清單外的檔案、symlink → 拒絕；
- **掃描**：
  - 不帶前綴的 64 位 hex 會被抓到；`sha256:`、`0x`、40 位的 commit 不會被當成 token；
  - 結束標記帶 manifest digest；不完整的結束標記不帶 digest；
- **3.2**：全部 pass；反向檢查失敗、缺少、前綴無相符、瀏覽器未執行。

## 10 已知限制與剩餘風險

- 判定是產生端自己的判定。B1-07 必須獨立重算：
  - 開檔防 symlink；
  - 核對 digest；
  - 對照 expected；
  - 檢查 hash chain 與 A／B 綁定（V1～V7）。
- 測試輸出含耗時，host 回覆含時間與 ID，所以兩次執行的證據檔只在這些欄位以外相同。
- `dirty_patch_digest` 不含 untracked 檔。
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
