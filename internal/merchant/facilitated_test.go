package merchant

import (
	"encoding/hex"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/SIMPLYBOYS/agent-payment-authority/internal/eth"
	"github.com/SIMPLYBOYS/agent-payment-authority/internal/eth/ethtest"
	"github.com/SIMPLYBOYS/agent-payment-authority/internal/x402"
)

// fakeFacilitator answers /verify and /settle as given and counts the /settle calls.
type fakeFacilitator struct {
	verify  x402.VerifyResponse
	settle  x402.SettleResponse
	settles atomic.Int32
}

func (f *fakeFacilitator) server(t *testing.T) *httptest.Server {
	mux := http.NewServeMux()
	answer := func(w http.ResponseWriter, r *http.Request, v any) {
		var req x402.FacilitatorRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil || req.X402Version != 2 || string(req.PaymentPayload.Accepted) != string(req.PaymentRequirements) {
			t.Errorf("%s: not the request the shop sends: %+v %v", r.URL.Path, req, err)
		}
		json.NewEncoder(w).Encode(v)
	}
	mux.HandleFunc("POST /verify", func(w http.ResponseWriter, r *http.Request) { answer(w, r, f.verify) })
	mux.HandleFunc("POST /settle", func(w http.ResponseWriter, r *http.Request) { f.settles.Add(1); answer(w, r, f.settle) })
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	return srv
}

// facilitatedShop is s's chain and products, settled through the facilitator at url, confirmations deep.
func (s *chainShop) facilitatedShop(url string, confirmations uint64) *http.ServeMux {
	cfg := SettleConfig{ChainID: ethtest.Chain, Token: s.usdc, TokenName: "USDC", TokenVersion: "2", PayTo: payTo, Confirmations: confirmations}
	via, err := NewFacilitated(url, cfg, s.rpc, http.DefaultClient)
	if err != nil {
		s.t.Fatal(err)
	}
	m, err := NewSettled("http://127.0.0.1:8402", via)
	if err != nil {
		s.t.Fatal(err)
	}
	mux := http.NewServeMux()
	m.Register(mux)
	return mux
}

func (s *chainShop) getVia(mux *http.ServeMux, id, header string) (*httptest.ResponseRecorder, SettlementResponse) {
	req := httptest.NewRequest("GET", strings.TrimPrefix(s.offer(id).URL, "http://127.0.0.1:8402"), nil)
	req.Header.Set(x402.HeaderPaymentSignature, header)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	var sr SettlementResponse
	if h := rec.Header().Get(HeaderPaymentResponse); h != "" {
		x402.Decode(h, &sr)
	}
	return rec, sr
}

// No facilitator answering: pending, nothing delivered; the same payment again asks again.
func TestFacilitatorUnavailableIsPending(t *testing.T) {
	s := newChainShop(t, 1)
	dead := httptest.NewServer(http.NotFoundHandler())
	dead.Close()
	mux := s.facilitatedShop(dead.URL, 1)
	if rec, _ := s.getVia(mux, "kr-market", s.header("kr-market", s.auth(500000), nil)); rec.Code != http.StatusAccepted || strings.Contains(rec.Body.String(), "canary") {
		t.Fatalf("%d %s", rec.Code, rec.Body)
	}
}

// Refused at /verify: 402 with the facilitator's reason, /settle never asked, nothing delivered.
func TestFacilitatorRefusalIs402(t *testing.T) {
	s := newChainShop(t, 1)
	f := &fakeFacilitator{verify: x402.VerifyResponse{InvalidReason: "invalid_exact_evm_payload_signature", Payer: s.payer.Addr.Hex()}}
	mux := s.facilitatedShop(f.server(t).URL, 1)
	rec, sr := s.getVia(mux, "kr-market", s.header("kr-market", s.auth(500000), nil))
	if rec.Code != http.StatusPaymentRequired || sr.Success || sr.ErrorReason != "invalid_exact_evm_payload_signature" || f.settles.Load() != 0 {
		t.Fatalf("%d %+v settles %d", rec.Code, sr, f.settles.Load())
	}
}

// Settled by the facilitator: delivered only once the transfer is deep enough on the shop's own node, and
// the same payment again reads the depth again, never asking /settle again.
func TestFacilitatedDeliveredOnceDeep(t *testing.T) {
	s := newChainShop(t, 1)
	// A real transaction for the facilitator to name: a transfer of 1 base unit from anvil's key 0.
	data, _ := eth.Call("mint(address,uint256)", payTo, uint64(1))
	rc := ethtest.Mine(t, s.rpc, ethtest.Key(t, 0), &eth.Tx{To: &s.usdc, Gas: 200_000, Data: data})
	tx := "0x" + hex.EncodeToString(rc.Logs[0].TxHash[:]) // the mint's Transfer: its transaction
	f := &fakeFacilitator{verify: x402.VerifyResponse{IsValid: true, Payer: s.payer.Addr.Hex()},
		settle: x402.SettleResponse{Success: true, Transaction: tx, Network: "eip155:84532", Payer: s.payer.Addr.Hex()}}
	mux := s.facilitatedShop(f.server(t).URL, 3)
	h := s.header("kr-market", s.auth(500000), nil)
	if rec, sr := s.getVia(mux, "kr-market", h); rec.Code != http.StatusAccepted || sr.Transaction != "" && sr.Success {
		t.Fatalf("not deep yet: %d %+v", rec.Code, sr)
	}
	ethtest.RPC(t, s.rpc, "evm_mine")
	ethtest.RPC(t, s.rpc, "evm_mine")
	rec, sr := s.getVia(mux, "kr-market", h)
	if rec.Code != http.StatusOK || !sr.Success || sr.Transaction != tx || !strings.Contains(rec.Body.String(), "canary") {
		t.Fatalf("deep: %d %+v %s", rec.Code, sr, rec.Body)
	}
	if n := f.settles.Load(); n != 1 {
		t.Fatalf("/settle asked %d times", n)
	}
}
