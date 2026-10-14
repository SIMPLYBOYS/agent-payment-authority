# UX3 三個情境的場景與資料

> 2026-10-15，Claude Code。依 Aaron「我們開始 UX3」與 UX2 的拆分（[commercial-scenes.md](commercial-scenes.md) 第 9、10 節）。分支 `commercial-ux3`（從 main `1148bc6`）。
> **UX3 Done（Aaron 10/15：「UX3 先標 done」）；未 merge、未 push。**
> 實作完成；獨立上下文 review APPROVE（無 must-fix），五項 should-fix 已修（第 11 節）。 只用 fake CLI、合成資料與測試金鑰；不付款、不連鏈。咖啡店與便利商店是示範情境：示意商品，沒有接 POS、庫存或物流。

## 1 Aaron 的核准

- 10/15「我們開始 UX3」。
- 10/15「素材清單是可以」：13 項 LimeZu 素材（第 8 節）。
- 10/15「我們UX3 採做法 B」：每個情境的佈置是資料檔。新增一個情境＝新增佈置檔、目錄 entry、商品與素材行，不改 Unity 程式（第 3 節）。

## 2 一段話

主持人頁先選情境（UX2-D3），再選客人。
- 選了哪個情境，客人卡片就只列那個情境的 entry，Unity 換成那個情境的房間，場景只呈現那個情境的場次。
- 每一場的工具只看得到、也只買得到自己情境的商品。

三個情境走同一套委託、請購、Policy、核准、憑證與事件；各自示範一種結果：
- 資料研究室：Ask → 核准；
- 街角咖啡：範圍內 Allow（UX2-D2）；
- 巷口便利商店：Ask → 核准時憑證已撤銷 → 這筆已關閉。

交付區在 P2 前保持空，標示「after payment」（UX2-D4）。

## 3 新增一個情境要做什麼（做法 B）

1. **佈置檔**：`unity/AgentPaymentAuthority/Assets/APA/Resources/Layouts/<key>.json`。內容：
   - 標題；
   - 地板與牆的素材鍵和色調；
   - 道具 `{key, x, y}`；
   - 走道六個點（door、aisle、shelf、bend、front、counter）；
   - 分區標示，其中交付區那一條要寫 after payment。

   單位是 32 px 一格，房間 12 × 8。WebGL 重建後，Unity 自己讀進來，不改程式。
2. **素材**：在 `limezu-assets.txt` 加行（`key <TAB> 路徑 <TAB> 裁切範圍或 -`），再用 `scripts/unity/import-limezu.sh` 匯入。
3. **商品**：在 `internal/merchant/merchant.go` 的 `products` 加幾筆，`Scene` 填情境鍵。新情境的目錄自動出現在 `/scenes/<key>/catalog`。這是 Go 的 fixture 資料。
4. **場景目錄**：entry 加 `"scene": "<key>"`。
5. **頁面名稱**（選用）：`web/adapter.js` 的 `VENUES` 加一行：名稱、是否示範情境、商品類別、交付方式。沒有這一行時，頁面寫「情境 <key>」，並當作示範情境。

商店目錄的商家名稱與 Unity 能力回報的測試都由資料推導（review S4），新增情境不必改它們。

新的佈置檔會自動受下列測試檢查：

| 測試 | 檢查內容 |
|---|---|
| EditMode `LayoutTests` | key 等於檔名；ASCII；色調不透明；素材鍵都在清單裡；走道相連的兩點同 x 或同 y、都在房間內；交付區標示 after payment；UX3 目錄的每個 scene 都有佈置 |
| PlayMode `CorridorsKeepClearInEveryRoom` | 每個房間裡 A、B 的走道（含 v2 的 B 偏移）不碰道具的實際像素 |
| PlayMode `RoutesKeepClearOfPropsInFourDirectionsWithAsciiLabels` | 每個房間實際走一遍 |

## 4 資料端（第一步，`9e0713e`、`8e765d1`）

- **商店**（`internal/merchant`）：
  - 商品帶 `Scene`。`""` 是資料商店，`/catalog` 的內容與位元組都不變。
  - 示範情境各有自己的目錄 `/scenes/{scene}/catalog` 與資源路徑：
    - `cafe-lunch-set`（0.08，`/scenes/cafe/lunch-set?pickup=1230`）；
    - `mart-coffee-beans`（0.36，`/scenes/mart/coffee-beans?qty=2`）。
  - 三者同一個示意商家的 payTo（registry 只有一個 `merchant_pay_to`）。示範情境不列法幣價格。
- **工具橋接**（`internal/toolbridge`）：
  - `Binding.Scene` 由主持人設定。
  - `list_products` 讀該情境的目錄，只留該情境的可信商品。
  - 別的情境的商品，對這場是 `UNKNOWN_PRODUCT`，紀錄也不記商品名。
  - Store 的報價簿涵蓋所有情境。
- **主持人**（`cmd/demo`）：
  - 場景目錄 entry 的 `scene`：`data`、`cafe` 或 `mart`；省略即 data，M1 entry 的 digest 不變。
  - 未知的情境拒絕載入。
  - `scene` 進 digest、經 `planRun` 帶進 `buyer.Host.Scene` 與 Binding。
  - `/api/host/status` 的每個 entry 帶 `scene`。
  - 場次只在目錄裡那筆 entry 的 digest 與受理時相同時，才顯示 `scene`；entry 改過就不顯示，不猜。
- **目錄與指令**：
  - `testdata/demo/scenarios-ux3.json`：三個情境的 C2（apa.pay.v2）entry；
  - `testdata/cli/cafe-lunch.json`、`testdata/cli/mart-beans.json`：假 CLI 腳本；
  - `make present-ux3`。
- 現行的 apa-buyer 與 live 流程都是資料商店（Binding 的 Scene 為空），行為不變。

## 5 Unity（做法 B）

- **佈置檔**：`Resources/Layouts/{data,cafe,mart}.json`，見第 3 節。
  - **便利商店**：沿用原雜貨店的佈置，加上取貨櫃。水果架下移 0.1 格：舊佈置在 v2 時，B 偏移後的走道會碰到它。舊測試只跑 v1，所以沒抓到。
  - **資料研究室**：櫃台點左移一格，放得下 4 格寬的電腦桌（核准櫃台）。
- **ShopBuilder**：依 `limezu-assets.txt` 把每個鍵的圖與裁切範圍放進 ShopView（`art`、`artKeys`、`artRects`）。同一張主題圖只匯入設定一次。不再寫死道具位置。
- **ShopView**：
  - `UseLayout(key)` 重建房間，所有客人回到門外；
  - 走道點改成依佈置的實例方法 `Point`；
  - 分區標示加深色底板，淺色牆上也看得清楚；
  - 預設房間是 `data`（主場景，UX2-D1）。
- **橋接**：
  - v2 訊息多了 `layout`。一個場景世代只有一個佈置；同一場景換佈置、或建置沒有的佈置，整筆拒絕（Invalid），場景什麼都不重設。例外：Unity 正在 v1（回放）時，任何 v2 訊息（UX3 之前就是如此）先切回 v2，v1 的狀態會重設。
  - 新場景回報 `layout`，例如 `layout:3:0:cafe`。
  - 能力回報加 `;layouts=cafe,data,mart`。
  - 換回 v1 時用預設房間。
- 畫面上的字（標題、分區標示）是英文：內建字型沒有中文。中文在網頁上。

## 6 網頁

- **情境選擇**（`#venue-picker`，在 Agent 操作最上面）：
  - 目錄有兩個以上情境時才出現；示範情境標「示範情境」。
  - 頁首 `#venue-chip` 寫目前的情境。
  - 選了情境，卡片與「一起開始」只列該情境的 entry。
- **場景**（`web/scene.js`）：
  - `setVenue(venue, room)`：換情境就開新場景，不保留任何位，再跟著該情境的場次。
  - 每一則 v2 訊息都帶同一個 `layout`。
  - 建置沒有那個佈置時，送空值（預設房間），場景說明會寫出來。
  - 目錄只有一個情境時不篩選場次，但仍用它的房間與用語（review S3）。
- **核准摘要**：加「情境」「交付方式」兩列；商品類別依情境寫（數位資料、餐點・示意菜單、商品・示意商品）。原本一律寫「數位資料」，對咖啡店與便利商店是錯的。
  - 有多個情境、但這筆的場次不在最近 20 場或目錄已改時，寫「商品・情境未確認」與「情境：未確認」，不退回數位資料（review S2）。
  - 只有資料商店的目錄（M1 等）不加這兩列，與 UX3 之前相同。
- **情境選擇的按鈕**：只在情境清單改變時重建；選擇只改 `aria-pressed`，所以焦點留在按鈕上（review S5）。
- **證據抽屜**：報價區第一列「情境」：名稱、示範情境的說明、交付方式。
- 畫布的 `data-room` 記 Unity 回報的房間，瀏覽器檢查用它確認 Unity 真的換了房間。

## 7 驗證

（第 10 節記錄每輪結果，失敗照實保留。）

- Go：`go vet`、`go build`、`go test ./...`。
- Unity：EditMode 與 PlayMode（`scripts/unity/unity.sh test`）。
- 網頁單元測試：`node --test web/test/*.test.mjs`。
- 反向突變：`scripts/verify/mutation-check.py` 的 UX3 項目，加上兩項重新定錨。
- 瀏覽器：`node scripts/verify/ux3-ui.mjs`。用正式的 `scenarios-ux3.json` 依序走：
  1. 資料研究室入口；
  2. 街角咖啡：B 範圍內 Allow；
  3. 巷口便利商店：A Ask → 撤銷 → 核准時關閉；
  4. 資料研究室：A Ask → 核准（模擬）。

  每一步都檢查三件事：Unity 回報的房間、卡片只列該情境、場景只呈現該情境的客人。另外截桌面 1440×900 與手機 390×844（無橫向捲動）。

## 8 素材與授權

LimeZu Modern Interiors v41.4（limezu.itch.io）：可使用，不可散布。圖檔、匯入副本（`Assets/LimeZu/`，sha256 記在被忽略的 `IMPORTED.txt`）與 WebGL 建置（`Build/WebGL/`）都不進 git，也不公開。進 git 的只有清單與佈置檔。

UX3 新增的 13 項（Aaron 10/15 核准）：

| 情境 | 項目 | 來源 |
|---|---|---|
| 資料研究室 | `bookshelf-wide`、`bookshelf-narrow`、`terminal`、`pc-desk`、`library-desk` | `5_Classroom_and_library_32x32.png` 主題圖裁切。素材包的 Classroom_and_Library singles 資料夾其實是樂器，所以從主題圖裁 |
| 街角咖啡 | `menu-board`、`ice-counter`、`cafe-counter`、`ice-display`、`cafe-table`、`cafe-chair` | Ice Cream Shop singles 14、102、100、37、75、76 |
| 巷口便利商店 | `locker-1`、`locker-2` | Gym singles 114、115 |

- `library-desk` 已匯入，但目前的佈置沒有用到。
- Gym 114／115 是一組灰色櫃門的左端與中段，畫面上讀起來像一個灰色櫃子。若要更像取貨櫃，可以加 116（右端），或換別的素材，這需要另外核准。

## 9 已知限制

- 證據抽屜的「目錄價格」只讀 `/catalog`（資料商店），所以示範情境的商品顯示「目錄未取得」。
- Unity 上方標題仍寫「M1 / no payment」，沿用既有測試對中性文字的要求；頁首寫的是實際模式（例如 C2）。
- Unity 的「ready」素材回報只在啟動時對預設房間送一次；換房間後若那個房間缺素材，畫面標題會寫 LimeZu pending，但頁面上的素材狀態不變。
- 每次換房間都會新建 Sprite（缺素材時還有 placeholder 貼圖），不回收。單次成本很小；若要展示好幾個小時，再按素材鍵快取。
- 切換情境只改變頁面看哪個情境。其他情境的場次照常進行，在場次列表與面板裡都看得到。
- `cmd/demo TestTimingInterleaved` 在 main `1148bc6` 的乾淨 worktree 上也失敗。原因是等 B 的第 2 次呼叫數時，場次已經結束，結束後不再顯示呼叫數。與 UX3 無關，本片未修。

## 10 驗證紀錄

失敗照實保留；修正後只重跑受影響的部分。證據放在 `artifacts/ux3/`（git 忽略，截圖含 LimeZu 圖）。

**第一步（資料端）**
- 9e0713e：`go vet`、`go build` 通過；`go test ./...` 1225 通過、2 失敗、4 跳過（另略過 `TestTimingInterleaved`，見第 9 節）。
  - 失敗的是 `apa-buyer TestApprovedVersionInit` 與 `buyer TestVersion`，都是全套並行時逾時（holder check 2.1 s）；單獨重跑各 1/1 通過。
- 9e0713e 反向突變 9 項：8 KILLED，#314（Host 未綁情境）SURVIVED。原因是它的測試在 cmd/demo，而檢查器只跑被改檔案所在的套件。
  - 8e765d1 在 internal/buyer 補 `TestHostScene` 後，#314 KILLED。

**第二、三步（Unity、網頁）**，在 8e765d1 加上本片未提交的改動上跑：
- 第一輪：
  - Unity EditMode 17/17、PlayMode 27/27；
  - 網頁單元測試 96/96，contract vectors 通過；
  - WebGL 建置成功（14,940,749 bytes）。
- 瀏覽器第一輪：**FAIL (1)**。流程的每一項都通過，失敗的是「沒有頁面錯誤」。這個判定把瀏覽器的網路狀態紀錄也算進去：task 建立前讀到的 404，以及撤銷後核准的預期 409。
  - 改法：網路狀態另列 404 路徑，判定只算例外與 console error。
  - 同時看截圖發現，分區標示在便利商店的白牆上幾乎看不到，所以加了深色底板；頁首另外加上情境標籤。
- 第二輪（重跑 Unity、WebGL、瀏覽器）：
  - Unity EditMode 17/17、PlayMode 27/27；
  - WebGL 建置成功；
  - 瀏覽器 **PASS**，含頁首標籤、三個情境、桌面與手機截圖。

- 5bdf531（提交後的乾淨工作樹）：瀏覽器 PASS。

**review 修正後（aed6d88，乾淨工作樹）**
- 反向突變 10 項（UX3 八項，含 review S1 新增的一項，加上兩項重新定錨）：10 KILLED。
- Go：vet 通過；merchant＋toolbridge 55 通過；cmd/demo 139 通過（略過 `TestTimingInterleaved`）。
- 網頁單元測試 96/96。
- Unity：EditMode 17/17、PlayMode 27/27。
- WebGL 建置成功；瀏覽器 PASS（38 項，含 S5 的按鈕節點檢查）。

**WebGL 建置**（aed6d88，本機，不進 git）sha256：
- `WebGL.data` `aafb66fb…f4d1cd`
- `WebGL.wasm` `859c115d…ca26bb`
- `WebGL.framework.js` `3c7d6ab5…e65666`
- `WebGL.loader.js` `0dc56612…88fa5a`

## 11 獨立上下文 review（Claude Code subagent，10/15）

結論：APPROVE，沒有 must-fix。確認：一場不論經哪條路都買不到別的情境的商品；`/catalog` 的位元組（以 1148bc6 與 5bdf531 實際比對）和 M1 entry 的 digest 都沒變；SceneState 的佈置規則與 scene.js 的換場都正確。

| 項目 | 問題 | 處理（aed6d88） |
|---|---|---|
| S1 | 拿掉主持人接上各情境目錄的那一行，測試仍通過（假 CLI 不管 list_products 結果照樣請購） | 端到端測試檢查每場的呼叫為 `list_products:ok …`；新增突變 #316，KILLED |
| S2 | 場次不在最近 20 場或目錄已改時，核准摘要退回「數位資料」 | 改寫「商品・情境未確認」與「情境：未確認」 |
| S3 | 只有一個非 data 情境的目錄顯示資料研究室與資料用語 | 單一情境也送它的房間、用它的用語 |
| S4 | 文件說新增情境不必改程式，但能力回報測試與商家名稱寫死 | 兩者都由資料推導 |
| S5 | 選情境後按鈕被重建，鍵盤焦點掉了 | 選擇只改 `aria-pressed`；瀏覽器檢查確認按鈕是同一個節點 |

附註：
- 已處理：預設房間一定登記；頁面換手時忘掉房間回報；刪除未用欄位與多餘空行。
- 寫進文件：v1 回放時的 v2 訊息會重設、ready 素材回報只送一次、換房間的 Sprite 不回收。
