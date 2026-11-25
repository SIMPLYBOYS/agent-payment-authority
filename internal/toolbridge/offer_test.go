package toolbridge

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/SIMPLYBOYS/agent-payment-authority/internal/contracts"
	"github.com/SIMPLYBOYS/agent-payment-authority/internal/x402"
	"github.com/decred/dcrd/dcrec/secp256k1/v4"
)

// The Host takes a signed offer only when it holds (docs/commercial-offer-receipt.md):
// its terms are the 402's, and its key is the one the shop's did:web document names.
func TestSignedOfferChecked(t *testing.T) {
	key, _ := secp256k1.GeneratePrivateKey()
	other, _ := secp256k1.GeneratePrivateKey()
	req := x402.Requirement{Scheme: "exact", Network: "eip155:84532", Amount: "500000", Asset: "0x" + strings.Repeat("4", 40), PayTo: "0x" + strings.Repeat("3", 40),
		MaxTimeoutSeconds: 60, Extra: map[string]string{"name": "USDC", "version": "2"}}
	var srv *httptest.Server
	var signWith, docFor *secp256k1.PrivateKey
	var amount string
	var jws bool
	var extra map[string]string
	srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		did, _ := x402.DIDWeb(srv.URL)
		if r.URL.Path == "/.well-known/did.json" {
			json.NewEncoder(w).Encode(x402.ShopDocument(did, x402.Address(docFor.PubKey())))
			return
		}
		url := srv.URL + "/resources/kr-market?edition=1"
		s, _ := x402.SignOffer(signWith, x402.OfferPayload{Version: 1, ResourceURL: url, Scheme: req.Scheme, Network: req.Network, Asset: req.Asset, PayTo: req.PayTo, Amount: amount})
		if jws {
			s = x402.Signed{Format: "jws", Signature: "e30.e30.c2ln"}
		}
		ext, _ := x402.OfferExtensions(s)
		entry := req
		entry.Extra = map[string]string{"name": "USDC", "version": "2"}
		for k, v := range extra {
			entry.Extra[k] = v
		}
		accepts, _ := json.Marshal(entry)
		h, _ := x402.Encode(x402.PaymentRequired{X402Version: 2, Resource: x402.ResourceInfo{URL: url}, Accepts: []json.RawMessage{accepts}, Extensions: ext})
		w.Header().Set(x402.HeaderPaymentRequired, h)
		w.WriteHeader(http.StatusPaymentRequired)
	}))
	defer srv.Close()
	shop := &Shop{Client: NewClient(), Now: func() uint64 { return 1790726450 }}
	p := Product{ID: "kr-market", URL: srv.URL + "/resources/kr-market?edition=1"}
	for name, c := range map[string]struct {
		sign, doc *secp256k1.PrivateKey
		amount    string
		jws       bool
		extra     map[string]string
		ok        bool
	}{
		"the shop's key, the 402's terms":         {key, key, "500000", false, nil, true},
		"a key the document does not name":        {other, key, "500000", false, nil, false},
		"another amount than the 402's":           {key, key, "1", false, nil, false},
		"a JWS offer: as if unsigned":             {key, key, "500000", true, nil, true},
		"the authorization flow, named":           {key, key, "500000", false, map[string]string{"paymentFlow": "authorization"}, true},
		"an escrow flow":                          {key, key, "500000", false, map[string]string{"paymentFlow": "escrow"}, false},
		"a flow it does not know":                 {key, key, "500000", false, map[string]string{"paymentFlow": "later"}, false},
		"a Permit2 transfer the Signer cannot do": {key, key, "500000", false, map[string]string{"assetTransferMethod": "permit2"}, false},
	} {
		signWith, docFor, amount, jws, extra = c.sign, c.doc, c.amount, c.jws, c.extra
		if _, _, err := shop.quote(context.Background(), p); (err == nil) != c.ok {
			t.Errorf("%s: %v", name, err)
		}
	}
}

// Boundary 4: the credential discloses what the shop's 402 asks, inside what
// the host allows the venue; the host's own when the shop asks nothing.
func TestVenueRequires(t *testing.T) {
	allowed := map[string]string{"country": "", "age_over_18": "true"}
	quote := func(schema string, req map[string]string) contracts.Quote {
		m, _ := x402.CredentialMember(x402.CredentialRequirement{Schema: schema, Requires: req})
		ext, _ := json.Marshal(map[string]json.RawMessage{x402.CredentialKey: m})
		return contracts.Quote{Extensions: ext}
	}
	if got, err := venueRequires(contracts.Quote{Extensions: json.RawMessage(`{}`)}, allowed); err != nil || len(got) != 2 {
		t.Errorf("none declared: %v %v", got, err)
	}
	if got, err := venueRequires(quote("apa.credential.v2", map[string]string{"country": ""}), allowed); err != nil || len(got) != 1 || got["country"] != "" {
		t.Errorf("the shop's, narrower: %v %v", got, err)
	}
	if _, err := venueRequires(quote("apa.credential.v2", map[string]string{"birth_date": ""}), allowed); err == nil {
		t.Error("an attribute the host does not allow was disclosed")
	}
	if _, err := venueRequires(quote("other.schema", map[string]string{"country": ""}), allowed); err == nil {
		t.Error("another schema accepted")
	}
}
