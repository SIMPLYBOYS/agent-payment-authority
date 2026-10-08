# B1-04 Buyer wrapper：CLI 橋接 PoC

> **10/8 真實 plugin ID 查詢（Claude Code 執行，待 Codex 核對）**：依 Aaron 核准，在 macOS 以 §46 已審查的包裝執行一次。結果 **`no_match`**（exit 3）：`plugin list` 在該環境列出 1 筆，名稱不是兩個候選。依規則停止，不重試、不擴大查詢（第 46.10 節）。這不代表 plugin 不存在；完整 ID 仍未取得。
>
> **10/8 `5bab248` 通過限定範圍技術複核，一般例外下的程序收尾問題關閉**（第 46.9 節；修訂紀錄 46.8，第二輪 46.7，第一輪 46.6）。Linux 目錄清理差異、最後 `rmdir` 的時窗、脫離 process group 的程序仍是未解限制。真實查詢須 Aaron 另行指定執行者並核准一次嘗試；本次 review 不算執行核准。真實查詢尚未核准、未執行；Gate、認證清單及產品行為不變。
>
> **10/8 方案 A 後的診斷型 C0′（Claude Code 執行，待 Codex review）**：取得 init（exit `0`，`observed`）。**plugins 仍非空**：同樣 2 個（`cc-plugin-agents-md`、`cc-plugin-plugin-authoring`，path `relative`）。§44.4 的其他條件都符合：agents／skills 為 0，MCP 1 個且 apa connected，tools 3／3／0，`dontAsk`，`approved`。依 44.5：**方案 A 本次未達目標**，保存證據後停止；不推論來源、不自動換方案（第 45 節）。正式請購阻擋仍在。
>
> **10/8 方案 A 驗收與下一次診斷清單**：**方案 A 於 `40bd263` 通過限定範圍技術 review，關閉這項實作與 fake 驗證。** 只證明 APA 子程序的專用 plugin 根目錄設定、失敗處理與 fake 回歸符合要求，不代表真實 plugins 已停用，也不代表正式請購可放行（第 43.6 節）。下一次診斷型 C0′ 的執行確認清單已整理（第 44 節；§39.3 的 `PRODUCT_BASE` 改為 `40bd263`）。**`d46ca0b` 通過 Codex 限定文件複核**：下一次診斷型 C0′ 清單已通過文件複核，尚待 Aaron 另行核准執行（第 44.7 節）。
>
> **10/8 方案 A 實作（Claude Code 執行，待 Codex review）**：程式 `6caba93`，測試補正 `086f640`、`40bd263`。本機定向 fake 測試通過。**核准的那一次完整斷網容器驗證（`086f640`）失敗**，原因是測試腳本在 Linux 上讀錯權限欄位（`stat -f` 是 macOS 寫法），不是產品行為；已在 `40bd263` 修正，只改測試。**10/8 經 Aaron 核准，對 `40bd263` 補跑一次完整斷網容器驗證：通過**（overall exit 0；基準線 PASS、255／255 突變被抓到、隔離 17 項符合；Claude Code 執行，待 Codex 核對；第 43.5 節）。`086f640` 的失敗紀錄保留。plugins 是否真的停用仍未驗證。
>
> **10/8 plugins 方案於 `14eb7b5` 通過限定設計複核，R1、R2 關閉**（第 42 節，修訂紀錄 42.8，驗收紀錄 42.9）。通過範圍是受控實驗的設計，不代表 plugins 已停用或正式請購可放行。等待 Aaron 核准方案 A 的實作、fake 測試與一次完整斷網容器驗證；真實診斷須在實作 review 通過後另行核准。依官方文件，沒有任何候選方案能同時保證四件事：清空 plugins、保留 APA MCP、不改登入、不需要完整 plugin ID。方案 A（APA 專用暫存 plugin 根目錄）是受控實驗，不是停用保證：空根目錄可能在背景被重新填入；結果非空只代表未達目標，不推論來源。
>
> **10/8 診斷型 C0′ 執行結果（Claude Code 執行，待 Codex review）**：取得 init（exit `0`，`observed`，版本為核准的 `2.1.293`）。**plugins 非空（2 個：`cc-plugin-agents-md`、`cc-plugin-plugin-authoring`）**；agents 明確為空（`agents_count` 0）；skills 0。依 39.7.3：**記錄執行阻擋並停止**，不追查來源、不改允許清單、不重試。正式 Gate 仍會拒絕這次 init（第 41 節）。**10/8 Codex 核對完成**：本次 agents／skills 明確為空，plugins 非空，正式請購阻擋仍在；agents 結果可作為 L1-c 對應觀測項目的證據，L1-b、L1-c 整體不標通過。下一步建議為 plugins 最小隔離或停用方案的設計提案，待 Aaron 核准派工（第 41.1 節）。
>
> **10/8 核准 CLI 版本改為 `2.1.293 (Claude Code)`（Claude Code 執行 fake 驗證，待 Codex 限定 review）**：版本由 Aaron 提供，不是 Claude Code 或 Codex 查詢所得。程式 `bd29a4e`；新版通過、舊版與其他版本被拒、init 須為 `2.1.293`；定向 fake 回歸全部通過（第 40 節）。**程式 `bd29a4e` 通過 Codex 限定 review**；§39.3 產品基準與 §3、§7.3、§39.7.4 的版本說明已補正（第 40.1 節）。**`3d74230` 通過限定文件複核，R1 關閉**（第 40.2 節）。可提出下一次診斷型 C0′ 執行核准；review 不等於執行核准。新版真實 runtime 的行為仍待觀測，plugins／agents 仍未知。
>
> **10/8 §39.3 變數名稱修正（Claude Code 執行最小分支測試，待 Codex review）**：三處緊接全形字元的變數改為 `${BR}`、`${PROBE_EXIT}`（兩處）。最小分支測試不含 CLI、建置或刪除命令，涵蓋正常回報、前置拒絕與證據保存失敗；在 bash 3.2、四種 locale 下 16／16 PASS（第 39.15 節）。版本釘選未改，C0′ 未重跑。**10/8 `90d6e2f` 通過 Codex 限定 review，bash 變數名稱問題關閉**（第 39.16 節）。剩下的阻擋是 CLI 版本與釘選不符。
>
> **10/8 診斷型 C0′ 第二次核准執行（Claude Code 執行，待 Codex review）**：Aaron 核准一次；前置步驟 1～5 全部通過（基準 `9fcd105`，`vcs.modified=false`）。**apa-buyer 的版本檢查拒絕**：`/Users/mac/.local/bin/claude` 回報的版本不是核准的 `2.1.289 (Claude Code)`。probe 會話沒有啟動、沒有送出訊息、沒有 trace，plugins／agents 仍未知。另發現 §39.3 腳本最後一行在 bash 3.2 下觸發 `unbound variable`，腳本以 1 結束；證據已保存（第 39.14 節）。依核准不重試、不更新 CLI。
>
> **10/8 診斷型 C0′ 執行（Claude Code 執行，待 Codex review）**：依 Aaron 核准執行一次 §39.3 腳本，**在前置檢查第 5 步停止（exit 2），probe 未啟動**：建置出的二進位讀不到 `vcs.revision`／`vcs.modified`（兩者皆空）。沒有啟動 probe 會話、沒有送出訊息、沒有 trace；plugins／agents **未取得觀測，仍是未知**。依核准不重試（第 39.11 節）。
>
> **10/8 建置前置流程修正與驗證（Claude Code 執行，待 Codex review）**：§39.3 改在全新暫存目錄中以一般本機 clone 建置（`d61c838`）。只執行步驟 1～5 的建置前置驗證，結果 PASS：`vcs.revision` 等於 `d61c838…`、`vcs.modified=false`。沒有執行產物、真實 CLI 或 C0′（第 39.12 節）。**10/8 建置前置流程於 `d61c838` 通過 Codex 限定 review**（驗證紀錄 `1c9f03c`；第 39.13 節）。可進入「另行核准一次診斷型 C0′」階段；尚未取得新的 runtime 觀測，plugins／agents 仍未知；review 不代替執行核准。
>
> **10/8 L1-b 來源調查暫緩**：plugin 來源調查依 Aaron 決定暫緩，來源未確認保留為已知限制。正式 Gate 維持不變。下一步為整理一次診斷型 C0′ 清單，確認目前 plugins／agents 是否仍非空；待 Codex review 與 Aaron 另行核准，尚未執行（第 39.7 節）。暫緩調查不是解除阻擋：真實 CLI 若仍載入 plugins，現有閘門的執行阻擋仍在。**診斷型 C0′ 清單於 `d12adcc` 通過限定文件 review，R1／R2 關閉**（Codex 靜態核對，不是測試通過；第 39.10 節）。真實執行仍待 Aaron 另行核准，尚未執行。
>
> **10/8 L1-b 第一段**：probe 的 plugins 顯示候選加入三個名稱（完全相符才顯示），只有 fake 驗證（`e99f389`；第 38 節）。**probe 顯示候選修正於 `e99f389` 通過限定範圍技術 review**（第 38.1 節）；只關閉這項修正，**L1-b 的 plugin 來源仍未確認**。閘門、認證清單與啟動參數未變。
>
> **10/7 L1-a**：分支 `b1-04-live` 以固定環境變數 `CLAUDE_AGENT_SDK_DISABLE_BUILTIN_AGENTS=1` 停用內建 agents（`79a4a5f`、測試修正 `418f0de`；第 35 節）。**10/8 於 `418f0de` 通過 Codex 限定範圍技術 review，L1-a 關閉**（第 36 節）；只證明固定環境變數的傳遞與 fake 回歸，**不證明真實 CLI 的 agents 已清空**。未執行真實 CLI，閘門與認證接受清單未變。B1-04 仍 InProgress；L1-b 需 Aaron 另行核准。
>
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
- 環境只帶 `HOME USER LOGNAME PATH TMPDIR LANG LC_ALL TERM`，加上固定的 `DISABLE_AUTOUPDATER=1`、`CLAUDE_CODE_DISABLE_NONESSENTIAL_TRAFFIC=1`（Aaron 10/3 同意；依官方 env-vars 文件，本機 help 未列；用來減少更新與非必要流量，不是防火牆），以及 10/7 起的 `CLAUDE_AGENT_SDK_DISABLE_BUILTIN_AGENTS=1`（L1-a，見第 35 節；依官方文件在 `-p` 模式移除內建 subagent 類型，對真實 init 的效果未驗證），fake 模式再加情境路徑。API key、`ANTHROPIC_AUTH_TOKEN`、`CLAUDE_CODE_OAUTH_TOKEN`、`ANTHROPIC_PROFILE`、雲端供應商、代理、`CLAUDE_CONFIG_DIR` 與 host 自己的設定都不傳。fake 模式的 `HOME` 換成每次新建的空目錄，fake 不會靠近使用者的登入。
- `--version` 檢查（fake 命令確認 `--fake-cli` 確實是 fake；live 要求輸出恰為核准版本 `2.1.293 (Claude Code)`；10/3 前為 2.1.287，10/3–10/4 為 2.1.288，10/4–10/8 為 2.1.289，第 28、40 節）也走同一套程序規則：獨立 process group、輸出 4 KiB、stderr 4 KiB、5 秒，結束時回收整個 group。逾時、取消、超量或非零退出都視為失敗，不啟動 task；啟動 task 前再確認一次未被取消。
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
- `claude_code_version`：等於核准版本的版號 `2.1.293`（10/4 前為 `2.1.288`，10/4–10/8 為 `2.1.289`）；fake 為 `0.0.0`（`cli_version`）。
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
3. **CLI 的網路存取不受限**：它需要連 Anthropic API。模型沒有網路工具；`DISABLE_AUTOUPDATER`、`CLAUDE_CODE_DISABLE_NONESSENTIAL_TRAFFIC` 兩個固定環境變數只是減少更新與非必要流量，不是防火牆，也不排除 plugin 或 managed 設定的副作用（第三個固定變數只關內建 agents，與網路無關）。
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

## 35 L1-a：以固定環境變數停用內建 agents（2026-10-07；Claude Code 執行，待 Codex review）

- **依據**：Aaron 10/7 核准 L1-a（`docs/commercial-demo-roadmap.md` §8）：從 `3de40b8` 建立 `b1-04-live`，只加入停用內建 agents 的固定環境變數及 fake 驗證。
- **不做**（依核准範圍）：不執行真實 CLI（含版本、登入、help、C0／C0′）；不讀 HOME、keychain、受管設定或憑證；不加任何 plugin 參數；不改認證接受清單（仍為空）；不改 init 閘門。
- **程式**（`79a4a5f`；測試修正 `418f0de`）：
  - `internal/buyer/host.go` 的 `fixedEnv` 加入 `CLAUDE_AGENT_SDK_DISABLE_BUILTIN_AGENTS=1`。依據是第 32.2 節第 6 點的官方文件（`-p` 模式移除全部內建 subagent 類型，含 `general-purpose`）。
  - buyer 啟動的每個 CLI 程序都經 `CLIEnv`：版本檢查、live 與 fake run、C0 與 C0′ probe、demo host 的 fake run。父環境的同名變數不在 allowlist，所以不能把它改掉。
  - **效果未驗證**：它能否讓真實 2.1.289 init 的 `agents` 清單變空，要等 L1-c 的一次真實觀測。閘門照舊拒絕任何非空的 `agents`、`plugins`、`skills`；2 個 plugins 的來源仍未確認（L1-b）。
- **測試**：
  - `TestArgvAndEnv`：CLI 環境的完整清單（含新變數），父環境的金鑰、token、`CLAUDE_CONFIG_DIR` 等仍不傳。
  - `TestCLIEnvReachesChild`：以腳本代替 CLI，子程序看到的值是 `1`；父環境設 `0` 也一樣；`CLAUDE_CODE_OAUTH_TOKEN` 不傳。
  - `TestLiveEnvDisablesBuiltinAgents`：以包裝腳本記錄啟動時的值，再 `exec` 成 fake；live run（版本檢查＋run）、C0 probe、C0′ probe 的每個 CLI 程序都看到 `1`。
  - 既有的 `TestHostInitGate` 仍拒絕 `agents: ["Explore"]`。
  - 突變 #248：從 `fixedEnv` 拿掉這個變數。提交前預檢：`TestArgvAndEnv`、`TestCLIEnvReachesChild` 失敗；另以同樣改法確認 `TestLiveEnvDisablesBuiltinAgents` 失敗（看到 `unset`）。
- **失敗與修正**（保留紀錄）：`79a4a5f` 的本機 `make check` 中，新的 `TestCLIEnvReachesChild` 失敗：剛建立的腳本在高負載的全套測試裡 10 秒內沒有回應 `--version`，與第 34.2 節的待辦同一症狀（原因未確認）。這個測試要驗的是環境，不是該上限，所以 `418f0de` 讓兩個新測試在受限呼叫前先預跑一次新腳本：預跑上限 60 秒，不套用正式版本檢查的 5 秒限制（`TestCLIEnvReachesChild` 之後的受限呼叫用的是測試的 10 秒上限）。10/8 用語更正：原寫「不限時」，實際一直有 60 秒上限；`418f0de` 的 commit 訊息中的 "untimed" 同此更正，歷史不改寫；包裝腳本遇到 `APA_WARM=1` 立即結束，而 CLI 的 allowlist 不會傳這個變數。正式上限未改。紀錄：`artifacts/b1-04/79a4a5ff204c-20261007T110623Z/`（附 `FAILED-NOTE.txt`）。
- **本機驗證**（Claude Code 執行，待 Codex 獨立 review；`418f0de`，`artifacts/b1-04/418f0de1506d-20261007T111449Z/`）：
  - check-contracts、`go test -race -cover ./... -count=1`、20 項證據匯入測試：exit 0。覆蓋率 buyer 92.9%、apa-buyer 81.7%、toolbridge 91.8%。
  - `make check` exit 2、B1-01 exit 1（34 項 PASS、1 項 FAIL，即其中的 `make check`）：失敗的只有 `TestLiveOptIn`（`make check` 與 B1-01 兩次）與 `TestVersion`（B1-01 中一次），都是第 34.2 節列出的既有計時失敗，本輪沒有改這兩個測試；同一 commit 的 race 步驟跑同樣的測試則通過。新的三個測試在兩次執行中都通過。主機負載仍高（另有非本工作的長時間程序）。
- **容器完整驗證**（Claude Code 執行，待 Codex 核對紀錄；`418f0de`，斷網容器，映像 `sha256:78258368…b6d0`，以全名 `docker.io/library/apa-verify:b1-05` 指定）：
  - **第一次（保留）**：在 Claude Code session 結束時被終止（exit 143，證據匯入無檔案），沒有結果。`artifacts/verify-container/runs/418f0de1506d-20261007T112159Z/`（附 `INTERRUPTED-NOTE.txt`）、`artifacts/b1-04/container-418f0de-attempt1-terminated.log`。
  - **第二次**（Aaron 啟動 Docker 後補跑一次）：overall exit 0。七步全過：check-contracts 16 秒（adapter 26／26、hostcreds 23／23）、check 74、race-cover 270（apa-buyer 81.7％、buyer 92.9％）、verify-b1-01 43、guards、evidence-import-tests、mutation 346 秒——**248／248 突變全數被殺**，#248（拿掉停用內建 agents 的變數）由 `TestArgvAndEnv|TestCLIEnvReachesChild` 抓到；隔離自檢 17 項 ok。容器內 `TestLiveOptIn`、`TestVersion` 都通過。證據：`artifacts/verify-container/runs/418f0de1506d-20261007T160539Z/`、`artifacts/b1-04/container-418f0de-attempt2.log`。
  - `2593487` 只改註解與文件，未另跑容器。
- **仍須 Aaron 另行核准**：L1-b（plugin 來源確認方式；若需參數，另一個實作停止點）、L1-c（一次 C0′ 觀測）、L1-d（階段 B 與認證接受值）、L1-e（一次階段 C）。B1-04 仍 InProgress；未標 Done、未合併 main。

## 36 驗收紀錄：L1-a 於 `418f0de` 通過限定範圍技術 review（2026-10-08）

- **結論**（Codex）：L1-a 於 `418f0de` 通過限定範圍技術 review，切片關閉；不需要再跑容器。
- **Codex 確認**：
  - 完整容器七步全部 exit 0；
  - 248／248 突變被抓到，#248 是測試斷言失敗，不是編譯失敗；
  - 17 項隔離檢查通過，證據 manifest 雜湊核對通過；
  - `418f0de` 之後的差異（`2593487`、`b58f163` 等）只有文件與測試註解，沒有產品邏輯變更。
- **證據歸屬**：Codex 前輪獨立重跑定向 race 測試；完整容器（`artifacts/verify-container/runs/418f0de1506d-20261007T160539Z/`）由 Claude Code 執行、Codex 本輪核對紀錄，未重跑。
- **範圍**：只證明固定環境變數的傳遞與 fake 回歸，**不證明真實 CLI 的 agents 已清空**（那是 L1-c）。init 閘門、認證接受清單、plugin 參數都未變。
- **下一步**：L1-b（plugin 來源釐清）仍需 Aaron 另行核准。B1-04 仍 InProgress；未標 Done、未合併 main。

## 37 L1-b 資料釐清：受管設定檔讀取結果（2026-10-08；Claude Code 執行，待 Codex review）

- **依據**：Aaron 10/8 核准 L1-b 的資料釐清部分：只讀第 32.5 節第 4 點核准的 plugin 相關鍵名與值，不輸出整份設定或憑證；資料不足時列為未確認，不擴大讀取。
- **讀取範圍**（以只輸出核准欄位的腳本執行，`python3 -I`）：
  - `/Library/Application Support/ClaudeCode/managed-settings.json`：`enabledPlugins` 的鍵名與值、`syncClaudeAiPlugins`、`strictPluginOnlyCustomization`、`agents` 相關鍵是否存在；
  - `/Library/Application Support/ClaudeCode/managed-settings.d/`：檔名與每個檔案的相同欄位。
  - **未讀**：第 32.5 節第 4 點的第三項 macOS 設定描述檔 `com.anthropic.claudecode`（該項明載「是否查看、用什麼方式查看，由 Aaron 決定」，本次核准沒有包含）；也沒有讀 HOME、`~/.claude`、keychain 或其他設定。
- **結果**：兩個路徑都**不存在**。對這兩個確切路徑做 `stat`，在沙箱內外都得到 `FileNotFoundError`（errno 2），不是權限問題。沒有列出該目錄的其他內容。

### 37.1 兩個 plugins 的來源：**未確認**

- **本次查詢時，兩個指定路徑不存在**。這不能證明先前取得 init（10/4）時它們也不存在，所以不能據此排除檔案型受管設定是當時的來源（10/8 依 Codex 核對修正用語）。
- **與既有紀錄的落差**：第 32.3 節記錄 Aaron 表示「受管設定：有」。若屬實，來源只能是本次沒有讀的管道，例如 macOS 設定描述檔；依官方文件也可能有其他受管設定的提供方式（本次未查證，不列為依據）。
- **仍未排除的來源**（依第 32.2、32.3 節的文件整理，都未確認）：
  - macOS 設定描述檔（未讀）；
  - `~/.claude/skills/` 下帶 manifest 的 skills-dir plugin（`--restricted` 是否影響它，文件未說明）；
  - 其他文件列出的來源：依文件，`--restricted` 不讀 user／project／local 設定，固定的 `CLAUDE_CODE_DISABLE_NONESSENTIAL_TRAFFIC=1` 使 synced plugins 不載入，工作目錄是空的暫存目錄、未傳 `CLAUDE_CODE_PLUGIN_DIRS`——這些是依文件推論的排除，未在 2.1.289 實際驗證。
- **能確認身分的方式**（都需另行核准，本次未做）：
  1. Aaron 決定是否查看 macOS 設定描述檔、用什麼方式；
  2. Aaron 提供只含名稱的 plugin 摘要；
  3. L1-c 的一次 C0′ 觀測：第 32.6 節的身分投影會給出每個 plugin 的名稱長度與路徑分類（synced／cache／其他 plugins 目錄／skills／工作目錄／其他），可以區分 skills-dir 與 plugins 目錄，但仍不是來源認證；名稱沒有候選依據時只顯示 `other`。

### 37.2 是否有只影響 demo 該次啟動的停用方式

依第 32.2 節的官方文件（10/4 查證；本次沒有重新查證，也沒有在 2.1.289 實測）：

| 方式 | 只影響該次啟動 | 能否用於本專案 |
|---|---|---|
| `--settings '{"enabledPlugins":{"<id>":false}}'`（flag 來源） | 是 | 已知候選（未完整查證或實測）；**要先知道 plugin id**；若該 plugin 由受管設定強制啟用（`true`），flag 來源不能覆寫 |
| `--safe-mode` | 是 | 不載入 plugins、MCP servers、custom agents；是否也停用 `--mcp-config` 指定的 apa bridge **未確認**，若停用則 demo 無法運作 |
| `--bare` | 是 | 需要 `ANTHROPIC_API_KEY` 或 `apiKeyHelper`，不讀 OAuth 或 keychain，**不能用本人訂閱登入** |
| `--setting-sources` 不含 `user` | 是 | 只影響 synced plugins；固定的非必要流量變數依文件已使其不載入 |

**結論**：依目前已查閱的文件，已知候選方式是 `--settings` 的 flag 來源（只影響該次啟動、保留 MCP）；尚未完整查證或實測，不能宣稱它是唯一的方式。它需要 plugin id，且依文件對受管設定強制啟用的 plugin 無效（10/8 依 Codex 核對修正用語）。目前 id 與來源都未確認，所以**還不能決定 L1-b 的實作內容**；本次也沒有加任何參數。

### 37.3 下一步（待 Aaron 決定，均未開始）

- 是否查看 macOS 設定描述檔（以及方式），或由 Aaron 提供 plugin 名稱摘要；
- 或者跳到 L1-c，以一次 C0′ 觀測取得名稱長度與路徑分類（需執行核准）；
- 取得 id 後，若可在 session 層停用，再另開 L1-b 的實作與 fake 驗證停止點。

本次沒有修改設定、程式、plugin 參數或認證接受清單；沒有執行真實 CLI、登入檢查或 C0′。B1-04 仍 InProgress。

### 37.4 核對紀錄（2026-10-08）

- Codex 核對 `9343e81`（只核對文件，未重新讀取設定）：**L1-b 的資料釐清可記為完成，結果是「來源未確認」，還不能決定停用參數。**
- 依核對修正兩處用語（37.1、37.2）：「兩個指定路徑在本次查詢時不存在」不能證明 10/4 取得 init 時也不存在；`--settings` 是依目前已查閱文件的已知候選，不宣稱唯一。
- 下一步建議（Codex）：先由 Aaron 提供 plugin 名稱摘要（名稱／完整 ID；介面有顯示時附來源／marketplace 與是否啟用），不貼完整設定、路徑、token 或登入資料；名稱先當作候選，不直接認定就是 init 的那兩個。不擴大到設定描述檔調查或再跑 C0′；不修改設定、不放寬閘門、不跑容器。

### 37.5 候選名稱：pixel-office 已保存的 CLI 能力紀錄（2026-10-08；Claude Code 執行，待 Codex review）

- **依據**：Aaron 10/8 指示：從 pixel-office 既有、已保存的 CLI 能力紀錄，只取 plugins 名稱及該筆紀錄的時間／引擎來源；不重新啟動 Agent，不輸出原始 init、設定或憑證；未保存就直接回報。
- **怎麼找到的**：只讀 pixel-office 程式定位紀錄（`backend/main.py`：Claude Code CLI 的 `system/init` 到達時把 plugins 的名稱存進 `cli_caps`，約 L3720–3738；存檔鍵 `cli_caps`，L6588）。之後以只輸出指定欄位的腳本讀 `backend/office_state.json` 的 `cli_caps.plugins` 與 `cli_caps.at`，沒有輸出其他內容，也沒有讀 pixel-office 的設定、憑證或 HOME。
- **結果**：
  - plugins（3 個，只有名稱）：`cc-plugin-agents-md`、`cc-plugin-telemetry`、`cc-plugin-plugin-authoring`。
  - 引擎來源：Claude Code CLI 開工時自己回報的 init（pixel-office 程式中 `cli_caps` 只由 CLI 的 init 寫入）。
  - 時間：紀錄本身的 `at` 只有時分 `12:00`、沒有日期；`office_state.json` 檔案最後存檔時間為 2026-10-08 00:38:40（本機時間），那是整份狀態檔的存檔時間，不是這筆能力紀錄的取得時間。紀錄中的 CLI 版本未保存。
- **限制，只能當候選**：
  - pixel-office 啟動 CLI 的參數、設定來源與工作目錄可能和 APA 不同（APA 用 `--restricted`、固定環境與空的暫存工作目錄）；這是那邊的環境回報的清單，不能直接認定就是 APA 10/4 C0′ init 列出的那兩個。
  - 數量不同：這裡 3 個，APA 的 C0′ init 是 2 個，所以至少有一個不對應。
  - 只有名稱，沒有 marketplace 或來源；不能據此判斷是否由受管設定強制啟用。
- **對 L1-b 的影響**：有了候選 id 的名稱部分，但 `--settings` 的 `enabledPlugins` 需要完整 id（可能含 `@<marketplace>` 等後綴，第 32.2 節第 4 點），而且仍不知道哪兩個、來源為何。**仍不能決定停用參數**；本次沒有修改任何設定、程式、plugin 參數或認證清單，也沒有執行真實 CLI 或 C0′。

### 37.6 核對紀錄與建議的下一步（2026-10-08）

- Codex 核對 `d8ec510`（只核對文件與 probe 程式，未重新讀取 pixel-office 的狀態資料）：候選名稱紀錄可接受，沒有阻擋問題；**L1-b 的來源判斷仍是「未確認」**。
- 建議的下一步（**提案，待 Aaron 核准，未開始**）：
  1. **probe 顯示候選修正＋fake 測試**：把 37.5 的三個名稱加入 probe 的 plugins 顯示候選清單；只有完全相符才顯示名稱，其餘維持 `other`；不新增原始路徑或設定內容的輸出；正式閘門、認證清單與啟動參數全部不變；測試後交 Codex review。
  2. 通過後，另行核准一次**診斷型 C0′**：觀測 APA 實際列出哪些候選、路徑分類，以及 L1-a 之後的 agents 清單；目標是辨識，不預設 plugins 已清空，也不等於 L1-c 的清空條件通過；結果仍為 `other` 就停止回報，不擴大收集或重試。
- 顯示名稱不等於允許執行；名稱吻合也不能單靠它認定來源或組出停用 id。

## 38 L1-b 第一段：probe 顯示候選修正＋fake 測試（2026-10-08；Claude Code 執行；於 `e99f389` 通過限定範圍技術 review，見 38.1）

- **依據**：Aaron 10/8 核准「probe 顯示候選修正＋fake 測試」：只把 `cc-plugin-agents-md`、`cc-plugin-telemetry`、`cc-plugin-plugin-authoring` 加入顯示候選清單，完全相符才顯示，其他維持 `other`。
- **不做**：不改認證清單或啟動參數；不讀設定與憑證；不執行真實 CLI 或 C0′。診斷型 C0′ 另行核准。
- **程式**（`e99f389`）：`internal/buyer/probe.go` 的 `pluginDisplayCandidates` 由空清單改為上述三個名稱；比對沿用既有的 `pick`（`slices.Contains`，完全相符）。註解寫明這是另一個環境的候選，不是已確認的來源或 id；顯示清單不是接受清單。路徑仍只輸出分類。
- **測試**：
  - `TestProbePluginCandidates`：三個名稱完全相符時顯示原名；近似或未知名稱——加 `@marketplace` 後綴、大小寫不同、尾端空白、候選的前綴（`cc-plugin-agents`）、以候選結尾的名稱、未知名稱——都只顯示 `other` 與長度；以標記字串檢查這些近似名稱與原始路徑（含 HOME）都不會出現在結果或 trace 中，路徑只剩分類（`home_plugins_cache`、`home_skills`、`other`、`relative`）。
  - `TestHostInitGate` 新增兩案：plugins 恰為候選名稱（一個、三個）時，閘門仍以 `plugins` 拒絕。
  - 既有的 `TestProbeEntries`、`TestProbeNoLeak` 照舊通過（其他 plugin 名稱仍為 `other`）。
  - 突變 #249（清空候選清單）、#250（名稱改為前綴比對）：提交前預檢都由 `TestProbePluginCandidates` 的斷言抓到，不是編譯失敗。共 250 項。
- **本機驗證**（Claude Code 執行，待 Codex review；`e99f389`，`artifacts/b1-04/e99f389777ad-20261007T164822Z/`）：make check、check-contracts、`go test -race -cover ./... -count=1`、B1-01、20 項證據匯入測試全部 exit 0；覆蓋率 buyer 92.9%、apa-buyer 81.7%。這次本機沒有出現第 34.2 節的計時失敗。
- **容器完整驗證**（Claude Code 執行，待 Codex 核對紀錄；`e99f389`，斷網容器，映像 `sha256:78258368…b6d0`）：overall exit 0；七步全過（check-contracts 17 秒、check 79、race-cover 277、verify-b1-01 42、guards、evidence-import-tests、mutation 335 秒）；**250／250 突變全數被殺**，#249、#250 由 `TestProbePluginCandidates` 抓到，#248 照舊；隔離自檢 17 項 ok。證據：`artifacts/verify-container/runs/e99f389777ad-20261007T165410Z/`、`artifacts/b1-04/container-e99f389.log`。
- **範圍**：顯示名稱不等於允許執行；名稱吻合也不能單靠它認定來源或組出停用 id。來源仍未確認；L1-b 的停用參數仍未決定。B1-04 仍 InProgress。

### 38.1 驗收紀錄：probe 顯示候選修正於 `e99f389` 通過限定範圍技術 review（2026-10-08）

- **結論**：probe 顯示候選修正於 `e99f389` 通過限定範圍技術 review，**只關閉這項修正**。
- **證據歸屬**（10/8 依 Codex 提供的內容補正；原寫「轉交指示沒有列出 Codex 自行重跑的項目」）：
  - **Codex 獨立執行**：以 `e99f389` 的暫存匯出副本為基準，本機定向 race 測試各三輪，全部通過——`GOPROXY=off GOSUMDB=off GOTOOLCHAIN=local go test -race -p 1 ./internal/buyer -run '^(TestProbePluginCandidates|TestProbeEntries|TestProbeNoLeak|TestProbeNullStrings|TestPathClass|TestHostInitGate)$' -count=3 -timeout=180s`，exit 0，套件耗時 15.905 秒。只用 fake CLI，沒有執行真實 CLI。這是本機定向重跑，不是容器驗證；結果在 Codex 的工具輸出中，未另存 log，所以這裡沒有證據路徑。
  - **Claude Code 執行、Codex 核對紀錄**：`e99f389` 的斷網容器七步、250／250 突變、17 項隔離檢查（`artifacts/verify-container/runs/e99f389777ad-20261007T165410Z/`）。Codex 另外核對 manifest 的 278 個項目，雜湊全部相符；並確認 #249、#250 由測試斷言抓到，不是編譯失敗。Codex 沒有重跑完整容器。
  - **Claude Code 執行，維持原歸屬**：本機 make check、check-contracts、race／coverage、B1-01、證據匯入（`artifacts/b1-04/e99f389777ad-20261007T164822Z/`）；這些不寫成 Codex 已全部核對。
  - 本次同步沒有重跑測試。
- **範圍**：**L1-b 的 plugin 來源仍未確認。** 名稱相符不代表來源可信、完整 plugin ID 已確認，或允許執行。
- **未做**：沒有修改停用參數或認證清單；沒有執行診斷型 C0′。真實執行仍需 Aaron 另行核准；B1-04 仍 InProgress，未標 Done、未合併 main。

## 39 診斷型 C0′ 執行確認清單（2026-10-08 整理；待 Codex review 與 Aaron 另行核准；尚未執行）

> **狀態**：診斷型 C0′ 執行確認清單已整理，待 Codex review 與 Aaron 另行核准；尚未執行。
>
> **10/8 修訂**：plugin 來源調查暫緩後，診斷重點與結果分支以第 39.7 節為準：39.1 第 1、2 項（名稱、路徑分類）改為既有輸出的附帶資訊；39.5「plugins／agents 非空」的處理改依 39.7.3。39.2～39.4 的執行邊界不變。
>
> **10/8 驗收**：清單於 `d12adcc` 通過限定文件 review，R1／R2 關閉（第 39.10 節）；真實執行仍未核准。
>
> **10/8 R1／R2 修訂**：39.3 命令改為 bash 失敗即停止流程、stdout 改存 `stdout.txt`；39.4、39.7.3 的 `not_started` 與 `orphaned_output` 說明更正。原文與對照見第 39.8 節。

- **依據**：vault〈Claude-Code-B1-04-診斷型C0prime執行清單整理指示-1395a1f-20261008〉。本節與該筆記都**不構成真實執行核准**。
- **開工核對**：分支 `b1-04-live`，HEAD `1395a1f`；工作區只有未追蹤的 `demo`（不動）。程式基準是 `e99f389`；其後 `0958725`、`70bde1e`、`1395a1f` 只改 `docs/cli-poc.md`（`git diff --stat e99f389 1395a1f` 只有這個檔案）。
- **現行行為依原始碼核對，未執行任何 CLI**：`cmd/apa-buyer/main.go` 的 `probe`、`checkLive`、`versionLimits`；`internal/buyer/probe.go` 的 `ProbeTimeout`、`Probe`、`watch`、`observe`、`ProbeResult`；`internal/buyer/runner.go` 的 `DefaultLimits`、`Outcome`、`Input*`；`internal/buyer/host.go` 的 `CLIEnv`／`fixedEnv`。
- 現況：L1-a 已通過限定範圍技術 review；probe 顯示候選修正於 `e99f389` 通過。**L1-b 的 plugin 來源仍未確認**。本清單不重開已關閉的修正，也不代表 L1-b 全部完成。

### 39.1 診斷目的（三項分開）

1. **plugin 名稱**：這次 APA 啟動的 init 所回報的 plugins，是否**完全符合**三個顯示候選 `cc-plugin-agents-md`、`cc-plugin-telemetry`、`cc-plugin-plugin-authoring`（`plugins_entries[].name`；不符者只會是 `other`＋長度）。
2. **plugin 路徑分類**：`plugins_entries[].path` 的既有分類（`home_plugins_synced`／`home_plugins_cache`／`home_plugins_other`／`home_skills`／`work_dir`／`other`／`relative`／`absent`／`not_string`）；不收集原始路徑。
3. **agents**：在 L1-a 的 `CLAUDE_AGENT_SDK_DISABLE_BUILTIN_AGENTS=1` 之下，`agents_count` 與 `agents_entries` 是否為空。probe 的 CLI 環境經 `CLIEnv`，含這個固定變數。

這是**診斷，不是正式 live 放行驗收**：
- plugins 或 agents 非空，也是有效的診斷結果。
- 名稱相符不證明來源、完整 plugin ID 或可信程度；路徑分類只是對 CLI 回報文字的分類，不是來源認證。
- 不因觀測結果自動修改閘門、認證接受清單或停用參數。
- 不直接當成路線 L1-c「確認 plugins／agents 清空」的驗收通過；若結果可支持後續判斷，仍須另行 review。

### 39.2 待 Aaron 核准的執行範圍（未勾選，待核准）

- [ ] 只核准**一次**診斷型 C0′ 執行嘗試。版本檢查失敗、解析錯誤或逾時，都停止，不重試。
- [ ] CLI 路徑 `/Users/mac/.local/bin/claude`；程式釘選 `2.1.293 (Claude Code)`（`cmd/apa-buyer/main.go` 的 `approvedCLIVersion`；10/8 由 `2.1.289` 更新，第 40 節）。本輪不執行版本查詢；執行時命令會先自行檢查，不符就停止，不自行更新 CLI 或修改釘選。
- [ ] 沿用既有的程序層 `DISABLE_AUTOUPDATER=1`（`fixedEnv`），不改全域設定。
- [ ] 只送一次固定訊息 `Reply only OK.`（`OneMessageText`）；依程式記錄實際寫入結果 `input_status` 與位元組數 `stdin_bytes_written`。95 位元組是完整封裝 `input_message_bytes` 的預期大小，**不可代替實際寫入量**。
- [ ] 一則訊息不等於一次模型請求；可能產生用量，無法確認時記為 `unknown`（`usage`）；停止 CLI 不能撤回已送出的請求。
- [ ] CLI 會使用既有 HOME、網路與登入環境（含 `~/.claude` 與 keychain）；這**不是作業系統沙箱**（第 7.5 節）。Claude Code 不另外讀取設定、憑證或 keychain 內容。
- [ ] 不開 Store、不建 task、不執行業務工具（`business_calls_executed` 必須為 0），不進入階段 C。

### 39.3 命令（僅供未來核准後使用；本輪不得執行，包括不建置、不查詢真實 CLI 版本）

適用 shell：**bash**（macOS 內建 3.2 即可）。把下列內容存成一個檔案，以 `bash 檔名` 整個執行；**不要逐行貼到互動 shell**（zsh 或互動 bash 中，`exit` 會關掉終端機，且貼上時不保證停在失敗處）。

```bash
#!/bin/bash
# 僅供未來核准後使用；本輪不得執行。
# 前置步驟任一失敗 → die：記錄原因、刪除自己建立的暫存目錄，以 exit 2 結束，不會到達 probe。
# probe 只在檔案末端呼叫一次；它的非零退出碼是診斷結果，照樣保存，不重試。
set -u
REPO=/Users/mac/Documents/OpenSourceProjects/agent-payment-authority
CLI=/Users/mac/.local/bin/claude
PRODUCT_BASE=40bd2630cbae0adf552e1dd54ae87ee4c8022cd4   # 已 review 的產品基準（10/8 方案 A；之前為 bd29a4e、e99f389）
EV_OWNED=; WT_MADE=
die() {
  echo "STOP（probe 未啟動）：$*" >&2
  if [ -n "$EV_OWNED" ]; then echo "$*" > "$EV/precheck-stop.txt"; fi   # 只寫入本次自己建立的證據目錄
  if [ -n "$WT_MADE" ]; then rm -rf "$WT" || echo "暫存目錄未能移除：$WT" >&2; fi   # 只刪本次 mktemp 建立的目錄
  exit 2
}

# 1. 工作目錄與分支
cd "$REPO" || die "無法進入 $REPO"
BR=$(git branch --show-current) || die "git branch 失敗"
[ "$BR" = b1-04-live ] || die "分支是「${BR}」，不是 b1-04-live"

# 2. 工作區：只容許未追蹤的 demo（或完全乾淨）
ST=$(git status --porcelain --untracked-files=all) || die "git status 失敗"
[ -z "$ST" ] || [ "$ST" = "?? demo" ] || die "工作區有 demo 以外的差異"

# 3. 執行基準：產品基準必須是祖先，且之後只能有文件差異
BASE=$(git rev-parse --verify 'HEAD^{commit}') || die "git rev-parse 失敗"
git merge-base --is-ancestor "$PRODUCT_BASE" "$BASE" || die "$PRODUCT_BASE 不是 $BASE 的祖先，或 git 失敗"
DIFF=$(git diff --name-only "$PRODUCT_BASE" "$BASE" -- . ':(exclude)docs' ':(exclude)README.md') || die "git diff 失敗"
[ -z "$DIFF" ] || die "$PRODUCT_BASE 之後有非文件差異"

# 4. 證據目錄（不加 -p：已存在就停止，不覆寫）、全新暫存目錄中的一般本機 clone 與建置
mkdir -p "$REPO/artifacts/b1-04" || die "無法建立 artifacts/b1-04"
EV="$REPO/artifacts/b1-04/c0prime-diag-$(date -u +%Y%m%dT%H%M%SZ)"
mkdir "$EV" || die "證據目錄已存在或無法建立：$EV"
EV_OWNED=1
echo "$BASE" > "$EV/commit.txt" || die "無法寫入 commit.txt"
WT=$(mktemp -d "${TMPDIR:-/tmp}/apa-c0p.XXXXXX") || die "mktemp 失敗"
WT_MADE=1
git clone --quiet --no-hardlinks --no-checkout "$REPO" "$WT/src" || die "本機 clone 失敗"   # 本機路徑，不經網路；不改原工作區
git -C "$WT/src" checkout --quiet --detach "$BASE" || die "checkout $BASE 失敗"
[ -d "$WT/src/.git" ] || die "clone 的 .git 不是目錄"
[ "$(git -C "$WT/src" rev-parse --verify 'HEAD^{commit}')" = "$BASE" ] || die "clone 的 HEAD 不是 $BASE"
(cd "$WT/src" && go build -o "$WT/apa-buyer" ./cmd/apa-buyer) || die "建置失敗"   # $WT 是新建的空目錄，不可能拿到舊二進位
[ -x "$WT/apa-buyer" ] || die "建置後沒有可執行檔"

# 5. 二進位 VCS：revision 必須等於 BASE，modified 必須為 false；欄位缺失、重複或不符都停止
VM=$(go version -m "$WT/apa-buyer") || die "go version -m 失敗"
printf '%s\n' "$VM" | awk '$1 == "build"' > "$EV/go-version-build.txt" || die "無法寫入 go-version-build.txt"   # 原始 build 行，供核對解析
REV=$(printf '%s\n' "$VM" | awk '$1 == "build" && $2 ~ /^vcs\.revision=/ { sub(/^vcs\.revision=/, "", $2); print $2 }')
MOD=$(printf '%s\n' "$VM" | awk '$1 == "build" && $2 ~ /^vcs\.modified=/ { sub(/^vcs\.modified=/, "", $2); print $2 }')
printf 'vcs.revision=%s\nvcs.modified=%s\n' "$REV" "$MOD" > "$EV/binary-vcs.txt" || die "無法寫入 binary-vcs.txt"
[ "$REV" = "$BASE" ] || die "vcs.revision 缺失或不是 $BASE"
[ "$MOD" = false ] || die "vcs.modified 缺失或不是 false"

# 6. 唯一一次 probe（版本檢查由 apa-buyer 自己做，不符時 exit 1、沒有 JSON）
date -u +%FT%TZ > "$EV/started-utc.txt" || die "無法寫入 started-utc.txt"
APA_LIVE_CLI=true "$WT/apa-buyer" probe-init \
  --cli "$CLI" \
  --send-one-message \
  --trace-dir "$EV/runs" > "$EV/stdout.txt" 2> "$EV/apa-buyer-error.txt"
PROBE_EXIT=$?

# 7. 不論 PROBE_EXIT 為何都走到這裡；之後的命令不改變 PROBE_EXIT，也不再呼叫 probe。
#    證據保存是否成功另外記在 SAVED；失敗不呼叫 die（probe 已啟動），也不重試 probe。
SAVED=1
echo "exit=$PROBE_EXIT" > "$EV/exit-code.txt" || SAVED=
date -u +%FT%TZ > "$EV/ended-utc.txt" || SAVED=
rm -rf "$WT" || echo "暫存目錄未能移除：$WT" >&2   # 清理照常嘗試
if [ -z "$SAVED" ]; then
  echo "執行已嘗試，證據不完整：probe exit=${PROBE_EXIT}；exit-code.txt 或 ended-utc.txt 未能寫入；目錄：$EV" >&2
  exit 5
fi
echo "probe exit=${PROBE_EXIT}；證據：${EV}"
exit "$PROBE_EXIT"
```

- **每條失敗路徑（依閱讀核對，本輪未執行、未做語法檢查）**：第 1～6 步的每個命令與條件都接 `|| die`；`die` 以 `exit 2` 結束整個 bash 程序，之後的行都不會執行。probe 只在第 6 步出現一次，在它之前沒有任何不經判斷就繼續的步驟。`set -u` 只防未定義變數，不代替上述條件（沒有用 `set -e`，避免 probe 的非零退出碼中斷第 7 步的保存）。
- **退出碼**：`exit 2` 只來自 `die`（前置停止，probe 未啟動，若證據目錄已建立則有 `precheck-stop.txt`）；`exit 5` 只來自第 7 步（**probe 已嘗試，但 `exit-code.txt` 或 `ended-utc.txt` 未能寫入，證據不完整**；probe 的退出碼印在 stderr 那一行）；其餘 `0`／`1`／`4` 是 probe 自己的退出碼（39.4），只在第 7 步保存都成功時回傳。`PROBE_EXIT` 在 probe 結束後立即保存；「probe 退出碼」與「證據是否保存成功」（`SAVED`）分開記錄，寫檔、時間、移除工作樹都不會改變 `PROBE_EXIT`。保存失敗不呼叫 `die`（它會誤寫「probe 未啟動」），不重試 probe，工作樹清理照常嘗試。
- **工作區規則**：`git status --porcelain --untracked-files=all` 只容許空輸出或恰為 `?? demo`；被 `.gitignore` 忽略的檔案（例如 `artifacts/`、`var/`）不算在內。

- **為什麼用暫存目錄中的一般本機 clone**（10/8 修訂，原為 `git worktree add`）：`git worktree add` 的連結工作樹中 `.git` 是檔案；10/8 的執行在該流程下建出的二進位讀不到 VCS 欄位（第 39.11 節）。改為 `git clone --no-hardlinks --no-checkout` 原 repo 的本機路徑，再 `checkout --detach` 執行基準：`.git` 是目錄，不經網路、不在原 repo 新增工作樹，也不改原工作區。VCS 嚴格檢查不變：`vcs.revision` 必須等於基準、`vcs.modified` 必須為 `false`，空值或缺失都停止，不以手動填寫的 SHA 代替。此流程的建置前置驗證見第 39.12 節。
- **原理由（保留）**：先前幾次真實執行都要求工作區乾淨（第 27、30 節），現在 repo 根目錄有未追蹤的 `demo`（不得刪除或搬動）。Go 判斷 `vcs.modified` 時，依其實作也會把未追蹤檔算進去，所以直接在 repo 建置預期會得到 `vcs.modified=true`。這是依 Go 行為的推論，本輪沒有建置驗證；若執行時仍不是 `false`，就停止。
- `stdout.txt` 是 apa-buyer 的 stdout 原樣保存，**不是單一 JSON 文件**：第一行是 Host 的模式提示（`PROBE MODE (C0′): …`，含 CLI 路徑），之後才是縮排的 JSON 白名單投影（與 trace 相同的投影）；版本檢查等啟動前拒絕時兩部分都沒有；`Probe` 本身回傳錯誤（例如 trace 無法寫入）時只有提示行。不要把整份檔案直接當 JSON 解析；trace（`runs/probe-*.jsonl`）維持原格式。`apa-buyer-error.txt` 只有 apa-buyer 自己的錯誤行，不含 CLI 的 stdout／stderr 文字。
- **時間上限**（依原始碼）：
  - 版本檢查：`versionLimits` 最多 5 秒，加 1 秒清理寬限；**不算在 init 等待內**。
  - init 等待：`ProbeTimeout` 30 秒，從 CLI 程序啟動起算（`Limits.Total`）。
  - 停止後清理：最多兩個寬限期，`DefaultLimits.Grace` 3 秒，約 6 秒；不算在 init 等待內。
  - 不為取得更多資訊延長等待，也不等模型回答。

### 39.4 結果怎麼讀（exit code 依 `probe`；`3` 只屬 C0，這裡不會出現）

| exit | status／code | 意思 |
|---|---|---|
| `0` | `observed`／`init_observed` | 取得 init（版本欄位為核准值），只代表取得觀測資料，**不代表正式閘門通過** |
| `0` | `observed`／`init_observed`，`input_status=not_started` | init 在訊息寫入前就先到，probe 停止、不再寫入；**實際寫入 0 位元組仍是有效觀測**（`probe.go` 最終判定只有 `failed`／`partial` 會排除觀測） |
| `4` | `no_init_after_message` | 訊息完整送出（`input_status=complete`），30 秒內沒有 init |
| `1`（有 JSON） | `failed`／固定代碼 | 其他失敗，回報固定代碼（見下） |
| `1`（無 JSON，已確認的前置拒絕） | — | `stdout.txt` **沒有**模式提示行，且錯誤行是 `main.go` `probe` 在印出提示前的拒絕訊息：`APA_LIVE_CLI`、`--cli` 非絕對路徑、多餘參數、版本檢查失敗或逾時、版本不符、版本檢查前後取消（訊息含 `nothing was started` 或 `the probe was not started`）。probe 會話沒有啟動、沒有 trace。若流程已進入版本檢查，可能已執行 CLI 版本查詢；更早的拒絕（`APA_LIVE_CLI`、`--cli`、多餘參數、版本檢查前取消）不會執行 |
| `1`（無 JSON，probe 或輸出保存失敗） | — | `stdout.txt` 有模式提示行但沒有 JSON，或無法歸入上一列（例如 `Probe` 回傳錯誤、trace 無法寫入、JSON 輸出失敗）。**可能已執行 CLI 並送出訊息**；不能據此宣稱零輸入、零用量或沒有 trace。照實保存 `stdout.txt`、錯誤行與 `runs/` 內實際存在的檔案，交 Codex 判斷 |

`exit 1` 的固定代碼（依原始碼，任一出現即停止）：
- 寫入：
  - `input_failed`、`input_partial`：寫入失敗或只寫一部分；**即使同時取得 init，也判失敗、不採用觀測**。
  - `input_not_started`：只在**沒有取得 init**（逾時）且訊息未開始寫入時出現。`not_started` 本身不必然失敗：init 先到時是上表的 `observed`、exit `0`。
  - 不要求成功觀測一定寫滿 95 位元組；以 `input_status` 與 `stdin_bytes_written` 照實記錄。
- init：`init_cli_version`（版本欄位不是核准值）、`init_malformed`。
- API：`api_retry_auth`、`api_retry_rate_limit`、`api_retry`（init 前出現 `api_retry`；只記代碼，不記錯誤文字）。
- 訊息：`hook_message`、`unexpected_message`（init 前出現 assistant、user、result 等）。
- 解讀：`ambiguous_json`（成員重複或大小寫折疊後重複）、`parse_error`。
- 程序：`cancelled`、`output_overflow`、`nonzero_exit`、`start_failed`、`not_installed`、`orphaned_output`、`cli_error` 等 runner 結果。

**任何結果都停止**：不追加第二次觀測、不自動改用其他參數、不轉往 C0 重跑或正式購物流程。程序清理沿用既有方式（process group 結束、寬限期後 SIGKILL）；**不保證能回收脫離 process group 的子程序**（第 7.5 節第 4 點）。`orphaned_output` 只是既有管線在等候期限後仍未正常關閉等情況的訊號（例如脫離 group 的程序仍握著 stdout／stderr）；**沒有這個代碼，不代表已證明沒有殘留程序**，本清單也不另做程序掃描。

### 39.5 證據與解讀

- **只保存**：apa-buyer 的 stdout（`stdout.txt`，含模式提示與 JSON 白名單投影）與 trace、exit code、執行 commit、二進位 vcs 資訊、起訖時間。不擴大收集原始 init、未知欄位名稱、原始路徑或 CLI 錯誤文字。
- 若在版本檢查等前置步驟就停止而沒有 trace，**照實記錄不存在**，不補造檔案或觀測資料。
- 未來的證據先標「Claude Code 執行，待 Codex review」，不預寫為已驗收。
- **預先列出的解讀**：
  - 候選名稱相符：只記「名稱相符，來源未確認」。
  - 顯示 `other`：維持未知，不猜名稱或完整 ID。
  - agents 為空：只證明這次條件下回報為空，不外推到其他環境。
  - plugins／agents 非空：保留觀測，交 Codex review；不自動放寬閘門。
  - `apiKeySource`（`auth_source`）為 `none`：不解讀成已確認訂閱或計費方式。
  - 與 pixel-office 的名稱或數量相符：不據此認定兩者設定來源相同。

### 39.6 尚待核准

- 這一次診斷型 C0′ 的執行（39.2 全部項目）；
- 執行後的任何下一步（停用參數、認證接受值、階段 B、L1-c、L1-d、L1-e）都另行決定。

本輪沒有跑測試、容器、真實 CLI、登入檢查或 C0′；沒有修改程式、接受清單或啟動參數。B1-04 仍 InProgress。


### 39.7 修訂：plugin 來源調查暫緩後的診斷重點與結果分支（2026-10-08；待 Codex review 與 Aaron 另行核准；尚未執行）

> plugin 來源調查依 Aaron 決定暫緩，來源未確認保留為已知限制。正式 Gate 維持不變。下一步為整理一次診斷型 C0′ 清單，確認目前 plugins／agents 是否仍非空；待 Codex review 與 Aaron 另行核准，尚未執行。

- **依據**：vault〈Claude-Code-B1-04-plugin來源調查暫緩與診斷清單修訂指示-20261008〉，更新前一份清單整理指示的診斷重點及後續分支。本節**不構成真實執行核准**。
- **開工核對**：分支 `b1-04-live`，HEAD `de06c5a`；工作區只有未追蹤的 `demo`（不動）。程式基準仍是 `e99f389`；其後只改 `docs/cli-poc.md`（`git diff --stat e99f389 de06c5a` 只有這個檔案）。

#### 39.7.1 決策與界線

- **停止追加的調查**：讀取受管設定、macOS 設定描述檔、HOME 或 keychain；追查 plugin 完整 ID、安裝來源或 pixel-office 設定；為查來源而新增 probe 欄位或擴大輸出。
- **兩件事分開記錄**：
  1. 來源調查已暫緩；來源未確認是已知限制，不再是主線開發的前置工作。
  2. 真實 CLI 若仍載入 plugins，現有閘門的執行阻擋仍存在。**暫緩調查不等於阻擋已解決。**
- 這項決策不代表允許未知 plugins、不放寬閘門、不核准真實執行。閘門、認證接受清單與啟動參數維持不變。

#### 39.7.2 診斷目的（取代 39.1 的主次）

沿用既有 probe，不改程式。下一次診斷型 C0′ 只回答：

1. 這次 init 的 plugins 是否為空？
2. 在 `CLAUDE_AGENT_SDK_DISABLE_BUILTIN_AGENTS=1`（L1-a，經 `CLIEnv`）之下，agents 是否為空？
3. 其他既有白名單觀測值是什麼（`stdout.txt` 中 JSON 投影／trace 的其餘欄位，如 `cli_version`、`auth_source`、`permission_mode`、`tools_count`、`mcp_servers_count`、`apa_server_status`、`skills_count`、`extra_members`）？

39.1 的候選名稱與路徑分類照常隨既有輸出保存，但**不追加調查，也不以查明來源為成功條件**。

**「明確為空」的判定**（依原始碼 `observe`／`count`；兩者都須成立）：
- plugins：`fields.plugins` 為 `array`，`plugins_count` 為 `0`，`plugins_entries` 為空。
- agents：`fields.agents` 為 `array`，`agents_count` 為 `0`，`agents_entries` 為空。
- `plugins_count`／`agents_count` 為 `null`（成員缺少或不是陣列），或 `fields.plugins`／`fields.agents` 為 `absent` 或其他型別：記為**未確認**。**缺欄位、型別錯誤或未知值不得解讀成空清單。**

這仍只是取得啟動觀測資料，不是正式 AI 請購或 live 閘門驗收。plugins 或 agents 非空，也是有效的診斷結果。

**plugin 觀測的依據**：以 APA wrapper 當次 runtime 的 init 為準。pixel-office 的名稱只是 probe 的顯示候選，**不作為允許依據**；名稱相符不代表允許執行，也不代表兩邊來源相同。

#### 39.7.3 結果分支

| 結果 | 怎麼辨認（39.4） | 後續處理 |
|---|---|---|
| 取得 init，plugins／agents 均明確為空 | exit `0`，且符合 39.7.2 的判定 | 交 Codex 核對。其他閘門條件（版本、工具、MCP、權限模式、hook、skills 等）仍須確認，再處理階段 B 與認證接受值。**不得直接啟動真實請購。** |
| 取得 init，但 plugins 或 agents 非空 | exit `0`，`plugins_count` 或 `agents_count` 大於 `0` | **記錄執行阻擋並停止。** 另提最小隔離或停用方案；不自動追查來源、不改允許清單、不重試。方案的實作與真實驗證仍須另行核准；在 Aaron 核准方案前，路線文件「來源不明時不先實作 plugin 停用方式」仍適用。 |
| plugins／agents 資料缺失、型別錯誤或無法判定 | exit `0`，但不符合為空、也不是明確非空（如 `count` 為 `null`） | 如實記為未確認，交 Codex 核對；不猜測已清空、不放行。 |
| 版本不符、逾時、解析錯誤或其他失敗 | exit `4`、exit `1`（有 JSON 的固定代碼；無 JSON 時分「已確認的前置拒絕」與「probe 或輸出保存失敗」兩種，見 39.4）、腳本 exit `5`（證據不完整） | 保存既有白名單證據與固定代碼後停止；不重跑、不自行修改版本或參數。`input_failed`／`input_partial` 時即使同時取得 init，也不採用觀測；`not_started` 但 init 先到而成功（exit `0`）的情況屬上面三列，依 plugins／agents 判定。 |

各分支都不自動執行下一次 CLI、不放寬閘門。`auth_source` 為 `none` 仍不能證明登入方式或計費來源，認證決策另行處理；名稱相符不證明來源可信或完整 plugin ID 已確認；路徑分類不是來源認證。

#### 39.7.4 執行邊界（不變）

指示第 5 節列出的條件都已在 39.2～39.5，本次不改：CLI 路徑與 `2.1.293 (Claude Code)` 釘選（10/8 由 `2.1.289` 更新，第 40 節）、不符就停止、本輪不查詢版本（39.2）；一次嘗試、版本檢查失敗也不再執行（39.2、39.4）；只送一次 `Reply only OK.`，記錄 `input_status` 與 `stdin_bytes_written`（39.2）；可能產生用量，一則訊息不等於一次請求，無法確認記 `unknown`（39.2）；沿用既有 HOME、網路、登入環境與程序清理限制（39.2、39.4）；不開 Store、不建 task、不執行業務工具（39.2）；等待上限、停止條件與白名單證據保存（39.3～39.5）；沿用程序層 `DISABLE_AUTOUPDATER=1`，不改全域設定（39.2）。

補明一點：核准後的 CLI 仍可能自行使用登入資料，**不保證沒有背景活動**（第 7.5 節）。

#### 39.7.5 尚待核准

- 本節修訂的 Codex review；
- 一次診斷型 C0′ 的執行（39.2 全部項目），須 Aaron 另行核准；本節、清單 review 與暫緩調查決策都不構成執行核准；
- 執行後的下一步依 39.7.3 分流，各自另行決定：均為空時的其他閘門條件、階段 B 與認證接受值；非空時的最小隔離或停用方案（設計、實作、真實驗證）。

本輪只改文件：沒有跑測試、容器、真實 CLI、版本查詢、登入檢查或 C0′；沒有修改程式、設定、接受清單或啟動參數；沒有讀取受管設定、HOME、keychain 或 pixel-office。L1-b 不標通過或 Done；B1-04 仍 InProgress。

### 39.8 修訂紀錄：依 Codex 對 `5ed66f1` 的 R1／R2（2026-10-08；待 Codex 限定複核；尚未執行）

> 診斷型 C0′ 清單 R1／R2 文件修訂完成，待 Codex 限定複核；真實執行仍待 Aaron 另行核准，尚未執行。

- **依據**：vault〈Claude-Code-B1-04-診斷型C0prime清單R1-R2修訂指示-5ed66f1-20261008〉。Codex 的 review 只閱讀文件、原始碼與 vault 提交，沒有執行測試或真實 CLI；本節不是新一輪測試結果。
- **開工核對**：分支 `b1-04-live`，HEAD `5ed66f1`；工作區只有未追蹤的 `demo`（不動）。`e99f389` 之後仍只有文件提交。
- **核對方式**：只閱讀原始碼（`cmd/apa-buyer/main.go` 的 `main`、`probe`；`internal/buyer/probe.go` 的最終判定；`internal/buyer/runner.go` 的 `stdinWriter.write`、`Orphaned`）。命令沒有執行，沒有建置、沒有建立工作樹，也沒有做 `bash -n` 語法檢查。

| 項目 | 原寫法（`5ed66f1`） | 現行 | 位置 |
|---|---|---|---|
| R1 前置失敗停止 | `git status`、`git diff --stat`、`go version -m … \| grep \| tee` 只印出結果，以註解要求操作者停止；整段貼上時 probe 仍會啟動；各命令失敗處理不一致 | bash 腳本整檔執行；分支、工作區（只容許空或 `?? demo`）、`e99f389` 為祖先且非文件差異為空、證據目錄（不加 `-p`，已存在即停）、暫時工作樹、建置、`vcs.revision`＝基準、`vcs.modified=false` 各有 `\|\| die`；`die` 以 exit 2 結束，不到達 probe；probe 只呼叫一次 | 39.3 |
| R1 probe 退出碼 | `echo "exit=$?" \| tee`；之後的命令可能影響整段結果 | `PROBE_EXIT=$?` 立即保存；不用 `set -e`，非零也照樣寫 `exit-code.txt`、結束時間，再移除工作樹；腳本以 `PROBE_EXIT` 結束，不重試 | 39.3 |
| R2 stdout 格式 | `> result.json`，稱為「白名單投影」 | `> stdout.txt`；說明含 `PROBE MODE` 提示行與其後的 JSON 投影，不能整份當 JSON 解析；trace 格式不變 | 39.3、39.5、39.7.2 |
| R2 `not_started` | 「`input_not_started`（訊息沒有完整寫入；即使同時取得 init，也不採用觀測）」；39.7.3「寫入未完整（`input_*`）時即使同時取得 init，也不採用觀測」 | `failed`／`partial` 即使有 init 也判失敗；`not_started` 時若 init 先到，是 `observed`、exit 0、寫入 0 位元組；`input_not_started` 只在逾時且未取得 init 時出現；不要求寫滿 95 位元組 | 39.4、39.7.3 |
| R2 `orphaned_output` | 「不保證能回收脫離 process group 的子程序，那種情況記為 `orphaned_output`」 | 清理不保證回收；`orphaned_output` 只是管線未正常關閉等情況的訊號，沒有這個代碼不代表沒有殘留程序；不新增程序掃描 | 39.4 |

**不變**：plugin 來源調查暫緩；正式 Gate、認證接受清單與啟動參數；空／非空／未知的分開處理（未知不當作空）；非空時另提方案、設計實作與真實驗證另行核准；結果能否支持 L1-c 由後續 review 判斷；已關閉的 probe 顯示候選修正與 L1-a 不重開。沒有擴大任何權限或真實執行範圍。

本輪只改文件：沒有改產品程式、建置、跑測試或容器、執行命令範例、真實 CLI、版本查詢、登入檢查或 C0′；沒有讀設定或憑證。L1-b 不標通過；B1-04 仍 InProgress。

### 39.9 補正紀錄：依 Codex 對 `ff094f5` 的複核（2026-10-08；待 Codex 複核；尚未執行）

Codex 以文件與原始碼靜態核對 `ff094f5`，接受前置工作區、程式差異、建置與 VCS 判斷，以及 stdout 格式、`not_started`、`orphaned_output` 的修訂；另提兩處小缺口。本節只補這兩處。

| 項目 | 原寫法（`ff094f5`） | 現行 | 位置 |
|---|---|---|---|
| R1 證據保存失敗 | 第 7 步不檢查 `exit-code.txt`、`ended-utc.txt` 的寫入；寫入失敗時仍以 probe 的 `0` 結束並印出證據位置 | 兩個寫入各自 `\|\| SAVED=`；保存失敗時 stderr 印「執行已嘗試，證據不完整」與 probe 退出碼，腳本以 `exit 5` 結束，不回傳成功；不呼叫 `die`、不重試 probe，工作樹清理照常嘗試 | 39.3 第 7 步、退出碼說明 |
| R2 無 JSON 的解讀 | 「`1`（無 JSON）」一律解讀為啟動前被拒 | 分成兩列：已確認的前置拒絕（沒有模式提示行，錯誤行是提示前的拒絕訊息；版本檢查本身仍會執行一次 CLI 版本查詢）；probe 或輸出保存失敗（可能已執行 CLI 並送出訊息，不能宣稱零輸入、零用量或沒有 trace） | 39.4、39.7.3 |

依閱讀核對 `cmd/apa-buyer/main.go`（`main`、`probe`、`checkLive`）與 `internal/buyer/probe.go`（`Probe` 的錯誤回傳、`probeTrace`）。本輪沒有執行腳本、`bash -n`、測試、容器、真實 CLI、版本查詢、登入檢查或 C0′；沒有改產品程式、Gate、接受清單或啟動參數，也沒有調查 plugin 來源。L1-b 不標通過；B1-04 仍 InProgress。

### 39.10 驗收紀錄：診斷型 C0′ 清單於 `d12adcc` 通過限定文件 review（2026-10-08）

> **診斷型 C0′ 清單於 `d12adcc` 通過限定文件 review，R1／R2 關閉。** 真實執行仍待 Aaron 另行核准，尚未執行。

- **證據歸屬**：Codex 對文件與原始碼的**靜態核對**（`5ed66f1` 的 R1／R2、`ff094f5` 的兩處補正、`d12adcc`）。**不是測試通過**：Codex 與 Claude Code 都沒有執行腳本、`bash -n`、測試、容器、真實 CLI 或 C0′。
- **通過範圍**：只是 §39 執行清單的文件內容（前置檢查的失敗停止、probe 只呼叫一次與退出碼保存、證據保存失敗回報、`stdout.txt` 格式、`not_started`、`orphaned_output`、無 JSON 的兩種解讀、結果分支）。不代表診斷已執行、plugins／agents 已清空、L1-b 或 L1-c 通過，也不構成執行核准。
- **同步時的非阻擋文案修正**（Codex 指示）：39.4「已確認的前置拒絕」列，原寫「版本檢查本身會執行一次 CLI 版本查詢」，改為「若流程已進入版本檢查，可能已執行 CLI 版本查詢；更早的拒絕不會執行」。39.9 表格保留 `d12adcc` 時的原文。
- **加註**（39.7.2）：plugin 觀測以 APA wrapper 當次 runtime 的 init 為準；pixel-office 名稱僅為顯示候選，不作為允許依據。
- 本輪只同步文件：沒有重跑驗證、調查 plugin 來源或執行 C0′；沒有改產品程式、Gate、接受清單或啟動參數。L1-b 不標完成；B1-04 仍 InProgress。

### 39.11 執行紀錄：一次診斷型 C0′（2026-10-08；Claude Code 執行，待 Codex review）

> **結果：前置檢查失敗，probe 未啟動。** 腳本在第 5 步（二進位 VCS 核對）以 `die` 停止，exit `2`。依核准不重試、不改參數或腳本。

- **核准**：Aaron 核准依本節清單執行一次診斷型 C0′（清單 review 基準 `d12adcc`）。核准不含第二次嘗試、C0 重跑、登入檢查、停用方案實作或階段 C。
- **開工核對**：分支 `b1-04-live`，HEAD `039dc89`；工作區只有未追蹤的 `demo`。`d12adcc..039dc89` 只改 `docs/cli-poc.md`、`docs/commercial-demo-roadmap.md`；`e99f389` 之後沒有非文件差異。
- **執行方式**：把 §39.3 的 bash 區塊原樣抽出成暫存檔，以 `bash 檔名` 執行一次（在 Claude Code 沙箱外執行，因為核准包含 CLI 使用既有 HOME、網路與登入環境）。沒有修改腳本內容。
- **經過**：第 1～4 步通過（分支、工作區、`e99f389` 祖先與非文件差異、證據目錄、暫時工作樹、建置）。第 5 步 `go version -m` 成功，但 `vcs.revision`、`vcs.modified` 都解析為空，`die "vcs.revision 缺失或不是 039dc896…"`，exit `2`。`die` 已移除暫時工作樹（`git worktree list` 只剩主工作樹）；repo 工作區仍只有 `?? demo`。
- **沒有發生的事**：probe 會話沒有啟動，因此沒有版本檢查、沒有送出 `Reply only OK.`、沒有 trace、`stdout.txt`、`exit-code.txt`、`started-utc.txt`、`ended-utc.txt`。CLI 沒有被執行，不會產生模型用量。這些檔案不存在是照實紀錄，不補造。
- **證據**（`artifacts/b1-04/c0prime-diag-20261007T181018Z/`，`artifacts/` 不進版控）：
  - `commit.txt`：`039dc896e47a7645d70914ccd9be0b0d8d673c77`
  - `binary-vcs.txt`：`vcs.revision=`、`vcs.modified=`（兩者皆空）
  - `precheck-stop.txt`：`vcs.revision 缺失或不是 039dc896e47a7645d70914ccd9be0b0d8d673c77`
  - 建置出的二進位仍留在 `$TMPDIR/apa-c0p.WpXh56/apa-buyer`（`die` 只移除工作樹，不刪這個目錄）；**本輪沒有再檢查它**。
- **plugins／agents**：未取得 init，**未知**；不得解讀為空。依 39.7.3 屬「版本不符、逾時、解析錯誤或其他失敗」一類：保存證據後停止。
- **原因未確認**（只列候選，均未驗證）：
  1. 在 `git worktree add` 建立的連結工作樹中建置時，Go 沒有嵌入 VCS 資訊；
  2. 環境中有 `GOFLAGS=-buildvcs=false` 或類似設定；
  3. §39.3 的 `awk` 欄位解析與實際 `go version -m` 輸出格式不符。
  
  區分這三者需要另行核准的唯讀檢查（例如對留下的二進位執行 `go version -m`、查看 `go env GOFLAGS`），本輪沒有做。

**待決定**：是否核准上述唯讀檢查；依原因修正 §39.3 後須再經 review；任何新的執行嘗試都須 Aaron 另行核准。本輪沒有修改 Gate、認證接受清單、啟動參數或產品程式；L1-b 不標完成；B1-04 仍 InProgress。

### 39.12 建置前置流程修正與驗證（2026-10-08；Claude Code 執行，待 Codex review）

> **結果：PASS。** 修訂後的 §39.3 步驟 1～5 在同一環境建出的二進位，`vcs.revision` 等於指定 commit，`vcs.modified=false`。只建置並以 `go version -m` 讀取產物；**沒有執行產物、真實 Claude CLI 或 C0′**。第 39.11 節的失敗紀錄保留不變。

- **依據**：Aaron 指示先修正並驗證建置前置流程，不重跑整份診斷腳本。
- **§39.3 修訂**（`d61c838`）：
  - 第 4 步：`git worktree add` 改為 `mktemp -d` 全新暫存目錄中的 `git clone --quiet --no-hardlinks --no-checkout "$REPO"`，再 `checkout --detach "$BASE"`。新增兩項檢查：`.git` 必須是目錄，clone 的 HEAD 必須等於基準，不符就 `die`。clone 來源是本機路徑，不經網路；原 repo 不新增工作樹，原工作區不變。
  - 第 5 步：另存 `go version -m` 的原始 `build` 行（`go-version-build.txt`），供核對解析。VCS 嚴格檢查不變：空值、缺失或不符都停止，不以手動填寫的 SHA 代替。
  - 清理：`die` 與第 7 步改為 `rm -rf "$WT"`，只刪本次 `mktemp` 建立的目錄。
- **驗證方式**：把 §39.3 bash 區塊中「`# 6. 唯一一次 probe`」之前的內容（步驟 1～5）原樣抽出。只做兩處更動：證據目錄前綴 `c0prime-diag-` 改為 `build-precheck-`；結尾加上寫 `result.txt`、刪暫存目錄、`exit 0`。抽出的內容不含 probe 呼叫；變數 `CLI` 有定義，但沒有被使用。
  - 以 `bash` 執行一次，在 Claude Code 沙箱外執行，與第 39.11 節的執行環境相同。
- **結果**（`artifacts/b1-04/build-precheck-20261007T181522Z/`，`artifacts/` 不進版控）：
  - `commit.txt`：`d61c838584a059f8421f95b0b6082acbe086fb83`
  - `binary-vcs.txt`：`vcs.revision=d61c838584a059f8421f95b0b6082acbe086fb83`、`vcs.modified=false`
  - `go-version-build.txt`：14 行 `build` 設定，含 `vcs=git`、`vcs.revision`、`vcs.time`、`vcs.modified=false`（格式為 tab 分隔的 `build<TAB>key=value`）
  - `result.txt`：`PASS`；腳本 exit `0`。
  - 暫存目錄已刪除；`git worktree list` 只有主工作樹；repo 工作區仍只有 `?? demo`。
- **對第 39.11 節原因候選的意義**（推論，不是重現驗證）：同一段 `awk` 解析能讀到兩個欄位，排除「`awk` 解析與輸出格式不符」。本次建置未被停用 VCS；沒有保存前次建置環境，因此不能單靠本次結果排除前次環境差異（10/8 依 Codex 修正文案，原寫「也排除了環境 `GOFLAGS` 停用 VCS」）。「連結工作樹中建置未嵌入 VCS」是候選之一，本輪**沒有**在 worktree 中重現，不寫成已證實；也不再重現。
- **留存**：第 39.11 節那次建出的二進位仍在 `$TMPDIR/apa-c0p.WpXh56/`，本輪沒有檢查或刪除。
- **注意**：下次正式執行時，基準是當時的 HEAD（本節之後的文件提交），而不是 `d61c838`。腳本會在當時重新 clone、建置、核對 VCS；本節的 PASS 只證明流程可行，不代替當次的檢查。

**待決定**：Codex review 本節與 §39.3 修訂；任何新的診斷型 C0′ 嘗試都須 Aaron 另行核准。本輪沒有修改 Gate、認證接受清單、啟動參數或產品程式；L1-b 不標完成；B1-04 仍 InProgress。

### 39.13 驗收紀錄：建置前置流程於 `d61c838` 通過限定 review（2026-10-08）

> **建置前置流程以 `d61c838` 為基準，通過 Codex 限定 review；驗證紀錄為 `1c9f03c`。**

- **證據歸屬**：Claude Code 執行建置前置驗證（第 39.12 節）；Codex 核對文件、原始 build 紀錄及解析結果。**Codex 未重新建置或執行腳本。**
- **Codex 確認**：一般本機 clone、指定 commit 與 `.git` 目錄檢查符合要求；清理目標是本次 `mktemp` 建立的暫存目錄；原始 build 紀錄包含正確的 `vcs.revision` 與 `vcs.modified=false`；VCS 嚴格檢查沒有放寬；產品程式未變。
- **非阻擋文案修正**（已同步到 39.12）：「排除 GOFLAGS 停用 VCS」改為「本次建置未被停用 VCS；沒有保存前次建置環境，因此不能單靠本次結果排除前次環境差異」。不再重現前次失敗。
- **現況**：可以進入「另行核准一次診斷型 C0′」的階段。尚未取得新的 runtime 觀測，plugins／agents 仍未知；本次 review 不代替執行核准。
- 本輪只同步紀錄：沒有重跑驗證、建置、真實 CLI 或 C0′；沒有修改 Gate、認證接受清單、啟動參數或產品程式。L1-b 不標完成；B1-04 仍 InProgress。

### 39.14 執行紀錄：第二次核准的診斷型 C0′（2026-10-08；Claude Code 執行，待 Codex review）

> **結果：版本檢查拒絕（已確認的前置拒絕），probe 會話未啟動。** plugins／agents 未取得觀測，仍未知。依核准不重試、不更新版本、不換參數。

- **核准**：Aaron 於 10/8 核准一次診斷型 C0′（「我核准一次診斷型 C0′」）。執行條件沿用前次核准與本節清單。
- **開工核對**：分支 `b1-04-live`，HEAD `9fcd105`；工作區只有 `?? demo`。`d61c838..9fcd105` 只改 `docs/cli-poc.md`；`e99f389` 之後沒有非文件差異。
- **執行方式**：§39.3 bash 區塊原樣抽出，以 `bash` 執行一次。在 Claude Code 沙箱外執行，因為核准包含使用既有 HOME、網路與登入環境。腳本沒有修改。
- **經過**：
  - 步驟 1～5 通過：clone 基準 `9fcd105`；`vcs.revision=9fcd105e3c25cc4a2e14dece3004c87c4d84be3d`、`vcs.modified=false`。
  - 步驟 6 啟動 apa-buyer 一次。apa-buyer 的內建版本檢查拒絕，exit `1`，沒有 JSON。錯誤行（全文）：`apa-buyer: /Users/mac/.local/bin/claude reports a version other than the approved "2.1.289 (Claude Code)"; nothing was started or upgraded`。
  - 步驟 7 保存了 `exit-code.txt`（`exit=1`）與 `ended-utc.txt`，並刪除了暫存目錄。
- **依 39.4 歸類**：「`1`（無 JSON，已確認的前置拒絕）」。`stdout.txt` 為 0 位元組（沒有模式提示行），錯誤行是 `checkLive` 的版本不符訊息。
  - 流程已進入版本檢查，所以 apa-buyer 已執行一次 CLI 版本查詢，**實際回報的版本字串沒有保存**（程式不輸出它）。
  - probe 會話沒有啟動：沒有送出 `Reply only OK.`，沒有 `runs/` trace，不會有對話用量。
- **證據**（`artifacts/b1-04/c0prime-diag-20261007T181935Z/`，`artifacts/` 不進版控）：
  - `commit.txt`、`binary-vcs.txt`、`go-version-build.txt`（含 `vcs.revision`、`vcs.modified=false`）
  - `started-utc.txt`（`2026-10-07T18:19:39Z`）、`ended-utc.txt`（`2026-10-07T18:19:40Z`）
  - `exit-code.txt`（`exit=1`）、`apa-buyer-error.txt`（上述一行）、`stdout.txt`（0 位元組）
  - 沒有 `runs/` 與 `precheck-stop.txt`：照實記錄不存在。
- **腳本缺陷（新發現，未修正）**：證據保存與清理之後，最後一行 `echo "probe exit=$PROBE_EXIT；證據：$EV"` 在 bash 3.2 下報 `PROBE_EXIT…: unbound variable`，`set -u` 使腳本以 `1` 結束，而不是 `exit "$PROBE_EXIT"`。這次 probe 的退出碼也是 `1`，所以數字碰巧相同；`exit-code.txt` 由 probe 退出碼寫入，內容正確。
  - 推測原因（未驗證）：bash 3.2 在目前 locale 下，把緊接在變數名稱後的全形字元 `；` 的位元組當成名稱的一部分。
  - 同類寫法還有兩處：`die "分支是「$BR」…"`、證據不完整分支的 `probe exit=$PROBE_EXIT；…`。
  - 可能的修正是改成 `${BR}`、`${PROBE_EXIT}`，須另經 review；本輪沒有修改腳本。
- **沒有做的事**：沒有另跑版本查詢、沒有更新或重裝 CLI、沒有修改釘選或參數、沒有登入檢查、沒有 C0 重跑或第二次嘗試。

**待決定**：
1. CLI 版本：由 Aaron 確認目前安裝的版本（或核准一次版本查詢），再決定是改回 `2.1.289`，還是評估新版並修改釘選。修改釘選屬於程式變更，須另經設計與 review。
2. §39.3 腳本的變數名稱缺陷修正，須經 review。
3. 任何新的診斷型 C0′ 嘗試，須 Aaron 另行核准。

本輪沒有修改 Gate、認證接受清單、啟動參數或產品程式；L1-b 不標完成；B1-04 仍 InProgress。

### 39.15 §39.3 變數名稱修正與最小分支測試（2026-10-08；待 Codex review）

> 第 39.14 節的失敗紀錄保留不變。本節只修正腳本文字並做最小分支測試；**沒有執行完整診斷腳本、CLI、建置或 C0′，也沒有修改版本釘選。**

- **證據歸屬（分開寫）**：
  - 第 39.14 節的真實執行：Claude Code 執行，Codex 核對紀錄。
  - `unbound variable` 的 bash 最小重現：Codex 獨立執行。
  - 本節的修正與分支測試（含對舊文字的對照）：Claude Code 執行，待 Codex review。
- **CLI 版本**：Aaron 表示目前的 Claude CLI 版本應為 `2.1.291`。這與第 39.14 節「回報版本不是 `2.1.289`」一致。apa-buyer 不輸出實際版本字串，本輪也沒有查詢，所以只記錄為 Aaron 的說明。釘選（`approvedCLIVersion`）沒有修改。
- **修正**（§39.3 bash 區塊，只改這三處）：
  - `die "分支是「$BR」…"` → `"分支是「${BR}」…"`
  - 證據不完整分支 `probe exit=$PROBE_EXIT；…` → `probe exit=${PROBE_EXIT}；…`
  - 最後一行 `echo "probe exit=$PROBE_EXIT；證據：$EV"` → `echo "probe exit=${PROBE_EXIT}；證據：${EV}"`
  - 修正後，區塊中已沒有「`$名稱` 緊接非 ASCII 字元」的寫法（以 `grep '\$[A-Za-z_][A-Za-z0-9_]*[^ -~]'` 核對，無結果）。
- **最小分支測試**：
  - 測試從 `docs/cli-poc.md` 的 bash 區塊原樣取出 `die()`、步驟 1 的分支判斷行、步驟 7（`SAVED=1` 到 `exit`）。在 `/bin/bash`（GNU bash 3.2.57）中以 `set -u` 執行。
  - 不含 CLI、建置；`rm` 以只記錄參數的 shell 函式替代，所以不刪除任何檔案。probe 退出碼以 `( exit N )` 模擬。
  - 四個案例各在 locale 未指定（沿用環境）、`C`、`en_US.UTF-8`、`zh_TW.UTF-8` 下執行：
    - 正常回報，probe 為 0：exit `0`，`exit-code.txt` 為 `exit=0`
    - 正常回報，probe 為 4：exit `4`，`exit-code.txt` 為 `exit=4`
    - 前置拒絕（分支為 `main`）：exit `2`；`precheck-stop.txt` 以「分支是「main」」開頭；`die` 之後的行沒有執行
    - 證據保存失敗（證據目錄不存在）：exit `5`；訊息含 `probe exit=3；exit-code.txt`
  - 判定條件：退出碼符合、訊息正確、沒有 `unbound`。
  - **結果**：修正後的文字 16／16 PASS。對照組是 `d124d8e` 的舊文字：`C` locale 下 4 個 PASS，其他三種 locale 下 12 個 FAIL（`PROBE_EXIT�`／`BR�: unbound variable`，以 `bash -c` 執行時 exit `127`）。這與第 39.14 節的現象一致：真實執行是以檔案執行，exit 為 `1`。
- **證據**（`artifacts/b1-04/script-branch-check-20261007T182433Z/`，`artifacts/` 不進版控）：
  - `branch-check.py`（測試本體）
  - `branch-check-fixed.txt`、`branch-check-old.txt`（兩次輸出）
  - `doc-base.txt`（`d124d8e`，修正前的 HEAD）
  - `doc-under-test.sha256`（測試當下的 `docs/cli-poc.md`；之後只在檔尾與開頭追加紀錄，bash 區塊沒有再改）

**待決定**：
1. Codex review 本節修正與測試。
2. CLI 版本：改回 `2.1.289`，或評估 `2.1.291` 並修改釘選（程式變更，須設計與 review）。
3. 任何新的診斷型 C0′ 嘗試，須 Aaron 另行核准。

本輪沒有修改 Gate、認證接受清單、啟動參數、版本釘選或產品程式；L1-b 不標完成；B1-04 仍 InProgress。

### 39.16 驗收紀錄：`90d6e2f` 通過限定 review，bash 變數名稱問題關閉（2026-10-08）

> **`90d6e2f` 通過 Codex 限定 review；§39.3 的 bash 變數名稱問題關閉。**

- **證據歸屬**：
  - 本輪分支測試：Claude Code 執行，Codex 核對紀錄，**沒有獨立重跑**。
  - 先前 `unbound variable` 的 bash 最小重現：Codex 獨立執行（分開記錄，見 39.15）。
  - 第 39.14 節的真實執行：Claude Code 執行，Codex 核對紀錄。
- **Codex 核對**：
  - 三處修正。
  - 修正後在四種 locale、四個案例下共 16／16 通過。
  - 舊文字在 UTF-8 環境出現 `unbound variable`，與先前的缺陷一致。
  - 測試沒有呼叫 CLI 或建置，刪除命令由替身取代。
- **範圍**：只關閉變數名稱問題。**不代表完整診斷腳本已端到端驗證。**
- **剩下的阻擋：CLI 版本與釘選不符。**
  - `2.1.291` 目前只是回報值（Aaron 的說明），尚未確認確切版本。
  - 先確認確切版本，再決定是否更新釘選。
  - 版本更新的程式 review，和下一次 C0′ 的執行核准，須分開取得。
- 本輪只同步驗收：沒有再修這個問題、沒有重跑 C0′、沒有查詢版本；沒有修改版本釘選、Gate、認證接受清單、啟動參數或產品程式。L1-b 不標完成；B1-04 仍 InProgress。

## 40 核准 CLI 版本更新為 `2.1.293 (Claude Code)`（2026-10-08；Claude Code 執行，待 Codex 限定 review）

> 版本釘選由 `2.1.289 (Claude Code)` 改為 `2.1.293 (Claude Code)`。**版本資訊由 Aaron 提供**，不是 Claude Code 或 Codex 獨立查詢所得。沒有執行真實 CLI、版本查詢、登入檢查或 C0′。新版真實 runtime 的行為仍待觀測。

- **版本來源**：Aaron 10/8 指示更新為 `2.1.293 (Claude Code)`。先前 Aaron 曾表示「應為 `2.1.291`」（第 39.15 節），本次以 `2.1.293` 為準；兩者都只是 Aaron 提供的值。第 39.14 節的版本檢查拒絕，與「已不是 `2.1.289`」一致，但程式不輸出實際版本字串。
- **程式**（`bd29a4e`；與 `1f0b260` 對 `2.1.289` 的作法相同，不擴大修改其他行為）：
  - `cmd/apa-buyer/main.go`：`approvedCLIVersion` 改為 `"2.1.293 (Claude Code)"`；註解加入 10/8 與「由 Aaron 提供」。
  - `cmd/apa-buyer/main_test.go`：
    - `TestLiveOptIn` 的拒絕清單改為 `2.1.289`、`2.1.288`、`2.1.287`（舊釘選）、`2.1.291`（Aaron 先前回報的值）、`2.1.294`（較新版）。
    - 釘選斷言改為 `2.1.293`。
    - `TestProbeOptIn` 的舊版清單加入 `2.1.289`。
    - `TestApprovedVersionInit`：init 為 `2.1.293` 時 `observed`；`2.1.289`、`2.1.291`、`2.1.288`、`2.1.287` 與 fake 自己的 `0.0.0` 都是 `init_cli_version`。
  - `cmd/demo/integration_test.go`：「不是 fake 的 CLI」替身的回報版本改為 `2.1.293`，維持「即使是核准版本也拒絕」的意義。
  - `internal/buyer/probe.go`、`internal/buyer/host.go`：只改註解中的版本例子。`internal/buyer/gate.go` 的「已核對的真實 init 紀錄（2.1.287／288／289）」是歷史事實，保留。
  - `scripts/verify/mutation-check.py`：pin 突變的標籤改為 `2.1.293`；替換值由 `"2.1.288"` 改為上一個釘選 `"2.1.289"`。
  - 沒有改 Gate 規則、認證接受清單（仍為空）或 plugin 參數。
- **定向 fake 回歸**（Claude Code 執行；`go1.26.4 darwin/amd64`；`-race -count=1`）：
  - `./cmd/apa-buyer/`：`TestVersionCheckCancelled`、`TestLiveOptIn`、`TestLiveEnvDisablesBuiltinAgents`、`TestLivePathWithFake`、`TestProbeOptIn`、`TestProbeCommand`、`TestProbeOneMessageCommand`、`TestApprovedVersionInit`，8／8 PASS。
  - `./internal/buyer/`：`-run 'TestProbe|TestHostInitGate|TestGate|TestInit'`，19 個頂層測試 PASS。
  - `./cmd/demo/`：`TestIntegrationStartRefusals` PASS。
  - **pin 突變**（手動一次）：在 `bd29a4e` 的 `git archive` 匯出中，把 probe 的 init 版本改成固定的 `"2.1.289"`，`TestApprovedVersionInit` 以測試失敗（`--- FAIL`，不是建置錯誤）結束，突變被抓到。完整的 `mutation-check.py`、容器驗證與全套測試**沒有執行**。
  - **失敗紀錄保留**：第一次在 Claude Code 沙箱內執行 `./cmd/apa-buyer/` 時，5 個測試因沙箱禁止 Unix socket／TCP bind（`bind: operation not permitted`）而失敗，不是程式問題。隨後在沙箱外以相同命令重跑，結果如上；兩次輸出都保存。測試只使用 fake CLI。
  - 測試在提交前的工作區執行，工作區內容與 `bd29a4e` 相同（提交後 `git status` 只有 `?? demo`）。
- **證據**（`artifacts/b1-04/pin-2.1.293-20261007T183915Z/`，`artifacts/` 不進版控）：`t-apa-buyer.txt`、`t-buyer.txt`、`t-demo.txt`、`t-mutant.txt`、`t-apa-buyer-sandboxed-FAIL.txt`、`commit.txt`、`go-version.txt`。
- **目前操作文件**：README 環境變數表、`docs/m1-boundaries.md` C0′ 列、本文件 39.2 的釘選項目都已改為 `2.1.293`。歷史執行紀錄（第 22–39.16 節等）保留原版本。
- **證據歸屬**：程式修改與上述測試由 Claude Code 執行，待 Codex 限定 review；版本值由 Aaron 提供。

**範圍與待決定**：只有版本釘選。不代表 `2.1.293` 真實 CLI 的旗標、init 格式、plugins／agents、登入或用量已驗證。下一次診斷型 C0′ 須在本次 review 通過後，由 Aaron 另行核准。B1-04 仍 InProgress；L1-b 不標完成；不合併、不推送、不標 Done。

### 40.1 程式 `bd29a4e` 通過限定 review；操作文件 R1 補正（2026-10-08；待 Codex 複核）

> **程式 `bd29a4e` 的版本釘選通過 Codex 限定 review。** 操作文件的 R1 已補正，待 Codex 複核；複核通過前不執行 C0′。

- **證據歸屬**：
  - **Codex 獨立執行**：在 `bd29a4e` 的暫存匯出副本中，重跑一次定向 fake race 測試；`cmd/apa-buyer`、`internal/buyer`、`cmd/demo` 都通過。
  - **Claude Code 執行、Codex 核對紀錄**：第 40 節的定向回歸，以及把 init 版本改回 `2.1.289` 的突變（確認是測試斷言失敗）。
  - 沒有重跑完整容器、全部突變或全套測試；沒有執行真實 CLI。
- **Codex 確認**：新版通過、其他版本被拒、init 跟隨新釘選；認證接受清單仍為空。
- **R1 補正（只改文件）**：
  - **§39.3 腳本**：`PRODUCT_BASE` 由 `e99f389` 改為已 review 的 `bd29a4ed503b5fc43a8d300de22e57bcf83b3318`；步驟 3 的註解改為不寫死 commit。若不改，版本更新會被判成「基準之後有非文件差異」，在 probe 前停止。祖先檢查、非文件差異檢查與二進位 VCS 檢查都保留，沒有移除任何保護。
  - **現行版本說明改為 `2.1.293`**：§3（`--version` 檢查）、§7.3（`claude_code_version`）、§39.7.4（執行邊界），並補上 `2.1.289` 的期間。
  - 歷史執行紀錄保持原樣。§39 其他小節裡「程式基準 `e99f389`」的開工核對，是當時的紀錄，也不改。
- 本輪沒有重跑 Go 測試、沒有執行 C0′ 或真實 CLI；沒有修改程式、Gate、認證接受清單或 plugin 參數。真實執行須另行核准。L1-b 不標完成；B1-04 仍 InProgress。

### 40.2 驗收紀錄：`3d74230` 通過限定文件複核，R1 關閉（2026-10-08）

> **`3d74230` 通過 Codex 限定文件複核；操作文件 R1 關閉。**

- **證據歸屬**：Codex 只做文件與 Git 唯讀核對，沒有重跑測試或執行腳本。
- **Codex 確認**：
  - `PRODUCT_BASE` 指向已 review 的 `bd29a4e`。
  - 該基準是目前 HEAD 的祖先，之後沒有非文件差異。
  - 祖先、差異與二進位 VCS 檢查都保留。
  - 現行版本說明已統一為 `2.1.293`，歷史紀錄沒有改寫。
- **現況**：
  - 已可提出下一次診斷型 C0′ 的執行核准；本次 review 不等於執行核准。
  - plugins／agents 仍未知；L1-b 未完成。
- 本輪只同步紀錄：沒有再跑驗證、測試、腳本、真實 CLI 或 C0′。B1-04 仍 InProgress。

## 41 診斷型 C0′ 執行紀錄（2026-10-08；Claude Code 執行，待 Codex review）

> **結果：取得 init。plugins 非空，agents 明確為空。** 依 39.7.3「取得 init，但 plugins 或 agents 非空」：**記錄執行阻擋並停止**。不追查來源、不改允許清單、不重試；最小隔離或停用方案另提，其設計、實作與真實驗證都須另行核准。

- **核准**：Aaron 10/8 核准依 §39.3 執行一次。執行基準為 `b2cee13` 或其後只改文件的 commit，產品基準維持 `bd29a4e`，CLI 必須是 `2.1.293 (Claude Code)`。核准接受既有 HOME、網路、登入環境，以及可能產生、無法確認的用量。
- **開工核對**：分支 `b1-04-live`，HEAD `b2cee13`；工作區只有 `?? demo`。`bd29a4e` 之後沒有非文件差異。
- **執行方式**：§39.3 的 bash 區塊原樣抽出，以 `bash` 執行一次。在 Claude Code 沙箱外執行，依核准使用既有環境；腳本沒有修改。腳本 exit `0`，訊息為 `probe exit=0；證據：…`。
- **前置**：步驟 1～5 通過；二進位 `vcs.revision=b2cee130c6e3f4c405dd907def398eb815ab6a04`、`vcs.modified=false`。版本檢查通過，代表 CLI `--version` 輸出等於 `2.1.293 (Claude Code)`；這是本次 runtime 的結果，不是另外的版本查詢。
- **時間**：`started-utc` `2026-10-07T18:48:54Z`，`ended-utc` `2026-10-07T18:48:57Z`。

**白名單觀測**（`stdout.txt` 的 JSON 投影；trace 的 `result` 與它相同，trace 只是不含 `trace` 路徑欄位）：

| 項目 | 值 |
|---|---|
| status／code | `observed`／`init_observed`；`runner_outcome` `init_observed` |
| `cli_exit_code` | `143`：取得 init 後由 Host 停止 CLI（SIGTERM），不是 CLI 自己失敗 |
| 寫入 | `input_status` `complete`；`stdin_bytes_written` 95；`input_message_bytes` 95 |
| 用量 | `usage` `unknown`：訊息已完整送出，可能產生了模型請求，無法確認 |
| 業務工具 | `business_calls_refused` 0、`business_calls_executed` 0 |
| `cli_version` | `approved`（init 的 `claude_code_version` 等於 `2.1.293`） |
| **plugins** | `fields.plugins` `array`；**`plugins_count` 2**；entries：`cc-plugin-agents-md`（path `relative`）、`cc-plugin-plugin-authoring`（path `relative`） |
| **agents** | `fields.agents` `array`；**`agents_count` 0**；`agents_entries` `[]`：依 39.7.2，**明確為空** |
| skills | `skills_count` 0 |
| tools | `tools_count` 3、`tools_expected` 3、`tools_other` 0 |
| MCP | `mcp_servers_count` 1；`apa_server_status` `connected` |
| `permission_mode` | `dontAsk` |
| `model` | `claude-sonnet-5-5` |
| `auth_source` | `none`：不能證明登入方式或計費來源 |
| `extra_members` | 6（白名單以外的成員數；名稱不收集） |

**解讀**（照 39.5、39.7 預先列出的規則，不額外推論）：
- **plugins 非空 → 執行阻擋仍在。** 正式 Gate 要求 plugins 為空，所以這次 init 會被拒，live 請購無法進行。
- 兩個名稱都完全符合顯示候選：只記「名稱相符，來源未確認」。
  - 第三個候選 `cc-plugin-telemetry` 這次沒有出現。這不代表任何來源結論。
  - path `relative` 只是對 CLI 回報文字的分類，不是來源認證。
- **agents 明確為空**：只證明在 L1-a 的 `CLAUDE_AGENT_SDK_DISABLE_BUILTIN_AGENTS=1` 下，這次 runtime 回報為空，不外推到其他環境。是否據此支持 L1-c 的 agents 部分，由 Codex review 判斷。
- `auth_source` `none`、`model`、`permission_mode` 只是觀測值；認證接受清單仍為空，認證決策另行處理。
- 與 pixel-office 的名稱或數量相符與否，都不據此認定兩者設定來源相同。

**證據**（`artifacts/b1-04/c0prime-diag-20261007T184850Z/`，`artifacts/` 不進版控）：
- `commit.txt`、`binary-vcs.txt`、`go-version-build.txt`
- `started-utc.txt`、`ended-utc.txt`、`exit-code.txt`（`exit=0`）
- `stdout.txt`（模式提示行加 JSON 投影）
- `apa-buyer-error.txt`（0 位元組）
- `runs/probe-1791398935849317000.jsonl`（1 行，`event: probe`）

暫存目錄已由腳本刪除；repo 工作區仍只有 `?? demo`。

**沒有做的事**：沒有第二次嘗試、C0 重跑、登入檢查或來源調查；沒有開 Store、建 task 或執行業務工具；沒有修改 Gate、認證接受清單、啟動參數、版本釘選或產品程式；沒有進入正式請購。

**待決定**：
1. Codex review 本次證據與解讀。
2. 是否請 Claude Code 提出 plugins 的最小隔離或停用方案（只提方案；設計、實作與真實驗證都另行核准，不追查來源）。
3. agents 為空的觀測能否支持 L1-c 的對應部分，由 review 判斷。

L1-b 不標完成；B1-04 仍 InProgress；不合併、不推送、不標 Done。

### 41.1 驗收紀錄：本次診斷型 C0′ 結果（2026-10-08）

> **本次 agents／skills 明確為空，plugins 非空，正式請購阻擋仍在。**

- **證據歸屬**：
  - Claude Code 執行。
  - Codex 核對：commit、二進位 VCS、`stdout.txt`／trace 投影的一致性，以及 Gate 原始碼。
  - Codex 沒有重跑真實 CLI。
- **結論範圍**：
  - agents 為空的結果，可作為 L1-c 對應觀測項目的證據（條件：L1-a 的 `CLAUDE_AGENT_SDK_DISABLE_BUILTIN_AGENTS=1`、CLI `2.1.293`、本次 runtime）。
  - **L1-b、L1-c 整體都不標通過。**
  - plugins 非空，正式 Gate 仍拒絕這次 init，live 請購的執行阻擋仍在。
- **plugin 來源**：
  - 來源調查繼續暫緩。
  - 以 APA 本次 runtime 回報的 plugins 為觀測依據，但**不據此允許載入或執行**。
- **下一步建議**：提出「plugins 最小隔離或停用方案」的設計提案，待 Aaron 核准派工。設計、實作與真實驗證都各自另行核准。
- 本輪只同步紀錄：沒有修改參數、Gate 或認證接受清單；沒有重跑 C0′；沒有進入正式請購。B1-04 仍 InProgress。

## 42 plugins 最小隔離或停用方案：設計提案（2026-10-08；Claude Code 查證與設計，待 Codex review）

> **plugins 最小隔離或停用方案：設計提案完成，待 Codex review；未核准實作或真實驗證。**

- **依據**：vault〈Claude-Code-B1-04-plugins最小隔離或停用方案設計派工-04c6135-20261008〉。本輪只查證與設計。
- **開工核對**：分支 `b1-04-live`，HEAD `04c6135`；工作區只有 `?? demo`。產品基準 `bd29a4e` 之後只有文件差異，沒有影響本設計的程式變更。
- **本輪沒有做的事**：
  - 沒有執行任何真實 CLI（含 `--help`、`--version`）。
  - 沒有讀 HOME、keychain、受管設定、設定描述檔或 pixel-office。
  - 沒有安裝、更新、移除 plugin，也沒有改任何設定。
  - 沒有新增 probe 欄位。
  - 沒有改產品程式、Gate、認證接受清單、版本釘選或啟動參數。
  - 沒有跑測試或容器。

### 42.1 目標與已知事實

**目標**：只影響 APA 啟動的 CLI 子程序，讓後續真實觀測有機會同時做到：
- plugins、agents、skills 明確為空；
- APA MCP 連線；
- 工具清單符合三個工具；
- permission mode、版本等既有 Gate 條件不變。

認證接受清單不在本輪範圍；即使 plugins 清空，也不能直接啟動正式請購。

**已知事實**（第 41 節；Claude Code 執行，Codex 核對紀錄，Codex 沒有重跑真實 CLI）：
- CLI `2.1.293`。
- plugins：`cc-plugin-agents-md`、`cc-plugin-plugin-authoring`，path 分類都是 `relative`（不能據此判斷來源或安裝位置）。
- agents、skills 這次為空（不代表每次都為空，也不證明因果）。
- APA MCP `connected`；tools 3／3。
- `auth_source=none`（不代表已確認登入或計費方式）。

**現行啟動條件**（原始碼）：
- `Argv`（`internal/buyer/host.go`）已含 `--restricted`、`--strict-mcp-config --mcp-config <檔案>`、`--tools ""`、`--permission-mode dontAsk`、`--permission-prompts none`、`--disable-slash-commands`、`--no-session-persistence`。
- probe 用 `ProbeArgv`，是同一組參數加上 stream-json 輸入。
- 環境（`CLIEnv`）只放白名單變數，再加固定的 `DISABLE_AUTOUPDATER=1`、`CLAUDE_CODE_DISABLE_NONESSENTIAL_TRAFFIC=1`、`CLAUDE_AGENT_SDK_DISABLE_BUILTIN_AGENTS=1`。
- live（`Host.Run`）與 probe（`Probe`）每次都建立自己的暫存目錄（`apa-run-*`／`apa-probe-*`），結束時 `RemoveAll`。

### 42.2 官方依據（2026-10-08 查閱，線上最新文件）

| 頁面 | 本設計用到的內容（依原文） |
|---|---|
| CLI 參考 https://code.claude.com/docs/en/cli-reference | `--restricted`：「loads only managed settings and `--settings`」（v2.1.248 起）。`--safe-mode`：plugins、skills、hooks、MCP servers、custom agents 等都不載入，「managed plugins … and policy-configured MCP servers do not」；「Authentication, model selection, built-in tools, and permissions work normally」。`--bare`：略過 installed plugins 等自動探索。`--settings`：只對這次 session 有效。`--setting-sources`。`--strict-mcp-config`。 |
| Plugin 載入 https://code.claude.com/docs/en/plugins/loading | id 為 `<name>@<origin>`（`@<marketplace>`、`@inline`、`@skills-dir`、`@synced`），各自有開關方式。`enabledPlugins` 共六種來源，managed 的 `true`／`false` 不能被覆寫。「Plugins load at session start from `installed_plugins.json` and the cache without using the network」。plugin 根目錄預設 `~/.claude/plugins`，可用 `CLAUDE_CODE_PLUGIN_CACHE_DIR` 改。skills-dir plugin 在 `~/.claude/skills/` 或專案 `.claude/skills/` 原地載入。synced plugin 在設了 `CLAUDE_CODE_DISABLE_NONESSENTIAL_TRAFFIC`、`--safe-mode`、bare，或 `--setting-sources` 不含 `user` 時不下載也不載入。 |
| 環境變數 https://code.claude.com/docs/en/env-vars | `CLAUDE_CODE_PLUGIN_CACHE_DIR`：「Override the plugins root directory … marketplaces and the plugin cache live in subdirectories under this path. Defaults to `~/.claude/plugins`」。`CLAUDE_CODE_SAFE_MODE` 等同 `--safe-mode`。`CLAUDE_CODE_SIMPLE`（等同 `--bare`）：「MCP tools from `--mcp-config` are still available」，但「OAuth tokens and keychain credentials are not read」。`CLAUDE_CONFIG_DIR`：設定、session、plugins 都在這個目錄下。 |
| 認證 https://code.claude.com/docs/en/authentication | 設了 `CLAUDE_CONFIG_DIR` 時，「keys the macOS Keychain entry to that directory too, so a session with a different `CLAUDE_CONFIG_DIR` reads a different entry」；每個目錄都有自己的 claude.ai 登入。 |
| 受管設定 https://code.claude.com/docs/en/managed-settings | managed settings 高於所有其他層級，user、project、local、`--settings` 都不能覆寫。 |

**版本適用性**：這些頁面不分版本。本輪看到的最高版本註記是 v2.1.290，沒有提到 `2.1.293`。以下各方案在 `2.1.293` 上的行為，都標為「**版本適用性未確認**」。

### 42.3 候選方案比較

| 方案 | 使用方式 | 作用範圍 | 前置資訊 | MCP 影響 | 登入影響 | 支援依據／未驗證處 | 撤回 |
|---|---|---|---|---|---|---|---|
| **A（第一個實作候選：受控實驗）APA 專用暫存 plugin 根目錄** | 子程序環境加 `CLAUDE_CODE_PLUGIN_CACHE_DIR=<該次暫存目錄>/plugins`（新建的空目錄，0700，隨暫存目錄刪除） | 只有 APA 子程序；Host 不主動修改日常設定，但**不能保證 CLI 不寫入其他日常狀態** | 不需要 plugin ID 或來源 | 文件沒有把它和 MCP 連在一起；`--mcp-config` 不經 plugin 根目錄（推論） | 文件只把 `CLAUDE_CONFIG_DIR` 和憑證連在一起，這個變數沒有（推論：不影響） | 文件明載它**改變 plugin 儲存根目錄，不是禁止載入的開關**。文件也說明，CLI 啟動後可能在背景取得設定宣告但本機缺少的 marketplace，再下載並重新載入 plugins，所以空根目錄可能被重新填入。**沒有涵蓋**：skills-dir plugin（原地載入）、managed 強制啟用的 plugin。對 `2.1.293` 的效果未驗證 | 移除該環境變數 |
| B `--safe-mode`（或 `CLAUDE_CODE_SAFE_MODE=1`） | 子程序加參數或環境變數 | 只有 APA 子程序 | 不需要 ID | **依文件，MCP servers 不載入**，沒有寫 `--mcp-config` 例外（`--bare` 則明寫保留）→ APA bridge 很可能不載入 | 文件：認證照常 | plugins 含 managed plugins 都不載入（文件明載）。與 `--restricted` 併用的行為未說明 | 移除參數 |
| C `--settings` 關閉指定 id | `--settings '{"enabledPlugins":{"<name>@<origin>":false}}'` | 只有這次 session | **需要完整 id**；目前只有顯示名稱，origin 未知 | 無 | 無 | 文件明載 flag 來源。但 managed 的 `true` 不能被覆寫；不得猜 id | 移除參數 |
| D `--bare` | 子程序加 `--bare` | 只有 APA 子程序 | 不需要 ID | 文件明載保留 `--mcp-config` 的工具 | **不讀 OAuth 與 keychain**，須改用 `ANTHROPIC_API_KEY` 或 `apiKeyHelper` → 改變認證與計費 | 文件明載 | 移除參數 |
| E 專用 `CLAUDE_CONFIG_DIR` | 子程序設為空的 APA 目錄 | 只有 APA 子程序 | 不需要 ID | 未說明 | **keychain 項目依目錄區分 → 要另外登入** | 文件明載憑證分開；skills-dir 的位置是否跟著移動，未說明 | 移除變數 |
| F 改 HOME | 子程序 HOME 指向暫存目錄 | 只有 APA 子程序 | 不需要 ID | 未說明 | 文件沒有說明 HOME 與 keychain 的關係 → 未知 | 無直接依據 | 還原 HOME |
| G 調整受管設定 | 由 Aaron 改本機或組織設定 | **全域**，影響日常環境 | 需要 ID | — | — | managed 優先權最高 | 再改回；不得當預設方案 |

不在選項內：讓這兩個 plugins 通過 Gate，不是隔離或停用。

### 42.4 推薦與理由

**沒有任何方案在官方文件上同時保證四件事**：清空 plugins、保留 APA MCP、不改登入、不需要完整 ID。

- **A 列為第一個實作候選**：它是唯一同時符合下列硬性條件的方案：
  - 只影響子程序、可撤回、不改全域設定；
  - 不需要 ID；
  - 文件沒有指出它影響 MCP 或登入。

  **A 是「暫存 plugin 根目錄的受控實驗」，不是已成立的隔離措施，也不是停用保證**：
  - `CLAUDE_CODE_PLUGIN_CACHE_DIR` 改變的是 plugin 儲存根目錄，不是禁止 plugin 載入的開關。
  - 依官方文件，CLI 啟動後可能在背景取得設定宣告、本機缺少的 marketplace，再下載並重新載入 plugins，所以新建的空根目錄也可能被重新填入。這是文件描述的可能行為，不是對本機 `2.1.293` 的實測結論；版本適用性未確認。
  - 既有的 `DISABLE_AUTOUPDATER=1`、`CLAUDE_CODE_DISABLE_NONESSENTIAL_TRAFFIC=1`，**不能當作禁止這些背景下載、寫入及載入的保證**。
  - A 對 skills-dir plugin 與 managed 強制啟用的 plugin 沒有作用。

  因此 A 需要一次真實診斷才知道這次有沒有達到目標。結果的解讀界線：
  - plugins 仍非空：只能判定本次方案未達目標，來源仍未確認；**不得推論必定來自根目錄之外**。
  - init 回報為空：只證明該次 init 回報為空，**不保證之後沒有背景載入**，也不因此宣告正式請購可放行。
- **B 不推薦**：依文件字面，它很可能讓 APA bridge 不載入，違反「保留 APA MCP」。除非另行核准一次只為確認 MCP 是否存活的診斷，否則不採用。
- **C、D、E、F、G 排除為預設**：
  - C 缺完整 id；
  - D、E 改變登入或計費；
  - F 沒有依據；
  - G 是全域修改。

**若 A 的真實診斷後 plugins 仍非空**（只代表本次方案未達目標，來源仍未確認）：下一步需要的資訊與最小授權（擇一，都須 Aaron 決定）：
1. **完整 plugin id**（`<name>@<origin>`）：可由 Aaron 自行在日常環境查看後提供，Claude Code 不讀 HOME；或另行核准 Claude Code 執行一次唯讀的 plugin 清單命令。拿到 id 後才能評估 C。注意：`<name>@<origin>` 的 origin，和「哪一層設定強制啟用它」是不同概念。若該 ID 被 managed settings 強制啟用，`--settings` 無法覆寫；**取得完整 ID 本身，不能證明方案 C 可以停用它**。現有兩個名稱與 `relative` 路徑分類只作 runtime 觀測，不拿來組出猜測的 ID，也不據此判斷 managed 來源。
2. **由 Aaron 決定是否調整受管設定**（G，全域）。
3. **另行核准一次以 B 為參數的診斷**，只確認 APA MCP 是否存活。

### 42.5 後續實作與 fake 驗證計畫（本輪不執行；須設計 review 與 Aaron 另行核准）

- **預計修改**：
  - `internal/buyer/host.go`：`Host.Run` 在該次暫存目錄下建立空的 `plugins`（0700），並在子程序環境加 `CLAUDE_CODE_PLUGIN_CACHE_DIR=<該路徑>`。由一個共用函式先移除環境中同名的值再加入，確保只出現一次。
  - `internal/buyer/probe.go`：`Probe` 用同一個函式。
  - `CLIEnv` 白名單不變（不含這個變數，父環境本來就傳不進來）。`--version` 檢查不加。`Argv`／`ProbeArgv` 不加參數。
  - Gate、`liveAcceptAuth`（空）、版本釘選、plugin 顯示候選都不變。
- **受影響入口**：`apa-buyer run --live`（`Host.Run`）、`apa-buyer probe-init`（`Probe`），以及使用同一個 `Host.Run` 的 demo fake run。對 fake 無害，但會一併改變它的子程序環境，須在 review 中確認。
- **測試**（以既有 `TestCLIEnvReachesChild` 的作法，由 fake CLI 記錄自己的環境）：
  1. live 與 probe 的子程序都看到 `CLAUDE_CODE_PLUGIN_CACHE_DIR`，指向該次暫存目錄下的空目錄，存在、權限 0700、不在 HOME 下。
  2. 父環境設 `CLAUDE_CODE_PLUGIN_CACHE_DIR=/elsewhere` 時，子程序仍只看到 APA 的值，且只出現一次。
  3. 執行結束後該目錄隨暫存目錄刪除。
  4. `Argv`／`ProbeArgv` 與既有 `TestArgvAndEnv` 的期望一致（沒有新增參數）。
  5. Gate 仍拒絕非空 plugins（既有 `TestHostInitGate`），認證接受清單仍空。
  6. 突變：拿掉 live 的設定、拿掉 probe 的設定、讓父環境的值勝出，各自須由測試以斷言失敗抓到。
  7. **建立專用 plugin 目錄失敗時，CLI 不啟動**，也不退回原本的 plugin 目錄。以 fake 的啟動計數，或同等可觀測的方式，確認沒有啟動。
  8. **清理只限本次資源**：正常結束、取消，以及 CLI 啟動失敗時，都清理本次建立的暫存目錄；不碰其他目錄。
- **容器驗證**：Codex 同意把一次完整斷網容器驗證納入後續實作的驗收計畫，因為共用的 Host 路徑也影響 demo。這是驗收範圍的建議，不是執行核准；本輪不跑。
- **限制**：fake 測試只能證明環境傳遞與程式行為，**不能證明真實 plugins 已停用**。

### 42.6 後續一次真實診斷計畫（實作通過 review 後，另取 Aaron 的一次執行核准）

- 沿用 §39.3 腳本與既有執行邊界。`PRODUCT_BASE` 改為實作通過 review 的 commit；版本、二進位基準、工作區、VCS 檢查不符就停止。
- **觀測條件**（對應現有投影欄位；全部符合才算本次診斷達到目標）：

  | 項目 | 必須符合 |
  |---|---|
  | MCP | `fields.mcp_servers` 為 `array`、`mcp_servers_count == 1`、`apa_server_status == connected`（只有 APA connected 不夠，可能還有其他 server） |
  | plugins | `fields.plugins` 為 `array`、`plugins_count == 0`、`plugins_entries` 為空 |
  | agents | `fields.agents` 為 `array`、`agents_count == 0`、`agents_entries` 為空 |
  | skills | `fields.skills` 為 `array`、`skills_count == 0`（現有投影沒有 `skills_entries`，不要求它，也不為此擴充 probe） |
  | 工具 | `tools_count == 3`、`tools_expected == 3`、`tools_other == 0` |
  | 權限及版本 | `permission_mode == dontAsk`、`cli_version == approved` |

- 缺欄位、`null`、型別錯誤或其他未知值，不能當作空清單。沿用既有診斷狀態與失敗處理；不改 probe 的退出碼或產品邏輯。
- 這些只是本次診斷的觀測條件，**不等於完整 live Gate 已通過，也不代表認證已獲接受**。
- **執行確認清單須明列的風險**：
  - CLI 可能在背景下載、寫入並載入 plugins 或 marketplace；
  - 既有停用更新、停用非必要流量的環境變數不是禁止保證；
  - init 為空不保證之後沒有背景載入；
  - Host 不主動修改日常設定，但不能保證 CLI 不寫入其他日常狀態。

  本設計不核准任何背景活動實驗，也不新增下載、安裝或監看步驟。
- **非空、未知、MCP 失聯或其他不符**：保存既有白名單證據後停止；不重試、不自動換成 B 或其他方案。
- 不開 Store、不建 task、不執行業務工具。沿用實際輸入記錄與 `usage` unknown 的規則；不新增原始 init、路徑或設定內容的輸出。
- **plugins 清空不代表正式請購可以放行**：其他 Gate 條件與認證接受值仍須另行處理。

### 42.7 尚待 Aaron 核准

1. 本設計的 Codex review。
2. 方案 A 的實作與 fake 驗證（含是否跑完整容器）。
3. 實作通過 review 後，一次真實診斷的執行。
4. 若 A 未達目標：在 42.4 的三個下一步中擇一。

本輪沒有改程式、Gate、認證接受清單、版本釘選或啟動參數；L1-b、L1-c 整體不標通過；B1-04 仍 InProgress。

### 42.8 修訂紀錄：依 Codex 對 `7d72714` 的 R1／R2（2026-10-08；待 Codex 限定複核）

> plugins 方案 R1、R2 設計補正完成，待 Codex 限定複核；方案 A 保留為受控實驗候選，尚未核准實作或真實驗證。

- **依據**：vault〈Claude-Code-B1-04-plugins方案R1-R2設計修訂指示-7d72714-20261008〉。
- **開工核對**：分支 `b1-04-live`，HEAD `7d72714`；工作區只有 `?? demo`。產品基準 `bd29a4e` 之後只有文件差異。
- **Codex review 的證據歸屬**：Codex 以文件、程式原始碼及官方公開文件做限定 review；沒有執行真實 CLI、產品測試或容器，沒有修改產品程式。
- **Codex 補充的官方依據**（2026-10-08 review 查閱）：
  - [Plugin 載入：啟動時不在磁碟上的 plugins 與 marketplaces](https://code.claude.com/docs/en/plugins/loading#plugins-and-marketplaces-that-aren%E2%80%99t-on-disk-at-session-start)
  - [Plugin 根目錄環境變數](https://code.claude.com/docs/en/env-vars#variables)
  - [Plugin 來源與設定優先序](https://code.claude.com/docs/en/plugins/loading#find-where-a-plugin-came-from)
  - 這是官方文件描述的可能行為，不是對本機 `2.1.293` 的實測結論；版本適用性未確認。
- 被取代的文字不再作為現行操作依據；原寫法保留在下表。

| 項目 | 原寫法（`7d72714`） | 現行 | 位置 |
|---|---|---|---|
| R1-1 定位 | A 為「APA 專用空 plugin 根目錄」的第一個實作候選 | A 是「暫存 plugin 根目錄的受控實驗」，不是已成立的隔離措施或停用保證；該變數改的是儲存根目錄，不是禁止載入的開關；空根目錄可能在背景被重新填入 | 42.3 A 列、42.4 |
| R1-2 非空的解讀 | 「若 A 的真實診斷後 plugins 仍非空：表示來源在 plugin 根目錄之外」 | 只能判定本次方案未達目標，來源仍未確認；不得推論必定來自根目錄之外 | 42.4 |
| R1-3 為空的解讀 | （未寫） | init 為空只證明該次 init 回報為空；不保證之後沒有背景載入，也不宣告正式請購可放行 | 42.4、42.6 |
| R1-4 背景活動 | （未寫） | 執行確認清單須明列可能的背景下載、寫入、載入；既有停用更新、非必要流量的變數不是禁止保證 | 42.4、42.6 |
| R1-5 日常狀態 | 「不寫日常 `~/.claude`」 | 「Host 不主動修改日常設定；不能保證 CLI 不寫入其他日常狀態」 | 42.3 A 列、42.6 |
| R1 ID 與設定來源 | 「若 origin 是 managed，C 無效」 | origin 和「哪一層設定強制啟用」是不同概念；被 managed 強制啟用時 `--settings` 無法覆寫；取得完整 ID 不能證明 C 可停用；現有名稱與路徑分類不拿來猜 ID 或判斷 managed | 42.4 |
| R2 診斷條件 | 三個 count 為 0、entries 為空、`apa_server_status` connected、tools、權限、版本 | 改為對應現有投影欄位的表格：加上 `fields.mcp_servers` 為 `array` 且 `mcp_servers_count == 1`；skills 只要求 `fields.skills` 為 `array`、`skills_count == 0`（沒有 `skills_entries`）；缺欄位、`null`、型別錯誤不能當空；觀測條件不等於 live Gate 通過或認證接受 | 42.6 |
| 測試計畫補充 | 六項 | 加第 7 項（建立專用 plugin 目錄失敗時 CLI 不啟動、不退回原目錄）與第 8 項（正常、取消、啟動失敗都只清理本次暫存目錄）；完整斷網容器驗證納入後續實作驗收計畫（不是執行核准） | 42.5 |

本輪只改文件：沒有執行真實 CLI、測試、容器、建置或真實診斷；沒有讀 HOME、keychain、受管設定、設定描述檔或 pixel-office；來源調查繼續暫緩。沒有改產品程式、測試腳本、啟動參數、Gate、認證接受清單、版本釘選或本機設定。L1-b、L1-c 不標整體通過；B1-04 仍 InProgress。

### 42.9 驗收紀錄：plugins 方案於 `14eb7b5` 通過限定設計複核（2026-10-08）

> **plugins 方案於 `14eb7b5` 通過限定設計複核，R1、R2 關閉。**

- **證據歸屬**：Codex 限定設計複核，依文件、程式原始碼及官方公開文件；沒有執行真實 CLI、產品測試或容器。
- **通過範圍**：方案 A（APA 專用暫存 plugin 根目錄）作為**受控實驗的設計**，包含 42.5 的實作與測試計畫，以及 42.6 的診斷條件。**不代表 plugins 已停用，也不代表正式請購可放行**；方案 A 的效果仍未驗證。
- **下一步**：
  - 等待 Aaron 核准方案 A 的實作、fake 測試與一次完整斷網容器驗證。
  - 真實診斷須在實作 review 通過後另行核准。
- 本輪只同步紀錄：沒有改程式、Gate、認證接受清單、版本釘選或啟動參數；沒有執行測試、容器或真實 CLI。L1-b、L1-c 不標整體通過；B1-04 仍 InProgress。

## 43 方案 A 實作與 fake 驗證（2026-10-08；Claude Code 執行，待 Codex review）

> **方案 A 已實作；本機定向 fake 測試通過。核准的一次完整斷網容器驗證失敗（測試在 Linux 上的寫法問題），修正後尚未在容器中驗證。** fake 測試不能證明真實 plugins 已停用；沒有執行真實 CLI。

- **核准**：Aaron 10/8 核准方案 A 的實作、fake 測試，以及一次完整斷網容器驗證。真實診斷須在實作 review 通過後另行核准。
- **開工核對**：分支 `b1-04-live`，HEAD `71e9017`；工作區只有 `?? demo`。

### 43.1 程式（`6caba93`）

- `internal/buyer/host.go`：
  - 新增 `EnvPluginRoot`（`CLAUDE_CODE_PLUGIN_CACHE_DIR`）與 `withPluginRoot(env, dir)`。它建立 `dir/plugins`（0700，空目錄），移除 env 中原有的同名值，再在最後加上唯一一個指向它的值。建立失敗就回傳錯誤，不退回其他根目錄。
  - `pluginMkdir`（預設 `os.Mkdir`）讓測試可以替換，與既有的 `watchHook` 同樣作法。
  - `Host.Run` 在建立工作目錄之後、啟動 CLI 之前呼叫它，並把結果交給 `Run`。
- `internal/buyer/probe.go`：`Probe` 用同一個函式。
- **清理**：沿用既有的 `defer os.RemoveAll(dir)`。根目錄在該次的 `apa-run-*`／`apa-probe-*` 暫存目錄裡，正常結束、取消、啟動失敗都會一併刪除，不碰其他目錄。
- **沒有改**：`CLIEnv` 白名單、`Argv`／`ProbeArgv`、`--version` 檢查、Gate、`liveAcceptAuth`（空）、版本釘選、plugin 顯示候選。demo 的 fake run 經同一個 `Host.Run`，也會拿到這個變數；fake CLI 不讀它。

### 43.2 測試（`internal/buyer/pluginroot_test.go`；`6caba93`、`086f640`、`40bd263`）

| 測試 | 對應 42.5 | 內容 |
|---|---|---|
| `TestWithPluginRoot` | 2 | 交給 runner 的 env 中，原有的值被移除，根目錄只出現一次且在最後。os/exec 對重複的鍵只保留最後一個，端到端測試看不到重複，所以直接檢查 slice。 |
| `TestPluginRootLive`、`TestPluginRootProbe` | 1、2、3、8 | 用腳本 CLI 記錄它看到的值：只設一次、權限 `drwx------`、空目錄、位於 `apa-run-*`／`apa-probe-*` 之下、不是父環境的值、不在 HOME 下。執行後該目錄已刪除，父環境的目錄沒被碰到。 |
| `TestPluginRootFailureStartsNoCLI` | 7 | 建立根目錄失敗時，live 與 probe 都回傳錯誤；腳本 CLI 的啟動標記不存在。 |
| `TestPluginRootCleanup` | 8 | live 與 probe 各兩種情況：建立後隨即取消，以及 CLI 不存在而無法啟動。兩種都確認根目錄已刪除，其他目錄不受影響。 |
| 既有 `TestArgvAndEnv`、`TestHostInitGate` | 4、5 | 沒有改動，照常通過（參數不變；Gate 仍拒絕非空 plugins）。 |

**突變**（`scripts/verify/mutation-check.py`，由 250 項增為 255 項）：
- live 改回原環境；probe 改回原環境；
- 保留原有的同名值；
- 父環境的值勝出（根目錄放在最前面）；
- 建立失敗仍繼續執行。

**本機驗證**（Claude Code 執行；macOS，`go1.26.4`；在沙箱外執行，因為沙箱禁止 socket bind）：
- `6caba93` 提交前：`go test -race -count=1 ./internal/buyer/ ./cmd/apa-buyer/ ./cmd/demo/` 全部 ok。
- 突變試跑：在匯出副本上逐一套用新增的突變。
  - 第一次試跑：live／probe 兩項因「`env` 宣告未使用」**編譯失敗**，不算被抓到；「保留原有同名值」**存活**，原因是 os/exec 的重複鍵規則。
  - 修正於 `086f640`：突變改成能編譯；新增 `TestWithPluginRoot`；新增「父環境勝出」突變。
  - 第二次試跑：5 項都以測試斷言失敗被抓到。
- `40bd263` 之後：`-race` 跑 plugin root 相關測試通過。另在副本把權限改成 0755，`TestPluginRootLive` 會失敗，確認新的權限檢查有效。

### 43.3 完整斷網容器驗證（核准的一次；**失敗**，保留紀錄）

- **commit**：`086f640`；映像 `docker.io/library/apa-verify:b1-05`（`sha256:78258368…b6d0`）。
- **結果**：overall exit `1`。證據在 `artifacts/verify-container/runs/086f6401c320-20261007T192704Z/`（匯入完整，284 個檔案）。

| 步驟 | 結果 |
|---|---|
| check-contracts | exit 0 |
| check（make） | exit 2：`TestPluginRootLive`、`TestPluginRootProbe` 失敗 |
| race-cover | exit 1：同上兩項 |
| verify-b1-01 | exit 1：同上兩項 |
| guards、evidence-import-tests | exit 0 |
| mutation | exit 1：baseline 失敗（同上兩項），整體 NOT OK；255 項都列為 KILLED，但 baseline 失敗時這些結果不能採用 |
| 隔離自檢 | 17 項全部符合預期 |

- **原因**：測試腳本用 `stat -f %Lp` 讀權限。這是 macOS 的寫法；在 Linux 上 `-f` 代表檔案系統狀態，輸出的是檔案系統資訊，權限欄位不符而失敗。日誌中的值仍可看到根目錄位置、只設一次、空目錄。這是**測試的可攜性問題，不是產品行為**；失敗的其他四個步驟都出自這兩個測試。
- **修正**：`40bd263` 改用 `ls -ld | cut -c1-10`，比對 `drwx------`，macOS 與 Linux 輸出相同。只改測試。
- **沒有重跑**：核准範圍是一次容器驗證，已經用掉。修正後的 `40bd263` **還沒有容器結果**；在 Linux 上是否通過，目前只能推論。

### 43.4 尚待核准

1. **再跑一次完整斷網容器驗證**（`40bd263` 或其後只改文件的 commit），須 Aaron 核准。
2. Codex review 實作、測試、突變與這次失敗紀錄。
3. 實作 review 通過後，一次真實診斷（42.6），另行核准。

本輪沒有執行真實 CLI；沒有改 Gate、認證接受清單、版本釘選或啟動參數。L1-b、L1-c 不標整體通過；B1-04 仍 InProgress。

### 43.5 補跑的完整斷網容器驗證（`40bd263`；2026-10-08；Claude Code 執行，待 Codex 核對）

> **結果：通過。** overall exit `0`。只代表 `40bd263` 的 fake 測試、突變與隔離檢查通過，**不代表真實 plugins 已停用**。`086f640` 的失敗紀錄（43.3）保留，新結果不回填到 `086f640`。

- **核准**：Aaron 10/8 核准對 `40bd263`，或其後確認只有文件差異的 commit，補跑一次。範圍包含未突變基準線、255 項突變與隔離檢查。
- **實際基準**：`40bd2630cbae0adf552e1dd54ae87ee4c8022cd4`。當時 HEAD 是 `0d6e443`，`40bd263..0d6e443` 只改 `docs/cli-poc.md`、`docs/commercial-demo-roadmap.md`，所以直接驗證 `40bd263`。
- **映像與限制**：沿用 `docker.io/library/apa-verify:b1-05`（`sha256:78258368de0907e58498095c327a6ee4edbe75421d18e524bd00661c3749b6d0`）。沒有重建映像、沒有新增依賴（go.mod／go.sum 與映像相符）、沒有放寬時間限制。
- **命令**（在 Claude Code 沙箱外執行，需要 Docker）：

  ```sh
  APA_VERIFY_IMAGE=docker.io/library/apa-verify:b1-05 DOCKER_CONTEXT=desktop-linux sh scripts/verify/container-verify.sh 40bd263
  ```

| 步驟 | exit | 秒 |
|---|---|---|
| check-contracts | 0 | 17 |
| check（make） | 0 | 94 |
| race-cover | 0 | 283 |
| verify-b1-01 | 0 | 43 |
| guards | 0 | 0 |
| evidence-import-tests | 0 | 2 |
| mutation | 0 | 372 |

- **突變**：baseline（未突變）PASS；`result: all mutations killed by a failing test`，255 項都列為 KILLED，包含 5 項方案 A 突變。
- **隔離自檢**：17 項全部符合預期，包含禁止外部網路與 DNS、根檔案系統與原始碼唯讀、非 root、沒有 capabilities、記憶體與 pids 上限。
- **證據**：`artifacts/verify-container/runs/40bd2630cbae-20261008T020624Z/`。匯入完整，284 個檔案，332414 bytes；container／capture exit 0；evidence import exit 0。
- **證據歸屬**：Claude Code 執行，待 Codex 核對紀錄。

**尚待**：Codex review 實作、測試、突變，以及這兩次容器紀錄（`086f640` 失敗、`40bd263` 通過）。review 通過後，一次真實診斷（42.6）須另行核准。本次沒有執行真實 CLI 或 C0′；沒有改 Gate、認證接受清單；不合併、不推送、不標 Done。L1-b、L1-c 不標整體通過；B1-04 仍 InProgress。

### 43.6 驗收紀錄：方案 A 於 `40bd263` 通過限定範圍技術 review（2026-10-08）

> **方案 A 於 `40bd263` 通過限定範圍技術 review，關閉這項實作與 fake 驗證。** 只證明 APA 子程序的專用 plugin 根目錄設定、失敗處理與 fake 回歸符合要求，**不代表真實 plugins 已停用，也不代表正式請購可放行**。

- **依據**：vault〈Claude-Code-B1-04-方案A驗收同步與診斷清單指示-77076a8-20261008〉。
- **證據歸屬**（分開記錄）：
  - **Claude Code 執行、Codex 本輪核對紀錄**：完整斷網容器的七個步驟、未突變基準線、255 項突變、17 項隔離檢查（43.5）。
  - **Codex 本輪獨立核對**：
    - manifest 的 283 個列項雜湊全部相符。匯入的 284 個檔案包含 manifest 本身，所以雜湊核對是 283 項，不是 284 項。
    - #251～#255 的日誌確認是測試斷言失敗，不是編譯失敗。
    - `40bd263` 之後只有文件差異。
  - **Codex 前輪原始碼 review**：獨立記錄，不寫成本輪重新執行測試。
  - Codex 沒有重跑容器、fake 測試或真實 CLI。
- **通過的證據**：`artifacts/verify-container/runs/40bd2630cbae-20261008T020624Z/`。
- **`086f640` 的失敗紀錄保留**（43.3）：該輪未突變基準線失敗，所以那 255 項 KILLED 不算驗收通過。新結果不回填到舊 commit。
- Gate、認證接受清單、產品程式不變；來源調查繼續暫緩。L1-b、L1-c 不標整體通過；B1-04 仍 InProgress。

## 44 下一次診斷型 C0′ 執行確認清單（方案 A 之後；2026-10-08 整理；待 Codex 文件複核與 Aaron 另行核准；尚未執行）

> 本清單只告知未來執行的界線與已知風險，**不構成執行核准**。文件複核通過也不等於執行核准。本輪沒有執行任何 CLI、測試、建置、容器，也沒有跑 `bash -n`。

- **開工核對**：分支 `b1-04-live`，HEAD `77076a8`；工作區只有 `?? demo`。實作驗收基準 `40bd263` 之後只有文件差異。
- **工具**：沿用 §39.3 的腳本與 §42.6 的觀測條件，不另做新的執行工具。

### 44.1 相對既有腳本的變更

- `PRODUCT_BASE` 由 `bd29a4ed503b5fc43a8d300de22e57bcf83b3318` 改為 `40bd2630cbae0adf552e1dd54ae87ee4c8022cd4`。只改這一行與它的註解。
- 執行基準：腳本取核准時的 HEAD。它必須是 `40bd263` 的後代，且之後只有 `docs/`、`README.md` 的差異；腳本已用 `merge-base --is-ancestor` 與非文件差異檢查判斷，不符就以 exit 2 停止。
- 保留的嚴格檢查：
  - 分支；
  - 工作區只容許空或 `?? demo`；
  - 祖先關係與非文件差異；
  - 證據目錄已存在就停止；
  - 全新暫存目錄中的本機 clone、`.git` 為目錄、clone HEAD 等於基準；
  - 二進位 `vcs.revision` 等於基準、`vcs.modified=false`。
- CLI 路徑維持 `/Users/mac/.local/bin/claude`；版本必須是 `2.1.293 (Claude Code)`，由 apa-buyer 內建檢查。不符就停止，不自行更新 CLI 或改釘選。
- 其他現行操作文字中已沒有失效的基準；歷史紀錄中的 `e99f389`、`bd29a4e` 保留原樣。

### 44.2 待 Aaron 核准的範圍（未勾選）

- [ ] **一次執行嘗試**，不是一直試到取得 init。前置檢查或版本檢查失敗也停止，不自動重試。
- [ ] C0′ 只嘗試寫入一次固定訊息 `Reply only OK.`。依程式記錄實際的 `input_status` 與 `stdin_bytes_written`，不以預定的 95 位元組代替實際結果。
- [ ] 一則訊息不等於一次模型請求；用量無法確認時記為 `unknown`。
- [ ] CLI 可使用既有 HOME、網路與登入環境；Claude Code 不另外讀取或輸出憑證。
- [ ] 不開 Store、不建 task、不執行業務工具，不進入正式請購。
- [ ] 沿用既有等待上限（init 30 秒；版本檢查與清理另計）、停止條件、程序清理，以及證據保存失敗的處理（exit 5，不呼叫 `die`、不重試）。

### 44.3 已知風險（核准前須知）

- **方案 A 不是禁止載入的開關**：子程序的 `CLAUDE_CODE_PLUGIN_CACHE_DIR` 指向該次暫存目錄下的空目錄。依官方文件，CLI 可能在背景下載、寫入，並重新載入 plugins 或 marketplace。
- 既有的 `DISABLE_AUTOUPDATER=1`、`CLAUDE_CODE_DISABLE_NONESSENTIAL_TRAFFIC=1`，**不是禁止上述活動的保證**。
- 方案 A 對 skills-dir plugin 與 managed 強制啟用的 plugin 沒有作用（第 42 節）。
- Host 不主動修改日常設定，但**不能保證 CLI 不寫入其他日常狀態**。
- 程序清理不保證回收脫離 process group 的子程序；沒有 `orphaned_output` 也不代表沒有殘留（39.4）。
- 本清單不新增背景下載、安裝、來源追查或監看步驟。

### 44.4 觀測條件（依現有投影欄位）

| 項目 | 必要條件 |
|---|---|
| plugins | `fields.plugins == array`、`plugins_count == 0`、`plugins_entries` 為空 |
| agents | `fields.agents == array`、`agents_count == 0`、`agents_entries` 為空 |
| skills | `fields.skills == array`、`skills_count == 0`（不要求不存在的 `skills_entries`） |
| MCP | `fields.mcp_servers == array`、`mcp_servers_count == 1`、`apa_server_status == connected` |
| 工具 | `tools_count == 3`、`tools_expected == 3`、`tools_other == 0` |
| 權限與版本 | `permission_mode == dontAsk`、`cli_version == approved` |

- 缺欄位、`null`、型別錯誤不得當作空。不改 probe 的產品邏輯或退出碼。
- **probe 的 `exit 0`／`observed` 不等於上表全部通過**，須逐項核對。
- 認證接受清單仍為空；`auth_source=none` 不能證明登入或計費方式，也不代表認證已獲接受。

### 44.5 結果分支

| 結果 | 處理 |
|---|---|
| 取得 init，且上表全部符合 | 只表示**本次 init 達到診斷目標**。停止，交 review。不保證之後沒有背景載入；不自動修改認證清單，也不進入正式請購。 |
| 取得 init，但非空或其他條件不符 | 保存證據後停止。不推論 plugin 來源，不自動換方案（42.4 的下一步須 Aaron 決定）。 |
| 未取得觀測，或證據不完整 | 依 39.4、39.7.3 與 exit 2／5 的既有規則回報。不宣稱成功，不重試。 |

### 44.6 證據

- 維持既有白名單：`commit.txt`、`binary-vcs.txt`、`go-version-build.txt`、`started-utc.txt`、`ended-utc.txt`、`exit-code.txt`、`stdout.txt`、`apa-buyer-error.txt`、`runs/` 的 trace。
- 不保存原始 init、CLI 原文、原始 plugin 路徑、設定內容或憑證。
- `stdout.txt` 的格式：模式提示行之後才是 JSON 投影，整份檔案不能直接當 JSON 解析。
- 未來的證據先標「Claude Code 執行，待 Codex review」。

本輪 Gate、認證接受清單、產品程式不變；沒有讀 HOME、keychain、受管設定、設定描述檔或 pixel-office。L1-b、L1-c 不標整體通過；B1-04 仍 InProgress。

### 44.7 驗收紀錄：`d46ca0b` 通過 Codex 限定文件複核（2026-10-08）

> **`d46ca0b` 通過 Codex 限定文件複核。方案 A 的實作驗收基準為 `40bd263`；下一次診斷型 C0′ 清單已通過文件複核，尚待 Aaron 另行核准執行。**

- **證據歸屬**：Codex 本輪只做文件差異與 Git 唯讀核對，確認了四件事：產品基準、祖先關係、其後只有文件差異，以及執行界線一致。沒有執行腳本、`bash -n`、測試、建置、容器或真實 CLI。
- **開工核對**（本次同步）：分支 `b1-04-live`，HEAD `d46ca0b`；工作區只有 `?? demo`。
- **等待 Aaron 核准的事項**：
  - 依 §39.3、§44 執行**一次**診斷嘗試；
  - 任何前置失敗（含版本檢查）也停止，不重試；
  - 接受既有登入環境、未知用量，以及 44.3 已列明的背景活動風險。
- **本次文件複核不是執行核准**。
- 本輪只同步紀錄：沒有修改 §39.3 腳本、Gate、認證接受清單、版本釘選或啟動參數；沒有查詢 CLI 版本、執行 C0′、追查 plugin 來源或進入正式請購。L1-b、L1-c 不標整體通過；B1-04 仍 InProgress。

## 45 方案 A 後的診斷型 C0′ 執行紀錄（2026-10-08；Claude Code 執行，待 Codex review）

> **結果：取得 init，plugins 仍非空；方案 A 本次未達目標。** 依 44.5：保存證據後停止。不推論 plugin 來源，不自動換方案，不重試。正式請購的執行阻擋仍在。

- **核准**：Aaron 10/8 核准「依 §39.3、§44 執行一次診斷嘗試」。核准接受既有登入環境、未知用量，以及 44.3 列明的背景活動風險。
- **開工核對**：分支 `b1-04-live`，HEAD `55c2cac`；工作區只有 `?? demo`。產品基準 `40bd263` 之後沒有非文件差異。
- **執行方式**：§39.3 的 bash 區塊原樣抽出（`PRODUCT_BASE=40bd263…`），以 `bash` 執行一次。在 Claude Code 沙箱外執行，依核准使用既有環境；腳本沒有修改。腳本 exit `0`。
- **前置**：步驟 1～5 通過；二進位 `vcs.revision=55c2cacb7e8b2c36fdcdd366658c19df2c4b143d`、`vcs.modified=false`。這個 commit 含方案 A（`6caba93`）。版本檢查通過（`2.1.293 (Claude Code)`）。
- **時間**：`started-utc` `2026-10-08T02:37:12Z`，`ended-utc` `2026-10-08T02:37:13Z`。

**依 44.4 逐項核對**（`stdout.txt` 的 JSON 投影；trace 的 `result` 與它相同，只差 `trace` 路徑欄位）：

| 項目 | 觀測 | 條件 |
|---|---|---|
| plugins | `fields.plugins` `array`；**`plugins_count` 2**；entries：`cc-plugin-agents-md`（`relative`）、`cc-plugin-plugin-authoring`（`relative`） | **不符** |
| agents | `fields.agents` `array`；`agents_count` 0；`agents_entries` `[]` | 符合 |
| skills | `fields.skills` `array`；`skills_count` 0 | 符合 |
| MCP | `fields.mcp_servers` `array`；`mcp_servers_count` 1；`apa_server_status` `connected` | 符合 |
| 工具 | `tools_count` 3、`tools_expected` 3、`tools_other` 0 | 符合 |
| 權限與版本 | `permission_mode` `dontAsk`；`cli_version` `approved` | 符合 |

其他觀測：
- `status`／`code`：`observed`／`init_observed`；`cli_exit_code` 143（取得 init 後由 Host 停止）。
- 寫入：`input_status` `complete`，`stdin_bytes_written` 95；`usage` `unknown`。
- 業務工具：`business_calls_refused` 0、`business_calls_executed` 0。
- 其他欄位：`model` `claude-sonnet-5-5`；`auth_source` `none`（不能證明登入或計費方式）；`extra_members` 6。

**解讀**（照 42.4、44.5，不額外推論）：
- 方案 A 本次未達目標：plugins 數量、名稱、路徑分類都與第 41 節相同。
- 這**只能判定本次方案未達目標，來源仍未確認**；不得推論它們必定來自 plugin 根目錄之外。
- agents、skills 為空，APA MCP 與工具清單也符合，但 plugins 非空，正式 Gate 仍會拒絕這次 init。
- probe 的 `exit 0`／`observed` 不等於 44.4 全部通過。

**證據**（`artifacts/b1-04/c0prime-diag-20261008T023528Z/`，`artifacts/` 不進版控）：
- `commit.txt`、`binary-vcs.txt`、`go-version-build.txt`、`started-utc.txt`、`ended-utc.txt`
- `exit-code.txt`（`exit=0`）、`stdout.txt`、`apa-buyer-error.txt`（0 位元組）
- `runs/probe-1791427033287836000.jsonl`（1 行）

暫存目錄已由腳本刪除；repo 工作區仍只有 `?? demo`。第 39.11 節那次留下的 `$TMPDIR/apa-c0p.WpXh56/` 不是本次產生的，仍未處理。

**沒有做的事**：沒有第二次嘗試、C0 重跑、版本查詢、登入檢查或來源調查；沒有開 Store、建 task 或執行業務工具；沒有修改 Gate、認證接受清單、啟動參數、版本釘選或產品程式；沒有進入正式請購。

**待決定**（42.4 列出的下一步，擇一，都須 Aaron 決定）：
1. 提供完整 plugin id（由 Aaron 自行查看後提供，或另行核准一次唯讀的 plugin 清單命令）。拿到 id 後才能評估 `--settings` 方案；若該 id 被 managed 強制啟用，`--settings` 無法覆寫。
2. 由 Aaron 決定是否調整受管設定（全域）。
3. 另行核准一次以 `--safe-mode` 為參數的診斷，只確認 APA MCP 是否存活。

另外也須決定：方案 A 的程式要保留還是撤回。它沒有達到目標，但也沒有影響其他觀測項目。

L1-b、L1-c 不標整體通過；B1-04 仍 InProgress；不合併、不推送、不標 Done。

## 46 完整 plugin ID 的最小查詢方案（2026-10-08；Claude Code 整理，未執行）

> 本節只整理方案：**沒有執行任何命令（含 `claude`、`--version`、`plugin list`），沒有讀設定、HOME 或 keychain，沒有改 Gate、認證接受清單、啟動參數或產品程式。** 不構成執行核准。

- **目的**：第 45 節的診斷之後，Aaron 需要在 42.4 的三個下一步中擇一。本節展開第 1 項「取得完整 plugin id」：只為那兩個 runtime 觀測到的名稱（`cc-plugin-agents-md`、`cc-plugin-plugin-authoring`），取得 `<name>@<origin>`，作為評估 `--settings` 方案（42.3 的 C）的前置資訊。

### 46.1 官方依據（2026-10-08 查閱；版本適用性未確認）

[Plugin 命令參考](https://code.claude.com/docs/en/plugins/cli-reference)的 `plugin list`：
- `claude plugin list --json` 對每個安裝輸出一個物件。一定有的欄位：`id`、`version`、`scope`、`enabled`、`installPath`。
- `id` 的形式：`name@marketplace`（安裝）、`name@inline`、`name@skills-dir`、`name@synced`。
- `scope`：安裝為 `user`／`project`／`local`／`managed`；skills-dir 為 `user`／`project`；另有 `session`、`synced`。
- `enabled`：「Whether the plugin is enabled in your merged settings」。
- 其他可能出現的欄位，例如 `mcpServers`（plugin 的 MCP 定義）、`errors`、`installPath`，可能含路徑或設定內容，**不保留**。

[Plugin 載入](https://code.claude.com/docs/en/plugins/loading)：
- marketplace plugin 有兩個名字：`marketplace.json` 的 entry name（`id` 的名稱部分），以及 manifest 的 `name`。兩者可以不同。
- init 回報的名稱是哪一個，文件沒有說明。

### 46.2 查詢工具（第二輪 R1～R3 與例外收尾修訂後；未對真實 CLI 執行）

**現行工具**（`5bab248`；包裝自 `1acbf01` 起未變）：
- 控制程式 `scripts/b1-04/plugin_id_query.py`；
- 操作包裝 `scripts/b1-04/plugin-id-query.sh`。

兩者取代原本的 shell 管線（原文見 46.6），以及第一輪文件中的三行包裝（見 46.7）。

**操作命令**（未執行；須 Aaron 決定由誰、是否執行；一次，失敗不重試）：

```bash
bash scripts/b1-04/plugin-id-query.sh /Users/mac/.local/bin/claude
```

**包裝是 repo 裡的檔案，整份執行，不逐行貼進互動 shell。** 它做的事（R3）：
- 在 `artifacts/b1-04/` 下以 `mkdir`（不加 `-p`）建立新的證據目錄，已存在或無法建立就停止，不覆寫，查詢不啟動（exit 21）；再預先建立 `result.txt`，失敗也停止（exit 22）。
- 只執行查詢一次，stdout 寫入 `result.txt`。查詢一返回，就把它的退出碼存進變數，後續命令不會改變它。
- 核對 `result.txt` 含 `result=` 行，`exit-code.txt` 也確實寫入。任一不完整，就印「查詢已嘗試，證據不完整（不重跑）」並以 exit 23 結束；不重跑查詢補證據。
- 證據完整時，以查詢自己的退出碼結束：成功 0、`no_match` 3、失敗 2、4～12，不會被改成 0。
- 參數錯誤或找不到 `python3`：exit 20，查詢未啟動。

**執行流程**：
1. **前置檢查**（任一失敗，CLI 呼叫零次，exit 2）：
   - 參數必須恰為 `--cli <絕對路徑>`，路徑是可執行的一般檔案；
   - `tempfile.mkdtemp(prefix="apa-plist.")` 建立暫存目錄，其下 `plugins`、`work` 都以 0700 建立。
   - 建立後立即開啟並保留兩個 file descriptor：上層目錄，以及本次目錄（`O_DIRECTORY|O_NOFOLLOW`）。另記下本次目錄的身分 `(st_dev, st_ino)`，清理時用它（第二輪 R1）。
2. **環境**（與 `internal/buyer` 的 `CLIEnv` 組裝規則一致；C1）：
   - 只取白名單八項 `HOME`、`USER`、`LOGNAME`、`PATH`、`TMPDIR`、`LANG`、`LC_ALL`、`TERM`，空值略過；
   - 再加固定的 `DISABLE_AUTOUPDATER=1`、`CLAUDE_CODE_DISABLE_NONESSENTIAL_TRAFFIC=1`、`CLAUDE_AGENT_SDK_DISABLE_BUILTIN_AGENTS=1`；
   - 最後加方案 A 的 `CLAUDE_CODE_PLUGIN_CACHE_DIR=<暫存目錄>/plugins`。

   環境從空的 dict 組起，父環境不能覆寫固定值，也傳不進其他變數（憑證、代理、`CLAUDE_CONFIG_DIR` 等）。
3. **啟動 CLI 一次**：`<cli> plugin list --json`；工作目錄是空的 `work`；stdin 為 `/dev/null`；stderr 丟到 `/dev/null`；以 `start_new_session` 放進自己的 process group。
4. **有界讀取**（R2）：在讀取期間就套用上限。
   - 從 CLI 啟動起 **30 秒**內，stdout 必須讀完、程序必須結束；否則為 `timeout`。
   - stdout 超過 **1 MiB**（1,048,576 bytes）就停止讀取，為 `output_overflow`。
   - 這些是本查詢的控制規則，不改產品時間限制。
5. **停止與回收**：逾時、超量或中斷時，先對本次 process group 送 SIGTERM，等 **3 秒**，再送 SIGKILL，然後回收。正常結束後，也會對同一個 group 送一次 SIGKILL，結束殘留的子程序。只處理本次啟動的 group，不以名稱搜尋或終止其他 Claude 程序。
   - **中斷（第二輪 R2）**：SIGINT／SIGTERM 的處理器只記錄訊號，不拋出例外。讀取迴圈每 0.1 秒檢查一次，有訊號就以 `interrupted` 進入上述停止流程。
   - 停止流程開始後，再收到的訊號只被記錄：不會跳過 SIGKILL、等待、回收、管線關閉或目錄清理，也不會延長或重設 3 秒寬限，不會再次啟動 CLI。
   - 若在 CLI 啟動前就收到訊號，不啟動 CLI。
   - **一般例外（例外收尾修訂）**：CLI 啟動後，讀取與等待都包在保證執行的 `finally` 收尾裡。`select()`、`os.read()` 等丟出任何例外，都改為固定代碼 `internal_error`，原始例外不輸出，然後一樣走完上述有界停止。
   - 停止流程每一步單獨嘗試：SIGTERM、寬限、SIGKILL、最多 3 秒的等待、對 group 的 SIGKILL、關閉管線。其中一步出錯，其餘照做，出錯的步數記為 `stop_errors=`。
   - 主程序沒有回收時記為 `cli_proc=not_reaped`，不寫 `reaped`。有任何停止錯誤或未回收時，結果不會是 `ok` 或 `no_match`，而是改為 `internal_error`。
   - 限制：程序群組 ID 在仍有成員時不會被重用（POSIX）；脫離本次 process group 的程序，無法由此機制保證回收。
6. **解讀**（R1；只在 CLI exit 0、未逾時、未超量時進行）：
   - **頂層**：必須是 JSON 陣列。JSON 必須是嚴格 UTF-8；不接受重複成員、`NaN`、`Infinity`。
   - **每筆紀錄**：必須是物件，且 `id` 為字串，否則整份拒絕（`record_shape`）。
   - **候選判定**：`id` 以第一個 `@` 分成名稱與 origin；名稱必須**完全等於** `cc-plugin-agents-md` 或 `cc-plugin-plugin-authoring`。其他紀錄整筆略過，不印、不驗證，只計入總數。
   - 候選紀錄的驗證規則：

     | 欄位 | 規則 | 拒絕代碼 |
     |---|---|---|
     | `id` | 全長 ≤ 200 | `reject=id` |
     | origin | 非空，符合 `[A-Za-z0-9][A-Za-z0-9._-]{0,99}`：不含 `/`、空白、控制字元、`@` | `reject=id` |
     | `scope` | 必須是字串，且屬於 `user`、`project`、`local`、`managed`、`session`、`synced` | `reject=scope` |
     | `enabled` | 必須是 JSON 布林值，不轉換字串、數字或 `null` | `reject=enabled` |
     | `errors` | 缺欄位記 0；存在時必須是字串陣列，只輸出長度；`null` 或其他型別一律拒絕 | `reject=errors` |

   - 不修補、不截斷、不猜測 ID。保守拒絕不代表該 ID 在 CLI 中一定無效。
   - **先驗證全部候選，再一次輸出**；任一候選不合法，就不輸出任何候選。
   - 錯誤只輸出固定代碼，不輸出原值、原始 JSON、錯誤內文或 traceback。
7. **清理**（第二輪 R1）：只刪除本次建立的目錄，依它的身分，不依路徑形狀。
   1. 在上層 fd 中以名稱 `lstat`（不跟隨 symlink）。必須是實體目錄，且 `(st_dev, st_ino)` 等於建立時記下的值；symlink、被換成其他目錄或身分不符都拒絕，不刪任何東西。
   2. 內容經由**本次目錄自己保留的 fd** 逐層刪除。子目錄以 `O_NOFOLLOW` 開啟，symlink 只移除不跟隨。所以路徑之後被換掉，也不會沿新路徑刪除。
   3. 唯一依名稱進行的一步，是最後的非遞迴 `rmdir`。執行前再核對一次名稱仍是本次目錄；`rmdir` 只能移除空目錄。
   4. 拒絕或失敗時回報 `dir_cleanup=failed`，保留剩下的內容，不擴大刪除範圍。
   - 剩餘限制：在第二次核對與 `rmdir` 之間，若名稱被換成另一個**空**目錄，理論上仍可能被移除；非空目錄、symlink、其他位置都不受影響。
   - 清理涵蓋前置失敗、正常結束、執行失敗與中斷。

**輸出**：只寫到 stdout，原始 stdout 只留在記憶體。
- 第一行固定為 `query=plugin_list v3`。
- 成功時，每個候選一行 `candidate {"id","origin","scope","enabled","errors"}`，接著 `matched=<n> entries=<m>`。
- 失敗時只有 `reason=` 或 `reject=` 的固定代碼。
- 最後六行固定：
  - `result=<代碼>`
  - `cli=<not_started|start_failed|exit_<n>|exit_signal_<n>>`
  - `cli_proc=<not_started|reaped|not_reaped>`：主程序是否已回收
  - `dir_cleanup=<ok|failed|none>`：**只表示暫存目錄的清理**，不代表程序也已回收
  - `signals=<n>`：收到的 SIGINT／SIGTERM 次數
  - `stop_errors=<n>`：停止流程中出錯的步數
- `cli_proc=reaped` 只代表主程序已回收，且已對本次 group 送出 SIGKILL；脫離 group 的程序不在保證範圍內。

**退出碼**：

| exit | `result=` | 意思 |
|---|---|---|
| 0 | `ok` | 至少一筆候選，全部合法，清理成功 |
| 2 | `precheck_failed` | 參數或暫存目錄失敗，CLI 未啟動 |
| 3 | `no_match` | 查詢成功但 `matched=0`：**不等於 plugin 不存在**；停止，不改成列出全部 id，不擴大候選，不重試 |
| 4 | `cli_failed` | CLI 非零退出或無法啟動；即使 stdout 是合法 JSON 也不解讀 |
| 5 | `timeout` | 超過 30 秒 |
| 6 | `output_overflow` | stdout 超過 1 MiB |
| 7 | `parse_error` | 非 JSON、非陣列、紀錄格式不符 |
| 8 | `format_rejected` | 候選欄位不符規則 |
| 9 | `cleanup_failed` | 主要作業成功，但目錄清理失敗或被拒絕；不算完整成功。主要作業失敗時保留主要代碼，另以 `dir_cleanup=failed` 標示 |
| 10 | `interrupted` | 腳本收到 SIGINT／SIGTERM。程序與目錄的實際狀態，看 `cli_proc=`、`dir_cleanup=`；不以此宣稱 CLI 已停止 |
| 11 | （無 `result=` 行） | 報告寫出失敗 |
| 12 | `internal_error` | 其他非預期錯誤（含讀取時的例外、停止步驟出錯、主程序未回收）。只輸出固定代碼；程序與目錄狀態看 `cli_proc=`、`stop_errors=`、`dir_cleanup=` |
| 20 | （包裝） | 參數錯誤或找不到 `python3`；查詢未啟動 |
| 21 | （包裝） | 證據目錄已存在或無法建立；不覆寫，查詢未啟動 |
| 22 | （包裝） | `result.txt` 無法建立；查詢未啟動 |
| 23 | （包裝） | 查詢已嘗試，但 `result.txt` 或 `exit-code.txt` 不完整；不重跑 |

- Python 自身無法執行控制程式時也可能回 exit 2，但 `result.txt` 不會有 `result=` 行，包裝會改回 exit 23。
- 一次執行最多啟動 CLI 一次，任何失敗都不自動重試。只要 `cli=` 不是 `not_started`，就不能聲稱「沒有執行」或「沒有背景活動」。

### 46.3 誰執行（擇一，Aaron 決定）

| 方式 | Claude Code 接觸的內容 | 說明 |
|---|---|---|
| **甲（建議）Aaron 自行執行**操作命令，只回傳 `result.txt` 與 `exit-code.txt` | 只有固定格式的報告 | Claude Code 不執行 CLI、不讀 HOME。 |
| 乙：核准 Claude Code 執行一次 | 同上 | 須在沙箱外執行（CLI 需要 HOME）。 |

### 46.4 已知限制與風險

- **設定視角不同**：環境變數的組裝規則一致，不代表查詢與 APA 使用相同的設定視角。APA 使用 `--restricted`；本查詢未確認該子命令對它的支援與行為，**不能把合併設定中的 enabled 當成 APA runtime 已載入的證據**。本輪不自行加入 `--restricted`，也不執行 help 或版本查詢。
- **名稱可能對不上**：`id` 的名稱部分是 marketplace entry name，init 的名稱可能是 manifest name；所以 `no_match` 不代表 plugin 不存在。
- **`scope` 不等於強制啟用的設定層**：`scope: managed` 是安裝範圍。若該 id 被 managed 強制啟用，`--settings` 無法覆寫；**取得完整 id 不能證明方案 C 可行**。
- **CLI 的行為**：
  - `plugin list` 是否有網路活動、是否觸發背景 marketplace 更新、是否讀登入資料，文件沒有說明。
  - 空的 plugin 根目錄可能被寫入，結束時隨暫存目錄刪除。
  - 不能保證 CLI 不寫入其他日常狀態。
  - CLI 版本沒有另外檢查（第 45 節的 runtime 為 `2.1.293`）。
- **官方格式依據**：[Plugin CLI reference 的 plugin list](https://code.claude.com/docs/en/plugins/cli-reference#plugin-list)。這是文件依據，不是對本機 `2.1.293` 的實測保證。

### 46.5 取得結果之後（另行決定，不自動進行）

- 有 id，且 `enabled: true`：可以設計 C 的受控實驗。做法是在子程序加 `--settings '{"enabledPlugins":{"<id>":false}}'`，只針對這兩個 id，flag 來源只對該次 session 有效。依 42.5、42.6 的模式寫實作計畫與診斷條件，交 review，實作與真實診斷各自另行核准。
- `scope: managed`，或有其他跡象顯示是受管設定強制啟用：C 可能無效，改由 Aaron 評估 42.4 的第 2、3 項。
- 結果不改 Gate、不放行這兩個 plugin。

L1-b、L1-c 不標整體通過；B1-04 仍 InProgress。

### 46.6 修訂紀錄：plugin 查詢腳本 R1、R2、C1（2026-10-08；Claude Code 執行，待 Codex 限定複核）

> **plugin 查詢腳本 R1、R2、C1 已修訂，定向 fake 驗證完成，待 Codex 限定複核。** 真實查詢尚未核准、未執行；Gate、認證清單及產品行為不變。L1-b、L1-c 未標整體通過，B1-04 維持 InProgress。

- **依據**：vault〈Claude-Code-B1-04-plugin查詢腳本R1-R2-C1修訂派工-aeca412-20261008〉。
- **提出修訂的 Codex review**：只核對文件、原始碼與官方文件，沒有執行查詢腳本或真實 CLI，不記為測試通過。
- **開工核對**：分支 `b1-04-live`，HEAD `aeca412`；工作區只有 `?? demo`。產品基準 `40bd263` 之後只有文件差異。Host、Probe、Gate、認證接受清單、版本釘選、產品啟動參數都沒有修改。

| 項目 | 原寫法（`46756f6`、`aeca412`） | 現行 | 位置 |
|---|---|---|---|
| R1 值的驗證 | 過濾器直接輸出 `r.get("scope")`、`r.get("enabled")`；`errors` 未驗型別；邊找邊印 | 候選判定、ID 長度與 origin 字元規則；`scope` 限六個官方值；`enabled` 須為布林；`errors` 須為字串陣列，缺欄位才記 0；拒絕重複成員與 NaN；先驗全部候選再一次輸出；錯誤只印固定代碼 | 46.2 第 6 點、`plugin_id_query.py` 的 `judge` |
| R2 失敗退出 | 非 JSON、非陣列分支 `exit(0)`；只印 CLI 退出碼；`rm -rf` 可能使整體成功；沒有時間與大小上限 | 單一控制程式；固定退出碼 0、2～12；CLI 非零、解析失敗、拒絕、逾時、超量都不回成功；清理失敗另行標示，且不覆蓋主要失敗；30 秒、3 秒寬限、1 MiB，在讀取期間生效；只處理本次 process group；只刪本次已驗證的暫存目錄；最多啟動一次 | 46.2、`run_cli`、`remove_own`、`main` |
| C1 環境 | 漏了 `LC_ALL`、`TERM`，且帶入空值；寫成「與 APA 子程序相同」 | 與 `CLIEnv` 規則一致（八項、空值略過、三個固定值），加方案 A 的根目錄；保留「組裝規則一致不代表設定視角相同」的說明 | 46.2 第 2 點、46.4、`build_env` |

**定向 fake 驗收**（`scripts/b1-04/test_plugin_id_query.py`；Claude Code 執行，待 Codex 核對）：
- **怎麼測到實際的檔案**：每個案例都載入實際的 `plugin_id_query.py` 並呼叫 `main()`，不另寫替身。
  - 只有逾時、超量、暫存目錄失敗三類案例，會把 `TIMEOUT`／`GRACE`、`MAX_OUTPUT`、`PREFIX` 調小或改掉，讓案例快速結束。
  - 另一案例由 `/bin/bash` 直接執行檔案本身。
- **測試條件**：CLI 一律是暫存目錄中的假 shell 腳本，不使用正式 CLI 路徑，也不讀本機 plugin 清單。環境值全部是合成的，HOME 是測試建立的空目錄。

| 測試 | 對應派工 §5 |
|---|---|
| `test_valid_candidate` | 合法候選；errors 只輸出筆數；`mcpServers`、`installPath` 與非候選的標記不外洩 |
| `test_no_match` | 非候選不輸出；零筆為 `no_match`（exit 3） |
| `test_not_json_or_not_list` | 非 JSON、非陣列、紀錄格式、重複成員、NaN、非 UTF-8 → exit 7，沒有 traceback |
| `test_bad_types_and_ids` | scope 物件或未知值、enabled 字串／null／數字、errors 型別、空 origin、路徑、空白、控制字元、多個 `@`、過長 → exit 8 |
| `test_no_partial_output` | 第二筆不合法時，第一筆也不輸出 |
| 所有案例的共同檢查 | 輸出與 stderr 中找不到 `MARK` 標記；假 CLI 的 stderr 標記不出現 |
| `test_cli_nonzero` | CLI exit 3、stdout 合法 → 仍 exit 4，不輸出候選 |
| `test_precheck` | 七種參數錯誤與暫存目錄失敗 → exit 2，假 CLI 呼叫零次 |
| `test_timeout_and_overflow` | 忽略 SIGTERM 的 CLI 逾時（SIGKILL）、輸出灌水超量 → exit 5／6，10 秒內結束；主程序與殘留子程序都已結束 |
| `test_leftover_child_after_normal_exit` | 正常結束後殘留的子程序也被結束 |
| `test_interrupted` | 對腳本送 SIGTERM → exit 10，CLI 已停、已清理 |
| `test_cleanup` | 清理失敗 → exit 9（主要成功）或保留主要代碼 4；`remove_own` 拒絕刪除 HOME、TMPDIR 本身與其他位置的目錄；他人目錄不被刪除 |
| `test_environment` | 八項白名單（空值略過）、三個固定值不被父環境覆寫、方案 A 根目錄、沒有憑證或其他變數；argv 固定、工作目錄為空的 `work`、呼叫一次 |
| `test_file_from_bash` | `/bin/bash` 3.2 直接執行檔案，`$?` 為 0 |
| 每個案例 | 假 CLI 最多被呼叫一次，沒有重試 |

**結果**（macOS；證據 `artifacts/b1-04/plugin-id-query-fake-20261008T031447Z/`）：
- 13 個測試，在 `/usr/bin/python3`（3.9.6）與 `python3`（3.12.6）下都 OK。
- `/bin/bash` 是 3.2.57。
- 命令：`<python> -I -m unittest discover -v -s scripts/b1-04 -p 'test_*.py'`。
- `py39.txt`、`py312.txt`、`exit-codes.txt`、`versions.txt` 中都沒有 `MARK`。

**故障試跑**（`fault-trials.txt`）：在副本中逐一植入 13 種錯誤，13 種都被測試抓到：
- 不驗 scope、放寬 enabled、不驗 errors 型別、不驗 origin；
- 非 JSON 當成功、忽略 CLI 退出碼、移除最後的 group SIGKILL；
- 傳入父環境、隱藏清理失敗、`remove_own` 不檢查；
- 關掉輸出上限、顯示 stderr、印出非候選。

**失敗嘗試與過程**：
- 第一版測試用「不可寫的 TMPDIR」模擬暫存目錄失敗，但 Python 的 `tempfile` 會改用其他可寫目錄，不能模擬。已改為讓 `mkdtemp` 本身失敗。
- 迴圈中重複 `setUp` 會留下暫存目錄，已改為全部清理。
- 也補上「正常結束後殘留子程序」的測試。修正前它不是一個獨立案例，故障試跑可能抓不到「移除最後的 group SIGKILL」。
- 一次故障試跑的命令（含 `rm -rf`）被權限拒絕，改用新的 `mktemp` 目錄重跑，結果如上。

**沒有做的事**：沒有執行真實 `claude plugin list`、版本查詢、C0′ 或任何真實 CLI；沒有讀 HOME、keychain、受管設定或 pixel-office；沒有重跑產品 Go 全套、容器、255 項突變或 Unity。既有結果不掛到本輪。

**待決定**：
1. Codex 限定複核本次修訂。
2. 複核通過後，由 Aaron 決定甲或乙，並另行核准一次真實查詢。

L1-b、L1-c 不標整體通過；B1-04 仍 InProgress。

### 46.7 修訂紀錄：plugin 查詢工具第二輪 R1～R3（2026-10-08；Claude Code 執行，待 Codex 限定複核）

> plugin 查詢工具第二輪 R1～R3 已修訂（`1acbf01`），定向 fake 驗證完成，待 Codex 限定複核。真實查詢尚未核准、未執行；Gate、認證清單、產品行為、方案 A 都不變。L1-b、L1-c 未標整體通過，B1-04 維持 InProgress。

- **依據**：vault〈Claude-Code-B1-04-plugin查詢工具第二輪R1-R3修訂派工-9d41696-20261008〉。本輪 R1～R3 是新編號，與 46.6 的第一輪 R1、R2、C1 不同。
- **開工核對**：分支 `b1-04-live`，HEAD `9d41696`；工作區只有 `?? demo`。工具與測試的基準是 `53dce97`，其後只有文件提交。

**Codex 本次 review 的證據歸屬**（分開記錄）：
- **Codex 獨立執行**：在斷網容器中做了本輪三項定向探測。條件：來源唯讀、非 root、空環境加合成的 HOME／TMPDIR，並有 CPU、記憶體、程序數、檔案大小與時間限制；沒有執行真實 CLI。
  - 中斷探測的前兩次沒有到達有效檢查點，不算產品失敗；有效的 R2 結果是之後以受控注入取得。
  - 探測輸出只在 Codex 的工具紀錄中，沒有另存成 repo 證據檔。本節不列它的 artifacts 路徑。
- **Claude Code 執行、Codex 核對紀錄**：`artifacts/b1-04/plugin-id-query-fake-20261008T031447Z/` 中，Python 3.9、3.12 各 13 項通過的輸出與退出碼，以及 13 種故障試跑的摘要。Codex 沒有獨立重跑這套測試，也不宣稱核對過摘要沒有列出的每項故障細節。

| 項目 | 原寫法（`53dce97`、`1a782a1`） | 現行（`1acbf01`） | 位置 |
|---|---|---|---|
| R1 目錄身分 | `remove_own(path)` 先 `realpath()`，再核對上層目錄與前綴後 `rmtree`。路徑被換成指向同前綴旁觀者的 symlink 時，會刪掉旁觀者 | `Scratch` 從建立起保留上層與本次目錄的 fd 及 `(st_dev, st_ino)`；以 `lstat` 拒絕 symlink 與身分不符；內容經由本次目錄的 fd 刪除；唯一依名稱的非遞迴 `rmdir` 前再核對一次 | `Scratch`、`clear`、`remove_own`；46.2 第 1、7 點 |
| R2 中斷 | 處理器拋出 `Interrupted`；在終止流程中收到的訊號會跳過 SIGKILL、等待與管線關閉，報告卻仍是 `cleanup=ok` | 處理器只記錄；停止流程（SIGTERM、3 秒、SIGKILL、回收、關閉）一定完成，不被跳過或延長；報告分開列 `cli_proc=` 與 `dir_cleanup=`，並加 `signals=` | `on_signal`、`run_cli`、`main`；46.2 第 5 點、輸出、exit 10 |
| R3 包裝退出碼 | 文件中的三行包裝以 `echo` 結束，查詢 exit 2 時整份 shell 回 0 | `scripts/b1-04/plugin-id-query.sh`：查詢一返回就保存退出碼；核對兩個證據檔；完整時 `exit` 查詢碼，否則 exit 23；證據目錄不覆寫；自有代碼 20～23 | 包裝檔；46.2 操作命令與退出碼 |

**定向 fake 驗收**（`scripts/b1-04/test_plugin_id_query.py`，19 個測試；Claude Code 執行，待 Codex 核對）：
- CLI 一律是假的 shell 腳本，每個案例最多啟動一次；不用正式 CLI 路徑、真實設定或 plugin 清單；HOME、TMPDIR 是測試建立的合成目錄。
- R1、R2 的案例載入實際的 `plugin_id_query.py`；R3 的案例以 `/bin/bash` 執行實際的 `plugin-id-query.sh`。

| 測試 | 對應 |
|---|---|
| `test_cleanup_identity` | **R1**，七種情況：<br>1. 原目錄未動：可清理；<br>2. 換成指向同上層、同前綴旁觀者的 symlink：拒絕，旁觀者與檔案不變；<br>3. 換成另一個同名實體目錄：拒絕，該目錄與檔案不變；<br>4. 換成指向本次（已搬走）目錄的 symlink：拒絕，且不動任何內容；<br>5. 指向 HOME、TMPDIR 本身的 symlink：拒絕；<br>6. 第一次核對通過後立刻換成 symlink：內容只經本次 fd 刪除，旁觀者不變，最後的 `rmdir` 被第二次核對擋下；<br>7. 第一次核對後換成空目錄：第二次核對擋下 `rmdir`，空目錄保留。 |
| `test_cleanup` | 清理失敗：主要成功時 exit 9；主要失敗時保留 exit 4。另以 `dir_cleanup=failed` 標示 |
| `test_interrupted` | **R2-1**：正常讀取期間收到 SIGTERM → exit 10，`cli_proc=reaped`，`dir_cleanup=ok`，`signals=1` |
| `test_interrupt_in_grace` | **R2-2**：假 CLI 忽略 SIGTERM（只記錄）。以它記錄到 TERM 作為就緒條件，確認已進入寬限期，再對腳本送一次 SIGTERM。結果：仍以 SIGKILL 結束（`cli=exit_signal_9`）、已回收、目錄已清，從注入起算在寬限 2 秒加 1.5 秒內結束 |
| `test_interrupt_burst_in_grace` | **R2-3**：寬限期間交替送 6 次 SIGTERM／SIGINT。不跳過收尾，不延長期限（同上界限），`signals=7` |
| `test_timeout_and_overflow`、`test_leftover_child_after_normal_exit`、其他既有案例 | **R2-4**：正常、逾時、超量等原有路徑仍成立 |
| `Wrapper.test_outcomes_kept` | **R3**：成功時外層 0；`no_match` 外層 3；已啟動後失敗外層 4；CLI 不存在（未啟動）外層 2。`exit-code.txt` 與外層一致 |
| `Wrapper.test_evidence_not_saved` | **R3**：`result.txt` 遺失、`exit-code.txt` 無法寫入 → 外層 23，有固定提示，查詢只啟動一次 |
| `Wrapper.test_evidence_directory` | **R3**：證據上層不存在、同名目錄已存在（既有內容不變）、上層是檔案 → 21，CLI 呼叫零次；參數錯誤 → 20 |

**程序存活檢查**：以 `/bin/ps -o stat=` 區分三種狀態：已不存在（空字串）、已退出未回收（`Z`）、仍在執行。測試要求假 CLI 主程序已不存在。這需要在 Claude Code 沙箱外執行，因為沙箱禁止 `/bin/ps`。

**命令與結果**（macOS；沙箱外；證據 `artifacts/b1-04/plugin-id-query-fake-r2-20261008T034502Z/`）：
- `/usr/bin/python3 -I -m unittest discover -v -s scripts/b1-04 -p 'test_*.py'`：Python 3.9.6，19 項 OK，exit 0。
- `python3 -I -m unittest discover -v -s scripts/b1-04 -p 'test_*.py'`：Python 3.12.6，19 項 OK，exit 0。
- `/bin/bash` 3.2.57。證據檔中都沒有 `MARK` 標記。
- **反向基準**（`reverse-baseline.txt`）：舊版（`53dce97` 工具、`1a782a1` 文件包裝）在合成目錄下重現了三個缺陷：
  - R1：舊 `remove_own` 回傳 True，旁觀者目錄被刪；
  - R2：舊版在寬限期間收到中斷，回 exit 10、`cleanup=ok`，但假 CLI 仍在執行（`Ss`）；探測後已另行終止它的 group；
  - R3：舊包裝外層 exit 0，而 `exit-code.txt` 是 `exit=2`。
- **故障試跑**（`fault-trials.txt`）：在新版副本中逐一放回 12 種舊缺陷或其變形。
  - 第一次 11 種被抓到；「`lstat` 改成跟隨 symlink」沒有被抓到。
  - 補上「指向本次目錄的 symlink」案例後，該項也被抓到。

**失敗嘗試與過程**：
- 沙箱內執行時，`/bin/ps` 被拒，兩個 R2 測試出錯（環境問題），改在沙箱外執行。
- 第一版 `Wrapper` 繼承 `Case`，重複執行了 `Case` 的測試；已改為只沿用輔助函式。
- `test_evidence_not_saved` 的第一版，在迴圈開始前就把第一個證據目錄的路徑寫進第二個案例，導致 `chmod` 作用在舊目錄而得到 0。已改為每個案例使用自己的證據上層。
- 三次含 `rm -rf` 的除錯或試跑命令被權限拒絕，已改用新的暫存目錄。
- 失敗的嘗試都沒有對真實 CLI 或真實資料執行。

**仍未驗證的限制**：
- 沒有對真實 `claude plugin list` 執行，`2.1.293` 的實際輸出與行為未知。
- 第二次核對與 `rmdir` 之間換成**空**目錄的極小時窗（46.2 第 7 點）。
- 脫離本次 process group 的程序。
- 只在 macOS（bash 3.2、Python 3.9／3.12）驗證；Linux 未驗證。
- 本輪沒有重跑產品 Go 全套、完整七步容器、255 項突變或 Unity。

**待決定**：
1. Codex 限定複核。
2. 複核通過後，由 Aaron 決定執行者，並另行核准一次真實查詢。

### 46.8 修訂紀錄：plugin 查詢工具例外收尾（2026-10-08；Claude Code 執行，待 Codex 限定複核）

> **一般例外下的程序收尾已補齊（`5bab248`），macOS 定向 fake 驗證完成，待 Codex 限定複核。** 真實查詢尚未核准、未執行。Linux 目錄清理差異另列為限制，不算 Linux 驗證通過。L1-b、L1-c 不標整體通過；B1-04 仍 InProgress。

- **依據**：vault〈Claude-Code-B1-04-plugin查詢工具例外收尾修訂派工-30fce00-20261008〉。補的是第二輪 R2 的一般例外路徑；第二輪的目錄身分、訊號只記錄、包裝保留退出碼都保留。
- **開工核對**：分支 `b1-04-live`，HEAD `30fce00`；工作區只有 `?? demo`。工具、包裝、測試自 `1acbf01` 起沒有變動。

**Codex 本次 review 的證據歸屬**（分開記錄）：
- **Codex 獨立執行**：在 Linux 斷網容器中執行。條件：來源唯讀、非 root、合成 HOME／TMPDIR，並有資源與時間限制。
  - 項目：六項定向回歸、一般 I/O 例外探測、目錄列舉釐清探測。
  - 這不是完整容器驗證，也沒有執行真實 CLI。
  - 結果只在 Codex 的工具紀錄中，沒有 repo 證據路徑。
- **Claude Code 執行、Codex 核對紀錄**：`artifacts/b1-04/plugin-id-query-fake-r2-20261008T034502Z/` 中，Python 3.9、3.12 各 19 項通過的輸出、退出碼，以及反向基準與故障試跑摘要。Codex 沒有獨立重跑 macOS 的 19 項，也不把故障摘要當成每項細節都已獨立驗證。
- **Codex 重現的缺口**：在忽略 SIGTERM 的假 CLI 輸出就緒後，於讀取迴圈注入 `OSError`。結果：exit 12、`result=internal_error`、`cli=started`、`cli_proc=unknown`，假 CLI 仍在執行（`S`）；探測後由 Codex 另行終止並回收。這只證明一般例外路徑會留下程序，不代表真實 Claude CLI 發生過同類錯誤。

**修正**（`5bab248`；`plugin_id_query.py`）：
- 新增 `stop(proc, pgid)`，集中有界停止的每一步：
  - 每步以 `attempt()` 單獨嘗試，出錯就計數、繼續下一步；
  - `poll()` 失敗時視為仍在執行，停止流程照做；
  - SIGKILL 後的 `wait` 也有上限（3 秒）。
- `run_cli()`：CLI 啟動後，讀取與等待放在 `try/except Exception/finally` 中。
  - 例外只轉為固定代碼 `internal_error`，例外內容不保存、不輸出；
  - `finally` 一定呼叫 `stop()`；
  - 回收後才標 `reaped`，否則為 `not_reaped`、`cli=exit_unknown`。
- `main()`：報告新增 `stop_errors=`；有停止錯誤或未回收時，結果不得為 `ok`／`no_match`。
- 訊號處理器仍只記錄訊號。程序回收（`cli_proc=`、`stop_errors=`）與目錄清理（`dir_cleanup=`）分開報告。
- 只處理本次啟動的 group；脫離 group 的程序仍屬既有限制。

**定向 fake 回歸**（`test_plugin_id_query.py`，22 個測試，新增 3 個；Claude Code 執行，待 Codex 核對）：
- 新測試用的注入驅動程式，載入實際的 `plugin_id_query.py`，替換三個位置之一，讓它丟出合成的 `OSError`：`select.select`、`os.read`，或停止流程中的 SIGTERM `killpg`。
- 只在假 CLI 已啟動並寫出就緒檔之後才丟出；假 CLI 忽略 SIGTERM。
- 驅動程式在 `main()` 前後各數一次本程序開啟的 pipe 數。只數 pipe，因為 Python 可能在 `tempfile` 使用後保持 `/dev/urandom` 開啟，那不是 CLI 的管線。

| 測試 | 確認 |
|---|---|
| `test_select_error_stops_cli` | 讀取時 `select()` 出錯 → exit 12、`result=internal_error`、`cli=exit_signal_9`（SIGKILL）、`cli_proc=reaped`、`stop_errors=0`。假 CLI 主程序已不存在（`ps` 區分不存在、zombie、執行中）；pipe 數前後相同；只啟動一次；沒有 traceback 或 `MARK` 內文 |
| `test_read_error_stops_cli` | `os.read()` 出錯，確認同上 |
| `test_stop_step_error_counted` | 逾時後的 SIGTERM 步驟出錯 → 其餘步驟照做（SIGKILL、回收、關閉）：exit 5、`cli_proc=reaped`、`stop_errors=1`，不回報成功 |
| 既有 19 項 | 正常讀取中斷、寬限期間中斷、重複訊號、逾時、超量、目錄身分、包裝等，全部仍通過 |

每個新測試都另外登記清理：測試結束時，對本案假 CLI 的 group 送 SIGKILL，只針對它。

**命令與結果**（macOS；沙箱外執行，因為需要 `/bin/ps`；證據 `artifacts/b1-04/plugin-id-query-fake-r3-20261008T041832Z/`）：
- `/usr/bin/python3 -I -m unittest discover -v -s scripts/b1-04 -p 'test_*.py'`：Python 3.9.6，22 項 OK，exit 0。
- `python3 -I -m unittest discover -v -s scripts/b1-04 -p 'test_*.py'`：Python 3.12.6，22 項 OK，exit 0。
- 結束後 `fake-claude` 殘留程序數為 0（`leftover-fakes.txt`）。證據中沒有 `MARK`。
- **反向驗證**（`reverse-teardown.txt`）：在副本中拿掉修正，都以 `AssertionError` 被抓到，不是編譯或前置失敗。
  - 拿掉保證收尾（例外跳過 `stop()`）：`test_select_error_stops_cli`、`test_read_error_stops_cli` 失敗。
  - 拿掉逐步保護（一步出錯就跳過其餘）：`test_stop_step_error_counted` 失敗。

**失敗嘗試與過程**：
- 新測試第一版以「所有 fd 數」比較，三項都多 1。逐一檢查後，多出的是字元裝置（`S_IFCHR`），推測是 Python 保留的 `/dev/urandom`，不是管線；正常路徑的直接實驗也沒有管線殘留。檢查已改成只數 pipe，沒有放寬「管線必須關閉」的要求。
- 第二版的注入驅動程式在 read 模式也替換了 `killpg`，使 `stop_errors=1`。這是測試錯誤，已改為每種模式只替換自己的位置。
- 反向驗證後，第一次粗略計數殘留程序得到 8 行，那是 `grep` 與本身命令列的自我匹配。排除後的精確列表為 0，最後的證據也記為 0。

**Linux 目錄清理差異**（未解，另列限制；不和上面的程序殘留混為一項）：
- Codex 在 Linux 斷網容器重跑六項定向回歸，一項通過、五項因目錄清理結果不符而失敗：

  | 測試 | 結果 |
  |---|---|
  | `Case.test_cleanup_identity` | 正常目錄的清理回 False |
  | `Case.test_interrupt_in_grace` | 主程序回收符合，但 `dir_cleanup=failed` |
  | `Case.test_interrupt_burst_in_grace` | 主程序回收與訊號數符合，但 `dir_cleanup=failed` |
  | `Wrapper.test_outcomes_kept` | 因清理失敗回 9，不是預期的 0 |
  | `Wrapper.test_evidence_not_saved` | 主要查詢碼為 9，不是測試預期的 0 |
  | `Wrapper.test_evidence_directory` | 通過 |

- 後續純合成目錄的探測發現，以 descriptor 和以路徑列舉目錄時結果有差異，原因尚未確認。
- 這些結果**不算 Linux 驗證通過**，也不斷言是五個獨立的產品缺陷，或是 Docker、Python 的問題。
- 本輪沒有為此改清理架構：沒有放寬身分檢查、沒有改回跟隨 symlink、沒有把拒絕記成成功。本輪也沒有在 Linux 重跑。真實查詢預定在 macOS 執行，但 Linux 差異仍是未解限制。

**仍未驗證**：
- 真實 `claude plugin list` 的輸出與行為。
- Linux 上的目錄清理。
- 第二次核對與 `rmdir` 之間換成空目錄的極小時窗。
- 脫離本次 process group 的程序。
- 本輪沒有重跑產品 Go 全套、完整容器、255 項突變或 Unity。

**待決定**：
1. Codex 限定複核。
2. 是否需要另外處理 Linux 目錄清理差異。
3. 複核通過後，由 Aaron 決定執行者，並另行核准一次真實查詢。

### 46.9 驗收紀錄：`5bab248` 通過限定範圍技術複核（2026-10-08）

> **`5bab248` 通過本次限定範圍技術複核，一般例外下的程序收尾問題關閉。** 沒有發現需要再阻擋這項修正的新問題。

- **確認內容**：
  - `select`、`read` 發生例外後，仍會執行停止、回收與管線關閉；
  - SIGTERM 步驟失敗，不會跳過後續的 SIGKILL 與回收；
  - 回收完成才標為 `reaped`；錯誤維持固定代碼，不輸出原始例外。
- **證據歸屬**（分開記錄）：
  - **Codex 獨立執行**：在斷網、來源唯讀的容器中做了三個定向 fake 探測，全部通過，整體 exit 0；確認主程序已回收、管線已關閉。這不是完整測試或完整容器驗證，輸出只在對話工具紀錄中。
  - **Claude Code 執行、Codex 核對紀錄**：macOS 上兩版 Python 各 22 項通過、退出碼與反向驗證摘要（`artifacts/b1-04/plugin-id-query-fake-r3-20261008T041832Z/`）。Codex 沒有重跑這兩套完整測試。
- **仍未解決的限制**（不寫成已解決）：
  - Linux 的目錄清理差異（46.8）：可另列工作；不必為了預定在 macOS 執行的一次查詢現在擴大重做。
  - 最後 `rmdir` 前被換成空目錄的極小時窗（46.2 第 7 點）。
  - 脫離本次 process group 的程序。
- **下一步**：真實查詢須 Aaron 另行指定執行者（46.3 的甲或乙），並核准一次嘗試；本次 review 不算執行核准。
- 本輪只同步文件：沒有重跑這項修正的測試，沒有改程式。L1-b、L1-c 不標整體通過；B1-04 仍 InProgress。

### 46.10 執行紀錄：一次真實 plugin ID 查詢（2026-10-08；Claude Code 執行，待 Codex 核對）

> **結果：`no_match`（exit 3）。** CLI 正常結束，列出 1 筆，名稱不是 `cc-plugin-agents-md` 或 `cc-plugin-plugin-authoring`。依規則停止：不重試、不改成列出全部 id、不擴大候選。完整 plugin id 仍未取得。

- **核准**：Aaron 10/8 核准 Claude Code 在 macOS 依 §46 的已審查包裝執行一次真實查詢。核准範圍：
  - 允許該次 CLI 使用既有 HOME、登入與網路環境；理解可能有背景活動或日常狀態寫入；
  - 只保存白名單過濾結果，不保存原始輸出；
  - 失敗、逾時或零筆符合都停止，不重試、不擴大；
  - 不修改設定、Gate、認證清單，不執行 C0′。
- **開工核對**：分支 `b1-04-live`，HEAD `9f90e52`；工作區只有 `?? demo`。`scripts/` 自 `5bab248`（已通過複核）之後沒有變動。
- **命令**（在 repo 根目錄執行一次；在 Claude Code 沙箱外執行，依核准使用既有環境）：

  ```bash
  bash scripts/b1-04/plugin-id-query.sh /Users/mac/.local/bin/claude
  ```

  包裝的退出碼：3。
- **證據**（`artifacts/b1-04/plugin-id-query-20261008T043401Z/`，`artifacts/` 不進版控；只有白名單結果，沒有原始輸出）：
  - `result.txt`（全文）：

    ```text
    query=plugin_list v3
    matched=0 entries=1
    result=no_match
    cli=exit_0
    cli_proc=reaped
    dir_cleanup=ok
    signals=0
    stop_errors=0
    ```

  - `exit-code.txt`：`exit=3`。
  - 暫存目錄（`apa-plist.*`）已刪除；repo 工作區仍只有 `?? demo`。

**解讀**（照 46.2、46.4，不額外推論）：
- `no_match` **不等於 plugin 不存在**。
  - 查詢在 APA 子程序相同的環境組裝下執行（含方案 A 的空 plugin 根目錄），但沒有 `--restricted`，設定視角與 APA runtime 不同。
  - `id` 的名稱部分是 marketplace entry name，init 回報的可能是 manifest name，兩者可以不同。
- 列出的 1 筆不是候選，依規則**沒有輸出**它的 id、scope 或其他內容，本紀錄也不推測它是什麼。
- CLI 有啟動（`cli=exit_0`），所以不能聲稱沒有執行或沒有背景活動；`cli_proc=reaped`、`stop_errors=0`、`dir_cleanup=ok`。
- 42.4 第 1 項（取得完整 id，再評估 `--settings` 方案）在這次查詢**沒有得到結果**。

**沒有做的事**：沒有重試、沒有改成列出全部 id、沒有擴大候選；沒有修改設定、Gate、認證接受清單、啟動參數或產品程式；沒有執行 C0′、版本查詢或登入檢查。

**待決定**（都須 Aaron 決定；不自動進行）：
1. Codex 核對本次紀錄。
2. 取得完整 id 的其他方式（例如由 Aaron 在自己的環境查看，或另行核准一次範圍不同的查詢，如列出全部 id 或改變環境），須另行設計與核准。
3. 或改評估 42.4 的第 2、3 項：調整受管設定，或以 `--safe-mode` 只驗 MCP。

L1-b、L1-c 不標整體通過；B1-04 仍 InProgress；不合併、不推送、不標 Done。
