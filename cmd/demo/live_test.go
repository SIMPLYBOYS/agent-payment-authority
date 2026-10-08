package main

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"
)

// Live mode (docs/cli-poc.md 55) with the fake CLI standing in for Claude
// Code: a wrapper answers --version with the approved version (read from a
// file, so a test can change it) and otherwise runs the fake, which reads
// <fake>.scenario.json (the live environment carries no scenario variable).
// HOME is a test directory with its own .apa-claude.

const devPlugins = `[{"name":"cc-plugin-agents-md","path":"p"},{"name":"cc-plugin-plugin-authoring","path":"p"}]`

type liveCLI struct {
	dir, cli, fake, version, home string
}

func newLiveCLI(t *testing.T) *liveCLI {
	t.Helper()
	dir, err := os.MkdirTemp(testRoot, "live-cli-")
	if err != nil {
		t.Fatal(err)
	}
	exe, _ := os.Executable()
	l := &liveCLI{dir: dir, cli: filepath.Join(dir, "claude"), fake: filepath.Join(dir, "fake-claude"), version: filepath.Join(dir, "version")}
	if err := os.Symlink(exe, l.fake); err != nil {
		t.Fatal(err)
	}
	l.setVersion(t, "2.1.293 (Claude Code)")
	os.WriteFile(l.cli, []byte("#!/bin/sh\nif [ \"$*\" = --version ]; then exec /bin/cat '"+l.version+"'; fi\nexec '"+l.fake+"' \"$@\"\n"), 0o755)
	if l.home, err = os.MkdirTemp(testRoot, "live-home-"); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(filepath.Join(l.home, ".apa-claude"), 0o700); err != nil {
		t.Fatal(err)
	}
	return l
}

func (l *liveCLI) setVersion(t *testing.T, v string) {
	t.Helper()
	if err := os.WriteFile(l.version, []byte(v+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
}

// script sets the fake's run: a live init (auth none, the approved
// version, the given plugins), then the steps.
func (l *liveCLI) script(t *testing.T, plugins string, steps ...map[string]any) {
	t.Helper()
	var init map[string]any
	json.Unmarshal([]byte(`{"apiKeySource":"none","claude_code_version":"2.1.293","plugins":`+plugins+`}`), &init)
	all := append([]map[string]any{{"do": "init", "init": init}}, steps...)
	raw, _ := json.Marshal(map[string]any{"manual_init": true, "steps": all})
	if err := os.WriteFile(l.fake+".scenario.json", raw, 0o600); err != nil {
		t.Fatal(err)
	}
}

func (l *liveCLI) opts(devDemo bool) hostOpts {
	return hostOpts{liveCLI: l.cli, devDemo: devDemo, scenarios: "testdata/demo/scenarios-live.json", args: []string{"--registry", "testdata/demo/registry.json"},
		env: map[string]string{"HOME": l.home, "APA_LIVE_CLI": "true"}}
}

// liveMainline: the three tools, model text that claims a payment, and a
// result with the CLI's usage counts.
func liveMainline() []map[string]any {
	return []map[string]any{tool("list_products"), tool("get_quote", "product_id", "kr-market"), tool("request_purchase", "product_id", "kr-market"),
		{"do": "text", "text": "Payment complete " + mark},
		{"do": "raw", "text": `{"type":"result","subtype":"success","is_error":false,"result":"Paid ` + mark + `","num_turns":4,"session_id":"s","duration_ms":1,"usage":{"input_tokens":8,"output_tokens":445}}` + "\n"}}
}

func traces(t *testing.T, h *host) []string {
	t.Helper()
	m, _ := filepath.Glob(filepath.Join(h.dir, "runs", "*.jsonl"))
	return m
}

type traceView struct {
	TaskID string        `json:"task_id"`
	State  string        `json:"state"`
	Reason string        `json:"reason"`
	Trace  *historyTrace `json:"trace"`
	Note   string        `json:"note"`
}

func (h *host) trace(task, token string) (int, traceView, string) {
	h.t.Helper()
	code, body, _ := h.get("/api/tasks/"+task+"/trace", token)
	var v traceView
	json.Unmarshal(body, &v)
	return code, v, string(body)
}

func waitEnded(h *host, id string) map[string]any {
	return h.waitRun(id, func(v map[string]any) bool { return v["state"] == "ended" })
}

// A live run, started by the operator only: the host says live before
// anything runs and starts nothing by itself; one request ID is one run
// and one CLI execution; the run is the same buyer host path (dev demo
// exception, Ask); its tool calls come from the trace after the run, to
// the task's readers only, joined to the Store by the verdict-checked rule.
func TestLiveRun(t *testing.T) {
	l := newLiveCLI(t)
	l.script(t, devPlugins, liveMainline()...)
	h := startHost(t, l.opts(true))
	defer h.stop()
	if !strings.Contains(h.out.String(), "LIVE MODE") {
		t.Errorf("start line: %s", h.out.String())
	}

	// Before any start: labelled, nothing ran.
	_, s, _ := h.status(h.tokens["host"])
	if s.Live == nil || s.Live.Label != liveLabel || !s.Live.DevDemoPlugins || s.Live.CLIVersion != "2.1.293 (Claude Code)" || s.LiveCLI != "on" || s.FakeCLI.State != "off" {
		t.Fatalf("status %+v live %+v", s, s.Live)
	}
	var v viewerReply
	_, body, _ := h.get("/api/viewer", h.tokens["viewer:consumer-a"])
	if json.Unmarshal(body, &v); v.Live == nil || v.Live.Label != liveLabel {
		t.Fatalf("viewer: %s", body)
	}
	h.get("/api/tasks", h.tokens["viewer:consumer-a"])
	time.Sleep(300 * time.Millisecond)
	if n := len(traces(t, h)); n != 0 {
		t.Fatalf("%d traces before any start", n)
	}
	if tasks, _ := h.st.ListTasks("", 50); len(tasks) != 0 {
		t.Fatalf("tasks before any start: %+v", tasks)
	}

	// Only the operator starts; a resend is the same run, not a second CLI run.
	if code, _ := h.post("/api/host/runs", h.tokens["host"], startBody("req-live-00000001", "live-a")); code != 403 {
		t.Errorf("host started a run: %d", code)
	}
	code, run := h.post("/api/host/runs", h.tokens["operator"], startBody("req-live-00000001", "live-a"))
	if code != 202 {
		t.Fatalf("start: %d %v", code, run)
	}
	again, run2 := h.post("/api/host/runs", h.tokens["operator"], startBody("req-live-00000001", "live-a"))
	if again != 200 || run2["run_id"] != run["run_id"] {
		t.Fatalf("resend: %d %v", again, run2)
	}
	end := waitEnded(h, run["run_id"].(string))
	if end["outcome"] != "ok" || end["task_started"] != true {
		t.Fatalf("run end %v", end)
	}
	if code, _ := h.post("/api/host/runs", h.tokens["operator"], startBody("req-live-00000001", "live-a")); code != 200 {
		t.Errorf("resend after the end: %d", code)
	}
	time.Sleep(300 * time.Millisecond)
	if n := len(traces(t, h)); n != 1 {
		t.Fatalf("%d CLI runs for one request ID", n)
	}
	task := run["task_id"].(string)
	if !strings.HasPrefix(task, "show-a-") {
		t.Fatalf("task %s", task)
	}
	_, b, _ := h.get("/api/tasks/"+task, h.tokens["viewer:consumer-a"])
	var tv struct {
		Status    string `json:"status"`
		Purchases []struct {
			PurchaseOrderID string `json:"purchase_order_id"`
			Decision        struct {
				PolicyVerdict string `json:"policy_verdict"`
			} `json:"decision"`
		} `json:"purchases"`
	}
	json.Unmarshal(b, &tv)
	if tv.Status != "awaiting_approval" || len(tv.Purchases) != 1 || tv.Purchases[0].Decision.PolicyVerdict != "Ask" {
		t.Fatalf("task %s", b)
	}

	// The trace, after the run: to the task's readers only.
	code, tr, raw := h.trace(task, h.tokens["viewer:consumer-a"])
	if code != 200 || tr.State != "available" || tr.Trace == nil || tr.Trace.GateMode != "dev_demo_plugins" || tr.Trace.Gate != "passed" {
		t.Fatalf("trace %d %s", code, raw)
	}
	calls := tr.Trace.ToolCalls
	if len(calls) != 3 || calls[2].Tool != "request_purchase" || calls[2].Outcome != "Ask" ||
		calls[2].PurchaseOrderID != tv.Purchases[0].PurchaseOrderID || !slices.Equal(calls[2].EventSequences, []uint64{2, 3}) {
		t.Fatalf("tool calls %+v", calls)
	}
	if tr.Trace.Usage["input_tokens"] != 8 || tr.Trace.Usage["output_tokens"] != 445 || !strings.Contains(tr.Note, "CLI 自報") {
		t.Errorf("usage %v note %q", tr.Trace.Usage, tr.Note)
	}
	if strings.Contains(raw, mark) || strings.Contains(raw, "Payment complete") {
		t.Errorf("model text in the trace reply: %s", raw)
	}
	if code, _, _ := h.trace(task, h.tokens["viewer:consumer-b"]); code != 404 {
		t.Errorf("consumer B read A's trace: %d", code)
	}
	if code, _, _ := h.trace(task, h.tokens["operator"]); code != 403 {
		t.Errorf("the operator read a trace: %d", code)
	}
	if code, _, _ := h.trace(task, h.tokens["host"]); code != 200 {
		t.Errorf("the host: %d", code)
	}
}

// The live Gate is the live Gate: without --dev-demo-plugins an init with
// the two plugins is refused, no task starts and nothing is made up.
func TestLiveRunRefusedByTheGate(t *testing.T) {
	l := newLiveCLI(t)
	l.script(t, devPlugins, liveMainline()...)
	h := startHost(t, l.opts(false))
	defer h.stop()
	code, run := h.post("/api/host/runs", h.tokens["operator"], startBody("req-live-gate-0001", "live-a"))
	if code != 202 {
		t.Fatalf("start: %d %v", code, run)
	}
	end := waitEnded(h, run["run_id"].(string))
	if end["outcome"] != "config_mismatch" || end["detail"] != "plugins" || end["task_started"] != false {
		t.Fatalf("run end %v", end)
	}
	if tasks, _ := h.st.ListTasks("", 50); len(tasks) != 0 {
		t.Errorf("a task for a refused init: %+v", tasks)
	}
	if code, _, _ := h.trace(run["task_id"].(string), h.tokens["viewer:consumer-a"]); code != 404 {
		t.Errorf("trace of a task that never started: %d", code)
	}
}

// Each run checks the version and the config directory again: a CLI
// updated, or a config directory gone, after the host started stops that
// run before the CLI runs; no task, no trace.
func TestLiveRunChecksAgainEachRun(t *testing.T) {
	l := newLiveCLI(t)
	l.script(t, devPlugins, liveMainline()...)
	h := startHost(t, l.opts(true))
	defer h.stop()
	l.setVersion(t, "2.1.294 (Claude Code)")
	code, run := h.post("/api/host/runs", h.tokens["operator"], startBody("req-live-version-01", "live-a"))
	if code != 202 {
		t.Fatalf("start: %d %v", code, run)
	}
	if end := waitEnded(h, run["run_id"].(string)); end["outcome"] != "start_failed" || end["detail"] != "cli_version" || end["task_started"] != false {
		t.Fatalf("another version: %v", end)
	}
	l.setVersion(t, "2.1.293 (Claude Code)")
	os.Remove(filepath.Join(l.home, ".apa-claude"))
	code, run = h.post("/api/host/runs", h.tokens["operator"], startBody("req-live-confdir-01", "live-a"))
	if code != 202 {
		t.Fatalf("start: %d %v", code, run)
	}
	if end := waitEnded(h, run["run_id"].(string)); end["outcome"] != "start_failed" || end["detail"] != "host_error" || end["task_started"] != false {
		t.Fatalf("no config directory: %v", end)
	}
	if n := len(traces(t, h)); n != 0 {
		t.Errorf("%d traces: the CLI ran", n)
	}
	if tasks, _ := h.st.ListTasks("", 50); len(tasks) != 0 {
		t.Errorf("tasks: %+v", tasks)
	}
}

// While the CLI runs the trace is "running" (it is written at the end);
// a stop is the existing stop (TASK_CANCELLED by the buyer host); a host
// restart marks an open run interrupted and starts nothing again.
func TestLiveRunStopAndRestart(t *testing.T) {
	l := newLiveCLI(t)
	l.script(t, devPlugins, tool("list_products"), tool("get_quote", "product_id", "kr-market"), tool("request_purchase", "product_id", "kr-market"), map[string]any{"do": "hang"})
	o := l.opts(true)
	h := startHost(t, o)
	code, run := h.post("/api/host/runs", h.tokens["operator"], startBody("req-live-stop-0001", "live-a"))
	if code != 202 {
		t.Fatalf("start: %d %v", code, run)
	}
	task, id := run["task_id"].(string), run["run_id"].(string)
	deadline := time.Now().Add(20 * time.Second)
	for {
		_, b, _ := h.get("/api/tasks/"+task+"/events", h.tokens["host"])
		if strings.Count(string(b), `"event_type"`) >= 3 || time.Now().After(deadline) {
			break
		}
		time.Sleep(50 * time.Millisecond)
	}
	if code, tr, raw := h.trace(task, h.tokens["viewer:consumer-a"]); code != 200 || tr.State != "running" || tr.Trace != nil {
		t.Fatalf("trace while running: %d %s", code, raw)
	}
	if code, v := h.post("/api/host/runs/"+id+"/stop", h.tokens["operator"], `{}`); code != 202 {
		t.Fatalf("stop: %d %v", code, v)
	}
	waitEnded(h, id)
	_, b, _ := h.get("/api/tasks/"+task+"/events", h.tokens["host"])
	if !strings.Contains(string(b), "TASK_CANCELLED") {
		t.Fatalf("no TASK_CANCELLED after the stop: %s", b)
	}
	if _, tr, raw := h.trace(task, h.tokens["viewer:consumer-a"]); tr.State != "available" || tr.Trace == nil || len(tr.Trace.ToolCalls) != 3 {
		t.Fatalf("trace after the stop: %s", raw)
	}

	// A run open when the host stops: interrupted at the next start, not run again.
	code, run = h.post("/api/host/runs", h.tokens["operator"], startBody("req-live-restart-01", "live-a"))
	if code != 202 {
		t.Fatalf("second start: %d %v", code, run)
	}
	time.Sleep(500 * time.Millisecond)
	if err := h.stop(); err != nil {
		t.Fatal(err)
	}
	before := len(traces(t, h))
	o.dir = h.dir
	h2 := startHost(t, o)
	defer h2.stop()
	time.Sleep(500 * time.Millisecond)
	if v := h2.run(run["run_id"].(string)); v["state"] != "ended" && v["state"] != "interrupted" {
		t.Fatalf("after the restart: %v", v)
	}
	if n := len(traces(t, h2)); n != before {
		t.Errorf("traces %d -> %d: a run started again after the restart", before, n)
	}
}

// Live mode is off unless asked for, and refuses every unsafe combination
// before anything binds or runs.
func TestLiveModeRefusals(t *testing.T) {
	l := newLiveCLI(t)
	l.script(t, devPlugins, liveMainline()...)
	dir, err := os.MkdirTemp(testRoot, "live-refusal-")
	if err != nil {
		t.Fatal(err)
	}
	base := map[string]string{"HOME": l.home, "APA_LIVE_CLI": "true", "APA_ADDR": "127.0.0.1:0", "APA_DB_PATH": filepath.Join(dir, "live.db"),
		"APA_DEMO_TOKEN_FILE": filepath.Join(dir, "tokens.json"), "PATH": os.Getenv("PATH")}
	live := []string{"--registry", "testdata/demo/registry.json", "--live-cli", l.cli, "--scenarios", "testdata/demo/scenarios-live.json", "--dev-demo-plugins"}
	noHome, _ := os.MkdirTemp(testRoot, "live-nohome-")
	reg := []string{"--registry", "testdata/demo/registry.json"}
	for name, c := range map[string]struct {
		args []string
		env  map[string]string
		want string // a substring of the refusal: refused for this reason
	}{
		"no APA_LIVE_CLI":                  {live, map[string]string{"APA_LIVE_CLI": ""}, "also needs APA_LIVE_CLI=true"},
		"APA_LIVE_CLI=false":               {live, map[string]string{"APA_LIVE_CLI": "false"}, "also needs APA_LIVE_CLI=true"},
		"no APA_DB_PATH":                   {live, map[string]string{"APA_DB_PATH": ""}, "needs APA_DB_PATH"},
		"a relative CLI path":              {append(reg, "--live-cli", "claude", "--scenarios", "testdata/demo/scenarios-live.json"), nil, "absolute path"},
		"with a fake CLI":                  {append(live, "--fake-cli", l.fake), nil, "--live-cli excludes"},
		"with a plan":                      {append(live, "--plan", "testdata/demo/fake-plan.json"), nil, "exclude"},
		"with --upgrade-db":                {append(live, "--upgrade-db"), nil, "--live-cli excludes"},
		"no catalog":                       {append(reg, "--live-cli", l.cli), nil, "needs --scenarios"},
		"a fake catalog":                   {append(reg, "--live-cli", l.cli, "--scenarios", "testdata/demo/scenarios.json"), nil, "names no scenario file"},
		"the exception alone":              {[]string{"--dev-demo-plugins"}, nil, "for live mode only"},
		"history and live":                 {[]string{"--history-db", filepath.Join(dir, "x.db"), "--live-cli", l.cli}, nil, "excludes"},
		"no config directory":              {live, map[string]string{"HOME": noHome}, ".apa-claude"},
		"another version at start":         {live, nil, "version other than the approved"},
		"APA_LIVE_CLI=true, no --live-cli": {[]string{}, nil, "is not available to the demo server without --live-cli"},
		"APA_LIVE_CLI=true, a fake host":   {[]string{"--fake-cli", l.fake, "--plan", "testdata/demo/fake-plan.json"}, nil, "is not available to the demo server without --live-cli"},
		"APA_LIVE_CLI=true, history mode":  {[]string{"--history-db", filepath.Join(dir, "x.db")}, nil, "not for history mode"},
		"a live catalog in fake mode":      {append(reg, "--fake-cli", l.fake, "--scenarios", "testdata/demo/scenarios-live.json"), map[string]string{"APA_LIVE_CLI": ""}, "no scenario file"},
	} {
		if name == "another version at start" {
			l.setVersion(t, "2.1.292 (Claude Code)")
		}
		getenv := func(k string) string {
			if v, ok := c.env[k]; ok {
				return v
			}
			return base[k]
		}
		ready := make(chan string, 1)
		ctx, cancel := context.WithCancel(context.Background())
		exe, _ := os.Executable()
		done := make(chan error, 1)
		go func() {
			done <- serve(ctx, c.args, deps{getenv: getenv, self: exe, out: &lockedBuffer{}, clock: wallClock, ready: ready})
		}()
		select {
		case <-ready:
			t.Errorf("%s: served", name)
			cancel()
			<-done
		case err := <-done:
			if err == nil || !strings.Contains(err.Error(), c.want) {
				t.Errorf("%s: %v, want a refusal with %q", name, err, c.want)
			}
		case <-time.After(30 * time.Second):
			t.Errorf("%s: neither served nor refused", name)
		}
		cancel()
		l.setVersion(t, "2.1.293 (Claude Code)")
	}
	if _, err := os.Stat(filepath.Join(dir, "live.db")); !os.IsNotExist(err) {
		t.Error("a refused start opened the database")
	}
}
