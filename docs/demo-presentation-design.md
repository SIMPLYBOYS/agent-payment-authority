# B1-06 商店展示深化設計（fake 展示模式）

狀態（2026-10-07）：**D1 於 `6338fd2` 通過 Codex 限定範圍技術 review；R1、R2、C1 關閉。通過範圍為 fake 展示的網頁呈現與相關修正，不代表真實 Agent、付款、區塊鏈、ZK 或整體 M1 完成。B1-04／B1-06 整體維持 InProgress。**（§15.7）**D2 設計（§16，`9b70c0c`）經 Aaron 10/7 核准實作：程式與測試 `bc79677`，計時修正 `d7070af`；依 Codex 對 `d7070af` 的 review 修正於 `78cb2cc`、測試補充 `6137708`（§17.6）；Claude Code 執行，待 Codex 獨立 review。不合併、不推送、不標 Done。** D3–D6 未開始。D1 實作於 `7ffc45d`，review 後修正於 `6338fd2`；設計於 `15d705a` 通過（§14.3）。

- **依據**：Aaron 轉交的〈B1-06 商店展示深化設計指示〉（vault，2026-10-07）。
- **Aaron 的回饋**：已實際操作目前 demo，初步確認基本展示可用，未發現重大問題；現有情境仍然陽春，同意往展示內容深化推進。這不是 `docs/demo-operation-design.md` 16.3 人工清單逐項完成的聲明。
- **開工核對**：
  - 分支 `b1-06-demo-integration`，HEAD `17efeac`，main `5391373`。
  - 工作區只有未追蹤的 `demo` 執行檔（不處理）。
  - 最近的產品修正是 `d18efa2`（停止的 202 受理通知）。
  - 證據歸屬沿用既有紀錄：`d18efa2` 的 255 項瀏覽器檢查由 Claude Code 執行；最近完整容器與 247 項突變屬 `667e933`。本輪不重跑，也不重新歸屬。

## 1 本輪讀過的資料

| 資料 | 用途 |
|---|---|
| PRD-第一期 §5（L152–184）、§8（L234–255）、§9 FR-4／5／16–20／23–25（L261–302）、§13（L366–376）、止損（L359） | 三區畫面、Inspector 必要欄位、證據欄位、報價與結算分開、角色點選待另行派工 |
| 執行計畫 §1-novies（L344–486）、§4（L675–705）、§5 驗收 37–44（L714）、§8（L807）、§9.1（L821） | 展示層定位「可互動的系統觀測介面，不是遊戲」、W12–W13 完整 UI 排程、止損 |
| 任務清單 B1-06 卡（L187–212）、B1-07 卡（L214–227）、狀態表（L49–66） | B1-06 Done 條件、Inspector 欄位、B1-07 唯讀驗證範圍 |
| 〈商店互動展示設計-WareTrack動畫參考〉 | 成品定位與證據主線、目標配置、動作對應、權限界線 |
| repo `docs/demo-operation-design.md`、`docs/demo-ui.md`、`docs/demo-integration.md`、`docs/m1-boundaries.md`、`docs/merchant.md`、`docs/contracts.md`、`docs/policy.md` | 現行行為與限制 |
| 程式：`web/`（index.html、host.html、app.js、host.js、hostcreds.js、adapter.js、render.js、unity.js、style.css）、`unity/AgentPaymentAuthority/Assets/APA/`（Projection.cs、ShopBridge.cs、ShopView.cs、ShopBuilder.cs、apa-bridge.jslib）、`cmd/demo/api.go`、`control.go`、`internal/store/ops.go`、`internal/contracts/events.go`、`types.go`、`internal/merchant/merchant.go`、`internal/toolbridge/shop.go`、`internal/buyer/host.go`、`testdata/policy/m1-demo-profile.json` | 各步驟的資料來源與缺口 |

## 2 現況摘要（設計的出發點）

**畫面**
- `/` 與 `/host` 都是：上方 badge → 認證與控制區（`/host` 含 token 檔、就緒表、主持人控制、最近場次、手動 viewer、task 清單）→ 下方三欄 grid（Unity 1.3fr、Inspector、Timeline），900 px 以下改為單欄（`web/style.css`）。
- 因此在 1280×900 的 `/host`，Unity 商店的位置在一個螢幕高度以下（`ui-d18efa270912-…/full-acceptance-stop.png`：商店區塊從約 860 px 開始）。觀眾得先捲過操作表單，才看得到場景。

**Unity**
- 場景是一間 12×8 格的商店，兩位客人 A／B（`ShopView.cs`）。只有本次 message 的 consumer 是完整亮度，另一位調暗且不移動。
- phase 對應三個站點：`entered`→門口、`quoted`／`unknown`→走道、`waiting`／`allowed_unpaid`→櫃檯，其餘→店外（ShopView.cs:26–43）。
- 走路是直線 `MoveTowards`，3 單位／秒，只用左右影格，不繞過貨架（:236–251）。
- 走到一半收到較新的序號時，停止並從目前位置直走到新站點；被打斷的那一步不回報 `rendered`。
- 沒有點選：無 collider、raycast 或 input。

**橋接**
- message 欄位：`v, gen, clear, consumer, task_id, seq, phase, verdict, reason, animate`（Projection.cs:13–22）。
- 只接受較新的 gen 或 seq；Unity 只經由 `apaUnityReport` 回報 `ready`、`applied`、`rendered`。

**資料**（一筆請購能查到的）
- `GET /api/tasks/{id}` 的 `PurchaseView`：`purchase_order_id`、`request_id`、`resource_url`、`chain_id`、`asset_id`、`pay_to`、`amount`、`intent_hash`、`payment_requirement_hash`、`decision`、`decided_at`、`deadline_at`、`approval_closed`、`closed_at`。
- `GET /api/tasks/{id}/events`：Store 事件，含序號、`observed_at`（Store 時鐘）、`prev_event_hash`、`evidence_mode=fixture`、`source=host`。
- `GET /catalog`：商店目錄，不需 token，是 fixture 價格：KRW 700、0.50 test USDC、FX 1400，並註明「binding quote 是 402 的 PAYMENT-REQUIRED」（`internal/merchant/merchant.go:108–118`）。

**沒有的資料**
- `list_products`、`get_quote` 不寫 Store。run trace 只記 `{tool, product_id, outcome}`，用牆鐘時間，沒有任何 HTTP 端點可讀（`cmd/demo/ui.go`）。
- 請購當時的完整 402 報價存在 `purchases.body`，但沒有 API 讀得到。
- `PURCHASE_PROPOSED` 與 `POLICY_DECIDED` 在同一個交易內寫入（`internal/store/ops.go:538–543`）。所以即時模式永遠看不到「已報價、未裁決」，`quoted` phase 只會在歷史回放的逐步重演中出現。
- **Allow 在現有 fixture 下到不了**：profile `allow_max=100000`（0.10），唯一商品的 402 金額是 500000（0.50）。依 `docs/policy.md` 規則 6，只有金額不超過 `allow_max` 才 Allow。

## 3 展示主線與每一步的依據

圖例：
- **A 有可靠資料**：Store 已提交事件，或 API 讀回的持久化欄位。
- **B 表現**：走路、待機、補間，只是表現。
- **C 觀測缺口**：需要新增觀測資料才能誠實呈現。
- **D 目前無法證明**：不能演成已完成。

| # | 主線步驟 | 分類 | 依據或缺口 |
|---|---|---|---|
| 1 | 客人進店 | A＋B | **A**：`TASK_STARTED`（`ops.go:388`），代表 task 已建立。它在 init 閘門之後、任何工具呼叫之前寫入，不代表「走進商店」這件事本身。<br>**B**：從店外走到門口是表現。 |
| 2 | 選擇資料商品 | C＋D | **C**：`list_products`、`get_quote` 沒有 Store 事件；trace 有工具名稱與結果，但沒有 HTTP 可讀。<br>**D**：fake 模式下是腳本，不是模型的選擇，不得顯示成「Agent 思考後決定」。 |
| 3 | 到櫃檯取得 HTTP 402 報價 | A（綁定）＋C（時刻） | **A**：請購綁定了 402 的 `payment_requirement_hash`，以及 `amount`、`asset_id`、`chain_id`、`pay_to`、`resource_url`。<br>**C**：拿到 402 的時刻沒有事件；完整報價沒有 API；目錄價格（KRW 700、FX 1400）是 fixture，不與此請購綁定。 |
| 4 | Host 組裝並提交請購 | A | `PURCHASE_PROPOSED`（`purchase_order_id`、`intent_hash`），與裁決同一交易。 |
| 5 | Policy 裁決 | A | `POLICY_DECIDED`：verdict、reason、approval_status、payment_status=unavailable、`decided_at`、`deadline_at`。 |
| 5a | Ask → 等待 | A＋B | **A**：`deadline_at` 由 Store 計算。<br>**B**：等待的待機動畫；前端倒數只是提示。 |
| 5b | 到期或撤回 | A | `APPROVAL_EXPIRED`（APPROVAL_TIMEOUT）；撤回只會因 task 取消而發生（`APPROVAL_WITHDRAWN`，HOST_CANCELLED）。 |
| 5c | Deny → 顯示原因 | A＋B | **A**：reason 固定代碼，例如 INSUFFICIENT_BUDGET。<br>**B**：離店動畫。 |
| 5d | Allow → 明示 M1 無付款能力 | A（語意）＋C（無法演示） | 契約與 adapter 已有 `allowed_unpaid`，但現有 fixture 永遠到不了（見第 2 節），需要決定（第 10 節）。 |
| 6 | 收尾：task 取消、CLI 逾時 | A | `TASK_CANCELLED`、`TASK_TIMED_OUT`。場次（`host_runs`）的結果另外記錄，不等於 task 狀態。 |
| — | 付款、取貨、完成 | D | M1 沒有 Signer、結算、鏈上交易與交付（`docs/m1-boundaries.md:3`）。一律標「未實作」。 |

## 4 主畫面與操作設計

「視覺 60％、技術證據 40％」是兩者的重視比例（PRD §5 L156），不是工時、完成率或驗收分數。設計上，讓觀眾先從場景看懂，再沿同一筆請購查證；兩者不互相抵銷。

### 4.1 桌面版線框（1280×900，`/host`）

```text
┌────────────────────────────────────────────────────────────────────────────────────────┐
│ M1／fake agent／不付款 │ 模式：即時｜回放中 │ 目前：consumer-a · show-a-03 · po-show-a-03-kr-market │
│ 能力：真實 Agent 未啟用 · 付款 未實作 · 憑證／ZK 未實作 · B1-07 獨立驗證 未執行 │ 就緒 ● 正常 ▾        │  ← 狀態列（固定）
├───────────────────────────────────────────────────────┬────────────────────────────────┤
│                                                       │ 這筆請購（目前選取）              │
│                  Unity 商店（主畫面）                  │ 客人 A · consumer-a · show-a-03  │
│          ┌──────────────────────────────┐             │ 步驟 ①進店 ②選商品○ ③報價○ ④請購   │
│          │  門 → 走道 → 櫃檯；A／B 標籤   │             │      ⑤裁決 ⑥收尾（目前：⑤ 等待）  │
│          │  選取框只框目前的客人          │             │ 狀態：Ask 等待核准（M1 無核准能力）│
│          └──────────────────────────────┘             │ 期限：18:41:56（Store 時間）       │
│  場景說明：Ask：等待人工核准（M1 無法核准）             │ 報價：500000（0.50 test USDC）     │
│  Unity 已呈現 show-a-03 #3 waiting                     │       目錄 KRW 700（fixture）      │
│                                                       │ 裁決：Ask · HUMAN_APPROVAL_REQUIRED│
├───────────────────────────────────────────────────────┤ 付款：未付款（unavailable）        │
│ 主持人：[情境 ▾ 主商品 Ask] [開始] │ 執行中：show-a-02 [停止] │ [查證這筆 ▸]  [其他請購 ▾]       │
│ 本筆：[取消 task show-a-03]  訊息：場次 run-… 的停止請求已受理…│                                │
├───────────────────────────────────────────────────────┴────────────────────────────────┤
│ Timeline（人讀事件，最新在上；可收成 3 列，錯誤列永遠顯示）                                      │
│ #3 POLICY_DECIDED Ask / HUMAN_APPROVAL_REQUIRED · 18:41:26 Store · host · fixture   [JSON ▸]  │
│ #2 PURCHASE_PROPOSED po-show-a-03-kr-market · 18:41:26                              [JSON ▸]  │
├──────────────────────────────────────────────────────────────────────────────────────────┤
│ ▸ 憑證與就緒（token 檔、就緒表；未載入時預設展開）  ▸ 最近場次（3 筆異常：見內）               │
│ ▸ 手動 viewer 模式   ▸ task 清單（清單更新於 18:41:30）   ▸ 技術詳情（Inspector 全欄位）       │
└──────────────────────────────────────────────────────────────────────────────────────────┘
```

- **Unity 位置**：在第一個螢幕高度內，約佔寬 62％。canvas 仍是 3:2，由 CSS 決定大小；Unity 會跟著 canvas，不需要重建（`docs/demo-ui.md` §2；`style.css:18`）。
- **主持人常用操作集中在一列**：情境選擇與開始、執行中場次的停止、目前選取 task 的取消（只在 `docs/demo-operation-design.md` 第 15 節的條件成立時出現）。完整的最近場次清單收在下方，可展開。
- **收在下方、可展開的區塊**：憑證、就緒、手動 viewer、task 清單、Inspector 全欄位。
  - 用原生 `<details>`。首次使用（尚未載入 token 檔）時，「憑證與就緒」預設展開。
  - 收合後，摘要列仍顯示狀態：「就緒：1 項未確認」「最近場次：1 筆中斷」「清單更新失敗（上次 18:41:30）」。錯誤、未確認、fake、回放不會因收合而消失。
- **狀態列固定在頂端**，永遠顯示：
  - fake 模式與不付款；
  - 即時或回放；
  - 目前 consumer、task、`purchase_order_id`；
  - 能力條：真實 Agent、付款、憑證／ZK、B1-07 的狀態。

### 4.2 一般 viewer 頁（`/`）

- 版面相同，但沒有主持人列、最近場次與憑證檔區。
- viewer token 輸入收在「viewer 驗證」區；尚未驗證時預設展開。
- 權限不變：viewer 只看自己 consumer 的 task，不顯示 operator 操作或其他 consumer 的資料。

### 4.3 窄視窗（900 px 以下）

由上而下：狀態列（換行顯示，不省略 fake 與回放）→ Unity（滿寬）→「這筆請購」卡 → 主持人列（按鈕可換行）→ Timeline（預設 3 列）→ 可展開區。資訊不能只藏在角色動畫裡。

### 4.4 「這筆請購」卡

這是 Inspector 的摘要層。完整 Inspector 欄位（任務清單 L199、PRD L163）仍保留在「技術詳情」與查證面板，不減少。

- **選取單位**：一筆 `purchase_order_id`。要分清兩個概念：
  - **場景呈現的請購**：網頁送給 Unity 的投影所依據的那筆，也就是 `adapter.scene()` 選出的最新裁決（沒有裁決時是最新的提案）。
  - **選取的請購**：卡片與查證面板正在顯示的那筆。
- **預設跟隨場景**：沒有手動選取時，選取的請購就是場景呈現的請購；新快照讓場景改成另一筆時，卡片跟著換。
- **手動選舊請購**：同一 task 有多筆請購時，可用「其他請購 ▾」選另一筆。
  - 只改卡片與查證面板，不改 Unity 的場景，也不送新的投影。
  - 卡片頂端固定顯示「你正在查看 po-X；商店場景呈現的是 po-Y」，並提供「回到場景這筆」。
  - 這是選取不同，不是同步失敗：不顯示「Unity 同步中」或任何錯誤。同步狀態只比較場景呈現的那筆（第 7 節「場景對應」頁）。
  - 切換 consumer、切換 task、清除或世代改變時，手動選取一律重設為跟隨場景。
- **步驟條 ①–⑥**：沒有獨立事件的步驟（② 選商品、③ 報價時刻）用空心圈，註明「無獨立事件」。只有在已提交事件到達時，才點亮 ④、⑤、⑥。
- **金額**：分開顯示 402 綁定的 amount（基本單位與 0.50 test USDC）和目錄的 KRW 700（fixture，不綁定）。不使用同一個「金額」欄位代表兩者（執行計畫 L413、PRD L167）。
- **期限**：顯示 `deadline_at`，並標明 Store 時間。前端倒數到 0 時只顯示「已達期限，等待後端確認」（現有 `adapter.scene()` 行為）。直到 `APPROVAL_EXPIRED` 寫入，才顯示「已到期」。

### 4.5 回放時的卡片與步驟條（R1）

回放時，卡片與步驟條只呈現「回放當步」，資料界線見 §6.2。

- **標題**：「回放當步 #n／共 m（唯讀）」。
  - n 是回放目前的事件序號，m 是保存的事件總數。
  - 狀態列同時顯示「回放中」。
- **步驟條**：只點亮序號不大於 n 的事件。尚未回放到的步驟一律空心，標「尚未發生（回放位置 #n）」。
- **卡片欄位**：
  - 請購固定資料只在該筆的 `PURCHASE_PROPOSED` 已回放後出現。
  - 裁決、原因、等待核准只在 `POLICY_DECIDED` 已回放後出現。
  - 到期、撤回只在對應事件已回放後出現。
  - task 取消、逾時亦同。
  - 尚未發生的欄位顯示「尚未發生」，不顯示最終值。
- **期限**：
  - `deadline_at` 在該筆 Ask 的 `POLICY_DECIDED` 已回放後才顯示，標「Store 時間；裁決時已決定的期限」。
  - 回放時停用倒數與「已達期限，等待後端確認」提示。
  - 是否到期只看回放是否已到 `APPROVAL_EXPIRED`。
- **手動選取**：回放中停用「其他請購 ▾」（最小方案）。
  - 卡片跟隨回放當步的場景請購。
  - 回放開始時，R2 的手動選取重設為跟隨場景；回放結束後也從跟隨場景開始。
  - 要看完整歷史，請用標示過的完整紀錄區（§7.2）。

## 5 場景動作／資料狀態／查證證據／失敗呈現 對照表

狀態欄說明：
- **現有**：已實作並測過。
- **待補**：本設計提案的切片會做。
- **未實作**：M1 不做或待決定。

| 場景動作 | 資料狀態（來源） | 查證證據 | 失敗與不確定時的呈現 | 狀態 |
|---|---|---|---|---|
| 客人從店外走到門口 | `TASK_STARTED`（events；`adapter.scene` → `entered`） | Timeline #1；`observed_at`（Store） | 讀取失敗時保留最後一致快照，狀態列顯示「離線／更新失敗；最後快照時間」（`app.js` show()） | 現有（站點與走路）；走路改由多個中途點連成是待補（D2） |
| 走到貨架（選商品） | 沒有事件；trace 有 `tool_call get_quote`，但不可讀 | 無；步驟條空心，註明「無獨立事件」 | 不演「思考」，不用字幕冒充推論 | 未實作（觀測缺口，見 7.1） |
| 走到櫃檯（402 報價） | 即時模式下與請購、裁決同時到達；只有回放能逐步呈現 `quoted` | `payment_requirement_hash`、amount、asset、chain、payTo（PurchaseView）；目錄價格標 fixture | 即時模式不演「先報價、等待裁決」的時間差，直接走到裁決對應的站點 | 現有（綁定欄位）；卡片與查證面板是待補（D1） |
| 提交請購 | `PURCHASE_PROPOSED`（`purchase_order_id`、`intent_hash`） | Timeline #2、查證面板「請購」頁 | `intent_hash` 缺少或不一致時，顯示「未驗證」，不以時間推定 | 現有（事件）；面板待補（D1） |
| Ask：在櫃檯等待 | `POLICY_DECIDED` Ask；`deadline_at`（Store 時間） | 裁決頁：verdict、reason、approval_status=pending | 已過期限但尚未寫入：「已達期限，等待後端確認」（現有） | 現有 |
| Deny：離店 | `POLICY_DECIDED` Deny；`deny_reason` | 裁決頁；之後的取消或逾時不覆蓋原因（`project()`） | 「Deny：原因」保留（3.4 已驗收） | 現有 |
| Allow：在櫃檯，「政策允許，M1 未付款」 | `POLICY_DECIDED` Allow、`PAYMENT_CAPABILITY_UNAVAILABLE` | 裁決頁：payment_status=unavailable | 不播放付款或取貨 | adapter 與 Unity 有此 phase；**現有 fixture 無法演示**（第 10 節決定 1） |
| Ask 到期：離店 | `APPROVAL_EXPIRED`（只關閉該筆 po） | Timeline、裁決頁的收尾 | 到期不代表 task 結束：task 仍可由主持人取消（3.4） | 現有 |
| 撤回：離店 | `APPROVAL_WITHDRAWN`（task 取消時） | 同上 | — | 現有 |
| task 取消 | `TASK_CANCELLED`（`HOST_CANCELLED`） | 最近場次的「目前」段；Timeline | 503：「取消結果尚未確認」（3.4） | 現有 |
| 場次停止 | `host_runs` 的 live／outcome／`store_end` | 最近場次的「當時」段 | 停止的 202 只是受理通知；「已停止，取消已記錄」只在 `store_end=TASK_CANCELLED` 時顯示（3.2、3.5） | 現有 |
| 多筆請購 | 每筆 po 各自的裁決、原因、期限（`adapter.scene()` 依 po 對齊） | 卡片「其他請購」、查證面板依 po 切換 | 不混用不同 po 的欄位 | 場景現有；卡片切換待補（D1） |
| 讀取失敗、舊資料 | 最後一致快照；`served_at` 與瀏覽器讀取時間分開標示 | 狀態列與卡片的時間來源標籤 | 「更新中」「離線」「Ask 狀態未確認」（3.3、3.4） | 現有；卡片待補 |
| 服務重啟 | 憑證 401 → 清除 → 重新載入檔案 | 最近場次保留 | 舊畫面不殘留（3.1、3.5） | 現有 |
| 資料尚未同步到 Unity | `#unity-status` 的 `rendered` 與快照序號比較 | 查證面板「場景對應」頁 | 「Unity 同步中」；走路中收到新狀態時改為追上（6.1） | 部分現有；追上規則待補（D2，6.1） |
| 歷史回放 | 不讀新資料，逐步重演已保存事件；場景現有；卡片只用回放位置以前的事件（§6.2） | 狀態列「回放中」；卡片「回放當步 #n／共 m」；完整紀錄區標「完整紀錄，非回放當步」 | 不寫 Store、不重送請購；結束後恢復最新狀態（現有）；不提前顯示裁決、收尾或到期 | 場景現有；卡片與標示待補（D1） |
| 點選客人 | 只是選取意圖；網頁檢查權限 | 卡片切到該客人可見的 task | 無權限時顯示「需要該 consumer 的 viewer token」 | 未實作（D3，需要另行派工，PRD L158） |
| 付款、取貨、完成 | 無 | 能力條標「付款 未實作」 | 一律不演 | 未實作（M1 之外） |

## 6 守則與設計的對應

1. **權威結果來自後端**：Unity 的 phase 只由 `adapter.scene()` 從已提交事件決定（現有）。卡片與步驟條也只讀同一份快照，動畫計時器不決定任何狀態。
2. **補間不產生裁決**：走路與待機只連接兩個已成立的站點。`rendered` 只是「已呈現」的回報，不回寫狀態。
3. **資料先於動畫到達時追上**：見 6.1。網頁上的文字（狀態列、卡片、Timeline）一律立即更新，不等 Unity。
4. **不冒充推論**：fake 模式不顯示任何「思考」字幕。步驟②只標「無獨立事件（fake 腳本）」。
5. **未取得詳情就是未確認**：沿用 3.4 R2 的規則，套用到卡片。
6. **倒數不等於後端到期**：沿用現有的「已達期限，等待後端確認」。到期只關閉該筆 Ask，task 仍可取消。
7. **依 `purchase_order_id` 對齊**：卡片、查證面板與場景都以 po 為單位（`adapter.scene()` 已如此）。選取的請購與場景呈現的請購可以不同，畫面必須明示（4.4）。
8. **場次停止 ≠ task 取消**：最近場次維持「當時」與「目前」分開（3.4）；Deny 原因不被覆蓋。
9. **世代與晚到防護**：卡片與查證面板掛在 app.js 的同一個世代（`state.gen`、`idGen`、`listSeq`），清除或切換時一起清空。Unity 的選取回報也要帶 gen，舊 gen 一律丟棄（D3）。
10. **Unity 不碰 token**：選取只用 `APA_Report("select", gen, seq, "consumer|task")` 回報 Unity 自己已知的值；網頁再檢查權限後，用既有的認證讀取。
11. **回放**：沿用現有規則（明標回放、不寫 Store、不重送請購、結束後恢復最新狀態）。卡片只顯示回放當步；資料界線見 §6.2。

### 6.1 追上規則（D2）

> 已由 §16.5 取代（D2 設計修訂，16.12）。以下保留原文。

- 走路中收到較新的序號：
  - **新 phase 對應的站點在下一個中途點之後**：取消剩餘的中途點，以加速（例如 2 倍）走到目標，到達後只回報最新序號的 `rendered`。
  - **落後超過一個 phase，或使用者剛切換畫面**：直接瞬移到目標（`animate=false`）。
- 單次走路最長 1.5 秒；超過時以瞬移收尾。過場不能阻擋安全狀態更新，例如 Deny 或到期一到就要在網頁上顯示，不等 Unity 走完。
- 被取代的那一步不回報 `rendered`（現有行為）。測試以最新序號的 `rendered` 為準。

### 6.2 回放的資料界線（R1）

- **現況與風險**：
  - `web/app.js` 的 `replay()`（約 374–395 行）逐步呼叫 `A.scene({ task: snap.task, events: snap.events.slice(0, i + 1) })`：事件是前綴，但 `task` 是完整的最終快照。
  - 場景的裁決取自事件前綴，所以 Unity 的 phase 與 verdict 不會提前。但卡片若直接讀 `PurchaseView` 的 `decision`、`approval_closed`、`closed_at`，就會在回放到「提出請購」時，提前顯示後來的裁決或到期。
  - 此外，`scene()` 會用 `task.served_at` 與 `deadline_at` 比較，加上「已達期限，等待後端確認」；回放時這等於用現在的時間判斷歷史 Ask。
- **規則**：
  1. **回放位置**：本次回放的 task、世代（`A.bump` 後的 gen）與目前事件序號 n。三者都要符合，當步內容才採用；世代改變時，一切作廢。
  2. **只用前綴事件**：當步卡片、步驟條與當步裁決，只採用序號不大於 n 的事件。
     - 裁決、原因、approval_status 取自前綴中的 `POLICY_DECIDED` 事件，不取 `PurchaseView.decision`；
     - 收尾取自前綴中的 `APPROVAL_EXPIRED`／`APPROVAL_WITHDRAWN`，時間用該事件的 `observed_at`；
     - task 狀態依前綴事件，以與 Store `project()` 相同的規則在網頁推導，並標「依已回放事件推導（網頁）」。
  3. **完整快照不冒充歷史**：
     - 只有前綴中已出現 `PURCHASE_PROPOSED` 的 `purchase_order_id`，才從 `PurchaseView` 取它的固定欄位：`request_id`、`resource_url`、`http_method`、`amount`、`asset_id`、`chain_id`、`pay_to`、`intent_hash`、`payment_requirement_hash`。這些欄位在提出時就固定（意圖以第一個 `PURCHASE_PROPOSED` 為準，`internal/contracts/events.go`）。
     - `PurchaseView` 的 `decision`、`approval_closed`、`closed_at` 在回放中不使用。
     - `deadline_at` 只在該筆 Ask 的 `POLICY_DECIDED` 已在前綴時顯示；它在裁決時就已決定，不是之後的事實。
     - task 層的綁定（consumer、agent、wallet、payer、task_id）是 task 開始時就固定的，可以顯示。
     - 沒有可靠依據的欄位顯示「未提供」或「尚未發生」，不推算補值。
  4. **完整紀錄可以保留**：Inspector 全欄位、完整 Timeline、查證面板可以繼續顯示整份已保存紀錄，但頂端要明標「完整紀錄，非回放當步（回放位置 #n／共 m）」。
     - Timeline 標出回放位置，之後的事件加註「回放尚未到此」。
     - 不在任何地方宣稱所有區塊都是當步狀態。
  5. **不以現在判斷歷史到期**：回放時，當步的計算不讀 `Date.now()`、最新的 `served_at`，也不使用倒數；「已達期限，等待後端確認」不出現在當步。
     - D1 的做法：`scene()` 的期限提示只在即時模式加上；回放傳入的當步檢視沒有 `served_at`。這會改變現有回放的場景文字，但只是不再用現在的時間；phase 與裁決不變。
  6. **與 R2 的選取**：回放中停用手動選取（§4.5）。回放開始與結束都沿用世代重設：手動選取歸零，卡片跟隨場景。
  7. **退出回放**：結束、停止或切換 task／consumer 時，沿用現有的 `replayRun++` 與世代作廢舊回放；舊世代的 Unity 回報由 `unity.js` 與 `renderedBy` 丟棄，當步檢視一併清除。
     - 回到即時模式時，先讀取新的一致快照（現有 `restore()` → `poll(true)`），卡片與場景才恢復最新狀態。
     - 不沿用歷史當步的裁決。
- **實作形態**（D1，不改 Store、Policy、契約或 API）：在 `web/adapter.js` 加一個純函式，例如 `stepView(snapshot, n)`，回傳只依前綴事件與固定欄位組成的當步檢視；`replay()` 每一步把它交給卡片與步驟條。
  - 場景投影可以繼續用現有 `scene()`，但改傳當步檢視，以符合第 5 條。
  - 不建立新的事件系統、資料表或 API。

## 7 證據面板（「查證這筆 ▸」）

以一筆 `purchase_order_id` 為單位，分頁顯示。每個欄位都標出來源：
- **Store**：已提交、持久化；
- **API**：伺服器讀取時的回覆，例如 `served_at`；
- **商店 HTTP**：`/catalog`，fixture；
- **瀏覽器**：前端觀測，例如讀取時間、Unity 回報。

瀏覽器讀取時間不寫成後端快照時間。

| 頁 | 欄位（來源） | 備註 |
|---|---|---|
| 報價 | `resource_url`、`http_method`、`amount`（基本單位）、`asset_id`、`chain_id`、`pay_to`、`payment_requirement_hash`（Store，PurchaseView）；目錄 `fiat_amount=700 KRW`、`fx_rate=1400`、`fx_source="fixture, not live FX"`、`price_note`（商店 HTTP `/catalog`，瀏覽器讀取時間） | 標明「402 的報價以 hash 綁定在請購上；目錄價格只是說明，不與此請購綁定」。完整 402 內容（scheme、maxTimeoutSeconds、extra）目前沒有 API，顯示「未提供（見 7.1 補充提案 1）」 |
| 請購 | `purchase_order_id`、`request_id`、`intent_hash`、`payment_requirement_hash`、`PURCHASE_PROPOSED` 的序號與 `observed_at`（Store） | 「`intent_hash` 為 fixture domain（`agent-payment-authority/m1-fixture/v2/verifier-not-deployed`），不是真實付款綁定」（`docs/contracts.md:49`） |
| 裁決 | `policy_verdict`、`reason`、`approval_status`、`payment_status`、`decided_at`、`deadline_at`、`approval_closed`、`closed_at`（Store） | 固定原因代碼，附中文說明 |
| Store 事件 | 該 po 的事件，以及 task 層事件（TASK_*）：序號、類型、`observed_at`、`prev_event_hash`、`evidence_mode`、`source`（Store） | 「hash chain 只表示這份事件序列前後相連，不是 ZKP，也不是 B1-07 獨立驗證」；單一事件 JSON 按需展開（現有，PRD L314） |
| 場景對應 | Unity 最後回報的 gen、seq、phase、task、verdict、reason（瀏覽器觀測；`web/unity.js:44` 的 `rendered`，沒有 `purchase_order_id`），對照網頁送出的投影（gen、task、seq、phase）與當時 `scene().po`（網頁推導） | 請購關聯是**網頁推導**：網頁記住每個送出投影的 gen、task、seq 與 po，Unity 回報相同的 gen 與 seq 時，標「Unity 已呈現 task X #seq（phase）；對應請購 po-Y 由網頁依送出的投影推導，不是 Unity 獨立確認」。gen 或 seq 不一致時顯示「Unity 同步中」，不判定成功。只比較場景呈現的請購；手動選取的另一筆不列入同步判斷 |

**輸出限制**
- 只顯示授權可見、白名單或遮罩後的欄位，沿用 `render.js` 只用 `textContent` 的做法。
- 不顯示 token、capability、私鑰、原始 CLI 文字、完整憑證或 witness。run trace 不進面板，除非 7.1 補充提案 2 經過核准。

**不宣稱**
- 面板沒有「通過」「已驗證」字樣。它是證據呈現介面，不取代 B1-07 獨立驗證器；在 B1-07 實作並由獨立終端執行前，一律顯示「B1-07 獨立驗證：未執行」。
- HTTP 402 不等於付款完成；hash chain 不等於 ZKP。

### 7.1 資料不足時的最小補充方案（提案，本輪不實作）

1. **請購當時的報價**：在 `GET /api/tasks/{id}` 的 `PurchaseView` 加一個白名單子物件 `quote`，內容是 `purchases.body` 已存的 x402 版本、scheme、network、amount、asset、payTo、maxTimeoutSeconds、resource URL 與 extra 的 name／version。
   - 不改 schema、不改契約、不改事件，只是讀取已存資料的新 API 欄位。
   - 生命週期與請購相同（持久化）；權限與 task 讀取相同（viewer 只看自己的、host 全部、operator 403）。
   - 測試：Go HTTP（欄位白名單、權限、與 `payment_requirement_hash` 的重算一致）、adapter 測試。
   - 這是 API 擴充，需要另行核准（D4）。
2. **工具呼叫的時間線**（選商品、取得報價的觀測）：host 唯讀端點，回傳該 task run trace 的 `tool_call` 紀錄（tool、product_id、outcome，都是固定代碼）。
   - 需要先補兩件事：trace 改記 Store 時鐘或相對序號（現為牆鐘，不能和事件時間比較）；決定保存期限（`var/runs` 目前沒有清理）。
   - 隱私：只輸出固定代碼，不輸出 prompt、CLI 文字或 capability。
   - 這會改 trace 收集內容與 API，本輪不做；列為 D6，需要另行設計與核准。
   - 在此之前，步驟②維持「無獨立事件」。

### 7.2 回放時的查證面板（R1）

- 回放中，查證面板顯示完整的已保存紀錄，頂端固定標示「完整紀錄，非回放當步（回放位置 #n／共 m）」。
- 「Store 事件」頁標出回放位置，之後的事件加註「回放尚未到此」。
- 「場景對應」頁比較的是回放送出的投影（回放世代、task、序號 n、phase）與 Unity 回報。同樣標為網頁推導，並標「回放」。
- 面板不提供「當步版」的報價、請購、裁決頁；當步內容只在卡片上（§4.5、§6.2）。這是 D1 的最小方案，避免兩套當步邏輯。

## 8 實作切片（提案）

切片以「展示深化 D1–D6」暫稱，掛在 B1-06 之下。不命名為 3.6，不改里程碑或 Done 條件。實際編號由 Codex 與 Aaron 決定。

### D1 場景為主的版面＋一條 Ask 流程與證據串接（建議第一片）

- **對應**：B1-06（FR-17、FR-18 部分行為），任務清單 L199 的 Inspector 欄位與單一 JSON 展開。
- **修改**：`web/index.html`、`web/host.html`、`web/style.css`、`web/app.js`（卡片、步驟條、查證面板的資料都來自既有快照）、`web/adapter.js`（新增純函式 `summary()`、`steps()`、`evidence()`）、`web/render.js`；`web/host.js` 的主持人列。
  - 不改 Go、Unity、契約。目錄價格由瀏覽器直接讀 `/catalog`（同源，不需 token）。
- **觀眾看到的差異**：
  - 一開頁就看到商店，以及右側「這筆請購」卡（步驟條、狀態、報價分層、裁決、期限）；
  - 主持人一列按鈕完成開始、停止、取消；
  - 「查證這筆」打開五頁證據，每個欄位都有來源標籤；
  - 能力條直接說明哪些還沒做。
- **場景與證據的對應**：顯示在「場景對應」頁。Unity 回報的 task、seq、phase 與網頁送出的投影對照；po 的關聯由網頁依送出的 gen、task、seq 與投影推導，並標為網頁推導。**D1 不改 bridge**：Unity 端的 message 欄位與回報格式都不變，不需要重建。
- **相依**：3.1–3.5 的既有行為與測試。
- **回歸風險**：
  - DOM 重排會影響既有 255 項瀏覽器檢查的選擇器。保留既有 id（`#status`、`#inspector`、`#timeline`、`#run-status`、`#runs`、`#task-pick`、`#list-note`、`#readiness`、`#creds`）。
  - `<details>` 收合時，`innerText` 讀不到內容，檢查需要先展開。
- **失敗呈現**：沿用既有的未確認、離線、503 文字，並加到卡片上。收合狀態的摘要列一定顯示錯誤數。
- **驗收條件**：
  1. 1280×900 的 `/host` 與 `/`，Unity canvas 完整落在第一個 viewport 內（以 `getBoundingClientRect` 斷言）。開始、停止、取消也在第一個 viewport。
  2. **預設一致**（沒有手動選取）：Ask 流程中，卡片、查證面板、Inspector 全欄位、Timeline 與場景投影的 task、po、序號、phase 一致；Unity 回報的 task、seq、phase 與送出的投影一致；「場景對應」頁的 po 標為網頁推導。`intent_hash` 與 `payment_requirement_hash` 與 API 相同。
  （第 2、2b 項都是**即時模式**的驗收。回放另以第 2c–2e 項驗收。）
  2b. **手動選取不同**：在有兩筆請購的 task（可沿用 `TestSeedDisplayDB` 種子的 task-a：一筆已到期、一筆等待中）手動選舊的那筆：
     - 卡片與查證面板顯示舊筆的裁決、原因、期限，不混入新筆；
     - 卡片頂端顯示「你正在查看 po-X；商店場景呈現的是 po-Y」；
     - Unity 的場景與回報不變，且不出現「Unity 同步中」或錯誤；
     - 按「回到場景這筆」後回到預設一致；
     - 切換 task 或 consumer 後，手動選取重設。
  2c. **回放當步：單筆請購的歷史界線**。用完整歷史 `TASK_STARTED → PURCHASE_PROPOSED → POLICY_DECIDED Ask → APPROVAL_EXPIRED`（例如計畫 host 的 task-a 到期後，或 3.4 的種子資料庫；瀏覽器時間早已超過期限）。以 3.5 的測試掛鉤扣住 Unity 回報，讓回放停在指定步驟（每步最多 6 秒）：
     - #1：卡片沒有任何請購。
     - #2：只有該筆請購與固定資料；沒有 Ask、原因或到期。
     - #3：顯示 Ask 等待與期限（Store 時間）；即使完整快照已到期、瀏覽器時間早已超過期限，也不顯示到期或「已達期限」。
     - #4：才顯示到期。
     - 每一步，完整紀錄區都看得到「完整紀錄，非回放當步（回放位置 #n／共 m）」。
     - 另以撤回（取消時 `APPROVAL_WITHDRAWN`）與 task 取消（`TASK_CANCELLED`）的歷史各驗一次，不提前出現。
  2d. **回放當步：多筆請購**。用同一 task 先有 po-A、後有 po-B 的歷史（`TestSeedDisplayDB` 的 task-a：po-A Ask 後到期，po-B Ask 等待），回放停在 po-A 的步驟時：
     - po-B 的裁決、期限、收尾不進入當步卡片；
     - 尚未提出的 po-B 不出現在當步卡片；
     - 「其他請購 ▾」停用，卡片跟隨回放的場景請購。
  2e. **恢復與晚到回報**：
     - 回放結束或按「停止回放」後，卡片與場景恢復最新一致快照（含 po-B 與最終收尾）；
     - 回放中切換 consumer 或 task 後，放行舊回放的 Unity 回報，不恢復舊卡片或舊裁決；
     - 整個回放期間不送出任何寫入請求，事件數不變。
  Node（純函式）：同一份最終快照配上每個事件前綴，`stepView()` 不混入未發生的裁決、收尾、到期或請購；沒有 `served_at` 時不產生期限提示。
  3. 金額分兩行（402 綁定、目錄 fixture），沒有單一混合金額。
  4. 所有區塊收合時，fake、回放、未確認、清單更新失敗、就緒未確認仍然可見（以可見文字斷言，不只看 `data-*`）。
  5. 切換 consumer、task 或清除時，卡片與查證面板立刻清空；晚到的回覆不恢復舊內容（沿用世代測試手法）。
  6. 全頁沒有「付款成功」「已驗證」「通過」等字樣；能力條四項顯示未啟用、未實作或未執行。
  7. 窄視窗（390×844）的順序如 4.3，沒有水平捲動。
- **驗證層級**：Node（adapter 新純函式，含多筆請購、缺資料與回放前綴）、真實瀏覽器（上列各項，包括 2c–2e，加既有回歸；瀏覽器斷言以觀眾可見的文字、時間來源與「完整紀錄」標籤為準）。不需要 Unity 重建；Go 未改時不需要完整容器（依既往做法，slice 最終 commit 的取捨由 review 指定）。

### D2 場景節奏：中途點、四向走路與追上

- **對應**：B1-06（L197–198），PRD L162「動畫不能跑在事件前面」。
- **修改**：`ShopView.cs`（門→走道→櫃檯的中途點、上下行走影格、限時與加速、6.1 追上規則）、`ShopBuilder.cs`（如需門口道具）、PlayMode 測試；重建 WebGL。
  - bridge 欄位不變，所以 `web/` 不改。（已由 §16.4 取代：D2 提議新增兩個選填欄位，網頁也要改；見 16.12）
- **觀眾看到的差異**：客人沿通道進店、到貨架、再到櫃檯，不再穿過貨架；Deny 或到期時轉身離店；新狀態一到，會加速或瞬移追上，不卡在半路。
- **風險**：
  - Unity 重建會讓建置檔 hash 改變，證據要重記。
  - 回放的逐步時間會變長（`renderedBy` 的 6 秒上限要能容納）。
- **驗收**：
  - EditMode／PlayMode：每個 phase 的終點、追上規則、最長時間、只回報最新序號；
  - 真實瀏覽器：既有 Unity 斷言全過，回放順序檢查（`docs/demo-operation-design.md` 16.1）仍通過。
- **驗證層級**：Unity 測試＋建置＋瀏覽器。Unity 有變動時，最終 commit 跑完整容器（容器不含 Unity，但要確認沒有連帶影響）。

### D3 點選客人（選取意圖；Codex 建議列入近期，D1 版面完成後另行派工）

- **對應**：PRD L158「角色點選等互動待另行派工」；任務清單 L201「UI 只讀展示 endpoint」。
- **修改**：
  - Unity：客人 collider 與點擊，`APA_Report("select", gen, seq, "consumer|task")`，只回報 Unity 已收到的值；選取框。
  - web：`unity.js` 接收 `select`，依 gen 檢查（舊 gen 丟棄）。`app.js`／`host.js` 檢查權限：
    - 同一 consumer：卡片聚焦。
    - `/host` 已載入檔案時點到另一位：顯示「切換到 consumer-b？」，按下後才走既有的 `switchTo`；不自動切換，也不在點選時發出受保護請求。
    - `/` 點到另一位：顯示「需要該 consumer 的 viewer token」，不送任何請求。
- **風險**：Unity 的 input 設定（舊 Input Manager）；點擊和 Unity 之外的頁面捲動之間的事件衝突。
- **驗收**：
  - PlayMode：點擊產生 select 回報，內容只有 consumer 與 task；
  - 瀏覽器：
    - 以 CDP 在 canvas 上實際點擊；
    - 偽造的舊 gen select 被丟棄；
    - viewer 點到他人時沒有發出請求（以 fetch 包裝計數）；
    - 切換後沒有殘留。
- **驗證層級**：Unity＋建置＋瀏覽器。

### D4 請購當時報價的白名單欄位（需核准 API 擴充；Codex 建議暫緩）

- **對應**：FR-4（`payment_requirement_hash` 綁定）、PRD L163 的商家與報價欄位。
- **修改**：`internal/store`（從 `purchases.body` 讀出的白名單）、`cmd/demo/api.go`、adapter 與查證面板。
- **觀眾看到的差異**：報價頁顯示請購當時 402 的完整白名單內容，並可看到它與 `payment_requirement_hash` 相符（由伺服器重算後提供 match 結果，或前端只顯示兩者並標「未在前端驗證」，待 review 決定）。
- **驗收**：Go（白名單、權限、重算一致、未知欄位不外洩）、adapter、瀏覽器。
- **驗證層級**：Go 變更，最終 commit 跑完整容器與突變。

### D5 Allow 路徑可示範（Codex 建議採低價 fixture 商品；不阻擋 D1）

- **對應**：PRD 止損「一間商店三條路徑」（L359）、`docs/m1-boundaries.md:20`。
- 依決定改 fixture 或 profile，加入第三個情境；驗收：Allow 只顯示「政策允許，M1 未付款」，沒有付款或取貨。

### D6 工具呼叫觀測（需另行設計與核准）

內容見 7.1 補充提案 2。不在近期切片內。

## 9 設計驗收問題的回答

| 問題 | 回答（D1 之後） |
|---|---|
| 觀眾如何辨認目前客人與請購 | 狀態列的「目前：consumer · task · po」、Unity 客人頭上的 A／B 標籤與選取框（D3 前只有亮度區分）、卡片標題 |
| 目前走到哪一步 | 卡片步驟條：只有已提交事件點亮；沒有獨立事件的步驟標空心並註明 |
| 為何停下 | 卡片狀態與裁決：Ask（等待核准，M1 無核准能力，期限為 Store 時間）、Deny 加原因代碼與中文說明、到期、撤回、取消、逾時；讀取失敗時說明是未確認 |
| 哪裡能查證 | 「查證這筆」五頁，每欄標明來源；單一事件 JSON；最近場次的「當時」與「目前」 |
| 哪些能力尚未實作 | 能力條：真實 Agent 未啟用、付款未實作、憑證／ZK 未實作、B1-07 未執行；Allow 路徑與工具呼叫觀測列在本文件第 3、8 節 |

## 10 待 Aaron 決定

Codex 10/7 review 已對三項提出建議（第 14 節）。下列選項保留原文，最終仍由 Aaron 確認。

1. **Allow 路徑是否要能示範**（D5）。目前 fixture 下不可能出現 Allow。選項：
   - **a. 新增一個低價 fixture 商品**（例如 0.10 test USDC，等於 `allow_max`）：改商店 fixture 與 registry 報價，不改 Policy。代價是多一個商品與 402。
   - **b. 新增一個 `allow_max` 較高的 demo profile**：改 Policy 的可信設定，會影響既有 Ask 情境的語意，不建議。
   - **c. 維持不可示範**：在畫面與文件註明「M1 fixture 只有 Ask／Deny 路徑」，Allow 只由 adapter 與 Go 測試涵蓋。

   建議 a。
2. **D4 的報價補充是否核准**：這是讀取已存資料的 API 擴充，不改 schema 或契約。不核准時，報價頁只顯示 PurchaseView 已有欄位與 hash。
3. **D3 角色點選是否列入近期**：PRD L158 要求另行派工。若暫緩，D1 的卡片由網頁選取，場景只亮度區分。

## 11 與 PRD、執行計畫、任務清單的差異與建議

| 位置 | 差異 | 建議 |
|---|---|---|
| 任務清單 B1-06 卡 L198「TaskStarted → 進場；QuoteReceived → 報價；Ask → 等待；Deny → 離場」 | 契約沒有 QuoteReceived 事件；`PURCHASE_PROPOSED` 與 `POLICY_DECIDED` 同時寫入，即時模式沒有「報價」階段，只有回放會逐步呈現 | 不新增事件（本輪不改契約）。步驟條把③標為「無獨立事件」。若要真實呈現報價時刻，屬 7.1 補充提案 2（D6），需要另行核准 |
| PRD 止損 L359「一間商店三條路徑」；`docs/m1-boundaries.md:20` 的 Allow 行為 | 現有 fixture 只走得到 Ask、Deny | 見第 10 節決定 1 |
| PRD §5 L163、任務清單 L199 的 Inspector 欄位 | 本設計新增的卡片是摘要，欄位較少 | 完整欄位保留在「技術詳情」與查證面板，不降低要求 |
| PRD §5 L164「Timeline 預設顯示人讀得懂的事件」 | 本設計讓 Timeline 可收成 3 列 | 預設展開（3 列以上），收合時錯誤列仍顯示；不預設隱藏 |
| 執行計畫 L677、L704–705（W12–W13 完整三區 UI） | 本設計提前改善展示層 | 本設計屬 B1-06 的展示投影深化，不宣稱 W12–W13 的完整 UI 或安全回歸完成 |
| 任務清單狀態表 L65（B1-06 列） | 列中只寫到 3.1 通過；3.2–3.5 的結論（3.5 技術 review 通過、人工清單未逐項確認）記在「目前下一步」與交付紀錄中 | 本輪只指出差異，不改狀態表；建議下次驗收同步時一併更新 |

## 12 仍未完成的主線（分開報告）

| 主線 | 狀態 |
|---|---|
| 視覺與操作（fake 展示） | 3.1–3.4 已通過限定 review；3.5 已通過 Codex 技術 review（最近的產品修正為 `d18efa2`），Aaron 已實際操作並初步確認基本操作可用，但 16.3 完整人工清單未逐項確認；本文件是下一步設計 |
| 真實 Agent（真實 CLI 請購） | 未啟用；B1-04 InProgress，真實 CLI 執行未核准 |
| 鏈上支付（Signer、預留、x402 settle） | 未實作（M2） |
| 憑證與 ZK（BBS+、gnark） | 未實作（spikes 另計） |
| B1-07 獨立證據驗證 | Waiting；展示面板不取代它 |

## 13 本輪檢查（Claude Code 執行，待 Codex 獨立 review）

- 只做閱讀、`git` 狀態核對與文件連結檢查。沒有執行產品測試、Unity、容器或突變，也沒有拿舊測試結果當成本輪結果。
- 本文件引用的程式行號，以 `17efeac` 的檔案為準。

## 14 review 紀錄與補正

### 14.1 Codex 10/7 review（`9de6a7b`）

依據：Codex 對 `9de6a7b` 的靜態核對（文件與程式，沒有執行測試），由 Aaron 轉交。轉交的內容從 R2 開始；本輪只依轉交的內容補正。

**R2：區分「選取的請購」與「場景正在呈現的請購」**
- **問題**：原 §4.4 允許選舊請購而不改 Unity，D1 驗收卻要求所有區塊的請購一致，兩者矛盾。此外，Unity 的回報（`web/unity.js:44`）沒有 `purchase_order_id`，不能宣稱 Unity 自己確認了某筆請購。
- **補正**：
  - §4.4：預設跟隨場景；手動選舊請購時明示兩者不同，而且不算同步失敗。
  - §6 第 7 條：註明兩者可以不同。
  - §7「場景對應」：po 的關聯標為網頁推導，依送出投影的 gen、task、seq 建立，不是 Unity 獨立驗證。
  - D1：不改 bridge；驗收分成「預設一致」（第 2 項）與「手動選取不同」（第 2b 項）。

**狀態文字**：§11、§12 改為：3.5 已通過 Codex 技術 review；Aaron 已實際操作並初步確認基本操作可用；16.3 完整人工清單未逐項確認。B1-04／B1-06 仍為 InProgress。

**三個待決事項的建議**（Codex），第 10 節選項保留：

| 項目 | 建議 | 對切片的影響 |
|---|---|---|
| Allow 情境 | 後續採低價 fixture 商品，不調整既有 Ask 的上限 | D5 採選項 a；不阻擋 D1 |
| D4 報價 API | 暫緩 | D1 的報價頁只用 PurchaseView 既有欄位與 hash；完整 402 欄位顯示「未提供」 |
| D3 角色點選 | 列入近期，但在 D1 版面完成後另行派工 | D1 的選取由網頁操作；場景只用亮度區分 |

D1 的實作核准不需等 D2–D6 全部定案。本輪只補文件，沒有修改產品程式，也沒有執行測試、Unity 或容器。

### 14.2 遺漏的 R1：回放資料界線（Codex 10/7，`6ba9401` 複核）

依據：vault〈Claude-Code-B1-06-展示深化R1回放補充指示-6ba9401-20261007〉。

- Codex 已接受 `6ba9401` 的 R2 與狀態文字，本輪不重做。
- 本節記錄這次收到完整 R1 的事實；14.1「轉交內容從 R2 開始」的紀錄照舊保留。

**R1 的問題**：
- 設計要求卡片顯示回放當步，但現有 `replay()` 給 `scene()` 的是事件前綴配完整的最終 `task`。
- 卡片若直接讀 `PurchaseView` 的裁決、收尾欄位，會在回放到「提出請購」時，提前顯示之後的裁決或到期。

**補正位置與對應**：

| R1 規則 | 補正位置 |
|---|---|
| 1 明確的回放位置（task、世代、序號） | §6.2 第 1 條 |
| 2 不得提前顯示結果 | §4.5 卡片欄位、§6.2 第 2 條 |
| 3 完整快照不冒充歷史；固定欄位依已出現的 po | §6.2 第 3 條 |
| 4 完整 Inspector 可保留但要標示 | §6.2 第 4 條、§7.2 |
| 5 不以現在時間判定歷史到期；停用倒數 | §4.5 期限、§6.2 第 5 條（另指出現有 `scene()` 期限提示用 `served_at`） |
| 6 與 R2 選取相容：回放中停用手動選取 | §4.5 手動選取、§6.2 第 6 條 |
| 7 退出回放，重新取得一致快照 | §6.2 第 7 條 |
| D1 驗收：區分即時模式、回放當步與標示過的完整紀錄 | §8 D1 第 2、2b（即時）、2c–2e（回放）與 Node 項 |

- 方案只用既有事件、快照與前端純函式（`stepView()`），不修改 Store、Policy、契約、API 或 bridge。
- 測試計畫留到 D1 核准後執行。
- 本輪只改文件；沒有執行測試、Unity 或容器。

### 14.3 驗收紀錄：D1 展示深化設計於 `15d705a` 通過限定 review（2026-10-07）

依據：Codex 對 `15d705a` 的靜態核對（文件差異與現有程式），由 Aaron 轉交。沒有執行測試；通過的是設計，不是實作。

**Codex 確認**：
- 回放卡片只採用當步以前的事件，不提前顯示裁決或收尾；
- 完整紀錄與回放當步有明確標示；
- 不用現在時間判定歷史 Ask 到期；
- 多筆請購、選取重設、退出回放與晚到回報都有驗收條件。

**結論**：R1、R2 均已關閉。D1 待 Aaron 核准實作；B1-04／B1-06 維持 InProgress。

**下一步建議**（Codex）：只核准 D1（場景優先版面、請購卡片、證據面板與回放資料界線），不必等待 D2–D6，也不需要再修訂設計。D2–D6 與第 10 節的待決事項維持原狀。

## 15 D1 實作紀錄（Claude Code 執行，待 Codex 獨立 review；程式與測試 `7ffc45d`）

> 後續：review 後修正於 `6338fd2`，並於 `6338fd2` 通過 Codex 限定範圍技術 review（§15.7）。本節保留當時的寫法。

依據：Aaron 核准 D1 實作（2026-10-07），範圍依 §8 D1 與 §4.4、§4.5、§6.2、§7、§7.2。

開工時：HEAD `c7d8cd8`，main `5391373`，工作區只有未追蹤的 `demo`。

### 15.1 修改

只改 `web/` 與瀏覽器測試。Go、Unity、bridge、契約、Store、API 都沒有改（`cmd/`、`internal/`、`unity/`、`testdata/` 自 `c7d8cd8` 起 0 個檔案變動）。

**版面**（`web/index.html`、`web/host.html`、`web/style.css`）
- 頂端狀態列固定，永遠顯示：
  - M1／fake／不付款；
  - 即時或「回放中 #n／共 m（唯讀）」；
  - 目前的 consumer · task · 場景請購；
  - 能力條：真實 Agent 未啟用、付款未實作、憑證／ZK 未實作、B1-07 未執行；
  - 警示：清單更新失敗、就緒未確認，或錯誤；
  - 同步狀態列（`#status`）。
- `/host` 在狀態列另有「載入 token 檔」。
- 主區：Unity 商店（約 62％寬）與右側「這筆請購」卡；`/host` 在商店下方有主持人列，順序是：
  1. 執行中場次的「停止」與目前 task 的「取消 task」；
  2. 開始按鈕；
  3. 訊息；
  4. 切換 consumer。
- Timeline 在下方，預設展開。
- 可收合區（`<details>`）：憑證與就緒（未載入檔案時展開）、最近場次、手動 viewer、task 清單與回放、Inspector 全欄位。
- 900 px 以下改為單欄，順序是商店 → 卡片 → 主持人列。
- 既有元素 id 全部保留。

**純函式**（`web/adapter.js`）
- `project()`：與 Store 的 project 規則相同。
- `stepView(snapshot, n)`：§6.2 的回放當步檢視。只用序號不大於 n 的事件與提出時固定的欄位；不用最終紀錄的裁決與收尾欄位；沒有 `served_at`。
- `card()`：卡片模型，包含步驟條、狀態、期限，以及 402 綁定金額與目錄價格分列；預設跟隨場景，手動選取時標示不同。
- `evidence()`：五頁證據。每列有來源；請購關聯標為網頁推導；回放時標「完整紀錄，非回放當步」與「回放尚未到此」。

**畫面與流程**（`web/render.js`、`web/app.js`、`web/host.js`）
- `renderCard`、`renderEvidence`；Timeline 在回放時標示位置。
- `app.js` 只在內容改變時重畫卡片與證據。
- 回放每一步用 `stepView`。場景投影也改傳當步檢視，所以回放時不再出現以現在時間判斷的「已達期限」（§6.2 第 5 條預告的行為改變）。
- 記住送出的投影與 Unity 最後的回報，供「場景對應」頁比較。
- 目錄 `/catalog` 只讀一次，不帶 token，並標示讀取時間。
- `host.js`：主持人列、各收合區的摘要、就緒警示。

### 15.2 測試與證據（Claude Code 執行，待 Codex 獨立 review；全部在 `7ffc45d`）

| 項目 | 命令 | exit | 時間 | 證據 |
|---|---|---|---|---|
| Node 40 項（新增 6 項）、契約檢查 | `make check-contracts` | 0 | 7 秒 | `artifacts/b1-06/checks-7ffc45d16c1c-20261006T211503Z/` |
| 語法、`go vet` | `node --check`；`go vet ./...` | 0 | 5 秒 | 同上 |
| Unity 建置 | 未重建；4 個檔案的 hash 與先前證據相同 | — | — | 同上 `webgl-loaded.sha256` |
| 真實瀏覽器 280 項（新增 25 項 D1 檢查） | `node scripts/verify/browser-ui.mjs` | 0 | 682 秒 | `artifacts/b1-06/ui-7ffc45d16c1c-20261006T211517Z/` |
| 反向（R1、R2 保護拿掉） | 見 15.3 | 1／1（預期） | 148／686 秒 | `artifacts/b1-06/reversal-7ffc45d16c1c/`（`BASELINE.txt` 記執行基準） |

- 依 §8 D1 的驗證層級，Go 未改，所以沒有跑完整容器與突變；突變清單仍是 247 項，屬 `667e933` 的容器結果。
- 若 review 要求，可另跑完整容器。

**Node 新增 6 項**（`web/test/adapter.test.mjs`）：
- 單筆 Ask 後到期的回放 #1–#4：#3 即使最終紀錄已到期、`served_at` 遠超過期限，也只顯示等待；
- 撤回與 task 取消只在回放到時出現；
- 較晚的請購不進入較早的步驟，回放中不接受手動選取；
- 即時模式預設跟隨場景，手動選取標示不同，且證據頁不判為同步失敗；
- 證據頁在回放時標「完整紀錄」與「回放尚未到此」，每列有來源，金額分兩列；
- `project()` 在 Deny 之後的取消仍保留 Deny。

**瀏覽器新增 25 項**，對應 §8 D1 驗收；斷言都讀觀眾看得見的文字。

在 `TestSeedDisplayDB` 的兩筆請購 task（`/` 頁）：
- 第 1 項：1280×900 下 Unity 完整在第一個 viewport。
- 第 2 項（預設一致）：
  - 卡片是場景請購 po-B；
  - Inspector、查證面板與卡片的 po、`intent_hash`、`payment_requirement_hash` 相同；
  - Unity 回報與送出投影一致，請購標「網頁推導」；
  - 金額分列。
- 第 2b 項（手動選取不同）：
  - 選 po-A 後顯示「你正在查看 po-A；商店場景呈現的是 po-B」，狀態為已到期；
  - Unity 回報不變，沒有「同步中」，證據頁寫明選取不同、不是同步失敗；
  - 「回到場景這筆」後回到 po-B。
- 第 2c、2d 項（回放當步）：以測試掛鉤扣住 Unity 回報，讓每步停留 6 秒。
  - #1：沒有請購；
  - #2：只有 po-A 的固定欄位，沒有 Ask、原因或期限；
  - #3：Ask 等待中。最終紀錄已到期，瀏覽器時間也早過期限，仍不顯示到期，並標「依已回放事件推導」；
  - #4：才到期，且卡片沒有 po-B；
  - #6：po-B。
  - 每步都檢查 Inspector 註記、Timeline 註記、證據 banner、狀態列，以及「回放尚未到此」的事件數；回放中沒有手動選取選單。
- 第 2e 項：
  - 回放結束回到即時最新快照；
  - 回放期間事件數不變，也沒有非 GET 請求；
  - 回放中清除 viewer 後放行舊回報，不恢復卡片或回放。
- 第 4、6、7 項：
  - 全部收合時，狀態列仍顯示 fake、不付款與四項能力，且沒有「付款成功」「已驗證」「通過」；
  - 390×844 時商店在卡片上方，沒有水平捲動。

在 3.5 的驗收 host（`/host`）：
- 全部收合、尚未載入檔案時，狀態列顯示就緒未確認，並提供載入入口。
- 載入後，商店與開始按鈕在第一個 viewport；Ask 卡片的 402 綁定金額（500000，fixture-usdc）與目錄 700 KRW 分列，並標示不綁定；主持人列的「取消 task」在第一個 viewport。
- 已取消 task 的回放：#3 沒有撤回或取消，#4 撤回但 task 未取消，#5 才取消。

### 15.3 反向驗證

- 拿掉兩項保護後跑完整瀏覽器測試（工作區暫改，之後還原）：
  - R1：回放時卡片改用完整快照，不用 `stepView`；
  - R2：卡片忽略手動選取。
- 預期失敗的是第 2b 項（手動選取）與 2c、2d 項（回放當步）的檢查。
- **第一次：R1 與 R2 一起拿掉**（`r1-r2-combined.out`）：
  - 兩項「D1 by hand」以斷言失敗。
  - 接著按「回到場景這筆」時，因為按鈕不存在（選取失效就不會出現）而中止，沒有跑到回放檢查。所以這次只證明了 R2。
- **第二次：只拿掉 R1**（`r1-only.out`）：
  - exit 1，274 PASS／6 FAIL。失敗的正是兩筆請購 task 回放 #1–#4，以及已取消 task 回放 #3、#4。
  - #6 與 #5 是最後一步，當步內容與完整快照相同，所以仍通過。這符合預期，不代表保護失效。

### 15.4 測試基礎設施的變更與發現（請 review 特別看）

**headless Chrome 把頁面標成 hidden**
- **現象**：加入卡片後，在 3.1 段落「切換到 consumer-a」之後，headless Chrome 回報 `document.visibilityState === "hidden"`，而且之後一直維持。頁面依設計在 hidden 時不輪詢，所以「3.1 pagehide: a readiness poll is held」失敗，後面各段連鎖失敗。
- **排查**（每次都是完整瀏覽器測試跑到該點）：
  - 未改的 HEAD `c7d8cd8` 在另一個 worktree 跑：visible，通過；
  - 只換回舊 CSS：仍 hidden；
  - 換回舊 `app.js` 與 `host.js`：visible；
  - 只換回舊 `host.js`：hidden；
  - `renderPanels` 不做事：visible；
  - 只不畫卡片：visible；
  - 不畫證據：hidden；
  - 卡片不含步驟條：hidden；
  - 卡片不含欄位列：hidden。
  - 卡片改成只在內容變動時重畫，仍 hidden。
- **其他檢查**：
  - 獨立 CDP 探測（載入檔案、切換、開 task、手動 viewer、Fetch 攔截、截圖尺寸切換、開著 task 觀察 60 秒）都沒有重現；
  - 沒有 JS 例外，Chrome 記錄沒有 GPU 崩潰，螢幕沒有休眠；
  - 視窗狀態是 normal；`Page.bringToFront`、捲動、改尺寸、隱藏卡片，都不能讓它恢復 visible。
- **判斷**：觸發與卡片的呈現有關，但頁面程式無法設定 `visibilityState`。**根因未確認**（推測與 macOS 上 headless Chrome 的遮蔽或背景化判斷有關，未證實；依 Codex 對 `7ffc45d` 的 review C1 修正用語）。
- **處置**：
  - Chrome 啟動參數加上 `--disable-backgrounding-occluded-windows`、`--disable-renderer-backgrounding`。這是 Puppeteer、Playwright 的預設自動化參數。之後既有 255 項全部通過。
  - 頁面在 hidden 時的行為（暫停輪詢、恢復時立即輪詢），仍由既有以 `visibilitychange` 與 hidden 覆寫驗證的檢查涵蓋。
  - 風險：如果這個 hidden 其實反映真實瀏覽器會遇到的問題，這兩個參數會讓測試看不到它。人工操作時若發現頁面停止更新，請回報。

**兩處固定等待改為條件等待**
- 3.1「離開再回來」：原本導向 `about:blank` 後固定等 500 ms；改為等到目前的歷史項目確實是 `about:blank` 且可讀。原本在新參數下會讀到 detach 中的頁面。
- 3.5 的四處一致檢查：先等 Store 出現預期的最後事件。場次剛開始時 task 可能尚未寫入。

**時間**
- 測試 log 加上各段落的累計秒數：delivery 2 R1 110 s、D1 132 s、3.1 178 s、3.2 231 s、3.3 266 s、3.4 319 s、3.5 423 s，結束 681 s。
- 完整執行由 `d18efa2` 的約 305 秒變成 682 秒。
  - D1 新增的兩段回放每步停 6 秒，約佔 70 秒；
  - 其餘增加集中在 3.5 段落（258 秒）。原因未逐項分析，也沒有確認是否與新參數有關。

### 15.5 未做與未測

- D2–D6；第 10 節待決事項維持原狀。
- 查證面板沒有「當步版」（§7.2 的最小方案）。
- 卡片不以 Unity 點選觸發（D3）。
- 只用 Chrome。人工操作未做。

### 15.6 review 後修正 R1、R2、C1（Claude Code 執行，待 Codex 獨立 review；程式與測試 `6338fd2`）

> 後續：於 `6338fd2` 通過 Codex 限定範圍技術 review（§15.7）。本節保留當時的寫法，包括轉貼截斷與誤讀暫存目錄輸出的更正。

依據：Codex 對 `7ffc45d` 的 review，由 Aaron 轉交。轉交內容從 R1 的中段開始，R1 前段缺漏，本輪依可見的要求修正：
- 不符時明示尚未一致，不能當成同步完成；
- 補錯 task、錯 phase、舊序號與完全一致的測試；
- 請購關聯維持網頁推導，不改 bridge。

**R1：場景對應的「同步」必須完全一致**（`web/adapter.js` 的 `evidence()`）
- 原本只比較世代與序號。現在 Unity 回報與送出投影的世代、task、序號、phase 全部相同，才顯示「Unity 已呈現 …」。
- 任何不同時，顯示「Unity 尚未一致：<原因>（不是同步完成）」。原因包括世代不同、task 不同、序號較舊、序號不同、phase 不同。沒有回報時顯示「Unity 尚未回報：尚未一致」。
- 請購關聯仍標「網頁推導」，bridge 不變。
- Node 新增一組案例：完全一致、錯 task、錯 phase、舊序號、錯世代、無回報、請購關聯仍為網頁推導。原有回放案例的期望值改為「尚未一致」。

**R2：收合最近場次後，錯誤仍看得見**（`web/host.js`）
- 「最近場次」的收合摘要與頂端警示分成兩類：
  - 歷史：「歷史：N 場中斷或紀錄寫入失敗」，警示為「場次歷史：…」。
  - 目前：場次清單讀取失敗（顯示上次資料）、task 狀態讀取失敗（未確認）、N 場的 Ask 狀態未確認。警示為「最近場次目前：…」，錯誤色。
- 原本場次清單讀取失敗時，錯誤被丟棄；現在會記錄。
- 換世代（清除、換檔、手動模式、pagehide）時清空，恢復讀取後重新計算，不殘留。

**C1：持續 hidden 與不帶新參數的 Chrome**
- **持續 hidden → visible 檢查**（3.5 段落）：
  - 以測試覆寫 `document.hidden` 為 true 並維持 7 秒：task 輪詢次數、`/api/host/status` 讀取次數、`/api/host/runs` 讀取次數都不增加；這段期間伺服器新開的一場也還沒出現。
  - 恢復 visible 後 3 秒內，三者都立即讀取；新場次出現在最近場次，就緒的「檢查時間」更新。
  - 這驗證的是頁面對 hidden 與 visible 的反應，不是瀏覽器自己的可見性判斷。
- **不帶兩個新參數的 Chrome smoke**（`node scripts/verify/browser-ui.mjs --smoke-plain`）：
  - 流程：新資料庫、`/host`、載入 token 檔、開一場 Ask、卡片與 Unity 與 Timeline 一致、觀察 20 秒、回放一次。
  - 結果：PASS。開發中 47 秒；在 `6338fd2` 上 43 秒（`artifacts/b1-06/smoke-plain-6338fd214467-20261007T031110Z/`）。可見性取樣全部 visible；20 秒內 task 輪詢，開發中 20 次，在 `6338fd2` 上 19 次。
  - 限制：這是短流程。原本的 hidden 只在長流程的 3.1 段落後出現，**沒有在不帶參數的 Chrome 重跑長流程**。一般瀏覽器在長時間操作下的行為，仍未驗證。

**證據**（Claude Code 執行，待 Codex 獨立 review；全部在 `6338fd2`；Go、Unity、bridge 未改，`cmd/`、`internal/`、`unity/`、`testdata/` 自 `7ffc45d` 起 0 變動）：

| 項目 | 命令 | exit | 時間 | 證據 |
|---|---|---|---|---|
| Node 41 項（新增 1 組 R1 案例）、契約檢查、語法 | `make check-contracts`；`node --check` | 0 | 4 秒 | `artifacts/b1-06/checks-6338fd214467-20261007T030345Z/` |
| R1 反向（Node） | `same` 改回只比較世代與序號 | 1（預期：新的 R1 案例失敗，其餘通過） | 1 秒內 | 同上 `r1-node-reversal.log`（提交前工作區，`adapter.js` 其餘相同） |
| 真實瀏覽器 289 項（新增 9 項：R2 5、C1 4） | `node scripts/verify/browser-ui.mjs` | 0 | 427 秒 | `artifacts/b1-06/ui-6338fd214467-20261007T030403Z/` |
| 不帶兩個新參數的 Chrome smoke | `node scripts/verify/browser-ui.mjs --smoke-plain` | 0 | 43 秒 | `artifacts/b1-06/smoke-plain-6338fd214467-20261007T031110Z/` |
| R2、C1 反向（瀏覽器） | `host.js` 拿掉場次警示與摘要分類、輪詢在 hidden 時仍讀場次清單 | 1（預期，283 PASS／6 FAIL） | — | `artifacts/b1-06/reversal-6338fd214467/` |

- 瀏覽器反向的 6 項失敗：R2 的收合歷史、目前讀取失敗、恢復、重新載入檔案後歷史回來，共 4 項；C1 的「7 秒 hidden 不讀取」與「hidden 期間新場次尚未顯示」，共 2 項。後者在 hidden 時仍讀了 3 次場次清單。
- 「清除全部後不殘留」在反向下仍通過，因為反向版本本來就沒有警示。
- 開發中另有兩次完整執行，都是 289 PASS／0 FAIL（443、439 秒），在提交前的工作區執行，不作為證據。
- 依 review 指示，沒有跑完整容器、247 項突變，也沒有重建 Unity。

**更正一則過程中的誤判**：其中一次完整執行，我一度以為中途中斷。原因是在不同的暫存目錄讀到舊的輸出檔。實際上那次已正常完成（289／0），沒有任何執行被丟棄或重跑來湊結果。

### 15.7 驗收紀錄：D1 於 `6338fd2` 通過限定範圍技術 review（2026-10-07）

依據：
- Codex 對 `6338fd2` 的 review，由 Aaron 轉交；
- 完整的同步指示見 vault〈Claude-Code-B1-06-D1驗收同步指示-6338fd2-20261007〉。

**結論**：D1 於 `6338fd2` 通過 Codex 限定範圍技術 review；R1、R2、C1 關閉。通過範圍為 fake 展示的網頁呈現與相關修正，不代表真實 Agent、付款、區塊鏈、ZK 或整體 M1 完成。B1-04／B1-06 整體維持 InProgress。本輪未發現阻擋問題。

**關閉理由**：
1. **R1 場景一致性**：
   - Unity 回報的世代、task、序號、phase 全部和送出的投影一致，才顯示「Unity 已呈現」。缺少回報或任一項不同，就不宣稱一致。
   - 請購關聯仍標為「網頁推導」：不是 Unity 自己回報請購編號，也沒有修改 bridge。
   - 上一輪 R1 的完整要求就是這些。§15.6 記錄的轉貼截斷，沒有另一段遺漏的必修要求。
2. **R2 收合後的警示**：最近場次收合後，摘要與頂端仍分開呈現歷史中斷／紀錄寫入失敗，以及目前讀取失敗／Ask 未確認。恢復讀取、換檔或清除憑證後，提示依目前資料重新計算。
3. **C1 可見性測試**：持續隱藏時停止輪詢、恢復可見後重新讀取的測試，以及不帶兩個新增 Chrome 參數的短測試，符合本輪要求。關閉這項 review 要求，**不代表已查明原先長流程的可見性問題**。

**證據歸屬**：

| 類別 | 內容 |
|---|---|
| Codex 本輪獨立執行（以 `6338fd2` 匯出的唯讀來源，在既有映像 `sha256:78258368de0907e58498095c327a6ee4edbe75421d18e524bd00661c3749b6d0` 的斷網容器內，全部通過） | • `node --test web/test/adapter.test.mjs web/test/hostcreds.test.mjs`：41 項通過；<br>• R1 定向探測：完全一致、錯 task、錯 phase、較舊與較新的序號、錯世代、沒有回報；<br>• R2 定向探測：歷史與目前錯誤同時顯示、一般已結束場次仍顯示讀取失敗、缺詳情時 Ask 未確認、場次清單讀取失敗、恢復後清除目前警示、清除身分後舊世代不殘留警示。<br>R2 探測使用 DOM 替身，不等於操作真實瀏覽器。這是定向容器重跑，不是完整容器驗證七步。探測檔是 Codex 的暫存副本，命令與結果記錄在 Codex 的 review 對話中，repo 內沒有對應 log |
| Claude Code 執行、Codex 核對紀錄 | • 真實瀏覽器 289 PASS：`artifacts/b1-06/ui-6338fd214467-20261007T030403Z/`；<br>• 不帶兩個新參數的短測試：`artifacts/b1-06/smoke-plain-6338fd214467-20261007T031110Z/`，43 秒，可見性取樣都是 visible，20 秒內 19 次 task 輪詢；<br>• R2／C1 反向：`artifacts/b1-06/reversal-6338fd214467/`，283 PASS／6 FAIL，失敗正是 R2 的 4 項與 C1 的 2 項；<br>• Node 檢查與 R1 反向：`artifacts/b1-06/checks-6338fd214467-20261007T030345Z/`。<br>Codex 沒有獨立重跑真實瀏覽器 |
| 本輪沒有重跑 | 完整容器七步、全部 247 項突變、Unity 建置。既有完整容器與突變結果仍屬 `667e933`，不是 `6338fd2` 的結果 |

**更正本節前一版**：前一版（`405a427`）寫「Codex 另行嘗試的 Docker 測試在轉交時尚未回傳輸出，不算通過」。依完整指示，Codex 本輪已在斷網容器完成上表的定向重跑並全部通過，所以改寫如上。

**保留的限制**：
- 一般 Chrome 不帶新增參數的長時間操作尚未驗證；先前頁面被判為隱藏的根因仍未確認。短測試通過不代表該問題已徹底排除。
- 持續隱藏與恢復的測試，是以覆寫 `document.hidden` 並觸發事件來驗證程式行為，不等於重現瀏覽器原生的遮蔽判斷。
- 目前不要求補跑無參數的完整長流程。這項限制不阻擋 D1 本次限定驗收，也不構成開始其他功能的授權。
- 其他失敗嘗試、未做與未測：見 §15.4–§15.6。查證面板沒有當步版；只用 Chrome；人工操作未做。

**下一步**：D2–D6 不自行開始，待 Aaron 另行派工。

B1-04／B1-06 整體維持 InProgress；不合併、不推送、不標 Done。

## 16 D2 場景動畫深化設計（Claude Code 撰寫；經三輪補正，`9b70c0c`；Aaron 10/7 同意 16.10 五項建議並核准實作，實作紀錄見 §17）

依據：Aaron 10/7「下一步建議進入 D2 場景動畫深化的設計確認」。目標是讓觀眾看懂 Agent 從選商品、提出請購，到 Ask／Deny／到期／撤回的過程，每段動畫都對應可查證的資料；沒有事件支持的動作，明示為展示動畫。

本節只是設計，不修改 Unity、網頁、Go、契約或 Store。它細化 §8 D2 與 §6.1；§8 D2 的原文保留。

### 16.1 原則

1. **事件才觸發**：客人的每一次移動，都由一個已提交的 Store 事件（經 `adapter.scene()` 的 phase）觸發。沒有新事件，客人就停在原地待機。
2. **資料先行**：狀態列、請購卡、Timeline 在資料到達時立即更新，不等 Unity 走完。Unity 只是說明，永遠不能比資料先說出結果。
3. **補間只在兩個已成立的狀態之間**：走路、在貨架前停留，都是從上一個已提交狀態走到下一個已提交狀態的「過場」。只有在終點狀態已提交後才播放，所以動畫不會跑在事件前面（PRD L162）。
4. **展示動畫明示**：沒有獨立事件支持的段落（選商品、走到櫃檯），在 Unity 客人頭上與網頁場景文字都標為「展示動畫」，並說明依據是哪個事件，或者為什麼沒有事件。
5. **不演未發生的事**：不顯示模型思考、核准、付款、取貨或交付。Deny、到期、撤回、取消都只演「離店」，不演任何交易完成。

### 16.2 分段劇本

類型：**事件** 表示終點狀態有 Store 事件；**展示** 表示沒有獨立事件，只是過場。

**Ask 主線**（即時；`TASK_STARTED` 先到，`PURCHASE_PROPOSED` 與 `POLICY_DECIDED` 同一交易到達）

| 段 | 觸發（已提交） | Unity 動作 | 類型 | 標示（Unity ASCII／網頁） | 查證 |
|---|---|---|---|---|---|
| 1 進店 | `TASK_STARTED` #1 | 店外 → 門口 | 事件 | `entered (#1)`／「進店：TASK_STARTED #1」（`#1` 來自 `cause_seq`，16.4） | Timeline #1 |
| 2 選商品 | #2、#3 到達後才播放 | 門口 → 貨架前，停留約 0.6 秒 | 展示 | `[anim] browsing - no event`／「展示動畫：選商品（fake 腳本的 list_products、get_quote；沒有 Store 事件，工具呼叫未公開，見 D6）」 | 無；卡片步驟②標空心 |
| 3 到櫃檯 | 同上 | 貨架 → 櫃檯 | 展示 | `[anim] to counter - quote bound in #2`／「展示動畫：到櫃檯（402 報價以 payment_requirement_hash 綁定在請購 #2）」 | 查證面板「報價」「請購」 |
| 4 提出請購、裁決 | #2 `PURCHASE_PROPOSED`、#3 `POLICY_DECIDED` Ask | 在櫃檯站定，面向櫃檯 | 事件 | `ASK (#3) - waiting for a human`（`#3` 是該請購 `POLICY_DECIDED` 的序號，不是快照最新序號）／「Ask：等待人工核准（M1 無核准能力）」 | 裁決頁、期限（Store 時間） |
| 5a 到期 | #4 `APPROVAL_EXPIRED` | 櫃檯 → 門口 → 店外 | 事件 | `ask expired (#4) - leaving`／「Ask 已到期（後端已記錄）：離店」 | Timeline #4 |
| 5b 撤回 | #4 `APPROVAL_WITHDRAWN`（task 取消時） | 同上 | 事件 | `ask withdrawn (#4) - leaving` | Timeline #4、#5 `TASK_CANCELLED` |

**Deny**：段 1–3 相同。段 4 由 #3 `POLICY_DECIDED` Deny 觸發，在櫃檯停留約 0.8 秒，再走櫃檯 → 門口 → 店外，標示 `DENY (#3, INSUFFICIENT_BUDGET) - leaving`。Deny 原因不被之後的取消或逾時覆蓋（現有 `project()` 規則）。

**其他情況**：

| 情況 | 觸發 | Unity | 備註 |
|---|---|---|---|
| CLI 逾時，Ask 仍未決 | `TASK_TIMED_OUT`（`task_end=timed_out`，16.4） | **留在櫃檯，不重播選商品**，標 `CLI timeout - ask still pending`（不帶序號；確切事件在網頁 Timeline） | 逾時不收尾該筆 Ask（已接受案例 cli-timeout-pending）。之後 `APPROVAL_EXPIRED` 才離店（待決 2） |
| 到期後才取消 | `TASK_CANCELLED`（已在店外） | 不動 | 卡片顯示 task 已取消；場景沒有新的移動 |
| 同一 task 第二筆請購 | 新的 `PURCHASE_PROPOSED` + `POLICY_DECIDED` | 從目前位置 → 貨架（展示）→ 櫃檯 | 前後 `cause_seq` 都有效且不同（新的裁決序號）才重播（16.4 第 2 條）；場景只演最新裁決的那筆（現有 `scene()`） |
| Deny、到期或撤回之後 CLI 逾時 | `TASK_TIMED_OUT` | 不動，保留原標籤（Deny 與原因、到期、撤回） | 不顯示 `ask still pending`；逾時只在網頁呈現（16.4） |
| 其他請購的事件增加，場景請購不變 | 例如舊請購的到期 | 不動，不重播 | phase、`cause_seq`、`task_end` 都沒變，只更新快照序號 |
| Allow | `POLICY_DECIDED` Allow | 櫃檯站定，標 `ALLOW - no payment capability` | 現有 fixture 不會出現（D5）；不演取貨 |
| 非目前 consumer | — | 調暗、留在店外 | 不暗示任何動作 |

### 16.3 路徑與站點

- 店內道具（`ShopBuilder.cs`）：貨架在 y=4.2，x 2.5–6.5；冰櫃在 y=5.3；櫃檯在 x=10.3；推車在 (2.6, 0.4)；水果台在 (5.0, 1.4)。現在客人是直線走，會穿過道具。
- **D2 改為固定中途點，每一段都是水平或垂直線段，避開道具**（座標以 32 px 格為單位，暫定值）：
  - 進店：店外 (-1.3, y_外) → (1.5, y_外) → 門口 (1.5, 1.2)。A 的 y_外 = 3.0，B 的 y_外 = 0.2。
  - 選商品：門口 (1.5, 1.2) → (1.5, 3.2) → 貨架前 (4.5, 3.2)。
  - 到櫃檯：貨架前 (4.5, 3.2) → (7.5, 3.2) → (7.5, 1.6) → 櫃檯 (8.5, 1.6)。
  - 離店：櫃檯 (8.5, 1.6) → (7.5, 1.6) → (7.5, 3.2) → (1.5, 3.2) → (1.5, y_外) → 店外。走上方通道，避開水果台 (5.0, 1.4) 與推車 (2.6, 0.4)。
  - 第二筆請購：櫃檯 → (7.5, 1.6) → (7.5, 3.2) → 貨架前 → 依「到櫃檯」回到櫃檯。
- **避障驗收**：PlayMode 測試沿每一段移動，以角色的占用範圍（腳下的矩形，實作時依 sprite 定出）掃過整段路徑，檢查不與任何道具的範圍（依 `ShopBuilder` 放置的 sprite 邊界）重疊。不只檢查中途點落在道具外。
- 走路用 LimeZu 影格的四個方向（右、上、左、下；目前只用右與左）；待機面向下方，在櫃檯時面向右。
- 不加尋路、不加自由移動或其他 NPC（PRD 止損 L359、執行計畫 L350）。

### 16.4 輸入：最小展示資料擴充（R1 選用方案 B）與標示

**為什麼不能只靠 phase**：同一個 `waiting → waiting` 可能是三件不同的事：
- 同一筆 Ask 遇到 CLI 逾時；
- 同一 task 新的一筆請購得到 Ask；
- 其他請購的事件增加。

現有投影只有 `v, gen, clear, consumer, task_id, seq, phase, verdict, reason, animate`（`web/adapter.js` 的 `projection()`）。`seq` 是快照最新序號，不是造成場景的事件序號；例如到期後又取消 task，最新序號屬於取消，場景仍是 expired。所以只靠共用轉換表無法區分上述情況。

**選用方案 B**：投影新增兩個欄位，都由網頁從既有 Store 事件推導（`adapter.scene()` 已經對齊的同一筆請購），不改 Store、Policy、契約，也不新增事件。

| 欄位 | 型別 | 來源與規則 | 驗證、缺值與未知值 |
|---|---|---|---|
| `cause_seq` | 整數 ≥ 0 | 決定目前 phase 的那個事件的序號：<br>• `entered` → `TASK_STARTED`；<br>• `quoted` → 場景請購的 `PURCHASE_PROPOSED`；<br>• `waiting`／`denied`／`allowed_unpaid` → 場景請購的 `POLICY_DECIDED`；<br>• `expired`／`withdrawn` → 該請購的收尾事件；<br>• 其他（`empty`、`unknown`）→ 0 | 網頁：只送 0 ≤ `cause_seq` ≤ `seq` 的整數，否則送 0。Unity：缺少、非整數、負數或大於 `seq` 都當成 0。0 表示「不知道」：Unity 不顯示事件序號 |
| `task_end` | `""`／`"cancelled"`／`"timed_out"` | 前綴事件中第一個 `TASK_CANCELLED` 或 `TASK_TIMED_OUT`（同 Store `project()` 的終止規則） | 其他值一律當成 `""` |

- **不傳給 Unity**：請購 ID、完整事件、token 或控制能力。Unity 仍只負責展示，不判斷權限或結果。
- **bridge 相容性**：`v` 維持 1，兩個欄位是選填。
  - 舊版 Unity 收到新欄位會忽略（`JsonUtility` 忽略未知欄位）。
  - 新版 Unity 收到沒有這兩個欄位的訊息時，退回方案 A 的行為：同 phase 不重播、不顯示事件序號、不顯示逾時標籤。
  - 這仍是 bridge 擴充，需 review；本設計不代表已核准。
- **兩個序號分開**：
  - Unity 標籤帶的 `#n` 一律是 `cause_seq`，也就是特定事件的序號。
  - 快照最新序號 `seq` 只用於新舊比較與 `rendered` 回報，不顯示成事件序號。
  - `cause_seq` 為 0 時，標籤不帶序號。

**展示端的兩份狀態**（第二輪補正，16.13；只是 Unity 展示端的區分，不新增 Store 狀態，也不加第三個 bridge 欄位）：
- **目標 T**：最後接受的展示目標，也就是應前往、應呈現的內容：phase、`cause_seq`、依下表決定的標籤、快照序號 `seq`。
- **已呈現 P**：最後實際呈現完成、已回報 `rendered` 的內容。

新訊息一律與**目標 T** 比較，不與已呈現 P 比較。這樣不會把已完成的舊狀態當成仍在進行的目標，造成重播。

**標籤**（由 phase 與 `task_end` 決定）：
- 只有 phase 為 `waiting`，且 `task_end=timed_out`，才標 `CLI timeout - ask still pending`。
- `denied`、`expired`、`withdrawn`、`allowed_unpaid` 的標籤依原裁決或收尾，**不被 task 逾時或取消覆蓋**，也不改回 pending。
  - 逾時或取消的資訊只在網頁（卡片的 task 狀態、Timeline）分開呈現；Unity 不在這些狀態上宣稱 Ask 仍未決。

**`cause_seq` 是否有效**：大於 0 且不大於 `seq` 才有效，否則視為未知（0）。

**轉換規則**（Unity 與網頁共用同一張表，以測試核對一致；依序比對，第一條符合的生效）：

| # | 新訊息 M 與目標 T 相比 | Unity 行為 |
|---|---|---|
| 1 | M 的 phase 與 T 不同 | 新目標：依 16.2 的段落與 16.3 的路徑前往（16.5 的追上規則適用） |
| 2 | phase 相同且為櫃檯類（`waiting`／`denied`／`allowed_unpaid`），T 與 M 的 `cause_seq` **都有效且不同** | 新的一筆請購：新目標，重播選商品（展示）→ 到櫃檯 |
| 3 | phase 相同，只有標籤改變（例如 `waiting` 加上 `timed_out`） | 不移動，換標籤；目標的位置不變 |
| 4 | phase 與標籤相同，`cause_seq` 相同，**或任一方是未知值**（`3 → 0`、`0 → 3`） | 展示目標的位置不變：不重播、不重新開始過場、不重設期限。但必須以 M 更新 T 的展示中繼資料：正規化後的 `cause_seq`（未知為 0）、依它重算的標籤、最新的 `seq`。標籤中的事件序號反映最新的有效值；`cause_seq` 未知時，標籤不顯示事件序號，也不殘留舊序號 |

未知 `cause_seq` 與有效值之間的切換，只會落在第 4 條（或 phase 改變時的第 1 條），不會觸發第 2 條的「新請購」。

第 4 條更新後，T 的 `cause_seq` 是 M 正規化後的值。之後的比較用這個新值：
- 例如 `0 → 3` 之後再來 `3 → 6`，前後都有效且不同，符合第 2 條，會重播；
- 例如 `3 → 0` 之後再來 `0 → 3`，仍是未知與有效之間的切換，只走第 4 條，不重播。

**回報 `rendered`**：
- 只在 P 追上 T，也就是實際到達終點、呈現完成時，對 T 目前的 `seq`（最新接受的序號）回報一次。
- **過場中**收到第 3、4 條的更新：保留正在進行的過場，不重新開始，不提前回報；也**不重設**這段過場的 2.5 秒期限。到達後只回報最新序號。
- **已到達**（P 與 T 的位置和標籤一致）時收到第 3、4 條的更新：不移動，直接對新序號回報（第 3 條換完標籤後回報）。
- **真正更換目標**（第 1、2 條）：依 16.5 加速、轉向或瞬移。被取代的過場，其完成回呼作廢（以每次過場的識別碼核對），不回報。
- **清除或更換世代**：所有過場與回呼作廢，不得恢復畫面或回報完成（沿用現有世代規則）。

> 已取代：`6c56391` 版本的轉換表以「上一個已呈現狀態」比較，逾時標籤沒有 `waiting` 條件，`cause_seq` 不同即視為新請購，相同內容會直接回報 `rendered`。這些由上方規則取代（16.13）。

**標示**
- **Unity 頭上標籤**：目前專案的字型與實作只支援 ASCII（`docs/demo-ui.md` §8）。不是 Unity 本身無法呈現中文。D2 沿用現況，使用英文短標籤。展示段落一律以 `[anim]` 開頭。
- **網頁**：
  - 在送出投影到收到 `rendered` 之間，場景文字顯示中文過場說明，例如「Unity 補間中：展示動畫：選商品 → 到櫃檯（沒有獨立事件）；裁決已在 #3 記錄」。
  - 卡片與狀態列不受影響。
  - 查證面板「場景對應」頁新增一列「這次過場」，列出段落與依據事件（`cause_seq`），來源標「網頁依轉換表推導」。

### 16.5 首次載入、資料先行與追上（細化 §6.1；取代 §6.1 的「落後超過一個 phase」）

**三種情境**（R2）：

| 情境 | 畫面 |
|---|---|
| 1 首次取得的快照已到 #3 Ask | 不播放過場（`animate=false`，現有 `!first && fresh.length > 0` 規則），客人直接站在櫃檯，標 `ASK (#3)`。網頁場景文字註明「首次載入為保存的快照，未觀測進店過程」。不冒充剛觀測到完整進店 |
| 2 已呈現 #1，之後收到 #3 | 播放「選商品 → 到櫃檯」的過場，標為展示動畫。卡片與狀態列在資料到達時立即顯示已記錄的 Ask，不等動畫完成 |
| 3 想完整觀看流程 | 使用明確標示的歷史回放（16.6）：逐事件重演，標「回放」，不是即時觀測。即時模式不保證能看到每一段，因為那取決於第一次讀取的時間，本設計不以此為前提 |

**資料先行**：狀態列、卡片、Timeline 在資料到達時立即更新，與 Unity 的過場無關。

**步調與上限**：
- 走速 4.5 單位／秒；貨架停留 0.6 秒；Deny 在櫃檯停留 0.8 秒。
- 單次過場最長 2.5 秒，路線較長時等比加速。

**追上規則**：Unity 只用自己已知的資料判斷：
- 新訊息的 `seq`、`phase`、`cause_seq`、`task_end`、`animate`；
- 目前位置、這次過場剩餘的路線、這次過場開始的時間。

phase 不當成數字比較大小。走到一半收到同世代、較新的訊息時：
1. **`animate=false`**（首次載入、切換畫面、回放結束恢復）：直接瞬移到新終點。
2. **新終點在剩餘路線上**，例如正走向櫃檯，而新訊息仍是櫃檯：保留路線，取消剩餘停留，以 2 倍速走完。
3. **新終點不在剩餘路線上**，例如還沒到櫃檯就已到期：從目前位置依 16.3 的通道重新規劃路線，以正常速度前往。
4. **時間上限**：任何過場從收到觸發它的訊息起超過 2.5 秒仍未到達時，直接瞬移到終點。
   - 期限只在真正更換目標（16.4 第 1、2 條）時重新起算；第 3、4 條的更新不延後期限。
5. **回報**：依 16.4「回報 `rendered`」：
   - 被取代的過場不回報；
   - 過場中不因收到較新序號而提前回報；
   - 到達後只對最新接受的序號回報一次。

這裡的「新訊息」是指與目標 T 相比，展示目標真正改變的訊息（16.4 第 1、2 條）。第 3、4 條不觸發 1–3 的重新規劃。

**其他沿用的規則**：同一事件再送一次不重播（現有 `AnimatedSeq` 與 `fresh()`）。

### 16.6 回放

- 回放仍是一個事件一步，現有規則全部沿用：回放當步、完整紀錄標示、不寫 Store。
- 回放時 `quoted`（只有 #2）會出現：
  - #1 → #2：門口 → 貨架前 → 走道（展示：選商品，停在貨架附近）；
  - #2 → #3：到櫃檯；
  - 之後依事件離店或停留。
- 每步過場仍以 2.5 秒為上限，加上 400 ms 間隔，在 `renderedBy` 的 6 秒上限內。

### 16.7 失敗與不確定

- **讀取失敗、更新中、Store 不可用**：Unity 停在最後一致的狀態，不做任何新的移動。狀態列與卡片顯示未確認或離線（現有規則）。
- **網頁切換、清除或離開**：Unity 立即清除（現有 `clear`），舊世代的過場不再回報。
- **Unity 未載入**：網頁照常運作，場景文字只有說明，沒有「已呈現」。

### 16.8 不做

- 思考泡泡或模型推論字幕；核准、付款、取貨、交付的動畫；多商店或自由移動的 NPC。
- 角色點選（D3）、Allow 情境的新商品（D5）、工具呼叫觀測（D6）。
- 新事件類型、契約或 Store 變更。

### 16.9 實作切片與驗收（提案）

- **D2a Unity 路徑與標籤**：`Projection.cs`（選填的 `cause_seq`、`task_end` 與驗證）、`ShopView.cs`（中途點、四向影格、轉換表、步調、追上、ASCII 標籤）、必要時調整 `ShopBuilder.cs`；EditMode／PlayMode 測試；重建 WebGL 並記錄新 hash。
- **D2b 網頁說明與投影欄位**：`adapter.js` 的 `projection()` 推導 `cause_seq`、`task_end`（回放時由 `stepView` 推導）、`transition()`；`app.js` 的場景文字與「這次過場」列；Node 測試。
- 兩片可以同一個實作 commit 交付。

**驗收**（瀏覽器斷言讀可見文字）：

測試控制首次快照的方法，不靠偶然的時序：
- **要 #1 先到**：測試目錄加一個情境，在 `TASK_STARTED` 之後先 `sleep`（fake CLI 已有的步驟，例如 10 秒），再取得報價與請購。測試用 `hostPost` 在伺服器端開始該場，確認 Store 只有 #1 後才在頁面開啟該 task，並斷言頁面顯示快照序號 1。這個條件不成立就判失敗，不略過。
- **要 #3 先到**：先確認 Store 已有 #3，再開啟該 task。

驗收項目：
1. **首次快照已到 #3**：客人直接在櫃檯，沒有過場，場景文字有「首次載入為保存的快照，未觀測進店過程」。
2. **已呈現 #1 再收到 #3**：依序看到「選商品」「到櫃檯」的展示動畫標示，再到櫃檯的 `ASK (#3)`。卡片在 Unity 到達櫃檯前，就已顯示 Ask（資料先行）。
3. **同 phase 的三種情況**：
   - 同一筆 Ask 遇到 CLI 逾時：不移動，標籤換成逾時說明；
   - 新的一筆請購：重播選商品；
   - 其他請購的事件增加：不移動、不重播。
   - Node 對轉換表測這三種；瀏覽器至少測新請購與「其他請購的事件」兩種。CLI 逾時另以種子資料或測試替身覆蓋。
4. **序號語意**：到期後又取消時，Unity 標籤是到期事件的序號，不是取消的序號。缺少 `cause_seq` 時，標籤不帶序號。
5. **欄位驗證與相容**：
   - Unity EditMode：缺欄位、非法值、大於 `seq` 的 `cause_seq`、未知的 `task_end`，都退回方案 A 行為；
   - 網頁 Node：送出的值在範圍內。
6. **Deny、到期、撤回**：都離店，標示帶對應事件序號；沒有付款、取貨或成功字樣。
7. **追上規則**：16.5 的 1–5 各一個 PlayMode 案例；單次過場不超過 2.5 秒；只回報最新序號。
8. **回放**：#1–#n 每步的位置與標籤符合 16.6；既有回放順序檢查（`docs/demo-operation-design.md` 16.1）與 D1 回放當步檢查全過。
9. **PlayMode 路徑**：角色占用範圍沿整段路徑不與道具重疊；四向影格；標籤只用 ASCII；展示段落以 `[anim]` 開頭。
10. **轉換表在 C# 與 JS 一致**（同步測試）。
11. **轉換表的適用條件**（第二輪補正；Node 測轉換表，Unity EditMode 測同一組輸入）：
    1. 同一筆等待中的 Ask 遇到 CLI 逾時：不移動，標 `CLI timeout - ask still pending`；
    2. Deny 後 CLI 逾時：保留 Deny 與原因，不出現 `ask still pending`；
    3. Ask 已到期後 CLI 逾時：保留到期，不改回 pending；已撤回時依相同原則；
    4. 同 phase 下 `cause_seq` 為 `3 → 0` 與 `0 → 3`：不重播選商品；
    5. 正向對照：兩個有效的裁決序號切換（例如 `3 → 6`，櫃檯類 phase），重播選商品。
12. **目標與已呈現分開**（第二輪補正；PlayMode，必要時加瀏覽器）：
    1. 走向櫃檯途中，收到目標相同、快照序號較新的訊息：不重播、不提前回報；到達後只回報最新序號；
    2. 過場途中連續收到無關的更新：原過場的期限不被延後，依 2.5 秒上限完成或跳到終點；
    3. 已到達終點後，收到目標相同的新序號：不移動，直接回報最新序號；
    4. 途中真正更換目標，或清除、更換世代：舊過場的完成回呼不回報。

    本輪只寫測試計畫，不執行。
13. **連續輸入**（第三輪補正，16.14；Node 測轉換表的序列，Unity EditMode／PlayMode 測同一組序列）：
    1. `cause_seq` 依序為 `0 → 3 → 6`（同一櫃檯類 phase）：
       - `0 → 3` 是第一次補齊，不重播，標籤改為 `#3`；
       - 接著 `3 → 6` 前後都有效且不同，重播選商品，到達後標籤為 `#6`。
    2. `cause_seq` 依序為 `3 → 0 → 3`：
       - `3 → 0`：不重播，標籤不再顯示 `#3`，不帶序號；
       - `0 → 3`：重新補齊，不重播，標籤回到 `#3`。
       - 任何時刻標籤都不殘留錯誤或過期的序號。
    3. 以上更新若發生在過場中：位置與過場照常進行，不提前回報 `rendered`，不延長原過場的 2.5 秒期限；標籤與 T 的 `seq` 仍立即更新為最新值，到達後只對最新 `seq` 回報。

**驗證層級**：
- Unity EditMode／PlayMode、WebGL 重建；
- Node（轉換表與文字）；
- 真實瀏覽器全套。
- 只改 Unity 與網頁時，完整容器（不含 Unity 與瀏覽器）由 review 決定是否需要。

### 16.10 待 Aaron 決定（Codex 10/7 的建議附於各項；不是已確認的決定，也不是實作授權）

1. **在貨架前停留的「選商品」展示動畫是否接受**：它標為展示動畫，並說明沒有事件。不接受時，客人直接從門口走到櫃檯，只標「到櫃檯」。建議接受，因為觀眾較容易看懂。Codex 建議：接受短暫停留，但持續標示「展示動畫」，不宣稱有獨立 Store 事件。
2. **CLI 逾時時客人的位置**：
   - a. 留在櫃檯直到 Ask 到期（建議，因為 Ask 尚未收尾）；
   - b. 立刻離店，另外標示「Ask 仍待到期」。
   - Codex 建議 a。標示依 16.4 可取得的資訊：`task_end=timed_out` 加上 phase 仍為 waiting。
3. **標籤語言**：目前專案的字型與實作只支援 ASCII，所以 Unity 內是英文短標籤，中文說明放在網頁。Unity 本身可以呈現中文，但需要加入中文字型；這項工作不在 D2 範圍。Codex 建議 D2 沿用英文短標籤加網頁中文，不在這輪加入中文字型。
4. **順序**：D2 與 D5（Allow 低價商品）誰先。D2 不依賴 D5；D5 完成後，Allow 只需在轉換表加一列。Codex 建議 D2 先於 D5；D5 仍需另行派工。
5. **bridge 擴充（方案 B）**：是否接受 16.4 的兩個選填欄位（`cause_seq`、`task_end`）。不接受時改採方案 A，同時縮減 16.2、16.9 的承諾（16.12 列出縮減內容）。

### 16.11 與 PRD、任務清單的對應

- PRD L162「動畫不能跑在事件前面」：由 16.1 第 2、3 條保證。展示段落只在終點事件已提交後播放。
- 任務清單 B1-06 卡 L198「QuoteReceived → 報價」：契約沒有這個事件，報價一段標為展示，並指向 #2 的 `payment_requirement_hash`（§11 已列此差異）。
- 任務清單 L200「事件重送不重複演動畫」：沿用現有機制（16.5）。
- 本設計不改 FR、契約或驗收門檻，也不授權實作。

### 16.12 修訂紀錄：依 Codex 對 `e61d801` 的 review（2026-10-07）

依據：vault〈Claude-Code-B1-06-D2設計修訂指示-e61d801-20261007〉。Codex 接受設計方向，D2 尚未通過設計 review，也尚未核准實作。本輪只改設計。

| 項目 | 原寫法（`e61d801`） | 現行（本次修訂） |
|---|---|---|
| R1 輸入 | 16.4：bridge 不改，Unity 只靠「上一個 phase → 新 phase」決定段落 | 被取代。選用方案 B：投影新增選填的 `cause_seq`、`task_end`，由網頁從既有事件推導；同 phase 的三種情況各有規則；標籤序號一律是 `cause_seq`；缺值時退回方案 A |
| R1 序號 | 16.2 標籤帶 `#n`，沒有說明來源 | `#n` 是 `cause_seq`，不是快照最新序號；CLI 逾時標籤不帶序號 |
| R2 首次載入 | 16.5 首次快照不播動畫，但 16.9 要求即時看到完整進店 | 16.5 分成三種情境；完整流程以歷史回放觀看；16.9 的即時驗收改為可控制首次快照（#1 或 #3） |
| R2 追上 | 6.1／16.5「落後超過一個 phase 就瞬移」 | 被取代。改為 16.5 的五條規則：`animate=false`、路線上、不在路線上、2.5 秒上限、只回報最新 |
| 路徑 | 16.3 宣稱水平與垂直線段，但部分是斜線；只驗中途點 | 全部改為水平或垂直；驗收以角色占用範圍掃過整段路徑 |
| 用語 | 「Unity 只能用 ASCII」 | 「目前專案字型／實作只支援 ASCII」 |
| 選項 | 四項待決 | 附上 Codex 建議（不是已確認的決定）；新增第 5 項：是否接受 bridge 擴充 |

> 第二輪補正見 16.13。本表的 16.4 轉換規則已再修訂。

**R1 選用方案 B 的理由**：方案 A 只能做到「同 phase 不動」。這樣第二筆請購無法重演，CLI 逾時也無法在 Unity 標示；Unity 標籤無法指出是哪個事件造成目前場景，與「每段動畫對應可查證的資料」的目標差距較大。

方案 B 只加兩個可從既有事件推導的欄位，選填、向後相容，不碰 Store、Policy、契約或事件。

**若 review 不接受方案 B（改採方案 A）時要縮減的承諾**：
- 16.2「同一 task 第二筆請購」改為「不重播，只更新標籤」；
- 「CLI 逾時」改為「Unity 不變，逾時說明只在網頁」；
- Unity 標籤不帶事件序號；
- 16.9 第 3、4 項改為只驗「同 phase 不動」與「標籤不冒稱事件序號」。

本輪沒有修改產品程式，也沒有執行測試、容器或 Unity 建置。

### 16.13 第二輪補正紀錄：依 Codex 對 `6c56391` 的限定 review（2026-10-07）

依據：vault〈Claude-Code-B1-06-D2第二輪設計補正指示-6c56391-20261007〉。

Codex 接受了以下部分，本輪不重做：
- 方案 B 以 `cause_seq`、`task_end` 作為最小展示資訊的方向；
- `cause_seq` 與快照 `seq` 分開；
- 首次載入的三種情境；
- 水平／垂直路徑與占用範圍避障；
- 英文 Unity 標籤加中文網頁說明，以及字型用語的更正。

D2 尚未通過完整設計 review，也尚未核准實作。

| 補正 | 原寫法（`6c56391`） | 現行 | 位置 |
|---|---|---|---|
| 一、逾時標籤條件 | 「phase 相同、`cause_seq` 相同、`task_end` 變為 `timed_out`」就換成 `ask still pending`，沒有限定 `waiting`；Deny 後逾時也會命中 | 只有 phase 為 `waiting` 才標 `ask still pending`；Deny、到期、撤回的標籤不被逾時或取消覆蓋，逾時資訊只在網頁分開呈現 | 16.4「標籤」、16.2 新增一列 |
| 一、未知 `cause_seq` | `cause_seq` 不同就視為新請購，與缺值退回方案 A 衝突（`3 → 0`、`0 → 3`） | 只有前後都有效且不同，才視為新請購（第 2 條）；未知與有效之間的切換一律落在第 4 條（或 phase 改變時的第 1 條） | 16.4 轉換規則 |
| 二、目標與已呈現 | 以「上一個已呈現狀態」比較；相同內容直接回報 `rendered` | 分成目標 T 與已呈現 P；新訊息與 T 比較；過場中不提前回報、不重設期限；已到達才直接回報；被取代或世代改變的回呼作廢 | 16.4「展示端的兩份狀態」「回報 `rendered`」、16.5 第 4、5 條 |
| 測試計畫 | 驗收 1–10 | 新增第 11 項（適用條件 5 案）與第 12 項（目標與已呈現 4 案） | 16.9 |

- 不增加第三個欄位，不改 Store、Policy、契約，也不重開 D1 驗收。
- 16.10 的五個選項維持待 Aaron 確認，附 Codex 建議，不視為已核准。
- 本輪沒有修改產品程式，也沒有執行測試、容器或 Unity 建置。

### 16.14 第三輪補正：第 4 條須更新展示中繼資料（2026-10-07）

依據：Codex 的補正要求，由 Aaron 轉交。只改這條規則與測試計畫，其他已接受的內容不重做。

| 項目 | 原寫法（`c884c32`） | 現行 |
|---|---|---|
| 16.4 轉換規則第 4 條 | 「展示目標不變：不重播、不移動；只把 T 的 `seq` 更新為 M 的 `seq`」，沒有更新 `cause_seq` 與標籤 | 位置與目前過場不變，不重新開始、不重設期限；但以 M 更新 T 的正規化 `cause_seq`、重算的標籤與最新 `seq`；事件序號反映最新的有效值，未知時不顯示 |
| 後續比較的基準 | 未說明 | 第 4 條更新後，T 的 `cause_seq` 用新值比較（16.4 的兩個例子） |
| 測試計畫 | 第 11、12 項 | 新增第 13 項：`0 → 3 → 6`、`3 → 0 → 3`，以及過場中發生時不提前回報、不延長期限 |

本輪沒有修改產品程式，也沒有執行測試、容器或 Unity 建置。D2 仍待 Codex 限定 review，尚未核准實作。

## 17 D2 實作紀錄（Claude Code 執行，待 Codex 獨立 review；程式與測試 `bc79677`）

依據：Aaron 10/7 同意 16.10 的五項建議（貨架短暫停留並標示展示動畫；Ask 未決時 CLI 逾時仍留在櫃檯；D2 先於 D5；Unity 英文短標籤搭配網頁中文；接受 `cause_seq`、`task_end` 兩個選填欄位），並核准依 `9b70c0c` 的已審查設計實作 D2a／D2b。本輪不合併、不推送、不標 Done；B1-04／B1-06 維持 InProgress。D3–D6 未開始。

Store、Policy、契約、API、事件與資料表都沒有改。Go 只改了一個測試檔（見 17.1 測試資料）。

### 17.1 修改

**bridge（D2a／D2b 共同）**
- 投影新增兩個選填欄位，`v` 仍為 1：
  - `cause_seq`：網頁由 `scene()` 推導（`entered` → `TASK_STARTED`；`quoted` → 場景請購的 `PURCHASE_PROPOSED`；`waiting`／`denied`／`allowed_unpaid` → 其 `POLICY_DECIDED`；`expired`／`withdrawn` → 該請購的收尾事件；其他 0），只送 0 < c ≤ `seq`，否則 0。
  - `task_end`：第一個 `TASK_CANCELLED` 或 `TASK_TIMED_OUT`。
- Unity 的 `ProjectionState.Parse`：`JsonUtility` 會把 `"3"`、`3.5` 讀成 3，所以兩個欄位只在原始 JSON 中**恰好出現一次**、且分別是整數字面值與 `"cancelled"`／`"timed_out"` 時才採用；其他一律當成未知（0／""），訊息仍照常顯示（方案 A 行為）。這是實作時發現的，設計沒有預先寫到。

**轉換表**（16.4 第 1–4 條）
- C# `SceneRules`（`Projection.cs`）與 JS `transition()`（`web/adapter.js`）各一份，以共用測試向量 `testdata/demo/scene-transitions.json`（20 個案例、39 步）核對：EditMode 與 Node 都讀同一檔，比對規則號、是否播「選商品」與標籤。
- `ProjectionState` 持有目標 T（phase、正規化的 `cause_seq`、`task_end`、最新 `seq`、verdict、reason）；新訊息一律與 T 比較。
- `browse`（是否播放 `[anim] browsing`）：第 2 條一律播；第 1 條在進入櫃檯類 phase 時播，除非上一個目標是 `quoted`（已選過商品），或上一個目標也是櫃檯類而 `cause_seq` 不是「都有效且不同」；進入 `quoted` 時播。

**Unity 場景**（`ShopView.cs`、`ShopBridge.cs`、`ShopBuilder.cs`）
- 路徑：16.3 的八個點（店外、店外列、門口、通道口、貨架前、轉角、櫃檯前、櫃檯）與其間的水平／垂直線段；兩點之間取最短的通道（八個點的 Floyd–Warshall，不是自由尋路）。走到一半換目標時，從目前所在的線段兩端擇近者接回通道。
- **道具調整**：以道具圖片的**實際不透明像素**計算，16.3 的上方通道（y = 3.2）會穿過水果台的圖（不透明範圍 y 1.77–3.84），垂直通道（x = 1.5）會碰到推車左緣（1.91，角色腳寬 ±0.44）。設計只看了水果台的放置點。依 16.9「必要時調整 `ShopBuilder.cs`」，水果台由 (5.0, 1.4) 移到 (5.0, 0.4)，推車由 (2.6, 0.4) 移到 (2.8, 0.4)；路徑與站點不變。
- 角色占用範圍：腳下 28 × 8 px（LimeZu 每一格影格最下 8 列的最寬範圍），即 ±0.44 × 0.25 單位。
- 步調：4.5 單位／秒；貨架停 0.6 秒；Deny 在櫃檯停 0.8 秒後離店；單次過場超過 2.5 秒時速度與停留等比例加快；另有 2.5 秒期限，到期直接移到終點。
- 追上（16.5）：
  1. `animate=false`：直接到終點；
  2. 新路線就是剩餘路線的開頭：沿原路以 2 倍速、取消剩餘停留；
  3. 其他：從目前位置依通道重新規劃，正常步調；
  4. 期限只在第 1、2 條時重新起算；第 3、4 條（`Relabel`）不碰期限；
  5. 被取代的過場、清除或換世代時的完成回呼都不呼叫。
- 目標與已呈現：`Go`（第 1、2 條）開始新過場；`Relabel`（第 3、4 條）只更新 T 與標籤，已到達時才立即回報。`rendered` 在到達時對**當下 T 的最新 `seq`** 回報一次（`ShopBridge.Rendered`）。
- 新世代即使沒有先收到 `clear`，畫面也會先清空（狀態原本就會清；畫面原本可能讓另一位客人繼續走）。
- 四向影格：走路用 LimeZu 的右、上、左、下；站立面向下，在櫃檯面向右。
- 標籤（ASCII，頭上兩行：名字＋標籤）：`entered (#1)`、`request proposed (#2)`、`[anim] browsing - no event`、`[anim] to counter - quote bound in request`、`ASK (#3) - waiting for a human`、`CLI timeout - ask still pending`、`DENY (#3, INSUFFICIENT_BUDGET) - leaving`、`ask expired (#4) - leaving`、`ask withdrawn (#4) - leaving`、`ALLOW - no payment capability`。reason 只顯示固定代碼（A–Z、0–9、_），其他不顯示。
  - **與設計文字的差異（請 review）**：16.2 寫的是 `[anim] to counter - quote bound in #2`，但 16.4 規定 Unity 標籤的 `#n` 一律是 `cause_seq`，而櫃檯段的 `cause_seq` 是裁決序號（#3）。Store 雖然在同一交易連續寫入 `PURCHASE_PROPOSED`、`POLICY_DECIDED`（`internal/store/ops.go` L538–541），但讓 Unity 自行用 `cause_seq − 1` 推算會是 bridge 以外的推論，所以 Unity 標籤不帶序號，#2 由網頁文字與查證面板提供。
  - 標籤錨點依客人在左半或右半房間改為向右或向左延伸，避免在店外時超出畫面。
- 測試用的 `Pace`（不序列化，預設 1）：只為了讓 PlayMode 測試能觸發 2.5 秒期限；正常步調下過場本來就會在期限內到達。

**網頁**（`web/adapter.js`、`web/app.js`、`web/render.js`）
- `scene()` 多回傳 `cause`、`end`；`projection()` 送出兩個欄位；回放每一步改用 `projection({gen, snapshot: stepView(...)})`，與即時共用同一個推導。
- `transition()` 與 `shopLabel()`：與 Unity 同一張表，另產生中文說明，例如「展示動畫：選商品 → 到櫃檯（沒有獨立事件）；裁決已在 #3 記錄」「新的一筆請購：…」「不移動，只換標籤：CLI 逾時，同一筆 Ask 仍未決」「不移動、不重播：場景的請購未變（快照 #n）」。
- `app.js` 在每個世代記住送給 Unity 的目標與最近一次真正的移動（第 1、2 條或首次載入）：
  - 首次載入：場景文字加「首次載入為保存的快照，未觀測進店過程」；
  - 移動中（送出到收到該序號的 `rendered`）：「Unity 補間中：」＋說明；Unity 未就緒時改為「Unity 未就緒；過場說明：」；
  - `#scene-text` 的 `data-motion`（first／moving／shown／waiting）供瀏覽器測試讀取。
- 查證面板「場景對應」頁新增「這次過場」列：說明＋規則號，來源「網頁依轉換表推導」。
- `docs/demo-ui.md` §3 補上兩個選填欄位與轉換規則的說明。

**測試資料**
- `testdata/demo/scene-transitions.json`：共用向量。
- `testdata/cli/ask-slow.json`、`testdata/demo/scenarios-d2.json`：進店後停 8 秒再請購，只供 D2 瀏覽器檢查（不改 `scenarios-acceptance.json`，它的 3 個情境數由 Go 測試與 3.2 檢查固定）。
- `internal/store/display_history_test.go` 的 `TestSeedDisplayDB` 加 `APA_SEED_ORDER=ask_ask_then_expire`：寫入「請購 1 Ask → 請購 2 Ask → 請購 1 到期」，與既有歷史 `ask_ask_then_expire` 同順序。只是測試檔，Store 程式未改；預設行為不變。
- 修正 `d7070af`（見 17.3）：2.5 秒期限改用真實時間（`Time.realtimeSinceStartup`），走路用 `Time.unscaledDeltaTime`。

### 17.2 測試與證據（Claude Code 執行，待 Codex 獨立 review；全部在 `d7070af`，另列 `bc79677` 的失敗紀錄）

| 項目 | 命令 | 結果 | 證據 |
|---|---|---|---|
| Node（adapter 與 hostcreds） | `node --test web/test/*.test.mjs`（於 `d7070af` 的 `git archive` 匯出目錄執行） | 47／47 | `artifacts/b1-06/d2-checks-d7070afdc0bd/node.txt` |
| Unity EditMode | `sh scripts/unity/unity.sh test OUT` | 10／10（新增 4：共用向量 20 案例 39 步、缺欄位與非法值、錯誤 JSON 型別、ASCII 標籤） | 同目錄 `unity-test/` |
| Unity PlayMode | 同上 | 12／12（新增 9，見下） | 同上 |
| WebGL 重建 | `sh scripts/unity/unity.sh webgl OUT` | exit 0，314 秒 | `webgl-sha256.txt` |
| 真實瀏覽器全套（預設兩個 Chrome 參數） | `node scripts/verify/browser-ui.mjs` | **309 PASS／1 FAIL**，837 秒 | `artifacts/b1-06/ui-d7070afdc0bd-20261007T054900Z/` |
| 完整容器（七步、突變） | `container-verify.sh d7070af`（見 17.4） | exit 0，七步全過，247／247 突變 | `artifacts/verify-container/runs/d7070afdc0bd-20261007T063755Z/` |

WebGL（`d7070af`，未提交，`Build/WebGL` 由 git 忽略）的 SHA-256：
- `WebGL.data` `d8f79c299814dcad149d6d608f15c9f9b2eb053306a9498250656ca3f5f206dc`
- `WebGL.framework.js` `3c7d6ab5547a85de82e08400bfd63520ba9c2f50c098eff05074a6a010e65666`
- `WebGL.loader.js` `9284bfdd99f31d36511799a1584b662a295238e14a9395a2a0494bdc1c7e6a42`
- `WebGL.wasm` `9a18e8cc36253ed20f2f2b3a11a0f8b330cdc42a0229a00124057d11ef60a757`

**PlayMode 新增案例**（對應 16.9）
- 第 9 項：`RoutesKeepClearOfPropsInFourDirectionsWithAsciiLabels`：兩位客人走過進店、選商品、到櫃檯、到期、第二筆請購、Deny 離店、撤回等 15 則訊息；每次過場開始時，以腳下範圍沿**整段路線**掃過（解析計算），走路中每一幀再檢查一次，對 11 個道具的不透明像素範圍都不重疊；四個方向都出現；所有標籤是 ASCII，展示段落以 `[anim] ` 開頭。
- 第 7 項（16.5 第 1–5 條）：`NotAnimatedJumpsAndTheReplacedMoveNeverReports`（1）、`AnEndAheadOnTheWayIsReachedAtTwiceTheSpeed`（2，並確認期限重新起算）、`AnEndOffTheWayTakesTheCorridorAgain`（3，新路線也掃過避障）、`TheLimitIsKeptWhateverUpdatesComeOnTheWay`（4，以 `Pace = 0.2` 讓過場來不及，於 2.5 ± 0.15 秒跳到終點）、`TheLongestMoveArrivesWithinTheLimit`（最長路線 ≤ 2.5 秒）、`TheLimitHoldsOnSlowFrames`（每幀 200 ms、`deltaTime` 上限 50 ms，仍在真實 2.95 秒內到達）。
- 第 12、13 項：`TheSameTargetOnTheWayAndAfterArriving`（途中 `3 → 0 → 3` 只更新 T、不重播、不提前回報，到達後只回報 #5；到達後 #6、#7 立即回報、不移動；CLI 逾時留在櫃檯換標籤）、`ZeroThreeSixOnTheWay`（途中 `0 → 3` 不重播，`3 → 6` 重播，只回報 #6）。
- 既有兩案例保留：Ask 走到櫃檯、重送不重播、Deny 在店外；舊世代的晚到回呼與訊息丟棄。

**瀏覽器 D2 段**（`browser-ui.mjs` 的「B1-06 D2」，頁面內記錄器保存每次場景文字變化與 Unity 回報的時間，以回報時間區分「沒有移動」與「走了一段」）
- 第 1 項：種子資料庫（Store 已有 #1–#6）開啟 task：Unity 在首次載入文字出現後 800 ms 內回報 #6 waiting（沒有過場）；場景文字與查證面板「這次過場」都寫「首次載入為保存的快照，未觀測進店過程」。
- 第 3、8 項（回放 `ask_ask_then_expire`）：#2「展示動畫：選商品（沒有獨立事件）；請購已在 #2 記錄」；#3「展示動畫：到櫃檯…裁決已在 #3 記錄」；#4、#6（其他請購的事件）Unity 立即回報、沒有「補間中」；#5（新的一筆請購）顯示「Unity 補間中：新的一筆請購：展示動畫：選商品 → 到櫃檯（沒有獨立事件）；裁決已在 #5 記錄」，查證面板同文並標（規則 2）、來源「網頁依轉換表推導」，Unity 走完才回報。實測（ms，步驟文字到 Unity 回報）：`{"1":2031,"2":1596,"3":1596,"4":-1,"5":2194,"6":-1}`。
- 第 2 項（即時，`ask-slow`）：伺服器端開始場次，確認 Store 只有 #1 才在頁面開啟；頁面首個快照是 #1（不成立即判失敗，不略過）；#3 到達時卡片已顯示 Ask，而 Unity 仍回報 #1、場景文字為「Unity 補間中：展示動畫：選商品 → 到櫃檯（沒有獨立事件）；裁決已在 #3 記錄」；之後 Unity 回報 #3。實測走路 2865 ms。
- 第 7 項的上限在瀏覽器中為 2.5 秒＋700 ms（一個慢幀與回報路徑）。
- 截圖 `d2-live-browsing.png`、`d2-live-to-counter.png` 本意是過場中的畫面，但這台機器負載很高，CDP 呼叫延遲，兩張實際都在到達之後才拍到；不能當作過場中的畫面證據。`d2-live-at-counter.png` 為到達後。

### 17.3 失敗與修正：`bc79677` 的瀏覽器全套（保留紀錄）

`bc79677`（第一個實作 commit）：Unity EditMode 10／10、PlayMode 11／11、WebGL 建置成功；**瀏覽器 304 PASS／6 FAIL**（`artifacts/b1-06/ui-bc7967775a0b-20261007T052440Z/`）：
- D2 四項：回放各步實測 `{"1":2964,"2":4224,"3":3970,"4":-1,"5":6191,"6":-1}` ms，即時走路 5397 ms，超過 2.5 秒；另一項是延遲讀取場景文字時過場已結束。
  - 原因：無頭 Chrome（SwiftShader）在高負載下幀很慢，Unity 會把每幀的 `deltaTime` 截到上限，遊戲時間落後真實時間；期限用 `Time.time` 判斷，所以在慢瀏覽器裡不成立。這是產品缺陷，不只是測試問題：一般瀏覽器在慢機器上也會發生。
  - 修正（`d7070af`）：期限改用真實時間；新增 `TheLimitHoldsOnSlowFrames` 重現慢幀；瀏覽器檢查改從記錄器讀「走路期間」頁面說了什麼，上限加入 700 ms。
- 既有兩項 `R2 order`（前置條件：回放前期限需在 4–8 秒後；實際只剩 1 秒），屬既有計時前置條件，本輪未改；`d7070af` 的全套中通過。

`d7070af` 唯一的失敗 `3.1 pagehide: a readiness poll is held`：該檢查等待 8 秒內出現下一次 5 秒一次的就緒輪詢；3.1 與 `web/host.js` 本輪都沒有改，同一段之後的「late poll restores nothing」「no more polling after leaving」都通過；`bc79677` 的全套中這項通過。判斷為高負載下的計時失敗，未修改該檢查，也沒有為了轉綠重跑；請 review 判斷是否需要重跑。

### 17.4 反向驗證、容器與 Go（Claude Code 執行，待 Codex 獨立 review）

每項只改一處、跑測試、再以位元組比對還原（`artifacts/b1-06/d2-checks-d7070afdc0bd/unity-reversals.py`）。

| 反向 | 改法 | 結果 |
|---|---|---|
| U1 | 水果台放回 (5.0, 1.4) | PlayMode 2 項失敗：`RoutesKeepClear…`、`AnEndOffTheWay…`（路線掃過水果台） |
| U2 | `Relabel` 一律回傳「已到達」（途中就回報） | PlayMode 3 項失敗：`TheLimitIsKept…`、`TheSameTarget…`、`ZeroThreeSix…` |
| U3 | 拿掉第 2 條 | EditMode 共用向量、PlayMode `ZeroThreeSix…` 失敗 |
| U4 | `Parse` 不檢查 `cause_seq` 的 JSON 型別 | EditMode `WrongTypesAreUnknownNeverANumber` 失敗 |
| U5 | 第 3、4 條更新時重設期限 | PlayMode `TheLimitIsKept…` 失敗 |
| U6 | `rendered` 回報過場開始時的序號 | PlayMode `TheLimitIsKept…`、`TheSameTarget…` 失敗（第一次的改法有語法錯誤，編譯失敗不算數，保留於 `U6-first-try-compile-error/`，加上大括號後重跑） |
| U7 | 期限與走路改回遊戲時間（`Time.time`、`deltaTime`） | PlayMode `TheLimitHoldsOnSlowFrames`、`TheLimitIsKept…` 失敗 |
| N1–N4（Node，於 `bc79677`；`d7070af` 未改 web） | 拿掉第 2 條／標籤序號改用快照序號／`task_end` 改取最後一個／未知序號也算新請購 | 各自讓共用向量、投影或回放序列的測試失敗（`node-reversals-bc79677.txt`） |
| N5（瀏覽器，於 `d7070af`） | `app.js` 不標首次載入 | 305 PASS／5 FAIL：D2 第 1 項三項、第 2 項「as a first load」一項，另一項為既有的 `R2 order` 前置條件（見 17.3）（`artifacts/b1-06/reversal-d7070afdc0bd-first-load/`） |

**完整容器**（`d7070af`，`APA_VERIFY_IMAGE=docker.io/library/apa-verify:b1-05 DOCKER_CONTEXT=desktop-linux sh scripts/verify/container-verify.sh d7070af`，exit 0）：映像 `sha256:78258368…b6d0`（與既有紀錄同一映像）；七步全過：check-contracts 126 秒（adapter 24／24、hostcreds 23／23）、check 416、race-cover 894（`cmd/apa-buyer`、`cmd/demo` 85.5％、`internal/buyer` 都通過）、verify-b1-01 262、guards 11、evidence-import-tests 3、mutation 2558 秒 **247／247 全數被殺**；隔離自檢 17 項 ok。主機高負載，所以比以往（約 11 分鐘）慢很多。證據：`artifacts/verify-container/runs/d7070afdc0bd-20261007T063755Z/`、`artifacts/b1-06/container-d7070af.log`。
- 前三次嘗試失敗、未執行：daemon 對短名 `apa-verify:b1-05` 回「No such image」，但 `docker images` 列得出、全名 `docker.io/library/apa-verify:b1-05` 可解析到同一個 ID；改以 `APA_VERIFY_IMAGE` 指定全名後執行。紀錄在 `artifacts/b1-06/d2-checks-d7070afdc0bd/container-attempt1.txt`。

**本機 Go**（`bc79677`，`go test ./... -count=1`，非沙箱）：599 passed、4 failed、3 skipped。失敗都在 `cmd/apa-buyer`（`TestLiveOptIn`、`TestProbeOptIn`、`TestApprovedVersionInit`）與 `internal/buyer`（`TestVersion`：「holder check took 2.69s」）的版本檢查計時；這兩個套件本輪沒有改。當時主機負載平均約 175（另有長時間執行中的 Python 程序，不屬本工作，未處理），閒置後重跑兩個套件仍失敗（負載未下降）。同樣的測試在容器（4 CPU 隔離）中通過。本輪唯一的 Go 變更是 `internal/store` 的測試檔，該套件通過。

### 17.5 未做與限制

- Unity 標籤的 #2（見 17.1 與設計文字的差異）。
- 瀏覽器只能讀網頁文字與 Unity 的回報，看不到畫布裡的標籤；Unity 標籤與位置由 PlayMode 驗證。過場中的截圖在本次高負載下沒有拍到（17.2）。
- 即時「新的一筆請購」：fake CLI 同一商品重送只算一筆請購（`retry-same.json`），所以第 2 條的瀏覽器驗證用 Store 寫入的歷史回放（16.9 第 3 項允許）；即時模式由 PlayMode 與 Node 覆蓋。CLI 逾時的 Unity 行為由 PlayMode 與 Node 覆蓋，瀏覽器未另測。
- 一般 Chrome（不帶兩個參數）長流程的可見性限制沿用 §15.7，本輪未跑 `--smoke-plain`。
- 高負載下的計時：`R2 order`（`bc79677` 與反向執行各一次）、`3.1 pagehide`（`d7070af` 一次）失敗；兩者都不是本輪的變更，未修改。
- 不合併、不推送、不標 Done；D3–D6 未開始。

### 17.6 依 Codex 對 `d7070af` 的 review 修正（Claude Code 執行，待 Codex 獨立 review；程式 `78cb2cc`、測試補充 `6137708`）

依據：Aaron 轉交的 Codex review。轉貼內容從 `web/app.js:84` 一項開始（沒有編號），之前的部分可能缺漏；vault 中沒有完整筆記。Codex 暫存目錄 `/private/tmp/apa-d2-review.G02d78` 的兩項 Unity 探測失敗，判斷屬於同一份 review，一併修正（下列第 2 項）。Codex 對兩件道具移位與櫃檯標籤不帶 #2 沒有提出退回。

1. **「補間中」提前結束**（`web/app.js:84`）：原本只比對世代與 `回報序號 >= 目標序號`，錯的 task、錯的 phase、未送出的較大序號都會結束提示。
   - 改為 `adapter.sameShown`：與最後送出的投影 gen、task、seq、phase 全部一致才結束；與查證面板「同步」同一規則。bridge 不變。
   - Node：`sameShown` 的正例與五個反例（另一 task、另一 phase、未送出的較大序號、較舊序號、另一世代）；另以 Codex 探測的方式執行 `app.js` 自己的回報處理，反例都不結束、完全一致才結束。
   - 瀏覽器（D2 即時段）：以頁面測試鉤子扣住 Unity 真正的 `rendered`，確定 Unity 仍在路上時餵入三則不符的回報，`data-motion` 三次都仍是 `moving`；放行真正的 #3 後才變成 `shown`。走路時間改以鉤子收到回報的時間計算。
2. **清除或換客人後殘留 `[anim]` 標籤**（Codex 探測 `ClearDuringBrowseMustClearVisibleLabel`、`ConsumerSwitchDuringBrowseMustClearInactiveLabel`）：`ShopView.ResetAll` 沒有重設目前段落，選商品途中清除或換世代後，客人頭上仍是 `[anim] browsing - no event`。改為一併重設；新增兩個 PlayMode 案例。
3. **C1 `3.1 pagehide`**：原檢查等 8 秒內下一次 5 秒輪詢，扣住第一個符合 `status` 或 `viewer` 的請求。改為共用的 `pagehideCheck`，兩種方式：
   - **network**：以 `visibilitychange` 主動觸發一次就緒輪詢，瀏覽器只扣住那一個 `GET /api/host/status`；觸發 pagehide 後放行，並等網路層確認該請求結束，才檢查憑證、畫面、輪詢。**實測發現**：頁面在 pagehide 時 abort，請求被取消（`Invalid InterceptionId`；`loadingFailed`、canceled），回覆無法再放行。
   - **reply**：為了真的「放行回覆」，回覆已到但內容由頁面內的測試用 fetch 包裝扣住（同 3.1-R1 的做法），pagehide 後才交給頁面；不得恢復憑證、畫面或就緒，也不再輪詢。
   - `--pagehide-only`：只跑這項，network 與 reply 各 5 輪（定向）。

**驗證**（全部 Claude Code 執行，待 Codex 獨立 review）

| 項目 | commit | 結果 | 證據 |
|---|---|---|---|
| Node（`git archive` 匯出） | `78cb2cc` | 49／49 | `artifacts/b1-06/d2r-checks-78cb2ccec4b1/node.txt` |
| Unity EditMode／PlayMode | `78cb2cc` | 10／10、14／14 | 同目錄 `unity-test/` |
| WebGL 重建 | `78cb2cc` | exit 0；hash 見下 | `webgl-sha256.txt` |
| 定向 pagehide（network 5 輪） | `78cb2cc` | 30／30 PASS | `artifacts/b1-06/pagehide-78cb2ccec4b1/` |
| 瀏覽器全套 | `78cb2cc` | 312 PASS／1 FAIL（`R2 order` 前置條件，見下） | `artifacts/b1-06/ui-78cb2ccec4b1-20261007T085630Z/` |
| 定向 pagehide（network 5＋reply 5 輪） | `6137708` | 55／55 PASS | `artifacts/b1-06/pagehide-613770850fab/` |
| 瀏覽器全套（同一 WebGL 建置；`6137708` 只改測試腳本） | `6137708` | **317 PASS／0 FAIL** | `artifacts/b1-06/ui-613770850fab-20261007T091325Z/` |

- 「定向通過」與「全套通過」分開記錄：定向 `6137708` 55／55；全套 `6137708` 317／0，其中 3.1 pagehide 的 network 5 項與 reply 4 項都通過。另有一次定向執行因反向改法沒有套用成功而等於未改動的重跑（55／55），保留為 `browser-pagehide-only-2-mutation-not-applied.txt`，不算反向。
- WebGL（`78cb2cc`，`6137708` 沿用）：`WebGL.data` `cd2321fcdb872d597755ff86ddeffc116c25f471cfdff33ee2a098f2a395ac73`、`WebGL.framework.js` `3c7d6ab5547a85de82e08400bfd63520ba9c2f50c098eff05074a6a010e65666`、`WebGL.loader.js` `77883badb548b3adab32685f56cac7e7ab4eb817fcd48f45cf7d54a6f663bc80`、`WebGL.wasm` `ac4e76c99d666a6af7303e8d100104c2a6ce50ca5fcd74c92a27916f0d704a7d`。
- Go、Store、驗證工具未改，依 review 未重跑完整容器與 247 項突變。

**反向**
- N6（Node）：在 `sameShown` 旁加回舊的 `seq >=` 判斷：`app.js` 回報處理測試失敗（「another task: still in transition」）。
- U8（Unity）：`ResetAll` 不重設段落：正好兩個新案例失敗。
- 瀏覽器定向：
  - 拿掉 `readiness()` 與 `pollReadiness` 的世代檢查：**仍 55／55 通過**。就緒面板依目前的憑證繪製，pagehide 已清空憑證，所以晚到的回覆即使被接受也沒有可顯示的內容；這項檢查驗證的是結果（什麼都沒恢復），不能單獨證明世代檢查本身。保留為 `browser-reversal-pagehide-no-gen-guards.txt`。
  - pagehide 不清除憑證：30 PASS／25 FAIL（network 每輪 3 項、reply 每輪 2 項）。

**`R2 order` 前置條件（第三次出現）**：這項要求進入回放時，task-a 的核准期限還在 4–8 秒後。期限在 fake plan 開始時就固定，之前的步驟在高負載下變慢，到這裡時已不足 4 秒。`78cb2cc` 這次只有前置條件失敗，之後「回放時仍是 #3」「回放中提交 #4」都通過；`bc79677` 那次「回放時仍是 #3」也失敗。這是本輪沒有改的區段，`6137708` 的全套中通過。建議另行改成與前面步驟耗時無關的設定（例如在這一段才開始的 task），本輪未改，請 review 決定。

**限制**：同 17.5。主機負載仍高；D2 回放 #5 的實測值為 3192 ms（`78cb2cc`，上限 3200 ms）與 2193 ms（`6137708`），在慢機器上接近上限。
