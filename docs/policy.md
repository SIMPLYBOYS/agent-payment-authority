# M1 確定性 Policy（B1-05 節點 1）

**狀態（2026-10-01）**：節點 1 於 `61b3652` 通過獨立 review，沒有阻擋驗收的程式問題。B1-05 整體仍在進行中（節點 2 持久化與事件查詢尚未實作），未合併 main。證據：Codex 在斷網、唯讀原始碼、有資源限制的 Linux 容器中獨立重跑 `go test -race -cover ./... -count=1`（Policy 覆蓋率 96.4%）、`go test ./internal/policy -count=1 -v` 與 `node scripts/check-contract-vectors.mjs`，全部通過；61／61 突變由 Claude Code 執行，Codex 核對紀錄與腳本，未獨立重跑。

`internal/policy` 的 `Decide` 是純函式：輸入一筆請購、可信設定、可信狀態與外部注入的時間，回傳 Ask、Deny 或 Allow（M1 無法付款）。它不扣預算、不發 grant、不付款，也不讀時鐘、檔案或網路，不保存狀態。請購去重、衝突拒絕、持久化、事件、取消／逾時與重啟恢復屬 B1-05 節點 2。

## 輸入

| 欄位 | 來源 | 說明 |
|---|---|---|
| `Session` | 已認證的 wrapper | consumer／agent／task；不可從請購 JSON 解出 |
| `Purchase` | 經 `contracts.Decode` 的請購 | 由 `Registry.ValidatePurchase` 再驗形狀、報價一致與歸屬 |
| `TaskStatus` | 執行期狀態（節點 2 持久化） | 不是 registry 的設定快照 |
| `Profile` | 可信設定：`testdata/policy/m1-demo-profile.json` | `nil` 視為缺設定 |
| `Credential` | consumer 的授權條件（M1 為 fixture） | `nil` 視為缺設定；不代表 BBS+ 已驗證 |
| `Budget` | 可信狀態：此 task 剩餘額度 | 預設剩餘額度等於 task cap；測試情境可由可信 fixture 提供較低額度。M1 裁決不扣減該額度 |
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
| 7 | profile 為 unattended（沒有核准人），且原本需要 Ask | Deny `APPROVER_UNAVAILABLE`（限額內的 Allow 不受影響，M1 的 Allow 仍不能付款） |
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

## Reviewer 接受的設計選擇（2026-10-01）

已接受的案例每個只有一個 Deny 條件，以下四點原由實作者決定，節點 1 review 時由 reviewer 接受（不是 Aaron 另外作出的決定）：

| 項目 | 接受的語意 |
|---|---|
| 多重 Deny 條件 | 固定順序：缺設定 → 資產 → 商家 → 效期 → 額度；都先於 Allow／Ask |
| 尚未生效的請購 | 沿用 `AUTHORIZATION_EXPIRED`，限 M1 相容處理；展示文案用「授權不在有效期間」，不把尚未生效寫成已過期 |
| 已結束 task | 拒收新請購，不新增 Deny 裁決，維持既有事件順序規則 |
| 計價資產 | 請購資產必須符合 profile 的計價資產，不直接比較不同資產的最小單位金額 |

效期 `valid_after < now < valid_before` 與 EIP-3009 參考實作一致；把「尚未生效」與「已過期」合併成同一個 reason code 是本專案的 M1 選擇，不是協議要求。

## 不在節點 1

去重與衝突拒絕、原子寫入、事件與 hash chain、取消／逾時、approval deadline 計時、重啟恢復、A／B 儲存隔離、事件查詢 endpoint、憑證 fixture 的設定載入。
