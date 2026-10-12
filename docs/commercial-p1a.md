# P1-a：委託與單筆模擬核准

> 2026-10-12，Claude Code。依 vault〈Claude-Code-P1-a委託與單筆模擬核准實作派工-b0700a7-20261012〉。分支 `commercial-p1a`，基準 `b0700a7`。
> **現行狀態（10/12）：P1-a 委託與單筆模擬核准已限定結案：既有階段性 fake 操作確認已記錄，c547ca4 的 R1～R3 限定技術複核通過。付款仍未啟用；P1-b 尚未開始。**（第 9 節。這是 P1-a 切片的限定結案，不是整體商業展示版完成，也不是正式核准、Signer、付款或交付的驗收。）
> 下列「狀態」是 R1～R3 交付時寫的，保留當時文字：
> **狀態（10/12，R1～R3 交付時）：P1-a R1～R3 補正完成，待 Codex 限定複核；Aaron 已提供階段性 fake 操作回饋。** 歷程：P1-a 實作與定向驗證到 `f95415d`（第 1–7 節，保留當時文字）；Codex 對 `f95415d` 的限定複核暫不通過，提出 R1～R3，本輪補正見第 8 節。本文的驗證結果都由 Claude Code 執行，待 Codex 核對。
> **Aaron 的回饋**（原話，本人操作與判定）：首版截圖「我實際看過截圖，目前是滿意的」；實際 fake 操作後「以實際操作過，暫時沒太大問題。」後者是階段性的 fake 體驗確認，不是逐項清單通過，也不取代技術複核。
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

## 5 定向驗證（Claude Code 執行，待 Codex 核對）

計畫 `scripts/verify/p1a-check-plan.json`，以既有進度工具 `go run ./cmd/m1-check --plan scripts/verify/p1a-check-plan.json --out artifacts/p1a` 執行：每步顯示步驟、耗時、最近輸出與 log 路徑，一步失敗後其餘不跑。證據在本機 `artifacts/p1a/`（git 忽略）。各次結果分開記，沒有拼成一次全過。所有瀏覽器與 host 測試只用 fake CLI；loopback 埠在 sandbox 外綁定。

| 範圍 | 對應風險 | 測試 |
|---|---|---|
| 契約與 Policy | 兩版本各自驗證與 digest、版本交叉誤用、付款綁定改變、Rules 的先後與 M1 輸出不變 | `internal/paycontract`（golden vectors、同欄位兩契約、綁定進 hash、裁決與事件規則）、`policy.TestRulesOrder`、`paypolicy.TestSameFieldsBothEntries`；既有 `policy`、`contracts` 測試不改 |
| Store | 遷移與失敗保護、重啟、Ask／會話／grant 一致、沒有半份核准 | `internal/store/pay_test.go`：核准一次與重送（含重啟後）、grant 後注入失敗整筆回滾、拒絕、別人的會話、意圖不符、期限前一秒與到期、核准與取消同時（10 輪）、報價改變作廢、版本邊界、委託防護、復原檢查拒絕竄改、schema 2→3、M1 證據拒絕 v1、v1 拒絕保留原代碼；既有 schema 1→3 升級測試 |
| 權限 | B 不能讀或核准 A；三種既有角色不能核准；live 拒絕模擬 | `cmd/demo/p1a_test.go`：`TestP1aPermissions`、`TestP1aLiveRefusesSimulation`（handler）、`TestP1aLiveHostHasNoSimulation`（live host，CLI 是測試自己的假程式） |
| 付款界線 | 模擬 grant 沒有付款能力；不產生付款或交付的假成功 | `signer.TestStubRefusesSimulated`（兩次都拒絕，只證明樁的規則）；主線與瀏覽器檢查確認沒有 SETTLED／DELIVERED |
| 網頁 | 正常、拒絕、到期、角色切換、鍵盤、晚到回覆 | `web/test/p1a.test.mjs`；`scripts/verify/p1a-ui.mjs`；UX1 相鄰流程 `ux1-ui.mjs`、`ux1-focus-ui.mjs` |
| M1 | 既有契約／Policy／Store 測試與證據包 | 受影響套件的既有測試；`verify-demo` 唯讀檢查兩份既有證據包，前後比對檔案雜湊，不重新產包 |

### 5.1 執行紀錄

1. **完整計畫，基準 `72e0941`**（`artifacts/p1a/m1-check-20261012T071807Z/`）：
   - go-targeted 295 個測試通過（58 s）；go-race 31 個（20 s）；go-race-demo 7 個（25 s）；vet；node 單元測試；M1 contract vectors。
   - M1 證據包：`m1-fixture-20261009T162703Z-4ddb7a`（fixture）與 `m1-live-20261010T173831Z-9d32ef`（live）都是 `verify-demo` PASS，81 與 8 個檔案的雜湊前後相同。
   - 瀏覽器：p1a-ui PASS（50 s）、ux1-ui PASS（1 m 42 s）、ux1-focus-ui PASS（28 s）。
   - reverse 步驟**沒有執行**：計畫執行中我改了受版控的文件與一個測試，reverse 在 setup 拒絕（「tracked tree has changes」），所以 0 項。
2. **只跑 reverse，基準 `ae85824`**（只多了文件與一個目錄測試，產品程式與 `72e0941` 相同；`m1-check-20261012T072330Z`）：17 項中 14 項被預期斷言抓到，3 項沒有：
   - 「task 存的版本選入口」：突變後仍被拒絕，但代碼是 `INVALID_PURCHASE`。原因是 Store 的 `contractRefusal` 只讀 M1 的錯誤代碼，`paycontract` 的代碼都被換成 `INVALID_PURCHASE`。這是產品的小錯（拒絕仍成立，只是代碼錯），已在 `560c354` 修正並補測試 `TestPayRefusalKeepsItsCode`。
   - 「委託不寬於 profile」：我的突變寫法編譯失敗（變數未使用），不算抓到；已改正寫法。
   - 「付款綁定在 hash 裡」：我的突變用了空的綁定，金額無法序列化，測試先崩潰，不算抓到；已改正寫法。
3. **修正後只重跑受影響部分，基準 `560c354`**（`m1-check-20261012T072740Z`）：
   - go-targeted 297 個通過（55 s）；go-race 32 個（16 s）；go-race-demo 8 個（24 s）；vet。
   - reverse 在第 17 項（瀏覽器）時，Claude Code 的工作階段結束，被中斷（`INTERRUPTED`）。中斷前完成的 16 項全部被預期斷言抓到，包含上一輪沒抓到的 3 項。
4. **補跑第 17 項，基準 `560c354`**（`m1-check-20261012T100102Z`）：「建立委託要先以該客人模擬」被 p1a-ui 的預期檢查抓到。

合計：在 `560c354` 上，17 項核心防護都各自被預期斷言抓到（第 3、4 兩次執行合起來；第 3 次是中斷的執行，只計它完成的 16 項）。`560c354` 只改了 Store 的拒絕代碼對應，所以瀏覽器流程與 M1 證據包沒有在它上面重跑，結果沿用第 1 次（`72e0941`）。

**沒有跑的**：
- 完整容器、全部突變、完整 M1 產包、真實展示；
- Unity 測試與建置（Unity 沒有改）；
- `make present-p1a` 本身的配對流程。目錄由 `TestP1aShippedCatalog` 檢查可載入；配對入口與 UX1 相同，是既有的 present-ui 範圍，本片沒有重跑。

**既有問題，不屬本片**：`cmd/demo` 的 `TestTimingInterleaved` 在本機每次都失敗，在基準 `b0700a7` 的乾淨工作樹上同樣失敗（本輪另以 git worktree 檢出 `b0700a7` 單獨重跑），所以計畫用 `-skip` 排除它，沒有修改。

**瀏覽器裡的 404**：頁面在新 task 的 Gate 通過前就去讀它，得到 404，之後重試成功（UX1 既有行為）；另有 `/favicon.ico`。這兩種都不是錯誤。

## 6 截圖

首個可操作版本（基準 `27055af`，fake；Aaron 看過後回覆「我實際看過截圖，目前是滿意的」）：`artifacts/p1a/p1a-ui-27055afbd0b6-20261012T070253Z/`（git 忽略，本機）。`p1a-1440x900-1-delegation.png`（主畫面與委託）、`-2-ask-summary.png`（核准摘要）、`-3-approved.png` 與 `p1a-390x844-3-approved.png`（核准後未啟用付款）。和 UX1 的差異：多了角色模擬標籤、委託步驟、「需要你處理」單筆摘要與結果；場景、商店美術與 Unity 都沿用 UX1，沒有新的場景美術。

## 7 沒有做的

- 沒有付款、金鑰、簽署、正式 Signer、一次性消耗紀錄（P1-b）；沒有 WebAuthn、語音、BBS+、ZK、Admin、影片或多場景引擎。
- 沒有執行真實 Claude CLI、版本或登入查詢，沒有讀個人設定或 keychain；Gate、認證與 CLI 釘選不變。
- 沒有重建 Unity、沒有跑 Unity 測試（Unity 沒有改）；沒有跑完整容器、全部突變或完整 M1 產包。
- 付款帳戶 ADR 的 Accepted 不授權上述任何外部執行。

## 8 Codex 限定複核 R1～R3 補正（基準 `f95415d`）

Codex 前輪核對的是原始碼、文件，以及部分驗證摘要與反向輸出。Docker daemon 沒有啟動，所以 Codex 沒有獨立執行測試、瀏覽器或資料庫竄改重現。以下都是 Claude Code 執行，待 Codex 核對。`560c354` 的反向檢查仍照第 5.1 節記錄：前 16 項完成後中斷，第 17 項另行補跑；這兩次執行的歸屬分開，沒有改寫成單輪 17 項全過。

| 項目 | 原問題 | 修正 | 位置 |
|---|---|---|---|
| R1 核准焦點 | 面板重畫時只依按鈕的 class 恢復焦點；如果待處理的那一筆換成另一筆，焦點會落到另一筆的核准或拒絕按鈕上 | 核准、拒絕、證據按鈕都帶著憑證世代、模擬能力、consumer、task、會話與 `intent_hash`（`adapter.approvalTarget`）。重畫後，焦點只回到「同一筆、同一能力、同一世代」的同一個按鈕（`sameTarget`）；送出類按鈕還要求該會話仍是 pending。其他情況焦點交給「需要你處理」標題，標題不會送出任何操作。期限到時倒數會停用按鈕，若該按鈕正有焦點，先把焦點交給標題。不自動點擊；使用者已把焦點移開時，不會搶回 | `web/host.js`、`web/adapter.js` |
| R2 模擬身分與重送 | `once` 只核對憑證世代，切換模擬客人不會改變世代，所以網路錯誤後的重送會取用當下的能力。後端仍有 consumer 檢查，這不是已證明的跨客人核准漏洞，而是前端操作的身分與結果歸屬不完整 | 委託確認與核准／拒絕這兩種操作，從第一次請求到重送結束都固定在開始時的那一份能力（以物件本身比對，A→B→A 也算更換）。等待前後、例外後都檢查。能力一旦更換就不再送出，操作回傳 `superseded`；舊操作晚到的成功、錯誤或沒有回覆，都不套用到新客人的面板，也不移動焦點；被取代的委託確認不會開始場次。頁面只在狀態列說明「結果未確認，可能已被後端記錄」，並以目前身分重新讀取。同一能力內的重送仍用原 request ID、原內容，次數上限不變 | `web/hostcreds.js`、`web/host.js` |
| R3 復原與到期的期限 | 會話建立時取兩者較早的期限是對的，但復原檢查沒有重新核對 `expires_at`，到期處理也直接用這個欄位；grant 的時間也沒有和會話、付款綁定完整比對 | 會話期限一律由已驗證的決策時間、terms 的核准秒數和 `valid_before` 重算（`sessionDeadline`）。復原檢查要求存的值與重算值相同，到期處理也用重算值。grant 的到期必須等於 `valid_before`；發出時間必須等於會話的關閉時間、grants 列的 `issued_at` 與 `GRANT_ISSUED` 的時間；核准者必須等於會話的 `resolved_by`；body 必須是 canonical。發 grant 時到期直接取 `valid_before`（核准時一定早於它）。不加外部錨點或簽章，也不修改歷史資料 | `internal/store/pay.go`、`ops.go` |

### 8.1 修正前的定向結果（R3）

基準 `f95415d`，先寫 `TestPayRecoveryDeadlines`，竄改只做在測試自己建立的資料庫上：
- 兩個合法檔案（pending、approved）照常重新開啟。
- 六種竄改全部被接受：pending 會話的期限往後或往前、approved 會話的期限往後、grant body 的到期或發出時間、grants 列的 `issued_at`。

紀錄在 `artifacts/p1a/r3-before-fix/go-test.txt`（本機，git 忽略；`go test -run TestPayRecoveryDeadlines -v`，exit 1）。修正後六種竄改全部拒絕，合法檔案照常開啟。另外新增 `TestPayExpiryIgnoresStoredDeadline`：Store 開著的時候把檔案裡的期限往後改，到了重算的期限仍然到期。

### 8.2 本輪驗證（基準 `c547ca4`）

計畫 `scripts/verify/p1a-r-check-plan.json`，用 `go run ./cmd/m1-check --plan scripts/verify/p1a-r-check-plan.json --out artifacts/p1a` 執行，結果在 `artifacts/p1a/m1-check-20261012T104237Z/`。整個 run exit 0，7 步都通過，同一次執行完成：

| 步驟 | 結果 | 耗時 |
|---|---|---|
| go-store（`internal/store`、`internal/paycontract` 全部，含 M1 的 Store 測試，因為到期規則是共用路徑） | 79 個通過 | 13 s |
| go-race-store（`-race`，Pay／Upgrade／Expir／Recovery／Cancel） | 27 個通過 | 50 s |
| go-demo-p1a（`cmd/demo` 的 `TestP1a*`） | 8 個通過 | 4 s |
| node（四個 web 單元測試檔，含 R1、R2 新測試：throw 與 timeout 兩條路徑、A→B、A→B→A、同能力重送沿用同一 request ID、晚到的成功與錯誤、`sameTarget`） | 0 fail | 6 s |
| p1a-ui（原流程，加上 R1：兩筆待核准時，焦點在較新那筆的核准鍵上，那筆到期後焦點到標題，真正按 Enter 後另一筆仍是 pending；焦點在證據鍵上時，重畫後同樣到標題，再按 Enter 仍不核准） | PASS | 1 m 7 s |
| ux1-focus-ui（UX1 焦點相鄰回歸） | PASS | 25 s |
| reverse（`^P1a(-R:\|: web: a reply)`：本輪 6 項新防護，加上 1 項因程式改寫而更新錨點的既有項目） | 7／7 被預期斷言抓到 | 2 m 34 s |

timeout 路徑在單元測試裡是用「以 `TimeoutError` 拒絕」模擬的，走的是和真正逾時同一個 catch，不是等 8 秒的真實逾時。

**本輪沒有跑**：完整 P1-a 計畫、其他瀏覽器流程（ux1-ui、dual、browser-ui 等）、M1 證據包與產包、完整容器、全部突變、Unity。理由是本輪只改了核准面板、hostcreds 和 Store 的 v1 期限；M1 的 Store 測試已包含在 go-store 裡。

### 8.3 暫不擴大的項目

- **Unity**：本輪不改、不重建。頁面保留中文「已核准（模擬），未啟用付款」。場景裡沿用 M1 的 `ALLOW - no payment capability` 標籤，這只是呈現限制，不能當成 Policy 改判 Allow 或正式核准的證據；專屬文字留到後續呈現修正。
- **`TestTimingInterleaved`**：另列為既有失敗追蹤。它在 `b0700a7` 乾淨的 worktree 上同樣失敗（Claude Code 的核對，Codex 尚未獨立確認）。計畫裡用 `-skip` 排除，skip 不算通過，本輪也不擴修。
- 沒有執行真實 CLI、版本或登入查詢；沒有金鑰、正式 Signer、WebAuthn、交易、付款或交付。P1-b 未開工。

## 9 限定驗收與結案（10/12）

**技術結論**（Codex，原文）：

> c547ca4 通過本次限定技術複核，R1～R3 可以關閉。本輪未發現這三項範圍內的新阻擋。

關閉理由：
- **R1**：焦點綁定同一份模擬能力、consumer、task、核准會話與 intent；換筆後不把焦點交給另一筆的核准鍵。瀏覽器紀錄確認退到標題後按 Enter，沒有核准另一筆。
- **R2**：整個重送操作固定在開始時的模擬能力，A→B→A 也視為更換，不用新能力補送舊操作；舊結果與新核准面板分開處理，被取代的委託確認不開始場次。
- **R3**：會話期限由決策時間、terms 與 `valid_before` 重算，復原與到期處理都用這條規則；grant 的時間與核准者和相關資料核對。修正前六種期限竄改被接受、修正後被拒絕的紀錄相符。

**Aaron 的操作回饋**（原話）：

> 以實際操作過，暫時沒太大問題。

這是 Aaron 本人的階段性 fake 體驗確認，不是全部操作項目逐項通過，也不是在 c547ca4 上重新操作的結果。首版截圖回饋（「我實際看過截圖，目前是滿意的」，基準 `27055af`）維持原來的歸屬。

**結案狀態**：P1-a 委託與單筆模擬核准已限定結案：既有階段性 fake 操作確認已記錄，c547ca4 的 R1～R3 限定技術複核通過。付款仍未啟用；P1-b 尚未開始。P1-a 切片標為 Done，只限上述範圍；整體商業展示版、正式本人核准、Signer、付款與交付都不因此結案。

**驗收基準**：產品與測試是 `c547ca49a73072b3a84022ced35be4b9d342c8b6`；其後 `d589e6e`、`160a68a` 只改文件，Codex 已核對。

**證據歸屬**（分開記錄）：
- **Claude Code 執行、Codex 核對紀錄**：
  - `artifacts/p1a/m1-check-20261012T104237Z/`：七步 PASS、相關定向測試，以及七項反向檢查（輸出命中預期斷言）。
  - R3 修正前的紀錄 `artifacts/p1a/r3-before-fix/go-test.txt`。
- **Codex 本輪**：唯讀核對程式、測試、驗證摘要與相關輸出、反向定義及 Git 差異，確認 c547ca4 之後只有文件修改；這些核對輸出在 Codex 的對話工具紀錄中，repo 沒有對應的 log 路徑。Codex 沒有獨立重跑測試、瀏覽器或容器。當時 Docker daemon 不可用，也沒有為此啟動；沒有執行真實 CLI、讀取登入資料或修改產品。
- **Aaron**：本人操作與階段性體驗判定。
- **早期驗證**：維持第 5、8 節的原有歸屬。`560c354` 的反向檢查是前 16 項完成後中斷、第 17 項另跑，不合寫成單輪全過；歷史上的失敗、修正與中斷紀錄全部保留。

**保留的限制**：
- 本片只到模擬核准與 simulated grant。付款能力仍是 `not_enabled`，不是正式本人驗證，不是可用的付款授權，也沒有付款或交付。
- Signer 樁拒絕 simulated grant，只證明樁的規則；正式 Signer、金鑰保管與一次性付款消耗都沒有驗收。
- Unity 沒有重建。既有的 `ALLOW - no payment capability` 標籤是呈現限制，不代表 Policy 改判 Allow 或正式核准；專屬呈現留到後續。
- R2 的 timeout 測試是注入 `TimeoutError`，不是實際等待 8 秒。
- `TestTimingInterleaved` 的基準失敗由 Claude Code 核對，Codex 尚未獨立確認；另列追蹤，skip 不算通過。
- 這次限定結案沒有完整容器、全部突變、M1 重新產包、Unity 重建或真實 CLI 驗證；舊紀錄不代表新基準跑過全套。
- EOA＋EIP-3009 ADR 的 Accepted 不等於核准建立金鑰、簽署、faucet、測試網或任何交易。

**下一步**：P1-a 已限定結案；P1-b 金鑰庫與獨立 Signer 待另行派工與核准。

本節只同步文件；沒有重跑測試、建置、展示、版本查詢或 CLI。

