# 上台順暢度：鏈與 Signer 隨時能開、鏈上互動看得見

> 2026-10-23，Claude Code。Aaron 10/23：「本機鏈和 Signer 如果是在拍攝 demo 影片時可以先 turn off 但實務上必須要能隨時被開啟且正常工作，我希望在 demo 中還是能夠展示 agent 與鍊上互動」。決定：本機鏈與 Base Sepolia 兩者都要；先做這項，再做批次 3。對應 `docs/commercial-gap-review.md` 的 C1、C4。

## 切片

| # | 內容 | 狀態 |
|---|---|---|
| O1 | 本機鏈可續用；本機一鍵啟動與停止 | 完成（見下） |
| O2 | 展示視窗的「鏈上紀錄」：Host 向鏈讀出兩筆交易的區塊、轉帳與預算合約事件；本機鏈與 Base Sepolia 都顯示，Base Sepolia 另附 basescan | 完成（見下） |
| O3 | Base Sepolia 一鍵啟動（語音服務、`/opt` 的 Signer、relayer、Host）；sudo 由腳本向 Aaron 要 | 完成；Aaron 10/23 實測：啟動到 Host，Ctrl-C、relayer 密語錯誤（卡住）、直接關視窗三種停法之後，`pgrep -lu apa-signer` 與 8402 都沒有殘留 |

## O1 本機鏈可續用、一鍵啟動

- `scripts/demo/p2-local.sh`：anvil 以 `--state anvil-state.json --state-interval 1` 保存鏈。同一個目錄再啟動就接著用同一條鏈（合約、餘額、交易都在），relayer、商店與 Host 的資料庫因此仍然對得上；只有第一次才部署。沒有保存檔的舊目錄照舊拒絕。測過：重開後區塊高度相同、預算合約仍在、重開前的交易收據與內容讀得到（checker 需要）。`P2_LOCAL_PORT` 可換埠（測試用）。
- `make demo-local`（`scripts/demo/demo-local.sh`）：一個終端機依序啟動本機鏈、開發用 Signer、fake Agent 的 Host。
  - 啟動前檢查：8545、8402 沒有程式在用；沒有殘留的 Signer socket；兩個錢包的金鑰檔在。
  - 每一步等前一步就緒，卡住時說卡在哪一步、看哪個紀錄。
  - Signer 的密語在同一個終端機由 Aaron 輸入（安全上必須保留）；Host 在 Signer 就緒後自動啟動並打開頁面。
  - Ctrl-C 一次停下三個；鏈保存在 `var/demo-local/chain/anvil-state.json`，下次同一個指令接著用。
  - 目錄：`var/demo-local`（鏈、Host 場次、紀錄），Signer 沿用 `var/p2-dev-e1a`（已登記 approver-a 的裝置）。

### 使用

```
make demo-local
```

輸入密語 → 頁面打開 → 配對 → 照常操作。結束按 Ctrl-C。

## O2 鏈上紀錄

- `GET /api/tasks/{task_id}/chain`（與讀 task 同一套權限）：Host 用自己的節點，只查 Store 為這個 task 記下的交易：區塊、時間、成功或失敗；tx#1 由預算合約記下的金額（`Spent`）；tx#2 的代幣轉帳（`Transfer`）。夠深的收據留在記憶體，不重複問。
- 主持人頁在展示視窗開著時，以該客人的 viewer token 每 5 秒讀一次並轉過去；展示視窗兩張交易卡多一行，例如「鏈上：區塊 #9・4 個確認・合約記下 0.50 USDC」「鏈上：區塊 #10・3 個確認・0.50 USDC 客人錢包→商店」。本機鏈沒有 explorer 也看得到；Base Sepolia 另有 basescan 連結。
- 只顯示交易編號與 Store 紀錄相同的那一筆；查不到寫「還查不到」，失敗寫「執行失敗」。

## O3 Base Sepolia 一鍵啟動

```
make demo-testnet                          # fake Agent
sh scripts/demo/demo-testnet.sh live t3    # 真實 Agent，E1-a 的 host 檔（第一次以 t3 建立），task 是 show-e1a-t3-…
```

- 啟動前檢查：8402 沒有程式在用；`/opt` 的 Signer、語音服務沒有在跑；relayer 的 socket 若是上次留下、沒有人回應，自動移除（apa-relayer 不接受已存在的 socket）。
- 順序：sudo 密碼一次 → 語音服務（背景，等健康檢查，約 2～3 分鐘）→ relayer 密語 → Signer 密語 → Host（打開頁面、印出配對碼）。每一步卡住會說卡在哪。Ctrl-C 停下這次啟動的全部。
- 每一場實際付款仍照往例先請 Aaron 核准（測試 USDC 與 gas）；開始前照 `docs/commercial-e1a.md` 6.2 確認餘額、每日上限與委託總額。

## 已知的界線（review）

- 本機鏈每秒存檔一次，停下時（Ctrl-C、關視窗）會等最後一次存檔完成。但程序被強制終止或當機時，最多會少掉最後 1 秒的鏈。若剛好落在一筆付款之間，Store、relayer、Signer 記得的交易鏈上沒有：畫面會寫「還查不到」，之後的預留也會失敗，只能換新目錄重來。每次重開成功都會備份一份 `anvil-state.json.bak`（重開當時的鏈）。
- 重開後的鏈時鐘（10/24 修正）：anvil 重開時，鏈的時鐘從存檔最後一個區塊的時間接著走，所以關掉一小時，鏈就慢一小時。Signer 的付款授權依這台電腦的時間「現在減 1 秒之後有效」，測試 USDC 在鏈上看來還沒生效（`AuthorizationNotYetValid`），商店模擬時被拒，沒有送出（代碼 VERIFICATION_FAILED；展示視窗現在寫「商店回覆沒有接受、沒有送出（商店的說法，鏈上未確認）」，主持人頁寫「商店回覆驗證未通過、沒有送出（商店的說法）」）。重開後的每一筆付款都會這樣。現在 `p2-local.sh` 重開時先用 `evm_setTime` 把鏈時鐘調到現在加 1 秒（review：這一秒若在呼叫途中跳過，鏈會慢 1 秒，同一秒內出塊的預留就等於生效時間；快 1 秒只讓授權在鏈上早 1 秒到期，商店本來就留 5 秒），再挖一個區塊（商店的模擬是在最新區塊上執行，用的是最新區塊的時間；review）；任一步不成就停下，不會帶著慢的時鐘繼續。測過：備用鏈（8546）關 40 秒後重開，下一個區塊的時間與現在相差 0 秒；最新區塊上執行的時間，只調到現在時慢 1 秒，調到現在加 1 秒後快 1 秒（anvil 1.8.3 實測）；不調時差 34～50 秒、最新區塊差 411 秒。Base Sepolia 是真的鏈，沒有這個問題。
  - 這台電腦的時鐘若往回調過（早於鏈上最後一個區塊），anvil 仍讓區塊時間往前走，鏈會比現在快，授權在鏈上會提早到期。沒有檢查這種情況。
  - 本機鏈只在有交易時才出新區塊。一筆沒有送出的授權過期後，Host 要看到時間已過期限的區塊，才會改記成「已過期」；沒有新交易時會一直停在待處理。這是之前就有的情況，不是這次的修正造成的。
- 重開時只檢查「預算合約在設定的位址」。anvil 每條新鏈的位址都一樣，所以這只是基本檢查，不能證明存檔是這個目錄的。
- `make demo-local` 沿用 `var/p2-dev-e1a` 的 Signer（已登記你的裝置），它的資料庫也有 E1-a 彩排的紀錄；本人核准的每日上限是跨鏈累計的。
- `make demo-testnet` 在整場執行期間每分鐘延續一次 sudo 的授權（`sudo -n -v`），停下時才能以 root 停掉 `/opt` 的語音服務與 Signer；停下後就不再延續。

## 實測（Aaron，10/23）

- 本機：`make demo-local` 啟動、付款後展示視窗顯示鏈上區塊與確認數、合約記錄與轉帳；Ctrl-C 後重開接著用同一條鏈（block 8）。
- Base Sepolia：`make demo-testnet` 啟動前發現 E1-a 實跑留下的 `/opt` Signer socket 並提示清除指令；清除後啟動到 Host；Ctrl-C、卡住、關視窗之後都沒有殘留程序。沒有跑場次、沒有交易。
