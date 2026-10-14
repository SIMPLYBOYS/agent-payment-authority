# Z1 ZK 額度電路 v1 spike：電路、量測、`intent_hash` 映射與 Z2 前置條件

> 2026-10-14，Claude Code。依 vault〈Claude-Code-Z1-ZK額度電路v1Spike派工-20261013〉與 Aaron「我們先做 Z1」。分支 `commercial-z1`（從 main `87c9ebf`）。
> **狀態：spike 完成，待 Claude Code 獨立上下文 review。** 開工時查證：repo 與 vault 都沒有 Z1／S3 的既有交付（`spikes/` 只有 `bbs`、`webauthn`），因此從頭建立。
> 本片**只證明本機 proof primitive 可行**：不接 Policy、Signer、Store，不建合約、不部署、不碰 testnet、RPC、faucet 或付款。資料全部合成；setup 是本機單方 setup，不是可信儀式。**不能宣稱預算已被鏈上保護。**

## 1 結論（先講）

- gnark v0.16.3 的 Groth16（BN254）可以在本機證明「一次預算扣款」：私密餘額能開啟舊承諾、扣款後不為負、新承諾等於餘額、nullifier 屬於這個 task 與狀態、`intent_hash` 綁在 proof 上。正例通過，8 個無法成立的 witness 都產不出 proof，7 種竄改公開輸入都驗不過，一個 tx#1 的 Go 模型擋下重播、舊狀態、別的 task 與已用的 nullifier。
- 成本很低：3,096 個約束；prove 中位數約 45 ms、verify 約 0.9 ms；proof 164 bytes；proving key 約 604 KB；記憶體峰值約 23 MB（第 5 節）。
- **`intent_hash` 映射建議採「兩個 128 位元 limb」**（第 4 節選項 A）：一對一、不截斷、合約端拆 `bytes32` 很便宜。正式格式由 Aaron 決定（Z1-D1），本片沒有定稿。
- **兩個要帶進 Z2 的發現**：
  1. gnark 會照常編譯「公開輸入沒有出現在任何約束裡」的電路，而這個輸入填什麼值都會驗過。反向檢查把 `intent_hi` 的約束拿掉後，竄改 `intent_hi` 的 proof 就通過了。Z2 的每個公開輸入都必須有「竄改即驗不過」的測試。
  2. 只檢查「扣款後餘額在 64 位元內」擋不住**負的金額**（欄位上 r−k），金額本身也要做範圍檢查。反向檢查已證實這一點。

## 2 版本與依賴

| 項目 | 選擇 | 理由 |
|---|---|---|
| gnark | **v0.16.3**（2026-08-24） | 已修正 GHSA-3mvx-pp85-pm65（critical，`<v0.16.2` 的 std 元件 soundness）。最新的 v0.16.4（10/07）修正 GHSA-7fx8-hmgc-82jp（high，模擬曲線 fake-GLV 純量乘法，影響 ECDSA／P256Verify），本電路只用 MiMC 與範圍檢查，用不到；且 v0.16.4 要求 Go ≥ 1.26.8，本機是 1.26.4，需另外下載工具鏈。**Z2 進產品前升到 ≥ v0.16.4。** |
| gnark-crypto | v0.21.0 | gnark v0.16.3 指定；與主模組（idemix 間接依賴）相同版本 |
| 授權 | Apache-2.0（gnark、gnark-crypto、pprof、intcomp）；MIT（semver、cbor、go-colorable、go-isatty、zerolog、float16）；BSD-3（bitset、golang.org/x/*） | 證據 `licenses.txt` |
| 模組 | `spikes/zk/` 獨立 go module | 主 `go.mod` 不變，`go test ./...` 不會帶進來（與 C1 的 `spikes/bbs` 相同做法） |
| 建置成本 | 下載約 14 個模組；編譯電路 8.6 ms | — |

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
- **nullifier 的語意**：每個 `r_old` 只能花一次；prover 必須每次用新的隨機 `r_new`。若重複使用，下一次的 nullifier 會和舊的相同，被合約拒絕（安全的一側）。在「合約只存每個 task 一個承諾、並且原子更新」的設計下，`commitment[task] == C_old` 本身就擋重播，nullifier 是第二道防線。兩者都保留到 Z2 再決定是否精簡。

## 4 `intent_hash` 進公開輸入的映射（ADR payment-account-model 開放問題 4）

`intent_hash` 是 SHA-256 的 256 位元，BN254 純量欄位 r ≈ 2^253.6，放不進一個欄位元素。

| 選項 | 做法 | 一對一 | 成本 | 合約端 | 評估 |
|---|---|---|---|---|---|
| **A 兩個 128 位元 limb**（spike 採用） | `hi = h[0:16]`、`lo = h[16:32]`，各範圍檢查 128 位元 | 是 | 2 個公開輸入、約 256 個約束 | `uint256(h) >> 128`、`uint128(uint256(h))` | **建議**。不截斷、不改契約語意 |
| B 取模 r | `h mod r` | 否（每個欄位值約有 5～6 個 256 位元原像） | 1 個公開輸入 | 合約要做同樣的取模 | 不建議：語意有歧義；要找到碰撞仍需 SHA-256 等級的工作量，但「同一個欄位值對應多個 intent」不應出現在付款契約裡 |
| C 截斷 248 位元 | 取前 31 bytes | 否（碰撞約 2^124） | 1 個公開輸入 | 合約截斷 | 安全上可接受，但 A 一樣便宜又不截斷，沒有理由選 C |
| D 改用欄位內的雜湊 | 契約的 `intent_hash` 改成 Poseidon／MiMC | — | 低 | 合約與 Signer 要實作同一個雜湊 | 改變凍結的 v1／v2 契約語意，需要新契約版本；不在 Z1 範圍 |
| E 電路內重算 SHA-256 | 把 intent 欄位作為私密輸入，電路內算 SHA-256 | 是 | 每個 64 bytes 區塊數萬個約束 | 不需拆分 | 可以在 proof 內綁住「amount 就是 intent 裡的 amount」，但成本高一到兩個數量級；Z2 若要這個保證再評估 |

**綁定的範圍**：選項 A 只保證「proof 綁在這個 `intent_hash`」，**不保證** `amount` 公開輸入等於 intent 裡的金額。這要由合約或 Signer 在電路外比對（合約 emit `intent_hash` 與 `amount`，Signer 簽前核對兩者與自己算出的 intent 一致）。這列為 Z2／P2 的前置條件，不是 Z1 已解決的事。

## 5 量測（`z1-61f06a7`，50 次 proof）

| 項目 | 數值 |
|---|---|
| 機器 | darwin/amd64，8 CPU，go1.26.4（未啟用 GPU 加速） |
| 約束數／公開輸入／私密輸入 | 3,096／7／3 |
| 編譯電路 | 8.6 ms |
| setup（本機單方） | 338 ms |
| prove | 中位數 45.0 ms（41.9～56.6，平均 45.6） |
| verify | 中位數 0.93 ms（0.86～1.53） |
| proof／verifying key／proving key | 164 bytes／556 bytes／604,367 bytes |
| 每次 proof 配置的記憶體 | 約 1.7 MB；程序記憶體峰值約 22.8 MB |

數據來自 `cmd/measure`（`artifacts/s3/z1-61f06a7/measure.json`）。只有一台機器、一種電路大小，僅供數量級參考。

## 6 測試與反向（`spikes/zk/zk_test.go`、`reverse.py`）

- **正例**：一筆 0.50 的扣款 proof 驗證通過；把整個餘額花完（`balance_new = 0`）也通過。
- **prover 產不出 proof**（每項確認是約束不成立，不是 witness 建立失敗）：扣過頭、負的金額、謊報比承諾更高的餘額、錯的 `r_old`、`C_new` 不是剩下的餘額、別的 task 的 nullifier、金額為 2^64、intent limb 為 2^128。原生端的 `Assignment` 也拒絕扣過頭。
- **竄改公開輸入**（同一個有效 proof）：換金額、換 task、舊的 `C_old`、換 `C_new`、換 nullifier、換 intent 的 hi 或 lo limb，全部驗不過。
- **tx#1 的 Go 模型**（不是合約）：先驗 proof，再要求 `commitment[task] == C_old` 與 nullifier 未用，然後原子寫入 `C_new`。重播 → `STALE_COMMITMENT`；同一舊狀態的第二筆 proof → `STALE_COMMITMENT`；新狀態的下一筆 → 通過；A 的狀態當成 B 的 task → 拒絕；把承諾還原後重送舊 proof → `NULLIFIER_USED`。
- **race**：通過。
- **反向**（在一次性副本拿掉一個約束，要求對應測試以預期斷言失敗）：6／6。

| 拿掉的約束 | 抓到它的測試 |
|---|---|
| `amount` 的範圍檢查 | 負的金額 → proof 產生了 |
| `balance_new` 的範圍檢查 | 扣過頭 → proof 產生了 |
| 舊承諾開啟 | 謊報較高餘額 → proof 產生了 |
| 新承諾 | `C_new` 不是剩下的 → proof 產生了 |
| nullifier | 別的 task 的 nullifier → proof 產生了 |
| `intent_hi` 的範圍檢查（它唯一的約束） | 換 intent 的 hi limb → **驗證通過** |

開發時的一次試跑是 5／6：「舊承諾開啟」原本用「錯的 `r_old`」測，但錯的 `r_old` 同時讓 nullifier 對不上，拿掉承諾約束後仍被 nullifier 擋下，所以測試不夠精準。改成只違反承諾開啟的「謊報較高餘額」後，在提交上跑 6／6。這次試跑發生在提交之前，證據沒有保留，在此照實記錄。

證據：`artifacts/s3/z1-61f06a7/`（`go-test.txt`、`go-test-race.txt`、`measure.json`、`reverse/`、`licenses.txt`、`modules.txt`）。不含任何 witness 秘密值。

## 7 setup 的信任假設

- spike 用 `groth16.Setup` 在同一個程序裡產生 proving／verifying key。產生時的隨機值（toxic waste）在程序記憶體裡出現過；**知道它的人可以偽造任何 proof**。所以本片的 proof 只證明電路與流程可行，不代表任何人都該相信它。
- Groth16 需要兩階段：phase 1（通用的 powers of tau，可沿用公開儀式的成果，例如 BN254 的公開 ptau）與 phase 2（綁定這個電路）。phase 2 至少需要一位誠實參與者，信任才成立。gnark 有 `mpcsetup` 可用。
- 角色：coordinator 收集並公開每一步的 transcript；contributors 各自加入隨機值並銷毀；任何人都能驗證 transcript。
- 另一條路是改用 PLONK 搭配公開的通用 KZG SRS，就不需要電路專屬的 phase 2，但 proof 較大、鏈上驗證較貴。
- **建議交 Aaron 決定（Z1-D4）**：展示版若由 host 單方 setup，必須在畫面與文件明標「setup 由主持人單方產生，信任等同信任主持人」；不能默稱完成可信多方儀式。

## 8 Z2 的前置條件

1. **Z1-D1** `intent_hash` 映射（建議 A），寫進正式付款契約前另行 review；同時決定 amount 與 intent 一致性由誰在電路外核對（合約／Signer），或改採選項 E。
2. **Z1-D2** task 的欄位映射（建議沿用 31 bytes 截斷，或由合約 `registerTask` 配發欄位內的 ID）。
3. **Z1-D3** 雜湊：MiMC（本 spike）或 Poseidon2（約束較少、較常見）。合約不需要重算雜湊，只比對承諾，所以不影響鏈上成本。
4. **Z1-D4** setup 方式與展示時的標示（第 7 節）。
5. gnark 升到 ≥ v0.16.4（需要 Go ≥ 1.26.8）。
6. 每個公開輸入都要有「竄改即驗不過」的測試（第 1 節發現 1）；金額與差值都要做範圍檢查（發現 2）。
7. 合約的原子更新：驗證 proof → `commitment[task] == C_old` → nullifier 未用 → 寫入 `C_new` 並 emit `intent_hash`。本片只有 Go 模型。

## 9 不在本片

合約、Solidity verifier 匯出、鏈上部署、testnet、RPC、faucet、付款、與 Policy／Signer／Store 的接線、可信 setup 儀式、正式 witness、真實餘額或身分資料、電路 v2（Z2）。
