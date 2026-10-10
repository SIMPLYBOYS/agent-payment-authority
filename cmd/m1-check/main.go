// Command m1-check runs a round's targeted fake verification with terminal
// progress (make check-m1-e3; docs/m1-evidence.md 16.6, 16.7):
//
//	m1-check [--list] [--out artifacts/m1-check] [--heartbeat 10s] [--plan FILE]
//
// Before running it prints the base commit, the steps with their commands
// and timeouts, the reverse items with their own test filters, and the
// evidence directory; --list prints that and runs nothing. Each step
// prints START, then (on stderr) what it can say reliably: for
// `go test -json` steps the test running and the counts so far, for the
// reverse checks the item, its time and its result; a quiet step gets a
// heartbeat (default every 10 s) with the counts, the step and total time,
// how long since its last output and its log. Each ends PASS, FAIL,
// TIMEOUT, INTERRUPTED or ERROR with its real exit code and its log. After
// a step that does not pass, the rest are NOT RUN; nothing is retried.
// stdout carries only the final JSON summary.
//
// Every byte a step prints goes to its log, up to maxLog; progress reads
// lines of up to maxLine and skips longer ones (they stay in the log). A
// log, progress file or summary that cannot be saved whole (a write, a
// short write or a Close that fails), and a summary that cannot be written
// to stdout, make the run incomplete; progress.log is closed before the
// summary and the exit code are formed.
//
// Exit codes, the first that applies: 2 a step was interrupted; 1 a step
// failed, timed out or could not run; 3 the evidence or the summary is
// incomplete (or usage); 0 every step passed and everything was saved and
// written. A save failure never changes a step's own result.
//
// The default plan is the M1-E3 10/11 fix round: fake only. It runs no real
// CLI, make m1-live-cli, version query or login.
package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/SIMPLYBOYS/agent-payment-authority/internal/progress"
)

// Step is one command of a plan. Kind says how its output is read: "go"
// (go test -json events), "reverse" (PLAN/ITEM/RESULT lines of a
// reverse-check script) or "plain". "{dir}" in Cmd is the evidence
// directory.
type Step struct {
	Name    string        `json:"name"`
	Cmd     []string      `json:"cmd"`
	Timeout time.Duration `json:"timeout"`
	Kind    string        `json:"kind"`
}

// e3Plan is the M1-E3 10/11 fix round (docs/m1-evidence.md 16.7): R1 group
// stop and signals, R3 saving and the summary, R4 the classifier, the R2
// contrasts the changed paths need, and the reverse items, each on its own
// tests. Fake only.
var e3Plan = []Step{
	{"vet", []string{"go", "vet", "./cmd/m1-live", "./cmd/m1-check", "./internal/progress", "./internal/verification"}, 5 * time.Minute, "plain"},
	{"r1-group", []string{"go", "test", "-json", "-count=1", "-race", "./internal/progress"}, 5 * time.Minute, "go"},
	{"r3-runner", []string{"go", "test", "-json", "-count=1", "-race", "./cmd/m1-check"}, 10 * time.Minute, "go"},
	{"r4-classifier", []string{"python3", "-I", "scripts/verify/m1_e3_reverse_test.py"}, 2 * time.Minute, "plain"},
	{"r1-r2-producer", []string{"go", "test", "-json", "-count=1", "-race", "-run", "^(TestSignals|TestInterruptInProcess|TestLiveAsk|TestStartFailure|TestFailedRuns)$", "./cmd/m1-live"}, 15 * time.Minute, "go"},
	{"r2-checker", []string{"go", "test", "-json", "-count=1", "-race", "-run", "^(TestLiveGolden|TestLiveRecordsThatContradict)$", "./internal/verification"}, 10 * time.Minute, "go"},
	{"reverse", []string{"python3", "-u", "scripts/verify/m1-e3-reverse.py", "--out", "{dir}/reverse", "^(R1|R2|R3|R4) "}, 30 * time.Minute, "reverse"},
}

// Exit codes.
const (
	exitPass        = 0 // every step passed; evidence and summary complete
	exitFailed      = 1 // a step failed, timed out or could not run
	exitInterrupted = 2 // interrupted (SIGINT or SIGTERM)
	exitIncomplete  = 3 // usage, or the evidence or the summary incomplete
)

// Limits on what a step's output may take.
var (
	maxLog  int64 = 256 << 20 // bytes of a step's log; beyond it the log is incomplete
	maxLine       = 1 << 20   // bytes of a line progress reads; longer ones are only in the log
	// openLog creates a new evidence file (a test replaces it to fail).
	openLog = func(path string) (io.WriteCloser, error) {
		return os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
	}
)

func main() {
	ctx, cancel := context.WithCancel(context.Background())
	sigs := make(chan os.Signal, 4)
	signal.Notify(sigs, os.Interrupt, syscall.SIGTERM)
	go func() {
		for n := 0; ; n++ {
			s := <-sigs
			if n == 0 {
				fmt.Fprintf(os.Stderr, "m1-check: %v: stopping the current step; the rest will not run\n", s)
				cancel()
			} else {
				fmt.Fprintf(os.Stderr, "m1-check: %v again: already stopping\n", s)
			}
		}
	}()
	os.Exit(run(ctx, os.Args[1:], os.Stdout, os.Stderr))
}

// Result is one step's outcome.
type Result struct {
	Name     string  `json:"name"`
	Result   string  `json:"result"` // PASS FAIL TIMEOUT INTERRUPTED ERROR NOT_RUN
	ExitCode *int    `json:"exit_code"`
	Seconds  float64 `json:"seconds"`
	Log      string  `json:"log,omitempty"`
	LogSaved bool    `json:"log_saved"` // the whole output is in Log
	Detail   string  `json:"detail,omitempty"`
}

// Summary is the stdout JSON (and summary.json).
type Summary struct {
	Base         string   `json:"base"`
	TrackedClean bool     `json:"tracked_clean"`
	Evidence     string   `json:"evidence"`
	Complete     bool     `json:"evidence_complete"`
	Steps        []Result `json:"steps"`
	Seconds      float64  `json:"seconds"`
	Verdict      string   `json:"verdict"`
}

func run(ctx context.Context, args []string, stdout, stderr io.Writer) int {
	fl := flag.NewFlagSet("m1-check", flag.ContinueOnError)
	fl.SetOutput(stderr)
	list := fl.Bool("list", false, "print the plan and the evidence directory; run nothing")
	out := fl.String("out", "artifacts/m1-check", "where the new evidence directory is made")
	heartbeat := fl.Duration("heartbeat", 10*time.Second, "how often a quiet step reports that it is still running")
	planFile := fl.String("plan", "", "a JSON plan instead of the M1-E3 round's (steps: name, cmd, timeout in ns, kind)")
	if fl.Parse(args) != nil || fl.NArg() != 0 {
		return exitIncomplete
	}
	plan := e3Plan
	if *planFile != "" {
		raw, err := os.ReadFile(*planFile)
		if err == nil {
			err = json.Unmarshal(raw, &plan)
		}
		if err != nil || len(plan) == 0 {
			fmt.Fprintf(stderr, "m1-check: plan %s: %v\n", *planFile, err)
			return exitIncomplete
		}
	}
	base, clean := baseCommit()
	dir := filepath.Join(*out, "m1-check-"+time.Now().UTC().Format("20060102T150405Z"))
	fmt.Fprintf(stderr, "m1-check: base %s (tracked tree clean: %v)\n", base, clean)
	fmt.Fprintf(stderr, "m1-check: %d steps (steps, not test cases):\n", len(plan))
	for i, s := range plan {
		fmt.Fprintf(stderr, "  %d/%d %-16s timeout %-6s %s\n", i+1, len(plan), s.Name, s.Timeout, strings.Join(expand(s.Cmd, dir), " "))
		if s.Kind == "reverse" && *list {
			// The items and their own test filters (the script's --list
			// reads its table only).
			c := exec.CommandContext(ctx, s.Cmd[0], append(expand(s.Cmd[1:], dir), "--list")...)
			c.Stdout, c.Stderr = stderr, stderr
			if err := c.Run(); err != nil {
				fmt.Fprintf(stderr, "m1-check: listing %s: %v\n", s.Name, err)
				return exitIncomplete
			}
		}
	}
	fmt.Fprintf(stderr, "m1-check: evidence %s\n", dir)
	if *list {
		return exitPass
	}
	sum := Summary{Base: base, TrackedClean: clean, Evidence: dir, Complete: true, Steps: []Result{}}
	saveErr := func(what string, err error) {
		if err != nil && sum.Complete {
			sum.Complete = false
			fmt.Fprintf(stderr, "m1-check: %s could not be saved whole (%v); the evidence is INCOMPLETE\n", what, err)
		}
	}
	if err := os.MkdirAll(*out, 0o700); err != nil {
		saveErr("the evidence directory", err)
	} else if err := os.Mkdir(dir, 0o700); err != nil { // new, never an old one
		saveErr("the evidence directory", err)
	}
	saving := sum.Complete
	// Progress goes to stderr and, while it can be saved, to the evidence
	// directory too.
	var prog io.Writer = stderr
	var progFile io.WriteCloser // closed before the summary is formed
	if saving {
		if f, err := openLog(filepath.Join(dir, "progress.log")); err != nil {
			saveErr("progress.log", err)
		} else {
			progFile = f
			prog = io.MultiWriter(stderr, &failSafe{w: f, onErr: func(err error) { saveErr("progress.log", err) }})
		}
		if raw, err := json.MarshalIndent(plan, "", "  "); err == nil {
			saveErr("plan.json", writeNew(filepath.Join(dir, "plan.json"), append(raw, '\n')))
		}
	}
	var mu sync.Mutex // progress lines from the step reader and the heartbeat
	say := func(format string, a ...any) {
		mu.Lock()
		defer mu.Unlock()
		fmt.Fprintf(prog, format+"\n", a...)
	}
	started := time.Now()
	stopped := false
	for i, s := range plan {
		if stopped {
			sum.Steps = append(sum.Steps, Result{Name: s.Name, Result: "NOT_RUN"})
			continue
		}
		r := runStep(ctx, i, len(plan), s, dir, saving, *heartbeat, started, say, saveErr)
		sum.Steps = append(sum.Steps, r)
		if r.Result != "PASS" {
			stopped = true
		}
	}
	sum.Seconds = time.Since(started).Seconds()
	code, verdict := exitPass, "PASS"
	for _, r := range sum.Steps {
		switch r.Result {
		case "INTERRUPTED":
			code, verdict = exitInterrupted, "INTERRUPTED"
		case "PASS", "NOT_RUN":
		default:
			if code == exitPass {
				code, verdict = exitFailed, "FAIL"
			}
		}
	}
	sum.Verdict = verdict
	say("m1-check: summary (%s total):", time.Duration(sum.Seconds*float64(time.Second)).Truncate(time.Second))
	for i, r := range sum.Steps {
		ec := "-"
		if r.ExitCode != nil {
			ec = fmt.Sprint(*r.ExitCode)
		}
		say("  %d/%d %-16s %-11s exit %-3s %6.1fs %s", i+1, len(sum.Steps), r.Name, r.Result, ec, r.Seconds, r.Log)
	}
	// progress.log ends here, before summary.json, the stdout JSON and the
	// exit code are formed, so its last writes and its Close count in them;
	// what follows goes to stderr only.
	if progFile != nil {
		mu.Lock()
		saveErr("progress.log", progFile.Close())
		prog = stderr
		mu.Unlock()
	}
	if saving {
		raw, _ := json.MarshalIndent(sum, "", "  ")
		saveErr("summary.json", writeNew(filepath.Join(dir, "summary.json"), append(raw, '\n')))
	}
	raw, _ := json.MarshalIndent(sum, "", "  ") // evidence_complete as it ended
	raw = append(raw, '\n')
	saved := "complete"
	if !sum.Complete {
		saved = "INCOMPLETE"
	}
	say("m1-check: %s; evidence %s (%s)", verdict, dir, saved)
	if n, err := stdout.Write(raw); err != nil || n != len(raw) {
		// A fixed message: the writer's error is not repeated.
		say("m1-check: the JSON summary was NOT delivered on stdout (the write failed or was short); the evidence files are %s in %s", saved, dir)
		sum.Complete = false
	}
	if code == exitPass && !sum.Complete {
		return exitIncomplete
	}
	return code
}

// expand puts the evidence directory in for "{dir}".
func expand(args []string, dir string) []string {
	out := make([]string, len(args))
	for i, a := range args {
		out[i] = strings.ReplaceAll(a, "{dir}", dir)
	}
	return out
}

// writeNew writes a new evidence file whole, or reports why not.
func writeNew(path string, data []byte) error {
	f, err := openLog(path)
	if err != nil {
		return err
	}
	if n, err := f.Write(data); err != nil || n != len(data) {
		f.Close()
		return firstErr(err, io.ErrShortWrite)
	}
	return f.Close()
}

func firstErr(err, or error) error {
	if err != nil {
		return err
	}
	return or
}

// failSafe reports the first write error (or short write) and then drops
// what follows; it never fails its caller.
type failSafe struct {
	w     io.Writer
	onErr func(error)
	err   bool
}

func (f *failSafe) Write(p []byte) (int, error) {
	if !f.err {
		if n, err := f.w.Write(p); err != nil || n != len(p) {
			f.err = true
			f.onErr(firstErr(err, io.ErrShortWrite))
		}
	}
	return len(p), nil
}

var (
	reversePlan = regexp.MustCompile(`^PLAN (\d+)$`)
	reverseItem = regexp.MustCompile(`^ITEM (\d+)/(\d+) (.*)$`)
	reverseRes  = regexp.MustCompile(`^RESULT (caught|not-caught) \| ([0-9.]+)s \| (.*?) \| (.*)$`)
)

// state is what a step's reader has seen, for the heartbeat.
type state struct {
	mu        sync.Mutex
	last      time.Time // last output
	current   string    // the test or item running
	passed    int
	failed    int
	items     int // reverse items planned
	caught    int
	notCaught int
}

// stepOut takes a step's output (stdout and stderr, one writer): every byte
// to the log while it is under maxLog and writes succeed (after that the
// log is incomplete and the rest is drained unsaved), and whole lines of up
// to maxLine to the progress reader. It never fails the step's pipe, so the
// step is never blocked on it.
type stepOut struct {
	log     io.Writer // nil: not saved
	logErr  error     // why the log is incomplete
	written int64
	line    []byte
	skip    bool // in a line longer than maxLine
	onByte  func()
	onLine  func(string)
}

var errLogLimit = errors.New("the log reached its size limit")

func (o *stepOut) Write(p []byte) (int, error) {
	n := len(p)
	o.onByte()
	if o.log != nil && o.logErr == nil {
		q := p
		if room := maxLog - o.written; int64(len(q)) > room {
			q, o.logErr = q[:room], errLogLimit
		}
		w, err := o.log.Write(q)
		o.written += int64(w)
		if err != nil || w != len(q) {
			o.logErr = firstErr(err, io.ErrShortWrite)
		}
	}
	for {
		i := bytes.IndexByte(p, '\n')
		chunk := p
		if i >= 0 {
			chunk = p[:i]
		}
		if !o.skip {
			if len(o.line)+len(chunk) > maxLine {
				o.skip, o.line = true, o.line[:0]
			} else {
				o.line = append(o.line, chunk...)
			}
		}
		if i < 0 {
			return n, nil
		}
		if !o.skip {
			o.onLine(string(o.line))
		}
		o.line, o.skip = o.line[:0], false
		p = p[i+1:]
	}
}

// flush hands on a last line without a newline.
func (o *stepOut) flush() {
	if len(o.line) > 0 && !o.skip {
		o.onLine(string(o.line))
	}
}

func runStep(ctx context.Context, i, n int, s Step, dir string, saving bool, every time.Duration, all time.Time,
	say func(string, ...any), saveErr func(string, error)) Result {
	r := Result{Name: s.Name}
	logName := fmt.Sprintf("%02d-%s.log", i+1, s.Name)
	logPath := filepath.Join(dir, logName)
	say("START %d/%d %s at %s", i+1, n, s.Name, time.Now().UTC().Format("15:04:05Z"))
	st := &state{last: time.Now()}
	o := &stepOut{
		onByte: func() { st.mu.Lock(); st.last = time.Now(); st.mu.Unlock() },
		onLine: func(line string) { read(s.Kind, line, st, say) },
	}
	var logFile io.WriteCloser
	if saving {
		f, err := openLog(logPath)
		if err != nil {
			saveErr(logName, err)
		} else {
			logFile, o.log, r.Log = f, f, logPath
			say("  log: %s (tail -f it from another terminal)", logPath)
		}
	}
	start := time.Now()
	stepCtx, cancel := context.WithTimeout(ctx, s.Timeout)
	defer cancel()
	cmd := expand(s.Cmd, dir)
	c := exec.CommandContext(stepCtx, cmd[0], cmd[1:]...)
	var g progress.Group
	g.Prepare(c, 10*time.Second)
	c.Stdout, c.Stderr = o, o // one writer: exec calls it from one goroutine at a time
	stopBeat := progress.Heartbeat(&sayWriter{say}, every, func(d time.Duration) string {
		st.mu.Lock()
		defer st.mu.Unlock()
		what := "no test events yet; the process is running"
		switch {
		case s.Kind == "go" && st.current != "":
			what = fmt.Sprintf("running %s; %d passed, %d failed so far", st.current, st.passed, st.failed)
		case s.Kind == "reverse" && st.current != "":
			what = fmt.Sprintf("item %s; %d of %d done (%d caught, %d not caught)", st.current, st.caught+st.notCaught, st.items, st.caught, st.notCaught)
		case s.Kind == "plain":
			what = "the process is running; it reports no events"
		}
		return fmt.Sprintf("  ... %d/%d %s: %s (step %s, all %s, last output %s ago, log %s)", i+1, n, s.Name, what, d,
			time.Since(all).Truncate(time.Second), time.Since(st.last).Truncate(time.Second), logName)
	})
	err := c.Start()
	if err == nil {
		err = c.Wait()
	}
	groupErr := g.Done()
	stopBeat()
	o.flush()
	if logFile != nil {
		if cerr := logFile.Close(); o.logErr == nil {
			o.logErr = cerr
		}
	}
	r.LogSaved = logFile != nil && o.logErr == nil
	if logFile != nil && o.logErr != nil {
		saveErr(logName, o.logErr)
	}
	r.Seconds = time.Since(start).Seconds()
	if c.ProcessState != nil {
		code := c.ProcessState.ExitCode()
		r.ExitCode = &code
	}
	st.mu.Lock()
	defer st.mu.Unlock()
	var exitErr *exec.ExitError
	switch {
	case g.Stopped() && ctx.Err() != nil:
		r.Result, r.Detail = "INTERRUPTED", "stopped by SIGINT or SIGTERM"
	case g.Stopped():
		r.Result, r.Detail = "TIMEOUT", "stopped after "+s.Timeout.String()
	case c.ProcessState == nil:
		r.Result, r.Detail = "ERROR", fmt.Sprintf("could not start: %v", err)
	case *r.ExitCode != 0:
		r.Result = "FAIL"
	case err != nil && !errors.As(err, &exitErr):
		// exit 0, but its output was not read to the end (a pipe error, or
		// pipes held open past WaitDelay): nothing says it passed.
		r.Result, r.Detail = "ERROR", fmt.Sprintf("exit 0 but the output was not read to the end: %v", err)
	case s.Kind == "reverse" && (st.items == 0 || st.caught != st.items):
		r.Result = "FAIL"
	default:
		r.Result = "PASS"
	}
	var notes []string
	if r.Detail != "" {
		notes = append(notes, r.Detail)
	}
	switch s.Kind {
	case "go":
		notes = append(notes, fmt.Sprintf("%d tests passed, %d failed", st.passed, st.failed))
	case "reverse":
		notes = append(notes, fmt.Sprintf("%d of %d items caught by the expected assertion, %d not", st.caught, st.items, st.notCaught))
	}
	if groupErr != nil {
		notes = append(notes, groupErr.Error())
	}
	if logFile != nil && o.logErr != nil {
		notes = append(notes, "log incomplete: "+o.logErr.Error())
	}
	r.Detail = strings.Join(notes, "; ")
	ec := "-"
	if r.ExitCode != nil {
		ec = fmt.Sprint(*r.ExitCode)
	}
	detail := ""
	if r.Detail != "" {
		detail = " (" + r.Detail + ")"
	}
	say("%s %d/%d %s: exit %s, %s%s %s", r.Result, i+1, n, s.Name, ec, time.Duration(r.Seconds*float64(time.Second)).Truncate(100*time.Millisecond), detail, r.Log)
	return r
}

// read takes what a step's output reliably says: go test -json events
// (top-level tests; failures of any test), or the reverse script's lines.
// The output itself is only in the log.
func read(kind, line string, st *state, say func(string, ...any)) {
	switch kind {
	case "go":
		var e struct{ Action, Test, Package string }
		if json.Unmarshal([]byte(line), &e) != nil || e.Test == "" {
			return
		}
		top := !strings.Contains(e.Test, "/")
		st.mu.Lock()
		defer st.mu.Unlock()
		switch e.Action {
		case "run":
			if top {
				st.current = e.Test
				say("  run  %s", e.Test)
			}
		case "pass":
			if top {
				st.passed++
			}
		case "fail":
			st.failed++
			say("  FAIL %s", e.Test)
		}
	case "reverse":
		st.mu.Lock()
		defer st.mu.Unlock()
		if m := reverseItem.FindStringSubmatch(line); m != nil {
			st.current = m[1] + "/" + m[2] + " " + m[3]
			say("  item %s/%s %s", m[1], m[2], m[3])
		} else if m := reversePlan.FindStringSubmatch(line); m != nil {
			fmt.Sscan(m[1], &st.items)
			say("  %s reverse items", m[1])
		} else if m := reverseRes.FindStringSubmatch(line); m != nil {
			if m[1] == "caught" {
				st.caught++
				say("    CAUGHT %s (%ss): %s", m[3], m[2], m[4])
			} else {
				st.notCaught++
				say("    NOT CAUGHT %s (%ss): %s", m[3], m[2], m[4])
			}
		}
	}
}

// sayWriter lets the heartbeat write through say (one lock for all lines).
type sayWriter struct{ say func(string, ...any) }

func (w *sayWriter) Write(p []byte) (int, error) {
	w.say("%s", strings.TrimRight(string(p), "\n"))
	return len(p), nil
}

func baseCommit() (string, bool) {
	head, err := exec.Command("git", "rev-parse", "HEAD").Output()
	if err != nil {
		return "unknown", false
	}
	st, err := exec.Command("git", "status", "--porcelain", "--untracked-files=no").Output()
	return strings.TrimSpace(string(head)), err == nil && len(strings.TrimSpace(string(st))) == 0
}
