package buyer

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"sync/atomic"
	"syscall"
	"testing"
	"time"

	"github.com/SIMPLYBOYS/agent-payment-authority/internal/buyer/fakecli"
	"github.com/SIMPLYBOYS/agent-payment-authority/internal/contracts"
	"github.com/SIMPLYBOYS/agent-payment-authority/internal/merchant"
	"github.com/SIMPLYBOYS/agent-payment-authority/internal/policy"
	"github.com/SIMPLYBOYS/agent-payment-authority/internal/store"
	"github.com/SIMPLYBOYS/agent-payment-authority/internal/toolbridge"
)

// The test binary doubles as the fake CLI (run through a symlink named
// fake-claude) and as the MCP bridge (argv "mcp-bridge"), so every test
// starts real processes without a separate build.
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

var testLimits = Limits{Line: 64 << 10, Stdout: 100 << 10, Stderr: 64 << 10, Total: 10 * time.Second, Grace: 500 * time.Millisecond}

func self(t *testing.T) string {
	t.Helper()
	p, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	return p
}

// fake is a symlink named fake-claude to the test binary.
func fake(t *testing.T) string {
	t.Helper()
	link := filepath.Join(t.TempDir(), "fake-claude")
	if err := os.Symlink(self(t), link); err != nil {
		t.Fatal(err)
	}
	return link
}

func scenario(name string) string {
	p, _ := filepath.Abs(filepath.Join("../../testdata/cli", name+".json"))
	return fakecli.EnvScenario + "=" + p
}

// runnerSpec runs a scenario with no host behind the bridge: enough for
// scenarios without tool steps.
func runnerSpec(t *testing.T, name string, lim Limits, env ...string) Spec {
	t.Helper()
	dir := t.TempDir()
	cfg, _ := json.Marshal(map[string]any{"mcpServers": map[string]any{"apa": map[string]any{
		"type": "stdio", "command": self(t), "args": []string{"mcp-bridge"},
		"env": map[string]string{toolbridge.EnvSocket: "/nonexistent", toolbridge.EnvCapability: "none"}}}})
	path := filepath.Join(dir, "mcp.json")
	os.WriteFile(path, cfg, 0o600)
	return Spec{Path: fake(t), Args: Argv("test prompt", path), Dir: dir, Env: CLIEnv(os.Getenv, append(env, scenario(name))...), Limits: lim}
}

func TestRunnerOutcomes(t *testing.T) {
	for _, c := range []struct {
		scenario string
		want     Outcome
		exit     int
	}{
		{"stream-split", OK, 0},
		{"truncated-json", ParseError, -1},
		{"not-json", ParseError, -1},
		{"two-results", ParseError, -1},
		{"oversized-line", OutputOverflow, -1},
		{"oversized-total", OutputOverflow, -1},
		{"stderr-flood", OutputOverflow, -1},
		{"nonzero-exit", NonzeroExit, 3},
		{"missing-result", MissingResult, 0},
		{"error-result", CLIError, 0},
		{"not-logged-in", NotLoggedIn, 1},
		{"rate-limited", RateLimited, 1},
	} {
		t.Run(c.scenario, func(t *testing.T) {
			r := Run(context.Background(), runnerSpec(t, c.scenario, testLimits))
			if r.Outcome != c.want {
				t.Fatalf("outcome %s, want %s (%+v)", r.Outcome, c.want, r)
			}
			// A fault may race the process's own exit; only check codes the
			// process chose.
			if c.exit >= 0 && r.ExitCode != c.exit {
				t.Errorf("exit %d, want %d", r.ExitCode, c.exit)
			}
		})
	}
	r := Run(context.Background(), runnerSpec(t, "stream-split", testLimits))
	if r.Messages != 5 || r.Result != "done" || r.Usage != nil {
		t.Errorf("split stream parsed as %+v", r)
	}
}

func TestRunnerNotInstalled(t *testing.T) {
	s := runnerSpec(t, "stream-split", testLimits)
	s.Path = filepath.Join(t.TempDir(), "claude")
	if r := Run(context.Background(), s); r.Outcome != NotInstalled || r.ExitCode != -1 {
		t.Errorf("%+v", r)
	}
}

// alive: the process exists and is not a zombie. A killed process whose
// parent has exited waits as a zombie until PID 1 reaps it, and a container
// without an init may never do so; a zombie runs nothing. (/proc is Linux
// only; elsewhere the kill check alone decides.)
func alive(pid int) bool {
	if syscall.Kill(pid, 0) != nil {
		return false
	}
	stat, err := os.ReadFile(fmt.Sprintf("/proc/%d/stat", pid))
	if i := strings.LastIndexByte(string(stat), ')'); err == nil && i > 0 && i+2 < len(stat) && stat[i+2] == 'Z' {
		return false
	}
	return true
}

func gone(pid int, within time.Duration) bool {
	for end := time.Now().Add(within); time.Now().Before(end); time.Sleep(20 * time.Millisecond) {
		if !alive(pid) {
			return true
		}
	}
	return false
}

func pids(t *testing.T, file string) []int {
	t.Helper()
	raw, _ := os.ReadFile(file)
	var out []int
	for _, f := range strings.Fields(string(raw)) {
		n, _ := strconv.Atoi(f)
		out = append(out, n)
	}
	return out
}

func TestRunnerTimeoutCancelAndCleanup(t *testing.T) {
	// A process that is not the run's must survive every kill below.
	bystander := exec.Command("/bin/sleep", "60")
	if err := bystander.Start(); err != nil {
		t.Fatal(err)
	}
	defer func() { bystander.Process.Kill(); bystander.Wait() }()

	lim := testLimits
	lim.Total = time.Second
	start := time.Now()
	r := Run(context.Background(), runnerSpec(t, "hang-ignore-term", lim))
	if r.Outcome != Timeout || time.Since(start) > lim.Total+2*lim.Grace+2*time.Second {
		t.Errorf("SIGTERM-proof hang: %+v after %v", r, time.Since(start))
	}

	ctx, cancel := context.WithCancel(context.Background())
	time.AfterFunc(300*time.Millisecond, cancel)
	if r := Run(ctx, runnerSpec(t, "hang-ignore-term", testLimits)); r.Outcome != Cancelled {
		t.Errorf("cancel: %+v", r)
	}

	// A grandchild in the group holding the pipes is killed with the group.
	pidFile := filepath.Join(t.TempDir(), "pids")
	start = time.Now()
	r = Run(context.Background(), runnerSpec(t, "grandchild-pipe", testLimits, fakecli.EnvPIDFile+"="+pidFile))
	if r.Outcome != OK || time.Since(start) > 5*time.Second {
		t.Errorf("grandchild: %+v after %v", r, time.Since(start))
	}
	if p := pids(t, pidFile); len(p) != 1 || !gone(p[0], 3*time.Second) {
		t.Errorf("grandchild %v still alive", p)
	}

	// One that left the group (new session) is not ours to signal: the
	// runner abandons the pipes after Grace and says so.
	pidFile = filepath.Join(t.TempDir(), "pids")
	start = time.Now()
	r = Run(context.Background(), runnerSpec(t, "detached-pipe", testLimits, fakecli.EnvPIDFile+"="+pidFile))
	p := pids(t, pidFile)
	if len(p) == 1 {
		defer syscall.Kill(p[0], syscall.SIGKILL)
	}
	if r.Outcome != Orphaned || time.Since(start) > 5*time.Second {
		t.Errorf("detached holder: %+v after %v", r, time.Since(start))
	}
	if len(p) != 1 || !alive(p[0]) {
		t.Errorf("detached holder %v was signalled", p)
	}
	if !alive(bystander.Process.Pid) {
		t.Error("an unrelated process was killed")
	}
}

func TestArgvAndEnv(t *testing.T) {
	a := Argv("PROMPT", "/cfg.json")
	want := []string{"-p", "PROMPT", "--output-format", "stream-json", "--verbose", "--mcp-config", "/cfg.json", "--strict-mcp-config",
		"--tools", "", "--allowedTools", "mcp__apa__list_products,mcp__apa__get_quote,mcp__apa__request_purchase",
		"--permission-mode", "dontAsk", "--setting-sources", "project", "--no-session-persistence", "--disable-slash-commands"}
	if !slices.Equal(a, want) {
		t.Errorf("argv %q", a)
	}
	parent := map[string]string{"HOME": "/h", "PATH": "/bin", "ANTHROPIC_API_KEY": "sk-x", "CLAUDE_CODE_USE_BEDROCK": "1",
		"HTTPS_PROXY": "http://p", "AWS_SECRET_ACCESS_KEY": "k", toolbridge.EnvCapability: "c", "APA_DB_PATH": "/db"}
	env := CLIEnv(func(k string) string { return parent[k] }, "EXTRA=1")
	if !slices.Equal(env, []string{"HOME=/h", "PATH=/bin", "EXTRA=1"}) {
		t.Errorf("env %q", env)
	}
}

// hostEnv is a full host: shop over HTTP, Store file, bridge, fake CLI.
type hostEnv struct {
	t    *testing.T
	now  atomic.Uint64
	reg  contracts.RegistryConfig
	st   *store.Store
	h    *Host
	prof policy.Profile
}

func newHost(t *testing.T) *hostEnv {
	t.Helper()
	e := &hostEnv{t: t}
	e.now.Store(t0)
	raw, _ := os.ReadFile("../../testdata/contracts/registry.json")
	if err := contracts.Decode(raw, &e.reg); err != nil {
		t.Fatal(err)
	}
	raw, _ = os.ReadFile("../../testdata/policy/m1-demo-profile.json")
	var err error
	if e.prof, err = policy.LoadProfile(raw); err != nil {
		t.Fatal(err)
	}
	reg, _ := contracts.NewRegistry(e.reg)
	mux := http.NewServeMux()
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	shop, _ := merchant.New(srv.URL)
	shop.Register(mux)
	offers, products := store.OfferBook{}, map[string]toolbridge.Product{}
	for _, o := range shop.Offers() {
		k := store.OfferKey{Method: o.Method, URL: o.URL}
		offers[k] = append(offers[k], o.Quote)
		products[o.ProductID] = toolbridge.Product{ID: o.ProductID, URL: o.URL}
	}
	if e.st, err = store.Open(store.Config{Path: filepath.Join(t.TempDir(), "db", "apa.db"), Registry: e.reg, Offers: offers, Clock: e.now.Load}); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { e.st.Close() })
	bridge, err := toolbridge.Listen(&toolbridge.Shop{Store: e.st, CatalogURL: srv.URL + "/catalog", Products: products, Client: toolbridge.NewClient(), Now: e.now.Load})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { bridge.Close() })
	e.h = &Host{Store: e.st, Registry: reg, Bridge: bridge, CLI: fake(t), BridgeCmd: []string{self(t), "mcp-bridge"},
		Limits: testLimits, TraceDir: filepath.Join(t.TempDir(), "runs")}
	return e
}

func (e *hostEnv) start(task string, withProfile bool) contracts.Session {
	e.t.Helper()
	reg, _ := contracts.NewRegistry(e.reg)
	tk, _, _ := reg.Binding(task)
	cred := policy.Credential{ConsumerID: tk.ConsumerID, ValidUntil: t0 + 86400, EvidenceMode: "fixture"}
	setup := store.TaskSetup{TaskID: task, Credential: &cred, ProfileAbsent: true, BudgetInitial: "5000000", AssetID: "fixture-usdc"}
	if withProfile {
		setup = store.TaskSetup{TaskID: task, Credential: &cred}
		p := e.prof
		setup.Profile = &p
	}
	if err := e.st.StartTask(setup); err != nil {
		e.t.Fatal(err)
	}
	return contracts.Session{ConsumerID: tk.ConsumerID, AgentID: tk.AgentID, TaskID: task}
}

func (e *hostEnv) run(ctx context.Context, task, scen string, env ...string) RunResult {
	e.t.Helper()
	e.h.Env = CLIEnv(os.Getenv, append(env, scenario(scen))...)
	res, err := e.h.Run(ctx, task, "Buy the brief.")
	if err != nil {
		e.t.Fatal(err)
	}
	return res
}

func (e *hostEnv) task(sess contracts.Session) store.TaskView {
	e.t.Helper()
	v, err := e.st.Task(sess)
	if err != nil {
		e.t.Fatal(err)
	}
	return v
}

func (e *hostEnv) eventTypes(sess contracts.Session) []string {
	ev, _ := e.st.Events(sess)
	var out []string
	for _, x := range ev {
		out = append(out, x.EventType)
	}
	return out
}

func outcomes(calls []toolbridge.CallRecord) string {
	var s []string
	for _, c := range calls {
		s = append(s, c.Tool+":"+c.Outcome)
	}
	return strings.Join(s, " ")
}

func TestHostMainLine(t *testing.T) {
	e := newHost(t)
	sess := e.start("task-a", true)
	tmp := t.TempDir()
	t.Setenv("TMPDIR", tmp) // after the bridge is up: the run's own directory lands here
	res := e.run(context.Background(), "task-a", "ask-mainline")
	if res.Report.Outcome != OK || res.StoreEnd != "" {
		t.Fatalf("%+v", res)
	}
	if got := outcomes(res.Calls); got != "list_products:ok get_quote:ok request_purchase:Ask" {
		t.Errorf("calls %s", got)
	}
	v := e.task(sess)
	if v.Status != "awaiting_approval" || len(v.Purchases) != 1 || v.Purchases[0].Decision.PolicyVerdict != "Ask" ||
		v.Purchases[0].Decision.PaymentStatus != "unavailable" || v.Purchases[0].ApprovalClosed != "" {
		t.Fatalf("store: %+v", v)
	}
	// The CLI's claim of payment changed nothing.
	if got := strings.Join(e.eventTypes(sess), ","); got != "TASK_STARTED,PURCHASE_PROPOSED,POLICY_DECIDED" {
		t.Errorf("events %s", got)
	}
	if left, _ := os.ReadDir(tmp); len(left) != 0 {
		t.Errorf("run left %v behind", left)
	}
	trace, _ := os.ReadFile(res.Trace)
	if !strings.Contains(string(trace), `"untrusted_cli_result":"Paid and downloaded the Korean market brief."`) || strings.Contains(string(trace), "Buy the brief") {
		t.Errorf("trace: %s", trace)
	}
}

func TestHostForgedAndUnauthenticatedRequests(t *testing.T) {
	e := newHost(t)
	sess := e.start("task-a", true)
	res := e.run(context.Background(), "task-a", "forged-args")
	want := "request_purchase:INVALID_ARGUMENTS request_purchase:INVALID_ARGUMENTS get_quote:INVALID_ARGUMENTS " +
		"get_quote:INVALID_ARGUMENTS get_quote:INVALID_ARGUMENTS get_quote:UNKNOWN_PRODUCT"
	if res.Report.Outcome != OK || outcomes(res.Calls) != want {
		t.Fatalf("%s / %+v", outcomes(res.Calls), res.Report)
	}
	// Plain text that names a tool call is a parse error, never a request.
	res = e.run(context.Background(), "task-a", "not-json")
	if res.Report.Outcome != ParseError || len(res.Calls) != 0 {
		t.Fatalf("%+v", res)
	}
	if v := e.task(sess); v.Status != "running" || len(v.Purchases) != 0 {
		t.Fatalf("store: %+v", v)
	}
}

func TestHostRetryAndCallBudget(t *testing.T) {
	e := newHost(t)
	sess := e.start("task-a", true)
	res := e.run(context.Background(), "task-a", "retry-same")
	if outcomes(res.Calls) != "request_purchase:Ask request_purchase:Ask" || len(e.task(sess).Purchases) != 1 {
		t.Fatalf("%s", outcomes(res.Calls))
	}
	res = e.run(context.Background(), "task-a", "too-many-calls")
	if n := len(res.Calls); n != toolbridge.MaxCalls+1 || res.Calls[n-1].Outcome != "TOO_MANY_CALLS" {
		t.Fatalf("%s", outcomes(res.Calls))
	}
}

func waitPurchase(t *testing.T, e *hostEnv, sess contracts.Session) {
	t.Helper()
	for end := time.Now().Add(5 * time.Second); time.Now().Before(end); time.Sleep(20 * time.Millisecond) {
		if v, _ := e.st.Task(sess); len(v.Purchases) == 1 {
			return
		}
	}
	t.Fatal("purchase never recorded")
}

func TestHostEndings(t *testing.T) {
	e := newHost(t)
	e.h.Limits.Total = 3 * time.Second // room for the purchase to land first, even under -race

	// CLI timeout: TASK_TIMED_OUT, the Ask stays pending.
	sess := e.start("task-a", true)
	res := e.run(context.Background(), "task-a", "hang")
	v := e.task(sess)
	if res.Report.Outcome != Timeout || res.StoreEnd != "TASK_TIMED_OUT" || !slices.Contains(e.eventTypes(sess), "TASK_TIMED_OUT") || v.Purchases[0].ApprovalClosed != "" {
		t.Fatalf("timeout: %+v %+v", res, v)
	}
	// A second end is the Store's refusal, reported, not hidden.
	res = e.run(context.Background(), "task-a", "hang")
	if res.StoreEnd != "" || res.EndError != "TASK_ALREADY_ENDED" {
		t.Errorf("second end: %+v", res)
	}

	// Host cancel: TASK_CANCELLED, the open Ask is withdrawn.
	sessB := e.start("task-b", true)
	e.h.Limits.Total = 10 * time.Second
	ctx, cancel := context.WithCancel(context.Background())
	go func() { waitPurchase(t, e, sessB); cancel() }()
	res = e.run(ctx, "task-b", "hang")
	v = e.task(sessB)
	if res.Report.Outcome != Cancelled || res.StoreEnd != "TASK_CANCELLED" || v.Status != "cancelled" || v.Purchases[0].ApprovalClosed != "withdrawn" {
		t.Fatalf("cancel: %+v %+v", res, v)
	}

	// A Deny stays the status and the reason through a later timeout.
	e2 := newHost(t)
	e2.h.Limits.Total = 3 * time.Second
	sess = e2.start("task-a", false)
	res = e2.run(context.Background(), "task-a", "hang")
	v = e2.task(sess)
	if outcomes(res.Calls) != "request_purchase:Deny" || res.StoreEnd != "TASK_TIMED_OUT" || v.Status != "denied" || v.DenyReason != "CONFIG_MISSING" {
		t.Fatalf("deny: %+v %+v", res, v)
	}
}

func TestHostAskExpires(t *testing.T) {
	e := newHost(t)
	sess := e.start("task-a", true)
	e.run(context.Background(), "task-a", "ask-mainline")
	p := e.task(sess).Purchases[0]
	e.now.Store(p.DeadlineAt)
	if err := e.st.ExpireDue(); err != nil {
		t.Fatal(err)
	}
	if q := e.task(sess).Purchases[0]; q.ApprovalClosed != "expired" || q.Decision != p.Decision {
		t.Errorf("after deadline: %+v", q)
	}
}

func TestHostCapabilityLifetime(t *testing.T) {
	e := newHost(t)
	sessA := e.start("task-a", true)
	capFile := filepath.Join(t.TempDir(), "cap")
	res := e.run(context.Background(), "task-a", "leak-capability", fakecli.EnvCapFile+"="+capFile)
	capability, _ := os.ReadFile(capFile)
	if res.Report.Outcome != OK || len(capability) != 64 {
		t.Fatalf("%+v cap %q", res, capability)
	}
	if !strings.Contains(res.Report.Result, string(capability)) {
		t.Fatal("the scenario did not leak the capability; the trace check below would prove nothing")
	}
	trace, _ := os.ReadFile(res.Trace)
	if strings.Contains(string(trace), string(capability)) || !strings.Contains(string(trace), "the capability is [capability]") {
		t.Fatalf("trace: %s", trace)
	}
	// Late: after the run, the capability opens nothing.
	args := json.RawMessage(`{"product_id":"kr-market"}`)
	r, err := toolbridge.Dial(context.Background(), e.h.Bridge.SocketPath(), string(capability), "request_purchase", args)
	if err != nil || !r.IsError || !strings.Contains(r.Content[0].Text, "RUN_NOT_ACTIVE") {
		t.Fatalf("late call: %+v %v", r, err)
	}
	// Cross-run: while B's run is active, A's old capability still opens nothing.
	sessB := e.start("task-b", true)
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan RunResult)
	go func() { done <- e.run(ctx, "task-b", "hang") }()
	waitPurchase(t, e, sessB)
	r, _ = toolbridge.Dial(context.Background(), e.h.Bridge.SocketPath(), string(capability), "request_purchase", args)
	cancel()
	<-done
	if !strings.Contains(r.Content[0].Text, "RUN_NOT_ACTIVE") || len(e.task(sessA).Purchases) != 0 {
		t.Fatalf("replay during another run: %+v", r)
	}
}

func TestHostNotInstalled(t *testing.T) {
	e := newHost(t)
	sess := e.start("task-a", true)
	e.h.CLI = filepath.Join(t.TempDir(), "claude")
	res := e.run(context.Background(), "task-a", "ask-mainline")
	if res.Report.Outcome != NotInstalled || res.StoreEnd != "" || e.task(sess).Status != "running" {
		t.Fatalf("%+v", res)
	}
	if _, err := e.h.Run(context.Background(), "task-unknown", "x"); err == nil {
		t.Error("unknown task ran")
	}
	if _, err := e.h.Run(context.Background(), "task-a-next", "x"); err == nil {
		t.Error("unstarted task ran")
	}
}

func script(t *testing.T, body string) string {
	t.Helper()
	p := filepath.Join(t.TempDir(), "cli")
	if err := os.WriteFile(p, []byte("#!/bin/sh\n"+body+"\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	return p
}

// R2: the version check runs under the runner's rules.
func TestVersion(t *testing.T) {
	lim := Limits{Stdout: 4 << 10, Stderr: 4 << 10, Total: 5 * time.Second, Grace: 500 * time.Millisecond}
	env := CLIEnv(os.Getenv)
	if v, err := Version(context.Background(), fake(t), env, lim); err != nil || !strings.Contains(v, "fake-claude") {
		t.Fatalf("fake: %q %v", v, err)
	}

	// A descendant holding the pipe neither delays the answer nor survives it.
	pidFile := filepath.Join(t.TempDir(), "pid")
	holder := script(t, "sleep 30 & echo $! > "+pidFile+"\necho fake-claude")
	start := time.Now()
	v, err := Version(context.Background(), holder, env, lim)
	t.Logf("holder check took %v", time.Since(start))
	if err != nil || !strings.Contains(v, "fake-claude") || time.Since(start) > 2*time.Second {
		t.Errorf("holder: %q %v after %v", v, err, time.Since(start))
	}
	if p := pids(t, pidFile); len(p) != 1 || !gone(p[0], 3*time.Second) {
		t.Errorf("holder %v survived", p)
	}
	// The same with a 150 ms context (the review's reproduction): whichever
	// way it ends, it ends at once and leaves no holder behind.
	pidFile2 := filepath.Join(t.TempDir(), "pid")
	ctx, cancel := context.WithTimeout(context.Background(), 150*time.Millisecond)
	defer cancel()
	start = time.Now()
	v, err = Version(ctx, script(t, "sleep 30 & echo $! > "+pidFile2+"\necho fake-claude"), env, lim)
	t.Logf("150 ms holder check: %q %v after %v", v, err, time.Since(start))
	if time.Since(start) > time.Second {
		t.Errorf("150 ms holder check took %v", time.Since(start))
	}
	for _, p := range pids(t, pidFile2) {
		if !gone(p, 3*time.Second) {
			t.Errorf("holder %d survived", p)
		}
	}

	// A check that outlives its context fails promptly.
	ctx, cancel = context.WithTimeout(context.Background(), 150*time.Millisecond)
	defer cancel()
	start = time.Now()
	if v, err := Version(ctx, script(t, "sleep 5\necho fake-claude"), env, lim); err == nil || time.Since(start) > 1500*time.Millisecond {
		t.Errorf("slow: %q %v after %v", v, err, time.Since(start))
	}

	for name, body := range map[string]string{
		"nonzero exit": "echo fake-claude\nexit 3",
		"flood":        "echo fake-claude\nhead -c 8192 /dev/zero",
		"missing":      "",
	} {
		path := script(t, body)
		if name == "missing" {
			path += "-absent"
		}
		if v, err := Version(context.Background(), path, env, lim); err == nil {
			t.Errorf("%s: accepted %q", name, v)
		}
	}
}
