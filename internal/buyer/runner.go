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
	"io"
	"io/fs"
	"os"
	"os/exec"
	"strings"
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
)

// Limits bound one run. Every limit ends the run when crossed.
type Limits struct {
	Line   int           // bytes in one stdout line (one stream-json message)
	Stdout int           // bytes of stdout in total
	Stderr int           // bytes of stderr in total; stderr content is never kept
	Total  time.Duration // wall time of the run
	Grace  time.Duration // SIGTERM to SIGKILL, and the wait for pipes once the CLI is gone
}

// DefaultLimits are the host's limits for a live run.
var DefaultLimits = Limits{Line: 1 << 20, Stdout: 8 << 20, Stderr: 1 << 20, Total: 5 * time.Minute, Grace: 3 * time.Second}

// Spec is one process to run. The host builds every field; nothing comes
// from the model.
type Spec struct {
	Path   string   // executable, absolute
	Args   []string // argv after the program name; no shell is involved
	Dir    string
	Env    []string // the whole environment
	Limits Limits
}

// Report is what the host learns from the CLI's own output. Result and
// Usage are untrusted CLI/model output.
type Report struct {
	Outcome     Outcome
	ExitCode    int // -1: killed, or never started
	Messages    int // stream-json messages read
	Result      string
	NumTurns    int
	Usage       json.RawMessage // nil: unknown
	StderrBytes int
}

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
}

// Run starts the process in its own process group, reads it under the
// limits and returns once the process has exited and both pipes are closed,
// or the pipes were abandoned after Grace. Every stop kills the whole group;
// processes outside the group are never signalled.
func Run(ctx context.Context, s Spec) Report {
	r := Report{ExitCode: -1}
	ctx, cancel := context.WithTimeout(ctx, s.Limits.Total)
	defer cancel()

	outR, outW, err := os.Pipe()
	if err != nil {
		r.Outcome = StartFailed
		return r
	}
	errR, errW, err := os.Pipe()
	if err != nil {
		outR.Close()
		outW.Close()
		r.Outcome = StartFailed
		return r
	}
	cmd := &exec.Cmd{
		Path: s.Path, Args: append([]string{s.Path}, s.Args...), Dir: s.Dir, Env: s.Env,
		Stdout: outW, Stderr: errW, // os.Files: no copying goroutines for Wait to hang on
		SysProcAttr: &syscall.SysProcAttr{Setpgid: true},
	}
	err = cmd.Start()
	outW.Close()
	errW.Close()
	if err != nil {
		outR.Close()
		errR.Close()
		r.Outcome = StartFailed
		if errors.Is(err, fs.ErrNotExist) {
			r.Outcome = NotInstalled
		}
		return r
	}
	pgid := cmd.Process.Pid
	group := func(sig syscall.Signal) { syscall.Kill(-pgid, sig) }

	var st stream
	outc, errc, waitc := make(chan error, 1), make(chan error, 1), make(chan error, 1)
	go func() { outc <- readStream(outR, s.Limits, &st) }()
	go func() {
		n, err := io.Copy(io.Discard, io.LimitReader(errR, int64(s.Limits.Stderr)+1))
		r.StderrBytes = int(n)
		if err == nil && n > int64(s.Limits.Stderr) {
			err = errOverflow
		}
		errc <- err
	}()
	go func() { waitc <- cmd.Wait() }()

	var (
		fault             Outcome
		exitErr           error
		done              = ctx.Done()
		escalate, abandon <-chan time.Time
		stopping          bool
	)
	stop := func(o Outcome) {
		if fault == "" {
			fault = o
		}
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
				stop(Timeout)
			} else {
				stop(Cancelled)
			}
		case err := <-outc:
			outc = nil
			switch {
			case errors.Is(err, os.ErrClosed):
				stop(Orphaned)
			case errors.Is(err, errOverflow), errors.Is(err, toolbridge.ErrTooLong):
				stop(OutputOverflow)
			case err != nil:
				stop(ParseError)
			}
		case err := <-errc:
			errc = nil
			switch {
			case errors.Is(err, os.ErrClosed):
				stop(Orphaned)
			case err != nil:
				stop(OutputOverflow)
			}
		case exitErr = <-waitc:
			waitc, escalate = nil, nil
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
		}
	}
	outR.Close()
	errR.Close()

	r.Messages, r.NumTurns, r.Usage = st.messages, st.numTurns, st.usage
	if st.result != nil {
		r.Result = *st.result
	}
	var ee *exec.ExitError
	if errors.As(exitErr, &ee) {
		r.ExitCode = ee.ExitCode()
	} else if exitErr == nil {
		r.ExitCode = 0
	}
	r.Outcome = fault
	if fault == "" {
		r.Outcome = classify(r.ExitCode, &st)
	}
	return r
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
// line. Anything else ends the run.
func readStream(r io.Reader, lim Limits, st *stream) error {
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
		var m struct {
			Type     string          `json:"type"`
			Subtype  string          `json:"subtype"`
			IsError  bool            `json:"is_error"`
			Result   *string         `json:"result"`
			NumTurns int             `json:"num_turns"`
			Usage    json.RawMessage `json:"usage"`
			Error    string          `json:"error"`
		}
		if json.Unmarshal(line, &m) != nil || m.Type == "" {
			return errParse
		}
		st.messages++
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
