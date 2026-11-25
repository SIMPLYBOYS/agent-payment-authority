package toolbridge

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"maps"
	"net/http"
	"net/url"
	"slices"
	"strings"
	"time"
	"unicode"

	"github.com/SIMPLYBOYS/agent-payment-authority/internal/contracts"
	"github.com/SIMPLYBOYS/agent-payment-authority/internal/credential"
	"github.com/SIMPLYBOYS/agent-payment-authority/internal/paycontract"
	"github.com/SIMPLYBOYS/agent-payment-authority/internal/store"
	"github.com/SIMPLYBOYS/agent-payment-authority/internal/x402"
)

// MaxBodyBytes bounds every response body the host reads from the shop.
const MaxBodyBytes = 64 << 10

// Product is a host-trusted resource the model may ask about, by ID only.
// Scene is its demo scene ("" is the data shop): a run sees and buys only
// its own scene's products. Require is what its venue asks of the buyer's
// credential (docs/commercial-venues.md 3.2): attribute -> required value ("" any).
type Product struct {
	ID, URL string
	Scene   string
	Require map[string]string
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
	CatalogURL string            // the data shop's catalog
	Scenes     map[string]string // UX3: another demo scene's catalog URL, by scene
	Products   map[string]Product
	Client     *http.Client
	Now        func() uint64 // trusted host clock, Unix seconds
	// Credentials is the Host's holder for apa.pay.v2 tasks (design 7):
	// the issued credentials and the consumers' holder keys.
	Credentials *credential.Dir
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
	p, ok := s.Products[product]
	ok = ok && p.Scene == b.Scene // another scene's product is not offered to this run
	if ok {
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
		items, err := s.catalog(ctx, b.Scene)
		if err != nil {
			return fail("SHOP_UNAVAILABLE", "the shop catalog could not be read safely")
		}
		rec.Outcome = "ok"
		return textResult(map[string]any{"products": items, "note": "Prices are informational; the binding quote is the shop's HTTP 402."}, false), rec
	}
	if !ok {
		return fail("UNKNOWN_PRODUCT", "the product is not offered by the trusted shop")
	}
	if tool == "get_quote" {
		_, req, err := s.quote(ctx, p)
		if err != nil {
			return fail("QUOTE_REFUSED", "the shop quote failed a host check")
		}
		rec.Outcome = "ok"
		// A 402 may be an outside server's (test402, B5): its text is cleaned and cut as a catalog's is.
		return textResult(map[string]any{"product_id": p.ID, "amount": Clean(string(req.Amount), 80), "asset": Clean(req.Asset, 80), "network": Clean(req.Network, 80),
			"note": "Quote only. Nothing is paid; a purchase request needs a human's approval."}, false), rec
	}

	// request_purchase. One logical purchase per task and product, with
	// stable identifiers: if the Store already has it, report it and submit
	// nothing; a retry never mints a new request or nonce.
	v, replay, code := s.request(ctx, b, p, PurchaseOrderID(b.Session.TaskID, p.ID))
	if code != "" {
		return fail(code, refusalText[code])
	}
	rec.Outcome = v.Decision.PolicyVerdict
	return textResult(projection(p.ID, v, replay), false), rec
}

// refusalText is what the model is told for request's refusal codes; any
// other code is the Store's.
var refusalText = map[string]string{
	"QUOTE_REFUSED":    "the shop quote failed a host check",
	"PURCHASE_REFUSED": "the purchase could not be assembled from trusted data",
	"RUN_NOT_ACTIVE":   "this run has ended",
	"RUN_NOT_READY":    "the host has not finished starting this run",
}

// Fare is the host's own request for one ride's fare (docs/commercial-transit.md): the run has no Agent;
// the host asks when the customer walks in through a subway gate. Each ride is its own purchase
// (po-<task>-<product>-<ride>); asking again for a ride reports it. code is "" or a refusal code.
func (s *Shop) Fare(ctx context.Context, b *Binding, product string, ride int) (v store.PurchaseView, replay bool, code string) {
	p, ok := s.Products[product]
	if !ok || p.Scene != b.Scene || ride < 1 {
		return v, false, "UNKNOWN_PRODUCT"
	}
	return s.request(ctx, b, p, fmt.Sprintf("%s-%d", PurchaseOrderID(b.Session.TaskID, p.ID), ride))
}

// request is one purchase of p as po for b's task: the stored one if the Store has it, else the shop's
// quote assembled and submitted. code is "" or a refusal code (refusalText's, or the Store's).
func (s *Shop) request(ctx context.Context, b *Binding, p Product, po string) (store.PurchaseView, bool, string) {
	refused := func(code string) (store.PurchaseView, bool, string) {
		if b.Stopped() { // the run stopped mid-call: that, not the symptom, is the outcome
			code = "RUN_NOT_ACTIVE"
		}
		return store.PurchaseView{}, false, code
	}
	// A failed read (the task may not exist yet) is no stored purchase: the
	// Store decides below, as before. An apa.pay.v1 task read as "" builds an
	// M1 order, which its Submit refuses (CONTRACT_VERSION_MISMATCH).
	task, _ := s.Store.Task(b.Session)
	if v, ok := stored(task, po); ok {
		return v, true, ""
	}
	quote, req, err := s.quote(ctx, p)
	if err != nil {
		return refused("QUOTE_REFUSED")
	}
	require, err := venueRequires(quote, p.Require)
	if err != nil {
		return refused("QUOTE_REFUSED")
	}
	// The stored task's contract version picks the purchase to build; the
	// model and the request never name it.
	var order any
	if paycontract.Supported(task.ContractVersion) {
		order, err = s.assemblePay(b, p, po, quote, req, task.ContractVersion)
	} else {
		order, err = s.assemble(b, p, po, quote, req)
	}
	if err != nil {
		return refused("PURCHASE_REFUSED")
	}
	raw, _ := json.Marshal(order)
	var res store.Result
	err = b.Commit(func() (err error) {
		if task.ContractVersion == paycontract.V2 {
			res, err = s.submitV2(b, raw, require)
		} else {
			res, err = s.Store.Submit(b.Session, raw)
		}
		return err
	})
	switch {
	case errors.Is(err, ErrRunEnded):
		return refused("RUN_NOT_ACTIVE")
	case errors.Is(err, ErrNotReady):
		return refused("RUN_NOT_READY")
	case err != nil:
		return refused(store.Code(err))
	}
	return res.Purchase, res.Replay, ""
}

// venueRequires is what the buyer's credential discloses for a quote: the
// shop's requirement in its 402 (boundary 4, docs/commercial-credential-ext.md)
// within what the host allows the venue (allowed); the host's own when the
// shop declares none (M1's and the fake shops, an external one).
func venueRequires(q contracts.Quote, allowed map[string]string) (map[string]string, error) {
	asked, err := x402.CredentialIn(q.Extensions)
	switch {
	case err != nil:
		return nil, err
	case asked == nil:
		return allowed, nil
	case asked.Schema != credential.SchemaV2:
		return nil, errors.New("the shop asks for a credential schema the host does not hold")
	}
	return asked.Requires, x402.WithinCap(asked.Requires, allowed)
}

// submitV2 is an apa.pay.v2 purchase inside Commit's lock (design 7):
// the Store's challenge, the holder's presentation, the submit. Missing
// or wrong holder material is submitted as such (a decided Deny); a proof
// that failed to build submits nothing and leaves the challenge issued.
func (s *Shop) submitV2(b *Binding, raw []byte, require map[string]string) (store.Result, error) {
	ch, ok, err := s.Store.IssueChallenge(b.Session, raw, slices.Collect(maps.Keys(require)))
	if err != nil {
		return store.Result{}, err
	}
	pr := store.Presented{Require: require}
	if ok {
		if s.Credentials == nil {
			pr.Unavailable = true
		} else if p, err := s.Credentials.Present(b.Session.TaskID, b.Session.ConsumerID, ch.Request()); errors.Is(err, credential.ErrHolderMaterial) {
			pr.Unavailable = true
		} else if err != nil {
			return store.Result{}, err
		} else {
			pr.Presentation = &p
		}
	}
	return s.Store.SubmitV2(b.Session, raw, pr)
}

func stored(v store.TaskView, po string) (store.PurchaseView, bool) {
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
	payment, note := "none", "No payment was made and no content was delivered; M1 cannot pay. An Ask waits for a human."
	if paycontract.Supported(v.Decision.Version) {
		note = "No payment was made and no content was delivered; payment is not enabled. An Ask waits for a human."
	}
	if pv := v.Payment; pv != nil { // P2 (docs/commercial-p2-host.md 8): the status, never its transactions
		payment, note = pv.Status, paymentNotes[pv.Status]
		if pv.Code == "DELIVERY_FAILED" {
			note = "Paid, but the shop did not deliver the content; a person handles it. Nothing more is paid."
		}
	}
	return map[string]any{
		"product_id": product, "decision": v.Decision.PolicyVerdict, "reason": v.Decision.Reason,
		"approval": approval, "already_requested": replay, "payment": payment, "note": note,
	}
}

// paymentNotes say what a payment's status means to the model. The
// content goes to the host, never to the model (H-D2).
var paymentNotes = map[string]string{
	"paying":       "The host is paying for this purchase. Request it again later for its status; the content goes to the host, not to you.",
	"confirming":   "The payment was sent and waits for the chain to confirm it; it is never paid twice. Request it again later for its status.",
	"paid":         "Paid. The content went to the host, which shows its receipt; it is not given to you.",
	"not_paid":     "Not paid: nothing was charged and no content was delivered.",
	"needs_review": "The payment needs a person's review; nothing more is paid automatically and no content was given to you.",
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
	if err := req.Payable(); err != nil {
		return contracts.Quote{}, req, err
	}
	offers, err := x402.OffersIn(pr.Extensions)
	if err != nil {
		return contracts.Quote{}, req, err
	}
	if len(offers) > 0 { // a signed offer (offer-receipt) is checked before anything is bought on it
		if err := s.signedOffer(ctx, p.URL, req, offers); err != nil {
			return contracts.Quote{}, req, err
		}
	}
	return q, req, nil
}

// signedOffer checks a 402's signed offer (docs/commercial-offer-receipt.md):
// one, for this resource and these terms, not expired, signed by a key the
// shop's did:web document at the resource's origin names. A shop that signs
// nothing is not asked (an external one, M1's).
func (s *Shop) signedOffer(ctx context.Context, resourceURL string, req x402.Requirement, offers []x402.Signed) error {
	if len(offers) != 1 {
		return errors.New("one signed offer for the one accepts entry")
	}
	p, by, err := x402.OpenOffer(offers[0])
	if err != nil {
		return err
	}
	if err := x402.MatchesOffer(p, resourceURL, req, s.Now()); err != nil {
		return err
	}
	u, err := url.Parse(resourceURL)
	if err != nil {
		return err
	}
	origin := u.Scheme + "://" + u.Host
	did, err := x402.DIDWeb(origin)
	if err != nil {
		return err
	}
	resp, body, err := s.get(ctx, origin+"/.well-known/did.json")
	if err != nil {
		return err
	}
	var doc x402.DIDDocument
	if resp.StatusCode != http.StatusOK || json.Unmarshal(body, &doc) != nil || !doc.Authorizes(did, by) {
		return errors.New("the offer's key is not the shop's did:web key")
	}
	return nil
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

// assemblePay is assemble for a pay-family task of version: the same
// trusted sources, the business intent and the EIP-3009 payment binding
// apart, under the version's domain (its own resource and requirement
// hashes and nonce).
func (s *Shop) assemblePay(b *Binding, p Product, po string, q contracts.Quote, req x402.Requirement, version string) (paycontract.PurchaseOrder, error) {
	sp, ok := paycontract.SpecOf(version)
	if !ok {
		return paycontract.PurchaseOrder{}, errors.New("not a pay-family version")
	}
	chain, ok := strings.CutPrefix(req.Network, "eip155:")
	if !ok {
		return paycontract.PurchaseOrder{}, errors.New("network is not eip155")
	}
	rh, err := sp.ResourceHash(http.MethodGet, p.URL)
	if err != nil {
		return paycontract.PurchaseOrder{}, err
	}
	qh, err := sp.RequirementHash(q)
	if err != nil {
		return paycontract.PurchaseOrder{}, err
	}
	now := s.Now()
	window := req.MaxTimeoutSeconds
	if window == 0 {
		window = 60
	}
	label := map[string]string{paycontract.Version: "apa-pay-v1", paycontract.V2: "apa-pay-v2"}[version] // v1's as it always was
	key, rid := b.Session.TaskID+"\x00"+p.ID, "req-"+p.ID
	if ride, ok := strings.CutPrefix(po, PurchaseOrderID(b.Session.TaskID, p.ID)+"-"); ok { // a ride (Fare): its own request and nonce
		key, rid = key+"\x00"+ride, rid+"-"+ride
	}
	nonce := sha256.Sum256([]byte(label + "/eip3009-nonce\x00" + key))
	order := paycontract.PurchaseOrder{
		Version: version, RequestID: rid, Quote: q,
		Intent: paycontract.Intent{
			Version: version, Domain: sp.Domain,
			ConsumerID: b.Session.ConsumerID, AgentID: b.Session.AgentID, WalletID: b.WalletID, TaskID: b.Session.TaskID,
			PurchaseOrderID: po, ResourceHash: rh, HTTPMethod: http.MethodGet, ResourceURL: p.URL,
			X402Version: q.X402Version, Scheme: req.Scheme, PaymentRequirementHash: qh,
		},
		// The binding's addresses lowercase; the quote keeps a 402's own case (EIP-55, B5-D1).
		PaymentBinding: paycontract.PaymentBinding{Method: "eip3009", ChainID: chain, AssetContract: strings.ToLower(req.Asset), From: b.From, PayTo: strings.ToLower(req.PayTo),
			Amount: req.Amount, ValidAfter: now - 1, ValidBefore: now + window, Nonce: "0x" + hex.EncodeToString(nonce[:])},
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

// catalog reads the scene's catalog and keeps only entries that match a
// trusted product of the scene exactly (ID, GET and URL). Shop text is untrusted: control
// characters are dropped and length is capped.
func (s *Shop) catalog(ctx context.Context, scene string) ([]catalogItem, error) {
	url := s.CatalogURL
	if scene != "" {
		if url = s.Scenes[scene]; url == "" {
			return nil, errors.New("no catalog for the scene")
		}
	}
	resp, body, err := s.get(ctx, url)
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
		if !ok || p.Scene != scene || e.HTTPMethod != http.MethodGet || e.ResourceURL != p.URL {
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
