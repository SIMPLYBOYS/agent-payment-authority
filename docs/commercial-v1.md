# V1 語音核准三態

> 2026-10-15，Claude Code。依 vault〈Claude-Code-V1語音核准三態與評測派工-20261013〉與 Aaron「我們開始進行 V1 語音三態」。分支 `commercial-v1`（從 main `cfc5c28`）。
> **狀態：實作與評測完成；獨立上下文 review 無 must-fix，S1–S8 已處理（第 12 節）。待 Aaron 驗收。** 本片只用合成音訊、公開資料集與 virtual authenticator；不收 Aaron 的聲音、不碰真實裝置、不改 `/opt/apa-signer`。

## 1 Aaron 的核准（10/15）

- 範圍選 **B**（「我們走 option b」）：照 PRD 的停損，一次性挑戰語＋語音轉文字＋聲紋，不做抗合成。「合成語音」標為已知邊界。
- D4 選 **(a)**；D8「請開始下載」。

| # | 決定 |
|---|---|
| D1 | 預訓練模型，本機 CPU，不訓練：<br>• 語音轉文字：faster-whisper `small`（MIT）；<br>• 聲紋：SpeechBrain ECAPA-TDNN `spkrec-ecapa-voxceleb`（Apache-2.0）。<br>執行環境：Python 3.12＋PyTorch 2.2.2 CPU（Intel Mac 的最後一版），放 `var/`（git 忽略）。 |
| D2 | 本機 sidecar（Unix socket），只回分數、不做決定。由 Signer 呼叫，依門檻合併裁決；Host 只轉送錄音並記錄 Signer 的結果。不上傳任何第三方。 |
| D3 | Signer 在 challenge 加 `voice_nonce`，從這一筆請購產生挑戰語：金額、商家，加上一組由 nonce 決定的代碼。WebAuthn 的 challenge 涵蓋這個 nonce。三種結果：<br>• 通過：grant 的 `amr: ["webauthn","voice"]`，事件 `VOICE_CHALLENGE_VERIFIED`；<br>• 失敗（唸錯、重播、不是本人）：不發 grant、進 Denied，事件 `VOICE_CHALLENGE_FAILED`＋原因；<br>• 不可用：摘要確認＋WebAuthn 仍可核准，grant 記 `voice=unavailable`，事件 `VOICE_UNAVAILABLE`，畫面明標「降級」。 |
| D4 | 「不可用」只有這幾種：sidecar 連不上、健康檢查失敗、10 秒內沒有結果；**沒有麥克風或拒絕麥克風權限也算不可用**（選 a），一樣明標降級並計入降級率。逾時後才到的結果不改變已發的 grant，只記為晚到。 |
| D5 | 門檻在評測前固定：<br>• 語音轉文字：金額、商家、代碼三項都對（結構化比對，不比全文）；<br>• 聲紋：調參組上 FAR＝1% 的分數。<br>門檻和模型版本寫進設定；之後要調低須 Aaron 核准。 |
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
