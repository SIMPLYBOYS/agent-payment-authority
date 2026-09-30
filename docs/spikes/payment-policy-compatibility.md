# 付款政策與帳戶模型相容性盤點（S4-A）

狀態：S4-A 研究草稿第二版，待 Aaron 或指定 reviewer 接受。**不代表採用任何候選。** 本輪沒有 PoC、部署、注資或交易。

- 查核日期：2026-09-30。
- 契約審查對象：`57791d1`（分支 `b1-02-contracts`）。
- 決策草案見 [ADR：付款帳戶模型](../adr/payment-account-model.md)，契約影響見 [contracts.md 文末一節](../contracts.md#s4-a-契約影響與遷移邊界)。

每項陳述標明依據：

| 標記 | 意思 |
|---|---|
| 【官方】 | 標準原文、官方規格或官方 repo 的文字；有固定 commit 者另註 |
| 【實測】 | 本 repo 內可重跑的測試輸出，附證據位置 |
| 【推論】 | 依上述事實的工程判斷，尚未實測 |
| 【第三方】 | 社群或廠商專案的自述，未獨立驗證 |

**SDK 與實作狀態的說法以固定 commit 為準**；沒有固定 commit 的，只寫「查核範圍內未見」，不寫「不支援」。

## 1. 查核範圍與固定 commit

原始碼與標準原文以 `artifacts/s4-a-sources/check-sources.sh` 依下列 commit 抓取並檢查，輸出在 `artifacts/s4-a-sources/runs/20260930T080431Z/findings.txt`（repo 內被 Git 忽略，只存在本機，可重跑；每次寫新目錄，較早的 `runs/20260930T074537Z/` 保留，少了 x402 規格的 gas、預設路徑與競態風險幾行）。

| repo | commit | commit 時間 | 查核內容 |
|---|---|---|---|
| ethereum/ERCs | `5993dff16ae5003b1eb9c450cf71159e5ae89275` | 2026-09-29 | ERC-8196、8126、8004、7710、3009 的 front matter；8196 第 58 行 |
| ethereum/EIPs | `94796f65120a8a17d3488ba948eed522912bef99` | 2026-09-30 | EIP-7702 front matter |
| x402-foundation/x402 | `6b6ee91fee027b540faabcb25774e73851006c3b` | 2026-09-29 | exact EVM 規格；ts／go／python 的 EVM exact 機制；v2 TS facilitator；ERC-7702 client 範例 |
| MetaMask/delegation-framework | `bff4b08f8006ad94322a6e3da8d90f274e20325d` | 2026-09-11 | enforcer 原始碼註解與檢查；`DelegationManager` 撤銷函式；部署文件 |
| MetaMask/smart-accounts-kit | `555dd2043a69d4febba108465c5d79aabc65abdb` | 2026-09-29 | `packages/x402` 原始碼 |

另以網頁讀取（未固定 commit）：[eips.ethereum.org](https://eips.ethereum.org) 上各標準的規格內容、[smart-accounts-kit issue #301](https://github.com/MetaMask/smart-accounts-kit/issues/301)、[erc-8004-contracts](https://github.com/erc-8004/erc-8004-contracts) README、社群 ERC-8196 實作 [askgrokwallet](https://github.com/richard7463/askgrokwallet) README。

沒有做：S4-B、任何鏈上呼叫、託管 facilitator（x402.org 等）的實測、Delegation Manager 與 DeleGator 的逐行審閱。

## 2. 標準與元件事實

| 項目 | 狀態 | 與本案有關的內容 |
|---|---|---|
| EIP-3009 | Draft（ERCs `5993dff`）【官方】 | 代幣合約須自行實作；nonce 為每個授權者唯一的隨機 32 bytes；合約呼叫端應用 `receiveWithAuthorization` 防搶跑；有 `cancelAuthorization`【官方】。Base Sepolia USDC 實作此介面，是 x402 的預設路徑 |
| x402 exact EVM 規格 | x402 `6b6ee91`【官方】 | `assetTransferMethod` 有 `eip3009`、`permit2`、`erc7710`，放在 requirements 的 `extra`，未指定時預設 `eip3009`（第 15 行）；facilitator 付 gas（第 5 行）；3009 需要 `extra.name`／`version`（第 72–73 行）；`erc7710` 的 payload 為 `delegationManager`、`permissionContext`、`delegator`，驗證靠模擬 `redeemDelegations`；`erc7710` 一節（約第 260–330 行）沒有提到 nonce 或 replay，並自列競態風險：client 可能在模擬與實際執行之間作廢 delegation（第 309 行） |
| x402 官方 SDK 的 EVM exact | x402 `6b6ee91`【官方】 | ts、go、python 三個 exact 機制目錄中，未見 `7710`、`redeemDelegations` 或 `delegationManager`；整個 repo 沒有以 7710 命名的檔案；v2 TS facilitator 只有 `eip3009`、`permit2`、`erc20approval` |
| x402 對 7702 帳戶的處理 | x402 `6b6ee91`【官方】 | 有 `examples/typescript/clients/erc7702/`：7702 帳戶的 client 設定與一般 EOA 相同。facilitator `eip3009.ts` 第 182–185 行：地址沒有 code 用 ecrecover，有 code 用 strict EIP-1271；註解說明不做 ECDSA fallback，原因已在 Base Sepolia 實際確認。是否接受簽章取決於 delegate 的 validator |
| MetaMask smart-accounts-kit | `555dd20`【第三方】 | `packages/x402` 有 `x402Client.ts` 與 `x402ExactEvmErc7710ServerScheme.ts`；client 只處理 `assetTransferMethod` 為 `erc7710` 的報價（第 93、108 行）。issue #301（2026-09-10）指出文件未寫明賣方須宣告此值 |
| EIP-7702 | Final（EIPs `94796f6`）【官方】 | 授權 tuple `[chain_id, address, nonce, y_parity, r, s]`；`chain_id=0` 對所有鏈有效；委派持續存在，設為零地址才清除；重播保護須由 delegate 實作；初始化可能被搶跑；換 delegate 有 storage 衝突；會破壞依賴 `tx.origin` 的保護 |
| ERC-7710 | Draft（ERCs `5993dff`）【官方】 | requires 1271、7579；`redeemDelegations(bytes[], bytes32[], bytes[])`；caveat 與撤銷在標準範圍外；不能因為過去收到 delegation 就假設未來仍可執行 |
| MetaMask Delegation Framework | `bff4b08`【官方原始碼】 | 部署文件 v1.3.0：`DelegationManager` `0xdb9B1e94B5b69Df7e401DDbedE43491141047dB3`、`EIP7702StatelessDeleGatorImpl` `0x63c0c19a282a1B52b07dD5a65b58948A07DAE32B`，網路清單含 Base Sepolia（未上鏈核對）。`DelegationManager` 有 `disableDelegation`／`enableDelegation`。repo 有 `audits/` 資料夾，涵蓋範圍未查。Apache-2.0／MIT |
| 關鍵 enforcers | `bff4b08`【官方原始碼】 | `ERC20TransferAmountEnforcer` 只檢查目標為指定代幣、方法為 `transfer`、累計金額不超過上限，**不檢查收款人**；`AllowedCalldataEnforcer` 可固定部分 calldata（適合固定收款人參數）；`ExactCalldataEnforcer` 要求 calldata 完全相同；`ERC20PeriodTransferEnforcer` 每期上限；`TimestampEnforcer`、`LimitedCallsEnforcer`、`NonceEnforcer`（遞增 nonce 可批次撤銷）；`RedeemerEnforcer` 限制可 redeem 的地址，但註解明寫「具 delegation 功能的 DeleGator 帳戶可以再委派給其他地址來繞過」 |
| ERC-8196 | Final（ERCs `5993dff`）【官方】 | requires 155、191、712、4337、8126。第 58 行：實作預期是智慧合約，「例如 ERC-4337 帳戶或專用的政策執行模組」。`executeAction(policyHash, target, value, data, nonce, entropyCommitment, signature)`；執行前 MUST 以 ERC-8126 查核，分數超過 `minVerificationScore` 即拒絕；`maxValuePerTx`／`maxValuePerDay` 以 wei 計、作用於 `value`；audit entry MUST 含 `previousHash`；文中未提 7702、7710、x402；沒有參考實作一節 |
| ERC-8126 | Final（ERCs `5993dff`）【官方】 | requires 含 3009 與 8004；主要在鏈下；`getLatestRiskScore(uint256) returns (uint8)` 屬可選介面，查無資料 MAY revert；分數 0–100，越低越安全；時效未定義，撤銷未處理；參考實作回固定分數，proof ID 為 SHA-256 |
| ERC-8004 | **Draft**（ERCs `5993dff`）【官方】 | `agentId` 是各 registry 的 ERC-721 tokenId，以 `eip155:{chainId}:{registry}` 區分；`agentWallet` 改動要 EIP-712 或 ERC-1271 簽章，token 轉移時清除 |
| ERC-8004 部署 | README（未固定 commit）【官方】 | Base Sepolia IdentityRegistry `0x8004A818BFB912233c491871b3d84c89A494BD9e`、ReputationRegistry `0x8004B663056A597Dffe9eCcC1965A193B7388713`；Validation Registry 仍在討論（未上鏈核對） |
| 社群 ERC-8196 實作 | askgrokwallet README【第三方】 | 自述「Unaudited developer preview」；8126 預查預設關閉，沒有訂閱金鑰時付費查詢回 402，判定收緊為 ask |

**依賴鏈狀態**：ERC-8196（Final）→ ERC-8126（Final）→ ERC-8004（Draft）。最底層的身份 registry 仍是 Draft，介面可能再變【官方狀態＋推論】。

## 3. 威脅模型

與 ADR 相同，分三層：

- **T1 Agent 被控制**：能提出請購、呼叫受限工具；**沒有付款私鑰，也沒有鏈上權限**。在基線下，繞過 Go 入口不代表能付款。
- **T2 受限執行權限失守**：`AuthorizationGrant`、7710 delegation 與執行身份、7702 delegate 接受的受限簽章金鑰、8196 policy 的 agent 金鑰被濫用或外流。
- **T3 Signer／主私鑰失守**：任何選項都沒有鏈上防線；7702 不保護失守的主私鑰。

## 4. 需求對照矩陣

候選可以組合。欄位只看「這個候選本身在鏈上能保證什麼」；本案鏈下元件（Policy、approval、Signer）另外存在。

| 本案需求 | A：EOA＋EIP-3009 | B1：7702＋7710＋enforcers | B2：7702＋EIP-3009 經 ERC-1271 | C：ERC-8196 模組 |
|---|---|---|---|---|
| R1 代幣轉帳的收款人、資產、金額受限 | 簽章涵蓋 `to`／`value`，代幣 domain 決定資產【官方】；簽什麼由 Signer 決定，鏈上不設限 | TransferAmount 限代幣與金額；收款人另用 AllowedCalldata 或 ExactCalldata【官方原始碼】 | validator 要從簽章夾帶的 typed-data 原文解析收款人與金額【推論】 | `allowedContracts` 只能限到代幣合約；`value` 以 wei 計，不涵蓋 ERC-20 calldata【官方＋推論】 |
| R2 task 累計預算 | 自建 ZK 預算合約，另一筆交易【計畫 §10】 | Period／累計 enforcer 可設上限但在鏈上公開，與 ZK 隱藏預算衝突；綁 ZK 需自訂 enforcer【推論】 | validator 需自寫累計狀態【推論】 | `maxValuePerDay` 只針對 native value【官方】 |
| R3 時窗 | `validAfter`／`validBefore`【官方】 | TimestampEnforcer【官方原始碼】 | 3009 本身的時窗【官方】 | policy 的 `validAfter`／`validUntil`【官方】 |
| R4 單次使用、防重播 | 代幣強制每個授權者的 nonce 唯一【官方】 | x402 `erc7710` 未明寫；需 LimitedCalls／Nonce enforcer 配合【官方＋原始碼】；S4-B 必測 | 3009 nonce 由代幣強制【官方】 | nonce 唯一性為 should【官方】 |
| R5 A／B 各自獨立 | 各自 EOA、各自金鑰（計畫已定） | 各自 EOA 各自委派；S4-B 必測 | 同 B1 | 各自 policy；依賴底層帳戶 |
| R6 T1 下無法付款 | 是：Agent 沒有金鑰【計畫】 | 是，前提是 delegation 與執行身份不交給 Agent | 同 B1 | 是，前提是 policy 的 agent 金鑰不交給 Agent |
| R7 T2 下限制仍在鏈上生效 | 否：限制在 Signer（鏈下） | 是，由 enforcers 強制【推論，待 S4-B】 | 是，由 validator 強制【推論】 | native value 與目標合約是；ERC-20 收款人需自寫【官方＋推論】 |
| R8 T3 | 無防線 | 無防線；owner 可直接送交易、改或清除委派【官方 7702】 | 同 B1 | 無防線；owner 可改 policy |
| R9 與 `intent_hash`、ZK 的密碼學綁定 | ZK 合約 emit `intent_hash`，Signer 簽前核對【計畫】 | ExactCalldata 可綁單筆轉帳內容；綁 `intent_hash`／ZK 需自訂 enforcer【推論】 | validator 可檢查 `intent_hash` 相關資料，需自寫【推論】 | 規格沒有外部 proof 掛點，需自寫【推論】 |
| R10 x402 真結算路徑 | 官方 SDK 支援；9/22 已對 test402 取得 402（計畫 §10） | 規格有；官方 SDK 在 `6b6ee91` 未見；MetaMask client 有；facilitator 支援未查 | 官方 SDK 在 `6b6ee91` 已依 code 路由到 EIP-1271，並有範例【官方】 | x402 規格沒有呼叫 `executeAction` 的路徑，需自訂 adapter 或 facilitator 擴充（成本，不是不可能）【推論】 |
| R11 撤銷 | grant 到期（鏈下）；`cancelAuthorization`【官方】 | `disableDelegation`、NonceEnforcer【官方原始碼】 | 換 validator 設定或清除委派【推論】 | `revokePolicy`【官方】 |
| R12 稽核鏈 | 本案事件＋B1-05 hash chain【計畫】 | 標準沒有 | 標準沒有 | audit entry MUST 含 `previousHash`【官方】 |

## 5. 若採 ERC-8196，必須先定的查詢語意

ERC-8126 沒有定義、而 ERC-8196 強制依賴的部分：

| 情況 | 標準怎麼說 | 本案若採用須定為 |
|---|---|---|
| 查無分數 | `getLatestRiskScore` MAY revert【官方】 | fail closed：Deny，reason `RISK_SCORE_UNAVAILABLE` |
| provider 失敗 | 錯誤碼 `0x04` VerificationFailed、`0x07` ProviderUnavailable【官方】 | fail closed，不重試到成功為止 |
| 分數時效 | 未定義；`getLatestRiskScore` 只回 `uint8`，沒有時間戳【官方】 | 另讀 `AttestationPosted` 事件或 provider API 取得時間，自訂 `max_age`，過期視同無分數【推論】 |
| 撤銷 | 8126 未處理；8004 在 token 轉移時清除 `agentWallet`【官方】 | 每次查核重核 `agentWallet` 與 registry 對應 |
| 門檻方向 | 分數超過 `minVerificationScore` 就拒絕，越低越安全【官方】 | 欄位名稱是 min，語意是上限，設定與測試要防寫反【推論】 |
| 證據 | 參考實作回固定分數【官方】 | fixture provider 只能標 fixture，不得當外部信任驗證通過 |

## 6. 工時粗估（推論）

| 項目 | 估計 | 主要不確定性 |
|---|---|---|
| A 基線 | 已排入原計畫（Signer、ZK 預算合約 W10） | 不另增 |
| B1 PoC | 3–6 個工作天 | facilitator 對 `erc7710` 的支援；不支援時是否自建由 Aaron 決定 |
| B2 PoC | 3–6 個工作天 | validator 自寫或採用；夾帶 typed-data 的簽章格式與 client 配合 |
| C PoC | 5–10 個工作天以上 | 模組本體、ERC-20 calldata 檢查、8126 provider 或 fixture、8004 註冊、x402 adapter 都要做 |

## 7. 暫定研究排序（供 ADR）

依目前證據與成本排序，不是採用決定：

1. **A 維持基線**。
2. **B1 先做**：enforcers 現成，直接回答 T2；第一步查 facilitator 支援。
3. **B2 第二**：官方 SDK 已支援路由，代價在 validator。
4. **C 第三**：成本最高，且 8126 語意與 8004 狀態未定；可先借用 policy hash 與 `previousHash` 兩個概念。
5. **ERC-8126／8004 不接入 M1 或 M2 的 Policy**；日後需要鏈上 agent 身份時，只在可信 registry 保留可選對應。

## 8. 限制與待查

- Delegation Manager 與 DeleGator 未逐行審閱；enforcer 只讀註解與 `require` 檢查。
- 託管 facilitator 對 `erc7710` 的支援未查；x402 以外的第三方 facilitator（例如 `t402` fork 的 Go 實作）未採信。
- B2 的現成 validator（例如 7579 session-key 模組）未查。
- 部署地址（Delegation Framework v1.3.0、ERC-8004）只來自文件，未上鏈核對。
- 社群 ERC-8196 實作未審閱程式。
- 本輪文件盤點在 S4 卡的 2–4 小時探索上限內完成；PoC 工時見第 6 節，是否進行由 Aaron 決定。
