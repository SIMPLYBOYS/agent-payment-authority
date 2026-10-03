# M1 邊界

M1 的終點是 **請購與 Ask／Deny**。M1 沒有 Signer、沒有結算、沒有鏈上交易，不取貨，也不產生基於未交付資料的摘要。M1 通過不等於付款、Signer 隔離、WebAuthn、憑證或 ZK 已驗收。

這一頁描述 M1 要守住的邊界。各項是否已實作，以最後一節的狀態表為準。

## 四條邊界

| 邊界 | 意思 | 不能因此宣稱 |
|---|---|---|
| **CLI 不可信** | Claude Code 與它的輸出視為可能被 prompt injection 完全控制。模型說「已付款」「已核准」不會變成事件；報價來自商家，模型不能改寫報價、額度設定或事件來源 | 模型沒被騙就代表系統安全 |
| **沒有付款或 grant 私鑰** | M1 不建立、不載入、不保存任何付款私鑰或 grant 簽章金鑰；設定檔與環境變數沒有放金鑰的欄位 | 完整的 Signer 隔離已驗收。M1 只能證明「沒有注入、沒有暴露」 |
| **工具入口受限** | 模型只能用三個入口：讀商品目錄、對受限商品發請求、提出請購。輸入先過 schema 驗證；任意檔案、任意 URL、shell 參數與未列出的工具一律拒絕 | 解析一句自然語言就等於授權 API |
| **Ask 不等於核准** | Ask 表示「等待真人核准」，狀態是 pending。不扣預算、不發 grant、不進 Approved。M1 沒有核准功能，畫面不放會假裝完成付款的按鈕 | Ask 之後付款會成功 |

補充規則：

- Deny 優先於 Ask，Ask 優先於 Allow。硬性 Deny 不能被真人覆蓋。
- 沒有核准人在場的 unattended 設定下，Ask 視為 Deny。
- 整合入口若得到 Allow，回報 `PAYMENT_CAPABILITY_UNAVAILABLE`，不產生 policy grant，也不演付款成功。
- 服務只綁 loopback，但 loopback 不是免認證的理由；展示 endpoint 的認證隨展示層一起實作。

## 目前的 fixture

下列資料都是版本化的測試資料，事件上必須標 `evidence_mode=fixture`，任務整體顯示「部分能力 fixture；未付款」。

| 項目 | fixture 內容 | 不代表 |
|---|---|---|
| 商品報價 | KRW 700，結算要求 0.50 test USDC | 即時報價 |
| 匯率 | 1 test USDC = KRW 1,400 | 即時 FX 或真實換匯 |
| Policy demo profile | 單筆 Allow 上限 0.10 test USDC、task cap 5 test USDC；主商品落在 Ask | 已定案的 `allow_max` |
| Deny 案例 | 有效剩餘額度 0.25（請購 0.50）、商家不在白名單、授權條件已過期、缺少必要設定 | 鏈上額度檢查 |
| 授權條件 | 明標 fixture 的授權條件 | BBS+ 憑證已驗證 |
| 消費者與錢包 | Consumer A／B 各自不同的 wallet reference 與 `from`，商家另有 `payTo` | 已建立錢包、持有金鑰、有鏈上資產 |
| 餘額 | fixture 或 unknown，與 task 額度分開顯示 | live 餘額 |
| CLI | fake CLI（`cmd/fake-claude`）：真子程序與真 MCP 橋接，但不跑模型，stream-json 依文件形狀產生 | live CLI 已驗收、真實模型能完成請購 |

真實的部分另外標示：真實模型（僅在明確 opt-in 的 live CLI 執行時）與真實的本機 HTTP。

## 誰可以啟動 task

- 只有在本機操作的主持人可以啟動 task，透過這個 repo 自己的入口。
- 模型、CLI 輸出與前端不能建立 task，也不能改寫 consumer／agent／task／wallet 的對應；對應來自主持人建立的可信設定。
- 一次只有一個 active task。Consumer A 與 B 依序操作。
- live CLI 必須明確啟用，並限制任務數、最大輪次與執行時間；失敗不自動重跑。

## 取消與重啟的預期行為

| 情況 | 預期 |
|---|---|
| 主持人取消 task | task 進入獨立的取消狀態並留下事件；不產生 Approved、Settled 或交付事件 |
| CLI 逾時、中止、限流、未登入 | 顯示為 task 層的錯誤狀態；不自動重跑，不改變請購的裁決結果 |
| CLI 結束 | 不會把請購改成 Approved 或 Settled |
| Ask 超過等待上限 | 進入 Denied 並記錄逾時原因；不扣預算 |
| 服務重啟 | 由持久化事件還原同一筆請購與 task 狀態；同一消費者沿用原本的 wallet reference |
| 頁面重新整理 | 由持久化狀態還原；已播過的事件不重複演出 |
| 相同請購重試 | 不新增第二筆邏輯請購 |
| 相同 key 但內容不同 | 拒絕 |
| 停止服務 | 只回收自己啟動的子行程；不尋找、不終止其他行程 |
| port 被占用 | 回報錯誤後結束；不終止占用者 |

事件名稱、狀態欄位與重試 key 的範圍由共用資料契約（B1-02）固定，這裡只描述行為。

## M1 不評估的能力

下列項目在 M1 的驗收結果中列為 `not_evaluated`，不因為「根本沒接付款」而算通過：憑證驗證、WebAuthn、語音核准、ZK 額度證明、Signer 與金鑰選擇、跨消費者的 grant／proof、鏈上付款與對帳。

## 實作狀態

| 項目 | 狀態 |
|---|---|
| 服務只綁 loopback IP，啟動後再核對實際綁定位址 | 已實作，有測試 |
| port 衝突時回報並結束，不終止占用者 | 已實作，有測試 |
| live CLI 預設關閉；設為開啟會拒絕啟動 | 已實作，有測試 |
| 沒有付款能力；設定沒有金鑰欄位 | 已實作（health 回報 `unavailable`） |
| 停止時釋放 port，可立即再啟動 | 已實作，有測試。目前沒有子行程 |
| `.env` 有不支援的行就不啟動，不使用部分載入的設定 | 已實作，由 `scripts/verify-b1-01.sh` 檢查 |
| 商店目錄與 402；任何 `PAYMENT-SIGNATURE` 都不交付受保護資料；只陳述設定的 URL、不抓取外部 URL | 已實作，有測試（B1-03，10/1 於 `ebd3f44` 結案；見 [merchant.md](merchant.md)） |
| 確定性 Policy：Ask／Deny／Allow-unavailable，不扣預算、不發 grant、不付款 | 已實作，有測試（B1-05，10/2 結案，Policy 驗收基準 `61b3652`；見 [policy.md](policy.md)） |
| 請購持久化與去重、事件 hash chain、取消／CLI 逾時／approval 到期、重啟恢復與驗證、A／B 隔離（Store 層，以 session 限制的查詢） | 已實作，有測試（B1-05，10/2 結案，驗收基準 `cb33768`；尚未接到 `cmd/demo`，接線與設定載入屬後續整合；見 [persistence-design.md](persistence-design.md)） |
| Buyer wrapper fake 模式（B1-04 首個交付點）：受限 MCP 工具、capability 綁定 run、402 嚴格檢查、Host 組裝請購寫入 Store、程序群組回收與輸出上限 | 已實作，有測試；**`b3c66f6` 通過 Codex 獨立技術 review**（10/3；見 [cli-poc.md](cli-poc.md) 第 10 節）。B1-04 整體仍 InProgress |
| Store 接線（fake 命令）：設定來源、Store 開檔、RunExpiry 啟停、關閉順序 | 已實作於 `cmd/apa-buyer`，有測試；`cmd/demo` 尚未接線（B1-06） |
| live 開關與 init 閘門（階段 A）：`--live`＋`APA_LIVE_CLI=true`、絕對路徑與核准版本、init 檢查通過前不開 Store、assistant 訊息與時間上限 | 已實作，以 fake 測試；**`e8f6719` 通過 Codex 獨立技術 review**（10/3；[cli-poc.md](cli-poc.md) 第 14 節）。不代表真實 CLI 的登入、模型或計費已驗證 |
| C0 init 探測（`probe-init`）：零輸入、零業務執行、零 Store 存取、白名單輸出 | 已實作，以 fake 測試；**`5f34a7b` 通過 Codex 獨立技術 review**（10/4）；**未對真實 CLI 執行**，不代表真實 CLI 行為、登入方式或用量已驗證（[cli-poc.md](cli-poc.md) 第 17–20 節） |
| live Claude Code 執行（階段 B／C） | 未執行、未驗證，沒有任何真實推論或登入檢查；live 認證接受清單為空，任何 live run 都會停在閘門 |
| 主持人事件檢視 HTTP 端點與展示層（B1-06）、驗收器（B1-07） | 尚未實作 |
