package store

// Tests of schema 2's run coordination (docs/demo-operation-design.md
// section 13): migration and upgrade, AcceptRun/EndRun/InterruptOpenRuns,
// the recovery rules of host_runs, VerifyFile, RestoreCopy, and the two
// leftover-WAL cases with an isolated child process.

import (
	"bufio"
	"database/sql"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/SIMPLYBOYS/agent-payment-authority/internal/contracts"
	"github.com/SIMPLYBOYS/agent-payment-authority/internal/policy"
)

// TestMain also runs the leftover-WAL children (APA_STORE_CHILD).
func TestMain(m *testing.M) {
	if c := os.Getenv("APA_STORE_CHILD"); c != "" {
		os.Exit(walChild(c, os.Getenv("APA_STORE_CHILD_DB")))
	}
	os.Exit(m.Run())
}

func runReq(id, consumer string, candidates ...string) RunRequest {
	return RunRequest{RequestID: "req-" + id + strings.Repeat("0", 16), Scenario: "ask", ScenarioDigest: "sha256:x", ConsumerID: consumer, Candidates: candidates}
}

func userVersion(t *testing.T, path string) int {
	t.Helper()
	var v int
	if err := rawDB(t, path).QueryRow(`PRAGMA user_version`).Scan(&v); err != nil {
		t.Fatal(err)
	}
	return v
}

// schema1 writes a task and a purchase through a schema 2 Store, then turns
// the file into schema 1 (the two differ by host_runs only).
func schema1(t *testing.T, e *env) {
	t.Helper()
	s := e.open()
	e.start(s, "task-a")
	submit(t, s, sessA, e.base)
	s.Close()
	db, err := sql.Open("sqlite", e.path)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`DROP TABLE host_runs; PRAGMA user_version = 1`); err != nil {
		t.Fatal(err)
	}
	db.Close()
}

func openOpt(e *env, upgrade bool) (*Store, error) {
	return Open(Config{Path: e.path, Registry: e.reg, Offers: e.offers, Clock: e.clock.clock(), UpgradeSchema1: upgrade})
}

func TestSchemaNewAndRefusedUpgrade(t *testing.T) {
	e := newEnv(t)
	e.open().Close()
	if v := userVersion(t, e.path); v != 2 || count(t, e.path, "host_runs", "") != 0 {
		t.Fatalf("a new database: schema %d", v)
	}
	e2 := newEnv(t)
	schema1(t, e2)
	before, _ := os.ReadFile(e2.path)
	_, err := openOpt(e2, false)
	wantCode(t, err, "SCHEMA_UPGRADE_REQUIRED")
	if after, _ := os.ReadFile(e2.path); string(after) != string(before) || userVersion(t, e2.path) != 1 {
		t.Fatal("a refused schema 1 database changed")
	}
	if m, _ := filepath.Glob(e2.path + ".schema1-*.bak"); len(m) != 0 {
		t.Fatal("a refused open made a backup")
	}
}

func TestUpgradeSchema1(t *testing.T) {
	e := newEnv(t)
	schema1(t, e)
	want := fingerprint(t, e.path)
	s, err := openOpt(e, true)
	if err != nil {
		t.Fatal(err)
	}
	s.Close()
	if userVersion(t, e.path) != 2 || fingerprint(t, e.path) != want {
		t.Fatal("upgrade: not schema 2, or the data changed")
	}
	m, _ := filepath.Glob(e.path + ".schema1-*.bak")
	if len(m) != 1 {
		t.Fatalf("backups %v", m)
	}
	if fi, _ := os.Stat(m[0]); fi.Mode().Perm() != 0o600 {
		t.Errorf("backup mode %v", fi.Mode())
	}
}

// Each failing step of the upgrade (hook) leaves the version the design
// says (section 13.8 R2).
func TestUpgradeFailures(t *testing.T) {
	for _, c := range []struct {
		stage   string
		version int
		backup  bool
	}{{"verified1", 1, false}, {"backed-up", 1, true}, {"migrate", 1, true}, {"verified2", 2, true}} {
		t.Run(c.stage, func(t *testing.T) {
			e := newEnv(t)
			schema1(t, e)
			upgradeHook = func(stage string) error {
				if stage == c.stage {
					return errors.New("injected")
				}
				return nil
			}
			defer func() { upgradeHook = nil }()
			if _, err := openOpt(e, true); err == nil {
				t.Fatal("the upgrade did not fail")
			}
			m, _ := filepath.Glob(e.path + ".schema1-*.bak")
			if userVersion(t, e.path) != c.version || (len(m) == 1) != c.backup {
				t.Fatalf("version %d, backups %v", userVersion(t, e.path), m)
			}
		})
	}
	t.Run("pre-migration verify fails", func(t *testing.T) {
		e := newEnv(t)
		schema1(t, e)
		db := rawDB(t, e.path)
		if _, err := db.Exec(`UPDATE tasks SET status = 'denied'`); err != nil {
			t.Fatal(err)
		}
		db.Close()
		var ce *CorruptError
		if _, err := openOpt(e, true); !errors.As(err, &ce) {
			t.Fatalf("got %v", err)
		}
		if m, _ := filepath.Glob(e.path + ".schema1-*.bak"); userVersion(t, e.path) != 1 || len(m) != 0 {
			t.Fatal("a database failing its schema 1 check was changed or backed up")
		}
	})
}

func TestAcceptRun(t *testing.T) {
	e := newEnv(t)
	s := e.open()
	before := fingerprint(t, e.path)
	r1, created, err := s.AcceptRun(runReq("one", "consumer-a", "task-a", "task-a-next"))
	if err != nil || !created || r1.TaskID != "task-a" || r1.State != "accepted" || r1.WalletID != "wallet-a" || r1.Payer == "" || r1.AgentID != "agent-a" {
		t.Fatalf("%+v %v %v", r1, created, err)
	}
	again, created, err := s.AcceptRun(runReq("one", "consumer-a", "task-a", "task-a-next"))
	if err != nil || created || again.RunID != r1.RunID {
		t.Fatalf("the same request ID made another run: %+v %v", again, err)
	}
	if got, found, err := s.RunByRequest(r1.RequestID); err != nil || !found || got.RunID != r1.RunID {
		t.Fatalf("RunByRequest: %+v %v %v", got, found, err)
	}
	e.start(s, "task-a-next") // a stored task is skipped too
	if _, _, err := s.AcceptRun(runReq("two", "consumer-a", "task-a", "task-a-next")); Code(err) != "NO_FREE_TASK" {
		t.Fatalf("got %v", err)
	}
	if _, _, err := s.AcceptRun(runReq("three", "consumer-b", "task-a")); Code(err) != "NO_FREE_TASK" {
		t.Fatalf("another consumer's task was allocated: %v", err)
	}
	// A clock behind refuses a new request, never the lookup of a known one.
	e.clock.Set(t0 - 100)
	if _, _, err := s.AcceptRun(runReq("four", "consumer-b", "task-b")); Code(err) != "CLOCK_REGRESSION" {
		t.Fatalf("got %v", err)
	}
	if _, found, err := s.RunByRequest(r1.RequestID); err != nil || !found {
		t.Fatal("a resend lookup failed with the clock behind")
	}
	e.clock.Set(t0 + 10)
	// Run operations change no task, purchase or event (task-a-next aside).
	if n, err := s.InterruptOpenRuns(); err != nil || n != 1 {
		t.Fatalf("interrupt: %d %v", n, err)
	}
	got, _, _ := s.GetRun(r1.RunID)
	if got.State != "interrupted" || got.InterruptedAt == 0 {
		t.Fatalf("%+v", got)
	}
	e2 := newEnv(t)
	s2 := e2.open()
	b2 := fingerprint(t, e2.path)
	r, _, _ := s2.AcceptRun(runReq("x", "consumer-a", "task-a"))
	s2.InterruptOpenRuns()
	s2.GetRun(r.RunID)
	if fingerprint(t, e2.path) != b2 {
		t.Fatal("run operations changed tasks, purchases or events")
	}
	_ = before
}

func TestEndRun(t *testing.T) {
	ok := []struct {
		name string
		end  RunEnd
		task bool
	}{
		{"ok", RunEnd{Outcome: "ok", TaskStarted: true}, true},
		{"prepare_failed with a Store code", RunEnd{Outcome: "store_unavailable", Detail: "prepare_failed:WALLET_BINDING_CHANGED"}, false},
		{"prepare_failed without a code", RunEnd{Outcome: "store_unavailable", Detail: "prepare_failed:STORE_ERROR"}, false},
		{"input_partial", RunEnd{Outcome: "input_error", Detail: "input_partial"}, false},
		{"assistant_messages", RunEnd{Outcome: "turn_limit", Detail: "assistant_messages", TaskStarted: true}, true},
		{"detail_unlisted", RunEnd{Outcome: "config_mismatch", Detail: "detail_unlisted"}, false},
		{"skipped", RunEnd{Outcome: "skipped", Detail: "scratch_home_failed"}, false},
		{"end write failed", RunEnd{Outcome: "cancelled", TaskStarted: true, StoreEndError: "CLOCK_REGRESSION"}, true},
	}
	for _, c := range ok {
		t.Run("valid "+c.name, func(t *testing.T) {
			e := newEnv(t)
			s := e.open()
			r, _, err := s.AcceptRun(runReq("a", "consumer-a", "task-a"))
			if err != nil {
				t.Fatal(err)
			}
			if c.task {
				e.start(s, "task-a")
			}
			if err := s.EndRun(r.RunID, c.end); err != nil {
				t.Fatal(err)
			}
			s.Close()
			s = e.open() // read back through the recovery check
			got, _, _ := s.GetRun(r.RunID)
			if got.State != "ended" || got.Outcome != c.end.Outcome || got.Detail != c.end.Detail || got.StoreEndError != c.end.StoreEndError || got.TaskStarted != c.end.TaskStarted {
				t.Fatalf("%+v", got)
			}
			wantCode(t, s.EndRun(r.RunID, c.end), "RUN_NOT_OPEN") // an ended run is never rewritten
		})
	}
	bad := map[string]RunEnd{
		"unknown outcome":                      {Outcome: "review_invalid_outcome"},
		"empty outcome":                        {Outcome: ""},
		"CLI-style free text":                  {Outcome: "cli_error", Detail: "Error: rate limited, retry later"},
		"unlisted lower-case code":             {Outcome: "cli_error", Detail: "not_a_listed_code"},
		"prepare_failed with an unlisted code": {Outcome: "store_unavailable", Detail: "prepare_failed:NOT_A_CODE"},
		"prepare_failed with nothing":          {Outcome: "store_unavailable", Detail: "prepare_failed:"},
		"prepare_failed lower case":            {Outcome: "store_unavailable", Detail: "prepare_failed:wallet_binding_changed"},
		"a newline":                            {Outcome: "cli_error", Detail: "host_error\nx"},
		"too long":                             {Outcome: "cli_error", Detail: "prepare_failed:" + strings.Repeat("A", 60)},
		"store_end and its error":              {Outcome: "cancelled", TaskStarted: true, StoreEnd: "TASK_CANCELLED", StoreEndError: "CLOCK_REGRESSION"},
		"skipped with a task":                  {Outcome: "skipped", TaskStarted: true},
		"cancelled with TASK_TIMED_OUT":        {Outcome: "cancelled", TaskStarted: true, StoreEnd: "TASK_TIMED_OUT"},
		"cancelled task without an end":        {Outcome: "cancelled", TaskStarted: true},
		"unknown store_end":                    {Outcome: "cancelled", TaskStarted: true, StoreEnd: "TASK_DONE"},
		"unknown store_end_error":              {Outcome: "cancelled", TaskStarted: true, StoreEndError: "Disk full"},
	}
	for name, end := range bad {
		t.Run("invalid "+name, func(t *testing.T) {
			e := newEnv(t)
			s := e.open()
			r, _, _ := s.AcceptRun(runReq("a", "consumer-a", "task-a"))
			wantCode(t, s.EndRun(r.RunID, end), "INVALID_RUN_RECORD")
			if got, _, _ := s.GetRun(r.RunID); got.State != "accepted" {
				t.Fatalf("an invalid end was written: %+v", got)
			}
		})
	}
	e := newEnv(t)
	s := e.open()
	r, _, _ := s.AcceptRun(runReq("a", "consumer-a", "task-a"))
	s.InterruptOpenRuns()
	wantCode(t, s.EndRun(r.RunID, RunEnd{Outcome: "ok"}), "RUN_NOT_OPEN") // interrupted is final
	wantCode(t, s.EndRun("run-none", RunEnd{Outcome: "ok"}), "RUN_NOT_OPEN")
}

// The CHECKs refuse the wrong shape of a state; rows of the right shape
// that break the lists or the combinations are refused when the database
// is opened, by the host_runs rules.
func TestRunRowsOnOpen(t *testing.T) {
	const acc = `INSERT INTO host_runs (run_id, request_id, scenario, scenario_digest, budget_initial, task_id, consumer_id, agent_id, wallet_id, payer, state, accepted_at`
	const vals = `'run-x', 'req-x0000000000000000', 'ask', 'sha256:x', '', 'task-a', 'consumer-a', 'agent-a', 'wallet-a', '0x1111111111111111111111111111111111111111'`
	shapes := map[string]string{
		"ended with task_started NULL": acc + `, ended_at, outcome, detail, store_end) VALUES (` + vals + `, 'ended', 1, 2, 'ok', '', '')`,
		"ended with outcome NULL":      acc + `, ended_at, detail, task_started, store_end) VALUES (` + vals + `, 'ended', 1, 2, '', 0, '')`,
		"accepted with ended_at":       acc + `, ended_at) VALUES (` + vals + `, 'accepted', 1, 2)`,
		"interrupted with an outcome":  acc + `, interrupted_at, outcome) VALUES (` + vals + `, 'interrupted', 1, 2, 'ok')`,
		"interrupted without its time": acc + `) VALUES (` + vals + `, 'interrupted', 1)`,
	}
	for name, q := range shapes {
		t.Run("CHECK "+name, func(t *testing.T) {
			e := newEnv(t)
			e.open().Close()
			if _, err := rawDB(t, e.path).Exec(q); err == nil {
				t.Fatal("the CHECKs accepted it")
			}
			// Written past the CHECKs, the open refuses it (structure or rules).
			db := rawDB(t, e.path)
			if _, err := db.Exec(`PRAGMA ignore_check_constraints = ON`); err != nil {
				t.Fatal(err)
			}
			if _, err := db.Exec(q); err != nil {
				t.Fatal(err)
			}
			db.Close()
			var ce *CorruptError
			if _, err := openOpt(e, false); !errors.As(err, &ce) {
				t.Fatalf("got %v", err)
			}
		})
	}
	rules := map[string]string{ // the right shape, refused by host_runs
		"unlisted outcome":          acc + `, ended_at, outcome, detail, task_started, store_end) VALUES (` + vals + `, 'ended', 1, 2, 'review_invalid_outcome', '', 0, '')`,
		"free-text detail":          acc + `, ended_at, outcome, detail, task_started, store_end) VALUES (` + vals + `, 'ended', 1, 2, 'cli_error', 'Error: boom', 0, '')`,
		"task_started without task": acc + `, ended_at, outcome, detail, task_started, store_end) VALUES (` + vals + `, 'ended', 1, 2, 'ok', '', 1, '')`,
		"store_end without event":   acc + `, ended_at, outcome, detail, task_started, store_end) VALUES (` + vals + `, 'ended', 1, 2, 'cancelled', '', 1, 'TASK_CANCELLED')`,
		"skipped with an end":       acc + `, ended_at, outcome, detail, task_started, store_end) VALUES (` + vals + `, 'ended', 1, 2, 'skipped', '', 0, 'TASK_CANCELLED')`,
		"bad request id":            strings.Replace(acc+`) VALUES (`+vals+`, 'accepted', 1)`, "'req-x0000000000000000'", "'x'", 1),
	}
	for name, q := range rules {
		t.Run("rule "+name, func(t *testing.T) {
			e := newEnv(t)
			e.open().Close()
			if _, err := rawDB(t, e.path).Exec(q); err != nil {
				t.Fatalf("the shape is right, the CHECKs must accept it: %v", err)
			}
			var ce *CorruptError
			if _, err := openOpt(e, false); !errors.As(err, &ce) || ce.Check != "host_runs" {
				t.Fatalf("got %v", err)
			}
		})
	}
	t.Run("binding differs from the stored task", func(t *testing.T) {
		e := newEnv(t)
		s := e.open()
		r, _, _ := s.AcceptRun(runReq("a", "consumer-a", "task-a"))
		e.start(s, "task-a")
		s.Close()
		if _, err := rawDB(t, e.path).Exec(`UPDATE host_runs SET payer = '0x2222222222222222222222222222222222222222' WHERE run_id = ?`, r.RunID); err != nil {
			t.Fatal(err)
		}
		var ce *CorruptError
		if _, err := openOpt(e, false); !errors.As(err, &ce) || ce.Check != "host_runs" {
			t.Fatalf("got %v", err)
		}
	})
	t.Run("ended without a task while the task exists", func(t *testing.T) {
		e := newEnv(t)
		s := e.open()
		r, _, _ := s.AcceptRun(runReq("a", "consumer-a", "task-a"))
		s.EndRun(r.RunID, RunEnd{Outcome: "start_failed"})
		e.start(s, "task-a") // (not possible through the host: the task is reserved)
		s.Close()
		var ce *CorruptError
		if _, err := openOpt(e, false); !errors.As(err, &ce) || ce.Check != "host_runs" {
			t.Fatalf("got %v", err)
		}
	})
	t.Run("a run's task removed from the registry still opens", func(t *testing.T) {
		e := newEnv(t)
		s := e.open()
		r, _, _ := s.AcceptRun(runReq("a", "consumer-a", "task-a-next"))
		s.Close()
		var tasks []contracts.Task
		for _, tk := range e.reg.Tasks {
			if tk.TaskID != "task-a-next" {
				tasks = append(tasks, tk)
			}
		}
		e.reg.Tasks = tasks
		s = e.open()
		s.InterruptOpenRuns()
		got, _, _ := s.GetRun(r.RunID)
		if got.State != "interrupted" || got.WalletID != "wallet-a" || got.ConsumerID != "consumer-a" {
			t.Fatalf("%+v", got)
		}
	})
}

// ---- VerifyFile and RestoreCopy (sections 13.11.2, 13.13, 13.14.1) ----

func stamps(t *testing.T, path string) string {
	t.Helper()
	var b strings.Builder
	for _, f := range []string{path, path + "-wal", path + "-shm", path + ".lock"} {
		fp, err := fingerprintOf(f)
		if err != nil {
			t.Fatal(err)
		}
		fmt.Fprintf(&b, "%s %v %d %x\n", filepath.Base(f), fp.exists, fp.size, fp.sum)
	}
	return b.String()
}

func dirList(t *testing.T, dir string) string {
	t.Helper()
	ents, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	var names []string
	for _, e := range ents {
		names = append(names, e.Name())
	}
	return strings.Join(names, ",")
}

func offline(t *testing.T) *env {
	t.Helper()
	e := newEnv(t)
	s := e.open()
	e.start(s, "task-a")
	s.Close()
	return e
}

func TestVerifyFileLocks(t *testing.T) {
	t.Run("no lock file: refused, nothing created", func(t *testing.T) {
		e := offline(t)
		list := dirList(t, filepath.Dir(e.path))
		if _, err := VerifyFile(e.path); err == nil {
			t.Fatal("verified without the database's lock")
		}
		if dirList(t, filepath.Dir(e.path)) != list {
			t.Fatal("the source directory changed")
		}
	})
	t.Run("a substitute lock elsewhere does not help", func(t *testing.T) {
		e := offline(t)
		os.WriteFile(filepath.Join(os.TempDir(), filepath.Base(e.path)+".lock"), nil, 0o600)
		if _, err := VerifyFile(e.path); err == nil {
			t.Fatal("verified with a lock elsewhere")
		}
	})
	t.Run("held by another holder: refused, source unchanged", func(t *testing.T) {
		e := offline(t)
		os.WriteFile(e.path+".lock", nil, 0o600)
		f, _ := os.OpenFile(e.path+".lock", os.O_RDWR, 0)
		defer f.Close()
		if err := syscall.Flock(int(f.Fd()), syscall.LOCK_EX|syscall.LOCK_NB); err != nil {
			t.Fatal(err)
		}
		before := stamps(t, e.path)
		if _, err := VerifyFile(e.path); err == nil || !strings.Contains(err.Error(), "in use") {
			t.Fatalf("got %v", err)
		}
		if stamps(t, e.path) != before {
			t.Fatal("the source changed")
		}
	})
	t.Run("offline with its lock: verified, source unchanged, lock released", func(t *testing.T) {
		e := offline(t)
		os.WriteFile(e.path+".lock", nil, 0o600)
		before := stamps(t, e.path)
		if v, err := VerifyFile(e.path); err != nil || v != 2 {
			t.Fatalf("%d %v", v, err)
		}
		if stamps(t, e.path) != before {
			t.Fatal("the source changed")
		}
		f, _ := os.OpenFile(e.path+".lock", os.O_RDWR, 0)
		defer f.Close()
		if err := syscall.Flock(int(f.Fd()), syscall.LOCK_EX|syscall.LOCK_NB); err != nil {
			t.Fatal("the lock was not released")
		}
	})
	t.Run("a change seen while reading refuses the copy", func(t *testing.T) {
		e := offline(t)
		os.WriteFile(e.path+".lock", nil, 0o600)
		verifyFileHook = func(string) error {
			f, _ := os.OpenFile(e.path, os.O_WRONLY|os.O_APPEND, 0)
			f.Write([]byte{0})
			return f.Close()
		}
		defer func() { verifyFileHook = nil }()
		if _, err := VerifyFile(e.path); err == nil || !strings.Contains(err.Error(), "changed while it was read") {
			t.Fatalf("got %v", err)
		}
	})
	t.Run("a held lock of a restore copy is refused too", func(t *testing.T) {
		e := offline(t)
		dir := filepath.Join(t.TempDir(), "restore")
		db, v, err := RestoreCopy(e.path, dir)
		if err != nil || v != 2 {
			t.Fatalf("%v %d", err, v)
		}
		f, _ := os.OpenFile(db+".lock", os.O_RDWR, 0)
		defer f.Close()
		syscall.Flock(int(f.Fd()), syscall.LOCK_EX|syscall.LOCK_NB)
		if _, err := VerifyFile(db); err == nil {
			t.Fatal("verified a restore copy whose lock is held")
		}
	})
}

func TestRestoreCopySteps(t *testing.T) {
	e := offline(t)
	calls := 0
	verifyFn = func(p string) (int, error) { calls++; return VerifyFile(p) }
	defer func() { verifyFn = VerifyFile }()
	t.Run("R-1 the directory exists", func(t *testing.T) {
		dir := t.TempDir()
		os.WriteFile(filepath.Join(dir, "demo.db"), []byte("keep"), 0o600)
		os.WriteFile(filepath.Join(dir, "demo.db.lock"), []byte("keep"), 0o600)
		calls = 0
		if _, _, err := RestoreCopy(e.path, dir); err == nil || calls != 0 {
			t.Fatalf("%v, verify called %d", err, calls)
		}
		for _, f := range []string{"demo.db", "demo.db.lock"} {
			if b, _ := os.ReadFile(filepath.Join(dir, f)); string(b) != "keep" {
				t.Fatalf("%s was overwritten or truncated", f)
			}
		}
	})
	for _, stage := range []string{"copy", "lock"} {
		t.Run("R-2/R-3 failure at "+stage, func(t *testing.T) {
			restoreHook = func(s string) error {
				if s == stage {
					return errors.New("injected")
				}
				return nil
			}
			defer func() { restoreHook = nil }()
			calls = 0
			if _, _, err := RestoreCopy(e.path, filepath.Join(t.TempDir(), "r")); err == nil || calls != 0 {
				t.Fatalf("%v, verify called %d", err, calls)
			}
		})
	}
	t.Run("R-5 all steps", func(t *testing.T) {
		before := stamps(t, e.path)
		calls = 0
		db, v, err := RestoreCopy(e.path, filepath.Join(t.TempDir(), "r"))
		if err != nil || v != 2 || calls != 1 {
			t.Fatalf("%v %d calls %d", err, v, calls)
		}
		if fi, _ := os.Stat(db + ".lock"); fi == nil || fi.Mode().Perm() != 0o600 {
			t.Fatal("the copy's lock is missing or not 0600")
		}
		if stamps(t, e.path) != before {
			t.Fatal("the source changed")
		}
	})
}

// ---- leftover WAL, cases A and B (sections 13.11.3, 13.12, 13.14.2) ----

func childEnv() (contracts.RegistryConfig, policy.Profile, error) {
	var reg contracts.RegistryConfig
	raw, err := os.ReadFile("../../testdata/contracts/registry.json")
	if err == nil {
		err = contracts.Decode(raw, &reg)
	}
	if err != nil {
		return reg, policy.Profile{}, err
	}
	raw, err = os.ReadFile("../../testdata/policy/m1-demo-profile.json")
	if err != nil {
		return reg, policy.Profile{}, err
	}
	p, err := policy.LoadProfile(raw)
	return reg, p, err
}

// walChild opens path the way the demo host does (its <db>.lock held), runs
// case A or B, prints a ready line before closing anything, and blocks until
// the parent kills it: what it wrote stays in the WAL.
func walChild(c, path string) int {
	lock, err := os.OpenFile(path+".lock", os.O_RDWR|os.O_CREATE, 0o600)
	if err != nil || syscall.Flock(int(lock.Fd()), syscall.LOCK_EX|syscall.LOCK_NB) != nil {
		fmt.Println("CHILD_LOCK_FAILED")
		return 2
	}
	reg, profile, err := childEnv()
	if err != nil {
		fmt.Println("CHILD_ENV_FAILED", err)
		return 2
	}
	clock := func() uint64 { return t0 + 60 }
	block := func(line string) { fmt.Println(line); os.Stdout.Sync(); select {} }
	switch c {
	case "V1WAL": // writes into a schema 1 file's WAL with checkpoints off, then dies
		db, err := sql.Open("sqlite", path+"?_pragma=busy_timeout(5000)&_pragma=wal_autocheckpoint(0)")
		if err == nil {
			_, err = db.Exec(`PRAGMA journal_mode = WAL; CREATE TABLE wal_probe (x INTEGER); INSERT INTO wal_probe VALUES (1)`)
		}
		if err != nil {
			fmt.Println("CHILD_WRITE_FAILED", err)
			return 2
		}
		block("V1_WAL_READY")
	case "A":
		upgradeHook = func(stage string) error {
			if stage == "verified2" {
				block("UPGRADE_FAILED_READY") // the injected failure: before any connection is closed
			}
			return nil
		}
		Open(Config{Path: path, Registry: reg, Clock: clock, UpgradeSchema1: true})
	case "B":
		s, err := Open(Config{Path: path, Registry: reg, Clock: clock, UpgradeSchema1: true})
		if err != nil {
			fmt.Println("CHILD_OPEN_FAILED", err)
			return 2
		}
		r, _, err := s.AcceptRun(runReq("b", "consumer-a", "task-a-next"))
		if err == nil {
			err = s.StartTask(TaskSetup{TaskID: "task-a-next", Profile: &profile, Credential: &policy.Credential{ConsumerID: "consumer-a", ValidUntil: t0 + 86400, EvidenceMode: "fixture"}})
		}
		if err == nil {
			err = s.EndRun(r.RunID, RunEnd{Outcome: "ok", TaskStarted: true})
		}
		if err != nil {
			fmt.Println("CHILD_RUN_FAILED", err)
			return 2
		}
		db, err := sql.Open("sqlite", path+"?_pragma=busy_timeout(5000)&_pragma=wal_autocheckpoint(0)")
		if err == nil {
			_, err = db.Exec(`UPDATE host_runs SET outcome = 'review_invalid_outcome' WHERE run_id = ?`, r.RunID)
		}
		if err != nil {
			fmt.Println("CHILD_UPDATE_FAILED", err)
			return 2
		}
		block("INVALID_OUTCOME_READY " + r.RunID)
	}
	return 3
}

// killedChild runs walChild c on path and kills it after its ready line.
func killedChild(t *testing.T, c, path string) string {
	t.Helper()
	exe, _ := os.Executable()
	cmd := exec.Command(exe, "-test.run=^$")
	cmd.Env = append(os.Environ(), "APA_STORE_CHILD="+c, "APA_STORE_CHILD_DB="+path)
	out, _ := cmd.StdoutPipe()
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	line := make(chan string, 1)
	go func() { sc := bufio.NewScanner(out); sc.Scan(); line <- sc.Text() }()
	var got string
	select {
	case got = <-line:
	case <-time.After(60 * time.Second):
		cmd.Process.Kill()
		t.Fatal("the child never got ready")
	}
	cmd.Process.Signal(syscall.SIGKILL)
	cmd.Wait()
	if !strings.HasSuffix(strings.Fields(got + " x")[0], "_READY") {
		t.Fatalf("child: %q", got)
	}
	f, _ := os.OpenFile(path+".lock", os.O_RDWR, 0) // the lock is free again once it exited
	if err := syscall.Flock(int(f.Fd()), syscall.LOCK_EX|syscall.LOCK_NB); err != nil {
		t.Fatal("the child's lock is still held")
	}
	syscall.Flock(int(f.Fd()), syscall.LOCK_UN)
	f.Close()
	return got
}

// throwaway copies the database (and its WAL when withWAL) and returns what
// query gives there; the evidence is never opened.
func throwaway(t *testing.T, path string, withWAL bool, query string) string {
	t.Helper()
	dir := t.TempDir()
	cp := filepath.Join(dir, "x.db")
	if err := copyFile(path, cp); err != nil {
		t.Fatal(err)
	}
	if withWAL {
		if err := copyFile(path+"-wal", cp+"-wal"); err != nil {
			t.Fatal(err)
		}
	}
	var s string
	if err := rawDB(t, cp).QueryRow(query).Scan(&s); err != nil {
		return "error: " + err.Error()
	}
	return s
}

func leftoverWAL(t *testing.T, path string) {
	t.Helper()
	fi, err := os.Stat(path + "-wal")
	if err != nil || fi.Size() == 0 {
		t.Fatal("no leftover WAL: the case would prove nothing") // fail, never skip
	}
}

func TestLeftoverWALCaseA(t *testing.T) {
	e := newEnv(t)
	schema1(t, e)
	pre := fingerprint(t, e.path)
	killedChild(t, "A", e.path)
	base := stamps(t, e.path)
	leftoverWAL(t, e.path)
	if v := throwaway(t, e.path, false, `SELECT user_version FROM pragma_user_version`); v != "1" {
		t.Fatalf("main file alone: schema %s, want 1", v)
	}
	if v := throwaway(t, e.path, false, `SELECT count(*) FROM sqlite_master WHERE name = 'host_runs'`); v != "0" {
		t.Fatal("main file alone has host_runs: the migration is not only in the WAL")
	}
	if v := throwaway(t, e.path, true, `SELECT user_version FROM pragma_user_version`); v != "2" {
		t.Fatalf("main file + WAL: schema %s, want 2", v)
	}
	// Re-verify without the hook: the stored data is complete.
	if v, err := VerifyFile(e.path); err != nil || v != 2 {
		t.Fatalf("VerifyFile: %d %v", v, err)
	}
	if stamps(t, e.path) != base {
		t.Fatal("VerifyFile changed the source")
	}
	m, _ := filepath.Glob(e.path + ".schema1-*.bak")
	if len(m) != 1 {
		t.Fatalf("backups %v", m)
	}
	db, v, err := RestoreCopy(m[0], filepath.Join(t.TempDir(), "restore"))
	if err != nil || v != 1 {
		t.Fatalf("restore: %d %v", v, err)
	}
	if fingerprint(t, db) != pre {
		t.Fatal("the restore copy is not the data before the upgrade")
	}
	if stamps(t, e.path) != base {
		t.Fatal("the failed database changed during the restore")
	}
}

func TestLeftoverWALCaseB(t *testing.T) {
	e := newEnv(t)
	schema1(t, e)
	line := killedChild(t, "B", e.path)
	runID := strings.Fields(line)[1]
	base := stamps(t, e.path)
	leftoverWAL(t, e.path)
	if v := throwaway(t, e.path, false, `SELECT count(*) FROM sqlite_master WHERE name = 'host_runs'`); v != "0" {
		t.Fatal("main file alone has host_runs")
	}
	if v := throwaway(t, e.path, true, `SELECT outcome FROM host_runs WHERE run_id = '`+runID+`'`); v != "review_invalid_outcome" {
		t.Fatalf("main file + WAL: outcome %q", v)
	}
	for _, q := range []string{`SELECT group_concat(integrity_check) FROM pragma_integrity_check`, `SELECT count(*) FROM pragma_foreign_key_check`} {
		if v := throwaway(t, e.path, true, q); v != "ok" && v != "0" {
			t.Fatalf("%s: %s (the structure must pass, so the refusal comes from the rules)", q, v)
		}
	}
	_, err := VerifyFile(e.path)
	var ce *CorruptError
	if !errors.As(err, &ce) || ce.Check != "host_runs" || !strings.Contains(ce.Detail, runID) || !strings.Contains(ce.Detail, "outcome") {
		t.Fatalf("got %v", err)
	}
	if stamps(t, e.path) != base {
		t.Fatal("VerifyFile changed the source")
	}
}

// existence and bytes of every file next to the database, and the
// directory's entries: a refused open must leave all of them as they were.
func dirState(t *testing.T, path string) string {
	t.Helper()
	return dirList(t, filepath.Dir(path)) + "\n" + stamps(t, path)
}

// Review R1 (3.2): a schema 1 file is refused before any SQLite
// connection touches it, also when a crash left a WAL to replay.
func TestRefusedSchema1KeepsWAL(t *testing.T) {
	e := newEnv(t)
	schema1(t, e)
	killedChild(t, "V1WAL", e.path)
	leftoverWAL(t, e.path)
	if v := throwaway(t, e.path, false, `SELECT count(*) FROM sqlite_master WHERE name = 'wal_probe'`); v != "0" {
		t.Fatal("the child's write is in the main file: the WAL proves nothing")
	}
	before := dirState(t, e.path)
	_, err := openOpt(e, false)
	wantCode(t, err, "SCHEMA_UPGRADE_REQUIRED")
	if after := dirState(t, e.path); after != before {
		t.Fatalf("the refused file changed:\n%s\n->\n%s", before, after)
	}
	// Without a WAL the header alone decides; nothing changes either.
	e2 := newEnv(t)
	schema1(t, e2)
	before = dirState(t, e2.path)
	_, err = openOpt(e2, false)
	wantCode(t, err, "SCHEMA_UPGRADE_REQUIRED")
	if dirState(t, e2.path) != before {
		t.Fatal("the refused file (no WAL) changed")
	}
}

// Review R2 (3.2): EndRun checks the stored task and events in its own
// write transaction, with the recovery's rules; a contradiction is refused
// and nothing is written, so a reopen never meets a record it refuses.
func TestEndRunChecksTheStore(t *testing.T) {
	for name, c := range map[string]struct {
		startTask, cancel bool
		end               RunEnd
	}{
		"task_started without a task":      {false, false, RunEnd{Outcome: "ok", TaskStarted: true}},
		"a task, task_started false":       {true, false, RunEnd{Outcome: "start_failed"}},
		"TASK_CANCELLED without the event": {true, false, RunEnd{Outcome: "cancelled", TaskStarted: true, StoreEnd: "TASK_CANCELLED"}},
	} {
		t.Run(name, func(t *testing.T) {
			e := newEnv(t)
			s := e.open()
			r, _, _ := s.AcceptRun(runReq("a", "consumer-a", "task-a"))
			if c.startTask {
				e.start(s, "task-a")
			}
			wantCode(t, s.EndRun(r.RunID, c.end), "INVALID_RUN_RECORD")
			if got, _, _ := s.GetRun(r.RunID); got.State != "accepted" {
				t.Fatalf("written: %+v", got)
			}
			s.Close()
			if _, err := openOpt(e, false); err != nil {
				t.Fatalf("reopen: %v", err)
			}
		})
	}
	t.Run("the same end after the cancel event is accepted", func(t *testing.T) {
		e := newEnv(t)
		s := e.open()
		r, _, _ := s.AcceptRun(runReq("a", "consumer-a", "task-a"))
		e.start(s, "task-a")
		if err := s.Cancel("task-a"); err != nil {
			t.Fatal(err)
		}
		if err := s.EndRun(r.RunID, RunEnd{Outcome: "cancelled", TaskStarted: true, StoreEnd: "TASK_CANCELLED"}); err != nil {
			t.Fatal(err)
		}
		s.Close()
		if _, err := openOpt(e, false); err != nil {
			t.Fatalf("reopen: %v", err)
		}
	})
}

// migrate's own refusal of schema 1 is the second layer behind Open's
// version check (review R1): reached directly here, since Open stops first.
func TestMigrateRefusesSchema1(t *testing.T) {
	e := newEnv(t)
	schema1(t, e)
	db, err := sql.Open("sqlite", e.path)
	if err != nil {
		t.Fatal(err)
	}
	s := &Store{path: e.path, w: db}
	defer db.Close()
	wantCode(t, s.migrate(false), "SCHEMA_UPGRADE_REQUIRED")
	if userVersion(t, e.path) != 1 || count(t, e.path, "sqlite_master", "name = 'host_runs'") != 0 {
		t.Fatal("migrate changed a schema 1 file without the upgrade")
	}
}

func TestListTasks(t *testing.T) {
	e := newEnv(t)
	s := e.open()
	e.start(s, "task-a")
	e.clock.Add(5)
	e.start(s, "task-b")
	e.clock.Add(5)
	e.start(s, "task-a-next")
	before := fingerprint(t, e.path)
	all, err := s.ListTasks("", 10)
	if err != nil || len(all) != 3 || all[0].TaskID != "task-a-next" || all[2].TaskID != "task-a" {
		t.Fatalf("all: %+v %v", all, err)
	}
	a, _ := s.ListTasks("consumer-a", 10)
	if len(a) != 2 || a[0].TaskID != "task-a-next" || a[1].TaskID != "task-a" || a[0].ConsumerID != "consumer-a" || a[0].Status != "running" {
		t.Fatalf("consumer-a: %+v", a)
	}
	if one, _ := s.ListTasks("consumer-a", 1); len(one) != 1 || one[0].TaskID != "task-a-next" {
		t.Fatalf("limit: %+v", one)
	}
	if none, _ := s.ListTasks("consumer-z", 10); len(none) != 0 {
		t.Fatalf("unknown consumer: %+v", none)
	}
	if fingerprint(t, e.path) != before {
		t.Fatal("listing wrote")
	}
}
