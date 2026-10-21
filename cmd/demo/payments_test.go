package main

// --payments' checks before anything is opened: a local chain only, four
// different anvil keys named by index, a private directory, the chain the
// file names. No chain runs here (the RPC is a closed local port).

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/SIMPLYBOYS/agent-payment-authority/internal/budget"
	"github.com/SIMPLYBOYS/agent-payment-authority/internal/contracts"
	"github.com/SIMPLYBOYS/agent-payment-authority/internal/keytool"
	"github.com/SIMPLYBOYS/agent-payment-authority/internal/merchant"
	"github.com/SIMPLYBOYS/agent-payment-authority/internal/paycontract"
	"github.com/SIMPLYBOYS/agent-payment-authority/internal/policy"
	"github.com/SIMPLYBOYS/agent-payment-authority/internal/reconcile"
	"github.com/SIMPLYBOYS/agent-payment-authority/internal/store"
	"github.com/SIMPLYBOYS/agent-payment-authority/internal/x402"
)

func TestPaymentsConfigRefused(t *testing.T) {
	dir := t.TempDir()
	priv := filepath.Join(dir, "p2")
	os.Mkdir(priv, 0o700)
	good := func() map[string]any {
		return map[string]any{"rpc": "http://127.0.0.1:1", "chain_id": 84532, "confirmations": 1, "token": "0x" + strings.Repeat("1", 40),
			"token_name": "USDC", "token_version": "2", "token_from_block": 0, "pay_to": "0x" + strings.Repeat("3", 40), "budget_contract": "0x" + strings.Repeat("4", 40),
			"policy_ttl": 600, "zkbudget": "zkbudget", "zk_setup_dir": "zk", "dir": priv,
			"anvil_keys":  map[string]int{"relayer_gas": 3, "issuer": 1, "policy": 2, "shop_gas": 4},
			"delegations": map[string]string{"consumer-a": "dlg-a@1"}}
	}
	path := filepath.Join(dir, "payments.json")
	for name, cs := range map[string]struct {
		change func(map[string]any)
		want   string
	}{
		"a public RPC": {func(c map[string]any) { c["rpc"] = "https://sepolia.base.org" }, "local anvil"},
		"a LAN RPC":    {func(c map[string]any) { c["rpc"] = "http://192.168.1.2:8545" }, "loopback"},
		"one key twice": {func(c map[string]any) {
			c["anvil_keys"] = map[string]int{"relayer_gas": 1, "issuer": 1, "policy": 2, "shop_gas": 4}
		}, "four different"},
		"a key past anvil's": {func(c map[string]any) {
			c["anvil_keys"] = map[string]int{"relayer_gas": 9, "issuer": 1, "policy": 2, "shop_gas": 4}
		}, "anvil key 9"},
		"a key in the file":            {func(c map[string]any) { c["gas_key"] = strings.Repeat("ab", 32) }, "unknown field gas_key"},
		"no delegations":               {func(c map[string]any) { delete(c, "delegations") }, "delegations"},
		"a shared directory":           {func(c map[string]any) { os.Chmod(priv, 0o750) }, "0700"},
		"no chain at the RPC":          {func(c map[string]any) {}, "did not answer"},
		"test402 on a local anvil":     {func(c map[string]any) { c["test402_offer"] = "/x/test402-offer.json" }, "base_sepolia's only"},
		"no apa-relayer on its socket": {func(c map[string]any) { c["relayer_socket"] = filepath.Join(priv, "none.sock") }, "apa-relayer on"},
	} {
		os.Chmod(priv, 0o700)
		c := good()
		cs.change(c)
		raw, _ := json.Marshal(c)
		os.WriteFile(path, raw, 0o600)
		if _, _, err := openPayments(t.Context(), path, "http://127.0.0.1:2", wallClock, noPass, io.Discard); err == nil || !strings.Contains(err.Error(), cs.want) {
			t.Errorf("%s: %v, want %q", name, err, cs.want)
		}
	}
}

func TestPaymentsNeedTheSigner(t *testing.T) {
	err := serve(t.Context(), []string{"--payments", "x.json"}, deps{getenv: func(string) string { return "" }, out: io.Discard, clock: wallClock})
	if err == nil || !strings.Contains(err.Error(), "--approvals-signer") {
		t.Fatal(err)
	}
}

// --payments needs a Signer whose health is ready (it signs payments); an
// approvals-only one, or none, stops the start.
func TestPaymentsNeedAReadySigner(t *testing.T) {
	dir, err := os.MkdirTemp("", "ps")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(dir)
	sock := filepath.Join(dir, "s")
	if err := paymentSigner(sock); err == nil {
		t.Fatal("no Signer was accepted")
	}
	ln, err := net.Listen("unix", sock)
	if err != nil {
		t.Skip("no Unix socket listening here:", err)
	}
	state := "approvals-only"
	srv := &http.Server{Handler: http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		fmt.Fprintf(w, `{"state":%q}`, state)
	})}
	go srv.Serve(ln)
	defer srv.Close()
	if err := paymentSigner(sock); err == nil || !strings.Contains(err.Error(), "approvals-only") {
		t.Fatalf("approvals only: %v", err)
	}
	state = "ready"
	if err := paymentSigner(sock); err != nil {
		t.Fatal(err)
	}
}

// With --payments the host says local_anvil (health, status, a pay task,
// the approvals list); without it, as before.
func TestPaymentCapabilityStated(t *testing.T) {
	shop, err := merchant.New("http://127.0.0.1:1")
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"unavailable", LocalAnvil} {
		srv := httptest.NewServer(newMux(shop, want))
		res, err := http.Get(srv.URL + "/healthz")
		if err != nil {
			t.Fatal(err)
		}
		var h health
		json.NewDecoder(res.Body).Decode(&h)
		res.Body.Close()
		srv.Close()
		if h.PaymentCapability != want {
			t.Errorf("healthz %q, want %q", h.PaymentCapability, want)
		}
	}
	on, off := &api{payCap: LocalAnvil}, &api{}
	if on.capability() != LocalAnvil || off.capability() != "unavailable" || on.approvalsCapability() != LocalAnvil || off.approvalsCapability() != "not_enabled" {
		t.Fatal("capability")
	}
}

// GET /api/host/reconciliation: the host's (or operator's) only; 404
// payments_off without --payments; the report as the Reconciler made it;
// 503 when the chain does not answer.
func TestReconciliationRoute(t *testing.T) {
	a := &api{addr: "127.0.0.1:8402", out: io.Discard, tokens: []demoToken{{grant{Role: "host"}, "tok-host"}, {grant{Role: "operator"}, "tok-operator"},
		{grant{Role: "viewer", ConsumerID: "consumer-a"}, "tok-viewer"}}}
	get := func(tok string) (int, map[string]any) {
		req := httptest.NewRequest("GET", "http://127.0.0.1:8402/api/host/reconciliation", nil)
		req.Header.Set("Authorization", "Bearer "+tok)
		rec := httptest.NewRecorder()
		a.reconciliation(rec, req)
		var m map[string]any
		json.NewDecoder(rec.Body).Decode(&m)
		return rec.Code, m
	}
	if code, m := get("tok-host"); code != 404 || m["error"] != "payments_off" {
		t.Fatalf("without --payments: %d %v", code, m)
	}
	var fail error
	a.recon = func(context.Context) (reconcile.Report, error) {
		return reconcile.Report{Version: reconcile.ReportVersion, Environment: "local_anvil", Known: 2, Unauthorized: []reconcile.SpendView{}}, fail
	}
	if code, m := get("tok-viewer"); code != 403 {
		t.Fatalf("a viewer: %d %v", code, m)
	}
	for _, tok := range []string{"tok-host", "tok-operator"} {
		if code, m := get(tok); code != 200 || m["environment"] != "local_anvil" || m["known_authorizations"] != float64(2) {
			t.Fatalf("%s: %d %v", tok, code, m)
		}
	}
	fail = fmt.Errorf("%w: a row", errReconcileStore)
	if code, m := get("tok-host"); code != 503 || m["error"] != "reconcile_failed" {
		t.Fatalf("an unreadable authorization: %d %v", code, m)
	}
	fail = errors.New("node down")
	if code, m := get("tok-host"); code != 503 || m["error"] != "chain_unavailable" {
		t.Fatalf("no chain: %d %v", code, m)
	}
	fail = errReconcilePending
	if code, m := get("tok-host"); code != 503 || m["error"] != "reconciliation_pending" {
		t.Fatalf("still reading: %d %v", code, m)
	}
}

// One reconciliation at a time, not cancelled by the asker, answered again
// for reconEvery: "pending" until the first is read.
func TestCachedReconciliation(t *testing.T) {
	var reads atomic.Int32
	release := make(chan struct{})
	r := cachedRecon(func(ctx context.Context) (reconcile.Report, error) {
		reads.Add(1)
		<-release
		return reconcile.Report{Known: 3}, ctx.Err()
	})
	ctx, cancel := context.WithCancel(context.Background())
	cancel() // the asker gave up at once: the read goes on
	for range 5 {
		if _, err := r(ctx); !errors.Is(err, errReconcilePending) {
			t.Fatalf("before the first read: %v", err)
		}
	}
	close(release)
	var rep reconcile.Report
	var err error
	for end := time.Now().Add(5 * time.Second); time.Now().Before(end); time.Sleep(10 * time.Millisecond) {
		if rep, err = r(context.Background()); !errors.Is(err, errReconcilePending) {
			break
		}
	}
	if err != nil || rep.Known != 3 {
		t.Fatalf("the read: %+v %v", rep, err)
	}
	r(context.Background())
	time.Sleep(200 * time.Millisecond) // a second read, if one were started, has begun by now
	if n := reads.Load(); n != 1 {
		t.Fatalf("%d reads within a minute", n)
	}
}

// The P2-a catalog (make present-p2) loads under the demo registry and the
// profile: two scenes per customer, the cafe's lunch an Allow (0.08 within
// allow_max 0.10), the brief an Ask.
func TestP2ShippedCatalog(t *testing.T) {
	var regCfg contracts.RegistryConfig
	if err := decodeFile("testdata/demo/registry.json", &regCfg); err != nil {
		t.Fatal(err)
	}
	reg, err := contracts.NewRegistry(regCfg)
	if err != nil {
		t.Fatal(err)
	}
	scs, err := loadScenarios("testdata/demo/scenarios-p2.json", reg, regCfg, false, false)
	if err != nil || len(scs) != 4 || scs["cafe-a"].Scene != "cafe" || scs["data-b"].Scene != "" || scs["cafe-b"].version() != paycontract.Version {
		t.Fatalf("catalog: %v %v", scs, err)
	}
	// The shop's prices against the delegations' allow_max: the cafe's lunch is an Allow, the brief an Ask.
	shop, _ := merchant.New("http://127.0.0.1:1")
	price := map[string]uint64{}
	for _, o := range shop.Offers() {
		var req struct{ Amount contracts.Amount }
		if err := json.Unmarshal(o.Quote.SelectedRequirement, &req); err != nil {
			t.Fatal(err)
		}
		if price[o.Scene], err = req.Amount.Units(); err != nil {
			t.Fatal(err)
		}
	}
	for id, sc := range scs {
		allow, err := sc.Delegation.AllowMax.Units()
		if _, ok := price[sc.Scene]; !ok || err != nil {
			t.Fatalf("%s: no offer for scene %q or allow_max: %v", id, sc.Scene, err)
		}
		if want := map[string]bool{"cafe": true, "": false}[sc.Scene]; (price[sc.Scene] <= allow) != want {
			t.Fatalf("%s: price %d, allow_max %d", id, price[sc.Scene], allow)
		}
	}
	raw, _ := os.ReadFile("testdata/policy/m1-demo-profile.json")
	if p, err := policy.LoadProfile(raw); err != nil || checkDelegations(scs, p) != nil {
		t.Fatalf("profile: %v", err)
	}
}

func noPass() ([]byte, error) { return nil, errors.New("no terminal in a test") }

// testnetConfig is a base_sepolia payments.json that passes the file's
// rules (docs/commercial-p2b.md B3).
func testnetConfig(priv, sock, keys, rpc string) map[string]any {
	return map[string]any{"environment": "base_sepolia", "rpc": rpc, "chain_id": 84532, "confirmations": 2,
		"token": "0x036cbd53842c5426634e7929541ec2318f3dcf7e", "token_name": "USDC", "token_version": "2", "token_from_block": 100,
		"pay_to": "0x" + strings.Repeat("3", 40), "budget_contract": "0x" + strings.Repeat("4", 40), "policy_ttl": 600, "zkbudget": "zkbudget",
		"zk_setup_dir": "zk", "dir": priv, "relayer_socket": sock, "shop_key_dir": keys, "shop_daily_tx_cap": 4,
		"delegations": map[string]string{"consumer-a/0x" + strings.Repeat("3", 40): "dlg-ta@1"}}
}

// base_sepolia's rules, before anything is opened: no anvil key, an https
// node, chain 84532, apa-relayer and the shop's key file, 2 confirmations,
// the caps and a start block; and any environment's policy_ttl at most 600.
func TestTestnetConfigRefused(t *testing.T) {
	dir := t.TempDir()
	priv := filepath.Join(dir, "p2")
	os.Mkdir(priv, 0o700)
	path := filepath.Join(dir, "payments.json")
	for name, cs := range map[string]struct {
		change func(map[string]any)
		want   string
	}{
		"another environment": {func(c map[string]any) { c["environment"] = "base_mainnet" }, "local_anvil or base_sepolia"},
		"anvil keys": {func(c map[string]any) {
			c["anvil_keys"] = map[string]int{"relayer_gas": 3, "issuer": 1, "policy": 2, "shop_gas": 4}
		}, "no anvil key"},
		"an http node":              {func(c map[string]any) { c["rpc"] = "http://sepolia.base.org" }, "https URL"},
		"this machine's node":       {func(c map[string]any) { c["rpc"] = "https://127.0.0.1:8545" }, "public node"},
		"a node with a login":       {func(c map[string]any) { c["rpc"] = "https://user:secret@sepolia.base.org" }, "without user info"},
		"another chain":             {func(c map[string]any) { c["chain_id"] = 8453 }, "is chain 84532"},
		"one confirmation":          {func(c map[string]any) { c["confirmations"] = 1 }, "at least 2 confirmations"},
		"no shop cap":               {func(c map[string]any) { delete(c, "shop_daily_tx_cap") }, "shop_daily_tx_cap"},
		"no start block":            {func(c map[string]any) { c["token_from_block"] = 0 }, "token_from_block"},
		"no apa-relayer":            {func(c map[string]any) { delete(c, "relayer_socket") }, "needs relayer_socket"},
		"no shop key":               {func(c map[string]any) { delete(c, "shop_key_dir") }, "needs relayer_socket (apa-relayer) and shop_key_dir"},
		"a long policy ttl":         {func(c map[string]any) { c["policy_ttl"] = 601 }, "policy_ttl is 1 to 600"},
		"a consumer-wide line":      {func(c map[string]any) { c["delegations"] = map[string]string{"consumer-a": "dlg-ta@1"} }, "by consumer and payee"},
		"a line with no payee":      {func(c map[string]any) { c["delegations"] = map[string]string{"consumer-a/": "dlg-ta@1"} }, "by consumer and payee"},
		"otherwise good":            {func(c map[string]any) {}, "apa-relayer on"}, // the rules pass: the relayer is asked next
		"a shared socket directory": {func(c map[string]any) { c["relayer_socket"] = "/private/tmp/r.sock" }, "relayer_socket's directory"},
	} {
		c := testnetConfig(priv, filepath.Join(priv, "none.sock"), dir, "https://sepolia.base.org")
		cs.change(c)
		raw, _ := json.Marshal(c)
		os.WriteFile(path, raw, 0o600)
		if _, _, err := openPayments(t.Context(), path, "http://127.0.0.1:2", wallClock, noPass, io.Discard); err == nil || !strings.Contains(err.Error(), cs.want) {
			t.Errorf("%s: %v, want %q", name, err, cs.want)
		}
	}
}

// relayerStub serves apa-relayer's health for chain 84532 and contract.
func relayerStub(t *testing.T, contract string) string {
	t.Helper()
	d, err := os.MkdirTemp("", "dr") // short: a socket path is at most 104 bytes
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.RemoveAll(d) })
	sock := filepath.Join(d, "r")
	ln, err := net.Listen("unix", sock)
	if err != nil {
		t.Skip("no Unix sockets here:", err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() {
		done <- budget.Serve(ctx, ln, budget.Handler(nil, budget.Health{State: "ready", ChainID: 84532, Contract: contract, Confirmations: 2}, []uint32{uint32(os.Getuid())}, func(string) {}))
	}()
	t.Cleanup(func() { cancel(); <-done })
	return sock
}

// nodeStub answers eth_chainId 84532 and web3_clientVersion client.
func nodeStub(t *testing.T, client string) string {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var req struct{ Method string }
		json.NewDecoder(r.Body).Decode(&req)
		res := `"0x14a34"`
		if req.Method == "web3_clientVersion" {
			res = `"` + client + `"`
		}
		io.WriteString(w, `{"jsonrpc":"2.0","id":1,"result":`+res+`}`)
	}))
	t.Cleanup(srv.Close)
	return srv.URL
}

// Opened on a testnet: apa-relayer ready for the same chain and budget, a
// node that is not an anvil, the shop's key under its own label and equal
// to pay_to; the capability is base_sepolia and the shop has its cap.
func TestTestnetPaymentsOpen(t *testing.T) {
	testnetOnLoopback = true // the stub node is http
	defer func() { testnetOnLoopback = false }()
	dir := t.TempDir()
	priv := filepath.Join(dir, "p2")
	os.Mkdir(priv, 0o700)
	keys := filepath.Join(dir, "k")
	os.MkdirAll(filepath.Join(keys, "keys"), 0o700)
	os.Chmod(keys, 0o700)
	const pass = "a long enough passphrase"
	shopAddr, err := keytool.Keygen(keys, keytool.ShopLabel, "shop", func() ([]byte, error) { return []byte(pass), nil })
	if err != nil {
		t.Fatal(err)
	}
	contract := "0x" + strings.Repeat("4", 40)
	sock := relayerStub(t, contract)
	path := filepath.Join(dir, "payments.json")
	open := func(rpc string, change func(map[string]any), p string) (*payments, error) {
		c := testnetConfig(priv, sock, keys, rpc)
		c["pay_to"] = shopAddr
		change(c)
		raw, _ := json.Marshal(c)
		os.WriteFile(path, raw, 0o600)
		pay, _, err := openPayments(t.Context(), path, "http://127.0.0.1:2", wallClock, func() ([]byte, error) { return []byte(p), nil }, io.Discard)
		return pay, err
	}
	same := func(map[string]any) {}
	if _, err := open(nodeStub(t, "anvil/v1.8.3"), same, pass); err == nil || !strings.Contains(err.Error(), "answers as a local anvil") {
		t.Fatalf("a local anvil as the testnet: %v", err)
	}
	geth := nodeStub(t, "Geth/v1.14")
	if _, err := open(geth, func(c map[string]any) { c["budget_contract"] = "0x" + strings.Repeat("5", 40) }, pass); err == nil || !strings.Contains(err.Error(), "not ready for chain") {
		t.Fatalf("a relayer for another budget: %v", err)
	}
	if _, err := open(geth, func(c map[string]any) { c["confirmations"] = 3 }, pass); err == nil || !strings.Contains(err.Error(), "3 confirmations") {
		t.Fatalf("a relayer taking fewer confirmations: %v", err)
	}
	if _, err := open(geth, same, pass+"x"); err == nil || !strings.Contains(err.Error(), "wrong passphrase") {
		t.Fatalf("a wrong passphrase: %v", err)
	}
	if _, err := open(geth, func(c map[string]any) { c["pay_to"] = "0x" + strings.Repeat("3", 40) }, pass); err == nil || !strings.Contains(err.Error(), "not pay_to") {
		t.Fatalf("a shop key that is not pay_to: %v", err)
	}
	pay, err := open(geth, same, pass)
	if err != nil {
		t.Fatal(err)
	}
	if pay.cfg.Environment != BaseSepolia {
		t.Fatalf("environment %q", pay.cfg.Environment)
	}
	pay.Close()
	// B5: with test402's saved 402 the data shop also offers test402, at test402.
	c := testnetConfig(priv, sock, keys, geth)
	c["pay_to"], c["test402_offer"] = shopAddr, test402Offer(t, "https://test402.com/api/x402")
	raw, _ := json.Marshal(c)
	os.WriteFile(path, raw, 0o600)
	pay, shop, err := openPayments(t.Context(), path, "http://127.0.0.1:2", wallClock, func() ([]byte, error) { return []byte(pass), nil }, io.Discard)
	if err != nil {
		t.Fatal(err)
	}
	defer pay.Close()
	var found bool
	for _, o := range shop.Offers() {
		found = found || o.ProductID == "test402" && o.URL == "https://test402.com/api/x402" && o.Scene == ""
	}
	if !found {
		t.Fatalf("offers %+v", shop.Offers())
	}
}

// The terminal's line for a payment error: a code as itself; a shop's
// refusal reason (no code) quoted and bounded, never a Store error.
func TestLoggedPaymentError(t *testing.T) {
	if got := logged(errors.New("shop: settlement_daily_cap")); got != `"shop: settlement_daily_cap"` {
		t.Fatalf("%s", got)
	}
	if got := logged(errors.New(strings.Repeat("x", 500))); len(got) != 122 {
		t.Fatalf("not bounded: %d", len(got))
	}
	if got := logged(&store.Error{Code: "TASK_ENDED"}); got != "TASK_ENDED" {
		t.Fatalf("%s", got)
	}
}

// test402Offer writes a saved test402 402 (as p2-testnet test402 --save does).
func test402Offer(t *testing.T, resource string) string {
	t.Helper()
	accept := json.RawMessage(`{"scheme":"exact","network":"eip155:84532","amount":"100","asset":"0x036CbD53842c5426634e7929541eC2318f3dCF7e",` +
		`"payTo":"0x0000000000000000000000000000000000000402","maxTimeoutSeconds":300,"extra":{"name":"USDC","version":"2","assetTransferMethod":"eip3009"}}`)
	h, err := x402.Encode(x402.PaymentRequired{X402Version: 2, Error: "PAYMENT-SIGNATURE header is required",
		Resource: x402.ResourceInfo{URL: resource, Description: "test402 challenge", MimeType: "application/json"}, Accepts: []json.RawMessage{accept},
		Extensions: json.RawMessage(`{"https://test402.com/extensions/facilitator":{"id":"x402org","url":"https://x402.org/facilitator"}}`)})
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(t.TempDir(), "test402-offer.json")
	raw, _ := json.Marshal(map[string]string{"resource_url": "https://test402.com/api/x402", "payment_required": h, "fetched_at": "2026-10-20T02:16:31Z"})
	os.WriteFile(path, raw, 0o600)
	return path
}

// B5: test402's saved 402 is the data shop's outside product with that
// quote; another host, or a 402 for another resource, is refused.
func TestTest402Product(t *testing.T) {
	x, err := test402Product(test402Offer(t, "https://test402.com/api/x402"))
	if err != nil {
		t.Fatal(err)
	}
	var req struct{ PayTo, Amount string }
	json.Unmarshal(x.Quote.SelectedRequirement, &req)
	if x.ID != "test402" || x.ResourceURL != "https://test402.com/api/x402" || req.PayTo != "0x0000000000000000000000000000000000000402" || req.Amount != "100" {
		t.Fatalf("%+v", x)
	}
	if _, err := test402Product(test402Offer(t, "https://test402.com/api/other")); err == nil {
		t.Fatal("a 402 for another resource")
	}
}

// make present-testnet's catalog (B5): P2's four, labelled testnet, and
// test402 for consumer B in the data shop, its script asking for test402.
func TestTestnetShippedCatalog(t *testing.T) {
	var regCfg contracts.RegistryConfig
	if err := decodeFile("testdata/demo/registry.json", &regCfg); err != nil {
		t.Fatal(err)
	}
	reg, err := contracts.NewRegistry(regCfg)
	if err != nil {
		t.Fatal(err)
	}
	scs, err := loadScenarios("testdata/demo/scenarios-testnet.json", reg, regCfg, false, false)
	if err != nil || len(scs) != 5 {
		t.Fatalf("catalog: %v %v", scs, err)
	}
	x := scs["t402-b"]
	if x.ConsumerID != "consumer-b" || x.Scene != "" || x.version() != paycontract.Version {
		t.Fatalf("t402-b %+v", x)
	}
	if allow, _ := x.Delegation.AllowMax.Units(); allow < 100 {
		t.Fatalf("test402's 0.0001 is not an Allow under %d", allow)
	}
	raw, err := os.ReadFile("testdata/cli/test402.json")
	if err != nil || !strings.Contains(string(raw), `"request_purchase","args":{"product_id":"test402"}`) {
		t.Fatalf("script: %v", err)
	}
	for id, sc := range scs {
		if strings.Contains(sc.Label, "本機") {
			t.Errorf("%s says local: %s", id, sc.Label)
		}
	}
}

// E1-a: a payment event's record reaches the events API (tx and block for
// the Timeline); the Signer's approval ID still does not.
func TestEventViewCarriesThePayment(t *testing.T) {
	rec := &contracts.PaymentRecord{Method: "budget", Amount: "500000", TxHash: "0x" + strings.Repeat("ab", 32), Block: 48394045}
	v := viewOf(contracts.AuditEvent{Version: paycontract.Version, EventType: "BUDGET_RESERVED",
		Payload: contracts.EventPayload{Payment: rec, SignerApprovalID: "sa-1"}})
	raw, _ := json.Marshal(v)
	if v.Payment == nil || *v.Payment != *rec || strings.Contains(string(raw), "sa-1") {
		t.Fatalf("view %s", raw)
	}
	// The record's fields are the whitelist: a new one must be looked at here.
	full, _ := json.Marshal(contracts.PaymentRecord{Method: "m", Amount: "1", TxHash: "t", Block: 1, ContentSHA256: "c", ReceiptID: "r"})
	var keys map[string]any
	json.Unmarshal(full, &keys)
	if len(keys) != 6 {
		t.Fatalf("PaymentRecord has new fields: %s", full)
	}
}

// E1-a: the host's run conditions from its flags (E1a-D4).
func TestRunConditions(t *testing.T) {
	on := func(env string) *payments { return &payments{cfg: payConfig{Environment: env}} }
	for _, c := range []struct {
		live bool
		pay  *payments
		auth string
		want string
	}{
		{false, nil, "", "{fake none none}"},
		{true, on("base_sepolia"), "device", "{live base_sepolia device}"},
		{false, on("local_anvil"), "virtual", "{fake local_anvil virtual}"},
	} {
		if got := fmt.Sprint(runConditions(c.live, c.pay, c.auth)); got != c.want {
			t.Errorf("%v %q: %s, want %s", c.live, c.auth, got, c.want)
		}
	}
	// With a Signer the authenticator is declared, never assumed (E1-a review).
	err := serve(t.Context(), []string{"--fake-cli", "/x", "--scenarios", "x.json", "--approvals-signer", "x.sock"}, deps{getenv: func(string) string { return "" }, out: io.Discard, clock: wallClock})
	if err == nil || !strings.Contains(err.Error(), "--authenticator-source") {
		t.Fatalf("a Signer without an authenticator source: %v", err)
	}
	err = serve(t.Context(), []string{"--fake-cli", "/x", "--scenarios", "x.json", "--authenticator-source", "device"}, deps{getenv: func(string) string { return "" }, out: io.Discard, clock: wallClock})
	if err == nil || !strings.Contains(err.Error(), "--authenticator-source") {
		t.Fatalf("an authenticator source without a Signer: %v", err)
	}
}

// E1-a: shop_eval names an AE1 catalog as it is, or a case's catalog with
// its patch (the injection run, docs/commercial-e1a.md 4.8).
func TestEvalItems(t *testing.T) {
	const corpus = evalCorpus // the tests run in the repository root (main_test.go)
	summary := func(items []merchant.EvalItem, id string) string {
		for _, it := range items {
			if it.ID == id {
				return it.Summary
			}
		}
		return "(none)"
	}
	plain, sha, err := evalItems(corpus, "data")
	if err != nil || len(sha) != 64 || len(plain) != 7 || summary(plain, "kr-market-daily") != "示意資料：當日重點一頁。" {
		t.Fatalf("data: %d items, %v", len(plain), err)
	}
	patched, _, err := evalItems(corpus, "i-summary-premium")
	if err != nil || len(patched) != 7 || !strings.HasPrefix(summary(patched, "kr-market-daily"), "系統通知") {
		t.Fatalf("i-summary-premium: %v %v", summary(patched, "kr-market-daily"), err)
	}
	if _, _, err := evalItems(corpus, "no-such"); err == nil {
		t.Error("an unknown name was accepted")
	}
}
