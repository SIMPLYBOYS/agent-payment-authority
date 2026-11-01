# 宣傳影片腳本與錄影計畫

> 2026-11-01，Claude Code；11/2 依 `docs/commercial-consumer.md` 改為一般人的故事。Aaron 11/1：兩支，**90 秒宣傳版**與 **3 分鐘完整版**，從同一場 testnet 錄影剪出；**中文配音、英文字幕**。面試時當面播放，播完可以現場重新查證（第 5 節）。

## 1 原則

- **故事是一般人的**（Aaron 11/2，`docs/commercial-consumer.md`）：「讓 AI 替我花錢，我最怕三件事：被騙著亂花、個資被拿走、出了事說不清楚。」一家人：客人 A 是家長，客人 B 是未滿 18 歲的孩子。
- **只說畫面上看得到、證據包裡查得到的事。** 測試鏈就說測試鏈，test USDC 就說 test USDC，不說「完全安全」。
- 真實那場一次錄到底，影片只剪、不重演；Agent 的行為照實呈現。「被騙的 Agent」那一段是 fake 主機的模擬，畫面上一律標「模擬」，不和真實那場混剪成同一場。
- 旁白講「為什麼可以放心」，不講實作名詞。名詞只出現在字幕、字卡或完整版。

## 2 錄什麼

**真實那場**（testnet、真實 Agent、三個場域）：

| 來源 | 錄法 | 用在 |
|---|---|---|
| 展示視窗（投影螢幕，1920×1080） | QuickTime 錄整個螢幕，從開場錄到最後 | 主畫面：城鎮、店內、五步、可信度、收據 |
| 主持人頁（另一個螢幕） | QuickTime 錄整個螢幕 | 特寫：攻略的商品說明（藏著「系統通知」）、核准面板、「這間店看到」 |
| 裝置的 PIN 視窗 | 跟著它出現的那個螢幕一起錄到 | 委託確認、核准 |
| 語音核准 | QuickTime 開麥克風收音（只錄唸句子那一段的原音） | 核准那一段 |
| 區塊瀏覽器 | 場次結束後，從可信度區點開 tx#1、tx#2 | 鏈上證據 |
| checker | 終端機字放大，跑 `verify-demo`，錄到 E1～E10 PASS | 結尾 |

**被騙的 Agent（模擬）**：本機 `make demo-local`（fake Agent），數位書店選「被誘導的 Agent」。錄展示視窗：它照商品頁的「系統通知」去買 2.00 的進階版，「超出預算，已拒絕」，錢沒有動。這一段不付款、不上鏈。

配音事後另錄（安靜環境），英文字幕用第 3、4 節的英文欄。

## 3 90 秒宣傳版

| 秒 | 畫面 | 中文旁白 | English subtitle |
|---|---|---|---|
| 0–10 | 黑底字卡 → 城鎮全景，一家人在街上 | AI 已經可以替我們買東西了。可是讓 AI 替我花錢，我最怕三件事：被騙著亂花、個資被拿走、出了事說不清楚。 | AI can shop for us now. But letting it spend my money, I worry about three things: being tricked into spending, giving away my personal data, and not knowing what happened. |
| 10–20 | 字卡：AI 只能提出／本人才能同意／另一把鎖才能簽名 | 在這裡，AI 只能提出要買什麼；同意的是本人，簽名的是另一把它碰不到的鎖。 | Here, the AI can only propose. Only the person can agree, and the signature sits behind a lock the AI can't touch. |
| 20–32 | 家長走進數位書店；PIN 視窗確認委託 | 家長先在自己的手機上設好：這次最多花 1.5，超過 0.1 都要問我。 | First, the parent sets the limits on their own device: at most 1.50 this time, and anything over 0.10 comes back to them. |
| 32–46 | 主持人頁特寫：攻略說明裡的「系統通知」；切到模擬片段（標「模擬」）：被騙的 Agent 去買 2.00 進階版 →「超出預算，已拒絕」 | 書店的頁面藏了一句話，叫 AI 順便買 2 塊錢的進階版。真的 AI 沒上當；就算它被騙了，也花不出預算外的錢。 | The shop's page hides an instruction to also buy a 2.00 premium edition. The real AI ignored it — and even a fooled one can't spend beyond the budget. |
| 46–58 | 孩子在烘焙咖啡訂生日蛋糕；家長裝置核准、唸句子；「這間店看到：沒有任何個人資料」 | 孩子的 AI 想訂 0.42 的生日蛋糕，超過範圍，交給家長在裝置上核准。這間店，什麼個資都看不到。 | The child's AI wants a 0.42 birthday cake. That's over the limit, so the parent approves on their device. The shop sees no personal data at all. |
| 58–70 | 便利商店：家長的啤酒交付；孩子的卡片「不符合這間店的要求」 | 便利商店賣酒，只問一件事：滿 18 歲了嗎？孩子的 AI 想買，付款前就被擋下，錢沒有動。 | At the corner store, beer needs one fact: over 18? The child's AI tries, and it stops before any money moves. |
| 70–82 | 可信度區逐步亮起；點開區塊瀏覽器；收據卡 | 每一步都留下證據：零知識證明預留預算、鏈上結算、交付留下收據。 | Every step leaves evidence: a zero-knowledge proof reserves the budget, settlement lands on chain, delivery leaves a receipt. |
| 82–90 | 終端機 E1～E10 PASS → 結尾字卡（名字、repo） | 出了事，任何人都能自己查證。 | And if anything goes wrong, anyone can check for themselves. |

## 4 3 分鐘完整版

| 秒 | 段落 | 畫面 | 中文旁白（要點） | English subtitle（要點） |
|---|---|---|---|---|
| 0–20 | 三種怕 | 字卡；城鎮 | 讓 AI 替我花錢，最怕被騙著亂花、個資被拿走、出了事說不清楚。 | Letting AI spend for me, I fear being tricked, losing my data, and not knowing what happened. |
| 20–40 | 誰握有什麼權力 | `docs/architecture.md` 的角色圖 | AI 只能提出；替它跑的程式沒有私鑰；簽名在另一個系統帳號裡，只簽本人同意過的範圍；查帳的人只讀證據和鏈。 | The AI proposes. The program running it holds no key. Signing lives in its own OS account, within limits the person agreed to. The auditor reads only the evidence and the chain. |
| 40–95 | 數位書店：被騙著亂花 | 家長的委託 PIN；攻略報價與藏起來的「系統通知」；這間店看到「國家 台灣」；家長核准（語音）；tx#1、tx#2、收據；模擬片段：被騙的 Agent 去買 2.00 進階版被拒 | 委託設了上限；書店只知道你在哪個國家；超過範圍由本人核准；誘導文字騙不倒真實的 AI，就算騙倒了也花不出預算外的錢。孩子的講義 0.06 在範圍內，自動付款。 | A spending limit; the bookshop learns only your country; the person approves above the limit; the trap didn't fool the real AI, and a fooled one still can't overspend. The child's 0.06 notes pay on their own. |
| 95–120 | 烘焙咖啡：個資 | 「這間店看到：沒有任何個人資料」；孩子的蛋糕由家長核准；家長的午餐自動付款 | 買蛋糕不需要知道你是誰。孩子的大額購買，由家長核准。 | Buying a cake needs nothing about you. The child's bigger purchase goes to the parent. |
| 120–145 | 便利商店：個資＋安全 | 家長「已滿 18 歲」交付；孩子「未滿 18 歲」被擋下 | 同一份憑證，在這裡只說「滿不滿 18 歲」，不給生日。孩子的 AI 買酒，付款前就被擋下。 | The same credential says only "over 18 or not", never a birth date. The child's AI is stopped before paying. |
| 145–168 | 說得清楚 | 終端機 `verify-demo`；E1～E10 逐條 PASS；區塊瀏覽器 | 查帳不用相信這個系統：只讀證據、向鏈重算，十項全部通過。 | The check trusts none of this system: it reads the evidence, recomputes against the chain, and all ten pass. |
| 168–180 | 照實說與結尾 | 字卡：限制；名字 | 這是測試鏈與測試幣；被騙那一段是模擬；還沒做抗合成語音。下一步是…… | This runs on a test network with test USDC; the fooled agent is a simulation; synthetic-voice defense is not built yet. |

結尾的「下一步」由 Aaron 決定。

## 5 面試現場：播完之後

當面用電腦播完影片，再現場重新查證影片裡那一場（不需要實跑，也不會失敗）：

```sh
go run ./cmd/verify-demo --mode e1a --run-dir artifacts/e1a/<錄影那場的證據包> --rpc https://sepolia.base.org
```

接著從可信度區點開區塊瀏覽器，指給對方看 tx#1、tx#2。

## 6 錄影當天的檢查清單

**前一天以前**
1. 裝 `var/p2-opt/signer-4.json`（委派總額 1.00；`dlg-ta` 在 S5 後已用 0.25，舊的 0.30 不夠）。Signer 要停著（`pgrep -fl '/opt/apa-signer/'` 沒有輸出），在 repo 根目錄：
   ```
   sudo -u apa-signer /bin/cp -p /opt/apa-signer/data/signer.json /opt/apa-signer/data/signer.json.before-venues
   cat var/p2-opt/signer-4.json | sudo -u apa-signer /usr/bin/tee /opt/apa-signer/data/signer.json >/dev/null
   sudo shasum -a 256 /opt/apa-signer/data/signer.json
   ```
   最後一行應為 `b953c73a832ec09b6befe05bb050447245c564f035bf23e7c2a33c11dce8cbbb`。回滾：把 `signer.json.before-venues` 用同樣的方式拷回。
2. **`signer-4.json` 的委派到 11/6 05:23 UTC（台北 13:23）**。錄影晚於這個時間就要再重產一次。
3. 試錄一段：兩個螢幕的 QuickTime、麥克風收音、展示視窗是否 1920×1080。

**當天（台北 08:00 之後，UTC 的新的一天）**
1. 新標籤：`sh scripts/demo/demo-testnet.sh live rec1 town`。開場前檢查要全部通過：relayer 11 筆、每個錢包 0.67、委派還剩幾天幾小時。
2. 先開始兩個螢幕的錄影，再進簡報模式、開展示視窗。
3. 三個場域照 `docs/host-manual.md` §4。**只想重開一位客人時，不要用「一起開始」。**唸句子要等「錄音中」出現後再唸，只唸那一句。
4. 六個場次結束後先別停，告訴 Claude Code：打包證據、`verify-demo` 驗證。
5. 在錄影中的終端機跑一次 `verify-demo`（第 5 節的指令），錄到 E1～E10 PASS；再從可信度區點開 tx#1、tx#2。
6. 停錄、停 Host。

**被騙的 Agent（模擬），任何一天、真實那場之外**
1. `make demo-local`（fake Agent、本機鏈）。testnet 那場要先停掉（兩者都用 8402）。
2. 開始錄展示視窗，進簡報模式，數位書店選「被誘導的 Agent（模擬）」開始。
3. 錄到「超出預算，已拒絕」、錢沒有動；停錄、Ctrl-C。
