package x402

import (
	"encoding/base64"
	"encoding/json"
	"os"
	"strings"
	"testing"

	"github.com/SIMPLYBOYS/agent-payment-authority/internal/contracts"
)

// The spec's own example headers must decode, or the shop and a real x402
// client would disagree on the wire format.
func TestDecodesSpecExamples(t *testing.T) {
	raw, err := os.ReadFile("../../testdata/x402/spec-examples.json")
	if err != nil {
		t.Fatal(err)
	}
	var ex struct {
		PaymentRequired  string `json:"payment_required"`
		PaymentSignature string `json:"payment_signature"`
	}
	if err := json.Unmarshal(raw, &ex); err != nil {
		t.Fatal(err)
	}
	var pr PaymentRequired
	if err := Decode(ex.PaymentRequired, &pr); err != nil {
		t.Fatalf("spec PAYMENT-REQUIRED: %v", err)
	}
	if err := pr.Validate(); err != nil || pr.Resource.URL != "https://api.example.com/premium-data" || pr.Error != "PAYMENT-SIGNATURE header is required" {
		t.Fatalf("spec PAYMENT-REQUIRED = %+v, %v", pr, err)
	}
	if _, err := pr.Quote(0); err != nil {
		t.Fatalf("spec requirement as a contracts quote: %v", err)
	}
	var pp PaymentPayload
	if err := Decode(ex.PaymentSignature, &pp); err != nil {
		t.Fatalf("spec PAYMENT-SIGNATURE: %v", err)
	}
	if pp.X402Version != 2 || pp.Resource == nil || string(pr.Accepts[0]) != string(pp.Accepted) {
		t.Fatalf("spec PAYMENT-SIGNATURE = %+v; accepted should echo the offer", pp)
	}
}

func TestEncodeRoundTrip(t *testing.T) {
	in := PaymentRequired{
		X402Version: 2,
		Resource:    ResourceInfo{URL: "http://127.0.0.1:8402/r?x=1"},
		Accepts:     []json.RawMessage{json.RawMessage(`{"scheme":"exact","b":1,"a":"é"}`)},
	}
	h, err := Encode(in)
	if err != nil {
		t.Fatal(err)
	}
	var out PaymentRequired
	if err := Decode(h, &out); err != nil {
		t.Fatal(err)
	}
	// Canonical: keys sorted, unknown members kept.
	if string(out.Accepts[0]) != `{"a":"é","b":1,"scheme":"exact"}` || out.Resource != in.Resource {
		t.Fatalf("round trip = %+v", out)
	}
}

func TestDecodeFailsClosed(t *testing.T) {
	// Chosen so the standard encoding holds '+' and '/' and ends in padding.
	raw := []byte(`{"x402Version":2,"accepted":{"k":"???>>>~~"},"payload":{}}`)
	std := base64.StdEncoding.EncodeToString(raw)
	if !strings.ContainsAny(std, "+/") || !strings.HasSuffix(std, "=") {
		t.Fatalf("test input lost its alphabet or padding markers: %s", std)
	}
	var ok PaymentPayload
	if err := Decode(std, &ok); err != nil {
		t.Fatalf("control: %v", err)
	}
	// The same bytes with the unused low bits before "=" set: lenient decoding
	// yields the valid payload again, so only Strict() rejects it.
	pad := strings.IndexByte(std, '=')
	const alphabet = "ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz0123456789+/"
	malleated := std[:pad-1] + string(alphabet[strings.IndexByte(alphabet, std[pad-1])|1]) + std[pad:]
	if lenient, err := base64.StdEncoding.DecodeString(malleated); err != nil || string(lenient) != string(raw) {
		t.Fatalf("malleated input should decode leniently to the same bytes: %v", err)
	}
	b64 := base64.StdEncoding.EncodeToString
	cases := map[string]string{
		"empty":             "",
		"url-safe alphabet": base64.URLEncoding.EncodeToString(raw),
		"unpadded":          base64.RawStdEncoding.EncodeToString(raw),
		"not base64":        "paid",
		"non-zero pad bits": malleated,
		"not JSON":          b64([]byte("settled")),
		"trailing JSON":     b64(append(raw, []byte(" {}")...)),
		"duplicate member":  b64([]byte(`{"x402Version":2,"x402Version":1,"accepted":{},"payload":{}}`)),
		"unknown member":    b64([]byte(`{"x402Version":2,"accepted":{},"payload":{},"paid":true}`)),
		"case-variant name": b64([]byte(`{"X402Version":2,"accepted":{},"payload":{}}`)),
		"null member":       b64([]byte(`{"x402Version":2,"accepted":null,"payload":{}}`)),
		"missing member":    b64([]byte(`{"x402Version":2,"accepted":{}}`)),
		"float":             b64([]byte(`{"x402Version":2.0,"accepted":{},"payload":{}}`)),
		"too large":         b64([]byte(`{"x402Version":2,"accepted":{"p":"` + strings.Repeat("x", contracts.MaxJSONBytes) + `"},"payload":{}}`)),
	}
	for name, h := range cases {
		var p PaymentPayload
		if err := Decode(h, &p); err == nil {
			t.Errorf("%s: accepted", name)
		}
	}
}

func TestQuote(t *testing.T) {
	offer := json.RawMessage(`{"scheme":"exact","network":"eip155:84532","amount":"1","asset":"0x4444444444444444444444444444444444444444","payTo":"0x3333333333333333333333333333333333333333","maxTimeoutSeconds":60}`)
	pr := PaymentRequired{X402Version: 2, Resource: ResourceInfo{URL: "http://127.0.0.1/r"}, Accepts: []json.RawMessage{offer}}
	absent, err := pr.Quote(0)
	if err != nil {
		t.Fatal(err)
	}
	pr.Extensions = json.RawMessage(`{}`)
	empty, _ := pr.Quote(0)
	h1, _ := absent.Hash()
	h2, _ := empty.Hash()
	if h1 != h2 {
		t.Fatal("absent and empty extensions should be the same quote")
	}
	pr.Extensions = json.RawMessage(`{"fixture":{"info":{},"schema":{}}}`)
	withExt, _ := pr.Quote(0)
	if h3, _ := withExt.Hash(); h3 == h1 {
		t.Fatal("extensions must be part of the quote hash")
	}

	ambiguous := pr
	ambiguous.Accepts = []json.RawMessage{json.RawMessage(`{"payTo":"0x3333333333333333333333333333333333333333","payto":"0x1111111111111111111111111111111111111111"}`)}
	if _, err := ambiguous.Quote(0); contracts.ErrorCode(err) != "AMBIGUOUS_KEY" {
		t.Fatalf("case-variant keys: %v, want AMBIGUOUS_KEY", err)
	}
	for name, bad := range map[string]PaymentRequired{
		"version 1":    {X402Version: 1, Resource: pr.Resource, Accepts: pr.Accepts},
		"no accepts":   {X402Version: 2, Resource: pr.Resource},
		"no resource":  {X402Version: 2, Accepts: pr.Accepts},
		"array offer":  {X402Version: 2, Resource: pr.Resource, Accepts: []json.RawMessage{json.RawMessage(`[]`)}},
		"index beyond": pr,
	} {
		i := 0
		if name == "index beyond" {
			i = 1
		}
		if _, err := bad.Quote(i); err == nil {
			t.Errorf("%s: accepted", name)
		}
	}
	// Validate stands alone for buyers checking an offer before quoting it.
	if err := (PaymentRequired{X402Version: 1, Resource: pr.Resource, Accepts: pr.Accepts}).Validate(); err == nil {
		t.Error("Validate accepted x402Version 1")
	}
}
