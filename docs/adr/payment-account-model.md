# ADR：付款帳戶模型與受限付款路徑

- **狀態**：Proposed（S4-A 草案，第二版，待 Aaron 核准）。未採用任何候選。
- **日期**：2026-09-30
- **決策者**：Aaron
- **依據**：[相容性盤點](../spikes/payment-policy-compatibility.md)（含固定 commit 的來源查核）、[契約影響與遷移邊界](../contracts.md#s4-a-契約影響與遷移邊界)；vault 執行計畫 §1-septies、§3.5、§10，PRD §12、§14，任務清單 S4-A／S4-B。
- **契約審查對象**：`57791d1`（分支 `b1-02-contracts`）。

## 範圍

決定正式付款模組（wallet、Signer、execution adapter、settlement）開工前，消費者帳戶採什麼模型、受限付款走哪條路徑。M1 不受影響：M1 沒有付款能力。

**已定、不由本 ADR 決定**：A、B 各有獨立錢包，店家另有 M 地址；不以一個共享帳戶加兩份 permission 代替；驗收 44（跨消費者隔離）無論選哪條路徑都保留。

## 背景

現行基線（計畫 §1-septies、§10）：每位消費者一個 EOA，金鑰由隔離的 Signer 保管；Signer 只在同時拿到一次性 `AuthorizationGrant` 與鏈上已驗的 `intent_hash` 時，才簽 EIP-3009 `transferWithAuthorization`；task 預算由自建 ZK 合約在另一筆交易扣減。

研究問題（計畫 §3.5）是「受限 Agent 繞過 Go 入口後，鏈上是否仍阻止越權付款」。這個問題只在**有人持有受限付款權限**時才成立，所以先把威脅分開。

## 威脅模型

| 代號 | 威脅 | 攻擊者手上有什麼 |
|---|---|---|
| T1 | Agent 被控制 | Claude Code 與其輸出被 prompt injection 完全控制；能提出請購、呼叫受限工具。**沒有付款私鑰，也沒有鏈上權限** |
| T2 | 受限執行權限失守 | 某個「只能做有限付款」的權限被濫用或外流：`AuthorizationGrant`、ERC-7710 delegation 與被允許的執行身份、7702 delegate 接受的受限簽章金鑰、ERC-8196 policy 的 agent 金鑰 |
| T3 | Signer／主私鑰失守 | 持有消費者 EOA（或帳戶 owner）金鑰的元件被攻破 |

各選項在三種威脅下的防線：

| | A 基線 | B1 7702＋7710 | B2 7702＋EIP-3009 經 ERC-1271 | C ERC-8196 |
|---|---|---|---|---|
| T1 | Agent 沒有金鑰，繞過 Go 也無法付款；風險是誘導系統產生看似合法的請購，由 Policy、grant、人類核准與 `intent_hash` 綁定處理 | 同左，前提是不把 delegation 或執行身份交給 Agent | 同左 | 同左；若把 policy 的 agent 金鑰交給 Agent，T1 會升級成 T2 |
| T2 | grant 在鏈下，一次性、綁 `intent_hash`、audience 為 Signer；外流只能讓 Signer 簽那一筆。限制由 Signer 執行，**鏈上不設限** | 由 Delegation Manager 與 enforcers 在鏈上限制【推論，待 S4-B 實測】 | 由 delegate 的 `isValidSignature` 驗證邏輯限制；需自寫或採用現成 validator【推論】 | 由模組限制 native value 與目標合約；ERC-20 收款人與金額需自寫檢查【官方＋推論】 |
| T3 | 該消費者的資金全部暴露（跨消費者由錢包隔離限制） | 同左。7702 不保護失守的主私鑰：owner 仍可直接送交易、改或清除委派【官方】 | 同左 | 同左；owner 可改寫或撤銷 policy |

T3 在所有選項下都沒有鏈上防線。B 類選項的潛在好處是：每筆付款改由受限權限執行，主私鑰只用於設定與撤銷，可以更少上線（【推論】，取決於 delegation 的簽發方式，S4-B 要記錄實際用鑰頻率）。

## 選項

選項可以組合，不是互斥的多選一。

### A. 保留基線：EOA＋EIP-3009

- **現有元件**：USDC 的 EIP-3009、x402 官方 SDK 的 `eip3009` 路徑、x402.org facilitator。
- **自寫範圍**：Signer、grant、ZK 預算合約（已在計畫內）。
- **信任假設**：Signer 與 host 正確；鏈上只保證簽章、nonce 與時窗。
- **取捨**：官方支援最完整，計畫已排；T2 的限制全在鏈下，主私鑰每筆付款都要使用。

### B. 7702 委派帳戶

消費者 EOA 以 EIP-7702（Final）委派給 delegate 合約，地址不變。付款有兩條子路徑。

**B1：ERC-7710 delegation＋caveat enforcers，x402 `assetTransferMethod=erc7710`**

- **現有元件**：MetaMask Delegation Framework v1.3.0 的 EIP-7702 Stateless DeleGator、Delegation Manager 與 enforcers（部署文件列有 Base Sepolia）；x402 exact 規格的 `erc7710`；MetaMask smart-accounts-kit 的 `x402Erc7710Client`。
- **自寫範圍**：enforcer 組合設定。收款人要另用 AllowedCalldata 或 ExactCalldata 限制，因為 `ERC20TransferAmountEnforcer` 只檢查代幣、`transfer` 方法與累計金額。若要綁 `intent_hash` 或 ZK 預算，需要自訂 enforcer。
- **缺口**：x402 官方 SDK 在 `6b6ee91` 的 ts／go／python exact 機制中未見 `erc7710`；facilitator 支援未查。依 S4-B 卡，不自動自建 facilitator：若無支援，記錄缺口並由 Aaron 決定。
- **信任假設**：Delegation Manager 與 enforcers 正確；委派初始化沒被搶跑；`RedeemerEnforcer` 自述具 delegation 功能的帳戶可再委派繞過，須另外防範。

**B2：一般 EIP-3009／Permit2 路徑，由 delegate 的 ERC-1271 驗簽限制**

- **現有元件**：x402 官方 repo 在 `6b6ee91` 有 ERC-7702 client 範例；facilitator 對有 code 的地址改用 strict EIP-1271 `isValidSignature`，原始碼註解說明已在 Base Sepolia 實際確認。
- **自寫範圍**：能表達本案限制的 validator。`isValidSignature` 只收到 digest，要檢查收款人與金額，簽章必須夾帶 typed-data 原文（例如巢狀 typed-data 格式），validator 據此解析【推論】。
- **取捨**：不需要新的 x402 路徑，官方 SDK 已支援路由；但限制邏輯落在自寫或第三方 validator，稽核負擔比 B1 重。

### C. ERC-8196 政策模組

- **標準**：Final。第 58 行寫實作「預期是智慧合約，例如 ERC-4337 帳戶**或專用的政策執行模組**」，所以不必建完整 AA 系統，可以做成 7702 delegate 上或獨立的模組。
- **現有元件**：標準介面；ERC-8004 在 Base Sepolia 有 registry 部署；標準本身沒有參考實作，查到的社群實作自述未稽核。
- **自寫範圍**：模組本體；ERC-20 calldata 的收款人與金額檢查（標準的限額作用於 wei 計的 `value`）；ERC-8126 provider 串接或 fixture；8004 註冊；x402 整合。
- **x402 整合**：x402 exact 規格沒有呼叫 `executeAction` 的路徑，整合需要自訂 adapter 或 facilitator 擴充。這是成本，不代表無法整合。
- **待定語意**：ERC-8126 的分數時效、查詢失敗與撤銷沒有定義，本案須自訂 fail-closed 規則；最底層 ERC-8004 仍是 Draft。

## 暫定研究排序（依目前證據與成本，待核准）

這是研究順序，不是採用決定。

1. **A 維持基線**，也是正式付款模組的預設路徑，直到 S4-B 有證據。
2. **S4-B 先做 B1**：受限權限由現成的 enforcers 在鏈上強制，最直接回答 T2（repo 附 `audits/` 資料夾，涵蓋哪些版本與合約未查）。第一步先查 facilitator 對 `erc7710` 的支援；若沒有支援，暫停並回報。
3. **B2 為第二順位**：若 B1 卡在 facilitator，B2 可沿用官方 SDK 已支援的路由，代價是自寫或採用 validator。
4. **C 排第三**：成本最高（模組、ERC-20 檢查、8126 provider、x402 adapter 都要做），且依賴的 8126 語意與 8004 狀態未定。可先借用兩個概念：policy hash 綁定與 `previousHash` 稽核鏈（B1-05 已計畫 hash chain）。若 Aaron 要求 8196 相容，另開「8196 介面 profile」研究。
5. **ERC-8126／8004 不接入 M1 或 M2 的 Policy。** 若之後需要鏈上 agent 身份，只在可信 registry 保留可選的 8004 對應。

## S4-B 驗收設計（以 B1 為例，B2 比照）

前置：Aaron 另行核准與 testnet opt-in；固定版本或 commit（DeleGator、Delegation Manager、enforcers、x402 client、facilitator、USDC 合約）；A、B、M 為專用測試帳戶；每個案例記錄 tx hash 或 revert reason。

受測對象固定為**一份有效受限權限 P_A**（A 委派；限制代幣為 USDC、方法為 `transfer`、收款人為 M、金額上限、時窗、使用次數），以及**一個合法執行身份 E**（被允許 redeem 的地址）。

1. **正向對照**：用 P_A 與 E，經真實 x402 verify→settle 付款成功；鏈上 Transfer 的資產、payer、payee、金額與報價一致。正向不成立，整輪判 INCONCLUSIVE，負向結果不採計。
2. **政策限制負向**：沿用**同一份 P_A 與同一個 E**，每案只改一項：錯資產、錯收款人、超額、效期開始前、效期結束後、重播（成功後原樣再送一次；超過使用次數）。低於上限的金額是否應通過，依所選 enforcer 事先寫定（ExactCalldata 會拒，TransferAmount 會放行）。每案必須 revert 在對應的 enforcer 並記錄 revert reason；因簽章錯誤或呼叫者不符而失敗，不算這一步通過。
3. **未授權呼叫者（另列）**：非 E 的地址拿同一份 P_A 去 redeem，必須被拒；另測 `RedeemerEnforcer` 自述的再委派繞過。這組結果不能代替第 2 步。
4. **跨消費者隔離**：P_A 不能動 B 的資金；B 的權限不能動 A 的資金。
5. **撤銷**：`disableDelegation` 後重跑第 1 步必須失敗。
6. **7702 帳戶生命週期**：初始化防搶跑、換 delegate 的 storage、owner 清除委派。這組記錄 T3 的邊界，不宣稱能保護主私鑰。

**判定**：1～6 都通過，但 ZK 預算與 `intent_hash` 綁定尚未整合時，只算**局部帳戶 PoC**，不解鎖完整付款架構的採用。採用還需要綁定方案的結果與 Aaron 核准。

## 未解問題

1. facilitator（x402.org 或其他）是否支援 `erc7710`；不支援時是否值得自建，由 Aaron 決定。
2. enforcer 能否表達「單筆金額等於報價、收款人等於 M」，同時不在鏈上公開 task 額度（period cap 是明文）。
3. B2 的 validator 要自寫還是採用現成方案；現成方案的稽核範圍未查。
4. `intent_hash` 是 SHA-256 的 256 bits，超過 BN254 scalar field，作 Groth16 public input 需拆 limb 或改用 field hash（交 S3 與 v2 契約）。
5. EIP-3009 與 ERC-7710 都還是 Draft；USDC 已實作 3009 是事實，7710 靠 MetaMask 實作。

## 後果

- **維持 A**：現有計畫不變；README 與面試說法不得宣稱鏈上錢包政策保障。
- **採用 B1 或 B2**：正式付款派工前，同步 PRD（FR-4／7／11／13–15／19／24）、契約 v2、Signer 職責、狀態機、驗收與圖表，並重估工時（PRD §12）。
- **不論選哪個**：契約 v1 的 fixture digest 不得作為任何鏈上、EIP-712 或 ZK 值；正式路徑用 v2 契約與新的 golden vectors。

## 證據等級

事實來自官方文件與固定 commit 的原始碼（2026-09-30 查核，見相容性盤點第 1 節），以及本 repo 的契約探測測試；沒有鏈上實測。每條陳述的來源與標記見[相容性盤點](../spikes/payment-policy-compatibility.md)。
