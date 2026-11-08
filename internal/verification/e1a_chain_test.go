package verification

import (
	"context"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"math/big"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/SIMPLYBOYS/agent-payment-authority/internal/contracts"
	"github.com/SIMPLYBOYS/agent-payment-authority/internal/eth"
	"github.com/SIMPLYBOYS/agent-payment-authority/internal/paycontract"
	"github.com/SIMPLYBOYS/agent-payment-authority/internal/signer/signertest"
)

// stubChain is a node the test writes: receipts, transactions, calls and logs.
type stubChain struct {
	id, latest uint64
	receipts   map[string]*eth.Receipt
	txs        map[string]map[string]string // hash -> {to, input}
	code       string
	verifier   eth.Address
	field      []byte
	proofFails bool
	proofErr   int // the RPC error code when the proof call fails (0: 3, a revert)
	logs       []eth.Log
	calls      map[string][]byte // calldata (hex) -> its return
}

func (c *stubChain) ChainID(context.Context) (*big.Int, error) {
	return new(big.Int).SetUint64(c.id), nil
}
func (c *stubChain) BlockNumber(context.Context) (uint64, error) { return c.latest, nil }
func (c *stubChain) Receipt(_ context.Context, h []byte) (*eth.Receipt, error) {
	return c.receipts["0x"+hex.EncodeToString(h)], nil
}
func (c *stubChain) Call(_ context.Context, m eth.Msg) ([]byte, error) {
	if b, ok := c.calls[hex.EncodeToString(m.Data)]; ok {
		return b, nil
	}
	switch {
	case strings.HasPrefix(hex.EncodeToString(m.Data), hex.EncodeToString(eth.Selector("verifier()"))):
		w := make([]byte, 32)
		copy(w[12:], c.verifier[:])
		return w, nil
	case strings.HasPrefix(hex.EncodeToString(m.Data), hex.EncodeToString(eth.Selector("taskField(string)"))):
		return c.field, nil
	case c.proofFails && c.proofErr != 0:
		return nil, &eth.RPCError{Code: c.proofErr, Message: "limit exceeded"}
	case c.proofFails:
		return nil, &eth.RPCError{Code: 3, Message: "execution reverted"}
	}
	return nil, nil
}
func (c *stubChain) FilterLogs(context.Context, eth.Address, [][32]byte, uint64, uint64) ([]eth.Log, error) {
	return c.logs, nil
}
func (c *stubChain) Raw(_ context.Context, method string, out any, params ...any) error {
	var v any
	switch method {
	case "eth_getTransactionByHash":
		v = c.txs[params[0].(string)]
	case "eth_getCode":
		v = c.code
	default:
		return errors.New("unexpected " + method)
	}
	raw, _ := json.Marshal(v)
	return json.Unmarshal(raw, out)
}

func word(b []byte) (w [32]byte) { copy(w[32-len(b):], b); return }

// chainCase is one paid Allow and the chain that settles it, every part as
// it should be; each test case changes one.
type chainCase struct {
	m     E1aManifest
	run   *e1aRun
	chain *stubChain
}

func newChainCase(t *testing.T) *chainCase {
	t.Helper()
	art, err := os.ReadFile(filepath.Join("../..", "var/contracts/out/Verifier.sol/Verifier.json"))
	if err != nil {
		t.Skip("no compiled Verifier.sol (forge build in contracts/)")
	}
	var a struct {
		DeployedBytecode struct{ Object string } `json:"deployedBytecode"`
	}
	json.Unmarshal(art, &a)
	po := signertest.Order{Consumer: "consumer-a", Agent: "agent-a", Wallet: "wallet-a", Task: "task-a", From: "0x1111111111111111111111111111111111111111",
		PayTo: signertest.Merchant, Amount: "500000", N: 1, Now: 1790726400, Window: 300}.PO()
	intent, _ := po.IntentHash()
	want, _ := hex.DecodeString(strings.TrimPrefix(intent, "sha256:"))
	budget, token, verifier := eth.Address{0xbb}, eth.Address{0x36}, eth.Address{0xee}
	payer, _ := eth.ParseAddress(po.PaymentBinding.From)
	payee, _ := eth.ParseAddress(strings.ToLower(po.PaymentBinding.PayTo))
	nonce, _ := hex.DecodeString(strings.TrimPrefix(po.PaymentBinding.Nonce, "0x"))
	tx1, tx2 := "0x"+strings.Repeat("1", 64), "0x"+strings.Repeat("2", 64)
	field := make([]byte, 32)
	field[31] = 7
	spend, _ := eth.Call("spend(string,uint256,uint256,uint256,uint256,bytes32,uint256,bytes,bytes)", "task-a", big.NewInt(1), big.NewInt(500000), big.NewInt(2),
		big.NewInt(3), eth.Bytes32(want), uint64(9), []byte("proof"), []byte("sig"))
	amount := word(big.NewInt(500000).Bytes())
	spent := append(append(append([]byte{}, want...), amount[:]...), make([]byte, 64)...)
	h2, _ := hex.DecodeString(strings.TrimPrefix(tx2, "0x"))
	pay := func(typ, tx string) contracts.AuditEvent {
		return contracts.AuditEvent{EventType: typ, Scope: &contracts.EventScope{PurchaseOrderID: po.Intent.PurchaseOrderID, IntentHash: intent},
			Payload: contracts.EventPayload{Payment: &contracts.PaymentRecord{TxHash: tx}}}
	}
	decided := contracts.AuditEvent{EventType: "POLICY_DECIDED", Scope: &contracts.EventScope{PurchaseOrderID: po.Intent.PurchaseOrderID, IntentHash: intent},
		Payload: contracts.EventPayload{Decision: &contracts.PolicyDecision{PolicyVerdict: "Allow"}}}
	return &chainCase{
		m: E1aManifest{Start: E1aStart{Chain: "base_sepolia", ChainID: 84532, FromBlock: 5, ToBlock: 50, Wallets: []string{payer.Hex()}, Token: token.Hex(),
			BudgetContract: budget.Hex(), Verifier: verifier.Hex()}},
		run: &e1aRun{task: E1aTask{TaskID: "task-a"}, orders: []paycontract.PurchaseOrder{po},
			events: []contracts.AuditEvent{decided, pay("BUDGET_RESERVED", tx1), pay("PAYMENT_SETTLED", tx2)}},
		chain: &stubChain{id: 84532, latest: 60, code: a.DeployedBytecode.Object, verifier: verifier, field: field,
			txs: map[string]map[string]string{tx1: {"to": budget.Hex(), "input": "0x" + hex.EncodeToString(spend)}},
			receipts: map[string]*eth.Receipt{
				tx1: {Status: 1, BlockNumber: 10, Logs: []eth.Log{{Address: budget, Topics: [][32]byte{spentTopic, word(field)}, Data: spent}}},
				tx2: {Status: 1, BlockNumber: 12, Logs: []eth.Log{
					{Address: token, Topics: [][32]byte{transferTopic, word(payer[:]), word(payee[:])}, Data: amount[:]},
					{Address: token, Topics: [][32]byte{usedTopic, word(payer[:]), word(nonce)}},
				}},
			},
			logs: []eth.Log{{Address: token, Topics: [][32]byte{transferTopic, word(payer[:]), word(payee[:])}, Data: amount[:], TxHash: [32]byte(h2), BlockNumber: 12}},
		},
	}
}

func (c *chainCase) check() map[string]Verdict {
	var f findings
	e1aOnChain(&f, c.m, map[string]*e1aRun{"task-a": c.run}, c.chain, "../..")
	return f.verdicts(E1aClaims)
}

// E4 and E8 against a node the test writes (docs/commercial-e1a.md 4.5):
// the whole case passes; each change is caught by the claim it breaks.
func TestE1aChainClaims(t *testing.T) {
	if v := newChainCase(t).check(); v["E4"] != Pass || v["E8"] != Pass {
		t.Fatalf("the whole case: %v", v)
	}
	tx1, tx2 := "0x"+strings.Repeat("1", 64), "0x"+strings.Repeat("2", 64)
	for name, c := range map[string]struct {
		change func(*chainCase)
		claim  string
		want   Verdict
	}{
		"tx#1 reverted":                  {func(c *chainCase) { c.chain.receipts[tx1].Status = 0 }, "E4", Fail},
		"tx#2 not deep enough":           {func(c *chainCase) { c.chain.latest = 12 }, "E4", Inconclusive},
		"tx#1 is tx#2":                   {func(c *chainCase) { c.run.events[2].Payload.Payment.TxHash = tx1 }, "E4", Fail},
		"a reservation without tx":       {func(c *chainCase) { c.run.events[1].Payload.Payment = nil }, "E4", Fail},
		"Spent for another intent":       {func(c *chainCase) { c.chain.receipts[tx1].Logs[0].Data[0] ^= 1 }, "E4", Fail},
		"the proof refused":              {func(c *chainCase) { c.chain.proofFails = true }, "E4", Fail},
		"another verifier's code":        {func(c *chainCase) { c.chain.code = "0x6000" }, "E4", Fail},
		"tx#2 another amount":            {func(c *chainCase) { one := word(big.NewInt(1).Bytes()); c.chain.receipts[tx2].Logs[0].Data = one[:] }, "E4", Fail},
		"tx#2 another nonce":             {func(c *chainCase) { c.chain.receipts[tx2].Logs[1].Topics[2][0] ^= 1 }, "E4", Fail},
		"a second transfer in tx#2":      {func(c *chainCase) { c.chain.logs = append(c.chain.logs, c.chain.logs[0]) }, "E8", Fail},
		"a payer the start leaves out":   {func(c *chainCase) { c.m.Start.Wallets = []string{"0x2222222222222222222222222222222222222222"} }, "E8", Fail},
		"blocks that miss tx#1":          {func(c *chainCase) { c.m.Start.FromBlock = 11 }, "E8", Fail},
		"an end before tx#2":             {func(c *chainCase) { c.m.Start.ToBlock = 11 }, "E8", Inconclusive},
		"settled with no reservation":    {func(c *chainCase) { c.run.events = append(c.run.events[:1], c.run.events[2]) }, "E4", Fail},
		"a settlement without tx":        {func(c *chainCase) { c.run.events[2].Payload.Payment = nil }, "E4", Fail},
		"the proof not run (node error)": {func(c *chainCase) { c.chain.proofFails, c.chain.proofErr = true, -32005 }, "E4", Inconclusive},
		"a transfer to another payee":    {func(c *chainCase) { c.chain.logs[0].Topics[2][31] ^= 1 }, "E8", Fail},
		"an Ask paid on a simulated grant": {func(c *chainCase) {
			d := *c.run.events[0].Payload.Decision
			d.PolicyVerdict = "Ask"
			c.run.events[0].Payload.Decision = &d
			c.run.events = append(c.run.events, contracts.AuditEvent{EventType: "APPROVED", Scope: c.run.events[0].Scope, Payload: contracts.EventPayload{AuthorizationMethod: "simulated"}},
				contracts.AuditEvent{EventType: "GRANT_ISSUED", Scope: c.run.events[0].Scope, Payload: contracts.EventPayload{AuthorizationMethod: "simulated"}})
		}, "E8", Fail},
		"no end block": {func(c *chainCase) { c.m.Start.ToBlock = 0 }, "E8", Inconclusive},
		"settled on a Deny, never delivered": {func(c *chainCase) {
			d := *c.run.events[0].Payload.Decision
			d.PolicyVerdict = "Deny"
			c.run.events[0].Payload.Decision = &d
		}, "E8", Fail},
	} {
		cc := newChainCase(t)
		c.change(cc)
		if v := cc.check(); v[c.claim] != c.want {
			t.Errorf("%s: %s is %s, want %s (%v)", name, c.claim, v[c.claim], c.want, v)
		}
	}
}

// Each tx#2 is listed with its sender as the chain has it (a facilitator's, FA4); one the node
// cannot give is listed without, and neither changes E4 or E8.
func TestE1aSettlementSenders(t *testing.T) {
	tx2 := "0x" + strings.Repeat("2", 64)
	for name, c := range map[string]struct{ from, want string }{
		"read":      {"0xD407E409E34E0B9AFB99ECCEB609BDBCD5E7F1BF", "0xd407e409e34e0b9afb99ecceb609bdbcd5e7f1bf"},
		"not on it": {"", ""},
		"not one":   {"0x12", ""},
	} {
		cc := newChainCase(t)
		if c.from != "" {
			cc.chain.txs[tx2] = map[string]string{"from": c.from}
		}
		var f findings
		sent := e1aOnChain(&f, cc.m, map[string]*e1aRun{"task-a": cc.run}, cc.chain, "../..")
		if v := f.verdicts(E1aClaims); v["E4"] != Pass || v["E8"] != Pass ||
			len(sent) != 1 || sent[0].Transaction != tx2 || sent[0].PurchaseOrderID != cc.run.orders[0].Intent.PurchaseOrderID || sent[0].SentBy != c.want {
			t.Errorf("%s: %+v %v", name, sent, v)
		}
	}
}

// docs/commercial-refund.md: a refund is E4's too: confirmed, the purchase's whole amount back to its payer
// from its own sender, listed with the settlement; E8 does not count it (nothing leaves a payer wallet).
func TestE1aRefund(t *testing.T) {
	tx3 := "0x" + strings.Repeat("3", 64)
	refunded := func(change func(c *chainCase, shop, payer eth.Address)) *chainCase {
		cc := newChainCase(t)
		shop, _ := eth.ParseAddress("0x6bbe84371eab1a980ba862b90474a7e8e9bb773e")
		payer, _ := eth.ParseAddress(cc.run.orders[0].PaymentBinding.From)
		amount := word(big.NewInt(500000).Bytes())
		cc.run.events = append(cc.run.events, contracts.AuditEvent{EventType: "PAYMENT_REFUNDED", Scope: cc.run.events[2].Scope,
			Payload: contracts.EventPayload{Payment: &contracts.PaymentRecord{TxHash: tx3}}})
		cc.chain.txs[tx3] = map[string]string{"from": shop.Hex()}
		cc.chain.receipts[tx3] = &eth.Receipt{Status: 1, BlockNumber: 20, Logs: []eth.Log{{Address: eth.Address{0x36}, Topics: [][32]byte{transferTopic, word(shop[:]), word(payer[:])}, Data: amount[:]}}}
		if change != nil {
			change(cc, shop, payer)
		}
		return cc
	}
	cc := refunded(nil)
	var f findings
	sent := e1aOnChain(&f, cc.m, map[string]*e1aRun{"task-a": cc.run}, cc.chain, "../..")
	if v := f.verdicts(E1aClaims); v["E4"] != Pass || v["E8"] != Pass || len(sent) != 1 || sent[0].Refund == nil || sent[0].Refund.Transaction != tx3 ||
		sent[0].Refund.SentBy != "0x6bbe84371eab1a980ba862b90474a7e8e9bb773e" {
		t.Fatalf("a refund: %+v %v", sent, v)
	}
	for name, c := range map[string]struct {
		change   func(c *chainCase, shop, payer eth.Address)
		want, e8 Verdict
	}{
		"reverted":     {func(c *chainCase, _, _ eth.Address) { c.chain.receipts[tx3].Status = 0 }, Fail, Pass},
		"not deep yet": {func(c *chainCase, _, _ eth.Address) { c.chain.latest = 20 }, Inconclusive, Inconclusive}, // the node is before the pack's end too
		"part of the amount": {func(c *chainCase, _, _ eth.Address) {
			one := word(big.NewInt(1).Bytes())
			c.chain.receipts[tx3].Logs[0].Data = one[:]
		}, Fail, Pass},
		"to another wallet":   {func(c *chainCase, _, _ eth.Address) { c.chain.receipts[tx3].Logs[0].Topics[2][31] ^= 1 }, Fail, Pass},
		"not from its sender": {func(c *chainCase, _, payer eth.Address) { c.chain.txs[tx3] = map[string]string{"from": payer.Hex()} }, Fail, Pass},
	} {
		cc := refunded(c.change)
		if v := cc.check(); v["E4"] != c.want || v["E8"] != c.e8 {
			t.Errorf("%s: E4 %s, want %s; E8 %s, want %s", name, v["E4"], c.want, v["E8"], c.e8)
		}
	}
}

// I3 (docs/commercial-8004.md): each consumer the registry names an Agent for
// is listed with what its registry answers: the owner, no agentWallet, the
// file's sha256 and its declared wallet, the consumer's own or not. On
// another chain, or without --rpc, only the ref.
func TestE1aAgents(t *testing.T) {
	var reg contracts.RegistryConfig
	if raw, err := os.ReadFile("../../testdata/demo/registry.json"); err != nil || json.Unmarshal(raw, &reg) != nil {
		t.Fatal(err)
	}
	ref := "eip155:84532/erc721:0x8004A818BFB912233c491871b3d84c89A494BD9e/10569"
	reg.Consumers[0].AgentRegistryRef = ref
	owner := "0xccd7bec96111b217ccc92632e6e0173878ab4c6a"
	file := func(wallet string) []byte {
		uri := "data:application/json;base64," + base64.StdEncoding.EncodeToString([]byte(`{"apa":{"payment_wallet":"`+wallet+`"}}`))
		b, _ := eth.Encode(uri)
		return b
	}
	call := func(sig string) string { d, _ := eth.Call(sig, uint64(10569)); return hex.EncodeToString(d) }
	ow, _ := eth.ParseAddress(owner)
	ownerWord := word(ow[:])
	for name, c := range map[string]struct {
		chain    uint64
		wallet   string
		rpc      bool
		read, is bool
	}{
		"read":             {84532, reg.Wallets[0].From, true, true, true},
		"another wallet":   {84532, reg.Wallets[1].From, true, true, false},
		"on another chain": {31337, reg.Wallets[0].From, true, false, false},
		"no --rpc":         {84532, reg.Wallets[0].From, false, false, false},
	} {
		sc := &stubChain{id: c.chain, calls: map[string][]byte{call("ownerOf(uint256)"): ownerWord[:], call("getAgentWallet(uint256)"): make([]byte, 32), call("tokenURI(uint256)"): file(c.wallet)}}
		var chain Chain
		if c.rpc {
			chain = sc
		}
		got := e1aAgents(chain, reg)
		if len(got) != 1 || got[0].ConsumerID != "consumer-a" || got[0].Ref != ref {
			t.Fatalf("%s: %+v", name, got)
		}
		a := got[0]
		if read := a.Owner == owner && a.AgentWallet == "0x"+strings.Repeat("0", 40) && len(a.URISHA256) == 64 && a.DeclaredWallet == c.wallet; read != c.read || a.DeclaredIsConsumers != c.is {
			t.Errorf("%s: %+v", name, a)
		}
		if a.AgentWalletIsConsumers {
			t.Errorf("%s: no agentWallet reads as the consumer's", name)
		}
	}
	// §8 (11/7): the registry's agentWallet, set on the wallet's own signature: the consumer's, or another's.
	for name, c := range map[string]struct {
		wallet string
		is     bool
	}{"the consumer's wallet": {reg.Wallets[0].From, true}, "another wallet": {reg.Wallets[1].From, false}} {
		w, _ := eth.ParseAddress(c.wallet)
		ww := word(w[:])
		sc := &stubChain{id: 84532, calls: map[string][]byte{call("ownerOf(uint256)"): ownerWord[:], call("getAgentWallet(uint256)"): ww[:], call("tokenURI(uint256)"): file(reg.Wallets[0].From)}}
		if a := e1aAgents(sc, reg)[0]; a.AgentWalletIsConsumers != c.is || !strings.EqualFold(a.AgentWallet, c.wallet) {
			t.Errorf("%s: %+v", name, a)
		}
	}
}
