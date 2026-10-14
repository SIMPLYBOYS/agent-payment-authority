# C2 BBS+ 憑證接入 Policy：設計與契約升版（第四版）

> 2026-10-14，Claude Code，依 vault〈Claude-Code-C2-BBS憑證接入Policy派工-20261013〉與 Aaron「先做 option a」。分支 `commercial-c2`（從 `commercial-c1`）。
> **狀態：第四版設計稿；C2a 已通過 review；C2b 已實作並通過獨立上下文 review，should-fix 已修（第 18 節、[commercial-c2.md](commercial-c2.md) 第 8 節）。**
> - 第一版（`3d8aeb2`）與第二版（`2d9cc2c`）經 Claude Code 獨立上下文 review，都判定 not pass；第三版（`752bf2f`）**有條件通過**：沒有 must-fix，7 項 should-fix 須在 C2b 動工前修進文件，本版已修。對照見第 12～14 節。
> - **Aaron 10/14 核准 D2、D4、D8～D12 照建議進行**（第 9 節）；C2a（只做參數化）開始。
> - Aaron 已批准第一版的 C2-D1～D7 照建議進行。後續各版**新增 C2-D8、C2-D9，並調整 C2-D2、C2-D4、C2-D9 的建議**；第三次 review 另外指出三個應交 Aaron 的選擇（D10～D12）。這些都已由 Aaron 確認（第 9 節）。
> - **通過前不寫 Policy、Store、Host 的程式。**

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
  - C2 的 v2 也**只限 fake 場景目錄**：live 沒有委託確認與本人核准的途徑，見 C2-D9。
  - 切成兩片：**C2a** 只做參數化，用既有測試證明 v1、A2、P1-b、Signer 行為不變；**C2b** 才做憑證（第 10 節）。
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

第二次 review 補充：
- **委託的版本**：`ConfirmDelegation`（pay.go L259-268）在 task 存在之前就執行，不帶版本，`DelegationID` 用寫死的 domain。改法：
  - `DelegationRequest` 帶場景的契約版本（sim.go L178）；
  - 恢復時（pay.go L739）以 body 的版本推導 ID；
  - `taskContract` 檢查 `d.Version == setup.ContractVersion`。
- **grant 的版本**：store/approvals.go L408、pay.go L497 寫死，改為 task 的版本。
- **舊的 `Submit`**：路由條件是 `(v1 != nil) != t.v1()`（ops.go L557）。若一律改成 `isPay()`，v2 的 order 會不帶出示就走進 `submitV1`。所以 **`Submit` 明確拒絕 v2 task，v2 只走 `SubmitV2`**，並有負例。
- **畫面**：`web/host.js` 至少 8 處（L245、428、486、498、611、631、717、886）；`web/adapter.js` 的 `reasonText`／`REASONS_V1`（L485）要加 `CREDENTIAL_*` 的文字。
- **位置更正**：前一版寫的 `runs.go` L150-155／L327-328，實際是 `internal/store/runs.go` L155／L328，不是 Host 的檔案。
- **Signer 的 challenge 標籤**：approvals.go L323-325 的標籤與 challengeRecord Version 1，對 v2 的 order 仍是 `pay/v1/…`。這沒有安全問題，因為 intent 本身已經依 domain 分開；文件記下這點，不改。

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
- v2 的 `ValidateEventOrder` 要求一個 task 的事件流只有一個版本，因為 genesis 的 domain 依版本而定。
- 實作時，v1 的 `only()` 也要把 `credential` 列入「已知但不允許」的欄位（events.go L80-81），測試才會真的拒絕它。

### 3.2 `apa.pay.v2` 的差異

- **Domain**：`agent-payment-authority/pay/v2`。
- **型別**：`Intent`、`PaymentBinding`、`PurchaseOrder` 的欄位與 v1 相同。
- **新增 Deny 理由碼**：`CREDENTIAL_INVALID`、`CREDENTIAL_REVOKED`、`CREDENTIAL_STATUS_UNAVAILABLE`。細分原因放在驗證紀錄的 `detail`，取自固定代碼表。
- **新事件 `CREDENTIAL_VERIFIED`**：payload 只有 `credential`；其他 v2 事件不得帶這個欄位。內容：
  - `result`：`valid`／`absent`／`invalid`／`status_unavailable`／`revoked`／`expired`；
  - `detail`、`challenge_id`；
  - `credential_schema`（就是外層版本，review 的 note）；
  - `issuer`、`holder`、`status_index`、`expiry`；
  - `status_list_version`、`status_list_issued_at`、`status_list_sha256`；
  - `proof_sha256`：只供追溯，不是重播的鍵；
  - `verifier`。
- 共用的 `contracts.EventPayload` 以 omitempty 指標加入這個欄位；M1 的 schema 標為 `{"not":{}}`，M1 的 `ValidateEvent` 拒絕它。
- **v2 新增的 Ask 收尾配對**（第六次核對的 finding 2）：`(withdrawn, CREDENTIAL_REVOKED)`、`(withdrawn, CREDENTIAL_STATUS_UNAVAILABLE)`、`(withdrawn, AUTHORIZATION_EXPIRED)`，用於第 7.1 節「核准時重新檢查不通過」。
  - `Spec` 改為每個版本各有一張 Ask 收尾表：v1 的表和現在的 `ValidateDecision` 逐字相同，**v1 仍然拒絕這三種配對**，並有測試。
  - C2a 交付的 v2 規格只有三個 Deny 理由碼，還沒有這張表。這是 C2b 的第一個提交；在那之前，沒有任何路徑能建立 v2 task。
- **`PolicyDecision` 不變。**
- **過期**：verifier 回 `result=expired`，Deny `AUTHORIZATION_EXPIRED`（既有的理由碼）。計畫驗收第 1 項是「憑證效期過了 → verifier 拒絕，帳上留痕」，所以紀錄上要寫 expired，不能寫 valid（第二次 review 的 should-fix 1）。`Rules` 仍會以同一個 expiry 檢查，兩者一致。

### 3.3 v2 的事件順序與恢復不變式（review S1）

- 每筆 v2 請購**恰有一個** `CREDENTIAL_VERIFIED`，位置在 `PURCHASE_PROPOSED` 之後、`POLICY_DECIDED` 之前，三者同一個交易。
- `result` 對應理由碼：
  - `absent` 一定是 Deny `CONFIG_MISSING`；
  - `invalid`、`revoked`、`status_unavailable` 一定是 Deny，理由碼各自對應；
  - `expired` 一定是 Deny `AUTHORIZATION_EXPIRED`；
  - Allow 或 Ask 一定是 `result=valid`，而且 `expiry > decided_at`。
- **challenge 的規則**（第三次 review 的 S1）：
  - `SubmitV2` 一律消耗 **Store 為這個 (task, purchase_order_id) 保存的那一列**，不消耗出示所宣稱的那一列；兩者不同時，就是 `detail=CHALLENGE_MISMATCH`。
  - 已發出但過期的那一列，在 `SubmitV2` 裡標為 `used`，並記錄 `detail=CHALLENGE_EXPIRED`。
  - **`challenge_id` 為 NULL，若且唯若** `result=absent`、`detail=HOLDER_MATERIAL_UNAVAILABLE` 或 `detail=CHALLENGE_MISSING`（這個 (task, PO) 沒有任何保存的 challenge）。
  - 這個 (task, PO) 只有 `expired` 的列、沒有 `issued` 的列時，也是 `CHALLENGE_MISSING`。
  - 其他結果的 `challenge_id` 都指向一列 `state=used` 的 challenge，而且它的 task、purchase_order_id 相符。intent_hash 相符的要求只適用於 `detail` 不是 `INTENT_MISMATCH`、`CHALLENGE_EXPIRED`、`CHALLENGE_MISMATCH` 的紀錄。
  - `verify.go` 容許出示失敗後留下的 `issued` 列（第 7 節第 5 步），只要它沒有被任何驗證紀錄引用。
- **以憑證理由撤回**：只出現在 v2；只出現在還沒有 `TASK_CANCELLED` 的事件流；`APPROVAL_WITHDRAWN` 的 payload 只帶 decision 與 session，**不帶核准者**（events.go L124-130；帶了就會被 `ValidateEvent` 拒絕而回滾）。
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
  4. **expired**：`now >= expiry`。
  5. 以上都通過時回 `valid`。
  
  憑證步驟在 `Rules` 之前，所以過期的憑證會先於 `ASSET_NOT_ALLOWED`、`MERCHANT_NOT_ALLOWED` 決定理由碼（policy.go L128-137）。裁決同樣是 Deny，只有理由碼不同；「兩者一致」指的是裁決。
- **與 C1 spike 的差異**（review S3）：
  - spike 在 verifier 內最先以 `CREDENTIAL_EXPIRED` 拒絕；C2 把過期放在撤銷之後，並對應到既有的 `AUTHORIZATION_EXPIRED`。
  - spike 只在成功時用掉 challenge；C2 只要送出就用掉，因為這筆請購已有裁決。

## 5 角色、金鑰、信任設定（C2-D3）

- **目錄**：`--credential-dir`，必須是 0700、擁有者為目前使用者、不是 symlink。這比 Store 的 `dbPath`（store.go L270-296，只檢查權限，而且會跟隨 symlink）**更嚴格**。目錄裡的每個金鑰檔也一樣檢查擁有者，並且不跟隨 symlink。內容：
  - `issuer.key`：BBS+ 私鑰，0600；
  - `status.key`：Ed25519，簽狀態清單，0600，和 issuer 金鑰分開；
  - `status.pub`：狀態清單的公鑰。verifier 讀取後，必須符合 task snapshot 裡的指紋才使用；
  - `holders.json`：每個 consumer 的 holder 公鑰，也就是信任設定；
  - `holder/<consumer>.key`：0600；
  - `holder/<task>.cred`：0600。
- **`apa-issuer init --consumers a,b`** 產生測試金鑰與 `holders.json`，並且要求每個 consumer 的 holder 公鑰互不相同（review S10）。Host 載入 `holders.json` 時再檢查一次唯一性。
- **StartTask（v2）** 時，把信任設定（issuer ID、issuer 公鑰、status 公鑰的指紋、這個 consumer 的 holder 公鑰）與發出的憑證中繼資料，一起寫進 `credential_bbs` snapshot。之後改檔不影響已開始的 task。
- **發證的期限**：`expiry` = task 開始時間 + 憑證 TTL（預設 24 h，上限 24 h）。
  - **不**和 `Delegation.ExpiresAt` 綁在一起。後者只是「必須在這之前開始 task」的期限（pay.go L202），場景設定是 1800 s；若拿它當憑證期限，晚 29 分鐘開始的 task，憑證約 1 分鐘後就會過期（第二次 review 的 should-fix 6）。
  - 為了負例，issuer 提供只在測試中可用的短期限注入，不是產品旗標。
- **發證的時機與順序**：Store 不持有 issuer 金鑰。
  1. Host 在 `status.lock` 下，從 `--credential-dir/status.counter` 保留一個狀態索引：計數器加一、fsync 之後才釋放鎖。`status.json` 存在、但 `status.counter` 不存在或毀損時，**拒絕發證**，不從 0 重新開始（第六次核對的 finding 1）；
  2. 以 issuer 金鑰發證；
  3. 以暫存檔加改名的方式寫入 `holder/<task>.cred`；
  4. 再呼叫 `StartTask`，連同 snapshot 一起寫入。
  
  `StartTask` 失敗時，索引會留下空號（計數器不回捲），`.cred` 檔留作孤兒、下次以同一個 task ID 覆寫。這兩者都不會讓不同憑證共用同一個索引。
- **誠實標示**（review S5）：這些都在同一台主機、同一個程序。主持人的程序可以自行發證、出示、更新清單。C2 證明的是格式、綁定、撤銷與裁決的接線，**不是** issuer、holder、verifier 彼此獨立的保證。分程序部署另做。

## 6 撤銷：簽章的狀態清單（C2-D4，本版調整建議）

- **`status.json`**：`{version, issuer, list_version, issued_at, next_update, revoked:[…]}`，以 `status.key` 簽章。
- **寫入規則**（第二次 review 的 N3：兩個寫入者會把撤銷蓋掉）：
  - 每次簽發，不論是 `revoke` 還是更新，都先取得 `status.lock` 的獨占鎖（flock），**重新讀取目前的檔案並驗簽**；
  - **目前的檔案驗不過（不存在、毀損、簽章錯）時，拒絕簽發**，不當成空清單；只有 `apa-issuer init` 建立第 1 版（第三次 review 的 S4）。`status.json` 已存在時，`init` 拒絕執行，避免版本被重設回 1；
  - 在讀到的撤銷集合上做修改：只能加入，不能移除；
  - `list_version` 加一後簽章；寫暫存檔、fsync 暫存檔、改名、fsync 目錄，然後才釋放鎖；
  - Host 的自動更新與 CLI 的 `revoke` 都走同一個函式，所以撤銷不會被舊的集合覆蓋。
- **狀態索引的配發也在同一把鎖下**：計數器存在 `--credential-dir` 的 `status.counter`，和清單放在一起（第三次 review 的 S6）。如果計數器放在 Store 裡，換資料庫或兩個 Host 共用同一個目錄時，索引會重新從 0 開始，撞到已撤銷的舊索引，使合法的持有人被拒絕。
- **每次驗證、每次核准或付款前的重新檢查，都重新讀取 `status.json` 並驗簽，不使用任何快取**（第三次 review 的 S3）。否則在自動更新之下，CLI 的撤銷最多 300 s 內不會生效。
- **驗證時**，以下任一項都回 `status_unavailable`：
  - 檔案不存在、讀到不完整的檔案、簽章無效；
  - `issuer` 不符；
  - `now >= next_update`；
  - `next_update − issued_at > 600 s`：新鮮期有上限，review S4；
  - `issued_at > now + 60 s`：時鐘偏差上限；
  - **`list_version` 小於 Store 記錄的最高版本**：防止換回撤銷前的舊清單；
  - **版本相同但 SHA-256 不同**：同一版本出現兩種內容。Store 的 `credential_status_seen` 記錄 `(max_list_version, sha256)`，每次驗證與重新檢查時讀取並更新（第三次 review 的 S2）。
- **防回捲的範圍**：只能擋住 Store 已經看過的版本。若撤銷後的新版本還沒被任何驗證讀到，就被換回舊版，在新鮮期（≤ 600 s）內仍會通過。文件與畫面照實寫明。
- 使用的清單版本、`issued_at` 與 SHA-256，寫進驗證紀錄與事件（review S4）。
- **更新方式（C2-D4 的選項）**：
  - (a) **Host 在執行中每 300 s 自動由 issuer 更新**（新建議）。issuer 本來就在同一個程序，自動更新不會降低保證（第 5 節）；它避免了「超過 600 s 後每筆請購都被 Deny、task 結束」的操作成本。`revoke` 仍需明確執行，而且依上面的寫入規則不會被自動更新蓋掉。在 (a) 之下，「issuer 停止 → 拒絕」只能用測試的時間注入或刪除檔案來示範。
  - (b) 只能手動 `apa-issuer refresh`（第一版的建議）：保留了「issuer 停止就 fail closed」的展示效果，但主持人每 10 分鐘要操作一次。
  - 不論哪一個，都可以用測試的時間注入，示範「issuer 停止 → `CREDENTIAL_STATUS_UNAVAILABLE`」。

## 7 請購流程與 challenge（review M3、M4）

報價的 HTTP 讀取與 order 組裝**在鎖外**（鎖內無法被 `Disarm` 中斷，relay.go L81-90）。以下各步在 `Binding.Commit` 的鎖裡依序執行，所以 run 結束後不會再寫入，同一個 task 的請購也會逐一處理：

1. **先看已存紀錄**：鎖內再查一次這筆 purchase 是否已存在；有就直接回原紀錄，不取 challenge。重複的工具呼叫因此不會影響 task。
2. 使用鎖外已組裝好的 order（和現行 shop.go L113-124 相同，組裝不在鎖內）。在 Store 裡先檢查契約、ownership、`offered`、`CheckTaskActive`（review：這些都在 challenge 之前）。
3. **holder 狀態**：
   - 開始 task 時，信任設定裡**沒有登記這個 consumer 的 holder**，就不發證，task 以 absent 開始（第 8 節的表示方式）。請購時不取 challenge，以 `result=absent` 送出，Deny `CONFIG_MISSING`；snapshot、事件與裁決三者一致（review M3）。
   - 有登記，但 holder 材料**不見、毀損、讀不到或和登記的公鑰不符**：以 `result=invalid`、`detail=HOLDER_MATERIAL_UNAVAILABLE` 送出，Deny `CREDENTIAL_INVALID`。這是重試也不會改變的狀況，所以留下裁決，避免無限重試（C2-D12）。
4. `Store.IssueChallenge(task, purchase_order_id, intent_hash, disclose)`：
   - 以 **(task_id, purchase_order_id)** 為鍵，**最多一個**未用的 challenge（review M4：intent 含時間，不能當上限的鍵）；
   - 已有未用、未過期、**而且 intent_hash 相同**的 challenge 就沿用；intent 不同（例如前一次在送出前失敗、這次重試時時間已變），就把舊的標為 `expired` 再發新的（第二次 review 的 N1）。因為整串步驟都在同一把鎖裡，這樣做是安全的；
   - 過期的一律標為 `expired`；
   - 有效期 60 s。
5. holder 出示。材料都正常、但**產生 proof 的過程出錯**，屬於主機內部的暫時錯誤，不是憑證的問題：不送出、不留裁決、不 Deny，工具回傳錯誤；challenge 留在 `issued`，下次依上一步的規則處理（C2-D12：這縮小了 D2「Deny 並留紀錄」的範圍）。
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

### 7.1 核准與付款時重新檢查（第二次 review 的 should-fix 7）

- Ask 等待核准期間，憑證可能被撤銷或過期。現在的 A2 核准（store/approvals.go L402-410）、模擬核准（pay.go L497）與 P1-b 付款（payment.go L97-113）都不會重新檢查。
- v2 的規則：
  - 核准落地前，以這個 task 的 `credential_bbs` snapshot 與**當下**的狀態清單重新檢查撤銷、狀態可得性與期限；
  - 只在**核准**時重新檢查，拒絕不檢查；
  - 不通過時的處理是 C2-D11，建議 (b)：**以既有的撤回流程關閉會話**（`APPROVAL_WITHDRAWN`，v2 允許它帶 `CREDENTIAL_REVOKED`、`CREDENTIAL_STATUS_UNAVAILABLE` 或 `AUTHORIZATION_EXPIRED` 作為原因），不發 grant。
    - A2 路徑：`ResolveWebAuthn` 回傳既有的 `closed` outcome，Host 依現有邏輯 abandon Signer 的核准（cmd/demo/approvals.go L458）。不能用回傳錯誤的方式處理：那會讓交易回滾、Host 當成 store_unavailable，也不會 abandon（第三次 review 的 S5）。
    - 模擬核准路徑：`ResolveApproval` 回傳 `closed`。
    - 兩條路徑都留在事件鏈裡，不需要新的資料欄；
    - 呼叫 `t.close(p, "APPROVAL_WITHDRAWN", "withdrawn", <理由>, nil)`，也就是 `end()` 用的同一個基本操作（ops.go L753），不帶核准者，`resolve_request_id` 維持未設定；
    - 在 approve 分支裡，這個檢查放在「報價已改變（superseded）」與「Signer 的核准已過期」兩種 closed 之後；
    - 畫面不能沿用「已撤回（task 取消）」的文字，見第 10 節步驟 8。
  - 若選 (a)「會話維持原狀到逾時」：A2 只能記在 attempt（`verified` 加 `error_code`，或 `refused` 且沒有 `signer_approval_id`），模擬路徑則需要 schema 6 新增欄位，否則無處記錄；
  - P1-b 付款開始前做同樣的檢查，不通過時以 `refuse()` 回傳 `CREDENTIAL_REVOKED`、`CREDENTIAL_STATUS_UNAVAILABLE` 或 `AUTHORIZATION_EXPIRED`，**什麼都不寫入**。此時會話已經是 APPROVED，不能再撤回（events.go L292-295），而且交易會回滾，`credential_status_seen` 的更新也一起撤銷，這不影響安全。付款目前仍未啟用，先以測試涵蓋。
- 「核准等待中被撤銷」是自然的展示情境，列入第 11 節的負例。

## 8 Store schema 6（review S6）

- **新表**：
  - `credential_challenges(challenge_id PK, task_id FK, purchase_order_id, intent_hash, disclose, verifier_id, issued_at, expires_at, state CHECK('issued','used','expired'))`，以 partial unique index 保證每個 `(task_id, purchase_order_id)` 最多一個 `issued`；
  - `credential_verifications(task_id, purchase_order_id, challenge_id FK NULL, result, detail, issuer, holder, status_index, expiry, status_list_version, status_list_issued_at, status_list_sha256, proof_sha256, verifier, verified_at, UNIQUE(task_id, purchase_order_id))`；
  - `credential_status_seen(issuer PK, max_list_version, sha256)`；
  - 計數器不在 Store，見第 6 節。
- **重建**：`config_snapshots`（kind 加 `credential_bbs`）、`tasks`（`contract_version` 加 `apa.pay.v2`）、`host_runs`（同上）三張表的 CHECK。
  - 做法（第二次 review 的 should-fix 3：`PRAGMA foreign_keys=OFF` 在交易裡不生效）：
    1. 在唯一的寫入連線上，**交易開始之前**執行 `PRAGMA foreign_keys=OFF`；writer 的 DSN 每次連線都會設 `foreign_keys(1)`（store.go L196、L200）；
    2. `BEGIN` → 建新表 → 複製資料 → 刪舊表 → 改名 → 重建索引與觸發器；
    3. 交易內執行 `PRAGMA foreign_key_check`，必須沒有任何結果，否則回滾；
    4. `COMMIT` 之後恢復 `foreign_keys=ON`。
  - `tasks` 是 purchases、events、delegations 的外鍵目標；`config_snapshots` 也是 `tasks` 的外鍵目標。
  - 遷移前備份資料庫檔；遷移中途失敗就不提交。
  - 用 `s.w.Conn(ctx)` 固定同一條連線，並在 `BEGIN` 前讀回 `PRAGMA foreign_keys` 確認是 0。若 database/sql 中途重新連線，DSN 會把外鍵打開，`DROP TABLE tasks` 會失敗，這是安全的，但要有明確的錯誤訊息。
  - 全新的資料庫也會跑整條 DDL（store.go L477-483），所以重建步驟必須在空表上也安全。
- **委託的 request_id**：`delegations.request_id` 是跨版本唯一。v2 的確認若重用 v1 的 request ID，對應為 `IDEMPOTENCY_CONFLICT`，不能讓原始的 unique 錯誤以 store_unavailable 傳到頁面（sim.go L184）。
- **`tasks.credential_snapshot`** 在 v2 指向 `credential_bbs` 種類的 snapshot；`decodeSnapshot`（ops.go L112）、`settings()`（L674-678）、`verify.go` L166-172 依版本解碼。
- **absent 的表示方式**沿用現有的：`credential_snapshot` 為 NULL，`terms.credential_state="absent"`（第二次 review 的 M3）。
  - `taskContract`（pay.go L207）目前拒絕沒有憑證的 pay 系列 task，改為**只對 v2** 允許 absent；
  - `verify.go` L156 的交叉檢查依版本明確處理。
- **v2 task 帶 fixture 的 `credential` snapshot 視為毀損**，對應的負例在第 11 節。

## 9 需要 Aaron 決定的事

Aaron 10/14 已批准第一版的 D1～D7 照建議進行。下表標出**本版有變動、須再確認**的項目。

| # | 決定 | 選項 | 建議 | 本版狀態 |
|---|---|---|---|---|
| D1 | 契約升版方式 | 新增 `apa.pay.v2`；或在 v1 上直接加碼（違反 §3.1） | 新增 v2 | 已批准，不變 |
| D2 | 憑證失敗的語意 | (a) Deny 並留紀錄，task 結束；(b) 當成裁決前的錯誤，不留裁決；(c) 只把 `STATUS_UNAVAILABLE` 當成暫時、不結束 task 的拒絕，並留稽核紀錄 | (a)，**前提是採 D4(a)**：自動更新下 status 不可用的情況很少。若 D4 維持 (b) 手動，則建議 (c)，否則錯過一次更新就會讓 task 結束 | **已核准（D4 採 (a)）** |
| D3 | 角色放在哪 | 同一程序分套件，或分程序 | 同一程序，並誠實標示（第 5 節） | 已批准；本版補上誠實標示 |
| D4 | 撤銷清單的更新 | (a) Host 每 300 s 自動更新；(b) 只能手動 | **改建議為 (a)**（第 6 節） | **已核准** |
| D5 | `c0` 在 Z2 之前 | (a) 全零佔位，不揭露、不參與裁決；(b) 現在就放「已核准委託」的摘要，綁定已核准的條件 | (a)：C₀ 在計畫中是 Z2 的預算承諾，若現在挪作他用，Z2 時要改變它的意義；委託仍由 Store 的 snapshot 約束 | 已批准；本版補上 (b) |
| D6 | 正式依賴 | idemix v0.3.1 加入主 `go.mod`，go 指令升到 1.26.3 | 照做 | 已批准，不變 |
| D7 | 畫面 | 卡片與抽屜各加一列 | 照做；但 `web/host.js` 至少 8 處「非 V1 就當 M1」要一併改，`reasonText` 也要加文字，工作量比「一列」大 | 已批准；補上工作量 |
| **D8** | **v2 與核准、付款、委託的關係**（review M1） | (a) `paycontract` 依版本參數化，Signer、A2、P1-b、委託、grant 都在 C2 內支援 v2；(b) v2 只能用模擬核准、不能付款，另開一片補；(c) 不新增版本，在 v1 上加碼 | **(a)**：否則真正能核准與付款的契約只有 fixture 憑證，E1 的「同一個 intent_hash 串起憑證、裁決、核准、proof、交易」做不到 | **新增，已核准** |
| **D9** | **v2 可否用在 live，以及 `evidence_mode`** | (a) v2 在 fake 與 live 都可用；(b) **C2 的 v2 只限 fake**，live 之後另開一片 | **改建議為 (b)**（第二次 review 的 N2）：live 模式沒有模擬核准者（`ui.sim` 只在非 live 時建立，cmd/demo/main.go L367-372），委託只能經 `/api/sim/delegations` 確認且必須是 simulated（sim.go L178、grant.go L64），A2 也只限 fake（main.go L181-182）。所以 live 的 v2 拿不到委託，Ask 也沒有核准者。(a) 等於取消先前核准的 P1-a 與 A2 兩個界線，那需要另外決定。`evidence_mode` 沿用現況（描述付款證據層，付款仍未啟用） | **新增並改建議，已核准** |

| **D10** | **憑證效期**（第三次 review 的 D-a） | (a) task 開始後 24 h，另外由 `CheckTaskActive` 確保只在 task 進行中可用；(b) 照計畫字面「效期 ≤ task 生命」，以 run 的時限加核准期限為效期 | **(a)**：計畫 L155 寫「效期 ≤ task 生命」，驗收第 23 項也依賴它。(a) 的憑證本身較長，但使用時一定綁定 task_id，而且 task 必須仍在進行，實際可用期間仍不超過 task 生命。(b) 的效期只有幾分鐘，run 結束後才落地的核准會被判過期 | **新增，已核准** |
| **D11** | **核准等待中憑證被撤銷或過期**（D-b） | (a) 拒絕這次核准，會話維持到逾時，紀錄只留在 attempt；(b) 以撤回流程關閉會話，原因碼為 `CREDENTIAL_REVOKED` 等，Ask 就此結束 | **(b)**：兩條核准路徑都留在事件鏈裡，不需新欄位；畫面能直接顯示「憑證已撤銷，這筆已關閉」（第 7.1 節） | **新增，已核准** |
| **D12** | **出示失敗的語意**（D-c） | (a) 材料問題（不見、毀損、不符）→ Deny 留紀錄；產生 proof 的暫時錯誤 → 工具回錯誤、不留裁決；(b) 兩者都 Deny | **(a)**：材料問題重試也不會好，留裁決可以避免無限重試；暫時錯誤若也 Deny，會讓偶發錯誤結束 task。這縮小了 D2「Deny 並留紀錄」的範圍 | **新增，已核准** |

**Aaron 的決定（10/14）**：「針對這 7 項我核准依照建議的方式進行處理」。
- D2：(a)；
- D4：(a) 每 300 s 自動更新；
- D8：(a) 參數化，拆成 C2a 與 C2b；
- D9：(b) C2 只限 fake；
- D10：(a)；
- D11：(b) 撤銷時關閉會話；
- D12：(a)。

加上先前批准的 D1、D3、D5、D6、D7，C2-D1～D12 都已定案。

**之後的決定（不在 C2，先標明）**：驗收第 26 項要求在 x402 resource server 那一層出示，而且兩次出示對 server 不可串連。C2 在 Policy 這一層揭露 holder 公鑰，所以可以串連；這符合第 12 項「對自家 policy gate 不 unlinkable」。到 P2 做 x402 的 credential extension 時，需要另一種不揭露 holder、task、status 的出示方式，屆時可能要採用 C1-D2 的 (b)（blind signing）。

## 10 實作切片（核准後；拆成 C2a 與 C2b）

- **C2a 只做契約的依版本參數化與 Signer**（步驟 2，以及步驟 3 中 Signer 的部分）。用既有的 v1、A2、P1-b、Signer 測試與反向證明行為不變，交 review。
- **C2b 做其餘部分**（步驟 1、步驟 3 中 Store／Host／畫面的切換、步驟 4～10），以 C2a 通過 review 為前提。
- **實作時的調整（10/14）**：步驟 1（idemix 依賴）與步驟 3 中 Store、Host、畫面的切換移到 C2b。原因是 C2a 還無法建立 v2 task，這些切換在 C2a 裡無法用 v2 的資料測試；放到 C2b，就能用真正的 v2 task 測試，不留下未經測試的程式。

| 步驟 | 內容 |
|---|---|
| 1 | 依賴提交（idemix、go 指令）與授權紀錄 |
| 2 | `paycontract` 依版本參數化；v1 規格逐字凍結；v1 拒絕 `credential` 欄位；`Supported()`；v2 golden。M1、v1 的測試全部不改並通過 |
| 3 | 把 Signer、Store（A2、P1-b、委託、恢復檢查）、Host、`web/host.js`、`web/adapter.js` 的「只認 v1」改為「pay 系列」；以 v1 的既有測試證明行為不變 |
| 4 | `internal/credential`：issuer、holder、verifier、外層、狀態清單；從 spike 搬入規則並加上第 4 節的差異；單元測試與反向 |
| 5 | Store schema 6 與遷移；新表；`IssueChallenge`、`SubmitV2`；依版本的恢復檢查與第 3.3 節的不變式 |
| 6 | `paypolicy` v2 的憑證步驟 |
| 7 | Host 與 Shop：發證、`Commit` 內依序取 challenge、出示、送出；`apa-issuer`；`--credential-dir`；自動更新（若採 D4(a)）；**fake** 場景目錄允許 v2（scenarios.go L98） |
| 8 | 畫面：卡片與抽屜的「憑證檢查」列，`host.js` 對 v2 的判斷；**依 `decision.reason` 決定「已撤回」的文字**：`HOST_CANCELLED` 沿用「已撤回（task 取消）」，憑證理由改為「憑證已撤銷（或狀態無法確認、已過期），這筆已關閉」。要改的地方：`web/host.js` 的兩張結果表（L864、L1107）與 L1029，`web/adapter.js` L148、L284（第六次核對的 finding 3） |
| 9 | 文件：roadmap、transaction-flow §3.1／§10、`docs/policy.md`、操作說明 |
| 10 | 定向驗證計畫、反向、瀏覽器檢查，證據在 `artifacts/c2/` |

## 11 驗收

**對照計畫的驗收項目**
| 計畫驗收 | C2 涵蓋 | 不在 C2 |
|---|---|---|
| 1 憑證效期過了 → verifier 拒絕，帳上留痕（執行計畫 §5 L720） | `result=expired` → Deny `AUTHORIZATION_EXPIRED`，留下驗證紀錄與事件。另外，第 6 節展示表的第 1 項（存進本地 keystore）：發證並存入 holder 區（測試 issuer、測試金鑰） | 「核准上限 $5」寫進憑證，屬 C₀ 與 Z2；eKYC 屬第二期 |
| 2 已撤銷的憑證被拒絕 | `CREDENTIAL_REVOKED`，含核准等待中被撤銷（D11）。計畫寫的是「Status List 打開該位元」；本設計用簽章的 `revoked:[…]` 集合，意義相同 | — |
| 3 偽造的出示被拒絕並記錄 | `CREDENTIAL_INVALID` 與驗證紀錄、事件 | — |
| 4 超過憑證上限的請購 | — | Z2（上限在 C₀） |
| 12 不可串連 | 文件寫明：對自家 policy gate **可串連**（揭露 holder），符合計畫原文 | 對 x402 resource server 不可串連 → 第 26 項，P2 |
| 26 credential extension | — | P2（x402 層） |
| 28 credential → commitment | 把 A 的憑證拿去 B 的 task → `CREDENTIAL_INVALID`（snapshot 與 holder 比對） | `registerTask` 與 C₀ 的鏈上部分 → Z2、P2 |
| 29 presentation 重播 | 跨 verifier、舊 challenge、把憑證複製到別的 holder 出示 → 全部拒絕並記錄 | policy／human grant 的重播：既有 A2 與 P1-b 已涵蓋；C2 以 v2 再跑一次 |
| 38 遮罩掃描 | 驗收時掃描畫面事件、log 與資料庫，確認 **issuer.key、status.key、holder/*.key 的位元組與 `.cred` 裡的簽章**從未出現。`consumer_id` 是公開的系統識別碼，本來就在每個事件的 scope 與頁面上；「不揭露」只指它不在 proof 的揭露欄位裡。`c0` 是全零佔位，掃描它沒有意義。計畫第 38 項其餘部分（完整 grant 或 token、HTML／log 注入）沿用既有的檢查 | — |
| 35 撤銷 fail-closed | 清單抓不到、簽章錯、換回舊版清單 → 拒絕並記錄 | — |

**測試清單**，每一項都核對理由碼、`detail`、驗證紀錄、事件鏈與 snapshot 一致：
- 正例：v2 的 Allow、Ask（並以 A2 的 WebAuthn 核准走完）與依金額的 Deny，都可沿 `intent_hash` 追溯。
- 負例：
  - 沒有登記 holder → `CONFIG_MISSING`（absent）；holder 材料不見 → `CREDENTIAL_INVALID`；
  - 過期 → 驗證紀錄 `expired`、`AUTHORIZATION_EXPIRED`；撤銷 → `CREDENTIAL_REVOKED`；
  - **核准等待中被撤銷或過期** → 核准被拒、沒有 grant（第 7.1 節）；
  - 狀態清單：CLI 撤銷後 Host 自動更新，撤銷仍然存在；同版本不同內容 → 拒絕；
  - `Submit`（舊路徑）收到 v2 task 的 order → 拒絕；
  - 重試：取 challenge 後送出失敗，60 秒內以新的 intent 重試 → 發新的 challenge，不會被 Deny；
  - 產生 proof 失敗 → 工具回錯誤，沒有裁決；
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

## 13 第二版 review 對照（`2d9cc2c`，not pass）

| review | 處理 |
|---|---|
| N1 沿用 challenge 帶著舊 intent | §7 第 4 步：intent 相同才沿用，否則作廢再發；第 5 步：出示失敗不送出 |
| N2 D9 的 live 做不到 | D9 改建議為 (b) 只限 fake，並列出依賴 |
| N3 自動更新會蓋掉撤銷 | §6 的寫入規則（flock、重讀、只增不減、版本加一）；Store 記錄 (版本, SHA-256) |
| should-fix 1 驗收第 1 項對照錯誤、過期寫成 valid | §3.2、§4 加入 `expired`；§11 更正 |
| should-fix 2 沒有 challenge 的紀錄 | §3.3 的 NULL 規則 |
| should-fix 3 PRAGMA 順序 | §8 |
| should-fix 4 委託的版本來源 | §2.2 |
| should-fix 5 舊 `Submit` 的風險 | §2.2：`Submit` 拒絕 v2 |
| should-fix 6 憑證期限的來源 | §5：task 開始時間 + TTL，不綁委託期限 |
| should-fix 7 核准與付款時不重新檢查 | 新增 §7.1 |
| should-fix 8 D2 依賴 D4 | §9 的 D2 改為有條件再確認 |
| should-fix 9 漏列的分支 | §2.2、D7 |
| should-fix 10 切片太大 | §10 拆成 C2a、C2b |
| M3 absent 的表示方式與既有檢查衝突 | §8：沿用 NULL 加 `terms` absent，只對 v2 放行 |
| notes | §5（目錄規則比 dbPath 嚴格；發證時機與順序；載入時檢查唯一性）、§2.2（Signer 標籤）、§3.1（每個事件流只有一個版本；`only()` 列入 `credential`）、§7（報價讀取在鎖外）、§6（D4(a) 下的示範方式） |

## 14 第三版 review 對照（`752bf2f`，有條件通過）

| review | 處理 |
|---|---|
| S1 challenge 規則互相矛盾 | §3.3：消耗 Store 保存的那一列；過期列在送出時標 `used`；新增 `CHALLENGE_MISSING`、`CHALLENGE_EXPIRED`、`INTENT_MISMATCH`、`CHALLENGE_MISMATCH`；NULL 規則；容許留下的 `issued` 列 |
| S2 Store 沒有記錄摘要 | §6、§8：`credential_status_seen` 加 `sha256`；重新檢查時也讀取與更新 |
| S3 每次重新讀檔的規則被漏掉 | §6：每次驗證與重新檢查都重讀、不快取 |
| S4 清單缺漏或毀損時的寫入 | §6：拒絕簽發；只有 init 建第 1 版；fsync；寫明防回捲的範圍 |
| S5 核准時重新檢查的落點 | §7.1 與 D11：以撤回流程關閉會話；A2 回傳 `closed` 讓 Host abandon |
| S6 計數器與清單分處兩地 | §6：計數器放在憑證目錄、同一把鎖 |
| S7 驗收對照漏了 2、3、4、38 | §11 |
| D-a、D-b、D-c | §9 的 D10、D11、D12 |
| notes | §4（理由碼的先後）；§7 第 2 步（組裝在鎖外）；§8（固定連線、空資料庫、委託 request_id）；§10 第 7 步（只限 fake）；`ResolveApproval` 的 `!t.v1()`（pay.go L453）與 `paypolicy.Decide`（L34／L74）在改為 `isPay()` 時一併處理 |

## 15 C2a 交付（10/14，Claude Code；待 Claude Code 獨立上下文 review）

**做了什麼**（`a9de749`、`c086925`、`c88b18d`）：
- **`paycontract/version.go`**：`Spec`、`SpecOf`、`Supported`，以及 `V2`、`DomainV2`、`CredentialReasons`。
  - v1 的規格就是現有的常數，逐字不變；
  - v2 有自己的 domain，另外多三個 Deny 理由碼。
- order、裁決、委託、grant、事件都依**自己的版本欄位**檢查與計算摘要。
- 事件流規則：
  - 一個 task 的事件流只能有一個版本；
  - 事件所指的 order、事件內的裁決，都必須和事件本身同版本。
- 套件層的 `ResourceHash`、`RequirementHash`、`DelegationID`、`SessionID`、`GrantID`、`GenesisHash` 維持 v1，所以既有的呼叫端不用改。
- **Signer** 依 order 的版本推導會話 ID 與 jti；`signertest.Order` 可以產生 v2 的 order。
- **v2 golden 值已凍結**（`TestV2GoldenVectors`），每一個都和 v1 不同。

**沒有做的，移到 C2b**（第 10 節）：
- Store、Host、畫面的切換；
- `CREDENTIAL_VERIFIED` 事件與共用 payload 欄位，以及 v1 拒絕這個欄位；
- idemix 依賴。

C2a 階段沒有任何路徑能建立 v2 task。

**證據**（`artifacts/c2/`；本機、不在容器中）：

| 項目 | 基準 | 結果 | 檔案 |
|---|---|---|---|
| `go test -count=1 -race -skip ^TestTimingInterleaved$ ./...` | `c086925` | 24 個套件 ok，exit 0。v1 golden（`TestGoldenVectors`）、M1 向量、A2、P1-b、Signer 的既有測試都沒有修改，全部通過 | `c2a-c086925/go-test-race.txt` |
| 反向 `^C2a:` 4 項 | `c086925` | **3／4**。「憑證理由碼只屬於 v2」**沒有抓到**：突變後 `sp` 沒有被用到，建置失敗（紀錄保留） | `c2a-c086925/reverse.txt` |
| 上一項改為保留 `sp` 的突變後單獨補跑 | `c88b18d` | 1／1，`apa.pay.v1 CREDENTIAL_REVOKED: <nil>` | `c2a-c88b18d/reverse-reasons.txt` |

`c88b18d` 只改了 `scripts/verify/m-b-reverse.py`，所以 `c086925` 的測試結果適用於它。

## 16 第四版設計的條件核對（10/14，Claude Code 獨立上下文）

**判定**：S1～S4 已關閉；D10～D12 已交 Aaron 決定並經核准。S5、S6、S7 各有一處遺留，加上 4 項文字層的 should-fix，**本版已修正，修正後 C2b 可以開始**；reviewer 認為不需要再做一次完整 review。

| 核對 | 處理 |
|---|---|
| finding 1 第 5 節仍寫計數器在 Store | §5 改為在 `status.lock` 下從 `status.counter` 保留；計數器不見或毀損時拒絕發證 |
| finding 2 v2 撤回配對只寫在括號裡 | §3.2：每個版本一張 Ask 收尾表、v1 仍拒絕；§3.3：不變式；C2b 的第一個提交 |
| finding 3 畫面文字會寫成「task 取消」 | §10 步驟 8：依理由碼決定文字，列出要改的地方 |
| finding 4 第 38 項的對照做不到 | §11：改為掃描私鑰與簽章的位元組 |
| notes | `status.pub` 與指紋（§5）；P1-b 不通過時的代碼、什麼都不寫入（§7.1）；只有 expired 列時也是 `CHALLENGE_MISSING`（§3.3）；`init` 不覆寫既有清單（§6）；撤回的 payload 不帶核准者、在 switch 中的位置（§3.3、§7.1）；第 2 項的位元與集合（§11）；狀態欄位的文字（§9） |

## 17 C2a 的獨立上下文 review 與修正（10/14）

**review**（`651e7df..c88b18d`）：判定 **pass**，沒有 must-fix。
- v1 逐字不變：既有的 v1 golden 測試沒有修改而且通過；各種摘要的輸入與 domain 都和原本相同；舊反向項目的錨點也都還在。
- v2 不會被當成 v1：所有依版本取規格的地方都先檢查過版本。

**3 項 should-fix 與 note 的修正**（`a26f4a4`、`29d310c`）：
- **should-fix 1**：事件的 order、裁決、grant ID 必須和事件同版本，這三個檢查原本沒有測試。新增 `TestEventCrossVersion` 與 3 項反向。
- **should-fix 2**：新增測試，證明 Signer 存下的 jti 是 v2 的（`TestV2OrderChallenge` 改為讀取 `approval_challenges.jti`），並新增反向。
- **should-fix 3**：C2a 讓 `Validate` 接受所有 pay 系列版本，原本只認 v1 的呼叫端因此被默默放寬了。現在把它們釘回 v1：
  - 存下的委託必須是 `apa.pay.v1`（`store/pay.go`）；
  - `paypolicy.Decide` 只裁決和 task 同版本的請購。
  
  兩者各有測試與反向；C2b 會改為依 task 的版本。
- **note 1**：同一個 request ID 不得指向兩個版本的 order（`CompareRetry`）。
- **note 2**：凍結一組 v2 事件鏈雜湊（`TestV2EventChainGolden`）。
- 其餘 note 列為 C2b 的工作：錯誤訊息裡仍寫 `apa.pay.v1` 的地方、`Spec` 的欄位要不要不匯出、`signertest` 忽略 `SpecOf` 的 ok、Signer 的 Display 沒有顯示契約版本。

**證據**：

| 項目 | 基準 | 結果 | 檔案 |
|---|---|---|---|
| `go test -count=1 -race -skip ^TestTimingInterleaved$ ./...` | `a26f4a4` | 24 個套件 ok，exit 0 | `artifacts/c2/c2a-a26f4a4/go-test-race.txt` |
| 反向 `^C2a:` 11 項，同一個提交 | `a26f4a4` | **10／11**。「grant 事件的 ID 屬於自己的版本」**沒有抓到**：突變後 `sp` 沒有被用到，建置失敗（紀錄保留） | `c2a-a26f4a4/reverse.txt` |
| 上一項改為保留 `sp` 的突變後單獨補跑 | `29d310c` | 1／1 | `c2a-29d310c/reverse-grant.txt` |

`29d310c` 只改了反向腳本。

**下一步**：C2a 通過；第四版設計的條件已修正（第 16 節）。開始 C2b，第一個提交是 v2 的 Ask 收尾表。

## 18 C2b 交付（10/14，Claude Code；獨立上下文 review pass，見 commercial-c2.md 第 8 節）

實作內容、與本設計不同的地方、fake 操作步驟與驗證結果都寫在 [commercial-c2.md](commercial-c2.md)。這裡只記與設計各節的對應：

- 第 3 節（契約）：v2 的 Ask 收尾表 `879f96e`；`CREDENTIAL_VERIFIED` `64e917e`。
- 第 4～6 節（憑證、目錄、狀態清單）：`e356c30`；依賴 `9a6cb1a`；`IssueTask`、`Present` 與 `InitDir` 的 consumer 檢查 `f62d389`。
- 第 7 節（請購流程與 challenge）：`eddedc7`；第 7.1 節（核准與付款時重新檢查）：`c9dad4b`。
- 第 8 節（schema 6）：`0d18d29`；v2 task 啟動 `b04aebb`；第 3.3 節的恢復核對 `b8fb9b9`。
- 第 10 節步驟 3、6：Store 依版本與 paypolicy v2 `4ebbc00`；步驟 7（Host、Shop、`apa-issuer`、自動更新）`f62d389`；步驟 8（畫面）`1c519da`；步驟 10（驗證）見 commercial-c2.md 第 6 節。

**review 通過前，不宣稱 BBS+ 已參與商業交易的可信准駁。**

