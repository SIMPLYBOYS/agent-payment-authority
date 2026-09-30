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
