package facilitator

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/SIMPLYBOYS/agent-payment-authority/internal/contracts"
	"github.com/SIMPLYBOYS/agent-payment-authority/internal/eth"
	"github.com/SIMPLYBOYS/agent-payment-authority/internal/eth/ethtest"
	"github.com/SIMPLYBOYS/agent-payment-authority/internal/merchant"
	"github.com/SIMPLYBOYS/agent-payment-authority/internal/signer"
	"github.com/SIMPLYBOYS/agent-payment-authority/internal/x402"
)

var payTo, _ = eth.ParseAddress("0x3333333333333333333333333333333333333333")

// fixture is a local anvil with TestUSDC, a funded payer and the facilitator over the shop's settlement.
type fixture struct {
	t     *testing.T
	rpc   *eth.Client
	usdc  eth.Address
	payer *eth.Key
	mux   *http.ServeMux
	req   json.RawMessage // the requirement, canonical
}

func newFixture(t *testing.T, confirmations uint64) *fixture {
	t.Helper()
	c := ethtest.Anvil(t)
	usdc := ethtest.Deploy(t, c, "TestUSDC.sol/TestUSDC")
	pk, _ := eth.NewKey(eth.Keccak256([]byte("facilitator test payer")))
	data, _ := eth.Call("mint(address,uint256)", pk.Addr, uint64(5_000_000))
	ethtest.Mine(t, c, ethtest.Key(t, 0), &eth.Tx{To: &usdc, Gas: 200_000, Data: data})
	cfg := merchant.SettleConfig{ChainID: ethtest.Chain, Token: usdc, TokenName: "USDC", TokenVersion: "2", PayTo: payTo, Confirmations: confirmations,
		Poll: 50 * time.Millisecond, Wait: time.Second} // pending answered within a second
	st, err := merchant.OpenSettler(context.Background(), cfg, ethtest.Key(t, 4), c, filepath.Join(t.TempDir(), "settle.db"), func() uint64 { return ethtest.Now(t, c) })
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { st.Close() })
	mux := http.NewServeMux()
	New(st, cfg, ethtest.Key(t, 4).Addr).Register(mux)
	raw, _ := json.Marshal(x402.Requirement{Scheme: "exact", Network: "eip155:" + strconv.Itoa(ethtest.Chain), Amount: "500000", Asset: usdc.Hex(), PayTo: payTo.Hex(),
		MaxTimeoutSeconds: 300, Extra: map[string]string{"name": "USDC", "version": "2"}})
	req, _ := contracts.Canonical(raw)
	return &fixture{t: t, rpc: c, usdc: usdc, payer: pk, mux: mux, req: req}
}

// payload is an exact-EVM payload: value to payTo, signed by the payer; tamper may change the signature.
func (f *fixture) payload(value uint64, tamper func([]byte)) json.RawMessage {
	now := ethtest.Now(f.t, f.rpc)
	var nonce eth.Bytes32
	rand.Read(nonce[:])
	n := "0x" + hex.EncodeToString(nonce[:])
	msg := signer.Transfer{From: f.payer.Addr.Hex(), To: payTo.Hex(), Value: strconv.FormatUint(value, 10), ValidAfter: now - 10, ValidBefore: now + 60, Nonce: n}
	sep, _ := signer.DomainSeparator(signer.Domain{Name: "USDC", Version: "2", ChainID: strconv.Itoa(ethtest.Chain), VerifyingContract: f.usdc.Hex()})
	th, _ := signer.TransferHash(msg)
	rs, rec, err := f.payer.Sign(signer.TypedDigest(sep, th))
	if err != nil {
		f.t.Fatal(err)
	}
	sig := append(rs[:], 27+rec)
	if tamper != nil {
		tamper(sig)
	}
	out, _ := json.Marshal(map[string]any{"signature": "0x" + hex.EncodeToString(sig), "authorization": map[string]string{"from": msg.From, "to": msg.To,
		"value": msg.Value, "validAfter": strconv.FormatUint(msg.ValidAfter, 10), "validBefore": strconv.FormatUint(msg.ValidBefore, 10), "nonce": n}})
	return out
}

// request is a /verify or /settle body: payload for the requirement (accepted the same unless given), for resource.
func (f *fixture) request(payload, accepted json.RawMessage, resource string) x402.FacilitatorRequest {
	if accepted == nil {
		accepted = f.req
	}
	p := x402.PaymentPayload{X402Version: 2, Accepted: accepted, Payload: payload}
	if resource != "" {
		p.Resource = &x402.ResourceInfo{URL: resource}
	}
	return x402.FacilitatorRequest{X402Version: 2, PaymentPayload: p, PaymentRequirements: f.req}
}

func (f *fixture) post(path string, body any, out any) int {
	raw, _ := json.Marshal(body)
	rec := httptest.NewRecorder()
	f.mux.ServeHTTP(rec, httptest.NewRequest("POST", path, bytes.NewReader(raw)))
	if out != nil {
		if err := json.Unmarshal(rec.Body.Bytes(), out); err != nil {
			f.t.Fatalf("%s: %v %s", path, err, rec.Body)
		}
	}
	return rec.Code
}

func (f *fixture) balance(a eth.Address) uint64 {
	data, _ := eth.Call("balanceOf(address)", a)
	out, err := f.rpc.Call(context.Background(), eth.Msg{To: f.usdc, Data: data})
	if err != nil {
		f.t.Fatal(err)
	}
	v, _ := eth.Uint(out, 0)
	return v.Uint64()
}

func (f *fixture) gasNonce() uint64 {
	n, _ := f.rpc.PendingNonce(context.Background(), ethtest.Key(f.t, 4).Addr)
	return n
}

// GET /supported: exact on its chain, sent from its gas key.
func TestSupported(t *testing.T) {
	f := newFixture(t, 1)
	rec := httptest.NewRecorder()
	f.mux.ServeHTTP(rec, httptest.NewRequest("GET", "/supported", nil))
	var s x402.SupportedResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &s); err != nil || rec.Code != 200 {
		t.Fatalf("%d %s", rec.Code, rec.Body)
	}
	if len(s.Kinds) != 1 || s.Kinds[0] != (x402.SupportedKind{X402Version: 2, Scheme: "exact", Network: "eip155:" + strconv.Itoa(ethtest.Chain)}) ||
		len(s.Signers["eip155:*"]) != 1 || s.Signers["eip155:*"][0] != ethtest.Key(t, 4).Addr.Hex() || s.Extensions == nil {
		t.Fatalf("supported: %+v", s)
	}
}

// POST /verify is read only: a valid payment is said valid and nothing is sent or kept (it settles after).
func TestVerifyIsReadOnly(t *testing.T) {
	f := newFixture(t, 1)
	pl := f.payload(500000, nil)
	nonce, payee := f.gasNonce(), f.balance(payTo)
	var v x402.VerifyResponse
	if code := f.post("/verify", f.request(pl, nil, "http://shop/r"), &v); code != 200 || !v.IsValid || v.InvalidReason != "" || v.Payer != f.payer.Addr.Hex() {
		t.Fatalf("verify: %d %+v", code, v)
	}
	if f.gasNonce() != nonce || f.balance(payTo) != payee {
		t.Fatal("verify sent something")
	}
	var s x402.SettleResponse
	if f.post("/settle", f.request(pl, nil, "http://shop/r"), &s); !s.Success {
		t.Fatalf("settle after verify: %+v", s)
	}
}

// Everything it does not settle is refused, with the spec's reason, and sends nothing.
func TestRefusedRequestsSendNothing(t *testing.T) {
	f := newFixture(t, 1)
	nonce := f.gasNonce()
	other := func(field, value string) json.RawMessage {
		var m map[string]any
		json.Unmarshal(f.req, &m)
		m[field] = value
		raw, _ := json.Marshal(m)
		c, _ := contracts.Canonical(raw)
		return c
	}
	for name, tc := range map[string]struct {
		req  x402.FacilitatorRequest
		want string
	}{
		"bad signature":                 {f.request(f.payload(500000, func(s []byte) { s[5] ^= 1 }), nil, ""), "invalid_exact_evm_payload_signature"},
		"less paid":                     {f.request(f.payload(400000, nil), nil, ""), "invalid_exact_evm_payload_authorization_value"},
		"accepted not the requirements": {f.request(f.payload(500000, nil), other("amount", "1"), ""), x402.ErrInvalidRequirements},
		"version 1": {func() x402.FacilitatorRequest {
			r := f.request(f.payload(500000, nil), nil, "")
			r.X402Version = 1
			return r
		}(), x402.ErrInvalidVersion},
		"another network": {func() x402.FacilitatorRequest {
			r := f.request(f.payload(500000, nil), other("network", "eip155:1"), "")
			r.PaymentRequirements = r.PaymentPayload.Accepted
			return r
		}(), x402.ErrInvalidNetwork},
		"another payee": {func() x402.FacilitatorRequest {
			r := f.request(f.payload(500000, nil), other("payTo", "0x4444444444444444444444444444444444444444"), "")
			r.PaymentRequirements = r.PaymentPayload.Accepted
			return r
		}(), x402.ErrInvalidRequirements},
		"another scheme": {func() x402.FacilitatorRequest {
			r := f.request(f.payload(500000, nil), other("scheme", "upto"), "")
			r.PaymentRequirements = r.PaymentPayload.Accepted
			return r
		}(), x402.ErrInvalidScheme},
	} {
		var v x402.VerifyResponse
		if f.post("/verify", tc.req, &v); v.IsValid || v.InvalidReason != tc.want {
			t.Errorf("%s: verify %+v, want %s", name, v, tc.want)
		}
		var s x402.SettleResponse
		if f.post("/settle", tc.req, &s); s.Success || s.ErrorReason != tc.want || s.Transaction != "" {
			t.Errorf("%s: settle %+v, want %s", name, s, tc.want)
		}
	}
	if code := f.post("/settle", map[string]any{"x402Version": 2, "unknown": 1}, nil); code != http.StatusBadRequest {
		t.Errorf("no request: %d", code)
	}
	if f.gasNonce() != nonce {
		t.Fatal("a refused request sent a transfer")
	}
}

// One authorization pays once: settled, the same request again is the same transaction and nothing more;
// the same authorization for another resource is a replay.
func TestSettleOncePerAuthorization(t *testing.T) {
	f := newFixture(t, 1)
	pl := f.payload(500000, nil)
	payee := f.balance(payTo)
	var s x402.SettleResponse
	if code := f.post("/settle", f.request(pl, nil, "http://shop/r"), &s); code != 200 || !s.Success || len(s.Transaction) != 66 || s.Amount != "500000" ||
		s.Network != "eip155:"+strconv.Itoa(ethtest.Chain) || s.Payer != f.payer.Addr.Hex() {
		t.Fatalf("settle: %d %+v", code, s)
	}
	if f.balance(payTo) != payee+500000 {
		t.Fatal("not paid")
	}
	nonce := f.gasNonce()
	var again x402.SettleResponse
	if f.post("/settle", f.request(pl, nil, "http://shop/r"), &again); !again.Success || again.Transaction != s.Transaction || f.gasNonce() != nonce {
		t.Fatalf("again: %+v (first %s)", again, s.Transaction)
	}
	var replay x402.SettleResponse
	if f.post("/settle", f.request(pl, nil, "http://shop/other"), &replay); replay.Success || replay.ErrorReason != "invalid_exact_evm_payload_authorization_nonce_used" {
		t.Fatalf("replay: %+v", replay)
	}
	if f.balance(payTo) != payee+500000 {
		t.Fatal("paid twice")
	}
}

// Sent and not yet confirmed: settlement_pending with its transaction; once confirmed, the same request succeeds with it.
func TestSettlementPendingThenSettled(t *testing.T) {
	f := newFixture(t, 3)
	pl := f.payload(500000, nil)
	var s x402.SettleResponse
	if f.post("/settle", f.request(pl, nil, ""), &s); s.Success || s.ErrorReason != x402.ErrSettlementPending || len(s.Transaction) != 66 {
		t.Fatalf("pending: %+v", s)
	}
	ethtest.RPC(t, f.rpc, "evm_mine")
	ethtest.RPC(t, f.rpc, "evm_mine")
	var done x402.SettleResponse
	if f.post("/settle", f.request(pl, nil, ""), &done); !done.Success || done.Transaction != s.Transaction {
		t.Fatalf("settled: %+v (pending %s)", done, s.Transaction)
	}
}

// POST /refund (docs/commercial-refund.md): the facilitator's own settlement refunded to its payer, the whole
// amount, once, from its gas key; a body that names anything else is refused and sends nothing.
func TestRefund(t *testing.T) {
	f := newFixture(t, 1)
	gas := ethtest.Key(t, 4).Addr
	data, _ := eth.Call("mint(address,uint256)", gas, uint64(1_000_000))
	ethtest.Mine(t, f.rpc, ethtest.Key(t, 0), &eth.Tx{To: &f.usdc, Gas: 200_000, Data: data})
	var s x402.SettleResponse
	if f.post("/settle", f.request(f.payload(500000, nil), nil, "http://shop/r"), &s); !s.Success {
		t.Fatalf("settle: %+v", s)
	}
	n := f.gasNonce()
	for name, c := range map[string]struct {
		body any
		code int
		kind string
	}{
		"another transaction": {map[string]string{"transaction": "0x" + strings.Repeat("cd", 32)}, 200, "invalid"},
		"no transaction":      {map[string]string{}, 400, "invalid"},
		"another field":       {map[string]string{"transaction": s.Transaction, "to": f.payer.Addr.Hex()}, 400, "invalid"},
	} {
		var o merchant.RefundOutcome
		if code := f.post("/refund", c.body, &o); code != c.code || o.Kind != c.kind || f.gasNonce() != n {
			t.Errorf("%s: %d %+v", name, code, o)
		}
	}
	var o merchant.RefundOutcome
	if code := f.post("/refund", map[string]string{"transaction": s.Transaction}, &o); code != 200 || o.Kind != "refunded" || o.Amount != "500000" ||
		!strings.EqualFold(o.Payer, f.payer.Addr.Hex()) || f.gasNonce() != n+1 || f.balance(f.payer.Addr) != 5_000_000 {
		t.Fatalf("refund: %d %+v, payer %d", code, o, f.balance(f.payer.Addr))
	}
	var again merchant.RefundOutcome
	if f.post("/refund", map[string]string{"transaction": s.Transaction}, &again); again != o || f.gasNonce() != n+1 {
		t.Fatalf("again: %+v", again)
	}
}
