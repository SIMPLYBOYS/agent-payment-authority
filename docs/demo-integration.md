# B1-06 交付點 1：Store 接線、已認證唯讀事件 HTTP、fake 端到端整合

狀態（2026-10-04）：Claude Code 實作，待 Codex review。這份文件記錄整合決定，依據是 vault 筆記 `Claude-Code-B1-06-主線整合實作指示-20261004`，開工基準 `05bb1fe`（分支 `b1-06-demo-integration`，堆疊在 B1-04 之上，未合併 main）。

**本輪有什麼**：真實的本機商店 HTTP 402、Host 組裝請購、Policy、SQLite Store、到期 worker，以及已認證的唯讀 HTTP 查詢，全部在同一個程序裡。模型行為由 fake CLI 替身產生。

**本輪沒有什麼**：沒有 UI／Dashboard（交付點 2）、沒有真實 Claude CLI 請購（B1-04 live 暫緩）、沒有登入產品（只有 demo 用的 capability）、沒有付款、grant、Signer 或鏈上操作。B1-06 的完整完成門檻仍包含至少一次 opt-in live trace。

## 1 服務生命週期（`cmd/demo`）

同一個 Host 組合根管理 Store 與所有 worker。`apa-buyer run` 仍然存在，但它開自己的 Store 與到期 worker；**不要讓 `apa-buyer` 指向 demo 的資料庫**（見第 5 節）。

啟動順序：

1. 讀設定：`APA_ADDR`（只接受 loopback IP）、`APA_DB_PATH`（預設 `var/apa-demo-fake.db`）、`APA_DEMO_TOKEN_FILE`（預設 `var/demo-tokens.json`）。`APA_LIVE_CLI=true` 一律拒絕。
2. 讀可信設定檔：registry（`testdata/contracts/registry.json`）、profile（`testdata/policy/m1-demo-profile.json`），以及有 `--fake-cli` 時的 fake 計畫（第 4 節）。都來自本機檔案與命令列旗標，沒有任何一項來自 HTTP 客戶端。
3. 有 `--fake-cli` 時先檢查它確實是 fake：`--version` 必須含 `fake-claude`，否則不啟動。用暫存 HOME，不經 PATH 查找。
4. 綁定 listener；再確認實際綁到的位址是 loopback。
5. 用實際綁到的位址建立商店；商店 URL、402 requirement 與 Store 的報價簿都來自這份設定，不看請求的 Host header。
6. 取得資料庫鎖（`<APA_DB_PATH>.lock`，flock）。同一個資料庫已被另一個 demo 程序使用時拒絕啟動，避免兩套互不知情的到期 worker。
7. 開啟 Store：`store.Open` 會驗證所有已存紀錄，並記錄關閉期間已到期的 Ask。**驗證失敗就回傳錯誤、不提供任何服務**，不修補、不重建、不刪除資料庫。
8. 產生 demo capability，寫入 token 檔（目錄 0700、檔案 0600；第 2 節）。
9. 啟動到期 worker（`Store.RunExpiry`，目標週期 1 秒；不承諾最大延遲 1 秒）。
10. 開始服務 HTTP（health、商店、`/api/...`），印出 `listening on http://...`，這時才算 ready。
11. 有 fake 計畫時，在背景依序執行每一個 run。

關閉順序（收到 SIGINT／SIGTERM，或測試取消 context）：

1. 取消 fake run 的 context：runner 終止 fake 的整個程序群組；Host 對已開始的 task 記錄 `TASK_CANCELLED`（或逾時時 `TASK_TIMED_OUT`）。
2. `http.Server.Shutdown`：不再接收新請求，等進行中的請求結束。
3. 等 fake run 的 goroutine 結束（此時寫入仍可進行，Store 還開著）。
4. 停止到期 worker 並等待它結束。
5. 關閉 MCP bridge。
6. 關閉 Store，最後釋放資料庫鎖。

Store 一定最後關；不會在 run 或到期 worker 仍可能寫入時關閉資料庫。

## 2 身分與權限（demo capability）

這是 **demo 認證**，不是 WebAuthn、eKYC 或真實使用者登入。

- 每次啟動產生新的隨機 token（32 bytes，hex），只寫入 token 檔；不印在終端、log 或 trace，不放 URL，不進 repo。重啟後舊 token 失效。
- 權限由 Host 依可信 registry 對應，不看請求自述：
  - `viewer`：每個 registry consumer 一個，只能看該 consumer 的 task。
  - `host`（主持人）：可看全部 task。這是獨立的 token，不交給 Buyer 或展示端。
- Buyer（fake CLI）只拿到 MCP bridge 的 capability；它不是 HTTP token，HTTP API 不接受它。
- 請求需帶 `Authorization: Bearer <token>`。比對使用固定時間比較。query、body、其他 header 裡的 consumer、wallet、task、role 一律不影響權限。
- Host 與 Origin：`Host` header 必須等於實際綁定的位址；有 `Origin` 時必須是 `http://<實際綁定位址>`。不回任何 CORS header，不開放任意 Origin。用 Authorization header 而非 cookie，所以沒有 cookie session／CSRF 的問題。
- loopback 不是免認證理由：listener 只綁 loopback，API 仍然要 token。

## 3 HTTP 路由與回應

| 路由 | 用途 |
|---|---|
| `GET /api/tasks/{task_id}` | task 摘要、請購與裁決 |
| `GET /api/tasks/{task_id}/events` | task 的事件鏈 |

既有的 `GET /healthz` 與商店路由（`/catalog`、`/resources/...` 的 402）不變。

錯誤（穩定代碼，不含內部錯誤文字）：

| 狀態 | `error` | 何時 |
|---|---|---|
| 403 | `forbidden_host` | Host header 不是實際綁定位址 |
| 403 | `forbidden_origin` | 有 Origin 但不是本服務 |
| 401 | `unauthenticated` | 沒有或錯誤的 token |
| 404 | `not_found` | 不存在、尚未開始，或這個 token 無權看的 task；三者回應完全相同，不洩漏存在與否 |
| 503 | `store_unavailable` | Store 讀取失敗 |
| 405 | — | 非 GET（標準 mux 行為） |

所有回應都帶 `Cache-Control: no-store`、`X-Content-Type-Options: nosniff`。

`GET /api/tasks/{task_id}` 回應欄位：

- `task_id`、`consumer_id`、`agent_id`、`wallet_id`、`wallet_evidence_mode`：來自可信 registry。錢包是 fixture reference，不代表持有私鑰。
- `status`、`deny_reason`：Store 的 task 投影。
- `as_of_sequence`、`as_of_observed_at`：這份投影對應的最後一個已提交事件。
- `served_at`：Host 時鐘的回應時間。
- `purchases`：Store 的 `PurchaseView` 白名單投影，包括 `intent_hash`、`decision`（verdict、reason、approval_status、payment_status，原始裁決不改寫）、`decided_at`、Ask 的 `deadline_at`（理論到期時間）、`approval_closed`／`closed_at`（只有到期 worker 或 Host 已寫入時才有）。不含報價原文、extra 或 extensions。
- `payment_capability`：固定 `unavailable`。M1 不會 Settled，不交付付費內容。

**到期語意**：`deadline_at` 已過但 `approval_closed` 為空時，代表到期尚未被持久化。API 照實回傳 Store 的狀態，不把它說成已到期；UI 可以用 `served_at` 和 `deadline_at` 判斷。GET 不呼叫 `ExpireDue`，不寫任何事件。

`GET /api/tasks/{task_id}/events` 回應：`task_id`、`as_of_sequence`（最後一個事件的序號），以及 `events` 陣列。每個事件只輸出以下欄位：`event_id`、`event_type`、`sequence`、`prev_event_hash`、`purchase_order_id`、`intent_hash`、`code`、`decision`、`evidence_mode`、`source`、`observed_at`。全部從 Store 已保存的事件照抄，不重算、不重新產生。

兩個路由各自是一次讀取快照。它們都帶 `as_of_sequence`：數字不同代表中間有新事件，UI 應重取，不能把兩份混成同一時刻的狀態。

## 4 fake 流程與計畫檔

```sh
make demo-integration          # 建置 bin/demo、bin/fake-claude，然後啟動
# 等價於：
APA_ADDR=127.0.0.1:8402 ./bin/demo --fake-cli bin/fake-claude --plan testdata/demo/fake-plan.json
```

計畫檔 `testdata/demo/fake-plan.json` 是 **可信的 Host 測試設定**，不是 Policy 修改，也不是 agent 能提交的東西：

- `task-a`（consumer-a）：主線情境 `ask-mainline.json` → 主商品 Ask（`HUMAN_APPROVAL_REQUIRED`）。
- `task-b`（consumer-b）：同一情境，Host 把這個 task 的初始預算設為 `250000`（對應已接受案例 `deny-budget` 的 `remaining`）→ Deny（`INSUFFICIENT_BUDGET`）。

每個 run：fake CLI 經 MCP 呼叫商店（真實 HTTP 402）→ Host 組裝請購 → `Store.Submit`（裡面跑 Policy）→ 事件。fake 結尾宣稱「已付款」的文字不會進 Store，也不會出現在 API。

重跑：

- 同一資料庫裡已存在的 task 不會再跑（啟動時在開 fake CLI 之前就略過，log 會說明）；即使搶先，Store 也會以 `TASK_ALREADY_STARTED` 拒絕。不清空歷史。
- 要看新的 run：用新的 task ID（registry 裡有 `task-a-next`，同 consumer、同錢包），或設 `APA_DB_PATH` 到新檔案。

查詢（token 從 token 檔讀，不要貼到命令列歷史以外的地方）：

```sh
TOKEN=$(python3 -c 'import json;print([t["token"] for t in json.load(open("var/demo-tokens.json"))["tokens"] if t["role"]=="host"][0])')
curl -sS -H "Authorization: Bearer $TOKEN" http://127.0.0.1:8402/api/tasks/task-a
curl -sS -H "Authorization: Bearer $TOKEN" http://127.0.0.1:8402/api/tasks/task-b/events
```

## 5 資料庫歸屬

| 程序 | 預設資料庫 | 到期 worker |
|---|---|---|
| `cmd/demo`（本交付點） | `var/apa-demo-fake.db` | demo 程序內一個 |
| `apa-buyer run`（B1-04 fake） | `var/apa-fake.db` | apa-buyer 程序內一個 |
| `apa-buyer run --live` | `var/apa-live.db` | apa-buyer 程序內一個 |

demo 的 HTTP 讀取與 fake run 的寫入用同一個 `*store.Store`，不會出現讀 DB-A、寫 DB-B。demo 以 `<db>.lock` 防止兩個 demo 程序同時使用同一個資料庫。`apa-buyer` 不檢查這個鎖：把 `APA_DB_PATH` 指向 demo 資料庫是不支援的用法。

## 6 已知限制

- token 檔是本機檔案權限保護；同一使用者的其他程序讀得到。畫面接入前（交付點 2）再依相同權限模型設計交付方式。
- 沒有 task 清單路由；UI 需要時另行設計，仍依相同權限模型。
- 「到期」只在 worker 寫入後出現；worker 週期是目標值。
- 本輪沒有 UI，也沒有真實模型請購。
