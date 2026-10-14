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
	"github.com/SIMPLYBOYS/agent-payment-authority/internal/credential"
	"github.com/SIMPLYBOYS/agent-payment-authority/internal/paycontract"
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
	TaskID        string                    `json:"task_id"`
	Scenario      string                    `json:"scenario"`
	BudgetInitial contracts.Amount          `json:"budget_initial,omitempty"`
	Note          string                    `json:"note,omitempty"`
	bound         func(*toolbridge.Binding) // told the run's binding (host runs only)
	contract      string                    // host runs: the run's contract version ("" = apa.m1.v2)
	delegation    string                    // host runs: an apa.pay.v1 run's confirmed delegation
	scene         string                    // host runs: the entry's demo scene ("" is the data shop)
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
	// The host's times and calls for the timing record (not reported).
	ProcessStarted, ProcessReaped, CallsClosed time.Time               `json:"-"`
	Calls                                      []toolbridge.CallRecord `json:"-"`
}

// cliRunner runs tasks through the buyer host: the fake CLI (plan or
// scenario runs), or, in live mode (live != nil), the approved Claude Code
// CLI under the shared live settings.
type cliRunner struct {
	cli      string
	plan     plan
	traceDir string
	reg      *contracts.Registry
	profile  policy.Profile
	d        deps
	live     *liveSettings
	creds    *credential.Dir // C2: apa.pay.v2 tasks' issuer and holders
}

// liveSettings are a live runner's: the approved version and whether the
// development demo plugin exception is on (docs/cli-poc.md 55).
type liveSettings struct {
	want    string // the approved `claude --version` output
	devDemo bool
}

// newLiveRunner checks, before anything binds, that the live run can start
// as apa-buyer run --live would: an absolute CLI path, the APA-only config
// directory (buyer.LiveEnv) and the approved version. Each run checks the
// last two again (the CLI may have been updated meanwhile).
func newLiveRunner(ctx context.Context, cliPath, traceDir string, reg *contracts.Registry, profile policy.Profile, d deps, devDemo bool) (*cliRunner, error) {
	if !filepath.IsAbs(cliPath) {
		return nil, errors.New("--live-cli needs the absolute path of the approved Claude Code executable; no PATH lookup is done")
	}
	want := cmp(d.liveVersion, buyer.ApprovedCLIVersion)
	env, err := buyer.LiveEnv(d.getenv)
	if err != nil {
		return nil, err
	}
	if err := buyer.CheckLiveVersion(ctx, cliPath, env, want); err != nil {
		return nil, err
	}
	return &cliRunner{cli: cliPath, traceDir: traceDir, reg: reg, profile: profile, d: d, live: &liveSettings{want: want, devDemo: devDemo}}, nil
}

var versionLimits = buyer.Limits{Stdout: 4 << 10, Stderr: 4 << 10, Total: 5 * time.Second, Grace: time.Second}

// newFakeRunner reads the plan strictly (none in scenario mode: planPath
// "") and checks the fake before anything binds: every task is in the
// trusted registry, every scenario exists, and the executable's --version
// names fake-claude (the real CLI would run a model). The check runs with a
// scratch HOME, by absolute path.
func newFakeRunner(ctx context.Context, cliPath, planPath, traceDir string, reg *contracts.Registry,
	profile policy.Profile, d deps) (*cliRunner, error) {
	var p plan
	if planPath != "" {
		raw, err := os.ReadFile(planPath)
		if err != nil {
			return nil, err
		}
		dec := json.NewDecoder(bytes.NewReader(raw))
		dec.DisallowUnknownFields()
		if err := dec.Decode(&p); err != nil || dec.More() {
			return nil, fmt.Errorf("plan %s: not a plan file", planPath)
		}
	}
	var err error
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
	return &cliRunner{cli: cli, plan: p, traceDir: traceDir, reg: reg, profile: profile, d: d}, nil
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
func (f *cliRunner) runAll(ctx context.Context, st *store.Store, bridge *toolbridge.Server) []runOutcome {
	var out []runOutcome
	for _, r := range f.plan.Runs {
		if ctx.Err() != nil {
			out = append(out, runOutcome{TaskID: r.TaskID, Skipped: "host_stopping"})
			continue
		}
		var o runOutcome
		if reserved, err := st.TaskReserved(r.TaskID); err != nil || reserved {
			// a host run has reserved this task (docs/demo-operation-design.md 13.8 R1)
			o = runOutcome{TaskID: r.TaskID, Skipped: "task_reserved_by_run"}
		} else {
			o = f.runOne(ctx, st, bridge, r)
		}
		out = append(out, o)
		fmt.Fprintf(f.d.out, "demo: fake run %s: skipped=%q outcome=%q detail=%q task_started=%t store_end=%q\n",
			o.TaskID, o.Skipped, o.Outcome, o.Detail, o.TaskStarted, o.StoreEnd)
	}
	return out
}

func (f *cliRunner) runOne(ctx context.Context, st *store.Store, bridge *toolbridge.Server, r planRun) runOutcome {
	task, _, _ := f.reg.Binding(r.TaskID) // checked when the plan was read
	o := runOutcome{TaskID: task.TaskID}
	if _, err := st.Task(contracts.Session{ConsumerID: task.ConsumerID, AgentID: task.AgentID, TaskID: task.TaskID}); err == nil {
		o.Skipped = "task_already_recorded"
		return o
	}
	var env []string
	gate := buyer.Gate{CLIVersion: fakecli.InitVersion, AcceptAuth: []string{fakecli.AuthSource}}
	if f.live != nil {
		// The same settings as apa-buyer run --live: the APA-only config
		// directory, the approved version (checked again here), the
		// production auth list and the exception only when it was asked for.
		var err error
		if env, err = buyer.LiveEnv(f.d.getenv); err != nil {
			o.Outcome, o.Detail = "start_failed", "host_error"
			return o
		}
		if err := buyer.CheckLiveVersion(ctx, f.cli, env, f.live.want); err != nil {
			o.Outcome, o.Detail = "start_failed", "cli_version"
			return o
		}
		gate = buyer.Gate{CLIVersion: strings.Fields(f.live.want)[0], AcceptAuth: buyer.LiveAcceptAuth, DevDemoPlugins: f.live.devDemo}
	} else {
		home, err := os.MkdirTemp("", "apa-fake-home-")
		if err != nil {
			o.Skipped = "scratch_home_failed"
			return o
		}
		defer os.RemoveAll(home)
		env = buyer.CLIEnv(scratchHome(f.d.getenv, home), append([]string{fakecli.EnvScenario + "=" + r.Scenario}, f.d.fakeEnv...)...)
	}
	limits := buyer.DefaultLimits
	if f.d.limits != nil {
		limits = *f.d.limits
	}
	h := &buyer.Host{Registry: f.reg, Bridge: bridge, CLI: f.cli, BridgeCmd: []string{f.d.self, "mcp-bridge"}, Env: env, Limits: limits,
		Gate: gate, TraceDir: f.traceDir, Bound: r.bound, Scene: r.scene}
	prepare := func() (*store.Store, error) {
		setup := store.TaskSetup{TaskID: task.TaskID, Profile: &f.profile, BudgetInitial: r.BudgetInitial, ContractVersion: r.contract, DelegationID: r.delegation}
		if r.contract == paycontract.V2 {
			// C2: the BBS+ credential, issued now; verified at each purchase.
			if err := taskCredential(f.creds, &setup, task.ConsumerID, f.d.clock()); err != nil {
				return nil, err
			}
		} else {
			// The credential is an M1 fixture made at start, valid for a day of
			// host time; nothing verifies a BBS+ proof.
			setup.Credential = &policy.Credential{ConsumerID: task.ConsumerID, ValidUntil: f.d.clock() + 86400, EvidenceMode: "fixture"}
		}
		if err := st.StartTask(setup); err != nil {
			return nil, err
		}
		return st, nil
	}
	res, err := h.Run(ctx, task.TaskID, buyer.Prompt, prepare)
	if err != nil && !errors.Is(err, context.Canceled) {
		o.Detail = "host_error"
	}
	o.Outcome, o.TaskStarted, o.StoreEnd, o.EndError = string(res.Report.Outcome), res.TaskStarted, res.StoreEnd, res.EndError
	o.ProcessStarted, o.ProcessReaped, o.CallsClosed, o.Calls = res.ProcessStarted, res.ProcessReaped, res.CallsClosed, res.Calls
	if o.Detail == "" {
		o.Detail = res.Report.Detail
	}
	if res.PrepareError != nil {
		o.Detail = "prepare_failed:" + store.CodeOf(res.PrepareError) // STORE_ERROR without a code; never its text
	}
	return o
}
