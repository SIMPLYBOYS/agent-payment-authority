package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"syscall"
	"testing"
	"time"

	"github.com/SIMPLYBOYS/agent-payment-authority/internal/contracts"
)

// These tests run the producer with a stand-in for the Claude Code CLI: a
// script that answers --version with the approved version and otherwise
// runs the fake CLI (no model, no network) on a test scenario. The
// producer has no fake switch; the stand-in is only what --cli names. HOME
// is a new directory with an empty .apa-claude; the repository is a local
// clone; nothing of the user's login is read.

const marker = "SYNTHETIC-SENSITIVE-MARKER-7f3a" // must never reach a bundle

var (
	fakeOnce sync.Once
	fakeBin  string
	fakeErr  error
)

func fakeCLI(t *testing.T) string {
	fakeOnce.Do(func() {
		dir, err := os.MkdirTemp("", "m1-live-fake-")
		if err != nil {
			fakeErr = err
			return
		}
		fakeBin = filepath.Join(dir, "fake-claude")
		out, err := exec.Command("go", "build", "-o", fakeBin, "../fake-claude").CombinedOutput()
		if err != nil {
			fakeErr = err
			t.Log(string(out))
		}
	})
	if fakeErr != nil {
		t.Fatal(fakeErr)
	}
	return fakeBin
}

type env struct {
	root, repo, home, cli, count, out, work string
}

// liveInit is an init the dev demo gate passes: the approved version, auth
// none, exactly the two named plugins.
var liveInit = map[string]any{"apiKeySource": "none", "claude_code_version": "2.1.293",
	"plugins": []any{map[string]any{"name": "cc-plugin-agents-md", "path": "p"}, map[string]any{"name": "cc-plugin-plugin-authoring", "path": "p"}}}

func mainLine() []map[string]any {
	return []map[string]any{
		{"do": "init", "init": liveInit},
		{"do": "tool", "tool": "list_products"},
		{"do": "tool", "tool": "get_quote", "args": map[string]any{"product_id": "kr-market"}},
		{"do": "tool", "tool": "request_purchase", "args": map[string]any{"product_id": "kr-market"}},
		{"do": "text", "text": marker + " payment complete"},
		{"do": "stderr", "text": marker + " on stderr\n"},
		{"do": "result", "text": marker + " done"},
	}
}

// setup makes a clean local clone of this repository, a HOME with
// .apa-claude and the stand-in CLI, which records each start in count.
func setup(t *testing.T, version string, steps []map[string]any) env {
	t.Helper()
	root := t.TempDir()
	e := env{root: root, repo: filepath.Join(root, "repo"), home: filepath.Join(root, "home"), cli: filepath.Join(root, "claude"),
		count: filepath.Join(root, "count"), out: filepath.Join(root, "out"), work: filepath.Join(root, "work")}
	if out, err := exec.Command("git", "clone", "--quiet", "--no-hardlinks", "../..", e.repo).CombinedOutput(); err != nil {
		t.Fatalf("clone: %v %s", err, out)
	}
	os.MkdirAll(filepath.Join(e.home, ".apa-claude"), 0o700)
	sc, _ := json.Marshal(map[string]any{"description": "M1-E3 test", "manual_init": true, "steps": steps})
	scen := filepath.Join(root, "scenario.json")
	os.WriteFile(scen, sc, 0o600)
	script := "#!/bin/sh\n" +
		"if [ \"$1\" = \"--version\" ]; then echo version >> " + e.count + "; echo \"" + version + "\"; exit 0; fi\n" +
		"echo run >> " + e.count + "\n" +
		"echo $$ > " + e.count + ".pid\n" +
		"APA_FAKE_CLI_SCENARIO=" + scen + " exec " + fakeCLI(t) + " \"$@\"\n"
	os.WriteFile(e.cli, []byte(script), 0o700)
	return e
}

func (e env) env(extra ...string) []string {
	out := []string{"PATH=" + os.Getenv("PATH"), "HOME=" + e.home}
	for _, k := range []string{"GOCACHE", "GOMODCACHE", "GOPATH", "TMPDIR"} {
		if v := os.Getenv(k); v != "" {
			out = append(out, k+"="+v)
		}
	}
	return append(out, extra...)
}

func (e env) run(t *testing.T, environ []string, args ...string) (int, string, string) {
	t.Helper()
	var so, se bytes.Buffer
	base := []string{"--repo", e.repo, "--out", e.out, "--work", e.work}
	code := run(context.Background(), append(base, args...), environ, &so, &se)
	return code, so.String(), se.String()
}

func (e env) starts() []string {
	b, _ := os.ReadFile(e.count)
	return strings.Fields(string(b))
}

// bundleDir is the one bundle made, or "".
func (e env) bundleDir(t *testing.T) string {
	entries, _ := os.ReadDir(e.out)
	switch len(entries) {
	case 0:
		return ""
	case 1:
		return filepath.Join(e.out, entries[0].Name())
	}
	t.Fatalf("%d bundles", len(entries))
	return ""
}

func readJSON(t *testing.T, path string, dst any) {
	t.Helper()
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(b, dst); err != nil {
		t.Fatal(err)
	}
}

type runRec struct {
	Version       string         `json:"version"`
	Termination   string         `json:"termination"`
	ProducerError string         `json:"producer_error"`
	ExitCode      int            `json:"apa_buyer_exit_code"`
	GateMode      string         `json:"gate_mode"`
	GateResult    string         `json:"gate_result"`
	Usage         any            `json:"usage"`
	Snapshot      map[string]any `json:"snapshot"`
}

func runRecord(t *testing.T, dir string) runRec {
	var r runRec
	readJSON(t, filepath.Join(dir, "live", "run.json"), &r)
	return r
}

func manifest(t *testing.T, dir string) contracts.RunManifest {
	var m contracts.RunManifest
	readJSON(t, filepath.Join(dir, "manifest.json"), &m)
	return m
}

func endState(t *testing.T, dir string) string {
	var end map[string]string
	if _, err := os.Stat(filepath.Join(dir, "run-end.json")); err != nil {
		return ""
	}
	readJSON(t, filepath.Join(dir, "run-end.json"), &end)
	return end["state"]
}

func noMarker(t *testing.T, dir, stdout, stderr string) {
	t.Helper()
	filepath.WalkDir(dir, func(p string, d fs.DirEntry, err error) error {
		if err == nil && d.Type().IsRegular() {
			if b, _ := os.ReadFile(p); bytes.Contains(b, []byte(marker)) {
				t.Errorf("the synthetic sensitive marker is in %s", p)
			}
		}
		return nil
	})
	if strings.Contains(stdout+stderr, marker) {
		t.Error("the synthetic sensitive marker is in the producer's output")
	}
}

const approved = "2.1.293 (Claude Code)"

func live(e env) []string { return e.env("APA_LIVE_CLI=true") }

// A live-shaped Ask: one CLI start for the version, one for the run; the
// bundle claims ask only, everything else not evaluated; nothing of the
// CLI's text; the build removed, the database and trace kept.
func TestLiveAsk(t *testing.T) {
	e := setup(t, approved, mainLine())
	code, so, se := e.run(t, live(e), "--live", "--cli", e.cli, "--base", "HEAD", "--gate", "dev_demo_plugins")
	dir := e.bundleDir(t)
	if code != exitPass || dir == "" || endState(t, dir) != "complete" {
		t.Fatalf("exit %d, end %q\n%s\n%s", code, endState(t, dir), so, se)
	}
	if got := e.starts(); strings.Join(got, ",") != "version,run" {
		t.Fatalf("the CLI started %v", got)
	}
	m := manifest(t, dir)
	if len(m.Results) != 1 || m.Results[0].CaseID != "ask" || m.Results[0].Verdict != "PASS" || len(m.NotEvaluated) != 30+len(contracts.CapabilityIDs) {
		t.Fatalf("results %v, not evaluated %d", m.Results, len(m.NotEvaluated))
	}
	if m.Versions["gate_mode"] != "dev_demo_plugins" || m.Versions["cli"] != approved || len(m.Commands) != 1 || m.Commands[0].ExitCode != 0 {
		t.Fatalf("versions %v commands %v", m.Versions, m.Commands)
	}
	for _, ev := range m.Evidence {
		if ev.EvidenceMode != "live" || (ev.Source != "live_cli" && ev.Source != "provenance") {
			t.Errorf("%s: %s %s", ev.Path, ev.EvidenceMode, ev.Source)
		}
	}
	r := runRecord(t, dir)
	if r.GateMode != "dev_demo_plugins" || r.GateResult != "passed" || r.Usage != "unknown" || r.Snapshot["last_sequence"] != float64(3) ||
		r.Version != "apa.m1.live.v2" || r.Termination != "completed" || r.ProducerError != "" {
		t.Fatalf("run record %+v", r)
	}
	// The stages, in order, on stderr.
	last := -1
	for i := 1; i <= 6; i++ {
		at := strings.Index(se, fmt.Sprintf("m1-live: [%d/6] ", i))
		if at < last {
			t.Fatalf("stage %d missing or out of order:\n%s", i, se)
		}
		last = at
	}
	noMarker(t, dir, so, se)
	work, _ := os.ReadDir(e.work)
	kept, _ := os.ReadDir(filepath.Join(e.work, work[0].Name()))
	var names []string
	for _, k := range kept {
		names = append(names, k.Name())
	}
	if strings.Join(names, ",") != "db,runs,snapshot" {
		t.Fatalf("working files %v (the build must be gone, the database and trace kept)", names)
	}
}

// Nothing is created or started before the preconditions pass.
func TestPreconditions(t *testing.T) {
	// Each case has its own clone and directories: no case's preparation
	// may be what stops another.
	type pc struct {
		prepare func(e env)
		environ func(e env) []string
		args    func(e env) []string
	}
	std := func(e env) []string {
		return []string{"--live", "--cli", e.cli, "--base", "HEAD", "--gate", "dev_demo_plugins"}
	}
	cases := map[string]pc{
		"no APA_LIVE_CLI": {nil, func(e env) []string { return e.env() }, std},
		"no --live":       {nil, live, func(e env) []string { return []string{"--cli", e.cli, "--base", "HEAD", "--gate", "dev_demo_plugins"} }},
		"no gate stated":  {nil, live, func(e env) []string { return []string{"--live", "--cli", e.cli, "--base", "HEAD"} }},
		"another gate":    {nil, live, func(e env) []string { return []string{"--live", "--cli", e.cli, "--base", "HEAD", "--gate", "lenient"} }},
		"a relative CLI path": {nil, live, func(e env) []string {
			return []string{"--live", "--cli", "claude", "--base", "HEAD", "--gate", "dev_demo_plugins"}
		}},
		"no reviewed base":       {nil, live, func(e env) []string { return []string{"--live", "--cli", e.cli, "--gate", "dev_demo_plugins"} }},
		"a changed tracked file": {func(e env) { os.WriteFile(filepath.Join(e.repo, "go.mod"), []byte("module x\n"), 0o600) }, live, std},
		"code after the reviewed base": {func(e env) {
			exec.Command("git", "-C", e.repo, "config", "user.email", "t@example.invalid").Run()
			exec.Command("git", "-C", e.repo, "config", "user.name", "t").Run()
			os.WriteFile(filepath.Join(e.repo, "cmd", "x.go"), []byte("package x\n"), 0o600)
			exec.Command("git", "-C", e.repo, "add", "cmd/x.go").Run()
			exec.Command("git", "-C", e.repo, "commit", "--quiet", "-m", "code").Run()
		}, live, func(e env) []string {
			return []string{"--live", "--cli", e.cli, "--base", "HEAD~1", "--gate", "dev_demo_plugins"}
		}},
		"the bundle root a symlink": {func(e env) { os.Symlink(t.TempDir(), e.out) }, live, std},
		"a build that is not of a clean clone": {func(e env) {
			old := afterClone
			afterClone = func(src string) { os.WriteFile(filepath.Join(src, "README.md"), []byte("changed\n"), 0o600) }
			t.Cleanup(func() { afterClone = old })
		}, live, std},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			e := setup(t, approved, mainLine())
			if tc.prepare != nil {
				tc.prepare(e)
			}
			code, so, se := e.run(t, tc.environ(e), tc.args(e)...)
			if code != exitIncomplete || !strings.Contains(se, "nothing was started") {
				t.Fatalf("exit %d\n%s\n%s", code, so, se)
			}
			if len(e.starts()) != 0 {
				t.Fatalf("the CLI started: %v", e.starts())
			}
			if name == "a build that is not of a clean clone" {
				return // its bundle records the stop
			}
			if fi, err := os.Lstat(e.out); err == nil && fi.IsDir() {
				if entries, _ := os.ReadDir(e.out); len(entries) != 0 {
					t.Fatal("a bundle directory was made")
				}
			}
		})
	}
}

// Runs that fail are kept as they ended: the bundle is complete, the ask
// claim does not pass, nothing is retried.
func TestFailedRuns(t *testing.T) {
	strictRefused := mainLine()
	for name, tc := range map[string]struct {
		version string
		steps   []map[string]any
		args    []string
		starts  string
		verdict string
		trace   bool
		term    string
	}{
		"another CLI version":                 {"2.1.999 (Claude Code)", mainLine(), nil, "version", "INCONCLUSIVE", false, "wrapper_exit_nonzero"},
		"the strict gate refuses the plugins": {approved, strictRefused, []string{"--gate", "strict"}, "version,run", "INCONCLUSIVE", true, "wrapper_exit_nonzero"},
		"no purchase requested": {approved, []map[string]any{{"do": "init", "init": liveInit}, {"do": "tool", "tool": "list_products"}, {"do": "result", "text": "no"}},
			nil, "version,run", "FAIL", true, "completed"},
		"stopped by the outer bound": {approved, []map[string]any{{"do": "init", "init": liveInit}, {"do": "tool", "tool": "list_products"}, {"do": "hang"}},
			[]string{"--wait", "4s"}, "version,run", "FAIL", true, "outer_bound"},
	} {
		t.Run(name, func(t *testing.T) {
			e := setup(t, tc.version, tc.steps)
			args := []string{"--live", "--cli", e.cli, "--base", "HEAD", "--gate", "dev_demo_plugins"}
			args = append(args, tc.args...)
			start := time.Now()
			code, so, se := e.run(t, live(e), args...)
			dir := e.bundleDir(t)
			if code != exitFailures || dir == "" || endState(t, dir) != "complete" {
				t.Fatalf("exit %d, end %q (%s)\n%s\n%s", code, endState(t, dir), time.Since(start), so, se)
			}
			if got := strings.Join(e.starts(), ","); got != tc.starts {
				t.Fatalf("the CLI started %s, want %s (no retry)", got, tc.starts)
			}
			m := manifest(t, dir)
			if m.Results[0].Verdict != tc.verdict {
				t.Fatalf("ask %s (%s), want %s", m.Results[0].Verdict, m.Results[0].Reason, tc.verdict)
			}
			if _, err := os.Stat(filepath.Join(dir, "live", "trace.jsonl")); (err == nil) != tc.trace {
				t.Fatalf("trace kept: %v", err == nil)
			}
			if r := runRecord(t, dir); r.Termination != tc.term || (r.ProducerError == "") != (tc.term == "completed" || tc.term == "wrapper_exit_nonzero") {
				t.Fatalf("termination %q, producer error %q; want %s", r.Termination, r.ProducerError, tc.term)
			}
			noMarker(t, dir, so, se)
		})
	}
}

// A bundle not saved completely says so: exit 2, no success line, what was
// saved kept.
func TestSaveFailure(t *testing.T) {
	e := setup(t, approved, mainLine())
	defer func(f func(string)) { beforeManifest = f }(beforeManifest)
	beforeManifest = func(dir string) { os.Mkdir(filepath.Join(dir, "run-end.json"), 0o700) }
	code, so, se := e.run(t, live(e), "--live", "--cli", e.cli, "--base", "HEAD", "--gate", "dev_demo_plugins")
	dir := e.bundleDir(t)
	if code != exitIncomplete || strings.Contains(so, "complete") || !strings.Contains(se, "INCOMPLETE") {
		t.Fatalf("exit %d\n%s\n%s", code, so, se)
	}
	if _, err := os.Stat(filepath.Join(dir, "live", "ask.json")); err != nil {
		t.Fatalf("what was saved is kept: %v", err)
	}
	if got := strings.Join(e.starts(), ","); got != "version,run" {
		t.Fatalf("the CLI started %s", got)
	}
}

// The scan fails any bare 64-hex value except the trace's prompt_sha256 of
// the host's fixed prompt, exactly.
func TestScan(t *testing.T) {
	dir := t.TempDir()
	os.WriteFile(filepath.Join(dir, "trace.jsonl"), append([]byte(`{"event":"run_start",`), append(promptDigest(), '}')...), 0o600)
	if leaks := scan(dir); len(leaks) != 0 {
		t.Fatalf("the fixed prompt's digest flagged: %v", leaks)
	}
	other := strings.Repeat("ab", 32)
	for _, s := range []string{`{"prompt_sha256":"` + other + `"}`, `{"token":"` + other + `"}`, other} {
		os.WriteFile(filepath.Join(dir, "x.json"), []byte(s), 0o600)
		if leaks := scan(dir); len(leaks) != 1 || leaks[0] != "x.json" {
			t.Fatalf("%q: %v", s, leaks)
		}
	}
}

// The wrapper that cannot be started: recorded as such, no CLI start, no
// success.
func TestStartFailure(t *testing.T) {
	e := setup(t, approved, mainLine())
	defer func(f func(string)) { beforeRun = f }(beforeRun)
	beforeRun = func(bin string) { os.Remove(bin) }
	code, so, se := e.run(t, live(e), "--live", "--cli", e.cli, "--base", "HEAD", "--gate", "dev_demo_plugins")
	dir := e.bundleDir(t)
	if code != exitFailures || endState(t, dir) != "complete" || len(e.starts()) != 0 {
		t.Fatalf("exit %d, end %q, starts %v\n%s\n%s", code, endState(t, dir), e.starts(), so, se)
	}
	if r := runRecord(t, dir); r.Termination != "start_failed" || r.ProducerError != "apa-buyer could not be started" || r.ExitCode != -1 {
		t.Fatalf("%+v", r)
	}
	if m := manifest(t, dir); m.Results[0].Verdict == "PASS" {
		t.Fatal("a run that never started is a PASS")
	}
}

// ready waits until the one run has started: the stand-in CLI has been
// started for the run and the Store exists (the task started).
func ready(t *testing.T, e env) (cliPID int) {
	t.Helper()
	for end := time.Now().Add(60 * time.Second); time.Now().Before(end); time.Sleep(50 * time.Millisecond) {
		dbs, _ := filepath.Glob(filepath.Join(e.work, "*", "db", "live.db"))
		pid, _ := os.ReadFile(e.count + ".pid")
		if len(dbs) == 1 && strings.Contains(strings.Join(e.starts(), ","), "run") && len(bytes.TrimSpace(pid)) > 0 {
			fmt.Sscan(string(pid), &cliPID)
			return cliPID
		}
	}
	t.Fatal("the run did not start")
	return 0
}

// state is a process's state: "running", "zombie" or "gone" (reaped).
func state(pid int) string {
	out, _ := exec.Command("ps", "-o", "stat=", "-p", fmt.Sprint(pid)).Output()
	switch s := strings.TrimSpace(string(out)); {
	case s == "":
		return "gone"
	case strings.HasPrefix(s, "Z"):
		return "zombie"
	}
	return "running"
}

func parent(pid int) int {
	out, _ := exec.Command("ps", "-o", "ppid=", "-p", fmt.Sprint(pid)).Output()
	var p int
	fmt.Sscan(strings.TrimSpace(string(out)), &p)
	return p
}

var hang = []map[string]any{{"do": "init", "init": liveInit}, {"do": "tool", "tool": "list_products"}, {"do": "hang"}}

// An interruption (the run's context ending, in process) stops the run:
// recorded as interrupted, no success, one run, the wrapper and the CLI
// reaped.
func TestInterruptInProcess(t *testing.T) {
	e := setup(t, approved, hang)
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan int, 1)
	var so, se bytes.Buffer
	go func() {
		done <- run(ctx, []string{"--repo", e.repo, "--out", e.out, "--work", e.work, "--live", "--cli", e.cli, "--base", "HEAD", "--gate", "dev_demo_plugins"}, live(e), &so, &se)
	}()
	cli := ready(t, e)
	wrapper := parent(cli)
	cancel()
	var code int
	select {
	case code = <-done:
	case <-time.After(90 * time.Second):
		t.Fatal("the producer did not end after the interruption")
	}
	dir := e.bundleDir(t)
	if code != exitFailures || endState(t, dir) != "complete" || strings.Join(e.starts(), ",") != "version,run" {
		t.Fatalf("exit %d, end %q, starts %v\n%s", code, endState(t, dir), e.starts(), se.String())
	}
	if r := runRecord(t, dir); r.Termination != "interrupted" || !strings.HasPrefix(r.ProducerError, "interrupted") {
		t.Fatalf("%+v", r)
	}
	for _, pid := range []int{cli, wrapper} {
		if s := state(pid); s != "gone" {
			t.Fatalf("process %d is %s after the producer ended", pid, s)
		}
	}
}

// Real signals to the producer process: the wrapper and the CLI it
// started end and are reaped; a second signal during the cleanup does not
// cut it short; the bundle records the interruption, not a success.
func TestSignals(t *testing.T) {
	bin := filepath.Join(t.TempDir(), "m1-live")
	if out, err := exec.Command("go", "build", "-o", bin, ".").CombinedOutput(); err != nil {
		t.Fatalf("%v %s", err, out)
	}
	for name, sigs := range map[string][]syscall.Signal{
		"SIGINT":  {syscall.SIGINT},
		"SIGTERM": {syscall.SIGTERM},
		"SIGINT, then SIGTERM during the cleanup": {syscall.SIGINT, syscall.SIGTERM},
	} {
		t.Run(name, func(t *testing.T) {
			e := setup(t, approved, hang)
			c := exec.Command(bin, "--repo", e.repo, "--out", e.out, "--work", e.work, "--live", "--cli", e.cli, "--base", "HEAD", "--gate", "dev_demo_plugins")
			c.Env = live(e)
			var so, se bytes.Buffer
			c.Stdout, c.Stderr = &so, &se
			if err := c.Start(); err != nil {
				t.Fatal(err)
			}
			cli := ready(t, e)
			wrapper := parent(cli)
			for i, s := range sigs {
				if i > 0 {
					time.Sleep(100 * time.Millisecond)
				}
				c.Process.Signal(s)
			}
			waited := make(chan error, 1)
			go func() { waited <- c.Wait() }()
			select {
			case <-waited:
			case <-time.After(90 * time.Second):
				c.Process.Kill()
				t.Fatal("the producer did not end after the signal")
			}
			code := c.ProcessState.ExitCode()
			dir := e.bundleDir(t)
			if code != exitFailures || endState(t, dir) != "complete" || strings.Join(e.starts(), ",") != "version,run" {
				t.Fatalf("exit %d, end %q, starts %v\n%s", code, endState(t, dir), e.starts(), se.String())
			}
			if r := runRecord(t, dir); r.Termination != "interrupted" {
				t.Fatalf("termination %q", r.Termination)
			}
			if len(sigs) > 1 && !strings.Contains(se.String(), "already stopping") {
				t.Fatalf("the second signal was not reported:\n%s", se.String())
			}
			for _, pid := range []int{cli, wrapper} {
				if s := state(pid); s != "gone" {
					t.Fatalf("process %d is %s after the producer ended", pid, s)
				}
			}
			noMarker(t, dir, so.String(), se.String())
		})
	}
}

// A quiet stage reports that it is still waiting, and stops reporting when
// it ends.
func TestHeartbeats(t *testing.T) {
	e := setup(t, approved, hang)
	_, _, se := e.run(t, live(e), "--live", "--cli", e.cli, "--base", "HEAD", "--gate", "dev_demo_plugins", "--wait", "2s", "--heartbeat", "300ms")
	beat := strings.LastIndex(se, "m1-live: ... apa-buyer: still waiting")
	if beat < 0 || beat > strings.Index(se, "m1-live: [5/6]") || beat < strings.Index(se, "m1-live: [4/6]") {
		t.Fatalf("no heartbeat during the wrapper run, or one after it:\n%s", se)
	}
}
