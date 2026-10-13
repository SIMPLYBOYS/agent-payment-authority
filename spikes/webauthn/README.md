# A1 WebAuthn 實機可行性 spike

依 `docs/commercial-human-approval-design.md` 第 2、4 節與第 17～19 節。

**狀態**：A1 spike R1／R2 補正完成，待 Codex 限定複核；實機未驗收，A2 未開工，付款未啟用。

**範圍**：
- 只回答一件事：這台 macOS 12.7.6 上，哪個瀏覽器加哪種 authenticator，能在 `rp_id=localhost` 完成登記與一次核准；錯 challenge、重送、錯 origin、不要求 UV 時實際會怎樣。
- **只供測試**：
  - 不付款、不簽交易、不連鏈；
  - 不建立或使用付款金鑰；
  - 沒有 Signer、沒有真實 Agent，也不碰 Store。
- **實機登記會在 authenticator 裡建立一個 WebAuthn credential**。
  - spike 的伺服器狀態只在記憶體，重啟只清掉伺服器這一側。
  - 驗證器、瀏覽器或同步帳號裡的 credential **不會**因此被刪除。
- 請購是固定的一筆合成資料。
- 不是產品程式：A2 會在 Signer 裡重做，A2 一律重新登記，不沿用 spike 的 credential（D3）。
- 獨立 Go module，go-webauthn v0.18.2 不進主 `go.mod`；它帶進的 `x/crypto v0.57.0`、`x/sys v0.48.0` 與主 module 的釘選相同。

## 授權來源

| 項目 | 來源 | 範圍 |
|---|---|---|
| 決策接受 | Aaron 10/13 原話（設計文件第 17 節逐字保存） | D1～D7 採推薦方案與明列限制；D8 延後到 P2，A2 用獨立 `approvals.db` |
| A1 實作派工 | 同一段原話：「核准另行派工 A1 最小 spike 的實作、定向驗證與由我操作的實機登記／驗證」；之後 Aaron 在對話中寫「核准 A1 派工」，沒有另寫派工筆記，Claude Code 依此開工（第 18 節）；本輪依 vault〈A1 spike 反向驗證與結果保存限定補正派工-20677b0〉補正（第 19 節） | 實作與定向驗證；只供測試、不接付款、不用真實 Agent |
| 實機執行 | 同一段原話：實機登記與驗證「由我操作」 | 只由 Aaron 操作。依補正派工，要等 Codex 核對完成、Aaron 確認後才進行；裝置不支援時保存結果並停止 |

以上兩句原話都可以在 Claude Code 的對話中核對：10/13 的核准原文，以及「核准 A1 派工」。
- Codex 的 D1～D8 建議表，只是 Aaron 原話中「上述」所指的內容，本身不是核准。
- A2 實作、付款金鑰、簽署與交易都沒有核准。

## 伺服器規則（D6）

- **RP ID 與 origin**：
  - RP ID 是 `localhost`；origin 只接受 `http://localhost:<port>`。
  - `topOrigin` 一出現、或 `crossOrigin` 為 true，都拒絕。
- **登記**：必須 UP 與 UV，只接受 ES256 和 `attestation=none`。收到其他 `fmt` 就拒絕（`REGISTRATION_ATTESTATION`）。
- **challenge**：
  - 由伺服器以 `SHA-256("agent-payment-authority/pay/v1/webauthn-challenge" ‖ 0x00 ‖ record)` 產生；
  - `record` 含 32 bytes 隨機數、會話（`s1`／`s2`）、`intent_hash`、jti、approver 版本與期限（120 秒）；
  - 只能用一次。
- **核准**：
  - 一律要求 UV。第 5 步只是請瀏覽器「不要求」，判定仍要求 UV。
  - BE 必須等於登記時的值，不同就拒絕並標 `suspect`；BS 可以變。
  - 計數器倒退就拒絕（`ASSERTION_COUNTER`），並標 `suspect`；兩邊都是 0 可以接受。
- **Host 與 Origin**：只接受 `localhost:<port>` 或 `127.0.0.1:<port>`。後者只是為了讓「錯 origin」頁能把瀏覽器的反應記下來。

## 結果與證據（R2）

**每次啟動都建立一個新的私人 run 目錄**，不跨次沿用結果檔：
- `--out-dir` 指定的目錄必須已經存在、屬於自己、不是 symlink，而且權限是 0700（沒有群組或其他人的權限）。
- 每次啟動會在裡面以 `mkdir` 建立 `run-<UTC 時間>-<隨機>/`（0700），再以排他方式建立 `results.jsonl`（0600）。
- 路徑已經存在（檔案、目錄或 symlink）就拒絕啟動，不會追加、不會改權限，也不會碰旁邊的檔案。
- 啟動時會印出這次結果檔的完整路徑。

結果檔是 JSONL，同一次啟動內逐行追加：
- 每行都標 `authenticator`：`real-device` 或 `virtual`，兩者不混記。
- **只記固定字詞**：
  - 拒絕原因是白名單代碼，例如 `challenge_mismatch`、`origin_mismatch`、`top_origin`、`cross_origin`、`uv_missing`、`up_missing`、`rp_id_mismatch`、`backup_eligible_changed`、`counter_regressed`、`fmt_not_none`；
  - 分不出原因時記 `other:<函式庫錯誤類型>` 或 `other`。
- **不保存**：函式庫的原始錯誤文字或 DevInfo（裡面可能有預期與收到的 challenge）、challenge 值、assertion 原文、credential ID 原文。HTTP 回覆也一樣。
- `fmt`、attachment、transports 都只留已知值，其他記成 `other`；步驟名稱只接受頁面的五個步驟。
- credential ID 只留 SHA-256 前 4 bytes。
- 也記錄 user agent（前 300 字元）、UP／UV／BE／BS、計數器、AAGUID。
- **瀏覽器端的拒絕**記成 `browser:<步驟>`，`code=BROWSER_REPORTED`，只留錯誤名稱（固定清單，例如 `NotAllowedError`、`SecurityError`）。
  - 那是頁面轉報的，不是伺服器驗證的結果。
  - 錯誤訊息只顯示在你的頁面上，不寫入結果檔。

## 定向驗證（Claude Code 執行，待 Codex 核對；都不是實機結果）

```sh
cd spikes/webauthn
GOCACHE=$TMPDIR/gocache go test -count=1 -v ./...   # 軟體 authenticator，直接呼叫 handler，不綁 port
python3 reverse.py --self-test                       # 反向分類器：只有指定測試的預期斷言才算抓到
python3 reverse.py "$EVIDENCE/reverse"               # 自己建一次性副本跑 13 項；每項與整體有時限；全數抓到才 exit 0
go build -o "$TMPDIR/a1-spike" .
(cd ../.. && node spikes/webauthn/virtual.mjs "$TMPDIR/a1-spike" "$EVIDENCE/virtual")
                                                     # headless Chrome＋CDP virtual authenticator 跑整頁（要綁 loopback）
```

`$EVIDENCE` 是一個新的目錄，不能已經存在。結果與耗時記在設計文件第 19 節。

## Aaron 的實機操作（要等 Codex 核對完成、你確認後才做；約 30～60 分鐘）

**開始前**：
- 不要開真實 Agent。
- spike 只綁 `127.0.0.1:8123`，用完按 Ctrl-C 關掉。
- 不需要、也不要為了這個 spike 安裝 app、改系統設定或登入新帳號。

```sh
cd /Users/mac/Documents/OpenSourceProjects/agent-payment-authority/spikes/webauthn
go build -o ../../bin/a1-spike .
mkdir -p ../../var/a1 && chmod 700 ../../var/a1
../../bin/a1-spike --authenticator real-device --out-dir ../../var/a1
```

啟動時會印出這次的結果檔，例如 `var/a1/run-20261013T080000Z-1a2b3c4d/results.jsonl`。
- `bin/`、`var/` 都在 `.gitignore` 裡，不會被提交。
- 每個關卡都重新啟動一次，會各有一個新的 run 目錄。

### 關卡與停止條件

依序走三個關卡（D1：Touch ID 先、再手機；FIDO2 key 另行決定）：

| 關卡 | 組合 | 進入條件 |
|---|---|---|
| A | Chrome＋這台 Mac 的 Touch ID | 無 |
| B | Safari＋Touch ID | 無（不論 A 的結果） |
| C | Chrome＋手機 | Chrome 的對話框本來就有「使用手機或平板」，而且不需要安裝或改設定；否則記為「未提供」並略過 |

每個關卡都在 `http://localhost:8123/` 依序做：

| 按鈕 | 你要做的 | 預期 |
|---|---|---|
| 1 登記 | 依瀏覽器提示選 authenticator、驗證本人 | `ok`，`fmt` 是 `none`，`UV=1` |
| 2 核准（要求 UV） | 再驗證一次本人 | `verified` |
| 3 重送同一份 | 不用碰裝置 | `CHALLENGE_USED` |
| 4 錯 challenge | 驗證一次本人 | `ASSERTION_INVALID`，原因 `challenge_mismatch` |
| 5 不要求 UV 的探測 | 依提示操作 | 只記錄：裝置回的 `UV` 是 0 還是 1 |

**錯 origin**：A、B 各做一次。打開 `http://127.0.0.1:8123/`（頁面會顯示紅色提示），按「1 登記」，預期瀏覽器拒絕，例如記成 `browser:register` 加上 `SecurityError`。

**一個關卡算「不支援」**：
- 第 1 步沒有出現該 authenticator 的選項；
- 或第 1 步／第 2 步失敗。如果是誤按取消，可以再試一次；第二次仍失敗才算。

遇到「不支援」時：
- 保存這次的結果，停止這個關卡，不要換別的方法硬試；
- 按 Ctrl-C，進入下一個關卡。
- 「不支援」照實記錄，**不算通過**。

**全部停止**：A、B、C 都沒有走到第 2 步 `verified`。
- 保存結果並停止，不自動採購 FIDO2 key，也不擴大方案。
- 交 review，由你決定下一步。

**交回**：
- `var/a1/` 底下各次的 `results.jsonl`：沒有付款金鑰或密語，credential ID 只留雜湊，錯誤只有固定代碼。
- 每個關卡一句話：瀏覽器提示了什麼、你選了什麼、有沒有要求 Touch ID 或密碼。

結論由 review 判讀；virtual 或軟體 authenticator 的結果不能記成實機 PASS。

### 測試 credential 的清理

- 實機登記在驗證器建立的 credential，網站是 `localhost`，使用者名稱是 `a1-spike-tester`／`A1 spike tester`。
- spike 不會、也不能替你刪除它。
- 要不要清理、怎麼清理，由你辨認是這次建立的項目之後自己決定、自己操作。
- 不要自動或批次刪除個人 passkey。A2 會重新登記，不沿用這些 credential。
