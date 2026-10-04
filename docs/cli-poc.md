# B1-04 Buyer wrapper：CLI 橋接 PoC

> **狀態（2026-10-04）**：**C0′ 的實作及 fake 驗證於 `05e03e2` 通過 Codex 獨立技術 review（第 25 節）；真實 C0′ 的執行確認清單見第 26 節。10/4 經 Aaron 核准執行一次，在版本檢查即停止（CLI 回報的版本不是 2.1.288），probe 未啟動、訊息未送出（第 27 節）。核准版本已依 Aaron 指示改為 `2.1.289 (Claude Code)`（`1f0b260`，第 28 節），已通過 Codex 限定範圍 review（第 29 節）。10/4 經 Aaron 核准再執行一次真實 C0′：exit 0，取得真實 init，Codex 已核對證據（第 30、31 節）。正式 live 仍不能放行：plugins 2、agents 5 會先被 Gate 拒絕，`apiKeySource` = `none` 也不在接受清單；plugins／agents 的釐清方案待 Aaron 核准（第 31 節）。10/4 依核准完成 A 官方文件查證與 B 身分觀測（程式 `4e709ab`、`8da15dd`），review 指出 `null` 判斷問題，已修正於 `b46e332`；10/4 依 Aaron 指示同步本輪驗收（第 34 節）。未執行真實 CLI；真實 C0′、受管設定讀取與清單調整仍須 Aaron 另行核准（第 32–34 節）。** 以下為此前狀態：fake 首個交付點於 `b3c66f6` 通過 Codex 獨立技術 review（第 10 節）。**live 階段 A（live 開關、init 閘門、fake 驗證）於 `e8f6719` 通過 Codex 獨立技術 review，R1、R2 關閉**（第 14 節；修正歷程見第 11–13 節）。**C0 init 探測的實作與 fake 驗證於 `5f34a7b` 通過 Codex 獨立技術 review（第 19 節）；10/4 經 Aaron 核准對真實 CLI 執行一次 C0，30 秒內逾時，未取得 init（第 21 節）；該次證據已由 Codex 核對**：未送任何使用者訊息，CLI 的背景行為與用量未知。**C0′（`--send-one-message`，只送一則固定訊息）10/4 已實作並以 fake 測試；`cf97c87` 與 `38eb2ba` review 的寫入失敗處理已修正（第 23、24 節），於 `05e03e2` 通過技術 review，未對真實 CLI 執行**（第 22、25 節）。階段 B、C **未執行**，沒有執行登入檢查或帶 prompt 的 `claude -p`；live 認證接受清單維持空白。B1-04 整體仍 InProgress，未標 Done、未合併 main。這次通過不代表真實 Claude 的登入、模型或計費方式已驗證；fake 成功只證明 wrapper／bridge／Store 的行為，B1-04 的 live trace 驗收項仍保留。

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
- `--version` 檢查（fake 命令確認 `--fake-cli` 確實是 fake；live 要求輸出恰為核准版本 `2.1.289 (Claude Code)`；10/3 前為 2.1.287，10/3–10/4 為 2.1.288，第 28 節）也走同一套程序規則：獨立 process group、輸出 4 KiB、stderr 4 KiB、5 秒，結束時回收整個 group。逾時、取消、超量或非零退出都視為失敗，不啟動 task；啟動 task 前再確認一次未被取消。
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
- `claude_code_version`：等於核准版本的版號 `2.1.289`（10/4 前為 `2.1.288`）；fake 為 `0.0.0`（`cli_version`）。
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
| 其他：init 版號不符（`init_cli_version`）、JSON 歧義（`ambiguous_json`）、非 JSON（`parse_error`）、init 前出現 hook（`hook_message`）、`system/api_retry`（`api_retry_auth`、`api_retry_rate_limit`、其他為 `api_retry`；只記代碼，不保存錯誤文字）或 assistant／user／result 等訊息（`unexpected_message`）、輸出超量（`output_overflow`）、程序異常退出（`nonzero_exit`、`missing_result`）、取消（`cancelled`）等 | `failed` | 1 |
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

## 18 `71f222a` review 修正：api_retry 立即停止、啟動前檢查取消（2026-10-04，Claude Code 執行，待 Codex 獨立 review）

| 項目 | review 發現 | 修正 | 測試 | 突變 |
|---|---|---|---|---|
| R（必修） | probe 把 `system/api_retry` 當成一般通知繼續等待：之後若來 init，會被記為 `observed`；若沒有 init，則要等到逾時才回「沒有 init」 | `probe.go`：`api_retry` 立即停止 run，依錯誤類別記固定代碼 `api_retry_auth`（`authentication_failed`）、`api_retry_rate_limit`（`rate_limit`）、其他 `api_retry`；錯誤文字不保存 | `TestProbeAPIRetry`：認證失敗或限流，各接「之後送 init」與「之後不送」四種情境，加上未知錯誤文字（含標記）一案。全部 `failed`、代碼正確、沒有觀測資料、遠早於等待上限結束、stdin 0 位元組、沒有執行工具，結果與 trace 都不含錯誤文字。正向對照照舊：`TestProbeInitFirst`（正常 init 成功）、`TestProbeWaitsForInput`（純等待輸入逾時） | #178–180 |
| C1（建議） | 取消只在版本檢查之後才檢查；底層直接呼叫 `cmd.Start()` | `cmd/apa-buyer`：live 與 probe 都在版本檢查前先檢查 context；`runner.go`：啟動程序前檢查 context，已取消或逾時就不啟動（`Version` 與 `Run` 共用）；並加一個只供測試替換的啟動計數掛鉤 | `TestRunnerCancelledBeforeStart`（正向對照：計數看得到一次啟動；已取消時 `Run` 與 `Version` 都是 0 次）；`TestProbeOptIn`（已取消的 probe 連版本檢查都不執行，CLI 未被呼叫） | #181、#182 |

Codex 對 C1 的說明：這是程式碼觀察，三輪共 300 次測試沒有觀察到 fake 寫入副作用，不列為已動態重現的問題。共 182 項突變，原 99 項文字未動。

## 19 驗收紀錄：C0 實作與 fake 驗證於 `5f34a7b` 通過技術 review（2026-10-04）

- 結果：`5f34a7b` 通過 Codex 獨立技術 review。R1（api_retry）關閉，C1（提前檢查取消）完成，沒有新的阻擋問題。Codex 確認了 review 轉交截斷時，本輪對四種回歸情境的解讀是正確的。
- **通過的是 C0 的實作與 fake 驗證**。不代表真實 CLI 的行為、登入方式或用量已驗證。B1-04 整體仍 InProgress，未標 Done、未合併 main；live 認證接受清單維持空白。真實 C0、C0′ 與階段 B／C 都未核准、未執行。
- 證據歸屬：
  - **Codex 獨立重跑**：R1 原先失敗的四種情境各三輪（全部立即回報失敗；不被後續 init 覆蓋、不等到逾時；stdin 與業務工具執行均為零）；C1（已取消、已逾時的 context 都不啟動程序，正向對照確認啟動計數有效）；全套 race／coverage；C0 相關回歸三輪；JS 契約向量；20 項證據匯入測試。全部通過。
  - **Claude Code 執行、Codex 核對紀錄**（不是 Codex 獨立重跑）：182／182 突變、完整容器驗證七步、17 項隔離檢查，見 `artifacts/verify-container/runs/5f34a7b65949-20261003T175144Z/`；本機檢查見 `artifacts/b1-04/5f34a7b65949-20261003T174835Z/`。
- 歷程：`71f222a`（首次交付）→ `5f34a7b`（api_retry 修正與取消檢查；通過）。

## 20 執行一次真實 C0 的確認清單（2026-10-04 擬定；待 Aaron 核准，未執行）

本節只是清單。**沒有 Aaron 對「執行一次 C0」的明確核准，就不執行**；執行後不論結果，都不會自動轉往 C0′ 或階段 C。

### 20.1 執行前，由 Aaron 確認（不貼敏感內容）

1. **核准**：明確同意執行**一次** C0。結果不理想不會重跑；要重跑必須再次核准。
2. **CLI**：`claude --version` 恰為 `2.1.288 (Claude Code)`，路徑是 `/Users/mac/.local/bin/claude`（10/2 記錄的位置；若已不同，要回報新路徑）。版本若已變，就停止並重新核准釘選。
3. **自動更新**：決定執行期間是否暫停平時 CLI 的自動更新（Aaron 自己的環境設定，repo 不更動）。host 傳給 CLI 的 `DISABLE_AUTOUPDATER=1` 只作用於這一次啟動。
4. **登入與憑證來源**（階段 B，第 15.1 節第 1 項）：`claude auth status --text` 中「登入方式／方案」那一行；以及只答「有／無／不確定」：managed settings 檔、settings 檔的 `apiKeyHelper`、keychain 裡先前 Console 登入的 API key、`~/.config/anthropic` 的 active profile。這不阻擋 C0 執行，但 C0 的結果要搭配這些資訊才能解讀；**兩者都不能單獨證明計費方式**。
5. **接受的風險**：
   - CLI 以 Aaron 本人的權限、HOME 與網路執行，會使用 `~/.claude` 與 keychain；host 不讀取、不複製這些內容。
   - 程式保證不送任何使用者訊息，但**不保證** CLI 內部沒有背景連線、帳號查詢或推論。
   - 用量記為 `unknown`，可能不是零。
   - 不是作業系統沙箱；離開 process group 的子程序收不回來。
6. **停止與 C0 拿不到 init 時的處理**：若結果是 `no_init_before_input`（等待時間內沒有收到 init；原因可能是 CLI 要等使用者訊息才送 init，但單憑這個結果無法確定），不轉往 C0′；是否接受 C0′ 或其他做法，另行決定。

### 20.2 命令（由 Claude Code 在 Aaron 核准後執行，或由 Aaron 自己執行）

```sh
cd /Users/mac/Documents/OpenSourceProjects/agent-payment-authority
git status --short && git rev-parse HEAD        # 工作區乾淨，HEAD 為通過 review 的 5f34a7b（或之後只改文件的 commit）
go build -o bin/apa-buyer ./cmd/apa-buyer
APA_LIVE_CLI=true ./bin/apa-buyer probe-init --cli /Users/mac/.local/bin/claude --trace-dir var/runs
echo "exit=$?"
```

- 只跑這一條。啟動前，命令會再做一次 opt-in、絕對路徑與版本檢查，任何一項不符就不啟動。
- 最多等 init 30 秒；停止後的清理另需最多約 6 秒（兩個寬限期）。
- 不開 Store，不使用 `APA_DB_PATH`，不建立 task，不連商店，不執行任何業務工具。

### 20.3 結果怎麼讀

| exit | status | 意思 | 之後 |
|---|---|---|---|
| 0 | `observed` | 拿到一則可嚴格讀取、版本相符的 init；觀測資料是白名單投影 | 把觀測資料（第 17.3 節的欄位）和 20.1 第 4 項一起交 Codex review，再決定 `apiKeySource` 接受值、model／effort、agents／skills 怎麼處理。**不會自動放寬任何清單** |
| 3 | `no_init_before_input` | 30 秒內沒有 init，CLI 可能要等使用者訊息才送 | 停止。C0′（送一則訊息）或其他做法另行決定 |
| 1 | `failed` 與代碼 | `api_retry_auth`（認證失敗）、`api_retry_rate_limit`（限流）、`init_cli_version`、`ambiguous_json`、`hook_message`、`unexpected_message`、程序失敗等 | 停止，回報代碼，不重試 |
| 1（無 JSON） | — | 啟動前就被拒絕（opt-in、路徑、版本、取消） | 修正前提後再次核准 |

### 20.4 保存與交付

- 終端輸出與 `var/runs/probe-<時間>.jsonl` 都只有白名單投影；不保存原始 init、CLI 文字、prompt、環境變數值或 capability。
- 交付給 review 的是：終端 JSON、trace 檔、exit code、執行時的 commit。整份標為「Claude Code（或 Aaron）執行，待 Codex review」。
- 執行後即停止，等 review 與 Aaron 的下一步決定。

## 21 真實 C0 執行紀錄（2026-10-04，Claude Code 執行，待 Codex review）

依 Aaron 10/4 的核准，**只執行一次**；核准範圍不含 C0′、階段 C 或其他模型任務。

- 前置：HEAD `fd7b338cfe4acfd8a88e5b89df00900fdd83bf32`，工作區乾淨。`bin/apa-buyer` 是當場由該 commit 建置，`go version -m` 顯示 `vcs.revision=fd7b338…`、`vcs.modified=false`。
- 命令：`APA_LIVE_CLI=true ./bin/apa-buyer probe-init --cli /Users/mac/.local/bin/claude --trace-dir <證據目錄>/runs`，只跑一次。
- 版本：命令內建的版本檢查通過；若版本不是 `2.1.288 (Claude Code)`，命令會在啟動前就拒絕並輸出錯誤，而這次確實進入了探測。版本字串本身沒有另外保存。
- 時間：2026-10-03T18:45:31Z 至 18:46:02Z（UTC），約 31 秒。
- 結果：**exit 3，`no_init_before_input`**。runner 結果是 `timeout`：30 秒內 CLI 沒有送出 `system/init`，被 SIGTERM 結束（CLI exit 143）。`stdin_bytes_written` 0，業務呼叫拒絕 0、執行 0，`usage` 為 `unknown`，沒有觀測資料。
- 事後檢查：沒有殘留的 CLI 或 bridge 程序；探測用的暫存目錄已移除。Store 未開啟，商店與 task 都沒有啟動。
- 保存：`artifacts/b1-04/c0-real-20261003T184531Z/`（本機，`artifacts/` 不進版控）。內容只有 `commit.txt`、`binary-vcs.txt`、開始與結束時間、`exit-code.txt`、`stdout.json`（白名單 JSON）、`stderr.txt`（apa-buyer 自己的固定代碼行）與 `runs/` 的 trace。沒有任何 CLI 文字。
- 解讀（待 Codex review）：
  - 在零使用者輸入、`--input-format stream-json` 下，CLI 2.1.288 在 30 秒內沒有送出 init，符合第 15.2 節預想的情況之一：init 可能要等到第一則使用者訊息才送出。
  - 這次**沒有**取得 `apiKeySource`、model、agents／skills 等任何 init 資訊，所以第 7.6 節第 2–4 項仍無資料可據。
  - 程式能保證沒有寫入任何 stdin 位元組；但無法保證 CLI 內部沒有背景連線或推論，用量仍記 `unknown`。
- 依核准條件，執行後即停止：不重跑、不放寬任何清單、不轉往 C0′。下一步（是否、以及如何取得 init 資訊）待 Codex review 與 Aaron 決定。

## 22 C0′ 單訊息探測：實作（2026-10-04，Claude Code 執行，待 Codex 獨立 review；未對真實 CLI 執行）

依 Aaron 核准的 C0′ 實作指示（vault：`Claude-Code-B1-04-C0prime實作指示-8ff5705-20261004`），起點 `8ff5705`。本輪只有程式、fake 測試與文件；沒有執行真實 CLI（包含 `--version`、登入檢查、C0 重跑或 C0′）與任何推論。

### 22.1 命令與結果

```sh
APA_LIVE_CLI=true ./bin/apa-buyer probe-init --cli /Users/mac/.local/bin/claude --send-one-message   # 本輪未執行
```

- `--send-one-message` 是進入 C0′ 的唯一方式。沒有這個旗標就是原本的 C0，從頭到尾寫入 0 位元組。C0 逾時或失敗不會自動改走 C0′。opt-in、絕對路徑、版本與取消的檢查都和 C0 相同。
- 輸出新增 `mode`（`c0`／`c0_prime`）、`input_status`、`input_message_bytes`（預定大小，C0 為 0）、`stdin_bytes_written`。最後一項是 host **實際**寫入的位元組數，不再是固定的 0，也不是預定大小。

| 結果 | `status`／`code` | exit code |
|---|---|---|
| 取得可嚴格讀取且版本相符的 init | `observed`／`init_observed` | 0 |
| C0：等待上限內沒有 init | `no_init_before_input` | 3（不變） |
| C0′：訊息已完整寫入，等待上限內仍沒有 init | `no_init_after_message` | 4（新增） |
| C0′：寫入失敗或只寫了一部分（立即停止，不等逾時；之後讀到的 init 不算數） | `failed`／`input_failed`、`input_partial` | 1 |
| C0′：逾時，且訊息沒有完整寫入 | `failed`／`input_not_started`、`input_partial`、`input_failed` | 1 |
| 其他失敗（api_retry、JSON 歧義、hook、模型回覆等非預期訊息、版本、程序、取消） | `failed`，代碼同 C0 | 1 |

`input_status` 的值：`none`（C0，不寫）、`not_started`（run 已在停止，或啟動前已取消或啟動失敗，所以沒有開始寫）、`complete`（全部寫入）、`partial`（寫了一部分就失敗）、`failed`（一個位元組都沒寫進去，例如 pipe 已斷或逾時）。

### 22.2 訊息與格式依據

- 固定文字 `Reply only OK.`（程式常數 `OneMessageText`）。不能透過參數、檔案或 stdin 轉送指定別的內容；不含購物任務、商品、使用者內容或憑證。
- 一行 stream-json：`{"type":"user","message":{"role":"user","content":"Reply only OK."},"parent_tool_use_id":null}` 加換行，共 95 位元組。
- 依據：Agent SDK TypeScript 參考的 `SDKUserMessage` 型別（10/4 查閱）。必填欄位是 `type: "user"`、`message`（Anthropic SDK 的 `MessageParam`，即 role 與 content）、`parent_tool_use_id: string | null`；其餘欄位都是選填。SDK 串流輸入頁的範例也是同一形狀。CLI 參考列有 `--input-format stream-json`，但只說是 realtime streaming input。
- **缺口**：沒有任何一手文件逐字寫出 CLI stdin 的線上格式（每行一個 JSON 物件）。這是從 `--output-format stream-json` 為換行分隔 JSON 所做的推論；真實 CLI 從未收過這一行。fake 能接受，不代表真實 CLI 能接受。依指示不以真實 CLI 試誤。

### 22.3 寫入界線（實作與測試）

| 界線 | 實作 | 測試 | 突變 |
|---|---|---|---|
| C0 預設不送輸入；C0′ 需額外 opt-in | `ProbeSpec.SendOneMessage` 只由 `--send-one-message` 設定 | `TestProbeInitFirst`／`TestProbeWaitsForInput`（C0：mode `c0`、`input_status none`、host 與 fake 都是 0 位元組）；`TestProbeOneMessageCommand`（缺 opt-in 或相對路徑時 CLI 未被執行；不帶旗標時同一個 fake 得到 C0 的 exit 3） | #165、#186、#190 |
| 只寫一次、固定內容 | runner 的 `stdinWriter.write` 只呼叫一次 | `TestOneMessageLine`；`TestProbeOneMessage`（fake 收到恰好一行、恰好一則使用者訊息、內容雜湊相符，host 寫入數等於 fake 接收數） | #185、#189 |
| 停止後不開始寫；如實記錄 | writer 持鎖寫入；run 一開始停止就 `stop()`；寫入期限為清理寬限（3 秒）；寫入後即關閉 parent 的讀端，子程序關閉 stdin 時會得到 broken pipe，不會塞滿 buffer | `TestStdinWriter`（先停止則 `not_started`、pipe 中 0 位元組；正常為 `complete`；讀端已關閉則 `failed`）；`TestProbeOneMessageInterleaving`（init 先到、取消與寫入交錯、不讀 stdin、關閉 stdin、提前退出：記錄與實際一致，fake 最多收到一行，都在上限內結束） | #183、#184、#188 |
| 不等待模型回答、不開第二輪 | watch 收到 init 或任何 assistant／result 訊息即停止 | `TestProbeOneMessageStops`「model replies」「result before init」 | （沿用 #170） |
| 停止條件不退化 | 同 C0 | `TestProbeOneMessageStops`：api_retry（認證、限流，各接後續 init 或不接，加未知錯誤文字）、hook、JSON 歧義、舊版 init 都立即停止；fake 都只收到一則訊息 | （沿用 #169–#180） |
| C0′ 逾時不誤標為 C0 的零輸入 | 分類依 mode 與 `input_status` | `TestProbeOneMessageNoInit`（`no_init_after_message`，exit 4） | #187 |
| 零 Store、零業務執行 | 同 C0 | `TestProbeOneMessage`（init 前後經 MCP 直接呼叫業務工具皆拒，執行 0）；`TestProbeOneMessageCommand`（Store 開啟 hook 為 0、未建立資料庫） | （沿用 #167、#176） |
| 白名單輸出 | 結果與 trace 只有固定代碼與計數；不存訊息文字 | `TestProbeOneMessage`、`TestProbeOneMessageStops`「marks in the init」等：結果與 trace 不含 `Reply only`、標記或 capability | （沿用 #173） |

fake 新增（測試資料，不是真實 CLI 錄製）：`wait_message` 步驟（等到一則完整的 stdin 行才繼續）、`close_stdin` 步驟、情境選項 `no_stdin_read`、計數檔 `APA_FAKE_CLI_STDINMSGFILE`（行數、符合 `SDKUserMessage` 形狀的訊息數、第一則內容的 SHA-256）。情境檔 `probe1-wait-message.json`、`probe1-message-no-init.json`。

### 22.4 限制

- 「一則訊息」是 host 能控制的輸入上限，**不是**一次模型請求或固定用量的保證。用量記 `unknown`。收到 init 後終止程序，也撤回不了已送往服務端的請求或用量。
- 真實 2.1.288 會不會接受這一行、送入後會不會產生 init，都**未驗證**。
- 寫入在程序啟動後立即進行。若真實 CLI 在 init 之前就開始處理訊息，模型回覆會讓 probe 以 `unexpected_message` 停止，而不是等它答完。
- 其餘真實執行的限制（HOME、keychain、網路、離開 process group 的子孫程序）與 C0 相同。

突變：新增 8 項（#183–190）；#165、#172 因所指的程式行改寫而改指新位置（意圖不變）。共 190 項，原 99 項文字未動。

## 23 `cf97c87` review 修正：寫入失敗立即停止且不被後續 init 覆蓋（2026-10-04，Claude Code 執行，待 Codex 獨立 review）

| 項目 | review 發現 | 修正 | 測試 | 突變 |
|---|---|---|---|---|
| 1 | 寫入失敗只記在結果裡，run 要等到總逾時才結束 | `runner.go`：寫入一結束，主事件迴圈就檢查結果；`failed` 或 `partial` 立即停止 run（runner 結果 `input_error`，代碼 `input_failed`／`input_partial`），照常清理 | `TestProbeOneMessageWriteFailure`「later init false」：CLI 先關閉 stdin，寫入必定遇到 broken pipe。結果立即為 `failed`／`input_failed`，遠早於 10 秒等待上限 | #191 |
| 2 | 確認的寫入失敗之後若讀到 init，會被記為成功 | 第一個停止原因不會被之後的事件取代；`probe.go` 只在 run 確實停在 init 時保存觀測資料，停止後才讀到的 init 一律不保存 | 同上「later init true」：CLI 忽略 SIGTERM，在停止後仍印出 init 並被讀到；結果仍是 `failed`，沒有觀測資料 | #191、#192 |
| 3 | 不能一律要求 `InputComplete` | 「init 先到、run 停止、寫入未開始」仍是合法的 `observed` | `TestProbeOneMessageInitBeforeWrite`：只供測試替換的 `beforeWrite` 掛鉤讓寫入等到 run 開始停止，結果為 `observed`、`input_status not_started`、0 位元組 | #193 |

兩個受控時序都不靠運氣：失敗案例用既有的啟動計數掛鉤當屏障，等 CLI 關閉 stdin 之後才開始寫；init 先到案例用新增的 `beforeWrite` 掛鉤，等 run 開始停止之後才進入寫入。Codex 的獨立探測程式（未加入 repo）在暫存副本上重跑三輪，兩案都以 `input_failed` 在 1 秒內結束、沒有觀測資料。

Codex 回報首次連續執行回歸時曾出現一次子程序非零退出，新容器中未重現，原因未確定，不列為已確認的缺陷；本輪也未觀察到。

共 193 項突變，原 99 項文字未動。訊息格式對真實 CLI 的相容性仍未驗證。

## 24 `38eb2ba` review 修正：結果不依賴通知先後（2026-10-04，Claude Code 執行，待 Codex 獨立 review）

- 發現：寫入結果與 init 幾乎同時就緒時，主迴圈可能先處理 init；runner 只保留第一個停止原因，之後的寫入失敗無法更正結果。Codex 在斷網容器內三輪都重現了 `observed`／`init_observed`，而 `input_status` 卻是 `failed`、0 位元組、有觀測資料。
- 修正（`probe.go`）：`Run` 回傳前一定等寫入結束，所以分類時寫入狀態已是最終值。分類**先看寫入狀態**：`failed`／`partial` 一律判為 `failed`（`input_failed`／`input_partial`），不保存觀測資料，與 run 先停在哪個原因無關。init 先到、寫入確實未開始的 `not_started` 仍可判為 `observed`。runner 的停止規則（第一個原因保留）不變，第 23 節的立即停止也保留。
- 回歸測試 `TestProbeOneMessageWriteFailureAfterInit`，不靠延遲：
  - CLI 先關閉 stdin；啟動掛鉤當屏障，確認這發生在寫入之前，所以寫入必定遇到 broken pipe。
  - 新增只供測試替換的 `afterWrite` 掛鉤，在寫入釋放鎖之後、送出完成通知之前執行：先建立信號檔，通知 CLI 寫入已失敗、這時才印出 init；再等到 run 已因該 init 開始停止，才交出寫入結果。先後是「寫入失敗 → CLI 印出 init → run 停在 init → 交出寫入結果」，正好是之前會出錯的順序。
  - 測試先確認 run 確實先停在 init（`runner_outcome` 為 `init_observed`）、寫入確實是 `failed`，再要求結果為 `failed`／`input_failed` 且沒有觀測資料。
  - 第一版測試（`751887a`）讓 CLI 一關閉 stdin 就印出 init，先後不夠固定：在較快的 Linux 容器中，init 在寫入開始前就讓 run 停止，寫入成了 `not_started`，測試在自身的前提檢查處失敗（沒有假通過）。`05e03e2` 改為上述交握。該次失敗的容器紀錄保留於 `artifacts/verify-container/runs/751887a2a15d-20261004T032619Z/`（`FAILED-NOTE.txt`）。
- Codex 的重現程式（每輪 100 次）在暫存副本上重跑三輪通過。此機器上需把其中等待子程序就緒的屏障由 1 秒放寬為 5 秒：本機啟動新 shell 腳本有時超過 1 秒，原值會在屏障處失敗，與修正無關。只改屏障時間，判定條件不變。
- 突變：#192 因所指程式行改寫而改指新位置；新增 #194（寫入失敗輸給 init 停止）、#195（寫入失敗後仍保留觀測資料）。共 195 項，原 99 項文字未動。

## 25 驗收紀錄：C0′ 實作及 fake 驗證於 `05e03e2` 通過技術 review（2026-10-04）

- 結果：`05e03e2` 通過 Codex 獨立技術 review。C0′ 的寫入失敗與競態問題關閉，本次範圍內沒有新的阻擋問題。
- **通過的是 C0′ 的實作與 fake 驗證**。不代表真實 CLI 的訊息格式、登入方式或用量已驗證。B1-04 整體仍 InProgress，未標 Done、未合併 main；live 認證接受清單維持空白。真實 C0′ 未執行。
- 證據歸屬：
  - **Codex 獨立重跑**（斷網容器）：全套 race／coverage；C0／C0′ 相關回歸三輪，含「init 先到、尚未寫入」的合法路徑；原始 broken-pipe 探測三輪；上輪競態重現每輪 100 次、共三輪；JS 契約向量；20 項證據匯入測試。全部通過。
  - **Claude Code 執行、Codex 核對紀錄**（不是 Codex 全部重跑）：195／195 突變、完整容器驗證七步、17 項隔離檢查，見 `artifacts/verify-container/runs/05e03e29e846-20261004T034851Z/`；本機檢查見 `artifacts/b1-04/05e03e29e846-20261004T034505Z/`。
- 歷程：`cf97c87`（首次交付）→ `38eb2ba`（寫入失敗立即停止）→ `05e03e2`（結果不依賴通知先後；通過）。修正內容見第 23、24 節。

## 26 執行一次真實 C0′ 的確認清單（2026-10-04 擬定；待 Aaron 核准，未執行）

本節只是清單。**沒有 Aaron 對「執行一次 C0′」的明確核准就不執行**；執行後不論結果，都不重跑、不放寬任何清單，也不轉往階段 C。

### 26.1 與 C0 的差別

C0′ 會送出**一則**使用者訊息，所以和 C0 不同：設計上就預期可能產生模型請求與用量。程式只保證不會送第二則，也不會等模型回答；**不保證只有一次模型請求，也無法撤回已送出的請求**。

### 26.2 固定內容

- 訊息文字：`Reply only OK.`（程式常數，不能改）。
- 送出的那一行 stream-json：`{"type":"user","message":{"role":"user","content":"Reply only OK."},"parent_tool_use_id":null}` 加換行，共 95 位元組。只寫一次，在 CLI 啟動後立即寫入。
- 格式依據是 Agent SDK 的 `SDKUserMessage` 型別。**缺口**：沒有一手文件逐字寫出 CLI stdin 的線上格式，「每行一個 JSON 物件」是推論，真實 CLI 從未收過這一行（第 22.2 節）。

### 26.3 執行前，由 Aaron 確認

1. **核准**：明確同意執行**一次** C0′。核准範圍不含階段 C、帶購物 prompt 的 run、其他模型任務或 C0 重跑。
2. **CLI**：路徑 `/Users/mac/.local/bin/claude`；版本必須是 `2.1.289 (Claude Code)`（10/4 改釘選，見第 28 節；第 27 節那次執行時要求的是 `2.1.288`）。命令會先自行檢查版本，不符就停止，不升降版。若版本已變，需要重新核准釘選。
3. **自動更新**：維持程序層的 `DISABLE_AUTOUPDATER=1`，不改全域設定；是否另外暫停平時的自動更新，由 Aaron 決定。
4. **接受的風險**：
   - CLI 以 Aaron 本人的 HOME、網路與登入憑證執行，會使用 `~/.claude` 與 keychain；這不是作業系統沙箱。
   - 送出訊息後，可能產生一次或多次模型請求與用量，次數沒有硬上限保證；用量記為 `unknown`。
   - 若格式不被 CLI 接受，結果可能是解析錯誤、非預期訊息或逾時；這些都是可接受的結果，不會重試。
5. **登入與憑證來源**（階段 B，第 15.1 節第 1 項）：不阻擋 C0′ 執行，但解讀 `apiKeySource` 需要它。回報方式同前：登入方式／方案一行，以及各來源有／無／不確定；不貼 token、完整帳戶輸出或認證檔。

### 26.4 命令（核准後才執行）

```sh
cd /Users/mac/Documents/OpenSourceProjects/agent-payment-authority
git status --short && git rev-parse HEAD     # 工作區乾淨；HEAD 為通過 review 的 1f0b260（版本釘選 2.1.289），或之後只改文件的 commit
go build -o bin/apa-buyer ./cmd/apa-buyer
go version -m bin/apa-buyer | grep vcs       # vcs.revision 等於 HEAD、vcs.modified=false
APA_LIVE_CLI=true ./bin/apa-buyer probe-init --cli /Users/mac/.local/bin/claude --send-one-message --trace-dir <新證據目錄>/runs
echo "exit=$?"
```

- 只跑這一條；不帶 `--send-one-message` 就是 C0，不能混用。
- 最多等 init 30 秒；停止後清理最多約 6 秒（兩個寬限期）。版本檢查另計，最多 5 秒。
- 不開 Store、不使用 `APA_DB_PATH`、不建立 task、不連商店、不執行任何業務工具。

### 26.5 結果怎麼讀

| exit | status／code | 意思 | 之後 |
|---|---|---|---|
| 0 | `observed`／`init_observed` | 拿到一則可嚴格讀取、版本相符的 init；觀測資料是白名單投影 | 把觀測資料（第 17.3 節的欄位）與階段 B 回報一起交 Codex review，再由 Aaron 決定 `apiKeySource` 接受值、model／effort、agents／skills。**不會自動放寬任何清單** |
| 4 | `no_init_after_message` | 訊息已完整寫入，30 秒內仍沒有 init | 停止；原因不確定（格式可能不被接受，或 init 不會出現在此模式），另行決定 |
| 1 | `failed`／`input_failed`、`input_partial`、`input_not_started` | 訊息沒有完整寫入 | 停止，回報代碼，不重試 |
| 1 | `failed`／`api_retry_auth`、`api_retry_rate_limit`、`api_retry` | CLI 呼叫 API 失敗（認證、限流或其他） | 停止，不重試；認證失敗的原因由 Aaron 判斷 |
| 1 | `failed`／`unexpected_message`、`hook_message`、`ambiguous_json`、`parse_error`、`init_cli_version` 等 | 模型先回覆、出現 hook、歧義或非 JSON 輸出、版本不符等 | 停止，回報代碼 |
| 1（無 JSON） | — | 啟動前就被拒絕（opt-in、路徑、版本、取消） | 修正前提後再次核准 |

### 26.6 保存與交付

- 終端輸出與 trace 都只有白名單投影：模式、狀態與代碼、寫入狀態與位元組數、計數、觀測資料的固定標籤。不保存訊息文字、原始 init、CLI 文字、stdout／stderr、prompt、環境變數值或 capability。
- 另存：執行時的 commit、二進位的 vcs 資訊、起訖時間與 exit code。標為「Claude Code（或 Aaron）執行，待 Codex review」。
- 執行後即停止，等 review 與 Aaron 的下一步決定。

## 27 真實 C0′ 執行一次：版本檢查即停止（2026-10-04；Claude Code 執行，待 Codex review）

- 核准：Aaron 10/4 核准依第 26 節執行一次 `probe-init --send-one-message`；版本不符即停止，不自行更新或調整核准版本，不重試。
- 前置核對：HEAD `dcca26f`、工作區乾淨；重新建置，二進位 `vcs.revision=dcca26f…`、`vcs.modified=false`。
- 結果：exit 1。`/Users/mac/.local/bin/claude --version` 的輸出不等於 `2.1.288 (Claude Code)`，`apa-buyer` 在版本檢查處停止（`cmd/apa-buyer/main.go` 的 `checkLive`）。依程式設計不重複 CLI 文字，所以**實際版本未記錄**。
- 因此：probe 未啟動，stdin 0 位元組，C0′ 訊息未送出；執行過的只有 `--version`（在程序層停用自動更新的環境下）。Store 未開，業務呼叫 0，無觀測資料，無 trace 檔。起訖 04:32:52Z–04:32:54Z。
- 證據：`artifacts/b1-04/c0prime-real-20261004T043252Z/`（commit、binary-vcs、起訖時間、exit code、stderr 的固定訊息；stdout 與 `runs/` 為空）。
- 依核准條件已停止：不重跑、不更新 CLI、不改核准版本、不放寬清單、不轉往階段 C 或 C0。**C0′ 的目標（取得 init）未達成，這次不是 C0′ 的有效結果。** 之後要由 Aaron 自行確認本機 CLI 版本（Claude Code 未獲准執行 `--version` 以外的檢查，也不另外執行）；若版本已變，需重新決定釘選與是否再核准一次 C0′。

## 28 核准版本改為 2.1.289（`1f0b260`；交付時待 Codex 限定範圍 review，已通過，見第 29 節）

- 決定（Aaron，2026-10-04）：核准版本由 `2.1.288 (Claude Code)` 改為 `2.1.289 (Claude Code)`。原因是第 27 節那次 C0′ 在版本檢查即停止。只改版本釘選；不執行真實 CLI、登入檢查或 C0′，不改認證接受清單（仍為空）。
- 程式（`1f0b260`）：
  - `cmd/apa-buyer/main.go` 的 `approvedCLIVersion` 改為 `"2.1.289 (Claude Code)"`；live 與 probe 的 init 閘門照舊由它取版號，因此 init 須寫 `claude_code_version` = `2.1.289`。
  - `internal/buyer/gate.go`、`probe.go` 只改註解。`host.go` 的旗標註解仍寫依 2.1.287／2.1.288 的 `--help` 核對：**2.1.289 的 `--help` 未比對**（本輪不准執行真實 CLI）。
- 測試：
  - 版本檢查：`TestLiveOptIn` 對 2.1.288、2.1.287、2.1.290 皆拒絕，錯誤不重複 CLI 文字，且確認確實執行了版本檢查；`TestProbeOptIn` 對 2.1.288、2.1.287 拒絕；恰為 `2.1.289 (Claude Code)` 通過。
  - init 須符合新版本：新增 `TestApprovedVersionInit`，**不覆寫版本**（用正式釘選），由 shell 包裝在 `--version` 時回答釘選、其餘交給 fake。init 寫 2.1.289 → `observed`、`cli_version` = `approved`；寫 2.1.288、2.1.287 或 fake 的 0.0.0 → exit 1、`init_cli_version`，版本只顯示為 `other`。
  - 突變 #196：probe 的 init 版號寫死為舊值 `"2.1.288"`、不由釘選取得，被 `TestApprovedVersionInit` 抓到（斷言失敗，非編譯失敗）。共 196 項，原有項目文字未動。
- 驗證（Claude Code 執行，待 Codex 獨立 review；`1f0b260`）：
  - 本機 `artifacts/b1-04/1f0b260ae3e9-20261004T044907Z/`：make check、check-contracts、`go test -race -cover ./... -count=1`、B1-01、20 項證據匯入測試全部 exit 0；覆蓋率 buyer 91.9%、toolbridge 91.8%、apa-buyer 81.7%；乾淨匯出 demo-fake 0／2／0（同一 task 第二次依設計 exit 2）。
  - 容器完整驗證（既有映像 sha256:78258368…，未重建、無新依賴）：overall exit 0；七步驟全過；baseline PASS、196／196 突變；17 項隔離檢查 ok。`artifacts/verify-container/runs/1f0b260ae3e9-20261004T045259Z/`；主機端輸出 `artifacts/b1-04/container-1f0b260.log`。
- 範圍外：這不代表真實 CLI 2.1.289 的相容性、旗標、init 格式、登入或用量已驗證。真實 C0′ 要等本次 review 通過後，再由 Aaron 另行核准。歷史紀錄（第 15–27 節）保留當時的版本。

## 29 驗收紀錄：版本釘選 2.1.289 通過限定範圍 review（2026-10-04）

- 結果：程式 `1f0b260`、文件 `651ace0` 通過 Codex 限定範圍 review，沒有阻擋問題。確認核准版本為 `2.1.289 (Claude Code)`，舊版與其他版本仍被拒絕；init 必須符合新版本；認證接受清單仍為空；目前操作文件已更新，歷史版本紀錄保留。
- 證據歸屬：
  - **Codex 獨立重跑**（斷網容器）：`cmd/apa-buyer` 全套 race 測試；版本檢查、init 版本與 C0′ 命令測試三輪；init 閘門及寫入失敗／競態回歸三輪。全部通過。
  - **Claude Code 執行、Codex 核對紀錄**：完整容器驗證七步、196／196 突變、17 項隔離檢查（`artifacts/verify-container/runs/1f0b260ae3e9-20261004T045259Z/`）；本機檢查 `artifacts/b1-04/1f0b260ae3e9-20261004T044907Z/`。
- 範圍：**只有版本釘選**。不代表真實 CLI 2.1.289 的相容性、旗標（`--help` 未比對）、init 格式、訊息格式、登入或用量已驗證。不需要新增實作。
- 下一步：依第 26 節（版本 2.1.289、HEAD `1f0b260` 或之後只改文件的 commit）執行一次真實 C0′，**待 Aaron 核准，未執行**。B1-04 整體仍 InProgress，未標 Done、未合併 main；live 認證接受清單維持空白。

## 30 真實 C0′ 執行一次：取得 init（2026-10-04；Claude Code 執行，待 Codex review）

- 核准：Aaron 10/4 核准依第 26 節執行一次 `probe-init --send-one-message`，版本須為 `2.1.289 (Claude Code)`；接受 CLI 使用本人 HOME、網路與登入憑證及未知用量；不重試、不改訊息格式、不放寬清單、不轉往階段 C、不開 Store 或執行業務工具。
- 前置核對：HEAD `d30170e`、工作區乾淨；相對 `1f0b260` 只改 README 與 docs；重新建置，二進位 `vcs.revision=d30170e…`、`vcs.modified=false`。
- 結果：**exit 0，`observed`／`init_observed`**。版本檢查通過（`cli_version` = `approved`）。訊息完整寫入一次（`input_status` = `complete`，95 位元組）。業務呼叫拒絕 0、執行 0；Store 未開。CLI 在 init 後由 host 以 SIGTERM 結束（`cli_exit_code` 143）。起訖 05:47:35Z–05:47:37Z，用量 `unknown`。
- 觀測資料（白名單投影，原始 init 未保存）：
  - 工具 3／預期 3，其他 0；MCP server 1，`apa` 為 connected；`permissionMode` 為 `dontAsk`；model 為 `claude-sonnet-5-5`。
  - **`apiKeySource` 為 `none`**（顯示候選值之一）。
  - **plugins 2、agents 5**、skills 0。
  - 白名單外的 init 成員 7 個（名稱與內容未記錄）。
  - 欄位型別：`uuid`、`output_style`、`capabilities` 皆存在；`plugins`、`agents`、`skills` 為陣列。
- 事後檢查：沒有殘留的 CLI 或 bridge 程序，也沒有 probe 的 `apa-bridge-*` 暫存目錄。
- 證據：`artifacts/b1-04/c0prime-real-20261004T054735Z/`，內容為 commit、binary-vcs、起訖時間、exit code、stdout 白名單結果、`runs/` 的 trace；stderr 為空。
- **這次證明與未證明的事**：
  - 已觀測（10/4 依 Codex 核對修正措辭）：Host 完整寫入 95 位元組，停止前取得 init，未觀測到解析錯誤；**尚未證明 CLI 已解析或處理該訊息**。
  - 推測，未驗證：C0 零輸入 30 秒內沒有 init（第 21 節），這次送出訊息後約 2 秒內就有，所以 init 很可能要等到第一則使用者訊息才送出。
  - 未證明：沒有證明模型是否已處理這則訊息、請求次數或用量；送出後的請求不能撤回。
- **對正式 Gate 的影響，待 Codex review 與 Aaron 決定，本輪不改任何清單**：
  1. **依目前檢查順序，正式 Gate 會先因非空的 `plugins` 拒絕**（版本之後依序檢查 plugins、skills、agents，認證最後）。這是依程式推導，並非跑過正式 live。
  2. **即使 plugins／agents 都解決，`apiKeySource` = `none` 仍不在認證接受清單內**（清單為空），會停在 `auth_unconfirmed`。`none` 本身不證明是訂閱登入或任何計費方式；要不要接受、靠什麼佐證，需配合階段 B 的登入方式回報，由 Aaron 決定。plugins 與 agents 非空的處理見第 31 節的釐清方案。
  3. 白名單外成員 7 個：依設計不記錄；暫不擴大收集。
- 依核准條件已停止：不重跑、不放寬清單、不轉往階段 C。B1-04 整體仍 InProgress，未標 Done、未合併 main。

## 31 驗收紀錄：真實 C0′ 取得 init；plugins／agents 釐清方案（2026-10-04；方案待 Aaron 核准，未執行）

### 31.1 驗收

- 結果：Codex 核對 `d30170e` 的二進位資訊、stdout 與 trace，紀錄一致：95 位元組寫入完成、業務呼叫 0、取得 init、用量 unknown。**「取得真實 init」的目標已完成。**
- 證據歸屬：**Claude Code 執行、Codex 核對證據**，不是 Codex 獨立重跑。
- 第 30 節已依核對意見修正兩處：只寫「已觀測」，不寫「CLI 接受了該行」；Gate 的兩個阻擋原因分開寫。
- **正式 live 請購仍不能放行。** 不重跑、不放寬清單、不進階段 C；B1-04 仍 InProgress。
- 暫不擴大收集白名單外的 7 個成員。

### 31.2 目前已知與未知（plugins 2、agents 5）

- 已知：這次 init 的 `plugins` 有 2 個元素、`agents` 有 5 個；`tools` 正好是預期的 3 個、其他 0，所以這些 plugins 沒有在工具清單中多出可用工具；停止前沒有觀測到 hook 訊息。啟動參數含 `--restricted`（依 help 說明會忽略 user／project／local 設定檔），工作目錄是 probe 新建的暫存目錄。
- 未知：它們的名稱、來源與是否執行過。**出現在 init 只代表 CLI 列出它們，不代表已執行**；但也不能據此證明背景沒有執行，例如 plugin 的 hook 或 MCP 以外的行為。
- 推測，未驗證：5 個 agents 可能是 CLI 內建的那組（官方文件列出 6 個內建名稱，見第 32 節；10/4 修正：原句以本機自訂 agents 數量推論，並無依據，已刪除）；2 個 plugins 的來源可能不在 `--restricted` 涵蓋範圍內（例如受管設定或已安裝 plugin 的狀態）。**兩項都只是推測，不能據此允許。**

### 31.3 最小釐清方案（每一步都待 Aaron 核准；目前都未執行）

**A．不執行 CLI 的查證**（先做；不用模型、不送訊息）

1. Aaron 提供只含名稱的摘要，不貼內容、路徑細節或憑證：
   - 本機已安裝或啟用的 plugins 名稱；
   - `~/.claude/agents` 自訂 agents 的數量；
   - 是否有受管設定檔（`/Library/Application Support/ClaudeCode/managed-settings.json`），以及其中是否啟用 plugins（有／無／不確定）。
   - 也可以改為核准 Claude Code 只讀這幾個非憑證檔案的名稱或鍵名。
2. Claude Code 查一手文件，不執行 CLI，整理依據與缺口：
   - init 的 `plugins`、`agents` 元素是什麼格式，有哪些欄位；
   - 內建 agents 清單；
   - `--restricted` 是否涵蓋 plugins 和受管設定；
   - 是否有正式的旗標或設定可以不載入 plugins／agents。

**B．程式與 fake：只看身分，不放寬**（需核准實作，交 Codex review）

- probe 觀測資料新增 plugins／agents 的身分投影，比照 `apiKeySource` 的做法：
  - 名稱在 Host 固定的候選清單內（來自 A 的結果）才顯示原名，否則顯示 `other` 與長度；
  - agents 的來源只在文件列出的固定值內才顯示；
  - plugin 路徑只分類（在 `~/.claude/plugins` 下、在 probe 工作目錄下、其他），不保存原始路徑；
  - 不保存 description 等自由文字。
- Gate 不變，仍拒絕非空的 plugins／agents。只跑 fake 測試與突變，白名單不開旁路。

**C．再執行一次真實 C0′，取得名稱與來源**（需另行核准）

- 同第 26 節：會再送一則固定訊息，可能產生用量，請求次數沒有保證。
- 目前兩次觀測如此：C0 零輸入 30 秒內未取得 init（第 21 節），C0′ 送出訊息後取得（第 30 節）；其他零輸入方式尚未確認（10/4 修正原本的絕對敘述）。
- A 只能提出推測，要確認身分仍需 C 的實際 init。

**D．決定（C 之後，由 Aaron 決定）**

- 優先：若 A 找到有文件依據的方法，調整啟動參數讓 plugins／agents 不載入，讓 init 回到空清單，Gate 不變。這需要實作、fake 測試、review，再做一次真實驗證。
- 其次：只允許經確認的特定名稱與來源，不是全部允許；需要把 Gate 改成名稱清單，並另行 review。
- 認證（`none`）另依階段 B 摘要處理，與 plugins／agents 分開決定。

## 32 plugins／agents 釐清：A 查證與 B 身分觀測（2026-10-04；Claude Code 執行，待 Codex 限定範圍 review）

依據：vault 筆記 `Claude-Code-B1-04-plugins-agents釐清指示-92a3b77-20261004`（Aaron 核准第 2 節），起點 `92a3b77`。本輪沒有執行真實 CLI（含 `--help`、`--version`），沒有讀本機設定或 plugin／agent 內容，沒有改正式 Gate、認證接受清單或真實啟動參數。

### 32.1 官方來源

2026-10-04（UTC）查閱，以各頁的 `.md` 原文核對；需要時引用原文：

- Agent SDK TypeScript 參考（`SDKSystemMessage`、`ApiKeySource`）：https://code.claude.com/docs/en/agent-sdk/typescript
- CLI 參考（`--restricted`、`--bare`、`--safe-mode`、`--setting-sources`、`--settings`、`--plugin-dir`）：https://code.claude.com/docs/en/cli-reference
- Subagents（內建清單、範圍優先順序、停用方式）：https://code.claude.com/docs/en/sub-agents
- 環境變數（`CLAUDE_AGENT_SDK_DISABLE_BUILTIN_AGENTS`、`CLAUDE_CODE_SAFE_MODE`）：https://code.claude.com/docs/en/env-vars
- Plugin 載入（來源、synced plugins、`enabledPlugins` 的來源、磁碟位置）：https://code.claude.com/docs/en/plugins/loading
- 設定參考（`syncClaudeAiPlugins`、`enabledPlugins`）：https://code.claude.com/docs/en/settings-reference
- Headless（bare mode）：https://code.claude.com/docs/en/headless
- 受管設定（macOS 位置）：https://code.claude.com/docs/en/managed-settings

**版本差距**：這些是線上最新文件，不分版本；頁中提到的最高版本是 v2.1.288，**沒有提到 2.1.289**。以下各點是否完全適用於 2.1.289，未確認。

### 32.2 已確認（文件明載）

1. **init 結構**：`agents?: string[]`，只有名稱；`plugins: { name: string; path: string }[]`；`skills: string[]`；另有可選的 `plugin_errors`。
2. **內建 subagents** 共 6 個名稱：`Explore`、`Plan`、`general-purpose`、`claude`、`statusline-setup`、`claude-code-guide`。
3. **`--restricted`**：「loads only managed settings and `--settings`」；移除執行命令或程式碼的內建工具與 WebFetch；把檔案工具限制在工作目錄；拒絕 `bypassPermissions`。**沒有提到 plugins 或 agents。**
4. **plugin 來源**：
   - id 後綴有 `@<marketplace>`、`@inline`、`@skills-dir`（`~/.claude/skills/` 下含 manifest 的目錄）、`@synced`。
   - `enabledPlugins` 可來自 `--add-dir`、user、project、local、flag（`--settings`）、managed。
   - managed 的 `true` 強制啟用、`false` 封鎖，其他來源都不能覆寫。
5. **synced plugins 不下載也不載入的情況**：
   - 以 `ANTHROPIC_AUTH_TOKEN`、`CLAUDE_CODE_OAUTH_TOKEN` 或 `apiKeyHelper` 取代登入時；
   - 不從 Anthropic 擷取 feature flags 的 session，例如設了 `CLAUDE_CODE_DISABLE_NONESSENTIAL_TRAFFIC`；
   - `--bare`、`--safe-mode`；
   - `--setting-sources` 不含 `user`。
6. **內建 agents 的停用方式**：
   - 在 `-p` 模式設 `CLAUDE_AGENT_SDK_DISABLE_BUILTIN_AGENTS=1`，會移除全部內建類型，含 `general-purpose`。
   - `CLAUDE_CODE_DISABLE_EXPLORE_PLAN_AGENTS=1` 只移除 Explore 與 Plan。
   - `permissions.deny` 的 `Agent(<name>)` 會阻擋使用。
7. **`--safe-mode`**：
   - 不載入 plugins、MCP servers、custom agents 等；managed plugins 也不載入；
   - **MCP servers 同樣不載入**。
8. **`--bare`**：
   - 略過 plugins、subagents 等自動探索，只連命令列提供的 MCP；
   - 但「never reads OAuth credentials or the system keychain」，需要 `ANTHROPIC_API_KEY` 或 `apiKeyHelper`。
9. **`ApiKeySource`**：CLI 只回報四個值之一。`none` 是「No API key. The session authenticates another way, such as a claude.ai login, a bearer token, or a cloud provider」。

### 32.3 推論與推測（未驗證）

- **plugins 不太像 synced 來的**：本專案固定設 `CLAUDE_CODE_DISABLE_NONESSENTIAL_TRAFFIC=1`（`host.go` 的 `fixedEnv`）。依第 5 點，若文件適用於 2.1.289，那 2 個 plugins 應該不是 synced plugins。
- **可能的來源**：
  - 已排除的部分：`--restricted` 不讀 user／project／local，所以 user 的 `enabledPlugins` 不適用；工作目錄是空的暫存目錄；環境白名單不傳 `CLAUDE_CODE_PLUGIN_DIRS`。
  - 剩下文件提到的來源：managed settings（Aaron 表示「受管設定：有」），以及 `~/.claude/skills/` 下的 skills-dir plugin（`--restricted` 是否影響它，未確認）。
  - **來源仍未確認。**
- **5 個 agents**：比文件列的 6 個內建名稱少一個。可能是內建名稱的子集，也可能含 managed 或 plugin 定義的 agent。**未確認。**
- **`none`**：和 claude.ai 登入相符，但文件也列了 bearer token 與雲端供應商。環境白名單不傳 `CLAUDE_CODE_OAUTH_TOKEN`（Aaron 表示有設這個變數）和雲端變數，所以這次較可能是 claude.ai 登入。這只是推測；認證另行決定，本輪不改任何清單。
- **init 列出 ≠ 已執行**：init 列出 plugins／agents，只代表 CLI 這樣自述，不代表它們已執行。工具清單只有 3 個，也不能推論沒有其他背景行為。

### 32.4 未確認

- 2 個 plugins 的名稱與來源，以及 5 個 agents 的名稱。
- `--safe-mode` 是否也會停用 `--mcp-config` 指定的 apa bridge。文件只寫 MCP servers 不載入，沒區分命令列提供的。
- **名稱符合內建清單，只能確認名稱相符；來源仍未確認。** 官方 Subagents 文件明載，自訂的同名 `Explore` 可以覆寫內建 agent（10/4 依 Codex review 修正）。
- 停用內建 agents 的環境變數（`CLAUDE_AGENT_SDK_DISABLE_BUILTIN_AGENTS=1`）是否足以清空這次 init 的 agents 清單，仍需另行核准並驗證。
- managed settings 的 `enabledPlugins` 是不是來源。要確認得讀本機檔案，尚未獲准。

### 32.5 建議的最小方案

1. **A 不能回答身分問題，所以 B 有必要**。已實作，見 32.6。
2. **下一次真實觀測**（需另行核准）用 B 的投影可以得到：
   - agents 的名稱是否與 6 個內建名稱相符。名稱相符不代表來源是內建。
   - 每個 plugin 的名稱長度與路徑分類（synced／cache／其他 plugins 目錄／skills／工作目錄／其他）。路徑分類只是對 CLI 回報文字的分類，不是來源認證。
   - plugin 名稱沒有候選依據，仍只會顯示 `other`。若要顯示，需要 Aaron 提供名稱，或核准讀取下列檔案的鍵名。
3. **限制參數提案**（只是提案；現行啟動參數不變，待確認採用）：
   - **agents**：可提案在 CLI 環境加入 `CLAUDE_AGENT_SDK_DISABLE_BUILTIN_AGENTS=1`。文件明載它在 `-p` 生效，本專案也用 `-p`；文件沒有提到它影響 MCP。但名稱相符不能確認來源，這個變數是否足以清空清單，**仍需另行核准並驗證**（10/4 依 Codex review 修正）。
   - **plugins，若來自 managed settings 的強制啟用**：使用者端無法以 `--settings` 覆寫。有文件依據的停用方式都不適用：`--safe-mode` 同時停用 MCP servers，可能包含 apa bridge（未確認）；`--bare` 不能用訂閱登入。這時只能由 Aaron 調整受管設定（本輪不改），或另行評估是否逐一允許。
   - **plugins，若來自 skills-dir 或 marketplace**：可提案用 `--settings` 傳入 `{"enabledPlugins":{"<id>":false}}`（flag 來源，只對這次 session 有效），但要先知道 id。
4. **讀取本機檔案的請求**（待 Aaron 確認後才讀；只讀下列欄位，不讀其他內容）：
   - `/Library/Application Support/ClaudeCode/managed-settings.json`：`enabledPlugins` 的鍵名（plugin id）與值；頂層是否有 `syncClaudeAiPlugins`、`strictPluginOnlyCustomization` 及其值；`agents` 相關鍵是否存在。
   - `/Library/Application Support/ClaudeCode/managed-settings.d/`：檔名，以及每個檔案的相同欄位。
   - macOS 設定描述檔 `com.anthropic.claudecode` 也可能提供受管設定；是否查看、用什麼方式查看，由 Aaron 決定。

### 32.6 B 實作（`4e709ab`；測試修正 `8da15dd`）

- `internal/buyer/probe.go`：
  - 觀測資料新增 `plugins_entries`、`agents_entries`，各取前 16 個元素；`*_count` 維持精確數量。
  - **名稱**：只有在固定顯示清單內才顯示原名，否則顯示 `other` 與長度。agents 清單是 32.2 第 2 點的 6 個內建名稱；plugins 清單為空，因為目前沒有官方依據或 Aaron 提供的名稱。
  - **其他標籤**：`not_string`、`absent`、`malformed`。元素可以是字串，或用嚴格 reader 讀出、`name` 為字串的物件。除了 `name` 與 plugin 的 `path`，元素的其他內容一律不看，例如 description。
  - **plugin 路徑只輸出分類**：依 CLI 的 HOME 與 probe 工作目錄，分成 `home_plugins_synced`、`home_plugins_cache`、`home_plugins_other`、`home_skills`、`work_dir`、`other`、`relative`、`absent`、`not_string`。
  - 分類只看路徑文字：先 `Clean`，根目錄須整段相符，不讀磁碟、不跟隨連結。
  - **已知限制**：路徑若以 symlink 後的形式出現（例如 `/private/var` 與 `/var`），工作目錄會被分到 `other`。
  - **顯示清單不是接受清單**：正式 Gate 不變，仍拒絕非空的 plugins／agents；認證接受清單仍為空。
- 測試：
  - `TestProbeEntries`：已知名稱、未知名稱、缺欄位、型別錯誤、超過 16 個的上限（計數仍精確）。
  - `TestPathClass`：各分類、根目錄整段相符（`pluginsX`、`workshop` 不算）、`..` 先清理、沒有 HOME 的情況。
  - `TestProbeNoLeak`：把合成標記與 fake capability 放進 plugin／agent 的名稱與路徑，都不會進入結果或 trace。
  - `TestHostInitGate` 新增：agents 只有 `["Explore"]` 時，Gate 仍拒絕（`agents`）。
  - 既有的 C0′ 停止、寫入失敗、競態與「init 先到、尚未寫入」回歸照舊。
- 突變 #197–#201：
  - 名稱原樣輸出；
  - 路徑原樣輸出；
  - 根目錄只比對前綴；
  - 不先 `Clean`；
  - 取消 16 個的上限。
  - 共 201 項，原有項目文字未動；提交前預檢 #197–#201 都被斷言抓到。
- `8da15dd` 只改測試：`TestProbeOneMessageInterleaving/cancel_after_0s` 在 `4e709ab` 的 B1-01 回歸中失敗一次。原因是 0 秒的取消搶在 Probe 自身的啟動前檢查之前，Probe 依設計拒絕啟動並回錯誤，測試卻把這當成失敗。現在接受這個結果，但前提是 fake 從未啟動、沒有寫入；連跑 30 次通過。

### 32.7 驗證（Claude Code 執行，待 Codex 獨立 review；最終程式 `8da15dd`）

- **本機**（`artifacts/b1-04/8da15dd7ee03-20261004T065920Z/`）：make check、check-contracts、`go test -race -cover ./... -count=1`、B1-01、20 項證據匯入測試全部 exit 0。
  - 覆蓋率：buyer 92.7%、toolbridge 91.8%、apa-buyer 81.7%。
  - 乾淨匯出 demo-fake：0／2／0（同一 task 的第二次依設計 exit 2）。
- **容器完整驗證**（`8da15dd`，沿用既有映像 sha256:78258368…，未重建、無新依賴）：overall exit 0，七步驟全過；baseline PASS、201／201 突變被抓到；17 項隔離檢查 ok。
  - 證據：`artifacts/verify-container/runs/8da15dd7ee03-20261004T063954Z/`；主機端輸出 `artifacts/b1-04/container-8da15dd.log`。
- **失敗嘗試**（保留，各附 FAILED-NOTE.txt，不算證據）：
  - `artifacts/b1-04/4e709aba45af-20261004T062856Z/`：B1-01 exit 1，原因是 0 秒取消的測試時序（`8da15dd` 已修）。其他四項 exit 0。
  - `artifacts/b1-04/8da15dd7ee03-20261004T063517Z/`：B1-01 exit 1。`cmd/apa-buyer` 的 `TestLiveOptIn` 在全套並行測試負載下，代用 shell 腳本的 `--version` 超過 5 秒上限、沒有執行。這個測試自 `1f0b260` 起未改；容器內與之後的本機重跑都通過。判斷是本機的時序問題，未修改程式。其他四項 exit 0。
- **未執行**：任何真實 CLI 命令、真實觀測、正式 live。

## 33 review 修正：`null` 判為 `not_string`；名稱相符不等於來源（2026-10-04；Claude Code 執行，待 Codex review）

- **依據**：Codex 對 `8da15dd`／`8d2baaf` 的 review（Aaron 於對話中轉交）。
  - Codex 在斷網容器獨立執行：buyer、apa-buyer 的 race 測試通過；相關回歸三輪通過；新增的 `null` 探測失敗，確認了問題。
  - 201 項突變屬 Claude Code 執行、Codex 核對紀錄。
  - 結論：這是觀測正確性問題，**沒有重現資料外洩或 Gate 繞過**。
- **問題**：`json.Unmarshal` 會把 JSON `null` 解成空字串而不報錯，所以 `name: null` 被顯示成 `other`、長度 0，`path: null` 被分類成 `relative`。
- **修正**（`b46e332`）：
  - 共用的 `pick` 先檢查 JSON 型別，`null` 一律為 `not_string`。經 `pick` 的欄位都套用同一規則：元素名稱、apa server status、`permissionMode`、`claude_code_version`、`model`、`apiKeySource`。
  - plugin 的 `path` 同樣處理。
  - 缺欄位仍為 `absent`。正式 Gate 未改。
- **測試**：
  - `TestProbeEntries` 新增 `name`／`path` 為 `null`，以及 agents 元素為 `null`、`{"name":null}`、`{}` 的情況。
  - 新增 `TestProbeNullStrings`，檢查頂層欄位為 `null`。這裡用 raw init 行，因為 fake 的 init 覆寫會把 `null` 當成移除。
  - 突變 #202、#203，共 203 項；提交前預檢都被斷言抓到。
- **文件**：第 32.4–32.5 節改為：
  - 名稱符合內建清單，只能確認名稱相符，來源仍未確認。依據是官方 Subagents 文件：自訂的同名 `Explore` 可以覆寫內建 agent。
  - 停用內建 agents 的環境變數是否足以清空清單，仍需另行核准並驗證。
  - 路徑分類只是對 CLI 回報文字的分類，不是來源認證。
- **驗證**（Claude Code 執行，待 Codex 獨立 review；`b46e332`）：
  - 本機 `artifacts/b1-04/b46e332e0997-20261004T074250Z/`：make check、check-contracts、race／coverage、B1-01、20 項證據匯入測試全部 exit 0。覆蓋率 buyer 92.8%、toolbridge 91.8%、apa-buyer 81.7%；demo-fake 0／2／0。
  - 容器完整驗證（既有映像 sha256:78258368…）：overall exit 0，七步驟全過；baseline PASS、203／203 突變；17 項隔離檢查 ok。證據在 `artifacts/verify-container/runs/b46e332e0997-20261004T071959Z/`。
  - 失敗嘗試（保留，附 FAILED-NOTE）：`artifacts/b1-04/b46e332e0997-20261004T073843Z/` 的 make check exit 2。原因是 `TestVersion` 的 holder 檢查在並行負載下花了 2.056 秒，超過 2 秒上限；該測試自 `b3c66f6` 起未改，同一次 race 步驟與容器都通過，重跑也通過。
- 本輪沒有重做 A 查證，沒有執行真實 CLI，沒有讀受管設定，也沒有放寬任何清單。

## 34 驗收紀錄：plugins／agents 身分觀測（`b46e332`）；非阻擋待辦：重複出現的時序測試失敗（2026-10-04）

### 34.1 驗收

- 依 Aaron 10/4 指示，同步本輪驗收：
  - A 官方文件查證（第 32 節）；
  - B 只顯示身分的觀測（`4e709ab`、測試修正 `8da15dd`）；
  - `null` 判為 `not_string` 的修正（`b46e332`）；
  - 對應文件（`8d2baaf`、`d739952`）。
- **證據歸屬**：
  - **Codex 獨立執行**（斷網容器，針對 `8da15dd`，見第 33 節）：buyer、apa-buyer 的 race 測試；相關回歸三輪；`null` 探測。`null` 探測當時失敗，促成 `b46e332`。
  - **對 `b46e332` 本身**：對話中沒有轉交 Codex 逐項重跑的內容，所以這裡不把任何項目記為 Codex 獨立重跑。
  - **Claude Code 執行**：`b46e332` 的本機檢查與容器完整驗證，包括 203／203 突變與 17 項隔離檢查（第 33 節）。
- **範圍**：
  - 只有觀測的正確性與文件。正式 Gate、認證接受清單與真實啟動參數都沒改。
  - 名稱與內建清單相符，只能確認名稱；路徑分類不是來源認證。
  - **2 個 plugins、5 個 agents 的身分與來源仍未確認。**
- **仍須 Aaron 另行核准**：真實 C0′（以新的觀測取得身分）、讀取受管設定（第 32.5 節列出的路徑與欄位）、任何清單調整。
- B1-04 整體仍 InProgress；未標 Done、未合併 main。live 認證接受清單維持空白。

### 34.2 非阻擋待辦：重複出現的時序測試失敗（未調查，未改逾時）

本輪在 macOS 本機驗證中出現下列失敗。每次同一 commit 的容器驗證都通過，本機重跑也通過：

| 首次出現 | 測試 | 情形 | 上限 |
|---|---|---|---|
| `8da15dd` 的 B1-01 | `cmd/apa-buyer` `TestLiveOptIn`（2.1.288 子案例） | 代用 shell 腳本的 `--version` 沒有在上限內執行，連標記檔都沒建立 | 正式版本檢查 5 秒（`versionLimits`） |
| `b46e332` 的 make check | `internal/buyer` `TestVersion`（holder 檢查） | 花了 2.056 秒 | 測試自訂 2 秒 |
| `7267f91`（B1-06 修正）的 make check | `cmd/apa-buyer` `TestLiveOptIn`（2.1.288 子案例），第二次出現 | 同第一列：代用腳本未在上限內執行；只重跑 make check 一次即通過（`artifacts/b1-06/7267f91a3fd5-20261004T103039Z/`） | 正式版本檢查 5 秒 |

- **相關但不同**：
  - `4e709ab` 的 `TestProbeOneMessageInterleaving/cancel_after_0s` 已找到原因：測試沒有考慮 0 秒取消可能搶在 Probe 的啟動前檢查之前。已在 `8da15dd` 修正測試，不列入本待辦。
  - 第 24 節也記過 Codex 的重現程式在本機需要把 1 秒的等待放寬為 5 秒。
- **原因未確認**：「並行測試負載下，本機啟動新程序或新建立的腳本變慢」只是推測，沒有量測。
- **本輪不做**：不調整正式逾時（`versionLimits` 等）或測試上限，也不以反覆重跑取代調查。上表的通過紀錄都來自另一次執行，不能證明原因。
- **待辦**（非阻擋，未排程）：
  1. 在本機量測並行 `go test` 時，啟動 shell 腳本的延遲分布；與容器內比較。
  2. 確認失敗是否集中在新建立的腳本第一次執行。
  3. 依量測結果，再決定是改測試的等待方式，還是需要 Aaron 決定調整正式上限。
