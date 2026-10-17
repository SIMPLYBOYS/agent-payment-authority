// Package reconcile is P2's Reconciler (docs/commercial-p2.md 9.12): what
// the chain says about EIP-3009 payments, read at confirmed depth only, and
// nothing kept. The same question asked again gets the same answer, so
// processing a payment twice changes nothing (§7). It reads the token's own
// events, never the submitter's transaction (commercial-transaction-flow
// §1 step 6: the token's from, not the facilitator's sender).
package reconcile

import (
	"context"
	"errors"
	"fmt"
	"math/big"

	"github.com/SIMPLYBOYS/agent-payment-authority/internal/eth"
)

var (
	transferTopic = eth.Topic("Transfer(address,address,uint256)")
	usedTopic     = eth.Topic("AuthorizationUsed(address,bytes32)")
)

// Token is the chain and EIP-3009 token reconciled, at a depth.
type Token struct {
	RPC           *eth.Client
	Address       eth.Address
	Confirmations uint64 // at least 1
	FromBlock     uint64 // no payment of interest before it
}

// Authorization is a payment the Signer signed: who pays whom how much,
// until when, under which nonce.
type Authorization struct {
	From, To    eth.Address
	Value       uint64
	ValidBefore uint64
	Nonce       eth.Bytes32
}

// Result is a payment's state on chain.
//
//   - settled: its nonce is used and the same transaction holds the token's
//     Transfer of Value from From to To;
//   - pending: unused, and still valid at confirmed depth (it may yet be
//     settled: never pay it again);
//   - not_settled_expired: unused, and the confirmed block's time is at or
//     past ValidBefore (the token refuses it in any later block);
//   - needs_review: its nonce is used, but not by that transfer (another
//     recipient or amount than recorded, a cancellation, or no event).
type Result struct {
	State string
	Tx    [32]byte // settled, or the transaction that used the nonce
	Block uint64
}

func (t Token) confirmed(ctx context.Context) (uint64, error) {
	latest, err := t.RPC.BlockNumber(ctx)
	if err != nil {
		return 0, err
	}
	c := max(t.Confirmations, 1)
	if latest+1 < c {
		return 0, errors.New("reconcile: the chain is shorter than the confirmations")
	}
	return latest + 1 - c, nil
}

func word(a eth.Address) [32]byte {
	var w [32]byte
	copy(w[12:], a[:])
	return w
}

// Status is one authorization's state at confirmed depth.
func (t Token) Status(ctx context.Context, a Authorization) (Result, error) {
	at, err := t.confirmed(ctx)
	if err != nil {
		return Result{}, err
	}
	data, _ := eth.Call("authorizationState(address,bytes32)", a.From, a.Nonce)
	out, err := t.RPC.CallAt(ctx, eth.Msg{To: t.Address, Data: data}, at)
	if err != nil {
		return Result{}, err
	}
	used, err := eth.Uint(out, 0)
	if err != nil {
		return Result{}, err
	}
	if used.Sign() == 0 {
		h, err := t.RPC.HeaderAt(ctx, at)
		if err != nil {
			return Result{}, err
		}
		if h.Timestamp >= a.ValidBefore {
			return Result{State: "not_settled_expired", Block: at}, nil
		}
		return Result{State: "pending", Block: at}, nil
	}
	logs, err := t.RPC.FilterLogs(ctx, t.Address, [][32]byte{usedTopic, word(a.From), a.Nonce}, t.FromBlock, at)
	if err != nil {
		return Result{}, err
	}
	var live []eth.Log
	for _, l := range logs {
		if !l.Removed {
			live = append(live, l)
		}
	}
	if len(live) != 1 {
		return Result{State: "needs_review", Block: at}, nil // used, and no single event says by what
	}
	rc, err := t.RPC.Receipt(ctx, live[0].TxHash[:])
	if err != nil {
		return Result{}, err
	}
	r := Result{State: "needs_review", Tx: live[0].TxHash, Block: live[0].BlockNumber}
	if rc != nil && rc.Status == 1 && transfers(rc.Logs, t.Address, a.From, a.To, a.Value) {
		r.State = "settled"
	}
	return r, nil
}

// transfers: logs hold token's Transfer of value from `from` to `to`.
func transfers(logs []eth.Log, token, from, to eth.Address, value uint64) bool {
	for _, l := range logs {
		if l.Removed || l.Address != token || len(l.Topics) != 3 || l.Topics[0] != transferTopic {
			continue
		}
		v, err := eth.Uint(l.Data, 0)
		if err == nil && l.Topics[1] == word(from) && l.Topics[2] == word(to) && v.IsUint64() && v.Uint64() == value {
			return true
		}
	}
	return false
}

// Spend is one Transfer out of a reconciled wallet.
type Spend struct {
	From, To eth.Address
	Value    *big.Int
	Tx       [32]byte
	Block    uint64
	Nonce    *eth.Bytes32 // the EIP-3009 nonce its transaction used, if any
}

// Unauthorized lists every confirmed Transfer out of wallets that no known
// authorization accounts for: a plain transfer (the Signer signs none), an
// authorization the Signer never signed, or one paid to another recipient
// or amount than signed. Zero is "no unauthorized new spend" (AE1, P2).
func (t Token) Unauthorized(ctx context.Context, wallets []eth.Address, known []Authorization) ([]Spend, error) {
	at, err := t.confirmed(ctx)
	if err != nil {
		return nil, err
	}
	signed := map[[2][32]byte]Authorization{}
	for _, a := range known {
		signed[[2][32]byte{word(a.From), a.Nonce}] = a
	}
	var out []Spend
	for _, w := range wallets {
		logs, err := t.RPC.FilterLogs(ctx, t.Address, [][32]byte{transferTopic, word(w)}, t.FromBlock, at)
		if err != nil {
			return nil, err
		}
		for _, l := range logs {
			if l.Removed || len(l.Topics) != 3 {
				continue
			}
			to, err1 := eth.WordAddress(l.Topics[2][:])
			v, err2 := eth.Uint(l.Data, 0)
			if err := errors.Join(err1, err2); err != nil {
				return nil, fmt.Errorf("reconcile: a Transfer log: %w", err)
			}
			s := Spend{From: w, To: to, Value: v, Tx: l.TxHash, Block: l.BlockNumber}
			rc, err := t.RPC.Receipt(ctx, l.TxHash[:])
			if err != nil {
				return nil, err
			}
			ok := false
			for _, u := range rc.Logs {
				if u.Removed || u.Address != t.Address || len(u.Topics) != 3 || u.Topics[0] != usedTopic || u.Topics[1] != word(w) {
					continue
				}
				n := eth.Bytes32(u.Topics[2])
				s.Nonce = &n
				if a, found := signed[[2][32]byte{word(w), n}]; found && a.To == to && v.IsUint64() && v.Uint64() == a.Value {
					ok = true
				}
			}
			if !ok {
				out = append(out, s)
			}
		}
	}
	return out, nil
}
