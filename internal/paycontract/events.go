package paycontract

import (
	"errors"
	"fmt"
	"regexp"
	"slices"

	"github.com/SIMPLYBOYS/agent-payment-authority/internal/contracts"
)

// An apa.pay.v1 task's events use the shared envelope (contracts.AuditEvent)
// with this version, this domain's chain and these rules. The M1 rules
// (contracts.ValidateEvent) refuse every one of them.

// EventTypes: M1's task events plus the delegation, approval session,
// approval closures and the grant, and P2's payment events
// (docs/commercial-p2-host.md 4).
var EventTypes = []string{"TASK_STARTED", "DELEGATION_BOUND", "DELEGATION_REVOKED", "PURCHASE_PROPOSED", "POLICY_DECIDED", "APPROVAL_SESSION_OPENED",
	"APPROVAL_EXPIRED", "APPROVAL_WITHDRAWN", "APPROVED", "APPROVAL_DENIED", "APPROVAL_SUPERSEDED", "GRANT_ISSUED",
	"TASK_CANCELLED", "TASK_TIMED_OUT", "CLI_EXITED",
	"VOICE_CHALLENGE_VERIFIED", "VOICE_CHALLENGE_FAILED", "VOICE_UNAVAILABLE", // V1: a voice outcome, just before the closure it leads to
	"PAYMENT_REQUESTED", "BUDGET_RESERVED", "PAYMENT_SIGNED", "PAYMENT_SUBMITTED", "PAYMENT_SETTLED", "PAYMENT_NOT_SETTLED",
	"PAYMENT_NEEDS_REVIEW", "RESOURCE_DELIVERED", "RECEIPT_ISSUED", "PAYMENT_REFUNDED"}

// PaymentEvents are P2's payment events, in their order; PAYMENT_REFUNDED is the shop's refund of a payment
// settled and not delivered, which a person asked for (docs/commercial-refund.md).
var PaymentEvents = []string{"PAYMENT_REQUESTED", "BUDGET_RESERVED", "PAYMENT_SIGNED", "PAYMENT_SUBMITTED", "PAYMENT_SETTLED",
	"PAYMENT_NOT_SETTLED", "PAYMENT_NEEDS_REVIEW", "RESOURCE_DELIVERED", "RECEIPT_ISSUED", "PAYMENT_REFUNDED"}

// Ended: the stream has stopped taking new purchases or payments, as the
// order rules see it (a Deny, a cancellation, a CLI timeout, the person's
// revocation of the delegation). A payment is requested only before; one
// requested is carried through after.
func Ended(events []contracts.AuditEvent) bool {
	for _, e := range events {
		switch {
		case e.EventType == "TASK_CANCELLED" || e.EventType == "TASK_TIMED_OUT" || e.EventType == "DELEGATION_REVOKED":
			return true
		case e.EventType == "POLICY_DECIDED" && e.Payload.Decision != nil && e.Payload.Decision.PolicyVerdict == "Deny":
			return true
		}
	}
	return false
}

// P2's payment codes. NotSettledCodes end a payment that spent nothing
// (never reserved); NeedsReviewCodes one a person must look at: reserved
// and not paid, or paid and not delivered (DELIVERY_FAILED).
var (
	NotSettledCodes  = []string{"RESERVE_FAILED", "NOT_PAID_EXPIRED", "TASK_ENDED"}
	NeedsReviewCodes = []string{"RESERVED_NOT_PAID", "NOT_SETTLED_EXPIRED", "VERIFICATION_FAILED", "RECONCILE_NEEDS_REVIEW", "DELIVERY_FAILED"}
)

// V1's voice codes (docs/commercial-v1.md): the failures deny the session;
// unavailability only degrades the approval, and says so.
var (
	VoiceFailures    = []string{"VOICE_REPLAY", "VOICE_PHRASE_MISMATCH", "VOICE_SPEAKER_MISMATCH", "VOICE_AUDIO_INVALID"}
	VoiceUnavailable = []string{"VOICE_DEVICE_UNAVAILABLE", "VOICE_SERVICE_UNAVAILABLE", "VOICE_TIMEOUT"}
)

// Closures are the events that end an approval session, and the approval
// state each records.
var Closures = map[string]string{"APPROVAL_EXPIRED": "expired", "APPROVAL_WITHDRAWN": "withdrawn", "APPROVED": "approved",
	"APPROVAL_DENIED": "denied", "APPROVAL_SUPERSEDED": "superseded"}

var lifecycleCodes = map[string]string{"TASK_CANCELLED": "HOST_CANCELLED", "TASK_TIMED_OUT": "CLI_TIMEOUT", "CLI_EXITED": ""}

// EventHash and GenesisHash are contracts.EventHash and GenesisHash under
// this version's domain: an M1 chain and a v1 chain never link.
// They are apa.pay.v1's; EventHash follows the event's own version.
func EventHash(e contracts.AuditEvent) (string, error) {
	sp, err := specOf(e.Version, "INVALID_EVENT", "event")
	if err != nil {
		return "", err
	}
	return sp.EventHash(e)
}

func GenesisHash(taskID string) (string, error) { return v1.GenesisHash(taskID) }

// ValidateEvent checks one event of an apa.pay.v1 task: shape, the task's
// registry binding, the purchase it names (whose v1 intent_hash it must
// carry) and the payload each type allows.
func ValidateEvent(reg *contracts.Registry, e contracts.AuditEvent, order *PurchaseOrder) error {
	sp, ok := SpecOf(e.Version)
	if !ok || !ids(e.EventID, e.Source) || !timestamp(e.ObservedAt) || e.Sequence == 0 || e.Sequence > contracts.MaxSafeInteger {
		return errf("invalid event metadata")
	}
	if !slices.Contains([]string{"fixture", "live", "replay"}, e.EvidenceMode) || e.Scope == nil || !digestPattern.MatchString(e.PrevEventHash) {
		return errf("task event needs evidence mode, scope and prev_event_hash")
	}
	p := e.Payload
	for _, v := range []string{p.Code, p.DelegationID, p.ApprovalSessionID, p.GrantID, p.SignerApprovalID} {
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
		if err != nil || order.Version != e.Version || s.IntentHash != h || s.PurchaseOrderID != order.Intent.PurchaseOrderID {
			return errf("event intent mismatch")
		}
	}
	purchase := s.PurchaseOrderID != ""
	d := p.Decision
	only := func(want ...string) error { // the payload fields this type carries, all present; no other
		have := map[string]bool{"code": p.Code != "", "decision": d != nil, "delegation": p.DelegationID != "", "session": p.ApprovalSessionID != "",
			"grant": p.GrantID != "", "method": p.AuthorizationMethod != "", "signer": p.SignerApprovalID != "", "credential": p.Credential != nil,
			"payment": p.Payment != nil, "conditions": p.RunConditions != nil}
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
		if r := p.RunConditions; r != nil { // E1-a: the run its events come from
			if err := ValidRun(*r); err != nil {
				return errf("%v", err)
			}
			return only("conditions")
		}
		return only()
	case "DELEGATION_BOUND":
		if purchase {
			return errf("a delegation is bound to the task, not a purchase")
		}
		if p.AuthorizationMethod != "" { // gap 4: confirmed by the person's device, the Signer's confirmation named; a simulated one says nothing more (as before)
			if p.AuthorizationMethod != "webauthn" || !ids(p.SignerApprovalID) {
				return errf("a delegation's confirmation is webauthn with the Signer's confirmation")
			}
			return only("delegation", "method", "signer")
		}
		return only("delegation")
	case "DELEGATION_REVOKED": // the person took it back with their device; the Signer's revocation named (docs/commercial-gap4.md 7)
		if purchase {
			return errf("a revocation is of the task's delegation, not a purchase")
		}
		if p.AuthorizationMethod != "webauthn" || !ids(p.SignerApprovalID) {
			return errf("a revocation is webauthn with the Signer's revocation")
		}
		return only("delegation", "method", "signer")
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
	case "VOICE_CHALLENGE_VERIFIED", "VOICE_CHALLENGE_FAILED", "VOICE_UNAVAILABLE":
		codes := map[string][]string{"VOICE_CHALLENGE_FAILED": VoiceFailures, "VOICE_UNAVAILABLE": VoiceUnavailable}[e.EventType]
		switch {
		case !purchase:
			return errf("a voice outcome needs a purchase")
		case codes == nil && p.Code != "", codes != nil && !slices.Contains(codes, p.Code):
			return errf("%s code %q", e.EventType, p.Code)
		case codes == nil:
			return only("session")
		}
		return only("session", "code")
	case "APPROVAL_DENIED":
		if d != nil && d.Reason == "VOICE_CHALLENGE_FAILED" { // V1: the voice denied it, not the approver: no method
			if !purchase {
				return errf("a closure needs a purchase")
			}
			if err := only("decision", "session"); err != nil {
				return err
			}
			return decisionOf(e, "denied")
		}
		fallthrough
	case "APPROVED": // decided by an approver, whose method is recorded
		fields, err := method(e, purchase, "decision", "session", "method")
		if err != nil {
			return err
		}
		if err := only(fields...); err != nil {
			return err
		}
		if e.EventType == "APPROVED" && d != nil && d.Reason != map[string]string{"simulated": "SIMULATED_APPROVAL", "webauthn": "WEBAUTHN_APPROVAL"}[p.AuthorizationMethod] {
			return errf("APPROVED's reason is its method's")
		}
		return decisionOf(e, Closures[e.EventType])
	case "GRANT_ISSUED":
		fields, err := method(e, purchase, "session", "grant", "method")
		if err != nil {
			return err
		}
		if p.GrantID != sp.GrantID(p.ApprovalSessionID) {
			return errf("grant ID is not the session's")
		}
		return only(fields...)
	case "CREDENTIAL_VERIFIED": // apa.pay.v2 only (C2)
		if sp.Version != V2 || !purchase {
			return errf("CREDENTIAL_VERIFIED is an apa.pay.v2 purchase's")
		}
		if err := only("credential"); err != nil {
			return err
		}
		return credentialCheck(p.Credential)
	case "PAYMENT_REQUESTED", "BUDGET_RESERVED", "PAYMENT_SIGNED", "PAYMENT_SUBMITTED", "PAYMENT_SETTLED", "PAYMENT_NOT_SETTLED",
		"PAYMENT_NEEDS_REVIEW", "RESOURCE_DELIVERED", "RECEIPT_ISSUED", "PAYMENT_REFUNDED":
		if !purchase {
			return errf("%s needs a purchase", e.EventType)
		}
		return paymentEvent(e, order)
	case "TASK_CANCELLED", "TASK_TIMED_OUT", "CLI_EXITED":
		want := lifecycleCodes[e.EventType]
		if p.Code == "" || (want != "" && p.Code != want) {
			return errf("%s needs code %s", e.EventType, want)
		}
		return only("code")
	}
	return errf("unsupported apa.pay.v1 event %s; cannot settle or deliver", e.EventType)
}

// CredentialOutcome is what a record decides, by CredentialDeny: its result, or "unmet" for a valid one whose
// claims did not say what the venue asked (docs/commercial-venues.md 3.2; the record keeps result valid).
func CredentialOutcome(c *contracts.CredentialCheck) string {
	if c.Result == "valid" && c.Detail == "CLAIM_UNMET" {
		return "unmet"
	}
	return c.Result
}

// claimsPattern: v2's disclosed attributes, "name=value" joined by "," (credential.Attributes).
var claimsPattern = regexp.MustCompile(`^(buyer_type=(business|individual)|country=[A-Z]{2}|age_over_18=(true|false))(,(buyer_type=(business|individual)|country=[A-Z]{2}|age_over_18=(true|false)))*$`)

// credentialCheck is a verification record's shape (design 3.2, 3.3): a
// result with its fixed detail; a challenge exactly when one was stored;
// what was established when the presentation verified; the status list it
// read when it got that far.
func credentialCheck(c *contracts.CredentialCheck) error {
	_, deny := CredentialDeny[c.Result]
	switch {
	case c.Result != "valid" && !deny:
		return errf("credential result %q", c.Result)
	case c.Detail != "" && !slices.Contains(CredentialDetails, c.Detail):
		return errf("credential detail %q", c.Detail)
	case !ids(c.Verifier):
		return errf("credential verifier")
	case (c.ChallengeID == "") != (c.Result == "absent" || slices.Contains(NoChallenge, c.Detail)):
		return errf("a credential record has a challenge exactly when one was stored")
	case c.ChallengeID != "" && !ids(c.ChallengeID):
		return errf("credential challenge ID")
	}
	established := c.Result == "valid" || c.Result == "revoked" || c.Result == "expired" || (c.Result == "status_unavailable" && c.Issuer != "")
	if established && (c.CredentialSchema != "apa.credential.v1" && c.CredentialSchema != "apa.credential.v2" || c.Issuer == "" || c.Holder == "" || c.StatusIndex == "" || c.Expiry == 0 || c.ProofSHA256 == "") {
		return errf("a verified presentation records what it established")
	}
	if (c.Result == "valid" || c.Result == "revoked" || c.Result == "expired") && (c.StatusListVersion == 0 || c.StatusListIssuedAt == 0 || c.StatusListSHA256 == "") {
		return errf("a status decision records the list it read")
	}
	// v2's claims (docs/commercial-venues.md 3.2): the disclosed attributes, only of an established v2
	// presentation; CLAIM_UNMET only on a valid one (the venue asked for what it did not say; a v1 credential
	// has no attribute to say it with).
	switch {
	case c.Claims != "" && (!established || c.CredentialSchema != "apa.credential.v2" || !claimsPattern.MatchString(c.Claims)):
		return errf("credential claims %q", c.Claims)
	case c.Detail == "CLAIM_UNMET" && c.Result != "valid":
		return errf("CLAIM_UNMET is a valid credential's")
	}
	if (c.Result == "absent" || c.Result == "invalid") && (c.Issuer != "" || c.StatusListVersion != 0) {
		return errf("a presentation that did not verify establishes nothing")
	}
	return nil
}

// method checks the decision's method: simulated (P1-a), or webauthn
// (A2), which also names the Signer's approval; it returns the payload
// fields the event then carries.
func method(e contracts.AuditEvent, purchase bool, fields ...string) ([]string, error) {
	switch {
	case !purchase:
		return nil, errf("%s needs a purchase", e.EventType)
	case e.Payload.AuthorizationMethod == "simulated":
		return fields, nil
	case e.Payload.AuthorizationMethod == "webauthn":
		return append(fields, "signer"), nil
	}
	return nil, errf("%s: the method is simulated or webauthn", e.EventType)
}

func decisionOf(e contracts.AuditEvent, approvals ...string) error {
	d := e.Payload.Decision
	if err := ValidateDecision(*d); err != nil {
		return err
	}
	if d.Version != e.Version || d.IntentHash != e.Scope.IntentHash {
		return errf("decision version or intent mismatch")
	}
	if !slices.Contains(approvals, d.ApprovalStatus) {
		return coded("INVALID_DECISION", e.EventType+" cannot record approval_status "+d.ApprovalStatus)
	}
	return nil
}

// chain is one task's position in its stream; see contracts' chainState.
type chain struct {
	version                 string
	next                    uint64
	prev                    string
	bound                   bool
	delegation              string // DELEGATION_BOUND's; DELEGATION_REVOKED names it, once
	revoked                 bool
	stopped, lifecycleEnded bool
	proposed                map[string]bool
	verdict, intent         map[string]string
	session, closed         map[string]string // po -> session ID; po -> closure event
	granted                 map[string]bool
	credential              map[string]string // v2: po -> verification result
	voiced                  map[string]string // V1: po -> its session's voice outcome event
	grantMethod             map[string]string // po -> its grant's authorization method
	paid                    map[string]string // P2: po -> its last payment event
}

var txHashPattern, sha256Pattern = regexp.MustCompile(`^0x[0-9a-f]{64}$`), regexp.MustCompile(`^[0-9a-f]{64}$`)

// paymentEvent checks a P2 payment event's payload: the record fields it
// carries, all of them and no others, and its code.
func paymentEvent(e contracts.AuditEvent, order *PurchaseOrder) error {
	p := e.Payload
	if p.Decision != nil || p.DelegationID != "" || p.ApprovalSessionID != "" || p.GrantID != "" || p.AuthorizationMethod != "" ||
		p.SignerApprovalID != "" || p.Credential != nil {
		return errf("%s carries only its payment record and code", e.EventType)
	}
	r := p.Payment
	want := map[string][]string{ // record fields per type; nil: no record at all
		"PAYMENT_REQUESTED":  {"method", "amount"},
		"BUDGET_RESERVED":    {"method", "amount", "tx", "block"},
		"PAYMENT_SIGNED":     {"method"},
		"PAYMENT_SETTLED":    {"tx", "block"},
		"RESOURCE_DELIVERED": {"content"},
		"RECEIPT_ISSUED":     {"content", "receipt"},
		"PAYMENT_REFUNDED":   {"amount", "tx", "block"}, // the whole amount back, in its own transaction
	}[e.EventType]
	codes := map[string][]string{"PAYMENT_NOT_SETTLED": NotSettledCodes, "PAYMENT_NEEDS_REVIEW": NeedsReviewCodes}[e.EventType]
	switch {
	case codes == nil && p.Code != "", codes != nil && !slices.Contains(codes, p.Code):
		return errf("%s code %q", e.EventType, p.Code)
	case (want == nil) != (r == nil):
		return errf("%s: payment record must %s", e.EventType, map[bool]string{true: "be absent", false: "be present"}[r != nil])
	case r == nil:
		return nil
	}
	have := map[string]bool{"method": r.Method != "", "amount": r.Amount != "", "tx": r.TxHash != "", "block": r.Block != 0,
		"content": r.ContentSHA256 != "", "receipt": r.ReceiptID != ""}
	for k, v := range have {
		if v != slices.Contains(want, k) {
			return errf("%s: payment field %s must %s", e.EventType, k, map[bool]string{true: "be absent", false: "be present"}[v])
		}
	}
	switch {
	case r.Method != "" && r.Method != "delegation" && r.Method != "webauthn":
		return errf("a payment is authorized by a delegation or a human approval")
	case r.TxHash != "" && !txHashPattern.MatchString(r.TxHash), r.ContentSHA256 != "" && !sha256Pattern.MatchString(r.ContentSHA256),
		r.ReceiptID != "" && !ids(r.ReceiptID):
		return errf("%s: malformed payment record", e.EventType)
	case r.Amount != "" && (order == nil || r.Amount != order.PaymentBinding.Amount):
		return errf("%s: the amount is the purchase's", e.EventType)
	}
	return nil
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
			sp, err := specOf(e.Version, "INVALID_EVENT_ORDER", "event")
			if err != nil {
				return err
			}
			g, err := sp.GenesisHash(task)
			if err != nil {
				return err
			}
			st = &chain{version: e.Version, prev: g, proposed: map[string]bool{}, verdict: map[string]string{}, intent: map[string]string{}, credential: map[string]string{},
				session: map[string]string{}, closed: map[string]string{}, granted: map[string]bool{}, voiced: map[string]string{},
				grantMethod: map[string]string{}, paid: map[string]string{}}
			tasks[task] = st
		}
		if e.Version != st.version {
			return coded("INVALID_EVENT_ORDER", fmt.Sprintf("task %s: one contract version per event stream", task))
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
	case "DELEGATION_BOUND":
		st.delegation = e.Payload.DelegationID
	case "DELEGATION_REVOKED": // also after the task ended: the Signer would still have paid under it
		if st.revoked || e.Payload.DelegationID != st.delegation {
			return errors.New("one revocation, of the delegation bound to the task")
		}
		st.revoked, st.stopped = true, true
	case "PURCHASE_PROPOSED":
		if st.stopped || st.proposed[po] {
			return errors.New("one proposal per purchase, none after the task ended")
		}
		st.proposed[po], st.intent[po] = true, e.Scope.IntentHash
	case "CREDENTIAL_VERIFIED":
		if st.version != V2 || st.stopped || !st.proposed[po] || st.verdict[po] != "" || st.credential[po] != "" {
			return errors.New("one credential verification per v2 purchase, after its proposal and before its decision")
		}
		st.credential[po] = CredentialOutcome(e.Payload.Credential)
	case "POLICY_DECIDED":
		if st.stopped || !st.proposed[po] || st.verdict[po] != "" {
			return errors.New("one decision per proposed purchase, none after the task ended")
		}
		if st.version == V2 { // the decision follows the verification (design 3.3)
			d, r := e.Payload.Decision, st.credential[po]
			want, deny := CredentialDeny[r]
			switch {
			case r == "":
				return errors.New("a v2 decision needs its purchase's credential verification first")
			case deny && (d.PolicyVerdict != "Deny" || d.Reason != want):
				return fmt.Errorf("credential %s must decide Deny %s", r, want)
			case !deny && d.PolicyVerdict == "Deny" && slices.Contains(CredentialReasons, d.Reason):
				return errors.New("a valid credential cannot be denied for a credential reason")
			}
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
	case "VOICE_CHALLENGE_VERIFIED", "VOICE_CHALLENGE_FAILED", "VOICE_UNAVAILABLE":
		if st.session[po] == "" || st.closed[po] != "" || st.voiced[po] != "" || e.Payload.ApprovalSessionID != st.session[po] {
			return errors.New("one voice outcome per open session")
		}
		st.voiced[po] = e.EventType
	case "APPROVAL_EXPIRED", "APPROVAL_WITHDRAWN", "APPROVED", "APPROVAL_DENIED", "APPROVAL_SUPERSEDED":
		if st.session[po] == "" || st.closed[po] != "" || e.Payload.ApprovalSessionID != st.session[po] {
			return errors.New("only an open session of this purchase can close, once")
		}
		byVoice := e.EventType == "APPROVAL_DENIED" && e.Payload.Decision != nil && e.Payload.Decision.Reason == "VOICE_CHALLENGE_FAILED"
		if byVoice != (st.voiced[po] == "VOICE_CHALLENGE_FAILED") {
			return errors.New("a failed voice challenge, and only one, closes its session denied for it")
		}
		if st.voiced[po] != "" && e.EventType != "APPROVED" && !byVoice {
			return errors.New("a voice outcome leads to its session's approval or its denial")
		}
		st.closed[po] = e.EventType
	case "GRANT_ISSUED":
		if st.closed[po] != "APPROVED" || st.granted[po] || e.Payload.ApprovalSessionID != st.session[po] {
			return errors.New("one grant per approved session")
		}
		st.granted[po] = true
		st.grantMethod[po] = e.Payload.AuthorizationMethod
	case "PAYMENT_REQUESTED", "BUDGET_RESERVED", "PAYMENT_SIGNED", "PAYMENT_SUBMITTED", "PAYMENT_SETTLED", "PAYMENT_NOT_SETTLED",
		"PAYMENT_NEEDS_REVIEW", "RESOURCE_DELIVERED", "RECEIPT_ISSUED", "PAYMENT_REFUNDED":
		return st.payment(e, po)
	case "TASK_CANCELLED", "TASK_TIMED_OUT":
		if st.lifecycleEnded {
			return errors.New("one TASK_CANCELLED or TASK_TIMED_OUT per task")
		}
		st.lifecycleEnded, st.stopped = true, true
	}
	return nil
}

// payment is P2's order for one purchase (docs/commercial-p2-host.md 3, 4):
// requested only for an Allow (delegation) or an approved grant (webauthn;
// a simulated grant never pays), and never once the task has ended; then
// reserved, signed, submitted, settled, delivered, receipt, each once and
// in order, also after the task ended (a payment in flight is carried
// through: its tx#1 may already be on chain). NOT_SETTLED ends one never
// reserved; NEEDS_REVIEW one reserved and not settled, or settled and not
// delivered (DELIVERY_FAILED). Nothing follows an end but one refund of a
// payment settled and not delivered (PAYMENT_REFUNDED).
// undelivered is the last payment step of one settled and not delivered: the one end a refund may follow.
const undelivered = "PAYMENT_NEEDS_REVIEW DELIVERY_FAILED"

func (st *chain) payment(e contracts.AuditEvent, po string) error {
	last := st.paid[po]
	method := map[string]string{"Allow": "delegation"}[st.verdict[po]]
	if st.granted[po] {
		method = st.grantMethod[po]
	}
	payable := method == "delegation" || method == "webauthn"
	after := map[string]string{"BUDGET_RESERVED": "PAYMENT_REQUESTED", "PAYMENT_SIGNED": "BUDGET_RESERVED", "PAYMENT_SUBMITTED": "PAYMENT_SIGNED",
		"PAYMENT_SETTLED": "PAYMENT_SUBMITTED", "RESOURCE_DELIVERED": "PAYMENT_SETTLED", "RECEIPT_ISSUED": "RESOURCE_DELIVERED"}
	switch t := e.EventType; {
	case t == "PAYMENT_REQUESTED":
		if !payable || last != "" || st.stopped {
			return errors.New("a payment is one Allow's or approved grant's, requested once, before the task ends")
		}
		if e.Payload.Payment.Method != method {
			return errors.New("the payment's method is the purchase's authorization")
		}
	case t == "BUDGET_RESERVED", t == "PAYMENT_SIGNED":
		if last != after[t] || e.Payload.Payment.Method != method {
			return fmt.Errorf("%s follows %s, once, under the purchase's authorization", t, after[t])
		}
	case t == "PAYMENT_NOT_SETTLED":
		if last != "PAYMENT_REQUESTED" {
			return errors.New("NOT_SETTLED ends a payment requested and never reserved")
		}
	case t == "PAYMENT_NEEDS_REVIEW":
		reserved := slices.Contains([]string{"BUDGET_RESERVED", "PAYMENT_SIGNED", "PAYMENT_SUBMITTED"}, last)
		if e.Payload.Code == "DELIVERY_FAILED" && last != "PAYMENT_SETTLED" || e.Payload.Code != "DELIVERY_FAILED" && !reserved {
			return errors.New("NEEDS_REVIEW follows a reservation not settled, or a settlement not delivered (DELIVERY_FAILED)")
		}
		if e.Payload.Code == "DELIVERY_FAILED" {
			st.paid[po] = undelivered
			return nil
		}
	case t == "PAYMENT_REFUNDED":
		if last != undelivered {
			return errors.New("a refund follows a payment settled and not delivered (DELIVERY_FAILED), once")
		}
	default:
		if last != after[t] {
			return fmt.Errorf("%s follows %s, once", t, after[t])
		}
	}
	st.paid[po] = e.EventType
	return nil
}
