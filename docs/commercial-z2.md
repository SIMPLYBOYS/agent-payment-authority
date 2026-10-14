# Z2 ZK 電路 v2 與本機預算合約

> 2026-10-14，Claude Code。依 vault〈Claude-Code-Z2-ZK電路v2與本機預算合約派工-20261013〉與 Aaron「我們要開始 Z2 的部分」。分支 `commercial-z2`（從 main `e36502e`）。
> **狀態：開工；設計選擇與工具已核准（第 1 節），實作中。** 只用合成資料與本機鏈（anvil）；不碰 Base Sepolia、testnet、faucet、外部交易或付款金鑰。

## 1 開工前的核准（Aaron 10/14：「以上的內容，我批准先依照建議的方式進行，除非後續很難做調整，我們在深入討論取捨」）

**前置**：Z1 Done；Z1-D1（`intent_hash` 拆兩個 128 位元 limb）、D2（task 欄位 31 bytes 截斷）、D3（MiMC）、D4（展示版由主持人單方 setup，畫面明標「信任等同信任主持人」）已決定（[commercial-z1.md](commercial-z1.md) 第 8 節）。

**工具**

| 工具 | 版本 | 授權 | 安裝 |
|---|---|---|---|
| Foundry（forge、anvil、cast） | v1.8.3（2026-09-15 stable） | MIT／Apache-2.0 | 官方 macOS x86_64 壓縮檔，核對 sha256，解到 `var/tools/`（git 忽略） |
| solc | 0.8.37（2026-09-10） | GPL-3.0（編譯器） | forge 下載到 `~/.svm` |
| gnark | v0.16.4 | Apache-2.0 | 依 Z1 前置條件升級 |
| Go 工具鏈 | 1.26.8 | BSD | 由 Go 自動下載到模組快取 |

**位置**：電路、prover、setup 與 Solidity verifier 匯出放在獨立模組 `zk/`；合約放 `contracts/`（Foundry 專案）。主模組 `go.mod` 不動。

**設計選擇**

- **Z2-D1 政策層在鏈上的綁定**：Host 用本機測試金鑰（secp256k1，只在 anvil）簽 `(chain_id, 合約地址, task, intent_hash, amount, N, C_new)`，合約以 `ecrecover` 驗證。這也把金額和 intent 綁在一起（補 Z1-D1 的已知限制）。
- **Z2-D2 部署範圍**：task 欄位 = `SHA-256(domain ‖ chain_id ‖ 合約地址 ‖ task_id)` 的前 31 bytes，由合約自己算。
- **Z2-D3 setup**：gnark `mpcsetup` 在本機跑 phase 1、phase 2 各三次貢獻，全部由我們執行；紀錄熵來源、輸出雜湊與重現方式；標示「信任等同信任主持人」。
- **Z2-D4 金額為 0**：合約拒絕。
