package contracts

import "fmt"

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
type EventPayload struct {
	Code     string          `json:"code,omitempty"`
	Decision *PolicyDecision `json:"decision,omitempty"`
}
type AuditEvent struct {
	Version      string       `json:"version"`
	EventID      string       `json:"event_id"`
	EventType    string       `json:"event_type"`
	Scope        *EventScope  `json:"scope,omitempty"`
	Sequence     uint64       `json:"sequence"`
	Payload      EventPayload `json:"payload"`
	EvidenceMode string       `json:"evidence_mode"`
	Source       string       `json:"source"`
	ObservedAt   uint64       `json:"observed_at"`
}

func (r *Registry) ValidateEvent(e AuditEvent, order *PurchaseOrder) error {
	if e.Version != Version || !ids(e.EventID, e.Source) || !timestamp(e.ObservedAt) || e.Sequence > MaxSafeInteger {
		return fmt.Errorf("invalid event metadata")
	}
	if e.EvidenceMode != "fixture" && e.EvidenceMode != "live" && e.EvidenceMode != "replay" {
		return fmt.Errorf("missing/invalid evidence mode")
	}
	if e.Payload.Code != "" && !ids(e.Payload.Code) {
		return fmt.Errorf("invalid event code")
	}
	if e.EventType == "SYSTEM_READY" {
		if e.Scope != nil || e.Sequence != 0 || e.Payload.Decision != nil || order != nil {
			return fmt.Errorf("system event must not invent task/intent")
		}
		return nil
	}
	if e.Scope == nil || e.Sequence == 0 {
		return fmt.Errorf("task event needs scope and positive sequence")
	}
	s := e.Scope
	t, _, err := r.Binding(s.TaskID)
	if err != nil {
		return err
	}
	if s.ConsumerID != t.ConsumerID || s.AgentID != t.AgentID || s.WalletID != t.WalletID {
		return fmt.Errorf("event ownership mismatch")
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
	switch e.EventType {
	case "TASK_STARTED":
		if s.PurchaseOrderID != "" || e.Payload.Decision != nil {
			return fmt.Errorf("task start cannot imply purchase")
		}
	case "PURCHASE_PROPOSED":
		if s.PurchaseOrderID == "" || e.Payload.Decision != nil {
			return fmt.Errorf("invalid purchase event")
		}
	case "POLICY_DECIDED":
		if s.PurchaseOrderID == "" || e.Payload.Decision == nil {
			return fmt.Errorf("missing policy decision")
		}
		d := e.Payload.Decision
		if err := d.Validate(); err != nil {
			return err
		}
		if d.IntentHash != s.IntentHash {
			return fmt.Errorf("decision intent mismatch")
		}
	case "TASK_CANCELLED", "TASK_TIMED_OUT", "CLI_EXITED":
		if e.Payload.Code == "" || e.Payload.Decision != nil {
			return fmt.Errorf("lifecycle event cannot approve/settle")
		}
	default:
		return fmt.Errorf("unsupported M1 event; cannot approve, grant, settle or deliver")
	}
	return nil
}

// ValidateEventOrder checks complete streams starting at sequence 1 per task.
// Per-event metadata/binding validation must be performed separately above.
func ValidateEventOrder(events []AuditEvent) error {
	idsSeen, next := map[string]bool{}, map[string]uint64{}
	for _, e := range events {
		if idsSeen[e.EventID] {
			return fmt.Errorf("duplicate event ID")
		}
		idsSeen[e.EventID] = true
		if e.Scope == nil {
			continue
		}
		key := e.Scope.TaskID
		if e.Sequence != next[key]+1 {
			return fmt.Errorf("event sequence gap/reorder")
		}
		next[key] = e.Sequence
	}
	return nil
}
