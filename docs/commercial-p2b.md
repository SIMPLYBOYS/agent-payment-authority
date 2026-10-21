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

B3b 的 review（546a990）：REQUEST CHANGES，沒有 Critical；三項 Important 已處理：
- 商店拒絕的原因在終端機上顯示成 `STORE_ERROR` → 沒有代碼的錯誤改以引號、最多 120 字印出（例如 `"shop: settlement_daily_cap"`），付款工作也有測試確認它把原因交給記錄。
- 本人核准面板的說明在 testnet 上仍寫「本機 anvil」→ 說明不再寫網路名稱。
- 委託與核准面板的「網路」一律寫 Base Sepolia（本機 anvil 也用 84532）→ 依主持人頁自己的 `payment_capability` 命名：`Base Sepolia testnet（84532）`、`本機 anvil（chain 84532）`或 `chain 84532（不付款）`。
- 未做：`localhost.`、`127.1` 這類寫法或解析到本機的網域名稱仍能通過「不是這台機器」的檢查（https＋不是 anvil 兩道檢查還在，設定也是操作者自己的）；Unity 兩位客人路徑沒有 testnet 的專屬測試。

**B4 `p2-testnet`**（完成，待 review）：
- `p2-testnet preflight`（唯讀）：RPC 必須是公開的 https、不含帳密、不是這台機器；鏈 84532、不是 anvil；Circle USDC 的 `name`／`version`／`decimals`（必須是 `USDC`、有版本、6 位）；四個角色的位址取自金鑰檔裡明文的位址（**不解密**），列出 ETH 與兩個付款錢包的 USDC 餘額，說明還缺哪些測試幣；`--test402 URL`：對 test402.com 打一次 GET，印出 PAYMENT-REQUIRED 的內容。輸出只有主機名稱，不印 RPC 完整網址。
- `p2-testnet deploy`：同樣的檢查；印出兩筆交易（Z2 verifier、預算合約）的送出者、nonce、gas 與最多花費，issuer 餘額不夠就停；輸入 `yes` 才問 issuer 金鑰的密語並送出。每一筆在送出**之前**寫進 `DIR/deploy.json`，所以 deploy 永遠不會跑第二次；每一筆等 2 個區塊確認，沒等到就停在「不明」、不重送。部署後確認合約的 verifier、issuer、policy 與計畫相同，再寫出 `DIR/relayer.json`（每日 12 筆）與 `DIR/host/`：`payments.json`（`base_sepolia`、Circle USDC、對帳從部署區塊開始、委派 `dlg-ta@1`／`dlg-tb@1`、商店每日 4 筆）、`registry.json`、`profile.json`（merchants 是商店位址）。
- `scripts/demo/p2-testnet.sh keygen|preflight|deploy|relayer` 建置並執行各步；`make present-testnet SIGNER=…` 用 `DIR/host/` 的檔案（`present.sh` 新增 `APA_PRESENT_PROFILE`）。
- 測試：`TestRefusals`（http、這台機器、帳密、別的代幣、目錄權限，全部在連線之前就拒絕）、`TestPreflightAndDeploy`（本機 anvil 冒充，只有測試能這樣：缺幣時的說明、沒輸入 yes 什麼都不送、送兩筆、檔案內容與權限、不印密語與 RPC 網址、不能跑第二次）。沒有任何測試連外。
- 部署用的 Z2 verifier 對應 repo 裡的 `zk/setup`（測試用的可信設定），與 P2-a 相同；這是 testnet，不宣稱 production 安全。

B4 的 review（b8bda11）：REQUEST CHANGES，沒有 Critical；五項 Important 都已處理：
- RPC 網址可能出現在錯誤訊息裡（網址的路徑或參數可能帶 API key）→ `eth.Client` 的連線錯誤只寫方法名稱與原因，不寫網址；demo、relayer、商店一併受益。
- 有些檢查要到兩筆交易都上鏈後才做，失敗了也無法續做 → registry、profile、`zkbudget` 與 `zk/setup` 都在輸入 `yes` 之前檢查；每筆交易確認後把合約位址與區塊寫進 `deploy.json`；兩筆都已確認但檔案還沒寫時，再跑一次 deploy 會重新讀 receipt、確認合約，然後只寫檔、**不送任何交易**；沒做完的紀錄一律拒絕，請你先查。
- 「最多花費」少算了 → 以實際送出的 gas 上限計算，再乘 2 涵蓋 Base 的 L1 資料費；輸入密語後再讀一次 base fee，fee cap 不到它的 1.5 倍就不送。
- `--test402` 讀不了真的回應（未知欄位會被拒）→ 先以 `x402.PaymentRequired` 解，再寬鬆地讀每個 accepted payment；遠端字串一律加引號印出。
- `make present-testnet` 會沿用這個 Signer socket 既有的 Host 資料庫 → 場次與 Host 資料庫改放 `var/p2-testnet/present/`，是新的。
- 建議也照做：腳本的目錄改為絕對路徑；deploy 前確認 `contracts/src/Verifier.sol` 與 `zk/setup/Verifier.sol` 相同、合約建置比原始碼新（不在腳本裡跑 forge，避免它去下載編譯器）；node 的版本字串加引號。

**B6 `/opt` 的產生器**（完成，待 review）：`p2-testnet opt-signer`（見 §6.1）：只讀兩個檔，不讀金鑰、不連任何節點；產生的設定以 `signer.ParseConfig` 驗證後才以 0600 寫出，目標檔已存在就拒絕；`--base` 的 WebAuthn 必須是 `localhost` 且含 `http://localhost:8402`。測試 `TestOptSigner`：保留與替換的每一段、`dlg-t402` 恰好一筆 0.0001、核准者版本加 1、語音商家改到商店位址、不覆寫、別的來源被拒。§6.1 寫出第 2 步與第 5 步在 `/opt` 的做法；實際的執行檔、設定與雜湊在第 5 步才產生。

B6 與 B4 第二輪的 review（af668a1、a4e79e9）：REQUEST CHANGES；沒有任何路徑會簽超過上限，B4 的修正都成立。
- Critical：`opt-signer` 讀不了 `/opt` 現在的 A2 設定（核准者沒有 `signer_total`）→ 改為先嚴格解碼、補上上限後才整份驗證。
- Important：沒有語音段時會產生沒有語音的設定（核准的收款方就不受 Signer 檢查）→ 一律拒絕，語音段可由 `--voice` 取自 V1 準備好的設定；§6.1 漏了聲紋登記 → 補上第 7 步；test402 的解碼仍有一半是嚴格的 → 整個 header 寬鬆讀取，讀不了時把內容（加引號、截斷）印出以便記錄。
- 建議也照做：`opt-signer` 確認 `payments.json` 是 Circle USDC、https 節點；語音商家必須恰好一個；小數位數設為 6；`deploy.json` 等檔案改為寫暫存檔再改名（當機不會留下半份 journal）；網址無法解析時的錯誤也不帶網址。

第二輪（de0a9ad）：APPROVE。建議也照做：語音 socket 必須在 Signer 自己的 `/opt/apa-signer` 之下（拒絕別台機器或 repo 的語音段），輸出列出語音 socket 與模型以便第 5 步前核對；補上網址無法解析時的測試。

**B5 test402**（完成，待 review；Aaron 10/20：B5-D1 A、B5-D2 A）：
- 契約（B5-D1，c69d2df，review APPROVE）：報價裡的 asset、payTo 可以是任一大小寫，比對時視為同一個位址；綁定仍一律小寫；bridge 組單時把位址轉成小寫。付款的 `authorization.to` 照報價的寫法送出（簽章只涵蓋位址，不涵蓋大小寫），對方若逐字比對也相同。apa.pay.v2 共用同一個 Validate，同樣適用。
- 委派依收款方選：`payments.json` 的 `delegations` 可寫 `consumer/payee`，先找這一行，再找 consumer 自己的；testnet 一律寫成 `consumer/payee`，所以沒有對應那一行的購買不會開始付款（不會預留）。Store 的 `PayTask` 帶上每筆 Allow 的收款方。
- 外部商品：`merchant.External`：資料商店的目錄列出 test402，resource URL 是 test402 的，報價是 B5-D2 存下的原文；這家商店不提供它的 resource 也不回它的 402。demo 的 `payments.json` 的 `test402_offer`（只限 `base_sepolia`）讀那個檔，以 bridge 讀 402 的同一套規則檢查（一個報價、resource 就是要的 URL、host 是 test402.com）。
- `p2-testnet deploy`：`DIR/test402-offer.json` 在時，profile 的 merchants 加入 test402 的收款方（必須是 Circle USDC 的付款）、`payments.json` 加 `test402_offer` 與 `consumer-b/0x…0402 → dlg-t402@1`；`opt-signer` 從這一行產生 `dlg-t402`（恰好一筆 0.0001），不再需要 `--test402-pay-to`。
- 目錄：`testdata/demo/scenarios-testnet.json`（P2 的四場，標為 testnet，加上 consumer B 的 `t402-b`）與 `testdata/cli/test402.json`；`make present-testnet` 用它。
- 測試：`TestQuoteAddressInEitherCase`、`TestEIP55QuoteGetsALowercaseBinding`、`TestStarterByPayee`、`TestPaymentSignatureKeepsTheQuotesCase`、`TestExternalProductListedNotServed`、`TestTest402Product`、`TestTestnetPaymentsOpen`（有 test402 時商店多一個報價）、`TestTestnetShippedCatalog`、`TestPreflightAndDeploy`（test402 的三處）、`TestOptSigner`（`dlg-t402` 由委派產生）。
- 未知：test402 那邊的 facilitator 如何驗證我們的付款，要到第 7 步實際付款才知道；不通過的話會是「驗證未通過，沒有送出」，只有 tx#1 用掉。

B5 的 review（11397d5）：REQUEST CHANGES，沒有 Critical；付款路徑都成立（consumer A 買 test402 不會開始付款、不會預留；dlg-t402 只有 wallet-b 恰好一筆；test402 當下的 402 與存下的不同時被拒；對帳把 test402 那筆算為已授權）。一項 Important 已處理：`get_quote` 會把外部 402 的 amount、asset、network 原樣交給 Agent → 改為與目錄文字一樣清理並截斷。建議也照做：付款工作連商店的 HTTP client 不用環境變數的 proxy、header 有上限；`base_sepolia` 的委派一律要是「客人／收款方」；deploy 確認存下的 402 就是 §6.2 記錄的（`/api/x402`、payTo `0x…0402`），否則不送；測試也檢查 dlg-t402 的收款方與客人。未做：test402 列在資料商店的目錄裡，consumer A 的 Agent 若請購它，會記為 Allow 但不付款（沒有對應的委派）；腳本化的 fake 場次不會這樣做。

B5 的阻礙（10/20，第 1 步之後；已由 B5-D1 A 解決）：test402 的 402 以 EIP-55 大小寫混合的寫法給出 asset（`0x036CbD53…CF7e`），而 apa.pay.v1 要求付款綁定的位址一律小寫，且報價裡的 `asset`、`payTo` 必須與綁定**逐字**相同（`internal/paycontract` 的 Validate；Store、Signer、bridge 共用）。所以現在的契約無法表示 test402 的報價，請購會在組單時被拒（不會花到錢）。要繼續有兩件事要 Aaron 決定，見對話（B5-D1 契約比對大小寫、B5-D2 再取一次 402 存下原文作為 Host 報價簿的固定報價）。

**B5 test402**（暫停，等 §6 第 1 步）：Agent 只能經由商店的目錄請購，而且 bridge 會自己去取那筆 402；test402 的商品等於讓 bridge 信任一個外部的 resource URL，Store 的報價簿也要放進 test402 **現在**實際回的報價（要與它逐欄相同，否則 test402 會拒絕，雖然不會花到錢，但 tx#1 已用掉）。所以 B5 要在第 1 步（唯讀：preflight 與對 test402 的一次 GET）核准並取得現在的 402 之後才設計與實作；其他各段不受影響。要做的事已列在 §4 B5：委派依收款方選、policy 的 merchants、目錄商品，以及 bridge 對外部 resource 的信任規則。

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

### 6.1 第 2 步與第 5 步在 `/opt` 的做法（B6）

sudo 一律由你輸入；我不執行 sudo、不動 `/opt`。`$P` 是 repo 目錄。

**第 2 步的 `/opt` 部分：兩個付款錢包**（在部署之前，因為 `p2-testnet deploy` 要它們的位址）
- 用 `/opt` **現有的**執行檔建立：`sudo -u apa-signer /opt/apa-signer/bin/apa-signer keygen --dir /opt/apa-signer/data --wallet wallet-a`，再 `--wallet wallet-b`，兩把用**同一個**密語（`serve` 只問一次）。金鑰檔格式與新版相同（B1 的固定測試向量證明新版打得開舊格式）。
- 印出的 `wallet … address 0x…` 兩行可以貼回（位址是公開的）。若 `/opt/apa-signer/data/keys` 已有同名檔，keygen 會拒絕，什麼都不寫；那時先問我。

**第 5 步：`/opt` 部署**（第 4 步部署完成、`var/p2-testnet/host/` 已寫出之後）
1. 建置（W-host，mac）：從乾淨的 worktree 建置 `apa-signer`（`GOFLAGS=-mod=readonly GOPROXY=off GOTOOLCHAIN=local`）到 `var/p2-opt/apa-signer`，記下 SHA-256；語音檔案依 V1 §15 重新產生（`scripts/demo/v1-opt-stage.sh`）。
2. 取得目前的設定：`sudo cat /opt/apa-signer/data/signer.json > $P/var/p2-opt/signer.base.json`（`var/p2-opt` 是 0700）。
3. 產生新設定：`var/p2-testnet/bin/p2-testnet opt-signer --dir var/p2-testnet --base var/p2-opt/signer.base.json --voice var/v1-opt/signer.json --out var/p2-opt/signer.json`。deploy 寫進 `payments.json` 的 test402 那一行（`consumer-b/0x…0402`）會自動產生 `dlg-t402`，不必另外指定。`/opt` 現在是 A2 的設定（核准者沒有 P2 的上限、沒有語音段），所以語音段取自 V1 準備好的 `var/v1-opt/signer.json`；沒有語音段一律拒絕（語音是 Signer 檢查「核准的這筆付給誰」的唯一一關），語音商家也必須恰好一個。它保留 socket、callers、核准者與 WebAuthn，換掉錢包、資產、委派（`dlg-ta`、`dlg-tb`、`dlg-t402`）、核准者上限（版本加 1：當時進行中的核准挑戰會回 `APPROVER_CHANGED`，已登記的裝置不受影響）、語音的商家位址與小數位數，並加上 `reservation` 段；印出新檔的 SHA-256。
4. 停止與備份：照 V1 §15 的「開始前」與 D 部署第 1、2 步（`pgrep -fl '/opt/apa-signer/'` 應無輸出；`signer.json`、`approvals.db`、執行檔各留一份備份，不刪）。
5. 安裝：照 V1 §15 D 第 3 步安裝執行檔與語音檔，`signer.json` 改用 `var/p2-opt/signer.json`；第 4 步核對新的雜湊。
6. 核准表遷移（只有 `/opt/apa-signer/data/approvals.db` 還在時）：`sudo -u apa-signer /opt/apa-signer/bin/apa-signer approver migrate --dir /opt/apa-signer/data`；舊檔留為 `approvals.db.migrated`。
7. 聲紋登記：語音服務照 V1 §15 S1 啟動後，照 V1 §15「V」為要核准 Ask 的核准者登記聲紋（approver-a；approver-b 也要核准時一樣）。`/opt` 的 A2 沒有登記過，不登記的話每一筆 Ask 的核准都會回 `VOICE_NOT_ENROLLED`。
8. 啟動：Signer 改用 **`serve`**（不是 `serve-approvals`）：`cd /; sudo -u apa-signer /opt/apa-signer/bin/apa-signer serve --dir /opt/apa-signer/data`，輸入錢包的密語。預期 `apa-signer: serving on /opt/apa-signer/run/signer.sock (reservations checked on chain 84532; approvals on the same socket)`，這一行可以貼回。
9. 之後第 6 步：`sh scripts/demo/p2-testnet.sh relayer`（另一個終端機，輸入 relayer 金鑰的密語），再 `make present-testnet SIGNER=/opt/apa-signer/run/signer.sock`（輸入商店金鑰的密語）。

回滾：停掉 Signer，把第 4 步的備份放回原位（舊執行檔仍能打開補過表的資料庫，V1 §15）；testnet 上已發生的交易不能撤回，照 §7 處理。

### 6.2 第 1 步的結果

**test402 的 402**（Aaron 10/20 核准第 1 步；Claude Code 以 `p2-testnet test402` 執行，2026-10-20T02:05:08Z，一次 GET、不跟隨轉址、不帶金鑰、不付款。第一次嘗試在沙箱裡憑證驗證失敗，連線沒有送出任何請求，改在沙箱外重試同一次）：

| 欄位 | 值 |
|---|---|
| x402 版本、scheme、網路 | 2、`exact`、`eip155:84532` |
| amount | `100`（0.0001 USDC） |
| asset | `0x036CbD53842c5426634e7929541eC2318f3dCF7e`（Circle USDC，與 §2 相同） |
| payTo | `0x0000000000000000000000000000000000000402` |
| maxTimeoutSeconds | 300 |
| extra | `name` `USDC`、`version` `2`、`assetTransferMethod` `eip3009` |

- payTo 是一個好記的位址（結尾 `0402`），很可能沒有人持有它的私鑰：付過去的 0.0001 測試 USDC 等於燒掉，這對證明相容性沒有影響。
- test402 自己宣告的 EIP-712 version 是 `2`；部署前 preflight 仍會從鏈上讀 `version()` 核對。
- B5 的 `dlg-t402` 收款方就是這個 payTo。第二次 GET（B5-D2，2026-10-20T02:16:29Z）把 402 原文存於 `var/p2-testnet/test402-offer.json`，內容與第一次相同；deploy 看到這個檔就把 test402 加進 profile 的 merchants、`payments.json` 的 `test402_offer` 與委派。

**鏈上的唯讀 preflight**（Aaron 10/21 執行 `p2-testnet.sh preflight`，貼回輸出）：
- 鏈 84532，`sepolia.base.org`，節點 `reth/v2.5.2-7134be3/x86_64-unknown-linux-gnu/base/v1.5.0`（不是 anvil），區塊 48373279。
- Circle USDC `0x036cbd53842c5426634e7929541ec2318f3dcf7e`：name `USDC`、version `2`、decimals 6，與 test402 宣告的 domain 相同。
- 第 2 步的六個位址：relayer-gas `0x83ee3c45b42834d31ec0832b47d8ec4c4b71e9d2`、issuer `0xccd7bec96111b217ccc92632e6e0173878ab4c6a`、policy `0xe27fb36cf960ec633da6ef05dd8c5106e7fe05fd`、shop `0x30d9c45f7bab98561004d25b27aed474180a7b99`、wallet-a `0x245664a4bfb21a9e9d47f232f6d691223b833554`、wallet-b `0xba5f348020f9ec76c4a5a24dea20f4d7c0828667`；餘額全部為 0（還沒領測試幣）。
- 第 1 步完成。

**第 3 步：領測試幣**（Aaron 10/21，在瀏覽器自己領；每個位址一次）：
- 測試 ETH：Coinbase CDP 的 faucet。官方文件寫的 `portal.cdp.coinbase.com/products/faucet` 已失效（Page not found）；現在在 CDP Portal 左側 **Onchain Tools → Faucet**（路徑 `/onchain-tools/faucet`），每次 0.0001 ETH、每天最多 0.1 ETH。relayer-gas、issuer、shop 各領一次。
- 測試 USDC：`https://faucet.circle.com`（Base Sepolia），wallet-a、wallet-b 各領一次，各 20 USDC。
- 之後的 preflight（區塊 48374673）：relayer-gas、issuer、shop 各 0.000100 ETH；policy 0；wallet-a、wallet-b 各 20.000000 USDC；`ready for deploy`。第 3 步完成。

**第 4 步：部署**（Aaron 10/21 執行 `p2-testnet.sh deploy`，看過計畫後輸入 yes；issuer 送出）：
- 計畫：verifier（nonce 0，gas 上限 1442492）、預算合約（nonce 1，gas 上限 761796）；最多 0.000048 測試 ETH（fee cap 11000000 wei），issuer 持有 0.0001。
- Z2 verifier `0x5c262a916c02c6afffa57ad591ebd8c457474751`，區塊 48374723，交易 `0x22e9682ff0de8075e72f42f8a18227a6d3f7ed5954dab816242d2111a18a6190`。
- 預算合約 `0xeb5d2964082c33b13f7e5f841495f86a928b31dd`，區塊 48374726，交易 `0x5ffd2b4b7f5327d6cdfab932ed1d5a3fbb1cdc0eacd7f0b065986a04d6b0d972`；部署後核對 verifier、issuer、policy 與計畫相同。
- 寫出的檔：`var/p2-testnet/relayer.json`（每日 12 筆、確認 2）、`var/p2-testnet/host/payments.json`（對帳從區塊 48374723 開始；委派 `consumer-a/<shop>`→`dlg-ta@1`、`consumer-b/<shop>`→`dlg-tb@1`、`consumer-b/0x…0402`→`dlg-t402@1`；`test402_offer`）、`registry.json`、`profile.json`（merchants：商店與 test402）。Claude Code 讀過這些公開設定核對無誤（未讀金鑰目錄）。
- 第 4 步完成；我們自己送的交易目前 2 筆（§6 預計 11 筆）。

**第 5 步的準備**（Claude Code，10/21，不動 `/opt`）：`var/p2-opt/apa-signer` 從乾淨的 worktree 建置（commit `c0bc0c5fd91861e2fd1d1f4d155b351449267d7f`，`GOFLAGS=-mod=readonly GOPROXY=off GOTOOLCHAIN=local`），SHA-256 `85c240c00b7607ef90e53601a17312727b506e0d09c5ea10ef2bcb1829a14edc`。語音服務的檔案自 V1 準備時（f81517a）沒有變動，沿用 `var/v1-opt/stage`。

**第 5 步的實際經過**（Aaron 10/21）：核對、產生新 `signer.json`（SHA-256 `c7f6c47681e697a6ec51a565daeeb931bf7cb07b793506a9cbdd72199f743c19`：兩個錢包、3 筆委派、2 位核准者、鏈 84532 的預留核對、語音段）、備份三個 `.a2`、安裝、核准表遷移（credentials 2、approver_handles 2、其他紀錄各 4；舊檔留為 `approvals.db.migrated`）都照 §6.1 完成。遇到兩個問題：
- 語音服務載入失敗：模型的 `label_encoder.ckpt` 是指向 repo 內絕對路徑的連結，`apa-signer` 讀不到（V1 只以 mac 驗證過，所以沒發現）。Aaron 在 `/opt` 改為指向同目錄 `label_encoder.txt` 的相對連結，並確認 `/opt/apa-signer/voice` 沒有其他絕對連結；health `ok`。`v1-opt-stage.sh` 改為複製時把連結換成檔案，並拒絕任何絕對連結（5c09328）。approver-a 的聲紋登記完成（3 段，存於 `signer.db`）。
- `serve` 只問一次密語，wallet-b 當初用了另一個密語，啟動被拒（沒有簽任何東西）。Aaron 記得 wallet-b 的密語，選擇加上 `apa-signer rekey`（3dee9b4，review APPROVE，修正 261cfc8）：用目前的密語解開、確認位址不變，以新密語重新加密，寫入的檔案讀回驗證後才改名取代，並同步目錄。執行檔因此重建：commit `261cfc87828bde415950f96e8601fa0d00da088f`，SHA-256 `560e32fa161e505f280aa34a0b26b107f2646c888f10cf9ba32f01da6e1ac4c8`（取代 `85c240c0…`）。
- Aaron 安裝新執行檔、以 `rekey` 把 wallet-b 改為與 wallet-a 相同的密語，再以 `serve` 啟動：health `{"state":"ready","payment":"available"}`（兩把錢包都已解開、預留核對在鏈 84532）。第 5 步完成。

**第 6 步前：換掉 shop 金鑰**（10/21）：啟動 Host 時 shop 金鑰的密語不對，Aaron 確定想不起來，那把金鑰無法再打開（位址 `0x30d9c45f7bab98561004d25b27aed474180a7b99` 上的 0.0001 測試 ETH 留在那裡；還沒有任何付款付到它）。Aaron 選 A：維持 P2b-D3（商店自己付 gas、自己收款），換一把新金鑰：
- Aaron 把舊檔改名為 `keys/shop.key.lost-20261021`（不刪），以 `apa-relayer keygen --role shop` 建立新的：shop `0x6bbe84371eab1a980ba862b90474a7e8e9bb773e`。
- Claude Code 把 `host/payments.json`（`pay_to` 與 `consumer-a/…`、`consumer-b/…` 兩行委派）、`registry.json`（`merchant_pay_to`）、`profile.json`（merchants）中的舊商店位址換成新的；原檔留為 `*.before-shop-change`。合約、relayer 與 test402 那一行都不受影響。
- 之後：新位址領一次測試 ETH；以 `opt-signer` 重產 `/opt` 的 `signer.json`（dlg-ta、dlg-tb 的收款方與語音商家改為新位址），重裝並重啟 Signer。
- Aaron 為新 shop 領了 0.0001 測試 ETH，重產並安裝 `/opt` 的 `signer.json`（SHA-256 `b3dbb4e1374b882ab8d2c76d3602cffdf13ee56c4b80727292ff6a1f8a23fd97`；dlg-ta、dlg-tb 與語音商家改為新 shop），重啟 Signer。

**第 6 步前：公開節點的 `eth_getLogs` 上限**（10/21）：Host 就緒後，對帳回 503 `chain_unavailable`。原因是 `sepolia.base.org` 每次 `eth_getLogs` 最多 200 個區塊（`-32614 eth_getLogs is limited to a 200 range`），而 Reconciler 一次問 2000。這也會讓付款工作永遠判斷不了「已付款」（它在 `Status` 也讀 log），所以第 ① 場之前先修：
- 每次 `eth_getLogs` 改為 200 個區塊。
- `Status` 只讀那筆授權可能被用掉的區塊：EIP-3009 只在 `validAfter` 之後、`validBefore` 之前有效，以區塊時間二分搜尋找出這段（一筆付款的窗口最多幾百個區塊），不再從部署區塊讀到現在；`Authorization` 因此多了 `validAfter`。
- 完整的對帳報告仍從部署區塊讀起（每個錢包每 200 區塊一次），`p2-reconcile.sh` 等候上限改為 600 秒；主持人頁的對帳列只等 5 秒，可能顯示「未確認」，以報告為準。
- 測試：`TestUnderABlockRangeCap`（本機 anvil 前放一個同樣拒絕超過 200 區塊的代理：付款狀態只讀它自己的窗口，報告分段讀完）；突變 2 項。
- review（b12fcbc）APPROVE；補上窗口兩端的測試（`TestStatusWindowEdges`），授權已用但在它之後的窗口內沒有事件（例如被取消）時改回交人工處理（cc5f564）。
- 主持人頁每幾秒就問一次對帳，而一次完整對帳要從部署區塊讀起（約每錢包一百次查詢）：每問一次就重讀整條鏈會壓垮公開節點、拖累付款判斷。改為同一時間只讀一次、不受頁面等待中斷、結果保留一分鐘；第一次還沒讀完時回 503 `reconciliation_pending`，頁面顯示「未確認：對帳計算中」，`p2-reconcile.sh` 每 10 秒再問。測試 `TestCachedReconciliation`。

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
