# agent-payment-authority

建立在 x402 之上的 Agent 付款授權與證據層：Agent 可以提出付款，但不能自己批准，也不能自己簽名。

> **目前狀態：B1-00～03 已驗收（B1-02 共用契約 `apa.m1.v2` fixture，10/1 於 `2dc8d93` 結案；B1-03 最小資料商店（目錄＋402），10/1 於 `ebd3f44` 結案）；B1-05 確定性 Policy＋Store（`internal/policy`、`internal/store`）10/2 結案，程式驗收基準 `cb33768`，main 已 fast-forward 至 `7ad304a`（[docs/persistence-design.md](docs/persistence-design.md)）；主持人事件 HTTP 端點移至 B1-06；B1-04 Buyer wrapper 10/2 開始（分支 `b1-04-buyer`）：**fake 首個交付點 10/3 於 `b3c66f6` 通過技術 review**；**live 階段 A（開關、init 閘門、fake 驗證）10/3 於 `e8f6719` 通過技術 review**；C0 init 探測（`apa-buyer probe-init`，不送任何使用者訊息、不開 Store）10/4 於 `5f34a7b` 通過技術 review；**10/4 經 Aaron 核准對真實 CLI 執行一次 C0，30 秒內逾時，未取得 init**（未送任何使用者訊息；CLI 的背景行為與用量未知）。C0′（`probe-init --send-one-message`，只送一則固定訊息）10/4 已實作，fake 驗證於 `05e03e2` 通過技術 review；10/4 經核准執行一次，在版本檢查即停止、未送出訊息，核准版本隨後改為 `2.1.289 (Claude Code)`，`1f0b260` 通過限定範圍 review；10/4 再經核准執行一次，取得真實 init（Codex 已核對證據）；正式 live 仍不能放行（plugins 2、agents 5 會被 Gate 拒絕，`apiKeySource` 為 `none` 也不在接受清單），plugins／agents 的官方文件查證與只顯示身分的觀測已完成並驗收（程式 `b46e332`；正式 Gate 不變）；10/7 L1-a 在分支 `b1-04-live` 以固定環境變數停用內建 agents（`79a4a5f`／`418f0de`，只有 fake 驗證；10/8 通過 Codex 限定範圍技術 review，L1-a 關閉，不證明真實 CLI 的 agents 已清空；[docs/cli-poc.md](docs/cli-poc.md) 第 35、36 節）；未執行真實 CLI，真實 C0′、受管設定讀取與清單調整仍須另行核准。階段 B／C 未執行，live 認證接受清單為空。B1-04 整體 InProgress；真實 Claude 的登入、模型與計費方式都還沒驗證。**B1-06 交付點 1（Store 接線、已認證唯讀事件 HTTP、fake 端到端整合）已實作，修正後於 `f2d63fb` 通過 Codex 限定範圍 review（R1、R2 關閉）；交付點 2（Unity＋LimeZu 商店與 Dashboard，接上同一條 fake 資料鏈）經兩輪修正，於 `6aee7f0` 通過 Codex 限定範圍技術 review（R1～R3 與第二輪兩項關閉；只代表 fake 展示資料鏈，素材與 Web 建置公開發布未核准）；Aaron 10/5 實際操作後初步接受 fake 展示資料鏈；展示操作流程設計（[docs/demo-operation-design.md](docs/demo-operation-design.md)）中**交付點 3.1 於 `b2b7daa` 通過 Codex 限定範圍技術 review，R1～R3 關閉**（一鍵啟動、主持人唯讀頁 `/host`、token 檔載入、憑證清除與世代控制、就緒面板；不代表 B1-06、M1 或真實模型／付款流程整體完成）；3.2（主持人控制與場次持久化，同一 SQLite 內的協調資料表）依 `a845dcb` 的已審查設計實作於 `718aa91`，**於 `69ae9b2` 通過 Codex 限定範圍技術 review**（最近一次完整容器驗證屬 `c0d4c96`）；3.3（動態 task 清單：`GET /api/tasks`，頁面清單改由 Store 提供）實作於 `843ca4a`，經兩輪 review 後修正（`0818db8` 請求序號、`2c55835` 更新失敗可見），**於 `2c55835` 通過 Codex 限定範圍技術 review，R1、R2 關閉**（最近一次完整容器驗證屬 `843ca4a`，`2c55835` 未跑完整容器）；3.4（取消 task、錯誤呈現與中斷收尾：`POST /api/host/tasks/{task_id}/cancel`，`/host` 分開顯示場次歷史與 task 目前狀態）實作於 `d61771c`，review 後修正 R1～R3，**於 `3a6af59` 通過 Codex 限定範圍技術 review，R1～R3 關閉**（瀏覽器測試補充 `3b24cd4`；最近一次完整容器驗證屬 `d61771c`，修正後未重跑）；3.5（fake 展示流程整體操作驗收）已通過 Codex 技術 review（自動化部分 `667e933` 完整容器 247／247；最近的產品修正 `d18efa2`），Aaron 已實際操作並初步確認基本操作可用，設計文件第 16.3 節的完整人工清單未逐項確認；下一步的商店展示深化設計見 [docs/demo-presentation-design.md](docs/demo-presentation-design.md)（D1 設計於 `15d705a` 通過 Codex 限定 review，R1、R2 關閉；Aaron 核准後 D1 實作於 `7ffc45d`：場景優先版面、請購卡片、證據面板與回放當步；**於 `6338fd2` 通過 Codex 限定範圍技術 review，R1、R2、C1 關閉**，範圍為 fake 展示的網頁呈現，不代表真實 Agent、付款、區塊鏈、ZK 或整體 M1 完成；一般 Chrome 長流程的可見性問題仍未確認；D2 設計（同文件 §16，`9b70c0c`）經 Aaron 核准後實作：場景路徑與四向走路、轉換表與兩個選填 bridge 欄位（`cause_seq`、`task_end`）、追上與只回報最新序號、網頁過場說明，程式與測試 `bc79677`、計時修正 `d7070af`，依 review 修正 `78cb2cc`／`6137708`（§17.6），於產品程式 `78cb2cc`、瀏覽器測試補充 `6137708` 通過 Codex 限定範圍技術 review，R1、R2、C1 關閉（§17.7；範圍為 fake 場景動畫與網頁呈現，不代表真實 Agent、付款、區塊鏈、ZK 或整體 M1 完成）；`R2 order` 測試穩定性另行派工；D3–D6 未開始）；尚無真實模型請購；B1-06 仍 InProgress。** M1 尚未完成。** 可啟動 loopback 服務、回應 health、提供商品目錄與 HTTP 402 報價、跑契約與雜湊測試；規格見 [docs/contracts.md](docs/contracts.md)、[docs/merchant.md](docs/merchant.md) 與 [docs/policy.md](docs/policy.md)。
> 尚未實作：真實 Claude Code（live）請購、真實使用者認證（Dashboard 只用本機 demo viewer capability）、任何付款、簽章或付費內容交付。`cmd/demo` 已開啟 Store、到期 worker 與已認證唯讀 API（[docs/demo-integration.md](docs/demo-integration.md)），並提供 Dashboard 與 Unity 商店（[docs/demo-ui.md](docs/demo-ui.md)）。
> 第一個里程碑 M1 的終點是 **請購與 Ask／Deny**，沒有真付款。邊界見 [docs/m1-boundaries.md](docs/m1-boundaries.md)。
> **商業展示版交付路線**：[docs/commercial-demo-roadmap.md](docs/commercial-demo-roadmap.md)（現況與缺口、完整消費主線、真實 AI／授權與 ZK／鏈上付款／多 Agent 營運的切片與順序）。**路線提案；已依 Codex 對 `638c9f7` 的 review 修訂 R1–R4（該文件 §11），待限定複核與 Aaron 核准；未核准實作。**

## 需求

| 工具 | 版本 | 用途 |
|---|---|---|
| Go | 1.26 以上（以 1.26.4 驗證） | 編譯與測試 |
| make | 系統內建即可 | `make check`、`make dev` |
| Node.js | 20 以上 | `make check-contracts` 的展示端向量檢查、web adapter 測試與瀏覽器檢查；啟動 Go 服務不需要 |
| Google Chrome | 安裝在 `/Applications/Google Chrome.app`（固定路徑） | `make m1-fixture` 的瀏覽器步驟與 `scripts/verify/*-ui.mjs`（以 CDP 操作，不需要 Playwright） |
| Unity Web 建置 | 由本 repo 的 Unity 專案建置（見「B1-06 交付點 2」） | 商店畫面；`make m1-fixture` 的瀏覽器步驟需要它。建置不在 Git 裡（`unity/AgentPaymentAuthority/Build/` 被忽略），要先匯入本機已取得授權的 LimeZu 素材，再以 `scripts/unity/unity.sh webgl` 建置；或把既有的建置副本放到該路徑並保存 sha256 |

只 checkout 這個 repo 就能安裝、啟動與測試。不需要其他 repo、submodule、symlink 或 local module replace。唯一的第三方 Go 套件是 SQLite driver `modernc.org/sqlite`（不需 CGO），第一次建置時由 `proxy.golang.org` 下載並以 `go.sum` 核對；授權見 [docs/third-party.md](docs/third-party.md)。

預設的 fake 模式與所有測試**不需要**也不會使用：Claude Code CLI、模型服務、RPC、facilitator、testnet 帳戶。只有明確 opt-in 的 live 命令（`make m1-live-cli`、`make present-live`，每次都需另行核准）才需要官方 Claude Code CLI（釘選 `2.1.293`）與本人的官方登入（APA 專用設定目錄 `~/.apa-claude`）；登入由使用者自己透過官方流程完成，這個 repo 不讀取、不複製也不保存登入憑證。

**雙 Agent 商店（M-B，10/11 實作；10/12 fake 技術 review 通過，R1／R2／C1 關閉；M-C 真實雙 Agent 展示待 Aaron 核准）**：主持人頁每位客人一列，各有開始與停止，另有「一起開始」（各自送出的兩個請求）；兩位客人同時在同一間 Unity 商店，各自依自己最新場次的事件呈現；選取或查看較早的 task 只換面板。同時執行上限是部署設定 `--max-runs`（預設 2；`make demo`、`make present-fake` 不帶時即為 2）。每場在 trace 旁多寫一份時間紀錄 `<task>-<nanos>.timing.json`，`go run ./cmd/timing-judge --db … --a … --b … [--stop a|b] [--fake]` 唯讀判定兩場的工具呼叫是否交錯，並核對資料庫的場次結束與取消事件（結束碼 0 交錯、1 未達成、2 不判定、3 報告沒有完整寫出）。只用 fake 驗證，不代表兩位真實 Agent 已同時工作（M-C 另行核准）。驗證計畫：`go run ./cmd/m1-check --plan scripts/verify/m-b-check-plan.json --out artifacts/m-b`（加 `--list` 只列計畫）。設計與實作紀錄見 [docs/multi-agent-demo-design.md](docs/multi-agent-demo-design.md)。

**商業流程 P1-a：委託與單筆模擬核准（10/12 限定結案：既有階段性 fake 操作確認已記錄，c547ca4 的 R1～R3 限定技術複核通過；付款仍未啟用；P1-b 設計已通過限定複核（`e725c44`），Aaron 核准實作、兩個新依賴與常設委託的信任邊界；Codex 對 `22e45ed` 的技術 review 暫不通過（R1～R3），R1～R3 補正於 `fcc5095`，R2 回覆分類補正於 `07b16fd`；P1-b 實作缺口 R1～R3 已通過限定技術複核（程式 `07b16fd`、測試與計畫 `935de2f`；Codex 唯讀核對，沒有獨立重跑）。現行：P1-b 金鑰庫與獨立 Signer 通過限定驗收並結案（Done，2026-10-13）：範圍包含既有實作複核、本機 A／B0／B1 隔離操作與測試資料清理；正式付款仍未啟用；既有安全限制與非阻擋追蹤項保留。P1-b OS 隔離部署最小方案：Aaron 已核准 D1～D7；keygen 工具（D4）已實作並完成定向驗證，設計措辭（D6）已更新；keygen 通過 B1 一次性未注資測試金鑰用途的限定技術複核；A／B0／B1 由 Aaron 依 Route B 操作並完成安全停止與 D5 清理，Codex 依回報判讀（[docs/commercial-p1b-os-isolation.md](docs/commercial-p1b-os-isolation.md)）；系統設定已依 D1 核准並由 Aaron 執行（OS 隔離方案第 8 節）；日常或 A／B 展示用付款金鑰、以正式材料簽署、連鏈與交易仍未核准（[docs/commercial-p1b.md](docs/commercial-p1b.md)、[docs/commercial-p1b-design.md](docs/commercial-p1b-design.md)；分支 `commercial-p1b`）；分支 `commercial-p1a`；不付款）**：正式契約 `apa.pay.v1`（業務意圖與 EIP-3009 付款綁定一起算 `intent_hash`）、Store schema 3（委託、核准會話、grant）與 fake 限定的模擬核准者。主持人頁可「以 A 模擬」→「建立委託」→ Agent 請購 →「需要你處理」看單筆摘要 → 核准或拒絕（模擬）；成功只顯示「已核准（模擬），未啟用付款」，留下一筆 `simulated` grant，Signer 樁拒絕它。沒有金鑰、簽署或交易，沒有執行真實 CLI。`make present-p1a`；說明、操作步驟與驗證見 [docs/commercial-p1a.md](docs/commercial-p1a.md)。

**本人核准最小流程（A1 實機可行性、A2 正式接線；10/13 設計）**：本人核准設計另開條件與 close 到期判斷補正完成，待 Codex 限定複核；R1 原存取權限與 R2 維持關閉；整份設計尚未通過；A1／A2 未執行，付款未啟用（Codex 對初版 `6c49619`：方向可沿用、設計暫不通過，先補逐路由權限、不持付款金鑰的核准組裝與實機入口、回覆遺失的恢復）。推薦：WebAuthn 由 Signer 驗證（Host 只轉送、不簽發），兩步「Signer 發一次性 challenge → 驗 assertion 並保存已驗證核准」，付款（P2）才引用並消耗；登記是管理者在 Signer 終端機發起與確認的儀式；`rp_id=localhost`、唯一 origin `http://localhost:8402`（現行 `127.0.0.1` 不能做 WebAuthn）；Go 函式庫 go-webauthn v0.18.2 只放在 Signer；核准專用的 `apa-signer serve-approvals` 不讀付款私鑰；看資料與發起核准要該 consumer 的 viewer 憑證，結果只取決於 Signer 驗過的 assertion。待決 D1～D8。見 [docs/commercial-human-approval-design.md](docs/commercial-human-approval-design.md)。

**商業展示 UX1：主畫面與操作重整（10/12 完成：Aaron 以 fake 彩排手動驗證並判定合格，結合 Codex 技術複核；不代表真實 Agent、付款或整體商業展示版；分支 `commercial-ux1`）**：主持人頁改為商店場景為主視覺，旁邊每位 Agent 一張操作卡（執行狀態、Host 已回覆的工具呼叫次數，不知道時寫未確認、開始、只對執行中可按的「停止 Agent」、先確認的「取消請購／task」），點卡或點畫布上的客人選到同一位，查證放在「查看證據」抽屜，憑證與場次清單收在「進階」。只用 fake 驗證：`make present-fake`；手動彩排用 `make present-rehearsal`。說明與手動驗收步驟見 [docs/commercial-ux1.md](docs/commercial-ux1.md)。

**M1 的結果**：M1 已於 2026-10-11 結案，見 [docs/m1-result.md](docs/m1-result.md)（fixture 與 live 證據包、逐卡核對、重跑命令、限制與結案紀錄）。**乾淨目錄**：2026-10-11 在只有本 repo clone 的新目錄，以 `cp .env.example .env` 與 `make m1-fixture` 完成 fixture E2E（31 案 PASS，檢查器 PASS）；使用本機的 Go／Node 快取與既有的 Unity Web 建置副本（`docs/m1-result.md` 第 3.4 節）。

## 啟動

```sh
cp .env.example .env
make check    # go vet + go test + go build，不呼叫付費模型、不連 testnet
make dev      # 先 build，再於前景啟動
```

`make dev` 會開啟 Store（`APA_DB_PATH`，預設 `var/apa-demo-fake.db`；恢復驗證失敗就不啟動、不修補），並把本次啟動的 demo token 寫到 `var/demo-tokens.json`（0600）。不帶 fake 計畫時只提供既有紀錄的查詢，不啟動任何 run。

啟動後：

```sh
curl http://127.0.0.1:8402/healthz
```

```json
{"status":"ok","mode":"fixture","live_cli":false,"claude_login":"not_checked","payment_capability":"unavailable"}
```

商店（fixture 資料，沒有付款能力）：

```sh
curl http://127.0.0.1:8402/catalog                                  # 200，商品目錄，不含付費內容
curl -i 'http://127.0.0.1:8402/resources/kr-market?edition=1'      # 402，報價在 PAYMENT-REQUIRED header（base64 JSON）
```

帶任何 `PAYMENT-SIGNATURE` 都拿不到內容：格式不對回 400，格式正確也只回 402，因為 M1 無法驗證或結算付款。細節見 [docs/merchant.md](docs/merchant.md)。

## Buyer wrapper：fake 模式

```sh
APA_DB_PATH=var/apa-fake.db make demo-fake
```

**只有 fake 模式**：`bin/fake-claude` 不跑模型、不連網，不需要 Claude Code CLI 或登入；Go 依賴需已下載（首次建置會連 `proxy.golang.org`，之後可離線）。它是真的子程序，透過 MCP bridge 呼叫三個受限工具（目錄、報價、請購）；host 對本機商店發真 HTTP 取得 402，把請購寫進真的 SQLite Store。主商品得到 Ask，沒有付款、沒有內容交付；CLI 說「已付款」也不會改變任何紀錄。Store 只在 CLI 的 init 通過 host 檢查之後才開，之前的業務工具呼叫一律拒絕；fake 拿到的是空的暫存 HOME。同一個 DB 裡 task 只能啟動一次，重跑請用 `TASK=task-a-next` 或新的 `APA_DB_PATH`，不會刪除舊紀錄。live 模式尚未實作與驗證；橋接方式、信任邊界、上限與 CLI 相容性見 [docs/cli-poc.md](docs/cli-poc.md)。

## B1-06 交付點 1：fake 端到端整合

```sh
make demo-integration     # 建置 bin/demo、bin/fake-claude；APA_ADDR 預設 127.0.0.1:8402
```

同一個 demo 程序負責商店（真 HTTP 402）、Store 與到期 worker、已認證的唯讀 API，並依可信計畫 `testdata/demo/fake-plan.json` 跑 fake buyer：`task-a`（consumer-a）得到 Ask；`task-b`（consumer-b）由 Host 設定較低預算，得到 Deny（`INSUFFICIENT_BUDGET`）。fake 不跑模型、拿空的暫存 HOME，不找 PATH 上的 `claude`。

查詢（token 從 0600 的 token 檔讀，每次啟動重新產生；viewer 只能看自己 consumer 的 task，host 可看全部）：

```sh
TOKEN=$(python3 -c 'import json;print([t["token"] for t in json.load(open("var/demo-tokens.json"))["tokens"] if t["role"]=="host"][0])')
curl -sS -H "Authorization: Bearer $TOKEN" http://127.0.0.1:8402/api/tasks/task-a
curl -sS -H "Authorization: Bearer $TOKEN" http://127.0.0.1:8402/api/tasks/task-b/events
```

停止：`Ctrl-C`。關閉順序是先取消 fake run、停 HTTP、等 run 記錄結束，再停到期 worker、關 bridge，最後關 Store。重啟同一個資料庫會保留紀錄；已存在的 task 不會再跑，要新的 run 請改計畫用 `task-a-next` 或設新的 `APA_DB_PATH`。這是 demo 認證（不是使用者登入），沒有 UI，也沒有真實模型或付款。路由、回應欄位、權限與限制見 [docs/demo-integration.md](docs/demo-integration.md)。

**停止**：在執行 `make dev` 的終端按 `Ctrl-C`（或對該行程送 `SIGTERM`）。服務會關閉 listener 後結束，port 隨即釋放，可以直接再啟動一次。

## B1-06 交付點 2：Unity 商店與 Dashboard

```sh
LIMEZU_DIR=/path/to/limezu sh scripts/unity/import-limezu.sh    # 只複製清單內的 LimeZu 檔案（不進 git）
sh scripts/unity/unity.sh webgl artifacts/b1-06/unity-local     # Unity 6000.5.3f1，WebGL 建置（不進 git）
make demo-integration                                           # 然後開 http://127.0.0.1:8402/
```

頁面用自己 consumer 的 viewer token 登入（從 `var/demo-tokens.json` 取，只放頁面記憶體），同一份投影同時驅動 Unity 商店與 Inspector／Timeline。只有 fake 資料鏈：模擬 Agent、fixture、未付款，不演成功取貨。素材、授權、測試與限制見 [docs/demo-ui.md](docs/demo-ui.md)。

**一鍵啟動（交付點 3.1，已通過技術 review）**：首次準備（上面前兩行）完成後，展示時只要：

```sh
make demo        # 檢查建置與素材 → 編譯 → 啟動；不建 Unity、不跑測試、不印 token
```

開 `http://127.0.0.1:8402/host`（主持人本機頁），選 `var/demo-tokens.json` 載入（只留在頁面記憶體），就緒面板列出 HTTP／Store／fake CLI／Unity 狀態。3.2 起（待 review）：啟動時不跑場次，在「主持人控制」選情境按「開始」，每場配發新的 `show-*` task，頁面自動切到該 consumer 開啟它；可停止進行中的場次。`make demo` 使用自己的新資料庫 `var/apa-demo-host-runs.db`，既有的 `var/apa-demo-fake.db` 不升級。`/` 仍是只能手動貼 token 的一般 viewer 頁。見 [docs/demo-operation-design.md](docs/demo-operation-design.md) 第 12、13.16 節。

## M1 證據包與唯讀檢查

```sh
make m1-fixture                                    # M1-E1：fake CLI 產生 artifacts/m1/<run-id>/（約 8 分鐘，含瀏覽器）
make verify-m1 RUN_DIR=artifacts/m1/<run-id>       # M1-E2：唯讀檢查一個證據包，JSON 報告寫到 stdout
```

- `verify-m1` 只讀指定的證據包，以及 repo 中凍結的 `testdata/contracts/cases.json` 與可信 fixture。它不執行 `m1-fixture`、模型或包內的命令，也不開 Store、不連網、不寫檔。
- 檢查器本身（`go run ./cmd/verify-demo --run-dir DIR`）的結束碼：
  - 0：PASS；
  - 1：FAIL；
  - 2：INCONCLUSIVE（缺資料或版本不支援）；
  - 3：無法檢查，或報告寫不出。
- 經 `make` 時，非 0 的結束碼會變成 make 自己的 `Error N`（結束碼 2），要看報告裡的 `verdict`。
- 格式、V1～V7 的覆蓋、共用的依賴與限制見 [docs/m1-evidence.md](docs/m1-evidence.md) 第 15 節。

**live 單筆 Ask 包（M1-E3）**：

```sh
APA_LIVE_CLI=true make m1-live-cli CLI=/abs/path/claude BASE=<review 過的產品 commit> GATE=dev_demo_plugins
make verify-m1 RUN_DIR=artifacts/m1/<m1-live-run-id> MODE=live
```

- **`m1-live-cli` 會跑一次真實模型**（用 APA 專用設定目錄的本人訂閱登入）。要另經 Aaron 核准才能執行；一般測試、建置與 `verify-m1` 都不會啟動模型。
- 前置任一不符就停，不啟動 CLI：
  - 要有 `--live` 與 `APA_LIVE_CLI=true`；
  - Gate 模式要明確給；
  - review 過的基準之後只能有文件差異；
  - 已追蹤的檔案沒有修改；
  - 本機乾淨 clone 的建置不能標為 modified。
- 執行規則：
  - 只執行一次，不重試，不自動改模式；
  - 上限沿用 apa-buyer：180 秒、10 則回覆、8 次工具呼叫；
  - 資料庫、trace 與建置都放在新的 `artifacts/m1-live-work/<run-id>/`，證據包是新的 `artifacts/m1/<run-id>/`。
- 剩餘風險：
  - 開發展示 plugin 例外下，嚴格零 plugin 模式沒有驗證；
  - plugin 名稱、Gate 與專用目錄，不證明 plugin 的內容、沒有背景活動，或作業系統層級的隔離；
  - 可能用到訂閱額度，用量可能是 `unknown`。
- live 包只宣稱 `ask`。PASS 不代表 fixture 全案例或 M1 完成。
- 格式、判定與真實執行前的確認清單見 [docs/m1-evidence.md](docs/m1-evidence.md) 第 16 節。
- 執行中按 Ctrl-C（SIGINT）或送 SIGTERM：只停這一次執行。apa-buyer 的行程群組先收 SIGTERM 做有上限的清理；之後看整個群組，30 秒內沒清空才對群組送 SIGKILL（apa-buyer 本身先結束不算清完）。資料庫、trace 與證據包保留，`live/run.json` 記為 `interrupted`，結束碼 1，不重試。再按一次不會跳過清理。

**M1-E3 修正輪的目標驗證（只用 fake，有進度）**：

```sh
make check-m1-e3 ARGS=--list   # 只列出計畫（基準 commit、步驟、命令、上限、反向項目的 -run 篩選與預期斷言、證據目錄），不執行
make check-m1-e3               # 執行；進度在 stderr，最後的 JSON 摘要在 stdout
tail -f artifacts/m1-check/m1-check-<時間>/05-r1-r2-producer.log   # 另一個終端看某一步的完整輸出
```

- 10/11 的計畫：`go vet`；`internal/progress`（R1，`-race`）；`cmd/m1-check`（R3，`-race`）；反向分類器的 fake 測試（R4）；產包器的訊號、中斷、正常完成與失敗判讀（`-race`，只跑相關測試）；檢查器的 golden 與 run 紀錄負例；R1～R4 反向驗證。不啟動真實 CLI。
- 每步印 `START i/N`、正在跑的測試（只取自 `go test -json` 事件）或反向項目；安靜時約每 10 秒一行心跳（已完成數、本步與總耗時、距上次輸出多久、log 檔名）。結果是 `PASS`、`FAIL`、`TIMEOUT`、`INTERRUPTED` 或 `ERROR`，附真實結束碼與 log 路徑。
- 反向驗證：每項只跑對應的測試，先確認未突變時通過；突變在一次性的 clone 裡做。只有對應測試以預期的斷言失敗才算 `CAUGHT`；編譯或環境失敗、panic（包含測試逾時）、逾時、訊號、沒跑到測試、其他斷言都不算。
- 一步沒過就停，後面標 `NOT_RUN`，不重試。結束碼依序判斷：2 被中斷；1 有一步沒過；3 證據或摘要不完整（log 存不完整、超過 256 MiB 上限、`progress.log` 最後寫入或關閉失敗、摘要寫不到 stdout）；0 全部通過且都已保存與送出。保存失敗不會改變步驟本身的結果。
- 證據：`plan.json`、`progress.log`、每步的 log、`summary.json`，都在新的 `artifacts/m1-check/m1-check-<時間>/`。

## 設定

設定來自環境變數；`make dev` 會載入 `.env`（若存在），`.env` 裡的值會蓋過同名的環境變數。

`.env` 每一行只能是空行、`#` 開頭的註解，或 `KEY=VALUE`。等號前後不能有空白；不支援引號、`export`、行內註解與任何 shell 語法。值只能用英數字與 `_ . : / @ % + , - [ ]`。只要有一行不符合，`make dev` 會列出行號（不印出內容）並結束，不會用讀到一半的設定啟動：

```text
dev: .env line(s) 2 are not plain KEY=VALUE (no spaces, quotes or shell syntax); nothing was started
```

| 變數 | 必填 | 說明 |
|---|---|---|
| `APA_ADDR` | 是 | 服務位址，必須是 loopback IP 與 port，例如 `127.0.0.1:8402` 或 `[::1]:8402`。不接受 `0.0.0.0`、LAN IP 或主機名稱。`8123` 保留給 WebAuthn 驗證頁，請勿使用 |
| `APA_LIVE_CLI` | 否 | 預設 `false`。`make dev`（demo）設為 `true` 會拒絕啟動。`apa-buyer run --live` 需要它為 `true`，另需絕對路徑的 `--cli` 與核准版本 `2.1.293 (Claude Code)`（10/8 起，版本由 Aaron 提供；10/4～10/8 為 `2.1.289`）；live 的認證接受清單自 10/8 起只接受 `none`（`8976e86`，Aaron 核准；依據見 [docs/live-purchase-execution-decision.md](docs/live-purchase-execution-decision.md) 9.3），預設嚴格閘門仍拒絕非空 plugins；只有明確加 `--dev-demo-plugins` 才接受指定的兩個 plugins（見 [docs/cli-poc.md](docs/cli-poc.md) 第 7 節） |
| `APA_DB_PATH` | 否 | Store 檔案路徑，不接受 DSN，目錄須為 0700。`cmd/demo`（`make dev`、`make demo-integration`）未設定時為 `var/apa-demo-fake.db`；`apa-buyer run` 未設定時 fake 為 `var/apa-fake.db`、live 為 `var/apa-live.db`。不要讓 `apa-buyer` 指向 demo 的資料庫（各自有到期 worker） |
| `APA_DEMO_TOKEN_FILE` | 否 | `cmd/demo` 寫入 demo token 的檔案，預設 `var/demo-tokens.json`；目錄須為 0700，檔案 0600，每次啟動重新產生 |

缺少或不合法的設定會在啟動時以一行訊息說明原因並結束，例如：

```text
demo: APA_ADDR is required (e.g. 127.0.0.1:8402); copy .env.example to .env or export it
```

## health 代表什麼

`status: ok` 只表示行程已啟動且能回應。它**不代表** Claude Code 已安裝或已登入（`claude_login: not_checked`），也**不代表**可以付款（`payment_capability: unavailable`）。

## port 被占用時

服務只回報錯誤並結束，不會尋找或終止占用該 port 的行程：

```text
demo: 127.0.0.1:8402 is already in use by another process; it was left untouched, stop it yourself or change APA_ADDR
```

## 驗收 B1-01

```sh
sh scripts/verify-b1-01.sh
```

腳本把未被忽略的檔案複製到暫存目錄，在那裡編譯、寫測試用的 `.env`、啟動與停止服務，port 由系統挑選。它只讀取這個 repo，不會建立、修改或刪除你的 `.env`、`bin/` 或其他檔案，也只對自己啟動的行程送信號。需要 `git`、`curl`、`pgrep`。

每項檢查印出 `PASS` 或 `FAIL`。全部通過才以 0 結束；任何一項失敗以 1 結束；無法建立暫存副本以 2 結束。預期被拒絕的案例若意外啟動了服務，會在期限後被終止並記為失敗。要保留輸出時自行導向 `artifacts/`（不進版控）。

## 隔離驗證（僅供驗證）

`scripts/verify/` 在 Linux 容器裡重跑全部檢查與 mutation tests。**Docker 只是驗證工具，不是 demo 的執行相依**：`make check`、`make dev` 都不需要 Docker。

```sh
sh scripts/verify/build-image.sh                        # 準備階段：以固定 digest 拉取基礎映像並建置（可連網）
sh scripts/verify/container-verify.sh                   # 測試階段：--network none、唯讀原始碼、資源硬限制
sh scripts/verify/container-verify.sh --inject-failure  # 自我測試：必須以非零結束
sh scripts/verify/container-verify.sh --inject-export truncate  # 匯出故障自我測試（pack-fail／truncate／missing／oversize），必須以非零結束
```

主機端需要 `python3`（`scripts/verify/evidence_import.py`）。容器輸出的 archive 視為不可信：主機端限制總量，在暫存區驗證（檔案白名單、拒絕連結與特殊檔案、路徑越界、大小上限、manifest hash、必要檔案）後才寫入 `<run>/container/`；被拒的 archive 不解開、原樣留存。打包、傳輸、驗證、保存任一步失敗，腳本都以非零結束。

隔離設定寫在 `container-verify.sh` 開頭，每次實際使用的 `docker run` 參數也會存進結果目錄。結果寫入新的 `artifacts/verify-container/runs/<commit>-<時間>/`，不覆寫舊證據。

## 不進版控的東西

`.env`、`bin/`、`var/`（執行資料）、`artifacts/`（驗收輸出）、憑證與金鑰檔、Claude Code CLI 原始輸出、音檔、LimeZu 授權素材。規則在 `.gitignore`。不含敏感資料的測試 fixture 之後放 `testdata/`，會進版控。

## 目錄

```text
cmd/demo/          demo host：health、商店、Store 與到期 worker、已認證唯讀 API、fake run
internal/config/   環境變數載入與驗證
docs/              M1 邊界說明
scripts/           可重跑的驗收腳本；scripts/verify/ 為容器隔離驗證（僅供驗證）
internal/contracts/ M1 fixture 契約與語意驗證
schemas/           JSON Schema
testdata/contracts/ 固定測試向量與已接受的事前案例（cases.json）
internal/merchant/ fixture 資料商店：目錄、受保護資源與 402
internal/x402/     x402 v2 HTTP header 編解碼（不驗證、不結算）
testdata/merchant/ 商店的預期 wire 輸出（手寫）
testdata/x402/     x402 規格原文的範例 header（互通向量）
internal/policy/   確定性 Policy：Ask／Deny／Allow（M1 無法付款）
testdata/policy/   M1 demo policy profile（fixture）
internal/store/    B1-05 持久化：SQLite、事件鏈、重送、到期、恢復驗證
cmd/apa-buyer/     B1-04 buyer host（fake 模式）與 MCP bridge 子命令
cmd/fake-claude/   fake CLI：不跑模型，依情境輸出 stream-json
internal/buyer/    CLI 程序生命週期、串流解析、結果分類、run trace
internal/toolbridge/ MCP adapter、capability relay、402 檢查與請購組裝
testdata/cli/      fake CLI 情境（主線與故障）
testdata/demo/     demo host 的可信 fake 計畫
web/               Dashboard（原生 HTML／CSS／JS modules，嵌入 cmd/demo）與其單元測試
unity/AgentPaymentAuthority/ Unity 6000.5.3f1 商店專案（自有腳本；LimeZu 素材、場景與建置不進 git）
scripts/unity/     LimeZu 匯入與 Unity 批次命令；scripts/verify/browser-ui.mjs 為實際瀏覽器測試
```

## 在受限沙箱裡跑測試

`cmd/demo` 與 `internal/merchant` 的測試會在 loopback 上開臨時 port。若執行環境禁止本機綁定 port，會看到 `bind: operation not permitted`，這是環境限制，不是測試失敗。
