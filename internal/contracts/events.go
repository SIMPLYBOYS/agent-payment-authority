package contracts

import (
	"errors"
	"fmt"
	"slices"
)

type EventScope struct {
	ConsumerID      string `json:"consumer_id"`
	AgentID         string `json:"agent_id"`
	WalletID        string `json:"wallet_id"`
	TaskID          string `json:"task_id"`
	PurchaseOrderID string `json:"purchase_order_id,omitempty"`
	IntentHash      string `json:"intent_hash,omitempty"`
}

// Payload is deliberately small and typed: raw CLI text, tokens, biometrics,
// signatures and private keys have no field here. Extend only with a version.
// The last four fields came with apa.pay.v1 (internal/paycontract validates
// them); an M1 event never carries them and ValidateEvent refuses one that does.
type EventPayload struct {
	Code                string           `json:"code,omitempty"`
	Decision            *PolicyDecision  `json:"decision,omitempty"`
	DelegationID        string           `json:"delegation_id,omitempty"`
	ApprovalSessionID   string           `json:"approval_session_id,omitempty"`
	GrantID             string           `json:"grant_id,omitempty"`
	AuthorizationMethod string           `json:"authorization_method,omitempty"`
	SignerApprovalID    string           `json:"signer_approval_id,omitempty"` // apa.pay.v1, A2: the Signer's verified decision
	Credential          *CredentialCheck `json:"credential,omitempty"`         // apa.pay.v2 (C2): CREDENTIAL_VERIFIED only
}

// CredentialCheck is apa.pay.v2's record of one purchase's BBS+ credential
// verification (docs/commercial-c2-design.md 3.2): its result and fixed
// detail, the challenge it used, what the presentation established, the
// status list it read. No proof, signature or key: ProofSHA256 is for
// tracing only, never a replay key. internal/paycontract validates it.
type CredentialCheck struct {
	Result             string `json:"result"`
	Detail             string `json:"detail,omitempty"`
	ChallengeID        string `json:"challenge_id,omitempty"`
	CredentialSchema   string `json:"credential_schema,omitempty"`
	Issuer             string `json:"issuer,omitempty"`
	Holder             string `json:"holder,omitempty"`
	StatusIndex        string `json:"status_index,omitempty"`
	Expiry             uint64 `json:"expiry,omitempty"`
	StatusListVersion  uint64 `json:"status_list_version,omitempty"`
	StatusListIssuedAt uint64 `json:"status_list_issued_at,omitempty"`
	StatusListSHA256   string `json:"status_list_sha256,omitempty"`
	ProofSHA256        string `json:"proof_sha256,omitempty"`
	Verifier           string `json:"verifier"`
}

// AuditEvent is one link of its task's hash chain. PrevEventHash is the
// EventHash of the previous event of the same task, or GenesisHash(task) for
// sequence 1; SYSTEM_READY has no task and no chain. An event never carries
// its own hash.
type AuditEvent struct {
	Version       string       `json:"version"`
	EventID       string       `json:"event_id"`
	EventType     string       `json:"event_type"`
	Scope         *EventScope  `json:"scope,omitempty"`
	Sequence      uint64       `json:"sequence"`
	PrevEventHash string       `json:"prev_event_hash,omitempty"`
	Payload       EventPayload `json:"payload"`
	EvidenceMode  string       `json:"evidence_mode"`
	Source        string       `json:"source"`
	ObservedAt    uint64       `json:"observed_at"`
}

// M1 event types. There is no APPROVED, GRANT_ISSUED, SETTLED or
// RESOURCE_DELIVERED: M1 cannot approve, grant, settle or deliver.
var eventTypes = []string{"SYSTEM_READY", "TASK_STARTED", "PURCHASE_PROPOSED", "POLICY_DECIDED", "APPROVAL_EXPIRED", "APPROVAL_WITHDRAWN", "TASK_CANCELLED", "TASK_TIMED_OUT", "CLI_EXITED"}

// Approval states each decision-carrying event may record.
var decisionApprovals = map[string][]string{
	"POLICY_DECIDED":     {"pending", "not_requested"},
	"APPROVAL_EXPIRED":   {"expired"},
	"APPROVAL_WITHDRAWN": {"withdrawn"},
}

// Lifecycle events and their required code ("" = any ID). TASK_TIMED_OUT is
// the CLI/task timeout only; an approval timeout is APPROVAL_EXPIRED.
var lifecycleCodes = map[string]string{"TASK_CANCELLED": "HOST_CANCELLED", "TASK_TIMED_OUT": "CLI_TIMEOUT", "CLI_EXITED": ""}

// ValidateEvent checks one event's shape and bindings. It ignores the task's
// current status, so events of a cancelled or failed task remain verifiable.
func (r *Registry) ValidateEvent(e AuditEvent, order *PurchaseOrder) error {
	if e.Version != Version || !ids(e.EventID, e.Source) || !timestamp(e.ObservedAt) || e.Sequence > MaxSafeInteger {
		return fmt.Errorf("invalid event metadata")
	}
	if !slices.Contains(evidenceModes, e.EvidenceMode) {
		return fmt.Errorf("missing/invalid evidence mode")
	}
	if e.Payload.Code != "" && !ids(e.Payload.Code) {
		return fmt.Errorf("invalid event code")
	}
	if p := e.Payload; p.DelegationID != "" || p.ApprovalSessionID != "" || p.GrantID != "" || p.AuthorizationMethod != "" || p.SignerApprovalID != "" || p.Credential != nil {
		return fmt.Errorf("delegation, approval session, grant and credential fields belong to the pay family, not M1")
	}
	if e.EventType == "SYSTEM_READY" {
		if e.Scope != nil || e.Sequence != 0 || e.PrevEventHash != "" || e.Payload.Decision != nil || order != nil {
			return fmt.Errorf("system event must not invent task, chain or intent")
		}
		return nil
	}
	if e.Scope == nil || e.Sequence == 0 || !digestPattern.MatchString(e.PrevEventHash) {
		return fmt.Errorf("task event needs scope, positive sequence and prev_event_hash")
	}
	s := e.Scope
	t, _, err := r.Binding(s.TaskID)
	if err != nil {
		return err
	}
	if s.ConsumerID != t.ConsumerID || s.AgentID != t.AgentID || s.WalletID != t.WalletID {
		return coded("OWNERSHIP_MISMATCH", "event scope does not match the task binding")
	}
	if s.PurchaseOrderID == "" {
		if s.IntentHash != "" || order != nil {
			return fmt.Errorf("intent without purchase")
		}
	} else {
		if order == nil {
			return fmt.Errorf("purchase snapshot required")
		}
		if err := r.ValidatePurchase(Session{t.ConsumerID, t.AgentID, t.TaskID}, *order); err != nil {
			return err
		}
		h, err := order.Intent.Hash()
		if err != nil || s.IntentHash != h || s.PurchaseOrderID != order.Intent.PurchaseOrderID {
			return fmt.Errorf("event intent mismatch")
		}
	}
	d := e.Payload.Decision
	switch e.EventType {
	case "TASK_STARTED":
		if s.PurchaseOrderID != "" || d != nil {
			return fmt.Errorf("task start cannot imply purchase")
		}
	case "PURCHASE_PROPOSED":
		if s.PurchaseOrderID == "" || d != nil {
			return fmt.Errorf("invalid purchase event")
		}
	case "POLICY_DECIDED", "APPROVAL_EXPIRED", "APPROVAL_WITHDRAWN":
		if s.PurchaseOrderID == "" || d == nil {
			return fmt.Errorf("%s needs a purchase and a decision", e.EventType)
		}
		if err := d.Validate(); err != nil {
			return err
		}
		if d.IntentHash != s.IntentHash {
			return fmt.Errorf("decision intent mismatch")
		}
		if !slices.Contains(decisionApprovals[e.EventType], d.ApprovalStatus) {
			return coded("INVALID_DECISION", e.EventType+" cannot record approval_status "+d.ApprovalStatus)
		}
	case "TASK_CANCELLED", "TASK_TIMED_OUT", "CLI_EXITED":
		want := lifecycleCodes[e.EventType]
		if e.Payload.Code == "" || (want != "" && e.Payload.Code != want) || d != nil {
			return fmt.Errorf("%s needs code %s and cannot approve/settle", e.EventType, want)
		}
	default:
		return fmt.Errorf("unsupported M1 event; cannot approve, grant, settle or deliver")
	}
	return nil
}

// EventHash is the value the next event of the same task stores as
// prev_event_hash: a fixture-domain digest of the whole canonical event,
// including its own prev_event_hash.
func EventHash(e AuditEvent) (string, error) {
	return digestValue(FixtureDomain+"/event", e)
}

// GenesisHash is prev_event_hash for sequence 1. Binding it to the task ID
// stops a chain prefix from being replayed under another task.
func GenesisHash(taskID string) (string, error) {
	return digestValue(FixtureDomain+"/event-genesis", map[string]string{"task_id": taskID})
}

// chainState is one task's position in its stream. stopped: no new purchase
// or decision (after Deny, cancel or timeout). lifecycleEnded: the single
// TASK_CANCELLED / TASK_TIMED_OUT has been recorded. intent pins each
// purchase_order_id to the intent_hash of its first PURCHASE_PROPOSED.
type chainState struct {
	next                    uint64
	prev                    string
	stopped, lifecycleEnded bool
	proposed, closed        map[string]bool
	verdict, intent         map[string]string
}

// ValidateEventOrder checks complete per-task streams: IDs unique, sequence
// contiguous from 1, the prev_event_hash chain, and the M1 transition rules
// (docs/contracts.md). It is pure: it cannot see a truncated tail or a fully
// recomputed chain without an external checkpoint, and it persists nothing.
// Run ValidateEvent on each event separately.
func ValidateEventOrder(events []AuditEvent) error {
	seen, tasks := map[string]bool{}, map[string]*chainState{}
	for _, e := range events {
		if seen[e.EventID] {
			return coded("INVALID_EVENT_ORDER", "duplicate event ID "+e.EventID)
		}
		seen[e.EventID] = true
		if e.Scope == nil {
			continue
		}
		task := e.Scope.TaskID
		st := tasks[task]
		if st == nil {
			genesis, err := GenesisHash(task)
			if err != nil {
				return err
			}
			st = &chainState{prev: genesis, proposed: map[string]bool{}, closed: map[string]bool{}, verdict: map[string]string{}, intent: map[string]string{}}
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
			var c *CodedError
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

// apply enforces the transition table for one task-scoped event. Each guard
// has its own message so tests can show which one fired.
func (st *chainState) apply(e AuditEvent) error {
	po := e.Scope.PurchaseOrderID
	if (st.next == 0) != (e.EventType == "TASK_STARTED") {
		return errors.New("TASK_STARTED must be the first event and appear once")
	}
	// A purchase keeps the intent it was proposed with: later decisions and
	// approval closures must reference the same intent_hash.
	if pinned := st.intent[po]; pinned != "" && e.Scope.IntentHash != pinned {
		return coded("PURCHASE_INTENT_CHANGED", fmt.Sprintf("purchase %s was proposed with %s, not %s", po, pinned, e.Scope.IntentHash))
	}
	switch e.EventType {
	case "PURCHASE_PROPOSED":
		if st.stopped {
			return errors.New("no new purchase after the task ended (Deny, cancel or timeout)")
		}
		if st.proposed[po] {
			return errors.New("one proposal per purchase")
		}
		st.proposed[po], st.intent[po] = true, e.Scope.IntentHash
	case "POLICY_DECIDED":
		if st.stopped {
			return errors.New("no decision after the task ended (Deny, cancel or timeout)")
		}
		if !st.proposed[po] || e.Payload.Decision == nil {
			return errors.New("a decision needs an earlier proposal of the same purchase")
		}
		if st.verdict[po] != "" {
			return errors.New("one decision per purchase")
		}
		st.verdict[po] = e.Payload.Decision.PolicyVerdict
		// M1: after a Deny the customer leaves; the task takes no new purchase
		// or decision. An Ask already open may still expire or be withdrawn.
		if st.verdict[po] == "Deny" {
			st.stopped = true
		}
	case "APPROVAL_EXPIRED", "APPROVAL_WITHDRAWN":
		if st.verdict[po] != "Ask" || st.closed[po] {
			return errors.New("only an open Ask can expire or be withdrawn, once")
		}
		st.closed[po] = true
	case "TASK_CANCELLED", "TASK_TIMED_OUT":
		if st.lifecycleEnded {
			return errors.New("one TASK_CANCELLED or TASK_TIMED_OUT per task")
		}
		st.lifecycleEnded, st.stopped = true, true
	}
	return nil
}
