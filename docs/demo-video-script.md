# 宣傳影片腳本與錄影計畫

> 2026-11-01 起草；11/2 依 `docs/story.md`（五幕、一家人、三種怕）重寫。Aaron 11/1：兩支，**90 秒宣傳版**與 **3 分鐘完整版**，從同一場錄影剪出；**中文配音、英文字幕**。
> Aaron 11/2：錄影版不需要真的上鏈（本機鏈）；面試現場用 testnet 的證據包重新查證。
> **Aaron 11/5：改在 Base Sepolia 測試網錄**（路線圖 C）。理由：A 做出來的 Agent 鏈上身分（ERC-8004）只在測試網上顯示；同一場的證據包就是面試現場查證用的那一份；B2 已在測試網跑過一整輪（`docs/commercial-rehearsal.md` 第 4 節）。
> **Aaron 11/6：影片改成自動產生的示意動畫，插入 11/6 testnet 實跑的片段**（第 7 節）。第 2、6 節的現場錄影只留作紀錄；第 3、4 節的腳本照舊，模擬的畫面一律標「示意動畫」。

## 1 原則

- 故事照 `docs/story.md`：「讓 AI 替我花錢，我最怕三件事：被騙著亂花、個資被拿走、出了事說不清楚。」一家人：客人 A 是家長，客人 B 是未滿 18 歲的孩子。
- **只說畫面上看得到、證據包裡查得到的事。** 測試網就說測試網，測試幣就說測試幣，不說「完全安全」。
- 真實 Agent 那場一次錄到底，影片只剪、不重演。「被騙的 Agent」是 fake 主機在本機鏈上的模擬，畫面上一律標「模擬」，不和真實那場混成同一場。
- 旁白講「為什麼可以放心」；技術名詞（ERC-8004、WebAuthn、x402、BBS+、Groth16 ZK、EIP-3009）出現在可信度區的標籤與字幕。

## 2 錄什麼

**主場：真實 Agent、Base Sepolia 測試網**（`sh scripts/demo/demo-testnet.sh live <新標籤> town`）

| 來源 | 錄法 | 用在 |
|---|---|---|
| 展示視窗（1920×1080） | QuickTime 錄整個螢幕，從開場錄到最後 | 主畫面：城鎮、店內、五步、可信度（第一項是 Agent 身分）、收據 |
| 主持人頁（另一個螢幕） | QuickTime 錄整個螢幕 | 特寫：委託條款、攻略的商品說明（藏著「系統通知」）、核准面板與語音挑戰、「這間店看到」 |
| 裝置的 PIN 視窗 | 跟著它出現的那個螢幕一起錄到 | 委託確認、核准 |
| checker | 終端機字放大，跑 `verify-demo --rpc https://sepolia.base.org`，錄到 E1～E10 PASS | 終幕 |
| Basescan | 收據卡上的連結（預留、付款、Agent 身分） | 終幕的特寫，可以事後補錄 |

測試網版有語音挑戰：等「錄音中」出現再念，金額與代碼一個字一個字念（B2：自然地念「0.42」會被辨識成「0.12」）。

**模擬：被騙的 Agent**（`make demo-local`，fake Agent、本機鏈）：複合式書店選「被誘導的 Agent（模擬）」，錄到「超出預算，已拒絕」、錢沒有動。

配音事後另錄（安靜環境），英文字幕用第 3、4 節的英文欄。

## 3 90 秒宣傳版

| 秒 | 幕 | 畫面 | 中文旁白 | English subtitle |
|---|---|---|---|---|
| 0–10 | 序幕 | 黑底字卡 → 城鎮全景（主持人頁「全景：走一趟整條街」，鏡頭平移整條街；剪輯時加速），一家人在街上 | AI 已經可以替我們買東西了。可是讓 AI 替我花錢，我最怕三件事：被騙著亂花、個資被拿走、出了事說不清楚。 | AI can shop for us now. But letting it spend my money, I worry about three things: being tricked into spending, giving away my data, and not knowing what happened. |
| 10–22 | 一、設定 | Didit 的沙盒驗證頁（證件、自拍說明）；字卡：「家長通過身分驗證，才拿到憑證」（小字：Didit 沙盒，沒有驗證真人）；家長在裝置上確認委託（11/11） | AI 只能提出要買什麼。家長先通過身分驗證，再在自己的裝置上設好：這次最多花 1.5，超過 0.1 都要問我。 | The AI can only propose. The parent passes an identity check, then sets the limits on their own device: at most 1.50, over 0.10 comes back to them. |
| 22–40 | 二、複合式書店 | 主持人頁特寫：攻略說明裡的「系統通知」；家長在裝置上核准、念出金額與代碼；切到模擬片段（標「模擬」）：被騙的 Agent 去買 2.00 進階版 →「超出預算，已拒絕」 | 書店的頁面藏了一句話，叫 AI 順便買 2 塊錢的進階版。真的 AI 沒上當；就算它被騙了，也花不出預算外的錢。 | The shop's page hides an instruction to also buy a 2.00 premium edition. The real AI ignored it — and even a fooled one can't spend beyond the budget. |
| 40–54 | 三、街角烘焙咖啡 | 「這間店看到：沒有任何個人資料」；孩子的生日蛋糕，家長在裝置上核准 | 孩子的 AI 想訂 0.42 的生日蛋糕，超過範圍，交給家長核准。這間店，什麼個資都看不到。 | The child's AI wants a 0.42 birthday cake. That's over the limit, so the parent approves. The shop sees no personal data at all. |
| 54–66 | 四、巷口便利商店 | 家長的啤酒交付；孩子的卡片「不符合這間店的要求」 | 便利商店賣酒，只問一件事：滿 18 歲了嗎？孩子的 AI 想買，付款前就被擋下，錢沒有動。 | At the corner store, beer needs one fact: over 18? The child's AI tries, and it stops before any money moves. |
| 66–80 | 終幕 | 可信度區逐步亮起（Agent 身分 ERC-8004、Groth16 ZK、EIP-3009 的標籤）；收據卡與 Basescan | 每一步都留下證據：替你買東西的 AI 有自己的鏈上身分，零知識證明預留預算，結算記在鏈上，交付留下收據。 | Every step leaves evidence: the AI buying for you has its own on-chain identity, a zero-knowledge proof reserves the budget, settlement lands on chain, delivery leaves a receipt. |
| 80–90 | 終幕 | 終端機 E1～E10 PASS（向 Base Sepolia 重算）→ 結尾字卡：定位句（`docs/positioning.md` §1）、名字、repo | 出了事，任何人都能自己查證。 | And if anything goes wrong, anyone can check for themselves. |

## 4 3 分鐘完整版

| 秒 | 幕 | 畫面 | 中文旁白（要點） | English subtitle（要點） |
|---|---|---|---|---|
| 0–20 | 序幕 | 字卡；城鎮 | 讓 AI 替我花錢，最怕被騙著亂花、個資被拿走、出了事說不清楚。 | Letting AI spend for me, I fear being tricked, losing my data, and not knowing what happened. |
| 20–40 | 誰握有什麼權力 | `docs/architecture.md` 的角色圖 | AI 只能提出，它在鏈上有自己的身分；替它跑的程式沒有私鑰；簽名在另一個系統帳號裡，只簽本人同意過的範圍；查帳的人只讀證據和鏈。 | The AI proposes, under its own on-chain identity. The program running it holds no key. Signing lives in its own OS account, within limits the person agreed to. The auditor reads only the evidence and the chain. |
| 40–60 | 一、設定 | Didit 的沙盒驗證頁；字卡：身分憑證（Didit eKYC 沙盒）；家長、孩子的委託 PIN（11/11） | 家長通過身分驗證，才拿到證明「我是誰」的憑證；委託寫下能花多少、超過多少要問。孩子的委託，由家長在自己的裝置上確認。 | The parent passes an identity check to get the credential that says who they are; the mandate says how much, and when to ask. The child's is confirmed on the parent's device. |
| 60–105 | 二、複合式書店：被騙著亂花 | 攻略報價（x402）與藏起來的「系統通知」；這間店看到「國家 台灣」；家長在裝置上核准、念出金額與代碼；tx#1、tx#2、收據；模擬：被騙的 Agent 被拒 | 書店只知道你在哪個國家；超過範圍由本人核准，還要念出這一筆的金額；誘導文字騙不倒真實的 AI，就算騙倒了也花不出預算外的錢。孩子的講義 0.06 在範圍內，自動付款。 | The bookshop learns only your country; the person approves above the limit and reads this payment's amount aloud; the trap didn't fool the real AI, and a fooled one still can't overspend. The child's 0.06 notes pay on their own. |
| 105–130 | 三、烘焙咖啡：個資 | 「這間店看到：沒有任何個人資料」；孩子的蛋糕由家長核准；家長的午餐自動付款 | 買蛋糕不需要知道你是誰。孩子的大額購買，由家長核准。 | Buying a cake needs nothing about you. The child's bigger purchase goes to the parent. |
| 130–150 | 四、便利商店：個資＋安全 | 家長「已滿 18 歲」交付；孩子「未滿 18 歲」被擋下 | 同一份憑證，在這裡只說「滿不滿 18 歲」，不給生日。孩子的 AI 買酒，付款前就被擋下。 | The same credential says only "over 18 or not", never a birth date. The child's AI is stopped before paying. |
| 150–168 | 終幕：說得清楚 | 收據卡上的 Agent 身分與 Basescan；終端機 `verify-demo`；E1～E10 逐條 PASS，報告列出 facilitator 與兩個 Agent；字卡：「也會在付款前擋下：商家不在名單、委託或授權過期、重放用過的授權」 | 查帳不用相信這個系統：只讀證據、向鏈重算，十項全部通過，連付款經過誰結算、是哪兩個 AI 買的都列出來。影片演了兩種被擋下的情況；商家不在名單、授權過期、重放用過的授權，也都會在付款前被擋下，都有自動測試。 | The check trusts none of this system: it reads the evidence, recomputes against the chain, and all ten pass — down to who settled each payment and which two AIs bought. Beyond the two refusals shown, an unlisted shop, an expired authorization and a replayed one are all stopped before paying, and each is covered by tests. |
| 168–180 | 照實說與結尾 | 字卡：限制；定位句（`docs/positioning.md` §1）；名字 | x402 處理付款；這一層證明這筆錢是為了哪個任務、經過誰的同意才付出去的。這段在 Base Sepolia 測試網、用測試幣錄；被騙那一段是本機的模擬；身分驗證接的是 Didit 的沙盒，沒有驗證真人（11/11）；AI 的鏈上身分由平台代為註冊。下一步是…… | x402 moves the money; this layer proves who allowed it, for what, and within which limits. Recorded on the Base Sepolia testnet with test coins; the fooled agent is a local simulation; the credential comes from a test issuer, eKYC is next; the AIs' on-chain identities were registered by the platform. |

結尾的「下一步」由 Aaron 決定。

## 5 面試現場：播完之後

播完影片，用電腦現場重新查證**錄影那一場的證據包**（錄好之前，用 B2 的 `artifacts/e1a/rehearsal-b1`、`rehearsal-b2`）：

```sh
go run ./cmd/verify-demo --mode e1a --run-dir artifacts/e1a/<錄影那一場的證據包> --rpc https://sepolia.base.org
```

再打開區塊瀏覽器，指給對方看 tx#1（Groth16 ZK 預算預留）、tx#2（EIP-3009 結算），以及兩個 Agent 的 ERC-8004 身分（#10573、#10574）。

## 6 錄影的檢查清單

**時間**
- 測試網每個 UTC 日（台北 08:00 歸零）只夠一整輪：relayer 11／12、商店結算 5／6。兩者的計數都不分標籤，錄之前當天不要跑別的測試網交易。
- 委託到 **11/6 05:23 UTC（台北 13:23）**，開場前檢查要求至少還剩 3 小時，所以 11/6 最晚台北 10:23 開始。
- 晚於這個時間，要先重產委託：Claude Code 以 `p2-testnet opt-signer` 產生新的 `signer.json`，Aaron 停下 Signer、用 sudo 安裝（做法同 `docs/commercial-live-town.md` 6.3）。

**前一天**
1. 試錄一段：兩個螢幕的 QuickTime；展示視窗是否 1920×1080。

**當天**
2. Claude Code 唯讀檢查：時間、8402／8403、`var/present.conf` 的 CLI 檔案、relayer 與商店今天的筆數、委託剩餘的時間、核准人 24 小時的累計。
3. 兩個螢幕開始錄影。
4. `sh scripts/demo/demo-testnet.sh live <新標籤> town`（例如 `rec1`；用過的標籤：`t1`～`t4`、`fa3`、`b1`、`b2`）。依序輸入 sudo 的密碼、relayer 的密語、Signer 的密語、商店金鑰的密語。開場前檢查的結果和 `e1-evidence: start recorded …` 那一行告訴 Claude Code（配對碼不要貼）。
5. 在**簡報模式外**按「開啟展示視窗」，拖到錄影的螢幕、全螢幕；主持人頁再進入簡報模式。
6. 序幕：「全景：走一趟整條街」。
7. 三個場域照 `docs/host-manual.md` §4，**每個場域只按一次「一起開始」**，遊戲模式保持「關（A、B 一起）」；不要單獨開始任何一位。
   - 語音核准：等「錄音中」出現再念，金額與代碼一個字一個字念。
   - 每筆交付後，讓展示視窗的收據卡和「為什麼可信」停幾秒。
8. 六個場次結束後先別停，告訴 Claude Code：打包證據、`verify-demo` 驗證。
9. 在放大字體的終端機跑一次 `verify-demo --mode e1a --run-dir artifacts/e1a/<證據包> --rpc https://sepolia.base.org`，錄到 E1～E10 PASS。停錄、Ctrl-C。
10. 中途出錯就照實記下，當天不重跑（額度不夠）；改天重錄要先重產委託。

**另一次（任何時間）**
11. `make demo-local`，錄「被誘導的 Agent（模擬）」。

**11/6 的錄影（標籤 `rec1`）**：
- 六場的結果：
  - 家長的首爾攻略：語音通過，付款、交付。
  - 家長的午餐、家長的啤酒、孩子的講義：自動付款、交付。家長的啤酒是第一次在 testnet 上結算。
  - 孩子的啤酒：付款前被擋。
  - 孩子的生日蛋糕：語音又沒過（金額錯、商家與代碼對，聲紋 0.494），沒有付款。
- 證據包 `artifacts/e1a/recording-rec1`（run `e1a-20261106T021652Z-558b99`，程式 `9710730`）：E1～E10 全部 PASS，facilitator 與兩個 Agent 都列出來了。
- 畫面錄在 `artifacts/video/rec1/`（git 不收）：兩個螢幕各 45 分鐘。`screencapture` 不理會停止訊號，錄滿 45 分鐘才寫出檔案。
  - `rec1-cforce.mov`（3840×2160）：主持人頁的簡報模式（城鎮、店內、五步、核准面板、裝置的驗證視窗、卡片上的「Agent 身分 ↗」），之後切到 VS Code（終端機裡的 checker PASS，旁邊是 Claude Code 的對話）。
  - `rec1-syncmaster.mov`（1680×1050）：桌面與 Finder，沒有錄到展示視窗。
  - **兩段都有私人內容**（檔案名稱、文件縮圖、對話、瀏覽器分頁與書籤）。用到的片段只裁出 App 的範圍，用之前逐段檢查。

## 7 改用自動產生的示意動畫（Aaron 11/6：做法 A）

**為什麼改**：testnet 的現場錄影缺點太多：
- 每個 UTC 日只夠跑一整輪，錄壞了不能重來。
- 語音核准會失敗（11/6 的蛋糕）。
- 委託有時間窗口，還要 sudo 和螢幕錄製的權限。

**影片是什麼**：
- 畫面是產品本身：網頁與 Unity。由腳本自動操作，fake Agent 照真實 Agent 的劇本走，核准、付款、收據都發生在本機的測試鏈上。
- 每一段模擬的畫面都標「示意動畫：fake Agent・本機測試鏈」。
- 真實的證據另外拿：11/6 那場的 testnet 證據包，面試時現場查證（第 5 節）。

**插入的真實片段**：從 11/6 的錄影（`rec1-cforce.mov`）剪出，只裁頁面的範圍，標「Base Sepolia 實跑（11/6）」。
- 主持人頁簡報模式的書店：首爾攻略的核准面板與裝置的驗證視窗。
- 終端機上 checker 的 PASS（只裁那幾行）。
- 那場蛋糕的核准沒過，所以咖啡店只用示意動畫。
- 展示視窗沒有錄到：收據卡與「為什麼可信」上完整的「ERC-8004 #10573」不在錄影裡，示意動畫在本機鏈上也不顯示。要放這個畫面，得另外取得，例如之後在 testnet 的主持人程式上補截一張圖。

**怎麼做**：
1. 讓本機的付款流程能完全自動跑，只用測試金鑰。現在 `p2-dev.sh` 的 Signer 要在終端機輸入密語，現有的瀏覽器檢查（a2-ui、v1-ui）只自動化核准，沒有付款。這一步要先查清楚再做。
2. 一支導演腳本（放在 repo 裡，可以重跑）：
   - 啟動本機鏈、測試用的 Signer、主持人程式與 fake Agent。
   - headless Chrome 開 1920×1080 的展示視窗與主持人頁。
   - 照第 3、4 節的時間表操作：全景、三個場域各一次「一起開始」、核准，交付後在收據卡和「為什麼可信」停幾秒。
   - 連續截圖，用 ffmpeg 轉成影片片段。
3. 字卡與「示意動畫」的標示用 HTML 做，同一個 Chrome 截成畫面。
4. 用 ffmpeg 把片段、字卡與真實片段剪成 90 秒與 3 分鐘兩版。字幕另外輸出成 SRT。

**跟 Aaron 對的時間點**：
- 先做 20～30 秒的樣片（城鎮加書店），Aaron 看過再做完整版。
- 完整的片段做好後，剪兩個版本。

**不在這次的範圍**：中文配音（Aaron 另外錄）、配樂。

**Aaron 的決定（11/6，照建議）**：
- V1 旁白：Aaron 之後另外錄中文配音；先放字幕。**Aaron 11/6 看過兩個版本：「目前的影片效果已相當不錯，我並不打算進行錄音」**：不錄配音，燒進畫面的字幕就是旁白。
- V2 字幕：另外輸出 SRT，也做一版燒進畫面的。
- V3「被誘導的 Agent」：保留，它本來就是模擬。
- 書店的樣片（`artifacts/video/sample-books.mp4`）：「方向可以」。

**做好的（11/6）**：都在 `artifacts/video/`，git 不收。
- 拍攝（`scripts/video/film.mjs`）：
  - `--stage full --view present`：主持人頁的簡報模式，全景加三個場域。核准時，如果請購屬於另一位客人，先按「切換到客人 X」。
  - `--stage full --view show`：同一輪從展示視窗拍，給「為什麼可信」與收據用。
  - `--stage fooled`：被誘導的 Agent，超出預算被拒絕。
  - 每一輪都寫 `marks.json`，記下每個時間點的秒數，剪接照它裁。
  - WebGL 改用顯示卡（Metal）算：檢查工具用的 CPU 算法每秒只有 2～4 格，改了之後約 25 格。
- 剪接（`scripts/video/cut.py --cut 90|180`）：
  - 照第 3、4 節的每一行裁片段、調速度。
  - 畫面在上，下方一條黑帶放中英字幕。字幕太長時自動縮小字級。
  - 另外輸出 `.zh.srt`、`.en.srt`。
  - 字卡由程式畫：三種怕、身分憑證、誰握有什麼權力、照實說、定位句。
  - checker 那一格用 11/6 那場證據包重新跑一次的真實輸出排版，標明出處。
  - 3 分鐘版插入 11/6 真實的書店核准，從錄影裁出頁面範圍，標「Base Sepolia 實跑（11/6）」。
  - 這版 ffmpeg 沒有 drawtext 和 libass，所以文字都先畫成圖再疊上去。字幕帶用上下拼接：直接疊加會多出一列像素，H.264 不接受。
- 成品：`apa-demo-90s.mp4`（90 秒）、`apa-demo-180s.mp4`（180 秒），1920×1080、30 fps，各附兩份 SRT。

**重做一次**（11/9 Aaron：「主片用新城鎮重新拍攝」；11/10 住宅、餐車區與醫院改完後：「主片請再重拍」。每次拍到新資料夾：11/6 的素材在 `full-present2`、`full-show2`、`fooled`，成品備份為 `apa-demo-90s-1106.*`、`apa-demo-180s-1106.*`；11/9 的在 `full-present3`、`full-show3`、`fooled2`，成品備份為 `apa-demo-90s-1109.*`、`apa-demo-180s-1109.*`；11/10 的在 `full-present4`、`full-show5`、`fooled3`，成品備份為 `-1110`。11/11（Aaron：影片跟上現況）：三段都帶 `--ekyc` 重拍，客人的屬性來自 Didit 的驗證，第 2 步會寫「身分經 Didit eKYC 驗證・沙盒」；加一段 Didit 驗證頁的畫面，到自拍說明為止（自拍要真的相機與人臉，不拍）；字卡與旁白改掉「eKYC 是下一期」（`docs/commercial-ekyc.md`）。11/10 起 `film.mjs` 的全景等 20 秒：地圖 11/8 加寬到 202 格後，原本的 9 秒在餐車區之前就進書店了；序幕前半是主持人頁的西段（醫院、住宅），後半是展示視窗的餐車區到東端）：
1. 依序跑 `node scripts/video/film.mjs --stage full --view present --ekyc <Didit session> --out artifacts/video/full-present5`、`--view show --ekyc <session> --out artifacts/video/full-show6`、`--stage fooled --ekyc <session> --out artifacts/video/fooled4`，大約各 3 分鐘；`--stage ekyc --out artifacts/video/ekyc3` 拍 Didit 的沙盒驗證頁（約 1 分鐘，要網路與 `var/ekyc/didit-sandbox.key`）。
2. 再跑 `python3 -I scripts/video/cut.py --cut 90` 與 `--cut 180`。

**11/16 再重拍**（Aaron：「主片、地鐵加演、eKYC 都可以重新拍攝」：城鎮加了和店員、嚮導說話，地下鐵兩站重排並加寬）：`full-present6`、`full-show7`、`fooled5`（帶同一個 Didit session）、`ekyc4`、`transit-present2`；`cut.py` 改讀這幾個資料夾。11/11 的成品備份為 `apa-demo-90s-1111.*`、`apa-demo-180s-1111.*`，11/9 的地下鐵加演為 `apa-addon-transit-1109.*`。錄影的整套環境（本機鏈、Signer、核准用的虛擬裝置）由 `film.mjs` 自己起，不用 `/opt` 的 Signer，也不受 testnet 委派到期影響；要先停掉 demo-local（同樣用 8402）。

**11/19 主片再重拍**（Aaron：「請將主片重拍，並更新至作品集中」：街景改了，書店招牌改成 BOOK STORE、建築連成一排、南側支路、店門口的桌椅與攤車、電話亭）：`full-present7`、`full-show8`、`fooled6`（帶同一個 Didit session）；Didit 的驗證頁沒有城鎮，沿用 `ekyc4`；地下鐵加演不在這次範圍，仍是 11/16 的 `transit-present2`。11/16 的成品備份為 `apa-demo-90s-1116.*`、`apa-demo-180s-1116.*`。作品集（agent-portfolio-starter 的 `public/assets/payment-authority/`）的素材從同一次拍攝取：封面 `town.jpg` 是 `full-show8` 第 6.5 秒、8 秒預覽 `town-preview.mp4` 從第 13.75 秒起（11/11 的封面與預覽對過 `full-show5`，是同樣的位置），兩支海報取成品的第 47 秒與第 142 秒。

**加演短片（11/7，`docs/commercial-post-hotel.md` N6）**：郵局與飯店各一段，不改 90 秒與 3 分鐘版。
- 拍攝：`node scripts/video/film.mjs --stage full --view present --venues hotel,post --out artifacts/video/addon-present2`（約 1 分半）。郵局在交付失敗之後，腳本按交易卡上的「商店退款」，等到「商店已退款」再停 6 秒（11/8，`docs/commercial-refund.md`）。
- 剪接：`python3 -I scripts/video/cut.py --cut addon`。每段先放 3 秒字卡（場域與示範的保護，標「示意動畫」），再放兩句字幕。
- 成品：`apa-addon-hotel.mp4`（24.5 秒：收款人被換掉，付款前就被拒絕；孩子的早餐券照常付款）、`apa-addon-post.mp4`（31 秒，11/8 重拍：已付款、交付失敗，不再付一次；主持人按「商店退款」，錢退回客人的錢包，鏈上確認後才記下），1920×1080、30 fps，各附兩份 SRT。
- 剪接點寫在 `cut.py` 的 `HOTEL`、`POST`，是看 11/8 那次拍攝（`addon-present2`）定的；重拍之後要重新對一次。

**加演短片：地下鐵（11/9，`docs/commercial-transit.md`、`docs/commercial-atm.md`）**：Aaron 11/9：「請提交，並加演短片」。邊走邊付，不改 90 秒與 3 分鐘版。
- 拍攝：`node scripts/video/film.mjs --stage transit --out artifacts/video/transit-present`。遊戲模式走客人 A，每一步等於在地圖上點一下（Unity 的 `Walk` 指令）：走到 ATM 領測試 USDC → 下樓到商店街站 → 往車廂走，在閘門停下，第一次用裝置確認捷運委託，扣第 1 趟 → 搭到海灘站 → 出站（免費）→ 再進站，扣第 2 趟，不用再確認。每個時間點照主持人頁狀態列的字記進 `marks.json`。
- 剪接：`python3 -I scripts/video/cut.py --cut transit`。照 `marks.json` 裁，不用手讀秒數；走路的片段加快，ATM 與閘門的回答照原速。先放 3 秒字卡（「加演：地下鐵」，標「示意動畫」），再放五句字幕。
- 成品：`apa-addon-transit.mp4`（39 秒），1920×1080、30 fps，附兩份 SRT。11/16 重拍（`transit-present2`）：車站加寬、列車在中間、上月台的通道在大廳東端，走的路變長：`film.mjs` 走進車廂的點改成 x 824.5、1124.5，海灘站下車到出閘門改等 24 秒；成品 52.5 秒。走近 ATM 時，銀行行員的招呼（「要領測試幣嗎？」）也拍進去了。五句：ATM 領錢（本機鑄造、每人每天最多 20；testnet 用 Circle faucet）→ 第一次進站在裝置上確認捷運委託 → Policy 一核准就開閘、扣 0.02、鏈上結算在背景 → 海灘站出站免費 → 再進站自動扣第二趟，不用再確認。
- 虛擬驗證器是瞬間確認的，閘門上「開通捷運自動扣款…」只停 0.3 秒；剪接把那一段放慢到約三分之一速度，讓字讀得到。
- 為了拍這段，Unity 加了 `Walk` 指令（等於在地圖上點一下），也修了兩件事：點到走不到的地方不再讓客人停下（原本的路線照走）；列車行進中點地圖不會讓客人走下車。

## 8 補的三幕：對照組、安靜放行、收回委託（11/28 準備）

來源：FUTUREMODE 筆記 §0.2 的設計 3（例外管理：範圍內的安靜放行，超過才找人）與設計 6（開場放沒有閘的對照組，問題先於解法），加上簡報「Agent 失控，要馬上停」那一列。三幕先各剪成獨立短片給 Aaron 看；放進 90 秒、3 分鐘版的哪裡、要不要取代現有的開場，由 Aaron 決定。

| 幕 | 長度 | 畫面 | 中文字幕 | 標示 |
|---|---|---|---|---|
| 對照組 | 13 秒 | 兩張字卡：月底的帳單，七行「AI 助理 代付」與合計 3.24；第二張在 2.00 與一筆 0.09 旁加註 | 先看沒有這一層的話：AI 拿著一張卡，看到什麼就付什麼。月底的帳單，只有一行行「AI 助理代付」和一個總數。／其中 2.00 是書店頁面騙它買的，0.09 是孩子的 AI 買的啤酒。帳單上看不出是哪個任務、誰同意的。 | 示意：沒有授權層的對照組（不是產品畫面） |
| 安靜放行 | 24.5 秒 | 字卡「上限內，安靜放行」；三段自動付款（孩子的講義、家長的午餐、家長的啤酒）；書店的核准 | 範圍內的小額，安靜放行：孩子的講義 0.06、家長的午餐 0.08、家長的啤酒 0.09，都自動付款，沒有人被打擾。／超過 0.10 的那一筆，才走到本人面前：首爾攻略 0.50，家長在裝置上核准這一筆。 | 示意動畫 |
| 收回委託 | 19.4 秒 | 字卡「Agent 失控，要馬上停」；家長的請購等核准，家長收回委託；步驟 03 變成「本人已收回委託，不再付款」，孩子的講義照常付款 | 家長的 AI 正在等核准。家長決定不讓它再花錢：在自己的裝置上收回這份委託。／收回之後，Signer 一律拒簽這份委託，等著的這一筆不會付款。孩子的委託不受影響，講義照常付款。 | 示意動畫 |

**帳單的數字**：城鎮同一天的六筆（攻略 0.50、講義 0.06、午餐 0.08、蛋糕 0.42、兩位客人的啤酒各 0.09），加上被誘導的 Agent 要買的進階版 2.00。這一層會擋下其中兩筆：進階版（超出預算）和孩子的啤酒（未滿 18 歲）。帳單畫的是「如果都付了」，不是發生過的事。

**拍攝**（本機測試鏈，不受 testnet 的額度與委派期限影響；要先停掉 demo-local）：
1. `node scripts/video/film.mjs --stage full --view present --out artifacts/video/<資料夾>`：主片的拍法，現在多記下每位客人收據出現的時間點（`<場域>-paid-consumer-a`、`-consumer-b`），安靜放行照這些點裁。
2. `node scripts/video/film.mjs --stage revoke --out artifacts/video/<資料夾>`（約 30 秒的畫面）：書店一起開始，家長的請購等核准時，主持人用客人卡片上的「收回委託（本人裝置）」收回（簡報模式下那個按鈕不在畫面上），停到孩子的講義交付。
3. `python3 -I scripts/video/cut.py --cut scenes`：讀 `quiet-present1`、`revoke2`，輸出 `apa-scene-control`、`apa-scene-quiet`、`apa-scene-revoke` 三支，各附兩份 SRT。正式重拍時加 `--ekyc`，改 `cut.py` 讀的資料夾。

**11/28 的樣片**：`quiet-present1`、`revoke2` 沒有帶 `--ekyc`，第 2 步沒有「身分經 Didit eKYC 驗證」那句。

**為了這一幕改的畫面**：收回委託之後，交易卡與展示視窗的步驟原本還寫「超過自動範圍，等本人核准」，現在寫「本人已收回委託，不再付款」；「task 已結束」的說明多了「或本人收回了委託」。城鎮裡客人頭上的標籤仍是「超過自動範圍，等核准」，要等到核准期限過了才離店：那是 Unity 自己的字，要改就得重建 WebGL，這次沒動。
