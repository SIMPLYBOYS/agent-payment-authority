# 落差批次 4：本人確認委託

> 2026-10-24，Claude Code。範圍是 `docs/commercial-gap-review.md` 的 A1。
> Aaron 10/24：「批次 4 依照建議的方向進行」：**G3＝B**（Signer 只在本人以已登記的裝置確認過涵蓋這筆付款的委託後才簽）；**G4**：只用裝置，不加語音。
> 第 6 節的決定已確認，照第 3 節的切片實作。

## 1 現況

- 委託（任務預算、自動範圍、商家、核准期限、有效時間）由主持人扮演客人確認：`POST /api/sim/delegations`（`cmd/demo/sim.go`），Store 記 `confirm_method: simulated`（`internal/store/pay.go`）。
- Signer 不知道這份委託。它只認 `signer.json` 裡管理者設定的固定委派（本機 `dlg-a`、`dlg-b`；testnet `dlg-ta`、`dlg-tb`、`dlg-t402`），以及每一筆 Ask 的本人核准（A2）。
- 所以「客人授權了這筆預算」目前說不出口：故事的第一步是模擬的。

## 2 做法

### 2.1 本人確認（Signer）

- 沿用 A2 的裝置驗證：challenge、驗證、計數器與 BE 旗標檢查、同一請求重送拿到同一個答案。
- 綁定的不是請購，而是**委託條款的雜湊**：版本、委託 ID、客人、錢包、鏈、資產合約、商家、需求、任務預算、自動範圍、核准期限、有效時間。不含時間與確認者。
- 另一個 challenge 標籤與 jti 前綴，所以委託的確認永遠不能被當成某一筆請購的核准，反之亦然。
- 不唸句子（G4）。
- 驗證通過後，Signer 記一筆「已確認的委託」（新表 `delegation_confirmations`）：確認 ID、委託 ID（唯一）、條款雜湊與條款、客人、錢包、核准人與版本、任務預算、自動範圍、確認時間、可付款到何時、綁定的 task（第一次付款時綁定）。
- 健康檢查回報 `delegations: "device"`。

### 2.2 付款時的檢查（G3＝B）

每一筆付款（Allow 與 Ask 都一樣）的授權都要帶確認 ID。Signer 在原本的檢查之外再檢查：

| 檢查 | 不符時 |
|---|---|
| 確認存在 | `DELEGATION_NOT_CONFIRMED` |
| 同一位客人、同一個錢包；確認仍在有效期內；確認的裝置仍有效、核准人設定沒變 | 拒絕 |
| 收款人在委託的商家內、資產相同 | 拒絕 |
| Allow 的金額不超過自動範圍 | 拒絕 |
| 第一次付款綁定這個 task；之後只能是同一個 task | `DELEGATION_OTHER_TASK` |
| 這個委託已接受與已簽的總額加上這一筆，不超過任務預算 | `OVER_TASK_BUDGET` |

管理者在 `signer.json` 的固定上限（每筆、總額、核准人 24 小時）照舊保留：那是管理者的硬上限，委託條款則來自 Host。

### 2.3 Host 與頁面

- 新路由：`POST /api/delegations/challenge`、`/api/delegations/assertion`，只接受該客人自己的 viewer token；條款由 Host 依情境與設定組出，頁面不能改。
- 驗證通過後 Store 記 `confirm_method: webauthn`，帶 Signer 的確認 ID 與裝置；`DELEGATION_BOUND` 事件帶確認方法。
- 付款時 Host 把 task 的委託確認 ID 交給 Signer。
- **付款主機不再有「以 A 模擬」確認委託**：委託框的按鈕改為「用已登記的裝置確認委託並開始」。不付款的 fake 主機照舊模擬，並照實標示。
- 啟動時 Signer 沒有回報 `delegations` 就不以付款模式啟動，就緒表也有一列。理由：舊版 Signer 會忽略不認得的欄位而照簽，等於沒有檢查。
- 展示視窗第 1 步：本人確認時寫「客人用自己的裝置確認了委託：預算…」；模擬時寫「主持人代為確認委託（示範，不是本人）」。
- checker（`internal/verification`）：E6 的對照表接受 `webauthn` 的委託確認，從 `not_evaluated` 移除「委託由本人確認」，版本 e1a.2。

### 2.4 相容與部署

| 組合 | 結果 |
|---|---|
| 新 Host＋舊 Signer | Host 不以付款模式啟動（健康檢查擋下） |
| 舊 Host＋新 Signer | 每一筆付款都被拒（`DELEGATION_NOT_CONFIRMED`）：失敗時是關閉的 |
| 退回舊 Signer | 新表與新欄位被忽略，舊的行為照舊 |

- 本機：`p2-dev.sh` 每次從 repo 重建開發用 Signer，不用另外做。
- testnet：Aaron 以 sudo 把新執行檔裝到 `/opt`；`signer.json` 不用改。
- E1-a 真實 Agent：同一條裝置確認路徑取代現在「只能確認委託的模擬」。

## 3 切片

| | 內容 | 規模（含測試） |
|---|---|---|
| S1 | 合約：委託的確認方法欄位、`DELEGATION_BOUND` 的內容、兩份證據對照表 | 約 180 行 |
| S2 | Signer：委託的 challenge 與驗證、新表、健康檢查 | 約 550 行 |
| S3 | Signer：付款時的檢查 | 約 320 行（含付款流程的測試資料） |
| S4 | Host 與 Store：路由、客戶端、確認方法、確認 ID 帶進付款、移除付款主機的模擬確認、健康檢查擋下 | 約 450 行 |
| S5 | 頁面：裝置確認委託、一起開始、展示視窗第 1 步、可信度區 | 約 230 行 |
| S6 | checker e1a.2 | 約 70 行 |
| S7 | 本機彩排、`/opt` 換執行檔（Aaron）、一場 testnet、文件 | — |

### 2.5 review 補記（S1～S3，10/24）

- `valid_seconds` 是「委託確認後多久內要開始 task」，由 Host 檢查（Store 的委託到期）；Signer 只管付款期限（確認後 2 小時）。兩者分開記，不是同一個限制。
- 放棄（abandon）的確認不再能付款；核准人的錢包或客人被改掉（即使版本沒變）也不能付款。
- test402 那一筆（`dlg-t402`）以後也要帶委託確認，而且確認的商家要包含 test402 的收款位址；S4 一併處理。

### 2.6 review 補記（S4～S6，10/24）

- 一起開始的順序照 Aaron 的決定：先依序完成每位客人的裝置確認，兩位都確認後才各自開始；沒確認的那位不開始，其他照常。
- 裝置確認進行中，確認按鈕與一起開始都先停用（避免第二次提示，或確認好的委託沒有被開始）。
- 展示視窗第 1 步依驗證器寫：「本人以已登記的裝置確認了委託…」；測試用的 virtual authenticator 照實寫出，不寫成實體裝置。
- 有 Signer 的主持人（A2）即使不付款，委託也改用裝置確認、不再提供角色模擬；但只有付款主機會在啟動時檢查 Signer 是否新版。舊版 Signer 的不付款 A2 主機無法確認委託（頁面會顯示 Signer 拒絕）。
- 就緒表的「委託確認」那一列還沒做：付款主機在啟動時就擋下舊版 Signer，已足夠；之後要再加。
- `scripts/verify/a2-ui.mjs` 修正後跑 4 次，3 次全過；1 次在第一筆核准時沒有完成（面板停在待核准，裝置被要求的次數比預期多），原因未查明（記錄被截斷）。之後追查，彩排時留意。
- 付款主機上 `/api/host/simulate` 仍會發出只能確認委託的角色模擬（E1-a 時代的 delegationOnly），但確認委託的路由一律拒絕；之後清掉。

### 2.7 S7：Aaron 的步驟

**本機彩排（不用 sudo）**：`make demo-local`（`p2-dev.sh` 每次從 repo 重建開發用 Signer；它的 `signer.db` 第一次啟動時自動加上新表與新欄位）。資料研究室按「一起開始」：依序跳出 A、B 的委託確認（各輸入一次 PIN），兩位都確認後各自開始；之後照常核准兩筆請購。展示視窗第 1 步應寫「本人以已登記的裝置確認了委託：預算 1.50…」。

**testnet：`/opt` 換新的 Signer（Aaron 以 sudo）**：執行檔已建好 `var/gap4-opt/apa-signer`（commit `8db1ce2`，SHA-256 `816a54bdd47701d925850f12e07a4c8463442ecc997d11ee03de3c3856d45d7f`）。
1. 確定 `make demo-testnet` 已停下（Ctrl-C 看到「已停下」）。
2. 備份（留著，不刪）：
   ```
   sudo /bin/cp -p /opt/apa-signer/bin/apa-signer /opt/apa-signer/bin/apa-signer.gap3
   sudo -u apa-signer /bin/cp -p /opt/apa-signer/data/signer.db /opt/apa-signer/data/signer.db.gap3
   ```
3. 安裝並核對：
   ```
   sudo install -o root -g wheel -m 0755 var/gap4-opt/apa-signer /opt/apa-signer/bin/apa-signer
   shasum -a 256 /opt/apa-signer/bin/apa-signer
   ```
   應與上面的 SHA-256 相同。
4. `make demo-testnet`：Signer 第一次啟動時自動加上新表與新欄位；Host 確認 Signer 回報 `delegations: device` 才啟動。
5. 資料研究室按「一起開始」：A、B 各自用裝置確認委託，再各自用裝置＋語音核准請購。

回滾（需要時，先停下）：`sudo install -o root -g wheel -m 0755 /opt/apa-signer/bin/apa-signer.gap3 /opt/apa-signer/bin/apa-signer`（舊版會忽略新表與新欄位；但新版 Host 不會在舊版 Signer 上啟動付款模式）。

### 2.8 進度

- Aaron 10/24 本機彩排（`make demo-local`，fake Agent，本機 anvil，實體裝置）：「本機彩排執行正常」。新版開發用 Signer、裝置確認委託、一起開始、核准與付款都照 §2.7 完成。
- 待做：`/opt` 換新的 Signer 後一場 testnet。

## 4 預設的做法（照建議，不另外問）

- 管理者的固定上限保留（2.2）。
- Ask 的付款也要有確認，Allow 與 Ask 一起算進任務預算。
- 一份確認只用在一個 task（和 Store 現在的綁定一致）。
- 確認後 2 小時內可以付款（`valid_seconds` 1800 秒只是開始 task 的期限）。
- 停用裝置時，它確認過的委託一起失效。
- checker 只改 E6 與移除 `not_evaluated` 那一項，不新增 claim。

## 5 界線

- Claude Code 不碰 `/opt`、不用 sudo、不讀金鑰與 `var/` 下的資料庫；`/opt` 換執行檔由 Aaron 執行。
- virtual authenticator 的結果不寫成實機結果；本機的結果不寫成 testnet 的結果。
- testnet 的每一場仍照往例由 Aaron 開始。

## 6 Aaron 的決定（10/24）

- **預算對上限**：委託的任務預算降到 1.50（資料研究室、街角咖啡，本機與 testnet），和 testnet 核准人每 24 小時 1.50 的上限一致；客人確認的數字就是實際能花的上限。
- **一起開始**：按一次，依序跳出 A、B 各自的裝置驗證（每位客人確認自己的委託），兩位都確認後各自開始。
- 其餘照第 4 節的預設。
