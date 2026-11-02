package main

// Tests of UX3's data side (docs/commercial-scenes.md): a catalog entry's
// scene picks the storefront the run may buy from, and the host shows the
// scene to the page. Fake CLI, test keys; payment stays off.

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/SIMPLYBOYS/agent-payment-authority/internal/contracts"
	"github.com/SIMPLYBOYS/agent-payment-authority/internal/policy"
)

func TestUX3ShippedCatalog(t *testing.T) {
	var regCfg contracts.RegistryConfig
	if err := decodeFile("testdata/demo/registry.json", &regCfg); err != nil {
		t.Fatal(err)
	}
	reg, _ := contracts.NewRegistry(regCfg)
	scs, err := loadScenarios("testdata/demo/scenarios-ux3.json", reg, regCfg, false, false)
	if err != nil || len(scs) != 7 || scs["mart-b"].Scene != "mart" || scs["books-a"].Scene != "books" || scs["cafe-b"].Scene != "cafe" || scs["mart-a"].Scene != "mart" { // the family (docs/story.md)
		t.Fatalf("catalog: %v %v", scs, err)
	}
	raw, _ := os.ReadFile("testdata/policy/m1-demo-profile.json")
	if p, err := policy.LoadProfile(raw); err != nil || checkDelegations(scs, p) != nil {
		t.Fatalf("profile: %v", err)
	}
	// "data" is the omitted scene: an M1 entry's digest stays what it was.
	entry := func(scene string) string {
		e := map[string]any{"id": "ask", "consumer_id": "consumer-a", "scenario": "testdata/cli/ask-mainline.json", "label": "x"}
		if scene != "" {
			e["scene"] = scene
		}
		raw, _ := json.Marshal(map[string]any{"description": "test", "scenarios": []any{e}})
		p := filepath.Join(t.TempDir(), "scenarios.json")
		os.WriteFile(p, raw, 0o600)
		return p
	}
	plain, err1 := loadScenarios(entry(""), reg, regCfg, false, false)
	data, err2 := loadScenarios(entry("data"), reg, regCfg, false, false)
	if err1 != nil || err2 != nil || plain["ask"].digest != data["ask"].digest {
		t.Fatalf("data scene digest: %v %v", err1, err2)
	}
	cafe, err := loadScenarios(entry("cafe"), reg, regCfg, false, false)
	if err != nil || cafe["ask"].digest == plain["ask"].digest {
		t.Fatalf("a scene must change the digest: %v", err)
	}
	if _, err := loadScenarios(entry("zoo"), reg, regCfg, false, false); err == nil {
		t.Fatal("an unknown scene loaded")
	}
}

// Each scene's run buys its own storefront's product (the family, docs/story.md): the cafe's lunch and the
// mart's beer, within allow_max, are Allows; the page sees the scene.
func TestUX3ScenesEndToEnd(t *testing.T) {
	path, _ := credDir(t, "consumer-a", "consumer-b")
	h := controlHost(t, hostOpts{expiryEvery: time.Hour, scenarios: "testdata/demo/scenarios-ux3.json", args: []string{"--credential-dir", path}})
	defer h.stop()

	_, _, body := h.status(h.tokens["operator"])
	var st struct {
		Runs struct {
			Scenarios []map[string]any `json:"scenarios"`
		} `json:"runs"`
	}
	json.Unmarshal(body, &st)
	scenes := map[string]any{}
	for _, s := range st.Runs.Scenarios {
		scenes[s["id"].(string)] = s["scene"]
	}
	if scenes["books-a"] != "books" || scenes["cafe-b"] != "cafe" || scenes["mart-a"] != "mart" {
		t.Fatalf("status scenes: %v", scenes)
	}
	for i, c := range []struct{ id, sim, product, verdict string }{
		{"cafe-a", "consumer-a", "cafe-lunch-set", "Allow"},
		{"mart-a", "consumer-a", "mart-beer-6", "Allow"},
	} {
		run := h.delegateAndRunAs(h.simulate(c.sim), c.id, "0"+string(rune('1'+i)), "apa.pay.v2")
		task := run["task_id"].(string)
		if run["scene"] != c.id[:4] {
			t.Errorf("%s run view scene %v", c.id, run["scene"])
		}
		// The run read its own scene's catalog (review S1: the fake CLI
		// requests the purchase whatever list_products answered).
		rec, _ := h.record(run["run_id"].(string), task)
		var calls []string
		for _, x := range rec.Calls {
			calls = append(calls, x.Tool+":"+x.Outcome)
		}
		if got := strings.Join(calls, " "); got != "list_products:ok get_quote:ok request_purchase:"+c.verdict {
			t.Errorf("%s calls %s", c.id, got)
		}
		code, raw, _ := h.get("/api/tasks/"+task, h.tokens["host"])
		var v struct {
			Purchases []struct {
				PurchaseOrderID string `json:"purchase_order_id"`
				Decision        struct {
					PolicyVerdict string `json:"policy_verdict"`
					PaymentStatus string `json:"payment_status"`
				} `json:"decision"`
			} `json:"purchases"`
		}
		if code != 200 || json.Unmarshal(raw, &v) != nil || len(v.Purchases) != 1 || v.Purchases[0].PurchaseOrderID != "po-"+task+"-"+c.product ||
			v.Purchases[0].Decision.PolicyVerdict != c.verdict || v.Purchases[0].Decision.PaymentStatus != "not_enabled" {
			t.Fatalf("%s purchases: %d %s", c.id, code, raw)
		}
	}
}

// A run view shows the scene of the entry the run was accepted with; once
// the entry is edited (another digest), the view shows none rather than
// the new one.
func TestUX3RunViewScene(t *testing.T) {
	script := scenarioFile(t, done)
	catalog := filepath.Join(t.TempDir(), "scenarios.json")
	write := func(scene string) {
		raw, _ := json.Marshal(map[string]any{"description": "test", "scenarios": []any{
			map[string]any{"id": "s", "consumer_id": "consumer-a", "scene": scene, "scenario": script, "label": "s"}}})
		os.WriteFile(catalog, raw, 0o600)
	}
	write("cafe")
	h := controlHost(t, hostOpts{expiryEvery: time.Hour, scenarios: catalog})
	_, r := h.start("req-s-00000000000001", "s")
	id := r["run_id"].(string)
	if v := h.waitRun(id, ended); v["scene"] != "cafe" {
		t.Fatalf("run view: %v", v)
	}
	h.stop()
	write("mart")
	h = controlHost(t, hostOpts{dir: h.dir, expiryEvery: time.Hour, scenarios: catalog})
	defer h.stop()
	if v := h.run(id); v["run_id"] != id || v["scene"] != nil {
		t.Fatalf("run view after the entry changed: %v", v)
	}
}
