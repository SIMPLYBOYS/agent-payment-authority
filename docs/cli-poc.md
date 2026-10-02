# B1-04 Buyer wrapper：CLI 橋接 PoC

> **狀態（2026-10-02，Claude Code 執行，待 Codex 獨立 review；第 9 節為 `ba0b440` review 的 R1／R2 修正）**：fake 模式已實作並有測試：真子程序、真 loopback HTTP 402、真 SQLite Store。**真實模型推論尚未執行**；live 模式在這個版本不可啟用。fake 成功只證明 wrapper／bridge／Store 的行為，不證明真實 Claude 能完成請購。B1-04 的 live trace 驗收項仍保留。

## 1 流程與元件

```text
主持人 ── apa-buyer run ──┬─ fixture 商店（127.0.0.1:隨機 port，真 HTTP）
                          ├─ Store（SQLite，APA_DB_PATH）＋ RunExpiry
                          ├─ toolbridge.Server（Unix socket，0700 私有目錄）
                          └─ CLI 子程序（固定 executable＋argv，獨立 process group）
                                 └─ MCP server：apa-buyer mcp-bridge（stdio）
                                        └─ 每次工具呼叫：連 socket，附本 run 的 capability
```

| 部分 | 位置 | 職責 |
|---|---|---|
| runner | `internal/buyer/runner.go` | 啟動、process group、stdout／stderr 上限、stream-json 解析、結果分類、逾時／取消、回收 |
| host 編排 | `internal/buyer/host.go` | 由 registry 綁定身分、發 capability、寫 MCP 設定、組 argv／env、timeout／cancel 對應 Store、run trace |
| MCP adapter | `internal/toolbridge/mcp.go` | JSON-RPC 2.0、一行一訊息（MCP 2025-06-18 stdio），只服務 `initialize`、`ping`、`tools/list`、`tools/call` |
| 工具定義與參數 | `internal/toolbridge/tools.go` | 三個工具；參數只接受 `product_id`（`^[a-z0-9-]{1,64}$`），拒絕重複 key 與任何其他欄位 |
| 身分綁定與 relay | `internal/toolbridge/relay.go` | capability → `Binding`；撤銷、每 run 呼叫上限；bridge 程序端 `RunBridge`／`Dial` |
| 402 檢查與請購 | `internal/toolbridge/shop.go` | 目錄白名單、402 嚴格檢查、由可信資料組裝 PurchaseOrder、`Store.Submit`、白名單投影 |
| Store 接線 | `cmd/apa-buyer/main.go` | 設定來源、Store 開檔、StartTask、RunExpiry 啟停、關閉順序 |
| fake CLI | `internal/buyer/fakecli/`、`cmd/fake-claude/` | 接受 host 的 argv、依 `--mcp-config` 啟動 MCP server 並以 MCP 呼叫工具、輸出 stream-json、可注入故障 |
| 情境 | `testdata/cli/*.json` | 主線、重送、偽造參數、串流邊界、CLI 故障、清理情境 |

MCP 直接以標準庫實作，沒有新增第三方依賴。fake CLI 走的是同一份 MCP adapter 與 relay，不是直接呼叫 Go 函式。

## 2 信任邊界

**身分**：主持人以 `--task` 選定 registry 裡的 task；host 由 registry 取出 consumer、agent、wallet 與 `from`，建立 `Binding`。模型只看得到三個工具，唯一能填的是 `product_id`。

**caller 認證**：每個 run 一個 capability（32 bytes 隨機，hex）。它只出現在 host 記憶體（以 SHA-256 為 key）、MCP 設定檔（0600，放在 0700 暫存目錄，不在 CLI 工作目錄內，run 結束即刪）與 bridge 程序的環境變數。不進 prompt、argv、URL、run trace 或 artifacts；trace 寫入前另把所有字串中的 capability 換成 `[capability]`。只在 JSON 填 consumer／task 的請求不會被接受：relay 只認 capability。

- CLI 結束後立即撤銷，撤銷就是結束 run：不再接受新呼叫；進行中的呼叫被取消（HTTP 立即中止），撤銷要等它們全部結束才返回。之後才寫 Store 的結束事件與 run trace，所以紀錄完整。晚到、重放或偽造的 capability 一律 `RUN_NOT_ACTIVE`。
- 請購寫入（`Store.Submit`）經 `Binding.Commit`，在 run 的鎖下先確認 run 仍有效再寫。結束 run 取同一把鎖，所以兩者互斥：run 要嘛在寫入開始前結束（不寫），要嘛等寫入完成才結束。已通過其他檢查的呼叫在 run 結束後也寫不進去，結果為 `RUN_NOT_ACTIVE`。
- 每個 run 最多 8 次工具呼叫（`TOO_MANY_CALLS`）。紀錄最多 9 筆（第 9 筆是 `TOO_MANY_CALLS`），之後的嘗試只計次（trace 的 `tool_attempts`）。
- Store 再以 session 做歸屬檢查：即使 host 錯配 A 的 caller 與 B 的 task、wallet 或 payer，也會被 `OWNERSHIP_MISMATCH` 拒絕。
- **同一 OS 使用者的其他程序不在防護範圍內**：它們可讀 run 期間的 MCP 設定檔、bridge 程序環境與 socket。capability 防的是模型產生的請求與其他 run，不是本機同帳號的惡意程序。

**報價（402）**：

1. 商品與 URL 只來自 host 設定（`merchant.Offers()`）；模型只能選商品 ID。
2. HTTP client 不套環境代理、不跟隨 redirect（3xx 直接拒絕），整體 10 秒、header 64 KiB、body 64 KiB。
3. 必須是 402、恰好一個 `PAYMENT-REQUIRED`，以 `internal/x402` 嚴格解碼（未知欄位拒絕）並 `Validate`。
4. `resource.url` 必須逐字等於實際請求的允許 URL（不正規化，沿用商店的比較語意）。
5. `accepts` 必須恰好一項；quote 由該 requirement 與 `extensions` 原樣形成，不從 prompt 或目錄重建價格。
6. Store 的報價簿是 host 設定，不是收到的 402；402 改價時 host 檢查會通過，但 Store 以 `QUOTE_NOT_OFFERED` 拒絕新請購。

目錄只保留 ID、方法（GET）與 URL 都符合可信設定的項目；商店文字視為不可信，去除控制與格式字元（含換行、ESC、bidi override）並截至 200 字。

**請購組裝**：身分來自 `Binding`，條款來自已檢查的 402，時間來自 host clock（`valid_after = now−1`，`valid_before = now + maxTimeoutSeconds`）。同一 task 與商品的識別固定：`purchase_order_id = po-<task>-<product>`、`request_id = req-<product>`、`nonce = sha256("apa-m1/eip3009-nonce\0" + task + "\0" + product)`。送出前先以 `Store.Task` 查已保存的請購；已存在就回報，不再送新內容，不產生新 nonce。同 key 不同內容由 Store 的 idempotency 規則拒絕。

**模型看得到的結果**：裁決、理由、approval 狀態、`already_requested` 與固定說明「未付款、未交付」。不含 hash、URL、wallet、payer 或其他 task。

**不可信的 CLI 輸出**：stdout 裡任何文字（含看起來像 tool call 的字串、assistant 的 `tool_use`、「已付款」）都不會變成請求或事件。工具請求只有經 MCP → relay → capability 這一條路。

## 3 程序邊界與上限

| 項目 | live 預設 | fake 命令 | 超過時 |
|---|---|---|---|
| 單行 stdout（一則 stream-json） | 1 MiB | 同 | `output_overflow`，終止 |
| stdout 總量 | 8 MiB | 同 | `output_overflow`，終止 |
| stderr 總量（只計數，內容不保存） | 1 MiB | 同 | `output_overflow`，終止 |
| 總執行時間 | 5 分鐘 | 60 秒 | `timeout` → `Store.CLITimeout` |
| SIGTERM → SIGKILL、程序結束後等 pipe | 3 秒 | 同 | — |
| 工具呼叫次數／run | 8 | 同 | `TOO_MANY_CALLS` |
| MCP 單則訊息、relay 單則訊息 | 64 KiB | 同 | 結束該 session／拒絕 |
| 單次工具呼叫 | 20 秒 | 同 | 該呼叫失敗 |

- 固定 executable 與 argv，`exec` 直接啟動，不經 shell。
- 工作目錄是空的私有暫存目錄；MCP 設定在它之外。
- 環境只帶 `HOME USER LOGNAME PATH TMPDIR LANG LC_ALL TERM`，加上 fake 模式的情境路徑；API key、雲端供應商、代理與 host 自己的設定都不傳。
- `--version` 檢查（fake 命令確認 `--fake-cli` 確實是 fake）也走同一套程序規則：獨立 process group、輸出 4 KiB、stderr 4 KiB、5 秒，結束時回收整個 group。逾時、取消、超量或非零退出都視為失敗，不啟動 task；啟動 task 前再確認一次未被取消。
- 子程序在自己的 process group。任何停止都對整個 group 送 SIGTERM，期限後 SIGKILL；程序自行結束後也立即 SIGKILL 該 group，回收仍握著 pipe 的子孫。
- 已離開 group（例如 `setsid`）的程序不會被送信號：等候期限後放棄 pipe，結果標 `orphaned_output`。這是已知限制，見第 8 節。
- 不使用 `--dangerously-skip-permissions`，也不使用 `--bare`。

**結果分類**：`ok` 必須同時有程序 exit 0、恰好一則 `result` 訊息、`subtype: success` 且 `is_error: false`。exit 0 不足以判定成功。其他：`not_installed`、`start_failed`、`not_logged_in`、`rate_limited`、`timeout`、`cancelled`、`output_overflow`、`parse_error`（非 JSON 行、截斷、缺 `type`、第二則 result）、`nonzero_exit`、`missing_result`、`cli_error`、`orphaned_output`。

**Store 語意**：只有 `timeout`（`TASK_TIMED_OUT`，Ask 保留待到期）與 `cancelled`（`TASK_CANCELLED`，Ask 撤回）寫 Store；其他失敗不新增契約事件，診斷留在 run trace。既有 Deny 的狀態與理由不被後續結束事件覆蓋。task 已結束時 Store 拒絕第二次結束，回報 `TASK_ALREADY_ENDED`。結束後才到達的請購同樣由 Store 拒絕。

**run trace**（`--trace-dir`，預設 `var/runs/`，檔案 0600）：與 AuditEvent 鏈分開的 JSONL。內容是 task、consumer、CLI 檔名、prompt 的 SHA-256（不存 prompt）、上限、每次工具呼叫（工具名、驗過的商品 ID、結果碼）、結果分類、exit code、訊息數、usage（沒有就 `unknown`，不補零）、stderr 位元組數，以及清理後的 CLI 結果文字，欄位名 `untrusted_cli_result`。不含 capability、環境變數、stderr 內容或認證資料。

## 4 從乾淨環境重跑 fake 流程

先決條件：Go 1.26、make、這個 repo，以及 Go 依賴（`modernc.org/sqlite`）已下載或在模組快取中：首次建置需能連 `proxy.golang.org`，之後可離線。不需要 Claude Code CLI、登入或 Docker。

```sh
APA_DB_PATH=var/apa-fake.db make demo-fake
```

輸出第一行是 `FAKE MODE: …`，接著是 JSON 報告：工具呼叫、Store 裡的 task 與請購（主商品為 Ask、`payment_status: unavailable`）與 trace 路徑。

- DB 路徑遵守 `APA_DB_PATH`；未設定時 fake 命令用 `var/apa-fake.db`，不寫進 demo 預設的 `var/apa.db`。目錄須為 0700。
- 同一 DB 中 task 只會啟動一次。重跑同一 task 會清楚回報並結束，不刪除任何紀錄。可改用 `TASK=task-a-next make demo-fake`，或把 `APA_DB_PATH` 指到新檔。
- 命令會先執行 `--fake-cli --version`，輸出不含 `fake-claude` 就拒絕。誤把真實 `claude` 當成 fake 也不會跑推論。
- 不帶 `--fake-cli` 時直接拒絕：這個版本沒有 live 模式。

測試：

```sh
go test ./internal/buyer/... ./internal/toolbridge/... ./cmd/apa-buyer/ -count=1
```

測試二進位同時扮演 fake CLI（經名為 `fake-claude` 的 symlink）與 MCP bridge（argv `mcp-bridge`），所以每個測試都啟動真實子程序。

## 5 Store 接線（由 B1-06 移來的部分）

這次承接、已在 `cmd/apa-buyer` 實作：

- 設定來源：registry `testdata/contracts/registry.json`、profile `testdata/policy/m1-demo-profile.json`（皆 fixture，可用旗標指定）。credential 是啟動時以 host clock 產生的 M1 fixture，有效 24 小時，不驗證 BBS+。budget 預設為 profile 的 task cap。報價簿由商店設定 `merchant.Offers()` 建立。
- 可信 host clock（Unix 秒）；測試用固定時鐘。
- 啟動順序：商店 → Store 開檔（含恢復驗證與補記到期）→ StartTask → RunExpiry → bridge。前面任何一步失敗都不開 bridge。
- RunExpiry 每秒一次；錯誤寫到 stderr，報告列出次數，不吞掉。
- 關閉順序：run 結束（取消時先回收 CLI）→ 關 bridge → 停 RunExpiry 並等它結束 → 關 Store → 關商店。

仍屬 B1-06：`cmd/demo` 尚未開啟 Store；主持人事件 HTTP 端點、展示層、完整服務接線、live opt-in 指令與 M1 端到端驗收。

## 6 CLI 相容性（本機 2.1.287，2026-10-02）

只執行了 `claude --version` 與 `--help`／`auth --help`，沒有送出推論請求，也沒有執行 `claude auth status`。

| 項目 | 本機 `--help` 已確認 | 僅依官方文件 |
|---|---|---|
| `-p/--print` | 有 | 非互動語意 |
| `--output-format stream-json` | 有（choices 含 stream-json） | 每行一則 JSON 的訊息格式（`system/init`、`assistant`、`user`、`result`、`system/api_retry`）與欄位 |
| `--verbose` | 有 | stream-json 必須搭配 `--verbose` |
| `--mcp-config <configs...>` | 有 | 設定檔格式 `{"mcpServers":{name:{type:"stdio",command,args,env}}}` |
| `--strict-mcp-config` | 有：只用 `--mcp-config` 的 server | — |
| `--tools ""` | 有：`""` 停用所有內建工具 | — |
| `--allowedTools` | 有（免提示規則） | MCP 工具名為 `mcp__<server>__<tool>`；這不是隔離機制 |
| `--permission-mode dontAsk` | 有（choices 含 dontAsk） | 不在允許清單內的工具直接拒絕、不提示 |
| `--setting-sources project` | 有：`user, project, local` | **未驗證**：只載 project 是否會略過使用者層 hooks、CLAUDE.md 與記憶 |
| `--no-session-persistence` | 有 | — |
| `--disable-slash-commands` | 有 | — |
| `--max-turns` | **本機 help 沒有** | 不使用；以工具次數與總時間限制 |
| `--bare` | 有：不讀 OAuth／keychain | 不使用（會改走 API key） |
| exit code、登入失敗與限流的輸出 | — | 分類規則（`authentication_failed`、`rate_limit`、結果文字含 `/login`、`Not logged in`、`rate limit`）**未以實際失敗驗證** |

fake CLI 的 stream-json 依文件形狀產生，**不是真實 CLI 的錄製**；它也不模擬 CLI 的權限層（每個腳本化的工具呼叫都會送到 bridge，以測 host 自己的檢查）。

本機 help 另有 `--restricted`（移除會執行程式的內建工具、忽略 user／project／local 設定檔）與 `--permission-prompts none`（會提示的動作自動拒絕）。兩者可能比 `--setting-sources project` 更貼近需求，但本輪沒有採用：語意尚未驗證，也還沒確認它們與訂閱登入能否並用。列入 live 提案待決。

## 7 live 模式提案（本輪未執行，需 review 後另行核准）

建議指令形態（尚未實作；`--live` 旗標不存在）：

```sh
apa-buyer run --live --cli "$(command -v claude)" --task task-a
```

- 先決條件：本機已安裝未修改的官方 CLI（目前 2.1.287），使用者本人已透過官方流程登入。repo 不讀取、不複製、不代理 OAuth token。
- 非秘密的登入確認：候選為 `claude auth status`（本機 help 有此子命令；輸出內容未檢視，可能含帳號資訊，需先確認只記錄「已登入／未登入」）。確認失敗就停止，不改走 API key 或雲端供應商。
- 版本：記錄 `claude --version`；模型 ID 從 `system/init` 的 `model` 欄位取得並寫入 trace。
- 範圍：單一 task、單次 run；工具最多 8 次；總時間 5 分鐘；最大模型輪次本機沒有旗標，只能由工具次數與時間間接限制（待決）。
- 工具與設定來源：同第 3 節的 argv；是否改用 `--restricted`／`--permission-prompts none` 待決。
- 取消：Ctrl-C → 回收 process group → `TASK_CANCELLED`。
- 失敗：限流、未登入、逾時或任何非 `ok` 都立即停止並回報，不自動重跑、不開下一輪。
- 用量：`result.usage` 有就原樣記錄並標「CLI 回報」，沒有就 `unknown`。訂閱用量、模型成本估算、test USDC 與 gas 分開，不宣稱推論經 x402 支付。
- 本人訂閱登入官方 CLI，不等於任何第三方產品或多人服務模式已獲准；公開多人服務須另行確認條款與計費。

## 8 尚未驗證或未完成

- 真實 CLI 的 stream-json 實際內容、exit code、未登入與限流的實際輸出。
- `--setting-sources project` 是否足以略過使用者層 hooks、CLAUDE.md 與記憶；真實 CLI 下 MCP server 的程序群組與 stderr 繼承方式。
- 最大模型輪次（本機無 `--max-turns`）。
- 離開 process group 的子孫程序不會被終止，只能放棄 pipe 並標 `orphaned_output`；要完整回收需作業系統層沙箱或容器。
- 同一 OS 使用者的其他程序可讀取 run 期間的 capability（見第 2 節）。
- CLI 正常結束或輸出解析失敗時，Store 的 task 不會結束（只有 timeout／cancel 有 Store 語意）。這個 run 之後已無法寫入（第 2 節）；同一 task 的後續 run 仍可合法提出請購。
- 主持人 HTTP 端點、UI、`cmd/demo` 接線與端到端驗收屬 B1-06。

## 9 `ba0b440` review 修正（2026-10-02，Claude Code 執行，待 Codex 獨立 review）

| 項目 | review 發現 | 修正 | 回歸測試 |
|---|---|---|---|
| R1 | 撤銷只移除 capability：已取得 `Binding` 的呼叫照常執行，402 放行後仍新增 Ask；host 在呼叫完成前取紀錄快照 | `relay.go`：`Binding` 帶 run context 與進行中計數；`Revoke`／`Close` 結束 run＝取消 context、等進行中呼叫結束；寫入經 `Commit` 與結束互斥。`shop.go`：`Submit` 改經 `Commit`；run 中途結束的呼叫結果為 `RUN_NOT_ACTIVE`。`host.go`：`Revoke` 返回後才取紀錄、寫 Store 結束事件與 trace | `TestRevokeWithCallInFlight`（真 HTTP＋Store：請購卡在 402 時撤銷，`Revoke` 不等商店就返回、紀錄已完整、Store 無請購）；`TestCommitGate`（已過檢查的呼叫在結束後寫不進去；結束等待進行中呼叫並取消其 context；結束等待進行中的寫入，之後拒絕寫入） |
| R2 | `checkFake` 用 `exec.CommandContext().Output()`：沒有 process group、pipe 清理與輸出上限；150 ms context 加上持 pipe 的子孫，約 2 秒後仍回傳成功 | `runner.go` 抽出共用的 `execute`；新增 `Version` 以同一套規則執行 `--version`；`checkFake` 改用它；StartTask 前再確認 context 未取消 | `TestVersion`（持 pipe 的子孫被回收且不拖延回應；150 ms context 立即結束且不留子孫；逾時、非零退出、超量、不存在皆失敗）；`TestVersionCheckCancelled`（取消時命令立即結束、不建立資料庫） |
| 調整 | 拒絕的呼叫也全數保存紀錄（100 次留 100 筆） | 紀錄上限 `MaxCalls+1`，其餘只計次 | `TestCallRecordsBounded` |
| 調整 | 「不需網路」未寫依賴前提 | 第 4 節與 README 補上 | — |

**容器 init 差異（Codex 回報）**：Codex 的容器未加 `--init` 時，`TestRunnerTimeoutCancelAndCleanup` 的子孫回收檢查失敗，加上 `--init` 後通過。原因是被終止的子孫在父程序結束後成為 zombie，沒有 init 的 PID 1 不會回收它，而 `kill(pid, 0)` 對 zombie 仍回成功。本 repo 的 `container-verify.sh` 以 `sh` 為 PID 1，歷次容器驗證都通過；推測是 `sh` 回收了它，未另行查證。測試現在把 zombie 視為已結束（Linux 讀 `/proc/<pid>/stat` 的狀態欄位），不再依賴 PID 1 是否回收。驗證腳本的 `docker run` 參數未改。

另：在本機（macOS）新寫入的 shell 腳本第一次執行約需 0.4～0.5 秒啟動，所以 150 ms 的版本檢查在腳本實際執行前就逾時失敗，是安全的結果。Linux 上同一案例預期會在腳本印出版本後立即成功，持 pipe 的子孫同樣被回收。
