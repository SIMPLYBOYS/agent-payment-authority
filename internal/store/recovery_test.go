package store

import (
	"errors"
	"fmt"
	"strings"
	"sync"
	"testing"
)

// richHistory leaves a closed database with every kind of row: task-a has an
// expired Ask and an Allow, task-b a Deny then a cancel, task-a-next an open Ask.
func richHistory(t *testing.T) *env {
	t.Helper()
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
	e.start(s, "task-a-next")
	submit(t, s, sessANext, variant(t, forTask(t, e.base, "task-a-next"), 7))
	s.Close()
	return e
}

func openErr(e *env) error {
	s, err := Open(Config{Path: e.path, Registry: e.reg, Offers: e.offers, Clock: e.clock.clock()})
	if err == nil {
		s.Close()
	}
	return err
}

func eventHash(t *testing.T, path, task string, seq int) string {
	t.Helper()
	return mustScalar(t, path, fmt.Sprintf(`SELECT event_hash FROM events WHERE task_id = '%s' AND sequence = %d`, task, seq))
}

// One field changed at a time; Open must refuse and name the check.
func TestRecoveryRefusesCorruption(t *testing.T) {
	cases := []struct {
		name, check string
		sql         []string
	}{
		{"snapshot body", "snapshot", []string{`UPDATE config_snapshots SET body = replace(body, 'm1-demo-v1', 'm1-demo-v9') WHERE kind = 'profile'`}},
		{"snapshot hash", "foreign_keys", []string{`UPDATE config_snapshots SET snapshot_hash = 'sha256:00' WHERE snapshot_hash = (SELECT terms_snapshot FROM tasks WHERE task_id = 'task-a')`}},
		{"snapshot kind", "snapshot", []string{`UPDATE config_snapshots SET kind = 'credential' WHERE kind = 'profile'`}},
		{"referenced snapshot deleted", "foreign_keys", []string{`DELETE FROM config_snapshots WHERE kind = 'profile'`}},
		{"profile present but column NULL", "snapshot", []string{`UPDATE tasks SET profile_snapshot = NULL WHERE task_id = 'task-a'`}},
		{"task status", "projection", []string{`UPDATE tasks SET status = 'running' WHERE task_id = 'task-b'`}},
		{"task deny_reason", "projection", []string{`UPDATE tasks SET deny_reason = 'INSUFFICIENT_BUDGET' WHERE task_id = 'task-b'`}},
		{"task next_sequence", "chain", []string{`UPDATE tasks SET next_sequence = next_sequence + 1 WHERE task_id = 'task-a'`}},
		{"task chain_head", "chain", []string{`UPDATE tasks SET chain_head = 'sha256:00' WHERE task_id = 'task-a'`}},
		{"task budget_remaining", "budget", []string{`UPDATE tasks SET budget_remaining = '4999999' WHERE task_id = 'task-a'`}},
		{"task wallet", "task", []string{`UPDATE tasks SET wallet_id = 'wallet-b' WHERE task_id = 'task-a'`}},
		{"task payer", "task", []string{`UPDATE tasks SET payer = '0x2222222222222222222222222222222222222222' WHERE task_id = 'task-a'`}},
		{"purchase body", "purchase", []string{`UPDATE purchases SET body = replace(body, '"request_id":"request-1"', '"request_id":"request-8"') WHERE task_id = 'task-a'`}},
		{"purchase intent_hash", "purchase", []string{`UPDATE purchases SET intent_hash = 'sha256:00' WHERE task_id = 'task-a' AND request_id = 'request-1'`}},
		{"purchase retry_key", "purchase", []string{`UPDATE purchases SET retry_key = 'sha256:00' WHERE task_id = 'task-a' AND request_id = 'request-1'`}},
		{"purchase payer", "purchase", []string{`UPDATE purchases SET payer = '0x2222222222222222222222222222222222222222' WHERE task_id = 'task-a' AND request_id = 'request-1'`}},
		{"purchase nonce", "purchase", []string{`UPDATE purchases SET eip3009_nonce = '0x` + strings.Repeat("e", 64) + `' WHERE task_id = 'task-a' AND request_id = 'request-1'`}},
		{"purchase verdict", "decision", []string{`UPDATE purchases SET policy_verdict = 'Deny' WHERE task_id = 'task-a' AND request_id = 'request-1'`}},
		{"purchase decided_at", "decision", []string{`UPDATE purchases SET decided_at = decided_at + 1 WHERE task_id = 'task-a' AND request_id = 'request-1'`}},
		{"purchase approval_closed", "decision", []string{`UPDATE purchases SET approval_closed = 'withdrawn' WHERE task_id = 'task-a' AND request_id = 'request-1'`}},
		{"purchase closed_at", "decision", []string{`UPDATE purchases SET closed_at = closed_at + 1 WHERE task_id = 'task-a' AND request_id = 'request-1'`}},
		{"purchase closed without event", "decision", []string{`UPDATE purchases SET approval_closed = 'expired', closed_at = 1790726500 WHERE task_id = 'task-a-next'`}},
		{"event body", "event", []string{`UPDATE events SET body = replace(body, '"observed_at":1790726450', '"observed_at":1790726451') WHERE task_id = 'task-a' AND sequence = 1`}},
		{"event hash", "event", []string{`UPDATE events SET event_hash = 'sha256:00' WHERE task_id = 'task-a' AND sequence = 2`}},
		{"event sequence", "event", []string{`UPDATE events SET sequence = 99 WHERE task_id = 'task-a' AND sequence = 6`}},
		{"event observed_at", "event", []string{`UPDATE events SET observed_at = observed_at + 1 WHERE task_id = 'task-a' AND sequence = 2`}},
		{"event type", "event", []string{`UPDATE events SET event_type = 'TASK_STARTED' WHERE task_id = 'task-a' AND sequence = 2`}},
		{"event purchase reference", "event", []string{`UPDATE events SET purchase_order_id = NULL WHERE task_id = 'task-a' AND sequence = 2`}},
		{"purchase deleted, events stay", "foreign_keys", []string{`DELETE FROM purchases WHERE task_id = 'task-a' AND request_id = 'request-50'`}},
		{"middle event deleted", "chain", []string{`DELETE FROM events WHERE task_id = 'task-a' AND sequence = 2`}},
		{"event inserted", "event", []string{`INSERT INTO events SELECT task_id, 99, 'evt-inserted', event_type, purchase_order_id, observed_at, body, event_hash FROM events WHERE task_id = 'task-a' AND sequence = 2`}},
		{"tail deleted, head not updated", "chain", []string{`DELETE FROM events WHERE task_id = 'task-b' AND sequence = 4`}},
		{"schema version", "schema", []string{`PRAGMA user_version = 5`}}, // 4 is the current schema since P1-b
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			e := richHistory(t)
			db := rawDB(t, e.path)
			for _, q := range c.sql {
				if _, err := db.Exec(q); err != nil {
					t.Fatalf("%s: %v", q, err)
				}
			}
			var ce *CorruptError
			if err := openErr(e); !errors.As(err, &ce) || ce.Check != c.check {
				t.Fatalf("Open: %v, want a %q corruption", err, c.check)
			}
		})
	}
}

// Without an external checkpoint a truncated tail with a re-synced head can
// pass; this test pins that documented limit. A truncation the other tables
// contradict is still caught.
func TestRecoveryTailLimits(t *testing.T) {
	e := richHistory(t)
	db := rawDB(t, e.path)
	head := eventHash(t, e.path, "task-b", 3)
	for _, q := range []string{
		`DELETE FROM events WHERE task_id = 'task-b' AND sequence = 4`,
		`UPDATE tasks SET chain_head = '` + head + `', next_sequence = 4 WHERE task_id = 'task-b'`,
	} {
		if _, err := db.Exec(q); err != nil {
			t.Fatal(err)
		}
	}
	if err := openErr(e); err != nil {
		t.Fatalf("documented limit changed: a consistent tail truncation is now detected (%v); update the design", err)
	}

	e = richHistory(t)
	db = rawDB(t, e.path)
	head = eventHash(t, e.path, "task-a-next", 1)
	for _, q := range []string{
		`DELETE FROM events WHERE task_id = 'task-a-next' AND sequence > 1`,
		`UPDATE tasks SET chain_head = '` + head + `', next_sequence = 2, status = 'running' WHERE task_id = 'task-a-next'`,
	} {
		if _, err := db.Exec(q); err != nil {
			t.Fatal(err)
		}
	}
	var ce *CorruptError
	if err := openErr(e); !errors.As(err, &ce) || ce.Check != "decision" {
		t.Fatalf("Open: %v; the purchase row without its events must be caught", err)
	}
}

// Recovery reads one consistent snapshot: legitimate commits from another
// connection during verification are never reported as corruption.
func TestRecoveryUnderConcurrentWrites(t *testing.T) {
	e := richHistory(t)
	writer, checker := e.open(), e.open()
	var wg sync.WaitGroup
	wg.Go(func() {
		for n := 100; n < 130; n++ {
			if _, err := writer.Submit(sessANext, raw(t, variant(t, forTask(t, e.base, "task-a-next"), n))); err != nil {
				t.Error(err)
				return
			}
		}
	})
	wg.Go(func() {
		for range 30 {
			if err := checker.verify(schemaVersion); err != nil {
				t.Errorf("verify during commits: %v", err)
				return
			}
		}
	})
	wg.Wait()
	consistent(t, writer, sessANext)
}
