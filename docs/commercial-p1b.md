# P1-b：金鑰庫與獨立 Signer（實作紀錄）

> 2026-10-12，Claude Code。依 vault〈Claude-Code-P1-b金鑰庫與獨立Signer實作派工-e725c44-20261012〉（Aaron：「這三件事我都核准，請提供派工筆記」）實作。設計見 [commercial-p1b-design.md](commercial-p1b-design.md)，分支 `commercial-p1b`，從 `e725c44` 開出。
> **狀態：P1-b 實作缺口 R1～R3 已通過限定技術複核（程式 `07b16fd`、測試與計畫 `935de2f`；Codex 唯讀核對，沒有獨立重跑）；OS 隔離部署尚未驗收，正式付款仍未啟用。**（第 9.4 節；下一步為 OS 隔離部署規劃與另行核准；P1-b 不標 Done。）
> 以下為 `a33ebf7` 交付時的狀態，保留原文：P1-b R1、R3 限定複核關閉；R2 回覆分類補正完成，待 Codex 限定複核；OS 隔離部署尚未驗收；正式付款仍未啟用。（第 9 節：Codex 對 `fcc5095` 的限定複核關閉 R1、R3；R2 剩下的回覆分類已補正並在隔離容器完成定向測試。）
> 以下為 `fcc5095` 交付時的狀態，保留原文：P1-b 實作 R1～R3 補正完成，待 Codex 限定複核；OS 隔離部署尚未驗收；正式付款仍未啟用。（第 8 節：Codex 對 `22e45ed` 的技術 review 暫不通過，R1～R3 已補正並在隔離容器完成定向測試。）
> 以下為 `22e45ed` 交付時的狀態，保留原文：P1-b 已實作並完成本輪定向測試，待 Codex 技術 review；OS 隔離部署尚未驗收；正式付款仍未啟用。（本輪 run 的 29 項反向檢查中，1 項因反向項目本身的突變寫法無法編譯而未計為抓到；修正該項目後單獨補跑，已被預期斷言抓到。見第 5 節。）
> 沒有建立系統帳號或 socket 目錄，沒有日常或展示用付款金鑰，沒有連鏈、faucet 或 facilitator，Signer 沒有接進任何展示流程。所有執行結果歸 Claude Code，待 Codex 核對。

## 1 定向驗收計畫（執行前）

**基準**：本計畫與反向項目提交後的 HEAD（m1-check 會記錄 `base` 與 `tracked_clean`）。實際為 `f434d10`。

**命令**：

```sh
go run ./cmd/m1-check --plan scripts/verify/p1b-check-plan.json --list   # 只列步驟與反向項目，不執行
env HOME="$T/home" GOCACHE="$(go env GOCACHE)" GOMODCACHE="$(go env GOMODCACHE)" \
    GOFLAGS=-mod=readonly GOPROXY=off GOTOOLCHAIN=local \
    go run ./cmd/m1-check --plan scripts/verify/p1b-check-plan.json --out artifacts/p1b
```

- `$T` 是本輪新建的空暫存目錄；`HOME` 指向它，所以測試讀不到日常 HOME、keychain、既有 wallet 或登入資料。
- `GOPROXY=off`、`-mod=readonly`、`GOTOOLCHAIN=local`：測試階段不下載依賴、不改 go.mod，也不換工具鏈。依賴已在測試前另外取得（`go get`，與本計畫分開）。
- 測試只連自己建立的 Unix socket（`$TMPDIR` 下的短路徑）與本機 loopback，不連外部服務。Claude Code 的工具沙箱禁止綁定 Unix socket，所以這個 run 在沙箱外執行；網路隔離靠上面的設定與測試本身只用本機 socket，**不是** OS 層的斷網容器。
- 證據目錄：`artifacts/p1b/m1-check-<UTC 時間>/`（git 忽略）。stderr 顯示目前步驟、耗時、心跳與 log 路徑；stdout 是機器可讀摘要。

**步驟、每步上限與預估**：

| # | 步驟 | 範圍 | 上限 | 預估 |
|---|---|---|---|---|
| 1 | vet | `go vet ./...` | 5 m | 30 s |
| 2 | crypto-keystore | EIP-712 規格範例向量（domain、digest、地址、r‖s‖v）、Circle 公布的 type hash、決定性、low-S、金鑰檔加解密與竄改／錯密語／弱參數／舊版本拒絕、金鑰檔權限 | 5 m | 20 s |
| 3 | authorization | 綁定逐欄、假／改版本／過期委託、跨 consumer、每筆與累計上限、simulated／webauthn 拒絕、正式組裝回 `RESERVATION_UNAVAILABLE` | 5 m | 20 s |
| 4 | state-race（`-race`） | 重送／衝突／重播／同 nonce、並行超額、三個中斷點與重啟、R3-a、R3-b、錢包鎖有界等待、委託先到期、金鑰版本不符 | 10 m | 40 s |
| 5 | ipc-host-store-race（`-race`） | caller uid、64 KiB、固定路由、log 不含簽章與金鑰；Host client 的 requested／unknown 與恢復、回覆遺失；schema 4、1／2／3 升級、舊 simulated 保留 | 10 m | 1 m |
| 6 | adjacent-store | `internal/store`（全部，含 M1）、`paycontract`、`paypolicy`、`m1evidence` | 10 m | 1 m |
| 7 | adjacent-store-race（`-race`） | Store 的 Pay／Upgrade／Expir／Recovery／Cancel | 10 m | 1.5 m |
| 8 | adjacent-demo | `cmd/demo` 的 `TestP1a*`、`TestHistory*`、`TestHistoricalTaskKeepsItsBinding`（`--upgrade-db` 與歷史檔讀取經過改動的路徑） | 10 m | 1 m |
| 9 | reverse | `^P1b:` 29 項，每項先跑未突變基準，再只在一次性 clone 拿掉一個防護 | 40 m | 12 m |

合計預估約 18–20 分鐘。

**反向檢查**：只有預期斷言命中才算抓到；編譯錯誤、panic、逾時、未跑到測試都不算。項目見 `python3 scripts/verify/m-b-reverse.py --list '^P1b:'`。

**不在本計畫**：完整斷網容器七步、全部突變、M1 重新產包、瀏覽器流程、Unity、node web 測試（本片沒有改 web）。

**失敗處理**：保存原始結果，定位、修正後只重跑受影響的步驟或項目；測試程式的缺陷與產品缺陷分開記，不把多輪結果拼成同一輪全過。

## 2 提交與來源對照

| 提交 | 內容 |
|---|---|
| `3140cb3` | 設計文件同步：`e725c44` 通過限定設計複核、Aaron 三項核准、第 5.6 節查詢文字、Codex 證據歸屬 |
| `48f929a` | Signer 核心、金鑰庫、socket 服務、`cmd/apa-signer`、兩個新依賴 |
| `edb9261` | Store schema 4 `payment_attempts`、Host 的 Signer client |
| `f434d10` | 定向計畫、29 項反向項目、兩個測試收緊（本輪驗收基準） |
| `0dd458a` | 修正一項反向項目的突變寫法（工具缺陷，見第 5 節） |

| 設計 | 實作 | 測試 |
|---|---|---|
| 3.3 金鑰檔、終端機解鎖 | `internal/signer/keystore.go`、`cmd/apa-signer/main.go`（`assemble`、`readPassphrase`、`private`）、`termios_*.go` | `crypto_test.go` `TestKeyFile`；`cmd/apa-signer/main_test.go` |
| 3.5 可信設定 | `internal/signer/config.go` | `core_test.go`（經 `signertest.Config`） |
| 4.2 介面、caller uid、上限 | `internal/signer/server.go`、`peercred_darwin.go`、`peercred_linux.go`、`api.go` | `server_test.go` |
| 4.3 檢查 1–7 | `core.go` `verify` | `TestRefusalsConsumeNothing`、`TestBindingFields`、`TestProductionVerifierRefuses` |
| EIP-712、簽章編碼 | `internal/signer/eip712.go` | `TestEIP712SpecVector`、`TestTypeHashes`、`TestSignOne` |
| 5.1–5.2 快照、指紋、唯一性、累計 | `core.go` `accept`、`fingerprint`、`schema` | `TestResendConflictReplay`、`TestTotalCap`、`TestConcurrentCap` |
| 5.3 最後判定 | `core.go` `finish` | `TestFinalDeadline`、`TestAcceptDeadline`、`TestDelegationEndsFirst` |
| 5.4 中斷點與恢復 | `core.go` `recover`、`resume`；測試 hook `at` | `TestInterruptions`、`TestKeyVersionMismatch` |
| 5.6 錢包鎖 | `core.go` `lockWallet` | `TestWalletBusy`、`TestAbandonRaces` |
| 5.7 abandon、查詢 | `core.go` `Abandon`、`Get` | `TestAbandonRaces`、`TestFinalDeadline` |
| 6.4 Host `payment_attempts` | `internal/store/payment.go`；`store.go`（版本 4、遷移）、`verify.go`、`snapshot.go`、`runs.go`（代碼表） | `internal/store/payment_test.go`；既有升級測試改為期待 schema 4 |
| Host client | `internal/signerclient/client.go` | `client_test.go` |
| 7 正式組裝與測試替身 | `cmd/apa-signer` `assemble`（固定 `signer.Unavailable{}`）；替身只在 `_test.go` 與 `internal/signer/signertest`（合成請購與設定，沒有金鑰） | `TestAssemblyRefusesEveryOrder` |

## 3 依賴

| 模組 | 版本 | 用到的 API | 理由 |
|---|---|---|---|
| `github.com/decred/dcrd/dcrec/secp256k1/v4` | v4.4.1 | `PrivKeyFromBytes`、`PubKey().SerializeUncompressed`、`ecdsa.SignCompact`、（測試）`ecdsa.RecoverCompact` | 決策 2 核准；RFC 6979 決定性、low-S 的 compact 簽章 |
| `golang.org/x/crypto` | v0.57.0 | `sha3.NewLegacyKeccak256`、`scrypt.Key` | 決策 2 核准；keccak-256 與金鑰檔的 KDF |
| `golang.org/x/sys` | v0.48.0（不變） | `unix.GetsockoptXucred`／`GetsockoptUcred`、`IoctlGetTermios`／`IoctlSetTermios` | 原本就釘在 go.mod（indirect），現在直接 import，所以移到直接依賴；版本不變 |

- 選版：v0.57.0 的 `x/crypto` 需要的 `x/sys` 正好是已釘的 v0.48.0，不連帶升級其他模組；secp256k1 v4.4.1 只多帶進 `github.com/decred/dcrd/crypto/blake256 v1.1.0`（出現在 `go list -m all`；本 repo 沒有 import 它，go.sum 也沒有它的條目，離線建置與 `go mod verify` 都通過）。
- go.mod：新增一個直接依賴區塊（三個模組），並移除 `x/crypto`、`x/sys` 的 indirect 行；go.sum 新增四行（兩個模組各自的 `h1:` 與 `/go.mod`）。沒有 go-ethereum，沒有升級其他模組或工具鏈。
- 取得方式：`go get` 兩個模組（測試前、與測試分開）。`go mod tidy` 在離線（`GOPROXY=off`）下會去查 modernc libc 的測試依賴而失敗，所以手動把 tidy 會做的變更（indirect → direct）寫進 go.mod；之後離線 `go build ./...`、`go mod verify`（all modules verified）、`go list -m all` 通過。

**簽章編碼與來源**：`ecdsa.SignCompact(key, digest, false)` 回 `[27 + recid] ‖ r ‖ s`，s 已是 low-S；本實作重排成以太坊的 `r ‖ s ‖ v`（v＝27 或 28）。凍結向量取自 EIP-712 規格的範例（`Example.js`／`Example.sol`：私鑰 keccak256("cow")、Mail 型別），domain separator、digest、地址與 r、s、v 全部和規格公布值一致；`TransferWithAuthorization` 的 type hash 等於 Circle FiatToken（`EIP3009.sol`）公布的常數 `0x7c7c…2267`。`TestSignOne` 另外驗 65 bytes、v∈{27,28}、s ≤ n/2、ecrecover 得回錢包地址、digest 由 domain 與 message 重建後相同。

## 4 設計與實作的差異

沒有改變信任邊界或付款能力的差異。

| 主題 | 設計 | 實作 | 影響 |
|---|---|---|---|
| 鎖 | 5.6：錢包鎖序列化接受與簽署，取不到有界等待後 `SIGNER_BUSY` | 每個錢包一個容量 1 的 channel；等待上限 `lockWait` 3 秒（套件變數，只有測試改短）；4.3 的檢查也在鎖內 | 與設計一致；上限值是實作選擇 |
| 最後判定 | 5.3 步驟 4：影響 0 列時同一交易重讀 | 同一交易再做一次 `WHERE state='accepted'` 的條件式到期更新，提交後重讀狀態回覆 | 同義 |
| abandon 已到期 | 5.7／8：查詢先寫到期，再 abandon | abandon 自己也會把已到期但仍 `accepted` 的列寫成 `expired_unsigned`，再回 `ATTEMPT_NOT_ACCEPTED` | 更保守；結果相同 |
| Signer DB 欄位 | `from`、`created_at`、`jti` | `payer`（`from` 是 SQL 保留字）、`accepted_at`；沒有 `jti`（A2 才有 webauthn） | 命名 |
| 錯誤碼 | 4.2 清單 | A2 的 `APPROVER_UNKNOWN`、`ASSERTION_INVALID`、`GRANT_REPLAYED` 沒有實作（webauthn 一律 `HUMAN_APPROVAL_UNAVAILABLE`）；`SIGNER_LOCKED` 不需要（只有解鎖成功才開始服務）；`INTENT_MISMATCH` 只在 Host client 用 | — |
| 回覆 | 4.2 | 多帶 `request_id`；`GET /v1/health` 回 `state: unlocked` | — |
| 金鑰檔參數 | 3.3 只說 scrypt＋AES-256-GCM | 版本 1；scrypt N∈[2^14, 2^20] 且為 2 的冪、r=8、p=1，新檔 N=2^17；salt 16、nonce 12 bytes；AAD 綁 wallet_id 與 address；只讀 `0600` 或更嚴的一般檔 | 實作選擇 |
| `verifyPay` 增加的檢查 | 6.2：放在 `verifyPay` | 放在 Store 的復原檢查 `verifyPayments`（schema ≥ 4 時對整張表做一次查詢），內容相同，另加 `updated_at ≥ requested_at` | 位置不同 |
| schema 4 | 6.4 DDL | 照 DDL，另加兩個 CHECK：簽署相關狀態必須有 `signer_attempt_id`；`rejected` 必須有 `error_code`；`delegation_ref` 不可為空字串 | 更嚴 |
| `delegation_ref` | 「委託 ID＋版本」 | 格式 `id@version`，Store 與 client 都驗 | — |
| Host 的請購範圍 | 6.1：`delegation` 是自動放行 | `BeginPayment` 只收 Policy 為 `Allow` 的 v1 請購；Ask（含已模擬核准的）一律 `PAYMENT_NOT_ALLOWED` | 依設計；明確化 |
| Host client 的測試 | — | 用記錄型 ledger 替身（實作 Store 的三個方法），因為 Store 的合成請購用 registry 的付款人地址，測試金鑰簽不了；Store 那一半由 `internal/store` 的測試涵蓋；`*store.Store` 滿足介面有編譯期斷言 | client 與真 Store 的端到端未在同一個測試裡跑 |
| 歷史檔 | — | 唯讀歷史檔接受 schema 2～4（原本只接受 2 與當時的 3），不遷移 | 讓既有 schema 3 證據檔照舊可讀 |

## 5 驗收對照

**本輪 run**：`artifacts/p1b/m1-check-20261012T134841Z/`（git 忽略），基準 `f434d10`，`tracked_clean: true`，證據完整。整個 run exit 1（下表第 9 步），總耗時 3 m 26 s。環境照第 1 節：測試 HOME 是新建的空目錄、`GOPROXY=off`、`-mod=readonly`、`GOTOOLCHAIN=local`；因 Unix socket 需要在工具沙箱外執行。

| # | 步驟 | 結果 | exit | 耗時 | 內容 |
|---|---|---|---|---|---|
| 1 | vet | PASS | 0 | 1.3 s | `go vet ./...` |
| 2 | crypto-keystore | PASS | 0 | 2.8 s | 5 個測試 |
| 3 | authorization | PASS | 0 | 2.1 s | 5 個測試 |
| 4 | state-race | PASS | 0 | 3.8 s | 9 個測試，`-race` |
| 5 | ipc-host-store-race | PASS | 0 | 14.1 s | 14 個測試，`-race` |
| 6 | adjacent-store | PASS | 0 | 13.3 s | 89 個測試（Store 全部含 M1、paycontract、paypolicy、m1evidence） |
| 7 | adjacent-store-race | PASS | 0 | 55.9 s | 32 個測試，`-race` |
| 8 | adjacent-demo | PASS | 0 | 4.2 s | 14 個測試（`TestP1a*`、`TestHistory*`、`TestHistoricalTaskKeepsItsBinding`） |
| 9 | reverse | FAIL | 1 | 1 m 48 s | 29 項中 28 項被預期斷言抓到；用到的每個測試未突變時都先通過（同一測試的基準只跑一次，後續項目沿用；第 28 項沿用第 23 項的 `TestPaymentDelegationPath` 基準） |

**未抓到的 1 項與補跑**：「P1b: R1 Store: B reads nothing of A's」的突變把 `consumer != consumerID` 換成 `false`，留下未使用的變數，突變副本編譯失敗（`declared and not used: consumer`），依規則不算抓到。這是**反向項目的缺陷，不是產品缺陷**。`0dd458a` 改成 `len(consumer) < 0`（可編譯、恆為假），只重跑這一項：`artifacts/p1b/rerun-20261012T135238Z/`，基準 `0dd458a`，同樣的環境設定，**抓到**（`TestPaymentDelegationPath: B read A's attempt: <nil>`），exit 0。第 9 步的原始失敗紀錄保留，不併成同一輪全過。

**反向項目與設計第 8 節的對照**（29 項，`python3 scripts/verify/m-b-reverse.py --list '^P1b:'`）：

| 群組 | 項目（拿掉的防護 → 命中的斷言） |
|---|---|
| R1 授權 | 委託版本比對 →「another version」；累計上限 →「over the cap」；委託綁 consumer 與錢包 →「A's delegation for B」；simulated 拒絕 →「simulated: … want GRANT_METHOD_NOT_ACCEPTED」；Store 的 consumer 範圍 →「B read A's attempt」（補跑） |
| 正式組裝 | `Unavailable{}` 換成放行的驗證器 →「the production assembly signed」 |
| R2 | 恢復跳過 →「recovery used the request, not the snapshot」；指紋比對 →「same ID, other content」；以快照地址選鑰 →「signed with another key」；查詢重驗期限 →「saved result refused after expiry」 |
| R3-a | 接受時的期限 →「accepted at the deadline」；最後判定的期限條件、改成 `<=`、改用接受時間 →「signed after the deadline」 |
| R3-b | 最後判定的狀態條件 →「signature returned after abandon」；abandon 取錢包鎖 →「abandon blocked by the signing lock」；abandon 的狀態條件 →「abandon overwrote signed」、「abandon after expiry」 |
| 密碼學／金鑰庫 | v 的位置與值 →「signature」；金鑰檔版本 →「other version: accepted」；scrypt 下限 →「weak scrypt: accepted」；金鑰檔權限 →「group-readable key file」 |
| IPC | caller uid 清單 →「a uid not configured」 |
| R4 Store | 同一請購只有一個 request ID →「resend 0」；method CHECK →「non-delegation payment attempt accepted」；復原檢查的 Allow 條件 →「want a payment corruption」；只付 Allow →「a simulated approval was paid」；Host 不寫 webauthn →「webauthn:」 |
| Host client | 無回覆記為 unknown →「no Signer」 |

**照實記的限制**：
- 有些防護有第二層：例如「同一請購只有一個 request ID」拿掉查找後，`intent_hash UNIQUE` 也會擋下第二筆；「Host 不寫 webauthn」拿掉 Host 的方法檢查後，schema 的 CHECK 也會拒絕寫入。反向項目證明的是測試能看見那一層防護被拿掉（預期斷言命中），不代表另一層不存在。
- 並行超額（R1）沒有單獨的反向項目：累計檢查同時受錢包鎖、單一寫入連線與 `BEGIN IMMEDIATE` 保護，拿掉其中一層不會改變結果；累計檢查本身由「the cumulative cap」覆蓋，`TestConcurrentCap` 在 `-race` 下通過。
- R3「Host 本地撤回、grant 尚有效」照設計是**被接受**，沒有測試；R1「Host 不持簽發鑰」由設計與程式（Host 沒有任何簽發金鑰）成立，沒有測試。
- caller uid 測試是同一個 uid（見第 6 節），不是跨使用者證據。
- Host client 與真 Store 沒有在同一個測試裡端到端執行（第 4 節）。

**不外洩**：掃描本輪證據，沒有任何測試金鑰、密語或測試金鑰簽出的授權。唯一一段 64 bytes 的 hex 是 EIP-712 規格公開範例的簽章（在「v」反向項目裡被突變了 v），屬公開測試向量。Signer 的 log 只有路由、狀態、錯誤碼、attempt ID 與 intent hash（`TestServer` 驗證 log 不含簽章與金鑰）。

**沿用／未跑**：node web 測試、瀏覽器流程、完整 P1-a 計畫、M1 產包、完整容器、全部突變、Unity 都沒有跑（本片沒有改 web 或 Unity）。正式 run 之前另做過一次整棵樹的 `go test ./...` 預檢（不是證據）：其中 `internal/buyer` 的 `TestVersion`（fake CLI `--version` 的計時，全部套件並行時 2.2 s 超過門檻）失敗一次，單獨重跑通過；它不在本片路徑上，本輪不列為結果。

## 6 OS 隔離（尚未驗收，本輪不執行）

> 10/13：最小部署方案見 [commercial-p1b-os-isolation.md](commercial-p1b-os-isolation.md)（P1-b OS 隔離部署最小方案完成，待 Codex review 與 Aaron 執行核准；尚未部署，正式付款未啟用）。方案取代下面第 3 步的 `scripts/verify/p1b-isolation.sh`：該腳本沒有撰寫，方案改用 macOS 內建命令與 `curl --unix-socket`。以下保留當時的文字。

本輪的 caller uid 測試在**同一個 OS 使用者**下進行：伺服器與客戶端都是目前的 uid，設定允許它就通過，不允許就 403。這證明程式讀的是核心回報的對端 uid，**不**證明 Agent／Host 在 OS 層讀不到金鑰或連不到 Signer。跨使用者的證據需要 Aaron 核准系統設定（決策 4）後才做，最小步驟：

1. 建立 `apa-signer` 使用者與群組（不能登入、沒有 shell），以及 socket 目錄（例如 `/var/run/apa-signer`，擁有者 `apa-signer`、群組為 Host 使用者可進入的群組、`0750`）。
2. 以 `apa-signer` 身分建立資料目錄（`0700`）、設定檔與**一次性未注資測試金鑰**（不是 A／B 展示金鑰），從終端機輸入密語啟動 `apa-signer serve --dir …`。
3. 執行 `scripts/verify/p1b-isolation.sh`（尚未撰寫，屆時與核准一起提交），以 Host 使用者與另一個非 Host 使用者各試：讀金鑰目錄與 DB（應被拒）、連 socket（Host 允許、其他 uid 403）、`ps`／`lsof` 看不到密語。
4. 結束後移除測試金鑰與帳號，或依核准保留。

## 7 本輪沒有做的

- 沒有建立系統使用者、群組、socket 目錄或服務；沒有改系統權限。
- 沒有建立、匯入、讀取或解鎖日常或 A／B 展示用付款金鑰；測試用的是公開向量金鑰（keccak256("cow")）與由原始碼中固定字串推導、未注資的一次性測試金鑰，金鑰檔只存在測試暫存目錄。
- 沒有連 RPC、faucet、facilitator，沒有部署或送交易；Signer 沒有接進 present-live／present-p1a 或任何展示流程。
- 沒有執行真實 Claude CLI、版本或登入查詢。

## 8 R1～R3 限定補正（Codex 對 `22e45ed` 的技術 review）

依 vault〈Claude-Code-P1-b實作R1-R3限定補正派工-22e45ed-20261012〉。Codex 的結論：P1-b 本輪限定技術 review 暫不通過，R1、R2、R3 都已用合成資料重現。這三項是實作缺口，不是重開設計的同名問題。

**前輪證據歸屬**（照實記，不寫成本輪驗收）：
- Codex 在 `22e45ed` 的暫存匯出副本加了三個定向探測，在斷網、來源唯讀、有資源上限的 Linux 容器裡執行：`TestCodexPaymentAfterCancel`（「cancelled task accepted a NEW payment request」）、`TestCodexHandlerDeadline`（5 秒期限後仍阻塞，釋放 DB 後才返回）、`TestCodexUnknownSaveFailure`（保存錯誤沒有傳回），三項都以斷言失敗重現。既有的 `TestPaymentDelegationPath`、`TestSignOne`、`TestProductionVerifierRefuses`、`TestPay` 通過；這四項不是完整套件，也沒有加 race。第一次容器探測少掛載新依賴的本機快取校驗檔，Signer／client 測試沒有啟動，補正掛載後才完成；這是環境失敗，不算產品失敗。Codex 的輸出只在對話工具紀錄，repo 沒有對應 log；前輪沒有修改產品，也沒有做 OS 跨使用者隔離、正式付款或真實 CLI。
- Claude Code 前輪的兩份紀錄不變：`artifacts/p1b/m1-check-20261012T134841Z/`（基準 `f434d10`，前八步 PASS，第九步 28／29，整輪 FAIL）與 `artifacts/p1b/rerun-20261012T135238Z/`（基準 `0dd458a`，只補跑那一項）。Codex 核對了摘要、輸出與突變修正，沒有重跑。兩次不能合寫成同一輪九步全過。

### 8.1 定向計畫（執行前）

- 計畫：`scripts/verify/p1b-r-check-plan.json`（7 步）；只列步驟與反向項目：`go run ./cmd/m1-check --plan scripts/verify/p1b-r-check-plan.json --list`。
- 執行：`sh scripts/verify/container-plan.sh scripts/verify/p1b-r-check-plan.json artifacts/p1b/r-<UTC 時間>`。
  - 容器旗標沿用 `container-verify.sh`：`--network none`（只有 lo）、唯讀根檔案系統（映像裡的 Go 工具鏈與工具不可寫）、commit 的 clone 唯讀掛在 `/src`、tmpfs `/scratch` 放 HOME、TMPDIR 與 build cache、`--user 10001`、`--cap-drop ALL`、no-new-privileges、記憶體／pids／CPU 與 ulimit 上限、整體 45 分鐘；不掛 host home、登入資料或 Docker socket；只設白名單環境變數。映像是既有的釘選驗證映像 `apa-verify:b1-05`（Go 1.26.4、python3、git、gcc）。
  - Go 模組：在 host 上**只從本機模組快取**（`GOPROXY=file://…`，不連網）依該 commit 的 go.sum 建一份，唯讀掛入。
  - 唯一可寫的掛載是新的證據目錄；容器先記錄隔離探測（外網、寫 `/src`、寫工具鏈、寫模組快取、上線的網卡、uid、版本與完整環境變數）到 `isolation.txt`，再跑 m1-check。
- 預估 6–10 分鐘（容器內 build cache 從零開始，含 race runtime）；每步上限見計畫。

| # | 步驟 | 範圍 | 上限 |
|---|---|---|---|
| 1 | vet | `go vet ./...`；也是所有套件（含只因介面改動需要重新編譯的測試）的編譯檢查 | 10 m |
| 2 | r1-store-race | `TestPaymentAfterCancel`（從未付款、unknown 後取消、Deny 後取消、CLI timeout 不算取消）與相鄰 `TestPayment*`、`TestUpgradeSchema3` | 10 m |
| 3 | r1-r3-client-race | `internal/signerclient` 全部：`TestRealStoreCancel`（真 Store＋真 Signer socket）、`TestUnknownSaveFailure`、`TestUnknownThenResume`、`TestReplyLost`、`TestPay` | 10 m |
| 4 | r2-signer-race | `TestLockWaitHonorsContext`、`TestHandlerDeadline`、`TestDeadlineAfterAccept`，以及相鄰的 IPC／恢復／期限／abandon／正例 | 10 m |
| 5 | production-refusal | `cmd/apa-signer`（正式組裝回 `RESERVATION_UNAVAILABLE`；組裝改了恢復的 context） | 10 m |
| 6 | adjacent-store-race | Store 的 Pay／Cancel／Upgrade／Recovery | 15 m |
| 7 | reverse | `P1b-R:` 7 項新增，加上因程式改寫而更新錨點的 2 項既有項目 | 30 m |

**反向項目**（每項先跑未突變基準；只算預期斷言命中）：

| 項目 | 拿掉的防護 | 測試 → 預期斷言 |
|---|---|---|
| R1 新付款 | `BeginPayment` 的取消檢查改成恆假 | `TestPaymentAfterCancel/a_purchase_never_paid` →「cancelled task accepted a new payment request」 |
| R1 既有 unknown | 同上 | `TestPaymentAfterCancel/an_unknown_attempt` →「handed out its unknown attempt for sending」 |
| R1 接線 | 同上 | `TestRealStoreCancel` →「sign sent after cancel」 |
| R2 鎖等待 | 錢包鎖等待不看 context | `TestLockWaitHonorsContext` →「lock wait ignored its cancellation」 |
| R2 DB 等待 | 依 request ID 查詢不帶 context | `TestHandlerDeadline` →「handler outlived its deadline」 |
| R3 保存失敗 | 只回 `Unknown`，丟掉保存錯誤 | `TestUnknownSaveFailure` →「save failure hidden」 |
| R3 回傳更新後的紀錄 | 不採用更新後的 attempt | `TestUnknownThenResume` →「no Signer」 |
| 既有（錨點更新） | abandon 取錢包鎖（`Abandon` 簽名多了 ctx） | `TestAbandonRaces/abandon_first` →「abandon blocked by the signing lock」 |
| 既有（錨點更新） | 無回覆時不記 unknown（程式改寫） | `TestUnknownThenResume` →「no Signer」 |

**不在本計畫**：原九步全部、原 29 項中其餘 27 項、完整容器七步、全部突變、M1 產包、瀏覽器、Unity、真實 CLI、cmd/demo（本輪沒有改 demo 的路徑；其 P1a／History／Cancel 測試在提交前的本機預檢通過，不算證據）。

### 8.2 修正與正反例對照

| 項目 | 來源（基準 `22e45ed`） | 修正（`a3d33aa`） | 正例 | 反例 |
|---|---|---|---|---|
| R1 取消後的新付款 | `internal/store/payment.go` `BeginPayment`；`internal/signerclient/client.go` `Pay` | `BeginPayment` 在它自己的寫入交易裡看 task 的事件：已提交 `TASK_CANCELLED` 就回 `TASK_ALREADY_ENDED`，**既有的** requested／unknown 列也不再交出去（不是 Replay），所以 client 的 `Pay` 不送 sign。看的是事件而不是 `project()` 的狀態，因為先有 Deny 再取消的 task 狀態仍是 `denied`。`Payment`、`UpdatePayment`、`Resume`、`Abandon` 照舊，同一個 request ID | 取消前：付款一次、重送同一個 ID 與簽章；取消後：signed 的查詢回原授權，unknown 的查詢與對帳照常；CLI timeout 不算取消（未改） | 取消後首次付款被拒、列數不增、client 沒送 sign；unknown 後取消也不重送；先 Deny 後取消一樣被拒 |
| R2 處理期限 | `internal/signer/server.go` Handler；Core 的鎖等待與 DB 操作 | request context 傳進 `Core.Sign`／`Get`／`Abandon`：錢包鎖等待同時看 context（用同一個剩餘期限，不重新起算；`lockWait` 3 秒仍是另一個上限），所有 DB 呼叫用 `…Context`。期限讓工作結束時，Handler 回 503 `SIGNER_TIMEOUT`。啟動恢復用自己的有界 context（`apa-signer` 60 秒）。最後判定的期限與狀態條件、abandon、保存後才回傳都沒改；request 的期限與 `auth_deadline` 是兩件事 | 期限到後下一個正常請求照常；接受後才逾時的 attempt 維持 accepted、同一個 request ID 完成簽署；相鄰的 IPC、恢復、R3-a、R3-b、正例都通過 | 握手確認在等錢包鎖後取消，或給 100 ms 期限：都在期限內結束、沒有保留任何 attempt；握手確認在等 DB 唯一連線（pool 的等待計數）後，經由真的 Handler 在期限內回 `SIGNER_TIMEOUT` |
| R3 unknown 保存失敗 | `internal/signerclient/client.go` `settle` | 檢查 `UpdatePayment` 的錯誤：失敗時回 `Unknown` 與 `NotSaved`（`PAYMENT_STATE_NOT_SAVED`）的組合錯誤，兩者都能用 `errors.Is` 辨認；成功時回更新後的 attempt。不重試、不換 ID | 保存成功：回 Unknown，回傳的 attempt 與 ledger 都是 unknown；保存恢復後同一 ID 查詢、再付款 | 保存失敗：同時是 Unknown 與 NotSaved，回傳的 attempt 仍是 requested、沒有宣稱已存 unknown，只送過一次 sign |

**新增與更新的測試**：`internal/store/payment_test.go` `TestPaymentAfterCancel`；`internal/signerclient/store_test.go` `TestRealStoreCancel`（真 Store＋真 Signer socket，計數的傳輸替身；Store 的 registry 是測試專用的副本，兩個錢包改成測試金鑰的地址，共用凍結 fixture 不變，付款人驗證不放寬）；`internal/signer/deadline_test.go` `TestLockWaitHonorsContext`、`TestHandlerDeadline`、`TestDeadlineAfterAccept`；`client_test.go` `TestUnknownSaveFailure`。`TestUnknownThenResume` 原本斷言回傳的是舊的 requested 紀錄（等於把 R3 的缺陷寫進了測試），現改為斷言 unknown。

**介面變動**：`Core.Sign`／`Get`／`Abandon` 與 `NewCore` 多了 `context.Context` 參數；Signer 多一個錯誤碼 `SIGNER_TIMEOUT`（503，client 記為 unknown）；client 多一個 `NotSaved`。信任邊界、限額規則、契約 hash、schema 與付款能力都沒有改，也沒有新依賴。

**照實記的限制**：
- 取消只擋住**之後**的付款請求。取消在 `BeginPayment` 交易之後才提交時，已送出的 sign 不會被召回；這沿用既有的 attempt／abandon 規則，沒有新增全域即時撤回。已 signed 的授權不能撤回。
- CLI timeout（`TASK_TIMED_OUT`，狀態 `failed`）與 Deny 不在 R1 範圍，行為沒變：timeout 後的 Allow 請購仍可以建立付款嘗試。是否也要擋，請 Codex／Aaron 決定。
- R2 的 5 秒是 Handler 的常數；測試用較短的 request context 證明期限會傳遞，沒有另外等滿 5 秒。
- 跨 OS 使用者的隔離仍未驗收（第 6 節）。

### 8.3 本輪驗證

| 執行 | 基準 | 結果 |
|---|---|---|
| 本機預檢（不是證據） | `ca6c600` | 提交前本機跑過受影響套件與 `cmd/demo` 的 P1a／History／Cancel 測試，通過；9 項反向本機預跑 9／9（`scratchpad`，不保留） |
| 第一次容器 run：`artifacts/p1b/r-20261012T164216Z/` | `ca6c600` | **設定失敗，容器沒有啟動，沒有測試執行**：`container-plan.sh` 在設定了新的 `GOMODCACHE` 之後才讀本機快取路徑，`go mod download` 找不到檔案（`modules.txt`）。這是驗證工具的缺陷，不是產品失敗；`1bfbe44` 修正 |
| 正式容器 run：`artifacts/p1b/r-20261012T164244Z/` | `1bfbe44`（`tracked_clean: true`，證據完整） | 7 步全部 PASS，exit 0，總耗時 4 m 37 s（見下表） |

| # | 步驟 | 結果 | exit | 耗時 | 內容 |
|---|---|---|---|---|---|
| 1 | vet | PASS | 0 | 48.1 s | `go vet ./...`（含所有套件的編譯） |
| 2 | r1-store-race | PASS | 0 | 1 m 20 s | 6 個測試，`-race` |
| 3 | r1-r3-client-race | PASS | 0 | 17.2 s | 5 個測試，`-race` |
| 4 | r2-signer-race | PASS | 0 | 5.1 s | 16 個測試，`-race` |
| 5 | production-refusal | PASS | 0 | 9.8 s | 2 個測試 |
| 6 | adjacent-store-race | PASS | 0 | 1 m 17 s | 29 個測試，`-race` |
| 7 | reverse | PASS | 0 | 40.2 s | 9／9 被預期斷言抓到（第 8.1 節的表；每項的未突變基準先通過） |

實際命中的斷言（`07-reverse.log`）：「abandon blocked by the signing lock」「no Signer」（兩項）「cancelled task accepted a new payment request: <nil>」「cancelled task handed out its unknown attempt for sending」「po-3 after cancel: <nil>; sign sent after cancel: 3 -> 4, attempts 2 -> 3」「lock wait ignored its cancellation」「handler outlived its deadline」「save failure hidden: SIGNER_OUTCOME_UNKNOWN」。

**隔離紀錄**：`isolation.txt`（run 自己的容器）：外部 HTTPS 與 IP 都被拒；寫 `/src`、Go 工具鏈、模組快取都被拒；uid 10001；Go 1.26.4 linux/amd64；完整環境變數清單（只有白名單與映像內建的 `PATH`、`GOLANG_VERSION`、`HOSTNAME`、`PWD`）。其中「上線網卡」那一行因為探測寫法錯誤（`/sys/class/net/bonding_masters` 不是目錄）而**沒有記到**；run 結束後另開一個同樣 `--network none`、同 uid 的容器補測，記在 `isolation-interfaces.txt`：只有 `lo` 上線、沒有 IPv4 路由。這是補充紀錄，不是同一個容器實例；探測寫法已在之後的提交修正。

**不外洩**：掃描本輪證據，沒有 64 bytes 的 hex、測試密語或金鑰。

**沒有跑**：原九步全部、原 29 項中其餘 27 項、完整容器七步、全部突變、M1 產包、瀏覽器、Unity、真實 CLI；`cmd/demo` 只有提交前的本機預檢。原 `m1-check-20261012T134841Z`（整輪 FAIL）與 `rerun-20261012T135238Z` 的紀錄不變，也不和本輪合併。

## 9 R2 回覆分類限定補正（Codex 對 `fcc5095` 的限定複核）

依 vault〈Claude-Code-P1-b實作R2回覆分類限定補正派工-fcc5095-20261013〉。Codex 的結論：R1、R3 本輪限定複核關閉（取消後不新增或重送付款；unknown 保存錯誤與更新後紀錄的處理）。R2 還剩一項：Signer 已接受請求後 context 結束，Handler 卻回 `404 ATTEMPT_UNKNOWN`。P1-b 不標 Done。

**前輪證據歸屬**（照實記）：
- Claude Code 執行、Codex 核對：`artifacts/p1b/r-20261012T164244Z/`（基準 `1bfbe44`，七步 PASS、反向九項命中；Codex 沒有重跑）；`artifacts/p1b/r-20261012T164216Z/`（設定階段失敗、測試容器沒有啟動，原樣保留）；正式 run 的網卡列舉探測失敗，另開同旗標容器的 lo／路由補測是補充紀錄，不是原實例的觀測。
- Codex 獨立執行：在 `fcc5095` 的暫存匯出副本、斷網且來源唯讀的 Linux 容器，以測試資料跑 `TestHandlerDeadline`、`TestDeadlineAfterAccept`、`TestRealStoreCancel`、`TestUnknownThenResume`、`TestUnknownSaveFailure`、`TestPaymentAfterCancel`，全部通過；另加的 `TestCodexHandlerAfterAcceptDeadline` 以「accepted attempt misclassified after request cancellation: HTTP=404 code=ATTEMPT_UNKNOWN persisted_state=accepted; want HTTP=503 SIGNER_TIMEOUT」失敗重現缺口（在 after-accept 取消 request context，經實際 Handler）。這組沒有加 race，不是完整套件或完整容器驗證；輸出只在 Codex 對話工具紀錄，沒有 repo log 路徑。沒有觀察到重複簽署或付款。

### 9.1 根因與修正

- **根因**：`core.finish` 把「讀取失敗」和「讀到但不存在」合在一起，回 `errors.Join(err, Refusal("ATTEMPT_UNKNOWN"))`；Handler 先處理 Refusal，蓋掉了 context 失敗，所以接受後才結束的請求被回成 404。client 收到沒有狀態的 404，走的是明確拒絕，不會記 unknown。
- **修正**（只改錯誤來源）：讀取失敗原樣回傳；只有讀取成功而且確實沒有紀錄才回 `ATTEMPT_UNKNOWN`。Handler 不用改：非拒絕的錯誤在 request context 已結束時本來就回 `503 SIGNER_TIMEOUT`，其他錯誤回 `SIGNER_ERROR`，回覆與 log 都不帶原始 DB 錯誤。`Get`、`Abandon` 原本就分開這兩種情況。client 不用改：它對 503 已經記 unknown、回 `Unknown`、不重送；本輪補的是接線測試。
- **相鄰影響**：啟動恢復走同一個 `finish`。以前恢復時讀取失敗會被當成拒絕而略過，現在會讓 `NewCore` 回錯誤。`TestFinishReadError` 在 Core 層驗這一點。
- 不變：request 處理期限（5 秒）、`auth_deadline`、最後寫入前的到期檢查、abandon 競爭、保存後才回傳簽章；accepted 紀錄不刪；`Resume` 只查詢，不簽新授權，完成 accepted attempt 沿用同一 request ID 的既有路徑，沒有新增自動重試。

### 9.2 定向計畫（執行前）

- 計畫：`scripts/verify/p1b-r2b-check-plan.json`（5 步）；只列：`go run ./cmd/m1-check --plan scripts/verify/p1b-r2b-check-plan.json --list`。
- 執行：`sh scripts/verify/container-plan.sh scripts/verify/p1b-r2b-check-plan.json artifacts/p1b/r2b-<UTC 時間>`，同一個釘選映像與隔離方式（第 8.1 節），模組只取自本機快取，不下載、不重建映像。預估 4–6 分鐘。

| # | 步驟 | 範圍 | 上限 |
|---|---|---|---|
| 1 | vet | `go vet ./...`（所有套件的編譯檢查） | 10 m |
| 2 | r2b-signer-race | 新測試 `TestHandlerAfterAccept`（cancel、deadline）、`TestFinishReadError`、`TestClientAfterAccept`、`TestClientAfterAcceptNotSaved`；相鄰的期限、恢復、abandon、IPC | 10 m |
| 3 | client-race | `TestUnknownThenResume`、`TestUnknownSaveFailure`、`TestReplyLost`、`TestRealStoreCancel` | 10 m |
| 4 | production-refusal | `cmd/apa-signer`（正式組裝與恢復） | 10 m |
| 5 | reverse | `P1b-R2b:` 4 項：把 `finish` 改回合併的錯誤 | 20 m |

| 反向項目 | 測試 → 預期斷言 |
|---|---|
| Handler，取消 | `TestHandlerAfterAccept/cancel` →「accepted attempt answered 404 ATTEMPT_UNKNOWN」 |
| Handler，期限 | `TestHandlerAfterAccept/deadline` →「accepted attempt answered 404 ATTEMPT_UNKNOWN」 |
| client 接線 | `TestClientAfterAccept` →「client took an accepted attempt for never accepted」 |
| Core | `TestFinishReadError` →「a failed read became a refusal」 |

**不在本計畫**：上輪七步、上輪九項反向、原九步／29 項、完整容器流程、全部突變、M1 產包、瀏覽器、Unity、真實 CLI；Store 沒有改動，不跑 Store 套件。

### 9.3 本輪驗證

| 執行 | 基準 | 結果 |
|---|---|---|
| 本機（不是證據） | `07b16fd` 提交前 | 提交前本機跑 `internal/signer`、`internal/signerclient`、`cmd/apa-signer`（`-race`），通過 |
| 第一次容器 run：`artifacts/p1b/r2b-20261012T172925Z/` | `935de2f` | **沒有啟動容器，沒有測試執行**：`docker image inspect apa-verify:b1-05` 那一刻回「No such image」，runner 帶著空的映像 ID 繼續，`docker run` exit 125（`invalid reference format`）。隨即查證映像仍在（ID `78258368de09`，與上輪同一個），判定為 Docker 的暫時錯誤；不修改、不重建映像，同一個 commit 重跑 |
| 正式容器 run：`artifacts/p1b/r2b-20261012T172959Z/` | `935de2f`（`tracked_clean: true`，證據完整） | 5 步全部 PASS，exit 0，總耗時 2 m 18 s |

| # | 步驟 | 結果 | exit | 耗時 | 內容 |
|---|---|---|---|---|---|
| 1 | vet | PASS | 0 | 47.9 s | `go vet ./...`（含所有套件的編譯；本輪沒有介面變動，沒有套件只因介面而需要重新編譯） |
| 2 | r2b-signer-race | PASS | 0 | 1 m 2 s | 13 個測試，`-race` |
| 3 | client-race | PASS | 0 | 4.3 s | 4 個測試，`-race` |
| 4 | production-refusal | PASS | 0 | 9.7 s | 2 個測試 |
| 5 | reverse | PASS | 0 | 13.8 s | 4／4 被預期斷言抓到（每項的未突變基準先通過） |

實際命中的斷言（`05-reverse.log`）：
- `TestHandlerAfterAccept/cancel`、`/deadline`：「accepted attempt answered 404 ATTEMPT_UNKNOWN; want 503 SIGNER_TIMEOUT」
- `TestClientAfterAccept`：「client took an accepted attempt for never accepted: ATTEMPT_UNKNOWN」
- `TestFinishReadError`：「a failed read became a refusal: context canceled」

**驗證內容**（第 2、3 步）：
- Handler：hook 在接受提交後（握手：已提交 1 筆 accepted）取消 request context，或讓 100 ms 的測試期限在同一點到達（hook 等 `ctx.Done()`，不用 sleep 猜位置）：兩者都回 `503 SIGNER_TIMEOUT`、沒有授權；新的查詢讀到同一筆 accepted；同一 request ID 的既有路徑完成簽署，attempt 仍是 1 筆，signed 可查回；從沒見過的 ID 仍是 `404 ATTEMPT_UNKNOWN`。正式 5 秒值不變。
- client 接線：client 經 Unix socket 打實際 Handler，測試記下 Handler 真正回的是「503 SIGNER_TIMEOUT」；client 回 `Unknown`，Result 與 ledger 都是 unknown，request ID 不變，只送過一次 sign；`Resume` 讀到 accepted、沒有授權、沒有送 sign；之後同一 ID 的 `Pay` 完成，attempt ID 不變，signed 可查回。保存 unknown 失敗時仍同時是 `Unknown` 與 `NotSaved`（R3 沒有退回）。
- 相鄰：接受前的 DB／錢包鎖中斷（`TestHandlerDeadline`、`TestLockWaitHonorsContext`）、`TestDeadlineAfterAccept`、三個中斷點與恢復、R3-a 期限、R3-b abandon、金鑰版本、委託先到期、IPC，以及 client 的 unknown／回覆遺失／取消接線，都通過。

**隔離紀錄**（`isolation.txt`，本次 run 自己的容器）：外部 HTTPS 與 IP 被拒；寫 `/src`、Go 工具鏈、模組快取被拒；上線網卡只有 `lo`（探測已在 `fcc5095` 修正，本次有記到）；uid 10001；Go 1.26.4；環境變數清單同第 8.3 節。

**不外洩**：掃描本輪證據，沒有 64 bytes 的 hex、測試密語或金鑰。

**沒有跑**：上輪七步、上輪九項反向、原九步／29 項、完整容器流程、全部突變、M1 產包、瀏覽器、Unity、真實 CLI、Store 套件（沒有改動）。前面各輪的紀錄（`m1-check-20261012T134841Z`、`rerun-20261012T135238Z`、`r-20261012T164216Z`、`r-20261012T164244Z`）不變，也不和本輪合併。

**另列決策，不在本輪**：CLI timeout 後是否禁止新的付款（目前行為不變；那是 task 的 timeout，與 Signer 的 request timeout 是不同問題）。

### 9.4 限定驗收（10/13）

依 vault〈Claude-Code-P1-b實作R1-R3限定驗收同步派工-a33ebf7-20261013〉。本節只同步驗收結論；本輪沒有改程式，也沒有重跑任何驗證。

**結論**（Codex）：

> 程式基準 07b16fd、測試與計畫基準 935de2f 通過 Codex 本次限定複核。R2 回覆分類問題關閉；R1、R3 維持關閉。本輪未發現新的阻擋項目。OS 隔離部署尚未驗收，正式付款仍未啟用。

`935de2f..a33ebf7` 只有文件差異。

**R2 關閉理由**：
- `finish` 已分開「讀取失敗」與「成功讀取但不存在」：前者不再附上 `ATTEMPT_UNKNOWN`，真正不存在仍回 404。
- 接受後取消 context 或期限到達，Handler 回 `503 SIGNER_TIMEOUT`，保留原 attempt，不回傳授權。
- client 經實際 Handler／Unix socket 收到 503 後保存 unknown，不自動重送；同一 request ID 可查詢與恢復，`Resume` 不觸發簽署。
- unknown 保存失敗時仍能同時辨認 `Unknown` 與 `NotSaved`，R3 沒有退回。
- 五步 PASS 與四項反向檢查的原始紀錄相符；四項都命中預期斷言，不是編譯失敗。

本次依 security-audit 的限定複核範圍進行，不等於全面安全稽核，也不代表已驗收 macOS 跨使用者隔離或正式付款能力。

**證據歸屬**：
- **Claude Code 執行，Codex 核對紀錄**：`artifacts/p1b/r2b-20261012T172959Z/`，基準 `935de2f`，五步 PASS、反向 4／4。隔離紀錄（只有 `lo` 上線、寫入被拒）來自該次容器的執行紀錄，不是 Codex 本輪親自探測。
- **Codex 本輪唯讀核對**：Git 差異、產品原始碼、Handler／client／Core 測試、計畫、摘要、正向與反向輸出及隔離紀錄。Codex 沒有獨立重跑測試或容器，沒有執行真實 CLI、讀取正式金鑰，也沒有改動產品或證據。
- **前輪 Codex 獨立探測**：見第 9 節開頭的紀錄（`fcc5095` 匯出副本上的六項既有測試與 `TestCodexHandlerAfterAcceptDeadline`）。那是修正前的探測，與本輪唯讀複核分開，沒有在修正後的基準重新執行。
- `artifacts/p1b/r2b-20261012T172925Z/`：未啟動容器的失敗紀錄原樣保留，不改寫成成功，也不與正式 run 合併；當時映像查詢異常的根因沒有獨立確認。
- `TestFinishReadError` 是 Core 層的錯誤分類測試，不是部署或啟動故障演練。

**之後**：整個 P1-b 不標 Done。下一步為 OS 隔離部署規劃與另行核准（本輪不撰寫部署方案、不執行部署）。CLI timeout 後是否禁止新付款，繼續另列決策。
