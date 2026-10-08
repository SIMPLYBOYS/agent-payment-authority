package main

// Tests of B1-06 slice 3.1 (docs/demo-operation-design.md sections 3, 6.1
// and 6.3): GET /api/host/status, the host page /host, the expiry
// observation, and scripts/demo/start.sh.

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/SIMPLYBOYS/agent-payment-authority/internal/store"
)

// unityBuildDir writes the named build files into a new directory.
func unityBuildDir(t *testing.T, names ...string) string {
	t.Helper()
	dir := t.TempDir()
	for _, n := range names {
		os.WriteFile(filepath.Join(dir, n), []byte("// "+n), 0o600)
	}
	return dir
}

func (h *host) status(token string) (int, statusReply, []byte) {
	h.t.Helper()
	code, body, _ := h.get("/api/host/status", token)
	var s statusReply
	json.Unmarshal(body, &s)
	return code, s, body
}

func TestHostStatusAccess(t *testing.T) {
	h := startHost(t, hostOpts{expiryEvery: time.Hour, args: []string{"--unity-build", unityBuildDir(t, "WebGL.data", "WebGL.framework.js", "WebGL.loader.js", "WebGL.wasm")}})
	defer h.stop()
	for name, c := range map[string]struct {
		token string
		mod   func(*http.Request)
		want  int
	}{
		"no token":              {"", nil, 401},
		"unknown token":         {strings.Repeat("0", 64), nil, 401},
		"viewer A":              {h.tokens["viewer:consumer-a"], nil, 403},
		"viewer B":              {h.tokens["viewer:consumer-b"], nil, 403},
		"host":                  {h.tokens["host"], nil, 200},
		"operator (3.2)":        {h.tokens["operator"], nil, 200},
		"host, foreign Host":    {h.tokens["host"], func(r *http.Request) { r.Host = "evil.example:8402" }, 403},
		"host, foreign Origin":  {h.tokens["host"], func(r *http.Request) { r.Header.Set("Origin", "http://evil.example") }, 403},
		"host token in the URL": {"", func(r *http.Request) { r.URL.RawQuery = "token=" + h.tokens["host"] }, 401},
	} {
		var mods []func(*http.Request)
		if c.mod != nil {
			mods = append(mods, c.mod)
		}
		if code, _, _ := h.get("/api/host/status", c.token, mods...); code != c.want {
			t.Errorf("%s: %d, want %d", name, code, c.want)
		}
	}
	code, s, body := h.status(h.tokens["host"])
	if code != 200 || s.HTTP != "ok" || s.LiveCLI != "off" || s.PaymentCapability != "unavailable" || s.FakeCLI.State != "off" ||
		s.UnityBuild.State != "ok" || len(s.UnityBuild.Files) != 4 || s.ServedAt == 0 {
		t.Errorf("status %d %+v", code, s)
	}
	// Fixed fields only: no token, no path of this host's files.
	for _, secret := range []string{h.tokens["host"], h.tokens["viewer:consumer-a"], h.tokens["viewer:consumer-b"], h.dir, "tokens.json", "demo.db", testRoot} {
		if bytes.Contains(body, []byte(secret)) {
			t.Errorf("status names %q", secret)
		}
	}
	var fields map[string]any
	json.Unmarshal(body, &fields)
	// 3.2 added caller_role; docs/cli-poc.md 56 added build (the binary's own
	// vcs record: revision, modified, time, nothing else).
	build, _ := fields["build"].(map[string]any)
	if len(fields) != 9 || fields["caller_role"] != "host" || len(build) != 3 || build["revision"] == nil || build["modified"] == nil || build["time"] == nil {
		t.Errorf("status fields %v", fields)
	}
}

func TestHostStatusUnityBuild(t *testing.T) {
	for name, c := range map[string]struct {
		dir   string
		state string
		files int
	}{
		"complete":     {unityBuildDir(t, "WebGL.data", "WebGL.framework.js", "WebGL.loader.js", "WebGL.wasm"), "ok", 4},
		"two missing":  {unityBuildDir(t, "WebGL.loader.js", "WebGL.wasm", "index.html"), "missing", 2},
		"no directory": {filepath.Join(t.TempDir(), "none"), "missing", 0},
	} {
		h := startHost(t, hostOpts{expiryEvery: time.Hour, args: []string{"--unity-build", c.dir}})
		_, s, _ := h.status(h.tokens["host"])
		if s.UnityBuild.State != c.state || len(s.UnityBuild.Files) != c.files {
			t.Errorf("%s: %+v", name, s.UnityBuild)
		}
		h.stop()
	}
}

// The state is the latest completed check; until there is one it is
// unconfirmed, never ok. A later success does not hide the last error.
func TestExpiryHealth(t *testing.T) {
	e := &expiryHealth{targetEvery: 1}
	if v := e.view(); v.State != "unconfirmed" || v.CheckedAt != 0 || v.LastError != "" || v.TargetPeriodSeconds != 1 {
		t.Fatalf("before any check: %+v", v)
	}
	e.observe(100, nil)
	if v := e.view(); v.State != "ok" || v.CheckedAt != 100 || v.LastError != "" {
		t.Fatalf("after a success: %+v", v)
	}
	e.observe(101, errors.New("disk I/O error")) // no Store code: a fixed one, the text stays in the log
	if v := e.view(); v.State != "error" || v.CheckedAt != 101 || v.LastError != "STORE_ERROR" || v.LastErrorAt != 101 {
		t.Fatalf("after an error: %+v", v)
	}
	e.observe(102, nil)
	if v := e.view(); v.State != "ok" || v.CheckedAt != 102 || v.LastError != "STORE_ERROR" || v.LastErrorAt != 101 {
		t.Fatalf("after recovering: %+v", v)
	}
	e.observe(103, &store.Error{Code: "DB_BUSY", Detail: "x"})
	if v := e.view(); v.State != "error" || v.LastError != "DB_BUSY" || v.LastErrorAt != 103 {
		t.Fatalf("a coded error: %+v", v)
	}
}

// The host's one worker reports into the status: unconfirmed before its
// first tick (here an hour away), ok once it has run.
func TestHostStatusFollowsTheWorker(t *testing.T) {
	h := startHost(t, hostOpts{expiryEvery: time.Hour})
	if _, s, _ := h.status(h.tokens["host"]); s.Store.State != "unconfirmed" || s.Store.CheckedAt != 0 || s.Store.TargetPeriodSeconds != 3600 {
		t.Errorf("before the first tick: %+v", s.Store)
	}
	h.stop()
	h = startHost(t, hostOpts{dir: h.dir, expiryEvery: 20 * time.Millisecond})
	defer h.stop()
	var s statusReply
	for end := time.Now().Add(10 * time.Second); time.Now().Before(end); time.Sleep(20 * time.Millisecond) {
		if _, s, _ = h.status(h.tokens["host"]); s.Store.State == "ok" {
			break
		}
	}
	if s.Store.State != "ok" || s.Store.CheckedAt == 0 || s.Store.LastError != "" {
		t.Errorf("after ticks: %+v", s.Store)
	}
}

// Asking for the status writes nothing: no event of any task changes and
// the database files keep their size and time.
func TestHostStatusWritesNothing(t *testing.T) {
	h := startHost(t, hostOpts{runs: []planRun{{TaskID: "task-a", Scenario: mainline(t)}, {TaskID: "task-b", Scenario: mainline(t), BudgetInitial: "250000"}},
		expiryEvery: time.Hour})
	defer h.stop()
	h.waitRuns()
	if _, s, _ := h.status(h.tokens["host"]); s.FakeCLI.State != "ok" {
		t.Errorf("fake CLI with a plan: %+v", s.FakeCLI)
	}
	before := map[string]string{}
	for _, id := range []string{"task-a", "task-b"} {
		_, b, _ := h.get("/api/tasks/"+id+"/events", h.tokens["host"])
		before[id] = string(b)
	}
	files := func() string {
		var out strings.Builder
		for _, f := range dbFiles(filepath.Join(h.dir, "demo.db"))[:3] {
			if fi, err := os.Stat(f); err == nil {
				fmt.Fprintf(&out, "%s:%s:%d;", f, fi.ModTime(), fi.Size())
			}
		}
		return out.String()
	}
	f0 := files()
	for range 50 {
		if code, _, _ := h.status(h.tokens["host"]); code != 200 {
			t.Fatal(code)
		}
	}
	for id, b := range before {
		if _, now, _ := h.get("/api/tasks/"+id+"/events", h.tokens["host"]); string(now) != b {
			t.Errorf("%s events changed after status queries", id)
		}
	}
	if files() != f0 {
		t.Error("the database files changed after status queries")
	}
}

func TestHostPage(t *testing.T) {
	h := startHost(t, hostOpts{expiryEvery: time.Hour})
	defer h.stop()
	code, body, hdr := h.get("/host", "")
	if code != 200 || !strings.Contains(hdr.Get("Content-Security-Policy"), "script-src 'self' 'wasm-unsafe-eval'") ||
		!bytes.Contains(body, []byte(`id="token-file"`)) || !bytes.Contains(body, []byte(`/ui/host.js`)) {
		t.Errorf("/host: %d %s", code, hdr)
	}
	if _, body, _ := h.get("/", ""); bytes.Contains(body, []byte(`type="file"`)) || bytes.Contains(body, []byte("host.js")) {
		t.Error("the viewer page / offers the token file")
	}
	for _, a := range []string{"host.js", "hostcreds.js"} {
		if code, _, hdr := h.get("/ui/"+a, ""); code != 200 || !strings.HasPrefix(hdr.Get("Content-Type"), "text/javascript") {
			t.Errorf("/ui/%s: %d", a, code)
		}
	}
	if code, _, _ := h.get("/host", "", func(r *http.Request) { r.Host = "evil.example" }); code != 403 {
		t.Errorf("/host with a foreign Host: %d", code)
	}
}

// scriptBin builds the demo once and names the test binary fake-claude
// (TestMain turns it into the fake CLI), for APA_DEMO_BIN.
func scriptBin(t *testing.T) string {
	t.Helper()
	bin := t.TempDir()
	if out, err := exec.Command("go", "build", "-o", filepath.Join(bin, "demo"), "./cmd/demo").CombinedOutput(); err != nil {
		t.Fatalf("go build: %v %s", err, out)
	}
	exe, _ := os.Executable()
	if err := os.Symlink(exe, filepath.Join(bin, "fake-claude")); err != nil {
		t.Fatal(err)
	}
	return bin
}

// startScript prepares scripts/demo/start.sh with a database and token file
// of its own and an empty Unity project directory (no build, no import).
func startScript(t *testing.T, bin string, env ...string) (*exec.Cmd, *lockedBuffer) {
	t.Helper()
	dir, _ := os.MkdirTemp(testRoot, "script-")
	// The child's temporary files (fake HOME, bridge socket) go to a
	// directory of this test under the test process's own temporary
	// directory (the container's scratch), never /tmp. Kept short: a Unix
	// socket path must fit in about 100 bytes.
	scratch, err := os.MkdirTemp("", "apa-s-")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.RemoveAll(scratch) })
	c := exec.Command("sh", "scripts/demo/start.sh")
	c.Env = append([]string{"PATH=" + os.Getenv("PATH"), "HOME=" + dir, "TMPDIR=" + scratch, "APA_DEMO_BIN=" + bin,
		"APA_DB_PATH=" + filepath.Join(dir, "demo.db"), "APA_DEMO_TOKEN_FILE=" + filepath.Join(dir, "tokens.json"),
		"APA_UNITY_PROJECT=" + t.TempDir()}, env...)
	out := &lockedBuffer{}
	c.Stdout, c.Stderr = out, out
	return c, out
}

// envOf is the value of name in the script's environment.
func envOf(c *exec.Cmd, name string) string {
	for _, kv := range c.Env {
		if v, ok := strings.CutPrefix(kv, name+"="); ok {
			return v
		}
	}
	return ""
}

func TestStartScript(t *testing.T) {
	bin := scriptBin(t)
	t.Run("address in use, nothing of Unity: messages and the host's exit code", func(t *testing.T) {
		ln, err := net.Listen("tcp", "127.0.0.1:0")
		if err != nil {
			t.Fatal(err)
		}
		defer ln.Close()
		c, out := startScript(t, bin, "APA_ADDR="+ln.Addr().String())
		err = c.Run()
		var exit *exec.ExitError
		if !errors.As(err, &exit) || exit.ExitCode() != 1 {
			t.Fatalf("exit: %v", err)
		}
		for _, want := range []string{"Unity 建置不完整", "LimeZu 匯入紀錄", "already in use"} {
			if !strings.Contains(out.String(), want) {
				t.Errorf("output lacks %q: %s", want, out.String())
			}
		}
	})
	t.Run("database locked by another demo process", func(t *testing.T) {
		c, out := startScript(t, bin, "APA_ADDR=127.0.0.1:0")
		db := envOf(c, "APA_DB_PATH")
		os.MkdirAll(filepath.Dir(db), 0o700)
		f, err := os.OpenFile(db+".lock", os.O_RDWR|os.O_CREATE, 0o600)
		if err != nil {
			t.Fatal(err)
		}
		defer f.Close()
		if err := syscall.Flock(int(f.Fd()), syscall.LOCK_EX|syscall.LOCK_NB); err != nil {
			t.Fatal(err)
		}
		err = c.Run()
		var exit *exec.ExitError
		if !errors.As(err, &exit) || exit.ExitCode() != 1 || !strings.Contains(out.String(), "in use by another demo process") {
			t.Fatalf("exit %v: %s", err, out.String())
		}
	})
	t.Run("starts, serves /host, prints no token, stops on SIGTERM", func(t *testing.T) {
		c, out := startScript(t, bin, "APA_ADDR=127.0.0.1:0")
		if err := c.Start(); err != nil {
			t.Fatal(err)
		}
		var addr string
		for end := time.Now().Add(60 * time.Second); addr == "" && time.Now().Before(end); time.Sleep(50 * time.Millisecond) {
			if i := strings.Index(out.String(), "listening on http://"); i >= 0 {
				addr = strings.Fields(out.String()[i+len("listening on http://"):])[0]
			}
		}
		if addr == "" {
			c.Process.Kill()
			t.Fatalf("never served: %s", out.String())
		}
		req, _ := http.NewRequest(http.MethodGet, "http://"+addr+"/host", nil)
		resp, err := noProxy.Do(req)
		if err != nil || resp.StatusCode != 200 {
			t.Errorf("/host: %v %v", err, resp)
		}
		if resp != nil {
			resp.Body.Close()
		}
		raw, _ := os.ReadFile(envOf(c, "APA_DEMO_TOKEN_FILE"))
		var file struct{ Tokens []demoToken }
		json.Unmarshal(raw, &file)
		if len(file.Tokens) == 0 {
			t.Fatal("no token file")
		}
		c.Process.Signal(syscall.SIGTERM)
		err = c.Wait()
		for _, tok := range file.Tokens {
			if strings.Contains(out.String(), tok.Token) {
				t.Error("the script or the host printed a token")
			}
		}
		if err != nil {
			t.Errorf("exit after SIGTERM: %v", err)
		}
		if !strings.Contains(out.String(), "/host") || !strings.Contains(out.String(), "Unity 建置不完整") {
			t.Errorf("output: %s", out.String())
		}
	})
}
