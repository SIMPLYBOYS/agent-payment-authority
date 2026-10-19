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
  - Signer 的 request ID 由 Store 在 `payment_attempts` 產生並提交（每張請購一個，`intent_hash` 唯一），之後只用這一個。relayer 與商店本身也各自冪等（§9.6、§9.10）。
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
- **向 Signer 請求簽名時**（`signerclient.Pay`）：結果不明（沒有可讀回應、503）在期限前等待，用同一個 request ID 再問；Signer 的拒絕，以及它的 403 `CALLER_NOT_ALLOWED`、400 `BAD_REQUEST`（設定或請求錯誤，再問也一樣），在 tx#1 已花之後轉 `RESERVED_NOT_PAID` 由人處理；Store 的 `CREDENTIAL_STATUS_UNAVAILABLE`、`CLOCK_REGRESSION` 在期限前等待。
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
| 問題（Aaron 已決定） | CLI 逾時（`TASK_TIMED_OUT`），或同一個 task 的另一張請購被 Deny 之後，原本開著的 Ask 仍可被本人核准、發出 grant，但不會付款：本人核准了，什麼都沒付 | **Aaron：「原本開著的 Ask 應該是不可以被核准同時也不會付款」**（已接受案例 `cli-timeout-pending` 本來就規定逾時後 Ask 維持 pending、不得有 `APPROVED`、`GRANT_ISSUED`）。見 §12.6 |

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
  - `Signer`：Host 的 Signer client（`signerclient.Pay`）：請求先記入 `payment_attempts`（request ID 由 Store 產生並提交），再送 `/v1/sign`；再問時用同一個 request ID（§3）；
  - `Shop`：送出帶 `PAYMENT-SIGNATURE` 的請求；
  - `Chain`：Reconciler。
- **每個狀態怎麼推進**：
  - `requested`：**先問 relayer 這筆 intent 已經有什麼**（`Lookup`，只查不建）。
    - 已預留：記下預留；已失敗：`RESERVE_FAILED`；送出未定：等待。
    - 什麼都沒有時才看 task：已結束（`TaskEnded`）就 `TASK_ENDED`；已太接近 `valid_before` 就 `NOT_PAID_EXPIRED`。
    - 否則 `Reserve`。relayer 拒絕就 `RESERVE_FAILED`（交易送出前就拒絕，或已 revert，都沒有花錢）；`RESERVATION_PENDING`、`TASK_BUSY`、`RELAYER_NONCE_BLOCKED`、`TASK_REGISTRATION_PENDING`（task 的登記尚未確認）、連線錯誤都等下一輪，由 `Lookup` 決定。
    - 所以中斷或回應遺失時，tx#1 只會有一筆，「沒花錢」也只在 relayer 確定沒有預留之後才會記下。
  - `reserved`：以付款的授權經 `signerclient.Pay` 向 Signer 請求簽名（委派 `id@version`，或本人核准：Store 從 grant 讀出 `signer_approval_id`），帶上預留證據；request ID 是 `payment_attempts` 的那一個。
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
  - `TestLostAnswersNeverPayTwice`：預留的回應遺失時，下一輪由 `Lookup` 找到，只有一筆 tx#1；Signer 的回應遺失時，用 `payment_attempts` 的同一個 request ID 再問；商店的回應遺失時轉 `unknown`，再送同一份簽章，最後交付。
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

第二輪 review（a7d6876）：結論 APPROVE。審查者確認：Signer 的簽名是由請購自己的付款綁定組成，所以正確的簽名一定通過比對；每一個要求的 Store 轉換都被 from-set 與順序規則接受；22 項突變的原句各只出現一次。處理：

| 等級 | 問題 | 處理 |
|---|---|---|
| 建議 | 已 `settled` 的付款交付前仍要問鏈；節點找不到舊 log 時（修剪、範圍限制）就永遠不交付 | 已 `settled` 時不再問鏈，只看商店：200 交付、400／402 `DELIVERY_FAILED`、其他等待；先前的保護條件移除 |
| 建議 | `unknown` 的付款收到 400／402 記成 `VERIFICATION_FAILED`，但 tx#2 可能已在鏈上 | `unknown` 時交給鏈上判定（`valid_before` 之後收斂為 settled 或 `NOT_SETTLED_EXPIRED`） |
| 建議 | `expired_unsigned`、授權的 From／To／金額／有效期間沒有會失敗的測試；測試用了 Signer 不會回的狀態 `signing` | `TestSignerAnswers` 改為兩種已關閉狀態、六個欄位各改一個、`accepted`；加突變 |
| 建議 | `byChain` 裡 `ans.Status == 400` 已不可能 | 改為只看 402 |

H2 突變共 28 項，28／28 KILLED。

### 12.3 H3 接線

- **H3a（404b61a）付款工作的真元件**：
  - **Signer 握手照 §3（Aaron：「修正回原有設計應該有的規範」）**：
    - `payment_attempts` 是 Signer 的握手紀錄：`signerclient.Pay` 先以 `BeginPayment` 記下請求並提交（request ID 由 Store 產生，同一張請購永遠是同一個），再送 `/v1/sign`；回應記回這一列（accepted、signed、rejected、unknown…）。
    - `BeginPayment` 接受 webauthn：只限本人核准過的 Ask，且 grant 是 webauthn、未過期；Store 自己從 grant 讀出 `signer_approval_id`，呼叫端不能指定。schema 7 重建 `payment_attempts`（新增 `approval_id`，方法與依據以 CHECK 一一對應）；schema 7 只在這個分支，升級時舊列原樣保留。恢復檢查要求 webauthn 列就是該請購 grant 的核准。
    - `signerclient.Pay(req, reservation)` 帶上預留證據，委派送 `delegation_id@version`，本人核准送 `approval_id`。503（`SIGNER_ERROR`、`SIGNER_TIMEOUT`）或沒有可讀的回應記成 unknown、回一般錯誤，下一輪用同一個 request ID 再問；拒絕回 `Refused`。
    - 依 H1 的規則（已請求的付款可以走完），已預留（tx#1 已花）的請購在 task 取消後仍可 `BeginPayment`；尚未預留的照舊拒絕（`TASK_ALREADY_ENDED`）。
    - 先前的版本（404b61a 的 `signerclient.Client.Sign` 與付款工作自己由 intent 算出的 request ID）不經過 `payment_attempts`，偏離了 §3，已移除。
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
- **H3c 端到端（anvil，虛擬）與 starter 搬進 payflow**：
  - starter 改為 `payflow.Starter`（`Worker.Before`），demo 只負責接線；`TestStarter` 測登記的重試規則（仍在登記中或節點錯誤：下一輪再試；拒絕：不再試）、Allow 用客人的委派、核准的 Ask 用 grant、每張請購只開始一次、被拒絕的不再試、已結束的 task 不登記。
  - `TestPaidOnAnvil`：所有真元件一起在本機 anvil 上跑。部署 Verifier、Budget、TestUSDC，以這次產生的一次性測試金鑰當付款人並鑄幣；Signer 在 Unix socket 上以 `budget.Verifier` 在鏈上確認預留；relayer 與會結算的商店（HTTP）用 anvil 公開金鑰付 gas；Reconciler 判定。一筆 Allow：登記預算 → 預留 → 簽名 → 結算 → 交付；商店收到 500000、付款人剩 4500000；再推進一次不會多送任何東西；`Unauthorized` 對帳為零。**這是虛擬結果**：金鑰是測試裡產生的，不是 `apa-signer keygen` 與真的 keystore（H6 才用你建立的金鑰）。
  - H3 突變 19 項，19／19 KILLED（2 項跨套件、1 項在 anvil 上，放在 LOCAL_MUTATIONS）。
- **H3d（2e20d40）本機 anvil 設定**：`cmd/p2-local` 在全新的 84532 anvil 上部署 Verifier、Budget（issuer 與 policy 為 anvil 金鑰 1、2）與 TestUSDC，把 TestUSDC 的程式碼放到 registry fixture 的資產位址（`0x4444…`，它的 EIP-712 domain 讀自己的位址，所以 registry、profile、scenarios 與 `signer.json` 都不必改），鑄幣給兩個付款錢包，在 `var/p2-local`（0700）寫出 `payments.json` 與列出這兩個錢包的 `registry.json`，並印出 `signer.json` 的 `reservation` 段。`scripts/demo/p2-local.sh 0x<wallet-a> 0x<wallet-b>` 啟動 anvil、建 zkbudget、執行它。在全新 anvil 上部署位址固定。

#### H3 的 review（404b61a、5b6e967、1e13b7f）與修正

Claude Code 獨立 context 的 code-reviewer，結論 REQUEST CHANGES（沒有 Critical）。審查者確認：沒有重付、沒有未經鏈上確認就記成已付款、沒有從檔案讀金鑰、沒有第二筆 tx#1、簽名或 `PAYMENT-SIGNATURE`；`Client.Sign` 對 Signer 每一種回應的對應都正確；停止順序（付款工作 → Store → `pay.Close`）正確。

| 等級 | 問題 | 處理 |
|---|---|---|
| Important | 沒有檢查 Signer 能簽付款就開始 tx#1：照說明啟動 `serve-approvals` 時每筆 Allow 花掉 tx#1 後被 `PAYMENT_MODE_OFF` 拒絕；沒有 `reservation` 段時等到期限才結束 | `--payments` 時 Signer health 必須是 `ready`，否則不啟動；`PAYMENT_MODE_OFF` 加入等待清單；修正 `--approvals-signer` 說明；`TestPaymentsNeedAReadySigner` |
| Important | starter 在 task 登記成功前就開始付款：登記因節點錯誤失敗時，同一輪的付款得到 `TASK_NOT_REGISTERED`，永久變成 `RESERVE_FAILED` | 登記成功（或被拒絕）之前不開始該 task 的付款；`TestStarter` 改為驗證這個順序 |
| Important | 503 → 一般錯誤沒有測試 | 當時加了 `TestSignUnavailableIsNotARefusal`；改回 §3 後由 `TestPaySendsTheApprovalAndReservation` 測 `Pay` 的 503（記成 unknown、不是拒絕）與突變 |
| 建議 | `begin` 在任何錯誤後都不再試 | 只有 Store 的拒絕（有代碼）不再試 |
| 建議 | loopback 不能證明是本機鏈（`ssh -L` 到公開節點也能通過，鏈 ID 84532 也相同） | demo 與 p2-local 都要求 `web3_clientVersion` 以 `anvil/` 開頭；`TestAnvilOnly` |
| 建議 | Signer 的 `max_window_seconds` 小於 300、或確認數高於 relayer 時，每筆付款都會在 tx#1 之後失敗 | p2-local 印出的說明寫明 `max_window_seconds` 至少 300；確認數與 relayer 相同（1）。H6 的 `signer.json` 依此設定 |
| 建議 | 已 `settled` 時內容超過 1 MiB，每秒重抓、永遠不結束 | `ErrTooLarge`：已 `settled` 時轉 `DELIVERY_FAILED` |
| 建議 | 預設 client 會跟隨轉址並帶上 `PAYMENT-SIGNATURE` | 不跟隨轉址：3xx 就是回應，交給鏈上判定 |
| 建議 | 停止順序：HTTP 伺服器（商店）先停，付款工作之後才停 | 付款工作改用 runs 的 context，在 HTTP 伺服器停止前停止；仍在 Store 關閉前等它結束 |
| 建議 | 啟動時恢復失敗一次，整個程序就沒有付款 | 5 秒後再試 |
| 建議 | `a key in the file` 的測試期待的字串太寬 | 改為 `unknown field gas_key` |
| 建議 | `/health` 與 `payment_capability` 仍寫「不能付款」 | H4 處理（畫面與文字） |
| 建議 | `ac.pays` 以值的型別判斷 `Unavailable{}` | 不改：只有 `assemble` 建立它 |

H3 突變共 26 項，26／26 KILLED。

第二輪 review（2e20d40、af43f1a）：結論 REQUEST CHANGES（沒有 Critical）。審查者確認：付款工作在每一條返回路徑都會在 Store 關閉前停止、不會 deadlock；TestUSDC 放到 fixture 位址後，鑄幣、EIP-3009、對帳與 Signer 的 domain 都一致；`verifyingContract` 是 `0x4444…`，簽名不可能在真的 Base Sepolia 重放。

| 等級 | 問題 | 處理 |
|---|---|---|
| Important | 重用同一個 `var/p2-local` 開新鏈：舊的 `relayer.db` 以為 task 已登記（task ID 與合約位址在新 anvil 上都相同），每筆付款都 `RESERVE_FAILED`，舊的已簽交易還可能在新鏈上送出 | p2-local 拒絕已有 `payments.json`、`relayer.db` 或 `settle.db` 的目錄；腳本說明改為新鏈用新的 `P2_LOCAL_DIR`；TestSetup 在新的 anvil 上用同一目錄驗證被拒 |
| Important | 登記時的 `RELAYER_NONCE_BLOCKED` 被 starter 當成永久拒絕 | 與 `TASK_REGISTRATION_PENDING` 一樣下一輪再試；TestStarter 與突變 |
| 建議 | 輸出沒說要用新的 registry、委派 ID 寫死且沒說明 | 印出 `--registry`、`--payments` 的路徑，以及 `signer.json` 要用的委派 ID、資產與商家 |
| 建議 | 以位置取 registry 的錢包與資產 | 以 `wallet_id` 查；只接受一個資產 |
| 建議 | 8545 已被佔用時，腳本的等待會連到別的節點 | 先檢查 8545 沒人聽、等待時確認 anvil 還活著、`curl --max-time` |
| 建議 | `setCode` 之後失敗，anvil 無法再設定 | 錯誤訊息說明要重開 anvil |
| 建議 | 沒有代碼的 `StartPayment` 錯誤重試、恢復失敗的 5 秒重試、`paymentSigner` 與 `eth.Anvil` 的呼叫點沒有測試 | 不另加：前兩者需要讓 Store 或 relayer 故障的替身，呼叫點由函式本身的測試與 H6 的實跑涵蓋 |
| 建議 | 已 `settled` 時商店回 200／400／402 以外的狀態，會一直重抓 | 不改：商店是同一程序內的 `merchant.NewSettled`，不會轉址或回 404；換成外部商店時再設上限 |

H3 突變共 29 項，29／29 KILLED。

### 12.4 H4 畫面與 Agent 看到的付款狀態

- **H4a Agent 看到什麼（§8）**：`request_purchase` 的 `payment` 在付款開始後改為 `payment_status`（`paying`、`confirming`、`paid`、`not_paid`、`needs_review`），並附固定的說明文字；付款前照舊是 `none`。Agent 看不到任何交易 hash、內容 hash 或內容本身（H-D2）。`paid` 但交付失敗時，說明寫明商店沒有交付、由人處理。`TestPaymentStatusProjected`；突變 2 項，2／2 KILLED。
- **H4b 網頁（H-D8 A）**：
  - 請購卡片：付款開始後依 Store 事件加上「預留、簽名、送出、鏈上確認、交付」步驟；未付款或待人工處理時，以結束事件與代碼取代被截斷的步驟。狀態文字依 §5 的表（付款中、等待鏈上確認、已付款／已交付／交付失敗、未付款、待人工處理）；明細列出 tx#1、tx#2（區塊）、收據與內容 sha256（內容只交給 Host）。
  - 場景：新增 paying、confirming、paid、delivered、delivery_failed、not_paid、payment_review 階段與文字；回放時與 Unity 回報比對的改為送給 Unity 的階段。
  - `payment_capability`：有 `--payments` 時，`/healthz`、主持人狀態、pay task 與核准清單都回 `local_anvil`（模擬核准者照舊 `not_enabled`：模擬授權不能付款）。Allow 與本人核准後、付款事件出現前的文字改為「尚未付款」（無論之後是否真的開始付款都成立），等待核准時寫明「核准後由 Host 付款」；沒有 `--payments` 時與之前相同。
  - 測試：`web/test/p2.test.mjs`（6 個）、`TestPaymentCapabilityStated`；網頁測試 109 個全過。
- **H4c Unity 如實顯示付款（Aaron：「Unity 的文字不正確問題請優先處理好」）**：
  - Unity 新增九個階段：裁決 `allowed`（Allow）與 `approved`（本人核准），標籤是「not paid yet」，直到出現付款事件；以及付款的 `paying`、`confirming`、`paid`、`delivered`、`delivery_failed`、`not_paid`、`payment_review`，各有如實的英文標籤（例如「PAID AND DELIVERED (#10) - leaving with a receipt」）。沒有 `--payments` 時照舊是 `allowed_unpaid`「ALLOW - no payment capability」。
  - 位置：付款進行中、已付款待交付、交付失敗、待人工處理都留在櫃台；已交付與未付款離店。付款的每一步都有新的事件序號，但不算櫃台的新請購，不會重播挑選動畫；新的一筆請購照舊會。
  - 網頁直接送這些階段與它們自己的事件序號給 Unity（取代先前「Unity 停在裁決階段」的權宜作法）。訊息新增選填欄位 `payments`（`local_anvil` 或空，bridge 版本不變）；Unity 的標題列據此顯示「P2-a / local test payments」或照舊「M1 / no payment」。
  - 共用轉場向量新增 4 個付款案例（共 24 個），網頁與 Unity 的規則測試都用它。Unity：EditMode 18／18、PlayMode 28／28（新增 `PaymentPhasesAreKnownAndSayTheHostPays`、`APaymentWaitsAtTheCounterAndLeavesDelivered`）；網頁測試 109／109。Web build 已用 `scripts/unity/unity.sh webgl` 重建（git 不追蹤），建置內含新文字。
- 待做：對帳頁（H5，AE1 的 `Unauthorized`）。

### 12.5 改回 §3 的 Signer 握手（Aaron 指示）

- 實作見 §12.3 的「Signer 握手照 §3」。測試：`TestBeginPaymentWebAuthn`（記錄、重送、不能改用委派、重啟後的恢復檢查、改掉 `approval_id` 會被擋、grant 過期）、`TestBeginPaymentAfterTheTaskEnded`、`TestUpgradeSchema6`（P1-b 的 attempt 在重建後保留）、`TestPaySendsTheApprovalAndReservation`；付款工作的測試改為經由真的 Store `BeginPayment`；`TestPaidOnAnvil` 以 Store 為 Signer client 的帳本，真的 Signer 在鏈上確認預留後簽名。
- 突變：§3 新增 7 項；H2、H3 的相關項目改到新程式碼，移除 3 項只針對已刪除程式碼的項目。§3、H2、H3 共 61 項，61／61 KILLED。

#### Unity 與 §3 修正的 review（6445e26、83c7ca8）與處理

Claude Code 獨立 context 的 code-reviewer，結論 REQUEST CHANGES。審查者確認：網頁與 Unity 的規則、標籤與位置一致；`payments` 欄位無法被濫用；`payment_attempts` 的重建、CHECK、升級與版本化的恢復檢查都正確；`BeginPayment` 自己從 grant 讀核准、不能改用另一種依據；每張請購只會有一個 request ID。

| 等級 | 問題 | 處理 |
|---|---|---|
| Critical | 本人核准後付款的路徑上，Unity 整段停在 `approved`，每一步還被當成新請購重播挑選動畫，交付後也不離店 | 有付款事件時一律送付款階段；`p2.test.mjs` 新增「核准後付款」測試（每一步的階段、不是規則 2、不重播） |
| Important | Allow 在付款沒有開始時也顯示「the host pays」（task 已取消、開始付款被拒、以有 `--payments` 的 host 看舊 task） | 付款事件出現前一律「not paid yet／尚未付款」；測試涵蓋取消後的 Allow |
| Important | 已花 tx#1 之後，憑證狀態暫時讀不到（`CREDENTIAL_STATUS_UNAVAILABLE`）就轉人工處理 | 它與 `CLOCK_REGRESSION` 在期限前等待；撤銷、過期與 grant 到期照舊是最終的；`TestSignerStoreAnswers` |
| 建議 | Signer 的 403、400 被記成 unknown | 有代碼時回傳為拒絕、不改 attempt 的記錄（它們發生在 Signer 查詢請求之前，同一個 request ID 先前的送出可能已被接受）；`TestPayCallerNotAllowedIsARefusal` 涵蓋 403、400 與記錄不變 |
| 建議 | 結果不明且附上 Store 代碼時，被當成 Store 拒絕 | 先判斷 `Unknown`：一律等待 |
| 建議 | `call` 沒有 context，停止時最多等 10 秒 | 不改：與 P1-b 相同，Timeout 有上限 |
| 建議 | §3 仍寫「request ID 由 `intent_hash` 決定」 | 修正 |
| 建議 | schema 7 在分支內原地修改，早期分支建立的 schema 7 資料庫無法開啟 | 不改：var/ 沒有 schema 7 的資料庫（最高 6），且無法開啟是安全的失敗 |
| 建議 | 雙客人場景裡 M1 與 pay task 交錯時，標題列來回切換 | 版本 2：同一個場景內只要有一方的 host 付款就維持 |

第二輪 review（c968c50）：結論 REQUEST CHANGES（只剩一項）。審查者確認：Unity 在兩條路徑都收到付款階段、核准後的每一步都不是新請購；網頁與 Unity 的標籤一致；`sign()` 的順序讓最終拒絕維持最終、暫時的情況等待。處理：

| 等級 | 問題 | 處理 |
|---|---|---|
| Important | 有付款能力、付款尚未開始時，卡片的收尾步驟仍寫「未啟用付款」 | 改為「尚未付款；付款開始後見下列步驟」；測試確認沒有步驟寫「未啟用付款」 |
| 建議 | 文件把 403／400 寫成「記為拒絕」，實際上不寫入 | 文件改為「回傳為拒絕、不改 attempt 的記錄」 |
| 建議 | `CALLER_NOT_ALLOWED` 現在會立即結束已預留的付款（以前等到期限） | 保留；寫進 §7 |
| 建議 | 缺 `CLOCK_REGRESSION`、400 與「記錄不變」的測試 | 補上，並加突變 |
| 建議 | 場景清除後標題列仍顯示「P2-a」直到客人出現 | 場景重置時同步更新 |
| 建議 | 核准頁的說明「核准後由 Host 付款」在 task 先結束時不成立 | 改為「task 仍進行時，Host 會為核准的這一筆開始付款」 |

驗證（第二輪處理）：網頁測試 111／111；Unity EditMode 18／18、PlayMode 28／28，Web build 重建；新增 2 項突變，2／2 KILLED（§3、H2、H3 共 67 項）。

驗證（第一輪處理）：網頁測試 111／111；Unity EditMode 18／18、PlayMode 28／28，Web build 重建（建置內已無「the host pays」）；§3、H2、H3 突變 65／65 KILLED（新增 4 項）。

### 12.6 task 結束後的 Ask 不能核准（Aaron 決定）

- **規則**：task 結束後（CLI 逾時，或同一個 task 有請購被 Deny；取消原本就會撤回 Ask），原本開著的 Ask 不再由任何人決定，維持 pending，到期時照常記錄 `APPROVAL_EXPIRED`；不會有 `APPROVED`、`GRANT_ISSUED`，也就不會付款。依據是已接受案例 `cli-timeout-pending`（`docs/persistence-design.md` 的 `CLITimeout` 列：不撤回 Ask），不新增事件或原因代碼。
- **Store**：
  - `BeginApprovalAttempt` 拒絕（`TASK_ALREADY_ENDED`），不會向 Signer 要求挑戰；
  - `ResolveWebAuthn` 對結束前就開始的驗證回 `closed`、不記錄（demo 依既有流程請 Signer 放棄這個核准）；
  - `ResolveApproval`（模擬核准）與 `ResolveVoiceFailure` 也回 `closed`、不記錄；
  - 核准清單每一項新增 `task_ended`。
- **demo 與網頁**：挑戰請求回 409 `task_ended`；主持人頁的模擬核准與本人核准面板不顯示核准按鈕，改寫「task 已結束：這筆不能核准」與原因；核准結果的文字在有 `--payments` 時寫「尚未付款」，不再寫「付款未啟用」；本人核准面板的商品列在有 `--payments` 時不再寫「本片不付款」。
- **測試**：`TestEndedTaskAskIsNotDecided`（CLI 逾時與另一張請購被 Deny 兩種：新的核准被拒、進行中的本人核准與模擬核准都不記錄、之後照常到期）、`TestA2EndedTaskAskIsNotApprovable`（demo：409、進行中的驗證不採用、清單標示、沒有 grant）、網頁測試 113 個。
