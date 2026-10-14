# Z2 ZK 電路 v2 與本機預算合約

> 2026-10-14，Claude Code。依 vault〈Claude-Code-Z2-ZK電路v2與本機預算合約派工-20261013〉與 Aaron「我們要開始 Z2 的部分」。分支 `commercial-z2`（從 main `e36502e`）。
> **狀態：實作完成，待 Claude Code 獨立上下文 review。** 設計選擇與工具已核准（第 1 節）。 只用合成資料與本機鏈（anvil）；不碰 Base Sepolia、testnet、faucet、外部交易或付款金鑰。

## 1 開工前的核准（Aaron 10/14：「以上的內容，我批准先依照建議的方式進行，除非後續很難做調整，我們在深入討論取捨」）

**前置**：Z1 Done；Z1-D1（`intent_hash` 拆兩個 128 位元 limb）、D2（task 欄位 31 bytes 截斷）、D3（MiMC）、D4（展示版由主持人單方 setup，畫面明標「信任等同信任主持人」）已決定（[commercial-z1.md](commercial-z1.md) 第 8 節）。

**工具**

| 工具 | 版本 | 授權 | 安裝 |
|---|---|---|---|
| Foundry（forge、anvil、cast） | v1.8.3（2026-09-15 stable） | MIT／Apache-2.0 | 官方 macOS x86_64 壓縮檔，核對 sha256，解到 `var/tools/`（git 忽略） |
| solc | 0.8.37（2026-09-10） | GPL-3.0（編譯器） | forge 下載到 `~/.svm` |
| gnark | v0.16.4 | Apache-2.0 | 依 Z1 前置條件升級 |
| Go 工具鏈 | 1.26.8 | BSD | 由 Go 自動下載到模組快取 |

**位置**：電路、prover、setup 與 Solidity verifier 匯出放在獨立模組 `zk/`；合約放 `contracts/`（Foundry 專案）。主模組 `go.mod` 不動。

**設計選擇**

- **Z2-D1 政策層在鏈上的綁定**：Host 用本機測試金鑰（secp256k1，只在 anvil）簽 `(chain_id, 合約地址, task, intent_hash, amount, N, C_new)`，合約以 `ecrecover` 驗證。這也把金額和 intent 綁在一起（補 Z1-D1 的已知限制）。
- **Z2-D2 部署範圍**：task 欄位 = `SHA-256(domain ‖ chain_id ‖ 合約地址 ‖ task_id)` 的前 31 bytes，由合約自己算。
- **Z2-D3 setup**：gnark `mpcsetup` 在本機跑 phase 1、phase 2 各三次貢獻，全部由我們執行；紀錄熵來源、輸出雜湊與重現方式；標示「信任等同信任主持人」。
- **Z2-D4 金額為 0**：合約拒絕。

## 2 做了什麼

| 部分 | 內容 | 位置 |
|---|---|---|
| 電路 v2 | Z1 的電路，task 欄位改為綁定 chain ID 與合約地址（Z2-D2）。公開輸入 `task, C_old, amount, C_new, N, intent_hi, intent_lo`；私密 `balance_old, r_old, r_new`；3,096 個約束 | `zk/circuit.go` |
| setup 儀式 | gnark `mpcsetup`：phase 1、phase 2 各三次貢獻，每次貢獻都序列化、重新讀回並依序驗證，再以固定字串的 beacon 封存 | `zk/setup.go`、`zk/setup/transcript.json` |
| prover 與工具 | `zkbudget setup / random / commit / prove / fixtures`；`prove` 先在 Go 端驗證 proof 再輸出 calldata | `zk/cmd/zkbudget/` |
| Solidity verifier | gnark 匯出的 Groth16 verifier（收 256 bytes 的 proof），**會拒絕 ≥ r 的公開輸入**（`PublicInputNotInField`），補上 Z1 前置條件 6 | `contracts/src/Verifier.sol`（與 `zk/setup/Verifier.sol` 相同） |
| 預算合約 | `registerTask`、`spend`、`taskField`、`policyDigest`；一個 task 存一個承諾 | `contracts/src/Budget.sol` |
| 測試 | Go：電路的拒絕與竄改、部署範圍、金額 0、setup 儀式。forge：27 個測試（不依賴 forge-std） | `zk/zk_test.go`、`contracts/test/` |
| 本機鏈端到端 | anvil 上部署、登記、證明、policy 簽章、由轉送者送出、檢查狀態與事件，以及鏈上的負例 | `scripts/verify/z2-anvil.py` |
| 反向 | 電路 10 項、合約 9 項 | `zk/reverse.py`、`contracts/reverse.py` |

**依賴與版本**：gnark v0.16.4（要求的 gnark-crypto 是 pseudo-version `v0.21.1-0.20260913224755-1409fe592052`，不是正式標籤，記錄在案）、Go 1.26.8、Foundry v1.8.3（sha256 `1b469229…47b8` 與官方 `.sha256` 相同）、solc 0.8.37（因 stack too deep 開啟 `via_ir`）。授權：gnark、gnark-crypto Apache-2.0；Foundry MIT／Apache-2.0；solc GPL-3.0（只是編譯器）；其餘 Go 依賴與 Z1 相同（MIT、BSD-3、Apache-2.0）。主模組 `go.mod` 沒有動。

## 3 合約的檢查順序（`spend`）

1. 金額不為 0（Z2-D4）。
2. 由合約自己算 task 欄位：`SHA-256(label ‖ chain_id ‖ 本合約地址 ‖ task_id)` 的前 31 bytes。
3. `commitment[task]` 已登記且等於 `C_old`，否則 `StaleCommitment`。
4. nullifier 未使用。
5. `C_new` 不為 0（0 代表「未登記」）。
6. policy 簽章（EIP-191）簽的是 `(label, chain_id, 本合約, task, intent_hash, amount, N, C_new)`，簽署者必須是建構時設定的 policy 地址；只接受 low-s，v 為 27 或 28。
7. 由合約組出七個公開輸入（intent 的兩個 limb 由合約從 `bytes32` 拆出），交給 verifier；verifier 拒絕 ≥ r 的值與無效 proof。
8. 同一筆交易裡：標記 nullifier、寫入 `C_new`、emit `Spent(task, intent_hash, amount, N, C_new)`。

- `registerTask` 只有 issuer 能呼叫，同一個 task 只能登記一次，承諾必須在 (0, r)。
- 送交者不限：授權來自 policy 簽章，不是 `msg.sender`。轉送者可以代送；有人搶先送出同一筆，結果也相同。

## 4 setup 儀式的紀錄（Z2-D3、Z1-D4）

`zk/setup/transcript.json`：

- 參與者：本機同一個程序，phase 1、phase 2 各三次貢獻，全部由我們執行。
- 熵：每次貢獻取自作業系統的 `crypto/rand`；兩個 beacon 是固定字串，不是公開的隨機信標。
- 每一步的輸出 SHA-256：phase 1 三筆、phase 2 三筆、proving key、verifying key。
- 信任：**信任等同信任主持人**——執行這個程序的人可以留下 toxic waste 並偽造 proof。這不是獨立的可信儀式。
- 重現：任何人拿到每一步的序列化結果都能依序重新驗證（`mpcsetup.VerifyPhase1／VerifyPhase2`）。本次只公開雜湊，沒有保存中間檔；要讓第三方完整重驗，需另外保存各步的序列化檔。

## 5 測試

**Go（`zk/`）**：正例；11 個 prover 產不出 proof 的 witness；7 種竄改公開輸入；task 欄位換鏈、換合約、換 task 都不同；金額 0 被拒；兩次貢獻的儀式產生的金鑰能證明與驗證，另一組 setup 的 proof 驗不過。

**forge（`contracts/test/Budget.t.sol`，27 個）**：每個拒絕都用 `expectRevert(錯誤選擇器)` 指定原因，並在需要時確認狀態未改。

| 類別 | 測試 |
|---|---|
| 閉環 | 登記後連續兩次扣款（含 `Spent` 事件）；轉送者代送 |
| 登記 | 只有 issuer；只能一次；承諾 ≥ r 被拒 |
| 必須拒絕 | 金額 0；未登記的 task；重播；同一舊狀態的第二筆；把承諾還原後重用 nullifier |
| 竄改（policy 也簽了，只剩 proof 能擋） | 金額、`C_new`、nullifier、intent hi limb、intent lo limb、proof 位元 |
| 錯的 task 與 consumer | A 的 proof 用在 B（B 被刻意設成 A 目前的承諾，只剩 proof 內的 task 綁定能擋）；B 的 policy 授權用在 A；同一個 proof 換到另一個部署 |
| policy 層（Z2-D1） | 沒有簽章；別的金鑰；簽的是別的金額；簽的是別的 intent；malleated 簽章（high-s） |
| 標準值 | nullifier + r、`C_new` + r 被 verifier 以 `PublicInputNotInField` 拒絕 |
| 失敗後重送 | 竄改的交易失敗後狀態不變；正確的重送成功；再送一次被拒 |

## 6 本機鏈端到端（`z2-anvil.py`，anvil 1.8.3，chain 31337）

部署 verifier 與預算合約；合約算出的 task 欄位等於 prover 的；issuer 登記（47,988 gas），別人登記失敗；竄改金額的交易**實際上鏈並失敗**（status 0），狀態不變；別的金鑰簽的 policy 被拒；由轉送者送出正確的一筆（296,182 gas），承諾更新、nullifier 已用、恰一個 `Spent` 事件且含 `intent_hash`；同一筆再送被拒；新狀態的下一筆成功。

## 7 成本

| 項目 | 數值 |
|---|---|
| 約束數 | 3,096（與 Z1 相同；電路只有 task 欄位的值不同） |
| prove／verify（Go） | 與 Z1 相同電路，約 45 ms／0.9 ms（Z1 的量測；本片沒有重量） |
| setup 儀式（三加三次貢獻） | 約 19 秒 |
| `registerTask` | 47,988 gas |
| `spend` | 約 270,000～296,000 gas（其中 `verifyProof` 中位數約 227,000） |
| 被竄改的 proof | 會讓預編譯合約耗盡轉給它的 gas 才失敗（forge 量到約 10 億 gas）；成本由送交者負擔，但前端估算 gas 時就會失敗 |

## 8 反向（在一次性副本拿掉一個檢查，要求指定的測試失敗）

- 電路：10／10（與 Z1 相同的 10 項約束）。
- 合約：9／9。

| 拿掉的檢查 | 抓到它的測試 |
|---|---|
| 金額不為 0 | `test_ZeroAmount`（改由 verifier 拒絕，錯誤不符） |
| `C_old` 必須是目前的承諾 | `test_SecondSpendOfAnOldState`（改由 nullifier 擋下，錯誤不符；也就是 nullifier 是第二道防線） |
| nullifier 只能用一次 | `test_UsedNullifierWithTheCommitmentReinstated` |
| policy 簽章 | `test_PolicySignatureByAnotherKey` |
| 拒絕 high-s 簽章 | `test_MalleatedPolicySignature` |
| 寫入 `C_new` | `test_RegisterAndSpendTwice` |
| 只有 issuer 能登記 | `test_RegisterOnlyIssuer` |
| 只能登記一次 | `test_RegisterOnce` |
| 驗證 proof | `test_TamperedAmount` |

- 合約反向的試跑歷程：第一次 0／9，原因是腳本先以寫入模式開啟 `foundry.toml` 再讀，複本沒有設定、編譯失敗；第二、三次 0／9，原因是 forge 的 `--match-test` 比對含括號的簽名，`^name$` 什麼都比對不到。修正 harness 後 9／9。這些試跑都在提交前，證據沒有保留。
- 端到端的歷程：`0a61dc3` 那次因輸出目錄是相對路徑，zkbudget 建到 `zk/` 底下而失敗（紀錄保留在 `anvil-failed-relative-path/`）；`77f8673` 修正後 PASS。

## 9 與設計不同或補充的地方

- **不依賴 forge-std**：測試自行宣告用到的 cheatcode 介面，避免把 git submodule 帶進 repo。
- **fixtures 由 Go 產生**：`zkbudget fixtures` 依 forge 測試合約的部署者與 nonce 算出合約地址，產生 `contracts/test/Fixtures.sol`。測試在 `setUp` 確認地址與 chain 相符，不相符就失敗。fixtures 與 `Verifier.sol` 必須來自同一次 setup。
- **proving key 已提交**（`zk/setup/pk.bin`，約 590 KB），讓乾淨的 clone 能跑端到端與重新產生 fixtures。這把 key 屬於主持人單方的 setup，不是秘密。
- **送交者不限**：授權在 policy 簽章（第 3 節）。
- **`via_ir`**：`spend` 的參數與區域變數超過 EVM 堆疊限制，依編譯器建議開啟。

## 10 不在本片；P2 的前置條件

- 不部署 testnet，不碰 Base Sepolia、faucet、外部交易、付款金鑰。本機 proof 與本機鏈通過，不代表鏈上付款或商店交付完成。
- policy 簽章目前用 anvil 的測試金鑰；正式的 policy 金鑰由誰持有、怎麼保管、怎麼和 Policy／核准（grant）接線，是 P2 的事。
- issuer 怎麼產生 C₀、`r₀` 怎麼交給 prover、prover 怎麼保存新舊 witness 直到交易確認（Z1 第 8 節 9 (a)、(d)）。
- 真正的可信 setup（若要超過「信任等同信任主持人」）。
- 前端顯示「信任等同信任主持人」的畫面標示（Z1-D4）要在 UI 接上 ZK 流程時一起做。

## 11 證據（只在本機，`artifacts/` 為 git 忽略）

`artifacts/z2/z2-0a61dc3/`：`go-test.txt`、`go-test-race.txt`、`forge-test.txt`、`forge-gas.txt`、`reverse-circuit/`（10／10）、`reverse-contract/`（9／9）、`anvil-failed-relative-path/`。`artifacts/z2/z2-77f8673/anvil/`：端到端 PASS 與 `gas.json`。

