// Package progress is the small terminal-progress helper shared by the M1
// commands (cmd/m1-live, cmd/m1-check): a heartbeat line while a step has
// nothing new to say, and the bounded stop of a command's own process
// group. It reads nothing of what the command prints.
package progress

import (
	"fmt"
	"io"
	"os/exec"
	"sync"
	"sync/atomic"
	"syscall"
	"time"
)

// Heartbeat writes line(elapsed) to w every interval until stop is
// called. stop waits for the writer: no heartbeat follows its return.
func Heartbeat(w io.Writer, every time.Duration, line func(elapsed time.Duration) string) (stop func()) {
	start := time.Now()
	done := make(chan struct{})
	var wg sync.WaitGroup
	wg.Go(func() {
		t := time.NewTicker(every)
		defer t.Stop()
		for {
			select {
			case <-done:
				return
			case <-t.C:
				fmt.Fprintln(w, line(time.Since(start).Truncate(time.Second)))
			}
		}
	})
	var once sync.Once
	return func() { once.Do(func() { close(done); wg.Wait() }) }
}

// Group runs c in a process group of its own and, when c's context is
// done, stops that group: SIGTERM first (the command's own bounded
// cleanup), then SIGKILL to the group if it has not ended after grace.
// Both signals go to this command's group only, and only while its leader
// has not been reaped (Wait has not returned), so the group ID cannot have
// been reused. A process that left the group (its own new group or
// session) is out of reach and stays the command's own responsibility.
// Stopped reports whether the stop ran (the context ended the command).
type Group struct {
	stopped atomic.Bool
	waited  chan struct{}
}

// Prepare sets c up; call it before c.Start or c.Run, and call Done after
// c.Wait or c.Run returns.
func (g *Group) Prepare(c *exec.Cmd, grace time.Duration) {
	g.waited = make(chan struct{})
	c.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	c.Cancel = func() error {
		g.stopped.Store(true)
		pgid := c.Process.Pid
		syscall.Kill(-pgid, syscall.SIGTERM)
		go func() {
			select {
			case <-g.waited:
			case <-time.After(grace):
				syscall.Kill(-pgid, syscall.SIGKILL)
			}
		}()
		return nil
	}
	// exec's own last resort (the direct child only, and its pipes) comes
	// after the group kill.
	c.WaitDelay = grace + 5*time.Second
}

// Done marks the leader reaped: no signal is sent to the group after it.
func (g *Group) Done() { close(g.waited) }

// Stopped says whether the context ended the command.
func (g *Group) Stopped() bool { return g.stopped.Load() }
