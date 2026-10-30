# 場域矩陣：每間店只看它需要的

> 2026-10-30，Claude Code。完成清單第 2 項（`docs/commercial-gap-review.md` §9）。
> 概念借自 vault〈CrossHub 產品架構分析〉§4.1。依 vault〈執行計畫〉§7 的紅線，repo 和畫面都不寫 CrossHub 的名字。
> **Aaron 10/30：「D1～D5 照建議方式進行」**：三個屬性；每間店的要求由 Host 的情境設定；B 未滿 18 歲、買啤酒被擋下；A、B 都是 `business`；便利商店也上 testnet，商店的每日上限一併調高。照第 4 節的切片實作。

## 1 一段話

城鎮裡三間店，向同一位客人要的身分資料各不相同：
- 資料研究室：只賣授權報告給已驗證的企業買方。
- 咖啡店：不看任何身分欄位。
- 便利商店：賣酒時要確認客人已滿 18 歲，但不知道生日。

Agent 每次請購，都由 verifier 依「這間店要什麼」發出 challenge。客人的 BBS+ 憑證只揭露這些欄位，其他一律不揭露，checker 事後可以查證。觀眾看到的是：同一份憑證，在三間店揭露三種不同的內容。

## 2 現況（`internal/credential`，C2）

- `apa.credential.v1` 固定 8 個欄位：`schema, issuer, task_id, consumer_id, c0, expiry, holder, status`。**沒有任何身分屬性。**
- 每次出示都揭露同一組 `Mandatory`（`consumer_id`、`c0` 永不揭露）。`Store.IssueChallenge` 一律用 `credential.Mandatory`（`internal/store/credential.go:197`）。
- 憑證在 task 開始時發出（一個 task 一份），issuer、holder、verifier 在同一個程序裡（C2 §5，照實標示）。

## 3 做法

### 3.1 憑證 v2：加三個身分屬性

`apa.credential.v2`，在 v1 的 8 個欄位後面依序加上：

| 欄位 | 值 | 誰會要 |
|---|---|---|
| `buyer_type` | `business` 或 `individual` | 資料研究室 |
| `country` | ISO 3166-1 alpha-2，例如 `TW` | 資料研究室（跨境：台灣買方、韓國商店） |
| `age_over_18` | `true` 或 `false` | 便利商店（酒類） |

- 屬性值由 issuer 依每位客人的設定發出（`--credential-dir` 裡新增 `subjects.json`，和 `holders.json` 一樣由 `apa-issuer init` 產生）。
- **照實寫**：`age_over_18` 是 issuer 簽好的布林值，出示時選擇性揭露它。這**不是**範圍證明：憑證裡根本沒有生日，所以也沒有可以洩漏的生日。範圍證明另做。
- v1 照舊驗證，以前的證據包仍然有效。新的 task 一律發 v2。

### 3.2 每間店的要求

- 在情境目錄的每個 scene（`data`、`cafe`、`mart`）加上 `discloses`，列出這間店要求的屬性。verifier 發 challenge 時，用 `Mandatory ∪ discloses`。
- 請購時檢查揭露出來的值：例如 `age_over_18=false` 卻要買酒，就 Deny，理由碼是新的 `CREDENTIAL_CLAIM_UNMET`。這一步放在既有的 Policy 規則之前，和其他憑證失敗一樣。
- 事件 `CREDENTIAL_VERIFIED` 帶出揭露的欄位名和值（不含 `consumer_id`、`c0`）。主持人頁和展示視窗據此顯示「這間店看到了什麼」。

### 3.3 便利商店情境（真實 Agent）

城鎮已有便利商店的畫面，但 live 目錄裡沒有它。要新增：
- 商品：例如「團隊聚會的啤酒 6 罐」，金額落在 Allow 範圍內，要求 `age_over_18`。
- 兩位客人的設定值決定結果。D3 是要不要做一個「被擋下」的例子。

### 3.4 checker 與證據

- E 系列裡管憑證的那一項加一條：揭露的欄位集合**正好等於**這一個 scene 的要求，沒有多揭露。
- 報告的 `not_evaluated` 照實列出：`age_over_18` 不是範圍證明；issuer、holder、verifier 在同一個程序。

### 3.5 畫面

- 主持人頁的請購卡片：「這間店看到：企業買方、TW」「這間店看到：無身分欄位」「這間店看到：已滿 18 歲」。
- 展示視窗的第 2 步（選購）帶同一句話。可信度區（完成清單第 3 項）再加上「最少揭露，由 checker 查證」。

## 4 規模（含測試）

| 切片 | 內容 | 約 |
|---|---|---|
| V1 | 憑證 v2 的欄位、格式、issuer 的 `subjects.json`、v1 照舊 | 250 行 |
| V2 | scene 的 `discloses`、challenge、揭露值的檢查與 `CREDENTIAL_CLAIM_UNMET`、事件內容 | 300 行 |
| V3 | 便利商店的 live 目錄與 Unity 房間的商品 | 150 行 |
| V4 | checker 加一條、`not_evaluated` | 100 行 |
| V5 | 主持人頁與展示視窗的那一句 | 100 行 |
| V6 | 本機彩排；testnet 併入完成清單第 5 項 | — |

## 5 要 Aaron 決定的事

| # | 問題 | 選項 | 建議 |
|---|---|---|---|
| D1 | 三個屬性夠嗎 | (a) `buyer_type`、`country`、`age_over_18`；(b) 再加其他（例如 `kyc_level`） | (a)：三間店各用到一個以上，剛好說清楚 |
| D2 | 每間店的要求由誰決定 | (a) Host 的情境設定（`discloses`），畫面上照實寫「由主持人設定」；(b) 商店在 402 報價裡宣告，Policy 依店的類別設上限，要多了就擋 | (a)。(b) 能演出「商店想多要資料被擋下」，但要改報價契約，規模約多一倍，列為之後 |
| D3 | 要不要演一個被擋下的例子 | (a) 要：B 的 `age_over_18=false`，B 的 Agent 想買啤酒被 Deny，錢沒有動；(b) 不要，兩位都滿 18 歲 | (a)：對投資人最直觀，「Agent 不能替不符資格的人買」。B 在其他兩間店照常 |
| D4 | 兩位客人的 `buyer_type` | (a) A、B 都是 `business`；(b) A `business`、B `individual` | (a)：兩位在資料研究室都買得到，不用改現有情境 |
| D5 | testnet 上便利商店那一筆 | (a) 和另外兩間一起跑：一場變成三個場域、六位次，relayer 和商店的每日上限要重算（商店一天 4 筆會不夠）；(b) 便利商店只跑本機，testnet 仍跑兩個場域 | 先問 D3。如果 B 被擋下，便利商店只多 1 筆真付款，但仍超過商店一天 4 筆。建議 (a)，並在設計時一起提高商店的每日上限設定（要改 `payments.json`，由你決定） |

## 6 實作（10/30，分支 `commercial-complete`）

| 切片 | commit 內容 | 狀態 |
|---|---|---|
| V1 | `apa.credential.v2`：v1 的 8 個欄位後加 `buyer_type`、`country`、`age_over_18`。憑證目錄有 `subjects.json` 才發 v2（名單上沒有的客人不發，不會 v1、v2 混著發）；沒有就照舊發 v1。`apa-issuer subjects` 寫這個檔 | 完成 |
| V2 | 商品帶 `Requires`（`internal/merchant`）：資料研究室兩項要求 `buyer_type=business` 並揭露 `country`；便利商店新增「啤酒 6 罐」0.09，要求 `age_over_18=true`；咖啡店不要求。challenge 依商品要求，揭露 v2 憑證的對應屬性；驗證有效但不符要求時，紀錄仍是 `valid`、detail 記 `CLAIM_UNMET`，Policy 以 `CREDENTIAL_CLAIM_UNMET` Deny。v1 憑證遇到有要求的商品一律 `CLAIM_UNMET`。揭露的屬性放在 `CREDENTIAL_VERIFIED` 事件的 `claims`；**不改 Store 的資料表**（舊資料庫不用升級） | 完成 |
| V3 | 城鎮 live 目錄加 `town-mart-a`、`town-mart-b`（A、B 都買啤酒）；fake 的 ux3 目錄加 `mart-beer-a`、`-b`；`present.sh` 每次開場寫入兩位客人的屬性（都是台灣的企業買方；B 未滿 18 歲） | 完成；Unity 的便利商店房間沿用 ux3 的 |
| V4 | checker e1a.3 的 E2：v2 憑證揭露的屬性必須正好等於商品的要求（最少揭露），而且只有在不符要求時才是 `CLAIM_UNMET`；`CLAIM_UNMET` 的請購不得交付。v1 紀錄（E1-a、S4、S5）照舊：S4 的證據包在 e1a.3 下 E2 仍是 PASS | 完成 |
| V5 | 主持人頁的請購卡片「這間店看到：企業買方、國家 台灣」；展示視窗第 2 步同一句；`CREDENTIAL_CLAIM_UNMET` 白話為「不符合這間店的要求」。Unity 字幕寫「憑證不符商店」（字型子集裡沒有「合、間、要、求」） | 完成；Unity 要重建才會顯示 |
| testnet | 開場前檢查以三個場域計：relayer 11 筆（登記 6、預留 5）、每個錢包 0.67；`demo-testnet.sh live L town` 建 host 時 `e1a-host --shop-cap 6`（一場結算 5 筆） | 完成 |

**還沒做／要注意**
- 本機彩排（完成清單第 5 項）：A 的啤酒付款交付，B 的啤酒在付款前被擋下；兩位在資料研究室都看到「企業買方、國家 台灣」。
- Unity 要重建（`scripts/unity/unity.sh webgl`），字幕才會出現「憑證不符商店」。
- **testnet 的 `dlg-ta` 總額 0.30 不夠正式錄影**（E1-a 0.09＋S5 0.08＋錄影那場 A 的午餐 0.08、啤酒 0.09＝0.34）。Aaron 10/30：「請調整上限」。`opt-signer` 的 `dlgTotal` 改為 1.00（每筆 0.10 不變；`dlg-t402` 不變）；以 `var/p2-opt/signer.base-3.json` 產生 `var/p2-opt/signer-4.json`（SHA-256 `b953c73a832ec09b6befe05bb050447245c564f035bf23e7c2a33c11dce8cbbb`），和 `signer-3.json` 只差兩份委派的總額與有效期（到 11/6 05:23 UTC）。安裝由 Aaron 執行，正式錄影前裝上即可。
- `age_over_18` 是 issuer 簽的布林值，不是範圍證明；issuer、holder、verifier 仍在同一個程序（C2 §5）。
