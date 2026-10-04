# B1-06 交付點 1：Store 接線、已認證唯讀事件 HTTP、fake 端到端整合

狀態（2026-10-04）：Claude Code 實作，待 Codex review。這份文件記錄整合決定，依據是 vault 筆記 `Claude-Code-B1-06-主線整合實作指示-20261004`，開工基準 `05bb1fe`（分支 `b1-06-demo-integration`，堆疊在 B1-04 之上，未合併 main）。

**本輪有什麼**：真實的本機商店 HTTP 402、Host 組裝請購、Policy、SQLite Store、到期 worker，以及已認證的唯讀 HTTP 查詢，全部在同一個程序裡。模型行為由 fake CLI 替身產生。

**本輪沒有什麼**：沒有 UI／Dashboard（交付點 2）、沒有真實 Claude CLI 請購（B1-04 live 暫緩）、沒有登入產品（只有 demo 用的 capability）、沒有付款、grant、Signer 或鏈上操作。B1-06 的完整完成門檻仍包含至少一次 opt-in live trace。

## 1 服務生命週期（`cmd/demo`）

同一個 Host 組合根管理 Store 與所有 worker。`apa-buyer run` 仍然存在，但它開自己的 Store 與到期 worker；**不要讓 `apa-buyer` 指向 demo 的資料庫**（見第 5 節）。

啟動順序：

1. 讀設定：`APA_ADDR`（只接受 loopback IP）、`APA_DB_PATH`（預設 `var/apa-demo-fake.db`）、`APA_DEMO_TOKEN_FILE`（預設 `var/demo-tokens.json`）。`APA_LIVE_CLI=true` 一律拒絕。**token 路徑不得指向資料庫本身、`-wal`、`-shm` 或 `.lock`**，在取得鎖、開 Store、寫 token 之前檢查（第 8.1 節）。
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
4. 關閉 MCP bridge（此時已沒有 run，不再有工具呼叫）。
5. 停止到期 worker 並等待它結束。
6. 關閉 Store，最後釋放資料庫鎖。

（10/4 修正：原文寫「到期 worker → bridge」，與程式不符。程式的順序是「bridge → 到期 worker → Store」，與已驗收的 `apa-buyer` 相同。run 結束後，bridge 和到期 worker 互不依賴，兩者都在 Store 之前關閉。`TestStopOrder` 檢查實際順序，第 8.3 節。）

Store 一定最後關；不會在 run 或到期 worker 仍可能寫入時關閉資料庫。

## 2 身分與權限（demo capability）

這是 **demo 認證**，不是 WebAuthn、eKYC 或真實使用者登入。

- 每次啟動產生新的隨機 token（32 bytes，hex），只寫入 token 檔；不印在終端、log 或 trace，不放 URL，不進 repo。重啟後舊 token 失效。
- 權限由 Host 依可信 registry 對應，不看請求自述：
  - `viewer`：目前 registry 的每個 consumer 一個，只能看**已保存歸屬**為該 consumer 的 task。目前 registry 已移除的 consumer 沒有 viewer token。
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

- `task_id`、`consumer_id`、`agent_id`、`wallet_id`、`payer`、`wallet_evidence_mode`：**task 開始時保存的綁定**，`wallet_evidence_mode` 取自該 task 自己的 registry 快照；與請購狀態在同一次讀取取得。不會用目前 registry 覆蓋，目前 registry 換了錢包或已不列出這個 task 都一樣（第 8.2 節）。錢包是 fixture reference，不代表持有私鑰。
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

## 7 驗證紀錄（Claude Code 執行，待 Codex 獨立 review）

程式 `4599d9c`，文件 `1f4d681`。以下都在 `1f4d681` 上執行；相對基準 `05bb1fe` 的差異是這兩個 commit。

**測試與驗收點對應**（`cmd/demo/integration_test.go`；真實 HTTP、SQLite，fake CLI 為子程序；預期值來自 `testdata/contracts/cases.json`，不由 Policy 產生）：

| 驗收點 | 測試 |
|---|---|
| Ask／Deny（402、請購、Policy、Store、HTTP 對得上） | `TestIntegrationAskDenyAndAccess/ask_and_deny`：對照案例 `ask`、`deny-budget`，以及商店自己 402 的 URL 與金額 |
| 身分、主持人權限、Host／Origin | `.../identity` |
| 唯讀查詢 | `.../read_only`；`TestIntegrationExpiryOnlyByWorker`：截止時間已過，GET 仍不寫到期，worker 寫入後才出現，對照案例 `approval-deadline-exact` |
| 投影安全與無付款 | `.../projection`：fake 宣稱付款的文字、合成標記、bridge capability、token 都不出現在 API 或 log；沒有 Approved／Settled 等事件 |
| 到期與取消、重送與恢復、子程序回收 | `TestIntegrationEndsRestartAndReplay`：Deny 後 CLI 逾時與 Ask 後主持人停止，都不改寫原裁決（對照 `cancel-pending`）；fake 的孫程序被回收；重啟後紀錄仍在；已記錄的 task 不重跑；`task-a-next` 沿用 wallet-a；同一請購重送只有一筆裁決 |
| 啟動失敗不假裝 ready | `TestIntegrationStartRefusals`：資料庫損壞（不修改原檔、不發 token）、非 fake CLI（只被呼叫 `--version`）、同一資料庫第二個 host、計畫檔錯誤 |
| 獨立性 | 以上都只用本 repo、fake 執行檔與暫存 HOME，不經 PATH 找 `claude` |

突變 #204–#215：Host、Origin、token 比對、viewer 範圍、grant、未開始 task 的 404、GET 觸發到期、關閉前未等 run、資料庫鎖、計畫未知欄位、非 fake CLI、已記錄 task 重跑。全部由測試斷言抓到；提交前預檢時有 4 項起初編譯失敗或掛住，改寫後通過。

**結果**：

- **本機**（`artifacts/b1-06/1f4d681c6a95-20261004T091254Z/`）：make check、check-contracts、`go test -race -cover ./... -count=1`、B1-01、20 項證據匯入測試全部 exit 0。
  - 覆蓋率：cmd/demo 81.6%、buyer 92.8%、toolbridge 91.8%、apa-buyer 81.7%。
  - 乾淨匯出 demo-fake：0／2／0。
  - 本機檢查總耗時 217 秒。
- **手動執行一次**（`artifacts/b1-06/manual-20261004T091726Z/`）：`bin/demo --fake-cli bin/fake-claude --plan testdata/demo/fake-plan.json`。
  - task-a 為 Ask／pending，task-b 為 Deny／`INSUFFICIENT_BUDGET`，各 3 個事件；viewer A 讀 task-b 得 404，沒有 token 得 401；SIGINT 後 exit 0。
  - 證據不含 token 值。
  - 建置、啟動到兩個 run 都記錄完約 6 秒。
- **容器完整驗證**（既有映像 sha256:78258368…，未重建、無新依賴）：overall exit 0，七步驟全過；baseline PASS、215／215 突變；17 項隔離檢查 ok。
  - 證據在 `artifacts/verify-container/runs/1f4d681c6a95-20261004T091745Z/`。
  - 總耗時 1154 秒。容器內各步驟的耗時無法從現有證據區分（匯入後時間戳相同），本輪不改驗證工具。
- 本輪沒有出現時序測試失敗。

## 8 review 修正（`4a25bc8` 的 review；Claude Code 執行，待 Codex 獨立 review）

依據：vault 筆記 `Claude-Code-B1-06-交付點1修正指示-4a25bc8-20261004`。Codex 對 `4a25bc8` 的 review：

- **Codex 獨立重跑**（斷網容器）：cmd/demo、cmd/apa-buyer 的 race 測試通過；`TestIntegration*` 三輪通過；三個額外探測確認 R1 的兩種路徑衝突與 R2 的錢包錯置；buyer 全套 race 中 `TestHostInitSequence` 失敗。
- **Claude Code 執行、Codex 核對紀錄**（非 Codex 獨立重跑）：七步容器驗證、215 項突變、17 項隔離檢查。
- 第 7 節的「全套通過」因此不成立。

修正程式在 `7267f91`。

### 8.1 R1：token 檔不得取代資料庫的檔案

- **問題**：`APA_DEMO_TOKEN_FILE` 設成 `APA_DB_PATH` 時，服務照常 ready，原資料庫被 token JSON 取代。token 寫到 `<db>.lock` 時，rename 換掉了鎖檔，第二個程序就能取得鎖。這是可信設定誤用造成的資料損壞與鎖失效，不是遠端攻擊。
- **修正**：
  - `checkTokenPath` 拿 token 路徑與資料庫、`-wal`、`-shm`、`.lock` 比對，**在取得鎖、開 Store、寫 token 之前**執行。
  - 比對方式有兩種：
    - 正規化路徑相同：絕對路徑、清理 `.`／`..`、最近一層已存在的上層目錄解析 symlink；
    - 檔案已存在時，以 `os.SameFile` 判定為同一個檔案，可擋 hard link，以及不分大小寫檔案系統上的大小寫別名。
  - `newTokens` 在 rename 前再檢查一次；這時資料庫、WAL 與鎖檔都已存在。
  - 衝突時回傳固定的設定錯誤：不提供 HTTP、不宣告 ready，也不刪除或搬動任何檔案。
  - token 檔仍以原子方式更新，權限 0600，目錄須為 0700。
- **限制**：這是設定檢查，不防同一 OS 使用者在檢查與 rename 之間的惡意競態。路徑別名靠 symlink 解析與 `SameFile` 處理，並未涵蓋所有可能的檔案系統別名。
- **測試**：
  - `TestTokenPathMustNotBeADatabaseFile`：資料庫、WAL、SHM、鎖，相對路徑、`.`、`..`、symlink 上層目錄、hard link，全部拒絕；獨立的 token 檔可以通過。
  - `TestTokenPathConflictRefusedAtStart`：走正式啟動路徑。先建立含請購的資料庫，再用 6 種衝突路徑啟動，結果皆為錯誤、未 ready、**Store 從未開啟**（`onStore` 未被呼叫），資料庫與其他檔案的位元組都不變；用可控時鐘。另有：已有程序持鎖時，`newTokens` 拒絕鎖檔路徑，鎖仍然有效。正向對照：用正常 token 檔在同一個資料庫啟動，查詢正常。

### 8.2 R2：已保存的 task 用自己保存的綁定

- **問題**：API 用**目前** registry 的綁定填身分，再接上 Store 的歷史請購。registry 換了錢包之後，舊 task 回應的 `wallet_id` 會變成新錢包。
- **修正**：
  - 新增 `Store.ReadTask(taskID, visible)`，在一次讀取交易裡取得 task 保存的綁定、投影與事件：
    - 綁定是 consumer、agent、wallet、payer；錢包的 evidence mode 取自該 task 自己的 registry 快照。
    - 權限函式只拿 task **保存的** consumer 來問，不看請求內容。
    - 不存在與無權查看都回同一個 `OWNERSHIP_MISMATCH`。
    - 保存的綁定若與快照不一致，視為 corrupt（API 回 503），不從目前 registry 補值。
  - API 改用 `ReadTask`，不再讀目前 registry，回應多了 `payer`。viewer token 仍依目前 registry 的 consumer 產生，已移除的 consumer 沒有 token。
  - GET 仍然不寫入、不跑到期。沒有修改契約、cases.json、schema、Policy 或 hash chain。
- **測試**：
  - `TestReadTask`（Store 層）：綁定內容、與 `Task` 相同的投影、權限只拿保存的 consumer 來問、未知／未開始／無權回同一錯誤、讀取不寫入（含截止時間已過）、綁定被竄改時回 corrupt。
  - `TestHistoricalTaskKeepsItsBinding`：重現 Codex 的情境，並在整合層檢查：
    - task-a 保存後，把 consumer-a 改到 `wallet-a-new` 和新地址再重啟：task-a 的 wallet、payer、請購、intent hash、事件都與之前相同。
    - 新 registry 下，task-b 照常建立。task-a-next 被 Store 既有規則 `WALLET_BINDING_CHANGED`（B1-05）拒絕；這是既有規則，不是本輪新增。
    - viewer A 可讀自己的舊 task；viewer B 帶 consumer、wallet 參數或 header 仍得 404。
    - 再把 consumer-a 整個從 registry 移除：沒有它的 viewer token，主持人仍可讀，viewer B 不能。

### 8.3 關閉順序

- 採用程式的順序：HTTP → run → bridge → 到期 worker → Store。理由見第 1 節。
- `deps.stage` 是測試用的掛鉤，記錄每一步。`TestStopOrder` 檢查順序，並確認被取消的 run 在 Store 關閉前寫入了 `TASK_CANCELLED`。
- 既有突變「Store 在 run 結束前關閉」仍保留。

### 8.4 late-init 測試（`internal/buyer` `TestHostInitSequence`）

- **事實**：Codex 執行 `go test -race ./cmd/demo ./cmd/apa-buyer ./internal/buyer -count=1` 時，這個測試失敗：`the late init was not read`。trace 是 `init_missing`、messages 0、outcome timeout。
- **檢查測試的同步方式**：
  - 情境 `init-late` 要求 fake「從啟動起忽略 SIGTERM」，但 fake 要等 Go runtime 啟動、`TestMain` 分派、讀取並解析情境檔之後，才呼叫 `signal.Ignore`。
  - host 的時間上限是固定 300 ms。如果 fake 自己的啟動超過 300 ms（`-race` 的測試二進位較慢），SIGTERM 會在忽略生效前到達，fake 被終止，init 從未印出。
  - 也就是說，測試沒有建立「先忽略 SIGTERM，再停止」的先後關係。
- **隔離重跑三次**（`artifacts/b1-06/late-init-20261004T101105Z/`，`4a25bc8`）：三次都通過，各約 7.9 秒。通過不能證明原因。
- **實驗**：在暫存副本把上限改成 1 ms，讓 SIGTERM 一定比忽略早到。結果重現了完全相同的失敗：同一句斷言，trace 為 `init_missing`、messages 0、timeout（`experiment-total-1ms.log`）。
- **修正**（只改測試，兩次）：
  1. `7267f91`：改成 fake 證明已忽略 SIGTERM 之後才取消 run。依據是 fake 在 `signal.Ignore` 之後立刻寫入 stdin 計數檔。
     - 本機三次 race 都通過，但**容器驗證的突變 baseline 失敗**：0.09 秒、`the late init was not read`。
     - 原因：stdin 檔是在 fake 的 MCP handshake **之前**寫的。容器比較快，取消搶在 handshake 前；已停止的 run 的 bridge 拒絕 handshake，fake 結束，沒有印出 init。
     - 容器紀錄保留：`artifacts/verify-container/runs/7267f91a3fd5-20261004T103542Z/`，附 FAILED-NOTE。其餘六步 exit 0，222／222 突變被抓到，但 baseline 失敗，所以這一步和整次執行都算失敗。
  2. `b161e79`：測試改用自己的情境，第一步是 `savecap`（寫 capability 檔）。fake 的步驟只會在「忽略 SIGTERM」與「handshake 完成」之後才執行，所以這個檔案就是同步點；看到它才取消 run。
     - 共用的 `init-late.json` 不變，另一個測試仍在使用。
- 兩次修正都：
  - 把時間上限改為 30 秒，不讓它介入；
  - 保留所有斷言：init 確實被讀到並通過 Gate，但沒有 prepare、沒開始 task、沒有工具呼叫、沒有 Store 結束紀錄；
  - 停止原因從 timeout 改為 cancel；兩者都會讓 run 進入停止中，被測的是同一條「停止後的 init」路徑；
  - 不放寬正式 runner 或產品的逾時。
- 既有突變「run 停止後仍 prepare」（#149）、「停止不 disarm」（#150）在 `b161e79` 的測試下仍被抓到。
- **結果**：Codex 的同一命令，`7267f91` 上 exit 0（114 秒），`b161e79` 上 exit 0（104 秒）（`codex-command-*.log`）。
- **仍未知**：沒有量測 fake 在 `-race` 下的啟動與 handshake 時間分布。兩種失敗的機制都有直接證據（1 ms 實驗、容器 baseline），但 Codex 那次的實際啟動耗時沒有觀測資料。

### 8.5 驗證（Claude Code 執行，待 Codex 獨立 review）

- **Codex 的原命令**：`b161e79` 上 exit 0，104 秒。
- **本機全套**（`7267f91`，`artifacts/b1-06/7267f91a3fd5-20261004T103039Z/`，226 秒）：check-contracts、race／coverage、B1-01、20 項匯入測試都 exit 0。覆蓋率 cmd/demo 83.1%、store 82.0%；demo-fake 0／2／0。
  - **make check exit 2**：`cmd/apa-buyer` `TestLiveOptIn` 的 2.1.288 子案例中，代用腳本的 `--version` 在 5 秒版本檢查上限內沒有執行。這是 `docs/cli-poc.md` 第 34.2 節非阻擋待辦的第二次出現，該測試自 `1f0b260` 起未改。
  - 失敗紀錄保留在 FAILED-NOTE。只重跑 make check 一次：exit 0，46 秒。
- **容器**：
  - `7267f91`：baseline 失敗（見 8.4）。
  - `b161e79`：上一個 session 結束時被中止（exit 143、證據匯入失敗），不算結果。
  - 最終一次見 8.6。
- **突變**：#216–#222 為新增；#208、#210 改指新的讀取位置；共 222 項。提交前預檢都被斷言抓到。#216 起初沒被抓到，因為測試沒證明拒絕發生在 Store 開啟前；補上 `onStore` 斷言後抓到。

### 8.6 驗證流程加速（2026-10-05，依 Aaron 指示；`4c598a1`）

- **突變**：原本每項突變都匯出到新的暫存目錄，而 Go 的 build cache 也以來源目錄為鍵，所以每項都重新編譯整個模組的套件。
  - 現在每項仍從全新匯出開始，但永遠在同一個路徑，只重建受影響的部分。本機三項的小樣本每項 2–5 秒，baseline 12 秒。
  - 每項的 go test 加上 300 秒上限：掛住只讓該項失敗（只會判成 BUILD?，不會判成 KILLED），不再耗盡整步的一小時。
  - baseline、每一項與總計都記錄秒數。
- **容器**：每一步的耗時記在 `exit-codes.txt`（`seconds=N`）。檢查內容不變，匯入器的測試照常通過。
- **我的工作流程**：
  - 本機不再另跑一整套與容器相同的命令（make check、race、B1-01、匯入測試），完整檢查只在最終 commit 的斷網容器跑一次。
  - 本機只跑本輪相關測試、review 指定的命令，以及受影響突變的預檢。
  - 若 Codex 或 Aaron 仍要求本機全套，再恢復。
- **最終容器驗證**（`7a7747d`，Docker 啟動後重跑；既有映像 sha256:78258368…，未重建）：overall exit 0，七步驟全過；baseline PASS、222／222 突變；17 項隔離檢查 ok。
  - 證據：`artifacts/verify-container/runs/7a7747d39d95-20261004T163317Z/`；主機端輸出 `artifacts/b1-06/container-7a7747d.log`。
  - 總耗時 952 秒，之前同類執行為 1154／1245 秒。
  - 各步驟：check-contracts 21、check 91、race-cover 196、verify-b1-01 45、guards 1、evidence-import-tests 1、mutation 580 秒（其中 baseline 37 秒）。
  - 10/5 先前一次因 Docker daemon 未執行而沒有跑到任何檢查（`artifacts/b1-06/container-8dfa317.log`）。
