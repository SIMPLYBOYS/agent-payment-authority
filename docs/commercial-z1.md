# Z1 ZK 額度電路 v1 spike：電路、量測、`intent_hash` 映射與 Z2 前置條件

> 2026-10-14，Claude Code。依 vault〈Claude-Code-Z1-ZK額度電路v1Spike派工-20261013〉與 Aaron「我們先做 Z1」。分支 `commercial-z1`（從 main `87c9ebf`）。
> **狀態：spike 完成；第一次獨立上下文 review not pass，修正後第二次 review pass（第 10 節）。** 開工時查證：repo 與 vault 都沒有 Z1／S3 的既有交付（`spikes/` 只有 `bbs`、`webauthn`），因此從頭建立。
> 本片**只證明本機 proof primitive 可行**：不接 Policy、Signer、Store，不建合約、不部署、不碰 testnet、RPC、faucet 或付款。資料全部合成；setup 是本機單方 setup，不是可信儀式。**不能宣稱預算已被鏈上保護。**

## 1 結論（先講）

- gnark v0.16.3 的 Groth16（BN254）可以在本機證明「一次預算扣款」：私密餘額能開啟舊承諾、扣款後不為負、新承諾等於剩下的餘額、nullifier 綁定 task 與 `r_old`、`intent_hash` 綁在 proof 上。
  - 正例通過；11 個無法成立的 witness 都產不出 proof；7 種竄改公開輸入都驗不過。
  - 一個 tx#1 的 Go 模型只收公開值，擋下重播、舊狀態、別的 task、未登記的 task、已用的 nullifier，以及非標準寫法（值 + r）的公開輸入。
- 成本很低：3,096 個約束；prove 中位數約 45 ms、verify 約 0.9 ms；proof 164 bytes；proving key 約 604 KB；記憶體峰值約 23～25 MB（第 5 節）。
- **`intent_hash` 映射採「兩個 128 位元 limb」**（第 4 節選項 A；Z1-D1，Aaron 10/14 決定）：一對一、不截斷、合約端拆 `bytes32` 很便宜。proof 不綁定 `amount` 與 intent 內金額的一致性，是已知限制，後續再看是否調整。
- **三個要帶進 Z2 的發現**：
  1. gnark 會照常編譯「公開輸入沒有出現在任何約束裡」的電路，而這個輸入填什麼值都會驗過。反向檢查把 `intent_hi` 或 `intent_lo` 唯一的約束拿掉後，竄改它的 proof 就通過了。每個公開輸入都要有「竄改即驗不過」的測試。
  2. gnark 的 Go verifier 會把公開輸入先取模 r：同一個 proof 拿「值 + r」去驗也會通過（`TestAliasedPublicInputs`）。所以「竄改即驗不過」只在輸入是標準值（[0, r)）時成立。合約與 verifier 必須自己拒絕 ≥ r 的輸入，並用同一個標準值做承諾與 nullifier 的比對（第一次 review 的 M1）。
  3. 只檢查「扣款後餘額在 64 位元內」擋不住**負的金額**（欄位上 r−k），金額本身也要做範圍檢查。反向檢查已證實這一點。

## 2 版本與依賴

| 項目 | 選擇 | 理由 |
|---|---|---|
| gnark | **v0.16.3**（2026-08-24） | 理由如下 |
| gnark-crypto | v0.21.0 | gnark v0.16.3 指定；與主模組（idemix 的間接依賴）相同版本 |
| 授權 | Apache-2.0（gnark、gnark-crypto、pprof、intcomp）；MIT（semver、cbor、go-colorable、go-isatty、zerolog、float16）；BSD-3（bitset、golang.org/x/*） | 證據 `licenses.txt` |
| 模組 | `spikes/zk/` 獨立 go module | 主 `go.mod` 不變，`go test ./...` 不會帶進來（與 C1 的 `spikes/bbs` 相同做法） |
| 建置成本 | 下載約 14 個模組；編譯電路約 9 ms | — |

**為什麼是 v0.16.3**（安全公告於 2026-10-14 以 GitHub API `repos/consensys/gnark/security-advisories` 查證；版本日期取自 proxy.golang.org）：
- 已修正 GHSA-3mvx-pp85-pm65（critical，`<v0.16.2` 的 std 電路元件 soundness）。v0.16.3 自己的變更是「small field 的 quotient range check」，與 BN254 無關。
- 最新的 v0.16.4（10/07）修正 GHSA-7fx8-hmgc-82jp（high，模擬曲線 fake-GLV 純量乘法，影響 ECDSA／P256Verify）。本電路只用原生 `ToBinary` 與 MiMC，用不到這個元件。
- v0.16.4 要求 Go ≥ 1.26.8，本機是 1.26.4，用它還得另外下載工具鏈。
- **Z2 進產品前要升到 ≥ v0.16.4。**

## 3 電路（`spikes/zk/zk.go`）

**公開輸入**（順序固定）：`task, C_old, amount, C_new, N, intent_hi, intent_lo`。**私密**：`balance_old, r_old, r_new`。

| 約束 | 內容 |
|---|---|
| 範圍 | `balance_old`、`amount`、`balance_new = balance_old − amount` 都在 [0, 2^64)（各一次 `ToBinary(·, 64)`）；扣過頭時差值在欄位上約為 2^254，過不了範圍檢查 |
| 舊承諾 | `C_old = H(D_commit, task, balance_old, r_old)` |
| 新承諾 | `C_new = H(D_commit, task, balance_new, r_new)` |
| nullifier | `N = H(D_nullifier, task, r_old)` |
| intent | `intent_hi`、`intent_lo` 各在 [0, 2^128)（這同時讓它們出現在約束裡；見第 1 節發現 1） |

- `H` 是 BN254 純量欄位上的 MiMC（gnark `std/hash/mimc` 與 gnark-crypto 原生版一致，正例 proof 依賴兩者相同）。`D_commit`、`D_nullifier` 是由標籤 `agent-payment-authority/zk/v1/…` 的 SHA-256 取模得到的區隔常數。
- `task` 的欄位值：`SHA-256("agent-payment-authority/zk/v1/task\0" ‖ task_id)` 的前 31 bytes（248 位元，小於 r；碰撞成本約 2^124）。這也是一種映射，交 Z2 決定（Z1-D2）。
- **nullifier 的語意**：nullifier 綁的是 task 與 `r_old`，不是餘額。每個 `r_old` 只能花一次，prover 每次都要用新的隨機 `r_new`。
  - 在「合約只存每個 task 一個承諾、並且原子更新」的設計下，`commitment[task] == C_old` 本身就擋重播，nullifier 是第二道防線。兩者都保留到 Z2 再決定是否精簡。
  - 重複使用或遺失 `r_new` 的後果見第 8 節 (d)：預算不會被多花，但這個 task 會卡住。

## 4 `intent_hash` 進公開輸入的映射（ADR payment-account-model 開放問題 4）

`intent_hash` 是 SHA-256 的 256 位元，BN254 純量欄位 r ≈ 2^253.6，放不進一個欄位元素。

| 選項 | 做法 | 一對一 | 成本 | 合約端 | 評估 |
|---|---|---|---|---|---|
| **A 兩個 128 位元 limb**（spike 採用） | `hi = h[0:16]`、`lo = h[16:32]`，各範圍檢查 128 位元 | 是 | 2 個公開輸入、約 258 個約束；鏈上每多一個公開輸入約多 6～7k gas（一次 ecMul 加一次 ecAdd） | `uint256(h) >> 128`、`uint128(uint256(h))` | **建議**。不截斷、不改契約語意 |
| B 取模 r | `h mod r` | 否（2^256／r ≈ 5.29，每個欄位值有 5 或 6 個 256 位元原像） | 1 個公開輸入 | 合約要做同樣的取模 | 不建議：一般性的取模碰撞約需 2^126.8，與 SHA-256 同級；但「同一個欄位值對應多個 intent」不應出現在付款契約裡 |
| C 截斷 248 位元 | 取前 31 bytes | 否（碰撞約 2^124） | 1 個公開輸入 | 合約截斷 | 安全上可接受，但 A 一樣便宜又不截斷，沒有理由選 C |
| D 改用欄位內的雜湊 | 契約的 `intent_hash` 改成 Poseidon／MiMC | — | 低 | 合約與 Signer 要實作同一個雜湊 | 改變凍結的 v1／v2 契約語意，需要新契約版本；不在 Z1 範圍 |
| E 電路內重算 SHA-256 | 把 intent 欄位作為私密輸入，電路內算 SHA-256 | 是 | 每個 64 bytes 區塊數萬個約束 | 不需拆分 | 可以在 proof 內綁住「amount 就是 intent 裡的 amount」，但成本高一到兩個數量級；Z2 若要這個保證再評估 |

- **電路內的範圍檢查不能防別名**：選項 A 的 128 位元檢查限制的是約束系統裡的值，但 verifier 收到的公開輸入若是「值 + r」，取模後仍相同（第 1 節發現 2）。所以合約要自己從 `bytes32` 拆出兩個 limb，不能從 calldata 收。
- **綁定的範圍**：選項 A 只保證「proof 綁在這個 `intent_hash`」，**不保證** `amount` 公開輸入等於 intent 裡的金額。這要由合約或 Signer 在電路外比對：合約 emit `intent_hash` 與 `amount`，Signer 簽前核對兩者與自己算出的 intent 一致。這列為 Z2／P2 的前置條件，不是 Z1 已解決的事。

## 5 量測（`z1-192df5b`，50 次 proof）

| 項目 | 數值 |
|---|---|
| 機器 | darwin/amd64，8 CPU，go1.26.4（未啟用 GPU 加速） |
| 約束數／公開輸入／私密輸入 | 3,096／7／3（MiMC 約 2,640，範圍檢查約 195＋258） |
| setup（本機單方） | 304 ms |
| prove | 中位數 44.5 ms |
| verify（Go 原生） | 中位數 0.92 ms |
| proof／verifying key／proving key | 164 bytes（gnark 壓縮編碼；給 Solidity 的 calldata 是 256 bytes）／556 bytes／604,367 bytes |
| 每次 proof 配置的記憶體 | 約 1.7 MB；程序記憶體峰值約 24.9 MB |

- 數據來自 `cmd/measure`（`measure.json`）。第一版 `z1-61f06a7` 的數字幾乎相同（setup 338 ms、prove 45.0 ms、verify 0.93 ms、峰值 22.8 MB）；兩次之間電路沒有改。
- 只有一台機器、一種電路大小，僅供數量級參考。Go 的 verify 時間不能推算鏈上 gas，鏈上成本由 Z2 量測。

## 6 測試與反向（`spikes/zk/zk_test.go`、`reverse.py`）

- **正例**：一筆 0.50 的扣款 proof 驗證通過；把整個餘額花完（`balance_new = 0`）也通過。
- **prover 產不出 proof**：witness 建不起來視為測試失敗，所以每一項的拒絕都來自 `groth16.Prove` 的約束檢查。
  - 只違反一個約束的案例：扣過頭、負的金額、謊報比承諾更高的餘額、`C_new` 不是剩下的餘額、別的 task 的 nullifier、承諾到 2^64 的餘額、別的 task 的承諾、intent hi limb 為 2^128、intent lo limb 為 2^128。
  - 有多個原因的兩個案例，名稱已寫明：錯的 `r_old`（承諾開啟與 nullifier 都失敗）、金額為 2^64（`balance_new` 也繞回）。
  - 原生端的 `Assignment` 也拒絕扣過頭。
- **竄改公開輸入**（同一個有效 proof）：換金額、換 task、舊的 `C_old`、換 `C_new`、換 nullifier、換 intent 的 hi 或 lo limb，全部驗不過。
- **公開輸入的別名**（`TestAliasedPublicInputs`）：`amount + r`、`nullifier + r` 在 gnark 的 Go verifier 都驗得過（記錄函式庫的行為）；帳本模型以 `PUBLIC_INPUT_NOT_CANONICAL` 拒絕。
- **tx#1 的 Go 模型**（不是合約）：只收七個公開值；先拒絕 [0, r) 以外的值，再用這些值驗證 proof，最後用同一組標準值判斷 `commitment[task] == C_old` 與 nullifier 未用，原子寫入 `C_new`。
  - 重播 → `STALE_COMMITMENT`；同一舊狀態的第二筆 proof → `STALE_COMMITMENT`。
  - 換成 proof 沒有證明的 `C_new` → `PROOF_INVALID`。
  - 新狀態的下一筆 → 通過。
  - 兩個 task 都登記後，把 A 的 proof 當成 B 的送出 → `PROOF_INVALID`；未登記的 task → `STALE_COMMITMENT`。
  - 把承諾還原後重送舊 proof → `NULLIFIER_USED`；同一個 nullifier 寫成 N + r → `PUBLIC_INPUT_NOT_CANONICAL`。
- **race**：通過。
- **反向**（在一次性副本拿掉一個約束，要求對應測試以預期斷言失敗）：`192df5b` 10／10。

| 拿掉的約束 | 抓到它的測試 |
|---|---|
| `amount` 的範圍檢查 | 負的金額 → proof 產生了 |
| `balance_new` 的範圍檢查 | 扣過頭 → proof 產生了 |
| `balance_old` 的範圍檢查 | 承諾到 2^64 的餘額 → proof 產生了 |
| 舊承諾開啟 | 謊報較高餘額 → proof 產生了 |
| 新承諾 | `C_new` 不是剩下的 → proof 產生了 |
| nullifier | 別的 task 的 nullifier → proof 產生了 |
| `intent_hi` 的範圍檢查 | hi limb 為 2^128 → proof 產生了；換 hi limb → **驗證通過**（它唯一的約束） |
| `intent_lo` 的範圍檢查 | lo limb 為 2^128 → proof 產生了；換 lo limb → **驗證通過** |

- 歷程：
  - 開發時的一次試跑是 5／6：「舊承諾開啟」原本用「錯的 `r_old`」測，但錯的 `r_old` 同時讓 nullifier 對不上，所以改成「謊報較高餘額」。這次試跑在提交之前，證據沒有保留。
  - 第一版提交 `61f06a7` 跑 6／6。
  - review 後補了 4 項，`192df5b` 共 10／10。
- 尚未單獨隔離的部分：承諾雜湊與 nullifier 雜湊之間的區隔常數。「另一個 task」的竄改測試也會先被 nullifier 擋下。
- **證據只在本機**：`artifacts/s3/z1-192df5b/`（`go-test.txt`、`go-test-race.txt`、`measure.json`、`reverse/`、`licenses.txt`、`modules.txt`），以及第一版的 `z1-61f06a7/`。`artifacts/` 是 git 忽略的目錄，與其他切片相同。證據不含任何 witness 秘密值。

## 7 setup 的信任假設

- spike 用 `groth16.Setup` 在同一個程序裡產生 proving／verifying key。產生時的隨機值（toxic waste）在程序記憶體裡出現過，**知道它的人可以偽造任何 proof**。所以本片的 proof 只證明電路與流程可行，不代表任何人都該相信它。
- Groth16 需要兩階段：
  - phase 1 是通用的 powers of tau，可以沿用公開儀式的成果；但 gnark 的 `mpcsetup` 要接公開的 ptau 檔需要格式轉換，不能直接用。
  - phase 2 綁定這個電路，至少需要一位誠實參與者，信任才成立。全部參與者都是我們自己，就等於單方信任。
- 角色：coordinator 收集並公開每一步的 transcript；contributors 各自加入隨機值並銷毀；任何人都能驗證 transcript。
- 另一條路是改用 PLONK 搭配公開的通用 KZG SRS，就不需要電路專屬的 phase 2，但 proof 較大、鏈上驗證較貴。
- **建議交 Aaron 決定（Z1-D4）**：展示版若由 host 單方 setup，必須在畫面與文件明標「setup 由主持人單方產生，信任等同信任主持人」；不能默稱完成可信多方儀式。

## 8 Z2 的前置條件

1. **Z1-D1（Aaron 10/14 已決定）**：`intent_hash` 採選項 A，拆成兩個 128 位元 limb（Aaron：「Z1-D1 先按照建議的方式拆成兩份」）。寫進正式付款契約前仍另行 review。「proof 只保證綁在這個 `intent_hash`，不保證公開的 `amount` 等於 intent 裡的金額」列為已知限制（Aaron：「也是已知的情況，後續看是否需要做調整」）；由誰在電路外核對（合約或 Signer），或是否改採選項 E，後續再定。
2. **Z1-D2** task 的欄位映射（建議沿用 31 bytes 截斷，或由合約 `registerTask` 配發欄位內的 ID）。
3. **Z1-D3** 雜湊：MiMC（本 spike）或 Poseidon2（約束較少、較常見）。合約不需要重算雜湊，只比對承諾，所以不影響鏈上成本。
4. **Z1-D4** setup 方式與展示時的標示（第 7 節）。
5. gnark 升到 ≥ v0.16.4（需要 Go ≥ 1.26.8）。
6. **公開輸入必須是標準值**（發現 2）：
   - verifier 與合約拒絕 ≥ r 的值；
   - 合約自己從 `task_id` 算 `TaskField`、從 `bytes32` 拆 intent limb，不從 calldata 收；
   - 承諾與 nullifier 的比對使用傳給 verifier 的同一組值；
   - 確認 gnark 匯出的 Solidity verifier 會拒絕 ≥ r 的輸入。
7. 每個公開輸入都要有「竄改即驗不過」的測試（發現 1）；金額與差值都要做範圍檢查（發現 3）。
8. 合約的原子更新：驗證 proof → `commitment[task] == C_old` → nullifier 未用 → 寫入 `C_new` 並 emit `intent_hash`。本片只有 Go 模型。
9. 以下是第一次 review 的 S4，都會影響安全：
   - **(a) 初始承諾 C₀**：電路不證明第一個狀態，信任根是 issuer 的 `registerTask` 簽章。Z2 要規定 issuer 用相同的 H、區隔常數與 `TaskField` 產生 C₀，`r₀` 是均勻隨機、小於 r 的 254 位元值，餘額 < 2^64，並寫明 `r₀` 怎麼交到 prover 手上。
   - **(b) 誰可以花**：proof 只證明「知道 (balance, r)」。任何拿到 witness 的人（例如從 Store 外洩）都能用任意 `intent_hash` 花這筆預算，也能挑一個 `r_new` 把擁有者鎖在外面。要從以下擇一：
     - 限制送交者；
     - Signer 對 `(task, N, C_new, intent_hash)` 簽章；
     - 在電路內加入花費金鑰。

     proof 也沒有綁 `msg.sender`，送出的 proof 可被搶先送交；狀態變化相同，但若有任何東西依送交者發放就會有影響。
   - **(c) 部署範圍**：區隔標籤 `agent-payment-authority/zk/v1/` 不含 chain ID 或合約地址。同一把 verifying key、同一個 `commitment[task]` 的任何部署都會接受同一個 proof。要把鏈與合約綁進 `TaskField` 或區隔常數，或讓 `task_id` 全域唯一，並寫下來。
   - **(d) 遺失或重複使用 r**：重複使用 `r_new` 對預算是安全的，但下一次的 nullifier 會和已用的相同，這個 task 就永遠卡住（review 的探測已確認）。tx#1 確認前遺失 `r_new` 也一樣。prover 必須保存新舊兩份 witness，直到確認為止。
   - **(e) 隱私的極限**：金額是公開的，所以 `balance_new = B₀ − Σ amounts`，隱藏的只有 B₀ 與後續加值。r 必須是全熵隨機數：餘額只有 64 位元、task 又是公開的，r 太小時可以對承諾暴力猜出餘額。測試用的 r = 1001～1003 只是合成值。
10. 金額為 0 的扣款目前允許（會用掉一個 nullifier 並換一個承諾）；Z2 決定要不要允許。
11. 鏈上 gas 與 Solidity calldata 的實測。

## 9 不在本片

合約、Solidity verifier 匯出、鏈上部署、testnet、RPC、faucet、付款、與 Policy／Signer／Store 的接線、可信 setup 儀式、正式 witness、真實餘額或身分資料、電路 v2（Z2）。

## 10 第一次獨立上下文 review（10/14）與修正

Claude Code 獨立上下文 subagent review `87c9ebf..2ad16f5`：**not pass**。電路本身沒有發現能為錯誤陳述造出有效 proof 的方法；問題在 tx#1 的 Go 模型、測試的精準度與報告的缺漏。

| 項目 | 內容 | 處理（`192df5b` 程式，本文件） |
|---|---|---|
| M1 | 帳本模型用 proof 沒驗過的值做判斷與寫入（可以存進任意 `C_new`）；接受「值 + r」的別名 | 模型只收公開值、拒絕非標準值、用同一組值判斷；`TestAliasedPublicInputs`；第 1 節發現 2、第 8 節第 6 項 |
| S1 | witness 建立失敗也算成「prover 拒絕」 | 改為測試失敗 |
| S2 | 三個拒絕案例不只一個原因；`balance_old` 與 `intent_lo` 的範圍檢查沒有反向 | 新增只違反單一約束的案例，多原因的案例改名；反向補 4 項 |
| S3 | 帳本的「另一個 task」其實是「未登記的 task」 | 兩個 task 都登記後送 A 的 proof；未登記另列一案 |
| S4 | Z2 前置條件缺 C₀、誰可以花、部署範圍、r 的遺失或重用、隱私極限 | 第 8 節第 9 項 |
| notes | 公告查證的來源與日期、選項 A 的 gas、calldata 大小、ptau 格式轉換、nullifier 的用詞、金額為 0、證據只在本機、程式小處 | 第 2、3、4、5、6、7、8 節與程式註解 |

**第二次 review**（`2ad16f5..ff50bd8`）：**pass**。M1、S1～S4 都已解決，沒有新的 must-fix 或 should-fix。reviewer 另以 7 個突變探測帳本模型，6 個被抓到；沒被抓到的是「≥ r 改成 > r」的邊界。三個建議已在 `0daf737` 補上：
- 用金額為 0 的 proof 測 `amount = r`（剛好等於 r 的別名）；改回 `> r` 的突變現在會失敗；
- 「偽造 C_new」改在新帳本上只換 `C_new`，直接重現 M1 的情境，並確認 42 沒有被寫入；
- 修正 task B 那一案的註解（B 刻意登記成 A 目前的承諾）。

