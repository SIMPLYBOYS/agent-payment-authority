# 上台順暢度：鏈與 Signer 隨時能開、鏈上互動看得見

> 2026-10-23，Claude Code。Aaron 10/23：「本機鏈和 Signer 如果是在拍攝 demo 影片時可以先 turn off 但實務上必須要能隨時被開啟且正常工作，我希望在 demo 中還是能夠展示 agent 與鍊上互動」。決定：本機鏈與 Base Sepolia 兩者都要；先做這項，再做批次 3。對應 `docs/commercial-gap-review.md` 的 C1、C4。

## 切片

| # | 內容 | 狀態 |
|---|---|---|
| O1 | 本機鏈可續用；本機一鍵啟動與停止 | 完成（見下） |
| O2 | 展示視窗的「鏈上紀錄」：Host 向鏈讀出兩筆交易的區塊、轉帳與預算合約事件；本機鏈與 Base Sepolia 都顯示，Base Sepolia 另附 basescan | 進行中 |
| O3 | Base Sepolia 一鍵啟動（語音服務、`/opt` 的 Signer、relayer、Host）；sudo 由腳本向 Aaron 要 | 未開始 |

## O1 本機鏈可續用、一鍵啟動

- `scripts/demo/p2-local.sh`：anvil 以 `--state anvil-state.json --state-interval 1` 保存鏈。同一個目錄再啟動就接著用同一條鏈（合約、餘額、交易都在），relayer、商店與 Host 的資料庫因此仍然對得上；只有第一次才部署。沒有保存檔的舊目錄照舊拒絕。測過：重開後區塊高度相同、預算合約仍在、重開前的交易收據與內容讀得到（checker 需要）。`P2_LOCAL_PORT` 可換埠（測試用）。
- `make demo-local`（`scripts/demo/demo-local.sh`）：一個終端機依序啟動本機鏈、開發用 Signer、fake Agent 的 Host。
  - 啟動前檢查：8545、8402 沒有程式在用；沒有殘留的 Signer socket；兩個錢包的金鑰檔在。
  - 每一步等前一步就緒，卡住時說卡在哪一步、看哪個紀錄。
  - Signer 的密語在同一個終端機由 Aaron 輸入（安全上必須保留）；Host 在 Signer 就緒後自動啟動並打開頁面。
  - Ctrl-C 一次停下三個；鏈保存在 `var/demo-local/chain/anvil-state.json`，下次同一個指令接著用。
  - 目錄：`var/demo-local`（鏈、Host 場次、紀錄），Signer 沿用 `var/p2-dev-e1a`（已登記 approver-a 的裝置）。

### 使用

```
make demo-local
```

輸入密語 → 頁面打開 → 配對 → 照常操作。結束按 Ctrl-C。
