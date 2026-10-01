package store

import (
	"context"
	"database/sql"
	"database/sql/driver"
	"errors"
	"strings"
	"sync/atomic"
	"testing"

	"modernc.org/sqlite"
)

var errInjected = errors.New("injected non-EOF storage read failure")

// faultPlan makes the failAt-th Rows.Next of every query containing match
// return errInjected, after the query itself succeeded. hits counts how often
// the fault fired, so a test cannot pass without reaching it.
type faultPlan struct {
	match  string
	failAt int
	hits   atomic.Int32
}

type faultConnector struct {
	dsn  string
	plan *faultPlan
}

func (c faultConnector) Driver() driver.Driver { return &sqlite.Driver{} }
func (c faultConnector) Connect(context.Context) (driver.Conn, error) {
	conn, err := (&sqlite.Driver{}).Open(c.dsn)
	if err != nil {
		return nil, err
	}
	return &faultConn{Conn: conn, plan: c.plan}, nil
}

type faultConn struct {
	driver.Conn
	plan *faultPlan
}

func (c *faultConn) QueryContext(ctx context.Context, q string, args []driver.NamedValue) (driver.Rows, error) {
	rows, err := c.Conn.(driver.QueryerContext).QueryContext(ctx, q, args)
	if err != nil || !strings.Contains(q, c.plan.match) {
		return rows, err
	}
	return &faultRows{Rows: rows, plan: c.plan}, nil
}

func (c *faultConn) BeginTx(ctx context.Context, opts driver.TxOptions) (driver.Tx, error) {
	return c.Conn.(driver.ConnBeginTx).BeginTx(ctx, opts)
}

type faultRows struct {
	driver.Rows
	plan *faultPlan
	n    int
}

func (r *faultRows) Next(dest []driver.Value) error {
	r.n++
	if r.n == r.plan.failAt {
		r.plan.hits.Add(1)
		return errInjected
	}
	return r.Rows.Next(dest)
}

// faultyReader makes Open use the fault-injecting driver for its read handle
// (recovery and queries) until the test ends.
func faultyReader(t *testing.T, plan *faultPlan) {
	t.Helper()
	orig := openReader
	openReader = func(dsn string) (*sql.DB, error) { return sql.OpenDB(faultConnector{dsn, plan}), nil }
	t.Cleanup(func() { openReader = orig })
}

// REV1: a failed row read during recovery is never taken for the end of the
// result set. Open refuses with that error (wrapped as a ReadError naming the
// check), returns no Store and writes nothing, not even the expiry it would
// otherwise record. Each scan fails on its first row and, where the scan has
// several rows, after some rows were read.
func TestRecoveryReadFailures(t *testing.T) {
	const (
		fkScan        = "PRAGMA foreign_key_check"
		snapshotScan  = "SELECT snapshot_hash, kind, body FROM config_snapshots"
		taskScan      = "FROM tasks ORDER BY task_id"
		loadEvents    = "SELECT body FROM events WHERE task_id"
		loadPurchases = "SELECT purchase_order_id, body, intent_hash"
		purchaseScan  = "SELECT purchase_order_id, request_id, retry_key"
		eventScan     = "SELECT sequence, event_id, event_type"
	)
	cases := []struct {
		name, match string
		failAt      int
		check       string
	}{
		// foreign_key_check returns no rows on a sound database: only the first read can fail.
		{"foreign keys, first row", fkScan, 1, "foreign_keys"},
		{"snapshots, first row", snapshotScan, 1, "snapshot"},
		{"snapshots, after rows", snapshotScan, 3, "snapshot"},
		{"tasks, first row", taskScan, 1, "task"},
		{"tasks, after rows", taskScan, 2, "task"},
		{"task load events, first row", loadEvents, 1, "task"},
		{"task load events, after rows", loadEvents, 3, "task"},
		{"task load purchases, first row", loadPurchases, 1, "task"},
		{"task load purchases, after rows", loadPurchases, 2, "task"},
		{"purchases, first row", purchaseScan, 1, "purchase"},
		{"purchases, after rows", purchaseScan, 2, "purchase"},
		{"events, first row", eventScan, 1, "event"},
		{"events, after rows", eventScan, 4, "event"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			e := richHistory(t)
			e.clock.Add(100) // task-a-next's open Ask is due: a passing recovery would expire it
			before := fingerprint(t, e.path)
			plan := &faultPlan{match: c.match, failAt: c.failAt}
			faultyReader(t, plan)
			s, err := Open(Config{Path: e.path, Registry: e.reg, Offers: e.offers, Clock: e.clock.clock()})
			if s != nil {
				s.Close()
				t.Fatal("Open returned a Store after a failed recovery read")
			}
			var re *ReadError
			if plan.hits.Load() == 0 {
				t.Fatalf("the fault was never reached (%v)", err)
			}
			if !errors.Is(err, errInjected) || !errors.As(err, &re) || re.Check != c.check {
				t.Fatalf("Open: %v; want the injected failure as a %q ReadError", err, c.check)
			}
			if fingerprint(t, e.path) != before {
				t.Fatal("a failed recovery still wrote")
			}
		})
	}

	// The same database without a fault: normal EOF everywhere, recovery
	// passes and the due Ask is expired.
	e := richHistory(t)
	e.clock.Add(100)
	s := e.open()
	if p := purchaseOf(t, task(t, s, sessANext), "po-task-a-next-7"); p.ApprovalClosed != "expired" {
		t.Fatalf("after a clean recovery: %+v", p)
	}
	// An empty database also opens through the wrapped reader.
	plan := &faultPlan{match: "never matches any query"}
	faultyReader(t, plan)
	newEnv(t).open()
}
