# P1-b：金鑰庫與獨立 Signer（實作紀錄）

> 2026-10-12，Claude Code。依 vault〈Claude-Code-P1-b金鑰庫與獨立Signer實作派工-e725c44-20261012〉（Aaron：「這三件事我都核准，請提供派工筆記」）實作。設計見 [commercial-p1b-design.md](commercial-p1b-design.md)，分支 `commercial-p1b`，從 `e725c44` 開出。
> **狀態：實作與定向測試進行中。**沒有建立系統帳號或 socket 目錄，沒有日常或展示用付款金鑰，沒有連鏈、faucet 或 facilitator，Signer 沒有接進任何展示流程。

## 1 定向驗收計畫（執行前）

**基準**：本計畫與反向項目提交後的 HEAD（m1-check 會記錄 `base` 與 `tracked_clean`）。

**命令**：

```sh
go run ./cmd/m1-check --plan scripts/verify/p1b-check-plan.json --list   # 只列步驟與反向項目，不執行
env HOME="$T/home" GOCACHE="$(go env GOCACHE)" GOMODCACHE="$(go env GOMODCACHE)" \
    GOFLAGS=-mod=readonly GOPROXY=off GOTOOLCHAIN=local \
    go run ./cmd/m1-check --plan scripts/verify/p1b-check-plan.json --out artifacts/p1b
```

- `$T` 是本輪新建的空暫存目錄；`HOME` 指向它，所以測試讀不到日常 HOME、keychain、既有 wallet 或登入資料。
- `GOPROXY=off`、`-mod=readonly`、`GOTOOLCHAIN=local`：測試階段不下載依賴、不改 go.mod，也不換工具鏈。依賴已在測試前另外取得（`go get`，與本計畫分開）。
- 測試只連自己建立的 Unix socket（`$TMPDIR` 下的短路徑）與本機 loopback，不連外部服務。Claude Code 的工具沙箱禁止綁定 Unix socket，所以這個 run 在沙箱外執行；網路隔離靠上面的設定與測試本身只用本機 socket，**不是** OS 層的斷網容器。
- 證據目錄：`artifacts/p1b/m1-check-<UTC 時間>/`（git 忽略）。stderr 顯示目前步驟、耗時、心跳與 log 路徑；stdout 是機器可讀摘要。

**步驟、每步上限與預估**：

| # | 步驟 | 範圍 | 上限 | 預估 |
|---|---|---|---|---|
| 1 | vet | `go vet ./...` | 5 m | 30 s |
| 2 | crypto-keystore | EIP-712 規格範例向量（domain、digest、地址、r‖s‖v）、Circle 公布的 type hash、決定性、low-S、金鑰檔加解密與竄改／錯密語／弱參數／舊版本拒絕、金鑰檔權限 | 5 m | 20 s |
| 3 | authorization | 綁定逐欄、假／改版本／過期委託、跨 consumer、每筆與累計上限、simulated／webauthn 拒絕、正式組裝回 `RESERVATION_UNAVAILABLE` | 5 m | 20 s |
| 4 | state-race（`-race`） | 重送／衝突／重播／同 nonce、並行超額、三個中斷點與重啟、R3-a、R3-b、錢包鎖有界等待、委託先到期、金鑰版本不符 | 10 m | 40 s |
| 5 | ipc-host-store-race（`-race`） | caller uid、64 KiB、固定路由、log 不含簽章與金鑰；Host client 的 requested／unknown 與恢復、回覆遺失；schema 4、1／2／3 升級、舊 simulated 保留 | 10 m | 1 m |
| 6 | adjacent-store | `internal/store`（全部，含 M1）、`paycontract`、`paypolicy`、`m1evidence` | 10 m | 1 m |
| 7 | adjacent-store-race（`-race`） | Store 的 Pay／Upgrade／Expir／Recovery／Cancel | 10 m | 1.5 m |
| 8 | adjacent-demo | `cmd/demo` 的 `TestP1a*`、`TestHistory*`、`TestHistoricalTaskKeepsItsBinding`（`--upgrade-db` 與歷史檔讀取經過改動的路徑） | 10 m | 1 m |
| 9 | reverse | `^P1b:` 29 項，每項先跑未突變基準，再只在一次性 clone 拿掉一個防護 | 40 m | 12 m |

合計預估約 18–20 分鐘。

**反向檢查**：只有預期斷言命中才算抓到；編譯錯誤、panic、逾時、未跑到測試都不算。項目見 `python3 scripts/verify/m-b-reverse.py --list '^P1b:'`。

**不在本計畫**：完整斷網容器七步、全部突變、M1 重新產包、瀏覽器流程、Unity、node web 測試（本片沒有改 web）。

**失敗處理**：保存原始結果，定位、修正後只重跑受影響的步驟或項目；測試程式的缺陷與產品缺陷分開記，不把多輪結果拼成同一輪全過。
