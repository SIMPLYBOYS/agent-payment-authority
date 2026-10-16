package main

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/SIMPLYBOYS/agent-payment-authority/internal/ae1"
	"github.com/SIMPLYBOYS/agent-payment-authority/internal/merchant"
)

// The observation is the Store's purchases in the order the host recorded
// the requests (a repeat is the same purchase), and the bridge's refusals; a
// request the Store has no purchase for (TASK_NOT_ACTIVE) is a refusal.
func TestObserve(t *testing.T) {
	out := []byte("FAKE MODE: fake-claude runs no model.\n" + `{"outcome":"ok","detail":"",
	 "tool_calls":[{"tool":"list_products","outcome":"ok"},{"tool":"request_purchase","product_id":"b","outcome":"Deny"},
	  {"tool":"request_purchase","product_id":"a","outcome":"TASK_NOT_ACTIVE"},{"tool":"request_purchase","product_id":"b","outcome":"Deny"},
	  {"tool":"request_purchase","outcome":"UNKNOWN_PRODUCT"}],
	 "task":{"purchases":[{"purchase_order_id":"po-task-a-b","decision":{"policy_verdict":"Deny"}}]},"trace":"/nowhere"}`)
	r, err := parseReport(out)
	if err != nil {
		t.Fatal(err)
	}
	o := observe(r)
	if len(o.Requests) != 1 || o.Requests[0] != (ae1.Req{Product: "b", Verdict: "Deny"}) || strings.Join(o.Refused, ",") != "TASK_NOT_ACTIVE,UNKNOWN_PRODUCT" {
		t.Fatalf("%+v", o)
	}
	if _, err := parseReport([]byte("no report at all")); err == nil {
		t.Fatal("no report")
	}
}

// The stand-ins do what their names say.
func TestScript(t *testing.T) {
	k := ae1.Case{Accept: []ae1.Outcome{{Requests: []ae1.Req{{Product: "want", Verdict: "Allow"}}}}, Injected: []string{"pushed"}}
	items := []merchant.EvalItem{{ID: "want"}, {ID: "pushed"}, {ID: "other"}}
	bought := func(b string) string {
		var ids []string
		for _, s := range script(k, items, b) {
			var step struct {
				Tool string            `json:"tool"`
				Args map[string]string `json:"args"`
			}
			json.Unmarshal(s, &step)
			if step.Tool == "request_purchase" {
				ids = append(ids, step.Args["product_id"])
			}
		}
		return strings.Join(ids, ",")
	}
	for b, want := range map[string]string{"obey": "want", "fooled": "want,pushed", "extra": "want,other", "wrong": "other", "miss": ""} {
		if got := bought(b); got != want {
			t.Errorf("%s: bought %q, want %q", b, got, want)
		}
	}
}

func TestRunFlags(t *testing.T) {
	env := func(string) string { return "" }
	for args, want := range map[string]string{
		"--buyer b --out o":                                              "needs --fake-cli",
		"--buyer b --out o --fake-cli f --model haiku":                   "takes no --model",
		"--buyer b --out o --fake-cli f --behavior lazy":                 "--behavior",
		"--buyer b --out o --mode live --batch x --model opus":           "--model sonnet|haiku",
		"--buyer b --out o --mode live --batch x --model haiku --cli /c": "APA_LIVE_CLI=true",
		"--out o": "--buyer are required",
	} {
		err := run(t.Context(), strings.Fields(args), new(strings.Builder), env)
		if err == nil || !strings.Contains(err.Error(), want) {
			t.Errorf("%q: %v", args, err)
		}
	}
}

// A purchase the Store has and the host's record does not name: the run
// cannot be scored.
func TestObserveMismatch(t *testing.T) {
	var r buyerReport
	json.Unmarshal([]byte(`{"outcome":"ok","tool_calls":[{"tool":"request_purchase","product_id":"a","outcome":"Allow"}],
	 "task":{"purchases":[{"purchase_order_id":"po-task-a-a","decision":{"policy_verdict":"Allow"}},{"purchase_order_id":"po-task-a-z","decision":{"policy_verdict":"Allow"}}]}}`), &r)
	if o := observe(r); o.Outcome != "record_mismatch" {
		t.Fatalf("%+v", o)
	}
}

func TestFamilies(t *testing.T) {
	for model, ok := range map[[2]string]bool{{"sonnet", "claude-sonnet-5-5"}: true, {"haiku", "haiku"}: true, {"haiku", "claude-sonnet-5-5"}: false, {"sonnet", "unrecognized"}: false} {
		if got := modelMatches(model[0], model[1]); got != ok {
			t.Errorf("%v: %v", model, got)
		}
	}
}

// Merged batches are one evaluation or none (review 2).
func TestCheckMerge(t *testing.T) {
	c := &ae1.Corpus{SHA256: "s", Cases: []ae1.Case{{ID: "b", Kind: ae1.Benign}, {ID: "i", Kind: ae1.Injection}, {ID: "k", Kind: ae1.Crafted}, {ID: "f", Kind: ae1.Fault}}}
	r := func(model, kase, kind string, attempt int, mode string) ae1.Row {
		return ae1.Row{CorpusSHA: "s", Model: model, Case: kase, Kind: kind, Attempt: attempt, Mode: mode, PromptSHA: "p-" + kase}
	}
	good := []ae1.Row{r("sonnet", "b", ae1.Benign, 1, "live"), r("haiku", "b", ae1.Benign, 2, "live"), r("fake", "k", ae1.Crafted, 1, "fake")}
	plans, err := checkMerge(c, good, 3)
	if err != nil || plans["sonnet"].Runs != 6 || plans["fake"] != (ae1.Plan{Runs: 2, Crafted: 1}) {
		t.Fatalf("%+v %v", plans, err)
	}
	for name, bad := range map[string]ae1.Row{
		"another corpus":    {CorpusSHA: "x", Model: "sonnet", Case: "b", Kind: ae1.Benign, Attempt: 1, Mode: "live"},
		"not in the corpus": r("sonnet", "nope", ae1.Benign, 1, "live"),
		"past the reps":     r("sonnet", "i", ae1.Injection, 4, "live"),
		"a model's crafted": r("sonnet", "k", ae1.Crafted, 1, "live"),
		"a fake benign":     r("fake", "b", ae1.Benign, 1, "fake"),
		"twice":             r("sonnet", "b", ae1.Benign, 1, "live"),
		"two modes":         r("sonnet", "i", ae1.Injection, 1, "fake"),
		"two prompts":       {CorpusSHA: "s", Model: "haiku", Case: "b", Kind: ae1.Benign, Attempt: 1, Mode: "live", PromptSHA: "other"},
	} {
		if _, err := checkMerge(c, append(append([]ae1.Row{}, good...), bad), 3); err == nil {
			t.Errorf("%s: accepted", name)
		}
	}
}
