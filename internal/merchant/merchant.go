// Package merchant is the M1 fixture data shop: a catalog and one protected
// resource that answers HTTP 402 with an x402 v2 PAYMENT-REQUIRED header.
//
// M1 has no settlement adapter, so no request ever receives protected
// content, whatever PAYMENT-SIGNATURE it carries. The shop fetches nothing
// and never builds a URL from the request: every URL it states comes from the
// base URL its host configured.
package merchant

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"

	"github.com/SIMPLYBOYS/agent-payment-authority/internal/contracts"
	"github.com/SIMPLYBOYS/agent-payment-authority/internal/x402"
)

type product struct {
	ID, Title, Summary, MimeType string
	Path, Query                  string // matched exactly; the query is not normalized
	Requirement                  x402.Requirement
	AssetDisplay                 string
	FiatAmount, FiatCurrency     string
	FXRate                       string // fiat units per whole asset unit; fixture, not live FX
	content                      string // protected; M1 never serves it
}

// products is fixture data. PayTo, asset and extra must match the trusted
// registry in testdata/contracts/registry.json; a test checks that they do.
var products = []product{{
	ID:       "kr-market",
	Title:    "Korean market brief, edition 1 (fixture)",
	Summary:  "Synthetic sample: sector moves and FX notes. The catalog carries no paid content.",
	MimeType: "application/json",
	Path:     "/resources/kr-market",
	Query:    "edition=1",
	Requirement: x402.Requirement{
		Scheme:            "exact",
		Network:           "eip155:84532",
		Amount:            "500000",
		Asset:             "0x4444444444444444444444444444444444444444",
		PayTo:             "0x3333333333333333333333333333333333333333",
		MaxTimeoutSeconds: 60,
		Extra:             map[string]string{"name": "USDC", "version": "2"},
	},
	AssetDisplay: "0.50 test USDC",
	FiatAmount:   "700",
	FiatCurrency: "KRW",
	FXRate:       "1400",
	content:      `{"edition":1,"canary":"APA-PROTECTED-kr-market-e1","body":"Synthetic fixture text. Seeing this in any response is a leak."}`,
}}

type offer struct {
	product
	url      string
	accepts  []byte // canonical requirement JSON
	required x402.PaymentRequired
}

type Merchant struct {
	catalog []byte
	offers  map[string]offer // path + "?" + raw query
}

type catalogEntry struct {
	ProductID   string `json:"product_id"`
	Title       string `json:"title"`
	Summary     string `json:"summary"`
	HTTPMethod  string `json:"http_method"`
	ResourceURL string `json:"resource_url"`
	Price       price  `json:"price"`
}
type price struct {
	AssetAmount  string `json:"asset_amount"`
	AssetDisplay string `json:"asset_display"`
	FiatAmount   string `json:"fiat_amount"`
	FiatCurrency string `json:"fiat_currency"`
	FXRate       string `json:"fx_rate"`
	FXSource     string `json:"fx_source"`
}

// New builds the shop for baseURL, a trusted http(s) origin with no path,
// e.g. "http://127.0.0.1:8402".
func New(baseURL string) (*Merchant, error) {
	u, err := url.Parse(baseURL)
	if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" || u.User != nil || u.Path != "" || u.RawQuery != "" || u.Fragment != "" {
		return nil, fmt.Errorf("merchant base URL %q must be an http(s) origin with no path", baseURL)
	}
	m := &Merchant{offers: map[string]offer{}}
	entries := []catalogEntry{}
	for _, p := range products {
		o := offer{product: p, url: baseURL + p.Path + "?" + p.Query}
		raw, err := json.Marshal(p.Requirement)
		if err != nil {
			return nil, err
		}
		if o.accepts, err = contracts.Canonical(raw); err != nil {
			return nil, err
		}
		o.required = x402.PaymentRequired{
			X402Version: x402.Version,
			Resource:    x402.ResourceInfo{URL: o.url, Description: p.Title, MimeType: p.MimeType},
			Accepts:     []json.RawMessage{o.accepts},
			Extensions:  json.RawMessage(`{}`),
		}
		m.offers[p.Path+"?"+p.Query] = o
		entries = append(entries, catalogEntry{
			ProductID: p.ID, Title: p.Title, Summary: p.Summary, HTTPMethod: "GET", ResourceURL: o.url,
			Price: price{string(p.Requirement.Amount), p.AssetDisplay, p.FiatAmount, p.FiatCurrency, p.FXRate, "fixture, not live FX"},
		})
	}
	m.catalog, err = json.Marshal(map[string]any{
		"merchant":      "APA fixture data shop",
		"evidence_mode": "fixture",
		"price_note":    "Catalog prices are informational. The binding quote is the PAYMENT-REQUIRED header of the HTTP 402 from resource_url.",
		"products":      entries,
	})
	return m, err
}

// Offer is one quote the shop serves at a resource.
type Offer struct {
	ProductID, Method, URL string
	Quote                  contracts.Quote
}

// Offers lists the quotes the shop's 402 serves, in product order. A host
// builds its trusted offer book from this configuration, never from a 402
// it happened to receive.
func (m *Merchant) Offers() []Offer {
	var out []Offer
	for _, p := range products {
		o := m.offers[p.Path+"?"+p.Query]
		out = append(out, Offer{ProductID: p.ID, Method: "GET", URL: o.url,
			Quote: contracts.Quote{X402Version: o.required.X402Version, SelectedRequirement: o.accepts, Extensions: o.required.Extensions}})
	}
	return out
}

// Register adds the shop's routes. Anything else stays the mux's 404.
func (m *Merchant) Register(mux *http.ServeMux) {
	mux.HandleFunc("GET /catalog", m.serveCatalog)
	paths := map[string]bool{}
	for _, o := range m.offers {
		if !paths[o.Path] {
			paths[o.Path] = true
			mux.HandleFunc("GET "+o.Path, m.serveResource)
		}
	}
}

func (m *Merchant) serveCatalog(w http.ResponseWriter, _ *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	w.Write(m.catalog)
}

// problem is the response body; the protocol data is in the header.
type problem struct {
	Error   string `json:"error,omitempty"`
	Message string `json:"message"`
}

func (m *Merchant) serveResource(w http.ResponseWriter, r *http.Request) {
	o, ok := m.offers[r.URL.Path+"?"+r.URL.RawQuery]
	if !ok {
		http.NotFound(w, r)
		return
	}
	status, body := http.StatusPaymentRequired, problem{Message: "PAYMENT-SIGNATURE header is required"}
	if sig := r.Header.Values(x402.HeaderPaymentSignature); len(sig) > 0 {
		status, body = o.judge(sig)
	}
	pr := o.required
	pr.Error = body.Message
	h, err := x402.Encode(pr)
	if err != nil {
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}
	w.Header().Set(x402.HeaderPaymentRequired, h)
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(body)
}

// judge classifies a PAYMENT-SIGNATURE per the spec's HTTP mapping: a
// malformed payload or one for other requirements is 400 (Invalid Payment); a
// well-formed one for this offer is 402 (Payment Failed), because M1 cannot
// verify or settle it. Neither path delivers anything.
func (o offer) judge(values []string) (int, problem) {
	bad := func(code, msg string) (int, problem) { return http.StatusBadRequest, problem{code, msg} }
	if len(values) != 1 {
		return bad(x402.ErrInvalidPayload, "exactly one PAYMENT-SIGNATURE header is allowed")
	}
	var p x402.PaymentPayload
	if err := x402.Decode(values[0], &p); err != nil {
		return bad(x402.ErrInvalidPayload, "PAYMENT-SIGNATURE is not a valid x402 v2 PaymentPayload")
	}
	if p.X402Version != x402.Version {
		return bad(x402.ErrInvalidVersion, "only x402Version 2 is supported")
	}
	if p.Payload[0] != '{' {
		return bad(x402.ErrInvalidPayload, "payload must be an object")
	}
	// Optional, but an object when present (spec 5.2.2); null already failed Decode.
	if p.Extensions != nil && p.Extensions[0] != '{' {
		return bad(x402.ErrInvalidPayload, "extensions must be an object")
	}
	if p.Resource != nil && p.Resource.URL != o.url {
		return bad(x402.ErrInvalidPayload, "resource does not match this endpoint")
	}
	if !bytes.Equal(p.Accepted, o.accepts) {
		var a struct{ Scheme, Network string }
		json.Unmarshal(p.Accepted, &a) // a field of another type stays "" and still fails below
		switch {
		case a.Scheme != o.Requirement.Scheme:
			return bad(x402.ErrInvalidScheme, "only scheme exact is offered")
		case a.Network != o.Requirement.Network:
			return bad(x402.ErrInvalidNetwork, "only network "+o.Requirement.Network+" is offered")
		default:
			return bad(x402.ErrInvalidRequirements, "accepted does not match the offered requirement")
		}
	}
	return http.StatusPaymentRequired, problem{"M1_PAYMENT_UNAVAILABLE", "M1 has no payment verification or settlement; nothing was charged or delivered"}
}
