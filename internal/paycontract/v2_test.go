package paycontract

import (
	"strings"
	"testing"

	"github.com/SIMPLYBOYS/agent-payment-authority/internal/contracts"
)

// toV2 is a v1 order's business fields under apa.pay.v2's version, domain
// and hashes.
func toV2(t *testing.T, p PurchaseOrder) PurchaseOrder {
	t.Helper()
	sp, _ := SpecOf(V2)
	rh, err := sp.ResourceHash(p.Intent.HTTPMethod, p.Intent.ResourceURL)
	if err != nil {
		t.Fatal(err)
	}
	qh, err := sp.RequirementHash(p.Quote)
	if err != nil {
		t.Fatal(err)
	}
	p.Version, p.Intent.Version, p.Intent.Domain, p.Intent.ResourceHash, p.Intent.PaymentRequirementHash = V2, V2, DomainV2, rh, qh
	return p
}

// apa.pay.v2 golden vectors for the same M1 fixture purchase (frozen at
// C2a; a change needs a new version, never an edit here). Every one
// differs from v1's (TestGoldenVectors): another domain.
func TestV2GoldenVectors(t *testing.T) {
	_, p1 := base(t)
	p := toV2(t, p1)
	sp, _ := SpecOf(V2)
	key, _ := p.RetryKey()
	g, _ := sp.GenesisHash(p.Intent.TaskID)
	session := sp.SessionID("task-a", p.Intent.PurchaseOrderID)
	got := map[string]string{
		"resource_hash": p.Intent.ResourceHash, "payment_requirement_hash": p.Intent.PaymentRequirementHash, "intent_hash": mustHash(t, p),
		"retry_key": key, "genesis_task_a": g, "session_id": session, "grant_id": sp.GrantID(session), "delegation_id": sp.DelegationID("dlg-req-000000000001"),
	}
	want := map[string]string{
		"resource_hash":            "sha256:7eead973f92b487cbc8abfa02286e81de18b531663b2d98ff36a8f58a9f39b89",
		"payment_requirement_hash": "sha256:8fa7b3e837e6f3dba62303e3e6f531d228e105f258a151d45597f87d8cfbd1d3",
		"intent_hash":              "sha256:42ee271cb23b5db2e071e8754cfde8670be82b9d406870ddfe22daac1c44ca50",
		"retry_key":                "sha256:c1e44a19292e290426d67eef642a2840c8ef2d3c7860a7c8162a7ed95d771c91",
		"genesis_task_a":           "sha256:af5e4f553efc6314bcf8f2a6ebd728cf465dfeb25d4f15fb10c4fa27ab276bca",
		"session_id":               "aps-a5b739e0e486e401524335139c742ecb",
		"grant_id":                 "grant-3baa95f77b6ce9f0d73697d4ecb84fbc",
		"delegation_id":            "dlg-930116ae4de71dbd614f32dd4e7d773a",
	}
	for k, v := range got {
		if want[k] != v {
			t.Errorf("%s = %q, frozen %q", k, v, want[k])
		}
	}
	// The package-level functions stay v1's.
	if SessionID("task-a", p.Intent.PurchaseOrderID) == session || DelegationID("dlg-req-000000000001") == want["delegation_id"] {
		t.Fatal("a package-level ID is not v1's")
	}
}

// Each version takes only its own domain and hashes; one business purchase
// under two versions is two digests and one purchase_order_id.
func TestVersionsApart(t *testing.T) {
	_, p1 := base(t)
	p2 := toV2(t, p1)
	if mustHash(t, p1) == mustHash(t, p2) {
		t.Fatal("v1 and v2 intents share a hash")
	}
	for name, mut := range map[string]func(*PurchaseOrder){
		"v2 order, v1 domain":        func(p *PurchaseOrder) { p.Intent.Domain = Domain },
		"v2 order, v1 intent":        func(p *PurchaseOrder) { p.Intent.Version = Version },
		"v2 order, v1 resource hash": func(p *PurchaseOrder) { p.Intent.ResourceHash = p1.Intent.ResourceHash },
		"v2 order, v1 quote hash":    func(p *PurchaseOrder) { p.Intent.PaymentRequirementHash = p1.Intent.PaymentRequirementHash },
		"unknown version":            func(p *PurchaseOrder) { p.Version, p.Intent.Version = "apa.pay.v9", "apa.pay.v9" },
	} {
		p := p2
		mut(&p)
		if p.Validate() == nil {
			t.Errorf("%s: validated", name)
		}
	}
	v1Under2 := p1
	v1Under2.Intent.Domain = DomainV2
	if v1Under2.Validate() == nil {
		t.Fatal("a v1 order took the v2 domain")
	}
	if _, err := CompareRetry(p1, p2); Code(err) != "IDEMPOTENCY_CONFLICT" {
		t.Fatalf("the same purchase_order_id under two versions: %v", err)
	}
}

// The credential reasons are v2's only.
func TestDecisionReasonsByVersion(t *testing.T) {
	_, p1 := base(t)
	for _, c := range []struct {
		version, reason string
		ok              bool
	}{
		{V2, "CREDENTIAL_REVOKED", true}, {V2, "CREDENTIAL_INVALID", true}, {V2, "CREDENTIAL_STATUS_UNAVAILABLE", true},
		{V2, "INSUFFICIENT_BUDGET", true}, {Version, "CREDENTIAL_REVOKED", false}, {"apa.pay.v9", "INSUFFICIENT_BUDGET", false},
	} {
		d := contracts.PolicyDecision{Version: c.version, IntentHash: mustHash(t, p1), PolicyVerdict: "Deny", ApprovalStatus: "not_requested",
			PaymentStatus: PaymentNotEnabled, Reason: c.reason}
		if err := ValidateDecision(d); (err == nil) != c.ok {
			t.Errorf("%s %s: %v", c.version, c.reason, err)
		}
	}
	sp, _ := SpecOf(V2)
	if got := sp.ReasonCodes(); len(got) != len(ReasonCodes)+3 {
		t.Fatalf("v2 reasons %v", got)
	}
}

// One event stream has one version: a v2 event cannot follow a v1 genesis.
func TestEventStreamOneVersion(t *testing.T) {
	scope := &contracts.EventScope{ConsumerID: "consumer-a", AgentID: "agent-a", WalletID: "wallet-a", TaskID: "task-a"}
	g, _ := GenesisHash("task-a")
	first := contracts.AuditEvent{Version: Version, EventID: "evt-1", EventType: "TASK_STARTED", Scope: scope, Sequence: 1, PrevEventHash: g,
		EvidenceMode: "fixture", Source: "host", ObservedAt: 1790000000}
	h, err := EventHash(first)
	if err != nil {
		t.Fatal(err)
	}
	second := first
	second.Version, second.EventID, second.EventType, second.Sequence, second.PrevEventHash = V2, "evt-2", "DELEGATION_BOUND", 2, h
	if err := ValidateEventOrder([]contracts.AuditEvent{first, second}); Code(err) != "INVALID_EVENT_ORDER" {
		t.Fatalf("a mixed stream: %v", err)
	}
	// A v2 stream starts at v2's genesis, not v1's.
	sp, _ := SpecOf(V2)
	g2, _ := sp.GenesisHash("task-a")
	v2first := first
	v2first.Version, v2first.PrevEventHash = V2, g
	if err := ValidateEventOrder([]contracts.AuditEvent{v2first}); Code(err) != "EVENT_CHAIN_BROKEN" {
		t.Fatalf("v2 on v1's genesis: %v", err)
	}
	v2first.PrevEventHash = g2
	if err := ValidateEventOrder([]contracts.AuditEvent{v2first}); err != nil {
		t.Fatalf("v2 on v2's genesis: %v", err)
	}
	if _, err := EventHash(contracts.AuditEvent{Version: "apa.pay.v9"}); err == nil {
		t.Fatal("hashed an event of no pay version")
	}
}

// A grant's jti is its own version's.
func TestGrantIDByVersion(t *testing.T) {
	sp, _ := SpecOf(V2)
	g := AuthorizationGrant{Version: V2, ApprovalSessionID: "aps-1", JTI: sp.GrantID("aps-1"), IntentHash: "sha256:" + strings.Repeat("0", 64),
		Decision: "approve", AuthorizationMethod: "simulated", Issuer: Issuer, Audience: "signer", ApproverID: "sim-a", IssuedAt: 1790000000, ExpiresAt: 1790000060}
	if err := g.Validate(); err != nil {
		t.Fatalf("v2 grant: %v", err)
	}
	g.JTI = GrantID("aps-1") // v1's
	if g.Validate() == nil {
		t.Fatal("a v2 grant took v1's jti")
	}
}
