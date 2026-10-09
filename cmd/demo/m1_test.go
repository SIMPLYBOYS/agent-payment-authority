package main

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"maps"
	"math/big"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/SIMPLYBOYS/agent-payment-authority/internal/buyer"
	"github.com/SIMPLYBOYS/agent-payment-authority/internal/contracts"
	"github.com/SIMPLYBOYS/agent-payment-authority/internal/policy"
)

// M1-E1 (docs/m1-evidence.md): the integration cases of the accepted
// manifest that the demo host can run end to end, through the same host,
// fake CLI subprocess, MCP bridge, Store and authenticated HTTP API as the
// demo (the host runs in this test process). Expected values come from
// cases.json only. The observed values are read the way the Store-level
// test reads them (internal/store/cases_test.go), but from the host's HTTP
// API and, for the budget, from the database after the host stopped.
//
// With APA_M1_RUN_DIR set (cmd/m1-fixture), each case's evidence is written
// to <dir>/<case_id>.json: the expected and observed values, the task and
// event replies of the API, the allow-listed run traces, the host's runs
// and the budget; no token, no CLI text.
//
// deny-merchant, deny-asset and deny-unattended run with a trusted --profile
// (and, for deny-asset, --registry) of their own (m1Settings). The other three integration cases keep their
// Store-level evidence: the host has no trusted setting for an expired
// credential or a missing configuration, and its one product (500000) is
// above the case's allow amount (allow-unavailable: 50000 within 100000).
var m1E2E = []string{"ask", "deny-budget", "deny-merchant", "deny-asset", "deny-unattended", "cancel-pending", "cancel-no-purchase",
	"cli-timeout", "cli-timeout-pending", "approval-timeout", "approval-deadline-before", "approval-deadline-exact", "restart"}

// m1Settings change the trusted profile (testdata/policy/m1-demo-profile.json)
// and registry (testdata/demo/registry.json) for a case, as the case's
// parameters say: merchant not_allowed, asset unlisted (the registry lists
// another asset only, so the merchant's quoted asset is not trusted; the
// Store refuses a profile asset the registry does not list), mode unattended.
var m1Settings = map[string]func(profile, registry map[string]any){
	"deny-merchant": func(p, _ map[string]any) { p["merchants"] = []string{"0x5555555555555555555555555555555555555555"} },
	"deny-asset": func(p, r map[string]any) {
		p["asset_id"] = "other-usdc"
		r["assets"] = []map[string]any{{"asset_id": "other-usdc", "chain_id": "84532", "asset_contract": "0x6666666666666666666666666666666666666666",
			"decimals": 6, "eip712_name": "USDC", "eip712_version": "2"}}
	},
	"deny-unattended": func(p, _ map[string]any) { p["mode"] = "unattended" },
}

type m1Evidence struct {
	CaseID   string                       `json:"case_id"`
	Layer    string                       `json:"layer"` // host_e2e
	Harness  string                       `json:"harness"`
	Expected contracts.ExpectedOutcome    `json:"expected"`
	Observed m1Observed                   `json:"observed"`
	Tasks    []json.RawMessage            `json:"tasks"`     // GET /api/tasks/{id}, host token
	Events   map[string]json.RawMessage   `json:"events"`    // GET /api/tasks/{id}/events, host token
	Traces   map[string][]any             `json:"traces"`    // task -> its run traces through the history allow-list (loadTrace, checked against the Store)
	HostRuns json.RawMessage              `json:"host_runs"` // GET /api/host/runs, operator token
	Budget   map[string]map[string]string `json:"budget"`    // task -> initial, remaining (the database, read after the host stopped)
	Verdict  string                       `json:"verdict"`
}

type m1Observed struct {
	Result          string   `json:"result"`
	Reason          string   `json:"reason"`
	ApprovalStatus  string   `json:"approval_status"`
	PaymentStatus   string   `json:"payment_status"`
	BudgetDeducted  string   `json:"budget_deducted"`
	ForbiddenEvents []string `json:"forbidden_events_found"`
	Purchases       int      `json:"purchases"`
}

const m1Harness = "cmd/demo TestM1FixtureCases: the demo host (serve) in the test process with --scenarios; the fake CLI and the MCP bridge as subprocesses; replies read from the authenticated HTTP API with the host token"

func TestM1FixtureCases(t *testing.T) {
	var m contracts.CaseManifest
	raw, err := os.ReadFile("testdata/contracts/cases.json")
	if err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(raw, &m); err != nil || m.RequireAccepted() != nil {
		t.Fatalf("case manifest: %v", err)
	}
	profileRaw, _ := os.ReadFile("testdata/policy/m1-demo-profile.json")
	registryRaw, _ := os.ReadFile("testdata/demo/registry.json")
	profile, err := policy.LoadProfile(profileRaw)
	if err != nil {
		t.Fatal(err)
	}
	out := os.Getenv("APA_M1_RUN_DIR")
	if out != "" {
		if err := os.MkdirAll(out, 0o700); err != nil {
			t.Fatal(err)
		}
	}
	// One trusted catalog for every case: consumer-a, the accepted product.
	dir := t.TempDir()
	catalog := filepath.Join(dir, "m1-catalog.json")
	scen := func(steps ...map[string]any) string { return scenarioFile(t, steps...) }
	purchase := []map[string]any{tool("list_products"), tool("get_quote", "product_id", "kr-market"), tool("request_purchase", "product_id", "kr-market")}
	entries := []map[string]any{
		{"id": "ask", "consumer_id": "consumer-a", "scenario": mainline(t), "label": "Ask"},
		{"id": "deny-budget", "consumer_id": "consumer-a", "scenario": mainline(t), "budget_initial": "250000", "label": "Deny budget"},
		{"id": "deny-merchant", "consumer_id": "consumer-a", "scenario": mainline(t), "label": "Deny merchant (profile)"},
		{"id": "deny-asset", "consumer_id": "consumer-a", "scenario": mainline(t), "label": "Deny asset (profile)"},
		{"id": "deny-unattended", "consumer_id": "consumer-a", "scenario": mainline(t), "label": "Deny unattended (profile)"},
		{"id": "hang-before", "consumer_id": "consumer-a", "scenario": scen(tool("list_products"), map[string]any{"do": "hang"}), "label": "stops before a purchase"},
		// cancel-no-purchase stops a run that calls no tool: no request is in
		// flight when the stop comes (a stop during a tool call once left the
		// host's HTTP shutdown waiting past its 5 s limit, not reproduced).
		{"id": "idle", "consumer_id": "consumer-a", "scenario": scen(map[string]any{"do": "hang"}), "label": "no tool, then hangs"},
		{"id": "hang-after", "consumer_id": "consumer-a", "scenario": scen(append(purchase, map[string]any{"do": "hang"})...), "label": "stops after the Ask"},
	}
	cat, _ := json.Marshal(map[string]any{"description": "M1-E1 test catalog", "scenarios": entries})
	os.WriteFile(catalog, cat, 0o600)
	budgetOf := map[string]string{"deny-budget": "250000"}

	for _, id := range m1E2E {
		c := m.Cases[slices.IndexFunc(m.Cases, func(c contracts.AcceptanceCase) bool { return c.CaseID == id })]
		t.Run(id, func(t *testing.T) {
			params := maps.Clone(m.Defaults)
			maps.Copy(params, c.Parameters)
			// The host clock is the wall clock until a case sets it; then it
			// stays there (an offset on the seconds clock moved on by one
			// whenever a second passed during the wait below).
			var frozen atomic.Uint64
			clock := func() uint64 {
				if f := frozen.Load(); f != 0 {
					return f
				}
				return wallClock()
			}
			o := hostOpts{scenarios: catalog, args: []string{"--registry", "testdata/demo/registry.json"}, clock: clock, expiryEvery: 20 * time.Millisecond}
			if change := m1Settings[id]; change != nil {
				var p, r map[string]any
				json.Unmarshal(profileRaw, &p)
				json.Unmarshal(registryRaw, &r)
				change(p, r)
				d := t.TempDir()
				for name, v := range map[string]map[string]any{"profile": p, "registry": r} {
					b, _ := json.Marshal(v)
					os.WriteFile(filepath.Join(d, name+".json"), b, 0o600)
					o.args = append(o.args, "--"+name, filepath.Join(d, name+".json")) // the last --registry wins
				}
			}
			if c.Operation == "cli-timeout" {
				lim := buyer.DefaultLimits
				lim.Total = 3 * time.Second
				o.limits = &lim
			}
			h := startHost(t, o)
			stopped := false
			stop := func() {
				if !stopped {
					stopped = true
					if err := h.stop(); err != nil {
						t.Fatalf("host stop: %v\n%s", err, h.out.String())
					}
				}
			}
			defer stop()
			ev := m1Evidence{CaseID: id, Layer: "host_e2e", Harness: m1Harness, Expected: c.Expected, Events: map[string]json.RawMessage{}, Traces: map[string][]any{}, Budget: map[string]map[string]string{}}
			scenario := "ask"
			var tasks []string
			read := func(task string) taskReply {
				_, body, _ := h.get("/api/tasks/"+task, h.tokens["host"])
				var tr taskReply
				json.Unmarshal(body, &tr)
				return tr
			}
			start := func(sc string) map[string]any {
				code, v := h.post("/api/host/runs", h.tokens["operator"], startBody(fmt.Sprintf("req-m1-run-%08d", reqN.Add(1)), sc))
				if code != 202 {
					t.Fatalf("start %s: %d %v", sc, code, v)
				}
				tasks = append(tasks, v["task_id"].(string))
				return v
			}
			waitEvent := func(task, typ string) {
				for end := time.Now().Add(30 * time.Second); time.Now().Before(end); time.Sleep(20 * time.Millisecond) {
					_, body, _ := h.get("/api/tasks/"+task+"/events", h.tokens["host"])
					if strings.Contains(string(body), `"`+typ+`"`) {
						return
					}
				}
				t.Fatalf("%s: no %s", task, typ)
			}
			var obs m1Observed
			outcome := func(tr taskReply) (string, string, string) { // as purchaseOutcome in internal/store
				if len(tr.Purchases) == 0 {
					return "", "", "not_requested"
				}
				p := tr.Purchases[0]
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
			lastCode := func(task string) string {
				_, body, _ := h.get("/api/tasks/"+task+"/events", h.tokens["host"])
				var e struct {
					Events []struct {
						Code string `json:"code"`
					} `json:"events"`
				}
				json.Unmarshal(body, &e)
				if len(e.Events) == 0 {
					return ""
				}
				return e.Events[len(e.Events)-1].Code
			}

			switch c.Operation {
			case "policy":
				scenario = c.CaseID
				run := start(scenario)
				h.waitRun(run["run_id"].(string), ended)
				tr := read(tasks[0])
				p := tr.Purchases
				if len(p) == 1 {
					obs.Result, obs.Reason, obs.ApprovalStatus = p[0].Decision.PolicyVerdict, p[0].Decision.Reason, p[0].Decision.ApprovalStatus
				}
			case "advance-clock":
				run := start("ask")
				h.waitRun(run["run_id"].(string), ended)
				n, _ := strconv.ParseUint(params["advance_seconds"], 10, 64)
				decided := read(tasks[0]).Purchases[0].DecidedAt
				frozen.Store(decided + n)          // the host clock is now decided_at + advance_seconds
				time.Sleep(300 * time.Millisecond) // the expiry worker runs every 20 ms
				obs.Result, obs.Reason, obs.ApprovalStatus = outcome(read(tasks[0]))
			case "cancel":
				if params["before"] == "pending" {
					run := start("ask")
					h.waitRun(run["run_id"].(string), ended)
					if code, v := h.cancel(tasks[0]); code != 200 {
						t.Fatalf("cancel: %d %v", code, v)
					}
				} else {
					run := start("idle")
					waitEvent(tasks[0], "TASK_STARTED")
					if code, v := h.post("/api/host/runs/"+run["run_id"].(string)+"/stop", h.tokens["operator"], `{}`); code != 202 {
						t.Fatalf("stop: %d %v", code, v)
					}
					h.waitRun(run["run_id"].(string), ended)
				}
				tr := read(tasks[0])
				obs.Result, obs.Reason = tr.Status, lastCode(tasks[0])
				_, _, obs.ApprovalStatus = outcome(tr)
			case "cli-timeout":
				sc := "hang-before"
				if params["phase"] == "pending" {
					sc = "hang-after"
				}
				run := start(sc)
				h.waitRun(run["run_id"].(string), ended)
				tr := read(tasks[0])
				obs.Result, obs.Reason = tr.Status, lastCode(tasks[0])
				_, _, obs.ApprovalStatus = outcome(tr)
			case "restart-and-new-task":
				run := start("ask")
				h.waitRun(run["run_id"].(string), ended)
				first := read(tasks[0])
				stop()
				o.dir = h.dir
				h = startHost(t, o)
				stopped = false
				run = start("ask")
				h.waitRun(run["run_id"].(string), ended)
				second := read(tasks[1])
				if first.WalletID != "" && first.WalletID == second.WalletID && first.Payer == second.Payer && first.ConsumerID == second.ConsumerID && tasks[0] != tasks[1] {
					obs.Result, obs.Reason = "same-wallet", "PERSISTED_BINDING"
				} else {
					obs.Result, obs.Reason = "different-wallet", ""
				}
				obs.ApprovalStatus = "not_requested"
			default:
				t.Fatalf("operation %s has no end-to-end mapping", c.Operation)
			}

			// What every case shows: payment, forbidden events, the replies.
			obs.PaymentStatus = "unavailable"
			for _, task := range tasks {
				tr := read(task)
				obs.Purchases += len(tr.Purchases)
				for _, p := range tr.Purchases {
					if p.Decision.PaymentStatus != "unavailable" {
						obs.PaymentStatus = p.Decision.PaymentStatus
					}
				}
				_, tb, _ := h.get("/api/tasks/"+task, h.tokens["host"])
				_, eb, _ := h.get("/api/tasks/"+task+"/events", h.tokens["host"])
				ev.Tasks = append(ev.Tasks, json.RawMessage(tb))
				ev.Events[task] = json.RawMessage(eb)
				// The raw trace is CLI output; only its allow-list projection is kept.
				files, _ := filepath.Glob(filepath.Join(h.dir, "runs", task+"-*.jsonl")) // sorted
				ev.Traces[task] = []any{}
				for _, f := range files {
					if tr, err := loadTrace(f, h.st, true); err != nil {
						ev.Traces[task] = append(ev.Traces[task], map[string]string{"not_available": err.Error()})
					} else {
						ev.Traces[task] = append(ev.Traces[task], tr)
					}
				}
				for _, f := range c.Expected.ForbiddenEvents {
					if strings.Contains(string(eb), `"`+f+`"`) {
						obs.ForbiddenEvents = append(obs.ForbiddenEvents, f)
					}
				}
			}
			_, rb, _ := h.get("/api/host/runs", h.tokens["operator"])
			ev.HostRuns = json.RawMessage(rb)
			stop()
			// The budget, from the database after the host stopped.
			db, err := sql.Open("sqlite", filepath.Join(h.dir, "demo.db")+"?mode=ro")
			if err != nil {
				t.Fatal(err)
			}
			deducted := new(big.Int)
			for _, task := range tasks {
				var remaining string
				if err := db.QueryRow(`SELECT budget_remaining FROM tasks WHERE task_id = ?`, task).Scan(&remaining); err != nil {
					t.Fatalf("budget of %s: %v\nhost runs: %s\n%s", task, err, rb, h.out.String())
				}
				initial := string(profile.TaskCap)
				if b := budgetOf[scenario]; b != "" {
					initial = b
				}
				ev.Budget[task] = map[string]string{"initial": initial, "remaining": remaining}
				i, _ := new(big.Int).SetString(initial, 10)
				r, _ := new(big.Int).SetString(remaining, 10)
				deducted.Add(deducted, new(big.Int).Sub(i, r))
			}
			db.Close()
			obs.BudgetDeducted = deducted.String()
			ev.Observed = obs

			x := c.Expected
			ok := obs.Result == x.Result && obs.Reason == x.Reason && obs.ApprovalStatus == x.ApprovalStatus &&
				obs.PaymentStatus == x.PaymentStatus && obs.BudgetDeducted == string(x.BudgetDeducted) && len(obs.ForbiddenEvents) == 0
			// Cases that end before a purchase must leave none.
			if (c.Operation == "cancel" && params["before"] != "pending") || (c.Operation == "cli-timeout" && params["phase"] != "pending") {
				ok = ok && obs.Purchases == 0
			}
			ev.Verdict = "FAIL"
			if ok {
				ev.Verdict = "PASS"
			} else {
				t.Errorf("%s: observed %+v, expected %+v", id, obs, x)
			}
			if out != "" {
				body, _ := json.MarshalIndent(ev, "", "  ")
				for _, tok := range h.tokens {
					if strings.Contains(string(body), tok) {
						t.Fatalf("%s: a token in the evidence", id)
					}
				}
				if err := os.WriteFile(filepath.Join(out, id+".json"), append(body, '\n'), 0o600); err != nil {
					t.Fatal(err)
				}
			}
		})
	}
}
