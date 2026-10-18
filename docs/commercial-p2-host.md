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
  ① 建 payments 列（intent_hash 為鍵，requested）
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
  - `BUDGET_RESERVED`、`PAYMENT_SIGNED`、`PAYMENT_SUBMITTED`
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
