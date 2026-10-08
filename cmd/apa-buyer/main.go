// Command apa-buyer is the local buyer host.
//
//	apa-buyer run --fake-cli PATH --scenario FILE [--task ID]
//	apa-buyer run --live --cli ABSOLUTE-PATH [--task ID]   (also needs APA_LIVE_CLI=true)
//	apa-buyer probe-init --cli ABSOLUTE-PATH [--send-one-message]  (also needs APA_LIVE_CLI=true)
//	apa-buyer mcp-bridge
//
// run starts the fixture shop on a loopback port and the CLI with the MCP
// bridge. The Store (APA_DB_PATH; default var/apa-fake.db, or
// var/apa-live.db for --live) is opened, and the task started, only after
// the CLI's init has passed the host's gate; then the tools are armed.
// Nothing is paid and no content is delivered.
//
// Fake mode runs the fake CLI: no model. Live mode runs the approved Claude
// Code executable, which runs a real model; it needs both --live and
// APA_LIVE_CLI=true, an absolute --cli path and the approved version, and
// its auth check accepts no value yet (docs/cli-poc.md section 7), so no
// live run can pass the gate in this build.
//
// probe-init is the C0 probe (docs/cli-poc.md section 17): it starts the
// approved CLI with no prompt and stream-json input it never writes to,
// waits up to 30 seconds for the init, and prints a whitelisted account of
// it. It opens no Store, runs no tool and never retries. Exit 0: init
// observed; 3: no init came before the wait ended; 1: anything else.
// --send-one-message selects C0′ (section 22): the same, but one fixed user
// message is written once; exit 4: no init came after the message was
// written whole. Nothing switches from C0 to C0′ on its own.
//
// mcp-bridge is started by the CLI from the host's MCP config, never by hand.
package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/SIMPLYBOYS/agent-payment-authority/internal/buyer"
	"github.com/SIMPLYBOYS/agent-payment-authority/internal/buyer/fakecli"
	"github.com/SIMPLYBOYS/agent-payment-authority/internal/contracts"
	"github.com/SIMPLYBOYS/agent-payment-authority/internal/merchant"
	"github.com/SIMPLYBOYS/agent-payment-authority/internal/policy"
	"github.com/SIMPLYBOYS/agent-payment-authority/internal/store"
	"github.com/SIMPLYBOYS/agent-payment-authority/internal/toolbridge"
)

func main() {
	// The bridge keeps default signal handling: the host's group kill ends it at once.
	if len(os.Args) > 1 && os.Args[1] == "mcp-bridge" {
		if err := toolbridge.RunBridge(context.Background(), os.Stdin, os.Stdout, os.Getenv); err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(1)
		}
		return
	}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	self, err := os.Executable()
	if err == nil {
		err = run(ctx, os.Args[1:], deps{getenv: os.Getenv, self: self, out: os.Stdout, clock: wallClock})
	}
	if err != nil {
		fmt.Fprintln(os.Stderr, "apa-buyer:", err)
		var ec exitCode
		if errors.As(err, &ec) {
			os.Exit(int(ec))
		}
		os.Exit(1)
	}
}

// exitCode is an error that sets the process exit code.
type exitCode int

func (e exitCode) Error() string { return fmt.Sprintf("exit status %d", int(e)) }

func wallClock() uint64 { return uint64(time.Now().Unix()) }

// approvedCLIVersion is the `claude --version` output a live run requires
// (2.1.287 recorded 2026-10-02; Aaron re-approved 2.1.288 on 2026-10-03,
// 2.1.289 on 2026-10-04 and 2.1.293 on 2026-10-08, each after a local
// update; 2.1.293 is the version Aaron gave, not one this repo queried).
// Another version stops the run; nothing upgrades.
const approvedCLIVersion = "2.1.293 (Claude Code)"

// liveAcceptAuth is the apiKeySource values a live init may state, for
// every live run, strict or the dev demo exception (Aaron 2026-10-08, L1-d;
// docs/live-purchase-execution-decision.md 9.3). Only "none". The value
// alone proves no login or billing: the basis is that the live environment
// carries no API key or token (CLIEnv) and the CLI uses the APA-only
// config directory, which Aaron confirmed is his own claude.ai
// subscription login (docs/cli-poc.md 48.1). Nothing widens the list.
var liveAcceptAuth = []string{"none"}

// deps are what tests replace.
type deps struct {
	getenv      func(string) string
	self        string // this executable, started again as the MCP bridge
	out         io.Writer
	clock       store.Clock
	limits      *buyer.Limits
	liveVersion string                                   // "" means approvedCLIVersion
	liveAuth    []string                                 // nil means liveAcceptAuth
	onStore     func(*store.Store)                       // after the task started, before the tools are armed
	openStore   func(store.Config) (*store.Store, error) // nil means store.Open
}

// prompt is the host's fixed instruction; the model never edits it.
const prompt = buyer.Prompt

// defaultDB keeps fake and live records apart from each other and from the
// demo's var/apa.db.
func defaultDB(live bool) string {
	if live {
		return "var/apa-live.db"
	}
	return "var/apa-fake.db"
}

func run(ctx context.Context, args []string, d deps) (err error) {
	if len(args) > 0 && args[0] == "probe-init" {
		return probe(ctx, args[1:], d)
	}
	if len(args) == 0 || args[0] != "run" {
		return errors.New("usage: apa-buyer run --fake-cli PATH --scenario FILE [--task ID] | run --live --cli ABSOLUTE-PATH [--task ID] | probe-init --cli ABSOLUTE-PATH  (or: apa-buyer mcp-bridge)")
	}
	fs := flag.NewFlagSet("run", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	fake := fs.String("fake-cli", "", "fake CLI executable")
	scenario := fs.String("scenario", "", "fake CLI scenario, e.g. testdata/cli/ask-mainline.json")
	liveFlag := fs.Bool("live", false, "run the approved Claude Code CLI (a real model); also needs APA_LIVE_CLI=true")
	devDemo := fs.Bool("dev-demo-plugins", false, "live only: the development demo exception; the init may list exactly the two named plugins (docs/live-purchase-execution-decision.md 9)")
	cliPath := fs.String("cli", "", "absolute path of the approved Claude Code executable (live only)")
	taskID := fs.String("task", "task-a", "trusted registry task to run")
	registryPath := fs.String("registry", "testdata/contracts/registry.json", "trusted registry fixture")
	profilePath := fs.String("profile", "testdata/policy/m1-demo-profile.json", "consumer profile fixture")
	traceDir := fs.String("trace-dir", "var/runs", "where run traces are written")
	if err := fs.Parse(args[1:]); err != nil {
		return err
	}
	if fs.NArg() != 0 {
		return fmt.Errorf("unexpected argument %q", fs.Arg(0))
	}
	live := *liveFlag
	switch {
	case live && (*fake != "" || *scenario != ""):
		return errors.New("--live cannot be combined with --fake-cli or --scenario")
	case !live && *devDemo:
		return errors.New("--dev-demo-plugins is for live runs only (--live with APA_LIVE_CLI=true)")
	case !live && *cliPath != "":
		return errors.New("--cli is for live runs only (--live with APA_LIVE_CLI=true)")
	case !live && *fake == "":
		return errors.New("pass --fake-cli for a fake run; a live run needs both --live and APA_LIVE_CLI=true (see docs/cli-poc.md)")
	}

	var (
		cli  string
		env  []string
		gate buyer.Gate
	)
	if live {
		if on, perr := strconv.ParseBool(d.getenv("APA_LIVE_CLI")); perr != nil || !on {
			return errors.New("--live also needs APA_LIVE_CLI=true; nothing was started")
		}
		if !filepath.IsAbs(*cliPath) {
			return errors.New("--live needs --cli with the absolute path of the approved Claude Code executable; no PATH lookup is done")
		}
		want := cmp(d.liveVersion, approvedCLIVersion)
		cli = *cliPath
		if env, err = buyer.LiveEnv(d.getenv); err != nil {
			return err
		}
		if ctx.Err() != nil {
			return errors.New("cancelled before the version check; nothing was started")
		}
		if err := checkLive(ctx, cli, env, want); err != nil {
			return err
		}
		accept := liveAcceptAuth
		if d.liveAuth != nil {
			accept = d.liveAuth
		}
		gate = buyer.Gate{CLIVersion: strings.Fields(want)[0], AcceptAuth: accept, DevDemoPlugins: *devDemo}
	} else {
		if cli, err = filepath.Abs(*fake); err != nil {
			return err
		}
		// The fake gets a scratch HOME: nothing of the user's login is near it.
		home, err := os.MkdirTemp("", "apa-fake-home-")
		if err != nil {
			return err
		}
		defer os.RemoveAll(home)
		getenv := func(k string) string {
			if k == "HOME" {
				return home
			}
			return d.getenv(k)
		}
		if err := checkFake(ctx, cli, getenv); err != nil {
			return err
		}
		scen, err := filepath.Abs(*scenario)
		if err != nil || *scenario == "" {
			return errors.New("--scenario is required")
		}
		if _, err := os.Stat(scen); err != nil {
			return err
		}
		env = buyer.CLIEnv(getenv, fakecli.EnvScenario+"="+scen)
		gate = buyer.Gate{CLIVersion: fakecli.InitVersion, AcceptAuth: []string{fakecli.AuthSource}}
	}
	dbPath := cmp(d.getenv("APA_DB_PATH"), defaultDB(live))

	var regCfg contracts.RegistryConfig
	if err := decodeFile(*registryPath, &regCfg); err != nil {
		return fmt.Errorf("registry: %w", err)
	}
	reg, err := contracts.NewRegistry(regCfg)
	if err != nil {
		return err
	}
	rawProfile, err := os.ReadFile(*profilePath)
	if err != nil {
		return err
	}
	profile, err := policy.LoadProfile(rawProfile)
	if err != nil {
		return fmt.Errorf("profile: %w", err)
	}
	task, _, err := reg.Binding(*taskID)
	if err != nil {
		return fmt.Errorf("task %q is not in the trusted registry", *taskID)
	}
	sess := contracts.Session{ConsumerID: task.ConsumerID, AgentID: task.AgentID, TaskID: task.TaskID}

	// Shop first: the Store's offer book is this configuration, never a 402
	// that happened to arrive.
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return err
	}
	shop, err := merchant.New("http://" + ln.Addr().String())
	if err != nil {
		ln.Close()
		return err
	}
	mux := http.NewServeMux()
	shop.Register(mux)
	srv := &http.Server{Handler: mux, ReadHeaderTimeout: 5 * time.Second}
	go srv.Serve(ln)
	defer func() {
		sctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		err = errors.Join(err, srv.Shutdown(sctx))
	}()
	offers := store.OfferBook{}
	products := map[string]toolbridge.Product{}
	for _, o := range shop.Offers() {
		k := store.OfferKey{Method: o.Method, URL: o.URL}
		offers[k] = append(offers[k], o.Quote)
		products[o.ProductID] = toolbridge.Product{ID: o.ProductID, URL: o.URL}
	}

	// The Store, the task and expiry start in prepare, after the CLI's init
	// passed the gate; they stop, in this order, after the bridge closes.
	var (
		st         *store.Store
		stopExpiry = func() {}
		expiry     sync.WaitGroup
		expiryMu   sync.Mutex
		expiryErrs []string
	)
	defer func() {
		stopExpiry()
		expiry.Wait()
		if st != nil {
			err = errors.Join(err, st.Close())
		}
	}()
	tools := &toolbridge.Shop{CatalogURL: "http://" + ln.Addr().String() + "/catalog", Products: products, Client: toolbridge.NewClient(), Now: d.clock}
	prepare := func() (*store.Store, error) {
		if ctx.Err() != nil {
			return nil, errors.New("cancelled before the task started; nothing was recorded")
		}
		open := store.Open
		if d.openStore != nil {
			open = d.openStore
		}
		s, err := open(store.Config{Path: dbPath, Registry: regCfg, Offers: offers, Clock: d.clock})
		if err != nil {
			return nil, err
		}
		// The credential is an M1 fixture made at start, valid for a day of
		// host time; nothing verifies a BBS+ proof.
		cred := policy.Credential{ConsumerID: task.ConsumerID, ValidUntil: d.clock() + 86400, EvidenceMode: "fixture"}
		if err := s.StartTask(store.TaskSetup{TaskID: task.TaskID, Profile: &profile, Credential: &cred}); err != nil {
			s.Close()
			if store.Code(err) == "TASK_ALREADY_STARTED" {
				return nil, fmt.Errorf("task %s already exists in %s; nothing was changed or deleted (the CLI had already started; a live CLI may already have used quota). Run another task (--task task-a-next) or set APA_DB_PATH to a new file: %w", task.TaskID, dbPath, err)
			}
			return nil, err
		}
		// Expiry runs for the host's lifetime; its errors are reported, not swallowed.
		expiryCtx, cancel := context.WithCancel(context.Background())
		stopExpiry = cancel
		expiry.Go(func() {
			s.RunExpiry(expiryCtx, time.Second, func(err error) {
				expiryMu.Lock()
				expiryErrs = append(expiryErrs, store.Code(err))
				expiryMu.Unlock()
				fmt.Fprintln(os.Stderr, "apa-buyer: expiry:", store.Code(err))
			})
		})
		tools.Store, st = s, s
		if d.onStore != nil {
			d.onStore(s)
		}
		return s, nil
	}

	bridge, err := toolbridge.Listen(tools)
	if err != nil {
		return err
	}
	defer func() { err = errors.Join(err, bridge.Close()) }()

	limits := buyer.DefaultLimits
	if d.limits != nil {
		limits = *d.limits
	}
	h := &buyer.Host{Registry: reg, Bridge: bridge, CLI: cli, BridgeCmd: []string{d.self, "mcp-bridge"},
		Env: env, Limits: limits, Gate: gate, TraceDir: *traceDir}
	mode := "fake"
	if live {
		mode = "live"
		fmt.Fprintf(d.out, "LIVE MODE: %s runs a real model on your login. Shop %s, Store %s (opened only after the init check), task %s.\n", cli, ln.Addr(), dbPath, task.TaskID)
		if gate.DevDemoPlugins {
			fmt.Fprintln(d.out, "gate: DEV-DEMO plugin exception (2 named plugins accepted; not the strict gate)")
		}
	} else {
		fmt.Fprintf(d.out, "FAKE MODE: %s runs no model. Shop %s, Store %s (opened only after the init check), task %s.\n", filepath.Base(cli), ln.Addr(), dbPath, task.TaskID)
	}
	res, err := h.Run(ctx, task.TaskID, prompt, prepare)
	if err != nil {
		return err
	}
	var view any // no task: the Store was never opened, or the task did not start
	if st != nil {
		if view, err = st.Task(sess); err != nil {
			return err
		}
	}
	expiryMu.Lock()
	report := map[string]any{
		"mode": mode, "outcome": res.Report.Outcome, "detail": res.Report.Detail, "exit_code": res.Report.ExitCode,
		"task_started": res.TaskStarted, "tool_calls": res.Calls, "not_ready_refusals": res.NotReady,
		"store_end": res.StoreEnd, "store_end_error": res.EndError, "expiry_errors": len(expiryErrs),
		"task": view, "trace": res.Trace, "payment": "none; M1 cannot pay and delivers no content",
		"note": "Only the Store's record counts. CLI text, including any claim of payment, is untrusted.",
	}
	expiryMu.Unlock()
	enc := json.NewEncoder(d.out)
	enc.SetIndent("", "  ")
	if err := enc.Encode(report); err != nil {
		return err
	}
	if res.PrepareError != nil {
		return fmt.Errorf("the run stopped before any tool ran: %w", res.PrepareError)
	}
	if res.Report.Outcome != buyer.OK {
		return fmt.Errorf("run ended %s %s", res.Report.Outcome, res.Report.Detail)
	}
	return nil
}

// cmp returns v, or def when v is empty.
func cmp(v, def string) string {
	if v == "" {
		return def
	}
	return v
}

// checkFake refuses a --fake-cli that is not the fake: the real CLI would
// run a model. The check runs under the runner's rules (process group,
// output limits, cleanup), so a slow or noisy executable cannot hold it up.
func checkFake(ctx context.Context, path string, getenv func(string) string) error {
	out, err := buyer.Version(ctx, path, buyer.CLIEnv(getenv), versionLimits)
	if err != nil || !strings.Contains(out, "fake-claude") {
		return fmt.Errorf("--fake-cli %s is not the fake CLI (its --version must name fake-claude); refusing to run it", path)
	}
	return nil
}

// checkLive requires the approved version exactly, under the same rules.
func checkLive(ctx context.Context, path string, env []string, want string) error {
	out, err := buyer.Version(ctx, path, env, versionLimits)
	if err != nil {
		return fmt.Errorf("version check of %s failed (%v); nothing was started", path, err)
	}
	if got := strings.TrimSpace(out); got != want {
		// The CLI's output is not repeated: it is CLI text.
		return fmt.Errorf("%s reports a version other than the approved %q; nothing was started or upgraded", path, want)
	}
	return nil
}

// probe runs the C0 init probe once. It needs APA_LIVE_CLI=true, an
// absolute --cli and the approved version, exactly as a live run; it opens
// no Store and starts no shop or task.
func probe(ctx context.Context, args []string, d deps) error {
	fs := flag.NewFlagSet("probe-init", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	cliPath := fs.String("cli", "", "absolute path of the approved Claude Code executable")
	traceDir := fs.String("trace-dir", "var/runs", "where the probe trace is written")
	oneMessage := fs.Bool("send-one-message", false, "C0′: write one fixed user message (needs its own approval to run)")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if fs.NArg() != 0 {
		return fmt.Errorf("unexpected argument %q", fs.Arg(0))
	}
	if on, perr := strconv.ParseBool(d.getenv("APA_LIVE_CLI")); perr != nil || !on {
		return errors.New("probe-init needs APA_LIVE_CLI=true; nothing was started")
	}
	if !filepath.IsAbs(*cliPath) {
		return errors.New("probe-init needs --cli with the absolute path of the approved Claude Code executable; no PATH lookup is done")
	}
	want := cmp(d.liveVersion, approvedCLIVersion)
	env, err := buyer.LiveEnv(d.getenv)
	if err != nil {
		return err
	}
	if ctx.Err() != nil {
		return errors.New("cancelled before the version check; the probe was not started")
	}
	if err := checkLive(ctx, *cliPath, env, want); err != nil {
		return err
	}
	if ctx.Err() != nil {
		return errors.New("cancelled after the version check; the probe was not started")
	}
	lim := buyer.DefaultLimits
	lim.Total = buyer.ProbeTimeout
	if d.limits != nil {
		lim = *d.limits
	}
	if *oneMessage {
		fmt.Fprintf(d.out, "PROBE MODE (C0′): %s; one fixed user message is written once, no tool runs, no Store is opened.\n", *cliPath)
	} else {
		fmt.Fprintf(d.out, "PROBE MODE (C0): %s; no user message is sent, no tool runs, no Store is opened.\n", *cliPath)
	}
	res, err := buyer.Probe(ctx, buyer.ProbeSpec{CLI: *cliPath, BridgeCmd: []string{d.self, "mcp-bridge"}, Env: env,
		Limits: lim, CLIVersion: strings.Fields(want)[0], TraceDir: *traceDir, SendOneMessage: *oneMessage})
	if err != nil {
		return err
	}
	enc := json.NewEncoder(d.out)
	enc.SetIndent("", "  ")
	if err := enc.Encode(res); err != nil {
		return err
	}
	switch res.Status {
	case buyer.ProbeObserved:
		return nil
	case buyer.ProbeNoInit:
		return fmt.Errorf("probe: %s: %w", res.Code, exitCode(3))
	case buyer.ProbeNoInitAfterInput:
		return fmt.Errorf("probe: %s: %w", res.Code, exitCode(4))
	default:
		return fmt.Errorf("probe: %s: %w", res.Code, exitCode(1))
	}
}

var versionLimits = buyer.Limits{Stdout: 4 << 10, Stderr: 4 << 10, Total: 5 * time.Second, Grace: time.Second}

func decodeFile(path string, dst any) error {
	raw, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	return contracts.Decode(raw, dst)
}
