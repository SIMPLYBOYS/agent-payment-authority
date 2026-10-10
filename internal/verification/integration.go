package verification

import (
	"encoding/json"
	"maps"
	"math/big"
	"reflect"
	"slices"
	"strconv"

	"github.com/SIMPLYBOYS/agent-payment-authority/internal/contracts"
	"github.com/SIMPLYBOYS/agent-payment-authority/internal/policy"
)

// The records, as internal/store and cmd/demo write them
// (docs/m1-evidence.md section 5): every member named, so an unknown one
// is refused.
type stored struct {
	Body string `json:"body"`
	Hash string `json:"hash"`
}

type storedTask struct {
	TaskID          string            `json:"task_id"`
	ConsumerID      string            `json:"consumer_id"`
	AgentID         string            `json:"agent_id"`
	WalletID        string            `json:"wallet_id"`
	Payer           string            `json:"payer"`
	Status          string            `json:"status"`
	DenyReason      string            `json:"deny_reason,omitempty"`
	BudgetRemaining string            `json:"budget_remaining"`
	Snapshots       map[string]stored `json:"snapshots"`
	Purchases       map[string]stored `json:"purchases"`
	Events          []stored          `json:"events"`
}

type storeRecord struct {
	CaseID       string            `json:"case_id"`
	Layer        string            `json:"layer"`
	Harness      string            `json:"harness"`
	Operation    string            `json:"operation"`
	Parameters   map[string]string `json:"parameters"`
	TestObserved json.RawMessage   `json:"test_observed"` // the producer's; not used
	Tasks        []storedTask      `json:"tasks"`
}

type e2eRecord struct {
	CaseID       string                     `json:"case_id"`
	Layer        string                     `json:"layer"`
	Harness      string                     `json:"harness"`
	Expected     contracts.ExpectedOutcome  `json:"expected"`
	Observed     json.RawMessage            `json:"observed"` // the producer's; not used
	Tasks        []json.RawMessage          `json:"tasks"`    // GET /api/tasks/{id}
	Events       map[string]json.RawMessage `json:"events"`   // GET /api/tasks/{id}/events
	Traces       json.RawMessage            `json:"traces"`
	HostRuns     json.RawMessage            `json:"host_runs"`
	Budget       json.RawMessage            `json:"budget"`
	HostSettings map[string]json.RawMessage `json:"host_settings"`
	Store        []storedTask               `json:"store"`
	Recomputed   json.RawMessage            `json:"recomputed"` // the producer's; not used
	Problems     json.RawMessage            `json:"problems"`   // the producer's; not used
	Verdict      string                     `json:"verdict"`    // the producer's; compared only
}

type terms struct {
	TaskID                  string           `json:"task_id"`
	ProfileState            string           `json:"profile_state"`
	CredentialState         string           `json:"credential_state"`
	BudgetInitial           contracts.Amount `json:"budget_initial"`
	AssetID                 string           `json:"asset_id"`
	ApprovalDeadlineSeconds uint64           `json:"approval_deadline_seconds"`
}

// m1Events are the event types an M1 record may hold; anything else
// (APPROVED, GRANT_ISSUED, SETTLED, RESOURCE_DELIVERED, unknown) is not an
// M1 outcome and fails V5.
var m1Events = []string{"TASK_STARTED", "PURCHASE_PROPOSED", "POLICY_DECIDED", "APPROVAL_EXPIRED", "APPROVAL_WITHDRAWN", "TASK_CANCELLED", "TASK_TIMED_OUT", "CLI_EXITED"}

// task is one stored task as the checker reads it.
type task struct {
	row        storedTask
	reg        *contracts.Registry
	regCfg     contracts.RegistryConfig
	terms      terms
	profile    *policy.Profile
	credential *policy.Credential
	orders     map[string]*contracts.PurchaseOrder
	events     []contracts.AuditEvent
	usable     bool // registry, terms and events read: outcomes can be derived
}

// story is what a task's events say happened to its first purchase.
type story struct {
	purchases int
	order     *contracts.PurchaseOrder
	decision  *contracts.PolicyDecision
	decidedAt uint64
	closure   string // expired | withdrawn
	closedAt  uint64
	lifecycle *contracts.AuditEvent // TASK_CANCELLED | TASK_TIMED_OUT
}

func (v *verifier) storeRecord(f *findings, c contracts.AcceptanceCase, p string) Observed {
	var rec storeRecord
	if !v.readJSON(f, "V2", p, &rec) {
		return Observed{}
	}
	if rec.CaseID != c.CaseID || rec.Layer != "store_integration" || rec.Operation != c.Operation {
		f.fail("V6", []string{p}, "the record is %s %s %s, not this case's Store record", quote(rec.CaseID), quote(rec.Layer), quote(rec.Operation))
		return Observed{}
	}
	o, _ := v.integration(f, c, p, "store_integration", rec.Tasks)
	return o
}

func (v *verifier) e2eRecord(f *findings, c contracts.AcceptanceCase, p string) Observed {
	var rec e2eRecord
	if !v.readJSON(f, "V2", p, &rec) {
		return Observed{}
	}
	if rec.CaseID != c.CaseID || rec.Layer != "host_e2e" {
		f.fail("V6", []string{p}, "the record is %s %s, not this case's host E2E record", quote(rec.CaseID), quote(rec.Layer))
		return Observed{}
	}
	if !reflect.DeepEqual(rec.Expected, c.Expected) {
		f.fail("V3", []string{p}, "the record carries another expected outcome than the frozen case's")
	}
	o, ts := v.integration(f, c, p, "host_e2e", rec.Store)
	v.e2eSources(f, p, rec, ts)
	v.e2eExecution(f, p, rec, ts)
	return o
}

// integration checks one record's tasks against the case: the case's task
// first, the task after the restart second.
func (v *verifier) integration(f *findings, c contracts.AcceptanceCase, p, layer string, rows []storedTask) (Observed, []*task) {
	ev := []string{p}
	o := Observed{ForbiddenFound: []string{}}
	want := 1
	if c.Operation == "restart-and-new-task" {
		want = 2
	}
	if len(rows) != want {
		f.missing("V2", ev, "%d stored tasks, the case needs %d", len(rows), want)
		return o, nil
	}
	var ts []*task
	for _, row := range rows {
		t := v.readTask(f, ev, row)
		if !t.usable {
			return o, nil
		}
		ts = append(ts, t)
	}
	params := map[string]string{}
	maps.Copy(params, v.tr.Cases.Defaults)
	maps.Copy(params, c.Parameters)
	s := v.tell(f, ev, ts[0])
	o.Purchases = s.purchases
	o.PaymentStatus = "unavailable"
	if s.decision != nil {
		o.PaymentStatus = s.decision.PaymentStatus
	}

	// V3: the outcome, derived from the events the way the case list states
	// each operation.
	switch c.Operation {
	case "policy":
		if s.decision == nil {
			f.missing("V3", ev, "no decision recorded")
			break
		}
		o.Result, o.Reason, o.ApprovalStatus = s.decision.PolicyVerdict, s.decision.Reason, s.decision.ApprovalStatus
	case "advance-clock":
		if s.decision == nil {
			f.missing("V3", ev, "no decision recorded")
			break
		}
		o.Result, o.Reason, o.ApprovalStatus = s.outcome()
		advance, _ := strconv.ParseUint(params["advance_seconds"], 10, 64)
		deadline := ts[0].terms.ApprovalDeadlineSeconds
		switch {
		case s.closure == "expired" && (advance < deadline || s.closedAt != s.decidedAt+advance):
			f.fail("V3", ev, "expired at +%d s with a %d s deadline; the case advances the clock %d s", s.closedAt-s.decidedAt, deadline, advance)
		case s.closure == "" && advance >= deadline:
			f.fail("V3", ev, "still open after advancing %d s past a %d s deadline", advance, deadline)
		}
	case "cancel", "cli-timeout":
		lifecycle := map[string]string{"cancel": "TASK_CANCELLED", "cli-timeout": "TASK_TIMED_OUT"}[c.Operation]
		if s.lifecycle == nil || s.lifecycle.EventType != lifecycle {
			f.fail("V3", ev, "no %s recorded", lifecycle)
			break
		}
		o.Result, o.Reason, o.ApprovalStatus = ts[0].row.Status, s.lifecycle.Payload.Code, "not_requested"
		if s.decision != nil {
			_, _, o.ApprovalStatus = s.outcome()
		}
		ends := params["before"] == "no_purchase" || params["phase"] == "before_purchase"
		if ends && s.purchases != 0 {
			f.fail("V2", ev, "the case ends before a purchase; %d recorded", s.purchases)
		}
		if !ends && s.purchases == 0 {
			f.fail("V2", ev, "the case has a pending purchase; none recorded")
		}
	case "restart-and-new-task":
		a, b := ts[0].row, ts[1].row
		if a.TaskID == b.TaskID || a.ConsumerID != params["consumer"] || b.ConsumerID != params["consumer"] {
			f.fail("V7", ev, "not two tasks of %s", quote(params["consumer"]))
			break
		}
		o.Result = "other-wallet"
		if a.WalletID == b.WalletID && a.Payer == b.Payer {
			o.Result = "same-wallet"
		}
		o.Reason, o.ApprovalStatus = "PERSISTED_BINDING", "not_requested"
	default:
		f.missing("V3", ev, "operation %s is not one this checker knows", quote(c.Operation))
	}

	// V3 budget: what the terms gave each task less what is left.
	deducted := new(big.Int)
	for _, t := range ts {
		i, ok1 := new(big.Int).SetString(string(t.terms.BudgetInitial), 10)
		r, ok2 := new(big.Int).SetString(t.row.BudgetRemaining, 10)
		if !ok1 || !ok2 {
			f.fail("V3", ev, "task %s: budget not an amount", quote(t.row.TaskID))
			continue
		}
		deducted.Add(deducted, i.Sub(i, r))
		for _, e := range t.events {
			if slices.Contains(c.Expected.ForbiddenEvents, e.EventType) && !slices.Contains(o.ForbiddenFound, e.EventType) {
				o.ForbiddenFound = append(o.ForbiddenFound, e.EventType)
			}
		}
	}
	o.BudgetDeducted = deducted.String()

	x := c.Expected
	for _, cmp := range [][3]string{{"result", o.Result, x.Result}, {"reason", o.Reason, x.Reason}, {"approval_status", o.ApprovalStatus, x.ApprovalStatus},
		{"budget_deducted", o.BudgetDeducted, string(x.BudgetDeducted)}} {
		if cmp[1] != cmp[2] {
			f.fail("V3", ev, "%s %s, expected %s", cmp[0], quote(cmp[1]), quote(cmp[2]))
		}
	}
	if o.PaymentStatus != x.PaymentStatus || o.PaymentStatus != "unavailable" {
		f.fail("V5", ev, "payment status %s; M1 has no payment", quote(o.PaymentStatus))
	}
	if len(o.ForbiddenFound) > 0 {
		f.fail("V5", ev, "forbidden events %v", o.ForbiddenFound)
	}
	v.parameters(f, ev, layer, params, ts[0], s)
	return o, ts
}

// readTask recomputes one stored task from its record alone.
func (v *verifier) readTask(f *findings, ev []string, row storedTask) *task {
	t := &task{row: row, orders: map[string]*contracts.PurchaseOrder{}}
	id := quote(row.TaskID)
	for kind, s := range row.Snapshots {
		claim := "V3"
		if kind == "registry" {
			claim = "V7"
		}
		if !slices.Contains([]string{"registry", "terms", "profile", "credential"}, kind) {
			f.fail("V3", ev, "task %s: unknown snapshot %s", id, quote(kind))
		} else if sha([]byte(s.Body)) != s.Hash {
			f.fail(claim, ev, "task %s: the %s snapshot's digest is not its body's", id, kind)
		}
	}
	reg, ok := row.Snapshots["registry"]
	if !ok {
		f.missing("V7", ev, "task %s: no registry snapshot", id)
		return t
	}
	if err := contracts.Decode([]byte(reg.Body), &t.regCfg); err != nil {
		f.fail("V7", ev, "task %s: registry snapshot: %v", id, err)
		return t
	}
	var err error
	if t.reg, err = contracts.NewRegistry(t.regCfg); err != nil {
		f.fail("V7", ev, "task %s: registry snapshot: %v", id, err)
		return t
	}
	tr, ok := row.Snapshots["terms"]
	if !ok {
		f.missing("V3", ev, "task %s: no terms snapshot", id)
		return t
	}
	if err := contracts.Decode([]byte(tr.Body), &t.terms); err != nil || t.terms.TaskID != row.TaskID {
		f.fail("V3", ev, "task %s: the terms snapshot is not this task's (%v)", id, err)
		return t
	}
	// Settings: present means a snapshot, absent none (the Store's own rule).
	for kind, state := range map[string]string{"profile": t.terms.ProfileState, "credential": t.terms.CredentialState} {
		_, has := row.Snapshots[kind]
		if state != "present" && state != "absent" || (state == "present") != has {
			f.fail("V3", ev, "task %s: terms call the %s %s; snapshot there: %v", id, kind, quote(state), has)
		}
	}
	if s, ok := row.Snapshots["profile"]; ok {
		t.profile = new(policy.Profile)
		if err := contracts.Decode([]byte(s.Body), t.profile); err != nil {
			f.fail("V3", ev, "task %s: profile snapshot: %v", id, err)
			t.profile = nil
		} else if err := t.profile.Validate(); err != nil {
			f.fail("V3", ev, "task %s: profile snapshot: %v", id, err)
		} else if budget, cap := units(t.terms.BudgetInitial), units(t.profile.TaskCap); budget == nil || cap == nil || budget.Cmp(cap) > 0 || t.terms.AssetID != t.profile.AssetID {
			f.fail("V3", ev, "task %s: the terms' budget or asset is outside the profile", id)
		}
	}
	if s, ok := row.Snapshots["credential"]; ok {
		t.credential = new(policy.Credential)
		if err := contracts.Decode([]byte(s.Body), t.credential); err != nil {
			f.fail("V3", ev, "task %s: credential snapshot: %v", id, err)
			t.credential = nil
		} else if c := t.credential; c.ConsumerID != row.ConsumerID || c.EvidenceMode != "fixture" || c.ValidUntil == 0 {
			f.fail("V3", ev, "task %s: the credential is not this consumer's M1 fixture", id)
		}
	}
	if !slices.ContainsFunc(t.regCfg.Assets, func(a contracts.Asset) bool { return a.AssetID == t.terms.AssetID }) {
		f.fail("V3", ev, "task %s: the terms' asset %s is not in the registry snapshot", id, quote(t.terms.AssetID))
	}
	if units(t.terms.BudgetInitial) == nil {
		f.fail("V3", ev, "task %s: the terms' budget is not an amount", id)
	}

	// V7: the row, the registry snapshot and the trusted identity agree.
	b, w, err := t.reg.Binding(row.TaskID)
	switch who, known := v.tr.identity(row.ConsumerID); {
	case err != nil:
		f.fail("V7", ev, "task %s is not in its registry snapshot", id)
	case b.ConsumerID != row.ConsumerID || b.AgentID != row.AgentID || w.WalletID != row.WalletID || w.From != row.Payer:
		f.fail("V7", ev, "task %s: the stored binding is not the registry snapshot's", id)
	case !known:
		f.missing("V7", ev, "consumer %s is not in the trusted registry", quote(row.ConsumerID))
	case who != identity{row.ConsumerID, row.AgentID, row.WalletID, row.Payer}:
		f.fail("V7", ev, "task %s: %s's agent, wallet or payer is not the trusted one", id, row.ConsumerID)
	}

	// V2: each purchase as stored.
	sess := contracts.Session{ConsumerID: row.ConsumerID, AgentID: row.AgentID, TaskID: row.TaskID}
	for po, s := range row.Purchases {
		var order contracts.PurchaseOrder
		if err := contracts.Decode([]byte(s.Body), &order); err != nil {
			f.fail("V2", ev, "purchase %s: %v", quote(po), err)
			continue
		}
		i := order.Intent
		switch h, err := i.Hash(); {
		case i.PurchaseOrderID != po:
			f.fail("V2", ev, "purchase %s is stored under another ID", quote(po))
		case err != nil || h != s.Hash:
			f.fail("V2", ev, "purchase %s: the intent digest is not the stored one", quote(po))
		}
		if err := order.Validate(); err != nil {
			f.fail("V2", ev, "purchase %s: %v", quote(po), err)
		}
		if err := t.reg.ValidatePurchase(sess, order); err != nil {
			f.fail("V7", ev, "purchase %s: %v", quote(po), err)
		}
		if i.ConsumerID != row.ConsumerID || i.AgentID != row.AgentID || i.WalletID != row.WalletID || i.TaskID != row.TaskID || i.From != row.Payer {
			f.fail("V7", ev, "purchase %s names another consumer, agent, wallet, task or payer", quote(po))
		}
		t.orders[po] = &order
	}

	// V4: the events: each body's hash, the sequence, the chain from the
	// task's genesis, the scope, the purchase it names, the contract's rules.
	prev, _ := contracts.GenesisHash(row.TaskID)
	proposed := map[string]int{}
	for n, s := range row.Events {
		var e contracts.AuditEvent
		if err := contracts.Decode([]byte(s.Body), &e); err != nil {
			f.fail("V4", ev, "task %s event %d: %v", id, n+1, err)
			return t
		}
		h, err := contracts.EventHash(e)
		switch {
		case err != nil || h != s.Hash:
			f.fail("V4", ev, "task %s event %d: the stored hash is not its body's", id, n+1)
		case e.Sequence != uint64(n+1):
			f.fail("V4", ev, "task %s event %d has sequence %d", id, n+1, e.Sequence)
		case e.PrevEventHash != prev:
			f.fail("V4", ev, "task %s event %d does not link to the event before it", id, n+1)
		}
		prev = h
		if !slices.Contains(m1Events, e.EventType) {
			f.fail("V5", ev, "task %s event %d is %s, not an M1 event", id, n+1, quote(e.EventType))
			continue
		}
		if e.Scope == nil || e.Scope.TaskID != row.TaskID {
			f.fail("V4", ev, "task %s event %d is not this task's", id, n+1)
			continue
		}
		if sc := e.Scope; sc.ConsumerID != row.ConsumerID || sc.AgentID != row.AgentID || sc.WalletID != row.WalletID {
			f.fail("V7", ev, "task %s event %d is scoped to another consumer, agent or wallet", id, n+1)
		}
		var order *contracts.PurchaseOrder
		if po := e.Scope.PurchaseOrderID; po != "" {
			if order = t.orders[po]; order == nil {
				f.fail("V4", ev, "task %s event %d names purchase %s, which is not stored", id, n+1, quote(po))
				continue
			}
		}
		if e.EventType == "PURCHASE_PROPOSED" {
			proposed[e.Scope.PurchaseOrderID]++
		}
		if err := t.reg.ValidateEvent(e, order); err != nil {
			claim := "V4"
			if contracts.ErrorCode(err) == "OWNERSHIP_MISMATCH" {
				claim = "V7"
			}
			f.fail(claim, ev, "task %s event %d: %v", id, n+1, err)
		}
		if d := e.Payload.Decision; d != nil && d.PaymentStatus != "unavailable" {
			f.fail("V5", ev, "task %s event %d records payment status %s", id, n+1, quote(d.PaymentStatus))
		}
		t.events = append(t.events, e)
	}
	if len(row.Events) == 0 {
		f.missing("V4", ev, "task %s: no events", id)
		return t
	}
	if err := contracts.ValidateEventOrder(t.events); err != nil {
		f.fail("V4", ev, "task %s: %v", id, err)
	}
	for po := range row.Purchases {
		if proposed[po] != 1 {
			f.fail("V4", ev, "purchase %s is proposed %d times", quote(po), proposed[po])
		}
	}
	t.usable = true
	return t
}

func units(a contracts.Amount) *big.Int {
	n, ok := new(big.Int).SetString(string(a), 10)
	if !ok || n.Sign() < 0 {
		return nil
	}
	return n
}

// tell reads what happened to the task's first purchase, and checks each
// decision's reason against the recorded settings (necessaryConditions).
func (v *verifier) tell(f *findings, ev []string, t *task) story {
	var s story
	first := ""
	for i, e := range t.events {
		po := e.Scope.PurchaseOrderID
		switch e.EventType {
		case "PURCHASE_PROPOSED":
			s.purchases++
			if first == "" {
				first, s.order = po, t.orders[po]
			}
		case "POLICY_DECIDED":
			v.necessaryConditions(f, ev, t, t.orders[po], e.Payload.Decision, e.ObservedAt)
			if po == first {
				s.decision, s.decidedAt = e.Payload.Decision, e.ObservedAt
			}
		case "APPROVAL_EXPIRED", "APPROVAL_WITHDRAWN":
			if po == first {
				s.closure, s.closedAt = e.Payload.Decision.ApprovalStatus, e.ObservedAt
			}
		case "TASK_CANCELLED", "TASK_TIMED_OUT":
			s.lifecycle = &t.events[i]
		}
	}
	return s
}

// outcome: an expired Ask reads denied, a withdrawn one cancelled, an open
// one pending (cases.json's wording for these operations).
func (s story) outcome() (string, string, string) {
	switch s.closure {
	case "expired":
		return "denied", "APPROVAL_TIMEOUT", "expired"
	case "withdrawn":
		return "cancelled", "HOST_CANCELLED", "withdrawn"
	}
	d := s.decision
	if d.PolicyVerdict == "Ask" {
		return "pending", d.Reason, d.ApprovalStatus
	}
	return d.PolicyVerdict, d.Reason, d.ApprovalStatus
}

// necessaryConditions checks that a decision's reason is true of what the
// task recorded at the decision's time. It is not the Policy engine: it
// asks only what each reason requires (a Deny reason's own condition holds;
// Allow and Ask need every Deny condition to be clear, and the allow limit
// and the approver to decide which), so that a record cannot report a
// legal Ask without the configuration an Ask needs.
func (v *verifier) necessaryConditions(f *findings, ev []string, t *task, order *contracts.PurchaseOrder, d *contracts.PolicyDecision, now uint64) {
	if order == nil || d == nil {
		return
	}
	i := order.Intent
	amount := units(i.Amount)
	config := t.profile != nil && t.credential != nil
	var assetOK, merchantOK, inTime, withinAllow, attended bool
	if config {
		a, trusted := t.reg.TrustedAsset(i.ChainID, i.AssetContract)
		assetOK = trusted && a.AssetID == t.profile.AssetID
		merchantOK = slices.Contains(t.profile.Merchants, i.PayTo)
		inTime = now < t.credential.ValidUntil && i.ValidAfter < now && now < i.ValidBefore
		allow := units(t.profile.AllowMax)
		withinAllow = amount != nil && allow != nil && amount.Cmp(allow) <= 0
		attended = t.profile.Mode == "interactive"
	}
	budget := units(t.terms.BudgetInitial)
	budgetOK := amount != nil && budget != nil && amount.Cmp(budget) <= 0
	clear := config && assetOK && merchantOK && inTime && budgetOK
	holds := map[string]bool{
		"CONFIG_MISSING":                 !config,
		"ASSET_NOT_ALLOWED":              config && !assetOK,
		"MERCHANT_NOT_ALLOWED":           config && !merchantOK,
		"AUTHORIZATION_EXPIRED":          config && !inTime,
		"INSUFFICIENT_BUDGET":            !budgetOK,
		"PAYMENT_CAPABILITY_UNAVAILABLE": clear && withinAllow,
		"APPROVER_UNAVAILABLE":           clear && !withinAllow && !attended,
		"HUMAN_APPROVAL_REQUIRED":        clear && !withinAllow && attended,
	}
	h, known := holds[d.Reason]
	switch {
	case !known:
		f.fail("V3", ev, "decision reason %s is not one this checker knows", quote(d.Reason))
	case !h:
		f.fail("V3", ev, "the decision says %s, but the recorded settings do not support it (configuration %v, asset %v, merchant %v, in time %v, budget %v, within the allow limit %v, attended %v)",
			d.Reason, config, assetOK, merchantOK, inTime, budgetOK, withinAllow, attended)
	}
}

// parameters checks that the case's parameters were the trusted settings
// the task ran with (V2): the case's own inputs, not just its ID.
func (v *verifier) parameters(f *findings, ev []string, layer string, params map[string]string, t *task, s story) {
	bad := func(format string, a ...any) { f.fail("V2", ev, "parameters: "+format, a...) }
	if r, ok := params["remaining"]; ok && string(t.terms.BudgetInitial) != r {
		bad("the task's budget is %s, the case's %s", t.terms.BudgetInitial, r)
	}
	if p := t.profile; p != nil {
		if a, ok := params["allow_max"]; ok && string(p.AllowMax) != a {
			bad("the allow limit is %s, the case's %s", p.AllowMax, a)
		}
		if m, ok := params["mode"]; ok && p.Mode != m {
			bad("the approver mode is %s, the case's %s", p.Mode, m)
		}
	}
	both := t.profile != nil && t.credential != nil
	switch params["required_config"] {
	case "present":
		if !both {
			bad("the case has its configuration; the task lacks a profile or credential")
		}
	case "missing":
		if both {
			bad("the case lacks configuration; the task has a profile and a credential")
		}
	}
	if d, ok := params["approval_deadline_seconds"]; ok && strconv.FormatUint(t.terms.ApprovalDeadlineSeconds, 10) != d {
		bad("the approval deadline is %d s, the case's %s", t.terms.ApprovalDeadlineSeconds, d)
	}
	if layer == "store_integration" && len(t.events) > 0 {
		if c, ok := params["clock"]; ok && strconv.FormatUint(t.events[0].ObservedAt, 10) != c {
			bad("the Store clock started at %d, the case's clock is %s", t.events[0].ObservedAt, c)
		}
	}
	if s.order == nil {
		return // the rest are about the purchase
	}
	i := s.order.Intent
	if a, ok := params["amount"]; ok && string(i.Amount) != a {
		bad("the amount is %s, the case's %s", i.Amount, a)
	}
	if p := t.profile; p != nil {
		switch params["merchant"] {
		case "allowed":
			if !slices.Contains(p.Merchants, i.PayTo) {
				bad("the case's merchant is allowed; %s is not in the profile", i.PayTo)
			}
		case "not_allowed":
			if slices.Contains(p.Merchants, i.PayTo) {
				bad("the case's merchant is not allowed; %s is in the profile", i.PayTo)
			}
		}
	}
	a, trusted := t.reg.TrustedAsset(i.ChainID, i.AssetContract)
	switch asset := params["asset"]; {
	case asset == "unlisted":
		if trusted && t.profile != nil && a.AssetID == t.profile.AssetID {
			bad("the case's asset is unlisted; %s is the profile's trusted asset", a.AssetID)
		}
	case asset != "":
		if !trusted || a.AssetID != asset || t.terms.AssetID != asset {
			bad("the case's asset is %s; the purchase's is %s (trusted %v), the task's %s", asset, a.AssetID, trusted, t.terms.AssetID)
		}
	}
	if c := t.credential; c != nil && s.decision != nil {
		switch params["credential"] {
		case "valid":
			if s.decidedAt >= c.ValidUntil {
				bad("the case's credential is valid; it ended at %d, before the decision at %d", c.ValidUntil, s.decidedAt)
			}
		case "expired":
			if s.decidedAt < c.ValidUntil {
				bad("the case's credential is expired; it is valid until %d, after the decision at %d", c.ValidUntil, s.decidedAt)
			}
		}
	}
}

// e2eSources compares the record's two views of each task: the API's
// replies (task and events) against the stored records (V4), the API's
// purchases (V2, V5), and the host's trusted settings against the
// snapshots the Store took (V2).
func (v *verifier) e2eSources(f *findings, p string, rec e2eRecord, ts []*task) {
	ev := []string{p}
	type apiEvent struct {
		EventID       string `json:"event_id"`
		EventType     string `json:"event_type"`
		Sequence      uint64 `json:"sequence"`
		PrevEventHash string `json:"prev_event_hash"`
		ObservedAt    uint64 `json:"observed_at"`
	}
	type apiTask struct {
		TaskID       string        `json:"task_id"`
		ConsumerID   string        `json:"consumer_id"`
		AgentID      string        `json:"agent_id"`
		WalletID     string        `json:"wallet_id"`
		Payer        string        `json:"payer"`
		Status       string        `json:"status"`
		AsOfSequence uint64        `json:"as_of_sequence"`
		Purchases    []apiPurchase `json:"purchases"`
	}
	read := map[string]*task{}
	for _, t := range ts {
		read[t.row.TaskID] = t
	}
	byTask := map[string]int{}
	for i, raw := range rec.Tasks {
		var api apiTask
		if err := json.Unmarshal(raw, &api); err != nil {
			f.fail("V4", ev, "the API task reply %d is unreadable: %v", i+1, err)
			continue
		}
		byTask[api.TaskID] = i
		var row *storedTask
		for k := range rec.Store {
			if rec.Store[k].TaskID == api.TaskID {
				row = &rec.Store[k]
			}
		}
		if row == nil {
			f.fail("V4", ev, "the API replied for task %s, which is not stored", quote(api.TaskID))
			continue
		}
		if api.Status != row.Status || api.AsOfSequence != uint64(len(row.Events)) {
			f.fail("V4", ev, "task %s: the API says %s as of event %d; the Store has %s with %d events", quote(row.TaskID), quote(api.Status), api.AsOfSequence, quote(row.Status), len(row.Events))
		}
		if api.ConsumerID != row.ConsumerID || api.AgentID != row.AgentID || api.WalletID != row.WalletID || api.Payer != row.Payer {
			f.fail("V7", ev, "task %s: the API names another consumer, agent, wallet or payer than the Store", quote(row.TaskID))
		}
		for _, pu := range api.Purchases {
			if pu.Decision.PaymentStatus != "unavailable" {
				f.fail("V5", ev, "task %s: the API shows payment status %s", quote(row.TaskID), quote(pu.Decision.PaymentStatus))
			}
			if t := read[row.TaskID]; t != nil {
				t.compareAPI(f, ev, pu)
			} else if s, ok := row.Purchases[pu.PurchaseOrderID]; !ok || s.Hash != pu.IntentHash {
				f.fail("V2", ev, "task %s: the API's purchase %s is not the stored one", quote(row.TaskID), quote(pu.PurchaseOrderID))
			}
		}
		if len(api.Purchases) != len(row.Purchases) {
			f.fail("V4", ev, "task %s: the API shows %d purchases, the Store %d", quote(row.TaskID), len(api.Purchases), len(row.Purchases))
		}
	}
	for _, row := range rec.Store {
		if _, ok := byTask[row.TaskID]; !ok {
			f.missing("V4", ev, "no API reply for task %s", quote(row.TaskID))
		}
		raw, ok := rec.Events[row.TaskID]
		var evs struct {
			AsOfSequence uint64     `json:"as_of_sequence"`
			Events       []apiEvent `json:"events"`
		}
		if !ok || json.Unmarshal(raw, &evs) != nil {
			f.missing("V4", ev, "no readable API event reply for task %s", quote(row.TaskID))
			continue
		}
		same := len(evs.Events) == len(row.Events) && evs.AsOfSequence == uint64(len(row.Events))
		for i := 0; same && i < len(evs.Events); i++ {
			var e contracts.AuditEvent
			if contracts.Decode([]byte(row.Events[i].Body), &e) != nil {
				break
			}
			a := evs.Events[i]
			same = a.EventID == e.EventID && a.EventType == e.EventType && a.Sequence == e.Sequence && a.PrevEventHash == e.PrevEventHash && a.ObservedAt == e.ObservedAt
		}
		if !same {
			f.fail("V4", ev, "task %s: the API's events are not the stored events", quote(row.TaskID))
		}
	}
	// The host's settings files and the Store's snapshots of them.
	for _, kind := range []string{"registry", "profile"} {
		raw, ok := rec.HostSettings[kind]
		if !ok {
			f.missing("V2", ev, "the host's %s is not recorded", kind)
			continue
		}
		for _, row := range rec.Store {
			s, ok := row.Snapshots[kind]
			if !ok {
				continue // absent settings are judged with the terms
			}
			var host, snap any
			if kind == "registry" {
				host, snap = new(contracts.RegistryConfig), new(contracts.RegistryConfig)
			} else {
				host, snap = new(policy.Profile), new(policy.Profile)
			}
			if err := contracts.Decode(raw, host); err != nil {
				f.fail("V2", ev, "the host's %s: %v", kind, err)
				break
			}
			if contracts.Decode([]byte(s.Body), snap) != nil || !reflect.DeepEqual(host, snap) {
				f.fail("V2", ev, "task %s: the Store's %s snapshot is not the host's %s", quote(row.TaskID), kind, kind)
			}
		}
	}
	if rec.Verdict != "PASS" {
		f.fail("V3", ev, "the E2E test itself recorded %s", quote(rec.Verdict))
	}
}

// apiPurchase is a purchase as GET /api/tasks/{id} shows it.
type apiPurchase struct {
	PurchaseOrderID        string                   `json:"purchase_order_id"`
	RequestID              string                   `json:"request_id"`
	HTTPMethod             string                   `json:"http_method"`
	ResourceURL            string                   `json:"resource_url"`
	ChainID                string                   `json:"chain_id"`
	AssetContract          string                   `json:"asset_contract"`
	AssetID                string                   `json:"asset_id"`
	PayTo                  string                   `json:"pay_to"`
	Amount                 string                   `json:"amount"`
	IntentHash             string                   `json:"intent_hash"`
	PaymentRequirementHash string                   `json:"payment_requirement_hash"`
	Decision               contracts.PolicyDecision `json:"decision"`
	DecidedAt              uint64                   `json:"decided_at"`
	DeadlineAt             uint64                   `json:"deadline_at"`
	ApprovalClosed         string                   `json:"approval_closed"`
	ClosedAt               uint64                   `json:"closed_at"`
}

// purchaseEvents is what the stored events say of one purchase.
type purchaseEvents struct {
	decision  *contracts.PolicyDecision
	decidedAt uint64
	closure   string
	closedAt  uint64
	sequences []uint64
}

func (t *task) purchaseEvents() map[string]*purchaseEvents {
	out := map[string]*purchaseEvents{}
	for _, e := range t.events {
		po := e.Scope.PurchaseOrderID
		if po == "" {
			continue
		}
		pe := out[po]
		if pe == nil {
			pe = &purchaseEvents{}
			out[po] = pe
		}
		pe.sequences = append(pe.sequences, e.Sequence)
		switch e.EventType {
		case "POLICY_DECIDED":
			pe.decision, pe.decidedAt = e.Payload.Decision, e.ObservedAt
		case "APPROVAL_EXPIRED", "APPROVAL_WITHDRAWN":
			pe.closure, pe.closedAt = e.Payload.Decision.ApprovalStatus, e.ObservedAt
		}
	}
	return out
}

// lifecycle is the task's TASK_CANCELLED or TASK_TIMED_OUT, if any.
func (t *task) lifecycle() *contracts.AuditEvent {
	for i, e := range t.events {
		if e.EventType == "TASK_CANCELLED" || e.EventType == "TASK_TIMED_OUT" {
			return &t.events[i]
		}
	}
	return nil
}

// runOutcomes are the ways the host and the trace may say a task's run
// ended, given the stored events: a CLI timeout is recorded only by the run
// itself (timeout); without a lifecycle event the run ended ok; a host
// cancel either stopped the run (cancelled) or came after it ended (ok).
func (t *task) runOutcomes() []string {
	switch e := t.lifecycle(); {
	case e == nil:
		return []string{"ok"}
	case e.EventType == "TASK_TIMED_OUT":
		return []string{"timeout"}
	}
	return []string{"cancelled", "ok"}
}

// compareAPI: the API's view of a purchase is the stored order, its
// original decision and its closure, field by field. The API was read after
// the run ended (its as_of_sequence is the last stored event, checked
// above), so the closure the events record is the one it must show.
func (t *task) compareAPI(f *findings, ev []string, pu apiPurchase) {
	id, po := quote(t.row.TaskID), quote(pu.PurchaseOrderID)
	order, s := t.orders[pu.PurchaseOrderID], t.row.Purchases[pu.PurchaseOrderID]
	pe := t.purchaseEvents()[pu.PurchaseOrderID]
	if order == nil || pe == nil || pe.decision == nil {
		f.fail("V2", ev, "task %s: the API shows purchase %s, which the Store did not decide", id, po)
		return
	}
	i := order.Intent
	assetID := ""
	if a, ok := t.reg.TrustedAsset(i.ChainID, i.AssetContract); ok {
		assetID = a.AssetID
	}
	for _, c := range [][3]string{{"request_id", pu.RequestID, order.RequestID}, {"http_method", pu.HTTPMethod, i.HTTPMethod},
		{"resource_url", pu.ResourceURL, i.ResourceURL}, {"chain_id", pu.ChainID, i.ChainID}, {"asset_contract", pu.AssetContract, i.AssetContract},
		{"asset_id", pu.AssetID, assetID}, {"pay_to", pu.PayTo, i.PayTo}, {"amount", pu.Amount, string(i.Amount)},
		{"intent_hash", pu.IntentHash, s.Hash}, {"payment_requirement_hash", pu.PaymentRequirementHash, i.PaymentRequirementHash}} {
		if c[1] != c[2] {
			f.fail("V2", ev, "task %s purchase %s: the API's %s is %s, the stored %s", id, po, c[0], quote(c[1]), quote(c[2]))
		}
	}
	deadline := uint64(0)
	if pe.decision.PolicyVerdict == "Ask" {
		deadline = pe.decidedAt + t.terms.ApprovalDeadlineSeconds
	}
	switch {
	case pu.Decision != *pe.decision:
		f.fail("V3", ev, "task %s purchase %s: the API's decision %s/%s/%s is not the recorded %s/%s/%s", id, po,
			pu.Decision.PolicyVerdict, pu.Decision.Reason, pu.Decision.ApprovalStatus, pe.decision.PolicyVerdict, pe.decision.Reason, pe.decision.ApprovalStatus)
	case pu.DecidedAt != pe.decidedAt || pu.DeadlineAt != deadline:
		f.fail("V3", ev, "task %s purchase %s: the API's decision time or deadline is not the recorded one", id, po)
	case pu.ApprovalClosed != pe.closure || pu.ClosedAt != pe.closedAt:
		f.fail("V3", ev, "task %s purchase %s: the API's approval closure %s at %d is not the recorded %s at %d", id, po,
			quote(pu.ApprovalClosed), pu.ClosedAt, quote(pe.closure), pe.closedAt)
	}
}

// e2eExecution checks the run's own records against the Store: the host's
// run for each task, and the run trace (the history allow-list) with its
// tool calls, the purchases they made and the events those led to. Both
// records are required for an E2E case; a missing one is INCONCLUSIVE, a
// contradiction FAIL. A run that ended before a purchase, by a cancel or a
// timeout, is checked as what it is: no request_purchase is required.
func (v *verifier) e2eExecution(f *findings, p string, rec e2eRecord, ts []*task) {
	ev := []string{p}
	absent := func(raw json.RawMessage) bool { return len(raw) == 0 || string(raw) == "null" }
	var runs struct {
		Runs []struct {
			TaskID      string `json:"task_id"`
			ConsumerID  string `json:"consumer_id"`
			State       string `json:"state"`
			Outcome     string `json:"outcome"`
			TaskStarted bool   `json:"task_started"`
			EndedAt     uint64 `json:"ended_at"`
		} `json:"runs"`
	}
	runsOK := false
	switch {
	case absent(rec.HostRuns):
		f.missing("V4", ev, "the host's run record (host_runs) is missing")
	case json.Unmarshal(rec.HostRuns, &runs) != nil:
		f.missing("V4", ev, "the host's run record is not readable")
	default:
		runsOK = true
	}
	var traces map[string][]json.RawMessage
	tracesOK := false
	switch {
	case absent(rec.Traces):
		f.missing("V4", ev, "the run traces (traces) are missing")
	case json.Unmarshal(rec.Traces, &traces) != nil:
		f.missing("V4", ev, "the run traces are not readable")
	default:
		tracesOK = true
	}
	stored := map[string]bool{}
	for _, t := range ts {
		stored[t.row.TaskID] = true
	}
	if runsOK {
		for _, r := range runs.Runs {
			if !stored[r.TaskID] {
				f.fail("V4", ev, "a host run for task %s, which the record does not store", quote(r.TaskID))
			}
		}
	}
	for _, t := range ts {
		id := quote(t.row.TaskID)
		want := t.runOutcomes()
		runOutcome := ""
		if runsOK {
			n := 0
			for _, r := range runs.Runs {
				if r.TaskID != t.row.TaskID {
					continue
				}
				n++
				switch {
				case r.ConsumerID != t.row.ConsumerID:
					f.fail("V7", ev, "task %s: its host run is %s's", id, quote(r.ConsumerID))
				case r.State != "ended" || !r.TaskStarted:
					f.fail("V4", ev, "task %s: its host run is %s (task started: %v)", id, quote(r.State), r.TaskStarted)
				case !slices.Contains(want, r.Outcome):
					f.fail("V4", ev, "task %s: the host run ended %s; the stored events allow %v", id, quote(r.Outcome), want)
				}
				runOutcome = r.Outcome
				// A cancel that stopped the run came before its end; one after
				// an ok run came after it (times are whole seconds).
				if e := t.lifecycle(); e != nil && e.EventType == "TASK_CANCELLED" {
					if r.Outcome == "cancelled" && r.EndedAt < e.ObservedAt || r.Outcome == "ok" && e.ObservedAt < r.EndedAt {
						f.fail("V4", ev, "task %s: the run ended %s at %d, the cancel is at %d", id, quote(r.Outcome), r.EndedAt, e.ObservedAt)
					}
				}
			}
			if n != 1 {
				f.fail("V4", ev, "task %s has %d host runs", id, n)
			}
		}
		if !tracesOK {
			continue
		}
		list := traces[t.row.TaskID]
		if len(list) == 0 {
			f.missing("V4", ev, "task %s has no run trace", id)
			continue
		}
		if len(list) > 1 {
			f.fail("V4", ev, "task %s has %d run traces", id, len(list))
			continue
		}
		var tr struct {
			TaskID       string `json:"task_id"`
			GateMode     string `json:"gate_mode"`
			Gate         string `json:"gate"`
			Outcome      string `json:"outcome"`
			NotAvailable string `json:"not_available"`
			ToolCalls    []struct {
				N               int      `json:"n"`
				Tool            string   `json:"tool"`
				Outcome         string   `json:"outcome"`
				PurchaseOrderID string   `json:"purchase_order_id"`
				EventSequences  []uint64 `json:"event_sequences"`
			} `json:"tool_calls"`
		}
		if json.Unmarshal(list[0], &tr) != nil {
			f.missing("V4", ev, "task %s: its run trace is not readable", id)
			continue
		}
		if tr.NotAvailable != "" {
			f.missing("V4", ev, "task %s: its run trace is not available (%s)", id, quote(tr.NotAvailable))
			continue
		}
		switch {
		case tr.TaskID != t.row.TaskID:
			f.fail("V4", ev, "task %s: its run trace is task %s's", id, quote(tr.TaskID))
			continue
		case tr.GateMode != "strict" || tr.Gate != "passed":
			f.fail("V6", ev, "task %s: a fixture run's trace says gate %s %s", id, quote(tr.GateMode), quote(tr.Gate))
		case !slices.Contains(want, tr.Outcome) || runOutcome != "" && tr.Outcome != runOutcome:
			f.fail("V4", ev, "task %s: the trace ended %s; the host run %s, the stored events allow %v", id, quote(tr.Outcome), quote(runOutcome), want)
		}
		pes := t.purchaseEvents()
		called := map[string]int{}
		for k, c := range tr.ToolCalls {
			if c.N != k+1 {
				f.fail("V4", ev, "task %s: tool call %d is numbered %d", id, k+1, c.N)
			}
			if c.Tool != "request_purchase" {
				continue
			}
			pe := pes[c.PurchaseOrderID]
			switch {
			case c.PurchaseOrderID == "":
				if slices.Contains([]string{"Ask", "Deny", "Allow"}, c.Outcome) {
					f.fail("V4", ev, "task %s: request_purchase %d reports %s without a purchase", id, c.N, quote(c.Outcome))
				}
				continue
			case pe == nil || pe.decision == nil:
				f.fail("V4", ev, "task %s: request_purchase %d names purchase %s, which the Store did not decide", id, c.N, quote(c.PurchaseOrderID))
				continue
			}
			called[c.PurchaseOrderID]++
			if c.Outcome != pe.decision.PolicyVerdict {
				f.fail("V3", ev, "task %s: request_purchase %d reports %s; the Store decided %s", id, c.N, quote(c.Outcome), pe.decision.PolicyVerdict)
			}
			if !slices.Equal(c.EventSequences, pe.sequences) {
				f.fail("V4", ev, "task %s: request_purchase %d names events %v; the purchase's stored events are %v", id, c.N, c.EventSequences, pe.sequences)
			}
		}
		for po := range t.orders {
			if called[po] != 1 {
				f.fail("V4", ev, "task %s: purchase %s is made by %d request_purchase calls in the trace", id, quote(po), called[po])
			}
		}
	}
}
