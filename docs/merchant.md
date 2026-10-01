# M1 最小資料商店：目錄＋402

B1-03 交付 `internal/merchant/`（商店）與 `internal/x402/`（x402 v2 HTTP header 編解碼），由 `cmd/demo` 啟動。

**狀態（2026-10-01）**：**B1-03 已結案（Done）**，驗收 commit `ebd3f44`。範圍：商品目錄、x402 v2 的 402 報價、與 B1-02 共用契約對接、`PaymentPayload.extensions` 格式檢查、HEAD 行為，以及 M1 不交付受保護內容。**不包含**簽章驗證、付款結算、Signer 或鏈上交易。

驗收證據：Codex 獨立重跑全部 Go race 測試、`extensions` 與方法測試、JavaScript 契約向量，全部通過；43／43 突變由 Claude Code 執行（`scripts/verify/mutation-check.py`，含 Linux 容器驗證），Codex 核對紀錄，不是 Codex 獨立重跑。

商店範圍是**本機真 HTTP＋fixture 商業資料**：商品目錄、一份受保護資料、HTTP 402 報價。沒有 settlement adapter、Signer、facilitator 或鏈上交易，不是 FR-13 驗收 7 的外部 live 證據。

## 規格依據

依 2026-10-01 取得的 coinbase/x402 一手規格核對：

| 文件 | commit | 說明 |
|---|---|---|
| `specs/transports-v2/http.md` | `aeef1bff6694099922c8df5b6e943b894433718b`（2026-03-05） | header 名稱、base64、HTTP 狀態碼對應；檔案 SHA-256 `4594c224…69b7` |
| `specs/x402-specification-v2.md` | `dd927a26cfefc98c24b3ec38b3a8f204dad0c60d`（2026-04-21） | `PaymentRequired`／`PaymentPayload` 欄位、錯誤代碼；協定版本 v2.0（2025-12-09） |

規格只寫「Base64-encoded」，沒有指定字母表；範例是標準字母表加 padding（RFC 4648 §4），本實作只接受這一種。規格範例的兩個 header 原文存在 `testdata/x402/spec-examples.json`，`x402.Decode` 必須能解開，作為互通向量。

## 端點

| 請求 | 回應 |
|---|---|
| `GET /catalog` | 200，商品目錄（JSON）。只有標題、摘要、價格與 `resource_url`，不含付費內容 |
| `GET /resources/kr-market?edition=1` | 402，`PAYMENT-REQUIRED` header 帶報價；body 是 `{"error","message"}` |
| `HEAD /catalog` | 200，與 GET 相同的 header，沒有 body |
| `HEAD /resources/kr-market?edition=1` | 402，沒有 body；`PAYMENT-REQUIRED` 與 GET 的報價相同 |
| 其他路徑或 query（含 `?edition=2`、多一個參數） | 404，不帶報價 |
| 對上述路徑用 GET、HEAD 以外的方法（POST、PUT、PATCH、DELETE、OPTIONS 等） | 405，不帶報價 |

HEAD 是 Go `net/http` 的 GET 路由本來就涵蓋的行為（同一個 handler，伺服器丟棄 body），沒有另外的處理邏輯。

受保護資源以**路徑＋原始 query 完全比對**，不正規化 query。商店不抓取任何外部 URL，也沒有代理或轉址功能。

## 報價

| 欄位 | 值 |
|---|---|
| `x402Version` | 2 |
| `scheme`／`network` | `exact`／`eip155:84532` |
| `amount` | `"500000"`（fixture USDC 6 位小數＝0.50 test USDC） |
| `asset` | `0x4444…4444`（registry 的 `fixture-usdc`） |
| `payTo` | `0x3333…3333`（registry 的 `merchant_pay_to`） |
| `maxTimeoutSeconds` | 60 |
| `extra` | `{"name":"USDC","version":"2"}`（與 registry 的 `eip712_name`／`eip712_version` 相同） |
| `extensions` | `{}` |

目錄另列 KRW 700，換算匯率 1400 KRW／USDC 是 **fixture，不是即時匯率**。目錄價格只供參考，具約束力的是 402 的 `PAYMENT-REQUIRED`。測試核對報價與 `testdata/contracts/registry.json` 一致（收款地址、可信資產、EIP-712 名稱與版本），並核對 500000 units、0.50 test USDC 與 KRW 700／1400 三者一致。

`resource.url` 與目錄的 `resource_url` 都由主機設定的 base URL 組成（`cmd/demo` 用實際綁定的 loopback 位址），**不採用請求的 `Host` 或 `X-Forwarded-Host`**。

## PAYMENT-SIGNATURE 的處理

M1 沒有驗證或結算能力，**任何請求都拿不到受保護資料**，也從不送出 `PAYMENT-RESPONSE`。狀態碼依規格的 HTTP 對應：格式錯或與報價不符屬 Invalid Payment（400），格式正確但無法驗證／結算屬 Payment Failed（402）。

| 情況 | 狀態 | body `error` |
|---|---|---|
| 沒有 header | 402 | （無） |
| 多個 header、非標準 base64、非 JSON、重複或未知欄位、缺 `payload`、`payload` 非物件、浮點數、超過 64 KiB | 400 | `invalid_payload` |
| `extensions` 出現但不是物件（字串、陣列、布林、數字、`null`） | 400 | `invalid_payload` |
| `resource.url` 不是這個端點 | 400 | `invalid_payload` |
| `x402Version` 不是 2 | 400 | `invalid_x402_version` |
| `accepted.scheme` 不是 `exact` | 400 | `invalid_scheme` |
| `accepted.network` 不是 `eip155:84532` | 400 | `invalid_network` |
| `accepted` 其他地方與報價不同（金額、收款人、`extra`、大小寫變體的 key 等） | 400 | `invalid_payment_requirements` |
| 格式正確且與報價完全相同 | 402 | `M1_PAYMENT_UNAVAILABLE` |

`extensions` 可以省略；出現時必須是物件（規格 5.2.2），內容不解析，省略、`{}` 或任何擴充物件都照常進入後續比對。`accepted` 以 canonical JSON 逐位元組比對報價。簽章本身不解析也不驗證：M1 不宣稱任何簽章有效或無效。所有 400／402 都附上 `PAYMENT-REQUIRED`（`error` 欄位說明原因），方便客戶端重新取得報價。

## 與共用契約對接

`x402.Decode` 以 `contracts.Decode` 解析 header（重複／未知／null 欄位、尾隨資料、非安全整數都拒絕），`PaymentRequired.Quote(i)` 把第 i 個 `accepts` 與 `extensions` 原樣轉成 `contracts.Quote`，保留未知欄位並計算 `payment_requirement_hash`；`extensions` 缺席與 `{}` 視為同一份報價。測試以 402 實際回傳的報價組出 task-a 的 `PurchaseOrder`，通過 `ValidatePurchase`（形狀、報價一致、歸屬）。

留給 B1-04 的責任：

- `Quote` 不包含 `resource.url`；buyer 必須確認 `resource.url` 等於自己請求的 URL，再用它計算 intent 的 `resource_hash`。
- 只請求目錄列出的 `resource_url`；目錄與報價都來自不受信任的商家輸入，仍須經 02 契約驗證與 B1-05 Policy。

## 已知限制

- 只有一項商品；商品資料寫在 `internal/merchant/merchant.go`，`testdata/merchant/` 放的是手寫的預期 wire 輸出，供測試逐位元組比對。
- decoder 刻意嚴格：規格日後新增的頂層欄位，會在更新本套件前被拒絕。
- 400／402 的 body 格式是本專案自訂（規格把 body 留給伺服器實作）。
- 受保護資料只以 canary 字串驗證「從未出現在任何回應」；M1 沒有任何交付路徑。
