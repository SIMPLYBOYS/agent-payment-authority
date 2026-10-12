package signer

import (
	"strings"
	"testing"

	"github.com/SIMPLYBOYS/agent-payment-authority/internal/paycontract"
)

// The stub refuses a simulated grant the first time and again: this proves
// the stub's rule only, not a Signer's single use (P1-b).
func TestStubRefusesSimulated(t *testing.T) {
	sid := paycontract.SessionID("task-a", "po-1")
	g := paycontract.AuthorizationGrant{Version: paycontract.Version, JTI: paycontract.GrantID(sid), IntentHash: "sha256:" + strings.Repeat("a", 64), ApprovalSessionID: sid,
		Decision: "approve", AuthorizationMethod: "simulated", Issuer: paycontract.Issuer, Audience: "signer", ApproverID: "simulated:consumer-a:host-x", IssuedAt: 100, ExpiresAt: 160}
	for i := 0; i < 2; i++ {
		if got := Accept(g); got != MethodNotAccepted {
			t.Fatalf("attempt %d: %s", i+1, got)
		}
	}
	g.AuthorizationMethod = "webauthn" // not a P1-a grant: invalid, and nothing would sign it anyway
	if got := Accept(g); got != InvalidGrant {
		t.Fatalf("webauthn: %s", got)
	}
}
