// Package facilitator is an x402 v2 facilitator (docs/commercial-facilitator.md):
// POST /verify (read only), POST /settle and GET /supported, over the shop's
// own settlement (merchant.Settler): its checks, its one transfer per
// authorization, its confirmations. It settles the exact scheme on one chain,
// in one token, to one payee: anything else is refused, never sent.
package facilitator

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strconv"
	"strings"

	"github.com/SIMPLYBOYS/agent-payment-authority/internal/contracts"
	"github.com/SIMPLYBOYS/agent-payment-authority/internal/eth"
	"github.com/SIMPLYBOYS/agent-payment-authority/internal/merchant"
	"github.com/SIMPLYBOYS/agent-payment-authority/internal/x402"
)

// Settler is what the facilitator settles with (merchant.Settler).
type Settler interface {
	Check(ctx context.Context, req x402.Requirement, payload json.RawMessage) (payer, reason string)
	Settle(ctx context.Context, req x402.Requirement, key string, payload json.RawMessage) merchant.Outcome
	Refund(ctx context.Context, settleTx string) merchant.RefundOutcome
}

// Server answers the three endpoints for one chain, token and payee.
type Server struct {
	st      Settler
	network string // CAIP-2
	token   eth.Address
	payTo   eth.Address
	signer  string // the address that sends the transfers (its gas key)
}

// New is the facilitator over st, for cfg's chain, token and payee; signer
// is the address its transfers are sent from.
func New(st Settler, cfg merchant.SettleConfig, signer eth.Address) *Server {
	return &Server{st: st, network: "eip155:" + strconv.FormatUint(cfg.ChainID, 10), token: cfg.Token, payTo: cfg.PayTo, signer: signer.Hex()}
}

func (f *Server) Register(mux *http.ServeMux) {
	mux.HandleFunc("POST /verify", f.verify)
	mux.HandleFunc("POST /settle", f.settle)
	mux.HandleFunc("GET /supported", f.supported)
	mux.HandleFunc("POST /refund", f.refund)
}

// refund is not x402's: the shop's refund of one of its own settlements, paid and not delivered
// (docs/commercial-refund.md), back to its payer, the whole amount, once. The body names the settlement's
// transaction only; the payer and the amount are the settler's record.
func (f *Server) refund(w http.ResponseWriter, r *http.Request) {
	raw, err := io.ReadAll(io.LimitReader(r.Body, 1<<10+1))
	var in struct {
		Transaction string `json:"transaction"`
	}
	if err != nil || len(raw) > 1<<10 || contracts.Decode(raw, &in) != nil {
		reply(w, http.StatusBadRequest, merchant.RefundOutcome{Kind: "invalid", Reason: "invalid_request"})
		return
	}
	o := f.st.Refund(r.Context(), in.Transaction)
	status := http.StatusOK
	if o.Kind == "pending" && o.Transaction == "" {
		status = http.StatusServiceUnavailable // nothing sent: ask again
	}
	reply(w, status, o)
}

func (f *Server) supported(w http.ResponseWriter, _ *http.Request) {
	reply(w, http.StatusOK, x402.SupportedResponse{Kinds: []x402.SupportedKind{{X402Version: x402.Version, Scheme: "exact", Network: f.network}},
		Extensions: []string{}, Signers: map[string][]string{"eip155:*": {f.signer}}})
}

func (f *Server) verify(w http.ResponseWriter, r *http.Request) {
	req, payload, _, reason, ok := f.read(w, r)
	if !ok {
		return
	}
	if reason != "" {
		reply(w, http.StatusOK, x402.VerifyResponse{InvalidReason: reason})
		return
	}
	payer, reason := f.st.Check(r.Context(), req, payload)
	status := http.StatusOK
	if reason == "settlement_node_unavailable" {
		status = http.StatusServiceUnavailable // the node, not the payment: ask again
	}
	reply(w, status, x402.VerifyResponse{IsValid: reason == "", InvalidReason: reason, Payer: payer})
}

func (f *Server) settle(w http.ResponseWriter, r *http.Request) {
	req, payload, key, reason, ok := f.read(w, r)
	if !ok {
		return
	}
	if reason != "" {
		reply(w, http.StatusOK, x402.SettleResponse{ErrorReason: reason, Network: f.network})
		return
	}
	out := f.st.Settle(r.Context(), req, key, payload)
	resp := out.Response
	resp.Network = f.network
	status := http.StatusOK
	switch out.Kind {
	case "delivered": // confirmed to the settler's depth
		resp.Success, resp.ErrorReason, resp.Amount = true, "", string(req.Amount)
	case "pending":
		if resp.Transaction != "" {
			resp.ErrorReason = x402.ErrSettlementPending // sent: the same request again answers it
		} else {
			resp.ErrorReason, status = out.Reason, http.StatusServiceUnavailable // nothing sent: ask again
		}
	default: // failed, invalid: nothing (more) sent
		resp.ErrorReason = out.Reason
	}
	reply(w, status, resp)
}

// read decodes a /verify or /settle request and checks it is one this
// facilitator settles: version 2 in both places, accepted exactly the
// requirements, exact on its network, in its token, to its payee. A body
// that is no request answers 400 itself (ok false); a request it does not
// settle has a reason. key is what the payment is for: its resource, or the
// requirements' hash when it names none.
func (f *Server) read(w http.ResponseWriter, r *http.Request) (req x402.Requirement, payload json.RawMessage, key, reason string, ok bool) {
	raw, err := io.ReadAll(io.LimitReader(r.Body, contracts.MaxJSONBytes+1))
	var in x402.FacilitatorRequest
	if err != nil || len(raw) > contracts.MaxJSONBytes || contracts.Decode(raw, &in) != nil {
		reply(w, http.StatusBadRequest, map[string]string{"error": x402.ErrInvalidPayload})
		return req, nil, "", "", false
	}
	p := in.PaymentPayload
	if in.X402Version != x402.Version || p.X402Version != x402.Version {
		return req, nil, "", x402.ErrInvalidVersion, true
	}
	got, err1 := contracts.Canonical(p.Accepted)
	want, err2 := contracts.Canonical(in.PaymentRequirements)
	if errors.Join(err1, err2) != nil || !bytes.Equal(got, want) || len(p.Payload) == 0 || p.Payload[0] != '{' {
		return req, nil, "", x402.ErrInvalidRequirements, true
	}
	if contracts.Decode(in.PaymentRequirements, &req) != nil {
		return req, nil, "", x402.ErrInvalidRequirements, true
	}
	asset, err1 := eth.ParseAddress(strings.ToLower(req.Asset))
	payTo, err2 := eth.ParseAddress(strings.ToLower(req.PayTo))
	switch {
	case req.Scheme != "exact":
		return req, nil, "", x402.ErrInvalidScheme, true
	case req.Network != f.network:
		return req, nil, "", x402.ErrInvalidNetwork, true
	case errors.Join(err1, err2) != nil || asset != f.token || payTo != f.payTo:
		return req, nil, "", x402.ErrInvalidRequirements, true
	}
	if p.Resource != nil && p.Resource.URL != "" {
		key = p.Resource.URL
	} else {
		sum := sha256.Sum256(want)
		key = "requirement:" + hex.EncodeToString(sum[:])
	}
	return req, p.Payload, key, "", true
}

func reply(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(v)
}
