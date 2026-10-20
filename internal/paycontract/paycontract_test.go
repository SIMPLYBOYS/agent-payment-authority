package paycontract

import (
	"encoding/json"
	"os"
	"strings"
	"testing"

	"github.com/SIMPLYBOYS/agent-payment-authority/internal/contracts"
)

func fixture(t *testing.T, path string, dst any) {
	t.Helper()
	raw, err := os.ReadFile("../../" + path)
	if err != nil {
		t.Fatal(err)
	}
	if err := contracts.Decode(raw, dst); err != nil {
		t.Fatal(err)
	}
}

// fromM1 is the M1 fixture purchase's business fields as an apa.pay.v1
// order: the same identity, resource, quote, payee, amount, window and
// nonce, under this version's hashes.
func fromM1(t *testing.T, m contracts.PurchaseOrder) PurchaseOrder {
	t.Helper()
	i := m.Intent
	rh, err := ResourceHash(i.HTTPMethod, i.ResourceURL)
	if err != nil {
		t.Fatal(err)
	}
	qh, err := RequirementHash(m.Quote)
	if err != nil {
		t.Fatal(err)
	}
	return PurchaseOrder{Version: Version, RequestID: m.RequestID, Quote: m.Quote,
		Intent: Intent{Version: Version, Domain: Domain, ConsumerID: i.ConsumerID, AgentID: i.AgentID, WalletID: i.WalletID, TaskID: i.TaskID,
			PurchaseOrderID: i.PurchaseOrderID, ResourceHash: rh, HTTPMethod: i.HTTPMethod, ResourceURL: i.ResourceURL, X402Version: i.X402Version,
			Scheme: i.Scheme, PaymentRequirementHash: qh},
		PaymentBinding: PaymentBinding{Method: "eip3009", ChainID: i.ChainID, AssetContract: i.AssetContract, From: i.From, PayTo: i.PayTo,
			Amount: i.Amount, ValidAfter: i.ValidAfter, ValidBefore: i.ValidBefore, Nonce: i.EIP3009Nonce}}
}

func base(t *testing.T) (contracts.PurchaseOrder, PurchaseOrder) {
	t.Helper()
	var m contracts.PurchaseOrder
	fixture(t, "testdata/contracts/purchase-input.json", &m)
	return m, fromM1(t, m)
}

func mustHash(t *testing.T, p PurchaseOrder) string {
	t.Helper()
	h, err := p.IntentHash()
	if err != nil {
		t.Fatal(err)
	}
	return h
}

// Golden vectors: values this implementation computes for the M1 fixture
// purchase as an apa.pay.v1 order (frozen at P1-a; a change needs a new
// version, never an edit here). They are this implementation's own, not an
// independent second implementation.
func TestGoldenVectors(t *testing.T) {
	_, p := base(t)
	key, _ := p.RetryKey()
	g, _ := GenesisHash(p.Intent.TaskID)
	got := map[string]string{
		"resource_hash":            p.Intent.ResourceHash,
		"payment_requirement_hash": p.Intent.PaymentRequirementHash,
		"intent_hash":              mustHash(t, p),
		"retry_key":                key,
		"genesis_task_a":           g,
		"session_id":               SessionID("task-a", p.Intent.PurchaseOrderID),
		"grant_id":                 GrantID(SessionID("task-a", p.Intent.PurchaseOrderID)),
		"delegation_id":            DelegationID("dlg-req-000000000001"),
	}
	want := map[string]string{
		"resource_hash":            "sha256:7d70211ab419bddf3ac4b164d0897a9edb1929e2f2d2b190cb2286bfdf308001",
		"payment_requirement_hash": "sha256:f5ec88c79a7fa930dea6b5607940b280773f768a65677413b5f2101053814d94",
		"intent_hash":              "sha256:bd300e71a57b6fe72413d6bbe1662acb959209d140a24977ac98059f54b01ba7",
		"retry_key":                "sha256:16cffefd3583ffc5c2c07f787efee06794cb61e3a0e4ed673d4c8c5ab753d2c1",
		"genesis_task_a":           "sha256:b3b4741fbb19ca947072f6ce540e3bcc36e67a60500a01143cd7ca57324b88ae",
		"session_id":               "aps-df5e6249e0ca9bb1c45a708b8abebfab",
		"grant_id":                 "grant-15ccfc477e3665e868811cb6fd041b60",
		"delegation_id":            "dlg-e20f699b2aa0984b4b3eae051ce0670d",
	}
	for k, v := range got {
		if want[k] != v {
			t.Errorf("%s = %q, frozen %q", k, v, want[k])
		}
	}
}

// The same business fields through the two contracts: each its own
// validation and digest; neither decoder takes the other's order.
func TestSameFieldsTwoContracts(t *testing.T) {
	m, p := base(t)
	mh, err := m.Intent.Hash()
	if err != nil {
		t.Fatal(err)
	}
	if vh := mustHash(t, p); vh == mh || p.Intent.ResourceHash == m.Intent.ResourceHash || p.Intent.PaymentRequirementHash == m.Intent.PaymentRequirementHash {
		t.Fatalf("digests are shared: v1 %s M1 %s", vh, mh)
	}
	rawV1, _ := json.Marshal(p)
	rawM1, _ := json.Marshal(m)
	var asM1 contracts.PurchaseOrder
	if err := contracts.Decode(rawV1, &asM1); err == nil {
		t.Fatal("the M1 decoder took an apa.pay.v1 order")
	}
	var asV1 PurchaseOrder
	if err := contracts.Decode(rawM1, &asV1); err == nil {
		t.Fatal("the v1 decoder took an M1 order")
	}
}

// Every payment binding field is in the intent hash; the ones the quote
// fixes cannot change alone; a method other than EIP-3009 is refused.
func TestBindingInTheHash(t *testing.T) {
	_, p := base(t)
	h := mustHash(t, p)
	for name, edit := range map[string]func(*PaymentBinding){
		"nonce":        func(b *PaymentBinding) { b.Nonce = "0x" + strings.Repeat("7", 64) },
		"valid_after":  func(b *PaymentBinding) { b.ValidAfter++ },
		"valid_before": func(b *PaymentBinding) { b.ValidBefore++ },
	} {
		q := p
		edit(&q.PaymentBinding)
		if g := mustHash(t, q); g == h {
			t.Errorf("%s: the intent hash did not change", name)
		}
	}
	for name, edit := range map[string]func(*PaymentBinding){
		"pay_to": func(b *PaymentBinding) { b.PayTo = "0x" + strings.Repeat("9", 40) },
		"amount": func(b *PaymentBinding) { b.Amount = "500001" },
		"asset":  func(b *PaymentBinding) { b.AssetContract = "0x" + strings.Repeat("5", 40) },
		"chain":  func(b *PaymentBinding) { b.ChainID = "8453" },
	} {
		q := p
		edit(&q.PaymentBinding)
		if err := q.Validate(); Code(err) != "INVALID_PURCHASE" {
			t.Errorf("%s alone (the quote says otherwise): %v", name, err)
		}
	}
	q := p
	q.PaymentBinding.Method = "permit2"
	if err := q.Validate(); Code(err) != "UNSUPPORTED_TRANSFER_METHOD" {
		t.Errorf("permit2: %v", err)
	}
	q = p
	q.Intent.PurchaseOrderID = "po-other"
	if mustHash(t, q) == h {
		t.Error("the business intent is not in the hash")
	}
}

func TestDecisionRule(t *testing.T) {
	_, p := base(t)
	h := mustHash(t, p)
	d := func(verdict, approval, reason string) contracts.PolicyDecision {
		return contracts.PolicyDecision{Version: Version, IntentHash: h, PolicyVerdict: verdict, ApprovalStatus: approval, PaymentStatus: PaymentNotEnabled, Reason: reason}
	}
	for _, ok := range []contracts.PolicyDecision{d("Ask", "pending", "HUMAN_APPROVAL_REQUIRED"), d("Ask", "approved", "SIMULATED_APPROVAL"),
		d("Ask", "denied", "APPROVER_DENIED"), d("Ask", "superseded", "QUOTE_CHANGED"), d("Ask", "expired", "APPROVAL_TIMEOUT"),
		d("Ask", "withdrawn", "HOST_CANCELLED"), d("Allow", "not_requested", "WITHIN_ALLOW_MAX"), d("Deny", "not_requested", "MERCHANT_NOT_ALLOWED")} {
		if err := ValidateDecision(ok); err != nil {
			t.Errorf("%+v: %v", ok, err)
		}
		if ok.Validate() == nil {
			t.Errorf("the M1 rule took %+v", ok)
		}
	}
	bad := []contracts.PolicyDecision{d("Allow", "not_requested", "PAYMENT_CAPABILITY_UNAVAILABLE"), d("Ask", "approved", "HUMAN_APPROVAL_REQUIRED"), d("Allow", "approved", "SIMULATED_APPROVAL")}
	paid := d("Ask", "approved", "SIMULATED_APPROVAL")
	paid.PaymentStatus = "settled"
	m1 := d("Ask", "pending", "HUMAN_APPROVAL_REQUIRED")
	m1.Version, m1.PaymentStatus = contracts.Version, "unavailable"
	for _, x := range append(bad, paid, m1) {
		if ValidateDecision(x) == nil {
			t.Errorf("took %+v", x)
		}
	}
}

func TestGrantAndDelegation(t *testing.T) {
	sid := SessionID("task-a", "po-1")
	g := AuthorizationGrant{Version: Version, JTI: GrantID(sid), IntentHash: "sha256:" + strings.Repeat("a", 64), ApprovalSessionID: sid, Decision: "approve",
		AuthorizationMethod: "simulated", Issuer: Issuer, Audience: "signer", ApproverID: "simulated:consumer-a:host-x", IssuedAt: 100, ExpiresAt: 160}
	if err := g.Validate(); err != nil {
		t.Fatal(err)
	}
	for name, edit := range map[string]func(*AuthorizationGrant){
		"webauthn":    func(x *AuthorizationGrant) { x.AuthorizationMethod = "webauthn" },
		"another jti": func(x *AuthorizationGrant) { x.JTI = GrantID("aps-other") },
		"audience":    func(x *AuthorizationGrant) { x.Audience = "merchant" },
		"expiry":      func(x *AuthorizationGrant) { x.ExpiresAt = x.IssuedAt },
	} {
		x := g
		edit(&x)
		if Code(x.Validate()) != "INVALID_GRANT" {
			t.Errorf("%s accepted", name)
		}
	}
	// A2: the Host's mirror of a Signer-verified approval.
	w := g
	w.AuthorizationMethod, w.Issuer, w.SignerApprovalID, w.CredentialRef, w.AMR = "webauthn", Recorder, "hap-1", "sha256:"+strings.Repeat("b", 64), []string{"webauthn"}
	w.ApproverID = "webauthn:approver-a"
	if err := w.Validate(); err != nil {
		t.Fatalf("webauthn grant: %v", err)
	}
	for name, edit := range map[string]func(*AuthorizationGrant){
		"host issuer":      func(x *AuthorizationGrant) { x.Issuer = Issuer },
		"no approval":      func(x *AuthorizationGrant) { x.SignerApprovalID = "" },
		"no credential":    func(x *AuthorizationGrant) { x.CredentialRef = "" },
		"amr":              func(x *AuthorizationGrant) { x.AMR = []string{"webauthn", "voice"} },
		"simulated mirror": func(x *AuthorizationGrant) { x.AuthorizationMethod = "simulated" },
	} {
		x := w
		edit(&x)
		if Code(x.Validate()) != "INVALID_GRANT" {
			t.Errorf("webauthn %s accepted", name)
		}
	}
	// V1: the voice outcome a webauthn grant records.
	for name, c := range map[string]struct {
		voice, reason string
		amr           []string
		ok            bool
	}{
		"verified":                  {"verified", "", []string{"webauthn", "voice"}, true},
		"verified, amr without it":  {"verified", "", []string{"webauthn"}, false},
		"unavailable with its code": {"unavailable", "VOICE_TIMEOUT", []string{"webauthn"}, true},
		"unavailable, amr with it":  {"unavailable", "VOICE_TIMEOUT", []string{"webauthn", "voice"}, false},
		"unavailable without code":  {"unavailable", "", []string{"webauthn"}, false},
		"unavailable, a failure":    {"unavailable", "VOICE_REPLAY", []string{"webauthn"}, false},
		"verified with a reason":    {"verified", "VOICE_TIMEOUT", []string{"webauthn", "voice"}, false},
		"failed":                    {"failed", "VOICE_REPLAY", []string{"webauthn"}, false},
	} {
		x := w
		x.Voice, x.VoiceReason, x.AMR = c.voice, c.reason, c.amr
		if err := x.Validate(); (err == nil) != c.ok {
			t.Errorf("voice %s: %v", name, err)
		}
	}
	if sv := g; true {
		sv.Voice = "verified"
		if Code(sv.Validate()) != "INVALID_GRANT" {
			t.Error("a simulated grant with a voice accepted")
		}
	}
	d := Delegation{Version: Version, DelegationID: DelegationID("r"), ConsumerID: "consumer-a", Scenario: "pay-a", Purpose: "x", Merchants: []string{"0x" + strings.Repeat("3", 40)},
		AssetID: "fixture-usdc", TaskBudget: "5000000", AllowMax: "100000", ApprovalDeadlineSeconds: 60, ConfirmedBy: "simulated:consumer-a:host-x", ConfirmMethod: "simulated", ConfirmedAt: 100, ExpiresAt: 200}
	if err := d.Validate(); err != nil {
		t.Fatal(err)
	}
	d.AllowMax = "5000001"
	if Code(d.Validate()) != "INVALID_DELEGATION" {
		t.Error("allow_max above the budget accepted")
	}
}

// A 402 writing the asset and payTo in EIP-55's mixed case is the same
// purchase (B5-D1, docs/commercial-p2b.md): accepted, the binding
// lowercase; another address, another prefix or a non-hex character in
// either is still refused.
func TestQuoteAddressInEitherCase(t *testing.T) {
	_, p := base(t)
	sp, _ := SpecOf(Version)
	const usdc, usdc55 = "0x036cbd53842c5426634e7929541ec2318f3dcf7e", "0x036CbD53842c5426634e7929541eC2318f3dCF7e" // as test402 writes it
	const shop, shopMixed = "0xabcdefabcdefabcdefabcdefabcdefabcdefabcd", "0xAbCdEfAbCdEfAbCdEfAbCdEfAbCdEfAbCdEfAbCd"
	// order: the binding's asset and payTo, and the quote's as written.
	order := func(asset, payTo, quotedAsset, quotedPayTo string) PurchaseOrder {
		q := p
		q.PaymentBinding.AssetContract, q.PaymentBinding.PayTo = asset, payTo
		var req map[string]any
		json.Unmarshal(p.Quote.SelectedRequirement, &req)
		req["asset"], req["payTo"] = quotedAsset, quotedPayTo
		q.Quote.SelectedRequirement, _ = json.Marshal(req)
		h, err := sp.RequirementHash(q.Quote)
		if err != nil {
			t.Fatal(err)
		}
		q.Intent.PaymentRequirementHash = h
		return q
	}
	if err := order(usdc, shop, usdc, shop).Validate(); err != nil {
		t.Fatalf("lowercase as before: %v", err)
	}
	if err := order(usdc, shop, usdc55, shopMixed).Validate(); err != nil {
		t.Fatalf("the quote in EIP-55's case: %v", err)
	}
	for name, q := range map[string]PurchaseOrder{
		"another asset":          order(usdc, shop, "0x036CbD53842c5426634e7929541eC2318f3dCF7f", shop),
		"another asset, non-hex": order(usdc, shop, "0x036CbD53842c5426634e7929541eC2318f3dCF7g", shop),
		"a 0X payTo":             order(usdc, shop, usdc, "0X"+shop[2:]),
		"a mixed-case binding":   order(usdc55, shop, usdc55, shop),
	} {
		if err := q.Validate(); Code(err) != "INVALID_PURCHASE" {
			t.Errorf("%s: %v", name, err)
		}
	}
}
