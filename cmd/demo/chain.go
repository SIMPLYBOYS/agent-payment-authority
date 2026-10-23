package main

// GET /api/tasks/{task_id}/chain (docs/commercial-ops.md O2): what the chain
// says of a task's payments, read by the Host from its own node, so the
// audience sees the Agent's purchase on the chain (also on a local chain,
// which has no explorer). Only the transactions the Store recorded for the
// task's purchases are asked, under the task's own read rules; nothing is
// computed from the page. A receipt, once deep enough, is kept in memory.

import (
	"context"
	"encoding/hex"
	"errors"
	"math/big"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/SIMPLYBOYS/agent-payment-authority/internal/eth"
	"github.com/SIMPLYBOYS/agent-payment-authority/internal/store"
)

var (
	chainSpentTopic    = eth.Topic("Spent(uint256,bytes32,uint256,uint256,uint256)")
	chainTransferTopic = eth.Topic("Transfer(address,address,uint256)")
)

// chainNode is what the Host asks of its node (eth.Client); tests stand in.
type chainNode interface {
	BlockNumber(ctx context.Context) (uint64, error)
	Receipt(ctx context.Context, hash []byte) (*eth.Receipt, error)
}

// chainTx is one transaction as the chain has it.
type chainTx struct {
	Hash   string `json:"hash"`
	Status string `json:"status"` // success | reverted | not_found (no receipt yet, or not on this chain)
	Block  uint64 `json:"block,omitempty"`
	// Spent: tx#1's amount as the budget contract recorded it (base units).
	Spent string `json:"spent,omitempty"`
	// Transfer: tx#2's token transfer.
	Transfer *chainTransfer `json:"transfer,omitempty"`
}

// chainTransfer: the token's transfer out of the task's own payer wallet
// (review: never another transfer in the same transaction); ToShop says it
// went to the purchase's pay-to address.
type chainTransfer struct {
	From   string `json:"from"`
	To     string `json:"to"`
	Amount string `json:"amount"` // base units
	ToShop bool   `json:"to_shop"`
}

type chainPurchase struct {
	Reserve *chainTx `json:"reserve,omitempty"`
	Settle  *chainTx `json:"settle,omitempty"`
}

type chainReply struct {
	Chain     string                   `json:"chain"` // the host's payment capability
	Head      uint64                   `json:"head"`  // the latest block now
	Purchases map[string]chainPurchase `json:"purchases"`
}

// chainReader reads and keeps the receipts (a final one never changes).
type chainReader struct {
	node     chainNode
	contract eth.Address // the budget contract (tx#1's Spent)
	token    eth.Address // the token (tx#2's Transfer)
	depth    uint64      // a receipt this deep is kept
	mu       sync.Mutex
	kept     map[string]chainTx
}

// tx: one recorded transaction. A node that does not answer is an error
// (the page keeps its last read); no receipt is not_found.
func (c *chainReader) tx(ctx context.Context, hash string, head uint64, payer, payTo eth.Address) (*chainTx, error) {
	if hash == "" {
		return nil, nil
	}
	c.mu.Lock()
	t, ok := c.kept[hash]
	c.mu.Unlock()
	if ok {
		return &t, nil
	}
	t = chainTx{Hash: hash, Status: "not_found"}
	raw, err := hex.DecodeString(strings.TrimPrefix(hash, "0x"))
	if err != nil || len(raw) != 32 {
		return &t, nil
	}
	r, err := c.node.Receipt(ctx, raw)
	if err != nil {
		return nil, err
	}
	if r == nil {
		return &t, nil
	}
	t.Status, t.Block = "reverted", r.BlockNumber
	if r.Status == 1 {
		t.Status = "success"
	}
	for _, l := range r.Logs {
		switch {
		case l.Address == c.contract && len(l.Topics) > 0 && l.Topics[0] == chainSpentTopic && len(l.Data) >= 64:
			t.Spent = new(big.Int).SetBytes(l.Data[32:64]).String()
		case l.Address == c.token && len(l.Topics) == 3 && l.Topics[0] == chainTransferTopic && len(l.Data) >= 32:
			var from, to eth.Address
			copy(from[:], l.Topics[1][12:])
			copy(to[:], l.Topics[2][12:])
			if from == payer { // the task's own wallet only
				t.Transfer = &chainTransfer{From: from.Hex(), To: to.Hex(), Amount: new(big.Int).SetBytes(l.Data[:32]).String(), ToShop: to == payTo}
			}
		}
	}
	if head >= r.BlockNumber+c.depth {
		c.mu.Lock()
		c.kept[hash] = t
		c.mu.Unlock()
	}
	return &t, nil
}

func (a *api) chain(w http.ResponseWriter, r *http.Request) {
	rec, ok := a.record(w, r)
	if !ok {
		return
	}
	if a.chainRead == nil {
		fail(w, http.StatusNotFound, "no_chain") // a host that does not pay has no chain to ask
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 10*time.Second)
	defer cancel()
	out, err := a.chainRead.view(ctx, a.payCap, rec)
	if err != nil {
		fail(w, http.StatusServiceUnavailable, "chain_unavailable") // the node's text stays here
		return
	}
	reply(w, http.StatusOK, out)
}

// view: a task's recorded transactions as the chain has them; the payer
// is the task's own wallet, the shop each purchase's pay-to.
func (c *chainReader) view(ctx context.Context, capability string, rec store.TaskRecord) (chainReply, error) {
	head, err := c.node.BlockNumber(ctx)
	if err != nil {
		return chainReply{}, err
	}
	out := chainReply{Chain: capability, Head: head, Purchases: map[string]chainPurchase{}}
	payer, _ := eth.ParseAddress(rec.Binding.Payer) // a zero address matches no transfer
	for _, p := range rec.Task.Purchases {
		if p.Payment == nil || (p.Payment.ReserveTx == "" && p.Payment.SettleTx == "") {
			continue
		}
		payTo, _ := eth.ParseAddress(strings.ToLower(p.PayTo))
		res, err1 := c.tx(ctx, p.Payment.ReserveTx, head, payer, payTo)
		set, err2 := c.tx(ctx, p.Payment.SettleTx, head, payer, payTo)
		if err := errors.Join(err1, err2); err != nil {
			return chainReply{}, err
		}
		out.Purchases[p.PurchaseOrderID] = chainPurchase{Reserve: res, Settle: set}
	}
	return out, nil
}
