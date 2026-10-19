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
	"testing"

	"github.com/SIMPLYBOYS/agent-payment-authority/internal/contracts"
	"github.com/SIMPLYBOYS/agent-payment-authority/internal/merchant"
	"github.com/SIMPLYBOYS/agent-payment-authority/internal/paycontract"
	"github.com/SIMPLYBOYS/agent-payment-authority/internal/policy"
	"github.com/SIMPLYBOYS/agent-payment-authority/internal/reconcile"
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
		"a key in the file":   {func(c map[string]any) { c["gas_key"] = strings.Repeat("ab", 32) }, "unknown field gas_key"},
		"no delegations":      {func(c map[string]any) { delete(c, "delegations") }, "delegations"},
		"a shared directory":  {func(c map[string]any) { os.Chmod(priv, 0o750) }, "0700"},
		"no chain at the RPC": {func(c map[string]any) {}, "did not answer"},
	} {
		os.Chmod(priv, 0o700)
		c := good()
		cs.change(c)
		raw, _ := json.Marshal(c)
		os.WriteFile(path, raw, 0o600)
		if _, _, err := openPayments(t.Context(), path, "http://127.0.0.1:2", wallClock, io.Discard); err == nil || !strings.Contains(err.Error(), cs.want) {
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
	on, off := &api{payments: true}, &api{}
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
	scs, err := loadScenarios("testdata/demo/scenarios-p2.json", reg, regCfg, false)
	if err != nil || len(scs) != 4 || scs["cafe-a"].Scene != "cafe" || scs["data-b"].Scene != "" || scs["cafe-b"].version() != paycontract.Version {
		t.Fatalf("catalog: %v %v", scs, err)
	}
	// The shop's prices against the delegations' allow_max: the cafe's lunch is an Allow, the brief an Ask.
	shop, _ := merchant.New("http://127.0.0.1:1")
	price := map[string]uint64{}
	for _, o := range shop.Offers() {
		var req struct{ Amount contracts.Amount }
		json.Unmarshal(o.Quote.SelectedRequirement, &req)
		price[o.Scene], _ = req.Amount.Units()
	}
	for id, sc := range scs {
		allow, _ := sc.Delegation.AllowMax.Units()
		if want := map[string]bool{"cafe": true, "": false}[sc.Scene]; (price[sc.Scene] <= allow) != want {
			t.Fatalf("%s: price %d, allow_max %d", id, price[sc.Scene], allow)
		}
	}
	raw, _ := os.ReadFile("testdata/policy/m1-demo-profile.json")
	if p, err := policy.LoadProfile(raw); err != nil || checkDelegations(scs, p) != nil {
		t.Fatalf("profile: %v", err)
	}
}
