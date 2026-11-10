# eKYC：憑證的身分來源接上廠商沙盒

> 2026-11-10，Claude Code。**已實作並接進 demo（11/11）：發行者的指令、每個任務的身分來源、畫面、啟動腳本、證據包。** Aaron 11/10：「eKYC 需要串真的廠商沙盒，因為除了街景好看之外，最重要的是這中間運作的金融工具系統是否能夠帶給看 demo 的人在實際商業場域的想像，提高現實生活中落地的可能性」；廠商：「先從 Didit 開始」。

## 1 一段話

現在的憑證由測試發行者直接發，`country` 與 `age_over_18` 是啟動腳本手填的（`apa-issuer subjects`）：信任鏈的起點是「我說我滿 18 歲」。改成：家長先在 Didit 的驗證頁完成身分驗證（沙盒），發行者向 Didit 查到「通過」才寫入家長的屬性，屬性由驗證結果算出，不再手填。商店、Policy、付款與查帳的流程都不變。

## 2 為什麼是 Didit

| | 沙盒 | 取捨 |
|---|---|---|
| Didit（選用） | 註冊即有，沙盒不計費、沒有寫期限，一天 500 筆；`sandbox_scenario` 可以指定結果 | 知名度較低 |
| Persona | 試用 60 天、只有沙盒，之後每月 250 美元起 | 沒有排定的展示日期，試用會先過期 |
| Sumsub | 試用後沙盒仍可用，要上傳範本證件 | 註冊條件不明；台灣證件只查到 NFC 護照 |
| Authme（台灣） | 找不到自助沙盒 | 要聯絡業務，時程無法掌握 |

## 3 做法

| | 內容 |
|---|---|
| 發行者 | `apa-issuer ekyc --dir D --consumer consumer-a --dependents consumer-b --key-file F`（`cmd/apa-issuer/ekyc.go`）：向 Didit 開一個驗證 session（`POST https://verification.didit.me/v3/session/`，`vendor_data` 是 consumer ID；流程用應用的預設 KYC workflow，或 `--workflow`；沙盒可加 `--scenario`），印出驗證頁網址，每 3 秒查一次結果（`GET /v3/session/{id}/decision/`），最多等 `--wait` 秒（預設 600）。已經做完的驗證用 `--session ID`，不另開。只接受第 5 節說的那種通過；其他結果不寫任何檔案，指令失敗。每一位 holder 都要是 `--consumer` 或 `--dependents` 之一。 |
| 屬性怎麼來 | `country`：證件的發證國（`issuing_state`，三碼轉兩碼；對照表由 `golang.org/x/text` 的 CLDR 資料產生後寫進程式，沒有新增相依套件）。`age_over_18`：由 `date_of_birth` 與當天日期算出。`buyer_type`：eKYC 證明不了，由 `--buyer-type` 給（預設 `individual`），照實標示。 |
| 留下什麼 | 憑證目錄多一個 `ekyc.json`（`credential.EKYC`）：廠商、環境（sandbox）、session ID、決定的時間、被驗證的人、受扶養的人、算出的屬性。**不存**姓名、生日、證件號碼、影像：這些留在 Didit。之後再用 `apa-issuer subjects` 手動改屬性，`ekyc.json` 會被刪掉：紀錄不替手填的值背書。 |
| 孩子 | 孩子不做 eKYC。孩子的屬性由已通過驗證的家長代為申請（`--dependents`，`ekyc.json` 的 `dependents`）：國家同家長，`age_over_18=false`，啤酒照舊在付款前被擋。 |
| 畫面 | 「為什麼可信」多一列「身分來源：Didit（沙盒）· session …」；沒有 `ekyc.json` 的場次照舊寫「測試發行者」。 |
| 證據包 | 帶上 `ekyc.json`。checker 沒有 Didit 的金鑰，不能自己向 Didit 查：這一項是「紀錄在案」，不是 checker 驗過的主張，照實寫進限制。 |
| 金鑰 | `var/ekyc/didit-sandbox.key`（0600，`var/` 不進 git）。程式只讀檔，不印出。 |

## 4 界線與風險

- **沙盒沒有驗任何真人**：Didit 沙盒用固定的假證件與假自拍，結果可以指定。畫面與影片都標「沙盒」。真的證件審查要 Didit 的正式環境（要過他們的商業審核），屬下一步。
- **多一個外部依賴**：驗證頁與查詢要網路和 Didit 的服務。eKYC 在開場前做完、憑證存好；台上只展示紀錄。現場重做是備案。
- **沒過的情況也要演得出來**：`sandbox_scenario` 指定「不通過」時，家長拿不到屬性，任務建不起來。
- 沙盒回傳固定的範例身分（巴西護照），算出來的國家是 BR，不是 TW（第 5 節）。書店只要求揭露國家，不限定哪一國，流程不受影響；畫面照實顯示 BR 並標「沙盒範例證件」。

## 5 沙盒實測（11/10，key 的權限：Sessions 讀寫、Decisions 讀、Workflows 讀；Media 不給）

- **流程**：`Basic KYC`（證件、活體、人臉比對、IP 分析；workflow `082e7191-…`，`GET /v3/workflows/` 查得到）。開 session 回 201 與驗證頁網址；同一個 `vendor_data` 還沒開始的 session 會被重複使用（只換 scenario）。
- **驗證頁**：繁體中文，頁首標「SANDBOX 僅測試資料」。可以選「台灣 · 國民身份證」，上傳正反面兩張圖（沙盒收任何圖，實測用寫著 NOT A REAL ID 的佔位圖），接著是自拍，**要相機**：沒有相機的瀏覽器停在「正在準備相機」。
- **回傳的資料是固定的範例身分**，跟選的國家、上傳的圖無關：巴西電子護照、`date_of_birth 1990-06-15`、`age 36`、`issuing_state BRA`、`nationality BRA`，另有姓名、證件號碼、MRZ。所以沙盒算出來的 `country` 是 `BR`，不是 `TW`；`age_over_18` 是 `true`。
- **scenario**：`approve`、`decline_document_expired`、`decline_minimum_age`、`decline_face_match_low_similarity`、`decline_liveness_attack` 等（清單在 Didit 文件 Sandbox & Test Data；`GET /v1/sandbox/scenarios/` 這把 key 讀不到，403）。
- **不能信的「通過」**：`POST /v3/session/{id}/simulate/ {"new_status":"Approved"}` 用同一把 key 就能把 session 直接改成 Approved，後台的「Simulate an outcome」也是這個。這種結果**沒有任何證件資料**（`id_verifications` 是 null），`decision_trace.decided_by` 是 `manual`、`actor.actor_type` 是 `API_KEY`。發行者必須拒絕：只接受 `status=Approved`、`id_verifications[0].status=Approved` 且有生日與發證國、`decided_by` 不是 `manual` 的結果。否則發行者等於能自己替自己蓋章。
- **自拍用假相機（做法 B）沒有成功**（11/10，Aaron：「先試 B」）：無頭 Chrome 加 `--use-fake-device-for-media-stream`，相機畫面開得起來（測試圖案），但 Didit 的頁面真的在找臉：按快門後回「未偵測到人臉」。改餵一張自己畫的臉（`--use-file-for-fake-video-capture`，mjpeg 與 y4m、720p 與 1080p 都試過），頁面停在自拍說明，40 秒內沒有進到相機畫面，原因沒有查出來。沙盒的「不執行真實檢查」不包含頁面上的人臉偵測。
- **走完驗證頁的通過（做法 A，Aaron 用自己的瀏覽器與相機，session `3466f016-…`）**：`status=Approved`、`decision_trace.decided_by=automatic`、`environment=sandbox`；`id_verifications[0]` 是上面的範例護照；`liveness_checks[0]`（`PASSIVE`，99 分）與 `face_matches[0]`（95 分）都是 Approved。發行者接受的就是這個形狀：自動決定、有證件的生日與發證國、活體與人臉比對各有一筆通過、`vendor_data` 是這位客人。
- **還沒看到**：`decline_*` 走完驗證頁後回傳的樣子。

## 6 驗證（實作時）

- Go `TestEKYC`（假的 Didit 伺服器）：新 session 與 `--session` 都寫入屬性並發得出憑證；剛滿 18 歲當天是 `true`、差一天是 `false`；`ekyc.json` 與輸出裡沒有姓名、生日、證件號碼、金鑰；手動 `subjects` 之後 `ekyc.json` 不在。被拒絕且不寫任何檔案的 13 種：不通過、手動改成通過（有沒有證件資料都拒）、沒有證件、沒有活體、人臉不符、別人的 session、發證國不是國碼、沒有生日、逾時、漏了一位 holder、金鑰檔別人讀得到、金鑰錯。拿掉「不是手動決定」的檢查，測試會失敗（反向驗證過）。`TestAlpha2`：國碼對照。
- 真的沙盒（11/11）：Aaron 走完的 session 寫出 `country=BR`、`age_over_18=true`；用模擬端點改成通過的 session（`ce3b9060-…`）被拒：`decided "manual", not by the vendor's checks`，沒有寫任何檔案。

## 7 接進 demo（11/11）

| | 內容 |
|---|---|
| 每個任務的紀錄 | 發憑證時（`Dir.IssueTask`），當下的 `ekyc.json` 記在這個任務的憑證檔旁（`Stored.EKYC`；不在簽章的憑證裡，沒有 verifier 檢查它）。之後手動改屬性，舊任務的紀錄不變，新任務沒有紀錄。 |
| Host | `GET /api/tasks/{id}` 多一個 `identity_source`：`provider`、`environment`、`session_id`、`verified`（這位客人就是被驗證的人；`false` 是家長代為申請的孩子）。沒有紀錄就沒有這一欄。 |
| 畫面 | 「為什麼可信」的「2 選購」：`出示客人的憑證，檢查通過（身分經 Didit eKYC 驗證・沙盒）`；孩子是 `由通過 Didit eKYC 的家長代為申請・沙盒`；沒有紀錄照舊是 `測試發行者`。不認得的廠商名不顯示。國家 BR 顯示「巴西」。 |
| 啟動 | `APA_PRESENT_EKYC_SESSION=<session>`（`scripts/demo/present.sh`，要有 `APA_PRESENT_CREDENTIALS`）：`apa-issuer ekyc --session` 取代手填的 `subjects`；金鑰檔預設 `var/ekyc/didit-sandbox.key`（`APA_PRESENT_EKYC_KEY`）。沿用的憑證目錄已經有這個 session 的紀錄就不連線。讀不到或不被接受就停在啟動前，不會退回手填的屬性。沒設這個變數，一切照舊。 |
| 證據包 | `e1-evidence pack --ekyc <憑證目錄>/ekyc.json`：原樣放進證據包並列入索引。checker 的主張不變（`TestPackEKYC`）。 |

用法（本機）：`APA_PRESENT_EKYC_SESSION=3466f016-4c1c-4f1f-9ff8-0bee1be446b2 make demo-local`。

驗證：Go `TestTaskIdentitySource`（被驗證的人、受扶養的人、手填之後的新任務、舊任務保留紀錄）；網頁 `show.test.mjs`（四種文字）；啟動腳本那一段對真的沙盒跑過五種情況（連線取得、沿用紀錄、別人的 session 被拒、不是 session ID、沒設）。整個 demo 帶著這個變數啟動後的畫面見下一段。

**11/11 Aaron 帶著變數跑了一筆書店：「完全沒看到跟 ekyc 有關係的內容」。** 後端是對的（那一場的憑證目錄有 `ekyc.json`，新任務的憑證是 BR 並記著 session），問題在畫面：字只出現在展示視窗最下面「為什麼可信」的一列，主畫面的五步只多了「國家 巴西」，沒有說它是驗證來的。改成五步的第 2 步直接說：`這間店看到：國家 巴西（身分經 Didit eKYC 驗證・沙盒）`（店什麼都沒看到時不加）；主持人頁的請購明細多一列「身分來源」。用真的瀏覽器看過（`scripts/verify/show-ui.mjs` 的流程，屬性改由那筆驗證產生）：展示視窗的第 2 步與「為什麼可信」都顯示出來。Aaron 的 demo 要重新啟動才是新的頁面。

## 8 還沒做

- 影片裡的 eKYC 段落：自拍要真的相機（第 5 節），不能全自動重拍；影片仍是測試發行者，字卡的「eKYC 是下一期」對影片仍然成立。
- testnet 彩排時帶 `APA_PRESENT_EKYC_SESSION` 與 `pack --ekyc`。
- Didit 的正式環境（真的證件審查）。
