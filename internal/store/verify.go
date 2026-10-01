package store

import (
	"bytes"
	"database/sql"
	"fmt"
	"sort"

	"github.com/SIMPLYBOYS/agent-payment-authority/internal/contracts"
	"github.com/SIMPLYBOYS/agent-payment-authority/internal/policy"
)

// verify is the design's recovery check (section 8), run before the Store
// serves anything. Everything is read in one read transaction, so commits
// from other connections cannot make two points in time look inconsistent.
// It uses only what is stored: never the current offer book or registry
// config. Any mismatch is a *CorruptError; nothing is repaired.
//
// It finds inconsistency. It cannot stop someone who rewrites snapshots,
// rows, events and chain head together, and without an external checkpoint
// it cannot guarantee to notice a truncated tail or a recomputed chain.
func (s *Store) verify() error {
	tx, err := s.r.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	bad := func(check, task, format string, a ...any) error {
		return &CorruptError{Check: check, TaskID: task, Detail: fmt.Sprintf(format, a...)}
	}

	// 1. SQLite structure (integrity_check, not quick_check, which skips UNIQUE
	// and index content), foreign keys (integrity_check does not look) and
	// schema version.
	var ok string
	if err := tx.QueryRow(`PRAGMA integrity_check`).Scan(&ok); err != nil || ok != "ok" {
		return bad("integrity", "", "integrity_check: %s %v", ok, err)
	}
	fk, err := tx.Query(`PRAGMA foreign_key_check`)
	if err != nil {
		return err
	}
	violated := fk.Next()
	fk.Close()
	if violated {
		return bad("foreign_keys", "", "foreign_key_check reports a broken reference")
	}
	var version int
	if err := tx.QueryRow(`PRAGMA user_version`).Scan(&version); err != nil || version != schemaVersion {
		return bad("schema", "", "schema version %d is not %d", version, schemaVersion)
	}

	// 2. Snapshots: hash, canonical body and a strict decode as their kind.
	snaps, err := tx.Query(`SELECT snapshot_hash, kind, body FROM config_snapshots`)
	if err != nil {
		return err
	}
	type snap struct{ kind, body string }
	all := map[string]snap{}
	for snaps.Next() {
		var h string
		var sn snap
		if err := snaps.Scan(&h, &sn.kind, &sn.body); err != nil {
			snaps.Close()
			return err
		}
		all[h] = sn
	}
	snaps.Close()
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
		case "terms":
			var t terms
			derr = contracts.Decode(c, &t)
		}
		if derr != nil {
			return bad("snapshot", "", "snapshot %s does not decode as %s: %v", h, sn.kind, derr)
		}
	}

	rows, err := tx.Query(`SELECT ` + taskCols + ` FROM tasks ORDER BY task_id`)
	if err != nil {
		return err
	}
	var tasks []taskRow
	for rows.Next() {
		t, err := scanTask(rows)
		if err != nil {
			rows.Close()
			return err
		}
		tasks = append(tasks, t)
	}
	rows.Close()
	wallets := map[string][2]string{}
	for _, row := range tasks {
		id := row.TaskID
		t, err := s.openTask(tx, 0, id)
		if err != nil {
			return bad("task", id, "task does not load: %v", err)
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
				return bad("snapshot", id, "profile reference: %v", err)
			}
		}
		if row.CredentialSnap.Valid {
			var cr policy.Credential
			if err := decodeSnapshot(tx, row.CredentialSnap.String, "credential", &cr); err != nil || cr.ConsumerID != row.ConsumerID {
				return bad("snapshot", id, "credential reference is broken or belongs to another consumer")
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

		// 4. Budget: M1 deducts nothing.
		if row.BudgetRemaining != string(tm.BudgetInitial) {
			return bad("budget", id, "budget_remaining %s is not budget_initial %s", row.BudgetRemaining, tm.BudgetInitial)
		}

		// 5. Purchases: canonical body, hashes and index columns.
		prow, err := tx.Query(`SELECT purchase_order_id, request_id, retry_key, chain_id, asset_contract, payer, eip3009_nonce, body, intent_hash FROM purchases WHERE task_id = ?`, id)
		if err != nil {
			return err
		}
		for prow.Next() {
			var po, req, key, chain, asset, payer, nonce, body, intent string
			if err := prow.Scan(&po, &req, &key, &chain, &asset, &payer, &nonce, &body, &intent); err != nil {
				prow.Close()
				return err
			}
			p := t.purchases[po]
			c, cerr := contracts.Canonical([]byte(body))
			o := p.Order
			i := o.Intent
			h, herr := i.Hash()
			k, kerr := o.RetryKey()
			if cerr != nil || !bytes.Equal(c, []byte(body)) || herr != nil || h != intent || kerr != nil || k != key ||
				i.PurchaseOrderID != po || o.RequestID != req || i.ChainID != chain || i.AssetContract != asset || i.From != payer || i.EIP3009Nonce != nonce {
				prow.Close()
				return bad("purchase", id, "purchase %s: body, hashes or index columns disagree", po)
			}
			if err := t.reg.ValidatePurchase(t.session(), o); err != nil {
				prow.Close()
				return bad("purchase", id, "purchase %s: %v", po, err)
			}
		}
		prow.Close()

		// 7. Events: canonical body, index columns, ids and hashes, then the
		// chain and the contract's order rules.
		erow, err := tx.Query(`SELECT sequence, event_id, event_type, purchase_order_id, observed_at, body, event_hash FROM events WHERE task_id = ? ORDER BY sequence`, id)
		if err != nil {
			return err
		}
		n := 0
		for erow.Next() {
			var seq, observed uint64
			var eid, typ, body, hash string
			var po sql.NullString
			if err := erow.Scan(&seq, &eid, &typ, &po, &observed, &body, &hash); err != nil {
				erow.Close()
				return err
			}
			e := t.events[n]
			n++
			c, cerr := contracts.Canonical([]byte(body))
			eh, herr := contracts.EventHash(e)
			if cerr != nil || !bytes.Equal(c, []byte(body)) || herr != nil || eh != hash || e.Sequence != seq || e.EventID != eid ||
				eid != eventID(id, seq) || e.EventType != typ || e.ObservedAt != observed || e.Scope == nil || e.Scope.TaskID != id ||
				e.Scope.PurchaseOrderID != po.String {
				erow.Close()
				return bad("event", id, "event %d: body, hash or index columns disagree", seq)
			}
			var order *contracts.PurchaseOrder
			if po.Valid {
				order = &t.purchases[po.String].Order
			}
			if err := t.reg.ValidateEvent(e, order); err != nil {
				erow.Close()
				return bad("event", id, "event %d: %v", seq, err)
			}
		}
		erow.Close()
		if len(t.events) == 0 {
			return bad("chain", id, "task has no events")
		}
		last := t.events[len(t.events)-1]
		head, _ := contracts.EventHash(last)
		if err := contracts.ValidateEventOrder(t.events); err != nil {
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
			case "APPROVAL_EXPIRED", "APPROVAL_WITHDRAWN":
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
	}
	return nil
}
