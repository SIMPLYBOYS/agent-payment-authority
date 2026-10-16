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
