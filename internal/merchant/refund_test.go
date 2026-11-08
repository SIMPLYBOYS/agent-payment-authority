package merchant

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/SIMPLYBOYS/agent-payment-authority/internal/eth"
	"github.com/SIMPLYBOYS/agent-payment-authority/internal/eth/ethtest"
)

// docs/commercial-refund.md: the shop's own settlement refunded to its payer, the whole amount, from the gas
// key, once: asked again, the same refund and nothing sent; anything else is refused and sends nothing; a
// refund written and not sent (a crash) is sent on the next ask, never a second one; a settlement after it
// takes the next nonce.
func TestRefund(t *testing.T) {
	s := newChainShop(t, 1)
	ctx := context.Background()
	gas := ethtest.Key(t, 4).Addr
	data, _ := eth.Call("mint(address,uint256)", gas, uint64(1_000_000)) // the local pay_to has no key: the gas key refunds
	ethtest.Mine(t, s.rpc, ethtest.Key(t, 0), &eth.Tx{To: &s.usdc, Gas: 200_000, Data: data})
	_, sr := s.get("kr-market", s.header("kr-market", s.auth(500000), nil))
	if !sr.Success {
		t.Fatalf("settle: %+v", sr)
	}
	n := s.gasNonce()
	for name, tx := range map[string]string{"not a hash": "0x12", "no 0x": strings.TrimPrefix(sr.Transaction, "0x"), "not the shop's": "0x" + strings.Repeat("ab", 32)} {
		if o := s.st.Refund(ctx, tx); o.Kind != "invalid" || s.gasNonce() != n {
			t.Errorf("%s: %+v", name, o)
		}
	}
	o := s.st.Refund(ctx, sr.Transaction)
	if o.Kind != "refunded" || o.Amount != "500000" || !strings.EqualFold(o.Payer, s.payer.Addr.Hex()) || len(o.Transaction) != 66 || o.Block == 0 || s.gasNonce() != n+1 {
		t.Fatalf("refund: %+v, gas nonce %d -> %d", o, n, s.gasNonce())
	}
	if s.balance(s.payer.Addr) != 5_000_000 || s.balance(gas) != 500_000 {
		t.Fatalf("balances: payer %d, gas key %d", s.balance(s.payer.Addr), s.balance(gas))
	}
	if again := s.st.Refund(ctx, strings.ToUpper(sr.Transaction[:2]) + sr.Transaction[2:]); again.Kind != "invalid" {
		t.Fatalf("0X: %+v", again) // the hash is 0x hex, as the shop wrote it
	}
	if again := s.st.Refund(ctx, sr.Transaction); again != o || s.gasNonce() != n+1 {
		t.Fatalf("again: %+v, gas nonce %d", again, s.gasNonce())
	}
	// A refund written, then a crash before it is sent: the next ask sends that one.
	_, sr2 := s.get("kr-market", s.header("kr-market", s.auth(500000), nil))
	if !sr2.Success || s.gasNonce() != n+2 {
		t.Fatalf("a settlement after the refund: %+v, gas nonce %d", sr2, s.gasNonce())
	}
	hook = func(stage string) error {
		if stage == "refund-written" {
			return errors.New("crash")
		}
		return nil
	}
	if o := s.st.Refund(ctx, sr2.Transaction); o.Kind != "pending" || s.gasNonce() != n+2 {
		t.Fatalf("crashed: %+v, gas nonce %d", o, s.gasNonce())
	}
	hook = nil
	if o := s.st.Refund(ctx, sr2.Transaction); o.Kind != "refunded" || o.Amount != "500000" || s.gasNonce() != n+3 {
		t.Fatalf("after the crash: %+v, gas nonce %d", o, s.gasNonce())
	}
	if s.balance(s.payer.Addr) != 5_000_000 {
		t.Fatalf("payer %d", s.balance(s.payer.Addr))
	}
}
