# C1 BBS+ 憑證選型 spike：候選、結論與 C2 需要的決定

> 2026-10-13，Claude Code，依 vault〈Claude-Code-C1-BBS憑證選型Spike派工-20261013〉（Aaron：「再開始 C1」）。分支 `commercial-c1`，基準 `main` `74cdc55`。程式在 `spikes/bbs/`（獨立 module，主 `go.mod` 不變）。
> **狀態：spike 完成，待 Claude Code 獨立上下文 review。這只證明格式與函式庫可用，不是憑證參與 Policy 的證據（那是 C2）。C2 未開始。**

## 1 結論（先講）

- **找不到仍在維護、且實作 IETF BBS 草案的 Go 函式庫。** W3C Data Integrity BBS cryptosuite（`bbs-2023`）用的是 IETF BBS 草案。
- **可用且仍在維護的 Go 實作，是舊版 BBS+**：`github.com/trustbloc/bbs-signature-go` v1.0.4，2026-01-29 發布，Apache-2.0。
  - 套件 `bbs12381g2pub` 使用 BLS12-381，公鑰在 G2，簽章是 `(A, e, s)`。
  - 屬於 2020 年 `BbsBlsSignature2020` 那一系。
- 以它做的 spike 證明了這些能力：
  - 發證、衍生 proof、只揭露指定欄位；
  - proof 綁定 verifier、challenge 與請購 intent；
  - 持有人綁定、期限、撤銷、challenge 只能用一次；
  - 每種拒絕都是固定代碼，驗證結果逐欄位說明。
- 正反測試全部通過；反向 7 項，有 1 項第一次沒有抓到，補正後抓到（第 4 節）。
- **它不是 `bbs-2023`。** 要沿用它，還是改走 IETF BBS，是 C2 的契約決定，要由 Aaron 決定（第 6 節）。
- 本片沒有改用 SD-JWT，也沒有把任何東西描述成 `bbs-2023` 的等價替代。

## 2 候選（2026-10-13 查證；以 proxy.golang.org、pkg.go.dev 與模組原始碼為準）

| 候選 | 版本／日期 | 授權 | 方案 | 判定 |
|---|---|---|---|---|
| `github.com/trustbloc/bbs-signature-go` | v1.0.4，2026-01-29 | Apache-2.0 | 舊版 BBS+（`bbs12381g2pub`；DST `BLS12381G1_XMD:BLAKE2B_SSWU_RO_BBS+_SIGNATURES:1_0_0`；`DeriveProof` 只有 nonce，沒有 header／presentation header） | **採用為 spike 對象**：仍在維護，三者之中最新 |
| `github.com/hyperledger/aries-bbs-go` | pseudo-version，2024-05-28 | Apache-2.0 | 同一套程式碼（trustbloc 的上游） | 不採用：較舊，沒有 tag |
| `github.com/IBM/idemix` 的 `bbs` | v0.3.1，2026-09-09 | Apache-2.0 | 同一套 `bbs12381g2pub` 程式碼，附在 idemix（Fabric）之中 | 不採用：要引入整個 idemix；方案與 trustbloc 相同 |
| `github.com/cloudflare/circl/bbs` | 只在 v1.3.4／v1.3.5（2023-10-16） | BSD-3-Clause | IETF BBS 的 API 形狀（`KeyGen`、`Sign`、`Verify`、`ProofGen`、`ProofVerify`，有 header、ph、disclosed indexes），引用 draft-03 | **不採用**：之後所有版本都沒有這個套件（最新 v1.6.5 沒有 `bbs`）；draft-03 已過時；找不到移除的說明 |
| `github.com/bronlabs/bron-crypto` | v0.1.0 | Apache-2.0 | 只有 BLS 與 hash-to-curve，沒有 BBS | 不適用 |

W3C `bbs-2023` 的實作報告列出 Digital Bazaar、Grotto Networking、Netis、Procivis、SpruceID、Trential 六家，但沒有列出語言，所以無法確認其中有沒有 Go 實作。

## 3 spike 做了什麼（`spikes/bbs/`）

- `credential.go`：
  - **欄位**：8 個固定順序的欄位 `schema, issuer, task_id, consumer_id, c0, expiry, holder, status`。每個訊息是 `名稱=值`，所以值不能被換到別的欄位。
  - **發證**：`Issuer.Issue`。
  - **出示**：`Present`。
    - 只揭露要求的欄位；預設把 `consumer_id`、`c0` 藏起來。
    - nonce 是 `SHA-256("…/presentation/v0" ‖ verifier ‖ challenge ‖ intent_hash)`。
    - 持有人用 Ed25519 簽 `nonce ‖ SHA-256(proof)`，這就是持有人綁定。
  - **驗證**：`Verifier.Verify`。
    - 依序檢查：verifier、challenge 有沒有用過、揭露的欄位是否正好等於要求的、必要欄位、以本 verifier 自己算出的 nonce 驗 proof、schema、issuer、持有人簽章、期限、撤銷。
    - 每種拒絕都是固定代碼；通過時回傳逐欄位的 `Result`，不回傳單純的「verified」。
- `credential_test.go`：
  - 正向一條；
  - 負例 18 項：錯的 issuer 金鑰、別的 issuer 簽的、竄改揭露的 task_id 或 expiry、竄改 proof、給別的 verifier 的 proof、別的 intent、別的 challenge、要求給別的 verifier、隱藏必要欄位、少給欄位、多揭露欄位、別人的持有人簽章、簽在別的 proof 上的持有人簽章、過期、撤銷、未信任的 issuer、未知的 schema；
  - 重播；未知欄位與零揭露；兩次 proof 位元組不同；
  - 函式庫輸入行為的紀錄（第 5 節）；
  - 效能 benchmark。
- `reverse.py`：在拋棄式複本裡拿掉一項驗證，要求指定的測試以預期的斷言失敗；建置錯誤不算抓到。
- 只用合成資料與固定 seed 的測試金鑰。沒有連到外部的 issuer、wallet、RPC 或驗證器，也沒有收集個資。

## 4 證據（`artifacts/s2/`）

| 項目 | 基準 | 結果 | 檔案 |
|---|---|---|---|
| `go test -count=1 -race -v ./...`（以 `rtk proxy` 取得完整輸出） | `6f7b37e` | 24 項 PASS、0 FAIL，exit 0 | `c1-6f7b37e/test-race.txt` |
| 同上（rtk 壓縮後的輸出，保留） | `ebb6956` | 「24 passed」，exit 0 | `c1-ebb6956/test-race.txt` |
| benchmark（本機 amd64） | `ebb6956` | Issue 4.5 ms、Derive 11.6 ms、Verify 7.8 ms／次（8 個欄位、揭露 6 個） | `c1-ebb6956/bench.txt` |
| 反向 7 項 | `ebb6956` | **6／7**。「holder binding」**沒有抓到**：突變後 `hp` 沒有被使用，建置失敗（紀錄保留） | `c1-ebb6956/reverse.txt`、`reverse/` |
| 反向「holder binding」改為 `false && …` 後單獨補跑 | `6f7b37e` | 1／1，`holder signature by another key: <nil>` | `c1-6f7b37e/reverse-holder.txt` |
| 實際編譯進 spike 的模組與授權 | `6f7b37e` | 全部是 Apache-2.0 或 BSD 類 | `c1-6f7b37e/modules.txt` |

重跑：
- `cd spikes/bbs && GOFLAGS=-mod=readonly GOPROXY=off go test -race ./...`
- `python3 -I spikes/bbs/reverse.py <out> [name]`

依賴（spike module 內）：
- trustbloc v1.0.4；
- 間接：IBM/mathlib（**未打 tag 的 pseudo-version** `v0.0.3-0.20260123144934-21c357e3e46f`）、gnark-crypto v0.19.2、kilic/bls12-381 v0.1.0、fabric-amcl、bitset、pkg/errors、x/crypto v0.47.0、x/sys v0.40.0。

## 5 發現

1. **`VerifyProof` 會就地改寫傳入的 proof 位元組**（`TestLibraryInput` 的 log：`input proof rewritten: true`）。
   - spike 一開始的持有人綁定檢查，因此對正確的出示也失敗。
   - 修正：驗證端先算好持有人簽章要用的雜湊，再把 proof 的**複本**交給函式庫。
   - C2 的每個呼叫端都要這樣做，或在包裝層統一處理。
2. **持有人綁定是揭露 holder 公鑰**。函式庫沒有 blind signing，無法把持有人秘密放進承諾裡，所以同一張憑證的多次出示，可以用 holder 欄位串起來。proof 位元組本身每次都不同（`TestProofsDiffer`）。
3. **沒有 header／presentation header**：情境綁定全部放進 nonce。spike 用 verifier、challenge、intent 的雜湊作為 nonce，負例證明換掉任何一項都會讓 proof 失敗。
4. **撤銷**：spike 只做「揭露 status 索引，由 verifier 持有已撤銷的集合」。撤銷清單從哪裡來、怎麼更新，留給 C2。
5. **challenge 只能用一次**：spike 只記在記憶體裡（`ponytail:` 註記）。持久化與跨程序保存留給 C2。
6. **IBM/mathlib 是 pseudo-version**，升級時要重跑全部測試。gnark-crypto 也在依賴裡；Z1 預期也會用到它，屆時要對齊版本。

## 6 C2 需要 Aaron 決定的事

| # | 決定 | 選項 | 建議 |
|---|---|---|---|
| C1-D1 | 憑證格式 | (a) 沿用舊版 BBS+（trustbloc），文件與畫面標明「BBS+（2020 系），不是 W3C `bbs-2023`」；(b) 自行在 gnark-crypto 上實作 IETF BBS，對照官方測試向量；(c) 用 cgo 接 Rust 的 IETF BBS 實作 | **(a)**：維護中、授權清楚、能力足夠 demo 使用。(b) 是自行實作密碼學，風險與工作量都高；(c) 增加跨語言建置與部署成本 |
| C1-D2 | 持有人綁定 | (a) 揭露 holder 公鑰，由持有人簽 nonce 與 proof（spike 的做法，可被串連）；(b) 等有 blind signing 的實作 | **(a)**：demo 只有單一 verifier；在畫面與文件上標明可被串連 |
| C1-D3 | 憑證外層 | spike 使用原始 `名稱=值` 訊息，不是 JSON-LD 的 VC Data Integrity | 由 C2 訂一份 `apa` 自己的正規化外層（版本、欄位順序、編碼），經 review 後才接 Policy |
| C1-D4 | 撤銷與 challenge 保存 | 狀態清單的來源、challenge 存放處 | C2 設計時一併決定 |

## 7 不在本片

- 沒有接 Policy、Store、Host、Signer、A2 或付款；
- 沒有改 `apa.pay.v1`、M1、正式授權條件或持有人信任根；
- 沒有發正式 consumer credential，主 `go.mod` 沒有變；
- 不標 C2 完成，也不開始 C2。
