package main

import (
	"bytes"
	"encoding/json"
	"io"
	"os"
	"path/filepath"
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
	if len(o.Requests) != 1 || o.Requests[0] != (ae1.Req{Product: "b", Verdict: "Deny"}) || strings.Join(o.Refused, ",") != "request_purchase:TASK_NOT_ACTIVE,request_purchase:UNKNOWN_PRODUCT" {
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

// A subset is never the plan (review 2-1).
func TestRunPlanIsTheCorpus(t *testing.T) {
	c := &ae1.Corpus{Cases: []ae1.Case{{Kind: ae1.Benign}, {Kind: ae1.Injection}, {Kind: ae1.Crafted}, {Kind: ae1.Fault}}}
	if planRuns(c, true, 3) != 6 || planRuns(c, false, 1) != 4 {
		t.Fatal(planRuns(c, true, 3), planRuns(c, false, 1))
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
		"--buyer b --out o --fake-cli f --first-rep 0": "--first-rep",
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
		"another corpus":    {CorpusSHA: "x", Model: "sonnet", Case: "b", Kind: ae1.Benign, Attempt: 3, Mode: "live"}, // only the corpus differs
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

// apa-buyer's last line explains a run that gave no report; it is bounded.
func TestLastLine(t *testing.T) {
	if lastLine([]byte("a\nb: why\n\n")) != "b: why" || lastLine(nil) != "no message" || len([]rune(lastLine([]byte(strings.Repeat("x", 400))))) != 301 {
		t.Fatal("lastLine")
	}
	l := &limited{w: new(bytes.Buffer), n: 4}
	l.Write([]byte("abcdef"))
	if l.w.String() != "abcd" {
		t.Fatal(l.w.String())
	}
}

// report --reconciliation (P2 H5): a local anvil reconciliation is read
// strictly and said to close nothing; anything else is refused, no report.
func TestReportReconciliation(t *testing.T) {
	corpus := filepath.Join("..", "..", "testdata/ae1/corpus.json")
	c, err := ae1.Load(corpus)
	if err != nil {
		t.Fatal(err)
	}
	var crafted string
	for _, k := range c.Cases {
		if k.Kind == ae1.Crafted {
			crafted = k.ID
			break
		}
	}
	dir := t.TempDir()
	rows := filepath.Join(dir, "results.jsonl")
	raw, _ := json.Marshal(ae1.Row{Corpus: c.Version, CorpusSHA: c.SHA256, Model: "fake", Case: crafted, Kind: ae1.Crafted, Attempt: 1, Mode: "fake"})
	os.WriteFile(rows, raw, 0o600)
	good := `{"version":"apa.reconciliation.v1","environment":"local_anvil","chain_id":84532,"token":"0x4444444444444444444444444444444444444444",` +
		`"from_block":0,"to_block":40,"wallets":["0x1111111111111111111111111111111111111111"],"known_authorizations":2,"unauthorized":[],"read_at":1}`
	run := func(name, file string) error {
		rec := filepath.Join(dir, name+".json")
		if file != "" {
			os.WriteFile(rec, []byte(file), 0o600)
		}
		return report([]string{"--out", filepath.Join(dir, name), "--reps", "1", "--corpus", corpus, "--thresholds", filepath.Join("..", "..", "testdata/ae1/thresholds.json"),
			"--reconciliation", rec, rows}, io.Discard)
	}
	if err := run("good", good); err != nil {
		t.Fatal(err)
	}
	md, _ := os.ReadFile(filepath.Join(dir, "good", "report.md"))
	js, _ := os.ReadFile(filepath.Join(dir, "good", "report.json"))
	if !strings.Contains(string(md), "不關閉驗收 18") || !strings.Contains(string(js), `"unauthorized_new_spend": "0 (local_anvil)"`) {
		t.Fatalf("report:\n%s\n%s", md, js)
	}
	for name, file := range map[string]string{
		"unknown-field": strings.Replace(good, `"read_at":1`, `"read_at":1,"extra":true`, 1),
		"two-values":    good + good,
		"not-a-report":  `{"version":"x"}`,
		"missing":       "",
	} {
		if err := run(name, file); err == nil {
			t.Errorf("%s: accepted", name)
		} else if _, err := os.Stat(filepath.Join(dir, name)); err == nil {
			t.Errorf("%s: a report was written", name)
		}
	}
}
