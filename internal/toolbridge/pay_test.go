package toolbridge

import (
	"fmt"
	"strings"
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

// P2: once a payment exists the model sees its status and a fixed note,
// never its transactions or the content; an Allow being paid says paying.
func TestPaymentStatusProjected(t *testing.T) {
	for _, st := range []string{"paying", "confirming", "paid", "not_paid", "needs_review"} {
		v := store.PurchaseView{Payment: &store.PaymentView{Status: st, SettleTx: "0x" + strings.Repeat("2", 64), ContentSHA256: strings.Repeat("c", 64)}}
		out := projection("kr-market", v, true)
		if out["payment"] != st || out["note"] == "" {
			t.Errorf("%s: %v", st, out)
		}
		for _, leak := range []string{strings.Repeat("2", 64), strings.Repeat("c", 64)} {
			if strings.Contains(fmt.Sprint(out), leak) {
				t.Errorf("%s: the model sees %s", st, leak)
			}
		}
	}
	failed := projection("kr-market", store.PurchaseView{Payment: &store.PaymentView{Status: "paid", Code: "DELIVERY_FAILED"}}, true)
	if failed["payment"] != "paid" || !strings.Contains(failed["note"].(string), "did not deliver") {
		t.Errorf("paid, not delivered: %v", failed)
	}
	e := newEnv(t)
	p := e.profile
	p.AllowMax = "500000" // kr-market is an Allow here
	d, _, err := e.st.ConfirmDelegation(store.DelegationRequest{RequestID: "dlg-req-0000000000000001", ConsumerID: "consumer-a", Scenario: "pay", Purpose: "a brief",
		AssetID: p.AssetID, Merchants: p.Merchants, TaskBudget: "5000000", AllowMax: "500000", ApprovalDeadlineSeconds: 30, ValidSeconds: 600,
		ConfirmedBy: "simulated:consumer-a:host-test"})
	if err != nil {
		t.Fatal(err)
	}
	cred := policy.Credential{ConsumerID: "consumer-a", ValidUntil: t0 + 86400, EvidenceMode: "fixture"}
	if err := e.st.StartTask(store.TaskSetup{TaskID: "task-a", Profile: &p, Credential: &cred, ContractVersion: paycontract.Version, DelegationID: d.DelegationID}); err != nil {
		t.Fatal(err)
	}
	b := &Binding{Session: sessA, WalletID: "wallet-a", From: "0x1111111111111111111111111111111111111111"}
	b.Arm()
	if out, rec := e.call(b, "request_purchase", "kr-market"); rec.Outcome != "Allow" || out["payment"] != "none" {
		t.Fatalf("before the payment: %v %v", out, rec)
	}
	ps := e.purchases(sessA)
	if _, _, err := e.st.StartPayment(store.PaymentStart{ConsumerID: "consumer-a", TaskID: "task-a", PurchaseOrderID: ps[0].PurchaseOrderID, DelegationRef: "dlg-a@1"}); err != nil {
		t.Fatal(err)
	}
	if out, _ := e.call(b, "request_purchase", "kr-market"); out["payment"] != "paying" || out["already_requested"] != true {
		t.Fatalf("being paid: %v", out)
	}
}
