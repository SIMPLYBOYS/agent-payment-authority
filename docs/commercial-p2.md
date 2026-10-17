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

## 9 P2-a 進度

### 9.1 Z2 合約：policy 授權的期限與 epoch（P2-D5）

- `spend` 多一個 `deadline`：超過即 `PolicyExpired`；`policyDigest` 納入 `deadline` 與合約的 `epoch`。
- `raiseEpoch(newEpoch)`：只有 policy 簽章者、只能往上；之後所有在較低 epoch 下的簽章都失效（`EpochRaised` 事件）。電路與 verifier 不變；storage 只在既有欄位後加 `epoch`。
- 驗證：forge 35 個測試通過（新增 4：過期、期限在簽章內、提高 epoch 作廢舊簽章、只有 policy 簽章者且只能往上）；`scripts/verify/z2-anvil.py`（本機 anvil，簽章帶期限）PASS；`contracts/reverse.py` 14／14 caught（新增 5 項）。

### 9.2 Go 端的最小以太坊客戶端（P2-D4）

`internal/eth`：Keccak256、位址、金鑰（decred secp256k1，RFC 6979、low S；`SignPersonal` 為 EIP-191，給預算合約的 policy 簽章用）、RLP、EIP-1559 交易（空 access list，在程序內簽章）、ABI（uint256、address、bytes32、uint8、bytes、string）、JSON-RPC 客戶端（有逾時與回應大小上限）。沒有新增依賴。
- `TestVectors`：兩筆交易與一個含動態型別的呼叫，與 foundry 1.8.3 的 `cast mktx`／`cast calldata` 逐位元組相同；`TestAnvil`：在本機 anvil 上，自己簽的轉帳被打包（status 1、21000 gas）、金額移動、同一筆原始交易第二次被拒。反向突變 4／4 KILLED。

### 9.3 本機測試用 USDC（P2-a）

`contracts/src/TestUSDC.sol`：ERC-20 餘額＋EIP-3009 `transferWithAuthorization`，EIP-712 domain 為（`USDC`、`2`），與付款 Signer 簽的一致；只有部署者能鑄幣；**只在本機 anvil 部署**（Base Sepolia 有它自己的 USDC）。forge 10 個測試：付款、重播、過期、尚未生效、別人簽、改收款人、改金額、被改造的簽章、餘額不足（不用掉 nonce）、只有擁有者鑄幣。合約測試合計 45 個通過。

### 9.4 本人核准併入 `signer.db`，付款時一筆交易消耗（P2-D1）

- **一個資料庫**：`signer.OpenDB`（WAL、full sync、foreign keys、immediate）。付款的 `attempts` 與核准表（`approver_handles`、`enrollments`、`credentials`、`approval_challenges`、`human_approvals`、`verify_requests`、語音兩表）都在 `DIR/signer.db`；核准表的 schema 沒有改。
- **付款用本人核准**：`/v1/sign` 的 `authorization` 可為 `{method: "webauthn", approval_id}`。Signer 會：
  - 從完整請購重算 intent 與 jti；
  - 要求核准是 `approve`、`verified`、未到期，credential 仍 `active`，approver 版本相同、屬於同一 consumer、錢包在其 `wallet_ids`；
  - 單筆上限用 approver 的 `signer_max`，期限取 `min(核准到期, valid_before)`；累計上限見 §9.5；
  - 一筆付款只能有一種授權：webauthn 帶了 delegation 欄位，或 delegation 帶了 `approval_id`，都回 `BAD_REQUEST`；
  - 在**接受付款的同一筆交易**裡重讀核准、改成 `consumed`，並建立 attempt（`attempts.approval_id` UNIQUE；`delegation_id` 與 `approval_id` 恰有一個）。
  - 拒絕碼：`APPROVAL_UNKNOWN`、`APPROVAL_DENIED`、`APPROVAL_CONSUMED`、`APPROVAL_NOT_VERIFIED`、`APPROVAL_EXPIRED`、`INTENT_MISMATCH`、`CREDENTIAL_REVOKED`、`APPROVER_CHANGED`、`CONSUMER_MISMATCH`、`OVER_PER_TX`。原本的 `HUMAN_APPROVAL_UNAVAILABLE` 在 Signer 端不再出現。
  - 委派付款的規則不變。
- **同一個行程**：`serve` 在設定有 approvers 與 webauthn 時，同一個 socket 也提供核准與登記路由（`signer.ServeAll`）；health 仍是付款的 `unlocked`。`serve-approvals` 保留（不讀金鑰、付款路由回 `PAYMENT_MODE_OFF`），資料改為 `signer.db`。
- **遷移**（一次性）：`apa-signer approver migrate --dir DIR`。
  - Signer 必須先停：命令先確認 socket 不存在。
  - 對舊檔做 `wal_checkpoint(TRUNCATE)`，把殘留的 WAL 併回主檔；有其他連線**開著交易**時拒絕。只開著、沒有交易的連線（例如舊版的管理命令）偵測不到，所以要靠「先停 Signer、不同時跑管理命令」。
  - 舊檔以可寫方式 ATTACH，複製期間的 immediate 交易也持有舊檔的寫鎖。在 `signer.db` 的一筆交易裡依欄位名逐表複製，並核對每表筆數。
  - 完成後把舊檔改名為 `approvals.db.migrated`，不刪除。
  - 若上次已提交、但在改名前中斷：`signer.db` 的核准資料與舊檔逐列相同時，再跑一次只會補完改名；`signer.db` 有其他核准資料時拒絕，什麼都不搬。
  - P1-b 的 `attempts` 表（`delegation_id NOT NULL`、沒有 `approval_id`）在開啟時以一筆交易重建，原有資料保留。
  - 目錄裡還有 `approvals.db` 時，除了 `migrate` 以外的命令（`serve`、`serve-approvals`、`approver …`）一律拒絕，並提示這個命令。`scripts/demo/v1-dev.sh` 也會先停下來，提示同一個命令。
- **還沒有做的**：
  - Host 端仍拒絕 webauthn 付款（`internal/store` 的 `HUMAN_APPROVAL_UNAVAILABLE`），也仍以 `approvals-only` 判斷核准是否可用。這兩點留到 P2-a 的 demo 接線。
  - 正式組裝的預留核對仍是 `Unavailable`，所以仍然什麼都不簽。
- **既有資料**：
  - `var/v1-dev/data`（還是 `approvals.db`）沒有遷移，也沒有讀取或複製；要不要、何時遷移，由 Aaron 決定。遷移後舊檔保留為 `.migrated`，不違反「demo 結束前不清理」。
  - `/opt/apa-signer/data/approvals.db` 要等 P2-b 與 V1 `/opt` 部署時，經另行核准，再由 Aaron 以 `apa-signer` 帳號執行同一個命令。
- **驗證**：
  - `internal/signer` 新增測試：
    - `TestHumanApprovalPays`：6 種拒絕都不寫 attempt、核准維持 verified；成功時核准與 attempt 一起寫入；重送得到同一筆；第二筆付款得到 `APPROVAL_CONSUMED`；A2 查詢顯示 consumed。
    - `TestHumanApprovalLapses`：到期、credential 撤銷、核准被放棄（`APPROVAL_NOT_VERIFIED`）、錢包不再屬於 approver（`CONSUMER_MISMATCH`）、approver 版本改變（`APPROVER_CHANGED`）。
    - `TestHumanApprovalChangesBeforeAccept`：在 verify 之後、accept 之前（測試掛勾 `after-verify`）放棄核准或撤銷 credential，都被接受交易裡的重讀擋下，不寫 attempt。
    - `TestHumanApprovalRace`：8 個請求搶同一個核准，恰好一筆簽成。（同一錢包的請求由錢包鎖序列化，所以這個測試證明的是結果，交易內的重讀由上一個測試證明。）
    - `TestMigrateApprovals`：開著交易時拒絕、P1-b attempts 保留、不能遷兩次、改名前中斷後可補完、遷移後可付款、`signer.db` 有其他核准資料時拒絕。
    - `TestServeAll`：真 Unix socket 上，核准在同一個 socket 驗證並付款。
  - `cmd/apa-signer`：`TestApproverMigrate`；既有測試改為 `signer.db`。
  - `go test -race ./internal/signer/... ./cmd/apa-signer/` 通過。
  - 反向突變新增 15 項（第一輪 review 後補 5 項、第二輪補 1 項），在工作樹的複本上執行，15／15 KILLED。既有突變中涉及 signer 的文字都仍只出現一次（mutation-check 與 m-b-reverse 的 47 項）。
- **順手修的既有失敗**：`cmd/demo` 的 `TestTimingInterleaved` 在本分支與 `main`（e41cd5d）都固定失敗。原因是測試的競態：已結束的 run 不再顯示 `tool_calls`，B 在第二次呼叫後立刻結束，測試等不到。改成 B 也等 `x3` 才結束（ea4e397）。
- **Review**：Claude Code 獨立 context 的 code-reviewer，結論是 APPROVE，附 3 項 Important 與 5 項建議：
  - 累計上限：Aaron 決定照建議另設欄位，見 §9.5。
  - 改名前中斷會卡住：已修。
  - 測試沒有證明 3 個拒絕碼與交易內重讀：已補。
  - 建議全部處理：`verify` 改用請求的 ctx；拒絕混用兩種授權；遷移時的寫鎖與文件用語；人類核准設計文件標註已被 P2-D1 取代；表清單補上 `approver_handles`。
  - 第二輪（7f1a563）：APPROVE，無 Critical／Important。3 項 should-fix 已處理：測試補上「筆數相同、內容不同」由 EXCEPT 擋下的情況（並加對應突變）；補完改名的路徑也檢查 Rollback 與 DETACH 的錯誤；每張表的欄位清單只查一次。

### 9.5 approver 的累計上限（`signer_total`）

- **決定**：Aaron「你指的兩件事，都先依照建議的方案進行」（10/17）。採方案 2：與每筆上限分開，另設總額與期間。
- **設定**：每個 approver 必填：
  - `signer_total`：至少等於 `signer_max`；
  - `signer_window_seconds`：大於 0，例如每日 86400；要「全期」就填一個很大的值。
  - 缺任一欄位，`ParseConfig` 就拒絕。這是硬上限，不能因為設定漏寫而靜默關閉。
- **檢查**：在接受付款的交易裡，加總這位 approver 的核准在期間內（`accepted_at > now - window`）已接受或已簽的 attempt，加上這一筆超過 `signer_total` 就回 `OVER_TOTAL_CAP`，什麼都不寫，核准維持 verified。
  - 放棄或過期未簽的 attempt 不計入，與委派的總額規則相同。
  - 各 approver 分開計算。
  - 委派的總額（`total_cap`）與 approver 的總額彼此獨立：一筆付款只用一種授權，所以同一位客人在一個期間內最多可付「委派總額＋approver 總額」。
- **設定值**：
  - 測試設定：`signer_max` 1,000,000、`signer_total` 2,000,000、每日。
  - `scripts/demo/v1-dev.sh`：`signer_total` 5,000,000、每日。
  - `a2-ui.mjs`、`v1-ui.mjs`：與測試設定相同。
  - `/opt` 的 `signer.json` 要在部署時補上（`docs/commercial-v1.md` §15）。
- **驗證**：
  - `TestHumanApprovalTotal`：4 筆共 2,000,000 通過，第 5 筆（1 單位）被拒且不消耗核准；B 的 approver 分開計算；第一筆離開期間後又可以付。
  - `TestApproverTotalConfig`：缺總額、總額低於每筆上限、缺期間，都被拒。
  - 反向突變 4／4 KILLED：不加總、忽略期間、不分 approver、接受總額低於每筆上限。

### 9.6 預算 relayer 與 Signer 的鏈上預留核對（P2-D2，本機）

- **zkbudget**：`commit`、`prove` 加 `-witness-stdin`，從 stdin 讀 `{"balance", "r", "rnew"}`。同時給旗標就拒絕。Z2 §10 的「witness 在命令列上」由此解決；relayer 只用這個方式。
- **`internal/budget` 的 `Relayer`**：持 gas、issuer、policy 三把金鑰（P2-a 用 anvil 的公開測試金鑰）。每個 task 的私密預算狀態（餘額、`r`、commitment）存在自己的 SQLite（建立時 0600）。
  - `RegisterTask(task, consumer, balance)`：產生新的 `r`，由 issuer 送 `registerTask`。重呼叫會補完尚未確認的登記；換 consumer 或餘額則拒絕。
  - `Reserve(order)`：
    - **intent 與金額都由 relayer 從完整請購自己重算**（Z2 §10 S3(b)）。
    - task 必須屬於請購的 consumer（`CONSUMER_MISMATCH`）；餘額不足回 `OVER_BUDGET`；同一 task 已有送出未確認的預留時回 `TASK_BUSY`（A／B 不共用、同一 commitment 不併發）。
    - 用 stdin witness 產生 proof，並核對 proof 的 task、舊 commitment、金額是自己的。
    - policy digest **在本地計算**（測試證明與合約的 `policyDigest`、`taskField` 相同）；epoch 從鏈上讀，期限是現在加 `PolicyTTL`。
    - **送出前先模擬**（Z2 §10 S3(c)）：模擬失敗回 `RESERVATION_SIMULATION_FAILED`，什麼都不寫、不送。
    - 已簽的原始交易**先寫入 DB 再送出**。中斷或重啟後，同一張請購只會重送這一筆（同 hash、同 nonce），絕不產生第二筆。等不到確認回 `RESERVATION_PENDING`（unknown）。
    - 確認數達到設定值後，在一筆寫入裡：預留改成 reserved，task 的餘額、`r`、commitment 換成新的。交易 revert 則預留改成 failed，task 不變。
    - 回傳證據 `Evidence`（chain、合約、task、intent、金額、tx hash、區塊）。同一張請購再呼叫，回同一份證據。
    - 已過期的請購（`valid_before` 已過）回 `ORDER_EXPIRED`，不動用預算。policy 期限取「現在＋`PolicyTTL`」與 `valid_before` 中較早者。
    - **交易永遠不會成功時**，預留改成 failed，task 的狀態不變，之後的請購可以再預留（同一個 nullifier 只在非 failed 的預留之間唯一）。「永遠不會成功」指：
      - revert；
      - nonce 已被同一把金鑰的其他交易用掉（另一筆已上鏈、這筆沒有 receipt）；
      - 鏈上時間已超過 policy 期限而這筆仍未上鏈。
      - 一張請購的預留一旦 failed，該請購就不再預留，要用新的請購。
    - **nonce**：寫入的交易存下 nonce 與期限。每次要送新交易前，先重送所有已寫入、未確認的交易；新交易的 nonce 取「節點的 pending nonce」與「已寫入未確認的最高 nonce＋1」中較大者。所以重啟後，新的預留不會搶走寫入在先的那一筆的 nonce。
    - 每次呼叫最多等 2 分鐘（`wait`），等不到回 `RESERVATION_PENDING`，不會一直佔著鎖。
    - 確認後的狀態更新：先把預留從 sent 改成 reserved（必須恰好一列），再只從這筆預留花掉的那個 commitment 更新 task（必須恰好一列），防止兩個程序重複扣款。
  - ponytail：所有 task 與各金鑰的 nonce 共用一把鎖；兩位客人的預留要重疊時，再改成每個 task 一把鎖並加 nonce 管理。
  - **已知、刻意的取捨**：
    - epoch 是從節點讀的。說謊的節點可以讓 relayer 簽一個未來 epoch 的授權，但仍受期限限制，而且合約只接受目前的 epoch。
    - tx#1 上鏈後，若 Signer 拒簽（例如期限已過），這筆預算就花掉了，不會退回。預算是上限，已預留未付款的部分交給對帳處理（P2 後續）。
- **Signer 的鏈上核對 `budget.Verifier`**（實作 `signer.ReservationVerifier`）。介面改為帶入 context 與完整請購。
  - 證據只採信 `tx_hash`，其餘從自己的節點重讀：交易成功、確認數足夠，且**預算合約本身**發出的 `Spent` 的 task（由請購的 task 與合約算出）、intent、金額都與請購相符，而且不是被 reorg 移除的 log。
  - 請購的鏈必須是 verifier 設定的鏈；verifier 自己的節點也必須在這條鏈上（同一個合約地址可能存在於別條鏈）。
  - intent hash 本身已涵蓋 task 與金額，所以 task 與金額的比對是第二道、獨立的檢查，由 `TestSpentFor` 逐欄單獨證明。
  - 拒絕碼：`RESERVATION_MISSING`、`RESERVATION_PENDING`、`RESERVATION_FAILED`、`RESERVATION_UNCONFIRMED`、`RESERVATION_MISMATCH`；節點連不上是 `RESERVATION_UNAVAILABLE`。
  - 尚未接進 `cmd/apa-signer`（正式組裝仍是 `Unavailable`），留到 demo 接線。
- **驗證**（本機 anvil，chain 84532，部署 Verifier 與 Budget；`go test -race` 通過）：
  - `TestReserveAndVerify`：
    - 預留後 verifier 接受；同一張請購再呼叫，不送第二筆。
    - verifier 拒絕：別的請購、別的 task、別的金額、從未送出的 hash、沒有證據、確認數不足、revert 的交易、**別的合約發出的一模一樣的仿冒 `Spent`**（自己部署一個原樣發出事件的合約）。
    - relayer 拒絕且什麼都不寫、不送：B 的請購動用 A 的 task、超過剩餘預算、未登記的 task、不是請購、**狀態落後鏈上**（被模擬擋下）。
    - 剩餘預算正確地跟著鏈上狀態走。
  - `TestReserveResumesAfterACrash`：寫入之後、送出之前中斷 → 其他請購回 `TASK_BUSY` → 重啟後送出的就是寫入的那一筆（gas nonce 只加 1、鏈上只有一個 `Spent`），verifier 接受。
  - `TestSignerSignsOnlyAReservedOrder`：真的 `signer.Core` 搭配這個 verifier：沒有預留回 `RESERVATION_MISSING`、拿別張請購的預留回 `RESERVATION_MISMATCH`、自己的預留才簽。
  - `TestPolicyDigestIsTheContracts`。
  - review 後新增：
    - `TestReserveAfterARevert`：寫入後過了 policy 期限才送出 → revert → failed → 同一個 task 的下一張請購可以預留，並通過 verifier。
    - `TestResendBeforeANewNonce`：中斷、重啟後，先來的另一個 task 的預留不會搶走 nonce，兩筆都預留成功，gas nonce 恰好加 2。
    - `TestDroppedReservationFails`：nonce 被同一把金鑰的另一筆交易用掉 → failed，不會一直 pending → 下一張請購可以預留。
    - `TestSpentFor`：事件比對逐欄（別的合約、別的事件、別的 task、intent、金額、被移除的 log）。
    - 拒絕表新增「已過期的請購」（`ORDER_EXPIRED`）與「別條鏈的 verifier」。
  - 「鏈上時間超過期限而仍未上鏈」這條判定在 anvil 上測不到：anvil 收到交易會立刻打包並 revert，走的是 revert 那條路。
- **突變**：
  - 需要 `var/` 本機工具（anvil、合約 build）的項目，在 mutation-check 的容器裡會被 skip、誤判為 SURVIVED，所以放在 `LOCAL_MUTATIONS`，由新的 `scripts/verify/local-mutations.py` 在工作樹的複本上執行：
    - 以 symlink 連到 `var/`；
    - **每個目標測試先在未突變的複本上跑一次，必須通過（不能 skip），否則整批停下**；
    - 突變後測試被 skip 算錯誤，不算 KILLED。
  - review 後，P2 全部 44 項（eth 4、D1 15、累計上限 5、預算 relayer 20）以 `local-mutations.py P2 --all` 重跑：14 個基準都通過，44／44 KILLED。
  - 過程中發現並修正：交易漏了 calldata（測試抓到）；「仿冒事件」與「續送」原本沒有被測到，已補。
- **既有、與本片無關**：
  - mutation-check 的 `B1-04 A R1 host: requested model missing from the trace`（`internal/buyer/host.go`）；
  - m-b-reverse 的 A2「viewer token 不能取代 assertion」（`cmd/demo/approvals.go`）；
  - 這兩項的突變文字在 `main` 上就已經找不到，之後另外修。

### 9.7 第三段 review（累計上限、zkbudget、relayer）與修正

Claude Code 獨立 context 的 code-reviewer，結論 REQUEST CHANGES。全部處理：

| 等級 | 問題 | 處理 |
|---|---|---|
| Critical | 預留 revert 後 nullifier 仍佔著 UNIQUE，該 task 之後每次都失敗 | 改成只在非 failed 的預留之間唯一；`TestReserveAfterARevert` |
| Important | 單一 gas 金鑰：重啟後新交易可能搶走寫入在先那筆的 nonce，使它永遠 pending、task 永遠 `TASK_BUSY`；await 沒有上限而且佔著鎖 | 存 nonce 與期限；送新交易前先重送；nonce 下限；判定「永遠不會成功」改為 failed；await 最多 2 分鐘；`TestResendBeforeANewNonce`、`TestDroppedReservationFails` |
| Important | `local-mutations.py` 沒有未突變的基準，環境壞掉也會被算成 KILLED | 加上基準，必須通過才判定 |
| 建議 | 確認後的狀態更新沒有檢查影響列數 | 先改預留（恰好一列），再只從花掉的 commitment 更新 task（恰好一列） |
| 建議 | task、金額的比對沒有被單獨測到 | 抽成 `spentFor`，以 `TestSpentFor` 逐欄測 |
| 建議 | verifier 不檢查鏈 | 請購的鏈與節點的鏈都檢查 |
| 建議 | epoch 從節點讀；Signer 在 tx#1 之後拒簽時預算不退回；failed 的請購不再預留 | 寫進 §9.6 |
| 建議 | `Reserve` 不檢查請購是否已過期 | `ORDER_EXPIRED`；policy 期限不晚於 `valid_before` |
| 建議 | 期間邊界的測試沒有釘住 | 改用剛好 86400 秒，並加上 `>=` 的突變 |
| 建議 | 委派與 approver 的總額彼此獨立 | 寫進 §9.5 |
| 建議 | zkbudget 漏掉 `-balance 0`；stdin 的 JSON 後面接著多餘資料也接受 | 改用 `fs.Visit` 判斷有沒有給旗標；要求 JSON 之後就是 EOF；補兩個測試 |
