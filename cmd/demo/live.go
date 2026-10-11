package main

import (
	"errors"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
)

// Live mode (docs/cli-poc.md 55): the operator starts a run of the real
// Claude Code CLI through the same buyer host, Gate and live settings as
// apa-buyer run --live; the pages follow the Store as for a fake run.

// liveLabel is the fixed label the pages show in live mode, before any run.
const liveLabel = "真實 Agent 即時展示・開發展示例外・合成資料・不付款"

// liveInfo goes with /api/viewer and /api/host/status in live mode.
type liveInfo struct {
	Record         string `json:"record"` // always "live_demo"
	Label          string `json:"label"`
	DevDemoPlugins bool   `json:"dev_demo_plugins"`
	CLIVersion     string `json:"cli_version"` // the approved version the host checked
}

// traceReply is GET /api/tasks/{task_id}/trace in live mode: the task's
// run trace, whitelisted (tool calls, gate, outcome, CLI-reported usage),
// with request_purchase joined to its Store purchase and events only when
// the verdicts agree. The buyer host writes the trace when the run ends,
// so a running task has none yet: that is said, never filled in.
type traceReply struct {
	TaskID string        `json:"task_id"`
	State  string        `json:"state"`            // running | available | not_available
	Reason string        `json:"reason,omitempty"` // not_available: no_trace | unreadable | inconsistent | ambiguous
	Trace  *historyTrace `json:"trace,omitempty"`
	Note   string        `json:"note"`
}

const traceNote = "工具呼叫來自 CLI 結束後才寫出的白名單 trace，不是逐工具即時串流；請購與事件的對應由 Host 推導；用量是 CLI 自報，不是計費證明。"

// taskTrace answers whoever may read the task (its consumer's viewer, the
// host); others get the task's own 404 or 403.
func (a *api) taskTrace(w http.ResponseWriter, r *http.Request) {
	rec, ok := a.record(w, r)
	if !ok {
		return
	}
	id := rec.Task.TaskID
	out := traceReply{TaskID: id, State: "not_available", Note: traceNote}
	if a.control != nil && a.control.running(id) {
		out.State = "running"
		reply(w, http.StatusOK, out)
		return
	}
	files := a.tracesOf(id)
	switch len(files) {
	case 0:
		out.Reason = "no_trace"
	case 1:
		t, err := loadTrace(files[0], a.st, true)
		switch {
		case errors.Is(err, errTraceDisagree):
			out.Reason = "inconsistent"
		case err != nil || t.TaskID != id:
			out.Reason = "unreadable"
		default:
			out.State, out.Trace = "available", t
		}
	default:
		out.Reason = "ambiguous"
	}
	reply(w, http.StatusOK, out)
}

// tracesOf lists the buyer host's traces of a task: <task_id>-<nanos>.jsonl.
func (a *api) tracesOf(taskID string) []string {
	name := regexp.MustCompile(`^` + regexp.QuoteMeta(taskID) + `-[0-9]+\.jsonl$`)
	entries, _ := os.ReadDir(a.traceDir)
	var out []string
	for _, e := range entries {
		if e.Type().IsRegular() && name.MatchString(e.Name()) {
			out = append(out, filepath.Join(a.traceDir, e.Name()))
		}
	}
	return out
}

// running says whether the run of a task is running or stopping.
func (c *control) running(taskID string) bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.byTask(taskID) != nil
}
