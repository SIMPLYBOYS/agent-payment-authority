package merchant

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/SIMPLYBOYS/agent-payment-authority/internal/eth"
	"github.com/SIMPLYBOYS/agent-payment-authority/internal/eth/ethtest"
	"github.com/SIMPLYBOYS/agent-payment-authority/internal/signer"
	"github.com/SIMPLYBOYS/agent-payment-authority/internal/x402"
)

var payTo, _ = eth.ParseAddress("0x3333333333333333333333333333333333333333")

// chainShop is a local anvil with TestUSDC, a funded payer and the settling shop.
type chainShop struct {
	t     *testing.T
	rpc   *eth.Client
	usdc  eth.Address
	payer *eth.Key
	st    *Settler
	mux   *http.ServeMux
	m     *Merchant
}

func newChainShop(t *testing.T, confirmations uint64) *chainShop {
	t.Helper()
	c := ethtest.Anvil(t)
	usdc := ethtest.Deploy(t, c, "TestUSDC.sol/TestUSDC")
	pk, _ := eth.NewKey(eth.Keccak256([]byte("merchant test payer")))
	data, _ := eth.Call("mint(address,uint256)", pk.Addr, uint64(5_000_000))
	ethtest.Mine(t, c, ethtest.Key(t, 0), &eth.Tx{To: &usdc, Gas: 200_000, Data: data})
	cfg := SettleConfig{ChainID: ethtest.Chain, Token: usdc, TokenName: "USDC", TokenVersion: "2", PayTo: payTo, Confirmations: confirmations}
	st, err := OpenSettler(context.Background(), cfg, ethtest.Key(t, 4), c, filepath.Join(t.TempDir(), "settle.db"), func() uint64 { return ethtest.Now(t, c) })
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { st.Close() })
	m, err := NewSettled("http://127.0.0.1:8402", st)
	if err != nil {
		t.Fatal(err)
	}
	mux := http.NewServeMux()
	m.Register(mux)
	return &chainShop{t: t, rpc: c, usdc: usdc, payer: pk, st: st, mux: mux, m: m}
}

// auth is an EIP-3009 authorization from `from`, signed by key.
type auth struct {
	key                *eth.Key
	from, to           eth.Address
	value, after, till uint64
	nonce              eth.Bytes32
}

func (s *chainShop) auth(value uint64) auth {
	now := ethtest.Now(s.t, s.rpc)
	a := auth{key: s.payer, from: s.payer.Addr, to: payTo, value: value, after: now - 10, till: now + 60}
	rand.Read(a.nonce[:])
	return a
}

func (s *chainShop) offer(id string) Offer {
	for _, o := range s.m.Offers() {
		if o.ProductID == id {
			return o
		}
	}
	s.t.Fatalf("no offer %s", id)
	return Offer{}
}

// header is the PAYMENT-SIGNATURE of a for the product's offer; tamper
// may change the signature.
func (s *chainShop) header(id string, a auth, tamper func([]byte)) string {
	o := s.offer(id)
	dom := signer.Domain{Name: "USDC", Version: "2", ChainID: strconv.Itoa(ethtest.Chain), VerifyingContract: s.usdc.Hex()}
	nonce := "0x" + hex.EncodeToString(a.nonce[:])
	msg := signer.Transfer{From: a.from.Hex(), To: a.to.Hex(), Value: strconv.FormatUint(a.value, 10), ValidAfter: a.after, ValidBefore: a.till, Nonce: nonce}
	sep, _ := signer.DomainSeparator(dom)
	th, _ := signer.TransferHash(msg)
	rs, rec, err := a.key.Sign(signer.TypedDigest(sep, th))
	if err != nil {
		s.t.Fatal(err)
	}
	sig := append(rs[:], 27+rec)
	if tamper != nil {
		tamper(sig)
	}
	payload, _ := json.Marshal(map[string]any{"signature": "0x" + hex.EncodeToString(sig), "authorization": map[string]string{
		"from": a.from.Hex(), "to": a.to.Hex(), "value": strconv.FormatUint(a.value, 10),
		"validAfter": strconv.FormatUint(a.after, 10), "validBefore": strconv.FormatUint(a.till, 10), "nonce": nonce}})
	h, err := x402.Encode(x402.PaymentPayload{X402Version: 2, Resource: &x402.ResourceInfo{URL: o.URL}, Accepted: o.Quote.SelectedRequirement,
		Payload: payload, Extensions: json.RawMessage(`{}`)})
	if err != nil {
		s.t.Fatal(err)
	}
	return h
}

func (s *chainShop) get(id, header string) (*httptest.ResponseRecorder, SettlementResponse) {
	o := s.offer(id)
	req := httptest.NewRequest("GET", strings.TrimPrefix(o.URL, "http://127.0.0.1:8402"), nil)
	req.Header.Set(x402.HeaderPaymentSignature, header)
	rec := httptest.NewRecorder()
	s.mux.ServeHTTP(rec, req)
	var sr SettlementResponse
	if h := rec.Header().Get(HeaderPaymentResponse); h != "" {
		if err := x402.Decode(h, &sr); err != nil {
			s.t.Fatalf("PAYMENT-RESPONSE: %v", err)
		}
	}
	return rec, sr
}

func (s *chainShop) balance(a eth.Address) uint64 {
	data, _ := eth.Call("balanceOf(address)", a)
	out, err := s.rpc.Call(context.Background(), eth.Msg{To: s.usdc, Data: data})
	if err != nil {
		s.t.Fatal(err)
	}
	v, _ := eth.Uint(out, 0)
	return v.Uint64()
}

func (s *chainShop) gasNonce() uint64 {
	n, _ := s.rpc.PendingNonce(context.Background(), ethtest.Key(s.t, 4).Addr)
	return n
}

const krContent = `{"canary":"APA-PROTECTED-kr-market"}`

// Verified, then settled, then delivered: 200 with the content and a
// PAYMENT-RESPONSE naming the transfer; the same payment again answers the
// same settlement and sends nothing more.
func TestSettleAndDeliver(t *testing.T) {
	s := newChainShop(t, 1)
	h := s.header("kr-market", s.auth(500000), nil)
	before := s.gasNonce()
	rec, sr := s.get("kr-market", h)
	if rec.Code != 200 || !strings.Contains(rec.Body.String(), "APA-PROTECTED-kr-market") || !sr.Success || sr.Payer != s.payer.Addr.Hex() ||
		sr.Network != "eip155:84532" || len(sr.Transaction) != 66 {
		t.Fatalf("settle: %d %s %+v", rec.Code, rec.Body, sr)
	}
	if s.balance(payTo) != 500000 || s.balance(s.payer.Addr) != 4_500_000 {
		t.Fatal("balances")
	}
	rec2, sr2 := s.get("kr-market", h)
	if rec2.Code != 200 || sr2 != sr || s.gasNonce() != before+1 || s.balance(payTo) != 500000 {
		t.Fatalf("again: %d %+v, gas nonce %d -> %d", rec2.Code, sr2, before, s.gasNonce())
	}
}

// A payment that fails verification is answered 402 with success false
// and its reason; nothing is sent, nothing moves, nothing is delivered.
func TestVerificationFailuresSendNothing(t *testing.T) {
	s := newChainShop(t, 1)
	poor, _ := eth.NewKey(eth.Keccak256([]byte("merchant test poor payer")))
	other, _ := eth.ParseAddress("0x9999999999999999999999999999999999999999")
	now := ethtest.Now(t, s.rpc)
	// An authorization used on chain already, by someone else's submission.
	used := s.auth(500000)
	usedHeader := s.header("kr-market", used, nil)
	var p x402.PaymentPayload
	x402.Decode(usedHeader, &p)
	v, _ := parse(p.Payload)
	data, _ := transferCall(v)
	ethtest.Mine(t, s.rpc, ethtest.Key(t, 0), &eth.Tx{To: &s.usdc, Gas: 200_000, Data: data})

	for name, cs := range map[string]struct {
		a      auth
		tamper func([]byte)
		want   string
	}{
		"another signer":       {func() auth { a := s.auth(500000); a.key = poor; return a }(), nil, "invalid_exact_evm_payload_signature"},
		"another recipient":    {func() auth { a := s.auth(500000); a.to = other; return a }(), nil, "invalid_exact_evm_payload_recipient_mismatch"},
		"another amount":       {s.auth(499999), nil, "invalid_exact_evm_payload_authorization_value"},
		"expired":              {func() auth { a := s.auth(500000); a.till = now - 1; return a }(), nil, "invalid_exact_evm_payload_authorization_valid_before"},
		"not yet valid":        {func() auth { a := s.auth(500000); a.after = now + 100; return a }(), nil, "invalid_exact_evm_payload_authorization_valid_after"},
		"no funds":             {func() auth { a := s.auth(500000); a.key, a.from = poor, poor.Addr; return a }(), nil, "insufficient_funds"},
		"a tampered signature": {s.auth(500000), func(b []byte) { b[10] ^= 1 }, "invalid_exact_evm_payload_signature"},
	} {
		h := s.header("kr-market", cs.a, cs.tamper)
		before := s.gasNonce()
		rec, sr := s.get("kr-market", h)
		if rec.Code != 402 || sr.Success || sr.ErrorReason != cs.want || strings.Contains(rec.Body.String(), "APA-PROTECTED") || s.gasNonce() != before {
			t.Errorf("%s: %d %+v %s", name, rec.Code, sr, rec.Body)
		}
	}
	rec, sr := s.get("kr-market", usedHeader)
	if rec.Code != 402 || sr.ErrorReason != "invalid_exact_evm_payload_authorization_nonce_used" {
		t.Errorf("used on chain: %d %+v", rec.Code, sr)
	}
	if s.balance(payTo) != 500000 { // the one submitted directly, no other
		t.Fatalf("payTo %d", s.balance(payTo))
	}
}

// One authorization pays once: settled for one resource, it is refused
// for another; sent again it is the same settlement.
func TestReplayElsewhereRefused(t *testing.T) {
	s := newChainShop(t, 1)
	a := s.auth(500000)
	if rec, _ := s.get("kr-market", s.header("kr-market", a, nil)); rec.Code != 200 {
		t.Fatalf("first: %d %s", rec.Code, rec.Body)
	}
	// The same authorization (same nonce) for the 360000 product: amount differs, and the nonce is used.
	b := a
	b.value = 360000
	rec, sr := s.get("mart-coffee-beans", s.header("mart-coffee-beans", b, nil))
	if rec.Code != 402 || sr.Success || strings.Contains(rec.Body.String(), "APA-PROTECTED") {
		t.Fatalf("elsewhere: %d %+v", rec.Code, sr)
	}
	if s.balance(payTo) != 500000 {
		t.Fatal("paid twice")
	}
}

// Delivery waits for the confirmations: mined but not yet confirmed is 202
// with nothing delivered; the same payment once confirmed is delivered.
func TestNothingDeliveredBeforeConfirmations(t *testing.T) {
	s := newChainShop(t, 2)
	h := s.header("kr-market", s.auth(500000), nil)
	s.st.wait = time.Second
	rec, _ := s.get("kr-market", h)
	if rec.Code != 202 || strings.Contains(rec.Body.String(), "APA-PROTECTED") || rec.Header().Get(HeaderPaymentResponse) != "" {
		t.Fatalf("one block: %d %s", rec.Code, rec.Body)
	}
	before := s.gasNonce()
	ethtest.RPC(t, s.rpc, "evm_mine")
	rec, sr := s.get("kr-market", h)
	if rec.Code != 200 || !sr.Success || s.gasNonce() != before {
		t.Fatalf("two blocks: %d %+v", rec.Code, sr)
	}
}

// A settlement whose result is not known in time is pending, never failed
// and never sent twice: unmined (automine off) it answers 202; mined later,
// the same payment is delivered, with one transfer.
func TestSlowSettlementIsNotRepaid(t *testing.T) {
	s := newChainShop(t, 1)
	s.st.wait = time.Second
	h := s.header("kr-market", s.auth(500000), nil)
	ethtest.RPC(t, s.rpc, "evm_setAutomine", false)
	before := s.gasNonce()
	for range 2 {
		if rec, _ := s.get("kr-market", h); rec.Code != 202 {
			t.Fatalf("unmined: %d %s", rec.Code, rec.Body)
		}
	}
	if s.gasNonce() != before+1 {
		t.Fatalf("gas nonce %d -> %d", before, s.gasNonce())
	}
	ethtest.RPC(t, s.rpc, "evm_mine")
	ethtest.RPC(t, s.rpc, "evm_setAutomine", true)
	rec, sr := s.get("kr-market", h)
	if rec.Code != 200 || !sr.Success || s.balance(payTo) != 500000 || s.gasNonce() != before+1 {
		t.Fatalf("mined: %d %+v", rec.Code, sr)
	}
}

// A crash after the transfer is written and before it is sent: the same
// payment again sends that very transfer.
func TestSettleResumesAfterACrash(t *testing.T) {
	s := newChainShop(t, 1)
	h := s.header("kr-market", s.auth(500000), nil)
	hook = func(stage string) error {
		if stage == "after-write" {
			return errors.New("crash")
		}
		return nil
	}
	rec, _ := s.get("kr-market", h)
	hook = nil
	before := s.gasNonce()
	if rec.Code != 202 || before != s.gasNonce() {
		t.Fatalf("crash: %d", rec.Code)
	}
	rec, sr := s.get("kr-market", h)
	if rec.Code != 200 || !sr.Success || s.gasNonce() != before+1 {
		t.Fatalf("after: %d %+v", rec.Code, sr)
	}
}
