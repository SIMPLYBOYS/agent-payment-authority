# M1 共用資料契約

B1-02 交付 `apa.m1.v2` 的 Go 型別、JSON Schema、固定測試向量與事前案例清單。範圍只到請購／Ask／Deny；沒有 policy engine、HTTP 工具認證、持久化事件服務、Signer 或付款。

**狀態（2026-10-01）**：**B1-02 已結案（Done）**，Aaron 10/1 決定，驗收 commit `2dc8d93`。範圍是本份規格、已接受的 `cases.json`、契約層回歸測試與 Linux 容器驗證工具；integration 層案例屬 B1-05／B1-06，尚未執行。B1-03 已解鎖，B1-05 仍待 B1-03。

9/30 紀錄：Aaron 接受 approval deadline 規則，以及 `b57b585` 的 `cases.json` 案例與預期結果（見「事前案例與證據索引」）。

**版本**：`apa.m1.v2` 取代從未驗收的 v1（`57791d1`）。v1 的 commit、golden vectors 與 review 證據都保留，v2 不接受 v1 文件。兩版都是 **M1 fixture 契約**；正式付款契約另行命名，不使用 `apa.m1.*`，也不與本版混用。

## 入口與使用順序

- `internal/contracts/`：共用型別與驗證函式。接收 JSON 時先 `Decode`，再呼叫型別語意驗證；`json.Unmarshal` 不能替代入口檢查。
- `schemas/m1.schema.json`：JSON Schema 2020-12，`$defs` 定義每個型別。Schema 管形狀；Go 再驗整數上限、報價一致性、歸屬、事件鏈、狀態轉移等跨欄位條件。列舉值由測試對照 Go 的清單。
- `testdata/contracts/registry.json`：A／B／商家三個不同地址、一筆可信資產；同一 A 的兩個 task 共用 Wallet A。全部地址均為合成 fixture，沒有私鑰或資產；不是真 token 部署地址。
- `purchase-input.json`／`events.json`／`golden.json`：固定請購、固定事件鏈與預期 canonical bytes／digest；Go 與 `scripts/check-contract-vectors.mjs` 使用同一份檔案。
- `cases.json`：在 Policy 實作前手寫的 expected outcomes，分 contracts／integration，沒有呼叫 Policy 函式產生答案。Aaron 於 2026-09-30 接受（`review_status=accepted`，`reviewer=aaron`）；接受紀錄見下方「案例清單接受紀錄」。

可信 host 設定 → `NewRegistry` → 已認證 wrapper 建立 `Session` → `Decode` 請購 → `ValidatePurchase` → 計算 retry key 與 intent hash → B1-05 才做 `CheckTaskActive`、Policy 與原子持久化。**不可以把 Session 從模型送來的同一份 JSON 反序列化。** 函式參數叫 Session 不表示已實作身分認證。

## 格式與數值

| 欄位 | v2 規則 |
|---|---|
| 金額 | JSON 十進位字串，最小單位，範圍 0～18446744073709551615；請購須大於 0。0.50 fixture USDC＝`"500000"`；非 uint256 codec |
| chain_id | 非零十進位字串，uint64 範圍；fixture 為 `"84532"` |
| 時間 | Unix seconds 的 JSON 整數，1～253402300799；拒絕常見毫秒輸入；效期上界嚴格大於下界。是否過期由 Policy 配注入 clock 判斷 |
| 地址／nonce | 小寫 `0x`＋40／64 個 hex 字元；不做默默大小寫轉換，也不表示已驗證 checksum、持鑰或部署 |
| ID | 1～128 字元英數及 `._:-`，開頭須英數 |
| 選填字串 | 不出現或有意義的值；出現空字串 `""` 即拒絕，與 schema 一致 |
| URL | 完整 http(s) URL，無 userinfo，**不得含 `#`**（空 fragment 也拒絕），不正規化 query 順序。通過形狀檢查不等於已在抓取白名單 |
| method／scheme／路徑 | M1 限 GET／POST、exact、x402 version 2；選定 requirement 的 `extra.assetTransferMethod` 未指定或為 `eip3009` 才接受，其餘回 `UNSUPPORTED_TRANSFER_METHOD`（M1 intent 帶的是 EIP-3009 nonce） |
| sequence | task 內從 1 遞增；系統事件為 0，沒有 task scope；安全整數上限 2^53−1 |

## Canonical encoding 與 hash

本案定義 **APA JSON subset**，不是 RFC 8785、EIP-712、EIP-3009 digest 或 ZK field 映射。

1. 輸入限 64 KiB；物件與陣列巢狀最多 32 層（純量不算一層）；拒絕重複 key（含 escape 後相同者）、多份文件、無效 UTF-8、無效 surrogate／U+FFFD。
2. JSON number 只接受 −(2^53−1)～2^53−1 的標準整數文字；拒絕浮點、exponent 與 `-0`。較大整數只能是明定的字串欄位。未知報價欄位若超出此 subset，整筆拒絕，不丟棄欄位後繼續。
3. 物件依 key 的 UTF-8 byte 字典序排序；陣列保留順序；移除空白。字串使用 Go `encoding/json` 的跳脫表示，含 `<>&`、U+2028／2029，不做 Unicode normalization。JavaScript probe 獨立實作相同規則，golden 內 9 組 canonical 案例兩邊一致。
4. `digest = "sha256:" + lowercase_hex(SHA256(UTF8(domain) || 0x00 || canonical_bytes))`。
5. 基底 domain 固定 `agent-payment-authority/m1-fixture/v2/verifier-not-deployed`；resource／requirement／intent／retry／purchase／cases／event／event-genesis 各加不同 suffix。

`resource_hash` 綁完整 method＋URL。`payment_requirement_hash` 綁 `{x402_version, selected_requirement, extensions}`：保留完整選定 requirement 與外層 extensions（含未知欄位）。報價物件中**只差大小寫的 key**（例如 `payTo` 與 `payto`）一律拒絕，reason `AMBIGUOUS_KEY`，避免之後以 Go struct（不分大小寫）解析 wire 時讀到另一個值。`Quote` 是內部容器，不冒充 x402 的 wire envelope；完整官方 wire 解析與版本核對屬 B1-03。

`AuthorizationIntent` 包含 consumer／agent／wallet／task／purchase、resource hash／method／URL、x402 metadata／requirement hash、chain／asset／from／pay_to／amount／效期／fixture nonce。各欄位改變須改 hash 或拒絕。`PurchaseOrder.Validate` 另核對 scheme／network／asset／payTo／amount 與報價一致及付款路徑，採固定檢查順序；它只驗形狀、一致性與路徑，**通過不代表 Policy 放行**。

所有 digest 都是 **fixture domain**。目前 verifier 未部署、nonce 不具簽章效力；未完成 EIP-712 typed-data 或 ZK public-input 映射，不得沿用本版 hash 宣稱真付款綁定。

## 歸屬、可信資產與重試

Registry 驗 consumer→agent→wallet、task→consumer、wallet→chain／from；拒絕共享 wallet／地址、重複 agent、未擁有的 wallet 及 payer＝merchant。回傳資料為副本。A 的 Session 配 B 的 task／wallet／from／consumer／agent 一律拒絕，reason `OWNERSHIP_MISMATCH`。

**可信資產**放在 registry 的 `assets` 區塊，是資產資料唯一的權威來源：`asset_id`、`chain_id`、`asset_contract`、`decimals`（0～77）、`eip712_name`、`eip712_version`，由可信設定提供。Registry 驗證條目（至少一筆、ID 與 `chain_id`＋合約不重複）並以 `TrustedAsset(chain_id, contract)` 提供查詢；**是否允許使用由 B1-05 Policy 判斷**，不在清單內即 Deny，reason `ASSET_NOT_ALLOWED`。案例 defaults 只以 `asset_id` 引用資產，不維護另一份資料。

同一個 registry snapshot 重新載入、或同一消費者另一個預置 task，仍指向原 wallet。這只驗證**契約重建**，不表示服務 crash recovery 或磁碟持久化完成。

Retry scope 固定 `(consumer_id, task_id, wallet_id, request_id)`，必須先完成可信歸屬檢查。

| 情況 | `CompareRetry` 結果 |
|---|---|
| 同 scope、相同 canonical 完整 PurchaseOrder | `replay` |
| 同 scope、內容不同 | `IDEMPOTENCY_CONFLICT` |
| 不同 scope，但同 task 重用 `purchase_order_id`（`purchase_order_id` 在 task 內唯一） | `IDEMPOTENCY_CONFLICT` |
| 不同 scope，但同一 `(chain_id, asset_contract, from)` 重用 `eip3009_nonce`（依 EIP-3009 語意唯一） | `IDEMPOTENCY_CONFLICT` |
| 其他不同 scope（含 A／B 使用同一個 request_id） | `new` |

wrapper 重試須沿用原 purchase_order_id／nonce，不能每次重建。`CompareRetry` 是純函式，**沒有做原子插入、鎖或唯一索引**；B1-05 必須把驗證結果與事件一起持久化並建立上述唯一索引，避免並行重入。

## 事件、決策與狀態轉移

`AuditEvent` 包含版本、event ID、type、scope、sequence、`prev_event_hash`、typed payload、evidence_mode、source、observed_at。task scope 帶完整歸屬；purchase event 另帶 purchase_order_id／intent_hash，必須與 registry 及完整請購 snapshot 比對。沒有 task 的 SYSTEM_READY 不造 consumer，也沒有事件鏈；尚無請購的 TASK_STARTED 不造 intent hash。

**決策與核准狀態**：`POLICY_DECIDED` 記錄 Policy 的裁決。Ask 的裁決終身不變；核准請求結束時，另發一個事件記錄，**不回寫、不抹除原本的 Ask**，也沒有任何核准能力。

| 事件 | policy_verdict | approval_status | reason | 意思 |
|---|---|---|---|---|
| `POLICY_DECIDED` | Ask | pending | HUMAN_APPROVAL_REQUIRED | 等待真人核准，不核准、不扣款 |
| `POLICY_DECIDED` | Deny | not_requested | Deny 代碼之一 | 拒絕，不發 grant |
| `POLICY_DECIDED` | Allow | not_requested | PAYMENT_CAPABILITY_UNAVAILABLE | M1 沒有付款能力 |
| `APPROVAL_EXPIRED` | Ask | expired | APPROVAL_TIMEOUT | 等待核准逾時；請購狀態為 denied，不扣預算 |
| `APPROVAL_WITHDRAWN` | Ask | withdrawn | HOST_CANCELLED | 已提出的核准請求被撤回（例如主持人取消 task） |

尚未提出核准就取消，仍是 `not_requested`，不發 `APPROVAL_WITHDRAWN`。`payment_status` 一律 `unavailable`，否則 reason `M1_PAYMENT_UNAVAILABLE`；其他不合法組合 reason `INVALID_DECISION`。Deny 代碼：`INSUFFICIENT_BUDGET`、`MERCHANT_NOT_ALLOWED`、`AUTHORIZATION_EXPIRED`、`CONFIG_MISSING`、`APPROVER_UNAVAILABLE`、`ASSET_NOT_ALLOWED`、`TASK_NOT_ACTIVE`。

M1 event type：SYSTEM_READY、TASK_STARTED、PURCHASE_PROPOSED、POLICY_DECIDED、APPROVAL_EXPIRED、APPROVAL_WITHDRAWN、TASK_CANCELLED（code `HOST_CANCELLED`）、TASK_TIMED_OUT（code `CLI_TIMEOUT`，只代表 CLI／task 逾時，approval 逾時用 APPROVAL_EXPIRED）、CLI_EXITED（任一 code）。不接受 APPROVED／GRANT_ISSUED／SETTLED／RESOURCE_DELIVERED；lifecycle event 不能攜帶決策。

**三種責任分開**：

| 責任 | 規則 | 落在哪裡 |
|---|---|---|
| 新請購受理 | task 為 denied／cancelled／failed 時拒收新請購，reason `TASK_NOT_ACTIVE` | `CheckTaskActive(status)` 定義規則；B1-05 以持久化的執行期狀態呼叫。registry 的 `status` 只是設定快照 |
| 歷史事件驗證 | `ValidateEvent` 只驗歸屬、intent 綁定與事件格式，**不看 task 目前狀態**，已結束 task 的事件仍可驗證 | 契約 |
| 狀態轉移 | 對整條串流套用下表 | 契約：`ValidateEventOrder` |

`ValidateEventOrder` 對每個 task 的串流檢查（違反時 reason `INVALID_EVENT_ORDER`，鏈斷時 `EVENT_CHAIN_BROKEN`，請購 intent 改變時 `PURCHASE_INTENT_CHANGED`）。下文「task 結束」指出現 Deny 裁決、TASK_CANCELLED 或 TASK_TIMED_OUT 之一：

- event ID 不重複；sequence 從 1 連續；`prev_event_hash` 連成鏈（見下節）。
- 第一個事件必須是 TASK_STARTED，且只出現一次。
- **intent 固定**：每個 `(task_id, purchase_order_id)` 以首次 PURCHASE_PROPOSED 的 `intent_hash` 為準；之後同一請購的 POLICY_DECIDED、APPROVAL_EXPIRED、APPROVAL_WITHDRAWN 必須引用相同 `intent_hash`，否則 `PURCHASE_INTENT_CHANGED`。即使每個事件都附上本身合法的請購快照，也不能中途換成另一份內容。
- PURCHASE_PROPOSED：task 結束後不得出現（包含其他 purchase_order_id）；同一請購只能提出一次。
- POLICY_DECIDED：須有同一請購較早的 PURCHASE_PROPOSED；每個請購只有一個；task 結束後不得出現（包含其他 purchase_order_id）。
- **Deny 終止 task**：M1 中 Deny 後客人離場，該 task 不再接受任何請購或裁決；registry 或執行期的 task 狀態為 `denied`。已存在的 pending Ask 仍可依下條收尾，不會產生核准或付款能力。
- APPROVAL_EXPIRED／APPROVAL_WITHDRAWN：只能結束一個仍開著的 Ask，且只有一次；task 結束後仍可出現（例如另一筆請購被 Deny、CLI 逾時或主持人取消後，待決的核准到期或被撤回）。
- TASK_CANCELLED／TASK_TIMED_OUT：每個 task 最多一個；可以出現在 Deny 之後，只作收尾紀錄（CLI 逾時或主持人取消），不改變任何裁決。**後續的 task 狀態與展示投影必須保留原本的 Deny 與其 reason**，不得因收尾事件改顯示為 cancelled／failed 而抹掉 Deny 原因；此投影由 B1-05／B1-06 實作，B1-07 檢查。
- 取消時若有待決的 Ask，最終應出現 APPROVAL_WITHDRAWN；前綴檢查無法強制「最終一定出現」，由 B1-05 與 B1-07 負責。
- 歷史事件不以 task 目前狀態否定：已 denied／cancelled 的 task，其合法歷史仍能通過 `ValidateEvent` 與 `ValidateEventOrder`。

payload 只開放 code／decision，不接受 raw CLI output、私鑰或生物資料欄位。這是型別限制，不能取代 B1-04／05 的 redaction 與事件來源認證。live／fixture／replay 表示該事件來源，不能整個任務因此冒充 live payment。

## 事件 hash 鏈（`prev_event_hash`）

- **範圍**：每個 task 一條鏈；SYSTEM_READY 不在任何鏈上，且不得帶 `prev_event_hash`。
- **首筆**：sequence 1 的 `prev_event_hash` = `GenesisHash(task_id)` = fixture domain 加 `/event-genesis` 對 `{"task_id": …}` 的 digest。綁 task ID，防止把一段鏈前綴移到另一個 task。
- **後續**：`prev_event_hash` = 同 task 上一個事件的 `EventHash`。
- **事件 hash**：`EventHash(e)` = fixture domain 加 `/event` 對整個事件 canonical JSON 的 digest，包含該事件自己的 `prev_event_hash`。事件沒有存放自己 hash 的欄位，所以自己的 hash 不納入自身計算。
- **測試向量**：`events.json` 為 task-a 的四個事件；`golden.json` 的 `event_genesis_task_a` 與 `event_hashes` 由 Go 與 JavaScript 各自重算並比對。

限制：這是純函式檢查。沒有外部 checkpoint 時，**無法偵測尾端截斷或整條鏈重算**；中段修改、刪除、插入與重排可以偵測。持久化、原子追加與重啟恢復交 B1-05。新增這個欄位不代表防竄改稽核已完成。

## 事前案例與證據索引

`cases.json` 的 defaults 與每個案例一起參與 manifest hash。defaults 恰為八個 key：remaining＝5000000、allow_max＝100000、merchant＝allowed、credential＝valid、required_config＝present、mode＝interactive、clock＝1790726450、asset＝`fixture-usdc`（registry 的 asset_id）。parameters 是相對基線的單案例覆蓋；amount 覆蓋要同時更新 trusted quote／intent 並重算 hash，不由 LLM 改寫。cancel／timeout 為各自獨立案例，不接續其他案例執行。

**approval deadline 邊界（Aaron 2026-09-30 接受）**：

- 計時起點：**可信 host 記錄的 Ask 時間**，即該請購 `POLICY_DECIDED`（Ask，approval pending）事件由 host 寫入的 `observed_at`。不採用模型、CLI 或前端提供的時間。
- 時間單位：整數 Unix 秒；`approval_deadline_seconds` 為正整數秒。
- `elapsed = now − Ask 的 observed_at`；規則為 **`elapsed >= approval_deadline_seconds` 即 expired**（取代前版「恰好到期仍 pending」的寫法，前版由實作者寫定、未獲接受）。
- 三個預期案例：`approval-deadline-before`（29 秒，pending）、`approval-deadline-exact`（30 秒，expired）、`approval-timeout`（31 秒，expired）。`advance_seconds` 即評估當下的 `elapsed`。
- `CaseManifest.Validate` 依此規則檢查 `advance-clock` 案例：`advance_seconds` 與 result 不一致即 `CONTRADICTORY_EXPECTATION`。
- 契約不實作計時器；B1-05 依確認後的規則實作。

`CaseManifest.Validate` 的規則（違反時的 reason code）：

| 規則 | 代碼 |
|---|---|
| operation 屬於封閉清單且 layer 相符（contracts：compare-retry、binding、validate-decision、validate-purchase；integration：policy、cancel、advance-clock、cli-timeout、restart-and-new-task） | `UNKNOWN_OPERATION` |
| 參數名稱屬於該 operation 的允許集合；defaults 恰為八個 key | `UNKNOWN_PARAMETER` |
| 參數值在值域內、必填參數齊全 | `INVALID_PARAMETER` |
| expected 一致：result 屬於該 operation、reason 屬於該 result 允許的代碼、approval_status 與 result 及參數相符、payment_status＝unavailable、`budget_deducted`＝"0"、四種禁止事件齊全；`advance-clock` 的 result 符合 deadline 規則 | `CONTRADICTORY_EXPECTATION` |
| case ID 不重複、不與 capability ID 相同 | `INVALID_MANIFEST` |

`ValidateRefs(registry)` 另核對 defaults 與案例引用的 asset_id、consumer 存在於 registry。

**Claim 分兩類**：case claim 是 `cases.json` 的每個 case_id，每個恰好出現在 results 或 not_evaluated 一次；capability claim 是封閉清單 `credential_verification`、`webauthn`、`voice_approval`、`zk_budget_proof`、`signer_key_selection`（涵蓋真實金鑰選擇與金鑰隔離）、`cross_consumer_grant_proof`、`onchain_settlement`（涵蓋鏈上付款與對帳）。**M1 run manifest 必須把七個 capability 全部列為 not_evaluated**；出現在 results、漏列任何一個，reason `CAPABILITY_CLAIM_INVALID`；not_evaluated 含未知 ID，reason `UNKNOWN_CLAIM`。

每個 expected outcome 明列禁止提前核准、grant、付款與交付，並斷言 `budget_deducted` 為 0。尚未實作的 integration 案例保持 not_evaluated，不因成功解析 manifest 就算通過。B1-02 測試驗證案例結構與契約 guard；不是這些 Policy／CLI／持久化案例已跑通。

`RunManifest` 固定 run ID、commit／dirty patch digest、config digest、事前 case manifest digest、工具版本、命令＋exit code、證據索引、每個 case 的 PASS／FAIL／INCONCLUSIVE 與 not_evaluated。證據 path 相對該 run directory，不允許 `..`／絕對路徑；檔案 digest 定義為原始 bytes 的 SHA-256（含前綴 `sha256:`），不同於 JSON canonical digest。

`ValidateAgainst` 僅驗索引結構、case coverage 與 capability 規則，不讀檔、不核對證據 bytes、不重算 Policy，更不是 B1-07 checker。B1-07 須防 symlink escape、驗 digest、對照事前 expected outcomes，缺證據用 INCONCLUSIVE。`RequireAccepted` 是流程 guard，不是 reviewer 的數位簽章，不能阻止檔案作者自行改 accepted；正式接受紀錄須另留。

**Reason code 是封閉清單**（`ReasonCodes`），錯誤以 `CodedError` 攜帶，呼叫端與 B1-07 以 `ErrorCode(err)` 比對代碼，不比對訊息文字。新增代碼需要新版本。

## 重跑與驗收

```sh
make check-contracts   # go test ./internal/contracts/... 與 JS 向量檢查
make check
```

JavaScript probe 需要 Node.js 20 以上，只讀 fixture、不連網、不建完整前端。它以 `JSON.parse` 讀已驗證的 committed fixture，不可拿來接收不可信 ingress（JSON.parse 會丟掉重複 key）。正式商店／Dashboard adapter 尚未實作，B1-06 必須重用相同測試向量。

Schema 的本機測試檢查 JSON 可解析、所有 `$ref` 可解析、Go 型別與 schema 的 properties／required 一致，以及列舉值（task 狀態、事件型別、evidence mode、approval 狀態、裁決、operation、defaults）與 Go 清單一致；目前環境未安裝完整 JSON Schema validator，未宣稱通過外部 meta-schema 驗證。實際 ingress 仍由嚴格 Decode＋語意驗證把關。

### 案例清單接受紀錄

| 項目 | 值 |
|---|---|
| 接受者與日期 | Aaron，2026-09-30 |
| 接受的版本 | commit `b57b585` 的 `testdata/contracts/cases.json`（31 個案例）；檔案 SHA-256 `f2c9d448235734a08708de39181a54aa878d227050ca266873d5003839dc49ef`，當時 `review_status=pending`，manifest digest `sha256:927fceb7b784ba835e64291908e44db8fdf034768d1dce2a4c0ede476dfe5399` |
| 記錄接受後的檔案 | 案例內容不變，只把 `review_status` 改為 `accepted`、`reviewer` 改為 `aaron`；manifest digest `sha256:d8b740748cdb7fcf7e88ff877b508b8459c6046675d2aef19246dc1124b8d51d` |
| 固定方式 | `golden.json` 的 `accepted_case_manifest_digest`，由 Go（`TestGoldenVectors`）與 JavaScript probe 各自重算比對；修改任何案例都會失敗，須重新接受 |
| 範圍 | 只代表案例與預期結果被接受。整合測試（integration 層案例）尚未執行，屬 B1-05／B1-06。`RequireAccepted` 是流程 guard，不是數位簽章 |

### 結案

B1-02 於 2026-10-01 由 Aaron 決定結案，驗收 commit `2dc8d93`（獨立 review 與 Linux 容器隔離驗證均已通過）。B1-03 解鎖；B1-05 依賴 B1-02 與 B1-03，仍待 B1-03。

## S4-A 契約影響與遷移邊界

> **狀態（2026-10-01，Aaron 確認）**：
>
> | 項目 | 狀態 |
> |---|---|
> | B1-02＋S4-A 的 M1 契約影響審查 | 已接受，基準 `2dc8d93` |
> | S4-A 帳戶方案選型／ADR | 仍待決，維持 Proposed |
> | S4-B、testnet 操作 | 未核准 |
>
> 接受契約影響審查，代表目前 fixture 契約與下方遷移邊界足以繼續 M1，**不是採用 ERC-8196、7702 或 7710**。

> 2026-09-30 S4-A 第三版。初審對象 `57791d1`；依會合規則，本版已對照 B1-02 v2 修正後的契約重新核對（同一 commit 交付）。依據見 [ADR](adr/payment-account-model.md) 與[相容性盤點](spikes/payment-policy-compatibility.md)；C1～C10 見 vault 的 B1-02 契約 Review。候選代號沿用 ADR：A 基線、B1 7702＋7710、B2 7702＋EIP-3009 經 ERC-1271、C ERC-8196。

### 1. 結論

`apa.m1.v2` 仍是 M1 fixture 契約，不升格為正式付款契約。正式付款路徑使用**另行命名的正式付款契約**：把業務 intent 與路徑專屬的付款授權拆開，兩者仍以同一個 `intent_hash` 做密碼學綁定。B1-02 這一輪補的是防呆與規則，不是改成正式付款格式。

### 2. 影響表（依 v2 重核）

| 項目 | M1 v2 現況 | 候選路徑的影響 | 處置 |
|---|---|---|---|
| 業務 intent 與付款授權 | 單一 `AuthorizationIntent` 同時含業務欄位與 `eip3009_nonce` | B1 沒有 3009 nonce，改帶 `delegationManager`、`permissionContext`、`delegator`；B2 仍用 3009 欄位，但簽章改為 delegate 接受的格式；C 用 `policyHash` 與自己的 nonce | M1 保留。正式付款契約拆成業務 intent＋`payment_binding{method, 路徑欄位}`，`intent_hash` 涵蓋兩者 |
| 付款路徑 | **已修（SA1）**：`extra.assetTransferMethod` 未指定或 `eip3009` 才接受，其餘 `UNSUPPORTED_TRANSFER_METHOD` | x402 以此欄位選路徑；B2 不需要新值，由 facilitator 依地址是否有 code 改用 EIP-1271 驗簽 | 完成；正式付款契約依選定路徑另定 |
| `eip3009_nonce` 與 action nonce | **已修（C3）**：唯一範圍 `(chain_id, asset_contract, from, eip3009_nonce)`，重用判 `IDEMPOTENCY_CONFLICT` | `erc7710` 的重播保護靠 enforcer 與 Delegation Manager | 契約規則完成；原子唯一索引交 B1-05；7710 重播交 S4-B 實測 |
| 內部 `agent_id` 與鏈上身份 | 內部字串 ID | ERC-8004 `agentId` 是各 registry 的 uint256，命名空間 `eip155:{chainId}:{registry}` | M1 保留。若日後需要，在可信 registry 加可選的 `agent_registry_ref`，由主持人設定，不由模型或請購填寫 |
| 簽章 domain | fixture SHA-256 domain（v2） | 3009 用代幣的 EIP-712 domain；7710 用 Delegation Manager 的 EIP-712；ERC-8196 用 `AgentAction` typehash | M1 digest 不得作任何鏈上簽章。registry 資產已帶 `eip712_name`／`eip712_version`，供正式付款契約定義 domain 時對照 |
| ZK public inputs | 計畫把 `intent_hash` 當 public input | SHA-256 輸出超過 BN254 scalar field | 交 S3 與正式付款契約：拆 limb 或改 field hash；M1 不宣稱可直接作 public input |
| grant 與政策撤銷 | 沒有 grant 與撤銷事件 | 7710 與 ERC-8196 都有鏈上撤銷 | M1 不加；付款模組前在正式付款契約定義 grant 簽發與撤銷事件 |
| audit schema | **已加（決定 5）**：`prev_event_hash` 每 task 一條鏈，genesis 綁 task ID，Go／JS 測試向量 | ERC-8196 的 `previousHash` 概念相同 | 契約完成；持久化、原子追加、重啟恢復與 checkpoint 交 B1-05 |
| 資產與金額 | **已修（C5）**：registry `assets` 區塊為唯一權威，`TrustedAsset` 查詢，Deny 代碼 `ASSET_NOT_ALLOWED` | ERC-8196 以 wei 計；7710 用 ERC-20 enforcer 綁代幣與金額（收款人另需 calldata 限制） | 契約完成；Policy 比對交 B1-05 |
| 報價解析一致性 | **已修（C4）**：只差大小寫的 key 拒絕，`AMBIGUOUS_KEY` | B1-03／04 解析 wire 時用精確大小寫 | 完成 |

### 3. C1～C10 與 SA1 的歸屬（v2 狀態）

| 項目 | M1 v2 已完成 | 留給 B1-05 或後續 |
|---|---|---|
| C1 取消與 approval 逾時 | `expired`／`withdrawn`；APPROVAL_EXPIRED／APPROVAL_WITHDRAWN 事件保留原 Ask；`budget_deducted` 斷言 | 等待上限計時、實際額度記錄 |
| C2 事件與狀態 | 三項責任分開；`ValidateEventOrder` 轉移規則；歷史事件不看目前狀態；`CheckTaskActive` | 以執行期狀態拒收新請購 |
| C3 唯一性 | purchase_order_id（task 內）與 nonce（付款人＋資產內）唯一 | 原子唯一索引與並行重入 |
| C4 大小寫重複 key | 報價物件拒絕 | wire 解析用精確大小寫 |
| C5 資產 | registry `assets`、`TrustedAsset`、`ASSET_NOT_ALLOWED`、`deny-asset` 案例 | Policy 實際比對 |
| C6 案例缺口 | 補歸屬、取消、逾時、邊界、資產案例（共 31 案） | —（Aaron 9/30 已接受 `cases.json`） |
| C7 原因代碼 | 封閉 `ReasonCodes`、`CodedError`、`ErrorCode` | — |
| C8 manifest | 封閉 operation／參數／outcome 表、七個 capability claim、`ValidateRefs`、負向測試 | B1-06 產出 run manifest；B1-07 依證據判定 |
| C9 schema 差異 | 空字串拒絕；列舉值對照測試 | 外部 validator |
| C10 文件差異 | 32 層巢狀（只計容器）；URL 拒絕 `#` | — |
| SA1 付款路徑 | 只接受 `eip3009` 或未指定 | — |

### 4. 遷移邊界

- M1 的所有 digest（v1、v2）只屬 fixture domain，不得作為鏈上值、EIP-712 digest 或 ZK public input。
- 正式付款模組開工前須備齊：Aaron 核准的 ADR、另行命名的正式付款契約（業務 intent 與 `payment_binding` 拆分、domain、路徑綁定）、新的 golden vectors 與回歸測試。
- 若走 B1：`payment_binding` 至少含 `delegation_manager`、`delegator`、`permission_context` 的 hash，且與 `from` 對應同一位消費者。
- 若走 B2：`payment_binding` 保留 3009 欄位，另記錄 delegate 位址與簽章格式版本，避免換 delegate 後舊簽章語意改變。
- 無論選哪條路徑：完整 requirement、task、消費者、付款地址、資產、金額、效期與實際執行內容都要有密碼學綁定；ZK 與 intent 綁定要求不因候選而刪除。

### 5. 會合規則

初審對象 `57791d1`，本版已依 B1-02 v2 修正重核並與之同一 commit 交付。之後任一方再改契約，另一方須重核，兩者再以同一 commit 驗收。

**B1-02 第二輪修正後重核（9/30）**：本輪變更為事件串流的 intent 固定（`PURCHASE_INTENT_CHANGED`）、Deny 終止 task，以及 approval deadline 邊界的修訂提案。上方影響表**不需變動**，理由：

- 沒有改動 `AuthorizationIntent`、`eip3009_nonce`、fixture domain、registry `assets` 或付款路徑規則，四個候選（A、B1、B2、C）的對應欄位不受影響。
- intent 固定強化了「同一請購的事件引用同一 intent」這條稽核連結，方向與遷移邊界的「完整密碼學綁定」一致；正式付款契約屬另一套命名，不承接 M1 的事件規則。
- Deny 終止 task 與 deadline 提案是 M1 流程語意，不涉及帳戶模型、delegation 或結算；deadline 規則被接受後由 B1-05 實作，不擴大 S4-A 研究或採用範圍。

**第三輪收尾後重核（9/30）**：新增 `advance-clock` 案例的 deadline 一致性檢查，以及「收尾事件不得抹掉 Deny 原因」的投影規則。兩者都只作用於 M1 案例清單與狀態投影，沒有改動 intent、nonce、domain、資產或付款路徑，影響表同樣不需變動。

## apa.pay.v1（P1-a，2026-10-12）

正式付款契約另立名稱，與 M1 的 `apa.m1.v2` 完全分開（實作 `internal/paycontract/`，說明見 [commercial-p1a.md](commercial-p1a.md)）。這一節只記錄契約；P1-a 沒有付款能力。

- **網域**：`agent-payment-authority/pay/v1/<kind>`（`intent`、`resource`、`requirement`、`retry`、`purchase`、`event`、`event-genesis`）。M1 的 fixture 網域與 digest 不變，也不拿來做鏈上值。
- **請購**：`{version, request_id, intent, payment_binding, quote}`。`intent` 是業務意圖（身分、task、`purchase_order_id`、資源與其 hash、x402 版本、scheme、`payment_requirement_hash`）；`payment_binding` 是 EIP-3009 的 `{method: "eip3009", chain_id, asset_contract, from, pay_to, amount, valid_after, valid_before, nonce}`，付款欄位只在這裡。`intent_hash = H(pay/v1/intent ‖ canonical({intent, payment_binding}))`：綁定任一欄改變，hash 就變。報價裡的 network、asset、payTo、amount 必須等於綁定；其中 asset 與 payTo 在報價裡可以是任一大小寫（402 可能用 EIP-55 的大小寫混合寫法，例如 test402），比對時視為同一個位址，綁定本身仍一律小寫，報價則照收到的原文計算 hash（Aaron 10/20 B5-D1，`docs/commercial-p2b.md`）。apa.pay.v2 共用同一個 Validate，同樣適用。
- **裁決**：沿用 `PolicyDecision` 的欄位形狀，`version` 為 `apa.pay.v1`、`payment_status` 一律 `not_enabled`；Ask 的核准狀態可為 pending、approved（`SIMULATED_APPROVAL`）、denied（`APPROVER_DENIED`）、superseded（`QUOTE_CHANGED`）、expired、withdrawn；範圍內是 Allow／`WITHIN_ALLOW_MAX`，不是付款。M1 的 `Validate` 拒絕這個版本，`paycontract.ValidateDecision` 拒絕 M1 的。
- **事件**：沿用 `AuditEvent` 外框，`version` 為 `apa.pay.v1`，hash 鏈用自己的網域。新增 `DELEGATION_BOUND`、`APPROVAL_SESSION_OPENED`、`APPROVED`、`APPROVAL_DENIED`、`APPROVAL_SUPERSEDED`、`GRANT_ISSUED`；`EventPayload` 為此新增四個只屬 v1 的選填欄位（`delegation_id`、`approval_session_id`、`grant_id`、`authorization_method`）。M1 的 `ValidateEvent` 拒絕帶任何一個的事件，`schemas/m1.schema.json` 把它們標成 M1 不得出現（`not: {}`）；既有 M1 事件的 canonical 形式與 hash 不變。仍然沒有 SETTLED 或 RESOURCE_DELIVERED。
- **授權（grant）**：`{version, jti, intent_hash, approval_session_id, decision: approve, authorization_method: simulated, issuer, audience: signer, approver_id, issued_at, expires_at}`；`jti` 由會話導出，一個會話最多一個；`expires_at` 是付款綁定的 `valid_before`。P1-a 只發 `simulated`，Signer 樁拒絕它。
- **golden vectors**：`internal/paycontract/paycontract_test.go` 凍結 M1 fixture 請購改寫成 v1 後的各個 hash 與導出 ID；這些是本實作自己算出的值，不是另一套獨立實作的對照。

