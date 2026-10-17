package budget

import (
	"context"
	"encoding/hex"
	"encoding/json"
	"math/big"
	"strconv"
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
	if err != nil || po.PaymentBinding.ChainID != strconv.FormatUint(v.ChainID, 10) {
		return signer.Refusal("RESERVATION_MISMATCH")
	}
	// Its own node must be on the configured chain: the same contract
	// address can exist on another.
	if id, err := v.RPC.ChainID(ctx); err != nil || !id.IsUint64() || id.Uint64() != v.ChainID {
		return signer.Refusal("RESERVATION_UNAVAILABLE")
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
	ih, err := IntentBytes(intent)
	if err != nil || !spentFor(r.Logs, v.Contract, TaskField(v.ChainID, v.Contract, po.Intent.TaskID), ih, amount) {
		return signer.Refusal("RESERVATION_MISMATCH")
	}
	return nil
}

// spentFor: one of logs is contract's own Spent of amount for task and
// intent. (The intent hash covers the task and the amount too: their
// checks are a second, independent one.)
func spentFor(logs []eth.Log, contract eth.Address, task *big.Int, intent eth.Bytes32, amount uint64) bool {
	for _, l := range logs {
		if l.Removed || l.Address != contract || len(l.Topics) != 2 || l.Topics[0] != spentTopic ||
			new(big.Int).SetBytes(l.Topics[1][:]).Cmp(task) != 0 {
			continue
		}
		w0, err0 := eth.Word(l.Data, 0)
		w1, err1 := eth.Uint(l.Data, 1)
		if err0 == nil && err1 == nil && hex.EncodeToString(w0) == hex.EncodeToString(intent[:]) && w1.IsUint64() && w1.Uint64() == amount {
			return true
		}
	}
	return false
}
