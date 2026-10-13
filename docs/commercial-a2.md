# A2 本人核准產品接線：實作差異、驗證與限制

> 2026-10-13，Claude Code。依 vault〈Claude-Code-A2本人核准產品接線實作派工-f2bf224-20261013〉（Aaron 已批准「A2 開工」）。分支 `commercial-a2`，基準 `f2bf224`（工作區只有未追蹤的 `demo`；沒有適用的 AGENTS.md）。
> **狀態：A2 已實作並完成本輪定向自動化驗證；獨立上下文 review 判定 not pass（第 6 節）；R1～R6 已修正（第 7 節）；第二次 review Signer pass、Host 窄幅 not pass，S1、S2 已修正（第 8 節）；第三次 review 窄幅 not pass，A、B 已修正（第 9 節）；第四次 review 窄幅 not pass，S-B、S-A 已修正（第 10 節），待第五次獨立上下文 review；實機登記與操作未驗收，付款未啟用。**
> 以下為 `f0817c7` 時的狀態，保留原文：A2 已實作並完成本輪定向自動化驗證，待 Codex 技術 review；實機登記與操作未驗收，付款未啟用。A2 不自行標 Done；本輪所有結果都是「Claude Code 執行，待 Codex 核對」，不是實機結果。
>
> **歸屬的變更**：Codex 訂閱已到期，從這一節起沒有第三方核對。Review 改由 Claude Code 另開的獨立上下文 subagent 執行：它們只拿到 diff、設計文件與本文件，沒有作者寫程式時的推理；這比作者自審接近獨立核對，但仍是同一個模型。紀錄一律寫「Claude Code 獨立上下文 review」，不寫成 Codex。

規則以 [commercial-human-approval-design.md](commercial-human-approval-design.md) 為準（第 2～9 節，第 13～16 節的修訂，第 17 節的決策，第 22 節 A1 結案）。本文件只記：實作了什麼、和設計哪裡不同、驗證的證據、還沒做的事，以及實機與部署清單。

## 1 交付

| 層 | 內容 | 位置 | 提交 |
|---|---|---|---|
| 依賴 | `go-webauthn/webauthn v0.18.2`（D4），只有 Signer 使用。新增的間接模組：`fxamacker/cbor/v2`、`go-viper/mapstructure/v2`、`go-webauthn/x`、`golang-jwt/jwt/v5`、`google/go-tpm`、`philhofer/fwd`、`tinylib/msgp`、`x448/float16`。既有釘選沒有改（`x/crypto v0.57.0`、`x/sys v0.48.0`、`google/uuid v1.6.0`） | `go.mod`、`go.sum` | `c2719db`（單獨提交） |
| Signer | 設定的 `approvers` 與 `webauthn` 段落；`approvals.db`；E1 登記；challenge、verify、close、三種查詢、abandon；`serve-approvals` 的路由 | `internal/signer/approvals.go`、`enroll.go`、`server.go`、`config.go` | `2fcc2cd` |
| 命令 | `apa-signer serve-approvals`、`approver enroll begin｜confirm`、`approver revoke`、`approver list` | `cmd/apa-signer/approvals.go` | `2fcc2cd` |
| 契約 | APPROVED／APPROVAL_DENIED／GRANT_ISSUED 可以是 webauthn，並帶 `signer_approval_id`；`WEBAUTHN_APPROVAL`；webauthn grant 是 Signer 決定的鏡像（`Recorder`）；M1 一律拒絕新欄位（schema 同步） | `internal/paycontract`、`internal/contracts/events.go`、`schemas/m1.schema.json` | `a3392a8` |
| Store | schema 5：`grants` 重建（webauthn 必有 `signer_approval_id`）、`approval_attempts`；attempt 的開始、共用與衝突；Signer 結果的落地；恢復檢查；`--upgrade-db` 1～4 → 5 | `internal/store/approvals.go`、`store.go`、`pay.go` | `9a76073` |
| Host | `--approvals-signer`、`--authenticator-source`；Host／Origin 的精確清單；核准路由；登記中繼與 `/approve/enroll`；主持人狀態 | `cmd/demo/approvals.go`、`api.go`、`main.go`、`status.go` | `d88573a` |
| 頁面 | 「需要你處理」的本人核准面板；登記頁；依核准方式切換的文字與頁首 | `web/host.js`、`web/approve.js`、`web/enroll.*`、`web/adapter.js`、`web/app.js` | `7595f01`、`71b094b`、`9d3c2aa` |
| 入口 | `make present-a2 SIGNER=<socket>`：P1-a 的目錄、固定 `127.0.0.1:8402`、打開 `http://localhost:8402/host` | `Makefile`、`scripts/demo/present.sh` | `a8f1c52` |
| 驗證工具 | 計畫 `a2-check-plan.json`；反向項目 `^A2:`（11 項）；瀏覽器 `a2-ui.mjs` | `scripts/verify/` | `8a90a4d`、`1f9d966`、`70c9dc5` |

## 2 與設計的差異（review 時請特別看）

| # | 設計 | 實作 | 理由 |
|---|---|---|---|
| 1 | `GET /v1/enrollments/{id}?code=` | `POST /v1/enrollments/{id}/options`，確認碼放在 body；Host 端同樣是 POST | 確認碼不進任何 URL、瀏覽器歷史或紀錄 |
| 2 | 錯誤碼清單（第 6.1 節） | 另外新增：<br>`SESSION_MISMATCH`：會話 ID 必須等於 `SessionID(task, purchase_order_id)`；<br>`SESSION_DECIDED`：一個會話永遠只有一個決定；<br>`CHALLENGE_OPEN`：同一會話已有 issued 的 challenge；<br>`CREDENTIAL_UNKNOWN`／`CREDENTIAL_SUSPECT`／`CREDENTIAL_EXISTS`；<br>`ATTESTATION_INVALID`／`ATTESTATION_FORMAT`；<br>`ENROLLMENT_UNKNOWN`／`ENROLLMENT_CLOSED`；<br>`VERIFY_UNKNOWN`／`APPROVAL_UNKNOWN`／`APPROVAL_NOT_VERIFIED`；<br>verify 時的 `INTENT_MISMATCH` | 把設計已有的規則落成可查的固定代碼 |
| 3 | `display` 含委託範圍 | Signer 的 `display` 只有請購與它自己設定裡的資訊；委託範圍（預算、`allow_max`）由頁面從 Host 的 Store 顯示 | Signer 沒有 Host 的委託資料；頁面本來就不是可信顯示（第 4.7 節） |
| 4 | `signer_max` | 設定裡驗格式，A2 不據以拒絕 | 付款上限是 P2 的事 |
| 5 | 同一會話最多一筆非終態 attempt | 「refused 但仍有 challenge」也算未結束，要先 close 才能另開 | 符合第 7 節：Host 的 refused 不代表 Signer 的 challenge 已失效 |
| 6 | 核准 grant 的期限 | `min(Signer 核准的期限, valid_before)`；恢復檢查只要求不晚於 `valid_before` | webauthn grant 是 Signer 決定的鏡像 |
| 7 | — | Host 的核准操作用一把全域鎖串行（`ponytail:` 註解） | 展示用的 Host 同時只會有少量操作；Signer 的條件式寫入仍是最終依據 |
| 8 | — | 委託確認仍是 P1-a 的模擬（「以 A 模擬」）；只有單筆核准是本人裝置 | A2 範圍只接單筆核准；頁首與卡片照實寫委託確認是模擬 |
| 9 | 驗證器來源標示 | `--authenticator-source device｜virtual` 由主持人宣告 | 頁面與 Host 都分不出實機或 virtual；這只是標籤，不是證據 |
| 10 | 合法重送 | 已 verified 的 attempt 以同一 verify request ID 重送時，Host 先向 Signer 查回同一筆，再走 Store 的重送路徑 | 不重新驗證、不比計數器（第 4.4 節第 5 點） |
| 11 | 第 8 節面板列「已過期（後端時間為準）；沒有核准。」 | A2 面板的 expired、withdrawn、superseded 標題不寫「沒有核准」或「核准作廢」，結果由面板訊息說明（第 8、9 節，review N-b、C） | 嘗試的結果可能未知；設計第 7、8 節規定未知不寫成「未核准」，標題與訊息不得互相矛盾 |

另外：
- `serve-approvals` 不讀 `keys/`、不問密語、不開 `signer.db`，付款與 attempts 路由一律回 409 `PAYMENT_MODE_OFF`。
- `serve` 完全沒有改：缺 key 仍拒絕啟動，`webauthn` 的付款請求仍回 `HUMAN_APPROVAL_UNAVAILABLE`。

## 3 本輪驗證（Claude Code 執行，待 Codex 核對；都不是實機結果）

**環境**：
- 本機 macOS，`GOPROXY=off`、`GOFLAGS=-mod=readonly`，依賴只來自本機模組快取。
- 這輪**沒有在隔離容器裡執行**：Docker daemon 沒有啟動，而工具沙箱不能開啟應用程式。
- 測試只用合成資料、軟體 authenticator 與 CDP virtual authenticator；Chrome 每次用新的測試 profile。
- 沒有使用個人瀏覽器 profile、keychain 或真實 credential。

### 3.1 m1-check 計畫（`scripts/verify/a2-check-plan.json`）

基準 `1f9d966`，tracked tree clean。證據在 `artifacts/human-approval/m1-check-20261013T103010Z/`，摘要與進度另存為 `plan-1f9d966.summary.json`、`plan-1f9d966.progress.txt`。整輪 exit 0、233.6 秒、`evidence_complete: true`、verdict PASS。

| 步驟 | 命令（摘要） | exit | 耗時 | 結果 |
|---|---|---|---|---|
| vet | `go vet ./...` | 0 | 3.2 s | PASS |
| signer-a2-race | Signer 與 `apa-signer` 的 A2 測試（`-race`） | 0 | 10.2 s | 15 個測試通過 |
| contract | `./internal/paycontract/`、`./internal/contracts/...` | 0 | 2.1 s | 37 個 |
| store-a2-race | Store 的 attempt、schema、升級與恢復測試（`-race`） | 0 | 30.7 s | 9 個 |
| host-a2-race | `./cmd/demo/ -run ^TestA2`（`-race`；真 Host、真 Store，真 Signer handler 在測試 socket 上） | 0 | 51.0 s | 11 個 |
| adjacent-store | `./internal/store/`、`./internal/paypolicy/`、`./internal/m1evidence/` | 0 | 15.7 s | 88 個 |
| adjacent-signer-p1b | `./internal/signer/...`、`./internal/signerclient/`、`./cmd/apa-signer/`（P1-b 全部） | 0 | 6.6 s | 51 個 |
| adjacent-demo | `./cmd/demo/ -skip ^TestTimingInterleaved$` | 0 | 51.3 s | 83 個 |
| web | 五個 `node --test` 檔（含 `approve.test.mjs`） | 0 | 5.7 s | PASS |
| m1-golden-readonly | `verify-demo` 唯讀檢查 `testdata/verification/m1-fixture-20261009T162703Z-4ddb7a` | 0 | 2.0 s | PASS。證據包前後的檔案雜湊合計相同：`c02e1dd9…6bf980` |
| reverse | `m-b-reverse.py '^A2:'` | 0 | 54.5 s | 11／11 項由預期斷言抓到 |

**反向第 4 項的補正**：
- **原紀錄**：基準 `1f9d966` 時，第 4 項（重放）雖然以 `replay:` 斷言抓到，實際訊息卻是 `CREDENTIAL_REVOKED`：測試在檢查重放之前就撤銷了 credential。這筆照實保留。
- **補正**：`70c9dc5` 把重放檢查移到撤銷之前，並只補跑這一項（`artifacts/human-approval/reverse-item4-70c9dc5/`）。結果是抓到，訊息為 `replay: … UNIQUE constraint failed: human_approvals.jti`。
- **這說明**：拿掉兩道 used 檢查之後，還有 jti 唯一性這一層擋住重放。
- 兩次不合寫成同一輪。

### 3.2 瀏覽器（B 層，`scripts/verify/a2-ui.mjs`）

| 基準 | 結果 | 證據 |
|---|---|---|
| `1f9d966` | **FAIL**：13 項失敗，exit 1，184 s。根因是腳本在 `enroll.js`（module，HTML 解析完才執行）掛上處理函式之前就按了登記；CSP 的 `form-action 'none'` 讓表單沒有送出，所以 A 沒有登記，後面的檢查連鎖失敗。原始紀錄保留 | `artifacts/human-approval/a2-ui-1f9d9668a2fc-20261013T103423Z/` |
| `9d3c2aa` | 修正（頁面在處理函式掛上後標 `data-ready`，腳本等它）後只補跑這一層：**ALL PASS**，35 項，exit 0，47 s | `artifacts/human-approval/a2-ui-9d3c2aa06d37-20261013T103803Z/` |

`9d3c2aa` 一輪涵蓋的範圍：
- E1 登記：終端機 begin，`/approve/enroll` 建立 credential，終端機 confirm（在虛擬終端機回答 yes），兩邊指紋一致；
- `127.0.0.1` 頁面不提供裝置核准，提示改從 localhost 開啟；
- localhost 頁首寫 A2；
- 核准：裝置只被要求一次，Store 有 webauthn grant，付款未啟用，焦點回到標題；
- 鍵盤拒絕：Tab 先到核准、再到拒絕，按 Enter；
- 另一個視窗正在核准時，這頁按拒絕得到 409，裝置沒被要求；
- 「取消這次驗證」之後，拒絕成為新的 attempt；
- Signer 停掉：顯示「結果確認中」，輪詢期間裝置不會被要求；Signer 回來後以同一組 ID 恢復，仍不自動彈出；之後繼續同一筆 attempt 並完成核准；
- 保留 B 的 challenge 回覆期間切換到 A：B 的晚到回覆不要求裝置，也不在 A 的面板上顯示；
- 卡片與場景的文字不寫「模擬」；
- 每張截圖拍攝時，token、配對碼與確認碼欄位都是空的。

**截圖**（`9d3c2aa`）：
- `a2-1440x900-4-main.png`：主畫面；
- `a2-1440x900-1-ask-summary.png`：核准摘要；
- `a2-1440x900-2-approved-unpaid.png`：成功、付款未啟用；
- `a2-1440x900-3-result-unknown.png`：結果未知；
- `a2-1440x900-0-enrolled.png`：登記完成。

### 3.3 其他

**`TestTimingInterleaved`**：
- 在 A2 改動之前的基準 `f2bf224` 上同樣失敗：以 `git worktree` 單獨跑這一項，FAIL，`multirun_test.go:325`。
- 這是既有的追蹤項，這輪依指示沒有擴修，計畫中以 `-skip` 照名跳過。**跳過不是通過。**

**A2 入口的實測**：用測試 Signer、虛擬終端機，並用 `true` 取代開啟瀏覽器。
- Host 印出 `human approval (A2) on http://localhost:<port> … (signer approvals-only) … payment disabled`；
- `present.sh` 回報 `就緒 http://localhost:<port>/host`；
- `approvals.db` 與 `signer.json` 都是 0600。

## 4 沒有做的

- **容器**：沒有在隔離容器中執行（Docker daemon 未啟動）。要補跑時，Docker 開著的情況下執行：`APA_VERIFY_IMAGE=sha256:78258368… sh scripts/verify/container-plan.sh scripts/verify/a2-check-plan.json <新目錄>`。
- **其他驗證**：沒有跑完整容器七步、所有歷史突變、完整 M1 產包、所有瀏覽器套件，也沒有重建 Unity。
- **實機與部署**：實機登記與操作（M 層）都沒有做，也沒有部署到 `/opt/apa-signer`：沒有 sudo、沒有改系統帳號或群組。
- **不在範圍**：P2、D8 的資料庫整合、語音、ZK、憑證、Admin、真實 Agent、付款金鑰、簽署、連鏈、交易，都沒有做。
- **A1**：沒有搬用 A1 的 credential，也沒有清理 A1 的暫存或證據。

**已知限制**：
- 頁面由 Host 提供，不是可信交易顯示（第 4.7 節）。
- E1 登記期間信任 Host 與其頁面（D2，已接受的風險）。
- 同步 passkey 的私鑰可能存在雲端帳號（D6）。
- 同一把 credential 並行核准兩筆時，可能觸發計數器政策（D6；這版逐筆操作）。

## 5 實機與部署清單（不執行；review 通過、Aaron 確認後才做）

**基準**：Codex review 通過的提交（本輪最後一個程式與測試提交是 `70c9dc5`，只改了測試；之後只有文件）。

**組合**：fake Agent＋合成請購＋Chrome／Google 密碼管理工具 PIN＋付款 disabled。
- 由 Aaron 代操作 A／B 時標「展示角色」，不宣稱是兩位自然人。
- A2 要重新登記，不沿用 A1 的測試 passkey。
- Touch ID、Safari、手機、安全金鑰都不是前置條件。

**部署差異**：對 P1-b 已建立的 `/opt/apa-signer` 只做增量，不重開 P1-b 的全套 OS 隔離驗收。
1. 以 review 過的提交建置 `apa-signer`，放到 `/opt/apa-signer/bin/apa-signer`（root、0755；需要 sudo，由 Aaron 執行）。
2. 在 `/opt/apa-signer/data/signer.json`（apa-signer、0600）加入以下段落：
   - `approvers`：approver-a 對應 consumer-a／wallet-a，approver-b 對應 consumer-b／wallet-b，`version` 1；
   - `webauthn`：`rp_id` 為 `localhost`，`origins` 為 `["http://localhost:8402"]`，期限 120／300 秒；
   - `wallets`：只放展示 registry 的公開地址（`0x1111…`、`0x2222…`）；
   - `callers`：Host 的 uid。

   **不建立付款金鑰**；`keys/` 與 `signer.db` 都不需要。
3. 啟動：`sudo -u apa-signer /opt/apa-signer/bin/apa-signer serve-approvals --dir /opt/apa-signer/data`。它會建立 `approvals.db`（0600）。socket 沿用 P1-b 的 `run/` 目錄與群組。
4. 增量核對：
   - `signer.json`、`approvals.db` 的擁有者與 0600；
   - socket 的權限；
   - `keys/` 不存在，或沒有被讀取；
   - `signer.db` 不存在，或沒有改變；
   - health 回 `approvals-only`。

**操作**：
1. `make present-a2 SIGNER=/opt/apa-signer/run/<socket>`，打開的是 `http://localhost:8402/host`。
2. 登記：
   - 終端機執行 `sudo -u apa-signer … approver enroll begin --dir /opt/apa-signer/data --approver approver-a`，記下登記 ID 與確認碼；
   - Chrome 打開 `http://localhost:8402/approve/enroll`，選 Google 密碼管理工具，用 PIN 完成；
   - 終端機執行 `… approver enroll confirm --id …`，核對兩邊的指紋後輸入 `yes`。
   - 這一步會在 Google 密碼管理工具新增一把 `localhost` 的 passkey。
3. 單筆核准：
   - 在頁面配對；
   - 「以 A 模擬」確認委託並開始（委託確認仍是模擬）；
   - 對 Ask 按「用已登記的裝置核准」，用 PIN 完成；
   - 預期看到「本人核准已驗證，付款未啟用。」。
4. 單筆拒絕：再開一場，按「用裝置拒絕這筆」，預期看到「你已拒絕這筆（已驗證裝置）。」。

**停止與清理界線**：
- Ctrl-C 停止 Host 與 `serve-approvals`。
- `approvals.db` 保留作為紀錄。
- 裝置端 passkey 是否清理，由 Aaron 辨認後自行決定，不自動刪除。
- 不建立、讀取或解鎖付款金鑰，不簽署，不送交易。

## 6 獨立上下文 review（10/13；Claude Code subagent，不是 Codex）

兩個 subagent 同時審 `f2bf224..f0817c7`，各審一半，只拿到 diff、設計文件與本文件。
- Signer／契約（security-auditor）：另以 `go test -overlay` 寫了 5 個審查用測試，不碰 repo；其中 2 個重現缺陷。
- Store／Host／web／工具／證據（code-reviewer）：Host 整合測試 11 項在沙箱裡無法綁 loopback，只以第 3.1 節的證據為準。

**判定：not pass。** R1～R6 須關閉後才交實機登記／驗證；其餘檢查項目與第 2 節的十項差異通過（#5、#10 判為「其實不是差異」，#8 需改）。

### 6.1 要修的（R）

| # | 嚴重度 | 位置 | 問題 | 失敗情境 |
|---|---|---|---|---|
| R1 | should-fix | `internal/signer/approvals.go` `commit` | 計數器比對在「issued→used」條件式更新之前 | 同一 challenge 兩份 assertion 在途（計數器非 0）：後提交的那份被判 `ASSERTION_COUNTER`，credential 被標 `suspect`；應得 `CHALLENGE_USED`、credential 仍 active。reviewer 以 overlay 測試重現 |
| R2 | should-fix | `internal/signer/approvals.go` `check` | BE 比對在簽章驗證之前，且結果會標 `suspect` | 只要知道 credential ID（challenge 回覆就有），用垃圾簽章和翻轉的 BE 旗標就能讓本人的 credential 失效，不需要私鑰。reviewer 以 overlay 測試重現。函式庫在簽章之後自己會檢查 BE |
| R3 | should-fix | `internal/store/approvals.go` `BeginApprovalAttempt`；`cmd/demo/approvals.go` `assertion` | 會話已 `withdrawn`／`superseded` 但仍有 open attempt 時，challenge 路由先回 shared／replay 才查會話；assertion 路由不看會話 | 主持人取消 task 後，viewer 仍能拿到 challenge、裝置仍被要求、Signer 仍寫一筆 verified→abandoned。結果不會落到已關閉的會話，但違反 6.3「會話仍是 pending」 |
| R4 | should-fix | `web/adapter.js`；`cmd/demo/sim.go` | 頁面用詞依 Host 的核准方式（全域旗標），不依該筆紀錄的 `authorization_method`；A2 模式下 P1-a 的 `simResolve` 仍能把 Ask 以 simulated 核准 | A2 模式下用模擬身分核准 → 紀錄是 simulated（CHECK 擋得住升權），畫面卻寫「本人核准已驗證」；違反第 8 節三個來源分開 |
| R5 | should-fix | `web/adapter.js` 抽屜的授權列與事件列 | 任何 grant 都寫「模擬 grant」 | A2 的 webauthn grant 在抽屜被寫成模擬；`a2-ui.mjs` 只檢查卡片與面板，沒查抽屜 |
| R6 | should-fix（測試缺口） | `cmd/demo/approvals_test.go` | `resolveUsed`（`CHALLENGE_USED`→查 challenge→查核准；close 回 `used`）在 Host 層沒有測試 | 設計第 9 節「兩個分頁各送一份 assertion」「查無後 verify 先成功」「close 回覆遺失」三列沒有 I 層證據 |

### 6.2 備註（note；修 R 時順手處理或列入下一輪）

Signer：
- N1 `commit` 的 `validBefore` 從 `display` JSON 取，解析失敗會寫出 `expires_at=0`（fail-open 方向）；改從 verify 請求的 order 取。
- N2 `TestAssertionRules` 的 crossOrigin 案例同時帶 topOrigin，測不到 crossOrigin 本身；補單獨案例。
- N3 `assertionCode` 依函式庫英文字串分碼；升級時要重跑 `TestAssertionRules`。
- N4 `go.mod` 把 `fxamacker/cbor/v2` 標 indirect，但 `signertest` 直接 import。
- N5 `Revoke` 裡有死碼。
- N6 challenge 時 suspect 併入 `CREDENTIAL_REVOKED`，verify 時細分 `CREDENTIAL_SUSPECT`，不一致。
- 未測：`APPROVER_CHANGED`、`AUTHORIZATION_EXPIRED`、commit 內「issued 但已到期」分支。

Host／web：
- N7 「verify 先成功」時 Host 沒有比對回覆的 `ApprovalSessionID`／`ChallengeID`（intent 涵蓋 task／PO，實務上等同）。
- N8 close 重送用新的 `close_request_id`，不是同一組 ID（Signer 只以 challenge_id 條件式寫入，無正確性問題）。
- N9 會話 `expired`、attempt `verifying`／`unknown` 時，標題寫「沒有核准」，訊息寫「無法確認」，互相矛盾。
- N10 outcome `mismatch` 沒有文案。
- N11 設計寫核准路由要限速；沒有實作（既有路由也沒有；D7 已接受只能造成打擾）。
- N12 全域鎖持鎖期間呼叫 Signer（差異 #7，已註記）。
- N13 文件第 3.3 節「入口實測」與「TestTimingInterleaved 在 f2bf224 失敗」沒有對應的 artifacts；`08-adjacent-demo.log` 另有兩筆既有 `t.Skip`（`TestSeedCancelDisplay`、`TestAliveTreatsZombieAsGone`）文件沒提。
- N14 反向第 7 項（viewer 跨 consumer）抓到的是第二層 `SIGNER_MISMATCH`（502），不是被測的那層（404）；斷言 regex 對任何狀態碼都成立。
- N15 `ApprovalAttemptOf` 在 `OWNERSHIP_MISMATCH` 時仍回另一 consumer 的 attempt 結構（呼叫端都先看錯誤碼）。
- N16 已 verified 的 attempt 以同一 verify ID 重送時，Host 不比對這次的 assertion 內容（Signer 層才會）。
- N17 `make present-a2` 固定 `device`，主持人無法宣告 `virtual`。

### 6.3 十項差異的判定

| # | Signer 側 | Host 側 |
|---|---|---|
| 1 | acceptable | acceptable |
| 2 | acceptable | — |
| 3 | acceptable | acceptable |
| 4 | acceptable | — |
| 5 | 其實不是差異（設計第 7 節已寫） | acceptable |
| 6 | acceptable | acceptable |
| 7 | acceptable；但 R1 顯示 Signer 在並行下不自足，不能靠 Host 的鎖當保證 | acceptable，附 N12 |
| 8 | acceptable | **needs change**（R4） |
| 9 | acceptable | acceptable，附 N17 |
| 10 | 其實不是差異（設計第 7 節第 6 列） | acceptable，附 N16 |

### 6.4 下一步

R1～R6 的修正另開一輪：修正、只重跑受影響的部分、再交一次獨立上下文 review。在那之前不做實機與部署。

## 7 修正輪（10/13；Aaron 在對話中核准修正計畫；Claude Code 執行，都不是實機結果）

範圍：第 6 節的 R1～R6，並處理 N1、N2、N4、N5、N6、N9、N10、N13、N14、N17 與未測分支。只重跑受影響的部分，本機執行、未在容器。**這一輪的通過不併入第 3 節**：第 3 節是 `1f9d966`／`9d3c2aa` 的結果，本節是修正後各自的重跑。

### 7.1 修正

| # | 提交 | 修正 | 新測試 | 反向（新／改） |
|---|---|---|---|---|
| R1 | `73acb69` | `commit` 在寫入交易內先讀 challenge 狀態；不是 issued（used／closed／已到期）就依狀態回 `CHALLENGE_USED`／`CHALLENGE_CLOSED`／`CHALLENGE_EXPIRED`，不看 credential、不比計數器 | `TestTwoAssertionsInFlight` | 新「R1 the commit answers by the challenge before the counter」；第 4、5 項同時關掉新的狀態檢查 |
| R2 | `73acb69` | 移除簽章前的 BE 比對；函式庫在簽章之後的 BE 檢查對應 `ASSERTION_INVALID_FLAGS:suspect`，BS 無 BE 為 `ASSERTION_INVALID_FLAGS` | `TestFlagsNeedTheSignature` | 新「R2 flags are compared only after the signature」 |
| R3 | `570a88c` | Store 回傳 attempt 時一併回會話狀態；會話不是 pending 時不再 share open attempt（`SESSION_CLOSED` 並帶該 attempt）。Host 對這種 attempt 只關閉 challenge、不驅動：`drive`（challenge、status 路由）與 assertion 路由呼叫 Signer 之前（`closeOnSession`；cancel 的關閉部分抽成 `closeAttempt`）。Signer 已 used 的仍會落地為 closed 並 abandon | `TestA2CancelledTask`（改：另一頁的 challenge 請求關閉 challenge、不驗證任何 assertion）、`TestA2AssertionAfterCancel` | 新兩項 R3 |
| R4 | `2faca43` | 已決定的紀錄依自己的方式用詞（會話 `resolved_by` 是否 `webauthn:`、收尾事件的 `authorization_method`），不依 Host 模式；只有仍在等的依模式。A2 模式下 `POST /api/sim/approvals/{session}` 回 409 `human_approval_required`（委託確認仍是模擬） | `TestA2NoSimulatedApproval`；`approve.test.mjs` 兩種模式 × 兩種紀錄 | — |
| R5 | `2faca43` | 抽屜的「授權」列、「裁決」說明與 grant 列依 grant 的方式 | 同上；`a2-ui.mjs` 核准後打開抽屜檢查 | — |
| R6 | `570a88c` | （測試缺口） | `TestA2ChallengeUsedElsewhere`（另一份 assertion 得 `CHALLENGE_USED`；cancel 時 close 回 used；自己的 verify 查無後 close 回 used）、`TestA2TwoAssertions` | 新兩項 R6 |

備註：
- N1、N2、N5、N6 與未測分支：`73acb69`。N6 之後 challenge 對 suspect 回 `CREDENTIAL_SUSPECT`。未測分支由 `TestChangedAndLateBranches` 補上（`APPROVER_CHANGED`、`AUTHORIZATION_EXPIRED`、commit 時到期）。
- N4：`dc71087`。
- N9、N10、N17：`2faca43`。用法：`make present-a2 SIGNER=… AUTH=virtual`；只接受 device 或 virtual，其他值在啟動前就停。
- N14：`6220250`。第 7 項的 regex 改為 `B's viewer: 502, want 404`。拿掉 Store 的擁有權檢查後，仍是 Signer 自己的 consumer 檢查擋下（`SIGNER_MISMATCH`），regex 寫明這一點，不再對任何狀態碼都成立。
- N13：見 7.2 的 `timing-interleaved-f2bf224.txt`。另外，`08-adjacent-demo.log` 裡的兩筆既有 `t.Skip`（`TestSeedCancelDisplay`、`TestAliveTreatsZombieAsGone`）在 A2 之前就存在，A2 沒有碰它們；它們是跳過，不是通過。
- 沒有處理、留待下一輪或已接受：N3（升級 go-webauthn 時要重跑 `TestAssertionRules`）、N7、N8、N11（D7）、N12（差異 #7）、N15、N16。

### 7.2 重跑（證據都在 `artifacts/human-approval/`）

| 重跑 | 基準 | 結果 | 證據 |
|---|---|---|---|
| Signer 反向 8 項（challenge、UV、origin、replay、close first、R1、R2、R2 serve） | `73acb69` | 8／8 預期斷言。第 5 項這次由 schema 的 `closed_at` CHECK 擋下遲到的寫入（測試斷言「late verify」成立） | `reverse-signer-73acb69/` |
| `go test -race`：store、cmd/demo、paycontract、signerclient、signer、cmd/apa-signer（cmd/demo 照名跳過 `TestTimingInterleaved`） | `570a88c` | 全部 ok，exit 0 | `fix-570a88c/go-test.txt` |
| Host／Store 反向 9 項 | `570a88c` | **8／9**。「the other decision gets no challenge」**沒抓到**：R3 把該行改成 switch，舊錨點不存在（紀錄保留） | `reverse-host-570a88c/` |
| 上一項改錨點後單獨補跑 | `75a5445` | 1／1 | `reverse-other-decision-75a5445/` |
| `go test -race ./cmd/demo/`（照名跳過同上） | `2faca43` | ok，exit 0 | `fix-2faca43/go-test-cmd-demo.txt` |
| web 測試 5 檔 | `2faca43` | 全部 pass（29、7、27、9、13） | `fix-2faca43/node-tests.txt` |
| 瀏覽器 a2-ui（CDP virtual authenticator，不是實機） | `2faca43` | 36 項 PASS、0 FAIL（多了抽屜一項） | `a2-ui-2faca439fc7e-20261013T113855Z/` |
| `present.sh` 拒絕不合法的 `APA_AUTHENTICATOR_SOURCE` | `2faca43` | 啟動前停止，exit 2 | `fix-2faca43/present-authsrc-refusal.txt` |
| `TestTimingInterleaved` 於 A2 之前的基準（`git worktree`） | `f2bf224` | FAIL，`multirun_test.go:325`，exit 1 | `fix-2faca43/timing-interleaved-f2bf224.txt` |
| 同上，但第一次 `cd` 進 worktree 失敗（沙箱外 TMPDIR 不同），實際跑在主工作區 | `2faca43` | FAIL，同一斷言。檔頭標籤錯、雜湊正確，已加註保留 | `fix-2faca43/timing-interleaved-2faca43-misdirected.txt` |
| 反向第 7 項（N14）單獨補跑 | `6220250` | 1／1，`B's viewer: 502, want 404` | `reverse-item7-6220250/` |

**沒有重跑**：
- **A2 入口實測**：以測試 Signer 起 `present.sh`。這次被 Claude Code 的安全檢查擋下：命令裡 `present.sh` 會在 `sh -c` 中執行 `rm`，檢查無法分析，所以命令沒有執行，也沒有繞過。第 3.3 節那次入口實測沒有存檔，仍是無證據的陳述。之後由 Aaron 執行 `make present-a2 SIGNER=…`，或在實機階段一併驗證。
- **其他**：容器（Docker daemon 未啟動）、m1-check 全計畫、所有歷史突變。

### 7.3 下一步

- 第二次獨立上下文 review 只看 `20ea7c7..dc71087`（與本節）。
- 通過之後，才討論實機登記／驗證與部署（第 5 節清單）。

## 8 第二次獨立上下文 review 與第二輪修正（10/13；Claude Code，都不是實機結果）

### 8.1 判定

兩個 subagent 只審 `20ea7c7..321c87f`，只拿到 diff、設計文件與本文件。
- **Signer（security-auditor）：pass。** R1、R2 已關閉。reviewer 另以 overlay 重現：challenge 在 verify 途中被關閉或到期時，修正前 credential 被標 `suspect`，修正後仍 active。N1、N6 正確；R1、R2 的新測試在還原修正後會失敗。
- **Host／Store／web（code-reviewer）：not pass（窄）。** R3、R5、R6 已關閉；R4 在 `sim.go` 與 `adapter.js` 已關閉。剩兩項 should-fix：
  - **S1**：`host.js` 兩個面板仍依「哪個面板」用詞，不依紀錄。例如以 `--approvals-signer` 開啟存有 P1-a 模擬核准的資料庫時，A2 面板會把模擬核准寫成「本人核准已驗證」。
  - **S2**：R3 改寫 `TestA2CancelledTask` 後，Host 的 abandon 路徑沒有測試了。

更正第 7 節：
- `TestA2TwoAssertions` 被 Host 的 `attempt_not_ready` 擋下，沒有走到 `resolveUsed`；它是 Host 防護的測試，**不算 R6 的覆蓋**。
- 設計第 9 節「close 回覆遺失」（close 失敗後重試才查到 used）仍沒有 I 層測試。
- N14 的 502 來自 **Host** 比對 Signer 回覆的 `consumer_id`（`onChallenge`），不是 Signer 自己的檢查。提交 `6220250` 的訊息寫錯，訊息不改，以本節為準。

### 8.2 第二輪修正（`0cea87f`）

- **S1**：兩個面板的「已核准／已拒絕」與 grant 列，改由 `approve.js` 的 `decidedText`、`grantLine` 依紀錄自己的方式決定。P1-a 的文字不變。
- **S2**：新增 `TestA2CancelledAfterSignerVerified`。情境：Signer 已 verified，Host 不知道；task 取消後，Host 下一次查詢發現 challenge 已 used，已撤回的會話不記錄任何決定，Signer 的核准被 abandon。另新增一項反向。
- **Signer note 1**：`APPROVER_CHANGED` 的兩半（approver 版本、設定雜湊）各有自己的案例與反向。
- **N-b**：
  - 會話到期、結果未知時，恢復設計第 7 節的原文「無法確認這次驗證的結果；這筆已依後端期限關閉。」，不寫「沒有核准」。第 7 節的 N9 修正方向錯了，這裡更正。
  - A2 面板的「已過期」標題拿掉「沒有核准」。
  - 會話撤回或報價改變時，不再寫成「依期限關閉」。
- **N-f**：mismatch 文案不再說「仍等你決定」。
- **N-d**：反向項目的註解改寫正確的那一層。

不處理、記錄為已知：
- **Signer note 2**：`refuse()` 標 `suspect` 前不重讀 challenge 狀態。需要持鑰者簽章、BE 卻改變的 assertion；依 D6 這本身就可疑，R2 仍成立。
- **Signer note 3**：同一把 credential 兩個 challenge 並行、亂序 verify，會觸發計數器政策。這是 D6 已接受的影響，已列在第 4 節。
- **N-c**：`ApprovalAttemptOf` 唯讀、不記錄到期，所以期限已過但尚未記錄時，assertion 仍會送到 Signer。之所以正確，是因為 Signer 的 challenge 期限不超過 HostDeadline，且 Signer 的時鐘不落後 Host。
- **N-e**：本節起，`go test`、`node --test` 與反向的證據檔都寫明命令；瀏覽器的 log 只記 commit 與驗證器，命令見本文件（第三次 review 指出原句誇大，第 9 節起另存 `browser-runs.txt` 含命令）。
- 第 7 節列的其他 note 不變。

### 8.3 重跑（`0cea87f`；本機、未在容器）

| 重跑 | 結果 | 證據 |
|---|---|---|
| `go test -count=1 -race -skip ^TestTimingInterleaved$ ./internal/signer/... ./cmd/apa-signer/ ./cmd/demo/` | 全部 ok，exit 0 | `artifacts/human-approval/fix-0cea87f/go-test.txt` |
| web 測試 5 檔 | 全部 pass（29、8、27、9、13） | `artifacts/human-approval/fix-0cea87f/node-tests.txt` |
| 反向 3 項（S2、approver 版本、設定雜湊） | 3／3 預期斷言 | `artifacts/human-approval/reverse-0cea87f/` |
| 瀏覽器 a2-ui（CDP virtual，不是實機） | 36 PASS、0 FAIL | `artifacts/human-approval/a2-ui-0cea87f4c951-20261013T121941Z/` |
| 瀏覽器 p1a-ui（P1-a 面板改用 `decidedText`） | 54 PASS、0 FAIL | `artifacts/p1a/p1a-ui-0cea87f4c951-20261013T122029Z/` |

下一步：第三次獨立上下文 review，只看第二輪修正的差異。之後才討論實機與部署。

## 9 第三次獨立上下文 review 與第三輪修正（10/13；Claude Code，都不是實機結果）

第三次 review 由一個 subagent 執行，只審 `321c87f..4662ae3`。判定 **not pass（窄）**：
- S1、S2 已關閉；`APPROVER_CHANGED` 的兩半各自獨立；Signer 端與反向項目沒有問題。
- 剩兩項 should-fix：
  - **A**：已決定紀錄的標籤與來源列仍依面板。例如舊資料庫的模擬核准在 A2 面板顯示「本人核准・驗證器：已登記的裝置」。
  - **B**：會話到期、結果未知，且 Signer 仍連不上時，恢復輪詢的「結果確認中」蓋過設計第 7 節的終點文字。
- note：
  - **C**：withdrawn／superseded 標題仍有「沒有核准」「核准作廢」；註解誤寫 N9。
  - **D**：outcome closed 寫「在驗證完成前」，但 S2 的情境裡 Signer 其實驗證過。
  - **E**：N-e 誇大，見第 8.2 節更正。

第三輪修正（`13dbc03`）：
- **A**：用 `recordTag` 讓與面板方式不同的紀錄顯示自己的標籤與來源列；A2 面板上的模擬紀錄不提裝置。
- **B**：用 `panelNote`，會話不是 pending 時，存下的 attempt 文字優先於上一次操作的訊息。
- **C**：A2 面板的標題改寫，列為第 2 節差異 #11。
- **D**：改為「這筆已關閉（過期、取消或報價改變），裝置的驗證沒有被採用；不會付款。」
- 測試：`approve.test.mjs` 新增一項（A、B）。

重跑（`13dbc03`；只改了 web，沒有改 Go）：

| 重跑 | 結果 | 證據 |
|---|---|---|
| web 測試 5 檔 | 全部 pass（29、9、27、9、13） | `artifacts/human-approval/fix-13dbc03/node-tests.txt` |
| 瀏覽器 a2-ui（CDP virtual，不是實機） | 36 PASS、0 FAIL | `artifacts/human-approval/a2-ui-13dbc03e5c4e-20261013T123052Z/`；命令與輸出在 `fix-13dbc03/browser-runs.txt` |
| 瀏覽器 p1a-ui | 54 PASS、0 FAIL | `artifacts/p1a/p1a-ui-13dbc03e5c4e-20261013T123143Z/`；同上 |

沒有 W 或 B 層測試在畫面上直接呈現 B 的情境（Signer 斷線且會話到期）；B 只在 `panelNote` 的單元測試覆蓋。

下一步：第四次獨立上下文 review，只看 `4662ae3..` 本輪差異。

## 10 第四次獨立上下文 review 與第四輪修正（10/13；Claude Code，都不是實機結果）

第四次 review 由一個 subagent 執行，只審 `4662ae3..6c38cf3`。判定 **not pass（窄）**：A、B 已關閉。剩兩項 should-fix：
- **S-B**（B 的同類，舊碼）：會話關閉後，pending 時寫下的訊息仍會留著。例如按核准後取消裝置提示，然後期限到，標題寫「已過期」，訊息卻仍是「這筆仍在等你決定」。另外，`challenged` 或 `requested` 的 attempt 在會話關閉後沒有終點文字。
- **S-A**（A 的殘留）：A2 面板上的模擬紀錄，intent 列寫「Signer 依完整請購重算比對」，且仍顯示裝置提示說明。所以第 9 節「A2 面板上的模擬紀錄不提裝置」是誇大，以本節為準。

note：
- outcome closed 也涵蓋「Signer 的核准已過期、會話仍 pending」。
- 卡片與抽屜在 A2 模式下，對 expired、superseded 仍是 P1-a 的「已過期，無法核准」「這次核准作廢」，與面板用詞不同；差異 #11 只涵蓋面板。
- 跨模式下，已決定的會話配上未知的 attempt。
- A 也只有單元測試：a2-ui 與 p1a-ui 只有同方式的紀錄，沒有在畫面層執行 `other` 分支。第 9 節只對 B 說明了這點，這裡補上。

第四輪修正（`598bbef`）：
- **S-B**：會話不是 pending 時，任何未結束的 attempt 都依會話狀態給終點文字：
  - unknown／verifying：「無法確認這次驗證的結果；這筆已依後端期限關閉。」（或已關閉，任務取消／報價改變）；
  - requested／challenged／refused：「……；這次驗證沒有完成。」
  - 只有最終訊息（ok、deny、closed）留下。已決定的會話不加訊息，標題即結果。
- **S-A**：與面板方式不同的紀錄，intent 列寫「後端比對這一份」，也不顯示裝置提示說明。
- **note 3**：outcome closed 改為「裝置的驗證沒有被採用（這筆已關閉，或 Signer 的核准已過期）；不會付款。」
- 測試：`approve.test.mjs` 新增一項（S-B）。

不處理、記錄為已知：
- 卡片與抽屜的 expired、superseded 用詞（P1-a 共用，差異 #11 只涵蓋面板）。
- A、S-A、B、S-B 都只有單元測試，沒有畫面層測試。

重跑（`598bbef`；只改了 web）：

| 重跑 | 結果 | 證據 |
|---|---|---|
| web 測試 5 檔 | 全部 pass（29、10、27、9、13） | `artifacts/human-approval/fix-598bbef/node-tests.txt` |
| 瀏覽器 a2-ui（CDP virtual，不是實機）與 p1a-ui | 36 與 54 PASS、0 FAIL | `fix-598bbef/browser-runs.txt`（含命令）；`artifacts/human-approval/a2-ui-598bbef7650d-20261013T124122Z/`、`artifacts/p1a/p1a-ui-598bbef7650d-20261013T124208Z/` |

下一步：第五次獨立上下文 review，只看 `6c38cf3..` 本輪差異。
