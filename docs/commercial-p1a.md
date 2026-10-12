# P1-a：委託與單筆模擬核准

> 2026-10-12，Claude Code。依 vault〈Claude-Code-P1-a委託與單筆模擬核准實作派工-b0700a7-20261012〉。分支 `commercial-p1a`，基準 `b0700a7`。
> **狀態：P1-a 已實作，待 Codex review 與 Aaron fake 操作驗收。** 本文的驗證結果都由 Claude Code 執行，待 Codex 核對；沒有 Codex 獨立重跑，也沒有 Aaron 的體驗判定（首版截圖 Aaron 已看過，回覆「目前是滿意的」，這不是 fake 操作驗收）。
> 本片**不付款**：核准只留下一筆明標 `simulated` 的授權（grant），它不能取得付款能力；沒有金鑰、簽署、faucet、部署或交易，沒有執行真實 CLI。

設計依據是 [commercial-transaction-flow.md](commercial-transaction-flow.md) 第 7–11 節。三項前置決定已由 Aaron 核准：付款帳戶採 EOA＋EIP-3009（[ADR](adr/payment-account-model.md) 改為 Accepted，`5aa5f12` 提交 Codex 依核准寫入的修改）；首片只用 fake 模式的模擬核准者；`apa.pay.v1` 接線與 schema 3 範圍照第 10 節。

## 1 使用者實際走完的流程

配對 → 在客人 A 的卡上按「以 A 模擬（fake）」→「建立委託」看主持人設定的內容 →「確認委託並開始（模擬）」→ Agent 用原本的三個工具選購、請購 → 金額 0.50 高於委託的 allow_max 0.10，得到 Ask → 右上「需要你處理」顯示單筆摘要與後端期限 →「核准此筆（模擬）」或「拒絕」→ 結果與證據。

成功出口是「已核准（模擬），未啟用付款」，Store 裡恰好一筆 `authorization_method=simulated` 的 grant，Signer 樁拒絕它（`GRANT_METHOD_NOT_ACCEPTED`）。拒絕、過期、取消與報價改變各有自己的結果，都沒有 grant。

## 2 實作內容

| 部分 | 內容 | 位置 |
|---|---|---|
| 契約 | `apa.pay.v1`：業務意圖（`Intent`）與付款綁定（`PaymentBinding`：EIP-3009 的方法、鏈、資產、付款人、收款人、金額、時間窗、nonce）一起算 `intent_hash`，網域 `agent-payment-authority/pay/v1`；自己的驗證、重試鍵、裁決規則（`payment_status` 一律 `not_enabled`）、事件規則與 hash 鏈；委託與模擬 grant 的格式；凍結的 golden vectors | `internal/paycontract/` |
| Policy | 從 `policy.Decide` 原樣搬出判斷順序成 `policy.Rules`；`policy.Decide` 的入口、輸出與 M1 測試都不變；新入口 `paypolicy.Decide` 自己驗證、算 v1 hash、寫 v1 裁決（範圍內是 `Allow`／`WITHIN_ALLOW_MAX`，不是付款） | `internal/policy/policy.go`、`internal/paypolicy/` |
| Signer 樁 | 只判斷規則：模擬 grant 一律 `GRANT_METHOD_NOT_ACCEPTED`；沒有金鑰、不簽、不記錄、不消耗 jti | `internal/signer/` |
| Store schema 3 | `tasks.contract_version`、`host_runs.contract_version`／`delegation_id`；新表 `delegations`、`approval_sessions`、`grants`（`authorization_method` 由 CHECK 限定 `simulated`）；`--upgrade-db` 從 schema 1 或 2 升到 3（先驗證、`VACUUM INTO` 備份、單一交易遷移、再驗證）；開啟時的復原檢查涵蓋新表 | `internal/store/store.go`、`pay.go`、`verify.go`、`runs.go` |
| 寫入規則 | `Submit` 依 task 存的版本選入口，請購內容不能選（`CONTRACT_VERSION_MISMATCH`）；v1 的 Ask 與核准會話同一交易寫入；`ResolveApproval` 在一個寫入交易裡依序：核准者的 consumer、同一請求的重送、先記錄已到期、會話是否仍 pending、核准者看到的 `intent_hash`、報價是否仍在報價簿；核准時關閉會話、寫 grant 與 `APPROVED`、`GRANT_ISSUED` 一起提交 | `internal/store/pay.go` |
| 委託 | 委託在 task 之前確認並保存（不建 task）；Gate 通過後 `StartTask` 在同一交易建立 task、綁定委託、寫 `TASK_STARTED` 與 `DELEGATION_BOUND`；委託只能縮小 profile（預算、allow_max、商家），到期、已綁定、別人的委託都拒絕 | `internal/store/pay.go`、`ops.go` |
| 工具橋接 | 三個工具與模型看到的回覆欄位不變；依 Store 裡 task 的版本組 M1 或 v1 請購 | `internal/toolbridge/shop.go` |
| Host | 情境目錄可標 `contract: apa.pay.v1` 與委託條件（只限 fake；live 目錄拒絕）；開始時要帶確認過的 `delegation_id`；模擬核准者：`POST /api/host/simulate`（operator、同源）換一個綁 consumer 與這個 host 程序、30 分鐘的短效能力，一次一位；`GET /api/sim/approvals`、`POST /api/sim/delegations`、`POST /api/sim/approvals/{id}` 只收這個能力；viewer、host、operator token 一律 403；live host 一律 `simulation_unavailable` | `cmd/demo/sim.go`、`control.go`、`scenarios.go` |
| M1 證據 | M1 匯出與檢查器遇到 `apa.pay.v1` task 明確拒絕（`M1_CONTRACT_REQUIRED`） | `internal/m1evidence/` |
| 網頁 | 頁首「P1-a／fake agent／模擬核准／不付款」與「角色模擬：客人 A（主持人扮演，不是本人驗證）」；卡上「以 A 模擬」「建立委託」與委託內容（唯讀）；「需要你處理」：模式列、單筆摘要、後端期限倒數（只是提示）、一個主要按鈕與拒絕、結果與證據入口；Agent 與請購的狀態分開寫；設計註解沒有搬進頁面 | `web/host.html`、`host.js`、`hostcreds.js`、`adapter.js`、`style.css` |

## 3 與設計草案不同或補充的地方

- **付款欄位只放一次**：第 3.1 節寫業務意圖也含金額、收款人等；實作把這些只放在 `PaymentBinding`，`intent_hash` 一起涵蓋兩者，避免同一欄位兩份還要比對一致。
- **別的客人的會話回 404，不是 403**：B 的模擬核准者讀或核准 A 的會話時，與不存在的會話同樣回 `not_found`，不透露 A 的會話是否存在（第 7 節表格原寫 403）。
- **沒有 `DELEGATION_CONFIRMED` 事件**：確認委託時還沒有 task，事件鏈屬於 task，所以確認紀錄在 `delegations` 表；task 建立時寫 `DELEGATION_BOUND`。
- **拒絕事件也記錄方法**：`APPROVAL_DENIED` 與 `APPROVED` 一樣帶 `authorization_method=simulated`（誰、怎麼決定的）；到期、撤回、作廢沒有人決定，不帶。
- **期限**：核准會話的期限是「委託的核准秒數」與付款綁定的 `valid_before` 兩者較早的；模擬 grant 的到期就是 `valid_before`，不比付款意圖活得久。
- **沒有 challenge 欄位**：設計 3.2 的 WebAuthn／語音 challenge 留到 A2；P1-a 不預留可誤用的入口。
- **核准不改 task 狀態**：核准、拒絕、作廢與到期一樣只是關閉 Ask；task 狀態仍由事件投影（`awaiting_approval` → `running`）。請購與核准的狀態看各自的欄位。
- **Unity 沒有重建**：Unity 只認 M1 的 phase。頁面把模擬核准的結果送成「對它為真」的 M1 phase：已核准 → `allowed_unpaid`（留在櫃台，英文標籤「ALLOW - no payment capability」，不演取貨）、拒絕 → `denied`（帶 `APPROVER_DENIED` 離店）、作廢 → `withdrawn`。頁面自己的中文標籤寫真正發生的事。Unity 畫面左上「M1 / no payment」也是 Unity 自己的字。要有專屬的「已核准（模擬）」標籤需重建 Unity，本片沒做。
- **歷史模式**：記錄下來的 schema 2 歷史資料庫照舊唯讀開啟，不遷移。

## 4 fake 操作驗收步驟（Aaron）

1. `make present-p1a`（fake CLI，不執行模型；目錄 `testdata/demo/scenarios-p1a.json`）。瀏覽器打開後輸入終端機上的配對碼。頁首應是「P1-a／fake agent／模擬核准／不付款」。
2. 客人 A 的卡按「以 A 模擬（fake）」：頁首出現「角色模擬：客人 A（主持人扮演，不是本人驗證）」。
3. 按「建立委託」：看需求、商家範圍、預算 5.00 test USDC、「超過 0.10 一律要你核准」、核准期限 60 秒 → 「確認委託並開始（模擬）」。
4. Agent 跑完後，卡上寫「Agent 已結束；請購仍等你核准（模擬）」；右上「需要你處理」出現單筆摘要與倒數。按「核准此筆（模擬）」→ 「已核准（模擬），未啟用付款」與「Signer 樁：拒絕模擬授權」。「查看技術證據」可看 `APPROVED`、`GRANT_ISSUED`（simulated）。
5. 再「建立委託」一次，這次按「拒絕」→「已拒絕」，沒有授權。
6. 再一次，等 60 秒不按 → 「已過期，無法核准」。
7. 按 B 卡的「以 B 模擬」：A 的模擬失效，B 看不到 A 的任何一筆；B 自己跑一次。
8. 鍵盤：在「需要你處理」裡按 Tab 會先到「核准此筆（模擬）」再到「拒絕」，Enter 可以按下。

每一步都不會付款。結束按 Ctrl-C；場次目錄保留在 `var/present-*`。

## 5 定向驗證

（見第 5.1 節的執行紀錄。）

## 6 截圖

首個可操作版本（基準 `27055af`，fake）：`artifacts/p1a/p1a-ui-27055afbd0b6-20261012T070253Z/`（git 忽略，本機）。`p1a-1440x900-1-delegation.png`（主畫面與委託）、`-2-ask-summary.png`（核准摘要）、`-3-approved.png` 與 `p1a-390x844-3-approved.png`（核准後未啟用付款）。和 UX1 的差異：多了角色模擬標籤、委託步驟、「需要你處理」單筆摘要與結果；場景、商店美術與 Unity 都沿用 UX1，沒有新的場景美術。

## 7 沒有做的

- 沒有付款、金鑰、簽署、正式 Signer、一次性消耗紀錄（P1-b）；沒有 WebAuthn、語音、BBS+、ZK、Admin、影片或多場景引擎。
- 沒有執行真實 Claude CLI、版本或登入查詢，沒有讀個人設定或 keychain；Gate、認證與 CLI 釘選不變。
- 沒有重建 Unity、沒有跑 Unity 測試（Unity 沒有改）；沒有跑完整容器、全部突變或完整 M1 產包。
- 付款帳戶 ADR 的 Accepted 不授權上述任何外部執行。
