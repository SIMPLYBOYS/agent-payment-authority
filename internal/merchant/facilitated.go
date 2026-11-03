package merchant

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
	"sync"

	"github.com/SIMPLYBOYS/agent-payment-authority/internal/eth"
	"github.com/SIMPLYBOYS/agent-payment-authority/internal/x402"
)

// Settlement settles a shop's payments: its own Settler (P2-D3), or an x402
// facilitator over HTTP (docs/commercial-facilitator.md FA2). config is the
// chain, token and payee its offers name. Only this package's two kinds are one.
type Settlement interface {
	config() SettleConfig
	Pay(ctx context.Context, o offer, payload json.RawMessage) Outcome
}

func (s *Settler) config() SettleConfig { return s.cfg }

// Facilitated settles through an x402 facilitator: POST /verify, and only a
// valid payment then POST /settle. The shop holds no gas key. Before it
// delivers, the shop reads the transfer's depth on its own node (P2-D6:
// cfg.Confirmations); a facilitator's success alone is not enough.
type Facilitated struct {
	url    string // the facilitator's base URL, without the trailing slash
	client *http.Client
	cfg    SettleConfig
	rpc    *eth.Client
	mu     sync.Mutex
	// settled: a payment the facilitator settled, by what it paid for and its payload: asked
	// again (its transfer not deep enough yet), its depth is read again, /settle never asked again.
	// ponytail: in memory; a shop started again asks /settle again, which a facilitator answers
	// for an authorization it settled (ours) or refuses as used (the chain keeps it paid once).
	settled map[string]SettlementResponse
}

// NewFacilitated settles through the facilitator at url for cfg's offers,
// reading the transfers' depth on rpc; client is the HTTP client (its
// timeout above the facilitator's own wait for a transfer).
func NewFacilitated(url string, cfg SettleConfig, rpc *eth.Client, client *http.Client) (*Facilitated, error) {
	if cfg.Confirmations == 0 || cfg.TokenName == "" || cfg.TokenVersion == "" || rpc == nil || client == nil {
		return nil, errors.New("merchant: a facilitated shop needs at least one confirmation, the token's EIP-712 domain, a node and an HTTP client")
	}
	return &Facilitated{url: strings.TrimSuffix(url, "/"), client: client, cfg: cfg, rpc: rpc, settled: map[string]SettlementResponse{}}, nil
}

func (f *Facilitated) config() SettleConfig { return f.cfg }

// Pay: the same outcomes as the shop's own settlement. Nothing the
// facilitator answered: pending, the same PAYMENT-SIGNATURE again asks again.
func (f *Facilitated) Pay(ctx context.Context, o offer, payload json.RawMessage) Outcome {
	network := "eip155:" + strconv.FormatUint(f.cfg.ChainID, 10)
	resp := SettlementResponse{Network: network}
	sum := sha256.Sum256(append([]byte(o.url+"\x00"), payload...))
	key := hex.EncodeToString(sum[:])
	f.mu.Lock()
	done, ok := f.settled[key]
	f.mu.Unlock()
	if ok {
		return f.deep(ctx, done)
	}
	req := x402.FacilitatorRequest{X402Version: x402.Version, PaymentRequirements: o.accepts,
		PaymentPayload: x402.PaymentPayload{X402Version: x402.Version, Resource: &x402.ResourceInfo{URL: o.url}, Accepted: o.accepts, Payload: payload}}
	var v x402.VerifyResponse
	if code, err := f.post(ctx, "/verify", req, &v); err != nil || code >= 500 {
		return Outcome{Kind: "pending", Reason: "facilitator_unavailable", Response: resp} // nothing sent
	}
	resp.Payer = v.Payer
	if !v.IsValid {
		resp.ErrorReason = v.InvalidReason
		if v.InvalidReason == x402.ErrInvalidPayload {
			return Outcome{Kind: "invalid", Reason: v.InvalidReason}
		}
		return Outcome{Kind: "failed", Reason: v.InvalidReason, Response: resp}
	}
	var s x402.SettleResponse
	code, err := f.post(ctx, "/settle", req, &s)
	s.Network = network
	switch {
	case err != nil:
		// No answer: it may have sent the transfer. Pending: the same payment again asks /settle again,
		// which settles one authorization once (and the chain pays it once).
		return Outcome{Kind: "pending", Reason: "facilitator_unavailable", Response: resp}
	case s.Success:
		f.mu.Lock()
		f.settled[key] = s
		f.mu.Unlock()
		return f.deep(ctx, s)
	case s.ErrorReason == x402.ErrSettlementPending && s.Transaction != "":
		return Outcome{Kind: "pending", Reason: s.ErrorReason, Response: s}
	case code >= 500 && s.Transaction == "":
		return Outcome{Kind: "pending", Reason: "facilitator_unavailable", Response: resp} // nothing sent
	default:
		if s.ErrorReason == "" {
			s.ErrorReason = "unexpected_settle_error"
		}
		return Outcome{Kind: "failed", Reason: s.ErrorReason, Response: s}
	}
}

// deep: delivered once the settled transfer is cfg.Confirmations deep on the
// shop's own node; pending before; failed if it reverted.
func (f *Facilitated) deep(ctx context.Context, s SettlementResponse) Outcome {
	h, err := hex.DecodeString(strings.TrimPrefix(s.Transaction, "0x"))
	if err != nil || len(h) != 32 {
		s.Success, s.ErrorReason = false, "invalid_transaction_state"
		return Outcome{Kind: "failed", Reason: s.ErrorReason, Response: s}
	}
	pending := s
	pending.Success, pending.ErrorReason = false, x402.ErrSettlementPending
	rc, err := f.rpc.Receipt(ctx, h)
	if err != nil || rc == nil {
		return Outcome{Kind: "pending", Reason: x402.ErrSettlementPending, Response: pending}
	}
	if rc.Status != 1 {
		s.Success, s.ErrorReason = false, "invalid_transaction_state"
		return Outcome{Kind: "failed", Reason: s.ErrorReason, Response: s}
	}
	head, err := f.rpc.BlockNumber(ctx)
	if err != nil || head+1 < rc.BlockNumber+f.cfg.Confirmations {
		return Outcome{Kind: "pending", Reason: x402.ErrSettlementPending, Response: pending}
	}
	return Outcome{Kind: "delivered", Response: s}
}

func (f *Facilitated) post(ctx context.Context, path string, body, out any) (int, error) {
	raw, err := json.Marshal(body)
	if err != nil {
		return 0, err
	}
	r, err := http.NewRequestWithContext(ctx, "POST", f.url+path, bytes.NewReader(raw))
	if err != nil {
		return 0, err
	}
	r.Header.Set("Content-Type", "application/json")
	res, err := f.client.Do(r)
	if err != nil {
		return 0, err
	}
	defer res.Body.Close()
	data, err := io.ReadAll(io.LimitReader(res.Body, 1<<16))
	if err != nil {
		return res.StatusCode, err
	}
	if err := json.Unmarshal(data, out); err != nil && res.StatusCode < 500 {
		return res.StatusCode, err // an answer that is no answer
	}
	return res.StatusCode, nil
}

// Quota is the facilitator's transfers today and its daily cap (our
// apa-facilitator's GET /health; another facilitator has none: an error).
func (f *Facilitated) Quota(ctx context.Context) (sent, cap uint64, err error) {
	r, err := http.NewRequestWithContext(ctx, "GET", f.url+"/health", nil)
	if err != nil {
		return 0, 0, err
	}
	res, err := f.client.Do(r)
	if err != nil {
		return 0, 0, err
	}
	defer res.Body.Close()
	var h struct {
		SentToday  *uint64 `json:"sent_today"`
		DailyTxCap uint64  `json:"daily_tx_cap"`
	}
	if res.StatusCode != 200 || json.NewDecoder(io.LimitReader(res.Body, 1<<12)).Decode(&h) != nil || h.SentToday == nil {
		return 0, 0, errors.New("merchant: the facilitator reports no quota")
	}
	return *h.SentToday, h.DailyTxCap, nil
}
