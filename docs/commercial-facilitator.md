# 獨立的 facilitator（x402 v2 `/verify`、`/settle`）

> 2026-11-03，Claude Code。Aaron 11/3：FUTUREMODE 筆記裡還沒處理的項目「依照建議順序進行」，這是第 1 項。筆記 §3.2：facilitator 的 verify 唯讀、settle 才改變狀態；§5.4：VALIDATE→MATCH→RESERVE→SETTLE。
> **Aaron 11/3 的決定**：F1 A（主路徑用自己的 `apa-facilitator`，x402.org 可切換）；F2 做一次（一筆經 x402.org 結算的相容性證明，不是每場 demo 都走它；真的送出前再核准一次）；F3 不需要（不跑 S3，面試現場用 S5 的 `testnet-town-t4`）。「從 FA1 開始」。

## 1 現況

P2-D3 選 A：商店自己驗證並結算（`internal/merchant/settle.go` 的 `Settler`，與商店同一個程序）：

- 驗證：嚴格解析 exact-EVM payload；重算 EIP-712 簽章；查餘額、`authorizationState`（nonce 沒用過）；`eth_call` 先模擬一次。
- 結算：先寫進自己的資料庫（以付款人＋授權 nonce 為鍵）再送出 `transferWithAuthorization`；同一份 payload 再來，回同一筆交易，不會送第二筆；同一個 nonce 換了內容，回 `nonce_used`；結果不明時回 pending，之後接著查。
- 確認數夠了才交付（P2-D6）；每日交易上限（P2b-D5）；gas 金鑰在商店。
- **查證不看誰送出 tx#2**：checker 的 E4、E8 與對帳程式看的是代幣自己的事件（`Transfer`、`AuthorizationUsed`），所以改由 facilitator 送出不影響它們。

缺的是 x402 的分工：沒有獨立的 facilitator 服務，也沒有分開的 `/verify` 與 `/settle`；從來沒有經過外部 facilitator 結算（test402.com 那一筆只驗證 wire 相容，它不結算）。

## 2 x402 v2 的 facilitator 介面（11/3 查證）

來源：[x402 v2 規格](https://github.com/coinbase/x402/blob/main/specs/x402-specification-v2.md)。

| | 請求 | 回應 |
|---|---|---|
| `POST /verify`（唯讀，不改任何狀態） | `x402Version`（2）、`paymentPayload`、`paymentRequirements` | `isValid`；`invalidReason`（無效時）；`payer` |
| `POST /settle`（改變狀態） | 同上 | `success`；`errorReason`（失敗時）；`payer`；`transaction`（沒送出就是空字串）；`network`（CAIP-2）；`amount` |
| `GET /supported` | — | `kinds`（`x402Version`、`scheme`、`network`）、`extensions`、`signers` |

- `errorReason` 是 `settlement_pending` 時，`transaction` 一定要有值：正好是我們現在「結果不明，接著查」的做法。
- **x402.org 公開 facilitator**（`https://x402.org/facilitator/supported`，11/3 讀取）：支援 v2 `exact` 在 `eip155:84532`（Base Sepolia），送交易的是它自己的地址 `0xd407…f1bf`。

## 3 做法

```text
Agent → Host（付款流程）→ 商店：PAYMENT-SIGNATURE
                              │
                              ├─ POST /verify   ──→ facilitator（唯讀）
                              ├─ POST /settle   ──→ facilitator ──→ transferWithAuthorization（tx#2）
                              └─ 自己查確認數（唯讀 RPC）→ 交付＋收據
```

- **新的 `apa-facilitator`**（`cmd/apa-facilitator`，程式在 `internal/facilitator`）：把 `Settler` 的驗證與結算搬過去，照第 2 節的介面提供 `/verify`、`/settle`、`/supported`；它持有 gas 金鑰與結算資料庫（0600）；只聽本機。
- **商店**：不再持有 gas 金鑰。收到 `PAYMENT-SIGNATURE` 後，以這個商品的報價當 `paymentRequirements`，先 `/verify`，通過才 `/settle`；`/settle` 成功後，商店用自己的唯讀 RPC 查確認數，夠了才交付（P2-D6 不變）。
- **設定**：每個環境一個 `facilitator_url`。本機指向自己的 `apa-facilitator`；testnet 預設也是，改成 `https://x402.org/facilitator` 就走公開的。
- **不重複付款**：自己的 facilitator 以付款人＋nonce 去重（同現在）。走 x402.org 時，同一份授權的 nonce 上鏈只能用一次；`/settle` 逾時當成結果不明（不重送新的授權），由對帳程式看鏈上的 `AuthorizationUsed` 決定。
- **查證**：E4、E8 不變；證據包多記一項「這一場用的 facilitator」（網址與它送交易的地址），checker 照實列出。

## 4 規模

| 切片 | 內容 |
|---|---|
| FA1 | `internal/facilitator`：三個端點，建在商店原本的 `Settler` 上；本機 anvil 測試（驗證唯讀、結算去重、pending 後接著查） |
| FA2 | `cmd/apa-facilitator`（金鑰載入、只聽本機）；商店改呼叫 facilitator；設定 `facilitator_url`；`demo-local.sh`、`demo-testnet.sh` 多啟動一個 `apa-facilitator`（gas 金鑰從商店移過去）；開場前檢查加一項 |
| FA3 | x402.org：`/supported` 檢查；**一筆**真的經 x402.org 結算的 testnet 付款（要 Aaron 核准） |
| FA4 | 證據包記 facilitator；文件、瀏覽器檢查、本機彩排一次 |

## 5 要 Aaron 決定

| # | 問題 | 建議 |
|---|---|---|
| F1 | 主路徑用誰的 facilitator | **A** 本機與 testnet 都用自己的 `apa-facilitator`（主路徑不依賴外部服務，保留 P2-D3 的理由），x402.org 可以切換；**B** testnet 主路徑改用 x402.org。建議 **A** |
| F2 | 要不要做一筆真的經 x402.org 結算的 testnet 付款 | 要：取代 test402「只驗證 wire、不結算」，證明跟官方 facilitator 真的相容。只做一筆自動放行的小額（例如孩子的講義 0.06 test USDC），gas 由 x402.org 付 |
| F3 | 跟 testnet 一家人那場（S3）的先後 | `signer-4.json` 的委派 **11/6 台北 13:23** 到期。**A** 先跑 S3（現在的程式），facilitator 在另一個 worktree 開發，不影響 S3；之後用新的委派做 FA3；**B** 先做完 facilitator 再跑 S3，S3 的證據就有 facilitator，但時間很趕，而且付款主路徑剛改完就上 testnet。建議 **A** |

## 6 實作

**FA1（11/3）**
- `merchant.Settler` 拆出兩個公開方法：`Check`（只做結算前的所有檢查，不讀寫自己的資料庫、不送交易：`/verify`）與 `Settle`（以報價與「付的是什麼」的鍵結算：`/settle`）；商店原本的 `Pay` 改為呼叫 `Settle`，行為不變。`merchant.SettlementResponse` 改成 `x402.SettleResponse` 的別名（PAYMENT-RESPONSE 本來就是 facilitator 的 SettleResponse）。
- `internal/x402`：facilitator 的請求與回應型別（`FacilitatorRequest`、`VerifyResponse`、`SettleResponse`、`SupportedResponse`），照第 2 節。
- `internal/facilitator`：`POST /verify`、`POST /settle`、`GET /supported`。只結算 `exact`、自己這條鏈、這個代幣、這一個收款人；`paymentPayload.accepted` 必須與 `paymentRequirements` 完全相同（canonical JSON）；兩處的 `x402Version` 都要是 2。結果的對應：確認數夠了 `success: true`（帶 `amount`）；送出了還沒確認 `settlement_pending`（帶交易編號，同一個請求再來就接著查）；什麼都沒送出又是節點的問題回 503；拒絕帶規格的原因、什麼都不送。「付的是什麼」的鍵：payload 的 `resource.url`，沒有就用報價的雜湊；同一份授權換了鍵就是重放（`nonce_used`）。
- 測試（本機 anvil，`internal/facilitator` 5 項）：`/supported` 的內容；`/verify` 不送任何東西（gas nonce 與餘額不變，之後照樣能結算）；七種不結算的請求在兩個端點都被拒絕、什麼都不送；同一份授權只付一次、同一個請求再來是同一筆交易、換資源是重放；確認數不夠時 `settlement_pending`，夠了同一筆交易成功。反向驗證：拿掉「accepted 必須等於報價」或讓 `/verify` 去結算，測試都會失敗。商店、x402、Host 原有的測試（234 項）全過。
- 還沒做（FA2）：可執行的 `apa-facilitator`、商店改走 HTTP 呼叫它、腳本與設定。

**FA2（一）程式（11/3）**
- 商店：`merchant.Facilitated`（`internal/merchant/facilitated.go`）先 `POST /verify`，有效才 `POST /settle`；結算成功後，商店自己用唯讀 RPC 查交易的確認數，夠了才交付（P2-D6 不變）。同一筆付款再來，只重查確認數，不會再呼叫 `/settle`。facilitator 沒回應：pending、什麼都不送；`/verify` 拒絕：402 帶它的原因。商店建立報價時讀的鏈、代幣、收款人，由 `merchant.Settlement` 介面提供（商店自己的 `Settler` 或 `Facilitated`）。
- Host：付款設定多 `facilitator_url`，只接受本機迴路位址上的 `apa-facilitator`（`http://127.0.0.1:PORT`）或 Base Sepolia 上的 `https://x402.org/facilitator`。有設定時，Host 不載入商店金鑰、不問它的密語；啟動前 `GET /supported` 必須列出這條鏈的 v2 `exact`，否則不啟動；開場前檢查的「商店今天送了幾筆」改問 `apa-facilitator` 的 `GET /health`（x402.org 沒有，不顯示）。沒有設定時照舊在 Host 裡結算。
- `cmd/apa-facilitator serve --payments FILE [--listen 127.0.0.1:8403]`：讀 Host 同一份付款設定檔裡需要的欄位；testnet 的規則與 Host 相同（https 的公開節點、至少 2 個確認、每日上限、`shop_key_dir`）；只聽本機迴路位址；金鑰在 testnet 是 `shop_key_dir`（密語從終端機問），本機是 `anvil_keys.shop_gas`；結算資料庫是設定檔 `dir` 裡的 `settle.db`（0600）；`GET /health` 回今天送了幾筆與每日上限。
- 測試：`TestPaidOnAnvil` 分成兩條，同一筆付款在本機 anvil 上，一條商店自己結算、一條經 facilitator 結算，都交付；商店端 3 項（facilitator 沒回應、`/verify` 拒絕、確認數夠了才交付且只呼叫一次 `/settle`）；Host 的設定規則（5 種 `facilitator_url`）與經 facilitator 啟動（不問商店密語、facilitator 不支援這條鏈或沒回應時不啟動、額度來自 facilitator）；`apa-facilitator` 在本機 anvil 上啟動與 6 種拒絕啟動。相關套件 270 項全過。

**FA2（二）腳本（11/3）**
- `present.sh`：付款設定的 `facilitator_url` 是 `http://127.0.0.1:PORT` 時，先建置並啟動 `apa-facilitator`（同一份設定檔、那個位址；輸出寫到場次目錄的 `facilitator.log`），`/supported` 回應了才啟動 Host；那個位址已有程式在回應就不啟動。Host 停了、或按 Ctrl-C，facilitator 一起停。`facilitator_url` 是 x402.org 或沒有設定時，什麼都不多啟動。
- 新寫的付款設定都帶 `facilitator_url: http://127.0.0.1:8403`：`p2-local`、`p2-testnet deploy`、`p2-testnet e1a-host`（新的場次標籤，`make demo-local` 的 live 也是用它建 `live-標籤`）。`demo-local.sh` 的 fake 每次另寫一份 `場次/host/payments.json`（鏈的檔加上 `facilitator_url`，0600），所以沿用保留的鏈也經 facilitator；鏈自己的檔不動。
- **保留下來的設定檔照舊在 Host 裡結算**：`var/p2-testnet/host`（`make demo-testnet` 的 fake）、已有的 `testnet-town-t1`～`t4`、之前建好的 `live-標籤`。它們的證據包是那時候的做法，不改寫；要經 facilitator 就用新的標籤。
- testnet 上經 facilitator 時，商店金鑰的密語在 Host 啟動前、由 `apa-facilitator` 問（`apa-facilitator shop key passphrase:`），Host 不再問。
- 啟動前檢查：`demo-local.sh`、`demo-testnet.sh` 多查 8403 有沒有被占用。
- `apa-facilitator` 只在有 `shop_key_dir` 時要求金鑰等於 `pay_to`：本機的 `pay_to`（`0x3333…`）沒有金鑰，gas 由 anvil 的 key 4 付，與原本 Host 裡結算相同。
- 驗證：本機新鏈上跑一次 `p2-local` 與 `apa-facilitator`：設定檔帶 `facilitator_url`；`/supported` 列出 `eip155:84532` 的 `exact`；`/health` 就緒、今天 0 筆；錯誤的 `/settle` 回 400；`settle.db` 是 0600。相關 8 個套件全過。**還沒跑**：完整的 `make demo-local`（要 Signer 的密語，Aaron）。

**FA4 程式：證據包記 facilitator（11/4）**
- 證據包的 start（`e1-evidence start`，跑之前記錄）多一項 `facilitator`：host 目錄付款設定裡的 `facilitator_url`。沒有這一項，就是 Host 裡的商店自己結算（之前的證據包都是）。`e1-evidence start` 印出的那一行也寫出「由誰結算」。
- checker（`verify-demo --mode e1a`）的報告多兩項，只列事實，不影響任何 claim：
  - `facilitator`：照 start 的記錄列出。
  - `settlements`：每一筆 tx#2 的購買單、交易編號，以及鏈上 `eth_getTransactionByHash` 讀到的送出者（`sent_by`），也就是這一筆的 gas 是誰付的。沒有 `--rpc` 時是空的。
- 送出者的地址不在 start 裡記錄，而是讀鏈：start 記錄時本機的 `apa-facilitator` 還沒啟動，而且鏈上的送出者比 facilitator 自己的 `/supported` 更可信。checker 不比對兩者，這一點寫進了報告的 limits。
- checker 版本仍是 `e1a.4`：判定規則沒有改，只是多列兩項。
- 測試：
  - `TestE1aSettlementSenders`：讀得到送出者、節點沒有這筆交易、地址格式不對，三種情況都列出那一筆，E4、E8 不變。
  - `TestV2PaidOnAnvil`：本機 anvil 上從 start、付款、打包到查證走一遍，報告的 `facilitator` 等於 host 目錄的設定，`sent_by` 等於實際送出 tx#2 的金鑰（anvil key 4）。
- 用新的 checker 重查 S5 的 `testnet-town-t4`（Base Sepolia）：**E1～E10 全部 PASS**，沒有 findings。`facilitator` 是空的（那時 Host 裡的商店自己結算）；4 筆 tx#2 的送出者都是 `0x6bbe…773e`，也就是商店的 `pay_to`（P2b-D3：商店自己付 gas）。`artifacts/e1a/testnet-town-t4.report.json` 沒有改寫。`settlements` 依購買單排序，同一份證據包每次的報告相同。
- 還沒做：瀏覽器檢查與本機彩排一次（要 Signer 的密語），以及 FA3 那一筆經 x402.org 的付款（要 Aaron 核准）。那一場的 `settlements` 應該列出 x402.org 的 `0xd407…f1bf`。（FA3 11/4 已做，結果如此，見下一段。）

**FA3：一筆經 x402.org 的付款（Aaron 11/4，標籤 fa3，Base Sepolia）**
- Aaron 選 A：真實 Agent 的城鎮 live 場次，只跑客人 B 的國二數學講義（`town-data-b`），由 x402.org 結算，留下證據包。
- 程式：`p2-testnet e1a-host` 多一個參數 `--facilitator URL`，預設 `http://127.0.0.1:8403`。`demo-testnet.sh` 從環境變數 `APA_FACILITATOR` 讀網址，傳給它。Host 啟動前照 FA2 的規則檢查這個網址，在 base_sepolia 上只接受 `127.0.0.1` 或 x402.org。`present.sh` 遇到 x402.org 不啟動任何東西，所以不問商店金鑰的密語。
- 執行：`APA_FACILITATOR=https://x402.org/facilitator sh scripts/demo/demo-testnet.sh live fa3 town`。start 記下 `facilitator: https://x402.org/facilitator`，起點是區塊 54224321。04:33（UTC）只開始客人 B：真實 Agent 請購 `books-math-g8` 0.06，走 Allow，付款並交付（`show-e1a-fa3-b-01`）。
- Host 的建置是 `d024623`，加上還沒提交的 `cmd/p2-testnet`、`demo-testnet.sh` 修改（`vcs.modified=true`）。Host 本身的程式和 `d024623` 相同，證據包的 `code_commit` 記的也是它。
- 證據包 `artifacts/e1a/testnet-town-fa3`（只在本機，`artifacts/` 不進 git，同 t4；run `e1a-20261104T043605Z-2a757b`，結束區塊 54225428）。`verify-demo --mode e1a --rpc https://sepolia.base.org`：**E1～E10 全部 PASS**，沒有 findings；報告在 `artifacts/e1a/testnet-town-fa3.report.json`。
  - `facilitator`：`https://x402.org/facilitator`。
  - `settlements`：1 筆。tx#2 `0x594da0a51758e910ab5817432fa6dac9c13d17ca57153923d1e9bc36f36357a2` 的送出者是 `0xd407e409e34e0b9afb99ecceb609bdbcd5e7f1bf`，也就是 x402.org 在 `/supported` 列出的 signer：這筆的 gas 由 x402.org 付，商店的 `pay_to`（`0x6bbe…773e`）沒有送任何交易。對照 t4 的 4 筆都是 `pay_to` 自己送出。
- 查證要在 sandbox 外跑：sandbox 裡 Go 驗不了公開節點的 TLS 憑證（`x509: OSStatus -26276`），E4、E8 會變成 INCONCLUSIVE（鏈沒有回應），那不是查證的結果。
