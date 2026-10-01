# B1-05 節點 2 設計：持久化與事件查詢

**狀態（2026-10-01）**：設計待 review，**尚未實作**。本文件不安裝 driver、不改依賴、不啟動服務；文中需要下載或重建映像的步驟都待核准後才執行。

範圍是把節點 1 的 `policy.Decide` 接到持久化：請購去重與衝突拒絕、原子寫入、事件與 hash chain、取消／CLI 逾時／approval 逾時、重啟恢復、A／B 隔離與事件查詢。不改已接受的契約、schema 與 `cases.json`，不新增事件型別。

## 1. 儲存選擇

**SQLite 是唯一的持久化來源**；JSONL 只當日後的匯出格式，本節點不實作。

理由：請購、去重索引、裁決、事件與 task 狀態必須一起成功或一起失敗。SQLite 的交易在當機或斷電時仍是全有或全無（[Atomic Commit In SQLite](https://www.sqlite.org/atomiccommit.html)、[transactional](https://www.sqlite.org/transactional.html)），唯一索引由資料庫強制。但資料庫**不會**替本專案做授權隔離、事件驗證或決定交易邊界，這些在下文逐項說明。

### 1.1 Go driver

| 候選 | 版本（2026-10-01 查 proxy.golang.org） | 需要 CGO | 授權 | 備註 |
|---|---|---|---|---|
| **`modernc.org/sqlite`（建議）** | v1.60.1（2026-09-29）；前一版 v1.60.0 | 否（「CGo-free port of the C SQLite3 library」） | BSD-3-Clause | 內含 SQLite 3.53.4；`database/sql` driver 名稱 `sqlite`；DSN 支援 `_txlock=immediate` 與 `_pragma` |
| `github.com/ncruces/go-sqlite3` | v0.35.6（2026-09-23） | 否（Wasm 版 SQLite 經 wasm2go 轉成 Go） | MIT | 必要依賴只有 Go 與 `x/sys`；仍是 v0.x |
| `github.com/mattn/go-sqlite3` | v1.14.52（2026-09-05） | **是**（需 `CGO_ENABLED=1` 與 gcc） | MIT | 驗證映像雖有 gcc，但 CGO 讓建置依賴 C 工具鏈，不採用 |

建議 `modernc.org/sqlite`：v1 穩定 API、標準 `database/sql`、不需 CGO，交易鎖模式可在 DSN 指定。代價是傳遞依賴較多（v1.60.1 的 go.mod：`modernc.org/libc`、`mathutil`、`memory`、`fileutil`、`golang.org/x/sys`、`github.com/google/pprof`、`dustin/go-humanize`、`google/uuid`、`mattn/go-isatty`、`ncruces/go-strftime`、`remyoudompheng/bigfft`）。`modernc.org/sqlite` 與 `modernc.org/libc` 在 pkg.go.dev 標示 BSD-3-Clause；其餘傳遞依賴的授權在實際加入時逐一列入 `go.sum` 對照紀錄。

**待決**：v1.60.1 發布僅兩天。若 reviewer 偏好較長的實際使用期，改釘較早的 v1 版本；版本在實作 commit 內固定，`go.sum` 一併提交。

### 1.2 對隔離驗證與建置的影響

這是專案第一個第三方 Go 依賴。目前容器驗證以 `--network none`、`GOPROXY=off`、空的 `GOMODCACHE` 執行，加入依賴後**現行映像會建置失敗**。

| 方案 | 做法 | 取捨 |
|---|---|---|
| **A. 映像內預載模組（建議）** | `build-image.sh` 的準備階段（已允許網路拉取 base image）把 `go.mod`／`go.sum` 複製進 Dockerfile，`go mod download` 並以 `sum.golang.org` 驗證；模組放在映像的唯讀路徑，測試階段複製到 `/scratch` 使用，仍 `--network none`、`GOPROXY=off`、`-mod=readonly`。新映像標籤 `apa-verify:b1-05`；`container-verify.sh` 比對映像記錄的 `go.sum` 雜湊與受測 commit 一致，不一致即失敗 | 測試階段維持斷網；每次依賴變動都要重建映像 |
| B. 提交 `vendor/` | `go mod vendor`，`-mod=vendor` | 不需改映像，但 repo 體積會明顯增加（`modernc.org/sqlite` 含多平台產生碼，實際大小需實測），review diff 很大 |
| C. 掛載主機模組快取 | — | 違反「不掛載主機家目錄」，不採用 |

主機端：第一次 `make check` 需連 `proxy.golang.org`／`sum.golang.org` 下載模組。README「沒有任何第三方 Go 套件」的敘述屆時更新。

**待核准的操作**（本輪都未執行）：`go get modernc.org/sqlite@<版本>`、修改 `go.mod`／`go.sum`、修改 `scripts/verify/Dockerfile` 與 `build-image.sh`、重建驗證映像（會拉取已釘 digest 的 base image 並下載模組）。

## 2. 資料表

全部用 `STRICT` 表。schema 版本記在 `PRAGMA user_version`；migration 以 Go 內嵌、只往前、在單一交易內套用。開檔時版本比程式新或未知就拒絕開啟，不嘗試降版。

```sql
-- 可信設定快照：只存公開或已遮罩的欄位，以型別化 struct 重新序列化，不存原始設定檔
CREATE TABLE config_snapshots (
  snapshot_hash TEXT PRIMARY KEY,           -- sha256 of canonical body（儲存層內部用，不是契約 digest）
  kind          TEXT NOT NULL CHECK (kind IN ('registry', 'profile', 'credential')),
  body          TEXT NOT NULL,              -- canonical JSON
  recorded_at   INTEGER NOT NULL
) STRICT;

CREATE TABLE tasks (
  task_id             TEXT PRIMARY KEY,
  consumer_id         TEXT NOT NULL,
  agent_id            TEXT NOT NULL,
  wallet_id           TEXT NOT NULL,
  registry_snapshot   TEXT NOT NULL REFERENCES config_snapshots,
  profile_snapshot    TEXT REFERENCES config_snapshots,     -- NULL＝缺設定（CONFIG_MISSING）
  credential_snapshot TEXT REFERENCES config_snapshots,     -- NULL＝缺設定
  asset_id            TEXT,                                 -- 計價資產（來自 profile）
  budget_initial      TEXT NOT NULL,                        -- 最小單位十進位字串
  budget_remaining    TEXT NOT NULL,                        -- M1 永遠等於 budget_initial
  status              TEXT NOT NULL,                        -- running / awaiting_approval / denied / cancelled / failed
  deny_reason         TEXT,                                 -- 第一個 Deny 寫入後不再改
  next_sequence       INTEGER NOT NULL,
  chain_head          TEXT NOT NULL,                        -- 最後一個事件的 EventHash
  started_at          INTEGER NOT NULL
) STRICT;

CREATE TABLE purchases (
  task_id           TEXT NOT NULL REFERENCES tasks,
  purchase_order_id TEXT NOT NULL,
  retry_key         TEXT NOT NULL UNIQUE,   -- PurchaseOrder.RetryKey()：(consumer, task, wallet, request_id)
  chain_id          TEXT NOT NULL,
  asset_contract    TEXT NOT NULL,
  payer             TEXT NOT NULL,
  eip3009_nonce     TEXT NOT NULL,
  body              TEXT NOT NULL,          -- canonical PurchaseOrder
  intent_hash       TEXT NOT NULL,
  policy_verdict    TEXT NOT NULL,          -- 原始裁決，寫入後不改
  approval_status   TEXT NOT NULL,
  reason            TEXT NOT NULL,
  decided_at        INTEGER NOT NULL,       -- POLICY_DECIDED 的 observed_at（Ask 計時起點）
  approval_closed   TEXT,                   -- NULL / expired / withdrawn
  closed_at         INTEGER,
  PRIMARY KEY (task_id, purchase_order_id),
  UNIQUE (chain_id, asset_contract, payer, eip3009_nonce)
) STRICT;

CREATE TABLE events (
  task_id           TEXT NOT NULL REFERENCES tasks,
  sequence          INTEGER NOT NULL,
  event_id          TEXT NOT NULL UNIQUE,
  event_type        TEXT NOT NULL,
  purchase_order_id TEXT,
  body              TEXT NOT NULL,          -- canonical AuditEvent
  event_hash        TEXT NOT NULL,          -- contracts.EventHash(body)
  PRIMARY KEY (task_id, sequence)
) STRICT;
```

唯一範圍完全沿用 B1-02：retry key（同 scope）、`(task_id, purchase_order_id)`、`(chain_id, asset_contract, from, eip3009_nonce)`。語意判定一律呼叫 `contracts.CompareRetry`；資料庫唯一索引只是最後防線，違反時同樣回 `IDEMPOTENCY_CONFLICT` 並回滾。

原始裁決存在 `purchases` 且不回寫；approval 結束另記在 `approval_closed`，事件串流仍保有原本的 Ask。`event_id` 由 `task_id` 與 sequence 決定性產生（雜湊後截短，符合 ID 格式），重算與比對不依賴隨機值。事件 `source` 為 host，`evidence_mode` 為 `fixture`。

## 3. 寫入流程與交易邊界

連線設定：`journal_mode=WAL`、`synchronous=FULL`（WAL 下 FULL 才保證已提交交易在斷電後仍在；[PRAGMA synchronous](https://www.sqlite.org/pragma.html#pragma_synchronous)）、`foreign_keys=ON`、`busy_timeout`。所有寫入交易以 `BEGIN IMMEDIATE` 開始（DSN `_txlock=immediate`），一開始就取得寫鎖（[BEGIN IMMEDIATE](https://www.sqlite.org/lang_transaction.html)）。**判定所需的可信狀態都在同一個交易內讀取**，沒有交易外的先查後寫；同一個檔案被另一個行程開啟時，也由 SQLite 的檔案鎖序列化。

### 3.1 提交請購 `Submit(session, purchase, now)`

交易外（純函式、無狀態）：`contracts.Decode` 解析請購。session 由呼叫端傳入，**不可從請購內容建立**。

交易內，依序：

1. 讀 task 列；不存在或 `consumer_id／agent_id` 與 session 不符 → 拒絕。
2. 以該 task 的 registry 快照執行 `Registry.ValidatePurchase(session, purchase)`：**先驗可信歸屬**（`OWNERSHIP_MISMATCH`）。
3. 以 retry key 查既有請購：
   - 存在 → `CompareRetry`：`replay` 就回傳既有原始裁決與目前 approval 狀態，**不呼叫 Policy、不寫事件**，即使 task 已結束也能取得；內容不同 → `IDEMPOTENCY_CONFLICT`。
4. 以 `(task_id, purchase_order_id)` 與 nonce 查既有請購 → 用 `CompareRetry` 判定，衝突即 `IDEMPOTENCY_CONFLICT`。
5. 確定是新請購後才套用 `contracts.CheckTaskActive(task.status)`（`TASK_NOT_ACTIVE`）。
6. 先處理該 task 已到期的 Ask（見 3.3），再以快照組出 `policy.Input`（profile／credential 快照、`Budget{task_id, budget_remaining}`、`now`）呼叫 `policy.Decide`。
7. 寫入 `purchases`、`PURCHASE_PROPOSED` 與 `POLICY_DECIDED` 兩個事件（sequence、`prev_event_hash` 取自 `tasks.next_sequence`／`chain_head`），更新 task 狀態：Ask → `awaiting_approval`；Deny → `denied` 並寫 `deny_reason`；Allow → 維持 `running`。
8. 寫入前以 `ValidateEvent` 驗每個新事件，並對「既有串流＋新事件」執行 `ValidateEventOrder`；不通過就回滾。

任何一步失敗都回滾，不留下半份請購、孤立事件或只更新一半的 task。M1 不扣 `budget_remaining`。

### 3.2 生命週期

| 操作 | 交易內寫入 | task 狀態 |
|---|---|---|
| `StartTask(session, now)` | 記錄 registry／profile／credential 快照與初始額度，寫 `TASK_STARTED` | `running` |
| `Cancel(session, now)` | 每個仍開著的 Ask 先寫 `APPROVAL_WITHDRAWN`，再寫 `TASK_CANCELLED`（`HOST_CANCELLED`） | `cancelled`；若已 `denied` 則保持 `denied` 與原 `deny_reason` |
| `CLITimeout(session, now)` | 寫 `TASK_TIMED_OUT`（`CLI_TIMEOUT`）；**不撤回** 開著的 Ask（案例 `cli-timeout-pending` 預期 approval 仍為 pending） | `failed`；若已 `denied` 則保持 |

每個 task 最多一個 `TASK_CANCELLED`／`TASK_TIMED_OUT`，由 `ValidateEventOrder` 強制。收尾事件不改寫任何裁決，task 投影保留原本的 Deny 與原因。

### 3.3 Ask 到期

規則沿用已接受的語意：計時起點是 host 寫入的 Ask `observed_at`（`purchases.decided_at`），`now - decided_at >= approval_deadline_seconds` 即到期。到期在下列時機於同一個寫入交易內處理：該 task 的任何寫入操作之前，以及 host 定期呼叫的 `ExpireDue(now)`。到期寫 `APPROVAL_EXPIRED`（`APPROVAL_TIMEOUT`），`approval_closed = expired`。依契約，approval 逾時不算 task 結束；沒有其他開著的 Ask 時 task 回到 `running`。

重啟不會重新計時：起點存在資料庫，重新開啟後照原起點判定。既有裁決也不會因重新載入而重跑 Policy。

## 4. 可信狀態與隔離

- **快照**：開始 task 時記錄 registry、profile（含版本 `profile_id`）、credential 條件與初始額度的快照，該 task 之後都用自己的快照。重啟後換了設定檔，既有 task 與既有結果不變；新 task 才用新設定。
- **快照只存必要欄位**：由型別化 struct 重新序列化（`Credential` 只有 `consumer_id`、`valid_until`、`evidence_mode`），設定檔多出的欄位在載入時就被 `contracts.Decode` 拒絕，不會經由快照保存隱藏欄位或秘密。
- **額度綁定 task 與計價資產**：`budget_remaining` 屬於該 task 與 `asset_id`；`Decide` 已拒絕不同 task 的額度與非計價資產。
- **A／B 隔離**：所有讀寫都以 session 的 `consumer_id`＋`task_id` 限定；A 讀寫 B 的 task 直接拒絕。retry key 含 consumer／task／wallet，同名 request ID 不會互相命中。
- **可信 session 的來源**：B1-04 完成前，節點 2 的測試直接在程式內建立 `contracts.Session`，這是**測試用可信 context**，不代表真實登入或 caller 認證已完成。節點 2 不提供任何以 HTTP 寫入的端點。

## 5. 事件查詢

| 介面 | 呼叫者 | 範圍 |
|---|---|---|
| `Store.Task(session, task_id)`、`Store.Events(session, task_id)` | wrapper（B1-04）與測試 | 只能讀 session 自己的 task |
| 主持人檢視（例如 loopback 的 `GET /host/tasks/{task_id}/events`） | 展示層（B1-06） | 主持人可看所有 task；**不得**經由 B1-04 的工具橋接暴露給 agent |

讀取在唯讀交易內完成，回傳 canonical JSON，以 `encoding/json` 輸出（`<`、`>`、`&` 會跳脫）。**待決**：主持人檢視的 HTTP 端點放在節點 2 還是 B1-06，以及它在 M1 是否需要額外的本機 token。

## 6. 遮罩與注入

- 事件 payload 依契約只有 code／decision，沒有自由文字欄位；scope 的 ID 都受 ID 格式限制。
- 商家提供的字串只會出現在 `purchases.body` 的報價裡，當資料保存，不進 log。log 只記 ID、事件型別與 reason code，不記原始請購或報價。
- 不寫入 credential 隱藏欄位、token 或私鑰：型別上就沒有這些欄位。
- HTML 的跳脫屬展示層（B1-06）；節點 2 確保 JSON 輸出不會讓惡意字串變成可執行內容，並以測試檢查。

## 7. 重啟恢復與損壞處理

開檔時，在接受任何操作之前：

1. `PRAGMA quick_check` 與 `user_version` 檢查。
2. 每個 task：依 sequence 讀出事件，以 `contracts.Decode` 解析，重算 `EventHash` 與 `prev_event_hash` 連結，對整條串流執行 `ValidateEvent`（對照該 task 的 registry 快照與請購）與 `ValidateEventOrder`。
3. 核對 `tasks.chain_head`、`next_sequence` 與最後一個事件一致；每筆請購都有對應的 `PURCHASE_PROPOSED`／`POLICY_DECIDED`，intent hash 與裁決相符；由事件重算的 task 狀態與 `deny_reason` 等於存放值。

任何不一致都**拒絕服務**並回報是哪個 task、哪項檢查，不自動修補、不宣稱恢復成功。**待決**：整個資料庫拒絕開啟（建議，M1 最簡單也最保守），或只隔離出問題的 task。

**限制**：鏈頭與事件放在同一個資料庫。可以偵測中段修改、刪除、插入與重排，也能發現只刪了尾端事件卻沒同步改鏈頭的不一致；但能同時改寫事件與 `chain_head` 的人可以截斷尾端或重算整條鏈而不被發現。沒有外部 checkpoint 前，不宣稱能偵測這兩種情況。

## 8. 測試計畫

全部以 `go test -race` 執行；並行測試開多個 `Store`（各自的連線）指向同一個資料庫檔，模擬跨連線競爭並重複多輪。寫入中途失敗用測試專用的注入點（`export_test.go` 設定，正式程式不可開啟）；當機以子行程在交易中途 `os.Exit` 模擬，再由父行程重新開檔檢查。

| # | 情境 | 預期 | 驗證方式 |
|---|---|---|---|
| 1 | 相同請購循序與並行重送 | 只有一列請購、一組 `PURCHASE_PROPOSED`／`POLICY_DECIDED`；其餘呼叫拿到 `replay` 與相同裁決 | 多 goroutine／多 Store 同時送出，查列數與事件數 |
| 2 | 同 key 不同內容、purchase ID 重用、nonce 重用 | `IDEMPOTENCY_CONFLICT`，資料表與事件不變 | 衝突前後比對全部列數與 `chain_head` |
| 3 | task 結束後的合法 replay | 先驗歸屬，再回傳原結果；新的請購 `TASK_NOT_ACTIVE`；別人的 session 重送同一筆 → `OWNERSHIP_MISMATCH` | Deny 後重送原請購與新請購 |
| 4 | Deny 與新請購並行 | 串流仍通過 `ValidateEventOrder`；Deny 之後沒有新的裁決 | 並行多輪，每輪檢查不變式（不假設先後） |
| 5 | 寫入中途失敗與當機 | 不留下半份請購或孤立事件；重新開檔通過第 7 節檢查 | 注入點在每個寫入步驟後失敗；子行程在提交前結束 |
| 6 | Ask 在 deadline 前、恰好、超過，以及跨重啟 | 29 秒 pending、30／31 秒 expired；重啟後依原起點判定 | 注入時鐘；在 deadline 前關閉、到期後重新開檔 |
| 7 | Deny 後取消、Deny 後 CLI 逾時 | 收尾事件寫入，task 投影仍為 denied 與原 reason，裁決未改 | 讀 task 投影與事件 |
| 8 | A／B 隔離 | 同名 request ID 各自是新請購；A 讀寫 B 的 task 被拒；切換 profile 並重啟後既有 task 不變 | 以兩個 session 交叉操作 |
| 9 | 事件鏈竄改 | 中段修改、刪除、插入 → 開檔失敗；同時改寫尾端與 `chain_head` → **不偵測**，以測試記錄這個限制 | 直接以 SQL 修改資料庫檔後重新開檔 |
| 10 | 遮罩與注入 | 報價含 `<script>`、換行、U+2028 時，JSON 輸出已跳脫、log 不含原文；設定檔多出秘密欄位 → 載入失敗 | 檢查輸出位元組與擷取的 log |
| 11 | M1 無付款能力 | `budget_remaining` 不變；沒有 APPROVED／GRANT_ISSUED／SETTLED／RESOURCE_DELIVERED；credential 快照為 `fixture` | 每個情境結束時檢查 |

另以 `cases.json` 的 16 個 integration 案例在儲存層執行（不含真實 CLI）：8 個 policy 案例、取消兩案、approval 逾時三案、CLI 逾時兩案、重啟後同一 consumer 的新 task 沿用同一錢包。這是儲存層證據；端到端執行仍屬 B1-06。新增的 mutation 會覆蓋交易內的歸屬、replay、衝突、active-task 檢查、到期與收尾規則。

## 9. 最小可驗收範圍

- `internal/audit`（或 `internal/store`）的 `Store`：`Open`、`StartTask`、`Submit`、`Cancel`、`CLITimeout`、`ExpireDue`、`Task`、`Events`，以及第 7 節的開檔驗證。
- 第 8 節的 11 項測試與 16 個 integration 案例在儲存層通過；mutation 與容器驗證更新後通過。
- 不含：JSONL 匯出、B1-04 的 caller 認證、外部 checkpoint、主持人檢視 HTTP 端點（視待決事項）。

## 10. 待決事項

1. **Driver 與版本**：`modernc.org/sqlite` v1.60.1 或較早的 v1 版本；或改用 `ncruces/go-sqlite3`。
2. **驗證映像**：方案 A（映像內預載模組）或 B（`vendor/`）；核准後才下載依賴與重建映像。
3. **approval deadline 設定**：目前 profile 沒有這個欄位。建議加 `approval_deadline_seconds` 並把 profile 升為 `m1-demo-v2`；demo 數值待定（案例以 30 秒驗證邊界）。這是 profile 變更，不是契約變更。
4. **資料庫路徑**：建議 `APA_DB_PATH`，預設 `var/apa.db`（`var/` 已在 `.gitignore`）；選填，不影響 B1-01 的啟動檢查。
5. **fixture 憑證與額度來源**：建議新增 `testdata/policy/m1-demo-credentials.json`。demo 若以真實時鐘執行，固定的 `valid_until` 會過期，需要決定用遠期值還是啟動時設定。
6. **開檔驗證失敗時**：整個資料庫拒絕開啟（建議），或只隔離問題 task。
7. **主持人檢視 HTTP 端點**：放在節點 2 或 B1-06，以及 M1 是否加本機 token。
8. **APPROVAL_EXPIRED 的 `observed_at`**：建議用 host 實際寫入的時間，不回填理論到期時刻。
9. **approval 逾時後的 task 狀態**：契約不把 approval 逾時算成 task 結束；本設計在沒有其他開著的 Ask 時讓 task 回到 `running`，可再提出新請購。這是實作者的選擇，請確認。
