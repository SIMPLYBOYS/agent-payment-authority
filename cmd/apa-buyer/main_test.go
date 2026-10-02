package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
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
}

func newFixture(t *testing.T) *fixture {
	self, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	f := &fixture{t: t, self: self, fake: filepath.Join(dir, "fake-claude"), db: filepath.Join(dir, "db", "apa.db"), traceDir: filepath.Join(dir, "runs")}
	if err := os.Symlink(self, f.fake); err != nil {
		t.Fatal(err)
	}
	return f
}

func (f *fixture) run(ctx context.Context, cli, scen string, ready func(*buyer.Host), extra ...string) (string, error) {
	env := map[string]string{"APA_DB_PATH": f.db, "PATH": os.Getenv("PATH"), "HOME": os.Getenv("HOME")}
	var out bytes.Buffer
	args := append([]string{"run", "--fake-cli", cli, "--scenario", "../../testdata/cli/" + scen + ".json",
		"--registry", "../../testdata/contracts/registry.json", "--profile", "../../testdata/policy/m1-demo-profile.json",
		"--trace-dir", f.traceDir}, extra...)
	lim := buyer.Limits{Line: 64 << 10, Stdout: 1 << 20, Stderr: 64 << 10, Total: 20 * time.Second, Grace: 500 * time.Millisecond}
	err := run(ctx, args, deps{getenv: func(k string) string { return env[k] }, self: f.self, out: &out,
		clock: func() uint64 { return t0 }, limits: &lim, ready: ready})
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
		"live":        {[]string{"run", "--scenario", "x"}, "live Claude Code runs are not enabled"},
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
	ready := func(h *buyer.Host) {
		go func() {
			for end := time.Now().Add(10 * time.Second); time.Now().Before(end); time.Sleep(20 * time.Millisecond) {
				if v, err := h.Store.Task(sessA); err == nil && len(v.Purchases) == 1 {
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
