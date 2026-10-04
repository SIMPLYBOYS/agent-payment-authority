package main

// Regression tests for the review of 4a25bc8 (docs/demo-integration.md
// section 8): R1 the token file and the database's files, R2 a stored
// task's own binding, and the stop order.

import (
	"context"
	"encoding/json"
	"errors"
	"maps"
	"net/http"
	"os"
	"path/filepath"
	"reflect"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/SIMPLYBOYS/agent-payment-authority/internal/contracts"
	"github.com/SIMPLYBOYS/agent-payment-authority/internal/store"
)

// R1: a token path naming the database, its WAL or SHM file or its lock,
// however spelled, is refused before any lock, Store or token write.
func TestTokenPathMustNotBeADatabaseFile(t *testing.T) {
	dir, err := os.MkdirTemp(testRoot, "paths-")
	if err != nil {
		t.Fatal(err)
	}
	db := filepath.Join(dir, "demo.db")
	link := filepath.Join(testRoot, "link-"+filepath.Base(dir))
	if err := os.Symlink(dir, link); err != nil {
		t.Fatal(err)
	}
	cwd, _ := os.Getwd()
	rel, _ := filepath.Rel(cwd, db)
	for name, tok := range map[string]string{
		"database": db, "wal": db + "-wal", "shm": db + "-shm", "lock": db + ".lock",
		"relative": rel, "dot": dir + "/./demo.db", "dot-dot": dir + "/sub/../demo.db-wal",
		"symlinked parent": filepath.Join(link, "demo.db"), "symlinked parent, lock": filepath.Join(link, "demo.db.lock"),
	} {
		if err := checkTokenPath(db, tok); !errors.Is(err, errTokenPath) {
			t.Errorf("%s (%s): %v", name, tok, err)
		}
	}
	if err := checkTokenPath(rel, db); !errors.Is(err, errTokenPath) {
		t.Errorf("relative database, absolute token: %v", err)
	}
	if err := checkTokenPath(db, filepath.Join(dir, "tokens.json")); err != nil {
		t.Errorf("a separate token file: %v", err)
	}
	// An existing file under another name (a hard link) is the same file.
	os.WriteFile(db, []byte("x"), 0o600)
	if err := os.Link(db, filepath.Join(dir, "other-name")); err != nil {
		t.Fatal(err)
	}
	if err := checkTokenPath(db, filepath.Join(dir, "other-name")); !errors.Is(err, errTokenPath) {
		t.Errorf("hard link to the database: %v", err)
	}
}

// R1 through the real start: a database holding a purchase, then each
// conflicting token path. Nothing serves, nothing is declared ready, and
// the database and its files keep their bytes; a held lock stays held;
// a separate token file then starts normally on the same database.
func TestTokenPathConflictRefusedAtStart(t *testing.T) {
	h := startHost(t, hostOpts{runs: []planRun{{TaskID: "task-a", Scenario: mainline(t)}}, expiryEvery: time.Hour})
	h.waitRuns()
	if err := h.stop(); err != nil {
		t.Fatal(err)
	}
	db := filepath.Join(h.dir, "demo.db")
	files := func() map[string]string {
		m := map[string]string{}
		for _, f := range dbFiles(db) {
			if b, err := os.ReadFile(f); err == nil {
				m[f] = string(b)
			}
		}
		return m
	}
	before := files()
	if !strings.HasPrefix(before[db], "SQLite format 3") {
		t.Fatal("setup: no database")
	}
	frozen := wallClock() // controllable: nothing here may advance or expire anything
	for _, tok := range append(dbFiles(db), h.dir+"/./demo.db", filepath.Join(h.dir, "x", "..", "demo.db.lock")) {
		base := hostEnv(h.dir, "127.0.0.1:0")
		env := func(k string) string {
			if k == "APA_DEMO_TOKEN_FILE" {
				return tok
			}
			return base(k)
		}
		ready := make(chan string, 1)
		opened := false // the refusal must come before the Store opens
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		err := serve(ctx, []string{"--trace-dir", filepath.Join(h.dir, "runs")},
			deps{getenv: env, out: &lockedBuffer{}, clock: func() uint64 { return frozen }, ready: ready, expiryEvery: time.Hour,
				onStore: func(*store.Store) { opened = true }})
		cancel()
		if !errors.Is(err, errTokenPath) || len(ready) != 0 || opened {
			t.Errorf("token %s: ready %d, Store opened %t, %v", tok, len(ready), opened, err)
		}
		if after := files(); !maps.Equal(after, before) {
			t.Errorf("token %s: the database's files changed", tok)
		}
	}

	// Another owner holds the lock: the token writer refuses its path, and
	// the lock still excludes a second owner.
	unlock, err := lockDB(db)
	if err != nil {
		t.Fatal(err)
	}
	var cfg contracts.RegistryConfig
	decodeFile("testdata/contracts/registry.json", &cfg)
	if _, err := newTokens(cfg, db+".lock", db); !errors.Is(err, errTokenPath) {
		t.Errorf("token over a held lock: %v", err)
	}
	if u, err := lockDB(db); err == nil {
		u()
		t.Error("a second owner took the lock")
	}
	unlock()

	h = startHost(t, hostOpts{dir: h.dir, expiryEvery: time.Hour})
	defer h.stop()
	if v := h.task("task-a"); len(v.Purchases) != 1 || v.Purchases[0].Decision.PolicyVerdict != "Ask" {
		t.Errorf("positive control: %+v", v)
	}
}

func writeRegistry(t *testing.T, dir, name string, edit func(*contracts.RegistryConfig)) string {
	t.Helper()
	var cfg contracts.RegistryConfig
	if err := decodeFile("testdata/contracts/registry.json", &cfg); err != nil {
		t.Fatal(err)
	}
	edit(&cfg)
	if _, err := contracts.NewRegistry(cfg); err != nil {
		t.Fatalf("%s is not a valid registry: %v", name, err)
	}
	raw, _ := json.Marshal(cfg)
	p := filepath.Join(dir, name)
	os.WriteFile(p, raw, 0o600)
	return p
}

// R2: a stored task answers with the binding it was started with, whatever
// the current registry says; access follows the caller's grant on the
// stored consumer, including after the current registry drops it.
func TestHistoricalTaskKeepsItsBinding(t *testing.T) {
	h := startHost(t, hostOpts{runs: []planRun{{TaskID: "task-a", Scenario: mainline(t)}}, expiryEvery: time.Hour})
	h.waitRuns()
	before, beforeEvents := h.task("task-a"), h.events("task-a")
	if err := h.stop(); err != nil {
		t.Fatal(err)
	}
	const newFrom = "0x5555555555555555555555555555555555555555"
	moved := writeRegistry(t, h.dir, "registry-moved.json", func(c *contracts.RegistryConfig) {
		for i := range c.Wallets {
			if c.Wallets[i].WalletID == "wallet-a" {
				c.Wallets[i].WalletID, c.Wallets[i].From = "wallet-a-new", newFrom
			}
		}
		for i := range c.Consumers {
			if c.Consumers[i].WalletID == "wallet-a" {
				c.Consumers[i].WalletID = "wallet-a-new"
			}
		}
		for i := range c.Tasks {
			if c.Tasks[i].WalletID == "wallet-a" {
				c.Tasks[i].WalletID = "wallet-a-new"
			}
		}
	})
	h = startHost(t, hostOpts{dir: h.dir, expiryEvery: time.Hour, args: []string{"--registry", moved},
		runs: []planRun{{TaskID: "task-b", Scenario: mainline(t)}, {TaskID: "task-a-next", Scenario: mainline(t)}}})
	out := h.waitRuns()
	// The new registry serves new tasks as before; a new task for consumer-a
	// on another wallet is the Store's existing refusal (B1-05), not new.
	if out[0].Outcome != "ok" || !out[0].TaskStarted || out[1].TaskStarted || out[1].Detail != "prepare_failed:WALLET_BINDING_CHANGED" {
		t.Fatalf("runs under the new registry: %+v", out)
	}
	a, ae := h.task("task-a"), h.events("task-a")
	if a.WalletID != "wallet-a" || a.Payer != "0x1111111111111111111111111111111111111111" || a.WalletEvidenceMode != "fixture" ||
		a.ConsumerID != "consumer-a" || !reflect.DeepEqual(a.Purchases, before.Purchases) || a.AsOfSequence != before.AsOfSequence ||
		!reflect.DeepEqual(ae, beforeEvents) {
		t.Errorf("task-a under the moved registry: %+v", a)
	}
	if b := h.task("task-b"); b.WalletID != "wallet-b" || len(b.Purchases) != 1 {
		t.Errorf("task-b: %+v", b)
	}
	_, notFound, _ := h.get("/api/tasks/task-zzz", h.tokens["host"])
	if code, _, _ := h.get("/api/tasks/task-a", h.tokens["viewer:consumer-a"]); code != 200 {
		t.Errorf("viewer A on its own stored task: %d", code)
	}
	for _, p := range []string{"/api/tasks/task-a", "/api/tasks/task-a/events", "/api/tasks/task-a?consumer_id=consumer-b&wallet_id=wallet-b"} {
		if code, body, _ := h.get(p, h.tokens["viewer:consumer-b"], func(r *http.Request) { r.Header.Set("X-Wallet-ID", "wallet-b") }); code != 404 || string(body) != string(notFound) {
			t.Errorf("viewer B %s: %d %s", p, code, body)
		}
	}
	if err := h.stop(); err != nil {
		t.Fatal(err)
	}

	// consumer-a, its wallet and its tasks are gone from the current
	// registry: no viewer token exists for it, the host still reads the
	// stored task, viewer B still cannot.
	dropped := writeRegistry(t, h.dir, "registry-dropped.json", func(c *contracts.RegistryConfig) {
		c.Consumers = slices.DeleteFunc(c.Consumers, func(x contracts.Consumer) bool { return x.ConsumerID == "consumer-a" })
		c.Wallets = slices.DeleteFunc(c.Wallets, func(x contracts.WalletRef) bool { return x.WalletID == "wallet-a" })
		c.Tasks = slices.DeleteFunc(c.Tasks, func(x contracts.Task) bool { return x.ConsumerID == "consumer-a" })
	})
	h = startHost(t, hostOpts{dir: h.dir, expiryEvery: time.Hour, args: []string{"--registry", dropped}})
	defer h.stop()
	if _, ok := h.tokens["viewer:consumer-a"]; ok {
		t.Error("a viewer token exists for a consumer the registry dropped")
	}
	if a := h.task("task-a"); a.WalletID != "wallet-a" || !reflect.DeepEqual(a.Purchases, before.Purchases) {
		t.Errorf("host on the dropped task: %+v", a)
	}
	if code, _, _ := h.get("/api/tasks/task-a", h.tokens["viewer:consumer-b"]); code != 404 {
		t.Errorf("viewer B on the dropped task: %d", code)
	}
}

// The stop order: HTTP, the runs (which record their end), the bridge, the
// expiry worker, then the Store; the cancelled run's end is on record.
func TestStopOrder(t *testing.T) {
	var mu sync.Mutex
	var stages []string
	hang := scenarioFile(t, tool("request_purchase", "product_id", "kr-market"), map[string]any{"do": "hang"})
	h := startHost(t, hostOpts{runs: []planRun{{TaskID: "task-a", Scenario: hang}},
		stage: func(s string) { mu.Lock(); stages = append(stages, s); mu.Unlock() }})
	h.waitFor("task-a", func(v taskReply) bool { return len(v.Purchases) == 1 })
	if err := h.stop(); err != nil {
		t.Fatal(err)
	}
	mu.Lock()
	got := slices.Clone(stages)
	mu.Unlock()
	if want := []string{"http_stopped", "runs_ended", "bridge_closed", "expiry_stopped", "store_closed"}; !slices.Equal(got, want) {
		t.Errorf("stop order %v, want %v", got, want)
	}
	h = startHost(t, hostOpts{dir: h.dir})
	defer h.stop()
	if e := h.events("task-a"); e.Events[len(e.Events)-1].EventType != "TASK_CANCELLED" {
		t.Errorf("the run's end was not recorded before the Store closed: %v", eventTypes(e))
	}
}
