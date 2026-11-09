# 街上的 ATM：領取測試 USDC

> 2026-11-09，Claude Code。Aaron 11/9：「應該可以設計一個場域是可以與 faucet 互動去領取 usdc 的」；testnet 用真的 Circle faucet（Aaron 自己領，場域盯著鏈上）；「依照建議的方式建置 atm」：LimeZu 的 ATM，放在商店街、地下鐵樓梯口對面；「每位客人每天最多領 20 的上限請加上」。

## 1 一段話

客人走到商店街的 ATM 前停下，網頁向 Host 問這位客人的錢包。本機鏈：Host 當場鑄造 10 test USDC 給這個錢包，ATM 上方顯示「領到 +10.00、餘額」。testnet：Circle 的 USDC 不能鑄造，faucet 又要真人驗證，所以 ATM 顯示錢包地址與餘額，Aaron 在 Circle faucet 領取；網頁每 5 秒問一次餘額，變多了 ATM 就顯示「入帳 +N、餘額」。

## 2 做法

| | 內容 |
|---|---|
| Host | `POST /api/host/atm {request_id, consumer_id}`（operator）。回覆錢包地址、餘額（base units）與 `mode`：本機 `mint`（加上 `minted`、`tx`），testnet `watch`。本機用 TestUSDC 的擁有者（anvil key 0，`p2-local` 用它部署）送 `mint`，等到成功才回覆；同一個 request ID 只鑄一次。每位客人每個 UTC 日最多 20（兩次），超過回 `mode: limit`（加上 `limit`）、不鑄造；計數在 Host 的記憶體，Host 重啟就重新算。沒有付款功能的 Host 回 404 `no_atm`。`cmd/demo/atm.go` |
| Unity | `town.atms`：ATM 前面的一塊人行道（`spot`）與字的位置。客人在上面停下，或往上走進去，才回報 `atm`（路過不算），離開再回來才再回報。`Atm {id, code, amount, balance, address}` 顯示網頁的回答：`minted`、`limit`（今日已達提領上限）、`watch`（一直顯示）、`arrived`、`stopped`、其他是「暫時無法使用」。ATM 本身是道具 `x-atm`（`ME_Singles_City_Props_32x32_ATM_1.png`），立在人行道靠路緣那側，正面朝人行道。 |
| 網頁 | `host.js` 的 `atmReached`：遊戲模式走的那位客人到了 ATM 才問 Host；`watch` 時每 5 秒問一次，最多 5 分鐘；換了客人或時間到就顯示「沒有等到入帳」。展示視窗也接上（`showlink`、`show.js`）。 |

## 3 界線

- 不是付款：不經過 Policy、不開任務、不進交易紀錄。鑄造是一筆轉進錢包的轉帳，對帳（P2-a）與證據包的 E8 只算轉出，不受影響。
- testnet 上 Host 不送任何交易，只讀餘額。入帳的來源 ATM 不分辨：任何人轉進這個錢包都算。
- 本機每次停下就鑄 10 個，每位客人每個 UTC 日最多 20（台北 08:00 換日）；testnet 的上限是 Circle faucet 自己的，ATM 不另外限制。

## 4 驗證

- Go：`TestATM`（新的 anvil、部署 TestUSDC）：鑄造、重送只鑄一次、當天第三次被擋、UTC 隔天又能領、testnet 模式只讀、不是擁有者的金鑰會失敗。
- Unity PlayMode：`TheAtmAnswersTheCustomerAtIt`：路過不回報、停下回報一次、`watch` 一直顯示、`arrived` 之後消失、上限的字、不存在的 ATM 回 Invalid。
- 網頁：展示視窗的轉送（`show.test.mjs`）。
