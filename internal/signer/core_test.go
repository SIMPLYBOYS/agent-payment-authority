package signer

// Tests of the Signer core (docs/commercial-p1b-design.md 4, 5, 8). Keys are
// public test material (keccak256 of a fixed string, the first one the
// EIP-712 specification's own); delegations, orders and reservations are
// synthetic; nothing is sent anywhere.

import (
	"bytes"
	"context"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"math/big"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/SIMPLYBOYS/agent-payment-authority/internal/paycontract"
	"github.com/SIMPLYBOYS/agent-payment-authority/internal/signer/signertest"
	"github.com/decred/dcrd/dcrec/secp256k1/v4"
	"github.com/decred/dcrd/dcrec/secp256k1/v4/ecdsa"
)

const (
	t0       = 1790726450
	asset    = signertest.Asset
	merchant = signertest.Merchant
	chain    = signertest.Chain
)

var (
	keyA = secp256k1.PrivKeyFromBytes(keccak([]byte("cow")))
	keyB = secp256k1.PrivKeyFromBytes(keccak([]byte("apa-signer test wallet b")))
)

var bg = context.Background()

// reserved is the test-only reservation verifier: every order is reserved
// unless refuse names a code.
type reserved struct{ refuse string }

func (r reserved) Verify(context.Context, string, *paycontract.PurchaseOrder, json.RawMessage) error {
	if r.refuse != "" {
		return Refusal(r.refuse)
	}
	return nil
}

type env struct {
	t     *testing.T
	dir   string
	now   atomic.Uint64
	cfg   *Config
	keys  map[string]*secp256k1.PrivateKey
	res   ReservationVerifier
	cores []*Core
	quote string // the EIP-712 version the next orders' quotes name

}

func newEnv(t *testing.T) *env {
	t.Helper()
	e := &env{t: t, dir: t.TempDir(), res: reserved{}, quote: "2"}
	e.now.Store(t0)
	a, b := Address(keyA.PubKey()), Address(keyB.PubKey())
	raw := signertest.Config("/unused", 0, t0, a, b)
	cfg, err := ParseConfig(raw)
	if err != nil {
		t.Fatal(err)
	}
	e.cfg, e.keys = cfg, map[string]*secp256k1.PrivateKey{a: keyA, b: keyB}
	t.Cleanup(func() {
		for _, c := range e.cores {
			c.Close()
		}
	})
	return e
}

// core opens (or reopens: a restart) the Core on the env's DB.
func (e *env) core() *Core {
	e.t.Helper()
	c, err := NewCore(bg, e.cfg, e.keys, filepath.Join(e.dir, "signer.db"), e.now.Load, e.res)
	if err != nil {
		e.t.Fatal(err)
	}
	e.cores = append(e.cores, c)
	return c
}

type spec struct {
	consumer, agent, wallet string
	key                     *secp256k1.PrivateKey
	payTo, amount           string
	n                       int    // purchase and nonce number
	window                  uint64 // valid_before - now
}

func a(n int) spec {
	return spec{"consumer-a", "agent-a", "wallet-a", keyA, merchant, "500000", n, 60}
}

// order builds a valid apa.pay.v1 purchase as the Host's tool bridge does,
// with the quote matching the binding.
func (e *env) order(s spec) json.RawMessage {
	raw, _ := json.Marshal(e.po(s))
	return raw
}

func (e *env) po(s spec) paycontract.PurchaseOrder {
	return signertest.Order{Consumer: s.consumer, Agent: s.agent, Wallet: s.wallet, From: Address(s.key.PubKey()), PayTo: s.payTo, Amount: s.amount,
		N: s.n, Now: e.now.Load(), Window: s.window, DomainVersion: e.quote}.PO()
}

func delegation(id string) Authorization {
	return Authorization{Method: "delegation", DelegationID: id, DelegationVersion: 1}
}

func (e *env) req(id string, s spec) SignRequest {
	return SignRequest{RequestID: id, Authorization: delegation("dlg-" + strings.TrimPrefix(s.consumer, "consumer-")), Order: e.order(s)}
}

func count(t *testing.T, c *Core, where string) int {
	t.Helper()
	var n int
	if err := c.db.QueryRow(`SELECT count(*) FROM attempts WHERE ` + where).Scan(&n); err != nil {
		t.Fatal(err)
	}
	return n
}

func code(err error) string {
	var r Refusal
	if errors.As(err, &r) {
		return string(r)
	}
	if err != nil {
		return "ERR:" + err.Error()
	}
	return ""
}

// recovered is the address that signed digest, from the 65-byte r‖s‖v.
func recovered(t *testing.T, sigHex, digestHex string) string {
	t.Helper()
	sig, _ := hex.DecodeString(strings.TrimPrefix(sigHex, "0x"))
	digest, _ := hex.DecodeString(strings.TrimPrefix(digestHex, "0x"))
	pub, _, err := ecdsa.RecoverCompact(append([]byte{sig[64]}, sig[:64]...), digest)
	if err != nil {
		t.Fatal(err)
	}
	return Address(pub)
}

func TestSignOne(t *testing.T) {
	e := newEnv(t)
	c := e.core()
	r, err := c.Sign(bg, e.req("request-0000000001", a(1)))
	if err != nil || r.State != "signed" || r.Authorization == nil {
		t.Fatalf("%+v %v", r, err)
	}
	m := r.Authorization.Transfer
	if m.From != Address(keyA.PubKey()) || m.To != merchant || m.Value != "500000" || m.Nonce != fmt.Sprintf("0x%064x", 1) || m.ValidBefore != t0+60 {
		t.Fatalf("message %+v", m)
	}
	sig, _ := hex.DecodeString(strings.TrimPrefix(r.Authorization.Signature, "0x"))
	if len(sig) != 65 || (sig[64] != 27 && sig[64] != 28) || recovered(t, r.Authorization.Signature, r.TypedDigest) != Address(keyA.PubKey()) {
		t.Fatalf("signature %x does not recover to wallet-a", sig)
	}
	// low-S: s <= n/2 (EIP-2), n the secp256k1 group order
	if halfN, _ := new(big.Int).SetString("7fffffffffffffffffffffffffffffff5d576e7357a4501ddfe92f46681b20a0", 16); new(big.Int).SetBytes(sig[32:64]).Cmp(halfN) > 0 {
		t.Fatalf("high s %x", sig[32:64])
	}
	sep, _ := DomainSeparator(Domain{Name: "USDC", Version: "2", ChainID: chain, VerifyingContract: asset})
	sh, _ := TransferHash(m)
	if "0x"+hex.EncodeToString(TypedDigest(sep, sh)) != r.TypedDigest {
		t.Fatal("the digest is not the one rebuilt from the message and the registered domain")
	}
}

// Every refusal leaves nothing reserved: no attempt, no spend.
func TestRefusalsConsumeNothing(t *testing.T) {
	e := newEnv(t)
	c := e.core()
	b := spec{"consumer-b", "agent-b", "wallet-b", keyB, merchant, "500000", 2, 60}
	cases := map[string]struct {
		r    SignRequest
		want string
	}{
		"simulated":             {SignRequest{RequestID: "request-0000000001", Authorization: Authorization{Method: "simulated"}, Order: e.order(a(1))}, "GRANT_METHOD_NOT_ACCEPTED"},
		"webauthn, no approval": {SignRequest{RequestID: "request-0000000002", Authorization: Authorization{Method: "webauthn"}, Order: e.order(a(1))}, "APPROVAL_UNKNOWN"},
		"unknown delegation":    {SignRequest{RequestID: "request-0000000003", Authorization: delegation("dlg-z"), Order: e.order(a(1))}, "DELEGATION_UNKNOWN"},
		"another version":       {SignRequest{RequestID: "request-0000000004", Authorization: Authorization{Method: "delegation", DelegationID: "dlg-a", DelegationVersion: 2}, Order: e.order(a(1))}, "DELEGATION_VERSION"},
		"A's delegation for B":  {SignRequest{RequestID: "request-0000000005", Authorization: delegation("dlg-a"), Order: e.order(b)}, "CONSUMER_MISMATCH"},
		"a confirmation here":   {SignRequest{RequestID: "request-0000000020", Authorization: Authorization{Method: "delegation", DelegationID: "dlg-a", DelegationVersion: 1, DelegationConfirmationID: "hap-1"}, Order: e.order(a(1))}, "BAD_REQUEST"}, // gap 4: a payment-only Signer keeps none
		"B's key in A's order":  {e.req("request-0000000006", spec{"consumer-a", "agent-a", "wallet-a", keyB, merchant, "500000", 3, 60}), "CONSUMER_MISMATCH"},
		"another merchant":      {e.req("request-0000000007", spec{"consumer-a", "agent-a", "wallet-a", keyA, "0x9999999999999999999999999999999999999999", "500000", 4, 60}), "MERCHANT_NOT_ALLOWED"},
		"over per tx":           {e.req("request-0000000008", spec{"consumer-a", "agent-a", "wallet-a", keyA, merchant, "600001", 5, 60}), "OVER_PER_TX"},
		"window too long":       {e.req("request-0000000009", spec{"consumer-a", "agent-a", "wallet-a", keyA, merchant, "500000", 6, 601}), "WINDOW_INVALID"},
		"not an order":          {SignRequest{RequestID: "request-0000000010", Authorization: delegation("dlg-a"), Order: json.RawMessage(`{"version":"apa.m1.v2"}`)}, "ORDER_INVALID"},
	}
	for name, cs := range cases {
		if _, err := c.Sign(bg, cs.r); code(err) != cs.want {
			t.Errorf("%s: %v, want %s", name, err, cs.want)
		}
	}
	// The order's quote must name the registered EIP-712 domain.
	e.quote = "9"
	if _, err := c.Sign(bg, e.req("request-0000000011", a(7))); code(err) != "ASSET_NOT_ALLOWED" {
		t.Errorf("another domain version: %v", err)
	}
	e.quote = "2"
	e.now.Store(t0 + 3600) // the delegation's valid_until
	if _, err := c.Sign(bg, e.req("request-0000000012", a(8))); code(err) != "DELEGATION_EXPIRED" {
		t.Errorf("expired delegation: %v", err)
	}
	if n := count(t, c, "1"); n != 0 {
		t.Fatalf("%d attempts after refusals", n)
	}
}

// Each binding field changed alone, without the quote, is refused: the
// Signer signs only a binding that is the quote's.
func TestBindingFields(t *testing.T) {
	e := newEnv(t)
	c := e.core()
	for name, cs := range map[string]struct {
		edit func(*paycontract.PurchaseOrder)
		want string
	}{
		"chain": {func(o *paycontract.PurchaseOrder) { o.PaymentBinding.ChainID = "8453" }, "ORDER_INVALID"},
		"asset": {func(o *paycontract.PurchaseOrder) {
			o.PaymentBinding.AssetContract = "0x5555555555555555555555555555555555555555"
		}, "ORDER_INVALID"},
		"payee": {func(o *paycontract.PurchaseOrder) {
			o.PaymentBinding.PayTo = "0x9999999999999999999999999999999999999999"
		}, "ORDER_INVALID"},
		"amount":   {func(o *paycontract.PurchaseOrder) { o.PaymentBinding.Amount = "500001" }, "ORDER_INVALID"},
		"method":   {func(o *paycontract.PurchaseOrder) { o.PaymentBinding.Method = "permit2" }, "ORDER_INVALID"},
		"payer":    {func(o *paycontract.PurchaseOrder) { o.PaymentBinding.From = Address(keyB.PubKey()) }, "CONSUMER_MISMATCH"},
		"wallet":   {func(o *paycontract.PurchaseOrder) { o.Intent.WalletID = "wallet-b" }, "CONSUMER_MISMATCH"},
		"consumer": {func(o *paycontract.PurchaseOrder) { o.Intent.ConsumerID = "consumer-b" }, "CONSUMER_MISMATCH"},
		"quote": {func(o *paycontract.PurchaseOrder) {
			o.Intent.PaymentRequirementHash = "sha256:" + strings.Repeat("0", 64)
		}, "ORDER_INVALID"},
	} {
		o := e.po(a(1))
		cs.edit(&o)
		raw, _ := json.Marshal(o)
		if _, err := c.Sign(bg, SignRequest{RequestID: "request-0000000001", Authorization: delegation("dlg-a"), Order: raw}); code(err) != cs.want {
			t.Errorf("%s: %v, want %s", name, err, cs.want)
		}
	}
	if n := count(t, c, "1"); n != 0 {
		t.Fatalf("%d attempts", n)
	}
}

// R3-a, before computing: the accept transaction takes its own time after
// the write lock; a deadline reached after the checks accepts nothing.
func TestAcceptDeadline(t *testing.T) {
	e := newEnv(t)
	var calls atomic.Int32
	clock := func() uint64 {
		if calls.Add(1) == 1 {
			return t0 // the checks
		}
		return t0 + 60 // the accept transaction: valid_before, the deadline
	}
	c, err := NewCore(bg, e.cfg, e.keys, filepath.Join(e.dir, "signer.db"), clock, e.res)
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	if _, err := c.Sign(bg, e.req("request-0000000001", a(1))); code(err) != "AUTHORIZATION_EXPIRED" || count(t, c, "1") != 0 {
		t.Fatalf("accepted at the deadline: %v", err)
	}
}

func TestProductionVerifierRefuses(t *testing.T) {
	e := newEnv(t)
	e.res = Unavailable{}
	c := e.core()
	if _, err := c.Sign(bg, e.req("request-0000000001", a(1))); code(err) != "RESERVATION_UNAVAILABLE" || count(t, c, "1") != 0 {
		t.Fatalf("%v", err)
	}
}

func TestResendConflictReplay(t *testing.T) {
	e := newEnv(t)
	c := e.core()
	first, err := c.Sign(bg, e.req("request-0000000001", a(1)))
	if err != nil {
		t.Fatal(err)
	}
	again, err := c.Sign(bg, e.req("request-0000000001", a(1)))
	if err != nil || again.AttemptID != first.AttemptID || again.Authorization.Signature != first.Authorization.Signature || count(t, c, "1") != 1 {
		t.Fatalf("resend: %+v %v", again, err)
	}
	other := a(2)
	if _, err := c.Sign(bg, e.req("request-0000000001", other)); code(err) != "REQUEST_ID_CONFLICT" {
		t.Fatalf("same ID, other content: %v", err)
	}
	if _, err := c.Sign(bg, e.req("request-0000000002", a(1))); code(err) != "INTENT_IN_USE" {
		t.Fatalf("same order, new ID: %v", err)
	}
	s := a(3)
	r := e.req("request-0000000003", s)
	r.Order = bytes.Replace(r.Order, []byte(fmt.Sprintf("0x%064x", 3)), []byte(fmt.Sprintf("0x%064x", 1)), 1) // reuse order 1's nonce
	if _, err := c.Sign(bg, r); code(err) != "NONCE_IN_USE" {
		t.Fatalf("reused nonce: %v", err)
	}
}

func TestTotalCap(t *testing.T) {
	e := newEnv(t)
	c := e.core()
	if _, err := c.Sign(bg, e.req("request-0000000001", a(1))); err != nil {
		t.Fatal(err)
	}
	// 500000 signed of 1000000: another 500000 fits, a third does not.
	if _, err := c.Sign(bg, e.req("request-0000000002", a(2))); err != nil {
		t.Fatal(err)
	}
	if _, err := c.Sign(bg, e.req("request-0000000003", a(3))); code(err) != "OVER_TOTAL_CAP" {
		t.Fatalf("over the cap: %v", err)
	}
	// A restart keeps the spend: it is derived from the saved states.
	c = e.core()
	if _, err := c.Sign(bg, e.req("request-0000000004", a(4))); code(err) != "OVER_TOTAL_CAP" {
		t.Fatalf("after a restart: %v", err)
	}
	// B's delegation is its own.
	if _, err := c.Sign(bg, SignRequest{RequestID: "request-0000000005", Authorization: delegation("dlg-b"),
		Order: e.order(spec{"consumer-b", "agent-b", "wallet-b", keyB, merchant, "500000", 5, 60})}); err != nil {
		t.Fatalf("B: %v", err)
	}
}

func TestConcurrentCap(t *testing.T) {
	e := newEnv(t)
	c := e.core()
	s1, s2 := a(1), a(2)
	s1.amount, s2.amount = "600000", "600000"
	var wg sync.WaitGroup
	errs := make([]error, 2)
	for i, s := range []spec{s1, s2} {
		wg.Go(func() { _, errs[i] = c.Sign(bg, e.req(fmt.Sprintf("request-000000000%d", i+1), s)) })
	}
	wg.Wait()
	ok := 0
	for _, err := range errs {
		if err == nil {
			ok++
		} else if code(err) != "OVER_TOTAL_CAP" {
			t.Fatalf("%v", err)
		}
	}
	if ok != 1 {
		t.Fatalf("both accepted over the cap: %v", errs)
	}
}

// crashAt makes the named stage fail once, as a crash there would.
func crashAt(stage string) func() {
	hook = func(s, _ string) error {
		if s == stage {
			hook = nil
			return errors.New("crash at " + stage)
		}
		return nil
	}
	return func() { hook = nil }
}

// The three interruption points (design 5.4): recovery reads the snapshot,
// signs with the same key and digest, and the bytes are those of an
// uninterrupted run.
func TestInterruptions(t *testing.T) {
	ref := newEnv(t)
	want, err := ref.core().Sign(bg, ref.req("request-0000000001", a(1)))
	if err != nil {
		t.Fatal(err)
	}
	for _, stage := range []string{"after-accept", "after-sign", "after-final"} {
		t.Run(stage, func(t *testing.T) {
			e := newEnv(t)
			c := e.core()
			defer crashAt(stage)()
			if _, err := c.Sign(bg, e.req("request-0000000001", a(1))); err == nil {
				t.Fatal("no crash")
			}
			c = e.core() // restart: recovery runs before serving
			r, err := c.Get(bg, "", "request-0000000001")
			if err != nil || r.State != "signed" || r.Authorization.Signature != want.Authorization.Signature {
				t.Fatalf("recovery used the request, not the snapshot, or signed differently: %+v %v", r, err)
			}
			again, err := c.Sign(bg, e.req("request-0000000001", a(1)))
			if err != nil || again.Authorization.Signature != want.Authorization.Signature || count(t, c, "1") != 1 {
				t.Fatalf("a second attempt for the same request: %+v %v", again, err)
			}
		})
	}
}

// R3-a: the final decision takes its time after the write lock; a deadline
// reached between signing and saving lets no signature out, == included.
func TestFinalDeadline(t *testing.T) {
	for _, c := range []struct {
		name string
		at   uint64 // the clock when the final decision runs, from the deadline
		want string
	}{{"reached before saving", 1, "ATTEMPT_EXPIRED"}, {"exactly at the deadline", 0, "ATTEMPT_EXPIRED"}} {
		t.Run(c.name, func(t *testing.T) {
			e := newEnv(t)
			core := e.core()
			hook = func(s, _ string) error {
				if s == "after-sign" {
					e.now.Store(t0 + 60 + c.at) // valid_before is t0+60, the deadline
				}
				return nil
			}
			defer func() { hook = nil }()
			r, err := core.Sign(bg, e.req("request-0000000001", a(1)))
			if code(err) != c.want || r.Authorization != nil || count(t, core, "state = 'expired_unsigned'") != 1 {
				t.Fatalf("signed after the deadline: %+v %v", r, err)
			}
		})
	}
	// A signed attempt whose deadline passes later is still answered as saved.
	e := newEnv(t)
	core := e.core()
	first, _ := core.Sign(bg, e.req("request-0000000001", a(1)))
	e.now.Store(t0 + 3600)
	r, err := core.Get(bg, first.AttemptID, "")
	if err != nil || r.State != "signed" || r.Authorization.Signature != first.Authorization.Signature {
		t.Fatalf("saved result refused after expiry: %+v %v", r, err)
	}
}

// R3-b: abandon takes no wallet lock, so it can land between signing and the
// final decision; the conditional updates decide.
func TestAbandonRaces(t *testing.T) {
	t.Run("abandon first", func(t *testing.T) {
		e := newEnv(t)
		c := e.core()
		var abandoned Reply
		var aerr error
		blocked := false
		hook = func(s, id string) error {
			if s == "after-sign" {
				done := make(chan struct{})
				go func() { abandoned, aerr = c.Abandon(bg, id); close(done) }()
				select {
				case <-done:
				case <-time.After(2 * time.Second): // bounded: a handshake, not a guess at timing
					blocked = true
				}
			}
			return nil
		}
		defer func() { hook = nil }()
		r, err := c.Sign(bg, e.req("request-0000000001", a(1)))
		if blocked {
			t.Fatal("abandon blocked by the signing lock")
		}
		if aerr != nil || abandoned.State != "abandoned" {
			t.Fatalf("abandon: %+v %v", abandoned, aerr)
		}
		if code(err) != "ATTEMPT_ABANDONED" || r.Authorization != nil {
			t.Fatalf("signature returned after abandon: %+v %v", r, err)
		}
	})
	t.Run("signed first", func(t *testing.T) {
		e := newEnv(t)
		c := e.core()
		var ab Reply
		var aerr error
		hook = func(s, id string) error {
			if s == "after-final" {
				ab, aerr = c.Abandon(bg, id)
			}
			return nil
		}
		defer func() { hook = nil }()
		r, err := c.Sign(bg, e.req("request-0000000001", a(1)))
		if err != nil || r.State != "signed" || code(aerr) != "ATTEMPT_NOT_ACCEPTED" || ab.State != "signed" {
			t.Fatalf("abandon overwrote signed: %+v %v / %+v %v", r, err, ab, aerr)
		}
	})
	t.Run("expired, then abandon", func(t *testing.T) {
		e := newEnv(t)
		c := e.core()
		release := crashAt("after-accept")
		c.Sign(bg, e.req("request-0000000001", a(1)))
		release()
		e.now.Store(t0 + 60)
		g, _ := c.Get(bg, "", "request-0000000001")
		ab, err := c.Abandon(bg, g.AttemptID)
		var spent int
		c.db.QueryRow(`SELECT coalesce(sum(amount), 0) FROM attempts WHERE state IN ('accepted', 'signed')`).Scan(&spent)
		if g.State != "expired_unsigned" || code(err) != "ATTEMPT_NOT_ACCEPTED" || ab.State != "expired_unsigned" || spent != 0 {
			t.Fatalf("abandon after expiry: %+v / %+v %v / spent %d", g, ab, err, spent)
		}
	})
}

// The wallet lock is a bounded wait: a second request for the same wallet
// while one is signing is refused SIGNER_BUSY, another wallet is not held.
func TestWalletBusy(t *testing.T) {
	e := newEnv(t)
	c := e.core()
	defer func(d time.Duration) { lockWait = d }(lockWait)
	lockWait = 50 * time.Millisecond
	var same, other error
	hook = func(s, _ string) error {
		if s == "after-sign" {
			hook = nil
			_, same = c.Sign(bg, e.req("request-0000000002", a(2)))
			_, other = c.Sign(bg, SignRequest{RequestID: "request-0000000003", Authorization: delegation("dlg-b"),
				Order: e.order(spec{"consumer-b", "agent-b", "wallet-b", keyB, merchant, "500000", 3, 60})})
		}
		return nil
	}
	defer func() { hook = nil }()
	if _, err := c.Sign(bg, e.req("request-0000000001", a(1))); err != nil {
		t.Fatal(err)
	}
	if code(same) != "SIGNER_BUSY" || other != nil {
		t.Fatalf("same wallet %v, other wallet %v", same, other)
	}
}

// R3, the delegation ends first: accepted, interrupted, and on restart the
// delegation's valid_until has passed though valid_before has not.
func TestDelegationEndsFirst(t *testing.T) {
	e := newEnv(t)
	e.cfg.Delegations[0].ValidUntil = t0 + 30 // valid_before is t0+60
	c := e.core()
	release := crashAt("after-accept")
	c.Sign(bg, e.req("request-0000000001", a(1)))
	release()
	e.now.Store(t0 + 30)
	c = e.core()
	if r, err := c.Get(bg, "", "request-0000000001"); err != nil || r.State != "expired_unsigned" || r.Authorization != nil || r.AuthDeadline != t0+30 {
		t.Fatalf("signed after the authorization expired: %+v %v", r, err)
	}
}

func TestKeyVersionMismatch(t *testing.T) {
	e := newEnv(t)
	c := e.core()
	release := crashAt("after-accept")
	c.Sign(bg, e.req("request-0000000001", a(1)))
	release()
	e.keys = map[string]*secp256k1.PrivateKey{Address(keyA.PubKey()): keyB, Address(keyB.PubKey()): keyB} // wallet-a's address now maps to another key
	c = e.core()
	if r, err := c.Sign(bg, e.req("request-0000000001", a(1))); code(err) != "KEY_VERSION_MISMATCH" || count(t, c, "state = 'accepted'") != 1 {
		t.Fatalf("signed with another key: %+v %v", r, err)
	}
}

// docs/commercial-velocity.md: the Signer's own rate limit, a backup to the Host's Policy: at most Max payments
// of one wallet within the window, the next refused VELOCITY_LIMIT with nothing reserved; another wallet's do
// not count; once the window has passed the wallet pays again.
func TestSignerVelocity(t *testing.T) {
	e := newEnv(t)
	e.cfg.Velocity = &Velocity{Max: 2, WindowSeconds: 600}
	c := e.core()
	small := func(n int) spec { s := a(n); s.amount = "100000"; return s }
	for i := 1; i <= 2; i++ {
		if r, err := c.Sign(bg, e.req(fmt.Sprintf("request-%010d", i), small(i))); err != nil || r.State != "signed" {
			t.Fatalf("payment %d: %+v %v", i, r, err)
		}
	}
	before := count(t, c, "1 = 1")
	if _, err := c.Sign(bg, e.req("request-0000000003", small(3))); code(err) != "VELOCITY_LIMIT" || count(t, c, "1 = 1") != before {
		t.Fatalf("the 3rd within the window: %v (attempts %d -> %d)", err, before, count(t, c, "1 = 1"))
	}
	b := spec{"consumer-b", "agent-b", "wallet-b", keyB, merchant, "100000", 4, 60}
	if r, err := c.Sign(bg, e.req("request-0000000004", b)); err != nil || r.State != "signed" {
		t.Fatalf("another wallet: %+v %v", r, err)
	}
	e.now.Add(601)
	if r, err := c.Sign(bg, e.req("request-0000000005", small(5))); err != nil || r.State != "signed" {
		t.Fatalf("after the window: %+v %v", r, err)
	}
	bad := *e.cfg
	for _, c := range []struct {
		v  Velocity
		ok bool
	}{{Velocity{2, 600}, true}, {Velocity{0, 600}, false}, {Velocity{2, 0}, false}, {Velocity{2, 86401}, false}} {
		raw, _ := json.Marshal(struct {
			*Config
			Velocity Velocity `json:"velocity"`
		}{&bad, c.v})
		if _, err := ParseConfig(raw); (err == nil) != c.ok {
			t.Errorf("velocity %+v: %v", c.v, err)
		}
	}
}
