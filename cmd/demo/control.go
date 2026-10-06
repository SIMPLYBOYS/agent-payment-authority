package main

// Host control of fake runs (docs/demo-operation-design.md sections 13.1,
// 13.3-13.5, 13.8 R3): the operator starts and stops runs; the operator and
// the host read them. One run at a time; each run gets a new demo task. A
// request ID always answers with the same run, even after a restart.
// Nothing here writes an event: the run's own buyer host does (StartTask,
// Cancel, CLITimeout), and host_runs holds the coordination record.

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"mime"
	"net/http"
	"regexp"
	"slices"
	"strings"
	"sync"

	"github.com/SIMPLYBOYS/agent-payment-authority/internal/store"
	"github.com/SIMPLYBOYS/agent-payment-authority/internal/toolbridge"
)

// liveRun is what this process knows beyond host_runs: whether the run's
// process is running or stopping, and an end that could not be recorded.
type liveRun struct {
	run       store.Run
	state     string // running | stopping | ended
	cancel    context.CancelFunc
	end       *store.RunEnd
	recordErr string // T4 failed: the process ended, its record was not written
}

type control struct {
	mu        sync.Mutex
	st        *store.Store
	fake      *fakeRunner
	bridge    *toolbridge.Server
	scenarios map[string]*scenario
	ctx       context.Context // ends with the host
	wg        *sync.WaitGroup // the host waits for every run before closing the Store
	out       io.Writer
	active    *liveRun            // the one run whose process is running or stopping
	ended     map[string]*liveRun // runs that ended in this process, by run_id
}

// runView is a run as the API shows it: fixed fields and codes, never a
// token or any CLI or error text.
type runView struct {
	RunID         string `json:"run_id"`
	RequestID     string `json:"request_id"`
	Scenario      string `json:"scenario"`
	TaskID        string `json:"task_id"`
	ConsumerID    string `json:"consumer_id"`
	State         string `json:"state"`          // accepted | interrupted | ended (host_runs)
	Live          string `json:"live,omitempty"` // running | stopping | ended (this process)
	AcceptedAt    uint64 `json:"accepted_at"`
	InterruptedAt uint64 `json:"interrupted_at,omitempty"`
	EndedAt       uint64 `json:"ended_at,omitempty"`
	Outcome       string `json:"outcome,omitempty"`
	Detail        string `json:"detail,omitempty"`
	TaskStarted   *bool  `json:"task_started,omitempty"`
	StoreEnd      string `json:"store_end,omitempty"`
	StoreEndError string `json:"store_end_error,omitempty"`
	RecordError   string `json:"record_error,omitempty"` // the run's end could not be recorded (code)
}

// view merges the stored run with what this process knows. c.mu is held,
// and r was read under it: runOne records the end and updates memory under
// c.mu too, so a view never mixes an older row with a newer end.
func (c *control) view(r store.Run) runView {
	v := runView{RunID: r.RunID, RequestID: r.RequestID, Scenario: r.Scenario, TaskID: r.TaskID, ConsumerID: r.ConsumerID,
		State: r.State, AcceptedAt: r.AcceptedAt, InterruptedAt: r.InterruptedAt, EndedAt: r.EndedAt}
	if r.State == "ended" {
		started := r.TaskStarted
		v.Outcome, v.Detail, v.TaskStarted, v.StoreEnd, v.StoreEndError = r.Outcome, r.Detail, &started, r.StoreEnd, r.StoreEndError
	}
	lr := c.ended[r.RunID]
	if c.active != nil && c.active.run.RunID == r.RunID {
		lr = c.active
	}
	if lr != nil {
		v.Live, v.RecordError = lr.state, lr.recordErr
		if lr.end != nil && r.State != "ended" { // ended here, record not written: show what is known, marked
			e := lr.end
			v.Outcome, v.Detail, v.TaskStarted, v.StoreEnd, v.StoreEndError = e.Outcome, e.Detail, &e.TaskStarted, e.StoreEnd, e.StoreEndError
		}
	}
	return v
}

func (c *control) register(mux *http.ServeMux, a *api) {
	mux.HandleFunc("GET /api/host/runs", func(w http.ResponseWriter, r *http.Request) { c.list(w, r, a) })
	mux.HandleFunc("GET /api/host/runs/{run_id}", func(w http.ResponseWriter, r *http.Request) { c.get(w, r, a) })
	mux.HandleFunc("POST /api/host/runs", func(w http.ResponseWriter, r *http.Request) { c.start(w, r, a) })
	mux.HandleFunc("POST /api/host/runs/{run_id}/stop", func(w http.ResponseWriter, r *http.Request) { c.stop(w, r, a) })
	mux.HandleFunc("POST /api/host/tasks/{task_id}/cancel", func(w http.ResponseWriter, r *http.Request) { c.cancelTask(w, r, a) })
}

// reader admits the host (read only) and the operator.
func reader(w http.ResponseWriter, r *http.Request, a *api) bool {
	g, ok := a.authorize(w, r)
	if !ok {
		return false
	}
	if g.Role != "host" && g.Role != "operator" {
		fail(w, http.StatusForbidden, "forbidden_role")
		return false
	}
	return true
}

// postGuard admits the operator only, on a same-origin JSON POST (section
// 6.2): Origin must be present and this host's own; the whole body is at
// most 1 KiB and is exactly one JSON object (JSON white space around it
// allowed), decoded strictly into dst. A null decodes into a struct
// without an error and More() does not mean the end of the body, so the
// object type and the true end are checked apart (review of d61771c, R1).
func postGuard(w http.ResponseWriter, r *http.Request, a *api, dst any) bool {
	g, ok := a.authorize(w, r)
	if !ok {
		return false
	}
	if r.Header.Get("Origin") != "http://"+a.addr {
		fail(w, http.StatusForbidden, "forbidden_origin")
		return false
	}
	if g.Role != "operator" {
		fail(w, http.StatusForbidden, "forbidden_role")
		return false
	}
	if mt, _, err := mime.ParseMediaType(r.Header.Get("Content-Type")); err != nil || mt != "application/json" {
		fail(w, http.StatusBadRequest, "bad_request")
		return false
	}
	body, err := io.ReadAll(http.MaxBytesReader(w, r.Body, 1024))
	if err != nil || !bytes.HasPrefix(bytes.TrimLeft(body, " \t\r\n"), []byte("{")) {
		fail(w, http.StatusBadRequest, "bad_request")
		return false
	}
	dec := json.NewDecoder(bytes.NewReader(body))
	dec.DisallowUnknownFields()
	if err := dec.Decode(dst); err != nil {
		fail(w, http.StatusBadRequest, "bad_request")
		return false
	}
	if _, err := dec.Token(); err != io.EOF { // only white space may follow the object
		fail(w, http.StatusBadRequest, "bad_request")
		return false
	}
	return true
}

var requestIDFormat = regexp.MustCompile(`^[A-Za-z0-9-]{16,64}$`)

// start is POST /api/host/runs (section 13.8 R3): formats first; then a read
// only lookup of the request ID, which answers a resend with its run even
// when the clock is behind or the scenario retired; only a new ID meets the
// current catalog, the one-run rule and the write (which checks the ID once
// more inside its transaction). The run's process starts after the commit.
func (c *control) start(w http.ResponseWriter, r *http.Request, a *api) {
	var req struct {
		RequestID string `json:"request_id"`
		Scenario  string `json:"scenario"`
	}
	if !postGuard(w, r, a, &req) {
		return
	}
	if !requestIDFormat.MatchString(req.RequestID) || !scenarioID.MatchString(req.Scenario) {
		fail(w, http.StatusBadRequest, "bad_request")
		return
	}
	if run, found, ok := c.lookup(w, req.RequestID); !ok {
		return
	} else if found {
		c.mu.Lock()
		c.answer(w, run, req.Scenario)
		c.mu.Unlock()
		return
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	if run, found, ok := c.lookup(w, req.RequestID); !ok { // a twin may have been accepted meanwhile
		return
	} else if found {
		c.answer(w, run, req.Scenario)
		return
	}
	sc := c.scenarios[req.Scenario]
	if sc == nil {
		fail(w, http.StatusBadRequest, "unknown_scenario")
		return
	}
	if c.active != nil {
		reply(w, http.StatusConflict, map[string]any{"error": "run_active", "run": c.view(c.active.run)})
		return
	}
	run, created, err := c.st.AcceptRun(store.RunRequest{RequestID: req.RequestID, Scenario: sc.ID, ScenarioDigest: sc.digest,
		BudgetInitial: string(sc.BudgetInitial), ConsumerID: sc.ConsumerID, Candidates: sc.candidates})
	switch {
	case store.Code(err) == "NO_FREE_TASK":
		fail(w, http.StatusConflict, "no_free_task")
		return
	case err != nil:
		reply(w, http.StatusServiceUnavailable, map[string]string{"error": "store_unavailable", "code": store.CodeOf(err)})
		return
	case !created:
		c.answer(w, run, req.Scenario)
		return
	}
	ctx, cancel := context.WithCancel(c.ctx)
	lr := &liveRun{run: run, state: "running", cancel: cancel}
	c.active = lr
	c.wg.Go(func() { c.runOne(ctx, lr, sc) })
	reply(w, http.StatusAccepted, c.view(run))
}

// lookup finds the run of a request ID with a read only query; ok is false
// when it has already answered a failure.
func (c *control) lookup(w http.ResponseWriter, requestID string) (store.Run, bool, bool) {
	run, found, err := c.st.RunByRequest(requestID)
	if err != nil {
		reply(w, http.StatusServiceUnavailable, map[string]string{"error": "store_unavailable", "code": store.CodeOf(err)})
		return store.Run{}, false, false
	}
	return run, found, true
}

// answer answers a known run: 200 with it, or 409 when the request ID named
// another scenario. c.mu is held; the row is read again under it, and only
// that read is shown: if it fails, or the run is gone (host_runs rows are
// never deleted), the answer is 503 with a fixed code, never the older row.
func (c *control) answer(w http.ResponseWriter, run store.Run, scenarioName string) {
	if run.Scenario != scenarioName {
		fail(w, http.StatusConflict, "request_id_conflict")
		return
	}
	fresh, found, err := c.st.GetRun(run.RunID)
	switch {
	case err != nil:
		reply(w, http.StatusServiceUnavailable, map[string]string{"error": "store_unavailable", "code": store.CodeOf(err)})
		return
	case !found:
		reply(w, http.StatusServiceUnavailable, map[string]string{"error": "store_unavailable", "code": "RUN_NOT_FOUND"})
		return
	}
	reply(w, http.StatusOK, c.view(fresh))
}

// runOne runs the accepted run's process to its end and records the end
// (T4). The end is recorded with fixed codes only; if the record cannot be
// written, this process remembers the end and its record error, and a
// restart marks the run interrupted.
func (c *control) runOne(ctx context.Context, lr *liveRun, sc *scenario) {
	defer lr.cancel()
	o := c.fake.runOne(ctx, c.st, c.bridge, planRun{TaskID: lr.run.TaskID, Scenario: sc.Scenario, BudgetInitial: sc.BudgetInitial})
	end := runEnd(o)
	c.mu.Lock() // the end's record and this process's view of it change together
	err := c.st.EndRun(lr.run.RunID, end)
	lr.state, lr.end = "ended", &end
	if err != nil {
		lr.recordErr = store.CodeOf(err)
	}
	if c.active == lr {
		c.active = nil
	}
	c.ended[lr.run.RunID] = lr
	if len(c.ended) > 50 { // ponytail: drop an arbitrary old one; the record is in host_runs
		for id := range c.ended {
			if id != lr.run.RunID {
				delete(c.ended, id)
				break
			}
		}
	}
	c.mu.Unlock()
	fmt.Fprintf(c.out, "demo: run %s task %s: outcome=%q detail=%q task_started=%t store_end=%q store_end_error=%q record_error=%q\n",
		lr.run.RunID, lr.run.TaskID, end.Outcome, end.Detail, end.TaskStarted, end.StoreEnd, end.StoreEndError, lr.recordErr)
}

// runEnd maps the fake runner's outcome to the run's end, codes only.
func runEnd(o runOutcome) store.RunEnd {
	e := store.RunEnd{Outcome: o.Outcome, Detail: o.Detail, TaskStarted: o.TaskStarted, StoreEnd: o.StoreEnd, StoreEndError: o.EndError}
	if o.Skipped != "" {
		e.Outcome, e.Detail = "skipped", o.Skipped
	}
	if e.Outcome == "" { // the buyer host returned before it ran the CLI
		e.Outcome = "start_failed"
	}
	if !store.ValidDetail(e.Detail) {
		e.Detail = "detail_unlisted"
	}
	return e
}

// stop is POST /api/host/runs/{run_id}/stop: only a running run's process
// is stopped; its buyer host records TASK_CANCELLED as it always does.
func (c *control) stop(w http.ResponseWriter, r *http.Request, a *api) {
	var empty struct{}
	if !postGuard(w, r, a, &empty) {
		return
	}
	id := r.PathValue("run_id")
	c.mu.Lock()
	if lr := c.active; lr != nil && lr.run.RunID == id {
		if lr.state == "running" {
			lr.state = "stopping"
			lr.cancel()
		}
		v := c.view(lr.run)
		c.mu.Unlock()
		reply(w, http.StatusAccepted, v)
		return
	}
	c.mu.Unlock()
	if _, found, err := c.st.GetRun(id); err != nil {
		reply(w, http.StatusServiceUnavailable, map[string]string{"error": "store_unavailable", "code": store.CodeOf(err)})
	} else if found {
		fail(w, http.StatusConflict, "run_not_running")
	} else {
		fail(w, http.StatusNotFound, "not_found")
	}
}

func (c *control) get(w http.ResponseWriter, r *http.Request, a *api) {
	if !reader(w, r, a) {
		return
	}
	c.mu.Lock()
	run, found, err := c.st.GetRun(r.PathValue("run_id"))
	var v runView
	if err == nil && found {
		v = c.view(run)
	}
	c.mu.Unlock()
	switch {
	case err != nil:
		reply(w, http.StatusServiceUnavailable, map[string]string{"error": "store_unavailable", "code": store.CodeOf(err)})
	case !found:
		fail(w, http.StatusNotFound, "not_found")
	default:
		reply(w, http.StatusOK, v)
	}
}

// list is GET /api/host/runs: the latest 20 runs, newest first.
func (c *control) list(w http.ResponseWriter, r *http.Request, a *api) {
	if !reader(w, r, a) {
		return
	}
	c.mu.Lock()
	runs, err := c.st.Runs(20)
	out := make([]runView, 0, len(runs))
	for _, run := range runs {
		out = append(out, c.view(run))
	}
	c.mu.Unlock()
	if err != nil {
		reply(w, http.StatusServiceUnavailable, map[string]string{"error": "store_unavailable", "code": store.CodeOf(err)})
		return
	}
	reply(w, http.StatusOK, map[string]any{"runs": out})
}

// summary is the status's runs field: the scenarios the host offers (id,
// consumer, label: trusted catalog text), the active run and the
// interrupted ones among the latest 20 (IDs, tasks and states only).
func (c *control) summary() map[string]any {
	var scs []map[string]string
	for _, sc := range c.scenarios {
		scs = append(scs, map[string]string{"id": sc.ID, "consumer_id": sc.ConsumerID, "label": sc.Label})
	}
	slices.SortFunc(scs, func(a, b map[string]string) int { return strings.Compare(a["id"], b["id"]) })
	out := map[string]any{"scenarios": scs, "active": nil, "interrupted": []map[string]string{}}
	c.mu.Lock()
	defer c.mu.Unlock()
	runs, err := c.st.Runs(20)
	if err != nil {
		out["error"] = store.CodeOf(err)
		return out
	}
	if c.active != nil {
		out["active"] = map[string]string{"run_id": c.active.run.RunID, "task_id": c.active.run.TaskID, "live": c.active.state}
	}
	var interrupted []map[string]string
	for _, r := range runs {
		if r.State == "interrupted" {
			interrupted = append(interrupted, map[string]string{"run_id": r.RunID, "task_id": r.TaskID})
		}
	}
	if interrupted != nil {
		out["interrupted"] = interrupted
	}
	return out
}
