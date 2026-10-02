package main

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"maps"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/SIMPLYBOYS/agent-payment-authority/internal/buyer"
	"github.com/SIMPLYBOYS/agent-payment-authority/internal/buyer/fakecli"
	"github.com/SIMPLYBOYS/agent-payment-authority/internal/contracts"
	"github.com/SIMPLYBOYS/agent-payment-authority/internal/store"
	"github.com/SIMPLYBOYS/agent-payment-authority/internal/toolbridge"
)

// The test binary is also the fake CLI (symlink fake-claude) and the bridge.
func TestMain(m *testing.M) {
	switch {
	case filepath.Base(os.Args[0]) == "fake-claude":
		os.Exit(fakecli.Main(os.Args[1:], os.Getenv, os.Stdout, os.Stderr))
	case len(os.Args) > 1 && os.Args[1] == "mcp-bridge":
		if err := toolbridge.RunBridge(context.Background(), os.Stdin, os.Stdout, os.Getenv); err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(1)
		}
		os.Exit(0)
	}
	os.Exit(m.Run())
}

const t0 = 1790726450

type fixture struct {
	t        *testing.T
	self     string
	fake     string
	db       string
	traceDir string
	home     string // scratch HOME: tests never see the user's login
	now      uint64
}

func newFixture(t *testing.T) *fixture {
	self, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	f := &fixture{t: t, self: self, fake: filepath.Join(dir, "fake-claude"), db: filepath.Join(dir, "db", "apa.db"), traceDir: filepath.Join(dir, "runs"),
		home: filepath.Join(dir, "home"), now: t0}
	if err := os.Symlink(self, f.fake); err != nil {
		t.Fatal(err)
	}
	return f
}

func (f *fixture) run(ctx context.Context, cli, scen string, onStore func(*store.Store), extra ...string) (string, error) {
	return f.exec(ctx, append([]string{"run", "--fake-cli", cli, "--scenario", "../../testdata/cli/" + scen + ".json"}, extra...),
		nil, deps{onStore: onStore})
}

// exec runs the command with the fixture's paths, a scratch HOME and env
// added to the base environment; d's test hooks are kept.
func (f *fixture) exec(ctx context.Context, args []string, env map[string]string, d deps) (string, error) {
	base := map[string]string{"APA_DB_PATH": f.db, "PATH": os.Getenv("PATH"), "HOME": f.home}
	for k, v := range env {
		base[k] = v
	}
	var out bytes.Buffer
	args = append(args, "--registry", "../../testdata/contracts/registry.json", "--profile", "../../testdata/policy/m1-demo-profile.json",
		"--trace-dir", f.traceDir)
	lim := buyer.Limits{Line: 64 << 10, Stdout: 1 << 20, Stderr: 64 << 10, Total: 20 * time.Second, Grace: 500 * time.Millisecond, Assistant: 10}
	now := f.now
	d.getenv, d.self, d.out, d.clock, d.limits = func(k string) string { return base[k] }, f.self, &out, func() uint64 { return now }, &lim
	err := run(ctx, args, d)
	return out.String(), err
}

// stored reopens the database the command closed.
func (f *fixture) stored(sess contracts.Session) store.TaskView {
	f.t.Helper()
	var reg contracts.RegistryConfig
	raw, _ := os.ReadFile("../../testdata/contracts/registry.json")
	contracts.Decode(raw, &reg)
	st, err := store.Open(store.Config{Path: f.db, Registry: reg, Clock: func() uint64 { return t0 }})
	if err != nil {
		f.t.Fatal(err)
	}
	defer st.Close()
	v, err := st.Task(sess)
	if err != nil {
		f.t.Fatal(err)
	}
	return v
}

var sessA = contracts.Session{ConsumerID: "consumer-a", AgentID: "agent-a", TaskID: "task-a"}

func TestFakeMainLine(t *testing.T) {
	f := newFixture(t)
	out, err := f.run(context.Background(), f.fake, "ask-mainline", nil)
	if err != nil {
		t.Fatalf("%v\n%s", err, out)
	}
	if !strings.HasPrefix(out, "FAKE MODE:") {
		t.Errorf("not labelled fake: %s", out)
	}
	var report struct {
		Outcome string         `json:"outcome"`
		Task    store.TaskView `json:"task"`
		Trace   string         `json:"trace"`
		Payment string         `json:"payment"`
	}
	if err := json.Unmarshal([]byte(out[strings.Index(out, "{"):]), &report); err != nil {
		t.Fatal(err)
	}
	if report.Outcome != "ok" || report.Task.Status != "awaiting_approval" || len(report.Task.Purchases) != 1 ||
		report.Task.Purchases[0].Decision.PolicyVerdict != "Ask" || !strings.HasPrefix(report.Payment, "none") {
		t.Fatalf("report: %+v", report)
	}
	if fi, err := os.Stat(report.Trace); err != nil || fi.Mode().Perm() != 0o600 {
		t.Errorf("trace: %v %v", fi, err)
	}
	// The record is on disk, in the Store's own format, after the command closed it.
	if v := f.stored(sessA); len(v.Purchases) != 1 || v.Purchases[0].PurchaseOrderID != "po-task-a-kr-market" {
		t.Fatalf("stored: %+v", v)
	}

	// Rerunning the same task is reported, and nothing is deleted.
	_, err = f.run(context.Background(), f.fake, "ask-mainline", nil)
	if err == nil || !strings.Contains(err.Error(), "already exists") || !strings.Contains(err.Error(), "nothing was changed") {
		t.Fatalf("rerun: %v", err)
	}
	if v := f.stored(sessA); len(v.Purchases) != 1 {
		t.Fatalf("rerun changed the record: %+v", v)
	}
	// The next fixture task of the same consumer runs in the same database.
	if out, err := f.run(context.Background(), f.fake, "ask-mainline", nil, "--task", "task-a-next"); err != nil {
		t.Fatalf("%v\n%s", err, out)
	}
}

func TestRefusals(t *testing.T) {
	f := newFixture(t)
	real := filepath.Join(t.TempDir(), "claude")
	os.WriteFile(real, []byte("#!/bin/sh\necho '2.1.287 (Claude Code)'\n"), 0o755)
	for name, c := range map[string]struct {
		args []string
		want string
	}{
		"no mode":     {[]string{"run", "--scenario", "x"}, "pass --fake-cli for a fake run"},
		"real cli":    {[]string{"run", "--fake-cli", real, "--scenario", "x"}, "is not the fake CLI"},
		"no scenario": {[]string{"run", "--fake-cli", f.fake}, "--scenario is required"},
		"no command":  {nil, "usage"},
		"stray arg":   {[]string{"run", "--fake-cli", f.fake, "extra"}, "unexpected argument"},
		"bad task":    {[]string{"run", "--fake-cli", f.fake, "--scenario", "../../testdata/cli/ask-mainline.json", "--task", "task-z", "--registry", "../../testdata/contracts/registry.json", "--profile", "../../testdata/policy/m1-demo-profile.json"}, "not in the trusted registry"},
	} {
		err := run(context.Background(), c.args, deps{getenv: func(k string) string { return map[string]string{"APA_DB_PATH": f.db, "PATH": os.Getenv("PATH")}[k] },
			self: f.self, out: &bytes.Buffer{}, clock: func() uint64 { return t0 }})
		if err == nil || !strings.Contains(err.Error(), c.want) {
			t.Errorf("%s: %v", name, err)
		}
	}
	if _, err := os.Stat(f.db); !os.IsNotExist(err) {
		t.Error("a refused start created the database")
	}
}

func TestShutdownMidRun(t *testing.T) {
	f := newFixture(t)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	ready := func(st *store.Store) {
		go func() {
			for end := time.Now().Add(10 * time.Second); time.Now().Before(end); time.Sleep(20 * time.Millisecond) {
				if v, err := st.Task(sessA); err == nil && len(v.Purchases) == 1 {
					break
				}
			}
			cancel()
		}()
	}
	start := time.Now()
	out, err := f.run(ctx, f.fake, "hang", ready)
	if err == nil || !strings.Contains(err.Error(), "cancelled") || time.Since(start) > 15*time.Second {
		t.Fatalf("%v after %v\n%s", err, time.Since(start), out)
	}
	// The Store was closed cleanly and holds the cancellation.
	v := f.stored(sessA)
	if v.Status != "cancelled" || len(v.Purchases) != 1 || v.Purchases[0].ApprovalClosed != "withdrawn" {
		t.Fatalf("after shutdown: %+v", v)
	}
}

// R2: a version check cut short by cancellation starts nothing.
func TestVersionCheckCancelled(t *testing.T) {
	f := newFixture(t)
	slow := filepath.Join(t.TempDir(), "fake-claude")
	os.WriteFile(slow, []byte("#!/bin/sh\nsleep 5\necho fake-claude\n"), 0o755)
	ctx, cancel := context.WithTimeout(context.Background(), 150*time.Millisecond)
	defer cancel()
	start := time.Now()
	_, err := f.run(ctx, slow, "ask-mainline", nil)
	if err == nil || !strings.Contains(err.Error(), "is not the fake CLI") || time.Since(start) > 2*time.Second {
		t.Fatalf("%v after %v", err, time.Since(start))
	}
	if _, err := os.Stat(f.db); !os.IsNotExist(err) {
		t.Error("the database was created")
	}
}

// dirState is every file under dir with its content hash: a run that must
// not touch the Store leaves it equal.
func dirState(t *testing.T, dir string) map[string]string {
	t.Helper()
	out := map[string]string{}
	entries, _ := os.ReadDir(dir)
	for _, e := range entries {
		raw, err := os.ReadFile(filepath.Join(dir, e.Name()))
		if err != nil {
			t.Fatal(err)
		}
		out[e.Name()] = fmt.Sprintf("%x", sha256.Sum256(raw))
	}
	return out
}

// marker is a CLI that leaves a file behind whenever it is executed.
func marker(t *testing.T, version string) (path, ran string) {
	dir := t.TempDir()
	path, ran = filepath.Join(dir, "claude"), filepath.Join(dir, "ran")
	os.WriteFile(path, []byte("#!/bin/sh\ntouch "+ran+"\necho '"+version+"'\n"), 0o755)
	return path, ran
}

// Stage A: live needs --live and APA_LIVE_CLI=true together, an absolute
// --cli and the approved version; anything else starts no CLI and opens no
// Store. Only scripts and the fake are ever executed here.
func TestLiveOptIn(t *testing.T) {
	f := newFixture(t)
	cli, ran := marker(t, "2.1.287 (Claude Code)")
	for name, c := range map[string]struct {
		args []string
		env  map[string]string
		want string
	}{
		"env unset":        {[]string{"run", "--live", "--cli", cli}, nil, "needs APA_LIVE_CLI=true"},
		"env false":        {[]string{"run", "--live", "--cli", cli}, map[string]string{"APA_LIVE_CLI": "false"}, "needs APA_LIVE_CLI=true"},
		"env not a bool":   {[]string{"run", "--live", "--cli", cli}, map[string]string{"APA_LIVE_CLI": "yes"}, "needs APA_LIVE_CLI=true"},
		"env without flag": {[]string{"run", "--cli", cli}, map[string]string{"APA_LIVE_CLI": "true"}, "--cli is for live runs only"},
		"relative cli":     {[]string{"run", "--live", "--cli", "bin/claude"}, map[string]string{"APA_LIVE_CLI": "true"}, "absolute path"},
		"bare name":        {[]string{"run", "--live", "--cli", "claude"}, map[string]string{"APA_LIVE_CLI": "true"}, "absolute path"},
		"no cli":           {[]string{"run", "--live"}, map[string]string{"APA_LIVE_CLI": "true"}, "absolute path"},
		"with fake":        {[]string{"run", "--live", "--fake-cli", f.fake}, map[string]string{"APA_LIVE_CLI": "true"}, "cannot be combined"},
	} {
		_, err := f.exec(context.Background(), c.args, c.env, deps{})
		if err == nil || !strings.Contains(err.Error(), c.want) {
			t.Errorf("%s: %v", name, err)
		}
	}
	if _, err := os.Stat(ran); err == nil {
		t.Fatal("a refused opt-in executed the CLI")
	}

	// The approved version is required exactly; the check itself runs the
	// CLI with --version only.
	other, ran2 := marker(t, "2.1.288 (Claude Code)")
	_, err := f.exec(context.Background(), []string{"run", "--live", "--cli", other}, map[string]string{"APA_LIVE_CLI": "true"}, deps{})
	if err == nil || !strings.Contains(err.Error(), "not the approved") {
		t.Errorf("other version: %v", err)
	}
	if _, err := os.Stat(ran2); err != nil {
		t.Error("the version check did not run")
	}
	// A cancelled or timed-out version check stops too.
	slow := filepath.Join(t.TempDir(), "claude")
	os.WriteFile(slow, []byte("#!/bin/sh\nsleep 5\necho '2.1.287 (Claude Code)'\n"), 0o755)
	ctx, cancel := context.WithTimeout(context.Background(), 150*time.Millisecond)
	defer cancel()
	start := time.Now()
	_, err = f.exec(ctx, []string{"run", "--live", "--cli", slow}, map[string]string{"APA_LIVE_CLI": "true"}, deps{})
	if err == nil || !strings.Contains(err.Error(), "version check") || time.Since(start) > 2*time.Second {
		t.Errorf("slow version: %v after %v", err, time.Since(start))
	}
	if _, err := os.Stat(f.db); !os.IsNotExist(err) {
		t.Error("a refused live start created the database")
	}
}

// liveFake prepares the fake as a live CLI: the live environment carries no
// scenario variable, so the fake reads <path>.scenario.json.
func liveFake(t *testing.T, f *fixture, scen string) string {
	raw, err := os.ReadFile("../../testdata/cli/" + scen + ".json")
	if err != nil {
		t.Fatal(err)
	}
	os.WriteFile(f.fake+".scenario.json", raw, 0o600)
	return f.fake
}

// Stage A: the live path, run with the fake only: the same gate, relay,
// HTTP 402 and Store as a live run, ending in an Ask.
func TestLivePathWithFake(t *testing.T) {
	f := newFixture(t)
	cli := liveFake(t, f, "ask-mainline")
	out, err := f.exec(context.Background(), []string{"run", "--live", "--cli", cli}, map[string]string{"APA_LIVE_CLI": "true"},
		deps{liveVersion: fakecli.Version, liveAuth: []string{fakecli.AuthSource}})
	if err != nil {
		t.Fatalf("%v\n%s", err, out)
	}
	if !strings.HasPrefix(out, "LIVE MODE:") || !strings.Contains(out, `"mode": "live"`) {
		t.Errorf("not labelled live: %s", out)
	}
	if v := f.stored(sessA); len(v.Purchases) != 1 || v.Purchases[0].Decision.PolicyVerdict != "Ask" || v.Purchases[0].Decision.PaymentStatus != "unavailable" {
		t.Fatalf("stored: %+v", v)
	}
	if defaultDB(true) != "var/apa-live.db" || defaultDB(false) != "var/apa-fake.db" {
		t.Error("default databases are not apart")
	}

	// With the production auth list (empty), no live init passes, and the
	// Store is never opened.
	f2 := newFixture(t)
	cli = liveFake(t, f2, "ask-mainline")
	out, err = f2.exec(context.Background(), []string{"run", "--live", "--cli", cli}, map[string]string{"APA_LIVE_CLI": "true"},
		deps{liveVersion: fakecli.Version})
	if err == nil || !strings.Contains(err.Error(), "config_mismatch auth_unconfirmed") {
		t.Fatalf("%v\n%s", err, out)
	}
	if _, err := os.Stat(filepath.Dir(f2.db)); !os.IsNotExist(err) {
		t.Error("the Store was opened before the init passed")
	}
}

// Stage A, A1: a refused init opens no Store: a new database is not
// created, and an existing one with a due Ask is left byte for byte.
func TestNoStoreBeforeInit(t *testing.T) {
	f := newFixture(t)
	if _, err := f.run(context.Background(), f.fake, "init-bad-tools", nil); err == nil || !strings.Contains(err.Error(), "config_mismatch tools") {
		t.Fatalf("bad init: %v", err)
	}
	if _, err := os.Stat(filepath.Dir(f.db)); !os.IsNotExist(err) {
		t.Fatal("the database directory was created")
	}

	if out, err := f.run(context.Background(), f.fake, "ask-mainline", nil); err != nil {
		t.Fatalf("%v\n%s", err, out)
	}
	before := dirState(t, filepath.Dir(f.db))
	f.now = t0 + 100 // the Ask's deadline has passed: opening the Store would expire it
	for _, scen := range []string{"init-bad-tools", "init-missing", "hook-before-init"} {
		if _, err := f.run(context.Background(), f.fake, scen, nil, "--task", "task-a-next"); err == nil {
			t.Errorf("%s passed", scen)
		}
		if after := dirState(t, filepath.Dir(f.db)); !maps.Equal(before, after) {
			t.Fatalf("%s changed the database: %v -> %v", scen, before, after)
		}
	}
	if v := f.stored(sessA); v.Purchases[0].ApprovalClosed != "" {
		// stored() opens the Store at t0, so the Ask is still open here.
		t.Errorf("the Ask was closed: %+v", v)
	}
}

// Stage A, A1: init passed but the Store could not be prepared: no tool
// runs and nothing is written.
func TestStoreUnavailableAfterInit(t *testing.T) {
	f := newFixture(t)
	if err := os.MkdirAll(filepath.Dir(f.db), 0o755); err != nil { // not private: the Store refuses it
		t.Fatal(err)
	}
	out, err := f.run(context.Background(), f.fake, "ask-mainline", nil)
	if err == nil || !strings.Contains(err.Error(), "the run stopped before any tool ran") {
		t.Fatalf("%v\n%s", err, out)
	}
	if !strings.Contains(out, `"tool_calls": null`) || !strings.Contains(out, `"task_started": false`) {
		t.Errorf("report: %s", out)
	}
	if _, err := os.Stat(f.db); !os.IsNotExist(err) {
		t.Error("the database was created")
	}
}
