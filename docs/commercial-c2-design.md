# C2 BBS+ 憑證接入 Policy：設計與契約升版（第二版，待 review）

> 2026-10-14，Claude Code，依 vault〈Claude-Code-C2-BBS憑證接入Policy派工-20261013〉與 Aaron「先做 option a」。分支 `commercial-c2`（從 `commercial-c1`）。
> **狀態：第二版設計稿。** 第一版（`3d8aeb2`）經 Claude Code 獨立上下文 review 判定 not pass（4 項 must-fix、12 項 should-fix；第 12 節對照）。Aaron 已批准第一版的 C2-D1～D7 照建議進行；本版**新增 C2-D8、C2-D9，並調整 C2-D4 的建議**，這三項須 Aaron 再確認（第 9 節）。**通過前不寫 Policy、Store、Host 的程式。**

依據：
- [commercial-c1.md](commercial-c1.md)：選型與 C1-D1～D4；
- [commercial-demo-roadmap.md](commercial-demo-roadmap.md) 的 C2 段；
- [commercial-transaction-flow.md](commercial-transaction-flow.md) §3.1（新理由碼隨契約版本增加；不放寬舊版白名單）與 §10.1；
- vault 執行計畫的驗收第 1、12、26、28、29、35 項（第 11 節對照）；
- 現行程式以 `526d39f` 為準。

## 1 一段話

- **新增契約 `apa.pay.v2`。**
  - `paycontract` 改成依版本參數化：v1 的規格完全凍結，v2 = v1 加三個 Deny 理由碼與 `CREDENTIAL_VERIFIED` 事件。
  - 委託、核准會話、grant、Signer 的 intent 雜湊、A2 本人核准、P1-b 付款路徑，都改成依物件自己的版本選擇規格。所以 **v2 task 一樣能用 WebAuthn 核准，將來也能付款**（C2-D8）。
  - `apa.pay.v1` 保留為明標 fake 的開發模式（fixture 憑證），M1 不動。
- **流程**：
  1. 測試 issuer 在 task 開始時，依可信 task 與 holder 信任設定發出憑證；
  2. 請購時，在同一個 `Commit` 鎖裡依序做：取 challenge（綁定 task、purchase_order_id、intent、揭露欄位）→ holder 出示 → Store 送出；
  3. Store 在同一個寫入交易裡驗證、用掉 challenge，寫下驗證紀錄與 `CREDENTIAL_VERIFIED`，再交 Policy。
- **Policy 只用 verifier 的結構化結果。** 失敗以新理由碼 Deny 並留下紀錄；過期用 `AUTHORIZATION_EXPIRED`；「沒有登記 holder」用 `CONFIG_MISSING`。
- **可追溯**：裁決、驗證紀錄、事件、challenge、憑證 snapshot 五者互相核對，重新開啟 Store 時也會驗證（第 3.3 節）。
- 模型能用的工具不變，只能指定 `product_id`。
- **誠實標示**：issuer、holder、verifier 在同一個程序，C2 證明的是**接線與規則**，不是彼此獨立的保證（第 5 節）。

## 2 現況與阻礙

### 2.1 第一版就列出的（review 確認正確）

| # | 現況 | 處理 |
|---|---|---|
| B1 | Store 寫入時（`ops.go` L505）和開啟時（`verify.go` L114-116）都拒絕不是 fixture 的憑證 | v2 用新的 snapshot 種類 `credential_bbs`；檢查依版本分開（第 8 節） |
| B2 | 每筆請購沒有欄位記錄用了哪張憑證或哪次出示 | 新表 `credential_verifications`、事件 `CREDENTIAL_VERIFIED` |
| B3 | `CompareRetry` 雜湊整個 order；proof 每次都不同 | 出示不放進 order |
| B4 | 沒有地方保存 challenge | 新表 `credential_challenges`（第 7 節） |
| B5 | `Rules` 只看憑證是否為 nil 與 `ValidUntil` | `Rules` 不改；v2 的憑證步驟放在它之前 |
| B6 | M1 的 provenance 會雜湊 demo registry、M1 profile 與 `cases.json` | C2 的 fixture 一律放新檔 |
| B7 | 主 module 是 `go 1.26.0`；idemix 要求 1.26.3 | 升 go 指令（C2-D6） |

### 2.2 第一版漏掉、review 指出的（v1 專屬的分支與檢查）

| 位置 | 現況 | 處理 |
|---|---|---|
| `paycontract` 的 `Validate`、`digest`、`ValidateDecision`、`ValidateEvent`、`ValidateEventOrder`（paycontract.go L66/L127/L148/L151/L287，events.go L41/L212） | 寫死 `Version` 與 `Domain` | 改為依版本參數化（第 3.1 節） |
| `grant.go` 的 `short()`（L46）、`Delegation.Validate`（L54）、`AuthorizationGrant.Validate`（L106） | 寫死 v1 | 委託、grant、會話的 ID 與驗證都改用物件自己的版本 |
| Signer：`approvals.go` L329-334、`core.go` L256-260 | 以 `IntentHash()` 拒絕非 v1 | 改為 `paycontract.Supported(version)` |
| Store：`purchaseRow.decode`（ops.go L92-101）、`related()`（L637-651）、`openTask`（L140）、`StartTask` 的 genesis（L449）、`Submit` 的解碼（L541-559）、`taskContract`（pay.go L185-219）、`ConfirmDelegation`（pay.go L267）、`verifyPay`／`verifyPayTables`（pay.go L625、L714-715）、`payment.go` L110／L202、`approvals.go` L153 | 只認 v1，或把 v2 當成 M1 或資料毀損 | 都改為依版本分派；v2 共用 v1 的路徑，再加上憑證步驟 |
| `emit()`（ops.go L221） | `evidence_mode` 寫死 `fixture` | v2 沿用這個標示：它描述的是付款證據層，C2 不付款；第 9 節 C2-D9 說明 |
| Host 與畫面：`shop.go` L120／L163、`scenarios.go` L93-106（v1 只限 fake）、`control.go` L268、`runs.go` L150-155／L327-328、`api.go` L226／L273、`web/adapter.js` L84-85、`web/host.js` L245／486／498／611／631（非 V1 一律當 M1） | 只認 v1 | 一律改為「pay 系列版本」的判斷（`isPay(version)`）；v2 能否用在 live 見 C2-D9 |
| Store schema：`config_snapshots.kind`（store.go L301）、`tasks.contract_version`（L399）、`host_runs.contract_version`（L411）的 CHECK | SQLite 無法用 ALTER 修改 CHECK；`tasks` 是外鍵目標 | schema 6 重建資料表（第 8 節） |

不受影響：`m1evidence` 一律拒絕非 M1 的版本（m1evidence.go L174-182）；Unity 沒有契約版本的假設。

## 3 契約

### 3.1 `paycontract` 依版本參數化（C2-D8）

- 新增 `type Spec struct{ Version, Domain string; ReasonCodes, DenyReasons, EventTypes map[string]bool; PayloadFields map[string][]string }` 與 `SpecOf(version) (Spec, bool)`。
- **`specV1` 的內容與現在的常數逐字相同**，v1 的 golden 測試原封不動。
- **v1 拒絕 `credential` 這個 payload 欄位**，並有測試（review M2）。
- 所有 digest、ID、驗證函式都改用物件自己的 `Version` 取得規格：`IntentHash`、`RetryKey`、`SessionID`、`GrantID`、`DelegationID`、事件雜湊。
- 委託、grant、核准會話的版本一律等於 task 的契約版本。
- `Supported(version)` 只接受 v1 與 v2，Signer、Store、Host 都用它取代 `== paycontract.Version`。
- v2 另外凍結一組 golden：intent_hash、retry_key、genesis、session、grant、delegation。

### 3.2 `apa.pay.v2` 的差異

- **Domain**：`agent-payment-authority/pay/v2`。
- **型別**：`Intent`、`PaymentBinding`、`PurchaseOrder` 的欄位與 v1 相同。
- **新增 Deny 理由碼**：`CREDENTIAL_INVALID`、`CREDENTIAL_REVOKED`、`CREDENTIAL_STATUS_UNAVAILABLE`。細分原因放在驗證紀錄的 `detail`，取自固定代碼表。
- **新事件 `CREDENTIAL_VERIFIED`**：payload 只有 `credential`；其他 v2 事件不得帶這個欄位。內容：
  - `result`：`valid`／`absent`／`invalid`／`revoked`／`status_unavailable`；
  - `detail`、`challenge_id`；
  - `credential_schema`（就是外層版本，review 的 note）；
  - `issuer`、`holder`、`status_index`、`expiry`；
  - `status_list_version`、`status_list_issued_at`、`status_list_sha256`；
  - `proof_sha256`：只供追溯，不是重播的鍵；
  - `verifier`。
- 共用的 `contracts.EventPayload` 以 omitempty 指標加入這個欄位；M1 的 schema 標為 `{"not":{}}`，M1 的 `ValidateEvent` 拒絕它。
- **`PolicyDecision` 不變。**
- **過期**：verifier 回 `valid` 並帶出 expiry，由 `Rules` 判定 `AUTHORIZATION_EXPIRED`。畫面不得把這種情況寫成「憑證有效」，要寫「憑證已過期」（review 的 note）。

### 3.3 v2 的事件順序與恢復不變式（review S1）

- 每筆 v2 請購**恰有一個** `CREDENTIAL_VERIFIED`，位置在 `PURCHASE_PROPOSED` 之後、`POLICY_DECIDED` 之前，三者同一個交易。
- `result` 對應理由碼：
  - `absent` 一定是 Deny `CONFIG_MISSING`；
  - `invalid`、`revoked`、`status_unavailable` 一定是 Deny，理由碼各自對應；
  - Allow 或 Ask 一定是 `result=valid`，而且 `expiry > decided_at`。
- `verify.go` 在開啟 Store 時交叉核對以下各項，不一致就回報資料毀損：
  - 驗證紀錄 ↔ 事件 ↔ challenge 的 `state=used` ↔ task 的 `credential_bbs` snapshot（issuer、holder、status_index、expiry）；
  - 驗證紀錄的 `(task_id, purchase_order_id)` 唯一；
  - verifier 與 Policy 用同一個交易時間 `now`。

## 4 憑證外層 `apa.credential.v1` 與驗證規則

- **欄位**：`schema, issuer, task_id, consumer_id, c0, expiry, holder, status`，固定順序，每個訊息是 UTF-8 的 `名稱=值`。
- **格式**：
  - `schema` = `apa.credential.v1`；
  - `issuer` 符合 `^[a-z0-9-]{1,64}$`；
  - `task_id`、`consumer_id` 用 registry 的格式；
  - `expiry`：Unix 秒，十進位，不帶 `+`、沒有前導零；
  - `holder`：`ed25519:` 加 64 個小寫 hex；
  - `status`：十進位；
  - `c0`：`0x` 加 64 個小寫 hex（C2-D5）。
- **簽章**：idemix v0.3.1 `bbs`。一律標明「BBS+（舊版），不是 W3C `bbs-2023`」。
- **必要揭露**：`schema, issuer, task_id, expiry, holder, status`。
- **出示 `apa.presentation.v1`**：`{version, challenge_id, disclosed, proof, holder_sig}`。
  - nonce 的內容依序為 domain `agent-payment-authority/credential/v1/presentation`、verifier ID、challenge ID、intent_hash、揭露清單，每段都有長度前綴；
  - holder 簽 `"…/credential/v1/holder\0" ‖ nonce ‖ SHA-256(proof)`。
- **驗證順序**，先出現的失敗就決定結果：
  1. **invalid**：challenge 依 (task, purchase_order_id) 取回且未用、未過期，intent 相符；外層格式；揭露集合正好等於要求；proof 標頭的訊息數是 8、索引等於要求；proof 驗證（傳複本）；schema 與 issuer 受信；holder 等於信任設定裡這個 consumer 的公鑰，持有人簽章正確；揭露的 `task_id`、`expiry`、`status`、`holder` **等於這個 task 的 `credential_bbs` snapshot**（review S2）。
  2. **status_unavailable**：狀態清單不可用（第 6 節）。
  3. **revoked**：status 在撤銷清單中。
  4. 以上都通過時回 `valid`，期限交給 `Rules`。
- **與 C1 spike 的差異**（review S3）：
  - spike 在 verifier 內就以 `CREDENTIAL_EXPIRED` 拒絕；C2 改由 `Rules` 判定期限。
  - spike 只在成功時用掉 challenge；C2 只要送出就用掉，因為這筆請購已有裁決。

## 5 角色、金鑰、信任設定（C2-D3）

- **目錄**：`--credential-dir`，必須是 0700、擁有者為目前使用者、不是 symlink，規則和 Store 的 `dbPath` 相同（store.go L271-295）。內容：
  - `issuer.key`：BBS+ 私鑰，0600；
  - `status.key`：Ed25519，簽狀態清單，0600，和 issuer 金鑰分開；
  - `holders.json`：每個 consumer 的 holder 公鑰，也就是信任設定；
  - `holder/<consumer>.key`：0600；
  - `holder/<task>.cred`：0600。
- **`apa-issuer init --consumers a,b`** 產生測試金鑰與 `holders.json`，並且要求每個 consumer 的 holder 公鑰互不相同（review S10）。
- **StartTask（v2）** 時，把信任設定（issuer ID、issuer 公鑰、status 公鑰的指紋、這個 consumer 的 holder 公鑰）與發出的憑證中繼資料，一起寫進 `credential_bbs` snapshot。之後改檔不影響已開始的 task。
- **發證**：
  - `expiry` = min(`Delegation.ExpiresAt`, 開始時間 + 24 h)（review S9：沒有「valid_until」這個欄位）；
  - 為了負例，issuer 提供只在測試中可用的短期限注入，不是產品旗標；
  - `status` 索引由 Store 的 `credential_status_counter` 配發，保證唯一。
- **誠實標示**（review S5）：這些都在同一台主機、同一個程序。主持人的程序可以自行發證、出示、更新清單。C2 證明的是格式、綁定、撤銷與裁決的接線，**不是** issuer、holder、verifier 彼此獨立的保證。分程序部署另做。

## 6 撤銷：簽章的狀態清單（C2-D4，本版調整建議）

- **`status.json`**：`{version, issuer, list_version, issued_at, next_update, revoked:[…]}`，以 `status.key` 簽章。寫入時先寫暫存檔再改名（atomic rename）。
- **驗證時**，以下任一項都回 `status_unavailable`：
  - 檔案不存在、讀到不完整的檔案、簽章無效；
  - `issuer` 不符；
  - `now >= next_update`；
  - `next_update − issued_at > 600 s`：新鮮期有上限，review S4；
  - `issued_at > now + 60 s`：時鐘偏差上限；
  - **`list_version` 小於 Store 記錄的最高版本**：防止換回撤銷前的舊清單。Store 在每次驗證時更新這個最高版本。
- 使用的清單版本、`issued_at` 與 SHA-256，寫進驗證紀錄與事件（review S4）。
- **更新方式（C2-D4 的選項）**：
  - (a) **Host 在執行中每 300 s 自動由 issuer 更新**（新建議）。issuer 本來就在同一個程序，自動更新不會降低保證（第 5 節）；它避免了「超過 600 s 後每筆請購都被 Deny、task 結束」的操作成本。`revoke` 仍需明確執行。
  - (b) 只能手動 `apa-issuer refresh`（第一版的建議）：保留了「issuer 停止就 fail closed」的展示效果，但主持人每 10 分鐘要操作一次。
  - 不論哪一個，都可以用測試的時間注入，示範「issuer 停止 → `CREDENTIAL_STATUS_UNAVAILABLE`」。

## 7 請購流程與 challenge（review M3、M4）

全部在 `Binding.Commit` 的鎖裡依序執行，所以 run 結束後不會再寫入，同一個 task 的請購也會逐一處理：

1. **先看已存紀錄**：鎖內再查一次這筆 purchase 是否已存在；有就直接回原紀錄，不取 challenge。重複的工具呼叫因此不會影響 task。
2. 依 v1 的方式組裝 order。在 Store 裡先檢查契約、ownership、`offered`、`CheckTaskActive`（review：這些都在 challenge 之前）。
3. **holder 狀態**：
   - 信任 snapshot 裡**沒有登記這個 consumer 的 holder**，就是「沒有憑證」：不取 challenge，以 `result=absent` 送出，Deny `CONFIG_MISSING`。task 的 `credential_bbs` snapshot 本身就標示 `state=absent`，三者一致（review M3）。
   - 有登記，但 holder 材料不見或讀不到：以 `result=invalid`、`detail=HOLDER_MATERIAL_UNAVAILABLE` 送出，Deny `CREDENTIAL_INVALID`。
4. `Store.IssueChallenge(task, purchase_order_id, intent_hash, disclose)`：
   - 以 **(task_id, purchase_order_id)** 為鍵，**最多一個**未用的 challenge（review M4：intent 含時間，不能當上限的鍵）；
   - 已有未用、未過期的 challenge 就直接回傳同一個，不取代；過期的標為 `expired`；
   - 有效期 60 s。
5. holder 出示。
6. `Store.SubmitV2(session, raw, presentation)`，在一個寫入交易裡：
   1. 重新做第 1、2 步的檢查；
   2. 依第 4 節驗證；
   3. 用掉 challenge；
   4. 寫入 `credential_verifications`，`UNIQUE(task_id, purchase_order_id)`，外鍵指向 challenge；
   5. `paypolicy` v2 裁決；
   6. 依序 emit `PURCHASE_PROPOSED`、`CREDENTIAL_VERIFIED`、`POLICY_DECIDED`。
- **重播**：
  - 同一份出示再送一次，因 challenge 已用而 invalid；
  - 同一個 request ID 會在第 1 步回傳原紀錄，不會借用舊結果去通過新的請購，因為 request ID 只對應這一筆 purchase；
  - 跨 verifier、跨 task、跨請購，分別由 nonce、snapshot 比對、intent 比對擋下。
- **鎖**：驗證約 3.4 ms，在寫入交易內進行（`ponytail:` 註記）；有瓶頸時再改成鎖外驗證（C1 review 的 N5）。

## 8 Store schema 6（review S6）

- **新表**：
  - `credential_challenges(challenge_id PK, task_id FK, purchase_order_id, intent_hash, disclose, verifier_id, issued_at, expires_at, state CHECK('issued','used','expired'))`，以 partial unique index 保證每個 `(task_id, purchase_order_id)` 最多一個 `issued`；
  - `credential_verifications(task_id, purchase_order_id, challenge_id FK NULL, result, detail, issuer, holder, status_index, expiry, status_list_version, status_list_issued_at, status_list_sha256, proof_sha256, verifier, verified_at, UNIQUE(task_id, purchase_order_id))`；
  - `credential_status_counter(issuer PK, next_index)`；
  - `credential_status_seen(issuer PK, max_list_version)`。
- **重建**：`config_snapshots`（kind 加 `credential_bbs`）、`tasks`（`contract_version` 加 `apa.pay.v2`）、`host_runs`（同上）三張表的 CHECK。
  - 做法：在一個交易裡 `PRAGMA foreign_keys=OFF` → 建新表 → 複製資料 → 刪舊表 → 改名 → 重建索引與觸發器 → `PRAGMA foreign_key_check` 必須沒有任何結果 → `foreign_keys=ON`。
  - `tasks` 是 purchases、events、delegations 的外鍵目標，所以改名後要以 `foreign_key_check` 驗證。
  - 遷移前備份資料庫檔；遷移中途失敗就不提交。
- **`tasks.credential_snapshot`** 在 v2 指向 `credential_bbs` 種類的 snapshot；`decodeSnapshot`（ops.go L112）、`settings()`（L674-678）、`verify.go` L166-172 依版本解碼。
- **v2 task 帶 fixture 的 `credential` snapshot 視為毀損**，對應的負例在第 11 節。

## 9 需要 Aaron 決定的事

Aaron 10/14 已批准第一版的 D1～D7 照建議進行。下表標出**本版有變動、須再確認**的項目。

| # | 決定 | 選項 | 建議 | 本版狀態 |
|---|---|---|---|---|
| D1 | 契約升版方式 | 新增 `apa.pay.v2`；或在 v1 上直接加碼（違反 §3.1） | 新增 v2 | 已批准，不變 |
| D2 | 憑證失敗的語意 | (a) Deny 並留紀錄，task 結束；(b) 當成裁決前的錯誤，不留裁決；(c) 只把 `STATUS_UNAVAILABLE` 當成暫時、不結束 task 的拒絕，並留稽核紀錄 | (a)；若採 D4(a)，status 不可用的情況很少，不必另外處理 | 已批准；本版補上 (c) 這個選項 |
| D3 | 角色放在哪 | 同一程序分套件，或分程序 | 同一程序，並誠實標示（第 5 節） | 已批准；本版補上誠實標示 |
| D4 | 撤銷清單的更新 | (a) Host 每 300 s 自動更新；(b) 只能手動 | **改建議為 (a)**（第 6 節） | **須再確認** |
| D5 | `c0` 在 Z2 之前 | (a) 全零佔位，不揭露、不參與裁決；(b) 現在就放「已核准委託」的摘要，綁定已核准的條件 | (a)：C₀ 在計畫中是 Z2 的預算承諾，若現在挪作他用，Z2 時要改變它的意義；委託仍由 Store 的 snapshot 約束 | 已批准；本版補上 (b) |
| D6 | 正式依賴 | idemix v0.3.1 加入主 `go.mod`，go 指令升到 1.26.3 | 照做 | 已批准，不變 |
| D7 | 畫面 | 卡片與抽屜各加一列 | 照做；但 `web/host.js` 有 5 處「非 V1 就當 M1」要一併改，工作量比「一列」大 | 已批准；本版補上工作量 |
| **D8** | **v2 與核准、付款、委託的關係**（review M1） | (a) `paycontract` 依版本參數化，Signer、A2、P1-b、委託、grant 都在 C2 內支援 v2；(b) v2 只能用模擬核准、不能付款，另開一片補；(c) 不新增版本，在 v1 上加碼 | **(a)**：否則真正能核准與付款的契約只有 fixture 憑證，E1 的「同一個 intent_hash 串起憑證、裁決、核准、proof、交易」做不到 | **新增，須確認** |
| **D9** | **v2 可否用在 live，以及 `evidence_mode`** | (a) v2 在 fake 與 live 的場景目錄都可用，`evidence_mode` 沿用現況（描述付款證據層，付款仍未啟用）；(b) v2 只限 fake | **(a)**：商業 demo 要以真實 Agent 請購；每次真實執行仍需另行核准 | **新增，須確認** |

**之後的決定（不在 C2，先標明）**：驗收第 26 項要求在 x402 resource server 那一層出示，而且兩次出示對 server 不可串連。C2 在 Policy 這一層揭露 holder 公鑰，所以可以串連；這符合第 12 項「對自家 policy gate 不 unlinkable」。到 P2 做 x402 的 credential extension 時，需要另一種不揭露 holder、task、status 的出示方式，屆時可能要採用 C1-D2 的 (b)（blind signing）。

## 10 實作切片（核准後）

| 步驟 | 內容 |
|---|---|
| 1 | 依賴提交（idemix、go 指令）與授權紀錄 |
| 2 | `paycontract` 依版本參數化；v1 規格逐字凍結；v1 拒絕 `credential` 欄位；`Supported()`；v2 golden。M1、v1 的測試全部不改並通過 |
| 3 | 把 Signer、Store（A2、P1-b、委託、恢復檢查）、Host、`web/host.js`、`web/adapter.js` 的「只認 v1」改為「pay 系列」；以 v1 的既有測試證明行為不變 |
| 4 | `internal/credential`：issuer、holder、verifier、外層、狀態清單；從 spike 搬入規則並加上第 4 節的差異；單元測試與反向 |
| 5 | Store schema 6 與遷移；新表；`IssueChallenge`、`SubmitV2`；依版本的恢復檢查與第 3.3 節的不變式 |
| 6 | `paypolicy` v2 的憑證步驟 |
| 7 | Host 與 Shop：發證、`Commit` 內依序取 challenge、出示、送出；`apa-issuer`；`--credential-dir`；自動更新（若採 D4(a)）；場景目錄允許 v2 |
| 8 | 畫面：卡片與抽屜的「憑證檢查」列，`host.js` 對 v2 的判斷 |
| 9 | 文件：roadmap、transaction-flow §3.1／§10、`docs/policy.md`、操作說明 |
| 10 | 定向驗證計畫、反向、瀏覽器檢查，證據在 `artifacts/c2/` |

## 11 驗收

**對照計畫的驗收項目**
| 計畫驗收 | C2 涵蓋 | 不在 C2 |
|---|---|---|
| 1 憑證存進本地 keystore | 發證並存入 holder 區（測試 issuer、測試金鑰） | 「核准上限 $5」寫進憑證，屬 C₀ 與 Z2；eKYC 屬第二期 |
| 12 不可串連 | 文件寫明：對自家 policy gate **可串連**（揭露 holder），符合計畫原文 | 對 x402 resource server 不可串連 → 第 26 項，P2 |
| 26 credential extension | — | P2（x402 層） |
| 28 credential → commitment | 把 A 的憑證拿去 B 的 task → `CREDENTIAL_INVALID`（snapshot 與 holder 比對） | `registerTask` 與 C₀ 的鏈上部分 → Z2、P2 |
| 29 presentation 重播 | 跨 verifier、舊 challenge、把憑證複製到別的 holder 出示 → 全部拒絕並記錄 | policy／human grant 的重播：既有 A2 與 P1-b 已涵蓋；C2 以 v2 再跑一次 |
| 35 撤銷 fail-closed | 清單抓不到、簽章錯、換回舊版清單 → 拒絕並記錄 | — |

**測試清單**，每一項都核對理由碼、`detail`、驗證紀錄、事件鏈與 snapshot 一致：
- 正例：v2 的 Allow、Ask（並以 A2 的 WebAuthn 核准走完）與依金額的 Deny，都可沿 `intent_hash` 追溯。
- 負例：
  - 沒有登記 holder → `CONFIG_MISSING`（absent）；holder 材料不見 → `CREDENTIAL_INVALID`；
  - 過期 → `AUTHORIZATION_EXPIRED`；撤銷 → `CREDENTIAL_REVOKED`；
  - 狀態清單缺漏、簽章錯、issuer 錯、`issued_at` 在未來、新鮮期超過上限、換回舊版 → `CREDENTIAL_STATUS_UNAVAILABLE`；
  - 以下都 → `CREDENTIAL_INVALID`：
    - holder 不符；把 A 的憑證用在 B 的 task；
    - 跨 verifier、跨請購；
    - challenge 不存在、已用、過期；
    - 揭露不足或過度；
    - proof 被竄改、訊息數被竄改；
    - 揭露值與 snapshot 不符。
  - v2 task 帶 fixture 憑證 snapshot → 開啟 Store 時被判為毀損。
- **存取權**：
  - `--credential-dir` 的權限、擁有者、symlink 不合規則就拒絕啟動；
  - 金鑰檔不是 0600 就拒絕；
  - A 的 holder 材料不能用在 B 的 task。
- **並行**：同一個 product 的兩次工具呼叫在同一秒發生，只產生一筆 purchase，task 不會因此被 Deny（review M4）。
- **恢復**：重新開啟 Store 時，核對第 3.3 節的不變式；手動竄改任一筆紀錄 → 判為毀損。
- **不變**：M1 契約與 frozen case、v1 golden、`policy.Decide` 與 `Rules`、`CONFIG_MISSING` 的語意。M1 checker 與 `m1evidence` 仍只接受 fixture。
- 受影響的套件跑 race；拒絕的關鍵條件各做預期斷言型的反向。
- **checker**：本片不新增 claim。E1-b 加 credential claim 時，缺資料為 INCONCLUSIVE、可證偽的矛盾為 FAIL，不讓舊的 M1 包升級成新的 claim；這條規則記在 roadmap 的 E1-b 段。
- **review 通過前，不宣稱 BBS+ 已參與商業交易的可信准駁。**

## 12 第一版 review 對照（`3d8aeb2`，not pass）

| review | 處理 |
|---|---|
| M1 v2 與核准、付款、委託脫節 | §3.1 參數化、§2.2 列出全部位置、C2-D8 |
| M2 v1 白名單會被放寬 | §3.1：v1 拒絕 `credential`，並有測試 |
| M3 「缺憑證」沒有定義 | §7 第 3 步：absent 與 `HOLDER_MATERIAL_UNAVAILABLE`；§3.2 的 `result` |
| M4 challenge 的鍵與取代 | §7：在 `Commit` 內；以 (task, PO) 為鍵；最多一個；沿用不取代 |
| S1 不變式 | §3.3 |
| S2 snapshot 比對 | §4 第 1 項 |
| S3 和 spike 的差異與順序 | §4 |
| S4 狀態清單的強化 | §6、§8 |
| S5 誠實標示、目錄權限 | §5 |
| S6 schema 6 重建 | §8 |
| S7 漏列的分支 | §2.2、C2-D9 |
| S8 版本機制、委託與 grant 的版本 | §3.1 |
| S9 expiry 的來源 | §5 |
| S10 holders.json 的位置與唯一性 | §5 |
| S11 決定的選項 | §9 的 D2、D4、D5 |
| S12 驗收的缺漏 | §11 |
| notes | `credential_schema`（§3.2）；同一個 `now`、UNIQUE 與外鍵（§3.3、§8）；過期的用詞（§3.2）；C₀ 佔位（D5）；鎖內驗證（§7） |
