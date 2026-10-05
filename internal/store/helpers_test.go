package store

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/SIMPLYBOYS/agent-payment-authority/internal/contracts"
	"github.com/SIMPLYBOYS/agent-payment-authority/internal/policy"
)

const t0 = 1790726450 // cases.json defaults.clock, inside the fixture purchase's validity window

// fakeClock is the injected trusted clock. Tests move it explicitly; nothing
// depends on wall time.
type fakeClock struct {
	mu  sync.Mutex
	now uint64
}

func (c *fakeClock) Now() uint64    { c.mu.Lock(); defer c.mu.Unlock(); return c.now }
func (c *fakeClock) Set(v uint64)   { c.mu.Lock(); c.now = v; c.mu.Unlock() }
func (c *fakeClock) Add(d uint64)   { c.mu.Lock(); c.now += d; c.mu.Unlock() }
func (c *fakeClock) clock() Clock   { return c.Now }
func newClock(at uint64) *fakeClock { return &fakeClock{now: at} }

var (
	sessA     = contracts.Session{ConsumerID: "consumer-a", AgentID: "agent-a", TaskID: "task-a"}
	sessANext = contracts.Session{ConsumerID: "consumer-a", AgentID: "agent-a", TaskID: "task-a-next"}
	sessB     = contracts.Session{ConsumerID: "consumer-b", AgentID: "agent-b", TaskID: "task-b"}
)

const untrustedAsset = "0x5555555555555555555555555555555555555555"

type env struct {
	t       *testing.T
	path    string
	clock   *fakeClock
	reg     contracts.RegistryConfig
	offers  OfferBook
	base    contracts.PurchaseOrder // purchase-input.json: 0.50, an Ask under the demo profile
	allow   contracts.PurchaseOrder // 0.05: Allow-unavailable
	denied  contracts.PurchaseOrder // untrusted asset: Deny ASSET_NOT_ALLOWED
	profile policy.Profile
}

func readFixture(t *testing.T, path string, dst any) {
	t.Helper()
	raw, err := os.ReadFile("../../" + path)
	if err != nil {
		t.Fatal(err)
	}
	if err := contracts.Decode(raw, dst); err != nil {
		t.Fatalf("%s: %v", path, err)
	}
}

func newEnv(t *testing.T) *env {
	t.Helper()
	e := &env{t: t, path: filepath.Join(t.TempDir(), "db", "apa.db"), clock: newClock(t0)}
	readFixture(t, "testdata/contracts/registry.json", &e.reg)
	readFixture(t, "testdata/contracts/purchase-input.json", &e.base)
	raw, err := os.ReadFile("../../testdata/policy/m1-demo-profile.json")
	if err != nil {
		t.Fatal(err)
	}
	if e.profile, err = policy.LoadProfile(raw); err != nil {
		t.Fatal(err)
	}
	e.allow = variant(t, editQuote(t, e.base, "amount", "50000"), 50)
	e.denied = variant(t, editQuote(t, e.base, "asset", untrustedAsset), 60)
	e.offers = OfferBook{}
	for _, p := range []contracts.PurchaseOrder{e.base, e.allow, e.denied} {
		e.offer(p)
	}
	return e
}

func (e *env) offer(p contracts.PurchaseOrder) {
	k := OfferKey{p.Intent.HTTPMethod, p.Intent.ResourceURL}
	e.offers[k] = append(e.offers[k], p.Quote)
}

func (e *env) open() *Store {
	e.t.Helper()
	s, err := Open(Config{Path: e.path, Registry: e.reg, Offers: e.offers, Clock: e.clock.clock()})
	if err != nil {
		e.t.Fatalf("Open: %v", err)
	}
	e.t.Cleanup(func() { s.Close() })
	return s
}

// setup is the demo profile with a valid fixture credential for the task's consumer.
func (e *env) setup(taskID string) TaskSetup {
	p := e.profile
	consumer := map[string]string{"task-a": "consumer-a", "task-a-next": "consumer-a", "task-b": "consumer-b"}[taskID]
	return TaskSetup{TaskID: taskID, Profile: &p, Credential: &policy.Credential{ConsumerID: consumer, ValidUntil: t0 + 86400, EvidenceMode: "fixture"}}
}

func (e *env) start(s *Store, taskID string) {
	e.t.Helper()
	if err := s.StartTask(e.setup(taskID)); err != nil {
		e.t.Fatalf("StartTask %s: %v", taskID, err)
	}
}

func raw(t *testing.T, p contracts.PurchaseOrder) []byte {
	t.Helper()
	b, err := json.Marshal(p)
	if err != nil {
		t.Fatal(err)
	}
	return b
}

func submit(t *testing.T, s *Store, sess contracts.Session, p contracts.PurchaseOrder) Result {
	t.Helper()
	r, err := s.Submit(sess, raw(t, p))
	if err != nil {
		t.Fatalf("Submit %s: %v", p.Intent.PurchaseOrderID, err)
	}
	return r
}

func wantCode(t *testing.T, err error, code string) {
	t.Helper()
	if Code(err) != code {
		t.Fatalf("error %v, want code %s", err, code)
	}
}

// variant is the same quote under another request, purchase order and nonce.
func variant(t *testing.T, p contracts.PurchaseOrder, n int) contracts.PurchaseOrder {
	t.Helper()
	p.RequestID = fmt.Sprintf("request-%d", n)
	p.Intent.PurchaseOrderID = fmt.Sprintf("po-%s-%d", p.Intent.TaskID, n)
	p.Intent.EIP3009Nonce = fmt.Sprintf("0x%064x", n)
	return p
}

// forTask moves a purchase to another task of the registry, with that task's
// consumer, agent, wallet and payer.
func forTask(t *testing.T, p contracts.PurchaseOrder, task string) contracts.PurchaseOrder {
	t.Helper()
	switch task {
	case "task-b":
		p.Intent.ConsumerID, p.Intent.AgentID, p.Intent.WalletID, p.Intent.From = "consumer-b", "agent-b", "wallet-b", "0x2222222222222222222222222222222222222222"
	case "task-a", "task-a-next":
		p.Intent.ConsumerID, p.Intent.AgentID, p.Intent.WalletID, p.Intent.From = "consumer-a", "agent-a", "wallet-a", "0x1111111111111111111111111111111111111111"
	}
	p.Intent.TaskID = task
	return p
}

// editQuote changes one field in both intent and quoted requirement and
// recomputes the requirement hash, as the trusted shop would for a new quote.
func editQuote(t *testing.T, p contracts.PurchaseOrder, field string, value any) contracts.PurchaseOrder {
	t.Helper()
	var req map[string]any
	if err := json.Unmarshal(p.Quote.SelectedRequirement, &req); err != nil {
		t.Fatal(err)
	}
	switch field {
	case "amount":
		p.Intent.Amount, req["amount"] = contracts.Amount(value.(string)), value
	case "asset":
		p.Intent.AssetContract, req["asset"] = value.(string), value
	default:
		extra := req["extra"].(map[string]any)
		extra[field] = value
	}
	b, err := json.Marshal(req)
	if err != nil {
		t.Fatal(err)
	}
	p.Quote.SelectedRequirement = b
	if p.Intent.PaymentRequirementHash, err = p.Quote.Hash(); err != nil {
		t.Fatal(err)
	}
	return p
}

// rawDB opens the database file directly, with foreign keys off, to inspect
// or deliberately corrupt it.
func rawDB(t *testing.T, path string) *sql.DB {
	t.Helper()
	db, err := sql.Open("sqlite", path+"?_pragma=busy_timeout(5000)")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	return db
}

func count(t *testing.T, path, table, where string, args ...any) int {
	t.Helper()
	var n int
	q := "SELECT count(*) FROM " + table
	if where != "" {
		q += " WHERE " + where
	}
	if err := rawDB(t, path).QueryRow(q, args...).Scan(&n); err != nil {
		t.Fatal(err)
	}
	return n
}

// fingerprint is every row of every table, to prove an operation wrote nothing.
func fingerprint(t *testing.T, path string) string {
	t.Helper()
	db := rawDB(t, path)
	var b strings.Builder
	for _, q := range []string{
		`SELECT snapshot_hash FROM config_snapshots ORDER BY 1`,
		`SELECT task_id || status || coalesce(deny_reason,'') || next_sequence || chain_head FROM tasks ORDER BY 1`,
		`SELECT task_id || purchase_order_id || coalesce(approval_closed,'') || coalesce(closed_at,'') FROM purchases ORDER BY 1`,
		`SELECT task_id || sequence || event_hash FROM events ORDER BY 1`,
	} {
		rows, err := db.Query(q)
		if err != nil {
			t.Fatal(err)
		}
		for rows.Next() {
			var s string
			rows.Scan(&s)
			b.WriteString(s)
			b.WriteByte('\n')
		}
		rows.Close()
	}
	return b.String()
}

func eventTypes(t *testing.T, s *Store, sess contracts.Session) []string {
	t.Helper()
	events, err := s.Events(sess)
	if err != nil {
		t.Fatal(err)
	}
	var types []string
	for _, e := range events {
		types = append(types, e.EventType)
	}
	return types
}

func task(t *testing.T, s *Store, sess contracts.Session) TaskView {
	t.Helper()
	v, err := s.Task(sess)
	if err != nil {
		t.Fatal(err)
	}
	return v
}

func purchaseOf(t *testing.T, v TaskView, po string) PurchaseView {
	t.Helper()
	for _, p := range v.Purchases {
		if p.PurchaseOrderID == po {
			return p
		}
	}
	t.Fatalf("purchase %s not in task %s", po, v.TaskID)
	return PurchaseView{}
}

// consistent checks the shared projection against what was committed.
func consistent(t *testing.T, s *Store, sess contracts.Session) {
	t.Helper()
	events, err := s.Events(sess)
	if err != nil {
		t.Fatal(err)
	}
	if err := contracts.ValidateEventOrder(events); err != nil {
		t.Fatalf("event stream: %v", err)
	}
	status, reason := project(events)
	if v := task(t, s, sess); v.Status != status || v.DenyReason != reason {
		t.Fatalf("stored %s/%s, events give %s/%s", v.Status, v.DenyReason, status, reason)
	}
	if err := s.verify(schemaVersion); err != nil {
		t.Fatalf("verify: %v", err)
	}
}
