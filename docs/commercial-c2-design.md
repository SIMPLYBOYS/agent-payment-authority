# C2 BBS+ 憑證接入 Policy：設計與契約升版（待 review）

> 2026-10-14，Claude Code，依 vault〈Claude-Code-C2-BBS憑證接入Policy派工-20261013〉與 Aaron「先做 option a」（先做 C2 設計）。分支 `commercial-c2`（從 `commercial-c1`）。
> **狀態：設計稿，待 Claude Code 獨立上下文 review 與 Aaron 核准。本文件不含產品接線；通過前不寫 Policy、Store、Host 的程式。**

依據：
- [commercial-c1.md](commercial-c1.md)：選型與 Aaron 的 C1-D1～D4；
- [commercial-demo-roadmap.md](commercial-demo-roadmap.md) 的 C2 段；
- [commercial-transaction-flow.md](commercial-transaction-flow.md) §3.1（新理由碼一律隨契約版本增加）、§10.1（Policy 的拆分）；
- 現行程式的對照（第 2 節），以 `526d39f` 為準。

## 1 一段話

- **新增契約 `apa.pay.v2`**：和 v1 只差在「必須有通過驗證的 BBS+ 憑證出示」，另外加三個理由碼、一種事件。`apa.pay.v1` 原封不動，保留為明標 fake 的開發模式（fixture 憑證）。M1 也不動。
- **流程**：
  1. 測試 issuer 在 task 開始時，依可信的 task 發出憑證，存進 consumer 的 holder 區。
  2. 請購時，Host 先算出 intent，向 Store 取一次性 challenge；challenge 綁定 task、intent 與要揭露的欄位。
  3. holder 元件出示 derived proof。
  4. Store 在同一個寫入交易裡驗證、用掉 challenge、記下驗證結果，再交 Policy 裁決。
- **Policy 只使用 verifier 的結構化結果。** 驗證失敗、撤銷、撤銷狀態不可得，都以新理由碼 **Deny**，並留下紀錄。過期沿用既有的 `AUTHORIZATION_EXPIRED`，缺憑證沿用 `CONFIG_MISSING`。
- **追溯**：裁決紀錄可以沿 `intent_hash` 追到那一次出示的驗證紀錄與事件。
- 模型能用的工具不變，仍然只能指定 `product_id`。

## 2 現況與阻礙（為什麼不能直接接）

| # | 現況（`526d39f`） | 影響 | 本設計的處理 |
|---|---|---|---|
| B1 | Store 寫入時（`buildTerms`，`ops.go` L505）和開啟時（`verify.go` L114-116）都拒絕不是 fixture 的憑證 | 把 BBS 憑證放進現有的 `credential` snapshot，資料庫會打不開 | v2 用新的 snapshot 種類 `credential_bbs`；schema 升到 6，附遷移；檢查依契約版本分開 |
| B2 | 每筆請購沒有欄位記錄用了哪張憑證或哪次出示；`PolicyDecision` 與 v1 事件的白名單不收新欄位 | roadmap 的出口「裁決紀錄可追到該次出示的驗證結果」無處落地 | 新表 `credential_verifications`；新的 v2 事件 `CREDENTIAL_VERIFIED`；`PolicyDecision` 不變 |
| B3 | v1 的 golden 值已凍結；`CompareRetry` 會雜湊整個 order；derived proof 每次都不同 | 若把出示放進 `PurchaseOrder`，重送就會衝突，也會改動 v1 | 出示**不放進** order，當作 `Submit` 的另一個參數；`PurchaseOrder` 形狀不變 |
| B4 | `assembleV1` 算完 intent 就立刻 `Submit`，沒有地方保存 verifier 發出的 challenge | C1 review 的 N4：challenge 必須連同它所屬的 Request 一起保存 | 新表 `credential_challenges`，存下 task、intent、要揭露的欄位、期限；流程改為：取 challenge → 出示 → 送出 |
| B5 | `Rules` 只看憑證是否為 nil 與 `ValidUntil` | 撤銷、出示無效、狀態不可得沒有理由碼 | `Rules` 不改；v2 的 `paypolicy` 在 `Rules` 之前加入憑證步驟與新理由碼 |
| B6 | M1 的 provenance 會雜湊 `testdata/demo/registry.json`、M1 profile 與 `cases.json` | 改這些檔會讓 M1 的 digest 變動 | C2 的 fixture 一律放新檔（`testdata/c2/`），不碰 M1 的檔 |
| B7 | 主 module 是 `go 1.26.0`；idemix v0.3.1 要求 `go 1.26.3` | 引入正式依賴，必須提高 go 指令的版本 | 把 go 指令升到 1.26.3（本機工具鏈是 1.26.4）；正式依賴另外單獨提交 |

## 3 契約 `apa.pay.v2`（升版內容）

`apa.pay.v2` = `apa.pay.v1`，加上以下差異：
- **Domain** 改為 `agent-payment-authority/pay/v2`。所以 v2 的 `intent_hash`、事件雜湊與各種 ID 都和 v1 不同，另外凍結一組 v2 的 golden 值。
- **型別**：`Intent`、`PaymentBinding`、`PurchaseOrder` 的欄位與 v1 相同，所以模型與 402 的組裝不變。
- **新理由碼**，都是 Deny：
  - `CREDENTIAL_INVALID`：出示不成立。包括 proof 無效、揭露集合或訊息數不符、holder 簽章不符、issuer 或 schema 不受信、task 不符、challenge 不存在／已用／過期／屬於別的 intent；
  - `CREDENTIAL_REVOKED`：狀態清單標示已撤銷；
  - `CREDENTIAL_STATUS_UNAVAILABLE`：狀態清單缺漏、簽章無效或已經不新鮮。
  
  細分的原因寫在驗證紀錄的 `detail`，取自固定代碼表；理由碼本身不再細分，以免契約膨脹。
- **沿用的理由碼**：
  - 缺憑證（這個 consumer 沒有 holder 憑證）→ `CONFIG_MISSING`，語意和 M1 相同；
  - 憑證過期 → `AUTHORIZATION_EXPIRED`，由 `Rules` 依驗證後的 expiry 判定。
- **新事件 `CREDENTIAL_VERIFIED`**：在同一個交易裡，緊接在 `PURCHASE_PROPOSED` 之後、`POLICY_DECIDED` 之前寫入。payload 只有 `credential` 物件：
  - `result`：`valid` 或 `invalid`／`revoked`／`status_unavailable`；
  - `detail`、`challenge_id`；
  - `issuer`、`schema`、`credential_version`、`holder`（揭露的 holder 公鑰）、`status_index`、`expiry`；
  - `proof_sha256`：只用於追溯，**不是**唯一性或重播的鍵（C1 review 的 N6）；
  - `verifier`。
  
  共用的 `contracts.EventPayload` 加這個欄位時，M1 的 schema 記為 `{"not":{}}`，M1 的 `ValidateEvent` 也拒絕它，M1 的向量不變。
- **`PolicyDecision` 不變。** 裁決和驗證紀錄靠 `purchase_order_id` 與 `intent_hash` 連結。

## 4 憑證外層 `apa.credential.v1`（C1-D3）

- **欄位與順序**固定，沿用 spike：`schema, issuer, task_id, consumer_id, c0, expiry, holder, status`。每個訊息是 UTF-8 的 `名稱=值`。
- **格式**：
  - `schema` 固定為 `apa.credential.v1`；
  - `issuer` 符合 `^[a-z0-9-]{1,64}$`；
  - `task_id`、`consumer_id` 沿用 registry 的 ID 格式；
  - `expiry`：Unix 秒，十進位，不帶 `+`、沒有前導零；
  - `holder`：`ed25519:` 加 64 個小寫 hex；
  - `status`：十進位索引；
  - `c0`：`0x` 加 64 個小寫 hex（見第 8 節 C2-D5）。
- **簽章**：idemix v0.3.1 的 `bbs`（BLS12-381、`bbs12381g2pub`）。文件與畫面一律標明「BBS+（舊版），不是 W3C `bbs-2023`」。
- **必要揭露**：`schema, issuer, task_id, expiry, holder, status`。`consumer_id` 與 `c0` 不揭露。
- **出示的外層 `apa.presentation.v1`**：`{version, challenge_id, disclosed{6 個欄位}, proof(base64url), holder_sig(base64url)}`。
  - nonce 是 SHA-256，內容依序為 `agent-payment-authority/credential/v1/presentation`、verifier ID、challenge ID、intent_hash、揭露欄位的正規清單，每段都有 4 bytes 長度前綴。
  - holder 簽 `"…/credential/v1/holder\0" ‖ nonce ‖ SHA-256(proof)`。
- **驗證規則**沿用 C1 第二版：
  - 先比對 proof 標頭的訊息數必須是 8、揭露索引必須正好等於要求；
  - 傳複本給函式庫；
  - 依序檢查格式、issuer、schema、holder 簽章與狀態。

## 5 角色、金鑰與資料放在哪裡（C1-D4）

| 角色 | 放在哪裡 | 金鑰與資料 | 誰能指定 |
|---|---|---|---|
| Issuer（測試） | Host 程序內的 `internal/credential/issuer` | `--credential-dir` 底下的 `issuer.key`（BBS+ 私鑰，0600）、`status.key`（Ed25519，簽狀態清單）；由新的 `apa-issuer init` 產生測試金鑰 | 只有主持人的設定；模型與瀏覽器都不能 |
| Holder（consumer 的代理） | Host 程序內的 `internal/credential/holder`，由 Shop 呼叫 | `--credential-dir/holder/<consumer>.key`（Ed25519，0600）與發出的憑證 `holder/<task>.cred`（0600） | 只依可信的 binding（task 對應 consumer）；模型只能說 `product_id` |
| Verifier | Store 的 v2 submit 路徑（`internal/credential/verifier`，純函式＋Store 的資料表） | 信任設定：issuer ID 與公鑰、status 公鑰、registry 中每個 consumer 的 holder 公鑰 | 只有主持人的設定 |
| Policy | `paypolicy`（v2 分支） | 只吃 verifier 的結構化結果 | — |

- **同一個程序**：issuer、holder、verifier 都在 Host 程序裡（C2-D3）。程式上分成三個套件，各用各的金鑰檔，互不 import 對方的私密部分。畫面與文件標明「示範：三個角色在同一台主機、同一個程序」。
- **holder 的信任根**：新的設定檔 `testdata/c2/holders.json`，內容是每個 consumer 的 holder 公鑰。verifier 要求揭露的 holder 等於這個 consumer 登記的公鑰；consumer 由 task 經 registry 得出，不由出示者宣稱。
- **發證時機**：v2 task 開始（`StartTask`）時，issuer 依可信 task 發出：
  - `expiry` = min(委託的 valid_until, 開始時間 + 24 h)；
  - `status` = issuer 配發的下一個索引；
  - 寫入 holder 區，同時在 Store snapshot `credential_bbs`：issuer、issuer 公鑰指紋、schema、expiry、status、holder 指紋、簽章的 SHA-256；**不存私鑰，也不存簽章本身**。
  - issuer 缺金鑰或狀態清單時，v2 task 不啟動；這和缺 profile 時的處理一樣。

## 6 撤銷（C1-D4）

- **來源**：issuer 簽發的狀態清單 `status.json`：`{version, issuer, issued_at, next_update, revoked:[索引…]}`，以 `status.key`（Ed25519）簽章。
- `apa-issuer revoke --status N` 會重新簽發清單；`apa-issuer refresh` 只更新時間。
- **新鮮度**：每次驗證都重新讀檔、驗簽。以下任一情況都回 `CREDENTIAL_STATUS_UNAVAILABLE`，也就是 fail closed：
  - 檔案不存在；
  - 簽章無效；
  - `issuer` 不符；
  - `now >= next_update`；
  - `issued_at > now + 60 s`（時鐘偏差上限）。
- `next_update` 預設是 `issued_at + 600 s`，主持人用 `refresh` 延長。
- **不使用快取的「verified」**：每一筆請購都重新驗 proof 與狀態清單。

## 7 請購流程（v2）與 challenge

1. `Shop.Call` 的組裝與 v1 相同，算出 `intent_hash`。
2. `Store.IssueChallenge(session, intent_hash)`：
   - 寫入 `credential_challenges(challenge_id, task_id, intent_hash, disclose, verifier_id, issued_at, expires_at, state)`；
   - `expires_at` 是 issued_at + 60 s；
   - **每個 (task, intent) 同時只有一個未用的 challenge**；新的會取代舊的。
3. holder 用這個 challenge 出示。
4. `Store.SubmitV2(session, raw, presentation)`，在**一個寫入交易**裡：
   1. 檢查契約、order、ownership、retry；
   2. **同一個 request ID 已有紀錄時直接回傳原紀錄，不看出示**（B3）；
   3. 依 `challenge_id` 取回存下的 Request，比對 task、intent、未用、未過期；
   4. 驗證出示與狀態清單；
   5. 把 challenge 標為已用。**不論驗證結果，這一次送出都會用掉 challenge**，因為這筆請購已有裁決；
   6. 寫 `credential_verifications`；
   7. `paypolicy` v2 裁決；
   8. 依序 emit `PURCHASE_PROPOSED`、`CREDENTIAL_VERIFIED`、`POLICY_DECIDED`。
- **鎖**：驗證約 3.4 ms，在 Store 的寫入交易裡進行。展示的負載可以接受（`ponytail:` 註記）。若之後量測到瓶頸，改為鎖外驗證、鎖內只做保留與用掉（C1 review 的 N5）。
- **重播**：同一份出示再送一次，會因 challenge 已用而 `CREDENTIAL_INVALID`；但如果是同一個 request ID，會先在第 2 步回傳原紀錄。跨 verifier 會因 nonce 含 verifier ID 而失敗；跨 task 會因 task_id 揭露值與 challenge 不符而失敗；跨請購會因 intent 不符而失敗。

## 8 需要 Aaron 決定的事

| # | 決定 | 建議 |
|---|---|---|
| C2-D1 | 契約升版的方式 | **新增 `apa.pay.v2`**。v1 保留為明標 fake 的開發模式（fixture 憑證），M1 不動。另一個選擇是在 v1 上直接加碼，但這違反 §3.1 與 v1 golden 已凍結的承諾 |
| C2-D2 | 憑證失敗的語意 | **Deny 並留下紀錄**（新理由碼，task 結束）。另一個選擇是當成裁決前的錯誤，不留下裁決，但這樣稽核看不到「因憑證被拒」 |
| C2-D3 | 角色放在哪個程序 | **issuer、holder、verifier 都在 Host 程序內，分套件、分金鑰檔**，並標明是示範。分程序（類似 `apa-signer`）比較真實，但工作量大，也不是 demo 的必要條件 |
| C2-D4 | 撤銷來源與新鮮度 | **issuer 以 Ed25519 簽狀態清單，`next_update` 預設 600 s，過期或缺漏就 fail closed**（第 6 節） |
| C2-D5 | `c0` 在 Z2 之前的值 | **全零的 `0x00…00`，不揭露、不參與裁決**，文件標明「Z2 之前的佔位值」。Z2 定義 C₀ 後，以 `credential_version` 升版重新發證 |
| C2-D6 | 正式依賴與 go 版本 | **主 `go.mod` 加入 `github.com/IBM/idemix` v0.3.1（及 mathlib v0.3.2 等），go 指令升到 1.26.3**；依賴提交單獨做，並附授權清單 |
| C2-D7 | 畫面 | **C2 只在卡片與抽屜加上「憑證檢查」一列**（結果、issuer、到期、可串連的提示、「BBS+（舊版），不是 bbs-2023」）。完整的 UX 留給 UX2／UX3 |

## 9 實作切片（核准後）

| 步驟 | 內容 | 主要檔案 |
|---|---|---|
| 1 | 依賴提交（idemix、go 指令）與授權紀錄 | `go.mod`、`go.sum` |
| 2 | `internal/credential`（issuer、holder、verifier、外層、狀態清單），把 spike 的規則搬入；單元測試、反向 | 新套件 |
| 3 | 契約 v2：版本、domain、理由碼、`CREDENTIAL_VERIFIED`、驗證；v2 golden；M1 schema 的 `{"not":{}}` | `internal/paycontract`、`internal/contracts/events.go`、`schemas/m1.schema.json` |
| 4 | Store schema 6：`credential_bbs` snapshot、`credential_challenges`、`credential_verifications`；遷移 5→6；依版本分開的開啟檢查；`IssueChallenge`、`SubmitV2` | `internal/store` |
| 5 | `paypolicy` v2 分支：憑證步驟放在 `Rules` 之前 | `internal/paypolicy` |
| 6 | Host 與 Shop：發證、取 challenge、出示、送出；`apa-issuer` 命令；`--credential-dir`；`testdata/c2/` | `internal/buyer`、`internal/toolbridge`、`cmd/apa-issuer`、`cmd/demo` |
| 7 | 畫面一列（C2-D7） | `web/adapter.js` |
| 8 | 驗證：定向計畫與反向，證據在 `artifacts/c2/` | `scripts/verify/` |

## 10 驗收（對照派工與 roadmap）

- 正例：發證 → 取 challenge → 出示 → 驗證 → Policy 得出 Allow、Ask 或其他依金額的 Deny。裁決沿唯一的 `intent_hash` 可以追到 `credential_verifications` 與 `CREDENTIAL_VERIFIED`。
- 負例，每項核對理由碼、`detail`、Store snapshot、事件鏈三者一致：
  - 缺憑證 → `CONFIG_MISSING`；過期 → `AUTHORIZATION_EXPIRED`；撤銷 → `CREDENTIAL_REVOKED`；
  - 狀態清單缺漏、無效或過舊 → `CREDENTIAL_STATUS_UNAVAILABLE`；
  - 以下都回 `CREDENTIAL_INVALID`：
    - holder 不符；跨 task、跨 verifier、跨請購；
    - challenge 不存在、已用、過期；
    - 揭露不足、過度揭露；proof 無效或被竄改。
- 不變的部分：M1 的契約與 frozen case、v1 golden、`policy.Decide` 與 `Rules`、`CONFIG_MISSING` 的語意都不變。M1 checker 與 `m1evidence` 仍只接受 fixture。
- 受影響的套件跑 race；拒絕的關鍵條件各做預期斷言型的反向。
- checker：本片**不新增** credential claims（E1-b 才加），也不讓舊的 M1 包升級成新的 claim。
- **review 通過之前，不宣稱 BBS+ 已參與商業交易的可信准駁。**

## 11 不在本片

- eKYC、正式身分文件、真實使用者資料、外部 issuer、正式憑證發放；
- ZK（Z1、Z2）、付款（P2）、A2 的改動；
- 不可串連的持有人綁定（blind signing，C1-D2 的 (b)）；
- 分程序部署。
