# M1 共用資料契約

B1-02 交付 `apa.m1.v1` 的 Go 型別、JSON Schema、固定測試向量與事前案例清單。範圍只到請購／Ask／Deny；沒有 policy engine、HTTP 工具認證、持久化事件服務、Signer 或付款。契約與 `cases.json` 待 Aaron 或指定 reviewer 接受，不能因測試綠燈直接開始 B1-03／05。

## 入口與使用順序

- `internal/contracts/`：共用型別與驗證函式。接收 JSON 時先 `Decode`，再呼叫型別語意驗證；`json.Unmarshal` 不能替代入口檢查。
- `schemas/m1.schema.json`：JSON Schema 2020-12，`$defs` 定義每個型別。Schema 管形狀；Go 再驗整數上限、報價一致性、歸屬與狀態等跨欄位條件。
- `testdata/contracts/registry.json`：A／B／商家三個不同地址；同一 A 的兩個 task 共用 Wallet A。全部地址均為合成 fixture，沒有私鑰或資產；不是真 token 部署地址。
- `purchase-input.json`／`golden.json`：固定請購與預期 canonical bytes／digest；Go 與 `scripts/check-contract-vectors.mjs` 使用同一份檔案。
- `cases.json`：在 Policy 實作前手寫的 expected outcomes，分 contracts／integration。`review_status=pending`，沒有呼叫 Policy 函式產生答案。

可信 host 設定 → `NewRegistry` → 已認證 wrapper 建立 `Session` → `Decode` 請購 → `ValidatePurchase` → 計算 retry key 與 intent hash → B1-05 才做 policy／原子持久化。**不可以把 Session 從模型送來的同一份 JSON 反序列化。** 函式參數叫 Session 不表示已實作身分認證。

## 格式與數值

| 欄位 | v1 規則 |
|---|---|
| 金額 | JSON 十進位字串，最小單位，範圍 0～18446744073709551615；請購須大於 0。0.50 fixture USDC＝`"500000"`；非 uint256 codec |
| chain_id | 非零十進位字串，uint64 範圍；fixture 為 `"84532"` |
| 時間 | Unix seconds 的 JSON 整數，1～253402300799；拒絕常見毫秒輸入；效期上界嚴格大於下界。是否過期由 Policy 配注入 clock 判斷 |
| 地址／nonce | 小寫 `0x`＋40／64 個 hex 字元；不做默默大小寫轉換，也不表示已驗證 checksum、持鑰或部署 |
| ID | 1～128 字元英數及 `._:-`，開頭須英數 |
| URL | 完整 http(s) URL，無 userinfo／fragment，不正規化 query 順序；每個位元差異都保留。通過形狀檢查不等於已在抓取白名單 |
| method／scheme | M1 限 GET／POST 與 exact、x402 version 2；改用新版本／scheme 須升級契約，而非默默接受 |
| sequence | task 內從 1 遞增；系統事件為 0，沒有 task scope；安全整數上限 2^53−1 |

## Canonical encoding 與 hash

本案定義 **APA JSON subset v1**，不是 RFC 8785、EIP-712、EIP-3009 digest 或 ZK field 映射。

1. 輸入限 64 KiB、32 層巢狀；拒絕重複 key（含 escape 後相同者）、多份文件、無效 UTF-8、無效 surrogate／U+FFFD。
2. JSON number 只接受 −(2^53−1)～2^53−1 的標準整數文字；拒絕浮點、exponent 與 `-0`。較大整數只能是明定的字串欄位。未知報價欄位若超出此 subset，整筆拒絕，不丟棄欄位後繼續。
3. 物件依 key 的 UTF-8 byte 字典序排序；陣列保留順序；移除空白。字串使用 Go `encoding/json` 的跳脫表示，含 `<>&`、U+2028／2029，不做 Unicode normalization。JavaScript probe 獨立實作相同規則。
4. `digest = "sha256:" + lowercase_hex(SHA256(UTF8(domain) || 0x00 || canonical_bytes))`。
5. 基底 domain 固定 `agent-payment-authority/m1-fixture/v1/verifier-not-deployed`；resource／requirement／intent／retry／purchase／cases 各加不同 suffix。

`resource_hash` 綁完整 method＋URL。`payment_requirement_hash` 綁 `{x402_version, selected_requirement, extensions}`：保留完整選定 requirement 與外層 extensions（含未知欄位），補足選定 requirement 本身不含外層版本與擴充的情形。`Quote` 是內部容器，不冒充 x402 的 wire envelope；完整官方 wire 解析與版本核對屬 B1-03。

`AuthorizationIntent` 包含 consumer／agent／wallet／task／purchase、resource hash／method／URL、x402 metadata／requirement hash、chain／asset／from／pay_to／amount／效期／fixture nonce。各欄位改變須改 hash 或拒絕。`PurchaseOrder.Validate` 另核對 scheme／network／asset／payTo／amount 與報價一致，採固定檢查順序。

所有 digest 都是 **fixture domain**。目前 verifier 未部署、nonce 不具簽章效力；未完成 EIP-712 typed-data 或 ZK public-input 映射。後續替換 domain／演算法要升級版本與 golden vectors，不得沿用本版 hash 宣稱真付款綁定。

## 歸屬與重試

Registry 驗 consumer→agent→wallet、task→consumer、wallet→chain／from；拒絕共享 wallet／地址、重複 agent、未擁有的 wallet 及 payer＝merchant。回傳資料為副本。A 的 Session 配 B 的 task／wallet／from／consumer／agent 一律拒絕。

同一個 registry snapshot 重新載入、或同一消費者另一個預置 task，仍指向原 wallet。這只驗證**契約重建**，不表示服務 crash recovery 或磁碟持久化完成。

Retry scope 固定 `(consumer_id, task_id, wallet_id, request_id)`，必須先完成可信歸屬檢查。同 scope 與相同 canonical 完整 PurchaseOrder 回 `replay`；同 scope 不同內容回 `IDEMPOTENCY_CONFLICT`；不同 scope 為 `new`。wrapper 重試須沿用原 purchase_order_id／nonce，不能每次重建。A／B 可使用同一個 request_id 而不串帳。

`CompareRetry` 是純函式，**沒有做原子插入、鎖或唯一索引**；B1-05 必須把驗證結果與事件一起持久化，避免並行重入。

## 事件與決策

`AuditEvent` 包含版本、event ID、type、scope、sequence、typed payload、evidence_mode、source、observed_at。task scope 帶完整歸屬；purchase event 另帶 purchase_order_id／intent_hash，必須與 registry 及完整請購 snapshot 比對。沒有 task 的 SYSTEM_READY 不造 consumer；尚無請購的 TASK_STARTED 不造 intent hash。

| verdict | approval_status | payment_status | 邊界 |
|---|---|---|---|
| Ask | pending | unavailable | 等待，不核准、不扣款 |
| Deny | not_requested | unavailable | 拒絕，不發 grant |
| Allow | not_requested | unavailable | 整合契約只接受 reason＝PAYMENT_CAPABILITY_UNAVAILABLE |

M1 event type 限 SYSTEM_READY、TASK_STARTED、PURCHASE_PROPOSED、POLICY_DECIDED、TASK_CANCELLED、TASK_TIMED_OUT、CLI_EXITED；不接受 APPROVED／GRANT_ISSUED／SETTLED／RESOURCE_DELIVERED。CLI_EXITED 不能攜帶付款決策。Lifecycle event 不負責自行改寫請購裁決；完整狀態轉移由 B1-05 驗。

payload 只開放 code／decision，不接受 raw CLI output、私鑰或生物資料欄位。這是型別限制，不能取代 B1-04／05 的 redaction 與事件來源認證。live／fixture／replay 表示該事件來源，不能整個任務因此冒充 live payment。

## 事前案例與證據索引

`cases.json` 的 defaults 與每個案例一起參與 manifest hash。integration policy 共同基線為 remaining＝5000000、allow_max＝100000、merchant＝allowed、credential＝valid、required_config＝present、mode＝interactive、clock＝1790726450。parameters 是相對基線的單案例覆蓋；amount 覆蓋要同時更新 trusted quote／intent 並重算 hash，不由 LLM 改寫。cancel／timeout 為各自獨立案例，不接續其他案例執行。

每個 expected outcome 明列禁止提前核准、grant、付款與交付。尚未實作的 integration 案例保持 not_evaluated，不因成功解析 manifest 就算通過。B1-02 測試驗證案例結構與契約 guard；不是這些 Policy／CLI／持久化案例已跑通。

`RunManifest` 固定 run ID、commit／dirty patch digest、config digest、事前 case manifest digest、工具版本、命令＋exit code、證據索引、每個 case 的 PASS／FAIL／INCONCLUSIVE 與 not_evaluated。證據 path 相對該 run directory，不允許 `..`／絕對路徑；檔案 digest 定義為原始 bytes 的 SHA-256（含前綴 `sha256:`），不同於 JSON canonical digest。

`ValidateAgainst` 僅驗索引結構與 case coverage，不讀檔、不核對證據 bytes、不重算 Policy，更不是 B1-07 checker。B1-07 須防 symlink escape、驗 digest、對照事前 expected outcomes，缺證據用 INCONCLUSIVE。其 `RequireAccepted` 檢查是流程 guard，不是 reviewer 的數位簽章，不能阻止檔案作者自行改 accepted；正式接受紀錄須另留。

## 重跑與驗收

```sh
go test ./internal/contracts/... -count=1
node scripts/check-contract-vectors.mjs
make check
```

JavaScript probe 需要 Node.js 20 以上，只讀 fixture、不連網、不建完整前端。它以 `JSON.parse` 讀已驗證的 committed fixture，不可拿來接收不可信 ingress（JSON.parse 會丟掉重複 key）。正式商店／Dashboard adapter 尚未實作，B1-06 必須重用相同測試向量。

Schema 的本機測試檢查 JSON 可解析、所有 `$ref` 可解析，以及 Go 型別與 schema 的 properties／required 一致；目前環境未安裝完整 JSON Schema validator，未宣稱通過外部 meta-schema 驗證。實際 ingress 仍由嚴格 Decode＋語意驗證把關。

剩餘驗收：Aaron 或獨立 reviewer 確認 encoding／schema／case manifest／expected outcomes；本次實作者不自行把 B1-02 標 Done。B1-03／05 暫不解鎖。

## S4-A 契約影響與遷移邊界

> 2026-09-30 S4-A 草案第二版，待 Aaron 接受。審查對象 `57791d1`。本節只新增在文末，不改上文；B1-02 修正後依第 5 小節重新核對。依據見 [ADR](adr/payment-account-model.md) 與[相容性盤點](spikes/payment-policy-compatibility.md)；C1～C10 見 vault 的 B1-02 契約 Review。候選代號沿用 ADR：A 基線、B1 7702＋7710、B2 7702＋EIP-3009 經 ERC-1271、C ERC-8196。

### 1. 結論

`apa.m1.v1` 維持 fixture 契約，不升格為正式付款契約。正式付款路徑改用 v2：把業務 intent 與路徑專屬的付款授權拆開，兩者仍以同一個 `intent_hash` 做密碼學綁定。B1-02 這一輪要補的是防呆與規則，不是改成正式付款格式。

### 2. 影響表

| 項目 | v1 現況 | 候選路徑的影響 | 處置 |
|---|---|---|---|
| 業務 intent 與付款授權 | 單一 `AuthorizationIntent` 同時含業務欄位與 `eip3009_nonce` | B1 沒有 3009 nonce，改帶 `delegationManager`、`permissionContext`、`delegator`；B2 仍用 3009 欄位，但簽章改為 delegate 接受的格式；C 用 `policyHash` 與自己的 nonce | v1 保留。v2 拆成業務 intent＋`payment_binding{method, 路徑欄位}`，`intent_hash` 涵蓋兩者 |
| 付款路徑 | 沒有檢查 `extra.assetTransferMethod`；`permit2`、`erc7710` 報價配 3009 nonce 也能通過（SA1，實測） | x402 以此欄位選路徑，未指定時預設 `eip3009`；B2 不需要新值，由 facilitator 依地址是否有 code 改用 EIP-1271 驗簽 | **現在改**：未指定或 `eip3009` 才接受，其餘拒絕，reason `UNSUPPORTED_TRANSFER_METHOD` |
| `eip3009_nonce` 與 action nonce | 格式檢查；唯一性未定義（C3） | EIP-3009 的 nonce 在「代幣＋授權者」範圍內唯一；`erc7710` 的重播保護靠 enforcer 與 Delegation Manager | **現在定**：唯一範圍 `(chain_id, asset_contract, from, eip3009_nonce)`；原子唯一索引交 B1-05 |
| 內部 `agent_id` 與鏈上身份 | 內部字串 ID | ERC-8004 `agentId` 是各 registry 的 uint256，命名空間 `eip155:{chainId}:{registry}` | v1 保留。若日後需要，在可信 registry 加可選的 `agent_registry_ref`，由主持人設定，不由模型或請購填寫 |
| 簽章 domain | fixture SHA-256 domain | 3009 用代幣的 EIP-712 domain（`extra.name`／`version`）；7710 用 Delegation Manager 的 EIP-712；ERC-8196 用 `AgentAction` typehash | v1 digest 不得作任何鏈上簽章。v2 定義含 `chain_id` 與 verifying contract 的 domain，重產 golden vectors |
| ZK public inputs | 計畫把 `intent_hash` 當 public input | SHA-256 輸出超過 BN254 scalar field | 交 S3 與 v2：拆 limb 或改 field hash；v1 不宣稱可直接作 public input |
| grant 與政策撤銷 | 沒有 grant 與撤銷事件 | 7710 與 ERC-8196 都有鏈上撤銷 | M1 不加；付款模組前以 v2 定義 grant 簽發與撤銷事件 |
| audit schema | 有 task 內 `sequence`，沒有前一事件 hash | B1-05 要做 hash chain；ERC-8196 要求 `previousHash` | **待 Aaron**：B1-02 這輪先加 `prev_event_hash`（建議，免得 B1-05 再升版），或留給 B1-05 時升版 |
| 資產與金額 | 資產不綁可信設定（C5） | ERC-8196 以 wei 計；7710 用 ERC-20 enforcer 綁代幣與金額 | **現在定**：可信資產設定的欄位與 reason code；執行交 B1-05 Policy |
| 報價解析一致性 | 大小寫不同的同名 key 可並存（C4） | B1-03／04 若以 Go struct 解 wire 報價，讀法可能與契約不同 | **現在改**：拒絕只差大小寫的重複 key |

### 3. C1～C10 與 SA1 的歸屬

| 項目 | 現在於 B1-02 修正 | 留給 B1-05 或後續 | 待 Aaron 決定 |
|---|---|---|---|
| C1 取消與 approval 逾時 | `approval_status` 能表達逾時與撤回；定義對應事件；預期結果加剩餘額度斷言 | 等待上限計時、實際扣額記錄 | 新狀態值的命名 |
| C2 事件與狀態 | 狀態轉移表；`ValidateEventOrder` 加入不合法序列規則，歷史事件仍須能驗證 | 已結束的 task 拒收新請購（需要執行期狀態） | — |
| C3 唯一性 | purchase_order_id 與 nonce 的唯一範圍；同一 purchase_order_id 換 request_id 判衝突 | 原子唯一索引與並行重入 | — |
| C4 大小寫重複 key | 報價物件拒絕 | B1-03／04 解析 wire 時用精確大小寫 | — |
| C5 資產 | 可信資產設定欄位、reason `ASSET_NOT_ALLOWED`、預期案例 | Policy 實際比對 | 資產設定放 registry 或 policy defaults |
| C6 案例缺口 | 補歸屬與取消／逾時案例 | — | 接受 `cases.json` |
| C7 原因代碼 | 穩定的 reason code | — | — |
| C8 manifest | 定義封閉清單（operation 與各自的參數、reason code、capability claim ID）與驗證規則：未知 operation、錯誤參數名稱或值、矛盾的 expected outcome、未列或未知的 not_evaluated 都拒絕，並有負向測試；規格見 B1-02 Review C8 | B1-06 產出 run manifest；B1-07 依證據判定 claim | capability 清單內容與 `cases.json` |
| C9 schema 差異 | 空字串一致處理 | 外部 validator | — |
| C10 文件差異 | 修正文件 | — | — |
| SA1 付款路徑 | 拒絕非 `eip3009` 報價 | — | — |

另待 Aaron 決定：B1-02 修正是原地改 v1 還是升版（見 B1-02 契約 Review §5）、`prev_event_hash` 的時機、ADR 的暫定研究排序。

### 4. 遷移邊界

- v1 的所有 digest 只屬 fixture domain，不得作為鏈上值、EIP-712 digest 或 ZK public input。
- 正式付款模組開工前須備齊：Aaron 核准的 ADR、v2 契約（業務 intent 與 `payment_binding` 拆分、domain、路徑綁定）、新的 golden vectors 與回歸測試。
- 若走 B1：`payment_binding` 至少含 `delegation_manager`、`delegator`、`permission_context` 的 hash，且與 `from` 對應同一位消費者。
- 若走 B2：`payment_binding` 保留 3009 欄位，另記錄 delegate 位址與簽章格式版本，避免換 delegate 後舊簽章語意改變。
- 無論選哪條路徑：完整 requirement、task、消費者、付款地址、資產、金額、效期與實際執行內容都要有密碼學綁定，不為了可替換性刪除約束。

### 5. 會合規則

本節審的是 `57791d1`。B1-02 修正提交後，S4-A 要重新核對 `57791d1` 到修正 commit 之間的契約差異並更新本節；B1-02 與 S4-A 以同一個 commit 一起驗收。
