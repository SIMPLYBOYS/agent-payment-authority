// Package buyer runs one Claude Code (or fake CLI) process per task run: a
// fixed executable and argv, a private working directory, an allowlisted
// environment, bounded output and time, and cleanup of the whole process
// group. Tool calls do not pass through here: they reach the host only over
// the toolbridge relay, so nothing the CLI prints is ever taken as a tool
// request.
package buyer

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"os/exec"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/SIMPLYBOYS/agent-payment-authority/internal/toolbridge"
)

// Outcome classifies a finished run. Only OK means the CLI reported a
// complete, successful run; even then the Store, not the CLI, says what was
// requested.
type Outcome string

const (
	OK             Outcome = "ok"
	NotInstalled   Outcome = "not_installed"
	StartFailed    Outcome = "start_failed"
	NotLoggedIn    Outcome = "not_logged_in"
	RateLimited    Outcome = "rate_limited"
	Timeout        Outcome = "timeout"
	Cancelled      Outcome = "cancelled"
	OutputOverflow Outcome = "output_overflow"
	ParseError     Outcome = "parse_error"
	NonzeroExit    Outcome = "nonzero_exit"
	MissingResult  Outcome = "missing_result"
	CLIError       Outcome = "cli_error"
	Orphaned       Outcome = "orphaned_output" // a process outside the group kept a pipe open
	// Run diagnostics of the host's own checks; none is an apa.m1.v2 code.
	InputError       Outcome = "input_error"       // the one stdin write failed (detail input_failed or input_partial)
	ConfigMismatch   Outcome = "config_mismatch"   // the CLI's init (or a hook message) is not what the host configured
	TurnLimit        Outcome = "turn_limit"        // more assistant messages than Limits.Assistant
	StoreUnavailable Outcome = "store_unavailable" // init passed but the host could not prepare the Store or task
)

// Limits bound one run. Every limit ends the run when crossed.
type Limits struct {
	Line   int           // bytes in one stdout line (one stream-json message)
	Stdout int           // bytes of stdout in total
	Stderr int           // bytes of stderr in total; stderr content is never kept
	Total  time.Duration // wall time of the run from the CLI's start, waiting for init included
	Grace  time.Duration // SIGTERM to SIGKILL, and the wait for pipes once the CLI is gone
	// Complete type=assistant messages; the next one stops the run. An
	// output-message bound, not a count of model requests. 0: no bound.
	Assistant int
}

// DefaultLimits are the host's limits for a run. Cleanup after a stop takes
// up to two Grace periods more than Total.
var DefaultLimits = Limits{Line: 1 << 20, Stdout: 8 << 20, Stderr: 1 << 20, Total: 3 * time.Minute, Grace: 3 * time.Second, Assistant: 10}

// Spec is one process to run. The host builds every field; nothing comes
// from the model.
type Spec struct {
	Path   string   // executable, absolute
	Args   []string // argv after the program name; no shell is involved
	Dir    string
	Env    []string // the whole environment
	Limits Limits
	// Watch sees every stream-json message, in order, on the reader before
	// the next line is read. A non-empty outcome stops the run with it and
	// detail, a host-defined code. It may block (the CLI then waits on its
	// pipe); the run does not end before it returns.
	Watch func(typ, subtype string, line []byte) (Outcome, string)
	// Stopping is called once, from the run loop, when the run starts to
	// stop for any reason, the CLI's own exit included.
	Stopping func()
	// HoldStdin gives the process a pipe as stdin and keeps it open until
	// the process is gone (C0 probe). Otherwise stdin is the null device.
	HoldStdin bool
	// Input, with HoldStdin, is written to that pipe once, right after the
	// start, unless the run is already stopping (C0′). Nothing else is ever
	// written. nil: nothing is written.
	Input []byte
}

// Input statuses of a run's stdin (Report.InputStatus).
const (
	InputNone       = "none"        // nothing to write (C0, or no held stdin)
	InputNotStarted = "not_started" // the run was stopping before the write began
	InputComplete   = "complete"    // every byte was written
	InputPartial    = "partial"     // some bytes were written, then the write failed
	InputFailed     = "failed"      // no byte was written (closed pipe, deadline)
)

// stdinWriter writes a run's one input, at most once, and never after the
// run began to stop. The write holds the lock, so stop waits for a write in
// progress; a deadline bounds that wait.
type stdinWriter struct {
	mu      sync.Mutex
	stopped bool
	w       *os.File
	n       int
	status  string
	done    chan struct{}
}

func newStdinWriter(w *os.File) *stdinWriter {
	return &stdinWriter{w: w, status: InputNone, done: make(chan struct{})}
}

func (iw *stdinWriter) stop() {
	iw.mu.Lock()
	iw.stopped = true
	iw.mu.Unlock()
}

// write writes msg once; bound limits how long a blocked write may take.
// beforeWrite runs just before a write takes the lock. Tests replace it to
// order the write against a stop; nothing outside the package can.
var beforeWrite = func(*stdinWriter) {}

func (iw *stdinWriter) write(msg []byte, bound time.Duration) {
	defer close(iw.done)
	beforeWrite(iw)
	iw.mu.Lock()
	defer iw.mu.Unlock()
	if iw.stopped {
		iw.status = InputNotStarted
		return
	}
	iw.w.SetWriteDeadline(time.Now().Add(bound))
	n, err := iw.w.Write(msg)
	iw.n = n
	switch {
	case n == len(msg) && err == nil:
		iw.status = InputComplete
	case n > 0:
		iw.status = InputPartial
	default:
		iw.status = InputFailed
	}
}

// Report is what the host learns from the CLI's own output. Result and
// Usage are untrusted CLI/model output.
type Report struct {
	Outcome     Outcome
	Detail      string // host-defined code for Watch and limit stops
	ExitCode    int    // -1: killed, or never started
	Messages    int    // stream-json messages read
	Result      string
	NumTurns    int
	Usage       json.RawMessage // nil: unknown
	StderrBytes int
	Assistant   int    // complete assistant messages read
	InputBytes  int    // stdin bytes actually written (Spec.Input)
	InputStatus string // Input* constant
}

// stopErr ends a stream read with the outcome to report.
type stopErr struct {
	outcome Outcome
	detail  string
}

func (e *stopErr) Error() string { return string(e.outcome) + ": " + e.detail }

var (
	errOverflow = errors.New("output limit crossed")
	errParse    = errors.New("not a stream-json message")
)

// stream is the parse state of stdout. Only the reader goroutine touches it
// until it reports on its channel.
type stream struct {
	messages, numTurns int
	result             *string
	isError            bool
	subtype            string
	usage              json.RawMessage
	authFailed, rate   bool
	assistant          int
}

// Run starts the CLI and reads its stream-json under the limits (execute).
func Run(ctx context.Context, s Spec) Report {
	var st stream
	x := execute(ctx, s, func(r io.Reader) error { return readStream(r, s, &st) })
	r := Report{Outcome: x.fault, Detail: x.detail, ExitCode: x.exit, StderrBytes: x.stderrBytes, Messages: st.messages,
		NumTurns: st.numTurns, Usage: st.usage, Assistant: st.assistant, InputBytes: x.inputBytes, InputStatus: x.inputStatus}
	if st.result != nil {
		r.Result = *st.result
	}
	if x.fault == "" {
		r.Outcome = classify(x.exit, &st)
	}
	return r
}

// Version runs `path --version` under the same process rules as a run and
// returns its output. Any fault, a nonzero exit or more than Limits.Stdout
// bytes is an error; so is a ctx that ended meanwhile.
func Version(ctx context.Context, path string, env []string, lim Limits) (string, error) {
	var out string
	x := execute(ctx, Spec{Path: path, Args: []string{"--version"}, Dir: os.TempDir(), Env: env, Limits: lim}, func(r io.Reader) error {
		b, err := io.ReadAll(io.LimitReader(r, int64(lim.Stdout)+1))
		if err != nil {
			return err
		}
		if len(b) > lim.Stdout {
			return errOverflow
		}
		out = string(b)
		return nil
	})
	switch {
	case x.fault != "":
		return "", fmt.Errorf("%s --version: %s", path, x.fault)
	case x.exit != 0:
		return "", fmt.Errorf("%s --version: exit %d", path, x.exit)
	case ctx.Err() != nil:
		return "", ctx.Err()
	}
	return out, nil
}

// started is called after each successful process start. Tests replace it
// to count starts; nothing outside the package can.
var started = func() {}

// execute starts the process in its own process group, hands stdout to
// consume, counts stderr, and returns once the process has exited and both
// pipes are closed, or the pipes were abandoned after Grace. Every stop
// kills the whole group; processes outside the group are never signalled.
// fault is "" when nothing stopped the process; exit is -1 when it was
// killed or never started.
type execResult struct {
	fault       Outcome
	detail      string
	exit        int
	stderrBytes int
	inputBytes  int
	inputStatus string
}

func execute(ctx context.Context, s Spec, consume func(io.Reader) error) (x execResult) {
	x.exit, x.inputStatus = -1, InputNone
	ctx, cancel := context.WithTimeout(ctx, s.Limits.Total)
	defer cancel()
	var iw *stdinWriter
	notified := false
	notify := func() {
		if iw != nil {
			iw.stop() // no write begins once the run is stopping
		}
		if !notified && s.Stopping != nil {
			s.Stopping()
		}
		notified = true
	}

	outR, outW, err := os.Pipe()
	if err != nil {
		notify()
		return execResult{fault: StartFailed, exit: -1}
	}
	errR, errW, err := os.Pipe()
	if err != nil {
		outR.Close()
		outW.Close()
		notify()
		return execResult{fault: StartFailed, exit: -1}
	}
	cmd := &exec.Cmd{
		Path: s.Path, Args: append([]string{s.Path}, s.Args...), Dir: s.Dir, Env: s.Env,
		Stdout: outW, Stderr: errW, // os.Files: no copying goroutines for Wait to hang on
		SysProcAttr: &syscall.SysProcAttr{Setpgid: true},
	}
	if s.HoldStdin {
		inR, inW, perr := os.Pipe()
		if perr != nil {
			outR.Close()
			outW.Close()
			errR.Close()
			errW.Close()
			notify()
			return execResult{fault: StartFailed, exit: -1}
		}
		cmd.Stdin = inR
		defer inR.Close() // also closed right after the start: the child holds the only read end
		defer inW.Close() // held: the process sees no EOF while it runs; written only with Spec.Input
		iw = newStdinWriter(inW)
		defer func() { // the writer is done before the result is read
			<-iw.done
			x.inputBytes, x.inputStatus = iw.n, iw.status
		}()
	}
	if ctx.Err() != nil { // cancelled or out of time before the start: start nothing
		outR.Close()
		outW.Close()
		errR.Close()
		errW.Close()
		if iw != nil {
			if s.Input != nil {
				iw.status = InputNotStarted
			}
			close(iw.done) // nothing was written
		}
		notify()
		return execResult{fault: Cancelled, exit: -1}
	}
	err = cmd.Start()
	if err == nil {
		started()
	}
	if iw != nil {
		if r, ok := cmd.Stdin.(*os.File); ok {
			r.Close() // a child that closes its stdin now breaks the pipe instead of filling it
		}
		switch {
		case err == nil && s.Input != nil:
			go iw.write(s.Input, s.Limits.Grace)
		case s.Input != nil: // the start failed
			iw.status = InputNotStarted
			close(iw.done)
		default:
			close(iw.done)
		}
	}
	outW.Close()
	errW.Close()
	if err != nil {
		outR.Close()
		errR.Close()
		notify()
		if errors.Is(err, fs.ErrNotExist) {
			return execResult{fault: NotInstalled, exit: -1}
		}
		return execResult{fault: StartFailed, exit: -1}
	}
	pgid := cmd.Process.Pid
	group := func(sig syscall.Signal) { syscall.Kill(-pgid, sig) }

	outc, errc, waitc := make(chan error, 1), make(chan error, 1), make(chan error, 1)
	go func() { outc <- consume(outR) }()
	go func() {
		n, err := io.Copy(io.Discard, io.LimitReader(errR, int64(s.Limits.Stderr)+1))
		x.stderrBytes = int(n)
		if err == nil && n > int64(s.Limits.Stderr) {
			err = errOverflow
		}
		errc <- err
	}()
	go func() { waitc <- cmd.Wait() }()

	var writeDone <-chan struct{} // the one stdin write has finished
	if iw != nil && s.Input != nil {
		writeDone = iw.done
	}
	var (
		exitErr           error
		done              = ctx.Done()
		escalate, abandon <-chan time.Time
		stopping          bool
	)
	stop := func(o Outcome, detail string) {
		if x.fault == "" {
			x.fault, x.detail = o, detail
		}
		notify()
		if !stopping && waitc != nil {
			stopping = true
			group(syscall.SIGTERM)
			escalate = time.After(s.Limits.Grace)
		}
	}
	for outc != nil || errc != nil || waitc != nil {
		select {
		case <-done:
			done = nil
			if errors.Is(ctx.Err(), context.DeadlineExceeded) {
				stop(Timeout, "")
			} else {
				stop(Cancelled, "")
			}
		case err := <-outc:
			outc = nil
			var se *stopErr
			switch {
			case errors.As(err, &se):
				stop(se.outcome, se.detail)
			case errors.Is(err, os.ErrClosed):
				stop(Orphaned, "")
			case errors.Is(err, errOverflow), errors.Is(err, toolbridge.ErrTooLong):
				stop(OutputOverflow, "")
			case err != nil:
				stop(ParseError, "")
			}
		case err := <-errc:
			errc = nil
			switch {
			case errors.Is(err, os.ErrClosed):
				stop(Orphaned, "")
			case err != nil:
				stop(OutputOverflow, "")
			}
		case exitErr = <-waitc:
			waitc, escalate = nil, nil
			notify() // the CLI is gone: nothing it could still ask for may start
			// The leader is reaped. Whatever is left in its group is ours and
			// can only hold pipes open: kill it, then give the pipes Grace.
			// ponytail: the group ID is the leader's PID, reaped a moment
			// ago; it is not reused while any member lives, and a reuse in
			// this window would need the PID space to wrap.
			group(syscall.SIGKILL)
			abandon = time.After(s.Limits.Grace)
		case <-escalate:
			escalate = nil
			group(syscall.SIGKILL)
		case <-abandon:
			abandon = nil
			outR.Close() // wakes the readers; a process that left the group holds the other end
			errR.Close()
		case <-writeDone:
			writeDone = nil
			// A write that failed is final: stop now, before anything the CLI
			// prints later (an init included) can be taken for success.
			if st := iw.status; st == InputFailed || st == InputPartial {
				stop(InputError, "input_"+st)
			}
		}
	}
	outR.Close()
	errR.Close()

	var ee *exec.ExitError
	if errors.As(exitErr, &ee) {
		x.exit = ee.ExitCode()
	} else if exitErr == nil {
		x.exit = 0
	}
	return x
}

// classify judges a run that ended on its own. Exit 0 alone proves nothing:
// a run needs exactly one result message that reports success.
func classify(exit int, st *stream) Outcome {
	failed := exit != 0 || st.result == nil || st.isError || st.subtype != "success"
	switch {
	case !failed:
		return OK
	case st.authFailed:
		return NotLoggedIn
	case st.rate:
		return RateLimited
	case exit != 0:
		return NonzeroExit
	case st.result == nil:
		return MissingResult
	default:
		return CLIError
	}
}

// readStream reads stream-json: one JSON object with a string "type" per
// line. Anything else ends the run, as do the assistant bound and Watch.
func readStream(r io.Reader, s Spec, st *stream) error {
	lim := s.Limits
	br := bufio.NewReaderSize(r, 64<<10)
	total := 0
	for {
		line, err := toolbridge.ReadLine(br, lim.Line)
		if errors.Is(err, io.EOF) {
			return nil
		}
		if err != nil {
			return err
		}
		if total += len(line) + 1; total > lim.Stdout {
			return errOverflow
		}
		if strings.TrimSpace(string(line)) == "" {
			continue
		}
		// One strict reading serves the classification below, the
		// assistant bound, Watch and the gate alike (objectFields).
		f, err := objectFields(line)
		if errors.Is(err, errAmbiguous) {
			return &stopErr{ParseError, "ambiguous_json"}
		}
		var m struct {
			Type, Subtype, Error string
			IsError              bool
			Result               *string
			NumTurns             int
			Usage                json.RawMessage
		}
		if err == nil {
			err = errors.Join(field(f, "type", &m.Type), field(f, "subtype", &m.Subtype), field(f, "is_error", &m.IsError),
				field(f, "result", &m.Result), field(f, "num_turns", &m.NumTurns), field(f, "usage", &m.Usage), field(f, "error", &m.Error))
		}
		if err != nil || m.Type == "" {
			return errParse
		}
		st.messages++
		if m.Type == "assistant" {
			if st.assistant++; lim.Assistant > 0 && st.assistant > lim.Assistant {
				return &stopErr{TurnLimit, "assistant_messages"}
			}
		}
		if s.Watch != nil {
			if o, d := s.Watch(m.Type, m.Subtype, line); o != "" {
				return &stopErr{o, d}
			}
		}
		switch {
		case m.Type == "system" && m.Subtype == "api_retry":
			st.authFailed = st.authFailed || m.Error == "authentication_failed"
			st.rate = st.rate || m.Error == "rate_limit"
		case m.Type == "result":
			if st.result != nil {
				return errParse // a second result: not one run's output
			}
			text := ""
			if m.Result != nil {
				text = *m.Result
			}
			st.result, st.isError, st.subtype, st.numTurns = &text, m.IsError, m.Subtype, m.NumTurns
			if len(m.Usage) > 0 && string(m.Usage) != "null" {
				st.usage = m.Usage
			}
			if m.IsError || m.Subtype != "success" {
				// ponytail: text patterns for login and rate limits are taken
				// from the docs and local help, not from a failing live run.
				low := strings.ToLower(text)
				st.authFailed = st.authFailed || strings.Contains(low, "not logged in") || strings.Contains(low, "/login") || strings.Contains(low, "invalid api key")
				st.rate = st.rate || strings.Contains(low, "rate limit") || strings.Contains(low, "usage limit")
			}
		}
	}
}
