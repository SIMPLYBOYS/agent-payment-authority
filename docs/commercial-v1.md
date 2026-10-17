# V1 語音核准三態

> 2026-10-15，Claude Code。依 vault〈Claude-Code-V1語音核准三態與評測派工-20261013〉與 Aaron「我們開始進行 V1 語音三態」。分支 `commercial-v1`（從 main `cfc5c28`）。
> **V1 Done（Aaron 10/16：「V1 先標記為完成，並做 merge」）；已 fast-forward merge 進本機 main，未 push。**
> 實作與評測完成；獨立上下文 review 無 must-fix，S1–S8 已處理（第 12 節）。實機輪（Aaron 的聲音與真實裝置）需另行同意與核准。 本片只用合成音訊、公開資料集與 virtual authenticator；不收 Aaron 的聲音、不碰真實裝置、不改 `/opt/apa-signer`。

## 1 Aaron 的核准（10/15）

- 範圍選 **B**（「我們走 option b」）：照 PRD 的停損，一次性挑戰語＋語音轉文字＋聲紋，不做抗合成。「合成語音」標為已知邊界。
- D4 選 **(a)**；D8「請開始下載」。

| # | 決定 |
|---|---|
| D1 | 預訓練模型，本機 CPU，不訓練：<br>• 語音轉文字：faster-whisper `small`（MIT）；<br>• 聲紋：SpeechBrain ECAPA-TDNN `spkrec-ecapa-voxceleb`（Apache-2.0）。<br>執行環境：Python 3.12＋PyTorch 2.2.2 CPU（Intel Mac 的最後一版），放 `var/`（git 忽略）。 |
| D2 | 本機 sidecar（Unix socket），只回分數、不做決定。由 Signer 呼叫，依門檻合併裁決；Host 只轉送錄音並記錄 Signer 的結果。不上傳任何第三方。 |
| D3 | Signer 在 challenge 加 `voice_nonce`，從這一筆請購產生挑戰語：金額、商家，加上一組由 nonce 決定的代碼。WebAuthn 的 challenge 涵蓋這個 nonce。三種結果：<br>• 通過：grant 的 `amr: ["webauthn","voice"]`，事件 `VOICE_CHALLENGE_VERIFIED`；<br>• 失敗（唸錯、重播、不是本人）：不發 grant、進 Denied，事件 `VOICE_CHALLENGE_FAILED`＋原因；<br>• 不可用：摘要確認＋WebAuthn 仍可核准，grant 記 `voice=unavailable`，事件 `VOICE_UNAVAILABLE`，畫面明標「降級」。 |
| D4 | 「不可用」只有這幾種：sidecar 連不上、健康檢查失敗、10 秒內沒有結果；**沒有麥克風或拒絕麥克風權限也算不可用**（選 a），一樣明標降級並計入降級率。逾時後才到的結果不改變已發的 grant，只記為晚到。 |
| D5 | 門檻在評測前固定：<br>• 語音轉文字：金額、商家、代碼三項都對（結構化比對，不比全文）；<br>• 聲紋：調參組上 FAR＝1% 的分數。<br>門檻和模型版本寫進設定；之後要調低須 Aaron 核准。<br>• **2026-10-16 修訂（Aaron：「我覺得可以放寬比對規則」）**：只放寬商家，拼音音節允許每 4 個錯、漏或多 1 個，標點與數字不算音節（`merchantNear`，d0e1328）；金額與代碼仍須完全相同（代碼是防重播用，錯一位數會讓舊錄音被接受的機率從萬分之一升到約萬分之 37）。 |
| D6 | 資料：<br>• 自動化測試：macOS 內建 TTS 在本機產生（國語、英文多個聲音），當作不同說話者；<br>• 評測：LibriSpeech test-clean（CC BY 4.0），按說話者切成調參組與評測組，兩組不重疊，報 FAR／FRR 並標明只適用於該資料集；<br>• Aaron 本人的聲音：本片不收。 |
| D7 | 錄音只在記憶體，評分後丟棄；不寫進 log、證據、資料庫或 trace。只存結果碼、分數、門檻、模型版本、錄音的 SHA-256（擋重播）。轉寫文字不存。聲紋模板加密存放，可用指令刪除。 |
| D8 | 下載：PyPI、Hugging Face（兩個模型）、openslr（LibriSpeech test-clean），約 2–3 GB，放 `var/voice/`。版本與 sha256 記在第 2 節；之後全部離線執行。 |
| D9 | 只接 A2 的本人裝置核准；模擬核准標「語音未評估」。實機輪（Aaron 的聲音與真實裝置）等 review 後另外核准。 |

## 2 工具與資料（下載紀錄，10/15）

都在 `var/voice/`（git 忽略），合計約 3.3 GB（含 uv 下載快取）。下載後全部離線執行（`HF_HUB_OFFLINE=1`）。

| 項目 | 版本／來源 | 授權 | 核對 |
|---|---|---|---|
| Python 環境 | Python 3.12.6（Homebrew），uv venv `var/voice/.venv`；鎖定檔 `var/voice/requirements.txt` | — | sha256 `7069fd96…2964d9` |
| 主要套件 | torch 2.2.2、torchaudio 2.2.2（CPU）、speechbrain 1.0.3、faster-whisper 1.1.1、ctranslate2 4.8.3、numpy 1.26.4、huggingface-hub 0.36.2 | BSD／Apache-2.0／MIT | PyPI |
| 語音轉文字模型 | Hugging Face `Systran/faster-whisper-small`，commit `536b0662742c02347bc0e980a01041f333bce120`，473 MB | MIT | 固定 commit |
| 聲紋模型 | Hugging Face `speechbrain/spkrec-ecapa-voxceleb`，commit `0f99f2d0ebe89ac095bcc5903c4dd8f72b367286`，92 MB | Apache-2.0 | 固定 commit |
| 評測資料 | openslr 12 LibriSpeech `test-clean.tar.gz`（330.6 MB；40 位說話者、2620 段） | CC BY 4.0 | md5 `32fa31d2…f4849a9`，與 openslr 公布的 md5sum 相同 |

**安裝過程的修正**（照實記錄）：
- huggingface-hub 2.x 不帶 `requests`，也不是這兩個套件預期的 API，所以鎖在 1.0 以下。
- Hugging Face 的 xet 下載用戶端連不上（沒有走沙箱的代理），改用一般 HTTP（`HF_HUB_DISABLE_XET=1`）。
- 想把 `av` 鎖在 15 以下，但這台機器沒有 14.x 的預編譯版本，已撤回。sidecar 自己用 soundfile 把錄音解碼成 PCM 再交給模型，不經過 `av`。
- SpeechBrain 的設定檔把權重路徑寫成 Hub 名稱，載入時用 `pretrained_path` 改指向本機目錄。
- ctranslate2 與 PyTorch 在 Intel Mac 上各帶一份 OpenMP runtime，放在同一個行程會衝突。所以語音轉文字和聲紋各用一個獨立行程。不採用 `KMP_DUPLICATE_LIB_OK`，因為官方說明它可能讓結果出錯。

**冒煙測試**（離線；macOS TTS 美佳 zh_TW 與 Ting-Ting zh_CN 唸「核准 0.36 test USDC，付給巷口便利商店，代碼 4712」）：
- 語音轉文字：金額 0.36、代碼 4712 兩個聲音都對；商家名稱出現同音字（巷口→相口／向口、商店→商点），輸出是簡體。每段約 3.5 秒（CPU）。所以商家比對要比讀音，不能比字形。
- 聲紋：兩個不同聲音的 cosine 相似度 0.515（門檻待評測決定）。

加了 `pypinyin` 0.55.0（MIT，PyPI）：sidecar 用它把轉寫轉成不帶聲調的拼音，商家依讀音比對。

## 3 架構與介面

```
頁面 ── 錄音（16 kHz WAV，最多 10 秒）＋WebAuthn 簽章 ──▶ Host ──▶ Signer ──▶ 語音 sidecar（只回分數）
                                                        │        └─ 依門檻判定：通過／失敗／不可用
                                                        └─ 記錄 Signer 的結果：grant、事件、拒絕
```

- **sidecar**（`voice/sidecar.py`）：HTTP over Unix socket，呼叫者只有 Signer。
  - `GET /v1/health` 回報兩個模型的固定版本；
  - `POST /v1/analyze` 收 WAV，回轉寫、拼音、192 維聲紋向量；格式不對或太長回 400。
  - 兩個模型各在一個子行程（OpenMP 衝突，第 2 節）。
  - 不寫 log、不落地任何錄音或文字。
- **Signer 設定**（`signer.json` 的 `voice`）：
  - `socket`；`timeout_ms`（1–10000）；`speaker_threshold_milli`（千分位，canonical JSON 不允許小數）；
  - `asr_model`、`speaker_model`：sidecar 回報的版本必須相同，否則當作服務不可用；
  - `decimals`；`merchants`：每個 `pay_to` 的唸法名稱和拼音。挑戰語的商家名稱來自這裡，不信任頁面送來的文字。
  - 有 `voice` 時，Signer 每個請求的時限是 5 秒加上 `timeout_ms`；Host 呼叫 Signer 的時限放寬到 20 秒。
- **sidecar 的位置**（review S4）：Signer 只連「屬於 Signer 自己的使用者、其他人不能寫」的目錄裡的 socket，而且連上後確認對方行程的 uid 是同一個使用者，否則不送錄音、記為不可用。每個請求都重新連線檢查。
- **Signer 流程**：
  - 核准的 challenge 才加 `voice_nonce`（拒絕不用語音），而且 WebAuthn 的 challenge 涵蓋它；
  - `display.voice` 帶挑戰語，例如「核准 0.5 USDC，付給示意商家，代碼 9 7 4 0」；
  - verify 先驗 WebAuthn，再判語音。
  - 核准者沒有可用的聲紋模板：`VOICE_NOT_ENROLLED`。模板不存在、解不開、金鑰檔權限不對，或模板是別的聲紋模型做的，都算沒有。收款人沒有唸法：`VOICE_MERCHANT_UNKNOWN`。兩者都直接拒絕發 challenge。
  - verify 時，Signer 自己出問題（金鑰、模板、自己的資料庫）回 `VOICE_SIGNER_UNAVAILABLE`：這次不核准、challenge 保留，**不會**變成降級核准。
  - 錄音不是頁面的格式（16 kHz 單聲道 16-bit、0.5–10 秒），Signer 不送 sidecar，直接判失敗（`VOICE_AUDIO_INVALID`）。
- **資料**：只新增兩張表（`voice_templates`、`voice_results`），舊的 `approvals.db` 不用遷移。
  - 模板是三段以上錄音聲紋向量的平均，用 AES-GCM 加密；金鑰是 Signer 目錄裡的 `voice.key`（0600）。
  - 結果只存結果碼、三個欄位對不對、分數、門檻、模型版本、錄音的 SHA-256。
- **管理指令**：
  - `apa-signer approver voice-enroll --dir DIR --approver ID WAV...`（至少三段；零向量、NaN 一律拒絕）；
  - `apa-signer approver voice-delete --dir DIR --approver ID`；
  - `apa-signer approver voice-stats --dir DIR`：依結果與原因列出次數，以及降級（不可用）的比例。
- **契約**：
  - 事件 `VOICE_CHALLENGE_VERIFIED`、`VOICE_CHALLENGE_FAILED`（`code`：`VOICE_REPLAY`／`VOICE_PHRASE_MISMATCH`／`VOICE_SPEAKER_MISMATCH`／`VOICE_AUDIO_INVALID`）、`VOICE_UNAVAILABLE`（`code`：`VOICE_DEVICE_UNAVAILABLE`／`VOICE_SERVICE_UNAVAILABLE`／`VOICE_TIMEOUT`）。每個核准會話最多一筆，出現在它導致的關閉之前。驗證器不強制「緊接」，中間可以有其他事件。grant 的 `voice` 與語音事件之間目前也沒有交叉比對。
  - 語音失敗只能接 `APPROVAL_DENIED`，原因 `VOICE_CHALLENGE_FAILED`，不帶核准方法；這個原因也只能出現在語音失敗之後。
  - webauthn grant：語音通過時 `amr: ["webauthn","voice"]`、`voice: "verified"`；不可用時 `amr: ["webauthn"]`、`voice: "unavailable"`＋`voice_reason`。
- **Host**：assertion 路由轉送 `voice`（上限 512 KB）。Signer 回語音失敗時，把這次嘗試標為 `closed`（保留錯誤碼），並把會話關成 denied（`resolved_by: signer-voice-check`）。Store 的復原檢查接受這種關閉。

## 4 三態規則（實作後）

| 結果 | 什麼時候 | 會話 | grant | 畫面 |
|---|---|---|---|---|
| 通過 | 金額、商家（比讀音）、代碼都對，而且聲紋 cosine ≥ 門檻 | 核准 | `amr` 含 voice | 「本人核准已驗證（裝置＋語音），付款未啟用。」 |
| 失敗 | 錄音的 SHA-256 用過（重播）；挑戰語不符；聲紋低於門檻；錄音格式不對或無法辨識 | 拒絕，之後不再發 challenge | 無 | 「語音挑戰未通過，這筆已拒絕。」加上原因 |
| 不可用 | sidecar 連不上、模型版本不對、回 5xx、沒有在時限內回答；socket 位置不可信；頁面沒有麥克風或被拒絕權限（D4 a） | 核准 | `voice: unavailable`＋原因 | 「本人核准已驗證（語音降級），付款未啟用。」加上原因 |
| （Signer 自身問題） | 金鑰、模板、Signer 資料庫出錯 | 不變（仍在等待） | 無 | 「Signer 讀不到聲紋設定，這次沒有核准（請找管理者）」 |

- 頁面錄音不到 1 秒不會送出，會請使用者重唸，這筆仍在等待。
- 逾時後才到的 sidecar 回應不會被讀取，因為請求已隨時限取消；但只要分析結果在時限內完整回來，就一定照結果判定（review S2）。
- 判定語音期間，challenge 若已被關閉或過期，回覆會是 `CHALLENGE_CLOSED`／`CHALLENGE_EXPIRED`，會話不會因語音被拒。這時語音結果不會回給頁面，就算通過也同樣會被拒，所以不洩漏任何資訊。

## 5 評測報告（`voice/eval.py`，`artifacts/v1/eval-1/v1-eval.json`）

本機、離線、預訓練模型；資料只有公開資料集和本機合成語音，沒有任何真人的私人錄音。所有數字都只適用於這些資料集。

**聲紋**（LibriSpeech test-clean，英文朗讀；40 位說話者按說話者分成調參組、評測組各 20 人，不重疊；每人用 3 段錄音建模板，另取 10 段當測試）：

| | 本人比對 | 冒名比對 | FAR（95% CI） | FRR（95% CI） | EER（近似） |
|---|---|---|---|---|---|
| 調參組（決定門檻） | 200 | 3800 | 0.97%（0.71–1.34%） | 0.5%（0.09–2.78%） | 0.5% |
| 評測組 | 200 | 3800 | **0.58%（0.38–0.88%）** | **1.0%（0.27–3.57%）** | 0.61% |

括號是**以單次比對計**的 Wilson 95% 區間（review S8）：3800 次冒名比對只來自 20 位說話者、190 組配對，彼此並不獨立，所以實際的不確定性比這些區間大。若要以說話者為單位估計，需要依說話者 bootstrap，本片沒有做。

門檻是調參組 FAR ≤ 1% 的最低 cosine，取千分位並往上進位：**0.365**（`speaker_threshold_milli: 365`）。

**挑戰語**（macOS TTS 美佳 zh_TW、Ting-Ting zh_CN 各 25 句；金額、代碼隨機，商家三選一；走真的 sidecar，比對用 Signer 自己的程式碼）：
- 通過 47/50（94%，95% CI 84–98%）。金額 50/50、商家 50/50；3 次錯在代碼的數字（美佳 1、Ting-Ting 2）。
- 也就是說，本人唸對了，約有 6% 會因為辨識錯一個數字被判成失敗，而失敗會讓這筆被拒絕。這是 B 範圍下的主要誤拒來源。
- 延遲：中位數 3.7 秒、p95 5.5 秒、最長 8.1 秒；超過 10 秒時限 0/50，所以本批降級率 0%。最長值已接近時限。

**重播**：每段錄音拿去比對另一個 challenge 的代碼，50/50 被擋下。這只驗證了「比對邏輯會拒絕不同的四位代碼」（review S8），不是抗重播能力的實證；隨機猜中代碼的機率是萬分之一。位元組完全相同的重送由錄音 SHA-256 擋下，Go 測試已涵蓋。

**合成聲音冒充**：兩個 macOS 國語聲音互相拿對方的模板比對，全部通過（22/22、22/22）。同一套 TTS 引擎的不同聲音，ECAPA 分不出來；本片不做抗合成，這是範圍 B 寫明的已知邊界。所以：
- 瀏覽器檢查裡的「別的說話者」改用 LibriSpeech 真人錄音建模板，不用另一個 TTS 聲音；
- 不能宣稱語音能擋下合成語音冒充。

## 6 風險與失敗案例

- **合成語音**（已知邊界）：任何能產生核准者聲音的 TTS 或語音轉換，都可能通過聲紋。擋得住的是重播（一次性代碼）和不同的真人（依英文資料集的數字）。
- **誤拒**：辨識錯數字約 6%（TTS 國語樣本），加上聲紋 FRR 約 1%（英文資料）。失敗會讓這筆被拒；要重來只能重新請購。
- **降級可以被選，所以語音擋不住拿到裝置的人**（review S1）：頁面回報「沒有麥克風」就會降級（D4 選 a）。這表示拿到核准者裝置的人，只要拒絕麥克風權限，就能只靠 WebAuthn 核准。在下游拒絕或限制 `voice=unavailable` 之前，語音對「裝置持有者」沒有保護作用；它目前保護的是：
  - 在有唸的情況下，擋重播和不同的真人；
  - 在紀錄上，讓降級看得見。
  - 降級仍要求摘要確認和 WebAuthn，畫面和紀錄都會標明。降級比例用 `apa-signer approver voice-stats` 計算。
- **給 P2 付款路徑**：付款時若要看語音結果，必須讀 **Signer 自己的 `voice_results`**，不能讀 grant 的 `voice` 欄位，因為 grant 是 Host 寫的。
- **延遲**：Intel CPU 上最長 8.1 秒，接近 10 秒時限；機器忙的時候，降級會變多。
- **國語與真人**：聲紋門檻是用英文朗讀資料定的；國語真人（包含 Aaron 本人）的 FAR／FRR 還沒有量。
- **信任**：頁面由 Host 提供，Host 可以顯示一句話、卻送另一段錄音。挑戰語由 Signer 依 `voice_nonce` 產生並比對，所以送錯的錄音會被判失敗，不會變成通過。
- **時限**：serve-approvals 每個請求的時限多了語音的 10 秒（付款 Signer 維持 5 秒）；Host 的用戶端放寬到 20 秒。
- **Host 的鎖**：Host 的核准路由共用一把鎖，一次語音判定最多佔用 15–20 秒，期間所有客人的核准操作都要排隊。sidecar 每個模型一次也只處理一個請求，所以兩位核准者同時核准時，後到的比較容易逾時、變成降級。
- **sidecar 停掉之後**：模型 worker 掛掉時 sidecar 會整個退出，需要外部的 supervisor 重新啟動；在那之前每次核准都會降級。Signer 放棄等待後，sidecar 仍會把那次分析做完，下一個請求要排在它後面。
- **未測到的情況**：
  - 同一段錄音同時用在兩個 challenge 時，唯一索引會擋下，但回覆的是一般的 Signer 錯誤，沒有測試；
  - 結果剛好在時限邊界回來（S2）沒有可重現的測試；
  - sidecar 屬於別的使用者的情況，在單一使用者的測試環境裡無法建立；
  - 回覆遺失後，Host 經由狀態路由找回語音失敗，沒有測試。

## 7 畫面文字

- 錄音中：「請唸出下面這句（錄音中，最多 10 秒；錄音只用來比對，不會保存）」，下面是挑戰語，以及「唸完了」按鈕。
- 錄音太短：「錄音不到 1 秒，沒有送出；這筆仍在等你決定，請再按核准並唸完整句。」
- 三種結果的標題見第 4 節。grant 說明會加「語音挑戰通過」或「降級：語音不可用（原因）」。
- 證據抽屜「裁決」區有「語音挑戰」列，來源是那筆語音事件。
- 原因文字：
  - 錄音是重播（這段錄音已用過）
  - 唸的內容和這筆不符（金額、商家或代碼）
  - 聲音不是已登記的核准者
  - 錄音無法辨識
  - 沒有麥克風，或沒有允許使用麥克風
  - 語音服務無法使用
  - 語音服務逾時

## 8 操作步驟

1. 準備環境（第 2 節，已完成）：`var/voice/.venv`、`var/voice/models`、`var/voice/data`。
2. 啟動 sidecar：

   ```
   var/voice/.venv/bin/python -I voice/sidecar.py --socket <socket> --models var/voice/models
   ```

3. 在 `signer.json` 加 `voice` 段：`asr_model`、`speaker_model` 填 sidecar health 回報的版本；`speaker_threshold_milli` 填 365。
4. 登記聲紋：`apa-signer approver voice-enroll --dir DIR --approver ID a.wav b.wav c.wav`。之後重新啟動 `serve-approvals`。
5. 評測：

   ```
   voice/eval.py --data var/voice/data/LibriSpeech/test-clean --models var/voice/models --socket <socket> --out DIR
   ```

6. 瀏覽器檢查：`node scripts/verify/v1-ui.mjs --threshold-milli 365`。它會自己啟動 sidecar、Signer 和 demo host。

本片沒有動 `/opt/apa-signer`。要在那裡啟用語音，需要你核准後另外部署：新程式、`voice` 段，以及你本人的聲紋登記。

## 9 驗證紀錄

失敗照實保留；修正後只重跑受影響的部分。證據在 `artifacts/v1/`（git 忽略）。

- **Go**（be3030e 起）：
  - Signer 的語音測試用回應寫死的假 sidecar，涵蓋通過、重送、換人、代碼／金額／商家不符、錄音無法辨識、重播、逾時、模型版本不符、服務停掉、沒有麥克風、缺語音、拒絕時不收語音、沒有模板、刪除模板，以及資料庫裡沒有錄音和轉寫。
  - Host 的 V1 測試走真的 Signer socket：通過、失敗、降級，重新啟動後仍能通過復原檢查。
  - paycontract：grant 的語音欄位、事件順序與形狀。
- **全套**（4627841）：`go vet` 通過；`go test ./...` 1241 通過（略過已知的 `TestTimingInterleaved`）。
  - 9ce7d0b 那一輪全套有 1 項失敗：apa-buyer `TestLiveOptIn`，是全套並行時版本檢查逾時；單獨重跑 1/1 通過。
- **網頁單元測試**：99/99。
- **反向突變**（4627841）：V1 新增 10 項，10 KILLED（`artifacts/v1/mutations-4627841.txt`）。
- **評測**（df58759 的腳本）：見第 5 節。
- **瀏覽器**（`scripts/verify/v1-ui.mjs`；virtual authenticator；核准者的聲音是 macOS TTS 美佳；「別的說話者」是 LibriSpeech 121 號的真人錄音）：
  - 第 1、2 輪（df58759，`ui-1`、`ui-2`）：**FAIL (7)**。每次附錄音的 verify，Signer 都回 `BAD_REQUEST`，接著會話過期。原因是 Signer 的 approvals 路由只讀 64 KiB；Go 測試只送幾個位元組，沒經過 HTTP，所以沒抓到。同時發現 Signer 每個請求的時限 5 秒、Host 用戶端 10 秒，會把語音判定切短。另外，降級時面板標題沒有寫「降級」。
  - 4826e0d 修正：verify 上限改 512 KiB；有語音時放寬時限；新增 `TestV1VoiceRealSizeAndSlow`（320 KB 錄音、sidecar 6 秒）；面板標題改依語音結果寫。
  - 第 3 輪（`ui-3`）：**FAIL (1)**。重播那一項判成「內容不符」而不是「重播」：錄音經過麥克風重錄，位元組不同，雜湊對不上，但舊代碼對不上這次的 challenge，所以仍被拒、沒有 grant。是檢查的預期寫錯了，改成檢查「因舊代碼被拒」（8b6f802）。
  - 第 4 輪（`ui-4`，未提交的工作樹）：PASS（24 項）。
  - 第 5 輪（8b6f802 乾淨工作樹，`ui-5-clean`）：**PASS（24 項）**。包含：通過、換人被拒、重播被拒、沒有麥克風降級、sidecar 停掉降級、手機 390 px 沒有橫向捲動、沒有頁面錯誤。
    - 其中「資料庫和 log 裡找不到錄音」那一項，review 指出（S7）是拿原始 TTS 檔去搜，但頁面送出的是重新錄製、重新取樣後的位元組，所以這項檢查**本來就不可能找到**，不能算數。那時這個結論只有 Go 測試在支撐。
  - 第 6 輪（f338827 乾淨工作樹，`ui-6-clean`）：**PASS（25 項）**。改用 CDP 抓下頁面實際送出的 4 段錄音，拿原始位元組和 base64 片段去搜兩個資料庫、Host log 和 Signer log，都找不到。

## 10 結果的來源

| 類別 | 本片有的 | 本片沒有的 |
|---|---|---|
| 離線 fixture | Go 測試裡回應寫死的假 sidecar | — |
| virtual | CDP virtual authenticator；瀏覽器的「麥克風」是 TTS 錄音的串流 | 真實的 authenticator、真實的麥克風 |
| 本機預訓練模型 | sidecar（faster-whisper small、ECAPA）的評測與瀏覽器檢查 | — |
| 實機 | 無 | Aaron 的聲音、真實裝置：等 review 後另外核准 |
| 外部模型或服務 | 無（全部本機、離線） | — |

## 11 沒有做的

- 抗合成（範圍 B 的停損）。
- 國語真人的 FAR／FRR，以及 Aaron 本人的聲紋登記與實機輪：都需要先取得同意並另外核准。
- `/opt/apa-signer` 的部署。
- 模擬核准的路徑不接語音（D9），畫面上標「語音未評估」。

## 12 獨立上下文 review（Claude Code subagent，10/16）

結論：目前範圍內（付款未啟用、D4 a 已接受）沒有 must-fix；實機輪之前要先處理 S1–S8。
- 確認：語音失敗不會在 Signer、Store、Host 或頁面變成核准；因語音被拒的會話不能重開。
- 也指出：除了 D4 a，還有四條路能把失敗變成「不可用」，或讓每次核准都變成「不可用」（S2–S5）。

| 項目 | 問題 | 處理 |
|---|---|---|
| S1 | 文件高估了語音能擋的東西；「計入降級率」沒有實作 | 第 6 節改寫：語音擋不住拿到裝置的人，也寫明 P2 要讀 Signer 的 `voice_results`。加 `voice-stats` 計算降級比例（5d40ff1） |
| S2 | 分析剛好在時限時回來，會被判成逾時（降級） | 有結果就照結果判定（5d40ff1）。沒有可重現的測試 |
| S3 | 金鑰、模板出問題時一律降級；登記時零向量會產生壞模板；模板的模型版本沒有比對 | 發 challenge 時檢查模板（含模型版本）；verify 時回 `VOICE_SIGNER_UNAVAILABLE`；登記時拒絕零向量、NaN（5d40ff1、f338827） |
| S4 | Signer 會把錄音送給任何綁在那個 socket 上的行程 | 檢查目錄擁有者與權限、對方行程 uid；每個請求重新連線（5d40ff1） |
| S5 | sidecar 例外與 worker 死掉都會變成降級 | Signer 先自己檢查 WAV 格式；sidecar 解析例外回 400、模型錯誤回 500、worker 死掉就退出（5d40ff1） |
| S6 | 付款 Signer 的時限被意外改了 | 只有 serve-approvals 加語音時間（5d40ff1） |
| S7 | 瀏覽器檢查的「找不到錄音」本來就不可能找到 | 改搜頁面實際送出的錄音，並納入 Signer 的 log（第 6 輪 PASS） |
| S8 | 信賴區間太窄；重播數字被說得太強 | 第 5 節改寫 |

附註：
- 已處理：NaN 分數不再算通過；查重播時資料庫出錯不再忽略。
- 寫進第 6 節：Host 的鎖、同一錄音同時用於兩個 challenge、challenge 在判定中被關閉、事件順序只檢查「之前」、grant 與事件沒有交叉比對、模型以 revision 字串固定。

修正後的驗證：
- 反向突變（V1 共 16 項）：
  - 5d40ff1 上 15 KILLED，#335（零向量）SURVIVED。拿掉零向量檢查後，NaN 讓 JSON 編碼失敗，測試只檢查「有錯誤」，分不出是哪一層擋下。
  - f338827 把測試改成檢查錯誤原因後，#335 KILLED。
- Signer、Host 的 V1 測試全過，新增 `TestVoiceSignerTroubleAndInputs`。
- Go 全套第一次：1 項失敗，`internal/buyer TestProbeOneMessage`（假 CLI 沒收到訊息，與 V1 無關）。單獨重跑 3/3 通過，屬於全套並行時的時序問題。
- 網頁 99/99。
- 瀏覽器第 6 輪 PASS（25 項）。

review 時的意外，照它的回報記錄：
- 它曾在沙箱外對 repo 的 `internal/store/approvals.go` 套用一次突變後還原，已確認與 commit 相同；
- 它的一次 `grep -r` 走進了 `var/`，只搜一個事件名稱，沒有讀出任何語音資料；
- 它留下 `$TMPDIR/approvals.go.orig` 等暫存檔，沒有放進 repo。

## 13 開發模式實機輪（A）：操作步驟

Aaron 10/16「先做開發模式（A）」。這一輪用你的帳號在 `var/v1-dev/`（git 忽略、權限 0700）跑 sidecar 和 `serve-approvals`。不碰 `/opt/apa-signer`、不需要 sudo、不建立付款金鑰、不付款。

這一輪會做兩件需要你同意的事：
- **錄你的聲音建立聲紋模板**：錄音檔登記完就刪除；模板加密存在 `var/v1-dev/data`。
- **在 Google 密碼管理工具新增一把 `localhost` 的 passkey**：這是開發模式自己的 Signer 用的，跟 A2 在 `/opt` 的那把分開。

以下步驟都在 repo 根目錄執行，請依編號順序做。

**終端機 1：啟動**

1. `sh scripts/demo/v1-dev.sh`
   - 看到 `v1-dev: ready` 就是好了；
   - 讓它一直開著，結束時按 Ctrl-C。

**終端機 2：登記**（設 `A=var/v1-dev/bin/apa-signer`、`D=var/v1-dev/data`）

2. **錄音並登記聲紋**：
   - 先確認麥克風編號：`ffmpeg -hide_banner -f avfoundation -list_devices true -i ""`。下面假設是 `:0`；第一次錄音時，macOS 可能會問終端機能不能用麥克風。
   - 建一個私人暫存目錄：

     ```
     R=$(mktemp -d); chmod 700 "$R"
     ```

   - 錄 3 段，每段約 6 秒，用一般說話的音量和速度唸國語：

     ```
     ffmpeg -hide_banner -loglevel error -f avfoundation -i ":0" -ar 16000 -ac 1 -t 6 "$R/1.wav"
     ```

     唸：「我是核准者，這是登記用的第一句話。」

     ```
     ffmpeg -hide_banner -loglevel error -f avfoundation -i ":0" -ar 16000 -ac 1 -t 6 "$R/2.wav"
     ```

     唸：「今天天氣很好，我們去買咖啡豆。」

     ```
     ffmpeg -hide_banner -loglevel error -f avfoundation -i ":0" -ar 16000 -ac 1 -t 6 "$R/3.wav"
     ```

     唸：「請確認金額與商家，再按下核准。」
   - 登記，然後立刻刪除錄音：

     ```
     $A approver voice-enroll --dir $D --approver approver-a "$R/1.wav" "$R/2.wav" "$R/3.wav"
     rm -P "$R"/*.wav && rmdir "$R"
     ```

   - 應該看到 `voice template of approver-a from 3 recordings`。這一行可以貼回來。
3. **裝置登記，第一段（終端機）**：`$A approver enroll begin --dir $D --approver approver-a`
   - 輸出含**一次性代碼**，請不要貼回來；
   - 記下登記 ID（`enr-…`）和代碼。
4. **裝置登記，第二段（瀏覽器）**：用 Chrome 打開 `http://localhost:8402/approve/enroll`。
   - 這一步要等第 6 步的 Host 啟動後才能打開；如果還沒啟動，先做第 6 步再回來。
   - 輸入登記 ID 和代碼，選 Google 密碼管理工具，用 PIN 完成。
   - 頁面會顯示一個指紋。
5. **裝置登記，第三段（終端機）**：一定要在第 4 步完成之後才執行。

   ```
   $A approver enroll confirm --dir $D --id enr-…
   ```

   核對終端機和頁面的指紋相同後，輸入 `yes`。

**終端機 3：Host**

6. 執行下面這行，會打開 `http://localhost:8402/host`：

   ```
   make present-a2 SIGNER=$PWD/var/v1-dev/run/signer.sock REGISTRY=$PWD/var/v1-dev/registry.json
   ```

   - 頁面要你輸入終端機上的配對碼，**配對碼不要貼回來**；
   - 第一次核准時，Chrome 會問能不能用麥克風，請按允許；
   - 如果 macOS 沒有允許 Chrome 使用麥克風，要到「系統設定 → 隱私權與安全性 → 麥克風」打開。

**試用**（每一筆都是「以 A 模擬」→「建立委託」→「確認並開始」→ 等「需要你處理」）

- **Host 資料庫沿用（Aaron 2026-10-16：「讓 present-a2 在多次啟動之間保留 Host 資料庫，讓 task 不會重複使用」）。**
  - 先前的問題：每次啟動 Host 都開一個新的資料庫，task 又從 `show-a-01` 開始。會話 ID 由 task 算出（`SessionID(task, po)`），而 Signer 會保留每個已決定的會話，所以重啟後被 Signer 以 `SESSION_DECIDED` 拒絕（實機試用時遇到：show-a-01 到 06）。
  - 現在：`make present-a2` 的 Host 資料庫和 trace 改放 `var/a2-host-<Signer socket 路徑的 12 位 sha256>/`，每次啟動都沿用；配對 token 和建置的程式仍在每次新的 `var/present-XXXXXX`。啟動時終端機會印出「A2 的 Host 資料庫沿用 …」。
  - 一個 Signer 對應一個 Host 資料庫：開發模式（`var/v1-dev/run/signer.sock`）和 /opt 各用各的。
  - 開發模式那份（`var/a2-host-b0fa460f7678`）先放進了舊場次 `var/present-tWKEpN` 的資料庫與 trace（`.backup`，已用 show-a-01 到 06），所以下一筆是 show-a-07。
  - 上限：每位客人 20 個 task（registry `show-a-01` 到 `show-a-20`），在同一個 Signer 的生命週期內用完就是 `NO_FREE_TASK`。刪掉 `var/a2-host-…` 而 Signer 資料還在，撞號就會回來。
  - 2026-10-16 實機試用時 consumer-a 的 20 個 task 全部用完（頁面顯示「展示 task 已用完」）。開發模式改用擴充 registry：`v1-dev.sh` 每次啟動都會產生 `var/v1-dev/registry.json`，內容是原本的 registry 再加上 `show-a/b-21` 到 `100`（不縮排，因為 Host 讀 registry 的上限是 64 KiB）。Host 改用 `make present-a2 SIGNER=… REGISTRY=$PWD/var/v1-dev/registry.json` 啟動（`present.sh` 的 `APA_PRESENT_REGISTRY`，只用於 fake）。已提交的 `testdata/demo/registry.json` 不變。
  - 已實測：在用完 20 個 task 的資料庫副本上，用擴充 registry 啟動 Host 會正常就緒；透過 API 建立委託並開始一筆，被選中的是 `show-a-21`。
  - 這些都只是 demo 的做法；正式的 Host 本來就只有一個長期資料庫。

7. **唸對 5 次**：
   - 按「用已登記的裝置核准」→ 唸畫面上的整句（含四位代碼）→「唸完了」→ PIN；
   - 預期看到「本人核准已驗證（裝置＋語音），付款未啟用。」；
   - 記下每次是通過，還是「語音挑戰未通過（原因）」。
8. **故意唸錯一次**：例如把代碼唸成別的數字。預期看到「語音挑戰未通過（唸的內容和這筆不符…），這筆已拒絕」。
9. **拒絕麥克風一次**：
   - 在 Chrome 網址列左邊的網站設定，把 `localhost:8402` 的麥克風改成「封鎖」，再按核准並完成 PIN；
   - 預期看到「本人核准已驗證（語音降級），付款未啟用。」；
   - 做完把麥克風改回「詢問」或「允許」。
10. **看結果**：`$A approver voice-stats --dir $D`。輸出只有次數，可以整段貼回來。

**結束與清理**

11. 在終端機 3、終端機 1 按 Ctrl-C。
12. 要不要清理由你決定：
    - 聲紋模板：`$A approver voice-delete --dir $D --approver approver-a`；
    - 整個開發模式資料：刪掉 `var/v1-dev/`，含 `approvals.db`、`voice.key`；
    - Google 密碼管理工具裡那把 `localhost` passkey：你自己辨認後再決定要不要刪除。

**回報時只需要**：第 2 步的那一行、第 7–9 步每次的結果、第 10 步的輸出。不要貼一次性代碼、配對碼或整段終端機輸出。

**注意**：
- 門檻 0.365 是用英文資料定的，這一輪是你本人國語的第一次實測。
- 評測裡約 6% 會因為數字辨識錯而被拒；被拒的那一筆不能再核准，要重新請購。

## 14 開發模式實機輪（A）：結果（2026-10-16）

**環境**：Aaron 自己的 Intel Mac、自己的 Chrome 設定檔；Google 密碼管理工具的 `localhost` passkey；開發模式 Signer（`var/v1-dev`，門檻 365/1000）。以下是**真實裝置**的結果，不是 TTS 或虛擬驗證器。只記次數與比對結果，沒有保存錄音或轉寫。

**一、修正之前（02:27–17:22，共 18 次）：只有 0 次通過**。一路找到並修正的原因，依發現順序：

| 原因 | 修正 |
|---|---|
| `enroll begin` 遇到「已完成、待確認」的登記會違反資料表 CHECK | 92261da |
| 挑戰語出現時就寫「錄音中」，但麥克風還沒打開，開頭會漏掉 | 5fdb1fb |
| 商家「示意商家」容易聽成「11电价」 | 876b0c7（改成「測試商店」） |
| Host 重啟後 task 從 show-a-01 重算，會話 ID 撞上 Signer 已決定的紀錄（`SESSION_DECIDED`） | 6672020（present-a2 保留 Host 資料庫） |
| ScriptProcessor 在主執行緒上，Unity 一忙，錄音內容就亂了（長度不變） | f1efa24（改用 MediaRecorder） |
| Chrome 用了收音差的預設麥克風 | 15662e8（面板可選麥克風） |
| 挑戰語排在面板最後，又被 `keepFocus` 捲出畫面，結果唸到的是上方的說明文字（「…最多10秒…」） | 7b66242 |
| 20 個展示 task 用完 | a56c522（開發用的擴充 registry） |

這段期間的聲紋分數：預設麥克風 0.19–0.37，wireless microphone 0.60–0.65。

**二、修正之後、商家放寬之前（18:10–18:15，5 次）**：2 次通過，3 次內容不符。3 次都錯在商家，其中 2 次代碼也錯（Aaron：不小心唸錯）。Aaron 因此決定放寬比對規則：只放寬商家，見 D5 修訂（d0e1328）。

**三、商家放寬之後（18:27–18:33，7 次）**：

| 結果 | 次數 | 說明 |
|---|---|---|
| 通過（裝置＋語音） | 4 | 三項都對；聲紋 0.59–0.66 |
| 內容不符 | 1 | 18:33:15：金額 ✗、商家 ✓、代碼 ✓；聲紋 0.568 |
| 不可用（裝置） | 1 | 18:31:40：在 Chrome 封鎖麥克風，核准成立、標為降級（D4 a） |
| 不可用（逾時） | 1 | 18:32:26：分析超過 10 秒，核准成立、標為降級 |

- **聲紋**：本人 9 次都在 0.568–0.66，門檻 0.365。這一輪沒有測試他人冒用：他人與合成語音的結果，仍以 §5 的評測為準。
- **逾時**：這台機器開機後負載經常在 8–24，自動瀏覽器檢查也遇過同樣的逾時（ui-8、ui-10）。
- **`voice-stats` 總計**：30 筆，含修正前的失敗；降級 3 筆（10.0%）。

**尚未做**：§13 裡的「故意唸錯一次」還沒有單獨做，這輪的拒絕都是不小心唸錯的；代碼與金額仍然要完全一致，不能放寬。

## 15 /opt 正式部署（option B）：操作步驟

**決定**：Aaron「要做 /opt 的正式部署」；Python 來源選「下載獨立版 Python（建議）」。sudo 一律由 Aaron 輸入；Claude Code 不執行 sudo，也不動 `/opt`。

**延後（10/16）**：Aaron「先照你的建議進行」：`/opt` 部署延到 P2 部署時一起做，E1-b 之前必須完成。原因：AE1、P2 可能還會改 Signer，到時要重新建置執行檔（重跑 `scripts/demo/v1-opt-stage.sh`、重建 `apa-signer`、重新產生 `signer.json` 與核對值），而 P2 本來就要動 `/opt`；開發模式（`var/v1-dev`）在那之前可用於練習與展示，但它的 Signer 與語音服務以 mac 執行，沒有 P1-b 隔離，不能當作最終驗收的證據。下面的雜湊是 1942796 那一版的，部署前要更新。

**P2 帶來的部署變更（10/17，部署前照此更新本節的步驟與核對值）**：
- `signer.json` 的每個 approver 要加 `signer_total` 與 `signer_window_seconds`，新執行檔缺這兩個欄位就拒絕啟動（`docs/commercial-p2.md` §9.5）。
- 核准表搬進 `signer.db`。停掉 Signer 後，以 `apa-signer` 帳號執行一次 `apa-signer approver migrate --dir /opt/apa-signer/data`，舊檔保留為 `approvals.db.migrated`（§9.4）。
- 本節以下寫的 `approvals.db`、`signer.json` 與執行檔的 hash 都是 P2 之前的值，部署前要重新產生。

**為什麼不直接用開發模式的 venv**：語音服務必須以 `apa-signer` 執行（Signer 會檢查 socket 對端的 uid），所以它載入的任何東西都不能讓其他帳號改寫（P1-b 隔離）。Homebrew 的 Python、標準函式庫，以及 openssl、sqlite、xz、mpdecimal 都屬於 mac；wheel 裡還留著指向 `/usr/local/lib`（mac 的）和 `/tmp/vendor/lib`（任何人都能寫）的 rpath。所以改用 python-build-standalone 3.12.6，離線裝入固定版本的套件，刪掉所有絕對路徑的 rpath（16 個檔案）後重新 ad-hoc 簽章，再整包以 root 擁有的方式裝進 `/opt`（`scripts/demo/v1-opt-stage.sh`）。

**準備好的東西**（`var/v1-opt/`，git 忽略）：
- `apa-signer`：從 f81517a 的乾淨 worktree 建置（`GOFLAGS=-mod=readonly GOPROXY=off GOTOOLCHAIN=local`），SHA-256 `1012f3e91dc4275aa92a597ee72a4d48e71b204d5e461a4ec1bbe3511793b24e`；Go 程式與第二輪 review APPROVE 的 158b768 相同；
- `signer.json`：A2 的設定（`/opt` 上那份，SHA-256 `425e0070…2cef`）加上 `voice` 段（socket `/opt/apa-signer/voice-run/voice.sock`、門檻 365、商家「測試商店」），SHA-256 `f641ac0ebda6d0556fb85a8746ea4204d07f2ca433339f774ca930434b1568d0`；本機以暫存 socket 驗證過（`config-check.txt`）；
- `stage/voice/`：`python/`、`models/`、`sidecar.py`，共 20655 個檔案，清單 `stage/manifest.sha256`。

**本機已驗證**：
- 搬到別的路徑、改成唯讀、用 `env -i HOME=/var/empty` 從 `/` 啟動，健康檢查通過（模型載入約 130 秒）、分析正確（約 3.2 秒）；
- 新執行檔打開 A2 時期的 `approvals.db` 會補上 `voice_templates`、`voice_results`；舊執行檔（A2，9987eb4）也能打開補過表的資料庫，所以可以回滾；
- `/opt` 的 Signer 已決定過 show-a-01 到 03（10/13）：Host 資料庫 `var/a2-host-b6424086acbc` 已先接上 10/13 的場次 `var/present-XIxA0b`（schema 5，已用 `--upgrade-db` 升級，升級前的備份留著），下一筆會是 show-a-04。

**開始前**：停掉開發模式的 Host（終端機 3）；開發模式的語音服務與 Signer（終端機 1）建議也先停掉，因為兩組模型一起跑，這台機器容易逾時。以下的 `$P` 是 repo 目錄：`P=/Users/mac/Documents/OpenSourceProjects/agent-payment-authority`。

**D 部署（終端機 W-host，mac）**
1. 核對：

   ```
   shasum -a 256 $P/var/v1-opt/apa-signer        # 1012f3e91dc4275aa92a597ee72a4d48e71b204d5e461a4ec1bbe3511793b24e
   (cd $P/var/v1-opt/stage && shasum -a 256 -c manifest.sha256 | grep -vc ': OK$')   # 0
   pgrep -fl '/opt/apa-signer/'                    # 應無輸出
   sudo shasum -a 256 /opt/apa-signer/data/signer.json /opt/apa-signer/bin/apa-signer
   # signer.json 425e0070…2cef；apa-signer 9a40cc24…（A2）。不同就停下來，先問
   ```

2. 備份（留著，不刪）：

   ```
   sudo -u apa-signer /bin/cp -p /opt/apa-signer/data/signer.json /opt/apa-signer/data/signer.json.a2
   sudo -u apa-signer /bin/cp -p /opt/apa-signer/data/approvals.db /opt/apa-signer/data/approvals.db.a2
   sudo /bin/cp -p /opt/apa-signer/bin/apa-signer /opt/apa-signer/bin/apa-signer.a2
   ```

3. 安裝執行檔與語音檔案：

   ```
   sudo install -o root -g wheel -m 0755 $P/var/v1-opt/apa-signer /opt/apa-signer/bin/apa-signer
   sudo /usr/bin/ditto $P/var/v1-opt/stage/voice /opt/apa-signer/voice
   sudo /usr/sbin/chown -R root:wheel /opt/apa-signer/voice
   sudo /bin/chmod -R go-w,a+rX /opt/apa-signer/voice
   sudo install -d -o apa-signer -g apa-signer -m 0700 /opt/apa-signer/voice-run
   sudo -u apa-signer /bin/sh -c 'umask 077; cat > /opt/apa-signer/data/signer.json' < $P/var/v1-opt/signer.json
   ```

4. 核對安裝結果：

   ```
   sudo shasum -a 256 /opt/apa-signer/bin/apa-signer /opt/apa-signer/data/signer.json   # 1012f3e9… 與 f641ac0e…
   (cd /opt/apa-signer && shasum -a 256 -c $P/var/v1-opt/stage/manifest.sha256 | grep -vc ': OK$')   # 0
   sudo find /opt/apa-signer/voice \( ! -user root -o -perm -g+w -o -perm -o+w \) | head -3   # 應無輸出
   ls -led /opt/apa-signer/voice-run    # drwx------ apa-signer apa-signer
   ```

**S1 啟動語音服務（另開終端機 W-voice）**

```
cd /; sudo -u apa-signer /usr/bin/env -i HOME=/var/empty PATH=/usr/bin:/bin /opt/apa-signer/voice/python/bin/python3.12 -I /opt/apa-signer/voice/sidecar.py --socket /opt/apa-signer/voice-run/voice.sock --models /opt/apa-signer/voice/models
```

- 預期：`voice sidecar: listening on /opt/apa-signer/voice-run/voice.sock`，之後模型要載入 2～3 分鐘。
- 在 W-host 確認就緒（重複執行到看見 `"ok": true`）：

  ```
  sudo -u apa-signer /usr/bin/curl -s --unix-socket /opt/apa-signer/voice-run/voice.sock http://voice/v1/health
  ```

  應該是 `{"ok": true, "asr": "faster-whisper-small@536b066", "speaker": "spkrec-ecapa-voxceleb@0f99f2d"}`。

**V 登記聲紋（W-host；在 S2 之前；用 wireless microphone）**
1. 找出 wireless microphone 的編號 N：`ffmpeg -hide_banner -f avfoundation -list_devices true -i ""`（看 audio devices 那一段）。
2. 錄 3 段（各約 6 秒），句子同 §13：

   ```
   R=$(mktemp -d); chmod 700 "$R"
   ffmpeg -hide_banner -loglevel error -f avfoundation -i ":N" -ar 16000 -ac 1 -t 6 "$R/1.wav"   # 我是核准者，這是登記用的第一句話。
   ffmpeg -hide_banner -loglevel error -f avfoundation -i ":N" -ar 16000 -ac 1 -t 6 "$R/2.wav"   # 今天天氣很好，我們去買咖啡豆。
   ffmpeg -hide_banner -loglevel error -f avfoundation -i ":N" -ar 16000 -ac 1 -t 6 "$R/3.wav"   # 請確認金額與商家，再按下核准。
   ```

3. 交給 apa-signer、登記，然後立刻刪除兩邊的錄音：

   ```
   sudo -u apa-signer /bin/mkdir -m 700 /opt/apa-signer/data/voice-enroll
   for i in 1 2 3; do sudo -u apa-signer /bin/sh -c "umask 077; cat > /opt/apa-signer/data/voice-enroll/$i.wav" < "$R/$i.wav"; done
   cd /; sudo -u apa-signer /opt/apa-signer/bin/apa-signer approver voice-enroll --dir /opt/apa-signer/data --approver approver-a /opt/apa-signer/data/voice-enroll/1.wav /opt/apa-signer/data/voice-enroll/2.wav /opt/apa-signer/data/voice-enroll/3.wav
   sudo -u apa-signer /bin/rm -P /opt/apa-signer/data/voice-enroll/1.wav /opt/apa-signer/data/voice-enroll/2.wav /opt/apa-signer/data/voice-enroll/3.wav
   sudo -u apa-signer /bin/rmdir /opt/apa-signer/data/voice-enroll
   rm -P "$R"/*.wav && rmdir "$R"; cd $P
   ```

   應該看到 `voice template of approver-a from 3 recordings`。

**S2 啟動 Signer（另開終端機 W-signer）**

```
cd /; sudo -u apa-signer /opt/apa-signer/bin/apa-signer serve-approvals --dir /opt/apa-signer/data
```

預期：`apa-signer: serving approvals only on /opt/apa-signer/run/signer.sock (payment unavailable: …)`。不會問密語；問了就 Ctrl-C 停止。

**C 核對（W-host）**

```
ls -led /opt/apa-signer/run/signer.sock      # srw-rw---- apa-signer apa-signer-clients
sudo ls -le /opt/apa-signer/data             # signer.json、approvals.db、voice.key 都是 -rw------- apa-signer
curl -sS --unix-socket /opt/apa-signer/run/signer.sock http://signer/v1/health   # {"state":"approvals-only","payment":"unavailable"}
```

**H 啟動 Host（W-host，repo 目錄）**

```
make present-a2 SIGNER=/opt/apa-signer/run/signer.sock REGISTRY=$P/var/v1-dev/registry.json
```

- 確認終端機印出「A2 的 Host 資料庫沿用 …a2-host-b6424086acbc」，然後配對（配對碼不要貼回來）。
- 裝置不用重新登記：`/opt` 已有 10/13 登記的 approver-a credential `b8923ecec9bc556c`。Google 密碼管理工具裡有兩把 localhost passkey（`/opt` 與開發模式各一把），瀏覽器會自動用對應的那一把。

**O 試用**：「以 A 模擬 → 建立委託 → 確認並開始」；在面板選 wireless microphone，按核准，等「錄音中，請唸這一句：」再唸大字，按「唸完了」，輸入 PIN。預期「本人核准已驗證（裝置＋語音）」。approver-b 沒有聲紋模板，核准會被以 `VOICE_NOT_ENROLLED` 拒絕，這是預期的。

**X 停止**：Host、Signer、語音服務各自 Ctrl-C。資料全部保留（Aaron：商業 demo 結束前都不做清理）。

**R 回滾（需要時）**：停掉三個程序後，

```
sudo install -o root -g wheel -m 0755 /opt/apa-signer/bin/apa-signer.a2 /opt/apa-signer/bin/apa-signer
sudo -u apa-signer /bin/cp -p /opt/apa-signer/data/signer.json.a2 /opt/apa-signer/data/signer.json
```

`approvals.db` 不用還原：A2 的執行檔能打開補過語音表的資料庫（已驗證）；要完全回到部署前，也可以用 `approvals.db.a2`。`/opt/apa-signer/voice` 與 `voice-run` 留著不影響 A2。

**已知（review 第二輪 note，不阻擋）**：
- 用 Chrome 預設麥克風時，`NotReadableError`、`AbortError`（麥克風被占用）也會走降級；D4 只寫了「沒有麥克風、拒絕權限」。這是原本就有的行為，沒有擴大攻擊面（拒絕權限本來就能降級），留待 Aaron 決定要收緊，還是把 D4 補寫清楚。
- 商家相似度只比兩個名字本身；兩家各差一個音節時，同一句唸錯的話可能同時通過兩家。現在只有一家商家。
- 「拔掉指定麥克風」只有單元測試，v1-ui 沒有對應情境。
