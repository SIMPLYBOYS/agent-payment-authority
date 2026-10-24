# 落差批次 3：兩位客人一起購物並付款

> 2026-10-24，Claude Code。範圍是 `docs/commercial-gap-review.md` 的 A2、A3（G5 選 A：兩位客人同時購物，付款依序處理，畫面照實說明）。
> Aaron 10/24 的決定：**情境**：資料研究室，兩位客人都買 0.50 的簡報，各自要本人用裝置核准；**一起開始**：一鍵依序完成。

## 1 現況

- 本機（`scenarios-p2.json`）與 testnet（`scenarios-testnet.json`）的目錄本來就有客人 B 的資料與咖啡情境；同時執行上限是預設的 2。
- 缺的是主持人頁：
  - 「一起開始」原本排除付款情境。
  - 核准面板只顯示選取的客人，另一位客人的請購在等，畫面上看不出來。
  - 本機 Signer（`var/p2-dev-e1a`）的設定裡有 approver-b，但只登記了 approver-a 的裝置。

## 2 做法

- **一起開始**（`web/host.js` startAll、asCustomer）：付款情境按一次就依序處理每位客人：主持人扮演該客人，確認委託（模擬，和「確認委託並開始（模擬）」相同），再開始該客人的場次。
  - 每位客人各自送出，結果逐一顯示；委託沒確認的客人不會開始。
  - 進行中，卡片上的角色模擬按鈕先停用，避免中途換人讓確認作廢。
- **核准面板**（A2，本人用裝置核准）：以每位客人自己的 viewer token 讀其他客人的待核准。面板上方列出「客人 B 也有一筆請購等你核准：…」，旁邊有「切換到客人 B」。
  - 這一行只列出、可切換，不能在這裡核准；核准仍在該客人自己的面板做，綁定的客人與 challenge 規則不變。
- **付款依序**（`web/adapter.js` showQueue，展示視窗）：
  - 有兩位以上客人在購物、而且這場有付款時，客人卡片下方寫「客人同時選購；付款由 Host 依序處理，一次處理一筆；一筆等鏈上確認太久時，會先處理下一筆。」
  - 一位客人的付款在進行，另一位已核准、還沒開始付款時，後者的付款步驟寫「排在前一筆付款之後（Host 依序處理付款）」，不寫「準備付款」。
  - 依據（review 2）：付款工作（`internal/payflow` 的 Run）是一個迴圈，自己等每一筆的鏈上確認（預留最多 2 分鐘、商店結算約 20～30 秒），等不到才換下一筆；新的付款在每一輪開頭才開始（`Before`）。所以另一筆在進行時，還在「準備付款」的那一筆確實排在後面。

## 3 Aaron 要做的事

1. 本機 Signer 替 approver-b 登記裝置：`make demo-local` 執行中，另開終端機照 `docs/commercial-p2-host.md` 12.8 的第 8～10 步做，但改成 `--dir var/p2-dev-e1a/data --approver approver-b`。可以用同一台裝置。
2. 本機彩排：資料研究室按「一起開始」，A、B 各自核准，看展示視窗的付款依序說明。
3. testnet 之後再做：approver-b 在 `/opt` 的裝置與聲紋、B 的測試幣與每日上限（逐場核准）。

### 3.1 testnet（Aaron 10/24：「先補完批次 3 的 testnet」）

10/24 讀鏈（Base Sepolia，唯讀）：wallet-a 18.91、wallet-b 19.92 test USDC；relayer-gas 0.0000910、shop（`0x6bbe…773e`）0.0000978 ETH，都夠。

- approver-b 的裝置在 A2（10/13）登記到 `/opt`（credential `663f78ce86cf48fc`，`docs/commercial-a2.md` 13.6），但**沒有聲紋**；`/opt` 的核准要語音，沒登記會回 `VOICE_NOT_ENROLLED`。聲紋存在 `signer.db`，Signer 每次核准時才讀，所以可以在 `make demo-testnet` 執行中登記（語音服務在第 1 步就緒後）。
- Host 資料庫沿用 `var/p2-testnet/present/a2-host-*`，task 編號接著用，不會撞到 Signer 已決定過的 `show-b-01`、`show-b-02`。
- 上限：approver-b 每筆 1.00、24 小時 1.50（0.50 可以）；relayer 每日 12 筆、商店每日 4 筆。一場會送 6 筆交易：登記 2（每位客人的 task 一筆）、預留 2、結算 2。
- 兩位核准人實際上是同一個人（Aaron）、同一台裝置、同一個聲音；展示時照實說明（`docs/commercial-gap-review.md` A5）。

步驟：
1. `make demo-testnet`，照提示輸入 sudo、relayer 與錢包的密語。
2. 第 1 步「語音服務就緒」之後，另開終端機：
   - `cd /; sudo -u apa-signer /opt/apa-signer/bin/apa-signer approver list --dir /opt/apa-signer/data`：approver-b 應是 active。
   - 照 `docs/commercial-v1.md` §15「V」錄 3 段並登記，把 `--approver approver-a` 改成 `--approver approver-b`；應看到 `voice template of approver-b from 3 recordings`。錄音照步驟立刻刪除。
3. Host 就緒、頁面打開後：選資料研究室，按「一起開始」，A、B 各自用裝置＋語音核准。
4. 結束後跑對帳（`p2-reconcile.sh`，同 P2-c 第 8 步）。

### 3.2 第一次 testnet 實跑（Aaron 10/24，台北時間約 02:00＝UTC 10/23 18:00）

- 一起開始：A、B 都開始；A 以裝置＋語音核准通過（`grant-c199985bc216…`），付款停在「未付款：預算預留失敗，沒有花錢」（`show-a-09`，RESERVE_FAILED）。
- 原因（讀鏈，唯讀）：relayer 的每日上限（P2b-D5：12 筆）以 UTC 日計，而且算 relayer 管的所有金鑰：UTC 10/23 relayer-gas 送了 2 筆、issuer 送了 10 筆（每開一個付款場次就在鏈上登記一筆 task，不論之後有沒有付款），合計 12，所以這筆預留在送出交易前就被拒（`RELAYER_DAILY_CAP`），沒有送出任何交易，沒有花錢。商店今天 2 筆（上限 4）。
- 修正：Host 被 relayer 拒絕預留時，把拒絕原因印在終端機（payflow reserve）；之前只有 RESERVE_FAILED，原因只能從鏈上推算。
- 下一次：UTC 午夜（台北 08:00）後計數歸零。一場兩位客人用 relayer 4 筆（登記 2、預留 2）、商店 2 筆；開場前不要多開沒有要付款的場次。
- 之後：`make demo-testnet` 在 relayer 就緒後先檢查今天的筆數與餘額，不夠就停下（`docs/commercial-ops.md` O3 開場前檢查）。

### 3.3 第二次 testnet 實跑（Aaron 10/24，台北 08:00 後）

- 一起開始：A 核准（裝置＋語音）後付款、上鏈成功；**B 的 Ask 過期**。
- 原因：兩位客人的請購幾乎同時送出，核准期限（120 秒）同時開始倒數；A 的裝置＋語音核准約一分鐘，B 剩下的時間不夠。
- 修正：資料研究室的 data-a、data-b 核准期限改為 180 秒（`scenarios-testnet.json`、`scenarios-p2.json`）。上限的依據：付款授權在報價後 300 秒到期（商店的 maxTimeoutSeconds），Host 在到期前 30 秒不再開始付款；180 秒核准完仍留約 85 秒讓排在後面的那一筆付款開始。Signer 不另外限制 Ask 等待的時間。手冊第 4 節改為「A 一核准完就馬上核准 B，不要等 A 的付款完成」。
- 要重新啟動 Host 才會用新的期限。

## 4 已知、不在本批

- 非 A2 的模擬核准（`AUTH` 沒有 Signer 的 fake 主持人頁）仍一次只扮演一位客人：一起開始後角色模擬停在最後一位，另一位要按「以 A 模擬」再核准。
- 主持人頁的客人卡片不顯示付款排隊；只有展示視窗說明。
- 真正同時付款（A4）不在本批。

## 5 進度

- 實作：一起開始、核准面板的「也有一筆等你核准」、展示視窗的付款依序說明。
- 測試：網頁單元測試全部通過（show 18，含付款依序）；`scripts/verify/a2-ui.mjs` 新增第 8 段（一起開始兩位客人的付款情境、A 的面板列出 B 的請購並可切換、A 與 B 各自用裝置核准），全部通過（CDP virtual authenticator，不是實體裝置結果，不付款）。
- review：三輪（2fad463 → 21f49e9 → f6282cc），第三輪 APPROVE；建議另補 677c29d（交付中也算進行中）。
- Aaron 10/24 本機彩排（`make demo-local`，fake Agent，本機 anvil，實體裝置）：本機 Signer 替 approver-b 登記裝置後，資料研究室按「一起開始」，A、B 各自核准、付款依序完成，展示視窗的說明與第 2 節描述「完全一致」。testnet（approver-b 在 `/opt` 的裝置與聲紋、B 的測試幣）還沒做。
