package main

import (
	"context"
	"encoding/hex"
	"errors"
	"math/big"
	"strings"
	"testing"
	"time"

	"github.com/SIMPLYBOYS/agent-payment-authority/internal/eth"
	"github.com/SIMPLYBOYS/agent-payment-authority/internal/store"
)

// stubNode is a node the test writes: receipts by hash, one block time.
type stubNode struct {
	head     uint64
	receipts map[string]*eth.Receipt
	asked    int
	err      error
	wallet   []byte // what getAgentWallet answers (nil: the call fails)
	calls    int
}

func (n *stubNode) Call(context.Context, eth.Msg) ([]byte, error) {
	n.calls++
	if n.wallet == nil {
		return nil, errors.New("execution reverted")
	}
	return n.wallet, nil
}

func (n *stubNode) BlockNumber(context.Context) (uint64, error) { return n.head, nil }
func (n *stubNode) Receipt(_ context.Context, h []byte) (*eth.Receipt, error) {
	n.asked++
	if n.err != nil {
		return nil, n.err
	}
	return n.receipts["0x"+hex.EncodeToString(h)], nil
}

func w32(b []byte) (w [32]byte) { copy(w[32-len(b):], b); return }

// O2: the two transactions as the chain has them: tx#1's Spent by the
// budget contract, tx#2's Transfer of the token; another contract's log
// is not taken; a missing receipt is not_found; a deep one is kept.
func TestChainReaderReadsWhatTheChainSays(t *testing.T) {
	budget, token, other := eth.Address{0xbb}, eth.Address{0x36}, eth.Address{0x99}
	payer, shop := eth.Address{0x11}, eth.Address{0x33}
	amount := w32(big.NewInt(500000).Bytes())
	spent := append(append(make([]byte, 32), amount[:]...), make([]byte, 64)...)
	nine := w32(big.NewInt(999).Bytes())
	otherSpent := append(append(make([]byte, 32), nine[:]...), make([]byte, 64)...) // another contract's: never taken
	tx1, tx2, tx3 := "0x"+strings.Repeat("1", 64), "0x"+strings.Repeat("2", 64), "0x"+strings.Repeat("3", 64)
	n := &stubNode{head: 20, receipts: map[string]*eth.Receipt{
		tx1: {Status: 1, BlockNumber: 9, Logs: []eth.Log{{Address: budget, Topics: [][32]byte{chainSpentTopic}, Data: spent}, {Address: other, Topics: [][32]byte{chainSpentTopic}, Data: otherSpent}}},
		tx2: {Status: 1, BlockNumber: 10, Logs: []eth.Log{{Address: token, Topics: [][32]byte{chainTransferTopic, w32(payer[:]), w32(shop[:])}, Data: amount[:]},
			{Address: token, Topics: [][32]byte{chainTransferTopic, w32(other[:]), w32(other[:])}, Data: amount[:]}}}, // another wallet's transfer: never taken
		tx3: {Status: 0, BlockNumber: 19},
	}}
	c := &chainReader{node: n, contract: budget, token: token, depth: 2, kept: map[string]chainTx{}}
	ctx := context.Background()
	get := func(hash string, to eth.Address) *chainTx {
		t.Helper()
		r, err := c.tx(ctx, hash, 20, payer, to)
		if err != nil {
			t.Fatal(err)
		}
		return r
	}
	r1 := get(tx1, shop)
	if r1.Status != "success" || r1.Block != 9 || r1.Spent != "500000" || r1.Transfer != nil {
		t.Fatalf("tx#1: %+v", r1)
	}
	r2 := get(tx2, shop)
	if r2.Status != "success" || r2.Transfer == nil || *r2.Transfer != (chainTransfer{From: payer.Hex(), To: shop.Hex(), Amount: "500000", ToShop: true}) || r2.Spent != "" {
		t.Fatalf("tx#2: %+v", r2)
	}
	c.kept = map[string]chainTx{}
	if r := get(tx2, other); r.Transfer == nil || r.Transfer.ToShop {
		t.Fatalf("a transfer to another address than the purchase's pay-to is not said to be the shop's: %+v", r.Transfer)
	}
	if r3 := get(tx3, shop); r3.Status != "reverted" {
		t.Fatalf("a reverted transaction: %+v", r3)
	}
	if r := get("0x"+strings.Repeat("4", 64), shop); r.Status != "not_found" {
		t.Fatalf("no receipt: %+v", r)
	}
	if r := get("not a hash", shop); r.Status != "not_found" {
		t.Fatalf("a bad hash: %+v", r)
	}
	if get("", shop) != nil {
		t.Fatal("no hash: nothing")
	}
	get(tx1, shop) // read again after the reset above: kept from here on
	asked := n.asked
	get(tx1, shop) // deep enough: kept
	get(tx3, shop) // block 19 at head 20: not deep enough, asked again
	if n.asked != asked+1 {
		t.Fatalf("asked %d times more, want 1 (tx#1 kept, tx#3 asked again)", n.asked-asked)
	}
	// A node that does not answer is an error, never "not found" (review).
	n.err = errors.New("429")
	if _, err := c.tx(ctx, "0x"+strings.Repeat("5", 64), 20, payer, shop); err == nil {
		t.Fatal("a node error reads as not found")
	}
}

// The route's rules (review): the task's own read rules, and no chain on a
// host that does not pay.
func TestChainRouteRules(t *testing.T) {
	h := startHost(t, hostOpts{expiryEvery: time.Hour, runs: []planRun{{TaskID: "task-a", Scenario: mainline(t)}}})
	defer h.stop()
	h.waitRuns()
	for name, c := range map[string]struct {
		token, err string
		code       int
	}{
		"the host, no --payments":   {h.tokens["host"], "no_chain", 404},
		"its own viewer":            {h.tokens["viewer:consumer-a"], "no_chain", 404},
		"another consumer's viewer": {h.tokens["viewer:consumer-b"], "not_found", 404},
		"the operator":              {h.tokens["operator"], "forbidden_role", 403},
	} {
		code, body, _ := h.get("/api/tasks/task-a/chain", c.token)
		if code != c.code || !strings.Contains(string(body), c.err) {
			t.Errorf("%s: %d %s", name, code, body)
		}
	}
}

// The view takes the payer from the task's own binding and the shop from
// each purchase (review); a node error is an error, not an empty answer.
func TestChainViewUsesTheTasksPayerAndEachShop(t *testing.T) {
	token, payer, shop := eth.Address{0x36}, eth.Address{0x11}, eth.Address{0x33}
	amount := w32(big.NewInt(500000).Bytes())
	tx2 := "0x" + strings.Repeat("2", 64)
	n := &stubNode{head: 20, receipts: map[string]*eth.Receipt{
		tx2: {Status: 1, BlockNumber: 10, Logs: []eth.Log{{Address: token, Topics: [][32]byte{chainTransferTopic, w32(payer[:]), w32(shop[:])}, Data: amount[:]}}},
	}}
	ref := "eip155:84532/erc721:0x8004A818BFB912233c491871b3d84c89A494BD9e/10569"
	c := &chainReader{node: n, token: token, depth: 2, kept: map[string]chainTx{}, agents: map[string]string{"consumer-a": ref}}
	rec := store.TaskRecord{Binding: store.TaskBinding{ConsumerID: "consumer-a", Payer: strings.ToUpper(payer.Hex()[2:])}, Task: store.TaskView{Purchases: []store.PurchaseView{
		{PurchaseOrderID: "po-1", PayTo: shop.Hex(), Payment: &store.PaymentView{SettleTx: tx2}},
		{PurchaseOrderID: "po-2", PayTo: shop.Hex()}, // no payment: not asked
	}}}
	rec.Binding.Payer = "0x" + rec.Binding.Payer
	out, err := c.view(context.Background(), "local_anvil", rec)
	if err != nil || out.Head != 20 || len(out.Purchases) != 1 {
		t.Fatalf("view: %+v %v", out, err)
	}
	if tr := out.Purchases["po-1"].Settle.Transfer; tr == nil || !tr.ToShop || out.Purchases["po-1"].Reserve != nil {
		t.Fatalf("po-1: %+v", out.Purchases["po-1"])
	}
	// The addresses to look up: the task's payer as 0x hex (lower case), the token; no budget contract known, none said.
	if out.Payer != payer.Hex() || out.Token != token.Hex() || out.Contract != "" {
		t.Fatalf("addresses: payer %q token %q contract %q", out.Payer, out.Token, out.Contract)
	}
	// The task's own consumer's Agent identity, the registry's; another consumer's task says none of it.
	if out.Agent != ref {
		t.Fatalf("agent %q", out.Agent)
	}
	// Its agentWallet on chain: none while the registry has none (asked again), then the wallet, kept.
	if out.AgentWallet != "" {
		t.Fatalf("no agentWallet read as %q", out.AgentWallet)
	}
	n.wallet = make([]byte, 32)
	if out, _ := c.view(context.Background(), "local_anvil", rec); out.AgentWallet != "" {
		t.Fatalf("the zero address read as %q", out.AgentWallet)
	}
	pw := w32(payer[:])
	n.wallet = pw[:]
	if out, _ := c.view(context.Background(), "local_anvil", rec); out.AgentWallet != payer.Hex() {
		t.Fatalf("agentWallet %q, want %s", out.AgentWallet, payer.Hex())
	}
	calls := n.calls
	if out, _ := c.view(context.Background(), "local_anvil", rec); out.AgentWallet != payer.Hex() || n.calls != calls {
		t.Fatalf("a set agentWallet is asked again (%d calls) or lost: %q", n.calls-calls, out.AgentWallet)
	}
	rec.Binding.ConsumerID = "consumer-b"
	if out, _ := c.view(context.Background(), "local_anvil", rec); out.Agent != "" || out.AgentWallet != "" {
		t.Fatalf("consumer-b's task names %q %q", out.Agent, out.AgentWallet)
	}
	rec.Binding.ConsumerID = "consumer-a"
	rec.Task.Purchases[0].PayTo = eth.Address{0x44}.Hex() // another shop than the transfer's
	c.kept = map[string]chainTx{}
	if out, _ := c.view(context.Background(), "local_anvil", rec); out.Purchases["po-1"].Settle.Transfer.ToShop {
		t.Fatal("a transfer to another address reads as the shop's")
	}
	n.err = errors.New("timeout")
	c.kept = map[string]chainTx{}
	if _, err := c.view(context.Background(), "local_anvil", rec); err == nil {
		t.Fatal("a node error reads as an answer")
	}
}

// refundNode stands in for the Host's node: chainNode's receipts, and a transaction's sender.
type refundStub struct {
	*stubNode
	from string
}

func (n refundStub) Raw(_ context.Context, method string, out any, _ ...any) error {
	if n.from == "" {
		return errors.New("no answer")
	}
	*(out.(*struct {
		From string `json:"from"`
	})) = struct {
		From string `json:"from"`
	}{n.from}
	return nil
}

// docs/commercial-refund.md: the Host records a refund only when its own node has it confirmed, moving the
// purchase's whole amount from the transaction's sender to the task's payer; anything else is not this refund.
func TestRefundOnChain(t *testing.T) {
	token, payer, shop, other := eth.Address{0x36}, eth.Address{0x11}, eth.Address{0x33}, eth.Address{0x44}
	amount := big.NewInt(80000)
	w := w32(amount.Bytes())
	tx := "0x" + strings.Repeat("7", 64)
	transfer := func(from, to eth.Address, v [32]byte) eth.Log {
		return eth.Log{Address: token, Topics: [][32]byte{chainTransferTopic, w32(from[:]), w32(to[:])}, Data: v[:]}
	}
	for name, c := range map[string]struct {
		rc      *eth.Receipt
		head    uint64
		from    string
		block   uint64
		pending bool
	}{
		"confirmed":             {&eth.Receipt{Status: 1, BlockNumber: 10, Logs: []eth.Log{transfer(shop, payer, w)}}, 11, shop.Hex(), 10, false},
		"not on the node yet":   {nil, 11, shop.Hex(), 0, true},
		"not deep yet":          {&eth.Receipt{Status: 1, BlockNumber: 11, Logs: []eth.Log{transfer(shop, payer, w)}}, 11, shop.Hex(), 0, true},
		"no sender read":        {&eth.Receipt{Status: 1, BlockNumber: 10, Logs: []eth.Log{transfer(shop, payer, w)}}, 11, "", 0, true},
		"reverted":              {&eth.Receipt{Status: 0, BlockNumber: 10}, 11, shop.Hex(), 0, false},
		"from another account":  {&eth.Receipt{Status: 1, BlockNumber: 10, Logs: []eth.Log{transfer(other, payer, w)}}, 11, shop.Hex(), 0, false},
		"to another wallet":     {&eth.Receipt{Status: 1, BlockNumber: 10, Logs: []eth.Log{transfer(shop, other, w)}}, 11, shop.Hex(), 0, false},
		"part of the amount":    {&eth.Receipt{Status: 1, BlockNumber: 10, Logs: []eth.Log{transfer(shop, payer, w32(big.NewInt(1).Bytes()))}}, 11, shop.Hex(), 0, false},
		"another token's":       {&eth.Receipt{Status: 1, BlockNumber: 10, Logs: []eth.Log{{Address: other, Topics: transfer(shop, payer, w).Topics, Data: w[:]}}}, 11, shop.Hex(), 0, false},
	} {
		n := &stubNode{head: c.head, receipts: map[string]*eth.Receipt{}}
		if c.rc != nil {
			n.receipts[tx] = c.rc
		}
		f := &refunder{node: refundStub{n, c.from}, token: token, depth: 2}
		block, pending, why := f.onChain(context.Background(), tx, payer.Hex(), amount)
		if block != c.block || pending != c.pending || (block == 0) != (why != "") {
			t.Errorf("%s: block %d pending %v why %q", name, block, pending, why)
		}
	}
}

// The route's rules: the operator only; a host that does not pay has nothing to refund.
func TestRefundRouteRules(t *testing.T) {
	h := startHost(t, hostOpts{expiryEvery: time.Hour, runs: []planRun{{TaskID: "task-a", Scenario: mainline(t)}}})
	defer h.stop()
	h.waitRuns()
	path := "/api/host/tasks/task-a/purchases/po-1/refund"
	if code, body := h.post(path, h.tokens["host"], `{}`); code != 403 || body["error"] != "forbidden_role" {
		t.Errorf("the host's read token: %d %v", code, body)
	}
	if code, body := h.post(path, h.tokens["operator"], `{}`); code != 404 || body["error"] != "no_refund" {
		t.Errorf("no --payments: %d %v", code, body)
	}
}
