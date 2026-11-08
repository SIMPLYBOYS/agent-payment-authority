package store

import (
	"fmt"
	"testing"

	"github.com/SIMPLYBOYS/agent-payment-authority/internal/contracts"
	"github.com/SIMPLYBOYS/agent-payment-authority/internal/paycontract"
	"github.com/SIMPLYBOYS/agent-payment-authority/internal/policy"
)

// docs/commercial-velocity.md (Z1 A, Z2 A): one consumer's purchases decided Allow or Ask within the window,
// across its tasks, are at most the profile's max; the next is Deny VELOCITY_LIMIT before any payment. Another
// consumer's do not count, a Deny counts for nothing, and once the window has passed the consumer buys again.
func TestVelocityLimit(t *testing.T) {
	e := newEnv(t)
	e.profile.Velocity = &policy.Velocity{Max: 2, WindowSeconds: 150} // short: every purchase within its quote's validity
	s := e.open()
	n := 200
	buy := func(task string) contracts.PolicyDecision {
		t.Helper()
		n++
		o := v1Of(t, variant(t, forTask(t, e.base, task), n))
		o.PaymentBinding.Nonce = fmt.Sprintf("0x%064x", n)
		sess := map[string]contracts.Session{"task-a": sessA, "task-a-next": sessANext, "task-b": sessB}[task]
		return submitV1(t, s, sess, o).Purchase.Decision
	}
	start := func(task, consumer, req string) {
		t.Helper()
		setup := e.setup(task)
		setup.ContractVersion, setup.DelegationID = paycontract.Version, e.delegation(s, consumer, req, 30)
		if err := s.StartTask(setup); err != nil {
			t.Fatalf("StartTask %s: %v", task, err)
		}
	}
	start("task-a", "consumer-a", "dlg-req-a-0000000000001")
	start("task-b", "consumer-b", "dlg-req-b-0000000000001")
	if d := buy("task-a"); d.PolicyVerdict != "Ask" {
		t.Fatalf("the 1st: %+v", d)
	}
	e.clock.Add(100)
	if d := buy("task-b"); d.PolicyVerdict != "Ask" { // another consumer's: not A's count
		t.Fatalf("B's: %+v", d)
	}
	start("task-a-next", "consumer-a", "dlg-req-a-0000000000002")
	if d := buy("task-a-next"); d.PolicyVerdict != "Ask" { // the 2nd, in another task of A
		t.Fatalf("the 2nd: %+v", d)
	}
	if d := buy("task-a"); d.PolicyVerdict != "Deny" || d.Reason != "VELOCITY_LIMIT" {
		t.Fatalf("the 3rd within the window: %+v", d)
	}
	// The Deny ended task-a and counts for nothing; B still buys.
	if d := buy("task-b"); d.PolicyVerdict != "Ask" {
		t.Fatalf("B's 2nd: %+v", d)
	}
	// The window counts from each decision: the 1st leaves it 150 s after it.
	e.clock.Add(51)
	if d := buy("task-a-next"); d.PolicyVerdict != "Ask" {
		t.Fatalf("after the 1st left the window: %+v", d)
	}
	s.Close()
	e.open() // the recovery check takes the VELOCITY_LIMIT Deny
}
