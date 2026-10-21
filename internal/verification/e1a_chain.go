package verification

// E4 and E8 (docs/commercial-e1a.md 4.5, E1a-D5 A): the checker reads the
// chain itself, read only. The chain is not the system under test; the
// pack's own receipts are never taken for it.

import (
	"bytes"
	"context"
	"encoding/hex"
	"encoding/json"
	"errors"
	"math/big"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/SIMPLYBOYS/agent-payment-authority/internal/eth"
)

// Chain is what E4 and E8 read: *eth.Client.
type Chain interface {
	ChainID(context.Context) (*big.Int, error)
	BlockNumber(context.Context) (uint64, error)
	Receipt(context.Context, []byte) (*eth.Receipt, error)
	Call(context.Context, eth.Msg) ([]byte, error)
	FilterLogs(context.Context, eth.Address, [][32]byte, uint64, uint64) ([]eth.Log, error)
	Raw(context.Context, string, any, ...any) error
}

var (
	spendSel        = eth.Selector("spend(string,uint256,uint256,uint256,uint256,bytes32,uint256,bytes,bytes)")
	spentTopic      = eth.Topic("Spent(uint256,bytes32,uint256,uint256,uint256)")
	transferTopic   = eth.Topic("Transfer(address,address,uint256)")
	usedTopic       = eth.Topic("AuthorizationUsed(address,bytes32)")
	chainLogRange   = uint64(200) // a public node's eth_getLogs cap
	chainTimeout    = 2 * time.Minute
	confirmationsOf = map[string]uint64{"base_sepolia": 2, "local_anvil": 1}
)

// e1aOnChain is E4 and E8; without a chain both are INCONCLUSIVE.
func e1aOnChain(f *findings, m E1aManifest, runs map[string]*e1aRun, chain Chain, repo string) {
	if chain == nil {
		f.missing("E4", nil, "no --rpc: tx#1, tx#2 and the proof were not read from the chain")
		f.missing("E8", nil, "no --rpc: the wallets' transfers were not counted on the chain")
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), chainTimeout)
	defer cancel()
	s := m.Start
	id, err := chain.ChainID(ctx)
	if err != nil {
		f.missing("E4", nil, "the chain does not answer: %v", err)
		f.missing("E8", nil, "the chain does not answer: %v", err)
		return
	}
	if id.Uint64() != s.ChainID || confirmationsOf[s.Chain] == 0 {
		f.fail("E4", []string{"manifest.json"}, "the node is chain %d; the pack's start says %s %d", id, s.Chain, s.ChainID)
		return
	}
	latest, err := chain.BlockNumber(ctx)
	if err != nil {
		f.missing("E4", nil, "no latest block: %v", err)
		return
	}
	budget, err1 := eth.ParseAddress(s.BudgetContract)
	token, err2 := eth.ParseAddress(s.Token)
	if errors.Join(err1, err2) != nil {
		f.fail("E4", []string{"manifest.json"}, "the start names no budget contract or token")
		return
	}
	e1aVerifier(ctx, f, chain, s, budget, repo)
	settled := map[string]bool{} // tx#2 hashes, lower case
	for _, run := range runs {
		orders := map[string]int{}
		for i, o := range run.orders {
			orders[o.Intent.PurchaseOrderID] = i
		}
		pay := map[string]*[2]string{} // purchase -> tx#1, tx#2
		for _, e := range run.events {
			if e.Payload.Payment == nil || e.Scope == nil {
				continue
			}
			p := pay[e.Scope.PurchaseOrderID]
			if p == nil {
				p = &[2]string{}
				pay[e.Scope.PurchaseOrderID] = p
			}
			switch e.EventType {
			case "BUDGET_RESERVED":
				p[0] = e.Payload.Payment.TxHash
			case "PAYMENT_SETTLED":
				p[1] = e.Payload.Payment.TxHash
			}
		}
		for po, p := range pay {
			i, ok := orders[po]
			if !ok {
				continue // E2 says so
			}
			o := run.orders[i]
			intent, _ := o.IntentHash()
			amount := new(big.Int)
			amount.SetString(string(o.PaymentBinding.Amount), 10)
			if p[0] != "" && strings.EqualFold(p[0], p[1]) {
				f.fail("E4", nil, "%s: tx#1 and tx#2 are the same transaction", po)
				continue
			}
			if p[0] != "" {
				e1aReservation(ctx, f, chain, latest, s, budget, p[0], intent, amount)
			}
			if p[1] != "" {
				settled[strings.ToLower(p[1])] = true
				e1aSettlement(ctx, f, chain, latest, s, token, p[1], o.PaymentBinding.From, o.PaymentBinding.PayTo, o.PaymentBinding.Nonce, amount)
			}
		}
	}
	e1aTransfers(ctx, f, chain, s, token, latest, settled)
}

func txBytes(h string) ([]byte, bool) {
	b, err := hex.DecodeString(strings.TrimPrefix(h, "0x"))
	return b, err == nil && len(b) == 32
}

// confirmed is a receipt of a successful transaction deep enough.
func confirmed(ctx context.Context, f *findings, chain Chain, latest uint64, s E1aStart, claim, h string) *eth.Receipt {
	b, ok := txBytes(h)
	if !ok {
		f.fail(claim, nil, "%s is not a transaction hash", h)
		return nil
	}
	r, err := chain.Receipt(ctx, b)
	switch {
	case err != nil:
		f.missing(claim, nil, "%s: no receipt: %v", h, err)
		return nil
	case r == nil:
		f.fail(claim, nil, "%s is not on the chain", h)
		return nil
	case r.Status != 1:
		f.fail(claim, nil, "%s reverted", h)
		return nil
	case latest < r.BlockNumber || latest-r.BlockNumber+1 < confirmationsOf[s.Chain]:
		f.missing(claim, nil, "%s is not %d blocks deep yet", h, confirmationsOf[s.Chain])
		return nil
	}
	return r
}

// e1aReservation: tx#1 is spend on the budget contract for this intent and
// amount, its Spent log says so, and its proof verifies again now.
func e1aReservation(ctx context.Context, f *findings, chain Chain, latest uint64, s E1aStart, budget eth.Address, h, intent string, amount *big.Int) {
	r := confirmed(ctx, f, chain, latest, s, "E4", h)
	if r == nil {
		return
	}
	var tx struct {
		To    string `json:"to"`
		Input string `json:"input"`
	}
	if err := chain.Raw(ctx, "eth_getTransactionByHash", &tx, h); err != nil {
		f.missing("E4", nil, "%s: the transaction cannot be read: %v", h, err)
		return
	}
	in, err := hex.DecodeString(strings.TrimPrefix(tx.Input, "0x"))
	if err != nil || !strings.EqualFold(tx.To, budget.Hex()) || len(in) < 4+9*32 || !bytes.Equal(in[:4], spendSel) {
		f.fail("E4", nil, "tx#1 %s is not spend on the budget contract %s", h, budget.Hex())
		return
	}
	args := in[4:]
	w := func(i int) []byte { return args[32*i : 32*i+32] }
	dyn := func(i int) []byte { // a dynamic argument's bytes
		off := new(big.Int).SetBytes(w(i)).Uint64()
		if off+32 > uint64(len(args)) {
			return nil
		}
		n := new(big.Int).SetBytes(args[off : off+32]).Uint64()
		if off+32+n > uint64(len(args)) {
			return nil
		}
		return args[off+32 : off+32+n]
	}
	want, _ := hex.DecodeString(strings.TrimPrefix(intent, "sha256:"))
	if !bytes.Equal(w(5), want) || new(big.Int).SetBytes(w(2)).Cmp(amount) != 0 {
		f.fail("E4", nil, "tx#1 %s spends for intent 0x%x, amount %s; the purchase is %s, %s", h, w(5), new(big.Int).SetBytes(w(2)), intent, amount)
		return
	}
	spent := false
	for _, l := range r.Logs {
		if l.Address == budget && len(l.Topics) == 2 && l.Topics[0] == spentTopic && len(l.Data) >= 64 && bytes.Equal(l.Data[:32], want) &&
			new(big.Int).SetBytes(l.Data[32:64]).Cmp(amount) == 0 {
			spent = true
		}
	}
	if !spent {
		f.fail("E4", nil, "tx#1 %s has no Spent log of this intent and amount", h)
	}
	// The proof again: verifyProof(proof, [task, C_old, amount, C_new, nullifier, intent_hi, intent_lo]).
	taskID := dyn(0)
	field, err := chain.Call(ctx, eth.Msg{To: budget, Data: mustCall("taskField(string)", string(taskID))})
	verifier, err2 := chain.Call(ctx, eth.Msg{To: budget, Data: eth.Selector("verifier()")})
	if errors.Join(err, err2) != nil || len(field) != 32 || len(verifier) != 32 {
		f.missing("E4", nil, "tx#1 %s: the task field or verifier cannot be read: %v", h, errors.Join(err, err2))
		return
	}
	hi, lo := new(big.Int).SetBytes(want[:16]), new(big.Int).SetBytes(want[16:])
	data, err := eth.Call("verifyProof(bytes,uint256[7])", dyn(7), new(big.Int).SetBytes(field), new(big.Int).SetBytes(w(1)), new(big.Int).SetBytes(w(2)),
		new(big.Int).SetBytes(w(3)), new(big.Int).SetBytes(w(4)), hi, lo)
	var to eth.Address
	copy(to[:], verifier[12:])
	if err == nil {
		_, err = chain.Call(ctx, eth.Msg{To: to, Data: data})
	}
	if err != nil {
		f.fail("E4", nil, "tx#1 %s: its proof does not verify again: %v", h, err)
	}
}

func mustCall(sig string, args ...any) []byte {
	b, _ := eth.Call(sig, args...)
	return b
}

// e1aVerifier: the budget contract's verifier is the start's, and its code
// is what this repository's Verifier.sol compiles to.
func e1aVerifier(ctx context.Context, f *findings, chain Chain, s E1aStart, budget eth.Address, repo string) {
	out, err := chain.Call(ctx, eth.Msg{To: budget, Data: eth.Selector("verifier()")})
	if err != nil || len(out) != 32 {
		f.missing("E4", nil, "the budget contract's verifier cannot be read: %v", err)
		return
	}
	v, _ := eth.WordAddress(out)
	if !strings.EqualFold(v.Hex(), s.Verifier) {
		f.fail("E4", []string{"manifest.json"}, "the budget contract's verifier is %s, not the start's %s", v.Hex(), s.Verifier)
		return
	}
	var code string
	if err := chain.Raw(ctx, "eth_getCode", &code, v.Hex(), "latest"); err != nil {
		f.missing("E4", nil, "the verifier's code cannot be read: %v", err)
		return
	}
	var art struct {
		DeployedBytecode struct {
			Object string `json:"object"`
		} `json:"deployedBytecode"`
	}
	raw, err := os.ReadFile(filepath.Join(repo, "var/contracts/out/Verifier.sol/Verifier.json"))
	if err != nil || json.Unmarshal(raw, &art) != nil || art.DeployedBytecode.Object == "" {
		f.missing("E4", nil, "no compiled Verifier.sol to compare with (forge build in contracts/): %v", err)
		return
	}
	if !strings.EqualFold(strings.TrimPrefix(code, "0x"), strings.TrimPrefix(art.DeployedBytecode.Object, "0x")) {
		f.fail("E4", nil, "the verifier's code on the chain is not this repository's Verifier.sol")
	}
}

// e1aSettlement: tx#2 moved the purchase's amount from its payer to its
// payee by the purchase's own EIP-3009 authorization (its nonce).
func e1aSettlement(ctx context.Context, f *findings, chain Chain, latest uint64, s E1aStart, token eth.Address, h, from, to, nonce string, amount *big.Int) {
	r := confirmed(ctx, f, chain, latest, s, "E4", h)
	if r == nil {
		return
	}
	pf, err1 := eth.ParseAddress(strings.ToLower(from))
	pt, err2 := eth.ParseAddress(strings.ToLower(to))
	n, ok := txBytes(nonce)
	if errors.Join(err1, err2) != nil || !ok {
		f.fail("E4", nil, "tx#2 %s: the purchase's payer, payee or nonce is not one", h)
		return
	}
	var moved, used bool
	for _, l := range r.Logs {
		switch {
		case l.Address == token && len(l.Topics) == 3 && l.Topics[0] == transferTopic && topicIs(l.Topics[1], pf) && topicIs(l.Topics[2], pt) &&
			new(big.Int).SetBytes(l.Data).Cmp(amount) == 0:
			moved = true
		case l.Address == token && len(l.Topics) == 3 && l.Topics[0] == usedTopic && topicIs(l.Topics[1], pf) && bytes.Equal(l.Topics[2][:], n):
			used = true
		}
	}
	if !moved || !used {
		f.fail("E4", nil, "tx#2 %s: transfer of %s from %s to %s %v, its authorization (nonce %s) used %v", h, amount, from, to, moved, nonce, used)
	}
}

func topicIs(t [32]byte, a eth.Address) bool { return bytes.Equal(t[12:], a[:]) }

// e1aTransfers is E8: every transfer out of the start's wallets, from its
// first block to now, counted on the chain in ranges, is one of the pack's
// settlements; anything else is unauthorized.
func e1aTransfers(ctx context.Context, f *findings, chain Chain, s E1aStart, token eth.Address, latest uint64, settled map[string]bool) {
	if len(s.Wallets) == 0 || s.FromBlock == 0 || s.FromBlock > latest {
		f.missing("E8", []string{"manifest.json"}, "the start names no wallets or first block")
		return
	}
	for _, w := range s.Wallets {
		a, err := eth.ParseAddress(strings.ToLower(w))
		if err != nil {
			f.fail("E8", []string{"manifest.json"}, "wallet %s is not an address", w)
			continue
		}
		var topic [32]byte
		copy(topic[12:], a[:])
		for from := s.FromBlock; from <= latest; from += chainLogRange {
			to := min(from+chainLogRange-1, latest)
			logs, err := chain.FilterLogs(ctx, token, [][32]byte{transferTopic, topic}, from, to)
			if err != nil {
				f.missing("E8", nil, "blocks %d-%d cannot be read: %v", from, to, err)
				return
			}
			for _, l := range logs {
				if !settled["0x"+hex.EncodeToString(l.TxHash[:])] {
					f.fail("E8", nil, "a transfer out of %s in tx 0x%x (block %d) is none of the pack's payments", w, l.TxHash, l.BlockNumber)
				}
			}
		}
	}
}
