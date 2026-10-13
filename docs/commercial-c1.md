# C1 BBS+ 憑證選型 spike：候選、結論與 C2 需要的決定

> 2026-10-13～14，Claude Code，依 vault〈Claude-Code-C1-BBS憑證選型Spike派工-20261013〉（Aaron：「再開始 C1」）。分支 `commercial-c1`，基準 `main` `74cdc55`。程式在 `spikes/bbs/`（獨立 module，主 `go.mod` 不變）。
> **狀態：spike 完成，Claude Code 獨立上下文 review pass（第二次，第 9 節）。第一版判定 not pass（must-fix：揭露集合沒有綁到 proof），已修正並在同一提交重跑全部證據（第 8 節）。C1-D1～D4 待 Aaron 決定。這只證明格式與函式庫可用，不是憑證參與 Policy 的證據（那是 C2）。C2 未開始。**

## 1 結論（先講）

- **找不到仍在維護、且實作 IETF BBS 草案的 Go 函式庫。** W3C Data Integrity BBS cryptosuite（`bbs-2023`）用的是 IETF BBS 草案。
- **採用 `github.com/IBM/idemix` v0.3.1 的 `bbs` 套件**：2026-09-09 發布，Apache-2.0，依賴打了 tag 的 `IBM/mathlib` v0.3.2。
  - 它是**舊版 BBS+**（`bbs12381g2pub` 一系，BLS12-381，公鑰在 G2），和 trustbloc、aries 同源。
  - 但它補強了三處：先複製再反轉 bitvector（不改寫傳入的 proof）、訊息數上限 1024、`VerifyProof` 內 recover panic。
  - 第一版用的 trustbloc v1.0.4 這三處都沒有。
- 以它做的 spike（`6090587`）證明了這些能力，正反測試 38 項通過，反向 11／11：
  - 發證、衍生 proof；
  - **揭露的欄位正好等於要求的欄位**：proof 聲稱的揭露索引與訊息數，必須和要求一致；
  - proof 綁定 verifier、challenge 與請購 intent；
  - 持有人綁定、期限、撤銷，以及由 verifier 發出、只能用一次的 challenge；
  - 嚴格的欄位格式；每種拒絕都是固定代碼，結果逐欄位說明。
- **它不是 `bbs-2023`。** 要沿用舊版 BBS+，還是改走 IETF BBS，是 C2 的契約決定，要由 Aaron 決定（第 6 節）。
- 本片沒有改用 SD-JWT，也沒有把任何東西描述成 `bbs-2023` 的等價替代。

## 2 候選（2026-10-13 查證；以 proxy.golang.org、pkg.go.dev 與模組原始碼為準）

| 候選 | 版本／日期 | 授權 | 方案 | 判定 |
|---|---|---|---|---|
| `github.com/IBM/idemix` 的 `bbs` | v0.3.1，2026-09-09 | Apache-2.0 | 舊版 BBS+（`bbs12381g2pub` 一系），有上述三處補強；`bbs` 套件只 import `IBM/mathlib`、`x/crypto` 的 `blake2b` 與 `hkdf`，以及標準庫，所以編譯進來的程式不包含 idemix 其他部分（第二次 review 更正） | **採用**（第二版）：仍在維護，四者之中最新，三處補強 |
| `github.com/trustbloc/bbs-signature-go` | v1.0.4，2026-01-29 | Apache-2.0 | 舊版 BBS+（DST `BLS12381G1_XMD:BLAKE2B_SSWU_RO_BBS+_SIGNATURES:1_0_0`；`DeriveProof` 只有 nonce，沒有 header／presentation header）；依賴未打 tag 的 mathlib pseudo-version | 第一版採用；第二版改掉：`VerifyProof` 會改寫傳入的 proof，格式錯誤的 proof 會 panic，聲稱的訊息數沒有上限 |
| `github.com/hyperledger/aries-bbs-go` | pseudo-version，2024-05-28 | Apache-2.0 | 同一系（DST 相同） | 不採用：較舊，沒有 tag |
| `github.com/cloudflare/circl/bbs` | 只在 v1.3.4／v1.3.5（2023-10-16，依 pkg.go.dev） | BSD-3-Clause | IETF BBS 的 API 形狀（有 header、ph、disclosed indexes），引用 draft-03 | **不採用**：最新的 v1.6.5 沒有這個套件；draft-03 已過時；找不到移除的說明 |
| `github.com/bronlabs/bron-crypto` | v0.1.0 | Apache-2.0 | 只有 BLS 與 hash-to-curve，沒有 BBS | 不適用 |

- W3C `bbs-2023` 的實作報告列出 Digital Bazaar、Grotto Networking、Netis、Procivis、SpruceID、Trential 六家，但沒有列出語言，無法確認其中有沒有 Go 實作。
- 「aries 是 trustbloc 的上游」只依兩者程式碼相同推斷，沒有找到一手說明。
- 「沒有其他仍在維護的 Go IETF BBS」是依上述搜尋與查證所得，不是窮盡證明。

## 3 spike 做了什麼（`spikes/bbs/`，`6090587`）

- `credential.go`：
  - **欄位**：8 個固定順序的欄位 `schema, issuer, task_id, consumer_id, c0, expiry, holder, status`，每個訊息是 `名稱=值`。
  - **必要揭露欄位寫死在程式裡**：`schema, issuer, task_id, expiry, holder, status`，不交給設定。
  - **發證**：`Issuer.Issue`。發證時就檢查格式：expiry 是不帶 `+`、沒有前導零的十進位數；holder 是 `ed25519:` 加 64 個小寫 hex；status 是正規的十進位數。
  - **出示**：`Present`。
    - 只揭露要求的欄位，未知或重複的欄位會被拒絕；
    - nonce 是 SHA-256，涵蓋版本字串、verifier、challenge、intent，每段加 4 bytes 長度前綴；
    - 持有人用 Ed25519 簽 `nonce ‖ SHA-256(proof)`。
  - **驗證**：`Verifier.Verify`，依序檢查：
    1. verifier 是不是自己；
    2. challenge 是不是自己發出且還沒用過（加鎖；通過才算用過，失敗可以重試）；
    3. 要求的欄位是否合法；揭露的欄位是否正好等於要求的；必要欄位是否都在；
    4. **proof 聲稱的訊息數必須是 8，揭露索引必須正好等於要求的**（用函式庫的 `ParsePoKPayload` 讀取）；
    5. 以本 verifier 自己算出的 nonce 驗 proof（傳複本給函式庫）；
    6. schema、issuer、holder 的格式與簽章、expiry 的格式與期限、status 的格式與撤銷。
  - 每種拒絕都是固定代碼；通過時回傳逐欄位的 `Result`。
- `credential_test.go`：
  - 正向一條；
  - 負例 30 項，包括：
    - review 的攻擊情境：只揭露部分欄位的 proof、偽造 holder 與 status；只揭露 schema；
    - 聲稱 65535 則訊息、長度欄位越界、過短的 proof：都在 2 秒內回 `PROOF_INVALID`，沒有 panic；
    - 不合格式的 expiry、holder、status：用繞過格式檢查的簽章產生；
    - 從沒發出過的 challenge；要求揭露未知或重複的欄位；
    - 以及第一版就有的各項。
  - 發證拒絕不合格式的欄位；重播被拒、失敗後可以重試；出示拒絕不合法的要求；nonce 不會因 NUL 相撞；兩次 proof 位元組不同；
  - 斷言函式庫不改寫傳入的 proof；
  - 效能 benchmark。
- `reverse.py`：在拋棄式複本裡拿掉一項驗證，要求指定的測試以預期的斷言失敗；建置錯誤不算抓到。共 11 項。
- 只用合成資料與固定 seed 的測試金鑰；沒有連到外部的 issuer、wallet、RPC 或驗證器，也沒有收集個資。

## 4 證據（`artifacts/s2/`；git 忽略，只存在這台機器上）

| 項目 | 基準 | 結果 | 檔案 |
|---|---|---|---|
| `go test -count=1 -race -v ./...`（以 `rtk proxy` 取得完整輸出） | `6090587` | 38 項 PASS（8 個頂層、30 個子測試）、0 FAIL，exit 0 | `c1-6090587/test-race.txt` |
| benchmark（本機 amd64） | `6090587` | Issue 1.7 ms、Derive 3.8 ms、Verify 3.4 ms／次（8 個欄位、揭露 6 個） | `c1-6090587/bench.txt` |
| 反向 11 項（全部在同一個提交） | `6090587` | 11／11 預期斷言 | `c1-6090587/reverse.txt`、`reverse/` |
| 實際編譯進 spike 的模組與授權 | `6090587` | idemix v0.3.1、mathlib v0.3.2、gnark-crypto v0.21.0、fabric-amcl：Apache-2.0；bitset、x/crypto v0.57.0、x/sys v0.48.0：BSD 類 | `c1-6090587/modules.txt` |
| 第一版的證據（trustbloc；保留） | `ebb6956`、`6f7b37e` | race 24 PASS；反向第一次 6／7（holder binding 突變後建置失敗），補跑 1／1 | `c1-ebb6956/`、`c1-6f7b37e/` |

重跑：
- `cd spikes/bbs && GOFLAGS=-mod=readonly GOPROXY=off GOCACHE=$TMPDIR/gocache go test -race ./...`
- `python3 -I spikes/bbs/reverse.py <out> [name]`（GOCACHE 沒有設定時，自動使用暫存目錄）

## 5 發現

1. **舊版 BBS+ 的 `VerifyProof` 是從 proof 讀取揭露索引與訊息數，只檢查「揭露數不超過訊息數」**；trustbloc 與 idemix 都一樣。
   - 驗證端若只依要求重建訊息，就會被只揭露部分欄位的 proof 騙過：持有人綁定、撤銷、期限與 task 都能偽造（第一版的 must-fix，第 8 節）。
   - C2 不論用哪個庫，都要先把 proof 標頭和要求比對。
2. trustbloc v1.0.4 的 `parsePoKPayload` 會**就地反轉 bitvector**，所以會改寫傳入的 proof；idemix v0.3.1 先複製再反轉。spike 仍傳複本給函式庫，並以測試斷言不會被改寫。
3. **持有人綁定**：spike 的做法是揭露 holder 公鑰，所以同一張憑證的多次出示可以用 holder 欄位串起來；proof 位元組本身每次都不同。
   - trustbloc 沒有 blind signing。**idemix v0.3.1 有**：同一 module 的 `bccsp/schemes/aries/blind_sign.go` 提供 `BlindMessages`、`BlindSign`、`UnblindSign`、`VerifyBlinding`，`bbs` 的 `SignWithKeyB` 也支援。
   - 所以「持有人秘密不揭露」的綁定方式（link secret）現在就可行，但要使用 `aries` scheme 套件，不只是 `bbs`；spike 沒有實作它（第二次 review 更正）。
4. **沒有 header／presentation header**：情境綁定全部放在加了長度前綴的 nonce。
5. **撤銷**：揭露 status 索引，由 verifier 持有已撤銷的集合；清單的來源與更新留給 C2。
6. **challenge**：由 verifier 發出並加鎖，只存在記憶體裡（`ponytail:` 註記），持久化留給 C2。
7. gnark-crypto v0.21.0 在依賴裡；Z1 預期也會用到，屆時要對齊版本。

## 6 C2 需要 Aaron 決定的事

| # | 決定 | 選項 | 建議 |
|---|---|---|---|
| C1-D1 | 憑證格式與函式庫 | (a) 沿用舊版 BBS+，用 idemix v0.3.1 的 `bbs`，文件與畫面標明「BBS+（`bbs12381g2pub` 一系），不是 W3C `bbs-2023`」；(b) 自行在 gnark-crypto 上實作 IETF BBS，對照官方測試向量；(c) 用 cgo 接 Rust 的 IETF BBS 實作 | **(a)**：維護中、有 tag、授權清楚、已補強；能力足夠 demo 使用。(b) 是自行實作密碼學，風險與工作量都高；(c) 增加跨語言建置與部署成本 |
| C1-D2 | 持有人綁定 | (a) 揭露 holder 公鑰，由持有人簽 nonce 與 proof（spike 的做法，可被串連）；(b) 用 idemix `aries` scheme 的 blind signing，以不揭露的持有人秘密綁定（同一 module 已有，需引入 `aries` 套件並另做 spike） | **(a)**：demo 只有單一 verifier，(a) 已有正反測試；在畫面與文件上標明可被串連。要求不可串連時再做 (b) |
| C1-D3 | 憑證外層 | spike 使用原始 `名稱=值` 訊息，不是 JSON-LD 的 VC Data Integrity | 由 C2 訂 `apa` 自己的正規化外層：版本、欄位順序、編碼，並固定 spike 已採用的嚴格格式；經 review 後才接 Policy |
| C1-D4 | 撤銷與 challenge 保存 | 狀態清單的來源、challenge 存放處 | C2 設計時一併決定 |

**Aaron 的決定（10/14）**：「開工前要你決定的四件事，都允許依照建議的方式下去進行」。
- C1-D1：採 (a)，用 idemix v0.3.1 的舊版 BBS+，文件與畫面標明不是 `bbs-2023`；
- C1-D2：採 (a)，揭露 holder 公鑰並由持有人簽名，標明可被串連；
- C1-D3：由 C2 訂 `apa` 的正規化外層，經 review 後才接 Policy；
- C1-D4：撤銷與 challenge 保存在 C2 設計時決定。

## 7 不在本片

- 沒有接 Policy、Store、Host、Signer、A2 或付款；
- 沒有改 `apa.pay.v1`、M1、正式授權條件或持有人信任根；
- 沒有發正式 consumer credential，主 `go.mod` 沒有變；
- 不標 C2 完成，也不開始 C2。

## 8 第一次獨立上下文 review 與修正（10/13～14）

review 由一個 Claude Code 獨立上下文 subagent 執行，範圍是 `74cdc55..e05de0f`，判定 **not pass**。

- **must-fix**：揭露集合沒有綁到 proof。reviewer 在複本上重現兩個攻擊：
  - 只揭露 `{schema, issuer, task_id, expiry}` 的 proof，配上偽造的 holder（竊取者的金鑰）與 status `999`，**通過驗證**；
  - 只揭露 `{schema}`，task 與期限任意填寫，也通過。
- **should-fix**：
  - 格式錯誤的 proof 會讓 trustbloc panic；
  - proof 聲稱 65535 則訊息時，耗費約 12 秒 CPU；
  - status 不在設定的必要欄位時，撤銷檢查被跳過；
  - idemix 被我誤寫成「同一份程式碼」，而且我說 trustbloc 是「最新」。實際上 idemix 比 trustbloc 新，而且已經補強。
- **must-fix（文件）**：第一版第 1、3、5 節宣稱「只揭露指定欄位」「持有人綁定、期限、撤銷」「換掉任何一項 proof 都會失敗」已被證明。因為上述漏洞，這些陳述在第一版**不成立**。
- **note**：
  - 要求揭露未知欄位會 panic；
  - nonce 遇到 NUL 時有歧義；
  - challenge 沒有檢查是否為自己發出，`used` 也沒有加鎖；
  - 格式寬鬆（`+`、前導零、`ed25519:` 前綴可省略）；
  - `reverse.py` 沒有設定 GOCACHE；
  - `artifacts/` 被 git 忽略。

修正（`6090587`），細節見第 3 節：
- 揭露集合與訊息數綁定；
- 改用 idemix v0.3.1；
- 必要欄位寫死在程式裡；
- 嚴格格式；
- 未知或重複的欄位改回傳固定代碼；
- 長度前綴的 nonce；
- 由 verifier 發出、加鎖、只能用一次的 challenge；
- 新增上述負例與 4 項反向（揭露集合、必要欄位、expiry 格式、status 格式）。

全部證據都在 `6090587` 重跑（第 4 節）。第一版的結果與證據保留，不併入第二版。

## 9 第二次獨立上下文 review（10/14；只看 `e05de0f..270810e`）

判定 **pass**。

reviewer 讀了 idemix v0.3.1 的相關原始碼，所有攻擊與 fuzz 都在複本裡執行，沒有改動 repo。
- **must-fix 已關閉**，以下嘗試都回 `PROOF_INVALID`：
  - 為 `{0,1,2,5}` 衍生的 proof，把標頭改寫成要求的集合，再偽造 holder 與 status；
  - 揭露索引超出訊息數；
  - 訊息數改為 0、6、7、9、15、16、1024；
  - 多揭露 `consumer_id` 並由持有人重新簽名；
  - 補到 9000 bytes、聲稱 65535 則訊息的 proof，5 ms 內被拒。
  
  把這道檢查拿掉後，部分揭露的偽造會通過，證明擋住它的就是這道檢查。
- 先前的 should-fix 與 note 全部在程式裡關閉：
  - 約 34 萬筆隨機 proof 的 fuzz 沒有 panic；
  - 8 個並行驗證同一筆出示，只有一筆通過；
  - 拒絕的先後順序不會洩漏資訊。
- 測試、反向與證據互相一致：38 = 8 + 30；benchmark、授權、11／11 都和檔案相符。

新發現：
- **N1（文件，should-fix，已更正）**：idemix 有 blind signing；已更正第 5 節第 3 點與 C1-D2。
- **N2（文件，note，已更正）**：`bbs` 套件也 import `x/crypto`；已更正第 2 節。
- **N3（測試，note，未改）**：
  - 「聲稱 65535 則訊息」「過短」「長度欄位越界」三個負例，在 idemix 依大小就被拒，沒有走到訊息數綁定，所以不能證明那道綁定；
  - 「只揭露 schema」在拿掉綁定後會變成 `HOLDER_BINDING_INVALID`；
  - 真正獨立證明 must-fix 的，是「只揭露部分欄位」那一項（反向第 8 項）。
- **N4（給 C2，note）**：`Verify` 信任呼叫端給的 Request（intent、揭露欄位），challenge 只記字串。C2 若從持有人的訊息取得 Request，intent 綁定就會變成由持有人宣稱。應在發 challenge 時存下 Request，驗證時依 challenge 取回；未用的 challenge 也要有期限與上限。
- **N5（給 C2，note）**：驗證期間持有鎖，約 3.4 ms 的配對運算會序列化。C2 應改為：先保留 challenge，再在鎖外驗證，最後用掉或釋放。
- **N6（給 C2，note）**：proof 位元組可被延展，函式庫接受尾端多出的 bytes。第三方延展會因持有人簽章而失敗，但 C2 不得把 proof 位元組當成唯一性或重播的鍵。
- **N7（反向，note）**：反向第 9 項（必要欄位）拿掉檢查後，被後面的 status 格式檢查擋下；只有隱藏 task_id 才會直接通過。若改成以 task_id 為目標，更能證明這道檢查本身的作用。
- **N8（note）**：`BenchmarkVerify` 量到的是 generator 已快取的情況；訊息數固定為 8，所以沒有影響。

派工筆記寫「交 Codex review」；Codex 已到期，依 Aaron 的指示改由 Claude Code 獨立上下文 review，本節即是。
