package paycontract

import (
	"strings"
	"testing"

	"github.com/SIMPLYBOYS/agent-payment-authority/internal/contracts"
)

// chainOf links events of one task as the Store does (sequence, prev hash
// under this version's domain), from (type, payload, purchase) steps.
type step struct {
	typ string
	p   contracts.EventPayload
	po  bool
}

func chainOf(t *testing.T, o PurchaseOrder, steps ...step) []contracts.AuditEvent {
	t.Helper()
	h := mustHash(t, o)
	sp, _ := SpecOf(o.Version)
	prev, _ := sp.GenesisHash(o.Intent.TaskID)
	var out []contracts.AuditEvent
	for n, s := range steps {
		sc := &contracts.EventScope{ConsumerID: o.Intent.ConsumerID, AgentID: o.Intent.AgentID, WalletID: o.Intent.WalletID, TaskID: o.Intent.TaskID}
		if s.po {
			sc.PurchaseOrderID, sc.IntentHash = o.Intent.PurchaseOrderID, h
		}
		e := contracts.AuditEvent{Version: o.Version, EventID: "evt-" + strings.Repeat("0", 10) + string(rune('a'+n)), EventType: s.typ, Scope: sc, Sequence: uint64(n + 1),
			PrevEventHash: prev, Payload: s.p, EvidenceMode: "fixture", Source: "host", ObservedAt: 1790726450}
		prev, _ = EventHash(e)
		out = append(out, e)
	}
	return out
}

func decision(t *testing.T, o PurchaseOrder, approval, reason string) *contracts.PolicyDecision {
	return &contracts.PolicyDecision{Version: o.Version, IntentHash: mustHash(t, o), PolicyVerdict: "Ask", ApprovalStatus: approval, PaymentStatus: PaymentNotEnabled, Reason: reason}
}

func TestEventOrder(t *testing.T) {
	_, o := base(t)
	sid := SessionID(o.Intent.TaskID, o.Intent.PurchaseOrderID)
	start := []step{{"TASK_STARTED", contracts.EventPayload{}, false}, {"DELEGATION_BOUND", contracts.EventPayload{DelegationID: "dlg-1"}, false},
		{"PURCHASE_PROPOSED", contracts.EventPayload{}, true}, {"POLICY_DECIDED", contracts.EventPayload{Decision: decision(t, o, "pending", "HUMAN_APPROVAL_REQUIRED")}, true}}
	opened := step{"APPROVAL_SESSION_OPENED", contracts.EventPayload{ApprovalSessionID: sid}, true}
	approved := step{"APPROVED", contracts.EventPayload{Decision: decision(t, o, "approved", "SIMULATED_APPROVAL"), ApprovalSessionID: sid, AuthorizationMethod: "simulated"}, true}
	grant := step{"GRANT_ISSUED", contracts.EventPayload{ApprovalSessionID: sid, GrantID: GrantID(sid), AuthorizationMethod: "simulated"}, true}
	expired := step{"APPROVAL_EXPIRED", contracts.EventPayload{Decision: decision(t, o, "expired", "APPROVAL_TIMEOUT"), ApprovalSessionID: sid}, true}
	ok := append(append([]step{}, start...), opened, approved, grant)
	if err := ValidateEventOrder(chainOf(t, o, ok...)); err != nil {
		t.Fatalf("the main line: %v", err)
	}
	for name, steps := range map[string][]step{
		"no delegation bound":     append(append([]step{start[0]}, start[2:]...), opened, approved),
		"a grant before APPROVED": append(append([]step{}, start...), opened, grant),
		"two closures":            append(append([]step{}, start...), opened, approved, expired),
		"a closure, no session":   append(append([]step{}, start...), approved),
		"two grants":              append(append([]step{}, start...), opened, approved, grant, grant),
		"a grant after an expiry": append(append([]step{}, start...), opened, expired, grant),
	} {
		if err := ValidateEventOrder(chainOf(t, o, steps...)); err == nil {
			t.Errorf("%s: accepted", name)
		}
	}
}

func TestEventShape(t *testing.T) {
	m, o := base(t)
	var cfg contracts.RegistryConfig
	fixture(t, "testdata/contracts/registry.json", &cfg)
	reg, err := contracts.NewRegistry(cfg)
	if err != nil {
		t.Fatal(err)
	}
	sid := SessionID(o.Intent.TaskID, o.Intent.PurchaseOrderID)
	es := chainOf(t, o, step{"TASK_STARTED", contracts.EventPayload{}, false}, step{"DELEGATION_BOUND", contracts.EventPayload{DelegationID: "dlg-1"}, false},
		step{"APPROVED", contracts.EventPayload{Decision: decision(t, o, "approved", "SIMULATED_APPROVAL"), ApprovalSessionID: sid, AuthorizationMethod: "simulated"}, true},
		step{"GRANT_ISSUED", contracts.EventPayload{ApprovalSessionID: sid, GrantID: GrantID(sid), AuthorizationMethod: "simulated"}, true},
		step{"APPROVED", contracts.EventPayload{Decision: decision(t, o, "approved", "SIMULATED_APPROVAL"), ApprovalSessionID: sid, AuthorizationMethod: "webauthn"}, true},
		step{"SETTLED", contracts.EventPayload{}, true})
	for i, want := range []bool{true, true, true, true, false, false} {
		var order *PurchaseOrder
		if es[i].Scope.PurchaseOrderID != "" {
			order = &o
		}
		if err := ValidateEvent(reg, es[i], order); (err == nil) != want {
			t.Errorf("event %d %s: %v", i, es[i].EventType, err)
		}
		// The M1 rules refuse every apa.pay.v1 event.
		var mo *contracts.PurchaseOrder
		if order != nil {
			mo = &m
		}
		if reg.ValidateEvent(es[i], mo) == nil {
			t.Errorf("the M1 rule took %s", es[i].EventType)
		}
	}
	// An M1-version event carrying a v1 payload field is refused by M1.
	m1 := es[1]
	m1.Version = contracts.Version
	if reg.ValidateEvent(m1, nil) == nil {
		t.Error("M1 took delegation_id")
	}
	// E1-a: run conditions on TASK_STARTED only, valid values only; never M1's.
	run := &contracts.RunConditions{Agent: "live", Chain: "base_sepolia", Authenticator: "device"}
	started := es[0]
	started.Payload.RunConditions = run
	if err := ValidateEvent(reg, started, nil); err != nil {
		t.Errorf("TASK_STARTED with run conditions: %v", err)
	}
	bad := *run
	bad.Chain = "mainnet"
	started.Payload.RunConditions = &bad
	if ValidateEvent(reg, started, nil) == nil {
		t.Error("TASK_STARTED took chain mainnet")
	}
	bound := es[1]
	bound.Payload.RunConditions = run
	if ValidateEvent(reg, bound, nil) == nil {
		t.Error("DELEGATION_BOUND took run conditions")
	}
	// Gap 4: a delegation confirmed by the person's device names the Signer's confirmation; nothing else does.
	byDevice := es[1]
	byDevice.Payload.AuthorizationMethod, byDevice.Payload.SignerApprovalID = "webauthn", "dconf-1"
	if err := ValidateEvent(reg, byDevice, nil); err != nil {
		t.Errorf("DELEGATION_BOUND confirmed by device: %v", err)
	}
	for _, c := range [][2]string{{"webauthn", ""}, {"simulated", "dconf-1"}, {"simulated", ""}, {"", "dconf-1"}} {
		x := es[1]
		x.Payload.AuthorizationMethod, x.Payload.SignerApprovalID = c[0], c[1]
		if ValidateEvent(reg, x, nil) == nil {
			t.Errorf("DELEGATION_BOUND took method %q with Signer %q", c[0], c[1])
		}
	}
	started.Payload.RunConditions, started.Version = run, contracts.Version
	if reg.ValidateEvent(started, nil) == nil {
		t.Error("M1 took run conditions")
	}
}

// A2: a decision verified by the Signer is recorded with method webauthn
// and the Signer's approval ID, and its own reason; neither mixes with
// the simulated form.
func TestWebAuthnEventShape(t *testing.T) {
	_, o := base(t)
	var cfg contracts.RegistryConfig
	fixture(t, "testdata/contracts/registry.json", &cfg)
	reg, err := contracts.NewRegistry(cfg)
	if err != nil {
		t.Fatal(err)
	}
	sid := SessionID(o.Intent.TaskID, o.Intent.PurchaseOrderID)
	approved := func(reason, method, signer string) contracts.EventPayload {
		return contracts.EventPayload{Decision: decision(t, o, "approved", reason), ApprovalSessionID: sid, AuthorizationMethod: method, SignerApprovalID: signer}
	}
	for _, c := range []struct {
		name string
		typ  string
		p    contracts.EventPayload
		ok   bool
	}{
		{"webauthn approved", "APPROVED", approved("WEBAUTHN_APPROVAL", "webauthn", "hap-1"), true},
		{"webauthn without its approval", "APPROVED", approved("WEBAUTHN_APPROVAL", "webauthn", ""), false},
		{"webauthn with the simulated reason", "APPROVED", approved("SIMULATED_APPROVAL", "webauthn", "hap-1"), false},
		{"simulated with a Signer approval", "APPROVED", approved("SIMULATED_APPROVAL", "simulated", "hap-1"), false},
		{"simulated with the webauthn reason", "APPROVED", approved("WEBAUTHN_APPROVAL", "simulated", ""), false},
		{"other method", "APPROVED", approved("WEBAUTHN_APPROVAL", "voice", "hap-1"), false},
		{"webauthn denied", "APPROVAL_DENIED", contracts.EventPayload{Decision: decision(t, o, "denied", "APPROVER_DENIED"), ApprovalSessionID: sid, AuthorizationMethod: "webauthn", SignerApprovalID: "hap-1"}, true},
		{"webauthn grant", "GRANT_ISSUED", contracts.EventPayload{ApprovalSessionID: sid, GrantID: GrantID(sid), AuthorizationMethod: "webauthn", SignerApprovalID: "hap-1"}, true},
		{"webauthn grant without its approval", "GRANT_ISSUED", contracts.EventPayload{ApprovalSessionID: sid, GrantID: GrantID(sid), AuthorizationMethod: "webauthn"}, false},
		{"a Signer approval elsewhere", "APPROVAL_SESSION_OPENED", contracts.EventPayload{ApprovalSessionID: sid, SignerApprovalID: "hap-1"}, false},
	} {
		es := chainOf(t, o, step{"TASK_STARTED", contracts.EventPayload{}, false}, step{"DELEGATION_BOUND", contracts.EventPayload{DelegationID: "dlg-1"}, false}, step{c.typ, c.p, true})
		if err := ValidateEvent(reg, es[2], &o); (err == nil) != c.ok {
			t.Errorf("%s: %v", c.name, err)
		}
	}
	// The M1 rules refuse signer_approval_id on an M1 event.
	m1 := chainOf(t, o, step{"TASK_STARTED", contracts.EventPayload{SignerApprovalID: "hap-1"}, false})[0]
	m1.Version = contracts.Version
	if reg.ValidateEvent(m1, nil) == nil {
		t.Error("M1 took signer_approval_id")
	}
}

// V1: one voice outcome per open session, just before the closure it leads
// to; a failed voice, and only it, closes the session denied for it.
func TestVoiceEventOrder(t *testing.T) {
	_, o := base(t)
	sid := SessionID(o.Intent.TaskID, o.Intent.PurchaseOrderID)
	start := []step{{"TASK_STARTED", contracts.EventPayload{}, false}, {"DELEGATION_BOUND", contracts.EventPayload{DelegationID: "dlg-1"}, false},
		{"PURCHASE_PROPOSED", contracts.EventPayload{}, true}, {"POLICY_DECIDED", contracts.EventPayload{Decision: decision(t, o, "pending", "HUMAN_APPROVAL_REQUIRED")}, true},
		{"APPROVAL_SESSION_OPENED", contracts.EventPayload{ApprovalSessionID: sid}, true}}
	verified := step{"VOICE_CHALLENGE_VERIFIED", contracts.EventPayload{ApprovalSessionID: sid}, true}
	unavailable := step{"VOICE_UNAVAILABLE", contracts.EventPayload{ApprovalSessionID: sid, Code: "VOICE_TIMEOUT"}, true}
	failed := step{"VOICE_CHALLENGE_FAILED", contracts.EventPayload{ApprovalSessionID: sid, Code: "VOICE_SPEAKER_MISMATCH"}, true}
	approved := step{"APPROVED", contracts.EventPayload{Decision: decision(t, o, "approved", "WEBAUTHN_APPROVAL"), ApprovalSessionID: sid, AuthorizationMethod: "webauthn", SignerApprovalID: "hap-1"}, true}
	byVoice := step{"APPROVAL_DENIED", contracts.EventPayload{Decision: decision(t, o, "denied", "VOICE_CHALLENGE_FAILED"), ApprovalSessionID: sid}, true}
	byApprover := step{"APPROVAL_DENIED", contracts.EventPayload{Decision: decision(t, o, "denied", "APPROVER_DENIED"), ApprovalSessionID: sid, AuthorizationMethod: "webauthn", SignerApprovalID: "hap-1"}, true}
	expired := step{"APPROVAL_EXPIRED", contracts.EventPayload{Decision: decision(t, o, "expired", "APPROVAL_TIMEOUT"), ApprovalSessionID: sid}, true}
	with := func(s ...step) []step { return append(append([]step{}, start...), s...) }
	for name, c := range map[string]struct {
		steps []step
		ok    bool
	}{
		"verified, approved":              {with(verified, approved), true},
		"unavailable, approved":           {with(unavailable, approved), true},
		"failed, denied for it":           {with(failed, byVoice), true},
		"failed, then approved":           {with(failed, approved), false},
		"failed, denied by the approver":  {with(failed, byApprover), false},
		"denied for a voice not failed":   {with(byVoice), false},
		"verified, denied for the voice":  {with(verified, byVoice), false},
		"two voice outcomes":              {with(verified, unavailable, approved), false},
		"a voice outcome, then expired":   {with(unavailable, expired), false},
		"a voice outcome after closing":   {with(approved, verified), false},
		"a voice outcome with no session": {append(append([]step{}, start[:4]...), verified), false},
	} {
		if err := ValidateEventOrder(chainOf(t, o, c.steps...)); (err == nil) != c.ok {
			t.Errorf("%s: %v", name, err)
		}
	}
}

func TestVoiceEventShape(t *testing.T) {
	_, o := base(t)
	var cfg contracts.RegistryConfig
	fixture(t, "testdata/contracts/registry.json", &cfg)
	reg, err := contracts.NewRegistry(cfg)
	if err != nil {
		t.Fatal(err)
	}
	sid := SessionID(o.Intent.TaskID, o.Intent.PurchaseOrderID)
	for _, c := range []struct {
		name, typ string
		p         contracts.EventPayload
		ok        bool
	}{
		{"verified", "VOICE_CHALLENGE_VERIFIED", contracts.EventPayload{ApprovalSessionID: sid}, true},
		{"verified with a code", "VOICE_CHALLENGE_VERIFIED", contracts.EventPayload{ApprovalSessionID: sid, Code: "VOICE_TIMEOUT"}, false},
		{"failed with its code", "VOICE_CHALLENGE_FAILED", contracts.EventPayload{ApprovalSessionID: sid, Code: "VOICE_REPLAY"}, true},
		{"failed with an unavailable code", "VOICE_CHALLENGE_FAILED", contracts.EventPayload{ApprovalSessionID: sid, Code: "VOICE_TIMEOUT"}, false},
		{"unavailable with its code", "VOICE_UNAVAILABLE", contracts.EventPayload{ApprovalSessionID: sid, Code: "VOICE_DEVICE_UNAVAILABLE"}, true},
		{"unavailable with a failure code", "VOICE_UNAVAILABLE", contracts.EventPayload{ApprovalSessionID: sid, Code: "VOICE_PHRASE_MISMATCH"}, false},
		{"unavailable with no code", "VOICE_UNAVAILABLE", contracts.EventPayload{ApprovalSessionID: sid}, false},
		{"denied for the voice, no method", "APPROVAL_DENIED", contracts.EventPayload{Decision: decision(t, o, "denied", "VOICE_CHALLENGE_FAILED"), ApprovalSessionID: sid}, true},
		{"denied for the voice, with a method", "APPROVAL_DENIED", contracts.EventPayload{Decision: decision(t, o, "denied", "VOICE_CHALLENGE_FAILED"), ApprovalSessionID: sid, AuthorizationMethod: "webauthn", SignerApprovalID: "hap-1"}, false},
	} {
		es := chainOf(t, o, step{"TASK_STARTED", contracts.EventPayload{}, false}, step{"DELEGATION_BOUND", contracts.EventPayload{DelegationID: "dlg-1"}, false}, step{c.typ, c.p, true})
		if err := ValidateEvent(reg, es[2], &o); (err == nil) != c.ok {
			t.Errorf("%s: %v", c.name, err)
		}
	}
}

// The person's revocation (docs/commercial-gap4.md 7): one, of the bound
// delegation, by device; nothing proposed or requested after it, a payment
// already requested carried through, and it may follow the task's end.
func TestRevocation(t *testing.T) {
	_, o := base(t)
	var cfg contracts.RegistryConfig
	fixture(t, "testdata/contracts/registry.json", &cfg)
	reg, err := contracts.NewRegistry(cfg)
	if err != nil {
		t.Fatal(err)
	}
	rev := func(id, signer string) step {
		return step{"DELEGATION_REVOKED", contracts.EventPayload{DelegationID: id, AuthorizationMethod: "webauthn", SignerApprovalID: signer}, false}
	}
	start := []step{{"TASK_STARTED", contracts.EventPayload{}, false}, {"DELEGATION_BOUND", contracts.EventPayload{DelegationID: "dlg-1"}, false}}
	proposed := step{"PURCHASE_PROPOSED", contracts.EventPayload{}, true}
	cancelled := step{"TASK_CANCELLED", contracts.EventPayload{Code: "HOST_CANCELLED"}, false}
	for name, c := range map[string]struct {
		steps []step
		ok    bool
	}{
		"revoked, then cancelled":       {append(append([]step{}, start...), rev("dlg-1", "hap-1"), cancelled), true},
		"cancelled, then revoked":       {append(append([]step{}, start...), cancelled, rev("dlg-1", "hap-1")), true},
		"a proposal after it":           {append(append([]step{}, start...), rev("dlg-1", "hap-1"), proposed), false},
		"another delegation's":          {append(append([]step{}, start...), rev("dlg-2", "hap-1")), false},
		"twice":                         {append(append([]step{}, start...), rev("dlg-1", "hap-1"), rev("dlg-1", "hap-2")), false},
		"before the delegation's bound": {[]step{start[0], rev("dlg-1", "hap-1")}, false},
	} {
		if err := ValidateEventOrder(chainOf(t, o, c.steps...)); (err == nil) != c.ok {
			t.Errorf("%s: %v", name, err)
		}
	}
	if !Ended(chainOf(t, o, append(append([]step{}, start...), rev("dlg-1", "hap-1"))...)) {
		t.Error("a revoked task still takes purchases")
	}
	es := chainOf(t, o, append(append([]step{}, start...), rev("dlg-1", "hap-1"))...)
	if err := ValidateEvent(reg, es[2], nil); err != nil {
		t.Errorf("a revocation: %v", err)
	}
	for _, c := range [][2]string{{"webauthn", ""}, {"simulated", "hap-1"}, {"", "hap-1"}} {
		x := es[2]
		x.Payload.AuthorizationMethod, x.Payload.SignerApprovalID = c[0], c[1]
		if ValidateEvent(reg, x, nil) == nil {
			t.Errorf("DELEGATION_REVOKED took method %q with Signer %q", c[0], c[1])
		}
	}
}
