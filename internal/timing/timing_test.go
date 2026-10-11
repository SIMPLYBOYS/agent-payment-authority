package timing

import (
	"strings"
	"testing"
)

func ms(v int64) *int64 { return &v }

// rec is a valid record of a run whose process lives from p0 to p1 and
// whose calls start at the given times, each answered 5 ms later.
func rec(consumer, run, host, mode string, p0, p1 int64, starts ...int64) Record {
	r := Record{Version: Version, HostInstance: host, Clock: Clock, RunID: run, TaskID: "task-" + run, ConsumerID: consumer, Mode: mode, Outcome: "ok",
		Lifecycle: Lifecycle{AcceptedMs: ms(p0 - 1), ProcessStartedMs: ms(p0), ProcessReapedMs: ms(p1), CallsClosedMs: ms(p1 + 1), RunEndedMs: ms(p1 + 2)}}
	for i, s := range starts {
		r.Calls = append(r.Calls, Call{ID: i + 1, Tool: "list_products", StartedMs: ms(s), CompletedMs: ms(s + 5), Outcome: "ok"})
	}
	return r
}

// row is the database's view of r when the two agree.
func row(r Record) *HostRun {
	return &HostRun{RunID: r.RunID, TaskID: r.TaskID, ConsumerID: r.ConsumerID, State: "ended", Outcome: r.Outcome, StoreEnd: r.StoreEnd, TaskEnd: r.StoreEnd}
}

func judge(a, b Record, live bool) Verdict { return Judge(a, b, row(a), row(b), live) }

func kinds(v Verdict) string {
	var k []string
	for _, p := range v.Problems {
		k = append(k, p.Kind)
	}
	return strings.Join(k, ",")
}

// The operational overlap: A, B, A, B call starts on one host.
func TestInterleaved(t *testing.T) {
	a := rec("consumer-a", "ra", "h1", "live", 0, 100, 10, 30, 50)
	b := rec("consumer-b", "rb", "h1", "live", 5, 120, 20, 40, 90)
	v := judge(a, b, true)
	if v.Conclusion != Interleaved || v.Fake || !strings.Contains(v.Text, "不代表跨度內每一刻都在工作") {
		t.Fatalf("%+v", v)
	}
	// B first works too (B, A, B, A).
	if v := judge(b, a, true); v.Conclusion != Interleaved {
		t.Fatalf("reversed: %+v", v)
	}
	// Fake records are judged only when live is not required, and say so.
	fa, fb := rec("consumer-a", "ra", "h1", "fake", 0, 100, 10, 30), rec("consumer-b", "rb", "h1", "fake", 0, 100, 20, 40)
	if v := judge(fa, fb, false); v.Conclusion != Interleaved || !v.Fake || !strings.HasPrefix(v.Text, "（fake 紀錄") {
		t.Fatalf("fake: %+v", v)
	}
	if v := judge(fa, fb, true); v.Conclusion != NotJudged || kinds(v) != NotLive {
		t.Fatalf("fake where live is required: %+v", v)
	}
}

// What does not count as interleaving.
func TestNotInterleaved(t *testing.T) {
	for name, tc := range map[string]struct {
		a, b Record
		want string
	}{
		// A's span 0..105 holds both of B's calls, but nothing of A's after
		// them: a span with a quiet gap, not interleaving.
		"spans meet, a quiet gap":       {rec("consumer-a", "ra", "h1", "live", 0, 200, 0, 100), rec("consumer-b", "rb", "h1", "live", 0, 200, 40, 45), SpansOnly},
		"one call each, both in flight": {rec("consumer-a", "ra", "h1", "live", 0, 200, 10), rec("consumer-b", "rb", "h1", "live", 0, 200, 12), SpansOnly},
		"processes only":                {rec("consumer-a", "ra", "h1", "live", 0, 200, 10, 20), rec("consumer-b", "rb", "h1", "live", 100, 300, 150, 160), ProcessesOnly},
		"one after the other":           {rec("consumer-a", "ra", "h1", "live", 0, 100, 10, 20), rec("consumer-b", "rb", "h1", "live", 200, 300, 210, 220), NoWorkOverlap},
		"same millisecond":              {rec("consumer-a", "ra", "h1", "live", 0, 200, 10, 30), rec("consumer-b", "rb", "h1", "live", 0, 200, 10, 30), SpansOnly},
	} {
		t.Run(name, func(t *testing.T) {
			if v := judge(tc.a, tc.b, true); v.Conclusion != tc.want {
				t.Fatalf("got %s, want %s: %+v", v.Conclusion, tc.want, v)
			}
		})
	}
}

// Records that cannot be judged (section 6.3): never an overlap.
func TestNotJudged(t *testing.T) {
	good := func() (Record, Record) {
		return rec("consumer-a", "ra", "h1", "live", 0, 100, 10, 30, 50), rec("consumer-b", "rb", "h1", "live", 5, 120, 20, 40, 90)
	}
	for name, tc := range map[string]struct {
		edit func(a, b *Record)
		want string
	}{
		"another host":         {func(a, b *Record) { b.HostInstance = "h2" }, NotComparable},
		"unknown version":      {func(a, b *Record) { a.Version = "v0" }, Incomplete},
		"no process":           {func(a, b *Record) { a.Lifecycle.ProcessStartedMs = nil }, Incomplete},
		"times go backwards":   {func(a, b *Record) { a.Lifecycle.ProcessReapedMs = ms(-5) }, Invalid},
		"call before process":  {func(a, b *Record) { a.Calls[0].StartedMs = ms(-50) }, Invalid},
		"call after process":   {func(a, b *Record) { a.Calls[2].StartedMs = ms(500); a.Calls[2].CompletedMs = ms(501) }, Invalid},
		"answer before start":  {func(a, b *Record) { a.Calls[1].CompletedMs = ms(1) }, Invalid},
		"no start":             {func(a, b *Record) { a.Calls[1].StartedMs = nil }, Invalid},
		"one id twice":         {func(a, b *Record) { a.Calls[1].ID = 1 }, Invalid},
		"one consumer":         {func(a, b *Record) { b.ConsumerID = "consumer-a" }, Invalid},
		"stop state, no time":  {func(a, b *Record) { a.Lifecycle.StopAcceptedFrom = "running" }, Invalid},
		"stop time, no state":  {func(a, b *Record) { a.Lifecycle.StopAcceptedMs = ms(20) }, Invalid},
		"answer after closing": {func(a, b *Record) { a.Calls[2].CompletedMs = ms(102) }, Invalid},
	} {
		t.Run(name, func(t *testing.T) {
			a, b := good()
			tc.edit(&a, &b)
			v := judge(a, b, true)
			if v.Conclusion != NotJudged || !strings.Contains(kinds(v), tc.want) {
				t.Fatalf("got %s %q, want %s", v.Conclusion, kinds(v), tc.want)
			}
		})
	}
	// host_runs: no row, or a row that names another task.
	a, b := good()
	if v := Judge(a, b, nil, row(b), true); v.Conclusion != NotJudged || kinds(v) != Incomplete {
		t.Fatalf("no row: %+v", v)
	}
	other := row(a)
	other.TaskID = "task-x"
	if v := Judge(a, b, other, row(b), true); v.Conclusion != NotJudged || kinds(v) != Invalid {
		t.Fatalf("row mismatch: %+v", v)
	}
}

// The records against the database (review of eed0f90, R2): each case
// changes one thing of an agreeing pair, and must be refused by the check
// it names (the detail), not by another field.
func TestRecordVsDatabase(t *testing.T) {
	agree := func() (Record, Record, *HostRun, *HostRun) {
		a := rec("consumer-a", "ra", "h1", "live", 0, 60, 10, 30)
		a.Lifecycle.StopAcceptedMs, a.Lifecycle.StopAcceptedFrom, a.StoreEnd, a.Outcome = ms(40), "running", "TASK_CANCELLED", "cancelled"
		b := rec("consumer-b", "rb", "h1", "live", 5, 120, 20, 50, 90)
		return a, b, row(a), row(b)
	}
	a, b, ra, rb := agree()
	if v := Judge(a, b, ra, rb, true); v.Conclusion != Interleaved {
		t.Fatalf("agreeing records: %+v", v)
	}
	for name, tc := range map[string]struct {
		edit func(a, b *Record, ra, rb *HostRun)
		want string
	}{
		"A's TASK_CANCELLED has no event in the Store": {func(a, b *Record, ra, rb *HostRun) { ra.TaskEnd = "" }, "is not the task's end event in the Store"},
		"B cancelled in host_runs, the record says ok": {func(a, b *Record, ra, rb *HostRun) {
			rb.Outcome, rb.StoreEnd, rb.TaskEnd = "cancelled", "TASK_CANCELLED", "TASK_CANCELLED"
		}, "differ from host_runs"},
		"B's run not ended in host_runs":   {func(a, b *Record, ra, rb *HostRun) { rb.State, rb.Outcome, rb.StoreEnd = "accepted", "", "" }, "host_runs says it is accepted"},
		"B's run interrupted in host_runs": {func(a, b *Record, ra, rb *HostRun) { rb.State, rb.Outcome, rb.StoreEnd = "interrupted", "", "" }, "host_runs says it is interrupted"},
		"B's outcome differs":              {func(a, b *Record, ra, rb *HostRun) { rb.Outcome = "timeout" }, "differ from host_runs"},
		"A's Store end differs":            {func(a, b *Record, ra, rb *HostRun) { ra.StoreEnd = "" }, "differ from host_runs"},
	} {
		t.Run(name, func(t *testing.T) {
			a, b, ra, rb := agree()
			tc.edit(&a, &b, ra, rb)
			v := Judge(a, b, ra, rb, true)
			if v.Conclusion != NotJudged || kinds(v) != Invalid || !strings.Contains(v.Problems[0].Detail, tc.want) {
				t.Fatalf("got %s %+v, want invalid: %s", v.Conclusion, v.Problems, tc.want)
			}
		})
	}
	// Not every end other than ok is a contradiction: B timed out on its
	// own, and the database says so.
	a, b, ra, _ = agree()
	b.Outcome, b.StoreEnd = "timeout", "TASK_TIMED_OUT"
	if v := Judge(a, b, ra, row(b), true); v.Conclusion != Interleaved {
		t.Fatalf("B timed out: %+v", v)
	}
	if s := StopIsolation(a, b, row(b)); !s.Held {
		t.Fatalf("B timed out on its own: %+v", s)
	}
}

// A call cut off without an answer stays unanswered: valid, and not counted.
func TestInterruptedCall(t *testing.T) {
	a := rec("consumer-a", "ra", "h1", "live", 0, 100, 10, 30, 50)
	b := rec("consumer-b", "rb", "h1", "live", 5, 120, 20, 40, 90)
	a.Calls[1].CompletedMs, a.Calls[1].Outcome = nil, "interrupted"
	if ps := Check(a, row(a)); len(ps) != 0 {
		t.Fatalf("problems: %v", ps)
	}
	// Without A's 30 ms call the starts are 10a 20b 40b 50a 90b: still A, B, A, B.
	if v := judge(a, b, true); v.Conclusion != Interleaved {
		t.Fatalf("%+v", v)
	}
	a.Calls[2].CompletedMs = nil // now only A's 10 ms call counts
	if v := judge(a, b, true); v.Conclusion == Interleaved {
		t.Fatalf("an unanswered call counted: %+v", v)
	}
}

func TestParseStrict(t *testing.T) {
	if _, err := Parse([]byte(`{"version":"apa.demo.timing.v1","extra":1}`)); err == nil {
		t.Fatal("an unknown member was accepted")
	}
	if r, err := Parse([]byte(`{"version":"apa.demo.timing.v1","calls":[{"id":1,"started_ms":3,"completed_ms":null}]}`)); err != nil || r.Calls[0].CompletedMs != nil {
		t.Fatalf("%+v %v", r, err)
	}
}

// Stop isolation (section 6.5).
func TestStopIsolation(t *testing.T) {
	stopped := func() (Record, Record) {
		a := rec("consumer-a", "ra", "h1", "live", 0, 60, 10, 30)
		a.Lifecycle.StopAcceptedMs, a.Lifecycle.StopAcceptedFrom, a.StoreEnd, a.Outcome = ms(40), "running", "TASK_CANCELLED", "cancelled"
		b := rec("consumer-b", "rb", "h1", "live", 5, 120, 20, 50, 90)
		return a, b
	}
	a, b := stopped()
	if s := StopIsolation(a, b, row(b)); !s.Held {
		t.Fatalf("%+v", s)
	}
	// B's task cancelled in the Store (after its run, as the operator's
	// cancel can): the records agree with host_runs, but B did not end with
	// its own result as far as the database shows.
	late := row(b)
	late.TaskEnd = "TASK_CANCELLED"
	if v := Judge(a, b, row(a), late, true); v.Conclusion != Interleaved {
		t.Fatalf("judged: %+v", v)
	}
	if s := StopIsolation(a, b, late); s.Held || !strings.Contains(strings.Join(s.Missing, " "), "在 Store 有 TASK_CANCELLED") {
		t.Fatalf("B cancelled in the Store: %+v", s)
	}
	for name, edit := range map[string]func(a, b *Record){
		"A had ended: the stop was refused": func(a, b *Record) { a.Lifecycle.StopAcceptedMs, a.Lifecycle.StopAcceptedFrom = nil, "" },
		"no call of A before the stop": func(a, b *Record) {
			a.Calls = a.Calls[1:]
			a.Calls[0].StartedMs, a.Calls[0].CompletedMs = ms(41), ms(45)
		},
		"A not cancelled":             func(a, b *Record) { a.StoreEnd = "" },
		"B not running at the stop":   func(a, b *Record) { b.Lifecycle.ProcessReapedMs = ms(39) },
		"no call of B after the stop": func(a, b *Record) { b.Calls = b.Calls[:1] },
		"B cancelled too":             func(a, b *Record) { b.StoreEnd = "TASK_CANCELLED" },
	} {
		t.Run(name, func(t *testing.T) {
			a, b := stopped()
			edit(&a, &b)
			if s := StopIsolation(a, b, row(b)); s.Held || !strings.HasPrefix(s.Text, "停止隔離未示範") {
				t.Fatalf("%+v", s)
			}
		})
	}
}
