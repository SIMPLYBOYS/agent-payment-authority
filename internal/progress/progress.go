// Package progress is the small terminal-progress helper shared by the M1
// commands (cmd/m1-live, cmd/m1-check): a heartbeat line while a step has
// nothing new to say, and the bounded stop of a command's own process
// group. It reads nothing of what the command prints.
package progress

import (
	"errors"
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
// done, stops the whole group: SIGTERM first (the command's own bounded
// cleanup), then, if any member is still there after grace, SIGKILL.
// The leader ending is not the group ending: a member that ignores SIGTERM
// and closed its copies of the pipes outlives the leader, so the stop
// watches the group itself until it is empty or killed.
//
// Signals go to this group's ID only, never by name. The ID cannot be
// reused while the group exists (POSIX); the stop sends a signal only
// while the leader is unreaped or a probe at most probeEvery earlier
// found the group, and stops sending once a probe finds it gone. A
// process that left the group (its own new group or session) is out of
// reach and stays the command's own responsibility.
type Group struct {
	stopped atomic.Bool
	ended   chan struct{} // closed when a stop has finished
	left    bool          // members were still there after the SIGKILL bound
}

// probeEvery is how often a stop looks whether the group is empty;
// afterKill bounds the wait for the group to go after SIGKILL.
const (
	probeEvery = 20 * time.Millisecond
	afterKill  = 5 * time.Second
)

// Prepare sets c up; call it before c.Start or c.Run, and call Done after
// c.Wait or c.Run returns.
func (g *Group) Prepare(c *exec.Cmd, grace time.Duration) {
	g.ended = make(chan struct{})
	c.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	c.Cancel = func() error {
		g.stopped.Store(true)
		pgid := c.Process.Pid
		syscall.Kill(-pgid, syscall.SIGTERM)
		go func() {
			defer close(g.ended)
			if gone(pgid, time.Now().Add(grace)) {
				return
			}
			syscall.Kill(-pgid, syscall.SIGKILL)
			g.left = !gone(pgid, time.Now().Add(afterKill))
		}()
		return nil
	}
	// exec's own last resort (the direct child only, and its pipes) comes
	// after the group kill.
	c.WaitDelay = grace + 5*time.Second
}

// gone probes the group until it is empty (true) or until passes (false).
// A zombie member still counts until it is reaped.
func gone(pgid int, until time.Time) bool {
	for {
		if errors.Is(syscall.Kill(-pgid, 0), syscall.ESRCH) {
			return true
		}
		if time.Now().After(until) {
			return false
		}
		time.Sleep(probeEvery)
	}
}

// Done returns at once when no stop ran (a command that ended by itself is
// not waited for or signalled). After a stop it waits until the stop has
// finished: the group empty, or killed (at most grace plus afterKill from
// the stop), and reports a group that outlived the SIGKILL.
func (g *Group) Done() error {
	if !g.stopped.Load() {
		return nil
	}
	<-g.ended
	if g.left {
		return fmt.Errorf("the process group still had members %s after SIGKILL", afterKill)
	}
	return nil
}

// Stopped says whether the context ended the command.
func (g *Group) Stopped() bool { return g.stopped.Load() }
