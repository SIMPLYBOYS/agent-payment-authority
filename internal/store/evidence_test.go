package store

import (
	"fmt"
	"testing"

	"github.com/SIMPLYBOYS/agent-payment-authority/internal/contracts"
	"github.com/SIMPLYBOYS/agent-payment-authority/internal/paycontract"
)

// E1-a (docs/commercial-e1a.md 4.1): a pay task's events carry the evidence
// mode and source of its run conditions, kept across a restart; a task
// without them stays fixture/host; an M1 task takes none.
func TestRunConditionsEvidence(t *testing.T) {
	e := newEnv(t)
	s := e.open()
	run := &paycontract.RunConditions{Agent: "live", Chain: "base_sepolia", Authenticator: "device"}
	start := func(task, consumer string, r *paycontract.RunConditions) (string, string) {
		setup := e.setup(task)
		setup.ContractVersion, setup.DelegationID, setup.Run = paycontract.Version, e.delegation(s, consumer, "dlg-req-"+task+"-000000000", 30), r
		if err := s.StartTask(setup); err != nil {
			t.Fatalf("StartTask %s: %v", task, err)
		}
		o := v1Of(t, forTask(t, e.base, task))
		res := submitV1(t, s, contracts.Session{ConsumerID: consumer, AgentID: o.Intent.AgentID, TaskID: task}, o)
		return res.Purchase.Approval.SessionID, res.Purchase.IntentHash
	}
	id, intent := start("task-a", "consumer-a", run)
	start("task-b", "consumer-b", nil)
	s.Close()
	s = e.open() // the run conditions come back from the task's terms
	if r, err := approve(s, id, intent, "approve-req-000000001", "consumer-a", "approve"); err != nil || r.Outcome != "approved" {
		t.Fatalf("approve: %+v %v", r, err)
	}
	labels := func(sess contracts.Session) string {
		events, err := s.Events(sess)
		if err != nil {
			t.Fatal(err)
		}
		var out []string
		for _, e := range events {
			out = append(out, e.EventType+"="+e.EvidenceMode+"/"+e.Source)
		}
		return fmt.Sprint(out)
	}
	want := "[TASK_STARTED=live/host DELEGATION_BOUND=fixture/simulated PURCHASE_PROPOSED=live/live_cli POLICY_DECIDED=live/host " +
		"APPROVAL_SESSION_OPENED=live/signer_webauthn APPROVED=fixture/simulated GRANT_ISSUED=fixture/simulated]"
	if got := labels(sessA); got != want {
		t.Errorf("with run conditions:\n got %s\nwant %s", got, want)
	}
	if got := labels(sessB); got != "[TASK_STARTED=fixture/host DELEGATION_BOUND=fixture/host PURCHASE_PROPOSED=fixture/host POLICY_DECIDED=fixture/host APPROVAL_SESSION_OPENED=fixture/host]" {
		t.Errorf("without run conditions: %s", got)
	}
	m1 := e.setup("task-a-next")
	m1.Run = run
	if err := s.StartTask(m1); err == nil {
		t.Error("an M1 task took run conditions")
	}
}
