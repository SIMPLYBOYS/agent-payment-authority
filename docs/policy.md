# M1 確定性 Policy（B1-05 節點 1）

`internal/policy` 的 `Decide` 是純函式：輸入一筆請購、可信設定、可信狀態與外部注入的時間，回傳 Ask、Deny 或 Allow（M1 無法付款）。它不扣預算、不發 grant、不付款，也不讀時鐘、檔案或網路，不保存狀態。請購去重、衝突拒絕、持久化、事件、取消／逾時與重啟恢復屬 B1-05 節點 2。

## 輸入

| 欄位 | 來源 | 說明 |
|---|---|---|
| `Session` | 已認證的 wrapper | consumer／agent／task；不可從請購 JSON 解出 |
| `Purchase` | 經 `contracts.Decode` 的請購 | 由 `Registry.ValidatePurchase` 再驗形狀、報價一致與歸屬 |
| `TaskStatus` | 執行期狀態（節點 2 持久化） | 不是 registry 的設定快照 |
| `Profile` | 可信設定：`testdata/policy/m1-demo-profile.json` | `nil` 視為缺設定 |
| `Credential` | consumer 的授權條件（M1 為 fixture） | `nil` 視為缺設定；不代表 BBS+ 已驗證 |
| `Budget` | 可信狀態：此 task 剩餘額度 | M1 不扣款，等於 task cap |
| `Now` | 可信 host 時鐘（Unix 秒） | 0 表示沒有時鐘 |

demo profile `m1-demo-v1`：資產 `fixture-usdc`，單筆 Allow 上限 0.10（`100000`），task cap 5（`5000000`），商家白名單只有 registry 的 `merchant_pay_to`，互動模式。這只是版本化的 demo 設定，不替第二批 Signer 的 `allow_max` ADR 定案。測試核對它等於 `cases.json` 的 defaults 與 registry。

## 不做裁決的情況（回傳錯誤）

以下情況沒有可記錄的裁決，`Decide` 回傳錯誤、不回傳 decision：

- 請購不合法，或 session 不擁有它：`OWNERSHIP_MISMATCH` 等契約錯誤。
- task 已結束（denied／cancelled／failed）：`TASK_NOT_ACTIVE`；未知狀態也拒絕。`ValidateEventOrder` 不允許 task 結束後再出現裁決，所以這裡是拒收，不是 Deny 裁決。
- 可信輸入互相矛盾：額度屬於另一個 task、憑證屬於另一個 consumer 或不是 fixture、profile 不合法、剩餘額度超過 task cap、沒有時鐘。

## 裁決順序

Deny 優先，真人不能覆蓋。依序檢查，第一個不通過的項目決定 Deny 原因：

| 順序 | 條件 | 結果 |
|---|---|---|
| 1 | 沒有 profile 或沒有憑證 | Deny `CONFIG_MISSING` |
| 2 | 資產不在 registry 可信清單，或不是 profile 的計價資產 | Deny `ASSET_NOT_ALLOWED` |
| 3 | `pay_to` 不在 profile 商家白名單 | Deny `MERCHANT_NOT_ALLOWED` |
| 4 | 憑證已過期（`now >= valid_until`），或 `now` 不在請購效期內 | Deny `AUTHORIZATION_EXPIRED` |
| 5 | 金額大於剩餘額度 | Deny `INSUFFICIENT_BUDGET` |
| 6 | 金額不超過 `allow_max` | Allow `PAYMENT_CAPABILITY_UNAVAILABLE` |
| 7 | profile 為 unattended（沒有核准人） | Deny `APPROVER_UNAVAILABLE` |
| 8 | 其他 | Ask `HUMAN_APPROVAL_REQUIRED`，approval `pending` |

邊界：金額等於剩餘額度仍可；等於 `allow_max` 為 Allow；憑證在 `now < valid_until` 時有效；請購效期依 EIP-3009 為 `valid_after < now < valid_before`（兩端都不含）。每個裁決都通過 `PolicyDecision.Validate`，`payment_status` 一律 `unavailable`，帶請購的 `intent_hash`。

## 驗收案例

`cases.json` 的 8 個 `policy` 案例直接由測試讀取並執行，預期結果在 Policy 實作前就已寫定並經 Aaron 接受，沒有從 `Decide` 倒推。參數對應：

| 參數 | 對應 |
|---|---|
| `remaining` | `Budget.Remaining` |
| `allow_max`／`mode` | profile 同名欄位 |
| `merchant` | `allowed`＝白名單含 registry 商家；`not_allowed`＝空白名單 |
| `credential` | `valid`＝`valid_until` 為一天後；`expired`＝`valid_until` 等於 `now` |
| `required_config` | `missing`＝沒有 profile |
| `clock` | `Now` |
| `asset` | `unlisted`＝請購與報價改用 registry 不信任的合約，並重算 requirement hash |
| `amount` | 請購與報價同時改金額，並重算 requirement hash |

測試遇到沒有對應的參數會直接失敗，不會默默略過。

## 待 review 確認的設計選擇

已接受的案例每個只有一個 Deny 條件，以下幾點由實作者決定，請 reviewer 確認：

1. **多個 Deny 條件同時成立時的順序**（上表 1～5）。
2. **請購尚未生效**（`now <= valid_after`）也回 `AUTHORIZATION_EXPIRED`：封閉的原因清單沒有「尚未生效」，新增代碼需要升版。
3. **`TASK_NOT_ACTIVE` 以拒收（錯誤）處理**，不產生 Deny 裁決，理由見上。
4. **資產必須是 profile 的計價資產**：額度以單一資產的最小單位計，不能拿另一種代幣的金額來比。

## 不在節點 1

去重與衝突拒絕、原子寫入、事件與 hash chain、取消／逾時、approval deadline 計時、重啟恢復、A／B 儲存隔離、事件查詢 endpoint、憑證 fixture 的設定載入。
