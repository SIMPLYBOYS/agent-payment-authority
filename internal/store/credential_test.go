package store

import (
	"database/sql"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/SIMPLYBOYS/agent-payment-authority/internal/contracts"
	"github.com/SIMPLYBOYS/agent-payment-authority/internal/credential"
	"github.com/SIMPLYBOYS/agent-payment-authority/internal/paycontract"
	"github.com/SIMPLYBOYS/agent-payment-authority/internal/policy"
)

// v2env is env with a credential directory of synthetic test keys (only
// consumer-a has a registered holder) and the Store as its verifier.
type v2env struct {
	*env
	dir     *credential.Dir
	credDir string
	ttl     uint64 // the credential's lifetime from the task's start
}

func newV2(t *testing.T) *v2env {
	t.Helper()
	e := newEnv(t)
	path := filepath.Join(t.TempDir(), "cred")
	if err := credential.InitDir(path, "issuer-test", []string{"consumer-a"}, t0); err != nil {
		t.Fatal(err)
	}
	d, err := credential.OpenDir(path)
	if err != nil {
		t.Fatal(err)
	}
	return &v2env{e, d, path, MaxCredentialTTL}
}

func (v *v2env) config() Config {
	return Config{Path: v.path, Registry: v.reg, Offers: v.offers, Clock: v.clock.clock(), Credentials: v.dir, VerifierID: "host-test"}
}

func (v *v2env) open() *Store {
	v.t.Helper()
	s, err := Open(v.config())
	if err != nil {
		v.t.Fatalf("Open: %v", err)
	}
	v.t.Cleanup(func() { s.Close() })
	return s
}

// setupV2 is task's apa.pay.v2 setup as the Host builds it: a v2
// delegation, then the credential issued for 24 h (absent without a
// registered holder).
func (v *v2env) setupV2(s *Store, task string) TaskSetup {
	v.t.Helper()
	consumer := map[string]string{"task-a": "consumer-a", "task-a-next": "consumer-a", "task-b": "consumer-b"}[task]
	d, _, err := s.ConfirmDelegation(DelegationRequest{ContractVersion: paycontract.V2, RequestID: "dlg-req-v2-" + task + "-00000", ConsumerID: consumer, Scenario: "pay",
		Purpose: "a brief", AssetID: v.profile.AssetID, Merchants: v.profile.Merchants, TaskBudget: "5000000", AllowMax: "100000", ApprovalDeadlineSeconds: 30,
		ValidSeconds: 600, ConfirmedBy: "simulated:" + consumer + ":host-test"})
	if err != nil {
		v.t.Fatalf("ConfirmDelegation: %v", err)
	}
	p := v.profile
	setup := TaskSetup{TaskID: task, Profile: &p, ContractVersion: paycontract.V2, DelegationID: d.DelegationID}
	st, ok, err := v.dir.IssueTask(task, consumer, int64(v.clock.clock()()+v.ttl))
	if err != nil {
		v.t.Fatal(err)
	}
	if !ok {
		setup.CredentialAbsent = true
		return setup
	}
	if setup.BBS, err = NewBBSCredential(v.dir, st); err != nil {
		v.t.Fatal(err)
	}
	return setup
}

// C2b, design 5 and 8: a v2 task starts with its credential_bbs snapshot, or
// absent without a registered holder; it reopens; Submit refuses it.
func TestStartV2(t *testing.T) {
	v := newV2(t)
	s := v.open()
	a := v.setupV2(s, "task-a")
	if err := s.StartTask(a); err != nil {
		t.Fatal(err)
	}
	b := v.setupV2(s, "task-b")
	if b.BBS != nil || !b.CredentialAbsent {
		t.Fatal("consumer-b has no registered holder")
	}
	if err := s.StartTask(b); err != nil {
		t.Fatal(err)
	}
	s.Close()
	s = v.open()
	var kind, state string
	db := rawDB(t, v.path)
	if err := db.QueryRow(`SELECT c.kind, json_extract(m.body, '$.credential_state') FROM tasks t JOIN config_snapshots c ON c.snapshot_hash = t.credential_snapshot
		JOIN config_snapshots m ON m.snapshot_hash = t.terms_snapshot WHERE t.task_id = 'task-a'`).Scan(&kind, &state); err != nil || kind != "credential_bbs" || state != "present" {
		t.Fatalf("task-a: %s %s %v", kind, state, err)
	}
	if err := db.QueryRow(`SELECT json_extract(m.body, '$.credential_state') FROM tasks t JOIN config_snapshots m ON m.snapshot_hash = t.terms_snapshot
		WHERE t.task_id = 'task-b' AND t.credential_snapshot IS NULL`).Scan(&state); err != nil || state != "absent" {
		t.Fatalf("task-b: %s %v", state, err)
	}
	po := v1Of(t, v.base)
	po.Version, po.Intent.Version = paycontract.V2, paycontract.V2
	body, _ := json.Marshal(po)
	if _, err := s.Submit(sessA, body); Code(err) != "CONTRACT_VERSION_MISMATCH" {
		t.Fatalf("Submit took a purchase in a v2 task: %v", err)
	}
}

func TestStartV2Refuses(t *testing.T) {
	for name, c := range map[string]struct {
		mut  func(v *v2env, s *Store, setup *TaskSetup)
		code string
	}{
		"a fixture credential": {func(v *v2env, s *Store, setup *TaskSetup) {
			setup.BBS, setup.Credential = nil, &policy.Credential{ConsumerID: "consumer-a", ValidUntil: t0 + 86400, EvidenceMode: "fixture"}
		}, "INVALID_TASK_SETUP"},
		"a v1 delegation": {func(v *v2env, s *Store, setup *TaskSetup) {
			setup.DelegationID = v.delegation(s, "consumer-a", "dlg-req-v1-task-a-0000", 30)
		}, "DELEGATION_UNAVAILABLE"},
		"a credential in a v1 task": {func(v *v2env, s *Store, setup *TaskSetup) {
			setup.ContractVersion, setup.DelegationID = paycontract.Version, v.delegation(s, "consumer-a", "dlg-req-v1-task-a-0000", 30)
		}, "INVALID_TASK_SETUP"},
		"both credentials": {func(v *v2env, s *Store, setup *TaskSetup) {
			setup.Credential = &policy.Credential{ConsumerID: "consumer-a", ValidUntil: t0 + 86400, EvidenceMode: "fixture"}
		}, "INVALID_TASK_SETUP"},
		"absent not declared":       {func(v *v2env, s *Store, setup *TaskSetup) { setup.BBS = nil }, "INVALID_TASK_SETUP"},
		"another task's credential": {func(v *v2env, s *Store, setup *TaskSetup) { setup.BBS.TaskID = "task-a-next" }, "INVALID_TASK_SETUP"},
		"past the TTL":              {func(v *v2env, s *Store, setup *TaskSetup) { setup.BBS.Expiry++ }, "INVALID_TASK_SETUP"},
		"already expired": {func(v *v2env, s *Store, setup *TaskSetup) {
			setup.BBS.Expiry = t0
		}, "INVALID_TASK_SETUP"},
	} {
		t.Run(name, func(t *testing.T) {
			v := newV2(t)
			s := v.open()
			setup := v.setupV2(s, "task-a")
			c.mut(v, s, &setup)
			if err := s.StartTask(setup); Code(err) != c.code {
				t.Fatalf("%s: %v", name, err)
			}
		})
	}
	// Without the Store's credential directory no v2 task starts.
	v := newV2(t)
	s := v.open()
	setup := v.setupV2(s, "task-a")
	s.Close()
	plain := v.env.open()
	if err := plain.StartTask(setup); Code(err) != "INVALID_TASK_SETUP" {
		t.Fatalf("no credential directory: %v", err)
	}
}

// Design 8: a v2 task with a fixture snapshot, a v1 task with a
// credential_bbs one, or a snapshot of another task does not reopen.
func TestV2SnapshotRecovery(t *testing.T) {
	swap := `UPDATE tasks SET credential_snapshot = (SELECT credential_snapshot FROM tasks WHERE task_id = ?) WHERE task_id = ?`
	for name, mut := range map[string]func(db *sql.DB) error{
		"a v2 task with the fixture":     func(db *sql.DB) error { _, err := db.Exec(swap, "task-b", "task-a"); return err },
		"an M1 task with credential_bbs": func(db *sql.DB) error { _, err := db.Exec(swap, "task-a", "task-b"); return err },
		"another task's snapshot": func(db *sql.DB) error { // well-formed and hashed, for task-a-next
			var body string
			if err := db.QueryRow(`SELECT body FROM config_snapshots WHERE kind = 'credential_bbs'`).Scan(&body); err != nil {
				return err
			}
			body = strings.Replace(body, `"task_id":"task-a"`, `"task_id":"task-a-next"`, 1)
			h := digest([]byte(body))
			if _, err := db.Exec(`INSERT INTO config_snapshots VALUES (?, 'credential_bbs', ?, ?)`, h, body, t0); err != nil {
				return err
			}
			_, err := db.Exec(`UPDATE tasks SET credential_snapshot = ? WHERE task_id = 'task-a'`, h)
			return err
		},
	} {
		t.Run(name, func(t *testing.T) {
			v := newV2(t)
			s := v.open()
			if err := s.StartTask(v.setupV2(s, "task-a")); err != nil {
				t.Fatal(err)
			}
			if err := s.StartTask(v.setup("task-b")); err != nil { // an apa.m1.v2 task with the fixture
				t.Fatal(err)
			}
			s.Close()
			db := rawDB(t, v.path)
			if _, err := db.Exec(`PRAGMA foreign_keys = OFF`); err != nil {
				t.Fatal(err)
			}
			if err := mut(db); err != nil {
				t.Fatal(err)
			}
			var ce *CorruptError
			if _, err := Open(v.config()); !errors.As(err, &ce) {
				t.Fatalf("Open: %v", err)
			}
		})
	}
}

// v2Of is v1Of under apa.pay.v2, keeping m's nonce.
func v2Of(t *testing.T, m contracts.PurchaseOrder) paycontract.PurchaseOrder {
	t.Helper()
	o := v1Of(t, m)
	sp, _ := paycontract.SpecOf(paycontract.V2)
	o.Version, o.Intent.Version, o.Intent.Domain, o.PaymentBinding.Nonce = paycontract.V2, paycontract.V2, paycontract.DomainV2, m.Intent.EIP3009Nonce
	var err error
	if o.Intent.ResourceHash, err = sp.ResourceHash(o.Intent.HTTPMethod, o.Intent.ResourceURL); err != nil {
		t.Fatal(err)
	}
	if o.Intent.PaymentRequirementHash, err = sp.RequirementHash(o.Quote); err != nil {
		t.Fatal(err)
	}
	return o
}

// present is the Host's holder answering ch for task (consumer-a's).
func (v *v2env) present(ch Challenge) *credential.Presentation {
	v.t.Helper()
	st, err := v.dir.ReadCredential(ch.TaskID)
	if err != nil {
		v.t.Fatal(err)
	}
	hk, err := v.dir.HolderKey("consumer-a")
	if err != nil {
		v.t.Fatal(err)
	}
	_, pk, _ := v.dir.IssuerPublic()
	p, err := credential.Present(st.Credential, st.Signature, pk, hk, ch.Request())
	if err != nil {
		v.t.Fatal(err)
	}
	return &p
}

// buy is the Host's in-Commit sequence (design 7): challenge, present, submit.
func (v *v2env) buy(s *Store, sess contracts.Session, o paycontract.PurchaseOrder) (Result, error) {
	v.t.Helper()
	body, _ := json.Marshal(o)
	ch, ok, err := s.IssueChallenge(sess, body)
	if err != nil {
		return Result{}, err
	}
	var pr Presented
	if ok {
		pr.Presentation = v.present(ch)
	}
	return s.SubmitV2(sess, body, pr)
}

// started is a Store with task-a started as apa.pay.v2 (consumer-a, with a
// credential) and task-b (consumer-b, absent).
func (v *v2env) started() *Store {
	v.t.Helper()
	s := v.open()
	for _, task := range []string{"task-a", "task-b"} {
		if err := s.StartTask(v.setupV2(s, task)); err != nil {
			v.t.Fatal(err)
		}
	}
	return s
}

func verification(t *testing.T, path, po string) (result, detail string, challenge sql.NullString) {
	t.Helper()
	if err := rawDB(t, path).QueryRow(`SELECT result, detail, challenge_id FROM credential_verifications WHERE purchase_order_id = ?`, po).
		Scan(&result, &detail, &challenge); err != nil {
		t.Fatal(err)
	}
	return
}

// C2b, design 3.3 and 7: a valid credential lets the rules decide; the
// three events are written in order with the verification record; the
// challenge is used; a resend is the stored purchase; the file reopens.
func TestSubmitV2(t *testing.T) {
	v := newV2(t)
	s := v.started()
	allow, ask := v2Of(t, v.allow), v2Of(t, v.base)
	for _, c := range []struct {
		o               paycontract.PurchaseOrder
		verdict, reason string
	}{{allow, "Allow", "WITHIN_ALLOW_MAX"}, {ask, "Ask", "HUMAN_APPROVAL_REQUIRED"}} {
		r, err := v.buy(s, sessA, c.o)
		if err != nil || r.Replay || r.Purchase.Decision.PolicyVerdict != c.verdict || r.Purchase.Decision.Reason != c.reason {
			t.Fatalf("%s: %+v %v", c.verdict, r.Purchase.Decision, err)
		}
		if res, detail, chl := verification(t, v.path, c.o.Intent.PurchaseOrderID); res != "valid" || detail != "" || !chl.Valid {
			t.Fatalf("%s: verification %s %s %v", c.verdict, res, detail, chl)
		}
	}
	got := strings.Join(eventTypes(t, s, sessA), " ")
	want := "TASK_STARTED DELEGATION_BOUND PURCHASE_PROPOSED CREDENTIAL_VERIFIED POLICY_DECIDED PURCHASE_PROPOSED CREDENTIAL_VERIFIED POLICY_DECIDED APPROVAL_SESSION_OPENED"
	if got != want {
		t.Fatalf("events:\n got %s\nwant %s", got, want)
	}
	var issued int
	rawDB(t, v.path).QueryRow(`SELECT count(*) FROM credential_challenges WHERE state != 'used'`).Scan(&issued)
	if issued != 0 {
		t.Fatalf("%d challenges not used", issued)
	}
	if r, err := v.buy(s, sessA, allow); err != nil || !r.Replay {
		t.Fatalf("resend: %+v %v", r, err)
	}
	body, _ := json.Marshal(allow)
	if _, err := s.SubmitV2(sessB, body, Presented{}); Code(err) != "OWNERSHIP_MISMATCH" {
		t.Fatalf("another consumer's session: %v", err)
	}
	s.Close()
	v.open()
}

// Each case changes one thing; the purchase is decided Deny with the
// credential's reason, and its record says why (design 3.3, 4, 6, 7).
func TestSubmitV2Denies(t *testing.T) {
	for name, c := range map[string]struct {
		sess                   contracts.Session
		run                    func(t *testing.T, v *v2env, s *Store, body []byte) (Result, error)
		result, detail, reason string
		challenge              bool
	}{
		"no registered holder": {sessB, nil, "absent", "", "CONFIG_MISSING", false},
		"holder material unavailable": {sessA, func(t *testing.T, v *v2env, s *Store, body []byte) (Result, error) {
			if _, _, err := s.IssueChallenge(sessA, body); err != nil {
				t.Fatal(err)
			}
			return s.SubmitV2(sessA, body, Presented{Unavailable: true})
		}, "invalid", "HOLDER_MATERIAL_UNAVAILABLE", "CREDENTIAL_INVALID", false},
		"no challenge": {sessA, func(t *testing.T, v *v2env, s *Store, body []byte) (Result, error) {
			return s.SubmitV2(sessA, body, Presented{Presentation: &credential.Presentation{ChallengeID: "chl-none"}})
		}, "invalid", "CHALLENGE_MISSING", "CREDENTIAL_INVALID", false},
		"challenge expired": {sessA, func(t *testing.T, v *v2env, s *Store, body []byte) (Result, error) {
			ch, _, _ := s.IssueChallenge(sessA, body)
			p := v.present(ch)
			v.clock.Add(ChallengeSeconds)
			return s.SubmitV2(sessA, body, Presented{Presentation: p})
		}, "invalid", "CHALLENGE_EXPIRED", "CREDENTIAL_INVALID", true},
		"a presentation for another challenge": {sessA, func(t *testing.T, v *v2env, s *Store, body []byte) (Result, error) {
			ch, _, _ := s.IssueChallenge(sessA, body)
			ch.ID = "chl-replayed"
			return s.SubmitV2(sessA, body, Presented{Presentation: v.present(ch)})
		}, "invalid", "CHALLENGE_MISMATCH", "CREDENTIAL_INVALID", true},
		"the challenge's intent is not the order's": {sessA, func(t *testing.T, v *v2env, s *Store, body []byte) (Result, error) {
			ch, _, _ := s.IssueChallenge(sessA, body)
			if _, err := rawDB(t, v.path).Exec(`UPDATE credential_challenges SET intent_hash = 'sha256:other'`); err != nil {
				t.Fatal(err)
			}
			return s.SubmitV2(sessA, body, Presented{Presentation: v.present(ch)})
		}, "invalid", "INTENT_MISMATCH", "CREDENTIAL_INVALID", true},
		"a tampered proof": {sessA, func(t *testing.T, v *v2env, s *Store, body []byte) (Result, error) {
			ch, _, _ := s.IssueChallenge(sessA, body)
			p := v.present(ch)
			p.Proof[len(p.Proof)-1] ^= 1
			return s.SubmitV2(sessA, body, Presented{Presentation: p})
		}, "invalid", "PROOF_INVALID", "CREDENTIAL_INVALID", true},
		"revoked": {sessA, func(t *testing.T, v *v2env, s *Store, body []byte) (Result, error) {
			st, _ := v.dir.ReadCredential("task-a")
			if err := v.dir.Revoke(st.Credential["status"], t0); err != nil {
				t.Fatal(err)
			}
			return v.buyRaw(s, sessA, body)
		}, "revoked", "", "CREDENTIAL_REVOKED", true},
		"no status list": {sessA, func(t *testing.T, v *v2env, s *Store, body []byte) (Result, error) {
			os.Remove(filepath.Join(v.credDir, "status.json"))
			return v.buyRaw(s, sessA, body)
		}, "status_unavailable", "STATUS_LIST_UNAVAILABLE", "CREDENTIAL_STATUS_UNAVAILABLE", true},
		"expired": {sessA, func(t *testing.T, v *v2env, s *Store, body []byte) (Result, error) {
			v.clock.Add(10)
			return v.buyRaw(s, sessA, body)
		}, "expired", "", "AUTHORIZATION_EXPIRED", true},
	} {
		t.Run(name, func(t *testing.T) {
			v := newV2(t)
			if name == "expired" {
				v.ttl = 10
			}
			s := v.started()
			o := v2Of(t, forTask(t, v.allow, c.sess.TaskID))
			body, _ := json.Marshal(o)
			run := c.run
			if run == nil {
				run = func(t *testing.T, v *v2env, s *Store, body []byte) (Result, error) { return v.buyRaw(s, c.sess, body) }
			}
			r, err := run(t, v, s, body)
			if err != nil || r.Purchase.Decision.PolicyVerdict != "Deny" || r.Purchase.Decision.Reason != c.reason {
				t.Fatalf("%s: %+v %v", name, r.Purchase.Decision, err)
			}
			if res, detail, chl := verification(t, v.path, o.Intent.PurchaseOrderID); res != c.result || detail != c.detail || chl.Valid != c.challenge {
				t.Fatalf("%s: verification %s/%s challenge %v", name, res, detail, chl)
			}
			s.Close()
			v.open()
		})
	}
}

func (v *v2env) buyRaw(s *Store, sess contracts.Session, body []byte) (Result, error) {
	var o paycontract.PurchaseOrder
	if err := json.Unmarshal(body, &o); err != nil {
		v.t.Fatal(err)
	}
	return v.buy(s, sess, o)
}

// A list older than one already seen is not used (design 6): rollback.
func TestSubmitV2StatusRollback(t *testing.T) {
	v := newV2(t)
	s := v.started()
	old, err := os.ReadFile(filepath.Join(v.credDir, "status.json"))
	if err != nil {
		t.Fatal(err)
	}
	if err := v.dir.Refresh(t0); err != nil {
		t.Fatal(err)
	}
	if r, err := v.buy(s, sessA, v2Of(t, v.allow)); err != nil || r.Purchase.Decision.PolicyVerdict != "Allow" {
		t.Fatalf("first: %+v %v", r.Purchase.Decision, err)
	}
	if err := os.WriteFile(filepath.Join(v.credDir, "status.json"), old, 0o600); err != nil {
		t.Fatal(err)
	}
	o := v2Of(t, variant(t, v.allow, 51))
	r, err := v.buy(s, sessA, o)
	if err != nil || r.Purchase.Decision.Reason != "CREDENTIAL_STATUS_UNAVAILABLE" {
		t.Fatalf("rolled back: %+v %v", r.Purchase.Decision, err)
	}
	if res, detail, _ := verification(t, v.path, o.Intent.PurchaseOrderID); res != "status_unavailable" || detail != "STATUS_LIST_ROLLBACK" {
		t.Fatalf("verification %s/%s", res, detail)
	}
}

// Refusals decide nothing: no presentation for a task with a credential,
// a v2 order through Submit or into a v1 task.
func TestSubmitV2Refuses(t *testing.T) {
	v := newV2(t)
	s := v.started()
	body, _ := json.Marshal(v2Of(t, v.allow))
	if _, _, err := s.IssueChallenge(sessA, body); err != nil {
		t.Fatal(err)
	}
	if _, err := s.SubmitV2(sessA, body, Presented{}); Code(err) != "PRESENTATION_REQUIRED" {
		t.Fatalf("no presentation: %v", err)
	}
	var n int
	rawDB(t, v.path).QueryRow(`SELECT count(*) FROM credential_challenges WHERE state = 'issued'`).Scan(&n)
	if n != 1 {
		t.Fatalf("the refusal changed the challenge: %d issued", n)
	}
	if _, err := s.Submit(sessA, body); Code(err) != "CONTRACT_VERSION_MISMATCH" {
		t.Fatalf("Submit: %v", err)
	}
	v1body, _ := json.Marshal(v1Of(t, v.allow))
	if _, err := s.SubmitV2(sessA, v1body, Presented{}); Code(err) != "CONTRACT_VERSION_MISMATCH" {
		t.Fatalf("a v1 order: %v", err)
	}
	if len(eventTypes(t, s, sessA)) != 2 {
		t.Fatal("a refusal wrote events")
	}
}

// Design 7 step 4: one usable challenge per purchase, reused for the same
// intent until it expires; none when the purchase is stored or the
// credential absent.
func TestIssueChallenge(t *testing.T) {
	v := newV2(t)
	s := v.started()
	body, _ := json.Marshal(v2Of(t, v.allow))
	a, ok, err := s.IssueChallenge(sessA, body)
	if err != nil || !ok || a.VerifierID != "host-test" || a.ExpiresAt != t0+ChallengeSeconds || !slices.Equal(a.Disclose, credential.Mandatory) {
		t.Fatalf("first: %+v %t %v", a, ok, err)
	}
	if b, _, _ := s.IssueChallenge(sessA, body); b.ID != a.ID {
		t.Fatal("the same intent got another challenge")
	}
	v.clock.Add(ChallengeSeconds)
	b, _, _ := s.IssueChallenge(sessA, body)
	if b.ID == a.ID {
		t.Fatal("an expired challenge was reused")
	}
	var states string
	rawDB(t, v.path).QueryRow(`SELECT group_concat(state, ' ') FROM (SELECT state FROM credential_challenges ORDER BY issued_at)`).Scan(&states)
	if states != "expired issued" {
		t.Fatalf("states %q", states)
	}
	if _, err := s.SubmitV2(sessA, body, Presented{Presentation: v.present(b)}); err != nil {
		t.Fatal(err)
	}
	if _, ok, err := s.IssueChallenge(sessA, body); ok || err != nil {
		t.Fatalf("a stored purchase got a challenge: %v", err)
	}
	bb, _ := json.Marshal(v2Of(t, forTask(t, v.allow, "task-b")))
	if _, ok, err := s.IssueChallenge(sessB, bb); ok || err != nil {
		t.Fatalf("an absent credential got a challenge: %v", err)
	}
}

// Design 3.3: Open cross-checks the records, events, challenges and the
// status versions seen; a challenge left issued by a failed presentation
// is fine.
func TestV2CredentialRecovery(t *testing.T) {
	prepare := func(t *testing.T) *v2env {
		v := newV2(t)
		s := v.started()
		if _, err := v.buy(s, sessA, v2Of(t, v.allow)); err != nil {
			t.Fatal(err)
		}
		if _, err := v.buy(s, sessB, v2Of(t, forTask(t, v.allow, "task-b"))); err != nil {
			t.Fatal(err)
		}
		body, _ := json.Marshal(v2Of(t, v.base))
		if _, _, err := s.IssueChallenge(sessA, body); err != nil { // never presented
			t.Fatal(err)
		}
		s.Close()
		return v
	}
	v := prepare(t)
	v.open()
	for name, sql := range map[string]string{
		"a record's result":                `UPDATE credential_verifications SET result = 'revoked' WHERE task_id = 'task-a'`,
		"a record's time":                  `UPDATE credential_verifications SET verified_at = verified_at + 1 WHERE task_id = 'task-a'`,
		"a record removed":                 `DELETE FROM credential_verifications WHERE task_id = 'task-a'`,
		"the used challenge's intent":      `UPDATE credential_challenges SET intent_hash = 'sha256:other' WHERE state = 'used'`,
		"the used challenge set back":      `UPDATE credential_challenges SET state = 'expired' WHERE state = 'used'`,
		"the leftover challenge used":      `UPDATE credential_challenges SET state = 'used' WHERE state = 'issued'`,
		"the status versions seen dropped": `DELETE FROM credential_status_seen`,
	} {
		t.Run(name, func(t *testing.T) {
			v := prepare(t)
			db := rawDB(t, v.path)
			if _, err := db.Exec(`PRAGMA foreign_keys = OFF; ` + sql); err != nil {
				t.Fatal(err)
			}
			var ce *CorruptError
			if _, err := Open(v.config()); !errors.As(err, &ce) {
				t.Fatalf("Open: %v", err)
			}
		})
	}
}

// askV2 submits task-a's base purchase (an Ask) with its credential and
// returns its session ID and intent.
func (v *v2env) askV2(s *Store) (string, string) {
	v.t.Helper()
	r, err := v.buy(s, sessA, v2Of(v.t, v.base))
	if err != nil || r.Purchase.Approval == nil {
		v.t.Fatalf("v2 Ask: %+v %v", r.Purchase, err)
	}
	return r.Purchase.Approval.SessionID, r.Purchase.IntentHash
}

// Design 7.1: an approval rechecks the credential (revoked, the list
// unavailable, expired) and, if it no longer holds, withdraws the session
// with that reason and issues no grant; a denial is not rechecked.
func TestV2ApprovalRecheck(t *testing.T) {
	revoke := func(t *testing.T, v *v2env) {
		st, _ := v.dir.ReadCredential("task-a")
		if err := v.dir.Revoke(st.Credential["status"], t0); err != nil {
			t.Fatal(err)
		}
	}
	for name, c := range map[string]struct {
		ttl               uint64
		mut               func(t *testing.T, v *v2env)
		decision, outcome string
		closure, reason   string
	}{
		"still valid":    {MaxCredentialTTL, func(*testing.T, *v2env) {}, "approve", "approved", "approved", "SIMULATED_APPROVAL"},
		"revoked":        {MaxCredentialTTL, revoke, "approve", "closed", "withdrawn", "CREDENTIAL_REVOKED"},
		"no status list": {MaxCredentialTTL, func(t *testing.T, v *v2env) { os.Remove(filepath.Join(v.credDir, "status.json")) }, "approve", "closed", "withdrawn", "CREDENTIAL_STATUS_UNAVAILABLE"},
		"expired":        {20, func(t *testing.T, v *v2env) { v.clock.Add(20) }, "approve", "closed", "withdrawn", "AUTHORIZATION_EXPIRED"},
		"a denial":       {MaxCredentialTTL, revoke, "deny", "denied", "denied", "APPROVER_DENIED"},
	} {
		t.Run(name, func(t *testing.T) {
			v := newV2(t)
			v.ttl = c.ttl
			s := v.started()
			id, intent := v.askV2(s)
			c.mut(t, v)
			r, err := approve(s, id, intent, "approve-req-000000001", "consumer-a", c.decision)
			if err != nil || r.Outcome != c.outcome || (r.Grant != nil) != (c.outcome == "approved") {
				t.Fatalf("%s: %+v %v", name, r, err)
			}
			if r.Purchase.ApprovalClosed != c.closure || lastReason(t, s) != c.reason {
				t.Fatalf("%s: closed %s, reason %s", name, r.Purchase.ApprovalClosed, lastReason(t, s))
			}
			s.Close()
			v.open()
		})
	}
}

// The webauthn path closes the same way: outcome closed, so the Host
// abandons the Signer's approval (design 7.1).
func TestV2WebAuthnRecheck(t *testing.T) {
	v := newV2(t)
	s := v.started()
	id, intent := v.askV2(s)
	begin(t, s, "consumer-a", id, "att-req-00000001", "approve")
	for _, u := range []AttemptUpdate{{From: []string{"requested"}, State: "challenged", ChallengeID: "chl-1"}, {From: []string{"challenged"}, State: "verifying", VerifyRequestID: "ver-1"}} {
		u.ConsumerID, u.RequestID = "consumer-a", "att-req-00000001"
		if _, err := s.UpdateApprovalAttempt(u); err != nil {
			t.Fatal(err)
		}
	}
	st, _ := v.dir.ReadCredential("task-a")
	if err := v.dir.Revoke(st.Credential["status"], t0); err != nil {
		t.Fatal(err)
	}
	r, err := s.ResolveWebAuthn(wa("consumer-a", "att-req-00000001", "approve", "hap-1", intent))
	if err != nil || r.Outcome != "closed" || r.Grant != nil || r.Purchase.ApprovalClosed != "withdrawn" || lastReason(t, s) != "CREDENTIAL_REVOKED" {
		t.Fatalf("%+v %v", r, err)
	}
	s.Close()
	v.open()
}

// A payment starts only while the credential holds; a refusal writes
// nothing (design 7.1).
func TestV2PaymentRecheck(t *testing.T) {
	v := newV2(t)
	s := v.started()
	o := v2Of(t, v.allow)
	if r, err := v.buy(s, sessA, o); err != nil || r.Purchase.Decision.PolicyVerdict != "Allow" {
		t.Fatalf("%+v %v", r.Purchase.Decision, err)
	}
	st, _ := v.dir.ReadCredential("task-a")
	if err := v.dir.Revoke(st.Credential["status"], t0); err != nil {
		t.Fatal(err)
	}
	if _, _, err := s.BeginPayment(payReq(o.Intent.PurchaseOrderID)); Code(err) != "CREDENTIAL_REVOKED" {
		t.Fatalf("revoked: %v", err)
	}
	var n int
	rawDB(t, v.path).QueryRow(`SELECT count(*) FROM payment_attempts`).Scan(&n)
	if n != 0 {
		t.Fatal("a refused payment was recorded")
	}
}

// lastReason is the reason of task-a's last event that closed an Ask.
func lastReason(t *testing.T, s *Store) string {
	t.Helper()
	events, err := s.Events(sessA)
	if err != nil {
		t.Fatal(err)
	}
	for i := len(events) - 1; i >= 0; i-- {
		if d := events[i].Payload.Decision; d != nil && events[i].EventType != "POLICY_DECIDED" {
			return d.Reason
		}
	}
	return ""
}
