package toolbridge

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
	"unicode"

	"github.com/SIMPLYBOYS/agent-payment-authority/internal/contracts"
	"github.com/SIMPLYBOYS/agent-payment-authority/internal/store"
	"github.com/SIMPLYBOYS/agent-payment-authority/internal/x402"
)

// MaxBodyBytes bounds every response body the host reads from the shop.
const MaxBodyBytes = 64 << 10

// Product is a host-trusted resource the model may ask about, by ID only.
type Product struct {
	ID, URL string
}

// PurchaseOrderID is the one purchase order ID of a task and product: a
// retried request_purchase finds it, and the demo's history replay maps a
// recorded tool call to its purchase by it.
func PurchaseOrderID(taskID, productID string) string {
	return fmt.Sprintf("po-%s-%s", taskID, productID)
}

// Shop executes the three tools against the trusted shop and the Store.
type Shop struct {
	Store      *store.Store
	CatalogURL string
	Products   map[string]Product
	Client     *http.Client
	Now        func() uint64 // trusted host clock, Unix seconds
}

// NewClient is the only HTTP client the shop tools use: no proxy from the
// environment, no redirects, bounded time and headers. Bodies are read
// through MaxBodyBytes.
func NewClient() *http.Client {
	return &http.Client{
		Timeout: 10 * time.Second,
		Transport: &http.Transport{
			Proxy:                  nil,
			MaxResponseHeaderBytes: MaxMessageBytes,
			ResponseHeaderTimeout:  10 * time.Second,
		},
		CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse },
	}
}

var _ Executor = (*Shop)(nil)

// Call runs one tool for b. Refusals carry a code and fixed text, never
// shop or request content.
func (s *Shop) Call(ctx context.Context, b *Binding, tool, product string) (ToolResult, CallRecord) {
	// The record names a product only when the host offers it: a well-formed
	// ID the CLI chose is still CLI text (a 64-hex capability is well-formed).
	rec := CallRecord{Tool: tool}
	if _, ok := s.Products[product]; ok {
		rec.ProductID = product
	}
	fail := func(code, msg string) (ToolResult, CallRecord) {
		if b.Stopped() { // the run stopped mid-call: that, not the symptom, is the outcome
			code, msg = "RUN_NOT_ACTIVE", "this run has ended"
		}
		rec.Outcome = code
		return refusal(code, msg), rec
	}
	if tool == "list_products" {
		items, err := s.catalog(ctx)
		if err != nil {
			return fail("SHOP_UNAVAILABLE", "the shop catalog could not be read safely")
		}
		rec.Outcome = "ok"
		return textResult(map[string]any{"products": items, "note": "Prices are informational; the binding quote is the shop's HTTP 402."}, false), rec
	}
	p, ok := s.Products[product]
	if !ok {
		return fail("UNKNOWN_PRODUCT", "the product is not offered by the trusted shop")
	}
	if tool == "get_quote" {
		_, req, err := s.quote(ctx, p)
		if err != nil {
			return fail("QUOTE_REFUSED", "the shop quote failed a host check")
		}
		rec.Outcome = "ok"
		return textResult(map[string]any{"product_id": p.ID, "amount": req.Amount, "asset": req.Asset, "network": req.Network,
			"note": "Quote only. Nothing is paid; a purchase request needs a human's approval."}, false), rec
	}

	// request_purchase. One logical purchase per task and product, with
	// stable identifiers: if the Store already has it, report it and submit
	// nothing; a retry never mints a new request or nonce.
	po := PurchaseOrderID(b.Session.TaskID, p.ID)
	if v, ok := s.stored(b, po); ok {
		rec.Outcome = v.Decision.PolicyVerdict
		return textResult(projection(p.ID, v, true), false), rec
	}
	quote, req, err := s.quote(ctx, p)
	if err != nil {
		return fail("QUOTE_REFUSED", "the shop quote failed a host check")
	}
	order, err := s.assemble(b, p, po, quote, req)
	if err != nil {
		return fail("PURCHASE_REFUSED", "the purchase could not be assembled from trusted data")
	}
	raw, _ := json.Marshal(order)
	var res store.Result
	err = b.Commit(func() (err error) {
		res, err = s.Store.Submit(b.Session, raw)
		return err
	})
	if errors.Is(err, ErrRunEnded) {
		return fail("RUN_NOT_ACTIVE", "this run has ended")
	}
	if errors.Is(err, ErrNotReady) {
		return fail("RUN_NOT_READY", "the host has not finished starting this run")
	}
	if err != nil {
		return fail(store.Code(err), "the host refused the purchase request")
	}
	rec.Outcome = res.Purchase.Decision.PolicyVerdict
	return textResult(projection(p.ID, res.Purchase, res.Replay), false), rec
}

func (s *Shop) stored(b *Binding, po string) (store.PurchaseView, bool) {
	v, err := s.Store.Task(b.Session)
	if err != nil {
		return store.PurchaseView{}, false
	}
	for _, p := range v.Purchases {
		if p.PurchaseOrderID == po {
			return p, true
		}
	}
	return store.PurchaseView{}, false
}

// projection is all the model learns about a decision.
func projection(product string, v store.PurchaseView, replay bool) map[string]any {
	approval := v.Decision.ApprovalStatus
	if v.ApprovalClosed != "" {
		approval = v.ApprovalClosed
	}
	return map[string]any{
		"product_id": product, "decision": v.Decision.PolicyVerdict, "reason": v.Decision.Reason,
		"approval": approval, "already_requested": replay, "payment": "none",
		"note": "No payment was made and no content was delivered; M1 cannot pay. An Ask waits for a human.",
	}
}

func (s *Shop) get(ctx context.Context, url string) (*http.Response, []byte, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, nil, err
	}
	resp, err := s.Client.Do(req)
	if err != nil {
		return nil, nil, err
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(io.LimitReader(resp.Body, MaxBodyBytes+1))
	if err != nil || len(body) > MaxBodyBytes {
		return nil, nil, errors.New("response body unreadable or too large")
	}
	return resp, body, nil
}

// quote fetches the trusted resource and checks the 402 strictly: the status,
// exactly one PAYMENT-REQUIRED header that decodes, resource.url equal to the
// URL requested (the quote hash does not cover it), and exactly one offer.
func (s *Shop) quote(ctx context.Context, p Product) (contracts.Quote, x402.Requirement, error) {
	var req x402.Requirement
	resp, _, err := s.get(ctx, p.URL)
	if err != nil {
		return contracts.Quote{}, req, err
	}
	headers := resp.Header.Values(x402.HeaderPaymentRequired)
	if resp.StatusCode != http.StatusPaymentRequired || len(headers) != 1 {
		return contracts.Quote{}, req, errors.New("not a single-offer 402")
	}
	var pr x402.PaymentRequired
	if err := x402.Decode(headers[0], &pr); err != nil {
		return contracts.Quote{}, req, err
	}
	if err := pr.Validate(); err != nil || pr.Resource.URL != p.URL || len(pr.Accepts) != 1 {
		return contracts.Quote{}, req, errors.New("402 is not for the requested resource")
	}
	q, err := pr.Quote(0)
	if err != nil {
		return contracts.Quote{}, req, err
	}
	if err := json.Unmarshal(q.SelectedRequirement, &req); err != nil {
		return contracts.Quote{}, req, err
	}
	return q, req, nil
}

// assemble builds the whole purchase from trusted data: identity from the
// binding, terms from the verified 402, times from the host clock, and IDs
// and nonce derived from task and product so a retry is the same purchase.
func (s *Shop) assemble(b *Binding, p Product, po string, q contracts.Quote, req x402.Requirement) (contracts.PurchaseOrder, error) {
	chain, ok := strings.CutPrefix(req.Network, "eip155:")
	if !ok {
		return contracts.PurchaseOrder{}, errors.New("network is not eip155")
	}
	rh, err := contracts.ResourceHash(http.MethodGet, p.URL)
	if err != nil {
		return contracts.PurchaseOrder{}, err
	}
	qh, err := q.Hash()
	if err != nil {
		return contracts.PurchaseOrder{}, err
	}
	now := s.Now()
	window := req.MaxTimeoutSeconds
	if window == 0 {
		window = 60
	}
	nonce := sha256.Sum256([]byte("apa-m1/eip3009-nonce\x00" + b.Session.TaskID + "\x00" + p.ID))
	order := contracts.PurchaseOrder{
		Version: contracts.Version, RequestID: "req-" + p.ID, Quote: q,
		Intent: contracts.AuthorizationIntent{
			Version: contracts.Version, Domain: contracts.FixtureDomain,
			ConsumerID: b.Session.ConsumerID, AgentID: b.Session.AgentID, WalletID: b.WalletID, TaskID: b.Session.TaskID,
			PurchaseOrderID: po, ResourceHash: rh, HTTPMethod: http.MethodGet, ResourceURL: p.URL,
			X402Version: q.X402Version, Scheme: req.Scheme, PaymentRequirementHash: qh,
			ChainID: chain, AssetContract: req.Asset, From: b.From, PayTo: req.PayTo, Amount: req.Amount,
			ValidAfter: now - 1, ValidBefore: now + window, EIP3009Nonce: "0x" + hex.EncodeToString(nonce[:]),
		},
	}
	return order, order.Validate()
}

type catalogItem struct {
	ProductID    string `json:"product_id"`
	Title        string `json:"title"`
	Summary      string `json:"summary"`
	AssetDisplay string `json:"price"`
	FiatDisplay  string `json:"price_local,omitempty"`
}

// catalog reads the shop's catalog and keeps only entries that match a
// trusted product exactly (ID, GET and URL). Shop text is untrusted: control
// characters are dropped and length is capped.
func (s *Shop) catalog(ctx context.Context) ([]catalogItem, error) {
	resp, body, err := s.get(ctx, s.CatalogURL)
	if err != nil || resp.StatusCode != http.StatusOK {
		return nil, errors.New("catalog unavailable")
	}
	var c struct {
		Products []struct {
			ProductID   string `json:"product_id"`
			Title       string `json:"title"`
			Summary     string `json:"summary"`
			HTTPMethod  string `json:"http_method"`
			ResourceURL string `json:"resource_url"`
			Price       struct {
				AssetDisplay string `json:"asset_display"`
				FiatAmount   string `json:"fiat_amount"`
				FiatCurrency string `json:"fiat_currency"`
			} `json:"price"`
		} `json:"products"`
	}
	if err := json.Unmarshal(body, &c); err != nil {
		return nil, err
	}
	items := []catalogItem{}
	for _, e := range c.Products {
		p, ok := s.Products[e.ProductID]
		if !ok || e.HTTPMethod != http.MethodGet || e.ResourceURL != p.URL {
			continue
		}
		item := catalogItem{ProductID: p.ID, Title: Clean(e.Title, 200), Summary: Clean(e.Summary, 200), AssetDisplay: Clean(e.Price.AssetDisplay, 200)}
		if e.Price.FiatAmount != "" {
			item.FiatDisplay = Clean(e.Price.FiatCurrency+" "+e.Price.FiatAmount, 200)
		}
		items = append(items, item)
	}
	return items, nil
}

// Clean makes untrusted text safe to keep: control and format characters
// (newlines, escape sequences, bidi overrides) are dropped, invalid UTF-8 is
// replaced, and the result is at most max runes.
func Clean(s string, max int) string {
	s = strings.Map(func(r rune) rune {
		if unicode.IsControl(r) || unicode.Is(unicode.Cf, r) {
			return -1
		}
		return r
	}, strings.ToValidUTF8(s, "\uFFFD"))
	if r := []rune(s); len(r) > max {
		s = string(r[:max])
	}
	return s
}
