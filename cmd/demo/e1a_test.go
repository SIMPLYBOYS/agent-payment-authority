package main

import (
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/SIMPLYBOYS/agent-payment-authority/internal/buyer"
	"github.com/SIMPLYBOYS/agent-payment-authority/internal/contracts"
)

// E1-a (docs/commercial-e1a.md 4.2): the live pay catalog loads only on a
// host that pays, each entry with its task's words, which the digest covers.
func TestE1aCatalog(t *testing.T) {
	var regCfg contracts.RegistryConfig
	if err := decodeFile("testdata/demo/registry.json", &regCfg); err != nil {
		t.Fatal(err)
	}
	reg, err := contracts.NewRegistry(regCfg)
	if err != nil {
		t.Fatal(err)
	}
	scs, err := loadScenarios("testdata/demo/scenarios-e1a.json", reg, regCfg, true, true)
	if err != nil || len(scs) != 3 || scs["e1a-inject"].Task != "買今天的韓國市場每日摘要。" || scs["e1a-ask"].Contract != "apa.pay.v2" {
		t.Fatalf("live, paying: %v %v", scs, err)
	}
	for _, c := range []struct{ live, pay bool }{{true, false}, {false, false}} {
		if _, err := loadScenarios("testdata/demo/scenarios-e1a.json", reg, regCfg, c.live, c.pay); err == nil {
			t.Errorf("live %v, paying %v: the E1-a catalog loaded", c.live, c.pay)
		}
	}
	raw, _ := os.ReadFile("testdata/demo/scenarios-e1a.json")
	p := filepath.Join(t.TempDir(), "no-task.json")
	os.WriteFile(p, []byte(strings.Replace(string(raw), `"task": "買一份韓國市場簡報第 1 期。",`, "", 1)), 0o600)
	if _, err := loadScenarios(p, reg, regCfg, true, true); err == nil || !strings.Contains(err.Error(), "task") {
		t.Errorf("a live pay entry without its task: %v", err)
	}
	// The model is given the entry's task in the paying host's words; a fake run, the fixed prompt.
	if got := promptFor(scs["e1a-deny"].Task); !strings.Contains(got, "you never pay directly") || !strings.HasSuffix(got, "買韓國市場完整歷史資料庫。") || promptFor("") != buyer.Prompt {
		t.Errorf("prompt %q", got)
	}
}

// E1a-D1 A: a live host that pays lets the host play the customer to
// confirm a delegation, and for nothing else: approvals answer
// simulation_unavailable.
func TestDelegationOnlySimulator(t *testing.T) {
	a := &api{addr: "127.0.0.1:8402", out: io.Discard, control: &control{scenarios: map[string]*scenario{}},
		sim: &simulator{consumers: []string{"consumer-a"}, now: time.Now, instance: "0123456789abcdef", delegationOnly: true}}
	tok, _, err := a.sim.issue("consumer-a")
	if err != nil {
		t.Fatal(err)
	}
	call := func(h http.HandlerFunc, method, body string) (int, string) {
		r := httptest.NewRequest(method, "http://127.0.0.1:8402/x", strings.NewReader(body))
		r.Host = "127.0.0.1:8402"
		r.Header.Set("Origin", "http://127.0.0.1:8402")
		r.Header.Set("Authorization", "Bearer "+tok)
		r.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()
		h(w, r)
		return w.Code, w.Body.String()
	}
	if code, body := call(a.simApprovals, "GET", ""); code != 403 || !strings.Contains(body, "simulation_unavailable") {
		t.Errorf("approvals: %d %s", code, body)
	}
	if code, body := call(a.simResolve, "POST", `{"request_id":"e1a-req-0000000001","intent_hash":"x","decision":"approve"}`); code != 403 || !strings.Contains(body, "simulation_unavailable") {
		t.Errorf("approve: %d %s", code, body)
	}
	// Past the guard: the delegation route takes the capability (here, an unknown scenario).
	if code, body := call(a.simDelegation, "POST", `{"request_id":"e1a-req-0000000001","scenario":"none"}`); code != 400 || !strings.Contains(body, "unknown_scenario") {
		t.Errorf("delegation: %d %s", code, body)
	}
}

// E1-a review: what each mode allows, and the starts a live host refuses.
func TestHostRoles(t *testing.T) {
	for _, c := range []struct {
		live, paying bool
		want         roles
	}{
		{false, false, roles{agent: "fake", sim: true}},
		{false, true, roles{agent: "fake", sim: true}},
		{true, false, roles{label: liveLabel, agent: "live"}},
		{true, true, roles{label: liveLabelPaying, agent: "live", sim: true, delegationOnly: true}},
	} {
		if got := hostRoles(c.live, c.paying); got != c.want {
			t.Errorf("live %v, paying %v: %+v, want %+v", c.live, c.paying, got, c.want)
		}
	}
	d := deps{getenv: func(string) string { return "" }, out: io.Discard, clock: wallClock}
	for args, want := range map[string]string{
		"--live-cli /x --scenarios x.json --approvals-signer s.sock --authenticator-source device": "only to pay",
		"--approvals-signer s.sock --authenticator-source device":                                  "a host that starts runs",
	} {
		if err := serve(t.Context(), strings.Fields(args), d); err == nil || !strings.Contains(err.Error(), want) {
			t.Errorf("%s: %v, want %q", args, err, want)
		}
	}
}
