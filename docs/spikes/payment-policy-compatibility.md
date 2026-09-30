# 付款政策與帳戶模型相容性盤點（S4-A）

狀態：S4-A 研究草稿，待 Aaron 或指定 reviewer 接受。**不代表採用任何候選。** 本輪沒有 PoC、部署、注資或交易，也沒有讀合約原始碼。

- 查核日期：2026-09-30。
- 契約審查對象：`57791d1`（分支 `b1-02-contracts`）。
- 決策草案見 [ADR：付款帳戶模型](../adr/payment-account-model.md)，契約影響見 [contracts.md 文末一節](../contracts.md#s4-a-契約影響與遷移邊界)。

每項陳述標明依據：

| 標記 | 意思 |
|---|---|
| 【官方】 | 標準原文、官方規格或官方 repo 的文字，查核日當下內容 |
| 【實測】 | 本 repo 內可重跑的測試輸出，附證據位置 |
| 【推論】 | 依上述事實的工程判斷，尚未實測 |
| 【第三方】 | 社群或廠商專案的自述，未獨立驗證 |

## 1. 查核範圍

讀過的一手來源：

- [ERC-8196](https://eips.ethereum.org/EIPS/eip-8196)（另核 [raw 原文](https://raw.githubusercontent.com/ethereum/ERCs/master/ERCS/erc-8196.md)）、[ERC-8126](https://eips.ethereum.org/EIPS/eip-8126)、[ERC-8004](https://eips.ethereum.org/EIPS/eip-8004)（另核 raw 原文）
- [EIP-7702](https://eips.ethereum.org/EIPS/eip-7702)、[ERC-7710](https://raw.githubusercontent.com/ethereum/ERCs/master/ERCS/erc-7710.md)、[EIP-3009](https://eips.ethereum.org/EIPS/eip-3009)
- [x402 exact EVM 規格](https://github.com/x402-foundation/x402/blob/main/specs/schemes/exact/scheme_exact_evm.md)與[官方 TypeScript EVM exact 實作目錄](https://github.com/x402-foundation/x402/tree/main/typescript/packages/mechanisms/evm/src/exact)
- [MetaMask Delegation Framework](https://github.com/MetaMask/delegation-framework)（README 與 `src/enforcers/` 檔名）、[smart-accounts-kit issue #301](https://github.com/MetaMask/smart-accounts-kit/issues/301)
- [erc-8004-contracts](https://github.com/erc-8004/erc-8004-contracts) README；社群 ERC-8196 實作 [askgrokwallet](https://github.com/richard7463/askgrokwallet) README

沒有做：S4-B、任何鏈上呼叫、enforcer 與 Delegation Manager 原始碼逐行閱讀、facilitator 對 `erc7710` 的實測、x402 Go SDK 查核。

## 2. 標準與元件事實

| 項目 | 狀態 | 與本案有關的內容 |
|---|---|---|
| EIP-3009 | Draft（2020-09-28）【官方】 | 代幣合約須自行實作；nonce 為每個授權者唯一的隨機 32 bytes，非遞增；合約呼叫端應用 `receiveWithAuthorization` 防搶跑；有 `cancelAuthorization`【官方】。Base Sepolia USDC 實作此介面，是 x402 的預設路徑（計畫 §10 對 test402 的 9/22 實打紀錄） |
| x402 exact EVM | 規格在 main 分支，x402Version 2【官方】 | `assetTransferMethod` 可為 `eip3009`、`permit2`、`erc7710`，放在 requirements 的 `extra`，未指定時預設 `eip3009`；`eip3009` 需要 `extra.name`／`extra.version` 作為代幣 EIP-712 domain；`erc7710` 的 payload 是 `delegationManager`、`permissionContext`、`delegator`，驗證完全靠模擬 `redeemDelegations`，gas 由 facilitator 付；`erc7710` 一節沒有明寫重播保護【官方】 |
| x402 官方 TS SDK（EVM exact） | 查核日目錄 | README 只列 EIP-3009 與 Permit2，未見 `erc7710`、delegation 或 7702【官方】 |
| MetaMask smart-accounts-kit | 廠商套件 | 有 `x402Erc7710Client`，只處理 `assetTransferMethod: erc7710`，賣方須明確宣告；一般 exact v2 賣方會失敗（issue #301，2026-09-10）【第三方】 |
| EIP-7702 | Final（Core）【官方】 | 授權 tuple `[chain_id, address, nonce, y_parity, r, s]`；`chain_id=0` 對所有鏈有效；委派持續存在，設為零地址才清除；重播保護須由 delegate 實作並納入簽章；初始化可能被搶跑，須驗 EOA 簽章；換 delegate 有 storage 衝突；會破壞依賴 `tx.origin` 的保護【官方】 |
| ERC-7710 | Draft【官方】 | 依賴 ERC-1271、ERC-7579；`redeemDelegations(bytes[] permissionContexts, bytes32[] modes, bytes[] executionCallData)`；caveat 與撤銷在標準範圍外；「不能因為過去收到 delegation 就假設未來仍可執行」【官方】 |
| MetaMask Delegation Framework | Apache-2.0／MIT，repo 有 `audits/`【官方】 | 有 EIP-7702 Stateless DeleGator。`src/enforcers/` 含 `ERC20TransferAmountEnforcer`、`ERC20PeriodTransferEnforcer`、`ERC20StreamingEnforcer`、`AllowedTargetsEnforcer`、`AllowedMethodsEnforcer`、`AllowedCalldataEnforcer`、`ExactCalldataEnforcer`、`TimestampEnforcer`、`LimitedCallsEnforcer`、`RedeemerEnforcer`、`NonceEnforcer` 等【官方，功能依檔名判斷，未讀原始碼】。Base Sepolia 部署地址未查 |
| ERC-8196 | Final（2026-03-14）【官方】 | requires 155、191、712、4337、8126；`registerPolicy`／`executeAction(policyHash, target, value, data, nonce, entropyCommitment, signature)`／`revokePolicy`；「執行任何 agent 動作前 MUST 以 ERC-8126 查核」，分數超過 `minVerificationScore` 即拒絕；`maxValuePerTx`／`maxValuePerDay` 以 wei 計，作用於 `value`；目標合約以 `allowedContracts`／`blockedContracts` 管理；audit entry MUST 含 `previousHash`；文中未提 7702、7710、x402；沒有 Reference Implementation 一節【官方】 |
| ERC-8126 | Final（2026-01-15）【官方】 | requires 包含 3009 與 8004；主要在鏈下；`getLatestRiskScore(uint256 agentId) returns (uint8)` 屬可選的鏈上介面，查無資料時 MAY revert；分數 0–100，越低越安全；時效未定義，撤銷未處理；參考實作各項回固定分數，proof ID 為 SHA-256 雜湊【官方】 |
| ERC-8004 | **Draft**（2025-08-13）【官方】 | `agentId` 是各 registry 的 ERC-721 tokenId，以 `eip155:{chainId}:{registry}` 命名空間區分；`agentWallet` 預設為擁有者，改動要 EIP-712 或 ERC-1271 簽章，token 轉移時清除【官方】 |
| ERC-8004 部署 | 官方 contracts repo README | Base Sepolia IdentityRegistry `0x8004A818BFB912233c491871b3d84c89A494BD9e`、ReputationRegistry `0x8004B663056A597Dffe9eCcC1965A193B7388713`；Validation Registry 仍在討論；未提稽核【官方，未上鏈核對】 |
| 社群 ERC-8196 實作 | askgrokwallet | 自述為「Unaudited developer preview」；8126 預查預設關閉，沒有訂閱金鑰時付費查詢回 402，判定收緊為 ask【第三方】 |

**依賴鏈狀態**：ERC-8196（Final）→ ERC-8126（Final）→ ERC-8004（Draft）。最底層的身份 registry 仍是 Draft，介面可能再變【官方狀態＋推論】。EIP-3009 本身也是 Draft，但 USDC 已實作，x402 以它為預設路徑。

## 3. 需求對照矩陣

候選不是三選一，可以組合。欄位以「這個候選本身能否在鏈上保證」判斷；本案鏈下元件（Policy、approval、Signer）另外存在。

| 本案需求 | EOA＋EIP-3009（基線） | 7702＋ERC-7710＋enforcers | ERC-8196 模組 |
|---|---|---|---|
| R1 代幣轉帳的收款人、資產、金額受限 | 授權簽章涵蓋 `to`／`value`，代幣 domain 決定資產【官方】；**簽什麼由 Signer 決定，鏈上不設限** | 可用 AllowedTargets（代幣）＋AllowedMethods（transfer）＋AllowedCalldata／ExactCalldata（收款人）＋ERC20TransferAmount 組合限制【推論，依 enforcer 檔名】 | `allowedContracts` 只能限到代幣合約；`value` 以 wei 計，不涵蓋 ERC-20 calldata 裡的金額與收款人【官方＋推論】；須自寫 |
| R2 task 累計預算 | 自建 ZK 預算合約，與付款是兩筆交易【計畫 §10】 | Period／Streaming enforcer 可設累計上限，但額度在鏈上公開，與 ZK 隱藏預算衝突；要綁 ZK 須自寫 enforcer【推論】 | `maxValuePerDay` 只針對 native value【官方】 |
| R3 時窗 | `validAfter`／`validBefore`【官方】 | TimestampEnforcer【檔名】 | policy 層 `validAfter`／`validUntil`【官方】 |
| R4 單次使用、防重播 | 代幣強制每個授權者的 nonce 唯一【官方】 | x402 `erc7710` 一節未寫；需 LimitedCalls／Nonce enforcer 與 Delegation Manager 行為配合【官方＋檔名】；S4-B 必測 | nonce 唯一性為 should【官方】 |
| R5 A／B 各自獨立，A 的權限不能動 B | 各自 EOA、各自金鑰（計畫已定） | 各自 EOA 各自委派，不共用 delegate 狀態；S4-B 必測 | 各自 policy；依賴底層帳戶 |
| R6 受限 Agent 繞過 Go 入口仍擋越權付款 | 否：鏈上沒有受限權限，防線全在 Signer 與 grant【推論】 | 可以，前提是 enforcer 組合正確，由 `redeemDelegations` 在鏈上強制【推論，待 S4-B 實測】 | native value 與目標合約可以；ERC-20 收款人依規格不行【官方＋推論】 |
| R7 與 `intent_hash`、ZK 的密碼學綁定 | ZK 合約 emit `intent_hash`，Signer 簽前核對【計畫 §1-septies】 | ExactCalldata 可綁單筆轉帳內容；綁 `intent_hash` 或 ZK 狀態需自寫 enforcer【推論】 | 規格沒有外部 proof 掛點，需自寫【推論】 |
| R8 x402 真結算路徑 | 官方 SDK 支援；9/22 已對 test402 取得 402（計畫 §10） | 規格支援；官方 TS SDK 未實作；MetaMask client 有；facilitator 支援未查【官方＋第三方】 | x402 規格沒有此路徑，facilitator 需呼叫 `executeAction`，不在標準內【推論】 |
| R9 Ask 的人類核准 | 鏈下 approval service 簽 human grant【計畫】 | 同左；也可把單次核准表達為一次性 delegation，但簽 delegation 的仍是帳戶擁有者金鑰【推論】 | 同左 |
| R10 撤銷 | grant 到期（鏈下）；`cancelAuthorization`【官方】 | 撤銷機制在參考實作、不在標準內【官方】 | `revokePolicy`【官方】 |
| R11 稽核鏈 | 本案事件＋B1-05 hash chain【計畫】 | 標準沒有 | audit entry MUST 含 `previousHash`【官方】 |
| R12 主私鑰失守 | 全部失守 | 7702 不改變：EOA 私鑰仍可送交易、改或清除委派【官方】 | 擁有者金鑰仍控制 policy |

## 4. 若採 ERC-8196，必須先定的查詢語意

以下是 ERC-8126 沒有定義、而 ERC-8196 又強制依賴的部分，列出本案必須自訂的處理方式：

| 情況 | 標準怎麼說 | 本案若採用須定為 |
|---|---|---|
| 查無分數 | `getLatestRiskScore` MAY revert【官方】 | fail closed：Deny，reason `RISK_SCORE_UNAVAILABLE` |
| provider 失敗 | 錯誤碼 `0x04` VerificationFailed、`0x07` ProviderUnavailable【官方】 | fail closed，不重試到成功為止 |
| 分數時效 | 未定義；`getLatestRiskScore` 只回 `uint8`，沒有時間戳【官方】 | 須另讀 `AttestationPosted` 事件或 provider API 取得時間，自訂 `max_age`，過期視同無分數【推論】 |
| 撤銷 | 8126 未處理；8004 在 token 轉移時清除 `agentWallet`【官方】 | 每次查核重核 `agentWallet` 與 registry 對應 |
| 門檻方向 | 分數超過 `minVerificationScore` 就拒絕，越低越安全【官方】 | 欄位名稱是 min，實際語意是上限，設定與測試要防寫反【推論】 |
| 證據 | 參考實作回固定分數【官方】 | fixture provider 只能標 fixture，不得當外部信任驗證通過 |

## 5. 工時粗估（推論）

| 項目 | 估計 | 主要不確定性 |
|---|---|---|
| 基線 | 已排入原計畫（Signer、ZK 預算合約 W10） | 不另增 |
| S4-B：7702＋7710 PoC | 3–6 個工作天 | facilitator 是否支援 `erc7710`；若要自架 facilitator，信任邊界也要重寫 |
| ERC-8196 模組 PoC | 5–10 個工作天以上 | 規格無參考實作；ERC-20 calldata 檢查要自寫；8126 provider 取得或自建 fixture；8004 註冊；x402 沒有現成接法 |

## 6. 本輪結論（供 ADR）

1. **基線保留**：EOA＋EIP-3009 仍是 x402 的預設路徑與官方 SDK 唯一完整支援的 EVM 路徑之一。
2. **S4-B 候選建議 7702＋7710＋enforcers**：x402 規格已有 `erc7710` 路徑，enforcer 可在鏈上限制代幣、收款人、金額與時間，最貼近「繞過 Go 入口仍擋越權」的研究問題。
3. **ERC-8196 不列為付款路徑候選**：要求 4337、限額作用於 wei 而非 ERC-20 金額、x402 沒有接法、8126 的查詢失敗與時效未定義、最底層 8004 仍是 Draft、規格沒有參考實作。可以借用的概念有兩個：policy hash 綁定與 `previousHash` 稽核鏈，後者本案 B1-05 已在計畫內。
4. **ERC-8126／8004 不接入 M1 或 M2 的 Policy**：若之後需要鏈上 agent 身份，只在可信 registry 保留可選的 8004 對應，不讓模型或請購欄位填寫。

## 7. 限制與待查

- enforcer 功能依檔名判斷，未讀原始碼，也未查 Base Sepolia 部署地址。
- x402.org 或其他 facilitator 是否支援 `erc7710` 未查；x402 Go SDK 未查（非官方 fork `t402` 有 Go 版 `erc7710` facilitator，未採信）。
- ERC-8004 地址只來自 README，未上鏈核對。
- 社群 ERC-8196 實作未審閱程式。
- 本輪文件盤點在 S4 卡的 2–4 小時探索上限內完成；完整 PoC 工時見第 5 節，是否進行由 Aaron 決定。
