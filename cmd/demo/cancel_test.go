package main

// Tests of slice 3.4 (docs/demo-operation-design.md section 6.1 and the
// 3.4 instructions): POST /api/host/tasks/{task_id}/cancel, its roles and
// guards, every row of the cancel table, Store failures, concurrent
// cancels, stop and cancel together, and runs of a previous host. Expected
// outcomes come from the accepted cases (testdata/contracts/cases.json).

import (
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/SIMPLYBOYS/agent-payment-authority/internal/buyer"
	"github.com/SIMPLYBOYS/agent-payment-authority/internal/store"
)

// testClock is a host clock the test moves; behindOnce makes the next
// reading an hour behind (one Store write then refuses with
// CLOCK_REGRESSION), and behind makes every reading so.
type testClock struct {
	now        atomic.Uint64
	behindOnce atomic.Bool
	behind     atomic.Bool
}

func newTestClock() *testClock {
	c := &testClock{}
	c.now.Store(wallClock())
	return c
}

func (c *testClock) read() uint64 {
	if c.behind.Load() || c.behindOnce.CompareAndSwap(true, false) {
		return c.now.Load() - 3600
	}
	return c.now.Load()
}

var reqN atomic.Int64

func (h *host) cancel(id string) (int, map[string]any) {
	h.t.Helper()
	return h.post("/api/host/tasks/"+id+"/cancel", h.tokens["operator"], `{}`)
}

// startRun starts scenario and waits until its run has ended.
func (h *host) startRun(scenario string) map[string]any {
	h.t.Helper()
	code, v := h.post("/api/host/runs", h.tokens["operator"], startBody(fmt.Sprintf("req-cancel-%08d", reqN.Add(1)), scenario))
	if code != 202 {
		h.t.Fatalf("start %s: %d %v", scenario, code, v)
	}
	return h.waitRun(v["run_id"].(string), ended)
}

// startHang starts the hang scenario and waits for its Ask.
func (h *host) startHang() map[string]any {
	h.t.Helper()
	code, v := h.post("/api/host/runs", h.tokens["operator"], startBody(fmt.Sprintf("req-cancel-%08d", reqN.Add(1)), "hang"))
	if code != 202 {
		h.t.Fatalf("start hang: %d %v", code, v)
	}
	h.waitFor(v["task_id"].(string), func(t taskReply) bool { return len(t.Purchases) == 1 })
	return v
}

func count(e eventsReply, typ string) int {
	n := 0
	for _, v := range e.Events {
		if v.EventType == typ {
			n++
		}
	}
	return n
}

// keys says the reply has exactly these fields: no task content.
func keys(v map[string]any, want ...string) bool {
	got := []string{}
	for k := range v {
		got = append(got, k)
	}
	slices.Sort(got)
	slices.Sort(want)
	return slices.Equal(got, want)
}

func TestCancelAccessAndGuards(t *testing.T) {
	h := controlHost(t, hostOpts{expiryEvery: time.Hour})
	defer h.stop()
	id := h.startRun("ask")["task_id"].(string)
	before := h.events(id)
	path := "/api/host/tasks/" + id + "/cancel"
	for name, c := range map[string]struct {
		token string
		want  int
	}{"none": {"", 401}, "a wrong token": {strings.Repeat("0", 64), 401}, "viewer A": {h.tokens["viewer:consumer-a"], 403}, "host": {h.tokens["host"], 403}} {
		if code, v := h.post(path, c.token, `{}`); code != c.want {
			t.Errorf("as %s: %d %v, want %d", name, code, v, c.want)
		}
	}
	op := h.tokens["operator"]
	for name, g := range map[string]struct {
		body string
		mod  func(*http.Request)
	}{
		"no Origin":         {`{}`, func(r *http.Request) { r.Header.Del("Origin") }},
		"foreign Origin":    {`{}`, func(r *http.Request) { r.Header.Set("Origin", "http://evil.example") }},
		"another Host":      {`{}`, func(r *http.Request) { r.Host = "evil.example" }},
		"form content type": {`{}`, func(r *http.Request) { r.Header.Set("Content-Type", "application/x-www-form-urlencoded") }},
		"no body":           {``, nil},
		"over 1 KiB":        {`{"` + strings.Repeat("a", 1100) + `":1}`, nil},
		"an unknown field":  {`{"task_id":"` + id + `"}`, nil},
		"two values":        {`{}{}`, nil},
		"an array":          {`[]`, nil},
		"malformed":         {`{`, nil},
		"token in the URL":  {`{}`, func(r *http.Request) { r.Header.Del("Authorization"); r.URL.RawQuery = "token=" + op }},
	} {
		var mods []func(*http.Request)
		if g.mod != nil {
			mods = append(mods, g.mod)
		}
		if code, v := h.post(path, op, g.body, mods...); code != 400 && code != 401 && code != 403 {
			t.Errorf("%s: %d %v", name, code, v)
		}
	}
	if code, _ := h.post("/api/host/tasks/bad%20id/cancel", op, `{}`); code != 400 {
		t.Errorf("a malformed task ID: %d", code)
	}
	if after := h.events(id); len(after.Events) != len(before.Events) || h.task(id).Status != "awaiting_approval" {
		t.Fatalf("a refused request wrote: %v -> %v", eventTypes(before), eventTypes(after))
	}
	code, v := h.cancel(id)
	if code != 200 || v["state"] != "cancelled" || !keys(v, "state") {
		t.Fatalf("operator cancel: %d %v", code, v)
	}
}

func TestCancelRows(t *testing.T) {
	clk := newTestClock()
	h := controlHost(t, hostOpts{expiryEvery: time.Hour, clock: clk.read})
	defer h.stop()

	t.Run("not stored", func(t *testing.T) {
		for _, id := range []string{"show-a-20", "no-such-task"} {
			if code, v := h.cancel(id); code != 404 || v["error"] != "not_found" || !keys(v, "error") {
				t.Errorf("%s: %d %v", id, code, v)
			}
		}
	})

	t.Run("an Ask waiting, then the same cancel again", func(t *testing.T) {
		id := h.startRun("ask")["task_id"].(string)
		code, v := h.cancel(id)
		if code != 200 || v["state"] != "cancelled" || v["already"] != nil {
			t.Fatalf("cancel: %d %v", code, v)
		}
		e := h.events(id)
		types := eventTypes(e)
		if !slices.Equal(types[len(types)-2:], []string{"APPROVAL_WITHDRAWN", "TASK_CANCELLED"}) || e.Events[len(e.Events)-1].Code != "HOST_CANCELLED" {
			t.Fatalf("events %v", types)
		}
		wr, wre, wap, _, _ := expected(t, "cancel-pending")
		if r, re, ap := outcome(h.task(id).Purchases[0]); r != wr || re != wre || ap != wap || h.task(id).Status != "cancelled" {
			t.Fatalf("outcome %s/%s/%s", r, re, ap)
		}
		code, v = h.cancel(id)
		if code != 200 || v["state"] != "cancelled" || v["already"] != true || !keys(v, "state", "already") {
			t.Fatalf("again: %d %v", code, v)
		}
		if again := h.events(id); len(again.Events) != len(e.Events) {
			t.Fatalf("a repeated cancel wrote: %v", eventTypes(again))
		}
	})

	t.Run("a Deny is final", func(t *testing.T) {
		id := h.startRun("deny-budget")["task_id"].(string)
		before := h.events(id)
		if code, v := h.cancel(id); code != 409 || v["error"] != "not_eligible" || v["status"] != "denied" || !keys(v, "error", "status") {
			t.Fatalf("%d %v", code, v)
		}
		if v := h.task(id); v.Status != "denied" || v.DenyReason != "INSUFFICIENT_BUDGET" || len(h.events(id).Events) != len(before.Events) {
			t.Fatalf("the Deny changed: %+v", v)
		}
	})

	// The deadline is 30 s: 29 s after the decision the Ask is withdrawn by
	// the cancel; at exactly 30 s it expires first (accepted cases
	// approval-deadline-before and -exact, then cancel-pending).
	for _, c := range []struct {
		after  uint64
		closer string
		caseID string
	}{{29, "APPROVAL_WITHDRAWN", "cancel-pending"}, {30, "APPROVAL_EXPIRED", "approval-deadline-exact"}} {
		t.Run(fmt.Sprintf("cancel %d s after the Ask", c.after), func(t *testing.T) {
			at := clk.now.Load()
			id := h.startRun("ask")["task_id"].(string)
			decided := h.task(id).Purchases[0].DecidedAt
			if decided != at {
				t.Fatalf("decided at %d, clock %d", decided, at)
			}
			clk.now.Store(at + c.after)
			defer clk.now.Store(at + 31) // later subtests start after this one's events
			if code, v := h.cancel(id); code != 200 || v["already"] != nil {
				t.Fatalf("%d %v", code, v)
			}
			types := eventTypes(h.events(id))
			if !slices.Equal(types[len(types)-2:], []string{c.closer, "TASK_CANCELLED"}) {
				t.Fatalf("events %v", types)
			}
			wr, wre, wap, _, _ := expected(t, c.caseID)
			if r, re, ap := outcome(h.task(id).Purchases[0]); r != wr || re != wre || ap != wap {
				t.Fatalf("outcome %s/%s/%s, want %s/%s/%s", r, re, ap, wr, wre, wap)
			}
		})
	}

	t.Run("an Ask expired by the worker, the task running again", func(t *testing.T) {
		at := clk.now.Load()
		id := h.startRun("ask")["task_id"].(string)
		clk.now.Store(at + 31)
		if err := h.st.ExpireDue(); err != nil {
			t.Fatal(err)
		}
		if v := h.task(id); v.Status != "running" || v.Purchases[0].ApprovalClosed != "expired" {
			t.Fatalf("after expiry: %+v", v)
		}
		if code, v := h.cancel(id); code != 200 || v["already"] != nil {
			t.Fatalf("%d %v", code, v)
		}
		e := h.events(id)
		if count(e, "APPROVAL_EXPIRED") != 1 || count(e, "APPROVAL_WITHDRAWN") != 0 || count(e, "TASK_CANCELLED") != 1 || h.task(id).Status != "cancelled" {
			t.Fatalf("events %v", eventTypes(e))
		}
	})

	t.Run("a write the Store refuses is no success", func(t *testing.T) {
		id := h.startRun("ask")["task_id"].(string)
		before := h.events(id)
		clk.behind.Store(true)
		code, v := h.cancel(id)
		clk.behind.Store(false)
		if code != 503 || v["error"] != "store_unavailable" || v["code"] != "CLOCK_REGRESSION" || v["state"] != nil {
			t.Fatalf("%d %v", code, v)
		}
		if after := h.events(id); len(after.Events) != len(before.Events) || h.task(id).Status != "awaiting_approval" {
			t.Fatalf("a refused cancel wrote: %v", eventTypes(after))
		}
		if code, v := h.cancel(id); code != 200 || v["already"] != nil {
			t.Fatalf("retry: %d %v", code, v)
		}
	})
}

// A CLI timeout ends the task as failed and leaves its Ask to the deadline
// (accepted case cli-timeout-pending); a cancel is refused before and after
// that Ask expires.
func TestCancelAfterCLITimeout(t *testing.T) {
	clk := newTestClock()
	lim := buyer.DefaultLimits
	lim.Total = 3 * time.Second
	h := controlHost(t, hostOpts{expiryEvery: time.Hour, clock: clk.read, limits: &lim})
	defer h.stop()
	run := h.startHang()
	id := run["task_id"].(string)
	if v := h.waitRun(run["run_id"].(string), ended); v["outcome"] != "timeout" || v["store_end"] != "TASK_TIMED_OUT" {
		t.Fatalf("run %v", v)
	}
	wr, wre, wap, _, _ := expected(t, "cli-timeout-pending")
	if v := h.task(id); v.Status != wr || outcomeApproval(v) != wap || lastCode(h.events(id)) != wre {
		t.Fatalf("after the timeout: %+v", v)
	}
	before := h.events(id)
	if code, v := h.cancel(id); code != 409 || v["error"] != "already_ended" || v["end"] != "TASK_TIMED_OUT" || !keys(v, "error", "end") {
		t.Fatalf("%d %v", code, v)
	}
	if after := h.events(id); len(after.Events) != len(before.Events) {
		t.Fatalf("a refused cancel wrote: %v", eventTypes(after))
	}
	clk.now.Add(31)
	if err := h.st.ExpireDue(); err != nil {
		t.Fatal(err)
	}
	e := h.events(id)
	if count(e, "APPROVAL_EXPIRED") != 1 || count(e, "TASK_CANCELLED") != 0 || h.task(id).Status != "failed" {
		t.Fatalf("the Ask after the timeout: %v", eventTypes(e))
	}
	if code, _ := h.cancel(id); code != 409 || count(h.events(id), "TASK_CANCELLED") != 0 {
		t.Fatalf("after the expiry: %d", code)
	}
}

func sameJSON(a, b any) bool {
	x, _ := json.Marshal(a)
	y, _ := json.Marshal(b)
	return string(x) == string(y)
}

func outcomeApproval(v taskReply) string { return v.Purchases[0].Decision.ApprovalStatus }

func lastCode(e eventsReply) string { return e.Events[len(e.Events)-1].Code }

// Store failures through a test double's hook: a failed read, another
// writer ending the task between the read and the write, a failed re-read
// and a task gone on the re-read. None answers a success it cannot back.
func TestCancelStoreFaults(t *testing.T) {
	h := controlHost(t, hostOpts{expiryEvery: time.Hour})
	defer h.stop()
	defer func() { cancelFault = nil }()
	fault := func(stage string, f func() error) {
		cancelFault = func(s string) error {
			if s == stage {
				return f()
			}
			return nil
		}
	}

	t.Run("the read fails", func(t *testing.T) {
		id := h.startRun("ask")["task_id"].(string)
		fault("read", func() error { return errors.New("disk " + mark) })
		code, v := h.cancel(id)
		cancelFault = nil
		if code != 503 || v["code"] != "STORE_ERROR" || strings.Contains(fmt.Sprint(v), mark) || count(h.events(id), "TASK_CANCELLED") != 0 {
			t.Fatalf("%d %v", code, v)
		}
	})

	t.Run("ended by another writer: the committed end answers", func(t *testing.T) {
		id := h.startRun("ask")["task_id"].(string)
		fault("write", func() error { return h.st.Cancel(id) })
		code, v := h.cancel(id)
		cancelFault = nil
		if code != 200 || v["already"] != true || count(h.events(id), "TASK_CANCELLED") != 1 || count(h.events(id), "APPROVAL_WITHDRAWN") != 1 {
			t.Fatalf("%d %v %v", code, v, eventTypes(h.events(id)))
		}
	})

	t.Run("timed out by another writer: no cancel claimed", func(t *testing.T) {
		id := h.startRun("ask")["task_id"].(string)
		fault("write", func() error { return h.st.CLITimeout(id) })
		code, v := h.cancel(id)
		cancelFault = nil
		if code != 409 || v["end"] != "TASK_TIMED_OUT" || count(h.events(id), "TASK_CANCELLED") != 0 {
			t.Fatalf("%d %v", code, v)
		}
	})

	t.Run("the re-read fails", func(t *testing.T) {
		id := h.startRun("ask")["task_id"].(string)
		cancelFault = func(s string) error {
			switch s {
			case "write":
				return h.st.Cancel(id)
			case "reread":
				return errors.New("gone " + mark)
			}
			return nil
		}
		code, v := h.cancel(id)
		cancelFault = nil
		if code != 503 || v["code"] != "STORE_ERROR" || v["state"] != nil || strings.Contains(fmt.Sprint(v), mark) {
			t.Fatalf("%d %v", code, v)
		}
	})

	t.Run("the task is gone on the re-read", func(t *testing.T) {
		id := h.startRun("ask")["task_id"].(string)
		cancelFault = func(s string) error {
			switch s {
			case "write":
				return h.st.Cancel(id)
			case "reread":
				return &store.Error{Code: "OWNERSHIP_MISMATCH"}
			}
			return nil
		}
		code, v := h.cancel(id)
		cancelFault = nil
		if code != 503 || v["code"] != cancelGoneCode || v["state"] != nil {
			t.Fatalf("%d %v", code, v)
		}
	})
}

// Concurrent cancels, with the expiry worker at the exact deadline, write
// one TASK_CANCELLED and close each Ask once; a restart reads the same.
func TestCancelConcurrent(t *testing.T) {
	clk := newTestClock()
	h := controlHost(t, hostOpts{expiryEvery: time.Hour, clock: clk.read})
	ids := []string{}
	for range 3 {
		ids = append(ids, h.startRun("ask")["task_id"].(string))
	}
	clk.now.Add(30) // every Ask is due now
	var wg sync.WaitGroup
	codes := make(chan string, 64)
	for _, id := range ids {
		for range 6 {
			wg.Go(func() {
				code, v := h.cancel(id)
				codes <- fmt.Sprint(id, " ", code, " ", v["already"])
			})
		}
		wg.Go(func() { h.st.ExpireDue() })
	}
	wg.Wait()
	close(codes)
	fresh := map[string]int{}
	for c := range codes {
		parts := strings.Fields(c)
		if parts[1] != "200" {
			t.Errorf("cancel: %s", c)
		}
		if parts[2] == "<nil>" {
			fresh[parts[0]]++
		}
	}
	want := map[string]eventsReply{}
	for _, id := range ids {
		e := h.events(id)
		closed := count(e, "APPROVAL_EXPIRED") + count(e, "APPROVAL_WITHDRAWN")
		if fresh[id] != 1 || count(e, "TASK_CANCELLED") != 1 || closed != 1 || count(e, "APPROVAL_EXPIRED") != 1 {
			t.Errorf("%s: %d fresh cancels, events %v", id, fresh[id], eventTypes(e))
		}
		want[id] = e
	}
	h.stop()
	h = controlHost(t, hostOpts{dir: h.dir, expiryEvery: time.Hour, clock: clk.read})
	defer h.stop()
	for _, id := range ids {
		if got := h.events(id); !sameJSON(got, want[id]) {
			t.Errorf("%s after a restart: %v", id, eventTypes(got))
		}
	}
}

// While its run is running or stopping a task is not cancelled here; the
// stop ends it once, and nothing is proposed after the cancel.
func TestCancelWhileRunning(t *testing.T) {
	h := controlHost(t, hostOpts{expiryEvery: time.Hour})
	defer h.stop()
	op := h.tokens["operator"]
	run := h.startHang()
	id, runID := run["task_id"].(string), run["run_id"].(string)
	if code, v := h.cancel(id); code != 409 || v["error"] != "run_active" || !keys(v, "error") {
		t.Fatalf("running: %d %v", code, v)
	}
	if count(h.events(id), "TASK_CANCELLED") != 0 {
		t.Fatal("a refused cancel wrote")
	}
	var wg sync.WaitGroup
	var answers sync.Map
	wg.Go(func() { h.post("/api/host/runs/"+runID+"/stop", op, `{}`) })
	for i := range 8 {
		wg.Go(func() {
			time.Sleep(time.Duration(i) * 5 * time.Millisecond)
			code, v := h.cancel(id)
			answers.Store(i, fmt.Sprintf("%d %v %v", code, v["error"], v["already"]))
		})
	}
	wg.Wait()
	h.waitRun(runID, ended)
	answers.Range(func(_, a any) bool {
		if s := a.(string); s != "409 run_active <nil>" && s != "200 <nil> true" {
			t.Errorf("cancel with a stop: %s", s)
		}
		return true
	})
	e := h.events(id)
	if count(e, "TASK_CANCELLED") != 1 || count(e, "APPROVAL_WITHDRAWN") != 1 || count(e, "PURCHASE_PROPOSED") != 1 || e.Events[len(e.Events)-1].EventType != "TASK_CANCELLED" {
		t.Fatalf("events %v", eventTypes(e))
	}
	if code, v := h.cancel(id); code != 200 || v["already"] != true {
		t.Fatalf("after the stop: %d %v", code, v)
	}
}

// Runs of a previous host: an orderly shutdown ends the running run with
// its cancel; a run whose end records failed comes back interrupted and
// its task can be cancelled; one with no task answers 404; a cancel after
// a failed cancel record leaves that run's history as it was. Nothing
// reruns, and the database still opens (its recovery check) afterwards.
func TestCancelAfterRestart(t *testing.T) {
	clk := newTestClock()
	cat := catalogFile(t)
	h := controlHost(t, hostOpts{expiryEvery: time.Hour, clock: clk.read, scenarios: cat})
	op := h.tokens["operator"]

	// Running at an orderly shutdown.
	shut := h.startHang()
	h.stop()
	h = controlHost(t, hostOpts{dir: h.dir, expiryEvery: time.Hour, clock: clk.read, scenarios: cat})
	op = h.tokens["operator"]
	if v := h.run(shut["run_id"].(string)); v["state"] != "ended" || v["store_end"] != "TASK_CANCELLED" {
		t.Fatalf("after an orderly shutdown: %v", v)
	}
	if code, v := h.cancel(shut["task_id"].(string)); code != 200 || v["already"] != true {
		t.Fatalf("cancel after shutdown: %d %v", code, v)
	}
	clk.now.Add(1)

	// A run whose stop record fails once (T3), its end record written (T4).
	r3 := h.startHang()
	clk.behindOnce.Store(true)
	h.post("/api/host/runs/"+r3["run_id"].(string)+"/stop", op, `{}`)
	v3 := h.waitRun(r3["run_id"].(string), ended)
	if v3["store_end_error"] != "CLOCK_REGRESSION" || v3["state"] != "ended" || v3["record_error"] != nil {
		t.Fatalf("T3 alone fails: %v", v3)
	}
	// A run whose both records fail: still accepted, interrupted on restart.
	r4 := h.startHang()
	clk.behind.Store(true)
	h.post("/api/host/runs/"+r4["run_id"].(string)+"/stop", op, `{}`)
	h.waitRun(r4["run_id"].(string), ended)
	clk.behind.Store(false)
	// A run accepted for show-a-09 whose task was never started.
	db, err := sql.Open("sqlite", filepath.Join(h.dir, "demo.db")+"?_pragma=busy_timeout(5000)")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`INSERT INTO host_runs (run_id, request_id, scenario, scenario_digest, budget_initial, task_id, consumer_id, agent_id, wallet_id, payer, state, accepted_at)
		VALUES ('run-notask', 'req-notask-00000001', 'ask', 'sha256:x', '', 'show-a-09', 'consumer-a', 'agent-a', 'wallet-a', '0x1111111111111111111111111111111111111111', 'accepted', ?)`, clk.now.Load()); err != nil {
		t.Fatal(err)
	}
	db.Close()
	h.stop()

	h = controlHost(t, hostOpts{dir: h.dir, expiryEvery: time.Hour, clock: clk.read, scenarios: cat})
	op = h.tokens["operator"]
	runsBefore := func() string { _, b, _ := h.get("/api/host/runs", op); return string(b) }()
	if v := h.run(r4["run_id"].(string)); v["state"] != "interrupted" || h.task(r4["task_id"].(string)).Status != "awaiting_approval" {
		t.Fatalf("interrupted: %v", v)
	}
	if v := h.run("run-notask"); v["state"] != "interrupted" {
		t.Fatalf("interrupted, no task: %v", v)
	}
	if code, v := h.cancel("show-a-09"); code != 404 {
		t.Fatalf("no task: %d %v", code, v)
	}
	for _, id := range []string{r4["task_id"].(string), r3["task_id"].(string)} {
		if code, v := h.cancel(id); code != 200 || v["already"] != nil {
			t.Fatalf("cancel %s: %d %v", id, code, v)
		}
		if v := h.task(id); v.Status != "cancelled" {
			t.Fatalf("%s: %s", id, v.Status)
		}
	}
	if after := func() string { _, b, _ := h.get("/api/host/runs", op); return string(b) }(); after != runsBefore {
		t.Fatalf("a cancel changed the runs' history:\n%s\n%s", runsBefore, after)
	}
	if v := h.run(r3["run_id"].(string)); v["store_end_error"] != "CLOCK_REGRESSION" || v["state"] != "ended" {
		t.Fatalf("the failed record's history: %v", v)
	}
	h.stop()
	if _, err := store.VerifyFile(filepath.Join(h.dir, "demo.db")); err != nil {
		t.Fatalf("recovery check after the cancels: %v", err)
	}
	h = controlHost(t, hostOpts{dir: h.dir, expiryEvery: time.Hour, clock: clk.read, scenarios: cat})
	defer h.stop()
	if v := h.run(r4["run_id"].(string)); v["state"] != "interrupted" {
		t.Fatalf("reopened: %v", v)
	}
	if _, b, _ := h.get("/api/host/runs", h.tokens["operator"]); strings.Count(string(b), `"run_id"`) != 4 {
		t.Fatalf("a run was added or rerun: %s", b)
	}
}

// TestSeedCancelDisplay writes the database the browser test's slice 3.4
// section starts on (scripts/verify/browser-ui.mjs), through the host's own
// runs: show-a-01 a CLI timeout after its Ask; show-a-02 an Ask, run ended;
// show-b-01 a Deny; show-a-03 a stop whose cancel record failed; show-a-04
// a stop whose both records failed (interrupted on the next start). The
// clock is ten minutes back, so every Ask is past its deadline when the
// browser's host opens the database. Skipped unless APA_SEED_CANCEL_DIR is set.
func TestSeedCancelDisplay(t *testing.T) {
	dir := os.Getenv("APA_SEED_CANCEL_DIR")
	if dir == "" {
		t.Skip("APA_SEED_CANCEL_DIR not set")
	}
	clk := newTestClock()
	clk.now.Store(wallClock() - 600)
	cat := catalogFile(t)
	lim := buyer.DefaultLimits
	lim.Total = 3 * time.Second
	h := controlHost(t, hostOpts{dir: dir, expiryEvery: time.Hour, clock: clk.read, scenarios: cat, limits: &lim})
	r := h.startHang()
	if v := h.waitRun(r["run_id"].(string), ended); r["task_id"] != "show-a-01" || v["outcome"] != "timeout" || v["store_end"] != "TASK_TIMED_OUT" {
		t.Fatalf("timeout: %v", v)
	}
	h.stop()
	clk.now.Add(1)
	h = controlHost(t, hostOpts{dir: dir, expiryEvery: time.Hour, clock: clk.read, scenarios: cat})
	defer h.stop()
	op := h.tokens["operator"]
	if v := h.startRun("ask"); v["task_id"] != "show-a-02" || v["outcome"] != "ok" {
		t.Fatalf("ask: %v", v)
	}
	if v := h.startRun("deny-budget"); v["task_id"] != "show-b-01" || h.task("show-b-01").Status != "denied" {
		t.Fatalf("deny: %v", v)
	}
	r = h.startHang()
	clk.behindOnce.Store(true)
	h.post("/api/host/runs/"+r["run_id"].(string)+"/stop", op, `{}`)
	if v := h.waitRun(r["run_id"].(string), ended); r["task_id"] != "show-a-03" || v["state"] != "ended" || v["store_end_error"] != "CLOCK_REGRESSION" {
		t.Fatalf("cancel record failed: %v", v)
	}
	r = h.startHang()
	clk.behind.Store(true)
	h.post("/api/host/runs/"+r["run_id"].(string)+"/stop", op, `{}`)
	v := h.waitRun(r["run_id"].(string), ended)
	clk.behind.Store(false)
	if r["task_id"] != "show-a-04" || v["state"] != "accepted" || v["record_error"] != "CLOCK_REGRESSION" {
		t.Fatalf("both records failed: %v", v)
	}
}
