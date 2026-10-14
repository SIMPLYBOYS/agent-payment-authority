# C2：BBS+ 憑證接入 Policy（`apa.pay.v2`）

> 2026-10-14，Claude Code。設計與 Aaron 的決定見 [commercial-c2-design.md](commercial-c2-design.md)（第四版；C2-D1～D12 已核准，第 9 節）。分支 `commercial-c2`。
> **C2 Done（Aaron 10/14：「C2 請標記為 done 並 merge 進 main」）；已 fast-forward merge 進本機 main，未 push。** 付款仍未啟用；憑證是 fake 目錄與測試金鑰。
> **狀態（10/14）：C2a、C2b 都已通過 Claude Code 獨立上下文 review（C2b：pass，無 must-fix；三項 should-fix 已修，第 8 節）。** 本文的驗證都由 Claude Code 執行。
> 本片**不付款**：沒有付款金鑰、簽署、faucet、鏈或交易；沒有執行真實 CLI。憑證全部是合成的測試金鑰，沒有 eKYC、真實 issuer、錢包或 RPC。

## 1 一段話

`apa.pay.v2` 是 `apa.pay.v1` 加上一個憑證步驟。task 開始時，測試 issuer 為這個 task 發一張 BBS+ 憑證（`task_id、consumer_id、c0、expiry、holder、status` 等 8 個欄位）；每筆請購在 Policy 之前，由 Host 的 holder 對 Store 發的 challenge 出示 derived proof，Store 在同一個寫入交易裡驗證、留下驗證紀錄與 `CREDENTIAL_VERIFIED` 事件，再讓 Policy 依結果裁決。憑證無效、被撤銷、狀態無法確認或已過期，一律 Deny 並留紀錄；有效時，原本的 Rules 照常決定 Allow、Ask 或 Deny。Ask 等待核准時若憑證被撤銷或過期，核准時重新檢查會以撤回關閉，不發授權。

**誠實標示**：BBS+ 是 idemix v0.3.1 的舊版 BBS+，**不是 W3C `bbs-2023`**；issuer、holder、verifier 都在 host 同一個程序（設計第 5 節）。C2 證明的是格式、綁定、撤銷與裁決的接線，不是三方彼此獨立。對自家 Policy 而言，出示會揭露 holder 公鑰，所以可串連（C1-D2）。

## 2 使用者看到的流程（`make present-c2`）

配對 →「以 A 模擬」→「建立委託」→「確認委託並開始（模擬）」→ Host 為 task 發證 → Agent 選購、請購 → 驗證通過（卡片與證據抽屜的「憑證檢查：通過：BBS+（舊版，非 W3C bbs-2023）・測試金鑰」）→ 0.50 高於 allow_max 0.10，得到 Ask。

- 等待核准時執行 `apa-issuer revoke --task show-a-01`（腳本會印出完整指令）→ 按「核准此筆（模擬）」→「憑證已撤銷，這筆已關閉」，沒有授權；不會寫成「已撤回（task 取消）」。
- 客人 B 的憑證仍有效 → 核准 →「已核准（模擬），未啟用付款」。
- 頁首是「C2／fake agent／BBS+ 憑證／模擬核准／不付款」；能力列寫「憑證 BBS+ 舊版（非 W3C bbs-2023；測試 issuer，與 host 同一程序）· ZK 未實作」。

## 3 實作內容

| 部分 | 內容 | 位置 |
|---|---|---|
| 契約 | 依版本參數化（C2a）；v2 的 Ask 收尾表（可因三種憑證理由撤回；v1 不變且拒絕）；`CREDENTIAL_VERIFIED` 事件與 payload（M1、v1 拒絕；v2 每筆請購恰一個，在 PROPOSED 與 DECIDED 之間，裁決隨結果） | `internal/paycontract/`、`internal/contracts/events.go` |
| 憑證 | `apa.credential.v1` 外層、測試 issuer、holder 出示（nonce 綁 verifier、challenge、intent、揭露清單；holder Ed25519 簽章）、驗證順序 invalid → status_unavailable → revoked → expired → valid；憑證目錄（0700、擁有者、無 symlink、0600 金鑰、原子寫入）；簽章的狀態清單（只增不減、鎖內重讀、新鮮期 ≤ 600 s、時鐘偏差 60 s）；狀態索引計數器；`IssueTask`（Host 發證）、`Present`（Host 的 holder；材料問題與產生 proof 失敗分開） | `internal/credential/` |
| 依賴 | `github.com/IBM/idemix v0.3.1`、`github.com/IBM/mathlib v0.3.2`，go 1.26.3（C2-D6） | `go.mod` |
| Store schema 6 | `credential_bbs` 快照種類、`apa.pay.v2` 進 CHECK（重建表、外鍵關閉在交易外、`foreign_key_check`）；新表 `credential_challenges`、`credential_verifications`、`credential_status_seen`；1～5 版可升級 | `internal/store/store.go` |
| Store 流程 | v2 task 啟動帶 `credential_bbs` 快照（或宣告 absent）；`IssueChallenge`；`SubmitV2`（用掉 Store 自己保存的 challenge、讀當下狀態清單並防回捲、驗證、紀錄與三個事件同一交易）；`Submit` 拒絕 v2；開啟時的交叉核對；核准（模擬與 WebAuthn）時重新檢查，不通過以撤回關閉；付款開始前同樣檢查，只拒絕不寫入 | `internal/store/credential.go`、`pay.go`、`ops.go`、`approvals.go`、`payment.go`、`verify.go` |
| Policy | `paypolicy` v2：只收驗證紀錄，不收 fixture；非 valid 直接 Deny（先於 Rules）；valid 把驗證過的 expiry 交給 Rules。`policy.Decide`、`Rules`、`CONFIG_MISSING` 的語意不變 | `internal/paypolicy/` |
| Host、Shop | `--credential-dir`（啟動時先更新清單，之後每 300 s；v2 目錄沒有它就不啟動）；v2 場次在 `StartTask` 前發證；`Commit` 鎖內依序 challenge → 出示 → `SubmitV2`；v2 只限 fake 目錄 | `cmd/demo/`、`internal/toolbridge/shop.go` |
| issuer CLI | `apa-issuer init / revoke（--task 或 --status）/ refresh`，只有測試金鑰，不印任何秘密 | `cmd/apa-issuer/` |
| 畫面 | 卡片與抽屜的「憑證檢查」與細節；憑證理由的文字；以憑證理由撤回時依理由寫「憑證已撤銷（狀態無法確認、已過期），這筆已關閉」，`HOST_CANCELLED` 沿用「已撤回（task 取消）」；頁首與能力列 | `web/adapter.js`、`host.js`、`app.js` |

## 4 與設計不同或補充的地方

- **`PRESENTATION_REQUIRED`**：task 有憑證、Host 卻沒有出示也沒有回報材料問題時，`SubmitV2` 拒絕且不留裁決（新的 Store 代碼）。這是 Host 的程式錯誤，不是憑證的問題。
- **holder 材料的判斷多一步**：`Dir.Present` 先用 issuer 公鑰驗證存下的簽章；簽章壞掉屬於材料問題（Deny `CREDENTIAL_INVALID`，`HOLDER_MATERIAL_UNAVAILABLE`），不是「產生 proof 失敗」。否則壞掉的憑證會被當成暫時錯誤而無限重試（C2-D12 的本意）。
- **`InitDir` 檢查 consumer ID**：consumer ID 會組成金鑰檔名；格式不符或重複就拒絕（避免 `../` 跳出目錄）。
- **PurchaseView 多兩個欄位**：pay 系列的請購檢視帶 `credential`（取自 `CREDENTIAL_VERIFIED`）與 `closed_reason`（收尾事件的理由），畫面依此顯示。M1 的檢視不變（`display-histories.json` 未改）。
- **Unity 的英文標籤沒改**：以憑證理由撤回時 Unity 仍顯示「ask withdrawn - leaving」（對它為真）；Unity 沒有重建，`scene-transitions.json` 與 `Projection.cs` 共用。頁面自己的文字寫明理由。場景下方「Unity 已呈現 … withdrawn（HUMAN_APPROVAL_REQUIRED）」括號裡是 Unity 回報的投影理由（原裁決），P1-a 以 `HOST_CANCELLED` 撤回時也一樣，屬既有行為。
- **Shop 沒有憑證目錄時**：v2 task 的請購以材料不可得送出（Deny）。實際上沒有目錄時 v2 task 根本不會啟動，這只是防線。
- **verifier ID** 固定為 `apa-demo-host`（同一台 host）。
- **沒有憑證目錄時重新開啟**（review S1）：狀態清單讀不到，等待中的 v2 Ask 核准時以 `CREDENTIAL_STATUS_UNAVAILABLE` 撤回，付款被拒；不會當掉。
- **快到期的 challenge 不再沿用**（review note 3）：剩不到 5 秒就發新的，避免重試時因時鐘跨過期限而被 Deny。

## 5 fake 操作步驟（Aaron）

1. `make present-c2`。腳本會建置 `apa-issuer`，在場次目錄建 `cred/`（客人 A、B 的 holder，測試金鑰），並印出撤銷指令。頁首應是「C2／fake agent／BBS+ 憑證／模擬核准／不付款」。
2. 「以 A 模擬」→「建立委託」→「確認委託並開始（模擬）」。Ask 出現後，卡片「憑證檢查」寫「通過：BBS+（舊版，非 W3C bbs-2023）・測試金鑰」；「查看技術證據」可看憑證細節（issuer、狀態索引、清單版本、challenge、verifier）。
3. 在另一個終端機執行腳本印出的 `…/apa-issuer revoke --dir …/cred --task show-a-01`，再按「核准此筆（模擬）」→「憑證已撤銷，這筆已關閉」，訊息「未核准：憑證已撤銷，這筆已關閉（後端判定）」；沒有授權。
4. 「以 B 模擬」→ 建立委託並開始 → 核准 →「已核准（模擬），未啟用付款」。

每一步都不會付款。結束按 Ctrl-C；場次目錄保留在 `var/present-*`（含測試金鑰，可直接刪除）。

**注意**：「已看過的清單版本」以 issuer ID 為鍵。若重新建立憑證目錄（同一個 issuer ID、新的清單從第 1 版開始）卻沿用舊的資料庫，新 task 會一直被判 `CREDENTIAL_STATUS_UNAVAILABLE`（`STATUS_LIST_ROLLBACK`），直到新清單的版本超過舊的。這是安全的一側（拒絕），但請把憑證目錄與資料庫一起換新；`make present-c2` 每次都是新的場次目錄，不會遇到（review note 1）。

## 6 驗證（Claude Code 執行；證據在本機 `artifacts/c2/`，git 忽略）

各次結果分開記，沒有拼成一次全過。

| 項目 | 基準 | 結果 |
|---|---|---|
| `go test ./...` | `afcdd13` | 25 個套件 ok；`cmd/demo` FAIL：只有 `TestTimingInterleaved`，在 main `74cdc55` 上同樣失敗（先前記錄過的環境問題），`-race` 那次通過。較早一次（`fb9bd7d`）另有 `cmd/apa-buyer TestLiveOptIn` 在整體負載下逾時，單獨重跑 ok |
| `go test -race`（credential、store、paypolicy、paycontract、toolbridge、contracts、signer、apa-issuer、cmd/demo） | `dd15334` | 全部 ok（`c2b-dd15334/race.txt`） |
| 頁面測試（6 個檔案，含新的 `c2.test.mjs`） | `afcdd13` | 全部通過 |
| 瀏覽器檢查 `scripts/verify/c2-ui.mjs` | `dd15334` | PASS（`c2-ui-dd15334a5a11-…`）：頁首、憑證列與抽屜、撤銷後核准、B 核准、截圖時 token 欄位為空，以及第 38 項遮罩掃描（8 個金鑰種子與簽章值 × 12 處：頁面、API、資料庫與 WAL、trace、host log，都沒有出現）。第一次在 `0581a38` 也 PASS |
| 反向 `^C2`（C2a 11＋C2b 25） | `dd15334` | 35／36：C2a「請購只在同版本 task 裁決」錨點已因 `4ebbc00` 過時，未抓到，保留；`59c5bf0` 重新錨定後單獨補跑 1／1 |
| 反向（較早的分次） | — | `7e536a9` 19 項 18／19（「恢復時紀錄須等於事件」突變後變數未使用、建置失敗，未抓到）；`fb9bd7d` 修正後 1／1。`4ebbc00` 2／2；`c687bfe` 新增 6 項 6／6 |
| 其他切片因 C2b 過時的突變 | `afcdd13` | P1a 兩項（`t.v1()` 改名、委託規則改成 pay 系列）重新錨定，2／2；M1 突變 #79（宣告缺設定）重新錨定，在一次性 worktree 手動套用，`TestStartTask` FAIL（抓到） |

**在 main 上就已失效、不是 C2 造成的錨點**（未修改，請另行決定）：`m-b-reverse.py` 的 P1a-R「R3 recovery checks the grant's times」、P1a-R「R1 ui: a rebuilt panel…」（出現兩次）、P1b「IPC: a caller uid not configured is refused」；`mutation-check.py` 有 23 個（`python3 scripts/verify/mutation-anchors.py` 列出）。

**設計第 11 節的測試清單**都有對應測試，除了兩項：
- 「產生 proof 失敗 → 工具回錯誤，沒有裁決」：`Present` 先驗簽章後，`DeriveProof` 幾乎只剩亂數來源失敗會出錯，沒有可注入的故障點；程式路徑是直接回傳錯誤、不送出（`Shop.submitV2`）。
- 「並行：同一 product 兩次工具呼叫」：靠既有的 `Binding.Commit` 鎖（P1-a 已測）；v2 的第二次呼叫在鎖內看到已存的 purchase，`IssueChallenge` 不發 challenge，`SubmitV2` 回原紀錄（`TestSubmitV2` 的重送、`TestIssueChallenge`）。

## 7 沒有做的

- 付款、Signer 的付款路徑、鏈、x402 credential extension（驗收第 26 項，屬 P2）。
- C₀（`c0` 是全零佔位，C2-D5；屬 Z2）、eKYC、真實 issuer、三方分程序部署。
- v2 的真實 Agent（C2-D9：只限 fake）。
- Touch ID、Safari、手機、安全金鑰、容器執行（Aaron 排到後期）。
- checker 不新增 credential claim（E1-b）。

## 8 獨立上下文 review（10/14）

Claude Code 獨立上下文 subagent review `8a36be2..7eff220`：**pass，無 must-fix**。決策路徑（用 Store 自己的 challenge、快照的 issuer 金鑰與狀態金鑰指紋、每次重讀清單、驗證先後、v2 先於 Rules 的 Deny、三個事件同一交易、核准時重查的位置）都與設計一致；v1 與 M1 只經 `pay()` 改動。reviewer 也在暫存副本重現了 S1 並確認 S2、S3 的突變原本不會被抓到。

| 項目 | 處理 |
|---|---|
| S1 沒有憑證目錄重新開啟後核准 v2 Ask 會當掉（nil） | 修正：讀不到清單即 `status_unavailable`；`TestV2RecheckWithoutDirectory` |
| S2 Shop 的「材料問題 vs 產生 proof 失敗」沒有測試 | 補 `TestC2HolderMaterialMissing` 與反向項目 |
| S3「已看過的最高版本」會前進沒有測試 | 補 `TestStatusSeenAdvances` 與反向項目 |
| note 1 已看過的版本以 issuer ID 為鍵 | 不改 schema；第 5 節寫明操作方式 |
| note 2 重新開啟時沒有核對 Allow／Ask 的 `expiry > decided_at` | 未改：要同時竄改快照、紀錄與事件鏈才會出現，與其他交叉核對同一層級；留給後續 |
| note 3 快到期的 challenge 會被沿用 | 修正：剩不到 5 秒不沿用；`TestChallengeReuseMargin` 與反向項目 |
| note 4 `credentialCheck` 接受沒有 issuer 的 status_unavailable | 未改：Verify 不會產生這種組合 |
| note 5 `InitDir` 會先把既有路徑改成 0700 | 未改：測試工具、同一使用者；請只對新路徑執行 `apa-issuer init` |

修正提交 `df2c054`：`go test` store、toolbridge 通過，cmd/demo 只有已知的 `TestTimingInterleaved`；三個新反向項目 3／3。
