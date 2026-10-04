package buyer

import (
	"context"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/SIMPLYBOYS/agent-payment-authority/internal/buyer/fakecli"
)

// probeEnv runs the C0 probe against the fake, with a scratch HOME and the
// fake's stdin byte counter.
type probeEnv struct {
	t     *testing.T
	stdin string // file the fake keeps its received stdin byte count in
	msgs  string // file the fake keeps its count of complete stdin lines in
	cap   string // file the fake saves the run's capability to (savecap)
	spec  ProbeSpec
}

func newProbe(t *testing.T, scen string, total time.Duration) *probeEnv {
	t.Helper()
	dir := t.TempDir()
	p := &probeEnv{t: t, stdin: filepath.Join(dir, "stdin-count"), msgs: filepath.Join(dir, "stdin-msgs"), cap: filepath.Join(dir, "cap")}
	home := filepath.Join(dir, "home")
	getenv := func(k string) string {
		if k == "HOME" {
			return home
		}
		return os.Getenv(k)
	}
	lim := testLimits
	lim.Total = total
	p.spec = ProbeSpec{CLI: fake(t), BridgeCmd: []string{self(t), "mcp-bridge"},
		Env: CLIEnv(getenv, scenario(scen), fakecli.EnvStdinFile+"="+p.stdin, fakecli.EnvStdinMsgFile+"="+p.msgs,
			fakecli.EnvCapFile+"="+p.cap),
		Limits: lim, CLIVersion: fakecli.InitVersion, TraceDir: filepath.Join(dir, "runs")}
	return p
}

func (p *probeEnv) run(ctx context.Context) (ProbeResult, time.Duration) {
	p.t.Helper()
	start := time.Now()
	res, err := Probe(ctx, p.spec)
	if err != nil {
		p.t.Fatal(err)
	}
	return res, time.Since(start)
}

// stdinBytes is the fake's own count of the stdin bytes it received.
func (p *probeEnv) stdinBytes() string {
	p.t.Helper()
	raw, err := os.ReadFile(p.stdin)
	if err != nil {
		p.t.Fatalf("the fake never started its stdin counter: %v", err)
	}
	return string(raw)
}

func TestProbeArgv(t *testing.T) {
	a := ProbeArgv("/cfg.json")
	if !slices.Equal(a[:3], []string{"-p", "--input-format", "stream-json"}) || !slices.Equal(a[3:], Argv("x", "/cfg.json")[2:]) {
		t.Fatalf("argv %q", a)
	}
	// No prompt: every argument is a flag or a flag's value.
	values := map[string]bool{"--input-format": true, "--output-format": true, "--mcp-config": true, "--tools": true, "--permission-mode": true,
		"--permission-prompts": true, "--allowedTools": true, "--model": true, "--effort": true}
	for i := 0; i < len(a); i++ {
		if values[a[i]] {
			i++
		} else if !strings.HasPrefix(a[i], "-") {
			t.Errorf("positional argument %q", a[i])
		}
	}
}

// The fake that sends its init without waiting: observed, stopped at once,
// no tool run, zero stdin bytes.
func TestProbeInitFirst(t *testing.T) {
	p := newProbe(t, "probe-init-first", 10*time.Second)
	res, took := p.run(context.Background())
	if res.Status != ProbeObserved || res.Code != "init_observed" || took > 5*time.Second {
		t.Fatalf("%+v after %v", res, took)
	}
	if p.stdinBytes() != "0" || res.StdinWritten != 0 || res.Mode != ModeC0 || res.InputStatus != InputNone || res.InputSize != 0 {
		t.Errorf("C0 input: fake %s bytes, host %+v", p.stdinBytes(), res)
	}
	if res.CallsExecuted != 0 || res.CallsRefused < 1 {
		t.Errorf("business calls: executed %d, refused %d", res.CallsExecuted, res.CallsRefused)
	}
	o := res.Observation
	if o == nil || o.ToolsCount != 3 || o.ToolsExpected != 3 || o.MCPServers != 1 || o.APAServer != "connected" || o.PermissionMode != "dontAsk" ||
		o.CLIVersion != "approved" || o.Model != "sonnet" || o.AuthSource != "other" || o.AuthSourceLen != len(fakecli.AuthSource) || o.Fields["cwd"] != "string" {
		t.Errorf("observation %+v", o)
	}
	if res.Usage != "unknown" {
		t.Errorf("usage %q", res.Usage)
	}
}

// The fake that waits for a user message: none is sent, so no init; the
// probe ends at its wait limit and does not fall back to anything.
func TestProbeWaitsForInput(t *testing.T) {
	p := newProbe(t, "probe-wait-input", 1500*time.Millisecond)
	res, took := p.run(context.Background())
	if res.Status != ProbeNoInit || res.Code != "no_init_before_input" || res.Observation != nil || took > 6*time.Second ||
		res.Mode != ModeC0 || res.InputStatus != InputNone || res.StdinWritten != 0 {
		t.Fatalf("%+v after %v", res, took)
	}
	if p.stdinBytes() != "0" || res.CallsExecuted != 0 {
		t.Errorf("stdin %s, executed %d", p.stdinBytes(), res.CallsExecuted)
	}
}

// Positive control for the counter and the wait: given input, the same
// fake reports its byte count and goes on to the init.
func TestFakeCountsStdin(t *testing.T) {
	sc := genScenario(t, fakecli.Scenario{ManualInit: true, Steps: []fakecli.Step{{Do: "wait_input"}, {Do: "init"}, {Do: "exit"}}})
	count := filepath.Join(t.TempDir(), "count")
	s := runnerSpec(t, sc, testLimits, fakecli.EnvStdinFile+"="+count)
	cmd := exec.Command(s.Path, s.Args...)
	cmd.Dir, cmd.Env, cmd.Stdin = s.Dir, s.Env, strings.NewReader("abc")
	out, err := cmd.Output()
	if err != nil {
		t.Fatal(err)
	}
	raw, _ := os.ReadFile(count)
	if string(raw) != "3" || !strings.Contains(string(out), `"subtype":"init"`) {
		t.Fatalf("count %q, out %s", raw, out)
	}
}

func manual(steps ...fakecli.Step) fakecli.Scenario {
	return fakecli.Scenario{ManualInit: true, Steps: steps}
}

// Every other ending is a failure with a fixed code; none sends input or
// runs a tool, and each ends within its limits.
func TestProbeFailures(t *testing.T) {
	const tools3 = `["mcp__apa__list_products","mcp__apa__get_quote","mcp__apa__request_purchase"]`
	ambiguousInit := `{"type":"system","subtype":"init","tools":` + tools3 + `,"mcp_servers":[{"name":"apa","status":"connected"}],` +
		`"permissionMode":"default","PermissionMode":"dontAsk","claude_code_version":"` + fakecli.InitVersion + `"}` + "\n"
	pidFile := filepath.Join(t.TempDir(), "pids")
	for name, c := range map[string]struct {
		sc   fakecli.Scenario
		code string
	}{
		"old init version":      {manual(fakecli.Step{Do: "init", Init: map[string]json.RawMessage{"claude_code_version": json.RawMessage(`"2.1.287"`)}}, fakecli.Step{Do: "hang"}), "init_cli_version"},
		"ambiguous init":        {manual(fakecli.Step{Do: "raw", Text: ambiguousInit}, fakecli.Step{Do: "hang"}), "ambiguous_json"},
		"nested ambiguity":      {manual(fakecli.Step{Do: "raw", Text: `{"type":"system","subtype":"init","mcp_servers":[{"name":"apa","NAME":"x"}]}` + "\n"}, fakecli.Step{Do: "hang"}), "ambiguous_json"},
		"hook before init":      {manual(fakecli.Step{Do: "raw", Text: `{"type":"system","subtype":"hook_started","hook_event":"SessionStart","hook_name":"h"}` + "\n"}, fakecli.Step{Do: "init"}, fakecli.Step{Do: "hang"}), "hook_message"},
		"assistant before init": {manual(fakecli.Step{Do: "text", Text: "hello"}, fakecli.Step{Do: "init"}, fakecli.Step{Do: "hang"}), "unexpected_message"},
		"result before init":    {manual(fakecli.Step{Do: "result", Text: "done"}, fakecli.Step{Do: "hang"}), "unexpected_message"},
		"not json":              {manual(fakecli.Step{Do: "raw", Text: "init ok\n"}, fakecli.Step{Do: "hang"}), "parse_error"},
		"output flood":          {manual(fakecli.Step{Do: "flood", Bytes: 200000}, fakecli.Step{Do: "hang"}), "output_overflow"},
		"stderr flood":          {manual(fakecli.Step{Do: "stderr", Bytes: 300000}, fakecli.Step{Do: "hang"}), "output_overflow"},
		"crash":                 {manual(fakecli.Step{Do: "exit", Code: 3}), "nonzero_exit"},
		"pipe holder":           {manual(fakecli.Step{Do: "holder"}, fakecli.Step{Do: "exit"}), "missing_result"},
	} {
		t.Run(name, func(t *testing.T) {
			p := newProbe(t, genScenario(t, c.sc), 5*time.Second)
			p.spec.Env = append(p.spec.Env, fakecli.EnvPIDFile+"="+pidFile)
			res, took := p.run(context.Background())
			if res.Status != ProbeFailed || res.Code != c.code || took > 8*time.Second {
				t.Fatalf("%s %s (%s %s) after %v", res.Status, res.Code, res.Outcome, res.Detail, took)
			}
			if p.stdinBytes() != "0" || res.CallsExecuted != 0 {
				t.Errorf("stdin %s, executed %d", p.stdinBytes(), res.CallsExecuted)
			}
			if (c.code == "init_cli_version") != (res.Observation != nil) {
				t.Errorf("observation %+v", res.Observation)
			}
			if c.code == "init_cli_version" && res.Observation.CLIVersion != "other" {
				t.Errorf("version shown as %q", res.Observation.CLIVersion)
			}
		})
	}
	for _, pid := range pids(t, pidFile) {
		if !gone(pid, 3*time.Second) {
			t.Errorf("pipe holder %d survived", pid)
		}
	}
}

func TestProbeCancel(t *testing.T) {
	p := newProbe(t, genScenario(t, manual(fakecli.Step{Do: "hang"})), 10*time.Second)
	ctx, cancel := context.WithCancel(context.Background())
	time.AfterFunc(300*time.Millisecond, cancel)
	res, took := p.run(ctx)
	if res.Status != ProbeFailed || res.Code != "cancelled" || took > 5*time.Second || p.stdinBytes() != "0" {
		t.Fatalf("%+v after %v", res, took)
	}
	// Cancelled before it starts: the CLI is not run at all.
	p2 := newProbe(t, "probe-init-first", 10*time.Second)
	done, stop := context.WithCancel(context.Background())
	stop()
	if _, err := Probe(done, p2.spec); err == nil {
		t.Fatal("a cancelled probe started")
	}
	if _, err := os.Stat(p2.stdin); !os.IsNotExist(err) {
		t.Error("the CLI ran after the cancel")
	}
}

// Unknown or mistyped values are projected to fixed labels and still count
// as an observation; a display candidate for the auth source is shown but
// accepts nothing.
func TestProbeUnknownValues(t *testing.T) {
	sc := manual(fakecli.Step{Do: "init", Init: map[string]json.RawMessage{
		"permissionMode": json.RawMessage(`"superMode"`), "mcp_servers": json.RawMessage(`[{"name":"apa","status":"pending"},{"name":"x","status":"connected"}]`),
		"tools": json.RawMessage(`7`), "model": json.RawMessage(`5`), "plugins": json.RawMessage(`"all"`), "skills": json.RawMessage(`[]`),
		"agents": json.RawMessage(`[{"name":"a"},{"name":"b"}]`), "apiKeySource": json.RawMessage(`"keychain"`), "brand_new": json.RawMessage(`1`),
	}}, fakecli.Step{Do: "hang"})
	p := newProbe(t, genScenario(t, sc), 5*time.Second)
	res, _ := p.run(context.Background())
	o := res.Observation
	if res.Status != ProbeObserved || o == nil {
		t.Fatalf("%+v", res)
	}
	if o.PermissionMode != "other" || o.APAServer != "other" || o.MCPServers != 2 || o.Fields["tools"] != "number" || o.ToolsCount != 0 ||
		o.Model != "not_string" || o.Plugins != nil || *o.Skills != 0 || *o.Agents != 2 || o.AuthSource != "keychain" || o.AuthSourceLen != 0 || o.ExtraMembers != 1 {
		t.Errorf("observation %+v", o)
	}
}

// Marks and the run's own capability, placed in every string the init can
// carry, reach neither the result nor the trace.
func TestProbeNoLeak(t *testing.T) {
	const mark = "MARKPROBE7d1e"
	sc := manual(fakecli.Step{Do: "savecap"}, fakecli.Step{Do: "init", Init: map[string]json.RawMessage{
		"model": json.RawMessage(`"$CAPABILITY"`), "apiKeySource": json.RawMessage(`"` + mark + `"`),
		"tools":          json.RawMessage(`["mcp__apa__list_products","` + mark + `","$CAPABILITY"]`),
		"mcp_servers":    json.RawMessage(`[{"name":"apa","status":"` + mark + `"},{"name":"$CAPABILITY","status":"connected"}]`),
		"permissionMode": json.RawMessage(`"$CAPABILITY"`), "agents": json.RawMessage(`[{"name":"` + mark + `","source":"$CAPABILITY"},"$CAPABILITY","` + mark + `"]`),
		"skills":  json.RawMessage(`[{"name":"$CAPABILITY"}]`),
		"plugins": json.RawMessage(`[{"name":"` + mark + `","path":"/$CAPABILITY/` + mark + `"},{"name":"$CAPABILITY","path":"$CAPABILITY"}]`),
		"cwd":     json.RawMessage(`"/` + mark + `"`), "session_id": json.RawMessage(`"$CAPABILITY"`), mark: json.RawMessage(`"$CAPABILITY"`),
	}}, fakecli.Step{Do: "hang"})
	p := newProbe(t, genScenario(t, sc), 5*time.Second)
	res, _ := p.run(context.Background())
	capability, _ := os.ReadFile(p.cap)
	if len(capability) != 64 || res.Status != ProbeObserved {
		t.Fatalf("setup: cap %d bytes, %+v", len(capability), res)
	}
	out, _ := json.Marshal(res)
	trace, _ := os.ReadFile(res.Trace)
	for _, s := range []string{string(out), string(trace)} {
		if strings.Contains(s, mark) || strings.Contains(s, string(capability)) {
			t.Fatalf("leak: %s", s)
		}
	}
	if o := res.Observation; len(o.PluginEntries) != 2 || len(o.AgentEntries) != 3 || o.PluginEntries[1].Name != "other" || o.PluginEntries[1].Path != "relative" {
		t.Errorf("entries: %+v %+v", o.PluginEntries, o.AgentEntries) // the marks were seen, as lengths and classes only
	}
	if res.Observation.Model != "other" || res.Observation.AuthSource != "other" || res.Observation.ExtraMembers != 1 {
		t.Errorf("observation %+v", res.Observation)
	}
}

// api_retry before the init stops the probe at once with a fixed code: a
// later init does not turn it into an observation, and it is not left to
// the wait limit. The retry's error text is never kept.
func TestProbeAPIRetry(t *testing.T) {
	const mark = "MARKRETRY5c0a"
	retry := func(e string) fakecli.Step { return fakecli.Step{Do: "retry", Error: e} }
	for name, c := range map[string]struct {
		sc   fakecli.Scenario
		code string
	}{
		"auth, then init":       {manual(retry("authentication_failed"), fakecli.Step{Do: "init"}, fakecli.Step{Do: "hang"}), "api_retry_auth"},
		"auth, no init":         {manual(retry("authentication_failed"), fakecli.Step{Do: "hang"}), "api_retry_auth"},
		"rate limit, then init": {manual(retry("rate_limit"), fakecli.Step{Do: "init"}, fakecli.Step{Do: "hang"}), "api_retry_rate_limit"},
		"rate limit, no init":   {manual(retry("rate_limit"), fakecli.Step{Do: "hang"}), "api_retry_rate_limit"},
		"unknown error text":    {manual(retry(mark), fakecli.Step{Do: "init"}, fakecli.Step{Do: "hang"}), "api_retry"},
	} {
		t.Run(name, func(t *testing.T) {
			p := newProbe(t, genScenario(t, c.sc), 10*time.Second)
			res, took := p.run(context.Background())
			if res.Status != ProbeFailed || res.Code != c.code || res.Observation != nil || took > 3*time.Second {
				t.Fatalf("%s %s observation %v after %v", res.Status, res.Code, res.Observation, took)
			}
			if p.stdinBytes() != "0" || res.CallsExecuted != 0 {
				t.Errorf("stdin %s, executed %d", p.stdinBytes(), res.CallsExecuted)
			}
			out, _ := json.Marshal(res)
			trace, _ := os.ReadFile(res.Trace)
			if strings.Contains(string(out)+string(trace), mark) {
				t.Fatalf("retry text kept: %s %s", out, trace)
			}
		})
	}
}

// A run cancelled before its process starts starts nothing.
func TestRunnerCancelledBeforeStart(t *testing.T) {
	var starts atomic.Int32
	started = func() { starts.Add(1) }
	defer func() { started = func() {} }()
	ran := filepath.Join(t.TempDir(), "ran")
	cli := script(t, "touch "+ran+"\necho fake-claude")
	if _, err := Version(context.Background(), cli, CLIEnv(os.Getenv), testLimits); err != nil || starts.Load() != 1 {
		t.Fatalf("control: %v, starts %d", err, starts.Load()) // the counter sees a start
	}
	os.Remove(ran)
	starts.Store(0)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if r := Run(ctx, Spec{Path: cli, Dir: t.TempDir(), Env: CLIEnv(os.Getenv), Limits: testLimits}); r.Outcome != Cancelled || r.ExitCode != -1 {
		t.Errorf("run: %+v", r)
	}
	if _, err := Version(ctx, cli, CLIEnv(os.Getenv), testLimits); err == nil {
		t.Error("version check passed")
	}
	if starts.Load() != 0 {
		t.Fatalf("%d processes were started", starts.Load())
	}
	time.Sleep(200 * time.Millisecond)
	if _, err := os.Stat(ran); !os.IsNotExist(err) {
		t.Fatal("the process ran")
	}
}

// plugins and agents, element by element: a name is printed only when it
// is on its display list, a plugin path only as a class; anything else is
// a fixed label, and the per-element lists stop at maxEntries.
func TestProbeEntries(t *testing.T) {
	p := newProbe(t, "probe-init-first", 5*time.Second)
	var home string
	for _, kv := range p.spec.Env {
		if v, ok := strings.CutPrefix(kv, "HOME="); ok {
			home = v
		}
	}
	plugins, _ := json.Marshal([]any{
		map[string]any{"name": "mkt-plugin", "path": home + "/.claude/plugins/cache/mkt/mkt-plugin/1.0.0"},
		map[string]any{"name": "synced-plugin", "path": home + "/.claude/plugins/synced/synced-plugin"},
		map[string]any{"name": "dir-plugin", "path": home + "/.claude/skills/dir-plugin"},
		map[string]any{"name": "rel", "path": "plugins/rel"},
		map[string]any{"name": 5, "path": 7},
		map[string]any{"path": "/opt/elsewhere"},
		"bare-string", 3,
	})
	many := make([]string, maxEntries+4)
	for i := range many {
		many[i] = "Plan"
	}
	manyRaw, _ := json.Marshal(many)
	for name, c := range map[string]struct {
		init    map[string]json.RawMessage
		plugins []EntryObservation
		agents  []EntryObservation
	}{
		"mixed": {map[string]json.RawMessage{"plugins": plugins, "agents": json.RawMessage(`["Explore","general-purpose","my-agent",4,{"name":"Plan","description":"free text"}]`)},
			[]EntryObservation{{"other", 10, "home_plugins_cache"}, {"other", 13, "home_plugins_synced"}, {"other", 10, "home_skills"},
				{"other", 3, "relative"}, {"not_string", 0, "not_string"}, {"absent", 0, "other"}, {"other", 11, "absent"}, {Name: "malformed"}},
			[]EntryObservation{{Name: "Explore"}, {Name: "general-purpose"}, {"other", 8, ""}, {Name: "malformed"}, {Name: "Plan"}}},
		"empty":       {map[string]json.RawMessage{"plugins": json.RawMessage(`[]`), "agents": json.RawMessage(`[]`)}, []EntryObservation{}, []EntryObservation{}},
		"absent":      {map[string]json.RawMessage{"plugins": nil, "agents": nil}, nil, nil},
		"wrong types": {map[string]json.RawMessage{"plugins": json.RawMessage(`"all"`), "agents": json.RawMessage(`{"name":"Plan"}`)}, nil, nil},
		"over the cap": {map[string]json.RawMessage{"agents": manyRaw, "plugins": json.RawMessage(`[]`)}, []EntryObservation{},
			slices.Repeat([]EntryObservation{{Name: "Plan"}}, maxEntries)},
	} {
		p.spec.Env = append(slices.DeleteFunc(slices.Clone(p.spec.Env), func(kv string) bool { return strings.HasPrefix(kv, fakecli.EnvScenario+"=") }),
			scenario(genScenario(t, manual(fakecli.Step{Do: "init", Init: c.init}, fakecli.Step{Do: "hang"}))))
		res, _ := p.run(context.Background())
		o := res.Observation
		if res.Status != ProbeObserved || o == nil {
			t.Fatalf("%s: %+v", name, res)
		}
		if !slices.Equal(o.PluginEntries, c.plugins) || !slices.Equal(o.AgentEntries, c.agents) {
			t.Errorf("%s:\nplugins %+v\nagents  %+v", name, o.PluginEntries, o.AgentEntries)
		}
		if name == "over the cap" && (o.Agents == nil || *o.Agents != maxEntries+4) {
			t.Errorf("the count is not exact: %v", o.Agents)
		}
	}
}

// pathClass reads only the path's text, and a root is matched whole.
func TestPathClass(t *testing.T) {
	r := pathRoots{home: "/h/u", work: "/tmp/apa-probe-1/work"}
	for p, want := range map[string]string{
		"/h/u/.claude/plugins/synced/a":        "home_plugins_synced",
		"/h/u/.claude/plugins/cache/m/a/1":     "home_plugins_cache",
		"/h/u/.claude/plugins/data/a":          "home_plugins_other",
		"/h/u/.claude/plugins":                 "home_plugins_other",
		"/h/u/.claude/pluginsX/a":              "other",
		"/h/u/.claude/plugins/../../x":         "other",
		"/h/u/.claude/plugins/cache/../synced": "home_plugins_synced",
		"/h/u/.claude/skills/p":                "home_skills",
		"/tmp/apa-probe-1/work/p":              "work_dir",
		"/tmp/apa-probe-1/workshop":            "other",
		"h/u/.claude/plugins/cache/a":          "relative",
		"":                                     "relative",
	} {
		if got := pathClass(p, r); got != want {
			t.Errorf("%q: %s, want %s", p, got, want)
		}
	}
	if got := pathClass("/.claude/plugins/cache/a", pathRoots{}); got != "other" {
		t.Errorf("no HOME: %s", got)
	}
}
