package store

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"sort"
	"time"

	"github.com/SIMPLYBOYS/agent-payment-authority/internal/contracts"
	"github.com/SIMPLYBOYS/agent-payment-authority/internal/paycontract"
	"github.com/SIMPLYBOYS/agent-payment-authority/internal/policy"
	"modernc.org/sqlite"
	sqlite3 "modernc.org/sqlite/lib"
)

type taskRow struct {
	TaskID, ConsumerID, AgentID, WalletID, Payer string
	RegistrySnap                                 string
	ProfileSnap, CredentialSnap                  sql.NullString
	TermsSnap, BudgetRemaining, Status           string
	DenyReason                                   sql.NullString
	NextSeq                                      uint64
	ChainHead                                    string
	StartedAt                                    uint64
	ContractVersion                              string // apa.m1.v2 | apa.pay.v1
}

// taskCols are the columns scanTask reads. A schema 1 or 2 file (verified
// before an upgrade) has no contract_version: all its tasks are apa.m1.v2.
func (s *Store) taskCols() string {
	cols := `task_id, consumer_id, agent_id, wallet_id, payer, registry_snapshot, profile_snapshot, credential_snapshot,
  terms_snapshot, budget_remaining, status, deny_reason, next_sequence, chain_head, started_at`
	if s.legacy {
		return cols + `, 'apa.m1.v2'`
	}
	return cols + `, contract_version`
}

func scanTask(row interface{ Scan(...any) error }) (taskRow, error) {
	var t taskRow
	err := row.Scan(&t.TaskID, &t.ConsumerID, &t.AgentID, &t.WalletID, &t.Payer, &t.RegistrySnap, &t.ProfileSnap, &t.CredentialSnap,
		&t.TermsSnap, &t.BudgetRemaining, &t.Status, &t.DenyReason, &t.NextSeq, &t.ChainHead, &t.StartedAt, &t.ContractVersion)
	return t, err
}

// purchaseRow is one stored purchase of either contract: Order for an
// apa.m1.v2 task, V1 for an apa.pay.v1 task, whose Ask also has a Session.
type purchaseRow struct {
	Order          contracts.PurchaseOrder
	V1             *paycontract.PurchaseOrder
	Body           string
	Decision       contracts.PolicyDecision
	DecidedAt      uint64
	ApprovalClosed sql.NullString
	ClosedAt       sql.NullInt64
	Session        *sessionRow
}

// terms are a purchase's fields both contracts have, wherever each keeps them.
type purchaseTerms struct {
	PO, RequestID, Method, URL, ReqHash string
	ChainID, Asset, From, PayTo, Nonce  string
	Amount                              contracts.Amount
}

func (p *purchaseRow) terms() purchaseTerms {
	if v := p.V1; v != nil {
		i, b := v.Intent, v.PaymentBinding
		return purchaseTerms{i.PurchaseOrderID, v.RequestID, i.HTTPMethod, i.ResourceURL, i.PaymentRequirementHash, b.ChainID, b.AssetContract, b.From, b.PayTo, b.Nonce, b.Amount}
	}
	i := p.Order.Intent
	return purchaseTerms{i.PurchaseOrderID, p.Order.RequestID, i.HTTPMethod, i.ResourceURL, i.PaymentRequirementHash, i.ChainID, i.AssetContract, i.From, i.PayTo, i.EIP3009Nonce, i.Amount}
}

const purchaseCols = `body, intent_hash, policy_verdict, approval_status, reason, decided_at, approval_closed, closed_at`

// scanPurchase reads a purchase of a task of contract version.
func scanPurchase(row interface{ Scan(...any) error }, version string) (purchaseRow, error) {
	var p purchaseRow
	d := &p.Decision
	if err := row.Scan(&p.Body, &d.IntentHash, &d.PolicyVerdict, &d.ApprovalStatus, &d.Reason, &p.DecidedAt, &p.ApprovalClosed, &p.ClosedAt); err != nil {
		return p, readErr("purchase", "", err)
	}
	return p, p.decode(version)
}

// decode fills the stored body and the decision's fixed fields by version.
func (p *purchaseRow) decode(version string) error {
	d := &p.Decision
	if paycontract.Supported(version) {
		d.Version, d.PaymentStatus = version, paycontract.PaymentNotEnabled
		p.V1 = new(paycontract.PurchaseOrder)
		return contracts.Decode([]byte(p.Body), p.V1)
	}
	d.Version, d.PaymentStatus = contracts.Version, "unavailable"
	return contracts.Decode([]byte(p.Body), &p.Order)
}

func decodeSnapshot(q interface {
	QueryRow(string, ...any) *sql.Row
}, hash, kind string, dst any) error {
	var k, body string
	if err := q.QueryRow(`SELECT kind, body FROM config_snapshots WHERE snapshot_hash = ?`, hash).Scan(&k, &body); errors.Is(err, sql.ErrNoRows) {
		return fmt.Errorf("snapshot %s does not exist", hash)
	} else if err != nil {
		return readErr("snapshot", "", err)
	}
	if k != kind {
		return fmt.Errorf("snapshot %s is %s, not %s", hash, k, kind)
	}
	return contracts.Decode([]byte(body), dst)
}

// taskTx is one write transaction's view of one task: its stored events and
// purchases, plus the events this transaction appends.
type taskTx struct {
	tx         *sql.Tx
	now        uint64
	row        taskRow
	reg        *contracts.Registry
	regCfg     contracts.RegistryConfig
	terms      terms
	events     []contracts.AuditEvent
	stored     int // events[:stored] are already in the database
	purchases  map[string]*purchaseRow
	delegation string // apa.pay.v1: the bound delegation's ID
}

func (s *Store) openTask(tx *sql.Tx, now uint64, taskID string) (*taskTx, error) {
	row, err := scanTask(tx.QueryRow(`SELECT `+s.taskCols()+` FROM tasks WHERE task_id = ?`, taskID))
	if errors.Is(err, sql.ErrNoRows) {
		return nil, refuse("UNKNOWN_TASK", "no such task has been started")
	} else if err != nil {
		return nil, readErr("task", taskID, err)
	}
	if row.ContractVersion != contracts.Version && !paycontract.Supported(row.ContractVersion) {
		return nil, fmt.Errorf("task %s has unknown contract version %q", taskID, row.ContractVersion)
	}
	t := &taskTx{tx: tx, now: now, row: row, purchases: map[string]*purchaseRow{}}
	if err := decodeSnapshot(tx, row.RegistrySnap, "registry", &t.regCfg); err != nil {
		return nil, err
	}
	if t.reg, err = contracts.NewRegistry(t.regCfg); err != nil {
		return nil, err
	}
	if err := decodeSnapshot(tx, row.TermsSnap, "terms", &t.terms); err != nil {
		return nil, err
	}
	rows, err := tx.Query(`SELECT body FROM events WHERE task_id = ? ORDER BY sequence`, taskID)
	if err != nil {
		return nil, readErr("task", taskID, err)
	}
	defer rows.Close()
	for rows.Next() {
		var body string
		var e contracts.AuditEvent
		if err := rows.Scan(&body); err != nil {
			return nil, readErr("task", taskID, err)
		}
		if err := contracts.Decode([]byte(body), &e); err != nil {
			return nil, err
		}
		t.events = append(t.events, e)
	}
	if err := drained(rows); err != nil {
		return nil, readErr("task", taskID, err)
	}
	t.stored = len(t.events)
	prow, err := tx.Query(`SELECT purchase_order_id, `+purchaseCols+` FROM purchases WHERE task_id = ?`, taskID)
	if err != nil {
		return nil, readErr("task", taskID, err)
	}
	defer prow.Close()
	for prow.Next() {
		var po string
		p, err := scanPurchase(scanFunc(func(dst ...any) error { return prow.Scan(append([]any{&po}, dst...)...) }), row.ContractVersion)
		if err != nil {
			return nil, err
		}
		t.purchases[po] = &p
	}
	if err := drained(prow); err != nil {
		return nil, readErr("task", taskID, err)
	}
	if t.pay() {
		if err := t.loadSessions(); err != nil {
			return nil, err
		}
	}
	return t, nil
}

type scanFunc func(...any) error

func (f scanFunc) Scan(dst ...any) error { return f(dst...) }

func (t *taskTx) session() contracts.Session {
	return contracts.Session{ConsumerID: t.row.ConsumerID, AgentID: t.row.AgentID, TaskID: t.row.TaskID}
}

// emit appends one event to the task's chain in memory; finish writes it.
func (t *taskTx) emit(typ string, p *purchaseRow, payload contracts.EventPayload) error {
	seq := uint64(len(t.events)) + 1
	prev, err := t.genesis()
	if len(t.events) > 0 {
		prev, err = t.eventHash(t.events[len(t.events)-1])
	}
	if err != nil {
		return err
	}
	scope := &contracts.EventScope{ConsumerID: t.row.ConsumerID, AgentID: t.row.AgentID, WalletID: t.row.WalletID, TaskID: t.row.TaskID}
	if p != nil {
		scope.PurchaseOrderID, scope.IntentHash = p.terms().PO, p.Decision.IntentHash
	}
	e := contracts.AuditEvent{
		Version: t.row.ContractVersion, EventID: eventID(t.row.TaskID, seq), EventType: typ, Scope: scope,
		Sequence: seq, PrevEventHash: prev, Payload: payload, EvidenceMode: "fixture", Source: "host", ObservedAt: t.now,
	}
	// E1-a: a pay task's evidence mode and source by the run conditions in
	// its TASK_STARTED (this event, when it is the first); an entry the table
	// lacks stays fixture/host, never a refused write.
	run := payload.RunConditions
	if len(t.events) > 0 {
		run = t.events[0].Payload.RunConditions
	}
	if mode, src, ok := paycontract.Evidence(run, e); ok && paycontract.Supported(e.Version) {
		e.EvidenceMode, e.Source = mode, src
	}
	t.events = append(t.events, e)
	return nil
}

// finish validates the new events against the contract, writes them and the
// projection derived from the whole stream by project(). Nothing is written
// unless all of it holds.
func (t *taskTx) finish() error {
	for _, e := range t.events[t.stored:] {
		if err := t.validateEvent(e); err != nil {
			return fmt.Errorf("event %s: %w", e.EventType, err)
		}
	}
	if err := t.validateOrder(); err != nil {
		return err
	}
	for _, e := range t.events[t.stored:] {
		body, err := canonical(e)
		if err != nil {
			return err
		}
		h, err := t.eventHash(e)
		if err != nil {
			return err
		}
		var po any
		if e.Scope.PurchaseOrderID != "" {
			po = e.Scope.PurchaseOrderID
		}
		if _, err := t.tx.Exec(`INSERT INTO events VALUES (?, ?, ?, ?, ?, ?, ?, ?)`,
			t.row.TaskID, e.Sequence, e.EventID, e.EventType, po, e.ObservedAt, string(body), h); err != nil {
			return err
		}
		t.row.ChainHead, t.row.NextSeq = h, e.Sequence+1
	}
	t.stored = len(t.events)
	status, reason := project(t.events)
	t.row.Status = status
	_, err := t.tx.Exec(`UPDATE tasks SET status = ?, deny_reason = ?, next_sequence = ?, chain_head = ? WHERE task_id = ?`,
		status, nullable(reason), t.row.NextSeq, t.row.ChainHead, t.row.TaskID)
	return err
}

func nullable(s string) any {
	if s == "" {
		return nil
	}
	return s
}

// project derives a task's status from its events; writes, queries and
// recovery all use it. The first terminal event decides a terminal status
// (a later cancel or CLI timeout keeps a Deny and its reason); otherwise an
// open Ask means awaiting_approval and none means running.
func project(events []contracts.AuditEvent) (status, denyReason string) {
	open := map[string]bool{}
	for _, e := range events {
		po := ""
		if e.Scope != nil {
			po = e.Scope.PurchaseOrderID
		}
		switch e.EventType {
		case "POLICY_DECIDED":
			switch e.Payload.Decision.PolicyVerdict {
			case "Deny":
				return "denied", e.Payload.Decision.Reason
			case "Ask":
				open[po] = true
			}
		case "APPROVAL_EXPIRED", "APPROVAL_WITHDRAWN", "APPROVED", "APPROVAL_DENIED", "APPROVAL_SUPERSEDED": // the last three: apa.pay.v1 only
			delete(open, po)
		case "TASK_CANCELLED":
			return "cancelled", ""
		case "TASK_TIMED_OUT":
			return "failed", ""
		}
	}
	if len(open) > 0 {
		return "awaiting_approval", ""
	}
	return "running", ""
}

// openAsks lists the task's open Asks oldest first, so closures are emitted
// in a fixed order.
func (t *taskTx) openAsks() []*purchaseRow {
	var asks []*purchaseRow
	for _, p := range t.purchases {
		if p.Decision.PolicyVerdict == "Ask" && !p.ApprovalClosed.Valid {
			asks = append(asks, p)
		}
	}
	sort.Slice(asks, func(i, j int) bool {
		a, b := asks[i], asks[j]
		return a.DecidedAt < b.DecidedAt || a.DecidedAt == b.DecidedAt && a.terms().PO < b.terms().PO
	})
	return asks
}

// close ends an open Ask as expired or withdrawn (or, apa.pay.v1 only,
// approved, denied or superseded), once. An apa.pay.v1 Ask's session
// closes with it, in the same transaction; by names who resolved it
// (approved or denied: its request ID, the approver and the method).
func (t *taskTx) close(p *purchaseRow, event, approval, reason string, by *resolver) error {
	d := p.Decision
	d.ApprovalStatus, d.Reason = approval, reason
	payload := contracts.EventPayload{Decision: &d}
	if p.Session != nil {
		payload.ApprovalSessionID = p.Session.ID
		if by != nil {
			payload.AuthorizationMethod, payload.SignerApprovalID = by.method, by.signer
		}
	}
	if err := t.emit(event, p, payload); err != nil {
		return err
	}
	po := p.terms().PO
	res, err := t.tx.Exec(`UPDATE purchases SET approval_closed = ?, closed_at = ? WHERE task_id = ? AND purchase_order_id = ? AND approval_closed IS NULL`,
		approval, t.now, t.row.TaskID, po)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n != 1 {
		return errors.New("approval was already closed")
	}
	p.ApprovalClosed, p.ClosedAt = sql.NullString{String: approval, Valid: true}, sql.NullInt64{Int64: int64(t.now), Valid: true}
	if t.pay() {
		return t.closeSession(p, approval, by)
	}
	return nil
}

// expire records APPROVAL_EXPIRED for each open Ask whose deadline has been
// reached at the sampled time: now >= decided_at and now - decided_at >=
// deadline. Checking now >= decided_at first means no unsigned underflow.
func (t *taskTx) expire() error {
	for _, p := range t.openAsks() {
		due := t.now >= p.DecidedAt && t.now-p.DecidedAt >= t.terms.ApprovalDeadlineSeconds
		if p.Session != nil { // apa.pay.v1: the deadline recomputed from what is stored (never past the binding's valid_before)
			due = t.now >= t.sessionDeadline(p)
		}
		if due {
			if err := t.close(p, "APPROVAL_EXPIRED", "expired", "APPROVAL_TIMEOUT", nil); err != nil {
				return err
			}
		}
	}
	return nil
}

func (t *taskTx) ended() bool {
	for _, e := range t.events {
		if e.EventType == "TASK_CANCELLED" || e.EventType == "TASK_TIMED_OUT" {
			return true
		}
	}
	return false
}

// StartTask records the task's trusted snapshots and its TASK_STARTED event.
// Host capability; agents never call it.
func (s *Store) StartTask(setup TaskSetup) error {
	return s.write(func(tx *sql.Tx, now uint64) error {
		task, wallet, err := s.registry.Binding(setup.TaskID)
		if err != nil {
			return refuse("UNKNOWN_TASK", "task is not in the trusted registry")
		}
		var exists int
		if err := tx.QueryRow(`SELECT count(*) FROM tasks WHERE task_id = ?`, task.TaskID).Scan(&exists); err != nil {
			return err
		}
		if exists > 0 {
			return refuse("TASK_ALREADY_STARTED", "task "+task.TaskID+" has already been started")
		}
		var prevWallet, prevPayer string
		err = tx.QueryRow(`SELECT wallet_id, payer FROM tasks WHERE consumer_id = ? LIMIT 1`, task.ConsumerID).Scan(&prevWallet, &prevPayer)
		if err == nil && (prevWallet != wallet.WalletID || prevPayer != wallet.From) {
			return refuse("WALLET_BINDING_CHANGED", "consumer "+task.ConsumerID+" already uses another wallet")
		} else if err != nil && !errors.Is(err, sql.ErrNoRows) {
			return err
		}
		// The other direction: a wallet or payer another consumer has used, in
		// any stored task (ended ones too), is not available, whatever the
		// current registry says. Checked under the write lock with the insert.
		for _, c := range []struct{ col, value, code string }{
			{"wallet_id", wallet.WalletID, "WALLET_ID_IN_USE"},
			{"payer", wallet.From, "PAYER_IN_USE"},
		} {
			var owner string
			err := tx.QueryRow(`SELECT consumer_id FROM tasks WHERE `+c.col+` = ? AND consumer_id != ? LIMIT 1`, c.value, task.ConsumerID).Scan(&owner)
			if err == nil {
				return refuse(c.code, c.col+" "+c.value+" already belongs to consumer "+owner)
			} else if !errors.Is(err, sql.ErrNoRows) {
				return err
			}
		}
		version, dlg, err := s.taskContract(tx, now, &setup, task)
		if err != nil {
			return err
		}
		tm, err := s.buildTerms(setup)
		if err != nil {
			return refuse("INVALID_TASK_SETUP", err.Error())
		}
		regSnap, err := s.snapshot(tx, "registry", json.RawMessage(s.regBody), now)
		if err != nil {
			return err
		}
		var profileSnap, credSnap any
		if setup.Profile != nil {
			if profileSnap, err = s.snapshot(tx, "profile", setup.Profile, now); err != nil {
				return err
			}
		}
		if setup.Credential != nil {
			if credSnap, err = s.snapshot(tx, "credential", setup.Credential, now); err != nil {
				return err
			}
		}
		if setup.BBS != nil {
			if credSnap, err = s.snapshot(tx, "credential_bbs", setup.BBS, now); err != nil {
				return err
			}
		}
		termsSnap, err := s.snapshot(tx, "terms", tm, now)
		if err != nil {
			return err
		}
		genesis, err := contracts.GenesisHash(task.TaskID)
		if err != nil {
			return err
		}
		if sp, ok := paycontract.SpecOf(version); ok {
			genesis, err = sp.GenesisHash(task.TaskID)
			if err != nil {
				return err
			}
		}
		if _, err := tx.Exec(`INSERT INTO tasks (task_id, consumer_id, agent_id, wallet_id, payer, registry_snapshot, profile_snapshot, credential_snapshot,
			terms_snapshot, budget_remaining, status, deny_reason, next_sequence, chain_head, started_at, contract_version)
			VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, 'running', NULL, 1, ?, ?, ?)`,
			task.TaskID, task.ConsumerID, task.AgentID, wallet.WalletID, wallet.From, regSnap, profileSnap, credSnap, termsSnap,
			string(tm.BudgetInitial), genesis, now, version); err != nil {
			return err
		}
		t, err := s.openTask(tx, now, task.TaskID)
		if err != nil {
			return err
		}
		if err := t.emit("TASK_STARTED", nil, contracts.EventPayload{RunConditions: setup.Run}); err != nil {
			return err
		}
		if dlg != nil { // Gate passed: the delegation is bound with the task, in this transaction
			res, err := tx.Exec(`UPDATE delegations SET task_id = ?, bound_at = ? WHERE delegation_id = ? AND task_id IS NULL`, task.TaskID, now, dlg.DelegationID)
			if err != nil {
				return err
			}
			if n, _ := res.RowsAffected(); n != 1 {
				return refuse("DELEGATION_UNAVAILABLE", "the delegation was bound meanwhile")
			}
			bound := contracts.EventPayload{DelegationID: dlg.DelegationID}
			if dlg.ConfirmMethod == "webauthn" { // gap 4: the person's confirmation, named as the Signer keeps it
				bound.AuthorizationMethod, bound.SignerApprovalID = "webauthn", dlg.SignerConfirmationID
			}
			if err := t.emit("DELEGATION_BOUND", nil, bound); err != nil {
				return err
			}
		}
		return t.finish()
	})
}

func (s *Store) buildTerms(setup TaskSetup) (terms, error) {
	task, _, _ := s.registry.Binding(setup.TaskID)
	tm := terms{TaskID: setup.TaskID, ProfileState: "absent", CredentialState: "absent", BudgetInitial: setup.BudgetInitial,
		AssetID: setup.AssetID, ApprovalDeadlineSeconds: setup.ApprovalDeadlineSeconds}
	// ponytail: setup.Run is checked where it is written, TASK_STARTED's
	// validation (paycontract; contracts refuses it on an M1 event).
	hasCred := setup.Credential != nil || setup.BBS != nil
	if (setup.Profile == nil) != setup.ProfileAbsent || hasCred == setup.CredentialAbsent || setup.Credential != nil && setup.BBS != nil {
		return tm, errors.New("declare a missing profile or credential explicitly; never pass both or neither")
	}
	if p := setup.Profile; p != nil {
		if err := p.Validate(); err != nil {
			return tm, fmt.Errorf("profile: %w", err)
		}
		tm.ProfileState = "present"
		if tm.BudgetInitial == "" {
			tm.BudgetInitial = p.TaskCap
		}
		if tm.AssetID == "" {
			tm.AssetID = p.AssetID
		}
	}
	v2 := setup.ContractVersion == paycontract.V2
	if c := setup.Credential; c != nil {
		if v2 || c.ConsumerID != task.ConsumerID || c.EvidenceMode != "fixture" || c.ValidUntil == 0 {
			return tm, errors.New("credential must be this consumer's M1 fixture with a valid_until, in an apa.m1.v2 or apa.pay.v1 task")
		}
		tm.CredentialState = "present"
	}
	if b := setup.BBS; b != nil {
		if err := b.validate(); err != nil {
			return tm, err
		}
		if !v2 || b.ConsumerID != task.ConsumerID || b.TaskID != task.TaskID {
			return tm, errors.New("credential_bbs must be this task's and consumer's, in an apa.pay.v2 task")
		}
		tm.CredentialState = "present"
	}
	if tm.ApprovalDeadlineSeconds == 0 {
		tm.ApprovalDeadlineSeconds = defaultDeadline
	}
	var cfg contracts.RegistryConfig
	if err := contracts.Decode(s.regBody, &cfg); err != nil {
		return tm, err
	}
	return tm, tm.validate(cfg, setup.Profile)
}

// Result is what Submit returns. Replay means the purchase was already
// accepted and nothing was decided again.
type Result struct {
	Replay   bool
	Purchase PurchaseView
}

// Submit records a purchase for the session's task and returns its decision,
// in the order of the design's Submit section: format, ownership, this task's
// expiry, retry-key replay or conflict, purchase-ID and nonce conflicts, then
// for a new purchase only the current offer book, the active-task rule, the
// Policy and the write. Refusals roll everything back.
func (s *Store) Submit(sess contracts.Session, raw []byte) (Result, error) {
	// The body's own version only picks the decoder; the stored task's
	// contract version decides which entry may take it (checked below).
	var head struct {
		Version string `json:"version"`
	}
	json.Unmarshal(raw, &head)
	var po contracts.PurchaseOrder
	var v1 *paycontract.PurchaseOrder
	if paycontract.Supported(head.Version) {
		v1 = new(paycontract.PurchaseOrder)
		if err := contracts.Decode(raw, v1); err != nil {
			return Result{}, refuse("INVALID_PURCHASE", "purchase does not decode as a pay-family purchase order")
		}
	} else if err := contracts.Decode(raw, &po); err != nil {
		return Result{}, refuse("INVALID_PURCHASE", "purchase does not decode as an M1 purchase order")
	}
	var out Result
	err := s.write(func(tx *sql.Tx, now uint64) error {
		t, err := s.openTask(tx, now, sess.TaskID)
		if Code(err) == "UNKNOWN_TASK" || err == nil && (t.row.ConsumerID != sess.ConsumerID || t.row.AgentID != sess.AgentID) {
			return refuse("OWNERSHIP_MISMATCH", "session does not own this task")
		} else if err != nil {
			return err
		}
		if (v1 != nil) != t.pay() {
			return refuse("CONTRACT_VERSION_MISMATCH", "the purchase's contract is not the task's ("+t.row.ContractVersion+")")
		}
		if t.row.ContractVersion == paycontract.V2 {
			return refuse("CONTRACT_VERSION_MISMATCH", "an apa.pay.v2 purchase is submitted with its credential presentation (SubmitV2)")
		}
		if v1 != nil {
			return s.submitPay(t, sess, *v1, nil, &out)
		}
		if err := t.reg.ValidatePurchase(sess, po); err != nil {
			return contractRefusal(err)
		}
		if err := t.expire(); err != nil {
			return err
		}
		key, err := po.RetryKey()
		if err != nil {
			return contractRefusal(err)
		}
		// Replay and conflicts are judged on stored rows only, never on the
		// current offer book.
		existing, err := s.related(t, key, purchaseTerms{PO: po.Intent.PurchaseOrderID, ChainID: po.Intent.ChainID, Asset: po.Intent.AssetContract,
			From: po.Intent.From, Nonce: po.Intent.EIP3009Nonce})
		if err != nil {
			return err
		}
		for _, e := range existing {
			if e.V1 != nil { // another contract's row holds the nonce (the unique index backs this up)
				return refuse("IDEMPOTENCY_CONFLICT", "a stored purchase already holds this retry key, purchase_order_id or nonce")
			}
			switch kind, err := contracts.CompareRetry(e.Order, po); {
			case err != nil:
				return contractRefusal(err)
			case kind == "replay":
				out = Result{Replay: true, Purchase: t.view(t.purchases[po.Intent.PurchaseOrderID])}
				return t.finish() // only this task's expiry, if any
			}
		}
		if !s.offered(po.Intent.HTTPMethod, po.Intent.ResourceURL, po.Quote) {
			return refuse("QUOTE_NOT_OFFERED", "quote is not in the current trusted offer book")
		}
		status, _ := project(t.events)
		if err := contracts.CheckTaskActive(status); err != nil {
			return contractRefusal(err)
		}
		in := policy.Input{Session: sess, Purchase: po, TaskStatus: status, Budget: policy.Budget{TaskID: t.row.TaskID, Remaining: contracts.Amount(t.row.BudgetRemaining)}, Now: now}
		if in.Profile, in.Credential, err = t.settings(); err != nil {
			return err
		}
		d, err := policy.Decide(t.reg, in)
		if err != nil {
			return contractRefusal(err)
		}
		body, err := canonical(po)
		if err != nil {
			return err
		}
		p := &purchaseRow{Order: po, Body: string(body), Decision: d, DecidedAt: now}
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
		if err := t.finish(); err != nil {
			return err
		}
		out = Result{Purchase: t.view(p)}
		return s.at("after-events")
	})
	return out, err
}

// related reads the stored purchases that share the retry key, or the
// task's purchase_order_id, or the payer's nonce: the rows a new purchase
// is a replay of or conflicts with.
func (s *Store) related(t *taskTx, key string, k purchaseTerms) ([]purchaseRow, error) {
	rows, err := t.tx.Query(`SELECT p.task_id, `+purchaseColsP+`, t.contract_version FROM purchases p JOIN tasks t USING (task_id) WHERE p.retry_key = ?
	  UNION ALL SELECT p.task_id, `+purchaseColsP+`, t.contract_version FROM purchases p JOIN tasks t USING (task_id) WHERE p.retry_key != ? AND ((p.task_id = ? AND p.purchase_order_id = ?)
	    OR (p.chain_id = ? AND p.asset_contract = ? AND p.payer = ? AND p.eip3009_nonce = ?))`,
		key, key, t.row.TaskID, k.PO, k.ChainID, k.Asset, k.From, k.Nonce)
	if err != nil {
		return nil, err
	}
	var out []purchaseRow
	for rows.Next() {
		var task, version string
		var p purchaseRow
		err := rows.Scan(&task, &p.Body, &p.Decision.IntentHash, &p.Decision.PolicyVerdict, &p.Decision.ApprovalStatus, &p.Decision.Reason,
			&p.DecidedAt, &p.ApprovalClosed, &p.ClosedAt, &version)
		if err == nil {
			err = p.decode(version)
		}
		if err != nil {
			rows.Close()
			return nil, err
		}
		out = append(out, p)
	}
	return out, errors.Join(rows.Err(), rows.Close())
}

const purchaseColsP = `p.body, p.intent_hash, p.policy_verdict, p.approval_status, p.reason, p.decided_at, p.approval_closed, p.closed_at`

// settings are the task's stored profile and credential (nil: absent).
func (t *taskTx) settings() (*policy.Profile, *policy.Credential, error) {
	var p *policy.Profile
	var c *policy.Credential
	if t.row.ProfileSnap.Valid {
		p = new(policy.Profile)
		if err := decodeSnapshot(t.tx, t.row.ProfileSnap.String, "profile", p); err != nil {
			return nil, nil, err
		}
	}
	if t.row.CredentialSnap.Valid && t.row.ContractVersion != paycontract.V2 { // v2: the verifier's record, t.bbs()
		c = new(policy.Credential)
		if err := decodeSnapshot(t.tx, t.row.CredentialSnap.String, "credential", c); err != nil {
			return nil, nil, err
		}
	}
	return p, c, nil
}

// insertPurchase writes a new purchase row; the unique indexes back up the
// CompareRetry checks.
func (t *taskTx) insertPurchase(p *purchaseRow, key string) error {
	k := p.terms()
	d := p.Decision
	if _, err := t.tx.Exec(`INSERT INTO purchases VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, NULL, NULL)`,
		t.row.TaskID, k.PO, k.RequestID, key, k.ChainID, k.Asset, k.From, k.Nonce, p.Body,
		d.IntentHash, d.PolicyVerdict, d.ApprovalStatus, d.Reason, p.DecidedAt); uniqueViolation(err) {
		return refuse("IDEMPOTENCY_CONFLICT", "a stored purchase already holds this retry key, purchase_order_id or nonce")
	} else if err != nil {
		return err
	}
	t.purchases[k.PO] = p
	return nil
}

func uniqueViolation(err error) bool {
	var e *sqlite.Error
	return errors.As(err, &e) && (e.Code() == sqlite3.SQLITE_CONSTRAINT_UNIQUE || e.Code() == sqlite3.SQLITE_CONSTRAINT_PRIMARYKEY)
}

// contractRefusal keeps a contract error's code and drops its detail, which
// can quote the request (a case-variant key name, for example).
func contractRefusal(err error) error {
	code := paycontract.Code(err) // either contract's code (paycontract falls back to the M1 one)
	if code == "" {
		code = "INVALID_PURCHASE"
	}
	return refuse(code, "refused by a contract check")
}

func (s *Store) offered(method, url string, quote contracts.Quote) bool {
	c, err := canonical(quote)
	if err != nil {
		return false
	}
	for _, q := range s.offers[OfferKey{method, url}] {
		if bytes.Equal(q, c) {
			return true
		}
	}
	return false
}

// Cancel is the host ending a task: due Asks expire first, the rest are
// withdrawn, then TASK_CANCELLED. A Deny already recorded stays the status.
func (s *Store) Cancel(taskID string) error {
	return s.end(taskID, "TASK_CANCELLED", "HOST_CANCELLED", true)
}

// CLITimeout records the CLI timeout. Open Asks stay pending (accepted case
// cli-timeout-pending) and can still expire later.
func (s *Store) CLITimeout(taskID string) error {
	return s.end(taskID, "TASK_TIMED_OUT", "CLI_TIMEOUT", false)
}

func (s *Store) end(taskID, event, code string, withdraw bool) error {
	return s.write(func(tx *sql.Tx, now uint64) error {
		t, err := s.openTask(tx, now, taskID)
		if err != nil {
			return err
		}
		if t.ended() {
			return refuse("TASK_ALREADY_ENDED", "task already has TASK_CANCELLED or TASK_TIMED_OUT")
		}
		if err := t.expire(); err != nil {
			return err
		}
		if withdraw {
			for _, p := range t.openAsks() {
				if err := t.close(p, "APPROVAL_WITHDRAWN", "withdrawn", "HOST_CANCELLED", nil); err != nil {
					return err
				}
			}
		}
		if err := t.emit(event, nil, contracts.EventPayload{Code: code}); err != nil {
			return err
		}
		return t.finish()
	})
}

// ExpireDue records every reached Ask deadline, one transaction per task so
// one task's failure leaves the others alone. Open runs it before serving;
// the host also runs it on a target period (RunExpiry), which is not a bound
// on how late a query can be.
func (s *Store) ExpireDue() error {
	rows, err := s.w.Query(`SELECT DISTINCT task_id FROM purchases WHERE policy_verdict = 'Ask' AND approval_closed IS NULL ORDER BY task_id`)
	if err != nil {
		return err
	}
	var ids []string
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			rows.Close()
			return err
		}
		ids = append(ids, id)
	}
	if err := errors.Join(rows.Err(), rows.Close()); err != nil {
		return err
	}
	var errs []error
	for _, id := range ids {
		errs = append(errs, s.write(func(tx *sql.Tx, now uint64) error {
			t, err := s.openTask(tx, now, id)
			if err != nil {
				return err
			}
			if err := t.expire(); err != nil {
				return err
			}
			return t.finish()
		}))
	}
	return errors.Join(errs...)
}

// RunExpiry calls ExpireDue every period until ctx ends, reporting errors to
// onErr. The period is a target; lock waits, scheduling and errors can make
// expiry later.
func (s *Store) RunExpiry(ctx context.Context, every time.Duration, onErr func(error)) {
	s.RunExpiryReporting(ctx, every, func(err error) {
		if err != nil && onErr != nil {
			onErr(err)
		}
	})
}

// RunExpiryReporting is RunExpiry reporting every completed ExpireDue, nil
// on success, so a host can say when expiry last ran and how. It is an
// observation only: the same single loop, nothing else is called.
func (s *Store) RunExpiryReporting(ctx context.Context, every time.Duration, report func(error)) {
	tick := time.NewTicker(every)
	defer tick.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-tick.C:
			err := s.ExpireDue()
			if report != nil {
				report(err)
			}
		}
	}
}

// PurchaseView is the whitelisted projection of a purchase. It never carries
// the stored quote, its extra or its extensions.
type PurchaseView struct {
	PurchaseOrderID        string                   `json:"purchase_order_id"`
	RequestID              string                   `json:"request_id"`
	HTTPMethod             string                   `json:"http_method"`
	ResourceURL            string                   `json:"resource_url"`
	ChainID                string                   `json:"chain_id"`
	AssetContract          string                   `json:"asset_contract"`
	AssetID                string                   `json:"asset_id,omitempty"` // empty: not a trusted asset
	PayTo                  string                   `json:"pay_to"`
	Amount                 contracts.Amount         `json:"amount"`
	IntentHash             string                   `json:"intent_hash"`
	PaymentRequirementHash string                   `json:"payment_requirement_hash"`
	Decision               contracts.PolicyDecision `json:"decision"` // the original decision, never rewritten
	DecidedAt              uint64                   `json:"decided_at"`
	DeadlineAt             uint64                   `json:"deadline_at,omitempty"` // Asks: decided_at + deadline (apa.pay.v1: the session's)
	ApprovalClosed         string                   `json:"approval_closed,omitempty"`
	ClosedAt               uint64                   `json:"closed_at,omitempty"`
	ClosedReason           string                   `json:"closed_reason,omitempty"` // the closing event's reason (e.g. HOST_CANCELLED, CREDENTIAL_REVOKED)
	Approval               *ApprovalView            `json:"approval,omitempty"`      // apa.pay.v1 Asks: the approval session and its grant
	// apa.pay.v2: the purchase's verification record, as its CREDENTIAL_VERIFIED event holds it.
	Credential *contracts.CredentialCheck `json:"credential,omitempty"`
	// P2: the purchase's payment, as its payment events record it; absent before any.
	Payment *PaymentView `json:"payment,omitempty"`
}

// ApprovalView is an apa.pay.v1 Ask's approval session as stored, and its
// grant once approved. A simulated grant gives no payment capability.
type ApprovalView struct {
	SessionID  string     `json:"approval_session_id"`
	State      string     `json:"state"` // pending | approved | denied | expired | withdrawn | superseded
	OpenedAt   uint64     `json:"opened_at"`
	ExpiresAt  uint64     `json:"expires_at"`
	ClosedAt   uint64     `json:"closed_at,omitempty"`
	ResolvedBy string     `json:"resolved_by,omitempty"`
	Grant      *GrantView `json:"grant,omitempty"`
}

// GrantView is the stored grant's identity, method and window.
type GrantView struct {
	JTI                 string `json:"jti"`
	AuthorizationMethod string `json:"authorization_method"`
	Audience            string `json:"audience"`
	IssuedAt            uint64 `json:"issued_at"`
	ExpiresAt           uint64 `json:"expires_at"`
	Voice               string `json:"voice,omitempty"`        // V1: verified | unavailable
	VoiceReason         string `json:"voice_reason,omitempty"` // V1: why unavailable
}

func grantView(g paycontract.AuthorizationGrant) *GrantView {
	return &GrantView{JTI: g.JTI, AuthorizationMethod: g.AuthorizationMethod, Audience: g.Audience, IssuedAt: g.IssuedAt, ExpiresAt: g.ExpiresAt,
		Voice: g.Voice, VoiceReason: g.VoiceReason}
}

// TaskView is a committed, consistent snapshot of one task. AsOf names the
// last committed event; it does not claim to be current as of now.
type TaskView struct {
	TaskID          string         `json:"task_id"`
	ContractVersion string         `json:"contract_version"`
	DelegationID    string         `json:"delegation_id,omitempty"` // apa.pay.v1: the bound delegation
	Status          string         `json:"status"`
	DenyReason      string         `json:"deny_reason,omitempty"`
	AsOfSequence    uint64         `json:"as_of_sequence"`
	AsOfObservedAt  uint64         `json:"as_of_observed_at"`
	Purchases       []PurchaseView `json:"purchases"`
}

// closingEvents close an Ask.
var closingEvents = []string{"APPROVAL_EXPIRED", "APPROVAL_WITHDRAWN", "APPROVED", "APPROVAL_DENIED", "APPROVAL_SUPERSEDED"}

func (t *taskTx) view(p *purchaseRow) PurchaseView {
	k := p.terms()
	v := PurchaseView{PurchaseOrderID: k.PO, RequestID: k.RequestID, HTTPMethod: k.Method, ResourceURL: k.URL,
		ChainID: k.ChainID, AssetContract: k.Asset, PayTo: k.PayTo, Amount: k.Amount, IntentHash: p.Decision.IntentHash,
		PaymentRequirementHash: k.ReqHash, Decision: p.Decision, DecidedAt: p.DecidedAt,
		ApprovalClosed: p.ApprovalClosed.String, ClosedAt: uint64(p.ClosedAt.Int64)}
	if a, ok := t.reg.TrustedAsset(k.ChainID, k.Asset); ok {
		v.AssetID = a.AssetID
	}
	if p.Decision.PolicyVerdict == "Ask" {
		v.DeadlineAt = p.DecidedAt + t.terms.ApprovalDeadlineSeconds
	}
	for _, e := range t.events {
		switch {
		case !t.pay() || e.Scope == nil || e.Scope.PurchaseOrderID != k.PO: // an M1 view is what it always was
		case e.EventType == "CREDENTIAL_VERIFIED":
			v.Credential = e.Payload.Credential
		case slices.Contains(closingEvents, e.EventType) && e.Payload.Decision != nil:
			v.ClosedReason = e.Payload.Decision.Reason
		}
	}
	if t.pay() {
		v.Payment = t.paymentView(k.PO)
	}
	if ss := p.Session; ss != nil {
		v.DeadlineAt = ss.ExpiresAt
		v.Approval = &ApprovalView{SessionID: ss.ID, State: ss.State, OpenedAt: ss.OpenedAt, ExpiresAt: ss.ExpiresAt, ClosedAt: uint64(ss.ClosedAt.Int64),
			ResolvedBy: ss.ResolvedBy.String, Grant: ss.Grant}
	}
	return v
}

// read runs fn on the session's own task in one read transaction: a
// consistent snapshot of what was committed, without expiring anything.
func (s *Store) read(sess contracts.Session, fn func(t *taskTx) error) error {
	tx, err := s.r.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	t, err := s.openTask(tx, 0, sess.TaskID)
	if Code(err) == "UNKNOWN_TASK" || err == nil && (t.row.ConsumerID != sess.ConsumerID || t.row.AgentID != sess.AgentID) {
		return refuse("OWNERSHIP_MISMATCH", "session does not own this task")
	} else if err != nil {
		return err
	}
	return fn(t)
}

func (t *taskTx) taskView() TaskView {
	v := TaskView{TaskID: t.row.TaskID, ContractVersion: t.row.ContractVersion, DelegationID: t.delegation, Status: t.row.Status,
		DenyReason: t.row.DenyReason.String, Purchases: []PurchaseView{}}
	if n := len(t.events); n > 0 {
		v.AsOfSequence, v.AsOfObservedAt = t.events[n-1].Sequence, t.events[n-1].ObservedAt
	}
	for _, p := range t.purchases {
		v.Purchases = append(v.Purchases, t.view(p))
	}
	sort.Slice(v.Purchases, func(i, j int) bool {
		return v.Purchases[i].DecidedAt < v.Purchases[j].DecidedAt || v.Purchases[i].DecidedAt == v.Purchases[j].DecidedAt && v.Purchases[i].PurchaseOrderID < v.Purchases[j].PurchaseOrderID
	})
	return v
}

// Task returns the session's task as last committed.
func (s *Store) Task(sess contracts.Session) (TaskView, error) {
	var v TaskView
	err := s.read(sess, func(t *taskTx) error { v = t.taskView(); return nil })
	return v, err
}

// TaskBinding is the identity a task was started with, as stored with it:
// never the host's current registry. WalletEvidenceMode comes from the
// task's own registry snapshot.
type TaskBinding struct {
	TaskID             string `json:"task_id"`
	ConsumerID         string `json:"consumer_id"`
	AgentID            string `json:"agent_id"`
	WalletID           string `json:"wallet_id"`
	Payer              string `json:"payer"`
	WalletEvidenceMode string `json:"wallet_evidence_mode"`
}

// TaskRecord is one read transaction's account of a stored task: its
// binding, its projection and its events, all as of the same commit.
type TaskRecord struct {
	Binding TaskBinding
	Task    TaskView
	Events  []contracts.AuditEvent
}

// ReadTask is the host's read of a stored task for a caller whose trusted
// grant is visible: it is asked with the task's stored consumer, never a
// value from the request. An unknown task and one the caller may not see
// are the same OWNERSHIP_MISMATCH. A task whose snapshot no longer holds
// its stored wallet is refused as corrupt, never filled from elsewhere.
// Nothing is expired or written.
func (s *Store) ReadTask(taskID string, visible func(consumerID string) bool) (TaskRecord, error) {
	tx, err := s.r.Begin()
	if err != nil {
		return TaskRecord{}, err
	}
	defer tx.Rollback()
	t, err := s.openTask(tx, 0, taskID)
	if Code(err) == "UNKNOWN_TASK" || err == nil && !visible(t.row.ConsumerID) {
		return TaskRecord{}, refuse("OWNERSHIP_MISMATCH", "no such task for this caller")
	} else if err != nil {
		return TaskRecord{}, err
	}
	task, w, err := t.reg.Binding(taskID)
	if err != nil || task.ConsumerID != t.row.ConsumerID || task.AgentID != t.row.AgentID || w.WalletID != t.row.WalletID || w.From != t.row.Payer {
		return TaskRecord{}, &CorruptError{Check: "binding", TaskID: taskID, Detail: "the task's registry snapshot does not hold its stored binding"}
	}
	return TaskRecord{
		Binding: TaskBinding{TaskID: t.row.TaskID, ConsumerID: t.row.ConsumerID, AgentID: t.row.AgentID, WalletID: t.row.WalletID,
			Payer: t.row.Payer, WalletEvidenceMode: w.EvidenceMode},
		Task: t.taskView(), Events: t.events,
	}, nil
}

// Events returns the session's task events as last committed.
func (s *Store) Events(sess contracts.Session) ([]contracts.AuditEvent, error) {
	var events []contracts.AuditEvent
	err := s.read(sess, func(t *taskTx) error { events = t.events; return nil })
	return events, err
}
