package store

// apa.pay.v1 in the Store (docs/commercial-transaction-flow.md sections 7,
// 9-11; P1-a): a delegation confirmed before any task, bound to the task in
// StartTask's transaction; a v1 purchase decided by paypolicy, whose Ask
// opens its approval session in the same transaction; and the approval:
// one write transaction checks the session, its deadline, the approver,
// the intent the approver saw and the current quote, then closes the
// session and, when approved, writes the simulated grant. Nothing here can
// pay: a simulated grant gives no payment capability.

import (
	"cmp"
	"database/sql"
	"errors"
	"fmt"
	"slices"
	"strings"

	"github.com/SIMPLYBOYS/agent-payment-authority/internal/contracts"
	"github.com/SIMPLYBOYS/agent-payment-authority/internal/paycontract"
	"github.com/SIMPLYBOYS/agent-payment-authority/internal/paypolicy"
	"github.com/SIMPLYBOYS/agent-payment-authority/internal/policy"
)

// specs is a pay-family version's spec; an unknown version gets the zero
// Spec, whose IDs never match a stored one.
func specs(version string) paycontract.Spec { s, _ := paycontract.SpecOf(version); return s }

// pay reports a pay-family task (apa.pay.v1 or apa.pay.v2); spec is its
// contract's rules (C2a, docs/commercial-c2-design.md 3.1).
func (t *taskTx) pay() bool { return paycontract.Supported(t.row.ContractVersion) }

func (t *taskTx) spec() paycontract.Spec { s, _ := paycontract.SpecOf(t.row.ContractVersion); return s }

func (t *taskTx) genesis() (string, error) {
	if t.pay() {
		return t.spec().GenesisHash(t.row.TaskID)
	}
	return contracts.GenesisHash(t.row.TaskID)
}

func (t *taskTx) eventHash(e contracts.AuditEvent) (string, error) {
	if t.pay() {
		return paycontract.EventHash(e)
	}
	return contracts.EventHash(e)
}

// validateEvent applies the task's own contract to one event; M1 tasks keep
// the M1 rules unchanged.
func (t *taskTx) validateEvent(e contracts.AuditEvent) error {
	var p *purchaseRow
	if po := e.Scope.PurchaseOrderID; po != "" {
		if p = t.purchases[po]; p == nil {
			return fmt.Errorf("event names purchase %s, which is not stored", po)
		}
	}
	if t.pay() {
		var order *paycontract.PurchaseOrder
		if p != nil {
			order = p.V1
		}
		return paycontract.ValidateEvent(t.reg, e, order)
	}
	var order *contracts.PurchaseOrder
	if p != nil {
		order = &p.Order
	}
	return t.reg.ValidateEvent(e, order)
}

func (t *taskTx) validateOrder() error {
	if t.pay() {
		return paycontract.ValidateEventOrder(t.events)
	}
	return contracts.ValidateEventOrder(t.events)
}

// sessionRow is one approval_sessions row, with its grant once approved.
type sessionRow struct {
	ID, TaskID, PO, ConsumerID, IntentHash string
	OpenedAt, ExpiresAt                    uint64
	State                                  string
	ResolveRequestID, ResolvedBy           sql.NullString
	ClosedAt                               sql.NullInt64
	Grant                                  *GrantView
	grantBody                              string
	grantIssuedAt                          uint64 // the grants row's issued_at column
}

const sessionCols = `approval_session_id, task_id, purchase_order_id, consumer_id, intent_hash, opened_at, expires_at, state, resolve_request_id, resolved_by, closed_at`

func scanSession(row interface{ Scan(...any) error }) (sessionRow, error) {
	var s sessionRow
	err := row.Scan(&s.ID, &s.TaskID, &s.PO, &s.ConsumerID, &s.IntentHash, &s.OpenedAt, &s.ExpiresAt, &s.State, &s.ResolveRequestID, &s.ResolvedBy, &s.ClosedAt)
	return s, err
}

// loadSessions attaches each Ask's session and grant, and the bound
// delegation's ID, to an apa.pay.v1 task read in t's transaction.
func (t *taskTx) loadSessions() error {
	id := t.row.TaskID
	if err := t.tx.QueryRow(`SELECT delegation_id FROM delegations WHERE task_id = ?`, id).Scan(&t.delegation); err != nil && !errors.Is(err, sql.ErrNoRows) {
		return readErr("task", id, err)
	}
	rows, err := t.tx.Query(`SELECT `+sessionCols+` FROM approval_sessions WHERE task_id = ?`, id)
	if err != nil {
		return readErr("task", id, err)
	}
	defer rows.Close()
	for rows.Next() {
		ss, err := scanSession(rows)
		if err != nil {
			return readErr("task", id, err)
		}
		p := t.purchases[ss.PO]
		if p == nil {
			return &CorruptError{Check: "approval", TaskID: id, Detail: "session " + ss.ID + " names no stored purchase"}
		}
		p.Session = &ss
	}
	if err := drained(rows); err != nil {
		return readErr("task", id, err)
	}
	grows, err := t.tx.Query(`SELECT g.approval_session_id, g.body, g.issued_at FROM grants g JOIN approval_sessions s USING (approval_session_id) WHERE s.task_id = ?`, id)
	if err != nil {
		return readErr("task", id, err)
	}
	defer grows.Close()
	for grows.Next() {
		var sid, body string
		var issued uint64
		if err := grows.Scan(&sid, &body, &issued); err != nil {
			return readErr("task", id, err)
		}
		var g paycontract.AuthorizationGrant
		if err := contracts.Decode([]byte(body), &g); err != nil {
			return err
		}
		for _, p := range t.purchases {
			if p.Session != nil && p.Session.ID == sid {
				p.Session.Grant = &GrantView{JTI: g.JTI, AuthorizationMethod: g.AuthorizationMethod, Audience: g.Audience, IssuedAt: g.IssuedAt, ExpiresAt: g.ExpiresAt}
				p.Session.grantBody, p.Session.grantIssuedAt = body, issued
			}
		}
	}
	return drained(grows)
}

// sessionDeadline is an apa.pay.v1 Ask's approval deadline, from what is
// stored and verified, never from the session row: the decision time plus
// the terms' approval seconds, but never past the payment binding's
// valid_before (a payment could not be made after it anyway).
func (t *taskTx) sessionDeadline(p *purchaseRow) uint64 {
	return min(p.DecidedAt+t.terms.ApprovalDeadlineSeconds, p.V1.PaymentBinding.ValidBefore)
}

// resolver is who closed a session by deciding it: the approval request's
// ID, the approver and the method; signer is the Signer's approval ID of a
// webauthn decision (A2).
type resolver struct{ requestID, by, method, signer string }

func (t *taskTx) closeSession(p *purchaseRow, state string, by *resolver) error {
	ss := p.Session
	if ss == nil {
		return errors.New("an apa.pay.v1 Ask has no approval session")
	}
	var req, who any
	if by != nil {
		req, who = by.requestID, by.by
	}
	res, err := t.tx.Exec(`UPDATE approval_sessions SET state = ?, closed_at = ?, resolve_request_id = ?, resolved_by = ? WHERE approval_session_id = ? AND state = 'pending'`,
		state, t.now, req, who, ss.ID)
	if uniqueViolation(err) {
		return refuse("IDEMPOTENCY_CONFLICT", "this approval request ID already decided another session")
	} else if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n != 1 {
		return errors.New("approval session was already closed")
	}
	ss.State, ss.ClosedAt = state, sql.NullInt64{Int64: int64(t.now), Valid: true}
	if by != nil {
		ss.ResolveRequestID, ss.ResolvedBy = sql.NullString{String: by.requestID, Valid: true}, sql.NullString{String: by.by, Valid: true}
	}
	return nil
}

// taskContract checks setup's contract version and, for the pay family, its
// delegation of the same version, inside StartTask's transaction. It narrows
// setup to the delegation: the budget, allow_max, merchants and approval
// deadline the consumer confirmed, never wider than the host's profile. An
// apa.pay.v1 task needs the fixture credential; an apa.pay.v2 task its
// BBS+ credential, issued for at most MaxCredentialTTL, or a declared absent
// one, and the Store's credential directory to verify against.
func (s *Store) taskContract(tx *sql.Tx, now uint64, setup *TaskSetup, task contracts.Task) (string, *paycontract.Delegation, error) {
	switch setup.ContractVersion {
	case "", contracts.Version:
		if setup.DelegationID != "" {
			return "", nil, refuse("INVALID_TASK_SETUP", "an apa.m1.v2 task has no delegation")
		}
		return contracts.Version, nil, nil
	case paycontract.Version, paycontract.V2:
	default:
		return "", nil, refuse("INVALID_TASK_SETUP", "unknown contract version")
	}
	v2 := setup.ContractVersion == paycontract.V2
	d, bound, err := readDelegation(tx, setup.DelegationID)
	switch {
	case err != nil:
		return "", nil, err
	case d == nil || d.ConsumerID != task.ConsumerID:
		return "", nil, refuse("DELEGATION_UNAVAILABLE", "no confirmed delegation of this consumer")
	case bound || now >= d.ExpiresAt:
		return "", nil, refuse("DELEGATION_UNAVAILABLE", "the delegation is already bound or has expired")
	case d.Version != setup.ContractVersion:
		return "", nil, refuse("DELEGATION_UNAVAILABLE", "the delegation is another contract version's")
	}
	p := setup.Profile
	switch b := setup.BBS; {
	case p == nil || !v2 && setup.Credential == nil:
		return "", nil, refuse("INVALID_TASK_SETUP", "an apa.pay.v1 task needs the profile and the credential")
	case v2 && (s.creds == nil || s.verifier == ""):
		return "", nil, refuse("INVALID_TASK_SETUP", "an apa.pay.v2 task needs the Store's credential directory and verifier ID")
	case b != nil && (b.Expiry <= now || b.Expiry > now+MaxCredentialTTL):
		return "", nil, refuse("INVALID_TASK_SETUP", "the credential's expiry is not within the TTL from now")
	}
	limit, _ := p.TaskCap.Units()
	allow, _ := p.AllowMax.Units()
	budget, _ := d.TaskBudget.Units()
	narrow, _ := d.AllowMax.Units()
	if budget > limit || narrow > allow || d.AssetID != p.AssetID || slices.ContainsFunc(d.Merchants, func(m string) bool { return !slices.Contains(p.Merchants, m) }) {
		return "", nil, refuse("DELEGATION_UNAVAILABLE", "the delegation is wider than the host's profile")
	}
	narrowed := *p
	narrowed.AllowMax, narrowed.Merchants = d.AllowMax, slices.Clone(d.Merchants)
	setup.Profile, setup.BudgetInitial, setup.AssetID, setup.ApprovalDeadlineSeconds = &narrowed, d.TaskBudget, d.AssetID, d.ApprovalDeadlineSeconds
	return setup.ContractVersion, d, nil
}

func readDelegation(q interface {
	QueryRow(string, ...any) *sql.Row
}, id string) (*paycontract.Delegation, bool, error) {
	var body string
	var task sql.NullString
	err := q.QueryRow(`SELECT body, task_id FROM delegations WHERE delegation_id = ?`, id).Scan(&body, &task)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, false, nil
	} else if err != nil {
		return nil, false, err
	}
	var d paycontract.Delegation
	if err := contracts.Decode([]byte(body), &d); err != nil {
		return nil, false, err
	}
	return &d, task.Valid, nil
}

// DelegationRequest is a consumer's confirmation of the host's proposal.
// Every term is the host's (catalog and profile); RequestID makes a resend
// the same delegation; ConfirmedBy is the trusted simulated approver.
type DelegationRequest struct {
	ContractVersion                                                string // the scenario's pay-family version ("" = apa.pay.v1)
	RequestID, ConsumerID, Scenario, Purpose, AssetID, ConfirmedBy string
	Merchants                                                      []string
	TaskBudget, AllowMax                                           contracts.Amount
	ApprovalDeadlineSeconds, ValidSeconds                          uint64
}

// DelegationView is a delegation as stored, and the task bound to it.
type DelegationView struct {
	paycontract.Delegation
	TaskID string `json:"task_id,omitempty"`
}

// ConfirmDelegation records a confirmed delegation. A resend of the same
// request returns the same delegation; the same request ID with other
// terms is IDEMPOTENCY_CONFLICT. No task is created.
func (s *Store) ConfirmDelegation(req DelegationRequest) (DelegationView, bool, error) {
	var out DelegationView
	replay := false
	if !requestIDPattern.MatchString(req.RequestID) {
		return out, false, refuse("INVALID_DELEGATION", "request_id")
	}
	version := cmp.Or(req.ContractVersion, paycontract.Version)
	sp, ok := paycontract.SpecOf(version)
	if !ok {
		return out, false, refuse("INVALID_DELEGATION", "contract version")
	}
	err := s.write(func(tx *sql.Tx, now uint64) error {
		id := sp.DelegationID(req.RequestID)
		d := paycontract.Delegation{Version: version, DelegationID: id, ConsumerID: req.ConsumerID, Scenario: req.Scenario, Purpose: req.Purpose,
			Merchants: req.Merchants, AssetID: req.AssetID, TaskBudget: req.TaskBudget, AllowMax: req.AllowMax, ApprovalDeadlineSeconds: req.ApprovalDeadlineSeconds,
			ConfirmedBy: req.ConfirmedBy, ConfirmMethod: "simulated", ConfirmedAt: now, ExpiresAt: now + req.ValidSeconds}
		old, bound, err := readDelegation(tx, id)
		if err != nil {
			return err
		}
		if old != nil {
			d.ConfirmedAt, d.ExpiresAt = old.ConfirmedAt, old.ConfirmedAt+req.ValidSeconds
			a, _ := canonical(d)
			b, _ := canonical(*old)
			if string(a) != string(b) {
				return refuse("IDEMPOTENCY_CONFLICT", "this request ID confirmed other terms")
			}
			out, replay = DelegationView{Delegation: *old}, true
			if bound {
				tx.QueryRow(`SELECT task_id FROM delegations WHERE delegation_id = ?`, id).Scan(&out.TaskID)
			}
			return nil
		}
		if !slices.ContainsFunc(s.consumers(), func(c string) bool { return c == req.ConsumerID }) {
			return refuse("INVALID_DELEGATION", "unknown consumer")
		}
		if err := d.Validate(); err != nil {
			return refuse("INVALID_DELEGATION", paycontract.Code(err))
		}
		body, err := canonical(d)
		if err != nil {
			return err
		}
		if _, err := tx.Exec(`INSERT INTO delegations (delegation_id, request_id, consumer_id, body, confirmed_at, expires_at) VALUES (?, ?, ?, ?, ?, ?)`,
			id, req.RequestID, req.ConsumerID, string(body), now, d.ExpiresAt); err != nil {
			return err
		}
		out = DelegationView{Delegation: d}
		return nil
	})
	return out, replay, err
}

// consumers are the trusted registry's consumer IDs.
func (s *Store) consumers() []string {
	var cfg contracts.RegistryConfig
	if contracts.Decode(s.regBody, &cfg) != nil {
		return nil
	}
	out := make([]string, 0, len(cfg.Consumers))
	for _, c := range cfg.Consumers {
		out = append(out, c.ConsumerID)
	}
	return out
}

// submitV1 is Submit for an apa.pay.v1 task, in the same order as M1's:
// ownership, expiry, replay or conflict, offer book, active task, the
// decision (paypolicy), the write. An Ask opens its approval session in the
// same transaction; its deadline never passes the binding's valid_before.
func (s *Store) submitV1(t *taskTx, sess contracts.Session, po paycontract.PurchaseOrder, out *Result) error {
	if err := paycontract.ValidatePurchase(t.reg, sess, po); err != nil {
		return contractRefusal(err)
	}
	if err := t.expire(); err != nil {
		return err
	}
	key, err := po.RetryKey()
	if err != nil {
		return contractRefusal(err)
	}
	i, b := po.Intent, po.PaymentBinding
	existing, err := s.related(t, key, purchaseTerms{PO: i.PurchaseOrderID, ChainID: b.ChainID, Asset: b.AssetContract, From: b.From, Nonce: b.Nonce})
	if err != nil {
		return err
	}
	for _, e := range existing {
		if e.V1 == nil {
			return refuse("IDEMPOTENCY_CONFLICT", "a stored purchase already holds this retry key, purchase_order_id or nonce")
		}
		switch kind, err := paycontract.CompareRetry(*e.V1, po); {
		case err != nil:
			return contractRefusal(err)
		case kind == "replay":
			*out = Result{Replay: true, Purchase: t.view(t.purchases[i.PurchaseOrderID])}
			return t.finish()
		}
	}
	if !s.offered(i.HTTPMethod, i.ResourceURL, po.Quote) {
		return refuse("QUOTE_NOT_OFFERED", "quote is not in the current trusted offer book")
	}
	status, _ := project(t.events)
	if err := contracts.CheckTaskActive(status); err != nil {
		return contractRefusal(err)
	}
	in := paypolicy.Input{Session: sess, Purchase: po, ContractVersion: t.row.ContractVersion, TaskStatus: status,
		Budget: policy.Budget{TaskID: t.row.TaskID, Remaining: contracts.Amount(t.row.BudgetRemaining)}, Now: t.now}
	if in.Profile, in.Credential, err = t.settings(); err != nil {
		return err
	}
	d, err := paypolicy.Decide(t.reg, in)
	if err != nil {
		return contractRefusal(err)
	}
	body, err := canonical(po)
	if err != nil {
		return err
	}
	p := &purchaseRow{V1: &po, Body: string(body), Decision: d, DecidedAt: t.now}
	if err := t.insertPurchase(p, key); err != nil {
		return err
	}
	if err := s.at("after-purchase"); err != nil {
		return err
	}
	if err := t.emit("PURCHASE_PROPOSED", p, contracts.EventPayload{}); err != nil {
		return err
	}
	if err := t.emit("POLICY_DECIDED", p, contracts.EventPayload{Decision: &d}); err != nil {
		return err
	}
	if d.PolicyVerdict == "Ask" {
		ss := &sessionRow{ID: t.spec().SessionID(t.row.TaskID, i.PurchaseOrderID), TaskID: t.row.TaskID, PO: i.PurchaseOrderID,
			ConsumerID: t.row.ConsumerID, IntentHash: d.IntentHash, OpenedAt: t.now, State: "pending"}
		ss.ExpiresAt = t.sessionDeadline(p)
		if _, err := t.tx.Exec(`INSERT INTO approval_sessions (approval_session_id, task_id, purchase_order_id, consumer_id, intent_hash, opened_at, expires_at, state)
			VALUES (?, ?, ?, ?, ?, ?, ?, 'pending')`, ss.ID, ss.TaskID, ss.PO, ss.ConsumerID, ss.IntentHash, ss.OpenedAt, ss.ExpiresAt); err != nil {
			return err
		}
		p.Session = ss
		if err := t.emit("APPROVAL_SESSION_OPENED", p, contracts.EventPayload{ApprovalSessionID: ss.ID}); err != nil {
			return err
		}
	}
	if err := t.finish(); err != nil {
		return err
	}
	*out = Result{Purchase: t.view(p)}
	return s.at("after-events")
}

// ApprovalRequest is one approve or deny of an approval session. ConsumerID
// and Approver come from the trusted simulated-approver capability, never
// the request body; IntentHash is the summary the approver saw.
type ApprovalRequest struct {
	RequestID, SessionID, ConsumerID, IntentHash string
	Decision                                     string // approve | deny
	Approver                                     string
}

// ApprovalResult is the session after the request. Outcome is approved or
// denied when this request (or its resend: Replay) decided it; otherwise
// the session was already closed (expired, withdrawn, superseded, or decided
// by another request) and Outcome is "closed", or the quote changed and
// this request superseded it ("superseded").
type ApprovalResult struct {
	Outcome  string                          `json:"outcome"`
	Replay   bool                            `json:"replay"`
	TaskID   string                          `json:"task_id"`
	Purchase PurchaseView                    `json:"purchase"`
	Grant    *paycontract.AuthorizationGrant `json:"grant,omitempty"`
}

// ResolveApproval decides an approval session in one write transaction
// (section 10 row 4): the session must be the approver's consumer's (else,
// as for an unknown one, OWNERSHIP_MISMATCH); a resend of the request that
// decided it returns that result; deadlines reached are recorded first and
// a closed session is not decided again; the intent the approver saw must
// be the session's (INTENT_MISMATCH, nothing changes); a quote no longer
// offered supersedes the session. Approving closes the session and writes
// the one simulated grant, together.
func (s *Store) ResolveApproval(req ApprovalRequest) (ApprovalResult, error) {
	var out ApprovalResult
	if !requestIDPattern.MatchString(req.RequestID) || (req.Decision != "approve" && req.Decision != "deny") || req.Approver == "" {
		return out, refuse("INVALID_APPROVAL", "request_id, decision or approver")
	}
	err := s.write(func(tx *sql.Tx, now uint64) error {
		ss, err := scanSession(tx.QueryRow(`SELECT `+sessionCols+` FROM approval_sessions WHERE approval_session_id = ?`, req.SessionID))
		if errors.Is(err, sql.ErrNoRows) || err == nil && ss.ConsumerID != req.ConsumerID {
			return refuse("OWNERSHIP_MISMATCH", "no such approval session for this approver")
		} else if err != nil {
			return err
		}
		t, err := s.openTask(tx, now, ss.TaskID)
		if err != nil {
			return err
		}
		p := t.purchases[ss.PO]
		out.TaskID = t.row.TaskID
		if p == nil || p.Session == nil || !t.pay() {
			return &CorruptError{Check: "approval", TaskID: ss.TaskID, Detail: "session without its apa.pay.v1 purchase"}
		}
		// A resend of the request that decided this session: the same answer.
		if p.Session.ResolveRequestID.String == req.RequestID {
			want := map[string]string{"approve": "approved", "deny": "denied"}[req.Decision]
			if p.Session.State != want || p.Session.IntentHash != req.IntentHash {
				return refuse("IDEMPOTENCY_CONFLICT", "this approval request ID decided the session otherwise")
			}
			out.Outcome, out.Replay, out.Purchase = want, true, t.view(p)
			out.Grant, err = grantOf(p.Session)
			return err
		}
		var other int
		if err := tx.QueryRow(`SELECT count(*) FROM approval_sessions WHERE resolve_request_id = ?`, req.RequestID).Scan(&other); err != nil {
			return err
		}
		if other > 0 {
			return refuse("IDEMPOTENCY_CONFLICT", "this approval request ID already decided another session")
		}
		if err := t.expire(); err != nil {
			return err
		}
		if p.Session.State != "pending" {
			out.Outcome, out.Purchase = "closed", t.view(p)
			return t.finish() // the deadline just recorded, if any
		}
		if req.IntentHash != p.Session.IntentHash {
			return refuse("INTENT_MISMATCH", "the approved summary is not this session's intent")
		}
		if !s.offered(p.V1.Intent.HTTPMethod, p.V1.Intent.ResourceURL, p.V1.Quote) {
			if err := t.close(p, "APPROVAL_SUPERSEDED", "superseded", "QUOTE_CHANGED", nil); err != nil {
				return err
			}
			out.Outcome = "superseded"
		} else if req.Decision == "deny" {
			if err := t.close(p, "APPROVAL_DENIED", "denied", "APPROVER_DENIED", &resolver{req.RequestID, req.Approver, "simulated", ""}); err != nil {
				return err
			}
			out.Outcome = "denied"
		} else {
			if err := t.close(p, "APPROVED", "approved", "SIMULATED_APPROVAL", &resolver{req.RequestID, req.Approver, "simulated", ""}); err != nil {
				return err
			}
			g := paycontract.AuthorizationGrant{Version: t.row.ContractVersion, JTI: t.spec().GrantID(p.Session.ID), IntentHash: p.Session.IntentHash,
				ApprovalSessionID: p.Session.ID, Decision: "approve", AuthorizationMethod: "simulated", Issuer: paycontract.Issuer, Audience: "signer",
				ApproverID: req.Approver, IssuedAt: now, ExpiresAt: p.V1.PaymentBinding.ValidBefore} // pending here means now < the deadline <= valid_before
			if err := g.Validate(); err != nil {
				return err
			}
			body, err := canonical(g)
			if err != nil {
				return err
			}
			if _, err := tx.Exec(`INSERT INTO grants (jti, approval_session_id, intent_hash, authorization_method, body, issued_at) VALUES (?, ?, ?, ?, ?, ?)`, g.JTI, g.ApprovalSessionID, g.IntentHash, g.AuthorizationMethod, string(body), now); uniqueViolation(err) {
				return refuse("IDEMPOTENCY_CONFLICT", "this session already has a grant")
			} else if err != nil {
				return err
			}
			if err := s.at("after-grant"); err != nil {
				return err
			}
			if err := t.emit("GRANT_ISSUED", p, contracts.EventPayload{ApprovalSessionID: p.Session.ID, GrantID: g.JTI, AuthorizationMethod: "simulated"}); err != nil {
				return err
			}
			p.Session.Grant = &GrantView{JTI: g.JTI, AuthorizationMethod: g.AuthorizationMethod, Audience: g.Audience, IssuedAt: g.IssuedAt, ExpiresAt: g.ExpiresAt}
			out.Outcome, out.Grant = "approved", &g
		}
		if err := t.finish(); err != nil {
			return err
		}
		out.Purchase = t.view(p)
		return nil
	})
	return out, err
}

func grantOf(ss *sessionRow) (*paycontract.AuthorizationGrant, error) {
	if ss.grantBody == "" {
		return nil, nil
	}
	var g paycontract.AuthorizationGrant
	return &g, contracts.Decode([]byte(ss.grantBody), &g)
}

// ApprovalItem is one of a consumer's approval sessions as the simulated
// approver sees it: the session and the purchase summary it was opened for,
// read from the stored task; the delegation's terms when there is one.
type ApprovalItem struct {
	TaskID     string           `json:"task_id"`
	ConsumerID string           `json:"consumer_id"`
	WalletID   string           `json:"wallet_id"`
	Payer      string           `json:"payer"`
	Purchase   PurchaseView     `json:"purchase"`
	Delegation *DelegationView  `json:"delegation,omitempty"`
	Attempt    *ApprovalAttempt `json:"attempt,omitempty"` // A2: the session's latest webauthn attempt
}

// Approvals lists consumerID's approval sessions, newest first, at most
// limit, read only (nothing expires here; a deadline past served_at with a
// pending state is said by the caller to await the backend).
func (s *Store) Approvals(consumerID string, limit int) ([]ApprovalItem, error) {
	tx, err := s.r.Begin()
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	rows, err := tx.Query(`SELECT DISTINCT task_id FROM approval_sessions WHERE consumer_id = ? ORDER BY opened_at DESC, task_id DESC LIMIT ?`, consumerID, limit)
	if err != nil {
		return nil, &ReadError{Check: "approval", Err: err}
	}
	var tasks []string
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			rows.Close()
			return nil, &ReadError{Check: "approval", Err: err}
		}
		tasks = append(tasks, id)
	}
	if err := drained(rows); err != nil {
		return nil, &ReadError{Check: "approval", Err: err}
	}
	out := []ApprovalItem{}
	for _, id := range tasks {
		items, err := s.itemsOf(tx, id, consumerID)
		if err != nil {
			return nil, err
		}
		out = append(out, items...)
	}
	slices.SortFunc(out, func(a, b ApprovalItem) int {
		return int(b.Purchase.Approval.OpenedAt) - int(a.Purchase.Approval.OpenedAt)
	})
	if len(out) > limit {
		out = out[:limit]
	}
	return out, nil
}

// itemsOf is a task's approval items of consumer, with each session's
// latest webauthn attempt.
func (s *Store) itemsOf(tx *sql.Tx, id, consumerID string) ([]ApprovalItem, error) {
	t, err := s.openTask(tx, 0, id)
	if err != nil {
		return nil, err
	}
	var dv *DelegationView
	if t.delegation != "" {
		d, _, err := readDelegation(tx, t.delegation)
		if err != nil || d == nil {
			return nil, fmt.Errorf("task %s: delegation %s unreadable", id, t.delegation)
		}
		dv = &DelegationView{Delegation: *d, TaskID: id}
	}
	var out []ApprovalItem
	for _, p := range t.purchases {
		if p.Session != nil && t.row.ConsumerID == consumerID {
			it := ApprovalItem{TaskID: id, ConsumerID: t.row.ConsumerID, WalletID: t.row.WalletID, Payer: t.row.Payer, Purchase: t.view(p), Delegation: dv}
			if err := attemptOf(tx, &it); err != nil {
				return nil, err
			}
			out = append(out, it)
		}
	}
	return out, nil
}

// verifyPay is the recovery check of an apa.pay.v1 task's own records
// (section 10 row 5): the delegation bound to it, every Ask's session and
// every approved session's one grant, each matching the events. An M1 task
// has none of them. It returns "" or what is wrong.
func (t *taskTx) verifyPay() string {
	if !t.pay() {
		if t.delegation != "" {
			return "an apa.m1.v2 task has a delegation"
		}
		return ""
	}
	events := map[string][]contracts.AuditEvent{}
	for _, e := range t.events {
		events[e.EventType] = append(events[e.EventType], e)
	}
	b := events["DELEGATION_BOUND"]
	if t.delegation == "" || len(b) != 1 || b[0].Payload.DelegationID != t.delegation {
		return "the bound delegation does not match DELEGATION_BOUND"
	}
	d, _, err := readDelegation(t.tx, t.delegation)
	if err != nil || d == nil || d.ConsumerID != t.row.ConsumerID {
		return "the bound delegation is unreadable or another consumer's"
	}
	opened, granted, grantedAt, grantedBy := map[string]bool{}, map[string]string{}, map[string]uint64{}, map[string]string{}
	for _, e := range events["APPROVAL_SESSION_OPENED"] {
		opened[e.Payload.ApprovalSessionID] = true
	}
	for _, e := range events["GRANT_ISSUED"] {
		granted[e.Payload.ApprovalSessionID] = e.Payload.GrantID
		grantedAt[e.Payload.ApprovalSessionID] = e.ObservedAt
		grantedBy[e.Payload.ApprovalSessionID] = e.Payload.SignerApprovalID
	}
	resolvedBy := map[string]string{} // session -> the Signer approval its APPROVED or APPROVAL_DENIED names
	for _, typ := range []string{"APPROVED", "APPROVAL_DENIED"} {
		for _, e := range events[typ] {
			resolvedBy[e.Payload.ApprovalSessionID] = e.Payload.AuthorizationMethod + "/" + e.Payload.SignerApprovalID
		}
	}
	for po, p := range t.purchases {
		ss := p.Session
		if (p.Decision.PolicyVerdict == "Ask") != (ss != nil) {
			return "purchase " + po + ": an Ask has exactly one session, nothing else has one"
		}
		if ss == nil {
			continue
		}
		closed := p.ApprovalClosed.String
		if ss.ExpiresAt != t.sessionDeadline(p) {
			return "purchase " + po + ": the session's deadline is not the one its decision time, terms and valid_before give"
		}
		if ss.ID != t.spec().SessionID(t.row.TaskID, po) || ss.IntentHash != p.Decision.IntentHash || ss.ConsumerID != t.row.ConsumerID ||
			ss.OpenedAt != p.DecidedAt || !opened[ss.ID] || (ss.State == "pending") != (closed == "") || closed != "" && ss.State != closed ||
			ss.ClosedAt.Valid && uint64(ss.ClosedAt.Int64) != uint64(p.ClosedAt.Int64) {
			return "purchase " + po + ": its session does not match the purchase or its events"
		}
		g, err := grantOf(ss)
		switch {
		case err != nil:
			return "purchase " + po + ": grant does not decode"
		case (ss.State == "approved") != (g != nil) || (g != nil) != (granted[ss.ID] != ""):
			return "purchase " + po + ": an approved session has exactly one grant and GRANT_ISSUED, nothing else has one"
		case g != nil && (g.Validate() != nil || g.JTI != granted[ss.ID] || g.IntentHash != ss.IntentHash || g.ApprovalSessionID != ss.ID):
			return "purchase " + po + ": the grant does not match its session or event"
		case g != nil:
			// Its times: issued when the session was approved (the closing
			// time, the grants row and GRANT_ISSUED say the same), expiring
			// at the binding's valid_before (a webauthn grant: the Signer's
			// approval expiry, never later); by the approver who closed it,
			// and for webauthn the same Signer approval everywhere.
			c, _ := contracts.Canonical([]byte(ss.grantBody))
			vb := p.V1.PaymentBinding.ValidBefore
			expiry := g.ExpiresAt == vb || g.AuthorizationMethod == "webauthn" && g.ExpiresAt < vb
			if string(c) != ss.grantBody || !expiry || g.IssuedAt != uint64(ss.ClosedAt.Int64) ||
				g.IssuedAt != ss.grantIssuedAt || g.IssuedAt != grantedAt[ss.ID] || g.ApproverID != ss.ResolvedBy.String ||
				grantedBy[ss.ID] != g.SignerApprovalID || resolvedBy[ss.ID] != g.AuthorizationMethod+"/"+g.SignerApprovalID {
				return "purchase " + po + ": the grant's times, approver or Signer approval do not match its session, binding or events"
			}
		}
		if m := resolvedBy[ss.ID]; m != "" && (strings.HasPrefix(m, "webauthn/") != strings.HasPrefix(ss.ResolvedBy.String, "webauthn:")) {
			return "purchase " + po + ": the method of its decision event is not its session's"
		}
	}
	return ""
}

// verifyPayTables checks what no single task's check sees: sessions and
// delegations of apa.m1.v2 tasks, grants of sessions not approved, and
// each delegation's body against its columns.
func verifyPayTables(tx *sql.Tx) error {
	bad := func(format string, a ...any) error {
		return &CorruptError{Check: "approval", Detail: fmt.Sprintf(format, a...)}
	}
	for _, c := range []struct{ q, why string }{
		{`SELECT count(*) FROM approval_sessions s JOIN tasks t USING (task_id) WHERE t.contract_version NOT IN ('apa.pay.v1', 'apa.pay.v2')`, "an apa.m1.v2 task has an approval session"},
		{`SELECT count(*) FROM delegations d JOIN tasks t USING (task_id) WHERE t.contract_version NOT IN ('apa.pay.v1', 'apa.pay.v2') OR t.consumer_id != d.consumer_id OR json_extract(d.body, '$.version') != t.contract_version`, "a delegation is bound to an apa.m1.v2 or another consumer's task"},
		{`SELECT count(*) FROM grants g JOIN approval_sessions s USING (approval_session_id) WHERE s.state != 'approved' OR g.intent_hash != s.intent_hash`, "a grant's session is not approved or has another intent"},
	} {
		var n int
		if err := tx.QueryRow(c.q).Scan(&n); err != nil {
			return readErr("approval", "", err)
		}
		if n > 0 {
			return bad("%s", c.why)
		}
	}
	rows, err := tx.Query(`SELECT delegation_id, request_id, consumer_id, body, confirmed_at, expires_at FROM delegations`)
	if err != nil {
		return readErr("approval", "", err)
	}
	defer rows.Close()
	for rows.Next() {
		var id, req, consumer, body string
		var confirmed, expires uint64
		if err := rows.Scan(&id, &req, &consumer, &body, &confirmed, &expires); err != nil {
			return readErr("approval", "", err)
		}
		var d paycontract.Delegation
		c, cerr := contracts.Canonical([]byte(body))
		if cerr != nil || string(c) != body || contracts.Decode(c, &d) != nil || d.Validate() != nil || d.DelegationID != id || id != specs(d.Version).DelegationID(req) ||
			d.ConsumerID != consumer || d.ConfirmedAt != confirmed || d.ExpiresAt != expires {
			return bad("delegation %s: body and columns disagree", id)
		}
	}
	if err := drained(rows); err != nil {
		return readErr("approval", "", err)
	}
	return nil
}
