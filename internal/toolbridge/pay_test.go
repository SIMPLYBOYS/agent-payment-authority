package toolbridge

import (
	"testing"

	"github.com/SIMPLYBOYS/agent-payment-authority/internal/paycontract"
	"github.com/SIMPLYBOYS/agent-payment-authority/internal/policy"
	"github.com/SIMPLYBOYS/agent-payment-authority/internal/store"
)

// An apa.pay.v1 task (its version stored with it, not named by the model)
// gets an apa.pay.v1 purchase from the same three tools; the model sees the
// same whitelisted keys, payment none.
func TestPayTaskGetsV1Purchase(t *testing.T) {
	e := newEnv(t)
	d, _, err := e.st.ConfirmDelegation(store.DelegationRequest{RequestID: "dlg-req-0000000000000001", ConsumerID: "consumer-a", Scenario: "pay", Purpose: "a brief",
		AssetID: e.profile.AssetID, Merchants: e.profile.Merchants, TaskBudget: "5000000", AllowMax: "100000", ApprovalDeadlineSeconds: 30, ValidSeconds: 600,
		ConfirmedBy: "simulated:consumer-a:host-test"})
	if err != nil {
		t.Fatal(err)
	}
	p := e.profile
	cred := policy.Credential{ConsumerID: "consumer-a", ValidUntil: t0 + 86400, EvidenceMode: "fixture"}
	if err := e.st.StartTask(store.TaskSetup{TaskID: "task-a", Profile: &p, Credential: &cred, ContractVersion: paycontract.Version, DelegationID: d.DelegationID}); err != nil {
		t.Fatal(err)
	}
	b := &Binding{Session: sessA, WalletID: "wallet-a", From: "0x1111111111111111111111111111111111111111"}
	b.Arm()
	out, rec := e.call(b, "request_purchase", "kr-market")
	if rec.Outcome != "Ask" || out["decision"] != "Ask" || out["approval"] != "pending" || out["payment"] != "none" {
		t.Fatalf("request_purchase: %v %v", out, rec)
	}
	for _, k := range []string{"intent_hash", "resource_url", "pay_to", "approval_session_id", "grant"} {
		if _, ok := out[k]; ok {
			t.Errorf("the model sees %s", k)
		}
	}
	ps := e.purchases(sessA)
	if len(ps) != 1 || ps[0].Decision.Version != paycontract.Version || ps[0].Decision.PaymentStatus != "not_enabled" || ps[0].Approval == nil {
		t.Fatalf("stored: %+v", ps)
	}
	// Called again: the stored purchase, nothing new.
	if again, _ := e.call(b, "request_purchase", "kr-market"); again["already_requested"] != true || len(e.purchases(sessA)) != 1 {
		t.Fatalf("again: %v", again)
	}
}
