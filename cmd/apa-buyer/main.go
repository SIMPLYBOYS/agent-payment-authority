// Command apa-buyer is the local buyer host.
//
//	apa-buyer run --fake-cli PATH --scenario FILE [--task ID]
//	apa-buyer mcp-bridge
//
// run is FAKE MODE ONLY in this build: it starts the fixture shop on a
// loopback port, opens the Store at APA_DB_PATH (default var/apa-fake.db),
// starts the task, and runs the fake CLI against the shop through the MCP
// bridge. No model runs, nothing is paid and no content is delivered. A live
// Claude Code run is not available yet; see docs/cli-poc.md.
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
		os.Exit(1)
	}
}

func wallClock() uint64 { return uint64(time.Now().Unix()) }

// deps are what tests replace.
type deps struct {
	getenv func(string) string
	self   string // this executable, started again as the MCP bridge
	out    io.Writer
	clock  store.Clock
	limits *buyer.Limits
	ready  func(*buyer.Host) // called once the bridge is up
}

// prompt is the host's fixed instruction; the model never edits it.
const prompt = "You are the buyer agent for one task. Use only the apa tools: list the products, get the quote for the Korean market brief, then request its purchase. " +
	"You cannot pay and will not receive the content; the host records the request and a human decides."

func run(ctx context.Context, args []string, d deps) (err error) {
	if len(args) == 0 || args[0] != "run" {
		return errors.New("usage: apa-buyer run --fake-cli PATH --scenario FILE [--task ID]  (or: apa-buyer mcp-bridge)")
	}
	fs := flag.NewFlagSet("run", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	fake := fs.String("fake-cli", "", "fake CLI executable (required: live runs are not enabled in this build)")
	scenario := fs.String("scenario", "", "fake CLI scenario, e.g. testdata/cli/ask-mainline.json")
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
	if *fake == "" {
		return errors.New("live Claude Code runs are not enabled in this build; pass --fake-cli (see docs/cli-poc.md)")
	}
	cli, err := filepath.Abs(*fake)
	if err != nil {
		return err
	}
	if err := checkFake(ctx, cli, d.getenv); err != nil {
		return err
	}
	scen, err := filepath.Abs(*scenario)
	if err != nil || *scenario == "" {
		return errors.New("--scenario is required")
	}
	if _, err := os.Stat(scen); err != nil {
		return err
	}
	dbPath := d.getenv("APA_DB_PATH")
	if dbPath == "" {
		dbPath = "var/apa-fake.db" // fake records stay out of the demo's default var/apa.db
	}

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

	st, err := store.Open(store.Config{Path: dbPath, Registry: regCfg, Offers: offers, Clock: d.clock})
	if err != nil {
		return err
	}
	defer func() { err = errors.Join(err, st.Close()) }()
	// The credential is an M1 fixture made at start, valid for a day of host
	// time; nothing verifies a BBS+ proof.
	cred := policy.Credential{ConsumerID: task.ConsumerID, ValidUntil: d.clock() + 86400, EvidenceMode: "fixture"}
	if ctx.Err() != nil {
		return errors.New("cancelled before the task started; nothing was recorded")
	}
	if err := st.StartTask(store.TaskSetup{TaskID: task.TaskID, Profile: &profile, Credential: &cred}); err != nil {
		if store.Code(err) == "TASK_ALREADY_STARTED" {
			return fmt.Errorf("task %s already exists in %s; nothing was changed or deleted. Run another task (--task task-a-next) or set APA_DB_PATH to a new file", task.TaskID, dbPath)
		}
		return err
	}

	// Expiry runs for the host's lifetime; its errors are reported, not swallowed.
	expiryCtx, stopExpiry := context.WithCancel(context.Background())
	var expiry sync.WaitGroup
	var expiryErrs []string
	var expiryMu sync.Mutex
	expiry.Go(func() {
		st.RunExpiry(expiryCtx, time.Second, func(err error) {
			expiryMu.Lock()
			expiryErrs = append(expiryErrs, store.Code(err))
			expiryMu.Unlock()
			fmt.Fprintln(os.Stderr, "apa-buyer: expiry:", store.Code(err))
		})
	})
	defer func() { stopExpiry(); expiry.Wait() }() // before the Store closes

	bridge, err := toolbridge.Listen(&toolbridge.Shop{Store: st, CatalogURL: "http://" + ln.Addr().String() + "/catalog",
		Products: products, Client: toolbridge.NewClient(), Now: d.clock})
	if err != nil {
		return err
	}
	defer func() { err = errors.Join(err, bridge.Close()) }()

	limits := buyer.DefaultLimits
	limits.Total = 60 * time.Second
	if d.limits != nil {
		limits = *d.limits
	}
	h := &buyer.Host{Store: st, Registry: reg, Bridge: bridge, CLI: cli, BridgeCmd: []string{d.self, "mcp-bridge"},
		Env: buyer.CLIEnv(d.getenv, fakecli.EnvScenario+"="+scen), Limits: limits, TraceDir: *traceDir}
	if d.ready != nil {
		d.ready(h)
	}
	fmt.Fprintf(d.out, "FAKE MODE: %s runs no model. Shop %s, Store %s, task %s.\n", filepath.Base(cli), ln.Addr(), dbPath, task.TaskID)
	res, err := h.Run(ctx, task.TaskID, prompt)
	if err != nil {
		return err
	}
	view, err := st.Task(contracts.Session{ConsumerID: task.ConsumerID, AgentID: task.AgentID, TaskID: task.TaskID})
	if err != nil {
		return err
	}
	expiryMu.Lock()
	report := map[string]any{
		"mode": "fake", "outcome": res.Report.Outcome, "exit_code": res.Report.ExitCode, "tool_calls": res.Calls,
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
	if res.Report.Outcome != buyer.OK {
		return fmt.Errorf("run ended %s", res.Report.Outcome)
	}
	return nil
}

// checkFake refuses a --fake-cli that is not the fake: the real CLI would
// run a model. The check runs under the runner's rules (process group,
// output limits, cleanup), so a slow or noisy executable cannot hold it up.
func checkFake(ctx context.Context, path string, getenv func(string) string) error {
	lim := buyer.Limits{Stdout: 4 << 10, Stderr: 4 << 10, Total: 5 * time.Second, Grace: time.Second}
	out, err := buyer.Version(ctx, path, buyer.CLIEnv(getenv), lim)
	if err != nil || !strings.Contains(out, "fake-claude") {
		return fmt.Errorf("--fake-cli %s is not the fake CLI (its --version must name fake-claude); refusing to run it", path)
	}
	return nil
}

func decodeFile(path string, dst any) error {
	raw, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	return contracts.Decode(raw, dst)
}
