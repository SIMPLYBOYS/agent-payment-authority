package store

import (
	"database/sql"
	"encoding/json"
	"errors"
	"path/filepath"
	"strings"
	"testing"

	"github.com/SIMPLYBOYS/agent-payment-authority/internal/credential"
	"github.com/SIMPLYBOYS/agent-payment-authority/internal/paycontract"
	"github.com/SIMPLYBOYS/agent-payment-authority/internal/policy"
)

// v2env is env with a credential directory of synthetic test keys (only
// consumer-a has a registered holder) and the Store as its verifier.
type v2env struct {
	*env
	dir *credential.Dir
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
	return &v2env{e, d}
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
	st, ok, err := v.dir.IssueTask(task, consumer, int64(v.clock.clock()())+MaxCredentialTTL)
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
