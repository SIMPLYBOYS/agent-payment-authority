package buyer

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/SIMPLYBOYS/agent-payment-authority/internal/buyer/fakecli"
)

// C0′ tests: the one fixed message, written once, with the fake only.

// msgCount is the fake's account of complete stdin lines.
func (p *probeEnv) msgCount() string {
	p.t.Helper()
	raw, err := os.ReadFile(p.msgs)
	if err != nil {
		p.t.Fatalf("the fake never started its line counter: %v", err)
	}
	return string(raw)
}

func oneMessageReceived() string {
	sum := sha256.Sum256([]byte(OneMessageText))
	return "lines=1 user_messages=1 content_sha256=" + hex.EncodeToString(sum[:])
}

func newProbe1(t *testing.T, scen string, total time.Duration) *probeEnv {
	p := newProbe(t, scen, total)
	p.spec.SendOneMessage = true
	return p
}

// The message is one stream-json line of the SDKUserMessage shape.
func TestOneMessageLine(t *testing.T) {
	var m map[string]json.RawMessage
	if json.Unmarshal(oneMessage, &m) != nil || len(m) != 3 || string(m["type"]) != `"user"` || string(m["parent_tool_use_id"]) != "null" ||
		string(m["message"]) != `{"role":"user","content":"Reply only OK."}` || oneMessage[len(oneMessage)-1] != '\n' ||
		strings.Count(string(oneMessage), "\n") != 1 {
		t.Fatalf("message %q", oneMessage)
	}
}

// The writer writes once, never after a stop, and records what happened.
func TestStdinWriter(t *testing.T) {
	r, w, _ := os.Pipe()
	iw := newStdinWriter(w)
	iw.stop()
	iw.write([]byte("x\n"), time.Second)
	<-iw.done
	w.Close()
	got, _ := io.ReadAll(r)
	if iw.status != InputNotStarted || iw.n != 0 || len(got) != 0 {
		t.Errorf("after stop: %s %d %q", iw.status, iw.n, got)
	}
	r, w, _ = os.Pipe()
	iw = newStdinWriter(w)
	iw.write([]byte("x\n"), time.Second)
	w.Close()
	if got, _ = io.ReadAll(r); iw.status != InputComplete || iw.n != 2 || string(got) != "x\n" {
		t.Errorf("normal: %s %d %q", iw.status, iw.n, got)
	}
	r, w, _ = os.Pipe()
	r.Close() // nobody reads: a broken pipe
	iw = newStdinWriter(w)
	iw.write([]byte("x\n"), time.Second)
	w.Close()
	if iw.status != InputFailed || iw.n != 0 {
		t.Errorf("broken pipe: %s %d", iw.status, iw.n)
	}
}

// C0′ main case: the fake waits for one complete message, then inits. The
// host wrote it whole, once; the fake got exactly that line.
func TestProbeOneMessage(t *testing.T) {
	p := newProbe1(t, "probe1-wait-message", 10*time.Second)
	res, took := p.run(context.Background())
	if res.Status != ProbeObserved || res.Mode != ModeC0Prime || res.InputStatus != InputComplete || took > 5*time.Second {
		t.Fatalf("%+v after %v", res, took)
	}
	if res.StdinWritten != len(oneMessage) || res.InputSize != len(oneMessage) || p.stdinBytes() != fmt.Sprint(len(oneMessage)) {
		t.Errorf("bytes: host %d of %d, fake %s", res.StdinWritten, res.InputSize, p.stdinBytes())
	}
	if p.msgCount() != oneMessageReceived() {
		t.Errorf("fake received %s", p.msgCount())
	}
	if res.CallsExecuted != 0 || res.CallsRefused < 1 {
		t.Errorf("business calls: executed %d, refused %d", res.CallsExecuted, res.CallsRefused)
	}
	out, _ := json.Marshal(res)
	trace, _ := os.ReadFile(res.Trace)
	if strings.Contains(string(out)+string(trace), "Reply only") {
		t.Errorf("message text kept: %s", out)
	}
}

// The fake reads the message and never inits: a bounded wait, nothing more
// written, and not reported as C0's no_init_before_input.
func TestProbeOneMessageNoInit(t *testing.T) {
	p := newProbe1(t, "probe1-message-no-init", 1500*time.Millisecond)
	res, took := p.run(context.Background())
	if res.Status != ProbeNoInitAfterInput || res.Code != "no_init_after_message" || res.InputStatus != InputComplete || took > 6*time.Second {
		t.Fatalf("%+v after %v", res, took)
	}
	if p.msgCount() != oneMessageReceived() || p.stdinBytes() != fmt.Sprint(len(oneMessage)) {
		t.Errorf("fake: %s, %s bytes", p.msgCount(), p.stdinBytes())
	}
}

// checkInput holds for any C0′ run: what the host reports matches its
// status, the fake never got more than one line or more bytes than were
// written, no tool ran, and the run ended in bounds.
func checkInput(t *testing.T, p *probeEnv, res ProbeResult, took time.Duration) {
	t.Helper()
	if took > 8*time.Second {
		t.Fatalf("took %v", took)
	}
	switch res.InputStatus {
	case InputComplete:
		if res.StdinWritten != len(oneMessage) {
			t.Errorf("complete but %d bytes", res.StdinWritten)
		}
	case InputNotStarted, InputFailed:
		if res.StdinWritten != 0 {
			t.Errorf("%s but %d bytes", res.InputStatus, res.StdinWritten)
		}
	case InputPartial:
		if res.StdinWritten <= 0 || res.StdinWritten >= len(oneMessage) {
			t.Errorf("partial with %d bytes", res.StdinWritten)
		}
	default:
		t.Errorf("input status %q", res.InputStatus)
	}
	var lines, users, got int // a missing count file: the fake received nothing
	if raw, err := os.ReadFile(p.msgs); err == nil {
		fmt.Sscanf(string(raw), "lines=%d user_messages=%d", &lines, &users)
	}
	if raw, err := os.ReadFile(p.stdin); err == nil {
		fmt.Sscan(string(raw), &got)
	}
	if lines > 1 || users > 1 || got > res.StdinWritten {
		t.Errorf("fake got %d lines, %d messages, %d bytes; host wrote %d", lines, users, got, res.StdinWritten)
	}
	if res.CallsExecuted != 0 || res.Mode != ModeC0Prime {
		t.Errorf("%+v", res)
	}
}

// Init before the message, a stop racing the write, a CLI that does not
// read or closes its stdin, and an early exit: whatever was written is
// recorded as it happened, never more than once.
func TestProbeOneMessageInterleaving(t *testing.T) {
	t.Run("init first", func(t *testing.T) {
		p := newProbe1(t, "probe-init-first", 10*time.Second)
		res, took := p.run(context.Background())
		if res.Status != ProbeObserved {
			t.Fatalf("%+v", res)
		}
		checkInput(t, p, res, took)
	})
	for _, d := range []time.Duration{0, 2 * time.Millisecond, 10 * time.Millisecond, 40 * time.Millisecond} {
		t.Run(fmt.Sprintf("cancel after %v", d), func(t *testing.T) {
			sc := manual(fakecli.Step{Do: "wait_message"}, fakecli.Step{Do: "sleep", MS: 500}, fakecli.Step{Do: "init"}, fakecli.Step{Do: "hang"})
			p := newProbe1(t, genScenario(t, sc), 10*time.Second)
			ctx, cancel := context.WithCancel(context.Background())
			time.AfterFunc(d, cancel)
			start := time.Now()
			res, err := Probe(ctx, p.spec)
			took := time.Since(start)
			if err != nil {
				// The cancel won the race to Probe's first check: nothing
				// may have started, and nothing was written.
				if _, serr := os.Stat(p.stdin); !strings.Contains(err.Error(), "cancelled before the probe started") || serr == nil || res.StdinWritten != 0 {
					t.Fatalf("%v; fake started: %v; %+v", err, serr == nil, res)
				}
				return
			}
			if res.Status != ProbeFailed || res.Code != "cancelled" {
				t.Fatalf("%+v", res)
			}
			checkInput(t, p, res, took)
		})
	}
	t.Run("does not read stdin", func(t *testing.T) {
		sc := manual(fakecli.Step{Do: "hang"})
		sc.NoStdinRead = true
		p := newProbe1(t, genScenario(t, sc), 1500*time.Millisecond)
		res, took := p.run(context.Background())
		if res.Status != ProbeNoInitAfterInput || p.stdinBytes() != "0" { // the line sits in the pipe buffer
			t.Fatalf("%+v, fake %s bytes", res, p.stdinBytes())
		}
		checkInput(t, p, res, took)
	})
	t.Run("closes stdin", func(t *testing.T) {
		sc := manual(fakecli.Step{Do: "close_stdin"}, fakecli.Step{Do: "hang"})
		sc.NoStdinRead = true
		p := newProbe1(t, genScenario(t, sc), 1500*time.Millisecond)
		res, took := p.run(context.Background())
		if res.Status == ProbeObserved {
			t.Fatalf("%+v", res)
		}
		checkInput(t, p, res, took)
	})
	t.Run("exits early", func(t *testing.T) {
		sc := manual(fakecli.Step{Do: "exit"})
		sc.NoStdinRead = true
		p := newProbe1(t, genScenario(t, sc), 5*time.Second)
		res, took := p.run(context.Background())
		if res.Status != ProbeFailed {
			t.Fatalf("%+v", res)
		}
		checkInput(t, p, res, took)
	})
}

// After the message, every existing stop still applies at once: api_retry
// (not overwritten by a later init), a model reply, a hook, ambiguity, an
// old version. Marks, the capability and the message text never reach the
// result or the trace.
func TestProbeOneMessageStops(t *testing.T) {
	const mark = "MARKC0P9a3e"
	wait := fakecli.Step{Do: "wait_message"}
	for name, c := range map[string]struct {
		sc   fakecli.Scenario
		code string
	}{
		"auth retry, then init": {manual(wait, fakecli.Step{Do: "retry", Error: "authentication_failed"}, fakecli.Step{Do: "init"}, fakecli.Step{Do: "hang"}), "api_retry_auth"},
		"auth retry, no init":   {manual(wait, fakecli.Step{Do: "retry", Error: "authentication_failed"}, fakecli.Step{Do: "hang"}), "api_retry_auth"},
		"rate retry, then init": {manual(wait, fakecli.Step{Do: "retry", Error: "rate_limit"}, fakecli.Step{Do: "init"}, fakecli.Step{Do: "hang"}), "api_retry_rate_limit"},
		"rate retry, no init":   {manual(wait, fakecli.Step{Do: "retry", Error: "rate_limit"}, fakecli.Step{Do: "hang"}), "api_retry_rate_limit"},
		"unknown retry text":    {manual(wait, fakecli.Step{Do: "retry", Error: mark}, fakecli.Step{Do: "init"}, fakecli.Step{Do: "hang"}), "api_retry"},
		"model replies":         {manual(wait, fakecli.Step{Do: "text", Text: "OK " + mark}, fakecli.Step{Do: "init"}, fakecli.Step{Do: "hang"}), "unexpected_message"},
		"result before init":    {manual(wait, fakecli.Step{Do: "result", Text: "OK " + mark}, fakecli.Step{Do: "hang"}), "unexpected_message"},
		"hook":                  {manual(wait, fakecli.Step{Do: "raw", Text: `{"type":"system","subtype":"hook_started","hook_event":"x","hook_name":"h"}` + "\n"}, fakecli.Step{Do: "hang"}), "hook_message"},
		"ambiguous init":        {manual(wait, fakecli.Step{Do: "raw", Text: `{"type":"system","subtype":"init","model":"sonnet","Model":"x"}` + "\n"}, fakecli.Step{Do: "hang"}), "ambiguous_json"},
		"old init version":      {manual(wait, fakecli.Step{Do: "init", Init: map[string]json.RawMessage{"claude_code_version": json.RawMessage(`"2.1.287"`)}}, fakecli.Step{Do: "hang"}), "init_cli_version"},
		"marks in the init": {manual(fakecli.Step{Do: "savecap"}, wait, fakecli.Step{Do: "init", Init: map[string]json.RawMessage{
			"model": json.RawMessage(`"$CAPABILITY"`), "apiKeySource": json.RawMessage(`"` + mark + `"`), "cwd": json.RawMessage(`"$PROMPT"`)}}, fakecli.Step{Do: "hang"}), "init_observed"},
	} {
		t.Run(name, func(t *testing.T) {
			p := newProbe1(t, genScenario(t, c.sc), 10*time.Second)
			res, took := p.run(context.Background())
			if res.Code != c.code || took > 3*time.Second || res.Mode != ModeC0Prime || res.CallsExecuted != 0 {
				t.Fatalf("%s %s after %v (%+v)", res.Status, res.Code, took, res)
			}
			if p.msgCount() != oneMessageReceived() {
				t.Errorf("fake received %s", p.msgCount())
			}
			out, _ := json.Marshal(res)
			trace, _ := os.ReadFile(res.Trace)
			all := string(out) + string(trace)
			capability, _ := os.ReadFile(p.cap)
			if strings.Contains(all, mark) || strings.Contains(all, "Reply only") || (len(capability) == 64 && strings.Contains(all, string(capability))) {
				t.Fatalf("leak: %s", all)
			}
		})
	}
}

// A write that fails is final: the probe stops at once, before the wait
// limit, and an init the CLI prints afterwards does not make it a success.
// The started hook is only a barrier: the CLI has closed its stdin before
// the write begins, so the write meets a broken pipe every time.
func TestProbeOneMessageWriteFailure(t *testing.T) {
	for _, laterInit := range []bool{false, true} {
		t.Run(fmt.Sprintf("later init %v", laterInit), func(t *testing.T) {
			ready := filepath.Join(t.TempDir(), "stdin-closed")
			body := "exec 0<&-\n: > " + ready + "\n"
			if laterInit { // ignores SIGTERM, so the init is printed, and read, after the stop
				body = "trap '' TERM\n" + body + "sleep 0.3\nprintf '%s\\n' '{\"type\":\"system\",\"subtype\":\"init\",\"claude_code_version\":\"" + fakecli.InitVersion + "\"}'\n"
			}
			body += "sleep 30\n"
			barrier := false
			started = func() {
				for until := time.Now().Add(5 * time.Second); time.Now().Before(until); time.Sleep(time.Millisecond) {
					if _, err := os.Stat(ready); err == nil {
						barrier = true
						return
					}
				}
			}
			defer func() { started = func() {} }()
			p := newProbe1(t, "probe1-wait-message", 10*time.Second)
			p.spec.CLI = script(t, body)
			start := time.Now()
			res, err := Probe(context.Background(), p.spec)
			took := time.Since(start)
			if err != nil || !barrier || res.InputStatus != InputFailed || res.StdinWritten != 0 {
				t.Fatalf("setup: %v barrier %v %+v", err, barrier, res)
			}
			if res.Status != ProbeFailed || res.Code != "input_failed" || res.Observation != nil || took > 7*time.Second {
				t.Fatalf("%s %s observation %v after %v", res.Status, res.Code, res.Observation != nil, took)
			}
		})
	}
}

// The legitimate early path: the init comes first, the run stops, and the
// write that had not begun never does. That is an observation, not a
// failure. The beforeWrite hook holds the write until the run is stopping.
func TestProbeOneMessageInitBeforeWrite(t *testing.T) {
	beforeWrite = func(iw *stdinWriter) {
		for until := time.Now().Add(5 * time.Second); time.Now().Before(until); time.Sleep(time.Millisecond) {
			iw.mu.Lock()
			stopped := iw.stopped
			iw.mu.Unlock()
			if stopped {
				return
			}
		}
	}
	defer func() { beforeWrite = func(*stdinWriter) {} }()
	p := newProbe1(t, "probe-init-first", 10*time.Second)
	res, _ := p.run(context.Background())
	if res.Status != ProbeObserved || res.InputStatus != InputNotStarted || res.StdinWritten != 0 || p.stdinBytes() != "0" || res.Observation == nil {
		t.Fatalf("%+v, fake %s bytes", res, p.stdinBytes())
	}
}

// Both ready at once, in the order that used to win: the write fails on
// the broken pipe first, then the CLI prints its init, the run stops on it,
// and only then is the write result delivered. The order is fixed by
// handshakes, not delays: the CLI has closed stdin before the write (start
// hook as barrier); the afterWrite hook signals the CLI to print its init
// only once the write has failed, then holds the result until the run has
// stopped on that init. The result must still be the write failure, with
// no observation.
func TestProbeOneMessageWriteFailureAfterInit(t *testing.T) {
	dir := t.TempDir()
	ready, written := filepath.Join(dir, "ready"), filepath.Join(dir, "written")
	cli := script(t, "exec 0<&-\n: > "+ready+"\nwhile [ ! -f "+written+" ]; do sleep 0.01; done\n"+
		"printf '%s\\n' '{\"type\":\"system\",\"subtype\":\"init\",\"claude_code_version\":\""+fakecli.InitVersion+"\"}'\nsleep 30\n")
	barrier, held := false, false
	started = func() { // the CLI has closed stdin before the write
		for until := time.Now().Add(5 * time.Second); time.Now().Before(until); time.Sleep(time.Millisecond) {
			if _, err := os.Stat(ready); err == nil {
				barrier = true
				return
			}
		}
	}
	afterWrite = func(iw *stdinWriter) {
		os.WriteFile(written, nil, 0o600) // the write is over: let the CLI print its init
		for until := time.Now().Add(5 * time.Second); time.Now().Before(until); time.Sleep(time.Millisecond) {
			iw.mu.Lock()
			stopped := iw.stopped
			iw.mu.Unlock()
			if stopped { // the run has stopped on the init
				held = true
				return
			}
		}
	}
	defer func() { started, afterWrite = func() {}, func(*stdinWriter) {} }()
	p := newProbe1(t, "probe1-wait-message", 10*time.Second)
	p.spec.CLI = cli
	res, err := Probe(context.Background(), p.spec)
	if err != nil || !barrier || !held || res.InputStatus != InputFailed || res.Outcome != InitObserved {
		t.Fatalf("setup (write failed, then the init stopped the run): %v barrier %v held %v %+v", err, barrier, held, res)
	}
	if res.Status != ProbeFailed || res.Code != "input_failed" || res.Observation != nil {
		t.Fatalf("a failed write reported as %s %s, observation %v", res.Status, res.Code, res.Observation != nil)
	}
}
