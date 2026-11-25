# 憑證要求放進 x402 的 extensions（第 4 條邊界，11/25）

## 1 邊界

FUTUREMODE 的第 4 條邊界：憑證的要求放在 `PAYMENT-REQUIRED` 的 `extensions`，證明放在 `PaymentPayload` 的 `extensions`。之前兩邊都是空的：要求由 Host 在程序裡讀商店的設定，證明在 Host 建立請購時驗。

這一輪做前半（要求）。後半（證明交給商店驗）牽涉隱私取捨，見第 4 節，等 Aaron 決定。

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

## 4 還沒做：證明交給商店驗

要讓商店自己驗證明，有三個問題：

1. **綁定**：現在的證明綁 intent 雜湊，裡面有客人、Agent、task 等商店看不到的欄位，商店算不出來。要改成綁商店看得到的東西（商店的 DID、資源、選中的報價、EIP-3009 的 nonce），付款時再產生一份新的證明。
2. **商店要有驗證材料**：發行者公鑰與狀態清單現在只在 Host 的目錄裡，沒有對外提供。
3. **隱私**：證明的必揭露欄位包含 holder 公鑰與 task ID。交給商店，不同商店就能用 holder 公鑰把同一位客人串起來，等於放掉 BBS+「不可串連」這個主要賣點；要保住，必揭露欄位與 holder 綁定的做法都得重新設計。

所以後半要先決定：維持「Host 驗、商店只看到結果」，還是付出上面的代價讓商店自己驗。
