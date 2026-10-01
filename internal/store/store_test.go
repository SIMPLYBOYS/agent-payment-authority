package store

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"testing"

	"github.com/SIMPLYBOYS/agent-payment-authority/internal/contracts"
	"github.com/SIMPLYBOYS/agent-payment-authority/internal/policy"
)

func TestPathRules(t *testing.T) {
	e := newEnv(t)
	for _, p := range []string{"", "file:apa.db", "apa.db?mode=memory", "apa.db#x"} {
		if _, err := Open(Config{Path: p, Registry: e.reg, Clock: e.clock.clock()}); err == nil {
			t.Errorf("Open(%q) accepted a DSN-like path", p)
		}
	}
	wide := filepath.Join(t.TempDir(), "wide")
	os.Mkdir(wide, 0o755)
	os.Chmod(wide, 0o755)
	if _, err := Open(Config{Path: filepath.Join(wide, "apa.db"), Registry: e.reg, Clock: e.clock.clock()}); err == nil {
		t.Error("Open accepted a directory readable by others")
	}
	private := filepath.Join(t.TempDir(), "p")
	os.Mkdir(private, 0o700)
	os.Symlink(filepath.Join(private, "elsewhere.db"), filepath.Join(private, "apa.db"))
	if _, err := Open(Config{Path: filepath.Join(private, "apa.db"), Registry: e.reg, Clock: e.clock.clock()}); err == nil {
		t.Error("Open accepted a symlinked database path")
	}

	s := e.open()
	e.start(s, "task-a")
	dir := filepath.Dir(e.path)
	fi, err := os.Stat(dir)
	if err != nil || fi.Mode().Perm() != 0o700 {
		t.Fatalf("database directory mode %v, %v; want 0700", fi.Mode().Perm(), err)
	}
	for _, f := range []string{"apa.db", "apa.db-wal", "apa.db-shm"} {
		if _, err := os.Stat(filepath.Join(dir, f)); err != nil {
			t.Errorf("%s: %v (WAL files live in the private directory)", f, err)
		}
	}
}

// A relative path resolves against the working directory at Open.
func TestRelativePath(t *testing.T) {
	e := newEnv(t) // fixtures are read relative to the package directory first
	base := t.TempDir()
	t.Chdir(base)
	e.path = filepath.Join("var", "apa.db")
	e.open()
	if _, err := os.Stat(filepath.Join(base, "var", "apa.db")); err != nil {
		t.Fatalf("relative path did not resolve against the working directory: %v", err)
	}
}

// Every pooled connection gets the PRAGMAs, not just the first one.
func TestPragmasOnEveryConnection(t *testing.T) {
	s := newEnv(t).open()
	ctx := context.Background()
	for _, h := range []struct {
		name  string
		conns int
		want  map[string]int
	}{
		{"reader", 4, map[string]int{"foreign_keys": 1, "busy_timeout": 5000, "synchronous": 2, "query_only": 1}},
		{"writer", 1, map[string]int{"foreign_keys": 1, "busy_timeout": 5000, "synchronous": 2, "query_only": 0}},
	} {
		db := s.r
		if h.name == "writer" {
			db = s.w
		}
		var held []interface{ Close() error }
		for i := range h.conns {
			c, err := db.Conn(ctx)
			if err != nil {
				t.Fatal(err)
			}
			held = append(held, c)
			for p, v := range h.want {
				var got int
				if err := c.QueryRowContext(ctx, "PRAGMA "+p).Scan(&got); err != nil || got != v {
					t.Errorf("%s connection %d: %s = %d, want %d (%v)", h.name, i, p, got, v, err)
				}
			}
		}
		for _, c := range held {
			c.Close()
		}
	}
	var mode string
	s.r.QueryRow("PRAGMA journal_mode").Scan(&mode)
	if mode != "wal" {
		t.Errorf("journal_mode %q, want wal", mode)
	}
}

func TestStartTask(t *testing.T) {
	e := newEnv(t)
	s := e.open()
	e.start(s, "task-a")
	wantCode(t, s.StartTask(e.setup("task-a")), "TASK_ALREADY_STARTED")
	wantCode(t, s.StartTask(TaskSetup{TaskID: "task-z"}), "UNKNOWN_TASK")

	bad := map[string]func(*TaskSetup){
		"nil profile, not declared absent": func(x *TaskSetup) { x.Profile = nil },
		"profile and absent both":          func(x *TaskSetup) { x.ProfileAbsent = true },
		"invalid profile":                  func(x *TaskSetup) { x.Profile.Mode = "auto" },
		"budget above the cap":             func(x *TaskSetup) { x.BudgetInitial = "5000001" },
		"budget not an amount":             func(x *TaskSetup) { x.BudgetInitial = "0.5" },
		"asset not the profile's":          func(x *TaskSetup) { x.AssetID = "other" },
		"absent profile without asset":     func(x *TaskSetup) { x.Profile, x.ProfileAbsent, x.BudgetInitial = nil, true, "5000000" },
		"absent profile with unknown asset": func(x *TaskSetup) {
			x.Profile, x.ProfileAbsent, x.BudgetInitial, x.AssetID = nil, true, "5000000", "nope"
		},
		"absent profile without budget":       func(x *TaskSetup) { x.Profile, x.ProfileAbsent, x.AssetID = nil, true, "fixture-usdc" },
		"credential of another consumer":      func(x *TaskSetup) { x.Credential.ConsumerID = "consumer-a" },
		"live credential":                     func(x *TaskSetup) { x.Credential.EvidenceMode = "live" },
		"nil credential, not declared absent": func(x *TaskSetup) { x.Credential = nil },
	}
	for name, edit := range bad {
		x := e.setup("task-b")
		edit(&x)
		wantCode(t, s.StartTask(x), "INVALID_TASK_SETUP")
		if count(t, e.path, "tasks", "task_id = 'task-b'") != 0 {
			t.Fatalf("%s: a refused StartTask left a task", name)
		}
	}
	// A lower trusted budget is kept, never reset to the task cap.
	low := e.setup("task-b")
	low.BudgetInitial = "250000"
	if err := s.StartTask(low); err != nil {
		t.Fatal(err)
	}
	if r := submit(t, s, sessB, forTask(t, e.base, "task-b")); r.Purchase.Decision.Reason != "INSUFFICIENT_BUDGET" {
		t.Fatalf("decision %+v, want INSUFFICIENT_BUDGET from the 0.25 budget", r.Purchase.Decision)
	}
}

// The same consumer keeps its wallet across tasks; a reloaded registry that
// moves it is refused, not followed.
func TestWalletBindingKept(t *testing.T) {
	e := newEnv(t)
	s := e.open()
	e.start(s, "task-a")
	s.Close()
	e.reg.Wallets[0].From = "0x9999999999999999999999999999999999999999"
	s = e.open() // existing tasks verify against their own snapshot
	wantCode(t, s.StartTask(e.setup("task-a-next")), "WALLET_BINDING_CHANGED")
}

// 9.1 #1: one logical purchase however often it is sent.
func TestReplaySequentialAndConcurrent(t *testing.T) {
	e := newEnv(t)
	s := e.open()
	e.start(s, "task-a")
	first := submit(t, s, sessA, e.base)
	again := submit(t, s, sessA, e.base)
	if first.Replay || !again.Replay || again.Purchase.Decision != first.Purchase.Decision {
		t.Fatalf("first %+v, again %+v", first, again)
	}

	stores := []*Store{s, e.open(), e.open()}
	p := variant(t, e.base, 2)
	var wg sync.WaitGroup
	results := make([]Result, 12)
	errs := make([]error, 12)
	for i := range results {
		wg.Go(func() { results[i], errs[i] = stores[i%3].Submit(sessA, raw(t, p)) })
	}
	wg.Wait()
	fresh := 0
	for i, r := range results {
		if errs[i] != nil {
			t.Fatal(errs[i])
		}
		if !r.Replay {
			fresh++
		}
		if r.Purchase.Decision != results[0].Purchase.Decision {
			t.Fatalf("decisions differ: %+v vs %+v", r.Purchase.Decision, results[0].Purchase.Decision)
		}
	}
	if fresh != 1 || count(t, e.path, "purchases", "") != 2 || count(t, e.path, "events", "") != 5 {
		t.Fatalf("fresh %d, purchases %d, events %d; want 1, 2, 5", fresh, count(t, e.path, "purchases", ""), count(t, e.path, "events", ""))
	}
	consistent(t, s, sessA)
}

// 9.1 #2: conflicts write nothing.
func TestConflictsWriteNothing(t *testing.T) {
	e := newEnv(t)
	s := e.open()
	e.start(s, "task-a")
	submit(t, s, sessA, e.base)
	before := fingerprint(t, e.path)

	sameKey := e.base
	sameKey.Intent.PurchaseOrderID, sameKey.Intent.EIP3009Nonce = "po-other", "0x"+strings.Repeat("b", 64)
	samePO := e.base
	samePO.RequestID, samePO.Intent.EIP3009Nonce = "request-other", "0x"+strings.Repeat("c", 64)
	sameNonce := e.base
	sameNonce.RequestID, sameNonce.Intent.PurchaseOrderID = "request-other", "po-other"
	for name, p := range map[string]contracts.PurchaseOrder{"same retry key": sameKey, "purchase_order_id reused": samePO, "nonce reused": sameNonce} {
		_, err := s.Submit(sessA, raw(t, p))
		wantCode(t, err, "IDEMPOTENCY_CONFLICT")
		if fingerprint(t, e.path) != before {
			t.Fatalf("%s: a conflict changed the database", name)
		}
	}
}

// 9.1 #3: a finished task still replays; it takes nothing new; nobody else
// can replay it.
func TestReplayAfterTaskEnded(t *testing.T) {
	e := newEnv(t)
	s := e.open()
	e.start(s, "task-a")
	e.start(s, "task-b")
	denied := submit(t, s, sessA, e.denied)
	if denied.Purchase.Decision.PolicyVerdict != "Deny" || task(t, s, sessA).Status != "denied" {
		t.Fatalf("setup: %+v", denied)
	}
	before := fingerprint(t, e.path)
	again := submit(t, s, sessA, e.denied)
	if !again.Replay || again.Purchase.Decision != denied.Purchase.Decision || fingerprint(t, e.path) != before {
		t.Fatalf("replay after Deny: %+v", again)
	}
	_, err := s.Submit(sessA, raw(t, variant(t, e.base, 3)))
	wantCode(t, err, "TASK_NOT_ACTIVE")
	_, err = s.Submit(sessB, raw(t, e.denied))
	wantCode(t, err, "OWNERSHIP_MISMATCH")
	if fingerprint(t, e.path) != before {
		t.Fatal("refusals changed the database")
	}
}

// 9.1 #4: a Deny racing another purchase never lets a decision follow it.
func TestDenyRacesNewPurchase(t *testing.T) {
	for round := range 15 {
		e := newEnv(t)
		s1, s2 := e.open(), e.open()
		e.start(s1, "task-a")
		var wg sync.WaitGroup
		var denyErr, askErr error
		wg.Go(func() { _, denyErr = s1.Submit(sessA, raw(t, e.denied)) })
		wg.Go(func() { _, askErr = s2.Submit(sessA, raw(t, e.base)) })
		wg.Wait()
		for _, err := range []error{denyErr, askErr} {
			if err != nil && Code(err) != "TASK_NOT_ACTIVE" {
				t.Fatalf("round %d: %v", round, err)
			}
		}
		events, _ := s1.Events(sessA)
		denied := false
		for _, ev := range events {
			if ev.EventType == "POLICY_DECIDED" {
				if denied {
					t.Fatalf("round %d: a decision after the Deny", round)
				}
				denied = ev.Payload.Decision.PolicyVerdict == "Deny"
			}
		}
		consistent(t, s1, sessA)
	}
}

// 9.1 #5a: a failure at any write step leaves nothing behind.
func TestWriteFailureRollsBack(t *testing.T) {
	for _, stage := range []string{"after-purchase", "after-events", "before-commit"} {
		e := newEnv(t)
		s := e.open()
		e.start(s, "task-a")
		before := fingerprint(t, e.path)
		s.hook = func(at string) error {
			if at == stage {
				return errors.New("injected failure")
			}
			return nil
		}
		if _, err := s.Submit(sessA, raw(t, e.base)); err == nil {
			t.Fatalf("%s: Submit succeeded through an injected failure", stage)
		}
		s.hook = nil
		if fingerprint(t, e.path) != before {
			t.Fatalf("%s: rollback left rows behind", stage)
		}
		s.Close()
		consistent(t, e.open(), sessA)
	}
}

// 9.1 #5b: a process that dies before commit leaves nothing behind. The
// child is this test binary re-run with APA_STORE_CRASH set.
func TestCrashBeforeCommit(t *testing.T) {
	if path := os.Getenv("APA_STORE_CRASH"); path != "" {
		e := newEnv(t)
		e.path = path
		s := e.open()
		s.hook = func(at string) error {
			if at == "before-commit" {
				os.Exit(7)
			}
			return nil
		}
		s.Submit(sessA, raw(t, e.base))
		t.Fatal("child should have exited before commit")
	}
	e := newEnv(t)
	s := e.open()
	e.start(s, "task-a")
	s.Close()
	cmd := exec.Command(os.Args[0], "-test.run=^TestCrashBeforeCommit$")
	cmd.Env = append(os.Environ(), "APA_STORE_CRASH="+e.path)
	err := cmd.Run()
	var exit *exec.ExitError
	if !errors.As(err, &exit) || exit.ExitCode() != 7 {
		t.Fatalf("child: %v, want exit 7", err)
	}
	s = e.open()
	if count(t, e.path, "purchases", "") != 0 || count(t, e.path, "events", "") != 1 {
		t.Fatal("the crashed transaction left rows")
	}
	consistent(t, s, sessA)
}

// 9.1 #7: closing records after a Deny keep the Deny and its reason.
func TestDenyThenEnd(t *testing.T) {
	for _, end := range []string{"cancel", "cli-timeout"} {
		e := newEnv(t)
		s := e.open()
		e.start(s, "task-a")
		submit(t, s, sessA, e.denied)
		var err error
		if end == "cancel" {
			err = s.Cancel("task-a")
		} else {
			err = s.CLITimeout("task-a")
		}
		if err != nil {
			t.Fatal(err)
		}
		if v := task(t, s, sessA); v.Status != "denied" || v.DenyReason != "ASSET_NOT_ALLOWED" {
			t.Fatalf("%s after Deny: %s/%s", end, v.Status, v.DenyReason)
		}
		wantCode(t, s.Cancel("task-a"), "TASK_ALREADY_ENDED")
		consistent(t, s, sessA)
	}
}

// 9.1 #8: A and B never see or touch each other; a profile change only
// affects tasks started after it.
func TestIsolationAB(t *testing.T) {
	e := newEnv(t)
	s := e.open()
	e.start(s, "task-a")
	e.start(s, "task-b")
	a := submit(t, s, sessA, e.base)
	b := submit(t, s, sessB, forTask(t, e.base, "task-b")) // same request_id "request-1"
	if a.Replay || b.Replay {
		t.Fatal("the same request_id in A and B must be two purchases")
	}
	aOnB := contracts.Session{ConsumerID: "consumer-a", AgentID: "agent-a", TaskID: "task-b"}
	_, err := s.Task(aOnB)
	wantCode(t, err, "OWNERSHIP_MISMATCH")
	_, err = s.Events(aOnB)
	wantCode(t, err, "OWNERSHIP_MISMATCH")
	_, err = s.Submit(aOnB, raw(t, forTask(t, e.base, "task-b")))
	wantCode(t, err, "OWNERSHIP_MISMATCH")

	// A new profile for a new task; task-a keeps its own snapshot across a restart.
	s.Close()
	s = e.open()
	looser := e.profile
	looser.ProfileID, looser.AllowMax = "m1-demo-loose", "5000000"
	x := e.setup("task-a-next")
	x.Profile = &looser
	if err := s.StartTask(x); err != nil {
		t.Fatal(err)
	}
	if r := submit(t, s, sessANext, variant(t, forTask(t, e.base, "task-a-next"), 9)); r.Purchase.Decision.PolicyVerdict != "Allow" {
		t.Fatalf("task-a-next under the looser profile: %+v", r.Purchase.Decision)
	}
	if r := submit(t, s, sessA, e.base); !r.Replay || r.Purchase.Decision.PolicyVerdict != "Ask" {
		t.Fatalf("task-a after the profile change: %+v", r)
	}
}

// 9.1 #11: nothing in M1 approves, grants, settles, delivers or spends.
func TestNoPaymentCapability(t *testing.T) {
	e := newEnv(t)
	s := e.open()
	e.start(s, "task-a")
	e.start(s, "task-b")
	submit(t, s, sessA, e.base)
	submit(t, s, sessA, e.allow)
	e.clock.Add(31)
	if err := s.ExpireDue(); err != nil {
		t.Fatal(err)
	}
	submit(t, s, sessB, forTask(t, e.denied, "task-b"))
	if err := s.Cancel("task-b"); err != nil {
		t.Fatal(err)
	}
	allowed := []string{"TASK_STARTED", "PURCHASE_PROPOSED", "POLICY_DECIDED", "APPROVAL_EXPIRED", "APPROVAL_WITHDRAWN", "TASK_CANCELLED", "TASK_TIMED_OUT"}
	for _, sess := range []contracts.Session{sessA, sessB} {
		for _, typ := range eventTypes(t, s, sess) {
			if !slices.Contains(allowed, typ) {
				t.Fatalf("event %s in M1", typ)
			}
		}
		for _, p := range task(t, s, sess).Purchases {
			if p.Decision.PaymentStatus != "unavailable" {
				t.Fatalf("payment status %s", p.Decision.PaymentStatus)
			}
		}
	}
	if n := count(t, e.path, "tasks", "budget_remaining != '5000000'"); n != 0 {
		t.Fatalf("%d tasks spent budget", n)
	}
	var c policy.Credential
	if err := decodeSnapshot(rawDB(t, e.path), mustScalar(t, e.path, `SELECT credential_snapshot FROM tasks WHERE task_id = 'task-a'`), "credential", &c); err != nil || c.EvidenceMode != "fixture" {
		t.Fatalf("credential snapshot %+v, %v", c, err)
	}
}

func mustScalar(t *testing.T, path, q string) string {
	t.Helper()
	var s string
	if err := rawDB(t, path).QueryRow(q).Scan(&s); err != nil {
		t.Fatal(err)
	}
	return s
}

// The unique indexes are a backstop behind CompareRetry. The index rejects a
// duplicate on its own (raw insert), uniqueViolation recognises that error,
// and Submit refuses the same clash with IDEMPOTENCY_CONFLICT. Under the write
// lock the CompareRetry lookup answers first, so this test does not reach
// Submit's backstop branch itself.
func TestUniqueIndexBackstop(t *testing.T) {
	e := newEnv(t)
	s := e.open()
	e.start(s, "task-a")
	submit(t, s, sessA, e.base)
	before := fingerprint(t, e.path)
	clash := variant(t, e.base, 30)
	clash.Intent.PurchaseOrderID = e.base.Intent.PurchaseOrderID
	tx, _ := s.w.Begin()
	_, err := tx.Exec(`INSERT INTO purchases SELECT task_id, purchase_order_id, 'request-x', 'sha256:x', chain_id, asset_contract, payer, eip3009_nonce,
	  body, intent_hash, policy_verdict, approval_status, reason, decided_at, NULL, NULL FROM purchases`)
	tx.Rollback()
	if !uniqueViolation(err) {
		t.Fatalf("duplicate primary key: %v, want a unique violation", err)
	}
	_, err = s.Submit(sessA, raw(t, clash))
	wantCode(t, err, "IDEMPOTENCY_CONFLICT")
	if fingerprint(t, e.path) != before {
		t.Fatal("conflict wrote")
	}
}
