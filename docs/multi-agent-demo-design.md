# 雙 Agent 商店：M-A 最小設計與範圍修訂提案

> 2026-10-11，Claude Code（M-A）。初版 `79000a2`（基準 HEAD `d3b7c53`，M1 已於 10/11 結案）。
> **狀態（10/11）：M-A 的 R1～R4 已通過 Codex 限定設計複核並關閉**（文件、Git 與相關原始碼的唯讀核對，沒有執行測試或真實 CLI）。**現行（10/12）：M-B fake 技術 review 通過，R1／R2／C1 關閉（第 14.4 節）。M-C 已依 Aaron 核准執行一次（Aaron 操作）：兩位真實 Agent 的工具呼叫判定為交錯；停止隔離未示範（第 15 節；Claude Code 唯讀核對，待 Codex 核對）。**歷程：Aaron 核准 M-B 的 fake 實作與定向驗證；M-B 實作（第 14 節）；10/12 Codex 限定 review 提出 R1、R2、C1，暫不通過；補正（第 14.3 節）通過限定複核。補正對照見第 13 節。
> 只讀程式與文件寫成，沒有改產品、沒有跑測試或展示。M1 與 B1-00～B1-07 維持 Done，本文件的工作是後續切片，不以 M1 的 PASS 代替並行驗收。
> 方向（Aaron 已接受）：先讓兩位不同 consumer 的 Agent 在同一間商店有重疊的工作流程，各自的資料與場景不串，之後再接 Admin 營運視角。容量原則（Aaron 最新）：同時執行的 Agent 數是部署設定，本輪預設與驗收規模為 2，不是產品永久上限。下面的生命週期、橋接格式與權限方案是本文件的**推薦**，不是 Aaron 已逐項核准的細節。

## 0 推薦方案（一段話）

Host 從「全域同時一場」改成「**每位 consumer 最多一場**，總數不超過部署設定的**同時執行上限**（`--max-runs`，預設 2）」；`stopping` 未收尾前仍占名額。主持人頁每位 consumer 一列，各有自己的「開始」與「停止」，另有一個「一起開始」便利鍵（送多個獨立的開始請求）。Unity 橋接升到 **v2**：同一個場景裡每位客人有自己的欄位世代、task 與序號，同時作用；回報要與該角色最後送出的投影六項全部一致才算完成；舊的 v1 單客人路徑保留給 `/` viewer 頁與歷史回放。主持人頁的選取與「查看較早的 task」只換面板，**場景永遠跟隨每位 consumer 最新場次的 task**。真實工作重疊以 Host 在事發時、以單調時間記下的場次與工具呼叫時點判定，寫成獨立的時間紀錄檔；M-B 以 fake 完成紀錄與唯讀判定工具，M-C 只用已 review 的工具核對真實證據。不改 M1 的 trace 與檢查器。

## 1 範圍修訂對照

現行規則都寫明「一次一個 active task／同時一場」。下表是出處、建議的新規則與影響；**review 通過後**才同步到 PRD、執行計畫與任務清單，本輪不改這些正式文件。

| 出處 | 現行規則（摘要） | 建議的新規則 | 影響 |
|---|---|---|---|
| PRD-第一期 §3.2 消費者、Agent 與錢包（9/27） | 依序重用同一間商店；一次只需一個 active task，不新增大量 NPC 或多模型並行 | 每位 consumer 一次最多一個執行中的場次；不同 consumer 的場次可重疊，同時執行上限由部署設定決定（本輪預設與驗收 2）；仍不新增大量 NPC | 正式規則文字 |
| PRD-第一期 §5「像素客人的消費流程」 | 兩個 profile 依序操作，一次一個主線 task | 多位客人可同時在店內，各自依自己的 task 事件呈現；本輪畫面支援兩位 | 同上 |
| 執行計畫 §1-novies（9/27 定案）、§4（9/27 錢包更新）、§9.2 循序圖 | 一個 active task；主線一次啟動其中一位 | 同 PRD；§9.2 增補雙場循序 | 同上 |
| 第一批任務清單 §1、B1-06 卡 | 一次只跑其中一個 task；一次啟動一位 | **不改**：M1 已結案，保留為 M1 當時的規則；新規則寫在後續任務卡 | 無 |
| `docs/m1-boundaries.md`「誰可以啟動 task」 | 一次只有一個 active task，A 與 B 依序操作 | 加註：M1 範圍如此；多場見本文件 | 文件 |
| `docs/demo-operation-design.md` §4、§6.1、§13.3 | 有場次 `running`／`stopping` 時 `409 run_active` | 每位 consumer 一場、總數不超過有效上限；錯誤碼分開（第 3.3 節） | `cmd/demo` |
| `docs/demo-ui.md` §1、§3 | 只有目前 viewer 的客人會移動；同一世代只能有一個 task | v2：每位客人各自的欄位世代與 task；v1 不變 | `web/`、Unity |
| `docs/demo-presentation-design.md` §2、§4.4 | 另一位客人調暗、不移動；切換 consumer 時重設選取 | v2 每位都完整亮度；選取與查看舊 task 只換面板 | `web/`、Unity |
| `docs/cli-poc.md` §55.1 | live 目錄只有 `live-a`；同時一場 | live 目錄加 `live-b`；同時執行數不超過有效上限 | 可信設定 |
| `docs/commercial-demo-roadmap.md` §0、§6.3、§6.8 | 一次一個 active task、Host 同時一場；舊 M-B 含 Admin 總覽 | M-B 收斂為多場 Host、雙角色與選取；Admin 後接（第 9 節） | 路線文件 |
| `docs/m1-result.md` §8、各處「最多 20 個付款帳號」 | 寫成後續要求的固定數字 | 「約 20 個付款帳號」是先前估計的展示規模，不是產品上限（第 3.1 節） | 文件用語 |

## 2 要交付的體驗（M-B 完成時）

1. 主持人頁每位 consumer 一列，各有「開始」「停止」；另有「一起開始」。不需要切換憑證或手動選 task 才能看見另一位。
2. 同一間 Unity 商店同時呈現兩位客人，各自依自己 task 的已提交事件進場、報價、等待或離場；不是把同一筆請購複製成兩個 NPC。
3. 主持人選 A 或 B，請購卡、Timeline、Inspector 與白名單 trace 跟著換；也可以查看某位的較早 task。這些都只換面板，不重啟場次，場景不變。Unity 內點選角色屬 D3，後移，不阻擋本片。
4. 可以只停止 A，B 繼續；A 的取消、到期、錯誤或慢 trace 都不拖住 B。
5. 啟動服務、配對、刷新、讀取模式資訊都不會自動執行模型。保留已驗收的簡化入口，以及 fake／live／開發展示例外／未付款的標示；啟動時顯示有效的同時執行上限。

## 3 場次與生命週期

### 3.1 三種數量分開

| 數量 | 意思 | 本輪 | 不代表 |
|---|---|---|---|
| 可管理的 consumer／付款帳號數 | registry 裡的 consumer 與各自的錢包參照 | 2（consumer-a、consumer-b） | 「約 20 個付款帳號」是先前估計的展示規模，不是產品上限；本輪不擴增 |
| 同時執行的 Agent 數 | 同時處於 `running`／`stopping` 的場次數 | 部署設定 `--max-runs`，預設 2，驗收 2 | 調高數值不代表該容量已實測，也不代表畫面能呈現同樣多的角色 |
| 同時付款數 | 同時進行的付款 | 0（M1 與本片都沒有付款） | 是否並行付款，隨付款整合另行設計 |

- 每位 consumer 預置的 20 個 `show-*` task 是**目前的測試資料池**，與帳號數、同時執行數都不同；本輪不擴增資料池，也不做壓力測試。
- 資料鍵與場次管理都以 consumer／run／task 定位（進行中場次以 `consumer_id` 為鍵、畫面欄位以 `consumer_id` 為鍵），不以 A／B 索引當永久架構。首輪只驗兩位，不要求 Unity 立即支援任意人數。

### 3.2 同時執行上限（部署設定）

- **設定方式**：`cmd/demo` 新增旗標 `--max-runs N`，只在有場次目錄（`--scenarios`）時有效；`scripts/demo/*.sh` 不帶時用預設 2。不做自動容量探測、排隊或排程。
- **啟動時驗證**（非法就拒絕啟動，結束碼非 0，訊息寫明原因與允許範圍）：
  - 必須是整數；
  - `1 ≤ N ≤ 場次目錄裡的 consumer 數`（每位 consumer 最多一場，超過這個數字沒有意義）；
  - 沒有 `--scenarios` 卻給了 `--max-runs`：拒絕（沒有主持人場次）。
- **顯示有效值**：啟動訊息寫出有效上限與 consumer 數；`/api/host/status` 增加 `capacity: {"max_runs": N, "consumers": M}`，主持人頁的就緒面板顯示「同時執行上限 N（部署設定）」。
- `N = 1` 等於現行的單場行為，可作為退回設定。
- **畫面能力**：Unity v2 在 `bridge` 回報中宣告它能呈現的客人（目前是 consumer-a、consumer-b 兩位，第 5.2 節）。有效上限或場次目錄的 consumer 超過畫面能呈現的範圍時，主持人頁明示「此場景最多同時呈現 {k} 位：{名單}；其他場次只在場次列表與面板中」，場次列表永遠列出全部場次，不靜默遺漏。本片不需要讓後端拒絕這種組合，只要求不靜默少顯示。

### 3.3 名額規則

- **每位 consumer**：最多一場處於 `running` 或 `stopping`。
- **總數**：最多 N 場（有效上限）處於 `running` 或 `stopping`。`stopping` 尚未收尾時仍占名額。
- 名額只看**程序狀態**（記憶體中的 `running`／`stopping`），不看 task 狀態。這與現行「`host_runs` 是持久紀錄，程序狀態只在記憶體」的分離一致（demo-operation-design §13.5）。
- 超過有效名額明確拒絕；本片不實作排隊。

### 3.4 兩層狀態分開

| 層 | 狀態 | 來源 | 說明 |
|---|---|---|---|
| 場次程序 | `running`、`stopping`、`ended` | 記憶體（`liveRun`） | 只有 `running`／`stopping` 占名額 |
| 場次紀錄 | `accepted`、`ended`、`interrupted` | `host_runs` | 重啟時 `accepted` 一律改 `interrupted`（現行） |
| task | `running`、`awaiting_approval`、`denied`、`cancelled`、`failed` | Store 投影 | `APPROVAL_EXPIRED` 是請購的核准狀態，不是 task 狀態 |

**推薦規則**：場次結束後，該 consumer 的 Ask 若仍 `awaiting_approval`，**允許**同一位 consumer 開始新場次。理由：現行行為本來就是如此（只檢查進行中的場次）；等待中的 Ask 是在等真人，不是 Agent 在工作；新場次照 M1 規則配發新 task、沿用同一個錢包參照。
- 場景中每位 consumer 只有**一個角色**，代表該 consumer **最新場次**的 task（第 5.4 節的「場景 task」）。較早仍在等待的 Ask 不另外生成角色，只出現在該 consumer 的 task 列表，標示「較早的 task：等待核准」，可以在面板查看。
- 「一場」是場次程序，「一個 task」是 Store 的紀錄；兩者分開稱呼，不再用「目前請購」同時指兩件事。

### 3.5 開始、重送與停止

開始仍是 `POST /api/host/runs {"request_id","scenario"}`（operator、同 Origin、嚴格 JSON，現行）。在 `c.mu` 下依序檢查：

| 順序 | 情況 | 回覆 |
|---|---|---|
| 1 | request ID 已存在、情境相同 | `200` 同一場（即使名額已滿或已結束，現行） |
| 2 | request ID 已存在、情境不同 | `409 request_id_conflict`（現行） |
| 3 | 情境不存在 | `400 unknown_scenario`（現行） |
| 4 | 該情境的 consumer 已有場次 `running`／`stopping` | `409 {"error":"consumer_busy","run":<runView>}`（取代該情況下的 `run_active`） |
| 5 | 已有 N 場 `running`／`stopping`（N 為有效上限） | `409 {"error":"capacity_full","max_runs":N,"runs":[<runView>…]}` |
| 6 | 該 consumer 沒有可用的 `show-*` task | `409 no_free_task`（現行） |
| 7 | Store 錯誤 | `503 store_unavailable`（現行） |
| 8 | 受理 | `202 <runView>` |

- request ID 仍是全域唯一（`host_runs.request_id UNIQUE`），先於名額檢查，所以重送永遠拿到同一場，不會重複執行。
- **停止** `POST /api/host/runs/{id}/stop`：只作用於該 run ID（現行即以 ID 查詢，改為在每位 consumer 的進行中場次裡查）。受理時記下停止受理的時點（第 6.2 節）。`stopping` 再停止回 `202`（現行）；已結束回 `409 run_not_running`（現行）。
- **取消 task** `POST /api/host/tasks/{task_id}/cancel`：該 task 的場次仍 `running`／`stopping` 時回 `409 run_active`（語意不變，改為依 task 查詢）。
- 「一起開始」是前端便利鍵：依序送多個獨立請求（各自的 request ID）。後端沒有「多場」請求；一個被拒（例如 `consumer_busy`、`capacity_full`）不影響其他，畫面分別顯示結果。

### 3.6 task 配發、持久化與恢復

- **配發**：沿用 `AcceptRun`，每位 consumer 從自己的 `show-*` 池取第一個未用的 task；不同 consumer 不搶同一個池。`host_runs.task_id UNIQUE` 與單一寫入連線（`BEGIN IMMEDIATE`）讓同時受理的請求序列化，不需要改 schema。
- **同時寫入**：各場的 `Submit`／`StartTask`／`EndRun` 由 Store 的單一寫入連線序列化（現行）。重試鍵、nonce、請購 ID 都含 task，各場不會衝突。
- **到期**：沿用每個 host 一個的 `RunExpiryReporting`，它本來就走所有 task；A 的 Ask 到期只寫 A 的事件。
- **重啟**：沿用 `InterruptOpenRuns`：所有 `accepted` 都改 `interrupted`，不自動重開模型，畫面各自標示中斷；不寫事件、不取消 task（現行）。
- **鎖**：`c.mu` 目前在受理、取消與場次收尾時跨 Store 寫入持有。多場時仍正確（寫入以毫秒計），只是一場收尾時其他場次的控制請求要排隊；本片不改鎖的粒度。

### 3.7 每場的資源隔離

- 已經是每場獨立的（保留）：
  - CLI 程序群組與停止（`Setpgid`、SIGTERM 後 SIGKILL）；
  - 暫存目錄 `apa-run-*`（工作目錄、plugin 目錄、`mcp.json`）與 MCP bridge 子程序；
  - capability：每場 32 位元組，`Bridge.Register` 註冊、場次結束 `Revoke`；共用的 `toolbridge.Server` 以 capability 的雜湊分開各場的 `Binding`；
  - trace 檔名 `<task>-<nanos>.jsonl`、usage 與錯誤紀錄。
- 要改的：`control.active`（單一指標）改成「以 `consumer_id` 為鍵的進行中場次」對照表。所有用到它的地方一起改：受理檢查、停止、`view`、場次收尾、`summary()`、`running(taskID)`（`live.go`）、`cancelTask`（`cancel.go`）。
- 停止 A 只取消 A 的 context：A 的程序群組收到訊號、A 的 capability 被撤銷；其他場次的 context、程序群組、capability 都不受影響。這是 M-B 的必測反例（第 10 節）。

### 3.8 上限與共享登入

- 每場的上限沿用：180 秒、10 則回覆、8 次工具呼叫、收尾寬限；同時執行數增加，**不提高**任何個別上限。
- 多個真實 CLI 共用同一個設定與登入目錄（`~/.apa-claude`，apa-buyer 每場自行設定）。同時使用可能遇到限流、設定檔鎖或 token 更新的競爭，這在 M-C 之前沒有實測過。處理方式：
  - 不自動重試模型；
  - 一場失敗只記在該場（`start_failed`、`failed` 等），其他場次照常；
  - 是否接受多個場次同時使用同一個本人訂閱，列為 M-C 的核准事項（第 11 節）。

## 4 身分與權限

- A、B 維持各自的 consumer、agent、task、wallet reference 與可信設定綁定；同一位的新 task 沿用錢包參照。不建立真實錢包或私鑰。
- 兩個 demo consumer **不是**兩個 Claude 訂閱帳號，也不代表已做真實身分驗證、真人委託或 grant。真實認證沿用既有核准方式，本片不重新設計登入。
- 讀取權限不新增：
  - 主持人頁讀 task 與事件時，**每個角色欄位使用該 consumer 自己的 viewer token**（`/host` 本來就持有各 consumer 的 viewer token，`host.js` 開頭的規則保留）。這樣即使前端把 A 的欄位指錯成 B 的 task，伺服器也會回 404，不會把 B 的資料顯示在 A 下；
  - 場次列表與狀態沿用 host／operator 的現行讀取權限；
  - viewer 仍只看自己的 consumer；operator 仍不能讀 task 內容。
- 不新增公開聚合或免驗證端點；token 與 capability 不進入投影、Unity 訊息、時間紀錄或 log（沿用現行掃描）。
- 新的讀取欄位只有兩個，都屬場次程序或部署狀態，host 與 operator 都可讀：
  - `runView.tool_calls`：該場目前已完成的工具呼叫**次數**（不含內容；只在 `live` 為 `running`／`stopping` 時出現），供主持人判斷 A 是否已在工作、何時可以示範停止（第 6.5 節）；
  - `status.capacity`：有效的同時執行上限與 consumer 數（第 3.2 節）。

## 5 雙角色投影與選取

### 5.1 名詞

| 名詞 | 意思 |
|---|---|
| 角色欄位（slot） | 每位 consumer 一個，穩定鍵是 `consumer_id`（不寫死 A／B；本片畫面支援兩位） |
| 欄位世代 `slot` | 該 consumer 的**場景 task** 換成新場次的 task，或欄位被清空時遞增；只影響這一位客人 |
| 場景世代 `scene` | 整頁換憑證、進出歷史回放、全頁重設時遞增；清空整個場景 |
| 場景 task | 該 consumer 最新場次的 task；場景只呈現它 |
| 查看 task | 主持人在面板正在看的 task；可以是場景 task，也可以是同一位的較早 task |
| 選取 | 主持人正在查看哪一位 consumer；只決定面板 |

### 5.2 橋接 v2

**Web → Unity**（`SendMessage("Bridge","Receive", json)`，現行通道）：

```json
{"v":2,"scene":7,"consumer":"consumer-a","slot":3,"task_id":"show-a-02","seq":3,"phase":"waiting","verdict":"Ask","reason":"HUMAN_APPROVAL_REQUIRED","animate":true,"cause_seq":3,"task_end":""}
{"v":2,"scene":7,"consumer":"consumer-b","slot":5,"clear":true}
{"v":2,"scene":8,"clear":true}
```

`task_end` 沿用 v1 的字串語意（`""`、`cancelled`、`timed_out`）。`clear` 沿用 v1 的布林欄位：不帶 `consumer` 是清空整個場景，帶 `consumer` 與 `slot` 是只清空那位客人（M-B 實作時由字串改為布林，見第 14 節）。

**Unity 的處理**（每位客人一份 v1 現有的狀態，外面包一層場景世代）：
- `scene` 比目前舊：Stale，丟棄；比目前新：清空**所有**客人，採用新的場景世代。
- 同一場景內，依 `consumer` 找到該客人的欄位：
  - `slot` 比目前舊：Stale；比目前新：只清空**這位**客人，採用新的欄位世代；
  - 同一欄位世代、不同 `task_id`：Invalid（一個欄位世代只有一個 task，取代現行「一個世代只有一個 task」）；
  - 序號倒退、重複：沿用 v1 的 Older／Duplicate 規則，**以該客人自己的序號判斷**，A 的序號不會推進或壓住 B。
- 不在 Unity 客人清單裡的 consumer：Invalid，不生成角色。
- v1 訊息照舊：單一客人語意（新世代清空全部、另一位調暗）。`/` viewer 頁與歷史回放繼續使用 v1。

**Unity → Web 回報**：
- `rendered`：`APA_Report("rendered", scene, seq, "consumer|slot|task|phase|verdict|reason")`。
- 新增 `bridge`：Unity 在 `ready` **之前**送 `APA_Report("bridge", 0, 0, "versions=1,2;customers=consumer-a,consumer-b")`，宣告支援的橋接版本與能呈現的客人。現行頁面不認得 `bridge` 這個種類，會忽略它，所以舊頁面配新 Unity 不受影響。
- Unity 只回報它實際收到並呈現的欄位；網頁推導的請購對應繼續標示為推導（現行）。

**完成判定（每個角色各自判定）**：
- 現行規則（`web/adapter.js` 的 `sameShown`）已經要求回報的世代、task、序號與階段，全部等於最後送出的投影。v2 保留這個精確比對，再加上角色與欄位的隔離：
- 對每個角色，只有回報的 **scene、consumer、slot、task、seq、phase 六項**全部等於「該角色最後送出的投影」，才標示「已呈現」並結束該角色的「補間中」。
- 以下都**不能**確認完成，也不能結束另一位或較新目標的等待：缺欄位或格式錯誤；任何一項不同（錯 consumer、slot、scene、task、phase）；較舊的序號；**比最後送出的序號更大**的序號（從沒送過的投影）。
- 同一條規則用在三個地方，不另留較寬鬆的成功條件：每位角色的狀態列、查證面板的「場景對應」、動畫完成判定（取代現行只比世代與序號的 `renderedBy`）。

**版本相容**（不容許靜默少顯示一位）：

| 頁面 | Unity | 行為 |
|---|---|---|
| 新 | 新（宣告 v2 與客人清單） | v2；清單內的客人同時呈現，清單外的依第 3.2 節明示 |
| 新 | 舊（沒有 `bridge` 回報） | 改用 v1，只呈現**選取中**的一位，並在場景上方顯示「此 Unity 建置只支援單一客人：場景只呈現選取的 {consumer}；其他位的資料仍在面板」 |
| 舊 | 新 | 舊頁面只送 v1，Unity 照 v1 單客人處理（現行行為） |

### 5.3 場景呈現

- v2 的每位客人都完整亮度（不再調暗另一位），各自走、停、離場；`ShopView` 已有 consumer-a、consumer-b 兩個客人物件，各自的目標、路線、期限與回呼，走路本來就逐位推進。
- 店內的共用點位（貨架、櫃台前、櫃台）加每位客人的固定偏移（例如 A 偏左、B 偏右），兩人同時到櫃台不重疊。不新增尋路、不加素材。
- 狀態列改成每位客人一行。
- 沿用 D2 規則：事件先行（只有已提交事件才觸發動作）、單次過場以真實時間計最長 2.5 秒、追上時加速；這些本來就逐位計算。兩位同時走路互不等待，一位的 2.5 秒上限不影響另一位。

### 5.4 主持人頁的資料、選取與查看

- **每個角色欄位一份狀態**（現行 `adapter` 的單一 state 拆成以 `consumer_id` 為鍵的多份）：場景 task、快照、已見事件、欄位世代、輪詢、送給 Unity 的最後投影與動作、最近的回報。
- **場景 task 只跟隨最新場次**：取自 `/api/host/runs`（每 2 秒）中該 consumer 最新的場次。新場次出現時，該欄位的世代加一，Unity 只清這一位、改呈現新 task。主持人的任何選取或查看都**不改**場景 task。
- **輪詢**：每個欄位各自每秒讀場景 task 與事件（現行的一致性重讀規則照舊），用該 consumer 的 viewer token。欄位之間互不等待：A 讀取失敗或逾時，只把 A 標成「讀取失敗」，B 照常更新。
- **選取 A／B**：只決定請購卡、Timeline、Inspector 與 trace 顯示哪一位；不呼叫 `forget()`、不清 Unity、不改任何欄位世代、不停止任何輪詢。
- **查看較早的 task**：主持人在該 consumer 的 task 列表點較早的 task 時，面板改讀那個 task（用同一個 viewer token，另有自己的查看世代與晚到防護）；**不送替代的場景投影、不增加欄位世代、不停止任何欄位的輪詢**。
  - 場景 task 與查看 task 分開保存。兩者不同時，面板頂端明示「正在查看較早的 task（show-a-01）；場景呈現的是 show-a-02」，並提供「回到場景這筆」；
  - 這是使用者的選擇，不標成同步失敗；查證面板的「場景對應」仍只比對場景 task。
- **請購卡標題**改為「正在查看：A 的請購（show-a-02）」，另有場景摘要「場景：A #3 等待核准 · B #2 報價中」，兩者分開。
- **晚到回覆**：每個欄位與查看各用自己的世代檢查；舊欄位世代、舊查看世代或舊場景世代的回覆一律丟棄。選取或查看的切換不使任何欄位的回覆失效。
- **刷新**：由 Store 還原每個欄位（各自最新場次的 task），首次載入不播動畫（現行 D2 規則逐位套用）；查看回到場景 task。刷新不會開始任何場次。
- **一位到期或離場**：只更新那一位；離場後角色回到店外，欄位保留到該 consumer 下一場開始。
- **trace**：「CLI 執行中」的判斷改為依 task 查（現行 `running(taskID)` 只看單一場次）；慢 trace 只影響該位的 trace 面板。

### 5.5 歷史回放

本片不升級為多場。進入回放時場景世代加一，暫停所有欄位的輪詢與查看，回放沿用 v1 單客人；離開時場景世代再加一，重新從 `/api/host/runs` 與 Store 取得每個欄位的最新資料。回放期間或之前發出的回覆，因世代不同一律丟棄，不能覆蓋恢復後的場景。單筆回放的既有行為與測試不得退化。

## 6 真實工作重疊的證據

### 6.1 現有紀錄夠不夠

| 紀錄 | 有的時間 | 不足之處 |
|---|---|---|
| trace（每場結束時寫出） | 只有 `run_start.at`（秒）；`tool_call` 沒有時間 | 無法量測 |
| Store 事件 `observed_at` | 秒，只有任務開始、請購、裁決等事件 | 太粗，也不涵蓋列目錄、查報價 |
| `host_runs` | 受理、結束、中斷時間（秒，牆鐘） | 沒有程序開始與回收、停止受理、單調時間或 Host 實例 |

結論：需要最小新增的**時間紀錄**。

### 6.2 時間紀錄檔（白名單）

場次結束時，Host 在 trace 旁另寫 `<task>-<nanos>.timing.json`。**不改 M1 的 trace 格式與檢查器**（M1 的 live 檢查器嚴格解碼 trace，新增欄位會被判為未知成員）；不保存模型原文或憑證；不需要簽章、外部錨點或串流平台。

```json
{
  "version": "apa.demo.timing.v1",
  "host_instance": "<Host 啟動時產生的 128 位元亂數>",
  "host_started_utc": "2026-10-11T08:00:00.000Z",
  "clock": "monotonic_ms_since_host_start",
  "run_id": "run-…", "task_id": "show-a-02", "consumer_id": "consumer-a", "mode": "live",
  "lifecycle": {
    "accepted_ms": 1200, "process_started_ms": 1210,
    "stop_accepted_ms": null, "stop_accepted_from": null,
    "process_reaped_ms": 12950, "run_ended_ms": 12990
  },
  "calls": [
    {"id": 1, "tool": "list_products", "product_id": "", "started_ms": 6020, "completed_ms": 6031, "outcome": "ok"},
    {"id": 2, "tool": "get_quote", "product_id": "kr-market", "started_ms": 8410, "completed_ms": 8450, "outcome": "ok"},
    {"id": 3, "tool": "request_purchase", "product_id": "kr-market", "started_ms": 10870, "completed_ms": null, "outcome": "interrupted"}
  ],
  "written_utc": "2026-10-11T08:00:13.100Z"
}
```

- **Host 實例**：`host_instance` 在每次 Host 啟動時產生，所有場次的紀錄都帶它；兩份紀錄同一個值，才是同一個 Host 生命週期。不能只因檔案在同一個目錄就認定相同。
- **時間基準**：`*_ms` 全部是「距 Host 啟動的單調時間」（Go 的 `time.Since(hostStart)` 使用單調時鐘），用來比較先後與區間；只有同一個 `host_instance` 的單調時間可以互相比較。UTC 時間只供閱讀，不用可跳動的牆鐘判定先後。
- **生命週期時點**：受理、程序開始、停止受理（以及受理當時的程序狀態）、程序回收、場次結束。停止受理與程序回收分開記錄；不用秒級 Store 事件代替。
- **工具呼叫**：每場內唯一的 `id`，`started_ms`（toolbridge 收到呼叫）與 `completed_ms`（回覆送出）配對，加上 `tool`、`product_id`、`outcome`；run／task／consumer 綁定在檔案層級（來自該場的 `Binding`）。中斷而沒有完成的呼叫，`completed_ms` 為 `null` 並標 `outcome:"interrupted"`，不補造完成時間。
- 時點都在**事發當下**記錄；`written_utc` 是檔案寫出的時間，兩者分開，不把寫出時間當成事發時間。

### 6.3 紀錄的完整性檢查

判定工具先檢查每一份紀錄，任何一項不成立就不往下判定重疊：

| 問題 | 判定 |
|---|---|
| 缺檔、缺必要欄位、版本不認得 | `incomplete`（證據不足） |
| 兩份紀錄的 `host_instance` 不同 | `not_comparable`（不同 Host，時間不能比較） |
| 時間倒退：受理 > 程序開始、程序開始 > 程序回收、`started_ms` > `completed_ms` | `invalid` |
| 工具呼叫落在該場的程序區間外 | `invalid` |
| 呼叫 `id` 重複、或有完成卻沒有開始 | `invalid` |
| 檔案內的 run／task／consumer 與 `host_runs` 不一致 | `invalid` |
| 紀錄說場次已結束，`host_runs` 不是 `ended`；或紀錄的 `outcome`、`store_end` 與 `host_runs` 不同（10/12 R2） | `invalid` |
| 紀錄宣稱 `TASK_CANCELLED`／`TASK_TIMED_OUT`，該 task 在 Store 的 `events` 沒有同一個結束事件（10/12 R2） | `invalid` |
| 沒有 `host_runs` 列；資料庫的 `host_runs` 或 `events` 讀不到 | `incomplete` |
| `mode` 不是 `live`（M-C 時） | `not_live` |

只有檔名或 `mode` 標籤，不能判 PASS。

### 6.4 重疊判定（只宣稱觀測到的事）

三種區間分開：
- **程序區間** P = [`process_started_ms`, `process_reaped_ms`]：程序存活，不代表在工作；
- **工具活動跨度** S = [第一次呼叫的 `started_ms`, 最後一次完成的 `completed_ms`]：其間可能有等待或閒置，不宣稱每一刻都在工作；
- **已觀測的呼叫區間** C_i = [`started_ms`, `completed_ms`]：實際觀測到的工具活動。

**交錯條件**（機器可判定的 operational 定義）：把兩場已完成呼叫的 `started_ms` 合併排序，存在 A、B、A、B（或 B、A、B、A）的交替順序，即：
- 存在 a1 < b1 < a2 < b2（或 b1 < a1 < b2 < a2），其中 a 是 A 的呼叫開始、b 是 B 的呼叫開始；
- 四個呼叫都落在各自的程序區間內；
- 兩場都通過 6.3 的完整性檢查，是同一個 `host_instance`、不同的 consumer、run 與 task。

**結論與原文**：

| 結論 | 條件 | 輸出原文 |
|---|---|---|
| 交錯成立 | 交錯條件成立 | 「同一 Host 上，consumer-a 與 consumer-b 的真實工具呼叫交錯發生：雙方都在對方兩次呼叫之間有自己的工具呼叫。這不代表跨度內每一刻都在工作，也不代表觀測到模型供應商內部同時推論。」 |
| 只有工具活動跨度相交 | S_A ∩ S_B 非空，但沒有交替順序 | 「兩場的工具活動跨度相交，但沒有觀測到交錯的工具呼叫；未確認兩場同時有活動。」 |
| 只有程序存活重疊 | P_A ∩ P_B 非空，S 不相交 | 「兩場的程序同時存活，但工具活動沒有重疊；未確認同時工作。」 |
| 只有 task 重疊 | 只有 Ask 等待期間重疊 | 「只有 task（Ask 等待）重疊，不是工作重疊。」 |
| 不判定 | 6.3 任一問題 | 依 6.3 的判定原文，列出未確認的部分 |

只有「交錯成立」算達到 M-C 的工作重疊；其他都照實回報，不寫成「已證明持續同時工作」。

### 6.5 有界的開始方式與停止隔離

- **開始**：主持人按「一起開始」，兩個請求相隔不到一秒送出。真實的單筆 Ask 一場約 10 秒、3 次工具呼叫，兩場同時開始時可能交錯，但不保證。這是**一次**嘗試：沒有達到交錯就照實記錄 6.4 的結論，不重試到通過；下一次嘗試需另行核准。不加假工具呼叫、不讓工具卡住、不加睡眠或延遲。
- **停止隔離**（路線 §6.2 第 3 步）：主持人頁顯示每場的 `tool_calls` 次數。在 A 仍是 `running` 且已有至少 1 次完成的工具呼叫、B 也仍是 `running` 時，才停止 A。成立條件（全部可從時間紀錄與 Store 查證）：
  - A 的 `stop_accepted_ms` 存在，且 `stop_accepted_from` 是 `running`；A 在這個時點之前有完成的呼叫；
  - A 的 `process_reaped_ms` 在 `stop_accepted_ms` 之後，Store 有 A 的 `TASK_CANCELLED`；
  - B 的程序區間包含 `stop_accepted_ms`，B 在該時點之後有自己的呼叫開始與完成，並以自己的結果結束（不是被取消）。
  - （10/12 R2）B 的 task 在 Store 沒有 `TASK_CANCELLED`。task 可能在場次結束後被主持人取消，這不算矛盾，但停止隔離就不能成立。
  A 已結束時停止會回 `409 run_not_running`，不能記成「成功停止活躍的 Agent」。真實的時間窗很短；若這次沒能在條件成立時停止，照實記為「停止隔離未示範」，不重試。

### 6.6 切片分工

- **M-B**：以 fake 完成時間紀錄與**唯讀判定工具**，並以正常與反例驗證（第 10 節）。fake CLI 加一個只在測試用的步驟 `wait_file`（等待某個檔案出現才繼續），用握手精確產生交錯、不交錯、停止等情況。
- **M-C**：只使用 M-B 已 review 的工具，核對另行核准的真實兩場證據；不在臨場修改規則來取得 PASS。真實執行的確認清單在 M-C 另行取得核准；本設計的 review 不代替執行核准。

## 7 介面變更一覽

| 介面 | 變更 | 權限 | 錯誤 |
|---|---|---|---|
| `cmd/demo --max-runs N` | 新旗標；預設 2；`1 ≤ N ≤ consumer 數`；需要 `--scenarios` | 本機啟動 | 非整數、超出範圍或沒有 `--scenarios`：拒絕啟動，結束碼非 0 |
| `POST /api/host/runs` | 請求不變；名額改為每 consumer 一場、總數不超過 N | operator（現行） | 新增 `409 consumer_busy`（帶 `run`）、`409 capacity_full`（帶 `max_runs` 與 `runs`）；其餘現行 |
| `POST /api/host/runs/{id}/stop` | 依 run ID 在進行中的場次查找；記錄停止受理時點 | operator | 現行 |
| `POST /api/host/tasks/{id}/cancel` | 依 task 查進行中的場次 | operator | 現行 |
| `GET /api/host/status` | `runs.active` 由單一物件改為陣列 `[{run_id,task_id,consumer_id,live}]`（最多 N）；新增 `capacity` | host、operator | 現行 |
| `runView` | 增加 `tool_calls`（次數；只在 `live` 為 `running`／`stopping` 時出現） | host、operator | — |
| `GET /api/tasks/{id}/trace` | 「執行中」改依 task 判斷 | 現行 | 現行 |
| 橋接 | v2 訊息；`bridge` 回報（版本與客人清單）；`rendered` 細節帶 consumer 與欄位世代；六項精確完成判定；v1 保留 | — | Invalid／Stale 規則見第 5.2 節 |
| 時間紀錄檔 | `<task>-<nanos>.timing.json`（新，第 6.2 節） | Host 本機檔案，不經 HTTP | 判定規則見第 6.3 節 |
| 重疊判定工具 | 唯讀，讀兩份時間紀錄與 `host_runs`，輸出 6.4 的結論與原文 | 本機 | 第 6.3、6.4 節 |
| 可信設定 | `scenarios.json` 加 `ask-b`（consumer-b 主商品 Ask）；`scenarios-live.json` 加 `live-b` | — | — |

不改事件契約、Store 的付款語意或 M1 的證據格式。

## 8 檔案影響

| 範圍 | 檔案 | 內容 |
|---|---|---|
| Host | `cmd/demo/main.go`（`--max-runs`、Host 實例 ID 與啟動時點、註解）、`control.go`、`cancel.go`、`live.go` | 有效上限、以 consumer 為鍵的進行中場次、名額與錯誤碼、`summary`、`capacity`、生命週期時點、依 task 判斷執行中 |
| 工具橋 | `internal/toolbridge`（呼叫的編號與單調時點）、`internal/buyer/host.go`、`cmd/demo/fakeruns.go`（寫時間紀錄檔） | 時間紀錄 |
| 判定工具 | 新的唯讀工具（Go 小程式或 `scripts/verify/` 下的腳本，M-B 實作時擇一） | 第 6.3、6.4 節 |
| fake | `internal/buyer/fakecli`、`testdata/cli/` | `wait_file` 步驟與多場測試情境 |
| 設定 | `testdata/demo/scenarios.json`、`scenarios-live.json`、`scripts/demo/*.sh`（不帶 `--max-runs` 時用預設） | `ask-b`、`live-b` |
| 網頁 | `web/adapter.js`、`web/app.js`、`web/host.js`、`web/host.html`、`web/unity.js` | 以 consumer 為鍵的欄位狀態與輪詢、選取與查看、v2 投影、六項完成判定、容量與畫面能力的明示 |
| Unity | `Projection.cs`、`ShopBridge.cs`、`ShopView.cs` | 場景世代加每位客人的欄位、v2 解析、逐位清空、偏移、逐位狀態列、`bridge` 回報 |
| 測試 | `cmd/demo/*_test.go`、`internal/toolbridge` 測試、`web/test/*.mjs`、Unity EditMode／PlayMode、新的多場瀏覽器腳本 | 第 10 節 |
| 文件 | `docs/demo-operation-design.md`、`demo-ui.md`、`demo-presentation-design.md`、`m1-boundaries.md`（加註）、README | 依第 1 節同步 |

不需要改：Store schema、事件契約、Policy、Gate、認證清單、CLI 版本釘選、plugin 設定、M1 的產包器與檢查器。

## 9 順序、出口與任務編號

| 片 | 內容 | 出口（可集中 review） | 核准 |
|---|---|---|---|
| **M-A**（本文件） | 最小設計與範圍修訂 | 本文件經 Codex review（R1～R4 補正後限定複核） | — |
| **M-B** | 多場 Host（有效上限、預設 2）、資料隔離、Unity 雙角色、最小主持人選取與查看、時間紀錄與唯讀判定工具；**只用 fake** | 一次集中 review：Host 名額與停止隔離、網頁與 Unity v2、時間紀錄與判定工具（以 fake 握手產生的正常與反例驗證），以及第 10 節的回歸 | 設計複核通過後核准實作 |
| **M-C** | 兩位真實 Agent 的受控展示：以 M-B 已 review 的工具判定工作重疊與停止隔離 | 真實執行的時間紀錄、判定結論原文、兩場的結果與畫面；未達目標照實記錄 | 每次 live 執行另行核准 |
| 後續 | Admin 活動／請購營運總覽；之後隨付款整合加入結算金額與鏈上證據 | 另行設計 | — |

- 相對路線文件舊版 M-B（含 Admin 總覽），本片收斂為多場與角色選取；主持人頁的場次狀態是控制用途，不是營收儀表板。
- 付款、約 20 個付款帳號的展示規模、ZK 不是開始 M-B 的前置條件；這些最終商業展示的要求也不因此刪除。
- **M-B 的實作順序**（同一個 review 出口，不拆成多次審批）：
  1. Host 的 `--max-runs`、名額、錯誤碼、停止與取消依場次或 task 查找、`capacity`，加上 Go race 測試；
  2. Host 實例、生命週期時點、工具呼叫編號與單調時點、時間紀錄檔、fake 的 `wait_file`、唯讀判定工具與其反例；
  3. Unity v2（`Projection`、`ShopBridge`、`ShopView`）與 EditMode／PlayMode 測試；
  4. 網頁的欄位狀態、輪詢、選取與查看、v2 投影、六項完成判定、容量明示，加上 node 測試；
  5. 多場瀏覽器流程與既有流程的回歸。
- **任務編號建議**：B1 卡都已 Done，不占用。建議新增 **B2-01 雙 Agent 商店（M-B）**、**B2-02 兩位真實 Agent 工作重疊（M-C）**，Admin 總覽之後為 B2-03。M-A／M-B／M-C 簡稱保留。若「第二批」的 B2 編號已保留給付款模組，由 Aaron 指定其他編號。本輪補正不定案編號、不開工。
- **分支建議**：設計複核通過後，從最終設計 commit 開新分支（例如 `b2-dual-agent`）實作 M-B；`b1-04-live` 保留 M1 的狀態。本輪不建立分支。

## 10 後續驗收與測試成本

### 10.1 最小驗收表

| 情境 | 必須成立 | 主要驗證 |
|---|---|---|
| A、B 交錯執行 | 各自的 task、請購、事件鏈、錢包參照與畫面一致；不串資料 | Go（fake，`wait_file` 交錯）、瀏覽器 |
| 容量設定 | 預設 2 並在啟動訊息與 `status.capacity` 顯示；非整數、0、超過 consumer 數、沒有 `--scenarios` 都拒絕啟動；`N = 1` 等於單場行為 | Go |
| 同人重複開始、重送、名額已滿、`stopping` 占位 | 不重複執行、不超過有效上限；`stopping` 未收尾時新開始回 `capacity_full`；`consumer_busy`、`request_id_conflict` 可理解 | Go |
| 超出畫面能力 | 有效上限或 consumer 超過 Unity 宣告的客人時，主持人頁明示哪些不在場景中；場次列表列出全部場次，不靜默遺漏；舊 Unity 時明示只呈現選取的一位 | node、瀏覽器 |
| 停 A、B 繼續 | 只取消 A 的 context、程序群組與 capability；兩場各有收尾結果；A 的停止受理時點與程序回收時點分開記錄 | Go（race）、瀏覽器 |
| A 慢 trace、失敗或到期 | B 的資料、輪詢與動畫不被拖住 | Go、node、瀏覽器 |
| Unity 完成回報 | 正例：六項完全一致才完成。反例：錯 consumer、錯 slot、錯 scene、錯 task、錯 phase、舊 seq、未送過的較大 seq、缺欄位或格式錯誤，都不完成、不結束另一位或較新目標的等待；放行真正符合的回報後才完成 | node、Unity |
| 查看較早的 task | A、B 正在各自更新時查看 A 的較早 task：兩位場景仍跟隨最新資料、欄位世代不變；面板明示「場景呈現的是…」；「回到場景這筆」後面板回到最新；回放退出後沒有舊回覆污染場景或面板 | node、瀏覽器 |
| viewer 越權、晚到回覆、舊 Unity 回報 | 不暴露其他 consumer；舊欄位、舊查看或舊場景世代的回覆與回報不覆蓋新狀態 | Go、node、Unity |
| 刷新與服務重啟 | 不自動執行模型、不混場；中斷與未確認照實顯示 | Go、瀏覽器 |
| 單場 fake、live 接線替身、單筆歷史回放、`/` viewer 頁 | 必要的既有行為不退化；不重開 M1 驗收 | 既有測試 |
| 時間紀錄與判定工具 | 同一 Host 的合法交錯 → 交錯成立；不同 `host_instance` → `not_comparable`；呼叫配對缺失、時間倒退、呼叫落在程序區間外、`id` 重複 → `invalid`；只有 task 重疊、只有程序存活重疊、跨度相交但沒有交替（跨度內有空白的活動區間）→ 不通過並輸出對應原文；中斷的呼叫保留 `null` 完成時間；A 已結束才停止 → 不算停止隔離 | Go 或 script 單元測試（以 fake 握手與手寫的反例紀錄，M-B 實作時建立） |
| 真實工作重疊與停止隔離 | M-C 以已 review 的工具核對真實兩場證據 | M-C |

### 10.2 測試與耗時

- **每次修改**：只跑受影響的定向測試。
  - `go test -race` 跑 `./cmd/demo` 的場次、容量與取消測試（現行套件約 1～3 分鐘）、`./internal/toolbridge`、`./internal/buyer`；
  - `node --test web/test/*.mjs`（約 10 秒）；
  - Unity EditMode／PlayMode（以 `scripts/unity/unity.sh` 執行，約 1～2 分鐘，需要 Unity）；
  - 判定工具的單元測試（秒級）。
- **M-B 出口前一次**：
  - 新的多場瀏覽器流程（預估 3～5 分鐘）；
  - 既有的 `browser-ui.mjs` 回歸（約 7 分鐘）與歷史回放、live 替身流程（各約 1～2 分鐘）。
- **不預設跑**：`make m1-fixture`（只在改到 fixture 或證據共用程式時才跑，約 8 分鐘）、完整容器（必要時由實作派工明列次數與範圍，約 15～20 分鐘）、全部突變與全部反向清單。
- **反向檢查**只集中在關鍵保護：
  - 有效上限、同 consumer 拒絕、`stopping` 占位、非法設定拒絕；
  - 停止只作用於指定場次、capability 只撤銷該場；
  - 六項完成判定（任一項不比對都要被抓到）、欄位與查看世代的晚到防護、Unity 的 Invalid／Stale；
  - 舊 Unity 與超出畫面能力的明示；
  - 判定工具的 `host_instance` 檢查、配對與時間倒退檢查、交替條件（只有跨度相交不得通過）。
  沿用 `m1-e3-reverse.py` 的作法：每項指定對應測試與預期斷言，在一次性 clone 中執行。
- **進度**：耗時的驗證沿用 `cmd/m1-check` 的 `--plan`，顯示步驟、基準、elapsed、log 位置與 PASS／FAIL／NOT_RUN。失敗先保存紀錄並定位，不原樣重跑到轉綠。

## 11 需要 Aaron 決定的事項

雙 Agent 重疊的方向與「同時執行上限是部署設定、本輪預設 2」的原則已接受，不再列為待決。只剩：
1. **M-C 的真實執行**：多個場次同時使用同一個本人訂閱（`~/.apa-claude`）的用量與限流風險，以及核准幾次嘗試（重疊一次、停止隔離一次，或合併為一次）。
2. **正式規則文字**：第 1 節的建議新規則，在複核通過後由 Aaron 確認再同步到 PRD、執行計畫與後續任務卡。
3. **任務編號**：若 B2 已保留給付款模組，指定雙 Agent 卡的編號。

其他細節（錯誤碼、旗標名稱、欄位世代、偏移、檔名、時間紀錄欄位、測試範圍）是本文件的推薦，由 review 決定是否調整。

## 12 本片不做的事

- 大量同時執行、效能或壓力測試、自動容量探測、排隊、排程平台、分散式 runtime；
- 擴增 `show-*` 測試資料池或付款帳號；
- Admin 聚合、營收數據；
- 付款、grant、真實核准、ZK、Signer、鏈上交易；
- Unity 內點選角色（D3）、新的尋路或素材、超過兩位客人的 Unity 畫面；
- 改 Store 的付款語意、事件契約、M1 證據格式與檢查器；
- 完整的 AI 活動監控、保存模型原文；
- 為了重疊而加假工具呼叫、卡住工具、睡眠或延遲。

## 13 R1～R4 補正對照（Codex 對 `79000a2` 的核對）

Codex 的結論是「方向通過，R1～R4 補正後再限定複核；暫不開始 M-B」。Codex 這次做的是文件、Git 差異與相關原始碼的唯讀核對，沒有跑測試、容器、Unity 或 CLI，沒有 repo 證據 log。本輪是設計修訂，不以任何舊測試結果作為通過證據。原寫法保留在 `79000a2`。

| 項目 | 原寫法（`79000a2`） | 修改後的規則 | 驗收位置 |
|---|---|---|---|
| **R1** 容量 | §3.1 寫死 `maxConcurrentRuns = 2`、「不做成旗標」；「20 個付款帳號」寫成固定要求 | 三種數量分開（§3.1）；同時執行上限是部署設定 `--max-runs`，預設與驗收 2，有驗證、拒絕與顯示規則（§3.2、§7）；「約 20 個付款帳號」是先前估計的展示規模；20 個 `show-*` task 是測試資料池；資料鍵以 consumer／run／task 定位；超出畫面能力時明示、不靜默遺漏 | §10.1「容量設定」「同人重複開始…`stopping` 占位」「超出畫面能力」 |
| **R2** Unity 回報 | §5.2 的完成條件只列 scene、consumer、slot、task；把現行同步描述成只比世代與序號 | 六項（scene、consumer、slot、task、seq、phase）全部等於該角色最後送出的投影才完成；缺項、錯項、舊序號、未送過的較大序號都不完成；同一規則用於狀態列、查證面板與動畫完成；註明現行 `sameShown` 已精確比對世代、task、序號、階段，v2 在其上加角色與欄位隔離（§5.2） | §10.1「Unity 完成回報」 |
| **R3** 查看與場景 | §5.4 一處說選取只換面板，另一處允許手動選舊 task 後更換 Unity 欄位 | 場景 task 只跟隨每位 consumer 最新場次；選取與查看較早 task 只換面板，不送替代投影、不增欄位世代、不停輪詢；場景 task 與查看 task 分開保存並明示差異、提供回到場景這筆；只有歷史回放切換場景模式，退出後重新取得資料、舊回覆丟棄（§5.1、§5.4、§5.5） | §10.1「查看較早的 task」 |
| **R4** 時間證據 | §6.2～6.4 只記錄呼叫的牆鐘毫秒；沒有 Host 實例、程序與停止時點；以首末呼叫的跨度當成有效工作區間；判定工具一處說 M-C 實作、一處列入 M-B | 時間紀錄加 Host 實例、單調時間基準、生命週期與停止受理時點、呼叫編號與開始完成配對、中斷呼叫不補時間（§6.2）；完整性檢查與判定（§6.3）；分開程序區間、工具活動跨度、已觀測呼叫區間，以「呼叫開始的交替順序」作為 operational 定義，並列出各結論原文，不宣稱持續工作或供應商內部同時推論（§6.4）；停止隔離的可查證條件（§6.5）；判定工具在 M-B 以 fake 完成，M-C 只用已 review 的工具（§6.6、§9） | §10.1「停 A、B 繼續」「時間紀錄與判定工具」 |

## 14 M-B 實作紀錄（10/11，Claude Code；只用 fake；待 Codex 限定 review）

> 後續：10/12 R1／R2／C1 補正見第 14.3 節，限定驗收見第 14.4 節。本節與 14.1～14.3 保留當時的狀態文字。

**核准**：Aaron 核准 M-B 的 fake 實作、Unity 測試與 WebGL 重建、定向驗證、關鍵反向驗證與必要文件同步。未核准：真實 Claude CLI（含版本查詢、登入、C0′、真實請購、`make present-live`）、完整容器、全套突變、M1 重新產包、M-C。分支 `b2-dual-agent`，從 `a9bd836` 建立。

**實作**（設計第 3～10 節）：
- **Host**（`cmd/demo`）：`--max-runs`（預設 2；`1～consumer 數`；沒有 `--scenarios` 或非法值時拒絕啟動，啟動訊息與 `status.capacity` 顯示有效值）；以 `consumer_id` 為鍵的進行中場次；`consumer_busy`、`capacity_full`；停止依 run、取消與 trace 執行中依 task 查找；程序結束後的停止回 `run_not_running`；`runView.tool_calls` 與 `timing`；可信設定加 `ask-b`、`live-b`。
- **時間紀錄**（`internal/timing`、`cmd/demo/timing.go`、`internal/toolbridge`、`internal/buyer`）：每次工具呼叫的編號與開始、完成時點；程序開始、回收與「呼叫關閉」時點；每場結束時寫 `<task>-<nanos>.timing.json`（與 trace 同一個時間戳，成對）；寫不出來時 `timing=not_written`，不補時間。M1 的 trace 與檢查器沒有改。
- **唯讀判定工具** `cmd/timing-judge`：讀兩份紀錄、`host_runs` 與 Store 的 task 結束事件（資料庫以唯讀開啟；10/12 R2 起核對狀態、結果與取消事件，第 14.3 節）；結束碼 0 交錯成立（加 `--stop` 時停止隔離也成立）、1 判定但未達成、2 不判定或用法錯誤、3 報告沒有完整寫出（沒有判定結果；10/12 C1）；fake 紀錄要加 `--fake` 才判定，判定文字標明是 fake。
- **Unity**（`Projection.cs`、`ShopBridge.cs`、`ShopView.cs`）：v2 的場景世代與每位客人的欄位；`bridge` 回報；六項完成回報；v2 時兩位完整亮度、各自一行狀態；v1 不變。
- **網頁**（`web/scene.js` 新增、`unity.js`、`adapter.js`、`app.js`、`host.js`、`host.html`）：每位 consumer 的場景欄位，各用自己的 viewer token 輪詢；按客人分組的開始鍵、每場的停止鍵、「一起開始」；選取與查看較早 task 只換面板，附「回到場景這筆」；`#unity-status` 映照所選客人的欄位（只有六項完全一致才是「已呈現」）；舊 Unity 或場景無法呈現全部客人時明示。

**與設計文字的差異**（實作時的小決定，請 review 判定）：
1. `clear` 用 v1 既有的布林欄位，不用字串：Unity 的 `JsonUtility` 無法把字串放進布林欄位，改成以有沒有 `consumer` 區分清空場景或單一客人（第 5.2 節已同步）。
2. 客人 B 的偏移：只往下 0.45 時，兩人在櫃台幾乎重疊（第一次截圖看得出來），改成從貨架起再往左一格，B 的標籤放在腳下。
3. 「一起開始」使用場次目錄中每位 consumer 的第一個情境（依 id 排序），按鈕上寫明是哪兩個情境。
4. 時間紀錄多一個 `calls_closed_ms`（場次的呼叫全部結束的時點）：程序回收後，仍可能有一個進行中的呼叫在收尾，所以「完成時點不晚於呼叫關閉」才是正確的上界；`completed_ms` 是 Host 得到結果、送出回覆之前的時點。
5. 主持人頁的 `#unity-status` 映照所選客人的場景欄位，讓既有的單場瀏覽器檢查仍然有意義；它只在六項一致時寫成「已呈現」。
6. fake CLI 新增只供測試的 `wait_file` 步驟，live 不使用。

**操作**：
- 雙場展示：`make demo`（或 `make present-fake`）照常啟動；主持人頁每位客人一列。`--max-runs N` 改變同時執行上限（預設 2）。
- 判定時間紀錄：`go run ./cmd/timing-judge --db <APA_DB_PATH> --a <A>.timing.json --b <B>.timing.json [--stop a|b] [--fake]`。結束碼 0／1／2／3 見上；資料庫與紀錄矛盾時不判定（2），結束碼 3 時不論 stdout 留下什麼都不算判定結果。
- M-B 驗證計畫：`scripts/verify/m-b-check-plan.json`，以 `cmd/m1-check --plan` 執行（`--list` 只列計畫，第 14.1 節）。


### 14.1 驗證結果（Claude Code 執行，待 Codex 核對）

證據目錄都在 `artifacts/m-b/`（git 忽略，只在本機）。每次執行的 `summary.json` 記有基準 commit、每步結束碼與秒數。

**整輪**（基準 `67a5041`，`artifacts/m-b/m1-check-20261011T043725Z/`，計畫 `scripts/verify/m-b-check-plan.json`）：

| 步驟 | 結果 | 結束碼 | 秒數 | 內容 |
|---|---|---|---|---|
| vet | PASS | 0 | 0.6 | `go vet` demo、timing-judge、timing、toolbridge、buyer |
| go-race-demo | PASS | 0 | 339.5 | `./cmd/demo` race：65 個測試 |
| go-race-timing-bridge | PASS | 0 | 115.6 | timing、timing-judge、toolbridge、buyer race：81 個測試 |
| node | PASS | 0 | 0.4 | adapter、hostcreds、scene：64 個測試 |
| reverse-selftest | PASS | 0 | 0.1 | 反向分類器與錨點 |
| unity | PASS | 0 | 99.8 | 場景重建；EditMode 14／14、PlayMode 20／20（`unity/*.xml`） |
| dual-ui | PASS | 0 | 39.3 | 新的雙場瀏覽器流程（截圖見下） |
| browser-ui | PASS | 0 | 428.9 | 既有單場 fake 與主持人頁 |
| history-ui | PASS | 0 | 49.5 | 歷史回放 |
| live-ui | PASS | 0 | 95.6 | live 接線代用（假 CLI、測試建立的 HOME） |
| present-ui | PASS | 0 | 99.2 | 展示流程 |
| reverse | INTERRUPTED | −1 | 77.8 | 前一個工作階段結束時中斷；中斷前 7 項抓到、2 項沒抓到（見下） |

**反向驗證的失敗與修正**（紀錄都保留，沒有原樣重跑）：
1. `043725Z`，第 2 項「停止中的場次占名額」沒抓到：突變直接把場次移出進行中，連停止請求本身也找不到這一場，測試失敗在別的斷言。改為只計 `running` 的場次（`d148ade`）。
2. `043725Z`，第 9 項「停止隔離需要 B 在停止後的呼叫」沒抓到：突變讓變數 `after` 未使用而編譯失敗。改為 `if false && !after`（`d148ade`）。
3. `152440Z`（基準 `d148ade`）：第 1～9 項都抓到；第 10 項卡住，手動停止，紀錄為 INTERRUPTED。原因：突變後 scene 測試第 7 個也失敗，失敗的斷言跳過了測試自己的 `scene.stop()`，輪詢計時器讓 `node --test` 永遠不結束。測試改為每個測試結束（成功或失敗）都停止所有場景（`124ba22`）。
4. `153211Z`（基準 `124ba22`）：第 1～12 項抓到；第 13 項（Unity）的未突變基準在副本中不通過：複製的 Library 保留原本的檔案時間，編輯器沿用舊的 script 紀錄，PlayMode 找不到場景上的 script。副本改用新的檔案時間複製（`480c750`，與 `cp -R` 相同；只改反向腳本）。
5. `154143Z`（基準 `480c750`）：只重跑 Unity 那一項，抓到。

**13 項命中的斷言**（第 1～12 項 `153211Z`，第 13 項 `154143Z`）：

| # | 項目 | 命中 |
|---|---|---|
| 1 | 名額上限 | `TestCapacityAndStopping` `full: 202` |
| 2 | 停止中的場次占名額 | `TestCapacityAndStopping` `while stopping: 202` |
| 3 | 每位 consumer 一場 | `TestTwoRunsAtOnce` `same consumer: 409` |
| 4 | 停止只影響自己那場 | `TestStopIsolation` `B after A's stop` |
| 5 | 非法 `--max-runs` 拒絕啟動 | `TestMaxRunsSetting` `more than consumers` |
| 6 | 兩個 Host 不比較 | `TestNotJudged/another_host` `want not_comparable` |
| 7 | 程序外的呼叫無效 | `TestNotJudged/call_before_process` `want invalid` |
| 8 | 交錯需要 A,B,A,B | `TestNotInterleaved/spans_meet,_a_quiet_gap` `want spans_only` |
| 9 | 停止隔離需要 B 停止後的呼叫 | `TestStopIsolation/no_call_of_B_after_the_stop` `Held:true` |
| 10 | 六項規則比對欄位 | scene 測試「the six-field rule」`another slot` |
| 11 | 每個欄位用自己的 token | scene 測試「two consumers at once」`a consumer's task read with another's token` |
| 12 | 場景無法呈現全部時明示 | scene 測試「each consumer's scene task」`deep-equal` |
| 13 | 較舊欄位世代是 Stale | `SceneStateTests.ASlotChangeResetsThatCustomerOnly` `Expected: Stale` |

**改測試後重跑受影響部分**：node 與 reverse-selftest（基準 `480c750`，`m1-check-20261011T154520Z`）PASS，node 64／64。整輪的其他步驟沒有重跑：之後的三個 commit 只改 scene 測試與反向腳本。

**WebGL**：從 `8b3c265` 重建（Unity 6000.5.3f1；之後 `unity/` 沒有再改）。sha256 記在 `artifacts/m-b/webgl-8b3c265.sha256`：

```text
3217162c96b765a63749237035c3658f9588a8a055c9d1eec1092f333f91ecca  WebGL.data
3c7d6ab5547a85de82e08400bfd63520ba9c2f50c098eff05074a6a010e65666  WebGL.framework.js
813b3921e95b836b17e0beea85e39b996ecae337329bec6d6c868b8f0ef7ae65  WebGL.loader.js
9a6efc0b73c3a3d9c8d03314539e94d601b15e0dd05f1913d297b268e0f8d4e4  WebGL.wasm
```

**雙場截圖**（`043725Z/dual-ui/`；每張都先確認 token 欄位是空的）：`dual-both-ask`、`dual-a-asks-b-entered`、`dual-a-stopped-b-waits`、`dual-viewing-older`、`dual-after-reload`、`dual-old-unity`。截圖證明頁面的 DOM 與狀態文字；畫布內容以 Unity 測試為準，DOM 不證明畫布畫了什麼。

**fake 時間判定範例**（`artifacts/m-b/timing-judge-fake-example/`；`judge-example.mjs` 以測試建立的 HOME 與假 CLI 跑兩場，用 `wait_file` 排出 A、B、A 的呼叫，A 停止後 B 再呼叫一次）：

```text
timing-judge --fake --db $TMP/state/demo.db --a show-a-01-….timing.json --b show-b-01-….timing.json --stop a
exit 0
conclusion: interleaved
text: （fake 紀錄：不是真實模型的工作）同一 Host 上，consumer-a 與 consumer-b 的工具呼叫交錯發生：雙方都在對方兩次呼叫之間有自己的工具呼叫。這不代表跨度內每一刻都在工作，也不代表觀測到模型供應商內部同時推論。
stop_isolation.held: true
```

這是 fake 紀錄，不是真實模型的工作。

**限制**：
- 沒有執行真實 Claude CLI、完整斷網容器、全套突變、M1 重新產包。
- 畫布上兩位客人的名字標籤字小、對比低，在截圖中不易辨識；兩人的狀態另有畫布下方各自一行的文字。
- 「場景無法呈現全部客人」的提示，用目前兩位 consumer 的登錄無法在瀏覽器觸發，只由 node 單元測試涵蓋。
- browser-ui 3.5 原本由瀏覽器檢查 Unity 呈現**較舊** task 的到期狀態；現在場景跟隨最新場次，這項改由 Unity 測試涵蓋。

### 14.2 M-C 執行前確認清單（未執行；M-C 須另行核准）

- [ ] Aaron 核准 M-C 的真實執行，以及兩場同時使用同一個本人訂閱（第 11 節：用量、限流、嘗試次數）。
- [ ] 本機 CLI 版本等於釘選版本；專用設定目錄、Gate 與認證清單照現行。
- [ ] 用 `scenarios-live.json`（`live`、`live-b`），`--max-runs 2`；啟動訊息顯示上限 2 與 host instance。
- [ ] 只嘗試一次「一起開始」；沒有交錯就照實記錄，不重試（第 6.5 節）。
- [ ] 停止隔離只在 A `running` 且已有完成的呼叫、B 仍 `running` 時才停止 A；沒抓到時機就記為「停止隔離未示範」。
- [ ] 以 M-B review 過的 `timing-judge`（不加 `--fake`）判定；保留判定結論原文、兩份時間紀錄、trace 與 Store 匯出。
- [ ] 證據中沒有 token、配對碼或登入資料。

### 14.3 R1、R2、C1 補正（10/12，Claude Code 執行，待 Codex 核對）

**依據**：Codex 對 `eed0f90` 的限定 review。該 review 是原始碼與 Git 的唯讀核對；當時 Docker 沒有運行，Codex 沒有獨立執行這些反例。Codex 另核對了驗證摘要與四個 WebGL 檔的雜湊（相符），沒有逐一核對全部反向 log。M-B 暫不通過；M1 與 B1-00～B1-07 維持 Done；M-C 未開始。第 14.1 節的紀錄維持原樣：`67a5041` 整輪前 11 步 PASS、最後的反向步驟中斷，之後的反向修正與分批結果，不是同一基準上的整輪全過。

**修改**（開工基準 `eed0f90`）：
- **R1**（`web/scene.js`，`5cd2384`）：`reset()`、`stop()` 會把所有欄位移出場景，但舊欄位的世代沒有改，所以一個回應已到、JSON 尚未讀完的請求，仍可能通過世代檢查。新增 `current(s, gen)`：欄位仍是該 consumer 目前的欄位，且世代未變。請求開始前、取得回應後、讀完 JSON 後、更新狀態、送 Unity 投影、通知畫面與安排下一次讀取之前，都要先通過這項檢查；舊工作結束時也不會清除新欄位的 in-flight 狀態。正常換 task、A／B 各自輪詢、回放後恢復的行為不變。
- **R2**（`internal/timing`、`cmd/timing-judge`，`3591736`）：判定工具以唯讀方式讀取每場的 `host_runs`（`state`、`outcome`、`store_end`），以及該 task 在 Store `events` 的結束事件（`timing.ReadHostRun`；判定工具和 demo 的端到端測試共用這段讀取）。
  - `Check`：紀錄說場次已結束而 `host_runs` 不是 `ended`、`outcome` 或 `store_end` 與 `host_runs` 不同、紀錄宣稱的 Store 結束在 `events` 找不到同一事件，都判 `invalid`。沒有 `host_runs` 列，或資料庫讀不到，判 `incomplete`。這兩種情況都不判定（結束碼 2），也不做停止隔離判定。
  - `StopIsolation`：另一場的 task 在 Store 有 `TASK_CANCELLED` 時不成立（task 可能在場次結束後被取消，所以這不算矛盾）。
  - 合法的取消、逾時等結束照各自的語意處理：B 自己逾時時，交錯與停止隔離仍可成立。沒有改 schema、Policy、事件契約、M1 檢查器或 Store 的寫入流程。
- **C1**（`cmd/timing-judge`，`3591736`）：報告寫出失敗或短寫時，結束碼 3，stderr 給出不含敏感內容的原因（「the report was not written in full (n of m bytes); there is no verdict」）。結束碼 3 時，不論 stdout 留下什麼，都不算判定結果。
- 反向項目與本輪計畫：`67fd695`（`scripts/verify/m-b-reverse.py` 新增 8 項，以 `^(R1|R2|C1):` 單獨執行；`scripts/verify/m-b-r1r2c1-check-plan.json`）。文件：`107819f` 與本節。

**最小重現**（修正前）：
- Node：新的兩個 scene 測試在 `eed0f90` 的 `scene.js` 上都失敗，斷言是 `the old reply sent a projection`。
- 瀏覽器：同一流程在 `eed0f90` 的 `scene.js` 上 FAIL（`artifacts/m-b/r1-repro-prefix-dual-ui/`；工作區是新測試加上舊的 `scene.js`）。舊回覆把 A 的欄位 2 送進新場景 3，Unity 回報 `consumer-a|2|show-a-03|waiting|…`；A 真正的欄位 1 因此顯示為未呈現（`shown:false`）。

**新增測試**：
- Node（`web/test/scene.test.mjs`）：A 的事件回應已到、JSON 被扣住，接著 `reset()`（或 `stop()`→`start()`），新場景指向新的 task，然後才放行舊 JSON。檢查四件事：沒有任何新投影、沒有通知、超過一個輪詢間隔後舊 task 沒有再被讀取、新場景照常更新。
- 瀏覽器（`scripts/verify/dual-ui.mjs`，「old reply after new credentials」）：
  - 觸發方式：按「清除全部憑證」再載入 token 檔，走真實的憑證生命週期；只使用測試建立的合成憑證，不保存 token。
  - 扣住 JSON 的方法：頁面自己的 `fetch` 被包一層，只攔住場景讀取（以呼叫來源判斷）的一份事件內容。
  - Unity 的回報逐筆記錄下來，用來確認新場景沒有收到 A 欄位 2 的東西，且 A 的欄位 1 仍顯示已呈現。
- Go，`internal/timing`：`TestRecordVsDatabase` 從一致的一對紀錄出發，每個案例只改一處，並檢查拒絕的原因文字；逾時是合法結束的正例；`TestStopIsolation` 加上「B 在 Store 被取消」的案例。
- Go，`cmd/timing-judge`：
  - `TestJudge` 與 `TestRecordsAgainstTheDatabase` 由命令入口讀取以 Store 本身建立的資料庫（`store.Open`、`AcceptRun`、`StartTask`、`Cancel`、`EndRun`）。
  - 案例：一致的停止 A／B 繼續（結束碼 0）；A 宣稱取消但資料庫沒有該事件（直接刪除事件列，模擬被改過的資料庫）；A 宣稱取消但資料庫說 A 正常結束；B 在 Store 被取消；`host_runs` 仍是 `accepted`；B 的 outcome 不同；資料庫沒有 `events` 表；未達交錯（`processes_only`，結束碼 1）。
  - `TestReportWrite`：寫入錯誤、短寫但不回 error、正常寫出（只有一份完整 JSON）三種 writer。

**驗證**（`artifacts/m-b/m1-check-20261011T162236Z/`，基準 `67fd695`；計畫 `scripts/verify/m-b-r1r2c1-check-plan.json`）：

| 步驟 | 結果 | 結束碼 | 秒數 | 內容 |
|---|---|---|---|---|
| vet | PASS | 0 | 0.3 | demo、timing-judge、timing |
| go-race-timing-judge | PASS | 0 | 4.2 | timing、timing-judge race：10 個測試（不含子測試） |
| go-race-demo-runs | PASS | 0 | 22.9 | demo 的多場與時間紀錄測試 race：5 個（`TestStopIsolation` 改用 `ReadHostRun`） |
| node | PASS | 0 | 5.7 | adapter、hostcreds、scene：66 個測試 |
| reverse-selftest | PASS | 0 | 0.1 | 21 項錨點與分類器 |
| dual-ui | PASS | 0 | 46.6 | 雙場流程，含新的 R1 瀏覽器案例 |
| history-ui | PASS | 0 | 35.4 | 回放的暫停與恢復（`scene.js` 有改） |
| browser-ui | PASS | 0 | 433.9 | 既有單場與主持人頁的憑證流程（`scene.js` 有改） |
| reverse | FAIL | 2 | 0.3 | 執行期間我改了文件，工作區有未提交的修改，腳本依規則拒絕建立副本；紀錄保留 |

反向驗證在提交文件後只重跑這一步（基準 `107819f`，只多了文件；`artifacts/m-b/m1-check-20261011T163221Z/`，PASS，26.7 秒）。8 項都由預期斷言抓到：

| 項目 | 突變 | 命中 |
|---|---|---|
| R1 reset | `current` 只比世代 | scene「…before reset (new credentials)…」`the old reply sent a projection` |
| R1 stop→start | 同上 | scene「…before stop, then start…」`the old reply sent a projection` |
| R2 宣稱的結束需有事件 | 移除事件比對 | `TestRecordsAgainstTheDatabase/A's_record_says_TASK_CANCELLED,…` `want not judged, A's cancel has no event in the Store` |
| R2 outcome／store_end 與 host_runs 一致 | 移除比對 | `…/B's_record_has_another_outcome_than_host_runs` `want not judged, B's outcome differs from host_runs` |
| R2 host_runs 須為 ended | 移除狀態比對 | `TestRecordVsDatabase/B's_run_not_ended_in_host_runs` `want invalid: host_runs says it is accepted` |
| R2 B 在 Store 被取消時停止隔離不成立 | 移除檢查 | `…/B_was_cancelled_in_the_database,…` `want stop isolation not held, B cancelled in the Store` |
| C1 寫入錯誤 | 不檢查寫入 | `TestReportWrite/a_write_error` `want exit 3 and the reason` |
| C1 短寫 | 不檢查長度 | `TestReportWrite/a_short_write_without_an_error` `want exit 3 and the reason` |

「host_runs 須為 ended」一項只在原因上被抓到：在真實資料庫裡，未結束的列沒有 outcome，所以移除狀態比對後，outcome 比對仍會拒絕這個情況（結論仍是不判定）。這項確認的是指出的原因，不代表安全性依賴它。

**沿用與沒有重跑**：
- 沿用第 14.1 節的 Unity 與 WebGL 結果：本輪沒有改 `unity/`，沒有重建。
- 沒有重跑 live-ui、present-ui：本輪只改了 `scene.js` 的防護，兩者走的主持人頁流程由 browser-ui 涵蓋，也不使用判定工具。
- 沒有重跑完整的 demo race、M1、容器或全套突變。
- 沒有執行真實 CLI。

### 14.4 M-B 限定驗收（10/12）

**結論**：M-B 的雙 Agent fake 實作通過限定範圍技術 review。R1、R2、C1 於產品修正 `5cd2384`、`3591736`，搭配測試與計畫 `67fd695` 通過本次限定複核，三項關閉。可以進入 M-C 執行準備；這不代表真實雙 Agent 已有工作交錯、停止隔離或付款能力，也不構成 M-C 真實執行核准。

**關閉理由**：
1. **R1**：同時核對 slot 實例與世代，已移除 slot 的晚到回覆不能更新狀態、送投影或恢復輪詢；Node 的 reset／stop-start 與瀏覽器的憑證生命週期案例涵蓋原缺口。
2. **R2**：時間紀錄已對照 `host_runs` 終態、結果與 Store 結束事件；缺失或矛盾不判定，B 在 Store 被取消時不會宣稱停止隔離成立。合法逾時等結果保留原語意。
3. **C1**：報告寫入錯誤或短寫回非零（3），正常輸出仍是一份完整 JSON；不完整輸出不能作為成功判定。

M1 與 B1-00～B1-07 維持 Done。本節記錄 M-B 技術 review 通過與三項關閉，不代替 Aaron 對其他任務卡或整體商業展示版做結案決定。

**驗收基準**：產品 `5cd2384`、`3591736`，測試與計畫 `67fd695`。`67fd695..994291b` 只有 README、設計與路線文件的差異；本節所在的文件提交也不另算產品驗收基準。

**證據歸屬**：
- **Codex 本輪核對**：唯讀核對原始碼、測試設計與 Git 差異，並核對已保存的定向驗證與八項反向紀錄。Docker 當時未運行；Codex 本輪沒有重跑 Go、Node、瀏覽器或反向測試，沒有執行真實 CLI，也沒有重建 Unity。本輪核對沒有 repo 內的 log。
- **Claude Code 執行、Codex 核對紀錄**：
  - `artifacts/m-b/m1-check-20261011T162236Z/`（基準 `67fd695`）：前八步通過（vet、timing／判定工具 race、demo 多場 race、Node 66 項、反向自檢、dual-ui、history-ui、browser-ui）。同一次第九步反向驗證因工作區有未提交修改而被拒（exit 2），整輪 summary 保留 FAIL。
  - `artifacts/m-b/m1-check-20261011T163221Z/`（基準 `107819f`）：提交文件後只重跑反向步驟，八項由預期斷言抓到。這兩次結果不是同一輪的九步全過。
- **限制**：「host_runs 須為 ended」一項只驗證被指出的拒絕原因。移除該檢查後，結果比對仍會拒絕，結論仍是不判定，所以不宣稱八項在移除後都必然錯誤放行。
- §14.1 的 Unity／WebGL 結果與先前的分批驗證，維持原基準與原歸屬。本輪沒有改 Unity，也沒有新建置；前輪 Codex 的四檔雜湊核對，不是本輪的重建或重跑。

**M-C 的界線**：
- 執行前確認清單見第 14.2 節，仍未執行，須另行核准。
- Aaron 仍需決定三件事：執行者、真實嘗試次數，以及多場共用本人訂閱的用量與限流風險。
- M-B 的 fake 交錯與停止隔離結果，不代表真實模型已有同樣結果。
- 開發展示的 plugin 例外與其剩餘風險照舊，嚴格零 plugin 模式仍未驗證；Gate、認證清單與版本釘選都沒有改。
- 雙角色展示不代表任意容量已驗收：可管理的帳號數、同時執行的 Agent 數與付款並行數是三個不同的數量，20 不是產品硬上限。
- 付款、Admin、真實核准／grant、Signer、BBS+、ZK、語音等屬於後續切片，不在本次驗收範圍。

## 15 M-C 真實雙 Agent 執行（10/12，一次）

**核准與分工**：Aaron 核准一次 M-C，由 Aaron 在自己的終端機操作（`make present-live`；配對碼與登入資料不經 Claude Code），Claude Code 協助。核准包含兩場同時使用 Aaron 本人的訂閱。執行前，Claude Code 只做唯讀準備：`~/.local/bin/claude` 已自動更新到 2.1.296，host 只接受 `2.1.293 (Claude Code)`；經 Aaron 選擇，把本機 `var/present.conf`（git 忽略）的 `cli=` 改為 `/Users/mac/.local/share/claude/versions/2.1.293`，只改這一行。開發展示 plugin 例外照舊開啟；Gate、認證清單與版本釘選未改。

**這一次**（場次目錄 `var/present-GyCNVx/`，git 忽略）：
- 建置 `0dbefd8`（`build.txt`）。Go 標記 `vcs.modified=true`，來源是未追蹤的 `demo` 目錄；`3591736..0dbefd8` 的產品與判定工具程式沒有差異。
- Host instance `70e423a9411278f7b1c735f7cb53109d`，於 2026-10-11T16:47:55Z 啟動，同時執行上限 2。
- 兩場：
  - A：`live-a`，`show-a-01`，受理於 16:48:16Z，結束於 16:48:26Z。
  - B：`live-b`，`show-b-01`，受理於 16:48:19Z，結束於 16:48:29Z。
- 兩場都由 CLI 2.1.293 執行；init gate passed（`dev_demo_plugins`）；model 為 `claude-sonnet-5-5`。各自依序呼叫 `list_products`、`get_quote`、`request_purchase`，最後都得到 Ask。outcome 都是 `ok`，結束碼 0，trace 沒有 stderr。
- 兩場的受理時點相隔約 3.7 秒（`accepted_ms` 分別是 20495、24192），大於 §6.5 預期「一起開始」的一秒內。紀錄無法區分是按了一次「一起開始」還是分別按下開始，這裡照實記錄。
- 沒有受理任何停止（兩份紀錄的 `stop_accepted_ms` 都是 null）。
- Store：A 的 Ask 在 16:48:53Z 到期（`APPROVAL_EXPIRED`），task 仍是 `running`。B 的 task 在 16:48:55Z 撤回並取消（`APPROVAL_WITHDRAWN`、`TASK_CANCELLED`），這時 B 的場次已結束 26 秒，`host_runs.store_end` 仍是 `""`，所以這是場次結束後的取消（例如主持人頁的取消），不是場次中的停止。
- 沒有付款，也沒有任何付款能力。

**判定**（Claude Code 以 `0dbefd8` 建置 `cmd/timing-judge`，不加 `--fake`；資料庫以唯讀開啟，前後 sha256 相同；輸出在 `artifacts/m-c/judge-GyCNVx/`）：
- `timing-judge --db …/live.db --a …show-a-01….timing.json --b …show-b-01….timing.json`：**結束碼 0，`interleaved`，`fake: false`**。
  - 呼叫開始時點（毫秒）：A 24564、25979、27796；B 27365、28899、30737。
  - 依時間排序是 A、A、B、A、B、B，含有 A（25979）、B（27365）、A（27796）、B（28899）的交替鏈。
  - 判定原文：「同一 Host 上，consumer-a 與 consumer-b 的工具呼叫交錯發生：雙方都在對方兩次呼叫之間有自己的工具呼叫。這不代表跨度內每一刻都在工作，也不代表觀測到模型供應商內部同時推論。」
- 加上 `--stop a`：結束碼 1，`held: false`，原文為「停止隔離未示範：沒有在 running 時受理的停止（停止被拒或對已結束的場次）」。依 §6.5，這一次不重試。即使有停止，B 的 task 在 Store 有 `TASK_CANCELLED`，依 R2 停止隔離也不會成立。

**證據檢查**：兩份 trace 與兩份時間紀錄都只有固定代碼、工具名稱、商品 ID 與用量計數；搜尋 token、bearer、配對碼與密碼字樣，只命中用量欄位（`input_tokens` 等），沒有憑證。

**結論**（Claude Code 唯讀核對，待 Codex 核對）：
- 這一次 M-C 的兩位真實 Agent，在同一 Host 上的工具呼叫判定為**交錯**（M-C 的工作重疊條件，§6.4）。
- **停止隔離未示範**。
- 這不代表跨度內每一刻都在工作，也不代表觀測到供應商內部同時推論；不代表付款能力。
- 只有一次結果，不推論穩定性或任意容量。

**後續待決**：是否另外核准一次以示範停止隔離（停止 A 的時機要在 A 已有完成的呼叫、且兩場都仍在執行時），由 Aaron 決定；本紀錄不自動安排。
