// Command demo runs the local M1 demo host: health, the fixture data shop
// (catalog and HTTP 402), the Store with its expiry worker, and the
// authenticated read-only task API. With --fake-cli and --plan it also runs
// the fake buyer for each task of a trusted plan. It binds loopback only,
// has no payment capability, and never starts the live Claude Code CLI.
//
//	demo [--fake-cli PATH (--plan FILE | --scenarios FILE)] [--registry FILE] [--profile FILE] [--trace-dir DIR] [--upgrade-db]
//	demo --verify-db PATH                     (read-only check of an offline database)
//	demo --restore-copy BACKUP --restore-dir DIR  (a verified restore copy in a new directory)
//	demo mcp-bridge   (started by the fake CLI from the host's MCP config)
//
// docs/demo-integration.md is the design: lifecycle, identity, routes.
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
	"sync"
	"syscall"
	"time"

	"github.com/SIMPLYBOYS/agent-payment-authority/internal/budget"
	"github.com/SIMPLYBOYS/agent-payment-authority/internal/buyer"
	"github.com/SIMPLYBOYS/agent-payment-authority/internal/config"
	"github.com/SIMPLYBOYS/agent-payment-authority/internal/contracts"
	"github.com/SIMPLYBOYS/agent-payment-authority/internal/credential"
	"github.com/SIMPLYBOYS/agent-payment-authority/internal/eth"
	"github.com/SIMPLYBOYS/agent-payment-authority/internal/keytool"
	"github.com/SIMPLYBOYS/agent-payment-authority/internal/merchant"
	"github.com/SIMPLYBOYS/agent-payment-authority/internal/paycontract"
	"github.com/SIMPLYBOYS/agent-payment-authority/internal/policy"
	"github.com/SIMPLYBOYS/agent-payment-authority/internal/reconcile"
	"github.com/SIMPLYBOYS/agent-payment-authority/internal/signerclient"
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
		err = serve(ctx, os.Args[1:], deps{getenv: os.Getenv, self: self, out: os.Stderr, clock: wallClock})
	}
	if err != nil {
		fmt.Fprintln(os.Stderr, "demo:", err)
		os.Exit(1)
	}
}

func wallClock() uint64 { return uint64(time.Now().Unix()) }

// deps are what tests replace.
type deps struct {
	getenv       func(string) string
	self         string // this executable, started again as the MCP bridge
	out          io.Writer
	clock        store.Clock
	ready        chan<- string       // receives the bound address once serving
	expiryEvery  time.Duration       // 0 means one second
	refreshEvery time.Duration       // C2: the status list's refresh; 0 means statusRefresh
	limits       *buyer.Limits       // nil means buyer.DefaultLimits
	fakeEnv      []string            // extra fake CLI variables (tests: pid and capability files)
	onStore      func(*store.Store)  // after the Store opened, before serving
	stage        func(string)        // tests: each stop step, in the order it happens
	runsDone     chan<- []runOutcome // receives the fake runs' outcomes when all ended
	liveVersion  string              // tests: "" means buyer.ApprovedCLIVersion
	pairOut      io.Writer           // --pairing: where the code is shown; nil means /dev/tty
	now          func() time.Time    // the pairing window's clock (monotonic); nil means time.Now
	onApprovals  func(*approvals)    // tests: the A2 relay once made (to wrap its Store)
}

// run serves with no fake runs; the B1-01 tests and checks use it.
func run(ctx context.Context, getenv func(string) string, ready chan<- string) error {
	return serve(ctx, nil, deps{getenv: getenv, out: io.Discard, clock: wallClock, ready: ready})
}

func cmp(v, def string) string {
	if v == "" {
		return def
	}
	return v
}

// serve runs the demo host until ctx ends; docs/demo-integration.md section 1
// is the start and stop order.
func serve(ctx context.Context, args []string, d deps) (err error) {
	fs := flag.NewFlagSet("demo", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	fakePath := fs.String("fake-cli", "", "fake CLI executable; runs the plan's tasks")
	planPath := fs.String("plan", "", "trusted fake run plan, e.g. testdata/demo/fake-plan.json")
	registryPath := fs.String("registry", "testdata/contracts/registry.json", "trusted registry fixture")
	profilePath := fs.String("profile", "testdata/policy/m1-demo-profile.json", "consumer profile fixture")
	traceDir := fs.String("trace-dir", "var/runs", "where fake run traces are written")
	unityDir := fs.String("unity-build", "unity/AgentPaymentAuthority/Build/WebGL/Build", "the Unity Web build's Build directory (served as /unity/)")
	scenariosPath := fs.String("scenarios", "", "trusted scenario catalog for host-started runs, e.g. testdata/demo/scenarios.json")
	upgradeDB := fs.Bool("upgrade-db", false, "migrate a schema 1 to 4 database to 5 (verified, backed up first)")
	verifyDB := fs.String("verify-db", "", "check an offline database read only, then exit")
	restoreCopy := fs.String("restore-copy", "", "backup to restore into --restore-dir, then exit")
	restoreDir := fs.String("restore-dir", "", "the new directory of --restore-copy")
	historyDB := fs.String("history-db", "", "history mode: serve one recorded real run read only (a private copy is opened; nothing runs or expires)")
	historyTracePath := fs.String("history-trace", "", "history mode: the recorded run trace, for its tool calls (optional)")
	liveCLI := fs.String("live-cli", "", "live mode: absolute path of the approved Claude Code executable; a run the operator starts runs it (needs APA_LIVE_CLI=true, APA_DB_PATH and --scenarios with a live catalog; docs/cli-poc.md 55)")
	devDemo := fs.Bool("dev-demo-plugins", false, "live mode only: the development demo plugin exception (docs/live-purchase-execution-decision.md 9)")
	pairingOn := fs.Bool("pairing", false, "with --scenarios: show a one-time code on this terminal; /host trades it once for this run's tokens (docs/cli-poc.md 56)")
	maxRuns := fs.Int("max-runs", 0, "with --scenarios: running or stopping runs at most (a deployment setting; default 2, at most one per consumer)")
	readyFile := fs.String("ready-file", "", "write {addr, pid} here once serving (for scripts/demo/present.sh)")
	approvalsSigner := fs.String("approvals-signer", "", "A2: the Unix socket of apa-signer serve-approvals (approvals only), or of apa-signer serve (P2: approvals and, with --payments, payments); the person's approval then runs on http://localhost:<port> (fake mode with --scenarios only)")
	credentialDir := fs.String("credential-dir", "", "C2: the credential directory (apa-issuer init) for apa.pay.v2 scenarios: test issuer, status list, holders")
	authSource := fs.String("authenticator-source", "", "A2: device or virtual, the authenticator's source as shown and logged (a label, not evidence); required with --approvals-signer (E1-a: it sets the approvals' evidence mode)")
	paymentsPath := fs.String("payments", "", "P2: the payment configuration, a local anvil or the Base Sepolia testnet (docs/commercial-p2-host.md 12.3, docs/commercial-p2b.md B3); with --approvals-signer, whose Signer then signs payments too")
	if err := fs.Parse(args); err != nil {
		return err
	}
	maxRunsSet := false
	fs.Visit(func(f *flag.Flag) { maxRunsSet = maxRunsSet || f.Name == "max-runs" })
	if maxRunsSet && *scenariosPath == "" {
		return errors.New("--max-runs is for a host that starts runs (--scenarios); nothing was started")
	}
	if fs.NArg() != 0 {
		return fmt.Errorf("unexpected argument %q", fs.Arg(0))
	}
	// One-shot database commands (docs/demo-operation-design.md 13.11.2,
	// 13.13, 13.14.1). <db>.lock is cooperative: stop every tool that may
	// write the database first; holding the lock does not prove no other
	// SQLite writer exists.
	if *verifyDB != "" {
		v, err := store.VerifyFile(*verifyDB)
		if err != nil {
			return fmt.Errorf("verify %s: %w", *verifyDB, err)
		}
		fmt.Fprintf(d.out, "demo: %s: schema %d, recovery check passed; nothing was changed\n", *verifyDB, v)
		return nil
	}
	if *restoreCopy != "" || *restoreDir != "" {
		if *restoreCopy == "" || *restoreDir == "" {
			return errors.New("--restore-copy and --restore-dir go together")
		}
		db, v, err := store.RestoreCopy(*restoreCopy, *restoreDir)
		if err != nil {
			return fmt.Errorf("restore copy (not restored): %w", err)
		}
		fmt.Fprintf(d.out, "demo: restore copy %s: schema %d, recovery check passed; point a compatible build's APA_DB_PATH at it\n", db, v)
		return nil
	}
	if *historyTracePath != "" && *historyDB == "" {
		return errors.New("--history-trace goes with --history-db")
	}
	if *historyDB != "" {
		if *fakePath != "" || *planPath != "" || *scenariosPath != "" || *upgradeDB || *liveCLI != "" || *devDemo || *pairingOn {
			return errors.New("--history-db serves a recorded run only; it excludes --fake-cli, --plan, --scenarios, --upgrade-db, --live-cli and --dev-demo-plugins")
		}
		return serveHistory(ctx, *historyDB, *historyTracePath, *registryPath, *unityDir, d)
	}
	if *planPath != "" && *scenariosPath != "" {
		return errors.New("--plan and --scenarios exclude each other")
	}
	// Live mode (docs/cli-poc.md 55) is off unless asked for twice: the
	// --live-cli flag and APA_LIVE_CLI=true. It runs nothing by itself: a
	// run starts only when the operator starts one.
	live := *liveCLI != ""
	switch {
	case *devDemo && !live:
		return errors.New("--dev-demo-plugins is for live mode only (--live-cli)")
	case live && (*fakePath != "" || *planPath != "" || *upgradeDB):
		return errors.New("--live-cli excludes --fake-cli, --plan and --upgrade-db")
	case live && *scenariosPath == "":
		return errors.New("--live-cli needs --scenarios with a live catalog, e.g. testdata/demo/scenarios-live.json")
	case !live && (*fakePath == "") != (*planPath == "" && *scenariosPath == ""):
		return errors.New("--fake-cli goes with --plan or --scenarios")
	case *pairingOn && *scenariosPath == "":
		return errors.New("--pairing is for a host that starts runs (--scenarios)")
	case *approvalsSigner != "" && (*scenariosPath == "" || !live && *fakePath == ""):
		return errors.New("--approvals-signer is for a host that starts runs (--scenarios, with --fake-cli or --live-cli)")
	case live && *approvalsSigner != "" && *paymentsPath == "":
		return errors.New("a live host takes --approvals-signer only to pay (E1-a): with --payments")
	case *paymentsPath != "" && *approvalsSigner == "":
		return errors.New("--payments needs --approvals-signer: that Signer signs the payments")
	case *approvalsSigner != "" && *authSource != "device" && *authSource != "virtual",
		*approvalsSigner == "" && *authSource != "":
		return errors.New("--authenticator-source is device or virtual, given with --approvals-signer")
	}
	// The pairing code goes to the operator's terminal only.
	pairOut := d.pairOut
	if *pairingOn && pairOut == nil {
		tty, err := os.OpenFile("/dev/tty", os.O_WRONLY, 0)
		if err != nil {
			return errors.New("--pairing shows its code on this terminal only, and there is no terminal (/dev/tty); use the token file instead; nothing was started")
		}
		defer tty.Close()
		pairOut = tty
	}
	now := d.now
	if now == nil {
		now = time.Now
	}

	cfg, err := config.Load(d.getenv)
	if err != nil {
		return err
	}
	switch {
	case live && !cfg.LiveCLI:
		return errors.New("--live-cli also needs APA_LIVE_CLI=true; nothing was started")
	case !live && cfg.LiveCLI:
		return errors.New("APA_LIVE_CLI=true is not available to the demo server without --live-cli: it runs the real CLI in live mode only (docs/cli-poc.md 55); unset it or set it to false")
	case live && d.getenv("APA_DB_PATH") == "":
		return errors.New("live mode needs APA_DB_PATH naming its own new demo database (never the fake demo's); nothing was started")
	}
	dbPath := cmp(d.getenv("APA_DB_PATH"), "var/apa-demo-fake.db")
	tokenPath := cmp(d.getenv("APA_DEMO_TOKEN_FILE"), "var/demo-tokens.json")
	// Before any lock, Store or token write: the token file must never
	// replace the database or one of its files.
	if err := checkTokenPath(dbPath, tokenPath); err != nil {
		return err
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
	var scenarios map[string]*scenario
	var effMaxRuns, nConsumers int
	if *scenariosPath != "" {
		if scenarios, err = loadScenarios(*scenariosPath, reg, regCfg, live, live && *paymentsPath != ""); err != nil {
			return err
		}
		if err := checkDelegations(scenarios, profile); err != nil {
			return err
		}
		if effMaxRuns, nConsumers, err = capacity(scenarios, *maxRuns, maxRunsSet); err != nil {
			return err
		}
	}
	// C2: apa.pay.v2 scenarios need the credential directory; its status
	// list is refreshed now (a missing or invalid one stops the start)
	// and then every statusRefresh (C2-D4 (a)).
	var creds *credential.Dir
	if *credentialDir != "" {
		if creds, err = credential.OpenDir(*credentialDir); err != nil {
			return fmt.Errorf("credential directory: %w", err)
		}
		if err := creds.Refresh(int64(d.clock())); err != nil {
			return fmt.Errorf("credential directory: status list: %w", err)
		}
	}
	for _, sc := range scenarios {
		if sc.version() == paycontract.V2 && creds == nil {
			return fmt.Errorf("scenario %s is apa.pay.v2: start with --credential-dir", sc.ID)
		}
	}
	hostStart := time.Now() // the timing records' clock zero; monotonic
	instance, err := newInstanceID()
	if err != nil {
		return err
	}
	var runner *cliRunner
	switch {
	case live:
		if runner, err = newLiveRunner(ctx, *liveCLI, *traceDir, reg, profile, d, *devDemo); err != nil {
			return err
		}
	case *fakePath != "":
		if runner, err = newFakeRunner(ctx, *fakePath, *planPath, *traceDir, reg, profile, d); err != nil {
			return err
		}
	}

	ln, err := net.Listen("tcp", cfg.Addr)
	if err != nil {
		if errors.Is(err, syscall.EADDRINUSE) {
			return fmt.Errorf("%s is already in use by another process; it was left untouched, stop it yourself or change APA_ADDR", cfg.Addr)
		}
		return fmt.Errorf("listen on %s: %w", cfg.Addr, err)
	}
	defer ln.Close() // a no-op once the server owns it
	// Re-check what we actually bound, not just what was configured.
	if tcp, ok := ln.Addr().(*net.TCPAddr); !ok || !tcp.IP.IsLoopback() {
		return fmt.Errorf("refusing to serve on non-loopback address %s", ln.Addr())
	}
	addr := ln.Addr().String()

	// The shop states URLs on the address actually bound, never on the Host
	// header a client sends; the Store's offer book is this configuration.
	var shop *merchant.Merchant
	var pay *payments
	if *paymentsPath != "" {
		if err := paymentSigner(*approvalsSigner); err != nil {
			return err
		}
		if pay, shop, err = openPayments(ctx, *paymentsPath, "http://"+addr, d.clock, shopPassphrase, d.out); err != nil {
			return err
		}
		defer pay.Close() // after the worker (stopped with expiry) and the Store
	} else if shop, err = merchant.New("http://" + addr); err != nil {
		return err
	}
	offers := store.OfferBook{}
	products, scenes := map[string]toolbridge.Product{}, map[string]string{}
	for _, o := range shop.Offers() {
		k := store.OfferKey{Method: o.Method, URL: o.URL}
		offers[k] = append(offers[k], o.Quote)
		products[o.ProductID] = toolbridge.Product{ID: o.ProductID, URL: o.URL, Scene: o.Scene}
		if o.Scene != "" {
			scenes[o.Scene] = o.CatalogURL
		}
	}

	unlock, err := lockDB(dbPath)
	if err != nil {
		return err
	}
	defer unlock()
	st, err := store.Open(store.Config{Path: dbPath, Registry: regCfg, Offers: offers, Clock: d.clock, UpgradeDB: *upgradeDB,
		Credentials: creds, VerifierID: verifierID})
	if err != nil {
		// Recovery failed: nothing is served, repaired or rebuilt.
		return fmt.Errorf("store %s: %w", dbPath, err)
	}
	defer func() { err = errors.Join(err, st.Close()); d.at("store_closed") }() // last: after runs, expiry and the bridge
	if d.onStore != nil {
		d.onStore(st)
	}
	// Recovery R (section 13.3): runs still accepted belong to a previous
	// host process; they are marked interrupted before anything is served.
	if n, err := st.InterruptOpenRuns(); err != nil {
		return fmt.Errorf("store %s: mark runs of a previous host interrupted: %s", dbPath, store.CodeOf(err))
	} else if n > 0 {
		fmt.Fprintf(d.out, "demo: %d run(s) of a previous host marked interrupted\n", n)
	}
	tokens, err := newTokens(regCfg, tokenPath, dbPath)
	if err != nil {
		return err
	}

	expiryCtx, stopExpiry := context.WithCancel(context.Background())
	var expiry sync.WaitGroup
	every := d.expiryEvery
	if every == 0 {
		every = time.Second
	}
	health := &expiryHealth{targetEvery: every.Seconds()}
	expiry.Go(func() {
		st.RunExpiryReporting(expiryCtx, every, func(err error) {
			health.observe(d.clock(), err)
			if err != nil {
				fmt.Fprintln(d.out, "demo: expiry:", store.CodeOf(err)) // a fixed code, never the error's text
			}
		})
	})
	defer func() { stopExpiry(); expiry.Wait(); d.at("expiry_stopped") }()
	if creds != nil {
		refreshEvery := d.refreshEvery
		if refreshEvery == 0 {
			refreshEvery = statusRefresh
		}
		expiry.Go(func() { refreshStatus(expiryCtx, creds, refreshEvery, d) })
	}

	var bridge *toolbridge.Server
	if runner != nil {
		tools := &toolbridge.Shop{Store: st, CatalogURL: "http://" + addr + "/catalog", Scenes: scenes, Products: products, Client: toolbridge.NewClient(), Now: d.clock,
			Credentials: creds}
		runner.creds = creds
		cond := runConditions(live, pay, *authSource)
		if err := errors.Join(paycontract.CheckEvidenceTable(), paycontract.ValidRun(cond)); err != nil {
			return err
		}
		runner.cond = &cond
		if bridge, err = toolbridge.Listen(tools); err != nil {
			return err
		}
		defer func() { err = errors.Join(err, bridge.Close()); d.at("bridge_closed") }()
	}

	runCtx, cancelRuns := context.WithCancel(ctx)
	defer cancelRuns()
	if pay != nil {
		// Stopped with the runs, before the HTTP server (the shop) stops; waited
		// for with expiry, before the Store closes.
		expiry.Go(func() { pay.run(runCtx, st, *approvalsSigner, d.clock) })
	}
	var runs sync.WaitGroup
	capability, payCap := "unavailable", ""
	if pay != nil {
		capability, payCap = pay.cfg.Environment, pay.cfg.Environment
	}
	mux := newMux(shop, capability)
	roles := hostRoles(live, pay != nil)
	ui := &api{st: st, tokens: tokens, addr: addr, clock: d.clock, expiry: health, fake: runner != nil && !live, out: d.out, payCap: payCap}
	if pay != nil {
		ui.payContract = pay.cfg.BudgetContract
		ui.relayer, _ = pay.relayer.(*budget.Client)
		ui.settler = pay.settler
		contract, _ := eth.ParseAddress(pay.cfg.BudgetContract) // both parsed at start (openPayments)
		token, _ := eth.ParseAddress(pay.cfg.Token)
		ui.chainRead = &chainReader{node: pay.rpc, contract: contract, token: token, depth: pay.cfg.Confirmations, kept: map[string]chainTx{}}
	}
	if pay != nil {
		var wallets []eth.Address
		for _, w := range regCfg.Wallets {
			wa, err := eth.ParseAddress(w.From)
			if err != nil {
				return fmt.Errorf("registry wallet %s: %w", w.WalletID, err)
			}
			wallets = append(wallets, wa)
		}
		ui.recon = cachedRecon(func(ctx context.Context) (reconcile.Report, error) {
			return pay.reconciliation(ctx, st, wallets, d.clock())
		})
	}
	var pairCode string
	if *pairingOn {
		if ui.pairing, pairCode, err = newPairing(now); err != nil {
			return err
		}
		mux.HandleFunc("POST /api/host/pair", ui.pair)
	}
	if live {
		ui.live = &liveInfo{Record: "live_demo", Label: roles.label, DevDemoPlugins: *devDemo, CLIVersion: runner.live.want}
		ui.traceDir = *traceDir
		mux.HandleFunc("GET /api/tasks/{task_id}/trace", ui.taskTrace)
	}
	if scenarios != nil {
		ui.control = &control{st: st, runner: runner, bridge: bridge, scenarios: scenarios, ctx: runCtx, wg: &runs, out: d.out,
			active: map[string]*liveRun{}, ended: map[string]*liveRun{}, maxRuns: effMaxRuns, consumers: nConsumers, instance: instance, started: hostStart, profile: profile}
		ui.control.register(mux, ui)
		if roles.sim {
			var ids []string
			for _, c := range regCfg.Consumers {
				ids = append(ids, c.ConsumerID)
			}
			ui.sim = &simulator{consumers: ids, now: now, instance: instance, delegationOnly: roles.delegationOnly}
		}
		ui.registerSim(mux)
		fmt.Fprintf(d.out, "demo: concurrent runs: at most %d (deployment setting --max-runs) of %d consumers in the scenario catalog; host instance %s\n", effMaxRuns, nConsumers, instance)
	}
	if root, err := os.OpenRoot(*unityDir); err == nil {
		ui.unity = root
		defer root.Close()
	} else {
		fmt.Fprintln(d.out, "demo: no Unity Web build at", *unityDir, "- the page will say the scene is not ready")
	}
	if *approvalsSigner != "" {
		_, port, _ := net.SplitHostPort(addr)
		ui.alias = "localhost:" + port
		ui.approve = &approvals{st: st, signer: signerclient.NewApprovals(*approvalsSigner), source: *authSource, agent: roles.agent, out: d.out, payCap: payCap}
		if d.onApprovals != nil {
			d.onApprovals(ui.approve)
		}
		ui.registerApprovals(mux)
		state := "unavailable"
		if ans := ui.approve.signer.Health(); ans.Kind == signerclient.KindOK {
			state = ans.Reply.State
		}
		payState := "payment disabled"
		switch {
		case pay != nil && pay.cfg.Environment == BaseSepolia:
			payState = "payments on the Base Sepolia testnet (chain " + strconv.FormatUint(pay.cfg.ChainID, 10) + ", test USDC, apa-relayer and caps)"
		case pay != nil:
			payState = "payments on local chain " + strconv.FormatUint(pay.cfg.ChainID, 10) + " (P2-a, test keys)"
		}
		fmt.Fprintf(d.out, "demo: human approval (A2) on http://%s through %s (signer %s); authenticator source %s; %s\n", ui.alias, *approvalsSigner, state, *authSource, payState)
	}
	ui.register(mux)
	ui.registerUI(mux)
	srv := &http.Server{Handler: mux, ReadHeaderTimeout: 5 * time.Second}
	errc := make(chan error, 1)
	go func() { errc <- srv.Serve(ln) }()
	payState := "no payment capability"
	if pay != nil {
		payState = "payments on " + pay.cfg.Environment + " (test USDC)"
	}
	if live {
		fmt.Fprintf(d.out, "listening on http://%s (LIVE MODE: %s; a run starts only when the operator starts one, and then runs %s on the APA-only config directory, which may use your own subscription; dev demo plugin exception %t; %s); Store %s; demo tokens in %s\n",
			addr, roles.label, *liveCLI, *devDemo, payState, dbPath, tokenPath)
	} else {
		fmt.Fprintf(d.out, "listening on http://%s (fixture mode, live CLI off, %s); Store %s; demo tokens in %s\n", addr, payState, dbPath, tokenPath) // review of C3: a host that pays said it did not
	}
	if *pairingOn {
		if err := showPairingCode(pairOut, pairCode, addr); err != nil {
			return errors.New("the pairing code could not be shown on the terminal; nothing was started")
		}
	}
	if *readyFile != "" {
		if err := writeReady(*readyFile, addr); err != nil {
			return fmt.Errorf("ready file: %w", err)
		}
	}
	if d.ready != nil {
		d.ready <- addr
	}

	if runner != nil && *planPath != "" {
		runs.Go(func() {
			out := runner.runAll(runCtx, st, bridge)
			if d.runsDone != nil {
				d.runsDone <- out
			}
		})
	}

	select {
	case err = <-errc:
	case <-ctx.Done():
	}
	// Stop order: no new runs, no new requests, wait for the runs (they may
	// still record their end), then the bridge, expiry and the Store (deferred,
	// so they run in that order on every return after they started).
	cancelRuns()
	sctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	err = errors.Join(err, ignoreClosed(srv.Shutdown(sctx)))
	d.at("http_stopped")
	runs.Wait()
	d.at("runs_ended")
	return err
}

func (d deps) at(stage string) {
	if d.stage != nil {
		d.stage(stage)
	}
}

func ignoreClosed(err error) error {
	if errors.Is(err, http.ErrServerClosed) {
		return nil
	}
	return err
}

var errTokenPath = errors.New("APA_DEMO_TOKEN_FILE must not name the database or one of its files (-wal, -shm, .lock); nothing was started or changed")

var errTokenInput = errors.New("APA_DEMO_TOKEN_FILE must not name an input file of the demo (the history trace); nothing was started or changed")

// dbFiles are the database's own files: the database, its WAL and
// shared-memory files, and the demo's lock.
func dbFiles(db string) []string { return []string{db, db + "-wal", db + "-shm", db + ".lock"} }

// canonicalPath is p made absolute and clean, with its nearest existing
// ancestor resolved through symlinks, so a relative and an absolute
// spelling, "." and ".." steps, or a symlinked parent name one place.
func canonicalPath(p string) (string, error) {
	abs, err := filepath.Abs(p)
	if err != nil {
		return "", err
	}
	dir, rest := abs, ""
	for {
		if real, err := filepath.EvalSymlinks(dir); err == nil {
			return filepath.Join(real, rest), nil
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			return abs, nil
		}
		rest, dir = filepath.Join(filepath.Base(dir), rest), parent
	}
}

// checkTokenPath refuses a token path that names one of the database's
// files, by canonical path and, for files that exist, by identity (which
// also catches a spelling a case-insensitive file system treats as the
// same name). ponytail: a configuration check, not a guard against another
// local user racing renames; newTokens repeats it before its rename.
func checkTokenPath(db, token string, inputs ...string) error {
	t, err := canonicalPath(token)
	if err != nil {
		return err
	}
	ti, terr := os.Stat(t)
	files := dbFiles(db)
	for i, f := range append(files, inputs...) {
		c, err := canonicalPath(f)
		if err != nil {
			return err
		}
		same := c == t
		if fi, err := os.Stat(c); err == nil && terr == nil && os.SameFile(fi, ti) {
			same = true
		}
		if same && i < len(files) {
			return errTokenPath
		}
		if same {
			return errTokenInput
		}
	}
	return nil
}

// lockDB keeps a second demo process off the same database: each would run
// its own expiry worker, unaware of the other.
func lockDB(dbPath string) (func(), error) {
	abs, err := filepath.Abs(dbPath)
	if err != nil {
		return nil, err
	}
	if err := os.MkdirAll(filepath.Dir(abs), 0o700); err != nil {
		return nil, err
	}
	f, err := os.OpenFile(abs+".lock", os.O_RDWR|os.O_CREATE, 0o600)
	if err != nil {
		return nil, err
	}
	if err := syscall.Flock(int(f.Fd()), syscall.LOCK_EX|syscall.LOCK_NB); err != nil {
		f.Close()
		return nil, fmt.Errorf("database %s is in use by another demo process; nothing was started", abs)
	}
	return func() { f.Close() }, nil // closing releases the lock
}

// LocalAnvil is the payment capability with --payments on a local anvil
// (P2-a): test keys and test USDC. BaseSepolia (payments.go) is the testnet's.
const LocalAnvil = "local_anvil"

// shopPassphrase asks the shop key's passphrase on this terminal (P2b-D4).
func shopPassphrase() ([]byte, error) {
	tty, err := os.OpenFile("/dev/tty", os.O_RDWR, 0)
	if err != nil {
		return nil, errors.New("it is read from a terminal (/dev/tty), and there is none")
	}
	defer tty.Close()
	return keytool.ReadPassphrase(tty, "demo", "shop key passphrase: ")
}

// newMux serves /healthz and the shop; capability is unavailable or
// LocalAnvil.
func newMux(shop *merchant.Merchant, capability string) *http.ServeMux {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /healthz", func(w http.ResponseWriter, r *http.Request) { handleHealth(w, r, capability) })
	shop.Register(mux)
	return mux
}

func decodeFile(path string, dst any) error {
	raw, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	return contracts.Decode(raw, dst)
}

// health reports that the process is up, and states outright what that does
// not imply: no Claude login check has run and nothing here can pay.
type health struct {
	Status            string `json:"status"`
	Mode              string `json:"mode"`
	LiveCLI           bool   `json:"live_cli"`
	ClaudeLogin       string `json:"claude_login"`
	PaymentCapability string `json:"payment_capability"`
}

func handleHealth(w http.ResponseWriter, _ *http.Request, capability string) {
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(health{
		Status:            "ok",
		Mode:              "fixture",
		LiveCLI:           false,
		ClaudeLogin:       "not_checked",
		PaymentCapability: capability,
	})
}

// writeReady tells a launcher this process serves: its address and pid,
// published whole (scripts/demo/present.sh opens the page only then). It
// never replaces or follows anything already there (review R1, docs/cli-poc.md
// 56.8): the JSON goes into a new temporary file of its own (CreateTemp:
// a random name, created exclusively), then appears at path by a hard link,
// which fails if path exists in any form (file, directory or symlink, not
// followed). Only that temporary file is removed; on a conflict nothing else
// is touched and the start stops.
func writeReady(path, addr string) error {
	body, _ := json.Marshal(map[string]any{"addr": addr, "pid": os.Getpid()})
	tmp, err := os.CreateTemp(filepath.Dir(path), ".ready-*")
	if err != nil {
		return err
	}
	defer os.Remove(tmp.Name()) // this call's own file only; path keeps the link
	if _, err := tmp.Write(body); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	if err := os.Link(tmp.Name(), path); err != nil {
		return fmt.Errorf("%s already exists or cannot be made; nothing was replaced (%w)", path, err)
	}
	return nil
}

// runConditions are what this host's pay tasks' events come from (E1-a,
// E1a-D4): the Agent, the payments' chain (none without payments), the
// authenticator declared with the Signer ("" = none).
func runConditions(live bool, pay *payments, authenticator string) paycontract.RunConditions {
	r := paycontract.RunConditions{Agent: "fake", Chain: "none", Authenticator: cmp(authenticator, "none")}
	if live {
		r.Agent = "live"
	}
	if pay != nil {
		r.Chain = pay.cfg.Environment
	}
	return r
}

// roles are what a host's mode allows (E1-a): the live label, the Agent the
// approvals name, and the simulated customer: in fake mode for delegations
// and approvals; on a live host that pays (E1a-D1 A) for delegations only;
// on any other live host none (simulation_unavailable).
type roles struct {
	label, agent        string
	sim, delegationOnly bool
}

func hostRoles(live, paying bool) roles {
	switch {
	case !live:
		return roles{label: "", agent: "fake", sim: true}
	case paying:
		return roles{label: liveLabelPaying, agent: "live", sim: true, delegationOnly: true}
	}
	return roles{label: liveLabel, agent: "live"}
}
