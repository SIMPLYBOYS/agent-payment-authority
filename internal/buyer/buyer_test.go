package buyer

import (
	"context"
	"encoding/json"
	"errors"
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
	p := name + ".json"
	if !filepath.IsAbs(name) {
		p, _ = filepath.Abs(filepath.Join("../../testdata/cli", p))
	}
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
		"--restricted", "--tools", "", "--permission-mode", "dontAsk", "--permission-prompts", "none",
		"--allowedTools", "mcp__apa__list_products,mcp__apa__get_quote,mcp__apa__request_purchase",
		"--model", "sonnet", "--effort", "low", "--no-session-persistence", "--disable-slash-commands"}
	if !slices.Equal(a, want) {
		t.Errorf("argv %q", a)
	}
	if i := slices.Index(a, "--tools"); a[i+1] != "" {
		t.Errorf("--tools value %q is not the empty string", a[i+1])
	}
	for _, banned := range []string{"--bare", "--safe-mode", "--dangerously-skip-permissions", "bypassPermissions", "--setting-sources"} {
		if slices.Contains(a, banned) {
			t.Errorf("argv has %s", banned)
		}
	}
	parent := map[string]string{"HOME": "/h", "PATH": "/bin", "ANTHROPIC_API_KEY": "sk-x", "ANTHROPIC_AUTH_TOKEN": "t", "CLAUDE_CODE_OAUTH_TOKEN": "o",
		"ANTHROPIC_PROFILE": "p", "CLAUDE_CODE_USE_BEDROCK": "1", "CLAUDE_CONFIG_DIR": "/c", "HTTPS_PROXY": "http://p",
		"AWS_SECRET_ACCESS_KEY": "k", toolbridge.EnvCapability: "c", "APA_DB_PATH": "/db"}
	env := CLIEnv(func(k string) string { return parent[k] }, "EXTRA=1")
	if !slices.Equal(env, []string{"HOME=/h", "PATH=/bin", "DISABLE_AUTOUPDATER=1", "CLAUDE_CODE_DISABLE_NONESSENTIAL_TRAFFIC=1", "CLAUDE_AGENT_SDK_DISABLE_BUILTIN_AGENTS=1", "EXTRA=1"}) {
		t.Errorf("env %q", env)
	}
}

// L1-a: the built-in agents switch reaches the CLI process as 1, whatever
// the parent environment says (the parent's value is not allowlisted).
func TestCLIEnvReachesChild(t *testing.T) {
	cli := script(t, `printf '%s|%s\n' "${CLAUDE_AGENT_SDK_DISABLE_BUILTIN_AGENTS-unset}" "${CLAUDE_CODE_OAUTH_TOKEN-unset}"`)
	// Ready first: a script created a moment ago can take seconds to start
	// once under a loaded go test run (docs/cli-poc.md 34.2). This test is
	// about the environment, not about that limit, so the script first runs
	// once in a warm-up capped at 60 s, without the 10 s test limit of the
	// limited call below.
	ready, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()
	if err := exec.CommandContext(ready, cli).Run(); err != nil {
		t.Fatalf("the script does not start: %v", err)
	}
	parent := map[string]string{"PATH": os.Getenv("PATH"), "HOME": t.TempDir(), "CLAUDE_AGENT_SDK_DISABLE_BUILTIN_AGENTS": "0", "CLAUDE_CODE_OAUTH_TOKEN": "o"}
	out, err := Version(context.Background(), cli, CLIEnv(func(k string) string { return parent[k] }), testLimits)
	if err != nil || out != "1|unset\n" {
		t.Fatalf("child saw %q (%v)", out, err)
	}
}

// hostEnv is a full host: shop over HTTP, Store file, bridge, fake CLI.
// Prepare starts the run's task on first use (a test convenience: the
// command's own Prepare opens the Store and refuses an existing task).
type hostEnv struct {
	t         *testing.T
	now       atomic.Uint64
	reg       contracts.RegistryConfig
	st        *store.Store
	h         *Host
	prof      policy.Profile
	noProfile map[string]bool
	prepared  atomic.Int32
}

func newHost(t *testing.T) *hostEnv {
	t.Helper()
	e := &hostEnv{t: t, noProfile: map[string]bool{}}
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
		products[o.ProductID] = toolbridge.Product{ID: o.ProductID, URL: o.URL, Scene: o.Scene}
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
	e.h = &Host{Registry: reg, Bridge: bridge, CLI: fake(t), BridgeCmd: []string{self(t), "mcp-bridge"},
		Limits: testLimits, Gate: Gate{CLIVersion: fakecli.InitVersion, AcceptAuth: []string{fakecli.AuthSource}},
		TraceDir: filepath.Join(t.TempDir(), "runs")}
	return e
}

func (e *hostEnv) session(task string) contracts.Session {
	reg, _ := contracts.NewRegistry(e.reg)
	tk, _, _ := reg.Binding(task)
	return contracts.Session{ConsumerID: tk.ConsumerID, AgentID: tk.AgentID, TaskID: task}
}

// prepare starts task unless it already exists.
func (e *hostEnv) prepare(task string) Prepare {
	return func() (*store.Store, error) {
		e.prepared.Add(1)
		sess := e.session(task)
		if _, err := e.st.Task(sess); err == nil {
			return e.st, nil
		}
		cred := policy.Credential{ConsumerID: sess.ConsumerID, ValidUntil: t0 + 86400, EvidenceMode: "fixture"}
		setup := store.TaskSetup{TaskID: task, Credential: &cred}
		if e.noProfile[task] {
			setup.ProfileAbsent, setup.BudgetInitial, setup.AssetID = true, "5000000", "fixture-usdc"
		} else {
			p := e.prof
			setup.Profile = &p
		}
		return e.st, e.st.StartTask(setup)
	}
}

func (e *hostEnv) runWith(ctx context.Context, task, scen string, prepare Prepare, env ...string) RunResult {
	e.t.Helper()
	e.h.Env = CLIEnv(os.Getenv, append(env, scenario(scen))...)
	res, err := e.h.Run(ctx, task, "Buy the brief.", prepare)
	if err != nil {
		e.t.Fatal(err)
	}
	return res
}

func (e *hostEnv) run(ctx context.Context, task, scen string, env ...string) RunResult {
	e.t.Helper()
	return e.runWith(ctx, task, scen, e.prepare(task), env...)
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

// refusedAfter: the run's records begin with first, and every record after
// it is a call the stop refused. A call the CLI sent just after the
// stopping line may reach the relay before the host has read that line; it
// is then admitted and cancelled by the stop (RUN_NOT_ACTIVE), never run.
func refusedAfter(calls []toolbridge.CallRecord, first string) bool {
	if len(calls) == 0 || calls[0].Tool+":"+calls[0].Outcome != first {
		return false
	}
	for _, c := range calls[1:] {
		if c.Outcome != "RUN_NOT_ACTIVE" {
			return false
		}
	}
	return true
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
	sess := e.session("task-a")
	tmp := t.TempDir()
	t.Setenv("TMPDIR", tmp) // after the bridge is up: the run's own directory lands here
	res := e.run(context.Background(), "task-a", "ask-mainline")
	if res.Report.Outcome != OK || res.StoreEnd != "" || !res.TaskStarted || e.prepared.Load() != 1 {
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
	if strings.Contains(string(trace), "Paid and downloaded") || strings.Contains(string(trace), "Buy the brief") ||
		!strings.Contains(string(trace), `"gate":"passed"`) || !strings.Contains(string(trace), `"model":"sonnet"`) ||
		!strings.Contains(string(trace), `"requested_model":"sonnet"`) ||
		!strings.Contains(string(trace), fmt.Sprintf(`"result_chars":%d`, len("Paid and downloaded the Korean market brief."))) {
		t.Errorf("trace: %s", trace)
	}
}

// genScenario writes a scenario to a temp file and returns its name for runWith.
func genScenario(t *testing.T, sc fakecli.Scenario) string {
	t.Helper()
	raw, _ := json.Marshal(sc)
	p := filepath.Join(t.TempDir(), "scenario.json")
	os.WriteFile(p, raw, 0o600)
	return strings.TrimSuffix(p, ".json") // scenario() adds the directory only to bare names
}

func badInit(over map[string]string) fakecli.Scenario {
	init := map[string]json.RawMessage{}
	for k, v := range over {
		init[k] = json.RawMessage(v)
	}
	return fakecli.Scenario{ManualInit: true, Steps: []fakecli.Step{
		{Do: "tool", Tool: "list_products"},
		{Do: "init", Init: init},
		{Do: "tool", Tool: "request_purchase", Args: json.RawMessage(`{"product_id":"kr-market"}`)},
		{Do: "result", Text: "done"},
	}}
}

// Stage A, A2: every init the gate refuses ends the run before Prepare, with
// no business tool run.
func TestHostInitGate(t *testing.T) {
	const tools3 = `["mcp__apa__list_products","mcp__apa__get_quote","mcp__apa__request_purchase"]`
	for name, c := range map[string]struct {
		sc   fakecli.Scenario
		code string
	}{
		"tool added":        {badInit(map[string]string{"tools": `["mcp__apa__list_products","mcp__apa__get_quote","mcp__apa__request_purchase","Bash"]`}), "tools"},
		"tool missing":      {badInit(map[string]string{"tools": `["mcp__apa__list_products","mcp__apa__get_quote"]`}), "tools"},
		"tool repeated":     {badInit(map[string]string{"tools": `["mcp__apa__list_products","mcp__apa__list_products","mcp__apa__get_quote"]`}), "tools"},
		"prefix only":       {badInit(map[string]string{"tools": `["mcp__apa__list_products","mcp__apa__get_quote","mcp__apa__request_purchases"]`}), "tools"},
		"extra server":      {badInit(map[string]string{"mcp_servers": `[{"name":"apa","status":"connected"},{"name":"gmail","status":"connected"}]`}), "mcp_servers"},
		"server not ready":  {badInit(map[string]string{"mcp_servers": `[{"name":"apa","status":"pending"}]`}), "mcp_servers"},
		"permission mode":   {badInit(map[string]string{"permissionMode": `"default"`}), "permission_mode"},
		"auth none":         {badInit(map[string]string{"apiKeySource": `"none"`}), "auth_unconfirmed"},
		"auth api key":      {badInit(map[string]string{"apiKeySource": `"ANTHROPIC_API_KEY"`}), "auth_unconfirmed"},
		"auth missing":      {badInit(map[string]string{"apiKeySource": `null`}), "auth_missing"},
		"auth not a string": {badInit(map[string]string{"apiKeySource": `true`}), "init_malformed"},
		"tools not strings": {badInit(map[string]string{"tools": `[1,2,3]`}), "init_malformed"},
		"servers object":    {badInit(map[string]string{"mcp_servers": `{"name":"apa"}`}), "init_malformed"},
		"plugins loaded":    {badInit(map[string]string{"plugins": `[{"name":"x","path":"/p"}]`}), "plugins"},
		// L1-b: the probe's plugin display candidates are not accepted either.
		"display-candidate plugin":      {badInit(map[string]string{"plugins": `[{"name":"cc-plugin-telemetry","path":"/p"}]`}), "plugins"},
		"all display-candidate plugins": {badInit(map[string]string{"plugins": `[{"name":"cc-plugin-agents-md","path":"/p"},{"name":"cc-plugin-telemetry","path":"/p"},{"name":"cc-plugin-plugin-authoring","path":"/p"}]`}), "plugins"},
		"built-in agent":                {badInit(map[string]string{"agents": `["Explore"]`}), "agents"}, // a display candidate is not accepted
		"agents listed":                 {badInit(map[string]string{"agents": `[{"name":"general-purpose","description":"","source":"built-in"}]`}), "agents"},
		"skills wrong type":             {badInit(map[string]string{"skills": `"all"`}), "init_malformed"},
		"other version":                 {badInit(map[string]string{"claude_code_version": `"2.1.288"`}), "cli_version"},
		"duplicate key": {fakecli.Scenario{ManualInit: true, Steps: []fakecli.Step{
			{Do: "tool", Tool: "list_products"},
			{Do: "raw", Text: `{"type":"system","subtype":"init","tools":` + tools3 + `,"tools":` + tools3 + `,"mcp_servers":[{"name":"apa","status":"connected"}],"permissionMode":"dontAsk","apiKeySource":"` + fakecli.AuthSource + `","claude_code_version":"` + fakecli.InitVersion + `"}` + "\n"},
			{Do: "tool", Tool: "list_products"}, {Do: "result", Text: "done"}}}, "ambiguous_json"}, // stopped by the reader, before the gate
	} {
		t.Run(name, func(t *testing.T) {
			e := newHost(t)
			res := e.runWith(context.Background(), "task-a", genScenario(t, c.sc), e.prepare("task-a"))
			want := ConfigMismatch
			if c.code == "ambiguous_json" {
				want = ParseError
			}
			if res.Report.Outcome != want || res.Report.Detail != c.code {
				t.Fatalf("outcome %s %q, want %s %q", res.Report.Outcome, res.Report.Detail, want, c.code)
			}
			if e.prepared.Load() != 0 || res.TaskStarted || len(res.Calls) != 0 || res.NotReady == 0 {
				t.Errorf("prepared %d, task %v, records %v, not ready %d", e.prepared.Load(), res.TaskStarted, res.Calls, res.NotReady)
			}
			if _, err := e.st.Task(e.session("task-a")); store.Code(err) != "OWNERSHIP_MISMATCH" {
				t.Errorf("task exists: %v", err)
			}
		})
	}
}

// Stage A: a missing init, a hook message before it, an init that arrives
// once the run is stopping, and a second init: none prepares or re-arms.
func TestHostInitSequence(t *testing.T) {
	e := newHost(t)
	res := e.run(context.Background(), "task-a", "init-missing")
	if res.Report.Outcome != ConfigMismatch || res.Report.Detail != "init_missing" || e.prepared.Load() != 0 || res.NotReady != 3 || len(res.Calls) != 0 {
		t.Fatalf("missing: %+v", res)
	}
	res = e.run(context.Background(), "task-a", "hook-before-init")
	if res.Report.Outcome != ConfigMismatch || res.Report.Detail != "hook_message" || e.prepared.Load() != 0 {
		t.Fatalf("hook before init: %+v", res)
	}

	// Late init: the run is stopped (cancelled) only once the fake shows it
	// ignores SIGTERM and has finished its MCP handshake: its first step,
	// savecap, writes the capability file, and steps run only after both.
	// It then prints a valid init 800 ms later, inside the 5 s grace before
	// SIGKILL. (A fixed 300 ms time limit raced the fake's own start under
	// -race: a SIGTERM before the ignore killed it and no init was read;
	// stopping before the handshake made the bridge refuse it, and the fake
	// exited; docs/demo-integration.md 8.4.)
	e.h.Limits.Total, e.h.Limits.Grace = 30*time.Second, 5*time.Second
	ready := filepath.Join(t.TempDir(), "cap")
	late := genScenario(t, fakecli.Scenario{ManualInit: true, IgnoreTerm: true, Steps: []fakecli.Step{{Do: "savecap"}, {Do: "sleep", MS: 800},
		{Do: "init"}, {Do: "tool", Tool: "request_purchase", Args: json.RawMessage(`{"product_id":"kr-market"}`)}, {Do: "hang"}}})
	ctx, cancel := context.WithCancel(context.Background())
	go func() {
		for end := time.Now().Add(20 * time.Second); time.Now().Before(end); time.Sleep(5 * time.Millisecond) {
			if _, err := os.Stat(ready); err == nil {
				break
			}
		}
		cancel()
	}()
	res = e.run(ctx, "task-a", late, fakecli.EnvCapFile+"="+ready)
	cancel()
	if res.Report.Outcome != Cancelled || e.prepared.Load() != 0 || res.TaskStarted || res.StoreEnd != "" || len(res.Calls) != 0 {
		t.Fatalf("late init: %+v prepared %d", res, e.prepared.Load())
	}
	trace, _ := os.ReadFile(res.Trace)
	if !strings.Contains(string(trace), `"gate":"passed"`) {
		t.Errorf("the late init was not read (the test would prove nothing): %s", trace)
	}
	e.h.Limits = testLimits

	// A second init after a legitimate purchase stops the run; the purchase
	// stays and the gate does not open again.
	sess := e.session("task-a")
	res = e.run(context.Background(), "task-a", "init-duplicate")
	if res.Report.Outcome != ConfigMismatch || res.Report.Detail != "init_duplicate" || !refusedAfter(res.Calls, "request_purchase:Ask") || res.StoreEnd != "" {
		t.Fatalf("duplicate: %+v", res)
	}
	if v := e.task(sess); len(v.Purchases) != 1 || v.Status != "awaiting_approval" {
		t.Fatalf("store after duplicate init: %+v", v)
	}
}

// A hook message after a legitimate purchase keeps the purchase and stops
// all later submissions.
func TestHostHookAfterPurchase(t *testing.T) {
	e := newHost(t)
	sess := e.session("task-a")
	res := e.run(context.Background(), "task-a", "hook-after-purchase")
	if res.Report.Outcome != ConfigMismatch || res.Report.Detail != "hook_message" || !refusedAfter(res.Calls, "request_purchase:Ask") {
		t.Fatalf("%+v %s", res.Report, outcomes(res.Calls))
	}
	if v := e.task(sess); len(v.Purchases) != 1 || v.Purchases[0].ApprovalClosed != "" {
		t.Fatalf("store: %+v", v)
	}
	if got := strings.Join(e.eventTypes(sess), ","); got != "TASK_STARTED,PURCHASE_PROPOSED,POLICY_DECIDED" {
		t.Errorf("events %s", got)
	}
}

// Init passed but Prepare failed: no tool runs, nothing is recorded.
func TestHostPrepareFails(t *testing.T) {
	e := newHost(t)
	failing := func() (*store.Store, error) { e.prepared.Add(1); return nil, errors.New("store unavailable") }
	res := e.runWith(context.Background(), "task-a", "ask-mainline", failing)
	if res.Report.Outcome != StoreUnavailable || res.PrepareError == nil || res.TaskStarted || len(res.Calls) != 0 || e.prepared.Load() != 1 {
		t.Fatalf("%+v", res)
	}
	if _, err := e.st.Task(e.session("task-a")); err == nil {
		t.Error("a task exists")
	}
}

// Init and cancel race: cancelled while Prepare runs, the run waits for
// Prepare, never arms, and records the cancel on the task Prepare made.
func TestHostCancelDuringPrepare(t *testing.T) {
	e := newHost(t)
	inside, release := make(chan struct{}), make(chan struct{})
	slow := func() (*store.Store, error) {
		close(inside)
		<-release
		return e.prepare("task-a")()
	}
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan RunResult)
	go func() { done <- e.runWith(ctx, "task-a", "ask-mainline", slow) }()
	<-inside
	cancel()
	select {
	case <-done:
		t.Fatal("the run ended while Prepare was still running")
	case <-time.After(500 * time.Millisecond):
	}
	close(release)
	res := <-done
	sess := e.session("task-a")
	if res.Report.Outcome != Cancelled || !res.TaskStarted || res.StoreEnd != "TASK_CANCELLED" || len(res.Calls) != 0 {
		t.Fatalf("%+v %s", res, outcomes(res.Calls))
	}
	if v := e.task(sess); len(v.Purchases) != 0 || v.Status != "cancelled" {
		t.Fatalf("store: %+v", v)
	}
}

// The assistant bound: 10 complete messages pass, the 11th stops the run;
// user, stream_event and system messages do not count.
func TestAssistantLimit(t *testing.T) {
	lim := testLimits
	lim.Assistant = 10
	if r := Run(context.Background(), runnerSpec(t, "assistant-10", lim)); r.Outcome != OK || r.Assistant != 10 {
		t.Errorf("10: %+v", r)
	}
	if r := Run(context.Background(), runnerSpec(t, "assistant-11", lim)); r.Outcome != TurnLimit || r.Detail != "assistant_messages" || r.Assistant != 11 {
		t.Errorf("11: %+v", r)
	}
}

// The trace keeps whitelisted diagnostics only: marks placed in the auth
// value, stderr, assistant text, prompt, environment and capability never
// reach it.
func TestHostTraceWhitelist(t *testing.T) {
	e := newHost(t)
	e.h.Gate.AcceptAuth = []string{"MARK-AUTH-7f3a"}
	sc := fakecli.Scenario{ManualInit: true, Steps: []fakecli.Step{
		{Do: "init", Init: map[string]json.RawMessage{"apiKeySource": json.RawMessage(`"MARK-AUTH-7f3a"`), "model": json.RawMessage(`"MARK-MODEL-7f3a echo of the prompt"`)}},
		{Do: "stderr", Text: "MARK-STDERR-7f3a"},
		{Do: "text", Text: "MARK-ASSISTANT-7f3a"},
		{Do: "tool", Tool: "request_purchase", Args: json.RawMessage(`{"product_id":"kr-market"}`)},
		{Do: "raw", Text: `{"type":"result","subtype":"success","is_error":false,"result":"ok","num_turns":1,"usage":{"input_tokens":5,"output_tokens":7,"note":"MARK-USAGE-7f3a"}}` + "\n"},
	}}
	e.h.Env = CLIEnv(func(k string) string {
		if k == "HOME" {
			return filepath.Join(t.TempDir(), "MARK-HOME-7f3a")
		}
		return os.Getenv(k)
	}, scenario(genScenario(t, sc)))
	res, err := e.h.Run(context.Background(), "task-a", "MARK-PROMPT-7f3a", e.prepare("task-a"))
	if err != nil || res.Report.Outcome != OK || outcomes(res.Calls) != "request_purchase:Ask" {
		t.Fatalf("%+v %v", res, err)
	}
	trace, _ := os.ReadFile(res.Trace)
	if strings.Contains(string(trace), "MARK-") || strings.Contains(string(trace), "7f3a") {
		t.Fatalf("a mark reached the trace: %s", trace)
	}
	if !strings.Contains(string(trace), `"usage":{"input_tokens":5,"output_tokens":7}`) || !strings.Contains(string(trace), `"model":"unrecognized"`) {
		t.Errorf("usage counts or model label missing: %s", trace)
	}

	// R1 (ca46cab review): a CLI that echoes the prompt in its result. The
	// prompt stays out of the trace; only the result's length is kept.
	echo := fakecli.Scenario{Steps: []fakecli.Step{
		{Do: "tool", Tool: "list_products"},
		{Do: "result", Text: "You asked: $PROMPT"},
	}}
	e2 := newHost(t)
	e2.h.Env = CLIEnv(os.Getenv, scenario(genScenario(t, echo)))
	const prompt = "MARK-PROMPT-91c2 Buy the Korean market brief for task-a."
	res, err = e2.h.Run(context.Background(), "task-a", prompt, e2.prepare("task-a"))
	if err != nil || res.Report.Outcome != OK || !strings.Contains(res.Report.Result, prompt) {
		t.Fatalf("the fake did not echo the prompt (the check below would prove nothing): %+v %v", res.Report, err)
	}
	trace, _ = os.ReadFile(res.Trace)
	if strings.Contains(string(trace), "MARK-PROMPT-91c2") || strings.Contains(string(trace), "Korean market brief") {
		t.Fatalf("the echoed prompt reached the trace: %s", trace)
	}
	if !strings.Contains(string(trace), fmt.Sprintf(`"result_chars":%d`, len("You asked: ")+len(prompt))) {
		t.Errorf("result length missing: %s", trace)
	}
}

// R2 (ca46cab review): the reader refuses any message whose objects name a
// member twice, exactly or up to case, before it is classified or checked;
// fields are then read by exact name only.
func TestObjectFields(t *testing.T) {
	for _, c := range []struct {
		line string
		err  error
	}{
		{`{"type":"system","subtype":"init"}`, nil},
		{`{"type":"system","subtype":"hook_started","subtype":"status"}`, errAmbiguous},
		{`{"type":"system","subtype":"status","Subtype":"hook_started"}`, errAmbiguous},
		{`{"permissionMode":"default","PermissionMode":"dontAsk"}`, errAmbiguous},
		{`{"kind":1,"\u212aind":2}`, errAmbiguous}, // Kelvin sign: encoding/json folds it to k
		{`{"a":[{"name":"apa","NAME":"x"}]}`, errAmbiguous},
		{`{"a":{"b":{"c":1,"c":2}}}`, errAmbiguous},
		{`{"a":[{"x":1},{"x":2}]}`, nil}, // the same name in sibling objects is fine
		{`["type"]`, errNotObject},
		{`{"type":"x"} trailing`, errNotObject},
	} {
		if _, err := objectFields([]byte(c.line)); err != c.err {
			t.Errorf("%s: %v, want %v", c.line, err, c.err)
		}
	}
	// Exact names only: a case variant alone is not the field.
	f, _ := objectFields([]byte(`{"PermissionMode":"dontAsk"}`))
	var perm *string
	if field(f, "permissionMode", &perm); perm != nil {
		t.Error("a case variant was read as permissionMode")
	}
}

func TestHostAmbiguousMessages(t *testing.T) {
	const tools3 = `["mcp__apa__list_products","mcp__apa__get_quote","mcp__apa__request_purchase"]`
	initWith := func(perm string) string {
		return `{"type":"system","subtype":"init","tools":` + tools3 + `,"mcp_servers":[{"name":"apa","status":"connected"}],` + perm +
			`,"apiKeySource":"` + fakecli.AuthSource + `","claude_code_version":"` + fakecli.InitVersion + `"}` + "\n"
	}
	purchase := fakecli.Step{Do: "tool", Tool: "request_purchase", Args: json.RawMessage(`{"product_id":"kr-market"}`)}
	for name, sc := range map[string]fakecli.Scenario{
		// The review's first case, and its exact-duplicate twin.
		"init case variant": {ManualInit: true, Steps: []fakecli.Step{{Do: "raw", Text: initWith(`"permissionMode":"default","PermissionMode":"dontAsk"`)}, purchase, {Do: "result", Text: "done"}}},
		"init duplicate":    {ManualInit: true, Steps: []fakecli.Step{{Do: "raw", Text: initWith(`"permissionMode":"default","permissionMode":"dontAsk"`)}, purchase, {Do: "result", Text: "done"}}},
		// The review's second case, before the init.
		"hook subtype twice": {ManualInit: true, Steps: []fakecli.Step{{Do: "raw", Text: `{"type":"system","subtype":"hook_started","subtype":"status"}` + "\n"}, {Do: "init"}, purchase, {Do: "result", Text: "done"}}},
	} {
		t.Run(name, func(t *testing.T) {
			e := newHost(t)
			res := e.runWith(context.Background(), "task-a", genScenario(t, sc), e.prepare("task-a"))
			if res.Report.Outcome != ParseError || res.Report.Detail != "ambiguous_json" || e.prepared.Load() != 0 || res.TaskStarted || len(res.Calls) != 0 {
				t.Fatalf("%+v prepared %d %s", res.Report, e.prepared.Load(), outcomes(res.Calls))
			}
			if _, err := e.st.Task(e.session("task-a")); err == nil {
				t.Error("a task was started")
			}
		})
	}
	// The review's second case after the init: the run stops at the
	// ambiguous hook line, and the purchase sent after it is not recorded.
	e := newHost(t)
	sc := fakecli.Scenario{Steps: []fakecli.Step{
		{Do: "tool", Tool: "list_products"},
		{Do: "raw", Text: `{"type":"system","subtype":"status","Subtype":"hook_started"}` + "\n"},
		{Do: "sleep", MS: 500},
		purchase,
		{Do: "result", Text: "done"},
	}}
	res := e.runWith(context.Background(), "task-a", genScenario(t, sc), e.prepare("task-a"))
	if res.Report.Outcome != ParseError || res.Report.Detail != "ambiguous_json" || !refusedAfter(res.Calls, "list_products:ok") {
		t.Fatalf("%+v %s", res.Report, outcomes(res.Calls))
	}
	if v := e.task(e.session("task-a")); len(v.Purchases) != 0 {
		t.Fatalf("a purchase after the ambiguous line: %+v", v)
	}
}

func TestHostForgedAndUnauthenticatedRequests(t *testing.T) {
	e := newHost(t)
	sess := e.session("task-a")
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
	sess := e.session("task-a")
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
	sess := e.session("task-a")
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
	sessB := e.session("task-b")
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
	e2.noProfile["task-a"] = true
	res = e2.run(context.Background(), "task-a", "hang")
	v = e2.task(e2.session("task-a"))
	if outcomes(res.Calls) != "request_purchase:Deny" || res.StoreEnd != "TASK_TIMED_OUT" || v.Status != "denied" || v.DenyReason != "CONFIG_MISSING" {
		t.Fatalf("deny: %+v %+v", res, v)
	}

	// A timeout before any task exists records nothing: no task is created
	// to note a failure.
	e3 := newHost(t)
	e3.h.Limits.Total, e3.h.Limits.Grace = 300*time.Millisecond, 500*time.Millisecond
	res = e3.run(context.Background(), "task-a", "init-late")
	if res.Report.Outcome != Timeout || res.TaskStarted || res.StoreEnd != "" || res.EndError != "" {
		t.Fatalf("timeout before a task: %+v", res)
	}
}

func TestHostAskExpires(t *testing.T) {
	e := newHost(t)
	sess := e.session("task-a")
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
	sessA := e.session("task-a")
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
	if strings.Contains(string(trace), string(capability)) || strings.Contains(string(trace), "the capability is") {
		t.Fatalf("trace: %s", trace)
	}
	// Late: after the run, the capability opens nothing.
	args := json.RawMessage(`{"product_id":"kr-market"}`)
	r, err := toolbridge.Dial(context.Background(), e.h.Bridge.SocketPath(), string(capability), "request_purchase", args)
	if err != nil || !r.IsError || !strings.Contains(r.Content[0].Text, "RUN_NOT_ACTIVE") {
		t.Fatalf("late call: %+v %v", r, err)
	}
	// Cross-run: while B's run is active, A's old capability still opens nothing.
	sessB := e.session("task-b")
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
	e.h.CLI = filepath.Join(t.TempDir(), "claude")
	res := e.run(context.Background(), "task-a", "ask-mainline")
	if res.Report.Outcome != NotInstalled || res.StoreEnd != "" || res.TaskStarted || e.prepared.Load() != 0 {
		t.Fatalf("%+v", res)
	}
	if _, err := e.h.Run(context.Background(), "task-unknown", "x", e.prepare("task-unknown")); err == nil {
		t.Error("unknown task ran")
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

// R1 follow-up (59da5d9 review): a CLI puts the run's own capability, or a
// name-shaped secret, where the trace keeps names: the init's model and a
// well-formed product_id. Only listed models and offered products are
// recorded, so neither reaches the trace.
func TestHostTraceKeepsOnlyListedNames(t *testing.T) {
	for name, c := range map[string]struct {
		model, product, secret string
	}{
		"capability as model":      {`"$CAPABILITY"`, "kr-market", ""},
		"name-shaped secret model": {`"MARKSECRET9f3a"`, "kr-market", "MARKSECRET9f3a"},
		"capability as product_id": {`"sonnet"`, "$CAPABILITY", ""},
	} {
		t.Run(name, func(t *testing.T) {
			e := newHost(t)
			capFile := filepath.Join(t.TempDir(), "cap")
			sc := fakecli.Scenario{ManualInit: true, Steps: []fakecli.Step{
				{Do: "savecap"},
				{Do: "init", Init: map[string]json.RawMessage{"model": json.RawMessage(c.model)}},
				{Do: "tool", Tool: "request_purchase", Args: json.RawMessage(`{"product_id":"` + c.product + `"}`)},
				{Do: "result", Text: "done"},
			}}
			res := e.runWith(context.Background(), "task-a", genScenario(t, sc), e.prepare("task-a"), fakecli.EnvCapFile+"="+capFile)
			capability, _ := os.ReadFile(capFile)
			if len(capability) != 64 || res.Report.Outcome != OK || !res.TaskStarted || len(res.Calls) != 1 {
				t.Fatalf("setup: cap %d bytes, %+v %s", len(capability), res.Report, outcomes(res.Calls))
			}
			if c.product == "$CAPABILITY" && (res.Calls[0].Outcome != "UNKNOWN_PRODUCT" || res.Calls[0].ProductID != "") {
				t.Errorf("record for an unoffered product: %+v", res.Calls[0])
			}
			if c.model != `"sonnet"` && res.Model != "unrecognized" {
				t.Errorf("model kept as %q", res.Model)
			}
			trace, _ := os.ReadFile(res.Trace)
			secret := cmpSecret(c.secret, string(capability))
			if strings.Contains(string(trace), secret) || strings.Contains(string(trace), string(capability)) {
				t.Fatalf("a CLI-chosen secret reached the trace: %s", trace)
			}
			if !strings.Contains(string(trace), `"requested_model":"sonnet"`) {
				t.Errorf("requested model missing: %s", trace)
			}
		})
	}
}

func cmpSecret(secret, capability string) string {
	if secret == "" {
		return capability
	}
	return secret
}
