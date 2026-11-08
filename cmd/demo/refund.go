package main

// POST /api/host/tasks/{task_id}/purchases/{purchase_order_id}/refund (docs/commercial-refund.md): a person,
// the operator, asks the shop to refund a payment it settled and did not deliver. The Host records the refund
// (PAYMENT_REFUNDED) only once its own node has the refund's transfer confirmed: the purchase's whole amount
// back to the task's payer wallet, from the account that sent it. The shop's word alone is not enough.

import (
	"context"
	"encoding/hex"
	"errors"
	"math/big"
	"net/http"
	"strings"
	"time"

	"github.com/SIMPLYBOYS/agent-payment-authority/internal/eth"
	"github.com/SIMPLYBOYS/agent-payment-authority/internal/merchant"
	"github.com/SIMPLYBOYS/agent-payment-authority/internal/store"
)

// refundNode is what the refund check asks of the Host's node (eth.Client); tests stand in.
type refundNode interface {
	BlockNumber(ctx context.Context) (uint64, error)
	Receipt(ctx context.Context, hash []byte) (*eth.Receipt, error)
	Raw(ctx context.Context, method string, out any, params ...any) error
}

// refunder asks the shop (its settlement: its own settler or apa-facilitator) and reads the chain.
type refunder struct {
	shop  interface{ Refund(context.Context, string) merchant.RefundOutcome }
	node  refundNode
	token eth.Address
	depth uint64        // confirmations, as a settlement's
	wait  time.Duration // the longest one request waits for the shop
}

// onChain is the refund's transfer as the node has it: its block once confirmed to depth with the token's
// Transfer of amount from the transaction's own sender to payer; "" with a reason otherwise (pending: not yet
// deep, or not found; another: not this refund).
func (f *refunder) onChain(ctx context.Context, tx, payer string, amount *big.Int) (block uint64, pending bool, why string) {
	h, err := hex.DecodeString(strings.TrimPrefix(tx, "0x"))
	if err != nil || len(h) != 32 || !strings.HasPrefix(tx, "0x") {
		return 0, false, "not a transaction hash"
	}
	to, err := eth.ParseAddress(strings.ToLower(payer))
	if err != nil {
		return 0, false, "the task has no payer wallet"
	}
	rc, err1 := f.node.Receipt(ctx, h)
	head, err2 := f.node.BlockNumber(ctx)
	switch {
	case errors.Join(err1, err2) != nil, rc == nil:
		return 0, true, "not on the node yet"
	case rc.Status != 1:
		return 0, false, "it reverted"
	case head+1 < rc.BlockNumber+f.depth:
		return 0, true, "not confirmed yet"
	}
	var t struct {
		From string `json:"from"`
	}
	if err := f.node.Raw(ctx, "eth_getTransactionByHash", &t, tx); err != nil {
		return 0, true, "its sender cannot be read yet"
	}
	from, err := eth.ParseAddress(strings.ToLower(t.From))
	if err != nil {
		return 0, false, "no sender"
	}
	for _, l := range rc.Logs {
		if l.Removed || l.Address != f.token || len(l.Topics) != 3 || l.Topics[0] != chainTransferTopic {
			continue
		}
		a, err1 := eth.WordAddress(l.Topics[1][:])
		b, err2 := eth.WordAddress(l.Topics[2][:])
		v, err3 := eth.Uint(l.Data, 0)
		if errors.Join(err1, err2, err3) == nil && a == from && b == to && v.Cmp(amount) == 0 {
			return rc.BlockNumber, false, ""
		}
	}
	return 0, false, "it moves no " + amount.String() + " of the token from its sender to the payer"
}

func (a *api) refund(w http.ResponseWriter, r *http.Request) {
	var empty struct{}
	if !postGuard(w, r, a, &empty) {
		return
	}
	if a.refunds == nil {
		fail(w, http.StatusNotFound, "no_refund") // a host that does not pay refunds nothing
		return
	}
	id, po := r.PathValue("task_id"), r.PathValue("purchase_order_id")
	if !taskIDFormat.MatchString(id) || po == "" {
		fail(w, http.StatusBadRequest, "bad_request")
		return
	}
	rec, err := a.st.ReadTask(id, func(string) bool { return true })
	switch {
	case store.Code(err) == "OWNERSHIP_MISMATCH":
		fail(w, http.StatusNotFound, "not_found")
		return
	case err != nil:
		storeDown(w, store.CodeOf(err))
		return
	}
	var p *store.PurchaseView
	for i := range rec.Task.Purchases {
		if rec.Task.Purchases[i].PurchaseOrderID == po {
			p = &rec.Task.Purchases[i]
		}
	}
	if p == nil || p.Payment == nil {
		fail(w, http.StatusNotFound, "not_found")
		return
	}
	pv := p.Payment
	if pv.RefundTx != "" {
		reply(w, http.StatusOK, map[string]any{"state": "refunded", "refund_tx": pv.RefundTx, "refund_block": pv.RefundBlock})
		return
	}
	if pv.State != "needs_review" || pv.Code != "DELIVERY_FAILED" || pv.SettleTx == "" {
		fail(w, http.StatusConflict, "not_refundable") // only a payment settled and not delivered
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), a.refunds.wait)
	defer cancel()
	o := a.refunds.shop.Refund(ctx, pv.SettleTx)
	switch o.Kind {
	case "refunded":
	case "pending":
		reply(w, http.StatusAccepted, map[string]any{"state": "pending", "reason": o.Reason}) // asked again, the same refund
		return
	default:
		reply(w, http.StatusConflict, map[string]any{"error": "refund_refused", "reason": o.Reason})
		return
	}
	amount, _ := new(big.Int).SetString(string(p.Amount), 10)
	block, pending, why := a.refunds.onChain(ctx, o.Transaction, rec.Binding.Payer, amount)
	switch {
	case pending:
		reply(w, http.StatusAccepted, map[string]any{"state": "pending", "reason": "refund_not_confirmed"})
		return
	case why != "":
		reply(w, http.StatusConflict, map[string]any{"error": "refund_not_on_chain", "detail": why})
		return
	}
	if _, err := a.st.PaymentRefunded(p.IntentHash, o.Transaction, block); err != nil {
		storeDown(w, store.CodeOf(err))
		return
	}
	reply(w, http.StatusOK, map[string]any{"state": "refunded", "refund_tx": o.Transaction, "refund_block": block})
}
