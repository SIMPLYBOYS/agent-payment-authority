package main

import (
	"context"
	"encoding/json"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

// The pairing code: 12 Base32 characters from crypto/rand, used once; it
// closes on success, after 5 failures or at 5 minutes (the given clock);
// nothing reopens it.
func TestPairingWindow(t *testing.T) {
	var at atomic.Int64
	base := time.Now()
	now := func() time.Time { return base.Add(time.Duration(at.Load())) }
	p, shown, err := newPairing(now)
	if err != nil || !regexp.MustCompile(`^[A-Z2-7]{4}-[A-Z2-7]{4}-[A-Z2-7]{4}$`).MatchString(shown) {
		t.Fatalf("code %q %v", shown, err)
	}
	q, other, _ := newPairing(now)
	if other == shown {
		t.Fatal("two windows, one code")
	}
	typed := strings.ToLower(strings.ReplaceAll(shown, "-", " "))
	if got := p.redeem(typed); got != "ok" {
		t.Fatalf("the right code, typed loosely: %s", got)
	}
	if got := p.redeem(shown); got != "closed" {
		t.Fatalf("a used code: %s", got)
	}
	// Five failures close the window, the right code included afterwards.
	for i := 0; i < pairingMaxFailures; i++ {
		if got := q.redeem("AAAA-AAAA-AAAA"); got != "wrong" {
			t.Fatalf("failure %d: %s", i, got)
		}
	}
	if got := q.redeem(other); got != "closed" {
		t.Fatalf("after five failures: %s", got)
	}
	// Four failures leave it open; the clock closes it at 5 minutes.
	r, third, _ := newPairing(now)
	for i := 0; i < pairingMaxFailures-1; i++ {
		r.redeem("not a code")
	}
	at.Store(int64(pairingTTL))
	if got := r.redeem(third); got != "closed" {
		t.Fatalf("at 5 minutes: %s", got)
	}
	// Many at once: exactly one success.
	s, fourth, _ := newPairing(time.Now)
	var ok atomic.Int32
	var wg sync.WaitGroup
	for i := 0; i < 50; i++ {
		wg.Go(func() {
			if s.redeem(fourth) == "ok" {
				ok.Add(1)
			}
		})
	}
	wg.Wait()
	if ok.Load() != 1 {
		t.Fatalf("%d successes", ok.Load())
	}
	for _, c := range []string{"", "ABCD-EFGH-JKL", "ABCD-EFGH-JKLMN", "ABCD-EFGH-JK1M", "ABCD_EFGH_JKLM"} {
		if normalizeCode(c) != "" {
			t.Errorf("%q taken as a code", c)
		}
	}
}

var shownCode = regexp.MustCompile(`[A-Z2-7]{4}-[A-Z2-7]{4}-[A-Z2-7]{4}`)

func pairingHost(t *testing.T, now func() time.Time) (*host, *lockedBuffer, string) {
	t.Helper()
	term := &lockedBuffer{}
	h := startHost(t, hostOpts{scenarios: "testdata/demo/scenarios.json", args: []string{"--registry", "testdata/demo/registry.json", "--pairing"}, pairOut: term, now: now})
	code := shownCode.FindString(term.String())
	if code == "" {
		t.Fatalf("no code on the terminal: %q", term.String())
	}
	return h, term, code
}

func (h *host) pair(body string, mod ...func(*http.Request)) (int, map[string]any, http.Header) {
	h.t.Helper()
	req, _ := http.NewRequest(http.MethodPost, "http://"+h.addr+"/api/host/pair", strings.NewReader(body))
	req.Header.Set("Origin", "http://"+h.addr)
	req.Header.Set("Content-Type", "application/json")
	for _, m := range mod {
		m(req)
	}
	res, err := noProxy.Do(req)
	if err != nil {
		h.t.Fatal(err)
	}
	defer res.Body.Close()
	var v map[string]any
	json.NewDecoder(res.Body).Decode(&v)
	return res.StatusCode, v, res.Header
}

// POST /api/host/pair: only this host's own page, a strict small body and
// the right code get this run's tokens, once; the code is on the terminal
// only, never in the host's log.
func TestPairingEndpoint(t *testing.T) {
	h, term, code := pairingHost(t, nil)
	defer h.stop()
	if strings.Contains(h.out.String(), code) {
		t.Fatal("the pairing code is in the host's log")
	}
	body := `{"code":"` + code + `"}`
	for name, c := range map[string]struct {
		body string
		mod  func(*http.Request)
		code int
	}{
		"no Origin":           {body, func(r *http.Request) { r.Header.Del("Origin") }, 403},
		"Origin null":         {body, func(r *http.Request) { r.Header.Set("Origin", "null") }, 403},
		"another origin":      {body, func(r *http.Request) { r.Header.Set("Origin", "http://127.0.0.1:1") }, 403},
		"another Host":        {body, func(r *http.Request) { r.Host = "localhost" }, 403},
		"not JSON":            {body, func(r *http.Request) { r.Header.Set("Content-Type", "text/plain") }, 400},
		"an extra field":      {`{"code":"` + code + `","role":"host"}`, nil, 400},
		"two objects":         {body + body, nil, 400},
		"too large":           {`{"code":"` + strings.Repeat("A", 2048) + `"}`, nil, 400},
		"a token, not a code": {`{"code":"` + h.tokens["host"] + `"}`, nil, 403},
	} {
		mod := c.mod
		if mod == nil {
			mod = func(*http.Request) {}
		}
		got, v, _ := h.pair(c.body, mod)
		if got != c.code || v["tokens"] != nil {
			t.Errorf("%s: %d %v", name, got, v)
		}
	}
	// A GET is no way in.
	if code, _, _ := h.get("/api/host/pair", ""); code != 405 && code != 404 {
		t.Errorf("GET: %d", code)
	}
	// The right code, as typed (lower case, spaces): the token file's tokens, no-store, no CORS.
	got, v, hdr := h.pair(`{"code":"` + strings.ToLower(strings.ReplaceAll(code, "-", " ")) + `"}`)
	if got != 200 || hdr.Get("Cache-Control") != "no-store" || hdr.Get("Access-Control-Allow-Origin") != "" {
		t.Fatalf("pair: %d %v", got, hdr)
	}
	raw, _ := json.Marshal(v["tokens"])
	var paired []demoToken
	json.Unmarshal(raw, &paired)
	file, _ := os.ReadFile(filepath.Join(h.dir, "tokens.json"))
	var onDisk struct{ Tokens []demoToken }
	json.Unmarshal(file, &onDisk)
	if len(paired) == 0 || !slices.Equal(paired, onDisk.Tokens) {
		t.Fatalf("paired tokens %v, file %v", paired, onDisk.Tokens)
	}
	if got, v, _ := h.pair(body); got != 410 || v["error"] != "pairing_closed" || v["tokens"] != nil {
		t.Errorf("a second use: %d %v", got, v)
	}
	// The roles are the token file's: the operator reads no task, a viewer starts nothing.
	if code, _, _ := h.get("/api/tasks", h.tokens["operator"]); code != 403 {
		t.Errorf("operator read tasks: %d", code)
	}
	if code, _ := h.post("/api/host/runs", h.tokens["viewer:consumer-a"], startBody("req-pair-00000001", "ask")); code != 403 {
		t.Errorf("a viewer started a run: %d", code)
	}
	if n := strings.Count(term.String(), code); n != 1 {
		t.Errorf("the code shown %d times", n)
	}
	if strings.Contains(h.out.String(), code) {
		t.Error("the pairing code reached the host's log")
	}

	// Another host's code is not this host's.
	h2, _, code2 := pairingHost(t, nil)
	defer h2.stop()
	if got, _, _ := h2.pair(`{"code":"` + code + `"}`); got != 403 {
		t.Errorf("the first host's code on the second: %d", got)
	}
	if got, _, _ := h2.pair(`{"code":"` + code2 + `"}`); got != 200 {
		t.Errorf("the second host's own code: %d", got)
	}
}

// The window closes at 5 minutes of the host's clock and after five
// failures; concurrent uses of the right code: one success.
func TestPairingEndpointWindow(t *testing.T) {
	var at atomic.Int64
	base := time.Now()
	h, _, code := pairingHost(t, func() time.Time { return base.Add(time.Duration(at.Load())) })
	at.Store(int64(pairingTTL))
	if got, _, _ := h.pair(`{"code":"` + code + `"}`); got != 410 {
		t.Errorf("at 5 minutes: %d", got)
	}
	h.stop()

	h, _, code = pairingHost(t, nil)
	for i := 0; i < pairingMaxFailures; i++ {
		if got, _, _ := h.pair(`{"code":"AAAA-AAAA-AAAA"}`); got != 403 {
			t.Fatalf("failure %d: %d", i, got)
		}
	}
	if got, _, _ := h.pair(`{"code":"` + code + `"}`); got != 410 {
		t.Errorf("after five failures: %d", got)
	}
	h.stop()

	h, _, code = pairingHost(t, nil)
	defer h.stop()
	var ok atomic.Int32
	var wg sync.WaitGroup
	for i := 0; i < 20; i++ {
		wg.Go(func() {
			if got, _, _ := h.pair(`{"code":"` + code + `"}`); got == 200 {
				ok.Add(1)
			}
		})
	}
	wg.Wait()
	if ok.Load() != 1 {
		t.Errorf("%d concurrent successes", ok.Load())
	}
}

// Pairing exists only when asked for, on a host that starts runs; the
// viewer page and a host without it have no way to host credentials. The
// ready file names this process.
func TestPairingOffAndReadyFile(t *testing.T) {
	ready := filepath.Join(t.TempDir(), "ready.json")
	h := startHost(t, hostOpts{scenarios: "testdata/demo/scenarios.json", args: []string{"--registry", "testdata/demo/registry.json", "--ready-file", ready}})
	defer h.stop()
	if got, v, _ := h.pair(`{"code":"AAAA-AAAA-AAAA"}`); got != 404 && got != 405 || v["tokens"] != nil {
		t.Errorf("pairing without --pairing: %d %v", got, v)
	}
	raw, err := os.ReadFile(ready)
	var r struct {
		Addr string `json:"addr"`
		PID  int    `json:"pid"`
	}
	if err != nil || json.Unmarshal(raw, &r) != nil || r.Addr != h.addr || r.PID != os.Getpid() {
		t.Errorf("ready file %s %v", raw, err)
	}
	// The mode, without a token: the label only.
	if code, body, hdr := h.get("/api/mode", ""); code != 200 || strings.TrimSpace(string(body)) != `{"mode":"fake"}` || hdr.Get("Cache-Control") != "no-store" {
		t.Errorf("mode: %d %s", code, body)
	}
	if code, _, _ := h.get("/api/mode", "", func(r *http.Request) { r.Host = "localhost" }); code != 403 {
		t.Errorf("mode on another Host: %d", code)
	}
	_, body, _ := h.get("/", "")
	if strings.Contains(string(body), "/api/host/pair") {
		t.Error("the viewer page knows the pairing endpoint")
	}
	if _, _, raw := h.status(h.tokens["host"]); !strings.Contains(string(raw), `"build":{`) {
		t.Errorf("no build record in the status: %s", raw)
	}
	for name, c := range map[string]struct {
		args []string
		want string
	}{
		"with a plan":  {[]string{"--pairing", "--fake-cli", "/bin/true", "--plan", "testdata/demo/fake-plan.json"}, "--pairing is for a host that starts runs"},
		"history mode": {[]string{"--pairing", "--history-db", "x.db"}, "--history-db serves a recorded run only"},
		"no host runs": {[]string{"--pairing"}, "--pairing is for a host that starts runs"},
	} {
		ready := make(chan string, 1)
		done := make(chan error, 1)
		ctx, cancel := context.WithCancel(context.Background())
		go func() {
			done <- serve(ctx, c.args, deps{getenv: hostEnv(t.TempDir(), "127.0.0.1:0"), out: &lockedBuffer{}, clock: wallClock, pairOut: &lockedBuffer{}, ready: ready})
		}()
		select {
		case <-ready:
			t.Errorf("%s: served", name)
			cancel()
			<-done
		case err := <-done:
			if err == nil || !strings.Contains(err.Error(), c.want) {
				t.Errorf("%s: %v, want %q", name, err, c.want)
			}
		case <-time.After(30 * time.Second):
			t.Errorf("%s: neither served nor refused", name)
		}
		cancel()
	}
}
