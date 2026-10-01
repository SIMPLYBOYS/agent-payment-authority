package store

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"time"

	"github.com/SIMPLYBOYS/agent-payment-authority/internal/contracts"
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
}

const taskCols = `task_id, consumer_id, agent_id, wallet_id, payer, registry_snapshot, profile_snapshot, credential_snapshot,
  terms_snapshot, budget_remaining, status, deny_reason, next_sequence, chain_head, started_at`

func scanTask(row interface{ Scan(...any) error }) (taskRow, error) {
	var t taskRow
	err := row.Scan(&t.TaskID, &t.ConsumerID, &t.AgentID, &t.WalletID, &t.Payer, &t.RegistrySnap, &t.ProfileSnap, &t.CredentialSnap,
		&t.TermsSnap, &t.BudgetRemaining, &t.Status, &t.DenyReason, &t.NextSeq, &t.ChainHead, &t.StartedAt)
	return t, err
}

type purchaseRow struct {
	Order          contracts.PurchaseOrder
	Body           string
	Decision       contracts.PolicyDecision
	DecidedAt      uint64
	ApprovalClosed sql.NullString
	ClosedAt       sql.NullInt64
}

const purchaseCols = `body, intent_hash, policy_verdict, approval_status, reason, decided_at, approval_closed, closed_at`

func scanPurchase(row interface{ Scan(...any) error }) (purchaseRow, error) {
	var p purchaseRow
	d := &p.Decision
	if err := row.Scan(&p.Body, &d.IntentHash, &d.PolicyVerdict, &d.ApprovalStatus, &d.Reason, &p.DecidedAt, &p.ApprovalClosed, &p.ClosedAt); err != nil {
		return p, readErr("purchase", "", err)
	}
	d.Version, d.PaymentStatus = contracts.Version, "unavailable"
	return p, contracts.Decode([]byte(p.Body), &p.Order)
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
	tx        *sql.Tx
	now       uint64
	row       taskRow
	reg       *contracts.Registry
	regCfg    contracts.RegistryConfig
	terms     terms
	events    []contracts.AuditEvent
	stored    int // events[:stored] are already in the database
	purchases map[string]*purchaseRow
}

func (s *Store) openTask(tx *sql.Tx, now uint64, taskID string) (*taskTx, error) {
	row, err := scanTask(tx.QueryRow(`SELECT `+taskCols+` FROM tasks WHERE task_id = ?`, taskID))
	if errors.Is(err, sql.ErrNoRows) {
		return nil, refuse("UNKNOWN_TASK", "no such task has been started")
	} else if err != nil {
		return nil, readErr("task", taskID, err)
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
		p, err := scanPurchase(scanFunc(func(dst ...any) error { return prow.Scan(append([]any{&po}, dst...)...) }))
		if err != nil {
			return nil, err
		}
		t.purchases[po] = &p
	}
	if err := drained(prow); err != nil {
		return nil, readErr("task", taskID, err)
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
	prev, err := contracts.GenesisHash(t.row.TaskID)
	if len(t.events) > 0 {
		prev, err = contracts.EventHash(t.events[len(t.events)-1])
	}
	if err != nil {
		return err
	}
	scope := &contracts.EventScope{ConsumerID: t.row.ConsumerID, AgentID: t.row.AgentID, WalletID: t.row.WalletID, TaskID: t.row.TaskID}
	if p != nil {
		scope.PurchaseOrderID, scope.IntentHash = p.Order.Intent.PurchaseOrderID, p.Decision.IntentHash
	}
	t.events = append(t.events, contracts.AuditEvent{
		Version: contracts.Version, EventID: eventID(t.row.TaskID, seq), EventType: typ, Scope: scope,
		Sequence: seq, PrevEventHash: prev, Payload: payload, EvidenceMode: "fixture", Source: "host", ObservedAt: t.now,
	})
	return nil
}

// finish validates the new events against the contract, writes them and the
// projection derived from the whole stream by project(). Nothing is written
// unless all of it holds.
func (t *taskTx) finish() error {
	for _, e := range t.events[t.stored:] {
		var order *contracts.PurchaseOrder
		if po := e.Scope.PurchaseOrderID; po != "" {
			order = &t.purchases[po].Order
		}
		if err := t.reg.ValidateEvent(e, order); err != nil {
			return fmt.Errorf("event %s: %w", e.EventType, err)
		}
	}
	if err := contracts.ValidateEventOrder(t.events); err != nil {
		return err
	}
	for _, e := range t.events[t.stored:] {
		body, err := canonical(e)
		if err != nil {
			return err
		}
		h, err := contracts.EventHash(e)
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
		case "APPROVAL_EXPIRED", "APPROVAL_WITHDRAWN":
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
		return a.DecidedAt < b.DecidedAt || a.DecidedAt == b.DecidedAt && a.Order.Intent.PurchaseOrderID < b.Order.Intent.PurchaseOrderID
	})
	return asks
}

// close ends an open Ask as expired or withdrawn, once.
func (t *taskTx) close(p *purchaseRow, event, approval, reason string) error {
	d := p.Decision
	d.ApprovalStatus, d.Reason = approval, reason
	if err := t.emit(event, p, contracts.EventPayload{Decision: &d}); err != nil {
		return err
	}
	res, err := t.tx.Exec(`UPDATE purchases SET approval_closed = ?, closed_at = ? WHERE task_id = ? AND purchase_order_id = ? AND approval_closed IS NULL`,
		approval, t.now, t.row.TaskID, p.Order.Intent.PurchaseOrderID)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n != 1 {
		return errors.New("approval was already closed")
	}
	p.ApprovalClosed, p.ClosedAt = sql.NullString{String: approval, Valid: true}, sql.NullInt64{Int64: int64(t.now), Valid: true}
	return nil
}

// expire records APPROVAL_EXPIRED for each open Ask whose deadline has been
// reached at the sampled time: now >= decided_at and now - decided_at >=
// deadline. Checking now >= decided_at first means no unsigned underflow.
func (t *taskTx) expire() error {
	for _, p := range t.openAsks() {
		if t.now >= p.DecidedAt && t.now-p.DecidedAt >= t.terms.ApprovalDeadlineSeconds {
			if err := t.close(p, "APPROVAL_EXPIRED", "expired", "APPROVAL_TIMEOUT"); err != nil {
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
		termsSnap, err := s.snapshot(tx, "terms", tm, now)
		if err != nil {
			return err
		}
		genesis, err := contracts.GenesisHash(task.TaskID)
		if err != nil {
			return err
		}
		if _, err := tx.Exec(`INSERT INTO tasks VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, 'running', NULL, 1, ?, ?)`,
			task.TaskID, task.ConsumerID, task.AgentID, wallet.WalletID, wallet.From, regSnap, profileSnap, credSnap, termsSnap,
			string(tm.BudgetInitial), genesis, now); err != nil {
			return err
		}
		t, err := s.openTask(tx, now, task.TaskID)
		if err != nil {
			return err
		}
		if err := t.emit("TASK_STARTED", nil, contracts.EventPayload{}); err != nil {
			return err
		}
		return t.finish()
	})
}

func (s *Store) buildTerms(setup TaskSetup) (terms, error) {
	task, _, _ := s.registry.Binding(setup.TaskID)
	tm := terms{TaskID: setup.TaskID, ProfileState: "absent", CredentialState: "absent", BudgetInitial: setup.BudgetInitial,
		AssetID: setup.AssetID, ApprovalDeadlineSeconds: setup.ApprovalDeadlineSeconds}
	if (setup.Profile == nil) != setup.ProfileAbsent || (setup.Credential == nil) != setup.CredentialAbsent {
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
	if c := setup.Credential; c != nil {
		if c.ConsumerID != task.ConsumerID || c.EvidenceMode != "fixture" || c.ValidUntil == 0 {
			return tm, errors.New("credential must be this consumer's M1 fixture with a valid_until")
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
	var po contracts.PurchaseOrder
	if err := contracts.Decode(raw, &po); err != nil {
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
		var existing []purchaseRow
		rows, err := tx.Query(`SELECT `+purchaseCols+` FROM purchases WHERE retry_key = ?
		  UNION ALL SELECT `+purchaseCols+` FROM purchases WHERE retry_key != ? AND ((task_id = ? AND purchase_order_id = ?)
		    OR (chain_id = ? AND asset_contract = ? AND payer = ? AND eip3009_nonce = ?))`,
			key, key, po.Intent.TaskID, po.Intent.PurchaseOrderID, po.Intent.ChainID, po.Intent.AssetContract, po.Intent.From, po.Intent.EIP3009Nonce)
		if err != nil {
			return err
		}
		for rows.Next() {
			p, err := scanPurchase(rows)
			if err != nil {
				rows.Close()
				return err
			}
			existing = append(existing, p)
		}
		if err := errors.Join(rows.Err(), rows.Close()); err != nil {
			return err
		}
		for _, e := range existing {
			switch kind, err := contracts.CompareRetry(e.Order, po); {
			case err != nil:
				return contractRefusal(err)
			case kind == "replay":
				out = Result{Replay: true, Purchase: t.view(t.purchases[po.Intent.PurchaseOrderID])}
				return t.finish() // only this task's expiry, if any
			}
		}
		if !s.offered(po) {
			return refuse("QUOTE_NOT_OFFERED", "quote is not in the current trusted offer book")
		}
		status, _ := project(t.events)
		if err := contracts.CheckTaskActive(status); err != nil {
			return contractRefusal(err)
		}
		in := policy.Input{Session: sess, Purchase: po, TaskStatus: status, Budget: policy.Budget{TaskID: t.row.TaskID, Remaining: contracts.Amount(t.row.BudgetRemaining)}, Now: now}
		if t.row.ProfileSnap.Valid {
			in.Profile = new(policy.Profile)
			if err := decodeSnapshot(tx, t.row.ProfileSnap.String, "profile", in.Profile); err != nil {
				return err
			}
		}
		if t.row.CredentialSnap.Valid {
			in.Credential = new(policy.Credential)
			if err := decodeSnapshot(tx, t.row.CredentialSnap.String, "credential", in.Credential); err != nil {
				return err
			}
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
		i := po.Intent
		if _, err := tx.Exec(`INSERT INTO purchases VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, NULL, NULL)`,
			i.TaskID, i.PurchaseOrderID, po.RequestID, key, i.ChainID, i.AssetContract, i.From, i.EIP3009Nonce, p.Body,
			d.IntentHash, d.PolicyVerdict, d.ApprovalStatus, d.Reason, now); uniqueViolation(err) {
			// The unique indexes back up the CompareRetry checks above.
			return refuse("IDEMPOTENCY_CONFLICT", "a stored purchase already holds this retry key, purchase_order_id or nonce")
		} else if err != nil {
			return err
		}
		t.purchases[i.PurchaseOrderID] = p
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

func uniqueViolation(err error) bool {
	var e *sqlite.Error
	return errors.As(err, &e) && (e.Code() == sqlite3.SQLITE_CONSTRAINT_UNIQUE || e.Code() == sqlite3.SQLITE_CONSTRAINT_PRIMARYKEY)
}

// contractRefusal keeps a contract error's code and drops its detail, which
// can quote the request (a case-variant key name, for example).
func contractRefusal(err error) error {
	code := contracts.ErrorCode(err)
	if code == "" {
		code = "INVALID_PURCHASE"
	}
	return refuse(code, "refused by a contract check")
}

func (s *Store) offered(po contracts.PurchaseOrder) bool {
	c, err := canonical(po.Quote)
	if err != nil {
		return false
	}
	for _, q := range s.offers[OfferKey{po.Intent.HTTPMethod, po.Intent.ResourceURL}] {
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
				if err := t.close(p, "APPROVAL_WITHDRAWN", "withdrawn", "HOST_CANCELLED"); err != nil {
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
	tick := time.NewTicker(every)
	defer tick.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-tick.C:
			if err := s.ExpireDue(); err != nil && onErr != nil {
				onErr(err)
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
	DeadlineAt             uint64                   `json:"deadline_at,omitempty"` // Asks: decided_at + deadline
	ApprovalClosed         string                   `json:"approval_closed,omitempty"`
	ClosedAt               uint64                   `json:"closed_at,omitempty"`
}

// TaskView is a committed, consistent snapshot of one task. AsOf names the
// last committed event; it does not claim to be current as of now.
type TaskView struct {
	TaskID         string         `json:"task_id"`
	Status         string         `json:"status"`
	DenyReason     string         `json:"deny_reason,omitempty"`
	AsOfSequence   uint64         `json:"as_of_sequence"`
	AsOfObservedAt uint64         `json:"as_of_observed_at"`
	Purchases      []PurchaseView `json:"purchases"`
}

func (t *taskTx) view(p *purchaseRow) PurchaseView {
	i := p.Order.Intent
	v := PurchaseView{PurchaseOrderID: i.PurchaseOrderID, RequestID: p.Order.RequestID, HTTPMethod: i.HTTPMethod, ResourceURL: i.ResourceURL,
		ChainID: i.ChainID, AssetContract: i.AssetContract, PayTo: i.PayTo, Amount: i.Amount, IntentHash: p.Decision.IntentHash,
		PaymentRequirementHash: i.PaymentRequirementHash, Decision: p.Decision, DecidedAt: p.DecidedAt,
		ApprovalClosed: p.ApprovalClosed.String, ClosedAt: uint64(p.ClosedAt.Int64)}
	if a, ok := t.reg.TrustedAsset(i.ChainID, i.AssetContract); ok {
		v.AssetID = a.AssetID
	}
	if p.Decision.PolicyVerdict == "Ask" {
		v.DeadlineAt = p.DecidedAt + t.terms.ApprovalDeadlineSeconds
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

// Task returns the session's task as last committed.
func (s *Store) Task(sess contracts.Session) (TaskView, error) {
	var v TaskView
	err := s.read(sess, func(t *taskTx) error {
		v = TaskView{TaskID: t.row.TaskID, Status: t.row.Status, DenyReason: t.row.DenyReason.String, Purchases: []PurchaseView{}}
		if n := len(t.events); n > 0 {
			v.AsOfSequence, v.AsOfObservedAt = t.events[n-1].Sequence, t.events[n-1].ObservedAt
		}
		for _, p := range t.purchases {
			v.Purchases = append(v.Purchases, t.view(p))
		}
		sort.Slice(v.Purchases, func(i, j int) bool {
			return v.Purchases[i].DecidedAt < v.Purchases[j].DecidedAt || v.Purchases[i].DecidedAt == v.Purchases[j].DecidedAt && v.Purchases[i].PurchaseOrderID < v.Purchases[j].PurchaseOrderID
		})
		return nil
	})
	return v, err
}

// Events returns the session's task events as last committed.
func (s *Store) Events(sess contracts.Session) ([]contracts.AuditEvent, error) {
	var events []contracts.AuditEvent
	err := s.read(sess, func(t *taskTx) error { events = t.events; return nil })
	return events, err
}
