package store

import (
	"context"
	"slices"
	"sync"
	"testing"
	"time"

	"github.com/SIMPLYBOYS/agent-payment-authority/internal/contracts"
)

func countType(t *testing.T, s *Store, sess contracts.Session, typ string) int {
	t.Helper()
	n := 0
	for _, x := range eventTypes(t, s, sess) {
		if x == typ {
			n++
		}
	}
	return n
}

// A downtime across the deadline: Open expires before serving, so the first
// query and the first replay both see it, once.
func TestRestartAcrossDeadline(t *testing.T) {
	e := newEnv(t)
	s := e.open()
	e.start(s, "task-a")
	submit(t, s, sessA, e.base)
	s.Close()
	e.clock.Add(40)
	s = e.open()
	p := purchaseOf(t, task(t, s, sessA), e.base.Intent.PurchaseOrderID)
	if p.ApprovalClosed != "expired" || p.ClosedAt != t0+40 || p.DeadlineAt != t0+30 {
		t.Fatalf("after restart: %+v", p)
	}
	if r := submit(t, s, sessA, e.base); !r.Replay || r.Purchase.ApprovalClosed != "expired" || r.Purchase.Decision.PolicyVerdict != "Ask" {
		t.Fatalf("first replay: %+v", r)
	}
	if n := countType(t, s, sessA, "APPROVAL_EXPIRED"); n != 1 {
		t.Fatalf("%d APPROVAL_EXPIRED", n)
	}
	consistent(t, s, sessA)
}

// 9.1 #6 and the accepted rule: 29 s pending, 30 and 31 s expired.
func TestAskDeadline(t *testing.T) {
	for wait, want := range map[uint64]string{29: "", 30: "expired", 31: "expired"} {
		e := newEnv(t)
		s := e.open()
		e.start(s, "task-a")
		submit(t, s, sessA, e.base)
		e.clock.Add(wait)
		if err := s.ExpireDue(); err != nil {
			t.Fatal(err)
		}
		if p := purchaseOf(t, task(t, s, sessA), e.base.Intent.PurchaseOrderID); p.ApprovalClosed != want {
			t.Fatalf("after %d s: closed %q, want %q", wait, p.ApprovalClosed, want)
		}
	}
}

// At the deadline an Ask can only close as expired, whichever host action
// takes the lock first; a second earlier it is withdrawn.
func TestCloseAtDeadline(t *testing.T) {
	type outcome struct {
		closed, status string
		withdrawn      int
	}
	run := func(t *testing.T, wait uint64, end string, concurrent bool) outcome {
		e := newEnv(t)
		s1, s2 := e.open(), e.open()
		e.start(s1, "task-a")
		submit(t, s1, sessA, e.base)
		e.clock.Add(wait)
		act := func() {
			var err error
			if end == "cancel" {
				err = s1.Cancel("task-a")
			} else {
				err = s1.CLITimeout("task-a")
			}
			if err != nil {
				t.Error(err)
			}
		}
		if concurrent {
			var wg sync.WaitGroup
			wg.Go(act)
			wg.Go(func() {
				if err := s2.ExpireDue(); err != nil {
					t.Error(err)
				}
			})
			wg.Wait()
		} else {
			act()
		}
		consistent(t, s1, sessA)
		v := task(t, s1, sessA)
		return outcome{purchaseOf(t, v, e.base.Intent.PurchaseOrderID).ApprovalClosed, v.Status, countType(t, s1, sessA, "APPROVAL_WITHDRAWN")}
	}
	for range 8 {
		if got := run(t, 30, "cancel", true); got != (outcome{"expired", "cancelled", 0}) {
			t.Fatalf("cancel racing expiry at the deadline: %+v", got)
		}
		if got := run(t, 30, "cli-timeout", true); got != (outcome{"expired", "failed", 0}) {
			t.Fatalf("CLI timeout racing expiry at the deadline: %+v", got)
		}
	}
	if got := run(t, 30, "cancel", false); got != (outcome{"expired", "cancelled", 0}) {
		t.Fatalf("cancel at the deadline: %+v", got)
	}
	if got := run(t, 29, "cancel", false); got != (outcome{"withdrawn", "cancelled", 1}) {
		t.Fatalf("cancel a second before: %+v", got)
	}
}

// CLI timeout before the deadline leaves the Ask pending; it expires later
// and the task stays failed.
func TestCLITimeoutThenExpiry(t *testing.T) {
	e := newEnv(t)
	s := e.open()
	e.start(s, "task-a")
	submit(t, s, sessA, e.base)
	if err := s.CLITimeout("task-a"); err != nil {
		t.Fatal(err)
	}
	if p := purchaseOf(t, task(t, s, sessA), e.base.Intent.PurchaseOrderID); p.ApprovalClosed != "" {
		t.Fatalf("CLI timeout closed the Ask: %+v", p)
	}
	e.clock.Add(30)
	if err := s.ExpireDue(); err != nil {
		t.Fatal(err)
	}
	v := task(t, s, sessA)
	if v.Status != "failed" || purchaseOf(t, v, e.base.Intent.PurchaseOrderID).ApprovalClosed != "expired" {
		t.Fatalf("after expiry: %+v", v)
	}
	_, err := s.Submit(sessA, raw(t, variant(t, e.base, 4)))
	wantCode(t, err, "TASK_NOT_ACTIVE")
	consistent(t, s, sessA)
}

// Several workers expiring the same Ask record it once.
func TestConcurrentExpiryOnce(t *testing.T) {
	e := newEnv(t)
	stores := []*Store{e.open(), e.open(), e.open(), e.open()}
	e.start(stores[0], "task-a")
	submit(t, stores[0], sessA, e.base)
	e.clock.Add(30)
	var wg sync.WaitGroup
	for _, s := range stores {
		wg.Go(func() {
			if err := s.ExpireDue(); err != nil {
				t.Error(err)
			}
		})
	}
	wg.Wait()
	if n := countType(t, stores[0], sessA, "APPROVAL_EXPIRED"); n != 1 {
		t.Fatalf("%d APPROVAL_EXPIRED", n)
	}
}

// holdLock takes the write lock from another connection until release is called.
func holdLock(t *testing.T, path string) (release func()) {
	t.Helper()
	db := rawDB(t, path)
	db.SetMaxOpenConns(1)
	if _, err := db.Exec("BEGIN IMMEDIATE"); err != nil {
		t.Fatal(err)
	}
	return func() { db.Exec("COMMIT") }
}

// The clock is read after the write lock is held: time that passes while an
// operation waits for the lock counts. The hook runs when Submit is about to
// ask for the lock and holds it there until the clock has moved, so an
// implementation that sampled the clock any earlier would see the old time.
func TestClockSampledAfterLock(t *testing.T) {
	cases := []struct {
		name     string
		purchase func(e *env) contracts.PurchaseOrder
		prepare  func(t *testing.T, e *env, s *Store)
		from, to uint64
		check    func(t *testing.T, r Result, s *Store, e *env)
	}{
		{"deadline passes while waiting", func(e *env) contracts.PurchaseOrder { return e.base },
			func(t *testing.T, e *env, s *Store) { submit(t, s, sessA, e.base) }, t0 + 29, t0 + 31,
			func(t *testing.T, r Result, s *Store, e *env) {
				if !r.Replay || r.Purchase.ApprovalClosed != "expired" || r.Purchase.ClosedAt != t0+31 {
					t.Fatalf("replay: %+v", r)
				}
			}},
		{"valid_before passes while waiting", func(e *env) contracts.PurchaseOrder { return e.base },
			func(*testing.T, *env, *Store) {}, 1790727000 - 1, 1790727000,
			func(t *testing.T, r Result, s *Store, e *env) {
				if r.Purchase.Decision.Reason != "AUTHORIZATION_EXPIRED" || r.Purchase.DecidedAt != 1790727000 {
					t.Fatalf("decision: %+v", r.Purchase)
				}
			}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			e := newEnv(t)
			s := e.open()
			e.start(s, "task-a")
			c.prepare(t, e, s)
			e.clock.Set(c.from)
			release := holdLock(t, e.path)
			called, advanced := make(chan struct{}), make(chan struct{})
			s.hook = func(at string) error {
				if at == "before-begin" {
					close(called)
					<-advanced
				}
				return nil
			}
			done := make(chan Result)
			go func() { done <- submit(t, s, sessA, c.purchase(e)) }()
			<-called
			e.clock.Set(c.to)
			close(advanced)
			release()
			c.check(t, <-done, s, e)
		})
	}
}

// A replay with a reached deadline adds only that APPROVAL_EXPIRED; a
// conflicting request rolls the expiry back with itself.
func TestExpiryAroundReplayAndConflict(t *testing.T) {
	e := newEnv(t)
	s := e.open()
	e.start(s, "task-a")
	submit(t, s, sessA, e.base)
	e.clock.Add(30)
	conflict := e.base
	conflict.Intent.PurchaseOrderID = "po-other"
	_, err := s.Submit(sessA, raw(t, conflict))
	wantCode(t, err, "IDEMPOTENCY_CONFLICT")
	if n := countType(t, s, sessA, "APPROVAL_EXPIRED"); n != 0 {
		t.Fatal("the refused request kept its expiry write")
	}
	before := eventTypes(t, s, sessA)
	r := submit(t, s, sessA, e.base)
	after := eventTypes(t, s, sessA)
	if !r.Replay || r.Purchase.ApprovalClosed != "expired" || !slices.Equal(after, append(before, "APPROVAL_EXPIRED")) {
		t.Fatalf("replay %+v; events %v -> %v", r, before, after)
	}
}

func TestClockRegression(t *testing.T) {
	e := newEnv(t)
	s := e.open()
	e.start(s, "task-a")
	e.start(s, "task-b")
	submit(t, s, sessA, e.base)
	e.clock.Add(10)
	if err := s.Cancel("task-b"); err != nil {
		t.Fatal(err)
	}
	before := fingerprint(t, e.path)
	e.clock.Set(t0) // behind the latest recorded time
	_, err := s.Submit(sessA, raw(t, variant(t, e.base, 5)))
	wantCode(t, err, "CLOCK_REGRESSION")
	wantCode(t, s.Cancel("task-a"), "CLOCK_REGRESSION")
	wantCode(t, s.CLITimeout("task-a"), "CLOCK_REGRESSION")
	if err := s.ExpireDue(); Code(err) != "CLOCK_REGRESSION" {
		t.Fatalf("ExpireDue: %v", err)
	}
	if fingerprint(t, e.path) != before {
		t.Fatal("a regressed clock wrote")
	}
	if v := task(t, s, sessB); v.Status != "cancelled" {
		t.Fatalf("reads still work and the ended task stays ended: %+v", v)
	}
	// expire itself never underflows when now is before decided_at.
	tx, _ := s.w.Begin()
	defer tx.Rollback()
	tt, err := s.openTask(tx, t0-100, "task-a")
	if err != nil {
		t.Fatal(err)
	}
	if err := tt.expire(); err != nil || len(tt.events) != tt.stored {
		t.Fatalf("expire before decided_at: %v, %d new events", err, len(tt.events)-tt.stored)
	}
}

// Queries return the committed snapshot; a passed deadline without an
// expiry event still reads as pending, with its deadline.
func TestQueryIsCommittedSnapshot(t *testing.T) {
	e := newEnv(t)
	s := e.open()
	e.start(s, "task-a")
	submit(t, s, sessA, e.base)
	e.clock.Add(40)
	v := task(t, s, sessA)
	p := purchaseOf(t, v, e.base.Intent.PurchaseOrderID)
	if p.ApprovalClosed != "" || p.DeadlineAt != t0+30 || v.AsOfObservedAt != t0 || v.AsOfSequence != 3 || v.Status != "awaiting_approval" {
		t.Fatalf("snapshot %+v / %+v", v, p)
	}
}

func TestRunExpiryStops(t *testing.T) {
	e := newEnv(t)
	s := e.open()
	e.start(s, "task-a")
	submit(t, s, sessA, e.base)
	e.clock.Add(30)
	ctx, cancel := context.WithCancel(context.Background())
	stopped := make(chan struct{})
	go func() { s.RunExpiry(ctx, time.Millisecond, func(err error) { t.Error(err) }); close(stopped) }()
	deadline := time.Now().Add(5 * time.Second)
	for countType(t, s, sessA, "APPROVAL_EXPIRED") == 0 {
		if time.Now().After(deadline) {
			t.Fatal("RunExpiry never expired the Ask")
		}
		time.Sleep(time.Millisecond)
	}
	cancel()
	select {
	case <-stopped:
	case <-time.After(5 * time.Second):
		t.Fatal("RunExpiry did not stop")
	}
}

// D1: the shared projection over interleavings, through real writes; the
// stored columns, the events and project() must agree in every case.
func TestProjectionInterleavings(t *testing.T) {
	type step func(t *testing.T, e *env, s *Store)
	ask := func(n int) step {
		return func(t *testing.T, e *env, s *Store) { submit(t, s, sessA, variant(t, e.base, n)) }
	}
	allow := func(t *testing.T, e *env, s *Store) { submit(t, s, sessA, e.allow) }
	deny := func(t *testing.T, e *env, s *Store) { submit(t, s, sessA, e.denied) }
	wait := func(d uint64) step {
		return func(t *testing.T, e *env, s *Store) { e.clock.Add(d); s.ExpireDue() }
	}
	cancel := func(t *testing.T, e *env, s *Store) { s.Cancel("task-a") }
	timeout := func(t *testing.T, e *env, s *Store) { s.CLITimeout("task-a") }
	cases := []struct {
		name         string
		steps        []step
		status, deny string
	}{
		{"Ask A open, B Allow", []step{ask(1), allow}, "awaiting_approval", ""},
		{"Ask A open, B Deny, A expires", []step{ask(1), deny, wait(30)}, "denied", "ASSET_NOT_ALLOWED"},
		{"Ask A open, CLI timeout, A expires", []step{ask(1), timeout, wait(30)}, "failed", ""},
		{"two Asks, one expires", []step{ask(1), wait(10), ask(2), wait(20)}, "awaiting_approval", ""},
		{"last Ask expires", []step{ask(1), wait(30)}, "running", ""},
		{"Deny then cancel", []step{deny, cancel}, "denied", "ASSET_NOT_ALLOWED"},
		{"cancel without purchases", []step{cancel}, "cancelled", ""},
		{"Allow only", []step{allow}, "running", ""},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			e := newEnv(t)
			s := e.open()
			e.start(s, "task-a")
			for _, st := range c.steps {
				st(t, e, s)
			}
			if v := task(t, s, sessA); v.Status != c.status || v.DenyReason != c.deny {
				t.Fatalf("got %s/%s, want %s/%s", v.Status, v.DenyReason, c.status, c.deny)
			}
			consistent(t, s, sessA)
			for _, p := range task(t, s, sessA).Purchases {
				if p.ApprovalClosed == "expired" && p.Decision.PolicyVerdict != "Ask" {
					t.Fatal("an expired Ask turned into another verdict")
				}
			}
		})
	}
}
