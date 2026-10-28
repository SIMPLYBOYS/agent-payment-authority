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

（尚未開始，等 D1～D5。）
