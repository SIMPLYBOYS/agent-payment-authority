# 雙 Agent 商店：M-A 最小設計與範圍修訂提案

> 2026-10-11，Claude Code（M-A）。基準 HEAD `d3b7c53`（M1 已於 10/11 結案）。待 Codex review；**本文件不是實作或執行核准**。
> 只讀程式與文件寫成，沒有改產品、沒有跑測試或展示。M1 與 B1-00～B1-07 維持 Done，本文件的工作是後續切片，不以 M1 的 PASS 代替並行驗收。
> 方向（Aaron 已接受）：先讓兩位不同 consumer 的 Agent 在同一間商店有重疊的工作流程，各自的資料與場景不串，之後再接 Admin 營運視角。下面的生命週期、橋接格式與權限方案是本文件的**推薦**，不是 Aaron 已逐項核准的細節。

## 0 推薦方案（一段話）

Host 從「全域同時一場」改成「**每位 consumer 最多一場、總數最多 2 場**」；`stopping` 未收尾前仍占名額。主持人頁每位 consumer 一列，各有自己的「開始」與「停止」，另有一個「A、B 一起開始」便利鍵（送兩個獨立的開始請求）。Unity 橋接升到 **v2**：同一個場景裡每位客人有自己的世代、task 與序號，兩位同時作用；舊的 v1 單客人路徑保留給 `/` viewer 頁與歷史回放。主持人頁以「選取 A／B」切換請購卡、Timeline、Inspector 與 trace，選取只換面板，不重啟場次、不清場景。真實工作重疊（M-C）以 Host 在**事發時**記下的每次工具呼叫時間判定，寫成獨立的時間紀錄檔，不改 M1 的 trace 與檢查器。

## 1 範圍修訂對照

現行規則都寫明「一次一個 active task／同時一場」。下表是出處、建議的新規則與影響；**review 通過後**才同步到 PRD、執行計畫與任務清單，本輪不改這些正式文件。

| 出處 | 現行規則（摘要） | 建議的新規則 | 影響 |
|---|---|---|---|
| PRD-第一期 §3.2 消費者、Agent 與錢包（9/27） | 依序重用同一間商店；一次只需一個 active task，不新增大量 NPC 或多模型並行 | 每位 consumer 一次最多一個執行中的場次；至少兩位不同 consumer 的場次可重疊，同時上限 2；仍不新增大量 NPC | 正式規則文字 |
| PRD-第一期 §5「像素客人的消費流程」 | 兩個 profile 依序操作，一次一個主線 task | 兩位客人可同時在店內，各自依自己的 task 事件呈現 | 同上 |
| 執行計畫 §1-novies（9/27 定案）、§4（9/27 錢包更新）、§9.2 循序圖 | 一個 active task；主線一次啟動其中一位 | 同 PRD；§9.2 增補雙場循序 | 同上 |
| 第一批任務清單 §1、B1-06 卡 | 一次只跑其中一個 task；一次啟動一位 | **不改**：M1 已結案，保留為 M1 當時的規則；新規則寫在後續任務卡 | 無 |
| `docs/m1-boundaries.md`「誰可以啟動 task」 | 一次只有一個 active task，A 與 B 依序操作 | 加註：M1 範圍如此；雙場見本文件 | 文件 |
| `docs/demo-operation-design.md` §4、§6.1、§13.3 | 有場次 `running`／`stopping` 時 `409 run_active` | 每位 consumer 一場、總數 2；錯誤碼分開（第 3.3 節） | `cmd/demo` |
| `docs/demo-ui.md` §1、§3 | 只有目前 viewer 的客人會移動；同一世代只能有一個 task | v2：每位客人各自的世代與 task；v1 不變 | `web/`、Unity |
| `docs/demo-presentation-design.md` §2、§4.4 | 另一位客人調暗、不移動；切換 consumer 時重設選取 | v2 兩位都完整亮度；選取只換面板 | `web/`、Unity |
| `docs/cli-poc.md` §55.1 | live 目錄只有 `live-a`；同時一場 | live 目錄加 `live-b`；同時最多 2 場 | 可信設定 |
| `docs/commercial-demo-roadmap.md` §0、§6.3、§6.8 | 一次一個 active task、Host 同時一場；舊 M-B 含 Admin 總覽 | M-B 收斂為雙場與角色選取；Admin 後接（第 9 節） | 路線文件 |

## 2 要交付的體驗（M-B 完成時）

1. 主持人頁有 consumer-a、consumer-b 兩列，各有「開始」「停止」；另有「A、B 一起開始」。不需要切換憑證或手動選 task 才能看見另一位。
2. 同一間 Unity 商店同時呈現兩位客人，各自依自己 task 的已提交事件進場、報價、等待或離場；不是把同一筆請購複製成兩個 NPC。
3. 主持人選 A 或 B，請購卡、Timeline、Inspector 與白名單 trace 跟著換；改變選取不重啟場次，另一位角色也不會消失。Unity 內點選角色屬 D3，後移，不阻擋本片。
4. 可以只停止 A，B 繼續；A 的取消、到期、錯誤或慢 trace 都不拖住 B。
5. 啟動服務、配對、刷新、讀取模式資訊都不會自動執行模型。保留已驗收的簡化入口，以及 fake／live／開發展示例外／未付款的標示。

## 3 場次與生命週期

### 3.1 名額規則

- **每位 consumer**：最多一場處於 `running` 或 `stopping`。
- **總數**：最多 2 場處於 `running` 或 `stopping`（常數 `maxConcurrentRuns = 2`，不做成旗標）。`stopping` 尚未收尾時仍占名額。
- 名額只看**程序狀態**（記憶體中的 `running`／`stopping`），不看 task 狀態。這與現行「`host_runs` 是持久紀錄，程序狀態只在記憶體」的分離一致（demo-operation-design §13.5）。

### 3.2 兩層狀態分開

| 層 | 狀態 | 來源 | 說明 |
|---|---|---|---|
| 場次程序 | `running`、`stopping`、`ended` | 記憶體（`liveRun`） | 只有 `running`／`stopping` 占名額 |
| 場次紀錄 | `accepted`、`ended`、`interrupted` | `host_runs` | 重啟時 `accepted` 一律改 `interrupted`（現行） |
| task | `running`、`awaiting_approval`、`denied`、`cancelled`、`failed` | Store 投影 | `APPROVAL_EXPIRED` 是請購的核准狀態，不是 task 狀態 |

**推薦規則**：場次結束後，該 consumer 的 Ask 若仍 `awaiting_approval`，**允許**同一位 consumer 開始新場次。理由：現行行為本來就是如此（只檢查進行中的場次）；等待中的 Ask 是在等真人，不是 Agent 在工作；新場次照 M1 規則配發新 task、沿用同一個錢包參照。
- 場景中每位 consumer 只有**一個角色**，代表該 consumer **最新**的 task。較早仍在等待的 Ask 不另外生成角色，只出現在該 consumer 的 task 列表，標示「較早的 task：等待核准」，可選取查看。
- 「一場」是場次程序，「一個 task」是 Store 的紀錄；兩者分開稱呼，不再用「目前請購」同時指兩件事。

### 3.3 開始、重送與停止

開始仍是 `POST /api/host/runs {"request_id","scenario"}`（operator、同 Origin、嚴格 JSON，現行）。在 `c.mu` 下依序檢查：

| 順序 | 情況 | 回覆 |
|---|---|---|
| 1 | request ID 已存在、情境相同 | `200` 同一場（即使名額已滿或已結束，現行） |
| 2 | request ID 已存在、情境不同 | `409 request_id_conflict`（現行） |
| 3 | 情境不存在 | `400 unknown_scenario`（現行） |
| 4 | 該情境的 consumer 已有場次 `running`／`stopping` | `409 {"error":"consumer_busy","run":<runView>}`（取代該情況下的 `run_active`） |
| 5 | 已有 2 場 `running`／`stopping` | `409 {"error":"capacity_full","runs":[<runView>…]}` |
| 6 | 該 consumer 沒有可用的 `show-*` task | `409 no_free_task`（現行） |
| 7 | Store 錯誤 | `503 store_unavailable`（現行） |
| 8 | 受理 | `202 <runView>` |

- request ID 仍是全域唯一（`host_runs.request_id UNIQUE`），先於名額檢查，所以重送永遠拿到同一場，不會重複執行。
- **停止** `POST /api/host/runs/{id}/stop`：只作用於該 run ID（現行即以 ID 查詢，改為在每位 consumer 的進行中場次裡查）。`stopping` 再停止回 `202`（現行）。
- **取消 task** `POST /api/host/tasks/{task_id}/cancel`：該 task 的場次仍 `running`／`stopping` 時回 `409 run_active`（語意不變，改為依 task 查詢）。
- 「A、B 一起開始」是前端便利鍵：依序送兩個獨立請求（各自的 request ID）。後端沒有「雙場」請求；一個被拒（例如 `consumer_busy`）不影響另一個，畫面分別顯示結果。

### 3.4 task 配發、持久化與恢復

- **配發**：沿用 `AcceptRun`，每位 consumer 從自己的 `show-*` 池（各 20 個）取第一個未用的 task；兩位 consumer 不搶同一個池。`host_runs.task_id UNIQUE` 與單一寫入連線（`BEGIN IMMEDIATE`）讓兩個同時受理的請求序列化，不需要改 schema。
- **同時寫入**：兩場的 `Submit`／`StartTask`／`EndRun` 由 Store 的單一寫入連線序列化（現行）。重試鍵、nonce、請購 ID 都含 task，兩場不會衝突。
- **到期**：沿用每個 host 一個的 `RunExpiryReporting`，它本來就走所有 task；A 的 Ask 到期只寫 A 的事件。
- **重啟**：沿用 `InterruptOpenRuns`：兩場的 `accepted` 都改 `interrupted`，不自動重開模型，畫面各自標示中斷；不寫事件、不取消 task（現行）。
- **鎖**：`c.mu` 目前在受理、取消與場次收尾時跨 Store 寫入持有。兩場時仍正確（寫入以毫秒計），只是一場收尾時另一場的控制請求要排隊；本片不改鎖的粒度。

### 3.5 每場的資源隔離

- 已經是每場獨立的（保留）：
  - CLI 程序群組與停止（`Setpgid`、SIGTERM 後 SIGKILL）；
  - 暫存目錄 `apa-run-*`（工作目錄、plugin 目錄、`mcp.json`）與 MCP bridge 子程序；
  - capability：每場 32 位元組，`Bridge.Register` 註冊、場次結束 `Revoke`；共用的 `toolbridge.Server` 以 capability 的雜湊分開各場的 `Binding`；
  - trace 檔名 `<task>-<nanos>.jsonl`、usage 與錯誤紀錄。
- 要改的：`control.active`（單一指標）改成「每位 consumer 的進行中場次」對照表。所有用到它的地方一起改：受理檢查、停止、`view`、場次收尾、`summary()`、`running(taskID)`（`live.go`）、`cancelTask`（`cancel.go`）。
- 停止 A 只取消 A 的 context：A 的程序群組收到訊號、A 的 capability 被撤銷；B 的 context、程序群組、capability 都不受影響。這是 M-B 的必測反例（第 10 節）。

### 3.6 上限與共享登入

- 每場的上限沿用：180 秒、10 則回覆、8 次工具呼叫、收尾寬限；總數增加到 2，**不提高**任何個別上限。
- 兩個真實 CLI 共用同一個設定與登入目錄（`~/.apa-claude`，apa-buyer 每場自行設定）。同時使用可能遇到限流、設定檔鎖或 token 更新的競爭，這在 M-C 之前沒有實測過。處理方式：
  - 不自動重試模型；
  - 一場失敗只記在該場（`start_failed`、`failed` 等），另一場照常；
  - 是否接受兩個場次同時使用同一個本人訂閱，列為 M-C 的核准事項（第 11 節）。

## 4 身分與權限

- A、B 維持各自的 consumer、agent、task、wallet reference 與可信設定綁定；同一位的新 task 沿用錢包參照。不建立真實錢包或私鑰。
- 兩個 demo consumer **不是**兩個 Claude 訂閱帳號，也不代表已做真實身分驗證、真人委託或 grant。真實認證沿用既有核准方式，本片不重新設計登入。
- 讀取權限不新增：
  - 主持人頁讀 task 與事件時，**每個角色欄位使用該 consumer 自己的 viewer token**（`/host` 本來就持有兩個 viewer token，`host.js` 開頭的規則保留）。這樣即使前端把 A 的欄位指錯成 B 的 task，伺服器也會回 404，不會把 B 的資料顯示在 A 下；
  - 場次列表與狀態沿用 host／operator 的現行讀取權限；
  - viewer 仍只看自己的 consumer；operator 仍不能讀 task 內容。
- 不新增公開聚合或免驗證端點；token 與 capability 不進入投影、Unity 訊息或 log（沿用現行掃描）。
- 唯一的新讀取欄位：`runView` 增加 `tool_calls`（該場目前已回覆的工具呼叫**次數**，不含內容；只在 `live` 為 `running`／`stopping` 時出現），供主持人判斷 A 是否已在工作、何時可以示範停止（第 6.4 節）。它屬場次程序狀態，與 `live` 同一層，host 與 operator 都可讀。

## 5 雙角色投影與選取

### 5.1 名詞

| 名詞 | 意思 |
|---|---|
| 角色欄位（slot） | 每位 consumer 一個，穩定鍵是 `consumer_id`（不寫死 A／B，但本片只有兩個） |
| 欄位世代 `slot` | 該 consumer 換 task 或欄位被清空時遞增；只影響這一位客人 |
| 場景世代 `scene` | 整頁換憑證、進出歷史回放、全頁重設時遞增；清空整個場景 |
| 欄位內容 | 該 consumer 目前呈現的 `task_id`、已提交事件的序號、階段與裁決 |
| 選取 | 主持人正在查看哪一位；只決定面板，與場景無關 |

### 5.2 橋接 v2

**Web → Unity**（`SendMessage("Bridge","Receive", json)`，現行通道）：

```json
{"v":2,"scene":7,"consumer":"consumer-a","slot":3,"task_id":"show-a-02","seq":3,"phase":"waiting","verdict":"Ask","reason":"HUMAN_APPROVAL_REQUIRED","animate":true,"cause_seq":3,"task_end":false}
{"v":2,"scene":7,"consumer":"consumer-b","slot":5,"clear":"slot"}
{"v":2,"scene":8,"clear":"scene"}
```

**Unity 的處理**（每位客人一份 v1 現有的狀態，外面包一層場景世代）：
- `scene` 比目前舊：Stale，丟棄；比目前新：清空**所有**客人，採用新的場景世代。
- 同一場景內，依 `consumer` 找到該客人的欄位：
  - `slot` 比目前舊：Stale；比目前新：只清空**這位**客人，採用新的欄位世代；
  - 同一欄位世代、不同 `task_id`：Invalid（一個欄位世代只有一個 task，取代現行「一個世代只有一個 task」）；
  - 序號倒退、重複：沿用 v1 的 Older／Duplicate 規則，**以該客人自己的序號判斷**，A 的序號不會推進或壓住 B。
- 未知的 consumer：Invalid，不生成角色。
- v1 訊息照舊：單一客人語意（新世代清空全部、另一位調暗）。`/` viewer 頁與歷史回放繼續使用 v1。

**Unity → Web 回報**：
- `rendered`：`APA_Report("rendered", scene, seq, "consumer|slot|task|phase|verdict|reason")`。頁面只在 `scene`、`consumer`、`slot`、`task` 都對得上自己的欄位時，才把那一位標成同步完成；B 的回報不會完成 A 的等待（現行 `renderedBy` 只比對世代與序號，v2 改為依欄位比對）。
- 新增 `bridge`：Unity 在 `ready` **之前**送 `APA_Report("bridge", 0, 0, "1,2")`，宣告支援的橋接版本。現行頁面不認得 `bridge` 這個種類，會忽略它，所以舊頁面配新 Unity 不受影響。
- Unity 只回報它實際收到並呈現的欄位；網頁推導的請購對應繼續標示為推導（現行）。

**版本相容**（不容許靜默少顯示一位）：

| 頁面 | Unity | 行為 |
|---|---|---|
| 新 | 新（宣告 2） | v2，兩位同時呈現 |
| 新 | 舊（沒有 `bridge` 回報） | 改用 v1，只呈現**選取中**的一位，並在場景上方顯示「此 Unity 建置只支援單一客人：場景只呈現選取的 {A／B}；另一位的資料仍在面板」 |
| 舊 | 新 | 舊頁面只送 v1，Unity 照 v1 單客人處理（現行行為） |

### 5.3 場景呈現

- 兩位都完整亮度（v2 不再調暗另一位），各自走、停、離場；`ShopView` 已有兩個客人物件與各自的目標、路線、期限與回呼，走路本來就逐位推進。
- 店內的共用點位（貨架、櫃台前、櫃台）加每位客人的固定偏移（例如 A 偏左、B 偏右），兩人同時到櫃台不重疊。不新增尋路、不加素材。
- 狀態列改成兩行，各寫一位客人的階段。
- 沿用 D2 規則：事件先行（只有已提交事件才觸發動作）、單次過場以真實時間計最長 2.5 秒、追上時加速；這些本來就逐位計算。
- 兩位同時走路：互不等待；一位的 2.5 秒上限不影響另一位。

### 5.4 主持人頁的資料與選取

- 每個角色欄位一份狀態（現行 `adapter` 的單一 state 拆成兩份）：自己的 task、快照、已見事件、欄位世代、輪詢、送給 Unity 的目標與動作、最近的回報。
- **欄位放哪個 task**：該 consumer 最新一場 host 場次的 task（`/api/host/runs` 每 2 秒的列表）；主持人也可以在該 consumer 的 task 列表手動選較早的 task。換 task 時該欄位的世代加一，Unity 只清這一位。
- **輪詢**：每個欄位各自每秒讀 task 與事件（現行的一致性重讀規則照舊），用該 consumer 的 viewer token。兩個欄位互不等待：A 讀取失敗或逾時，只把 A 標成「讀取失敗」，B 照常更新。
- **選取**：只決定請購卡、Timeline、Inspector 與 trace 顯示哪一位；切換選取不呼叫 `forget()`、不清 Unity、不改欄位世代。請購卡標題改成「正在查看：A 的請購（show-a-02）」，另有場景摘要「場景：A #3 等待核准 · B #2 報價中」，兩者分開。
- **晚到回覆**：每個欄位用自己的世代檢查；舊欄位世代或舊場景世代的回覆一律丟棄。選取切換不使任何欄位的回覆失效。
- **刷新**：由 Store 還原兩個欄位（各自最新場次的 task），首次載入不播動畫（現行 D2 規則逐位套用）；刷新不會開始任何場次。
- **一位到期或離場**：只更新那一位；離場後角色回到店外，欄位保留到該 consumer 下一場開始。
- **trace**：「CLI 執行中」的判斷改為依 task 查（現行 `running(taskID)` 只看單一場次）；慢 trace 只影響該位的 trace 面板。

### 5.5 歷史回放

本片不升級為雙場。進入回放時場景世代加一，暫停兩個欄位的輪詢，回放沿用 v1 單客人；離開時場景世代再加一，兩個欄位恢復輪詢並由 Store 還原。單筆回放的既有行為與測試不得退化。

## 6 真實工作重疊的證據（M-C）

### 6.1 現有紀錄夠不夠

| 紀錄 | 有的時間 | 用途 |
|---|---|---|
| trace（每場結束時寫出） | 只有 `run_start.at`（秒）；`tool_call` 沒有時間 | 不足以量測重疊 |
| Store 事件 `observed_at` | 秒，只有任務開始、請購、裁決等事件 | 太粗，也不涵蓋列目錄、查報價 |
| `host_runs` | 受理、結束、中斷時間（秒） | 只能得到程序區間，不能證明區間內有工作 |

**結論**：需要最小新增——**每次工具呼叫的事發時間**。

### 6.2 最小新增：工具呼叫時間紀錄

- 在 `toolbridge` 的每場 `Binding` 呼叫紀錄（現行已有、有上限）加兩個欄位：收到呼叫的時間 `received_at`、回覆的時間 `answered_at`（Host 時鐘，Unix 毫秒）。兩場在同一個 Host 程序，時鐘相同，可直接比較。
- 場次結束時，Host 在 trace 旁另寫一個時間紀錄檔 `<task>-<nanos>.timing.json`（白名單）：
  - `run_id`、`task_id`、`consumer_id`、`mode`（fake／live）；
  - 程序開始與結束時間；
  - 每次呼叫：`tool`、`product_id`、`outcome`、`received_at`、`answered_at`；
  - `written_at`：檔案寫出的時間。
  時間在**事發時**記錄（收到與回覆呼叫的當下），檔案在結束時寫出；兩者分開記錄，不把寫出時間當成事發時間。
- **不改 M1 的 trace 格式與檢查器**：M1 的 live 檢查器嚴格解碼 trace，新增欄位會被判為未知成員，所以時間紀錄寫成另一個檔案。
- 不保存模型原文；不需要串流平台。

### 6.3 重疊判定

- 每場的**有效工作區間** E = [第一次工具呼叫的 `received_at`, 最後一次的 `answered_at`]，必須落在該場程序的 `running` 區間內，且至少有 2 次工具呼叫（區間長度大於 0）。這符合路線 §6.2 的定義：程序仍在執行，且區間內有該場自己的真實工具呼叫。
- **重疊成立**的條件全部要有：
  1. 兩場是不同 consumer、不同 run、不同 task；
  2. 兩場都是 live 模式，在同一個 Host 程序；
  3. E_A 與 E_B 的交集長度大於 0。
- **只有 task 重疊**：程序區間或 Ask 等待期間重疊，但 E_A 與 E_B 沒有交集，判為「只有 task 重疊」，**不通過**。CLI 閒置、Ask 期限、回放與動畫都不算。
- 判定由一個唯讀小工具讀兩個時間紀錄檔與 `host_runs` 完成（M-C 實作），輸出兩個區間、交集與判定；不宣稱知道模型供應商內部是否同時推論。

### 6.4 有界的開始方式與停止隔離

- **開始**：主持人按「A、B 一起開始」，兩個請求相隔不到一秒送出。真實的單筆 Ask 一場約 10 秒，3 次工具呼叫分散在其中，兩場同時開始時有效工作區間通常會交集。這是**一次**嘗試：沒有達到重疊就照實記為「未達目標」，不重試到通過；下一次嘗試需另行核准。不加假工具呼叫、不讓工具卡住、不加睡眠。
- **停止隔離**（路線 §6.2 第 3 步）：主持人頁顯示每場的 `tool_calls` 次數。在 A 仍是 `running` 且已有至少 1 次工具呼叫、B 也仍是 `running` 時，才停止 A。證據：
  - A 的時間紀錄在停止前有工具呼叫，Store 有 A 的 `TASK_CANCELLED`；
  - B 在 A 的停止時間之後仍有 `answered_at`，並以自己的結果結束。
  A 已結束時，不能記成「成功停止活躍的 Agent」。真實的時間窗很短，若這次沒能在條件成立時停止，照實記為「停止隔離未示範」，不重試。
- **fake 測試**：fake CLI 加一個只在測試用的步驟 `wait_file`（等待某個檔案出現才繼續），用握手精確控制兩場交錯與停止時機。它只在 fake 情境使用，不進 live。
- 真實執行的確認清單在 M-C 另行取得核准；本設計的 review 不代替執行核准。

## 7 介面變更一覽

| 介面 | 變更 | 權限 | 錯誤 |
|---|---|---|---|
| `POST /api/host/runs` | 請求不變；名額改為每 consumer 一場、總數 2 | operator（現行） | 新增 `409 consumer_busy`（帶 `run`）、`409 capacity_full`（帶 `runs`）；其餘現行 |
| `POST /api/host/runs/{id}/stop` | 依 run ID 在進行中的場次查找 | operator | 現行 |
| `POST /api/host/tasks/{id}/cancel` | 依 task 查進行中的場次 | operator | 現行 |
| `GET /api/host/status` | `runs.active` 由單一物件改為陣列 `[{run_id,task_id,consumer_id,live}]`（最多 2） | host、operator | 現行 |
| `runView` | 增加 `tool_calls`（次數；只在 `live` 為 `running`／`stopping` 時出現） | host、operator | — |
| `GET /api/tasks/{id}/trace` | 「執行中」改依 task 判斷 | 現行 | 現行 |
| 橋接 | v2 訊息、`bridge` 回報、`rendered` 細節帶 consumer 與欄位世代；v1 保留 | — | Invalid／Stale 規則見第 5.2 節 |
| 時間紀錄檔 | `<task>-<nanos>.timing.json`（新） | Host 本機檔案，不經 HTTP | — |
| 可信設定 | `scenarios.json` 加 `ask-b`（consumer-b 主商品 Ask）；`scenarios-live.json` 加 `live-b` | — | — |

不改事件契約、Store 的付款語意或 M1 的證據格式。

## 8 檔案影響

| 範圍 | 檔案 | 內容 |
|---|---|---|
| Host | `cmd/demo/control.go`、`cancel.go`、`live.go`、`main.go`（註解） | 進行中場次對照表、名額與錯誤碼、`summary`、依 task 判斷執行中 |
| 工具橋 | `internal/toolbridge`（呼叫紀錄）、`internal/buyer/host.go`（寫時間紀錄檔） | 呼叫時間、時間紀錄檔 |
| fake | `internal/buyer/fakecli`、`testdata/cli/` | `wait_file` 步驟與雙場測試情境 |
| 設定 | `testdata/demo/scenarios.json`、`scenarios-live.json` | `ask-b`、`live-b` |
| 網頁 | `web/adapter.js`、`web/app.js`、`web/host.js`、`web/host.html`、`web/unity.js` | 每 consumer 的欄位狀態與輪詢、選取、v2 投影與回報、按鈕與文字 |
| Unity | `Projection.cs`、`ShopBridge.cs`、`ShopView.cs` | 場景世代加每位客人的欄位、v2 解析、逐位清空、偏移、兩行狀態、`bridge` 回報 |
| 測試 | `cmd/demo/*_test.go`、`web/test/*.mjs`、Unity EditMode／PlayMode、新的雙場瀏覽器腳本 | 第 10 節 |
| 文件 | `docs/demo-operation-design.md`、`demo-ui.md`、`demo-presentation-design.md`、`m1-boundaries.md`（加註）、README | 依第 1 節同步 |

不需要改：Store schema、事件契約、Policy、Gate、認證清單、CLI 版本釘選、plugin 設定、M1 的產包器與檢查器。

## 9 順序、出口與任務編號

| 片 | 內容 | 出口（可集中 review） | 核准 |
|---|---|---|---|
| **M-A**（本文件） | 最小設計與範圍修訂 | 本文件經 Codex review | — |
| **M-B** | 雙場 Host、資料隔離、Unity 雙角色、最小主持人選取、時間紀錄檔；**只用 fake** | 一次集中 review：Host 名額與停止隔離、網頁與 Unity v2、時間紀錄與重疊判定工具（以 fake 握手產生的交錯資料驗證），以及第 10 節的回歸 | 設計 review 通過後核准實作 |
| **M-C** | 兩位真實 Agent 的受控展示：重疊證據、停止隔離 | 真實執行的時間紀錄、重疊判定、兩場的結果與畫面；未達目標照實記錄 | 每次 live 執行另行核准 |
| 後續 | Admin 活動／請購營運總覽；之後隨付款整合加入結算金額與鏈上證據 | 另行設計 | — |

- 相對路線文件舊版 M-B（含 Admin 總覽），本片收斂為雙場與角色選取；主持人頁的 A／B 場次狀態是控制用途，不是營收儀表板。
- 付款、20 個付款帳號、ZK 不是開始 M-B 的前置條件；這些最終商業展示的要求也不因此刪除。
- **M-B 的實作順序**（同一個 review 出口，不拆成多次審批）：
  1. Host 名額、錯誤碼、停止與取消依場次或 task 查找，加上 Go race 測試；
  2. 工具呼叫時間與時間紀錄檔、fake 的 `wait_file`、重疊判定工具；
  3. Unity v2（`Projection`、`ShopBridge`、`ShopView`）與 EditMode／PlayMode 測試；
  4. 網頁的欄位狀態、輪詢、選取與 v2 投影，加上 node 測試；
  5. 雙場瀏覽器流程與既有流程的回歸。
- **任務編號建議**：B1 卡都已 Done，不占用。建議新增 **B2-01 雙 Agent 商店（M-B）**、**B2-02 兩位真實 Agent 工作重疊（M-C）**，Admin 總覽之後為 B2-03。M-A／M-B／M-C 簡稱保留。若「第二批」的 B2 編號已保留給付款模組，由 Aaron 指定其他編號。
- **分支建議**：設計 review 通過後，從本文件的 commit 開新分支（例如 `b2-dual-agent`）實作 M-B；`b1-04-live` 保留 M1 的狀態。本輪不建立分支。

## 10 後續驗收與測試成本

### 10.1 最小驗收表

| 情境 | 必須成立 | 主要驗證 |
|---|---|---|
| A、B 交錯執行 | 各自的 task、請購、事件鏈、錢包參照與畫面一致；不串資料 | Go（fake，`wait_file` 交錯）、瀏覽器 |
| 同人重複開始、重送、名額已滿 | 不重複執行、不超過 2；`consumer_busy`、`capacity_full`、`request_id_conflict` 可理解 | Go |
| 停 A、B 繼續 | 只取消 A 的 context、程序群組與 capability；兩場各有收尾結果 | Go（race）、瀏覽器 |
| A 慢 trace、失敗或到期 | B 的資料、輪詢與動畫不被拖住 | Go、node、瀏覽器 |
| viewer 越權、晚到回覆、舊 Unity 回報 | 不暴露其他 consumer；舊欄位或舊場景世代的回覆與回報不覆蓋新狀態；B 的回報不完成 A | Go、node、Unity |
| 新頁面配舊 Unity | 顯示「只支援單一客人」說明，不靜默少一位 | node、瀏覽器 |
| 刷新與服務重啟 | 不自動執行模型、不混場；中斷與未確認照實顯示 | Go、瀏覽器 |
| 單場 fake、live 接線替身、單筆歷史回放、`/` viewer 頁 | 必要的既有行為不退化；不重開 M1 驗收 | 既有測試 |
| 重疊判定工具 | 交集為正才通過；只有 task 重疊、單一工具呼叫、不同 Host 程序都不通過 | Go 或 script 單元測試 |
| 真實工作重疊 | M-C 另以真實兩場的證據判定 | M-C |

### 10.2 測試與耗時

- **每次修改**：只跑受影響的定向測試。
  - `go test -race` 跑 `./cmd/demo` 的場次與取消測試（現行套件約 1～3 分鐘）、`./internal/toolbridge`、`./internal/buyer`；
  - `node --test web/test/*.mjs`（約 10 秒）；
  - Unity EditMode／PlayMode（以 `scripts/unity/unity.sh` 執行，約 1～2 分鐘，需要 Unity）。
- **M-B 出口前一次**：
  - 新的雙場瀏覽器流程（預估 3～5 分鐘）；
  - 既有的 `browser-ui.mjs` 回歸（約 7 分鐘）與歷史回放、live 替身流程（各約 1～2 分鐘）。
- **不預設跑**：`make m1-fixture`（只在改到 fixture 或證據共用程式時才跑，約 8 分鐘）、完整容器（必要時由實作派工明列次數與範圍，約 15～20 分鐘）、全部突變與全部反向清單。
- **反向檢查**只集中在關鍵保護：
  - 名額上限、同 consumer 拒絕；
  - 停止只作用於指定場次、capability 只撤銷該場；
  - 欄位世代的晚到防護、Unity 的 Invalid／Stale；
  - 舊 Unity 的明示退回；
  - 重疊判定（只有 task 重疊不得通過）。
  沿用 `m1-e3-reverse.py` 的作法：每項指定對應測試與預期斷言，在一次性 clone 中執行。
- **進度**：耗時的驗證沿用 `cmd/m1-check` 的 `--plan`，顯示步驟、基準、elapsed、log 位置與 PASS／FAIL／NOT_RUN。失敗先保存紀錄並定位，不原樣重跑到轉綠。

## 11 需要 Aaron 決定的事項

雙 Agent 重疊的方向已接受，不再列為待決。只剩：
1. **M-C 的真實執行**：兩個場次同時使用同一個本人訂閱（`~/.apa-claude`）的用量與限流風險，以及核准幾次嘗試（重疊一次、停止隔離一次，或合併為一次）。
2. **正式規則文字**：第 1 節的建議新規則，在 review 通過後由 Aaron 確認再同步到 PRD、執行計畫與後續任務卡。
3. **任務編號**：若 B2 已保留給付款模組，指定雙 Agent 卡的編號。

其他細節（錯誤碼、欄位世代、偏移、檔名、測試範圍）是本文件的推薦，由 review 決定是否調整。

## 12 本片不做的事

- 20 人或更多同時執行、效能測試、排程平台、分散式 runtime；
- Admin 聚合、營收數據；
- 付款、grant、真實核准、ZK、Signer、鏈上交易；
- Unity 內點選角色（D3）、新的尋路或素材；
- 改 Store 的付款語意、事件契約、M1 證據格式與檢查器；
- 為了重疊而加假工具呼叫、卡住工具或睡眠。
