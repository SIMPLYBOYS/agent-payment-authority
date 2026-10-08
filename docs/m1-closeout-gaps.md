# M1 收尾差距盤點（短清單）

> 2026-10-09；Claude Code 只讀核對，待 Codex review。基準 HEAD `7c853d4`。
>
> 本文件只對照文件與 repo 內容，**本輪沒有執行任何測試、CLI 或展示**。
>
> 依據：
> - vault 任務清單 §1（M1 完成門檻）、§4 B1-04／B1-06／B1-07 任務卡、§6 統一驗收入口；
> - 執行計畫 §5 驗收 41–44；
> - repo `docs/cli-poc.md` 第 53～56 節、`docs/commercial-demo-roadmap.md`。

## 0 結論

- **M1 完成門檻**：B1-00～B1-07 全部通過。B1-06 產出執行證據，再由 B1-07 只讀核對；任何必要 claim 為 FAIL 或 INCONCLUSIVE，都不能結案。
- **目前狀態**：
  - B1-00～B1-03、B1-05 已 Done。
  - B1-04、B1-06 的能力大多已做出並經限定 review，但都**還沒有依任務卡的形式交出出口證據**。
  - **B1-07 完全未實作**，所以 M1 目前不能結案。
- **最短路徑**：
  1. 固定 M1 證據包格式，以 `make m1-fixture` 產生 fixture 證據（B1-06 的出口）；
  2. B1-07 只讀檢查器 `make verify-m1`；
  3. 一次 `make m1-live-cli` 真實證據包，並以 B1-07 驗證；
  4. `docs/m1-result.md` 與正式結案。

## 1 對照 M1 門檻

| 任務 | 判定 | 依據與缺口 |
|---|---|---|
| B1-00～B1-03、B1-05 | **已完成** | 任務清單第 3 節為 Done（Aaron 或 Codex 驗收）。 |
| B1-04 真實 Buyer wrapper | **能力已完成，缺證據形式** | 真實 CLI → wrapper → MCP → Ask → Store 已走通三次（第 53、55.11、56.9 節，Codex 核對紀錄）；live 開關、版本釘選、init Gate、fake runner 的錯誤案例都有測試與 review。缺：任務卡勾選項未逐項核對；live trace 不是以 B1-06 定義的 `make m1-live-cli` 證據包產生，也沒有經 B1-07 核對。 |
| B1-06 展示層與整合 | **展示能力已完成，缺 M1 出口** | 已完成：Unity＋LimeZu 商店、Dashboard、Inspector／Timeline、主持人 HTTP、Store 接線、A／B 隔離、取消與重啟、歷史回放、單一真實即時展示、啟動簡化（D1、D2、3.x、第 54～56 節）。**未實作**：`make m1-fixture`、`make m1-live-cli`、`tests/m1/`、`artifacts/m1/<run-id>/` 證據包、`docs/m1-result.md`。**缺證據**：在沒有 sibling repo 的乾淨目錄依 README 完成 fixture E2E。 |
| B1-07 只讀驗收器 | **未實作** | `cmd/verify-demo/`、`internal/verification/`、`testdata/verification/`、`make verify-m1` 都不存在。Owner 未指派。 |
| S1～S3、S4-B | 不列入 M1 門檻 | 依任務清單分列；S4-A 已 Accepted。 |

## 2 B1-04 與 B1-06 的必要出口

**B1-04**
1. 逐項核對任務卡的 8 個勾選項並附證據位置；大多已有 review 紀錄，交 Codex 判定能否勾選。
2. 以 `make m1-live-cli`（下面的 B1-06 第 2 項）跑出一次有上限的真實請購證據包，並經 `make verify-m1` 核對。
3. **需要決定**：M1 的 live 證據是否接受「開發展示 plugin 例外」下的結果。嚴格零 plugin 模式在 Aaron 本機至今沒有通過（決策稿 9.1）；這是範圍決定，不是工程缺陷。

**B1-06**
1. `make m1-fixture`：用 fake CLI 跑凍結的案例集，產生 `artifacts/m1/<run-id>/`，內容包含：
   - 程式 commit 與 dirty patch digest、依賴／CLI／模型版本、非敏感設定 digest；
   - case manifest 版本（`testdata/contracts/cases.json` 的 digest）、命令與 exit code；
   - 遮罩後的事件、每個案例的結果、結束標記。
   - 不含 token、配對碼或 CLI 原文。
2. `make m1-live-cli`：明確 opt-in 才跑一個有上限的真實任務，輸出相同格式的證據包（可重用 `present.sh` 或 `live.sh` 的路徑，不另寫 runner）。
3. `tests/m1/` 的案例對照任務卡的必測清單：
   - 主商品 Ask（`ask`）、額度不足 Deny（`deny-budget`）、非白名單 Deny（`deny-merchant`／`deny-asset`）；
   - CLI timeout／cancel（`cli-timeout`、`cancel-*`）、重複請購（`retry-*`）、重啟／刷新（`restart`）；
   - 惡意字串、LLM 自稱已付款（`no-premature-settlement`）、A／B 冒用（`cross-consumer*`、`altered-*`）。

   多數已有 Go 或瀏覽器測試，缺的是**以同一個 run ID 匯出成證據包**。
4. `docs/m1-result.md`：只放可公開的摘要與證據索引，並說明 hash／manifest 無法防止作者重製整包。
5. 乾淨目錄的 README 啟動與 fixture E2E 證據。

## 3 B1-07 的最小範圍

**輸入**：`artifacts/m1/<run-id>/` 與凍結的 case manifest，只讀。不呼叫 `policy.Decide` 等執行器函式作為判準；可以共用 schema 與 canonical parsing；不連模型或 testnet，也不改寫輸入。

**必要 claim**（每項輸出 PASS／FAIL／INCONCLUSIVE、expected／observed 與證據引用）：

| # | claim | FAIL 的例子 | INCONCLUSIVE 的例子 |
|---|---|---|---|
| V1 | 證據包有來源資訊（commit、dirty digest、版本、case 版本、run ID、命令） | — | 缺少任一項 |
| V2 | 每個案例的請購對得上輸入（金額、收款人、asset、requirement／intent digest） | 金額或收款人不符 | 缺少請購資料 |
| V3 | 裁決與原因等於凍結的 expected outcome | 預期 Deny 卻出現 Allow 或 Ask | — |
| V4 | 事件序號連續、hash chain 相連、綁定同一 task | 中段被竄改 | 缺少結束標記或 trace |
| V5 | 已宣告結束的 run 內沒有 Approved、Settled 或交付事件 | 出現假造的 Settled | — |
| V6 | evidence mode 一致：fixture 不冒充 live；fake 與 live 分開驗 | fixture 標成 live | live 證據缺失 |
| V7 | A／B registry snapshot：consumer、agent、task、wallet、from 一致 | 錯誤的 payer、A 的事件歸到 B | — |

- **`not_evaluated`**（不得 PASS）：憑證／BBS+、WebAuthn、ZK、Signer、鏈上付款與扣款隔離、真實 key 選擇。
- **退出碼**：必要 claim 全部 PASS → 0；FAIL 與 INCONCLUSIVE 各用不同的非零碼。
- **驗收**：
  - `go test ./internal/verification/...`，用 golden fixture 測檢查器本身：正常 Ask／Deny 通過；Allow 冒充、金額或收款人不符、假 Settled、中段竄改、fixture 冒充 live 應 FAIL；缺 trace 或結束標記應 INCONCLUSIVE。
  - 從另一個終端分別對 fixture 證據包與 live 證據包執行 `make verify-m1`。
  - 限制要寫明：沒有獨立 checkpoint 時，尾端缺漏可能無法偵測。
- **計畫驗收 43 的 M1 子集**（合法 Ask → 受控負向輸入 → 合法 Ask，同一凍結版本、不改 policy）：可以完全用 fixture 完成，不需消耗模型額度。

## 4 建議的下一個切片

**M1-E1：M1 證據包格式與 `make m1-fixture`**（B1-06 出口的第 1、3 項）。

- **理由**：B1-07 要讀的就是這個格式，必須先固定；而且全部用 fake CLI，可以直接實作，不需要真實執行的核准。
- **範圍**：
  - 證據包格式（manifest＋逐案例結果＋遮罩事件＋結束標記）與 schema 文件；
  - `make m1-fixture` 用 fake CLI 跑必測案例，每次一個新的 run 目錄；
  - 沿用既有的 demo host、buyer host 與 Store，不改 Policy、事件契約或 Gate。
- **驗收**：
  - Go 測試：證據包的欄位齊全；同一份輸入跑兩次，除了時間與 ID 以外內容一致；案例結果等於 `cases.json` 的 expected；
  - 證據包中掃不到 token 或配對碼；
  - 對新增的保護（例如遮罩、結束標記）做反向驗證；
  - 交 Codex 限定 review。
- **之後的順序**：
  1. M1-E2：B1-07 檢查器（第 3 節）；
  2. M1-E3：`make m1-live-cli` 一次真實證據包，需 Aaron 另行核准；
  3. M1-E4：`docs/m1-result.md`、乾淨目錄 E2E、逐卡結案。

  若希望更快，也可以把 E1 與 E2 合成一個切片，但 review 範圍會變大。

## 5 不算入 M1 的項目（後續商業展示的要求）

以下保留為商業展示版或第二期的要求，**不混算成 M1 已完成或 M1 的缺口**：
- 多 Agent 同時購物；
- 最多 20 個付款帳號；
- Admin 即時活動與累積數據；
- 真實核准與 grant；
- BBS+ 憑證、真 ZK（gnark）；
- Signer、鏈上預留與結算（兩筆 tx）、商店交付；
- 真實 testnet 錢包與資產隔離（計畫驗收 44 的付款階段）；
- 委託介面與語音。

M1 完成也不等於第一期 PRD 驗收完成。

## 6 本盤點的限制

- 只讀文件與目錄結構，沒有執行測試；第 1 節「已完成」的判定沿用既有的 review 與驗收紀錄。
- 任務卡的勾選項由 Codex 或 Aaron 判定；本文件只提供對照與建議，不改任何任務狀態。B1-04／B1-06 維持 InProgress，B1-07 維持 Waiting。
