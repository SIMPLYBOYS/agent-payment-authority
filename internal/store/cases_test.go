package store

import (
	"encoding/json"
	"maps"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"testing"

	"github.com/SIMPLYBOYS/agent-payment-authority/internal/contracts"
	"github.com/SIMPLYBOYS/agent-payment-authority/internal/m1evidence"
)

// The 16 accepted integration cases run against the Store. Expected values
// come from cases.json, written and accepted before the Store existed. This
// is store-level evidence; the end-to-end run with the real CLI is B1-06.
//
// How a case's outcome is read from the Store:
//   - policy: the purchase's original decision.
//   - advance-clock: the purchase after ExpireDue at the advanced time
//     (expired reads as result "denied", reason APPROVAL_TIMEOUT).
//   - cancel / cli-timeout: the task status and the lifecycle event's code,
//     with the purchase's approval state when there is a purchase.
//   - restart-and-new-task: the new task's wallet after a restart.
//
// Each case's stored tasks are then exported as stored (internal/m1evidence)
// and recomputed against the case: digests, bindings, the event chain and
// the outcome derived from the recorded events. With APA_M1_RUN_DIR set
// (make m1-fixture), the export is written to <dir>/<case_id>.json.
func TestAcceptedIntegrationCases(t *testing.T) {
	var m contracts.CaseManifest
	readFixture(t, "testdata/contracts/cases.json", &m)
	if err := m.RequireAccepted(); err != nil {
		t.Fatal(err)
	}
	ran := 0
	for _, c := range m.Cases {
		if c.Layer != "integration" {
			continue
		}
		ran++
		t.Run(c.CaseID, func(t *testing.T) {
			e := newEnv(t)
			params := maps.Clone(m.Defaults)
			maps.Copy(params, c.Parameters)
			setup, purchase := caseInputs(t, e, params)
			e.offers = OfferBook{}
			e.offer(purchase)
			s := e.open()
			if c.Operation != "policy" {
				setup.ApprovalDeadlineSeconds = 30
			}
			if dl, ok := params["approval_deadline_seconds"]; ok {
				n, _ := strconv.ParseUint(dl, 10, 64)
				setup.ApprovalDeadlineSeconds = n
			}
			if err := s.StartTask(setup); err != nil {
				t.Fatal(err)
			}

			var result, reason, approval string
			switch c.Operation {
			case "policy":
				d := submit(t, s, sessA, purchase).Purchase.Decision
				result, reason, approval = d.PolicyVerdict, d.Reason, d.ApprovalStatus
			case "advance-clock":
				submit(t, s, sessA, purchase)
				n, _ := strconv.ParseUint(params["advance_seconds"], 10, 64)
				e.clock.Add(n)
				if err := s.ExpireDue(); err != nil {
					t.Fatal(err)
				}
				result, reason, approval = purchaseOutcome(t, s, purchase)
			case "cancel", "cli-timeout":
				hasPurchase := params["before"] == "pending" || params["phase"] == "pending"
				if hasPurchase {
					submit(t, s, sessA, purchase)
				}
				var err error
				if c.Operation == "cancel" {
					err = s.Cancel("task-a")
				} else {
					err = s.CLITimeout("task-a")
				}
				if err != nil {
					t.Fatal(err)
				}
				v := task(t, s, sessA)
				events, _ := s.Events(sessA)
				result, reason, approval = v.Status, events[len(events)-1].Payload.Code, "not_requested"
				if hasPurchase {
					_, _, approval = purchaseOutcome(t, s, purchase)
				}
			case "restart-and-new-task":
				if params["consumer"] != "consumer-a" {
					t.Fatalf("case names consumer %s; this mapping covers consumer-a", params["consumer"])
				}
				s.Close()
				s = e.open()
				next := e.setup("task-a-next")
				if err := s.StartTask(next); err != nil {
					t.Fatal(err)
				}
				var w1, w2 string
				db := rawDB(t, e.path)
				db.QueryRow(`SELECT wallet_id || payer FROM tasks WHERE task_id = 'task-a'`).Scan(&w1)
				db.QueryRow(`SELECT wallet_id || payer FROM tasks WHERE task_id = 'task-a-next'`).Scan(&w2)
				if w1 == "" || w1 != w2 {
					t.Fatalf("wallets %q and %q", w1, w2)
				}
				result, reason, approval = "same-wallet", "PERSISTED_BINDING", "not_requested"
			default:
				t.Fatalf("operation %s has no mapping", c.Operation)
			}
			x := c.Expected
			if result != x.Result || reason != x.Reason || approval != x.ApprovalStatus {
				t.Fatalf("got %s/%s/%s, want %s/%s/%s", result, reason, approval, x.Result, x.Reason, x.ApprovalStatus)
			}
			if x.PaymentStatus != "unavailable" || x.BudgetDeducted != "0" || count(t, e.path, "tasks", "budget_remaining != ?", setupBudget(setup)) != 0 {
				t.Fatal("budget changed or payment expected")
			}
			for _, f := range x.ForbiddenEvents {
				for _, sess := range []contracts.Session{sessA, sessANext} {
					if types, err := s.Events(sess); err == nil && slices.ContainsFunc(types, func(ev contracts.AuditEvent) bool { return ev.EventType == f }) {
						t.Fatalf("forbidden event %s recorded", f)
					}
				}
			}
			s.Close()
			tasks := []string{"task-a"}
			if c.Operation == "restart-and-new-task" {
				tasks = append(tasks, "task-a-next")
			}
			exported, err := m1evidence.Export(e.path, tasks...)
			if err != nil {
				t.Fatal(err)
			}
			if _, problems := m1evidence.CheckIntegration(c, m.Defaults, exported); len(problems) > 0 {
				t.Fatalf("the exported evidence does not show the case: %v", problems)
			}
			if dir := os.Getenv("APA_M1_RUN_DIR"); dir != "" {
				body, _ := json.MarshalIndent(map[string]any{"case_id": c.CaseID, "layer": "store_integration",
					"harness": "internal/store TestAcceptedIntegrationCases", "operation": c.Operation, "parameters": params,
					"test_observed": map[string]string{"result": result, "reason": reason, "approval_status": approval}, "tasks": exported}, "", "  ")
				if err := os.MkdirAll(dir, 0o700); err != nil {
					t.Fatal(err)
				}
				if err := os.WriteFile(filepath.Join(dir, c.CaseID+".json"), append(body, '\n'), 0o600); err != nil {
					t.Fatal(err)
				}
			}
			e.open() // everything recorded passes recovery verification
		})
	}
	if ran != 16 {
		t.Fatalf("ran %d integration cases, the accepted manifest has 16", ran)
	}
}

func setupBudget(s TaskSetup) string {
	if s.BudgetInitial != "" {
		return string(s.BudgetInitial)
	}
	return string(s.Profile.TaskCap)
}

// caseInputs maps defaults plus overrides to trusted task settings and the
// trusted quote, like the node 1 test; an unknown parameter fails the test.
func caseInputs(t *testing.T, e *env, params map[string]string) (TaskSetup, contracts.PurchaseOrder) {
	t.Helper()
	setup := e.setup("task-a")
	p := *setup.Profile
	setup.Profile = &p
	purchase := e.base
	for k, v := range params {
		switch k {
		case "remaining":
			setup.BudgetInitial = contracts.Amount(v)
		case "allow_max":
			p.AllowMax = contracts.Amount(v)
		case "merchant":
			p.Merchants = map[string][]string{"allowed": {e.reg.MerchantPayTo}, "not_allowed": {}}[v]
		case "credential":
			setup.Credential.ValidUntil = map[string]uint64{"valid": t0 + 86400, "expired": t0}[v]
		case "required_config":
			if v == "missing" {
				setup.Profile, setup.ProfileAbsent = nil, true
			}
		case "mode":
			p.Mode = v
		case "clock":
			n, err := strconv.ParseUint(v, 10, 64)
			if err != nil || n != t0 {
				t.Fatalf("clock %s: the purchase fixture's validity window is built around %d", v, t0)
			}
			e.clock.Set(n)
		case "asset":
			if v == "unlisted" {
				purchase = editQuote(t, purchase, "asset", untrustedAsset)
			} else {
				setup.AssetID = v
			}
		case "amount":
			purchase = editQuote(t, purchase, "amount", v)
		case "before", "phase", "advance_seconds", "approval_deadline_seconds", "consumer":
			// operation parameters, applied by the caller
		default:
			t.Fatalf("parameter %s has no mapping; add one before trusting this test", k)
		}
	}
	return setup, purchase
}

// purchaseOutcome reads a purchase as an accepted case states it.
func purchaseOutcome(t *testing.T, s *Store, p contracts.PurchaseOrder) (result, reason, approval string) {
	t.Helper()
	v := purchaseOf(t, task(t, s, sessA), p.Intent.PurchaseOrderID)
	switch v.ApprovalClosed {
	case "expired":
		return "denied", "APPROVAL_TIMEOUT", "expired"
	case "withdrawn":
		return "cancelled", "HOST_CANCELLED", "withdrawn"
	}
	if v.Decision.PolicyVerdict == "Ask" {
		return "pending", v.Decision.Reason, v.Decision.ApprovalStatus
	}
	return v.Decision.PolicyVerdict, v.Decision.Reason, v.Decision.ApprovalStatus
}
