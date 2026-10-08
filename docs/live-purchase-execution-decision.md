# 第一筆真實請購的執行路線評估

> **狀態：執行路線評估，待 Codex review 與 Aaron 決策；未核准實作或真實執行。**
> 2026-10-08；分支 `b1-04-live`，評估基準 HEAD `bd3ac81`（產品程式基準 `40bd263`，查詢工具 `5bab248`）。本文件不改變現行 Gate、認證接受清單或任何執行限制，也不代表 plugins 已獲接受、零 plugins 要求已取消、來源已確認或正式請購已放行。
> 依據：vault〈Claude-Code-B1-04-回歸真實請購主線與執行邊界評估派工-bd3ac81-20261008〉。只做原始碼與文件唯讀核對；沒有跑測試、Docker、Unity 或任何 CLI。

## 1 結論與目前真正的阻擋

**推薦路線：路線一（維持零 plugins），以 APA 專用的 Claude 設定目錄（`CLAUDE_CONFIG_DIR`）取代方案 A 只換 plugin 快取根目錄的做法。**
- 這條路不改 Gate 的 plugins 規則。
- 代價是 Aaron 要為這個目錄另外登入一次。
- 它的效果要用一次診斷型 C0′ 才能確認，而這次診斷的結果，正好決定要不要轉向備案。

**條件式備案：路線二（接受有限且明確的 runtime 組成）。** 目前**尚不可採用**。真正缺的決策資訊只有一項：這兩個 plugins 實際包含哪些元件，特別是 hooks、MCP servers、agents、LSP 與 monitors。有了這份清單，加上 Aaron 對內容的確認，才能界定剩餘風險。

**第一筆真實請購目前被兩個獨立條件擋住**（`internal/buyer/gate.go` 依序檢查）：
1. **plugins 非空**：最近兩次真實 init 都回報 2 個（`docs/cli-poc.md` §41、§45）；方案 A 沒有清空它們。
2. **認證接受清單是空的**：`auth_source=none` 不在清單中，Gate 回 `auth_unconfirmed`。即使 plugins 問題解決，這一關仍會擋住。它要靠階段 B 的本人確認，以及 Aaron 明確核准的例外變更（roadmap §8 的 L1-d）。

「擴大 plugin ID 查詢」及後續診斷工具工作暫停。完整 ID 不再是主線必經的里程碑（§4 說明何時才需要）。

## 2 安全目的、控制與缺口

**標記**：
- 〔Host〕Host 強制執行；
- 〔CLI〕CLI 參數或宣告，依賴 CLI 遵守；
- 〔觀測〕某次 init 的回報；
- 〔無〕目前沒有證據。

| 風險 | 現行控制（執行位置） | 已能證明的 | 尚未覆蓋 |
|---|---|---|---|
| 模型使用業務以外的工具 | `--tools ""`、`--allowedTools` 三個 apa 工具、`--strict-mcp-config`〔CLI〕；Gate 要求 init 的工具恰為三個、MCP 恰為一個 `apa` 且 `connected`〔Host〕 | 不符就不開 Store、不 arm，業務工具一律拒絕（`RUN_NOT_READY`） | CLI 是否在 init 之後另增工具〔無〕 |
| 越權呼叫或冒用身分 | 每次執行一個 capability；`Binding` 由 Host 在 CLI 啟動前固定，模型不能提供；Gate 通過、Store 準備好後才 `Arm`；`MaxCalls` 8；每次呼叫 20 秒〔Host〕 | fake 與容器驗證（B1-04／B1-05 驗收） | — |
| 模型改寫金額、商品、授權或裁決 | 模型只能提供商品 ID，必須是受信任目錄中的商品；訂單由 Host 從受信任商店資料組成；Policy 在 Store 中確定性裁決；同一 task 與商品重試不產生新請購〔Host〕 | Policy、Store 驗收（B1-05） | — |
| 付款 | M1 沒有付款、Signer 或 testnet 路徑〔Host：程式中不存在〕 | — | — |
| plugins 加入提示內容（指令、skills、output style） | `--disable-slash-commands`〔CLI〕；Gate 拒絕非空 skills／agents〔Host，依 init〕 | 兩次觀測 skills／agents 為空〔觀測〕 | plugins 本身是否注入提示或 CLAUDE.md 類內容〔無〕。影響上限被上列 Host 控制框住 |
| plugins 的 hooks 執行命令（檔案、網路、子程序） | 串流中出現 `system`／hook 訊息就停止〔Host〕；process group 回收〔Host〕 | 兩次觀測沒有 hook 訊息〔觀測〕 | 依官方 CLI 參考（2026-10-08 查閱，`2.1.293` 適用性未確認），未加 `--include-hook-events` 時只有 SessionStart／Setup 會出現在串流，所以 PreToolUse 等 hook 可能無聲執行；它們以使用者身分在 Host 視野之外動作，也可能改寫工具輸入〔無〕 |
| plugins 帶入額外 MCP、agents、LSP、monitors | `--strict-mcp-config`〔CLI〕；Gate 檢查 MCP 數量與 agents〔Host〕 | 兩次觀測 MCP 只有一個 apa、agents 為空〔觀測〕 | LSP、monitors、背景活動〔無〕 |
| 認證與計費 | 環境白名單不傳 API key 或 OAuth token；Gate 只接受清單中的 `apiKeySource`，目前是空的〔Host〕 | 兩次觀測 `none`〔觀測〕 | `none` 不能證明登入方式或計費來源（Agent SDK 文件）；須本人確認〔無〕 |
| 執行時間、用量、證據 | `Total` 3 分鐘、`Assistant` 10、輸出上限、只保存白名單 trace、不保存提示與模型文字〔Host〕 | runner 驗收 | 用量只能記 `unknown`〔無〕 |

**「零 plugins」原本要防止的，是 Host 看不到也控制不了的程式碼與內容進入這次執行**：hooks 執行命令、注入指令、背景活動，以及「這次執行只用了宣告的元件」這項 trace 可信度。

- 業務層的風險（工具範圍、身分、金額、裁決、付款）已由 Host 的控制獨立覆蓋，不靠零 plugins。
- 零 plugins 實際補上的，是 **hooks 與背景活動** 這塊〔無〕的缺口，以及證據的單純性。
- 這塊缺口目前沒有其他控制可以取代。所以不建議只因為「工具數正確、沒看到 hook 訊息」就放寬。

## 3 兩條路線

### 路線一：維持零 plugins，使用 APA 專用設定目錄（推薦）

**做法**：子程序固定帶入 `CLAUDE_CONFIG_DIR=<APA 專用、持久的目錄>`，父環境不能覆寫；Aaron 為這個目錄自行登入一次。

**與方案 A 的差別**：方案 A 只把 plugin 快取根目錄換成空的暫存目錄。依官方環境變數文件，`CLAUDE_CONFIG_DIR` 是整個設定目錄：「All settings, session history, and plugins are stored under this path」。它同時隔開以下各項，這些都是方案 A 碰不到的：
- user 設定與已安裝 plugin 的紀錄；
- synced plugin 的狀態；
- session 資料；
- 憑證——依官方認證文件，macOS keychain 項目也以這個目錄區分。

**代價與所需核准**：
- **另一份登入**：要由 Aaron 本人以這個目錄登入。Claude Code 不代為登入，也不複製憑證。若登入同一個 claude.ai 帳號，預期計費來源相同，但仍以本人確認為準。
- 需要小的程式變更：固定環境變數加一項，並有 fake 驗證。這是設計與實作核准。

**限制，仍不能保證的事**：
- 受管設定是全機層級。若這兩個 plugins 由 managed settings 強制啟用，換設定目錄也不會清空。
- `~/.claude/skills` 型的 plugin 是否跟著設定目錄移動，文件沒有說明〔無〕。
- 背景活動與日常狀態寫入，同樣不能保證沒有。
- 文件不分版本，對 `2.1.293` 的適用性未確認。

**首次驗證出口**：實作通過 review 後，一次診斷型 C0′，條件沿用 §44.4。
- plugins／agents／skills 都明確為空 → 路線一成立，進入認證決策。
- plugins 仍非空 → 判定路線一未達目標，**同時**說明來源不在使用者設定層。不再追查，直接進入備案的決策。

### 路線二：接受有限且明確的 runtime 組成（條件式備案；目前尚不可採用）

**須先確認的事**：
- 兩個 plugins 的元件清單（hooks、MCP servers、agents、commands、skills、output styles、LSP、monitors）；
- Aaron 對內容與來源的確認。

**可接受的條件**：例如，確認沒有 hooks、MCP、LSP、monitors，只有被 `--disable-slash-commands` 關掉的命令或文件。

**Gate 調整**（若採用）：
- 只改 plugins 那一條，從「必須為空」改成「必須**恰好**等於核准清單」。比對條件是名稱、數量，以及當時核准的路徑分類。
- 多一個、少一個、名稱或分類不同，都拒絕；skills、agents、工具、MCP、權限、版本、認證等其他檢查一律不變。
- 清單寫死在程式中，經 review 與 Aaron 核准；不提供設定檔或環境變數放寬，避免變成「任意 plugins 都通過」。

**剩餘風險**：
- 名稱相同不代表內容不變，plugin 更新後內容可能改變。
- hooks 依然可能在 Host 視野之外執行，這部分風險由使用者承擔。
- 所以這條路需要 Aaron 明確接受這些風險。

**目前為什麼不可採用**：沒有元件清單，就無法界定剩餘風險。名稱相符、工具數正確、一次沒看到 hook 訊息，都不是足夠的依據。取得清單最直接的方式是 Aaron 本人查看自己安裝的 plugins；Claude Code 讀取這些內容須另行核准。

## 4 通往一次真實請購 trace 的最小步驟（每一步都須另行核准）

1. **Aaron 選路線**：選路線一，並接受另一份登入與上述限制。
2. **實作**（設計與實作核准）：
   - `fixedEnv` 加入 `CLAUDE_CONFIG_DIR=<Aaron 指定的 APA 專用目錄>`，live、probe 都套用，父環境不能覆寫；
   - fake 測試確認子程序收到唯一的值；
   - 加一項突變，並跑一次完整斷網容器。
   - 方案 A 與其他已驗收能力保持原狀，不重做。
3. **Aaron 本人登入與階段 B**：
   - Aaron 以該目錄自行登入，並自行確認登入方式，例如自己執行 `claude auth status`；
   - 再決定是否把特定認證值列入接受清單（L1-d 例外變更：程式只改清單，fake 覆蓋接受與拒絕）。
   - `none` 不直接視為已接受。
4. **一次診斷型 C0′**（執行核准）：**這次是必要的**，因為它回答「專用設定目錄能否清空 plugins」，並決定是否轉向備案。它不是每一步都重跑的例行檢查；認證決策之後不必再跑一次。
5. **一次真實請購嘗試**（執行核准；roadmap §8 的 L1-e，以 `docs/cli-poc.md` §7.2 的一條命令、一個 task 執行）：
   - **成功**：同一個 run／task 中有：
     - Gate 通過並 arm；
     - 真實的業務工具呼叫紀錄；
     - `request_purchase` 進入 Store，並有 Policy 裁決（Ask 或 Deny，依 demo profile 與現行 Policy；不為了展示修改裁決）；
     - Store 中有對應的 task、請購與事件；
     - trace 只保存白名單。
   - **分類**：Gate 拒絕、沒有請購（模型沒有呼叫）、達到上限（turn、時間、呼叫數）、逾時、CLI 錯誤，都如實記錄；用量記 `unknown`。
   - **停止**：任何失敗都停止，不重試。
6. **接上展示鏈**（另一個出口）：把同一筆真實 trace 接到既有的 Dashboard／Unity 資料流。這和第 5 步的出口分開；第 5 步完成，不代表這一步也完成。

這些步驟完成，不代表 M1、商業展示版或整體安全性完成；付款、Signer、ZK、多 Agent 仍是後續工作。

## 5 決定事項

**Aaron 現在要決定的**（3 題）：
1. 是否採用路線一，並接受：Aaron 為 APA 專用設定目錄另外登入一次、受管設定與背景活動的限制，以及「若 plugins 仍非空就轉向路線二的決策」。
2. APA 專用設定目錄的位置。建議放在 repo 與 `~/.claude` 之外、只供 APA 使用的目錄。
3. 是否同意把階段 B（本人確認登入方式）排在 C0′ 之前，或與它並行；認證值的實際變更仍另行核准。

**現在不必決定的**：
- 路線二是否採用（等路線一的診斷結果，或 Aaron 提供元件清單）；
- plugin ID 查詢是否恢復；
- Linux 目錄清理差異；
- 受管設定是否調整；
- 付款、Signer、ZK、多 Agent 的排程。
