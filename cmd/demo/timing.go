package main

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/SIMPLYBOYS/agent-payment-authority/internal/store"
	"github.com/SIMPLYBOYS/agent-payment-authority/internal/timing"
)

// writeTiming writes the run's timing record next to its trace
// (docs/multi-agent-demo-design.md 6.2): every time is the host's own,
// taken when it happened, on this host's monotonic clock; the file is
// written once, at the run's end. It answers written or not_written; a
// record that could not be written is said to be missing, never filled in
// later. Fixed codes, tool names and product IDs only.
func (c *control) writeTiming(lr *liveRun, o runOutcome, end store.RunEnd, stopAt, endedAt time.Time) string {
	at := func(t time.Time) *int64 {
		if t.IsZero() {
			return nil
		}
		v := t.Sub(c.started).Milliseconds() // both readings are monotonic
		return &v
	}
	mode := "fake"
	if c.runner.live != nil {
		mode = "live"
	}
	r := timing.Record{
		Version: timing.Version, HostInstance: c.instance, HostStartedUTC: c.started.UTC().Format(time.RFC3339Nano), Clock: timing.Clock,
		RunID: lr.run.RunID, TaskID: lr.run.TaskID, ConsumerID: lr.run.ConsumerID, Mode: mode, Outcome: end.Outcome, StoreEnd: end.StoreEnd,
		Lifecycle: timing.Lifecycle{AcceptedMs: at(lr.accepted), ProcessStartedMs: at(o.ProcessStarted), StopAcceptedMs: at(stopAt),
			ProcessReapedMs: at(o.ProcessReaped), CallsClosedMs: at(o.CallsClosed), RunEndedMs: at(endedAt)},
		Calls: []timing.Call{},
	}
	if !stopAt.IsZero() {
		r.Lifecycle.StopAcceptedFrom = "running" // a stop is only accepted while running
	}
	for _, call := range o.Calls {
		r.Calls = append(r.Calls, timing.Call{ID: call.ID, Tool: call.Tool, ProductID: call.ProductID, StartedMs: at(call.Started), CompletedMs: at(call.Completed), Outcome: call.Outcome})
	}
	r.WrittenUTC = time.Now().UTC().Format(time.RFC3339Nano)
	stamp := o.ProcessStarted // the trace's own name stamp, so the two files pair up
	if stamp.IsZero() {
		stamp = lr.accepted
	}
	raw, _ := json.MarshalIndent(r, "", "  ")
	if err := writeNewFile(c.runner.traceDir, fmt.Sprintf("%s-%d.timing.json", lr.run.TaskID, stamp.UnixNano()), append(raw, '\n')); err != nil {
		fmt.Fprintf(c.out, "demo: run %s: the timing record was not written (%v); its times are missing\n", lr.run.RunID, err)
		return "not_written"
	}
	return "written"
}

// writeNewFile writes a new file whole (never an existing one) in dir.
func writeNewFile(dir, name string, data []byte) error {
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return err
	}
	f, err := os.OpenFile(filepath.Join(dir, name), os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
	if err != nil {
		return err
	}
	if _, err := f.Write(data); err != nil {
		f.Close()
		return err
	}
	return f.Close()
}
