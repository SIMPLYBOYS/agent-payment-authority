package main

// Tests of the subway pass (docs/commercial-transit.md): one confirmation,
// then a fare per ride decided at once; a Deny ends the pass; it is not a
// run's place; the host's end closes it. Fake host, no payment.

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// transitCatalog: consumer A's subway pass with budget and allow_max (units)
// and a pay entry to shop with while it is open.
func transitCatalog(t *testing.T, budget, allow string) string {
	t.Helper()
	terms := func(purpose, budget, allow string) map[string]any {
		return map[string]any{"purpose": purpose, "task_budget": budget, "allow_max": allow, "approval_deadline_seconds": 60, "valid_seconds": 1800}
	}
	raw, _ := json.Marshal(map[string]any{"description": "test", "scenarios": []map[string]any{
		{"id": "transit-a", "consumer_id": "consumer-a", "scene": "subway", "contract": "apa.pay.v1", "transit": true, "label": "subway A", "delegation": terms("搭地下鐵", budget, allow)},
		{"id": "pay-a", "consumer_id": "consumer-a", "contract": "apa.pay.v1", "scenario": mainline(t), "label": "pay A", "delegation": terms("買一份韓國市場簡報", "5000000", "100000")},
	}})
	p := filepath.Join(t.TempDir(), "scenarios.json")
	os.WriteFile(p, raw, 0o600)
	return p
}

// velocityProfile is the demo's profile (testdata/policy) with a rate limit of max.
func velocityProfile(t *testing.T, max int) string {
	t.Helper()
	out, _ := json.Marshal(map[string]any{"profile_id": "transit-test", "evidence_mode": "fixture", "asset_id": "fixture-usdc", "allow_max": "100000",
		"task_cap": "5000000", "merchants": []string{"0x3333333333333333333333333333333333333333"}, "mode": "interactive",
		"velocity": map[string]any{"max": max, "window_seconds": 600}})
	f := filepath.Join(t.TempDir(), "profile.json")
	os.WriteFile(f, out, 0o600)
	return f
}

// openPass confirms the pass's delegation as sim (request n) and opens it.
func (h *host) openPass(sim, n string) (int, map[string]any) {
	h.t.Helper()
	code, d := h.post("/api/sim/delegations", sim, `{"request_id":"dlg-req-transit-`+n+`","scenario":"transit-a"}`)
	if code != 201 {
		h.t.Fatalf("confirm the pass's delegation: %d %v", code, d)
	}
	id := d["delegation"].(map[string]any)["delegation_id"].(string)
	return h.post("/api/host/transit", h.tokens["operator"], `{"request_id":"pass-req-transit-`+n+`","scenario":"transit-a","delegation_id":"`+id+`"}`)
}

func (h *host) ride(n string) (int, map[string]any) {
	h.t.Helper()
	return h.post("/api/host/transit/fare", h.tokens["operator"], `{"request_id":"fare-req-transit-`+n+`","consumer_id":"consumer-a","station":"shop-street"}`)
}

func TestTransitPass(t *testing.T) {
	h := controlHost(t, hostOpts{expiryEvery: time.Hour, scenarios: transitCatalog(t, "60000", "20000")}) // three rides of 0.02
	defer h.stop()
	sim := h.simulate("consumer-a")
	if code, r := h.ride("00"); code != 409 || r["error"] != "no_transit_pass" {
		t.Fatalf("a fare with no pass: %d %v", code, r)
	}
	if code, r := h.post("/api/host/runs", h.tokens["operator"], `{"request_id":"run-req-transit-00","scenario":"transit-a","delegation_id":"dlg-x"}`); code != 400 || r["error"] != "transit_scenario" {
		t.Fatalf("a pass started as a run: %d %v", code, r)
	}
	code, pass := h.openPass(sim, "01")
	if code != 201 || pass["transit"] != true || pass["live"] != "open" || pass["scene"] != "subway" || pass["state"] != "accepted" {
		t.Fatalf("open: %d %v", code, pass)
	}
	task := pass["task_id"].(string)
	if code, again := h.post("/api/host/transit", h.tokens["operator"], `{"request_id":"pass-req-transit-01","scenario":"transit-a","delegation_id":"`+pass["delegation_id"].(string)+`"}`); code != 200 || again["run_id"] != pass["run_id"] {
		t.Fatalf("resend: %d %v", code, again)
	}
	if code, other := h.openPass(sim, "02"); code != 409 || other["error"] != "transit_open" {
		t.Fatalf("a second pass while one is open: %d %v", code, other)
	}

	// Each ride its own purchase, allowed at once; a resend is the same ride.
	for i, n := range []string{"01", "02"} {
		code, r := h.ride(n)
		if code != 200 || r["decision"] != "Allow" || r["ride"] != float64(i+1) || r["pass"] != "open" || r["task_id"] != task ||
			!strings.HasSuffix(r["purchase_order_id"].(string), "-subway-fare-"+string(rune('1'+i))) || r["amount"] != "20000" {
			t.Fatalf("ride %s: %d %v", n, code, r)
		}
	}
	if code, r := h.ride("01"); code != 200 || r["ride"] != float64(1) || r["replay"] != true {
		t.Fatalf("resend of ride 1: %d %v", code, r)
	}
	// The customer shops while the pass is open: not a run's place.
	code, d := h.post("/api/sim/delegations", sim, `{"request_id":"dlg-req-transit-pay1","scenario":"pay-a"}`)
	if code != 201 {
		t.Fatalf("pay delegation: %d %v", code, d)
	}
	code, run := h.post("/api/host/runs", h.tokens["operator"], `{"request_id":"run-req-transit-pay1","scenario":"pay-a","delegation_id":"`+d["delegation"].(map[string]any)["delegation_id"].(string)+`"}`)
	if code != 202 {
		t.Fatalf("a shop run beside the pass: %d %v", code, run)
	}
	h.waitRun(run["run_id"].(string), ended)

	if code, r := h.ride("03"); code != 200 || r["decision"] != "Allow" || r["ride"] != float64(3) || r["pass"] != "open" {
		t.Fatalf("ride 3: %d %v", code, r)
	}
	// The host proposed every fare (no Agent): its events say so.
	code, raw, _ := h.get("/api/tasks/"+task+"/events", h.tokens["viewer:consumer-a"])
	var ev eventsReply
	json.Unmarshal(raw, &ev)
	proposed := 0
	for _, e := range ev.Events {
		if e.EventType == "PURCHASE_PROPOSED" {
			proposed++
			if e.EvidenceMode+"/"+e.Source != "fixture/host" {
				t.Fatalf("a fare's proposal: %s/%s", e.EvidenceMode, e.Source)
			}
		}
	}
	if code != 200 || proposed != 3 {
		t.Fatalf("events: %d, %d proposals", code, proposed)
	}
}

// A Deny ends the task (the pay contract's rule), so it ends the pass; the
// next gate needs another confirmation and gets a new task. (A fake host
// pays nothing, so the budget is never drawn down: one under a fare.)
func TestTransitDenyEndsPass(t *testing.T) {
	h := controlHost(t, hostOpts{expiryEvery: time.Hour, scenarios: transitCatalog(t, "10000", "10000")})
	defer h.stop()
	sim := h.simulate("consumer-a")
	code, pass := h.openPass(sim, "01")
	if code != 201 {
		t.Fatalf("open: %d %v", code, pass)
	}
	if code, r := h.ride("01"); code != 200 || r["decision"] != "Deny" || r["reason"] != "INSUFFICIENT_BUDGET" || r["pass"] != "closed" {
		t.Fatalf("a fare over the budget: %d %v", code, r)
	}
	if code, r := h.ride("02"); code != 409 || r["error"] != "no_transit_pass" {
		t.Fatalf("a fare after the Deny: %d %v", code, r)
	}
	if v := h.run(pass["run_id"].(string)); v["state"] != "ended" || v["outcome"] != "ok" || v["live"] != nil {
		t.Fatalf("the pass's run: %v", v)
	}
	if code, next := h.openPass(sim, "02"); code != 201 || next["task_id"] == pass["task_id"] {
		t.Fatalf("a new pass: %d %v", code, next)
	}
}

func TestTransitVelocityAndHostEnd(t *testing.T) {
	cat := transitCatalog(t, "200000", "20000")
	o := hostOpts{expiryEvery: time.Hour, scenarios: cat, args: []string{"--profile", velocityProfile(t, 2)}}
	h := controlHost(t, o)
	sim := h.simulate("consumer-a")
	if code, p := h.openPass(sim, "01"); code != 201 {
		t.Fatalf("open: %d %v", code, p)
	}
	for _, n := range []string{"01", "02"} {
		if code, r := h.ride(n); code != 200 || r["decision"] != "Allow" {
			t.Fatalf("ride %s: %d %v", n, code, r)
		}
	}
	if code, r := h.ride("03"); code != 200 || r["decision"] != "Deny" || r["reason"] != "VELOCITY_LIMIT" || r["pass"] != "closed" {
		t.Fatalf("the third within the window: %d %v", code, r)
	}
	// An open pass when the host stops: cancelled, its run ended.
	code, p := h.openPass(sim, "02")
	if code != 201 {
		t.Fatalf("open again: %d %v", code, p)
	}
	if err := h.stop(); err != nil {
		t.Fatal(err)
	}
	o.dir = h.dir
	h = controlHost(t, o)
	defer h.stop()
	if v := h.run(p["run_id"].(string)); v["state"] != "ended" || v["outcome"] != "cancelled" || v["store_end"] != "TASK_CANCELLED" {
		t.Fatalf("the pass after the host's end: %v", v)
	}
}
