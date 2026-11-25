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
