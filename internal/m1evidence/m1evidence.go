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
	"bytes"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"maps"
	"math/big"
	"os"
	"path/filepath"
	"reflect"
	"slices"
	"strings"

	"github.com/SIMPLYBOYS/agent-payment-authority/internal/contracts"
	"github.com/SIMPLYBOYS/agent-payment-authority/internal/policy"
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
	if err := c.settings(t, cfg); err != nil {
		return nil, err
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

// settings checks the profile and credential snapshots against the terms,
// as the Store's own recovery check does: a setting the terms call present
// has its snapshot and one they call absent has none; a present profile is
// valid and holds the task's budget and asset; a present credential is this
// consumer's M1 fixture. A missing configuration is legitimate only where
// the terms say absent (deny-missing-config).
func (c *checked) settings(t Task, cfg contracts.RegistryConfig) error {
	tm := c.terms
	for kind, state := range map[string]string{"profile": tm.ProfileState, "credential": tm.CredentialState} {
		_, has := t.Snapshots[kind]
		if state != "present" && state != "absent" {
			return fmt.Errorf("terms %s_state %q", kind, state)
		}
		if (state == "present") != has {
			return fmt.Errorf("terms say the %s is %s, the snapshot is present: %v", kind, state, has)
		}
	}
	if !slices.ContainsFunc(cfg.Assets, func(a contracts.Asset) bool { return a.AssetID == tm.AssetID }) {
		return fmt.Errorf("terms asset %s is not in the registry snapshot", tm.AssetID)
	}
	if s, ok := t.Snapshots["profile"]; ok {
		var p policy.Profile
		if err := contracts.Decode([]byte(s.Body), &p); err != nil {
			return fmt.Errorf("profile snapshot: %w", err)
		}
		if err := p.Validate(); err != nil {
			return fmt.Errorf("profile snapshot: %w", err)
		}
		budget, _ := tm.BudgetInitial.Units()
		limit, _ := p.TaskCap.Units()
		if budget > limit || tm.AssetID != p.AssetID {
			return errors.New("terms budget or asset outside the profile")
		}
	}
	if s, ok := t.Snapshots["credential"]; ok {
		var cr policy.Credential
		if err := contracts.Decode([]byte(s.Body), &cr); err != nil {
			return fmt.Errorf("credential snapshot: %w", err)
		}
		if cr.ConsumerID != t.ConsumerID || cr.EvidenceMode != "fixture" || cr.ValidUntil == 0 {
			return errors.New("the credential snapshot is not this consumer's M1 fixture")
		}
	}
	return nil
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

// Trusted are the frozen fixtures a contracts case's inputs come from
// (testdata/contracts): the purchase every case starts from and the
// registry that says who A and B are.
type Trusted struct {
	Purchase json.RawMessage
	Registry json.RawMessage
}

// LoadTrusted reads them from the repository at root.
func LoadTrusted(root string) (Trusted, error) {
	p, err1 := os.ReadFile(filepath.Join(root, "testdata/contracts/purchase-input.json"))
	r, err2 := os.ReadFile(filepath.Join(root, "testdata/contracts/registry.json"))
	return Trusted{p, r}, errors.Join(err1, err2)
}

// CheckContract checks that a contracts case's record shows that case: the
// recorded inputs are the trusted fixtures with exactly the changes the
// frozen case names (inputs), the contract code rerun on them gives what
// the test recorded, and that is the expected outcome. The reason of an
// accepted result (VALID_BINDING, SAME_PAYLOAD, NEW_REQUEST) is the case
// list's name for it; only a rejection has a code to observe.
func CheckContract(c contracts.AcceptanceCase, x Contract, trusted Trusted) (ContractObserved, []string) {
	var problems []string
	if x.CaseID != c.CaseID || x.Operation != c.Operation || x.Layer != "contracts_test" {
		return ContractObserved{}, []string{"the record is not this case's contracts test"}
	}
	for k, v := range c.Parameters {
		if x.Parameters[k] != v {
			problems = append(problems, fmt.Sprintf("parameter %s %q, the case says %q", k, x.Parameters[k], v))
		}
	}
	problems = append(problems, inputs(c, x, trusted)...)
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

// change is what one field of an input must be where the case changes it.
type change func(v any) bool

func to(want any) change { return func(v any) bool { return reflect.DeepEqual(v, want) } }
func other(base any) change {
	return func(v any) bool { return v != nil && !reflect.DeepEqual(v, base) }
}

// inputs compares the recorded inputs with the trusted fixtures, field by
// field: the fields the frozen case changes must hold the change, every
// other field must be the fixture's. The case's parameters come from the
// frozen case, never from the record.
func inputs(c contracts.AcceptanceCase, x Contract, trusted Trusted) []string {
	var problems []string
	bad := func(format string, a ...any) { problems = append(problems, fmt.Sprintf(format, a...)) }
	var cfg contracts.RegistryConfig
	var base contracts.PurchaseOrder
	if err := errors.Join(contracts.Decode(trusted.Registry, &cfg), contracts.Decode(trusted.Purchase, &base)); err != nil {
		return []string{"trusted fixtures: " + err.Error()}
	}
	reg, err := contracts.NewRegistry(cfg)
	if err != nil {
		return []string{"trusted registry: " + err.Error()}
	}
	baseRaw, _ := json.Marshal(base)
	baseFlat := flatten(baseRaw)
	// who is A's or B's identity in the trusted registry (task-a, task-b).
	who := func(letter string) (contracts.Task, contracts.WalletRef, bool) {
		t, w, err := reg.Binding("task-" + letter)
		return t, w, err == nil && (letter == "a" || letter == "b")
	}
	// as are the changes that make the trusted purchase another identity's.
	as := func(letter string) map[string]change {
		t, w, ok := who(letter)
		if !ok {
			bad("no identity %q in the trusted registry", letter)
			return nil
		}
		ch := map[string]change{}
		for k, v := range map[string]string{"intent.consumer_id": t.ConsumerID, "intent.agent_id": t.AgentID, "intent.wallet_id": t.WalletID,
			"intent.task_id": t.TaskID, "intent.from": w.From} {
			if v != baseFlat[k] {
				ch[k] = to(v)
			}
		}
		if len(ch) > 0 {
			ch["intent.purchase_order_id"] = other(baseFlat["intent.purchase_order_id"])
		}
		return ch
	}
	compare := func(name string, raw json.RawMessage, changes map[string]change) {
		if len(raw) == 0 {
			bad("no %s recorded", name)
			return
		}
		got := flatten(raw)
		for k := range union(got, baseFlat) {
			if ch, ok := changes[k]; ok {
				if !ch(got[k]) {
					bad("%s %s is %v, not the case's change", name, k, got[k])
				}
			} else if !reflect.DeepEqual(got[k], baseFlat[k]) {
				bad("%s %s is %v, the trusted fixture's is %v and the case does not change it", name, k, got[k], baseFlat[k])
			}
		}
		for k, ch := range changes {
			if _, ok := got[k]; !ok && !ch(nil) {
				bad("%s has no %s", name, k)
			}
		}
	}
	p := c.Parameters
	switch c.Operation {
	case "binding":
		t, _, ok := who(p["session"])
		if !ok {
			bad("session %q has no identity", p["session"])
		} else if x.Session == nil || *x.Session != (contracts.Session{ConsumerID: t.ConsumerID, AgentID: t.AgentID, TaskID: t.TaskID}) {
			bad("session %+v is not %s's", x.Session, p["session"])
		}
		changes := as(p["purchase"])
		if m := p["mutation"]; m != "" {
			_, wb, okB := who("b")
			tb, _, _ := who("b")
			field := map[string]string{"wallet_b": "intent.wallet_id", "from_b": "intent.from", "consumer_b": "intent.consumer_id", "agent_b": "intent.agent_id"}[m]
			value := map[string]string{"wallet_b": tb.WalletID, "from_b": wb.From, "consumer_b": tb.ConsumerID, "agent_b": tb.AgentID}[m]
			if field == "" || !okB {
				bad("mutation %q has no derivation", m)
			} else {
				changes[field] = to(value)
			}
		}
		compare("purchase", x.Purchase, changes)
		if !reflect.DeepEqual(flatten(x.Registry), flatten(trusted.Registry)) {
			bad("the recorded registry is not the trusted one")
		}
	case "compare-retry":
		compare("existing purchase", x.Purchase, nil)
		var changes map[string]change
		switch p["mutation"] {
		case "none":
		case "nonce":
			changes = map[string]change{"intent.eip3009_nonce": other(baseFlat["intent.eip3009_nonce"])}
		case "request_id":
			changes = map[string]change{"request_id": other(baseFlat["request_id"])}
		case "other_consumer":
			changes = as("b")
		default:
			bad("mutation %q has no derivation", p["mutation"])
		}
		compare("incoming purchase", x.Incoming, changes)
	case "validate-decision":
		h, _ := base.Intent.Hash()
		d := x.Decision
		if d == nil {
			bad("no decision recorded")
			break
		}
		if d.PaymentStatus != p["payment_status"] || d.IntentHash != h {
			bad("the decision is not the trusted purchase's with payment_status %s", p["payment_status"])
		}
		// The named field is the only defect: with it unset, the decision is valid.
		fixed := *d
		fixed.PaymentStatus = "unavailable"
		if err := fixed.Validate(); err != nil {
			bad("the decision has another defect: %v", err)
		}
	case "validate-purchase":
		req := "quote.selected_requirement."
		var changes map[string]change
		switch m := p["mutation"]; m {
		case "transfer_method_erc7710", "transfer_method_permit2":
			changes = map[string]change{req + "extra.assetTransferMethod": to(strings.TrimPrefix(m, "transfer_method_"))}
		case "case_variant_pay_to":
			changes = map[string]change{req + "payto": func(v any) bool { s, ok := v.(string); return ok && s != "" }}
		default:
			bad("mutation %q has no derivation", m)
		}
		if changes != nil {
			// The requirement digest follows the quote when it can be computed.
			changes["intent.payment_requirement_hash"] = func(v any) bool { return requirementHash(x.Purchase, v, baseFlat) }
		}
		compare("purchase", x.Purchase, changes)
	default:
		bad("operation %s has no derivation", c.Operation)
	}
	return problems
}

// requirementHash: the recorded payment_requirement_hash is the quote's own
// digest when that can be computed, and the fixture's otherwise.
func requirementHash(raw json.RawMessage, v any, baseFlat map[string]any) bool {
	var p contracts.PurchaseOrder
	if json.Unmarshal(raw, &p) != nil {
		return false
	}
	if h, err := p.Quote.Hash(); err == nil {
		return v == h
	}
	return reflect.DeepEqual(v, baseFlat["intent.payment_requirement_hash"])
}

// flatten maps a JSON document to path -> value (objects by key, arrays by
// index); numbers stay exact. Invalid JSON flattens to nothing.
func flatten(raw []byte) map[string]any {
	out := map[string]any{}
	d := json.NewDecoder(bytes.NewReader(raw))
	d.UseNumber()
	var v any
	if d.Decode(&v) != nil {
		return out
	}
	var walk func(prefix string, v any)
	walk = func(prefix string, v any) {
		switch t := v.(type) {
		case map[string]any:
			for k, e := range t {
				walk(prefix+k+".", e)
			}
		case []any:
			for i, e := range t {
				walk(fmt.Sprintf("%s%d.", prefix, i), e)
			}
		default:
			out[strings.TrimSuffix(prefix, ".")] = v
		}
	}
	walk("", v)
	return out
}

func union(a, b map[string]any) map[string]bool {
	out := map[string]bool{}
	for k := range a {
		out[k] = true
	}
	for k := range b {
		out[k] = true
	}
	return out
}
