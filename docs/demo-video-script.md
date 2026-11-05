# 宣傳影片腳本與錄影計畫

> 2026-11-01 起草；11/2 依 `docs/story.md`（五幕、一家人、三種怕）重寫。Aaron 11/1：兩支，**90 秒宣傳版**與 **3 分鐘完整版**，從同一場錄影剪出；**中文配音、英文字幕**。
> Aaron 11/2：錄影版不需要真的上鏈（本機鏈）；面試現場用 testnet 的證據包重新查證。
> **Aaron 11/5：改在 Base Sepolia 測試網錄**（路線圖 C）。理由：A 做出來的 Agent 鏈上身分（ERC-8004）只在測試網上顯示；同一場的證據包就是面試現場查證用的那一份；B2 已在測試網跑過一整輪（`docs/commercial-rehearsal.md` 第 4 節）。

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
| 10–22 | 一、設定 | 字卡：「家長已完成身分驗證，拿到憑證」（小字：eKYC 是下一期；這裡由測試發行者直接發）；家長在裝置上確認委託 | AI 只能提出要買什麼。家長先在自己的裝置上設好：這次最多花 1.5，超過 0.1 都要問我。 | The AI can only propose. The parent sets the limits on their own device: at most 1.50, and anything over 0.10 comes back to them. |
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
| 40–60 | 一、設定 | 字卡：身分憑證（eKYC 下一期）；家長、孩子的委託 PIN | 憑證證明「我是誰」；委託寫下能花多少、超過多少要問。孩子的委託，由家長在自己的裝置上確認。 | The credential says who I am; the mandate says how much, and when to ask. The child's mandate is confirmed on the parent's device. |
| 60–105 | 二、複合式書店：被騙著亂花 | 攻略報價（x402）與藏起來的「系統通知」；這間店看到「國家 台灣」；家長在裝置上核准、念出金額與代碼；tx#1、tx#2、收據；模擬：被騙的 Agent 被拒 | 書店只知道你在哪個國家；超過範圍由本人核准，還要念出這一筆的金額；誘導文字騙不倒真實的 AI，就算騙倒了也花不出預算外的錢。孩子的講義 0.06 在範圍內，自動付款。 | The bookshop learns only your country; the person approves above the limit and reads this payment's amount aloud; the trap didn't fool the real AI, and a fooled one still can't overspend. The child's 0.06 notes pay on their own. |
| 105–130 | 三、烘焙咖啡：個資 | 「這間店看到：沒有任何個人資料」；孩子的蛋糕由家長核准；家長的午餐自動付款 | 買蛋糕不需要知道你是誰。孩子的大額購買，由家長核准。 | Buying a cake needs nothing about you. The child's bigger purchase goes to the parent. |
| 130–150 | 四、便利商店：個資＋安全 | 家長「已滿 18 歲」交付；孩子「未滿 18 歲」被擋下 | 同一份憑證，在這裡只說「滿不滿 18 歲」，不給生日。孩子的 AI 買酒，付款前就被擋下。 | The same credential says only "over 18 or not", never a birth date. The child's AI is stopped before paying. |
| 150–168 | 終幕：說得清楚 | 收據卡上的 Agent 身分與 Basescan；終端機 `verify-demo`；E1～E10 逐條 PASS，報告列出 facilitator 與兩個 Agent；字卡：「也會在付款前擋下：商家不在名單、委託或授權過期、重放用過的授權」 | 查帳不用相信這個系統：只讀證據、向鏈重算，十項全部通過，連付款經過誰結算、是哪兩個 AI 買的都列出來。影片演了兩種被擋下的情況；商家不在名單、授權過期、重放用過的授權，也都會在付款前被擋下，都有自動測試。 | The check trusts none of this system: it reads the evidence, recomputes against the chain, and all ten pass — down to who settled each payment and which two AIs bought. Beyond the two refusals shown, an unlisted shop, an expired authorization and a replayed one are all stopped before paying, and each is covered by tests. |
| 168–180 | 照實說與結尾 | 字卡：限制；定位句（`docs/positioning.md` §1）；名字 | x402 處理付款；這一層證明這筆錢是為了哪個任務、經過誰的同意才付出去的。這段在 Base Sepolia 測試網、用測試幣錄；被騙那一段是本機的模擬；身分憑證由測試發行者發，eKYC 是下一期；AI 的鏈上身分由平台代為註冊。下一步是…… | x402 moves the money; this layer proves who allowed it, for what, and within which limits. Recorded on the Base Sepolia testnet with test coins; the fooled agent is a local simulation; the credential comes from a test issuer, eKYC is next; the AIs' on-chain identities were registered by the platform. |

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
