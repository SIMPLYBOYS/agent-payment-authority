package paycontract

import (
	"strings"
	"testing"

	"github.com/SIMPLYBOYS/agent-payment-authority/internal/contracts"
)

func testRegistry(t *testing.T) *contracts.Registry {
	t.Helper()
	var cfg contracts.RegistryConfig
	fixture(t, "testdata/contracts/registry.json", &cfg)
	r, err := contracts.NewRegistry(cfg)
	if err != nil {
		t.Fatal(err)
	}
	return r
}

// C2a review should-fix 1: an event's order, decision and grant are of the
// event's own version.
func TestEventCrossVersion(t *testing.T) {
	r := testRegistry(t)
	_, o1 := base(t)
	o2 := toV2(t, o1)
	es := chainOf(t, o2, step{"PURCHASE_PROPOSED", contracts.EventPayload{}, true})
	es[0].Version = Version // a v1 event naming the v2 order
	if err := ValidateEvent(r, es[0], &o2); err == nil {
		t.Error("a v1 event took a v2 order")
	}
	e := chainOf(t, o1, step{"PURCHASE_PROPOSED", contracts.EventPayload{}, true})[0]
	e.Version = V2
	if err := ValidateEvent(r, e, &o1); err == nil {
		t.Error("a v2 event took a v1 order")
	}
	d := decision(t, o1, "pending", "HUMAN_APPROVAL_REQUIRED")
	d.Version = V2
	es = chainOf(t, o1, step{"POLICY_DECIDED", contracts.EventPayload{Decision: d}, true})
	if err := ValidateEvent(r, es[0], &o1); err == nil {
		t.Error("a v1 event took a v2 decision")
	}
	sp2, _ := SpecOf(V2)
	sid := sp2.SessionID(o2.Intent.TaskID, o2.Intent.PurchaseOrderID)
	e = chainOf(t, o2, step{"GRANT_ISSUED", contracts.EventPayload{ApprovalSessionID: sid, GrantID: GrantID(sid), AuthorizationMethod: "simulated"}, true})[0]
	e.Version = V2
	if err := ValidateEvent(r, e, &o2); err == nil {
		t.Error("a v2 grant event took v1's grant ID")
	}
	e.Payload.GrantID = sp2.GrantID(sid)
	if err := ValidateEvent(r, e, &o2); err != nil {
		t.Errorf("a v2 grant event with v2's grant ID: %v", err)
	}
}

// Note 1: one request ID cannot name orders of two versions.
func TestCompareRetryAcrossVersions(t *testing.T) {
	_, o1 := base(t)
	o2 := o1
	o2.Intent.PurchaseOrderID = "po-other"
	o2.PaymentBinding.Nonce = "0x11111111111111111111111111111111111111111111111111111111111111aa"
	o2 = toV2(t, o2)
	if _, err := CompareRetry(o1, o2); Code(err) != "IDEMPOTENCY_CONFLICT" {
		t.Fatalf("v1 and v2 orders of one request: %v", err)
	}
}

// Note 2: a v2 stream of two events, hashed under v2's domain (frozen at
// C2a review).
func TestV2EventChainGolden(t *testing.T) {
	sp, _ := SpecOf(V2)
	scope := &contracts.EventScope{ConsumerID: "consumer-a", AgentID: "agent-a", WalletID: "wallet-a", TaskID: "task-a"}
	g, _ := sp.GenesisHash("task-a")
	first := contracts.AuditEvent{Version: V2, EventID: "evt-1", EventType: "TASK_STARTED", Scope: scope, Sequence: 1, PrevEventHash: g,
		EvidenceMode: "fixture", Source: "host", ObservedAt: 1790000000}
	h1, _ := EventHash(first)
	second := first
	second.EventID, second.EventType, second.Sequence, second.PrevEventHash = "evt-2", "DELEGATION_BOUND", 2, h1
	if err := ValidateEventOrder([]contracts.AuditEvent{first, second}); err != nil {
		t.Fatal(err)
	}
	h2, _ := EventHash(second)
	v1first := first
	v1first.Version = Version
	if v1h, _ := EventHash(v1first); v1h == h1 {
		t.Fatal("v1 and v2 hash an event alike")
	}
	if h1 != "sha256:52ff7d08f69d9a251a01b059a57138c246110e3905c59f782123d96cdfb176f9" || h2 != "sha256:60afbf451997321c679d662d6dfe1ae1d3870a24fbc9a879dc4804d18ea256a8" {
		t.Fatalf("v2 event hashes %s %s, frozen otherwise", h1, h2)
	}
}

func check(result, detail, challenge string) *contracts.CredentialCheck {
	c := &contracts.CredentialCheck{Result: result, Detail: detail, ChallengeID: challenge, Verifier: "host-1"}
	if result == "valid" || result == "revoked" || result == "expired" {
		c.CredentialSchema, c.Issuer, c.Holder, c.StatusIndex, c.Expiry, c.ProofSHA256 = "apa.credential.v1", "issuer-demo", "ed25519:"+strings.Repeat("a", 64), "7", 1791903600, strings.Repeat("b", 64)
		c.StatusListVersion, c.StatusListIssuedAt, c.StatusListSHA256 = 3, 1790726440, strings.Repeat("c", 64)
	}
	return c
}

func deny(t *testing.T, o PurchaseOrder, reason string) *contracts.PolicyDecision {
	return &contracts.PolicyDecision{Version: o.Version, IntentHash: mustHash(t, o), PolicyVerdict: "Deny", ApprovalStatus: "not_requested", PaymentStatus: PaymentNotEnabled, Reason: reason}
}

// C2b: one CREDENTIAL_VERIFIED per v2 purchase, between its proposal and
// its decision, and the decision follows its result (design 3.3).
func TestCredentialVerifiedOrder(t *testing.T) {
	_, o1 := base(t)
	o := toV2(t, o1)
	start := []step{{"TASK_STARTED", contracts.EventPayload{}, false}, {"DELEGATION_BOUND", contracts.EventPayload{DelegationID: "dlg-1"}, false},
		{"PURCHASE_PROPOSED", contracts.EventPayload{}, true}}
	verified := func(c *contracts.CredentialCheck) step {
		return step{"CREDENTIAL_VERIFIED", contracts.EventPayload{Credential: c}, true}
	}
	decided := func(d *contracts.PolicyDecision) step {
		return step{"POLICY_DECIDED", contracts.EventPayload{Decision: d}, true}
	}
	for name, c := range map[string]struct {
		steps []step
		ok    bool
	}{
		"valid, Ask":                          {[]step{verified(check("valid", "", "chl-1")), decided(decision(t, o, "pending", "HUMAN_APPROVAL_REQUIRED"))}, true},
		"valid, Deny by budget":               {[]step{verified(check("valid", "", "chl-1")), decided(deny(t, o, "INSUFFICIENT_BUDGET"))}, true},
		"invalid, Deny CREDENTIAL_INVALID":    {[]step{verified(check("invalid", "PROOF_INVALID", "chl-1")), decided(deny(t, o, "CREDENTIAL_INVALID"))}, true},
		"absent, Deny CONFIG_MISSING":         {[]step{verified(check("absent", "", "")), decided(deny(t, o, "CONFIG_MISSING"))}, true},
		"expired, Deny AUTHORIZATION_EXPIRED": {[]step{verified(check("expired", "", "chl-1")), decided(deny(t, o, "AUTHORIZATION_EXPIRED"))}, true},
		"no verification":                     {[]step{decided(decision(t, o, "pending", "HUMAN_APPROVAL_REQUIRED"))}, false},
		"invalid, Ask":                        {[]step{verified(check("invalid", "PROOF_INVALID", "chl-1")), decided(decision(t, o, "pending", "HUMAN_APPROVAL_REQUIRED"))}, false},
		"revoked, Deny another reason":        {[]step{verified(check("revoked", "", "chl-1")), decided(deny(t, o, "CREDENTIAL_INVALID"))}, false},
		"valid, Deny CREDENTIAL_REVOKED":      {[]step{verified(check("valid", "", "chl-1")), decided(deny(t, o, "CREDENTIAL_REVOKED"))}, false},
		"two verifications":                   {[]step{verified(check("valid", "", "chl-1")), verified(check("valid", "", "chl-1")), decided(deny(t, o, "INSUFFICIENT_BUDGET"))}, false},
	} {
		err := ValidateEventOrder(chainOf(t, o, append(append([]step{}, start...), c.steps...)...))
		if (err == nil) != c.ok {
			t.Errorf("%s: %v", name, err)
		}
	}
	// v1 never has it: neither in a stream nor as an event.
	v1 := []step{start[0], start[1], start[2], verified(check("valid", "", "chl-1")), decided(decision(t, o1, "pending", "HUMAN_APPROVAL_REQUIRED"))}
	if ValidateEventOrder(chainOf(t, o1, v1...)) == nil {
		t.Error("a v1 stream took CREDENTIAL_VERIFIED")
	}
}

// The payload field: v1 refuses it on any event (review M2); v2 takes it
// on CREDENTIAL_VERIFIED only, with a well-formed record.
func TestCredentialPayload(t *testing.T) {
	r := testRegistry(t)
	_, o1 := base(t)
	o2 := toV2(t, o1)
	e := chainOf(t, o1, step{"PURCHASE_PROPOSED", contracts.EventPayload{Credential: check("valid", "", "chl-1")}, true})[0]
	if ValidateEvent(r, e, &o1) == nil {
		t.Error("a v1 event took a credential field")
	}
	e = chainOf(t, o1, step{"CREDENTIAL_VERIFIED", contracts.EventPayload{Credential: check("valid", "", "chl-1")}, true})[0]
	if ValidateEvent(r, e, &o1) == nil {
		t.Error("a v1 CREDENTIAL_VERIFIED event")
	}
	for name, c := range map[string]struct {
		c  *contracts.CredentialCheck
		ok bool
	}{
		"valid":                         {check("valid", "", "chl-1"), true},
		"invalid":                       {check("invalid", "PROOF_INVALID", "chl-1"), true},
		"absent":                        {check("absent", "", ""), true},
		"challenge missing":             {check("invalid", "CHALLENGE_MISSING", ""), true},
		"holder material":               {check("invalid", "HOLDER_MATERIAL_UNAVAILABLE", ""), true},
		"unknown result":                {check("maybe", "", "chl-1"), false},
		"unknown detail":                {check("invalid", "SOMETHING", "chl-1"), false},
		"invalid without a challenge":   {check("invalid", "PROOF_INVALID", ""), false},
		"absent with a challenge":       {check("absent", "", "chl-1"), false},
		"valid without the status list": {func() *contracts.CredentialCheck { c := check("valid", "", "chl-1"); c.StatusListSHA256 = ""; return c }(), false},
		"invalid that established a holder": {func() *contracts.CredentialCheck {
			c := check("invalid", "PROOF_INVALID", "chl-1")
			c.Issuer = "issuer-demo"
			return c
		}(), false},
	} {
		e := chainOf(t, o2, step{"CREDENTIAL_VERIFIED", contracts.EventPayload{Credential: c.c}, true})[0]
		if err := ValidateEvent(r, e, &o2); (err == nil) != c.ok {
			t.Errorf("%s: %v", name, err)
		}
	}
	e = chainOf(t, o2, step{"PURCHASE_PROPOSED", contracts.EventPayload{Credential: check("valid", "", "chl-1")}, true})[0]
	if ValidateEvent(r, e, &o2) == nil {
		t.Error("a v2 proposal took a credential field")
	}
}
