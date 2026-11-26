# 最小版 KYT：付款前篩收款方（11/26）

## 1 為什麼

KYH（認識本人）、KYA（認識 Agent）、KYT（認識交易）是業界常用的三層：專案前兩層有 eKYC 與 ERC-8004，第三層一直空著。商店名單只擋「不在名單上的」收款人；一間名單上的商店事後被列為高風險（被盜用、被制裁），之前沒有任何地方擋得住。

這一版只做最小的一步：付款前，檢查收款方是否在一份風險名單上，命中就不付。**名單是示範用的，不是交易監控服務**：沒有接任何供應商、沒有風險分數、不追蹤資金來源與去向，名單裡的地址也是佔位用的，沒有指涉任何真實對象。

## 2 做法

- **名單**（`internal/kyt`、`testdata/kyt/demo-risk-list.json`）：格式 `apa.kyt-demo-list.v1`，檔案本身寫明來源與「不是監控服務」；地址一律小寫、不重複，讀取時從嚴。
- **Host 的 Policy**（`internal/paypolicy`、`internal/store`）：Host 用 `--risk-list PATH` 載入名單（預設不載入，既有場景不受影響），啟動時在終端機印出地址數與檔案的 sha256。付款合約的請購裁決時，**最先**篩收款方：命中就 Deny `PAYEE_FLAGGED`，不論它是否在商店名單上、金額是否在自動範圍內、憑證是否有效。M1 合約不加，因為它的原因代碼固定在契約測試向量裡。
- **Signer 也擋一道**（`internal/signer`）：設定檔選填的 `blocked_payees`；簽章前收款方在裡面就拒絕 `PAYEE_FLAGGED`，什麼都不記。和頻率上限一樣，Host 被繞過或設定錯了，Signer 仍然不簽。
- **頁面**：Policy 的拒絕原因寫成「收款人在風險名單上（示範名單），付款前已拒絕」；Signer 拒簽時，下一步說明要確認 Host 的名單是否也列了它。
- **證據**：裁決照舊記在 `POLICY_DECIDED`，原因 `PAYEE_FLAGGED` 是付款合約的拒絕原因之一，checker 照一般的 Deny 檢查（之後沒有付款）。

## 3 驗證

- `internal/kyt`：`TestList`（從嚴讀取、大小寫不影響比對、沒有名單就不擋）。
- `internal/store`：`TestPayeeFlagged`（同一筆請購，有名單是 Deny `PAYEE_FLAGGED`，沒有名單是原本的 Ask）。
- `internal/signer`：`TestSignerBlockedPayee`（不簽、不記；設定只接受小寫、不重複的地址）。

## 4 界線（照實說）

- **不是 KYT 服務**：只比對地址，沒有風險評分、交易圖分析、制裁名單更新或人工覆核流程。正式做法是在同一個位置改問 Chainalysis、TRM 這類供應商。
- **名單在啟動時讀一次**：Host 與 Signer 都要重啟才會換名單；任務開始後才被列入的收款人，重啟後的裁決才擋得到。
- **Host 與 Signer 的名單是兩份設定**：要一致由人維護；Signer 那份在 `/opt` 的設定檔，要 Aaron 安裝。
- **只篩收款方**：不篩付款錢包自己、也不看資金來源。
