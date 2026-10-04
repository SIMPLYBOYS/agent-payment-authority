package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/SIMPLYBOYS/agent-payment-authority/internal/buyer"
	"github.com/SIMPLYBOYS/agent-payment-authority/internal/buyer/fakecli"
	"github.com/SIMPLYBOYS/agent-payment-authority/internal/contracts"
	"github.com/SIMPLYBOYS/agent-payment-authority/internal/policy"
	"github.com/SIMPLYBOYS/agent-payment-authority/internal/store"
	"github.com/SIMPLYBOYS/agent-payment-authority/internal/toolbridge"
)

// plan is the trusted host's list of fake runs. It is host configuration
// (a file on this machine), never something an agent or an HTTP client
// submits. budget_initial is the host's budget for that task, set before it
// starts; it is not a policy change.
type plan struct {
	Description string    `json:"description"`
	Runs        []planRun `json:"runs"`
}

type planRun struct {
	TaskID        string           `json:"task_id"`
	Scenario      string           `json:"scenario"`
	BudgetInitial contracts.Amount `json:"budget_initial,omitempty"`
	Note          string           `json:"note,omitempty"`
}

// runOutcome is what the host reports of one run: fixed codes and counts,
// never CLI text.
type runOutcome struct {
	TaskID      string `json:"task_id"`
	Skipped     string `json:"skipped,omitempty"` // why the run did not start
	Outcome     string `json:"outcome,omitempty"`
	Detail      string `json:"detail,omitempty"`
	TaskStarted bool   `json:"task_started"`
	StoreEnd    string `json:"store_end,omitempty"`
	EndError    string `json:"store_end_error,omitempty"`
}

type fakeRunner struct {
	cli      string
	plan     plan
	traceDir string
	reg      *contracts.Registry
	profile  policy.Profile
	d        deps
}

var versionLimits = buyer.Limits{Stdout: 4 << 10, Stderr: 4 << 10, Total: 5 * time.Second, Grace: time.Second}

// newFakeRunner reads the plan strictly and checks the fake before anything
// binds: every task is in the trusted registry, every scenario exists, and
// the executable's --version names fake-claude (the real CLI would run a
// model). The check runs with a scratch HOME, by absolute path.
func newFakeRunner(ctx context.Context, cliPath, planPath, traceDir string, reg *contracts.Registry,
	profile policy.Profile, d deps) (*fakeRunner, error) {
	raw, err := os.ReadFile(planPath)
	if err != nil {
		return nil, err
	}
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.DisallowUnknownFields()
	var p plan
	if err := dec.Decode(&p); err != nil || dec.More() {
		return nil, fmt.Errorf("plan %s: not a plan file", planPath)
	}
	seen := map[string]bool{}
	for i, r := range p.Runs {
		if _, _, err := reg.Binding(r.TaskID); err != nil || seen[r.TaskID] {
			return nil, fmt.Errorf("plan %s: run %d: task %q is not a distinct trusted registry task", planPath, i, r.TaskID)
		}
		seen[r.TaskID] = true
		if r.BudgetInitial != "" {
			if _, err := r.BudgetInitial.Units(); err != nil {
				return nil, fmt.Errorf("plan %s: run %d: budget_initial is not an amount", planPath, i)
			}
		}
		if p.Runs[i].Scenario, err = filepath.Abs(r.Scenario); err != nil {
			return nil, err
		}
		if fi, err := os.Stat(p.Runs[i].Scenario); err != nil || !fi.Mode().IsRegular() {
			return nil, fmt.Errorf("plan %s: run %d: scenario %s is not a file", planPath, i, r.Scenario)
		}
	}
	cli, err := filepath.Abs(cliPath)
	if err != nil {
		return nil, err
	}
	home, err := os.MkdirTemp("", "apa-fake-home-")
	if err != nil {
		return nil, err
	}
	defer os.RemoveAll(home)
	out, err := buyer.Version(ctx, cli, buyer.CLIEnv(scratchHome(d.getenv, home)), versionLimits)
	if err != nil || !strings.Contains(out, "fake-claude") {
		return nil, fmt.Errorf("--fake-cli %s is not the fake CLI (its --version must name fake-claude); refusing to run it", cliPath)
	}
	return &fakeRunner{cli: cli, plan: p, traceDir: traceDir, reg: reg, profile: profile, d: d}, nil
}

// scratchHome hides the user's HOME from the fake: nothing of a login is near it.
func scratchHome(getenv func(string) string, home string) func(string) string {
	return func(k string) string {
		if k == "HOME" {
			return home
		}
		return getenv(k)
	}
}

// runAll runs the plan in order, each run through the buyer host with the
// shared Store and bridge. A task already in the Store is not run again:
// the record stands, nothing is cleared.
func (f *fakeRunner) runAll(ctx context.Context, st *store.Store, bridge *toolbridge.Server) []runOutcome {
	var out []runOutcome
	for _, r := range f.plan.Runs {
		if ctx.Err() != nil {
			out = append(out, runOutcome{TaskID: r.TaskID, Skipped: "host_stopping"})
			continue
		}
		o := f.runOne(ctx, st, bridge, r)
		out = append(out, o)
		fmt.Fprintf(f.d.out, "demo: fake run %s: skipped=%q outcome=%q detail=%q task_started=%t store_end=%q\n",
			o.TaskID, o.Skipped, o.Outcome, o.Detail, o.TaskStarted, o.StoreEnd)
	}
	return out
}

func (f *fakeRunner) runOne(ctx context.Context, st *store.Store, bridge *toolbridge.Server, r planRun) runOutcome {
	task, _, _ := f.reg.Binding(r.TaskID) // checked when the plan was read
	o := runOutcome{TaskID: task.TaskID}
	if _, err := st.Task(contracts.Session{ConsumerID: task.ConsumerID, AgentID: task.AgentID, TaskID: task.TaskID}); err == nil {
		o.Skipped = "task_already_recorded"
		return o
	}
	home, err := os.MkdirTemp("", "apa-fake-home-")
	if err != nil {
		o.Skipped = "scratch_home_failed"
		return o
	}
	defer os.RemoveAll(home)
	env := buyer.CLIEnv(scratchHome(f.d.getenv, home), append([]string{fakecli.EnvScenario + "=" + r.Scenario}, f.d.fakeEnv...)...)
	limits := buyer.DefaultLimits
	if f.d.limits != nil {
		limits = *f.d.limits
	}
	h := &buyer.Host{Registry: f.reg, Bridge: bridge, CLI: f.cli, BridgeCmd: []string{f.d.self, "mcp-bridge"}, Env: env, Limits: limits,
		Gate: buyer.Gate{CLIVersion: fakecli.InitVersion, AcceptAuth: []string{fakecli.AuthSource}}, TraceDir: f.traceDir}
	prepare := func() (*store.Store, error) {
		// The credential is an M1 fixture made at start, valid for a day of
		// host time; nothing verifies a BBS+ proof.
		cred := policy.Credential{ConsumerID: task.ConsumerID, ValidUntil: f.d.clock() + 86400, EvidenceMode: "fixture"}
		if err := st.StartTask(store.TaskSetup{TaskID: task.TaskID, Profile: &f.profile, Credential: &cred, BudgetInitial: r.BudgetInitial}); err != nil {
			return nil, err
		}
		return st, nil
	}
	res, err := h.Run(ctx, task.TaskID, buyer.Prompt, prepare)
	if err != nil && !errors.Is(err, context.Canceled) {
		o.Detail = "host_error"
	}
	o.Outcome, o.TaskStarted, o.StoreEnd, o.EndError = string(res.Report.Outcome), res.TaskStarted, res.StoreEnd, res.EndError
	if o.Detail == "" {
		o.Detail = res.Report.Detail
	}
	if res.PrepareError != nil {
		o.Detail = "prepare_failed:" + store.Code(res.PrepareError)
	}
	return o
}
