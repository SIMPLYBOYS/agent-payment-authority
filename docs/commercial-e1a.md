# E1-a 端到端部分整合：設計提案

> 2026-10-21，Claude Code。依 vault〈Claude-Code-E1a端到端部分整合派工-20261013〉、roadmap §5.4 E1 與 §7。分支 `commercial-e1a`（從 main `c8d3759`）。
> 前置：L2、P1、Z2、P2 都已 Done（P2 Done 是 Aaron 10/21 的決定）。本文只是提案。沒有改程式，沒有執行真實模型，沒有連鏈，也沒有動 `/opt`。§5 的決定、§6 的每一次執行都要 Aaron 另行核准。
> E1-a 完成後只能稱為「**部分整合**」。不使用「最終商業 demo 完成」或「end-to-end verified」的字樣。E1-b 另外派工。

## 1 要做到什麼

讓**一筆**請購在同一個 `intent_hash` 下，從頭到尾都可以查到：
- 委託
- 真實模型的請購
- Policy 裁決
- 憑證
- 本人核准
- 語音
- ZK 預留 tx#1
- Signer 簽章
- 結算 tx#2
- 對帳
- 交付與收據

畫面（Unity、Inspector、Timeline）與唯讀 checker 讀同一份白名單事件。checker 對每一項輸出 PASS、FAIL 或 INCONCLUSIVE。E1-a 範圍外的項目標 `not_evaluated`，不當成通過。

派工要求的驗收：
- 從乾淨資料庫跑成功 Ask、Hard Deny，以及至少一條攻擊或並行反例。
- 每條都能由 task 或 intent 查到授權與事件，A／B 的身分與金額不混用。
- 延遲、遺失、重複的事件，回覆未知、訂單重送、重新載入，都不重複付款、不提前交付；缺少證據時 checker 要指得出來。
- M1 舊證據包維持唯讀相容，不重算成新版本。

## 2 現況：每一環和 `intent_hash` 的關係

| 環節 | 在哪裡產生 | 以 `intent_hash` 串起？ | 畫面或 API 看得到嗎 |
|---|---|---|---|
| 真實模型請購 | live Buyer → bridge `request_purchase` | 是（`PURCHASE_PROPOSED`；Store 依訂單重算 intent，`internal/paycontract/events.go:116-118`） | 是；但 **live 不能付款**（§3.1） |
| BBS+ 憑證（C2） | `internal/store/credential.go:239-300`；nonce 綁 verifier、challenge、intent（`internal/credential/credential.go:154-163`） | 是 | 卡片有結果與 `proof_sha256`；**P2-c 用 `apa.pay.v1`，鏈上那幾筆沒有憑證** |
| Policy 裁決 | `store/pay.go:439` | 是（`decision.intent_hash` 與事件 scope） | 是 |
| 本人核准（A2） | Signer `approval_challenges`、`human_approvals`；Store `ResolveWebAuthn`（`store/approvals.go:362-470`） | 是（Signer 的列、grant 內容、Store 都比對） | grant 的 jti 與方法看得到；Signer 的 approval_id 看不到 |
| 語音（V1） | Signer `voice_results`（`signer/voice.go:125-140`） | **間接**：以 challenge 連到核准，再連到 intent | 事件只有結果代碼；分數與音訊雜湊留在 Signer |
| ZK 預留 tx#1（Z2） | relayer `reservations`（主鍵就是 intent_hash，`internal/budget/relayer.go:75`）；public input 含 intent 的高低兩半（`zk/circuit.go:49-56`） | 是 | 卡片有 tx#1 的 hash；**proof、public input、nullifier 都沒有進 Store 或 API** |
| Signer 簽章 | `signer/core.go:272-391`（重算 intent、核對預留、`attempts.intent_hash UNIQUE`） | 是 | 事件只寫方法，不含簽章 |
| 結算 tx#2 | `store/paid.go` PaymentSettled | 是 | 卡片有 tx#2 與區塊 |
| 對帳 | `internal/reconcile/reconcile.go`；以 EIP-3009 nonce 比對 | **不以 intent 為鍵**：報告是錢包層級，不寫事件 | Host 的對帳列 |
| 交付與收據 | `store/paid.go`；`receipts.intent_hash UNIQUE`，`ReceiptID = H(intent)` | 是 | 卡片有收據與內容雜湊 |

**一定要修的四個問題**：
1. **證據模式全標成 fixture**：Store 寫的每一個事件都固定是 `evidence_mode:"fixture", source:"host"`（`internal/store/ops.go:222`），連 Base Sepolia 上的真實付款也一樣。這違反 §7 的「模式誠實」。
2. **事件 API 丟掉付款欄位**：`GET /api/tasks/{id}/events` 回應時拿掉了 `payload.payment`（`cmd/demo/api.go:279-328`），Timeline 的付款事件看不到 tx hash。整個程式也沒有任何 explorer 連結。
3. **付款主機上還有舊文字**：證據抽屜寫「grant 不能取得付款能力；沒有付款或交付」（`web/adapter.js:849`）、「（本人核准；付款未啟用，不能付款）」（`web/adapter.js:865`）。
4. **live Buyer 的提示詞**寫「You cannot pay and will not receive the content」（`internal/buyer/host.go:62-63`），在付款主機上是錯的。

## 3 缺口：真實模型與付款還不能在同一次執行

### 3.1 現在擋在哪裡

1. **`scripts/demo/present.sh`**：
   - 付款的 Signer、registry、憑證都只給 fake 模式（:70、:75、:77）。
   - live 分支寫死 `scenarios-live.json` 與 `registry.json`，不傳 `--profile`、`--payments`、`--approvals-signer`（:128-134）。
2. **`cmd/demo/main.go:190-193`**：`--approvals-signer` 只限 fake，而 `--payments` 又需要 `--approvals-signer`，所以 live 不能付款。
3. **`cmd/demo/scenarios.go:109-113`**：pay 類情境在 live 會被拒（「a pay-family entry is fake only」）。live 情境也不能指定腳本檔（:123-124），所以目前沒有 live 的付款情境。
4. **委託只能模擬確認**：
   - pay 類 task 需要一個已確認的委託（`cmd/demo/control.go:270-273`），而確認委託只能走 `POST /api/sim/delegations`。
   - live 模式不建立模擬者，這條路由回 `simulation_unavailable`（`cmd/demo/sim.go:114-117`）。
   - 這是**已核准的界線**，不是疏漏：C2 的 D9 (b)（`docs/commercial-c2-design.md` D9）與交易流程設計（`docs/commercial-transaction-flow.md` §live 怎麼拒絕）。
5. **畫面標籤**：live 的固定標籤寫「合成資料・不付款」（`cmd/demo/live.go:16`）；`sources()` 永遠回報 `Agent: "fake"`（`cmd/demo/approvals.go:81`）。

請購之後的路徑（bridge、Store、付款工作、Signer、A2 核准、tx#1、tx#2）**本來就不分 fake 或 live**。所以 E1-a 只是接線，加上 §5 的幾個決定，不是另寫一條付款路徑。

### 3.2 checker

- 現在的 checker（`cmd/verify-demo`、`internal/verification`，`make verify-m1`）只認 M1 的證據包（`apa.m1.v2`）。M1 以外的事件，或付款狀態不是 `unavailable`，都判 FAIL（V5）。
- 七項能力（憑證、WebAuthn、語音、ZK、Signer 選鑰、跨客人 grant、鏈上結算）都必須列為 not_evaluated，不得宣稱。
- 沒有 profile 登記：fixture 與 live 是寫死的兩個分支（`internal/verification/verify.go:102-125`）。
- 可以直接當輸入的現成檔案：
  - 對帳報告：`apa.reconciliation.v1`，嚴格解碼。
  - AE1 的 `report.json`。

## 4 設計

### 4.1 E1a-1 證據模式與來源（契約變更，要 review）

- 每個新事件的 `evidence_mode` 與 `source` 依它**實際的來源**填寫（依 §5 E1a-D4）。
- 舊事件不改，M1 舊包不重算。
- 事件 API 改為帶出付款欄位：方法、金額、tx、區塊、內容雜湊、收據。這些都在白名單內，不含簽章與 grant 全文。

### 4.2 E1a-2 live 與付款接線

1. 拿掉 main.go 與 scenarios.go 的「pay 只限 fake」。改成：live 加 pay 時，必須同時有 Signer、payments、registry、profile，缺任何一項就拒絕啟動。
2. 新增 `testdata/demo/scenarios-e1a.json`：從 `scenarios-testnet.json` 的 `data-a`／`data-b` 改成 live 版，不指定腳本檔；合約依 §5 E1a-D2。
3. `present.sh` 的 live 分支傳入 Signer、payments、registry、profile。資料庫照 fake 的作法依 Signer 分開保存，避免 `SESSION_DECIDED`。新增 `make present-e1a`。
4. 委託的確認依 §5 E1a-D1。
5. 提示詞依 §5 E1a-D6。
6. 改正 `liveLabel`、`sources().Agent`，以及 §2 第 3 點的舊文字。

### 4.3 E1a-3 `apa.pay.v2` 加付款

- 依 §5 E1a-D2，讓 v2 的憑證走進付款。
- C2 的 D8 (a) 已讓 Signer、A2、委託、grant 支援 v2，但 **v2 加上 tx#1、tx#2 還沒有實跑過**。
- 先在本機 anvil 以 fake 跑通：憑證 → 裁決 → 核准 → tx#1 → 簽章 → tx#2 → 交付。補上缺的測試。
- 憑證的 issuer 是測試金鑰，在畫面與證據中照寫。

### 4.4 E1a-4 畫面

**Inspector 與 Timeline 增加**：
- tx#1、tx#2 的完整 hash、區塊、鏈 ID、合約，以及 explorer 連結（Base Sepolia 用 `https://sepolia.basescan.org/tx/…`；本機 anvil 不給連結，照寫「本機鏈」）。
- ZK 預留：顯示預留證據（鏈、合約、task、intent、金額、tx#1）。proof 本身不顯示，寫明「由鏈上 verifier 合約在 tx#1 驗證」。tx#1 成功就代表合約驗過；畫面不另外宣稱「已驗證」。
- grant：方法、jti、期限，以及語音的結果。
- 憑證：結果與 `proof_sha256`。
- 每一列的 `evidence_mode` 與來源。

**Unity**：付款階段已由實際付款狀態推進（P2），E1-a 不改 Unity。

**所有新欄位**：只出現在白名單 API，前端不自行推算。

### 4.5 E1a-5 證據包與 checker 的 `e1a` profile

**證據包產生器 `cmd/e1-evidence`**：
- 以唯讀方式讀 Store。
- 包進：對帳報告、AE1 `report.json` 的雜湊、各環節的白名單證據、tx#1 與 tx#2 的 hash。
- 掃描祕密的方式沿用 M1。
- 每次輸出到新的 `artifacts/e1a/<run-id>/`。

**`verify-demo --mode e1a`**：新增一個分支，有自己的 run ID 格式、版本鍵、來源表與 claim 集合。checker 不呼叫被測的 Policy、Signer 或 Store。

| claim | 檢查什麼 | 資料不足時 |
|---|---|---|
| E1 來源與雜湊 | 索引、檔案雜湊、run-end | FAIL |
| E2 同一 intent | 請購、裁決、憑證、核准、預留、簽章、結算、交付、收據都對到同一個 `intent_hash` | INCONCLUSIVE |
| E3 事件鏈 | hash chain、順序；交付不早於結算 | FAIL |
| E4 兩筆交易 | tx#1 與 tx#2 不同；依 §5 E1a-D5 向鏈唯讀查 receipt（鏈 ID、合約、成功、區塊確認數） | INCONCLUSIVE |
| E5 未知不重付 | 未結算或 needs_review 時沒有交付，同一 intent 只有一組 tx#1／tx#2 | FAIL |
| E6 模式誠實 | 每個事件的 mode 與來源符合 E1a-D4 的對照表；live 情境的請購來自 live trace | FAIL |
| E7 A／B 不混用 | 身分、錢包、委託、grant 各自獨立 | FAIL |
| E8 未授權新增支出 | 對帳報告 0 筆，區塊範圍涵蓋這些 tx | INCONCLUSIVE |
| E9 證據安全 | 只有白名單欄位；祕密掃描通過 | FAIL |
| 不在 E1-a 範圍 | checker 自行重驗 BBS+ proof 與 ZK proof（proof 本身沒有匯出）、語音 FAR／FRR、Admin 聚合 | `not_evaluated`，留給 E1-b |

每個 claim 都要有一個正例，以及一個「重算內外層雜湊後仍會被語意檢查抓到」的單項反例，作法和 M1 相同。

### 4.6 E1a-6 異常情況

| 情況 | 現有測試 | E1-a 要補的 |
|---|---|---|
| 延遲、遺失、重複事件 | 待查：P2 的付款工作與 Store 可能已有一部分 | 逐項列出測試名稱，缺的補上 |
| 回覆未知 | 待查（同上） | 同上 |
| 訂單重送 | 待查（同上） | 同上 |
| 頁面重新載入 | 待查（同上） | 同上 |
| live 加付款 | 無 | 補測試 |
| v2 加付款 | 無 | 補測試 |
| checker 反例 | 無 | 補測試 |

## 5 要你決定的事

| # | 問題 | 選項 | 建議 |
|---|---|---|---|
| E1a-D1 | live 模式下**委託由誰確認**（會改到 C2 D9 (b) 與交易流程設計的「live 拒絕模擬」界線） | **A** live 也開放「主持人扮演客人確認委託」這一條路由，其他模擬路由仍然拒絕：模擬核准 Ask 在 live 照樣拒絕，`simulated` grant 照樣不能付款。畫面與事件寫明「委託是模擬」。<br>**B** 新設計「本人以已登記的裝置確認委託」（WebAuthn），是新的流程與契約。 | **A**：和 P2-c 在 Base Sepolia 上的作法相同（你已接受）。E1-a 的重點是真實模型加真實付款。checker 把「委託由本人確認」列為 `not_evaluated`；B 要不要做，留給 E1-b 之前由你決定。 |
| E1a-D2 | E1-a 的實跑用哪個合約 | **A** `apa.pay.v2`：BBS+ 憑證進入同一條鏈（會放寬 D9 (b) 的「v2 只限 fake」）。<br>**B** `apa.pay.v1`：和 P2-c 相同，憑證標 `not_evaluated`。 | **A**：E1 的目標正是同一個 intent 從憑證串到交付。v1 少了憑證這一環，E1-b 還要重跑。issuer 是測試金鑰，照寫。 |
| E1a-D3 | 實跑哪幾場、在哪條鏈 | 先在**本機 anvil** 以 fake 跑通全部（不花測試幣），再在 **Base Sepolia** 以**真實模型**跑三場：<br>① 成功 Ask：請購、本人核准（裝置加語音）、付款、交付。<br>② Hard Deny：請購超出委託或政策外的商品，被 Deny。<br>③ 攻擊反例：AE1 語料中的一個注入案例（建議 `i-bypass-approval`，被推銷的商品在 `allow_max` 內），看鏈上有沒有被騙的付款。 | 照這樣。③ 同時補上一部分 AE1 D6「注入案例在 testnet 上付款後對帳」的差距（§18 記錄過）。每一場開始時都會送一筆登記 tx#0。只有成功的那一場會有 tx#1 與 tx#2；② 與 ③ 預期只有 tx#0。每一場各自核准。 |
| E1a-D4 | `evidence_mode` 與 `source` 怎麼填 | 依實際來源：<br>• live CLI 的請購：`live`／`live_cli`。<br>• fake Agent：`fixture`／`fake_cli`。<br>• Base Sepolia 上的預留、結算、交付：`live`／`base_sepolia`。<br>• 本機 anvil：`fixture`／`local_anvil`。<br>• 實機 WebAuthn 的核准：`live`／`signer_webauthn`；virtual authenticator：`fixture`。<br>• Store 自己產生的事件（task 開始、裁決）：跟著這個 run 的 Agent 模式。<br>• `replay` 只用在回放。 | 照這樣。本機 anvil 雖然是真的執行，但不是公開鏈，所以不寫成 live；P2-a 的紀錄也是這樣區分。 |
| E1a-D5 | checker 要不要自己讀鏈 | **A** 給 `--rpc` 時，以唯讀方式依 tx hash 向鏈查 receipt；沒給就把鏈上的 claim 判 INCONCLUSIVE。<br>**B** 只看證據包裡產生器匯出的 receipt。 | **A**：鏈不是被測系統。只信產生器匯出的 receipt，等於信被測系統自己說的話，這是 §7「自報成功不算」。RPC 用公開的 `https://sepolia.base.org`，和 P2-c 相同。 |
| E1a-D6 | 付款主機上 live Buyer 的提示詞 | 不付款的主機與 AE1 沿用現在的提示詞（AE1 的結果綁著它的 SHA）。付款主機另用一份，只改兩點：<br>• 拿掉「You cannot pay and will not receive the content」，改為「A purchase you request may be paid by the host after its policy and, when asked, the owner's approval; you never pay directly」。<br>• 商品名稱依情境。 | 照這樣。提示詞的 SHA 寫進證據包。實跑前你先看過全文。 |
| E1a-D7 | M-D Admin 營運總覽的時機 | 和 E1-a 同批，或 E1-a 之後 | **之後**：M-D 要先決定 Admin 的角色、token 與資料範圍。E1-a 的事件與 API 定案後再提案，避免改兩次。 |

## 6 執行清單（核准後才做；每一步各自核准）

| 步驟 | 誰 | 碰到什麼 | 交易 |
|---|---|---|---|
| 1 實作 E1a-1～E1a-6 | Claude Code | 本機；本機 anvil 用公開的測試金鑰 | 0（本機鏈） |
| 2 code review | Claude Code 的 code-reviewer | 本機 | 0 |
| 3 本機彩排 | Claude Code（fake）；live 那幾場由你執行 CLI | 本機 anvil。live 會用到 Claude 的 token | 0（本機鏈） |
| 4 Base Sepolia 實跑 ①②③ | 你：執行 CLI、核准或拒絕、輸入密語；`/opt` 的 Signer 與語音服務由你以 sudo 啟動 | RPC、你的裝置 | 約 3 筆 tx#0，加上 ① 的 tx#1、tx#2 |
| 5 對帳、證據包、checker | Claude Code（RPC 唯讀） | RPC | 0 |
| 6 結果與 review | Claude Code | 本機 | 0 |

**實跑前要你確認**：
- `claude --version` 是 `2.1.301 (Claude Code)`（`internal/buyer/live.go:21`）。
- Signer 的每日上限與委託總額還夠：P2b-D5 的 `dlg-ta`、`dlg-tb` 每筆 0.10、總額 0.30；relayer 每日 12 筆、商店每日 4 筆。
- 測試幣餘額。

## 7 界線

- 不使用主網、真錢或日常錢包。所有金鑰都是 P2-b 建立的測試金鑰。密語不放在參數、環境變數、檔案或對話裡。
- Claude Code 不執行真實 Claude CLI，不讀個人 CLI 設定，不用 sudo，不更動 `/opt`。
- virtual authenticator 的結果不寫成實機結果；本機 anvil 的結果不寫成 testnet 結果。
- E1-a 的結果只稱「部分整合」。範圍外的 claim 一律 `not_evaluated`。
- 不做 Admin 聚合（M-D），也不做 E1-b 的最終驗收。
