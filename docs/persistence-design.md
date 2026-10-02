# B1-05 節點 2 設計：持久化與事件查詢

**狀態（2026-10-02）**：**B1-05 已結案（Done）**，範圍是已驗收的確定性 Policy（`61b3652`）＋Store（程式驗收基準 `cb33768`），Aaron 2026-10-02 確認；main 由 `adda78e` fast-forward 至 `7ad304a`。主持人事件檢視 HTTP 端點移至 B1-06（未實作，保留認證、A／B 隔離與整合測試要求）。結案不代表 M1 或端到端整合完成。驗收與結案紀錄見第 16 節。設計歷程：第三版設計 `f8bb427` 通過技術審查；實作 `64f247c` 經 review 修正 REV1／REV2 為 `cb33768`；實作對照見第 14 節，修正見第 15 節。

第三版的修訂內容：第二版（`adad831`）的 review 保留 D1～D4 的方向，要求局部修訂三項：歷史請購的 replay 不受目前報價簿影響（R1）、缺 profile 是合法的 `CONFIG_MISSING` 情境（R2）、`ExpireDue` 週期不是延遲上限（C1），對照見第 13 節。第一版（`c9199e7`）到第二版的 D1～D4 修訂對照保留在同一節。

範圍是把節點 1 的 `policy.Decide` 接到持久化：請購去重與衝突拒絕、原子寫入、事件與 hash chain、取消／CLI 逾時／approval 逾時、重啟恢復、A／B 隔離與受 session 限制的查詢。不改節點 1 的 Policy 行為、已接受的契約、schema、`cases.json` 與 golden vectors，不新增事件型別。

本文的「儲存層錯誤」（例如 `CLOCK_REGRESSION`、`QUOTE_NOT_OFFERED`）只是 Store 回給呼叫端的錯誤，**不是**契約的 reason code，不寫入任何事件。

## 1. 儲存、driver 與執行環境

### 1.1 選擇

**SQLite 是唯一的持久化來源**；JSONL 只當日後的匯出格式，本節點不實作。請購、去重索引、裁決、事件與 task 狀態放在同一個交易，當機或斷電時全有或全無（[Atomic Commit In SQLite](https://www.sqlite.org/atomiccommit.html)）。資料庫**不會**替本專案做授權隔離、事件驗證或決定交易邊界，這些在下文逐項說明。

### 1.2 Driver

採用 **`modernc.org/sqlite v1.60.1`**，固定版本：

- 不需 CGO（官方文件：「CGo-free port of the C SQLite3 library」），BSD-3-Clause，內含 SQLite 3.53.4，`database/sql` driver 名稱 `sqlite`，DSN 支援 `_txlock` 與 `_pragma`（[pkg.go.dev](https://pkg.go.dev/modernc.org/sqlite@v1.60.1)）。
- 依 [changelog](https://gitlab.com/cznic/sqlite/-/raw/v1.60.1/CHANGELOG.md)：v1.60.0 起需要 Go 1.26（本專案為 1.26.4），釘 `modernc.org/libc v1.77.1`，且「downstream `go.mod` files must pin the same `modernc.org/libc` version」；本專案的 `go.mod` 會同時釘 `modernc.org/libc v1.77.1`。
- 不退版：v1.60.0 修正了「讀取 WAL 資料庫的記憶體映射 `-shm` 檔出錯時讓行程當掉」的問題，本設計正好使用 WAL。
- 相容性尚未驗證（未下載）；獲准後才以實際建置與測試確認，不宣稱已驗過。

比較過的替代：`github.com/ncruces/go-sqlite3` v0.35.6（不需 CGO、MIT，仍是 v0.x）、`github.com/mattn/go-sqlite3` v1.14.52（需 CGO 與 gcc），不採用。

### 1.3 連線與 PRAGMA

PRAGMA 多數是**每條連線**各自生效，不能在連線池任一條連線執行一次就視為全域。做法：

- 兩個 `*sql.DB`：**寫入 handle**（`_txlock=immediate`，`SetMaxOpenConns(1)`）與**讀取 handle**（預設 deferred，`query_only=1`）。
- 每條連線需要的 PRAGMA 都寫在 DSN 的 `_pragma`，由 driver 在建立每條新連線時執行：`foreign_keys(1)`、`busy_timeout(5000)`、`synchronous(FULL)`；讀取 handle 另加 `query_only(1)`。
- `journal_mode=WAL` 存在資料庫檔本身，開檔時設定一次並讀回確認。
- 不只相信設定：測試同時開出多條連線，逐條讀回上述 PRAGMA 值。
- DSN 由程式以 `file:` URI 組成並跳脫路徑；設定只接受檔案路徑，不接受任何 DSN 或 URI 參數。

WAL 下 `synchronous=FULL` 才保證已提交交易在斷電後仍在（[PRAGMA synchronous](https://www.sqlite.org/pragma.html#pragma_synchronous)）；`BEGIN IMMEDIATE` 一開始就取得寫鎖（[BEGIN](https://www.sqlite.org/lang_transaction.html)）。

### 1.4 隔離驗證映像

這是專案第一個第三方 Go 依賴；現行容器驗證以 `--network none`、`GOPROXY=off` 與空的模組快取執行，加入依賴後**現行映像會建置失敗**。採用「映像內預載模組」：

- **準備階段**（已允許網路拉取 base image）：`build-image.sh` 把受測 commit 的 `go.mod` 與 `go.sum` 複製到一個暫時的 build context（只有這兩個檔與 Dockerfile，不是整個工作區，`.env` 不會進入），在映像內 `go mod download` 並以 `sum.golang.org` 驗證，模組放在映像的唯讀路徑。映像記錄 `go.mod` 與 `go.sum` 兩者的 SHA-256。新標籤 `apa-verify:b1-05`。
- **測試階段**：維持 `--network none`、`GOPROXY=off`、`-mod=readonly` 與既有隔離限制；模組由映像路徑複製到 `/scratch` 使用。`container-verify.sh` 比對映像記錄的 `go.mod`、`go.sum` 雜湊與受測 commit **兩者**都一致，否則失敗。
- **主機端**：第一次 `make check` 需連 `proxy.golang.org`／`sum.golang.org` 下載模組。README「沒有任何第三方 Go 套件」的敘述屆時更新。

**待核准的操作**（本輪都未執行）：`go get modernc.org/sqlite@v1.60.1`、修改 `go.mod`／`go.sum`、修改 `scripts/verify/Dockerfile` 與 `build-image.sh`、重建驗證映像。

### 1.5 第三方元件與授權

`go.sum` 是模組校驗資料，**不是**授權清單。加入依賴的同一個 commit 另建 `docs/third-party.md`，逐一列出每個模組（含傳遞依賴）的版本、授權與來源連結。v1.60.1 的 go.mod 列出：`modernc.org/libc`、`mathutil`、`memory`、`fileutil`、`golang.org/x/sys`、`github.com/google/pprof`、`dustin/go-humanize`、`google/uuid`、`mattn/go-isatty`、`ncruces/go-strftime`、`remyoudompheng/bigfft`。目前只確認 `modernc.org/sqlite` 與 `modernc.org/libc` 在 pkg.go.dev 標示 BSD-3-Clause，其餘待實際加入時核對。

### 1.6 資料庫路徑與權限

- `APA_DB_PATH`，預設 `var/apa.db`。這是可信 Host 設定；agent 沒有任何途徑指定路徑。
- 相對路徑以**行程啟動時的工作目錄**為基準，啟動時轉成絕對路徑並記錄在啟動 log；`make dev` 在 repo 根目錄啟動，所以預設落在 `<repo>/var/apa.db`（`var/` 已在 `.gitignore`）。
- 資料庫所在目錄以 `0700` 建立；目錄已存在但權限比 `0700` 寬、或資料庫路徑是 symlink 或非一般檔案時拒絕啟動。目錄權限同時保護資料庫、`-wal` 與 `-shm` 三個檔；實作時以測試讀回三者與目錄的權限。

## 2. 資料表

全部用 `STRICT` 表。schema 版本記在 `PRAGMA user_version`；migration 以 Go 內嵌、只往前、在單一交易內套用；開檔時版本比程式新或未知就拒絕開啟。

```sql
-- 可信設定快照：由型別化 struct 重新序列化，不存原始設定檔
CREATE TABLE config_snapshots (
  snapshot_hash TEXT PRIMARY KEY,           -- "sha256:" + hex(sha256(body))；儲存層內部用，不是契約 digest
  kind          TEXT NOT NULL CHECK (kind IN ('registry', 'profile', 'credential', 'terms')),
  body          TEXT NOT NULL,              -- canonical JSON
  recorded_at   INTEGER NOT NULL
) STRICT;
-- terms：{task_id, profile_state, credential_state, budget_initial, asset_id, approval_deadline_seconds}，每個 task 一份
--   profile_state／credential_state 為 "present" 或 "absent"，由可信 Host 明確宣告（5.2）

CREATE TABLE tasks (
  task_id             TEXT PRIMARY KEY,
  consumer_id         TEXT NOT NULL,
  agent_id            TEXT NOT NULL,
  wallet_id           TEXT NOT NULL,
  payer               TEXT NOT NULL,                        -- 錢包的 from，同一 consumer 沿用
  registry_snapshot   TEXT NOT NULL REFERENCES config_snapshots,
  profile_snapshot    TEXT REFERENCES config_snapshots,     -- NULL 只在 terms.profile_state = absent 時合法（CONFIG_MISSING）
  credential_snapshot TEXT REFERENCES config_snapshots,     -- NULL 只在 terms.credential_state = absent 時合法
  terms_snapshot      TEXT NOT NULL REFERENCES config_snapshots,
  budget_remaining    TEXT NOT NULL,                        -- M1 永遠等於 terms.budget_initial
  status              TEXT NOT NULL,                        -- 由第 4 節的投影寫入
  deny_reason         TEXT,                                 -- 由第 4 節的投影寫入
  next_sequence       INTEGER NOT NULL,
  chain_head          TEXT NOT NULL,                        -- 最後一個事件的 EventHash
  started_at          INTEGER NOT NULL
) STRICT;

CREATE TABLE purchases (
  task_id           TEXT NOT NULL REFERENCES tasks,
  purchase_order_id TEXT NOT NULL,
  request_id        TEXT NOT NULL,
  retry_key         TEXT NOT NULL UNIQUE,   -- PurchaseOrder.RetryKey()：(consumer, task, wallet, request_id)
  chain_id          TEXT NOT NULL,
  asset_contract    TEXT NOT NULL,
  payer             TEXT NOT NULL,
  eip3009_nonce     TEXT NOT NULL,
  body              TEXT NOT NULL,          -- canonical PurchaseOrder，原樣保存（第 6 節）
  intent_hash       TEXT NOT NULL,
  policy_verdict    TEXT NOT NULL,          -- 原始裁決，寫入後不改
  approval_status   TEXT NOT NULL,
  reason            TEXT NOT NULL,
  decided_at        INTEGER NOT NULL,       -- 等於 POLICY_DECIDED 的 observed_at（Ask 計時起點）
  approval_closed   TEXT,                   -- NULL / expired / withdrawn
  closed_at         INTEGER,                -- 等於收尾事件的 observed_at
  PRIMARY KEY (task_id, purchase_order_id),
  UNIQUE (chain_id, asset_contract, payer, eip3009_nonce)
) STRICT;

CREATE TABLE events (
  task_id           TEXT NOT NULL REFERENCES tasks,
  sequence          INTEGER NOT NULL,
  event_id          TEXT NOT NULL UNIQUE,
  event_type        TEXT NOT NULL,
  purchase_order_id TEXT,
  observed_at       INTEGER NOT NULL,
  body              TEXT NOT NULL,          -- canonical AuditEvent
  event_hash        TEXT NOT NULL,          -- contracts.EventHash(body)
  PRIMARY KEY (task_id, sequence),
  FOREIGN KEY (task_id, purchase_order_id) REFERENCES purchases
) STRICT;

-- 防線：每個請購最多一個裁決與一個 approval 收尾；每個 task 最多一個生命週期結束事件
CREATE UNIQUE INDEX one_decision ON events (task_id, purchase_order_id) WHERE event_type = 'POLICY_DECIDED';
CREATE UNIQUE INDEX one_closure  ON events (task_id, purchase_order_id) WHERE event_type IN ('APPROVAL_EXPIRED', 'APPROVAL_WITHDRAWN');
CREATE UNIQUE INDEX one_end      ON events (task_id) WHERE event_type IN ('TASK_CANCELLED', 'TASK_TIMED_OUT');
```

唯一範圍完全沿用 B1-02：retry key（同 scope）、`(task_id, purchase_order_id)`、`(chain_id, asset_contract, from, eip3009_nonce)`。語意判定一律呼叫 `contracts.CompareRetry`；資料庫唯一索引只是最後防線，違反時同樣回 `IDEMPOTENCY_CONFLICT` 並回滾。`event_id` 由 `task_id` 與 sequence 決定性產生（雜湊後截短，符合 ID 格式）。事件 `source` 為 host，`evidence_mode` 為 `fixture`。

## 3. 時間、鎖與操作時序（D2）

### 3.1 可信時鐘

- Store 開啟時由 Host 注入時鐘（正式為 `time.Now().Unix()`，測試為可控的假時鐘）。**任何操作都不從呼叫端接受 `now`。**
- 每個寫入交易只取樣一次時鐘，時點是 `BEGIN IMMEDIATE` 成功取得寫鎖**之後**；同一交易內所有事件的 `observed_at` 與 `Decide` 都用這個值。等鎖期間不會沿用排隊前的時間。
- 讀取交易不取樣時鐘。

### 3.2 寫入交易的共同骨架

每個寫入操作都是一個交易，依序：

1. `BEGIN IMMEDIATE`（等寫鎖，受 `busy_timeout` 限制）。
2. `now` ← 時鐘。
3. 時鐘檢查：`now` 不得小於資料庫目前記錄的最大時間（所有事件的 `observed_at` 與快照的 `recorded_at`），否則以 `CLOCK_REGRESSION` 中止（3.5）。
4. **授權**：session 操作在這裡驗 task 屬於 session（Submit 另以該 task 的 registry 快照執行 `ValidatePurchase`）；Host 操作驗 task 存在。授權失敗即回滾，**之前沒有任何寫入**，所以跨 consumer 或未認證的請求不可能經由到期處理修改別人的 task。
5. **到期處理**：只處理這個操作的目標 task（3.4）。
6. 操作本身的邏輯（`Submit` 的分類順序見 3.3 之後的說明）。
7. 追加事件；以第 4 節的共同規則重算投影；`ValidateEvent`＋`ValidateEventOrder` 驗新串流；投影欄位與規則結果一致才寫入。
8. `COMMIT`。任何一步失敗都 `ROLLBACK`，**包含第 5 步的到期事件**。

到期事件隨回滾消失沒有語意問題：下一個寫入操作或 Host 的 `ExpireDue` 會在自己的交易裡重新判定。Ask 的收尾結果只取決於「第一個成功收尾它的交易所取樣的 `now`」是否已達期限（3.4），記錄時間較晚只代表觀測較晚。

### 3.3 各操作

| 操作 | 呼叫者 | 歸屬驗證 | 到期處理 | 交易 | 失敗或回滾 |
|---|---|---|---|---|---|
| 啟動 | Host | — | 恢復驗證（第 8 節，讀交易）通過後，Host 立即執行 `ExpireDue`，之後才開始服務 | 驗證：單一讀交易；到期：每個 task 一個寫交易 | 驗證失敗就拒絕服務，不寫任何東西 |
| `StartTask` | Host | task 在 registry 且尚未開始；同一 consumer 沿用既有錢包（5.3） | 無（新 task） | 單一寫交易 | 全部回滾 |
| `Submit`（新請購） | session | 第 4 步：task 屬於 session，再 `ValidatePurchase` | 只處理該 task | 同一交易：到期 → 分類 → 新請購才檢查**目前**報價簿 → `CheckTaskActive` → `Decide` → 寫入 | 全部回滾，含到期 |
| `Submit`（合法 replay） | session | 同上，**先驗歸屬再辨識 replay** | 同上 | 同一交易；只以已保存的 canonical 請購比對；**不檢查目前報價簿**、不套用 active-task、不重跑 Policy、不寫請購或裁決事件；唯一可能的寫入是該 task 已到期 Ask 的 `APPROVAL_EXPIRED`，沒有到期就不寫任何列 | — |
| `Submit`（衝突） | session | 同上 | 同上 | 回 `IDEMPOTENCY_CONFLICT`；不論新內容是否在目前報價簿，都不改回 `QUOTE_NOT_OFFERED` | 回滾，到期由之後的交易重新判定 |
| `Submit`（新請購的報價不在目前報價簿） | session | 同上 | 同上 | 回 `QUOTE_NOT_OFFERED` | 回滾，含到期；不寫任何列 |
| `Task`／`Events` | session | 讀交易內驗 task 屬於 session | **不處理** | 唯讀交易（WAL 下為一致快照） | — |
| `Cancel` | Host（主持人） | task 存在，尚無生命週期結束事件 | 只處理該 task | 同一交易：到期 → 撤回仍開著的 Ask（`APPROVAL_WITHDRAWN`）→ `TASK_CANCELLED` | 全部回滾 |
| `CLITimeout` | Host（wrapper 監控 CLI） | 同上 | 只處理該 task | 同一交易：到期 → `TASK_TIMED_OUT`；**不撤回** Ask（案例 `cli-timeout-pending`） | 全部回滾 |
| `ExpireDue` | Host（啟動時與定時） | 不接受外部請求 | 每個 task 各自 | 每個 task 一個寫交易 | 只回滾該 task |

`Cancel`、`CLITimeout`、`ExpireDue` 是 Host 能力，不經 B1-04 的工具橋接暴露給 agent；`Submit`、`Task`、`Events` 是 session 能力。

**查詢的時間語意**：`Task`／`Events` 回傳「該 task 最後一次提交的寫入交易」時的一致快照，附 `as_of_sequence` 與 `as_of_observed_at`。開著的 Ask 附 `deadline_at`（`decided_at + approval_deadline_seconds`，理論到期點）。回應不宣稱已依現在時間更新；已過 `deadline_at` 但尚未寫入 `APPROVAL_EXPIRED` 的 Ask 仍顯示為 pending，**不**顯示成已收尾。

Host 以 1 秒為目標週期執行 `ExpireDue`；這**不是**最多延遲 1 秒的保證。鎖等待、排程延遲、交易回滾、掃描執行時間或錯誤都可能使處理落後。查詢回傳已提交的一致快照，保留 `as_of_sequence`、`as_of_observed_at` 與 `deadline_at`，不宣稱資料已依現在時間更新。服務啟動時先執行一次 `ExpireDue` 的設計不變；本設計不提供任何即時性或延遲上限的保證。

**replay 承諾的修訂**：第一版寫「replay 不寫事件」，精確版本是：replay 不重跑 Policy、不新增請購或原始裁決；交易骨架第 5 步可能為同一 task 寫入 `APPROVAL_EXPIRED`，這是生命週期推進，責任在 Store；回傳的 approval 狀態是該交易提交後的狀態。

**`Submit` 的分類順序（第三版，R1）**：在 3.2 的骨架內依序：

1. 交易外：`contracts.Decode` 嚴格解析請購（格式）。
2. 第 4 步授權：task 存在且屬於 session，再以該 task 的 registry 快照執行 `ValidatePurchase`（形狀、報價與 intent 一致、歸屬）。**請購已存在也不能跳過這一步**；失敗即回滾，不回傳任何既有請購的內容，也不觸發第 5 步。
3. 第 5 步：只處理該 task 已到期的 Ask。
4. 以 retry key 查既有請購；存在就以**已保存的完整 canonical 請購**與 `contracts.CompareRetry` 比對：
   - 相同內容 → 合法 replay：回傳原始裁決與（第 3 步之後的）目前 approval 狀態。不檢查目前報價簿、不套用 active-task、不重跑 Policy、不新增邏輯請購或原始裁決。
   - 不同內容 → `IDEMPOTENCY_CONFLICT`，回滾；不論新內容是否在目前報價簿。
5. retry key 不存在：以 `(task_id, purchase_order_id)` 與 `(chain_id, asset_contract, from, eip3009_nonce)` 查既有請購，用 `CompareRetry` 判定；衝突即 `IDEMPOTENCY_CONFLICT`，回滾。唯一範圍沿用 B1-02。
6. 確定是新請購：先檢查**目前**可信報價簿（6.1），不在其中即 `QUOTE_NOT_OFFERED`，回滾；再 `CheckTaskActive`；再 `Decide`；再寫入。

未授權、衝突與拒收都整個回滾，不留下部分寫入（含第 3 步的到期事件）。合法 replay 的交易只可能提交第 3 步的 `APPROVAL_EXPIRED`。

### 3.4 到期規則與收尾的先後

- Ask 的起點 `D = decided_at`（POLICY_DECIDED 的 `observed_at`），期限 `W` 取自該 task 的 terms 快照。**`now ≥ D` 且 `now − D ≥ W` 即到期**；沿用已接受的 `elapsed >= deadline` 規則。
- 到期事件的 `observed_at` 是交易實際取樣的 `now`，不倒填理論到期點 `D + W`。
- 交易骨架的第 5 步一定先於操作本身。因此在取樣的 `now ≥ D + W` 時，Ask 只能以 expired 收尾；只有 `now < D + W` 時 `Cancel` 才會撤回它。**不變式：Ask 以 expired 收尾，若且唯若第一個收尾它的交易取樣到 `now ≥ D + W`。**
- 到期只追加一次：寫入前在交易內確認 `approval_closed IS NULL`，部分唯一索引 `one_closure` 是最後防線。
- 重啟不重新計時：`D` 在資料庫裡，重新開啟後照原起點判定。

與已接受案例對照：`cancel-pending`（取消時尚未到期）→ withdrawn；`approval-deadline-before`（29 秒）→ pending；`approval-deadline-exact`（30 秒）、`approval-timeout`（31 秒）→ expired；`cli-timeout-pending` → task failed、Ask 仍 pending。案例都不需要修改。

### 3.5 時鐘倒退

- 寫入交易取樣到的 `now` 小於資料庫記錄的最大時間 → 以 `CLOCK_REGRESSION` 中止：不處理到期、不寫事件、不改任何歷史時間。讀取照常服務。
- 到期判定先確認 `now ≥ D` 才計算 `now − D`；加上第 3 步的檢查，不會發生無號減法下溢。
- 已終止的 task 不會因時鐘倒退復活：狀態只由事件推導（第 4 節），倒退時不寫任何事件。
- 恢復方式是主持人修正主機時鐘；Store 不自動調整。

### 3.6 邊界情境

| 情境 | 結果 |
|---|---|
| 停機期間跨越 deadline，重啟 | 恢復驗證通過後先執行 `ExpireDue`：寫 `APPROVAL_EXPIRED`，`observed_at` 為重啟後的時間；服務在這之後才開始，所以重啟後第一次查詢就看到 expired；第一次 replay 也在第 5 步看到已收尾的結果 |
| 恰好 deadline 時 `Cancel` 與 `ExpireDue` 競爭 | 寫鎖序列化。先取得鎖的交易若取樣到 `now ≥ D + W`，結果一律是 Ask expired，接著（或之後的 `Cancel`）只寫 `TASK_CANCELLED`，task 為 cancelled，沒有 `APPROVAL_WITHDRAWN`；若 `Cancel` 取得鎖時 `now < D + W`，則 Ask withdrawn，之後的 `ExpireDue` 找不到開著的 Ask |
| 恰好 deadline 時 `CLITimeout` 與 `ExpireDue` 競爭 | Ask expired，`TASK_TIMED_OUT`，task failed |
| 多個 worker 同時到期同一 Ask | 寫鎖序列化；第二個交易看到 `approval_closed` 已有值，不寫；`one_closure` 索引兜底 |
| 等寫鎖期間跨越 deadline | 以取得鎖後的 `now` 判定：先寫 `APPROVAL_EXPIRED`，再進行原操作 |
| 等寫鎖期間跨越請購的 `valid_before` | `Decide` 以取得鎖後的 `now` 判定，回 Deny `AUTHORIZATION_EXPIRED`，依契約 Deny 終止 task（待確認，見第 12 節） |
| 時鐘倒退 | 寫入以 `CLOCK_REGRESSION` 中止，不下溢、不改歷史、終止的 task 不復活 |

## 4. task 狀態投影（D1）

一個共同的純函式 `project(events)`，供寫入、查詢與重啟驗證共用，不各自實作：

1. **終止**：依 sequence 找第一個終止事件——`POLICY_DECIDED` 且為 Deny、`TASK_CANCELLED` 或 `TASK_TIMED_OUT`。Deny → `denied`，`deny_reason` 為該裁決的 reason；`TASK_CANCELLED` → `cancelled`；`TASK_TIMED_OUT` → `failed`。依契約，task 結束後不會再有裁決，所以若同時有 Deny 與收尾事件，Deny 一定在前，狀態保持 `denied` 與原 Deny 原因。
2. **未終止**且仍有任何開著的 Ask（`POLICY_DECIDED` Ask，之後沒有同一請購的 `APPROVAL_EXPIRED`／`APPROVAL_WITHDRAWN`）→ `awaiting_approval`。
3. **未終止**且沒有開著的 Ask → `running`。
4. 請購的原始裁決與目前狀態分開：原始裁決存在 `purchases` 與 `POLICY_DECIDED` 事件，永不改寫；目前狀態由收尾事件推導。Ask 到期後仍是 Ask，以 expired／`APPROVAL_TIMEOUT` 表達請購收尾，不變成新的 Policy Deny，也不重新核准或重跑 Policy。

| 情境 | 結果 |
|---|---|
| Ask A 開著，請購 B 為 Allow | `awaiting_approval`，A 仍開著 |
| Ask A 開著，請購 B 為 Deny，之後 A 到期 | `denied`，`deny_reason` 為 B 的原因；A 為 Ask＋expired |
| Ask A 開著，CLI 逾時，之後 A 到期 | `failed`；A 為 Ask＋expired；新請購 `TASK_NOT_ACTIVE` |
| 同一 task 兩個 Ask 開著，只到期其中一個 | `awaiting_approval` |
| 未終止 task 的最後一個 Ask 到期 | `running`；原請購為 Ask＋expired |
| Deny 後主持人取消 | `denied`，原 Deny 原因不變 |
| 尚無請購就取消 | `cancelled` |
| 只有 Allow | `running` |

**投影如何與事件一致**：`ValidateEventOrder` 只驗事件串流本身是否合法，不能代替投影欄位的核對。每個寫入交易在第 7 步對「既有事件＋新事件」重新執行 `project`，把結果寫入 `tasks.status`／`deny_reason`；重啟驗證也以同一個函式重算並比對（第 8 節）。M1 的事件量小，每次寫入重算整條串流可以接受（日後量大再改成增量）。

## 5. 可信狀態、快照與隔離

### 5.1 快照

`StartTask` 記錄 registry、profile、credential 與 terms 四種快照，該 task 之後都用自己的快照；重啟後換了設定檔，既有 task 與既有結果不變，只有新 task 用新設定。快照以型別化 struct 重新序列化：`Credential` 只有 `consumer_id`、`valid_until`、`evidence_mode`；設定檔多出的欄位在載入時就被 `contracts.Decode` 拒絕，不會經由快照保存隱藏欄位或秘密。

### 5.2 額度、期限與 fixture 憑證

- **terms 快照**：`profile_state`、`credential_state`、`budget_initial`、`asset_id` 與 `approval_deadline_seconds`（預設 30 秒），都由可信 Host／fixture 提供，**不從請購推導**。期限放在 task 的 terms 快照，**不改 profile**：`policy.Decide` 不使用期限，節點 1 的 Profile、`m1-demo-v1` fixture、已接受案例與契約版本都不必變動。若 reviewer 偏好把期限放進 profile，遷移方式是新增 `m1-demo-v2` fixture 並保留 v1，本輪不動任何程式或 fixture。
- **profile 的三種情況**（R2）：

  | 情況 | `StartTask` | terms | 開檔恢復 |
  |---|---|---|---|
  | **存在** | 以 `Profile.Validate` 驗證後保存快照 | `profile_state = present`；`asset_id` 必須等於 profile 的計價資產；`budget_initial` 預設等於 task cap，可信 fixture 可給較低值（例如超額案例的 0.25），**不會被重設成 task cap**，且須 `≤ task_cap` | 快照存在、kind 正確、可解碼、`Validate` 通過；cap 與計價資產比對 |
  | **刻意缺失**（可信 Host／fixture 明確宣告） | `profile_snapshot` 保持 NULL；**不補預設 profile** | `profile_state = absent`；`budget_initial` 與 `asset_id` 由可信 fixture 明確提供（案例沿用 `cases.json` defaults 的 `remaining` 與 `asset`），`asset_id` 必須是該 task registry 快照中的可信資產 | NULL 合法；不做 cap 與計價資產比對；其餘檢查照做 |
  | **引用損壞或內容非法** | Host 提供的 profile 不合法 → `StartTask` 拒絕，不轉成缺設定 | — | 引用的快照不存在、kind 不對、無法解碼、`Validate` 失敗，或 `profile_state` 與欄位不一致（present 卻是 NULL，absent 卻有值）→ 拒絕服務 |

  缺 profile 的 task 以 `Profile=nil` 原樣傳給節點 1 的 `policy.Decide`，得到既有的 Deny `CONFIG_MISSING`，不新增 reason code。credential 用同一套 `credential_state` 區分合法缺失（`Credential=nil` → `CONFIG_MISSING`）與損壞引用。
- **額度**：M1 不扣款，`budget_remaining` 永遠等於 `budget_initial`，不論 profile 是否存在。額度綁定 task 與計價資產，`Decide` 已拒絕其他 task 的額度與非計價資產。
- **fixture 憑證效期**：測試用固定時間。demo 在建立新 task 時由 Host 產生明標 `fixture` 的絕對 `valid_until`（`started_at` 加上 Host 設定的有效長度）並存入快照；重啟不續期，既有 task 不受之後的設定影響。

### 5.3 錢包沿用與 A／B 隔離

- 同一 consumer 的所有 task 沿用同一個 `wallet_id` 與 `payer`。`StartTask` 時若該 consumer 已有 task，新的 registry 快照對這個 consumer 的錢包必須與既有 task 相同，否則以 `WALLET_BINDING_CHANGED` 拒絕，不在重新載入設定時默默更換。
- **反方向（REV2，2026-10-02 補）**：不同 consumer 不得使用另一個 consumer 在任何已保存 task（包含已結束的 task）用過的 `wallet_id` 或 `payer`，不論目前 registry 怎麼寫。每份 registry 快照各自合法時，仍可能把同一個錢包先後配給兩個 consumer；`StartTask` 在同一個持有寫鎖的交易內查核，衝突時分別以 `WALLET_ID_IN_USE`、`PAYER_IN_USE` 拒絕並整個回滾。這兩個是 Store 錯誤，不是契約的 reason code，也不寫成 Policy 裁決。`tasks.wallet_id`／`tasks.payer` **不加**全域 UNIQUE，因為同一 consumer 跨 task 重複使用是合法的。沿用 M1 registry 既有的地址與 ID 唯一語意，不新增多鏈地址重用、錢包轉讓或輪替。
- 所有 session 操作都以 session 的 `consumer_id`＋`task_id` 限定；A 讀寫 B 的 task 直接拒絕。retry key 含 consumer／task／wallet，同名 request ID 不會互相命中。
- **可信 session 的來源**：B1-04 完成前，節點 2 的測試直接在程式內建立 `contracts.Session`，這是**測試用可信 context**，不代表真實登入或 caller 認證已完成。節點 2 不提供任何 HTTP 寫入端點。

## 6. 報價的接受、保存與展示（D4）

`Quote.SelectedRequirement` 與 `Quote.Extensions` 是 `json.RawMessage`，可以承載任意巢狀欄位；型別化的 credential 保證不適用於報價。規則如下，不改 B1-02 契約。

### 6.1 接受範圍：只收可信報價簿裡的報價

- Store 開啟時由 Host 提供**可信報價簿**：每個 `(http_method, resource_url)` 對應的報價。M1 的報價簿就是 B1-03 商店自己產生的報價（內容由本專案撰寫）。
- 入口規則（**只適用於新請購**，順序見 3.3 之後的說明）：請購的 `Quote`（`x402_version`、`selected_requirement`、`extensions`）必須與**目前**報價簿中同一 method＋URL 的某一份報價 **canonical 完全相同**。這同時把報價綁定到商店實際提供的資源。
- **歷史請購不受目前報價簿影響**（R1）：報價簿可以在重啟時更新（改價、下架）。已接受的請購以保存的完整 canonical 內容辨識 replay 與衝突，不重新要求原報價仍在報價簿；但原報價下架後，引用它的**新**請購仍會被拒收，不能藉歷史請購重新開放舊報價。
- 通過的請購**原樣**保存 canonical body，requirement hash 保持可重算，不刪減、不遮罩後再沿用原 hash。
- 案例測試：`cases.json` 文件已規定 amount／asset 覆蓋要「同時更新 trusted quote」，所以各案例的可信報價簿就是該案例的報價（包含 B1-02 golden 輸入裡的 `extra.future_term`、`extensions.fixture-pricing` 等欄位）。

**為何不用欄位白名單**：已接受的 golden 輸入 `purchase-input.json`（8 個 policy 案例的 input）含 `extra.fixture`、巢狀的 `extra.future_term` 與 `extensions.fixture-pricing`。只允許固定欄位的白名單會在入口拒收已接受案例的輸入；用樣式辨識秘密又無法對任意自由文字給出保證。因此 M1 以**來源**（可信報價簿）限制輸入範圍，而不是掃描內容。

### 6.2 落盤前拒收

- 新請購的報價不在目前報價簿，就在提交前拒收，回 `QUOTE_NOT_OFFERED`，整個交易回滾，不寫任何列或事件。這涵蓋未知的擴充欄位、多出來的 `extra` 欄位、假 token、類似私鑰的欄位、巢狀敏感欄位與惡意 HTML。
- 錯誤訊息與 log 只含 code 與已驗證格式的 ID（`request_id`、`purchase_order_id`），**不含**被拒報價的任何鍵名或值。
- 影響：在 M1 裡，報價簿以外的商家報價於入口拒收，而不是成為 Policy 的 `MERCHANT_NOT_ALLOWED`。報價簿內的商家若不在 profile 白名單，仍由 Policy 判 Deny `MERCHANT_NOT_ALLOWED`（已接受案例 `deny-merchant` 不受影響）。這項語意調整待確認（第 12 節）。

### 6.3 查詢與展示

- 原始報價（`purchases.body`）不進一般 log，也不出現在任何查詢或展示回應。
- `Task`／`Events` 只回白名單投影：`purchase_order_id`、`request_id`、`http_method`、`resource_url`、`chain_id`、`asset_contract` 與對應的 `asset_id`、`pay_to`、`amount`、`intent_hash`、`payment_requirement_hash`、原始裁決（verdict／approval／reason）、`decided_at`、`deadline_at`、收尾狀態與 `closed_at`，以及型別化的事件。這些值都已通過契約的格式檢查（ID、地址、金額、列舉、URL）。
- JSON 輸出由 `encoding/json` 跳脫 `<`、`>`、`&`。**這只解決輸出編碼**，不是秘密遮罩，也不保證前端以 `innerHTML` 插入仍然安全；B1-06 必須以文字方式呈現。
- 本設計不宣稱能從任意自由文字辨識所有秘密；M1 的保證來自限制可接受的資料範圍。

## 7. 查詢介面

| 介面 | 呼叫者 | 範圍 |
|---|---|---|
| `Store.Task(session, task_id)`、`Store.Events(session, task_id)` | wrapper（B1-04）與測試 | 只能讀 session 自己的 task；時間語意見 3.3 |

主持人檢視所有 task 的 HTTP 端點建議移到 B1-06，與展示認證一起完成（第 10 節）。loopback 不是免認證的理由。

## 8. 開檔恢復驗證（D3）

開檔時、接受任何操作之前，在讀取 handle 上以**單一讀交易**完成全部檢查（WAL 下整個交易看到同一個一致快照），其他連線同時提交也不會讓不同時點的合法資料被誤判為損壞。

**讀取失敗不等於讀完**（REV1，2026-10-02 補）：每個逐列讀取在迴圈結束後都檢查 `Rows.Err()` 與關閉錯誤；`Rows.Next` 回 false 可能是讀完，也可能是讀取失敗。任何查詢、讀列或結束讀取的錯誤都回 `ReadError`（包著原始錯誤，註明檢查與 task），不說成資料已損壞，但同樣拒絕服務：`Open` 不回傳 Store、不執行 `ExpireDue`、不修補資料。內容不一致才是 `CorruptError`。

恢復驗證只用已保存的資料、快照、hash 與事件，**不查目前的可信報價簿**，也不以它否定先前已接受的報價（R1）；已保存報價的格式、canonical 形式、hash 與一致性檢查照做。

1. `PRAGMA integrity_check`（不用 `quick_check`：它不驗 UNIQUE 與索引內容一致性）與 `PRAGMA foreign_key_check`（`integrity_check` 不檢查外鍵）；`user_version` 等於程式支援的版本（[pragma 文件](https://www.sqlite.org/pragma.html#pragma_integrity_check)）。
2. **設定快照**（無條件）：重算 `snapshot_hash`；body 是 canonical 且能以 `contracts.Decode` 嚴格解碼成對應 kind 的型別（registry 另以 `NewRegistry` 驗證，profile 以 `Validate`，credential 須為 `fixture`）；task 引用的快照存在且 kind 正確；terms 的 `task_id` 等於 task，`approval_deadline_seconds` 為正整數。**profile／credential 的狀態**：terms 的 `profile_state`／`credential_state` 與對應欄位一致——`present` 時欄位必須引用合法快照（credential 的 `consumer_id` 等於 task 的 consumer），`absent` 時欄位必須為 NULL；不一致即損壞，不當成合法缺設定。
3. **task 與錢包**：task 的 consumer／agent／wallet／payer 等於其 registry 快照的 `Binding(task_id)`；同一 consumer 的所有 task 使用同一個 wallet 與 payer；並以所有已保存 task 建立 `wallet_id`→consumer 與 `payer`→consumer 的反向對應，同一個 wallet 或 payer 屬於兩個 consumer 時以檢查名稱 `wallet` 拒絕（REV2）。不因目前 registry 與歷史快照不同就拒絕開檔；歷史 task 仍各自以自己的快照驗證。
4. **額度**：無條件——`budget_initial` 是合法金額、`budget_remaining == budget_initial`、terms 的 `asset_id` 是該 task registry 快照中的可信資產。僅在 profile 存在時——`budget_initial ≤` profile 的 task cap、`asset_id` 等於 profile 的計價資產。
5. **請購**：body 是 canonical 且能嚴格解碼；`Intent.Hash()` 等於 `intent_hash`；`RetryKey()` 等於 `retry_key`；`request_id`、`chain_id`、`asset_contract`、`payer`、`eip3009_nonce`、`purchase_order_id` 等索引欄位等於 body；以 task 的 registry 快照執行 `ValidatePurchase`；報價仍可重算出 `payment_requirement_hash`。
6. **裁決與 approval**：裁決欄位等於對應 `POLICY_DECIDED` 的 decision；`decided_at` 等於該事件的 `observed_at`；`approval_closed`／`closed_at` 與收尾事件的型別和 `observed_at` 一致，沒有收尾事件時兩者為 NULL。
7. **事件**：body 是 canonical 且能嚴格解碼；索引欄位（task、sequence、event_id、type、purchase_order_id、observed_at）等於 body；`event_id` 等於決定性產生的值；`event_hash` 等於重算的 `EventHash`；sequence 從 1 連續，`prev_event_hash` 連成鏈，`chain_head`／`next_sequence` 等於最後一個事件；`ValidateEvent`（對照 registry 快照與請購）與 `ValidateEventOrder` 通過；每筆請購都有 `PURCHASE_PROPOSED` 與 `POLICY_DECIDED`，沒有孤立的請購或事件。
8. **投影**：以第 4 節的 `project` 重算，等於 `tasks.status` 與 `deny_reason`。

registry、consumer／wallet 歸屬、請購格式與 hash、事件與投影一致性（第 3、5～8 項）一律檢查，**不因缺 profile 或缺 credential 而跳過**。

**任何不一致都拒絕整個資料庫的服務**，回報檢查名稱與 task，不自動修補、不覆寫證據。

**威脅模型限制**：這些檢查能發現不一致，但能任意改寫整個資料庫的人，可以同步修改快照、索引、事件與鏈頭，讓結果仍然一致。只改尾端事件與鏈頭時，仍可能被請購、approval 或投影等其他一致性檢查發現；但沒有外部 checkpoint 時**無法保證**偵測尾端截斷或整條鏈重算。「無法保證偵測」不等於所有尾端修改都會通過驗證。

## 9. 測試計畫

全部以 `go test -race` 執行。並行測試開多個 `Store`（各自的連線）指向同一個資料庫檔並重複多輪；時鐘一律注入假時鐘。寫入中途失敗用測試專用注入點（`export_test.go` 設定，正式程式不可開啟）；當機以子行程在交易中途 `os.Exit` 模擬，再由父行程重新開檔。

### 9.1 基本（沿用第一版 1～11，依本版修訂）

| # | 情境 | 預期 | 驗證方式 |
|---|---|---|---|
| 1 | 相同請購循序與並行重送 | 只有一列請購、一組 `PURCHASE_PROPOSED`／`POLICY_DECIDED`；其餘拿到 replay 與相同原始裁決 | 多 goroutine／多 Store 同時送出，查列數與事件數 |
| 2 | 同 key 不同內容、purchase ID 重用、nonce 重用 | `IDEMPOTENCY_CONFLICT`，資料表、事件、`chain_head` 都不變 | 衝突前後比對 |
| 3 | task 結束後的合法 replay | 先驗歸屬再回傳原結果；新請購 `TASK_NOT_ACTIVE`；別人的 session 重送 → `OWNERSHIP_MISMATCH`，而且不觸發任何到期寫入 | Deny 後重送原請購、新請購與跨 session 重送 |
| 4 | Deny 與新請購並行 | 串流通過 `ValidateEventOrder`，Deny 之後沒有新裁決 | 並行多輪檢查不變式，不假設先後 |
| 5 | 寫入中途失敗與當機 | 沒有半份請購或孤立事件；重新開檔通過第 8 節全部檢查 | 每個寫入步驟後注入失敗；子行程在提交前結束 |
| 6 | Ask 在 29、30、31 秒 | pending、expired、expired | 假時鐘 |
| 7 | Deny 後取消、Deny 後 CLI 逾時 | 收尾事件寫入，`denied` 與原 reason 不變 | 讀投影與事件 |
| 8 | A／B 隔離 | 同名 request ID 各自是新請購；A 讀寫 B 被拒；切換 profile 並重啟後既有 task 不變 | 兩個 session 交叉操作 |
| 9 | 事件鏈竄改 | 中段修改、刪除、插入 → 開檔失敗；同時改寫尾端事件與 `chain_head` 的情況依第 8 節的限制記錄，不宣稱一定偵測 | 以 SQL 修改資料庫檔後重新開檔 |
| 10 | 遮罩與注入 | 見 9.5 | — |
| 11 | M1 無付款能力 | `budget_remaining` 不變；沒有 APPROVED／GRANT_ISSUED／SETTLED／RESOURCE_DELIVERED；credential 快照為 `fixture` | 每個情境結束時檢查 |

另以 `cases.json` 的 16 個 integration 案例在儲存層執行（不含真實 CLI）：8 個 policy 案例、取消兩案、approval 逾時三案、CLI 逾時兩案、重啟後同一 consumer 的新 task 沿用同一錢包。這是儲存層證據；端到端仍屬 B1-06。

### 9.2 狀態投影（D1）

`project` 的表格測試涵蓋第 4 節表中全部 8 個情境；同樣 8 個情境也經由 Store 的實際寫入執行，比對資料庫欄位、事件與 `project` 結果三者一致。

### 9.3 時序與時鐘（D2）

| 情境 | 預期 |
|---|---|
| 停機跨越 deadline，重啟後第一次查詢與第一次 replay | 都看到 expired；`observed_at` 為重啟後時間；只有一個 `APPROVAL_EXPIRED` |
| 恰好 deadline 時 `Cancel` 與 `ExpireDue` 競爭（兩種先後各跑多輪） | 都是 expired＋`TASK_CANCELLED`，沒有 `APPROVAL_WITHDRAWN` |
| deadline 前一秒 `Cancel` | withdrawn＋`TASK_CANCELLED` |
| 恰好 deadline 時 `CLITimeout` 與 `ExpireDue` 競爭 | expired＋`TASK_TIMED_OUT`，`failed` |
| deadline 前 `CLITimeout`，之後到期 | `failed`；Ask 由 pending 轉為 expired；不開放新請購 |
| 多個 worker 同時 `ExpireDue` 同一 Ask | 只有一個 `APPROVAL_EXPIRED` |
| 等寫鎖期間跨越 deadline 或 `valid_before` | 依取得鎖後的時間判定（測試持有寫鎖、推進假時鐘、再釋放） |
| replay 時有到期 | 只多一個 `APPROVAL_EXPIRED`，沒有新的請購或裁決事件；回傳 expired |
| 衝突請購時有到期 | 回滾後沒有 `APPROVAL_EXPIRED`；下一次 `ExpireDue` 寫入 |
| 時鐘倒退 | 寫入回 `CLOCK_REGRESSION`；沒有下溢；終止的 task 不復活；讀取照常 |
| 查詢時已過 deadline 但尚未到期處理 | 仍顯示 pending 並附 `deadline_at`，不顯示成已收尾 |

### 9.4 恢復驗證（D3）

對第 8 節每一項，各準備一個「只改一個欄位」的破壞：快照的 body、hash、kind；task 的 status、deny_reason、next_sequence、chain_head、budget_remaining、wallet／payer；請購的 body、intent_hash、retry_key、payer、nonce、裁決欄位、decided_at、approval_closed、closed_at；事件的 body、event_hash、sequence、observed_at、event_type、purchase_order_id；再加上刪除請購（留下孤立事件）、刪除事件（留下孤立請購）與外鍵斷裂。每一種都必須讓開檔失敗並指出正確的檢查名稱。另測：恢復驗證進行時另一個連線持續提交合法寫入，驗證不得誤判。

### 9.5 報價保存（D4）

| 情境 | 預期 |
|---|---|
| 報價的 `extra` 或 `extensions` 加入假 bearer token、`privateKey` 類欄位、巢狀敏感欄位、`<script>` 與換行字串（不在報價簿） | `QUOTE_NOT_OFFERED`；資料表列數不變；擷取的 log 與錯誤訊息都不含這些字串或鍵名 |
| 報價簿內的 golden 報價（含 `資料 & terms`） | 原樣保存；重新開檔後重算的 requirement hash 等於 intent 的 `payment_requirement_hash` |
| 查詢與展示回應 | 不含原始報價、`extra` 或 `extensions` 的任何內容；只有 6.3 的白名單欄位 |
| 每條連線的 PRAGMA | 多條連線逐一讀回 `foreign_keys`、`busy_timeout`、`synchronous`；讀取 handle 為 `query_only` |

### 9.6 歷史請購 replay 與目前報價簿（R1）

| 情境 | 預期 |
|---|---|
| 接受 P1 後，報價改價並重啟，再重送 P1 | replay；原始裁決、intent hash 與原報價不變 |
| 接受 P1 後，原報價／商品移除並重啟，再重送 P1 | 開檔恢復成功（不查報價簿）；replay |
| task 已結束、原報價也移除，再重送相同 P1 | 合法 replay；不重新檢查 active-task、不新增裁決 |
| 同一 retry key 改內容，且新內容不在報價簿 | `IDEMPOTENCY_CONFLICT`（不是 `QUOTE_NOT_OFFERED`）；沒有部分寫入 |
| 不同的新請購引用已移除的 Q1，且無其他衝突 | `QUOTE_NOT_OFFERED`；不能藉歷史請購重新開放舊報價 |
| A 嘗試重送 B 已接受的 P1 | 歸屬拒絕；回應不含 B 的原裁決；B 的 task 沒有任何到期寫入 |

### 9.7 缺 profile 與損壞引用（R2）

主要流程：可信 fixture 明確不提供 profile → `StartTask` 成功保存 `profile_state = absent` 與有效 terms → `Submit` 得到 Deny `CONFIG_MISSING` → 原裁決與事件保存 → 關閉並重新開啟 Store → 恢復成功，仍是同一個 Deny `CONFIG_MISSING` → 相同請購合法 replay，沒有新裁決。

| 情境 | 預期 |
|---|---|
| profile 存在但內容不合法（`StartTask` 時） | 拒絕，不轉成合法缺設定 |
| profile 存在、資料庫中被引用的快照遭刪除或 kind／body 被改 | 開檔拒絕服務 |
| `profile_state = present` 但 `profile_snapshot` 被改成 NULL | 開檔拒絕服務（不當成缺設定） |
| profile 缺失，但 terms 的格式、task 綁定、金額或 `asset_id` 錯誤 | `StartTask` 拒絕；若在資料庫中被改則開檔拒絕服務 |
| credential 明確缺失 | Deny `CONFIG_MISSING`，重啟後相同 |
| `credential_state = present` 但引用的 credential 快照損壞 | 開檔拒絕服務 |

## 10. 最小交付範圍

**節點 2 交付**：Store 的 `Open`（含第 8 節驗證與啟動時 `ExpireDue`）、`StartTask`、`Submit`、`Cancel`、`CLITimeout`、`ExpireDue`、`Task`、`Events`；第 9 節全部測試與 16 個 integration 案例在儲存層通過；新增的 mutation 與容器驗證（新映像）通過；`docs/third-party.md`。

**不含**：JSONL 匯出、B1-04 的 caller 認證、外部 checkpoint、主持人檢視 HTTP 端點、多行程部署（除 SQLite 檔案鎖以外的協調）。

**HTTP 範圍調整提案**（待確認）：B1-05 任務卡的「事件讀取 endpoint」移到 B1-06，與展示認證一起完成；B1-05 節點 2 交付受 session 限制的 Store 查詢（`Task`／`Events`）。任務清單建議同步文字：

> B1-05 交付清單中的「事件讀取 endpoint」移至 B1-06，與主持人展示認證一起完成；B1-05 節點 2 交付受 session 限制的 Store 查詢（Task／Events）。此調整待確認；未交付的 HTTP 端點不記為已驗收。

## 11. 九項建議的採納情形

| 項目 | 採納情形 |
|---|---|
| 1. Driver／版本 | **採納**：`modernc.org/sqlite v1.60.1`，`modernc.org/libc v1.77.1` 與其 go.mod 一致；changelog 的 WAL `-shm` 修正也支持不退版（1.2） |
| 2. 驗證映像 | **採納**：預載模組，綁定 `go.mod` 與 `go.sum` 兩者；build context 只有這兩個檔與 Dockerfile（1.4） |
| 3. Approval deadline | **採納並調整位置**：預設 30 秒，存在每個 task 的 terms 快照，不必把 profile 升版；Profile、v1 fixture、案例與契約都不動。若偏好 profile v2，遷移方式已寫明（5.2） |
| 4. DB 路徑 | **採納**：`APA_DB_PATH`，預設 `var/apa.db`，相對路徑以啟動時工作目錄為基準，目錄 `0700`，不接受 DSN（1.6） |
| 5. Fixture 來源 | **採納**：測試固定時間；demo 建 task 時產生 fixture 絕對效期並保存，重啟不續期；較低額度不重設成 task cap（5.2） |
| 6. 恢復失敗 | **採納**：整個資料庫拒絕服務，不自動修補（第 8 節） |
| 7. 主持人 HTTP | **採納為提案，待確認**：移到 B1-06，節點 2 交付 session 查詢（第 10 節） |
| 8. 到期事件時間 | **採納**：用取得寫鎖後取樣的 Host 時間，不倒填（3.1、3.4） |
| 9. 到期後 task | **採納**：只有未終止且沒有其他開著的 Ask 才回 `running`（第 4 節） |

沒有提出異議。

## 12. 仍待確認

1. **以可信報價簿限制入口**（6.1、6.2）：報價簿以外的商家報價在入口拒收（`QUOTE_NOT_OFFERED`），不成為 Policy Deny；只適用於新請購，歷史請購的 replay 與恢復不查目前報價簿（R1）。
2. **到期優先於取消／CLI 逾時**（3.4）：恰好 deadline 時一律 expired。已接受案例沒有涵蓋這個邊界，這是由 `elapsed >= deadline` 推出的選擇。
3. **等鎖跨越 `valid_before` 產生 Deny 並終止 task**（3.6）：依取得鎖後的時間判定是一致的語意，但後果較重。
4. **時鐘倒退時全資料庫拒絕寫入**（3.5），由主持人修正時鐘後恢復。
5. **`ExpireDue` 目標週期**：建議 1 秒；只是目標週期，不是延遲上限（C1）。
6. **主持人 HTTP 端點移到 B1-06**（第 10 節）。
7. **期限放在 terms 快照而非 profile v2**（5.2）。
8. **錢包改變時 `StartTask` 拒絕**（5.3）。
9. **新請購的拒收順序**（3.3 之後的說明，第三版新增）：先檢查目前報價簿，再檢查 active-task。新請購同時不在報價簿、task 也已結束時，回 `QUOTE_NOT_OFFERED`。兩者都整個回滾、不回顯內容，差別只在錯誤代碼。
10. **terms 明確記錄 `profile_state`／`credential_state`**（5.2，第三版新增）：用來區分合法缺設定與損壞引用；這是 terms 快照的欄位，不是 profile 或契約的變更。

## 13. 修訂對照

### 第三版（針對 `adad831` 的 review：R1、R2、C1）

| 項目 | 修改位置 | 採用規則 | 新增測試 |
|---|---|---|---|
| R1 報價簿與歷史 replay | 3.3 操作表與其後的「`Submit` 的分類順序」；6.1、6.2；第 8 節前言；第 12 節第 1、9 項 | 先驗歸屬與格式，再以 retry key 與已保存的 canonical 請購分類；replay 與衝突不查目前報價簿；只有新請購才查目前報價簿，再查 active-task；恢復不查報價簿 | 9.6：6 個情境 |
| R2 缺 profile | 第 2 節 terms 與欄位註解；5.2 三種情況表；第 8 節第 2、4 項與「不因缺設定而跳過」；第 12 節第 10 項 | terms 明確記錄 `profile_state`／`credential_state`；刻意缺失時 NULL 合法、terms 由可信 fixture 提供；cap 與計價資產比對只在 profile 存在時；其餘一律檢查；`Profile=nil` 原樣交給 Policy | 9.7：主要流程＋6 個反向與 credential 情境 |
| C1 `ExpireDue` 週期 | 3.3「查詢的時間語意」；第 12 節第 5 項 | 1 秒是目標週期，不是延遲上限；查詢保留 `as_of_sequence`、`as_of_observed_at`、`deadline_at`；未寫入到期事件就不顯示成已收尾 | 不新增（既有 9.3 末列沿用） |

### 第二版（針對 `c9199e7` 的 review：D1～D4）

| 項目 | 修改位置 | 設計決定 | 新增測試 |
|---|---|---|---|
| D1 狀態投影 | 第 4 節；3.2 第 7 步；第 8 節第 8 項 | 單一 `project` 函式：第一個終止事件決定終止狀態，否則依是否有開著的 Ask；原始裁決與請購目前狀態分開；寫入與恢復都以它重算比對 | 9.2：8 個交錯情境，函式與實際寫入兩層 |
| D2 時間與時序 | 第 3 節全部；7 | 時鐘由 Host 注入、取得寫鎖後取樣；共同交易骨架，先授權再到期；到期與操作同一交易、一起回滾；查詢為已提交快照並附 `deadline_at`；啟動先 `ExpireDue`；時鐘倒退拒絕寫入 | 9.3：11 個時序與時鐘情境 |
| D3 恢復驗證 | 第 8 節；2 的欄位與索引 | 單一讀交易；`integrity_check`＋`foreign_key_check`；快照、task、錢包、額度、請購、approval、事件、投影共 8 類檢查；任何不一致整庫拒絕；威脅模型與尾端限制的精確表述 | 9.4：逐欄位破壞與並行提交下不誤判 |
| D4 報價保存 | 第 6 節；1.3 | 以可信報價簿限制入口並原樣保存；落盤前拒收且不回顯內容；查詢只回白名單投影；JSON 跳脫不等於遮罩 | 9.5：敏感與惡意報價、合法報價的 hash 重算、展示白名單、逐連線 PRAGMA |

## 14. 實作對照（2026-10-02）

套件選 `internal/store`。

| 設計 | 實作 | 測試 |
|---|---|---|
| 1.2 driver | `modernc.org/sqlite v1.60.1`、`modernc.org/libc v1.77.1`（`go.mod`）；授權 `docs/third-party.md` | `go mod verify` |
| 1.3 連線與 PRAGMA | `store.go` `Open`：寫入 handle（`_txlock=immediate`、單一連線）、讀取 handle（`query_only`），PRAGMA 寫在 DSN、每條連線套用；WAL | `TestPragmasOnEveryConnection` |
| 1.4 驗證映像 | `scripts/verify/Dockerfile`、`build-image.sh`、`container-verify.sh`、`container-inner.sh` | 容器驗證、`write-module-cache` 探測 |
| 1.6 路徑 | `dbPath`：只收檔案路徑、相對路徑以工作目錄為基準、目錄 `0700`、拒絕 symlink | `TestPathRules`、`TestRelativePath` |
| 2 資料表 | `schema` 常數，`user_version = 1` | 全部 |
| 3.2 交易骨架 | `write`：鎖 → 時鐘 → 倒退檢查 → 操作 → 提交 | `TestClockSampledAfterLock`、`TestClockRegression`、`TestWriteFailureRollsBack`、`TestCrashBeforeCommit` |
| 3.3 Submit 順序 | `Submit`：解析 → 歸屬 → 到期 → retry key 與 `CompareRetry` → 報價簿 → active-task → `policy.Decide` → 寫入；唯一索引違反對應 `IDEMPOTENCY_CONFLICT` | `TestReplay*`、`TestConflictsWriteNothing`、`TestUniqueIndexBackstop`、`TestExpiryAroundReplayAndConflict` |
| 3.3 Host 操作 | `Cancel`、`CLITimeout`、`ExpireDue`（每個 task 一個交易）、`RunExpiry` | `TestCloseAtDeadline`、`TestCLITimeoutThenExpiry`、`TestConcurrentExpiryOnce`、`TestRunExpiryStops` |
| 3.4～3.6 到期與時鐘 | `expire`：先確認 `now ≥ decided_at` 再相減 | `TestAskDeadline`、`TestRestartAcrossDeadline`、`TestClockRegression` |
| 4 投影 | `project` | `TestProjectionInterleavings` |
| 5 快照、額度、隔離 | `StartTask`、`buildTerms`、`terms.validate`；缺 profile／credential 須明確宣告 | `TestStartTask`、`TestWalletBindingKept`、`TestIsolationAB`、`TestMissingProfileLifecycle` |
| 6 報價 | `offered`；`contractRefusal` 只留 code；Store 不寫 log | `TestQuoteSafety`、`TestStoreDoesNotLog` |
| 7 查詢 | `Task`、`Events`：唯讀交易、session 限定、`as_of_*`、`deadline_at` | `TestQueryIsCommittedSnapshot` |
| 8 恢復驗證 | `verify.go`：單一讀交易，依序 integrity、foreign_keys、schema、snapshot、task、budget、purchase、event、chain、decision、projection | `TestRecoveryRefusesCorruption`（33 項）、`TestRecoveryTailLimits`、`TestRecoveryUnderConcurrentWrites` |
| 9.1 integration 案例 | — | `TestAcceptedIntegrationCases`（16 個，預期值直接讀 `cases.json`） |

與設計文字的細部差異（不改語意）：

- 唯一索引違反在 `Submit` 寫入請購時對應 `IDEMPOTENCY_CONFLICT`。在寫鎖下 `CompareRetry` 的查詢會先回答，所以這是備援路徑；測試只直接證明索引本身會拒絕重複，並沒有走到 `Submit` 的這個分支。
- 恢復驗證的檢查名稱：`integrity`、`foreign_keys`、`schema`、`snapshot`、`task`、`wallet`（跨 consumer 歸屬，REV2 新增）、`budget`、`purchase`、`event`、`chain`、`decision`（含 approval 收尾）、`projection`。讀取失敗以 `ReadError` 回報同一組檢查名稱（REV1）。
- `go mod tidy` 沒有執行：離線時它要解析 `libc` 自己的測試依賴（`modernc.org/ccgo`），本模組不建置這些套件。`go build`、`go test` 與 `go mod verify` 都能以提交的 `go.sum` 離線通過。

## 15. 實作修正（2026-10-02，針對 `64f247c` 的 review）

| 項目 | 修改位置 | 守住的不變式 | 正式測試 |
|---|---|---|---|
| REV1 恢復讀取錯誤 | `store.go`：`ReadError`、`readErr`、`drained`（`Rows.Err()` 加關閉錯誤）、測試用的 `openReader`；`verify.go`：integrity、foreign_key_check、user_version、快照、task、請購、事件每一段讀取；`ops.go`：`openTask` 的事件與請購讀取、`decodeSnapshot`、`scanPurchase` | 讀取失敗永遠不被當成讀完；回傳包著原始錯誤的 `ReadError`；`Open` 不回傳 Store、不執行到期處理、不寫任何資料 | `TestRecoveryReadFailures`：外鍵、快照、task、task 載入的事件與請購、請購、事件共 13 個注入點，可多列的掃描分別測第一列失敗與讀到部分資料後失敗；每個案例確認注入確實命中、`errors.Is` 為注入的錯誤、`ReadError.Check` 正確、資料庫沒有任何變更；同一測試也確認無注入時正常開檔並處理到期、空資料庫可開 |
| REV2 跨 consumer 錢包歸屬 | `ops.go` `StartTask`：寫鎖交易內查核其他 consumer 是否已用過此 `wallet_id`／`payer`；`verify.go`：所有 task 的反向對應，檢查名稱 `wallet` | 同一 consumer 可沿用；不同 consumer 不得使用已被他人保存使用的 `wallet_id` 或 `payer`，包含已結束的 task；不加全域 UNIQUE | `TestWalletOwnershipAtStart`（payer 與 wallet_id 分開，各只觸發一條規則，核對錯誤代碼、欄位與擁有者，且沒有部分寫入；並確認合法情況仍可建立）、`TestWalletOwnershipAtRecovery`（見下）、`TestWalletOwnershipRace`（兩個 Store 各持一份合法但互相衝突的 registry 同時建立，至多一方成功，另一方 `PAYER_IN_USE`）；原有 `TestWalletBindingKept` 保留 |

**恢復測試如何避免被其他守衛提早擋下**：歷史衝突無法再經由公開 `StartTask` 建立，所以測試先在主資料庫以第一版 registry 建立 task-a，再在另一個空白資料庫以交接後的 registry 用真正的 `StartTask` 建立 task-b，兩者各自都能通過恢復驗證；之後以 SQLite `ATTACH` 把 task-b 的快照、task 與事件原樣併入。每一列都由正式程式寫出、hash 與事件鏈完整，所以只有新的 `wallet` 檢查會拒絕，測試也核對檢查名稱與 task。正式程式沒有為此加任何旁路。

**突變**：新增 12 項（REV1 8 項：`drained` 本身與七段讀取各自的錯誤檢查；REV2 4 項：建立時與恢復時的 payer、wallet_id），共 99 項。原有 87 項的比對文字全部仍然吻合，沒有改動。

## 16. 驗收紀錄（2026-10-02，`cb33768`）

| 項目 | 狀態 |
|---|---|
| B1-05 節點 1 確定性 Policy | 已通過，驗收基準 `61b3652`，不重開 |
| B1-05 節點 2 Store | 已通過技術 review，驗收基準 `cb33768d527a706ba2ae7a3daa11d1d49cea608f` |
| REV1 恢復讀取錯誤 | 已關閉：讀取失敗不再當成正常讀完，失敗時拒絕開檔 |
| REV2 跨 consumer 錢包歸屬 | 已關閉：建立與恢復都拒絕跨 consumer 共用已保存的 `wallet_id`／`payer`；同一 consumer 跨 task 沿用仍合法 |
| B1-05 整體 | 技術部分已通過；**10/2 經 Aaron 確認結案（Done）**，見下方結案紀錄 |
| 主持人事件 HTTP 移至 B1-06 | **10/2 Aaron 確認移至 B1-06**；未實作，列為 B1-06 未完成工作 |
| main 合併 | **10/2 經 Aaron 核准**，main 由 `adda78e` fast-forward 至 `7ad304a` |

**通過範圍**：SQLite 持久化、可信快照、請購去重與衝突、原子事件與投影、取消與逾時、重啟恢復、A／B 隔離、受可信 session 限制的 Store 查詢，以及 REV1、REV2 兩項修正。

**不包含**：真實 caller 認證、Claude Code Buyer wrapper（B1-04）、主持人 HTTP 端點、展示層與端到端整合（B1-06）、`cmd/demo` 的 Store 設定與生命週期接線、Signer、付款或 testnet。儲存層的 16 個 integration 案例不是 B1-06 端到端通過的證據。沒有外部 checkpoint 時事件鏈的尾端限制仍保留（第 8 節）。

**證據歸屬**

*Codex 獨立重跑*（`cb33768` 的暫存副本，既有 Linux 容器映像內；斷網、非 root、唯讀來源與工具鏈、scratch 寫入，有 CPU／記憶體／程序數／檔案與時間限制；沒有下載依賴、修改 repo 或重建映像）：

- `go test -race -cover ./... -count=1 -skip "^TestReview"` 通過；排除的只有 Codex 另外加入的探測，不是專案原有測試；Store 覆蓋率 81.9%。
- `node scripts/check-contract-vectors.mjs` 通過。
- `go test -race ./internal/store -run "TestRecoveryReadFailures|TestWalletOwnership|TestReview" -count=1 -v` 全部通過。
- REV1 的 13 個正式故障注入案例通過；Codex 原始重現的 task、快照、外鍵讀取失敗都被正確拒絕，並保留原始錯誤原因。
- REV2 的建立、恢復與競爭測試通過；Codex 另外確認原擁有者的 task 為進行中或已結束時，重新分配付款地址都回 `PAYER_IN_USE`，拒絕後沒有部分寫入。
- 探測原始碼在 Codex 的暫存目錄，輸出記錄在 Codex 的工具結果中；不是本 repo 的 artifacts run 目錄，正式測試也不依賴它。

*Claude Code 執行、Codex 核對紀錄*（不是 Codex 全數獨立重跑）：

- 最終 commit 的完整容器驗證：7 個步驟全部 exit 0；99／99 突變被測試抓到（原有 87 項加本輪 12 項）；17 項隔離探測與 19 項證據匯入測試通過。
- 本機檢查：`artifacts/b1-05/cb33768d527a-20261001T184802Z/`。
- 容器驗證：`artifacts/verify-container/runs/cb33768d527a-20261001T184940Z/`（目錄名稱是 UTC 時間，保持原樣）。
- 驗證映像：`sha256:78258368de0907e58498095c327a6ee4edbe75421d18e524bd00661c3749b6d0`。

`64f247c` 等先前版本的證據保留。本節之後的文件 commit 只改文件，沒有在其上重跑上述驗證；上述結果屬於 `cb33768`。

**結案紀錄（2026-10-02，Aaron 確認）**

上表為驗收當時的狀態，三項待確認事項其後由 Aaron 明確同意並執行：

1. **主持人事件 HTTP 端點移至 B1-06**：是責任轉移，不是刪除需求，也不算已完成。B1-06 須完成：主持人與展示端的認證、以 Store 查詢的白名單投影為資料來源、A／B 隔離、HTTP 與 UI 的整合測試。loopback 不是免認證的理由。
2. **B1-05 標 Done**：範圍是已驗收的確定性 Policy（`61b3652`）加 Store（程式驗收基準 `cb33768`）。不代表 M1、B1-06 端到端整合或真實 caller 認證完成。
3. **main 合併**：合併前核對 main 為 `adda78e`、工作區乾淨、`7ad304a` 相對 `cb33768` 只多文件變更；以 fast-forward 將 main 由 `adda78e` 前進到 `7ad304a`，`2a264c4` 至 `7ad304a` 的 16 個 commit 與 SHA 全部保留，`cb33768` 在 main 歷史中。未推送、未刪任何分支（`b1-05-policy` 保留在 `7ad304a`）。

**後續待辦（不因 Done 而消失）**：

- `cmd/demo`（或其他 host）開啟 Store：`APA_DB_PATH` 等設定載入、可信 registry／profile／credential／報價簿的來源。
- `RunExpiry` 的啟動、停止與錯誤處理，以及服務關閉時的 Store 關閉。
- B1-04 以已認證的 wrapper context 建立 session 並呼叫 `Submit`／`Task`／`Events`。
- B1-06 的主持人 HTTP 端點（上列要求）。

本結案紀錄由 main 上的文件 commit 記錄，沒有改寫已驗收的 commit。
