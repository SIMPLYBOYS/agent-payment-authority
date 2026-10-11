// Package timing is the demo host's timing record of one run and the
// read-only judgement of two runs' records (docs/multi-agent-demo-design.md
// 6.2-6.5): when the host accepted the run, started and reaped its process,
// accepted a stop, and took and answered each tool call, on one monotonic
// clock per host instance. It keeps fixed codes, tool names and product IDs
// only: no model text, token, capability or credential.
//
// The judgement says only what the records show: whether two runs' tool
// calls interleaved on one host. It never says that the runs worked at every
// moment of a span, nor anything about the model provider.
package timing

import (
	"bytes"
	"cmp"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"slices"
)

// Version of the record format.
const Version = "apa.demo.timing.v1"

// Clock names the time base of every *_ms field.
const Clock = "monotonic_ms_since_host_start"

// Record is one run's timing record (one file per run).
type Record struct {
	Version        string    `json:"version"`
	HostInstance   string    `json:"host_instance"`
	HostStartedUTC string    `json:"host_started_utc"`
	Clock          string    `json:"clock"`
	RunID          string    `json:"run_id"`
	TaskID         string    `json:"task_id"`
	ConsumerID     string    `json:"consumer_id"`
	Mode           string    `json:"mode"`      // fake | live
	Outcome        string    `json:"outcome"`   // the run's end code (host_runs)
	StoreEnd       string    `json:"store_end"` // TASK_CANCELLED, TASK_TIMED_OUT or ""
	Lifecycle      Lifecycle `json:"lifecycle"`
	Calls          []Call    `json:"calls"`
	WrittenUTC     string    `json:"written_utc"` // when the file was written, not when anything happened
}

// Lifecycle holds the run's times; nil when the moment did not happen.
type Lifecycle struct {
	AcceptedMs       *int64 `json:"accepted_ms"`
	ProcessStartedMs *int64 `json:"process_started_ms"`
	StopAcceptedMs   *int64 `json:"stop_accepted_ms"`
	StopAcceptedFrom string `json:"stop_accepted_from"` // the run's state when the stop was accepted: running, or "" without a stop
	ProcessReapedMs  *int64 `json:"process_reaped_ms"`
	CallsClosedMs    *int64 `json:"calls_closed_ms"` // no call is taken or answered after it
	RunEndedMs       *int64 `json:"run_ended_ms"`
}

// Call is one tool call the host took. CompletedMs is nil for a call that
// was cut off without an answer; it is never filled in afterwards.
type Call struct {
	ID          int    `json:"id"`
	Tool        string `json:"tool"`
	ProductID   string `json:"product_id"`
	StartedMs   *int64 `json:"started_ms"`
	CompletedMs *int64 `json:"completed_ms"`
	Outcome     string `json:"outcome"`
}

// HostRun is what the database says of a run, for the cross-check: its
// host_runs row (outcome and store_end are "" until the run ended) and its
// task's end event in the Store's events (TASK_CANCELLED, TASK_TIMED_OUT,
// or "" when the task has none; there is at most one).
type HostRun struct {
	RunID, TaskID, ConsumerID, State string
	Outcome, StoreEnd                string
	TaskEnd                          string
}

// ReadHostRun reads a run's HostRun from a demo database (the caller opens
// it, read only): nil when host_runs has no such run, an error when the
// database cannot be read.
func ReadHostRun(conn *sql.DB, runID string) (*HostRun, error) {
	var r HostRun
	err := conn.QueryRow(`SELECT h.run_id, h.task_id, h.consumer_id, h.state, COALESCE(h.outcome, ''), COALESCE(h.store_end, ''),
		COALESCE((SELECT e.event_type FROM events e WHERE e.task_id = h.task_id AND e.event_type IN ('TASK_CANCELLED', 'TASK_TIMED_OUT')), '')
		FROM host_runs h WHERE h.run_id = ?`, runID).Scan(&r.RunID, &r.TaskID, &r.ConsumerID, &r.State, &r.Outcome, &r.StoreEnd, &r.TaskEnd)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("the database's host_runs or events cannot be read for run %s", runID)
	}
	return &r, nil
}

// Problem kinds (section 6.3), worst first.
const (
	Incomplete    = "incomplete"     // a file, field or row is missing; the version is unknown
	Invalid       = "invalid"        // the record contradicts itself or host_runs
	NotComparable = "not_comparable" // two different host instances
	NotLive       = "not_live"       // a fake record where a live one is required
)

// Problem is one reason a record cannot be judged.
type Problem struct {
	Kind   string `json:"kind"`
	Detail string `json:"detail"`
}

// Parse decodes a record strictly: unknown members are an error.
func Parse(raw []byte) (Record, error) {
	var r Record
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&r); err != nil {
		return Record{}, err
	}
	return r, nil
}

// Check finds what keeps one record from being judged (section 6.3). row is
// the run's host_runs row, or nil when there is none.
func Check(r Record, row *HostRun) []Problem {
	var ps []Problem
	add := func(kind, format string, a ...any) { ps = append(ps, Problem{kind, fmt.Sprintf(format, a...)}) }
	if r.Version != Version || r.Clock != Clock {
		add(Incomplete, "version %q, clock %q", r.Version, r.Clock)
		return ps
	}
	if r.HostInstance == "" || r.RunID == "" || r.TaskID == "" || r.ConsumerID == "" || r.Mode == "" {
		add(Incomplete, "missing host instance, run, task, consumer or mode")
	}
	l := r.Lifecycle
	if l.AcceptedMs == nil || l.ProcessStartedMs == nil || l.ProcessReapedMs == nil || l.CallsClosedMs == nil || l.RunEndedMs == nil {
		add(Incomplete, "missing a lifecycle time (no process ran, or the record was cut short)")
	}
	// The record says the run ended, with an outcome and a Store end; the
	// database must say the same (review of eed0f90, R2). A Store end the
	// record claims must be the task's end event in the Store. The other way
	// round is not a contradiction: a task may be cancelled after its run
	// ended (the operator's cancel), so a Store end without one in the
	// record is left to StopIsolation.
	switch {
	case row == nil:
		add(Incomplete, "no host_runs row for %s", r.RunID)
	case row.RunID != r.RunID || row.TaskID != r.TaskID || row.ConsumerID != r.ConsumerID:
		add(Invalid, "the record's run, task or consumer differ from host_runs")
	case row.State != "ended":
		add(Invalid, "the record says the run ended; host_runs says it is %s", row.State)
	case row.Outcome != r.Outcome || row.StoreEnd != r.StoreEnd:
		add(Invalid, "the record's outcome or Store end (%s, %q) differ from host_runs (%s, %q)", r.Outcome, r.StoreEnd, row.Outcome, row.StoreEnd)
	case r.StoreEnd != "" && row.TaskEnd != r.StoreEnd:
		add(Invalid, "the record's Store end %s is not the task's end event in the Store (%q)", r.StoreEnd, row.TaskEnd)
	}
	if len(ps) > 0 {
		return ps
	}
	if *l.AcceptedMs > *l.ProcessStartedMs || *l.ProcessStartedMs > *l.ProcessReapedMs ||
		*l.ProcessReapedMs > *l.CallsClosedMs || *l.CallsClosedMs > *l.RunEndedMs {
		add(Invalid, "lifecycle times go backwards")
	}
	if l.StopAcceptedMs != nil && (*l.StopAcceptedMs < *l.AcceptedMs || *l.StopAcceptedMs > *l.RunEndedMs || l.StopAcceptedFrom == "") {
		add(Invalid, "the stop time is outside the run, or its state is missing")
	}
	if l.StopAcceptedMs == nil && l.StopAcceptedFrom != "" {
		add(Invalid, "a stop state without a stop time")
	}
	seen := map[int]bool{}
	for _, c := range r.Calls {
		switch {
		case seen[c.ID]:
			add(Invalid, "call %d appears twice", c.ID)
		case c.StartedMs == nil:
			add(Invalid, "call %d has no start", c.ID)
		case *c.StartedMs < *l.ProcessStartedMs || *c.StartedMs > *l.ProcessReapedMs:
			add(Invalid, "call %d starts outside the process", c.ID)
		case c.CompletedMs != nil && (*c.CompletedMs < *c.StartedMs || *c.CompletedMs > *l.CallsClosedMs):
			add(Invalid, "call %d completes before it starts or after the run's calls closed", c.ID)
		}
		seen[c.ID] = true
	}
	return ps
}

// Conclusions of Judge (section 6.4).
const (
	Interleaved   = "interleaved"    // the operational overlap: A, B, A, B call starts
	SpansOnly     = "spans_only"     // the tool activity spans meet, the calls do not interleave
	ProcessesOnly = "processes_only" // both processes alive at once, no tool activity together
	NoWorkOverlap = "no_work_overlap"
	NotJudged     = "not_judged" // a record has a problem (section 6.3)
)

// Verdict is the judgement of two runs.
type Verdict struct {
	Conclusion string    `json:"conclusion"`
	Text       string    `json:"text"`
	Problems   []Problem `json:"problems,omitempty"`
	Fake       bool      `json:"fake"` // judged on fake records: never real model work
}

// Judge judges two records (and their host_runs rows). live requires both to
// be live records; fake records are judged only when live is false, and the
// verdict then says so.
func Judge(a, b Record, rowA, rowB *HostRun, live bool) Verdict {
	v := Verdict{Conclusion: NotJudged}
	for _, p := range Check(a, rowA) {
		v.Problems = append(v.Problems, Problem{p.Kind, "A: " + p.Detail})
	}
	for _, p := range Check(b, rowB) {
		v.Problems = append(v.Problems, Problem{p.Kind, "B: " + p.Detail})
	}
	if len(v.Problems) == 0 {
		switch {
		case a.HostInstance != b.HostInstance:
			v.Problems = append(v.Problems, Problem{NotComparable, "the records come from two host instances; their times cannot be compared"})
		case a.ConsumerID == b.ConsumerID || a.RunID == b.RunID || a.TaskID == b.TaskID:
			v.Problems = append(v.Problems, Problem{Invalid, "the two records are not two consumers' runs and tasks"})
		case live && (a.Mode != "live" || b.Mode != "live"):
			v.Problems = append(v.Problems, Problem{NotLive, "a record is not from a live run"})
		}
	}
	if len(v.Problems) > 0 {
		v.Text = "未判定：紀錄不完整、矛盾、來自不同的 Host，或不是 live 紀錄；見 problems。"
		return v
	}
	v.Fake = a.Mode != "live" || b.Mode != "live"
	switch {
	case interleave(starts(a), starts(b)):
		v.Conclusion = Interleaved
		v.Text = fmt.Sprintf("同一 Host 上，%s 與 %s 的工具呼叫交錯發生：雙方都在對方兩次呼叫之間有自己的工具呼叫。這不代表跨度內每一刻都在工作，也不代表觀測到模型供應商內部同時推論。", a.ConsumerID, b.ConsumerID)
	case meets(span(a), span(b)):
		v.Conclusion = SpansOnly
		v.Text = "兩場的工具活動跨度相交，但沒有觀測到交錯的工具呼叫；未確認兩場同時有活動。"
	case meets(process(a), process(b)):
		v.Conclusion = ProcessesOnly
		v.Text = "兩場的程序同時存活，但工具活動沒有重疊；未確認同時工作。"
	default:
		v.Conclusion = NoWorkOverlap
		v.Text = "兩場的程序沒有同時存活；最多只有 task（例如 Ask 等待）重疊，不是工作重疊。"
	}
	if v.Fake {
		v.Text = "（fake 紀錄：不是真實模型的工作）" + v.Text
	}
	return v
}

// starts are a record's completed calls' start times, in order.
func starts(r Record) []int64 {
	var out []int64
	for _, c := range r.Calls {
		if c.StartedMs != nil && c.CompletedMs != nil {
			out = append(out, *c.StartedMs)
		}
	}
	slices.Sort(out)
	return out
}

// interleave: the two sets of call starts, merged in time order, hold an
// A, B, A, B or B, A, B, A run (four starts, alternating, strictly ordered).
// Two calls at the same millisecond do not order each other, so they count
// for neither side.
func interleave(a, b []int64) bool {
	type ev struct {
		t    int64
		side byte
	}
	var all []ev
	for _, t := range a {
		all = append(all, ev{t, 'a'})
	}
	for _, t := range b {
		all = append(all, ev{t, 'b'})
	}
	slices.SortFunc(all, func(x, y ev) int { return cmp.Compare(x.t, y.t) })
	// The longest alternating chain with strictly increasing times.
	for _, first := range []byte{'a', 'b'} {
		want, n, last := first, 0, int64(-1)
		for _, e := range all {
			if e.side == want && (n == 0 || e.t > last) {
				n, last = n+1, e.t
				want = 'a' + 'b' - want
				if n == 4 {
					return true
				}
			}
		}
	}
	return false
}

type interval struct{ from, to int64 }

func meets(x, y *interval) bool { return x != nil && y != nil && x.from <= y.to && y.from <= x.to }

// span is the tool activity span: the first completed call's start to the
// last completed call's answer.
func span(r Record) *interval {
	var s *interval
	for _, c := range r.Calls {
		if c.StartedMs == nil || c.CompletedMs == nil {
			continue
		}
		if s == nil {
			s = &interval{*c.StartedMs, *c.CompletedMs}
		}
		s.from, s.to = min(s.from, *c.StartedMs), max(s.to, *c.CompletedMs)
	}
	return s
}

func process(r Record) *interval {
	l := r.Lifecycle
	return &interval{*l.ProcessStartedMs, *l.ProcessReapedMs}
}

// Stop is the judgement of a stop isolation case (section 6.5): stopped is the
// run that was stopped, other the one that went on.
type Stop struct {
	Held    bool     `json:"held"`
	Text    string   `json:"text"`
	Missing []string `json:"missing,omitempty"`
}

// StopIsolation checks the case on two records that Judge could judge;
// otherRow is the other run's database view (Judge has checked both
// records against theirs).
func StopIsolation(stopped, other Record, otherRow *HostRun) Stop {
	var miss []string
	l := stopped.Lifecycle
	if l.StopAcceptedMs == nil || l.StopAcceptedFrom != "running" {
		miss = append(miss, "沒有在 running 時受理的停止（停止被拒或對已結束的場次）")
	} else {
		at := *l.StopAcceptedMs
		before := false
		for _, c := range stopped.Calls {
			if c.CompletedMs != nil && *c.CompletedMs < at {
				before = true
			}
		}
		if !before {
			miss = append(miss, "被停止的場次在停止受理前沒有完成的工具呼叫")
		}
		if *l.ProcessReapedMs < at || stopped.StoreEnd != "TASK_CANCELLED" {
			miss = append(miss, "被停止的場次沒有在停止後回收並記錄 TASK_CANCELLED")
		}
		o := other.Lifecycle
		if *o.ProcessStartedMs > at || *o.ProcessReapedMs < at {
			miss = append(miss, "另一場在停止受理時不在執行")
		}
		after := false
		for _, c := range other.Calls {
			if c.StartedMs != nil && c.CompletedMs != nil && *c.StartedMs > at {
				after = true
			}
		}
		if !after {
			miss = append(miss, "另一場在停止受理之後沒有自己的工具呼叫")
		}
		if other.StoreEnd == "TASK_CANCELLED" || other.Outcome == "cancelled" {
			miss = append(miss, "另一場也被取消，不是以自己的結果結束")
		}
		if otherRow == nil || otherRow.TaskEnd == "TASK_CANCELLED" {
			miss = append(miss, "另一場的 task 在 Store 有 TASK_CANCELLED（或沒有資料庫紀錄），不能確認以自己的結果結束")
		}
	}
	if len(miss) > 0 {
		return Stop{Text: "停止隔離未示範：" + miss[0], Missing: miss}
	}
	return Stop{Held: true, Text: fmt.Sprintf("停止 %s 於它仍在 running 時受理並回收；%s 在其後仍有自己的工具呼叫，並以自己的結果結束。", stopped.ConsumerID, other.ConsumerID)}
}
