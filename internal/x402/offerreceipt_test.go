package x402

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/decred/dcrd/dcrec/secp256k1/v4"
)

// Signed offers and receipts (docs/commercial-offer-receipt.md): one key
// signs both; the payload says what was signed; the shop's did:web document
// names the key.
func TestOfferReceipt(t *testing.T) {
	key, _ := secp256k1.GeneratePrivateKey()
	other, _ := secp256k1.GeneratePrivateKey()
	const url = "http://127.0.0.1:8402/resources/kr-market?edition=1"
	req := Requirement{Scheme: "exact", Network: "eip155:84532", Amount: "500000", Asset: "0x" + strings.Repeat("4", 40), PayTo: "0x" + strings.Repeat("3", 40)}
	op := OfferPayload{Version: 1, ResourceURL: url, Scheme: req.Scheme, Network: req.Network, Asset: req.Asset, PayTo: req.PayTo, Amount: string(req.Amount)}
	offer, err := SignOffer(key, op)
	if err != nil {
		t.Fatal(err)
	}
	if again, _ := SignOffer(key, op); again.Signature != offer.Signature {
		t.Fatal("an offer's signature is not deterministic: the 402 and its quote would change with each request")
	}
	if p, by, err := OpenOffer(offer); err != nil || by != Address(key.PubKey()) || MatchesOffer(p, url, req, 0) != nil {
		t.Fatalf("open %+v %s %v", p, by, err)
	}
	if strings.Contains(string(offer.Payload), "validUntil") {
		t.Errorf("an offer without validUntil says it: %s", offer.Payload)
	}
	cheaper := req
	cheaper.Amount = "1"
	if MatchesOffer(op, url, cheaper, 0) == nil || MatchesOffer(op, url+"x", req, 0) == nil {
		t.Error("an offer matched other terms or another resource")
	}
	if exp := (OfferPayload{Version: 1, ResourceURL: url, Scheme: "exact", Network: req.Network, Asset: req.Asset, PayTo: req.PayTo, Amount: "500000", ValidUntil: 10}); MatchesOffer(exp, url, req, 11) == nil {
		t.Error("an expired offer matched")
	}
	tampered := offer
	tampered.Payload = json.RawMessage(strings.Replace(string(offer.Payload), "500000", "500001", 1))
	if _, by, err := OpenOffer(tampered); err == nil && by == Address(key.PubKey()) {
		t.Error("a changed amount still recovers the shop's key")
	}
	// The receipt, against the offer it follows.
	ext, _ := OfferExtensions(offer)
	payer, tx := "0x"+strings.Repeat("1", 40), "0x"+strings.Repeat("a", 64)
	receipt := func(k *secp256k1.PrivateKey, edit func(*ReceiptPayload)) json.RawMessage {
		p := ReceiptPayload{Version: 1, Network: req.Network, ResourceURL: url, Payer: payer, IssuedAt: 1790726450, Transaction: tx}
		if edit != nil {
			edit(&p)
		}
		s, _ := SignReceipt(k, p)
		r, _ := ReceiptExtensions(s)
		return r
	}
	for name, c := range map[string]struct {
		resp json.RawMessage
		ok   bool
	}{
		"the offer's key, its terms": {receipt(key, nil), true},
		"none":                       {nil, false},
		"another key":                {receipt(other, nil), false},
		"another transaction":        {receipt(key, func(p *ReceiptPayload) { p.Transaction = "0x" + strings.Repeat("b", 64) }), false},
		"no transaction":             {receipt(key, func(p *ReceiptPayload) { p.Transaction = "" }), false},
		"another payer":              {receipt(key, func(p *ReceiptPayload) { p.Payer = "0x" + strings.Repeat("2", 40) }), false},
		"another resource":           {receipt(key, func(p *ReceiptPayload) { p.ResourceURL = url + "x" }), false},
	} {
		signed, err := ShopReceipt(ext, c.resp, url, req.Network, strings.ToUpper(payer), strings.ToUpper(tx)) // hex compared without case
		if !signed || (err == nil) != c.ok {
			t.Errorf("%s: %v %v", name, signed, err)
		}
	}
	if signed, err := ShopReceipt(json.RawMessage(`{}`), nil, url, req.Network, payer, tx); signed || err != nil {
		t.Errorf("an unsigned offer needs no receipt: %v %v", signed, err)
	}
	// The did:web document names the key, and only that one.
	did, err := DIDWeb("http://127.0.0.1:8402")
	if err != nil || did != "did:web:127.0.0.1%3A8402" {
		t.Fatalf("did %s %v", did, err)
	}
	doc := ShopDocument(did, Address(key.PubKey()))
	if !doc.Authorizes(did, Address(key.PubKey())) || doc.Authorizes(did, Address(other.PubKey())) || doc.Authorizes("did:web:example.com", Address(key.PubKey())) {
		t.Error("the document authorizes the wrong key or DID")
	}
}
