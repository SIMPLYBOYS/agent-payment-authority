package store

import (
	"encoding/json"
	"maps"
	"slices"
	"strings"
	"testing"

	"github.com/SIMPLYBOYS/agent-payment-authority/internal/contracts"
	"github.com/SIMPLYBOYS/agent-payment-authority/internal/m1evidence"
)

// The M1-E1 recomputation must refuse an export that does not hold what the
// Store recorded: each change below, made to a real Ask case's export, is
// reported, even where the test's own log would still say PASS.
func TestM1EvidenceRefusesAlteredExports(t *testing.T) {
	var m contracts.CaseManifest
	readFixture(t, "testdata/contracts/cases.json", &m)
	byID := func(id string) contracts.AcceptanceCase {
		return m.Cases[slices.IndexFunc(m.Cases, func(c contracts.AcceptanceCase) bool { return c.CaseID == id })]
	}
	ask := byID("ask")
	e := newEnv(t)
	setup, purchase := caseInputs(t, e, maps.Clone(m.Defaults))
	e.offers = OfferBook{}
	e.offer(purchase)
	s := e.open()
	if err := s.StartTask(setup); err != nil {
		t.Fatal(err)
	}
	submit(t, s, sessA, purchase)
	s.Close()
	exported, err := m1evidence.Export(e.path, "task-a")
	if err != nil {
		t.Fatal(err)
	}
	if _, problems := m1evidence.CheckIntegration(ask, m.Defaults, exported); len(problems) > 0 {
		t.Fatalf("the unaltered export: %v", problems)
	}
	po := purchase.Intent.PurchaseOrderID
	edit := func(body string, f func(map[string]any)) string {
		var v map[string]any
		if err := json.Unmarshal([]byte(body), &v); err != nil {
			t.Fatal(err)
		}
		f(v)
		b, _ := json.Marshal(v)
		return string(b)
	}
	intent := func(f func(map[string]any)) func(*m1evidence.Task) {
		return func(x *m1evidence.Task) {
			p := x.Purchases[po]
			p.Body = edit(p.Body, func(v map[string]any) { f(v["intent"].(map[string]any)) })
			x.Purchases[po] = p
		}
	}
	// lastScoped edits the last event's scope; rehash also stores the edited
	// event's own new hash, as someone recomputing it would.
	event := func(rehash bool, f func(map[string]any)) func(*m1evidence.Task) {
		return func(x *m1evidence.Task) {
			n := len(x.Events) - 1
			ev := x.Events[n]
			ev.Body = edit(ev.Body, f)
			if rehash {
				var a contracts.AuditEvent
				json.Unmarshal([]byte(ev.Body), &a)
				ev.Hash, _ = contracts.EventHash(a)
			}
			x.Events = append(slices.Clone(x.Events[:n]), ev)
		}
	}
	for name, change := range map[string]func(*m1evidence.Task){
		"a canonical field missing": intent(func(i map[string]any) { delete(i, "eip3009_nonce") }),
		"the amount changed":        intent(func(i map[string]any) { i["amount"] = "400000" }),
		"the payee changed":         intent(func(i map[string]any) { i["pay_to"] = "0x5555555555555555555555555555555555555555" }),
		"the event scope changed": event(false, func(v map[string]any) {
			v["scope"].(map[string]any)["wallet_id"] = "wallet-b"
		}),
		"the event scope changed and rehashed": event(true, func(v map[string]any) {
			v["scope"].(map[string]any)["consumer_id"] = "consumer-b"
		}),
		"the decision changed and rehashed": event(true, func(v map[string]any) {
			v["payload"].(map[string]any)["decision"].(map[string]any)["reason"] = "INSUFFICIENT_BUDGET"
		}),
		"the last event missing":     func(x *m1evidence.Task) { x.Events = x.Events[:len(x.Events)-1] },
		"every event missing":        func(x *m1evidence.Task) { x.Events = nil },
		"the registry snapshot gone": func(x *m1evidence.Task) { delete(x.Snapshots, "registry") },
		"the payer changed":          func(x *m1evidence.Task) { x.Payer = "0x2222222222222222222222222222222222222222" },
		"the budget deducted":        func(x *m1evidence.Task) { x.BudgetRemaining = "4500000" },
		// Each of these is caught by one check only.
		"the terms snapshot edited": func(x *m1evidence.Task) {
			tr := x.Snapshots["terms"]
			tr.Body = edit(tr.Body, func(v map[string]any) { v["approval_deadline_seconds"] = 31 })
			x.Snapshots["terms"] = tr
		},
		"the stored intent digest changed": func(x *m1evidence.Task) {
			p := x.Purchases[po]
			p.Hash = "sha256:" + strings.Repeat("0", 64)
			x.Purchases[po] = p
		},
		// Everything recomputed and relinked; only the intent no longer
		// matches its quote's requirement.
		"a re-forged purchase": func(x *m1evidence.Task) {
			p := x.Purchases[po]
			var order contracts.PurchaseOrder
			json.Unmarshal([]byte(p.Body), &order)
			order.Intent.Amount = "400000"
			h, _ := order.Intent.Hash()
			b, _ := json.Marshal(order)
			x.Purchases[po] = m1evidence.Stored{Body: string(b), Hash: h}
			prev, _ := contracts.GenesisHash(x.TaskID)
			var events []m1evidence.Stored
			for _, s := range x.Events {
				var e contracts.AuditEvent
				json.Unmarshal([]byte(s.Body), &e)
				if e.Scope.PurchaseOrderID == po {
					e.Scope.IntentHash = h
				}
				if e.Payload.Decision != nil {
					e.Payload.Decision.IntentHash = h
				}
				e.PrevEventHash = prev
				prev, _ = contracts.EventHash(e)
				b, _ := json.Marshal(e)
				events = append(events, m1evidence.Stored{Body: string(b), Hash: prev})
			}
			x.Events = events
		},
		"the last event's time changed": event(false, func(v map[string]any) { v["observed_at"] = v["observed_at"].(float64) + 1 }),
		"the first event missing":       func(x *m1evidence.Task) { x.Events = x.Events[1:] },
		"the terms changed": func(x *m1evidence.Task) {
			tr := x.Snapshots["terms"]
			tr.Body = edit(tr.Body, func(v map[string]any) { v["budget_initial"] = "4500000" })
			x.Snapshots["terms"] = tr
		},
	} {
		t.Run(name, func(t *testing.T) {
			x := exported[0]
			x.Snapshots, x.Purchases = maps.Clone(x.Snapshots), maps.Clone(x.Purchases)
			change(&x)
			if _, problems := m1evidence.CheckIntegration(ask, m.Defaults, []m1evidence.Task{x}); len(problems) == 0 {
				t.Fatal("accepted")
			}
		})
	}
	// The recorded outcome is Ask; another case's expected outcome is not met.
	if _, problems := m1evidence.CheckIntegration(byID("deny-budget"), m.Defaults, exported); len(problems) == 0 {
		t.Fatal("an Ask record accepted as deny-budget")
	}
	if _, err := m1evidence.Export(e.path, "task-b"); err == nil {
		t.Fatal("a task that was never started exported")
	}
}
