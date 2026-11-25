# 商家簽報價與收據（x402 Signed Offers & Receipts，11/25）

## 1 為什麼要做

之前商家沒有身分：Host 綁住報價的完整雜湊，但報價沒有人簽；交付時只記下內容的 sha256，商店說「交付了」只是它的回應碼。這一節補上 x402 的 Signed Offers & Receipts 擴充：商家簽每一筆報價與每一次交付的收據，Host 取報價前驗、交付時驗，checker 從證據包再驗一次。

## 2 規格（讀到的版本）

x402-foundation/x402 `specs/extensions/extension-offer-and-receipt.md` v0.6（2026-02-04），11/25 讀：

- 報價放在 402 的 `extensions["offer-receipt"].info.offers[]`；收據放在結算回應（`PAYMENT-RESPONSE`）的 `extensions["offer-receipt"].info.receipt`，只在 200 時出。
- 這裡用 EIP-712 格式：網域 `x402 offer`／`x402 receipt`、version `1`、chainId 固定 `1`（只當離鏈簽章格式用，不是任何代幣的網域，不能拿來轉帳）。
- 報價簽 `version`、`resourceUrl`、`scheme`、`network`、`asset`、`payTo`、`amount`、`validUntil`（可省）；收據簽 `version`、`network`、`resourceUrl`、`payer`、`issuedAt`、`transaction`（可省，這裡一律帶 tx#2）。
- 規格要求驗證者另外確認「這把金鑰有權代表這個商家」，可用 did:web、DNS 或 `payTo` 自己簽；這裡用 did:web。
- EIP-712 的型別字串是照規格文字組的，沒有和官方 SDK 交叉比對過。

## 3 做法

- **商店**（`internal/merchant`）：只有會結算的付款商店簽（本機 anvil 或 testnet）。M1 與 fake 模式的商店不簽，因為它們的報價雜湊已固定在契約測試向量與既有證據裡。
  - 啟動時產生一把專用的簽章金鑰（不是 `pay_to` 的金鑰），在 `/.well-known/did.json` 公布：`did:web:<host>`（埠號的冒號寫成 `%3A`），`EcdsaSecp256k1RecoveryMethod2020`，`blockchainAccountId` 為 `eip155:1:<地址>`。
  - 每個商品的報價簽一次。簽章是確定性的（RFC 6979）、報價不帶 `validUntil`，所以同一個商品的 402、它的報價雜湊與 Host 的報價清單，每次請求都一樣。
  - 交付成功（200）時，在 `PAYMENT-RESPONSE` 帶一張收據：這個資源、網路、付款人與 tx#2。郵局那種「付了沒交付」的回應是 402，不出收據。
- **Host 取報價**（`internal/toolbridge`）：402 帶了簽好的報價，就要它成立才繼續：只有一份、內容就是 402 的條件與這個資源、沒過期，簽章者在商店 did:web 文件裡（從資源所在的網址取 `/.well-known/did.json`）。不成立就不建立請購。沒簽的商店（外部商店、M1）照舊。
- **Host 交付**（`internal/payflow`）：收據要由報價的同一把金鑰簽，對得上這筆的資源、網路、付款人與 tx#2；對不上就在終端機寫出來，交付照實記下，讓 checker 判。收據存在 Store 的 `receipts.payment_response`；先回 202、之後才交付的情況，存的是交付那次的回應，所以收據不會掉。
- **證據包**（`internal/e1evidence`）：每個 task 多一個 `responses.json`，是每筆已交付付款的商店回應（收據在裡面）。
- **checker（E2）**（`internal/verification/e1a.go`）：報價簽章要還原得出金鑰、內容就是訂單的報價與資源；在簽過的報價上交付的付款，要有同一把金鑰簽的收據，對得上這筆的資源、網路、付款人與 tx#2。11/25 之前的證據包沒有 `responses.json`，也沒有簽過的報價，所以不受影響。
  - E9 的秘密掃描本來一律禁止 `signature` 欄位（EIP-3009 的付款簽章在用掉或過期前，拿到的人就能拿去轉帳）。現在只放行 `offer-receipt` 底下、形狀恰好是 r＋s＋v 的那一個；其他位置的 `signature` 照舊判失敗。結算回應裡的 `transaction`（交易雜湊）和 `tx_hash` 一樣放行。

## 4 驗證

- `internal/x402`：`TestOfferReceipt`（簽章確定、改金額就還原不出商家的金鑰、收據換金鑰或換交易都不成立、did:web 文件只認那一把）。
- `internal/merchant`：`TestSignedOfferAndReceipt`（anvil 上真的結算一筆：402 每次都一樣、did.json 認得這把金鑰、交付帶著成立的收據）。
- `internal/toolbridge`：`TestSignedOfferChecked`（文件沒列的金鑰、和 402 不同的金額，Host 都不建立請購）。
- `internal/verification`：`TestE1aShopSigned`（報價金額不符、交付了沒收據、別把金鑰簽的收據、收據的交易不對，E2 都判失敗；沒簽的商店與還沒交付的付款不要求收據）；`TestE1aUnsafeSignatures`（付款簽章、`offer-receipt` 以外的簽章、形狀不對的簽章，E9 都照舊找出來）。
- `internal/payflow`：`TestV2PaidOnAnvil`（anvil 上從報價到交付走一遍，打包證據後 checker 全部判定通過，含 E2 重驗報價與收據、E9）。

## 5 界線（照實說）

- **金鑰每次啟動都換**：示範用的商店沒有長期金鑰；正式的商店會保留金鑰，在 did.json 公布輪替。
- **did:web 走 http**：本機與 testnet 的商店都在 Host 同一台機器、同一個程序裡，did.json 是用 http 取的；did:web 原本要 https，信任的是那個網域。所以這裡證明的是「報價與收據出自同一把公布過的金鑰」，不是「這個商家是誰」。
- **checker 沒有 did.json**：金鑰是不是商店的，由 Host 取報價時驗；證據包裡沒有那份文件，checker 只確認報價與收據是同一把金鑰簽的、內容對得上。已寫進 checker 的限制清單。
- **收據證明的是商家的說法**：「交付了這個資源」由商家簽名負責；交付的內容對不對，仍只有內容的 sha256，checker 不判斷內容本身。
- **外部商店（test402.com）不簽**：Host 照舊接受沒簽的報價。要求所有商店都簽，要等對方支援這個擴充。
