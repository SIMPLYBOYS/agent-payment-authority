package signer

// A2's Signer (docs/commercial-human-approval-design.md 4, 6.1, 7, 9): the
// approval core against a software authenticator (signertest). None of it
// is a device result.

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"

	"github.com/SIMPLYBOYS/agent-payment-authority/internal/paycontract"
	"github.com/SIMPLYBOYS/agent-payment-authority/internal/signer/signertest"
)

const (
	origin = "http://localhost:8402"
	addrA  = "0x1111111111111111111111111111111111111111"
	addrB  = "0x2222222222222222222222222222222222222222"
)

type afx struct {
	t    *testing.T
	core *ApprovalCore
	now  uint64
	n    int
}

func newAFX(t *testing.T) *afx {
	t.Helper()
	f := &afx{t: t, now: 1790726450}
	cfg, err := ParseConfig(signertest.ApprovalsConfig("/tmp/apa-test.sock", 501, f.now, addrA, addrB, origin))
	if err != nil {
		t.Fatal(err)
	}
	core, err := NewApprovalCore(context.Background(), cfg, filepath.Join(t.TempDir(), "approvals.db"), func() uint64 { return f.now })
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { core.Close() })
	f.core = core
	return f
}

var bgA = context.Background()

// enroll registers and confirms a software authenticator for approver.
func (f *afx) enroll(approver string, a *signertest.Authenticator) *signertest.Authenticator {
	f.t.Helper()
	if a == nil {
		a = signertest.NewAuthenticator()
	}
	id, code, _, err := f.core.EnrollBegin(bgA, approver)
	if err != nil {
		f.t.Fatal(err)
	}
	o, err := f.core.EnrollOptions(bgA, id, code)
	if err != nil {
		f.t.Fatal(err)
	}
	if _, err := f.core.EnrollFinish(bgA, id, code, a.Create(o.Options, origin)); err != nil {
		f.t.Fatal(err)
	}
	if err := f.core.EnrollConfirm(bgA, id); err != nil {
		f.t.Fatal(err)
	}
	return a
}

// order is a new purchase of consumer x ("a" or "b").
func (f *afx) order(x string) json.RawMessage {
	f.n++
	from := map[string]string{"a": addrA, "b": addrB}[x]
	return signertest.Order{Consumer: "consumer-" + x, Agent: "agent-" + x, Wallet: "wallet-" + x, From: from, PayTo: signertest.Merchant,
		Amount: "500000", N: f.n, Now: f.now, Window: 600}.Raw()
}

func sessionOf(order json.RawMessage) string {
	var po paycontract.PurchaseOrder
	json.Unmarshal(order, &po)
	return paycontract.SessionID(po.Intent.TaskID, po.Intent.PurchaseOrderID)
}

func (f *afx) challenge(req string, order json.RawMessage, decision string) (ApprovalReply, error) {
	return f.core.Challenge(bgA, ChallengeRequest{RequestID: req, ApprovalSessionID: sessionOf(order), Decision: decision, Order: order, HostDeadline: f.now + 600})
}

func (f *afx) mustChallenge(req string, order json.RawMessage, decision string) ApprovalReply {
	f.t.Helper()
	r, err := f.challenge(req, order, decision)
	if err != nil {
		f.t.Fatalf("challenge %s: %v", req, err)
	}
	return r
}

func (f *afx) verify(vid, challengeID string, order, assertion json.RawMessage) (ApprovalReply, error) {
	return f.core.Verify(bgA, VerifyRequest{VerifyRequestID: vid, ChallengeID: challengeID, Order: order, Assertion: assertion})
}

func refusal(err error) string {
	var r Refusal
	if errors.As(err, &r) {
		return string(r)
	}
	if err != nil {
		return "error: " + err.Error()
	}
	return ""
}

func TestApproveAndResend(t *testing.T) {
	f := newAFX(t)
	a := f.enroll("approver-a", nil)
	order := f.order("a")
	c := f.mustChallenge("req-1", order, "approve")
	if c.State != "issued" || c.ConsumerID != "consumer-a" || c.Decision != "approve" || c.Display == nil || c.Display.Amount != "500000" || c.ExpiresAt != f.now+120 {
		t.Fatalf("challenge %+v", c)
	}
	// Same request ID, same content: the same challenge; other content: a conflict.
	if again := f.mustChallenge("req-1", order, "approve"); again.ChallengeID != c.ChallengeID || string(again.Options) != string(c.Options) {
		t.Fatalf("resend gave another challenge")
	}
	if _, err := f.challenge("req-1", order, "deny"); refusal(err) != "REQUEST_ID_CONFLICT" {
		t.Fatalf("same request ID, other decision: %v", err)
	}
	// One issued challenge per session.
	if _, err := f.challenge("req-2", order, "approve"); refusal(err) != "CHALLENGE_OPEN" {
		t.Fatalf("second challenge: %v", err)
	}
	as := a.Get(c.Options, origin)
	r, err := f.verify("ver-1", c.ChallengeID, order, as)
	if err != nil || r.State != "verified" || r.Decision != "approve" || r.ApprovalSessionID != sessionOf(order) || r.IntentHash != c.IntentHash ||
		r.ApproverID != "approver-a" || !strings.HasPrefix(r.CredentialRef, "sha256:") {
		t.Fatalf("verify %+v %v", r, err)
	}
	// The session is decided once: no new challenge, for either decision.
	if _, err := f.challenge("req-3", order, "deny"); refusal(err) != "SESSION_DECIDED" {
		t.Fatalf("after a decision: %v", err)
	}
	// A resend answers what was saved, without checking again: even after
	// the credential is revoked.
	if err := f.core.Revoke(bgA, Fingerprint(a.ID)); err != nil {
		t.Fatal(err)
	}
	if again, err := f.verify("ver-1", c.ChallengeID, order, as); err != nil || again.ApprovalID != r.ApprovalID {
		t.Fatalf("resend: %+v %v", again, err)
	}
	if _, err := f.verify("ver-1", c.ChallengeID, order, a.Get(c.Options, origin)); refusal(err) != "REQUEST_ID_CONFLICT" {
		t.Fatalf("same verify ID, other assertion: %v", err)
	}
	// Another assertion of the used challenge (a replay) is refused.
	if _, err := f.verify("ver-2", c.ChallengeID, order, as); refusal(err) != "CHALLENGE_USED" {
		t.Fatalf("replay: %v", err)
	}
	if q, err := f.core.ChallengeByRequest(bgA, "req-1"); err != nil || q.State != "used" || q.ApprovalID != r.ApprovalID || q.VerifyRequestID != "ver-1" {
		t.Fatalf("query: %+v %v", q, err)
	}
	if q, err := f.core.ApprovalByVerifyRequest(bgA, "ver-2"); refusal(err) != "CHALLENGE_USED" || q.ChallengeID != c.ChallengeID {
		t.Fatalf("saved refusal: %+v %v", q, err)
	}
	if _, err := f.core.ApprovalByVerifyRequest(bgA, "ver-9"); refusal(err) != "VERIFY_UNKNOWN" {
		t.Fatalf("unknown verify: %v", err)
	}
	if _, err := f.core.ChallengeByRequest(bgA, "req-9"); refusal(err) != "CHALLENGE_UNKNOWN" {
		t.Fatalf("unknown challenge: %v", err)
	}
	// Abandon: verified -> abandoned, once.
	if ab, err := f.core.Abandon(bgA, r.ApprovalID); err != nil || ab.State != "abandoned" {
		t.Fatalf("abandon %+v %v", ab, err)
	}
}

func TestDenyIsADecisionToo(t *testing.T) {
	f := newAFX(t)
	a := f.enroll("approver-a", nil)
	order := f.order("a")
	c := f.mustChallenge("req-1", order, "deny")
	r, err := f.verify("ver-1", c.ChallengeID, order, a.Get(c.Options, origin))
	if err != nil || r.Decision != "deny" || r.State != "verified" {
		t.Fatalf("deny %+v %v", r, err)
	}
	if _, err := f.challenge("req-2", order, "approve"); refusal(err) != "SESSION_DECIDED" {
		t.Fatalf("approve after deny: %v", err)
	}
}

func TestBindingAndIdentity(t *testing.T) {
	f := newAFX(t)
	a := f.enroll("approver-a", nil)
	b := f.enroll("approver-b", nil)
	order := f.order("a")
	c := f.mustChallenge("req-1", order, "approve")
	// B's authenticator for A's purchase: not in A's credentials.
	if _, err := f.verify("ver-1", c.ChallengeID, order, b.Get(c.Options, origin)); refusal(err) != "ASSERTION_INVALID" {
		t.Fatalf("B for A: %v", err)
	}
	// A changed order (another purchase): the intent does not match.
	if _, err := f.verify("ver-2", c.ChallengeID, f.order("a"), a.Get(c.Options, origin)); refusal(err) != "INTENT_MISMATCH" {
		t.Fatalf("other order: %v", err)
	}
	// A copied ID without the key: the signature fails.
	thief := signertest.NewAuthenticator()
	thief.ID = a.ID
	if _, err := f.verify("ver-3", c.ChallengeID, order, thief.Get(c.Options, origin)); refusal(err) != "ASSERTION_INVALID_SIGNATURE" {
		t.Fatalf("copied ID: %v", err)
	}
	// Three refusals close the challenge.
	if q, _ := f.core.ChallengeByID(bgA, c.ChallengeID); q.State != "closed" {
		t.Fatalf("after three refusals: %s", q.State)
	}
	// The session ID must be the order's; the consumer must have an approver for the wallet.
	bad := ChallengeRequest{RequestID: "req-2", ApprovalSessionID: "aps-other", Decision: "approve", Order: f.order("a"), HostDeadline: f.now + 600}
	if _, err := f.core.Challenge(bgA, bad); refusal(err) != "SESSION_MISMATCH" {
		t.Fatalf("session: %v", err)
	}
}

func TestAssertionRules(t *testing.T) {
	f := newAFX(t)
	a := f.enroll("approver-a", nil)
	n := 0
	for name, tc := range map[string]struct {
		edit func(ch string) []byte
		want string
	}{
		"127.0.0.1 origin": {func(ch string) []byte { return signertest.ClientData("webauthn.get", ch, "http://127.0.0.1:8402", nil) }, "ASSERTION_INVALID_ORIGIN"},
		"other port":       {func(ch string) []byte { return signertest.ClientData("webauthn.get", ch, "http://localhost:8403", nil) }, "ASSERTION_INVALID_ORIGIN"},
		"topOrigin": {func(ch string) []byte {
			return signertest.ClientData("webauthn.get", ch, origin, map[string]any{"topOrigin": "http://evil.example"})
		}, "ASSERTION_INVALID_ORIGIN"},
		"crossOrigin": {func(ch string) []byte {
			return signertest.ClientData("webauthn.get", ch, origin, map[string]any{"crossOrigin": true, "topOrigin": origin})
		}, "ASSERTION_INVALID_ORIGIN"},
		"type create": {func(ch string) []byte { return signertest.ClientData("webauthn.create", ch, origin, nil) }, "ASSERTION_INVALID_TYPE"},
		"other challenge": {func(string) []byte {
			return signertest.ClientData("webauthn.get", "AAAAAAAAAAAAAAAAAAAAAA", origin, nil)
		}, "ASSERTION_INVALID_CHALLENGE"},
	} {
		n++
		order := f.order("a")
		c := f.mustChallenge(fmt.Sprintf("req-o%d", n), order, "approve")
		ch, _ := signertest.Challenge(c.Options)
		if _, err := f.verify(fmt.Sprintf("ver-o%d", n), c.ChallengeID, order, a.GetData(tc.edit(ch))); refusal(err) != tc.want {
			t.Fatalf("%s: %v", name, err)
		}
		f.core.CloseChallenge(bgA, c.ChallengeID, fmt.Sprintf("close-o%d", n))
	}
	// The same, with the right order of each challenge.
	cases := []struct {
		name  string
		flags byte
		rpid  string
		want  string
	}{
		{"no UV", signertest.UP, "localhost", "ASSERTION_INVALID_FLAGS"},
		{"no UP", signertest.UV, "localhost", "ASSERTION_INVALID_FLAGS"},
		{"RP ID", signertest.UP | signertest.UV, "127.0.0.1", "ASSERTION_INVALID_RPID"},
	}
	for _, tc := range cases {
		order := f.order("a")
		n++
		c := f.mustChallenge(fmt.Sprintf("req-f%d", n), order, "approve")
		a.Flags, a.RPID = tc.flags, tc.rpid
		_, err := f.verify(fmt.Sprintf("ver-f%d", n), c.ChallengeID, order, a.Get(c.Options, origin))
		a.Flags, a.RPID = signertest.UP|signertest.UV, "localhost"
		if refusal(err) != tc.want {
			t.Fatalf("%s: %v", tc.name, err)
		}
		if _, err := f.core.CloseChallenge(bgA, c.ChallengeID, fmt.Sprintf("close-f%d", n)); err != nil {
			t.Fatal(err)
		}
	}
}

func TestCounterAndBackupFlags(t *testing.T) {
	// 0/0 is accepted every time (synced passkeys).
	f := newAFX(t)
	a := f.enroll("approver-a", nil)
	for i := 0; i < 2; i++ {
		order := f.order("a")
		c := f.mustChallenge("req-z"+string(rune('0'+i)), order, "approve")
		if _, err := f.verify("ver-z"+string(rune('0'+i)), c.ChallengeID, order, a.Get(c.Options, origin)); err != nil {
			t.Fatalf("0/0 #%d: %v", i, err)
		}
	}
	// Saved 5, received 3: refused, and the credential is suspect.
	f = newAFX(t)
	a = signertest.NewAuthenticator()
	a.Count = 5
	f.enroll("approver-a", a)
	order := f.order("a")
	c := f.mustChallenge("req-1", order, "approve")
	a.Count = 3
	if _, err := f.verify("ver-1", c.ChallengeID, order, a.Get(c.Options, origin)); refusal(err) != "ASSERTION_COUNTER" {
		t.Fatalf("counter back: %v", err)
	}
	a.Count = 9
	if _, err := f.verify("ver-2", c.ChallengeID, order, a.Get(c.Options, origin)); refusal(err) != "CREDENTIAL_SUSPECT" {
		t.Fatalf("after suspect: %v", err)
	}
	if _, err := f.challenge("req-2", f.order("a"), "approve"); refusal(err) != "CREDENTIAL_REVOKED" {
		t.Fatalf("new challenge for a suspect credential only: %v", err)
	}
	// BE is fixed at registration; BS may change while BE=1, and is written back.
	f = newAFX(t)
	a = signertest.NewAuthenticator()
	a.Flags = signertest.UP | signertest.UV | signertest.BE
	f.enroll("approver-a", a)
	order = f.order("a")
	c = f.mustChallenge("req-1", order, "approve")
	a.Flags |= signertest.BS
	if r, err := f.verify("ver-1", c.ChallengeID, order, a.Get(c.Options, origin)); err != nil || r.Flags != "UP=1 UV=1 BE=1 BS=1" {
		t.Fatalf("BS change: %+v %v", r, err)
	}
	order = f.order("a")
	c = f.mustChallenge("req-2", order, "approve")
	a.Flags = signertest.UP | signertest.UV
	if _, err := f.verify("ver-2", c.ChallengeID, order, a.Get(c.Options, origin)); refusal(err) != "ASSERTION_INVALID_FLAGS" {
		t.Fatalf("BE change: %v", err)
	}
	if list, _ := f.core.Credentials(bgA); len(list) != 1 || list[0].State != "suspect" {
		t.Fatalf("BE change leaves %+v", list)
	}
}

func TestDeadlinesAndClose(t *testing.T) {
	f := newAFX(t)
	a := f.enroll("approver-a", nil)
	// The Host's deadline can only make it shorter.
	order := f.order("a")
	c, err := f.core.Challenge(bgA, ChallengeRequest{RequestID: "req-h", ApprovalSessionID: sessionOf(order), Decision: "approve", Order: order, HostDeadline: f.now + 30})
	if err != nil || c.ExpiresAt != f.now+30 {
		t.Fatalf("host deadline: %+v %v", c, err)
	}
	f.core.CloseChallenge(bgA, c.ChallengeID, "close-h")
	// Verify at exactly the deadline: refused (strict <).
	order = f.order("a")
	c = f.mustChallenge("req-1", order, "approve")
	as := a.Get(c.Options, origin)
	f.now = c.ExpiresAt
	if _, err := f.verify("ver-1", c.ChallengeID, order, as); refusal(err) != "CHALLENGE_EXPIRED" {
		t.Fatalf("at the deadline: %v", err)
	}
	// Close one second before the deadline: closed; exactly at it: expired.
	f.now -= 120
	order = f.order("a")
	c = f.mustChallenge("req-2", order, "approve")
	f.now = c.ExpiresAt - 1
	if r, err := f.core.CloseChallenge(bgA, c.ChallengeID, "close-2"); err != nil || r.State != "closed" {
		t.Fatalf("close before: %+v %v", r, err)
	}
	if r, err := f.core.CloseChallenge(bgA, c.ChallengeID, "close-2b"); err != nil || r.State != "closed" { // idempotent
		t.Fatalf("close again: %+v %v", r, err)
	}
	if _, err := f.verify("ver-2", c.ChallengeID, order, a.Get(c.Options, origin)); refusal(err) != "CHALLENGE_CLOSED" {
		t.Fatalf("verify after close: %v", err)
	}
	f.now -= 119
	order = f.order("a")
	c = f.mustChallenge("req-3", order, "approve")
	f.now = c.ExpiresAt
	if r, err := f.core.CloseChallenge(bgA, c.ChallengeID, "close-3"); err != nil || r.State != "expired" {
		t.Fatalf("close at the deadline: %+v %v", r, err)
	}
	// Used, then past the deadline: close and queries still say used.
	f.now -= 120
	order = f.order("a")
	c = f.mustChallenge("req-4", order, "approve")
	v, err := f.verify("ver-4", c.ChallengeID, order, a.Get(c.Options, origin))
	if err != nil {
		t.Fatal(err)
	}
	f.now = c.ExpiresAt + 10
	if r, err := f.core.CloseChallenge(bgA, c.ChallengeID, "close-4"); err != nil || r.State != "used" || r.ApprovalID != v.ApprovalID || r.Decision != "approve" {
		t.Fatalf("close after used: %+v %v", r, err)
	}
	// A query of an issued challenge past its deadline records it expired.
	order = f.order("a")
	c = f.mustChallenge("req-5", order, "approve")
	f.now = c.ExpiresAt
	if r, err := f.core.ChallengeByID(bgA, c.ChallengeID); err != nil || r.State != "expired" || len(r.Options) != 0 {
		t.Fatalf("query past the deadline: %+v %v", r, err)
	}
	if _, err := f.core.CloseChallenge(bgA, "chl-unknown", "close-x"); refusal(err) != "CHALLENGE_UNKNOWN" {
		t.Fatalf("close unknown: %v", err)
	}
}

// A close between the checks and the commit: the commit's conditional
// update sees closed, saves CHALLENGE_CLOSED and writes no decision.
func TestCloseBeatsVerify(t *testing.T) {
	f := newAFX(t)
	a := f.enroll("approver-a", nil)
	order := f.order("a")
	c := f.mustChallenge("req-1", order, "approve")
	hook = func(stage, id string) error {
		if stage == "verify-checked" {
			if r, err := f.core.CloseChallenge(bgA, id, "close-1"); err != nil || r.State != "closed" {
				t.Errorf("close in the middle: %+v %v", r, err)
			}
		}
		return nil
	}
	defer func() { hook = nil }()
	if _, err := f.verify("ver-1", c.ChallengeID, order, a.Get(c.Options, origin)); refusal(err) != "CHALLENGE_CLOSED" {
		t.Fatalf("late verify: %v", err)
	}
	var n int
	f.core.db.QueryRow(`SELECT count(*) FROM human_approvals`).Scan(&n)
	if n != 0 {
		t.Fatal("a decision was written after close")
	}
	if r, err := f.core.ApprovalByVerifyRequest(bgA, "ver-1"); refusal(err) != "CHALLENGE_CLOSED" || r.ApprovalID != "" {
		t.Fatalf("saved outcome: %+v %v", r, err)
	}
}

func TestEnrollmentRules(t *testing.T) {
	f := newAFX(t)
	if _, err := f.challenge("req-0", f.order("a"), "approve"); refusal(err) != "CREDENTIAL_UNKNOWN" {
		t.Fatalf("no credential: %v", err)
	}
	if _, _, _, err := f.core.EnrollBegin(bgA, "approver-x"); refusal(err) != "APPROVER_UNKNOWN" {
		t.Fatalf("unknown approver: %v", err)
	}
	id, code1, _, err := f.core.EnrollBegin(bgA, "approver-a")
	if err != nil {
		t.Fatal(err)
	}
	// A wrong code counts; five close the enrollment.
	for i := 0; i < enrollFailures; i++ {
		if _, err := f.core.EnrollOptions(bgA, id, "WRONGCOD"); refusal(err) != "ENROLLMENT_CODE" {
			t.Fatalf("wrong code %d: %v", i, err)
		}
	}
	if _, err := f.core.EnrollOptions(bgA, id, code1); refusal(err) != "ENROLLMENT_CLOSED" {
		t.Fatalf("after five wrong codes: %v", err)
	}
	// No UV, a packed attestation: refused; pending until confirmed.
	for name, a := range map[string]*signertest.Authenticator{"no UV": {Flags: signertest.UP}, "packed": {Packed: true}} {
		base := signertest.NewAuthenticator()
		base.Flags |= a.Flags & 0 // keep UP|UV unless "no UV"
		if name == "no UV" {
			base.Flags = signertest.UP
		}
		base.Packed = a.Packed
		id, c, _, _ := f.core.EnrollBegin(bgA, "approver-a")
		o, _ := f.core.EnrollOptions(bgA, id, c)
		want := map[string]string{"no UV": "ATTESTATION_INVALID", "packed": "ATTESTATION_FORMAT"}[name]
		if _, err := f.core.EnrollFinish(bgA, id, c, base.Create(o.Options, origin)); refusal(err) != want {
			t.Fatalf("%s: %v", name, err)
		}
	}
	a := signertest.NewAuthenticator()
	id, c, _, _ := f.core.EnrollBegin(bgA, "approver-a")
	o, _ := f.core.EnrollOptions(bgA, id, c)
	fin, err := f.core.EnrollFinish(bgA, id, c, a.Create(o.Options, "http://127.0.0.1:8402"))
	if refusal(err) != "ATTESTATION_INVALID" {
		t.Fatalf("attestation from another origin: %+v %v", fin, err)
	}
	id, c, _, _ = f.core.EnrollBegin(bgA, "approver-a")
	o, _ = f.core.EnrollOptions(bgA, id, c)
	fin, err = f.core.EnrollFinish(bgA, id, c, a.Create(o.Options, origin))
	if err != nil || fin.Fingerprint != Fingerprint(a.ID) || fin.State != "finished" {
		t.Fatalf("finish: %+v %v", fin, err)
	}
	if _, err := f.challenge("req-1", f.order("a"), "approve"); refusal(err) != "CREDENTIAL_PENDING" {
		t.Fatalf("before confirm: %v", err)
	}
	if err := f.core.EnrollConfirm(bgA, id); err != nil {
		t.Fatal(err)
	}
	if _, err := f.challenge("req-2", f.order("a"), "approve"); err != nil {
		t.Fatalf("after confirm: %v", err)
	}
	// An expired enrollment cannot be used.
	id, c, exp, _ := f.core.EnrollBegin(bgA, "approver-b")
	f.now = exp
	if _, err := f.core.EnrollOptions(bgA, id, c); refusal(err) != "ENROLLMENT_CLOSED" {
		t.Fatalf("expired enrollment: %v", err)
	}
}

func TestRevokedMidway(t *testing.T) {
	f := newAFX(t)
	a := f.enroll("approver-a", nil)
	order := f.order("a")
	c := f.mustChallenge("req-1", order, "approve")
	if err := f.core.Revoke(bgA, Fingerprint(a.ID)); err != nil {
		t.Fatal(err)
	}
	if _, err := f.verify("ver-1", c.ChallengeID, order, a.Get(c.Options, origin)); refusal(err) != "CREDENTIAL_REVOKED" {
		t.Fatalf("revoked after the challenge: %v", err)
	}
	f.core.CloseChallenge(bgA, c.ChallengeID, "close-1")
	if _, err := f.challenge("req-2", order, "approve"); refusal(err) != "CREDENTIAL_REVOKED" {
		t.Fatalf("new challenge: %v", err)
	}
}

func TestApprovalsHandler(t *testing.T) {
	f := newAFX(t)
	a := f.enroll("approver-a", nil)
	h := ApprovalsHandler(f.core, func(string) {})
	do := func(uid uint32, method, path, body string) (int, ApprovalReply) {
		req := httptest.NewRequest(method, path, strings.NewReader(body)).WithContext(WithCaller(context.Background(), uid))
		w := httptest.NewRecorder()
		h.ServeHTTP(w, req)
		var r ApprovalReply
		json.Unmarshal(w.Body.Bytes(), &r)
		return w.Code, r
	}
	for _, route := range [][2]string{{"POST", "/v1/sign"}, {"GET", "/v1/attempts?request_id=r"}, {"GET", "/v1/attempts/att-1"}, {"POST", "/v1/attempts/att-1/abandon"}} {
		if status, r := do(501, route[0], route[1], `{"request_id":"r","authorization":{"method":"webauthn"}}`); status != http.StatusConflict || r.Error != "PAYMENT_MODE_OFF" {
			t.Fatalf("%v: %d %+v", route, status, r)
		}
	}
	if status, r := do(501, "GET", "/v1/health", ""); status != 200 || r.State != "approvals-only" || r.Payment != "unavailable" {
		t.Fatalf("health %d %+v", status, r)
	}
	if status, r := do(777, "GET", "/v1/health", ""); status != http.StatusForbidden || r.Error != "CALLER_NOT_ALLOWED" {
		t.Fatalf("other uid %d %+v", status, r)
	}
	order := f.order("a")
	body, _ := json.Marshal(ChallengeRequest{RequestID: "req-1", ApprovalSessionID: sessionOf(order), Decision: "approve", Order: order, HostDeadline: f.now + 600})
	status, c := do(501, "POST", "/v1/approvals/challenge", string(body))
	if status != 200 || c.State != "issued" {
		t.Fatalf("challenge %d %+v", status, c)
	}
	if status, r := do(501, "GET", "/v1/approvals/challenge?request_id=req-x", ""); status != http.StatusNotFound || r.Error != "CHALLENGE_UNKNOWN" {
		t.Fatalf("unknown %d %+v", status, r)
	}
	vb, _ := json.Marshal(VerifyRequest{VerifyRequestID: "ver-1", ChallengeID: c.ChallengeID, Order: order, Assertion: a.Get(c.Options, origin)})
	if status, r := do(501, "POST", "/v1/approvals/verify", string(vb)); status != 200 || r.State != "verified" {
		t.Fatalf("verify %d %+v", status, r)
	}
	if status, r := do(501, "GET", "/v1/approvals?verify_request_id=ver-x", ""); status != http.StatusNotFound || r.Error != "VERIFY_UNKNOWN" {
		t.Fatalf("unknown verify %d %+v", status, r)
	}
}

func TestApproverConfig(t *testing.T) {
	var c map[string]any
	base := signertest.ApprovalsConfig("/tmp/s.sock", 501, 1790726450, addrA, addrB, origin)
	for name, edit := range map[string]func(){
		"no webauthn":      func() { delete(c, "webauthn") },
		"IP origin":        func() { c["webauthn"].(map[string]any)["origins"] = []string{"http://127.0.0.1:8402"} },
		"https origin":     func() { c["webauthn"].(map[string]any)["origins"] = []string{"https://localhost:8402"} },
		"rp id":            func() { c["webauthn"].(map[string]any)["rp_id"] = "127.0.0.1" },
		"other's wallet":   func() { c["approvers"].([]any)[0].(map[string]any)["wallet_ids"] = []string{"wallet-b"} },
		"two per consumer": func() { c["approvers"].([]any)[1].(map[string]any)["consumer_id"] = "consumer-a" },
	} {
		json.Unmarshal(base, &c)
		edit()
		raw, _ := json.Marshal(c)
		if _, err := ParseConfig(raw); err == nil {
			t.Fatalf("%s: accepted", name)
		}
	}
	if _, err := ParseConfig(base); err != nil {
		t.Fatal(err)
	}
}
