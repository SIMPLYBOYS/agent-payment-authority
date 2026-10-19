# P2-b Testnet 準備：設計提案

> 2026-10-19，Claude Code。依 vault〈Claude-Code-P2Testnet付款結算對帳交付派工-20261013〉的 P2-b、`docs/commercial-p2.md` §5 與 §6 的「P2-b 再決定」、`docs/commercial-p2-host.md` H-D5。分支 `commercial-p2b`（從 main `897cca7`）。
> **Aaron 10/19：「照建議進行」：§5 的 P2b-D1–D8 全部採用建議。** 依 §4 分段實作。 這一段只做準備：程式改動都只在本機 anvil 上測試。沒有建立或讀取任何金鑰，也沒有連 RPC、faucet、facilitator、test402 或任何鏈，沒有動 `/opt`。§2 的查證只讀了公開文件網頁。

## 1 要做到什麼

讓 P2-a 的付款流程（預留 tx#1 → Signer 簽 EIP-3009 → 商店 verify／settle tx#2 → 對帳 → 交付）能在 Base Sepolia 上執行，並把 P2-c 要做的每一件外部操作列成清單，經 review 與你核准後才執行。P2-c 的停止點不變：一條成功 Ask、一條 Deny 的 testnet 證據，以及 test402.com 一筆真 402。

P2-b 的交付物：
1. 本文件：部署差異、地址、RPC、facilitator、測試資金與 gas、A／B 錢包隔離、金鑰保管、上限、交易清單、停止流程（派工列的各項）。
2. 程式：§4 的 B1–B6，每段各自提交與 review，全部在本機 anvil 上證明。
3. P2-c 的執行清單（§6）：每一步標明誰做、碰到什麼、要你核准的是什麼。

## 2 查證（只讀公開文件，10/19）

| 項目 | 結果 | 來源 | 還沒確定的 |
|---|---|---|---|
| Base Sepolia USDC | `0x036CbD53842c5426634e7929541eC2318f3dCF7e`（鏈 84532） | Circle 的 USDC 合約地址表；x402 文件；9/22 test402 的 402 也是這個 asset | — |
| EIP-712 domain | name 是 `USDC` | x402 文件的網路與代幣表 | **version** 文件沒寫，只說取自合約的 `version()`。P2-c 第 1 步以唯讀呼叫讀 `name()`、`version()`、`decimals()`，不符就停 |
| 公開 RPC | `https://sepolia.base.org` | Base 文件 | 沒有公開的速率上限。Base 建議 `eth_getLogs` 每次少於 2,000 個區塊（我們的對帳本來就是 2,000 一段） |
| USDC faucet | Circle 公開 faucet | 第三方整理 | 數量兩種說法：每 2 小時 20 USDC，或每 24 小時 10 USDC。用多少都夠，見 §5 P2b-D5 |
| x402.org facilitator | 支援 Base Sepolia，標示「Testnet only」 | x402 文件 | 我們的主路徑不用它（P2-D3）。test402.com 那一筆由 test402 自己找 facilitator 結算 |
| test402.com | 9/22 實測回 402：`eip155:84532`、`exact`、amount 100（0.0001 USDC）、asset 是上面的 USDC | `執行計畫.md` §10 | 現在是否仍在線、付款資訊是否改了，要在 P2-c 以一次唯讀 GET 重新確認 |

## 3 現況：P2-a 只認本機鏈的地方

| 部分 | 現在 | P2-b 要改成 |
|---|---|---|
| demo 的鏈 | `--payments` 只接受 loopback 的 `http`（`eth.LocalRPC`），且 `web3_clientVersion` 必須是 anvil（`cmd/demo/payments.go`）；`payment_capability` 只有 `local_anvil` | 新增 `base_sepolia` 環境：允許 `https`；以 `eth_chainId` 確認是 84532；不是 anvil 才算 testnet。畫面標示「P2 / Base Sepolia testnet」 |
| 預算 relayer 的金鑰 | gas、issuer、policy 都是 anvil 的公開金鑰，依索引取（`payments.json` 的 `anvil_keys`） | 獨立程序 `apa-relayer`（H-D5 已承諾 P2-b 前做）：加密金鑰檔，啟動時輸入密語 |
| 商店的 gas 金鑰（tx#2） | anvil 公開金鑰 4 | 加密金鑰檔，啟動時輸入密語 |
| 部署 | `p2-local`：部署 TestUSDC、`anvil_setCode`、鑄幣，只能用在 anvil | 新命令 `p2-testnet`：只部署 Verifier 與 Budget，資產用 Circle 的 USDC，不鑄幣 |
| 對帳起點 | `token_from_block` 寫 0，每次從頭掃 | 寫成部署 Budget 的區塊（錢包是新建的，之前沒有支出）。從 0 掃 Base Sepolia 要幾萬次 `eth_getLogs` |
| 等待與逾時 | 商店等 receipt 20 秒，`p2-local` 自己送交易用固定 1 gwei、只等 10 秒 | 確認數 2（P2-D6）；商店與 relayer 的等待改由設定決定，預設照公開鏈調大；`p2-testnet` 用 `eth.SignCall` 的 EIP-1559 定價 |
| 上限與停止 | Signer 有每筆與累計上限；沒有每日交易數上限、沒有停止開關 | relayer 與商店各自有每日交易數上限（記在自己的資料庫）；§7 的停止流程 |
| Signer 的預留核對 | `signer.json` 的 `reservation` 段接受任何 `https` RPC，並核對 `eth_chainId` | 不用改，只換設定值 |

## 4 程式分段（全部在本機 anvil 上證明；每段提交與 review）

- **B1 金鑰檔**：沿用 Signer 的加密格式（scrypt＋AES-256-GCM，`internal/signer/keystore.go`），讓 relayer 與商店的金鑰也能這樣存；但驗證資料（AAD）的標籤改成每個程式各自的（例如 `apa-relayer-key/v1`），relayer 的金鑰檔不能被當成 Signer 的錢包解開，反之亦然。新增 `apa-relayer keygen --dir DIR --role relayer-gas|issuer|policy|shop`，與 `apa-signer keygen` 相同：密語只在終端機輸入兩次，只印出位址。
- **B2 `apa-relayer`**：獨立程序，Unix socket（0600，檢查對端 uid），啟動時輸入一次密語，持有 relayer-gas、issuer、policy 三把。介面就是付款工作現在呼叫的三個：`RegisterTask`、`Reserve`，以及恢復時用來判斷「什麼都沒花」的 `Lookup`。每日交易數上限記在它自己的資料庫，**以 nonce 計**：同一個 nonce 因卡住而重新定價、重新簽名的不另計，所以上限算的就是鏈上最多會多出幾筆。超過就拒絕（新碼 `RELAYER_DAILY_CAP`，付款工作視為最終拒絕，不重試）。商店的每日上限同樣以 nonce 計。
- **B3 `base_sepolia` 環境**：demo 的閘門、`payment_capability`、畫面標示、對帳的 `environment` 與起點、確認數 2、逾時設定、商店的金鑰檔與每日上限。本機測試用 anvil 加 `--chain-id 84532`；只有測試能把 `base_sepolia` 指向 loopback，正式旗標不行。
- **B4 `p2-testnet`**（寫出的 `payments.json` 用新的委派 ID `dlg-ta`、`dlg-tb`，`policy_ttl` 固定 600；demo 拒絕超過 600 的 `policy_ttl`，§7「預算授權 10 分鐘內到期」才不依賴設定檔）：
  - `preflight`（唯讀）：`eth_chainId`、USDC 的 `name`／`version`／`decimals`、各位址的 ETH 與 USDC 餘額，和預期不符就停。
  - `deploy`：先印出要送的兩筆交易（部署 Verifier 與 Budget）、送出者、估計 gas，要你輸入 `yes` 才送；送出後等 2 個確認，寫出 `payments.json` 與 `registry.json`（錢包位址、Circle USDC、商店收款位址、部署區塊）。目錄已存在就拒絕。部署者就是 issuer（P2b-D3），所以 `apa-relayer` 必須是停著的（檢查它的 socket），兩個程序才不會用到同一個 nonce。
- **B5 test402**（照 P2b-D7 A）：
  - 委派依「客人＋收款方」選：`payments.json` 的 `delegations` 現在是一位客人一個（`cmd/demo/payments.go`、`internal/payflow` 的 `Starter`），付給 test402 會被拿 `dlg-tb` 去簽，而 Signer 會因收款方不在 `dlg-tb` 的 merchants 而拒絕。改成 `delegations` 以（客人、收款方）對應委派，Store 交給 `Starter` 的付款 task 帶上每筆的收款方。沒有對應的委派時照現在的規則：Store 拒絕、記錄、不重試，這筆不付款。
  - Policy profile 的 `merchants` 加入 test402 的收款位址（`internal/policy`）。
  - 目錄加一個 test402 的商品／場景，Agent 才請購得到（Agent 只能買目錄上的商品）；它的商店是外部的 x402 resource，交付內容照 H-D2 只存 hash。
  - 先在 P2-c 第 1 步重新確認 test402 現在的 402，再定商品的價格與收款方。
- **B6 `/opt` 重建的工具**：`/opt` 的 `signer.json` 需要的錢包位址（§6 第 2 步）與預算合約位址（第 4 步）在 P2-b 還不存在，所以這一段只做**產生器與檢查**：
  - 由 `p2-testnet` 寫出的 `payments.json` 產生 `/opt` 用的 `signer.json`：Base Sepolia 的 `reservation`、P2b-D5 的上限、`rp_id` `localhost` 與 P2-c 用的 Host 位址（`http://localhost:8402`），以及語音核准用的商家表（新的商店收款位址，否則 Ask 的語音核准回 `VOICE_MERCHANT_UNKNOWN`；test402 那一筆 0.0001 低於 allow_max，是 Allow，不需要）；
  - 委派用新的 ID（例如 `dlg-ta`、`dlg-tb`），讓累計上限從 0 開始，不和 `/opt` 的 `signer.db` 裡可能已有的紀錄混在一起；另有 `dlg-t402`（wallet-b，merchants 只有 test402 的收款位址，每筆與總額都是 0.0001），收款位址在 §6 第 1 步確認後才作為輸入傳給產生器；
  - 更新 V1 §15 的步驟（含 `approver migrate`）。實際的 `signer.json`、執行檔與核對值，在 §6 第 5 步才產生。只在 `var/` 準備，不動 `/opt`。

### 4.1 進度

**B1 金鑰檔**（完成，待 review）：
- `internal/signer/keystore.go`：AAD 改為「標籤＋ID＋位址」；`EncryptKey`／`DecryptKey` 用 Signer 的標籤 `apa-signer-key/v1`，與既有的錢包金鑰檔逐位元組相容；新增 `EncryptKeyFor`／`DecryptKeyFor` 與 `ReadKeyFile`。
- `internal/keytool`：讀密語（關掉回顯）、私有目錄檢查、產生金鑰檔（`Keygen`）、開啟金鑰檔（`Load`），從 `apa-signer` 移出，兩個程式共用；Host 的四個角色與標籤：relayer-gas、issuer、policy 用 `apa-relayer-key/v1`，shop 用 `apa-shop-key/v1`。
- `apa-relayer keygen --dir DIR --role …`：與 `apa-signer keygen` 相同的規則（目錄 0700、密語在終端機輸入兩次且至少 12 字、不覆寫既有檔、只印位址）。
- 測試：同一把金鑰只在自己的標籤與 ID 下打得開（relayer 的不是錢包、也不是商店的，反之亦然）；拒絕兩次不同、太短、不合法的 ID、已存在、目錄權限太寬。`apa-signer` 原有的 keygen 測試不變並通過。
- 突變：新增 2 項；`m-b-reverse.py` 的 keygen 三項改指向 `internal/keytool`，「沒有預留段就什麼都不簽」那一項的錨點在 P2 H3 後已失效，一併改正。

B1 的 review（04006c2、0c7ac6b）：APPROVE，兩項 Important 已處理：
- 沒有測試證明既有的錢包金鑰檔還打得開 → 以改動前的程式（8157530）產生一個固定的測試金鑰檔，`TestKeyFileFromBeforeLabels` 確認它仍以 Signer 的標籤打開、在別的標籤下打不開；並加上「改了 Signer 標籤」的突變。
- `keytool.Load` 沒有檢查目錄 → 與 `apa-signer` 一樣，`DIR` 與 `DIR/keys` 必須只屬於這個使用者；測試涵蓋缺檔、別的 ID 的檔、權限太寬的目錄。
- 建議也照做：`apa-signer` 的錯誤訊息恢復為「wallet ID … is not valid」；角色改為函式 `keytool.Role`（不是可被改寫的 map）；`apa-relayer` 沒有終端機時什麼都不寫的測試；跨標籤的突變改由 `internal/signer` 自己的測試抓。

**B2 `apa-relayer`**（完成，待 review）：
- `apa-relayer serve --dir DIR`：讀 `DIR/relayer.json`（socket、可呼叫的 uid、RPC、鏈、預算合約、確認數、`policy_ttl` 1–600 秒、zkbudget、每日交易上限，全部必填），確認 socket 不存在，問一次密語打開三把金鑰（relayer-gas、issuer、policy），以 `eth_chainId` 確認鏈，再在 socket（只有這個使用者能用）上服務；中斷時移除 socket。輸出只有位址與上限，沒有密語或金鑰。
- `internal/budget/serve.go`：路由 `GET /v1/health`、`POST /v1/register`、`/v1/reserve`、`/v1/lookup`；先檢查對端 uid；拒絕以代碼傳回（付款工作照原規則判斷），其他錯誤只回 `RELAYER_UNAVAILABLE`，不帶錯誤文字；`Client` 是 Host 這一端。
- 每日上限：`budget.Config.DailyTxCap`，以 nonce 計（`sent` 表，記在交易寫入之前）；同一 nonce 重簽不另計；超過回 `RELAYER_DAILY_CAP`，在任何交易之前。
- demo：`payments.json` 的 `relayer_socket`（選填）指向 `apa-relayer`；開始前先確認它 ready，且鏈與預算合約和設定相同，否則不啟動。
- 測試：socket 傳遞答案與拒絕、拒絕別的 uid、`apa-relayer serve` 的拒絕與實際啟動（以只回 `eth_chainId` 的假 RPC）；anvil 上的 `TestDailyTxCap`；`TestPaidOnAnvil` 改經 socket 呼叫 relayer，仍付款、交付、對帳 0。

B2 的 review（c29b0c6、221c71e）：APPROVE，一項 Important 已處理：`apa-relayer` 的密語在三把金鑰打開後立刻清除，不再留到程序結束。建議也照做：relayer 與 demo 都要求 socket 所在目錄只屬於這個使用者（別人無法放一個假的 relayer）；`callers` 只能是 `apa-relayer` 自己的 uid（socket 本來就只有它能連）；`zkbudget` 與 `zk_setup_dir` 必須是絕對路徑；register／reserve 不因 Host 斷線而中止（已寫入的交易照樣送出並判定，Host 下次以 Lookup 讀到）；`RELAYER_UNAVAILABLE` 在 relayer 自己的終端機記下原因（RPC 位址遮蔽），socket 上仍不帶文字；`TestServe` 等待 60 秒，不因 scrypt 慢而誤判。

**B3a `base_sepolia` 環境（Go 端）**（完成，待 review）：
- `payments.json` 的 `environment`：`local_anvil`（預設，P2-a 的檔案不用改）或 `base_sepolia`。
- `base_sepolia` 的規則（什麼都還沒打開前就檢查）：鏈 84532；RPC 是 `https`、不含帳密；不接受任何 anvil 金鑰；必須有 `relayer_socket`（`apa-relayer`）與 `shop_key_dir`；確認數至少 2；`shop_daily_tx_cap`；`token_from_block`（部署區塊）。任何環境的 `policy_ttl` 都是 1–600 秒。
- 鏈：`eth_chainId` 必須相符；`local_anvil` 必須是 anvil，`base_sepolia` **不能**是 anvil（本機鏈冒充 testnet 一律拒絕）。
- 商店金鑰：`shop_key_dir/keys/shop.key`，啟動時在終端機問密語（`demo shop key passphrase:`），必須等於 `pay_to`（P2b-D3：商店自己付 gas、自己收款）。
- 商店每日上限：`merchant.SettleConfig.DailyTxCap`，以交易 nonce 第一次使用的日子計；超過回 402 `settlement_daily_cap`，什麼都不送（付款工作記為驗證失敗，不再付）。testnet 上商店每秒查一次、最多等 2 分鐘；`apa-relayer` 每秒查一次。
- `payment_capability` 與 approvals 的 `sources.payment` 就是環境值；對帳報告的 `environment` 也是。
- 測試：`TestTestnetConfigRefused`（12 種）、`TestTestnetPaymentsOpen`（anvil 冒充、relayer 是別的預算合約、密語錯、商店金鑰不是 pay_to、正常開啟）、merchant 的 `TestDailyTxCap`（anvil）。

B3a 的 review（eaac5a3）：APPROVE，一項 Important 已處理：付款工作等商店的時間（原為 30 秒）短於 testnet 上商店自己的等待（2 分鐘），慢的付款都會先變成「結果不明」再由鏈判定；改為 testnet 上等商店 2 分 15 秒。建議也照做：`base_sepolia` 的 RPC 不能是這台機器（loopback）；`apa-relayer` 的 health 回報確認數，demo 要求不少於自己的設定；商店因每日上限拒絕時，終端機記下 `shop: settlement_daily_cap`（畫面仍顯示「商店驗證未通過，沒有送出」，不新增付款代碼）；有付款時 `present.sh` 等就緒 5 分鐘（要先輸入商店金鑰的密語）；文字與註解更新。未做：以 nonce 計的上限沒有「卡住的轉帳被取代」的測試（要模擬卡住的 nonce，留到 P2-c 前補）；密語輸入中按 Ctrl-C 不會中斷（要按 Enter），`apa-signer` 亦同。

**B3b 網頁與 Unity**（完成，待 review）：
- `web/adapter.js` 的 `PAYING`／`paysOn`：`local_anvil` 與 `base_sepolia` 都是「Host 會付款」；其他值一律不付款。卡片、明細、核准面板的來源、主持人頁的就緒表與對帳列、讀對帳的時機都改用它；`base_sepolia` 顯示為「Base Sepolia testnet」。
- Unity：`SceneRules.Paying`；橫幅分三種：`M1 / no payment`、`P2-a / local test payments`、`P2 / Base Sepolia testnet payments`；未知的值不顯示付款。
- 測試：網頁 116／116；Unity scene／editmode／playmode 全部通過（playmode 加上 testnet 橫幅與未知值）。WebGL 建置已重做。

## 5 要你決定的事

| # | 問題 | 選項 | 建議 |
|---|---|---|---|
| P2b-D1 | RPC | **A** 公開的 `https://sepolia.base.org`（不用帳號、沒有 API key，有未公開的速率限制）；**B** 需要 API key 的服務（例如 Alchemy），key 存在 0600 的設定檔 | **A**：整個 P2-c 只有十幾筆交易，對帳從部署區塊開始，量很小（對帳每一輪都從部署區塊重掃，所以部署要排在跑場之前不久）。被限速才改 B，改之前再問你。注意：Signer 的預留核對與 relayer 用同一個 RPC，證據裡會寫明這一點 |
| P2b-D2 | testnet 的 Signer 跑在哪裡 | **A** `/opt`（`apa-signer` 帳號，P1-b 隔離），與 V1 `/opt` 部署一起做；**B** 先用開發模式 `var/p2-dev`（你的帳號），之後再搬到 `/opt` | **A**：V1 §15 已寫明開發模式不能當最終驗收的證據；B 要建兩次錢包、領兩次測試幣，證據還要重做 |
| P2b-D3 | 要建哪些金鑰（全部是新的測試金鑰，由你建立） | 付款錢包 wallet-a、wallet-b（`/opt` 的 Signer）；relayer-gas（送 tx#1）；issuer（`registerTask`，**兼部署者**）；policy（只簽 `spend` 的授權，不送交易，不需要 gas）；shop（送 tx#2，**兼收款位址**） | 照這樣，共 6 把。部署者與 issuer 合併、收款與 shop 合併，可以少領兩次 ETH；角色仍各自記錄。Budget 部署後沒有擁有者權限，部署者合併 issuer 不多給權限。日常錢包一律不用 |
| P2b-D4 | relayer 與商店金鑰放哪裡 | **A** `var/p2-testnet/keys`（你的帳號，0700，加密），relayer 三把由 `apa-relayer` 持有，shop 由 demo 在啟動時要密語；**B** 也放進 `/opt`，以 `apa-signer` 帳號執行 | **A**：P2-D2 決定這些是 Host 端的金鑰；放進 `/opt` 會讓 Signer 帳號同時持有預算與商店的金鑰，模糊信任邊界 |
| P2b-D5 | 上限 | Signer：`dlg-ta`、`dlg-tb` 每筆 0.10、總額 0.30；approver-a、approver-b 每筆 1.00、24 小時 1.50；test402 專用委派恰好一筆 0.0001。relayer 每日 12 筆、商店每日 4 筆。每把 gas 帳戶只領一次 ETH，每個付款錢包只領一次 USDC | 照這樣：夠跑一輪 §6，再重跑一次 Allow 或 Ask；超過任何一項就停在那裡，不能自己繞過 |
| P2b-D6 | 測試幣從哪裡來 | 你在瀏覽器上自己領：USDC 用 Circle 的 faucet；ETH 從你選的 faucet（例如 Coinbase CDP） | 照這樣：我不連 faucet，也不經手 faucet 的帳號 |
| P2b-D7 | test402 那一筆怎麼付 | **A** 走同一條 Host 路徑：專用委派 `dlg-t402`（wallet-b，收款方是 test402 的位址，恰好一筆 0.0001），一樣先預留 tx#1、Signer 核對後才簽，test402 自己結算；**B** 另寫一個工具直接請 Signer 簽 | **A**：Signer 規定沒有鏈上預留不簽，B 得另開例外；A 證明的就是產品本身的路徑。細節在 B5 確認 test402 現在的 402 之後再定 |
| P2b-D8 | `cancelAuthorization`（撤回已簽未送的授權） | 做或不做 | **不做**：未知狀態本來就不重付，授權也有期限；多一筆鏈上交易沒有必要 |

## 6 P2-c 執行清單（核准後才做；每一步各自核准）

| 步驟 | 誰 | 碰到什麼 | 交易 |
|---|---|---|---|
| 1 preflight | Claude Code（`p2-testnet preflight`） | RPC 唯讀；test402 一次唯讀 GET | 0 |
| 2 建金鑰 | 你（`apa-relayer keygen` 4 把；在 `/opt` 以 `sudo -u apa-signer` 執行 `apa-signer keygen` 2 把） | 本機 | 0 |
| 3 領測試幣 | 你 | relayer-gas、issuer、shop 各領一次 ETH；wallet-a、wallet-b 各領一次 USDC | faucet 的 5 筆（不是我們送的） |
| 4 部署 | Claude Code（`p2-testnet deploy`，issuer 送出；你輸入密語） | RPC | 2 |
| 5 `/opt` 部署 | 你（依更新後的 V1 §15；sudo 只由你輸入） | `/opt` | 0 |
| 6 跑三場 | 你核准／拒絕；Allow 可由 Claude Code 代按 | RPC；**新的 Host 資料庫**（舊的保留不動） | Allow：tx#0、tx#1、tx#2；Ask 核准：tx#0、tx#1、tx#2；Ask 拒絕：tx#0 |
| 7 test402 | 同上 | test402、RPC | tx#0、tx#1（tx#2 由 test402 那邊送） |
| 8 對帳 | Claude Code（`p2-reconcile.sh`） | RPC 唯讀 | 0；報告是 `base_sepolia`，可結 AE1 驗收 18 |

H6 的第四場「task 結束後不能核准」不在 testnet 重跑：停止點不需要它，本機已證明；而且每個開始的 task 都會先送一筆登記 tx#0（H-D4），多跑一場就多一筆。Host 資料庫一定要用新的：舊資料庫裡還開著的付款 task 會各自觸發一筆登記。

我們自己送的交易合計 **11 筆**：部署 2（issuer）、登記 4 與預留 3（relayer，共 7 筆，在每日 12 筆之內）、結算 2（商店，在每日 4 筆之內）；test402 那一筆的結算由對方送，不算在內。每一筆的 hash、receipt、區塊與對應的 `intent_hash` 都寫進證據；沒有 receipt 的成功宣稱一律記 unknown／not_evaluated。

## 7 停止流程

**遇到就停**：任何付款變成 unknown 或 needs_review；對帳不是 0；交易 revert；碰到 §5 的任何上限；餘額和預期不符；RPC 回錯的鏈。

**怎麼停**（照順序）：
1. 主持人頁停止 run，然後在終端機按 Ctrl-C 停 Host。之後不會再開始新的付款。
2. 停 `apa-relayer`，之後不會再送 tx#1。
3. 停 Signer，之後不會再簽。

不使用 Budget 的 `raiseEpoch`：它只能由 policy 金鑰送出，而 policy 金鑰沒有 gas；已發出但沒用到的預算授權本來就會到期（期限是「現在＋10 分鐘」與付款期限較早的那個）。

**之後**：已簽或已送出的付款不能撤回，只能以對帳收斂。不自動重試、不另開錢包、不追加測試幣。原因寫進證據後，再決定下一步。

## 8 界線

- 只用 Base Sepolia 與測試幣。絕不連 mainnet、不用真錢、不搬入日常錢包。
- 金鑰全部由你建立；密語不出現在命令參數、環境變數、檔案或對話裡。我不讀金鑰檔，也不列出金鑰目錄。
- 在 §6 的對應步驟核准之前，不連 RPC、faucet、facilitator、test402 或任何鏈，不動 `/opt`。
- 不宣稱 mainnet 或 production 安全。

## 9 review（976b307）與處理

結論 REQUEST CHANGES，沒有 Critical。§3 的現況逐行核對無誤。

| 等級 | 問題 | 處理 |
|---|---|---|
| Important | §7 的 `raiseEpoch` 只能由 policy 金鑰送，但它沒有 gas，出事時得再領測試幣 | 拿掉這一步；未用的預算授權本來就會到期 |
| Important | §6 寫「四場」只列三場；取消那場也會送一筆登記，總數會變 12；舊 Host 資料庫裡開著的 task 也各送一筆 | 不在 testnet 跑取消那場，維持 11 筆；規定用新的 Host 資料庫 |
| Important | test402 走 Host 路徑還缺三件事：委派依收款方選、policy 的 merchants、目錄商品 | 寫進 B5 |
| Important | B6 要的錢包與合約位址在 P2-b 還不存在；`/opt` 的語音商家表沒有新的收款方會讓語音核准失敗 | B6 只做產生器與檢查，實際檔案在 §6 第 5 步產生；語音商家表列入 |
| Important | `apa-relayer` 的介面少了恢復時用的 `Lookup` | 補上 |
| 建議 | 每日上限要定義算什麼 | 以 nonce 計，重新定價不另計 |
| 建議 | 對帳每輪從部署區塊重掃 | 部署排在跑場之前不久；寫在 P2b-D1 |
| 建議 | Signer 與 relayer 共用 RPC | 證據裡寫明 |
| 建議 | 沿用金鑰檔格式會讓 relayer 的檔被當成 Signer 錢包解開 | AAD 標籤每個程式各自的 |
| 建議 | 部署者＝issuer 時兩個程序可能用到同一個 nonce | 部署時 `apa-relayer` 必須停著 |
| 建議 | `/opt` 的 `signer.db` 可能已有 `dlg-a`／`dlg-b` 的紀錄 | testnet 用新的委派 ID `dlg-ta`／`dlg-tb` |
| 建議 | `/opt` 的 WebAuthn 來源要含 P2-c 用的 Host 位址 | 寫進 B6 |

第二輪 review（e75ab40）：APPROVE，五項 Important 都已解決。四項建議已寫進 B4–B6：`dlg-t402` 列入產生器、`Starter` 取得收款方的方式與沒有對應委派時的處理、`policy_ttl` 固定 600 並由 demo 檢查、`p2-testnet` 寫新的委派 ID。
