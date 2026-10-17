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
// Confirmations 1 reads the head itself: no protection against a reorg of
// it (anvil, P2-D6); a public chain needs 2 or more, and a state read is
// only as final as that depth.
type Token struct {
	RPC           *eth.Client
	Address       eth.Address
	Confirmations uint64 // at least 1
	FromBlock     uint64 // no payment of interest before it: the token's deployment, say
}

// logRange is the most blocks one eth_getLogs asks for (providers cap it).
const logRange = 2000

// logs is FilterLogs from FromBlock to `to`, in ranges a provider allows,
// without removed logs. ponytail: a full rescan per call; a cursor if the
// history grows long.
func (t Token) logs(ctx context.Context, topics [][32]byte, to uint64) ([]eth.Log, error) {
	var out []eth.Log
	for from := t.FromBlock; from <= to; from += logRange {
		ls, err := t.RPC.FilterLogs(ctx, t.Address, topics, from, min(from+logRange-1, to))
		if err != nil {
			return nil, err
		}
		for _, l := range ls {
			if !l.Removed {
				out = append(out, l)
			}
		}
	}
	return out, nil
}

// receipt is a logged transaction's receipt, mined where its log says and
// at or below the confirmed block: anything else is a node behind another,
// or a reorg between the reads (an error, asked again later).
func (t Token) receipt(ctx context.Context, l eth.Log, at uint64) (*eth.Receipt, error) {
	rc, err := t.RPC.Receipt(ctx, l.TxHash[:])
	if err != nil {
		return nil, err
	}
	if rc == nil || rc.BlockNumber != l.BlockNumber || rc.BlockNumber > at {
		return nil, errors.New("reconcile: a logged transaction's receipt is not where its log is: ask again")
	}
	return rc, nil
}

// pairedTransfer is the token's Transfer right after the AuthorizationUsed
// log u in rc (the token emits them back to back): the one transfer that
// authorization made. nil when there is none.
func pairedTransfer(rc *eth.Receipt, token eth.Address, u eth.Log) *eth.Log {
	for i := range rc.Logs {
		l := &rc.Logs[i]
		if l.LogIndex == u.LogIndex+1 && !l.Removed && l.Address == token && len(l.Topics) == 3 && l.Topics[0] == transferTopic {
			return l
		}
	}
	return nil
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

// Status is one authorization's state at confirmed depth. not_settled_expired
// (the state that lets a Host pay again) is only as true as a.ValidBefore:
// it must be the signed one.
func (t Token) Status(ctx context.Context, a Authorization) (Result, error) {
	if a.ValidBefore == 0 || a.From == (eth.Address{}) || a.Nonce == (eth.Bytes32{}) {
		return Result{}, errors.New("reconcile: an authorization needs its signed validBefore, payer and nonce")
	}
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
	live, err := t.logs(ctx, [][32]byte{usedTopic, word(a.From), a.Nonce}, at)
	if err != nil {
		return Result{}, err
	}
	if len(live) != 1 {
		return Result{State: "needs_review", Block: at}, nil // used, and no single event says by what (a cancellation, say)
	}
	rc, err := t.receipt(ctx, live[0], at)
	if err != nil {
		return Result{}, err
	}
	r := Result{State: "needs_review", Tx: live[0].TxHash, Block: live[0].BlockNumber}
	if l := pairedTransfer(rc, t.Address, live[0]); rc.Status == 1 && l != nil {
		v, err := eth.Uint(l.Data, 0)
		if err == nil && l.Topics[1] == word(a.From) && l.Topics[2] == word(a.To) && v.IsUint64() && v.Uint64() == a.Value {
			r.State = "settled"
		}
	}
	return r, nil
}

// accounted: the Transfer l out of w was made by an authorization signed
// for exactly it (the AuthorizationUsed right before it in rc, of w, whose
// signed recipient and amount are l's). It also returns that nonce, if any.
func accounted(rc *eth.Receipt, token, w eth.Address, l eth.Log, signed map[[2][32]byte]Authorization) (*eth.Bytes32, bool) {
	to, err1 := eth.WordAddress(l.Topics[2][:])
	v, err2 := eth.Uint(l.Data, 0)
	if errors.Join(err1, err2) != nil {
		return nil, false
	}
	for _, u := range rc.Logs {
		if u.LogIndex+1 != l.LogIndex || u.Removed || u.Address != token || len(u.Topics) != 3 || u.Topics[0] != usedTopic || u.Topics[1] != word(w) {
			continue
		}
		n := eth.Bytes32(u.Topics[2])
		a, found := signed[[2][32]byte{word(w), n}]
		return &n, found && a.To == to && v.IsUint64() && v.Uint64() == a.Value
	}
	return nil, false
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
// or amount than signed. An authorization accounts only for the Transfer
// it made (the one right after its AuthorizationUsed). Zero is "no
// unauthorized new token transfer" (AE1, P2); approvals and permits, which
// move nothing themselves, are not looked at.
func (t Token) Unauthorized(ctx context.Context, wallets []eth.Address, known []Authorization) ([]Spend, error) {
	at, err := t.confirmed(ctx)
	if err != nil {
		return nil, err
	}
	seen := map[eth.Address]bool{}
	signed := map[[2][32]byte]Authorization{}
	for _, a := range known {
		signed[[2][32]byte{word(a.From), a.Nonce}] = a
	}
	var out []Spend
	for _, w := range wallets {
		if w == (eth.Address{}) || seen[w] {
			continue // the zero address would match every Transfer
		}
		seen[w] = true
		logs, err := t.logs(ctx, [][32]byte{transferTopic, word(w)}, at)
		if err != nil {
			return nil, err
		}
		for _, l := range logs {
			if len(l.Topics) != 3 {
				continue
			}
			to, err1 := eth.WordAddress(l.Topics[2][:])
			v, err2 := eth.Uint(l.Data, 0)
			if err := errors.Join(err1, err2); err != nil {
				return nil, fmt.Errorf("reconcile: a Transfer log: %w", err)
			}
			s := Spend{From: w, To: to, Value: v, Tx: l.TxHash, Block: l.BlockNumber}
			rc, err := t.receipt(ctx, l, at)
			if err != nil {
				return nil, err
			}
			var ok bool
			if s.Nonce, ok = accounted(rc, t.Address, w, l, signed); !ok {
				out = append(out, s)
			}
		}
	}
	return out, nil
}
