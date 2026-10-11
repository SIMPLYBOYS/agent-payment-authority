// Command timing-judge judges two demo runs' timing records, read only
// (docs/multi-agent-demo-design.md 6.3-6.6):
//
//	timing-judge --db DEMO_DB --a A.timing.json --b B.timing.json [--stop a|b] [--fake]
//
// It reads the two records and the runs' host_runs rows (the database is
// opened read only) and prints one JSON verdict: the conclusion and its text
// (interleaved, spans_only, processes_only, no_work_overlap, not_judged),
// the problems that kept it from judging, and with --stop the stop isolation
// case for the stopped run. Live records are required unless --fake, and a
// fake verdict says it is fake. It writes nothing.
//
// It reads each run's host_runs row (state, outcome, store_end) and its
// task's end event in the Store's events, and checks the records against
// them: a record that the database contradicts is not judged.
//
// Exit codes: 0 interleaved (and, with --stop, the stop isolation held);
// 1 judged, not reached; 2 not judged, or usage; 3 the report could not be
// written in full (a write error or a short write): there is no verdict,
// whatever the judgement was.
package main

import (
	"database/sql"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"os"

	"github.com/SIMPLYBOYS/agent-payment-authority/internal/timing"
	_ "modernc.org/sqlite"
)

func main() { os.Exit(run(os.Args[1:], os.Stdout, os.Stderr)) }

type verdict struct {
	timing.Verdict
	Stop *timing.Stop `json:"stop_isolation,omitempty"`
}

func run(args []string, stdout, stderr io.Writer) int {
	fl := flag.NewFlagSet("timing-judge", flag.ContinueOnError)
	fl.SetOutput(stderr)
	db := fl.String("db", "", "the demo database (read only): host_runs")
	fa := fl.String("a", "", "run A's timing record")
	fb := fl.String("b", "", "run B's timing record")
	stop := fl.String("stop", "", "a or b: the run that was stopped (stop isolation case)")
	fake := fl.Bool("fake", false, "judge fake records too (the verdict says so); live records are required otherwise")
	if fl.Parse(args) != nil || fl.NArg() != 0 || *db == "" || *fa == "" || *fb == "" || (*stop != "" && *stop != "a" && *stop != "b") {
		fmt.Fprintln(stderr, "usage: timing-judge --db DEMO_DB --a A.timing.json --b B.timing.json [--stop a|b] [--fake]")
		return 2
	}
	ra, ea := read(*fa)
	rb, eb := read(*fb)
	conn, err := sql.Open("sqlite", "file:"+*db+"?mode=ro")
	if err != nil {
		fmt.Fprintln(stderr, "timing-judge: database:", err)
		return 2
	}
	defer conn.Close()
	rowA, qa := timing.ReadHostRun(conn, ra.RunID)
	rowB, qb := timing.ReadHostRun(conn, rb.RunID)
	v := verdict{Verdict: timing.Judge(ra, rb, rowA, rowB, !*fake)}
	for _, e := range []error{ea, eb, qa, qb} {
		if e != nil {
			v.Conclusion = timing.NotJudged
			v.Problems = append(v.Problems, timing.Problem{Kind: timing.Incomplete, Detail: e.Error()})
		}
	}
	if *stop != "" && v.Conclusion != timing.NotJudged {
		s := timing.StopIsolation(ra, rb, rowB)
		if *stop == "b" {
			s = timing.StopIsolation(rb, ra, rowA)
		}
		v.Stop = &s
	}
	raw, _ := json.MarshalIndent(v, "", "  ")
	raw = append(raw, '\n')
	if n, err := stdout.Write(raw); err != nil || n != len(raw) {
		fmt.Fprintf(stderr, "timing-judge: the report was not written in full (%d of %d bytes); there is no verdict\n", n, len(raw))
		return 3
	}
	switch {
	case v.Conclusion == timing.NotJudged:
		return 2
	case v.Conclusion != timing.Interleaved || (v.Stop != nil && !v.Stop.Held):
		return 1
	}
	return 0
}

// read parses one record; a file that cannot be read or parsed is reported.
func read(path string) (timing.Record, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return timing.Record{}, fmt.Errorf("%s: cannot be read", path)
	}
	r, err := timing.Parse(raw)
	if err != nil {
		return timing.Record{}, fmt.Errorf("%s: not a timing record", path)
	}
	return r, nil
}
