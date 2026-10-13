# A1 WebAuthn 實機可行性 spike

依 `docs/commercial-human-approval-design.md` 第 2、4 節與第 17 節（Aaron 10/13 決定 D1～D7、核准另行派工 A1）。

**範圍**：
- 只回答一件事：這台 macOS 12.7.6 上，哪個瀏覽器加哪種 authenticator，能在 `rp_id=localhost` 完成登記與一次核准；錯 challenge、重送、錯 origin、不要求 UV 時實際會怎樣。
- **只供測試**：不付款、不簽交易、不連鏈、沒有 Signer、沒有金鑰、沒有真實 Agent、不碰 Store。
- 請購是固定的一筆合成資料。狀態只在記憶體，程式結束就消失。
- 不是產品程式：A2 會在 Signer 裡重做，這裡的程式與 credential 都不沿用（A2 重新登記，D3）。
- 獨立 Go module，go-webauthn v0.18.2 不進主 `go.mod`；它帶進的 `x/crypto v0.57.0`、`x/sys v0.48.0` 與主 module 的釘選相同。

## 伺服器規則（D6）

- RP ID `localhost`；origin 只接受 `http://localhost:<port>`。`topOrigin` 一出現、或 `crossOrigin` 為 true 都拒絕。
- 登記：必須 UP 與 UV、只接受 ES256、`attestation=none`（收到其他 `fmt` 就拒絕，`REGISTRATION_ATTESTATION`）。
- challenge：由伺服器以 `SHA-256("agent-payment-authority/pay/v1/webauthn-challenge" ‖ 0x00 ‖ record)` 產生，`record` 含 32 bytes 隨機數、會話（`s1`／`s2`）、`intent_hash`、jti、approver 版本與期限（120 秒）。只能用一次。
- 核准：
  - 一律要求 UV。第 5 步只是請瀏覽器「不要求」，判定仍要求 UV。
  - BE 必須等於登記時的值，不同就拒絕並標 `suspect`；BS 可以變。
  - 計數器倒退就拒絕（`ASSERTION_COUNTER`）並標 `suspect`；兩邊都是 0 可以接受。
- 只接受 Host／Origin 為 `localhost:<port>` 或 `127.0.0.1:<port>` 的請求；後者只為了讓「錯 origin」頁能把瀏覽器的反應記下來。
- 結果檔：JSONL，只會追加，權限 0600。
  - 每一行都標 `authenticator`：`real-device` 或 `virtual`，兩者不混記。
  - credential ID 只留 SHA-256 前 4 bytes。
  - 也記錄 user agent、UP／UV／BE／BS、計數器、`fmt`、AAGUID、attachment、transports。
  - 瀏覽器端的拒絕記成 `browser:<步驟>`，`code=BROWSER_REPORTED`：那是頁面轉報的，不是伺服器驗證的結果。

## 定向驗證（Claude Code 已跑；不是實機結果）

```sh
cd spikes/webauthn
GOCACHE=$TMPDIR/gocache go test -count=1 ./...         # 軟體 authenticator（P-256），直接呼叫 handler，不綁 port
D=$(mktemp -d) && cp -R . "$D"/ && python3 reverse.py "$D"   # 反向：拿掉每條規則，測試必須失敗
go build -o "$TMPDIR/a1-spike" . && (cd ../.. && node spikes/webauthn/virtual.mjs "$TMPDIR/a1-spike" "$(mktemp -d)")
                                                        # Chrome headless＋CDP virtual authenticator 跑整頁（要綁 loopback）
```

## Aaron 的實機操作（約 30～60 分鐘）

**開始前**：
- 不要開真實 Agent。
- 這個 spike 只綁 `127.0.0.1:8123`，用完按 Ctrl-C 關掉。

```sh
cd /Users/mac/Documents/OpenSourceProjects/agent-payment-authority/spikes/webauthn
go build -o ../../bin/a1-spike .
mkdir -p ../../var/a1 && chmod 700 ../../var/a1
../../bin/a1-spike --authenticator real-device --out ../../var/a1/real-device-results.jsonl
```

`bin/`、`var/` 都在 `.gitignore` 裡，結果檔不會被提交。

**每一個組合都重啟一次 spike**（Ctrl-C，再執行上面最後一行）。credential 只存在記憶體，重啟後核准時只會列出這個組合的 credential。結果檔會一直追加，靠 user agent 與時間區分。

依序試這些組合（D1：Touch ID 先、再手機；FIDO2 key 另行決定）：

1. Chrome + Touch ID（這台 Mac 的 Touch ID）
2. Safari + Touch ID
3. Chrome + 手機（瀏覽器出現「使用手機或平板」時才試；需要藍牙）

每個組合都在 `http://localhost:8123/` 依序按下列按鈕：

| 按鈕 | 你要做的 | 預期 |
|---|---|---|
| 1 登記 | 依瀏覽器提示選 authenticator、驗證本人 | `ok`，`fmt` 是 `none`，`UV=1` |
| 2 核准（要求 UV） | 再驗證一次本人 | `verified` |
| 3 重送同一份 | 不用碰裝置 | `CHALLENGE_USED` |
| 4 錯 challenge | 驗證一次本人 | `ASSERTION_INVALID`（challenge 不符） |
| 5 不要求 UV 的探測 | 依提示操作 | 只記錄：看裝置回的 `UV` 是 0 還是 1 |

**錯 origin**：每個瀏覽器做一次即可。
- 打開 `http://127.0.0.1:8123/`，頁面會顯示紅色提示。
- 按「1 登記」，預期瀏覽器拒絕（例如 `SecurityError`）。結果會記成 `browser:register`。

**取消也算結果**：瀏覽器沒有出現任何 authenticator 選項、或你按了取消，頁面會把錯誤記成 `browser:…`。

**裝置不支援時**（Aaron 10/13 的條件）：
- 保存結果檔並停止，不再換方案。
- 不自動採購 FIDO2 key，也不擴大範圍。
- 把結果交 review，由你決定下一步。

**交回**：
- `var/a1/real-device-results.jsonl`：沒有金鑰或密語，credential ID 只留雜湊。
- 每個組合一句話：瀏覽器提示了什麼、你選了什麼、有沒有要求 Touch ID 或密碼。

結論由 review 判讀；virtual 或軟體 authenticator 的結果不能記成實機 PASS。
