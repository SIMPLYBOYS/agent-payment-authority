package paycontract

import (
	"strings"
	"testing"

	"github.com/SIMPLYBOYS/agent-payment-authority/internal/contracts"
)

// P2's payment events (docs/commercial-p2-host.md 4): reserved only for an
// Allow (delegation) or an approved webauthn grant, and not once the task
// ended; then each step once and in order; an end ends it.
func TestPaymentEventOrder(t *testing.T) {
	_, o := base(t)
	sid := SessionID(o.Intent.TaskID, o.Intent.PurchaseOrderID)
	allow := &contracts.PolicyDecision{Version: o.Version, IntentHash: mustHash(t, o), PolicyVerdict: "Allow", ApprovalStatus: "not_requested",
		PaymentStatus: PaymentNotEnabled, Reason: "WITHIN_DELEGATION"}
	head := []step{{"TASK_STARTED", contracts.EventPayload{}, false}, {"DELEGATION_BOUND", contracts.EventPayload{DelegationID: "dlg-1"}, false},
		{"PURCHASE_PROPOSED", contracts.EventPayload{}, true}}
	allowed := append(append([]step{}, head...), step{"POLICY_DECIDED", contracts.EventPayload{Decision: allow}, true})
	ask := append(append([]step{}, head...), step{"POLICY_DECIDED", contracts.EventPayload{Decision: decision(t, o, "pending", "HUMAN_APPROVAL_REQUIRED")}, true},
		step{"APPROVAL_SESSION_OPENED", contracts.EventPayload{ApprovalSessionID: sid}, true})
	approvedBy := func(m string) []step {
		reason := map[string]string{"webauthn": "WEBAUTHN_APPROVAL", "simulated": "SIMULATED_APPROVAL"}[m]
		return append(append([]step{}, ask...),
			step{"APPROVED", contracts.EventPayload{Decision: decision(t, o, "approved", reason), ApprovalSessionID: sid, AuthorizationMethod: m}, true},
			step{"GRANT_ISSUED", contracts.EventPayload{ApprovalSessionID: sid, GrantID: GrantID(sid), AuthorizationMethod: m}, true})
	}
	pay := func(typ string, r *contracts.PaymentRecord, code string) step {
		return step{typ, contracts.EventPayload{Payment: r, Code: code}, true}
	}
	reserved := func(m string) step { return pay("BUDGET_RESERVED", &contracts.PaymentRecord{Method: m}, "") }
	signed := func(m string) step { return pay("PAYMENT_SIGNED", &contracts.PaymentRecord{Method: m}, "") }
	submitted, settled := pay("PAYMENT_SUBMITTED", nil, ""), pay("PAYMENT_SETTLED", &contracts.PaymentRecord{}, "")
	delivered, receipt := pay("RESOURCE_DELIVERED", &contracts.PaymentRecord{}, ""), pay("RECEIPT_ISSUED", &contracts.PaymentRecord{}, "")
	notSettled, review := pay("PAYMENT_NOT_SETTLED", nil, "RESERVE_FAILED"), pay("PAYMENT_NEEDS_REVIEW", nil, "RESERVED_NOT_PAID")
	deliveryFailed := pay("PAYMENT_NEEDS_REVIEW", nil, "DELIVERY_FAILED")
	cancelled := step{"TASK_CANCELLED", contracts.EventPayload{Code: "HOST_CANCELLED"}, false}
	then := func(base []step, s ...step) []step { return append(append([]step{}, base...), s...) }
	line := []step{reserved("delegation"), signed("delegation"), submitted, settled, delivered, receipt}
	for name, c := range map[string]struct {
		steps []step
		ok    bool
	}{
		"an Allow paid":                        {then(allowed, line...), true},
		"an Ask paid under webauthn":           {then(approvedBy("webauthn"), reserved("webauthn"), signed("webauthn"), submitted, settled, delivered, receipt), true},
		"never reserved":                       {then(allowed, notSettled), true},
		"reserved, not paid":                   {then(allowed, reserved("delegation"), signed("delegation"), review), true},
		"paid, not delivered":                  {then(allowed, line[:4]...), true},
		"paid, delivery failed":                {then(allowed, append(line[:4:4], deliveryFailed)...), true},
		"in flight when the task is cancelled": {then(allowed, reserved("delegation"), cancelled, signed("delegation"), submitted), true},
		"an open Ask reserved":                 {then(ask, reserved("webauthn")), false},
		"a simulated grant reserved":           {then(approvedBy("simulated"), reserved("simulated")), false},
		"an Allow reserved as webauthn":        {then(allowed, reserved("webauthn")), false},
		"an Ask reserved as delegation":        {then(approvedBy("webauthn"), reserved("delegation")), false},
		"reserved after the task ended":        {then(allowed, cancelled, reserved("delegation")), false},
		"reserved twice":                       {then(allowed, reserved("delegation"), reserved("delegation")), false},
		"signed before reserved":               {then(allowed, signed("delegation")), false},
		"settled before submitted":             {then(allowed, reserved("delegation"), signed("delegation"), settled), false},
		"delivered before settled":             {then(allowed, reserved("delegation"), signed("delegation"), submitted, delivered), false},
		"not settled after a reservation":      {then(allowed, reserved("delegation"), notSettled), false},
		"needs review before a reservation":    {then(allowed, review), false},
		"delivery failed before settled":       {then(allowed, reserved("delegation"), deliveryFailed), false},
		"anything after the receipt":           {then(allowed, append(line, deliveryFailed)...), false},
		"anything after an end":                {then(allowed, notSettled, reserved("delegation")), false},
	} {
		if err := ValidateEventOrder(chainOf(t, o, c.steps...)); (err == nil) != c.ok {
			t.Errorf("%s: %v", name, err)
		}
	}
}

// Each payment event's payload: its record fields, all and no others, its code.
func TestPaymentEventShape(t *testing.T) {
	_, o := base(t)
	var cfg contracts.RegistryConfig
	fixture(t, "testdata/contracts/registry.json", &cfg)
	reg, err := contracts.NewRegistry(cfg)
	if err != nil {
		t.Fatal(err)
	}
	tx, sum := "0x"+strings.Repeat("a", 64), strings.Repeat("b", 64)
	amount := o.PaymentBinding.Amount
	r := func(rec contracts.PaymentRecord) *contracts.PaymentRecord { return &rec }
	for _, c := range []struct {
		name, typ string
		p         contracts.EventPayload
		ok        bool
	}{
		{"reserved", "BUDGET_RESERVED", contracts.EventPayload{Payment: r(contracts.PaymentRecord{Method: "delegation", Amount: amount, TxHash: tx, Block: 3})}, true},
		{"reserved, another amount", "BUDGET_RESERVED", contracts.EventPayload{Payment: r(contracts.PaymentRecord{Method: "delegation", Amount: amount + "0", TxHash: tx, Block: 3})}, false},
		{"reserved, no block", "BUDGET_RESERVED", contracts.EventPayload{Payment: r(contracts.PaymentRecord{Method: "delegation", Amount: amount, TxHash: tx})}, false},
		{"reserved, a malformed hash", "BUDGET_RESERVED", contracts.EventPayload{Payment: r(contracts.PaymentRecord{Method: "delegation", Amount: amount, TxHash: "0x12", Block: 3})}, false},
		{"reserved, simulated", "BUDGET_RESERVED", contracts.EventPayload{Payment: r(contracts.PaymentRecord{Method: "simulated", Amount: amount, TxHash: tx, Block: 3})}, false},
		{"reserved with a decision", "BUDGET_RESERVED", contracts.EventPayload{Decision: &contracts.PolicyDecision{}, Payment: r(contracts.PaymentRecord{Method: "delegation", Amount: amount, TxHash: tx, Block: 3})}, false},
		{"signed", "PAYMENT_SIGNED", contracts.EventPayload{Payment: r(contracts.PaymentRecord{Method: "webauthn"})}, true},
		{"signed with a hash", "PAYMENT_SIGNED", contracts.EventPayload{Payment: r(contracts.PaymentRecord{Method: "webauthn", TxHash: tx})}, false},
		{"submitted", "PAYMENT_SUBMITTED", contracts.EventPayload{}, true},
		{"submitted with a record", "PAYMENT_SUBMITTED", contracts.EventPayload{Payment: r(contracts.PaymentRecord{TxHash: tx, Block: 1})}, false},
		{"settled", "PAYMENT_SETTLED", contracts.EventPayload{Payment: r(contracts.PaymentRecord{TxHash: tx, Block: 9})}, true},
		{"settled with content", "PAYMENT_SETTLED", contracts.EventPayload{Payment: r(contracts.PaymentRecord{TxHash: tx, Block: 9, ContentSHA256: sum})}, false},
		{"not settled", "PAYMENT_NOT_SETTLED", contracts.EventPayload{Code: "NOT_PAID_EXPIRED"}, true},
		{"not settled, a review code", "PAYMENT_NOT_SETTLED", contracts.EventPayload{Code: "RESERVED_NOT_PAID"}, false},
		{"needs review", "PAYMENT_NEEDS_REVIEW", contracts.EventPayload{Code: "VERIFICATION_FAILED"}, true},
		{"needs review, no code", "PAYMENT_NEEDS_REVIEW", contracts.EventPayload{}, false},
		{"delivered", "RESOURCE_DELIVERED", contracts.EventPayload{Payment: r(contracts.PaymentRecord{ContentSHA256: sum})}, true},
		{"delivered, a malformed hash", "RESOURCE_DELIVERED", contracts.EventPayload{Payment: r(contracts.PaymentRecord{ContentSHA256: "xyz"})}, false},
		{"receipt", "RECEIPT_ISSUED", contracts.EventPayload{Payment: r(contracts.PaymentRecord{ContentSHA256: sum, ReceiptID: "rcpt-1"})}, true},
		{"receipt, no ID", "RECEIPT_ISSUED", contracts.EventPayload{Payment: r(contracts.PaymentRecord{ContentSHA256: sum})}, false},
	} {
		es := chainOf(t, o, step{"TASK_STARTED", contracts.EventPayload{}, false}, step{"DELEGATION_BOUND", contracts.EventPayload{DelegationID: "dlg-1"}, false}, step{c.typ, c.p, true})
		if err := ValidateEvent(reg, es[2], &o); (err == nil) != c.ok {
			t.Errorf("%s: %v", c.name, err)
		}
	}
}

// M1 refuses a payment record on any of its events.
func TestM1RefusesPaymentRecord(t *testing.T) {
	m, _ := base(t)
	var cfg contracts.RegistryConfig
	fixture(t, "testdata/contracts/registry.json", &cfg)
	reg, err := contracts.NewRegistry(cfg)
	if err != nil {
		t.Fatal(err)
	}
	e := contracts.AuditEvent{Version: contracts.Version, EventID: "evt-0000000001", EventType: "TASK_STARTED",
		Scope:    &contracts.EventScope{ConsumerID: m.Intent.ConsumerID, AgentID: m.Intent.AgentID, WalletID: m.Intent.WalletID, TaskID: m.Intent.TaskID},
		Sequence: 1, Payload: contracts.EventPayload{Payment: &contracts.PaymentRecord{Method: "delegation"}}, EvidenceMode: "fixture", Source: "host", ObservedAt: 1790726450}
	e.PrevEventHash, _ = contracts.GenesisHash(m.Intent.TaskID)
	if err := reg.ValidateEvent(e, nil); err == nil {
		t.Fatal("M1 accepted a payment record")
	}
}
