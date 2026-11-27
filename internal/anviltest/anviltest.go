// Package anviltest starts the repository's local anvil for a test (var/ is
// not in git: without it the test skips).
//
// anvil picks its own port and says which (--port 0, "Listening on ..."): a
// port picked first and handed to anvil could be taken by another test's
// anvil in between, and this test would then run on that chain (its clock,
// its blocks) while its own anvil failed to start.
package anviltest

import (
	"bufio"
	"io"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"
)

// Start runs anvil with args on a loopback port of its own and returns its
// URL; it stops with the test.
func Start(t testing.TB, args ...string) string {
	t.Helper()
	_, file, _, _ := runtime.Caller(0)
	bin := filepath.Join(filepath.Dir(file), "../../var/tools/foundry-v1.8.3/bin/anvil")
	if _, err := os.Stat(bin); err != nil {
		t.Skip("no local anvil (var/tools/foundry-v1.8.3)")
	}
	if ln, err := net.Listen("tcp", "127.0.0.1:0"); err != nil {
		t.Skip("no loopback listening here:", err)
	} else {
		ln.Close()
	}
	cmd := exec.Command(bin, append([]string{"--host", "127.0.0.1", "--port", "0"}, args...)...)
	out, err := cmd.StdoutPipe()
	if err != nil {
		t.Fatal(err)
	}
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { cmd.Process.Kill(); cmd.Wait() })
	addr := make(chan string, 1)
	go func() {
		sc := bufio.NewScanner(out)
		for sc.Scan() {
			if a, ok := strings.CutPrefix(sc.Text(), "Listening on "); ok {
				addr <- a
				io.Copy(io.Discard, out) // the rest of its output, so it never blocks
				return
			}
		}
		close(addr) // exited without listening
	}()
	select {
	case a, ok := <-addr:
		if !ok {
			t.Fatal("anvil exited without listening")
		}
		return "http://" + strings.TrimSpace(a)
	case <-time.After(30 * time.Second):
		t.Fatal("anvil did not start")
	}
	return ""
}
