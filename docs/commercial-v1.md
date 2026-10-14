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

## 2 工具與資料（下載紀錄）

（下載後填入版本、來源與 sha256。）
