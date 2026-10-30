package paypolicy

import (
	"os"
	"testing"

	"github.com/SIMPLYBOYS/agent-payment-authority/internal/contracts"
	"github.com/SIMPLYBOYS/agent-payment-authority/internal/paycontract"
	"github.com/SIMPLYBOYS/agent-payment-authority/internal/policy"
)

func load(t *testing.T, path string, dst any) {
	t.Helper()
	raw, err := os.ReadFile("../../" + path)
	if err != nil {
		t.Fatal(err)
	}
	if err := contracts.Decode(raw, dst); err != nil {
		t.Fatal(err)
	}
}

func v1Of(t *testing.T, m contracts.PurchaseOrder) paycontract.PurchaseOrder {
	t.Helper()
	i := m.Intent
	rh, _ := paycontract.ResourceHash(i.HTTPMethod, i.ResourceURL)
	qh, err := paycontract.RequirementHash(m.Quote)
	if err != nil {
		t.Fatal(err)
	}
	return paycontract.PurchaseOrder{Version: paycontract.Version, RequestID: m.RequestID, Quote: m.Quote,
		Intent: paycontract.Intent{Version: paycontract.Version, Domain: paycontract.Domain, ConsumerID: i.ConsumerID, AgentID: i.AgentID, WalletID: i.WalletID,
			TaskID: i.TaskID, PurchaseOrderID: i.PurchaseOrderID, ResourceHash: rh, HTTPMethod: i.HTTPMethod, ResourceURL: i.ResourceURL, X402Version: i.X402Version,
			Scheme: i.Scheme, PaymentRequirementHash: qh},
		PaymentBinding: paycontract.PaymentBinding{Method: "eip3009", ChainID: i.ChainID, AssetContract: i.AssetContract, From: i.From, PayTo: i.PayTo,
			Amount: i.Amount, ValidAfter: i.ValidAfter, ValidBefore: i.ValidBefore, Nonce: i.EIP3009Nonce}}
}

// The same business fields through both entries: each validates and hashes
// by its own contract; the shared rules give the same kind and the same
// reason, each written in its own contract's words.
func TestSameFieldsBothEntries(t *testing.T) {
	var cfg contracts.RegistryConfig
	load(t, "testdata/contracts/registry.json", &cfg)
	reg, err := contracts.NewRegistry(cfg)
	if err != nil {
		t.Fatal(err)
	}
	var m contracts.PurchaseOrder
	load(t, "testdata/contracts/purchase-input.json", &m)
	var prof policy.Profile
	load(t, "testdata/policy/m1-demo-profile.json", &prof)
	sess := contracts.Session{ConsumerID: m.Intent.ConsumerID, AgentID: m.Intent.AgentID, TaskID: m.Intent.TaskID}
	cred := policy.Credential{ConsumerID: sess.ConsumerID, ValidUntil: 1790726450 + 86400, EvidenceMode: "fixture"}
	for _, c := range []struct {
		name, remaining, m1Verdict, m1Reason, v1Verdict, v1Reason string
	}{
		{"ask", "5000000", "Ask", "HUMAN_APPROVAL_REQUIRED", "Ask", "HUMAN_APPROVAL_REQUIRED"},
		{"budget", "1", "Deny", "INSUFFICIENT_BUDGET", "Deny", "INSUFFICIENT_BUDGET"},
	} {
		in := policy.Input{Session: sess, Purchase: m, TaskStatus: "running", Profile: &prof, Credential: &cred, Budget: policy.Budget{TaskID: sess.TaskID, Remaining: contracts.Amount(c.remaining)}, Now: 1790726450}
		d1, err := policy.Decide(reg, in)
		if err != nil {
			t.Fatal(err)
		}
		v := v1Of(t, m)
		d2, err := Decide(reg, Input{Session: sess, Purchase: v, ContractVersion: paycontract.Version, TaskStatus: "running", Profile: &prof, Credential: &cred, Budget: in.Budget, Now: in.Now})
		if err != nil {
			t.Fatal(err)
		}
		if d1.PolicyVerdict != c.m1Verdict || d1.Reason != c.m1Reason || d2.PolicyVerdict != c.v1Verdict || d2.Reason != c.v1Reason || d1.IntentHash == d2.IntentHash ||
			d1.Version != contracts.Version || d2.Version != paycontract.Version || d1.PaymentStatus != "unavailable" || d2.PaymentStatus != "not_enabled" {
			t.Errorf("%s: M1 %+v / v1 %+v", c.name, d1, d2)
		}
	}
	// Within allow_max: M1's Allow says it cannot pay; v1's Allow is in range, not a payment.
	small := m
	in := policy.Input{Session: sess, Purchase: small, TaskStatus: "running", Profile: &prof, Credential: &cred, Budget: policy.Budget{TaskID: sess.TaskID, Remaining: "5000000"}, Now: 1790726450}
	p := prof
	p.AllowMax = "500000"
	in.Profile = &p
	d1, _ := policy.Decide(reg, in)
	d2, _ := Decide(reg, Input{Session: sess, Purchase: v1Of(t, m), ContractVersion: paycontract.Version, TaskStatus: "running", Profile: &p, Credential: &cred, Budget: in.Budget, Now: in.Now})
	if d1.PolicyVerdict != "Allow" || d1.Reason != "PAYMENT_CAPABILITY_UNAVAILABLE" || d2.PolicyVerdict != "Allow" || d2.Reason != "WITHIN_ALLOW_MAX" || d2.PaymentStatus != "not_enabled" {
		t.Errorf("Allow: M1 %+v / v1 %+v", d1, d2)
	}
	// The entry is the task's version: an M1 task's version refuses the v1 entry.
	if _, err := Decide(reg, Input{Session: sess, Purchase: v1Of(t, m), ContractVersion: contracts.Version, TaskStatus: "running", Profile: &prof, Credential: &cred, Budget: in.Budget, Now: in.Now}); paycontract.Code(err) != "CONTRACT_VERSION_MISMATCH" {
		t.Errorf("an M1 task through the v1 entry: %v", err)
	}
	// C2a review should-fix 3: a v2 purchase in a v1 task is not decided.
	v2 := v1Of(t, m)
	sp, _ := paycontract.SpecOf(paycontract.V2)
	v2.Version, v2.Intent.Version, v2.Intent.Domain = paycontract.V2, paycontract.V2, paycontract.DomainV2
	v2.Intent.ResourceHash, _ = sp.ResourceHash(v2.Intent.HTTPMethod, v2.Intent.ResourceURL)
	v2.Intent.PaymentRequirementHash, _ = sp.RequirementHash(v2.Quote)
	if err := v2.Validate(); err != nil {
		t.Fatal(err)
	}
	if _, err := Decide(reg, Input{Session: sess, Purchase: v2, ContractVersion: paycontract.Version, TaskStatus: "running", Profile: &prof, Credential: &cred, Budget: in.Budget, Now: in.Now}); paycontract.Code(err) != "CONTRACT_VERSION_MISMATCH" {
		t.Errorf("a v2 purchase in a v1 task: %v", err)
	}
}

// C2b: apa.pay.v2 decides by the verifier's record first (design 3.3).
func TestV2CredentialStep(t *testing.T) {
	var cfg contracts.RegistryConfig
	load(t, "testdata/contracts/registry.json", &cfg)
	reg, _ := contracts.NewRegistry(cfg)
	var m contracts.PurchaseOrder
	load(t, "testdata/contracts/purchase-input.json", &m)
	var prof policy.Profile
	load(t, "testdata/policy/m1-demo-profile.json", &prof)
	sess := contracts.Session{ConsumerID: m.Intent.ConsumerID, AgentID: m.Intent.AgentID, TaskID: m.Intent.TaskID}
	v2 := v1Of(t, m)
	sp, _ := paycontract.SpecOf(paycontract.V2)
	v2.Version, v2.Intent.Version, v2.Intent.Domain = paycontract.V2, paycontract.V2, paycontract.DomainV2
	v2.Intent.ResourceHash, _ = sp.ResourceHash(v2.Intent.HTTPMethod, v2.Intent.ResourceURL)
	v2.Intent.PaymentRequirementHash, _ = sp.RequirementHash(v2.Quote)
	small := prof
	small.AllowMax = "500000" // within allow_max: Allow when the credential is valid
	budget := policy.Budget{TaskID: sess.TaskID, Remaining: "5000000"}
	in := func(c *contracts.CredentialCheck) Input {
		return Input{Session: sess, Purchase: v2, ContractVersion: paycontract.V2, TaskStatus: "running", Profile: &small, Check: c, Budget: budget, Now: 1790726450}
	}
	valid := &contracts.CredentialCheck{Result: "valid", Expiry: 1790726450 + 3600, Verifier: "host-1"}
	for _, c := range []struct {
		check           *contracts.CredentialCheck
		verdict, reason string
	}{
		{valid, "Allow", "WITHIN_ALLOW_MAX"},
		{&contracts.CredentialCheck{Result: "invalid", Detail: "PROOF_INVALID", Verifier: "host-1"}, "Deny", "CREDENTIAL_INVALID"},
		{&contracts.CredentialCheck{Result: "revoked", Verifier: "host-1"}, "Deny", "CREDENTIAL_REVOKED"},
		{&contracts.CredentialCheck{Result: "valid", Detail: "CLAIM_UNMET", Expiry: 1790726450 + 3600, Verifier: "host-1"}, "Deny", "CREDENTIAL_CLAIM_UNMET"}, // the venue matrix
		{&contracts.CredentialCheck{Result: "status_unavailable", Verifier: "host-1"}, "Deny", "CREDENTIAL_STATUS_UNAVAILABLE"},
		{&contracts.CredentialCheck{Result: "expired", Verifier: "host-1"}, "Deny", "AUTHORIZATION_EXPIRED"},
		{&contracts.CredentialCheck{Result: "absent", Verifier: "host-1"}, "Deny", "CONFIG_MISSING"},
	} {
		d, err := Decide(reg, in(c.check))
		if err != nil || d.PolicyVerdict != c.verdict || d.Reason != c.reason || d.Version != paycontract.V2 {
			t.Errorf("%s: %+v %v", c.check.Result, d, err)
		}
	}
	bad := in(valid)
	bad.Credential = &policy.Credential{ConsumerID: sess.ConsumerID, ValidUntil: 1790726450 + 86400, EvidenceMode: "fixture"}
	if _, err := Decide(reg, bad); err == nil {
		t.Error("a v2 decision took a fixture credential")
	}
	if _, err := Decide(reg, in(nil)); err == nil {
		t.Error("a v2 decision without the verifier's record")
	}
	v1in := Input{Session: sess, Purchase: v1Of(t, m), ContractVersion: paycontract.Version, TaskStatus: "running", Profile: &small, Check: valid, Budget: budget, Now: 1790726450}
	if _, err := Decide(reg, v1in); err == nil {
		t.Error("a v1 decision took a credential record")
	}
}
