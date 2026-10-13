package store

// Tests of schema 4's payment_attempts (P1-b; docs/commercial-p1b-design.md
// 6, 8 R4): the delegation path, the Host's refusals before any write, the
// consumer scope, the CHECKs and recovery check, and the upgrade of a
// schema 3 file holding a simulated grant. Synthetic files only.

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"github.com/SIMPLYBOYS/agent-payment-authority/internal/contracts"
	"github.com/SIMPLYBOYS/agent-payment-authority/internal/paycontract"
)

// payAllowed starts task-a as apa.pay.v1 (its base purchase an Ask) and
// submits the allowed purchase; it returns that purchase's ID and intent.
func (e *env) payAllowed(s *Store) (string, string) {
	e.t.Helper()
	e.payTask(s, "task-a", 30)
	o := v1Of(e.t, forTask(e.t, e.allow, "task-a"))
	o.PaymentBinding.Nonce = fmt.Sprintf("0x%064x", 50)
	r := submitV1(e.t, s, sessA, o)
	if r.Purchase.Decision.PolicyVerdict != "Allow" {
		e.t.Fatalf("not an Allow: %+v", r.Purchase.Decision)
	}
	return o.Intent.PurchaseOrderID, r.Purchase.IntentHash
}

func payReq(po string) PaymentRequest {
	return PaymentRequest{ConsumerID: "consumer-a", TaskID: "task-a", PurchaseOrderID: po, Method: "delegation", DelegationRef: "dlg-a@1"}
}

func TestPaymentDelegationPath(t *testing.T) {
	e := newEnv(t)
	s := e.open()
	po, intent := e.payAllowed(s)
	sessions, grants := count(t, e.path, "approval_sessions", ""), count(t, e.path, "grants", "")
	a, replay, err := s.BeginPayment(payReq(po))
	if err != nil || replay || a.State != "requested" || a.IntentHash != intent || a.Method != "delegation" || a.DelegationRef != "dlg-a@1" || a.SignerAttemptID != "" {
		t.Fatalf("begin: %+v %v %v", a, replay, err)
	}
	var o paycontract.PurchaseOrder
	if err := contracts.Decode(a.Order, &o); err != nil || o.Intent.PurchaseOrderID != po {
		t.Fatalf("the order sent to the Signer is not the stored purchase: %v", err)
	}
	if count(t, e.path, "approval_sessions", "") != sessions || count(t, e.path, "grants", "") != grants {
		t.Fatal("session invented for a delegation payment")
	}
	// The request ID is committed before the Signer is called; a resend,
	// also after a restart, gets the same one, never a new ID.
	for i := 0; i < 2; i++ {
		if i == 1 {
			s.Close()
			s = e.open()
		}
		again, replay, err := s.BeginPayment(payReq(po))
		if err != nil || !replay || again.RequestID != a.RequestID || count(t, e.path, "payment_attempts", "") != 1 {
			t.Fatalf("resend %d: %+v %v", i, again, err)
		}
	}
	other := payReq(po)
	other.DelegationRef = "dlg-a@2"
	if _, _, err := s.BeginPayment(other); Code(err) != "IDEMPOTENCY_CONFLICT" {
		t.Fatalf("another delegation for the same purchase: %v", err)
	}
	// unknown, then what the Signer answers for the same request ID.
	if u, err := s.UpdatePayment(a.RequestID, "unknown", "", ""); err != nil || u.State != "unknown" {
		t.Fatalf("unknown: %+v %v", u, err)
	}
	if u, err := s.UpdatePayment(a.RequestID, "signed", "att-1", ""); err != nil || u.State != "signed" || u.SignerAttemptID != "att-1" {
		t.Fatalf("signed: %+v %v", u, err)
	}
	if _, err := s.UpdatePayment(a.RequestID, "signed", "att-1", ""); err != nil {
		t.Fatalf("the same answer again: %v", err)
	}
	if _, err := s.UpdatePayment(a.RequestID, "abandoned", "att-1", ""); Code(err) != "PAYMENT_FINAL" {
		t.Fatalf("a final state changed: %v", err)
	}
	if _, err := s.UpdatePayment(a.RequestID, "requested", "", ""); Code(err) != "INVALID_PAYMENT_UPDATE" {
		t.Fatalf("back to requested: %v", err)
	}
	// Consumer scope is the Host's: B reads nothing of A's.
	if got, err := s.Payment("consumer-a", a.RequestID); err != nil || got.State != "signed" {
		t.Fatalf("A reads its own: %+v %v", got, err)
	}
	if _, err := s.Payment("consumer-b", a.RequestID); Code(err) != "UNKNOWN_PAYMENT" {
		t.Fatalf("B read A's attempt: %v", err)
	}
}

func TestPaymentAttemptIDFixed(t *testing.T) {
	e := newEnv(t)
	s := e.open()
	po, _ := e.payAllowed(s)
	a, _, _ := s.BeginPayment(payReq(po))
	if _, err := s.UpdatePayment(a.RequestID, "accepted", "att-1", ""); err != nil {
		t.Fatal(err)
	}
	if _, err := s.UpdatePayment(a.RequestID, "signed", "att-2", ""); Code(err) != "IDEMPOTENCY_CONFLICT" {
		t.Fatalf("the Signer's attempt ID changed: %v", err)
	}
	if _, err := s.UpdatePayment(a.RequestID, "accepted", "", ""); Code(err) != "IDEMPOTENCY_CONFLICT" {
		t.Fatalf("the Signer's attempt ID dropped: %v", err)
	}
}

// The Host refuses before writing anything.
func TestPaymentRefusals(t *testing.T) {
	e := newEnv(t)
	s := e.open()
	po, _ := e.payAllowed(s)
	ask := v1Of(t, forTask(t, e.base, "task-a")).Intent.PurchaseOrderID
	e.start(s, "task-b") // an apa.m1.v2 task
	submit(t, s, sessB, forTask(t, e.allow, "task-b"))
	for name, c := range map[string]struct {
		edit func(*PaymentRequest)
		want string
	}{
		"webauthn":         {func(r *PaymentRequest) { r.Method = "webauthn" }, "HUMAN_APPROVAL_UNAVAILABLE"},
		"simulated":        {func(r *PaymentRequest) { r.Method = "simulated" }, "GRANT_METHOD_NOT_ACCEPTED"},
		"no delegation":    {func(r *PaymentRequest) { r.DelegationRef = "" }, "DELEGATION_REF_REQUIRED"},
		"no version":       {func(r *PaymentRequest) { r.DelegationRef = "dlg-a" }, "DELEGATION_REF_REQUIRED"},
		"another consumer": {func(r *PaymentRequest) { r.ConsumerID = "consumer-b" }, "OWNERSHIP_MISMATCH"},
		"unknown task":     {func(r *PaymentRequest) { r.TaskID = "task-z" }, "OWNERSHIP_MISMATCH"},
		"unknown purchase": {func(r *PaymentRequest) { r.PurchaseOrderID = "po-none" }, "UNKNOWN_PURCHASE"},
		"an Ask":           {func(r *PaymentRequest) { r.PurchaseOrderID = ask }, "PAYMENT_NOT_ALLOWED"},
		"an M1 task": {func(r *PaymentRequest) {
			r.ConsumerID, r.TaskID, r.PurchaseOrderID = "consumer-b", "task-b", forTask(t, e.allow, "task-b").Intent.PurchaseOrderID
		}, "CONTRACT_VERSION_MISMATCH"},
	} {
		r := payReq(po)
		c.edit(&r)
		if _, _, err := s.BeginPayment(r); Code(err) != c.want {
			t.Errorf("%s: %v, want %s", name, err, c.want)
		}
	}
	if n := count(t, e.path, "payment_attempts", ""); n != 0 {
		t.Fatalf("%d rows written by refusals", n)
	}
}

// R4: a row that is not an allowed v1 purchase under a delegation is
// refused by a CHECK on write, or by the recovery check on open.
func TestPaymentSchemaChecks(t *testing.T) {
	setup := func(t *testing.T) (*env, string) {
		e := newEnv(t)
		s := e.open()
		po, _ := e.payAllowed(s)
		a, _, err := s.BeginPayment(payReq(po))
		if err != nil {
			t.Fatal(err)
		}
		e.start(s, "task-b") // an apa.m1.v2 task with an allowed purchase
		submit(t, s, sessB, forTask(t, e.allow, "task-b"))
		s.Close()
		return e, a.RequestID
	}
	for name, q := range map[string]string{
		"method simulated":   `UPDATE payment_attempts SET method = 'simulated'`,
		"method webauthn":    `UPDATE payment_attempts SET method = 'webauthn'`,
		"no delegation":      `UPDATE payment_attempts SET delegation_ref = ''`,
		"signed without ID":  `UPDATE payment_attempts SET state = 'signed'`,
		"rejected, no code":  `UPDATE payment_attempts SET state = 'rejected'`,
		"another state":      `UPDATE payment_attempts SET state = 'paid'`,
		"two for one intent": `INSERT INTO payment_attempts SELECT 'pay-2', task_id, purchase_order_id, intent_hash, method, delegation_ref, NULL, state, NULL, requested_at, updated_at FROM payment_attempts`,
	} {
		t.Run(name, func(t *testing.T) {
			e, _ := setup(t)
			if _, err := rawDB(t, e.path).Exec(q); err == nil {
				t.Fatal("non-delegation payment attempt accepted")
			}
		})
	}
	for name, q := range map[string]string{
		"an Ask's purchase": `UPDATE payment_attempts SET purchase_order_id = (SELECT purchase_order_id FROM purchases WHERE policy_verdict = 'Ask'),
			intent_hash = (SELECT intent_hash FROM purchases WHERE policy_verdict = 'Ask')`,
		"another intent": `UPDATE payment_attempts SET intent_hash = 'sha256:` + fmt.Sprintf("%064d", 0) + `'`,
		"updated before": `UPDATE payment_attempts SET updated_at = requested_at - 1`,
		"an M1 task's purchase": `UPDATE payment_attempts SET task_id = 'task-b', purchase_order_id = (SELECT purchase_order_id FROM purchases WHERE task_id = 'task-b'),
			intent_hash = (SELECT intent_hash FROM purchases WHERE task_id = 'task-b')`,
	} {
		t.Run(name, func(t *testing.T) {
			e, _ := setup(t)
			if _, err := rawDB(t, e.path).Exec(q); err != nil {
				t.Fatal(err)
			}
			var ce *CorruptError
			if err := openErr(e); !errors.As(err, &ce) || ce.Check != "payment" {
				t.Fatalf("Open: %v, want a payment corruption", err)
			}
		})
	}
}

// toSchema4 turns the current file into schema 4 as P1-b wrote it: schema
// 5 rebuilt grants (method webauthn, signer_approval_id) and added
// approval_attempts.
// toSchema5 turns the current file into schema 5 as A2 wrote it: schema 6
// only added the credential tables and widened three CHECKs.
func toSchema5(t *testing.T, path string) {
	t.Helper()
	tx, err := rawDB(t, path).Begin() // rawDB has foreign keys off
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback()
	if _, err := tx.Exec(`DROP TABLE credential_verifications; DROP TABLE credential_challenges; DROP TABLE credential_status_seen`); err != nil {
		t.Fatal(err)
	}
	if err := rebuildChecks(tx, schema6Checks, true); err != nil {
		t.Fatal(err)
	}
	if _, err := tx.Exec(`PRAGMA user_version = 5`); err != nil {
		t.Fatal(err)
	}
	if err := tx.Commit(); err != nil {
		t.Fatal(err)
	}
}

func toSchema4(t *testing.T, path string) {
	t.Helper()
	toSchema5(t, path)
	if _, err := rawDB(t, path).Exec(`DROP TABLE approval_attempts;
CREATE TABLE grants_v4 (
  jti                  TEXT PRIMARY KEY,
  approval_session_id  TEXT NOT NULL UNIQUE REFERENCES approval_sessions,
  intent_hash          TEXT NOT NULL,
  authorization_method TEXT NOT NULL CHECK (authorization_method = 'simulated'),
  body                 TEXT NOT NULL,
  issued_at            INTEGER NOT NULL
) STRICT;
INSERT INTO grants_v4 SELECT jti, approval_session_id, intent_hash, authorization_method, body, issued_at FROM grants;
DROP TABLE grants;
ALTER TABLE grants_v4 RENAME TO grants;
PRAGMA user_version = 4`); err != nil {
		t.Fatal(err)
	}
}

// toSchema3 turns the current file into schema 3 as P1-a wrote it: schema 4
// only added payment_attempts.
func toSchema3(t *testing.T, path string) {
	t.Helper()
	toSchema4(t, path)
	if _, err := rawDB(t, path).Exec(`DROP TABLE payment_attempts; PRAGMA user_version = 3`); err != nil {
		t.Fatal(err)
	}
}

// R4: a schema 3 file with a simulated grant upgrades to 4 unchanged; the
// grant can never be paid: no row may refer to it, and its purchase is an
// Ask, which BeginPayment refuses.
func TestUpgradeSchema3(t *testing.T) {
	e := newEnv(t)
	s := e.open()
	id, intent := e.payTask(s, "task-a", 30)
	if r, err := approve(s, id, intent, "approve-req-000000001", "consumer-a", "approve"); err != nil || r.Grant == nil {
		t.Fatalf("approve: %+v %v", r, err)
	}
	s.Close()
	toSchema3(t, e.path)
	grant := func() string {
		var g string
		if err := rawDB(t, e.path).QueryRow(`SELECT jti || authorization_method || body || issued_at FROM grants`).Scan(&g); err != nil {
			t.Fatal(err)
		}
		return g
	}
	before, want, g := mustRead(t, e.path), fingerprint(t, e.path), grant()
	if _, err := openOpt(e, false); Code(err) != "SCHEMA_UPGRADE_REQUIRED" || string(mustRead(t, e.path)) != string(before) {
		t.Fatalf("without the flag: %v", err)
	}
	s, err := openOpt(e, true)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	if userVersion(t, e.path) != schemaVersion || fingerprint(t, e.path) != want || grant() != g || count(t, e.path, "payment_attempts", "") != 0 {
		t.Fatal("migration changed simulated data")
	}
	if m, _ := filepath.Glob(e.path + ".schema3-*.bak"); len(m) != 1 {
		t.Fatalf("backups %v", m)
	}
	po := v1Of(t, forTask(t, e.base, "task-a")).Intent.PurchaseOrderID
	if _, _, err := s.BeginPayment(payReq(po)); Code(err) != "PAYMENT_NOT_ALLOWED" {
		t.Fatalf("a simulated approval was paid: %v", err)
	}
	v, _ := s.Task(sessA)
	if b, _ := json.Marshal(v.Purchases[0].Approval); v.Purchases[0].Approval.State != "approved" || v.Purchases[0].Decision.PaymentStatus != "not_enabled" {
		t.Fatalf("P1-a view changed: %s", b)
	}
}

func mustRead(t *testing.T, path string) []byte {
	t.Helper()
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return b
}

// R1 of the P1-b review (22e45ed): once the task's cancellation is
// committed, no payment attempt is created and none is handed out again for
// sending; what exists can still be read and reconciled.
func TestPaymentAfterCancel(t *testing.T) {
	t.Run("a purchase never paid", func(t *testing.T) {
		e := newEnv(t)
		s := e.open()
		po, _ := e.payAllowed(s)
		if err := s.Cancel("task-a"); err != nil {
			t.Fatal(err)
		}
		if _, _, err := s.BeginPayment(payReq(po)); Code(err) != "TASK_ALREADY_ENDED" || count(t, e.path, "payment_attempts", "") != 0 {
			t.Fatalf("cancelled task accepted a new payment request: %v", err)
		}
	})
	t.Run("an unknown attempt", func(t *testing.T) {
		e := newEnv(t)
		s := e.open()
		po, _ := e.payAllowed(s)
		a, _, err := s.BeginPayment(payReq(po))
		if err != nil {
			t.Fatal(err)
		}
		if _, err := s.UpdatePayment(a.RequestID, "unknown", "", ""); err != nil {
			t.Fatal(err)
		}
		if err := s.Cancel("task-a"); err != nil {
			t.Fatal(err)
		}
		if again, replay, err := s.BeginPayment(payReq(po)); Code(err) != "TASK_ALREADY_ENDED" || replay {
			t.Fatalf("cancelled task handed out its unknown attempt for sending: %+v %v", again, err)
		}
		// Still readable, and the Signer's answer for the same ID still recorded.
		if got, err := s.Payment("consumer-a", a.RequestID); err != nil || got.State != "unknown" {
			t.Fatalf("read after cancel: %+v %v", got, err)
		}
		if got, err := s.UpdatePayment(a.RequestID, "signed", "att-1", ""); err != nil || got.State != "signed" || count(t, e.path, "payment_attempts", "") != 1 {
			t.Fatalf("reconcile after cancel: %+v %v", got, err)
		}
	})
	t.Run("a Deny before the cancellation", func(t *testing.T) {
		// The task's status stays denied (project), but it was cancelled.
		e := newEnv(t)
		s := e.open()
		po, _ := e.payAllowed(s)
		d := v1Of(t, forTask(t, e.denied, "task-a"))
		d.PaymentBinding.Nonce = fmt.Sprintf("0x%064x", 60)
		if r := submitV1(t, s, sessA, d); r.Purchase.Decision.PolicyVerdict != "Deny" {
			t.Fatalf("not a Deny: %+v", r.Purchase.Decision)
		}
		if err := s.Cancel("task-a"); err != nil {
			t.Fatal(err)
		}
		if _, _, err := s.BeginPayment(payReq(po)); Code(err) != "TASK_ALREADY_ENDED" {
			t.Fatalf("cancelled task accepted a new payment request: %v", err)
		}
	})
	t.Run("a CLI timeout is not a cancellation", func(t *testing.T) {
		// Unchanged by R1: whether a timed-out task may still pay is not
		// decided here.
		e := newEnv(t)
		s := e.open()
		po, _ := e.payAllowed(s)
		if err := s.CLITimeout("task-a"); err != nil {
			t.Fatal(err)
		}
		if _, _, err := s.BeginPayment(payReq(po)); err != nil {
			t.Fatalf("CLI timeout taken for a cancellation: %v", err)
		}
	})
}
