package main

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
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
	os.WriteFile(real, []byte("#!/bin/sh\necho '"+approvedCLIVersion+"'\n"), 0o755)
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
	cli, ran := marker(t, approvedCLIVersion)
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
	// 2.1.288 and 2.1.287 are the earlier pins; 2.1.290 a later version.
	for _, v := range []string{"2.1.288", "2.1.287", "2.1.290"} {
		other, ran2 := marker(t, v+" (Claude Code)")
		_, err := f.exec(context.Background(), []string{"run", "--live", "--cli", other}, map[string]string{"APA_LIVE_CLI": "true"}, deps{})
		if err == nil || !strings.Contains(err.Error(), "other than the approved") || strings.Contains(err.Error(), v) {
			t.Errorf("version %s: %v", v, err) // the CLI's own version text is not repeated
		}
		if _, err := os.Stat(ran2); err != nil {
			t.Errorf("version %s: the version check did not run", v)
		}
	}
	// A cancelled or timed-out version check stops too.
	slow := filepath.Join(t.TempDir(), "claude")
	os.WriteFile(slow, []byte("#!/bin/sh\nsleep 5\necho '"+approvedCLIVersion+"'\n"), 0o755)
	ctx, cancel := context.WithTimeout(context.Background(), 150*time.Millisecond)
	defer cancel()
	start := time.Now()
	_, err := f.exec(ctx, []string{"run", "--live", "--cli", slow}, map[string]string{"APA_LIVE_CLI": "true"}, deps{})
	if err == nil || !strings.Contains(err.Error(), "version check") || time.Since(start) > 2*time.Second {
		t.Errorf("slow version: %v after %v", err, time.Since(start))
	}
	if _, err := os.Stat(f.db); !os.IsNotExist(err) {
		t.Error("a refused live start created the database")
	}

	// The pin itself (Aaron, 2026-10-04): exactly 2.1.289 passes the version
	// check, and the init gate then expects claude_code_version 2.1.289
	// (TestApprovedVersionInit).
	if approvedCLIVersion != "2.1.289 (Claude Code)" || strings.Fields(approvedCLIVersion)[0] != "2.1.289" {
		t.Fatalf("approved version %q", approvedCLIVersion)
	}
	if err := checkLive(context.Background(), cli, buyer.CLIEnv(os.Getenv), approvedCLIVersion); err != nil {
		t.Errorf("the approved version was refused: %v", err)
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

// probeExec runs `apa-buyer probe-init` with the fixture's scratch HOME.
func (f *fixture) probeExec(ctx context.Context, args []string, env map[string]string, d deps) (string, error) {
	base := map[string]string{"PATH": os.Getenv("PATH"), "HOME": f.home, "APA_DB_PATH": f.db}
	for k, v := range env {
		base[k] = v
	}
	var out bytes.Buffer
	lim := buyer.Limits{Line: 64 << 10, Stdout: 1 << 20, Stderr: 64 << 10, Total: 3 * time.Second, Grace: 500 * time.Millisecond}
	d.getenv, d.self, d.out, d.clock, d.limits = func(k string) string { return base[k] }, f.self, &out, func() uint64 { return f.now }, &lim
	err := run(ctx, append([]string{"probe-init", "--trace-dir", f.traceDir}, args...), d)
	return out.String(), err
}

// C0: the same opt-in as live; nothing runs when any part is missing.
func TestProbeOptIn(t *testing.T) {
	f := newFixture(t)
	cli, ran := marker(t, approvedCLIVersion)
	for name, c := range map[string]struct {
		args []string
		env  map[string]string
		want string
	}{
		"env unset":    {[]string{"--cli", cli}, nil, "needs APA_LIVE_CLI=true"},
		"env false":    {[]string{"--cli", cli}, map[string]string{"APA_LIVE_CLI": "0"}, "needs APA_LIVE_CLI=true"},
		"relative cli": {[]string{"--cli", "bin/claude"}, map[string]string{"APA_LIVE_CLI": "true"}, "absolute path"},
		"no cli":       {nil, map[string]string{"APA_LIVE_CLI": "true"}, "absolute path"},
		"stray arg":    {[]string{"--cli", cli, "extra"}, map[string]string{"APA_LIVE_CLI": "true"}, "unexpected argument"},
	} {
		if _, err := f.probeExec(context.Background(), c.args, c.env, deps{}); err == nil || !strings.Contains(err.Error(), c.want) {
			t.Errorf("%s: %v", name, err)
		}
	}
	if _, err := os.Stat(ran); err == nil {
		t.Fatal("a refused probe executed the CLI")
	}
	// Old version, and a version check that is cancelled: no probe.
	for _, v := range []string{"2.1.288", "2.1.287"} {
		old, _ := marker(t, v+" (Claude Code)")
		if _, err := f.probeExec(context.Background(), []string{"--cli", old}, map[string]string{"APA_LIVE_CLI": "true"}, deps{}); err == nil ||
			!strings.Contains(err.Error(), "other than the approved") {
			t.Errorf("old version %s: %v", v, err)
		}
	}
	slow := filepath.Join(t.TempDir(), "claude")
	os.WriteFile(slow, []byte("#!/bin/sh\nsleep 5\necho '"+approvedCLIVersion+"'\n"), 0o755)
	ctx, cancel := context.WithTimeout(context.Background(), 150*time.Millisecond)
	defer cancel()
	if _, err := f.probeExec(ctx, []string{"--cli", slow}, map[string]string{"APA_LIVE_CLI": "true"}, deps{}); err == nil || !strings.Contains(err.Error(), "version check") {
		t.Errorf("cancelled version check: %v", err)
	}
	// Cancelled before anything: not even the version check runs.
	done, stop := context.WithCancel(context.Background())
	stop()
	if _, err := f.probeExec(done, []string{"--cli", cli}, map[string]string{"APA_LIVE_CLI": "true"}, deps{}); err == nil || !strings.Contains(err.Error(), "cancelled before the version check") {
		t.Errorf("cancelled probe: %v", err)
	}
	if _, err := os.Stat(ran); err == nil {
		t.Fatal("a cancelled probe executed the CLI")
	}
	if _, err := os.Stat(f.traceDir); !os.IsNotExist(err) {
		t.Error("a refused probe wrote a trace")
	}
}

// C0 through the command with the fake: the right exit codes, the Store
// never opened (the hook is never called, and an existing database with a
// due Ask is unchanged byte for byte), and the terminal output free of the
// run's capability and planted marks.
func TestProbeCommand(t *testing.T) {
	f := newFixture(t)
	if out, err := f.run(context.Background(), f.fake, "ask-mainline", nil); err != nil { // a database with an Ask
		t.Fatalf("%v\n%s", err, out)
	}
	before := dirState(t, filepath.Dir(f.db))
	f.now = t0 + 100 // past the Ask's deadline: opening the Store would expire it
	opened := 0
	d := deps{liveVersion: fakecli.Version, openStore: func(c store.Config) (*store.Store, error) { opened++; return store.Open(c) }}
	env := map[string]string{"APA_LIVE_CLI": "true"}

	cli := liveFake(t, f, "probe-init-first")
	out, err := f.probeExec(context.Background(), []string{"--cli", cli}, env, d)
	if err != nil || !strings.HasPrefix(out, "PROBE MODE (C0):") || !strings.Contains(out, `"status": "observed"`) {
		t.Fatalf("observed: %v\n%s", err, out)
	}
	cli = liveFake(t, f, "probe-wait-input")
	out, err = f.probeExec(context.Background(), []string{"--cli", cli}, env, d)
	var ec exitCode
	if !errors.As(err, &ec) || ec != 3 || !strings.Contains(out, `"status": "no_init_before_input"`) {
		t.Fatalf("no init: %v\n%s", err, out)
	}
	if opened != 0 {
		t.Fatalf("the probe opened the Store %d times", opened)
	}
	if after := dirState(t, filepath.Dir(f.db)); !maps.Equal(before, after) {
		t.Fatal("the probe changed the database")
	}
	// Positive control: the same hook sees an ordinary run open the Store.
	if _, err := f.exec(context.Background(), []string{"run", "--fake-cli", f.fake, "--scenario", "../../testdata/cli/ask-mainline.json", "--task", "task-a-next"}, nil, deps{openStore: d.openStore}); err != nil || opened != 1 {
		t.Fatalf("control run: %v, opened %d", err, opened)
	}

	// Leaks: capability and marks in the init's strings stay out of the
	// terminal, the error and the trace.
	const mark = "MARKCMD4b2f"
	capFile := f.fake + ".cap" // the live environment has no room for a test variable
	sc := `{"manual_init":true,"steps":[{"do":"savecap"},{"do":"init","init":{"model":"$CAPABILITY","apiKeySource":"` + mark +
		`","claude_code_version":"$CAPABILITY","agents":[{"name":"` + mark + `"}]}},{"do":"hang"}]}`
	os.WriteFile(f.fake+".scenario.json", []byte(sc), 0o600)
	out, err = f.probeExec(context.Background(), []string{"--cli", f.fake}, env, d)
	capability, _ := os.ReadFile(capFile)
	if len(capability) != 64 || !errors.As(err, &ec) || ec != 1 || !strings.Contains(err.Error(), "init_cli_version") {
		t.Fatalf("leak setup: cap %d bytes, %v\n%s", len(capability), err, out)
	}
	traces, _ := os.ReadDir(f.traceDir)
	all := out + err.Error()
	for _, e := range traces {
		raw, _ := os.ReadFile(filepath.Join(f.traceDir, e.Name()))
		all += string(raw)
	}
	if strings.Contains(all, mark) || strings.Contains(all, string(capability)) {
		t.Fatalf("leak: %s", all)
	}
	if liveAcceptAuth != nil {
		t.Error("the live auth list is not empty")
	}
}

// C0′ through the command: the flag is the only way in; the same opt-in,
// path and version checks apply; exit 0 on an init after the message, 4
// when none came; the Store is never touched and C0 stays input-free.
func TestProbeOneMessageCommand(t *testing.T) {
	f := newFixture(t)
	cli, ran := marker(t, approvedCLIVersion)
	if _, err := f.probeExec(context.Background(), []string{"--cli", cli, "--send-one-message"}, nil, deps{}); err == nil || !strings.Contains(err.Error(), "needs APA_LIVE_CLI=true") {
		t.Fatalf("no opt-in: %v", err)
	}
	if _, err := f.probeExec(context.Background(), []string{"--cli", "claude", "--send-one-message"}, map[string]string{"APA_LIVE_CLI": "true"}, deps{}); err == nil {
		t.Fatal("relative path accepted")
	}
	if _, err := os.Stat(ran); err == nil {
		t.Fatal("a refused C0′ ran the CLI")
	}
	opened := 0
	d := deps{liveVersion: fakecli.Version, openStore: func(c store.Config) (*store.Store, error) { opened++; return store.Open(c) }}
	env := map[string]string{"APA_LIVE_CLI": "true"}

	out, err := f.probeExec(context.Background(), []string{"--cli", liveFake(t, f, "probe1-wait-message"), "--send-one-message"}, env, d)
	if err != nil || !strings.HasPrefix(out, "PROBE MODE (C0′):") || !strings.Contains(out, `"mode": "c0_prime"`) ||
		!strings.Contains(out, `"input_status": "complete"`) || !strings.Contains(out, `"status": "observed"`) || strings.Contains(out, "Reply only") {
		t.Fatalf("observed: %v\n%s", err, out)
	}
	out, err = f.probeExec(context.Background(), []string{"--cli", liveFake(t, f, "probe1-message-no-init"), "--send-one-message"}, env, d)
	var ec exitCode
	if !errors.As(err, &ec) || ec != 4 || !strings.Contains(out, `"status": "no_init_after_message"`) {
		t.Fatalf("no init: %v\n%s", err, out)
	}
	// Without the flag the same fake gets no input and no init: C0's exit 3.
	out, err = f.probeExec(context.Background(), []string{"--cli", liveFake(t, f, "probe1-message-no-init")}, env, d)
	if !errors.As(err, &ec) || ec != 3 || !strings.Contains(out, `"mode": "c0"`) || !strings.Contains(out, `"stdin_bytes_written": 0`) {
		t.Fatalf("C0: %v\n%s", err, out)
	}
	if opened != 0 {
		t.Fatalf("the probe opened the Store %d times", opened)
	}
	if _, err := os.Stat(f.db); !os.IsNotExist(err) {
		t.Error("the probe created the database")
	}
}

// The pin end to end, with no version override (deps{}): a CLI whose
// --version is exactly the pin passes, and its init must then state the
// pin's version number. Earlier pins and the fake's own number stop at
// init_cli_version, the version shown only as "other". The CLI is a script that
// answers --version itself and hands everything else to the fake.
func TestApprovedVersionInit(t *testing.T) {
	for v, want := range map[string]string{"2.1.289": "", "2.1.288": "init_cli_version", "2.1.287": "init_cli_version", fakecli.InitVersion: "init_cli_version"} {
		f := newFixture(t)
		os.WriteFile(f.fake+".scenario.json", []byte(`{"manual_init":true,"steps":[{"do":"init","init":{"claude_code_version":"`+v+`"}},{"do":"hang"}]}`), 0o600)
		cli := filepath.Join(t.TempDir(), "claude")
		os.WriteFile(cli, []byte("#!/bin/sh\nif [ \"$*\" = --version ]; then echo '"+approvedCLIVersion+"'; exit 0; fi\nexec "+f.fake+" \"$@\"\n"), 0o755)
		out, err := f.probeExec(context.Background(), []string{"--cli", cli}, map[string]string{"APA_LIVE_CLI": "true"}, deps{})
		if want == "" {
			if err != nil || !strings.Contains(out, `"status": "observed"`) || !strings.Contains(out, `"cli_version": "approved"`) {
				t.Errorf("init %s: %v\n%s", v, err, out)
			}
			continue
		}
		var ec exitCode
		if !errors.As(err, &ec) || ec != 1 || !strings.Contains(out, `"code": "`+want+`"`) || !strings.Contains(out, `"cli_version": "other"`) {
			t.Errorf("init %s: %v\n%s", v, err, out)
		}
	}
}
