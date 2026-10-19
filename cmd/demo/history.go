package main

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"regexp"
	"slices"
	"syscall"
	"time"

	"github.com/SIMPLYBOYS/agent-payment-authority/internal/config"
	"github.com/SIMPLYBOYS/agent-payment-authority/internal/contracts"
	"github.com/SIMPLYBOYS/agent-payment-authority/internal/merchant"
	"github.com/SIMPLYBOYS/agent-payment-authority/internal/store"
	"github.com/SIMPLYBOYS/agent-payment-authority/internal/toolbridge"
)

// History mode (docs/cli-poc.md 54): the demo host serves one recorded real
// run, read only, for a historical replay. It opens a private copy of the
// recorded Store (store.OpenSnapshot), runs no CLI, no expiry and no
// control, and freezes its clock at the last recorded event, so nothing is
// judged against today's time.

// historyLabel is the fixed label the pages show in history mode.
const historyLabel = "真實執行紀錄・歷史回放・未付款・開發展示例外"

// historyInfo goes with /api/viewer and /api/host/status in history mode.
type historyInfo struct {
	Record string        `json:"record"` // always "live_history"
	Label  string        `json:"label"`
	AsOf   uint64        `json:"as_of"` // the frozen clock: the last recorded event
	Trace  *historyTrace `json:"trace"` // nil without --history-trace
}

// historyTrace is the whitelisted part of the recorded run trace: no
// prompt, model text, path or token. Usage is the CLI's own counts, read
// for live mode only (nil: unknown).
type historyTrace struct {
	TaskID    string            `json:"task_id"`
	GateMode  string            `json:"gate_mode"`
	Gate      string            `json:"gate"`
	Outcome   string            `json:"outcome"`
	ToolCalls []historyCall     `json:"tool_calls"`
	Usage     map[string]uint64 `json:"usage,omitempty"`
}

// usageKeys are the counts a trace's run_end may carry (buyer.usageCounts).
var usageKeys = []string{"input_tokens", "output_tokens", "cache_creation_input_tokens", "cache_read_input_tokens"}

// historyCall is one recorded tool call and, for request_purchase, the
// Store purchase and events it led to.
type historyCall struct {
	N               int      `json:"n"` // its order in the trace, from 1
	Tool            string   `json:"tool"`
	ProductID       string   `json:"product_id"`
	Outcome         string   `json:"outcome"`
	PurchaseOrderID string   `json:"purchase_order_id,omitempty"`
	EventSequences  []uint64 `json:"event_sequences,omitempty"`
}

const maxTraceBytes = 1 << 20

var (
	errTrace         = errors.New("history trace is not a recorded run trace this host reads (a fixed refusal; the file is not quoted)")
	errTraceDisagree = errors.New("history trace and history database disagree on a request_purchase verdict; refused, neither was changed")
	traceWord        = regexp.MustCompile(`^[A-Za-z0-9_-]{0,64}$`)
)

// loadHistoryTrace reads the recorded run trace and maps its tool calls to
// the snapshot's purchases by the purchase order ID rule of the tools.
func loadHistoryTrace(path string, st *store.Store) (*historyTrace, error) {
	return loadTrace(path, st, false)
}

// loadTrace is loadHistoryTrace, with the run_end's usage counts when
// usage is set (live mode).
func loadTrace(path string, st *store.Store, usage bool) (*historyTrace, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	raw, err := io.ReadAll(io.LimitReader(f, maxTraceBytes+1))
	if err != nil {
		return nil, err
	}
	if len(raw) > maxTraceBytes {
		return nil, errTrace
	}
	word := func(m map[string]json.RawMessage, k string) (string, bool) {
		var v string
		if json.Unmarshal(m[k], &v) != nil || !traceWord.MatchString(v) {
			return "", false
		}
		return v, true
	}
	t := &historyTrace{ToolCalls: []historyCall{}}
	var starts, inits, ends int
	sc := bufio.NewScanner(bytes.NewReader(raw))
	sc.Buffer(make([]byte, 64<<10), maxTraceBytes)
	for sc.Scan() {
		var m map[string]json.RawMessage
		if json.Unmarshal(sc.Bytes(), &m) != nil {
			return nil, errTrace
		}
		ev, ok := word(m, "event")
		if !ok {
			return nil, errTrace
		}
		switch ev {
		case "run_start":
			starts++
			if t.TaskID, ok = word(m, "task_id"); !ok || t.TaskID == "" {
				return nil, errTrace
			}
			if t.GateMode, ok = word(m, "gate_mode"); !ok || (t.GateMode != "strict" && t.GateMode != "dev_demo_plugins") {
				return nil, errTrace
			}
		case "init":
			inits++
			if t.Gate, ok = word(m, "gate"); !ok {
				return nil, errTrace
			}
		case "tool_call":
			c := historyCall{N: len(t.ToolCalls) + 1}
			var ok1, ok2, ok3 bool
			c.Tool, ok1 = word(m, "tool")
			c.ProductID, ok2 = word(m, "product_id")
			c.Outcome, ok3 = word(m, "outcome")
			if !ok1 || !ok2 || !ok3 || !slices.ContainsFunc(toolbridge.Tools, func(x toolbridge.Tool) bool { return x.Name == c.Tool }) {
				return nil, errTrace
			}
			t.ToolCalls = append(t.ToolCalls, c)
		case "run_end":
			ends++
			if t.Outcome, ok = word(m, "outcome"); !ok {
				return nil, errTrace
			}
			var u map[string]json.RawMessage
			if usage && json.Unmarshal(m["usage"], &u) == nil { // "unknown" or absent: no counts
				for _, k := range usageKeys {
					var n uint64
					if v, ok := u[k]; ok && json.Unmarshal(v, &n) == nil {
						if t.Usage == nil {
							t.Usage = map[string]uint64{}
						}
						t.Usage[k] = n
					}
				}
			}
		}
	}
	if sc.Err() != nil || starts != 1 || inits != 1 || ends != 1 {
		return nil, errTrace
	}
	rec, err := st.ReadTask(t.TaskID, func(string) bool { return true })
	if err != nil {
		return nil, errors.New("history trace names a task the history database does not hold")
	}
	for i, c := range t.ToolCalls {
		if c.Tool != "request_purchase" || c.ProductID == "" {
			continue
		}
		// A verdict in the trace must be the stored purchase's verdict: a
		// trace and a Store that disagree are refused, never joined. A call
		// that ended in an error code is shown without a purchase.
		po := toolbridge.PurchaseOrderID(t.TaskID, c.ProductID)
		at := slices.IndexFunc(rec.Task.Purchases, func(p store.PurchaseView) bool { return p.PurchaseOrderID == po })
		verdict := c.Outcome == "Ask" || c.Outcome == "Deny" || c.Outcome == "Allow"
		if verdict && (at < 0 || rec.Task.Purchases[at].Decision.PolicyVerdict != c.Outcome) {
			return nil, errTraceDisagree
		}
		if !verdict {
			continue
		}
		t.ToolCalls[i].PurchaseOrderID = po
		for _, e := range rec.Events {
			if e.Scope != nil && e.Scope.PurchaseOrderID == po {
				t.ToolCalls[i].EventSequences = append(t.ToolCalls[i].EventSequences, e.Sequence)
			}
		}
	}
	return t, nil
}

// serveHistory is the demo host in history mode until ctx ends: the same
// loopback address, tokens, pages and read APIs, over a snapshot of the
// recorded Store; no lock, Store.Open, run recovery, expiry worker, bridge
// or control ever touches the recorded file.
func serveHistory(ctx context.Context, dbPath, tracePath, registryPath, unityDir string, d deps) (err error) {
	cfg, err := config.Load(d.getenv)
	if err != nil {
		return err
	}
	if cfg.LiveCLI {
		return errors.New("APA_LIVE_CLI=true is not for history mode: it runs no CLI; unset it or set it to false")
	}
	tokenPath := cmp(d.getenv("APA_DEMO_TOKEN_FILE"), "var/demo-tokens.json")
	// The token file is written at start: it may name neither the recorded
	// database's files nor the trace (checked here and again just before the
	// write, in newTokens).
	var inputs []string
	if tracePath != "" {
		inputs = append(inputs, tracePath)
	}
	if err := checkTokenPath(dbPath, tokenPath, inputs...); err != nil {
		return err
	}
	var regCfg contracts.RegistryConfig
	if err := decodeFile(registryPath, &regCfg); err != nil {
		return fmt.Errorf("registry: %w", err)
	}
	st, done, err := store.OpenSnapshot(dbPath)
	if err != nil {
		return fmt.Errorf("history database %s: %w", dbPath, err)
	}
	defer func() { err = errors.Join(err, done()); d.at("store_closed") }()
	asOf, err := st.LastObservedAt()
	if err != nil || asOf == 0 {
		return fmt.Errorf("history database %s holds no recorded event", dbPath)
	}
	info := &historyInfo{Record: "live_history", Label: historyLabel, AsOf: asOf}
	if tracePath != "" {
		if info.Trace, err = loadHistoryTrace(tracePath, st); err != nil {
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
	defer ln.Close()
	if tcp, ok := ln.Addr().(*net.TCPAddr); !ok || !tcp.IP.IsLoopback() {
		return fmt.Errorf("refusing to serve on non-loopback address %s", ln.Addr())
	}
	addr := ln.Addr().String()
	shop, err := merchant.New("http://" + addr)
	if err != nil {
		return err
	}
	tokens, err := newTokens(regCfg, tokenPath, dbPath, inputs...)
	if err != nil {
		return err
	}
	mux := newMux(shop, "unavailable")
	frozen := func() uint64 { return asOf }
	ui := &api{st: st, tokens: tokens, addr: addr, clock: frozen, expiry: &expiryHealth{}, history: info}
	if root, err := os.OpenRoot(unityDir); err == nil {
		ui.unity = root
		defer root.Close()
	} else {
		fmt.Fprintln(d.out, "demo: no Unity Web build at", unityDir, "- the page will say the scene is not ready")
	}
	ui.register(mux)
	ui.registerUI(mux)
	registerReadOnly(mux, ui)
	srv := &http.Server{Handler: mux, ReadHeaderTimeout: 5 * time.Second}
	errc := make(chan error, 1)
	go func() { errc <- srv.Serve(ln) }()
	fmt.Fprintf(d.out, "listening on http://%s (HISTORY MODE: %s; a read-only copy of %s, clock frozen at %d; no CLI, no expiry, no payment); demo tokens in %s\n",
		addr, historyLabel, dbPath, asOf, tokenPath)
	if d.ready != nil {
		d.ready <- addr
	}
	select {
	case err = <-errc:
	case <-ctx.Done():
	}
	sctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	err = errors.Join(err, ignoreClosed(srv.Shutdown(sctx)))
	d.at("http_stopped")
	return err
}

// historyFor is the history record a caller is shown: the trace only to a
// caller who may read its task (the host, or that task's consumer's
// viewer); every other caller gets the label and the record time alone.
func (a *api) historyFor(g grant) *historyInfo {
	h := a.history
	if h == nil || h.Trace == nil {
		return h
	}
	if _, err := a.st.ReadTask(h.Trace.TaskID, g.sees); err != nil {
		c := *h
		c.Trace = nil
		return &c
	}
	return h
}

// registerReadOnly answers the control routes in history mode: the server
// refuses every start, stop and cancel (the page hides them too, but that
// is not the guard), and there is no run to list.
func registerReadOnly(mux *http.ServeMux, a *api) {
	mux.HandleFunc("GET /api/host/runs", func(w http.ResponseWriter, r *http.Request) {
		if reader(w, r, a) {
			reply(w, http.StatusOK, map[string]any{"runs": []runView{}})
		}
	})
	mux.HandleFunc("GET /api/host/runs/{run_id}", func(w http.ResponseWriter, r *http.Request) {
		if reader(w, r, a) {
			fail(w, http.StatusNotFound, "not_found")
		}
	})
	for _, route := range []string{"POST /api/host/runs", "POST /api/host/runs/{run_id}/stop", "POST /api/host/tasks/{task_id}/cancel"} {
		mux.HandleFunc(route, func(w http.ResponseWriter, r *http.Request) {
			if _, ok := a.authorize(w, r); ok {
				fail(w, http.StatusConflict, "history_read_only")
			}
		})
	}
}
