# B1-06 交付點 2：Unity＋LimeZu 商店與 Dashboard

狀態（2026-10-05）：Claude Code 實作，待 Codex 獨立 review。依據 vault 筆記 `Claude-Code-B1-06-交付點2-Dashboard與商店實作指示-20261005`（新版：商店固定為 Unity＋LimeZu，獨立於 pixel-office），開工基準 `837f667`，分支 `b1-06-demo-integration`（未合併 main）。

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
| `APPROVAL_EXPIRED`／`APPROVAL_WITHDRAWN` | Ask 已到期（後端已記錄）／已撤回，離店 |
| `TASK_CANCELLED`／`TASK_TIMED_OUT` | 只加註；Deny 之後發生時不改變原 Deny 與原因 |

不會出現付款成功、成功取貨或鏈上完成。

## 2 Unity 環境與 LimeZu 素材

- **Editor**：Unity **6000.5.3f1**（Unity 6.5），已安裝 WebGLSupport 模組；本機唯讀確認，未安裝或更新任何東西。專案釘選在 `unity/AgentPaymentAuthority/ProjectSettings/ProjectVersion.txt`。
- **套件**：`Packages/manifest.json` 只用 editor 內建的套件：`com.unity.2d.sprite` 1.0.0、`com.unity.test-framework` 1.7.0，以及 imageconversion、imgui、jsonserialize 模組。沒有從網路下載套件。
- **LimeZu 來源**：Aaron 10/5 指定的素材包位置 `…/pixel-office/limezu`，使用其中的 **Modern_Interiors_v41.4**。
  - 只複製檔案，不引用 pixel-office 的程式、場景或 Library；本專案也不依賴那個目錄的其他內容。
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
- **歷史展示回放（唯讀）**：用新的頁面世代，依保存的事件逐步重演商店畫面。不重跑 task、不改事件或時間、不寫任何資料（瀏覽器測試會比對前後的事件序號）。
- **新 task**：重新展示不需要刪除資料。重啟服務時，已保存的 task 不會重跑；要新的 run，請在計畫裡改用 `task-a-next`（同 consumer、同錢包），或設定新的 `APA_DB_PATH`。不要把刪除資料庫當成預設的重跑步驟。

## 6 測試

| 層 | 命令 | 涵蓋 |
|---|---|---|
| JS adapter | `node --test web/test/adapter.test.mjs`（也在 `make check-contracts`） | 一致性、舊世代、舊快照、去重、Ask／Deny／Allow、期限未提交、取消／逾時保留 Deny、惡意字串、投影欄位；預期值取自 `cases.json` |
| Unity EditMode | `sh scripts/unity/unity.sh test OUT` | `ProjectionState`：去重與單次動畫、首次載入、舊世代／舊序號、世代清空、單 task、不合法輸入 |
| Unity PlayMode | 同上 | 真場景經由 Bridge：Ask 走到櫃檯且重複不重播、Deny 離場、切換世代清空、舊世代回呼與訊息丟棄、LimeZu／placeholder 標示 |
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
- 到期、取消、逾時與 Allow 的畫面，只在 adapter／Unity 單元層與後端整合測試驗證；瀏覽器沒有實際觸發這些狀態。
- 瀏覽器測試使用本機 Chrome 的 SwiftShader 繪圖；沒有在其他瀏覽器驗證。
- Web 建置與 LimeZu 素材不發布；建置輸出能否公開，待 Aaron 確認授權範圍。
- `84cdf5c` 的四線突變 runner 仍是獨立、未 review 的待辦，與本輪 UI 驗證無關。
