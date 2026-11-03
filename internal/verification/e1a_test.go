package verification

import (
	"github.com/SIMPLYBOYS/agent-payment-authority/internal/merchant"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/SIMPLYBOYS/agent-payment-authority/internal/contracts"
	"github.com/SIMPLYBOYS/agent-payment-authority/internal/paycontract"
)

// The checker's copy of the E1a-D4 table is its own (e1aMode never asks
// paycontract.Evidence) and says what the Store's says, for every event
// type, method and run condition the Store writes under.
func TestE1aModeIsTheStoresTable(t *testing.T) {
	types := append(slices.Clone(paycontract.EventTypes), "CREDENTIAL_VERIFIED")
	for _, a := range []string{"live", "fake"} {
		for _, c := range []string{"base_sepolia", "local_anvil", "none"} {
			for _, au := range []string{"device", "virtual", "none"} {
				r := contracts.RunConditions{Agent: a, Chain: c, Authenticator: au}
				for _, typ := range types {
					for _, m := range []string{"", "simulated", "webauthn", "other"} {
						e := contracts.AuditEvent{EventType: typ, Payload: contracts.EventPayload{AuthorizationMethod: m}}
						mode, src, ok := paycontract.Evidence(&r, e)
						gm, gs := e1aMode(r, e)
						if ok && (gm != mode || gs != src) || !ok && gm != "" && (typ == "APPROVED" || typ == "GRANT_ISSUED") {
							t.Errorf("%s (%q) under %+v: checker %s/%s, Store %s/%s %v", typ, m, r, gm, gs, mode, src, ok)
						}
					}
				}
			}
		}
	}
}

// E7 across runs: a grant or a delegation is one consumer's.
func TestE1aApart(t *testing.T) {
	var cfg contracts.RegistryConfig
	if err := readTrustedJSON("../..", "testdata/contracts/registry.json", &cfg); err != nil {
		t.Fatal(err)
	}
	reg, err := contracts.NewRegistry(cfg)
	if err != nil {
		t.Fatal(err)
	}
	ev := func(task, consumer string, p contracts.EventPayload) contracts.AuditEvent {
		return contracts.AuditEvent{Scope: &contracts.EventScope{TaskID: task, ConsumerID: consumer, IntentHash: "sha256:" + task}, Payload: p}
	}
	runs := func(b contracts.EventPayload) map[string]*e1aRun {
		return map[string]*e1aRun{
			"task-a": {task: E1aTask{TaskID: "task-a"}, events: []contracts.AuditEvent{ev("task-a", "consumer-a", contracts.EventPayload{DelegationID: "dlg-a", GrantID: "g-a"})}},
			"task-b": {task: E1aTask{TaskID: "task-b"}, events: []contracts.AuditEvent{ev("task-b", "consumer-b", b)}},
		}
	}
	var f findings
	e1aApart(&f, runs(contracts.EventPayload{DelegationID: "dlg-b", GrantID: "g-b"}), reg)
	if len(f) != 0 {
		t.Fatalf("apart: %+v", f)
	}
	for _, b := range []contracts.EventPayload{{DelegationID: "dlg-a"}, {GrantID: "g-a"}} {
		var f findings
		e1aApart(&f, runs(b), reg)
		if f.verdicts(E1aClaims)["E7"] != Fail {
			t.Errorf("%+v shared with consumer-a: %+v", b, f)
		}
	}
}

// Gap 4: "the delegation confirmed by the person" is off not_evaluated only
// when every run's DELEGATION_BOUND says the person's device.
func TestDelegationsByDevice(t *testing.T) {
	run := func(method string) *e1aRun {
		return &e1aRun{events: []contracts.AuditEvent{{EventType: "TASK_STARTED"}, {EventType: "DELEGATION_BOUND", Payload: contracts.EventPayload{AuthorizationMethod: method}}}}
	}
	for name, c := range map[string]struct {
		runs map[string]*e1aRun
		want bool
	}{
		"all by device":       {map[string]*e1aRun{"a": run("webauthn"), "b": run("webauthn")}, true},
		"one by the host":     {map[string]*e1aRun{"a": run("webauthn"), "b": run("")}, false},
		"no delegation event": {map[string]*e1aRun{"a": {events: []contracts.AuditEvent{{EventType: "TASK_STARTED"}}}}, false},
		"no run read":         {map[string]*e1aRun{}, false},
	} {
		if got := delegationsByDevice(c.runs); got != c.want {
			t.Errorf("%s: %v", name, got)
		}
	}
}

// The checker knows the town's live entries too (docs/commercial-live-town.md L4), each with its task and v2.
func TestE1aCatalogHasTheTown(t *testing.T) {
	c, err := e1aCatalog("../..", "")
	if err != nil {
		t.Fatal(err)
	}
	for _, id := range []string{"e1a-ask", "e1a-deny", "e1a-inject", "town-data-a", "town-data-b", "town-cafe-a", "town-cafe-b"} {
		if e, ok := c[id]; !ok || e.Task == "" || e.Contract != "apa.pay.v2" {
			t.Errorf("%s: %+v %v", id, e, ok)
		}
	}
}

// A pack is checked against the catalogs as they were at its commit (Aaron 11/3: the family's wording failed
// the S5 pack's E6): a task changed since keeps its older text; a commit this repository lacks, or no commit,
// reads them as they are now; a catalog not yet there at the commit has no entries.
func TestE1aCatalogAtThePacksCommit(t *testing.T) {
	repo := t.TempDir()
	git := func(args ...string) string {
		cmd := exec.Command("git", append([]string{"-C", repo, "-c", "user.email=t@t", "-c", "user.name=t", "-c", "commit.gpgsign=false"}, args...)...)
		out, err := cmd.CombinedOutput()
		if err != nil {
			t.Fatalf("git %v: %v %s", args, err, out)
		}
		return strings.TrimSpace(string(out))
	}
	write := func(task string) {
		dir := filepath.Join(repo, "testdata/demo")
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatal(err)
		}
		raw := `{"scenarios":[{"id":"town-data-a","task":"` + task + `","contract":"apa.pay.v2"}]}`
		if err := os.WriteFile(filepath.Join(dir, "scenarios-town-live.json"), []byte(raw), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	git("init", "-q")
	write("買一份韓國市場簡報。")
	git("add", ".")
	git("commit", "-q", "-m", "s5")
	old := git("rev-parse", "HEAD")
	write("幫我買一份首爾旅遊攻略。")
	git("commit", "-q", "-am", "family")
	write("working tree")
	if err := os.WriteFile(filepath.Join(repo, "testdata/demo/scenarios-e1a.json"), []byte(`{"scenarios":[]}`), 0o644); err != nil { // the other live catalog: never committed here
		t.Fatal(err)
	}
	for _, tc := range []struct{ commit, want string }{
		{old, "買一份韓國市場簡報。"},
		{git("rev-parse", "HEAD"), "幫我買一份首爾旅遊攻略。"},
		{strings.Repeat("0", 40), "working tree"}, // not in this repository
		{"", "working tree"},
		{"--output=x", "working tree"}, // never an option to git
	} {
		c, err := e1aCatalog(repo, tc.commit)
		if err != nil {
			t.Fatalf("%q: %v", tc.commit, err)
		}
		if got := c["town-data-a"].Task; got != tc.want {
			t.Errorf("%q: task %q, want %q", tc.commit, got, tc.want)
		}
	}
}

// E6 and E10 match an order to the trace's request_purchase by product: every product the shop sells, the
// town's cafe ones too (/scenes/cafe/lunch-set is cafe-lunch-set), reads back as its own ID from its URL.
func TestProductOfEveryOffer(t *testing.T) {
	shop, err := merchant.New("http://127.0.0.1:1")
	if err != nil {
		t.Fatal(err)
	}
	for _, o := range shop.Offers() {
		if got := productOf(o.URL); got != o.ProductID {
			t.Errorf("%s: %s reads as %q", o.ProductID, o.URL, got)
		}
	}
	if got := productOf("http://127.0.0.1:1/eval/kr-market-brief?v=1"); got != "kr-market-brief" {
		t.Errorf("an AE1 item: %q", got)
	}
}
