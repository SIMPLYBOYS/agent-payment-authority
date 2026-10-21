package verification

import (
	"slices"
	"testing"

	"github.com/SIMPLYBOYS/agent-payment-authority/internal/contracts"
	"github.com/SIMPLYBOYS/agent-payment-authority/internal/paycontract"
)

// The checker's copy of the E1a-D4 table is its own (e1aMode never asks
// paycontract.Evidence) and says what the Store's says, for every event
// type, method and run condition the Store writes under.
func TestE1aModeIsTheStoresTable(t *testing.T) {
	types := append(slices.Clone(paycontract.EventTypes), "CREDENTIAL_VERIFIED")
	for _, a := range []string{"live", "fake"} {
		for _, c := range []string{"base_sepolia", "local_anvil", "none"} {
			for _, au := range []string{"device", "virtual", "none"} {
				r := contracts.RunConditions{Agent: a, Chain: c, Authenticator: au}
				for _, typ := range types {
					for _, m := range []string{"", "simulated", "webauthn", "other"} {
						e := contracts.AuditEvent{EventType: typ, Payload: contracts.EventPayload{AuthorizationMethod: m}}
						mode, src, ok := paycontract.Evidence(&r, e)
						gm, gs := e1aMode(r, e)
						if ok && (gm != mode || gs != src) || !ok && gm != "" && (typ == "APPROVED" || typ == "GRANT_ISSUED") {
							t.Errorf("%s (%q) under %+v: checker %s/%s, Store %s/%s %v", typ, m, r, gm, gs, mode, src, ok)
						}
					}
				}
			}
		}
	}
}

// E7 across runs: a grant or a delegation is one consumer's.
func TestE1aApart(t *testing.T) {
	var cfg contracts.RegistryConfig
	if err := readTrustedJSON("../..", "testdata/contracts/registry.json", &cfg); err != nil {
		t.Fatal(err)
	}
	reg, err := contracts.NewRegistry(cfg)
	if err != nil {
		t.Fatal(err)
	}
	ev := func(task, consumer string, p contracts.EventPayload) contracts.AuditEvent {
		return contracts.AuditEvent{Scope: &contracts.EventScope{TaskID: task, ConsumerID: consumer, IntentHash: "sha256:" + task}, Payload: p}
	}
	runs := func(b contracts.EventPayload) map[string]*e1aRun {
		return map[string]*e1aRun{
			"task-a": {task: E1aTask{TaskID: "task-a"}, events: []contracts.AuditEvent{ev("task-a", "consumer-a", contracts.EventPayload{DelegationID: "dlg-a", GrantID: "g-a"})}},
			"task-b": {task: E1aTask{TaskID: "task-b"}, events: []contracts.AuditEvent{ev("task-b", "consumer-b", b)}},
		}
	}
	var f findings
	e1aApart(&f, runs(contracts.EventPayload{DelegationID: "dlg-b", GrantID: "g-b"}), reg)
	if len(f) != 0 {
		t.Fatalf("apart: %+v", f)
	}
	for _, b := range []contracts.EventPayload{{DelegationID: "dlg-a"}, {GrantID: "g-a"}} {
		var f findings
		e1aApart(&f, runs(b), reg)
		if f.verdicts(E1aClaims)["E7"] != Fail {
			t.Errorf("%+v shared with consumer-a: %+v", b, f)
		}
	}
}
