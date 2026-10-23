package main

import (
	"context"
	"encoding/hex"
	"encoding/json"
	"errors"
	"math/big"
	"strings"
	"testing"

	"github.com/SIMPLYBOYS/agent-payment-authority/internal/eth"
)

// stubNode is a node the test writes: receipts by hash, one block time.
type stubNode struct {
	head     uint64
	receipts map[string]*eth.Receipt
	asked    int
}

func (n *stubNode) BlockNumber(context.Context) (uint64, error) { return n.head, nil }
func (n *stubNode) Receipt(_ context.Context, h []byte) (*eth.Receipt, error) {
	n.asked++
	return n.receipts["0x"+hex.EncodeToString(h)], nil
}
func (n *stubNode) Raw(_ context.Context, method string, out any, _ ...any) error {
	if method != "eth_getBlockByNumber" {
		return errors.New("unexpected " + method)
	}
	raw, _ := json.Marshal(map[string]string{"timestamp": "0x6a3f2c00"})
	return json.Unmarshal(raw, out)
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
	tx1, tx2, tx3 := "0x"+strings.Repeat("1", 64), "0x"+strings.Repeat("2", 64), "0x"+strings.Repeat("3", 64)
	n := &stubNode{head: 20, receipts: map[string]*eth.Receipt{
		tx1: {Status: 1, BlockNumber: 9, Logs: []eth.Log{{Address: other, Topics: [][32]byte{chainSpentTopic}, Data: spent}, {Address: budget, Topics: [][32]byte{chainSpentTopic}, Data: spent}}},
		tx2: {Status: 1, BlockNumber: 10, Logs: []eth.Log{{Address: token, Topics: [][32]byte{chainTransferTopic, w32(payer[:]), w32(shop[:])}, Data: amount[:]}}},
		tx3: {Status: 0, BlockNumber: 19},
	}}
	c := &chainReader{node: n, contract: budget, token: token, depth: 2, kept: map[string]chainTx{}}
	ctx := context.Background()
	r1 := c.tx(ctx, tx1, 20)
	if r1.Status != "success" || r1.Block != 9 || r1.Spent != "500000" || r1.Time != 0x6a3f2c00 || r1.Transfer != nil {
		t.Fatalf("tx#1: %+v", r1)
	}
	r2 := c.tx(ctx, tx2, 20)
	if r2.Status != "success" || r2.Transfer == nil || *r2.Transfer != (chainTransfer{From: payer.Hex(), To: shop.Hex(), Amount: "500000"}) || r2.Spent != "" {
		t.Fatalf("tx#2: %+v", r2)
	}
	if r3 := c.tx(ctx, tx3, 20); r3.Status != "reverted" {
		t.Fatalf("a reverted transaction: %+v", r3)
	}
	if r := c.tx(ctx, "0x"+strings.Repeat("4", 64), 20); r.Status != "not_found" {
		t.Fatalf("no receipt: %+v", r)
	}
	if r := c.tx(ctx, "not a hash", 20); r.Status != "not_found" {
		t.Fatalf("a bad hash: %+v", r)
	}
	if c.tx(ctx, "", 20) != nil {
		t.Fatal("no hash: nothing")
	}
	asked := n.asked
	c.tx(ctx, tx1, 20) // deep enough: kept
	c.tx(ctx, tx3, 20) // block 19 at head 20: not deep enough, asked again
	if n.asked != asked+1 {
		t.Fatalf("asked %d times more, want 1 (tx#1 kept, tx#3 asked again)", n.asked-asked)
	}
}
