# B1-06 展示操作流程設計（fake 展示模式）

狀態（2026-10-05）：**設計提案，待 Codex review；尚未核准實作。** 依據 vault 筆記 `Claude-Code-B1-06-展示操作流程整合設計指示-20261005`。分支 `b1-06-demo-integration`，設計基準 `36f3b90`（程式基準仍是已驗收的 `6aee7f0`）。

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
  -> 主持人載入本機 token 檔（只在頁面記憶體）
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
6. 成功時印出網址（`http://127.0.0.1:8402/`）與 token 檔路徑；**不印 token**。

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
| 開始 | `POST /api/host/runs`，body `{"scenario":"ask"}` | 檢查沒有進行中的場次 → 從 demo registry 取該 consumer 下一個未在 Store 的 task（第 5 節）→ 在背景 goroutine 呼叫現有 `fakeRunner.runOne`（同一個 Store、bridge、限制） | `202 {"run_id","task_id","consumer_id","state":"running"}` |
| 停止 | `POST /api/host/runs/{run_id}/stop` | 取消該場的 context；現有 buyer host 會結束 fake 程序群組並記錄 `TASK_CANCELLED`／`HOST_CANCELLED`（未決 Ask 依現有規則撤回或到期） | `202 {"state":"stopping"}` |
| 查狀態 | `GET /api/host/runs/{run_id}`（頁面每秒輪詢，與 Dashboard 同節奏） | 讀記憶體中的場次紀錄 | `running`／`stopping`／`ended`，加上現有 `runOutcome` 的固定代碼 |
| 開始下一場 | 同「開始」 | 上一場 `ended` 後才接受 | 新 run、新 task |

**防重複**：

- host 同時最多一場（PRD：一次啟動一位的 task）。有場次在 `running` 或 `stopping` 時，「開始」回 `409 {"error":"run_active","run_id":…}`，頁面顯示「上一場尚未結束」並帶到那一場。連點兩次只會建立一場。
- 「開始」與 task 配發在同一把 host 鎖內完成；Store 的 `StartTask` 本身也拒絕重複（`TASK_ALREADY_STARTED`），是第二層保護。
- 按下後按鈕立即停用，直到收到回覆。

**停止的完成條件**：

- 狀態先變 `stopping`。只有 `runOne` 返回（buyer host 已記錄結束事件、程序群組已回收）才變 `ended`。頁面在那之前不顯示「已停止」。
- 停止後的結束事件照常進 Store，Dashboard 由輪詢看到 `TASK_CANCELLED`。

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
- host 配發 task 的規則：該 consumer 依編號第一個不在 Store 的展示 task（沿用 `runOne` 已有的 `st.Task` 檢查）。已結束的 task 永遠不重跑。
- `ponytail:` 上限是每位 20 場。用完時開始鈕回 `409 {"error":"no_free_task"}`，頁面顯示「展示 task 已用完：請設定新的 `APA_DB_PATH` 後重啟，或擴充 demo registry」。不刪資料庫。日後若真的不夠，再改為 host 依需要產生 task 綁定，那會動到 registry 的信任模型，要另外設計。

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

**角色分開**（token 仍在啟動時產生、寫入 0600 檔、不進 log）：

| 角色 | 現在 | 本設計 |
|---|---|---|
| viewer（每個 consumer 一個） | 讀自己 consumer 的 task | **不變**；新增可讀自己的 `GET /api/tasks` 清單 |
| host | 讀全部 task（唯讀） | **不變**；新增可讀 `GET /api/host/status` |
| operator（新） | — | 只能呼叫 `/api/host/runs*` 控制端點與 `/api/host/status`；**不能讀任何 task 的內容**（task 內容仍走 viewer） |

viewer token 不擴大成控制權限。host token 也保持唯讀，以免改變交付點 1 已驗收的「主持人唯讀」語意。

**控制端點的請求防護**（沿用 `authorize` 的順序，再加三項）：

- Host 必須是綁定位址；Origin 必須等於自己或不存在；只認 `Authorization: Bearer`（現有）。
- **POST 必須帶 `Origin`，且等於 `http://<綁定位址>`**（瀏覽器的 POST 一定會送；缺少就拒絕），擋住跨站表單。
- `Content-Type` 必須是 `application/json`。CORS 不開，跨站 JSON POST 需要預檢，所以會失敗。
- body 上限 1 KiB，嚴格解碼（`DisallowUnknownFields`），只接受情境 `id`。

錯誤一律回固定代碼：`unauthenticated`、`forbidden_origin`、`run_active`、`no_free_task`、`unknown_scenario`、`not_found`、`store_unavailable`。

**減少複製 token 的本機方案**（不需新 API，不改伺服器認證）：

- 頁面加一個「載入本機 token 檔」按鈕，是原生的 `<input type="file">`。
- 主持人選 `var/demo-tokens.json` 後，頁面在記憶體讀出 operator token 與各 consumer 的 viewer token：
  - 不寫入 localStorage、sessionStorage、cookie 或 URL；
  - 不送進 Unity；
  - 不寫 console；
  - 檔案內容讀完即丟。
- 重新整理頁面就要重新選檔（與現在手動貼 token 的規則一致）。
- 開始一場後，頁面用**該場 consumer 的 viewer token** 切換 viewer 並開啟新 task。Dashboard 對伺服器而言仍是一次一位 viewer，A／B 隔離與 404 規則不變。頁面持有多個 token 不等於合併身分，每個請求只帶一個。
- 手動貼 token 的路徑保留（例如只想以 B 的身分查看）。

考慮過但不採用：

- 一次性配對碼換 session：要新增登入交換端點與 session 狀態，且 cookie 被既有規則排除。
- 用 `pbcopy` 寫入剪貼簿：剪貼簿其他程式也讀得到。
- 把 token 放進啟動時打開的網址：違反「token 不放 URL」。

## 7 錯誤呈現（§4.5）

| 情況 | 偵測 | 主持人畫面 | 主持人可以做 | 需要新 task／run？ |
|---|---|---|---|---|
| 啟動失敗（埠、鎖、fake CLI、Store 恢復） | host 啟動檢查（現有） | 終端機原樣顯示原因，exit 非 0；沒有頁面 | 依訊息處理（停掉佔埠程序、換 `APA_DB_PATH`），再 `make demo` | — |
| Unity 建置缺少或載入失敗 | 就緒面板、頁面 Unity 狀態（現有） | 「Unity 場景未就緒」；Dashboard 仍可用 | 首次準備後重新整理頁面；可照常開始，但畫面不演商店 | 否 |
| Agent（fake CLI）逾時 | `runOutcome` 的 timeout 與 Store `TASK_TIMED_OUT`／`CLI_TIMEOUT`（現有） | 場次「CLI 逾時」；task 顯示逾時；Ask 若已成立仍等待到期 | 開始下一場 | 是，下一場用新 task；不重跑原 task |
| Store 寫入失敗 | `runOutcome` 的 `prepare_failed:代碼` 或 `store_end_error`（現有），讀取回 503 `store_unavailable` | 場次「Store 寫入失敗（代碼）」；不顯示成功 | 若可重試：開始下一場（新 task）。若 Store 不可用：停止 host、看 stderr，不自動修復 | 是 |
| 場次 host 錯誤 | `runOutcome.Detail = host_error` | 「場次錯誤（代碼）」 | 開始下一場 | 是 |
| 主持人停止 | 第 4 節 | 「停止中…」→「已停止」 | 開始下一場 | 是 |
| host 被關閉時有進行中的場次 | 現有關閉順序記錄 `HOST_CANCELLED` | 重啟後該 task 顯示已取消 | 開始下一場 | 是 |
| host 異常終止（kill -9、當機）時有進行中的場次 | 重啟後 Store 中狀態仍是 `created`／`running`（尚未有裁決）、沒有 `TASK_CANCELLED`／`TASK_TIMED_OUT`，且 host 沒有對應場次的 task。已到 Ask 的 task 照常由到期規則收尾，Deny 已是終態，所以只看這兩種狀態 | 列為「上次執行未正常結束」 | 按「標記取消」：`POST /api/host/tasks/{task_id}/cancel` → 現有 `Store.Cancel`（`TASK_CANCELLED`） | 是，之後開始下一場 |

**一律不做**：

- 不默默重跑整場；
- 不自動切到真實 CLI；
- 不把失敗、停止或逾時顯示成成功或付款；
- 不刪資料庫。

## 8 實作切片（B1-06 交付點 3）

沿用 B1-06 編號：交付點 1（Store 接線與唯讀 HTTP，已驗收）、交付點 2（Unity＋LimeZu 與 Dashboard，已驗收），本批為**交付點 3：展示操作流程**，拆成 3.1–3.5。

| 切片 | 修改的檔案與元件 | 使用者看到的結果 | 驗收測試 | 相依 |
|---|---|---|---|---|
| **3.1 一鍵啟動與就緒** | `Makefile`（`demo`）、`scripts/demo/start.sh`；`cmd/demo`：`GET /api/host/status`（host 角色、唯讀）、記住最後一筆到期錯誤；`web/`：就緒面板、「載入本機 token 檔」 | 一個指令啟動；頁面列出 HTTP／Store／fake CLI／Unity 建置／LimeZu 是否就緒；選一次檔案就能以 A 或 B 查看，不用複製 token | 腳本：缺建置、缺匯入、埠佔用、鎖住時的訊息與 exit code；HTTP：status 的權限（viewer 403、無 token 401、Host／Origin）、內容不含 token 或路徑以外的檔案資訊；瀏覽器：選檔後 storage／cookie／URL／console 都沒有 token，A／B 隔離不變 | 無（不需新控制 API 或新角色） |
| **3.2 主持人控制及權限** | `cmd/demo`：operator 角色、`scenarios.json` 載入、`POST /api/host/runs`、`…/stop`、`GET …/{run_id}`、單場鎖；`testdata/demo/scenarios.json`、`testdata/demo/registry.json`；`web/`：主持人面板 | 選情境按「開始」，fake 客人進店到 Ask 或 Deny；連點不重複；可停止 | HTTP：每個端點的權限（viewer／host 不能控制、operator 不能讀 task）、Origin／Content-Type／body 防護、409 `run_active`；整合：開始 → Store 有新 task、事件序列與 `ask`／`deny-budget` 案例一致；停止等記錄完成才 `ended`；demo registry 與契約 fixture 一致性測試 | 3.1 |
| **3.3 新一場 task 與 UI 選擇** | Store 唯讀清單查詢；`GET /api/tasks`；`web/adapter.js` 移除 `FIXTURE_TASKS`；開始後自動切 viewer 並開啟新 task | 每場是新 task；清單顯示該 consumer 的所有場次；舊場次可回放 | HTTP：viewer 只看到自己的 task（含 registry 移除後的舊 task 規則）；瀏覽器：連續兩場是兩個 task、前一場仍在清單且可回放；回放不寫資料 | 3.2 |
| **3.4 停止、錯誤、重啟恢復** | `cmd/demo`：場次錯誤代碼對應、未正常結束 task 的偵測與 `POST /api/host/tasks/{id}/cancel`；`web/`：第 7 節的錯誤文字 | 錯誤如實顯示與可採取的操作；重啟後不重跑，未結束的可標記取消 | 整合：fake 逾時、Store 寫入失敗（測試替身）、host 關閉與異常終止後重啟；每種都不出現成功字樣；取消沿用 Store 規則 | 3.2 |
| **3.5 實際瀏覽器操作驗收** | `scripts/verify/browser-ui.mjs` 新增操作流程段 | 一次跑完主持人實際會做的操作 | 真 Unity Web 建置：`make demo` 等同的啟動 → 選檔 → Ask 一場 → Deny 一場 → 中途停止一場 → 連點開始 → 重啟後清單與狀態；截圖；證據不含 token | 3.1–3.4（每個切片也各自補自己的瀏覽器檢查，3.5 是整體串接） |

**本輪、後續與 B1-06 完成條件的分界**：

- **本輪設計交付**：本文件與任務拆分，交 Codex review。不含任何程式。
- **後續實作交付**：3.1–3.5，各自實作、測試、review。每個切片要另行核准才開工。
- **B1-06 整體完成**：仍依任務卡，包含至少一次核准的 opt-in 真實 CLI trace、`tests/m1/`、`docs/m1-result.md` 與乾淨目錄重跑。交付點 3 全部通過也不代表 B1-06 或 M1 完成。

**建議的首批切片：3.1**。理由：

- 直接解決 Aaron 操作時的兩個痛點：逐一啟動、反覆找 token。
- **不需要新控制 API、新角色或認證變更**：status 用現有 host 唯讀 token，token 檔載入只在頁面端。風險與 review 範圍最小。
- 3.2–3.5 都建立在就緒面板與 token 載入上。
- 3.1 完成後，主持人仍沿用現有自動計畫（`make demo` 可先帶 `--plan`）做展示，等 3.2 核准再換成按鈕控制。

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
