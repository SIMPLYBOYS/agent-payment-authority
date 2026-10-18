package store

// Tests of schema 7's payments (P2 Host, docs/commercial-p2-host.md 4): the
// steps and their events, replays, refusals before any write, failures,
// the budget, the view, the recovery check and the upgrade from 6.

import (
	"encoding/json"
	"errors"
	"strings"
	"testing"
)

var (
	tx1      = "0x" + strings.Repeat("1", 64)
	tx2      = "0x" + strings.Repeat("2", 64)
	content  = strings.Repeat("c", 64)
	evidence = json.RawMessage(`{"tx_hash":"0x` + strings.Repeat("1", 64) + `"}`)
	auth     = json.RawMessage(`{"from":"0x1111111111111111111111111111111111111111","signature":"0xab"}`)
)

func payStart(po string) PaymentStart {
	return PaymentStart{ConsumerID: "consumer-a", TaskID: "task-a", PurchaseOrderID: po, DelegationRef: "dlg-a@1"}
}

// webauthnApproved starts task-a and approves its Ask with a person's
// verified approval (hap-1); it returns the purchase and its intent.
func (e *env) webauthnApproved(s *Store) (string, string) {
	e.t.Helper()
	id, intent := e.payTask(s, "task-a", 60)
	begin(e.t, s, "consumer-a", id, "att-req-00000001", "approve")
	for _, u := range []AttemptUpdate{{From: []string{"requested"}, State: "challenged", ChallengeID: "chl-1"},
		{From: []string{"challenged"}, State: "verifying", VerifyRequestID: "ver-1"}} {
		u.ConsumerID, u.RequestID = "consumer-a", "att-req-00000001"
		if _, err := s.UpdateApprovalAttempt(u); err != nil {
			e.t.Fatal(err)
		}
	}
	if r, err := s.ResolveWebAuthn(wa("consumer-a", "att-req-00000001", "approve", "hap-1", intent)); err != nil || r.Outcome != "approved" {
		e.t.Fatalf("approve: %+v %v", r, err)
	}
	var po string
	if err := rawDB(e.t, e.path).QueryRow(`SELECT purchase_order_id FROM approval_sessions WHERE approval_session_id = ?`, id).Scan(&po); err != nil {
		e.t.Fatal(err)
	}
	return po, intent
}

func (e *env) budgetLeft() string {
	var b string
	if err := rawDB(e.t, e.path).QueryRow(`SELECT budget_remaining FROM tasks WHERE task_id = 'task-a'`).Scan(&b); err != nil {
		e.t.Fatal(err)
	}
	return b
}

func (e *env) paymentEvents(po string) []string {
	rows, err := rawDB(e.t, e.path).Query(`SELECT event_type FROM events WHERE task_id = 'task-a' AND purchase_order_id = ? ORDER BY sequence`, po)
	if err != nil {
		e.t.Fatal(err)
	}
	defer rows.Close()
	var out []string
	for rows.Next() {
		var t string
		rows.Scan(&t)
		if strings.HasPrefix(t, "BUDGET_") || strings.HasPrefix(t, "PAYMENT_") || t == "RESOURCE_DELIVERED" || t == "RECEIPT_ISSUED" {
			out = append(out, t)
		}
	}
	return out
}

func (e *env) view(s *Store, po string) *PaymentView {
	e.t.Helper()
	v, err := s.Task(sessA)
	if err != nil {
		e.t.Fatal(err)
	}
	for _, p := range v.Purchases {
		if p.PurchaseOrderID == po {
			return p.Payment
		}
	}
	e.t.Fatalf("no purchase %s", po)
	return nil
}

// An Allow paid end to end: each step once, its event, the budget, the
// receipt, the view; the same step again is a replay; every step survives
// a restart and the recovery check.
func TestPaymentAllowPaid(t *testing.T) {
	e := newEnv(t)
	s := e.open()
	po, intent := e.payAllowed(s)
	before := e.budgetLeft()
	if e.view(s, po) != nil {
		t.Fatal("a payment view before any payment")
	}
	p, replay, err := s.StartPayment(payStart(po))
	if err != nil || replay || p.State != "requested" || p.IntentHash != intent || p.Method != "delegation" || p.GrantRef != "dlg-a@1" {
		t.Fatalf("start %+v %v %v", p, replay, err)
	}
	if v := e.view(s, po); v == nil || v.Status != "paying" || v.State != "requested" {
		t.Fatalf("requested view %+v", v)
	}
	other := payStart(po)
	other.DelegationRef = "dlg-a@2"
	if _, _, err := s.StartPayment(other); Code(err) != "IDEMPOTENCY_CONFLICT" {
		t.Fatalf("started again under another delegation: %v", err)
	}
	if again, replay, err := s.StartPayment(payStart(po)); err != nil || !replay || again.IntentHash != intent {
		t.Fatalf("start again %+v %v", again, err)
	}
	steps := []struct {
		name string
		do   func() (Payment, error)
		want string
	}{
		{"reserved", func() (Payment, error) {
			return s.PaymentReserved(intent, Reservation{Evidence: evidence, TxHash: tx1, Block: 7})
		}, "reserved"},
		{"signed", func() (Payment, error) { return s.PaymentSigned(intent, auth) }, "signed"},
		{"submitted", func() (Payment, error) { return s.PaymentSubmitted(intent) }, "submitted"},
		{"unknown", func() (Payment, error) { return s.PaymentUnknown(intent) }, "unknown"},
		{"settled", func() (Payment, error) { return s.PaymentSettled(intent, tx2, 9, json.RawMessage(`{"success":true}`)) }, "settled"},
		{"delivered", func() (Payment, error) { return s.PaymentDelivered(intent, content, 36) }, "delivered"},
	}
	for _, st := range steps {
		for i := range 2 { // the second: a replay, after a restart
			if i == 1 {
				s.Close()
				s = e.open()
			}
			p, err := st.do()
			if err != nil || p.State != st.want {
				t.Fatalf("%s (%d): %+v %v", st.name, i, p, err)
			}
		}
	}
	if got := e.paymentEvents(po); strings.Join(got, ",") != "PAYMENT_REQUESTED,BUDGET_RESERVED,PAYMENT_SIGNED,PAYMENT_SUBMITTED,PAYMENT_SETTLED,RESOURCE_DELIVERED,RECEIPT_ISSUED" {
		t.Fatalf("events %v", got)
	}
	if left := e.budgetLeft(); left == before {
		t.Fatalf("budget %s not deducted", left)
	}
	v := e.view(s, po)
	if v == nil || v.Status != "paid" || v.State != "delivered" || v.ReserveTx != tx1 || v.SettleTx != tx2 || v.SettleBlock != 9 ||
		v.ContentSHA256 != content || v.ReceiptID != ReceiptID(intent) {
		t.Fatalf("view %+v", v)
	}
	if n := count(t, e.path, "receipts", "intent_hash = ? AND settle_tx = ? AND reserve_tx = ? AND content_size = 36", intent, tx2, tx1); n != 1 {
		t.Fatalf("receipt rows %d", n)
	}
	// The same step with other values is a conflict; a step backwards is refused.
	if _, err := s.PaymentSettled(intent, "0x"+strings.Repeat("3", 64), 9, nil); Code(err) != "INVALID_PAYMENT_TRANSITION" {
		t.Fatalf("settled again after delivery: %v", err)
	}
	if _, err := s.PaymentReserved(intent, Reservation{Evidence: evidence, TxHash: tx1, Block: 7}); Code(err) != "INVALID_PAYMENT_TRANSITION" {
		t.Fatalf("reserved again: %v", err)
	}
	if _, err := s.PaymentOf("consumer-b", "task-a", po); Code(err) != "UNKNOWN_PAYMENT" {
		t.Fatalf("B read A's payment: %v", err)
	}
	if _, err := s.PaymentDelivered(intent, strings.Repeat("d", 64), 36); Code(err) != "IDEMPOTENCY_CONFLICT" {
		t.Fatalf("delivered again with other content: %v", err)
	}
	if _, err := s.PaymentDelivered(intent, content, 37); Code(err) != "IDEMPOTENCY_CONFLICT" {
		t.Fatalf("delivered again with another size: %v", err)
	}
}

// A payment in flight when the task ends is carried through: requested
// before a cancellation, a CLI timeout or another purchase's Deny, its
// reservation (whose tx#1 may be on chain) is recorded after.
func TestPaymentInFlightWhenTheTaskEnds(t *testing.T) {
	for _, end := range []string{"cancel", "timeout", "deny"} {
		t.Run(end, func(t *testing.T) {
			e := newEnv(t)
			s := e.open()
			po, intent := e.payAllowed(s)
			if _, _, err := s.StartPayment(payStart(po)); err != nil {
				t.Fatal(err)
			}
			switch end {
			case "cancel":
				if err := s.Cancel("task-a"); err != nil {
					t.Fatal(err)
				}
			case "timeout":
				if err := s.CLITimeout("task-a"); err != nil {
					t.Fatal(err)
				}
			case "deny":
				o := v1Of(t, forTask(t, e.denied, "task-a"))
				o.PaymentBinding.Nonce = "0x" + strings.Repeat("9", 64)
				if r := submitV1(t, s, sessA, o); r.Purchase.Decision.PolicyVerdict != "Deny" {
					t.Fatalf("not a Deny: %+v", r.Purchase.Decision)
				}
			}
			if p, err := s.PaymentReserved(intent, Reservation{Evidence: evidence, TxHash: tx1, Block: 7}); err != nil || p.State != "reserved" {
				t.Fatalf("the reservation after the task ended: %+v %v", p, err)
			}
			if _, err := s.PaymentSigned(intent, auth); err != nil {
				t.Fatal(err)
			}
			s.Close()
			s = e.open()
			defer s.Close()
		})
	}
}

// Another purchase's Deny ends the task for new payments, as the order
// rules see it: an Allow decided before it is not started after it.
func TestPaymentNotStartedAfterADeny(t *testing.T) {
	e := newEnv(t)
	s := e.open()
	po, _ := e.payAllowed(s)
	o := v1Of(t, forTask(t, e.denied, "task-a"))
	o.PaymentBinding.Nonce = "0x" + strings.Repeat("9", 64)
	submitV1(t, s, sessA, o)
	if _, _, err := s.StartPayment(payStart(po)); Code(err) != "TASK_ALREADY_ENDED" {
		t.Fatalf("after a Deny: %v", err)
	}
}

// Two Allows decided before either is reserved never go over the budget
// together: the second is refused at its start.
func TestPaymentStartOverBudget(t *testing.T) {
	e := newEnv(t)
	s := e.open()
	d, _, err := s.ConfirmDelegation(DelegationRequest{RequestID: "dlg-req-small-00000000", ConsumerID: "consumer-a", Scenario: "pay", Purpose: "a brief",
		AssetID: e.profile.AssetID, Merchants: e.profile.Merchants, TaskBudget: "80000", AllowMax: "60000", ApprovalDeadlineSeconds: 30, ValidSeconds: 600,
		ConfirmedBy: "simulated:consumer-a:host-test"})
	if err != nil {
		t.Fatal(err)
	}
	setup := e.setup("task-a")
	setup.ContractVersion, setup.DelegationID = "apa.pay.v1", d.DelegationID
	if err := s.StartTask(setup); err != nil {
		t.Fatal(err)
	}
	var pos []string
	for i := range 2 {
		o := v1Of(t, forTask(t, e.allow, "task-a"))
		o.Intent.PurchaseOrderID, o.RequestID = o.Intent.PurchaseOrderID+string(rune('a'+i)), o.RequestID+string(rune('a'+i))
		o.PaymentBinding.Nonce = "0x" + strings.Repeat(string(rune('5'+i)), 64)
		if r := submitV1(t, s, sessA, o); r.Purchase.Decision.PolicyVerdict != "Allow" {
			t.Fatalf("purchase %d: %+v", i, r.Purchase.Decision)
		}
		pos = append(pos, o.Intent.PurchaseOrderID)
	}
	if _, _, err := s.StartPayment(payStart(pos[0])); err != nil {
		t.Fatal(err)
	}
	if _, _, err := s.StartPayment(payStart(pos[1])); Code(err) != "OVER_BUDGET" {
		t.Fatalf("the second over the budget left: %v", err)
	}
}

// A purchase past its payment window is not started.
func TestPaymentStartExpired(t *testing.T) {
	e := newEnv(t)
	s := e.open()
	po, _ := e.payAllowed(s)
	e.clock.Add(3600)
	if _, _, err := s.StartPayment(payStart(po)); Code(err) != "PURCHASE_EXPIRED" {
		t.Fatalf("past valid_before: %v", err)
	}
}

func TestPaymentStepConflict(t *testing.T) {
	e := newEnv(t)
	s := e.open()
	po, intent := e.payAllowed(s)
	s.StartPayment(payStart(po))
	if _, err := s.PaymentReserved(intent, Reservation{Evidence: evidence, TxHash: tx1, Block: 7}); err != nil {
		t.Fatal(err)
	}
	other := json.RawMessage(`{"tx_hash":"0x` + strings.Repeat("4", 64) + `"}`)
	if _, err := s.PaymentReserved(intent, Reservation{Evidence: other, TxHash: tx1, Block: 7}); Code(err) != "IDEMPOTENCY_CONFLICT" {
		t.Fatalf("reserved with other evidence: %v", err)
	}
	if _, err := s.PaymentSettled(intent, tx2, 9, nil); Code(err) != "INVALID_PAYMENT_TRANSITION" {
		t.Fatalf("settled before signed: %v", err)
	}
}

// An Ask is paid under the person's verified approval (webauthn, the
// grant's Signer approval ID); a simulated grant never pays.
func TestPaymentWebAuthnAsk(t *testing.T) {
	e := newEnv(t)
	s := e.open()
	po, intent := e.webauthnApproved(s)
	p, _, err := s.StartPayment(PaymentStart{ConsumerID: "consumer-a", TaskID: "task-a", PurchaseOrderID: po})
	if err != nil || p.Method != "webauthn" || p.GrantRef != "hap-1" || p.IntentHash != intent {
		t.Fatalf("start %+v %v", p, err)
	}
	if _, err := s.PaymentReserved(intent, Reservation{Evidence: evidence, TxHash: tx1, Block: 7}); err != nil {
		t.Fatal(err)
	}
	s.Close()
	s = e.open() // the recovery check holds for a webauthn payment

	e2 := newEnv(t)
	s2 := e2.open()
	id, intent2 := e2.payTask(s2, "task-a", 30)
	if _, err := approve(s2, id, intent2, "approve-req-000000001", "consumer-a", "approve"); err != nil {
		t.Fatal(err)
	}
	var po2 string
	rawDB(t, e2.path).QueryRow(`SELECT purchase_order_id FROM approval_sessions WHERE approval_session_id = ?`, id).Scan(&po2)
	if _, _, err := s2.StartPayment(PaymentStart{ConsumerID: "consumer-a", TaskID: "task-a", PurchaseOrderID: po2}); Code(err) != "GRANT_METHOD_NOT_ACCEPTED" {
		t.Fatalf("a simulated grant: %v", err)
	}
}

// Refused before any write: another consumer, an open Ask, an Allow
// without a Signer delegation, an unknown purchase, a cancelled task.
func TestPaymentStartRefusals(t *testing.T) {
	e := newEnv(t)
	s := e.open()
	po, _ := e.payAllowed(s)
	var askPO string
	rawDB(t, e.path).QueryRow(`SELECT purchase_order_id FROM purchases WHERE task_id = 'task-a' AND policy_verdict = 'Ask'`).Scan(&askPO)
	noRef := payStart(po)
	noRef.DelegationRef = ""
	for name, cs := range map[string]struct {
		req  PaymentStart
		want string
	}{
		"another consumer":  {PaymentStart{ConsumerID: "consumer-b", TaskID: "task-a", PurchaseOrderID: po, DelegationRef: "dlg-a@1"}, "OWNERSHIP_MISMATCH"},
		"an open Ask":       {payStart(askPO), "PAYMENT_NOT_ALLOWED"},
		"no delegation ref": {noRef, "DELEGATION_REF_REQUIRED"},
		"unknown purchase":  {payStart("po-nope"), "UNKNOWN_PURCHASE"},
	} {
		if _, _, err := s.StartPayment(cs.req); Code(err) != cs.want {
			t.Errorf("%s: %v, want %s", name, err, cs.want)
		}
	}
	if err := s.Cancel("task-a"); err != nil {
		t.Fatal(err)
	}
	if _, _, err := s.StartPayment(payStart(po)); Code(err) != "TASK_ALREADY_ENDED" {
		t.Fatalf("after cancel: %v", err)
	}
	if n := count(t, e.path, "payments", ""); n != 0 {
		t.Fatalf("refusals wrote %d payments", n)
	}
}

// Failures: never reserved spends nothing (not_paid); reserved and not
// paid needs review, its budget kept spent; paid and not delivered is
// paid, for a person; a code that does not fit the state is refused.
func TestPaymentFailures(t *testing.T) {
	cases := []struct {
		name   string
		upTo   int // steps done before the failure: 0 requested, 1 reserved, 4 settled
		code   string
		state  string
		status string
	}{
		{"reserve failed", 0, "RESERVE_FAILED", "reserve_failed", "not_paid"},
		{"expired before reserving", 0, "NOT_PAID_EXPIRED", "not_paid_expired", "not_paid"},
		{"reserved, not paid", 1, "RESERVED_NOT_PAID", "needs_review", "needs_review"},
		{"settled, not delivered", 4, "DELIVERY_FAILED", "needs_review", "paid"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			e := newEnv(t)
			s := e.open()
			po, intent := e.payAllowed(s)
			before := e.budgetLeft()
			s.StartPayment(payStart(po))
			do := []func() (Payment, error){
				func() (Payment, error) {
					return s.PaymentReserved(intent, Reservation{Evidence: evidence, TxHash: tx1, Block: 7})
				},
				func() (Payment, error) { return s.PaymentSigned(intent, auth) },
				func() (Payment, error) { return s.PaymentSubmitted(intent) },
				func() (Payment, error) { return s.PaymentSettled(intent, tx2, 9, nil) },
			}
			for _, f := range do[:c.upTo] {
				if _, err := f(); err != nil {
					t.Fatal(err)
				}
			}
			if c.upTo == 0 {
				if _, err := s.PaymentFailed(intent, "RESERVED_NOT_PAID"); Code(err) != "INVALID_PAYMENT_TRANSITION" {
					t.Fatalf("needs review before a reservation: %v", err)
				}
			}
			p, err := s.PaymentFailed(intent, c.code)
			if err != nil || p.State != c.state || p.Code != c.code {
				t.Fatalf("failed: %+v %v", p, err)
			}
			if v := e.view(s, po); v == nil || v.Status != c.status || v.Code != c.code {
				t.Fatalf("view %+v", v)
			}
			if spent := e.budgetLeft() != before; spent != (c.upTo > 0) {
				t.Fatalf("budget spent %v after %d steps", spent, c.upTo)
			}
			s.Close()
			s = e.open()
			// Ended: another code is refused (a move it cannot make, or the same end with another value).
			if _, err := s.PaymentFailed(intent, "NOT_SETTLED_EXPIRED"); Code(err) != "INVALID_PAYMENT_TRANSITION" && Code(err) != "IDEMPOTENCY_CONFLICT" {
				t.Fatalf("an ended payment moved: %v", err)
			}
			if again, err := s.PaymentFailed(intent, c.code); err != nil || again.State != c.state {
				t.Fatalf("the same end again: %+v %v", again, err)
			}
		})
	}
	e := newEnv(t)
	s := e.open()
	po, intent := e.payAllowed(s)
	s.StartPayment(payStart(po))
	if _, err := s.PaymentFailed(intent, "NOT_A_CODE"); Code(err) != "INVALID_PAYMENT_UPDATE" {
		t.Fatalf("unknown code: %v", err)
	}
}

// The recovery check refuses a payment that disagrees with its events, a
// budget that disagrees with the reservations, a missing receipt.
func TestPaymentRecoveryRefuses(t *testing.T) {
	for name, sql := range map[string]string{
		"a state without its event":  `UPDATE payments SET state = 'submitted'`,
		"a budget not deducted":      `UPDATE tasks SET budget_remaining = '5000000' WHERE task_id = 'task-a'`,
		"a receipt removed":          `DELETE FROM receipts`,
		"a reservation's tx changed": `UPDATE receipts SET reserve_tx = '0x` + strings.Repeat("5", 64) + `'`,
	} {
		t.Run(name, func(t *testing.T) {
			e := newEnv(t)
			s := e.open()
			po, intent := e.payAllowed(s)
			s.StartPayment(payStart(po))
			s.PaymentReserved(intent, Reservation{Evidence: evidence, TxHash: tx1, Block: 7})
			s.PaymentSigned(intent, auth)
			if strings.HasPrefix(name, "a receipt") || strings.HasPrefix(name, "a reservation's") {
				s.PaymentSubmitted(intent)
				s.PaymentSettled(intent, tx2, 9, nil)
				if _, err := s.PaymentDelivered(intent, content, 1); err != nil {
					t.Fatal(err)
				}
			}
			s.Close()
			if _, err := rawDB(t, e.path).Exec(sql); err != nil {
				t.Fatal(err)
			}
			var ce *CorruptError
			if err := openErr(e); !errors.As(err, &ce) || ce.Check != "payment" {
				t.Fatalf("Open: %v, want a payment corruption", err)
			}
		})
	}
}

// A schema 6 file with a paid-family task upgrades to 7 unchanged, its
// payments and receipts tables empty, and pays afterwards.
func TestUpgradeSchema6(t *testing.T) {
	e := newEnv(t)
	s := e.open()
	po, intent := e.payAllowed(s)
	s.Close()
	toSchema6(t, e.path)
	want := fingerprint(t, e.path)
	if _, err := openOpt(e, false); Code(err) != "SCHEMA_UPGRADE_REQUIRED" {
		t.Fatalf("without the flag: %v", err)
	}
	s, err := openOpt(e, true)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	if userVersion(t, e.path) != 7 || fingerprint(t, e.path) != want {
		t.Fatal("the upgrade changed the data or the version")
	}
	if p, _, err := s.StartPayment(payStart(po)); err != nil || p.IntentHash != intent {
		t.Fatalf("pay after the upgrade: %+v %v", p, err)
	}
}

// The recovery check refuses a webauthn payment that is not its
// purchase's grant (another approval ID).
func TestPaymentRecoveryRefusesAnotherGrant(t *testing.T) {
	e := newEnv(t)
	s := e.open()
	po, _ := e.webauthnApproved(s)
	if _, _, err := s.StartPayment(PaymentStart{ConsumerID: "consumer-a", TaskID: "task-a", PurchaseOrderID: po}); err != nil {
		t.Fatal(err)
	}
	s.Close()
	if _, err := rawDB(t, e.path).Exec(`UPDATE payments SET grant_ref = 'hap-2'`); err != nil {
		t.Fatal(err)
	}
	var ce *CorruptError
	if err := openErr(e); !errors.As(err, &ce) || ce.Check != "payment" {
		t.Fatalf("Open: %v, want a payment corruption", err)
	}
}
