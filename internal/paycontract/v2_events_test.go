package paycontract

import (
	"testing"

	"github.com/SIMPLYBOYS/agent-payment-authority/internal/contracts"
)

func testRegistry(t *testing.T) *contracts.Registry {
	t.Helper()
	var cfg contracts.RegistryConfig
	fixture(t, "testdata/contracts/registry.json", &cfg)
	r, err := contracts.NewRegistry(cfg)
	if err != nil {
		t.Fatal(err)
	}
	return r
}

// C2a review should-fix 1: an event's order, decision and grant are of the
// event's own version.
func TestEventCrossVersion(t *testing.T) {
	r := testRegistry(t)
	_, o1 := base(t)
	o2 := toV2(t, o1)
	es := chainOf(t, o2, step{"PURCHASE_PROPOSED", contracts.EventPayload{}, true})
	if err := ValidateEvent(r, es[0], &o2); err == nil {
		t.Error("a v1 event took a v2 order")
	}
	e := chainOf(t, o1, step{"PURCHASE_PROPOSED", contracts.EventPayload{}, true})[0]
	e.Version = V2
	if err := ValidateEvent(r, e, &o1); err == nil {
		t.Error("a v2 event took a v1 order")
	}
	d := decision(t, o1, "pending", "HUMAN_APPROVAL_REQUIRED")
	d.Version = V2
	es = chainOf(t, o1, step{"POLICY_DECIDED", contracts.EventPayload{Decision: d}, true})
	if err := ValidateEvent(r, es[0], &o1); err == nil {
		t.Error("a v1 event took a v2 decision")
	}
	sp2, _ := SpecOf(V2)
	sid := sp2.SessionID(o2.Intent.TaskID, o2.Intent.PurchaseOrderID)
	e = chainOf(t, o2, step{"GRANT_ISSUED", contracts.EventPayload{ApprovalSessionID: sid, GrantID: GrantID(sid), AuthorizationMethod: "simulated"}, true})[0]
	e.Version = V2
	if err := ValidateEvent(r, e, &o2); err == nil {
		t.Error("a v2 grant event took v1's grant ID")
	}
	e.Payload.GrantID = sp2.GrantID(sid)
	if err := ValidateEvent(r, e, &o2); err != nil {
		t.Errorf("a v2 grant event with v2's grant ID: %v", err)
	}
}

// Note 1: one request ID cannot name orders of two versions.
func TestCompareRetryAcrossVersions(t *testing.T) {
	_, o1 := base(t)
	o2 := o1
	o2.Intent.PurchaseOrderID = "po-other"
	o2.PaymentBinding.Nonce = "0x11111111111111111111111111111111111111111111111111111111111111aa"
	o2 = toV2(t, o2)
	if _, err := CompareRetry(o1, o2); Code(err) != "IDEMPOTENCY_CONFLICT" {
		t.Fatalf("v1 and v2 orders of one request: %v", err)
	}
}

// Note 2: a v2 stream of two events, hashed under v2's domain (frozen at
// C2a review).
func TestV2EventChainGolden(t *testing.T) {
	sp, _ := SpecOf(V2)
	scope := &contracts.EventScope{ConsumerID: "consumer-a", AgentID: "agent-a", WalletID: "wallet-a", TaskID: "task-a"}
	g, _ := sp.GenesisHash("task-a")
	first := contracts.AuditEvent{Version: V2, EventID: "evt-1", EventType: "TASK_STARTED", Scope: scope, Sequence: 1, PrevEventHash: g,
		EvidenceMode: "fixture", Source: "host", ObservedAt: 1790000000}
	h1, _ := EventHash(first)
	second := first
	second.EventID, second.EventType, second.Sequence, second.PrevEventHash = "evt-2", "DELEGATION_BOUND", 2, h1
	if err := ValidateEventOrder([]contracts.AuditEvent{first, second}); err != nil {
		t.Fatal(err)
	}
	h2, _ := EventHash(second)
	v1first := first
	v1first.Version = Version
	if v1h, _ := EventHash(v1first); v1h == h1 {
		t.Fatal("v1 and v2 hash an event alike")
	}
	if h1 != "sha256:52ff7d08f69d9a251a01b059a57138c246110e3905c59f782123d96cdfb176f9" || h2 != "sha256:60afbf451997321c679d662d6dfe1ae1d3870a24fbc9a879dc4804d18ea256a8" {
		t.Fatalf("v2 event hashes %s %s, frozen otherwise", h1, h2)
	}
}
