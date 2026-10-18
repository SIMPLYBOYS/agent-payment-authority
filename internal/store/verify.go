package store

import (
	"bytes"
	"database/sql"
	"errors"
	"fmt"
	"sort"

	"github.com/SIMPLYBOYS/agent-payment-authority/internal/contracts"
	"github.com/SIMPLYBOYS/agent-payment-authority/internal/paycontract"
	"github.com/SIMPLYBOYS/agent-payment-authority/internal/policy"
)

// verify is the design's recovery check (section 8), run before the Store
// serves anything. Everything is read in one read transaction, so commits
// from other connections cannot make two points in time look inconsistent.
// It uses only what is stored: never the current offer book or registry
// config. A content mismatch is a *CorruptError; a read that fails (a query,
// a row, or the end of a result set) is a *ReadError, never taken for the
// end of the data. Either way nothing is repaired and Open serves nothing.
//
// It finds inconsistency. It cannot stop someone who rewrites snapshots,
// rows, events and chain head together, and without an external checkpoint
// it cannot guarantee to notice a truncated tail or a recomputed chain.
// version is the schema the file must have: 1 when checking before an
// upgrade, otherwise schemaVersion; host_runs is checked from 2 on.
func (s *Store) verify(version int) error {
	old := s.legacy
	s.legacy = version < 3
	defer func() { s.legacy = old }()
	tx, err := s.r.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	bad := func(check, task, format string, a ...any) error {
		return &CorruptError{Check: check, TaskID: task, Detail: fmt.Sprintf(format, a...)}
	}
	// classify keeps a failed read a read failure and wraps anything else as
	// corruption of the content being checked.
	classify := func(check, task string, err error, what string) error {
		var re *ReadError
		if errors.As(err, &re) {
			return readErr(check, task, err)
		}
		return &CorruptError{Check: check, TaskID: task, Detail: fmt.Sprintf("%s: %v", what, err), Err: err}
	}

	// 1. SQLite structure (integrity_check, not quick_check, which skips UNIQUE
	// and index content), foreign keys (integrity_check does not look) and
	// schema version.
	var ok string
	if err := tx.QueryRow(`PRAGMA integrity_check`).Scan(&ok); err != nil {
		return readErr("integrity", "", err)
	} else if ok != "ok" {
		return bad("integrity", "", "integrity_check: %s", ok)
	}
	fk, err := tx.Query(`PRAGMA foreign_key_check`)
	if err != nil {
		return readErr("foreign_keys", "", err)
	}
	violated := fk.Next()
	if err := drained(fk); err != nil {
		return readErr("foreign_keys", "", err)
	}
	if violated {
		return bad("foreign_keys", "", "foreign_key_check reports a broken reference")
	}
	var got int
	if err := tx.QueryRow(`PRAGMA user_version`).Scan(&got); err != nil {
		return readErr("schema", "", err)
	} else if got != version {
		return bad("schema", "", "schema version %d is not %d", got, version)
	}

	// 2. Snapshots: hash, canonical body and a strict decode as their kind.
	snaps, err := tx.Query(`SELECT snapshot_hash, kind, body FROM config_snapshots`)
	if err != nil {
		return readErr("snapshot", "", err)
	}
	type snap struct{ kind, body string }
	all := map[string]snap{}
	for snaps.Next() {
		var h string
		var sn snap
		if err := snaps.Scan(&h, &sn.kind, &sn.body); err != nil {
			snaps.Close()
			return readErr("snapshot", "", err)
		}
		all[h] = sn
	}
	if err := drained(snaps); err != nil {
		return readErr("snapshot", "", err)
	}
	for h, sn := range all {
		c, err := contracts.Canonical([]byte(sn.body))
		if err != nil || !bytes.Equal(c, []byte(sn.body)) || digest(c) != h {
			return bad("snapshot", "", "snapshot %s: body is not canonical or does not match its hash", h)
		}
		var derr error
		switch sn.kind {
		case "registry":
			var r contracts.RegistryConfig
			if derr = contracts.Decode(c, &r); derr == nil {
				_, derr = contracts.NewRegistry(r)
			}
		case "profile":
			var p policy.Profile
			if derr = contracts.Decode(c, &p); derr == nil {
				derr = p.Validate()
			}
		case "credential":
			var cr policy.Credential
			if derr = contracts.Decode(c, &cr); derr == nil && (cr.EvidenceMode != "fixture" || cr.ValidUntil == 0) {
				derr = fmt.Errorf("not an M1 fixture credential")
			}
		case "credential_bbs":
			var b BBSCredential
			if derr = contracts.Decode(c, &b); derr == nil {
				derr = b.validate()
			}
		case "terms":
			var t terms
			derr = contracts.Decode(c, &t)
		}
		if derr != nil {
			return bad("snapshot", "", "snapshot %s does not decode as %s: %v", h, sn.kind, derr)
		}
	}

	rows, err := tx.Query(`SELECT ` + s.taskCols() + ` FROM tasks ORDER BY task_id`)
	if err != nil {
		return readErr("task", "", err)
	}
	var tasks []taskRow
	for rows.Next() {
		t, err := scanTask(rows)
		if err != nil {
			rows.Close()
			return readErr("task", "", err)
		}
		tasks = append(tasks, t)
	}
	if err := drained(rows); err != nil {
		return readErr("task", "", err)
	}
	wallets := map[string][2]string{}
	walletOwner, payerOwner := map[string]string{}, map[string]string{}
	for _, row := range tasks {
		id := row.TaskID
		t, err := s.openTask(tx, 0, id)
		if err != nil {
			return classify("task", id, err, "task does not load")
		}

		// 2b. References: terms bound to this task; present/absent agrees
		// with the columns; a present credential belongs to this consumer.
		tm := t.terms
		var profile *policy.Profile
		if tm.TaskID != id || tm.ProfileState == "present" != row.ProfileSnap.Valid || tm.CredentialState == "present" != row.CredentialSnap.Valid {
			return bad("snapshot", id, "terms do not match the task or its profile/credential columns")
		}
		if row.ProfileSnap.Valid {
			profile = new(policy.Profile)
			if err := decodeSnapshot(tx, row.ProfileSnap.String, "profile", profile); err != nil {
				return classify("snapshot", id, err, "profile reference")
			}
		}
		// apa.pay.v2 keeps a credential_bbs snapshot of this task; the others
		// the fixture (a v2 task with a fixture is corrupt, and the reverse).
		if row.CredentialSnap.Valid && row.ContractVersion == paycontract.V2 {
			if b, err := t.bbs(); err != nil {
				return classify("snapshot", id, err, "credential reference")
			} else if b.ConsumerID != row.ConsumerID || b.TaskID != id || b.Expiry > row.StartedAt+MaxCredentialTTL {
				return bad("snapshot", id, "credential_bbs is another task's or consumer's, or outlives its TTL")
			}
		} else if row.CredentialSnap.Valid {
			var cr policy.Credential
			if err := decodeSnapshot(tx, row.CredentialSnap.String, "credential", &cr); err != nil {
				return classify("snapshot", id, err, "credential reference")
			} else if cr.ConsumerID != row.ConsumerID {
				return bad("snapshot", id, "credential belongs to another consumer")
			}
		}
		if err := tm.validate(t.regCfg, profile); err != nil {
			return bad("snapshot", id, "terms: %v", err)
		}

		// 3. Task and wallet: the registry binding, and one wallet per consumer.
		bt, bw, err := t.reg.Binding(id)
		if err != nil || bt.ConsumerID != row.ConsumerID || bt.AgentID != row.AgentID || bw.WalletID != row.WalletID || bw.From != row.Payer {
			return bad("task", id, "task does not match its registry binding")
		}
		w := [2]string{row.WalletID, row.Payer}
		if prev, seen := wallets[row.ConsumerID]; seen && prev != w {
			return bad("task", id, "consumer %s uses two wallets", row.ConsumerID)
		}
		wallets[row.ConsumerID] = w
		// Each task matches its own registry snapshot, so two snapshots can
		// hand one wallet to two consumers; ownership is checked across all
		// stored tasks, ended ones included.
		if owner, seen := walletOwner[row.WalletID]; seen && owner != row.ConsumerID {
			return bad("wallet", id, "wallet_id %s also belongs to consumer %s", row.WalletID, owner)
		}
		if owner, seen := payerOwner[row.Payer]; seen && owner != row.ConsumerID {
			return bad("wallet", id, "payer %s also belongs to consumer %s", row.Payer, owner)
		}
		walletOwner[row.WalletID], payerOwner[row.Payer] = row.ConsumerID, row.ConsumerID

		// 4. Budget: M1 deducts nothing; from schema 7 a reservation does
		// (checked with the task's payments, step 11).
		if version < 7 && row.BudgetRemaining != string(tm.BudgetInitial) {
			return bad("budget", id, "budget_remaining %s is not budget_initial %s", row.BudgetRemaining, tm.BudgetInitial)
		}
		if version >= 7 && !paycontract.Supported(row.ContractVersion) && row.BudgetRemaining != string(tm.BudgetInitial) {
			return bad("budget", id, "budget_remaining %s is not budget_initial %s", row.BudgetRemaining, tm.BudgetInitial)
		}

		// 5. Purchases: canonical body, hashes and index columns.
		prow, err := tx.Query(`SELECT purchase_order_id, request_id, retry_key, chain_id, asset_contract, payer, eip3009_nonce, body, intent_hash FROM purchases WHERE task_id = ?`, id)
		if err != nil {
			return readErr("purchase", id, err)
		}
		for prow.Next() {
			var po, req, key, chain, asset, payer, nonce, body, intent string
			if err := prow.Scan(&po, &req, &key, &chain, &asset, &payer, &nonce, &body, &intent); err != nil {
				prow.Close()
				return readErr("purchase", id, err)
			}
			p := t.purchases[po]
			c, cerr := contracts.Canonical([]byte(body))
			var h, k string
			var herr, kerr, verr error
			if p.V1 != nil { // apa.pay.v1: its own hashes and ownership rules
				h, herr = p.V1.IntentHash()
				k, kerr = p.V1.RetryKey()
				verr = paycontract.ValidatePurchase(t.reg, t.session(), *p.V1)
			} else {
				h, herr = p.Order.Intent.Hash()
				k, kerr = p.Order.RetryKey()
				verr = t.reg.ValidatePurchase(t.session(), p.Order)
			}
			x := p.terms()
			if cerr != nil || !bytes.Equal(c, []byte(body)) || herr != nil || h != intent || kerr != nil || k != key ||
				x.PO != po || x.RequestID != req || x.ChainID != chain || x.Asset != asset || x.From != payer || x.Nonce != nonce {
				prow.Close()
				return bad("purchase", id, "purchase %s: body, hashes or index columns disagree", po)
			}
			if verr != nil {
				prow.Close()
				return bad("purchase", id, "purchase %s: %v", po, verr)
			}
		}
		if err := drained(prow); err != nil {
			return readErr("purchase", id, err)
		}

		// 7. Events: canonical body, index columns, ids and hashes, then the
		// chain and the contract's order rules.
		erow, err := tx.Query(`SELECT sequence, event_id, event_type, purchase_order_id, observed_at, body, event_hash FROM events WHERE task_id = ? ORDER BY sequence`, id)
		if err != nil {
			return readErr("event", id, err)
		}
		n := 0
		for erow.Next() {
			var seq, observed uint64
			var eid, typ, body, hash string
			var po sql.NullString
			if err := erow.Scan(&seq, &eid, &typ, &po, &observed, &body, &hash); err != nil {
				erow.Close()
				return readErr("event", id, err)
			}
			e := t.events[n]
			n++
			c, cerr := contracts.Canonical([]byte(body))
			eh, herr := t.eventHash(e)
			if cerr != nil || !bytes.Equal(c, []byte(body)) || herr != nil || eh != hash || e.Sequence != seq || e.EventID != eid ||
				eid != eventID(id, seq) || e.EventType != typ || e.ObservedAt != observed || e.Scope == nil || e.Scope.TaskID != id ||
				e.Scope.PurchaseOrderID != po.String {
				erow.Close()
				return bad("event", id, "event %d: body, hash or index columns disagree", seq)
			}
			if err := t.validateEvent(e); err != nil {
				erow.Close()
				return bad("event", id, "event %d: %v", seq, err)
			}
		}
		if err := drained(erow); err != nil {
			return readErr("event", id, err)
		}
		if len(t.events) == 0 {
			return bad("chain", id, "task has no events")
		}
		last := t.events[len(t.events)-1]
		head, _ := t.eventHash(last)
		if err := t.validateOrder(); err != nil {
			return bad("chain", id, "%v", err)
		}
		if row.ChainHead != head || row.NextSeq != last.Sequence+1 {
			return bad("chain", id, "chain_head or next_sequence does not match the last event")
		}

		// 6. Decisions and approvals: each purchase matches its events.
		decided, closedBy := map[string]contracts.AuditEvent{}, map[string]contracts.AuditEvent{}
		for _, e := range t.events {
			switch e.EventType {
			case "POLICY_DECIDED":
				decided[e.Scope.PurchaseOrderID] = e
			case "APPROVAL_EXPIRED", "APPROVAL_WITHDRAWN", "APPROVED", "APPROVAL_DENIED", "APPROVAL_SUPERSEDED":
				closedBy[e.Scope.PurchaseOrderID] = e
			}
		}
		pos := make([]string, 0, len(t.purchases))
		for po := range t.purchases {
			pos = append(pos, po)
		}
		sort.Strings(pos)
		for _, po := range pos {
			p := t.purchases[po]
			d, ok := decided[po]
			if !ok || *d.Payload.Decision != p.Decision || d.ObservedAt != p.DecidedAt {
				return bad("decision", id, "purchase %s does not match its POLICY_DECIDED event", po)
			}
			c, closed := closedBy[po]
			switch {
			case closed && (!p.ApprovalClosed.Valid || p.ApprovalClosed.String != c.Payload.Decision.ApprovalStatus || uint64(p.ClosedAt.Int64) != c.ObservedAt || !p.ClosedAt.Valid):
				return bad("decision", id, "purchase %s approval closure does not match its event", po)
			case !closed && (p.ApprovalClosed.Valid || p.ClosedAt.Valid):
				return bad("decision", id, "purchase %s is closed without a closing event", po)
			}
			delete(decided, po)
		}
		if len(decided) > 0 {
			return bad("decision", id, "POLICY_DECIDED without a purchase row")
		}

		// 8. Projection: the shared rule over the events equals the columns.
		status, reason := project(t.events)
		if status != row.Status || reason != row.DenyReason.String || (reason == "") == row.DenyReason.Valid {
			return bad("projection", id, "status %s/%s is not what the events give (%s/%s)", row.Status, row.DenyReason.String, status, reason)
		}

		// 9. apa.pay.v1: the delegation, sessions and grants match the events.
		if why := t.verifyPay(); why != "" {
			return bad("approval", id, "%s", why)
		}

		// 10. apa.pay.v2: the credential records match the events, the
		// snapshot and the challenges (design 3.3).
		if why := t.verifyCredentials(); why != "" {
			return bad("credential", id, "%s", why)
		}

		// 11. P2: the payments match the payment events and the budget.
		if version >= 7 && t.pay() {
			if why := t.verifyPaid(tm.BudgetInitial); why != "" {
				return bad("payment", id, "%s", why)
			}
		}
	}
	if version >= 3 {
		if err := verifyPayTables(tx); err != nil {
			return err
		}
	}
	if version >= 4 {
		if err := verifyPayments(tx); err != nil {
			return err
		}
	}
	if version >= 5 {
		if err := verifyApprovals(tx); err != nil {
			return err
		}
	}
	if version >= 6 {
		if err := verifyCredentialTables(tx); err != nil {
			return err
		}
	}
	if version >= 7 {
		if err := verifyReceipts(tx); err != nil {
			return err
		}
	}
	if version >= 2 {
		return verifyRuns(tx, version)
	}
	return nil
}
