# ADR：付款帳戶模型與受限付款路徑

- **狀態**：Proposed（S4-A 草案，待 Aaron 核准）。未採用任何候選。
- **日期**：2026-09-30
- **決策者**：Aaron
- **依據**：[相容性盤點](../spikes/payment-policy-compatibility.md)、[契約影響與遷移邊界](../contracts.md#s4-a-契約影響與遷移邊界)；vault 執行計畫 §1-septies、§3.5、§10，PRD §12、§14。
- **契約審查對象**：`57791d1`（分支 `b1-02-contracts`）。

## 範圍

決定正式付款模組（wallet、Signer、execution adapter、settlement）開工前，消費者帳戶採什麼模型、受限付款走哪條路徑。M1 不受影響：M1 沒有付款能力。

**已定、不由本 ADR 決定**：A、B 各有獨立錢包，店家另有 M 地址；不以一個共享帳戶加兩份 permission 代替；驗收 44（跨消費者隔離）無論選哪條路徑都保留。

## 背景

現行基線（計畫 §1-septies、§10）：每位消費者一個 EOA，金鑰由隔離的 Signer 保管；Signer 只在同時拿到一次性 `AuthorizationGrant` 與鏈上已驗的 `intent_hash` 時，才簽 EIP-3009 `transferWithAuthorization`；task 預算由自建 ZK 合約在另一筆交易扣減。

研究問題（計畫 §3.5）：**受限 Agent 繞過 Go 入口後，鏈上是否仍阻止越權付款。** 在基線下答案是否定的：鏈上沒有受限權限，防線完全在 Signer 與 grant。

## 選項

三個選項可以組合，不是互斥的三選一。

### A. 保留基線：EOA＋EIP-3009

- **現有元件**：USDC 的 EIP-3009、x402 官方 SDK 的 `eip3009` 路徑、x402.org facilitator。
- **自寫範圍**：Signer、grant、ZK 預算合約（已在計畫內）。
- **信任假設**：Signer 與 host 正確；鏈上只保證簽章、nonce 與時窗。
- **優點**：官方支援最完整，計畫已排。
- **缺點**：無法宣稱鏈上錢包政策保障；Signer 或其金鑰失守即可任意付款。

### B. 7702 委派帳戶＋ERC-7710 delegation＋caveat enforcers

- **現有元件**：EIP-7702（Final）、MetaMask Delegation Framework 的 EIP-7702 Stateless DeleGator 與 enforcers、x402 exact 規格的 `assetTransferMethod=erc7710`、MetaMask 的 `x402Erc7710Client`。
- **自寫範圍**：delegation 與 enforcer 組合設定；綁 `intent_hash` 或 ZK 預算的自訂 enforcer（若要）；facilitator 若不支援 `erc7710` 則要自架。
- **信任假設**：enforcer 與 Delegation Manager 合約正確；委派初始化沒被搶跑；EOA 主私鑰仍在 Signer，主私鑰不受 delegation 限制。
- **優點**：受限權限在鏈上強制，直接回答研究問題；不改地址。
- **缺點**：ERC-7710 仍是 Draft；官方 TS SDK 未實作 `erc7710`；facilitator 支援未知；鏈上 period cap 會公開額度，與 ZK 隱藏預算衝突。

### C. ERC-8196 政策模組

- **現有元件**：標準介面（Final）；ERC-8004 在 Base Sepolia 有 registry 部署；沒有標準參考實作，社群實作自述未稽核。
- **自寫範圍**：模組本體（4337 帳戶或 7702 delegate）、ERC-20 calldata 的收款人與金額檢查、ERC-8126 provider 串接或 fixture、8004 註冊、x402 接法。
- **信任假設**：ERC-8126 provider 的分數可信且即時；8004 registry 對應正確。
- **優點**：policy 註冊、撤銷、`previousHash` 稽核鏈有標準介面。
- **缺點**：限額以 wei 作用於 native value，不涵蓋 USDC 金額；x402 沒有路徑；8126 的查詢失敗與分數時效未定義；最底層 ERC-8004 仍是 Draft。

## 建議（草案，待核准）

1. **A 維持基線**，也是正式付款模組的預設路徑，直到 S4-B 有證據。
2. **S4-B 的候選選 B**，接受條件見下節。
3. **C 不列為付款路徑候選。** 保留兩個概念：policy hash 綁定，以及 `previousHash` 稽核鏈（B1-05 已計畫 hash chain）。若 Aaron 要求 ERC-8196 相容，另開「8196 介面 profile」研究，不塞進 S4-B。
4. **ERC-8126／8004 不接入 Policy。** 若之後需要鏈上 agent 身份，只在可信 registry 保留可選的 8004 對應，細節見契約影響一節。

## S4-B 接受條件（候選 B）

S4-B 需 Aaron 另行核准與 testnet opt-in。通過須同時滿足：

- 固定版本或 commit：DeleGator、Delegation Manager、enforcers、x402 client、facilitator。
- A、B 各自獨立的 7702 帳戶與 M 地址；A 的 delegation 不能操作 B 的帳戶。
- delegation 限定代幣合約、`transfer` 方法、收款人 M、單筆金額、時窗與使用次數。
- 繞過 Go 入口的負向案例：agent 直接呼叫 `redeemDelegations`，改收款人、改金額、重送同一筆、超過時窗，全部 revert。
- 真實 x402 verify→settle，透過選定的 client 與 facilitator；只測直接合約呼叫不算。
- 委派初始化搶跑、撤銷後再用、換 delegate 的 storage 測試。
- `intent_hash` 與 ZK 預算的綁定方式有結論：做自訂 enforcer，或明確列為範圍外並寫清殘餘風險。
- 記錄 gas、延遲與每筆成本；不上主網。

## 未解問題

1. x402.org facilitator 是否支援 `erc7710`；若要自架，信任邊界怎麼寫。
2. enforcer 能否表達「單筆金額等於報價、收款人等於 M」，同時不在鏈上公開 task 額度。
3. 7702 委派後，EOA 主私鑰仍在 Signer：鏈上限制只約束 delegate 路徑，Signer 失守時不受限。
4. `intent_hash` 是 SHA-256 的 256 bits，超過 BN254 scalar field，作為 Groth16 public input 需要拆成兩個 limb 或改用 field hash（交 S3 與 v2 契約）。
5. EIP-3009 與 ERC-7710 都還是 Draft；USDC 已實作 3009 是事實，7710 沒有對應的事實基礎。

## 後果

- **採用 A**：現有計畫不變；README 與面試說法不得宣稱鏈上錢包政策保障。
- **採用 B**：正式付款派工前，同步 PRD（FR-4／7／11／13–15／19／24）、契約 v2、Signer 職責、狀態機、驗收與圖表，並重估工時（PRD §12）。
- **兩者都成立**：契約 v1 的 fixture digest 不得作為任何鏈上、EIP-712 或 ZK 值；正式路徑用 v2 契約與新的 golden vectors。

## 證據等級

本 ADR 的事實來自官方文件（2026-09-30 查核）與本 repo 的契約探測測試；沒有鏈上實測。每條陳述的來源與標記見[相容性盤點](../spikes/payment-policy-compatibility.md)。
