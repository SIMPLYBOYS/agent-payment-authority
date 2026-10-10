package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"
)

// These tests run the runner on small sh plans (--plan), never on the
// M1-E3 plan itself.

const marker = "SYNTHETIC-SENSITIVE-MARKER-7f3a" // a step's output: only in its log

func plan(t *testing.T, steps ...Step) string {
	t.Helper()
	raw, _ := json.Marshal(steps)
	p := filepath.Join(t.TempDir(), "plan.json")
	os.WriteFile(p, raw, 0o600)
	return p
}

func sh(name, script string, timeout time.Duration, kind string) Step {
	return Step{Name: name, Cmd: []string{"sh", "-c", script}, Timeout: timeout, Kind: kind}
}

func check(t *testing.T, ctx context.Context, args ...string) (int, Summary, string) {
	t.Helper()
	var so, se bytes.Buffer
	code := run(ctx, args, &so, &se)
	var sum Summary
	if err := json.Unmarshal(so.Bytes(), &sum); err != nil {
		t.Fatalf("stdout is not one JSON summary: %v\n%q", err, so.String())
	}
	if strings.Contains(so.String()+se.String(), marker) {
		t.Fatal("a step's output reached stdout or the progress")
	}
	return code, sum, se.String()
}

func results(sum Summary) string {
	var r []string
	for _, s := range sum.Steps {
		r = append(r, s.Result)
	}
	return strings.Join(r, ",")
}

// go test -json events give the progress its test names; the evidence is
// a new directory with the plan, each step's log, the progress and the
// summary; the step output stays in its log.
func TestPass(t *testing.T) {
	out, src := t.TempDir(), filepath.Join(t.TempDir(), "secret") // the plan's commands are printed; the marker is not in them
	os.WriteFile(src, []byte(marker+"\n"), 0o600)
	events := `printf '%s\n' '{"Action":"run","Test":"TestA"}' '{"Action":"pass","Test":"TestA"}'; echo "{\"Action\":\"output\",\"Test\":\"TestA\",\"Output\":\"$(cat ` + src + `)\"}"`
	code, sum, se := check(t, context.Background(), "--out", out, "--plan", plan(t,
		sh("one", "cat "+src, time.Minute, "plain"),
		sh("two", events, time.Minute, "go")))
	if code != exitPass || sum.Verdict != "PASS" || !sum.Complete || results(sum) != "PASS,PASS" {
		t.Fatalf("exit %d, %+v\n%s", code, sum, se)
	}
	if !strings.Contains(se, "START 1/2 one") || !strings.Contains(se, "run  TestA") || !strings.Contains(sum.Steps[1].Detail, "1 tests passed, 0 failed") {
		t.Fatalf("progress:\n%s", se)
	}
	for _, f := range []string{"plan.json", "progress.log", "summary.json", "01-one.log", "02-two.log"} {
		if _, err := os.Stat(filepath.Join(sum.Evidence, f)); err != nil {
			t.Fatal(err)
		}
	}
	if b, _ := os.ReadFile(filepath.Join(sum.Evidence, "01-one.log")); !strings.Contains(string(b), marker) {
		t.Fatal("the step's output is not in its log")
	}
}

// A failing step keeps its real exit code; the rest are not run.
func TestFailStopsTheRest(t *testing.T) {
	ran := filepath.Join(t.TempDir(), "ran")
	code, sum, se := check(t, context.Background(), "--out", t.TempDir(), "--plan", plan(t,
		sh("bad", "exit 7", time.Minute, "plain"),
		sh("later", "touch "+ran, time.Minute, "plain")))
	if code != exitFailed || results(sum) != "FAIL,NOT_RUN" || *sum.Steps[0].ExitCode != 7 || sum.Steps[1].ExitCode != nil {
		t.Fatalf("exit %d, %+v\n%s", code, sum, se)
	}
	if _, err := os.Stat(ran); err == nil {
		t.Fatal("a step after the failure ran")
	}
}

// running says whether pid is a live process (not reaped, not a zombie).
func running(pid int) bool {
	out, _ := exec.Command("ps", "-o", "stat=", "-p", strconv.Itoa(pid)).Output()
	s := strings.TrimSpace(string(out))
	return s != "" && !strings.HasPrefix(s, "Z")
}

func waitPid(t *testing.T, path string) int {
	t.Helper()
	for range 500 {
		b, _ := os.ReadFile(path)
		if pid, err := strconv.Atoi(strings.TrimSpace(string(b))); err == nil {
			return pid
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatal("the step's child did not start")
	return 0
}

func gone(t *testing.T, pid int) {
	t.Helper()
	for i := 0; i < 100 && running(pid); i++ {
		time.Sleep(20 * time.Millisecond)
	}
	if running(pid) {
		exec.Command("kill", "-9", strconv.Itoa(pid)).Run()
		t.Fatalf("process %d outlived the step", pid)
	}
}

// A step past its timeout is TIMEOUT, its whole group stopped, a child that
// ignores SIGTERM included.
func TestTimeout(t *testing.T) {
	pidFile := filepath.Join(t.TempDir(), "pid")
	code, sum, se := check(t, context.Background(), "--out", t.TempDir(), "--plan", plan(t,
		sh("slow", "trap '' TERM; sh -c 'trap \"\" TERM; sleep 60' & echo $! > "+pidFile+"; wait", 500*time.Millisecond, "plain"),
		sh("later", "true", time.Minute, "plain")))
	if code != exitFailed || results(sum) != "TIMEOUT,NOT_RUN" {
		t.Fatalf("exit %d, %+v\n%s", code, sum, se)
	}
	gone(t, waitPid(t, pidFile))
}

// An interrupt (the context main cancels on SIGINT or SIGTERM) stops the
// step's group and is INTERRUPTED, exit 2; the rest are not run.
func TestInterrupt(t *testing.T) {
	pidFile := filepath.Join(t.TempDir(), "pid")
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	var pid int
	go func() { defer close(done); pid = waitPid(t, pidFile); cancel() }()
	code, sum, se := check(t, ctx, "--out", t.TempDir(), "--plan", plan(t,
		sh("long", "sleep 60 & echo $! > "+pidFile+"; wait", time.Minute, "plain"),
		sh("later", "true", time.Minute, "plain")))
	<-done
	if code != exitInterrupted || sum.Verdict != "INTERRUPTED" || results(sum) != "INTERRUPTED,NOT_RUN" {
		t.Fatalf("exit %d, %+v\n%s", code, sum, se)
	}
	gone(t, pid)
}

// A quiet step gets heartbeats; none come after its result line.
func TestHeartbeat(t *testing.T) {
	_, sum, se := check(t, context.Background(), "--out", t.TempDir(), "--heartbeat", "100ms", "--plan", plan(t,
		sh("quiet", "sleep 1", time.Minute, "plain")))
	beats := strings.Count(se, "... 1/1 quiet: the process is running")
	end := strings.Index(se, "PASS 1/1 quiet")
	if sum.Verdict != "PASS" || beats < 3 || end < 0 || strings.LastIndex(se, "... 1/1 quiet") > end {
		t.Fatalf("%d heartbeats\n%s", beats, se)
	}
}

// The reverse checks pass only when every item was caught by an assertion.
func TestReverse(t *testing.T) {
	lines := func(res ...string) string {
		s := "echo PLAN 2"
		for i, r := range res {
			s += "; echo ITEM " + strconv.Itoa(i+1) + "/2 x" + strconv.Itoa(i) + "; echo 'RESULT " + r + " | 0.5s | x" + strconv.Itoa(i) + " | why'"
		}
		return s
	}
	for want, script := range map[string]string{
		"PASS":           lines("caught", "caught"),
		"FAIL":           lines("caught", "not-caught"),
		"FAIL, one item": lines("caught"), // fewer results than planned
	} {
		_, sum, se := check(t, context.Background(), "--out", t.TempDir(), "--plan", plan(t, sh("rev", script, time.Minute, "reverse")))
		if !strings.HasPrefix(want, sum.Verdict) || !strings.Contains(se, "item 1/2 x0") || !strings.Contains(se, "CAUGHT x0 (0.5s): why") {
			t.Fatalf("want %s: %+v\n%s", want, sum, se)
		}
	}
}

// A line longer than progress reads (and than the old 16 MiB scanner
// buffer) is saved whole in the log, and what follows it is still read
// (10/11 review, R3).
func TestLongLine(t *testing.T) {
	script := "head -c 17000000 /dev/zero | tr '\\0' a; echo; echo PLAN 1; echo ITEM 1/1 x; echo 'RESULT caught | 1s | x | why'; echo END-MARK"
	code, sum, se := check(t, context.Background(), "--out", t.TempDir(), "--plan", plan(t, sh("long", script, time.Minute, "reverse")))
	b, _ := os.ReadFile(sum.Steps[0].Log)
	if code != exitPass || !sum.Complete || !sum.Steps[0].LogSaved || len(b) < 17000000 || !strings.HasSuffix(string(b), "END-MARK\n") || !strings.Contains(se, "CAUGHT x") {
		t.Fatalf("exit %d, %d log bytes, %+v\n%s", code, len(b), sum, se)
	}
}

// A log over its limit is incomplete: the step keeps its own result, the
// run is incomplete, exit 3; a failing step still exits 1 (the step's
// failure comes first).
func TestLogLimit(t *testing.T) {
	defer func(m int64) { maxLog = m }(maxLog)
	maxLog = 1000
	for script, want := range map[string]int{"head -c 5000 /dev/zero": exitIncomplete, "head -c 5000 /dev/zero; exit 4": exitFailed} {
		code, sum, se := check(t, context.Background(), "--out", t.TempDir(), "--plan", plan(t, sh("big", script, time.Minute, "plain")))
		b, _ := os.ReadFile(sum.Steps[0].Log)
		wantResult := map[int]string{exitIncomplete: "PASS", exitFailed: "FAIL"}[want]
		if code != want || sum.Complete || sum.Steps[0].LogSaved || sum.Steps[0].Result != wantResult || len(b) != 1000 || !strings.Contains(sum.Steps[0].Detail, "size limit") {
			t.Fatalf("%s: exit %d, %d log bytes, %+v\n%s", script, code, len(b), sum, se)
		}
	}
}

// failAfter fails writes once n bytes were taken.
type failAfter struct {
	n     int
	short bool // a short write without an error, instead of an error
}

func (f *failAfter) Write(p []byte) (int, error) {
	if len(p) <= f.n {
		f.n -= len(p)
		return len(p), nil
	}
	k := f.n
	f.n = 0
	if f.short {
		return k, nil
	}
	return k, errors.New("disk full")
}

func (f *failAfter) Close() error { return nil }

// A log write that fails part way: the step is drained and keeps its
// result, the evidence is incomplete, exit 3.
func TestLogWriteFailure(t *testing.T) {
	defer func(o func(string) (io.WriteCloser, error)) { openLog = o }(openLog)
	openLog = func(path string) (io.WriteCloser, error) {
		if strings.HasSuffix(path, "01-w.log") {
			return &failAfter{n: 10}, nil
		}
		return os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
	}
	code, sum, se := check(t, context.Background(), "--out", t.TempDir(), "--plan", plan(t, sh("w", "head -c 300000 /dev/zero", time.Minute, "plain")))
	if code != exitIncomplete || sum.Complete || sum.Steps[0].Result != "PASS" || sum.Steps[0].LogSaved || !strings.Contains(se, "INCOMPLETE") {
		t.Fatalf("exit %d, %+v\n%s", code, sum, se)
	}
}

// A summary that cannot be written to stdout (an error, or a short write)
// is not a success: exit 3, a fixed message on stderr that says whether
// the evidence files were saved; summary.json is there. A failing step
// still exits 1.
func TestSummaryNotDelivered(t *testing.T) {
	for _, tc := range []struct {
		name   string
		out    *failAfter
		script string
		want   int
	}{
		{"error", &failAfter{n: 5}, "true", exitIncomplete},
		{"short write", &failAfter{n: 5, short: true}, "true", exitIncomplete},
		{"error, a failing step", &failAfter{n: 5}, "exit 4", exitFailed},
	} {
		t.Run(tc.name, func(t *testing.T) {
			out := t.TempDir()
			var se bytes.Buffer
			code := run(context.Background(), []string{"--out", out, "--plan", plan(t, sh("s", tc.script, time.Minute, "plain"))}, tc.out, &se)
			dirs, _ := filepath.Glob(filepath.Join(out, "m1-check-*", "summary.json"))
			if code != tc.want || len(dirs) != 1 || !strings.Contains(se.String(), "the JSON summary was NOT delivered on stdout") || !strings.Contains(se.String(), "evidence files are complete") {
				t.Fatalf("exit %d, %d summaries\n%s", code, len(dirs), se.String())
			}
		})
	}
}

// Evidence that cannot be saved makes the run incomplete, exit 3, even
// when every step passed; the summary still goes to stdout.
func TestSaveFailure(t *testing.T) {
	notDir := filepath.Join(t.TempDir(), "file")
	os.WriteFile(notDir, nil, 0o600)
	code, sum, se := check(t, context.Background(), "--out", notDir, "--plan", plan(t, sh("ok", "true", time.Minute, "plain")))
	if code != exitIncomplete || sum.Complete || !strings.Contains(se, "INCOMPLETE") {
		t.Fatalf("exit %d, %+v\n%s", code, sum, se)
	}
}

// --list runs nothing and writes nothing.
func TestList(t *testing.T) {
	ran, out := filepath.Join(t.TempDir(), "ran"), t.TempDir()
	var so, se bytes.Buffer
	code := run(context.Background(), []string{"--list", "--out", out, "--plan", plan(t, sh("one", "touch "+ran, time.Minute, "plain"))}, &so, &se)
	entries, _ := os.ReadDir(out)
	if _, err := os.Stat(ran); code != exitPass || err == nil || so.Len() != 0 || len(entries) != 0 || !strings.Contains(se.String(), "1/1 one") {
		t.Fatalf("exit %d, stdout %q, %d entries\n%s", code, so.String(), len(entries), se.String())
	}
}
