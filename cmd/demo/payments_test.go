package main

// --payments' checks before anything is opened: a local chain only, four
// different anvil keys named by index, a private directory, the chain the
// file names. No chain runs here (the RPC is a closed local port).

import (
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
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
		"a public RPC":        {func(c map[string]any) { c["rpc"] = "https://sepolia.base.org" }, "local anvil"},
		"a LAN RPC":           {func(c map[string]any) { c["rpc"] = "http://192.168.1.2:8545" }, "loopback"},
		"one key twice":       {func(c map[string]any) { c["anvil_keys"] = map[string]int{"relayer_gas": 1, "issuer": 1, "policy": 2, "shop_gas": 4} }, "four different"},
		"a key past anvil's":  {func(c map[string]any) { c["anvil_keys"] = map[string]int{"relayer_gas": 9, "issuer": 1, "policy": 2, "shop_gas": 4} }, "anvil key 9"},
		"a key in the file":   {func(c map[string]any) { c["gas_key"] = strings.Repeat("ab", 32) }, "payments:"},
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
