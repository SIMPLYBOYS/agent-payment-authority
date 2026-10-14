package main

// Tests of P1-a (docs/commercial-p1a.md, docs/commercial-transaction-flow.md
// sections 7 and 9): the fake-only simulated approver, a delegation before
// the run, the apa.pay.v1 Ask and its approval session, approve and deny,
// resends, the permission and consumer boundaries, expiry, cancel and an
// intent the approver did not see. Fake CLI only; no key, no payment.

import (
	"database/sql"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/SIMPLYBOYS/agent-payment-authority/internal/contracts"
	"github.com/SIMPLYBOYS/agent-payment-authority/internal/policy"
)

// payCatalog: an apa.pay.v1 entry per consumer (the main line asks: 0.50 is
// above allow_max 0.10) and an M1 one for A.
func payCatalog(t *testing.T, deadline int) string {
	t.Helper()
	d := map[string]any{"purpose": "買一份韓國市場簡報", "task_budget": "5000000", "allow_max": "100000", "approval_deadline_seconds": deadline, "valid_seconds": 1800}
	raw, _ := json.Marshal(map[string]any{"description": "test", "scenarios": []map[string]any{
		{"id": "pay-a", "consumer_id": "consumer-a", "contract": "apa.pay.v1", "scenario": mainline(t), "label": "pay A", "delegation": d},
		{"id": "pay-b", "consumer_id": "consumer-b", "contract": "apa.pay.v1", "scenario": mainline(t), "label": "pay B", "delegation": d},
		{"id": "ask", "consumer_id": "consumer-a", "scenario": mainline(t), "label": "M1 Ask"},
	}})
	p := filepath.Join(t.TempDir(), "scenarios.json")
	os.WriteFile(p, raw, 0o600)
	return p
}

// simulate makes the operator act as consumer; it returns the capability.
func (h *host) simulate(consumer string) string {
	h.t.Helper()
	code, body := h.post("/api/host/simulate", h.tokens["operator"], `{"consumer_id":"`+consumer+`"}`)
	if code != 200 || body["consumer_id"] != consumer {
		h.t.Fatalf("simulate %s: %d %v", consumer, code, body)
	}
	return body["token"].(string)
}

// delegateAndRun confirms the scenario's delegation as sim and starts the
// run with it; it waits for the run to end and returns the run.
func (h *host) delegateAndRun(sim, scenario, n string) map[string]any {
	h.t.Helper()
	return h.delegateAndRunAs(sim, scenario, n, "apa.pay.v1")
}

// delegateAndRunAs is delegateAndRun for a scenario of version.
func (h *host) delegateAndRunAs(sim, scenario, n, version string) map[string]any {
	h.t.Helper()
	code, d := h.post("/api/sim/delegations", sim, `{"request_id":"dlg-req-000000000`+n+`","scenario":"`+scenario+`"}`)
	if code != 201 {
		h.t.Fatalf("confirm delegation: %d %v", code, d)
	}
	id := d["delegation"].(map[string]any)["delegation_id"].(string)
	code, r := h.post("/api/host/runs", h.tokens["operator"], `{"request_id":"run-req-000000000`+n+`","scenario":"`+scenario+`","delegation_id":"`+id+`"}`)
	if code != 202 || r["contract_version"] != version || r["delegation_id"] != id {
		h.t.Fatalf("start with delegation: %d %v", code, r)
	}
	return h.waitRun(r["run_id"].(string), ended)
}

func (h *host) approvals(sim string) []map[string]any {
	h.t.Helper()
	code, raw, _ := h.get("/api/sim/approvals", sim)
	var v struct {
		Approvals []map[string]any `json:"approvals"`
	}
	if code != 200 || json.Unmarshal(raw, &v) != nil {
		h.t.Fatalf("approvals: %d %s", code, raw)
	}
	return v.Approvals
}

// pending is the one pending session of the list: its ID and intent hash.
func pending(t *testing.T, list []map[string]any) (string, string) {
	t.Helper()
	for _, a := range list {
		p := a["purchase"].(map[string]any)
		if ap := p["approval"].(map[string]any); ap["state"] == "pending" {
			return ap["approval_session_id"].(string), p["intent_hash"].(string)
		}
	}
	t.Fatalf("no pending session in %v", list)
	return "", ""
}

func resolveBody(req, intent, decision string) string {
	return `{"request_id":"` + req + `","intent_hash":"` + intent + `","decision":"` + decision + `"}`
}

func (h *host) grants() int {
	h.t.Helper()
	db, err := sql.Open("sqlite", filepath.Join(h.dir, "demo.db")+"?mode=ro")
	if err != nil {
		h.t.Fatal(err)
	}
	defer db.Close()
	var n int
	if err := db.QueryRow(`SELECT count(*) FROM grants`).Scan(&n); err != nil {
		h.t.Fatal(err)
	}
	return n
}

func TestP1aMainLine(t *testing.T) {
	h := controlHost(t, hostOpts{expiryEvery: time.Hour, scenarios: payCatalog(t, 60)})
	sim := h.simulate("consumer-a")
	run := h.delegateAndRun(sim, "pay-a", "01")
	task := run["task_id"].(string)
	id, intent := pending(t, h.approvals(sim))

	code, res := h.post("/api/sim/approvals/"+id, sim, resolveBody("approve-req-00000001", intent, "approve"))
	r, _ := res["result"].(map[string]any)
	if code != 200 || r["outcome"] != "approved" || res["signer_stub"] != "GRANT_METHOD_NOT_ACCEPTED" || res["payment_capability"] != "not_enabled" {
		t.Fatalf("approve: %d %v", code, res)
	}
	g := r["grant"].(map[string]any)
	if g["authorization_method"] != "simulated" || g["audience"] != "signer" || g["intent_hash"] != intent || !strings.HasPrefix(g["approver_id"].(string), "simulated:consumer-a:host-") {
		t.Fatalf("grant: %v", g)
	}
	// The same request again, and again after a restart: the same answer, still one grant.
	code, again := h.post("/api/sim/approvals/"+id, sim, resolveBody("approve-req-00000001", intent, "approve"))
	if a := again["result"].(map[string]any); code != 200 || a["replay"] != true || a["grant"].(map[string]any)["jti"] != g["jti"] || h.grants() != 1 {
		t.Fatalf("resend: %d %v (grants %d)", code, again, h.grants())
	}
	// A second request ID for the decided session: closed, no second grant.
	if code, other := h.post("/api/sim/approvals/"+id, sim, resolveBody("approve-req-00000002", intent, "approve")); code != 409 || other["error"] != "approval_closed" || h.grants() != 1 {
		t.Fatalf("another request: %d %v", code, other)
	}
	if err := h.stop(); err != nil {
		t.Fatal(err)
	}
	h = controlHost(t, hostOpts{dir: h.dir, expiryEvery: time.Hour, scenarios: payCatalog(t, 60)})
	defer h.stop()
	sim = h.simulate("consumer-a")
	if code, after := h.post("/api/sim/approvals/"+id, sim, resolveBody("approve-req-00000001", intent, "approve")); code != 200 || after["result"].(map[string]any)["replay"] != true || h.grants() != 1 {
		t.Fatalf("resend after restart: %d %v", code, after)
	}
	// The task as the viewer reads it: apa.pay.v1, not_enabled, approved with the grant.
	code, raw, _ := h.get("/api/tasks/"+task, h.tokens["viewer:consumer-a"])
	var tv taskReply
	json.Unmarshal(raw, &tv)
	if code != 200 || tv.ContractVersion != "apa.pay.v1" || tv.PaymentCapability != "not_enabled" || tv.DelegationID == "" || len(tv.Purchases) != 1 ||
		tv.Purchases[0].Approval == nil || tv.Purchases[0].Approval.State != "approved" || tv.Purchases[0].Approval.Grant == nil || tv.Purchases[0].Decision.PaymentStatus != "not_enabled" {
		t.Fatalf("task: %d %s", code, raw)
	}
	code, raw, _ = h.get("/api/tasks/"+task+"/events", h.tokens["viewer:consumer-a"])
	var types []string
	var ev eventsReply
	json.Unmarshal(raw, &ev)
	for _, e := range ev.Events {
		types = append(types, e.EventType)
	}
	if want := "TASK_STARTED,DELEGATION_BOUND,PURCHASE_PROPOSED,POLICY_DECIDED,APPROVAL_SESSION_OPENED,APPROVED,GRANT_ISSUED"; code != 200 || strings.Join(types, ",") != want {
		t.Fatalf("events %v, want %s", types, want)
	}
}

func TestP1aPermissions(t *testing.T) {
	h := controlHost(t, hostOpts{expiryEvery: time.Hour, scenarios: payCatalog(t, 60)})
	defer h.stop()
	simA := h.simulate("consumer-a")
	h.delegateAndRun(simA, "pay-a", "01")
	id, intent := pending(t, h.approvals(simA))
	body := resolveBody("perm-req-0000000001", intent, "approve")
	// viewer, host and operator never approve; no token is unauthenticated.
	for name, tok := range map[string]string{"viewer": h.tokens["viewer:consumer-a"], "host": h.tokens["host"], "operator": h.tokens["operator"]} {
		if code, b := h.post("/api/sim/approvals/"+id, tok, body); code != 403 || b["error"] != "forbidden_role" {
			t.Errorf("%s approves: %d %v", name, code, b)
		}
		if code, _, _ := h.get("/api/sim/approvals", tok); code != 403 {
			t.Errorf("%s lists approvals: %d", name, code)
		}
	}
	if code, _ := h.post("/api/sim/approvals/"+id, "", body); code != 401 {
		t.Errorf("no token: %d", code)
	}
	// The simulation itself is the operator's, same origin.
	if code, _ := h.post("/api/host/simulate", h.tokens["host"], `{"consumer_id":"consumer-a"}`); code != 403 {
		t.Errorf("host simulates: %d", code)
	}
	if code, _ := h.post("/api/host/simulate", h.tokens["operator"], `{"consumer_id":"consumer-z"}`); code != 400 {
		t.Errorf("an unknown consumer: %d", code)
	}
	if code, b := h.post("/api/sim/approvals/"+id, simA, body, func(r *http.Request) { r.Header.Set("Origin", "http://evil.example") }); code != 403 || b["error"] != "forbidden_origin" {
		t.Errorf("cross origin: %d %v", code, b)
	}
	// B: acting as B replaces A; B neither sees nor decides A's session.
	simB := h.simulate("consumer-b")
	if code, _, _ := h.get("/api/sim/approvals", simA); code != 401 {
		t.Errorf("A's capability after B was simulated: %d", code)
	}
	if list := h.approvals(simB); len(list) != 0 {
		t.Errorf("B sees %v", list)
	}
	if code, b := h.post("/api/sim/approvals/"+id, simB, body); code != 404 || b["error"] != "not_found" || h.grants() != 0 {
		t.Errorf("B approves A's session: %d %v", code, b)
	}
	// B cannot confirm A's scenario, nor start A's run with B's delegation.
	if code, b := h.post("/api/sim/delegations", simB, `{"request_id":"dlg-req-0000000000b1","scenario":"pay-a"}`); code != 403 || b["error"] != "forbidden_consumer" {
		t.Errorf("B confirms A's delegation: %d %v", code, b)
	}
	code, d := h.post("/api/sim/delegations", simB, `{"request_id":"dlg-req-0000000000b2","scenario":"pay-b"}`)
	if code != 201 {
		t.Fatalf("B's delegation: %d %v", code, d)
	}
	bid := d["delegation"].(map[string]any)["delegation_id"].(string)
	if code, b := h.post("/api/host/runs", h.tokens["operator"], `{"request_id":"run-req-00000000000x","scenario":"pay-a","delegation_id":"`+bid+`"}`); code != 409 || b["error"] != "delegation_unavailable" {
		t.Errorf("A's run with B's delegation: %d %v", code, b)
	}
	// The catalog decides the contract: apa.pay.v1 needs a delegation, M1 takes none.
	if code, b := h.post("/api/host/runs", h.tokens["operator"], startBody("run-req-00000000000y", "pay-b")); code != 400 || b["error"] != "delegation_required" {
		t.Errorf("apa.pay.v1 without a delegation: %d %v", code, b)
	}
	if code, b := h.post("/api/host/runs", h.tokens["operator"], `{"request_id":"run-req-00000000000z","scenario":"ask","delegation_id":"`+bid+`"}`); code != 400 || b["error"] != "delegation_required" {
		t.Errorf("M1 with a delegation: %d %v", code, b)
	}
	if code, _ := h.post("/api/host/runs", h.tokens["operator"], `{"request_id":"run-req-00000000000w","scenario":"ask","contract_version":"apa.pay.v1"}`); code != 400 {
		t.Errorf("a client-sent contract version: %d", code)
	}
}

func TestP1aClosedAndMismatch(t *testing.T) {
	var now atomic.Uint64
	now.Store(uint64(time.Now().Unix()))
	h := controlHost(t, hostOpts{expiryEvery: time.Hour, scenarios: payCatalog(t, 20), clock: now.Load})
	defer h.stop()
	sim := h.simulate("consumer-a")
	h.delegateAndRun(sim, "pay-a", "01")
	id, intent := pending(t, h.approvals(sim))
	// An intent the approver did not see: refused, nothing changes.
	if code, b := h.post("/api/sim/approvals/"+id, sim, resolveBody("mismatch-req-0000001", "sha256:"+strings.Repeat("0", 64), "approve")); code != 409 || b["error"] != "intent_mismatch" {
		t.Fatalf("intent mismatch: %d %v", code, b)
	}
	if _, i := pending(t, h.approvals(sim)); i != intent {
		t.Fatal("the session changed")
	}
	// At the deadline: expired (recorded by this request's transaction), no grant.
	now.Add(20)
	code, b := h.post("/api/sim/approvals/"+id, sim, resolveBody("late-req-000000000001", intent, "approve"))
	if code != 409 || b["error"] != "approval_closed" || h.grants() != 0 {
		t.Fatalf("approve at the deadline: %d %v", code, b)
	}
	if st := b["result"].(map[string]any)["purchase"].(map[string]any)["approval"].(map[string]any)["state"]; st != "expired" {
		t.Fatalf("state %v", st)
	}
	// A cancelled task's session is withdrawn: a later approval is closed.
	sim = h.simulate("consumer-b")
	run := h.delegateAndRun(sim, "pay-b", "02")
	id, intent = pending(t, h.approvals(sim))
	if code, b := h.post("/api/host/tasks/"+run["task_id"].(string)+"/cancel", h.tokens["operator"], `{}`); code != 200 {
		t.Fatalf("cancel: %d %v", code, b)
	}
	code, b = h.post("/api/sim/approvals/"+id, sim, resolveBody("after-cancel-req-0001", intent, "approve"))
	if code != 409 || b["result"].(map[string]any)["purchase"].(map[string]any)["approval"].(map[string]any)["state"] != "withdrawn" || h.grants() != 0 {
		t.Fatalf("approve after cancel: %d %v", code, b)
	}
}

func TestP1aDeny(t *testing.T) {
	h := controlHost(t, hostOpts{expiryEvery: time.Hour, scenarios: payCatalog(t, 60)})
	defer h.stop()
	sim := h.simulate("consumer-a")
	h.delegateAndRun(sim, "pay-a", "01")
	id, intent := pending(t, h.approvals(sim))
	code, b := h.post("/api/sim/approvals/"+id, sim, resolveBody("deny-req-000000000001", intent, "deny"))
	if r := b["result"].(map[string]any); code != 200 || r["outcome"] != "denied" || r["grant"] != nil || b["signer_stub"] != nil || h.grants() != 0 {
		t.Fatalf("deny: %d %v", code, b)
	}
	// The same request ID asking to approve now: a conflict, never a grant.
	if code, b := h.post("/api/sim/approvals/"+id, sim, resolveBody("deny-req-000000000001", intent, "approve")); code != 409 || b["error"] != "request_id_conflict" || h.grants() != 0 {
		t.Fatalf("same ID, other decision: %d %v", code, b)
	}
}

// A live host has no simulated approver: the handlers answer
// simulation_unavailable whatever the token (a handler test: no real CLI).
func TestP1aLiveRefusesSimulation(t *testing.T) {
	op := demoToken{grant: grant{Role: "operator"}, Token: strings.Repeat("a", 64)}
	a := &api{addr: "127.0.0.1:1", tokens: []demoToken{op}, clock: wallClock, out: os.Stderr} // sim nil: not fake
	mux := http.NewServeMux()
	a.registerSim(mux)
	for _, c := range []struct{ method, path, body string }{
		{"POST", "/api/host/simulate", `{"consumer_id":"consumer-a"}`},
		{"GET", "/api/sim/approvals", ""},
		{"POST", "/api/sim/delegations", `{"request_id":"dlg-req-000000000001","scenario":"pay-a"}`},
		{"POST", "/api/sim/approvals/aps-x", resolveBody("live-req-000000000001", "sha256:x", "approve")},
	} {
		req := httptest.NewRequest(c.method, "http://127.0.0.1:1"+c.path, strings.NewReader(c.body))
		req.Host = a.addr
		req.Header.Set("Origin", "http://"+a.addr)
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("Authorization", "Bearer "+op.Token)
		w := httptest.NewRecorder()
		mux.ServeHTTP(w, req)
		if w.Code != 403 || !strings.Contains(w.Body.String(), "simulation_unavailable") {
			t.Errorf("%s %s on a live host: %d %s", c.method, c.path, w.Code, w.Body.String())
		}
	}
}

// A live catalog cannot name apa.pay.v1: no real approver exists yet.
func TestP1aLiveCatalogRefusesPayContract(t *testing.T) {
	var regCfg contracts.RegistryConfig
	if err := decodeFile("testdata/demo/registry.json", &regCfg); err != nil {
		t.Fatal(err)
	}
	reg, _ := contracts.NewRegistry(regCfg)
	raw, _ := json.Marshal(map[string]any{"description": "test", "scenarios": []map[string]any{{"id": "pay-a", "consumer_id": "consumer-a", "contract": "apa.pay.v1", "label": "x",
		"delegation": map[string]any{"purpose": "x", "task_budget": "5000000", "allow_max": "100000", "approval_deadline_seconds": 30, "valid_seconds": 60}}}})
	p := filepath.Join(t.TempDir(), "live.json")
	os.WriteFile(p, raw, 0o600)
	if _, err := loadScenarios(p, reg, regCfg, true); err == nil || !strings.Contains(err.Error(), "fake only") {
		t.Fatalf("live apa.pay.v1 catalog: %v", err)
	}
	if _, err := loadScenarios(payCatalog(t, 30), reg, regCfg, false); err != nil {
		t.Fatalf("the fake catalog: %v", err)
	}
}

// A live host (its CLI is this test's fake behind a version script; nothing
// real runs) has no simulated approver: the operator's request is refused,
// and no run starts.
func TestP1aLiveHostHasNoSimulation(t *testing.T) {
	l := newLiveCLI(t)
	h := startHost(t, l.opts(false))
	defer h.stop()
	if code, b := h.post("/api/host/simulate", h.tokens["operator"], `{"consumer_id":"consumer-a"}`); code != 403 || b["error"] != "simulation_unavailable" {
		t.Fatalf("simulate on a live host: %d %v", code, b)
	}
	if code, _, _ := h.get("/api/sim/approvals", h.tokens["operator"]); code != 403 {
		t.Fatalf("approvals on a live host: %d", code)
	}
	if code, raw, _ := h.get("/api/host/runs", h.tokens["operator"]); code != 200 || strings.Contains(string(raw), "run_id") {
		t.Fatalf("runs: %d %s", code, raw)
	}
}

// The shipped P1-a catalog (make present-p1a) loads in fake mode and its
// delegations fit the demo profile.
func TestP1aShippedCatalog(t *testing.T) {
	var regCfg contracts.RegistryConfig
	if err := decodeFile("testdata/demo/registry.json", &regCfg); err != nil {
		t.Fatal(err)
	}
	reg, _ := contracts.NewRegistry(regCfg)
	scs, err := loadScenarios("testdata/demo/scenarios-p1a.json", reg, regCfg, false)
	if err != nil || len(scs) != 2 || scs["pay-a"].version() != "apa.pay.v1" || scs["pay-b"].ConsumerID != "consumer-b" {
		t.Fatalf("catalog: %v %v", scs, err)
	}
	raw, err := os.ReadFile("testdata/policy/m1-demo-profile.json")
	if err != nil {
		t.Fatal(err)
	}
	p, err := policy.LoadProfile(raw)
	if err != nil || checkDelegations(scs, p) != nil {
		t.Fatalf("profile: %v / %v", err, checkDelegations(scs, p))
	}
}
