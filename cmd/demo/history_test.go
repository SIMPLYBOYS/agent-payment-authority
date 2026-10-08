package main

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/SIMPLYBOYS/agent-payment-authority/internal/contracts"
)

// recordRun makes a recorded run the way a real one is kept: a fake run of
// task-a ends in an open Ask; the host stops; the database file and the
// run trace are copied into directories of their own.
func recordRun(t *testing.T) (db, trace string) {
	t.Helper()
	h := startHost(t, hostOpts{runs: []planRun{{TaskID: "task-a", Scenario: mainline(t)}}, expiryEvery: time.Hour})
	h.waitRuns()
	if err := h.stop(); err != nil {
		t.Fatal(err)
	}
	rec := t.TempDir()
	db = filepath.Join(rec, "apa-live.db")
	raw, err := os.ReadFile(filepath.Join(h.dir, "demo.db"))
	if err != nil {
		t.Fatal(err)
	}
	os.WriteFile(db, raw, 0o600)
	traces, _ := filepath.Glob(filepath.Join(h.dir, "runs", "task-a-*.jsonl"))
	if len(traces) != 1 {
		t.Fatalf("traces %v", traces)
	}
	tr, _ := os.ReadFile(traces[0])
	trace = filepath.Join(t.TempDir(), "task-a.jsonl")
	os.WriteFile(trace, tr, 0o600)
	return db, trace
}

// startHistory serves db (and trace) in history mode with a host clock far
// past every recorded deadline: anything judged against it would expire.
func startHistory(t *testing.T, args ...string) (*host, error) {
	t.Helper()
	return startHistoryTokens(t, "", args...)
}

// startHistoryTokens is startHistory with APA_DEMO_TOKEN_FILE set to
// tokenFile ("" keeps the host directory's tokens.json).
func startHistoryTokens(t *testing.T, tokenFile string, args ...string) (*host, error) {
	t.Helper()
	dir, err := os.MkdirTemp(testRoot, "history-") // 0700, as the token file needs
	if err != nil {
		t.Fatal(err)
	}
	if tokenFile == "" {
		tokenFile = filepath.Join(dir, "tokens.json")
	}
	exe, _ := os.Executable()
	h := &host{t: t, dir: dir, out: &lockedBuffer{}}
	ready := make(chan string, 1)
	done := make(chan error, 1)
	ctx, cancel := context.WithCancel(context.Background())
	late := func() uint64 { return wallClock() + 10*86400 }
	env := hostEnv(dir, "127.0.0.1:0")
	getenv := func(k string) string {
		if k == "APA_DEMO_TOKEN_FILE" {
			return tokenFile
		}
		return env(k)
	}
	d := deps{getenv: getenv, self: exe, out: h.out, clock: late, ready: ready}
	go func() { done <- serve(ctx, args, d) }()
	h.stop = func() error { cancel(); return <-done }
	select {
	case h.addr = <-ready:
	case err := <-done:
		cancel()
		return nil, err
	case <-time.After(30 * time.Second):
		cancel()
		t.Fatal("the history host never became ready")
	}
	raw, _ := os.ReadFile(tokenFile)
	var file struct{ Tokens []demoToken }
	json.Unmarshal(raw, &file)
	h.tokens = map[string]string{}
	for _, tok := range file.Tokens {
		key := tok.Role
		if tok.ConsumerID != "" {
			key += ":" + tok.ConsumerID
		}
		h.tokens[key] = tok.Token
	}
	return h, nil
}

// History mode (docs/cli-poc.md 54): a recorded run is served read only.
// Nothing expires, however late the host clock is; served_at is the last
// recorded event; the trace's tool calls are mapped to the Store purchase
// and its events; the recorded file and its directory are left as they
// were.
func TestHistoryMode(t *testing.T) {
	db, trace := recordRun(t)
	sum := func() [32]byte { b, _ := os.ReadFile(db); return sha256.Sum256(b) }
	before := sum()

	h, err := startHistory(t, "--history-db", db, "--history-trace", trace)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(h.out.String(), "HISTORY MODE") {
		t.Errorf("start line: %s", h.out.String())
	}
	code, body, _ := h.get("/api/tasks/task-a/events", h.tokens["viewer:consumer-a"])
	var evs struct {
		AsOf   uint64 `json:"as_of_sequence"`
		Events []struct {
			Sequence   uint64 `json:"sequence"`
			EventType  string `json:"event_type"`
			ObservedAt uint64 `json:"observed_at"`
		} `json:"events"`
	}
	json.Unmarshal(body, &evs)
	var types []string
	for _, e := range evs.Events {
		types = append(types, e.EventType)
	}
	if code != 200 || !slices.Equal(types, []string{"TASK_STARTED", "PURCHASE_PROPOSED", "POLICY_DECIDED"}) {
		t.Fatalf("events %d %v", code, types) // no APPROVAL_EXPIRED, though the clock is 10 days late
	}
	last := evs.Events[len(evs.Events)-1].ObservedAt

	code, body, _ = h.get("/api/tasks/task-a", h.tokens["viewer:consumer-a"])
	var task struct {
		Status   string `json:"status"`
		ServedAt uint64 `json:"served_at"`
	}
	json.Unmarshal(body, &task)
	if code != 200 || task.Status != "awaiting_approval" || task.ServedAt != last {
		t.Fatalf("task %d %+v, want awaiting_approval served at %d", code, task, last)
	}

	if code, body, _ := h.get("/api/mode", ""); code != 200 || !strings.Contains(string(body), `"mode":"history"`) || !strings.Contains(string(body), historyLabel) || strings.Contains(string(body), "tool_calls") {
		t.Errorf("mode: %d %s", code, body)
	}
	code, body, _ = h.get("/api/viewer", h.tokens["viewer:consumer-a"])
	var v viewerReply
	json.Unmarshal(body, &v)
	if code != 200 || v.History == nil || v.History.Record != "live_history" || v.History.Label != historyLabel || v.History.AsOf != last {
		t.Fatalf("viewer %d %s", code, body)
	}
	tr := v.History.Trace
	if tr == nil || tr.TaskID != "task-a" || tr.GateMode != "strict" || tr.Gate != "passed" || len(tr.ToolCalls) != 3 {
		t.Fatalf("trace %+v", tr)
	}
	rp := tr.ToolCalls[2]
	if tr.ToolCalls[0].Tool != "list_products" || tr.ToolCalls[1].Tool != "get_quote" || rp.Tool != "request_purchase" ||
		rp.ProductID != "kr-market" || rp.Outcome != "Ask" || rp.PurchaseOrderID != "po-task-a-kr-market" || !slices.Equal(rp.EventSequences, []uint64{2, 3}) {
		t.Fatalf("tool calls %+v", tr.ToolCalls)
	}
	if strings.Contains(string(body), "Payment complete") || strings.Contains(string(body), mark) {
		t.Errorf("model text reached the API: %s", body)
	}
	if _, s, _ := h.status(h.tokens["host"]); s.History == nil || s.History.Record != "live_history" || s.ServedAt != last {
		t.Errorf("host status %+v", s)
	}
	if err := h.stop(); err != nil {
		t.Fatal(err)
	}
	if sum() != before {
		t.Error("the recorded database changed")
	}
	names, _ := os.ReadDir(filepath.Dir(db))
	if len(names) != 1 || names[0].Name() != "apa-live.db" {
		var got []string
		for _, n := range names {
			got = append(got, n.Name())
		}
		t.Errorf("files next to the recorded database: %v (no lock, WAL or shm)", got)
	}

	// Without a trace: served, no tool calls.
	h, err = startHistory(t, "--history-db", db)
	if err != nil {
		t.Fatal(err)
	}
	_, body, _ = h.get("/api/viewer", h.tokens["viewer:consumer-a"])
	if json.Unmarshal(body, &v); v.History == nil || v.History.Trace != nil {
		t.Errorf("no trace: %s", body)
	}
	h.stop()
}

// What history mode refuses before serving anything.
func TestHistoryModeRefusals(t *testing.T) {
	db, trace := recordRun(t)
	bad := filepath.Join(t.TempDir(), "bad.jsonl")
	os.WriteFile(bad, []byte("not json\n"), 0o600)
	other := filepath.Join(t.TempDir(), "other.jsonl")
	raw, _ := os.ReadFile(trace)
	os.WriteFile(other, []byte(strings.ReplaceAll(string(raw), `"task_id":"task-a"`, `"task_id":"task-z"`)), 0o600)
	// One field outside its whitelist each: a tool the bridge does not
	// offer; a gate mode that is neither strict nor the exception.
	tool := filepath.Join(t.TempDir(), "tool.jsonl")
	os.WriteFile(tool, []byte(strings.Replace(string(raw), `"tool":"list_products"`, `"tool":"Bash"`, 1)), 0o600)
	mode := filepath.Join(t.TempDir(), "mode.jsonl")
	os.WriteFile(mode, []byte(strings.Replace(string(raw), `"gate_mode":"strict"`, `"gate_mode":"lenient"`, 1)), 0o600)
	// A database that may still be open (a -wal beside it), and a damaged
	// one: throwaway copies, never the recorded file.
	walDir := t.TempDir()
	walDB := filepath.Join(walDir, "apa-live.db")
	dbRaw, _ := os.ReadFile(db)
	os.WriteFile(walDB, dbRaw, 0o600)
	os.WriteFile(walDB+"-wal", nil, 0o600)
	broken := filepath.Join(t.TempDir(), "apa-live.db")
	os.WriteFile(broken, append(dbRaw[:len(dbRaw)/2:len(dbRaw)/2], make([]byte, len(dbRaw)-len(dbRaw)/2)...), 0o600)
	if !strings.Contains(string(raw), `"tool":"list_products"`) || !strings.Contains(string(raw), `"gate_mode":"strict"`) {
		t.Fatalf("the recorded trace changed shape: %s", raw)
	}
	for name, args := range map[string][]string{
		"trace alone":       {"--history-trace", trace},
		"with a fake CLI":   {"--history-db", db, "--fake-cli", "/bin/true", "--plan", "p.json"},
		"with an upgrade":   {"--history-db", db, "--upgrade-db"},
		"missing database":  {"--history-db", db + ".missing"},
		"malformed trace":   {"--history-db", db, "--history-trace", bad},
		"trace of no task":  {"--history-db", db, "--history-trace", other},
		"unknown tool":      {"--history-db", db, "--history-trace", tool},
		"unknown gate mode": {"--history-db", db, "--history-trace", mode},
		"a WAL beside it":   {"--history-db", walDB},
		"damaged database":  {"--history-db", broken},
	} {
		if h, err := startHistory(t, args...); err == nil {
			h.stop()
			t.Errorf("%s: served", name)
		}
	}
}

// History mode is read only on the server, not only on the page: every
// start, stop and cancel is refused whoever asks, there is no run, and the
// recorded events stay the three they were. The trace goes only to a caller
// who may read its task: consumer B's viewer and the operator get the label
// without it.
func TestHistoryModeReadOnly(t *testing.T) {
	db, trace := recordRun(t)
	h, err := startHistory(t, "--history-db", db, "--history-trace", trace)
	if err != nil {
		t.Fatal(err)
	}
	defer h.stop()
	for _, tok := range []string{"operator", "host", "viewer:consumer-a"} {
		for _, path := range []string{"/api/host/runs", "/api/host/runs/run-1/stop", "/api/host/tasks/task-a/cancel"} {
			body := `{}`
			if path == "/api/host/runs" {
				body = startBody("req-history-00000001", "ask")
			}
			if code, v := h.post(path, h.tokens[tok], body); code != 409 || v["error"] != "history_read_only" {
				t.Errorf("%s POST %s: %d %v", tok, path, code, v)
			}
		}
	}
	if code, v := h.post("/api/host/tasks/task-a/cancel", "", `{}`); code != 401 {
		t.Errorf("no token: %d %v", code, v)
	}
	if code, body, _ := h.get("/api/host/runs", h.tokens["operator"]); code != 200 || strings.TrimSpace(string(body)) != `{"runs":[]}` {
		t.Errorf("runs: %d %s", code, body)
	}
	if code, _, _ := h.get("/api/host/runs", h.tokens["viewer:consumer-a"]); code != 403 {
		t.Errorf("runs as a viewer: %d", code)
	}
	_, body, _ := h.get("/api/tasks/task-a/events", h.tokens["host"])
	if n := strings.Count(string(body), `"event_type"`); n != 3 {
		t.Errorf("%d events after the refused writes: %s", n, body)
	}

	var v viewerReply
	_, body, _ = h.get("/api/viewer", h.tokens["viewer:consumer-b"])
	if json.Unmarshal(body, &v); v.History == nil || v.History.Label != historyLabel || v.History.Trace != nil || strings.Contains(string(body), "request_purchase") {
		t.Errorf("consumer B's viewer: %s", body)
	}
	_, body, _ = h.get("/api/viewer", h.tokens["viewer:consumer-a"])
	if json.Unmarshal(body, &v); v.History == nil || v.History.Trace == nil {
		t.Errorf("consumer A's viewer: %s", body)
	}
	if _, s, _ := h.status(h.tokens["operator"]); s.History == nil || s.History.Trace != nil {
		t.Errorf("operator status history %+v", s.History)
	}
	if _, s, _ := h.status(h.tokens["host"]); s.History == nil || s.History.Trace == nil {
		t.Errorf("host status history %+v", s.History)
	}
}

// R1 (Codex review of the history slice): the token file is written at
// start, so it may not name the trace, however spelled (the same path, a
// symlink, a hard link): refused before anything is served, the trace left
// as it was. A token file of its own still works.
func TestHistoryTokenFileIsNotTheTrace(t *testing.T) {
	db, recorded := recordRun(t)
	dir, err := os.MkdirTemp(testRoot, "history-trace-") // 0700: the token directory is valid
	if err != nil {
		t.Fatal(err)
	}
	trace := filepath.Join(dir, "trace.jsonl")
	raw, _ := os.ReadFile(recorded)
	os.WriteFile(trace, raw, 0o600)
	link := filepath.Join(dir, "link.jsonl")
	os.Symlink(trace, link)
	hard := filepath.Join(dir, "hard.jsonl")
	os.Link(trace, hard)
	parent := filepath.Join(testRoot, "link-"+filepath.Base(dir))
	os.Symlink(dir, parent)
	cwd, _ := os.Getwd()
	rel, err := filepath.Rel(cwd, trace)
	if err != nil || filepath.IsAbs(rel) {
		t.Fatalf("no relative spelling of %s: %v", trace, err)
	}
	mode := func() os.FileMode {
		fi, err := os.Stat(trace)
		if err != nil {
			t.Fatalf("the trace is gone: %v", err)
		}
		return fi.Mode()
	}
	mode0 := mode()
	for name, tok := range map[string]string{
		"a relative spelling": rel,
		"the trace":           trace,
		"a symlink to it":     link,
		"a hard link to it":   hard,
		"a symlinked parent":  filepath.Join(parent, "trace.jsonl"),
		"a dot-dot spelling":  filepath.Join(dir, "sub", "..", "trace.jsonl"),
	} {
		h, err := startHistoryTokens(t, tok, "--history-db", db, "--history-trace", trace)
		if err == nil {
			h.stop()
			t.Errorf("%s: served", name)
		} else if !errors.Is(err, errTokenInput) {
			t.Errorf("%s: %v, want the token-input refusal", name, err)
		}
		if b, _ := os.ReadFile(trace); string(b) != string(raw) || mode() != mode0 {
			t.Fatalf("%s: the trace was changed (content or mode)", name)
		}
	}
	// Checked again just before the write (newTokens), not only at start.
	if _, err := newTokens(contracts.RegistryConfig{}, hard, db, trace); !errors.Is(err, errTokenInput) {
		t.Errorf("newTokens over the trace: %v", err)
	}
	if b, _ := os.ReadFile(trace); string(b) != string(raw) || mode() != mode0 {
		t.Fatal("newTokens changed the trace (content or mode)")
	}
	h, err := startHistoryTokens(t, filepath.Join(dir, "tokens.json"), "--history-db", db, "--history-trace", trace)
	if err != nil {
		t.Fatalf("a token file of its own: %v", err)
	}
	h.stop()
	if b, _ := os.ReadFile(trace); string(b) != string(raw) {
		t.Fatal("the trace was changed by a normal start")
	}
}

// R2 (Codex review of the history slice): a trace whose request_purchase
// verdict is not the Store's, or names a verdict for a purchase the Store
// does not hold, is refused, and neither file is changed. A matching trace
// is mapped; a call that ended in an error code is shown without a purchase.
func TestHistoryTraceVerdictMustMatchTheStore(t *testing.T) {
	db, trace := recordRun(t)
	raw, _ := os.ReadFile(trace)
	line := `"outcome":"Ask","product_id":"kr-market","tool":"request_purchase"`
	if !strings.Contains(string(raw), line) {
		t.Fatalf("the recorded trace changed shape: %s", raw)
	}
	dbBefore, _ := os.ReadFile(db)
	write := func(name, with string) string {
		p := filepath.Join(t.TempDir(), name+".jsonl")
		os.WriteFile(p, []byte(strings.Replace(string(raw), line, with, 1)), 0o600)
		return p
	}
	for name, with := range map[string]string{
		"Deny for the Store's Ask":   `"outcome":"Deny","product_id":"kr-market","tool":"request_purchase"`,
		"Allow for the Store's Ask":  `"outcome":"Allow","product_id":"kr-market","tool":"request_purchase"`,
		"Ask for no stored purchase": `"outcome":"Ask","product_id":"kr-weather","tool":"request_purchase"`,
	} {
		p := write(strings.ReplaceAll(name, " ", "-"), with)
		before, _ := os.ReadFile(p)
		if h, err := startHistory(t, "--history-db", db, "--history-trace", p); err == nil {
			h.stop()
			t.Errorf("%s: served", name)
		} else if !strings.Contains(err.Error(), "disagree") {
			t.Errorf("%s: %v", name, err)
		}
		if after, _ := os.ReadFile(p); string(after) != string(before) {
			t.Errorf("%s: the trace was changed", name)
		}
	}
	if after, _ := os.ReadFile(db); string(after) != string(dbBefore) {
		t.Error("the database was changed")
	}

	// An error code: no verdict, so no purchase or events are joined to it.
	p := write("error-code", `"outcome":"store_unavailable","product_id":"kr-market","tool":"request_purchase"`)
	h, err := startHistory(t, "--history-db", db, "--history-trace", p)
	if err != nil {
		t.Fatal(err)
	}
	var v viewerReply
	_, body, _ := h.get("/api/viewer", h.tokens["viewer:consumer-a"])
	json.Unmarshal(body, &v)
	h.stop()
	if v.History == nil || v.History.Trace == nil || len(v.History.Trace.ToolCalls) != 3 {
		t.Fatalf("error-code trace: %s", body)
	}
	if c := v.History.Trace.ToolCalls[2]; c.PurchaseOrderID != "" || len(c.EventSequences) != 0 || c.Outcome != "store_unavailable" {
		t.Errorf("an error-code call was joined to the Store: %+v", c)
	}
	// A failed call for a product the Store holds no purchase of: loaded,
	// and nothing is made up for it.
	p = write("error-no-purchase", `"outcome":"unknown_product","product_id":"kr-weather","tool":"request_purchase"`)
	if h, err = startHistory(t, "--history-db", db, "--history-trace", p); err != nil {
		t.Fatal(err)
	}
	_, body, _ = h.get("/api/viewer", h.tokens["viewer:consumer-a"])
	v = viewerReply{}
	json.Unmarshal(body, &v)
	h.stop()
	if v.History == nil || v.History.Trace == nil || len(v.History.Trace.ToolCalls) != 3 {
		t.Fatalf("failed call without a purchase: %s", body)
	}
	if c := v.History.Trace.ToolCalls[2]; c.PurchaseOrderID != "" || len(c.EventSequences) != 0 || c.Outcome != "unknown_product" || c.ProductID != "kr-weather" {
		t.Errorf("a failed call without a purchase got one: %+v", c)
	}
}
