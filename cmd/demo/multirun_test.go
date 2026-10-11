package main

// Tests of M-B's host runs (docs/multi-agent-demo-design.md 3, 6): the
// deployment setting --max-runs, one running or stopping run per consumer,
// a stopping run holding its place, a stop that touches only its own run,
// the timing records and their judgement. All fake; handshake files order
// the two runs' steps exactly (wait_file), never a fixed sleep.

import (
	"context"
	"database/sql"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/SIMPLYBOYS/agent-payment-authority/internal/timing"
)

// catalogOf writes a scenario catalog of id -> consumer and fake steps.
func catalogOf(t *testing.T, entries map[string]struct {
	consumer string
	steps    []map[string]any
}) string {
	t.Helper()
	var list []map[string]string
	for id, e := range entries {
		list = append(list, map[string]string{"id": id, "consumer_id": e.consumer, "scenario": scenarioFile(t, e.steps...), "label": id})
	}
	raw, _ := json.Marshal(map[string]any{"description": "test", "scenarios": list})
	p := filepath.Join(t.TempDir(), "scenarios.json")
	os.WriteFile(p, raw, 0o600)
	return p
}

type entry = struct {
	consumer string
	steps    []map[string]any
}

func waitFile(p string) map[string]any { return map[string]any{"do": "wait_file", "text": p} }

var (
	hang     = map[string]any{"do": "hang"}
	hangTerm = map[string]any{"do": "hang", "ignore_term": true} // stopping until SIGKILL, Grace later
	done     = map[string]any{"do": "result", "text": "done"}
)

func touch(t *testing.T, p string) {
	t.Helper()
	if err := os.WriteFile(p, nil, 0o600); err != nil {
		t.Fatal(err)
	}
}

// calls waits until the run has answered n calls (tool_calls) and is still
// running.
func (h *host) calls(runID string, n int) {
	h.t.Helper()
	h.waitRun(runID, func(v map[string]any) bool {
		c, _ := v["tool_calls"].(float64)
		return int(c) >= n
	})
}

func (h *host) start(id, scenario string) (int, map[string]any) {
	h.t.Helper()
	return h.post("/api/host/runs", h.tokens["operator"], startBody(id, scenario))
}

// record reads the timing record of a run and its host_runs row.
func (h *host) record(runID, taskID string) (timing.Record, *timing.HostRun) {
	h.t.Helper()
	files, _ := filepath.Glob(filepath.Join(h.dir, "runs", taskID+"-*.timing.json"))
	if len(files) != 1 {
		h.t.Fatalf("%s: %d timing records", taskID, len(files))
	}
	raw, _ := os.ReadFile(files[0])
	r, err := timing.Parse(raw)
	if err != nil {
		h.t.Fatal(err)
	}
	conn, err := sql.Open("sqlite", "file:"+filepath.Join(h.dir, "demo.db")+"?mode=ro")
	if err != nil {
		h.t.Fatal(err)
	}
	defer conn.Close()
	row, err := timing.ReadHostRun(conn, runID) // as cmd/timing-judge reads it
	if err != nil || row == nil {
		h.t.Fatalf("host_runs %s: %v %v", runID, row, err)
	}
	return r, row
}

// The deployment setting: refused when illegal or without host runs.
func TestMaxRunsSetting(t *testing.T) {
	two := catalogOf(t, map[string]entry{"a": {"consumer-a", []map[string]any{done}}, "b": {"consumer-b", []map[string]any{done}}})
	base := []string{"--fake-cli", "/bin/true", "--scenarios", two, "--registry", "testdata/demo/registry.json"}
	for name, c := range map[string]struct {
		args []string
		want string
	}{
		"without host runs":   {[]string{"--max-runs", "1"}, "--max-runs is for a host that starts runs"},
		"history mode":        {[]string{"--max-runs", "1", "--history-db", "x.db"}, "--max-runs is for a host that starts runs"},
		"zero":                {append([]string{"--max-runs", "0"}, base...), "it must be from 1 to 2"},
		"more than consumers": {append([]string{"--max-runs", "3"}, base...), "it must be from 1 to 2"},
		"not an integer":      {append([]string{"--max-runs", "two"}, base...), "invalid value"},
	} {
		ready := make(chan string, 1)
		errc := make(chan error, 1)
		ctx, cancel := context.WithCancel(context.Background())
		go func() {
			errc <- serve(ctx, c.args, deps{getenv: hostEnv(t.TempDir(), "127.0.0.1:0"), out: &lockedBuffer{}, clock: wallClock, ready: ready})
		}()
		select {
		case <-ready:
			t.Errorf("%s: served", name)
			cancel()
			<-errc
		case err := <-errc:
			if err == nil || !strings.Contains(err.Error(), c.want) {
				t.Errorf("%s: %v, want %q", name, err, c.want)
			}
		case <-time.After(30 * time.Second):
			t.Errorf("%s: neither served nor refused", name)
		}
		cancel()
	}
	one := map[string]*scenario{"a": {ConsumerID: "consumer-a"}}
	if n, m, err := capacity(one, 0, false); n != 1 || m != 1 || err != nil {
		t.Errorf("default with one consumer: %d %d %v", n, m, err)
	}
}

// Two consumers run at once; each consumer has one run at most; the status
// shows the capacity and both runs; a viewer reads only its own task.
func TestTwoRunsAtOnce(t *testing.T) {
	h := controlHost(t, hostOpts{expiryEvery: time.Hour, scenarios: catalogOf(t, map[string]entry{
		"hang-a": {"consumer-a", []map[string]any{tool("request_purchase", "product_id", "kr-market"), hang}},
		"hang-b": {"consumer-b", []map[string]any{tool("request_purchase", "product_id", "kr-market"), hang}},
		"ask":    {"consumer-a", []map[string]any{done}},
	})})
	defer h.stop()
	if !strings.Contains(h.out.String(), "concurrent runs: at most 2 (deployment setting --max-runs) of 2 consumers") {
		t.Errorf("no capacity line:\n%s", h.out.String())
	}
	_, st, _ := h.status(h.tokens["host"])
	if st.Capacity["max_runs"] != 2 || st.Capacity["consumers"] != 2 {
		t.Fatalf("capacity %v", st.Capacity)
	}
	_, a := h.start("req-hang-a-000000001", "hang-a")
	_, b := h.start("req-hang-b-000000001", "hang-b")
	if a["live"] != "running" || b["live"] != "running" || a["consumer_id"] != "consumer-a" || b["consumer_id"] != "consumer-b" {
		t.Fatalf("starts: %v %v", a, b)
	}
	h.calls(a["run_id"].(string), 1)
	h.calls(b["run_id"].(string), 1)
	_, st, _ = h.status(h.tokens["host"])
	if active, _ := st.Runs["active"].([]any); len(active) != 2 {
		t.Fatalf("active: %v", st.Runs["active"])
	}
	if code, c := h.start("req-ask-0000000000001", "ask"); code != 409 || c["error"] != "consumer_busy" || c["run"].(map[string]any)["run_id"] != a["run_id"] {
		t.Fatalf("same consumer: %d %v", code, c)
	}
	// A viewer reads only its own consumer's task; the host reads both.
	ta, tb := a["task_id"].(string), b["task_id"].(string)
	if code, _, _ := h.get("/api/tasks/"+tb, h.tokens["viewer:consumer-a"]); code != 404 {
		t.Errorf("viewer A read B's task: %d", code)
	}
	for _, id := range []string{ta, tb} {
		if code, _, _ := h.get("/api/tasks/"+id, h.tokens["host"]); code != 200 {
			t.Errorf("host read %s: %d", id, code)
		}
	}
	for _, r := range []map[string]any{a, b} {
		h.post("/api/host/runs/"+r["run_id"].(string)+"/stop", h.tokens["operator"], `{}`)
		if v := h.waitRun(r["run_id"].(string), ended); v["tool_calls"] != nil || v["timing"] != "written" {
			t.Errorf("ended: %v", v)
		}
	}
}

// --max-runs 1: the second consumer is refused while the first runs or is
// still stopping; a resend of the first answers with its run even when full.
func TestCapacityAndStopping(t *testing.T) {
	h := controlHost(t, hostOpts{expiryEvery: time.Hour, args: []string{"--max-runs", "1"}, scenarios: catalogOf(t, map[string]entry{
		"hang-a": {"consumer-a", []map[string]any{tool("list_products"), hangTerm}},
		"ask-b":  {"consumer-b", []map[string]any{done}},
	})})
	defer h.stop()
	code, a := h.start("req-hang-a-000000001", "hang-a")
	if code != 202 {
		t.Fatalf("start: %d %v", code, a)
	}
	h.calls(a["run_id"].(string), 1)
	if code, c := h.start("req-ask-b-0000000001", "ask-b"); code != 409 || c["error"] != "capacity_full" || c["max_runs"] != float64(1) {
		t.Fatalf("full: %d %v", code, c)
	}
	if code, again := h.start("req-hang-a-000000001", "hang-a"); code != 200 || again["run_id"] != a["run_id"] {
		t.Fatalf("resend when full: %d %v", code, again)
	}
	// Stopping: the fake ignores SIGTERM, so the run stays stopping until
	// the SIGKILL after the grace period, and still holds its place.
	if code, s := h.post("/api/host/runs/"+a["run_id"].(string)+"/stop", h.tokens["operator"], `{}`); code != 202 || s["live"] != "stopping" {
		t.Fatalf("stop: %d %v", code, s)
	}
	if code, c := h.start("req-ask-b-0000000002", "ask-b"); code != 409 || c["error"] != "capacity_full" {
		t.Fatalf("while stopping: %d %v", code, c)
	}
	h.waitRun(a["run_id"].(string), ended)
	if code, c := h.start("req-ask-b-0000000003", "ask-b"); code != 202 {
		t.Fatalf("after the stop: %d %v", code, c)
	}
}

// Concurrent starts with new IDs never pass the capacity or the
// one-per-consumer rule.
func TestConcurrentStartsCapped(t *testing.T) {
	h := controlHost(t, hostOpts{expiryEvery: time.Hour, scenarios: catalogOf(t, map[string]entry{
		"hang-a": {"consumer-a", []map[string]any{hang}},
		"hang-b": {"consumer-b", []map[string]any{hang}},
	})})
	defer h.stop()
	var wg sync.WaitGroup
	var mu sync.Mutex
	accepted := map[string]int{}
	for i := range 8 {
		wg.Go(func() {
			sc := []string{"hang-a", "hang-b"}[i%2]
			code, v := h.start("req-many-0000000000"+string(rune('a'+i)), sc)
			mu.Lock()
			defer mu.Unlock()
			switch {
			case code == 202:
				accepted[v["consumer_id"].(string)]++
			case code != 409 || (v["error"] != "consumer_busy" && v["error"] != "capacity_full"):
				t.Errorf("start %d: %d %v", i, code, v)
			}
		})
	}
	wg.Wait()
	if accepted["consumer-a"] != 1 || accepted["consumer-b"] != 1 {
		t.Fatalf("accepted %v", accepted)
	}
	_, body, _ := h.get("/api/host/runs", h.tokens["operator"])
	var list struct{ Runs []map[string]any }
	json.Unmarshal(body, &list)
	for _, r := range list.Runs {
		h.post("/api/host/runs/"+r["run_id"].(string)+"/stop", h.tokens["operator"], `{}`)
		h.waitRun(r["run_id"].(string), ended)
	}
}

// Stopping A touches A only: B keeps its capability, makes its next call
// after A's stop and ends with its own result; the timing records show the
// stop isolation.
func TestStopIsolation(t *testing.T) {
	gate := filepath.Join(t.TempDir(), "b-go")
	h := controlHost(t, hostOpts{expiryEvery: time.Hour, scenarios: catalogOf(t, map[string]entry{
		"hang-a": {"consumer-a", []map[string]any{tool("list_products"), hang}},
		"b":      {"consumer-b", []map[string]any{tool("list_products"), waitFile(gate), tool("get_quote", "product_id", "kr-market"), done}},
	})})
	defer h.stop()
	_, a := h.start("req-hang-a-000000001", "hang-a")
	_, b := h.start("req-b-00000000000001", "b")
	ra, rb := a["run_id"].(string), b["run_id"].(string)
	h.calls(ra, 1)
	h.calls(rb, 1)
	if code, s := h.post("/api/host/runs/"+ra+"/stop", h.tokens["operator"], `{}`); code != 202 {
		t.Fatalf("stop A: %d %v", code, s)
	}
	stoppedA := h.waitRun(ra, ended)
	if stoppedA["store_end"] != "TASK_CANCELLED" {
		t.Fatalf("A: %v", stoppedA)
	}
	if v := h.run(rb); v["live"] != "running" {
		t.Fatalf("B after A's stop: %v", v)
	}
	touch(t, gate) // only now may B make its next call
	endB := h.waitRun(rb, ended)
	if endB["outcome"] != "ok" || endB["store_end"] != nil {
		t.Fatalf("B: %v", endB)
	}
	ta, rowA := h.record(ra, a["task_id"].(string))
	tb, rowB := h.record(rb, b["task_id"].(string))
	if v := timing.Judge(ta, tb, rowA, rowB, false); v.Conclusion == timing.NotJudged {
		t.Fatalf("records: %+v", v)
	}
	if tb.Calls[len(tb.Calls)-1].Outcome != "ok" {
		t.Errorf("B's call after A's stop: %+v", tb.Calls)
	}
	if s := timing.StopIsolation(ta, tb, rowB); !s.Held {
		t.Fatalf("stop isolation: %+v\nA %+v\nB %+v", s, ta, tb)
	}
	// Stopping a run that already ended is refused, and is no stop.
	if code, c := h.post("/api/host/runs/"+ra+"/stop", h.tokens["operator"], `{}`); code != 409 || c["error"] != "run_not_running" {
		t.Fatalf("stop again: %d %v", code, c)
	}
}

// The timing records of two runs whose calls are made to alternate
// (A, B, A, B) judge as interleaved; two runs one after the other do not.
func TestTimingInterleaved(t *testing.T) {
	dir := t.TempDir()
	x := func(n string) string { return filepath.Join(dir, n) }
	h := controlHost(t, hostOpts{expiryEvery: time.Hour, scenarios: catalogOf(t, map[string]entry{
		"a":       {"consumer-a", []map[string]any{tool("list_products"), waitFile(x("x1")), tool("get_quote", "product_id", "kr-market"), waitFile(x("x3")), done}},
		"b":       {"consumer-b", []map[string]any{waitFile(x("x0")), tool("list_products"), waitFile(x("x2")), tool("get_quote", "product_id", "kr-market"), done}},
		"quick-a": {"consumer-a", []map[string]any{tool("list_products"), tool("get_quote", "product_id", "kr-market"), done}},
		"quick-b": {"consumer-b", []map[string]any{tool("list_products"), tool("get_quote", "product_id", "kr-market"), done}},
	})})
	defer h.stop()
	_, a := h.start("req-a-00000000000001", "a")
	_, b := h.start("req-b-00000000000001", "b")
	ra, rb := a["run_id"].(string), b["run_id"].(string)
	h.calls(ra, 1)
	touch(t, x("x0"))
	h.calls(rb, 1)
	touch(t, x("x1"))
	h.calls(ra, 2)
	touch(t, x("x2"))
	h.calls(rb, 2)
	touch(t, x("x3"))
	h.waitRun(ra, ended)
	h.waitRun(rb, ended)
	ta, rowA := h.record(ra, a["task_id"].(string))
	tb, rowB := h.record(rb, b["task_id"].(string))
	if ta.HostInstance == "" || ta.HostInstance != tb.HostInstance || ta.Mode != "fake" || len(ta.Calls) != 2 || len(tb.Calls) != 2 {
		t.Fatalf("records:\nA %+v\nB %+v", ta, tb)
	}
	v := timing.Judge(ta, tb, rowA, rowB, false)
	if v.Conclusion != timing.Interleaved || !v.Fake || !strings.HasPrefix(v.Text, "（fake 紀錄") {
		t.Fatalf("interleaved: %+v", v)
	}
	if v := timing.Judge(ta, tb, rowA, rowB, true); v.Conclusion != timing.NotJudged {
		t.Fatalf("fake judged as live: %+v", v)
	}
	// One after the other: no work overlap.
	_, qa := h.start("req-quick-a-00000001", "quick-a")
	h.waitRun(qa["run_id"].(string), ended)
	_, qb := h.start("req-quick-b-00000001", "quick-b")
	h.waitRun(qb["run_id"].(string), ended)
	qta, qrowA := h.record(qa["run_id"].(string), qa["task_id"].(string))
	qtb, qrowB := h.record(qb["run_id"].(string), qb["task_id"].(string))
	if v := timing.Judge(qta, qtb, qrowA, qrowB, false); v.Conclusion != timing.NoWorkOverlap {
		t.Fatalf("one after the other: %+v", v)
	}
	// A record never holds a token, capability or model text.
	for _, task := range []string{a["task_id"].(string), b["task_id"].(string)} {
		files, _ := filepath.Glob(filepath.Join(h.dir, "runs", task+"-*.timing.json"))
		raw, _ := os.ReadFile(files[0])
		for _, tok := range h.tokens {
			if strings.Contains(string(raw), tok) {
				t.Fatal("a token in a timing record")
			}
		}
	}
}

// A host stopped with two runs open ends each one by itself (its own
// TASK_CANCELLED; a host that dies without recording marks its runs
// interrupted instead, TestControlFailuresAndRestart), and a restart starts
// nothing by itself.
func TestHostStopWithTwoRuns(t *testing.T) {
	dir, err := os.MkdirTemp(testRoot, "host-")
	if err != nil {
		t.Fatal(err)
	}
	scenarios := catalogOf(t, map[string]entry{
		"hang-a": {"consumer-a", []map[string]any{tool("list_products"), hang}},
		"hang-b": {"consumer-b", []map[string]any{tool("list_products"), hang}},
	})
	h := controlHost(t, hostOpts{dir: dir, expiryEvery: time.Hour, scenarios: scenarios})
	_, a := h.start("req-hang-a-000000001", "hang-a")
	_, b := h.start("req-hang-b-000000001", "hang-b")
	h.calls(a["run_id"].(string), 1)
	h.calls(b["run_id"].(string), 1)
	h.stop()
	h2 := controlHost(t, hostOpts{dir: dir, expiryEvery: time.Hour, scenarios: scenarios})
	defer h2.stop()
	for _, r := range []map[string]any{a, b} {
		if v := h2.run(r["run_id"].(string)); v["state"] != "ended" || v["store_end"] != "TASK_CANCELLED" || v["live"] != nil {
			t.Errorf("after restart: %v", v)
		}
	}
	_, st, _ := h2.status(h2.tokens["host"])
	if active, _ := st.Runs["active"].([]any); len(active) != 0 {
		t.Errorf("something started by itself: %v", st.Runs["active"])
	}
}
