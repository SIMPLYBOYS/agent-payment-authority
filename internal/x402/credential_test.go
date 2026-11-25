package x402

import (
	"encoding/json"
	"testing"
)

// The credential requirement in a 402 (boundary 4): read back as written,
// absent when not declared, and kept inside what the host allows.
func TestCredentialRequirement(t *testing.T) {
	m, _ := CredentialMember(CredentialRequirement{Schema: "apa.credential.v2", Requires: map[string]string{"country": ""}})
	ext, _ := json.Marshal(map[string]json.RawMessage{CredentialKey: m, OfferReceiptKey: json.RawMessage(`{"info":{}}`)})
	r, err := CredentialIn(ext)
	if err != nil || r == nil || r.Schema != "apa.credential.v2" || r.Requires["country"] != "" || len(r.Requires) != 1 {
		t.Fatalf("read %+v %v", r, err)
	}
	if r, err := CredentialIn(json.RawMessage(`{}`)); r != nil || err != nil {
		t.Errorf("none declared: %+v %v", r, err)
	}
	for _, bad := range []string{`{"apa-credential":{"info":{"schema":"","requires":{"country":""}}}}`, `{"apa-credential":{"info":{"schema":"s","requires":{}}}}`, `{"apa-credential":1}`} {
		if _, err := CredentialIn(json.RawMessage(bad)); err == nil {
			t.Errorf("malformed accepted: %s", bad)
		}
	}
	allowed := map[string]string{"country": "", "age_over_18": "true"}
	for name, c := range map[string]struct {
		asked map[string]string
		ok    bool
	}{
		"one the host allows":       {map[string]string{"country": ""}, true},
		"a value where any is fine": {map[string]string{"country": "KR"}, true},
		"the host's fixed value":    {map[string]string{"age_over_18": "true"}, true},
		"another value than fixed":  {map[string]string{"age_over_18": "false"}, false},
		"one the host does not":     {map[string]string{"birth_date": ""}, false},
	} {
		if err := WithinCap(c.asked, allowed); (err == nil) != c.ok {
			t.Errorf("%s: %v", name, err)
		}
	}
}

// v2 5.1.2: the payment payload echoes the 402's extensions, adding the
// presentation inside apa-credential's info and deleting nothing.
func TestEcho(t *testing.T) {
	asked, _ := CredentialMember(CredentialRequirement{Schema: "apa.credential.v2", Requires: map[string]string{"country": ""}})
	required, _ := json.Marshal(map[string]json.RawMessage{CredentialKey: asked, OfferReceiptKey: json.RawMessage(`{"info":{"offers":[]}}`), "bazaar": json.RawMessage(`{"info":{"x":1}}`)})
	out, err := Echo(required, json.RawMessage(`{"version":"p"}`))
	if err != nil {
		t.Fatal(err)
	}
	var all map[string]map[string]json.RawMessage
	if err := json.Unmarshal(out, &all); err != nil {
		t.Fatal(err)
	}
	var info map[string]json.RawMessage
	json.Unmarshal(all[CredentialKey]["info"], &info)
	if string(info["schema"]) != `"apa.credential.v2"` || string(info["requires"]) != `{"country":""}` || string(info["presentation"]) != `{"version":"p"}` ||
		all[OfferReceiptKey] == nil || string(all["bazaar"]["info"]) != `{"x":1}` {
		t.Fatalf("echo %s", out)
	}
	if p, err := PresentationIn(out); err != nil || string(p) != `{"version":"p"}` {
		t.Fatalf("read back %s %v", p, err)
	}
	again, err := Echo(required, nil)
	if err != nil {
		t.Fatal(err)
	}
	if p, _ := PresentationIn(again); p != nil {
		t.Errorf("no presentation added, yet one read: %s", p)
	}
	if out, err := Echo(nil, nil); err != nil || string(out) != `{}` {
		t.Errorf("no extensions: %s %v", out, err)
	}
	if _, err := Echo(out, json.RawMessage(`{"version":"q"}`)); err == nil {
		t.Error("a presentation overwritten")
	}
}

// v2 6.1: only a payment this client can make, under a flow it knows.
func TestPayable(t *testing.T) {
	for extra, ok := range map[string]bool{
		``: true, `{"paymentFlow":"authorization"}`: true, `{"paymentFlow":"upfront"}`: true, `{"assetTransferMethod":"eip3009"}`: true,
		`{"paymentFlow":"escrow"}`: false, `{"paymentFlow":"subscription"}`: false, `{"assetTransferMethod":"permit2"}`: false, `{"assetTransferMethod":"erc7710"}`: false,
	} {
		var r Requirement
		if extra != "" {
			json.Unmarshal([]byte(extra), &r.Extra)
		}
		if err := r.Payable(); (err == nil) != ok {
			t.Errorf("%s: %v", extra, err)
		}
	}
}
