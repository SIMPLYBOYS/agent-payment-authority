# B1-06 交付點 2：Unity＋LimeZu 商店與 Dashboard

狀態（2026-10-05）：**B1-06 交付點 2 於 `6aee7f0` 通過 Codex 限定範圍技術 review**（文件 `eb97e61`；第 11 節）。R1～R3 與第二輪兩項修正關閉，未發現新的阻擋問題。通過的是 Unity＋LimeZu 與 Dashboard 的 fake 展示資料鏈，不代表真實模型請購、付款或整體 M1 完成。B1-04／B1-06 整體仍 InProgress，未合併 main、未標 Done；素材與 Web 建置公開發布仍未核准。第 7–10 節保留當時的寫法，「待 review」是寫作當時的狀態。

歷程：Codex 對 `21db978` 的限定 review 判定暫不通過；依 vault 筆記 `Claude-Code-B1-06-交付點2修正指示-21db978-20261005` 修正 R1～R3 於 `eda6469`（第 9 節）；Codex 對 `9d3c4e4` 的 review 指出兩處待修，修正於 `6aee7f0`（第 10 節）。

原交付：Claude Code 實作，依據 vault 筆記 `Claude-Code-B1-06-交付點2-Dashboard與商店實作指示-20261005`（新版：商店固定為 Unity＋LimeZu，獨立於 pixel-office），開工基準 `837f667`，分支 `b1-06-demo-integration`（未合併 main）。第 7 節是當時的紀錄。

**這一輪完成的是 fake UI 資料鏈**：本機商店 HTTP 402 → fake Buyer → Store → 已認證 HTTP → JavaScript adapter 的同一份投影 → Unity 商店與網頁 Inspector／Timeline。真實模型請購、付款與整體 M1 都沒有被證明；B1-06 的完整完成仍需至少一次核准的真實 CLI trace。

## 1 畫面

頁首固定「M1／fake agent／不付款」。三區：

| 區 | 內容 |
|---|---|
| 商店（Unity Web 建置） | 一間 LimeZu 雜貨店：入口、走道、貨架、冷藏櫃、櫃檯，以及 A／B 兩位像素客人（標示 sim agent）。只有目前 viewer 的客人會移動 |
| Inspector（網頁） | consumer、agent、wallet、wallet 證據、task、payer、task 狀態、快照序號、餘額與剩餘預算（API 未提供，明寫未知）、付款（未付款）；每筆請購的資源、payTo、amount（基本單位整數字串）、asset、chain、intent hash、裁決、reason、approval、payment_status |
| Timeline（網頁） | 事件序號、類型、時間、裁決或固定代碼、來源與 evidence_mode；每筆可展開 API 白名單 JSON，標明「不是 CLI 原始輸出，也不是完整未遮罩 raw log」 |

場景對應（只看已提交的事件，不新增事件型別）：

| 事件 | 商店 |
|---|---|
| `TASK_STARTED` | 進店（門口） |
| `PURCHASE_PROPOSED` | 取得報價（走道） |
| `POLICY_DECIDED` Ask | 在櫃檯等待人工核准（M1 無法核准） |
| `POLICY_DECIDED` Deny | 離店 |
| `POLICY_DECIDED` Allow | 櫃檯，標示「M1 無付款能力」 |
| `APPROVAL_EXPIRED`／`APPROVAL_WITHDRAWN` | **場景顯示的那筆請購**的 Ask 已到期（後端已記錄）／已撤回，離店 |
| `TASK_CANCELLED`／`TASK_TIMED_OUT` | 只加註；Deny 之後發生時不改變原 Deny 與原因 |

不會出現付款成功、成功取貨或鏈上完成。

**場景顯示哪一筆請購**（`web/adapter.js` 的 `scene()`，修正 R1）：

- `PURCHASE_PROPOSED`、`POLICY_DECIDED`、`APPROVAL_EXPIRED`、`APPROVAL_WITHDRAWN` 以 `purchase_order_id` 歸到各自的請購，沒有整個 task 共用的收尾旗標。
- 場景顯示**事件序號最大的 `POLICY_DECIDED` 所屬的請購**；還沒有任何裁決時，顯示最新提出的請購。
- 場景的 verdict、reason、approval 收尾與期限判斷都取自這一筆。舊請購的到期或撤回不改變它。
- 沒有 `purchase_order_id`、或對不上任何請購的收尾事件，一律不採用；沒有對應提出事件的裁決顯示 unknown。不會自行拼出到期或成功。
- Deny 後的取消／CLI 逾時仍只加註，不覆蓋 Deny 原因。
- Inspector 照舊列出每筆請購與 task 狀態；task 有多筆請購時，場景文字註明「此 task 有 N 筆請購；場景顯示最近裁決的 po」。

## 2 Unity 環境與 LimeZu 素材

- **Editor**：Unity **6000.5.3f1**（Unity 6.5），已安裝 WebGLSupport 模組；本機唯讀確認，未安裝或更新任何東西。專案釘選在 `unity/AgentPaymentAuthority/ProjectSettings/ProjectVersion.txt`。
- **套件**：`Packages/manifest.json` 只用 editor 內建的套件：`com.unity.2d.sprite` 1.0.0、`com.unity.test-framework` 1.7.0，以及 imageconversion、imgui、jsonserialize 模組。沒有從網路下載套件。
- **LimeZu 來源**：Aaron 10/5 指定的素材包位置 `…/pixel-office/limezu`，使用其中的 **Modern_Interiors_v41.4**。
  - 只複製檔案，不引用 pixel-office 的程式、場景或 Library；本專案也不依賴那個目錄的其他內容。
  - 紀錄：派工筆記原本要求不依賴 pixel-office。10/5 Claude Code 在交付點 2 實作中詢問素材位置，Aaron 回答 `/Users/mac/Documents/OpenSourceProjects/pixel-office/limezu`（Claude Code 對話紀錄 `b6b47224-92d2-4202-a040-e6fd70ed9470`）。這記為原禁止事項的例外，範圍只是一次性把清單內的檔案複製到本專案，執行時不依賴該目錄。這個指定只說明素材從哪裡來，不證明授權範圍，也不構成發布核准。
- **授權**（`Modern_Interiors_v41.4/LICENSE.txt`）：
  - 可：編輯並用於商業或非商業專案；
  - 不可：轉售或散布素材；
  - 須：署名 limezu.itch.io。
  - 因此：原始素材、匯入後的副本與含素材的 Web 建置都**不進 git**，也不發布；頁面與 Unity 場景都標示署名。是否可以公開散布建置輸出（例如部署成網站），**待 Aaron 確認**，目前只在本機提供。
- **使用項目**：`unity/AgentPaymentAuthority/limezu-assets.txt` 列出 16 個檔案，各有穩定的 key、原始相對路徑與裁切範圍：
  - 地板與牆（Room_Builder）；
  - 貨架 ×4、冷藏櫃 ×2、櫃檯 ×2、OPEN 牌、購物車、水果架（Grocery Store singles）；
  - 兩個 Premade 角色；
  - 授權檔。
- **匯入**：`LIMEZU_DIR=<素材包目錄> sh scripts/unity/import-limezu.sh`
  - 只複製清單內的檔案到 `Assets/LimeZu/`（git 忽略），並把每個檔案的 sha256 記在 `Assets/LimeZu/IMPORTED.txt`；
  - 缺檔時 exit 3。
- **場景**：`sh scripts/unity/unity.sh scene OUT`。由 `ShopBuilder` 依清單重建 `Assets/Scenes/Shop.unity`。
  - 場景會參照匯入後（被忽略）的檔案，所以**不進 git**；每台機器匯入後重建即可。
  - 檔案缺少時用 Unity 內的純色 placeholder，場景頁首顯示「LimeZu pending: placeholder art」，頁面顯示「placeholder，LimeZu 待接入」。這種狀態不算正式視覺完成。
- **Web 建置**：`sh scripts/unity/unity.sh webgl OUT`。輸出到 `unity/AgentPaymentAuthority/Build/WebGL`（git 忽略）。
  - 不壓縮，所以不需要額外的 Content-Encoding header；
  - 背景執行開啟；
  - 檔名為 `WebGL.loader.js`、`WebGL.framework.js`、`WebGL.wasm`、`WebGL.data`。
- **橋接**依本機 6000.5.3f1 文件（`Documentation/en/Manual/web-interacting-browser-*.html`）：
  - 頁面 → Unity：`unityInstance.SendMessage("Bridge", "Receive", json)`，只接受單一字串參數；
  - Unity → 頁面：`Assets/APA/Plugins/WebGL/apa-bridge.jslib`（ES5，`mergeInto(LibraryManager.library, …)`）呼叫 `window.apaUnityReport`。

## 3 資料責任與橋接

| 角色 | 做什麼 | 不做什麼 |
|---|---|---|
| Go／Store | 保存身分、請購、裁決與事件；唯一的業務狀態來源 | — |
| `web/adapter.js` | 持有 viewer token（`app.js` 的記憶體）、讀 HTTP、取得一致快照、產生唯讀投影 | 不產生裁決、到期或付款 |
| Unity（`ShopBridge`、`ShopView`） | 接收投影，呈現場景、角色與移動 | 不持有 token、不讀 API、不開 DB；回報只用於 ready 與呈現確認 |
| Dashboard（`render.js`） | 與 Unity 用同一份快照，呈現 Inspector、Timeline 與場景文字 | 不把內容當 HTML |

**投影訊息**（`ProjectionMessage`，版本 1）只有以下欄位：

- `v`、`gen`（頁面世代）、`clear`；
- `consumer`、`task_id`、`seq`（快照的 `as_of_sequence`）；
- `phase`、`verdict`、`reason`、`animate`。

沒有 token、API 位址或自由文字，金額不經過 Unity。Unity 對訊息的處理（`ProjectionState`，有 EditMode 測試）：

- 版本不符或欄位不合法：Invalid。整則檢查完才可能重置，所以不合法的訊息不會清掉畫面。
- 較舊世代：丟棄（Stale）。較新世代：先清空舊客人，再顯示。
- 同一世代只能有一個 task，序號不能倒退（Older），同序號同 phase 不重播（Duplicate）。
- 每個序號只演一次動畫；首次載入不演動畫（保存的快照不是新事件）。
- 舊世代的動畫回呼與訊息都丟棄。
- Unity 未就緒時，頁面只保留最新一則訊息，就緒後送出，不無限排隊。
- 載入或啟動失敗時顯示「Unity 場景未就緒」，不換成其他商店。

**路由**（`cmd/demo/ui.go`）：

| 路由 | 內容 |
|---|---|
| `GET /` | Dashboard 頁 |
| `GET /ui/{name}` | 只有 `app.js`、`adapter.js`、`render.js`、`unity.js`、`style.css`，都嵌入 Go 二進位 |
| `GET /unity/{name}` | 只有上述四個建置檔，從 `--unity-build` 目錄以 `os.Root` 開啟，名稱或連結都無法離開該目錄 |
| `GET /api/viewer` | 只回已認證 viewer 的 `role` 與 `consumer_id`；主持人 token 403，MCP capability 401 |

- 每個頁面回應都帶 CSP：`default-src 'none'; script-src 'self' 'wasm-unsafe-eval'; …; frame-ancestors 'none'`。`'wasm-unsafe-eval'` 只允許 Unity 編譯 WebAssembly，不允許 JS eval。
- 另有 `X-Frame-Options: DENY`、`no-store`、`nosniff`、`no-referrer`。
- 頁面與建置檔也檢查 Host；不開 CORS。

## 4 一致性與狀態規則（`web/adapter.js`、`web/app.js`）

- task 與 events 兩次讀取的 task ID 與 `as_of_sequence` 相符，才提交成一份快照。不相符時最多重取 3 次，仍不符就保留最後一致的快照，標示「更新中」。
- 較舊世代的回應、比已顯示更舊的快照都丟棄；事件依 `event_id` 去重。
- 輪詢約每秒一次（目標頻率，不是延遲保證），同時最多一個請求；頁面隱藏時暫停，`pagehide` 時停止並清除 token。
- API 失敗時顯示「離線／更新失敗」與最後快照時間，不顯示成功。401 清除 viewer；404 一律「找不到這個 task，或你沒有查看權限」。
- 期限已過但收尾事件尚未提交時，只顯示「已達期限，等待後端確認」，以 Store 的 `served_at` 判斷，不用瀏覽器時鐘。
- Ask 原裁決與 approval 收尾分開顯示；Deny 之後的取消或 CLI 逾時不覆蓋原 Deny 原因。fake 宣稱已付款的文字不是事件，不會出現在畫面上。
- token 只在 `app.js` 的記憶體，請求只用 `Authorization: Bearer`。不放 URL、cookie、localStorage、sessionStorage、console、Unity 或截圖。輸入欄位有遮罩，讀取後清空；重新整理後要重新輸入。
- **兩種模式**（修正 R2）：
  - **live**：輪詢讓商店與兩個面板維持最新一致快照。只有一個輪詢迴圈：每次排程前先清掉舊 timer。
  - **replay**（歷史展示回放）：
    - 進入時停止輪詢 timer、中止進行中的讀取，並開新的頁面世代。
    - 回放期間輪詢、`visibilitychange` 與舊回應都不改寫畫面。狀態列保持「歷史展示回放」，商店上方註明 Inspector 與 Timeline 仍是最新快照，與商店不是同一時刻。
    - 每一步用該事件前綴算出的 phase、verdict 與 reason；等 Unity 回報這一步已呈現（最多 6 秒）再走下一步。
  - **恢復**：回放結束或按「停止回放並恢復最新狀態」時：
    - 再開一個新世代，先清空商店；
    - 重新讀取一致的最新 task／events；
    - 不帶動畫送出完整快照。即使序號與回放最後一步相同，Unity 也會因世代較新而接受，不放寬舊序號規則。
  - 新回放、切換 task 或 consumer、`pagehide` 都會讓目前的回放失效；晚到的 timer 與 Unity 回呼不做任何事。恢復中的商店顯示「Unity 同步中」，讀取失敗沿用「離線／更新失敗」，不拿舊的 Unity 回報當成功。
  - 回放唯讀：不送出請購、不重啟 task、不改時間、不寫事件。
- **展開的 JSON**（修正 R3）：以 `event_id` 記住使用者展開的事件。重繪（輪詢、新事件、暫時讀取失敗）時保留展開與收合；手動收合的不會被自動展開。切換 consumer 或 task 時清除。
- Unity 的 `rendered` 回報帶 `phase|task|verdict|reason`。頁面把它寫在 `#unity-status` 的 `data-*` 屬性上，只供觀測，不含 token，也不是業務控制入口。
- 清除身分、切換 task 或 consumer、task 找不到時，`#unity-status` 的文字與 `data-*`（世代、序號、task、phase、verdict、reason）立即清除：
  - 沒有 task 時顯示「Unity 場景就緒（…）：等待選擇 task」；
  - 開啟中的 task 尚未呈現時顯示「Unity 同步中」；
  - 載入中或未就緒的狀態不變。
  - 舊世代晚到的 `rendered` 回報由 `unity.js` 丟棄（只接受最後送出的世代），不會恢復上一筆內容。

## 5 操作

**前置條件**：

- Go 1.26.4，Go 模組已下載；
- Node 22（只有測試需要）；
- Unity 6000.5.3f1 與 WebGL 模組；
- LimeZu Modern Interiors 素材包；
- 瀏覽器測試另需本機 Google Chrome。

不依賴其他個人 repo。

```sh
# 1 匯入素材、建置 Unity（素材或 Unity 有變更時才需要）
LIMEZU_DIR=/path/to/limezu sh scripts/unity/import-limezu.sh
sh scripts/unity/unity.sh webgl artifacts/b1-06/unity-local

# 2 啟動 fake demo：商店、Store、到期 worker、API、Dashboard，以及 task-a／task-b 的 fake run
make demo-integration                      # http://127.0.0.1:8402/

# 3 取得自己的 viewer token（只印 consumer-a 的那一個；不要放進 URL 或分享）
python3 -c 'import json;print([t["token"] for t in json.load(open("var/demo-tokens.json"))["tokens"] if t.get("consumer_id")=="consumer-a"][0])'
```

在頁面上：

1. 把 token 貼進遮罩欄位，按「驗證」。
2. 選 `task-a（demo fixture）`，或輸入 task ID。
3. 要看 B：按「清除／切換 consumer」，再輸入 consumer-b 的 token，選 `task-b`。

UI 上的選擇不構成授權，伺服器仍依 token 與 task 已保存的歸屬判斷。

- **首次載入**：fake run 在服務就緒後就跑完了，頁面顯示**保存的快照**，不演動畫，不假裝剛發生。
- **重新整理**：token 消失，需要重新驗證；之後從 Store 還原同一份狀態。
- **歷史展示回放（唯讀）**：用新的頁面世代，依保存的事件逐步重演商店畫面；回放期間停止輪詢，Inspector／Timeline 保留最新快照並標示與商店不同時刻。結束或按「停止回放並恢復最新狀態」後，重新讀取並完整恢復最新狀態（第 4 節）。不重跑 task、不改事件或時間、不寫任何資料（瀏覽器測試會比對前後的事件內容與序號）。
- **新 task**：重新展示不需要刪除資料。重啟服務時，已保存的 task 不會重跑；要新的 run，請在計畫裡改用 `task-a-next`（同 consumer、同錢包），或設定新的 `APA_DB_PATH`。不要把刪除資料庫當成預設的重跑步驟。

## 6 測試

| 層 | 命令 | 涵蓋 |
|---|---|---|
| JS adapter | `node --test web/test/adapter.test.mjs`（也在 `make check-contracts`） | 一致性、舊世代、舊快照、去重、Ask／Deny／Allow、期限未提交、取消／逾時保留 Deny、惡意字串、投影欄位；預期值取自 `cases.json`。R1：Store 建立的兩種多請購歷史、只有自己的到期／撤回才離店、不明關聯不收尾（11 項） |
| Store 歷史 | `go test ./internal/store -run TestDisplayHistories` | 用正式 Store 寫出「請購 1 到期後請購 2 Ask」與「兩筆 Ask 後請購 1 到期」，存成 `testdata/demo/display-histories.json`（API 回應的形狀），adapter 測試讀它；`-update-histories` 重產。`TestSeedDisplayDB` 只在設定 `APA_SEED_DB` 時寫入瀏覽器測試用的資料庫 |
| Unity EditMode | `sh scripts/unity/unity.sh test OUT` | `ProjectionState`：去重與單次動畫、首次載入、舊世代／舊序號、世代清空、單 task、不合法輸入 |
| Unity PlayMode | 同上 | 真場景經由 Bridge：Ask 走到櫃檯且重複不重播、Deny 離場、切換世代清空、舊世代回呼與訊息丟棄、LimeZu／placeholder 標示；`rendered` 回報帶 task、verdict 與 reason |
| HTTP | `go test ./cmd/demo -run TestDashboard` | 頁面與資產、CSP／Host、只提供列名檔案（DB、token、repo、建置目錄其他檔、外連 symlink 都拒絕）、`/api/viewer`（host 403、MCP 401、無 token 401、Host／Origin、token 放 URL 無效）、載入頁面不寫事件、沒有建置時 404 |
| 實際瀏覽器 | `node scripts/verify/browser-ui.mjs` | 本機 Chrome（headless、拋棄式 profile、SwiftShader），經 DevTools protocol 操作，不用套件、不錄 HAR／trace；完整清單見第 7 節 |

`make check`、`make check-contracts` 與斷網容器驗證仍涵蓋既有的 Go／JS 與 fake 回歸。**Unity 不在容器映像內**：Unity 測試與瀏覽器測試只在本機執行，分開記錄。容器通過不代表 Unity 已通過。

## 7 驗收條件對應（Claude Code 執行，待 Codex 獨立 review；程式 `417f104`）

| # | 條件 | 測試與結果 | 證據 |
|---|---|---|---|
| 1 | 402 → fake Buyer → Store → HTTP → 同一投影 → 真正載入的 Unity 與 Dashboard；task-a Ask、task-b Deny | 瀏覽器：Unity Web 建置載入（LimeZu）；task-a Inspector Ask `HUMAN_APPROVAL_REQUIRED` pending、Timeline #1–#3、Unity 回報「#3 waiting」；task-b Deny `INSUFFICIENT_BUDGET`、Unity「#3 denied」。PASS | `artifacts/b1-06/ui-417f10497fdd-20261004T193024Z/` |
| 2 | A／B 各自合法、跨 consumer 被拒 | 瀏覽器：A 讀 task-b 得同一個 404 且看不到 wallet-b；HTTP 與交付點 1 的整合測試。PASS | 同上；`go test ./cmd/demo` |
| 3 | 快速切換、舊回應晚到、序號不同、重複事件、API 失敗 | JS adapter 測試；Unity EditMode／PlayMode（舊世代、舊回呼）；瀏覽器：切換立即清空。PASS | 同上；`artifacts/b1-06/unity-417f10497fdd-20261004T192855Z/` |
| 4 | Ask 到期前後、未提交、取消、CLI 逾時、Deny 保留 | JS adapter 測試；後端行為由交付點 1 的整合測試涵蓋。**瀏覽器未實際觸發到期或取消** | — |
| 5 | 重啟、重新認證、刷新恢復；已保存 task 不重跑；同 consumer 後續 task 同錢包 | 瀏覽器：刷新後須重新驗證，再從 Store 還原；重啟後 task-a 不重跑（log）；task-a-next Ask、wallet-a。PASS | ui 證據 |
| 6 | 惡意字串、fake 宣稱付款、Allow 無付款能力 | 瀏覽器：惡意字串只成為文字，沒有執行 script 或載入 img；頁面沒有付款成功或已付款字樣。JS 測試：Allow 顯示「M1 無付款能力」、未知裁決顯示 unknown | 同上 |
| 7 | token 不進 URL、儲存區、log、證據；host／MCP 不通過 viewer | 瀏覽器：localStorage、sessionStorage 為 0，cookie 與 URL 為空；頁面與 console 沒有 token；host token 被拒；所有證據檔都檢查過沒有 token。HTTP：MCP 401、host 403 | 同上；`ui_test.go` |
| 8 | 靜態路由讀不到 token、DB、repo；GET 輪詢不寫事件 | HTTP 測試；瀏覽器：回放與輪詢前後的事件序號相同 | 同上 |
| 9 | Unity ready 前後、載入失敗、重送、切換世代、舊動畫回呼 | PlayMode 與 EditMode；瀏覽器：沒有建置時顯示「Unity 場景未就緒」並截圖 | unity 與 ui 證據 |
| 10 | Unity 與 Dashboard 顯示相同 task、序號、裁決；缺素材時標示 placeholder | 瀏覽器：Unity 回報的序號與 phase 與 Dashboard 相同；`ready` 回報區分 limezu 與 placeholder，本次為 limezu | 同上 |

容器（Go／JS 與 fake 回歸，Unity 不在映像內）：`024b2cf` 完整容器驗證 overall exit 0、七步驟全過（check-contracts 內含 adapter 9 項）、222／222 突變、17 項隔離檢查，672 秒；證據在 `artifacts/verify-container/runs/024b2cf144f0-20261004T193404Z/`。Unity（`417f104`）：EditMode 6／6、PlayMode 3／3、WebGL 建置成功（`artifacts/b1-06/unity-417f10497fdd-20261004T192855Z/`）。

失敗嘗試（保留）：
- 第一次 EditMode 5／6。不合法、但世代較新的訊息會先清空畫面才被拒；已改為先檢查整則訊息再重置。
- 第一次 Web 建置後的場景框線與標籤重疊，調整後重建。
- 一次 Unity 測試結果 XML 寫進了專案目錄（相對路徑），已改為絕對路徑並移回證據目錄。

截圖：

- `desktop-task-a-ask.png`：1280×900，task-a，序號 3，Ask；
- `narrow-task-b-deny.png`：390×844，task-b，序號 3，Deny；
- `desktop-unity-missing.png`：沒有建置時的畫面。

截圖時 token 欄位皆為空，由測試斷言。

## 8 限制與待確認

- 本輪場景是一間店、固定路點；沒有尋路、多場景或遊戲經濟。Unity 內的文字只用 ASCII（內建字型沒有中文），中文說明在 Dashboard。
- 金額只顯示基本單位整數字串（API 沒有資產精度），不做小數換算。
- 取消、CLI 逾時與 Allow 的畫面，只在 adapter／Unity 單元層與後端整合測試驗證；瀏覽器沒有實際觸發這些狀態。Ask 到期與多請購狀態自 `eda6469` 起由瀏覽器實際驗證（第 9 節）。
- 瀏覽器測試使用本機 Chrome 的 SwiftShader 繪圖；沒有在其他瀏覽器驗證。
- Web 建置與 LimeZu 素材不發布；建置輸出能否公開，待 Aaron 確認授權範圍。
- `84cdf5c` 的四線突變 runner 仍是獨立、未 review 的待辦，與本輪 UI 驗證無關。

## 9 修正 R1～R3（Claude Code 執行，待 Codex 獨立 review；程式 `eda6469`）

依據：vault 筆記 `Claude-Code-B1-06-交付點2修正指示-21db978-20261005`。Codex 對 `21db978` 的限定 review 重現了三個問題：

- **R1**：舊請購的到期蓋過新請購的 Ask。
- **R2**：回放時輪詢仍在跑；回放結束後 Unity 沒有恢復，Deny 的原因可能遺失。
- **R3**：輪詢重繪時，展開的 JSON 被收起。

規則見第 1 節（場景顯示哪一筆請購）與第 4 節（兩種模式、展開的 JSON）。

| 項 | 修改位置 | 測試 | 結果 |
|---|---|---|---|
| R1 | `web/adapter.js` `scene()`、`projection()` | adapter 測試 10：以正式 Store 寫出的兩種歷史（請購 1 到期後請購 2 Ask；兩筆 Ask 後請購 1 到期），兩者都應 waiting／Ask，投影一致，Inspector 列出 2 筆。adapter 測試 11：只有自己的到期／撤回才離店、不明關聯不收尾、孤立裁決 unknown。測試 5、6：Deny 後取消／逾時保留原因。瀏覽器：種子資料庫 task-a 有兩筆請購（一筆到期、一筆 pending）→ Dashboard waiting、Unity 回報 `task-a #6 waiting Ask` | PASS |
| R2 | `web/app.js`（`poll`、`replay`、`restore`、`forget`、`openTask`、事件處理）、`web/unity.js`、`ShopBridge.cs`、`index.html`、`style.css` | 瀏覽器：在一次輪詢完成後立刻回放，12 次取樣都沒有變回「已同步」；回放期間後端新增到期事件（3→4），恢復後 Unity 在比回放更新的世代回報 `task-a #4 expired Ask HUMAN_APPROVAL_REQUIRED`，Dashboard 同步；Deny 回放恢復為 `task-b #3 denied Deny INSUFFICIENT_BUDGET`，事件 JSON 前後相同；回放中切換 viewer 後 4 秒內 Unity 世代不變；連續兩次回放仍正確恢復；頁面隱藏再顯示後 5 秒內 3～7 次輪詢（只有一個迴圈）。PlayMode：`rendered` 回報帶 task、verdict、reason | PASS |
| R3 | `web/render.js` `renderTimeline()`、`web/app.js` | 瀏覽器（斷言實際可見）：展開 #2 後經兩次已完成的輪詢仍可見；新事件 #4 加入後仍可見；手動收合後不會被自動展開；開啟另一個 task 後全部清除 | PASS |

**反向驗證**（每次放回一個舊行為，確認測試以斷言失敗，不是編譯失敗；證據在 `artifacts/b1-06/reversal-eda646904e66/`，包含 diff 與輸出）：

- R1：用 `21db978` 的 `adapter.js` 跑新測試，測試 10、11 失敗（expected waiting，actual expired）。
- R3：展開狀態不保留，3 項 FAIL。
- R2 恢復：不開新世代、`shownSeq` 不重設，2 項 FAIL（Ask 與 Deny 的恢復）。
- R2 輪詢：
  - 只拿掉輪詢裡的 mode 判斷時，仍 55 PASS。原因是 `replay()` 的 `stopPolling()` 與新世代是另一層保護。
  - 同時拿掉 `stopPolling()`（即 `21db978` 的 timer 繼續跑）後，「回放中被改回已同步」FAIL。

**命令與結果**（全部在 `eda6469`）：

| 命令 | exit | 時間 | 證據 |
|---|---|---|---|
| `node --test web/test/adapter.test.mjs`（11／11） | 0 | 1 秒 | `artifacts/b1-06/checks-eda646904e66-20261005T032955Z/` |
| `go test -race -count=1 -v ./cmd/demo ./internal/store` | 0 | 81 秒 | 同上 |
| `sh scripts/unity/unity.sh test OUT`（EditMode 6／6、PlayMode 3／3） | 0 | 12＋10＋22 秒 | `artifacts/b1-06/unity-eda646904e66-20261005T032644Z/` |
| `sh scripts/unity/unity.sh webgl OUT` | 0 | 20 秒 | 同上；`webgl-build.txt` 記錄 commit、editor 與四個建置檔的 sha256，瀏覽器測試載入的就是這份建置 |
| `node scripts/verify/browser-ui.mjs`（55 PASS） | 0 | 90 秒 | `artifacts/b1-06/ui-eda646904e66-20261005T032813Z/` |
| `sh scripts/verify/container-verify.sh eda6469…`（Go／JS＋fake 回歸，Unity 不在映像內） | 第一次 1；重跑 0 | 1070 秒；925 秒 | 第一次 `artifacts/verify-container/runs/eda646904e66-20261005T033127Z/`（`artifacts/b1-06/container-eda6469.log`）；重跑 `artifacts/verify-container/runs/eda646904e66-20261005T035016Z/`（`artifacts/b1-06/container-eda6469-rerun.log`） |

Web 建置的 C# 與先前一次建置相同，所以 wasm 沒有重新產生，只有 `WebGL.data` 更新。

容器（Claude Code 執行，待 Codex 獨立 review）：

- **第一次失敗（保留）**：overall exit 1。七步中只有 `verify-b1-01` exit 1，其餘六步 exit 0，突變 222／222。
  - 失敗都是逾時：`TestApprovedVersionInit`（fake CLI 版本檢查與 init）、`TestIntegrationEndsRestartAndReplay`（HTTP 用戶端逾時）、`TestRunnerOutcomes/not-logged-in`。
  - 該步開始於 03:39:38Z，正好與 Claude Code 在主機上同時跑的四次反向驗證瀏覽器測試（Chrome＋SwiftShader＋Go 編譯，約 03:35–03:46Z）重疊。
  - 同一份 `make check` 在同一輪較早的 `check` 步驟 exit 0。
  - 判斷：第一次容器驗證有三項逾時，與瀏覽器反向驗證同時執行；疑似主機負載影響，原因未獨立證實。停止其他工作後重跑通過，原失敗紀錄保留。
- **重跑一次**（主機上不跑其他東西）：overall exit 0，七步全過，925 秒。
  - 各步：check-contracts 19、check 120、race-cover 260、verify-b1-01 52、mutation 453 秒。
  - check-contracts 內含 adapter 11／11；突變 222／222 KILLED；17 項隔離檢查都 ok。
  - 隔離設定與 runner 都沒改。

**失敗嘗試（保留）**：

- adapter 舊測試 5、6 在 R1 改寫後失敗。它們的收尾事件沒帶 `purchase_order_id`，新規則故意不採用；已補上 po，斷言不變。
- `TestSeedDisplayDB` 失敗兩次：
  - 第一次：憑證期限固定在測試時鐘起點後一天，請購 1 被 Deny `AUTHORIZATION_EXPIRED`。已改為跟著時鐘設定。
  - 第二次：fixture 的 EIP-3009 授權窗在過去。已改為每筆種子請購使用自己的 600 秒窗，只在種子 helper 內。
- 第一次瀏覽器測試在種子步驟失敗（exit 1，49 PASS；`artifacts/b1-06/ui-21db9782e125-20261005T031945Z/`，未提交的程式）。
- 回放世代的檢查原本可能空過（畫面原本就是同一狀態）。已改為點擊前先記錄世代，等待嚴格更新的世代。
- R2 輪詢的第一次反向驗證沒有失敗（見上）。已補做完整的舊行為反向。

**未測**：

- 取消、CLI 逾時與 Allow 仍沒有在瀏覽器觸發。
- 只用 Chrome＋SwiftShader。
- Unity 不在容器內。
- reviewer 的暫存探測沒有沿用，依筆記的重現條件重建。
- `gofmt -l` 列出 `internal/contracts` 三個檔案的既有格式差異，最後修改是 `4d52f44`。不是本輪的修改，契約也不改，所以未處理。

**待 Aaron**：

- LimeZu 素材與 Web 建置的公開散布仍未決。
- `pixel-office/limezu` 記為例外（第 2 節）。

## 10 第二輪修正：Unity 呈現文字、zombie 判斷（Claude Code 執行，待 Codex 獨立 review；程式 `6aee7f0`）

依據：Codex 對 `9d3c4e4` 的 review（Aaron 於對話中轉交）。

**Codex 獨立執行**：

- adapter 11／11；
- JS 狀態探測三輪，確認 R1、R3、回放恢復主流程；
- Store race 通過。

**Claude 執行、Codex 核對紀錄**：Unity、55 項瀏覽器檢查、222 項突變與完整容器結果。

`cmd/demo` race 有 zombie 判斷失敗，不能記為全套通過。下面兩處是這次 review 要求修正的。

**1. Unity 呈現文字殘留**（`web/app.js`）

問題：清除身分或切換 task／consumer 時，商店已清空，但 `#unity-status` 仍寫著上一筆的「Unity 已呈現 task-a #4 expired（HUMAN_APPROVAL_REQUIRED）」與對應的 `data-*`。

修正：

- 新增 `shopReset()`，在 `forget()`、`openTask()`、回放、恢復以及 task 找不到時呼叫，清除文字與屬性，改顯示當下狀態（第 4 節）。
- 原本的 `shopSync()` 併入。

瀏覽器測試新增 9 項（共 64 項）：

- **task 切換**：
  - 點擊當下，狀態是 `syncing`，沒有 task-a 的任何內容；
  - task-b 找不到後，顯示「等待選擇 task」；
  - 注入 task-a 舊世代的 `rendered` 回報（與 jslib 相同的入口 `window.apaUnityReport`），內容仍不恢復。
- **consumer 切換**：
  - 切換前確認 Unity 顯示 task-a #4；
  - 切換後沒有 task、序號、裁決或原因，顯示「等待選擇 task」；
  - 舊世代回報不恢復。
- **回放中切換**：
  - 切換前確認 Unity 已呈現 task-b 的回放步驟；
  - 4 秒觀察期後，沒有 task-b、Deny 或 `INSUFFICIENT_BUDGET`；
  - 注入回放世代的晚到回報，仍不恢復。

這一項取代原本只比對「世代沒變」的檢查。

反向驗證：用 `eda6469` 的 `app.js` 跑新測試，7 項 FAIL，都是斷言失敗，例如 `[rendered|Unity 已呈現 task-a #4 expired（HUMAN_APPROVAL_REQUIRED）]`。證據在 `artifacts/b1-06/reversal-6aee7f0f541b/`。

**2. 測試把 zombie 判為存活**（`cmd/demo/integration_test.go`）

修正：

- `alive()` 改用 Buyer 測試（`internal/buyer/buyer_test.go`）的判斷：`kill(pid, 0)` 成功，且 `/proc/<pid>/stat` 的狀態不是 `Z`。
- 正式程序清理邏輯與「fake grandchild survived」斷言都沒改。
- 新增 `TestAliveTreatsZombieAsGone`：建立一個被殺掉、尚未回收的子程序，確認 `alive` 回 false；沒有 `/proc` 的平台略過（macOS 為 SKIP）。

Linux 驗證：驗證映像，斷網、唯讀原始碼、uid 10001、4 CPU、只跑這兩個測試；證據在 `artifacts/b1-06/zombie-20261005T042225Z/`。

- 修正版：`-race -count=3` 全過。
- 放回舊 `alive`：`TestAliveTreatsZombieAsGone` 三次都失敗（`a zombie reads as alive`）。
- 同一環境下，`TestIntegrationEndsRestartAndReplay` 在舊 helper 下三次都通過，**沒有重現** Codex 看到的那次失敗。孤兒程序何時被回收取決於環境；機制由新測試證明。
- 第一次反向嘗試的替換沒有生效，實際跑的是新程式，已在 log 標明，重做後才是上面的結果。

**命令與結果**（`6aee7f0`）：

| 命令 | exit | 時間 | 證據 |
|---|---|---|---|
| `node --test web/test/adapter.test.mjs`（11／11） | 0 | 1 秒內 | `artifacts/b1-06/checks-6aee7f0f541b-20261005T043415Z/` |
| `go test -race -count=1 -v ./cmd/demo ./internal/store`（macOS；zombie 測試 SKIP） | 0 | 61 秒 | 同上 |
| `node scripts/verify/browser-ui.mjs`（64 PASS） | 0 | 83 秒 | `artifacts/b1-06/ui-6aee7f0f541b-20261005T043115Z/` |
| `sh scripts/verify/container-verify.sh 6aee7f0…`（Go／JS＋fake 回歸，Unity 不在映像內；主機無其他負載） | 0 | 605 秒 | `artifacts/verify-container/runs/6aee7f0f541b-20261005T043523Z/`（`artifacts/b1-06/container-6aee7f0.log`）；七步全過（check-contracts 18、check 86、race-cover 159、verify-b1-01 41、mutation 285 秒），adapter 11／11，222／222 突變，17 項隔離 ok |

Unity C# 與建置都沒有變更（`eda6469..6aee7f0` 沒有改到 `unity/`）。瀏覽器載入的建置檔 sha256 與 `eda6469` 那次建置相同（`webgl-loaded.sha256`），所以沒有重跑 Unity 測試或重建。

**未測**：

- 取消、CLI 逾時與 Allow 仍未在瀏覽器觸發；
- 只用 Chrome；
- 晚到回報用注入模擬，不是由 Unity 真的延遲產生。

## 11 驗收紀錄：交付點 2 於 `6aee7f0` 通過限定範圍技術 review（2026-10-05）

依據：Codex 對 `6aee7f0`（文件 `eb97e61`）的限定範圍 review，由 Aaron 於對話中轉交。

**結果**：

- R1、R2、R3（第 9 節）關閉；
- 第二輪兩項修正關閉（第 10 節）：Unity 呈現文字殘留、測試把 zombie 判為存活；
- 未發現新的阻擋問題；
- B1-06 交付點 2 的 fake UI 資料鏈通過技術 review。

**證據歸屬**：

| 類別 | 內容 |
|---|---|
| Codex 獨立執行（斷網容器） | adapter 11／11；JS 狀態探測三輪：切換身分、task、回放時清除舊資料，晚到的舊世代回報不會恢復；`cmd/demo`、`internal/store` 全套 race 測試；zombie 單元測試與關閉／重啟整合測試各三輪；全部通過 |
| Claude Code 執行、Codex 核對紀錄 | 64 項真實瀏覽器檢查（`ui-6aee7f0f541b-20261005T043115Z`）、222 項突變、完整容器七步與 17 項隔離檢查（`6aee7f0f541b-20261005T043523Z`）；WebGL 四個建置檔雜湊與上一輪一致 |
| Claude Code 執行、本次 review 未個別提及 | Unity EditMode 6／6、PlayMode 3／3、WebGL 建置（`eda6469`；Unity 之後未變更）；反向驗證（`reversal-eda646904e66`、`reversal-6aee7f0f541b`、`zombie-20261005T042225Z`） |

- Codex 判斷：原本的 zombie 整合失敗不必每次重現；新測試直接建立 zombie、驗證判斷機制，足以支持這次修正。

**保留**：

- 失敗嘗試：見第 9、10 節，包含 `eda6469` 的第一次容器驗證有三項逾時，與瀏覽器反向驗證同時執行；疑似主機負載影響，原因未獨立證實。停止其他工作後重跑通過，原失敗紀錄保留；以及第一次 zombie 反向替換未生效。
- 未測：
  - 取消、CLI 逾時與 Allow 未在瀏覽器觸發；
  - 只用 Chrome；
  - 晚到回報以注入模擬；
  - Unity 不在容器內。
- 待決：LimeZu 素材與 Web 建置的公開散布未核准。

**範圍**：這次通過不代表以下任何一項完成：

- 真實模型請購；
- 付款；
- 整體 M1。

B1-06 的完整完成仍需至少一次核准的真實 CLI trace。B1-04／B1-06 維持 InProgress；不合併、不標 Done、不啟動真實 CLI。
