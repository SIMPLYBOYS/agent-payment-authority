package signer

// Gap 4 (docs/commercial-gap4.md 2.1): the person's confirmation of a
// delegation, against a software authenticator (signertest). None of it is
// a device result.

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"

	"github.com/SIMPLYBOYS/agent-payment-authority/internal/contracts"
	"github.com/SIMPLYBOYS/agent-payment-authority/internal/paycontract"
	"github.com/SIMPLYBOYS/agent-payment-authority/internal/signer/signertest"
	"github.com/decred/dcrd/dcrec/secp256k1/v4"
)

// terms are a delegation's terms for consumer x ("a" or "b").
func (f *afx) terms(x, id string) json.RawMessage {
	raw, _ := json.Marshal(paycontract.DelegationTerms{Version: paycontract.Version, DelegationID: id, ConsumerID: "consumer-" + x, WalletID: "wallet-" + x,
		ChainID: signertest.Chain, AssetContract: signertest.Asset, Merchants: []string{signertest.Merchant}, Purpose: "買一份簡報",
		TaskBudget: "1500000", AllowMax: "100000", ApprovalDeadlineSeconds: 180, ValidSeconds: 1800})
	return raw
}

func (f *afx) dchallenge(req string, terms json.RawMessage) (ApprovalReply, error) {
	return f.core.DelegationChallenge(bgA, DelegationChallengeRequest{RequestID: req, Terms: terms, HostDeadline: f.now + 600})
}

func (f *afx) dverify(vid, challengeID string, terms, assertion json.RawMessage) (ApprovalReply, error) {
	return f.core.DelegationVerify(bgA, DelegationVerifyRequest{VerifyRequestID: vid, ChallengeID: challengeID, Terms: terms, Assertion: assertion})
}

func TestDelegationConfirmAndResend(t *testing.T) {
	f := newAFX(t)
	a := f.enroll("approver-a", nil)
	terms := f.terms("a", "dlg-1")
	c, err := f.dchallenge("dreq-1", terms)
	if err != nil || c.State != "issued" || c.ApprovalSessionID != "dlg-1" || c.ConsumerID != "consumer-a" || c.ApproverID != "approver-a" || c.Display != nil || len(c.Terms) == 0 ||
		c.ExpiresAt != f.now+120 || !strings.HasPrefix(c.IntentHash, "sha256:") {
		t.Fatalf("challenge %+v %v", c, err)
	}
	if again, err := f.dchallenge("dreq-1", terms); err != nil || again.ChallengeID != c.ChallengeID || string(again.Options) != string(c.Options) {
		t.Fatalf("resend: %+v %v", again, err)
	}
	if _, err := f.dchallenge("dreq-1", f.terms("a", "dlg-2")); refusal(err) != "REQUEST_ID_CONFLICT" {
		t.Fatalf("same request ID, other terms: %v", err)
	}
	if _, err := f.dchallenge("dreq-2", terms); refusal(err) != "CHALLENGE_OPEN" {
		t.Fatalf("second challenge: %v", err)
	}
	if _, err := f.core.DelegationConfirmation(bgA, "dlg-1"); refusal(err) != "DELEGATION_NOT_CONFIRMED" {
		t.Fatalf("before: %v", err)
	}
	as := a.Get(c.Options, origin)
	r, err := f.dverify("dver-1", c.ChallengeID, terms, as)
	if err != nil || r.State != "verified" || r.ApprovalSessionID != "dlg-1" || r.IntentHash != c.IntentHash || r.ExpiresAt != f.now+DelegationPayTTL ||
		!strings.HasPrefix(r.ApprovalID, "hap-") || !strings.HasPrefix(r.CredentialRef, "sha256:") {
		t.Fatalf("verify %+v %v", r, err)
	}
	if q, err := f.core.DelegationConfirmation(bgA, "dlg-1"); err != nil || q.ApprovalID != r.ApprovalID {
		t.Fatalf("confirmation %+v %v", q, err)
	}
	// One confirmation per delegation: a resend answers it; a replay or a new challenge does not make another.
	if again, err := f.dverify("dver-1", c.ChallengeID, terms, as); err != nil || again.ApprovalID != r.ApprovalID {
		t.Fatalf("resend %+v %v", again, err)
	}
	if _, err := f.dverify("dver-2", c.ChallengeID, terms, as); refusal(err) != "CHALLENGE_USED" {
		t.Fatalf("replay: %v", err)
	}
	if _, err := f.dchallenge("dreq-3", terms); refusal(err) != "DELEGATION_CONFIRMED" {
		t.Fatalf("after: %v", err)
	}
	var budget, allow uint64
	if err := f.core.db.QueryRow(`SELECT task_budget, allow_max FROM delegation_confirmations WHERE delegation_id = 'dlg-1' AND task_id IS NULL`).Scan(&budget, &allow); err != nil || budget != 1500000 || allow != 100000 {
		t.Fatalf("kept %d %d %v", budget, allow, err)
	}
}

// A delegation's confirmation is never a purchase's approval, nor the
// reverse; nor another delegation's.
func TestDelegationIsNeverAPurchase(t *testing.T) {
	f := newAFX(t)
	a := f.enroll("approver-a", nil)
	terms, order := f.terms("a", "dlg-1"), f.order("a")
	d, err := f.dchallenge("dreq-1", terms)
	if err != nil {
		t.Fatal(err)
	}
	p := f.mustChallenge("req-1", order, "approve")
	if _, err := f.verify("ver-1", d.ChallengeID, order, a.Get(d.Options, origin)); refusal(err) != "INTENT_MISMATCH" {
		t.Fatalf("a delegation's challenge as a purchase's: %v", err)
	}
	if _, err := f.dverify("dver-1", p.ChallengeID, terms, a.Get(p.Options, origin)); refusal(err) != "INTENT_MISMATCH" {
		t.Fatalf("a purchase's challenge as a delegation's: %v", err)
	}
	if _, err := f.dverify("dver-2", d.ChallengeID, f.terms("a", "dlg-2"), a.Get(d.Options, origin)); refusal(err) != "INTENT_MISMATCH" {
		t.Fatalf("another delegation's terms: %v", err)
	}
	// The challenge still stands after two refusals (three close it): the right terms confirm.
	if r, err := f.dverify("dver-4", d.ChallengeID, terms, a.Get(d.Options, origin)); err != nil || r.State != "verified" {
		t.Fatalf("then the right one: %+v %v", r, err)
	}
	// The same delegation ID with another budget is other terms.
	d3, err := f.dchallenge("dreq-3", f.terms("a", "dlg-3"))
	if err != nil {
		t.Fatal(err)
	}
	var tt paycontract.DelegationTerms
	json.Unmarshal(f.terms("a", "dlg-3"), &tt)
	tt.TaskBudget = contracts.Amount("1500001")
	other, _ := json.Marshal(tt)
	if _, err := f.dverify("dver-5", d3.ChallengeID, other, a.Get(d3.Options, origin)); refusal(err) != "INTENT_MISMATCH" {
		t.Fatalf("the same delegation, another budget: %v", err)
	}
	// And the purchase's own challenge was not touched.
	if r, err := f.verify("ver-2", p.ChallengeID, order, a.Get(p.Options, origin)); err != nil || r.State != "verified" || r.ApprovalSessionID != sessionOf(order) {
		t.Fatalf("the purchase's own: %+v %v", r, err)
	}
}

func TestDelegationRefusals(t *testing.T) {
	f := newAFX(t)
	f.enroll("approver-a", nil)
	set := func(x, id string, edit func(*paycontract.DelegationTerms)) json.RawMessage {
		var tt paycontract.DelegationTerms
		json.Unmarshal(f.terms(x, id), &tt)
		edit(&tt)
		raw, _ := json.Marshal(tt)
		return raw
	}
	n := 0
	for name, c := range map[string]struct {
		terms json.RawMessage
		code  string
	}{
		"another consumer's wallet": {set("b", "dlg-1", func(t *paycontract.DelegationTerms) { t.WalletID = "wallet-a" }), "CONSUMER_MISMATCH"},
		"another chain":             {set("a", "dlg-2", func(t *paycontract.DelegationTerms) { t.ChainID = "1" }), "CONSUMER_MISMATCH"},
		"an asset it does not know": {set("a", "dlg-3", func(t *paycontract.DelegationTerms) { t.AssetContract = "0x" + strings.Repeat("5", 40) }), "ASSET_UNKNOWN"},
		"no device enrolled":        {f.terms("b", "dlg-4"), "CREDENTIAL_UNKNOWN"},
		"malformed terms":           {set("a", "dlg-5", func(t *paycontract.DelegationTerms) { t.AllowMax = "1500001" }), "TERMS_INVALID"},
		"an unknown wallet":         {set("a", "dlg-6", func(t *paycontract.DelegationTerms) { t.WalletID = "wallet-z" }), "WALLET_NOT_REGISTERED"},
	} {
		n++
		if _, err := f.dchallenge(fmt.Sprintf("dreq-%d", n), c.terms); refusal(err) != c.code {
			t.Errorf("%s: %v, want %s", name, err, c.code)
		}
	}
	if _, err := f.core.DelegationChallenge(bgA, DelegationChallengeRequest{RequestID: "dreq-x", Terms: f.terms("a", "dlg-7"), HostDeadline: f.now}); refusal(err) != "AUTHORIZATION_EXPIRED" {
		t.Errorf("a Host deadline already past: %v", err)
	}
}

func TestDelegationRoutes(t *testing.T) {
	f := newAFX(t)
	a := f.enroll("approver-a", nil)
	h := ApprovalsHandler(f.core, func(string) {})
	do := func(method, path, body string) (int, ApprovalReply) {
		req := httptest.NewRequest(method, path, strings.NewReader(body)).WithContext(WithCaller(bgA, 501))
		w := httptest.NewRecorder()
		h.ServeHTTP(w, req)
		var r ApprovalReply
		json.Unmarshal(w.Body.Bytes(), &r)
		return w.Code, r
	}
	if status, r := do("GET", "/v1/health", ""); status != 200 || r.Delegations != "device" {
		t.Fatalf("health %d %+v", status, r)
	}
	terms := f.terms("a", "dlg-1")
	if status, r := do("GET", "/v1/approvals/delegations/dlg-1", ""); status != http.StatusNotFound || r.Error != "DELEGATION_NOT_CONFIRMED" {
		t.Fatalf("before %d %+v", status, r)
	}
	body, _ := json.Marshal(DelegationChallengeRequest{RequestID: "dreq-1", Terms: terms, HostDeadline: f.now + 600})
	status, c := do("POST", "/v1/approvals/delegations/challenge", string(body))
	if status != 200 || c.State != "issued" || len(c.Terms) == 0 {
		t.Fatalf("challenge %d %+v", status, c)
	}
	vb, _ := json.Marshal(DelegationVerifyRequest{VerifyRequestID: "dver-1", ChallengeID: c.ChallengeID, Terms: terms, Assertion: a.Get(c.Options, origin)})
	if status, r := do("POST", "/v1/approvals/delegations/verify", string(vb)); status != 200 || r.State != "verified" {
		t.Fatalf("verify %d %+v", status, r)
	}
	if status, r := do("GET", "/v1/approvals/delegations/dlg-1", ""); status != 200 || r.State != "verified" || r.ApprovalSessionID != "dlg-1" || r.RevokedAt != 0 {
		t.Fatalf("after %d %+v", status, r)
	}
	rb, _ := json.Marshal(DelegationRevokeRequest{RequestID: "rreq-1", DelegationID: "dlg-1", ConsumerID: "consumer-a", HostDeadline: f.now + 600})
	status, rc := do("POST", "/v1/approvals/delegations/revoke/challenge", string(rb))
	if status != 200 || rc.State != "issued" || rc.Decision != "deny" {
		t.Fatalf("revoke challenge %d %+v", status, rc)
	}
	rv, _ := json.Marshal(DelegationRevokeVerifyRequest{VerifyRequestID: "rver-1", ChallengeID: rc.ChallengeID, DelegationID: "dlg-1", Assertion: a.Get(rc.Options, origin)})
	if status, r := do("POST", "/v1/approvals/delegations/revoke/verify", string(rv)); status != 200 || r.State != "verified" || r.Decision != "deny" {
		t.Fatalf("revoke verify %d %+v", status, r)
	}
	if status, r := do("GET", "/v1/approvals/delegations/dlg-1", ""); status != 200 || r.RevokedAt == 0 || r.RevocationID == "" {
		t.Fatalf("revoked %d %+v", status, r)
	}
}

// Every payment of a Signer that takes approvals falls under a delegation
// the person confirmed, and within its terms (docs/commercial-gap4.md 2.2).
func TestPaymentNeedsTheConfirmation(t *testing.T) {
	f := newHFX(t)
	a := f.enroll("approver-a", nil)
	confirm := func(id string, edit func(*paycontract.DelegationTerms)) string {
		var tt paycontract.DelegationTerms
		json.Unmarshal(f.terms("a", id), &tt)
		if edit != nil {
			edit(&tt)
		}
		terms, _ := json.Marshal(tt)
		c, err := f.dchallenge("dreq-"+id, terms)
		if err != nil {
			t.Fatal(err)
		}
		r, err := f.dverify("dver-"+id, c.ChallengeID, terms, a.Get(c.Options, origin))
		if err != nil {
			t.Fatal(err)
		}
		return r.ApprovalID
	}
	conf := confirm("dlg-1", nil) // budget 1500000, allow_max 100000, task-consumer-a once it pays
	allow := func(id string) Authorization {
		return Authorization{Method: "delegation", DelegationID: "dlg-a", DelegationVersion: 1, DelegationConfirmationID: id}
	}
	ask := func(order json.RawMessage, id string) Authorization {
		return Authorization{Method: "webauthn", ApprovalID: f.approve(a, order, "approve").ApprovalID, DelegationConfirmationID: id}
	}
	n := 900
	sign := func(au Authorization, order json.RawMessage) error {
		n++
		_, err := f.pay.Sign(bg, SignRequest{RequestID: fmt.Sprintf("request-%010d", n), Authorization: au, Order: order})
		return err
	}
	for name, c := range map[string]struct {
		au    func(json.RawMessage) Authorization
		order json.RawMessage
		want  string
	}{
		"an Allow with none":      {func(json.RawMessage) Authorization { return allow("") }, f.orderA("50000"), "DELEGATION_NOT_CONFIRMED"},
		"an Ask with none":        {func(o json.RawMessage) Authorization { return ask(o, "") }, f.orderA("500000"), "DELEGATION_NOT_CONFIRMED"},
		"an unknown one":          {func(json.RawMessage) Authorization { return allow("hap-00") }, f.orderA("50000"), "DELEGATION_NOT_CONFIRMED"},
		"an Allow over allow_max": {func(json.RawMessage) Authorization { return allow(conf) }, f.orderA("100001"), "OVER_ALLOW_MAX"},
		"another task":            {func(json.RawMessage) Authorization { return allow(conf) }, f.order2("50000", "task-other"), "DELEGATION_OTHER_TASK"},
		"a payee outside its list": {func(o json.RawMessage) Authorization {
			return ask(o, confirm("dlg-2", func(t *paycontract.DelegationTerms) { t.Merchants = []string{"0x" + strings.Repeat("6", 40)} }))
		}, f.orderA("500000"), "MERCHANT_NOT_ALLOWED"},
	} {
		if c.want == "DELEGATION_OTHER_TASK" {
			if err := sign(allow(conf), f.orderA("50000")); err != nil { // binds task-consumer-a
				t.Fatalf("first payment: %v", err)
			}
		}
		if err := sign(c.au(c.order), c.order); refusal(err) != c.want {
			t.Errorf("%s: %v, want %s", name, err, c.want)
		}
	}
	// The task budget counts Allows and Asks together: 1500000 in all.
	spent := uint64(0)
	f.db.QueryRow(`SELECT coalesce(sum(amount), 0) FROM attempts WHERE confirmation_id = ?`, conf).Scan(&spent)
	for spent+500000 <= 1500000 {
		o := f.orderA("500000")
		if err := sign(ask(o, conf), o); err != nil {
			t.Fatalf("within the budget (%d spent): %v", spent, err)
		}
		spent += 500000
	}
	o := f.orderA("500000")
	if err := sign(ask(o, conf), o); refusal(err) != "OVER_TASK_BUDGET" {
		t.Fatalf("over the task budget (%d spent): %v", spent, err)
	}
	// A revoked device takes its confirmations with it; an expired one pays nothing.
	other := confirm("dlg-3", nil)
	if err := f.core.Revoke(bgA, Fingerprint(a.ID)); err != nil {
		t.Fatal(err)
	}
	if err := sign(allow(other), f.order2("50000", "task-3")); refusal(err) != "CREDENTIAL_REVOKED" {
		t.Errorf("revoked: %v", err)
	}
	f.now += DelegationPayTTL
	if err := sign(Authorization{Method: "delegation", DelegationID: "dlg-a", DelegationVersion: 1, DelegationConfirmationID: other}, f.order2("50000", "task-4")); refusal(err) != "DELEGATION_EXPIRED" {
		t.Errorf("expired: %v", err)
	}
}

// order2 is consumer A's next purchase in another task.
func (f *hfx) order2(amount, task string) json.RawMessage {
	f.n++
	return signertest.Order{Consumer: "consumer-a", Agent: "agent-a", Wallet: "wallet-a", Task: task, From: Address(keyA.PubKey()), PayTo: signertest.Merchant,
		Amount: amount, N: f.n, Now: f.now, Window: 600}.Raw()
}

// Review of S1-S3: an abandoned confirmation, a changed approver and an
// expired confirmation each pay nothing, on their own.
func TestConfirmationEndsAndChanges(t *testing.T) {
	f := newHFX(t)
	a := f.enroll("approver-a", nil)
	confirm := func(id string) string {
		terms := f.terms("a", id)
		c, err := f.dchallenge("dreq-"+id, terms)
		if err != nil {
			t.Fatal(err)
		}
		r, err := f.dverify("dver-"+id, c.ChallengeID, terms, a.Get(c.Options, origin))
		if err != nil {
			t.Fatal(err)
		}
		return r.ApprovalID
	}
	allow := func(id string) Authorization {
		return Authorization{Method: "delegation", DelegationID: "dlg-a", DelegationVersion: 1, DelegationConfirmationID: id}
	}
	n := 950
	sign := func(au Authorization, order json.RawMessage) error {
		n++
		_, err := f.pay.Sign(bg, SignRequest{RequestID: fmt.Sprintf("request-%010d", n), Authorization: au, Order: order})
		return err
	}
	abandoned := confirm("dlg-1")
	if _, err := f.core.Abandon(bgA, abandoned); err != nil {
		t.Fatal(err)
	}
	if err := sign(allow(abandoned), f.order2("50000", "task-1")); refusal(err) != "DELEGATION_NOT_CONFIRMED" {
		t.Errorf("abandoned: %v", err)
	}
	changed := confirm("dlg-2")
	ap := f.pay.cfg.approver("approver-a")
	ap.WalletIDs = []string{"wallet-z"} // the wallet taken from the approver, its version kept
	if err := sign(allow(changed), f.order2("50000", "task-2")); refusal(err) != "APPROVER_CHANGED" {
		t.Errorf("wallet taken from the approver: %v", err)
	}
	ap.WalletIDs = []string{"wallet-a"}
	expiring := confirm("dlg-3")
	f.now += DelegationPayTTL // an Ask, so no static delegation's own validity is in the way
	o := f.order2("500000", "task-3")
	ask := Authorization{Method: "webauthn", ApprovalID: f.approve(a, o, "approve").ApprovalID, DelegationConfirmationID: expiring}
	if err := sign(ask, o); refusal(err) != "DELEGATION_EXPIRED" {
		t.Errorf("expired: %v", err)
	}
}

// An attempts table from before gap 4 gains confirmation_id, its rows kept.
func TestMigrateConfirmation(t *testing.T) {
	db, err := OpenDB(filepath.Join(t.TempDir(), "signer.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	old := strings.Replace(schema, "  confirmation_id TEXT,\n", "", 1)
	if old == schema {
		t.Fatal("the schema has no confirmation_id line to drop")
	}
	if _, err := db.Exec(old); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`INSERT INTO attempts (attempt_id, request_id, fingerprint, intent_hash, wallet_id, chain_id, asset_contract, payer, nonce, delegation_id, amount, state,
		accepted_at, auth_deadline, typed_digest, snapshot, closed_at) VALUES ('att-old', 'request-old', 'fp', 'ih', 'wallet-a', 'c', 'x', 'p', 'n', 'dlg-a', 1, 'abandoned', 1, 2, 'd', '{}', 3)`); err != nil {
		t.Fatal(err)
	}
	cfg, err := ParseConfig(signertest.ApprovalsConfig("/tmp/apa-test.sock", 501, t0, Address(keyA.PubKey()), Address(keyB.PubKey()), origin))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := NewCoreDB(bg, cfg, map[string]*secp256k1.PrivateKey{Address(keyA.PubKey()): keyA, Address(keyB.PubKey()): keyB}, db, func() uint64 { return t0 }, reserved{}); err != nil {
		t.Fatal(err)
	}
	var n int
	if err := db.QueryRow(`SELECT count(*) FROM attempts WHERE attempt_id = 'att-old' AND confirmation_id IS NULL`).Scan(&n); err != nil || n != 1 {
		t.Fatalf("the old row %d %v", n, err)
	}
}

func (f *afx) rchallenge(req, id, consumer string) (ApprovalReply, error) {
	return f.core.DelegationRevokeChallenge(bgA, DelegationRevokeRequest{RequestID: req, DelegationID: id, ConsumerID: consumer, HostDeadline: f.now + 600})
}

func (f *afx) rverify(vid, challengeID, id string, assertion json.RawMessage) (ApprovalReply, error) {
	return f.core.DelegationRevokeVerify(bgA, DelegationRevokeVerifyRequest{VerifyRequestID: vid, ChallengeID: challengeID, DelegationID: id, Assertion: assertion})
}

// The person takes a delegation back with the device that confirmed it
// (docs/commercial-gap4.md 7): the Signer signs nothing more under it,
// an attempt it accepted and has not yet signed is dropped, and neither
// ceremony stands for the other.
func TestDelegationRevoke(t *testing.T) {
	f := newHFX(t)
	a := f.enroll("approver-a", nil)
	terms := f.terms("a", "dlg-1")
	dc, err := f.dchallenge("dreq-1", terms)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := f.rchallenge("rreq-0", "dlg-1", "consumer-a"); refusal(err) != "DELEGATION_NOT_CONFIRMED" {
		t.Fatalf("before the confirmation: %v", err)
	}
	conf, err := f.dverify("dver-1", dc.ChallengeID, terms, a.Get(dc.Options, origin))
	if err != nil {
		t.Fatal(err)
	}
	allow := Authorization{Method: "delegation", DelegationID: "dlg-a", DelegationVersion: 1, DelegationConfirmationID: conf.ApprovalID}
	n := 0
	sign := func(order json.RawMessage) (Reply, error) {
		n++
		return f.pay.Sign(bg, SignRequest{RequestID: fmt.Sprintf("request-%010d", 700+n), Authorization: allow, Order: order})
	}
	if _, err := sign(f.orderA("50000")); err != nil {
		t.Fatalf("before: %v", err)
	}
	if _, err := f.rchallenge("rreq-1", "dlg-1", "consumer-b"); refusal(err) != "CONSUMER_MISMATCH" {
		t.Fatalf("another consumer's: %v", err)
	}
	rc, err := f.rchallenge("rreq-2", "dlg-1", "consumer-a")
	if err != nil || rc.State != "issued" || rc.Decision != "deny" || rc.ApprovalSessionID != "dlg-1" || rc.IntentHash != dc.IntentHash || len(rc.Terms) == 0 {
		t.Fatalf("challenge %+v %v", rc, err)
	}
	if again, err := f.rchallenge("rreq-2", "dlg-1", "consumer-a"); err != nil || again.ChallengeID != rc.ChallengeID {
		t.Fatalf("resend %+v %v", again, err)
	}
	// Neither ceremony stands for the other (two refusals; three close the challenge).
	if _, err := f.dverify("dver-2", rc.ChallengeID, terms, a.Get(rc.Options, origin)); refusal(err) != "INTENT_MISMATCH" {
		t.Fatalf("a revocation's challenge as a confirmation: %v", err)
	}
	if _, err := f.rverify("rver-0", rc.ChallengeID, "dlg-2", a.Get(rc.Options, origin)); refusal(err) != "INTENT_MISMATCH" {
		t.Fatalf("another delegation's ID: %v", err)
	}
	// The revocation lands between a payment's acceptance and its signature:
	// the signature computed is dropped.
	var rev ApprovalReply
	hook = func(s, _ string) error {
		if s == "after-sign" {
			hook = nil
			rev, err = f.rverify("rver-1", rc.ChallengeID, "dlg-1", a.Get(rc.Options, origin))
		}
		return nil
	}
	defer func() { hook = nil }()
	if r, err := sign(f.orderA("50000")); refusal(err) != "ATTEMPT_ABANDONED" || r.Authorization != nil {
		t.Fatalf("signed after the revocation: %+v %v", r, err)
	}
	if err != nil || rev.State != "verified" || rev.Decision != "deny" || rev.ApprovalSessionID != "dlg-1" {
		t.Fatalf("revocation %+v %v", rev, err)
	}
	if again, err := f.rverify("rver-1", rc.ChallengeID, "dlg-1", a.Get(rc.Options, origin)); err == nil && again.ApprovalID != rev.ApprovalID {
		t.Fatalf("resend answered another: %+v", again)
	}
	if _, err := sign(f.orderA("50000")); refusal(err) != "DELEGATION_REVOKED" {
		t.Fatalf("after: %v", err)
	}
	if _, err := f.rchallenge("rreq-3", "dlg-1", "consumer-a"); refusal(err) != "DELEGATION_REVOKED" {
		t.Fatalf("a second revocation: %v", err)
	}
	if q, err := f.core.DelegationConfirmation(bgA, "dlg-1"); err != nil || q.ApprovalID != conf.ApprovalID || q.RevocationID != rev.ApprovalID || q.RevokedAt != f.now {
		t.Fatalf("the confirmation says %+v %v", q, err)
	}
}
