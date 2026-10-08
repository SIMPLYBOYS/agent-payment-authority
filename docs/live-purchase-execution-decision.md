# 第一筆真實請購的執行路線評估

> **狀態：第一次真實請購嘗試已執行（`docs/cli-poc.md` 第 52 節）：真實 init 通過完整 Gate（例外模式、認證 `none`），但 Store 因資料庫目錄權限（§51 腳本缺陷）拒絕開啟，沒有請購。已停止。Codex 核對通過，§51 已改用 `$EV/db/apa-live.db`（第 52.1 節）；再一次嘗試待 Aaron 另行核准。**
> - Aaron 已選定路線一（維持零 plugins）。專用設定目錄為 `~/.apa-claude`；階段 B 排在 C0′ 之前（第 6 節）。
> - 2026-10-08；分支 `b1-04-live`。評估基準 HEAD `bd3ac81`（產品程式基準 `40bd263`，查詢工具 `5bab248`）；本次補正的開工基準為 `34ca9eb`。
> - 本文件不改變現行 Gate、認證接受清單或任何執行限制，也不代表 plugins 已獲接受、零 plugins 要求已取消、來源已確認或正式請購已放行。
> - 依據：vault〈Claude-Code-B1-04-回歸真實請購主線與執行邊界評估派工-bd3ac81-20261008〉、〈Claude-Code-B1-04-路線一決策稿R1-R2文件補正派工-34ca9eb-20261008〉。只做原始碼與文件唯讀核對；沒有跑測試、Docker、Unity 或任何 CLI。

## 1 結論與目前真正的阻擋

**已選定路線一：維持零 plugins，保留方案 A，另加 APA 專用的 Claude 設定目錄（`CLAUDE_CONFIG_DIR`）。**
- 不改 Gate 的 plugins 規則。
- 代價是 Aaron 要為這個目錄另外登入一次。
- 效果要用一次診斷型 C0′ 才能確認（第 3 節的出口）。

**條件式備案：路線二（接受有限且明確的 runtime 組成）。** 目前**尚不可採用**，也不是診斷失敗後會自動生效的允許清單。
- 真正缺的決策資訊只有一項：這兩個 plugins 實際包含哪些元件，特別是 hooks、MCP servers、agents、LSP 與 monitors。
- 有了這份清單，加上 Aaron 對內容的確認，才能界定剩餘風險；是否評估由 Aaron 決定。

**第一筆真實請購目前被兩個獨立條件擋住**（`internal/buyer/gate.go` 依序檢查）：
1. **plugins 非空**：最近兩次真實 init 都回報 2 個（`docs/cli-poc.md` §41、§45）；方案 A 沒有清空它們。
2. **認證接受清單是空的**：`auth_source=none` 不在清單中，Gate 回 `auth_unconfirmed`。即使 plugins 問題解決，這一關仍會擋住。它要靠階段 B 的本人確認，以及 Aaron 另行核准的認證清單修改（roadmap §8 的 L1-d）。

「擴大 plugin ID 查詢」、來源調查及後續診斷工具工作暫停。完整 ID 不再是主線必經的里程碑；只有在 Aaron 決定評估路線二時，元件清單才是必要資訊。

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

### 路線一：維持零 plugins，保留方案 A，另加 APA 專用設定目錄（已選定）

**做法**：
- Host 在組裝子程序環境時，帶入**一個** `CLAUDE_CONFIG_DIR`，值為以子程序 HOME 組成的絕對路徑 `<HOME>/.apa-claude`，父環境的同名值不能取代它。
- Aaron 為這個目錄自行登入一次。
- 方案 A 的每次執行暫存 plugin 根目錄保持原狀。

**與方案 A 的差別**：方案 A 只把 plugin 快取根目錄換成空的暫存目錄。依官方環境變數文件，`CLAUDE_CONFIG_DIR` 是整個設定目錄：「All settings, session history, and plugins are stored under this path」。它還涵蓋以下各項，這些都是方案 A 碰不到的：
- user 設定與已安裝 plugin 的紀錄；
- synced plugin 的狀態；
- session 資料；
- 憑證——依官方認證文件，macOS keychain 項目也以這個目錄區分。

**代價與所需核准**：
- **另一份登入**：要由 Aaron 本人以這個目錄登入。Claude Code 不代為登入，也不複製憑證。若登入同一個 claude.ai 帳號，預期計費來源相同，但仍以本人確認為準。
- 需要小的程式變更與 fake 驗證（第 4 節），須另行核准。

**限制，仍不能保證的事**：
- 專用設定目錄不等於作業系統隔離。
- 受管設定是全機層級，不受設定目錄影響。
- `~/.claude/skills` 型的 plugin 是否跟著設定目錄移動，文件沒有說明〔無〕。
- 背景活動與日常狀態寫入，同樣不能保證沒有。
- 文件不分版本，對 `2.1.293` 的適用性未確認。

**首次驗證出口**：實作通過 review、Aaron 完成登入與階段 B 之後，另行核准一次診斷型 C0′，逐項核對 `docs/cli-poc.md` §44.4 的**全部**觀測條件（plugins、agents、skills、MCP、工具、權限與版本；缺欄位、`null` 或型別錯誤都不算明確為空）。
- **全部條件符合**：才能記為「本次 init 達到診斷目標」。這不保證後續沒有背景載入，不等於正式 Gate 或認證條件已通過，也不代表正式請購已獲核准。
- **plugins 仍非空**：只代表本次專用設定目錄方案未達目標，來源仍未確認。保存證據後停止，交由 Aaron 決定是否評估路線二；不得自動切換方案或放寬 Gate。
- **其他條件不符、未取得觀測或證據不完整**：依 §44.5 處理，停止、不重試。

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

**目前為什麼不可採用**：
- 沒有元件清單，就無法界定剩餘風險。名稱相符、工具數正確、一次沒看到 hook 訊息，都不是足夠的依據。
- 取得清單最直接的方式，是 Aaron 本人查看自己安裝的 plugins；Claude Code 讀取這些內容須另行核准。
- 來源調查繼續暫緩。

## 4 後續流程與實作邊界

### 4.1 待核准的流程（不是本輪執行清單）

1. 完成本輪文件補正與 Codex 限定複核。
2. Aaron 另行核准實作與 fake 驗證（4.2 的範圍；含一次完整斷網容器）。
3. 實作完成並通過 review。
4. Aaron 使用專用目錄登入，完成**階段 B 本人確認**，例如以同一個專用目錄自己執行 `claude auth status`。
5. 另行核准**一次 C0′**，逐項核對診斷結果（第 3 節的出口）。
6. 認證接受清單若需修改，另行取得核准與 review（L1-d：程式只改清單，fake 覆蓋接受與拒絕；`none` 不直接視為已接受）。**正式請購**也須另行核准，且執行前所有條件都須滿足。

本人確認、認證清單修改、C0′ 與正式請購，是**不同的核准事項**，不得把其中一項視為其他項目的授權。
- 認證清單修改不因排在第 6 點，就自動獲准。
- 認證清單修改也不要求重跑已完成的診斷。

**正式請購**（roadmap §8 的 L1-e，以 `docs/cli-poc.md` §7.2 的一條命令、一個 task 執行）：
- **成功**：同一個 run／task 中有：
  - Gate 通過並 arm；
  - 真實的業務工具呼叫紀錄；
  - `request_purchase` 進入 Store，並有 Policy 裁決（Ask 或 Deny，依 demo profile 與現行 Policy；不為了展示修改裁決）；
  - Store 中有對應的 task、請購與事件；
  - trace 只保存白名單。
- **分類**：Gate 拒絕、沒有請購（模型沒有呼叫）、達到上限（turn、時間、呼叫數）、逾時、CLI 錯誤，都如實記錄；用量記 `unknown`。
- **停止**：任何失敗都停止，不重試。

**接上展示鏈**是另一個出口：把同一筆真實 trace 接到既有的 Dashboard／Unity 資料流。真實 trace 完成，不代表這一步也完成。以上步驟完成，也不代表 M1、商業展示版或整體安全性完成；付款、Signer、ZK、多 Agent 仍是後續工作。

### 4.2 實作邊界（只寫入設計，未實作）

- **路徑組成**：`CLAUDE_CONFIG_DIR` 在每次組裝子程序環境時，從子程序的 HOME 動態組成絕對路徑。不把 `~` 或 `$HOME` 當作待展開的字串放進環境變數，也不直接寫進靜態的 `fixedEnv`。
- **HOME 異常**：子程序 HOME 缺失或不是絕對路徑時，明確拒絕；不默默退回日常設定。
- **唯一值**：Host 組裝的子程序環境只有一個指定的 `CLAUDE_CONFIG_DIR`，父環境的同名值不能取代它。這不是宣稱 Host 能阻止 CLI 內部所有設定行為。
- **同一個目錄**：登入、登入方式確認、C0′、之後的 live 都使用同一個專用設定目錄，避免確認到日常環境的登入。CLI 版本查詢的環境也要一致。
- **fake 驗證**：只使用測試 HOME，不接觸 Aaron 的專用登入目錄。
- **持久資料**：專用設定目錄不納入每次執行的暫存清理，不得刪除。方案 A 的執行暫存 plugin 根目錄仍依既有規則處理，兩者分開。
- **失敗時**：登入或認證失敗就停止，不自動改用日常 `~/.claude`，不複製憑證。
- **限制**：專用設定目錄不等於作業系統隔離，也不保證排除受管設定、背景活動或日常狀態寫入。
- 不擴大成新的隔離架構、診斷工具或清理工具開發。

## 5 決定事項

**已決定**（第 6 節）：
- 路線一；
- 專用設定目錄 `~/.apa-claude`；
- 階段 B 排在 C0′ 之前。

**仍須 Aaron 另行核准**（各自獨立）：
- 實作與 fake 驗證（4.1 第 2 步）；
- 一次 C0′（第 5 步）；
- 認證接受清單的修改；
- 正式請購（第 6 步）。

**現在不必決定的**：
- 路線二是否評估（等路線一的診斷結果，或 Aaron 提供元件清單）；
- plugin ID 查詢是否恢復；
- Linux 目錄清理差異；
- 受管設定是否調整；
- 付款、Signer、ZK、多 Agent 的排程。

## 6 決策紀錄

- **2026-10-08 Aaron：「我們採用路線一」。** 第 5 節（初版）第 1 題已決定：
  - 維持零 plugins，以 APA 專用的 `CLAUDE_CONFIG_DIR` 為下一步；
  - 接受另外登入一次，以及受管設定與背景活動的限制。
  - 初版此處寫「若 plugins 仍非空，就轉向路線二的決策」，已依 R1 改為：保存證據後停止，交 Aaron 決定是否評估路線二。
- **當時仍待決定（目錄與排序後續已決定；實作及執行核准仍待取得）**：
  - 專用設定目錄的位置；
  - 階段 B 的排序；
  - 實作核准；
  - 之後的登入、C0′、真實請購等各自的執行核准。
- **2026-10-08 Aaron 回答其餘兩題，並表示實作先等 Codex review**：
  - 專用設定目錄為 `~/.apa-claude`，實作時以子程序 HOME 組成絕對路徑；
  - 階段 B 排在 C0′ 之前；
  - 實作核准先等 Codex review 本決策稿。
- **2026-10-08 Codex review**：接受路線一方向；R1、R2 須補正（第 7 節）。整份尚未標為 review 通過。
- 選定路線不等於核准實作或真實執行；Gate、認證接受清單與現行限制不變。

## 7 修訂紀錄：R1、R2（2026-10-08；待 Codex 限定複核）

Codex 的前置 review 只做文件、原始碼與 Git 唯讀核對，沒有重跑測試或真實 CLI。

| 項目 | 原文（`0887377`／`34ca9eb`） | 現行 | 理由 | 位置 |
|---|---|---|---|---|
| R1 失敗分支 | 「plugins 仍非空 → 判定路線一未達目標，**同時**說明來源不在使用者設定層。不再追查，直接進入備案的決策」 | plugins 仍非空只代表本次方案未達目標，來源仍未確認；保存證據後停止，交 Aaron 決定是否評估路線二；不自動切換、不放寬 Gate | 非空不能證明來源，「來源不在使用者設定層」沒有證據支持 | 第 3 節路線一出口 |
| R1 成功分支 | 「plugins／agents／skills 都明確為空 → 路線一成立，進入認證決策」 | 須符合 §44.4 全部條件，才能記為「本次 init 達到診斷目標」；不保證之後沒有背景載入，不等於 Gate 或認證通過，也不是請購核准 | 只看三項會漏掉 MCP、工具、權限、版本；診斷成功與請購核准是不同事項 | 第 3 節路線一出口 |
| R1 備案性質 | 「若 plugins 仍非空就轉向路線二的決策」（第 5、6 節） | 路線二是待決策的備案，不會自動生效；決策紀錄註明原寫法 | 避免被讀成自動允許清單 | 第 1、3、6 節 |
| R2 方案 A | §1「…取代方案 A 只換 plugin 快取根目錄的做法」與 §4「方案 A…保持原狀」並存 | 一致改為「保留方案 A，另加專用設定目錄」 | 沒有核准撤回方案 A | 第 1、3 節 |
| R2 決策狀態 | 開頭寫「目錄位置、階段 B 排序…仍分別待定」；第 5 節仍列為現在要決定的三題 | 目錄與階段 B 排序列為已決定；歷史的「仍待決定」標為「當時狀態，後續已決定」 | 已有決定，現行摘要與決策紀錄須一致 | 開頭、第 5、6 節 |
| R2 流程順序 | §4 舊流程：選路線 → 實作 → 登入與階段 B → C0′ → 請購；認證決策夾在階段 B 中 | 六步流程：文件補正與複核 → 實作核准 → 實作與 review → 登入與階段 B → 一次 C0′ → 認證清單修改與正式請購各自核准；四類核准互不授權 | 統一順序，刪除「C0′ 成功後才開始階段 B」的舊含意 | 第 4.1 節 |
| R2 實作邊界 | 只寫「`fixedEnv` 加入 `CLAUDE_CONFIG_DIR=<目錄>`」 | 4.2 節：動態絕對路徑、HOME 異常拒絕、唯一值、同一目錄、fake 只用測試 HOME、持久不刪、失敗不退回、不是 OS 隔離 | 靜態 `fixedEnv` 無法表達以 HOME 組成的路徑，邊界須事先寫明 | 第 4.2 節 |

## 8 驗收紀錄：`9cf4e6a` 通過限定文件複核（2026-10-08）

> **`9cf4e6a` 通過 Codex 限定文件複核，R1／R2 關閉。** 路線一的設計可進入「等待 Aaron 核准實作」階段。

- **Codex 確認**：
  - plugins 非空不再被用來推論來源；成功須符合 §44.4 全部觀測條件；
  - 專用目錄、階段 B 排序、保留方案 A，與各項核准界線一致；
  - 動態絕對路徑、fake 使用測試 HOME、持久目錄不清理、失敗不回退，都已寫明；
  - 本次差異只有三份文件，沒有產品程式變更。
- **證據歸屬**：Codex 核對了文件差異、`docs/cli-poc.md` §44.4／44.5、Git 與 vault 提交；沒有執行測試、建置或 CLI。
- **非阻擋文字更正**（隨本次同步完成）：第 6 節「當時狀態，後續已決定」原本也涵蓋實作與執行核准，容易誤讀。已改為「目錄與排序後續已決定；實作及執行核准仍待取得」。
- **下一步**：等待 Aaron 核准「專用設定目錄實作與 fake 驗證」（4.1 第 2 步，範圍見 4.2）。本次 review 不包含登入、C0′ 或正式請購的執行核准。
- B1-04／B1-06 仍 InProgress。

## 9 提案：開發展示執行例外與正式請購的剩餘前提（2026-10-08；待 Codex review 與 Aaron 核准）

> **開發展示執行例外提案完成，待 Codex review 與 Aaron 核准；尚未修改 Gate 或執行真實請購。**

- **依據**：vault〈Claude-Code-B1-04-開發展示執行例外提案派工-59c3a2d-20261008〉。開工核對：分支 `b1-04-live`，HEAD `59c3a2d`；工作區只有 `?? demo`。
- **暫停的旁線**：清空 plugins、來源／ID／元件清單調查、C0′ 例行重跑、查詢工具與 Linux 清理，以及受管設定修改。既有工具、證據、專用設定目錄與方案 A 都保留，不撤回。
- **路線一的最新結果**：plugins 仍為 2 個（第 49 節），維持「Claude Code 執行，待 Codex 核對」；fake review 通過不等於真實隔離有效。

### 9.1 這份提案取代的前提（待核准；舊要求保留為歷史）

- 第 3 節路線二原本要求「**須先取得兩個 plugins 的元件清單與 Aaron 對內容的確認**」，才可能讓 Gate 接受固定組成。
- 本提案改為：以 **Aaron 明確接受下列剩餘風險**，取代「先取得元件清單」這個前提；只限明確 opt-in 的開發展示模式；預設嚴格模式不變。
- 在 Aaron 核准前，第 3 節的舊要求仍有效；本提案**不代表**兩個 plugins 已被允許執行。

**Aaron 須明確接受的剩餘風險**（原文）：

> 名稱與數量相符，不代表 plugin 內容、來源或行為已確認。APA 對業務工具的限制，不能保證阻止 plugins 的 hooks、檔案、網路或背景活動。專用設定目錄也不是作業系統隔離。

另外：使用合成業務資料、不付款，並不能消除 CLI 以本機使用者身分執行的風險。

### 9.2 推薦實作（一個方案）

**opt-in 方式**：在 `apa-buyer run --live` 加一個旗標 `--dev-demo-plugins`。
- 只能與 `--live` 併用；fake、probe、demo host 都不提供。
- 不用環境變數、不用設定檔，名稱也不可設定。沒有這個旗標，一律是嚴格模式。

**最小程式差異**（預估約 60 行加測試）：

| 位置 | 變更 |
|---|---|
| `internal/buyer/gate.go` | `Gate` 新增 `DevDemoPlugins bool`，以及固定清單 `devDemoPlugins = {"cc-plugin-agents-md", "cc-plugin-plugin-authoring"}`。<br>`check` 的 plugins 檢查：<br>- `DevDemoPlugins == false`：維持現行規則（非空即 `plugins`）；<br>- `true`：`plugins` 成員必須存在，且是恰為 2 筆的陣列。每筆必須是物件、`name` 為字串（以既有的 `objectFields` 嚴格讀取），兩個名稱各出現一次。<br>- 多出、缺少、重複、名稱不符 → `plugins`；成員缺少 → `plugins`；元素格式異常 → `init_malformed`。<br>skills、agents、工具、MCP、權限、版本、認證的檢查順序與規則都不動。 |
| `cmd/apa-buyer/main.go` | 解析 `--dev-demo-plugins`；沒有 `--live` 時拒絕。設定 `Gate.DevDemoPlugins`。<br>LIVE MODE 開頭行加上固定文字 `gate: DEV-DEMO plugin exception (2 named plugins accepted; not the strict gate)`。 |
| `internal/buyer/host.go` | trace 的 `run_start` 事件新增固定欄位 `"gate_mode": "strict"` 或 `"dev_demo_plugins"`。不輸出 plugin 內容、路徑、憑證或模型文字。 |
| `internal/buyer`（`Host`） | 新增 `GateMode() string`（或由 `Gate` 提供），供 trace 使用。不改 Host 身分綁定、工具授權、Policy、Store，也不碰付款或 Signer。 |

**不變**：
- 專用設定目錄（`LiveEnv`）與方案 A（每次執行的 plugin 根目錄）保留；
- probe 只觀測，不受旗標影響；
- 不新增 runner 或診斷工具；
- 執行上限照舊：`Total` 3 分鐘、`Assistant` 10、`MaxCalls` 8、每次呼叫 20 秒、輸出上限。

**定向 fake 測試**（實作時一次執行；本輪不跑）：
1. 預設模式（沒有旗標）：init 帶這兩個 plugins → 仍拒絕（`plugins`），不開 Store、不 arm。
2. 有旗標：恰好這兩個 plugins → Gate 通過，其餘照常（Ask 路徑，`ask-mainline` 情境）。
3. 有旗標，組成不符 → 拒絕：多一個、少一個、同名兩次、名稱不同、大小寫不同、`plugins` 成員缺少、元素不是物件、`name` 不是字串。
4. 有旗標，但其他條件不符（非空 skills／agents、工具數、MCP、權限模式、版本、認證值）→ 照樣拒絕，代碼與嚴格模式相同。
5. `--dev-demo-plugins` 沒有搭配 `--live`，或用在 probe → 拒絕。
6. trace 的 `run_start.gate_mode` 與開頭行能區分兩種模式；嚴格模式的 trace 不出現例外標示。
7. 突變：
   - 例外清單不檢查數量；
   - 例外清單不檢查名稱；
   - 預設模式也套用例外；
   - `gate_mode` 寫死；
   - 例外時跳過後續的 auth 檢查。
   全部要由斷言失敗抓到。

另跑一次完整斷網容器，確認既有回歸仍成立。

### 9.3 認證前提（一次整理）

**階段 B 已有的**（第 48.1 節，Aaron 本人）：
- 專用目錄 `loggedIn=true`、`authMethod=claude.ai`、`apiProvider=firstParty`、`configDirectory` 符合、`subscriptionType=max`；
- Aaron 確認是本人的訂閱帳號。

**還缺的**：沒有缺少的確認項目。只剩 9.5 第 3 題「是否接受真實請購會使用這個訂閱、用量無法確認」，由 Aaron 回答。

**認證接受清單的最小變更**：`liveAcceptAuth` 由空清單改為只接受 `none`（L1-d 例外變更）。
- **適用所有 live run，包含嚴格模式**：嚴格模式下，只要 init 的 plugins 為空且 `apiKeySource` 為 `none`，現在也會通過認證檢查。目前觀測到的 plugins 非空，所以嚴格模式仍在 plugins 那一條被拒，但這不是認證規則本身的限制。（10/8 依 Codex 小修更正；原文寫「實際效果只出現在例外模式」，不精確。）
- **依據**：
  - live 子程序的環境白名單不傳 `ANTHROPIC_API_KEY`、`ANTHROPIC_AUTH_TOKEN`、`CLAUDE_CODE_OAUTH_TOKEN` 等憑證（`CLIEnv`）；
  - 設定目錄固定為 `~/.apa-claude`，Aaron 已確認該目錄是 claude.ai 的本人訂閱登入。
- `none` 本身仍不能證明登入或計費方式；依據是上面這份本人確認，不是 `none` 這個值。
- 其他值一律拒絕；清單不會自動學習或放寬。
- 測試：`none` 被接受；`api_key`、`oauth_token`、未知值、成員缺少都被拒絕。

### 9.4 從實作到第一筆真實請購（最短步驟）

1. Codex review 本提案；Aaron 核准 9.5 的三題，以及實作。
2. 實作 9.2、9.3，跑定向 fake 測試與一次完整斷網容器；集中交一次 review。
3. 同步操作文件：
   - §39.3 的 `PRODUCT_BASE` 改為新的已 review commit；
   - §7.2 的版本仍寫 `2.1.288` 且使用 `./bin/apa-buyer`，改為 `2.1.293`，並以乾淨 clone 建置的二進位；
   - README 環境變數表中「認證接受清單為空」的說明；
   - `docs/m1-boundaries.md` 的 C0′ 列。
4. **不另跑 C0′**：請購執行本身就會經過同一個 init Gate，C0′ 不會回答任何直接阻擋請購的新問題。
5. Aaron 另行核准**一次正式請購嘗試**。
   - **工具**：沿用 §39.3 腳本的前置步驟 1～5（分支、工作區、祖先、非文件差異、證據目錄、乾淨 clone 建置、VCS 檢查）。
   - **第 6 步改為**：`APA_LIVE_CLI=true APA_DB_PATH="$EV/apa-live.db" "$WT/apa-buyer" run --live --dev-demo-plugins --cli /Users/mac/.local/bin/claude --task task-a --registry "$WT/src/testdata/contracts/registry.json" --profile "$WT/src/testdata/policy/m1-demo-profile.json" --trace-dir "$EV/runs"`。
     - registry 與 profile **明確指向乾淨 clone 中的檔案**，避免二進位來自驗收基準，資料卻讀自原工作區的相對路徑（10/8 依 Codex 小修加入）。
     - 全新測試資料庫放在證據目錄中；不操作既有的展示資料庫。
     - 任務資料是乾淨 clone 中既有的 `testdata/contracts/registry.json` 與 `testdata/policy/m1-demo-profile.json`。
   - 不付款、不接 Signer、不用正式資產。
   - **成功**：同一個 run／task 中有：
     - `init.gate` 為 `passed`，且 `run_start.gate_mode=dev_demo_plugins`；
     - 至少一次真實的業務工具呼叫；
     - `request_purchase` 進入 Store，並有 Policy 裁決（Ask 或 Deny，照既有規則；不為展示修改）；
     - Store 中有對應的 task、請購與事件；
     - trace 只有白名單。
   - **分類**（如實記錄）：Gate 拒絕（含代碼）、沒有請購、達到上限（turn、時間、呼叫數）、逾時、CLI 錯誤。用量記 `unknown`。
   - **停止**：任何失敗都停止，不自動重試。
6. 接上展示鏈（Dashboard／Unity）是另一個出口，另外派工；第 5 步完成不代表這一步完成。

### 9.5 Aaron 需要決定的事（集中三題）

1. **是否接受 9.1 的剩餘風險原文**，並以此取代「先取得元件清單」的前提，只限 `--dev-demo-plugins` 的開發展示模式；預設嚴格模式不變。
2. **是否核准認證接受清單只加入 `none`**（9.3 的依據與限制）。
3. **是否接受**：第一筆真實請購以 `~/.apa-claude` 的本人訂閱執行，用量無法確認（記 `unknown`）；使用合成資料、全新測試資料庫、不付款。

以上三題，加上「核准實作」，是第一筆真實請購之前已知的全部決策；之後只剩「核准一次正式請購嘗試」。

L1-b、L1-c 不標整體通過；B1-04／B1-06 仍 InProgress。

### 9.6 決策紀錄（2026-10-08）

- **Aaron 原文**：「我接受 §9.1 所列的未確認 plugin 行為風險，限於開發展示模式；核准認證接受清單只加入 none，並接受使用本人專用目錄訂閱、用量可能 unknown、合成資料且不付款的條件。核准依 §9.2／9.3 實作，納入 Codex 的兩項小修，完成 fake 測試、突變及一次完整斷網容器驗證後，集中交 review。不恢復 plugin 調查，不另跑 C0′。本次不核准真實請購執行；實作 review 通過後，再核准一次嘗試。」
- **9.5 的三題都已決定**：
  1. 接受 9.1 的剩餘風險，限於開發展示模式；第 3 節「須先取得元件清單」的前提，**只在 `--dev-demo-plugins` 模式**被取代；預設嚴格模式不變。
  2. 認證接受清單只加入 `none`。
  3. 接受以 `~/.apa-claude` 的本人訂閱執行、用量可能 `unknown`、合成資料、不付款。
- **核准實作**：依 9.2／9.3，含 fake 測試、突變、一次完整斷網容器，集中交 review。
- **兩項小修**（Codex 對 `1e7a286` 的限定設計 review；Aaron 之後轉貼）：
  1. 9.3「實際只影響例外模式」不精確：`none` 加入所有 live run 的清單，嚴格模式在 plugins 為空時也受影響。已照實改寫，並由 `TestDevDemoGate`、`TestDevDemoPluginsCommand` 測試。
  2. 正式請購命令的 registry／profile 明確指向乾淨 clone 的檔案。已改 9.4。
  Codex 該次只核對文件、Git 與 Gate／trace 原始碼，沒有執行測試或 CLI；階段 B 的依據來自文件記錄的本人確認，不是 Codex 獨立查驗登入環境。
- **不核准**：真實請購執行（實作 review 通過後另行核准一次）、恢復 plugin 調查、另跑 C0′。
