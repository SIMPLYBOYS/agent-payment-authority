# V1 語音核准三態

> 2026-10-15，Claude Code。依 vault〈Claude-Code-V1語音核准三態與評測派工-20261013〉與 Aaron「我們開始進行 V1 語音三態」。分支 `commercial-v1`（從 main `cfc5c28`）。
> **狀態：決策已核准，開工中。** 本片只用合成音訊、公開資料集與 virtual authenticator；不收 Aaron 的聲音、不碰真實裝置、不改 `/opt/apa-signer`。

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
