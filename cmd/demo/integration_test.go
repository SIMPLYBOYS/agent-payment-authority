package main

// End-to-end tests of B1-06 delivery 1 (docs/demo-integration.md): one demo
// host with a real loopback HTTP 402 shop, a real SQLite Store, its expiry
// worker and the fake CLI as a real subprocess, read back through the
// authenticated API. Expected outcomes come from the accepted
// testdata/contracts/cases.json, read the way internal/store/cases_test.go
// maps them, never from the Policy under test.

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"sync/atomic"
	"syscall"
	"testing"
	"time"

	"github.com/SIMPLYBOYS/agent-payment-authority/internal/buyer"
	"github.com/SIMPLYBOYS/agent-payment-authority/internal/buyer/fakecli"
	"github.com/SIMPLYBOYS/agent-payment-authority/internal/store"
	"github.com/SIMPLYBOYS/agent-payment-authority/internal/x402"
)

// lockedBuffer is the host's log, read while the host writes it.
type lockedBuffer struct {
	mu sync.Mutex
	b  bytes.Buffer
}

func (l *lockedBuffer) Write(p []byte) (int, error) {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.b.Write(p)
}

func (l *lockedBuffer) String() string {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.b.String()
}

type hostOpts struct {
	dir         string    // reuse a previous host's database and files
	runs        []planRun // nil: no fake runs
	clock       store.Clock
	expiryEvery time.Duration
	limits      *buyer.Limits
	fakeEnv     []string
	fakeCLI     string       // "" means the test binary as fake-claude
	args        []string     // more serve flags, e.g. --registry
	stage       func(string) // the stop steps
}

type host struct {
	t      *testing.T
	dir    string
	addr   string
	out    *lockedBuffer
	st     *store.Store
	tokens map[string]string // "host", "viewer:consumer-a", ...
	runs   chan []runOutcome
	stop   func() error
}

var noProxy = &http.Client{Transport: &http.Transport{Proxy: nil}, Timeout: 10 * time.Second}

// fakeBinary is the test binary under the name fake-claude, which TestMain
// turns into the fake CLI.
func fakeBinary(t *testing.T) string {
	t.Helper()
	exe, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	p := filepath.Join(t.TempDir(), "fake-claude")
	if err := os.Symlink(exe, p); err != nil {
		t.Fatal(err)
	}
	return p
}

// scenarioFile writes a fake scenario of the given steps.
func scenarioFile(t *testing.T, steps ...map[string]any) string {
	t.Helper()
	raw, _ := json.Marshal(map[string]any{"steps": steps})
	p := filepath.Join(t.TempDir(), "scenario.json")
	os.WriteFile(p, raw, 0o600)
	return p
}

func tool(name string, args ...string) map[string]any {
	s := map[string]any{"do": "tool", "tool": name}
	if len(args) == 2 {
		s["args"] = map[string]string{args[0]: args[1]}
	}
	return s
}

// mainline is ask-mainline.json with a capability save and a marked closing
// claim of payment, which nothing may repeat.
const mark = "MARKDEMO5b1c"

func mainline(t *testing.T) string {
	return scenarioFile(t, map[string]any{"do": "savecap"}, tool("list_products"), tool("get_quote", "product_id", "kr-market"),
		tool("request_purchase", "product_id", "kr-market"), map[string]any{"do": "text", "text": "Payment complete " + mark + " $CAPABILITY"},
		map[string]any{"do": "result", "text": "Paid and downloaded " + mark})
}

func serveArgs(t *testing.T, o hostOpts, dir string) []string {
	args := append([]string{"--trace-dir", filepath.Join(dir, "runs")}, o.args...)
	if o.runs == nil {
		return args
	}
	raw, _ := json.Marshal(plan{Description: "test plan", Runs: o.runs})
	planPath := filepath.Join(dir, fmt.Sprintf("plan-%d.json", time.Now().UnixNano()))
	os.WriteFile(planPath, raw, 0o600)
	cli := o.fakeCLI
	if cli == "" {
		cli = fakeBinary(t)
	}
	return append(args, "--fake-cli", cli, "--plan", planPath)
}

func hostEnv(dir, addr string) func(string) string {
	return func(k string) string {
		switch k {
		case "APA_ADDR":
			return addr
		case "APA_DB_PATH":
			return filepath.Join(dir, "demo.db")
		case "APA_DEMO_TOKEN_FILE":
			return filepath.Join(dir, "tokens.json")
		case "PATH":
			return os.Getenv("PATH")
		}
		return ""
	}
}

func startHost(t *testing.T, o hostOpts) *host {
	t.Helper()
	dir := o.dir
	if dir == "" {
		var err error
		if dir, err = os.MkdirTemp(testRoot, "host-"); err != nil {
			t.Fatal(err)
		}
	}
	exe, _ := os.Executable()
	clock := o.clock
	if clock == nil {
		clock = wallClock
	}
	h := &host{t: t, dir: dir, out: &lockedBuffer{}, runs: make(chan []runOutcome, 1)}
	ready := make(chan string, 1)
	done := make(chan error, 1)
	ctx, cancel := context.WithCancel(context.Background())
	d := deps{getenv: hostEnv(dir, "127.0.0.1:0"), self: exe, out: h.out, clock: clock, ready: ready, expiryEvery: o.expiryEvery,
		limits: o.limits, fakeEnv: o.fakeEnv, onStore: func(s *store.Store) { h.st = s }, runsDone: h.runs, stage: o.stage}
	args := serveArgs(t, o, dir)
	go func() { done <- serve(ctx, args, d) }()
	h.stop = func() error {
		cancel()
		select {
		case err := <-done:
			return err
		case <-time.After(30 * time.Second):
			t.Fatal("the host did not stop")
			return nil
		}
	}
	select {
	case h.addr = <-ready:
	case err := <-done:
		cancel()
		t.Fatalf("the host stopped before serving: %v", err)
	case <-time.After(30 * time.Second):
		cancel()
		t.Fatal("the host never became ready")
	}
	raw, err := os.ReadFile(filepath.Join(dir, "tokens.json"))
	if err != nil {
		t.Fatal(err)
	}
	var file struct{ Tokens []demoToken }
	json.Unmarshal(raw, &file)
	h.tokens = map[string]string{}
	for _, tok := range file.Tokens {
		key := tok.Role
		if tok.ConsumerID != "" {
			key += ":" + tok.ConsumerID
		}
		h.tokens[key] = tok.Token
	}
	return h
}

// waitRuns waits for the plan's runs to end.
func (h *host) waitRuns() []runOutcome {
	h.t.Helper()
	select {
	case out := <-h.runs:
		return out
	case <-time.After(60 * time.Second):
		h.t.Fatal("the fake runs did not end")
		return nil
	}
}

// get sends GET path as token (none if ""), with mod applied to the request.
func (h *host) get(path, token string, mod ...func(*http.Request)) (int, []byte, http.Header) {
	h.t.Helper()
	req, _ := http.NewRequest(http.MethodGet, "http://"+h.addr+path, nil)
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	for _, m := range mod {
		m(req)
	}
	resp, err := noProxy.Do(req)
	if err != nil {
		h.t.Fatal(err)
	}
	defer resp.Body.Close()
	var b bytes.Buffer
	b.ReadFrom(resp.Body)
	return resp.StatusCode, b.Bytes(), resp.Header
}

func (h *host) task(id string) taskReply {
	h.t.Helper()
	code, body, _ := h.get("/api/tasks/"+id, h.tokens["host"])
	var v taskReply
	if code != http.StatusOK || json.Unmarshal(body, &v) != nil {
		h.t.Fatalf("task %s: %d %s", id, code, body)
	}
	return v
}

func (h *host) events(id string) eventsReply {
	h.t.Helper()
	code, body, _ := h.get("/api/tasks/"+id+"/events", h.tokens["host"])
	var v eventsReply
	if code != http.StatusOK || json.Unmarshal(body, &v) != nil {
		h.t.Fatalf("events %s: %d %s", id, code, body)
	}
	return v
}

// waitFor polls the task until ok holds.
func (h *host) waitFor(id string, ok func(taskReply) bool) taskReply {
	h.t.Helper()
	for end := time.Now().Add(30 * time.Second); time.Now().Before(end); time.Sleep(50 * time.Millisecond) {
		if code, body, _ := h.get("/api/tasks/"+id, h.tokens["host"]); code == http.StatusOK {
			var v taskReply
			if json.Unmarshal(body, &v) == nil && ok(v) {
				return v
			}
		}
	}
	h.t.Fatalf("task %s never reached the expected state", id)
	return taskReply{}
}

// expected reads one accepted case's expected outcome.
func expected(t *testing.T, id string) (result, reason, approval, payment string, forbidden []string) {
	t.Helper()
	raw, err := os.ReadFile("testdata/contracts/cases.json")
	if err != nil {
		t.Fatal(err)
	}
	var file struct {
		Cases []struct {
			CaseID   string `json:"case_id"`
			Expected struct {
				Result, Reason  string
				ApprovalStatus  string   `json:"approval_status"`
				PaymentStatus   string   `json:"payment_status"`
				ForbiddenEvents []string `json:"forbidden_events"`
			} `json:"expected"`
		} `json:"cases"`
	}
	json.Unmarshal(raw, &file)
	for _, c := range file.Cases {
		if c.CaseID == id {
			x := c.Expected
			return x.Result, x.Reason, x.ApprovalStatus, x.PaymentStatus, x.ForbiddenEvents
		}
	}
	t.Fatalf("no case %s", id)
	return
}

// outcome maps a purchase the way internal/store/cases_test.go does.
func outcome(p store.PurchaseView) (result, reason, approval string) {
	switch p.ApprovalClosed {
	case "expired":
		return "denied", "APPROVAL_TIMEOUT", "expired"
	case "withdrawn":
		return "cancelled", "HOST_CANCELLED", "withdrawn"
	}
	if p.Decision.PolicyVerdict == "Ask" {
		return "pending", p.Decision.Reason, p.Decision.ApprovalStatus
	}
	return p.Decision.PolicyVerdict, p.Decision.Reason, p.Decision.ApprovalStatus
}

func eventTypes(e eventsReply) []string {
	var out []string
	for _, v := range e.Events {
		out = append(out, v.EventType)
	}
	return out
}

// quote is the shop's own 402 for its one product, read over HTTP: the
// independent source of what a purchase must name.
func quote(t *testing.T, addr string) (url, amount string) {
	t.Helper()
	resp, err := noProxy.Get("http://" + addr + "/catalog")
	if err != nil {
		t.Fatal(err)
	}
	var catalog struct {
		Products []struct {
			ResourceURL string `json:"resource_url"`
		} `json:"products"`
	}
	json.NewDecoder(resp.Body).Decode(&catalog)
	resp.Body.Close()
	url = catalog.Products[0].ResourceURL
	if resp, err = noProxy.Get(url); err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	var pr x402.PaymentRequired
	if resp.StatusCode != http.StatusPaymentRequired || x402.Decode(resp.Header.Get(x402.HeaderPaymentRequired), &pr) != nil || len(pr.Accepts) != 1 {
		t.Fatalf("402: %d", resp.StatusCode)
	}
	var req struct {
		Amount string `json:"amount"`
	}
	json.Unmarshal(pr.Accepts[0], &req)
	return url, req.Amount
}

// The main line: A's main product Ask and B's budget Deny, each through the
// real 402, the Store and back out of the authenticated API, with the
// identity, read-only and projection checks on the same host.
func TestIntegrationAskDenyAndAccess(t *testing.T) {
	capFile := filepath.Join(t.TempDir(), "cap")
	h := startHost(t, hostOpts{runs: []planRun{
		{TaskID: "task-a", Scenario: mainline(t)},
		{TaskID: "task-b", Scenario: mainline(t), BudgetInitial: "250000"},
	}, expiryEvery: time.Hour, fakeEnv: []string{fakecli.EnvCapFile + "=" + capFile}})
	defer func() {
		if err := h.stop(); err != nil {
			t.Errorf("stop: %v", err)
		}
	}()
	for _, o := range h.waitRuns() {
		if o.Skipped != "" || !o.TaskStarted || o.Outcome != "ok" {
			t.Fatalf("run %+v", o)
		}
	}
	url, amount := quote(t, h.addr)

	t.Run("ask and deny", func(t *testing.T) {
		for _, c := range []struct{ task, caseID, consumer, wallet string }{
			{"task-a", "ask", "consumer-a", "wallet-a"}, {"task-b", "deny-budget", "consumer-b", "wallet-b"},
		} {
			v, e := h.task(c.task), h.events(c.task)
			wantResult, wantReason, wantApproval, wantPayment, forbidden := expected(t, c.caseID)
			if v.ConsumerID != c.consumer || v.WalletID != c.wallet || v.WalletEvidenceMode != "fixture" || v.PaymentCapability != "unavailable" || len(v.Purchases) != 1 {
				t.Fatalf("%s: %+v", c.task, v)
			}
			p := v.Purchases[0]
			if p.ResourceURL != url || string(p.Amount) != amount || p.HTTPMethod != "GET" {
				t.Errorf("%s: purchase %s %s, the shop's 402 says %s %s", c.task, p.ResourceURL, p.Amount, url, amount)
			}
			if wantResult == "Ask" {
				wantResult = "pending" // the store's mapping of an open Ask
			}
			if r, re, a := outcome(p); r != wantResult || re != wantReason || a != wantApproval || p.Decision.PaymentStatus != wantPayment {
				t.Errorf("%s: %s/%s/%s/%s, case %s wants %s/%s/%s/%s", c.task, r, re, a, p.Decision.PaymentStatus, c.caseID, wantResult, wantReason, wantApproval, wantPayment)
			}
			types := eventTypes(e)
			if !slices.Equal(types, []string{"TASK_STARTED", "PURCHASE_PROPOSED", "POLICY_DECIDED"}) || e.AsOfSequence != v.AsOfSequence {
				t.Errorf("%s: events %v as of %d, task as of %d", c.task, types, e.AsOfSequence, v.AsOfSequence)
			}
			for _, f := range forbidden {
				if slices.Contains(types, f) {
					t.Errorf("%s: forbidden event %s", c.task, f)
				}
			}
			d := e.Events[2]
			if d.Decision == nil || *d.Decision != p.Decision || d.IntentHash != p.IntentHash || d.PurchaseOrderID != p.PurchaseOrderID ||
				d.EvidenceMode == "" || d.Source == "" || d.EventID == "" || e.Events[1].IntentHash != p.IntentHash {
				t.Errorf("%s: decision event %+v, purchase %+v", c.task, d, p)
			}
		}
		if b := h.task("task-b"); b.Status != "denied" || b.DenyReason != "INSUFFICIENT_BUDGET" {
			t.Errorf("task-b %s %s", b.Status, b.DenyReason)
		}
		if a := h.task("task-a"); a.Status != "awaiting_approval" || a.Purchases[0].DeadlineAt == 0 || a.Purchases[0].ApprovalClosed != "" {
			t.Errorf("task-a %+v", a)
		}
	})

	t.Run("identity", func(t *testing.T) {
		viewerA, viewerB, hostTok := h.tokens["viewer:consumer-a"], h.tokens["viewer:consumer-b"], h.tokens["host"]
		capability, _ := os.ReadFile(capFile)
		if viewerA == "" || viewerB == "" || hostTok == "" || len(capability) != 64 {
			t.Fatalf("setup: tokens %v, capability %d bytes", len(h.tokens), len(capability))
		}
		_, notFound, _ := h.get("/api/tasks/task-zzz", hostTok)
		for name, c := range map[string]struct {
			path, token string
			mod         func(*http.Request)
			code        int
		}{
			"no token":             {"/api/tasks/task-a", "", nil, 401},
			"wrong token":          {"/api/tasks/task-a", strings.Repeat("0", 64), nil, 401},
			"token in the query":   {"/api/tasks/task-a?token=" + hostTok, "", nil, 401},
			"bridge capability":    {"/api/tasks/task-a", string(capability), nil, 401},
			"basic scheme":         {"/api/tasks/task-a", "", func(r *http.Request) { r.Header.Set("Authorization", "Basic "+hostTok) }, 401},
			"viewer A sees A":      {"/api/tasks/task-a", viewerA, nil, 200},
			"viewer A, B's task":   {"/api/tasks/task-b", viewerA, nil, 404},
			"viewer A, B events":   {"/api/tasks/task-b/events", viewerA, nil, 404},
			"viewer B, A's task":   {"/api/tasks/task-a", viewerB, nil, 404},
			"viewer A claims B":    {"/api/tasks/task-b?consumer_id=consumer-b&wallet_id=wallet-b&role=host", viewerA, func(r *http.Request) { r.Header.Set("X-Consumer-ID", "consumer-b"); r.Header.Set("X-Role", "host") }, 404},
			"viewer A not started": {"/api/tasks/task-a-next", viewerA, nil, 404},
			"host sees A":          {"/api/tasks/task-a/events", hostTok, nil, 200},
			"host sees B":          {"/api/tasks/task-b", hostTok, nil, 200},
			"other Host header":    {"/api/tasks/task-a", hostTok, func(r *http.Request) { r.Host = "localhost" + h.addr[strings.LastIndex(h.addr, ":"):] }, 403},
			"other Origin":         {"/api/tasks/task-a", hostTok, func(r *http.Request) { r.Header.Set("Origin", "http://evil.example") }, 403},
			"own Origin":           {"/api/tasks/task-a", hostTok, func(r *http.Request) { r.Header.Set("Origin", "http://"+h.addr) }, 200},
		} {
			var mods []func(*http.Request)
			if c.mod != nil {
				mods = append(mods, c.mod)
			}
			code, body, hdr := h.get(c.path, c.token, mods...)
			if code != c.code || hdr.Get("Access-Control-Allow-Origin") != "" || hdr.Get("Cache-Control") != "no-store" {
				t.Errorf("%s: %d %s, want %d", name, code, body, c.code)
			}
			if code == 404 && !bytes.Equal(body, notFound) {
				t.Errorf("%s: 404 body %s differs from an unknown task's %s", name, body, notFound)
			}
			if code != 200 && (bytes.Contains(body, []byte("consumer")) || bytes.Contains(body, []byte("task-"))) {
				t.Errorf("%s: refusal names an owner or task: %s", name, body)
			}
		}
		req, _ := http.NewRequest(http.MethodPost, "http://"+h.addr+"/api/tasks/task-a", nil)
		req.Header.Set("Authorization", "Bearer "+hostTok)
		if resp, err := noProxy.Do(req); err != nil || resp.StatusCode != http.StatusMethodNotAllowed {
			t.Errorf("POST: %v %v", resp, err)
		} else {
			resp.Body.Close()
		}
	})

	t.Run("read only", func(t *testing.T) {
		before := map[string]eventsReply{"task-a": h.events("task-a"), "task-b": h.events("task-b")}
		for range 10 {
			h.get("/api/tasks/task-a", h.tokens["viewer:consumer-b"])
			h.get("/api/tasks/task-b", "")
			h.get("/api/tasks/task-a/events", h.tokens["viewer:consumer-a"])
			h.get("/api/tasks/task-b", h.tokens["host"])
		}
		for id, b := range before {
			if a := h.events(id); a.AsOfSequence != b.AsOfSequence || len(a.Events) != len(b.Events) {
				t.Errorf("%s: GETs changed the events: %d -> %d", id, b.AsOfSequence, a.AsOfSequence)
			}
		}
	})

	t.Run("projection", func(t *testing.T) {
		capability, _ := os.ReadFile(capFile)
		var all []byte
		for _, p := range []string{"/api/tasks/task-a", "/api/tasks/task-a/events", "/api/tasks/task-b", "/api/tasks/task-b/events"} {
			_, body, _ := h.get(p, h.tokens["host"])
			all = append(all, body...)
		}
		log := h.out.String()
		for _, secret := range append([]string{mark, string(capability), "Payment complete", "downloaded"}, h.tokens["host"], h.tokens["viewer:consumer-a"], h.tokens["viewer:consumer-b"]) {
			if bytes.Contains(all, []byte(secret)) || strings.Contains(log, secret) {
				t.Errorf("%q reached the API or the log", secret)
			}
		}
		for _, banned := range []string{"Approved", "SETTLED", "RESOURCE_DELIVERED", "GRANT_ISSUED", "tx_hash", "\"extra\"", "\"extensions\"", "prompt"} {
			if bytes.Contains(all, []byte(banned)) {
				t.Errorf("%q in the API output", banned)
			}
		}
		fi, err := os.Stat(filepath.Join(h.dir, "tokens.json"))
		if err != nil || fi.Mode().Perm() != 0o600 {
			t.Errorf("token file: %v %v", fi.Mode(), err)
		}
	})
}

// A GET never expires anything: an Ask past its deadline reads as still
// open until the worker records the expiry.
func TestIntegrationExpiryOnlyByWorker(t *testing.T) {
	var now atomic.Uint64
	now.Store(wallClock())
	h := startHost(t, hostOpts{runs: []planRun{{TaskID: "task-a", Scenario: mainline(t)}}, clock: now.Load, expiryEvery: time.Hour})
	defer h.stop()
	h.waitRuns()
	p := h.task("task-a").Purchases[0]
	now.Store(p.DeadlineAt + 1)
	for range 5 {
		v := h.task("task-a")
		if v.Purchases[0].ApprovalClosed != "" || v.Status != "awaiting_approval" || v.ServedAt <= v.Purchases[0].DeadlineAt ||
			slices.Contains(eventTypes(h.events("task-a")), "APPROVAL_EXPIRED") {
			t.Fatalf("a GET recorded or claimed the expiry: %+v", v)
		}
	}
	if err := h.st.ExpireDue(); err != nil { // what the worker does on its tick
		t.Fatal(err)
	}
	v := h.task("task-a")
	wantResult, wantReason, wantApproval, _, _ := expected(t, "approval-deadline-exact")
	if r, re, a := outcome(v.Purchases[0]); r != wantResult || re != wantReason || a != wantApproval ||
		v.Purchases[0].Decision.PolicyVerdict != "Ask" || v.Purchases[0].Decision.Reason != "HUMAN_APPROVAL_REQUIRED" {
		t.Errorf("after expiry: %s/%s/%s, decision %+v", r, re, a, v.Purchases[0].Decision)
	}
	if types := eventTypes(h.events("task-a")); types[len(types)-1] != "APPROVAL_EXPIRED" {
		t.Errorf("events %v", types)
	}
}

func alive(pid int) bool { return syscall.Kill(pid, 0) == nil }

// Cancel and CLI timeout record their end without rewriting a decision;
// shutdown reaps the fake's process group; a restart serves the same
// records, skips tasks already recorded, and a new task keeps the
// consumer's wallet with one purchase for a repeated request.
func TestIntegrationEndsRestartAndReplay(t *testing.T) {
	pidFile := filepath.Join(t.TempDir(), "pids")
	hang := scenarioFile(t, map[string]any{"do": "holder"}, tool("request_purchase", "product_id", "kr-market"), map[string]any{"do": "hang"})
	lim := buyer.DefaultLimits
	lim.Total = 3 * time.Second
	h := startHost(t, hostOpts{runs: []planRun{
		{TaskID: "task-b", Scenario: hang, BudgetInitial: "250000"}, // Deny, then the CLI time limit
		{TaskID: "task-a", Scenario: hang},                          // Ask, then the host stops
	}, limits: &lim, fakeEnv: []string{fakecli.EnvPIDFile + "=" + pidFile}})
	h.waitFor("task-a", func(v taskReply) bool { return len(v.Purchases) == 1 })
	if err := h.stop(); err != nil {
		t.Fatal(err)
	}
	pids := strings.Fields(func() string { b, _ := os.ReadFile(pidFile); return string(b) }())
	if len(pids) != 2 {
		t.Fatalf("holder pids %v", pids)
	}
	for _, p := range pids {
		var pid int
		fmt.Sscan(p, &pid)
		for end := time.Now().Add(3 * time.Second); alive(pid) && time.Now().Before(end); time.Sleep(20 * time.Millisecond) {
		}
		if alive(pid) {
			t.Errorf("fake grandchild %d survived the host", pid)
		}
	}

	// Restart on the same database, serve only.
	h = startHost(t, hostOpts{dir: h.dir})
	b, be := h.task("task-b"), h.events("task-b")
	_, _, _, _, _ = expected(t, "cli-timeout") // the code below is that case's lifecycle code
	if b.Status != "denied" || b.DenyReason != "INSUFFICIENT_BUDGET" || b.Purchases[0].Decision.Reason != "INSUFFICIENT_BUDGET" ||
		be.Events[len(be.Events)-1].EventType != "TASK_TIMED_OUT" || be.Events[len(be.Events)-1].Code != "CLI_TIMEOUT" {
		t.Errorf("task-b after the time limit: %+v %v", b, eventTypes(be))
	}
	a, ae := h.task("task-a"), h.events("task-a")
	wantResult, wantReason, wantApproval, _, _ := expected(t, "cancel-pending")
	if r, re, ap := outcome(a.Purchases[0]); r != wantResult || re != wantReason || ap != wantApproval || a.Status != "cancelled" ||
		a.Purchases[0].Decision.PolicyVerdict != "Ask" || ae.Events[len(ae.Events)-1].Code != "HOST_CANCELLED" {
		t.Errorf("task-a after the stop: %s/%s/%s %+v %v", r, re, ap, a, eventTypes(ae))
	}
	if err := h.stop(); err != nil {
		t.Fatal(err)
	}

	// Restart with a plan naming a recorded task and a new one for the same consumer.
	retry := scenarioFile(t, tool("request_purchase", "product_id", "kr-market"), tool("request_purchase", "product_id", "kr-market"),
		map[string]any{"do": "result", "text": "requested"})
	h = startHost(t, hostOpts{dir: h.dir, runs: []planRun{{TaskID: "task-a", Scenario: retry}, {TaskID: "task-a-next", Scenario: retry}}})
	defer h.stop()
	out := h.waitRuns()
	if out[0].Skipped != "task_already_recorded" || out[1].Skipped != "" || !out[1].TaskStarted {
		t.Fatalf("runs %+v", out)
	}
	if a2 := h.task("task-a"); len(a2.Purchases) != 1 || a2.AsOfSequence != a.AsOfSequence {
		t.Errorf("the recorded task changed: %+v", a2)
	}
	n, ne := h.task("task-a-next"), h.events("task-a-next")
	decided := 0
	for _, e := range ne.Events {
		if e.EventType == "POLICY_DECIDED" {
			decided++
		}
	}
	if n.WalletID != "wallet-a" || n.ConsumerID != "consumer-a" || len(n.Purchases) != 1 || decided != 1 || n.Purchases[0].Decision.PolicyVerdict != "Ask" {
		t.Errorf("task-a-next: %+v, %d decisions", n, decided)
	}
}

// Nothing is served when the start fails: a damaged database, a CLI that
// is not the fake, a database in use, a bad plan.
func TestIntegrationStartRefusals(t *testing.T) {
	freePort := func() string {
		ln, err := net.Listen("tcp", "127.0.0.1:0")
		if err != nil {
			t.Fatal(err)
		}
		defer ln.Close()
		return ln.Addr().String()
	}
	notServing := func(addr string) {
		if c, err := net.DialTimeout("tcp", addr, time.Second); err == nil {
			c.Close()
			t.Errorf("%s still accepts connections", addr)
		}
	}
	try := func(dir string, args ...string) (string, error) {
		addr := freePort()
		ready := make(chan string, 1)
		exe, _ := os.Executable()
		// A start that wrongly succeeds serves until this ends, then fails below.
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		err := serve(ctx, append([]string{"--trace-dir", filepath.Join(dir, "runs")}, args...),
			deps{getenv: hostEnv(dir, addr), self: exe, out: &lockedBuffer{}, clock: wallClock, ready: ready})
		if len(ready) != 0 {
			t.Errorf("declared ready before failing: %v", err)
		}
		notServing(addr)
		return addr, err
	}
	newDir := func() string { d, _ := os.MkdirTemp(testRoot, "refuse-"); return d }

	dir := newDir()
	os.WriteFile(filepath.Join(dir, "demo.db"), []byte("not a database, and not to be repaired"), 0o600)
	if _, err := try(dir); err == nil || !strings.Contains(err.Error(), "store") {
		t.Errorf("damaged database: %v", err)
	}
	if raw, _ := os.ReadFile(filepath.Join(dir, "demo.db")); string(raw) != "not a database, and not to be repaired" {
		t.Error("the damaged database was changed")
	}
	if _, err := os.Stat(filepath.Join(dir, "tokens.json")); err == nil {
		t.Error("tokens were issued for a host that never served")
	}

	planFile := filepath.Join(t.TempDir(), "plan.json")
	os.WriteFile(planFile, []byte(`{"runs":[{"task_id":"task-a","scenario":"testdata/cli/ask-mainline.json"}]}`), 0o600)
	notFake := filepath.Join(t.TempDir(), "claude")
	ran := filepath.Join(t.TempDir(), "ran")
	os.WriteFile(notFake, []byte("#!/bin/sh\necho \"$@\" >> "+ran+"\necho '2.1.289 (Claude Code)'\n"), 0o755)
	if _, err := try(newDir(), "--fake-cli", notFake, "--plan", planFile); err == nil || !strings.Contains(err.Error(), "not the fake CLI") {
		t.Errorf("not the fake: %v", err)
	}
	if raw, _ := os.ReadFile(ran); strings.TrimSpace(string(raw)) != "--version" {
		t.Errorf("the non-fake was run with %q", raw)
	}

	for name, body := range map[string]string{
		"unknown task":     `{"runs":[{"task_id":"task-z","scenario":"testdata/cli/ask-mainline.json"}]}`,
		"repeated task":    `{"runs":[{"task_id":"task-a","scenario":"testdata/cli/ask-mainline.json"},{"task_id":"task-a","scenario":"testdata/cli/ask-mainline.json"}]}`,
		"unknown field":    `{"runs":[{"task_id":"task-a","scenario":"testdata/cli/ask-mainline.json","policy":"allow"}]}`,
		"bad budget":       `{"runs":[{"task_id":"task-a","scenario":"testdata/cli/ask-mainline.json","budget_initial":"-1"}]}`,
		"missing scenario": `{"runs":[{"task_id":"task-a","scenario":"testdata/cli/none.json"}]}`,
	} {
		os.WriteFile(planFile, []byte(body), 0o600)
		if _, err := try(newDir(), "--fake-cli", fakeBinary(t), "--plan", planFile); err == nil || !strings.Contains(err.Error(), "plan") {
			t.Errorf("%s: %v", name, err)
		}
	}

	h := startHost(t, hostOpts{})
	if _, err := try(h.dir); err == nil || !strings.Contains(err.Error(), "in use by another demo process") {
		t.Errorf("second host on one database: %v", err)
	}
	if code, _, _ := h.get("/api/tasks/task-a", h.tokens["host"]); code != http.StatusNotFound {
		t.Errorf("the first host was disturbed: %d", code)
	}
	h.stop()
}
