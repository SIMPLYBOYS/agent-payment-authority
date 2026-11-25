# 憑證要求放進 x402 的 extensions（第 4 條邊界，11/25）

## 1 邊界

FUTUREMODE 的第 4 條邊界：憑證的要求放在 `PAYMENT-REQUIRED` 的 `extensions`，證明放在 `PaymentPayload` 的 `extensions`。之前兩邊都是空的：要求由 Host 在程序裡讀商店的設定，證明在 Host 建立請購時驗。

前半（要求）見第 2 節；後半（證明交給商店驗）見第 4 節。

## 2 做法（要求）

- **格式**：`extensions["apa-credential"].info = {"schema": "apa.credential.v2", "requires": {"country": ""}}`；值是 `""` 表示只要揭露、不限值。x402 沒有公布這種擴充，所以鍵名是本專案自己的；不認得的客戶端依規格忽略它。
- **商店**（`internal/merchant`）：付款商店（本機 anvil、testnet）在每個要求憑證的商品的 402 裡宣告，和簽好的報價放在同一個 `extensions`。內容固定，所以報價雜湊與 Host 的報價清單不變。M1 與 fake 模式的商店不宣告（它們的報價雜湊固定在契約測試向量裡），Host 照舊用自己的設定。
- **Host**（`internal/toolbridge`）：從 402 讀出要求，但只接受落在 Host 為這個場景設定範圍內的：只能要 Host 允許的屬性，Host 定了值的屬性，商店不能改。超出範圍就不建立請購（`QUOTE_REFUSED`），Host 不會因為商店開口就多揭露。範圍內的要求就是這次的揭露清單；商店可以要得比 Host 允許的少。
- **綁定**：要求在報價的 `extensions` 裡，所以算進 `payment_requirement_hash`，也就算進 intent 雜湊與 BBS+ 證明的 nonce。改了要求，就是另一份報價、另一個證明。
- **checker（E2）**：一筆的要求改從報價讀，沒有宣告時才用商店的固定設定；再確認宣告的要求沒有超出場景本來允許的範圍，並照舊核對揭露的屬性「不多不少」。

## 3 驗證

- `internal/x402`：`TestCredentialRequirement`（讀回原樣、沒宣告就是沒有、格式不對就拒絕、範圍檢查）。
- `internal/toolbridge`：`TestVenueRequires`（超出範圍的屬性、別的 schema 都拒絕；沒宣告時用 Host 的設定）。
- `internal/merchant`：`TestSignedOfferAndReceipt` 也確認 402 帶著要求。
- `internal/payflow`：`TestV2PaidOnAnvil`（anvil 上從報價到交付，checker 從報價讀要求，E1～E10 全部通過）。

## 4 證明交給商店驗（11/25，第一步）

**先想清楚的問題**：BBS+ 每次出示都會重新隨機化證明，證明本身串不起來；會被串連的是揭露的欄位。Host 自己驗的那份證明一定揭露 `holder`（holder 公鑰）、`task_id`、`status`、`expiry`，其中 holder 公鑰與狀態位置對每位客人、每張憑證都是固定值，直接交給商店，不同商店就能用它們把同一位客人串起來（`docs/commercial-c2-design.md` 第 12、26 項當時就預留了這件事）。

**做法**：付款時另外產生一份給商店的證明。

- **只揭露** `schema`、`issuer` 與商店 402 要求的屬性。`holder`、`task_id`、`status`、`expiry`、`consumer_id` 都不揭露，所以同一位客人在兩間商店出示的內容，只有它們本來就要求的屬性值，沒有可以比對的識別值；兩次出示的證明也不同。
- **綁定商店看得到的東西**：商店的 did:web、資源網址、402 的 accepts 項目、EIP-3009 授權的 nonce。商店自己就能重算 nonce；拿到別筆付款或別間商店的證明都驗不過。
- **放在** `PaymentPayload.extensions["apa-credential"].info.presentation`（`internal/payflow`）。每次送出都重新產生；商店的結算以轉帳本身防重，不受影響。拿不出證明（沒有 holder 材料、v1 憑證沒有屬性）時照樣送出、不帶證明，由商店拒絕。
- **商店**（`internal/merchant`，`TrustIssuer`）：信任一個發行者後，要求憑證的商品在結算前一定先驗：揭露的欄位恰好是它要求的、證明在它自己的綁定與發行者公鑰下成立、發行者是它信任的、它定了值的屬性值相符。不成立就回 402 `invalid_credential`，不結算、不送交易。Host 帶了 `--credential-dir` 又開了付款時，才替商店設定信任的發行者，所以沒有憑證目錄的場合照舊。
- **Host 照舊驗完整的那份**（holder 綁定、task、期限、撤銷），結果記在 `CREDENTIAL_VERIFIED`，Policy 依它裁決。

**驗證**

- `internal/credential`：`TestPresentToShop`（只揭露三個欄位；換商店、換付款、換資源、換 accepts、換發行者、要求的值不符、要求更多屬性、改過的屬性值，都驗不過；兩次出示的證明不同）。
- `internal/merchant`：`TestCredentialPresentedToTheShop`（沒有證明、別筆付款的證明：402 且沒有送出交易；這筆的證明：結算）。
- `internal/payflow`：`TestV2PaidOnAnvil`（anvil 上 Host 出示、商店自己驗、結算、交付，checker 全部判定通過）。

**界線（照實說）**

- **商店驗不到「是本人在出示」**：holder 不揭露，就沒有 holder 綁定。下一步是 link secret：發證時由發行者盲簽一個只有客人知道的秘密，出示時證明「知道它」而不揭露；要先確認 IBM 的 BBS+ 函式庫支援盲簽。
- **撤銷與期限由 Host 判斷**：商店相信 Host 的檢查；要讓商店自己驗，得改用零知識的撤銷證明。
- **發行者公鑰**：示範裡商店和 Host 在同一個程序，直接拿到公鑰；正式的做法是發行者公布它（例如發行者自己的 did:web）。
- **付款層仍可串連**：每間商店收到的 EIP-3009 授權都有同一個付款錢包地址，鏈上也看得到。不可串連只在憑證這一層成立。
- **checker 沒有重驗商店那份證明**：證據包裡沒有付款內容（裡面有 EIP-3009 簽章，不能放進證據包）。checker 照舊驗 Host 那份的結果（`CREDENTIAL_VERIFIED`）與揭露是否不多不少；商店拒絕沒有證明的付款，由測試保證。
