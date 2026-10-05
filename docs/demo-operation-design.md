# B1-06 展示操作流程設計（fake 展示模式）

狀態（2026-10-05）：**第二版設計提案，待 Codex review；尚未核准實作。** 第一版 `7bb4695` 經 Codex review：方向可行，需補四處——開始的重複請求、程序／task／收尾三種狀態、3.1 的 token 細節、取消端點權限。本版依此修訂：第 4、6、7、8 節改寫，新增第 6.3 節（3.1 細部規格），修訂紀錄見第 11 節。設計基準 `7bb4695`，程式基準仍是已驗收的 `6aee7f0`。

本文件只規劃 fake 展示模式。不啟用真實 CLI、核准、grant、Signer 或付款；不修改 B1-02 契約、已接受的 `cases.json`、Policy 或到期規則。提出設計不等於獲准實作。

背景：Aaron 10/5 實際操作過交付點 2，初步接受目前的 fake 展示資料鏈，可以繼續開發。這不是真實模型、付款或整體 M1 的驗收。

## 1 目前已有的能力與缺口

| 項目 | 現在（`6aee7f0`） | 展示時的缺口 |
|---|---|---|
| 啟動 | `make demo-integration`：編譯 `bin/demo`、`bin/fake-claude`，啟動單一 host 程序（商店 402、Store、到期 worker、唯讀 API、Dashboard、Unity 檔案） | Unity 建置與 LimeZu 匯入要另外確認；缺建置時只在 stderr 寫一行，頁面顯示「Unity 場景未就緒」 |
| 開始一場 | 啟動時依 `--plan`（`testdata/demo/fake-plan.json`）**自動**依序跑 task-a、task-b，各一次 | 主持人無法選情境或決定何時開始；開場前就已跑完 |
| 下一場 | Store 已記錄的 task 不重跑（`task_already_recorded`）；registry 只有 `task-a`、`task-a-next`、`task-b` | 要換 `APA_DB_PATH` 或改計畫檔重啟 |
| 停止 | 停止整個 host（Ctrl-C）：取消 run → 停 HTTP → 等 run 記錄結束 → 停 bridge、到期、Store；進行中的 Ask 記 `HOST_CANCELLED` | 沒有「只停這一場」 |
| 認證 | 啟動時為每個 consumer 產生 viewer token、另一個 host token（全部唯讀），寫入 `var/demo-tokens.json`（0600）；頁面手動貼 token | 每場要找檔案、複製貼上；切換 A／B 要再貼一次 |
| 頁面 | 固定 task 清單 `FIXTURE_TASKS`（`web/adapter.js`）；手動輸入 task ID；歷史回放（唯讀） | 新 task 不在清單內 |
| 就緒 | `GET /healthz` 只回程序活著與「無付款能力」；Unity 就緒由頁面自己顯示 | 沒有 Store／fake CLI／Unity 建置的整體就緒畫面 |
| Ask | Policy 給 Ask 後停在等待；30 秒（`internal/store` 的預設期限，已接受規則）後由到期 worker 記 `APPROVAL_EXPIRED` | 畫面已標示「M1 無法核准」；沒有缺口，只需在主持人畫面再說一次 |

## 2 目標流程與元件

```text
make demo                         (一個指令：檢查 → 編譯 Go → 啟動 host，不自動跑任何場次)
  -> 頁面：就緒面板（HTTP、Store、fake CLI、Unity 建置、LimeZu）
  -> 主持人在 /host 載入本機 token 檔（只在頁面記憶體）
  -> 選情境（Ask／Deny），按「開始」
       POST /api/host/runs  ->  host 配一個新 task  ->  fake Buyer -> 402 -> 請購 -> Policy -> Store
  -> Dashboard 自動切到該 consumer 的 viewer、開啟新 task，Unity／Inspector／Timeline 照既有輪詢同步
  -> 狀態：完成（Ask 等待／Deny）／停止／錯誤
  -> 「開始下一場」= 再按開始；前幾場仍在 Store，可回放
```

不新增的東西：核准、grant、Signer、付款、真實 CLI 開關、刪除或重設資料庫、改寫事件。

## 3 啟動與就緒（§4.1）

**三種操作分開**：

| 何時 | 做什麼 | 頻率 |
|---|---|---|
| 首次準備 | 匯入 LimeZu（`LIMEZU_DIR=… sh scripts/unity/import-limezu.sh`）；建置 Unity Web（`sh scripts/unity/unity.sh webgl OUT`） | 只在第一次、素材或 Unity 專案變更時 |
| 一般啟動 | `make demo` | 每場展示 |
| 開發驗證 | `make check`、Unity 測試、`node scripts/verify/browser-ui.mjs`、容器驗證 | 只在改程式時；正常展示不跑 |

**`make demo`**（新 Makefile 目標，呼叫新腳本 `scripts/demo/start.sh`）：

1. 檢查 Unity 建置檔：`unity/AgentPaymentAuthority/Build/WebGL/Build/WebGL.{loader.js,framework.js,wasm,data}`。缺少時**繼續啟動**，但印出明確訊息「Unity 建置不存在，請先執行首次準備；商店會顯示未就緒」，就緒面板也會顯示。
2. 檢查 `Assets/LimeZu/IMPORTED.txt`。缺少時同上，提示「目前若有建置，可能是 placeholder」。實際用哪種素材，仍以 Unity 自己的 ready 回報為準。
3. `go build -o bin/demo ./cmd/demo`、`go build -o bin/fake-claude ./cmd/fake-claude`（Go 建置快取，通常幾秒）。不建 Unity、不跑測試、不開容器。
4. 啟動 host：
   - 3.1 階段：沿用現有的 `--fake-cli bin/fake-claude --plan testdata/demo/fake-plan.json`，行為與現在相同；
   - 3.2 之後：改為 `--fake-cli bin/fake-claude --scenarios testdata/demo/scenarios.json --registry testdata/demo/registry.json`（`--scenarios` 是新旗標），**不帶 `--plan`**，啟動時不跑任何場次。
5. 失敗即停：埠被佔用、資料庫被另一個 demo 程序鎖住、fake CLI 的 `--version` 不是 fake-claude、Store 恢復失敗。這些都已是現有的啟動檢查，腳本只把 exit code 和那一行訊息原樣轉出，不自動改埠或刪檔。
6. 成功時印出主持人頁網址（`http://127.0.0.1:8402/host`）與 token 檔路徑；**不印 token**。

**就緒面板**（新唯讀端點 `GET /api/host/status`，見第 6 節權限）：

| 項目 | 判斷方式 | 顯示 |
|---|---|---|
| HTTP | 頁面能取得 status 本身 | 就緒 |
| Store | host 持有開啟的 Store；最近一次到期 worker 的錯誤代碼（`RunExpiry` 的 onErr 已有，改為記住最後一筆） | 就緒／「到期處理錯誤：代碼」 |
| fake CLI | 啟動時 `--version` 檢查的結果（現有） | 就緒（fake-claude 版本）；真實 CLI 永遠顯示「未啟用」 |
| Unity 建置 | `--unity-build` 目錄四個檔案是否存在（現有 `os.Root`） | 就緒／缺少 |
| Unity 場景 | 頁面上 Unity 的 ready 回報（現有） | LimeZu／placeholder／未就緒 |
| 進行中的場次 | 第 4 節的 run 狀態 | 無／task ID 與狀態 |

`/healthz` 不變（未認證、只說程序活著與無付款能力）。

## 4 主持人操作（§4.2）

**情境目錄**（新檔 `testdata/demo/scenarios.json`，取代自動計畫的角色）。它是 host 的可信設定，不是 agent 或 HTTP 請求能改的內容；欄位沿用現有 `planRun`：

```json
{"scenarios": [
  {"id": "ask",         "consumer_id": "consumer-a", "scenario": "testdata/cli/ask-mainline.json", "label": "主商品，Ask（等待核准）"},
  {"id": "deny-budget", "consumer_id": "consumer-b", "scenario": "testdata/cli/ask-mainline.json", "budget_initial": "250000", "label": "預算不足，Deny"}
]}
```

請求只帶情境 `id`。路徑、預算、consumer 都由 host 從這個檔案決定。

**操作與元件**：

| 按鈕 | HTTP | host 做什麼 | 回覆 |
|---|---|---|---|
| 開始 | `POST /api/host/runs`，body `{"request_id":"…","scenario":"ask"}` | 在 host 鎖內：依下方規則處理 request ID → 沒有進行中的場次 → 配發 task（第 5 節）→ **先**寫場次日誌 `accepted` → 在背景 goroutine 呼叫現有 `fakeRunner.runOne` | 新場次 `202`；重送 `200`；都帶 `{"run_id","task_id","consumer_id","state"}` |
| 停止 | `POST /api/host/runs/{run_id}/stop` | 只在場次 `running` 時：取消該場 context，現有 buyer host 結束 fake 程序群組並寫 `TASK_CANCELLED`。場次已結束時回 `409 run_not_running` | `202 {"state":"stopping"}` |
| 查狀態 | `GET /api/host/runs/{run_id}`、`GET /api/host/runs`（最近 20 場） | 讀記憶體與場次日誌 | 場次狀態、`runOutcome` 的固定代碼、收尾結果（第 7 節） |
| 取消 task | `POST /api/host/tasks/{task_id}/cancel` | 只寫 Store 取消，不涉及程序；條件與回應見第 6.1 節 | 第 6.1 節 |

**開始的重複請求**（取代第一版「只靠單場鎖」）：

- 頁面每次「新的開始操作」用 `crypto.randomUUID()` 產生一個 request ID，只留在頁面記憶體。網路錯誤或逾時的重送沿用同一個 ID，直到收到帶 `run_id` 的回覆。格式 `^[A-Za-z0-9-]{16,64}$`。
- host 在同一把鎖內處理：
  - ID 已見過、情境相同：回 `200` 與**同一個 run**，不再配發 task。場次可能已經結束，回覆照實帶出結束狀態。
  - ID 已見過、情境不同：`409 request_id_conflict`，什麼都不做。
  - 新 ID、有場次在 `running`／`stopping`：`409 run_active`，帶出那一場。
  - 新 ID、沒有進行中的場次：配發 task，寫日誌 `accepted`，然後才啟動。日誌寫不進去就回 `503 run_journal_unavailable`，不配發、不啟動。
- 第一場很快結束、回覆遺失、主持人再送一次：仍是同一個 ID，所以拿回同一場，不會建立第二場。按鈕停用只是輔助。
- 頁面重新整理會失去 request ID，下一次按鈕就是新的操作。為了避免主持人不知道上一場已建立，主持人頁面一律列出最近場次（`GET /api/host/runs`），開始前先看得到上一場。

**場次日誌**（新，host 自己的檔案，不在 Store 內，不改 Store 語意）：

- 路徑 `<APA_DB_PATH>.runs.jsonl`，權限 0600，只追加，每筆寫入後 fsync。token 路徑檢查也要排除它，和現有排除資料庫檔案的做法相同。
- 兩種紀錄，只有固定欄位與代碼，不含 token 或 CLI 文字：
  - `{"type":"accepted","request_id","run_id","scenario","task_id","at"}`：配發後、`StartTask` 之前寫入。
  - `{"type":"ended","run_id","outcome","detail","task_started","store_end","store_end_error","at"}`：`runOne` 返回後寫入。
- host 啟動時讀回：
  - 有 `accepted` 沒有 `ended` 的場次標為**結果不明**（上次 host 異常終止）；
  - 這些 request ID 仍然有效：同 ID 重送回該場，狀態「結果不明」，**不會被當成新場次**；
  - 只容許最後一行因寫到一半而不完整（略過並在就緒面板註明），其他損壞就拒絕啟動，不修補，和 Store 恢復失敗的處理一致。
- 配發 task 時，跳過 Store 已有的 task，也跳過日誌提過的 task：異常終止前已配發、但還沒寫進 Store 的 task 不會被重用。
- 結果不明的場次不算「進行中」（已經沒有程序），所以可以開始新場次，但主持人頁面會持續列出，直到主持人處理（第 7 節）。

**停止與取消是兩件事**：

- **停止**作用在場次（程序）。只在 fake CLI 還在跑（場次 `running`）時可按。結果是程序結束，buyer host 依既有規則寫 `TASK_CANCELLED`，未決 Ask 會撤回或到期。
- **取消 task** 作用在 Store 的 task。用在場次已結束、task 仍未收尾的情況：Ask 在等待、Ask 到期後、取消紀錄先前寫入失敗、或結果不明。它不涉及任何程序。
- fake CLI 已結束、Ask 還在等待時：「停止」停用（場次已 `ended`）；「取消 task」可用。主持人通常不必按，Ask 30 秒後由到期規則收尾。
- 「已停止」只在場次 `ended`，**而且**收尾紀錄寫入成功（`store_end` 為 `TASK_CANCELLED`）時才顯示。寫入失敗的顯示見第 7 節。

**Ask**：

- 不自動核准，也不提供核准按鈕。主持人面板固定顯示：「M1 無核准能力：Ask 停在等待，30 秒後由後端記錄到期。」
- 30 秒是已接受的到期規則，本批不改。
- 核准（WebAuthn）、grant、Signer、付款都列為後續工作（第 9 節），本批不暗中加入。

## 5 重複展示與歷史（§4.3）

**每場新 task**：

- 現有 registry 是 B1-02 的契約 fixture（`testdata/contracts/registry.json`），**不修改**。
- 新增 demo 專用的可信設定 `testdata/demo/registry.json`，用既有的 `--registry` 旗標傳入：
  - consumers、wallets、assets、`merchant_pay_to` 與契約 fixture **完全相同**（同錢包、同地址）；
  - tasks 保留 `task-a`、`task-a-next`、`task-b`，另外預置每位 consumer 20 個展示 task：`show-a-01`…`show-a-20`（consumer-a／wallet-a）、`show-b-01`…`show-b-20`（consumer-b／wallet-b）。
- 新增一個測試，斷言 demo registry 除了 tasks 是超集以外，與契約 fixture 相同。這樣 A／B 錢包與隔離不會因此偏移。
- host 配發 task 的規則：該 consumer 依編號第一個「不在 Store、也不在場次日誌」的展示 task。已建立的 task 永遠不重跑。
- `ponytail:` 上限是每位 20 場，是 **M1 的暫時限制**，不是最終免維護的體驗。用完時開始鈕回 `409 {"error":"no_free_task"}`，頁面顯示「展示 task 已用完：請設定新的 `APA_DB_PATH` 後重啟，或擴充 demo registry」。不刪資料庫。日後若真的不夠，再改為 host 依需要產生 task 綁定，那會動到 registry 的信任模型，要另外設計。

**同一 consumer 沿用錢包**：

- 同一 consumer 的所有展示 task 都綁同一錢包。
- Store 現有的 `WALLET_BINDING_CHANGED`／`WALLET_ID_IN_USE` 規則照常保護，不同 consumer 的資料與錢包仍隔離。

**歷史回放與新場次的區別**：

| | 歷史回放 | 新的展示執行 |
|---|---|---|
| 入口 | 頁面「歷史展示回放（唯讀）」（現有） | 主持人「開始」 |
| 是否寫入 | 不寫任何資料 | 新 task、新事件 |
| 作用對象 | 已保存的某個 task | 新配發的 task |

不刪資料庫、不覆寫事件、不重設歷史。

**舊資料庫**：既有 `var/apa-demo-fake.db` 裡的 task-a／task-b 照樣可讀，因為 Store 以保存的綁定回應（交付點 1 R2）。換成 demo registry 不影響它們。

**頁面的 task 清單**：

- 移除固定的 `FIXTURE_TASKS`。
- 新增唯讀端點 `GET /api/tasks`：viewer 只得到自己 consumer 的 task（依 Store 保存的 consumer 判斷，與 `ReadTask` 相同），host 角色得到全部。回最新在前的 task ID、狀態與開始時間。
- 需要在 Store 加一個唯讀查詢。這是新的 Store 讀取函式，不改寫入規則、事件或契約。
- 主持人開始一場後，頁面自動開啟該 task（第 6 節）。手動輸入 task ID 保留。

## 6 認證與權限（§4.4）

token 仍在啟動時產生、寫入 0600 檔、不進 log；每次啟動換新。viewer token 不擴大成控制權限。host token 也保持唯讀，以免改變交付點 1 已驗收的「主持人唯讀」語意。控制權限只給新的 operator 角色（3.2 起才產生）。

### 6.1 端點與角色（完整權限表）

「無」＝沒有或錯誤的 token。○＝允許，403＝`forbidden_role`，401＝`unauthenticated`。

| 端點 | 無 | viewer | host | operator | 切片 |
|---|---|---|---|---|---|
| `GET /healthz`、`GET /`、`GET /host`、`GET /ui/{name}`、`GET /unity/{name}` | ○ | ○ | ○ | ○ | 現有；`/host` 為 3.1 新增的靜態頁 |
| `GET /api/viewer` | 401 | ○（自己的 consumer） | 403 | 403 | 現有 |
| `GET /api/tasks/{id}`、`…/events` | 401 | ○（只限自己 consumer，否則 404） | ○（全部） | **403** | 現有；operator 的 403 為 3.2 新增 |
| `GET /api/tasks` | 401 | ○（只列自己的） | ○（全部） | 403 | 3.3 |
| `GET /api/host/status` | 401 | 403 | ○ | ○ | 3.1（host）；3.2 加 operator |
| `GET /api/host/runs`、`GET /api/host/runs/{run_id}` | 401 | 403 | ○（唯讀） | ○ | 3.2 |
| `POST /api/host/runs` | 401 | 403 | 403 | ○ | 3.2 |
| `POST /api/host/runs/{run_id}/stop` | 401 | 403 | 403 | ○ | 3.2 |
| `POST /api/host/tasks/{task_id}/cancel` | 401 | 403 | 403 | ○ | 3.4 |

operator 不能讀 task 內容：task 內容只走 viewer（或唯讀的 host）。operator 的回覆只有固定的狀態與代碼。

**`POST /api/host/tasks/{task_id}/cancel` 的規則**：在與「開始」相同的 host 鎖內，依序判斷：

| 情況 | 回應 | 寫入 |
|---|---|---|
| Store 沒有這個 task | `404 not_found` | 無 |
| 該 task 有場次在 `running`／`stopping` | `409 run_active`（請用「停止」） | 無 |
| 已有 `TASK_CANCELLED`（包括重複取消） | `200 {"state":"cancelled","already":true}` | 無 |
| 已有 `TASK_TIMED_OUT` | `409 already_ended {"end":"TASK_TIMED_OUT"}` | 無 |
| 已 Deny（`denied`） | `409 not_eligible {"status":"denied"}`：Deny 已是終態，不追加取消 | 無 |
| 其他（`awaiting_approval`、`running`，包括 Ask 已到期與結果不明的場次） | 呼叫現有 `Store.Cancel` → `200 {"state":"cancelled"}` | `TASK_CANCELLED`，未決 Ask 依既有規則先到期或撤回 |
| `Store.Cancel` 回 `TASK_ALREADY_ENDED` | 重新讀取，依上表的已結束列回應 | 無 |
| 其他 Store 錯誤 | `503 store_unavailable {"code":…}` | 無 |

**競態**：

- 「是否有進行中的場次」的檢查與 `Store.Cancel` 都在 host 鎖內。開始新場次也要拿同一把鎖，而新場次只會配發新 task，不會碰到這個 task。
- 場次結束時 buyer host 已撤銷工具能力（`Revoke`），之後不會再替它寫入。
- 「已結束」的判斷由 `Store.Cancel` 在自己的寫入交易內完成，所以兩個取消同時到達，只會有一個寫入，另一個得到 `TASK_ALREADY_ENDED`。
- 到期 worker 可能同時寫 `APPROVAL_EXPIRED`；`Store.Cancel` 的交易會先處理到期，順序一致。
- 不需要修改 Policy 或 Store 語意。

### 6.2 控制請求防護

沿用 `authorize` 的順序（Host 必須是綁定位址；Origin 必須等於自己或不存在；只認 `Authorization: Bearer`），所有 POST 再加三項：

- **必須帶 `Origin`，且等於 `http://<綁定位址>`**。瀏覽器的 POST 一定會送；缺少就拒絕，擋住跨站表單。
- `Content-Type` 必須是 `application/json`。不開 CORS，跨站 JSON POST 需要預檢，所以會失敗。
- body 上限 1 KiB，嚴格解碼（`DisallowUnknownFields`）：開始只接受 `request_id` 與 `scenario`，停止與取消不接受 body 欄位。

錯誤一律回固定代碼：

- 認證與請求：`unauthenticated`、`forbidden_role`、`forbidden_origin`、`bad_request`、`not_found`；
- 場次：`run_active`、`run_not_running`、`request_id_conflict`、`no_free_task`、`unknown_scenario`；
- 取消：`already_ended`、`not_eligible`；
- 儲存：`run_journal_unavailable`、`store_unavailable`。

### 6.3 主持人本機控制頁與 token 檔（3.1 細部規格，可單獨 review 與派工）

**兩個頁面**：

- `/`（現有）是一般 viewer 頁：只能手動貼一個 viewer token，**沒有**載入 token 檔的功能。
- `/host`（新）是主持人在本機使用的控制頁：
  - 包含同一套 Dashboard 與 Unity 商店（沿用 `web/` 模組）；
  - 另有就緒面板、「載入本機 token 檔」、consumer 切換、「清除全部憑證」。
- 兩頁都只是靜態檔案，不含任何資料；權限仍由伺服器依 token 判斷。

**3.1 載入的 token**：

- 只有 **host＋viewer**（現有的 token 檔內容）。operator 到 3.2 才加入，那時同步更新本頁的檢查。
- host token 只用於 `GET /api/host/status`；Dashboard 讀 task 一律用目前選定 consumer 的 viewer token。

**檔案檢查**（任一項不通過，整個檔案都不採用，並先執行「清除全部憑證」）：

1. 大小 ≤ 16 KiB；超過就不讀內容。
2. `JSON.parse` 成功；頂層是物件，只接受 `note`（字串）與 `tokens`（陣列）兩個鍵。
3. 每筆的 `role` 只能是 `viewer` 或 `host`；`token` 符合 `^[0-9a-f]{64}$`。
   - viewer 必須有 `consumer_id`，符合 `^[A-Za-z0-9._-]{1,64}$`；host 不得有 `consumer_id`。
   - 恰好一個 host、至少一個 viewer；token 與 consumer_id 都不得重複。
4. 向伺服器確認檔案屬於目前這次啟動：
   - host token 呼叫 `GET /api/host/status` 必須 200；
   - 每個 viewer token 呼叫 `GET /api/viewer` 必須 200，且回傳的 `consumer_id` 與檔案相同。
   - 任何一個不符，就顯示「token 檔與目前服務不符（服務可能已重啟），請重新載入」。
5. 讀完立刻清空選檔欄位（`input.value = ""`）。檔案文字與解析結果只存在載入函式的區域變數，函式結束即丟棄；頁面只保留 `{host, viewers: {consumer_id: token}}`。
6. 錯誤訊息是固定文字，**不包含**檔案內容、token 或路徑。

token 不寫入 localStorage、sessionStorage、cookie、URL、console 或 Unity，與現有規則相同。

**操作與清除規則**：

| 操作 | 保留 | 清除 | 之後的畫面 |
|---|---|---|---|
| 切換 consumer（按 A／B） | host token、其他 viewer token、就緒面板 | 目前 viewer 的畫面狀態：沿用現有 `forget()`，包括 Inspector、Timeline、展開的 JSON、回放、Unity 客人與 `#unity-status` | 以新 consumer 的 viewer token 驗證，等待選擇 task |
| 清除全部憑證 | 無 | host 與全部 viewer token、就緒面板內容、Dashboard 與 Unity 畫面 | 「尚未載入 token 檔」 |
| 載入新檔 | 無 | 先執行「清除全部憑證」，再檢查新檔 | 通過才採用；失敗就停在清除後的狀態 |
| 手動 viewer 模式（在 `/host` 貼一個 viewer token） | 無 | 先執行「清除全部憑證」 | 只持有那一個 viewer token；就緒面板顯示「需要 token 檔」 |
| 任何請求回 401（服務重啟後 token 全部失效） | 無 | 執行「清除全部憑證」 | 「服務已重啟或 token 失效，請重新載入 token 檔」 |
| `pagehide`、重新整理 | 無 | 全部（記憶體隨頁面消失） | 重新整理後要重新選檔 |

在 `/host` 上，viewer 的 401 也一律清除全部憑證：同一個檔案裡的其他 token 也已過期。`/` 頁維持現有行為，只清除那位 viewer。

**就緒端點 `GET /api/host/status`（3.1，host 唯讀）的回覆**只有固定欄位，不含 token、檔案路徑或 CLI 文字：

```json
{"http":"ok","store":{"state":"ok","last_expiry_error":""},"fake_cli":{"state":"ok","version":"fake-claude …"},
 "live_cli":"off","unity_build":{"state":"ok","files":["WebGL.data","WebGL.framework.js","WebGL.loader.js","WebGL.wasm"]},
 "payment_capability":"unavailable","served_at":1790726400}
```

- `store.state`：最近一次到期 worker 的結果（每秒一次，會碰資料庫）。錯誤時是 `error`，並帶出最近一筆錯誤代碼。
- `fake_cli.state`：沒有帶 `--fake-cli` 時是 `off`。
- `unity_build.files`：只列存在的四個建置檔名稱。
- 3.2 起再加 `runs`（進行中與結果不明的場次）。
- 頁面在載入 host token 後、頁面可見時，每 5 秒讀一次。

**3.1 的範圍**：

- 啟動仍沿用現有自動計畫：`make demo` 帶 `--plan`，啟動時自動跑 task-a、task-b，與 `make demo-integration` 相同。
- 3.1 不新增任何控制或寫入端點。

**考慮過但不採用**：

- 一次性配對碼換 session：要新增登入交換端點與 session 狀態，且 cookie 被既有規則排除。
- 用 `pbcopy` 寫入剪貼簿：剪貼簿其他程式也讀得到。
- 把 token 放進啟動時打開的網址：違反「token 不放 URL」。

## 7 錯誤呈現（§4.5）

**三種狀態分開判斷**：

- **場次**（程序）：`running`、`stopping`、`ended`、`unknown`（結果不明）。來源是 host 記憶體與場次日誌。
- **task 狀態**：Store 依事件投影（`project()`）的 `running`、`awaiting_approval`、`denied`、`cancelled`、`failed`。
- **收尾紀錄**：`runOutcome` 的 `store_end`／`store_end_error`，或「取消 task」的回應。

注意：Store 在**沒有未決 Ask、也沒有終止事件**時，task 狀態就是 `running`。所以 Ask 正常到期後也會回到 `running`。**不能只看 task 狀態是 `running` 就判斷上一場當機**；異常終止只由場次日誌（有 `accepted`、沒有 `ended`）判斷。畫面文字依事件與場次一起決定，不直接翻譯 `running`。

| 情況 | 場次 | task 狀態 | 收尾紀錄 | 主持人畫面 | 主持人可以做 |
|---|---|---|---|---|---|
| Ask 成立，fake 正常結束 | `ended`（`ok`） | `awaiting_approval` | 不需要 | 「場次完成：等待核准（M1 無法核准，30 秒後到期）」 | 開始下一場；必要時「取消 task」 |
| 上一列之後 Ask 到期 | `ended`（`ok`） | `running`（已有 `APPROVAL_EXPIRED`） | 不需要（到期是 Store 規則） | 「場次完成：Ask 已到期」 | 開始下一場；必要時「取消 task」 |
| Deny | `ended`（`ok`） | `denied` | 不需要 | 「場次完成：Deny（原因代碼）」 | 開始下一場 |
| 執行中按停止，取消寫入成功 | `stopping` → `ended`（`cancelled`） | `cancelled` | `store_end = TASK_CANCELLED` | 「停止中…」→「已停止，取消已記錄」 | 開始下一場 |
| 執行中按停止，取消寫入失敗 | `ended`（`cancelled`） | 寫入前的狀態 | `store_end_error = 代碼` | 「程序已停止，但取消紀錄寫入失敗（代碼）；task 尚未收尾」，**不顯示「已停止且已完成收尾」** | 「取消 task」重試；Store 持續失敗就停止 host、查 stderr |
| fake CLI 逾時 | `ended`（`timeout`） | `failed`；若先前已 Deny 則仍 `denied` | `TASK_TIMED_OUT`，或 `store_end_error` | 「CLI 逾時」；寫入失敗時同上一列的寫法 | 開始下一場（新 task） |
| 建立 task 失敗（`prepare_failed:代碼`） | `ended` | 不存在 | 無 | 「未建立 task（代碼）」 | 開始下一場 |
| 其他場次錯誤（`host_error`） | `ended` | 依事件 | 依 `runOutcome` | 「場次錯誤（代碼）」，另列 task 狀態 | 開始下一場；必要時「取消 task」 |
| host 正常關閉時有場次在跑 | 關閉時取消，寫 `ended` | `cancelled`（寫入成功時） | `TASK_CANCELLED` 或 `store_end_error` | 重啟後列在最近場次 | 開始下一場 |
| host 異常終止（kill -9、當機） | `unknown`（日誌有 `accepted` 沒有 `ended`） | 依事件，可能是任何一種 | 不明 | 「上次執行結果不明」，列出 task 與目前狀態 | 依 task 狀態「取消 task」（第 6.1 節條件），或保留不動；同 request ID 重送只會拿回這一場 |
| 啟動失敗（埠、鎖、fake CLI、Store 恢復、場次日誌損壞） | — | — | — | 終端機原樣顯示原因，exit 非 0；沒有頁面 | 依訊息處理，再 `make demo` |
| Unity 建置缺少或載入失敗 | 不影響 | 不影響 | — | 「Unity 場景未就緒」；Dashboard 仍可用 | 完成首次準備後重新整理；可以照常開始，但不會有商店畫面 |
| Store 不可用（讀取 503） | 不影響 | 讀不到 | — | 「Store 不可用」；不顯示成功 | 停止 host、查 stderr；不自動修復 |

每一次「開始下一場」都是新 run、新 task。原 task 不會重跑。

**一律不做**：

- 不默默重跑整場；
- 不自動切到真實 CLI；
- 不把失敗、停止或逾時顯示成成功或付款；
- 不刪資料庫。

## 8 實作切片（B1-06 交付點 3）

沿用 B1-06 編號：交付點 1（Store 接線與唯讀 HTTP，已驗收）、交付點 2（Unity＋LimeZu 與 Dashboard，已驗收），本批為**交付點 3：展示操作流程**，拆成 3.1–3.5。

| 切片 | 修改的檔案與元件 | 使用者看到的結果 | 驗收測試 | 相依 |
|---|---|---|---|---|
| **3.1 一鍵啟動與就緒** | `Makefile`（`demo`）、`scripts/demo/start.sh`；`cmd/demo`：`GET /api/host/status`（host 唯讀）、記住最近一筆到期錯誤、`GET /host` 靜態頁；`web/`：`host.html`、就緒面板、token 檔載入與第 6.3 節的清除規則 | 一個指令啟動；`/host` 列出 HTTP／Store／fake CLI／Unity 建置／LimeZu 是否就緒；選一次檔案就能在 A／B 之間切換查看，不用複製 token | 腳本：缺建置、缺匯入、埠佔用、鎖住時的訊息與 exit code。HTTP：status 依第 6.1 節的權限（viewer 403、無 token 401、Host／Origin），回覆不含 token 或路徑。JS 單元：第 6.3 節每一條檔案檢查各有一個拒絕案例（過大、非 JSON、多餘的鍵、未知角色、格式不符、重複、兩個 host、沒有 viewer）。瀏覽器（真 Unity 建置）：選檔後選檔欄位為空；storage／cookie／URL／console 都沒有 token；A↔B 切換後畫面不殘留；清除全部、載入新檔、手動 viewer 模式、重啟服務後的 401 都清掉舊憑證與舊畫面；舊檔案在重啟後被拒絕；`/` 頁沒有載入 token 檔的入口 | 無（不需新控制 API、新角色或伺服器端認證變更） |
| **3.2 主持人控制及權限** | `cmd/demo`：operator 角色、`scenarios.json`、場次日誌、`POST /api/host/runs`（request ID）、`…/stop`、`GET /api/host/runs*`、單場鎖、讀取端點拒絕 operator；`testdata/demo/scenarios.json`、`testdata/demo/registry.json`；`web/`：`/host` 的主持人面板、最近場次清單、token 檔接受 operator | 選情境按「開始」，fake 客人進店到 Ask 或 Deny；重送與連點都不重複；執行中可停止 | HTTP：第 6.1 節整張權限表；第 6.2 節的請求防護；request ID 的四種情況（新、重送同情境、重送不同情境、有進行中的場次）；第一場已結束後同 ID 重送仍回同一場；日誌寫入失敗不配發。整合：開始後 Store 有新 task，事件序列與 `ask`／`deny-budget` 案例一致；停止要等收尾寫入才 `ended`，寫入失敗（測試替身）不顯示已停止；重啟後同 ID 回「結果不明」的同一場，配發跳過日誌提過的 task；demo registry 與契約 fixture 一致 | 3.1 |
| **3.3 新一場 task 與 UI 選擇** | Store 唯讀清單查詢；`GET /api/tasks`；`web/adapter.js` 移除 `FIXTURE_TASKS`；開始後自動切 viewer 並開啟新 task | 每場是新 task；清單顯示該 consumer 的所有場次；舊場次可回放 | HTTP：viewer 只看到自己的 task（含 registry 移除後的舊 task 規則）；瀏覽器：連續兩場是兩個 task、前一場仍在清單且可回放；回放不寫資料 | 3.2 |
| **3.4 停止、錯誤、重啟恢復** | `cmd/demo`：`POST /api/host/tasks/{id}/cancel`（第 6.1 節規則）、場次結果對照；`web/`：第 7 節的狀態文字 | 每種結果都如實顯示，並列出可採取的操作；結果不明與未收尾的 task 可以取消 | HTTP：取消的每一列（404、409 run_active、重複取消 200 already、已逾時 409、Deny 409 not_eligible、成功）與並行兩個取消只寫一次；整合：fake 逾時、Store 寫入失敗、host 正常關閉與異常終止後重啟；Ask 到期後的 `running` 不被標成異常；每種都沒有成功字樣 | 3.2 |
| **3.5 實際瀏覽器操作驗收** | `scripts/verify/browser-ui.mjs` 新增操作流程段 | 一次跑完主持人實際會做的操作 | 真 Unity Web 建置：`make demo` 等同的啟動 → 選檔 → Ask 一場 → Deny 一場 → 中途停止一場 → 連點開始 → 重啟後清單與狀態；截圖；證據不含 token | 3.1–3.4（每個切片也各自補自己的瀏覽器檢查，3.5 是整體串接） |

**本輪、後續與 B1-06 完成條件的分界**：

- **本輪設計交付**：本文件與任務拆分，交 Codex review。不含任何程式。
- **後續實作交付**：3.1–3.5，各自實作、測試、review。每個切片要另行核准才開工。
- **B1-06 整體完成**：仍依任務卡，包含至少一次核准的 opt-in 真實 CLI trace、`tests/m1/`、`docs/m1-result.md` 與乾淨目錄重跑。交付點 3 全部通過也不代表 B1-06 或 M1 完成。

**建議的首批切片：3.1**。理由：

- 直接解決 Aaron 操作時的兩個痛點：逐一啟動、反覆找 token。
- **不需要新控制 API、新角色或伺服器端認證變更**：status 用現有 host 唯讀 token，token 檔只在 `/host` 頁的記憶體使用。風險與 review 範圍最小。
- 第 6.3 節是完整規格，可以單獨 review 與派工，不必等 3.2–3.4 的設計確認。
- 3.2–3.4 都建立在 `/host` 頁、就緒面板與 token 載入上；3.2 與 3.4 的控制、恢復設計（第 4、6.1、7 節）確認後再開工。

## 9 後續工作（不在本批）

- Ask 的真人核准（WebAuthn、Approver 權限映射）、grant、Signer、付款：PRD 第一期另有任務；本批不暗中加入任何一項。
- 真實 CLI 請購（B1-04 live 階段 B／C）：需 Aaron 另行核准。
- 剩餘瀏覽器情境：取消、CLI 逾時、Allow 在瀏覽器中觸發（交付點 2 未測項目，可併入 3.4／3.5）。
- PRD FR-23「從乾淨狀態重跑 success-ask、hard-deny」：3.2 的情境目錄是它的 fake 子集；prompt-injection／concurrent-spend 情境另列。
- LimeZu 素材與 Web 建置的公開發布：仍未核准。

## 10 需要 Aaron 決定的事項

無。

以下由 Codex review 判斷，不需要 Aaron 先決定：

- operator 角色是否合理；
- token 檔載入是否可接受；
- 每位 consumer 20 個展示 task 的上限。

## 11 修訂紀錄

**第二版（依 Codex 對 `7bb4695` 的 review）**：

| review 項目 | 修訂 |
|---|---|
| 1 單場鎖不能保證開始不重複 | 每次開始操作帶 request ID；同 ID 回同一場、不同情境拒絕；場次日誌讓重啟後同 ID 回到「結果不明」的同一場，配發跳過日誌提過的 task（第 4 節） |
| 2 程序結束、task 狀態、收尾成功混在一起 | 三種狀態分開，附對照表；不以 task 狀態 `running` 判斷當機（Ask 到期後也是 `running`），只以場次日誌判斷；取消寫入失敗不顯示「已停止」；停止（程序）與取消 task 分開，fake 結束後停止停用、取消 task 可用（第 4、7 節） |
| 3 token 選檔細節不完整 | 新增第 6.3 節：3.1 只載入 host＋viewer；只在主持人頁 `/host`；切換 consumer 與清除全部分開；清除全部、載入新檔、手動 viewer 模式、401 與重新整理的清除規則；檔案大小、格式、伺服器確認與清空選檔欄位 |
| 4 取消端點不在權限表 | 第 6.1 節完整權限表；取消只給 operator；條件、競態與各種回應（404、409、重複取消 200） |

保留的決定（review 同意）：

- 獨立 operator，viewer／host 保持唯讀；
- 本機選檔，token 只在頁面記憶體；
- demo registry 與契約 fixture 分開；
- 每位 consumer 20 個 task，是 M1 暫時限制；
- 正常展示不跑 Docker、不重建 Unity。
