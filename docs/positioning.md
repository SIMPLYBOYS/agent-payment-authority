# 定位

> 2026-11-03，Claude Code。給面試時被問「這是什麼、給誰看、跟誰比」準備。來源：Aaron 的 FUTUREMODE 筆記（`aaron-vault/research/FUTUREMODE-2026-x402-AI-Agent-支付架構-國泰演講筆記.md`，§0.2 的 9/4 討論與 9/24 以一手文件校正的競品）與講者 York（國泰）的演講；Aaron 11/3「先做 1～3」。
> **Aaron 11/3**：「本專案只專注做場域的模擬呈現，讓商業決策者體驗到 web3 結合 AI Agent 的未來支付場景」。所以這個專案**不提自己的商業模式**；沒有任何市場數字，不報 TAM、不報營收、不說有客戶。

## 1 一句話

> **x402 處理報價、付款與結算；這一層證明這筆錢是為了哪個任務、依哪份委託、經過哪條規則與哪一次本人核准才付出去的。**

*x402 moves the money. This layer proves who allowed it, for what, and within which limits.*

## 2 問題：兩種講法，同一套機制

| | 一般人（影片） | 企業（講者、銀行） |
|---|---|---|
| 一句話 | 讓 AI 替我花錢，我最怕三件事：被騙著亂花、個資被拿走、出了事說不清楚 | Agent 是第一種在付款那一刻沒有人在場的支出；額度、雙簽、職務分離、稽核都是繞著「人」蓋的 |
| 誰設上限 | 家長替自己與孩子設預算、自動放行上限 | 部門預算、單筆授權額度 |
| 超過上限 | 孩子的大額購買由家長在裝置上核准 | 部屬的支出由主管核准（職務分離） |
| 對外揭露 | 每間店只看到它要的屬性（國家、是否滿 18 歲） | 交易對手只拿到必要資料 |
| 事後 | 任何人都能拿證據向鏈查證 | 稽核按請購單回放，Deny 也在 |

**為什麼是現在**：x402 這類協議讓 Agent 不用註冊、不用訂閱就能按次付款，每一次呼叫都可能是一筆**不可逆**的支出。講者的說法是「支付工具把模型輸出轉換成不可逆的外部行動」；以前是月底一張帳單，現在是每次呼叫都要先有人授權。

## 3 這個專案是什麼、給誰看

**一個場域模擬**：一家人在像素城鎮裡讓 AI 替自己買東西，商業決策者在幾分鐘內**親身看到** web3 結合 AI Agent 的付款會長什麼樣子：

- 付款是真的：真實的 Claude Code Agent、Base Sepolia 上的兩筆交易（ZK 預算預留、EIP-3009 結算）、x402 的報價與收據。
- 安全是看得到的：誰提出、誰核准、誰簽名，在畫面上是不同的角色；被擋下的那一筆，錢沒有動。
- 證據是查得到的：看完可以當場用 checker 向鏈重算。

**它不是產品，也不賣東西。** 要不要做、誰來做、怎麼收費，是看的人的本業，由他們判斷。被問到時，可以提幾個看的人可能想到的方向，但不當成這個專案的主張：

| 看的人可能想到的 | 例如 |
|---|---|
| 銀行、錢包業者 | 帳戶底下開「Agent 子授權」：家庭帳戶（家長與孩子）與企業金庫（主管與部屬）共用同一套 |
| 企業財務 | 每個 Agent 按任務與商家設限、審批、收據對帳 |
| Agent 平台 | 讓自己的 Agent 能付款，又不必持有使用者的錢 |

講者的一句話可以借用：**付款的那一刻沒有人在場**；以前的內控都是繞著「人」蓋的，這個場域讓人看到人拿掉之後，授權與證據還掛得住。

## 4 放在哪一層，跟誰比

已經有人做了其中幾塊（9/24 以一手文件查證，連結在第 8 節）：

| 誰 | 已經做到 | 這一層另外補上的 |
|---|---|---|
| x402 協議 | 報價、付款、結算的 HTTP 協商；明說不管錢包託管、預算、Agent policy | 這筆錢為什麼可以付：授權與證據 |
| Coinbase CDP x402 client | 單筆與滾動時間窗的累計上限；資產、鏈、收款人白名單；付款前預留 | 任務脈絡（哪個任務、哪一步）與本人同意的證據 |
| Cloudflare Agents SDK | x402 付款的人工核准回呼 | 核准綁在 Signer 要簽的確切條款上；事後獨立查證 |
| Crossmint | 支出上限、商家白名單、門檻式人工核准、稽核 | 跨多筆付款的私密預算；只讀證據與鏈的獨立查證 |
| Google AP2 | 委託（mandate）串起意圖到付款的證據。v0.2 改成 Checkout／Payment 兩種，各分 open（事前的範圍）與 closed（確切的一筆）；2026/4 交給 FIDO 標準化（欄位對照見 §4.1） | 跟 x402 結算、獨立 signer、ZK 預算綁在同一個 `intent_hash` |
| Mastercard Verifiable Intent（Draft v0.1） | 和 Google 共同開發、建在 AP2 的委託上：發卡機構、本人、Agent 三層 SD-JWT 委託鏈；限制有收款方、金額範圍、總額、週期等；2026/4 交給 FIDO（§4.1） | 私密的預算（它的累計由支付網路追蹤）；Agent 不持任何金鑰；事後只讀證據與鏈的獨立查證 |
| ERC-8196（Final） | 把 owner policy 下沉到錢包層執行 | 以 USDC 與店家計的限制（8196 的限額以 wei 計）；本人核准與任務脈絡 |
| ERC-8366（Draft） | **用 Groth16 證明付款符合私密的支出政策**，經 ERC-3009 接進 x402 | 同一個任務的多筆付款共用一份預算（8366 是一份 policy 對一筆付款） |
| x402 Signed Offers & Receipts | 商家以 DID 簽報價與交付收據 | 人的身分與授權（那份 DID 是商家的） |

這個專案把它們缺的那幾塊**綁成同一筆可查證的授權**：

- **任務綁定**：任務、請購、本人授權、預算、付款、交付共用一個 `intent_hash`，連商家報價的完整內容也算進雜湊（`payment_requirement_hash`）。
- **本人核准**：本人在自己的裝置上確認委託；超過上限的單筆，裝置上顯示的是 Signer 綁定的摘要，不是模型的說法。
- **獨立簽名**：Signer 在另一個系統帳號，先到鏈上確認預算已經預留才簽。
- **跨多筆付款的私密預算**：同一個任務的多筆付款共用一份 ZK 預算（每筆一個 nullifier）；鏈上只看得到承諾值，看不到預算上限與剩餘（中繼與主持人那一側看得到，見限制）。
- **最少揭露**：BBS+ 憑證，每間店只看到它要的屬性。
- **獨立查證**：checker 只讀證據包與鏈，判定 E1～E10。

**互補，不是取代**：這一層可以疊在 CDP 這類錢包上面、接 AP2 的 mandate，或在商家端換成任何 x402 facilitator；Agent、Policy、本人核准、ZK 預算與 Signer 都不必改。

### 4.1 和 AP2 v0.2、Verifiable Intent v0.1 的欄位對照（11/17）

「Agent 代表誰、被授權做什麼」這一層還沒有定案的標準：FIDO 2026/4/28 成立 Agentic Authentication 與 Payments 兩個工作組，AP2 v0.2 與 Mastercard 的 Verifiable Intent 都交給它審。這個專案的委託與請購是自己的格式（`apa.pay.v2`），下表對照兩份草案的欄位，說明「設計對齊、格式不同」。依據是 11/17 讀的規格頁（第 8 節）；兩份都是草案，欄位名稱可能再改。兩份共用 AP2 的委託名稱（`mandate.checkout.*`、`mandate.payment.*`），Verifiable Intent 在上面多了發卡機構那一層與 Agent 簽的那一層。

| 概念 | 本專案 | AP2 v0.2 | Verifiable Intent v0.1 |
|---|---|---|---|
| 誰的錢、用什麼付 | 客人自己的錢包（`wallet_id`、`from`；私鑰在 Signer），資產是 `chain_id`＋`asset_contract` | Payment Mandate 的 `payment_instrument`；open 階段可限 `payment.allowed_payment_instruments` | L1：發卡機構簽的卡憑證（`pan_last_four`、`scheme`，`cnf.jwk` 是本人的公鑰）；L2／L3 的 `payment_instrument` |
| 本人事前給 Agent 的範圍 | 委託條款 `DelegationTerms`，本人用已登記的裝置確認（WebAuthn），Signer 保存它的雜湊 | open mandate（`mandate.checkout.open.1`、`mandate.payment.open.1`），本人在受信任的畫面上核准 | L2 Autonomous：本人簽的 open mandate |
| 可以付給誰 | `merchants`（收款地址名單） | `payment.allowed_payees`、`checkout.allowed_merchants` | `mandate.payment.allowed_payees`、`mandate.checkout.allowed_merchants` |
| 單筆金額 | `allow_max`：以下自動放行（Allow），以上逐筆交本人核准（Ask）；**是核准門檻，不是硬上限** | `payment.amount_range`（硬上限） | `mandate.payment.amount_range`（硬上限） |
| 總額 | `task_budget`：ZK 預算，付款前在鏈上預留；鏈上看不到上限與餘額 | `payment.budget` | `mandate.payment.budget`：由支付網路追蹤已授權的累計 |
| 次數與週期 | 頻率限制（同一位客人十分鐘內最多 5 筆，Policy 與 Signer 各一道）；捷運通行（每趟 0.02、總額 0.20） | `payment.agent_recurrence` | `mandate.payment.agent_recurrence`（`frequency`、`max_occurrences`、起訖日）、`mandate.payment.recurrence` |
| 買什麼 | `purpose`：一句自然語言，不限定商品 | `checkout.line_items`（可接受的商品 ID 與數量） | `mandate.checkout.line_items`；選填的 `prompt_summary` |
| 期限 | `valid_seconds`（確認後多久內可以開始任務）；Ask 的等待時間 `approval_deadline_seconds` | `exp` | `exp` |
| Agent 的身分與金鑰 | `agent_id`＋ERC-8004 身分（付款錢包由錢包簽章證明）；**Agent 沒有任何簽章金鑰** | open mandate 的 `cnf` 綁 Agent 的金鑰，Agent 用它簽 closed mandate | L2 的 `cnf.jwk` 是 Agent 的公鑰，L3 由 Agent 簽 |
| 這一筆的確切條款 | `PurchaseOrder`：`intent`（任務、請購、資源、x402 版本、報價雜湊）＋`payment_binding`（收款人、金額、時間窗、nonce）＋`quote`；Agent 提出但不簽，`intent_hash` 綁住全部 | closed mandate：`mandate.checkout.1`（`checkout_jwt`、`checkout_hash`）＋`mandate.payment.1`（`payee`、`payment_amount`、`payment_instrument`、`transaction_id`） | L3a（給支付網路）、L3b（給商家），Agent 簽 |
| 商家的報價 | x402 `PAYMENT-REQUIRED` 選中的那一項，完整內容算進 `payment_requirement_hash`；付款商店以 x402 Signed Offers & Receipts 簽報價與收據（11/25） | `checkout_jwt`：商家簽的購物車 | 同 AP2 |
| 超出範圍時本人逐筆核准 | `AuthorizationGrant`：Signer 驗過本人裝置的 WebAuthn 簽章（`amr: ["webauthn"]`，可加語音）才發，綁 `intent_hash`，用一次（`jti`） | 本人在場（human-present）時，本人確認 closed mandate | Immediate 模式：本人直接簽 closed mandate（確切的金額與收款人），沒有 L3 |
| 本人的金鑰 | passkey（WebAuthn） | 可驗證的數位憑證（OpenID4VP，建議 Digital Credentials API），或 Agent 提供者的受信任畫面；passkey 只列為「之後可能」 | L1 `cnf.jwk` 綁的本人金鑰 |
| 防重放 | EIP-3009 nonce、預算 nullifier、核准的 `jti` | key binding 的 `nonce`、`sd_hash` | L2、L3 各自的 `nonce`；L3 的 `exp` 建議 5 分鐘 |
| 最少揭露 | BBS+ 憑證：店家只看到要求的屬性（國家、是否滿 18 歲），憑證這一層不同店家之間無法串連；付款錢包地址每間店都看得到，付款層仍可串連 | SD-JWT 的 disclosure | SD-JWT；`allowed`、`line_items` 的個別項目可以選擇揭露 |
| 結果 | x402 `PAYMENT-RESPONSE`＋鏈上交易＋事件雜湊鏈＋證據包，checker 只讀證據與鏈重算 | Mandate Receipt（`iss`、`result`、`reference`） | 規格頁沒寫 |
| 格式 | JSON 加上分領域的雜湊（`apa.pay.v2`）；憑證是自訂信封的舊版 BBS+ | SD-JWT（`vct`：`mandate.*`） | SD-JWT 委託鏈（L1 `sd+jwt`；L2、L3 `kb-sd-jwt`） |

讀表的重點：

- **模式對得上**：本專案的 Allow（`allow_max` 以下）就是它們的 autonomous；Ask（本人逐筆核准）就是它們的 human-present／Immediate。差別在本專案用同一份委託依金額切換，它們用兩種委託。
- **最大的結構差異在 Agent 有沒有金鑰**：它們給 Agent 一把金鑰，由 Agent 簽這一筆；本專案的 Agent 什麼金鑰都沒有，請購不簽名，由 Policy 檢查、Signer 簽付款。後者更貼近「Agent 不能碰 custody」；代價是「這筆請購是哪個 Agent 送出的」沒有密碼學證明，靠的是 Host 的紀錄與 `agent_id`。
- **商家簽報價與收據**：它們要商家簽購物車；本專案綁住報價的完整雜湊，11/25 起付款商店也用 x402 的 Signed Offers & Receipts 簽每一筆報價與交付收據，金鑰公布在商店的 did:web 文件（`docs/commercial-offer-receipt.md`）。簽的是單一資源的報價，不是購物車。
- **預算看得到的人不同**：它們的累計由支付網路追蹤；本專案在鏈上以 ZK 預留，鏈上看不到上限與餘額（中繼與主持人那一側看得到，見 architecture.md 的限制）。
- **本專案沒有商品層級的限制**：它們有 `line_items`；本專案只有自然語言的 `purpose`，靠商店清單與憑證屬性要求來限制。
- **要互通的話**：委託條款可以包成 open mandate、請購包成 closed mandate；本人逐筆核准（Signer 驗過 WebAuthn 才發的 grant）在 AP2 裡沒有現成的位置，AP2 只把 passkey 列為之後可能的做法。

## 5 能說與不能說

| ⛔ 不能說 | ✅ 改說 |
|---|---|
| 第一個把 ZK 帶進 x402／Agent 支付 | 在 x402 付款流程上，把任務範圍的憑證、跨多筆付款的私密 ZK 預算、本人核准與獨立 signer 整合成一條可查證的授權鏈（ERC-8366 已有單筆的 ZK 支出政策） |
| 競品都只有單筆額度 | 錢包已經能管「花多少、付給誰」；這一層證明「為了哪個任務、誰同意、店家看到多少」 |
| 符合 x402／通過 x402 認證 | 採用 x402 v2 的 HTTP 支付協商與 EIP-3009 測試鏈結算，在協議外實作授權層 |
| 完整的金鑰分層（金庫、資金錢包、session key） | Agent 沒有私鑰；簽名在獨立的 Signer，只簽本人確認過的委託範圍，委託有總額與期限 |
| 安全、不會被騙 | 不假設 AI 不會被騙；就算被騙，也花不出委託範圍外的錢 |

其他照實說的限制：[architecture.md](architecture.md#照實說的限制)。威脅對照 OWASP Agentic Top 10：[architecture.md](architecture.md#owasp-agentic-top-10-對照1118)。

### 5.1 x402 v2 的五條邊界（11/18）

FUTUREMODE 筆記在 9/23 審查時列出五條邊界：做到才能說「通過本專案定義的 x402 v2 相容性測試」。11/18 逐條對照程式與測試：

| 邊界 | 程式 | 測試 | 狀態 |
|---|---|---|---|
| 1 報價完整綁定：`AuthorizationIntent` 帶 HTTP method、完整網址、`x402Version`、scheme，以及選中那份報價的雜湊 `payment_requirement_hash` | `internal/toolbridge/shop.go` 算雜湊；`PurchaseOrder.Validate` 重算，對不上就拒絕 | `internal/contracts/contracts_test.go`：改任何一個欄位，雜湊都會變；`Test402Tampering`：網址、版本、多份報價、轉址都拒絕 | 做到 |
| 2 `accepted` 必須完全相同：付款裡的 `accepted` 要和商店給的那份一模一樣 | `internal/merchant/merchant.go` 逐位元比對；`apa-facilitator` 轉成標準格式後比對 | `TestPaymentSignatureNeverDelivers`：金額、收款人、scheme、網路、只差大小寫的鍵都拒絕；facilitator 的 `accepted not the requirements` | 做到 |
| 3 驗證與結算分開：`/verify` 唯讀；驗證失敗就不結算；確認到設定的區塊深度前不交付 | `internal/facilitator`、`internal/merchant` | `TestVerifyIsReadOnly`、`TestVerificationFailuresSendNothing`、`TestNothingDeliveredBeforeConfirmations`；checker E4 向鏈核對兩筆交易 | 做到 |
| 4 擴充欄位放對位置：憑證的要求放在 `PAYMENT-REQUIRED` 的 `extensions`，證明放在 `PaymentPayload` 的 `extensions` | 要求：付款商店在 402 的 `extensions["apa-credential"]` 宣告，Host 只接受場景設定範圍內的。證明：付款時另外產生一份只揭露 schema、發行者與要求屬性的 BBS+ 證明，綁商店的 did:web、資源、報價與 EIP-3009 nonce，放進 `PaymentPayload` 的 `extensions`，商店自己驗過才結算（11/25，`docs/commercial-credential-ext.md`） | `TestVenueRequires`、`TestPresentToShop`、`TestCredentialPresentedToTheShop`、`TestV2PaidOnAnvil`；checker E2 從報價讀要求 | 做到。商店驗不到 holder 綁定，撤銷與期限仍由 Host 判斷 |
| 5 三種「預留」不混稱 | ZK 預算預留的事件一律叫 `BUDGET_RESERVED`；結算防重靠 EIP-3009 nonce；沒有用 CDP 的付款前預留 | — | 做到 |

所以能說的是「採用 x402 v2 的 HTTP 支付協商，本專案定義的五條邊界都做到並有測試」；不能說「符合 x402」或「通過認證」。

### 5.2 和 x402 v2 規格的其他差異（11/26）

對照 x402-foundation 的 `x402-specification-v2.md` 與 EVM `exact` 的 scheme 文件。

**規格的 MUST 規則**（11/26 修好，沒有已知違反的）：

| 規則 | 做法 | 測試 |
|---|---|---|
| 客戶端要在付款內容回傳 402 公告的擴充資訊，只能增加、不能刪改（§5.1.2） | Host 的付款內容帶上 402 的全部 `extensions`；憑證證明加進 `apa-credential` 原有的 `info` | `TestEcho`；`TestV2PaidOnAnvil` |
| 不認得的 `paymentFlow` 不得付款（§6.1） | 只付 `authorization`、`upfront` 或沒寫的（單筆、金額固定）；`escrow` 與不認得的拒絕；`assetTransferMethod` 只接受 `eip3009` 或沒寫，因為 Signer 只簽 EIP-3009 | `TestPayable`、`TestSignedOfferChecked` |
| 簽章報價可以是 EIP-712 或 JWS | 只讀 EIP-712；JWS 簽的報價當作沒簽，照常買，不拒絕整筆 | `TestOtherFormatsAreUnsigned` |

商店轉給 facilitator 時，刻意不帶客人的擴充資訊：facilitator（例如 x402.org）結算用不到憑證證明，帶過去等於把客人的屬性多給一個第三方。

**刻意從嚴、會擋掉一些合法商店的**：

- 402 只能有一個付款選項；提供多條鏈或多種資產的商店，Host 不買。之後可改成挑出符合自己鏈與資產的那一項。
- 402 最外層或 `resource` 有不認得的欄位就拒絕（防禦用，代價是對規格新增的欄位不寬容）。
- 只認 x402 v2；v1 的 `X-PAYMENT` 不支援。
- `offer-receipt` 的 EIP-712 型別字串照規格文字組，沒和官方 SDK 比對。

**規格有、本專案沒做的**：

- scheme 只做 `exact`；沒有 `upto`（先授權上限、依用量結算）、`batch-settlement`、`auth-capture`。
- 只有 EVM＋EIP-3009（USDC）；沒有 Permit2、ERC-7710、Solana 與其他鏈。
- 傳輸只有 HTTP；沒有 MCP、A2A。
- 擴充只做 `offer-receipt`；`apa-credential` 是本專案自訂的。沒有 `bazaar`（探索）、`sign-in-with-x`、`payment_identifier`、gas 代付等；facilitator 沒有 `/discovery` API。Agent 只能買固定清單上的商品，是刻意的安全選擇。

## 6 面試常見追問（短答）

- **跟 Coinbase CDP 有什麼不同？** CDP 管錢包能花多少、付給誰；這一層管為了哪個任務、誰同意、店家看到多少、事後能不能查。可以疊在 CDP 錢包上。
- **為什麼要 ZK？** 鏈上只看得到承諾值，看不到預算上限和剩餘，仍能確認沒有超支。這不是首創（ERC-8366）；差別在同一個任務的多筆付款共用一份私密預算。
- **AI 被騙了怎麼辦？** 誘導文字騙不倒影片裡的真實 Agent；就算騙倒了（模擬段），超出預算也會被 Policy 拒絕，錢沒有動。
- **還會擋哪些？** 商家不在名單（飯店那一幕：付款頁的收款人被換掉）、委託或授權過期、資產不對、短時間內請購太多次（同一位客人十分鐘內第 6 筆；Signer 另有一道）、重放用過的授權（EIP-3009 nonce、預算 nullifier、一次性核准），都在付款前被擋下，都有自動測試。
- **付了錢沒拿到東西怎麼辦？**（郵局那一幕）系統以鏈上的結果為準，記下「已付款、交付失敗、待人工處理」，不會再付一次。該不該退要有人判斷：主持人按「商店退款」，商店把全額退回客人的錢包，Host 自己向鏈上確認後才記下，checker 也查得到。鏈看不到交付，所以判斷留給人；商店不肯退的話系統沒辦法強制（沒有託管），爭議流程是下一步。
- **出事怎麼查？** 證據包加 checker：只讀證據與鏈，不呼叫系統本身。
- **這要賣給誰？** 這個專案不賣東西，是讓決策者親身體驗的場域模擬；銀行、企業財務、Agent 平台各自可能怎麼用，由看的人判斷（第 3 節）。
- **下一步？** 用 FUTUREMODE 講者列的三格來答（11/18）：
  - **信任**：Agent 身分與委託已經有（ERC-8004、BBS+ 憑證、本人確認的委託），但身分是展示平台代為註冊的；商家的身分與信譽還沒有。
  - **權利**：eKYC 只接了 Didit 沙盒（`docs/commercial-ekyc.md`）；退款只有全額一種，沒有爭議流程，也沒有託管；沒有交易監控（KYT）。
  - **營運**：只有一個 facilitator，沒有備援；金鑰只有一層，沒有金庫、資金錢包、session key 的分層；只做 `exact`，沒有按用量計費的 `upto`。
- **換成 MPP 還成立嗎？**（11/18）授權這一層成立，結算的接法要改。MPP 是 Stripe 與 Tempo 提出的 HTTP 402 協議：報價放在 `WWW-Authenticate`，付款證明放在 `Authorization`；它的 `charge` 對應 x402 的 `exact`，另外有 `session`（先存一筆，再按用量付）。Agent 只能提出、Policy 裁決、本人核准、Signer 獨立簽名、證據與 checker，都和用哪個協議無關。要改的有三處：
  - 請購單綁的是 x402 的報價雜湊，以及 `x402Version`、`scheme` 欄位，要改成綁 MPP 的報價。
  - Signer 現在只簽 EIP-3009，MPP 的每種付款方式（Tempo、Stripe 的卡）各有自己的證明格式。
  - ZK 預算合約在 Base Sepolia；付款若改在 Tempo 上，就跨了兩條鏈。

  這些都沒有實作，也沒有測過。

## 7 要 Aaron 決定

| # | 問題 | 建議 |
|---|---|---|
| ~~P1~~ | ~~主打哪個商業模式~~ | **Aaron 11/3 決定**：不提商業模式，專案只做場域的模擬呈現（第 3 節） |
| P2 | 影片結尾的定位句 | 第 1 節那一句（中文配音），字幕用英文版 |

## 8 來源

筆記 9/24 查證的一手文件：
[Coinbase CDP x402 FAQ](https://docs.cdp.coinbase.com/x402/support/faq)、
[Cloudflare Agents SDK x402](https://developers.cloudflare.com/agents/tools/payments/x402/pay-from-agents-sdk/)、
[Crossmint](https://www.crossmint.com/solutions/agentic-payments)、
[Google AP2](https://cloud.google.com/blog/products/ai-machine-learning/announcing-agents-to-payments-ap2-protocol)、
[x402 Signed Offers & Receipts](https://docs.x402.org/extensions/offer-receipt)、
[ERC-8366](https://ethereum-magicians.org/t/erc-8366-zero-knowledge-spending-policies/29281)、
[ERC-8196](https://eips.ethereum.org/EIPS/eip-8196)。
錢包層執行路徑的比較：[adr/payment-account-model.md](adr/payment-account-model.md)。

§4.1 的欄位對照，11/17 讀的規格頁：
[AP2 首頁（v0.2、交給 FIDO）](https://ap2-protocol.org/)、
[AP2 Agent Authorization](https://ap2-protocol.org/ap2/agent_authorization/)、
[AP2 Checkout Mandate](https://ap2-protocol.org/ap2/checkout_mandate/)、
[AP2 Payment Mandate](https://ap2-protocol.org/ap2/payment_mandate/)、
[Verifiable Intent](https://verifiableintent.dev)、
[Verifiable Intent Credential Format](https://verifiableintent.dev/spec/credential-format/)、
[Verifiable Intent Constraints](https://verifiableintent.dev/spec/constraints/)、
[FIDO 4/28 公告](https://fidoalliance.org/fido-alliance-to-develop-standards-for-trusted-ai-agent-interactions/)。

11/18 補的：
[OWASP Top 10 for Agentic Applications（2025-12-09）](https://genai.owasp.org/resource/owasp-top-10-for-agentic-applications-for-2026/)、
[MPP 協議](https://mpp.dev/protocol)、
[MPP 與 x402 的比較（MPP 官方）](https://mpp.dev/mpp-vs-x402)、
[Stripe：Introducing the Machine Payments Protocol](https://stripe.com/blog/machine-payments-protocol)。
