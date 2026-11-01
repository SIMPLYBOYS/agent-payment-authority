package verification

import (
	"testing"

	"github.com/SIMPLYBOYS/agent-payment-authority/internal/contracts"
	"github.com/SIMPLYBOYS/agent-payment-authority/internal/merchant"
)

// e1a.3 (docs/commercial-venues.md): each venue saw exactly what it asks; unmet exactly when the claims fall short.
func TestVenueClaims(t *testing.T) {
	v2 := func(detail, claims string) *contracts.CredentialCheck {
		return &contracts.CredentialCheck{Result: "valid", Detail: detail, CredentialSchema: "apa.credential.v2", Claims: claims}
	}
	data, beer, cafe := merchant.RequiresAt("/resources/kr-market"), merchant.RequiresAt("/scenes/mart/beer-6"), merchant.RequiresAt("/scenes/cafe/lunch-set")
	if books := merchant.RequiresAt("/scenes/books/seoul-guide"); len(books) != 1 || books["country"] != "" {
		t.Fatalf("the bookshop asks for the country only: %v", books)
	}
	if data == nil || beer == nil || cafe != nil {
		t.Fatalf("the shop's requirements: %v %v %v", data, beer, cafe)
	}
	for name, c := range map[string]struct {
		c    *contracts.CredentialCheck
		req  map[string]string
		pass bool
	}{
		"data, the country":             {v2("", "country=TW"), data, true}, // docs/commercial-consumer.md D4: the country only
		"data, called unmet":            {v2("CLAIM_UNMET", "country=TW"), data, false},
		"data, the buyer type too":      {v2("", "buyer_type=individual,country=TW"), data, false},
		"data, the age too":             {v2("", "country=TW,age_over_18=true"), data, false},
		"beer, an adult":                {v2("", "age_over_18=true"), beer, true},
		"beer, a minor unmet":           {v2("CLAIM_UNMET", "age_over_18=false"), beer, true},
		"beer, a minor let through":     {v2("", "age_over_18=false"), beer, false},
		"beer, an adult called unmet":   {v2("CLAIM_UNMET", "age_over_18=true"), beer, false},
		"cafe, nothing disclosed":       {v2("", ""), cafe, true},
		"cafe, the country disclosed":   {v2("", "country=TW"), cafe, false},
		"v1 before the venues":          {&contracts.CredentialCheck{Result: "valid", CredentialSchema: "apa.credential.v1"}, data, true},
		"v1 at the beer, unmet":         {&contracts.CredentialCheck{Result: "valid", Detail: "CLAIM_UNMET", CredentialSchema: "apa.credential.v1"}, beer, true},
		"revoked: nothing established":  {&contracts.CredentialCheck{Result: "revoked", CredentialSchema: "apa.credential.v2"}, beer, true},
	} {
		if why := venueClaims(c.c, c.req); (why == "") != c.pass {
			t.Errorf("%s: %q", name, why)
		}
	}
}
