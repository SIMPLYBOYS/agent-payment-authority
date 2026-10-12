package paycontract

import (
	"errors"
	"fmt"
	"slices"

	"github.com/SIMPLYBOYS/agent-payment-authority/internal/contracts"
)

// An apa.pay.v1 task's events use the shared envelope (contracts.AuditEvent)
// with this version, this domain's chain and these rules. The M1 rules
// (contracts.ValidateEvent) refuse every one of them.

// EventTypes: M1's task events plus the delegation, approval session,
// approval closures and the grant. No SETTLED or RESOURCE_DELIVERED: P1-a
// cannot pay or deliver.
var EventTypes = []string{"TASK_STARTED", "DELEGATION_BOUND", "PURCHASE_PROPOSED", "POLICY_DECIDED", "APPROVAL_SESSION_OPENED",
	"APPROVAL_EXPIRED", "APPROVAL_WITHDRAWN", "APPROVED", "APPROVAL_DENIED", "APPROVAL_SUPERSEDED", "GRANT_ISSUED",
	"TASK_CANCELLED", "TASK_TIMED_OUT", "CLI_EXITED"}

// Closures are the events that end an approval session, and the approval
// state each records.
var Closures = map[string]string{"APPROVAL_EXPIRED": "expired", "APPROVAL_WITHDRAWN": "withdrawn", "APPROVED": "approved",
	"APPROVAL_DENIED": "denied", "APPROVAL_SUPERSEDED": "superseded"}

var lifecycleCodes = map[string]string{"TASK_CANCELLED": "HOST_CANCELLED", "TASK_TIMED_OUT": "CLI_TIMEOUT", "CLI_EXITED": ""}

// EventHash and GenesisHash are contracts.EventHash and GenesisHash under
// this version's domain: an M1 chain and a v1 chain never link.
func EventHash(e contracts.AuditEvent) (string, error) { return digest("event", e) }

func GenesisHash(taskID string) (string, error) {
	return digest("event-genesis", map[string]string{"task_id": taskID})
}

// ValidateEvent checks one event of an apa.pay.v1 task: shape, the task's
// registry binding, the purchase it names (whose v1 intent_hash it must
// carry) and the payload each type allows.
func ValidateEvent(reg *contracts.Registry, e contracts.AuditEvent, order *PurchaseOrder) error {
	if e.Version != Version || !ids(e.EventID, e.Source) || !timestamp(e.ObservedAt) || e.Sequence == 0 || e.Sequence > contracts.MaxSafeInteger {
		return errf("invalid event metadata")
	}
	if !slices.Contains([]string{"fixture", "live", "replay"}, e.EvidenceMode) || e.Scope == nil || !digestPattern.MatchString(e.PrevEventHash) {
		return errf("task event needs evidence mode, scope and prev_event_hash")
	}
	p := e.Payload
	for _, v := range []string{p.Code, p.DelegationID, p.ApprovalSessionID, p.GrantID} {
		if v != "" && !ids(v) {
			return errf("invalid payload ID")
		}
	}
	s := e.Scope
	t, _, err := reg.Binding(s.TaskID)
	if err != nil {
		return err
	}
	if s.ConsumerID != t.ConsumerID || s.AgentID != t.AgentID || s.WalletID != t.WalletID {
		return coded("OWNERSHIP_MISMATCH", "event scope does not match the task binding")
	}
	if s.PurchaseOrderID == "" {
		if s.IntentHash != "" || order != nil {
			return errf("intent without purchase")
		}
	} else {
		if order == nil {
			return errf("purchase snapshot required")
		}
		if err := ValidatePurchase(reg, contracts.Session{ConsumerID: t.ConsumerID, AgentID: t.AgentID, TaskID: t.TaskID}, *order); err != nil {
			return err
		}
		h, err := order.IntentHash()
		if err != nil || s.IntentHash != h || s.PurchaseOrderID != order.Intent.PurchaseOrderID {
			return errf("event intent mismatch")
		}
	}
	purchase := s.PurchaseOrderID != ""
	d := p.Decision
	only := func(want ...string) error { // the payload fields this type carries, all present; no other
		have := map[string]bool{"code": p.Code != "", "decision": d != nil, "delegation": p.DelegationID != "", "session": p.ApprovalSessionID != "",
			"grant": p.GrantID != "", "method": p.AuthorizationMethod != ""}
		for k, v := range have {
			if v != slices.Contains(want, k) {
				return errf("%s: payload field %s must %s", e.EventType, k, map[bool]string{true: "be absent", false: "be present"}[v])
			}
		}
		return nil
	}
	switch e.EventType {
	case "TASK_STARTED":
		if purchase {
			return errf("task start cannot imply purchase")
		}
		return only()
	case "DELEGATION_BOUND":
		if purchase {
			return errf("a delegation is bound to the task, not a purchase")
		}
		return only("delegation")
	case "PURCHASE_PROPOSED":
		if !purchase {
			return errf("invalid purchase event")
		}
		return only()
	case "POLICY_DECIDED":
		if !purchase {
			return errf("a decision needs a purchase")
		}
		if err := only("decision"); err != nil {
			return err
		}
		return decisionOf(e, "pending", "not_requested")
	case "APPROVAL_SESSION_OPENED":
		if !purchase {
			return errf("a session needs a purchase")
		}
		return only("session")
	case "APPROVAL_EXPIRED", "APPROVAL_WITHDRAWN", "APPROVAL_SUPERSEDED": // closed by the deadline, a cancel or a quote: nobody decided
		if !purchase {
			return errf("a closure needs a purchase")
		}
		if err := only("decision", "session"); err != nil {
			return err
		}
		return decisionOf(e, Closures[e.EventType])
	case "APPROVED", "APPROVAL_DENIED": // decided by an approver, whose method is recorded
		if !purchase || p.AuthorizationMethod != "simulated" {
			return errf("%s needs a purchase; P1-a decides by simulation only", e.EventType)
		}
		if err := only("decision", "session", "method"); err != nil {
			return err
		}
		return decisionOf(e, Closures[e.EventType])
	case "GRANT_ISSUED":
		if !purchase || p.AuthorizationMethod != "simulated" {
			return errf("GRANT_ISSUED needs a purchase; P1-a grants are simulated only")
		}
		if p.GrantID != GrantID(p.ApprovalSessionID) {
			return errf("grant ID is not the session's")
		}
		return only("session", "grant", "method")
	case "TASK_CANCELLED", "TASK_TIMED_OUT", "CLI_EXITED":
		want := lifecycleCodes[e.EventType]
		if p.Code == "" || (want != "" && p.Code != want) {
			return errf("%s needs code %s", e.EventType, want)
		}
		return only("code")
	}
	return errf("unsupported apa.pay.v1 event %s; cannot settle or deliver", e.EventType)
}

func decisionOf(e contracts.AuditEvent, approvals ...string) error {
	d := e.Payload.Decision
	if err := ValidateDecision(*d); err != nil {
		return err
	}
	if d.IntentHash != e.Scope.IntentHash {
		return errf("decision intent mismatch")
	}
	if !slices.Contains(approvals, d.ApprovalStatus) {
		return coded("INVALID_DECISION", e.EventType+" cannot record approval_status "+d.ApprovalStatus)
	}
	return nil
}

// chain is one task's position in its stream; see contracts' chainState.
type chain struct {
	next                    uint64
	prev                    string
	bound                   bool
	stopped, lifecycleEnded bool
	proposed                map[string]bool
	verdict, intent         map[string]string
	session, closed         map[string]string // po -> session ID; po -> closure event
	granted                 map[string]bool
}

// ValidateEventOrder checks complete per-task streams of this version:
// unique IDs, contiguous sequences, the chain, and the transitions:
// TASK_STARTED then DELEGATION_BOUND first; per purchase one proposal, one
// decision, for an Ask one session, then at most one closure, and a grant
// only after APPROVED. Like contracts.ValidateEventOrder it is pure and
// cannot see a truncated tail.
func ValidateEventOrder(events []contracts.AuditEvent) error {
	seen, tasks := map[string]bool{}, map[string]*chain{}
	for _, e := range events {
		if seen[e.EventID] {
			return coded("INVALID_EVENT_ORDER", "duplicate event ID "+e.EventID)
		}
		seen[e.EventID] = true
		if e.Scope == nil {
			return coded("INVALID_EVENT_ORDER", "an apa.pay.v1 event always has a task")
		}
		task := e.Scope.TaskID
		st := tasks[task]
		if st == nil {
			g, err := GenesisHash(task)
			if err != nil {
				return err
			}
			st = &chain{prev: g, proposed: map[string]bool{}, verdict: map[string]string{}, intent: map[string]string{},
				session: map[string]string{}, closed: map[string]string{}, granted: map[string]bool{}}
			tasks[task] = st
		}
		if e.Sequence != st.next+1 {
			return coded("INVALID_EVENT_ORDER", fmt.Sprintf("task %s: sequence %d after %d", task, e.Sequence, st.next))
		}
		if e.PrevEventHash != st.prev {
			return coded("EVENT_CHAIN_BROKEN", fmt.Sprintf("task %s: event %s does not link to the previous event", task, e.EventID))
		}
		if err := st.apply(e); err != nil {
			code, detail := "INVALID_EVENT_ORDER", err.Error()
			var c *Error
			if errors.As(err, &c) {
				code, detail = c.Code, c.Detail
			}
			return coded(code, fmt.Sprintf("task %s: event %s: %s", task, e.EventID, detail))
		}
		h, err := EventHash(e)
		if err != nil {
			return err
		}
		st.next, st.prev = e.Sequence, h
	}
	return nil
}

func (st *chain) apply(e contracts.AuditEvent) error {
	po := e.Scope.PurchaseOrderID
	switch {
	case (st.next == 0) != (e.EventType == "TASK_STARTED"):
		return errors.New("TASK_STARTED must be the first event and appear once")
	case (st.next == 1) != (e.EventType == "DELEGATION_BOUND"):
		return errors.New("DELEGATION_BOUND must follow TASK_STARTED and appear once")
	}
	if pinned := st.intent[po]; pinned != "" && e.Scope.IntentHash != pinned {
		return coded("PURCHASE_INTENT_CHANGED", fmt.Sprintf("purchase %s was proposed with %s, not %s", po, pinned, e.Scope.IntentHash))
	}
	switch e.EventType {
	case "PURCHASE_PROPOSED":
		if st.stopped || st.proposed[po] {
			return errors.New("one proposal per purchase, none after the task ended")
		}
		st.proposed[po], st.intent[po] = true, e.Scope.IntentHash
	case "POLICY_DECIDED":
		if st.stopped || !st.proposed[po] || st.verdict[po] != "" {
			return errors.New("one decision per proposed purchase, none after the task ended")
		}
		st.verdict[po] = e.Payload.Decision.PolicyVerdict
		if st.verdict[po] == "Deny" {
			st.stopped = true
		}
	case "APPROVAL_SESSION_OPENED":
		if st.verdict[po] != "Ask" || st.session[po] != "" {
			return errors.New("one session per Ask")
		}
		st.session[po] = e.Payload.ApprovalSessionID
	case "APPROVAL_EXPIRED", "APPROVAL_WITHDRAWN", "APPROVED", "APPROVAL_DENIED", "APPROVAL_SUPERSEDED":
		if st.session[po] == "" || st.closed[po] != "" || e.Payload.ApprovalSessionID != st.session[po] {
			return errors.New("only an open session of this purchase can close, once")
		}
		st.closed[po] = e.EventType
	case "GRANT_ISSUED":
		if st.closed[po] != "APPROVED" || st.granted[po] || e.Payload.ApprovalSessionID != st.session[po] {
			return errors.New("one grant per approved session")
		}
		st.granted[po] = true
	case "TASK_CANCELLED", "TASK_TIMED_OUT":
		if st.lifecycleEnded {
			return errors.New("one TASK_CANCELLED or TASK_TIMED_OUT per task")
		}
		st.lifecycleEnded, st.stopped = true, true
	}
	return nil
}
