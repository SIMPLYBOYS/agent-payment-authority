package policy

import "testing"

// Rules keeps the M1 judgement order: with several checks failing, the
// first in the order names the outcome; removing it, the next one does.
func TestRulesOrder(t *testing.T) {
	p := Profile{ProfileID: "p", EvidenceMode: "fixture", AssetID: "usdc", AllowMax: "100000", TaskCap: "5000000", Merchants: []string{"0x3333333333333333333333333333333333333333"}, Mode: "interactive"}
	c := Credential{ConsumerID: "consumer-a", ValidUntil: 2000, EvidenceMode: "fixture"}
	ok := RuleInput{Profile: &p, Credential: &c, AssetTrusted: true, AssetID: "usdc", PayTo: p.Merchants[0], Amount: 500000, Remaining: 5000000, ValidAfter: 900, ValidBefore: 1100, Now: 1000}
	if got := Rules(ok); got != (RuleOutcome{NeedsApproval, "HUMAN_APPROVAL_REQUIRED"}) {
		t.Fatalf("the Ask: %+v", got)
	}
	breaks := []struct {
		reason string
		edit   func(*RuleInput)
	}{
		{"CONFIG_MISSING", func(in *RuleInput) { in.Credential = nil }},
		{"ASSET_NOT_ALLOWED", func(in *RuleInput) { in.AssetID = "other" }},
		{"MERCHANT_NOT_ALLOWED", func(in *RuleInput) { in.PayTo = "0x9999999999999999999999999999999999999999" }},
		{"AUTHORIZATION_EXPIRED", func(in *RuleInput) { in.Now = 1100 }},
		{"INSUFFICIENT_BUDGET", func(in *RuleInput) { in.Remaining = 1 }},
	}
	for first := range breaks {
		in := ok
		for _, b := range breaks[first:] {
			b.edit(&in)
		}
		if got := Rules(in); got != (RuleOutcome{Denied, breaks[first].reason}) {
			t.Errorf("checks %d.. failing: %+v, want Deny %s", first, got, breaks[first].reason)
		}
	}
	in := ok
	in.Amount = 100000
	if got := Rules(in); got != (RuleOutcome{WithinAllowMax, "WITHIN_ALLOW_MAX"}) {
		t.Errorf("at allow_max: %+v", got)
	}
	un := p
	un.Mode = "unattended"
	in = ok
	in.Profile = &un
	if got := Rules(in); got != (RuleOutcome{Denied, "APPROVER_UNAVAILABLE"}) {
		t.Errorf("unattended: %+v", got)
	}
}
