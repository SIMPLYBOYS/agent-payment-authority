package paycontract

import (
	"strings"
	"testing"

	"github.com/SIMPLYBOYS/agent-payment-authority/internal/contracts"
)

// chainOf links events of one task as the Store does (sequence, prev hash
// under this version's domain), from (type, payload, purchase) steps.
type step struct {
	typ string
	p   contracts.EventPayload
	po  bool
}

func chainOf(t *testing.T, o PurchaseOrder, steps ...step) []contracts.AuditEvent {
	t.Helper()
	h := mustHash(t, o)
	prev, _ := GenesisHash(o.Intent.TaskID)
	var out []contracts.AuditEvent
	for n, s := range steps {
		sc := &contracts.EventScope{ConsumerID: o.Intent.ConsumerID, AgentID: o.Intent.AgentID, WalletID: o.Intent.WalletID, TaskID: o.Intent.TaskID}
		if s.po {
			sc.PurchaseOrderID, sc.IntentHash = o.Intent.PurchaseOrderID, h
		}
		e := contracts.AuditEvent{Version: Version, EventID: "evt-" + strings.Repeat("0", 10) + string(rune('a'+n)), EventType: s.typ, Scope: sc, Sequence: uint64(n + 1),
			PrevEventHash: prev, Payload: s.p, EvidenceMode: "fixture", Source: "host", ObservedAt: 1790726450}
		prev, _ = EventHash(e)
		out = append(out, e)
	}
	return out
}

func decision(t *testing.T, o PurchaseOrder, approval, reason string) *contracts.PolicyDecision {
	return &contracts.PolicyDecision{Version: Version, IntentHash: mustHash(t, o), PolicyVerdict: "Ask", ApprovalStatus: approval, PaymentStatus: PaymentNotEnabled, Reason: reason}
}

func TestEventOrder(t *testing.T) {
	_, o := base(t)
	sid := SessionID(o.Intent.TaskID, o.Intent.PurchaseOrderID)
	start := []step{{"TASK_STARTED", contracts.EventPayload{}, false}, {"DELEGATION_BOUND", contracts.EventPayload{DelegationID: "dlg-1"}, false},
		{"PURCHASE_PROPOSED", contracts.EventPayload{}, true}, {"POLICY_DECIDED", contracts.EventPayload{Decision: decision(t, o, "pending", "HUMAN_APPROVAL_REQUIRED")}, true}}
	opened := step{"APPROVAL_SESSION_OPENED", contracts.EventPayload{ApprovalSessionID: sid}, true}
	approved := step{"APPROVED", contracts.EventPayload{Decision: decision(t, o, "approved", "SIMULATED_APPROVAL"), ApprovalSessionID: sid, AuthorizationMethod: "simulated"}, true}
	grant := step{"GRANT_ISSUED", contracts.EventPayload{ApprovalSessionID: sid, GrantID: GrantID(sid), AuthorizationMethod: "simulated"}, true}
	expired := step{"APPROVAL_EXPIRED", contracts.EventPayload{Decision: decision(t, o, "expired", "APPROVAL_TIMEOUT"), ApprovalSessionID: sid}, true}
	ok := append(append([]step{}, start...), opened, approved, grant)
	if err := ValidateEventOrder(chainOf(t, o, ok...)); err != nil {
		t.Fatalf("the main line: %v", err)
	}
	for name, steps := range map[string][]step{
		"no delegation bound":     append(append([]step{start[0]}, start[2:]...), opened, approved),
		"a grant before APPROVED": append(append([]step{}, start...), opened, grant),
		"two closures":            append(append([]step{}, start...), opened, approved, expired),
		"a closure, no session":   append(append([]step{}, start...), approved),
		"two grants":              append(append([]step{}, start...), opened, approved, grant, grant),
		"a grant after an expiry": append(append([]step{}, start...), opened, expired, grant),
	} {
		if err := ValidateEventOrder(chainOf(t, o, steps...)); err == nil {
			t.Errorf("%s: accepted", name)
		}
	}
}

func TestEventShape(t *testing.T) {
	m, o := base(t)
	var cfg contracts.RegistryConfig
	fixture(t, "testdata/contracts/registry.json", &cfg)
	reg, err := contracts.NewRegistry(cfg)
	if err != nil {
		t.Fatal(err)
	}
	sid := SessionID(o.Intent.TaskID, o.Intent.PurchaseOrderID)
	es := chainOf(t, o, step{"TASK_STARTED", contracts.EventPayload{}, false}, step{"DELEGATION_BOUND", contracts.EventPayload{DelegationID: "dlg-1"}, false},
		step{"APPROVED", contracts.EventPayload{Decision: decision(t, o, "approved", "SIMULATED_APPROVAL"), ApprovalSessionID: sid, AuthorizationMethod: "simulated"}, true},
		step{"GRANT_ISSUED", contracts.EventPayload{ApprovalSessionID: sid, GrantID: GrantID(sid), AuthorizationMethod: "simulated"}, true},
		step{"APPROVED", contracts.EventPayload{Decision: decision(t, o, "approved", "SIMULATED_APPROVAL"), ApprovalSessionID: sid, AuthorizationMethod: "webauthn"}, true},
		step{"SETTLED", contracts.EventPayload{}, true})
	for i, want := range []bool{true, true, true, true, false, false} {
		var order *PurchaseOrder
		if es[i].Scope.PurchaseOrderID != "" {
			order = &o
		}
		if err := ValidateEvent(reg, es[i], order); (err == nil) != want {
			t.Errorf("event %d %s: %v", i, es[i].EventType, err)
		}
		// The M1 rules refuse every apa.pay.v1 event.
		var mo *contracts.PurchaseOrder
		if order != nil {
			mo = &m
		}
		if reg.ValidateEvent(es[i], mo) == nil {
			t.Errorf("the M1 rule took %s", es[i].EventType)
		}
	}
	// An M1-version event carrying a v1 payload field is refused by M1.
	m1 := es[1]
	m1.Version = contracts.Version
	if reg.ValidateEvent(m1, nil) == nil {
		t.Error("M1 took delegation_id")
	}
}
