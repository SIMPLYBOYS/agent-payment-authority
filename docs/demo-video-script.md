# 宣傳影片腳本與錄影計畫

> 2026-11-01，Claude Code。Aaron 11/1：兩支，**90 秒宣傳版**與 **3 分鐘完整版**，從同一場 testnet 錄影剪出；**中文配音、英文字幕**。面試時當面播放，播完可以現場重新查證（第 5 節）。

## 1 原則

- **只說畫面上看得到、證據包裡查得到的事。** 測試鏈就說測試鏈，test USDC 就說 test USDC，不說「完全安全」。
- 一場實跑錄到底，影片只剪、不重演。Agent 的行為照實呈現。
- 旁白講「為什麼可信」，不講實作名詞。名詞只出現在字幕、字卡或完整版。

## 2 錄什麼（同一場、三個場域）

| 來源 | 錄法 | 用在 |
|---|---|---|
| 展示視窗（投影螢幕，1920×1080） | QuickTime 錄整個螢幕，從開場錄到最後 | 兩支的主畫面：城鎮、店內、五步、可信度、收據 |
| 主持人頁（另一個螢幕） | QuickTime 錄整個螢幕 | 特寫：KRW 報價、核准面板、「這間店看到」、委託條款 |
| 裝置的 PIN 視窗 | 跟著它出現的那個螢幕一起錄到 | 委託確認、核准 |
| 語音核准 | QuickTime 開麥克風收音（只錄唸句子那一段的原音） | 核准那一段 |
| 區塊瀏覽器 | 場次結束後，從可信度區點開 tx#1、tx#2 | 鏈上證據 |
| checker | 終端機字放大，跑 `verify-demo`，錄到 E1～E10 PASS | 結尾 |

配音事後另錄（安靜環境），英文字幕用第 3、4 節的英文欄。

## 3 90 秒宣傳版

| 秒 | 畫面 | 中文旁白 | English subtitle |
|---|---|---|---|
| 0–8 | 黑底字卡 →  城鎮全景，客人在走 | AI Agent 已經開始替人買東西。問題是：誰授權了這筆錢？ | AI agents are starting to buy things for us. So who authorized the money? |
| 8–16 | 字卡：Agent → 提出／本人 → 授權／Signer → 簽名 | 在這個系統裡，Agent 只能提出付款，不能自己批准，也不能自己簽名。 | Here, an agent can propose a payment, but never approve it, and never sign it. |
| 16–28 | 客人走進資料研究室；PIN 視窗 | 客人先用自己的裝置確認委託：預算 1.5，超過 0.1 就要問本人。 | First, the customer confirms the mandate on their own device: a 1.50 budget, and anything over 0.10 comes back to them. |
| 28–40 | 店內，Agent 選好商品；主持人頁特寫 700 KRW；第 2 步「這間店看到：企業買方、國家 台灣」 | 台灣的 Agent 向韓國的資料商店買一份市場簡報。商店只看到它需要的：這是一位台灣的企業買方。 | A Taiwanese agent buys a market brief from a Korean data shop. The shop learns only what it needs: a business buyer from Taiwan. |
| 40–54 | 核准面板：唸句子、輸入 PIN | 0.5 超過了自動範圍。本人用裝置核准，唸出一句綁定商家和金額的一次性句子。 | 0.50 is over the limit. The person approves on their device, reading a one-time phrase bound to the merchant and the amount. |
| 54–66 | 便利商店：A 的啤酒交付；B 的卡片「不符合這間店的要求」 | 便利商店賣酒，只問一件事：滿 18 歲了嗎？未滿的客人，付款前就被擋下，錢沒有動。 | At the corner store, beer needs one fact: over 18? For a customer who isn't, it stops before any money moves. |
| 66–80 | 可信度區逐步亮起；點開區塊瀏覽器；收據卡 | 每一步都留下證據：零知識證明預留預算、鏈上結算、交付留下收據。 | Every step leaves evidence: a zero-knowledge proof reserves the budget, settlement lands on chain, delivery leaves a receipt. |
| 80–90 | 終端機 E1～E10 PASS → 結尾字卡（名字、repo） | 而且，任何人都能自己查證。 | And anyone can check it for themselves. |

## 4 3 分鐘完整版

| 秒 | 段落 | 畫面 | 中文旁白（要點） | English subtitle（要點） |
|---|---|---|---|---|
| 0–20 | 問題 | 字卡；城鎮 | Agent 替人付錢時，錢包只知道花多少，不知道為了哪個任務、誰同意的。 | When an agent pays, the wallet knows how much, not for which task or with whose consent. |
| 20–45 | 誰握有什麼權力 | `docs/architecture.md` 的角色圖 | Agent 只能提出；Host 沒有私鑰；Signer 在另一個系統帳號裡，只簽本人確認過的委託；checker 只讀證據和鏈。 | The agent proposes. The host holds no key. The signer, in its own OS account, signs only within a mandate the person confirmed. The checker reads only the evidence and the chain. |
| 45–95 | 資料研究室 | 委託 PIN；選購；700 KRW；這間店看到；A 的核准（語音）；可信度 tx#1、tx#2；收據 | 委託；跨境報價；只揭露企業身分與國家；超過範圍時本人核准；ZK 預留、鏈上結算、收據。B 的 0.06 在範圍內自動付款。 | Mandate; a cross-border quote; only business status and country disclosed; the person approves above the limit; ZK reservation, on-chain settlement, a receipt. B's 0.06 is within the limit and pays on its own. |
| 95–115 | 街角烘焙咖啡 | 「這間店看到：沒有任何個人資料」；B 的 0.42 核准 | 買咖啡不需要知道你是誰：這間店什麼個資都看不到。 | Buying coffee needs nothing about you: this shop sees no personal data at all. |
| 115–140 | 便利商店 | A、B 都想買啤酒；「已滿 18 歲」／「未滿 18 歲」；B 被擋下 | 同一份憑證，在這裡只揭露「是否已滿 18 歲」，不揭露生日。B 未滿，付款前就被擋下。 | The same credential reveals only "over 18", never a birth date. B isn't, so it stops before payment. |
| 140–165 | 查證 | 終端機 `verify-demo`；E1～E10 逐條 PASS；區塊瀏覽器 | checker 不用相信這個系統：它只讀證據包，向鏈重算，十項全部通過。 | The checker trusts none of this system: it reads the evidence, recomputes against the chain, and all ten checks pass. |
| 165–180 | 照實說與結尾 | 字卡：限制；名字 | 這是測試鏈與測試幣；兩位客人其實是同一個人示範；還沒做抗合成語音。下一步是…… | This runs on a test network with test USDC; both customers are played by one person; synthetic-voice defense is not built yet. |

結尾的「下一步」由 Aaron 決定（例如：真實商店、獨立的 issuer 與 verifier、同時付款）。

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
