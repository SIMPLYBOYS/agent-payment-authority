package main

// HTTP tests of the Dashboard routes (docs/demo-ui.md): the page, its listed
// assets, the Unity build files, and GET /api/viewer.

import (
	"bytes"
	"encoding/json"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/SIMPLYBOYS/agent-payment-authority/internal/buyer/fakecli"
)

// fakeBuild is a stand-in Unity build directory: the four listed files, an
// extra file, and a link that points out of the directory.
func fakeBuild(t *testing.T, secret string) string {
	t.Helper()
	dir := t.TempDir()
	for _, n := range []string{"WebGL.loader.js", "WebGL.framework.js", "WebGL.wasm"} {
		os.WriteFile(filepath.Join(dir, n), []byte("// "+n), 0o600)
	}
	os.WriteFile(filepath.Join(dir, "index.html"), []byte("not to be served"), 0o600)
	if err := os.Symlink(secret, filepath.Join(dir, "WebGL.data")); err != nil {
		t.Fatal(err)
	}
	return dir
}

func TestDashboardRoutes(t *testing.T) {
	capFile := filepath.Join(t.TempDir(), "cap")
	secretDir, _ := os.MkdirTemp(testRoot, "secret-")
	secret := filepath.Join(secretDir, "outside.txt")
	os.WriteFile(secret, []byte("SECRET-OUTSIDE-THE-BUILD"), 0o600)
	h := startHost(t, hostOpts{runs: []planRun{{TaskID: "task-a", Scenario: mainline(t)}}, expiryEvery: time.Hour,
		fakeEnv: []string{fakecli.EnvCapFile + "=" + capFile}, args: []string{"--unity-build", fakeBuild(t, secret)}})
	defer h.stop()
	h.waitRuns()
	before := h.events("task-a")

	t.Run("page and assets", func(t *testing.T) {
		code, body, hdr := h.get("/", "")
		if code != 200 || !bytes.Contains(body, []byte("模式未確認（讀取中）")) || bytes.Contains(body, []byte("M1／fake agent")) /* docs/cli-poc.md 56.8: never fake before the mode is known */ || !strings.Contains(hdr.Get("Content-Security-Policy"), "script-src 'self' 'wasm-unsafe-eval'") ||
			hdr.Get("X-Frame-Options") != "DENY" || hdr.Get("Cache-Control") != "no-store" || hdr.Get("Access-Control-Allow-Origin") != "" {
			t.Fatalf("page: %d %v", code, hdr)
		}
		for _, n := range []string{"app.js", "adapter.js", "render.js", "unity.js", "style.css"} {
			if code, _, hdr := h.get("/ui/"+n, ""); code != 200 || hdr.Get("X-Content-Type-Options") != "nosniff" {
				t.Errorf("/ui/%s: %d", n, code)
			}
		}
		if code, _, _ := h.get("/", "", func(r *http.Request) { r.Host = "evil.example" }); code != 403 {
			t.Errorf("page under another Host: %d", code)
		}
	})

	t.Run("nothing else is served", func(t *testing.T) {
		for _, p := range []string{"/ui/embed.go", "/ui/index.html", "/ui/test", "/ui/..%2fembed.go", "/ui/package.json", "/web/app.js",
			"/tokens.json", "/demo.db", "/var/demo-tokens.json", "/unity/index.html", "/unity/..%2f..%2fdemo.db", "/unity/WebGL.data",
			"/unity/Shop.unity", "/README.md", "/.git/config"} { // /api/tasks is served since slice 3.3 (TestTaskList)
			code, body, _ := h.get(p, h.tokens["host"])
			if code == 200 || bytes.Contains(body, []byte("SECRET-OUTSIDE")) || bytes.Contains(body, []byte("SQLite")) || bytes.Contains(body, []byte(h.tokens["host"])) {
				t.Errorf("%s: %d %.80s", p, code, body)
			}
		}
		if code, body, hdr := h.get("/unity/WebGL.wasm", ""); code != 200 || hdr.Get("Content-Type") != "application/wasm" || string(body) != "// WebGL.wasm" {
			t.Errorf("listed build file: %d %s", code, hdr.Get("Content-Type"))
		}
	})

	t.Run("viewer endpoint", func(t *testing.T) {
		capability, _ := os.ReadFile(capFile)
		code, body, hdr := h.get("/api/viewer", h.tokens["viewer:consumer-a"])
		var v viewerReply
		json.Unmarshal(body, &v)
		if code != 200 || v.Role != "viewer" || v.ConsumerID != "consumer-a" || hdr.Get("Cache-Control") != "no-store" {
			t.Fatalf("viewer A: %d %s", code, body)
		}
		if bytes.Contains(body, []byte("task-")) || bytes.Contains(body, []byte("consumer-b")) || bytes.Contains(body, []byte("wallet")) {
			t.Errorf("the viewer reply says more than who it is: %s", body)
		}
		for name, c := range map[string]struct {
			token string
			mod   func(*http.Request)
			code  int
		}{
			"host token":       {h.tokens["host"], nil, 403},
			"MCP capability":   {string(capability), nil, 401},
			"no token":         {"", nil, 401},
			"other Host":       {h.tokens["viewer:consumer-a"], func(r *http.Request) { r.Host = "localhost:1" }, 403},
			"other Origin":     {h.tokens["viewer:consumer-a"], func(r *http.Request) { r.Header.Set("Origin", "http://evil.example") }, 403},
			"token in the URL": {"", func(r *http.Request) { r.URL.RawQuery = "token=" + h.tokens["viewer:consumer-a"] }, 401},
		} {
			var mods []func(*http.Request)
			if c.mod != nil {
				mods = append(mods, c.mod)
			}
			if code, body, _ := h.get("/api/viewer", c.token, mods...); code != c.code || bytes.Contains(body, []byte("consumer")) {
				t.Errorf("%s: %d %s", name, code, body)
			}
		}
	})

	t.Run("loading the page writes nothing", func(t *testing.T) {
		for range 5 {
			for _, p := range []string{"/", "/ui/app.js", "/unity/WebGL.loader.js"} {
				h.get(p, "")
			}
			h.get("/api/viewer", h.tokens["viewer:consumer-a"])
			h.get("/api/tasks/task-a", h.tokens["viewer:consumer-a"])
			h.get("/api/tasks/task-a/events", h.tokens["viewer:consumer-a"])
		}
		if after := h.events("task-a"); after.AsOfSequence != before.AsOfSequence || len(after.Events) != len(before.Events) {
			t.Errorf("events changed: %d -> %d", before.AsOfSequence, after.AsOfSequence)
		}
	})
}

// Without a Unity build the page still loads; the build route answers 404
// and the page says the scene is not ready (asserted in the browser test).
func TestDashboardWithoutUnityBuild(t *testing.T) {
	h := startHost(t, hostOpts{args: []string{"--unity-build", filepath.Join(t.TempDir(), "none")}})
	defer h.stop()
	if code, _, _ := h.get("/", ""); code != 200 {
		t.Errorf("page: %d", code)
	}
	if code, _, _ := h.get("/unity/WebGL.loader.js", ""); code != 404 {
		t.Errorf("build file without a build: %d", code)
	}
}
