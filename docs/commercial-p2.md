# P2 Testnet 付款、結算、對帳與交付：設計提案

> 2026-10-17，Claude Code。依 vault〈Claude-Code-P2Testnet付款結算對帳交付派工-20261013〉、roadmap §5.3 P2（驗收 7、24、25、30、31、39、44）。分支 `commercial-p2`（從 main `e41cd5d`）。
> **Aaron 10/17：「照建議」：第 6 節 P2-D1–P2-D6 全部採用建議。** 實作 P2-a（本機整合）；只用本機 anvil 與其公開測試金鑰。沒有建立或讀取任何付款金鑰、沒有連任何鏈、RPC、faucet 或 facilitator，也沒有動 `/opt`。

## 1 要做到什麼

一筆經本人核准（Ask）的請購，在 Base Sepolia 上走完：

1. **預留（tx#1）**：Z2 預算合約 `spend(...)`，留下帶 `intent_hash` 的 `Spent` 事件；
2. **簽署**：付款 Signer 核對本人核准與預留後，簽 EIP-3009 `TransferWithAuthorization`（簽章，不是交易）；
3. **結算（tx#2）**：買方依 x402 帶 `PAYMENT-SIGNATURE` 重送；商家 verify 成功才送 `transferWithAuthorization`；
4. **對帳**：Reconciler 依鏈上 receipt／事件把付款收斂成 confirmed、pending、failed、unknown；unknown 不重付；
5. **交付**：足夠確認後才交付數位商品並發收據。

A、B 兩位客人的錢包、核准、預留、付款、收據完全隔離。**兩筆交易不是同一筆、不是原子的**；已簽或已送出的不能撤回，只能以對帳狀態呈現。另外要在 P2 一併完成：V1 語音部署到 `/opt`（`docs/commercial-v1.md` §15）、AE1 的付款對帳（未授權新增支出 = 0）。

## 2 現況（盤點）

| 部分 | 有 | 缺 |
|---|---|---|
| 付款 Signer（P1-b） | `serve`：金鑰庫（scrypt＋AES-GCM、終端機密語）、`signer.db` 的 `attempts`、EIP-3009 簽章（decred secp256k1、RFC6979、low-S）、nonce 唯一、單筆／總額上限、Unix socket＋peer uid | 預留核對寫死為 `Unavailable`（什麼都不簽）；`webauthn` 方法回 `HUMAN_APPROVAL_UNAVAILABLE`；請求沒有 `approval_id` |
| 本人核准（A2／V1） | `serve-approvals`、`approvals.db` 的 `human_approvals`（state 已有 `consumed`） | 沒有任何地方消耗核准；與 `signer.db` 沒有連結（**D8**） |
| Z2 | `contracts/src/Budget.sol`（`registerTask`、`spend`、`Spent` 事件、Groth16 verifier）、`zk/`（gnark prover）、本機 anvil 驗證 | 沒有部署腳本、沒有 Go 端預留服務、沒有 Base Sepolia 設定；Z2 §10 的前置（policy 授權的期限與撤銷、policy 金鑰保管、送出前模擬）未處理 |
| x402／商店 | `PAYMENT-REQUIRED`、`PAYMENT-SIGNATURE` 編解碼；fixture 商店一律回 402 | verify、settle、`PAYMENT-RESPONSE`、確認後交付；沒有 facilitator 客戶端 |
| Host／Store | `payment_attempts`（requested…unknown）、`signerclient.Pay`／`Resume` | demo 沒接付款路徑；`payment_status` 寫死 `not_enabled`；沒有 reserved／submitted／settled／delivered 狀態、收據、交付表；沒有付款事件 |
| 鏈上工具 | Foundry v1.8.3（`var/tools/`，本機 anvil 用） | Go 端沒有 JSON-RPC、ABI、交易簽署（主模組刻意不引入 go-ethereum） |

## 3 金鑰與帳戶（全部只在 testnet；每一把的建立都另行核准）

| 角色 | 用途 | 保管 | 備註 |
|---|---|---|---|
| A、B 付款錢包 | EIP-3009 授權的 `from`；持有測試 USDC | 付款 Signer（`/opt/apa-signer/data/keys`，apa-signer 帳號、密語） | P1-b 決策 5：P2 申請時建立 |
| 商家 M | 收款地址（`payTo`） | 只需地址；若自行結算，另需 gas 金鑰（見 D3） | |
| 預留送出者（gas） | 送 tx#1 的 EOA，付 gas | 依 D2 | 與付款錢包分開，只持有少量測試 ETH |
| Z2 policy 簽章者 | 簽 `policyDigest`，授權一次 `spend` | 依 D2 | Z2 §10：要有期限與撤銷 |
| Z2 issuer | `registerTask`、產生初始 commitment | 依 D2 | |

## 4 狀態機（沿用 `docs/commercial-transaction-flow.md` §3.3）

`not_started → reserved（tx#1 確認）→ signed → submitted（tx#2 送出）→ settled（確認）→ delivered`；任何一步不明 → `unknown`，只靠對帳收斂成 `settled`、`not_settled_expired` 或 `needs_review`，**從不重付**。新增事件：`BUDGET_RESERVED`、`PAYMENT_SUBMITTED`、`PAYMENT_SETTLED`、`RESOURCE_DELIVERED`、`RECEIPT_ISSUED`。`payment_status` 改由狀態推導，不再寫死。

## 5 分段

- **P2-a 本機整合**（不連外、不用正式付款材料）：本機 anvil＋本機部署的 Z2 合約＋本機測試用 USDC（EIP-3009）、測試 Signer、fixture 商店的 verify／settle、Reconciler、交付閘門；全部必驗反例（派工列的 10 類）在本機證明「失敗或 unknown 不產生第二筆支出、不交付」。
- **P2-b testnet 準備**：部署差異、地址、RPC、facilitator、測試資金與 gas、A／B 錢包隔離、金鑰保管、每日上限、執行次數上限、交易清單與停止流程；V1 `/opt` 部署的重建；交 review 與 Aaron 核准。
- **P2-c 有界外部執行**：只按核准的部署與交易次數執行；保存 tx hash、receipt、區塊、去敏證據；失敗或 unknown 照核准的方式停下，不自動重試、不另開錢包。停止點：一條成功 Ask、一條 Deny 的 testnet 證據；test402.com 一筆真 402。

## 6 P2-a 開工前要 Aaron 決定的事

| # | 問題 | 選項 | 建議 |
|---|---|---|---|
| P2-D1（即 D8） | 本人核准怎麼與付款「原子地」消耗 | **A** 把核准表搬進 `signer.db`，由同一個 `serve` 行程在一筆交易裡「消耗核准＋建立付款 attempt」；`/opt` 既有的 `approvals.db` 以一次性遷移工具搬入（credential、聲紋模板等）。<br>**B** 兩個檔案用 `ATTACH` 同一筆交易：兩者都是 WAL，SQLite 在 WAL 下跨檔案**不是原子的**；要改回 rollback journal 才行，會影響核准服務的並行。<br>**C** 兩階段（核准先標「保留給 attempt X」、再建 attempt、重啟時對帳）：程式與恢復路徑最多。 | **A**：唯一真正單一交易的做法，證明最簡單；代價是一次資料遷移，且付款與核准變成同一個行程 |
| P2-D2 | 誰送 tx#1、誰持有 Z2 的 policy 與 issuer 金鑰 | **A** Host 端一個獨立的「預算 relayer」程序，持有 gas、policy、issuer 三把測試金鑰（各自加密檔、0700 目錄、啟動時輸入密語）；<br>**B** 放進付款 Signer（`/opt`）；<br>**C** 由主持人手動用 `cast` 送 | **A**：與交易流程決策 8 的推薦一致（Policy gate 用專用 gas 金鑰）；付款金鑰仍只在 Signer；在 UI 標明「預算授權由主持人方信任」（Z2 §10） |
| P2-D3 | tx#2 誰送（verify／settle） | **A** fixture 商店自己 verify（重算 EIP-712、查餘額與 nonce、`eth_call` 模擬）再用自己的 gas 金鑰送 `transferWithAuthorization`；另外對 test402.com 做一筆真 402 相容性測試。<br>**B** 主路徑就用 x402.org 公開 facilitator 做 verify／settle | **A**：主路徑不依賴外部服務的可用性與行為，失敗可重現；生態相容性由 test402.com 那一筆證明（roadmap 要求） |
| P2-D4 | Go 端怎麼跟鏈互動 | **A** 不引入新依賴：自寫最小 JSON-RPC 客戶端、ABI 編碼（只需這幾個函式）、EIP-1559 交易簽章（沿用 decred secp256k1），以 anvil 對照測試；<br>**B** 引入 go-ethereum（推翻 P1-b 決策 2，依賴很大）；<br>**C** 由 Go 呼叫 `cast` 子程序（私鑰要交給 `cast`，不好保管） | **A**：金鑰不離開我們的程序，依賴不增加；需要的功能很窄（送交易、查 receipt／log、`eth_call`） |
| P2-D5 | Z2 policy 授權的期限與撤銷（Z2 §10） | **A** 合約加上 `deadline`（納入 `policyDigest`）與 `epoch`（policy 簽章者可提高 epoch，使舊授權全部失效），重跑 Z2 測試與反向檢查後再 review；<br>**B** 不改合約，列為已知限制 | **A**：沒有期限的授權在公開測試鏈上可被重放到額度用完；改動小，但要重新 review Z2 |
| P2-D6 | 確認數 | 交付前等幾個區塊確認 | **2 個區塊**（Base Sepolia 約 4 秒）；本機 anvil 用 1。數字寫進設定，之後可調 |

**P2-b 再決定**（現在先不用選）：RPC 提供者（公開 `sepolia.base.org` 或需要 API key 的服務）、Base Sepolia USDC 合約地址與其 EIP-712 name／version（實作時查證，不憑記憶）、faucet 來源、各錢包的測試資金與 gas 上限、每日與單批交易上限、test402.com 那一筆的細節、`cancelAuthorization` 要不要做、V1 `/opt` 部署的時機。

## 7 必驗反例（P2-a 在本機全部證明）

預留未完成或對錯 intent；grant 範圍、期限、nonce 不符；EIP-3009 重播；request ID 重送；verify 失敗仍 settle；settle 逾時被當失敗而再付款；未確認就交付；跨 consumer 冒用；A／B 並行共用 commitment；Reconciler 重複處理。每一項都要證明：**不產生第二筆支出、不交付**。

## 8 界線

- 不建立、不讀取、不解鎖任何付款或 gas 金鑰，不連 RPC、faucet、facilitator 或任何鏈，直到 Aaron 對該項個別核准（P2-a 只用本機 anvil 與其公開測試金鑰）。
- 絕不連 mainnet、不用真錢、不搬入日常錢包。
- 不宣稱 mainnet 或 production 安全；所有成功宣稱都要有 testnet receipt 與對應的 `intent_hash`，否則記 unknown／not_evaluated。
