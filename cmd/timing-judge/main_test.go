package main

import (
	"bytes"
	"crypto/sha256"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/SIMPLYBOYS/agent-payment-authority/internal/contracts"
	"github.com/SIMPLYBOYS/agent-payment-authority/internal/policy"
	"github.com/SIMPLYBOYS/agent-payment-authority/internal/store"
	"github.com/SIMPLYBOYS/agent-payment-authority/internal/timing"
)

const t0 = uint64(1791700000)

func ms(v int64) *int64 { return &v }

// record is a valid record of run on task, its process living from p0 to
// p1, its calls starting at starts and each answered 5 ms later.
func record(consumer, run, task, mode string, p0, p1 int64, starts ...int64) timing.Record {
	r := timing.Record{Version: timing.Version, HostInstance: "h1", Clock: timing.Clock, RunID: run, TaskID: task, ConsumerID: consumer, Mode: mode, Outcome: "ok",
		Lifecycle: timing.Lifecycle{AcceptedMs: ms(p0 - 1), ProcessStartedMs: ms(p0), ProcessReapedMs: ms(p1), CallsClosedMs: ms(p1 + 1), RunEndedMs: ms(p1 + 2)}}
	for i, s := range starts {
		r.Calls = append(r.Calls, timing.Call{ID: i + 1, Tool: "list_products", StartedMs: ms(s), CompletedMs: ms(s + 5), Outcome: "ok"})
	}
	return r
}

// stopped is r stopped at 40 ms while running, cancelled in the Store.
func stopped(r timing.Record) timing.Record {
	r.Lifecycle.StopAcceptedMs, r.Lifecycle.StopAcceptedFrom, r.StoreEnd, r.Outcome = ms(40), "running", "TASK_CANCELLED", "cancelled"
	return r
}

func write(t *testing.T, r timing.Record) string {
	raw, _ := json.Marshal(r)
	p := filepath.Join(t.TempDir(), r.RunID+".timing.json")
	os.WriteFile(p, raw, 0o600)
	return p
}

// run is one run as the host leaves it in the database.
type spec struct {
	consumer, task string
	cancel         bool // stopped: TASK_CANCELLED, then the run ends cancelled
	open           bool // never ended: host_runs stays accepted
	cancelLate     bool // ended ok, then its task cancelled (the operator's cancel)
}

// storeDB makes a demo database through the Store itself (its schema,
// host_runs and events as the host writes them) and returns its path and
// each run's ID.
func storeDB(t *testing.T, specs ...spec) (string, []string) {
	t.Helper()
	var reg contracts.RegistryConfig
	raw, err := os.ReadFile("../../testdata/contracts/registry.json")
	if err == nil {
		err = contracts.Decode(raw, &reg)
	}
	if err != nil {
		t.Fatal(err)
	}
	if raw, err = os.ReadFile("../../testdata/policy/m1-demo-profile.json"); err != nil {
		t.Fatal(err)
	}
	profile, err := policy.LoadProfile(raw)
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(t.TempDir(), "db", "demo.db")
	st, err := store.Open(store.Config{Path: path, Registry: reg, Clock: func() uint64 { return t0 }})
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	var ids []string
	for i, sp := range specs {
		run, _, err := st.AcceptRun(store.RunRequest{RequestID: fmt.Sprintf("req-judge-%02d-000000000", i), Scenario: "ask", ScenarioDigest: "sha256:x", ConsumerID: sp.consumer, Candidates: []string{sp.task}})
		if err != nil {
			t.Fatal(err)
		}
		p, cred := profile, policy.Credential{ConsumerID: sp.consumer, ValidUntil: t0 + 86400, EvidenceMode: "fixture"}
		if err := st.StartTask(store.TaskSetup{TaskID: run.TaskID, Profile: &p, Credential: &cred}); err != nil {
			t.Fatal(err)
		}
		switch {
		case sp.open:
		case sp.cancel:
			err = errors.Join(st.Cancel(run.TaskID), st.EndRun(run.RunID, store.RunEnd{Outcome: "cancelled", TaskStarted: true, StoreEnd: "TASK_CANCELLED"}))
		default:
			err = st.EndRun(run.RunID, store.RunEnd{Outcome: "ok", TaskStarted: true})
			if err == nil && sp.cancelLate {
				err = st.Cancel(run.TaskID)
			}
		}
		if err != nil {
			t.Fatal(err)
		}
		ids = append(ids, run.RunID)
	}
	return path, ids
}

func judge(t *testing.T, args ...string) (int, map[string]any) {
	var out, errb bytes.Buffer
	code := run(args, &out, &errb)
	var v map[string]any
	json.Unmarshal(out.Bytes(), &v)
	return code, v
}

func problems(v map[string]any) string {
	ps, _ := json.Marshal(v["problems"])
	return string(ps)
}

func TestJudge(t *testing.T) {
	d, ids := storeDB(t, spec{consumer: "consumer-a", task: "task-a"}, spec{consumer: "consumer-b", task: "task-b"})
	a := record("consumer-a", ids[0], "task-a", "live", 0, 100, 10, 30, 50)
	b := record("consumer-b", ids[1], "task-b", "live", 5, 120, 20, 40, 90)
	before, _ := os.ReadFile(d)
	sum := sha256.Sum256(before)
	if code, v := judge(t, "--db", d, "--a", write(t, a), "--b", write(t, b)); code != 0 || v["conclusion"] != timing.Interleaved {
		t.Fatalf("interleaved: %d %v", code, v)
	}
	after, _ := os.ReadFile(d)
	if sha256.Sum256(after) != sum {
		t.Error("the database changed")
	}
	// Fake records: refused unless --fake, and then said to be fake.
	fa, fb := a, b
	fa.Mode, fb.Mode = "fake", "fake"
	if code, v := judge(t, "--db", d, "--a", write(t, fa), "--b", write(t, fb)); code != 2 || v["conclusion"] != timing.NotJudged {
		t.Fatalf("fake without --fake: %d %v", code, v)
	}
	if code, v := judge(t, "--db", d, "--a", write(t, fa), "--b", write(t, fb), "--fake"); code != 0 || v["fake"] != true || !strings.HasPrefix(v["text"].(string), "（fake 紀錄") {
		t.Fatalf("fake with --fake: %d %v", code, v)
	}
	// No host_runs row for B: not judged.
	one, oneIDs := storeDB(t, spec{consumer: "consumer-a", task: "task-a"})
	a1 := a
	a1.RunID = oneIDs[0]
	if code, v := judge(t, "--db", one, "--a", write(t, a1), "--b", write(t, b)); code != 2 || v["conclusion"] != timing.NotJudged {
		t.Fatalf("no row: %d %v", code, v)
	}
	// A stop case that did not happen: judged, not reached.
	if code, v := judge(t, "--db", d, "--a", write(t, a), "--b", write(t, b), "--stop", "a"); code != 1 || v["stop_isolation"].(map[string]any)["held"] != false {
		t.Fatalf("stop: %d %v", code, v)
	}
	// One after the other: judged, not reached.
	late := record("consumer-b", ids[1], "task-b", "live", 200, 300, 210, 220)
	if code, v := judge(t, "--db", d, "--a", write(t, a), "--b", write(t, late)); code != 1 || v["conclusion"] != timing.NoWorkOverlap {
		t.Fatalf("sequential: %d %v", code, v)
	}
	// A file that is not a record, and usage.
	bad := filepath.Join(t.TempDir(), "bad.json")
	os.WriteFile(bad, []byte(`{"version":1}`), 0o600)
	if code, _ := judge(t, "--db", d, "--a", bad, "--b", write(t, b)); code != 2 {
		t.Fatalf("bad file: %d", code)
	}
	if code, _ := judge(t, "--db", d, "--a", write(t, a)); code != 2 {
		t.Fatalf("usage: %d", code)
	}
}

// The records against the database, read by the command (review of eed0f90,
// R2). The agreeing case is a stop of A with B going on; every other case
// changes one thing of it and must be refused by the check its detail names.
func TestRecordsAgainstTheDatabase(t *testing.T) {
	recs := func(ids []string) (timing.Record, timing.Record) {
		return stopped(record("consumer-a", ids[0], "task-a", "live", 0, 60, 10, 30)), record("consumer-b", ids[1], "task-b", "live", 5, 120, 20, 50, 90)
	}
	d, ids := storeDB(t, spec{consumer: "consumer-a", task: "task-a", cancel: true}, spec{consumer: "consumer-b", task: "task-b"})
	a, b := recs(ids)
	code, v := judge(t, "--db", d, "--a", write(t, a), "--b", write(t, b), "--stop", "a")
	if code != 0 || v["conclusion"] != timing.Interleaved || v["stop_isolation"].(map[string]any)["held"] != true {
		t.Fatalf("agreeing: %d %v", code, v)
	}

	t.Run("A's record says TASK_CANCELLED, the database has no such event", func(t *testing.T) {
		d, ids := storeDB(t, spec{consumer: "consumer-a", task: "task-a", cancel: true}, spec{consumer: "consumer-b", task: "task-b"})
		conn, err := sql.Open("sqlite", d)
		if err == nil {
			_, err = conn.Exec(`DELETE FROM events WHERE task_id = 'task-a' AND event_type = 'TASK_CANCELLED'`)
			conn.Close()
		}
		if err != nil {
			t.Fatal(err)
		}
		a, b := recs(ids)
		code, v := judge(t, "--db", d, "--a", write(t, a), "--b", write(t, b), "--stop", "a")
		if code != 2 || v["conclusion"] != timing.NotJudged || v["stop_isolation"] != nil || !strings.Contains(problems(v), "is not the task's end event in the Store") {
			t.Fatalf("want not judged, A's cancel has no event in the Store: %d %v", code, v)
		}
	})
	t.Run("A's record says TASK_CANCELLED, the database says A ended ok", func(t *testing.T) {
		d, ids := storeDB(t, spec{consumer: "consumer-a", task: "task-a"}, spec{consumer: "consumer-b", task: "task-b"})
		a, b := recs(ids)
		code, v := judge(t, "--db", d, "--a", write(t, a), "--b", write(t, b), "--stop", "a")
		if code != 2 || v["conclusion"] != timing.NotJudged || !strings.Contains(problems(v), "differ from host_runs") {
			t.Fatalf("want not judged, host_runs says A ended ok: %d %v", code, v)
		}
	})
	t.Run("B was cancelled in the database, its record says it ended on its own", func(t *testing.T) {
		d, ids := storeDB(t, spec{consumer: "consumer-a", task: "task-a", cancel: true}, spec{consumer: "consumer-b", task: "task-b", cancelLate: true})
		a, b := recs(ids)
		code, v := judge(t, "--db", d, "--a", write(t, a), "--b", write(t, b), "--stop", "a")
		s, _ := v["stop_isolation"].(map[string]any)
		if code != 1 || s == nil || s["held"] != false || !strings.Contains(fmt.Sprint(s["missing"]), "在 Store 有 TASK_CANCELLED") {
			t.Fatalf("want stop isolation not held, B cancelled in the Store: %d %v", code, v)
		}
	})
	t.Run("B's record says it ended, host_runs says it is still accepted", func(t *testing.T) {
		d, ids := storeDB(t, spec{consumer: "consumer-a", task: "task-a", cancel: true}, spec{consumer: "consumer-b", task: "task-b", open: true})
		a, b := recs(ids)
		code, v := judge(t, "--db", d, "--a", write(t, a), "--b", write(t, b), "--stop", "a")
		if code != 2 || v["conclusion"] != timing.NotJudged || !strings.Contains(problems(v), "host_runs says it is accepted") {
			t.Fatalf("want not judged, host_runs says B is accepted: %d %v", code, v)
		}
	})
	t.Run("B's record has another outcome than host_runs", func(t *testing.T) {
		a, b := recs(ids)
		b.Outcome = "timeout"
		code, v := judge(t, "--db", d, "--a", write(t, a), "--b", write(t, b), "--stop", "a")
		if code != 2 || v["conclusion"] != timing.NotJudged || !strings.Contains(problems(v), "differ from host_runs") {
			t.Fatalf("want not judged, B's outcome differs from host_runs: %d %v", code, v)
		}
	})
	t.Run("a database without the Store's events", func(t *testing.T) {
		p := filepath.Join(t.TempDir(), "demo.db")
		conn, err := sql.Open("sqlite", p)
		if err == nil {
			_, err = conn.Exec(`CREATE TABLE host_runs (run_id TEXT, task_id TEXT, consumer_id TEXT, state TEXT, outcome TEXT, store_end TEXT)`)
			conn.Close()
		}
		if err != nil {
			t.Fatal(err)
		}
		code, v := judge(t, "--db", p, "--a", write(t, a), "--b", write(t, b))
		if code != 2 || v["conclusion"] != timing.NotJudged || !strings.Contains(problems(v), "cannot be read") {
			t.Fatalf("%d %v", code, v)
		}
	})
	t.Run("B did not reach interleaving and no stop: judged, not reached", func(t *testing.T) {
		d, ids := storeDB(t, spec{consumer: "consumer-a", task: "task-a"}, spec{consumer: "consumer-b", task: "task-b"})
		a := record("consumer-a", ids[0], "task-a", "live", 0, 100, 10, 30)
		b := record("consumer-b", ids[1], "task-b", "live", 5, 120, 50, 90)
		code, v := judge(t, "--db", d, "--a", write(t, a), "--b", write(t, b), "--stop", "a")
		if code != 1 || v["conclusion"] != timing.ProcessesOnly || v["stop_isolation"].(map[string]any)["held"] != false {
			t.Fatalf("%d %v", code, v)
		}
	})
}

type failing struct{}

func (failing) Write([]byte) (int, error) { return 0, errors.New("disk full") }

// short writes half and says nothing (a writer breaking io.Writer's rule).
type short struct{ bytes.Buffer }

func (s *short) Write(p []byte) (int, error) { return s.Buffer.Write(p[:len(p)/2]) }

// C1 (review of eed0f90): a report that was not written in full is no
// verdict: exit 3, a short reason on stderr, whatever the judgement.
func TestReportWrite(t *testing.T) {
	d, ids := storeDB(t, spec{consumer: "consumer-a", task: "task-a"}, spec{consumer: "consumer-b", task: "task-b"})
	args := []string{"--db", d, "--a", write(t, record("consumer-a", ids[0], "task-a", "live", 0, 100, 10, 30, 50)),
		"--b", write(t, record("consumer-b", ids[1], "task-b", "live", 5, 120, 20, 40, 90))}
	for name, w := range map[string]io.Writer{"a write error": failing{}, "a short write without an error": &short{}} {
		t.Run(name, func(t *testing.T) {
			var errb bytes.Buffer
			if code := run(args, w, &errb); code != 3 || !strings.Contains(errb.String(), "not written in full") {
				t.Fatalf("want exit 3 and the reason: exit %d, stderr %q", code, errb.String())
			}
		})
	}
	var out, errb bytes.Buffer
	if code := run(args, &out, &errb); code != 0 {
		t.Fatalf("exit %d", code)
	}
	dec := json.NewDecoder(&out)
	var v map[string]any
	if err := dec.Decode(&v); err != nil || v["conclusion"] != timing.Interleaved {
		t.Fatalf("%v %v", v, err)
	}
	if err := dec.Decode(&v); err != io.EOF {
		t.Fatalf("more than one report: %v", err)
	}
}
