# agent-payment-authority

建立在 x402 之上的 Agent 付款授權與證據層：Agent 可以提出付款，但不能自己批准，也不能自己簽名。

> **目前狀態：B1-00～03 已驗收（B1-02 共用契約 `apa.m1.v2` fixture，10/1 於 `2dc8d93` 結案；B1-03 最小資料商店（目錄＋402），10/1 於 `ebd3f44` 結案）；B1-05 進行中：節點 1（確定性 Policy）於 `61b3652` 通過 review，節點 2（持久化與事件查詢）設計待 review（[docs/persistence-design.md](docs/persistence-design.md)），尚未實作；B1-04 已解鎖，尚未開始。** 可啟動 loopback 服務、回應 health、提供商品目錄與 HTTP 402 報價、跑契約與雜湊測試；規格見 [docs/contracts.md](docs/contracts.md)、[docs/merchant.md](docs/merchant.md) 與 [docs/policy.md](docs/policy.md)。
> 尚未實作：Claude Code wrapper、請購持久化與去重、事件、展示層、任何付款、簽章或付費內容交付。
> 第一個里程碑 M1 的終點是 **請購與 Ask／Deny**，沒有真付款。邊界見 [docs/m1-boundaries.md](docs/m1-boundaries.md)。

## 需求

| 工具 | 版本 | 用途 |
|---|---|---|
| Go | 1.26 以上（以 1.26.4 驗證） | 編譯與測試 |
| make | 系統內建即可 | `make check`、`make dev` |
| Node.js | 20 以上 | 僅 `make check-contracts` 的獨立展示端向量檢查；啟動 Go 服務不需要 |

只 checkout 這個 repo 就能安裝、啟動與測試。不需要其他 repo、submodule、symlink 或 local module replace，目前也沒有任何第三方 Go 套件。

現階段**不需要**也不會使用：Claude Code CLI、模型服務、RPC、facilitator、testnet 帳戶。後續 live 模式才需要官方 Claude Code CLI 與本人的官方登入；登入由使用者自己透過官方流程完成，這個 repo 不讀取、不複製也不保存登入憑證。

## 啟動

```sh
cp .env.example .env
make check    # go vet + go test + go build，不呼叫付費模型、不連 testnet
make dev      # 先 build，再於前景啟動
```

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

**停止**：在執行 `make dev` 的終端按 `Ctrl-C`（或對該行程送 `SIGTERM`）。服務會關閉 listener 後結束，port 隨即釋放，可以直接再啟動一次。

## 設定

設定來自環境變數；`make dev` 會載入 `.env`（若存在），`.env` 裡的值會蓋過同名的環境變數。

`.env` 每一行只能是空行、`#` 開頭的註解，或 `KEY=VALUE`。等號前後不能有空白；不支援引號、`export`、行內註解與任何 shell 語法。值只能用英數字與 `_ . : / @ % + , - [ ]`。只要有一行不符合，`make dev` 會列出行號（不印出內容）並結束，不會用讀到一半的設定啟動：

```text
dev: .env line(s) 2 are not plain KEY=VALUE (no spaces, quotes or shell syntax); nothing was started
```

| 變數 | 必填 | 說明 |
|---|---|---|
| `APA_ADDR` | 是 | 服務位址，必須是 loopback IP 與 port，例如 `127.0.0.1:8402` 或 `[::1]:8402`。不接受 `0.0.0.0`、LAN IP 或主機名稱。`8123` 保留給 WebAuthn 驗證頁，請勿使用 |
| `APA_LIVE_CLI` | 否 | 預設 `false`。目前設為 `true` 會拒絕啟動，因為 wrapper 尚未實作 |

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
cmd/demo/          本機服務進入點：health 與商店路由
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
```

## 在受限沙箱裡跑測試

`cmd/demo` 與 `internal/merchant` 的測試會在 loopback 上開臨時 port。若執行環境禁止本機綁定 port，會看到 `bind: operation not permitted`，這是環境限制，不是測試失敗。
