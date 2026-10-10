// Command m1-check runs a round's targeted fake verification with terminal
// progress (make check-m1-e3; docs/m1-evidence.md 16.6):
//
//	m1-check [--list] [--out artifacts/m1-check] [--heartbeat 10s] [--plan FILE]
//
// Before running it prints the base commit, the steps with their commands
// and timeouts, and the evidence directory; --list prints that and runs
// nothing. Each step prints START, then (on stderr) what it can say
// reliably: for `go test -json` steps the test running and the counts so
// far, for the reverse checks the item and its result; a quiet step gets a
// heartbeat (default every 10 s) saying the process is still running and
// how long since its last output. Each ends PASS, FAIL, TIMEOUT,
// INTERRUPTED or ERROR with its real exit code and its log. After a step
// that does not pass, the rest are NOT RUN; nothing is retried. stdout
// carries only the final JSON summary.
//
// The default plan is M1-E3's fix round: fake only. It runs no real CLI,
// make m1-live-cli, version query or login.
package main

import (
	"bufio"
	"context"
	"encoding/json"
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
// (go test -json events), "reverse" (PLAN/ITEM lines of a reverse-check
// script) or "plain".
type Step struct {
	Name    string        `json:"name"`
	Cmd     []string      `json:"cmd"`
	Timeout time.Duration `json:"timeout"`
	Kind    string        `json:"kind"`
}

// e3Plan is the M1-E3 fix round (docs/m1-evidence.md 16.6): fake only.
var e3Plan = []Step{
	{"vet", []string{"go", "vet", "./cmd/m1-live", "./cmd/m1-check", "./internal/progress", "./internal/verification"}, 5 * time.Minute, "plain"},
	{"progress-helper", []string{"go", "test", "-json", "-count=1", "-race", "./internal/progress"}, 5 * time.Minute, "go"},
	{"checker-live", []string{"go", "test", "-json", "-count=1", "-run", "^(TestLive|TestModesAndLayers)", "./internal/verification"}, 10 * time.Minute, "go"},
	{"producer-fake", []string{"go", "test", "-json", "-count=1", "./cmd/m1-live"}, 20 * time.Minute, "go"},
	{"check-runner", []string{"go", "test", "-json", "-count=1", "./cmd/m1-check"}, 10 * time.Minute, "go"},
	{"reverse-r1-r2", []string{"python3", "-u", "scripts/verify/m1-e3-reverse.py", "^(R1|R2) "}, 40 * time.Minute, "reverse"},
}

// Exit codes.
const (
	exitPass        = 0 // every step passed and the evidence was saved
	exitFailed      = 1 // a step failed, timed out or could not run
	exitInterrupted = 2 // interrupted (SIGINT or SIGTERM)
	exitIncomplete  = 3 // usage, or the evidence could not be saved
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
	Detail   string  `json:"detail,omitempty"`
}

// Summary is the stdout JSON.
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
		fmt.Fprintf(stderr, "  %d/%d %-16s timeout %-6s %s\n", i+1, len(plan), s.Name, s.Timeout, strings.Join(s.Cmd, " "))
	}
	fmt.Fprintf(stderr, "m1-check: evidence %s\n", dir)
	if *list {
		return exitPass
	}
	sum := Summary{Base: base, TrackedClean: clean, Evidence: dir, Complete: true, Steps: []Result{}}
	saveErr := func(err error) {
		if err != nil && sum.Complete {
			sum.Complete = false
			fmt.Fprintf(stderr, "m1-check: the evidence could not be saved (%v); the run's evidence is INCOMPLETE\n", err)
		}
	}
	if err := os.MkdirAll(*out, 0o700); err != nil {
		saveErr(err)
	} else if err := os.Mkdir(dir, 0o700); err != nil { // new, never an old one
		saveErr(err)
	}
	// Progress goes to stderr and, as long as it can be saved, to the
	// evidence directory too.
	var prog io.Writer = stderr
	if sum.Complete {
		f, err := os.OpenFile(filepath.Join(dir, "progress.log"), os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
		if err != nil {
			saveErr(err)
		} else {
			defer f.Close()
			prog = io.MultiWriter(stderr, &failSafe{w: f, onErr: saveErr})
		}
		if raw, err := json.MarshalIndent(plan, "", "  "); err == nil {
			saveErr(os.WriteFile(filepath.Join(dir, "plan.json"), append(raw, '\n'), 0o600))
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
		r := runStep(ctx, i, len(plan), s, dir, sum.Complete, *heartbeat, started, say, saveErr)
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
	raw, err := json.MarshalIndent(sum, "", "  ")
	if err == nil && sum.Complete {
		saveErr(os.WriteFile(filepath.Join(dir, "summary.json"), append(raw, '\n'), 0o600))
	}
	if !sum.Complete {
		raw, _ = json.MarshalIndent(sum, "", "  ")
		say("m1-check: %s; the evidence is INCOMPLETE (%s)", verdict, dir)
	} else {
		say("m1-check: %s; evidence %s", verdict, dir)
	}
	stdout.Write(append(raw, '\n'))
	if !sum.Complete && code == exitPass {
		return exitIncomplete
	}
	return code
}

// failSafe reports the first write error and then keeps quiet.
type failSafe struct {
	w     io.Writer
	onErr func(error)
	err   bool
}

func (f *failSafe) Write(p []byte) (int, error) {
	if !f.err {
		if _, err := f.w.Write(p); err != nil {
			f.err = true
			f.onErr(err)
		}
	}
	return len(p), nil
}

var (
	reversePlan = regexp.MustCompile(`^PLAN (\d+)$`)
	reverseItem = regexp.MustCompile(`^ITEM (\d+)/(\d+) (.*)$`)
	reverseRes  = regexp.MustCompile(`^(caught|MISSED|BUILD |ANCHOR) ?(.*)$`)
)

// state is what a step's reader has seen, for the heartbeat.
type state struct {
	mu       sync.Mutex
	last     time.Time // last output
	current  string    // the test or item running
	passed   int
	failed   int
	items    int
	caught   int
	notCaugt int
}

func runStep(ctx context.Context, i, n int, s Step, dir string, saving bool, every time.Duration, all time.Time,
	say func(string, ...any), saveErr func(error)) Result {
	r := Result{Name: s.Name}
	logName := fmt.Sprintf("%02d-%s.log", i+1, s.Name)
	logPath := filepath.Join(dir, logName)
	say("START %d/%d %s at %s", i+1, n, s.Name, time.Now().UTC().Format("15:04:05Z"))
	if saving {
		say("  log: %s (tail -f it from another terminal)", logPath)
	}
	var logW io.Writer = io.Discard
	if saving {
		f, err := os.OpenFile(logPath, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
		if err != nil {
			saveErr(err)
		} else {
			defer f.Close()
			logW = &failSafe{w: f, onErr: saveErr}
			r.Log = logPath
		}
	}
	start := time.Now()
	st := &state{last: start}
	stepCtx, cancel := context.WithTimeout(ctx, s.Timeout)
	defer cancel()
	c := exec.CommandContext(stepCtx, s.Cmd[0], s.Cmd[1:]...)
	var g progress.Group
	g.Prepare(c, 10*time.Second)
	pr, pw := io.Pipe()
	c.Stdout, c.Stderr = pw, pw
	readDone := make(chan struct{})
	go func() {
		defer close(readDone)
		sc := bufio.NewScanner(pr)
		sc.Buffer(make([]byte, 64<<10), 16<<20)
		for sc.Scan() {
			line := sc.Text()
			logW.Write([]byte(line + "\n"))
			st.mu.Lock()
			st.last = time.Now()
			st.mu.Unlock()
			read(s.Kind, line, st, say)
		}
		if err := sc.Err(); err != nil {
			fmt.Fprintf(logW, "m1-check: output not read further: %v\n", err)
		}
		io.Copy(io.Discard, pr) // never block the step on a line too long to scan
	}()
	stopBeat := progress.Heartbeat(&sayWriter{say}, every, func(d time.Duration) string {
		st.mu.Lock()
		defer st.mu.Unlock()
		quiet := time.Since(st.last).Truncate(time.Second)
		what := "no test events yet; the process is running (it may still be compiling)"
		switch {
		case s.Kind == "go" && st.current != "":
			what = fmt.Sprintf("running %s; %d passed, %d failed so far", st.current, st.passed, st.failed)
		case s.Kind == "reverse" && st.current != "":
			what = fmt.Sprintf("item %s; %d caught, %d not caught so far", st.current, st.caught, st.notCaugt)
		case s.Kind == "plain":
			what = "the process is running"
		}
		return fmt.Sprintf("  ... %d/%d %s: %s (step %s, all %s, last output %s ago)", i+1, n, s.Name, what, d, time.Since(all).Truncate(time.Second), quiet)
	})
	err := c.Start()
	if err == nil {
		err = c.Wait()
	}
	g.Done()
	pw.Close()
	<-readDone
	stopBeat()
	r.Seconds = time.Since(start).Seconds()
	if c.ProcessState != nil {
		code := c.ProcessState.ExitCode()
		r.ExitCode = &code
	}
	st.mu.Lock()
	defer st.mu.Unlock()
	switch {
	case g.Stopped() && ctx.Err() != nil:
		r.Result, r.Detail = "INTERRUPTED", "stopped by SIGINT or SIGTERM"
	case g.Stopped():
		r.Result, r.Detail = "TIMEOUT", "stopped after "+s.Timeout.String()
	case c.ProcessState == nil:
		r.Result, r.Detail = "ERROR", fmt.Sprintf("could not start: %v", err)
	case *r.ExitCode != 0:
		r.Result = "FAIL"
	case s.Kind == "reverse" && (st.items == 0 || st.notCaugt > 0 || st.caught != st.items):
		r.Result, r.Detail = "FAIL", fmt.Sprintf("%d of %d items caught by an assertion", st.caught, st.items)
	default:
		r.Result = "PASS"
	}
	if s.Kind == "go" {
		r.Detail = strings.TrimSpace(fmt.Sprintf("%s %d tests passed, %d failed", r.Detail, st.passed, st.failed))
	}
	if s.Kind == "reverse" && r.Detail == "" {
		r.Detail = fmt.Sprintf("%d of %d items caught by an assertion", st.caught, st.items)
	}
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
			say("  %s reverse items", m[1])
		} else if m := reverseRes.FindStringSubmatch(line); m != nil {
			st.items++
			if m[1] == "caught" {
				st.caught++
				say("    CAUGHT %s", strings.TrimSpace(m[2]))
			} else {
				st.notCaugt++
				say("    NOT CAUGHT (%s) %s", strings.TrimSpace(m[1]), strings.TrimSpace(m[2]))
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
