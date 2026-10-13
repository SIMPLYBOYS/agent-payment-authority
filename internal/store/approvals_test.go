package store

// A2 in the Store (docs/commercial-human-approval-design.md 6.2, 7): the
// Host's approval attempts and the mirror of the Signer's verified
// decision. The Signer itself is not here: results are given as it would
// answer them.

import (
	"errors"
	"strings"
	"testing"
)

func wa(consumer, req, decision, signer, intent string) WebAuthnResult {
	return WebAuthnResult{ConsumerID: consumer, RequestID: req, Decision: decision, SignerApprovalID: signer, ApproverID: "approver-a",
		CredentialRef: "sha256:" + strings.Repeat("c", 64), IntentHash: intent, ExpiresAt: t0 + 300}
}

func begin(t *testing.T, s *Store, consumer, session, req, decision string) AttemptStart {
	t.Helper()
	st, err := s.BeginApprovalAttempt(AttemptRequest{ConsumerID: consumer, SessionID: session, RequestID: req, Decision: decision})
	if err != nil {
		t.Fatalf("begin %s: %v", req, err)
	}
	return st
}

func TestAttemptLifecycle(t *testing.T) {
	e := newEnv(t)
	s := e.open()
	id, intent := e.payTask(s, "task-a", 60)
	st := begin(t, s, "consumer-a", id, "att-req-00000001", "approve")
	if st.Kind != "new" || st.Attempt.State != "requested" || st.IntentHash != intent || st.Deadline == 0 || len(st.Order) == 0 {
		t.Fatalf("new attempt %+v", st)
	}
	// A resend of the request ID answers its own attempt; another page with
	// the same decision shares it; the other decision is a conflict.
	if r := begin(t, s, "consumer-a", id, "att-req-00000001", "approve"); r.Kind != "replay" {
		t.Fatalf("resend %+v", r)
	}
	if r := begin(t, s, "consumer-a", id, "att-req-00000002", "approve"); r.Kind != "shared" || r.Attempt.RequestID != "att-req-00000001" {
		t.Fatalf("second page %+v", r)
	}
	r, err := s.BeginApprovalAttempt(AttemptRequest{ConsumerID: "consumer-a", SessionID: id, RequestID: "att-req-00000003", Decision: "deny"})
	if Code(err) != "DECISION_CONFLICT" || r.Attempt.Decision != "approve" || r.Attempt.RequestID != "att-req-00000001" {
		t.Fatalf("other decision: %+v %v", r, err)
	}
	if _, err := s.BeginApprovalAttempt(AttemptRequest{ConsumerID: "consumer-a", SessionID: id, RequestID: "att-req-00000001", Decision: "deny"}); Code(err) != "IDEMPOTENCY_CONFLICT" {
		t.Fatalf("request ID reused for another decision: %v", err)
	}
	// Another consumer sees nothing.
	if _, err := s.BeginApprovalAttempt(AttemptRequest{ConsumerID: "consumer-b", SessionID: id, RequestID: "att-req-00000004", Decision: "approve"}); Code(err) != "OWNERSHIP_MISMATCH" {
		t.Fatalf("other consumer: %v", err)
	}
	if _, _, err := s.ApprovalAttemptOf("consumer-b", "att-req-00000001"); Code(err) != "OWNERSHIP_MISMATCH" {
		t.Fatalf("other consumer reads: %v", err)
	}
	// challenged -> verifying -> verified (the Signer's answer), then the
	// session is approved with the webauthn grant, once.
	up := func(from []string, state string, u AttemptUpdate) ApprovalAttempt {
		t.Helper()
		u.ConsumerID, u.RequestID, u.From, u.State = "consumer-a", "att-req-00000001", from, state
		a, err := s.UpdateApprovalAttempt(u)
		if err != nil {
			t.Fatalf("%s: %v", state, err)
		}
		return a
	}
	up([]string{"requested"}, "challenged", AttemptUpdate{ChallengeID: "chl-1"})
	if _, err := s.UpdateApprovalAttempt(AttemptUpdate{ConsumerID: "consumer-a", RequestID: "att-req-00000001", From: []string{"challenged"}, State: "challenged", ChallengeID: "chl-2"}); Code(err) != "ATTEMPT_STATE" {
		t.Fatalf("challenge changed: %v", err)
	}
	up([]string{"challenged"}, "verifying", AttemptUpdate{VerifyRequestID: "ver-1"})
	res, err := s.ResolveWebAuthn(wa("consumer-a", "att-req-00000001", "approve", "hap-1", intent))
	if err != nil || res.Outcome != "approved" || res.Grant == nil || res.Grant.AuthorizationMethod != "webauthn" || res.Grant.SignerApprovalID != "hap-1" ||
		res.Grant.ApproverID != "webauthn:approver-a" || res.Grant.ExpiresAt != t0+300 {
		t.Fatalf("resolve %+v %v", res, err)
	}
	if again, err := s.ResolveWebAuthn(wa("consumer-a", "att-req-00000001", "approve", "hap-1", intent)); err != nil || !again.Replay || again.Outcome != "approved" {
		t.Fatalf("resend %+v %v", again, err)
	}
	if _, err := s.ResolveWebAuthn(wa("consumer-a", "att-req-00000001", "approve", "hap-2", intent)); Code(err) != "IDEMPOTENCY_CONFLICT" {
		t.Fatalf("another Signer approval: %v", err)
	}
	types := eventTypes(t, s, sessA)
	if strings.Join(types[len(types)-2:], ",") != "APPROVED,GRANT_ISSUED" {
		t.Fatalf("events %v", types)
	}
	if item, err := s.ApprovalOf("consumer-a", id); err != nil || item.Attempt == nil || item.Attempt.State != "verified" || item.Purchase.Approval.State != "approved" ||
		item.Purchase.Approval.ResolvedBy != "webauthn:approver-a" {
		t.Fatalf("item %+v %v", item, err)
	}
	s.Close()
	e.open() // the recovery check passes
}

func TestAttemptDenyAndMismatch(t *testing.T) {
	e := newEnv(t)
	s := e.open()
	id, intent := e.payTask(s, "task-a", 60)
	begin(t, s, "consumer-a", id, "att-req-00000001", "deny")
	// The Signer's answer is of the other decision, or another intent: the
	// session does not change; the attempt is refused.
	if r, err := s.ResolveWebAuthn(wa("consumer-a", "att-req-00000001", "approve", "hap-1", intent)); err != nil || r.Outcome != "mismatch" || r.Purchase.Approval.State != "pending" {
		t.Fatalf("decision mismatch %+v %v", r, err)
	}
	a, _, _ := s.ApprovalAttemptOf("consumer-a", "att-req-00000001")
	if a.State != "refused" || a.ErrorCode != "DECISION_MISMATCH" {
		t.Fatalf("attempt after a mismatch %+v", a)
	}
	// Refused with a challenge is still open: close it at the Signer (closed) first.
	if _, err := s.UpdateApprovalAttempt(AttemptUpdate{ConsumerID: "consumer-a", RequestID: "att-req-00000001", From: []string{"refused"}, State: "refused", ChallengeID: "chl-1"}); err != nil {
		t.Fatal(err)
	}
	if _, err := s.BeginApprovalAttempt(AttemptRequest{ConsumerID: "consumer-a", SessionID: id, RequestID: "att-req-00000002", Decision: "approve"}); Code(err) != "DECISION_CONFLICT" {
		t.Fatalf("refused with a challenge still holds the session: %v", err)
	}
	if _, err := s.UpdateApprovalAttempt(AttemptUpdate{ConsumerID: "consumer-a", RequestID: "att-req-00000001", From: []string{"refused"}, State: "closed", CloseRequestID: "close-1"}); err != nil {
		t.Fatal(err)
	}
	begin(t, s, "consumer-a", id, "att-req-00000003", "deny")
	r, err := s.ResolveWebAuthn(wa("consumer-a", "att-req-00000003", "deny", "hap-3", intent))
	if err != nil || r.Outcome != "denied" || r.Grant != nil || r.Purchase.Approval.State != "denied" || count(t, e.path, "grants", "") != 0 {
		t.Fatalf("deny %+v %v", r, err)
	}
	s.Close()
	e.open()
}

// The session closes first (its deadline, or the host's cancel): the
// Signer's later result is recorded on the attempt only, and the caller
// is told to abandon it.
func TestAttemptAfterTheSessionClosed(t *testing.T) {
	for _, c := range []struct {
		name  string
		close func(e *env, s *Store)
		state string
	}{
		{"deadline", func(e *env, s *Store) { e.clock.Add(60) }, "expired"},
		{"cancel", func(e *env, s *Store) {
			if err := s.Cancel("task-a"); err != nil {
				e.t.Fatal(err)
			}
		}, "withdrawn"},
	} {
		t.Run(c.name, func(t *testing.T) {
			e := newEnv(t)
			s := e.open()
			id, intent := e.payTask(s, "task-a", 60)
			begin(t, s, "consumer-a", id, "att-req-00000001", "approve")
			c.close(e, s)
			r, err := s.ResolveWebAuthn(wa("consumer-a", "att-req-00000001", "approve", "hap-1", intent))
			if err != nil || r.Outcome != "closed" || r.Purchase.Approval.State != c.state || count(t, e.path, "grants", "") != 0 {
				t.Fatalf("%+v %v", r, err)
			}
			a, _, _ := s.ApprovalAttemptOf("consumer-a", "att-req-00000001")
			if a.State != "verified" || a.SignerApprovalID != "hap-1" {
				t.Fatalf("attempt %+v", a)
			}
			if _, err := s.BeginApprovalAttempt(AttemptRequest{ConsumerID: "consumer-a", SessionID: id, RequestID: "att-req-00000002", Decision: "approve"}); Code(err) != "SESSION_CLOSED" {
				t.Fatalf("new attempt on a closed session: %v", err)
			}
			s.Close()
			e.open()
		})
	}
}

// Old data is never raised: a simulated grant cannot become webauthn, and
// a webauthn grant needs its session's verified attempt (recovery check).
func TestApprovalSchemaChecks(t *testing.T) {
	e := newEnv(t)
	s := e.open()
	id, intent := e.payTask(s, "task-a", 60)
	if _, err := approve(s, id, intent, "approve-req-000000001", "consumer-a", "approve"); err != nil {
		t.Fatal(err)
	}
	s.Close()
	db := rawDB(t, e.path)
	if _, err := db.Exec(`UPDATE grants SET authorization_method = 'webauthn'`); err == nil {
		t.Fatal("a simulated grant became webauthn")
	}
	if _, err := db.Exec(`UPDATE grants SET signer_approval_id = 'hap-x'`); err == nil {
		t.Fatal("a simulated grant took a Signer approval")
	}
	// A webauthn session whose attempt is not verified: refused on open.
	e2 := newEnv(t)
	s2 := e2.open()
	id2, intent2 := e2.payTask(s2, "task-a", 60)
	begin(t, s2, "consumer-a", id2, "att-req-00000001", "approve")
	if _, err := s2.ResolveWebAuthn(wa("consumer-a", "att-req-00000001", "approve", "hap-1", intent2)); err != nil {
		t.Fatal(err)
	}
	s2.Close()
	for _, q := range []string{
		`UPDATE approval_attempts SET state = 'refused', signer_approval_id = NULL, error_code = 'X'`,
		`UPDATE grants SET signer_approval_id = 'hap-other'`,
	} {
		cp := e2.path + ".copy"
		copyFile(e2.path, cp)
		if _, err := rawDB(t, cp).Exec(q); err != nil {
			t.Fatalf("%s: %v", q, err)
		}
		_, err := Open(Config{Path: cp, Registry: e2.reg, Offers: e2.offers, Clock: e2.clock.clock()})
		var ce *CorruptError
		if !errors.As(err, &ce) || ce.Check != "approval" {
			t.Fatalf("%s: Open %v", q, err)
		}
	}
}

// A schema 4 file (P1-b) with a simulated grant upgrades to 5 unchanged.
func TestUpgradeSchema4(t *testing.T) {
	e := newEnv(t)
	s := e.open()
	id, intent := e.payTask(s, "task-a", 30)
	if _, err := approve(s, id, intent, "approve-req-000000001", "consumer-a", "approve"); err != nil {
		t.Fatal(err)
	}
	s.Close()
	toSchema4(t, e.path)
	want := fingerprint(t, e.path)
	if _, err := openOpt(e, false); Code(err) != "SCHEMA_UPGRADE_REQUIRED" {
		t.Fatalf("without the flag: %v", err)
	}
	s, err := openOpt(e, true)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	if userVersion(t, e.path) != 5 || fingerprint(t, e.path) != want || count(t, e.path, "grants", "authorization_method = 'simulated' AND signer_approval_id IS NULL") != 1 {
		t.Fatal("the upgrade changed simulated data")
	}
}
