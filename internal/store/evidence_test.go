package store

import (
	"encoding/json"
	"fmt"
	"strings"
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

// E1-a review: a paid purchase's events carry the chain and the declared
// authenticator: WebAuthn approval, reservation, settlement and the shop's
// delivery, on a local anvil with a virtual authenticator and on Base
// Sepolia with a device.
func TestPaymentEvidence(t *testing.T) {
	for _, c := range []struct {
		run  paycontract.RunConditions
		want string
	}{
		{paycontract.RunConditions{Agent: "fake", Chain: "local_anvil", Authenticator: "virtual"},
			"APPROVED=fixture/virtual_authenticator GRANT_ISSUED=fixture/virtual_authenticator PAYMENT_REQUESTED=fixture/local_anvil BUDGET_RESERVED=fixture/local_anvil " +
				"PAYMENT_SIGNED=fixture/local_anvil PAYMENT_SUBMITTED=fixture/local_anvil PAYMENT_SETTLED=fixture/local_anvil RESOURCE_DELIVERED=fixture/host_merchant RECEIPT_ISSUED=fixture/host_merchant"},
		{paycontract.RunConditions{Agent: "live", Chain: "base_sepolia", Authenticator: "device"},
			"APPROVED=live/signer_webauthn GRANT_ISSUED=live/signer_webauthn PAYMENT_REQUESTED=live/base_sepolia BUDGET_RESERVED=live/base_sepolia " +
				"PAYMENT_SIGNED=live/base_sepolia PAYMENT_SUBMITTED=live/base_sepolia PAYMENT_SETTLED=live/base_sepolia RESOURCE_DELIVERED=live/host_merchant RECEIPT_ISSUED=live/host_merchant"},
	} {
		e := newEnv(t)
		e.run = &c.run
		s := e.open()
		po, intent := e.webauthnApproved(s)
		if _, _, err := s.StartPayment(PaymentStart{ConsumerID: "consumer-a", TaskID: "task-a", PurchaseOrderID: po}); err != nil {
			t.Fatal(err)
		}
		for _, step := range []func() (Payment, error){
			func() (Payment, error) { return s.PaymentReserved(intent, Reservation{Evidence: evidence, TxHash: tx1, Block: 7}) },
			func() (Payment, error) { return s.PaymentSigned(intent, auth) },
			func() (Payment, error) { return s.PaymentSubmitted(intent) },
			func() (Payment, error) { return s.PaymentSettled(intent, tx2, 9, json.RawMessage(`{"success":true}`)) },
			func() (Payment, error) { return s.PaymentDelivered(intent, content, 36) },
		} {
			if _, err := step(); err != nil {
				t.Fatal(err)
			}
		}
		events, err := s.Events(sessA)
		if err != nil {
			t.Fatal(err)
		}
		var got []string
		for _, ev := range events {
			if ev.EventType == "APPROVED" || ev.EventType == "GRANT_ISSUED" || ev.Payload.Payment != nil || strings.HasPrefix(ev.EventType, "PAYMENT_") || ev.EventType == "RESOURCE_DELIVERED" || ev.EventType == "RECEIPT_ISSUED" {
				got = append(got, ev.EventType+"="+ev.EvidenceMode+"/"+ev.Source)
			}
		}
		if strings.Join(got, " ") != c.want {
			t.Errorf("%+v:\n got %s\nwant %s", c.run, strings.Join(got, " "), c.want)
		}
		s.Close()
	}
}
