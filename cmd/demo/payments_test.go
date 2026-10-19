package main

// --payments' checks before anything is opened: a local chain only, four
// different anvil keys named by index, a private directory, the chain the
// file names. No chain runs here (the RPC is a closed local port).

import (
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/SIMPLYBOYS/agent-payment-authority/internal/merchant"
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
