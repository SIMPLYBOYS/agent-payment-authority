# B 完成度驗證：本機彩排與 testnet 彩排

> 2026-11-04，Claude Code。Aaron 11/4：A～D 的方向（A 已結束，頻率限制列進 D）；「現在整理 B 的操作步驟」。
> B 的目的：在錄影（C）之前，把做好、但還沒在完整流程或真實鏈上看過的東西跑一遍；壞了就修，不加功能。

## 1 這次要看到的

| 項目 | 本機 B1 | testnet B2 |
|---|---|---|
| 三個場域、兩位客人、真實 Agent | ✓ | ✓ |
| 店名「複合式書店」、書架區（Unity 11/4 重新建置） | ✓ | ✓ |
| 經 `apa-facilitator` 結算（FA2 主路徑；testnet 上從沒跑過，只有 Host 自己結算的 t4 和 x402.org 的 fa3） | ✓ | ✓（要輸入商店金鑰的密語） |
| 證據包＋checker E1～E10 | ✓（本機鏈） | ✓（Base Sepolia） |
| checker 報告的 `facilitator` 與每筆 tx#2 的送出者 | ✓ | ✓ |
| 展示視窗的「Agent 身分 ERC-8004 #10573／#10574」 | —（本機鏈不顯示，E6 A） | ✓ |
| checker 報告的 `agents`（鏈上的擁有者、agentWallet、宣告的錢包） | — | ✓ |

## 2 B1 本機彩排（現在就可以跑）

開場前我已經唯讀查過（11/4）：8402、8403、8545 都空著；`var/present.conf` 的 `cli=` 指向 `var/cli/claude-2.1.301`，那個檔案還在；用過的本機標籤是 town1～town4，這次用 **town5**。

| # | 誰 | 做什麼 | 可以貼回的 |
|---|---|---|---|
| 1 | 你 | `sh scripts/demo/demo-local.sh live town5`，提示時輸入開發用 Signer 的密語；頁面會自動打開，**配對碼不要貼回** | `Host 啟動中` 之後的訊息，以及 `e1-evidence: start recorded …` 那一行（應該寫 `settled by the facilitator http://127.0.0.1:8403`） |
| 2 | 你 | 簡報模式 → 01 複合式書店 → 一起開始：A、B 依序用裝置確認委託；A 的首爾攻略 0.50 要你用裝置核准；B 的講義 0.06 自動付款；等兩位都「已交付」 | 兩位的最後狀態 |
| 3 | 你 | 02 街角烘焙咖啡 → 一起開始：A 的午餐 0.08 自動付款；B 的生日蛋糕 0.42 要家長（你）核准 | 同上 |
| 4 | 你 | 03 巷口便利商店 → 一起開始：A 的啤酒 0.09 自動付款；B 的啤酒在付款前被擋（憑證不符這間店的要求），錢沒有動 | 同上 |
| 5 | 你 | **先不要停**，告訴我做完了 | — |
| 6 | Claude Code | `e1-evidence pack`（6 場）＋`verify-demo --mode e1a --rpc http://127.0.0.1:8545` | — |
| 7 | 你 | 我回報後再 Ctrl-C | — |

- 要重開某一位客人時，只開始那一位（主持人手冊 §4）；不要用「一起開始」重開。
- 模型每一場都會真的執行，共 6 次。

## 3 B2 testnet 彩排（台北 11/5 08:00 之後、11/6 10:23 之前）

**時間的限制**：
- relayer 和商店的每日上限都照 UTC 的日期算。三個場域要 relayer 11 筆（每位客人登記 1 筆，加上 5 筆預留；B 的啤酒在付款前就被擋，沒有預留），上限是 12。今天（UTC 11/4）fa3 已用掉 2 筆，不夠，所以要等台北 11/5 08:00 換日。
- 委託 11/6 05:23 UTC（台北 13:23）到期，開場前檢查要求至少還剩 3 小時，所以最晚台北 11/6 10:23 開始。
- 超過這個時間，就要先重新產生委託（`opt-signer`，要你用 sudo 安裝）。
- 額度剛好夠：relayer 11/12、商店 5/6，等於**沒有重開的餘地**。中途出錯就照實記下，不要在同一天重跑。

**額度**：委託的累計上限各 1.00（自動付款才算）。已知用掉的：家長 0.16（t4 的兩次午餐），孩子 0.12（t4 的週報、fa3 的講義）。這次要家長 0.17、孩子 0.06，夠用。Signer 的實際紀錄在 `/opt`，我沒有讀；不夠的話，Signer 會拒絕並回 `OVER_TOTAL_CAP`。錢包餘額和 gas 由腳本的開場前檢查核對。

| # | 誰 | 做什麼 | 可以貼回的 |
|---|---|---|---|
| 0 | Claude Code | 開始前一刻唯讀再查：委託的剩餘時間、錢包餘額、8402／8403 空著 | — |
| 1 | 你 | `sh scripts/demo/demo-testnet.sh live b1 town`（不設 `APA_FACILITATOR`，就是 `apa-facilitator`）：依序輸入 sudo 的密碼、relayer 的密語、Signer 的密語；接著出現 `present: facilitator：請輸入商店金鑰的密語`，在 `apa-facilitator shop key passphrase:` 輸入商店金鑰的密語 | 開場前檢查的結果、`e1-evidence: start recorded …` 那一行（應該寫 `settled by the facilitator http://127.0.0.1:8403`） |
| 2 | 你 | 主持人頁按「開啟展示視窗」。這個按鈕只在簡報模式外出現：先開好展示視窗，再進簡報模式（場景移到展示視窗播放，主持人頁的簡報模式中間只寫「場景在展示視窗播放；這一頁只用來操作。」，Aaron 11/6） | — |
| 3～5 | 你 | 同 B1 的 2～4，三個場域各按一次「一起開始」 | 每個場域兩位的最後狀態 |
| 6 | 你 | 付款之後看展示視窗：「為什麼可信」第一項，以及收據卡上的「Agent 身分 ERC-8004 #10573」（家長）、「#10574」（孩子），點了會開 Basescan | 看到或沒看到 |
| 7 | 你 | **先不要停**，告訴我做完了 | — |
| 8 | Claude Code | `e1-evidence pack`＋`verify-demo --mode e1a --rpc https://sepolia.base.org`（在 sandbox 外跑，sandbox 裡驗不了 TLS） | — |
| 9 | 你 | 我回報後再 Ctrl-C | — |

**預期的 checker 報告**：
- E1～E10 全部 PASS。
- `facilitator` 是 `http://127.0.0.1:8403`。
- `settlements` 5 筆，送出者都是商店的 `pay_to` `0x6bbe…773e`（`apa-facilitator` 用商店金鑰送，和 t4 一樣，但這次經過 facilitator）。
- `agents` 2 筆：擁有者都是 issuer `0xccd7…4c6a`，`agentWallet` 為 0，宣告的錢包各是自己的。

## 4 結果

**B1 本機彩排（Aaron 11/4，標籤 town5）**
- 7 場都是真實 Agent（`claude-2.1.301`，模型 `claude-sonnet-5-5`），每場都只請購該買的那一件：
  - 書店：家長的首爾攻略走 Ask，跑了兩場（a-01 先單獨開始，a-02 是之後的「一起開始」），兩場都核准並付款；孩子的講義走 Allow（b-01）。
  - 咖啡店：家長的午餐走 Allow（a-03）；孩子的生日蛋糕走 Ask，核准並付款（b-02）。
  - 便利商店：家長的啤酒走 Allow（a-04）；孩子的啤酒在付款前被擋，錢沒有動（b-03）。
  - 家長在書店沒有理會「系統通知」，沒有去買進階版。
- 證據包 `artifacts/e1a/rehearsal-town5`（run `e1a-20261104T143948Z-681e47`，結束區塊 185，程式 `6108b58`）。`verify-demo --mode e1a --rpc http://127.0.0.1:8545`：**E1～E10 全部 PASS**，沒有 findings；報告在 `artifacts/e1a/rehearsal-town5.report.json`（只在本機）。
- `facilitator` 是 `http://127.0.0.1:8403`；6 筆 tx#2 的送出者都是 `0x15d3…6a65`（anvil 的 key 4），也就是 `apa-facilitator`。這是 FA2 的主路徑第一次和真實 Agent、三個場域一起完整跑完。
- `agents` 是空的：本機的 registry 沒有身分（E6 A），符合預期。
- **要注意**：家長的攻略付了兩次，原因是先單獨開始了家長，又按了「一起開始」，家長就又跑了一次（同 10/31 S5 的午餐）。本機沒有關係；但 B2 的 relayer 和商店額度剛好夠，**B2 每個場域只按一次「一起開始」，之前不要單獨開始任何一位**。

**B2 testnet 彩排，第一輪（Aaron 11/5 台北 15:31～15:36，標籤 b1）**
- 第一次啟動停在第 2 步：relayer 打不開自己的金鑰檔（`operation not permitted`）。原因是 macOS 擋下了這個 App 對「文件」資料夾的存取，連 Claude Code 也讀不到。Aaron 在系統設定恢復權限後重跑。relayer 沒有啟動，沒有用到額度。
- 跑了 5 場：

  | 場次 | 結果 |
  |---|---|
  | 書店・家長的首爾攻略 0.50（a-01） | Ask → 語音挑戰沒過（`VOICE_PHRASE_MISMATCH`）→ 核准被拒，沒有付款 |
  | 書店・孩子的講義 0.06（b-01） | Allow，已付款、已交付 |
  | 咖啡店・家長的午餐 0.08（a-02） | Allow，已付款、已交付 |
  | 咖啡店・孩子的生日蛋糕 0.42（b-02） | Ask → 語音挑戰沒過（同上）→ 沒有付款 |
  | 便利商店・孩子的啤酒（b-03） | 付款前 Deny `CREDENTIAL_CLAIM_UNMET`，符合預期 |
  | 便利商店・家長的啤酒 | 沒有開始 |

- 證據包 `artifacts/e1a/rehearsal-b1`（run `e1a-20261105T073834Z-0661a4`，結束區塊 54712217，程式 `bf70976`）。`verify-demo --mode e1a --rpc https://sepolia.base.org`：**E1～E10 全部 PASS**，沒有 findings。
  - `facilitator` 是 `http://127.0.0.1:8403`；2 筆結算的送出者都是商店的 `0x6bbe…773e`。
  - `agents` 2 筆：#10573（家長）、#10574（孩子），擁有者都是 issuer `0xccd7…4c6a`，`agentWallet` 為 0，宣告的錢包各是自己的。展示視窗上的身分，Aaron 還沒回報有沒有看到。
- **語音為什麼沒過**：Signer 的 `voice_results` 只記每一項對不對，沒有錄音和逐字稿。
  - 蛋糕：金額錯、商家對、代碼對，聲紋 0.387，門檻是 0.365。
  - 攻略：三項都錯，聲紋 0.051，像是沒錄到那段話：可能在「錄音中」出現前就念了。
  - 本機的麥克風測試（`v1-mic-check.mjs`，只啟動語音服務）：自然地念「0.42」，五種錄音方式都辨識成「0.12」；一個字一個字念「零、點、四、二」就辨識成「0.42」（Aaron 11/5）。代碼本來就是分開念的，一直都對。
  - 比對不放寬：金額必須完全一致，這是核准的內容。改用念法解決：金額像代碼一樣分開念，等「錄音中」出現再念。之後可以考慮讓 Signer 把金額也分開顯示（要重裝 `/opt` 的 Signer，彩排之後再說）。
- 額度：今天（UTC 11/5）用了 relayer 7 筆（5 次登記、2 次預留）、商店結算 2 筆。補跑的三場要 relayer 6 筆，今天只剩 5 筆。

**B2 第二輪（Aaron 11/5 台北 16:51～16:55，標籤 b2）**
- 為了當天補跑，Aaron 把 `var/p2-testnet/relayer.json` 的 `daily_tx_cap` 從 12 調到 18（開場前檢查在城鎮模式要求今天還剩 11 筆）。
- 三個場域各按一次「一起開始」，6 場全跑：

  | 場次 | 結果 |
  |---|---|
  | 書店・家長的首爾攻略 0.50（a-01） | Ask → **語音挑戰通過**（金額與代碼一個字一個字念）→ 核准、付款、交付 |
  | 書店・孩子的講義 0.06（b-01） | Allow，付款、交付 |
  | 咖啡店・家長的午餐 0.08（a-02） | Allow，付款、交付 |
  | 咖啡店・孩子的生日蛋糕 0.42（b-02） | Ask → **語音挑戰通過** → 家長核准、付款、交付 |
  | 便利商店・家長的啤酒 0.09（a-03） | 憑證只揭露「已滿 18 歲」、Allow、預留、簽名都完成；**商店沒有結算**（`PAYMENT_NEEDS_REVIEW`，`VERIFICATION_FAILED`），畫面寫「商店回覆沒有接受、沒有送出」 |
  | 便利商店・孩子的啤酒（b-03） | 付款前 Deny `CREDENTIAL_CLAIM_UNMET` |

- **家長的啤酒為什麼沒結算**：商店每天 6 筆的上限，是 facilitator 自己的結算紀錄在算（`payments.json` 的 `dir`：`var/p2-testnet/host`）。這份紀錄**不分標籤**，兩輪共用。第一輪 2 筆加上這一輪前 4 筆，剛好 6 筆，啤酒是第 7 筆，被拒絕。先前估的「5／6」以只補 3 場為前提，實際跑了 6 場。
  - 錢沒有動：鏈上沒有這一筆轉帳（checker 的 `settlements` 沒有它）。簽好的授權 5 分鐘（`valid_before`）後就失效，之後也不能再拿去結算。
  - facilitator 的紀錄檔只有啟動那一行，沒有寫出拒絕的原因。上面的判斷是從次數和程式推出來的。
- 證據包 `artifacts/e1a/rehearsal-b2`（run `e1a-20261105T085913Z-0981f8`，結束區塊 54736263，程式 `2e88cfb`）。`verify-demo --mode e1a --rpc https://sepolia.base.org`：**E1～E10 全部 PASS**，沒有 findings。
  - `facilitator` 是 `http://127.0.0.1:8403`；4 筆結算的送出者都是商店的 `0x6bbe…773e`。
  - `agents`：#10573（家長）、#10574（孩子）。

- 跑完後 Aaron 把 relayer 的上限改回 12。
- **展示視窗的身分**：第二輪跑的時候沒有開展示視窗。「開啟展示視窗」只在簡報模式外顯示，簡報模式的卡片上，身分只是一個「Agent 身分 ↗」標籤，看不到編號。
  - Aaron 之後開了展示視窗：「客人 A・為什麼可信」的第一項是「Agent 身分　ERC-8004 #10573（ERC-8004・平台代為註冊）」，付款預留那一格也讀到了鏈上紀錄。
  - 客人 B 那一排沒有身分：身分是跟著鏈上資訊一起帶來的，頁面要等那位客人有付款紀錄才去讀；B 的啤酒在付款前就被擋了。這是小缺口，不是錯誤。

**B2 合起來**：
- 這一期要在 testnet 上看到的都看到了：
  - 展示視窗上的 Agent 身分（ERC-8004 #10573）。
  - 經 `apa-facilitator` 的結算，包括兩筆語音核准過的 Ask。
  - checker 的 `facilitator` 與 `agents`：兩個 Agent 的 ERC-8004 身分都在鏈上讀到了。
- 唯一沒有在 testnet 上結算的是家長的啤酒。它的憑證與裁決都過了，只有結算撞上商店的上限；它的結算路徑與午餐相同，午餐兩輪都結算了。
- 要學到的：
  - 語音核准時，金額與代碼要分開念，並且等「錄音中」出現再念。
  - 估商店額度時，要把當天同一把商店金鑰的所有結算都算進去，不只是這個標籤的。

## 5 重開 A 之後：`/opt` 與 testnet（11/8 起，待做）

> Aaron 11/7 重開 A 的三項（付款錢包證明 `docs/commercial-8004.md` §8、退款 `docs/commercial-refund.md`、頻率限制 `docs/commercial-velocity.md` §8）都已在本機完成。testnet 照 memory 的原則只彩排**一場**，一起看所有還沒在真實鏈上看過的東西；第 1 項與第 3 項都要換 `/opt` 的 Signer，所以只裝一次。

**這一場要看到的**

| 項目 | 怎麼看 |
|---|---|
| 兩位 Agent 的 `agentWallet` 是自己的付款錢包 | `agent-wallet` 之後鏈上讀回；展示視窗的「Agent 身分」寫「付款錢包已由錢包簽章證明」；checker 的 `agents` 兩筆 `agent_wallet_is_consumer_wallet: true` |
| 郵局與飯店上 Base Sepolia | 飯店 A 付款前被拒（`MERCHANT_NOT_ALLOWED`）、B 交付；郵局 A 交付失敗、B 交付 |
| 退款 | 郵局 A 按「商店退款」，`apa-facilitator` 用商店金鑰送出，Host 向鏈上確認後記下；checker E4 查退款、報告列 `refund` |
| 頻率限制 | 正常場次不會碰到（每位客人最多 4 筆 Allow 或 Ask）；`/opt` 的 signer.json 與 run set 的 profile 都帶 5 筆／600 秒 |
| E1～E10 | 全部 PASS |

**額度**（UTC 日，台北 08:00 歸零；同一天的所有標籤共用，見 memory）：

| 上限 | 這一場要 | 現在 | 要做的 |
|---|---|---|---|
| relayer 每日 | 18 筆（10 次登記、8 次預留） | `var/p2-testnet/relayer.json` 的 `daily_tx_cap` 已改成 18（Aaron 11/8；relayer 下次啟動時生效） | — |
| 商店每日結算 | 8 筆（退款不算） | `demo-testnet.sh` 的城鎮已改成 `--shop-cap 9` | — |
| 錢包餘額 | 家長 0.75、孩子 0.56 | 開場前檢查（`testnet-preflight.py town`，已改成五個場域） | — |
| 委派總額 | 家長 Allow 0.25、孩子 0.14（每場） | `dlg-ta`、`dlg-tb` 各 1.00，跨場次累計（Signer 的紀錄在 `/opt`，我沒有讀） | 不夠時 Signer 會回 `OVER_TOTAL_CAP` |
| 委派期限 | — | 重新產生 signer.json 就有 7 天 | 下面第 3 步 |

**步驟**

| # | 誰 | 做什麼 |
|---|---|---|
| 1 | 你 | 看過程式與文件後說「請提交」 |
| 2 | Claude Code | 從提交後的乾淨 worktree 建置 `apa-signer`（`GOFLAGS=-mod=readonly GOPROXY=off GOTOOLCHAIN=local`）到 `var/reopen-opt/apa-signer`，記下 SHA-256 |
| 3 | 你 | `sudo cat /opt/apa-signer/data/signer.json > var/reopen-opt/signer.base.json`（`var/reopen-opt` 是 0700）；Claude Code 執行 `go run ./cmd/p2-testnet opt-signer --dir var/p2-testnet --base var/reopen-opt/signer.base.json --out var/reopen-opt/signer.json`（帶上兩位 Agent 的登記、頻率上限、新的 7 天委派），印出新檔的 SHA-256 |
| 4 | 你 | 停下 `/opt` 的服務（`pgrep -fl '/opt/apa-signer/'` 應無輸出），備份（留著，不刪）：`sudo -u apa-signer /bin/cp -p /opt/apa-signer/data/signer.json /opt/apa-signer/data/signer.json.reopen`、`sudo /bin/cp -p /opt/apa-signer/bin/apa-signer /opt/apa-signer/bin/apa-signer.reopen` |
| 5 | 你 | 安裝：`sudo install -o root -g wheel -m 0755 var/reopen-opt/apa-signer /opt/apa-signer/bin/apa-signer`、`sudo -u apa-signer /bin/sh -c 'umask 077; cat > /opt/apa-signer/data/signer.json' < var/reopen-opt/signer.json`；`sudo shasum -a 256 /opt/apa-signer/bin/apa-signer /opt/apa-signer/data/signer.json` 要等於第 2、3 步的 |
| 6 | 你 | 只啟動 Signer（relayer 不要開，它也拿著 issuer 的金鑰）：`cd /; sudo -u apa-signer /opt/apa-signer/bin/apa-signer serve --dir /opt/apa-signer/data`，輸入錢包的密語 |
| 7 | 你 | 另一個終端機：`go run ./cmd/p2-testnet agent-wallet --dir var/p2-testnet`。它印出計畫（兩筆 `setAgentWallet`，issuer 付 gas），打 `yes`、輸入 issuer 的密語；**這是兩筆 testnet 交易，要你核准**。完成後印出兩位的 `agentWallet`，那幾行可以貼回。然後 Ctrl-C 停掉 Signer |
| 8 | 你 | （第 1 項就算完成）決定 relayer 的上限；之後挑一個 UTC 日開場：`sh scripts/demo/demo-testnet.sh live <新標籤> town`，五個場域各按一次「一起開始」；郵局 A 交付失敗後按交易卡上的「商店退款」 |
| 9 | Claude Code | `e1-evidence pack`＋`verify-demo --mode e1a --rpc https://sepolia.base.org`（sandbox 外），回報 E1～E10、`agents`、`settlements` 的 `refund` |

回滾：停掉 Signer，把第 4 步的備份放回原位。testnet 上已送出的交易不能撤回。

**進度**
- 11/8 第 1 步：提交 `6b7a025`。
- 11/8 第 2 步：`var/reopen-opt/apa-signer`，SHA-256 `70287db42049fa1c0adda679d669bfaac2d70badee9fea12c6bacb2931dc7736`，從 `6b7a025` 的乾淨 worktree 建置（sandbox 裡建置，執行檔沒有記下 vcs 資訊）。
- 11/8 第 3 步：`var/reopen-opt/signer.json`，SHA-256 `875f61c9aecf927fa03628bbb0ce9bbed7902106c75536358422e163c6a9bb2f`：`wallet-a` 對 agent 10573、`wallet-b` 對 10574（擁有者 issuer `0xccd7…4c6a`），velocity 5 筆／600 秒，委派到台北 11/15 12:25。
- 11/8 第 4～7 步：Aaron 備份、安裝、只啟動 Signer（`serving on /opt/apa-signer/run/signer.sock`）；`/opt/apa-signer/bin/apa-signer` 的 SHA-256 我核對過等於第 2 步的（signer.json 我讀不到）。`agent-wallet` 送出兩筆 `setAgentWallet`，`cast` 獨立核對兩位的 `agentWallet` 都是自己的付款錢包（`docs/commercial-8004.md` §8）。第 1 項在鏈上完成；第 8、9 步待做。
- Aaron 11/8：第 8、9 步的 testnet 彩排延到 11/9。開場前我先唯讀檢查委派期限（到台北 11/15 12:25）、餘額、當天 UTC 日已用的 relayer 與商店筆數、連接埠。
