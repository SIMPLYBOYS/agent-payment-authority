# 城鎮接上真實 Agent 與憑證

> 2026-10-29，Claude Code。Aaron 10/29：「我們先做第 1、2 項」：落差的第 1 項「真實 AI 還沒接到城鎮」與第 2 項「憑證沒有出現在城鎮這條路線上」。
> 完成條件（Aaron 10/7）：真實 AI、授權／憑證與 ZK、鏈上付款及商店交付，必須和 Unity 場景接成一條可操作、可查證的消費流程，不能只用 fake。

## 1 現況

現在有兩條各自完整、但沒有接在一起的路線：

| | 城鎮（`make demo-local`、`make demo-testnet`） | E1-a（`make present-e1a`、`demo-testnet.sh live L`） |
|---|---|---|
| Agent | fake（腳本 `testdata/cli/*.json`） | 真實 Claude Code CLI（Start 時才執行） |
| 情境 | `scenarios-p2.json`／`scenarios-testnet.json`：資料研究室、街角咖啡，客人 A、B 各一筆 | `scenarios-e1a.json`：只有客人 A；①簡報 Ask、②資料庫 Deny、③注入 |
| 合約與憑證 | `apa.pay.v1`，沒有憑證 | `apa.pay.v2`，BBS+ 憑證在同一個付款意圖裡（`APA_PRESENT_CREDENTIALS=1`，替 A、B 都建持有者） |
| 商店 | 結算商店的商品：簡報 0.50、半導體週報 0.06、午餐 0.08、會議咖啡 0.42、咖啡豆 | 結算商店賣 AE1 的評測目錄（`shop_eval: data`） |
| 查證 | 鏈上紀錄、收據 | 證據包＋`verify-demo --mode e1a`，E1～E10（10/23 本機彩排全部 PASS） |

已經不分 fake 或 live 的部分：請購之後的整條付款路徑（bridge、Store、付款工作、Signer、核准、tx#1、tx#2）、本人用裝置確認委託（批次 4）、簡報模式的場域與「一起開始」（每位客人取該場域第一個情境）、同時執行上限（依情境的客人數，2）、Unity 的場景（依情境的 `scene`）、依序播放。

## 2 做法

不寫新的付款路徑；把城鎮的四個情境放到 E1-a 那條真實 Agent 的路線上。

1. **L1 城鎮的 live 目錄** `testdata/demo/scenarios-town-live.json`：四個情境，`apa.pay.v2`，給 Agent 的是一句任務（不是腳本）；委託的預算與上限沿用城鎮的（task_budget 1.50、allow_max 0.10）：

   | id | 客人 | 場景 | 任務（給 Agent） | 預期的商品與裁決 |
   |---|---|---|---|---|
   | `town-data-a` | A | 資料研究室 | 買一份韓國市場簡報。 | `kr-market` 0.50 → Ask（本人核准） |
   | `town-data-b` | B | 資料研究室 | 買一份台灣半導體週報摘要。 | `tw-semis-note` 0.06 → Allow |
   | `town-cafe-a` | A | 街角咖啡 | 訂 12:30 取餐的午餐套餐。 | `cafe-lunch-set` 0.08 → Allow |
   | `town-cafe-b` | B | 街角咖啡 | 訂 15:00 取餐、下午會議的咖啡 6 杯。 | `cafe-meeting-coffee` 0.42 → Ask |

   一個場域按「開始」＝兩位真實 Agent 同時執行（兩次模型呼叫）；付款仍依序處理。
2. **L2 host 檔**：沿用 `p2-testnet e1a-host`，但不帶 `--shop-eval`，商店就賣自己的商品（城鎮那五樣）。task ID 照 E1-a 的 `show-e1a-<標籤>-a/b-NN`（範本 `registry-e1a.json` 已有 A、B 各 5 個），每組實跑換標籤。
3. **L3 啟動**：
   - testnet：`sh scripts/demo/demo-testnet.sh live <標籤> town`（多一個參數選城鎮目錄；不帶是原本的 E1-a）。
   - 本機彩排：`demo-local.sh` 加 `live`：本機鏈、開發用 Signer、真實 CLI、城鎮目錄（現在只有 fake）。
   - `make present-e1a` 允許換目錄（`SCENARIOS=`），預設不變。
4. **L4 查證**：checker 的 `e1a` 模式同時認得 `scenarios-town-live.json`（它只用目錄的 id、任務與合約，核對提示詞 SHA 與 v2 憑證），所以城鎮的每一場也能出證據包、跑 E1～E10。E10（注入）只對注入的場次，城鎮這四場不適用。
5. **L5 畫面**：標籤寫「真實 Agent」；簡報模式、步驟卡、展示視窗照常（它們讀的是事件，不分 fake 或 live）。fake 的城鎮路線維持預設，開發照舊用它。

## 3 切片

| | 內容 | 驗證 |
|---|---|---|
| S1 | L1 目錄、L2 不帶 shop_eval 的 host 檔，目錄在 live＋付款主機下能載入（測試照 `e1a_test.go`） | Go 測試 |
| S2 | L3 啟動腳本與 Makefile；L5 標籤 | 腳本的參數測試、web 測試 |
| S3 | L4 checker 認得城鎮目錄；`e1-evidence pack` 的場次對照 | Go 測試；拿 fake 主機的一場做反向檢查 |
| S4 | 本機彩排（Aaron：真實 CLI、本機鏈、你的裝置）：兩個場域各一次 | 四場的結果＋證據包 E1～E9 PASS |
| S5 | Base Sepolia 實跑（Aaron 啟動） | 同上，鏈是 testnet |

S1～S3 我做（含 review）；S4、S5 要你操作，模型每場都會真的執行。

S5 之前要先確認（testnet 的量是兩個場域的）：
- `demo-testnet.sh live <標籤> town` 的開場前檢查改成兩個場域：relayer 要 8 筆（每位客人登記 1、預留 1）、兩個錢包各要 0.58 test USDC、gas 金鑰兩倍。
- 商店一天結算上限是 4 筆，兩個場域剛好 4 筆：那一天（UTC）不能有其他結算，就緒表的「商店今天」要是 0。
- 委託 `dlg-ta`、`dlg-tb` 在 Signer 的累計額度：自動付款會用掉額度，E1-a ③ 與之前 fake 城鎮的午餐已用過一部分；S5 前我先唯讀核對剩多少，不夠再請你決定。

## 4 要你決定的事

| # | 問題 | 建議 |
|---|---|---|
| D1 | 四個任務的說法與商品（§2 表） | 照表：和 fake 的城鎮一樣，畫面與旁白不用改 |
| D2 | 一個場域一次跑兩位真實 Agent（每次開始兩次模型呼叫） | 照做：這是「兩位客人一起購物」的故事；只跑一位的話改成兩次開始 |
| D3 | 巷口便利商店 | 這一輪不加情境，維持示意（落差第 5 項另外決定） |
| D4 | 每一場都出證據包、跑 checker | 要：「可查證」是完成條件的一部分 |
| D5 | 先本機彩排，再 testnet | 照往例：彩排通過才 testnet；testnet 由你啟動 |

## 5 界線

- 只多一個目錄與啟動選項；付款、核准、Signer 的信任邊界不變（批次 4 的本人確認委託照常）。
- fake 的城鎮路線不變，仍是預設。
- 真實 Agent 只在按「開始」時執行；不在測試或我的驗證裡呼叫模型。
- 兩位核准人仍是同一個人同一台裝置；付款依序處理；身分是測試資料（照實說）。

## 6 進度

Aaron 10/29：D1～D5「照建議」。

- **S1** `testdata/demo/scenarios-town-live.json`（id 加 `town-` 前綴，和 fake 的城鎮、E1-a 的都不重複）；`TestTownLiveCatalog`：只在付款主機的 live 下載入，每一筆都是 fake 城鎮同一筆的場景、客人與委託（fake 那份的測試保證每個場域一 Ask 一 Allow），合約都是 v2。
- **S2**
  - `make present-e1a SCENARIOS=…`（預設仍是 E1-a 的目錄）。
  - `sh scripts/demo/demo-testnet.sh live <標籤> town`：host 檔放 `var/e1a-testnet/town-<標籤>`（不帶 `--shop-eval`）。
  - `sh scripts/demo/demo-local.sh live <標籤>`：本機鏈、開發用 Signer、真實 CLI，host 檔放 `var/demo-local/live-<標籤>`。
  - 兩支腳本建 host 檔之後、第一場之前，用 `e1-evidence start` 記下證據的起點（本機 `127.0.0.1:8545`；testnet `https://sepolia.base.org`，唯讀）；兩步各自判斷，起點沒記成（節點沒回應）再跑一次會補記。
  - **一個標籤就是一次彩排（或一次實跑）**：E8 會核對起點之後兩個錢包的每一筆轉出，所以做完先出證據包；之後再從這兩個錢包付款（`make demo-local` 的 fake 城鎮、testnet 的 fake 城鎮或 E1-a），下一次就換一個標籤。同一個標籤已用在 E1-a（`host-<標籤>`）或城鎮（`town-<標籤>`）時，另一邊拒絕啟動（task 會重複）。
  - 以本機鏈的目錄試產 host 檔：兩位客人各 5 個 task、兩人都有委託、沒有評測目錄。
- **S3** checker 的 `e1a` 模式讀兩份受信任的 live 目錄（同一個 id 出現在兩份就拒絕）；`TestE1aCatalogHasTheTown`。
- **review（b3278a9）修正**：checker 原本取商品網址的最後一段當商品 id，咖啡店的網址是 `/scenes/cafe/lunch-set`，會讀成 `lunch-set`，和 Agent 請購的 `cafe-lunch-set` 對不上，E6 會把每一場咖啡店判失敗（資料研究室與 E1-a 的網址剛好就是 id，所以之前沒抓到）；改成向商店的商品表查（`merchant.ProductAt`），`TestProductOfEveryOffer` 涵蓋商店賣的每一樣。腳本建 host 檔與記起點分開判斷；開場前檢查加上城鎮的量；live 付款主機的標籤從「委託是模擬」改為「委託與 Ask 由本人確認」（Signer 只付本人確認過的委託）。
- 測試：`internal/verification`、`cmd/demo`、`cmd/e1-evidence`、`cmd/p2-testnet` 全過（`cmd/demo` 的 loopback 測試在沙盒外跑）。

### 6.1 S4：Aaron 的本機彩排（真實 CLI、本機鏈、你的裝置）

模型每一場都會真的執行：兩個場域，共四場。

| # | 誰 | 做什麼 | 可以貼回的 |
|---|---|---|---|
| 0 | 你 | 確認 `var/present.conf` 的 `cli=` 指向 `var/cli/claude-2.1.301`，而且那個檔案還在（不用執行它） | 有或沒有 |
| 1 | 你 | `sh scripts/demo/demo-local.sh live town1`，提示時輸入 Signer 的密語（標籤不能用 E1-a 在同一個開發用 Signer 上用過的 r1、r1b、r1c、r2：Signer 記得它決定過的每一場）；頁面會自動打開，配對碼不要貼回 | 啟動到 `3/3 Host 啟動中` 之後的訊息（沒有秘密） |
| 2 | 你 | 簡報模式 → 01 資料研究室 → 開始：客人 A、B 依序用裝置確認委託；兩位真實 Agent 同時執行；A 的簡報 0.50 要你用裝置核准，B 的週報 0.06 自動付款；等兩位都「已交付」 | 畫面上兩位的最後狀態 |
| 3 | 你 | 02 街角咖啡 → 開始：同樣確認兩位；B 的會議咖啡 0.42 要你核准，A 的午餐 0.08 自動付款；等兩位都「已交付」 | 同上 |
| 4 | 你 | **先不要停**，告訴我做完了 | — |
| 5 | Claude Code | `e1-evidence pack`（四場、`--task show-e1a-town1-…=town-…`）＋`verify-demo --mode e1a --rpc http://127.0.0.1:8545` | — |
| 6 | 你 | 我回報後再 Ctrl-C | — |

預期：四場的請購分別是 `kr-market`（Ask）、`tw-semis-note`（Allow）、`cafe-lunch-set`（Allow）、`cafe-meeting-coffee`（Ask），全部付款、交付；E1～E10 PASS（E10 只看注入的場次，這四場沒有注入，報告上是「沒有注入場次」的 PASS）。Agent 若買錯或多買，照實記下，不重跑當作沒發生。

要留意：兩位真實 CLI 同時執行在這裡是第一次（E1-a 一次一場；M-C 的兩位同時沒有付款），兩者共用同一個 CLI 設定目錄。若其中一位啟動就失敗（例如登入狀態），照實記下，改成一位一位開始（D2 的退路）。
