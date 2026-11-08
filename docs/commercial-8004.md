# 兩位 Agent 的鏈上身分（ERC-8004）

> 2026-11-04，Claude Code。Aaron 11/3：「就依照這個順序把剩下的功能做完」，這是第 4 項：替兩個 Agent 在 Base Sepolia 的 registry 註冊身分，並顯示在畫面上。前三項（facilitator FA1～FA4）已完成，見 `docs/commercial-facilitator.md`。
> **Aaron 11/4 的決定**：「先照建議」：E1 A（issuer 代為註冊）、E2 A（`data:` URI）、E3 A（宣告付款錢包、清掉 `agentWallet`）、E4 A（顯示、可信 registry、checker 列事實）、E5 A（信譽先不做）、E6 A（本機不顯示）。真的送出註冊前，Aaron 先看過最終的註冊檔再核准。**Aaron 11/4 核准了兩份註冊檔的內容**（§3 與 `p2-testnet agents` 印出的計畫：家長、孩子各一份，付款錢包標明未證明）。

## 1 現況

- 專案裡的 Agent 只有內部字串 ID（`consumer-a`、`consumer-b` 與各自的 task），沒有任何鏈上身分。`docs/contracts.md` 207 行在 M1 時保留了一條路：日後在可信 registry 加一個可選的 `agent_registry_ref`，由主持人設定，不由模型或請購單填寫。
- ADR（`docs/adr/payment-account-model.md`）決定 ERC-8126／8004 不接進 M1、M2 的 Policy。這次也照這個決定：身分只用來**顯示與查證**，不改變 Allow／Ask／Deny 的判斷。
- 9/29 的 spike 只從 README 抄下部署地址，沒有上鏈核對。

## 2 ERC-8004 查證（11/4）

**規格**（eips.ethereum.org，11/4 讀取）：仍是 **Draft**。
- Identity Registry 是 ERC-721。`register(string agentURI)` 會鑄一個 NFT 給呼叫者，`agentId` 就是 tokenId，依序遞增。鏈上的全域識別寫成 `eip155:{chainId}:{registry}` 加上 `agentId`。
- `agentURI` 指向註冊檔，可以用 `ipfs://`、`https://`，或 base64 的 `data:` URI。用 `data:` 的話，整份資料都在鏈上。
- 註冊檔的 `type` 是 `https://eips.ethereum.org/EIPS/eip-8004#registration-v1`，欄位有 `name`、`description`、`image`、`services`、`x402Support`、`active`、`registrations`（`{agentId, agentRegistry}`）、`supportedTrust`。`supportedTrust` 不填，代表只用來讓別人找得到這個 Agent（discovery）。
- `agentWallet` 是保留的 metadata：註冊時自動設成擁有者的地址。要換成別的錢包，必須附上那個錢包的 EIP-712 簽章（合約錢包則是 ERC-1271）；NFT 轉手時會被清掉。
- Reputation Registry 的 `giveFeedback(agentId, value, valueDecimals, tag1, tag2, endpoint, feedbackURI, feedbackHash)` 誰都可以呼叫，但**不能**是這個 Agent 的擁有者或被授權的 operator。

**Base Sepolia 上的部署**（官方合約 repo 的 README；11/4 用公開節點唯讀核對過）：

| 合約 | 地址 | 鏈上核對 |
|---|---|---|
| IdentityRegistry | `0x8004A818BFB912233c491871b3d84c89A494BD9e` | 有程式碼；是 EIP-1967 proxy（UUPS），實作在 `0x7274…9c02`；`getVersion()` 回 `2.0.0`；名稱 `AgentIdentity`、代號 `AGENT` |
| ReputationRegistry | `0x8004B663056A597Dffe9eCcC1965A193B7388713` | 同樣是 proxy、`2.0.0`；`getIdentityRegistry()` 指向上面那個 |
| ValidationRegistry | 沒有列 | — |

- 兩個合約的 `owner()` 都是 `0x5472…2603`。也就是說，**合約可以被第三方升級**，這要寫進照實說的限制。
- 已經有一萬多個 Agent 註冊過；用 `eth_call` 試跑 `register(string)`（只是模擬，沒有送出），下一個會拿到 10569。
- 別人的實際用法：有的用 `data:` URI，有的用 `ipfs://`。新註冊的 `agentWallet` 等於擁有者。

## 3 做法

不改付款路徑和 Policy。只替一家人的兩位 Agent 各註冊一個身分，並且讓畫面和證據包讀得到它。

1. **註冊**：新增 `p2-testnet agents --dir DIR`，只跑一次（真的送出前要 Aaron 核准）。
   - 依 host 檔的 registry，替客人 A（家長）、B（孩子）各組一份註冊檔，用 `data:` URI 放上鏈。
   - 每一位 3 筆交易：`register` → `setAgentURI`（補上自己的 `registrations`，因為 `agentId` 要鑄出來才知道）→ `unsetAgentWallet`（理由見 E3）。兩位一共 6 筆。
   - 送出前先用 `eth_call` 模擬每一筆。
   - 結果寫進 `DIR/host/agents.json`，內容有 `agentId`、每筆交易，以及註冊檔的 sha256。
2. **可信 registry**：registry.json 的每位客人多一個可選欄位 `agent_registry_ref`，寫成 CAIP-19 的 NFT 識別（例如 `eip155:84532/erc721:0x8004A818BFB912233c491871b3d84c89A494BD9e/10569`），照 contracts.md 207 行，由主持人設定。`e1a-host` 照抄。
3. **畫面**：展示視窗的「為什麼可信」與收據卡加一行「Agent 身分 ERC-8004 #10569」，點了連到 Basescan 那個 NFT 的頁面。本機鏈沒有這個 registry，就不顯示。（實作見 §7 I2：主持人頁沒有改。）
4. **查證**：checker 的報告多列 `agents`，跟 FA4 的 `settlements` 一樣，只列事實、不影響 claim。內容是每位客人的 `agentId`、鏈上讀到的 `ownerOf`、`tokenURI` 的 sha256 是否等於記下的那份、`getAgentWallet`。

**註冊檔**（客人 A，`p2-testnet agents` 印出的計畫；B 換成「客人 B（未滿 18 歲的孩子）」、「超過自動範圍的購買由家長核准」和 B 的錢包 `0xba5f…8667`；`agentId` 是鑄出來的號碼）：

```json
{
  "type": "https://eips.ethereum.org/EIPS/eip-8004#registration-v1",
  "name": "家長的購物 Agent（agent-payment-authority 展示）",
  "description": "客人 A（家長）的 AI 購物助理。它只能提出請購；付款要經過委託、Policy 與本人核准，由獨立的 Signer 簽名。由展示平台代為註冊；Base Sepolia 上的模擬，商品是示意的。",
  "services": [],
  "x402Support": true,
  "active": true,
  "registrations": [{ "agentId": 10569, "agentRegistry": "eip155:84532:0x8004A818BFB912233c491871b3d84c89A494BD9e" }],
  "supportedTrust": [],
  "apa": { "consumer_id": "consumer-a", "payment_wallet": "0x245664a4bfb21a9e9d47f232f6d691223b833554", "payment_wallet_proven": false }
}
```

**注意**：註冊檔會**永久公開**在鏈上，之後用 `setAgentURI` 改，舊的內容還是查得到。所以送出前，你要看過最終的註冊檔內容再核准。

## 4 規模

| 切片 | 內容 | 驗證 |
|---|---|---|
| I1 | `p2-testnet agents`：組註冊檔、模擬、送出、寫 `agents.json`；relayer 要停著（跟 deploy 同一個理由：issuer 的 nonce） | 用假節點測：成功、模擬失敗就不送、送到一半停掉後可以接著做 |
| I2 | registry 的 `agent_registry_ref`；Host、主持人頁與展示視窗的身分列 | Go 測試、頁面測試、瀏覽器截圖 |
| I3 | 證據包的 start 記下 `agents.json`；checker 報告列 `agents`（讀鏈） | 用假節點測：對得上、`tokenURI` 被改過、`agentId` 不存在 |
| I4 | testnet 真的註冊一次（**要 Aaron 核准**；6 筆交易，issuer 付 gas）；用新的 checker 重查 fa3 的證據包，確認舊包也能查 | 鏈上讀回 |

## 5 要 Aaron 決定

| # | 問題 | 建議 |
|---|---|---|
| E1 | 誰持有兩位 Agent 的身分（送註冊交易、付 gas） | **A**：issuer，也就是部署者（P2b-D3）。不用新增金鑰；跟 deploy 一樣，要在 relayer 停著時執行，密語在終端機輸入。畫面和註冊檔照實寫「由展示平台代為註冊」。**B**：另外產生一把專用金鑰，由 issuer 轉一點 ETH 給它。角色分得比較清楚，但多一筆轉帳和一把要保管的金鑰。**C**：家庭的付款錢包自己註冊。身分真的屬於付款人，但 Signer 得多一種會送交易的能力。**建議 A** |
| E2 | 註冊檔放哪裡 | **A**：`data:` URI，整份都在鏈上，不用架主機也不用釘選服務。**B**：IPFS，要釘選服務。**C**：https，要公開主機。**建議 A** |
| E3 | 付款錢包跟 Agent 怎麼連起來 | **A**：註冊檔寫出付款錢包，並標明「宣告，未證明」（`payment_wallet_proven: false`），再把 `agentWallet` 清掉。不清的話，它會指向 issuer，反而誤導。**B**：讓 Signer 對 `agentWallet` 的 EIP-712 訊息簽一次，在鏈上證明「付款錢包同意當這個 Agent 的錢包」。這最有說服力，但 Signer 要多一種簽章：它的 domain 跟付款不同，不能拿去當付款用；不過 Signer 要改程式、要 review，你也要用 sudo 跑一次。**建議先 A**，B 留到下一期 |
| E4 | 要接進展示到什麼程度 | **A**：顯示（客人卡＋Basescan 連結）＋可信 registry 的欄位＋checker 列出事實。**B**：A 再加上商店只賣給註冊檔宣告的錢包，也就是開始拿身分來擋。但 E3 選 A 的話，那個連結只是平台自己的宣告，擋了也證明不了什麼。**C**：只註冊，不接進展示。**建議 A** |
| E5 | 信譽（Reputation Registry） | **A**：先不做。**B**：每次交付後，由商店替 Agent 留一筆評價（`giveFeedback`）。商店不是擁有者，所以規格允許；但每次交付會多一筆商店付 gas 的交易，而且經 facilitator 結算時，Host 手上沒有商店的金鑰。**建議 A** |
| E6 | 本機彩排 | **A**：本機鏈沒有這個 registry，畫面就不顯示身分列。**B**：把官方合約部署到 anvil，要引入它的原始碼與 OpenZeppelin 的 upgradeable 套件。**建議 A** |

## 6 界線

- 身分不進 Policy，也不改變付款：Allow／Ask／Deny 照舊（ADR）。
- ERC-8004 仍是 Draft，registry 可以被第三方升級；這兩點都寫進照實說的限制，不說「符合 ERC-8004」，只說「在 ERC-8004 的 Base Sepolia registry 註冊了身分」。
- E3 選 A 的話，付款錢包與 Agent 的連結只是平台的宣告，不是密碼學證明；畫面和報告都要照實寫。

## 7 進度

**I1 註冊指令（11/4）**
- `p2-testnet agents --dir DIR [--rpc URL]`（`cmd/p2-testnet/agents.go`）：由 issuer 替 `consumer-a`、`consumer-b` 各送 `register()`、`setAgentURI`（註冊檔，`data:` URI，裡面寫著自己的 `agentId`）、`unsetAgentWallet`，共 6 筆。
- 送出前印出計畫（兩份註冊檔、最多花多少 test ETH、issuer 有多少），要打 `yes`，再輸入 issuer 的密語。apa-relayer 在跑就拒絕，因為它也拿著 issuer 的金鑰。
- 每一筆都先寫進 `DIR/host/agents.json` 再送出；中途停了，再跑一次會從那裡接著做，已經送過的步驟不會再送。全部完成後，讀鏈核對：擁有者是 issuer、`tokenURI` 等於記下的那一份（sha256）、`agentWallet` 已經清空。最後把身分寫進 `DIR/host/registry.json` 每位客人的 `agent_registry_ref`（CAIP-19）；`e1a-host` 會照抄。
- 可信 registry：`Consumer` 多一個可選欄位 `agent_registry_ref`；schema 同步；格式不對就不接受。
- 測試：
  - `TestAgents`：在本機 anvil 上，用 `contracts/src/TestAgentRegistry.sol` 代替 ERC-8004 的合約（只有用到的那幾個函式，行為照 11/4 對 Base Sepolia 2.0.0 的模擬）。依序驗證：沒有錢就不送；回答不是 yes 就不送；6 筆都先記錄再送出；鏈上的註冊檔寫著自己的號碼與付款錢包；`agentWallet` 為 0；registry 寫入身分；再跑一次什麼都不送也不問；從 B 的 register 之後接著做，不會再註冊一次；別次註冊的檔案會被拒絕。
  - 反向檢查：把 `unsetAgentWallet` 改成別的號碼，測試失敗。
  - registry 的身分格式（3 種錯誤的寫法被拒絕）。
- 對 Base Sepolia 唯讀跑過計畫（沒有終端機可以回答，停在 `yes` 之前，什麼都沒送）：issuer `0xccd7…4c6a` 有 0.000080 test ETH，計畫最多 0.000048。

**I2 畫面（11/4）**
- Host 的 `GET /api/tasks/{id}/chain` 多回一項 `agent`：這個 task 的客人在可信 registry 裡的 `agent_registry_ref`。只是 registry 的設定，不是從鏈上讀的；要查證，看下面的 checker。
- 展示視窗：
  - 「為什麼可信」清單的第一項是「Agent 身分 ERC-8004 #N」，附註「ERC-8004・平台代為註冊」。
  - 收據卡多一列「Agent 身分」。
  - 兩處都連到 Basescan 那個 NFT 的頁面（`https://sepolia.basescan.org/nft/<registry>/<agentId>`）。
- 只在 Base Sepolia、而且身分的鏈號是 84532 時顯示；本機鏈不顯示（E6 A）。頁面只在有付款紀錄之後才讀 `/chain`，所以身分會跟收據一起出現。
- 主持人頁沒有改。
- 測試：`TestChainViewUsesTheTasksPayerAndEachShop` 驗證只帶這位客人自己的身分；`show.test.mjs` 驗證收據列、可信清單第一項、本機鏈不顯示、另一條鏈的與格式不對的不顯示。

**I3 證據包與 checker（11/4）**
- 證據包不用改：它的 `registry.json` 是 Host 存下的 registry 快照，有 `agent_registry_ref` 就會帶著。
- checker（`verify-demo --mode e1a`）的報告多一項 `agents`。registry 裡每位有身分的客人列一筆，有 `--rpc` 時，再向那條鏈的 registry 讀：
  - `ownerOf` 和 `getAgentWallet`。
  - `tokenURI` 的 sha256。
  - 註冊檔宣告的付款錢包，以及它是不是 registry 裡這位客人的錢包。
- 身分在另一條鏈上，或沒有 `--rpc`，就只列身分本身。只列事實，不影響任何 claim；checker 版本仍是 `e1a.4`。
- limits 加一條：付款錢包是宣告、不是證明；ERC-8004 是 Draft，registry 可以被它的擁有者升級。
- 測試：`TestE1aAgents`，4 種情況：讀得到、宣告的不是這位客人的錢包、另一條鏈、沒有 `--rpc`。反向檢查：讓「是這位客人的錢包」永遠回答是，測試失敗。


**I4 Base Sepolia 註冊（Aaron 11/4）**
- Aaron 核准了兩份註冊檔，在終端機執行 `p2-testnet agents --dir var/p2-testnet`：6 筆交易都由 issuer `0xccd7…4c6a` 送出。
- 家長（`consumer-a`）是 **agent 10573**：`eip155:84532/erc721:0x8004A818BFB912233c491871b3d84c89A494BD9e/10573`（https://sepolia.basescan.org/nft/0x8004A818BFB912233c491871b3d84c89A494BD9e/10573）。三筆交易：register `0x984c…a407`、URI `0x5e0a…5330`、清掉 agentWallet `0xb3f6…2b06`。
- 孩子（`consumer-b`）是 **agent 10574**：`…/10574`。三筆交易：register `0x7000…1c86`、URI `0xb9b6…5e1a`、清掉 agentWallet `0xa7bb…20ed`。
- 另外用一份獨立的 Python 腳本唯讀核對鏈上（不經過專案的 Go 程式）：兩位的擁有者都是 issuer，`agentWallet` 都是 0；`tokenURI` 解出來就是核准的那兩份，`registrations` 寫著各自的號碼，付款錢包是 `0x2456…3554`、`0xba5f…8667`，標明未證明。sha256 與 `var/p2-testnet/host/agents.json` 相同，`registry.json` 兩位客人的 `agent_registry_ref` 已寫入。
- 還沒做：畫面上的身分列和 checker 的 `agents`，要一場新的 testnet 才看得到（頁面只在有付款之後讀 `/chain`；舊的證據包 registry 沒有這個欄位）。**Aaron 11/4：不單獨跑（原本提議的 id1），併進錄影前的 testnet 彩排**，同一場也驗證 apa-facilitator 在 testnet 上結算與頻率限制。

## 8 由 Signer 證明付款錢包（E3 B，Aaron 11/7 重開 A）

> Aaron 11/7：「請重開 A 並按照排序完成任務」，這是第 1 項（另外兩項是退款與頻率限制）。原本 E3 選 A：註冊檔只宣告付款錢包，`agentWallet` 清空。這次改成 B：付款錢包自己簽一份同意，registry 才把它設成這位 Agent 的 `agentWallet`。

**規格**（官方合約 `IdentityRegistryUpgradeable` 2.0.0，11/7 讀原始碼）：
- `setAgentWallet(agentId, newWallet, deadline, signature)`：只有擁有者（或被授權的 operator）能送。
- 簽章是 `newWallet` 對 EIP-712 `AgentWalletSet(uint256 agentId,address newWallet,address owner,uint256 deadline)` 的簽章，domain 是 `ERC8004IdentityRegistry`、版本 `1`、鏈號與 registry（proxy）的地址；合約錢包走 ERC-1271。
- `deadline` 不能已過，也不能超過 5 分鐘之後。NFT 轉手時 `agentWallet` 會被清掉。

**做法**：
- **Signer**（`internal/signer/agentwallet.go`）：多一條路徑 `POST /v1/agent-wallet`，請求只寫錢包 ID 與期限。
  - Signer 只替 `/opt` 的 signer.json 裡 `agents` 登記的那一位 Agent 簽：哪條鏈、哪個 registry、哪個號碼、擁有者是誰，都由管理者寫死，請求改不了。所以 Host 就算被攻破，也不能讓錢包同意別的 Agent，或同意轉手後的新擁有者。
  - 期限必須在 Signer 的時間之後、5 分鐘之內；什麼都不記，期限過了 registry 自己會拒絕，重設同一個錢包也不會改變任何東西。
  - 簽的 domain 是 registry 的，不是任何代幣的，所以這份簽章不能拿去當付款授權。這是 Signer 除了 EIP-3009 之外唯一會簽的東西。
- **`p2-testnet agent-wallet --dir DIR`**：讀 `agents.json`，鏈上已經是付款錢包的就跳過；印出計畫、要打 `yes`、輸入 issuer 的密語（relayer 要停著，理由同 I1）。每一位：向 Signer 要同意（期限是最新區塊時間加 4 分鐘），核對 Signer 簽的正是這位 Agent、這個錢包與擁有者；先模擬（簽章不對就在模擬時被拒，什麼都不送），交易先寫進 `agents.json`（`agent_wallet_tx`）再送出；送完向鏈上讀回 `getAgentWallet`。
- **`p2-testnet opt-signer`**：有 `agents.json` 時，替 `wallet-a`、`wallet-b` 各寫一筆 `agents`。
- **Host**（`cmd/demo/chain.go`）：`/chain` 多回 `agent_wallet`，是 Host 向鏈讀到的 `getAgentWallet`（設定了就記住）。
- **畫面**：`agent_wallet` 等於這個 task 的付款錢包時，「為什麼可信」寫「ERC-8004・平台代為註冊・付款錢包已由錢包簽章證明」，收據寫「ERC-8004 #N（付款錢包已證明）」。
- **checker**：`agents` 每一筆多一欄 `agent_wallet_is_consumer_wallet`，只列事實，不影響 claim。
- 註冊檔不改：鏈上那份仍寫 `payment_wallet_proven: false`（當時的事實）；證明是 `agentWallet`，不是註冊檔。要改註冊檔得多兩筆 `setAgentURI`，而且內容永久公開，所以先不做。

**驗證（11/7，本機）**：
- `TestAgentConfig`：`agents` 一個錢包只能一筆、要在錢包的鏈上、號碼不為 0、地址小寫。
- `TestAgentWallet`（Signer）：只簽登記的那一位；簽章的確是錢包的；domain 與型別都不是 EIP-3009 的；期限是現在、超過 5 分鐘、別的錢包、沒登記的錢包都拒絕；紀錄不含簽章。
- `TestAgentWallet`（`p2-testnet`，本機 anvil）：`contracts/src/TestAgentRegistry.sol` 照 2.0.0 加上 `setAgentWallet` 與 EIP-712 驗章。沒有 `yes` 不送；Signer 同意的是別的號碼，就不送；兩筆都先記錄再送出，送完兩位的 `agentWallet` 都是自己客人的錢包；再跑一次什麼都不問、不送。
- 反向檢查：把 Go 這邊的 domain 名稱改錯，模擬時合約回 `invalid wallet sig`，測試失敗、什麼都沒送。
- `TestOptSigner`、`TestChainViewUsesTheTasksPayerAndEachShop`、`TestE1aAgents`、`show.test.mjs` 各加了對應的情況。
- 本機鏈沒有官方 registry（E6 A），畫面不顯示身分列；真正的 domain 要在 Base Sepolia 上由模擬確認。

**還沒做（要 Aaron）**：提交；`/opt` 換新的 Signer 與 signer.json（sudo）；在 Base Sepolia 送兩筆 `setAgentWallet`（issuer 付 gas，要 Aaron 核准）；之後的 testnet 場次才看得到畫面上的「已證明」與 checker 的新欄位。
