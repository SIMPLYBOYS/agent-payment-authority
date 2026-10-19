# P2 Host 接線：設計提案

> **狀態：Aaron 10/18 決定 H-D1～H-D8 全部照建議**（「8 件事基於我看過設計文件的前提，我們都先依照建議的方向執行」）。實作照 §11 分段進行。H-D6 的付款錢包金鑰由 Aaron 自己建立，要等到 H6 才需要；在那之前我不建立、不讀取任何金鑰，也不連任何鏈或外部服務、不動 `/opt`。

P2-a 已經有各自完成並 review 過的元件（`docs/commercial-p2.md` §9）：
- 付款 Signer 的核准消耗與累計上限；
- 預算 relayer（tx#1）與 Signer 的鏈上預留核對；
- 商店自行 verify／settle 與交付把關（tx#2）；
- Reconciler。

但**正式流程裡沒有任何地方呼叫它們**。這份提案說明 Host 怎麼把它們串起來，從「Policy 允許」或「本人核准」一路走到「已付款、已交付、有收據」；也說明出錯、重啟、結果不明時怎麼收斂，以及要改哪些東西。

## 1 要做到什麼

- **Allow**（委派範圍內）與 **Ask 經本人核准**後，Host 自動完成付款，依序：
  1. 預留（tx#1，`BUDGET_RESERVED`）；
  2. Signer 簽 EIP-3009；
  3. 交給商店（`PAYMENT_SUBMITTED`）；
  4. 確認後結算（`PAYMENT_SETTLED`）；
  5. 交付（`RESOURCE_DELIVERED`）；
  6. 開收據（`RECEIPT_ISSUED`）。
- `payment_status` 由付款狀態推導，不再寫死 `not_enabled`。網頁的卡片與交付區照實顯示（UX2-D4：交付區只在交付事件後才演出）。
- **絕不自動再付一次**（交易流程決策 5）。結果不明一律交給 Reconciler 收斂：`settled`、`not_settled_expired`，或 `needs_review`。已預留未付款轉 `needs_review`，預算不退回（決策 6）。
- §7 的必驗反例中還沒證明的，在本機 anvil 上端到端證明：
  - 預留未完成或對錯 intent；
  - grant 範圍、期限、nonce 不符；
  - request ID 重送；
  - 結算逾時被當成失敗而再付款；
  - 跨 consumer 冒用；
  - A／B 並行共用 commitment。
- AE1 的「未授權新增支出 = 0」：Host 保存每一份簽出的授權，交給 `reconcile.Unauthorized` 對帳，結果寫進報告，取代現在的 `not_evaluated`。

## 2 現況（盤點，HEAD `d1d5c23`）

| 部分 | 現況 |
|---|---|
| 請購 | `request_purchase` 立即回傳裁決；`projection` 固定寫 `payment:"none"` 與「未啟用付款」（`internal/toolbridge/shop.go:193-207`）。Allow 之後什麼都不做；Ask 的 run 不等待，核准在 run 結束後仍可發生 |
| 本人核准 | `ResolveWebAuthn` 寫入 `grants`（`authorization_method=webauthn`、`signer_approval_id`、`expires_at = min(Signer 核准到期, valid_before)`），並發出 `APPROVED`、`GRANT_ISSUED`；之後什麼都不做 |
| 付款紀錄 | `payment_attempts`（P1-b）只有 `delegation`、只到 `signed`；`BeginPayment` 拒絕 webauthn；`signerclient.Pay` 不送 `reservation` 也不送 `approval_id`，而且不保存簽出的授權 |
| Store | schema 6；沒有 reserved／submitted／settled／delivered 狀態，也沒有收據、交付表；事件沒有付款類 |
| 預算 | `tasks.budget_remaining` 在 task 開始時設為委派的 `task_budget`（場景是 5,000,000），之後從不扣減；registry 沒有預算欄位 |
| demo | 同一個程序裡跑 M1 商店（`merchant.New`，從不交付）；只連 `serve-approvals`；看到 Signer health 是 `approvals-only` 才顯示核准可用 |
| Signer | `apa-signer serve` 用 `Unavailable`（什麼都不簽）；同一個 socket 已能同時提供核准與付款 |
| 時間窗 | 請購的 `valid_before = 送出當下 + 報價的 maxTimeoutSeconds（60 秒）`，intent 與核准都綁著它；商店不收超過 `maxTimeoutSeconds + 5 秒` 的授權 |
| UI | 網頁有 9 個階段＋pay 系列 3 個，沒有付款中、已付款、已交付；Unity 同樣只有 9 個；交付區標示「付款後才交付」 |

## 3 流程

```
request_purchase ─→ Store：POLICY_DECIDED
   Allow ─────────────────────────────┐
   Ask ─→ 本人核准 ─→ GRANT_ISSUED ────┤
                                      ▼
                  付款工作（Host，一次一筆，決策 7）
  ① 建 payments 列（intent_hash 為鍵，requested）  PAYMENT_REQUESTED
  ② relayer.Reserve(order)      tx#1 → reserved   BUDGET_RESERVED
  ③ signer /v1/sign {授權, order, reservation}
                                     → signed     PAYMENT_SIGNED（保存授權）
  ④ GET 資源＋PAYMENT-SIGNATURE   → submitted  PAYMENT_SUBMITTED
        200 → settled＋delivered     PAYMENT_SETTLED、RESOURCE_DELIVERED、RECEIPT_ISSUED
        202 → 以同一份 PAYMENT-SIGNATURE 重問，直到 200／402 或期限過去
        402 → 不重付；交給對帳
  ⑤ 結果不明（逾時、重啟、402 但可能已上鏈）→ unknown → Reconciler.Status
        settled → 用同一份簽章重取內容 → delivered
        not_settled_expired → needs_review（tx#1 已花，決策 6）
        needs_review → 人工
```

- **授權方式**：
  - Allow 用委派（`method=delegation`，`delegation_id`／版本）。
  - Ask 用本人核准（`method=webauthn`，`approval_id = grants.signer_approval_id`）。Signer 在接受付款的交易裡消耗核准（§9.4），也套用 approver 的單筆與累計上限（§9.5）。
- **冪等**：
  - `payments` 以 `intent_hash` 為唯一鍵；每一步都先寫狀態再呼叫外部。
  - Signer 的 request ID 由 `intent_hash` 決定。relayer 與商店本身也各自冪等（§9.6、§9.10）。
  - 因此重啟或重送都只會接著做同一筆，絕不會出現第二筆。
- **時間**：
  - 第 ② 到 ④ 步必須在 `valid_before` 之前完成，Ask 還要在 grant 到期之前完成。
  - 過了期限還沒送出：如果已經預留，轉 `needs_review`；如果還沒預留，轉 `not_paid_expired`，沒有花任何錢。
- **商店回 402 時**：
  - 驗證失敗不會送出任何東西，但 tx#1 已經花了，所以轉 `needs_review`。
  - `settlement_failed` 可能是被搶先送出（§9.10），先交給 Reconciler 判定，不能直接當成失敗。

## 4 Store 變更（schema 7）

- **新表 `payments`**，一張請購最多一列：
  - 鍵：`intent_hash` UNIQUE，以及 (`task_id`, `purchase_order_id`) UNIQUE；
  - 欄位：`method`、`grant_ref`（委派或 `signer_approval_id`）、`state`；
  - 預留證據：tx hash 與區塊；
  - Signer：attempt ID 與簽出的授權（from、to、value、validAfter、validBefore、nonce、簽章），用來對帳；
  - 商店回應：`PAYMENT-RESPONSE`、HTTP 狀態；
  - 對帳結果：狀態、交易、區塊；
  - 時間與錯誤碼。
- **狀態**：
  - 主線：`requested → reserved → signed → submitted → settled → delivered`
  - 分支：`reserve_failed`（沒花錢）、`not_paid_expired`（沒花錢）、`unknown`、`not_settled_expired`、`needs_review`、`delivery_failed`（已付款、交付失敗：決策 6 的「已付款，交付失敗」）
- **新表 `receipts`**，一筆付款最多一張：
  - `intent_hash`、`PAYMENT-RESPONSE`、tx#1 與 tx#2 的 hash；
  - 交付內容的 SHA-256 與大小、交付時間。
  - 只保存內容的 hash，不保存原文。
- **新事件**，各自以 (`task_id`, `purchase_order_id`) 唯一：
  - `PAYMENT_REQUESTED`（H1 review 後加入）、`BUDGET_RESERVED`、`PAYMENT_SIGNED`、`PAYMENT_SUBMITTED`
  - `PAYMENT_SETTLED`、`PAYMENT_NOT_SETTLED`（附原因）、`PAYMENT_NEEDS_REVIEW`
  - `RESOURCE_DELIVERED`、`RECEIPT_ISSUED`
- **其他**：
  - `tasks.budget_remaining` 在 `BUDGET_RESERVED` 時扣減；
  - `payment_attempts` 保留給 P1-b 的 Signer 握手，`BeginPayment` 改為接受 webauthn（帶 `grant` 的 `signer_approval_id`）。
- **升級**：沿用 `--upgrade-db` 的機制（先備份、再驗證）。依「demo 結束前不清理」的決定，既有的 `var/` 資料庫**不升級**，P2 的 demo 用新的資料庫（H-D7）。

## 5 `payment_status` 與畫面

| 付款狀態 | `payment_status` | 卡片文字 | 交付區 |
|---|---|---|---|
| （未決定，或 Deny） | `not_started` | 同現在 | 空 |
| `requested`、`reserved`、`signed` | `paying` | 付款中：預算已預留／已簽名 | 空 |
| `submitted`、`unknown` | `confirming` | 已送出，等待鏈上確認（不會自動再付） | 空 |
| `settled` | `paid` | 已付款（區塊 N） | 準備交付 |
| `delivered` | `paid` | 已付款、已交付，附收據 | 演出交付 |
| `delivery_failed` | `paid` | 已付款，交付失敗，待人工處理 | 標示失敗 |
| `reserve_failed`、`not_paid_expired` | `not_paid` | 未付款（原因） | 空 |
| `not_settled_expired`、`needs_review` | `needs_review` | 預算已預留但未付款，待人工處理；兩筆交易不是同一筆（決策 6） | 空 |

`paycontract.PaymentNotEnabled` 只留給 M1 與沒有接付款元件的設定（例如 AE1 的 M1 任務、`--approvals-signer` 模式），照舊。

## 6 元件與程序（P2-a，本機）

- **anvil**：本機鏈。部署 Z2 verifier、預算合約與 TestUSDC，並為兩位客人的錢包鑄造 TestUSDC。由一支新的設定腳本完成（類似 `v1-dev.sh`），寫出 registry、`signer.json`，以及 relayer 與商店的設定。
- **付款 Signer**：`apa-signer serve`，獨立程序，有自己的 keystore。`signer.json` 新增 `reservation` 段（RPC、預算合約、確認數），有這段時改用 `budget.Verifier`，沒有就照舊 `Unavailable`。health 改為回報付款與核准是否都可用，畫面據此顯示。
- **預算 relayer**：見 H-D5。
- **商店**：`merchant.NewSettled`，在 demo 程序裡跑，gas 金鑰用 anvil 的公開測試金鑰。
- **Reconciler**：在 demo 程序裡跑，啟動時、以及每次付款結果不明時呼叫。

## 7 恢復與對帳

- **啟動時**，依各付款狀態處理：
  - `requested`：預留前就中斷了，可以從頭再走一次（relayer 本身冪等）；
  - `reserved`、`signed`：如果還沒過期，就繼續下一步；
  - `submitted`：一律改成 `unknown`，再交給 Reconciler（決策 5）；
  - `settled` 但未交付：用保存的簽章再向商店取一次內容。
- **付款工作在呼叫 relayer 前後的兩條規則**（H1 第二輪 review）：
  - 第一次呼叫 relayer 之前，先檢查 task 是否已結束（`paycontract.Ended`）。已結束就以 `TASK_ENDED` 結束付款（沒有花錢），不會因為它已經在佇列裡就照付。交易流程設計說「取消只阻止新的動作」，對還沒送出 tx#1 的付款，這就是新的動作。
  - relayer 逾時或結果不明時，tx#1 可能已在鏈上。重啟後一律先再問一次 relayer（relayer 本身冪等），確定沒有預留，才可以記成 `RESERVE_FAILED`、`NOT_PAID_EXPIRED` 或 `TASK_ENDED`。不然這筆付款就停在 `requested`，它的金額也繼續占著預算。
- **unknown 收斂**：`Reconciler.Status` 判為
  - `settled`，或
  - `not_settled_expired`（轉 `needs_review`，因為 tx#1 已花），或
  - `needs_review`。

  `pending` 就繼續等，**任何一種結果都不會讓 Host 再付一次**。
- **AE1**：`reconcile.Unauthorized(兩位客人的錢包, payments 裡所有簽出的授權)` 的結果寫進 Host 的對帳頁與 AE1 報告。

## 8 Agent 看到什麼

`request_purchase` 照舊立即回傳裁決（不等待付款）。回應裡的 `payment` 從 `"none"` 改成付款狀態（`paying`、`paid` 等），說明文字也改寫。Agent 再呼叫一次就看得到最新狀態（現有的「已請購就回傳保存的狀態」機制）。內容是否交給 Agent，見 H-D2。

## 9 要 Aaron 決定的事

| # | 問題 | 選項 | 建議 |
|---|---|---|---|
| H-D1 | 誰觸發付款 | **A** Host 自動：Allow 後、或 `GRANT_ISSUED` 後，由付款工作一次一筆處理；<br>**B** 新增 Agent 工具，讓 Agent 自己按「付款」；<br>**C** 主持人在畫面上按鈕 | **A**：付款權限留在 Host 與 Signer，Agent 只能請購（交易流程的信任邊界）；也符合決策 4、7、8 |
| H-D2 | 交付的內容給誰 | **A** Host 收下內容，只保存它的 hash 並在畫面上顯示收據；`request_purchase` 只回付款與交付狀態；<br>**B** 已付款後把內容交給 Agent（新增工具或在回應中附上） | **A**：商店內容是不可信的，直接送進 Agent 會多一個 prompt injection 的入口（AE1 量過的那種）；要交給 Agent 時，比照目錄文字做清理與截斷，另開一片 |
| H-D3 | 付款時間窗（目前 60 秒） | **A** 會結算的商店報價 `maxTimeoutSeconds` 改為 300，與核准的有效期（`approval_ttl_seconds` 300）一致；M1 商店維持 60；<br>**B** 維持 60：Ask 超過 60 秒才核准就付不出去，要重新請購並再核准一次；<br>**C** 600（Signer 的上限） | **A**：讓本人核准有實際可用的時間；代價是一筆上不了鏈的轉帳最多占住商店的 gas nonce 5 分鐘（§9.11 會自動取代） |
| H-D4 | Z2 預算何時登記上鏈 | **A** task 開始時，以 `budget_initial` 登記（每個 task 一筆 tx#0，在背景進行、不擋 run 開始；登記完成前付款會等待）；<br>**B** 第一次付款前才登記 | **A**：付款時少一段等待；demo 的 task 數量不多 |
| H-D5 | 預算 relayer 跑在哪裡（P2-D2 決定的是「獨立程序」） | **A** P2-a 先放在 demo 程序裡，用 anvil 公開測試金鑰，先打通端到端；P2-b 之前再改成獨立程序 `apa-relayer`（Unix socket、加密金鑰檔、啟動時輸入密語）；<br>**B** 現在就做成獨立程序 | **A**：P2-a 只用公開測試金鑰，獨立程序帶來的保管效益要到 testnet 才有；接口不變，P2-b 再換。這會暫時偏離 P2-D2 的形式，所以要你同意 |
| H-D6 | 本機 demo 的兩個付款錢包金鑰 | **A** 由你用 `apa-signer keygen` 建立兩把新的本機測試金鑰（你自己輸入密語），只在 anvil 上持有 TestUSDC；<br>**B** 把 anvil 的公開金鑰匯入 keystore（要新增匯入命令，密語仍由你輸入） | **A**：沿用既有、已 review 過的建立流程。依規定，建立付款金鑰要你個別核准，這一題就是在請你核准 |
| H-D7 | 資料庫 | **A** schema 7 新增 `payments`、`receipts` 與事件；P2 的 demo 用新的資料庫，既有的 `var/` 資料庫不升級；<br>**B** 升級既有資料庫 | **A**：遵守「demo 結束前不清理」；升級路徑照樣提供並測試 |
| H-D8 | 這一片的 UI 範圍 | **A** 網頁（卡片步驟、`payment_status` 文字、交付區、收據、對帳頁）；Unity 先把新階段對映到現有的階段，Unity 交付區的演出另開 UX 片；<br>**B** 網頁與 Unity 一起做 | **A**：先讓事實正確；Unity 的演出不影響付款的正確性 |

## 10 界線

- 只在本機 anvil。不連 testnet、faucet 或任何外部服務；不碰 `/opt`；不用真錢。
- 付款錢包金鑰的建立與解鎖依 H-D6 由你進行；我不輸入密語、不讀金鑰。
- 不做 P2-b 的事：testnet 部署、RPC、faucet、上限、test402，以及 V1 `/opt` 部署。

## 11 分段（每段各自提交與 review）

1. **H1 Store**：schema 7、`payments`／`receipts`／事件、狀態轉移與 `payment_status` 推導（不需要鏈；以測試證明狀態轉移、冪等、重啟規則）。
2. **H2 付款工作**：以介面接上 relayer、Signer、商店、Reconciler。先用假的元件測試每一條失敗路徑：逾時、402、202、重啟、期限過去。
3. **H3 接線**：`apa-signer` 的 `reservation` 段與 health、`BeginPayment` 接受 webauthn、`signerclient` 帶 reservation 與 `approval_id`、cmd/demo 的旗標與本機設定腳本、H-D4 的登記。
4. **H4 網頁**：H-D8 A 的範圍。
5. **H5 AE1 對帳**：`Unauthorized` 的結果寫進報告。
6. **H6 本機端到端**：在 anvil 上跑 §1 列出的反例與一條 Allow、一條 Ask 的完整付款，整理證據。

H6 需要真的 Signer 與付款錢包，要等 H-D6 的金鑰建立好才能開始；H1 到 H5 不需要。

## 12 進度

### 12.1 H1 Store（schema 7）

- **事件**：
  - pay 系列新增 9 個付款事件：`PAYMENT_REQUESTED`、`BUDGET_RESERVED`、`PAYMENT_SIGNED`、`PAYMENT_SUBMITTED`、`PAYMENT_SETTLED`、`PAYMENT_NOT_SETTLED`、`PAYMENT_NEEDS_REVIEW`、`RESOURCE_DELIVERED`、`RECEIPT_ISSUED`（`PAYMENT_REQUESTED` 是 review 後加的，見下方）。
  - payload 新增 pay 系列專用的 `payment` 紀錄：預留的方法、金額、交易與區塊；結算的交易與區塊；交付內容的 SHA-256；收據 ID。每種事件只帶它該帶的欄位，所有欄位都要有，也不能多帶。
  - 原因代碼分兩組：
    - `PAYMENT_NOT_SETTLED`（沒有花錢）：`RESERVE_FAILED`、`NOT_PAID_EXPIRED`；
    - `PAYMENT_NEEDS_REVIEW`：`RESERVED_NOT_PAID`、`NOT_SETTLED_EXPIRED`、`VERIFICATION_FAILED`、`RECONCILE_NEEDS_REVIEW`、`DELIVERY_FAILED`。
  - M1 的事件一律不接受 `payment`；`schemas/m1.schema.json` 也標明它在 M1 永遠不會出現。
- **順序規則**（`paycontract.ValidateEventOrder`）：
  - 只有 Allow（委派）或已核准並發出 webauthn grant 的請購可以**請求付款**（`PAYMENT_REQUESTED`）；模擬核准的 grant 永遠不會付款。
  - 付款的方法必須是這張請購的授權方式；task 結束後（Deny、取消、CLI 逾時，`paycontract.Ended`）不能再請求付款。
  - 請求 → 預留 → 簽名 → 送出 → 結算 → 交付 → 收據，各一次、依序進行。**已經請求的付款，在 task 結束後仍可走完**：它的 tx#1 可能已經在鏈上。
  - `NOT_SETTLED` 只能結束一筆已請求、從未預留的付款；`NEEDS_REVIEW` 用在「已預留但未結算」或「已結算但未交付」（`DELIVERY_FAILED`）的付款；結束之後不能再有任何付款事件。
- **`payments` 表**：
  - 以 `intent_hash` 為鍵，一張請購最多一列；欄位有方法、grant 參照（Signer 的委派 `id@version`，或 Signer 的核准 ID）、金額、狀態與原因代碼、預留證據、簽出的授權、結算交易與區塊、商店的 `PAYMENT-RESPONSE`。
  - CHECK 規定每個狀態必須有的欄位。
- **`receipts` 表**：已交付的付款各有一張收據，記錄 tx#1、tx#2、`PAYMENT-RESPONSE`、內容的 SHA-256 與大小，不存內容本身。
- **Store 方法**：
  - `StartPayment`：Allow 走委派；Ask 必須經本人核准，而且是 webauthn。同一筆交易寫入付款列與 `PAYMENT_REQUESTED`。其他一律拒絕且什麼都不寫：
    - 別人的 task；
    - 依順序規則已結束的 task（和規則用同一個判斷 `paycontract.Ended`）；
    - Deny、尚未核准的 Ask、模擬核准；
    - 已過 `valid_before` 或 grant 已到期；
    - 剩餘預算扣掉其他已請求、尚未預留的付款之後，不夠這一筆（`OVER_BUDGET`）；
    - 沒有 Signer 委派；在另一個委派下再呼叫一次（`IDEMPOTENCY_CONFLICT`）；
    - 憑證不再有效。
  - 之後依序是 `PaymentReserved`（同一筆交易裡扣減 task 的剩餘預算）、`PaymentSigned`、`PaymentSubmitted`、`PaymentUnknown`、`PaymentSettled`、`PaymentDelivered`（同時開收據）、`PaymentFailed`，另有 `PaymentsIn`、`PaymentOf`。
  - 每一步的狀態和事件都在同一筆交易裡寫入。同一步驟、同樣的值再呼叫一次，回傳現況；值不同回 `IDEMPOTENCY_CONFLICT`；不合法的轉移回 `INVALID_PAYMENT_TRANSITION`。
- **檢視**：`PurchaseView.payment` 從付款事件推導（`PAYMENT_REQUESTED` 起就顯示「付款中」），包含 `status`（即 `payment_status`）、狀態、原因代碼、tx#1、tx#2、內容 hash、收據 ID。原始決策（`decision`）仍然不改寫：它的 `payment_status` 是決策當下的合約欄位。
- **恢復檢查**（schema 7）：
  - 每筆付款的狀態要和它的付款事件一致；有付款事件就一定要有對應的付款列；方法要符合請購的授權，webauthn 付款的 grant 參照必須是那個 grant 的 Signer 核准 ID。
  - pay 系列 task 的剩餘預算，必須等於初始預算減去已預留的總額。
  - 每筆已交付的付款恰好有一張收據，收據的 tx#1、tx#2 與付款列相同。
- **升級**：schema 6 → 7 只新增兩張表，沿用 `--upgrade-db`（先驗證、再備份）。
- **驗證**：
  - Store 新增測試：
    - `TestPaymentAllowPaid`：每一步各做兩次，第二次在重啟之後，都回傳同一個狀態；事件順序、預算、收據、檢視都正確；不能倒退；別的 consumer 讀不到。
    - `TestPaymentStepConflict`、`TestPaymentWebAuthnAsk`（模擬核准被拒）、`TestPaymentStartRefusals`。
    - `TestPaymentFailures`：4 種結束方式各自的狀態、`payment_status` 與預算。
    - `TestPaymentRecoveryRefuses`：狀態與事件不符、預算沒扣、收據被刪，都被恢復檢查擋下。
    - `TestUpgradeSchema6`。
  - paycontract 新增 `TestPaymentEventOrder`（21 種順序）、`TestPaymentEventShape`（20 種 payload）、`TestM1RefusesPaymentRecord`。
  - 既有測試只改了 schema 版本與升級的輔助函式（新增 `toSchema6`）。
  - `go test -race` 通過。突變 review 後共 22 項，22／22 KILLED（`local-mutations.py "P2 H1" --all`，先跑未突變的基準）。
- **狀態表（§4）的實作差異**：`not_settled_expired` 與 `delivery_failed` 都是 `needs_review` 加上原因代碼（`NOT_SETTLED_EXPIRED`、`DELIVERY_FAILED`），不另設狀態。

#### H1 的 review（9b854b4）與修正

Claude Code 獨立 context 的 code-reviewer，結論 REQUEST CHANGES。審查者確認不會讓資料庫變得無法開啟：每一步都和事件在同一筆交易裡寫入，而且寫入前會先檢查順序。但有兩項 Critical：

| 等級 | 問題 | 處理 |
|---|---|---|
| Critical | `requested` 沒有事件：付款已開始、tx#1 已在鏈上之後，task 才被取消或逾時，預留就記不進去了（只能錯記成「沒花錢」，或永遠卡在 `requested`） | `StartPayment` 發出 `PAYMENT_REQUESTED`；「task 結束後不能開始」改成套在請求上，已請求的付款可以走完；`TestPaymentInFlightWhenTheTaskEnds`（取消、逾時、另一張請購被 Deny 三種） |
| Critical | `StartPayment` 只看取消與逾時，但順序規則把任何 Deny 也視為結束 | 共用 `paycontract.Ended`；`TestPaymentNotStartedAfterADeny` |
| Important | 交付重送沒有比對內容 | 和收據的 hash 與大小比對 |
| Important | 預算只在預留之後才檢查：兩個 Allow 在預留前一起決定，加起來可能超過預算 | 開始時就扣掉其他已請求的付款再檢查；`TestPaymentStartOverBudget` |
| Important | 恢復檢查沒有比對 webauthn 的 grant | 比對 grant 的方法與 Signer 核准 ID；`TestPaymentRecoveryRefusesAnotherGrant` |
| 建議 | 預留重送沒比對 tx 與區塊；在另一個委派下重新開始沒有被拒；已過期的請購仍可開始；收據沒比對 tx#1；`requested` 的檢視顯示「未開始」；`PaymentsIn` 在 Go 裡過濾；負的大小變成 CHECK 錯誤；M1 的錯誤訊息沒提 payment；多餘的條件 | 都已處理（付款列加 `reserve_tx`／`reserve_block`） |
| 建議 | `PaymentSettled` 重送時若沒給 `PAYMENT-RESPONSE` 就不比對 | 不改：Reconciler 判定結算時沒有 `PAYMENT-RESPONSE` 可給 |
| 問題 | Signer 委派代號（`DelegationRef`）是否必須是 task 綁定的那個委派 | 兩者是不同的東西：Host 的委派是 task 的業務範圍，Signer 的委派是 `signer.json` 的付款授權，Store 無法比對；由 Host 的可信設定提供 |
| 問題（待 Aaron） | CLI 逾時（`TASK_TIMED_OUT`），或同一個 task 的另一張請購被 Deny 之後，原本開著的 Ask 仍可被本人核准、發出 grant，但不會付款：本人核准了，什麼都沒付 | 目前的行為就是這樣，H4 會讓畫面寫明「task 已結束，不會付款」。要不要改成這兩種情況下仍付款，需要你決定 |

第二輪 review（3777321）：結論 APPROVE。審查者確認：`paycontract.Ended` 與順序規則的三個結束條件完全相同；每一種 Store 呼叫順序都能被規則接受；開始時預先保留預算，所以 tx#1 上鏈後預留不可能因為預算不足被拒；恢復檢查的預算規則與扣減一致。處理：

| 等級 | 問題 | 處理 |
|---|---|---|
| Important | 取消不會停下排隊中的付款，與交易流程設計「取消只阻止新的動作」不符 | 新增沒花錢的結束代碼 `TASK_ENDED`（狀態 `not_paid_ended`）；付款工作的規則寫進 §7，H2 實作 |
| Important | 待決問題漏了「另一張請購被 Deny」 | 補上 |
| 建議 | `requested` 沒有「預留結果不明」的出口 | H2 的規則寫進 §7 |
| 建議 | 空的預留證據或授權可以寫入 | 拒絕 |
| 建議 | 缺測試：grant 到期、失敗後預算釋放、task 結束後記錄失敗 | 補上，並加兩項突變（H1 共 24 項，24／24 KILLED） |
| 建議 | 文件過時 | 修正 |

### 12.2 H2 付款工作（`internal/payflow`）

- **`Worker`**：一次處理一筆付款（決策 7），依 Store 的狀態推進。四個外部元件都是介面，H3 再接上真的：
  - `Relayer`：`Reserve` 對同一張請購冪等；`Lookup` 只查不建；
  - `Signer`：`/v1/sign`，同一個 request ID 回同一個 attempt；
  - `Shop`：送出帶 `PAYMENT-SIGNATURE` 的請求；
  - `Chain`：Reconciler。
- **每個狀態怎麼推進**：
  - `requested`：**先問 relayer 這筆 intent 已經有什麼**（`Lookup`，只查不建）。
    - 已預留：記下預留；已失敗：`RESERVE_FAILED`；送出未定：等待。
    - 什麼都沒有時才看 task：已結束（`TaskEnded`）就 `TASK_ENDED`；已太接近 `valid_before` 就 `NOT_PAID_EXPIRED`。
    - 否則 `Reserve`。relayer 拒絕就 `RESERVE_FAILED`（交易送出前就拒絕，或已 revert，都沒有花錢）；`RESERVATION_PENDING`、`TASK_BUSY`、`RELAYER_NONCE_BLOCKED`、`TASK_REGISTRATION_PENDING`（task 的登記尚未確認）、連線錯誤都等下一輪，由 `Lookup` 決定。
    - 所以中斷或回應遺失時，tx#1 只會有一筆，「沒花錢」也只在 relayer 確定沒有預留之後才會記下。
  - `reserved`：以付款的授權向 Signer 請求簽名（委派 `id@version`，或本人核准的 `approval_id`），request ID 由 intent 決定。
    - Signer 回「現在不行」（忙碌、逾時、預留尚未確認）就等待；其他拒絕，或太接近期限，轉 `RESERVED_NOT_PAID`（tx#1 已花，需要人工處理）。
    - Signer 的 attempt 已關閉而沒有簽（`abandoned`、`expired_unsigned`）也轉 `RESERVED_NOT_PAID`；attempt 還開著就等待，太接近期限才轉。
    - 簽回來的授權先和請購的付款綁定比對（From、To、金額、有效期間、nonce），不符就轉 `RESERVED_NOT_PAID`，不會送出。
  - `signed`：**先記成已送出**，再向商店送出 `PAYMENT-SIGNATURE`。同一筆付款每次送的都是同一份簽章。
  - `submitted`／`unknown`／`settled`：
    - 連線錯誤或 5xx：轉 `unknown`。
    - 200：**只有鏈上確認（`Chain.Status` 為 settled）才算已付款**，這時才記下結算、交付內容的 hash 與收據。鏈上還沒確認就等待；下一輪再送同一份簽章，商店是冪等的。
    - 202：等待。
    - 402（驗證失敗）或 400：`VERIFICATION_FAILED`，不再送出；已經 `settled` 時則是 `DELIVERY_FAILED`（已付款、未交付）。
    - 402 `settlement_failed`（例如被搶先送出）或 `invalid_exact_evm_payload_authorization_nonce_used`（商店說 nonce 已用過）：交給鏈上判定。
    - 其他狀態（408、429、代理的 404）：不算商店對這筆付款的回答，交給鏈上判定。
    - 由鏈上判定時：settled 就記下結算；之後商店交付就記為已交付，商店拒絕就是 `DELIVERY_FAILED`（已付款、未交付）。`not_settled_expired` 轉 `NOT_SETTLED_EXPIRED`；`needs_review` 轉 `RECONCILE_NEEDS_REVIEW`；pending 就等待，絕不重付。已經 `settled` 的付款只認 settled：鏈上回別的答案就等待（那是節點的問題）。
- **`Recover`**（啟動時）：`submitted` 一律轉 `unknown`（決策 5）。
- **`Run`**：先恢復，再每隔一段時間（`Every`，0 時為 1 秒；或被 `Kick` 喚醒）把所有未完成的付款各推進一次，一次一筆。`ErrPending` 以外的錯誤交給 `Log`。`Run` 執行時不另外呼叫 `Step`（一次一筆；即使另外呼叫也安全，因為每個外部呼叫都是冪等的）。
- **`Margin`**：距離 `valid_before` 多少秒以內就不再開始新的一步。必須大於商店自己的 5 秒，再加上簽名的時間，否則已預留、已簽名的付款會被商店以期限拒絕。
- **已知限制**：`late` 只看 `valid_before`。本人核准的 grant 或委派如果比它早到期，tx#1 會先花掉，再由 Signer 拒絕（`APPROVAL_EXPIRED`／`DELEGATION_EXPIRED`），轉 `RESERVED_NOT_PAID`。H3 接上 Signer 時再考慮在預留前檢查。
- 新增：`budget.Relayer.Lookup`（anvil 測試確認：預留前回空、預留後回同一份證據）、`store.Store.TaskEnded`、`merchant.ParsePayment`（照商店的方式讀 exact-EVM payload，不做判定）。`budget.Relayer.Reserve` 對還在登記中的 task 改回 `TASK_REGISTRATION_PENDING`。
- **驗證**（真的 Store、假的四個元件；`go test -race` 通過）：
  - `TestPaidEndToEnd`：一次推進就走完，預留、簽名、送出各一次；已交付的不再移動。
  - `TestPaidOnlyWhenTheChainSaysSo`：商店回 200 但鏈上未確認時不算已付款，確認後才交付；全程同一份簽章。
  - `TestLostAnswersNeverPayTwice`：預留的回應遺失時，下一輪由 `Lookup` 找到，只有一筆 tx#1；Signer 的回應遺失時，用同一個 request ID 再問；商店的回應遺失時轉 `unknown`，再送同一份簽章，最後交付。
  - `TestRecoverSubmitted`：已送出、Host 隨即中斷 → `unknown` → 由鏈上與同一份簽章完成，relayer 沒被再呼叫。
  - `TestTaskEndedBeforeAndAfterTheReservation`：取消時還沒預留的付款是 `TASK_ENDED`，relayer 從沒被要求預留；取消前已預留的付款照常走完。
  - `TestRefusals`：relayer 拒絕 → `RESERVE_FAILED`；Signer 拒絕 → `RESERVED_NOT_PAID`；商店驗證失敗 → `VERIFICATION_FAILED`，不再送出。
  - `TestUnknownMovedByTheChain`：202 時等待；過期未用 → `NOT_SETTLED_EXPIRED`；被搶先送出（鏈上已付）→ `DELIVERY_FAILED`，畫面顯示已付款。
  - `TestTooLate`、`TestWebAuthnAskPaid`（Signer 收到的是 `approval_id`，不是委派）、`TestRun`。
  - review 後新增：
    - `TestPaymentSignatureTheShopReads`：`PAYMENT-SIGNATURE` 送到真的商店 handler，回 402 `M1_PAYMENT_UNAVAILABLE`（代表通過商店的判斷），payload 也能被 `merchant.ParsePayment` 讀取。
    - `TestRelayerStates`：relayer 已送出就等待（不再 `Reserve`）、已失敗就結束、查詢錯誤就等待；task 忙碌時等待，之後預留一次。
    - `TestSignerAnswers`：attempt 已放棄 → `RESERVED_NOT_PAID`；簽了另一個 nonce → `RESERVED_NOT_PAID`，沒有送出；attempt 還開著就等待，太接近期限才轉。
    - `TestShopAnswers`：5xx → `unknown`；429 留在 `submitted`；400 → `VERIFICATION_FAILED`；nonce 已用過且鏈上已付 → `DELIVERY_FAILED`。
    - `TestChainAnswers`：節點錯誤就等待；`needs_review` → `RECONCILE_NEEDS_REVIEW`。
    - `TestSettledThenDelivered`：鏈上已付、商店回 202 → `settled`；之後鏈上回別的答案仍是 `settled`；下一輪交付；`settled` 之後商店回 402 → `DELIVERY_FAILED`。
    - budget 的 `TestBlockedRegistrationIsResigned` 加上：登記卡住時預留回 `TASK_REGISTRATION_PENDING`（anvil）。
- **突變**：H2 新增 11 項；review 後再加 11 項（其中 1 項在 anvil 上），共 22 項，22／22 KILLED（`local-mutations.py "P2 H2" --all`）。過程中修了兩項突變本身的問題：一項突變後變數沒被使用而編譯不過；另一項在 macOS 上時鐘只到微秒，`UnixNano()%1000` 永遠為 0，突變根本沒有改變 ID。

#### H2 的 review（91ca392）與修正

Claude Code 獨立 context 的 code-reviewer，結論 APPROVE（有應修項目）。審查者對照真的 relayer、Signer、商店、Store 的狀態轉換與 paycontract 的順序規則，確認沒有第二筆 tx#1、第二份簽章或 `PAYMENT-SIGNATURE`，不會在有預留時記成「沒花錢」，也不會在 `Chain.Status` 不是 settled 時記成已付款。

| 等級 | 問題 | 處理 |
|---|---|---|
| Important | Signer 的 attempt 已關閉（`expired_unsigned`、`abandoned`）時回的是狀態而不是錯誤，付款永遠停在 `reserved`，tx#1 已花卻沒人處理 | 這兩種狀態轉 `RESERVED_NOT_PAID`；其他未簽的狀態只在還來得及時等待；`TestSignerAnswers` |
| Important | 重試清單裡的 `TASK_REGISTRATION_PENDING`，`Reserve` 從不回；登記中的 task 回 `TASK_NOT_REGISTERED`，付款直接失敗 | `Reserve` 對登記中的 task 回 `TASK_REGISTRATION_PENDING`；anvil 測試 |
| Important | `PAYMENT-SIGNATURE` 沒有對真的商店測過；欄位名稱或格式一錯，每筆付款都會在 tx#1 之後變成 `VERIFICATION_FAILED` | `TestPaymentSignatureTheShopReads`，新增 `merchant.ParsePayment` |
| Important | 假元件漏掉整個分支（relayer 的 sent／failed／錯誤、可重試的拒絕、鏈上錯誤與 needs_review、商店 5xx 與 400、`settled` 後交付、`settled` 後 402） | 都補上測試與突變 |
| 建議 | 簽回來的授權在記錄前沒有比對 | 和付款綁定比對，不符就 `RESERVED_NOT_PAID` |
| 建議 | `settled` 後商店的任何非 200／202（包括 408、429）都變 `DELIVERY_FAILED` | 只有 400／402 是最終的，其他交給鏈上 |
| 建議 | `settled` 後鏈上回 `not_settled_expired`／`needs_review` 時，Store 會拒絕轉換，每輪都出錯 | 已 `settled` 只認 settled，其他答案就等待 |
| 建議 | 商店回 nonce 已用過時（商店遺失資料庫，或授權外洩）直接記成 `VERIFICATION_FAILED`，即使鏈上已付 | 交給鏈上判定 |
| 建議 | `Run`：`kick` 在 `Run` 裡才建立，與 `Kick` 不同步；`Every` 為 0 時空轉；錯誤被丟掉；沒寫明不能和 `Step` 同時呼叫 | `sync.Once` 建立；預設 1 秒；`Log`；寫進註解與本節 |
| 建議 | 寫明 `Margin` 要涵蓋什麼 | 寫進註解與本節 |
| 建議 | `late` 只看 `valid_before`，grant 或委派可能更早到期 | 列為已知限制，H3 再處理 |
| 建議（H3） | Signer client 必須把 503 `SIGNER_ERROR` 當成一般錯誤，不能當成拒絕，否則暫時的錯誤會變成 `RESERVED_NOT_PAID` | H3 實作 |
| 建議 | §12.2 漏了 `settled → DELIVERY_FAILED`；Signer 那一點在修正前不正確 | 修正 |

第二輪 review（a7d6876）：結論 APPROVE。審查者確認：Signer 的簽名是由請購自己的付款綁定組成，所以正確的簽名一定通過比對；每一個要求的 Store 轉換都被 from-set 與順序規則接受；22 項突變的原句各只出現一次。建議與處理見 12.2 之後的修正提交。

### 12.3 H3 接線

- **H3a（404b61a）付款工作的真元件**：
  - `signerclient.Client.Sign`：付款工作自己的 request ID，原樣送 `/v1/sign`（帶 reservation 與委派或 `approval_id`），不經過 `payment_attempts`。拒絕回 `signer.Refusal`；503（`SIGNER_ERROR`、`SIGNER_TIMEOUT`）或沒有可讀的回應回一般錯誤，用同一個 request ID 再問。
    - **與 §3 的差異**：§3 原本寫「`payment_attempts` 保留給 Signer 握手，`BeginPayment` 改為接受 webauthn」。P2 的 Signer 改在鏈上確認預留（`budget.Verifier`），不再需要 P1-b 的「先寫入再呼叫」握手；付款的紀錄是 `payments` 這一列。兩份狀態並存只會互相不一致，所以 `payment_attempts` 與 `BeginPayment` 維持 P1-b 原樣，只供 `delegation` 的舊路徑使用。
  - `payflow.HTTPShop`：帶 `PAYMENT-SIGNATURE` 的 GET；任何狀態都解讀 `PAYMENT-RESPONSE`；內容超過 1 MiB 當成沒有回應（hash 不會是內容的）。
  - `budget.Relayer` 與 `reconcile.Token` 直接就是 `Relayer` 與 `Chain`。
  - `signer.json` 新增 `reservation` 段（`rpc`、`chain_id`、`budget_contract`、`confirmations`）：有這段時 `apa-signer serve` 用 `budget.Verifier` 在鏈上確認預留；沒有就照舊 `Unavailable`，什麼都不簽。`rpc` 必須是 http(s) 且不含帳密，`chain_id` 必須是已登記資產的鏈，確認數 1 到 64。
  - health：`ServeAll` 有預留確認時回 `ready`／`payment: available`，否則 `approvals-only`；demo 與網頁兩種都視為核准可用。
- **H3b 付款接進 demo**：
  - `--payments <file>`（需要 `--approvals-signer`：同一個 Signer 也簽付款）。檔案只有本機鏈的部署資訊與資料庫目錄，**沒有任何金鑰**：relayer 的 gas、issuer、policy 與商店的 gas 以 anvil 公開測試金鑰的編號指定（`eth.AnvilKey`，H-D5），四把必須不同。`rpc` 必須是 loopback 的 http，且鏈 ID 要和檔案相符；目錄必須是 0700。
  - 有 `--payments` 時，商店改用 `merchant.NewSettled`（自己驗證與結算），報價的 `maxTimeoutSeconds` 為 300（H-D3）；M1 商店維持 60。
  - 付款工作在 expiry 的 goroutine 群組裡跑（Store 關閉前停止），`Margin` 30 秒；錯誤只記固定代碼。
  - **starter**（付款工作每一輪開始前執行，同一個 goroutine）：讀 `store.PayTasks()`，即未結束的 pay 系列 task、它的初始預算，以及已決定要付款（Allow，或本人核准的 Ask）但還沒開始付款的請購。
    - 每個 task 以初始預算向 relayer 登記一次（H-D4；未確認的登記由 relayer 自己完成）。relayer 拒絕就不再試；節點錯誤或仍在登記中就下一輪再試。
    - 每張請購在這個程序裡只 `StartPayment` 一次：Allow 用設定裡該客人的 Signer 委派，Ask 用 grant 的核准。被拒絕就記下代碼，重啟前不再試。
    - 以掃描而不是在三個地方掛勾子，Host 在「決定」與「開始付款」之間中斷時，重啟後仍會開始付款。
