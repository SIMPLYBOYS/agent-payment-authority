# B1-06 商店展示深化設計（fake 展示模式）

狀態（2026-10-07）：**設計提案，Claude Code 撰寫，待 Codex review。尚未核准實作。** 本文件不修改產品程式、契約、Store、Policy、Gate 或 Unity，也不核准真實 CLI、付款、testnet、ZK 或素材公開發布。

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

- **選取單位**：一筆 `purchase_order_id`。預設是場景正在顯示的那筆（`adapter.scene()` 選出的最新裁決）。同一 task 有多筆請購時，用「其他請購 ▾」切換；切換只改卡片與查證面板，不改 Unity 的場景。若選的不是場景顯示的那筆，要標出「場景顯示的是 po-…」。
- **步驟條 ①–⑥**：沒有獨立事件的步驟（② 選商品、③ 報價時刻）用空心圈，註明「無獨立事件」。只有在已提交事件到達時，才點亮 ④、⑤、⑥。
- **金額**：分開顯示 402 綁定的 amount（基本單位與 0.50 test USDC）和目錄的 KRW 700（fixture，不綁定）。不使用同一個「金額」欄位代表兩者（執行計畫 L413、PRD L167）。
- **期限**：顯示 `deadline_at`，並標明 Store 時間。前端倒數到 0 時只顯示「已達期限，等待後端確認」（現有 `adapter.scene()` 行為）。直到 `APPROVAL_EXPIRED` 寫入，才顯示「已到期」。

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
| 歷史回放 | 不讀新資料，逐步重演已保存事件 | 狀態列「回放中」 | 不寫 Store、不重送請購；結束後恢復最新狀態（現有） | 現有 |
| 點選客人 | 只是選取意圖；網頁檢查權限 | 卡片切到該客人可見的 task | 無權限時顯示「需要該 consumer 的 viewer token」 | 未實作（D3，需要另行派工，PRD L158） |
| 付款、取貨、完成 | 無 | 能力條標「付款 未實作」 | 一律不演 | 未實作（M1 之外） |

## 6 守則與設計的對應

1. **權威結果來自後端**：Unity 的 phase 只由 `adapter.scene()` 從已提交事件決定（現有）。卡片與步驟條也只讀同一份快照，動畫計時器不決定任何狀態。
2. **補間不產生裁決**：走路與待機只連接兩個已成立的站點。`rendered` 只是「已呈現」的回報，不回寫狀態。
3. **資料先於動畫到達時追上**：見 6.1。網頁上的文字（狀態列、卡片、Timeline）一律立即更新，不等 Unity。
4. **不冒充推論**：fake 模式不顯示任何「思考」字幕。步驟②只標「無獨立事件（fake 腳本）」。
5. **未取得詳情就是未確認**：沿用 3.4 R2 的規則，套用到卡片。
6. **倒數不等於後端到期**：沿用現有的「已達期限，等待後端確認」。到期只關閉該筆 Ask，task 仍可取消。
7. **依 `purchase_order_id` 對齊**：卡片、查證面板與場景都以 po 為單位（`adapter.scene()` 已如此）。
8. **場次停止 ≠ task 取消**：最近場次維持「當時」與「目前」分開（3.4）；Deny 原因不被覆蓋。
9. **世代與晚到防護**：卡片與查證面板掛在 app.js 的同一個世代（`state.gen`、`idGen`、`listSeq`），清除或切換時一起清空。Unity 的選取回報也要帶 gen，舊 gen 一律丟棄（D3）。
10. **Unity 不碰 token**：選取只用 `APA_Report("select", gen, seq, "consumer|task")` 回報 Unity 自己已知的值；網頁再檢查權限後，用既有的認證讀取。
11. **回放**：沿用現有規則，回放時狀態列與卡片明標「回放中」，卡片顯示回放的那一步。

### 6.1 追上規則（D2）

- 走路中收到較新的序號：
  - **新 phase 對應的站點在下一個中途點之後**：取消剩餘的中途點，以加速（例如 2 倍）走到目標，到達後只回報最新序號的 `rendered`。
  - **落後超過一個 phase，或使用者剛切換畫面**：直接瞬移到目標（`animate=false`）。
- 單次走路最長 1.5 秒；超過時以瞬移收尾。過場不能阻擋安全狀態更新，例如 Deny 或到期一到就要在網頁上顯示，不等 Unity 走完。
- 被取代的那一步不回報 `rendered`（現有行為）。測試以最新序號的 `rendered` 為準。

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
| 場景對應 | Unity 最後回報的 gen、seq、phase、task（瀏覽器觀測）對照快照的 `as_of_sequence`、phase（API） | 不一致時顯示「Unity 同步中」，不判定成功 |

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
- **場景與證據的對應**：卡片的 task、po、phase 與 Unity 回報的 task、seq、phase 對照，顯示在「場景對應」頁。
- **相依**：3.1–3.5 的既有行為與測試。
- **回歸風險**：
  - DOM 重排會影響既有 255 項瀏覽器檢查的選擇器。保留既有 id（`#status`、`#inspector`、`#timeline`、`#run-status`、`#runs`、`#task-pick`、`#list-note`、`#readiness`、`#creds`）。
  - `<details>` 收合時，`innerText` 讀不到內容，檢查需要先展開。
- **失敗呈現**：沿用既有的未確認、離線、503 文字，並加到卡片上。收合狀態的摘要列一定顯示錯誤數。
- **驗收條件**：
  1. 1280×900 的 `/host` 與 `/`，Unity canvas 完整落在第一個 viewport 內（以 `getBoundingClientRect` 斷言）。開始、停止、取消也在第一個 viewport。
  2. Ask 流程中，卡片、查證面板、Inspector 全欄位、Timeline 與 Unity 回報的 task、po、序號、phase 一致。`intent_hash` 與 `payment_requirement_hash` 與 API 相同。
  3. 金額分兩行（402 綁定、目錄 fixture），沒有單一混合金額。
  4. 所有區塊收合時，fake、回放、未確認、清單更新失敗、就緒未確認仍然可見（以可見文字斷言，不只看 `data-*`）。
  5. 切換 consumer、task 或清除時，卡片與查證面板立刻清空；晚到的回覆不恢復舊內容（沿用世代測試手法）。
  6. 全頁沒有「付款成功」「已驗證」「通過」等字樣；能力條四項顯示未啟用、未實作或未執行。
  7. 窄視窗（390×844）的順序如 4.3，沒有水平捲動。
- **驗證層級**：Node（adapter 新純函式，含多筆請購與缺資料）、真實瀏覽器（上列 7 項，加既有回歸）。不需要 Unity 重建；Go 未改時不需要完整容器（依既往做法，slice 最終 commit 的取捨由 review 指定）。

### D2 場景節奏：中途點、四向走路與追上

- **對應**：B1-06（L197–198），PRD L162「動畫不能跑在事件前面」。
- **修改**：`ShopView.cs`（門→走道→櫃檯的中途點、上下行走影格、限時與加速、6.1 追上規則）、`ShopBuilder.cs`（如需門口道具）、PlayMode 測試；重建 WebGL。
  - bridge 欄位不變，所以 `web/` 不改。
- **觀眾看到的差異**：客人沿通道進店、到貨架、再到櫃檯，不再穿過貨架；Deny 或到期時轉身離店；新狀態一到，會加速或瞬移追上，不卡在半路。
- **風險**：
  - Unity 重建會讓建置檔 hash 改變，證據要重記。
  - 回放的逐步時間會變長（`renderedBy` 的 6 秒上限要能容納）。
- **驗收**：
  - EditMode／PlayMode：每個 phase 的終點、追上規則、最長時間、只回報最新序號；
  - 真實瀏覽器：既有 Unity 斷言全過，回放順序檢查（`docs/demo-operation-design.md` 16.1）仍通過。
- **驗證層級**：Unity 測試＋建置＋瀏覽器。Unity 有變動時，最終 commit 跑完整容器（容器不含 Unity，但要確認沒有連帶影響）。

### D3 點選客人（選取意圖）

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

### D4 請購當時報價的白名單欄位（需核准 API 擴充）

- **對應**：FR-4（`payment_requirement_hash` 綁定）、PRD L163 的商家與報價欄位。
- **修改**：`internal/store`（從 `purchases.body` 讀出的白名單）、`cmd/demo/api.go`、adapter 與查證面板。
- **觀眾看到的差異**：報價頁顯示請購當時 402 的完整白名單內容，並可看到它與 `payment_requirement_hash` 相符（由伺服器重算後提供 match 結果，或前端只顯示兩者並標「未在前端驗證」，待 review 決定）。
- **驗收**：Go（白名單、權限、重算一致、未知欄位不外洩）、adapter、瀏覽器。
- **驗證層級**：Go 變更，最終 commit 跑完整容器與突變。

### D5 Allow 路徑可示範（需 Aaron 決定，見第 10 節）

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
| 任務清單狀態表 L65（B1-06 列） | 列中只寫到 3.1 通過；3.2–3.4 已通過、3.5 自動化部分待 review 的紀錄在「目前下一步」與交付紀錄中 | 本輪只指出差異，不改狀態表；建議下次驗收同步時一併更新 |

## 12 仍未完成的主線（分開報告）

| 主線 | 狀態 |
|---|---|
| 視覺與操作（fake 展示） | 3.1–3.4 已通過限定 review；3.5 自動化部分待 review，人工清單待 Aaron；本文件是下一步設計 |
| 真實 Agent（真實 CLI 請購） | 未啟用；B1-04 InProgress，真實 CLI 執行未核准 |
| 鏈上支付（Signer、預留、x402 settle） | 未實作（M2） |
| 憑證與 ZK（BBS+、gnark） | 未實作（spikes 另計） |
| B1-07 獨立證據驗證 | Waiting；展示面板不取代它 |

## 13 本輪檢查（Claude Code 執行，待 Codex 獨立 review）

- 只做閱讀、`git` 狀態核對與文件連結檢查。沒有執行產品測試、Unity、容器或突變，也沒有拿舊測試結果當成本輪結果。
- 本文件引用的程式行號，以 `17efeac` 的檔案為準。
