package main

// Tests of slice 3.2 (docs/demo-operation-design.md section 13): host
// control endpoints, their permissions and guards, runs with new tasks,
// resends, stop, failures of the end records, restart recovery, the demo
// registry and the scenario catalog.

import (
	"bytes"
	"database/sql"
	"encoding/json"
	"errors"
	"go/ast"
	"go/parser"
	"go/token"
	"net/http"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/SIMPLYBOYS/agent-payment-authority/internal/contracts"
	"github.com/SIMPLYBOYS/agent-payment-authority/internal/store"
)

// catalogFile writes a scenario catalog: ask, deny-budget and hang (a run
// that waits after its Ask until it is stopped).
func catalogFile(t *testing.T) string {
	t.Helper()
	hang := scenarioFile(t, tool("request_purchase", "product_id", "kr-market"), map[string]any{"do": "hang"})
	raw, _ := json.Marshal(map[string]any{"description": "test", "scenarios": []map[string]string{
		{"id": "ask", "consumer_id": "consumer-a", "scenario": mainline(t), "label": "Ask"},
		{"id": "deny-budget", "consumer_id": "consumer-b", "scenario": mainline(t), "budget_initial": "250000", "label": "Deny"},
		{"id": "hang", "consumer_id": "consumer-a", "scenario": hang, "label": "hang"},
	}})
	p := filepath.Join(t.TempDir(), "scenarios.json")
	os.WriteFile(p, raw, 0o600)
	return p
}

func controlHost(t *testing.T, o hostOpts) *host {
	t.Helper()
	if o.scenarios == "" {
		o.scenarios = catalogFile(t)
	}
	o.args = append(o.args, "--registry", "testdata/demo/registry.json")
	return startHost(t, o)
}

// post sends a same-origin JSON POST as the page would.
func (h *host) post(path, token, body string, mod ...func(*http.Request)) (int, map[string]any) {
	h.t.Helper()
	req, _ := http.NewRequest(http.MethodPost, "http://"+h.addr+path, strings.NewReader(body))
	req.Header.Set("Origin", "http://"+h.addr)
	req.Header.Set("Content-Type", "application/json")
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	for _, m := range mod {
		m(req)
	}
	resp, err := noProxy.Do(req)
	if err != nil {
		h.t.Fatal(err)
	}
	defer resp.Body.Close()
	var out map[string]any
	json.NewDecoder(resp.Body).Decode(&out)
	return resp.StatusCode, out
}

func startBody(id, scenario string) string {
	return `{"request_id":"` + id + `","scenario":"` + scenario + `"}`
}

func (h *host) run(id string) map[string]any {
	h.t.Helper()
	_, body, _ := h.get("/api/host/runs/"+id, h.tokens["operator"])
	var v map[string]any
	json.Unmarshal(body, &v)
	return v
}

func (h *host) waitRun(id string, ok func(map[string]any) bool) map[string]any {
	h.t.Helper()
	for end := time.Now().Add(30 * time.Second); time.Now().Before(end); time.Sleep(20 * time.Millisecond) {
		if v := h.run(id); ok(v) {
			return v
		}
	}
	h.t.Fatalf("run %s: %v", id, h.run(id))
	return nil
}

func ended(v map[string]any) bool { return v["live"] == "ended" }

func TestControlAccess(t *testing.T) {
	h := controlHost(t, hostOpts{expiryEvery: time.Hour})
	defer h.stop()
	op, host, va := h.tokens["operator"], h.tokens["host"], h.tokens["viewer:consumer-a"]
	for name, c := range map[string]struct {
		token string
		want  int
	}{"viewer": {va, 403}, "host": {host, 403}, "none": {"", 401}} {
		if code, _ := h.post("/api/host/runs", c.token, startBody("req-access-0000000001", "ask")); code != c.want {
			t.Errorf("POST runs as %s: %d, want %d", name, code, c.want)
		}
		if code, _ := h.post("/api/host/runs/run-x/stop", c.token, `{}`); code != c.want {
			t.Errorf("POST stop as %s: %d, want %d", name, code, c.want)
		}
	}
	for path, want := range map[string]map[string]int{
		"/api/host/runs":      {op: 200, host: 200, va: 403, "": 401},
		"/api/host/status":    {op: 200, host: 200, va: 403},
		"/api/tasks/task-a":   {op: 403},
		"/api/viewer":         {op: 403},
		"/api/host/runs/none": {op: 404, host: 404, va: 403},
	} {
		for tok, code := range want {
			if got, _, _ := h.get(path, tok); got != code {
				t.Errorf("GET %s: %d, want %d", path, got, code)
			}
		}
	}
	guards := map[string]struct {
		body string
		mod  func(*http.Request)
	}{
		"no Origin":          {startBody("req-guard-0000000001", "ask"), func(r *http.Request) { r.Header.Del("Origin") }},
		"foreign Origin":     {startBody("req-guard-0000000001", "ask"), func(r *http.Request) { r.Header.Set("Origin", "http://evil.example") }},
		"form content type":  {startBody("req-guard-0000000001", "ask"), func(r *http.Request) { r.Header.Set("Content-Type", "application/x-www-form-urlencoded") }},
		"over 1 KiB":         {`{"request_id":"req-guard-0000000001","scenario":"` + strings.Repeat("a", 1100) + `"}`, nil},
		"an unknown field":   {`{"request_id":"req-guard-0000000001","scenario":"ask","task_id":"task-a"}`, nil},
		"a short request ID": {startBody("short", "ask"), nil},
		"two values":         {startBody("req-guard-0000000001", "ask") + `{}`, nil},
	}
	for name, g := range guards {
		var mods []func(*http.Request)
		if g.mod != nil {
			mods = append(mods, g.mod)
		}
		code, _ := h.post("/api/host/runs", op, g.body, mods...)
		if code != 400 && code != 403 {
			t.Errorf("%s: %d", name, code)
		}
	}
	if _, body, _ := h.get("/api/host/runs", op); bytes.Contains(body, []byte("req-guard")) {
		t.Error("a refused request started a run")
	}
	if _, body, _ := h.get("/api/host/status", op); !bytes.Contains(body, []byte(`"caller_role":"operator"`)) || !bytes.Contains(body, []byte(`"scenarios"`)) {
		t.Errorf("status for the operator: %s", body)
	}
}

func TestControlRuns(t *testing.T) {
	h := controlHost(t, hostOpts{expiryEvery: time.Hour})
	defer h.stop()
	op := h.tokens["operator"]

	code, a := h.post("/api/host/runs", op, startBody("req-ask-000000000001", "ask"))
	if code != 202 || a["task_id"] != "show-a-01" || a["consumer_id"] != "consumer-a" || a["live"] != "running" {
		t.Fatalf("start ask: %d %v", code, a)
	}
	a = h.waitRun(a["run_id"].(string), ended)
	if a["state"] != "ended" || a["outcome"] != "ok" || a["task_started"] != true || a["record_error"] != nil {
		t.Fatalf("ask ended: %v", a)
	}
	_, _, _, _, wantAsk := expected(t, "ask")
	_ = wantAsk
	if types := eventTypes(h.events("show-a-01")); !slices.Equal(types, []string{"TASK_STARTED", "PURCHASE_PROPOSED", "POLICY_DECIDED"}) {
		t.Errorf("show-a-01 events %v", types)
	}
	if h.task("show-a-01").Purchases[0].Decision.PolicyVerdict != "Ask" {
		t.Error("show-a-01 is not an Ask")
	}
	// The same request ID, after the run ended: the same run, no new task.
	if code, again := h.post("/api/host/runs", op, startBody("req-ask-000000000001", "ask")); code != 200 || again["run_id"] != a["run_id"] {
		t.Fatalf("resend: %d %v", code, again)
	}
	if code, c := h.post("/api/host/runs", op, startBody("req-ask-000000000001", "deny-budget")); code != 409 || c["error"] != "request_id_conflict" {
		t.Fatalf("conflict: %d %v", code, c)
	}
	if code, c := h.post("/api/host/runs", op, startBody("req-unknown-00000001", "no-such")); code != 400 || c["error"] != "unknown_scenario" {
		t.Fatalf("unknown scenario: %d %v", code, c)
	}
	code, d := h.post("/api/host/runs", op, startBody("req-deny-00000000001", "deny-budget"))
	if code != 202 || d["task_id"] != "show-b-01" {
		t.Fatalf("start deny: %d %v", code, d)
	}
	h.waitRun(d["run_id"].(string), ended)
	if v := h.task("show-b-01"); v.Status != "denied" || v.DenyReason != "INSUFFICIENT_BUDGET" {
		t.Errorf("show-b-01: %+v", v)
	}
	// A running run blocks a new start; two starts with one new ID make one run.
	code, hang := h.post("/api/host/runs", op, startBody("req-hang-00000000001", "hang"))
	if code != 202 || hang["task_id"] != "show-a-02" {
		t.Fatalf("start hang: %d %v", code, hang)
	}
	h.waitFor("show-a-02", func(v taskReply) bool { return len(v.Purchases) == 1 })
	if code, c := h.post("/api/host/runs", op, startBody("req-other-0000000001", "ask")); code != 409 || c["error"] != "run_active" {
		t.Fatalf("run_active: %d %v", code, c)
	}
	// Stop: stopping, then ended with TASK_CANCELLED recorded.
	if code, s := h.post("/api/host/runs/"+hang["run_id"].(string)+"/stop", op, `{}`); code != 202 || (s["live"] != "stopping" && s["live"] != "ended") {
		t.Fatalf("stop: %d %v", code, s)
	}
	stopped := h.waitRun(hang["run_id"].(string), ended)
	if stopped["outcome"] != "cancelled" || stopped["store_end"] != "TASK_CANCELLED" || stopped["state"] != "ended" {
		t.Fatalf("stopped: %v", stopped)
	}
	if v := h.task("show-a-02"); v.Status != "cancelled" {
		t.Errorf("show-a-02 %s", v.Status)
	}
	if code, c := h.post("/api/host/runs/"+hang["run_id"].(string)+"/stop", op, `{}`); code != 409 || c["error"] != "run_not_running" {
		t.Fatalf("stop again: %d %v", code, c)
	}
	if code, _ := h.post("/api/host/runs/run-none/stop", op, `{}`); code != 404 {
		t.Fatalf("stop unknown: %d", code)
	}
	// Concurrent starts with one new ID: one run, one task.
	var wg sync.WaitGroup
	var codes [4]int
	var ids [4]any
	for i := range 4 {
		wg.Go(func() {
			c, v := h.post("/api/host/runs", op, startBody("req-twin-00000000001", "ask"))
			codes[i], ids[i] = c, v["run_id"]
		})
	}
	wg.Wait()
	n202 := 0
	for i := range 4 {
		if codes[i] == 202 {
			n202++
		}
		if ids[i] != ids[0] || (codes[i] != 202 && codes[i] != 200) {
			t.Fatalf("twins: %v %v", codes, ids)
		}
	}
	if n202 != 1 {
		t.Fatalf("twins: %v", codes)
	}
	h.waitRun(ids[0].(string), ended)
	_, body, _ := h.get("/api/host/runs", op)
	if strings.Count(string(body), `"run_id"`) != 4 {
		t.Errorf("runs: %s", body)
	}
}

// A clock behind refuses new starts but not resends; the end records that
// fail are shown as failures, and a restart marks such runs interrupted.
func TestControlFailuresAndRestart(t *testing.T) {
	var now atomic.Uint64
	now.Store(wallClock())
	cat := catalogFile(t)
	h := controlHost(t, hostOpts{expiryEvery: time.Hour, clock: now.Load, scenarios: cat})
	op := h.tokens["operator"]
	_, a := h.post("/api/host/runs", op, startBody("req-a-00000000000001", "ask"))
	h.waitRun(a["run_id"].(string), ended)

	now.Store(now.Load() - 3600)
	if code, v := h.post("/api/host/runs", op, startBody("req-a-00000000000001", "ask")); code != 200 || v["run_id"] != a["run_id"] {
		t.Fatalf("resend with the clock behind: %d %v", code, v)
	}
	if code, v := h.post("/api/host/runs", op, startBody("req-b-00000000000001", "ask")); code != 503 || v["code"] != "CLOCK_REGRESSION" {
		t.Fatalf("new start with the clock behind: %d %v", code, v)
	}
	now.Store(now.Load() + 3600)

	// Both end records fail: the stop's TASK_CANCELLED (T3) and the run's end (T4).
	_, hang := h.post("/api/host/runs", op, startBody("req-hang-00000000001", "hang"))
	h.waitFor(hang["task_id"].(string), func(v taskReply) bool { return len(v.Purchases) == 1 })
	now.Store(now.Load() - 3600)
	h.post("/api/host/runs/"+hang["run_id"].(string)+"/stop", op, `{}`)
	v := h.waitRun(hang["run_id"].(string), ended)
	if v["store_end_error"] != "CLOCK_REGRESSION" || v["record_error"] != "CLOCK_REGRESSION" || v["state"] != "accepted" || v["store_end"] != nil && v["store_end"] != "" {
		t.Fatalf("failed end records: %v", v)
	}
	now.Store(now.Load() + 3600)

	// T4 alone fails: the row is no longer open when the run ends.
	_, hang2 := h.post("/api/host/runs", op, startBody("req-hang-00000000002", "hang"))
	h.waitFor(hang2["task_id"].(string), func(v taskReply) bool { return len(v.Purchases) == 1 })
	db, err := sql.Open("sqlite", filepath.Join(h.dir, "demo.db")+"?_pragma=busy_timeout(5000)")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`UPDATE host_runs SET state = 'interrupted', interrupted_at = accepted_at WHERE run_id = ?`, hang2["run_id"]); err != nil {
		t.Fatal(err)
	}
	db.Close()
	h.post("/api/host/runs/"+hang2["run_id"].(string)+"/stop", op, `{}`)
	if v := h.waitRun(hang2["run_id"].(string), ended); v["record_error"] != "RUN_NOT_OPEN" || v["store_end"] != "TASK_CANCELLED" {
		t.Fatalf("T4 failure: %v", v)
	}
	h.stop()

	// Restart: the run whose end was not recorded is interrupted; its
	// request ID returns it, nothing reruns.
	h = controlHost(t, hostOpts{dir: h.dir, expiryEvery: time.Hour, clock: now.Load, scenarios: cat})
	defer h.stop()
	op = h.tokens["operator"]
	if !strings.Contains(h.out.String(), "1 run(s) of a previous host marked interrupted") {
		t.Errorf("log: %s", h.out.String())
	}
	if code, v := h.post("/api/host/runs", op, startBody("req-hang-00000000001", "hang")); code != 200 || v["state"] != "interrupted" || v["live"] != nil {
		t.Fatalf("resend after restart: %d %v", code, v)
	}
	// Ask expired after a run ended: the task is running again, the run is not interrupted.
	if v := h.run(a["run_id"].(string)); v["state"] != "ended" {
		t.Fatalf("an ended run changed on restart: %v", v)
	}
}

func TestPlanSkipsReservedTask(t *testing.T) {
	h := controlHost(t, hostOpts{expiryEvery: time.Hour})
	_, a := h.post("/api/host/runs", h.tokens["operator"], startBody("req-res-000000000001", "ask"))
	h.waitRun(a["run_id"].(string), ended)
	h.stop()
	h = startHost(t, hostOpts{dir: h.dir, expiryEvery: time.Hour, runs: []planRun{{TaskID: "show-a-02", Scenario: mainline(t)}},
		args: []string{"--registry", "testdata/demo/registry.json"}})
	if out := h.waitRuns(); out[0].Skipped != "" || !out[0].TaskStarted {
		t.Fatalf("a free show task in a plan: %+v", out[0])
	}
	// Reserve show-a-03 by hand and plan it.
	h.stop()
	db, _ := sql.Open("sqlite", filepath.Join(h.dir, "demo.db"))
	db.Exec(`INSERT INTO host_runs (run_id, request_id, scenario, scenario_digest, budget_initial, task_id, consumer_id, agent_id, wallet_id, payer, state, accepted_at, interrupted_at)
		VALUES ('run-r', 'req-reserved-0000001', 'ask', 'sha256:x', '', 'show-a-03', 'consumer-a', 'agent-a', 'wallet-a', '0x1111111111111111111111111111111111111111', 'interrupted', 1, 1)`)
	db.Close()
	h = startHost(t, hostOpts{dir: h.dir, expiryEvery: time.Hour, runs: []planRun{{TaskID: "show-a-03", Scenario: mainline(t)}},
		args: []string{"--registry", "testdata/demo/registry.json"}})
	defer h.stop()
	out := h.waitRuns()
	if out[0].Skipped != "task_reserved_by_run" {
		t.Fatalf("plan on a reserved task: %+v", out[0])
	}
}

func TestNoFreeTask(t *testing.T) {
	var reg contracts.RegistryConfig
	raw, _ := os.ReadFile("testdata/demo/registry.json")
	contracts.Decode(raw, &reg)
	var tasks []contracts.Task
	for _, tk := range reg.Tasks {
		if !strings.HasPrefix(tk.TaskID, "show-") || tk.TaskID == "show-a-01" || tk.TaskID == "show-b-01" {
			tasks = append(tasks, tk)
		}
	}
	reg.Tasks = tasks
	small, _ := json.Marshal(reg)
	p := filepath.Join(t.TempDir(), "registry.json")
	os.WriteFile(p, small, 0o600)
	h := startHost(t, hostOpts{expiryEvery: time.Hour, scenarios: catalogFile(t), args: []string{"--registry", p}})
	defer h.stop()
	_, a := h.post("/api/host/runs", h.tokens["operator"], startBody("req-one-000000000001", "ask"))
	h.waitRun(a["run_id"].(string), ended)
	if code, v := h.post("/api/host/runs", h.tokens["operator"], startBody("req-two-000000000001", "ask")); code != 409 || v["error"] != "no_free_task" {
		t.Fatalf("%d %v", code, v)
	}
}

// The demo registry is the contract fixture plus show-* tasks: consumers,
// wallets, assets and merchant identical, the fixture's tasks kept.
func TestDemoRegistrySuperset(t *testing.T) {
	var fixture, demo contracts.RegistryConfig
	for p, dst := range map[string]*contracts.RegistryConfig{"testdata/contracts/registry.json": &fixture, "testdata/demo/registry.json": &demo} {
		raw, err := os.ReadFile(p)
		if err != nil {
			t.Fatal(err)
		}
		if err := contracts.Decode(raw, dst); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := contracts.NewRegistry(demo); err != nil {
		t.Fatal(err)
	}
	same := func(a, b any) bool { x, _ := json.Marshal(a); y, _ := json.Marshal(b); return bytes.Equal(x, y) }
	if !same(fixture.Consumers, demo.Consumers) || !same(fixture.Wallets, demo.Wallets) || !same(fixture.Assets, demo.Assets) ||
		fixture.MerchantPayTo != demo.MerchantPayTo || fixture.Version != demo.Version {
		t.Fatal("the demo registry differs from the contract fixture beyond its tasks")
	}
	shows := map[string]int{}
	for i, tk := range demo.Tasks {
		if i < len(fixture.Tasks) {
			if !same(tk, fixture.Tasks[i]) {
				t.Fatalf("fixture task %d changed", i)
			}
			continue
		}
		c := map[string]string{"consumer-a": "wallet-a", "consumer-b": "wallet-b"}[tk.ConsumerID]
		if !strings.HasPrefix(tk.TaskID, "show-") || tk.WalletID != c {
			t.Fatalf("extra task %+v", tk)
		}
		shows[tk.ConsumerID]++
	}
	if shows["consumer-a"] != 20 || shows["consumer-b"] != 20 {
		t.Fatalf("show tasks %v", shows)
	}
}

func TestLoadScenarios(t *testing.T) {
	var regCfg contracts.RegistryConfig
	raw, _ := os.ReadFile("testdata/demo/registry.json")
	contracts.Decode(raw, &regCfg)
	reg, _ := contracts.NewRegistry(regCfg)
	if s, err := loadScenarios("testdata/demo/scenarios.json", reg, regCfg); err != nil || len(s) != 2 || len(s["ask"].candidates) != 20 || s["ask"].digest == "" {
		t.Fatalf("the shipped catalog: %v %v", s, err)
	}
	for name, body := range map[string]string{
		"unknown field":         `{"scenarios":[{"id":"ask","consumer_id":"consumer-a","scenario":"testdata/cli/ask-mainline.json","label":"x","extra":1}]}`,
		"bad id":                `{"scenarios":[{"id":"Ask!","consumer_id":"consumer-a","scenario":"testdata/cli/ask-mainline.json","label":"x"}]}`,
		"no demo task":          `{"scenarios":[{"id":"ask","consumer_id":"consumer-z","scenario":"testdata/cli/ask-mainline.json","label":"x"}]}`,
		"missing scenario file": `{"scenarios":[{"id":"ask","consumer_id":"consumer-a","scenario":"testdata/cli/none.json","label":"x"}]}`,
		"bad budget":            `{"scenarios":[{"id":"ask","consumer_id":"consumer-a","scenario":"testdata/cli/ask-mainline.json","label":"x","budget_initial":"-1"}]}`,
		"empty":                 `{"scenarios":[]}`,
	} {
		p := filepath.Join(t.TempDir(), "c.json")
		os.WriteFile(p, []byte(body), 0o600)
		if _, err := loadScenarios(p, reg, regCfg); err == nil {
			t.Errorf("%s: accepted", name)
		}
	}
}

// Fixed values only: an unlisted detail and a code-less error never reach
// a run's end, the status or the log (section 13.11.1).
func TestRunEndFixedValues(t *testing.T) {
	const mark = "APA-SECRET-MARK-7f3c"
	e := runEnd(runOutcome{Outcome: "config_mismatch", Detail: "x_" + mark})
	if e.Detail != "detail_unlisted" {
		t.Fatalf("%+v", e)
	}
	if d := "prepare_failed:" + store.CodeOf(errors.New("boom "+mark)); d != "prepare_failed:STORE_ERROR" {
		t.Fatal(d)
	}
	h := &expiryHealth{}
	h.observe(1, errors.New("disk "+mark))
	if v := h.view(); v.LastError != "STORE_ERROR" {
		t.Fatalf("%+v", v)
	}
	if e := runEnd(runOutcome{Skipped: "scratch_home_failed"}); e.Outcome != "skipped" || e.Detail != "scratch_home_failed" {
		t.Fatalf("%+v", e)
	}
	if e := runEnd(runOutcome{Detail: "host_error"}); e.Outcome != "start_failed" {
		t.Fatalf("%+v", e)
	}
}

// The host's own details (fakeruns.go, control.go) are listed: the string
// literals assigned to Skipped and Detail, and Detail literals in
// runOutcome composites, read with go/ast.
func TestHostDetailsListed(t *testing.T) {
	set := map[string]bool{}
	fset := token.NewFileSet()
	for _, name := range []string{"cmd/demo/fakeruns.go", "cmd/demo/control.go"} {
		f, err := parser.ParseFile(fset, name, nil, 0)
		if err != nil {
			t.Fatal(err)
		}
		ast.Inspect(f, func(n ast.Node) bool {
			add := func(e ast.Expr) {
				if b, ok := e.(*ast.BasicLit); ok && b.Kind == token.STRING {
					s, _ := strconv.Unquote(b.Value)
					set[s] = true
				}
			}
			switch x := n.(type) {
			case *ast.AssignStmt:
				for i, l := range x.Lhs {
					if se, ok := l.(*ast.SelectorExpr); ok && (se.Sel.Name == "Skipped" || se.Sel.Name == "Detail") && i < len(x.Rhs) {
						add(x.Rhs[i])
					}
				}
			case *ast.KeyValueExpr:
				if k, ok := x.Key.(*ast.Ident); ok && (k.Name == "Skipped" || k.Name == "Detail") {
					add(x.Value)
				}
			}
			return true
		})
	}
	for _, must := range []string{"host_error", "scratch_home_failed", "task_reserved_by_run", "detail_unlisted"} {
		if !set[must] {
			t.Errorf("the scan misses %q", must)
		}
	}
	for d := range set {
		// skips only the plan mode records (they never reach host_runs)
		if d == "host_stopping" || d == "task_already_recorded" {
			continue
		}
		if !store.ValidDetail(d) {
			t.Errorf("cmd/demo sets detail %q, which store.RunDetails does not list", d)
		}
	}
}
