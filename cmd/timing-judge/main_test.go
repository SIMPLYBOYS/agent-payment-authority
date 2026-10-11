package main

import (
	"bytes"
	"crypto/sha256"
	"database/sql"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/SIMPLYBOYS/agent-payment-authority/internal/timing"
)

func ms(v int64) *int64 { return &v }

func record(consumer, run, mode string, p0, p1 int64, starts ...int64) timing.Record {
	r := timing.Record{Version: timing.Version, HostInstance: "h1", Clock: timing.Clock, RunID: run, TaskID: "task-" + run, ConsumerID: consumer, Mode: mode, Outcome: "ok",
		Lifecycle: timing.Lifecycle{AcceptedMs: ms(p0 - 1), ProcessStartedMs: ms(p0), ProcessReapedMs: ms(p1), CallsClosedMs: ms(p1 + 1), RunEndedMs: ms(p1 + 2)}}
	for i, s := range starts {
		r.Calls = append(r.Calls, timing.Call{ID: i + 1, Tool: "list_products", StartedMs: ms(s), CompletedMs: ms(s + 5), Outcome: "ok"})
	}
	return r
}

func write(t *testing.T, r timing.Record) string {
	raw, _ := json.Marshal(r)
	p := filepath.Join(t.TempDir(), r.RunID+".timing.json")
	os.WriteFile(p, raw, 0o600)
	return p
}

// db makes a database with the host_runs columns the judge reads.
func db(t *testing.T, rows ...timing.Record) string {
	p := filepath.Join(t.TempDir(), "demo.db")
	conn, err := sql.Open("sqlite", p)
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	conn.Exec(`CREATE TABLE host_runs (run_id TEXT, task_id TEXT, consumer_id TEXT, state TEXT)`)
	for _, r := range rows {
		conn.Exec(`INSERT INTO host_runs VALUES (?, ?, ?, 'ended')`, r.RunID, r.TaskID, r.ConsumerID)
	}
	return p
}

func judge(t *testing.T, args ...string) (int, map[string]any) {
	var out, errb bytes.Buffer
	code := run(args, &out, &errb)
	var v map[string]any
	json.Unmarshal(out.Bytes(), &v)
	return code, v
}

func TestJudge(t *testing.T) {
	a := record("consumer-a", "ra", "live", 0, 100, 10, 30, 50)
	b := record("consumer-b", "rb", "live", 5, 120, 20, 40, 90)
	d := db(t, a, b)
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
	fa, fb := record("consumer-a", "fa", "fake", 0, 100, 10, 30), record("consumer-b", "fb", "fake", 0, 100, 20, 40)
	fd := db(t, fa, fb)
	if code, v := judge(t, "--db", fd, "--a", write(t, fa), "--b", write(t, fb)); code != 2 || v["conclusion"] != timing.NotJudged {
		t.Fatalf("fake without --fake: %d %v", code, v)
	}
	if code, v := judge(t, "--db", fd, "--a", write(t, fa), "--b", write(t, fb), "--fake"); code != 0 || v["fake"] != true || !strings.HasPrefix(v["text"].(string), "（fake 紀錄") {
		t.Fatalf("fake with --fake: %d %v", code, v)
	}
	// No host_runs row for B: not judged.
	if code, v := judge(t, "--db", db(t, a), "--a", write(t, a), "--b", write(t, b)); code != 2 || v["conclusion"] != timing.NotJudged {
		t.Fatalf("no row: %d %v", code, v)
	}
	// A stop case that did not happen: judged, not reached.
	if code, v := judge(t, "--db", d, "--a", write(t, a), "--b", write(t, b), "--stop", "a"); code != 1 || v["stop_isolation"].(map[string]any)["held"] != false {
		t.Fatalf("stop: %d %v", code, v)
	}
	// One after the other: judged, not reached.
	late := record("consumer-b", "rb", "live", 200, 300, 210, 220)
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
