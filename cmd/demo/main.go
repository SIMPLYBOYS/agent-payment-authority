// Command demo runs the local M1 demo host: health, the fixture data shop
// (catalog and HTTP 402), the Store with its expiry worker, and the
// authenticated read-only task API. With --fake-cli and --plan it also runs
// the fake buyer for each task of a trusted plan. It binds loopback only,
// has no payment capability, and never starts the live Claude Code CLI.
//
//	demo [--fake-cli PATH --plan FILE] [--registry FILE] [--profile FILE] [--trace-dir DIR]
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
	"sync"
	"syscall"
	"time"

	"github.com/SIMPLYBOYS/agent-payment-authority/internal/buyer"
	"github.com/SIMPLYBOYS/agent-payment-authority/internal/config"
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
	getenv      func(string) string
	self        string // this executable, started again as the MCP bridge
	out         io.Writer
	clock       store.Clock
	ready       chan<- string       // receives the bound address once serving
	expiryEvery time.Duration       // 0 means one second
	limits      *buyer.Limits       // nil means buyer.DefaultLimits
	fakeEnv     []string            // extra fake CLI variables (tests: pid and capability files)
	onStore     func(*store.Store)  // after the Store opened, before serving
	runsDone    chan<- []runOutcome // receives the fake runs' outcomes when all ended
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
	if err := fs.Parse(args); err != nil {
		return err
	}
	if fs.NArg() != 0 {
		return fmt.Errorf("unexpected argument %q", fs.Arg(0))
	}
	if (*fakePath == "") != (*planPath == "") {
		return errors.New("--fake-cli and --plan go together")
	}

	cfg, err := config.Load(d.getenv)
	if err != nil {
		return err
	}
	dbPath := cmp(d.getenv("APA_DB_PATH"), "var/apa-demo-fake.db")
	tokenPath := cmp(d.getenv("APA_DEMO_TOKEN_FILE"), "var/demo-tokens.json")
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
	var fake *fakeRunner
	if *fakePath != "" {
		if fake, err = newFakeRunner(ctx, *fakePath, *planPath, *traceDir, reg, profile, d); err != nil {
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
	shop, err := merchant.New("http://" + addr)
	if err != nil {
		return err
	}
	offers := store.OfferBook{}
	products := map[string]toolbridge.Product{}
	for _, o := range shop.Offers() {
		k := store.OfferKey{Method: o.Method, URL: o.URL}
		offers[k] = append(offers[k], o.Quote)
		products[o.ProductID] = toolbridge.Product{ID: o.ProductID, URL: o.URL}
	}

	unlock, err := lockDB(dbPath)
	if err != nil {
		return err
	}
	defer unlock()
	st, err := store.Open(store.Config{Path: dbPath, Registry: regCfg, Offers: offers, Clock: d.clock})
	if err != nil {
		// Recovery failed: nothing is served, repaired or rebuilt.
		return fmt.Errorf("store %s: %w", dbPath, err)
	}
	defer func() { err = errors.Join(err, st.Close()) }() // last: after runs, expiry and the bridge
	if d.onStore != nil {
		d.onStore(st)
	}
	tokens, err := newTokens(regCfg, tokenPath)
	if err != nil {
		return err
	}

	expiryCtx, stopExpiry := context.WithCancel(context.Background())
	var expiry sync.WaitGroup
	every := d.expiryEvery
	if every == 0 {
		every = time.Second
	}
	expiry.Go(func() {
		st.RunExpiry(expiryCtx, every, func(err error) { fmt.Fprintln(d.out, "demo: expiry:", store.Code(err)) })
	})
	defer func() { stopExpiry(); expiry.Wait() }()

	var bridge *toolbridge.Server
	if fake != nil {
		tools := &toolbridge.Shop{Store: st, CatalogURL: "http://" + addr + "/catalog", Products: products, Client: toolbridge.NewClient(), Now: d.clock}
		if bridge, err = toolbridge.Listen(tools); err != nil {
			return err
		}
		defer func() { err = errors.Join(err, bridge.Close()) }()
	}

	mux := newMux(shop)
	(&api{st: st, reg: reg, tokens: tokens, addr: addr, clock: d.clock}).register(mux)
	srv := &http.Server{Handler: mux, ReadHeaderTimeout: 5 * time.Second}
	errc := make(chan error, 1)
	go func() { errc <- srv.Serve(ln) }()
	fmt.Fprintf(d.out, "listening on http://%s (fixture mode, live CLI off, no payment capability); Store %s; demo tokens in %s\n", addr, dbPath, tokenPath)
	if d.ready != nil {
		d.ready <- addr
	}

	runCtx, cancelRuns := context.WithCancel(ctx)
	defer cancelRuns()
	var runs sync.WaitGroup
	if fake != nil {
		runs.Go(func() {
			out := fake.runAll(runCtx, st, bridge)
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
	// still record their end), then expiry, the bridge and the Store (deferred).
	cancelRuns()
	sctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	err = errors.Join(err, ignoreClosed(srv.Shutdown(sctx)))
	runs.Wait()
	return err
}

func ignoreClosed(err error) error {
	if errors.Is(err, http.ErrServerClosed) {
		return nil
	}
	return err
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

func newMux(shop *merchant.Merchant) *http.ServeMux {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /healthz", handleHealth)
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

func handleHealth(w http.ResponseWriter, _ *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(health{
		Status:            "ok",
		Mode:              "fixture",
		LiveCLI:           false,
		ClaudeLogin:       "not_checked",
		PaymentCapability: "unavailable",
	})
}
