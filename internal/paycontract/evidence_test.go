package paycontract

import (
	"testing"

	"github.com/SIMPLYBOYS/agent-payment-authority/internal/contracts"
)

// The E1-a table (docs/commercial-e1a.md E1a-D4): complete, and each kind
// of event labeled by where it came from.
func TestEvidenceTable(t *testing.T) {
	if err := CheckEvidenceTable(); err != nil {
		t.Fatal(err)
	}
	live := &RunConditions{Agent: "live", Chain: "base_sepolia", Authenticator: "device"}
	fake := &RunConditions{Agent: "fake", Chain: "local_anvil", Authenticator: "virtual"}
	ev := func(typ, method string) contracts.AuditEvent {
		return contracts.AuditEvent{EventType: typ, Payload: contracts.EventPayload{AuthorizationMethod: method}}
	}
	for _, c := range []struct {
		r            *RunConditions
		e            contracts.AuditEvent
		mode, source string
	}{
		{nil, ev("BUDGET_RESERVED", ""), "fixture", "host"}, // a task from before E1-a
		{live, ev("TASK_STARTED", ""), "live", "host"},
		{fake, ev("TASK_STARTED", ""), "fixture", "host"},
		{live, ev("PURCHASE_PROPOSED", ""), "live", "live_cli"},
		{fake, ev("PURCHASE_PROPOSED", ""), "fixture", "fake_cli"},
		{live, ev("DELEGATION_BOUND", ""), "fixture", "simulated"}, // never live
		{live, ev("CREDENTIAL_VERIFIED", ""), "fixture", "test_issuer"},
		{live, ev("APPROVED", "simulated"), "fixture", "simulated"},
		{live, ev("APPROVED", "webauthn"), "live", "signer_webauthn"},
		{fake, ev("APPROVED", "webauthn"), "fixture", "virtual_authenticator"},
		{live, ev("APPROVAL_DENIED", ""), "live", "signer_webauthn"}, // denied by the voice
		{live, ev("VOICE_CHALLENGE_FAILED", ""), "live", "signer_webauthn"},
		{live, ev("APPROVAL_SESSION_OPENED", ""), "live", "signer_webauthn"},
		{&RunConditions{Agent: "fake", Chain: "base_sepolia", Authenticator: "device"}, ev("APPROVAL_EXPIRED", ""), "fixture", "host"},
		{live, ev("PAYMENT_SETTLED", ""), "live", "base_sepolia"},
		{fake, ev("PAYMENT_SETTLED", ""), "fixture", "local_anvil"},
		{live, ev("RECEIPT_ISSUED", ""), "live", "host_merchant"},
		{fake, ev("RESOURCE_DELIVERED", ""), "fixture", "host_merchant"},
	} {
		mode, src, ok := Evidence(c.r, c.e)
		if !ok || mode != c.mode || src != c.source {
			t.Errorf("%s (%q) under %+v: %s/%s %v, want %s/%s", c.e.EventType, c.e.Payload.AuthorizationMethod, c.r, mode, src, ok, c.mode, c.source)
		}
	}
	for _, e := range []contracts.AuditEvent{ev("APPROVED", ""), ev("GRANT_ISSUED", "voice"), ev("SOMETHING_NEW", "")} {
		if _, _, ok := Evidence(live, e); ok {
			t.Errorf("%s (%q): not in the table, yet ok", e.EventType, e.Payload.AuthorizationMethod)
		}
	}
	if _, _, ok := Evidence(&RunConditions{Agent: "live", Chain: "none", Authenticator: "none"}, ev("BUDGET_RESERVED", "")); ok {
		t.Error("a payment event without a chain: ok")
	}
	if ValidRun(RunConditions{Agent: "live", Chain: "mainnet", Authenticator: "device"}) == nil {
		t.Error("mainnet accepted")
	}
}
