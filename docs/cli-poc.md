# B1-04 Buyer wrapper：CLI 橋接 PoC

> **狀態（2026-10-04）**：fake 首個交付點於 `b3c66f6` 通過 Codex 獨立技術 review（第 10 節）。**live 階段 A（live 開關、init 閘門、fake 驗證）於 `e8f6719` 通過 Codex 獨立技術 review，R1、R2 關閉**（第 14 節；修正歷程見第 11–13 節）。**C0 init 探測已實作（第 17 節），待 Codex 獨立 review，未執行**。階段 B、C **未執行**，沒有任何真實推論、登入檢查或 `claude -p`；live 認證接受清單維持空白。B1-04 整體仍 InProgress，未標 Done、未合併 main。這次通過不代表真實 Claude 的登入、模型或計費方式已驗證；fake 成功只證明 wrapper／bridge／Store 的行為，B1-04 的 live trace 驗收項仍保留。

## 1 流程與元件

```text
主持人 ── apa-buyer run ──┬─ fixture 商店（127.0.0.1:隨機 port，真 HTTP）
                          ├─ toolbridge.Server（Unix socket，0700 私有目錄；run 先 idle）
                          ├─ Store（SQLite，APA_DB_PATH）＋ RunExpiry：init 通過後才開
                          └─ CLI 子程序（固定 executable＋argv，獨立 process group）
                                 └─ MCP server：apa-buyer mcp-bridge（stdio）
                                        └─ 每次工具呼叫：連 socket，附本 run 的 capability
```

| 部分 | 位置 | 職責 |
|---|---|---|
| runner | `internal/buyer/runner.go` | 啟動、process group、stdout／stderr 上限、stream-json 解析、結果分類、逾時／取消、回收 |
| init 閘門 | `internal/buyer/gate.go` | 核對 `system/init`：三個完整工具名、只有 `apa` 且已連線、`dontAsk`、CLI 版本、plugins／skills／agents、認證來源（第 7.3 節） |
| host 編排 | `internal/buyer/host.go` | 由 registry 綁定身分、發 capability、寫 MCP 設定、組 argv／env、timeout／cancel 對應 Store、run trace |
| MCP adapter | `internal/toolbridge/mcp.go` | JSON-RPC 2.0、一行一訊息（MCP 2025-06-18 stdio），只服務 `initialize`、`ping`、`tools/list`、`tools/call` |
| 工具定義與參數 | `internal/toolbridge/tools.go` | 三個工具；參數只接受 `product_id`（`^[a-z0-9-]{1,64}$`），拒絕重複 key 與任何其他欄位 |
| 身分綁定與 relay | `internal/toolbridge/relay.go` | capability → `Binding`；撤銷、每 run 呼叫上限；bridge 程序端 `RunBridge`／`Dial` |
| 402 檢查與請購 | `internal/toolbridge/shop.go` | 目錄白名單、402 嚴格檢查、由可信資料組裝 PurchaseOrder、`Store.Submit`、白名單投影 |
| Store 接線與 live 開關 | `cmd/apa-buyer/main.go` | 設定來源；Store 開檔、StartTask、RunExpiry 都在 init 通過後的 `prepare`；關閉順序；live 雙重 opt-in 與版本檢查 |
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
| 總執行時間（自 CLI 啟動起，含等待 init） | 3 分鐘 | 同 | `timeout`；task 已建立才 `Store.CLITimeout` |
| SIGTERM → SIGKILL、程序結束後等 pipe | 3 秒 | 同 | 清理寬限另計：停止後最多再約兩個寬限期，不宣稱全部清理都在 3 分鐘內完成 |
| 完整 `type=assistant` 訊息 | 10 則 | 同 | 第 11 則即停止，`turn_limit`（輸出訊息上限，不是十次模型推論、API 請求或計費輪次）；user、stream_event、system 訊息不計 |
| 工具呼叫次數／run | 8 | 同 | `TOO_MANY_CALLS`；紀錄最多 9 筆 |
| init 前的業務工具呼叫 | 一律拒絕 | 同 | `RUN_NOT_READY`；不執行、不留紀錄、不計入工具次數，只計次（`not_ready_refusals`）；MCP 握手與列工具不受影響 |
| MCP 單則訊息、relay 單則訊息 | 64 KiB | 同 | 結束該 session／拒絕 |
| 單次工具呼叫 | 20 秒 | 同 | 該呼叫失敗 |

- 固定 executable 與 argv，`exec` 直接啟動，不經 shell。
- 工作目錄是空的私有暫存目錄；MCP 設定在它之外。
- 環境只帶 `HOME USER LOGNAME PATH TMPDIR LANG LC_ALL TERM`，加上固定的 `DISABLE_AUTOUPDATER=1`、`CLAUDE_CODE_DISABLE_NONESSENTIAL_TRAFFIC=1`（Aaron 10/3 同意；依官方 env-vars 文件，本機 help 未列；用來減少更新與非必要流量，不是防火牆），fake 模式再加情境路徑。API key、`ANTHROPIC_AUTH_TOKEN`、`CLAUDE_CODE_OAUTH_TOKEN`、`ANTHROPIC_PROFILE`、雲端供應商、代理、`CLAUDE_CONFIG_DIR` 與 host 自己的設定都不傳。fake 模式的 `HOME` 換成每次新建的空目錄，fake 不會靠近使用者的登入。
- `--version` 檢查（fake 命令確認 `--fake-cli` 確實是 fake；live 要求輸出恰為核准版本 `2.1.288 (Claude Code)`，10/3 前為 2.1.287）也走同一套程序規則：獨立 process group、輸出 4 KiB、stderr 4 KiB、5 秒，結束時回收整個 group。逾時、取消、超量或非零退出都視為失敗，不啟動 task；啟動 task 前再確認一次未被取消。
- 子程序在自己的 process group。任何停止都對整個 group 送 SIGTERM，期限後 SIGKILL；程序自行結束後也立即 SIGKILL 該 group，回收仍握著 pipe 的子孫。
- 已離開 group（例如 `setsid`）的程序不會被送信號：等候期限後放棄 pipe，結果標 `orphaned_output`。這是已知限制，見第 8 節。
- 不使用 `--dangerously-skip-permissions`、`--bare`、`--safe-mode` 或 `bypassPermissions`。

**結果分類**：`ok` 必須同時有程序 exit 0、恰好一則 `result` 訊息、`subtype: success` 且 `is_error: false`。exit 0 不足以判定成功。其他：`not_installed`、`start_failed`、`not_logged_in`、`rate_limited`、`timeout`、`cancelled`、`output_overflow`、`parse_error`（非 JSON 行、截斷、缺 `type`、第二則 result）、`nonzero_exit`、`missing_result`、`cli_error`、`orphaned_output`，以及 host 自己檢查的 run 診斷：`config_mismatch`（init 缺漏、不符、格式錯、重複，或出現 hook 訊息；`detail` 欄位說明是哪一項）、`turn_limit`、`store_unavailable`（init 通過但 Store 或 task 準備失敗）。這些都只是 run 診斷，不是 `apa.m1.v2` 的 reason code 或事件。

**Store 寫入界線（階段 A，A1）**：

| 時點 | Host 對 Store 做的事 |
|---|---|
| init 通過前（含缺 init、init 不符、init 前的 hook 訊息、版本或 opt-in 不符） | 不開啟、不建立目標 SQLite DB，不建立 task，不執行到期寫入。只寫 run trace。這不保證 CLI 自己不寫 `~/.claude` 或其他主機檔案 |
| init 通過、run 仍有效 | `prepare`：開 Store（migration、恢復驗證、補記到期，照已驗收的 Store 行為）→ StartTask → RunExpiry；成功後才放行業務工具 |
| `prepare` 失敗（含 task 已存在） | 不放行工具、不新增請購，依 Store 規則回錯，不刪檔、不修復。task 已存在時，CLI 已經啟動，live 下可能已有用量 |
| 已有合法請購後才出現 hook 訊息、第二則 init、訊息超限、解析失敗等 | 合法請購保留；run 立即停用（之後的提交都被拒），撤銷並等待進行中的呼叫結束 |
| run 停止後才讀到的 init | 不開 Store、不放行；`prepare` 若已開始，run 等它結束才算結束 |

只有 `timeout`（`TASK_TIMED_OUT`，Ask 保留待到期）與 `cancelled`（`TASK_CANCELLED`，Ask 撤回）寫 Store，且只在本 run 已建立 task 時；沒有 task 時不為記錄失敗而建立 task。其他失敗不新增契約事件，診斷留在 run trace。既有 Deny 的狀態與理由不被後續結束事件覆蓋。task 已結束時 Store 拒絕第二次結束，回報 `TASK_ALREADY_ENDED`。結束後才到達的請購同樣由 Store 拒絕。

**run trace**（`--trace-dir`，預設 `var/runs/`，檔案 0600）：與 AuditEvent 鏈分開的 JSONL，只有結構化的診斷：task、consumer、CLI 檔名、prompt 的 SHA-256（不存 prompt）、上限、init 閘門結果碼、host 要求的 model（`requested_model`）、init 所列 model（只在明列名稱 `sonnet`、`claude-sonnet-5-5` 之內時保存，否則記 `unrecognized`；格式像名稱不等於安全，64 位十六進位的 capability 也像名稱）、task 是否建立、prepare 失敗代碼、每次工具呼叫（工具名、host 提供的商品 ID；CLI 選的 ID 即使格式合法、但不是提供的商品就不記，結果碼）、結果分類與 `detail`、exit code、訊息數、assistant 訊息數、usage 的四個 token 計數（沒有就 `unknown`，不補零）、工具嘗試次數、init 前拒絕次數、stderr 位元組數、CLI 結果文字的字數（`result_chars`）。**CLI 的自由文字一律不保存**（結果可能回顯 prompt 或其他內容；`ca46cab` review R1）。也不含原始 stream、capability、環境變數、stderr 內容或 `apiKeySource` 的值；測試以識別標記與回顯 prompt 的 fake 確認。

## 4 從乾淨環境重跑 fake 流程

先決條件：Go 1.26、make、這個 repo，以及 Go 依賴（`modernc.org/sqlite`）已下載或在模組快取中：首次建置需能連 `proxy.golang.org`，之後可離線。不需要 Claude Code CLI、登入或 Docker。

```sh
APA_DB_PATH=var/apa-fake.db make demo-fake
```

輸出第一行是 `FAKE MODE: …`，接著是 JSON 報告：工具呼叫、Store 裡的 task 與請購（主商品為 Ask、`payment_status: unavailable`）與 trace 路徑。

- DB 路徑遵守 `APA_DB_PATH`；未設定時 fake 命令用 `var/apa-fake.db`，不寫進 demo 預設的 `var/apa.db`。目錄須為 0700。
- 同一 DB 中 task 只會啟動一次。重跑同一 task 會清楚回報並結束，不刪除任何紀錄。可改用 `TASK=task-a-next make demo-fake`，或把 `APA_DB_PATH` 指到新檔。
- 命令會先執行 `--fake-cli --version`，輸出不含 `fake-claude` 就拒絕。誤把真實 `claude` 當成 fake 也不會跑推論。
- 不帶 `--fake-cli` 也沒有 `--live` 時直接拒絕。fake 命令走與 live 相同的 init 閘門、`prepare`、relay、真 HTTP 402 與 Store 路徑；閘門接受的是 fake 自己標示的值（`claude_code_version` `0.0.0`、`apiKeySource` `fake-claude-no-auth`），不是任何真實 CLI 的值。

測試：

```sh
go test ./internal/buyer/... ./internal/toolbridge/... ./cmd/apa-buyer/ -count=1
```

測試二進位同時扮演 fake CLI（經名為 `fake-claude` 的 symlink）與 MCP bridge（argv `mcp-bridge`），所以每個測試都啟動真實子程序。

只重跑階段 A 的編排（只用 fake CLI；live 路徑的測試以 fake 頂替 `--cli`，注入 fake 的版本與認證標示值，scratch HOME，不會從 PATH 找任何 CLI）：

```sh
go test ./internal/toolbridge/ ./internal/buyer/ ./cmd/apa-buyer/ -count=1 \
  -run 'NotReady|Init|Hook|Prepare|Assistant|Trace|Live|NoStore|StoreUnavailable|ArgvAndEnv|Commit|Revoke'
```

## 5 Store 接線（由 B1-06 移來的部分）

這次承接、已在 `cmd/apa-buyer` 實作：

- 設定來源：registry `testdata/contracts/registry.json`、profile `testdata/policy/m1-demo-profile.json`（皆 fixture，可用旗標指定）。credential 是啟動時以 host clock 產生的 M1 fixture，有效 24 小時，不驗證 BBS+。budget 預設為 profile 的 task cap。報價簿由商店設定 `merchant.Offers()` 建立。
- 可信 host clock（Unix 秒）；測試用固定時鐘。
- 啟動順序（階段 A 起）：商店 → bridge（run 為 idle）→ CLI → `system/init` 通過閘門 → `prepare`：Store 開檔（含恢復驗證與補記到期）→ StartTask → RunExpiry → 放行業務工具。init 通過前不開 Store。
- RunExpiry 每秒一次；錯誤寫到 stderr，報告列出次數，不吞掉。
- 關閉順序：run 結束（取消時先回收 CLI）→ 關 bridge → 停 RunExpiry 並等它結束 → 關 Store → 關商店。

仍屬 B1-06：`cmd/demo` 尚未開啟 Store；主持人事件 HTTP 端點、展示層、完整服務接線、live opt-in 指令與 M1 端到端驗收。

## 6 CLI 相容性（本機 2.1.287，2026-10-02；10/3 補 help 細節與文件查閱）

只執行了 `claude --version` 與 `--help`／`auth --help`，沒有送出推論請求，也沒有執行 `claude auth status`。階段 A 起 argv 改為第 7.3 節所列；下表保留首輪的核對紀錄。

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
| `--setting-sources project` | 有：`user, project, local` | 階段 A 起改用 `--restricted`，不再傳 |
| `--restricted` | 有：移除會執行程式的內建工具與 WebFetch，忽略 user／project／local 設定檔（managed settings 與 `--settings` 仍適用） | 未以真實 run 驗證；help 沒提 CLAUDE.md |
| `--permission-prompts none` | 有：會提示的動作自動拒絕 | 未以真實 run 驗證 |
| `--model`、`--effort` | 有（`sonnet` 為別名；effort 有 `low`） | 候選值 `sonnet`／`low`，階段 C 前要確認版本支援與實際模型 |
| `--max-budget-usd` | 有（僅 `--print`） | 不使用，不宣稱為訂閱用量上限 |
| `--no-session-persistence` | 有 | — |
| `--disable-slash-commands` | 有 | — |
| `--max-turns` | **本機 help 沒有** | 官方 CLI 參考有列；保留版本差異，不假定支援、不以推論試跑；改由 host 計 assistant 訊息 |
| `--bare` | 有：不讀 OAuth／keychain | 不使用（會改走 API key） |
| exit code、登入失敗與限流的輸出 | — | 分類規則（`authentication_failed`、`rate_limit`、結果文字含 `/login`、`Not logged in`、`rate limit`）**未以實際失敗驗證** |

fake CLI 的 stream-json 依文件形狀產生，**不是真實 CLI 的錄製**；它也不模擬 CLI 的權限層（每個腳本化的工具呼叫都會送到 bridge，以測 host 自己的檢查）。

**init 訊息的依據（10/3 查閱 Agent SDK TypeScript 參考）**：init 的欄位有 `apiKeySource`、`tools`（字串陣列）、`mcp_servers`（`name`、`status`，status 為 `"connected" | "disconnected" | "error"`）、`model`、`permissionMode`（含 `"dontAsk"`）、`claude_code_version`、`cwd`，以及選填的 `skills`、`plugins`、`agents`（`AgentInfo` 為 `name`、`description`、`source`）；hook 相關 system 訊息的 subtype 為 `hook_started`、`hook_progress`、`hook_response`。**`apiKeySource` 的可能值未能確認**：同一頁第一次摘要列出一組值，要求逐字複製時卻找不到該型別定義，因此不採用任何值。這是 SDK 文件，不是 CLI 2.1.287 實際 init 的錄製。

**認證判定的依據（10/3 查閱官方認證文件）**：該文件所載的認證優先順序為雲端供應商、`ANTHROPIC_AUTH_TOKEN`、`ANTHROPIC_API_KEY`、`apiKeyHelper`、`CLAUDE_CODE_OAUTH_TOKEN`、Anthropic profile 與 federation 憑證，最後才是 `/login` 的訂閱 OAuth；Console 也可不建 API key 登入。這是文件的說法，**未在本機或真實 run 驗證**。因此「沒有 API key」或 `apiKeySource` 為 `none` 都不能推定為訂閱。兩件事要分開：(1) 前述環境變數，`CLIEnv` 的 allowlist 本來就不傳給 host 啟動的 CLI；(2) CLI 透過 HOME 或其他設定來源可能取得的憑證（managed settings 或 settings 檔的 `apiKeyHelper`、keychain 中先前 Console 登入留下的 API key、`~/.config/anthropic` 的 profile），環境 allowlist 管不到，host 也無法從 init 排除。

## 7 最小 live 驗證方案（2026-10-03 擬定；階段 A 於 `e8f6719` 通過技術 review；B、C 未執行）

目標只有一個：用本人登入的官方 CLI 跑**一次**真實 run，取得 CLI → 402 → 結構化請購的 trace，驗證第 2、3 節的邊界在真實 CLI 下仍成立。這不是擴大範圍或壓測。

### 7.1 階段

| 階段 | 內容 | 推論 | 狀態 |
|---|---|---|---|
| A 實作 live 開關與閘門 | 7.3 的程式變更，以 fake CLI 測試每個閘門 | 無 | **Aaron 10/3 核准；`e8f6719` 通過 Codex 獨立技術 review**（第 14 節） |
| B 執行前確認 | Aaron 本人確認 `claude --version`、是否已登入，以及非敏感的認證方式／訂閱確認（例如是 claude.ai 訂閱登入，且沒有 API key、token、profile 或 apiKeyHelper 在用），要對應預定的 CLI 與設定環境。不貼整份 JSON、email、帳號 ID、token 或 keychain 內容；repo 與 Claude 不讀取該輸出 | 無 | 未執行；A 通過 review 後 |
| C 單次 live run | 7.2 的一條命令、一個 task、一次；任何失敗即停，不重跑 | **有**（次數無硬保證，見 7.3） | **未核准**；需 Aaron 對這一次另行明確同意 |
| D 證據整理 | 只保留白名單化的診斷與 run trace；交 Codex review | 無 | 沒有 live 結果，不得預先宣稱 live 證據成立 |

### 7.2 階段 C 的命令（已實作，但尚無法通過閘門；本輪未執行）

```sh
APA_LIVE_CLI=true APA_DB_PATH=var/apa-live.db \
  ./bin/apa-buyer run --live --cli /Users/mac/.local/bin/claude --task task-a
```

- `APA_LIVE_CLI=true` 與 `--live` 必須同時存在（`APA_LIVE_CLI` 依 `strconv.ParseBool`，其他值視為未開啟）；`--live` 不得與 `--fake-cli`／`--scenario` 併用，`--cli` 只限 `--live`。`--cli` 必須是絕對路徑，不查 PATH、不 fallback。`--version` 必須恰為 `2.1.288 (Claude Code)`（10/3 由 2.1.287 改，見第 15.6 節）；不符就停止，不升級、不降級。以上任何一項不符：不執行 CLI 的推論、不開 Store。
- 資料庫預設 `var/apa-live.db`（`APA_DB_PATH` 可覆寫，規則同既有）；fake 預設 `var/apa-fake.db`、demo 預設 `var/apa.db` 不變。task 已存在就照既有規則拒絕，不刪紀錄。
- **目前 live 的認證接受清單是空的**（`liveAcceptAuth`），所以任何 live init 都會在 `auth_unconfirmed`／`auth_missing` 停下，不開 Store。決定接受值、以及其文件依據，是階段 C 開跑前的待決事項；不得在第一次失敗後自動學習或放寬，trace 也不記錄 `apiKeySource` 的值。

### 7.3 階段 A 的內容（已實作）

**設定**：argv 為 `-p <host prompt> --output-format stream-json --verbose --mcp-config <cfg> --strict-mcp-config --restricted --tools "" --permission-mode dontAsk --permission-prompts none --allowedTools mcp__apa__list_products,mcp__apa__get_quote,mcp__apa__request_purchase --model sonnet --effort low --no-session-persistence --disable-slash-commands`。`--tools` 的值是空字串本身。模型不能更動 executable、flags、cwd、server 或環境。環境見第 3 節。

**init 閘門**：run 註冊後是 idle；MCP 握手與列工具照常，三個業務工具一律 `RUN_NOT_READY`。runner 每讀一則 stream-json 就交給 host 檢查；唯一一則 `system/init` 依序核對下列項目，通過且 run 仍有效，才執行 `prepare`（Store、task、RunExpiry），成功後放行：

- 讀取規則（`ca46cab` review R2）：每一則 stream-json 在分類（`type`／`subtype`）與任何檢查之前，先以同一個嚴格規則讀取。任何層級的物件只要有成員名重複，不論完全相同，或依 Unicode 簡單大小寫折疊後相同（涵蓋 `encoding/json` 會視為同一欄位的拼法），整則訊息即為 `parse_error`（`detail` 為 `ambiguous_json`），run 停止。之後只依確切欄位名讀取，大小寫不同的拼法不算該欄位。閘門用的是同一個讀取函式。
- JSON 型別錯誤（例如 `tools` 不是字串陣列）：`init_malformed`。
- `tools`：恰為三個完整工具名，各一次。不是只比數量或前綴，重複值不能補足缺項（`tools`）。
- `mcp_servers`：恰一個，`name` 為 `apa`，`status` 為 `connected`（`mcp_servers`）。
- `permissionMode` 為 `dontAsk`（`permission_mode`）。
- `claude_code_version`：等於核准版本的版號 `2.1.288`；fake 為 `0.0.0`（`cli_version`）。
- `plugins`、`skills`、`agents`：若出現，必須是空陣列，沒有任何項目獲准（同名代碼）。欄位不存在不證明主機沒有載入這些內容。
- 認證：`apiKeySource` 必須是字串，且恰在接受清單內（`auth_missing`／`auth_unconfirmed`）。live 清單為空（7.2）。init 只能拒絕已知不相容或未知的來源，**不能獨自證明計費來源**，訂閱登入由階段 B 另行確認。

缺 init（`init_missing`）、第二則 init（`init_duplicate`）或任何 hook 相關 system 訊息（subtype 含 `hook`，`hook_message`）都停止 run，閘門不會重新開啟。這是在檢查 CLI 的自述，不是 OS 隔離，也不代表 CLI 完全可信。工具白名單維持三個，不為 `EndConversation` 開例外：依 10/3 review 查閱的官方工具頁，它不出現在非互動 `-p` 模式；若實際版本不同，停止並回報，不擴大集合。

**init 檢查失敗時的用量**：CLI 可能已產生模型請求與用量，請求次數沒有硬保證。Host 尚未放行本專案的業務工具；這不等於阻止 CLI 自己的網路或檔案行為。

**上限**：見第 3 節（3 分鐘、assistant 訊息 10 則、工具 8 次、既有輸出上限）。

**fake 驗證**：fake 的 init 欄位名依 SDK 參考，值是 fake 自己的標示，不是真實 CLI 的錄製。fake 在印出 init 後，若業務呼叫被 `RUN_NOT_READY` 拒絕，會每 20 ms 重送、最多 5 秒，模擬真實 CLI 首輪模型往返給 host 的準備時間。live 路徑的測試只以 fake 頂替 `--cli`，fake 在 live 環境下從 `<自身路徑>.scenario.json` 讀情境，所以 live 環境不需要多帶任何變數。

### 7.4 停止條件

下列任一情況都立即停止並回報，不自動重跑，也不開下一輪：

- 未登入、限流、`config_mismatch`、`turn_limit`、`store_unavailable`、逾時。
- 任何非 `ok` 的結果。
- 版本不符。
- Store 出現非預期事件。

### 7.5 已知的程序隔離限制（live 下仍然存在）

1. **這不是作業系統沙箱。** CLI 以使用者本人的權限執行。host 能限制的是工具呼叫，限制不了 CLI 程序本身會做什麼。CLI 不載入使用者設定、沒有內建工具，都只依賴 CLI 旗標照 help 所述運作；若 CLI 行為與說明不符，host 擋不住檔案或網路存取。init 閘門只能抓到 CLI 自己在 init 說出的不符。
2. **HOME 會傳給 CLI**，因為 CLI 要靠它找到登入。CLI 可能在 `~/.claude` 寫入快取或紀錄（即使用了 `--no-session-persistence`），也會讀 keychain 與 `~/.config/anthropic`。repo 不讀、不複製這些內容。
3. **CLI 的網路存取不受限**：它需要連 Anthropic API。模型沒有網路工具；兩個固定環境變數只是減少更新與非必要流量，不是防火牆，也不排除 plugin 或 managed 設定的副作用。
4. **process group**：CLI 若以獨立 group 或 session 啟動子程序（例如 detached 的 MCP server 或更新程式），host 的 group kill 收不到它們。這種情況只能放棄 pipe，結果標為 `orphaned_output`。bridge 會在 stdin 關閉時結束，capability 也已撤銷。
5. **同一 OS 使用者的其他程序**，在 run 期間可讀取 MCP 設定檔、bridge 環境變數與 socket，所以讀得到 capability。
6. **使用者層 CLAUDE.md 可能仍會載入。** help 對 `--restricted` 只提到設定檔；init 也看不出有沒有載入。
7. **init 可能列出內建 agents 等項目。** 閘門不核准任何項目，真實 CLI 若在 `--tools ""` 下仍列出內建 agents，第一次 run 會在閘門停下（fail closed）；是否核准任何項目屬階段 C 前的決定。
8. **assistant 訊息上限是事後觀察**：超過的那一則已經發生，也不等於模型請求次數。
9. **用量只取 CLI 回報的 `result.usage` 四個 token 計數**，標為「CLI 回報」，沒有就記 `unknown`。不等於帳單，也和 test USDC、gas 分開。

### 7.6 仍待 Aaron 決定或確認（B、C 前）

1. 階段 B：由 Aaron 本人確認版本、登入與否、非敏感的認證方式／訂閱確認（見 7.1）。
2. live 認證接受值：目前清單為空。要接受哪個 `apiKeySource` 值、依據為何，或維持空清單（則 live 必定停在閘門）。
3. `--model sonnet`、`--effort low` 是否就是要用的設定；實際版本是否支援。
4. init 若列出內建 agents 或 skills，是否核准任何項目（目前一律拒絕）。
5. 階段 C 的單次 run，另行明確核准。

本人訂閱登入官方 CLI，不代表任何第三方產品或多人服務模式已獲准。公開多人服務須另行確認條款與計費。

## 8 尚未驗證或未完成

- 真實 CLI 的 stream-json 實際內容、exit code、未登入與限流的實際輸出。
- `--restricted`、`--permission-prompts none` 在真實 run 下的實際效果；CLAUDE.md 是否仍載入；真實 CLI 下 MCP server 的程序群組與 stderr 繼承方式。
- 真實 CLI（核准版本 2.1.288）的 init 實際欄位、`apiKeySource` 的實際值，以及內建 agents／skills 是否出現在 init。
- 最大模型輪次（本機無 `--max-turns`；host 以 assistant 訊息數代替）。
- 離開 process group 的子孫程序不會被終止，只能放棄 pipe 並標 `orphaned_output`；要完整回收需作業系統層沙箱或容器。
- 同一 OS 使用者的其他程序可讀取 run 期間的 capability（見第 2 節）。
- CLI 正常結束或輸出解析失敗時，Store 的 task 不會結束（只有 timeout／cancel 有 Store 語意）。這個 run 之後已無法寫入（第 2 節）；同一 task 的後續 run 仍可合法提出請購。
- 主持人 HTTP 端點、UI、`cmd/demo` 接線與端到端驗收屬 B1-06。

## 9 `ba0b440` review 修正（2026-10-02，Claude Code 執行；10/3 於 `b3c66f6` 通過 Codex 獨立 review，見第 10 節）

| 項目 | review 發現 | 修正 | 回歸測試 |
|---|---|---|---|
| R1 | 撤銷只移除 capability：已取得 `Binding` 的呼叫照常執行，402 放行後仍新增 Ask；host 在呼叫完成前取紀錄快照 | `relay.go`：`Binding` 帶 run context 與進行中計數；`Revoke`／`Close` 結束 run＝取消 context、等進行中呼叫結束；寫入經 `Commit` 與結束互斥。`shop.go`：`Submit` 改經 `Commit`；run 中途結束的呼叫結果為 `RUN_NOT_ACTIVE`。`host.go`：`Revoke` 返回後才取紀錄、寫 Store 結束事件與 trace | `TestRevokeWithCallInFlight`（真 HTTP＋Store：請購卡在 402 時撤銷，`Revoke` 不等商店就返回、紀錄已完整、Store 無請購）；`TestCommitGate`（已過檢查的呼叫在結束後寫不進去；結束等待進行中呼叫並取消其 context；結束等待進行中的寫入，之後拒絕寫入） |
| R2 | `checkFake` 用 `exec.CommandContext().Output()`：沒有 process group、pipe 清理與輸出上限；150 ms context 加上持 pipe 的子孫，約 2 秒後仍回傳成功 | `runner.go` 抽出共用的 `execute`；新增 `Version` 以同一套規則執行 `--version`；`checkFake` 改用它；StartTask 前再確認 context 未取消 | `TestVersion`（持 pipe 的子孫被回收且不拖延回應；150 ms context 立即結束且不留子孫；逾時、非零退出、超量、不存在皆失敗）；`TestVersionCheckCancelled`（取消時命令立即結束、不建立資料庫） |
| 調整 | 拒絕的呼叫也全數保存紀錄（100 次留 100 筆） | 紀錄上限 `MaxCalls+1`，其餘只計次 | `TestCallRecordsBounded` |
| 調整 | 「不需網路」未寫依賴前提 | 第 4 節與 README 補上 | — |

**容器 init 差異（Codex 回報）**：Codex 的容器未加 `--init` 時，`TestRunnerTimeoutCancelAndCleanup` 的子孫回收檢查失敗，加上 `--init` 後通過。原因是被終止的子孫在父程序結束後成為 zombie，沒有 init 的 PID 1 不會回收它，而 `kill(pid, 0)` 對 zombie 仍回成功。本 repo 的 `container-verify.sh` 以 `sh` 為 PID 1，歷次容器驗證都通過；推測是 `sh` 回收了它，未另行查證。測試現在把 zombie 視為已結束（Linux 讀 `/proc/<pid>/stat` 的狀態欄位），不再依賴 PID 1 是否回收。驗證腳本的 `docker run` 參數未改。

另：在本機（macOS）新寫入的 shell 腳本第一次執行約需 0.4～0.5 秒啟動，所以 150 ms 的版本檢查在腳本實際執行前就逾時失敗，是安全的結果。Linux 上同一案例預期會在腳本印出版本後立即成功，持 pipe 的子孫同樣被回收。

## 10 驗收紀錄：fake 首個交付點於 `b3c66f6` 通過技術 review（2026-10-03）

- 結果：`b3c66f6` 通過 Codex 獨立 review，R1、R2 關閉，本次範圍內沒有新的阻擋問題。**通過的是 fake 首個交付點的技術驗收**；B1-04 整體仍為 InProgress，未標 Done、未合併 main。live 驗收項未執行（第 7 節）。
- Codex 確認：
  - R1：重跑延遲 402 的探測，撤銷後沒有新增請購，拒絕紀錄完整；寫入與撤銷互斥的測試通過。
  - R2：原本約 2 秒的版本檢查探測，現在約 3 ms 結束；取消、輸出超量與子程序清理測試通過。
  - 紀錄上限：100 次嘗試，只執行 8 次，保留 9 筆紀錄。
  - 不加 `--init` 的容器測試通過。
- 證據歸屬：
  - **Codex 獨立重跑**：全套 race／coverage；相關回歸與探測各三輪；JS 契約向量；20 項匯入器測試；B1-01 回歸。全部通過。
  - **Claude Code 執行、Codex 核對紀錄**（不是 Codex 全數重跑）：133／133 突變與容器完整驗證，見 `artifacts/verify-container/runs/b3c66f6cac36-20261002T153256Z/`；本機檢查見 `artifacts/b1-04/b3c66f6cac36-20261002T153035Z/`。
  - 首輪 review（`ba0b440`）的兩項必修與兩項調整見第 9 節。
- `var/apa-fake.db` 與 demo 預設資料庫分開：Codex review 已接受。

## 11 階段 A 實作紀錄（2026-10-03，Claude Code 執行，待 Codex 獨立 review）

依 Aaron 10/3 核准的階段 A 指示（vault：`Claude-Code-B1-04-live階段A實作指示-87ce160-20261003`），起點 `87ce160`。

| 項目 | 修改位置 | 不變式 | 測試 | 突變 |
|---|---|---|---|---|
| A1 Store 寫入界線 | `relay.go`（`Binding` 狀態 idle → armed → stopped → ended；`Arm`／`Disarm`；`Commit` 只在 armed）；`host.go`（`Prepare`、watch、`Stopping: b.Disarm`、task 存在才寫結束事件）；`cmd/apa-buyer/main.go`（Store 只在 `prepare` 開；關閉順序） | init 通過前不開／不建 DB、不建 task、不做到期寫入；init 前業務工具一律拒絕；停止中的 run 不再放行、不再提交；`prepare` 進行中時 run 等它結束 | `TestNotReadyUntilArmed`、`TestNoStoreBeforeInit`（新 DB 不建立；含到期 Ask 的既有 DB 逐位元組不變）、`TestStoreUnavailableAfterInit`、`TestHostPrepareFails`、`TestHostInitSequence`（缺 init、init 前 hook、停止後才到的 init、重複 init）、`TestHostHookAfterPurchase`、`TestHostCancelDuringPrepare`、`TestHostEndings`（無 task 時不寫結束事件），以及 R1 的 `TestRevokeWithCallInFlight`、`TestCommitGate` | #134–136、#146–151、#157 |
| A2 認證判定與 init 閘門 | `internal/buyer/gate.go`；`cmd/apa-buyer/main.go`（`liveAcceptAuth` 為空） | 只接受明列值；未知、缺失、型別錯誤、重複成員皆拒絕；不推定訂閱 | `TestHostInitGate`（18 種 init）、`TestLivePathWithFake`（空清單時 `auth_unconfirmed` 且 Store 未開） | #137–145 |
| A3 CLI 設定與上限 | `host.go`（argv、固定環境）、`runner.go`（`Limits.Assistant`、3 分鐘）、`cmd/apa-buyer/main.go`（雙重 opt-in、絕對路徑、核准版本、`var/apa-live.db`） | argv 固定；`--tools` 為空字串；不用 `--safe-mode`／`--bare`；環境只有 allowlist 加兩個固定值；assistant 第 11 則停止 | `TestArgvAndEnv`、`TestAssistantLimit`、`TestLiveOptIn`（各種 opt-in 缺漏不執行 CLI、不開 Store；版本不符、版本檢查逾時）、`TestLivePathWithFake`、`TestVersion`、`TestVersionCheckCancelled` | #153–156、#158–160 |
| A4 trace 白名單 | `host.go`（usage 只留四個計數；不存 `apiKeySource`、原始 stream、stderr、prompt、環境、capability） | 識別標記不進證據 | `TestHostTraceWhitelist`、`TestHostCapabilityLifetime` | #152、#123、#125 |

既有 B1-04 突變中，文字隨重構移動的 #122、#126、#127、#129、#130、#132 改指新程式碼（意圖不變）。共 160 項，原 99 項文字未動。驗證結果見 vault 任務清單的 B1-04 交付紀錄。

## 12 `ca46cab` review 修正（2026-10-03，Claude Code 執行，待 Codex 獨立 review）

Codex 以 fake CLI 在隔離容器重現兩項問題（沒有執行真實推論）。live 認證清單維持空白。

| 項目 | review 發現 | 修正 | 回歸測試 |
|---|---|---|---|
| R1 | trace 仍保存 `untrusted_cli_result`；只替換 capability、去除控制字元與截短，不是遮罩。fake 在結果回顯完整 prompt，三輪都原文進 trace | `host.go`：trace 不再保存任何 CLI 自由文字，只記結果字數 `result_chars`；init 的 model 只在符合模型名稱格式時保存，否則 `unrecognized`；移除 capability 替換（已無自由文字可替換） | `TestHostTraceWhitelist`：fake 在結果回顯 prompt（先確認回顯確實發生），trace 不含 prompt 任一片段，只有字數；model 帶 prompt 片段時記為 `unrecognized`。`TestHostMainLine`、`TestHostCapabilityLifetime` 確認結果文字與 capability 都不在 trace |
| R2 | stream 先用一般 JSON 解碼判斷種類，重複欄位檢查只在辨識為 init 後執行，也未處理大小寫別名。`"permissionMode":"default"` 加 `"PermissionMode":"dontAsk"` 通過閘門並產生 Ask；`"subtype":"hook_started"` 加 `"subtype":"status"` 未觸發 hook 停止 | `gate.go`：新增 `objectFields`（任何層級的重複成員名，含 Unicode 簡單大小寫折疊後相同者，一律拒絕；之後依確切名稱讀取）與 `field`；閘門改用它。`runner.go`：每則訊息先經 `objectFields`，歧義即 `parse_error`／`ambiguous_json`，之後的分類、assistant 計數、Watch 與閘門都讀同一份結果 | `TestObjectFields`（完全重複、大小寫別名、Kelvin 符號、巢狀與陣列中的物件、非物件、尾隨資料；大小寫不同的拼法不算該欄位）；`TestHostAmbiguousMessages`（review 的兩案及其變體：init 的 `permissionMode` 大小寫別名與完全重複、hook subtype 重複於 init 前，以及 init 後的大小寫別名 hook 訊息，其後的請購不會落帳）；`TestHostInitGate` 的重複成員案例改由讀取端以 `ambiguous_json` 擋下 |

測試時序：容器驗證中，重複 init 案例在 init 之後緊接送出的工具呼叫，曾在 host 讀到該行之前抵達 relay，被放行後隨即因停止而取消（記為 `RUN_NOT_ACTIVE`），而測試要求之後完全沒有紀錄。產品行為正確（沒有新請購，之後的呼叫都被拒），是測試過嚴：情境在停止行之後暫停 500 ms，斷言改為「請購不變，停止後的紀錄都是 `RUN_NOT_ACTIVE`」，hook 與歧義訊息的同類案例一併處理。

突變：#123 改為「CLI 結果文字寫入 trace」（原 capability 替換已移除）；#145 改指共用的 `objectFields` 歧義檢查（讀取端與閘門同一處）；#139–142、#144 因閘門改以確切欄位讀取而改指新程式碼（意圖不變）；新增 #161（成員名不做大小寫折疊）、#162（model 自由文字進 trace）。共 162 項，原 99 項文字未動。

## 13 `59da5d9` review 修正：trace 只保存明列名稱（2026-10-03，Claude Code 執行，待 Codex 獨立 review）

R2 已由 review 關閉。R1 仍有一個缺口：model 只檢查名稱格式，64 位十六進位的 capability 也符合格式；Codex 讓假 CLI 把該 run 的假通行碼放進 init 的 `model`，三輪都寫進了 trace（未涉及真實登入憑證）。因此「已無自由文字，可移除 capability 替換」的判斷不成立。

| 修改 | 位置 | 回歸測試 | 突變 |
|---|---|---|---|
| trace 保存 host 設定的 `requested_model`；CLI 回報的 model 只在明列集合（`sonnet`、`claude-sonnet-5-5`）內才保存，其他一律 `unrecognized` | `internal/buyer/host.go` | `TestHostTraceKeepsOnlyListedNames`（capability 當 model、符合名稱格式的敏感標記當 model：皆記 `unrecognized`，trace 不含）；Codex 的探測程式在暫存副本上重跑三輪通過 | #162（改指新程式碼）、#164 |
| 同類缺口一併修：工具呼叫紀錄的 `product_id` 原本只驗格式（`^[a-z0-9-]{1,64}$`，capability 也符合），改為只記 host 提供的商品 | `internal/toolbridge/shop.go` | `TestHostTraceKeepsOnlyListedNames`（capability 當 `product_id`：`UNKNOWN_PRODUCT`，紀錄不帶 ID，trace 不含）；`TestPurchaseIntegrity` | #163 |

逐一檢查 trace 其餘欄位：task、consumer、CLI 檔名、上限來自 host 設定；閘門、結果、`detail`、prepare 與 Store 代碼都是 host 定義的代碼；工具名只有三個已知名稱或 `unknown`；其餘是數字或布林。沒有其他由 CLI 決定內容的字串。共 164 項突變，原 99 項文字未動。

## 14 驗收紀錄：live 階段 A 於 `e8f6719` 通過技術 review（2026-10-03）

- 結果：`e8f6719` 通過 Codex 獨立技術 review，階段 A 的 R1、R2 關閉，本次檢查範圍內沒有新的阻擋問題。
  - model 修正通過：把通行碼塞進 model 的探測連跑三輪，trace 都沒有洩漏。
  - `product_id` 延伸修正可接受：屬同一個紀錄邊界問題；未知商品不保留 ID，合法商品的請購與重送測試仍通過。
  - R2 未退化：JSON 重複欄位與大小寫歧義的原始探測三輪皆通過。
- **通過的是階段 A 的技術驗收**：live 開關、init 閘門、init 通過前不寫 Store、上限與 trace 白名單，全部以 fake CLI 驗證。B1-04 整體仍 InProgress，未標 Done、未合併 main。live 認證接受清單維持空白；階段 B、C 仍需 Aaron 另行核准。這次通過**不代表**真實 Claude 的登入、模型或計費方式已驗證。
- 證據歸屬：
  - **Codex 獨立重跑**：斷網隔離容器內全套 race／coverage；相關回歸三輪；四項原始探測各三輪（prompt 回顯、init 大小寫歧義、hook subtype 重複、model 夾帶通行碼）；JS 契約向量；20 項證據匯入測試。全部通過。
  - **Claude Code 執行、Codex 核對紀錄**（不是 Codex 獨立重跑）：164／164 突變、完整容器驗證七步、17 項隔離檢查，見 `artifacts/verify-container/runs/e8f67192ca7c-20261003T033120Z/`；本機檢查見 `artifacts/b1-04/e8f67192ca7c-20261003T032813Z/`。
- 歷程：`ca46cab`（首次交付；R1、R2）→ `59da5d9`（R2 關閉；R1 的 model 缺口）→ `e8f6719`（通過）。修正內容見第 12、13 節。

## 15 第 7.6 節前置條件比對與「真實 init 最小驗證」提案（2026-10-03；提案，未核准、未實作、未執行）

原則（Aaron 10/3）：**登入成功不等於已證明訂閱計費，也不能據此放寬 `apiKeySource` 清單。** 本節只比對與提案；沒有執行登入檢查、`claude auth status`、`claude -p` 或任何推論。這次只讀了本機 `claude auth status --help`（有 `--json`（預設）與 `--text`）與 `claude --help` 的 `--input-format`（`-p` 模式可用 `stream-json` 輸入）。

### 15.1 比對

| 7.6 項目 | 目前狀態 | 缺什麼 |
|---|---|---|
| 1 階段 B：版本、登入與否、非敏感的認證方式／訂閱確認 | 未執行。就算登入成功，也只證明機器上有一個可用憑證，不證明某次 run 實際用哪個憑證、怎麼計費 | Aaron 本人回報 `claude --version`，以及 `claude auth status --text` 中「登入方式／方案類型」一行（不貼其他內容）。另只回報「有／無／不確定」：是否有 managed settings 檔、settings 檔是否設定 `apiKeyHelper`、keychain 是否留有先前 Console 登入的 API key、`~/.config/anthropic` 是否有 active profile。這些是 CLI 可能經由 HOME 或設定檔取得的憑證來源，環境 allowlist 管不到；至於 `ANTHROPIC_API_KEY`、`ANTHROPIC_AUTH_TOKEN`、`CLAUDE_CODE_OAUTH_TOKEN`、`ANTHROPIC_PROFILE`、`CLAUDE_CODE_USE_*` 等環境變數，`CLIEnv` 已不傳給 host 啟動的 CLI（10/4 依 C0 指示校正：原文誤寫為 allowlist 擋不到）。它們在文件中的優先順序未經驗證，不寫成事實 |
| 2 `apiKeySource` 接受值 | 清單為空；文件無法確認值域；從未見過真實 init 的值（2.1.287、2.1.288 皆未見） | **真實 init 的值**，加上第 1 項的證據，兩者都有才可能支持一個接受值。單獨任何一項都不夠 |
| 3 `--model sonnet`、`--effort low` | 只是候選；本機 help 有這兩個旗標 | 真實 init 回報的 model ID；CLI 是否接受 `--effort low`（不接受時的行為未知） |
| 4 init 中的 agents／skills | 一律拒絕；真實 init 會不會列出內建項目未知 | **真實 init 的內容** |
| 5 階段 C 單次 run | 未核准 | 1–4 完成後另行核准 |
| （附）init 欄位形狀 | 只依 SDK 文件（`mcp_servers.status`、`claude_code_version` 格式、`tools` 內容） | **真實 init 的內容** |

結論：2、3、4 都缺真實 init 的資訊。現在照第 7.2 節跑階段 C，必定在 `auth_unconfirmed` 停下（設計如此），而 trace 依規定不保存 init 的值，所以那次 run 會花掉用量卻得不到需要的資訊。因此先提下面的 C0。

### 15.2 提案 C0：真實 init 探測

目標：取得第 2–4 項需要的真實 init 事實。全程不執行任何工具、不開 Store、不送任何使用者訊息。

**做法（需先實作並經 Codex review，再經 Aaron 核准才執行一次）**：

- 新子命令 `apa-buyer probe-init --cli <絕對路徑>`：需要 `APA_LIVE_CLI=true`，`--version` 須恰為核准版本 `2.1.288 (Claude Code)`。
- 環境 allowlist、固定環境變數、私有空工作目錄、MCP 設定與 bridge 都與階段 C 相同，讓 `mcp_servers` 的狀態是真的。run 註冊後保持 idle、永不放行；不開 Store，也不使用任何 DB 路徑。
- argv 與階段 C 相同，只差兩點：`-p` 不帶 prompt，並加上 `--input-format stream-json`。stdin 保持開啟，**從頭到尾不寫入任何使用者訊息**。
- 最多等 30 秒。收到 `system/init`，或逾時，就關閉 stdin，並用既有的 runner 清理整個 process group。
- 輸出到終端與 run trace，只含白名單化的事實，原始 init 不保存：
  - 各欄位是否存在、型別是否正確。
  - `tools` 是否恰為三個工具，多出幾個、少了幾個（不列名稱）。
  - `mcp_servers`：數量；`apa` 的 status 在已知值集合內才顯示，否則記 `other`。
  - `permissionMode`：在 SDK 列舉內才顯示。
  - `claude_code_version`：等於核准版號才顯示。
  - `model`：在 `sonnet`、`claude-sonnet-5-5` 內才顯示，否則 `other`。
  - `apiKeySource`：只在一份候選詞彙內才顯示，否則記 `other` 與字數。候選詞彙取自 SDK 頁的第一次摘要，**明確標示為未確認**；它只決定能不能顯示，不代表被接受。
  - plugins／skills／agents：數量，以及 agents 的 `source` 類別（在已知類別內才顯示）。

**可能用量**：

- **設計上不應有任何模型推論**：沒有使用者訊息，模型就沒有可以回應的回合。
- **未驗證**：核准版本 2.1.288 在 stream-json 輸入模式下，會不會在收到第一則使用者訊息之前就送出 init。若不會，探測在 30 秒時以「init 未在輸入前出現」結束；仍然沒有送出訊息，預期沒有推論，但這次就拿不到 init。
- **無法完全排除**：CLI 自己的帳號、認證或背景連線。`CLAUDE_CODE_DISABLE_NONESSENTIAL_TRAFFIC=1` 只是減少，不是保證。沒有 result 訊息，所以 usage 只能記 `unknown`，不會補零。

**C0 的備案 C0′（只在 C0 拿不到 init 時，另行核准）**：寫入一則最短的使用者訊息，一收到 init 就終止。**只送一則使用者訊息；實際模型請求次數與用量沒有硬上限保證**。預期每次請求的輸入是 CLI 的系統提示、三個工具定義與那一句話，一收到 init 就中斷；用量記 `unknown`。

**停止條件**：opt-in 或版本不符、逾時、任何錯誤，都立即停止，不重試，也不改走 C0′；C0′ 必須另外核准。

**實作與測試範圍**：fake 需支援兩種行為（不等輸入就先送 init；等到輸入才送 init），各補測試；確認探測模式不開 Store、不放行工具、不送使用者訊息（以 fake 記錄 stdin 是否收到任何位元組）；輸出白名單的識別標記測試；加上對應突變。

### 15.3 C0 之後怎麼決定 `apiKeySource` 接受值

- 必須兩者都有：第 1 項的階段 B 證據（訂閱登入方式，且沒有排在它前面的其他憑證來源），加上 C0 看到的值。兩者一致，才由 Aaron 決定是否把**那一個值**列入清單，並記錄依據。不會在任何一次失敗後自動放寬。
- 即使列入，init 仍不能證明計費。最強的事後證據是：階段 C 結束後，由 Aaron 確認該時段在 Console／API 帳單上沒有對應用量（若有 Console 帳號）。這一步列為選做，由 Aaron 決定。
- 第 3、4 項（model／effort、agents／skills）也依 C0 的事實另行決定。

### 15.4 階段 C 本身的可能用量（供日後核准參考）

主流程需要 `list_products`、`get_quote`、`request_purchase`，通常是三到四次模型請求：每次工具結果回來，就再發一次請求。每次請求都帶 CLI 的系統提示、工具定義與累積的對話。上限是 10 則 assistant 訊息與 3 分鐘。確切 token 數未知；result 若有 usage 就原樣記錄，標為「CLI 回報」。

### 15.5 待 Aaron 決定

1. 是否核准**實作** C0（程式、fake 測試、Codex review；不執行）。
2. 實作通過 review 後，是否核准**執行一次** C0。
3. C0 拿不到 init 時，是否接受 C0′（只送一則使用者訊息；實際模型請求次數與用量沒有硬上限保證），或改用其他方式。
4. 階段 B 是否照 15.1 第 1 項的方式回報（只回報登入方式／方案類型一行與各項有／無）。

### 15.6 CLI 版本變動（2026-10-03）

- Aaron 回報本機 CLI 為 `2.1.288 (Claude Code)`；`claude --version` 本機重查結果相同。10/2 記錄並寫進程式的核准版本是 `2.1.287 (Claude Code)`（`cmd/apa-buyer` 的 `approvedCLIVersion`，閘門由它推得 `claude_code_version` `2.1.287`）。所以**目前的 live 與擬議的 C0 都會在版本檢查停下**，不執行 CLI 的推論，也不開 Store。這是預期的行為，不自動升級或降級。
- 本機 `--help` 重查（只讀說明文字）：argv 與 C0 會用到的旗標都還在，選項與說明文字都與 2.1.287 時的紀錄相同，包括 `-p`、`--output-format`、`--verbose`、`--mcp-config`、`--strict-mcp-config`、`--restricted`（全文相同）、`--tools ""`、`--permission-mode`（含 `dontAsk`）、`--permission-prompts`（含 `none`）、`--allowedTools`、`--model`、`--effort`（含 `low`）、`--no-session-persistence`、`--disable-slash-commands`、`--input-format`（含 `stream-json`）；`claude auth status` 仍有 `--json`／`--text`。`--max-turns` 仍不在本機 help。這只比對了說明文字，**不證明行為沒變**。
- 版本在 10/2 到 10/3 之間變動，多半是 Aaron 平時互動使用時的自動更新。host 傳給 CLI 的 `DISABLE_AUTOUPDATER=1` 只作用於 host 啟動的那次 run，不影響平時的使用；在階段 B 確認之後、C0 或 C 執行之前，版本可能再變。版本檢查會讓 run 停下（fail closed），但每次變動都需要重新核准。
- 待 Aaron 決定：
  1. 是否把核准版本改為 `2.1.288 (Claude Code)`。這要改 `approvedCLIVersion` 並更新文件，屬程式變更，需經 Codex review。
  2. 或維持 `2.1.287`，由 Aaron 自行還原 CLI 版本（repo 不做這件事）。
  3. 在 C0 或 C 期間，是否由 Aaron 自行暫停平時 CLI 的自動更新，以免版本在核准與執行之間再變。這是 Aaron 環境的設定，repo 不更動。
- 這只涵蓋第 15.1 節第 1 項的「版本」部分；登入方式／方案類型與各憑證來源的有／無尚未回報。
- **10/3 決定（Aaron）**：核准版本改為 `2.1.288 (Claude Code)`。程式 `cmd/apa-buyer` 的 `approvedCLIVersion` 已改；閘門由它推得 `claude_code_version` `2.1.288`。測試改以該常數為準，另以舊版 `2.1.287` 驗證「其他版本被拒」，並直接確認 `2.1.288` 通過版本檢查（`TestLiveOptIn`）。第 6 節與本節前段保留 2.1.287 時的歷史紀錄。版本選擇已決定（上面第 1、2 項）；執行期間是否暫停平時 CLI 的自動更新（第 3 項）仍待 Aaron 決定。這次只改版本釘選，沒有執行 CLI 推論、登入檢查或 C0；live 認證清單維持空白。

## 16 驗收紀錄：核准版本改為 2.1.288（`14ceccb`）通過技術 review（2026-10-03）

- 結果：`14ceccb` 的版本釘選變更通過 Codex 獨立技術 review，未發現阻擋問題。**這不代表真實 CLI 2.1.288 的完整相容性已驗證**：只比對了本機 help 文字，真實 init、認證來源與行為都未驗證。
- 證據歸屬：
  - **Codex 獨立重跑**：`cmd/apa-buyer` 全套 race／coverage（覆蓋率 81.1%）；buyer 的版本、init、trace、JSON 歧義與環境參數相關測試三輪；Codex 自製 fake 探測三輪（新版通過；舊版拒收；新版 `--version` 搭配舊版 init 仍拒收；正式認證清單為空時不開 Store）。
  - **Claude Code 執行、Codex 核對紀錄**（不是 Codex 獨立重跑）：完整容器驗證七步、164／164 突變、17 項隔離檢查，見 `artifacts/verify-container/runs/14ceccbd7fe9-20261003T145344Z/`；本機檢查見 `artifacts/b1-04/14ceccbd7fe9-20261003T145020Z/`。
- review 指出的兩處文件修正已處理：§15.6 不再同時寫「不再適用」與「仍待決」；C0′ 的用量（§15.2、§15.5）改為「只送一則使用者訊息；實際模型請求次數與用量沒有硬上限保證」。
- B1-04 整體仍 InProgress，未合併 main。C0 的實作與執行仍需 Aaron 另行核准；本輪未執行登入檢查或推論，live 認證清單維持空白。

## 17 C0 init 探測：實作（2026-10-04，Claude Code 執行，待 Codex 獨立 review）

依 Aaron 10/3 核准的 C0 實作指示（vault：`Claude-Code-B1-04-C0實作指示-24393fa-20261003`），起點 `24393fa`。**本輪只有程式、fake 測試與文件**；沒有執行真實 CLI、`claude --version`、`claude auth status`、`claude -p`、C0、C0′ 或任何推論。真實執行一次 C0 需 Aaron 另行核准。

### 17.1 命令

```sh
APA_LIVE_CLI=true ./bin/apa-buyer probe-init --cli /Users/mac/.local/bin/claude   # 本輪未執行
```

| 結果 | `status` | exit code |
|---|---|---|
| 觀測到可嚴格讀取、且 `claude_code_version` 等於核准版號的 init | `observed`（`code` `init_observed`） | 0 |
| 等待上限內沒有 init（例如 CLI 等使用者訊息才送） | `no_init_before_input` | 3 |
| 其他：init 版號不符（`init_cli_version`）、JSON 歧義（`ambiguous_json`）、非 JSON（`parse_error`）、init 前出現 hook（`hook_message`）或 assistant／user／result 等訊息（`unexpected_message`）、輸出超量（`output_overflow`）、程序異常退出（`nonzero_exit`、`missing_result`）、取消（`cancelled`）等 | `failed` | 1 |
| 啟動前拒絕：缺 `APA_LIVE_CLI=true`、`--cli` 非絕對路徑、版本不符、版本檢查逾時或取消 | 不執行探測，印錯誤 | 1 |

`observed` 只代表拿到了觀測資料，**不代表**通過正式 Gate、認證已確認或可以付款；`no_init_before_input` 與 `failed` 都不是成功。

### 17.2 執行界線與實作位置

| 界線 | 實作 | 測試 | 突變 |
|---|---|---|---|
| 明確 opt-in：`APA_LIVE_CLI=true`、絕對路徑、`--version` 恰為核准值；沿用版本檢查的程序清理與輸出上限；取消後不啟動 | `cmd/apa-buyer/main.go` `probe`、`checkLive`；`internal/buyer/probe.go` 開頭的取消檢查 | `TestProbeOptIn`（各種缺漏時 CLI 未執行、未寫 trace；舊版；版本檢查逾時）、`TestProbeCancel`（已取消則 CLI 不執行） | #174、#175 |
| 零輸入：`-p` 不帶 prompt、`--input-format stream-json`；stdin 是 host 保持開啟、從不寫入的 pipe（不送換行、空白或初始化訊息，逾時後也不補送） | `ProbeArgv`；`runner.go` 的 `Spec.HoldStdin` | `TestProbeArgv`；`TestProbeInitFirst`／`TestProbeWaitsForInput`（fake 自己計數，收到 0 位元組）；`TestFakeCountsStdin`（正向對照：給 3 位元組，計數為 3，且等輸入的 fake 隨即送出 init） | #165、#166 |
| 零業務執行：MCP 初始化與列工具照常；run 註冊後一直 idle，不 arm；觀測到 init 也不放行 | `Probe`（`toolbridge.Binding` 從不 `Arm`；bridge 的 executor 為 `refuseAll`，並計數） | `TestProbeInitFirst`：fake 在 init 前後都直接經 MCP 呼叫業務工具（`silent_tool`，不經模型），全被拒，`business_calls_executed` 為 0 | #167 |
| 零 Store 存取：不載入、不開啟 Store，不建 DB／task，不做 migration、到期或結束事件 | `probe` 不碰 `APA_DB_PATH`、不呼叫 Store | `TestProbeCommand`：Store 開啟 hook 從未被呼叫（同一 hook 在一般 run 會被呼叫，作正向對照）；含到期 Ask 的既有 DB 在兩次 probe 後逐位元組不變 | #176 |
| 有限執行：最長等 init 30 秒；收到 init、錯誤或取消即停止；沿用既有輸出上限與 process group 清理 | `ProbeTimeout`；`Probe` 的 watch；runner | `TestProbeWaitsForInput`、`TestProbeFailures`（輸出超量、stderr 洪水、異常退出、握 pipe 的子孫被回收） | #168、#172 |
| 嚴格解析：沿用 `objectFields`（巢狀重複欄位、大小寫別名一律拒收）；init 前的 hook、assistant／user／result 訊息以固定代碼停止 | runner 的讀取；`Probe` 的 watch | `TestProbeFailures`（重複 init 欄位、巢狀歧義、非 JSON、hook、assistant、result） | #169、#170 |
| 不自動重試：任何結果都不放寬、不重試、不改走 C0′；結束時撤銷 capability、等待進行中的呼叫 | `Probe` 只執行一次；`bridge.Revoke` | 程式中沒有重試路徑；`TestProbeFailures`、`TestProbeWaitsForInput` 各只啟動 CLI 一次 | — |
| init 版號須等於核准值；舊版 init 拒收（`--version` 為新版亦同） | `Probe` 的分類 | `TestProbeFailures`「old init version」：`init_cli_version`，觀測資料中版本記為 `other` | #171 |

附帶校正：共用讀取函式原本把非 JSON 也報為 `ambiguous_json`。現在只把「合法 JSON 但成員名重複」報為 `ambiguous_json`，非 JSON 報為 `parse_error`。拒收行為不變，只是代碼更準確。

### 17.3 輸出白名單

終端、probe trace（`var/runs/probe-<時間>.jsonl`）與錯誤訊息都使用同一個 `ProbeResult` 投影。原始 init、stdout／stderr 文字、prompt、環境變數值與 capability 都不保存。

- `fields`：已知 init 成員（依 SDK 參考）各記「absent」或 JSON 型別；其他成員只計數（`extra_members`）。
- `tools_count`、`tools_expected`（三個 apa 工具出現幾個）、`tools_other`；`mcp_servers_count`；`apa_server_status`（SDK 狀態 `connected`／`disconnected`／`error`，否則 `other`）。
- `permission_mode`（SDK 列舉，否則 `other`）；`cli_version`（`approved`／`other`）；`model`（`sonnet`、`claude-sonnet-5-5`，否則 `other`）。
- `auth_source`：只在顯示候選清單 `none`、`environment`、`keychain`、`claude_app`、`claude_app_org` 之內才顯示，否則記 `other` 與字數。**這份候選清單的來源是 10/3 對 SDK 參考頁的第一次摘要；要求逐字複製時找不到該型別，所以每個值都未確認**。它只決定能不能顯示，與正式的 `liveAcceptAuth` 分開，後者仍是空的；執行時不會把任何值加入清單。
- `plugins_count`、`skills_count`、`agents_count`：只記數量。沒有可靠的類別清單，所以不顯示類別。
- 版本檢查的錯誤訊息也不再重複 CLI 輸出的版本文字（live 與 probe 共用）。

測試：`TestProbeNoLeak`（把該次 fake 的 capability 與合成標記放進 model、auth source、工具名、server 名稱與狀態、權限模式、agents／skills／plugins、cwd、session_id、額外成員名稱與值），以及 `TestProbeCommand`（終端輸出、錯誤與 trace），兩者都確認沒有洩漏；`TestProbeUnknownValues` 確認未知列舉與錯誤型別都投影成固定標籤。突變 #173、#177。

### 17.4 fake 與限制

- fake 的兩種行為：`probe-init-first`（不等輸入就送 init）與 `probe-wait-input`（等到使用者訊息才送）。兩者都是測試資料，**不是**從真實 CLI 擷取的 fixture。新增的 fake 步驟：`wait_input`、`silent_tool`；stdin 位元組計數檔 `APA_FAKE_CLI_STDINFILE`；live／probe 環境下，capability 存到 `<自身路徑>.cap`。
- 「沒有使用者訊息」是程式能保證的界線，**不等於**保證 CLI 內部沒有背景連線或推論。沒有 result 訊息，用量記 `unknown`，不補零。不宣稱用了幾次模型請求，也不宣稱確認了訂閱計費。
- 真實 CLI 2.1.288 在 `--input-format stream-json` 下，會不會在收到輸入前送出 init，**未驗證**；這正是 C0 要觀測的事。
- 已知限制照舊：離開 process group 的子孫程序收不回來；CLI 以使用者權限執行、使用 HOME 與網路；30 秒是等待上限，停止後的清理寬限另計（最多約兩個寬限期）。

### 17.5 執行一次 C0 前還需要

1. Codex 通過本實作的 review。
2. Aaron 核准**執行一次** C0，並再次確認：真實 CLI 的路徑與版本、HOME 與網路的界線、可能用量（設計上沒有使用者訊息，但無法保證 CLI 內部沒有連線或推論）、停止條件。
3. 階段 B 的其餘回報（第 15.1 節第 1 項）不阻擋 C0 實作，但仍未完成。
4. 執行期間是否暫停平時 CLI 的自動更新；版本一變就停止，需重新核准。
5. C0 沒拿到 init 時怎麼處理另行決定：C0′ 與階段 C 都未核准，C0 失敗不會自動升級到它們。
