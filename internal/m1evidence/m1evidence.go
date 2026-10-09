// Package m1evidence is the M1-E1 evidence content (docs/m1-evidence.md): it
// exports what a Store recorded for a task, read only and as stored, and
// recomputes it against the frozen case specification. It uses the contract
// code (canonical forms, digests, registry bindings, event rules) and never
// policy.Decide: the observed outcome is derived from the recorded events,
// not from the engine that wrote them.
//
// This is the producer's own check (make m1-fixture). B1-07 checks the
// bundle independently.
package m1evidence

import (
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"maps"
	"math/big"
	"slices"

	"github.com/SIMPLYBOYS/agent-payment-authority/internal/contracts"
	_ "modernc.org/sqlite"
)

// Task is one stored task: its row, the configuration snapshots it was
// started with and its purchases and events, every body exactly as stored
// with the hash the Store stored beside it.
type Task struct {
	TaskID          string            `json:"task_id"`
	ConsumerID      string            `json:"consumer_id"`
	AgentID         string            `json:"agent_id"`
	WalletID        string            `json:"wallet_id"`
	Payer           string            `json:"payer"`
	Status          string            `json:"status"`
	DenyReason      string            `json:"deny_reason,omitempty"`
	BudgetRemaining string            `json:"budget_remaining"`
	Snapshots       map[string]Stored `json:"snapshots"` // registry, terms; profile and credential when present
	Purchases       map[string]Stored `json:"purchases"` // purchase_order_id -> body, intent_hash
	Events          []Stored          `json:"events"`    // by sequence: body, event_hash
}

// Stored is a body as the Store wrote it and the hash stored with it.
type Stored struct {
	Body string `json:"body"`
	Hash string `json:"hash"`
}

// Export reads the given tasks from a Store database, read only.
func Export(dbPath string, taskIDs ...string) ([]Task, error) {
	db, err := sql.Open("sqlite", "file:"+dbPath+"?mode=ro")
	if err != nil {
		return nil, err
	}
	defer db.Close()
	var out []Task
	for _, id := range taskIDs {
		t := Task{TaskID: id, Snapshots: map[string]Stored{}, Purchases: map[string]Stored{}, Events: []Stored{}}
		var deny sql.NullString
		snaps := map[string]*sql.NullString{"registry": {}, "profile": {}, "credential": {}, "terms": {}}
		err := db.QueryRow(`SELECT consumer_id, agent_id, wallet_id, payer, status, deny_reason, budget_remaining,
			registry_snapshot, profile_snapshot, credential_snapshot, terms_snapshot FROM tasks WHERE task_id = ?`, id).
			Scan(&t.ConsumerID, &t.AgentID, &t.WalletID, &t.Payer, &t.Status, &deny, &t.BudgetRemaining,
				snaps["registry"], snaps["profile"], snaps["credential"], snaps["terms"])
		if err != nil {
			return nil, fmt.Errorf("task %s: %w", id, err)
		}
		t.DenyReason = deny.String
		for kind, h := range snaps {
			if !h.Valid {
				continue
			}
			var k, body string
			if err := db.QueryRow(`SELECT kind, body FROM config_snapshots WHERE snapshot_hash = ?`, h.String).Scan(&k, &body); err != nil {
				return nil, fmt.Errorf("task %s: %s snapshot: %w", id, kind, err)
			}
			if k != kind {
				return nil, fmt.Errorf("task %s: snapshot %s is %s, not %s", id, h.String, k, kind)
			}
			t.Snapshots[kind] = Stored{body, h.String}
		}
		rows, err := db.Query(`SELECT purchase_order_id, body, intent_hash FROM purchases WHERE task_id = ?`, id)
		if err != nil {
			return nil, err
		}
		for rows.Next() {
			var po string
			var s Stored
			if err := rows.Scan(&po, &s.Body, &s.Hash); err != nil {
				rows.Close()
				return nil, err
			}
			t.Purchases[po] = s
		}
		if err := errors.Join(rows.Err(), rows.Close()); err != nil {
			return nil, err
		}
		rows, err = db.Query(`SELECT body, event_hash FROM events WHERE task_id = ? ORDER BY sequence`, id)
		if err != nil {
			return nil, err
		}
		for rows.Next() {
			var s Stored
			if err := rows.Scan(&s.Body, &s.Hash); err != nil {
				rows.Close()
				return nil, err
			}
			t.Events = append(t.Events, s)
		}
		if err := errors.Join(rows.Err(), rows.Close()); err != nil {
			return nil, err
		}
		out = append(out, t)
	}
	return out, nil
}

// Observed is the outcome derived from the recorded data.
type Observed struct {
	Result               string   `json:"result"`
	Reason               string   `json:"reason"`
	ApprovalStatus       string   `json:"approval_status"`
	PaymentStatus        string   `json:"payment_status"`
	BudgetDeducted       string   `json:"budget_deducted"`
	ForbiddenEventsFound []string `json:"forbidden_events_found"`
	Purchases            int      `json:"purchases"`
}

// terms mirrors the Store's terms snapshot (strict decoding: a field the
// Store adds must be added here before it is trusted).
type terms struct {
	TaskID                  string           `json:"task_id"`
	ProfileState            string           `json:"profile_state"`
	CredentialState         string           `json:"credential_state"`
	BudgetInitial           contracts.Amount `json:"budget_initial"`
	AssetID                 string           `json:"asset_id"`
	ApprovalDeadlineSeconds uint64           `json:"approval_deadline_seconds"`
}

// checked is one task after recomputation.
type checked struct {
	task      Task
	terms     terms
	events    []contracts.AuditEvent
	purchases int
	decision  *contracts.PolicyDecision // the first purchase's POLICY_DECIDED
	closure   string                    // its APPROVAL_EXPIRED / APPROVAL_WITHDRAWN, if any
	lifecycle *contracts.AuditEvent     // TASK_CANCELLED / TASK_TIMED_OUT
}

func digest(b string) string {
	h := sha256.Sum256([]byte(b))
	return "sha256:" + hex.EncodeToString(h[:])
}

// verify recomputes one task: snapshot digests, the registry binding of the
// task, each purchase (requirement and intent digests, binding) and each
// event (its stored hash, scope binding, the chain and the transition
// rules).
func verify(t Task) (*checked, error) {
	c := &checked{task: t}
	for kind, s := range t.Snapshots {
		if digest(s.Body) != s.Hash {
			return nil, fmt.Errorf("%s snapshot digest mismatch", kind)
		}
	}
	reg, ok := t.Snapshots["registry"]
	if !ok {
		return nil, errors.New("no registry snapshot")
	}
	tr, ok := t.Snapshots["terms"]
	if !ok {
		return nil, errors.New("no terms snapshot")
	}
	var cfg contracts.RegistryConfig
	if err := contracts.Decode([]byte(reg.Body), &cfg); err != nil {
		return nil, fmt.Errorf("registry snapshot: %w", err)
	}
	r, err := contracts.NewRegistry(cfg)
	if err != nil {
		return nil, fmt.Errorf("registry snapshot: %w", err)
	}
	if err := contracts.Decode([]byte(tr.Body), &c.terms); err != nil || c.terms.TaskID != t.TaskID {
		return nil, fmt.Errorf("terms snapshot does not belong to task %s (%v)", t.TaskID, err)
	}
	if _, err := c.terms.BudgetInitial.Units(); err != nil {
		return nil, fmt.Errorf("terms budget_initial: %w", err)
	}
	task, w, err := r.Binding(t.TaskID)
	if err != nil {
		return nil, err
	}
	if task.ConsumerID != t.ConsumerID || task.AgentID != t.AgentID || w.WalletID != t.WalletID || w.From != t.Payer {
		return nil, errors.New("the task row does not match its registry snapshot's binding")
	}
	orders := map[string]*contracts.PurchaseOrder{}
	for id, s := range t.Purchases {
		var po contracts.PurchaseOrder
		if err := contracts.Decode([]byte(s.Body), &po); err != nil {
			return nil, fmt.Errorf("purchase %s: %w", id, err)
		}
		if po.Intent.PurchaseOrderID != id {
			return nil, fmt.Errorf("purchase %s: stored under another purchase_order_id", id)
		}
		// The purchase itself (its requirement digest, binding and fields) is
		// validated with its PURCHASE_PROPOSED event below (ValidateEvent);
		// every stored purchase must have exactly one.
		h, err := po.Intent.Hash()
		if err != nil || h != s.Hash {
			return nil, fmt.Errorf("purchase %s: intent digest mismatch", id)
		}
		orders[id] = &po
	}
	for i, s := range t.Events {
		var e contracts.AuditEvent
		if err := contracts.Decode([]byte(s.Body), &e); err != nil {
			return nil, fmt.Errorf("event %d: %w", i+1, err)
		}
		if h, err := contracts.EventHash(e); err != nil || h != s.Hash {
			return nil, fmt.Errorf("event %d: stored hash mismatch", i+1)
		}
		if e.Scope == nil || e.Scope.TaskID != t.TaskID {
			return nil, fmt.Errorf("event %d: not this task's", i+1)
		}
		var order *contracts.PurchaseOrder
		if po := e.Scope.PurchaseOrderID; po != "" {
			if order = orders[po]; order == nil {
				return nil, fmt.Errorf("event %d: purchase %s is not recorded", i+1, po)
			}
		}
		if err := r.ValidateEvent(e, order); err != nil {
			return nil, fmt.Errorf("event %d: %w", i+1, err)
		}
		c.events = append(c.events, e)
	}
	if err := contracts.ValidateEventOrder(c.events); err != nil {
		return nil, err
	}
	first := ""
	for i, e := range c.events {
		switch e.EventType {
		case "PURCHASE_PROPOSED":
			c.purchases++
			if first == "" {
				first = e.Scope.PurchaseOrderID
			}
		case "POLICY_DECIDED":
			if e.Scope.PurchaseOrderID == first {
				c.decision = e.Payload.Decision
			}
		case "APPROVAL_EXPIRED", "APPROVAL_WITHDRAWN":
			if e.Scope.PurchaseOrderID == first {
				c.closure = e.Payload.Decision.ApprovalStatus
			}
		case "TASK_CANCELLED", "TASK_TIMED_OUT":
			c.lifecycle = &c.events[i]
		}
	}
	if c.purchases != len(t.Purchases) {
		return nil, fmt.Errorf("%d purchases stored, %d proposed in the events", len(t.Purchases), c.purchases)
	}
	return c, nil
}

// outcome reads the first purchase the way the case list states it: an
// expired Ask is denied, a withdrawn one cancelled, an open one pending.
func (c *checked) outcome() (string, string, string) {
	d := c.decision
	switch c.closure {
	case "expired":
		return "denied", "APPROVAL_TIMEOUT", "expired"
	case "withdrawn":
		return "cancelled", "HOST_CANCELLED", "withdrawn"
	}
	if d.PolicyVerdict == "Ask" {
		return "pending", d.Reason, d.ApprovalStatus
	}
	return d.PolicyVerdict, d.Reason, d.ApprovalStatus
}

// CheckIntegration recomputes an integration case from its exported tasks
// (the case's task first; restart: the task after the restart second) and
// compares the derived outcome with the case's expected outcome. Any
// problem means the evidence does not show the case.
func CheckIntegration(c contracts.AcceptanceCase, defaults map[string]string, tasks []Task) (Observed, []string) {
	var o Observed
	var problems []string
	bad := func(format string, a ...any) { problems = append(problems, fmt.Sprintf(format, a...)) }
	params := map[string]string{}
	maps.Copy(params, defaults)
	maps.Copy(params, c.Parameters)
	want := 1
	if c.Operation == "restart-and-new-task" {
		want = 2
	}
	if len(tasks) != want {
		bad("%d tasks exported, the case needs %d", len(tasks), want)
		return o, problems
	}
	var cs []*checked
	for _, t := range tasks {
		ct, err := verify(t)
		if err != nil {
			bad("task %s: %v", t.TaskID, err)
			return o, problems
		}
		cs = append(cs, ct)
	}
	first := cs[0]
	o.Purchases = first.purchases
	o.PaymentStatus = "unavailable"
	if first.decision != nil {
		o.PaymentStatus = first.decision.PaymentStatus
	}
	switch c.Operation {
	case "policy", "advance-clock":
		if first.decision == nil {
			bad("no decision recorded")
			break
		}
		if c.Operation == "policy" {
			d := first.decision
			o.Result, o.Reason, o.ApprovalStatus = d.PolicyVerdict, d.Reason, d.ApprovalStatus
		} else {
			o.Result, o.Reason, o.ApprovalStatus = first.outcome()
		}
	case "cancel", "cli-timeout":
		want := map[string]string{"cancel": "TASK_CANCELLED", "cli-timeout": "TASK_TIMED_OUT"}[c.Operation]
		if first.lifecycle == nil || first.lifecycle.EventType != want {
			bad("no %s recorded", want)
			break
		}
		o.Result, o.Reason, o.ApprovalStatus = first.task.Status, first.lifecycle.Payload.Code, "not_requested"
		if first.decision != nil {
			_, _, o.ApprovalStatus = first.outcome()
		}
		if (params["before"] == "no_purchase" || params["phase"] == "before_purchase") && o.Purchases != 0 {
			bad("the case ends before a purchase, %d recorded", o.Purchases)
		}
	case "restart-and-new-task":
		a, b := cs[0].task, cs[1].task
		if a.TaskID == b.TaskID || a.ConsumerID != b.ConsumerID {
			bad("the two tasks are not one consumer's two tasks")
			break
		}
		if a.WalletID != b.WalletID || a.Payer != b.Payer {
			o.Result = "other-wallet"
		} else {
			o.Result = "same-wallet"
		}
		o.Reason, o.ApprovalStatus = "PERSISTED_BINDING", "not_requested"
	default:
		bad("operation %s has no mapping", c.Operation)
	}
	deducted := new(big.Int)
	for _, ct := range cs {
		i, ok1 := new(big.Int).SetString(string(ct.terms.BudgetInitial), 10)
		r, ok2 := new(big.Int).SetString(ct.task.BudgetRemaining, 10)
		if !ok1 || !ok2 {
			bad("task %s: budget not readable", ct.task.TaskID)
			continue
		}
		deducted.Add(deducted, i.Sub(i, r))
		for _, e := range ct.events {
			if slices.Contains(c.Expected.ForbiddenEvents, e.EventType) && !slices.Contains(o.ForbiddenEventsFound, e.EventType) {
				o.ForbiddenEventsFound = append(o.ForbiddenEventsFound, e.EventType)
			}
		}
	}
	o.BudgetDeducted = deducted.String()
	x := c.Expected
	for _, f := range [][3]string{{"result", o.Result, x.Result}, {"reason", o.Reason, x.Reason}, {"approval_status", o.ApprovalStatus, x.ApprovalStatus},
		{"payment_status", o.PaymentStatus, x.PaymentStatus}, {"budget_deducted", o.BudgetDeducted, string(x.BudgetDeducted)}} {
		if f[1] != f[2] {
			bad("%s %q, expected %q", f[0], f[1], f[2])
		}
	}
	if len(o.ForbiddenEventsFound) > 0 {
		bad("forbidden events %v", o.ForbiddenEventsFound)
	}
	return o, problems
}

// Contract is a contracts-layer case as the test ran it: the inputs it
// called the contract code with and what it observed. Purchases keep their
// JSON as built (a quote may hold the very key variant under test).
type Contract struct {
	CaseID     string                    `json:"case_id"`
	Layer      string                    `json:"layer"` // contracts_test
	Operation  string                    `json:"operation"`
	Parameters map[string]string         `json:"parameters"`
	Registry   json.RawMessage           `json:"registry,omitempty"` // binding: the registry configuration
	Session    *contracts.Session        `json:"session,omitempty"`
	Purchase   json.RawMessage           `json:"purchase,omitempty"` // binding, validate-purchase; compare-retry: the existing one
	Incoming   json.RawMessage           `json:"incoming,omitempty"` // compare-retry
	Decision   *contracts.PolicyDecision `json:"decision,omitempty"` // validate-decision
	Observed   ContractObserved          `json:"observed"`
}

type ContractObserved struct {
	Result string `json:"result"`         // accept | reject | replay | new
	Code   string `json:"code,omitempty"` // the rejection's code
}

// RunContract calls the contract code with a case's recorded inputs.
func RunContract(x Contract) (ContractObserved, error) {
	var o ContractObserved
	purchase := func(raw json.RawMessage) (contracts.PurchaseOrder, error) {
		var p contracts.PurchaseOrder
		if len(raw) == 0 {
			return p, errors.New("no purchase recorded")
		}
		return p, json.Unmarshal(raw, &p)
	}
	verdict := func(err error) {
		o.Result = "accept"
		if err != nil {
			o.Result, o.Code = "reject", contracts.ErrorCode(err)
		}
	}
	p, perr := purchase(x.Purchase)
	switch x.Operation {
	case "compare-retry":
		in, err := purchase(x.Incoming)
		if err = errors.Join(perr, err); err != nil {
			return o, err
		}
		res, err := contracts.CompareRetry(p, in)
		o.Result = res
		if err != nil {
			o.Result, o.Code = "reject", contracts.ErrorCode(err)
		}
	case "binding":
		if perr != nil || x.Session == nil || len(x.Registry) == 0 {
			return o, errors.New("binding needs a registry, a session and a purchase")
		}
		var cfg contracts.RegistryConfig
		if err := json.Unmarshal(x.Registry, &cfg); err != nil {
			return o, err
		}
		r, err := contracts.NewRegistry(cfg)
		if err != nil {
			return o, err
		}
		verdict(r.ValidatePurchase(*x.Session, p))
	case "validate-decision":
		if x.Decision == nil {
			return o, errors.New("no decision recorded")
		}
		verdict(x.Decision.Validate())
	case "validate-purchase":
		if perr != nil {
			return o, perr
		}
		verdict(p.Validate())
	default:
		return o, fmt.Errorf("operation %s has no mapping", x.Operation)
	}
	return o, nil
}

// CheckContract reruns a contracts case from its recorded inputs and
// compares it with the expected outcome and with what the test recorded.
// The reason of an accepted result (VALID_BINDING, SAME_PAYLOAD,
// NEW_REQUEST) is the case list's name for it; only a rejection has a code
// to observe.
func CheckContract(c contracts.AcceptanceCase, x Contract) (ContractObserved, []string) {
	var problems []string
	if x.CaseID != c.CaseID || x.Operation != c.Operation || x.Layer != "contracts_test" {
		return ContractObserved{}, []string{"the record is not this case's contracts test"}
	}
	for k, v := range c.Parameters {
		if x.Parameters[k] != v {
			problems = append(problems, fmt.Sprintf("parameter %s %q, the case says %q", k, x.Parameters[k], v))
		}
	}
	o, err := RunContract(x)
	if err != nil {
		return o, append(problems, "inputs: "+err.Error())
	}
	if o != x.Observed {
		problems = append(problems, fmt.Sprintf("recorded %+v, the inputs give %+v", x.Observed, o))
	}
	if o.Result != c.Expected.Result {
		problems = append(problems, fmt.Sprintf("result %q, expected %q", o.Result, c.Expected.Result))
	}
	if o.Result == "reject" && o.Code != c.Expected.Reason {
		problems = append(problems, fmt.Sprintf("code %q, expected %q", o.Code, c.Expected.Reason))
	}
	return o, problems
}
