# B1-06 展示操作流程設計（fake 展示模式）

狀態（2026-10-06）：**切片 3.5（fake 展示流程整體操作驗收）的自動化部分完成於 `667e933`；Claude Code 執行，待 Codex 獨立 review（第 16 節）。人工操作驗收待 Aaron 執行（16.3）。** 切片 3.4 已於 `3a6af59` 通過 Codex 限定範圍技術 review（第 15.8 節）；3.1–3.3 已通過（第 12.7、13.19、14.7 節）。既有展示資料庫預設不升級。B1-04／B1-06 整體仍 InProgress；3.5 通過只代表 fake 展示操作流程，不代表真實 Agent、付款、ZK 或 M1 完成。第 12–15 節保留當時的寫法。

**3.5 現況（2026-10-07）**：已通過 Codex 技術 review（最近的產品修正為 `d18efa2`）；Aaron 已實際操作並初步確認基本操作可用，16.3 完整人工清單未逐項確認。B1-04／B1-06 仍 InProgress。

**下一步設計（2026-10-07）**：商店展示深化設計見 [`docs/demo-presentation-design.md`](demo-presentation-design.md)：D1 設計於 `15d705a` 通過 Codex 限定 review（R1、R2 關閉）；Aaron 核准後，D1 實作於 `7ffc45d`，**於 `6338fd2` 通過 Codex 限定範圍技術 review，R1、R2、C1 關閉**（該文件 §15.7）。通過範圍為 fake 展示的網頁呈現與相關修正，不代表真實 Agent、付款、區塊鏈、ZK 或整體 M1 完成。一般 Chrome 長流程的可見性問題仍未確認。D2 第三輪設計補正完成（該文件 §16、§16.12–§16.14），待 Codex 限定 review，尚未核准實作；D3–D6 未開始。

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

> **已由第 13 節取代**：第三版改用 SQLite 協調資料表 `host_runs`，見第 13.2–13.4 節。
>
> **待修（Codex 對 `7d11894` 的 review）**：本段的 JSONL 場次日誌未通過 review，不得據此實作。原因有二：
> 1. 「只追加」「不修補」「略過不完整尾行後繼續服務」互相衝突：半行留著，再追加就會黏在一起。
> 2. SQLite 與日誌缺少配對與恢復規則：日誌遺失或只還原資料庫時，request ID 的去重會消失；`ended` 寫入失敗也還沒有定義。
>
> 建議方向是在既有 SQLite 中另設獨立的協調資料表，但仍要先提出一致性與恢復設計。這只是後續設計方向，不是新增資料表的授權。以下保留第二版原文作為歷史。

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

**就緒端點 `GET /api/host/status`（3.1，host 唯讀）的回覆**只有固定欄位，不含 token、檔案路徑或 CLI 文字。下例是第二版的草案；實作的欄位以第 12 節為準（Store 的狀態改為 `unconfirmed`／`ok`／`error`，fake CLI 不回版本文字）：

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

> 本節的「host 異常終止 → `unknown`（依日誌判斷）」一列由第 13.4、13.5 節取代；其他規則保留。

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
| **3.2 主持人控制及權限**（第二版；已由第 13.6 節取代） | `cmd/demo`：operator 角色、`scenarios.json`、場次日誌、`POST /api/host/runs`（request ID）、`…/stop`、`GET /api/host/runs*`、單場鎖、讀取端點拒絕 operator；`testdata/demo/scenarios.json`、`testdata/demo/registry.json`；`web/`：`/host` 的主持人面板、最近場次清單、token 檔接受 operator | 選情境按「開始」，fake 客人進店到 Ask 或 Deny；重送與連點都不重複；執行中可停止 | HTTP：第 6.1 節整張權限表；第 6.2 節的請求防護；request ID 的四種情況（新、重送同情境、重送不同情境、有進行中的場次）；第一場已結束後同 ID 重送仍回同一場；日誌寫入失敗不配發。整合：開始後 Store 有新 task，事件序列與 `ask`／`deny-budget` 案例一致；停止要等收尾寫入才 `ended`，寫入失敗（測試替身）不顯示已停止；重啟後同 ID 回「結果不明」的同一場，配發跳過日誌提過的 task；demo registry 與契約 fixture 一致 | 3.1 |
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

## 12 切片 3.1 實作紀錄（Claude Code 執行，待 Codex 獨立 review；程式 `0252ceb`）

依據：第 3、6.3 節，加上實作指示的三項條件：非同步世代控制、`pagehide` 主動清除、就緒不假設成功。

### 12.1 如何操作

```sh
make demo        # 檢查 Unity 建置與 LimeZu 匯入 → 編譯 Go → 啟動 host；印出 http://127.0.0.1:8402/host
```

1. 開啟 `http://127.0.0.1:8402/host`，按「載入本機 token 檔」，選 `var/demo-tokens.json`。
2. 就緒面板顯示各項狀態；按「以 consumer-a 查看」或「以 consumer-b 查看」切換 viewer，再選 task。
3. 「清除全部憑證」丟掉所有 token 與畫面；在下方貼一個 viewer token，即進入手動 viewer 模式，會先清除已載入的檔案。

**限制**：

- 啟動時仍依固定計畫自動跑 task-a（Ask）、task-b（Deny），與 `make demo-integration` 相同。
- 沒有「開始」按鈕，也不能連續建立新場次；那是 3.2／3.3 的範圍。
- `/host` 是唯讀頁，不能啟動、停止或改動 task。

### 12.2 修改

| 檔案 | 內容 |
|---|---|
| `Makefile`、`scripts/demo/start.sh` | `make demo`。缺建置或缺匯入只提示、不中止；`go build` 兩個二進位，再 `exec` host（同一份計畫）。不建 Unity、不跑測試或 Docker、不印 token；啟動被拒時保留 host 的訊息與 exit code。`APA_DEMO_BIN`、`APA_UNITY_PROJECT` 供測試使用 |
| `internal/store/ops.go` | 新增 `RunExpiryReporting`：同一個迴圈，每完成一次 `ExpireDue` 就回報結果（成功為 nil）。`RunExpiry` 改為呼叫它，只在錯誤時轉給原本的 `onErr`，行為不變。沒有第二個 worker，也沒有新的寫入 |
| `cmd/demo/status.go`、`main.go`、`api.go`、`ui.go` | `GET /api/host/status`（只接受 host token；viewer 403、無 token 401，沿用 Host／Origin 檢查）；`GET /host` 頁；記錄 worker 結果的 `expiryHealth` |
| `web/host.html`、`web/host.js` | 主持人頁：同一套 Dashboard 與 Unity 商店，加上 token 檔、consumer 切換、清除全部、手動 viewer 模式、就緒面板（每 5 秒，頁面可見時） |
| `web/hostcreds.js` | 不含 DOM 的憑證與世代邏輯：檔案檢查、向伺服器確認、世代、401 的套用規則、就緒讀取 |
| `web/app.js` | 匯出 `forget`、`useViewer`、`onUnauthorized`、`unityInfo` 給 `/host`；`pagehide` 改為呼叫 `forget()`（清 token、停輪詢、作廢回放、清畫面與 Unity）。`/` 頁的行為除此之外不變 |
| `web/style.css`、`web/embed.go` | 主持人頁樣式；嵌入並列名 `host.html`、`host.js`、`hostcreds.js` |

### 12.3 就緒狀態的定義（條件三）

`store` 欄位：

| 欄位 | 意義 |
|---|---|
| `state` | **最近一次完成的到期檢查**的結果：`unconfirmed`（還沒有任何一次完成，不顯示成功）、`ok`、`error` |
| `checked_at` | 那一次完成的時間（Store 時鐘） |
| `last_error`、`last_error_at` | 曾經發生過的最近一次錯誤代碼與時間。之後成功，`state` 會回到 `ok`，但這兩欄保留，頁面寫「之後已恢復」 |
| `target_period_seconds` | worker 的目標週期（預設 1）；頁面註明這不是延遲保證 |

- 沒有 Store 代碼的錯誤記成固定代碼 `STORE_ERROR`；原文仍只寫在 host 的 log。
- `GET /api/host/status` 只讀記憶體中的紀錄、檢查四個建置檔是否存在，不呼叫 Store 的任何方法，所以不會觸發到期或寫入。
- `fake_cli` 只回 `ok`／`off`（啟動時 `--version` 檢查通過與否），不回版本文字，以免把 CLI 輸出放進回覆。

### 12.4 非同步世代與 pagehide（條件一、二）

- 每次載入、清除全部、手動 viewer 模式、`pagehide`，`hostcreds` 都開新世代，並中止上一世代的請求。
- 讀檔、每一個確認請求、就緒讀取，在提交結果前都核對世代。
- 401 只在屬於目前世代時才清除全部，所以舊請求的 401 清不掉新憑證。
- Dashboard 的 viewer 請求本來就有 `app.js` 自己的世代；每次憑證換世代時，`host.js` 也會呼叫 `forget()` 清掉畫面。
- `pagehide`：
  - `host.js` 清除全部憑證、停止就緒輪詢；`app.js` 清除 viewer token、停止 task 輪詢、作廢回放、清除 Dashboard 與 Unity。
  - 回到頁面（bfcache 或重新載入）不會自動恢復，要重新選檔。
  - 這是應用程式不再持有或使用憑證；不宣稱瀏覽器記憶體被安全覆寫。

### 12.5 測試與證據（Claude Code 執行，待 Codex 獨立 review；全部在 `0252ceb`）

| 驗收點 | 測試 | 命令 | exit | 時間 | 證據 |
|---|---|---|---|---|---|
| 檔案檢查（大小、格式、角色、重複、伺服器確認、清空欄位）；世代：A 載入中清除、讀檔未完成時清除、A／B 交錯（晚到成功與晚到 401）、B 成功後舊 A 的 401、手動模式後舊驗證完成、舊就緒讀取、目前世代的 401、pagehide 後的晚到驗證 | `web/test/hostcreds.test.mjs` 13 項（另含 adapter 11 項） | `node --test web/test/adapter.test.mjs web/test/hostcreds.test.mjs`（已加入 `make check-contracts`） | 0 | 1 秒內 | `artifacts/b1-06/checks-0252ceb13c30-20261005T090324Z/` |
| status 權限、回覆不含 token 與路徑、建置檔狀態、`unconfirmed`／`ok`／`error`／恢復、worker 未跑時 `unconfirmed`、50 次查詢不改事件與資料庫檔案、`/host` 頁與 `/` 沒有選檔、腳本（埠佔用、資料庫鎖住、缺建置與匯入的訊息、啟動後不印 token、SIGTERM 結束） | `cmd/demo/status_test.go` | `go test -race -count=1 -v ./cmd/demo ./internal/store` | 0 | 53 秒 | 同上 |
| 實際瀏覽器（真 Unity Web 建置）：第 6.3 節與三項條件，共 31 項 3.1 檢查。用 CDP `DOM.setFileInputFiles` 選檔，用 `Fetch.requestPaused` 真的扣住確認請求來製造交錯；真的重啟 host（同一位址）造成 401；`pagehide` 用事件模擬，加上真的離開後返回 | `scripts/verify/browser-ui.mjs`（全部 96 項） | `node scripts/verify/browser-ui.mjs` | 0 | 119 秒 | `artifacts/b1-06/ui-0252ceb13c30-20261005T090424Z/`（含 `desktop-host-ready.png`） |
| 反向驗證 | 拿掉 `hostcreds` 的世代檢查：Node 13 項中 8 項失敗；瀏覽器 3 項 FAIL（B 被清掉、手動模式被覆寫）。`pagehide` 改回舊行為、`/host` 不處理：瀏覽器 3 項 FAIL（離開後仍持有 token 與畫面、仍在輪詢、返回時仍在） | 同上，在拋棄式 worktree | 1（預期） | — | `artifacts/b1-06/checks-0252ceb13c30-20261005T090324Z/hostcreds-reversal.*`、`artifacts/b1-06/reversal-0252ceb13c30/` |
| Unity | 未修改（`6aee7f0..0252ceb` 沒有改到 `unity/`）；瀏覽器載入的建置檔 sha256 與 `eda6469` 那次建置相同，所以沒有重建或重跑 Unity 測試 | — | — | — | `checks-…/webgl-loaded.sha256` |
| 完整容器（Go／JS＋fake 回歸） | **尚未執行**：Docker daemon 未啟動。開啟 Docker 後對 `0252ceb` 補跑一次，結果補記於此 | `sh scripts/verify/container-verify.sh 0252ceb…` | 1（未啟動） | 2 秒 | `artifacts/b1-06/container-0252ceb.log` |

說明：

- 瀏覽器反向驗證中，「A 載入中按清除全部」這一項在拿掉世代檢查後仍通過。原因是瀏覽器真的中止了 A 的請求，結果和清除一樣。這種情況由 Node 測試證明：那裡的晚到回覆無法被中止，拿掉檢查就失敗。
- `go test -race -count=1 ./...` 全部套件在本機也通過（108 秒，未另存證據）。

**失敗嘗試（保留）**：

- `start.sh` 第一版的 `"$missing）"`：shell 把全形括號當成變數名稱的一部分，`set -u` 下中止。所有變數已改用 `${…}`。
- `TestExpiryHealth` 第一次失敗：`store.Code` 對沒有 Store 代碼的錯誤回空字串，最近錯誤會變成空白。已改為記 `STORE_ERROR`。
- 腳本測試第一次的呼叫參數錯置：測試本身的錯誤，已修正。
- hostcreds 測試 12 第一次失敗：模擬重啟時沒有讓舊 token 失效，伺服器替身回了 403 而不是 401。這是測試本身的錯誤。
- 容器驗證第一次沒有執行：Docker daemon 未啟動（`artifacts/b1-06/container-0252ceb.log`）。

**未測**：

- 只用 Chrome（SwiftShader）。
- `pagehide` 的晚到回應用事件模擬；真的離開與返回只確認不會自動恢復。
- 腳本的 Store 恢復失敗情境只沿用 host 既有測試，腳本本身只原樣轉出 exit code。

### 12.6 依 Codex 對 `0252ceb` 的 review 修正（Claude Code 執行，待 Codex 獨立 review；程式 `b2b7daa`）

review 結論：3.1 暫不通過，需修三處，不只是補跑容器。Codex 的獨立結果：JS 24 項通過、Store race 通過；`cmd/demo` race 因 R3 失敗；沒有重跑完整七步、突變或真實瀏覽器。

| 項 | 問題 | 修正 | 回歸測試 | 反向驗證 |
|---|---|---|---|---|
| R1 | `useViewer()` 在 `fetch` 回來後核對了世代，但 `await res.json()` 之後沒有再核對。舊 viewer 的 JSON 晚到時，會覆蓋畫面上的身分與 task 選單（前端身分殘留；**沒有證明後端跨 consumer 授權被繞過**） | `web/app.js`：讀完 JSON 後再核對一次世代，不符就什麼都不顯示 | 瀏覽器：把頁面的 `fetch` 包一層，扣住下一個 `/api/viewer` 回覆的 JSON，在 `/host` 上分別經過切換 consumer、清除全部、`pagehide`，在 `/` 上經過「清除／切換」，再放行。斷言身分與 task 選單 | 拿掉這次核對：4 項 FAIL（畫面變回 consumer-a） |
| R2 | 手動輸入、尚未按驗證的 token，`forget()` 與 `pagehide` 都沒清掉 | `forget()` 一併清空 token 欄位。順序調整：`signIn` 先讀取並清空欄位，再呼叫 `/host` 註冊的 `onTypedSignIn` 清除已載入的檔案，最後登入。原本在 document 捕捉階段先切換手動模式的做法已移除 | 瀏覽器：兩個頁面上，輸入後按清除，或觸發 `pagehide`，欄位都變空；輸入後按驗證仍能登入（`/host` 進入手動模式） | 拿掉清空：6 項 FAIL。其中「仍能登入」失敗是連帶的：舊 token 留在欄位，新輸入接在後面，變成無效 token |
| R3 | `TestStartScript` 自己建立子程序環境，漏了 `TMPDIR`。唯讀容器中子程序寫 `/tmp` 失敗（`mkdir /tmp/apa-fake-home-…: read-only file system`） | 測試替子程序指定 `TMPDIR`：測試程序自己暫存目錄下的新目錄（容器中即 `/scratch/tmp`），測試結束刪除。路徑保持短，bridge 的 Unix socket 路徑有長度上限。容器限制未放寬 | 同一測試 | 在與完整驗證相同的唯讀容器設定下只跑這個測試：`0252ceb` 三個案例都失敗（與 Codex 相同的錯誤），`b2b7daa` 三個都通過 |

**命令與結果**（全部在 `b2b7daa`）：

| 命令 | exit | 時間 | 證據 |
|---|---|---|---|
| `node --test web/test/adapter.test.mjs web/test/hostcreds.test.mjs`（24 項） | 0 | 1 秒內 | `artifacts/b1-06/checks-b2b7daa6c02d-20261005T093834Z/` |
| `go test -race -count=1 -v ./cmd/demo ./internal/store` | 0 | 61 秒 | 同上 |
| `node scripts/verify/browser-ui.mjs`（108 項，新增 12 項 `3.1-R1`／`3.1-R2`） | 0 | 158 秒 | `artifacts/b1-06/ui-b2b7daa6c02d-20261005T093935Z/` |
| 反向驗證（瀏覽器 R1、R2；唯讀容器 R3） | 1（預期）／見上 | — | `artifacts/b1-06/reversal-b2b7daa6c02d/` |
| `sh scripts/verify/container-verify.sh b2b7daa…`（Go／JS＋fake 回歸，Unity 不在映像內；主機無其他負載） | 0 | 681 秒 | `artifacts/verify-container/runs/b2b7daa6c02d-20261005T095032Z/`（`artifacts/b1-06/container-b2b7daa.log`）：七步全過（check-contracts 19、check 84、race-cover 150、verify-b1-01 42、mutation 361 秒），check-contracts 含 adapter 11／11 與 hostcreds 13／13，`cmd/demo` race 通過（覆蓋率 85.3%），222／222 突變，17 項隔離 ok |

- Unity 未修改，建置 sha256 與 `eda6469` 那次建置相同（`checks-…/webgl-loaded.sha256`）。
- `go test -race -count=1 ./...` 全部套件在本機也通過，未另存證據。

**失敗嘗試**：第一次修 R3 時，把 `TMPDIR` 設在測試資料目錄下的深層路徑。bridge 的 Unix socket 路徑因此超過長度上限，host 啟動失敗，測試等滿 60 秒。已改用短路徑。

**容器**：`0252ceb` 的第一次容器驗證因 Docker daemon 未啟動而沒有執行（`artifacts/b1-06/container-0252ceb.log`）；之後改為驗證修正後的 `b2b7daa`，沒有對 `0252ceb` 補跑。

### 12.7 驗收紀錄：切片 3.1 於 `b2b7daa` 通過限定範圍技術 review（2026-10-05）

依據：Codex 對 `b2b7daa` 的限定範圍 review（Aaron 於對話中轉交），以及 vault 筆記 `Claude-Code-B1-06-交付點3.1驗收文件同步指示-b2b7daa-20261005`。

**結論**：B1-06 交付點 3.1 於 `b2b7daa` 通過 Codex 限定範圍技術 review，R1～R3 關閉，未發現新的阻擋問題。

- 通過範圍：一鍵啟動、主持人唯讀頁、token 檔載入、憑證清除與世代控制、就緒面板。
- 此結論不代表 B1-06、M1 或真實模型／付款流程整體完成。

**關閉理由**：

| 項 | 已確認的修正 |
|---|---|
| R1 舊 viewer JSON 覆蓋新身分 | `useViewer()` 讀完 JSON 後再次核對世代；切換、清除或 `pagehide` 後的舊結果不再提交到畫面。這是前端身分殘留，沒有證據顯示後端跨 consumer 授權被繞過 |
| R2 尚未送出的 token 未清除 | `forget()` 清空輸入欄位；手動登入先取出並清空輸入值，再清除主持人頁載入的憑證，最後登入。正常登入仍可用 |
| R3 腳本測試漏掉 TMPDIR | 子程序使用測試自己的短暫存路徑；在唯讀容器內可正常執行，不需開放 `/tmp` 或放寬檔案系統限制 |

**證據歸屬**：

**Codex 獨立執行**：在斷網、唯讀容器內，用 `b2b7daa` 的乾淨暫存副本執行；使用 fixture，不執行真實 CLI。

- 受控 JS 探測各三輪：舊 JSON 在切換 consumer、清除與 `pagehide` 之後才完成，不恢復舊身分或選單。
- 未送出 token 的清除與 `pagehide` 探測各三輪，以及清除主持人憑證後仍能手動登入的正向對照，全部通過。
- `node --test web/test/hostcreds.test.mjs web/test/adapter.test.mjs`：24 項通過。
- `go test -race ./cmd/demo ./internal/store -count=1 -timeout=240s`：兩個套件通過。
- `go test -race ./cmd/demo -run 'TestStartScript|TestHostStatus|TestExpiryHealth' -count=3 -timeout=180s`：三輪通過。
- 這些 JS 探測使用 DOM 與非同步回呼的替身，不等於 Codex 獨立操作真實瀏覽器。reviewer 的探測沒有加入正式 repo。

**Claude Code 執行、Codex 核對紀錄**（**不是** Codex 重跑）：

- 真實瀏覽器 108 項檢查：`artifacts/b1-06/ui-b2b7daa6c02d-20261005T093935Z/`。
- 完整容器七步全部 exit 0、222／222 突變被測試抓到、17 項隔離檢查：`artifacts/verify-container/runs/b2b7daa6c02d-20261005T095032Z/`。
- Node 與 Go race 測試：`artifacts/b1-06/checks-b2b7daa6c02d-20261005T093834Z/`。
- R1～R3 的反向驗證：`artifacts/b1-06/reversal-b2b7daa6c02d/`。
  - R1 四項失敗；
  - R2 六項失敗，其中兩項是連帶的登入失敗；
  - R3 舊版三個案例失敗、修正版通過。
- `0252ceb` 實作時的反向驗證：`artifacts/b1-06/reversal-0252ceb13c30/`、`checks-0252ceb13c30-20261005T090324Z/hostcreds-reversal.*`。

**Unity**：本輪沒有修改，也沒有重建。瀏覽器沿用 `eda6469` 那次建置（雜湊相同），不算新一輪的 Unity 驗證。

**歷史保留**：

- `0252ceb` 當時 Docker 未啟動，沒有完成完整容器驗證。這筆紀錄保留；後來的通過結果屬於 `b2b7daa`，不倒填到 `0252ceb`。
- 第 12.1–12.6 節的「待 review」是寫作當時的狀態；失敗嘗試見第 12.5、12.6 節。

**保留的限制**：

- 一個指令 `make demo` 啟動，再到 `/host` 載入本機 token 檔；不必每次重建 Unity 或跑驗證容器。
- 仍使用 fake CLI，啟動時依固定計畫自動執行 task-a／task-b；沒有主持人「開始」控制，也不能連續配發新場次。
- 不代表真實模型請購、付款、WebAuthn 或整體 M1 已完成。
- 只在 Chrome 驗證。晚到回應與 `pagehide` 的部分情境用受控模擬（事件模擬、扣住的請求或 JSON）；真實離開再返回另有一項檢查。兩者分開記錄。
- Unity 不在容器內。

**下一步**：

- 3.2–3.4 的控制與持久化設計仍待修訂及 review，未核准實作。
- 第二版的 JSONL 方案未獲接受；不因 3.1 通過而實作 JSONL 或新增 SQLite 協調表。

B1-04／B1-06 整體維持 InProgress。交付點 3.1 標為技術 review 通過；不合併、不標 Done、不開始 3.2。

## 13 第三版：主持人控制、新場次與場次協調（設計提案，待 Codex review；尚未核准實作）

> **修訂**：Codex 對 `838de81` 的 review 接受同一 SQLite 內保存場次協調資料的方向，要求補四處（R1–R4）。修訂見第 13.8 節；本節中被修訂的段落已加註，原文保留。

依據：Aaron 10/5 指示，以 `901f600` 為文件基準修訂。範圍限於：主持人控制、新場次、持久化與恢復、狀態分離、3.2 最小切片。只改設計，未新增資料表、未遷移資料庫、未寫控制 API。

**本節取代的未接受提案**：

| 原提案 | 狀態 | 由本節取代為 |
|---|---|---|
| 第 4 節「場次日誌」：`<APA_DB_PATH>.runs.jsonl` 與它的讀回、損壞、配發規則 | 未通過 review（`7d11894`） | 第 13.2 節：同一個 SQLite 檔內的 `host_runs` 協調資料表 |
| 第 4 節「開始的重複請求」中「依日誌判斷已見過的 ID」 | 隨日誌一起取代 | 第 13.3 節：以 `host_runs.request_id` 的 UNIQUE 與同一交易判斷 |
| 第 7 節表格「host 異常終止 → unknown（日誌有 accepted 沒有 ended）」 | 隨日誌一起取代 | 第 13.4 節：啟動時把沒有結束紀錄的場次標為 `interrupted` |
| 第 8 節 3.2 列 | 範圍與測試改寫 | 第 13.6 節 |

**保留不變**：

- 第 4 節的情境目錄、停止與取消分開、Ask 不自動核准；
- 第 5 節的 demo registry 與每位 consumer 20 個展示 task（M1 暫時限制）；
- 第 6.1 節的權限表（viewer／host 唯讀、operator 控制）；
- 第 6.2 節的請求防護；
- 第 7 節「Ask 到期後 task 也是 `running`，不能據此判斷當機」的規則。

**不重開**：Policy 裁決、Store 的事件、到期、取消、逾時與 task 狀態投影（`project()`）都不改。協調資料表不寫任何事件，也不參與 task 狀態。

### 13.1 主持人控制

| 操作 | 誰可以 | 端點 | 作用對象 | 切片 |
|---|---|---|---|---|
| 開始 | operator | `POST /api/host/runs` `{"request_id","scenario"}` | 新場次、新 task | 3.2 |
| 停止 | operator | `POST /api/host/runs/{run_id}/stop` | 進行中場次的程序；buyer host 依既有規則寫 `TASK_CANCELLED` | 3.2 |
| 查場次 | operator、host（唯讀） | `GET /api/host/runs`（最近 20 場）、`GET /api/host/runs/{run_id}` | 場次紀錄 | 3.2 |
| 取消 task | operator | `POST /api/host/tasks/{task_id}/cancel` | Store 的 task，不涉及程序；規則同第 6.1 節 | 3.4 |

- viewer 與 host 維持唯讀：所有 POST 對 viewer、host 都回 403。
- operator 不能讀 task 內容：`/api/tasks/*` 對 operator 回 403。
- token 檔多一個 operator（啟動時產生，0600，不進 log）；`/host` 頁在 3.2 起接受恰好一個 operator。

**主持人頁的操作流程（3.2）**：

1. 選情境，按「開始」。頁面產生 request ID（`crypto.randomUUID()`，只在記憶體），送出。
2. 收到 `run_id` 與 `task_id` 前，網路錯誤或逾時的重送都用同一個 ID。
3. 收到後，頁面用該場 consumer 的 viewer token（來自已載入的檔案）切換 viewer，開啟該 task。這沿用 3.1 的 `useViewer` 與既有的手動輸入 task ID 流程，不需要 3.3 的清單 API。
4. 場次 `running` 時可按「停止」；場次結束後停止鈕停用。
5. 最近場次清單一律顯示，開始前看得到上一場。

### 13.2 協調資料表 `host_runs`（取代 JSONL）

放在既有資料庫檔案內，由 Store 套件提供只碰這張表的函式。schema 版本 1 → 2，**純新增**，不改任何既有資料表：

```sql
-- 欄位要求已由第 13.8 節 R1（綁定欄位）與第 13.9.1 節（狀態與欄位）修訂
CREATE TABLE host_runs (
  run_id          TEXT PRIMARY KEY,
  request_id      TEXT NOT NULL UNIQUE,
  scenario        TEXT NOT NULL,
  consumer_id     TEXT NOT NULL,
  task_id         TEXT NOT NULL UNIQUE,      -- 配發即保留；不設外鍵，task 可能從未建立
  state           TEXT NOT NULL CHECK (state IN ('accepted', 'ended', 'interrupted')),
  accepted_at     INTEGER NOT NULL,
  ended_at        INTEGER,
  outcome         TEXT,                      -- runOutcome 的固定代碼
  detail          TEXT,
  task_started    INTEGER CHECK (task_started IN (0, 1)),
  store_end       TEXT,                      -- TASK_CANCELLED／TASK_TIMED_OUT／空
  store_end_error TEXT,                      -- 收尾寫入失敗的 Store 代碼
  CHECK ((state = 'ended') = (ended_at IS NOT NULL))
) STRICT;
```

**為什麼用 SQLite，不用 JSONL**：

- 協調紀錄與 task 在同一個檔案、同一個 WAL，備份或還原一起進退，不會出現「只還原資料庫，去重紀錄消失」。
- 每次寫入都是完整交易，沒有半行，也沒有「略過損壞尾行再追加」的問題。
- request ID 與 task 保留由 UNIQUE 約束保證，是第二層保護；第一層是 host 鎖內的判斷。
- 資料庫已有單一 host 鎖（`.lock`），同一時間只有一個程序寫這張表。

這**不是**宣稱問題自動解決。仍需要下面的交易邊界與恢復規則，因為建立 task、啟動程序、寫收尾紀錄本來就是分開的交易或動作。

**遷移**：

- （已由第 13.8 節 R2 修訂：遷移前先驗證、既有資料庫不預設升級）`migrate()` 在一個交易內處理：版本 0 → 建立全部（含 `host_runs`）並設為 2；版本 1 → 只 `CREATE TABLE host_runs` 並設為 2；版本 2 → 不動；其他版本照舊拒絕。
- 失敗就整個回滾，資料庫仍是版本 1。
- 舊程式碰到版本 2 的資料庫會拒絕開啟（既有規則：版本不是 1 就拒絕）。不支援降版；換回舊版請用新的 `APA_DB_PATH`。
- （已由第 13.8 節 R1 修訂：不依賴目前的 registry）`verify()` 現有的 `integrity_check` 與 `foreign_key_check` 自動涵蓋新表；另加檢查：`state` 與 `ended_at` 一致，且 `task_id` 必須在 demo registry 或已在 `tasks` 中。

### 13.3 交易邊界

一場的實際順序（`buyer.Host.Run`）：受理 → 啟動 fake 程序 → init 通過 Gate → `prepare()` 呼叫 `StartTask` 建立 task → 工具呼叫與裁決 → 程序結束 → 收尾紀錄（只有逾時或取消才寫）→ 場次結束紀錄。

| 交易 | 何時 | 內容 | 不做的事 |
|---|---|---|---|
| T1 受理（已由第 13.8 節 R3 修訂：先唯讀查詢，再寫入） | `POST /api/host/runs`，在 host 鎖內 | ① 查 `request_id`：已存在且情境相同 → 不寫，回原場；情境不同 → 不寫，409。② 有進行中的場次（記憶體）→ 409 `run_active`。③ 配發：該 consumer 依編號第一個「不在 `tasks`、也不在 `host_runs.task_id`」的展示 task；沒有就 409 `no_free_task`。④ 插入 `state='accepted'` 的一列。⑤ commit 後才啟動程序並回 202 | 不寫事件，不建立 task |
| T2 建立 task | init 通過後 | 既有 `StartTask`，語意不變 | 不碰 `host_runs` |
| T3 收尾 | 程序因逾時或取消結束 | 既有 `CLITimeout`／`Cancel`，語意不變 | 不碰 `host_runs` |
| T4 場次結束 | `runOne` 返回後 | 把該列改為 `state='ended'`，寫入 `ended_at`、`outcome`、`detail`、`task_started`、`store_end`、`store_end_error`；條件是 `state='accepted'` | 不寫事件 |
| R 恢復 | host 啟動、Store 開啟後、開始服務前 | 把所有 `state='accepted'` 改為 `interrupted`（單一 host 鎖保證它們都屬於上一個程序） | 不寫事件，不自動取消 task |

- T1 到 T4 都經過 Store 既有的 `write()`：BEGIN IMMEDIATE、鎖內取時鐘。`host_runs` 的時間只供顯示，不加入時鐘倒退檢查的高水位查詢，所以不影響既有的 `CLOCK_REGRESSION` 語意。
- 「程序已啟動」不另寫一筆紀錄：恢復時只需要知道 task 是否建立，這直接查 `tasks` 即可。

### 13.4 各階段中斷後如何恢復

| 中斷點 | 重啟後的資料 | 場次顯示 | 同一 request ID 重送 | 主持人可做 |
|---|---|---|---|---|
| T1 commit 前（含 host 當機） | 沒有紀錄 | 無 | 當成新請求（之前什麼都沒執行） | 重送或重按 |
| T1 commit 後、回覆送達前 | `accepted`（若 host 仍在）或 `interrupted`（若 host 已重啟） | 照實 | 回同一場，不重新執行 | 看清單 |
| T1 後、`StartTask` 前（程序未啟動或 init 前） | `interrupted`；`tasks` 沒有該 task | 「場次中斷：未建立 task」 | 回同一場（`interrupted`） | 開始下一場（新 ID、新 task）；該 task 名額保留不重用 |
| `StartTask` 後、程序結束前 | `interrupted`；task 存在，可能 `running`、`awaiting_approval` 或 `denied`，沒有收尾事件 | 「場次中斷」，另列 task 狀態與最後事件 | 回同一場 | 依第 6.1 節規則「取消 task」（3.4）；Ask 仍由到期 worker 收尾 |
| 程序結束、T3 收尾寫入失敗 | 若 T4 成功：`ended`，帶 `store_end_error` | 「程序已停止，但取消（或逾時）紀錄寫入失敗（代碼）；task 尚未收尾」 | 回同一場 | 「取消 task」重試（3.4）；Store 持續失敗就停止 host、查 stderr |
| 程序結束、T4 寫入失敗 | 記憶體：場次已結束但紀錄未寫入；資料庫仍是 `accepted`，重啟後變 `interrupted` | 「程序已結束，但場次紀錄寫入失敗（代碼）」，不顯示完成 | 回同一場（記憶體或資料庫） | 可開始下一場（程序確已結束；若 Store 仍失敗，T1 也會失敗） |
| host 正常關閉時有場次在跑 | 既有關閉順序：取消場次 → buyer 寫 `TASK_CANCELLED` → T4 → 關 Store | `ended`（`cancelled`） | 回同一場 | 開始下一場 |

**孤兒程序**：host 被 kill -9 時，fake 程序可能比 host 晚結束，但工具橋接與 Store 都在 host 程序內，孤兒程序無法再寫入任何紀錄。

### 13.5 狀態分離與畫面文字

三層分開取得、分開顯示：

| 層 | 來源 | 值 |
|---|---|---|
| 場次程序 | host 記憶體（進行中）＋`host_runs`（持久） | `running`、`stopping`（記憶體）；`accepted`、`ended`、`interrupted`（資料表） |
| task | Store 投影（既有，經 viewer API 讀取） | `running`、`awaiting_approval`、`denied`、`cancelled`、`failed`，或尚未建立 |
| 請購收尾 | 事件（`APPROVAL_EXPIRED`／`WITHDRAWN`、`TASK_CANCELLED`／`TIMED_OUT`）與 `store_end`／`store_end_error` | 各自如實顯示 |

**「完整」文字的條件**：

- 「已停止，取消已記錄」：場次 `ended`，**且** `store_end = TASK_CANCELLED`，**且** T4 已寫入。
- 「場次完成：等待核准／Ask 已到期／Deny」：場次 `ended` 且 T4 已寫入；後半句依 task 事件。
- 任何一層缺紀錄或寫入失敗，就改用第 13.4 節的「但…失敗」或「中斷」文字。不顯示完成，也不顯示付款或取貨。

### 13.6 切片 3.2 的最小實作範圍（供派工）

> 檔案、測試與驗收條件的增補見第 13.8 節的 R1–R4 與 13.8.5。

**範圍**：operator、開始、停止、查場次、`host_runs` 與恢復、情境目錄、demo registry、`/host` 的控制面板。**不含**：取消 task（3.4）、`GET /api/tasks` 清單（3.3）、核准、付款、真實 CLI。

| 檔案 | 修改 |
|---|---|
| `internal/store/store.go` | `schemaVersion = 2`；`migrate()` 處理 0 → 2 與 1 → 2（單一交易、純新增）；`verify()` 加 `host_runs` 一致性檢查 |
| `internal/store/runs.go`（新） | `AcceptRun`（T1，含 request ID 判斷與配發，候選 task 由呼叫者給）、`EndRun`（T4）、`InterruptOpenRuns`（R）、`Run`、`Runs(limit)`。只碰 `host_runs`，讀 `tasks` 只用來判斷配發 |
| `internal/store/recovery_test.go` | 「schema version」案例改為未知版本 3（版本 2 自此合法） |
| `cmd/demo/api.go` | token 檔加 operator；`record()` 與 `/api/viewer` 拒絕 operator（403） |
| `cmd/demo/control.go`（新） | 控制端點、host 鎖、記憶體中的 `running`／`stopping`、停止（取消該場 context）、第 6.2 節的 POST 防護 |
| `cmd/demo/scenarios.go`（新）、`main.go`、`fakeruns.go` | `--scenarios`（與 `--plan` 互斥）；啟動時呼叫 `InterruptOpenRuns`；`runOne` 結束後呼叫 `EndRun` |
| `cmd/demo/status.go` | status 加 `runs`：進行中與中斷的場次（只有 ID、task、狀態代碼） |
| `testdata/demo/scenarios.json`、`testdata/demo/registry.json`（新） | 情境 `ask`、`deny-budget`；契約 fixture 的超集（每位 consumer 20 個 `show-*` task） |
| `scripts/demo/start.sh` | 改用 `--scenarios` 與 demo registry，啟動時不再自動跑場次 |
| `web/hostcreds.js`、`web/host.js`、`web/host.html` | 接受 operator；控制面板（情境、開始、停止、最近場次、第 13.5 節的文字）；request ID 只在記憶體 |
| `scripts/verify/browser-ui.mjs` | 第 3.2 段 |
| `scripts/verify/mutation-check.py` | 新增突變：request ID 去重、單場鎖、配發跳過已保留 task、恢復標 `interrupted`、operator 讀 task 被拒、T4 寫入失敗仍顯示完成 |

**測試**：

- **Store**（`runs_test.go`）：
  - 版本 1 的資料庫遷移到 2：原資料不變、`verify` 通過；未知版本 3 仍拒絕。
  - `AcceptRun`：同 ID 同情境回原場；同 ID 不同情境拒絕；配發跳過 `tasks` 與已保留的 task；名額用完回 `no_free_task`；UNIQUE 第二層保護。
  - 用 Store 既有的測試掛鉤在 T1 commit 前、後中斷，以及 T4 失敗；之後 `InterruptOpenRuns` 把 `accepted` 標為 `interrupted`。
  - 所有 `host_runs` 操作前後，`events` 與 `tasks` 不變。
- **HTTP**（`cmd/demo`）：
  - 第 6.1 節權限表中 3.2 的列（viewer／host 不能 POST、operator 不能讀 task）；
  - POST 防護：缺 Origin、外站 Origin、錯 Content-Type、超過 1 KiB、多餘欄位；
  - 回應碼：202、200（重送）、409 `request_id_conflict`／`run_active`／`no_free_task`／`run_not_running`、400 `unknown_scenario`、503。
- **整合**：
  - 開始 `ask` → 新 task `show-a-01` 的事件序列對應已接受案例 `ask`；`deny-budget` → `show-b-01` 對應 `deny-budget`；
  - 第一場結束後同 ID 重送 → 同一場、沒有新 task；兩個同 ID 的請求同時到達 → 一場；
  - 停止 hang 情境 → `stopping` → `ended`，帶 `TASK_CANCELLED`；
  - 用 Store 掛鉤讓 T3 失敗 → `store_end_error`，不顯示已停止；讓 T4 失敗 → 「場次紀錄寫入失敗」；
  - 重啟（同資料庫）→ 中斷的場次標為 `interrupted`，同 ID 重送回它，不重跑；
  - Ask 到期後 task 回到 `running` 不被當成中斷。
- **瀏覽器**（真 Unity Web 建置）：
  - 載入含 operator 的 token 檔；開始 Ask → 自動切到 consumer-a 並在 Unity 看到新 task；
  - 連點只有一場；扣住第一次開始的回覆後重送 → 同一場；
  - 停止進行中的場次；重啟 host 後清單顯示中斷，沒有自動重跑；
  - operator token 不出現在 viewer 請求、storage、URL、console 或證據；viewer 頁 `/` 不受影響。
- **容器**：完整驗證一次（含新突變）；Unity 不變則沿用建置並記錄雜湊。

**驗收條件**：

1. 每場一個新 task；已建立或已保留的 task 不重跑、不重用。
2. 同一 request ID 不論何時重送（包括重啟後），都只對應一場、不重複執行。
3. 受理、建立 task、程序、收尾、場次結束各階段中斷後，都能依第 13.4 節顯示，並給出可採取的操作。
4. 程序停止但任何紀錄寫入失敗時，畫面不顯示完整成功。
5. viewer／host 唯讀不變；operator 不能讀 task 內容。
6. Policy、Store 的事件與狀態語意不變（既有測試全過，`host_runs` 操作不改 `events`、`tasks`）。
7. 不執行真實 CLI、不付款；`make demo` 啟動後不自動跑場次。

**相依**：3.1（已通過）。3.3（清單 API）與 3.4（取消 task、錯誤文字補齊）在 3.2 之後。

### 13.7 需要 Aaron 決定的事項

> 已由第 13.8 節 R2 修訂：既有展示資料庫不預設升級，改為需要 Aaron 另行確認，見 13.8.6。

無。

需要知道的一點：3.2 實作後，第一次啟動會把本機 demo 資料庫（例如 `var/apa-demo-fake.db`）單向升級到 schema 版本 2。既有的 task 與事件不變，但舊版程式之後無法開啟它。要保留舊版可開的資料庫，可以先複製一份，或在 3.2 改用新的 `APA_DB_PATH`。

### 13.8 第三版修訂（依 Codex 對 `838de81` 的 review；設計提案，待 Codex review）

review 接受的方向不變：同一 SQLite 保存場次協調資料、三層狀態分離、既有 Policy／Store 語意。本節只補 R1–R4 與測試計畫。仍不新增資料表、不跑容器、不開始實作。

#### R1：歷史場次不依賴目前的設定

**問題**：13.2 要求 `task_id` 在目前的 registry 或 `tasks` 中。若場次已受理、task 尚未建立就中斷，之後 registry 移除該 task，資料庫就打不開。

**修訂**：

1. **受理時保存可信綁定**。`host_runs` 在 T1 寫入當時由可信設定得到的完整綁定，以及情境快照：

   | 欄位 | 來源（T1 當時） |
   |---|---|
   | `task_id`、`consumer_id`、`agent_id`、`wallet_id`、`payer` | demo registry 的 `Binding(task_id)` |
   | `scenario`、`scenario_digest` | 情境目錄中該項的 id，與其內容（含 `budget_initial` 與 scenario 檔內容）的 sha256 |
   | `budget_initial` | 情境目錄 |

   恢復、顯示與驗證只用這些已存欄位，**不讀目前的 registry 或情境目錄**。

2. **task 已存在時核對綁定**。`tasks` 有同一個 `task_id` 時，`consumer_id`、`agent_id`、`wallet_id`、`payer` 必須與場次列相同（`tasks` 的值本身也是當初存下的，不讀目前 registry）。

3. **與實際 task／事件的一致性規則**，在 `verify()` 中逐列檢查，任一不符即視為損壞（`CorruptError`，check 名 `host_runs`），不開始服務：

   | 規則 | 條件 |
   |---|---|
   | 綁定一致 | 同上 |
   | 已結束且 `task_started = 1` | `tasks` 必須有該 task |
   | 已結束且 `task_started = 0` | `tasks` 不得有該 task（名額已保留，其他路徑不得建立它，見下方） |
   | `store_end` 不為空 | `task_started = 1`，且該 task 的事件中有對應的 `TASK_CANCELLED` 或 `TASK_TIMED_OUT` |
   | `store_end_error` 不為空 | `store_end` 必須為空 |
   | `outcome` 為 `cancelled` 或 `timeout`，且 `task_started = 1` | `store_end` 與 `store_end_error` 恰好一個不為空 |
   | `interrupted` 或 `accepted` | 只檢查綁定；task 可有可無 |

   `store_end` 為空不代表不得有結束事件：之後的「取消 task」（3.4）可能補上 `TASK_CANCELLED`。

   SQLite 的 `integrity_check`／`foreign_key_check` 只保證結構，**不能取代**這些規則。
4. **保留的名額不被其他路徑使用**。`--plan` 模式（3.1 起的固定計畫）若點到 `host_runs` 已保留的 task，`runOne` 回報 `task_reserved_by_run` 並略過，不建立它。

#### R2：遷移失敗與開檔失敗分開

**問題**：Store 目前的順序是先提交遷移，再執行 `verify()`。所以「失敗就回滾、仍是版本 1」只涵蓋遷移交易本身，不涵蓋之後的驗證失敗。

**修訂**：

| 情況 | 處理 |
|---|---|
| 新資料庫（版本 0） | 一個交易建立版本 2 的全部結構，然後 `verify()`。驗證失敗就拒絕服務，資料庫是版本 2 的空庫，刪除或換路徑即可 |
| 版本 2 | 照常：`verify()` 後服務 |
| 版本 1，**沒有**明確的升級指示（預設） | **不遷移**，拒絕開啟：「資料庫是 schema 1；3.2 需要 schema 2。請改用新的 `APA_DB_PATH`，或備份後以 `--upgrade-db` 升級」。不寫任何東西 |
| 版本 1，帶 `--upgrade-db` | 依下面四步 |
| 其他版本 | 照舊拒絕 |

**升級的四步與失敗後的狀態**：

1. **遷移前驗證**：以版本 1 的規則完整執行既有 `verify()`（唯讀）。失敗 → 不遷移、不寫入，仍是版本 1，拒絕開啟（與現在的損壞處理相同）。
2. **備份**：`VACUUM INTO '<db>.schema1-<UTC 時間>.bak'`，在同一把資料庫鎖內，產生一致的單檔快照（權限 0600）。失敗 → 不遷移，仍是版本 1，拒絕開啟。
3. **遷移交易**：`CREATE TABLE host_runs`，`user_version = 2`，一次提交。失敗 → 回滾，仍是版本 1，拒絕開啟；備份檔保留。**提交時點**就在這一步結束。
4. **遷移後驗證**：以版本 2 的規則執行 `verify()`。失敗 → 資料庫已是版本 2，拒絕服務。（恢復方式已由第 13.9.2 節修訂：不把備份換回原路徑，改複製到新路徑）恢復方式是由人工把第 2 步的備份檔換回原路徑（文件寫明命令），程式不自動還原、不降版。因為遷移只新增一張空表，第 1 步已通過時第 4 步理論上不會失敗，但仍明定處理。

**3.2 開發與測試**一律使用新的資料庫。既有展示資料庫（例如 `var/apa-demo-fake.db`）的升級由 Aaron 另行確認，不默認執行（13.8.6）。

#### R3：重送查詢與寫入分開

**問題**：T1 經過既有 `write()`，而 `write()` 在查 request ID 之前就做時鐘檢查。時鐘倒退時，連重送既有請求都會被拒絕。

**修訂後的 `POST /api/host/runs` 處理順序**：

1. 認證（operator）與第 6.2 節的請求防護；body 格式檢查：`request_id` 符合格式，`scenario` 符合 `^[a-z0-9-]{1,32}$`。這一步只檢查格式，**不檢查情境是否在目前目錄中**。
2. **唯讀查詢**（Store 的讀取連線，不經 `write()`、不做時鐘檢查）：依 `request_id` 找場次。
   - 找到、情境 id 相同 → 回 200 與原場次（含持久狀態與記憶體狀態）。**即使該情境已從目前目錄下架**，也照樣查回，不重新執行。
   - 找到、情境 id 不同 → 409 `request_id_conflict`，不寫入。
3. 沒找到，才適用目前的設定：
   - 情境不在目前目錄 → 400 `unknown_scenario`；
   - host 記憶體中有進行中的場次 → 409 `run_active`。
4. **寫入交易**（經 `write()`，含時鐘檢查）：在交易內**再查一次** `request_id`。若已存在（並發的同 ID 請求先寫入了），就不寫，依第 2 步的規則回應。否則配發 task、插入列、提交。時鐘倒退 → 503 `store_unavailable`（代碼 `CLOCK_REGRESSION`），什麼都沒寫。
5. 提交後才啟動程序，回 202。

`GET /api/host/runs/{run_id}` 與清單也是唯讀查詢，不受時鐘檢查影響。

#### R4：控制請求延續 3.1 的世代規則

**operator 的角色驗證**：

- `GET /api/host/status` 的回覆加上 `caller_role`（`host` 或 `operator`），只表示呼叫者自己的角色，不含 token。
- 載入檔案時：
  - host token 必須得到 `caller_role = host`；
  - operator token 必須得到 `caller_role = operator`；
  - 每個 viewer token 照 3.1 的方式驗證。
- 角色對不上就整個檔案不採用。operator token 只用於 `/api/host/status` 與 `/api/host/runs*`。

**開始／停止回應的世代檢查**（`hostcreds` 的世代，與 3.1 相同）：

| 規則 | 內容 |
|---|---|
| 記下世代 | 送出開始或停止時，記下當時的世代 `g` 與 request ID |
| 回應不屬於目前世代 | 丟棄：不切換 consumer、不登入 viewer、不開啟 task、不更新場次面板、不顯示「已開始」 |
| 401 | 只在屬於目前世代時才清除全部憑證（與 3.1 相同） |
| 重送 | 只在同一世代內、用同一個 request ID；世代一變就停止重送，**不會**把它重送成新場次 |
| 新 request ID | 只在主持人按下新的「開始」時產生 |

**清除瀏覽器憑證不等於撤回伺服器已受理的場次**：

- 伺服器上的場次照常執行到結束（或由主持人重新載入憑證後按「停止」）。
- 重新載入檔案後，最近場次清單照實列出它，但頁面**不會**自動切換過去；只有目前世代收到的開始回應，才會自動開啟新 task。

**正常的自動開啟流程**（目前世代）：

1. 開始回應 202（或重送得到 200）且世代相同；
2. 以該場 consumer 的 viewer token 呼叫 `useViewer`；
3. viewer 確認後（`app.js` 自己的世代仍相同），開啟 `task_id`；
4. 任何一步發現世代改變就停止。

#### 13.8.5 增補的測試計畫

**Store**（`runs_test.go`、`recovery_test.go`）：

- R1：
  - 場次已受理、task 未建立、之後換掉 registry（移除該 task）→ 仍可開啟，場次顯示中斷，綁定取自場次列；
  - 綁定不一致（偽造 `tasks` 或場次列）→ `CorruptError(host_runs)`；
  - 第 13.8 節 R1 表格的每一條規則各一個反例；
  - `--plan` 點到已保留的 task 被略過。
- R2：
  - 版本 1 沒有升級指示 → 拒絕，且檔案位元組不變；
  - 遷移前驗證失敗 → 不遷移，仍是版本 1；
  - 備份失敗 → 不遷移；
  - 遷移交易失敗（掛鉤）→ 回滾，仍是版本 1，備份保留；
  - 遷移後驗證失敗（掛鉤）→ 版本 2、拒絕服務，備份可換回並以版本 1 程式路徑驗證通過；
  - 新資料庫直接是版本 2。
- R3：
  - 時鐘倒退時，既有 request ID 的重送仍回 200 與原場次，新 ID 回 503 且什麼都沒寫；
  - 情境下架後，既有 ID 仍查回原場次，新 ID 用下架的情境回 400；
  - 兩個同 ID 的請求並發，只有一列、一個程序（交易內二次確認）。

**HTTP**：`caller_role` 的四種 token（無、viewer、host、operator）；operator 呼叫 `/api/viewer` 與 `/api/tasks/*` 回 403。

**Node**（`hostcreds.test.mjs` 擴充）：

- 含 operator 的檔案：角色對不上（host 與 operator 互換）→ 不採用；
- 開始已送出 → 清除全部 → 回應才到：沒有切換 consumer、沒有登入、沒有開啟 task、面板不變；
- 開始已送出 → 載入新檔 B → 舊回應才到：B 的狀態不變；
- 舊世代控制請求的 401 不清除新憑證；
- 世代改變後不再重送，也沒有產生新的 request ID；
- 正常流程：目前世代的 202 → `useViewer` → 開啟 task；兩步之間世代改變則停止。

**瀏覽器**（真 Unity Web 建置）：

- 正常流程：開始 Ask → 自動切到 consumer-a，Unity 顯示新 task；
- 用 `Fetch` 在**回應階段**扣住開始的回覆（伺服器已受理）→ 清除全部 → 放行：頁面沒有切換、沒有登入；
- 重新載入檔案：清單列出那一場、沒有新場次、不自動切換；
- 同樣的扣住 → 載入新檔 → 放行：新檔的狀態不變；
- 重送（同 ID）得到同一場；
- 停止回應的世代規則同上。

**驗收條件增補**（接在第 13.6 節的 1–7 之後）：

8. 恢復與驗證不依賴目前的 registry 或情境目錄；場次與 task 的綁定與收尾一致性有逐條檢查。
9. 既有資料庫不會在沒有明確指示時被升級；遷移前驗證、備份、提交、遷移後驗證各階段失敗時，版本與恢復方式如上表。
10. 既有 request ID 的查詢不受時鐘檢查與情境下架影響；新 ID 才套用目前設定與寫入檢查，交易內再次去重。
11. 舊世代的控制回應不重新登入、不切換 consumer、不恢復畫面、不重送成新場次；正常流程會自動開啟新 task。

**3.2 檔案增補**：

- `internal/store/store.go`：遷移前驗證、`VACUUM INTO` 備份、`--upgrade-db` 的開檔選項；
- `cmd/demo/main.go`：`--upgrade-db` 旗標，預設關閉；
- `cmd/demo/status.go`：`caller_role`；
- `cmd/demo/fakeruns.go`：`task_reserved_by_run`；
- `web/hostcreds.js`：控制請求的世代與重送。

#### 13.8.6 需要 Aaron 決定的事項

- **既有展示資料庫是否升級到 schema 2**：例如 `var/apa-demo-fake.db`。
  - 預設：不升級，3.2 使用新的 `APA_DB_PATH`；
  - 若要升級：由 Aaron 確認後，以 `--upgrade-db` 執行一次，並保留自動產生的備份檔。
  - 這不阻擋 3.2 的開發與測試。
- 其他：無。

### 13.9 R1、R2 補充（依 Codex 對 `a976fb9` 的 review；設計提案，待 Codex 限定 review）

review 結論：R3、R4 關閉；R1、R2 方向正確，各補一處。本節只補這兩處，不改其他設計。

#### 13.9.1 R1：狀態與欄位要求

**問題**：第 13.2 節的表定義允許 `task_started` 為 NULL，而 13.8 的一致性規則只處理 0 或 1。所以 `state = ended`、`ended_at` 有值、`task_started = NULL`、`outcome = NULL` 這類不完整紀錄沒有明確的拒絕規則。

**欄位要求**（「必填」＝NOT NULL 且合法；「留空」＝必須是 NULL）：

| 欄位 | `accepted` | `interrupted` | `ended` |
|---|---|---|---|
| `run_id`、`request_id`、`scenario`、`scenario_digest`、`budget_initial`、`task_id`、`consumer_id`、`agent_id`、`wallet_id`、`payer`、`accepted_at` | 必填 | 必填 | 必填 |
| `interrupted_at`（新增欄位） | 留空 | 必填 | 留空 |
| `ended_at` | 留空 | 留空 | 必填，且 ≥ `accepted_at` |
| `outcome` | 留空 | 留空 | 必填，必須是合法代碼（見下） |
| `detail` | 留空 | 留空 | 必填，可以是空字串。（格式規則已由第 13.10 節修訂：原本只收小寫，會拒絕合法的 `prepare_failed:<Store 代碼>`）只能是代碼字元 `^[a-z0-9_:]{0,64}$`，不含 CLI 文字 |
| `task_started` | 留空 | 留空 | 必填，0 或 1 |
| `store_end` | 留空 | 留空 | 必填：`''`、`TASK_CANCELLED` 或 `TASK_TIMED_OUT` |
| `store_end_error` | 留空 | 留空 | 留空，或 Store 代碼 `^[A-Z_]{1,64}$` |

- `interrupted` 的結果欄位一律留空，因為結果不明；畫面改由 task 與事件判讀（第 13.4 節）。
- **合法的 `outcome`**：buyer 的固定代碼 `ok`、`not_installed`、`start_failed`、`not_logged_in`、`rate_limited`、`timeout`、`cancelled`、`output_overflow`、`parse_error`、`nonzero_exit`、`missing_result`、`cli_error`、`orphaned_output`、`input_error`、`config_mismatch`、`turn_limit`、`store_unavailable`，加上 `skipped`（程序沒有啟動，例如暫存 HOME 建立失敗；這時 `detail` 是略過原因）。
  - 清單只放在 Go 程式中的一份常數（與 `buyer.Outcome` 對照），寫入與驗證共用。
  - 不寫進 SQLite 的 CHECK，避免日後新增代碼就得遷移。

**組合規則**（13.8 R1 表的增補）：

| 規則 | 條件 |
|---|---|
| 程序沒啟動 | `outcome = skipped` ⇒ `task_started = 0`，且 `store_end = ''` |
| 收尾成功與失敗互斥 | `store_end_error` 不為空 ⇒ `store_end = ''`（沿用） |
| 收尾只對應取消或逾時 | `store_end = TASK_CANCELLED` ⇒ `outcome = cancelled`；`store_end = TASK_TIMED_OUT` ⇒ `outcome = timeout` |
| 取消或逾時一定有收尾結果 | `outcome` 為 `cancelled` 或 `timeout`，且 `task_started = 1` ⇒ `store_end` 與 `store_end_error` 恰好一個不為空（沿用） |
| 狀態轉換 | 只允許 `accepted → ended`（T4）與 `accepted → interrupted`（R）；`ended`、`interrupted` 是終態 |

**三處都檢查**：

1. **資料表約束**：SQLite 的 CHECK 只表達上表的「必填／留空」形狀，以及 `state`、`store_end`、`task_started` 的列舉。寫錯形狀的列無法寫入。
2. **寫入**：
   - `EndRun`（T4）在交易前，用與驗證相同的函式 `validRunRow` 檢查整列（形狀、`outcome` 清單、組合規則），不合法就回 `INVALID_RUN_RECORD`，什麼都不寫。
   - 更新條件是 `WHERE run_id = ? AND state = 'accepted'`；影響 0 列就回 `RUN_NOT_OPEN`，不覆寫終態。
   - `InterruptOpenRuns`（R）同樣只改 `accepted` 的列，並填 `interrupted_at`。
3. **恢復驗證**：`verify()` 對每一列執行 `validRunRow`，加上 13.8 R1 與 task／事件的對照。不合法就是 `CorruptError(host_runs)`，不開始服務。這也涵蓋檔案在程式之外被修改、繞過 CHECK 的情況。

**增補測試**（`runs_test.go`）：

- 寫入路徑：
  - `EndRun` 傳入缺欄位、`task_started` 為 NULL、未知 `outcome`、未知 `store_end`、`store_end` 與 `store_end_error` 同時有值、`skipped` 卻 `task_started = 1`、`cancelled` 配 `TASK_TIMED_OUT`、`detail` 含非代碼字元：每一種都回 `INVALID_RUN_RECORD`，資料表不變。
  - 對 `ended` 或 `interrupted` 的列呼叫 `EndRun` 或 `InterruptOpenRuns`：不覆寫，前者回 `RUN_NOT_OPEN`。
- 約束：直接以 SQL 插入「`ended` 但 `task_started` 為 NULL」「`ended` 但 `outcome` 為 NULL」「`accepted` 但 `ended_at` 有值」「`interrupted` 但 `outcome` 有值」，都被 CHECK 擋下。
- 恢復驗證：用測試連線開啟 `PRAGMA ignore_check_constraints = ON`，寫入上面各種不完整、矛盾或未知代碼的列（包括 review 舉的 `ended`／`ended_at` 有值／`task_started` NULL／`outcome` NULL）。重新開啟後每一種都是 `CorruptError(host_runs)`，並指出該列。（已由第 13.14.2 節修訂：違反 CHECK／NOT NULL 形狀的列可能先被 `integrity_check` 拒絕，預期改為「結構檢查拒絕」，不指定 `host_runs`；只有通過 CHECK、但違反清單或組合規則的列才預期 `CorruptError(host_runs)`）

#### 13.9.2 R2：遷移後驗證失敗的恢復程序

**問題**：只寫「把備份換回原路徑」沒有交代原資料庫的 `-wal`／`-shm`。WAL 是 SQLite 持久狀態的一部分，只替換主檔可能讓舊 WAL 套用到備份上，或混用兩份狀態。

**修訂**：不換回原路徑，改用新路徑恢復，原檔全部保留作為證據。

1. **停止服務**：結束 demo host（Ctrl-C 或 SIGTERM），確認 `<db>.lock` 已釋放，沒有其他程序開著這個資料庫。
2. **保留失敗的資料庫**：`<db>`、`<db>-wal`、`<db>-shm`、`<db>.lock` 都不改名、不刪除、不覆寫，也不對它執行 checkpoint 或 VACUUM。
3. （新副本的共同鎖由本步驟一併準備，見第 13.13.3 節）**複製備份到全新、未使用的路徑**：例如 `var/restore-<UTC 時間>/demo.db`。目錄新建、權限 0700，目標檔與 `-wal`／`-shm` 都必須原本不存在。
   - 備份是 `VACUUM INTO` 產生的單一完整檔，沒有 WAL。
   - 用 3.2 新增的唯讀檢查命令驗證這份副本：`bin/demo --verify-db <新路徑>`。它只讀不寫：回報 schema 版本（必須是 1），並以版本 1 的規則執行 `integrity_check`、`foreign_key_check` 與既有的全部恢復檢查。不通過就停止，換用其他備份。
4. **用相容的舊版程式指向恢復副本**：以 3.2 之前的程式（例如 `b2b7daa` 建置的 `bin/demo`），設定 `APA_DB_PATH=<新路徑>` 啟動。舊路徑不再使用。

程式不自動還原、不降版，也不修改失敗的資料庫。

**3.2 檔案增補**：

- `cmd/demo/main.go`：`--verify-db PATH`，唯讀，不取資料庫鎖以外的任何寫入，結束時回報版本與結果；
- `internal/store`：唯讀驗證入口（`VerifyFile`），依檔案的 schema 版本套用版本 1 或 2 的規則。

**增補測試**：

- 「舊路徑留有 WAL」：
  - 建立版本 1 的資料庫，寫入 task 與事件，讓 `-wal` 留有未 checkpoint 的頁（開著第二條連線並停用自動 checkpoint）；
  - 以 `--upgrade-db` 升級，並用測試掛鉤讓遷移後驗證失敗；
  - 依上面的四步恢復。
- 斷言：
  - 失敗的 `<db>`、`<db>-wal`、`<db>-shm` 在整個恢復過程中位元組不變；
  - 新路徑原本不存在；`--verify-db` 回報版本 1 並通過；
  - 新路徑上的 task、事件與升級前（含當時仍在 WAL 中的寫入）相同；
  - 若新路徑已存在檔案，或已存在 `-wal`／`-shm`，恢復步驟拒絕複製。
- 用舊版程式開啟恢復副本是手動步驟，列在操作文件中；自動測試以版本 1 規則的 `--verify-db` 代替，並明寫兩者不同。

#### 13.9.3 驗收條件與需要 Aaron 決定的事項

驗收條件增補（接在第 13.8.5 節的 8–11 之後）：

12. `host_runs` 每一種狀態的必填與留空欄位、合法代碼與組合規則，在資料表約束、寫入與恢復驗證三處都檢查；不完整或矛盾的列無法寫入，也無法通過開檔驗證。
13. 遷移後驗證失敗時，失敗的資料庫與其 WAL、SHM 都不被修改；恢復一律使用新的路徑，先通過唯讀驗證，再由相容程式開啟。

需要 Aaron 決定：與第 13.8.6 節相同（既有展示資料庫是否升級，預設不升級），沒有新增事項。

### 13.10 `detail` 規則修正（依 Codex 對 `795cc30` 的 review；設計提案，待 Codex 限定 review）

review 結論：原本的 R1、R2 缺口可關閉。13.9.1 新增的 `detail` 格式 `^[a-z0-9_:]{0,64}$` 只收小寫，但 `cmd/demo/fakeruns.go` 會產生 `prepare_failed:` 加上大寫 Store 代碼（例如 `prepare_failed:WALLET_BINDING_CHANGED`，現有測試也用到）。照原規則，這類合法的失敗結果會被 `EndRun` 拒絕，留下未完成的場次紀錄。

**修正：`detail` 改為「固定來源＋清單」，不只看字元格式。** `ended` 列的 `detail` 必須是下列其中一種，長度不超過 64：

| 形式 | 來源 | 內容 |
|---|---|---|
| 空字串 | — | 沒有補充代碼 |
| host 的固定代碼 | `cmd/demo` | `host_error`、`scratch_home_failed`、`task_reserved_by_run`（3.2 新增） |
| buyer 的固定代碼 | `buyer.Report.Detail` | `ambiguous_json`、`assistant_messages`、`input_failed`、`input_partial`、`hook_message`、`init_duplicate`、`init_malformed`、`unexpected_message`、`prepare_failed` |
| `prepare_failed:<代碼>` | `fakeruns.go` 用 `store.Code(err)` 組成 | `<代碼>` 必須是 Store 或契約的固定錯誤代碼（`*store.Error` 或 `contracts` 的代碼清單）。`store.Code` 回空字串時（沒有代碼的錯誤）寫成 `prepare_failed:STORE_ERROR`，原文只留在 host 的 log |
| `detail_unlisted` | host 正規化（已由第 13.11.1 節修訂：在 buyer 來源處正規化，原代碼不進 log） | buyer 回了不在清單內的代碼時，host 改記這個固定值，並在 log 記下原代碼。不把未列名的字串存進資料表 |

規則：

- **不接受任意來源的文字**：
  - 寫入時，host 只會把上表的來源填進 `detail`，不會放 CLI 輸出、錯誤訊息原文或 Store 錯誤的 `Detail` 欄位；
  - `validRunRow` 依清單比對，不是只檢查字元。
  - 例如 `prepare_failed:` 後面接的字串即使全是大寫字母，只要不在代碼清單內就拒絕。
- 這三份清單（host、buyer、Store／契約代碼）都是 Go 常數，寫入與恢復驗證共用；`outcome` 清單也一樣（13.9.1）。
- **清單與程式同步**：一個測試掃描 `internal/buyer` 與 `cmd/demo` 中產生 detail 的位置，以及 `internal/store`、`internal/contracts` 中的錯誤代碼。程式新增代碼而清單沒更新，測試就失敗。這樣正規化成 `detail_unlisted` 只是保險，不會成為常態。
- 其他欄位要求與組合規則不變。

**測試計畫增補**（`runs_test.go`、`cmd/demo` 整合測試）：

- **合法結果能保存並讀回**：
  - `EndRun` 寫入 `prepare_failed:WALLET_BINDING_CHANGED`、`prepare_failed:STORE_ERROR`、`input_partial`、`assistant_messages`、空字串、`detail_unlisted`，都成功；
  - 關閉後重新開啟（含恢復驗證），`Run` 讀回的值相同。
  - 整合測試：沿用現有的錢包綁定變更情境，讓 3.2 的場次以 `prepare_failed:WALLET_BINDING_CHANGED` 結束，重啟 host 後場次為 `ended`，`detail` 相同。
- **非法內容仍被拒絕**：
  - 寫入路徑：`EndRun` 傳入下列任一種，都回 `INVALID_RUN_RECORD`，資料表不變：
    - CLI 風格的自由文字（`Error: rate limited, retry later`）；
    - 不在清單的小寫代碼；
    - `prepare_failed:` 加上不在清單的大寫字串（`prepare_failed:NOT_A_CODE`）；
    - `prepare_failed:` 後面是空字串或小寫；
    - 含空白或換行；
    - 超過 64 字元。
  - 恢復驗證：以 `ignore_check_constraints` 直接寫入上述各種值，重新開啟時為 `CorruptError(host_runs)`。（已由第 13.14.2 節修訂：`detail` 的清單不在 CHECK 中，這些值不必略過 CHECK，直接寫入即可）
- **正規化**：buyer 回報不在清單的代碼時（測試替身），資料表存 `detail_unlisted`，log 有原代碼，場次照常 `ended`。
- **同步測試**：在清單中刪去一個 buyer 代碼，掃描測試失敗（反向驗證）。

**實作注意事項**（review 提出，納入 3.2 驗收）：

- （已由第 13.11.2 節修訂：`query_only` 不保證檔案不變，改為只讀檔案位元組並在拋棄式副本上驗證）**`--verify-db` 走獨立的唯讀入口**（`store.VerifyFile`）：只用 `query_only` 連線，不經過會遷移、建表、執行 `ExpireDue` 的 `Store.Open`；不寫入任何檔案，也不 checkpoint。測試斷言執行前後的主檔、`-wal`、`-shm` 位元組都不變。
- **恢復測試的比對基準與 WAL**：
  - 基準時間點定為「停止服務完成後」：host 程序結束、`.lock` 已釋放、所有連線關閉之後。此時記錄失敗資料庫的主檔、`-wal`、`-shm` 的位元組與大小作為基準，恢復步驟完成後再比對一次。
  - 測試要先確認殘留 WAL 真的存在，不能空過：基準時 `-wal` 必須存在且大小大於 0；且升級前寫入的一筆資料，在只讀主檔時（以不套用 WAL 的方式另行比對）不在主檔中，表示它確實還在 WAL 裡。不符合就讓測試失敗，而不是略過。
  - （已由第 13.11.3 節修訂：保留第二條連線與「連線全關後取基準」互相矛盾，改用隔離子程序與非正常退出）製造殘留 WAL 的方式：測試在升級前的寫入連線上關閉自動 checkpoint（`PRAGMA wal_autocheckpoint = 0`），並在關閉前保持另一條讀取連線開著，避免最後一條連線關閉時自動 checkpoint 清空 WAL。

**驗收條件修正**：第 12 條改為：

> `host_runs` 每一種狀態的必填與留空欄位、合法代碼（`outcome` 與 `detail` 都依固定清單，`detail` 包含 `prepare_failed:<Store 代碼>`）與組合規則，在資料表約束、寫入與恢復驗證三處都檢查。合法的失敗結果能保存並在重啟後讀回；不完整、矛盾或自由文字的值無法寫入，也無法通過開檔驗證。

需要 Aaron 決定：與第 13.8.6 節相同，沒有新增事項。

### 13.11 修正：未列名內容不改送 log；SQLite 唯讀與 WAL 測試方法（依 Codex 對 `35b1e2e` 的 review；設計提案，待 Codex 限定 review）

review 結論：`detail` 大小寫不相容已解決；R1–R4 不重開。本節只修兩組問題。

#### 13.11.1 未列名內容不改送到任何輸出

**問題**：13.10 要求把未列名的 buyer 代碼記到 log。這等於讓「不保存未知文字」的限制，在另一個輸出位置失效。同樣，13.10 說無代碼錯誤的「原文只留在 host 的 log」，也是這種情況。

已核對的事實：3.1 的 `cmd/demo/status.go` 註解寫「its text stays in the host's log」，但 `main.go` 實際只印 `store.Code(err)`，沒有代碼時印出空字串，並沒有印出原文。這個註解不正確，在 3.2 一併改正（見下方檔案清單）。

**規則**：

- 範圍：場次結果的每一個輸出位置：
  - `host_runs` 資料表；
  - buyer 的 run trace（`var/runs/*.jsonl`）；
  - demo host 的 stdout／stderr（含 `demo: fake run …` 那一行與到期錯誤那一行）；
  - HTTP 回應（status、場次）；
  - 瀏覽器畫面與證據檔。
- 這些位置**只出現固定值**：清單內的代碼、`detail_unlisted`、`prepare_failed:<清單內的 Store 代碼>`、`prepare_failed:STORE_ERROR`、`STORE_ERROR`。
- 不保存、不印出：
  - 未知字串、錯誤原文（`err.Error()`）、Store 錯誤的 `Detail` 欄位、CLI 輸出；
  - 也不記「原代碼」。要查原因，只能依時間與 task 在本機重現，不從輸出取得。

**正規化的位置**：在 buyer 產生 `Report.Detail` 的地方，也就是 trace 與回傳值之前，就比對清單：

- 不在清單 → `detail_unlisted`。所以 trace、`runOutcome`、stdout 與資料表看到的是同一個固定值。
- `cmd/demo` 再比對一次清單（第二層），不在清單同樣換成固定值。

**錯誤代碼統一**：

- `store.Code(err)` 回空字串的地方，一律寫 `STORE_ERROR`。這包括：
  - `fakeruns.go` 的 `prepare_failed:`；
  - buyer trace 的 `prepare_error`（目前寫 `error`）；
  - `main.go` 的 `demo: expiry:` 那一行（目前印空字串）；
  - `status.go` 的 `last_error`（已是 `STORE_ERROR`）。
- 以上都不附原文。

**buyer 代碼清單補齊**：13.10 的 buyer 清單缺了 `Gate.check()` 與 host 補上的代碼。完整清單：

| 來源 | 代碼 |
|---|---|
| runner 的停止原因 | `ambiguous_json`、`assistant_messages`、`input_failed`、`input_partial` |
| host 的 Watch | `hook_message`、`init_duplicate`、`unexpected_message`、`prepare_failed` |
| `Gate.check()` | `init_malformed`、`tools`、`mcp_servers`、`permission_mode`、`cli_version`、`plugins`、`skills`、`agents`（迴圈依鍵名回傳）、`auth_missing`、`auth_unconfirmed` |
| host 補上 | `init_missing` |

`passed` 只出現在 trace 的 `gate` 欄位，不是 `detail`；同步測試同時涵蓋 `gate` 欄位的固定值。

**清單同步測試**：

- 用 Go 的 `go/ast` 掃描 `internal/buyer` 與 `cmd/demo` 中：
  - `stop(…, "…")`、`stopErr{…, "…"}`；
  - Watch 與 `Gate.check()` 的 `return "…"`、`return X, "…"`；
  - 迴圈中以清單字面值回傳的鍵（`plugins`、`skills`、`agents`）；
  - `"input_"+…` 的組合，以 `InputFailed`、`InputPartial` 展開。
- 掃到但清單沒有的代碼，測試失敗。清單有、程式已不再產生的代碼也報出，避免清單過寬。
- 反向驗證：從清單刪去 `auth_unconfirmed`，測試失敗。

**合成敏感標記測試**：

標記 `APA-SECRET-MARK-7f3c`（測試專用的合成字串，不是真實憑證）以三種方式注入：

1. **buyer 未列名代碼**：測試用的 Watch 回傳 `(ConfigMismatch, "x_" + 標記)`。
2. **無代碼的建立失敗**：測試替身的 `prepare` 回傳 `errors.New("… " + 標記)`，`store.Code` 為空。
3. **無代碼的到期錯誤**：Store 測試掛鉤讓 `ExpireDue` 回傳含標記的非代碼錯誤。

斷言：

- 場次正常結束；
- `detail` 依序是 `detail_unlisted`、`prepare_failed:STORE_ERROR`；status 的 `last_error` 是 `STORE_ERROR`。

接著逐一搜尋下列輸出，**都不含標記**：

- host 的 stdout／stderr 緩衝；
- trace 目錄下所有檔案；
- 資料庫主檔與 `-wal` 的位元組；
- 所有 HTTP 回應本文（status、場次、task、events）；
- 瀏覽器測試的畫面文字與證據目錄（3.2 的瀏覽器段落使用第 1 種注入）。

反向驗證：讓 `fakeruns.go` 改回附上原文，標記測試失敗。

#### 13.11.2 `VerifyFile`：真正不碰來源檔

**問題**：

- SQLite 的 `query_only` 不等於檔案唯讀，官方說明它仍允許 checkpoint，不能據此保證主檔、`-wal`、`-shm` 不變。
- 一般的唯讀連線（`mode=ro`）仍可能寫入 `-shm` 的 wal-index。
- `immutable=1` 會忽略 WAL，驗證結果會漏掉尚未 checkpoint 的資料。

**修正**：`VerifyFile` 不讓 SQLite 開啟來源檔，只用一般檔案讀取取得位元組，驗證在拋棄式副本上進行。

1. （已由第 13.13 節修訂：只用來源既有的共同鎖，並說明合作式鎖的限制）**確認沒有程序正在使用**：取得 `<db>.lock` 的排他鎖（非阻塞），整個過程持有。取不到就拒絕：「資料庫正被使用」。
   - **（已取代、不得實作：見第 13.13.1 節。在另一個位置鎖另一個檔案，無法阻止 demo 同時使用來源）** 鎖檔不存在時，只在唯讀檢查專用的暫存目錄建立替代鎖，不在來源目錄建立任何檔案；但若同目錄有 `-wal` 且無法確認沒有寫入者，同樣拒絕。
2. **讀取前記錄**：主檔與 `-wal` 的大小、修改時間、sha256。`-shm` 是可重建的索引，不複製，但同樣記錄它的狀態。
3. **複製**：以一般讀檔把主檔與 `-wal`（若存在）複製到 0700 的新暫存目錄，檔名維持 `x.db`、`x.db-wal` 的對應。不複製 `-shm`，讓 SQLite 在副本上從 WAL 重建索引。
4. **讀取後再記錄一次**。與第 2 步任何一項不同，就丟棄副本並拒絕：「來源在讀取期間改變」。
5. **在副本上驗證**：依副本的 `user_version` 套用版本 1 或 2 的規則（`integrity_check`、`foreign_key_check`、既有恢復檢查、`host_runs` 規則）。副本可被 SQLite 正常開啟與 checkpoint，因為它是拋棄式的。
6. 刪除副本，回報版本與結果。

**不退回可寫模式**：任何一步失敗都只回報拒絕，不改用 `Store.Open`、不在來源上開任何 SQLite 連線。來源檔只被讀取，因此主檔、`-wal`、`-shm` 必然不變；測試仍逐位元組比對加以確認。

R2 的「遷移前驗證」（13.8 第 1 步）是 host 本身持有鎖、準備寫入時的正常開檔驗證，與 `--verify-db` 不同，不受這一節影響。

#### 13.11.3 殘留 WAL 測試：隔離子程序與非正常退出

**問題**：13.10 用「保留第二條連線」製造殘留 WAL，又要求「連線全部關閉後才取基準」，兩者互相矛盾。而且最後一條連線正常關閉時，仍可能 checkpoint 並清掉 WAL；只停用自動 checkpoint 擋不住這一步。

**修正**：用隔離的測試子程序產生殘留 WAL，讓它非正常退出，不經過正常關閉。

1. **準備版本 1 的資料庫**（測試輔助）：
   - 以版本 2 的 Store 正常寫入 task 與事件後關閉；
   - 在拋棄式副本上刪除 `host_runs` 並把 `user_version` 設回 1（兩版只差這張表）；
   - 再用版本 1 的規則以 `VerifyFile` 確認它是合法的版本 1 資料庫。
2. **子程序升級**：測試以自己的測試二進位重新執行（`TestMain` 依環境變數進入子程序模式），對這份資料庫執行 `--upgrade-db` 的開檔流程：
   - 遷移前驗證 → `VACUUM INTO` 備份 → 遷移交易提交（寫進 WAL）→ 遷移後驗證；
   - 遷移後驗證被測試掛鉤設為失敗。
   - 掛鉤在**關閉任何連線之前**，於 stdout 印出確認行 `UPGRADE_FAILED_READY`，然後阻塞。
3. **非正常退出**：父程序讀到確認行後，對子程序送 SIGKILL，等它結束（`Wait` 回來），確認 `.lock` 已釋放（父程序能取得排他鎖後再放開）。
4. **取基準**：此時才記錄失敗資料庫的主檔、`-wal`、`-shm` 的大小與 sha256。這就是「停止服務完成後」的基準。
5. **確認殘留 WAL 真的存在，不能空過**：
   - `-wal` 存在且大小大於 0；
   - 把**只有主檔**複製到拋棄式目錄並開啟：`user_version` 仍是 1，沒有 `host_runs`，表示遷移只在 WAL 裡；
   - 把主檔＋`-wal` 一起複製到另一個拋棄式目錄並開啟：`user_version` 是 2。
   - 任何一項不符，測試**失敗**，不略過。檢查只在副本上進行，原證據不動。
6. **依 13.9.2 恢復**：
   - 把備份複製到新路徑（目標不存在）；
   - 對新路徑執行 `VerifyFile`：版本 1，通過；
   - task 與事件和第 1 步完全相同。
7. **再比對一次基準**：失敗資料庫的主檔、`-wal`、`-shm` 的大小與 sha256 都與第 4 步相同。過程中也對失敗資料庫本身執行一次 `VerifyFile`（預期回報版本 2、遷移後驗證的同一失敗），以確認唯讀檢查不改變來源。

   > **已由第 13.12 節取代**：上一句的預期不成立。`VerifyFile` 不安裝子程序的測試掛鉤，不會重現那個注入的失敗；而且遷移本身是完整的，資料應驗證通過。改為第 13.12 節的案例 A（注入失敗、資料完整）與案例 B（資料真的不一致）。

**其他 `VerifyFile` 測試**：

- 鎖被另一程序持有 → 拒絕，來源不變；
- 讀取期間來源被改動（測試掛鉤在複製後、第二次記錄前追加一個位元組）→ 拒絕；
- 目標路徑已存在、`-wal` 或 `-shm` 已存在 → 恢復步驟拒絕複製（沿用 13.9.2）。

#### 13.11.4 驗收條件與 3.2 檔案增補

**驗收條件**：第 12 條增補，並新增第 14、15 條：

- 12（增補）：未列名代碼與錯誤原文不出現在資料表、trace、stdout／stderr、HTTP 回應或證據中，只出現固定值；以合成標記測試確認。
- 14：`VerifyFile` 只以一般讀檔取得來源位元組，在拋棄式副本上驗證；無法保證來源不變時拒絕，不退回可寫開啟；來源三個檔案逐位元組不變。（鎖的規則已由第 13.13 節修訂，條文見 13.13.5）
- 15：殘留 WAL 測試以隔離子程序的非正常退出產生 WAL，在程序結束後取基準，並斷言 WAL 存在且含尚未進入主檔的遷移；不符合時測試失敗。（重新驗證的預期已由第 13.12 節修訂）

**3.2 檔案增補**：

- `internal/buyer`：`detail` 清單與在產生處正規化，trace 的 `prepare_error` 改用 `STORE_ERROR`；
- `cmd/demo/fakeruns.go`：第二層清單比對，`prepare_failed:STORE_ERROR`；
- `cmd/demo/main.go`：`demo: expiry:` 那一行用 `STORE_ERROR`；
- `cmd/demo/status.go`：改正註解（3.1 的註解寫「原文留在 log」，與實際不符）；
- `internal/store`：`VerifyFile` 的位元組複製流程與鎖，子程序測試模式（測試檔）；
- `internal/buyer` 與 `cmd/demo` 的同步測試（`go/ast` 掃描）。

既有展示資料庫仍預設不升級（13.8.6），沒有新增需要 Aaron 決定的事項。

### 13.12 修訂 §13.11.3 第 7 步：注入失敗與真正不一致分成兩個案例（設計提案，待 Codex 限定 review）

**問題**：§13.11.3 第 7 步要求對失敗資料庫重新執行 `VerifyFile`，並「回報遷移後驗證的同一失敗」。但那個失敗是子程序的測試掛鉤注入的；`VerifyFile` 不安裝這個掛鉤。而且遷移本身完整寫入了 WAL，持久資料其實是一致的。所以重新驗證應該**通過**，原來的預期不成立。

**修訂**：分成兩個案例，各用自己的資料，互不修改對方的證據。

#### 案例 A：注入執行失敗，持久資料仍完整

1. 沿用 §13.11.3 第 1–5 步：
   - 準備版本 1 的資料庫；
   - 子程序以 `--upgrade-db` 升級，遷移交易提交；
   - 遷移後驗證被**測試掛鉤**設為失敗，子程序印出確認行 `UPGRADE_FAILED_READY` 後阻塞；
   - 父程序 SIGKILL，等它結束、`.lock` 已釋放；
   - 取基準：主檔、`-wal`、`-shm` 的大小與 sha256。
2. **殘留 WAL 的有效性斷言**（不得略過）：
   - `-wal` 存在且大小大於 0；
   - 只有主檔的拋棄式副本仍是 `user_version = 1`、沒有 `host_runs`；
   - 主檔＋`-wal` 的拋棄式副本是 `user_version = 2`。
   - 任何一項不成立，測試**失敗**。
3. **重新驗證**：以**沒有安裝失敗掛鉤**的 `VerifyFile` 檢查失敗資料庫的路徑（§13.11.2 的流程：取鎖、以讀檔複製主檔與 `-wal`、在拋棄式副本上驗證）。
4. **預期**：
   - 回報 schema 2；
   - `integrity_check`、`foreign_key_check`、既有恢復檢查與 `host_runs` 規則全部通過（遷移只新增一張空表）；
   - 來源主檔、`-wal`、`-shm` 的大小與 sha256 與第 1 步的基準相同。
5. 備份恢復到新路徑的步驟（§13.11.3 第 6 步）照舊；完成後再比對一次基準，仍相同。

案例 A 證明的是：被注入的失敗只發生在執行流程，持久資料完整；唯讀檢查能讀到 WAL 中的遷移結果，而且不改變來源。

#### 案例 B：持久資料真的不一致

> **已由第 13.14.2 節修訂**：下面以 NULL 欄位作為反例，但違反 NOT NULL／CHECK 的列可能先被 `integrity_check` 拒絕（check 名為 `integrity`），無法保證命中 `host_runs` 檢查。改用結構合法、只有 `outcome` 不在清單內的反例。原文保留為歷史。

1. **另一份拋棄式資料**，與案例 A 的檔案分開，不修改案例 A 的任何檔案：
   - 依案例 A 的第 1 步準備，但子程序的遷移後驗證**不注入失敗**；
   - 子程序在提交遷移後，用測試連線開啟 `PRAGMA ignore_check_constraints = ON`，寫入一筆明確違反恢復規則的 `host_runs` 列：`state = 'ended'`、`ended_at` 有值、`task_started` 為 NULL、`outcome` 為 NULL（§13.9.1 表格的反例）。
   - 子程序印出確認行 `INCONSISTENT_ROW_READY` 後阻塞；父程序 SIGKILL，等它結束、鎖已釋放，再取基準。
2. **殘留 WAL 的有效性斷言**（不得略過）：
   - `-wal` 存在且大小大於 0；
   - 只有主檔的拋棄式副本沒有那一筆不一致的列；
   - 主檔＋`-wal` 的拋棄式副本有那一筆列。
   - 任何一項不成立，測試**失敗**。
3. **重新驗證**：不安裝任何失敗掛鉤，以正式的 `VerifyFile` 檢查這份資料庫。
4. **預期**：
   - 拒絕；
   - 錯誤是 `CorruptError`，check 為 `host_runs`；
   - 指出的是那一筆列（`run_id`）與違反的規則（`ended` 的 `task_started` 必填）；
   - 來源主檔、`-wal`、`-shm` 的大小與 sha256 與第 1 步的基準相同。

案例 B 證明的是：拒絕來自正式的驗證規則，而不是測試掛鉤；違規的資料只在 WAL 中時，唯讀檢查也看得到；而且拒絕時同樣不改變來源。

#### 驗收條件與實作注意

第 15 條改為：

> 15：殘留 WAL 測試以隔離子程序的非正常退出產生 WAL，在程序結束後取基準；兩個案例都斷言 WAL 存在，且含有尚未進入主檔的內容，不符合時測試失敗。
> - 案例 A：注入的遷移後驗證失敗，持久資料完整；重新驗證時不安裝掛鉤，`VerifyFile` 回報 schema 2 並通過，來源不變。
> - 案例 B：另一份資料真的違反恢復規則；正式的 `VerifyFile` 以 `CorruptError(host_runs)` 拒絕並指出該列（案例 B 的輸入與前置確認已由第 13.14.2 節修訂），來源不變。

**實作注意**：

- 失敗掛鉤只存在於子程序模式的測試程式中，正式的 `VerifyFile` 沒有任何掛鉤參數；
- 兩個案例的暫存目錄、資料庫路徑與基準各自獨立；
- 案例 B 的反例沿用 §13.9.1 的規則，不新增規則。

其他設計（R1–R4、`detail` 清單、全輸出只用固定值、SQLite 架構、Policy）不變。既有展示資料庫預設不升級（13.8.6），沒有新增需要 Aaron 決定的事項。

### 13.13 VerifyFile 鎖規則（依 vault 筆記 `Claude-Code-B1-06-VerifyFile鎖規則補充指示-6ddee79-20261005`；設計提案，待 Codex 限定 review）

**歷程**：上一輪轉貼的指示被截斷，只收到 §13.11.3 第 7 步一項，因此只完成第 13.12 節（`6ddee79`）。本節補上當時遺漏的鎖規則。案例 A／B 不重做，只確認它們使用的檔案具備正確的共同鎖（13.13.4）。

#### 13.13.1 移除暫存替代鎖

§13.11.2 第 1 步原本允許：來源沒有 `<db>.lock` 時，在暫存目錄建立替代鎖。這**已取代、不得實作**。demo host 使用的是來源旁邊的 `<db>.lock`（`cmd/demo` 的 `lockDB`）；在別處鎖另一個檔案，無法阻止 demo 同時使用來源資料庫。現行流程不得因暫存替代鎖而放行。

#### 13.13.2 一般來源的驗證流程（取代 §13.11.2 第 1 步）

1. **操作者先停止**所有可能寫入該資料庫的程序與工具（demo host、`apa-buyer` 等）。
2. `VerifyFile` 開啟來源**既有的** `<db>.lock`，取得非阻塞排他鎖（`flock`，與 demo host 相同）。
3. **拒絕**：
   - 鎖檔不存在、無法開啟、或取不到鎖，都拒絕驗證。
   - `VerifyFile` 不在來源目錄補建鎖檔，也不改用其他位置的鎖。
   - 開啟鎖檔使用不建立檔案的方式（沒有 `O_CREATE`），所以鎖檔不存在時來源目錄不會多出任何檔案。
4. **持鎖範圍**：至少持有到下列全部完成才放開——
   - 讀取前紀錄（主檔、`-wal`、`-shm` 各自是否存在、大小、修改時間、sha256）；
   - 以一般讀檔複製主檔與 `-wal`；
   - 讀取後紀錄與比對。
5. **一致性檢查**：讀取前後的紀錄任何一項不同（包括檔案出現或消失），就拒絕，不驗證、也不採用這份副本。
6. SQLite 仍只開啟拋棄式副本，不開啟來源；任何失敗都不退回一般的 `Store.Open`（沿用 §13.11.2）。

前後雜湊比對是**額外**的檢查，不是共同鎖的替代品。「沒有偵測到變動」**不等於**「已排除所有並行寫入」。

#### 13.13.3 恢復副本的鎖由建立副本的步驟準備

§13.9.2 第 3 步建立全新的私有目錄與資料庫副本時，**同一步**一併建立新副本自己的共同鎖檔 `<新路徑>.lock`（空檔、權限 0600），之後才對新路徑執行 `bin/demo --verify-db`。

- 這是「建立恢復副本」的一部分，不是 `VerifyFile` 替任意來源補建鎖；`VerifyFile` 本身仍遵守 13.13.2，不建立任何檔案。
- 新路徑、0700 的私有目錄、目標不得已存在（主檔、`-wal`、`-shm`、`.lock` 任何一個已存在就拒絕，不覆寫）都沿用既有設計。
- 失敗資料庫的主檔、`-wal`、`-shm` 與原鎖檔都不修改。
- 新副本的共同鎖若已被持有（例如有人已用它啟動 demo），`VerifyFile` 照樣拒絕，不因為它是恢復副本而略過。

操作說明中的指令範例：**已移除**（依 Codex 對 `67f63e3` 的 review，見第 13.14.1 節）。

原範例逐行執行 `mkdir`、`cp`、建立鎖檔與驗證，但沒有在每一步失敗時中止。目錄已存在時，後續命令仍可能覆寫既有的資料庫或截斷鎖檔，與本節「目標已存在就拒絕、不覆寫」互相矛盾。

3.2 實作後，才提供經測試驗證的操作命令。在那之前，恢復步驟以第 13.14.1 節的文字為準。

#### 13.13.4 合作式鎖的限制

> `<db>.lock` 是合作式鎖，只約束使用同一路徑、同一鎖檔的程序。取得鎖不能證明沒有任何其他 SQLite 寫入者。驗證限於已停止寫入的離線來源；執行前必須停止其他可能寫入該來源的工具。

這句話放進操作說明與 `--verify-db` 的說明文字。本設計不承諾排除不遵守這個鎖的程序，也不為此擴大成新的全專案鎖架構、程序掃描工具或跨程序管理功能。

**案例 A／B 的鎖**（只確認，不重做）：

- 兩個案例的失敗資料庫都由子程序以 demo host 相同的方式開啟，`lockDB` 已在來源旁建立 `<db>.lock`；子程序被強制結束後鎖自動釋放，父程序確認可取得後才取基準（§13.11.3 第 3 步）。所以 `VerifyFile` 可使用既有共同鎖。
- 案例 A 的恢復副本依 13.13.3 建立自己的鎖檔後再驗證。

#### 13.13.5 測試計畫（本輪只寫計畫，未執行）

| # | 情境 | 預期 |
|---|---|---|
| 1 | 一般來源沒有 `<db>.lock` | 拒絕；來源目錄的檔案清單在執行前後相同（不新增鎖或其他檔案） |
| 2 | 一般來源沒有共同鎖，但暫存目錄有替代鎖 | 仍拒絕，不因替代鎖放行 |
| 3 | 另一程序（測試子程序）持有來源的共同鎖 | 拒絕；來源主檔、`-wal`、`-shm` 的大小與 sha256 不變 |
| 4 | 共同鎖可取得，來源已離線 | 沿用副本驗證流程並得出結果；來源三個檔案不變；結束後鎖已釋放 |
| 5 | 依 13.13.3 建立新副本與其共同鎖 | `VerifyFile` 完成驗證（版本 1、通過）；原失敗資料庫的三個檔案與原鎖檔不變 |
| 6 | 新副本的共同鎖被另一程序持有 | 拒絕 |
| 7 | 已取得合作式鎖，但不遵守鎖的寫入者在複製後、第二次紀錄前改動來源（測試掛鉤追加一個位元組，沿用 §13.11.3 的注入測試） | 拒絕採用副本。這項測試只證明「被偵測到的改動會被拒絕」，不代表能偵測所有外部寫入 |
| 8 | 恢復步驟的目標中已有 `.lock`（或主檔、`-wal`、`-shm`） | 建立副本的步驟拒絕，不覆寫 |

**驗收條件 14 改為**：

> 14：`VerifyFile` 只使用來源既有的 `<db>.lock` 取得非阻塞排他鎖。鎖檔不存在、無法開啟或取不到鎖時拒絕，不建立鎖檔、不改用其他位置的鎖。持鎖期間記錄、複製與比對來源；只以一般讀檔取得位元組，在拋棄式副本上驗證；偵測到改變或任何失敗時拒絕，不退回可寫開啟；來源三個檔案逐位元組不變。
> - 恢復副本的鎖檔由建立副本的步驟準備。
> - 文件寫明合作式鎖的限制。

其他設計（R1–R4、案例 A／B、`detail` 清單、全輸出固定值、SQLite 架構、Policy）不變。既有展示資料庫預設不升級（13.8.6），沒有新增需要 Aaron 決定的事項。

### 13.14 恢復步驟與案例 B 修訂（依 vault 筆記 `Claude-Code-B1-06-恢復範例與案例B修訂指示-67f63e3-20261005`；設計提案，待 Codex 限定 review）

review 結論：§13.13 的共同鎖規則已接受。本節只修兩處，不重做鎖規則與案例 A。

#### 13.14.1 恢復副本的建立步驟（取代 §13.13.3 已移除的命令範例）

原本的 shell 範例沒有在每一步失敗時中止，已整段移除，不保留可直接複製執行的命令。3.2 實作後才提供經測試驗證的操作命令。

**步驟**（依序執行，任何一步失敗就**立即停止**，不做後面的步驟，也不啟動服務）：

1. **建立全新的私有目錄**（權限 0700）。目錄已存在或建立失敗 → 停止，不沿用既有目錄。
2. **檢查目標**：新路徑的主檔、`-wal`、`-shm`、`.lock` 任一已存在 → 拒絕，不覆寫、不截斷。
3. **複製備份**到新路徑的主檔，並設權限 0600。複製或設權限失敗 → 停止。
4. **建立新副本的共同鎖** `<新路徑>.lock`：以「必須是新檔」的方式建立（檔案已存在就失敗，不截斷），權限 0600。失敗 → 停止。
5. **只有第 1–4 步全部成功**，才對新路徑執行 `VerifyFile`（§13.13.2 的流程）。
6. `VerifyFile` 通過後，才可用相容的舊版程式把 `APA_DB_PATH` 指向新路徑（§13.9.2 第 4 步）。

- 整個過程中，原失敗資料庫的主檔、`-wal`、`-shm` 與原鎖檔都不修改。
- 部分完成的新副本（例如已複製、但鎖檔建立失敗）**不得**顯示或回報為恢復成功；它留在原處作為未完成的紀錄，由操作者判斷是否刪除。

**測試計畫**（本輪只寫計畫，未執行）：恢復步驟在測試中以一個函式實作（3.2），以測試掛鉤注入失敗。

| # | 情境 | 預期 |
|---|---|---|
| R-1 | 目標目錄已存在（時間戳碰撞） | 停止；`VerifyFile` 未被呼叫；既有目錄內所有檔案的內容與 sha256 不變 |
| R-2 | 備份複製失敗（掛鉤讓複製回錯誤） | 停止；`VerifyFile` 未被呼叫；不回報成功 |
| R-3 | 鎖檔建立失敗（掛鉤，或目標已有 `.lock`） | 停止；`VerifyFile` 未被呼叫；預先存在的 `.lock` 內容不變（沒有被截斷） |
| R-4 | 目標已有主檔、`-wal` 或 `-shm` | 拒絕；這些檔案的內容不變 |
| R-5 | 全部成功 | `VerifyFile` 被呼叫一次並通過；原失敗資料庫的三個檔案與原鎖檔不變 |

「`VerifyFile` 未被呼叫」以測試替身計數確認。

#### 13.14.2 案例 B：改用結構合法、業務代碼不合法的反例

**問題**：§13.12 案例 B 用 `ignore_check_constraints` 寫入 `ended` 但 `task_started`、`outcome` 為 NULL 的列，卻要求預期是 `CorruptError(host_runs)`。

- 現行 `internal/store/verify.go` 先執行 `PRAGMA integrity_check`；失敗就回 check 名為 `integrity` 的 `CorruptError`，不會繼續到後面的業務一致性檢查。
- SQLite 官方說明 `integrity_check` 也會檢查 CHECK 與 NOT NULL 的違反。
- 所以這個輸入可能先被結構檢查拒絕，無法證明命中了 `host_runs` 檢查。

**修訂後的案例 B**（取代 §13.12 案例 B 的第 1–4 步；案例 A 不變）：

1. **案例 B 自己的拋棄式資料**：依案例 A 的第 1 步準備版本 1 的資料庫，子程序以 `--upgrade-db` 升級，遷移後驗證**不注入失敗**。接著子程序以 3.2 的正式路徑完成一場合法的場次：
   - 受理（T1），task 由 `StartTask` 建立，事件正常；
   - 以 `EndRun`（T4）寫成 `ended`，`outcome = ok`，其餘欄位、綁定、task 與事件全部一致。
2. **只改一個值**：子程序以測試連線把這一列的 `outcome` 更新為清單外的固定測試值 `review_invalid_outcome`。
   - `outcome` 的清單不在 SQLite 的 CHECK 中（§13.9.1），這個值符合 CHECK 與 NOT NULL，所以**不需要** `ignore_check_constraints`。
   - 子程序印出確認行 `INVALID_OUTCOME_READY` 後阻塞；父程序 SIGKILL，等它結束、確認鎖已釋放，再取基準。
3. **殘留 WAL 的有效性斷言**（不得略過，條件不成立就失敗）：
   - `-wal` 存在且大小大於 0；
   - 只有主檔的拋棄式副本**沒有** `review_invalid_outcome`；
   - 主檔＋`-wal` 的拋棄式副本**有** `review_invalid_outcome`。
4. **前置確認**：在主檔＋`-wal` 的拋棄式副本上執行 `PRAGMA integrity_check` 與 `PRAGMA foreign_key_check`，必須通過。這確保拒絕不是來自結構檢查。
5. **正式驗證**：以沒有任何失敗掛鉤的 `VerifyFile` 驗證來源。
6. **預期**：
   - `CorruptError`，check 為 `host_runs`；
   - 指出該 `run_id` 與不合法的 `outcome`；
   - 來源主檔、`-wal`、`-shm` 的大小與 sha256 與第 2 步的基準相同；
   - 案例 A 的資料與證據不動。

**不為了命中預期而改動驗證**：不關閉正式的 `integrity_check`、不重排既有驗證順序、不擴大修改 Store。

**NULL 等形狀違反的反例**：保留在形狀／結構的拒絕測試中，預期是「被拒絕、不開始服務」，check 為 `integrity`；不要求它命中 `host_runs`。§13.9.1 與 §13.10 中相關的現行測試文字已加註同樣的修訂：

- 違反 CHECK 或 NOT NULL 的列 → 預期結構檢查拒絕；
- 通過 CHECK、但違反清單或組合規則的列 → 直接寫入，不必略過 CHECK，預期 `CorruptError(host_runs)`。

**驗收條件 15 中案例 B 的條文改為**：

> - 案例 B：另一份拋棄式資料中一筆結構合法、只有 `outcome` 不在清單內的 `ended` 列，只存在於 WAL；前置確認 `integrity_check` 與 `foreign_key_check` 通過；正式的 `VerifyFile` 以 `CorruptError(host_runs)` 拒絕並指出該列與不合法的 `outcome`；來源不變。

其他設計（R1–R4、鎖規則、案例 A、`detail` 清單、全輸出固定值、SQLite 架構、Policy）不變。既有展示資料庫預設不升級（13.8.6），沒有新增需要 Aaron 決定的事項。

### 13.15 設計驗收紀錄：第三版控制與持久化設計於 `a845dcb` 通過限定設計 review（2026-10-05）

依據：Codex 對 `a845dcb` 的限定設計 review，由 Aaron 於對話中轉交。

**結果**：通過，本輪沒有新增阻擋項目。Codex 確認：

- 危險的恢復命令已移除，文字流程明確要求失敗即停止、不覆寫；
- 案例 B 改用結構合法、`outcome` 不合法的資料，能對準 `host_runs` 驗證規則；
- 相關測試預期與歷史段落已加註更新。

**review 歷程**：

| 輪次 | 基準 | 結果 |
|---|---|---|
| 1 | `838de81` | SQLite 方向可採用；需補 R1–R4 |
| 2 | `a976fb9` | R3、R4 關閉；R1、R2 各補一處 |
| 3 | `795cc30` | R1、R2 關閉；新增的 `detail` 規則需相容 `prepare_failed:<Store 代碼>` |
| 4 | `35b1e2e` | 大小寫問題解決；未列名內容不得改送 log、SQLite 唯讀與殘留 WAL 測試需修正 |
| 5 | `2850c62` 後的修訂 | 指示轉貼截斷，`6ddee79` 只完成案例 A／B；遺漏的鎖規則補於 `67f63e3` |
| 6 | `67f63e3` | 共同鎖規則通過；恢復命令範例與案例 B 需修正 |
| 7 | `a845dcb` | **通過** |

**範圍說明**：

- 這是**設計**驗收，不是程式驗收。Codex 沒有執行測試或資料庫操作；本設計也還沒有任何實作。
- 3.2（主持人控制與場次持久化）的實作待 Aaron 核准；核准前不開始。
- 實作時，開發與測試使用新資料庫，不升級既有展示資料庫（13.8.6）；不啟動真實 CLI、不付款、不擴大到 3.3／3.4。

B1-04／B1-06 整體維持 InProgress；不合併 main、不標 Done。

### 13.16 切片 3.2 實作紀錄（Claude Code 執行，待 Codex 獨立 review；程式 `718aa91`，測試 `d128b79`、`28aae6e`）

依據：第 13 節（含 13.8–13.14），於 `a845dcb` 通過限定設計 review；Aaron 10/6 核准實作。

#### 13.16.1 如何操作

```sh
make demo                    # 情境目錄＋demo registry＋自己的新資料庫 var/apa-demo-host-runs.db；啟動時不跑任何場次
```

1. 開啟 `http://127.0.0.1:8402/host`，載入 `var/demo-tokens.json`（3.2 起含 operator）。
2. 「主持人控制」列出情境（ask、deny-budget）。按「開始」後：
   - 頁面自動切到該場 consumer，並開啟新的 `show-*` task；
   - 「最近場次」分開列出程序、收尾紀錄與失敗。
3. 場次進行中可按「停止」。畫面要等收尾紀錄寫入後，才顯示「已停止，取消已記錄」。
4. 既有的 `var/apa-demo-fake.db` 是 schema 1，不會被升級。要升級需另行確認（13.8.6），以 `--upgrade-db` 執行。

**資料庫的單次命令**（經 3.2 測試驗證；執行前先停止所有可能寫入該資料庫的工具）：

```sh
bin/demo --verify-db PATH                              # 唯讀檢查，取該資料庫的 <db>.lock；不改來源
bin/demo --restore-copy BACKUP --restore-dir NEWDIR    # 在新目錄建立恢復副本與其鎖，通過唯讀檢查才回報成功
```

`<db>.lock` 是合作式鎖，只約束使用同一路徑、同一鎖檔的程序；取得鎖不能證明沒有其他 SQLite 寫入者（13.13.4）。

#### 13.16.2 修改

| 範圍 | 檔案 | 內容 |
|---|---|---|
| Store | `internal/store/store.go`、`verify.go`、`runs.go`（新）、`verifyfile.go`（新） | schema 2（`host_runs`）；版本 1 預設拒絕，`--upgrade-db` 走遷移前驗證 → `VACUUM INTO` 備份 → 遷移 → 遷移後驗證；`validRun`（形狀、清單、組合）在寫入與開檔驗證共用；`AcceptRun`／`RunByRequest`（唯讀）／`EndRun`／`InterruptOpenRuns`／`GetRun`／`Runs`／`TaskReserved`；`VerifyFile`、`RestoreCopy`；`CodeOf`、`ValidDetail` 與三份代碼清單 |
| buyer | `internal/buyer/host.go` | `Report.Detail` 在產生處正規化為清單值；`EndError` 與 trace 的 `prepare_error` 用 `STORE_ERROR`；測試用 `watchHook` |
| demo host | `cmd/demo/control.go`（新）、`scenarios.go`（新）、`api.go`、`main.go`、`fakeruns.go`、`status.go` | operator；控制端點與 POST 防護；單場鎖與記憶體中的進行狀態；情境目錄與 digest；啟動時標記中斷；`--scenarios`、`--upgrade-db`、`--verify-db`、`--restore-copy`／`--restore-dir`；`--plan` 略過已保留的 task；status 的 `caller_role` 與 `runs`；到期錯誤的 log 改印固定代碼 |
| 頁面 | `web/hostcreds.js`、`host.js`、`host.html`、`app.js`、`style.css` | token 檔需恰好一個 operator，以 `caller_role` 確認角色；開始／停止／清單，都依世代規則；控制面板與第 13.5 節的文字 |
| 設定與工具 | `testdata/demo/scenarios.json`、`testdata/demo/registry.json`（新）、`scripts/demo/start.sh`、`Makefile`、`scripts/verify/mutation-check.py` | 情境目錄；契約 fixture 的超集；`make demo` 改用情境模式與新資料庫；新增 11 項突變，兩項既有突變改指向改寫後的程式（同一缺陷） |

#### 13.16.3 與設計的差異（請 review 判斷）

1. **`unexpected_message` 移出清單**：13.11.1 把它列為 host Watch 的代碼，但它只出現在 C0 init 探測（`probe.go`），不會出現在 `Host.Run`。這是設計錯誤，由同步測試抓到。
2. **status 的 `runs` 多了 `scenarios`**（id、consumer、label）。主持人頁需要它來產生按鈕；label 是可信的情境目錄文字，不是 CLI 輸出。
3. **場次完成時只寫「場次完成；task 結果見 Inspector／Timeline」**，沒有把「等待核准／Ask 已到期／Deny」放進場次清單。operator 不能讀 task，這些內容由同頁的 Dashboard（viewer token）顯示。
4. **鎖被持有的測試**：用同一個測試程序中的另一個檔案描述子持有 `flock`，而不是另一個程序。`flock` 以開啟的檔案描述為單位，效果相同。實際的另一個 demo 程序持有鎖，在手動驗證中以 `--verify-db` 確認（拒絕「in use」）。
5. **合成標記測試的注入位置**：
   - 未列名代碼（`watchHook`）與無代碼的建立失敗，在 buyer 層注入，檢查 trace 檔與回傳值；
   - 無代碼的到期錯誤，在 `expiryHealth` 單元測試注入；
   - `cmd/demo` 的 `runEnd` 正規化另有單元測試。
   - 沒有經由執行中的 demo host 或瀏覽器注入標記。
6. **備份失敗**以第 2 步之後的掛鉤代表，沒有另外製造 `VACUUM INTO` 本身失敗。恢復步驟的 R-4（目標已有檔案）因目錄必須是新建，實際上走不到；程式仍檢查。
7. **收尾寫入失敗（T3）**以時鐘倒退製造，T3 與 T4 會同時失敗；只有 T4 失敗的情況，以執行中把該列改成中斷來製造（`RUN_NOT_OPEN`）。

#### 13.16.4 測試與證據（Claude Code 執行，待 Codex 獨立 review）

| 項目 | 命令 | exit | 時間 | 證據 |
|---|---|---|---|---|
| Node（adapter 11＋hostcreds 19，其中控制請求的世代 6 項） | `node --test web/test/adapter.test.mjs web/test/hostcreds.test.mjs` | 0 | 1 秒內 | `artifacts/b1-06/checks-718aa9160b3b-20261005T161732Z/` |
| Go 全部套件 race | `go test -race -count=1 -v ./...` | 0 | 107 秒 | 同上 |
| Store：遷移、升級各失敗點、`AcceptRun`／`EndRun`、列規則（CHECK 形狀與 `host_runs` 規則分開）、綁定、registry 移除後仍可開、`VerifyFile` 的鎖、恢復步驟、殘留 WAL 案例 A／B（隔離子程序 SIGKILL） | 同上（`internal/store`） | 0 | — | 同上 |
| 代碼清單同步（`go/ast`）：buyer 的 outcome 與 detail、Store 代碼、host 代碼 | 同上 | 0 | — | 同上 |
| 控制：權限表、POST 防護、202／200／409／400／404／503、雙重並發、停止、時鐘倒退、收尾失敗、重啟後中斷、`--plan` 略過保留、名額用完、情境目錄、demo registry 超集 | 同上（`cmd/demo`） | 0 | — | 同上 |
| 真實瀏覽器 130 項（3.2 新增 22 項） | `node scripts/verify/browser-ui.mjs` | 0 | 146 秒 | `artifacts/b1-06/ui-28aae6e32602-20261005T163729Z/`（含 `desktop-host-control.png`） |
| 突變 | 11 項新增（含 `mutation-check.py`），本機先逐項確認以測試失敗被抓到；兩項既有突變改指向後確認仍被抓到 | — | — | 完整結果見容器 |
| 瀏覽器反向：拿掉開始回覆的世代檢查（三層：`hostcreds`、`start`、`openRun`） | 同上，在拋棄式 worktree | 1（預期） | 143 秒 | `artifacts/b1-06/reversal-28aae6e32602/` |
| 完整容器（Go／JS＋fake 回歸，含新突變） | `sh scripts/verify/container-verify.sh 28aae6e…` | **未執行**（Docker daemon 未啟動） | 1 秒 | `artifacts/b1-06/container-28aae6e.log`；開啟 Docker 後對 `28aae6e` 補跑一次，結果補記於此 |

Unity 沒有修改，瀏覽器載入的建置檔 sha256 與 `eda6469` 建置相同（`checks-…/webgl-loaded.sha256`）。

#### 13.16.5 失敗嘗試（保留）

- **兩項既有突變失效**：`verify()` 加了版本參數、`grant.sees` 改寫後，兩項既有突變比對不到原文字，容器的突變步驟會失敗。已改指向新程式（注入同一缺陷），並確認仍以測試失敗被抓到。
- **`unexpected_message`**：見 13.16.3 第 1 點。
- **雙擊建立兩次開始**：瀏覽器測試對同一個元素按兩下，第一下重建了按鈕，第二下打在已移除的舊按鈕上，仍觸發一次開始，送出兩個不同的 request ID。伺服器正確地只受理一場，但頁面不應送出第二次。已在 `start()` 加上進行中即返回。
- **開始結束後按鈕未立即恢復**：要等下一次清單輪詢才重繪，導致下一次點擊落在停用的按鈕上。已在開始結束時立即重繪；測試改為等按鈕可用才點擊，是明確的就緒條件。
- **場次清單檢查空過**：測試中 `runItem` 的運算式缺少括號，`x || "".includes(…)` 使三項檢查在項目出現時就成立。停止的檢查因此在場次仍「停止中」時就通過，進而讓重啟比對失敗。已修正於 `d128b79`。
- **瀏覽器反向一度沒有失敗**：拿掉開始回覆的世代檢查後，原本的「清除全部後放行」案例仍通過，因為清除後頁面不再持有任何 token。已補上「重新載入同一檔案後放行」的案例（新世代、相同 token），於 `28aae6e` 加入後反向即失敗。
- **測試本身的錯誤**：一個測試對同一 host 停止兩次（等滿 30 秒）；hostcreds 測試的假伺服器缺 operator 與 `caller_role`；`openErr` 與既有輔助函式同名。都已修正。

#### 13.16.6 未測與限制

- 取消 task（3.4）、task 清單 API（3.3）、整體操作驗收（3.5）未做。
- 中斷的場次目前只能看到狀態；要收尾需要 3.4 的「取消 task」。
- 只用 Chrome（SwiftShader）。
- 既有展示資料庫沒有升級；用舊版程式開啟恢復副本是手動步驟，未自動測試。
- 不執行真實 CLI、不付款。

### 13.17 依 Codex 對 `2162f2f` 的 review 修正 R1、R2，及容器驗證發現的兩處（Claude Code 執行，待 Codex 獨立 review；最終程式 `c0d4c96`）

review 的獨立結果：

- Codex 在斷網容器內執行 `internal/store`、`internal/buyer`、`cmd/demo` 的 race 測試與 Node 30 項，全部通過；
- 額外探測重現了下列兩項問題；
- Claude 的瀏覽器通過紀錄與反向失敗紀錄已由 Codex 核對；Codex 沒有重跑瀏覽器或完整突變套件。

| 項 | 問題 | 修正 | 回歸測試 | 反向（突變，本機先確認被抓到） |
|---|---|---|---|---|
| R1 | 未帶 `--upgrade-db` 時，Store 先以可寫的 SQLite 連線讀取版本，再拒絕 schema 1。若非正常關閉後留有 WAL，這條連線會重播 WAL，關閉時還可能 checkpoint 並刪除它，「拒絕且不改變」並不成立 | `Open` 在開啟任何連線之前判斷版本：沒有待重播的 WAL 時，讀檔頭的 `user_version`（第 60–63 位元組）；有 WAL 時，把主檔與 WAL 以位元組複製到拋棄式目錄，在副本上讀版本。schema 1 且沒有升級指示就在此拒絕，來源不被 SQLite 開啟。`migrate()` 原有的拒絕保留為第二層 | `TestRefusedSchema1KeepsWAL`：<br>• 子程序對 schema 1 檔寫入（關閉自動 checkpoint）後被 SIGKILL，留下真實 WAL，並先確認那筆寫入只在 WAL 中；<br>• 拒絕前後，目錄清單、主檔、`-wal`、`-shm`、`.lock` 的存在、大小與 sha256 都相同；<br>• 沒有 WAL 的 schema 1 檔同樣不變 | 拿掉開檔前的版本檢查 → 測試失敗 |
| R2 | `EndRun` 只檢查列本身的規則，沒有核對實際的 task 與事件。task 不存在卻寫入 `task_started=true`、task 已存在卻寫入 `task_started=false`、沒有取消事件卻寫入 `store_end=TASK_CANCELLED`，三者都回成功，重新開啟時才得到 `CorruptError(host_runs)`。這是 Store API 的完整性缺口，**尚未證明可直接從 HTTP 觸發** | 把「場次與已存 task／事件」的規則抽成 `runVsStore`，恢復驗證與 `EndRun` 共用。`EndRun` 在同一個寫入交易內呼叫它，矛盾就回 `INVALID_RUN_RECORD` 並回滾 | `TestEndRunChecksTheStore`：<br>• 三種重現情況都被拒絕，列仍是 `accepted`，重新開啟沒有錯誤；<br>• 正向對照：先記錄取消事件，同樣的結束就被接受 | 拿掉 `EndRun` 中的核對 → 測試失敗 |

突變共 235 項（新增 2 項）。

**容器驗證發現的兩處**（都已修正，失敗紀錄保留）：

| 容器執行 | 結果 | 原因 | 修正 |
|---|---|---|---|
| `7869964` | overall exit 1：只有突變步驟失敗，234／235 被抓到，一項存活（「schema 1 未帶 `--upgrade-db` 就升級」）；其他六步通過 | R1 讓 `Open` 在 `migrate()` 之前就拒絕 schema 1，`migrate()` 原有的拒絕成為第二層，一般路徑已走不到，所以拿掉它沒有測試會失敗 | 第二層保留，另加 `TestMigrateRefusesSchema1` 直接驗證它；該突變改為同時指名兩個測試（`c5bf8c2`） |
| `c5bf8c2` | overall exit 1：只有 race-cover 失敗，`TestControlRuns` 看到 `live: ended`、`state: accepted`、沒有 `record_error`；235／235 突變全數被抓到，其他步驟通過 | **場次檢視的競態**：讀取路徑先讀資料表列（還是 `accepted`），之後 `runOne` 寫入結束並更新記憶體，讀取路徑才在鎖內組回覆，把舊的列和新的記憶體狀態混在一起。畫面上會短暫顯示「已受理」，而不是結束狀態 | `runOne` 在 `c.mu` 內寫入結束並更新記憶體；所有組成場次檢視的讀取都在 `c.mu` 內讀資料表列。鎖的順序仍是先 `c.mu` 再 SQLite（`start` 原本就如此），不會死鎖。`TestRunViewConsistent` 在 12 場內緊密輪詢；用舊程式跑 3 次，每次都抓到混合的檢視（`c0d4c96`） |

這個競態只能以機率方式偵測，回歸測試無法證明它不存在。

**命令與結果**（R1、R2 修正後的 `7869964`；最終 `c0d4c96` 的結果在本節末）：

| 命令 | exit | 時間 | 證據 |
|---|---|---|---|
| `node --test web/test/adapter.test.mjs web/test/hostcreds.test.mjs`（30 項） | 0 | 1 秒內 | `artifacts/b1-06/checks-78699641b042-20261005T171719Z/` |
| `go test -race -count=1 -v ./...` 第一次 | **1** | 131 秒 | 同上 `go-test-race.log`：`cmd/apa-buyer`、`cmd/demo`、`internal/buyer` 失敗，都是 fake CLI 的 `--version` 逾時（`TestApprovedVersionInit`、`TestIntegrationStartRefusals`、`TestVersion`、`TestProbeOneMessageStops`）；其他 7 個套件通過，包括本輪唯一修改的 `internal/store` |
| 同上，只重跑那三個套件一次 | 0 | 113 秒 | 同上 `go-test-race-rerun-3pkgs.log` |
| `node scripts/verify/browser-ui.mjs`（130 項） | 0 | 138 秒 | `artifacts/b1-06/ui-78699641b042-20261005T171931Z/` |
| 完整容器 | 1（見上：突變一項存活） | 621 秒 | `artifacts/verify-container/runs/78699641b042-20261005T172421Z/`（`artifacts/b1-06/container-7869964.log`） |

- 第一次 race 失敗時，主機的 load average 是 13–20。那三個套件在這次 commit 沒有修改，修正前同樣的全套也通過，推論是主機負載造成的時序失敗，與既有的「時序測試偶發失敗」非阻擋待辦同類，原因未獨立證實。失敗紀錄保留，只重跑一次。
- Unity 未修改，建置雜湊相同。

**最終程式 `c0d4c96` 的結果**：

| 命令 | exit | 時間 | 證據 |
|---|---|---|---|
| `node --test …`（30 項） | 0 | 1 秒內 | `artifacts/b1-06/checks-c0d4c96eb42a-20261005T175458Z/` |
| `go test -race -count=1 -v ./...`（10 個套件） | 0 | 110 秒 | 同上 |
| `node scripts/verify/browser-ui.mjs`（130 項） | 0 | 142 秒 | `artifacts/b1-06/ui-c0d4c96eb42a-20261005T175648Z/` |
| 完整容器（Go／JS＋fake 回歸；主機無其他負載） | 0 | 693 秒 | `artifacts/verify-container/runs/c0d4c96eb42a-20261005T175916Z/`（`artifacts/b1-06/container-c0d4c96.log`）：七步全過（check-contracts 19、check 85、race-cover 185、verify-b1-01 43、mutation 344 秒），check-contracts 含 adapter 11／11 與 hostcreds 19／19，235／235 突變，17 項隔離 ok |

另一次容器紀錄：`c5bf8c2` 的執行在 `artifacts/verify-container/runs/c5bf8c294ecc-20261005T173542Z/`（`artifacts/b1-06/container-c5bf8c2.log`）。

### 13.18 `answer()` 重新讀取失敗不再沿用舊資料（依 Codex 對 `fb6fdf3` 的 review；Claude Code 執行，待 Codex 獨立 review；程式 `69ae9b2`）

**review 的證據**：

- Codex 獨立通過：Store 與 demo 全套 race、Node 30 項、R1／R2 原始探測與修正回歸各三輪、場次一致性回歸三輪。
- Claude 執行、Codex 核對：完整容器七步、235／235 突變、17 項隔離、瀏覽器紀錄。
- 新的探測：`answer()` 在鎖內重新讀取，失敗時卻忽略錯誤，以第一次查詢的舊資料回 `200`（`state=accepted`、`live=ended`、沒有 `record_error`），三輪都重現。這是錯誤處理問題，不是已證明的權限繞過。

**修正**（只這一處；鎖順序、R1／R2 與既有語意不變）：

- 鎖內 `GetRun` 失敗：回 `503 store_unavailable`，代碼是固定值（沒有代碼的錯誤寫 `STORE_ERROR`），立即返回。
- 原本存在的場次重新讀取卻不存在：回 `503 store_unavailable`，代碼 `RUN_NOT_FOUND`。`host_runs` 的列不會被刪除，這代表 Store 異常；不回舊資料，也不重新建立場次。
- 只有這次讀到的列才會組成 `200` 的回應。

**回歸**：

- `TestAnswerRereadsOrFails` 確定地觸發三種情況：
  - 關閉 Store 後重新讀取失敗：`503 STORE_ERROR`，回應中沒有任何場次欄位；
  - 不存在的場次：`503 RUN_NOT_FOUND`；
  - 正向對照：拿著舊的 `accepted` 列重送，回應是最新的 `ended`。
- 新增突變「重新讀取失敗時仍用舊資料」，本機確認被抓到；突變共 236 項。

**命令與結果**（`69ae9b2`；Claude Code 執行）：

| 命令 | exit | 時間 | 證據 |
|---|---|---|---|
| `go test -race -count=1 -v ./cmd/demo ./internal/store` | 0 | 106 秒 | `artifacts/b1-06/checks-69ae9b2ff237-20261005T183015Z/` |
| `go test -race -count=3 -run TestAnswerRereadsOrFails ./cmd/demo`（3 次皆通過） | 0 | 5 秒 | 同上 `answer-x3.log` |

依 review 指示，本輪只交定向回歸。網頁與 Unity 都沒有修改，所以沒有重跑瀏覽器或 Unity。完整容器驗證沒有對 `69ae9b2` 重跑；最近一次通過的是 `c0d4c96`，它與 `69ae9b2` 只差 `control.go` 的 `answer()`、它的測試，以及一項突變。

### 13.19 驗收紀錄：切片 3.2 於 `69ae9b2` 通過限定範圍技術 review（2026-10-06）

依據：Codex 對 `69ae9b2` 的限定 review，由 Aaron 於對話中轉交。

**結論**：

- B1-06 交付點 3.2（主持人控制與場次持久化）於 `69ae9b2` 通過限定範圍技術 review；
- `answer()` 重新讀取失敗的問題關閉；
- 本次未發現新的阻擋項目。

**證據歸屬**：

| 類別 | 內容 |
|---|---|
| Codex 本輪獨立執行（斷網容器，race，各三輪，全部通過） | ① `answer()` 讀取失敗、② 場次不存在、③ 正常重新讀取（`TestAnswerRereadsOrFails` 的三種情況）；④ 上一輪會重現錯誤 200 的原始探測；以及場次控制與場次狀態一致性測試。確認讀取失敗回 503、不回舊場次，正常情況回最新紀錄 |
| Codex 先前各輪獨立執行 | Store 與 demo 全套 race、Node 30 項、R1／R2 原始探測與修正回歸各三輪、場次一致性回歸三輪（第 13.18 節） |
| Claude Code 執行、Codex 核對紀錄 | `69ae9b2` 的定向測試：`go test -race ./cmd/demo ./internal/store`、新測試 race ×3（`artifacts/b1-06/checks-69ae9b2ff237-20261005T183015Z/`）；`c0d4c96` 的完整容器七步、235／235 突變、17 項隔離；瀏覽器 130 項紀錄 |

**完整容器的歸屬**：最近一次完整容器驗證屬 `c0d4c96`，突變 235／235。`69ae9b2` 新增的第 236 項突變只在本機逐項確認被抓到，**沒有**跑過完整容器。不能寫成 `69ae9b2` 已通過完整的 236 項。

**保留**：

- 失敗嘗試：見第 13.16.5、13.17、13.18 節。
- 與設計的差異：見第 13.16.3 節。
- 未測：見第 13.16.6 節。
- 既有展示資料庫沒有被操作或升級。

**下一步**：可安排 3.3（動態 task 清單與新場次選擇），尚未核准、未開始。

B1-04／B1-06 整體維持 InProgress；不合併、不標整卡 Done。

## 14 切片 3.3：動態 task 清單與新場次選擇（Claude Code 執行，待 Codex 獨立 review；程式 `843ca4a`）

依據：第 5 節「頁面的 task 清單」與第 8 節的 3.3 列；Aaron 10/6 核准「可以安排 3.3」。3.3 在設計中只有一段簡短描述。本節是實作時具體採用的決定，一併交 review；若需調整，依 review 修改。「開始一場後自動開啟新 task」已在 3.2 完成，本切片不重做。

### 14.1 決定

| 項目 | 決定 |
|---|---|
| 端點 | `GET /api/tasks`，唯讀 |
| 誰看到什麼 | viewer：自己 consumer 的 task，依 Store **保存的** consumer 判斷（與 `ReadTask` 相同；目前 registry 已移除的 task 仍列給它自己的 consumer）。host：全部。operator：403（它不讀 task）。無 token：401。沿用 Host／Origin 檢查，URL 中的 token 無效。consumer 為空的 viewer token 一律 403，避免空字串被當成「全部」 |
| 內容 | 每筆只有 `task_id`、`consumer_id`、`status`、`deny_reason`（若有）、`started_at`，加上 `served_at`；沒有請購、事件或綁定細節 |
| 順序與數量 | `started_at` 由新到舊，同時間時以 `task_id` 由大到小；最多 50 筆，不分頁（`ponytail:` 上限；需要時再加分頁） |
| Store | `ListTasks(consumerID, limit)`：唯讀查詢 `tasks` 表，不改 schema、寫入規則、事件或契約 |
| 頁面（`/` 與 `/host`） | 移除固定的 `FIXTURE_TASKS`。viewer 驗證成功即登入；清單另有載入狀態（`#task-pick` 的 `data-state`：loading／ok／error）。清單讀取失敗時登入不失敗，手動輸入 task ID 照常可用，下一次輪詢再讀。清單旁有獨立、可見的清單狀態文字（`#list-note`），不動狀態列、Inspector、裁決或回放：成功時「清單更新於 HH:MM:SS」；已有清單後更新失敗時保留舊按鈕，顯示「更新失敗，以下為上次成功資料（HH:MM:SS）」；尚未成功讀過時顯示「清單尚未取得，可手動輸入 task ID」，不顯示「尚無 task」；恢復成功即清除錯誤，清除或切換身分時一併清空。之後每 5 秒、頁面可見時重新讀取（review 後修正：`0818db8` 見 14.5，`2c55835` 見 14.6） |
| 清單的世代 | 每次身分改變（清除、切換、`pagehide`），身分世代就加一並中止進行中的清單請求。身分改變後才到的回覆不顯示；401 沿用既有規則。同一身分內，每次請求另有序號：只有最新一次請求可以更新清單、處理錯誤與 401、安排下一次輪詢；被取代的請求不恢復舊狀態，也不新增計時器（`0818db8`，見 14.5） |
| 顯示 | `task_id（狀態）`。狀態以中文標示：已建立、執行中、等待核准、Deny、已取消、CLI 逾時；未知代碼原樣顯示。手動輸入 task ID 保留 |
| 舊場次 | 從清單開啟任何一個舊 task，沿用既有的唯讀歷史回放 |

### 14.2 測試與證據（Claude Code 執行，待 Codex 獨立 review；`843ca4a`）

| 項目 | 命令 | exit | 時間 | 證據 |
|---|---|---|---|---|
| Node 30 項（未變） | `node --test …` | 0 | 1 秒 | `artifacts/b1-06/checks-843ca4abb480-20261005T190251Z/` |
| Go 全部套件 race（含 `TestListTasks`、`TestTaskList`） | `go test -race -count=1 -v ./...` | 0 | 115 秒 | 同上 |
| 真實瀏覽器 139 項（3.3 新增 9 項） | `node scripts/verify/browser-ui.mjs` | 0 | 149 秒 | `artifacts/b1-06/ui-843ca4abb480-20261005T190448Z/` |
| 瀏覽器反向：拿掉清單的身分世代 | 同上，在拋棄式 worktree | 1（預期） | 155 秒 | `artifacts/b1-06/reversal-843ca4abb480/` |
| 完整容器（Go／JS＋fake 回歸；主機無其他負載） | `sh scripts/verify/container-verify.sh 843ca4a…` | 0 | 629 秒 | `artifacts/verify-container/runs/843ca4abb480-20261005T191312Z/`（`artifacts/b1-06/container-843ca4a.log`）：七步全過（check-contracts 14、check 74、race-cover 174、verify-b1-01 41、mutation 313 秒），adapter 11／11、hostcreds 19／19，238／238 突變，17 項隔離 ok |

**測試內容**：

- `TestListTasks`（Store）：
  - 依 consumer 篩選；
  - 由新到舊；
  - 數量上限；
  - 未知 consumer 回空清單；
  - 讀取不寫入。
- `TestTaskList`（HTTP）：
  - viewer A／B 各只看到自己的 task；host 看到全部；operator 403；無 token 401；URL 中的 token 401；外站 Origin 403；
  - 列出 20 次後，事件不變；
  - 重啟後新 task 排在前面；
  - registry 移除 `task-a-next` 後，它仍列給 consumer-a，不列給 B。
- 瀏覽器（3.2 段落的重啟之後）：
  - viewer A 的清單是 A 已保存的 `show-a-05…01`，由新到舊，沒有 B 的 task；
  - 伺服器端新開一場後，已開著的頁面不必重新整理，就在清單看到 `show-a-06`；
  - 從清單開啟較舊的 `show-a-01` 並回放，回放後清單仍在，事件沒有被寫入；
  - viewer A 的清單回覆被扣住，切換到 B 之後才放行：B 的清單與身分不變；
  - viewer B 的清單只有 `show-b-01`；
  - 頁面不再出現「demo fixture」字樣。
- 新增突變 2 項（共 238），本機先逐項確認被抓到：
  - 清單 SQL 拿掉 consumer 篩選；
  - operator 可讀清單。

**反向驗證**：

- 拿掉清單的身分世代檢查、但保留 `forget()` 中止請求時，反向仍是 139 PASS：被扣住的請求在切換時已被真的中止，這一層就擋住了舊回覆。
- 一併拿掉中止後，「A 的晚到回覆」才以斷言失敗，B 的清單被換成 A 的。和 3.1、3.2 一樣，這裡有兩層防護。

### 14.3 失敗嘗試（保留）

- **交付點 2 的路由測試**：它把 `/api/tasks` 列為「不應提供」的路徑。3.3 依核准範圍新增這個端點，所以從清單移除，並加註由 `TestTaskList` 涵蓋。
- **測試本身的錯誤**：
  - HTTP 測試的解碼結構缺 JSON 標籤，第一次讀不到任何 task；
  - 瀏覽器的 3.3 段落第一次放在 3.2 重啟比對之前，它自己開的新場次讓比對失敗，已移到重啟之後。
- **第一次反向沒有失敗**：見 14.2 的反向驗證。

### 14.4 未做

- 3.4（取消 task、錯誤文字補齊）、3.5（整體操作驗收）。
- 清單不分頁，最多 50 筆。
- 只用 Chrome。

### 14.5 review 後修正：同一身分的清單請求序號、清單失敗的降級（Claude Code 執行，待 Codex 獨立 review；程式 `0818db8`）

> **review 狀態（10/6，Codex 對 `0818db8` 的 review）**：下方問題一（同一身分的清單請求序號，Codex 稱 R2）已關閉。新的 R1——已有清單後更新失敗時，畫面沒有可見提示，只改了 `data-state`；本節「失敗時若已有清單就保留」的做法正是原因——由 14.6 修正（`2c55835`），尚待這次修正的驗收。本節以下保留原紀錄，不改寫。

依據：Codex 對 `843ca4a` 的 review。
- 問題一：`web/app.js` 的 `refreshList` 只有身分世代，同一身分的不同清單請求沒有區分。被取代或被中止的請求仍會安排計時器；回覆的 JSON 晚到時，舊的「等待核准」可能蓋掉新的「已取消」。這個問題由 Codex 以模組層級的替身探針重現，不是真實瀏覽器，也不是跨 consumer 的繞過。
- 問題二：14.1 原寫「讀到清單才完成登入」，但實作允許清單失敗後手動輸入 task。

**修正**（只動 `web/app.js`）：
- `refreshList` 每次呼叫取得新的 `listSeq`。`current()` 同時比對身分世代與序號。
  - 在 `fetch` 回來後、401 處理前、JSON 解析後、錯誤處理前、安排計時器前，都先確認 `current()`。
  - 不是目前請求就直接返回，不顯示、不改狀態、不設計時器。
  - 既有的身分切換、清除、`pagehide` 保護不變。
- 清單狀態以 `#task-pick` 的 `data-state` 表示（loading／ok／error），`forget()` 清除。
  - 失敗時，若畫面已有清單就保留；沒有清單才顯示「task 清單讀取失敗；可直接輸入 task ID」。
- 14.1 的文字已改為「viewer 驗證成功即登入；清單另有載入狀態」。

**測試與證據**：
- 依 review 指示，Go、Unity 與驗證工具未變，所以不重建 Unity，也不重跑整套容器與突變。
- 瀏覽器測試檔 `browser-ui.mjs` 只新增檢查。

| 項目 | 命令 | exit | 時間 | 證據 |
|---|---|---|---|---|
| Node 30 項（未變）＋語法檢查 | `node --test web/test/adapter.test.mjs web/test/hostcreds.test.mjs`；`node --check` | 0 | 1 秒內 | `artifacts/b1-06/checks-0818db89a1e1-20261005T195533Z/` |
| 真實瀏覽器 146 項（本次新增 7 項） | `node scripts/verify/browser-ui.mjs` | 0 | 約 180 秒（目錄時間到日誌最後修改） | `artifacts/b1-06/ui-0818db89a1e1-20261005T195224Z/` |
| 瀏覽器反向：`current()` 拿掉序號、只留身分世代 | 同上，在提交前的工作區暫改後還原（當時 HEAD 為 `09ad6ca`，所以執行目錄名為 `ui-09ad6cae94e0-20261005T194739Z`；除這一行外，程式與測試和 `0818db8` 相同） | 1（預期，145 PASS／1 FAIL） | 未計時 | `artifacts/b1-06/reversal-0818db89a1e1/`（差異與輸出） |

**新增的瀏覽器檢查**：在 3.3 段落、`show-a-01` 回放之後，在頁面中包裝 `fetch`，計算清單請求次數，並可扣住一次回覆的 JSON。
- **舊回覆晚到**：
  - 第一次重新讀取的 JSON 被扣住；
  - 伺服器端新開一場 `show-a-07`；
  - 第二次讀取已顯示 `show-a-07`；
  - 這時才放行第一次的 JSON，清單仍包含 `show-a-07`。
- **單一計時器**：連續觸發後，11 秒內清單讀取 1–3 次（實測 2 次）。
- **清單失敗的降級**：
  - 以 CDP `Fetch` 讓登入時的第一次 `/api/tasks` 回 503；
  - viewer 仍顯示已登入，`data-state` 為 error，狀態列提示清單讀取失敗；
  - 手動輸入 `show-a-01` 仍可開啟；
  - 下一次輪詢後，`data-state` 為 ok，清單出現。

**反向驗證**：
- 拿掉序號後，「舊回覆晚到」以斷言失敗：清單被換回沒有 `show-a-07` 的舊清單。其餘 145 項通過。
- 「單一計時器」在反向下仍通過（2 次）。原因是 `refreshList` 一進入就清除 `listTimer`：重複的計時器在下一輪就合併了，讀取次數不會累積。
  - 所以這項檢查是護欄，不能單獨證明序號的效果。
  - 序號的效果由「舊回覆晚到」證明。

**失敗嘗試（保留）**：
- 第一次執行時，新增段落用了與檔案層級 `visible()` 同名的常數，測試在第 13 項後以 `Cannot access 'visible' before initialization` 中止。已改名為 `wake`。

**未做**：
- 3.4、3.5。
- 只用 Chrome。

### 14.6 review 後修正 R1：已有清單後更新失敗要看得見（Claude Code 執行，待 Codex 獨立 review；程式 `2c55835`）

依據：Codex 對 `0818db8` 的 review R1。
- 位置：`web/app.js` 的 `listFailed()`。
- 問題：已有 task 按鈕時，更新失敗只設定 `data-state="error"`，不顯示任何文字，CSS 也沒有轉成可見提示。
- Codex 以 503 與斷線各重跑三輪，結果都是：觀眾仍看到原本的「等待核准」，畫面文字完全不變。
- 14.5 的瀏覽器測試只涵蓋首次載入失敗，沒有涵蓋「成功載入後，後續更新失敗」。

只補 R1，請求序號（14.5）不重做。

**修正**：
- **檔案**：`web/index.html`、`web/host.html` 在清單旁新增 `<span id="list-note" role="status">`。`web/style.css` 中，它平時為灰色，`data-kind="warn"` 時為警示色。
- **`web/app.js`**：
  - `listAt`：畫面上那份清單最後成功讀取的時間（瀏覽器本機時間 HH:MM:SS），不是後端資料快照時間；讀到之前為空。
  - 成功（`renderList`）時，更新 `listAt`，提示文字為「清單更新於 HH:MM:SS」。錯誤因此清除，時間更新。空清單仍顯示「尚無 task」。
  - 已成功讀過，之後更新失敗（`listFailed`）時，舊按鈕保留，提示文字為「更新失敗，以下為上次成功資料（HH:MM:SS）」，時間是畫面上那份清單最後成功讀取的時間。
  - 從未成功讀過就失敗時，清單清空，提示文字為「清單尚未取得，可手動輸入 task ID」，不顯示「尚無 task」。登入時的狀態列也用同一句話。
  - 驗證成功、首次讀取中，顯示「清單讀取中…」。
  - 清除或切換身分（`forget()`）時，`listAt` 與提示文字一併清空。
  - 提示文字只寫在 `#list-note`。輪詢中的更新失敗不改狀態列、Inspector、裁決或回放。
- **時間格式**：用 `Date#toTimeString().slice(0, 8)`，不用 `toLocaleTimeString`，避免不同環境把小時寫成一位數或 24 時。

**測試與證據**：
- Go、Unity 與驗證工具未變，依指示不重建 Unity，也不重跑整套容器與突變。

| 項目 | 命令 | exit | 時間 | 證據 |
|---|---|---|---|---|
| Node 30 項（未變）＋語法檢查 | `node --test web/test/adapter.test.mjs web/test/hostcreds.test.mjs`；`node --check` | 0 | 1 秒內 | `artifacts/b1-06/checks-2c558353c47f-20261006T033744Z/` |
| 真實瀏覽器 157 項（本次新增 11 項） | `node scripts/verify/browser-ui.mjs` | 0 | 約 199 秒（目錄時間到日誌最後修改） | `artifacts/b1-06/ui-2c558353c47f-20261006T033414Z/` |
| 瀏覽器反向：已有清單時更新失敗不寫提示 | 同上，在提交前的工作區暫改後還原。當時 HEAD 為 `5781d6e`，所以執行目錄名為 `ui-5781d6eb3fb3-20261006T033034Z`；除這一行外，程式與測試和 `2c55835` 相同 | 1（預期，155 PASS／2 FAIL） | 未計時 | `artifacts/b1-06/reversal-2c558353c47f/`（差異與輸出） |

**新增的瀏覽器檢查**：斷言讀的都是觀眾看得見的文字，也就是顯示中元素的 `innerText`，不是只看 `data-state`。
- **切換身分**：切換前的提示是「清單更新於 …」，切換後清空。
- **首次失敗**：登入時第一次 `/api/tasks` 回 503。
  - 看得見的提示為「清單尚未取得，可手動輸入 task ID」；
  - 清單中沒有「尚無 task」；
  - 狀態列同樣提示。
- **恢復**：提示為「清單更新於 HH:MM:SS」。
- **先成功取得非空清單，之後更新失敗**：503 與斷線（CDP `Fetch.failRequest`，`InternetDisconnected`）各一輪，每輪檢查：
  - 看得見的提示等於「更新失敗，以下為上次成功資料（失敗前提示中的清單最後成功讀取時間）」；
  - 舊清單按鈕不變，而且看得見；
  - 開著的 `show-a-01` 的狀態列與 Inspector 不變。比對時遮掉 Store 時間，因為 task 的輪詢仍在跑，那個時間每次都會更新；
  - 下一次成功讀取後，提示回到「清單更新於 HH:MM:SS」，`data-state` 為 ok。

**反向驗證**：
- 把 `listFailed()` 改回「已有清單時不寫提示」，503 與斷線兩項「失敗看得見」都以斷言失敗：畫面仍是「清單更新於 …」。其餘 155 項通過。

**失敗嘗試（保留）**：
- 第一次執行時，「狀態列與 Inspector 不變」直接比對全文，在 503 那一輪失敗。原因是狀態列含 Store 時間，task 輪詢每次都會更新它；斷線那一輪碰巧在同一秒內，所以通過。
- 這是測試的錯誤，不是產品問題：清單失敗沒有改動狀態列。
- 改為遮掉 ISO 時間後再比對，並要求狀態列仍指向 `show-a-01`。

**未做**：
- 3.4、3.5。
- 只用 Chrome。

### 14.7 驗收紀錄：切片 3.3 於 `2c55835` 通過限定範圍技術 review（2026-10-06）

依據：Codex 對 `2c55835` 的限定 review，由 Aaron 於對話中轉交。

**結論**：
- B1-06 交付點 3.3（動態 task 清單與新場次選擇）於 `2c55835` 通過限定範圍技術 review。
- R1（已有清單後更新失敗看不見，14.6）關閉；R2（同一身分的清單請求序號，14.5）維持關閉。
- 本次未發現新的阻擋項目。
- Codex 確認：
  - 更新失敗時，清單旁顯示警告與上次成功讀取時間，舊按鈕保留；
  - 首次載入失敗不再誤顯示「尚無 task」；
  - 恢復成功、清除或切換身分時，提示正確更新；
  - 不影響正在查看的 task、Inspector 或裁決。

**清單時間的定義**：提示中的時間是畫面上那份清單最後成功讀取的時間，即瀏覽器本機時間，不是後端資料快照時間。14.1、14.6 已依此用語修正。

**證據歸屬**：

| 類別 | 內容 |
|---|---|
| Codex 本輪獨立執行（各三輪，全部通過） | 503、斷線、恢復、清除與首次失敗探測；請求序號回歸；Node 30 項。JS 探測使用 DOM／網路替身，不等於真實瀏覽器操作 |
| Codex 前一輪獨立執行 | 斷網容器中 Node 30 項；舊回應、中止後計時器探測各三輪（14.5 的 review） |
| Claude Code 執行、Codex 核對紀錄 | `2c55835` 的瀏覽器 157 項（`artifacts/b1-06/ui-2c558353c47f-20261006T033414Z/`）；移除提示後兩項如預期失敗的反向驗證（`artifacts/b1-06/reversal-2c558353c47f/`）；`0818db8` 的瀏覽器 146 項與反向（14.5） |

**完整容器的歸屬**：
- 本輪沒有重跑 Go、Unity 或完整容器。
- 最近一次完整容器驗證屬 `843ca4a`：七步全過，238／238 突變，17 項隔離。
- 不能寫成 `2c55835` 已通過完整容器。
- `0818db8`、`2c55835` 只改前端與瀏覽器測試，Go、Unity 與驗證工具未變。

**保留**：
- 失敗嘗試：見 14.3，以及 14.5、14.6 的「失敗嘗試」。
- 未做：見 14.4，以及 14.5、14.6 的「未做」。只用 Chrome；清單不分頁，最多 50 筆。
- 既有展示資料庫沒有被操作或升級。

**下一步**：3.4（取消 task、錯誤呈現與中斷收尾），待另行派工，尚未開始。

B1-04／B1-06 整體維持 InProgress；不合併、不推送、不標整卡 Done。

## 15 切片 3.4：取消 task、錯誤呈現與中斷收尾（Claude Code 執行，待 Codex 獨立 review；程式 `d61771c`）

依據：
- Aaron 轉交的〈B1-06 交付點 3.4 實作指示〉（vault，`12a3325` 基準，2026-10-06）。
- 本文件第 6.1、6.2、7 節、第 8 節 3.4 列，以及第 13 節。

開工核對：分支 `b1-06-demo-integration`，HEAD `12a3325`，工作區乾淨，main `5391373`。

### 15.1 決定

| 項目 | 決定 |
|---|---|
| 端點 | `POST /api/host/tasks/{task_id}/cancel`（`cmd/demo/cancel.go`）。沿用 `postGuard`：只有 operator；Host 檢查；必要且同源的 Origin；`application/json`；1 KiB；body 只能是嚴格的 `{}`。task ID 格式不符回 `400 bad_request` |
| 判斷順序（第 6.1 節） | 在 `control.mu` 內先讀 task（`ReadTask`），然後依序：<br>① 不存在 → `404 not_found`<br>② 該 task 的場次在 `running`／`stopping`（`c.active`）→ `409 run_active`<br>③ 已有 `TASK_CANCELLED` → `200 {"state":"cancelled","already":true}`<br>④ 已有 `TASK_TIMED_OUT` → `409 already_ended {"end":"TASK_TIMED_OUT"}`<br>⑤ `denied` → `409 not_eligible {"status":"denied"}`<br>⑥ 其他 → `Store.Cancel`，提交成功才回 `200 {"state":"cancelled"}` |
| `TASK_ALREADY_ENDED` | 重新讀取，依③–⑤回應。重讀後仍不是終態屬異常，回 `503`、代碼 `TASK_ALREADY_ENDED` |
| Store 錯誤 | 讀取、寫入、重讀失敗一律回 `503 store_unavailable`，代碼用 `store.CodeOf`（無代碼時為 `STORE_ERROR`）。先前確認存在的 task 在寫入或重讀時不見，回 `503`、代碼 `TASK_NOT_FOUND`（tasks 不會被刪除，這代表 Store 異常）。新增的兩個代碼集中定義在 `cancel.go`，不加入契約 reason code |
| 鎖 | 從第一次讀取到回應都持有 `control.mu`，與「開始」相同。Store 呼叫不需要這把鎖，所以不會在持鎖時等待需要同一把鎖的程序。開始新場次只會配發新 task，碰不到這個 task。場次結束時，`runOne` 也在同一把鎖內清掉 `c.active` |
| 回應內容 | 只有固定欄位（`state`、`already`、`error`、`end`、`status`、`code`），不帶 task 詳情、請購或事件 |
| Store 語意 | 只呼叫既有的 `Store.Cancel`：到期的 Ask 先到期，其餘撤回，再寫 `TASK_CANCELLED`。不改 Policy、期限、Deny 保留規則或 schema，不新增資料表 |
| 歷史與現況 | `host_runs` 和場次 view 不因取消而改寫：`interrupted` 仍是 `interrupted`，`store_end_error` 保留 |
| `/host` 場次行 | 分成兩段：<br>① **當時**：`runText`，例如「程序已停止；當時取消紀錄寫入失敗（代碼）」「場次曾中斷（上一個 host 未正常結束）」「CLI 已逾時（已記錄）」。<br>② **目前**：`taskNow`，用 host 唯讀 token 讀 `GET /api/tasks`；`running`／`failed` 的 task 另讀 `GET /api/tasks/{id}`，判斷 Ask 是否到期或仍未決（每輪最多 5 筆，`ponytail:` 上限）。<br>不擴大 operator 的讀取權限；讀取失敗時顯示「讀取失敗，未確認」 |
| 現況文字 | 「Ask 等待中；task 可由主持人取消」「Ask 已到期；task 仍可由主持人取消」「task 現已取消」「後續已成功取消 task」（當時的取消紀錄寫入失敗）「Deny：原因」「未決 Ask 仍依原到期機制處理」／「Ask 已依原到期機制到期」（CLI 逾時） |
| 取消按鈕 | 「取消 task <ID>」只在場次不是 `running`／`stopping`，且 task 現況是 `awaiting_approval` 或 `running` 時出現。執行中的場次只有「停止」。按鈕只是提示，伺服器會重查 |
| 送出期間 | 一次只能有一個取消，送出期間所有取消按鈕停用。不自動重送，不新增輪詢迴圈：task 讀取併入既有的 2 秒場次輪詢 |
| 回覆的世代 | 回覆讀完後，憑證世代與 Dashboard 的 `viewKey()`（身分世代＋task）都要與按下時相同，才更新畫面。<br>清除全部、換檔、手動模式、`pagehide` 時，hostcreds 的世代檢查與 `host.js` 的檢查都會丟棄回覆。<br>切換 consumer 或 task 時，只顯示「回覆未套用（畫面已切換）；結果以最近場次的目前狀態為準」。<br>不登入、不開 task、不恢復舊憑證。清除憑證不撤銷伺服器已受理的取消 |
| 200 之後 | 呼叫 `refreshNow()`（Dashboard 的清單與開著的 task 立即重讀），再用 host token 讀該 task。讀到 `cancelled` 時顯示「task X 已取消」；讀取失敗時顯示「取消已記錄；畫面更新失敗」，不宣稱取消失敗 |
| 回覆遺失 | 顯示「結果尚未確認（沒有收到回覆）；請看最近場次的目前狀態，需要時再按一次（伺服器不會重複寫入）」 |
| 拒絕的文字 | `not_found`、`run_active`（請用「停止」）、`already_ended`（原紀錄保留）、`not_eligible`（Deny 原因保留）、`store_unavailable`（附代碼）各有固定文字，都寫在 `#run-status`（可見文字，不只 `data-*`） |

### 15.2 實際操作

使用新的測試資料庫，不刪舊檔：

```sh
APA_DB_PATH=var/apa-demo-34-check.db make demo   # 新資料庫；印出 http://127.0.0.1:8402/host
```

1. 開啟 `/host`，按「載入本機 token 檔」，選 `var/demo-tokens.json`。
2. 按「開始：主商品，Ask（等待核准）（consumer-a）」。頁面切到 consumer-a 並開啟新 task，商店顯示 Ask 等待。
3. 「最近場次」該行顯示「場次完成；Ask 等待中；task 可由主持人取消」和「取消 task show-a-NN」按鈕。按下後顯示「task show-a-NN 已取消」，該行改為「task 現已取消」。
4. Dashboard 的 Timeline 出現 `APPROVAL_WITHDRAWN`、`TASK_CANCELLED`，Inspector 的 task 狀態為 `cancelled`，商店顯示「Ask 已撤回（task 取消）：離店」。
5. 不按取消、等 30 秒，Ask 會由到期規則記錄；之後該行顯示「Ask 已到期；task 仍可由主持人取消」。
6. 執行中的場次只有「停止」；Deny 與 CLI 逾時的 task 沒有取消按鈕。

本輪 Claude Code 沒有啟動 `make demo` 做人工操作。上列步驟對應的流程，由真實瀏覽器測試在新的拋棄式資料庫上執行（15.3）。

### 15.3 指示第 6 節與測試對照（Claude Code 執行，待 Codex 獨立 review）

| 指示 | 測試 |
|---|---|
| HTTP 1：權限矩陣與防護；被拒不寫事件 | `TestCancelAccessAndGuards`：無 token／錯誤 token 401；viewer、host 403；無 Origin、外站 Origin、別的 Host、表單 Content-Type、無 body、超過 1 KiB、未知欄位、兩個 JSON 值、陣列、格式錯誤、URL 中的 token 都被拒；task ID 格式錯誤 400。被拒後事件數不變，operator 的 200 只有 `state` |
| HTTP 2：第 3 節每一列；期限前、恰好、之後 | `TestCancelRows`：<br>• 404（未儲存的 `show-a-20` 與任意 ID）；<br>• Ask 等待中取消，事件與 `cancel-pending` 案例一致，重複取消回 `already` 且不寫；<br>• Deny 409，原因保留；<br>• 第 29 秒取消：先撤回，再 `cancel-pending`；<br>• 恰好第 30 秒：先到期，再 `approval-deadline-exact`；<br>• 到期 worker 處理後（`running`）可取消，只有 EXPIRED、沒有 WITHDRAWN；<br>• 時鐘落後時寫入被拒，回 503 `CLOCK_REGRESSION`，不寫，恢復後可取消。<br>`TestCancelAfterCLITimeout`：逾時後 Ask 仍 pending（`cli-timeout-pending`），取消回 409 `already_ended`；Ask 之後照常到期，仍是 409，沒有 `TASK_CANCELLED` |
| HTTP 3：並行只一個取消、每筆 Ask 收尾一次、重開一致 | `TestCancelConcurrent`：3 個 task 各 6 個並行取消，另加到期 worker，都在恰好期限時。每個 task 只有 1 個非 already 的 200、1 個 `TASK_CANCELLED`、1 個收尾事件；重啟後事件逐筆相同（JSON 比對） |
| HTTP 4：執行中被拒；停止與取消交錯 | `TestCancelWhileRunning`：執行中 409 `run_active` 且不寫。停止與 8 個取消交錯時，答案只有 `409 run_active` 或 `200 already`。最後 1 個 `TASK_CANCELLED`、1 個撤回、1 個請購，`TASK_CANCELLED` 之後沒有任何事件 |
| HTTP 5：讀取、重讀、寫入失敗不回成功 | `TestCancelStoreFaults`（未匯出的測試掛鉤 `cancelFault`，不是公開 API）：<br>• 讀取失敗 → 503 `STORE_ERROR`，不含錯誤文字；<br>• 讀取與寫入之間另一個寫入者已取消 → 依已提交狀態回 `already`，只有一個 `TASK_CANCELLED`；<br>• 另一個寫入者記了逾時 → 409 `already_ended`，沒有取消；<br>• 重讀失敗 → 503，不回 `state`；<br>• 重讀時 task 不見 → 503 `TASK_NOT_FOUND`。<br>寫入失敗見 HTTP 2 的時鐘案例 |
| HTTP 6：正常關閉、異常終止後重啟 | `TestCancelAfterRestart`：<br>• 正常關閉時在跑的場次，重啟後為 ended、`TASK_CANCELLED`，取消回 `already`；<br>• 兩筆紀錄都寫入失敗 → 重啟後 interrupted，task 可取消；<br>• 有 run、沒有 task（只寫了 `host_runs`）→ interrupted，取消回 404；<br>• 沒有自動重跑（場次數不變） |
| HTTP 7：補取消後歷史不變、重開通過恢復 | 同上：<br>• 取消前後 `GET /api/host/runs` 完全相同，`store_end_error` 仍是 `CLOCK_REGRESSION`，interrupted 仍是 interrupted；<br>• 關閉後 `store.VerifyFile` 通過，再開一次仍相同 |
| Node | `web/test/hostcreds.test.mjs` 新增 4 項，共 34 項（含 adapter）：<br>• `cancelTask` 只送一次、回覆遺失不重送；<br>• 送出後清除全部或換檔，舊回覆丟棄、憑證不變（測試替身不理會 abort，所以驗的是世代檢查）；<br>• 401 只在自己的世代清除；<br>• `hostRead` 用 host token，同一世代規則 |
| 瀏覽器 1：Ask 等待取消，四處一致 | 新開 Ask 後取消：可見文字「task show-a-05 已取消」、該行「task 現已取消」；Unity 回報同一 task 的 `withdrawn`；Timeline 有 `APPROVAL_WITHDRAWN`、`TASK_CANCELLED`；Inspector 有 `cancelled`、`withdrawn`；全頁沒有付款或交付成功字樣 |
| 瀏覽器 2：到期後可取消；逾時與 Deny 被拒、原因不消失 | 種子資料庫（`TestSeedCancelDisplay`，經 host 自己的場次寫入）：<br>• 到期 Ask 的行顯示「Ask 已到期；task 仍可由主持人取消」，取消後事件為 EXPIRED → CANCELLED；<br>• 逾時與 Deny 的行沒有取消按鈕；<br>• 以 CDP 竄改 host 的 `/api/tasks` 回覆，讓按鈕出現：伺服器回 409，頁面顯示固定文字，事件數不變，下一輪恢復「Deny：INSUFFICIENT_BUDGET」與「CLI 已逾時（已記錄）」 |
| 瀏覽器 3：中斷後與收尾失敗後補取消 | 重啟時標記 1 個 interrupted，沒有重跑：<br>• 中斷場次的行由「場次曾中斷…；Ask 已到期；task 仍可由主持人取消」變成「場次曾中斷（上一個 host 未正常結束）；task 現已取消」；<br>• 收尾失敗的行由「程序已停止；當時取消紀錄寫入失敗（CLOCK_REGRESSION）；…」變成「…；後續已成功取消 task」；<br>• 三次取消後，場次歷史 JSON 不變 |
| 瀏覽器 4：已提交後扣住回覆，再切換或清除；區分世代與 abort | 在頁面包裝 `fetch`：取消的回覆在伺服器提交後才扣住 JSON，abort 結束不了它。分四種情況：<br>• 切換 task；<br>• 切換 consumer；<br>• 清除全部；<br>• 重新載入檔案。<br>放行後都沒有顯示舊結果，不開舊 task、不切回舊 consumer、不恢復憑證；伺服器的取消仍在，重新載入後由 Store 顯示「task 現已取消」 |
| 瀏覽器 5：取消後讀取失敗、POST 回覆遺失 | • CDP 在回應階段讓取消回覆失敗：顯示「結果尚未確認」，不含「已取消」也不含「未取消」；伺服器只有一個 `TASK_CANCELLED`，頁面只送一次。<br>• CDP 讓 200 之後的 host 讀取失敗：顯示「取消已記錄；畫面更新失敗」 |
| 瀏覽器 6：回放唯讀 | 取消後回放：沒有送出取消，事件數不變 |
| 其他 | 執行中的場次只有「停止」；停止後為「已停止，取消已記錄」，沒有取消按鈕。storage、cookie、URL、console 都沒有 token |

### 15.4 測試與證據（Claude Code 執行，待 Codex 獨立 review；全部在 `d61771c`）

| 項目 | 命令 | exit | 時間 | 證據 |
|---|---|---|---|---|
| `make check` | `go vet`、`go test ./...`、`go build` | 0 | 58 秒 | `artifacts/b1-06/checks-d61771c86c67-20261006T050106Z/` |
| `make check-contracts`（含 Node 34 項） | | 0 | 2 秒 | 同上 |
| 相關套件 race | `go test -race -count=1 ./cmd/demo ./internal/store` | 0 | 181 秒 | 同上 |
| 取消測試 race ×3 | `go test -race -count=1 -run TestCancel -v ./cmd/demo` | 0／0／0 | 59／59／57 秒 | 同上 |
| Unity 建置 | 未重建；4 個檔案的 hash 與 `843ca4a` 證據相同 | — | — | 同上 `webgl-loaded.sha256` |
| 真實瀏覽器 195 項（3.4 新增 38 項） | `node scripts/verify/browser-ui.mjs` | 0 | 290 秒 | `artifacts/b1-06/ui-d61771c86c67-20261006T050809Z/` |
| 瀏覽器反向：拿掉 `viewKey()` 檢查 | 同上，工作區暫改後還原 | 1（預期） | 296 秒 | `artifacts/b1-06/reversal-d61771c86c67/` |
| 完整斷網容器 | `sh scripts/verify/container-verify.sh d61771c…` | 0 | 906 秒 | `artifacts/verify-container/runs/d61771c86c67-20261006T052031Z/`（`artifacts/b1-06/container-d61771c.log`）：七步全過（check-contracts 27、check 112、race-cover 246、verify-b1-01 47、mutation 454 秒），adapter 11／11、hostcreds 23／23，245／245 突變，17 項隔離 ok；主機上沒有同時執行瀏覽器或 Unity |

**突變**：新增 7 項（共 245），本機逐項確認都以斷言失敗被抓到（`--- FAIL`），不是編譯失敗：
- host token 可取消；
- 拿掉 `run_active` 檢查；
- 重複取消不再依已存的 `TASK_CANCELLED` 回答；
- Deny 被取消；
- 寫入被拒卻回 200；
- `TASK_ALREADY_ENDED` 直接當成已取消；
- 重讀失敗卻回已取消。

**瀏覽器反向**：`current()` 只留憑證世代、拿掉 `viewKey()` 後，「切換 task」「切換 consumer」兩項以斷言失敗：舊回覆顯示「task show-a-08 已取消」「task show-a-09 已取消」。清除全部與換檔兩項仍通過，因為由世代檢查擋下，與 Node 測試涵蓋的層相同。

同一次反向另有一項既有檢查失敗：「R2: the backend added an event during the replay」（`as_of_sequence 4 -> 4`）。原因與 `host.js` 無關：
- 它屬於交付點 2 的 `/` 頁段落，就緒條件依時間而定（等到期限前 1 秒、再等一次輪詢）。
- 這次到期在回放開始前就已寫入，所以起點已是 4。
- 本輪提交後的完整執行中，它通過。
- 記錄為既有檢查的時間敏感問題，本輪不修（不在 3.4 範圍）。

### 15.5 失敗嘗試（保留）

- **測試錯誤**：
  - 第一版 `TestCancelConcurrent` 用 `fmt.Sprint` 比對事件。事件含 `Decision` 指標，位址不同，誤判為不一致；改為 JSON 比對。
  - `fmt.Sprint` 的空白規則讓「停止與取消交錯」的答案字串不如預期；改用 `Sprintf`。
  - `TestCancelAfterRestart` 第一版連開兩個 hang，第二個必然 409；已移除。
- **命名衝突**：
  - `cancel.go` 的 `ended` 與測試輔助函式 `ended` 同名，改為 `answerEnded`。
  - 瀏覽器段落的 `release` 與既有變數同名，改為 `releaseCancel`。
- **容器第一次執行**：12 秒即失敗，訊息為 `No such image: apa-verify:b1-05`（`artifacts/b1-06/container-d61771c-attempt1-no-image.log`）。
  - 當時是背景執行，使用預設 Docker context（另一個 daemon）；前景用 Docker Desktop context 時，映像存在。
  - 第二次明確設定 `DOCKER_CONTEXT=desktop-linux`，映像與限制不變。第一次留下的空目錄 `artifacts/verify-container/runs/d61771c86c67-20261006T051933Z/` 保留，不是通過紀錄。
- **反向中的既有時間敏感檢查**：見 15.4。

### 15.6 未做與未測

- 3.5（整體操作驗收）。
- 只用 Chrome。
- **CLI 逾時後 Ask 仍未決的畫面文字**：「未決 Ask 仍依原到期機制處理」只在 Go 測試驗證伺服器行為。瀏覽器的種子資料庫為了讓到期確定，時鐘提前 10 分鐘，所以畫面上看到的是「Ask 已依原到期機制到期」。
- **確認對話框**：取消前沒有對話框。按鈕文字含 task ID，伺服器重查；如需對話框，請 review 指示。
- **人工操作**：本輪沒有以 `make demo` 人工操作。
- **既有展示資料庫**：沒有被操作或升級。
- **其他**：沒有執行真實 CLI、付款、testnet、Signer。

### 15.7 review 後修正 R1～R3（Claude Code 執行，待 Codex 獨立 review；程式 `3a6af59`，測試補充 `3b24cd4`）

依據：Codex 對 `d61771c` 的 review，完整內容見 vault〈Claude-Code-B1-06-交付點3.4修正指示-d61771c-20261006〉。`d61771c` 尚未通過。最初轉貼的內容被截斷，缺少 R1；R2、R3 先修、未提交，取得完整指示後補上 R1，三項一起提交。

| 項目 | 問題 | 修正 | 測試 |
|---|---|---|---|
| R1 請求 body | 共用的 `postGuard` 把 body 解碼進 struct：`null` 不會出錯；`More()` 也不代表 body 已結束。`null`、`{}]`、`{} }` 都能取消 task（Codex 探測） | `cmd/demo/control.go` 的 `postGuard`（開始、停止、取消共用）：<br>• 先讀完整個 body（仍限 1 KiB）；<br>• 去掉 JSON 空白後，第一個字元必須是 `{`；<br>• 嚴格解碼；<br>• 之後 `Token()` 必須回 `io.EOF`，只允許空白。<br>拒絕一律回 `400 bad_request`，不回傳原始輸入。Store、Policy、契約都未改 | `TestCancelBody`：16 種不合法 body，各用一個獨立且等待中的 task，都回 400；事件逐筆（JSON）、task 狀態、場次清單都不變。`{}` 與前後有空白的 `{ }` 可以取消，重送回 `already`，不重複寫入。<br>`TestControlBodyShape`：開始的 `null`、物件後接 `]`／`}`、第二個值、陣列都回 400 且不建立場次；前後有空白的合法開始仍回 202。停止的不合法 body 回 400，程序仍在跑；` {}\n` 可以停止，收尾為 `TASK_CANCELLED` |
| R2 未取得詳情 | 詳情讀取 503，或超過每輪 5 筆的上限時，畫面顯示「無未決 Ask」 | `web/host.js` 的 `taskNow`：沒有詳情時顯示「Ask 狀態未確認（task 詳情未取得）」（逾時的 task 前面仍保留「CLI 已逾時」）。只有取得詳情後，才判斷未決、已到期或確定沒有。5 筆上限保留 | 瀏覽器：<br>• show-a-01 的詳情以 CDP 回 503，該行顯示「未確認」，沒有「無未決 Ask」；恢復讀取後回到「Ask 已依原到期機制到期」；<br>• 竄改清單，讓 14 筆都成為 `failed`，第 6 筆起共 9 行顯示「未確認」，且這些行都沒有「無未決 Ask」 |
| R3 503 文字 | 「Store 不可用，取消未完成」：另一個寫入者可能已提交取消（重讀失敗時也會回 503） | 改為「Store 不可用，取消結果尚未確認（可能已由其他寫入完成）；請看最近場次重新讀取的目前狀態」，附代碼。不當成已取消，也不當成未取消，不自動重送；場次清單重新讀取 Store。「已收到 200、之後畫面讀取失敗」的文字保留 | Go：`TestCancelStoreFaults` 的「重讀失敗」案例加斷言：資料庫恰好一筆 `TASK_CANCELLED`，task 為 `cancelled`。<br>瀏覽器：<br>• 伺服器已提交後以 503 取代回覆：固定的不確定文字，沒有「未取消」或「取消未完成」，恰好一筆 `TASK_CANCELLED`，之後該行為「task 現已取消」；<br>• 請求不到伺服器就回 503（`3b24cd4`）：同樣文字，沒有任何結論字樣，沒有 `TASK_CANCELLED`，該行仍是「Ask 等待中；task 可由主持人取消」，按鈕還在 |

**證據**（Claude Code 執行，待 Codex 獨立 review）：

| 項目 | 命令 | exit | 時間 | 證據 |
|---|---|---|---|---|
| vet、gofmt | `go vet ./cmd/demo`；`gofmt -l cmd/demo`（無輸出） | 0 | 2 秒 | `artifacts/b1-06/checks-3a6af595101f-20261006T065209Z/` |
| `cmd/demo` 全部 race | `go test -race -count=1 ./cmd/demo` | 0 | 250 秒 | 同上 |
| R1 回歸 race ×3 | `go test -race -count=1 -run 'TestCancelBody\|TestControlBodyShape' -v ./cmd/demo` | 0／0／0 | 57／60／58 秒 | 同上 |
| 取消與控制回歸 race | `go test -race -count=1 -run 'TestCancel\|TestControl' -v ./cmd/demo` | 0 | 135 秒 | 同上 |
| Node 34 項與語法檢查 | `make check-contracts`；`node --check` | 0 | 3 秒 | 同上 |
| 真實瀏覽器 202 項（本輪新增 7 項） | `node scripts/verify/browser-ui.mjs`，在 `3b24cd4` 執行 | 0 | 243 秒 | `artifacts/b1-06/ui-3b24cd4d10be-20261006T070202Z/` |
| R1 反向（2 項突變） | 拿掉物件型別檢查；拿掉 EOF 檢查 | 兩項都以 `--- FAIL` 抓到 | — | `artifacts/b1-06/reversal-3a6af595101f/r1-mutations.out` |
| R2／R3 反向 | `web/host.js` 換回 `d61771c` 版 | 1（預期，197 PASS／3 FAIL，正是當時 3 項 R2／R3 檢查） | 332 秒 | `artifacts/b1-06/reversal-3a6af595101f/r2-r3-old-host-js.out` |

- `3a6af59` 與 `3b24cd4` 的產品程式相同，只差 `browser-ui.mjs`。Go 與 Node 的結果在 `3a6af59` 取得，適用於兩者。
- R2／R3 反向在提交前的工作區執行。`web/host.js` 已用 `cmp` 確認與 `3a6af59` 相同，當時的瀏覽器測試只缺 `3b24cd4` 新增的檢查。真正的執行基準記在該目錄的 `BASELINE.txt`。
- 突變清單新增 R1 的 2 項，共 247 項。依指示，本輪沒有在容器重跑全部突變，也沒有重建 Unity。
- `d61771c` 的完整容器七步、245 項突變與 17 項隔離，只屬於 `d61771c`，不是修正後程式的結果。

**失敗與未測**（保留）：
- **反向的附帶失敗**：R1 反向拿掉物件型別檢查時，`TestControlBodyShape` 的停止案例在 `null` 被接受後，程序已在停止中，後續停止案例也連帶失敗。這是反向的連鎖結果，不是另外的問題。
- **R2／R3 的 Node 測試**：沒有。`host.js` 依賴 DOM，所以只由瀏覽器檢查。
- **既有回放時序檢查**：維持待辦（15.4）。
- **確認對話框**：沒有加，依 review 不是必修。
- **其他**：只用 Chrome；3.5 未開始；既有展示資料庫沒有被操作。

### 15.8 驗收紀錄：切片 3.4 於 `3a6af59` 通過限定範圍技術 review（2026-10-06）

依據：Codex 對 `3a6af59` 的限定 review，由 Aaron 於對話中轉交。

**結論**：
- B1-06 交付點 3.4（取消 task、錯誤呈現與中斷收尾）通過限定範圍技術 review。驗收基準：程式 `3a6af59`，瀏覽器測試補充 `3b24cd4`，文件 `1e9b0d1`。
- R1（請求 body）、R2（詳情缺失）、R3（取消回 503）全部關閉；沒有新增阻擋問題。
- Codex 確認：
  - `null`、`{}]`、`{} }` 回 400，不新增事件；合法 `{}` 正常取消；開始與停止的回歸通過。
  - 詳情讀取失敗或超過五筆上限時顯示「未確認」，恢復後呈現正確狀態。
  - 取消回 503 時顯示「結果尚未確認」；已提交取消的案例只有一筆 `TASK_CANCELLED`。

**證據歸屬**：

| 類別 | 內容 |
|---|---|
| Codex 本輪獨立執行（全部通過） | 斷網容器內的定向 Go race 測試、原始 body 探測、JS 畫面邏輯探測三輪、Node 34 項。JS 探測使用 DOM／網路替身，不等於真實瀏覽器操作 |
| Codex 前一輪獨立執行 | 七組既有取消 race 測試、Node 34 項；當時的 body 探測與 JS 探測重現了 R1～R3 |
| Claude Code 執行、Codex 核對紀錄 | `3b24cd4` 的瀏覽器 202 項（`artifacts/b1-06/ui-3b24cd4d10be-20261006T070202Z/`）與反向驗證（`artifacts/b1-06/reversal-3a6af595101f/`）；`d61771c` 的瀏覽器 195 項、完整容器、245 項突變（第 15.4 節） |

**完整容器的歸屬**：
- 最近一次完整容器驗證屬 `d61771c`：七步全過，245／245 突變，17 項隔離。
- `3a6af59` 與 `3b24cd4` 沒有重跑完整容器，也沒有重跑全部突變（清單現為 247 項，新增的 2 項只在本機逐項確認）。
- 不能把 `d61771c` 的結果寫成修正後版本的結果。

**保留的已知限制與歷史**：
- 失敗嘗試：15.5，以及 15.7 的「失敗與未測」。
- 未測與限制（15.6、15.7）：
  - CLI 逾時後 Ask 仍未決的畫面文字，瀏覽器沒有看到（只有 Go 驗證伺服器行為）；
  - 取消前沒有確認對話框（非必修）；
  - R2／R3 沒有 Node 測試；
  - 只用 Chrome；
  - 本輪沒有用 `make demo` 人工操作。
- 既有回放時序檢查（「R2: the backend added an event during the replay」）仍是待辦。
- 既有展示資料庫沒有被操作或升級；repo 根目錄未追蹤的 `demo` 執行檔沒有提交。

**下一步**：3.5（整體操作驗收），待另行派工，尚未開始。

B1-04／B1-06 整體維持 InProgress；不合併、不推送、不標整卡 Done。

## 16 切片 3.5：fake 展示流程整體操作驗收（Claude Code 執行，待 Codex 獨立 review；程式與測試 `667e933`）

依據：Aaron 轉交的〈B1-06 交付點 3.5 實作與驗收指示〉（vault，`6e2cbb9` 基準，2026-10-06）。

開工核對：
- 分支 `b1-06-demo-integration`，HEAD `6e2cbb9`，main `5391373`。
- 工作區只有未追蹤的 `demo` 執行檔，不處理、不提交。本輪建置都指定輸出位置。

本輪沒有修改產品程式（`cmd/`、`internal/`、`web/` 的非測試檔）、Unity、契約或 Store。只新增測試、驗收用情境目錄與文件。

### 16.1 回放時序測試的修正

- **對象**：既有檢查「R2: the backend added an event during the replay」（`/` 頁，固定計畫的 task-a）。
- **原因判斷**：
  - 舊的就緒條件只等到「期限前 1 秒」，之後還要等一次輪詢；到期 worker 約每秒一次，在期限後就會寫入。
  - 所以「回放先開始」還是「到期先寫入」要靠運氣。3.4 反向執行時曾失敗一次：開始回放時序號已是 4（15.4）。
  - 這是依程式行為推斷，並未在失敗當下另外取證。
- **修正**（只改 `scripts/verify/browser-ui.mjs`）：
  1. 依伺服器時鐘（`served_at`），等到 task-a 期限前 8 秒內，必須仍在期限前 4 秒以上，且序號為 3。不成立就是失敗。
  2. 在頁面安裝一個測試專用掛鉤：包裝 `window.apaUnityReport`，回放第 2 步起先扣住 Unity 的「已呈現」回報。每步最多等 6 秒（既有 `renderedBy` 上限），所以回放至少持續約 13 秒。
  3. 點回放，確認頁面進入回放，並同時讀到伺服器序號仍為 3：確認回放早於到期寫入。
  4. 輪詢伺服器，直到序號變成 4，並在那一刻確認頁面模式仍是 `replay`：確認到期是在回放期間寫入。
  5. 期間取樣狀態列，不得出現「已同步」。之後放行扣住的回報，回放結束，再核對 Unity 與 Dashboard 都是最新的 #4 expired。
- **順序證據**：每次執行都記在 `browser-test.log`。例如 `ui-667e933d0e25-20261006T085612Z`：「replay entered at 1791276999 with task-a #3; deadline 1791277007; #4 (APPROVAL_EXPIRED) committed, seen at 1791277007 in mode replay; 25 samples during the replay」。
- **反向**：
  - 不扣住任何回報（`__holdFrom = Infinity`）時，回放先結束；序號 4 是在 `live` 模式下看到的。「R2 order: the backend commits #4 while the page is still in replay」以斷言失敗，另兩項回放中的檢查也失敗。
  - 結果：exit 1，244 PASS／3 FAIL。
  - 證據：`artifacts/b1-06/reversal-667e933d0e25/`。
- **沒有改的**：沒有新增正式環境可呼叫的入口，沒有改 Policy、期限、Store 或回放語意。掛鉤只存在測試注入的頁面腳本中。

### 16.2 整體流程（自動化，同一個 host）

- **環境**：真實 `bin/demo` 與 `fake-claude`，demo registry，全新的拋棄式資料庫（路徑記在 log），之後在同一個資料庫重啟。
- **驗收用情境目錄 `testdata/demo/scenarios-acceptance.json`**：
  - 內容是出貨目錄的 `ask`、`deny-budget` 原樣，加上 `ask-wait`（`testdata/cli/hang.json`：Ask 後等待，直到停止或 fake CLI 的 3 分鐘時限）。
  - `make demo` 仍用 `testdata/demo/scenarios.json`，沒有改。
  - 加這個目錄的原因：出貨目錄的場次都會立刻結束，主持人無法示範「執行中停止」。
  - 這是測試與驗收資料，不是產品功能；請 review 決定是否接受，或改為併入出貨目錄。`TestLoadScenarios` 已加入它的載入檢查。

| 步驟 | 自動化檢查（每個情境都核對 Unity、Dashboard 場景、Inspector、Timeline 的 task、序號與結果） |
|---|---|
| 1 啟動、就緒 | 新資料庫，啟動時不跑任何場次；就緒面板 HTTP、Store、fake CLI、Unity 建置都是 ok；真實 CLI 與付款為 off |
| 2 token 檔 | 選檔欄位清空；storage、cookie、URL 都沒有 token |
| 3 Ask | 自動切到 consumer-a 並開啟 show-a-01；#3 waiting、Ask `HUMAN_APPROVAL_REQUIRED` pending、未付款；該行「場次完成；Ask 等待中；task 可由主持人取消」 |
| 4 Deny | 自動切到 consumer-b 並開啟 show-b-01；#3 denied、`INSUFFICIENT_BUDGET`；該行「場次完成；Deny：INSUFFICIENT_BUDGET」，沒有取消按鈕 |
| 4 停止 | `ask-wait` 的 show-a-02 進行中，只有「停止」。按下後顯示「已停止，取消已記錄」；#5 withdrawn，Timeline 有 `APPROVAL_WITHDRAWN`、`TASK_CANCELLED` |
| 4 取消 | show-a-03 場次結束後取消：「task show-a-03 已取消」、「task 現已取消」；#5 withdrawn |
| 4 到期 | show-a-01 在伺服器時鐘到期時由後端寫入 `APPROVAL_EXPIRED`；該行「Ask 已到期；task 仍可由主持人取消」；#4 expired |
| 5 切換與回放 | 切到 consumer-b 後畫面不留 consumer-a 的任何內容；切回後回放唯讀，事件數不變，結束後回到 #4 expired |
| 6 錯誤與恢復 | CDP 在瀏覽器網路層模擬（不是真實 Store 故障）：<br>• 詳情讀取回 503 時顯示「Ask 狀態未確認」，恢復後回到實際狀態；<br>• 取消請求在到達伺服器前回 503：顯示「取消結果尚未確認」，沒有寫入；<br>• 再按一次真正取消：「task show-a-01 已取消」；#5，expired 後 cancelled |
| 7 重啟 | 先留一個等待中的 Ask（show-a-04）與一個執行中的場次（show-a-05），再正常關閉，用同一個資料庫重啟：<br>• 頁面憑證被清除（舊 token 401）；<br>• 沒有重跑、沒有中斷；<br>• 場次與事件相同，只追加了 show-a-05 的關閉取消（以及 show-a-04 可能的到期）；<br>• show-a-05 的行為「已停止，取消已記錄」；歷史各行文字不變；<br>• show-a-04 之後依規則取消成功，四處一致 |

場次、task 與事件的對照（`ui-667e933d0e25-20261006T085612Z/browser-test.log` 的 run／task／events 段，每次執行的 run_id 不同）：

| 情境 | task | 事件 |
|---|---|---|
| Ask | show-a-01 | #3：TASK_STARTED、PURCHASE_PROPOSED、POLICY_DECIDED |
| Deny | show-b-01 | #3：同上（Deny） |
| 停止 | show-a-02 | #5：…、APPROVAL_WITHDRAWN、TASK_CANCELLED |
| 取消 | show-a-03 | #5：…、APPROVAL_WITHDRAWN、TASK_CANCELLED |
| 到期，之後取消 | show-a-01 | #4 APPROVAL_EXPIRED → #5 TASK_CANCELLED |
| 重啟：關閉時的取消 | show-a-05 | #5：…、APPROVAL_WITHDRAWN、TASK_CANCELLED |
| 重啟後取消 | show-a-04 | #5：…、APPROVAL_WITHDRAWN（或期限已到則為 EXPIRED）、TASK_CANCELLED |

截圖：`desktop-acceptance-ready`、`-ask`、`-deny`、`-stop`、`-cancel`、`-expired`、`-restart`.png。每張拍攝前都檢查 token 欄位為空；證據目錄另做 token 掃描。這幾張主要是上方控制區；含 Unity、Inspector、Timeline 的全頁截圖見 16.7。

### 16.3 Aaron 人工驗收清單（待 Aaron 執行）

自動化瀏覽器通過不能代替人工驗收。以下每步都還沒有替 Aaron 執行，也沒有勾選。

**啟動**：使用新資料庫，不刪舊檔。以下命令不含 token；token 只在 `var/demo-tokens.json`。

```sh
cd /Users/mac/Documents/OpenSourceProjects/agent-payment-authority && \
  go build -o bin/demo ./cmd/demo && \
  go build -o bin/fake-claude ./cmd/fake-claude && \
  APA_ADDR=127.0.0.1:8402 APA_DB_PATH=var/apa-demo-35-acceptance.db APA_DEMO_TOKEN_FILE=var/demo-tokens.json \
  ./bin/demo --fake-cli bin/fake-claude --scenarios testdata/demo/scenarios-acceptance.json \
  --registry testdata/demo/registry.json --unity-build unity/AgentPaymentAuthority/Build/WebGL/Build
```

整條是同一個成功鏈（`&&`）：進入 repo 或任一個建置失敗時，就不會啟動，也不會用到舊的 `bin/demo`。建置失敗時，終端機會顯示 Go 的錯誤，請停止並回報。

`var/apa-demo-35-acceptance.db` 若已存在（例如做過一次），請換一個新檔名，不要刪除舊檔。正常展示不需要重建 Unity、跑測試、改 SQLite 或刪資料庫。

**兩個時限不要混淆**：
- **Ask 期限是 30 秒**（從裁決起算，後端記錄到期）。
- **fake CLI 上限是 3 分鐘**（只對「Ask 後等待」的場次有意義）。

停止或取消時，結果取決於 Ask 是否已到期：
- **期限前**：`APPROVAL_WITHDRAWN` → `TASK_CANCELLED`，商店顯示「Ask 已撤回（task 取消）：離店」。
- **已到期後**：保留 `APPROVAL_EXPIRED`，再記 `TASK_CANCELLED`（不會再有撤回），商店仍顯示「Ask 已到期（後端已記錄）：離店」。

兩種都是正確結果。

| # | 操作 | 預期畫面 | 不符合時 |
|---|---|---|---|
| 1 | 執行上面的命令 | 終端機出現 `listening on http://127.0.0.1:8402`，沒有任何 `demo: run` 行 | 停止，回報終端機訊息（不附 token 檔） |
| 2 | 開 `http://127.0.0.1:8402/host`，按「載入本機 token 檔」，選 `var/demo-tokens.json` | 「已載入 token 檔：host、operator 與 2 位 viewer」。就緒面板：HTTP、Store、fake CLI、Unity 建置為就緒，真實 CLI 未啟用，付款為無付款能力 | 截圖（確認畫面沒有 token）後回報 |
| 3 | 按「開始：主商品，Ask（等待核准）」 | 頁面切到 consumer-a 並開啟 show-a-01；商店顯示 Ask 等待人工核准；Inspector 顯示 Ask、`HUMAN_APPROVAL_REQUIRED`、pending、未付款；Timeline #1–#3；最近場次「場次完成；Ask 等待中；task 可由主持人取消」。記下時間：約 30 秒後到期 | 回報 task ID 與畫面 |
| 4 | 按「開始：預算不足，Deny」 | 切到 consumer-b、show-b-01；商店顯示 Deny 離店；最近場次「Deny：INSUFFICIENT_BUDGET」，沒有取消按鈕 | 同上 |
| 5 | 按「開始：Ask 後等待（可按停止…）」，在 3 分鐘內按該行的「停止」 | 停止前該行為「進行中」，只有「停止」。按下後，上方顯示「場次 run-… 的停止請求已受理；最終結果請看最近場次。」（只是受理通知，不代表已完成）；該行隨後變成「已停止，取消已記錄」。<br>• 開始後 30 秒內按：Timeline 為 `APPROVAL_WITHDRAWN`、`TASK_CANCELLED`，商店顯示撤回。<br>• 超過 30 秒才按（例如第 60 秒）：Timeline 為 `APPROVAL_EXPIRED`、`TASK_CANCELLED`，商店顯示到期 | 超過 3 分鐘時該行會顯示「CLI 已逾時（已記錄）」，這也是正確行為（Ask 依 30 秒期限另行到期）；請記下來再做一次 |
| 6 | 按「開始：主商品，Ask」，場次完成後按該行的「取消 task show-a-NN」 | 「task show-a-NN 已取消」，該行「task 現已取消」。<br>• 30 秒內取消：Timeline 為 `APPROVAL_WITHDRAWN`、`TASK_CANCELLED`，商店顯示撤回。<br>• 已到期後取消：Timeline 為 `APPROVAL_EXPIRED`、`TASK_CANCELLED`，商店顯示到期 | 回報 |
| 7 | 步驟 3 過 30 秒後看 show-a-01 那一行；再按「以 consumer-a 查看」，輸入 show-a-01 開啟 | 該行「Ask 已到期；task 仍可由主持人取消」；商店「Ask 已到期（後端已記錄）：離店」；Timeline #4 `APPROVAL_EXPIRED` | 回報 |
| 8 | 按「歷史展示回放（唯讀）」 | 出現回放說明；商店重演 #1–#4；結束後回到最新狀態；事件數不變 | 回報 |
| 9 | 按「以 consumer-b 查看」 | 畫面不留 consumer-a 的 task、商店或面板內容 | 回報 |
| 10 | 終端機按 Ctrl-C，再執行同一個命令（同一個資料庫） | 頁面顯示憑證已失效；重新載入 `var/demo-tokens.json`（已是新 token）後，最近場次保留，沒有任何場次重跑，各行文字與重啟前相同 | 回報；不刪資料庫 |
| 11 | （可選）按 show-a-01 的「取消 task」 | 「task show-a-01 已取消」，該行「task 現已取消」 | 回報 |

**任何時候出現以下情況，就停止並回報**（附截圖、時間與 task ID，不附 token 檔）：
- 頁面出現付款成功、交付成功字樣；
- 畫面出現 token；
- 停止或取消後，商店與 Timeline 不一致；
- 重啟後有場次重跑。

結果：**待 Aaron 執行**。

### 16.4 驗證（Claude Code 執行，待 Codex 獨立 review；全部在 `667e933`）

| 項目 | 命令 | exit | 時間 | 證據 |
|---|---|---|---|---|
| `make check` | | 0 | 50 秒 | `artifacts/b1-06/checks-667e933d0e25-20261006T085130Z/` |
| `make check-contracts`（Node 34） | | 0 | 2 秒 | 同上 |
| race | `go test -race -count=1 ./cmd/demo ./internal/store` | 0 | 225 秒 | 同上 |
| Unity 建置 | 未重建；4 個檔案的 hash 與 `843ca4a`／`d61771c` 證據相同 | — | — | 同上 `webgl-loaded.sha256` |
| 真實瀏覽器 247 項（本輪新增 45 項：回放順序 3、3.5 段落 35、截圖 7） | `node scripts/verify/browser-ui.mjs` | 0 | 311 秒 | `artifacts/b1-06/ui-667e933d0e25-20261006T085612Z/` |
| 回放時序反向 | 同上，只改一行後還原 | 1（預期） | 301 秒 | `artifacts/b1-06/reversal-667e933d0e25/` |
| 完整斷網容器與全部突變 | `sh scripts/verify/container-verify.sh 667e933…` | 0 | 812 秒 | `artifacts/verify-container/runs/667e933d0e25-20261006T090222Z/`（`artifacts/b1-06/container-667e933.log`）：七步全過（check-contracts 31、check 92、race-cover 274、verify-b1-01 43、mutation 342 秒），adapter 11／11、hostcreds 23／23，**247／247 突變**，17 項隔離 ok；與瀏覽器循序執行，主機上沒有同時執行瀏覽器或 Unity |

- **突變**：本輪沒有新增或調整，仍是 247 項。本輪只改測試、驗收資料與文件，沒有新的產品程式分支需要突變。
- **開發中的執行**（工作區，未提交，不算證據）：
  - 回放修正後第一次完整瀏覽器 205 PASS；
  - 加入 3.5 段落後 247 PASS。

### 16.5 失敗嘗試（保留）

- **容器第一次執行**（`667e933`）：背景執行時，`docker image inspect` 回「No such image: apa-verify:b1-05」，沒有執行任何步驟。證據：`artifacts/b1-06/container-667e933-attempt1-no-image.log`；log 中的 `elapsed` 數值無意義，因為計時在失敗前尚未開始。
  - 前景立即確認：映像存在（`sha256:78258368…`），兩個 Docker context 指向同一個 daemon。
  - 推測：當時有其他程序在變動映像（daemon 的映像數由 36 變成 32），屬暫時性，未證實。
  - 那次 `container-verify.sh` 本身也在映像檢查時停止，留下近乎空的目錄 `artifacts/verify-container/runs/667e933d0e25-20261006T090131Z/`；保留，不是通過紀錄。
  - 映像、限制與命令都不變，重跑一次即通過（16.4）。
- **既有回放檢查**：3.4 反向中失敗過一次，原因與修正見 16.1。

### 16.6 未做與未測

- **人工操作驗收**：待 Aaron 執行（16.3）。
- **CLI 逾時的瀏覽器畫面**：只有 3.4 的種子資料庫（`ask-wait` 等 3 分鐘才逾時，自動化流程不等）。
- **異常終止（kill -9）**：自動化流程沒有做；中斷場次由 3.4 的種子資料庫與 Go 測試涵蓋。整體流程的重啟是正常關閉。
- **其他**：
  - 只用 Chrome；
  - 沒有確認對話框；
  - 沒有執行真實 CLI、付款、testnet、Signer、ZK；
  - 既有展示資料庫沒有被操作。

### 16.7 review 後補正：人工清單與截圖（Claude Code 執行，待 Codex 獨立 review；測試 `31cc6c7`）

依據：Codex 對 `667e933`／`824308f` 的 review。3.5 自動化部分可以接受，沒有發現新的產品程式缺陷。`scenarios-acceptance.json` 保留為獨立驗收資料，不併入預設情境目錄。人工驗收前，需要補正下列三項。

1. **期限判斷**（16.3 步驟 5、6）：
   - 原本一律寫「撤回」。現在區分 Ask 期限前（`APPROVAL_WITHDRAWN` → `TASK_CANCELLED`）與已到期後（保留 `APPROVAL_EXPIRED`，再記 `TASK_CANCELLED`）。
   - 寫明 30 秒是 Ask 期限、3 分鐘是 CLI 上限。例如第 60 秒才按停止，出現「到期＋取消」也是正確結果。
2. **啟動命令**：先 `cd` 到 repo，兩個 `go build` 與啟動串成同一條 `&&` 成功鏈。建置失敗時不會啟動舊執行檔。
3. **截圖**：
   - 原本 7 張 `desktop-acceptance-*` 主要是上方控制區，保留不刪。
   - 每張另補一張全頁的 `full-acceptance-*`：測試時把 viewport 暫時調成頁面高度後拍攝，所以 Unity 畫面、Inspector、Timeline 都在圖內；拍完恢復 1280×900。
   - 沒有改版面，也沒有重建 Unity。
   - 每張全頁截圖都在 `browser-test.log` 記下當時 Unity 回報（task、序號、場景）、狀態列與 Timeline 序號，可以和事件對照。例如 `full-acceptance-stop.png`：「Unity 已呈現 show-a-02 #5 withdrawn」，Timeline 1–5；`full-acceptance-expired.png`：「show-a-01 #4 expired」，Timeline 1–4。

| 項目 | 命令 | exit | 時間 | 證據 |
|---|---|---|---|---|
| 真實瀏覽器 254 項（新增 7 項：全頁截圖前的 token 欄位檢查） | `node scripts/verify/browser-ui.mjs`，在 `31cc6c7` 執行 | 0 | 307 秒 | `artifacts/b1-06/ui-31cc6c75bc79-20261006T102131Z/`（`full-acceptance-*.png` 7 張） |

- `31cc6c7` 只改 `scripts/verify/browser-ui.mjs`（測試），產品程式與 `667e933` 相同。
- 依 review 指示，沒有重跑完整容器、全部突變或 Unity 建置。16.4 的容器與 247 項突變結果屬於 `667e933`。
- **觀察（已由 16.8 修正）**：`full-acceptance-stop.png` 中，最近場次該行已是「已停止，取消已記錄」，但其上的狀態列仍是按下停止時的「停止中…（等收尾紀錄寫入後才顯示已停止）」。這是 3.2 起的既有行為：停止的提示不會回頭更新，實際結果以最近場次為準。人工驗收時可能會覺得前後不一致，所以先記下來。本輪沒有改產品程式。

人工驗收仍**待 Aaron 執行**。

### 16.8 review 後修正：停止的 202 不再留下「停止中」（Claude Code 執行，待 Codex 獨立 review；程式與測試 `d18efa2`）

依據：Codex 對 `31cc6c7`／`1f491d5` 的 review。三項補正（16.7）已關閉；停止後「停止中」一直殘留，是實際的畫面矛盾，需在人工驗收前做最小修正。

- **修正**（`web/host.js` 的 `stop()`）：
  - 請求送出、還沒回覆時，仍顯示「停止中…」。
  - 收到 202 後，改為中性的受理通知「場次 〈run ID〉 的停止請求已受理；最終結果請看最近場次。」，狀態為 `idle`，不再是 `busy`。
  - 不宣稱已停止或已取消，最終結果仍由最近場次的已提交紀錄呈現。
  - 世代保護與錯誤處理不變，沒有新增輪詢，也沒有改 Store。
- **瀏覽器回歸**（3.5 段落）：按停止、收到 202 之後，最近場次該行為「已停止，取消已記錄」。上方文字必須正好是那句受理通知，`data-state` 為 `idle`，不含「停止中」「已停止」「已取消」「取消已記錄」。
- **截圖**：補拍 `full-acceptance-stop.png`。上方是受理通知，該行為「已停止，取消已記錄」，Unity、Inspector、Timeline 一致（show-a-02 #5 withdrawn）。
- 人工清單 16.3 步驟 5 已同步改寫預期畫面。

| 項目 | 命令 | exit | 時間 | 證據 |
|---|---|---|---|---|
| Node 34 項、語法檢查 | `node --test web/test/adapter.test.mjs web/test/hostcreds.test.mjs`；`node --check` | 0 | 1 秒內 | （命令輸出，未另存目錄） |
| 真實瀏覽器 255 項（新增 1 項） | `node scripts/verify/browser-ui.mjs`，在 `d18efa2` 執行 | 0 | 305 秒 | `artifacts/b1-06/ui-d18efa270912-20261006T103714Z/` |

- 依 review 指示，沒有重跑完整容器、全部突變或 Unity 建置。
- 容器 247／247 屬 `667e933`。`d18efa2` 改了 `web/host.js`（產品前端），沒有完整容器結果。
- 人工驗收仍**待 Aaron 執行**。
