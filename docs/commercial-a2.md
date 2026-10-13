# A2 本人核准產品接線：實作差異、驗證與限制

> 2026-10-13，Claude Code。依 vault〈Claude-Code-A2本人核准產品接線實作派工-f2bf224-20261013〉（Aaron 已批准「A2 開工」）。分支 `commercial-a2`，基準 `f2bf224`（工作區只有未追蹤的 `demo`；沒有適用的 AGENTS.md）。
> **狀態：A2 已實作並完成本輪定向自動化驗證，待 Codex 技術 review；實機登記與操作未驗收，付款未啟用。** A2 不自行標 Done；本輪所有結果都是「Claude Code 執行，待 Codex 核對」，不是實機結果。

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
