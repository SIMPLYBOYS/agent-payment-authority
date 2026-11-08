# 郵局付了沒交付：商店退款

> 2026-11-07～08，Claude Code。Aaron 11/7：「請重開 A 並按照排序完成任務」，這是第 2 項（第 1 項是由 Signer 證明付款錢包，`docs/commercial-8004.md` §8；第 3 項是頻率限制，`docs/commercial-velocity.md`）。
> **Aaron 11/7 的決定**：R1 由主持人按「商店退款」觸發（人工介入）；R2 也上 testnet。

## 1 為什麼

郵局那一幕（`docs/commercial-post-hotel.md` N3）：付款完成，郵局沒交出寄件單，系統記下「已付款、交付失敗、待人工處理」，不會再付一次。觀眾一定會問「那錢呢？」原本的答案是「要在系統外處理」。

鏈看不到交付（郵局有沒有交出寄件單是鏈外的事），所以「該不該退」要有人判斷；判斷之後的執行（把錢退回去、留下可以查證的紀錄）可以交給系統。

## 2 做法

| 層 | 做什麼 |
|---|---|
| 事件 | 新的付款事件 `PAYMENT_REFUNDED`，紀錄 `amount`（全額，等於這筆請購的金額）、`tx`、`block`。只能接在 `PAYMENT_NEEDS_REVIEW` `DELIVERY_FAILED` 之後，一筆請購只有一次，之後什麼都不能再接（`internal/paycontract/events.go`）。 |
| Store | `PaymentRefunded`：只接受「已付款、交付失敗」的那一筆；同一筆再記一次照舊回答，換一筆交易就拒絕（`IDEMPOTENCY_CONFLICT`）。付款本身的狀態不變（仍是 `needs_review` `DELIVERY_FAILED`），畫面上的付款狀態變成 `refunded`；任務預算不加回去（鏈上的預算預留沒有撤回）。不改資料庫結構。 |
| 商店的結算 | `Settler.Refund`（`internal/merchant/refund.go`）：只退自己結算過、已確認的那一筆，付款人和金額取自商店自己的紀錄，請求只寫那筆結算的交易。退款是一筆代幣 `transfer`，由送結算交易的那把金鑰送出（testnet 上就是 `pay_to`，P2b-D3；本機的 `pay_to` 沒有私鑰，改由送交易的金鑰，`p2-local` 先發給它測試幣）。交易先寫進 `refunds` 表再送出；跟結算交易共用 nonce 的規則，兩邊不會互相卡住。 |
| facilitator | `apa-facilitator` 多一個 `POST /refund`，不是 x402 的端點；請求只寫結算的交易。x402.org 沒有這個端點，經它結算的付款沒有退款。 |
| Host | `POST /api/host/tasks/{task}/purchases/{po}/refund`，只有 operator 能呼叫。Host 請商店退款後，**自己向鏈上確認**：交易已經夠深、成功，裡面有代幣從送出者轉給這個 task 的付款錢包、金額等於這筆請購，才記 `PAYMENT_REFUNDED`。商店說退了不算數。還沒確認就回 202，再按一次會問同一筆。 |
| 畫面 | 簡報模式的交易卡，「交付失敗」那位客人下面多一顆「商店退款」。退款後第 5 步寫「交付失敗；商店已退款（第 3 筆鏈上交易），錢回到客人的錢包」；展示視窗「為什麼可信」多一項「6 退款」連到那筆交易；Unity 的客人標籤「交付失敗，商店已退款，離店」（字型補了「退」字，fontTools 經 Aaron 11/7 同意下載）。 |
| checker | E4 也查退款：夠深、成功、代幣從送出者轉回付款錢包、全額；報告的 `settlements` 那一筆多列 `refund`（交易與送出者）。退款不是從付款錢包轉出，所以不影響 E8。 |

## 3 驗證（本機）

- 事件規則：退款只能接在交付失敗之後一次；已交付、沒有複查、沒付款都不能退；退款之後不能再接任何事件；金額要是全額（`TestPaymentEventOrder`、`TestPaymentEventShape`）。
- Store：只有交付失敗的那一筆能退；狀態與預算不變；同一筆再記一次可以、換一筆不行；重新開啟資料庫後的完整性檢查接受（`TestPaymentFailures`）。
- 商店（本機 anvil）：退回付款人全額，只送一筆；再問一次同一筆、不送；不是這間店的結算、格式不對都拒絕、什麼都不送；寫好還沒送就當掉，下一次送的是那一筆；退款後的下一筆結算接著用下一個 nonce（`TestRefund`）。原本的結算測試（卡住的交易被取代、同時付款、每日上限等）照舊通過。
- facilitator：`POST /refund` 經 HTTP 退款一次；別的交易、空的請求、多一個欄位都拒絕（`TestRefund`）。
- Host：鏈上的轉帳不是全額、不是轉給付款錢包、不是從送出者轉出、別的代幣、交易失敗都拒絕；還沒上鏈、還不夠深、讀不到送出者都回「待確認」（`TestRefundOnChain`）；只有 operator 能呼叫，不付款的 Host 沒有退款（`TestRefundRouteRules`）。
- checker：退款交易失敗、不夠深、金額不對、轉給別人、不是從送出者轉出，E4 都抓得到；E8 不受影響（`TestE1aRefund`）。
- 頁面：退款是自己的狀態，卡片、展示視窗、Unity 的狀態一致；只有付款的 Host、交付失敗還沒退款時才有按鈕（`p2.test.mjs`）。

## 4 照實說的限制

- 「該不該退」是主持人判斷的；系統只確認退的是那一筆、全額、退給付款的錢包。沒有爭議流程、沒有部分退款。
- 退款由商店的金鑰送出；商店不肯退，系統沒有辦法強制（沒有託管）。
- 只有自己的 `apa-facilitator` 能退款；經 x402.org 結算的付款不能。
- 退款不會把任務預算加回去：鏈上的 ZK 預算預留沒有撤回。

## 5 進度

- 本機實拍（11/8，`film.mjs --venues hotel,post`）：郵局 A 交付失敗，腳本按「商店退款」，Host 確認鏈上的退款後記下（交易 `0xa55f…30ce`），交易卡、展示視窗與 Unity 的標籤都寫「商店已退款」；加演短片 `apa-addon-post.mp4` 已重剪（31 秒，`docs/demo-video-script.md` 第 7 節）。

**還沒做**：

- testnet（R2）：郵局上 Base Sepolia 要先調高每日上限、重新產生委託（`docs/commercial-post-hotel.md` §4），併進三項做完後的那一場 testnet 彩排。
