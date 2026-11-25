package merchant

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"math/big"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"reflect"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/SIMPLYBOYS/agent-payment-authority/internal/credential"
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
	return s.headerWith(id, a, tamper, json.RawMessage(`{}`))
}

// headerWith is header with ext as the payload's extensions (boundary 4: the buyer's presentation).
func (s *chainShop) headerWith(id string, a auth, tamper func([]byte), ext json.RawMessage) string {
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
		Payload: payload, Extensions: ext})
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
	if rec2.Code != 200 || !sameSettlement(sr2, sr) || s.gasNonce() != before+1 || s.balance(payTo) != 500000 {
		t.Fatalf("again: %d %+v, gas nonce %d -> %d", rec2.Code, sr2, before, s.gasNonce())
	}
	// The same authorization serialized otherwise (indented, upper-case
	// hex): the same settlement, not a replay.
	var p x402.PaymentPayload
	x402.Decode(h, &p)
	var e map[string]any
	json.Unmarshal(p.Payload, &e)
	au := e["authorization"].(map[string]any)
	au["from"] = "0x" + strings.ToUpper(strings.TrimPrefix(au["from"].(string), "0x"))
	p.Payload, _ = json.MarshalIndent(e, "", "  ")
	h3, _ := x402.Encode(p)
	if rec3, sr3 := s.get("kr-market", h3); rec3.Code != 200 || sr3.Transaction != sr.Transaction {
		t.Fatalf("re-serialized: %d %+v", rec3.Code, sr3)
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
		"a malleated (high S) signature": {s.auth(500000), func(b []byte) {
			n, _ := new(big.Int).SetString("fffffffffffffffffffffffffffffffebaaedce6af48a03bbfd25e8cd0364141", 16)
			hi := new(big.Int).Sub(n, new(big.Int).SetBytes(b[32:64]))
			hi.FillBytes(b[32:64])
			b[64] = 55 - b[64] // 27 <-> 28: the same key's other signature
		}, "invalid_exact_evm_payload_signature"},
		"longer than the offer allows": {func() auth { a := s.auth(500000); a.till = now + 600; return a }(), nil, "invalid_exact_evm_payload_authorization_valid_before"},
		"about to expire":              {func() auth { a := s.auth(500000); a.till = now + 2; return a }(), nil, "invalid_exact_evm_payload_authorization_valid_before"},
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

// A payment refused for its funds leaves nothing behind: the same
// PAYMENT-SIGNATURE, once the payer is funded, pays.
func TestRefusedThenRetried(t *testing.T) {
	s := newChainShop(t, 1)
	poor, _ := eth.NewKey(eth.Keccak256([]byte("merchant test poor payer")))
	a := s.auth(500000)
	a.key, a.from = poor, poor.Addr
	h := s.header("kr-market", a, nil)
	if rec, sr := s.get("kr-market", h); rec.Code != 402 || sr.ErrorReason != "insufficient_funds" {
		t.Fatalf("unfunded: %d %+v", rec.Code, sr)
	}
	data, _ := eth.Call("mint(address,uint256)", poor.Addr, uint64(500000))
	ethtest.Mine(t, s.rpc, ethtest.Key(t, 0), &eth.Tx{To: &s.usdc, Gas: 200_000, Data: data})
	if rec, sr := s.get("kr-market", h); rec.Code != 200 || !sr.Success {
		t.Fatalf("funded: %d %+v", rec.Code, sr)
	}
}

// Two identical requests at once: one transfer, both delivered.
func TestConcurrentSamePayment(t *testing.T) {
	s := newChainShop(t, 1)
	h := s.header("kr-market", s.auth(500000), nil)
	before := s.gasNonce()
	var wg sync.WaitGroup
	out := make([]SettlementResponse, 4)
	codes := make([]int, 4)
	for i := range out {
		wg.Add(1)
		go func() {
			defer wg.Done()
			rec, sr := s.get("kr-market", h)
			codes[i], out[i] = rec.Code, sr
		}()
	}
	wg.Wait()
	for i := range out {
		if codes[i] != 200 || !sameSettlement(out[i], out[0]) {
			t.Fatalf("%d: %d %+v", i, codes[i], out[i])
		}
	}
	if s.gasNonce() != before+1 || s.balance(payTo) != 500000 {
		t.Fatalf("gas nonce %d -> %d, payTo %d", before, s.gasNonce(), s.balance(payTo))
	}
}

// While its transfer is unmined, the same authorization for another
// resource is refused by the shop's own record (the chain has not used
// the nonce yet).
func TestReplayWhileUnmined(t *testing.T) {
	s := newChainShop(t, 1)
	s.st.wait = time.Second
	ethtest.RPC(t, s.rpc, "evm_setAutomine", false)
	h := s.header("kr-market", s.auth(500000), nil)
	if rec, _ := s.get("kr-market", h); rec.Code != 202 {
		t.Fatalf("unmined: %d", rec.Code)
	}
	var p x402.PaymentPayload
	x402.Decode(h, &p)
	var other offer
	for _, o := range s.m.offers {
		if o.ID == "kr-market" {
			other = o
		}
	}
	other.url += "&elsewhere=1"
	if out := s.st.Pay(context.Background(), other, p.Payload); out.Kind != "failed" || out.Reason != "invalid_exact_evm_payload_authorization_nonce_used" {
		t.Fatalf("elsewhere: %+v", out)
	}
	ethtest.RPC(t, s.rpc, "evm_setAutomine", true)
	ethtest.RPC(t, s.rpc, "evm_mine")
}

// Front-run: the same authorization submitted on chain by someone else
// first. The shop's transfer reverts: settlement_failed, nothing delivered,
// and payTo was paid once.
func TestFrontRunFails(t *testing.T) {
	s := newChainShop(t, 1)
	s.st.wait = time.Second
	ethtest.RPC(t, s.rpc, "evm_setAutomine", false)
	h := s.header("kr-market", s.auth(500000), nil)
	if rec, _ := s.get("kr-market", h); rec.Code != 202 {
		t.Fatalf("unmined: %d", rec.Code)
	}
	var p x402.PaymentPayload
	x402.Decode(h, &p)
	v, _ := parse(p.Payload)
	data, _ := transferCall(v)
	k := ethtest.Key(t, 0)
	nonce, _ := s.rpc.PendingNonce(context.Background(), k.Addr)
	hd, _ := s.rpc.LatestHeader(context.Background())
	tx := &eth.Tx{ChainID: big.NewInt(ethtest.Chain), Nonce: nonce, MaxPriorityFeePerGas: big.NewInt(100e9),
		MaxFeePerGas: new(big.Int).Add(new(big.Int).Mul(hd.BaseFee, big.NewInt(2)), big.NewInt(100e9)), Gas: 200_000, To: &s.usdc, Value: new(big.Int), Data: data}
	signed, _, _ := tx.Sign(k)
	if _, err := s.rpc.SendRaw(context.Background(), signed); err != nil {
		t.Fatal(err)
	}
	ethtest.RPC(t, s.rpc, "evm_mine")
	ethtest.RPC(t, s.rpc, "evm_setAutomine", true)
	rec, sr := s.get("kr-market", h)
	if rec.Code != 402 || sr.ErrorReason != "settlement_failed" || strings.Contains(rec.Body.String(), "APA-PROTECTED") || s.balance(payTo) != 500000 {
		t.Fatalf("front-run: %d %+v, payTo %d", rec.Code, sr, s.balance(payTo))
	}
}

// A transfer stuck in the pool (here: blocks too small to hold it) past
// its validBefore is failed, and the next payment takes its nonce, priced
// above it, replacing it there; stuck again, the one after that replaces
// the replacement (above both). Later transfers never wait behind it, and
// payTo is paid once, by the last.
func TestStuckTransferIsReplaced(t *testing.T) {
	s := newChainShop(t, 1)
	s.st.wait = time.Second
	ethtest.RPC(t, s.rpc, "evm_setAutomine", false)
	small, normal := "0x7530", "0x1c9c380" // 30000 is below a transfer's gas: it stays pooled
	stuckFor := func(h string) {
		t.Helper()
		ethtest.RPC(t, s.rpc, "evm_setBlockGasLimit", small)
		ethtest.RPC(t, s.rpc, "evm_mine")
		ethtest.RPC(t, s.rpc, "evm_increaseTime", 120) // past its validBefore
		ethtest.RPC(t, s.rpc, "evm_mine")
		// Normal blocks again, none mined yet: it is still pooled.
		ethtest.RPC(t, s.rpc, "evm_setBlockGasLimit", normal)
		if rec, sr := s.get("kr-market", h); rec.Code != 402 || sr.ErrorReason != "settlement_failed" {
			t.Fatalf("past its deadline: %d %+v", rec.Code, sr)
		}
	}
	first := s.header("kr-market", s.auth(500000), nil)
	if rec, _ := s.get("kr-market", first); rec.Code != 202 {
		t.Fatalf("first sent: %d", rec.Code)
	}
	before := s.gasNonce()
	stuckFor(first)
	var last string
	for i := range 2 {
		next := s.header("kr-market", s.auth(500000), nil)
		if rec, _ := s.get("kr-market", next); rec.Code != 202 {
			t.Fatalf("replacement %d sent: %d", i, rec.Code)
		}
		if s.gasNonce() != before {
			t.Fatalf("replacement %d took a new nonce: %d -> %d", i, before, s.gasNonce())
		}
		if i == 0 {
			stuckFor(next)
		}
		last = next
	}
	ethtest.RPC(t, s.rpc, "evm_mine")
	ethtest.RPC(t, s.rpc, "evm_setAutomine", true)
	if rec, sr := s.get("kr-market", last); rec.Code != 200 || !sr.Success || s.balance(payTo) != 500000 {
		t.Fatalf("last: %d %+v, payTo %d", rec.Code, sr, s.balance(payTo))
	}
}

// Identical requests overlapping while the transfer is unmined (the wait
// holds no lock): one transfer, all delivered once it is mined.
func TestConcurrentWhileUnmined(t *testing.T) {
	s := newChainShop(t, 1)
	s.st.wait = 10 * time.Second
	ethtest.RPC(t, s.rpc, "evm_setAutomine", false)
	h := s.header("kr-market", s.auth(500000), nil)
	before := s.gasNonce()
	var wg sync.WaitGroup
	codes := make([]int, 4)
	for i := range codes {
		wg.Add(1)
		go func() {
			defer wg.Done()
			rec, _ := s.get("kr-market", h)
			codes[i] = rec.Code
		}()
	}
	time.Sleep(time.Second) // all four waiting
	ethtest.RPC(t, s.rpc, "evm_mine")
	wg.Wait()
	ethtest.RPC(t, s.rpc, "evm_setAutomine", true)
	for i, c := range codes {
		if c != 200 {
			t.Fatalf("%d: %d", i, c)
		}
	}
	if s.gasNonce() != before+1 || s.balance(payTo) != 500000 {
		t.Fatalf("gas nonce %d -> %d, payTo %d", before, s.gasNonce(), s.balance(payTo))
	}
}

// DailyTxCap (docs/commercial-p2b.md B3, P2b-D5): one transfer a day here;
// the second payment is answered 402 settlement_daily_cap and sends
// nothing; the first one asked again is the same settlement, not refused.
func TestDailyTxCap(t *testing.T) {
	s := newChainShop(t, 1)
	s.st.cfg.DailyTxCap = 1
	h := s.header("kr-market", s.auth(500000), nil)
	if rec, _ := s.get("kr-market", h); rec.Code != 200 {
		t.Fatalf("first: %d %s", rec.Code, rec.Body)
	}
	before := s.gasNonce()
	rec, sr := s.get("kr-market", s.header("kr-market", s.auth(500000), nil))
	if rec.Code != 402 || sr.Success || sr.ErrorReason != "settlement_daily_cap" || s.gasNonce() != before || s.balance(payTo) != 500000 {
		t.Fatalf("second: %d %+v, gas nonce %d -> %d", rec.Code, sr, before, s.gasNonce())
	}
	if rec, _ := s.get("kr-market", h); rec.Code != 200 {
		t.Fatalf("the first again: %d", rec.Code)
	}
}

// sameSettlement: the same settlement, its receipt aside (signed again with each answer, at its own time).
func sameSettlement(a, b SettlementResponse) bool {
	a.Extensions, b.Extensions = nil, nil
	return reflect.DeepEqual(a, b)
}

// The settling shop signs its offers and receipts (x402 offer-receipt,
// docs/commercial-offer-receipt.md): the 402 carries the offer its Offers()
// gives the Host's book, the same for every request; did.json names the key;
// the delivery's PAYMENT-RESPONSE carries a receipt by that key for this
// payer and transfer.
func TestSignedOfferAndReceipt(t *testing.T) {
	s := newChainShop(t, 1)
	o := s.offer("kr-market")
	var req x402.Requirement
	json.Unmarshal(o.Quote.SelectedRequirement, &req)
	offers, err := x402.OffersIn(o.Quote.Extensions)
	if err != nil || len(offers) != 1 {
		t.Fatalf("offers %v %v", offers, err)
	}
	p, key, err := x402.OpenOffer(offers[0])
	if err != nil || x402.MatchesOffer(p, o.URL, req, 0) != nil {
		t.Fatalf("offer %+v %v", p, err)
	}
	// Boundary 4: the venue's credential requirement is in the same 402.
	if r, err := x402.CredentialIn(o.Quote.Extensions); err != nil || r == nil || r.Schema != "apa.credential.v2" || len(r.Requires) != 1 || r.Requires["country"] != "" {
		t.Fatalf("credential requirement %+v %v", r, err)
	}
	for range 2 { // the 402 is the book's, request after request
		rec := httptest.NewRecorder()
		s.mux.ServeHTTP(rec, httptest.NewRequest("GET", strings.TrimPrefix(o.URL, "http://127.0.0.1:8402"), nil))
		var pr x402.PaymentRequired
		if err := x402.Decode(rec.Header().Get(x402.HeaderPaymentRequired), &pr); err != nil {
			t.Fatal(err)
		}
		if q, _ := pr.Quote(0); string(q.Extensions) != string(o.Quote.Extensions) {
			t.Fatalf("the 402's extensions changed: %s", q.Extensions)
		}
	}
	rec := httptest.NewRecorder()
	s.mux.ServeHTTP(rec, httptest.NewRequest("GET", "/.well-known/did.json", nil))
	var doc x402.DIDDocument
	if err := json.Unmarshal(rec.Body.Bytes(), &doc); err != nil || !doc.Authorizes("did:web:127.0.0.1%3A8402", key) {
		t.Fatalf("did.json %d %s %v", rec.Code, rec.Body, err)
	}
	_, sr := s.get("kr-market", s.header("kr-market", s.auth(500000), nil))
	if signed, err := x402.ShopReceipt(o.Quote.Extensions, sr.Extensions, o.URL, sr.Network, s.payer.Addr.Hex(), sr.Transaction); !signed || err != nil {
		t.Fatalf("receipt %s: %v %v", sr.Extensions, signed, err)
	}
}

// Boundary 4 (docs/commercial-credential-ext.md 4): a shop that trusts an
// issuer settles a venue's payment only with a presentation of the asked
// attributes bound to that payment; without one, or with another payment's,
// nothing is sent.
func TestCredentialPresentedToTheShop(t *testing.T) {
	s := newChainShop(t, 1)
	is, err := credential.NewIssuerKey("issuer-test", bytes.Repeat([]byte{7}, 32))
	if err != nil {
		t.Fatal(err)
	}
	s.m.TrustIssuer(is.ID, is.PK)
	cred := credential.Credential{"schema": credential.SchemaV2, "issuer": is.ID, "task_id": "task-a", "consumer_id": "consumer-a", "c0": credential.C0Placeholder,
		"expiry": "1891903600", "holder": "ed25519:" + strings.Repeat("1", 64), "status": "3", "buyer_type": "individual", "country": "KR", "age_over_18": "true"}
	sig, err := is.Issue(cred)
	if err != nil {
		t.Fatal(err)
	}
	o := s.offer("kr-market")
	present := func(a auth) json.RawMessage {
		b := credential.ShopBinding{Verifier: "did:web:127.0.0.1%3A8402", ResourceURL: o.URL, Accepted: o.Quote.SelectedRequirement, PaymentNonce: "0x" + hex.EncodeToString(a.nonce[:])}
		p, err := credential.PresentToShop(cred, sig, is.PK, b, map[string]string{"country": ""})
		if err != nil {
			t.Fatal(err)
		}
		raw, _ := json.Marshal(p)
		ext, _ := x402.Echo(o.Quote.Extensions, raw)
		return ext
	}
	before := s.gasNonce()
	a := s.auth(500000)
	echoed, _ := x402.Echo(o.Quote.Extensions, nil)
	for name, ext := range map[string]json.RawMessage{"none": json.RawMessage(`{}`), "the 402's echoed, no presentation": echoed, "another payment's": present(s.auth(500000))} {
		rec, _ := s.get("kr-market", s.headerWith("kr-market", a, nil, ext))
		if rec.Code != 402 || !strings.Contains(rec.Body.String(), "invalid_credential") || s.gasNonce() != before {
			t.Fatalf("%s: %d %s, gas nonce %d -> %d", name, rec.Code, rec.Body, before, s.gasNonce())
		}
	}
	if rec, sr := s.get("kr-market", s.headerWith("kr-market", a, nil, present(a))); rec.Code != 200 || !sr.Success {
		t.Fatalf("with its presentation: %d %s", rec.Code, rec.Body)
	}
}
