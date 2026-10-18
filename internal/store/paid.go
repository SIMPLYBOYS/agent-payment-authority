package store

// P2's payments (docs/commercial-p2-host.md 3, 4): one row per paid
// purchase of a pay-family task, keyed by its intent, carried through
// reserved (tx#1), signed, submitted, settled (tx#2), delivered and its
// receipt, each step an event of the task's chain written in the same
// transaction. A step is recorded before its outside call is made, and
// asked again it is the same step: the Host never makes a second payment
// for a purchase. An unknown result moves only by what the chain says
// (the Reconciler): never paid again.

import (
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"math/big"
	"slices"

	"github.com/SIMPLYBOYS/agent-payment-authority/internal/contracts"
	"github.com/SIMPLYBOYS/agent-payment-authority/internal/paycontract"
)

// schema7 is schema 7's addition.
const schema7 = `
CREATE TABLE payments (
  intent_hash       TEXT PRIMARY KEY,
  task_id           TEXT NOT NULL,
  purchase_order_id TEXT NOT NULL,
  method            TEXT NOT NULL CHECK (method IN ('delegation', 'webauthn')),
  grant_ref         TEXT NOT NULL CHECK (grant_ref <> ''),
  amount            TEXT NOT NULL,
  state             TEXT NOT NULL CHECK (state IN ('requested', 'reserved', 'signed', 'submitted', 'unknown', 'settled', 'delivered',
                                                  'reserve_failed', 'not_paid_expired', 'needs_review')),
  code              TEXT,
  reservation       TEXT,
  reserve_tx        TEXT,
  reserve_block     INTEGER,
  authorization     TEXT,
  settle_tx         TEXT,
  settle_block      INTEGER,
  payment_response  TEXT,
  created_at        INTEGER NOT NULL,
  updated_at        INTEGER NOT NULL,
  UNIQUE (task_id, purchase_order_id),
  FOREIGN KEY (task_id, purchase_order_id) REFERENCES purchases,
  CHECK (state IN ('requested', 'reserve_failed', 'not_paid_expired') OR (reservation IS NOT NULL AND reserve_tx IS NOT NULL AND reserve_block IS NOT NULL)),
  CHECK (state NOT IN ('signed', 'submitted', 'unknown', 'settled', 'delivered') OR authorization IS NOT NULL),
  CHECK (state NOT IN ('settled', 'delivered') OR (settle_tx IS NOT NULL AND settle_block IS NOT NULL)),
  CHECK ((state IN ('reserve_failed', 'not_paid_expired', 'needs_review')) = (code IS NOT NULL))
) STRICT;
CREATE TABLE receipts (
  receipt_id       TEXT PRIMARY KEY,
  intent_hash      TEXT NOT NULL UNIQUE REFERENCES payments,
  reserve_tx       TEXT NOT NULL,
  settle_tx        TEXT NOT NULL,
  payment_response TEXT NOT NULL,
  content_sha256   TEXT NOT NULL,
  content_size     INTEGER NOT NULL CHECK (content_size >= 0),
  delivered_at     INTEGER NOT NULL
) STRICT;
`

// Payment is a payments row; Order is its purchase's stored order.
type Payment struct {
	IntentHash, TaskID, PurchaseOrderID, ConsumerID string
	Method, GrantRef                                string // delegation and id@version, or webauthn and the Signer's approval ID
	Amount                                          contracts.Amount
	State, Code                                     string
	Reservation, Authorization, PaymentResponse     json.RawMessage
	SettleTx                                        string
	SettleBlock                                     uint64
	CreatedAt, UpdatedAt                            uint64
	Order                                           json.RawMessage
}

// PaymentStatus is a purchase's payment_status from its payment's state
// (docs/commercial-p2-host.md 5); "" (no payment yet) is not_started.
func PaymentStatus(state, code string) string {
	switch state {
	case "requested", "reserved", "signed":
		return "paying"
	case "submitted", "unknown":
		return "confirming"
	case "settled", "delivered":
		return "paid"
	case "reserve_failed", "not_paid_expired":
		return "not_paid"
	case "needs_review":
		if code == "DELIVERY_FAILED" {
			return "paid" // paid, and its delivery failed: a person handles it
		}
		return "needs_review"
	}
	return "not_started"
}

const paidCols = `p.intent_hash, p.task_id, p.purchase_order_id, t.consumer_id, p.method, p.grant_ref, p.amount, p.state, coalesce(p.code, ''),
  coalesce(p.reservation, ''), coalesce(p.authorization, ''), coalesce(p.payment_response, ''), coalesce(p.settle_tx, ''), coalesce(p.settle_block, 0),
  p.created_at, p.updated_at, pu.body`

func paidWhere(cond string) string {
	return `SELECT ` + paidCols + ` FROM payments p JOIN purchases pu USING (task_id, purchase_order_id) JOIN tasks t USING (task_id) WHERE ` + cond
}

func scanPaid(row interface{ Scan(...any) error }) (Payment, error) {
	var p Payment
	var res, auth, resp, body string
	err := row.Scan(&p.IntentHash, &p.TaskID, &p.PurchaseOrderID, &p.ConsumerID, &p.Method, &p.GrantRef, &p.Amount, &p.State, &p.Code,
		&res, &auth, &resp, &p.SettleTx, &p.SettleBlock, &p.CreatedAt, &p.UpdatedAt, &body)
	raw := func(s string) json.RawMessage {
		if s == "" {
			return nil
		}
		return json.RawMessage(s)
	}
	p.Reservation, p.Authorization, p.PaymentResponse, p.Order = raw(res), raw(auth), raw(resp), json.RawMessage(body)
	return p, err
}

// PaymentStart is one purchase to pay: its consumer and task from the
// Host's session, and the Signer delegation an Allow is paid under (the
// Host's trusted configuration; never the Agent's).
type PaymentStart struct {
	ConsumerID, TaskID, PurchaseOrderID string
	DelegationRef                       string // id@version, for an Allow
}

// StartPayment records the payment of an allowed purchase (method
// delegation) or of an Ask approved by a person (method webauthn, the
// grant's Signer approval), with its PAYMENT_REQUESTED event, and commits
// it before anything outside is asked. The purchase's existing payment is
// returned as it is (Replay; under another delegation, a conflict).
// Refused, nothing written: another consumer's task, a task the order
// rules see as ended (paycontract.Ended: a Deny, a cancellation, a CLI
// timeout), a purchase that is not payable (Deny, an open or
// closed-unapproved Ask, a simulated grant), one past its valid_before or
// its grant's expiry, one over the task's budget left (less what other
// requested payments will reserve), a credential that no longer holds.
func (s *Store) StartPayment(req PaymentStart) (Payment, bool, error) {
	var out Payment
	var replay bool
	err := s.write(func(tx *sql.Tx, now uint64) error {
		t, err := s.openTask(tx, now, req.TaskID)
		if Code(err) == "UNKNOWN_TASK" || err == nil && t.row.ConsumerID != req.ConsumerID {
			return refuse("OWNERSHIP_MISMATCH", "no such task for this consumer")
		} else if err != nil {
			return err
		}
		if p, err := scanPaid(tx.QueryRow(paidWhere(`p.task_id = ? AND p.purchase_order_id = ?`), req.TaskID, req.PurchaseOrderID)); err == nil {
			if p.Method == "delegation" && p.GrantRef != req.DelegationRef {
				return refuse("IDEMPOTENCY_CONFLICT", "this purchase's payment was started under another delegation")
			}
			out, replay = p, true
			return nil
		} else if !errors.Is(err, sql.ErrNoRows) {
			return err
		}
		p := t.purchases[req.PurchaseOrderID]
		switch {
		case !t.pay():
			return refuse("CONTRACT_VERSION_MISMATCH", "only pay-family purchases are paid")
		case p == nil:
			return refuse("UNKNOWN_PURCHASE", "no such purchase in this task")
		case paycontract.Ended(t.events):
			return refuse("TASK_ALREADY_ENDED", "the task ended (a Deny, a cancellation, a timeout): no payment is started for it")
		case p.V1 != nil && p.V1.PaymentBinding.ValidBefore <= now:
			return refuse("PURCHASE_EXPIRED", "the purchase's payment window has passed")
		}
		method, ref := "", ""
		switch {
		case p.Decision.PolicyVerdict == "Allow":
			if !delegationRefPattern.MatchString(req.DelegationRef) {
				return refuse("DELEGATION_REF_REQUIRED", "an Allow is paid under a Signer delegation, id@version")
			}
			method, ref = "delegation", req.DelegationRef
		case p.Decision.PolicyVerdict == "Ask" && p.Session != nil && p.Session.State == "approved":
			var m string
			var signer sql.NullString
			if err := tx.QueryRow(`SELECT authorization_method, signer_approval_id FROM grants WHERE approval_session_id = ?`, p.Session.ID).Scan(&m, &signer); err != nil {
				return readErr("payment", req.TaskID, err)
			}
			if m != "webauthn" || !signer.Valid {
				return refuse("GRANT_METHOD_NOT_ACCEPTED", "only a person's verified approval pays an Ask")
			}
			if g := p.Session.Grant; g == nil || g.ExpiresAt <= now {
				return refuse("PURCHASE_EXPIRED", "the approval's grant has expired")
			}
			method, ref = "webauthn", signer.String
		default:
			return refuse("PAYMENT_NOT_ALLOWED", "only an Allow, or an Ask a person approved, is paid")
		}
		if reason, err := s.recheck(t); err != nil {
			return err
		} else if reason != "" {
			return refuse(reason, "the task's credential no longer holds")
		}
		amount := p.terms().Amount
		// The budget left less what payments already requested will reserve:
		// two Allows decided before either reserved never go over it together.
		left := t.row.BudgetRemaining
		rows, err := tx.Query(`SELECT amount FROM payments WHERE task_id = ? AND state = 'requested'`, req.TaskID)
		if err != nil {
			return readErr("payment", req.TaskID, err)
		}
		var pending []contracts.Amount
		for rows.Next() {
			var a contracts.Amount
			if err := rows.Scan(&a); err != nil {
				rows.Close()
				return readErr("payment", req.TaskID, err)
			}
			pending = append(pending, a)
		}
		if err := drained(rows); err != nil {
			return readErr("payment", req.TaskID, err)
		}
		for _, a := range append(pending, amount) {
			if left, err = subtract(left, a); err != nil {
				return err
			}
		}
		if _, err := tx.Exec(`INSERT INTO payments (intent_hash, task_id, purchase_order_id, method, grant_ref, amount, state, created_at, updated_at)
			VALUES (?, ?, ?, ?, ?, ?, 'requested', ?, ?)`, p.Decision.IntentHash, req.TaskID, req.PurchaseOrderID, method, ref, string(amount), now, now); err != nil {
			return err
		}
		if err := t.emit("PAYMENT_REQUESTED", p, contracts.EventPayload{Payment: &contracts.PaymentRecord{Method: method, Amount: amount}}); err != nil {
			return err
		}
		if err := t.finish(); err != nil {
			return err
		}
		out, err = scanPaid(tx.QueryRow(paidWhere(`p.intent_hash = ?`), p.Decision.IntentHash))
		return err
	})
	return out, replay, err
}

// step is one move of a payment: from one of the states `from` to `to`,
// with the event it emits (or none), its payload, the columns it sets. The
// same move asked again with the same values is answered as it stands.
type step struct {
	from    []string
	to      string
	event   string
	payload func(p Payment) contracts.EventPayload
	set     map[string]any
	also    func(tx *sql.Tx, t *taskTx, p Payment, now uint64) error
	same    func(tx *sql.Tx, p Payment) error // a replay's check of what set does not hold
}

func (s *Store) move(intent string, st step) (Payment, error) {
	var out Payment
	err := s.write(func(tx *sql.Tx, now uint64) error {
		p, err := scanPaid(tx.QueryRow(paidWhere(`p.intent_hash = ?`), intent))
		if errors.Is(err, sql.ErrNoRows) {
			return refuse("UNKNOWN_PAYMENT", "no such payment")
		} else if err != nil {
			return err
		}
		if p.State == st.to {
			for col, v := range st.set { // the same move again: the same values
				var have sql.NullString
				if err := tx.QueryRow(`SELECT CAST(`+col+` AS TEXT) FROM payments WHERE intent_hash = ?`, intent).Scan(&have); err != nil {
					return err
				}
				if have.String != fmt.Sprint(v) {
					return refuse("IDEMPOTENCY_CONFLICT", "this payment step was recorded with other values")
				}
			}
			if st.same != nil {
				if err := st.same(tx, p); err != nil {
					return err
				}
			}
			out = p
			return nil
		}
		if !slices.Contains(st.from, p.State) {
			return refuse("INVALID_PAYMENT_TRANSITION", fmt.Sprintf("a payment %s cannot become %s", p.State, st.to))
		}
		cols, args := "state = ?, updated_at = ?", []any{st.to, now}
		for col, v := range st.set {
			cols += ", " + col + " = ?"
			args = append(args, v)
		}
		if _, err := tx.Exec(`UPDATE payments SET `+cols+` WHERE intent_hash = ?`, append(args, intent)...); err != nil {
			return err
		}
		if st.event != "" || st.also != nil {
			t, err := s.openTask(tx, now, p.TaskID)
			if err != nil {
				return err
			}
			if st.event != "" {
				if err := t.emit(st.event, t.purchases[p.PurchaseOrderID], st.payload(p)); err != nil {
					return err
				}
			}
			if st.also != nil {
				if err := st.also(tx, t, p, now); err != nil {
					return err
				}
			}
			if err := t.finish(); err != nil {
				return err
			}
		}
		out, err = scanPaid(tx.QueryRow(paidWhere(`p.intent_hash = ?`), intent))
		return err
	})
	return out, err
}

func record(r contracts.PaymentRecord) func(Payment) contracts.EventPayload {
	return func(Payment) contracts.EventPayload { return contracts.EventPayload{Payment: &r} }
}

// Reservation is what the budget relayer answered (tx#1): its evidence,
// passed on to the Signer as is, and its transaction.
type Reservation struct {
	Evidence json.RawMessage
	TxHash   string // 0x + 64 hex
	Block    uint64
}

// PaymentReserved records the reservation (BUDGET_RESERVED) and deducts the
// amount from the task's remaining budget, in one transaction.
func (s *Store) PaymentReserved(intent string, r Reservation) (Payment, error) {
	p, err := s.paymentByIntent(intent)
	if err != nil {
		return Payment{}, err
	}
	return s.move(intent, step{from: []string{"requested"}, to: "reserved", event: "BUDGET_RESERVED",
		payload: record(contracts.PaymentRecord{Method: p.Method, Amount: p.Amount, TxHash: r.TxHash, Block: r.Block}),
		set:     map[string]any{"reservation": string(r.Evidence), "reserve_tx": r.TxHash, "reserve_block": r.Block},
		also: func(tx *sql.Tx, t *taskTx, p Payment, _ uint64) error {
			left, err := subtract(t.row.BudgetRemaining, p.Amount)
			if err != nil {
				return err
			}
			t.row.BudgetRemaining = left
			_, err = tx.Exec(`UPDATE tasks SET budget_remaining = ? WHERE task_id = ?`, left, p.TaskID)
			return err
		}})
}

func subtract(have string, amount contracts.Amount) (string, error) {
	h, ok1 := new(big.Int).SetString(have, 10)
	a, ok2 := new(big.Int).SetString(string(amount), 10)
	if !ok1 || !ok2 || h.Cmp(a) < 0 {
		return "", refuse("OVER_BUDGET", "the reservation is more than the task's remaining budget")
	}
	return h.Sub(h, a).String(), nil
}

// PaymentSigned records the Signer's EIP-3009 authorization (PAYMENT_SIGNED):
// kept for the shop and for reconciliation.
func (s *Store) PaymentSigned(intent string, authorization json.RawMessage) (Payment, error) {
	p, err := s.paymentByIntent(intent)
	if err != nil {
		return Payment{}, err
	}
	return s.move(intent, step{from: []string{"reserved"}, to: "signed", event: "PAYMENT_SIGNED",
		payload: record(contracts.PaymentRecord{Method: p.Method}), set: map[string]any{"authorization": string(authorization)}})
}

// PaymentSubmitted records that the payment is about to be sent to the shop
// (PAYMENT_SUBMITTED): from here its result may be on chain.
func (s *Store) PaymentSubmitted(intent string) (Payment, error) {
	return s.move(intent, step{from: []string{"signed"}, to: "submitted", event: "PAYMENT_SUBMITTED",
		payload: func(Payment) contracts.EventPayload { return contracts.EventPayload{} }})
}

// PaymentUnknown records a submitted payment whose result the Host does not
// know (a restart, a timeout): only the chain moves it from here.
func (s *Store) PaymentUnknown(intent string) (Payment, error) {
	return s.move(intent, step{from: []string{"submitted"}, to: "unknown"})
}

// PaymentSettled records the settlement (tx#2, PAYMENT_SETTLED): the shop's
// PAYMENT-RESPONSE, or the Reconciler's finding for an unknown one.
func (s *Store) PaymentSettled(intent, txHash string, block uint64, paymentResponse json.RawMessage) (Payment, error) {
	set := map[string]any{"settle_tx": txHash, "settle_block": block}
	if paymentResponse != nil {
		set["payment_response"] = string(paymentResponse)
	}
	return s.move(intent, step{from: []string{"submitted", "unknown"}, to: "settled", event: "PAYMENT_SETTLED",
		payload: record(contracts.PaymentRecord{TxHash: txHash, Block: block}), set: set})
}

// ReceiptID is a payment's receipt's ID.
func ReceiptID(intent string) string {
	h := sha256.Sum256([]byte("agent-payment-authority/receipt\x00" + intent))
	return "rcpt-" + hex.EncodeToString(h[:16])
}

// PaymentDelivered records the delivery (RESOURCE_DELIVERED) and the
// receipt (RECEIPT_ISSUED, a receipts row) of a settled payment: the
// content's SHA-256 and size only, never the content.
func (s *Store) PaymentDelivered(intent, contentSHA256 string, size int) (Payment, error) {
	if size < 0 {
		return Payment{}, refuse("INVALID_PAYMENT_UPDATE", "content size")
	}
	id := ReceiptID(intent)
	return s.move(intent, step{from: []string{"settled"}, to: "delivered", event: "RESOURCE_DELIVERED",
		payload: record(contracts.PaymentRecord{ContentSHA256: contentSHA256}),
		same: func(tx *sql.Tx, p Payment) error {
			var sum string
			var n int
			if err := tx.QueryRow(`SELECT content_sha256, content_size FROM receipts WHERE intent_hash = ?`, intent).Scan(&sum, &n); err != nil {
				return readErr("payment", p.TaskID, err)
			}
			if sum != contentSHA256 || n != size {
				return refuse("IDEMPOTENCY_CONFLICT", "this payment was delivered with other content")
			}
			return nil
		},
		also: func(tx *sql.Tx, t *taskTx, p Payment, now uint64) error {
			var reserveTx string
			if err := t.emit("RECEIPT_ISSUED", t.purchases[p.PurchaseOrderID], contracts.EventPayload{Payment: &contracts.PaymentRecord{ContentSHA256: contentSHA256, ReceiptID: id}}); err != nil {
				return err
			}
			if err := tx.QueryRow(`SELECT reserve_tx FROM payments WHERE intent_hash = ?`, intent).Scan(&reserveTx); err != nil {
				return err
			}
			resp := string(p.PaymentResponse)
			_, err := tx.Exec(`INSERT INTO receipts VALUES (?, ?, ?, ?, ?, ?, ?, ?)`, id, intent, reserveTx, p.SettleTx, resp, contentSHA256, size, now)
			return err
		}})
}

// PaymentFailed ends a payment that will not complete: one never reserved
// spent nothing (PAYMENT_NOT_SETTLED: RESERVE_FAILED, NOT_PAID_EXPIRED);
// one reserved and not settled, or settled and not delivered
// (DELIVERY_FAILED), is a person's to look at (PAYMENT_NEEDS_REVIEW).
func (s *Store) PaymentFailed(intent, code string) (Payment, error) {
	codeOnly := func(Payment) contracts.EventPayload { return contracts.EventPayload{Code: code} }
	switch {
	case slices.Contains(paycontract.NotSettledCodes, code):
		to := map[string]string{"RESERVE_FAILED": "reserve_failed", "NOT_PAID_EXPIRED": "not_paid_expired"}[code]
		return s.move(intent, step{from: []string{"requested"}, to: to, event: "PAYMENT_NOT_SETTLED", payload: codeOnly, set: map[string]any{"code": code}})
	case code == "DELIVERY_FAILED":
		return s.move(intent, step{from: []string{"settled"}, to: "needs_review", event: "PAYMENT_NEEDS_REVIEW", payload: codeOnly, set: map[string]any{"code": code}})
	case slices.Contains(paycontract.NeedsReviewCodes, code):
		return s.move(intent, step{from: []string{"reserved", "signed", "submitted", "unknown"}, to: "needs_review", event: "PAYMENT_NEEDS_REVIEW",
			payload: codeOnly, set: map[string]any{"code": code}})
	}
	return Payment{}, refuse("INVALID_PAYMENT_UPDATE", "unknown payment failure code")
}

func (s *Store) paymentByIntent(intent string) (Payment, error) {
	p, err := scanPaid(s.r.QueryRow(paidWhere(`p.intent_hash = ?`), intent))
	if errors.Is(err, sql.ErrNoRows) {
		return Payment{}, refuse("UNKNOWN_PAYMENT", "no such payment")
	}
	return p, err
}

// PaymentsIn lists the payments in the given states, oldest first: the
// payment worker's queue and its recovery on start.
func (s *Store) PaymentsIn(states ...string) ([]Payment, error) {
	if len(states) == 0 {
		return nil, nil
	}
	in, args := "?", []any{states[0]}
	for _, st := range states[1:] {
		in, args = in+", ?", append(args, st)
	}
	rows, err := s.r.Query(paidWhere(`p.state IN (`+in+`)`)+` ORDER BY p.created_at, p.intent_hash`, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Payment
	for rows.Next() {
		p, err := scanPaid(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, p)
	}
	return out, rows.Err()
}

// PaymentOf reads one purchase's payment for its consumer; another
// consumer's is answered as unknown.
func (s *Store) PaymentOf(consumerID, taskID, po string) (Payment, error) {
	p, err := scanPaid(s.r.QueryRow(paidWhere(`p.task_id = ? AND p.purchase_order_id = ?`), taskID, po))
	if errors.Is(err, sql.ErrNoRows) || err == nil && p.ConsumerID != consumerID {
		return Payment{}, refuse("UNKNOWN_PAYMENT", "no such payment for this consumer")
	}
	return p, err
}

// PaymentView is a purchase's payment as its events record it.
type PaymentView struct {
	Status        string `json:"status"` // PaymentStatus
	State         string `json:"state"`  // submitted also when the Host does not know its result
	Code          string `json:"code,omitempty"`
	ReserveTx     string `json:"reserve_tx,omitempty"`
	SettleTx      string `json:"settle_tx,omitempty"`
	SettleBlock   uint64 `json:"settle_block,omitempty"`
	ContentSHA256 string `json:"content_sha256,omitempty"`
	ReceiptID     string `json:"receipt_id,omitempty"`
}

var eventState = map[string]string{"PAYMENT_REQUESTED": "requested", "BUDGET_RESERVED": "reserved", "PAYMENT_SIGNED": "signed", "PAYMENT_SUBMITTED": "submitted",
	"PAYMENT_SETTLED": "settled", "RESOURCE_DELIVERED": "delivered", "RECEIPT_ISSUED": "delivered", "PAYMENT_NEEDS_REVIEW": "needs_review"}

// paymentView is po's payment from the task's events; nil before any.
func (t *taskTx) paymentView(po string) *PaymentView {
	var v *PaymentView
	for _, e := range t.events {
		if e.Scope == nil || e.Scope.PurchaseOrderID != po || !slices.Contains(paycontract.PaymentEvents, e.EventType) {
			continue
		}
		if v == nil {
			v = &PaymentView{}
		}
		v.State, v.Code = eventState[e.EventType], e.Payload.Code
		if e.EventType == "PAYMENT_NOT_SETTLED" {
			v.State = map[string]string{"RESERVE_FAILED": "reserve_failed", "NOT_PAID_EXPIRED": "not_paid_expired"}[e.Payload.Code]
		}
		if r := e.Payload.Payment; r != nil {
			switch e.EventType {
			case "BUDGET_RESERVED":
				v.ReserveTx = r.TxHash
			case "PAYMENT_SETTLED":
				v.SettleTx, v.SettleBlock = r.TxHash, r.Block
			case "RESOURCE_DELIVERED":
				v.ContentSHA256 = r.ContentSHA256
			case "RECEIPT_ISSUED":
				v.ReceiptID = r.ReceiptID
			}
		}
	}
	if v != nil {
		v.Status = PaymentStatus(v.State, v.Code)
	}
	return v
}

// lastPaymentEvent is each state's last payment event in its purchase's chain.
var lastPaymentEvent = map[string]string{"requested": "PAYMENT_REQUESTED", "reserved": "BUDGET_RESERVED", "signed": "PAYMENT_SIGNED", "submitted": "PAYMENT_SUBMITTED",
	"unknown": "PAYMENT_SUBMITTED", "settled": "PAYMENT_SETTLED", "delivered": "RECEIPT_ISSUED", "reserve_failed": "PAYMENT_NOT_SETTLED",
	"not_paid_expired": "PAYMENT_NOT_SETTLED", "needs_review": "PAYMENT_NEEDS_REVIEW"}

// verifyPaid is schema 7's recovery check for one task: every payment's
// state is its purchase's payment events (and no payment events without
// one), its method the purchase's authorization, and the task's remaining
// budget its initial one less what was reserved.
func (t *taskTx) verifyPaid(initial contracts.Amount) string {
	rows, err := t.tx.Query(`SELECT intent_hash, purchase_order_id, method, grant_ref, amount, state, coalesce(code, '') FROM payments WHERE task_id = ?`, t.row.TaskID)
	if err != nil {
		return "payments: " + err.Error()
	}
	type row struct{ intent, po, method, ref, amount, state, code string }
	var ps []row
	for rows.Next() {
		var r row
		if err := rows.Scan(&r.intent, &r.po, &r.method, &r.ref, &r.amount, &r.state, &r.code); err != nil {
			rows.Close()
			return "payments: " + err.Error()
		}
		ps = append(ps, r)
	}
	if err := drained(rows); err != nil {
		return "payments: " + err.Error()
	}
	last := map[string]string{}
	for _, e := range t.events {
		if slices.Contains(paycontract.PaymentEvents, e.EventType) {
			last[e.Scope.PurchaseOrderID] = e.EventType
		}
	}
	reserved := new(big.Int)
	for _, r := range ps {
		p := t.purchases[r.po]
		switch {
		case p == nil || p.Decision.IntentHash != r.intent || r.amount != string(p.terms().Amount):
			return "payment " + r.intent + " is not its purchase's"
		case r.method == "delegation" && p.Decision.PolicyVerdict != "Allow",
			r.method == "webauthn" && (p.Session == nil || p.Session.State != "approved"):
			return "payment " + r.po + "'s method is not its purchase's authorization"
		case r.method == "webauthn":
			var m string
			var signer sql.NullString
			if err := t.tx.QueryRow(`SELECT authorization_method, signer_approval_id FROM grants WHERE approval_session_id = ?`, p.Session.ID).Scan(&m, &signer); err != nil {
				return "payment " + r.po + ": its grant: " + err.Error()
			}
			if m != "webauthn" || signer.String != r.ref {
				return "payment " + r.po + " is not its purchase's webauthn grant"
			}
		}
		switch {
		case last[r.po] != lastPaymentEvent[r.state]:
			return fmt.Sprintf("payment %s is %s but its last payment event is %q", r.po, r.state, last[r.po])
		}
		if r.state != "requested" && r.state != "reserve_failed" && r.state != "not_paid_expired" {
			a, _ := new(big.Int).SetString(r.amount, 10)
			reserved.Add(reserved, a)
		}
		delete(last, r.po)
	}
	for po := range last {
		return "purchase " + po + " has payment events and no payment"
	}
	want, ok := new(big.Int).SetString(string(initial), 10)
	if !ok || want.Sub(want, reserved).String() != t.row.BudgetRemaining {
		return fmt.Sprintf("budget_remaining %s is not budget_initial %s less %s reserved", t.row.BudgetRemaining, initial, reserved)
	}
	return ""
}

// verifyReceipts is schema 7's table check: a receipt per delivered
// payment and none other.
func verifyReceipts(tx *sql.Tx) error {
	var n int
	if err := tx.QueryRow(`SELECT (SELECT count(*) FROM payments p LEFT JOIN receipts r USING (intent_hash) WHERE (p.state = 'delivered') != (r.intent_hash IS NOT NULL))
		+ (SELECT count(*) FROM receipts r LEFT JOIN payments p USING (intent_hash) WHERE p.intent_hash IS NULL OR r.settle_tx != p.settle_tx OR r.reserve_tx != p.reserve_tx)`).Scan(&n); err != nil {
		return readErr("payment", "", err)
	}
	if n > 0 {
		return &CorruptError{Check: "payment", Detail: "a receipt is missing for a delivered payment, or names another payment"}
	}
	return nil
}
