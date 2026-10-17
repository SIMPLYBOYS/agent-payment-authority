package budget

import (
	"context"
	"encoding/hex"
	"encoding/json"
	"math/big"
	"strings"

	"github.com/SIMPLYBOYS/agent-payment-authority/internal/eth"
	"github.com/SIMPLYBOYS/agent-payment-authority/internal/paycontract"
	"github.com/SIMPLYBOYS/agent-payment-authority/internal/signer"
)

// Evidence is what Reserve returns and the Host passes to the Signer as
// the order's reservation. The Signer trusts only TxHash: everything else
// is read again from its own node.
type Evidence struct {
	ChainID     uint64 `json:"chain_id"`
	Contract    string `json:"contract"`
	TaskID      string `json:"task_id"`
	IntentHash  string `json:"intent_hash"`
	Amount      uint64 `json:"amount"`
	TxHash      string `json:"tx_hash"`
	BlockNumber uint64 `json:"block_number"`
}

// Verifier is the payment Signer's ReservationVerifier on a chain: the
// evidence's transaction succeeded, has Confirmations blocks, and emitted
// the contract's Spent for the order's task, intent and amount.
type Verifier struct {
	RPC           *eth.Client
	ChainID       uint64
	Contract      eth.Address
	Confirmations uint64
}

var _ signer.ReservationVerifier = (*Verifier)(nil)

func (v *Verifier) Verify(ctx context.Context, intent string, po *paycontract.PurchaseOrder, raw json.RawMessage) error {
	var ev Evidence
	if len(raw) == 0 || json.Unmarshal(raw, &ev) != nil {
		return signer.Refusal("RESERVATION_MISSING")
	}
	hash, err := hex.DecodeString(strings.TrimPrefix(ev.TxHash, "0x"))
	if err != nil || len(hash) != 32 {
		return signer.Refusal("RESERVATION_MISSING")
	}
	amount, err := po.PaymentBinding.Amount.Units()
	if err != nil {
		return signer.Refusal("RESERVATION_MISMATCH")
	}
	r, err := v.RPC.Receipt(ctx, hash)
	switch {
	case err != nil:
		return signer.Refusal("RESERVATION_UNAVAILABLE") // the node: not a judgement of the reservation
	case r == nil:
		return signer.Refusal("RESERVATION_PENDING")
	case r.Status != 1:
		return signer.Refusal("RESERVATION_FAILED")
	}
	latest, err := v.RPC.BlockNumber(ctx)
	if err != nil {
		return signer.Refusal("RESERVATION_UNAVAILABLE")
	}
	if latest < r.BlockNumber || latest-r.BlockNumber+1 < v.Confirmations {
		return signer.Refusal("RESERVATION_UNCONFIRMED")
	}
	task := TaskField(v.ChainID, v.Contract, po.Intent.TaskID)
	ih, err := IntentBytes(intent)
	if err != nil {
		return signer.Refusal("RESERVATION_MISMATCH")
	}
	for _, l := range r.Logs {
		if l.Removed || l.Address != v.Contract || len(l.Topics) != 2 || l.Topics[0] != spentTopic ||
			new(big.Int).SetBytes(l.Topics[1][:]).Cmp(task) != 0 {
			continue
		}
		w0, err0 := eth.Word(l.Data, 0)
		w1, err1 := eth.Uint(l.Data, 1)
		if err0 == nil && err1 == nil && hex.EncodeToString(w0) == hex.EncodeToString(ih[:]) && w1.IsUint64() && w1.Uint64() == amount {
			return nil
		}
	}
	return signer.Refusal("RESERVATION_MISMATCH")
}
