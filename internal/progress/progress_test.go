package progress

import (
	"bytes"
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"testing"
	"time"
)

type syncBuf struct {
	mu sync.Mutex
	b  bytes.Buffer
}

func (s *syncBuf) Write(p []byte) (int, error) { s.mu.Lock(); defer s.mu.Unlock(); return s.b.Write(p) }
func (s *syncBuf) String() string              { s.mu.Lock(); defer s.mu.Unlock(); return s.b.String() }

// A heartbeat ticks while the step is quiet and never after stop returns.
func TestHeartbeat(t *testing.T) {
	var out syncBuf
	stop := Heartbeat(&out, 20*time.Millisecond, func(d time.Duration) string { return "beat" })
	time.Sleep(110 * time.Millisecond)
	stop()
	n := strings.Count(out.String(), "beat")
	if n < 2 {
		t.Fatalf("%d heartbeats in 110 ms at 20 ms", n)
	}
	time.Sleep(80 * time.Millisecond)
	if strings.Count(out.String(), "beat") != n {
		t.Fatal("a heartbeat after stop")
	}
	stop() // twice is fine
}

// alive says whether pid is a running process (not reaped, not a zombie).
func alive(pid int) bool {
	out, _ := exec.Command("ps", "-o", "stat=", "-p", strconv.Itoa(pid)).Output()
	s := strings.TrimSpace(string(out))
	return s != "" && !strings.HasPrefix(s, "Z")
}

// Cancelling stops the whole group, a grandchild in it included; one that
// ignores SIGTERM is killed after the grace period.
func TestGroupStop(t *testing.T) {
	for name, script := range map[string]string{
		"ends on SIGTERM": "sleep 60 & echo $! > %s; wait",
		"ignores SIGTERM": "trap '' TERM; sh -c 'trap \"\" TERM; sleep 60' & echo $! > %s; wait",
	} {
		t.Run(name, func(t *testing.T) {
			pidFile := filepath.Join(t.TempDir(), "pid")
			ctx, cancel := context.WithCancel(context.Background())
			c := exec.CommandContext(ctx, "sh", "-c", strings.Replace(script, "%s", pidFile, 1))
			var g Group
			g.Prepare(c, 300*time.Millisecond)
			if err := c.Start(); err != nil {
				t.Fatal(err)
			}
			var grandchild int
			for i := 0; i < 200 && grandchild == 0; i++ {
				b, _ := os.ReadFile(pidFile)
				grandchild, _ = strconv.Atoi(strings.TrimSpace(string(b)))
				time.Sleep(10 * time.Millisecond)
			}
			if grandchild == 0 {
				t.Fatal("the grandchild did not start")
			}
			cancel()
			start := time.Now()
			c.Wait()
			if err := g.Done(); err != nil {
				t.Fatal(err)
			}
			if !g.Stopped() {
				t.Fatal("not recorded as stopped")
			}
			for i := 0; i < 100 && alive(grandchild); i++ {
				time.Sleep(20 * time.Millisecond)
			}
			if alive(grandchild) {
				syscall.Kill(grandchild, syscall.SIGKILL)
				t.Fatal("the grandchild outlived the stop")
			}
			if time.Since(start) > 10*time.Second {
				t.Fatalf("the stop took %s", time.Since(start))
			}
		})
	}
}

// A command that ends by itself is not stopped, and Done does not wait
// for anything.
func TestGroupNotStopped(t *testing.T) {
	c := exec.CommandContext(context.Background(), "sh", "-c", "exit 3")
	var g Group
	g.Prepare(c, 10*time.Second)
	err := c.Run()
	start := time.Now()
	if derr := g.Done(); derr != nil || time.Since(start) > time.Second {
		t.Fatalf("Done %v after %s", derr, time.Since(start))
	}
	if g.Stopped() || c.ProcessState.ExitCode() != 3 || err == nil {
		t.Fatalf("stopped %v, exit %d", g.Stopped(), c.ProcessState.ExitCode())
	}
}

// state is "gone", "zombie" or "running".
func state(pid int) string {
	out, _ := exec.Command("ps", "-o", "stat=", "-p", strconv.Itoa(pid)).Output()
	switch s := strings.TrimSpace(string(out)); {
	case s == "":
		return "gone"
	case strings.HasPrefix(s, "Z"):
		return "zombie"
	}
	return "running"
}

// The leader takes SIGTERM and exits, but a member of the same group
// ignores SIGTERM and has closed its copies of the pipes, so the leader's
// Wait returns while the member runs on (10/11 review, R1). The stop must
// still end it: Done returns only once it is no longer running, after the
// grace period (it ignored SIGTERM) and within the SIGKILL bound.
func TestGroupMixed(t *testing.T) {
	ready := filepath.Join(t.TempDir(), "ready")
	member := `trap "" TERM; exec >/dev/null 2>&1 </dev/null; echo $$ > ` + ready + `.tmp; mv ` + ready + `.tmp ` + ready + `; while :; do sleep 0.05; done`
	ctx, cancel := context.WithCancel(context.Background())
	c := exec.CommandContext(ctx, "sh", "-c", "sh -c '"+member+"' & wait")
	var out bytes.Buffer
	c.Stdout, c.Stderr = &out, &out // pipes, as the M1 commands use
	var g Group
	const grace = 300 * time.Millisecond
	g.Prepare(c, grace)
	if err := c.Start(); err != nil {
		t.Fatal(err)
	}
	var pid int
	for i := 0; i < 500 && pid == 0; i++ {
		b, _ := os.ReadFile(ready)
		pid, _ = strconv.Atoi(strings.TrimSpace(string(b)))
		time.Sleep(10 * time.Millisecond)
	}
	if pid == 0 {
		t.Fatal("the member did not start")
	}
	defer syscall.Kill(pid, syscall.SIGKILL) // only matters when the test fails
	cancel()
	start := time.Now()
	c.Wait()
	if s := state(pid); s != "running" {
		t.Fatalf("the member is %s when the leader's Wait returned; the case did not arise", s)
	}
	err := g.Done()
	took := time.Since(start)
	if s := state(pid); s == "running" {
		t.Fatalf("the member is still running after Done (%s, %v)", took, err)
	}
	if err != nil || took < grace || took > grace+afterKill {
		t.Fatalf("Done %v after %s", err, took)
	}
}
