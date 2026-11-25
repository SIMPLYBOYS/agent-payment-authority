package credential

import (
	"crypto/ed25519"
	"testing"
)

// A presentation to a shop (boundary 4, docs/commercial-credential-ext.md 4):
// only the schema, the issuer and what the shop asks; bound to the shop's view
// of the payment; and nothing two presentations share that a shop could
// match them by.
func TestPresentToShop(t *testing.T) {
	is, err := NewIssuerKey("issuer-demo", issuerSeed[:])
	if err != nil {
		t.Fatal(err)
	}
	holder := ed25519.NewKeyFromSeed(holderSeed[:])
	cred := Credential{"schema": SchemaV2, "issuer": is.ID, "task_id": "show-a-01", "consumer_id": "consumer-a", "c0": C0Placeholder,
		"expiry": "1791903600", "holder": HolderField(holder.Public().(ed25519.PublicKey)), "status": "7",
		"buyer_type": "individual", "country": "KR", "age_over_18": "true"}
	sig, err := is.Issue(cred)
	if err != nil {
		t.Fatal(err)
	}
	b := ShopBinding{Verifier: "did:web:127.0.0.1%3A8402", ResourceURL: "http://127.0.0.1:8402/resources/kr-market?edition=1",
		Accepted: []byte(`{"amount":"500000"}`), PaymentNonce: "0x" + "ab"}
	asks := map[string]string{"country": ""}
	p, err := PresentToShop(cred, sig, is.PK, b, asks)
	if err != nil {
		t.Fatal(err)
	}
	if len(p.Disclosed) != 3 || p.Disclosed["country"] != "KR" || p.Disclosed["issuer"] != "issuer-demo" || p.Disclosed["schema"] != SchemaV2 {
		t.Fatalf("disclosed %v", p.Disclosed)
	}
	for _, f := range []string{"holder", "task_id", "status", "expiry", "consumer_id"} {
		if _, ok := p.Disclosed[f]; ok {
			t.Errorf("%s disclosed to a shop", f)
		}
	}
	if err := VerifyAtShop(p, b, "issuer-demo", is.PK, asks); err != nil {
		t.Fatalf("verify: %v", err)
	}
	// Another presentation of the same credential to the same shop: the same
	// disclosed values (nothing identifying in them), another proof.
	q, _ := PresentToShop(cred, sig, is.PK, b, asks)
	if string(q.Proof) == string(p.Proof) {
		t.Error("two presentations carry the same proof")
	}
	for name, c := range map[string]struct {
		b      ShopBinding
		issuer string
		asks   map[string]string
	}{
		"another shop":          {ShopBinding{Verifier: "did:web:other", ResourceURL: b.ResourceURL, Accepted: b.Accepted, PaymentNonce: b.PaymentNonce}, "issuer-demo", asks},
		"another payment":       {ShopBinding{Verifier: b.Verifier, ResourceURL: b.ResourceURL, Accepted: b.Accepted, PaymentNonce: "0xcd"}, "issuer-demo", asks},
		"another resource":      {ShopBinding{Verifier: b.Verifier, ResourceURL: b.ResourceURL + "x", Accepted: b.Accepted, PaymentNonce: b.PaymentNonce}, "issuer-demo", asks},
		"another accepts entry": {ShopBinding{Verifier: b.Verifier, ResourceURL: b.ResourceURL, Accepted: []byte(`{"amount":"1"}`), PaymentNonce: b.PaymentNonce}, "issuer-demo", asks},
		"an untrusted issuer":   {b, "issuer-other", asks},
		"a value it requires":   {b, "issuer-demo", map[string]string{"country": "JP"}},
		"more than it showed":   {b, "issuer-demo", map[string]string{"country": "", "age_over_18": ""}},
	} {
		if err := VerifyAtShop(p, c.b, c.issuer, is.PK, c.asks); err == nil {
			t.Errorf("%s: accepted", name)
		}
	}
	tampered := p
	tampered.Disclosed = map[string]string{"schema": SchemaV2, "issuer": "issuer-demo", "country": "JP"}
	if err := VerifyAtShop(tampered, b, "issuer-demo", is.PK, asks); err == nil {
		t.Error("a changed attribute verified")
	}
	if _, err := PresentToShop(cred, sig, is.PK, b, map[string]string{"birth_date": ""}); err == nil {
		t.Error("presented an attribute the credential does not have")
	}
}
