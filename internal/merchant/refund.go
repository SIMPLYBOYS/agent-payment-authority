package merchant

// The shop's refund of a payment it settled and did not deliver (docs/commercial-refund.md): the whole amount
// back to its payer, in one token transfer from the key that sends the shop's transfers (on the testnet that is
// pay_to itself, P2b-D3), once per settlement. A settlement's own transaction names it; its payer and amount are
// the shop's record of it, never a caller's. The refund takes its nonce as a settlement does, written before it
// is sent, so the two never wait behind each other's.

import (
	"context"
	"database/sql"
	"encoding/hex"
	"errors"
	"strconv"
	"strings"
	"time"

	"github.com/SIMPLYBOYS/agent-payment-authority/internal/eth"
)

const refundSchema = `
CREATE TABLE IF NOT EXISTS refunds (
  settle_tx  TEXT PRIMARY KEY,
  payer      TEXT NOT NULL,
  amount     INTEGER NOT NULL CHECK (amount > 0),
  raw_tx     TEXT NOT NULL,
  tx_hash    TEXT NOT NULL UNIQUE,
  tx_nonce   INTEGER NOT NULL,
  state      TEXT NOT NULL CHECK (state IN ('sent', 'refunded', 'failed')),
  block      INTEGER,
  created_at INTEGER NOT NULL
) STRICT;
`

// RefundOutcome is a refund as the shop has it: refunded once its transfer is confirmed (Transaction, Block);
// pending (asked again, the same transfer is sent again and checked, never a second one); failed (it reverted
// or its nonce went to another: asked again, a new transfer); invalid (no settlement of the shop's to refund).
type RefundOutcome struct {
	Kind        string `json:"kind"`
	Reason      string `json:"reason,omitempty"`
	Transaction string `json:"transaction,omitempty"`
	Block       uint64 `json:"block,omitempty"`
	Payer       string `json:"payer,omitempty"`
	Amount      string `json:"amount,omitempty"`
}

// Refund refunds the shop's settlement in transaction settleTx (0x hex).
func (s *Settler) Refund(ctx context.Context, settleTx string) RefundOutcome {
	h := strings.ToLower(strings.TrimPrefix(settleTx, "0x"))
	if b, err := hex.DecodeString(h); err != nil || len(b) != 32 || !strings.HasPrefix(settleTx, "0x") {
		return RefundOutcome{Kind: "invalid", Reason: "invalid_transaction"}
	}
	unavailable := RefundOutcome{Kind: "pending", Reason: "refund_store_unavailable"}
	s.mu.Lock()
	locked := true
	defer func() {
		if locked {
			s.mu.Unlock()
		}
	}()
	var state string
	switch err := s.db.QueryRowContext(ctx, `SELECT state FROM refunds WHERE settle_tx = ?`, h).Scan(&state); {
	case err == nil && state != "failed":
		s.mu.Unlock()
		locked = false
		return s.refunding(ctx, h, true)
	case err != nil && !errors.Is(err, sql.ErrNoRows):
		return unavailable
	}
	var payer, settled string
	var amount uint64
	switch err := s.db.QueryRowContext(ctx, `SELECT payer, amount, state FROM settlements WHERE tx_hash = ?`, h).Scan(&payer, &amount, &settled); {
	case errors.Is(err, sql.ErrNoRows) || err == nil && settled != "settled":
		return RefundOutcome{Kind: "invalid", Reason: "not_a_settlement"} // not the shop's, or not (yet) paid
	case err != nil:
		return unavailable
	}
	to, err := eth.ParseAddress(strings.ToLower(payer))
	if err != nil {
		return unavailable
	}
	if err := s.settleOpen(ctx); err != nil { // unfinished transfers first, as a settlement does
		return unavailable
	}
	raw, hash, nonce, _, r := s.prepare(ctx, mustCall("transfer(address,uint256)", to, amount))
	if r != "" {
		return RefundOutcome{Kind: "pending", Reason: r} // nothing written: asked again, it tries again
	}
	// Written before it is sent: asked again, this transfer is sent again, never a new one.
	q := `INSERT INTO refunds (settle_tx, payer, amount, raw_tx, tx_hash, tx_nonce, state, created_at) VALUES (?, ?, ?, ?, ?, ?, 'sent', ?)`
	if state == "failed" {
		q = `UPDATE refunds SET payer = ?2, amount = ?3, raw_tx = ?4, tx_hash = ?5, tx_nonce = ?6, state = 'sent', block = NULL, created_at = ?7 WHERE settle_tx = ?1`
	}
	if _, err := s.db.ExecContext(ctx, q, h, payer, amount, hex.EncodeToString(raw), hex.EncodeToString(hash), nonce, s.clock()); err != nil {
		return unavailable
	}
	if err := at("refund-written"); err != nil {
		return RefundOutcome{Kind: "pending", Reason: "refund_pending"}
	}
	s.mu.Unlock()
	locked = false
	return s.refunding(ctx, h, true)
}

// refunding brings a written refund up to the chain at confirmed depth, as decide does a settlement: refunded
// once its transfer is confirmed with the token's Transfer of the amount from the shop's key to the payer;
// failed when it reverted or its nonce went to another. wait: send it again and keep checking for at most s.wait.
func (s *Settler) refunding(ctx context.Context, settleTx string, wait bool) RefundOutcome {
	var state, hash, rawHex, payer string
	var nonce, amount uint64
	var block sql.NullInt64
	if err := s.db.QueryRowContext(ctx, `SELECT state, tx_hash, raw_tx, tx_nonce, payer, amount, block FROM refunds WHERE settle_tx = ?`, settleTx).
		Scan(&state, &hash, &rawHex, &nonce, &payer, &amount, &block); err != nil {
		return RefundOutcome{Kind: "pending", Reason: "refund_store_unavailable"}
	}
	out := RefundOutcome{Transaction: "0x" + hash, Payer: payer, Amount: strconv.FormatUint(amount, 10)}
	switch state {
	case "refunded":
		out.Kind, out.Block = "refunded", uint64(block.Int64)
		return out
	case "failed":
		out.Kind, out.Reason = "failed", "refund_failed"
		return out
	}
	raw, _ := hex.DecodeString(rawHex)
	h, _ := hex.DecodeString(hash)
	to, _ := eth.ParseAddress(strings.ToLower(payer))
	ctx, cancel := context.WithTimeout(ctx, s.wait)
	defer cancel()
	if wait {
		s.rpc.SendRaw(ctx, raw)
	}
	for {
		rc, err := s.rpc.Check(ctx, h, s.gas.Addr, nonce, 0, s.cfg.Confirmations) // a transfer has no deadline
		next := ""
		switch {
		case errors.Is(err, eth.ErrDropped) || rc != nil && rc.Status != 1:
			next = "failed"
		case rc != nil && !s.moved(rc, s.gas.Addr, to, amount):
			next = "failed" // succeeded without the transfer: no refund
		case rc != nil:
			next = "refunded"
		}
		if next != "" {
			var b any
			if rc != nil {
				b = rc.BlockNumber
			}
			if _, err := s.db.ExecContext(context.WithoutCancel(ctx), `UPDATE refunds SET state = ?, block = ? WHERE settle_tx = ? AND state = 'sent'`, next, b, settleTx); err != nil {
				return RefundOutcome{Kind: "pending", Reason: "refund_store_unavailable", Transaction: out.Transaction}
			}
			return s.refunding(context.WithoutCancel(ctx), settleTx, false)
		}
		if !wait {
			out.Kind, out.Reason = "pending", "refund_pending"
			return out
		}
		select {
		case <-ctx.Done():
			out.Kind, out.Reason = "pending", "refund_pending"
			return out
		case <-time.After(s.poll):
		}
	}
}

// moved: the receipt holds the token's own Transfer of amount from `from` to `to`.
func (s *Settler) moved(rc *eth.Receipt, from, to eth.Address, amount uint64) bool {
	for _, l := range rc.Logs {
		if l.Removed || l.Address != s.cfg.Token || len(l.Topics) != 3 || l.Topics[0] != transferTopic {
			continue
		}
		f, err1 := eth.WordAddress(l.Topics[1][:])
		t, err2 := eth.WordAddress(l.Topics[2][:])
		v, err3 := eth.Uint(l.Data, 0)
		if errors.Join(err1, err2, err3) == nil && f == from && t == to && v.IsUint64() && v.Uint64() == amount {
			return true
		}
	}
	return false
}
