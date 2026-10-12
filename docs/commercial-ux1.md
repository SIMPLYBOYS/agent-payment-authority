# UX1：商業展示主畫面與操作重整

> 2026-10-12，Claude Code。**狀態（10/12）：UX1 已實作；Codex 限定 review 提出 R1（操作卡更新丟失鍵盤焦點）、R2（手動清單情境衝突），UX1 尚未整體關閉；R1／R2 補正完成（第 7 節）；Codex 限定複核：R2 關閉，R1 留一個停止鍵焦點的局部缺口，已補正，待 Codex 複核（第 7.4 節）。Aaron 尚未判定畫面與操作是否達到本片目標。**依 vault〈Claude-Code-商業展示UX1主畫面與操作重整派工-20261012〉。分支 `commercial-ux1`，從 `32e5183` 建立。
> UX1 是暫稱，不占付款或 Admin 的任務編號。只用 fake CLI 與合成資料驗證；不執行真實 CLI。不改 Policy、Store schema、事件契約、付款、Gate、認證、版本釘選、plugin 例外、登入與配對權限。

## 1 版面與操作安排（實作前）

改前畫面（`artifacts/ux1/before-32e5183/`，1440×900、1280×900、390×844）實際看到的問題：

- 商店畫布只有約 415×310，第一屏大半是開始／停止按鈕與長文字；手機寬度第一屏完全看不到商店。
- 畫布上的客人標籤是細小的藍／橙字，壓在貨架上看不清；B 在店外時標籤擠在畫布邊界與購物車旁。畫布上下還有標題與兩行狀態，和網頁重複。
- 開始鍵的文字把情境、consumer、模式全塞在一起；停止鍵混在同一列；「取消 task」與「停止」的差別要讀說明才知道。
- Timeline 與 Inspector 全欄位直接攤在主畫面下方，占了一整頁。
- 沒有每場工具呼叫數（M-C 時抓停止時機只能猜）。

改後的分工（主持人頁 `/host`；`/` viewer 頁與歷史回放的規則不變）：

| 區域 | 初始顯示 | 展開後 |
|---|---|---|
| 頂部狀態 | 模式（fake／live／回放／未確認）、能力短列（付款未實作等）、就緒與連線警示 | live／歷史回放的完整說明在商店上方；live 的開發展示例外與訂閱用量風險全文在 Agent 操作區「展開完整說明」，開始前就看得到 |
| 商店場景（左，主視覺） | 放大的 Unity 畫布；兩位角色有色塊標記、名稱牌與簡短狀態；選取的角色有選取圈 | 不在畫布上放 hash、長錯誤碼或 trace |
| Agent 操作（右上） | 每位一張卡：A／B、執行狀態（執行中／停止中／已結束／未開始）、已回覆工具呼叫 N 次（未知時「未確認」）、業務狀態、開始、停止 Agent、取消請購／task；卡下「一起開始」 | 場次與 task 明細、最近場次在「進階」 |
| 選取摘要（右下） | 對象、商品、金額與單位、裁決、下一個可用操作 | 「查看證據」 |
| 查看證據 | 按鈕，預設收合 | 右側抽屜：查證面板、工具紀錄、Timeline、Inspector；Esc 關閉、焦點回到按鈕；不清場、不停輪詢 |
| 進階（頁尾，收合） | — | token 檔與就緒表、最近場次、手動 viewer、切換 consumer／task 與回放 |

操作語意：

- **選取**：點操作卡或點 Unity 角色，都選到同一位客人：面板改看該客人的場景 task，畫布顯示選取圈。選取只是 UI 意圖，不授權、不觸發業務操作。Unity 只回報「點到誰」（場景世代、欄位世代、consumer），頁面核對三項都是目前的才採用；舊場景、舊欄位或未知客人的回報丟棄。
- **停止 Agent**：只對執行中的場次可按；停止中仍占名額，顯示「停止中」；已結束時停用並寫明原因。
- **取消請購／task**：沿用現有後端取消（整個 task：未決 Ask 撤回並記錄 `TASK_CANCELLED`）；按鈕寫明影響範圍並要求確認；場次執行中不提供（請用停止）。
- **工具呼叫數**：`runView.tool_calls` 是 Host 已回覆的呼叫數（含被拒的呼叫），只在執行中／停止中提供；其他情況寫「場次結束後不提供」，讀取失敗寫「未確認」，不當成 0。
- Agent 已結束但 Ask 未到期：「執行已結束／請購待核准」；沒有核准功能，不放核准或付款按鈕。

Unity 場景：

- 鏡頭收緊，商店占滿畫布；門外加一條人行道，讓店外的客人站在可見的地面上；B 的店外等待列往上移，標籤不再貼著畫布下緣。
- 名稱與狀態放在不透明底牌上，A 藍、B 橙的色塊標記在腳下；選取時腳下多一圈黃色選取圈。
- 畫布上的標題保留（`M1 / no payment`、LimeZu 署名），改小放在角落；雙客人模式的狀態列不再畫在畫布上（網頁操作卡已有），文字仍保留給測試。
- 走道、站點與道具位置不變（不穿越道具、不瞬移）；動畫仍只跟隨已提交的狀態。
- 新增的橋接只有兩個最小訊息：頁面 → Unity `Select`（`{scene, consumer}`，只控制選取圈），Unity → 頁面 `select` 回報（點到的 consumer 與其欄位世代）。投影與完成回報契約不變。

## 2 實作（Claude Code 執行，待 Codex 核對）

分支 `commercial-ux1`，從 `32e5183` 開始。產品：`d27abde`（Unity）、`915cce7`（主持人頁）、`fc89b52`（回歸發現的修正：不自行選人、fake 開始鍵文字、能力短列留在頁首、`adapter.runCard`）。測試與計畫：`e9763ca`（UX1 瀏覽器流程與共用 harness、既有流程的選擇器）、`1e1ec30`、`03e46fd`、`4438dda`、`58a7c5d`（檢查計畫與 ux1-ui 的修正）。文件：本節所在的提交。

**網頁**（`web/host.html`、`host.js`、`style.css`、`scene.js`、`unity.js`、`adapter.js`、`app.js`）：
- 主持人頁：左側放大的商店畫布；右側是每位 Agent 的操作卡、「一起開始」、選取摘要（標題列有「查看證據」）；查證放在抽屜；憑證、就緒、最近場次、手動 viewer、切換與回放收在頁尾「進階」（歷史模式與手動 viewer 時自動展開一次）。既有元素 ID 不變，`/` viewer 頁版面不變。
- 操作卡（`adapter.runCard` 判斷執行狀態、停止可用性與呼叫數）：執行中／停止中（仍占名額）／已結束／未開始；已回覆工具呼叫 N 次（Host 的 `tool_calls`，含被拒的呼叫；只在執行中或停止中提供），沒有值時「未確認」，場次清單讀取失敗時「未確認（場次清單讀取失敗）」，場次結束後「場次結束後不提供」，從不寫 0；業務狀態取自場景欄位，Agent 已結束而 Ask 未到期時「執行已結束／請購待核准（M1 無核准功能；到期前不會付款）」；按鈕：選取、開始、停止 Agent（只有執行中可按；停止中與已結束停用並寫明原因）、取消請購／task（先確認，說明會取消整個 task 與 `TASK_CANCELLED`；場次執行中不提供）。
- 選取：操作卡、卡上的「選取」與點 Unity 角色是同一個動作——面板改看該客人的場景 task（照舊用該 consumer 的 viewer token），畫布顯示選取圈。不會自行選人：重新載入或換憑證後不切換任何客人（既有 3.2 規則；開始一場時照舊開啟它）。
- 點選回報的核對（`scene.picked`）：只採用目前場景世代、該客人目前欄位世代、且該欄位有 task 的回報；舊場景、舊欄位、未知客人、回放中都丟棄（`#unity-canvas` 的 `data-picks-dropped` 計數）。
- 抽屜：「查看證據」開啟右側抽屜（查證面板、工具紀錄、Timeline、Inspector）；Esc 或「關閉」關閉，焦點回到原按鈕；不重啟場景、不清空選取、不停輪詢。
- 頂部：模式徽章（fake／live／歷史／未確認）、能力短列（付款未實作等）、警示；live 與歷史回放的完整說明照舊在商店上方，live 的訂閱用量與 plugin 例外以短標示顯示在操作區，全文在「展開完整說明」，開始前就看得到。

**Unity**（`ShopView.cs`、`ShopBridge.cs`、`ShopBuilder.cs`）：
- 鏡頭收緊（正交大小 5.5 → 4.8，中心 (5.1, 4.1)），商店占滿畫布；門外加兩格人行道；B 的店外等待列 y 0.2 → 0.6，標籤不再貼著畫布下緣。走道其他站點與道具位置不變。
- 名稱與狀態放在該客人顏色的不透明底牌上（A 藍、B 橙）；腳下有同色標記；選取時加黃色選取圈。標題（`M1 / no payment`、LimeZu 署名）縮小放在左上角並加底；雙客人時畫布不再畫狀態列（文字保留給測試），過場標籤縮為 `[anim] browsing`／`[anim] to counter`（完整說明在 `LabelOf` 與網頁）。
- 橋接新增兩個最小訊息，不改投影與完成回報：頁面 → `Select {scene, consumer}`（只有目前場景、這個建置呈現的客人才移動選取圈；其他回報 `selected …|Stale/Invalid`）；點擊 → `select` 回報（場景、該客人的欄位世代、consumer）。v1 沒有選取。`bridge` 回報加 `;select=1`，舊建置不送 Select。

## 3 驗收對照（派工第 5 節）

| # | 情境 | 驗證層 | 結果 |
|---|---|---|---|
| 1 | 憑證後不必挑 consumer／task 就能開始雙場（開始時照舊開啟該場）；重新整理不自動執行，也不自行切換客人 | ux1-ui（token 檔；配對由 present-ui 驗證）、browser-ui 3.2 | PASS |
| 2 | 同時活動時角色、狀態、呼叫數可辨識；點角色與點操作列選到同一對象 | ux1-ui（實際滑鼠點畫布）、Unity SelectionTests、Node | PASS |
| 3 | 停 A 不影響 B；A 結束後停止不誤導；取消的提示與結果一致 | ux1-ui、Node `runCard` | PASS |
| 4 | 開關證據不清場、不停輪詢、不換人；查較早 task 有標示 | ux1-ui（抽屜）、dual-ui（較早 task） | PASS |
| 5 | 模式查詢失敗、慢／失敗讀取、晚到回覆、舊角色選取 | ux1-ui（`/api/mode` 失敗、面板讀取失敗、舊場景／舊欄位／未知客人的點擊）、dual-ui（R1 晚到）、Node | PASS |
| 6 | Ask 到期、task 取消、執行停止可分辨；沒有核准、付款、交付按鈕 | ux1-ui | PASS |
| 7 | 單人 viewer 與歷史回放可用；回放不當即時 | browser-ui、history-ui | PASS |
| 8 | 1440×900、1280×900、390×844；抽屜鍵盤關閉與焦點返回 | ux1-ui（第一屏、無橫向捲動、抽屜） | PASS |

## 4 驗證（Claude Code 執行，待 Codex 核對）

證據都在 `artifacts/ux1/`（git 忽略，只在本機）；計畫 `scripts/verify/ux1-check-plan.json`，以 `go run ./cmd/m1-check --plan scripts/verify/ux1-check-plan.json --out artifacts/ux1` 執行。每次的 `summary.json` 記有基準、每步結束碼與秒數。

| 執行 | 基準 | 步驟 | 結果 |
|---|---|---|---|
| `m1-check-20261011T191223Z` | `1e1ec30` | node 69／69（5.7 s）；reverse-selftest；Unity 場景重建＋EditMode 14／14＋PlayMode 25／25（103.6 s）；WebGL 建置（20.0 s，原始碼與先前開發建置相同，增量） | 前四步 PASS；第 5 步 webgl-hashes FAIL（exit 1：計畫在建置目錄內寫相對的證據路徑，我的錯），之後未執行。紀錄保留 |
| `m1-check-20261011T192541Z` | `03e46fd`（只改計畫） | 從 webgl-hashes 起 | hashes PASS；ux1-ui FAIL（A 的 Ask 到期後，Unity 在等待時間內沒有回報「已呈現」；見限制 1）；之後未執行 |
| `m1-check-20261011T192834Z` | `4438dda`（只改 ux1-ui） | 同上 | hashes PASS；ux1-ui FAIL（同一個等待，以及 1 像素截圖與視窗尺寸衝突，1440 的第一屏量到手機寬度）；之後未執行 |
| `m1-check-20261011T193325Z` | `58a7c5d`（只改 ux1-ui） | 從 webgl-hashes 起 | **全部 PASS**：hashes；ux1-ui 73 項（99.9 s）；dual-ui（47.0 s）；browser-ui（468.1 s）；history-ui（50.2 s）；live-ui（83.8 s）；present-ui（106.3 s）；反向 5／5（244.6 s） |

`1e1ec30..58a7c5d` 只改了 `scripts/verify/ux1-check-plan.json` 與 `ux1-ui.mjs`，產品程式與 Unity 沒有變，所以 node、Unity 測試與 WebGL 建置沿用 `191223Z` 的結果，沒有重跑。

**沒有跑的**：Go race（本輪沒有改 Go）；完整容器、全部突變、`make m1-fixture`、M1 重新產包；任何真實 CLI（`live-ui`、`present-ui` 用替身 CLI 與測試建立的 HOME）。

**WebGL**：由 `1e1ec30` 的 Unity 原始碼建置（Unity 6000.5.3f1；產品原始碼與 `fc89b52` 相同），sha256 在 `m1-check-20261011T193325Z/webgl.sha256`：

```text
c9c9e47618ffc1f8e90c99f33a1c75e3cb73a7ef1e5200860ad4537c32760468  WebGL.data
3c7d6ab5547a85de82e08400bfd63520ba9c2f50c098eff05074a6a010e65666  WebGL.framework.js
476908225cb4ee459c16a28dde2f222f1bde8e688aea9f197eef839f92f2b369  WebGL.loader.js
9c70798fece7dc10be81453010ce83df91f9ecd14861ab48e5d8b2a0f7347113  WebGL.wasm
```

**反向**（`m-b-reverse.py "^UX1:"`，一次性副本，先確認未突變通過）：

| 項目 | 突變 | 命中的斷言 |
|---|---|---|
| 舊場景的點擊不選人 | `scene.picked` 不比場景世代 | scene 測試「a click names a customer only for the current scene and its slot」`an older scene's click` |
| 其他欄位的點擊不選人 | 不比欄位世代 | 同一測試 `another slot's click` |
| Unity 不採用舊場景的 Select | 移除 `Stale` 判斷 | `SelectionTests.OnlyTheCurrentScenesSelectShowsTheRing` `an older scene's Select` |
| 只有執行中的場次可停止 | `stoppable` 改為非 idle 即可 | 「a card stops only a running run…」`a stopping run is not stopped again` |
| 沒有次數就不是 0 | 缺值寫成 0 次 | 同一測試 `no count from the host is unconfirmed, not 0` |

晚到回覆的既有防護（M-B R1：舊欄位的回覆不送投影）沿用已驗收的反向項目，本輪以 dual-ui 回歸，沒有另造相同突變。

**改前／改後截圖**（同尺寸；每張都先確認 token 與配對碼欄位是空的）：
- 改前（`32e5183`）：`artifacts/ux1/before-32e5183/`：`1440x900`、`1280x900`、`390x844` 各有 `ready`、`a-asks-b-entered`、`both-ask`，加 `1440x900-both-ask-full`。
- 改後（`58a7c5d`）：`artifacts/ux1/m1-check-20261011T193325Z/ux1-ui/`：三個尺寸的 `ready`、`two-at-work`；`1440x900` 的 `a-chosen-in-the-shop`、`evidence-drawer`、`a-stopped-b-running`、`b-ended-ask-pending`、`b-cancelled`、`a-expired`；`390x844` 的 `evidence-drawer`、`after-the-runs`。

實際看過的畫布問題與改善：改前畫布約 415×310，客人標籤是細小的藍／橙字壓在貨架上，B 在店外時標籤貼著購物車與畫布邊；改後畫布在 1440 寬約 965×643，名稱牌有底、客人腳下有色塊，店外有人行道，選取的客人有黃圈。過程中發現並修正：名稱牌第一版太大，B 走過時蓋住 A（字級改小、過場標籤縮短）；標題白字落在白牆上看不到（加底）。

## 5 限制

1. **無頭瀏覽器的畫面更新**：headless Chrome 在頁面沒有其他變化時，Unity 的走路會停住，直到有完整截圖畫出整頁（A 的 Ask 到期後走出店外：`ux1-ui-dev-1`～`5`、`192541Z`、`192834Z`；頁面自己的 `requestAnimationFrame` 迴圈與 1 像素截圖都不夠）。ux1-ui 在等待 Unity 回報時每 2 秒畫一次整頁。可見的瀏覽器沒有驗證過，手動驗收第 8 步請留意。
2. 畫布上的名稱牌在 390 寬時約 8 px，手機上主要靠操作卡閱讀。
3. 點選角色需要滑鼠先移到該處（Unity 以移動事件更新游標位置）；測試以實際的移動加點擊驗證，觸控沒有驗證。
4. 配對入口由 present-ui 驗證（需要終端機）；ux1-ui 以 token 檔取得同樣的憑證。
5. 「查看較早紀錄」沒有放在操作卡上，沿用「進階」裡的 task 清單與最近場次；查看較早 task 時的差異提示與「回到場景這筆」照舊。
6. 舊的 v2 Unity 建置（沒有 `select=1`）不顯示選取圈、也不回報點擊；操作卡照常可用。
7. 切換選取時，選取圈會先短暫消失（換 viewer 時面板先清空），再出現在新的客人腳下。
8. 這是 fake 與合成資料的驗證，不代表人工體驗已通過；真實 CLI 沒有執行。

## 6 Aaron 手動驗收（10/12 原版；已由第 7.2 節取代，保留為紀錄）

1. `make present-fake`，在開啟的頁面輸入終端機的配對碼。
2. 確認第一屏：左邊是商店，右邊兩張操作卡（未開始）與「一起開始」；頂部寫 fake、不付款。
3. 按「一起開始」：兩張卡變「執行中」，「已回覆工具呼叫」數字增加；畫布上兩位客人各有名稱牌，A 腳下有黃色選取圈。
4. 點畫布上的 B（或 B 卡的「選取」）：B 的卡變黃框，摘要與「正在查看」換成 B 的 task。
5. 按「查看證據」：右側抽屜打開，商店仍在動；按 Esc 關閉，選取不變。
6. 按 A 卡的「停止 Agent」：A 變「已結束」，停止鍵停用；B 不受影響。
7. B 結束後卡上寫「執行已結束／請購待核准」；按「取消請購／task」先出現確認，按「返回」不取消，再按「確認取消 task」才取消。
8. 等一筆 Ask 30 秒到期：卡上寫到期；畫面上沒有核准或付款按鈕。**請特別留意這一步畫布上的客人是否走出店外**（見限制）。
9. 把視窗縮到手機寬度：商店在最上方，沒有橫向捲動。

## 7 R1、R2 補正（10/12，Claude Code 執行，待 Codex 限定複核）

依 vault〈Claude-Code-UX1焦點與手動清單R1-R2補正派工-382a4b5-20261012〉。開工基準 `382a4b5`，工作區只有未追蹤的 `demo`。不改 Unity、Host、Policy、Store schema、Gate、認證、CLI 釘選或 plugins；投影、完成回報與選取隔離規則不變。

**Codex 前輪的核對與歸屬**（Codex 的結論，照它的說法記錄）：
- Codex 獨立執行 Node 69／69，核對四個 WebGL 交付檔的雜湊相符，並核對 `1e1ec30..58a7c5d` 只有檢查計畫與 UX1 瀏覽器腳本的差異。
- Codex 的定向 fake 瀏覽器探測確認了 R1 的焦點遺失（B 的取消確認取得焦點後，A 的選取更新使操作區重建，焦點落回 `BODY`）。
- 同一探測中，兩位的 Ask 到期後在沒有截圖的觀察期間內都回報完成，這一次沒有重現動畫卡住；這不是一般可見瀏覽器的驗證，也不證明每次都正常或 2.5 秒上限。
- 探測第一次在沙箱內沒有完成有效的瀏覽器檢查，暫存探測腳本的收尾也有 unresolved await；這不算產品失敗。修正收尾後在沙箱外完成有效檢查，全程只有 fake CLI 與測試 HOME。
- Codex 看過桌面與手機截圖：桌面的場景大小、角色辨識與資訊分層有改善；這不代替 Aaron 的體驗判定。
- 完整瀏覽器流程、Unity 與反向驗證由 Claude Code 執行，Codex 沒有重跑整套。Codex 的探測輸出只在對話的工具紀錄，沒有 repo 證據 log。

### 7.1 R1：操作卡更新不再丟失鍵盤焦點（`1bd1354`）

- `web/host.js` 的 `keepFocus`：操作卡與最近場次清單仍在內容改變時重建，但重建前記下有焦點的控制項（種類、客人或場次、task／run／情境，以及它建立時的憑證世代），重建後聚焦同一個控制項。
  - 找不到時：確認區已關閉（返回或 task 已結束）→ 該 task 的取消入口；仍沒有 → 同一位客人的「選取」；憑證世代不同或什麼都不剩 → 只會顯示資料的控制項（配對碼欄、載入 token 檔或查看證據）。
  - 不會落到另一個 task 的取消鍵；恢復焦點只呼叫 `focus()`，不會點擊或送出。
  - 焦點原本不在操作區（使用者已移到別處）時，什麼都不做。沒有內容改變時不重建，焦點不動。
- 確認區在它的 task 不再可取消、或不再是該客人最新的場次時關閉，不會移到別的 task。
- **測試中發現的頁面問題**：Unity 的 WebGL 輸入聽整頁的按鍵並取消預設動作，所以頁面上所有按鈕與欄位的 Enter、Space、Tab 都無效（鍵盤使用者無法打開或回答取消確認）。`web/app.js` 讓目標是頁面元素（不是商店畫布）的按鍵保留預設動作；Unity 仍收得到按鍵，頁面本身沒有取消任何按鍵的預設動作。沒有改 Unity。
- 新的瀏覽器檢查 `scripts/verify/ux1-focus-ui.mjs`（瀏覽器自己的按鍵事件；A 的呼叫以檔案閘門控制更新時點，不靠固定 sleep）：
  1. B 可取消，以鍵盤開啟確認，焦點在「確認」；A 的卡經正常輪詢更新（呼叫 1 → 2 次），焦點仍在 B 的「確認」。
  2. Tab 到「返回，不取消」，Enter：確認關閉、沒有取消、焦點回到 B 的取消入口；沒有更新時焦點不動。
  3. 使用者以 Tab 離開卡片後，A 再次更新（2 → 3 次），焦點不被搶回。
  4. 確認開著時 B 開始新的一場：舊確認關閉，焦點到 B 的「選取」，沒有取消任何 task。
  5. 最近場次清單裡開啟確認後「清除全部憑證」：沒有取消、焦點不在任何取消或確認鍵；重新載入 token 檔後也沒有打開確認。
  6. 以鍵盤明確確認一次：只取消 `show-b-01`，其他 task 未取消。
  7. 相鄰回歸：Enter 開抽屜、焦點在「關閉」，Esc 關閉、焦點回到「查看證據」。

### 7.2 R2：手動清單拆成三段（`33629dc`）

新增 fake 彩排入口 `make present-rehearsal`：`present.sh` 的 fake 模式改用 `testdata/demo/scenarios-rehearsal.json`（只在 fake 模式可用 `APA_PRESENT_SCENARIOS` 換目錄；live 目錄不變）。目錄內容：
- `wait-a`、`wait-b`：請購後等待，直到停止（fake CLI 3 分鐘後自行結束）。「一起開始」會開始這兩場。
- `z-ask-a`、`z-ask-b`：主商品 Ask 後結束（Ask 保持待核准，可等它到期或取消）。

這是 fake CLI 的等待，不執行模型，**不能當成真實 Agent 工作交錯或停止隔離的證據**。結束：在終端機按 Ctrl-C（場次資料留在 `var/present-XXXXXX`，可刪）。

**Aaron 的 fake 手動清單**（三段各自獨立；每段都從 `make present-rehearsal` 新開一次，輸入終端機上的配對碼）：

**A. 自然到期**
- 前置：新開的彩排頁，兩張卡都是「未開始」。
- 操作：只按 A 卡的「開始（fake，不執行模型）：主商品，Ask 後結束」，之後**什麼都不按**：不取消、不停止、不切頁、不截圖、不點畫面。
- 預期：A 卡先「執行中」，再「已結束」，寫「執行已結束／請購待核准」；約 30 秒後卡上寫 Ask 已到期，畫布上 A 走出店外；畫面上沒有核准或付款按鈕。
- 結束條件：A 走到店外，或等到 60 秒仍沒走出去時記下當時卡上與畫布上的狀態（不要重開來掩蓋）。

**B. 取消請購／task**
- 前置：新開的彩排頁。
- 操作：按 B 卡的「主商品，Ask 後結束」；B 寫「執行已結束／請購待核准」後，**30 秒內**按 B 卡的「取消請購／task」→ 讀確認文字 → 按「返回，不取消」（確認關閉、task 仍待核准）→ 再按一次「取消請購／task」→「確認取消 task」。
- 預期：確認文字說明會取消整個 task；確認後狀態列寫「task show-b-… 已取消」，B 卡寫已撤回（task 取消），B 離店；取消的是 B 的這一筆。
- 結束條件：B 卡寫已撤回。若按下前 Ask 已到期（卡上寫到期），照實記下，不要當成「未到期撤回」的結果。

**C. 停止隔離（fake）**
- 前置：新開的彩排頁。
- 操作：按「一起開始」（會開始 `wait-a`、`wait-b`）；兩張卡都是「執行中」、各「已回覆工具呼叫 1 次」後，按 A 卡的「停止 Agent」。
- 預期：A 卡變「已結束」（停止中時寫仍占名額），停止鍵停用；B 卡仍是「執行中」，B 的請購不受影響。
- 結束條件：A 已結束且 B 仍在執行；之後可按 B 的「停止 Agent」收尾，或讓 fake CLI 3 分鐘後自行結束。

另外請留意：手機寬度下名稱牌偏小；觸控沒有驗證；較早的 task 在「進階」裡查看；一般可見瀏覽器的自然到期體驗（A 段）是本輪最需要你確認的一項。

### 7.3 驗證（`artifacts/ux1/m1-check-20261012T033541Z/`，基準 `70dc9c9`；計畫 `scripts/verify/ux1-r1r2-check-plan.json`）

| 步驟 | 結果 | 結束碼 | 秒數 | 內容 |
|---|---|---|---|---|
| node | PASS | 0 | 5.8 | adapter、hostcreds、scene：69 項 |
| reverse-selftest | PASS | 0 | 0.1 | 27 項錨點與 node／unity／ui 分類器 |
| ux1-focus-ui | PASS | 0 | 22.9 | R1 鍵盤焦點 27 項 |
| rehearsal-entry | PASS | 0 | 17.5 | `make present-rehearsal` 就緒；三段前置可達（兩場等待中、停止 A 後 B 仍執行；兩筆 Ask 結束後待核准）7 項 |
| ux1-ui | PASS | 0 | 98.1 | 相鄰回歸（操作卡與取消確認有改）73 項 |
| reverse | PASS | 0 | 59.4 | `UX1-R1`：拿掉 `keepFocus` 的保護，命中 `FAIL B's confirm keeps the focus while A's card updates` |

開發過程（未列入上表）：第一版焦點檢查在修正前的頁面上失敗，追查後發現按鍵沒有觸發按鈕（Unity 取消了預設動作），補上 `app.js` 的處理後才是完整的 R1 檢查。

**沿用與沒有跑的**：
- WebGL 沿用第 4 節（由 `1e1ec30` 的 Unity 原始碼建置）；本輪沒有改 Unity，沒有重建或跑 Unity 測試。
- 沒有重跑 dual、browser、history、live、present 五個完整流程；沒有 Go、容器、全部突變、`make m1-fixture` 或真實 CLI。
- `app.js` 的按鍵處理也套用在 `/` viewer 頁，沒有另外跑 viewer 頁的流程。

產品基準：`1bd1354`（網頁）、`33629dc`（彩排入口）；測試與計畫：`70dc9c9`；WebGL 基準：`1e1ec30`。

### 7.4 R1 局部缺口：停止鍵的焦點只回到同一場（10/12，Claude Code 執行，待 Codex 複核）

**Codex 對 `c646c26` 的限定複核**（照 Codex 說法）：R2 的三段獨立彩排符合要求，關閉（Aaron 的實際體驗仍待確認）。R1 剩一個局部缺口：`keepFocus` 沒有保存操作卡「停止 Agent」的 `data-run`，恢復時也不比對 run；Codex 在真實瀏覽器 DOM 重現：焦點原在 A 的舊場次停止鍵，卡片換成 A 的新場次後，焦點落到新場次的停止鍵（程式不會自己按下，但使用者接著按 Enter 可能停止不是原本要停的場次）。Codex 另獨立執行 Node 69／69，並以本版建置的 fake 服務確認 `/` viewer 頁的 Enter 登入、Tab 移動焦點、Space 清除憑證都正常；先前沿用舊執行檔的按鍵探測不算本版缺陷。Codex 沒有啟動 Agent、執行真實 CLI 或修改產品，探測輸出只在對話紀錄，沒有 repo log。

**補正**（`a5ed03f`）：`keepFocus` 另記下有焦點的按鈕自己的 `data-run`，恢復時要求同一個 run；原場次的停止鍵不在了（場次結束、停止中或換成新的一場），焦點退回同一位客人的「選取」，不會落到新場次的停止鍵，也不會點擊。

**測試**（`531697f`，`ux1-focus-ui.mjs`；A 的 fake 情境多一道閘門）：
- 同一場更新：焦點在 A 的「停止 Agent」，A 的同一場經輪詢更新（呼叫 2 → 3 次），焦點仍在同一場的停止鍵。
- 換成新的一場：焦點在 A 舊場次的停止鍵，舊場次停止、A 開始新的一場後，焦點在 A 的「選取」，不在新場次的停止鍵；新場次沒有被停止。
- 原有案例照舊（B 的確認、返回、移開焦點、B 換場、憑證變更、明確確認、抽屜）。

**驗證**（`artifacts/ux1/m1-check-20261012T040507Z/`，基準 `531697f`；計畫 `scripts/verify/ux1-r1b-check-plan.json`）：

| 步驟 | 結果 | 結束碼 | 秒數 | 內容 |
|---|---|---|---|---|
| reverse-selftest | PASS | 0 | 0.1 | 28 項錨點與分類器 |
| ux1-focus-ui | PASS | 0 | 25.6 | 32 項 |
| reverse | PASS | 0 | 97.2 | `UX1-R1` 兩項：拿掉 `keepFocus` 命中 `FAIL B's confirm keeps the focus while A's card updates`；拿掉 run 比對命中 `FAIL a new run's stop does not get the old run's focus: A's 選取 has it` |

只跑受影響的焦點檢查；Node、其他瀏覽器流程、Unity、容器、產包與真實 CLI 都沒有重跑（本次只改 `keepFocus` 的 run 比對）。產品基準 `a5ed03f`；WebGL 基準仍是 `1e1ec30`。下一步：Codex 複核這個缺口；之後是 Aaron 依第 7.2 節的三段 fake 體驗確認。
