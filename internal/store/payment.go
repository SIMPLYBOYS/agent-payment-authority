package store

// The Host's mirror of payment attempts (P1-b; docs/commercial-p1b-design.md
// 6.4): one row per allowed apa.pay.v1 purchase sent to the Signer under a
// standing delegation. The request ID is made and committed here before the
// Signer is called, and is the only key the Host ever uses for it. The row
// holds no signature and no grant: schema 4 cannot refer to a simulated
// grant, and the only method is delegation (webauthn is A2's migration).

import (
	"crypto/rand"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"regexp"
	"slices"

	"github.com/SIMPLYBOYS/agent-payment-authority/internal/contracts"
)

// schema4 is schema 4's addition.
const schema4 = `
CREATE TABLE payment_attempts (
  request_id        TEXT PRIMARY KEY,
  task_id           TEXT NOT NULL,
  purchase_order_id TEXT NOT NULL,
  intent_hash       TEXT NOT NULL UNIQUE,
  method            TEXT NOT NULL CHECK (method = 'delegation'),
  delegation_ref    TEXT NOT NULL CHECK (delegation_ref <> ''),
  signer_attempt_id TEXT UNIQUE,
  state             TEXT NOT NULL CHECK (state IN ('requested', 'accepted', 'signed', 'abandoned', 'expired_unsigned', 'rejected', 'unknown')),
  error_code        TEXT,
  requested_at      INTEGER NOT NULL,
  updated_at        INTEGER NOT NULL,
  FOREIGN KEY (task_id, purchase_order_id) REFERENCES purchases,
  CHECK (state NOT IN ('accepted', 'signed', 'abandoned', 'expired_unsigned') OR signer_attempt_id IS NOT NULL),
  CHECK (state <> 'rejected' OR error_code IS NOT NULL)
) STRICT;
`

// delegationRefPattern is a standing delegation's ID and version, id@version.
var delegationRefPattern = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._:-]{0,127}@[1-9][0-9]{0,9}$`)

// PaymentRequest asks to pay one allowed purchase. ConsumerID and
// DelegationRef come from the Host's trusted configuration and session,
// never from the Agent.
type PaymentRequest struct {
	ConsumerID, TaskID, PurchaseOrderID string
	Method                              string // delegation; anything else is refused before any write
	DelegationRef                       string // id@version
}

// PaymentAttempt is a payment_attempts row; Order is its purchase's stored
// apa.pay.v1 order, what the Signer is sent.
type PaymentAttempt struct {
	RequestID, TaskID, PurchaseOrderID, IntentHash string
	Method, DelegationRef, SignerAttemptID         string
	State, ErrorCode                               string
	RequestedAt, UpdatedAt                         uint64
	Order                                          json.RawMessage
}

const paymentCols = `a.request_id, a.task_id, a.purchase_order_id, a.intent_hash, a.method, a.delegation_ref, coalesce(a.signer_attempt_id, ''),
  a.state, coalesce(a.error_code, ''), a.requested_at, a.updated_at, p.body, t.consumer_id`

func scanPayment(row interface{ Scan(...any) error }) (PaymentAttempt, string, error) {
	var a PaymentAttempt
	var body, consumer string
	err := row.Scan(&a.RequestID, &a.TaskID, &a.PurchaseOrderID, &a.IntentHash, &a.Method, &a.DelegationRef, &a.SignerAttemptID,
		&a.State, &a.ErrorCode, &a.RequestedAt, &a.UpdatedAt, &body, &consumer)
	a.Order = json.RawMessage(body)
	return a, consumer, err
}

func paymentWhere(cond string) string {
	return `SELECT ` + paymentCols + ` FROM payment_attempts a JOIN purchases p USING (task_id, purchase_order_id) JOIN tasks t USING (task_id) WHERE ` + cond
}

// BeginPayment records the request to pay an allowed purchase of the
// consumer's apa.pay.v1 task, and commits it, before the Signer is called.
// The purchase's existing row is returned as it is (Replay): the Host never
// makes a second request ID for a purchase. Once the task's cancellation is
// committed it refuses, the existing row too, so neither a new attempt nor a
// resend of a requested or unknown one reaches the Signer; Payment still
// reads it, and the Signer's abandon still applies. A cancellation committed
// after this transaction does not recall a request already sent.
func (s *Store) BeginPayment(req PaymentRequest) (PaymentAttempt, bool, error) {
	switch {
	case req.Method == "webauthn":
		return PaymentAttempt{}, false, refuse("HUMAN_APPROVAL_UNAVAILABLE", "webauthn payments are A2's")
	case req.Method != "delegation":
		return PaymentAttempt{}, false, refuse("GRANT_METHOD_NOT_ACCEPTED", "a payment is authorized by a standing delegation only")
	case !delegationRefPattern.MatchString(req.DelegationRef):
		return PaymentAttempt{}, false, refuse("DELEGATION_REF_REQUIRED", "delegation_ref is id@version")
	}
	var out PaymentAttempt
	var replay bool
	err := s.write(func(tx *sql.Tx, now uint64) error {
		t, err := s.openTask(tx, now, req.TaskID)
		if Code(err) == "UNKNOWN_TASK" || err == nil && t.row.ConsumerID != req.ConsumerID {
			return refuse("OWNERSHIP_MISMATCH", "no such task for this consumer")
		} else if err != nil {
			return err
		}
		p := t.purchases[req.PurchaseOrderID]
		switch {
		case slices.ContainsFunc(t.events, func(e contracts.AuditEvent) bool { return e.EventType == "TASK_CANCELLED" }):
			return refuse("TASK_ALREADY_ENDED", "the task was cancelled: no payment is sent for it")
		case !t.pay():
			return refuse("CONTRACT_VERSION_MISMATCH", "only apa.pay.v1 purchases are paid")
		case p == nil:
			return refuse("UNKNOWN_PURCHASE", "no such purchase in this task")
		case p.Decision.PolicyVerdict != "Allow":
			return refuse("PAYMENT_NOT_ALLOWED", "only a purchase the policy allowed is paid under a delegation")
		}
		a, _, err := scanPayment(tx.QueryRow(paymentWhere(`a.task_id = ? AND a.purchase_order_id = ?`), req.TaskID, req.PurchaseOrderID))
		if err == nil {
			if a.Method != req.Method || a.DelegationRef != req.DelegationRef {
				return refuse("IDEMPOTENCY_CONFLICT", "this purchase was requested under another authorization")
			}
			out, replay = a, true
			return nil
		} else if !errors.Is(err, sql.ErrNoRows) {
			return err
		}
		b := make([]byte, 16)
		if _, err := rand.Read(b); err != nil {
			return err
		}
		out = PaymentAttempt{RequestID: "pay-" + hex.EncodeToString(b), TaskID: req.TaskID, PurchaseOrderID: req.PurchaseOrderID, IntentHash: p.Decision.IntentHash,
			Method: req.Method, DelegationRef: req.DelegationRef, State: "requested", RequestedAt: now, UpdatedAt: now, Order: json.RawMessage(p.Body)}
		_, err = tx.Exec(`INSERT INTO payment_attempts (request_id, task_id, purchase_order_id, intent_hash, method, delegation_ref, state, requested_at, updated_at)
			VALUES (?, ?, ?, ?, ?, ?, 'requested', ?, ?)`, out.RequestID, out.TaskID, out.PurchaseOrderID, out.IntentHash, out.Method, out.DelegationRef, now, now)
		return err
	})
	return out, replay, err
}

// final states never change; unknown and the others move to what the
// Signer answers for the same request ID.
var (
	paymentFinal  = map[string]bool{"signed": true, "abandoned": true, "expired_unsigned": true}
	paymentStates = map[string]bool{"accepted": true, "signed": true, "abandoned": true, "expired_unsigned": true, "rejected": true, "unknown": true}
)

// UpdatePayment records what the Signer answered for requestID. A final
// state is kept (the same answer again is accepted), and the Signer's
// attempt ID, once known, does not change.
func (s *Store) UpdatePayment(requestID, state, signerAttemptID, errorCode string) (PaymentAttempt, error) {
	var out PaymentAttempt
	if !paymentStates[state] || errorCode != "" && !storeCodePattern.MatchString(errorCode) {
		return out, refuse("INVALID_PAYMENT_UPDATE", "error code")
	}
	err := s.write(func(tx *sql.Tx, now uint64) error {
		a, _, err := scanPayment(tx.QueryRow(paymentWhere(`a.request_id = ?`), requestID))
		if errors.Is(err, sql.ErrNoRows) {
			return refuse("UNKNOWN_PAYMENT", "no such payment request")
		} else if err != nil {
			return err
		}
		switch {
		case paymentFinal[a.State] && (state != a.State || signerAttemptID != a.SignerAttemptID):
			return refuse("PAYMENT_FINAL", "the payment attempt is already "+a.State)
		case a.SignerAttemptID != "" && signerAttemptID != a.SignerAttemptID:
			return refuse("IDEMPOTENCY_CONFLICT", "the Signer's attempt ID changed")
		}
		var id, code any
		if signerAttemptID != "" {
			id = signerAttemptID
		}
		if errorCode != "" {
			code = errorCode
		}
		if _, err := tx.Exec(`UPDATE payment_attempts SET state = ?, signer_attempt_id = ?, error_code = ?, updated_at = ? WHERE request_id = ?`,
			state, id, code, now, requestID); err != nil {
			return err
		}
		out, _, err = scanPayment(tx.QueryRow(paymentWhere(`a.request_id = ?`), requestID))
		return err
	})
	return out, err
}

// Payment reads one payment attempt of consumerID's tasks; another
// consumer's is answered as unknown (the Signer cannot tell consumers
// apart: this is the Host's scope, design 5.6).
func (s *Store) Payment(consumerID, requestID string) (PaymentAttempt, error) {
	a, consumer, err := scanPayment(s.r.QueryRow(paymentWhere(`a.request_id = ?`), requestID))
	if errors.Is(err, sql.ErrNoRows) || err == nil && consumer != consumerID {
		return PaymentAttempt{}, refuse("UNKNOWN_PAYMENT", "no such payment request for this consumer")
	}
	return a, err
}

// verifyPayments is schema 4's recovery check: every row is an allowed
// purchase of an apa.pay.v1 task with that purchase's intent, under a
// delegation.
func verifyPayments(tx *sql.Tx) error {
	var n int
	if err := tx.QueryRow(`SELECT count(*) FROM payment_attempts a LEFT JOIN purchases p USING (task_id, purchase_order_id) LEFT JOIN tasks t USING (task_id)
		WHERE p.task_id IS NULL OR t.contract_version NOT IN ('apa.pay.v1', 'apa.pay.v2') OR p.intent_hash != a.intent_hash OR p.policy_verdict != 'Allow'
		OR a.method != 'delegation' OR a.delegation_ref = '' OR a.updated_at < a.requested_at`).Scan(&n); err != nil {
		return readErr("payment", "", err)
	}
	if n > 0 {
		return &CorruptError{Check: "payment", Detail: "a payment attempt is not an allowed apa.pay.v1 purchase under a delegation, or its intent differs"}
	}
	return nil
}
