package paycontract

import (
	"errors"
	"fmt"
	"slices"

	"github.com/SIMPLYBOYS/agent-payment-authority/internal/contracts"
)

// RunConditions: contracts.RunConditions, the run a pay task's events come
// from, in its TASK_STARTED.
type RunConditions = contracts.RunConditions

var (
	runAgents         = []string{"live", "fake"}
	runChains         = []string{"base_sepolia", "local_anvil", "none"}
	runAuthenticators = []string{"device", "virtual", "none"}
)

// ValidRun checks r's values.
func ValidRun(r RunConditions) error {
	if !slices.Contains(runAgents, r.Agent) || !slices.Contains(runChains, r.Chain) || !slices.Contains(runAuthenticators, r.Authenticator) {
		return fmt.Errorf("run conditions %+v: agent live|fake, chain base_sepolia|local_anvil|none, authenticator device|virtual|none", r)
	}
	return nil
}

// Evidence is the closed table of a pay event's evidence_mode and source
// under its task's run conditions (E1a-D4), those of its TASK_STARTED. A
// task started without them (before E1-a) keeps fixture/host. ok is false only for an
// event type or method the table does not know: CheckEvidenceTable proves
// there is none, and the Store still writes such an event as fixture/host
// rather than refuse it (on a payment path a refused write loses a record).
func Evidence(r *RunConditions, e contracts.AuditEvent) (mode, source string, ok bool) {
	if r == nil {
		return "fixture", "host", true
	}
	agent := map[string]string{"live": "live", "fake": "fixture"}[r.Agent]
	auth := func() (string, string, bool) {
		switch r.Authenticator {
		case "device":
			return "live", "signer_webauthn", true
		case "virtual":
			return "fixture", "virtual_authenticator", true
		}
		return "fixture", "host", true
	}
	chain := func(source string) (string, string, bool) {
		if source == "" {
			source = r.Chain
		}
		switch r.Chain {
		case "base_sepolia":
			return "live", source, true
		case "local_anvil":
			return "fixture", source, true
		}
		return "", "", false // no payment without a chain
	}
	switch e.EventType {
	case "PURCHASE_PROPOSED":
		if r.Agent == "live" {
			return "live", "live_cli", true
		}
		return "fixture", "fake_cli", true
	case "TASK_STARTED", "POLICY_DECIDED", "TASK_CANCELLED", "TASK_TIMED_OUT", "CLI_EXITED":
		return agent, "host", true
	case "DELEGATION_BOUND": // by its confirmation's method (gap 4); a record without one is simulated
		if e.Payload.AuthorizationMethod == "webauthn" {
			return auth()
		}
		return "fixture", "simulated", true
	case "CREDENTIAL_VERIFIED": // a test issuer's credential
		return "fixture", "test_issuer", true
	case "APPROVED", "APPROVAL_DENIED", "GRANT_ISSUED": // decided: by the resolver's method
		switch m := e.Payload.AuthorizationMethod; {
		case m == "simulated":
			return "fixture", "simulated", true
		case m == "webauthn", m == "" && e.EventType == "APPROVAL_DENIED": // "": a denial by the voice, on the WebAuthn path only
			return auth()
		}
		return "", "", false
	case "VOICE_CHALLENGE_VERIFIED", "VOICE_CHALLENGE_FAILED", "VOICE_UNAVAILABLE": // the WebAuthn path only
		return auth()
	case "APPROVAL_SESSION_OPENED", "APPROVAL_EXPIRED", "APPROVAL_WITHDRAWN", "APPROVAL_SUPERSEDED":
		// Nobody decided, and a session records no method: a live run takes
		// WebAuthn approvals only; a fake run's session may end either way.
		if r.Agent == "live" {
			return auth()
		}
		return "fixture", "host", true
	case "PAYMENT_REQUESTED", "BUDGET_RESERVED", "PAYMENT_SIGNED", "PAYMENT_SUBMITTED", "PAYMENT_SETTLED", "PAYMENT_NOT_SETTLED", "PAYMENT_NEEDS_REVIEW":
		return chain("")
	case "RESOURCE_DELIVERED", "RECEIPT_ISSUED": // the host's shop delivers, not the chain
		return chain("host_merchant")
	}
	return "", "", false
}

// CheckEvidenceTable proves the table complete: every pay event type, with
// every method a decided event may carry, under every run condition that
// can pay or not. A host refuses to start otherwise.
func CheckEvidenceTable() error {
	types := append(slices.Clone(EventTypes), "CREDENTIAL_VERIFIED")
	var errs []error
	for _, a := range runAgents {
		for _, c := range runChains {
			for _, au := range runAuthenticators {
				r := &RunConditions{Agent: a, Chain: c, Authenticator: au}
				for _, typ := range types {
					methods := []string{""}
					switch typ {
					case "APPROVED", "GRANT_ISSUED":
						methods = []string{"simulated", "webauthn"}
					case "APPROVAL_DENIED":
						methods = []string{"simulated", "webauthn", ""}
					}
					if c == "none" && slices.Contains(PaymentEvents, typ) {
						continue // no payment event without a chain
					}
					for _, m := range methods {
						e := contracts.AuditEvent{EventType: typ, Payload: contracts.EventPayload{AuthorizationMethod: m}}
						if mode, src, ok := Evidence(r, e); !ok || mode == "" || src == "" {
							errs = append(errs, fmt.Errorf("%s (method %q) under %+v", typ, m, *r))
						}
					}
				}
			}
		}
	}
	if len(errs) > 0 {
		return fmt.Errorf("evidence table incomplete: %w", errors.Join(errs...))
	}
	return nil
}
