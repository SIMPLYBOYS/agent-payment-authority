package eth

import (
	"context"
	"errors"
	"math/big"
)

// ErrDropped: a written transaction can never succeed, judged at confirmed
// depth: from used its nonce otherwise, or the block's time is past its
// deadline with it unmined.
var ErrDropped = errors.New("dropped")

// Fees are a transaction's tip and fee cap.
type Fees struct{ Tip, Cap *big.Int }

// SignCall signs a call of data to `to` from key at nonce, with the
// current fees (the tip, and a cap of twice the base fee plus it). With
// replace, it replaces a transaction stuck at that nonce in a pool: both
// at least twice the current ones and above replace's by more than an
// eighth (a pool wants a bump on each, typically 10%). It returns the fees
// it used.
func (c *Client) SignCall(ctx context.Context, chainID uint64, key *Key, to Address, data []byte, nonce, gas uint64, replace *Fees) (raw, hash []byte, used Fees, err error) {
	h, err := c.LatestHeader(ctx)
	if err != nil {
		return nil, nil, used, err
	}
	tip, err := c.MaxPriorityFee(ctx)
	if err != nil {
		return nil, nil, used, err
	}
	fee := new(big.Int).Add(new(big.Int).Mul(h.BaseFee, big.NewInt(2)), tip)
	if replace != nil {
		bump := func(now, prior *big.Int) *big.Int {
			v := new(big.Int).Mul(now, big.NewInt(2))
			if prior != nil {
				p := new(big.Int).Add(prior, new(big.Int).Rsh(prior, 3))
				p.Add(p, big.NewInt(1))
				if p.Cmp(v) > 0 {
					v = p
				}
			}
			return v
		}
		tip, fee = bump(tip, replace.Tip), bump(fee, replace.Cap)
		if fee.Cmp(tip) < 0 {
			fee.Set(tip)
		}
	}
	tx := &Tx{ChainID: new(big.Int).SetUint64(chainID), Nonce: nonce, MaxPriorityFeePerGas: tip, MaxFeePerGas: fee, Gas: gas, To: &to, Value: new(big.Int), Data: data}
	raw, hash, err = tx.Sign(key)
	return raw, hash, Fees{Tip: tip, Cap: fee}, err
}

// Check looks once at a written transaction at confirmed depth (the
// newest block with `confirmations`, at least 1): its receipt there, or
// ErrDropped when it can never succeed there (from used its nonce
// otherwise, or the block's time is past deadline, 0 for none, with it
// unmined: the contract refuses it in any later block), or nil, nil: not
// decided. Judged at confirmed depth, a reorg of newer blocks never fails
// a transaction that is then mined.
func (c *Client) Check(ctx context.Context, hash []byte, from Address, nonce, deadline, confirmations uint64) (*Receipt, error) {
	latest, err := c.BlockNumber(ctx)
	if err != nil {
		return nil, err
	}
	if latest+1 < confirmations {
		return nil, nil
	}
	confirmed := latest + 1 - max(confirmations, 1)
	rc, err := c.Receipt(ctx, hash)
	if err != nil {
		return nil, err
	}
	if rc != nil {
		if rc.BlockNumber <= confirmed {
			return rc, nil
		}
		return nil, nil
	}
	used, err1 := c.NonceAt(ctx, from, confirmed)
	h, err2 := c.HeaderAt(ctx, confirmed)
	if err := errors.Join(err1, err2); err != nil {
		return nil, err
	}
	if used > nonce || deadline > 0 && h.Timestamp > deadline {
		// Asked again after: mined in between is not dropped.
		if again, err := c.Receipt(ctx, hash); err == nil && again == nil {
			return nil, ErrDropped
		}
	}
	return nil, nil
}
