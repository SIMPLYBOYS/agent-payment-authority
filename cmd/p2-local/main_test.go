package main

// The setup on a fresh local anvil: the contracts deployed, TestUSDC at the
// fixture asset address minting to both wallets, the files the demo reads;
// a second run on the same anvil refuses. Nothing but anvil here.

import (
	"io"
	"math/big"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/SIMPLYBOYS/agent-payment-authority/internal/contracts"
	"github.com/SIMPLYBOYS/agent-payment-authority/internal/eth"
	"github.com/SIMPLYBOYS/agent-payment-authority/internal/eth/ethtest"
	"github.com/SIMPLYBOYS/agent-payment-authority/internal/signer"
)

func TestSetup(t *testing.T) {
	rpc := ethtest.Anvil(t)
	root := ethtest.Root()
	if _, err := os.Stat(filepath.Join(root, "var/contracts/out/Budget.sol/Budget.json")); err != nil {
		t.Skip("no contract build (var/ is not in git)")
	}
	out := filepath.Join(t.TempDir(), "p2")
	a, b := "0x"+strings.Repeat("a1", 20), "0x"+strings.Repeat("b2", 20)
	args := []string{"--rpc", rpc.URL, "--wallet-a", a, "--wallet-b", b, "--out", out,
		"--contracts", filepath.Join(root, "var/contracts/out"), "--registry", filepath.Join(root, "testdata/demo/registry.json")}
	var log strings.Builder
	if err := run(t.Context(), args, &log); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(log.String(), `"reservation"`) {
		t.Fatalf("no reservation section printed: %s", log.String())
	}
	if fi, err := os.Stat(out); err != nil || fi.Mode().Perm() != 0o700 {
		t.Fatalf("out dir: %v %v", fi, err)
	}
	var reg contracts.RegistryConfig
	raw, _ := os.ReadFile(filepath.Join(out, "registry.json"))
	if err := contracts.Decode(raw, &reg); err != nil || reg.Wallets[0].From != a || reg.Wallets[1].From != b {
		t.Fatalf("registry: %+v %v", reg.Wallets, err)
	}
	var pay map[string]any
	raw, _ = os.ReadFile(filepath.Join(out, "payments.json"))
	if err := contracts.Decode(raw, &pay); err != nil || pay["token"] != reg.Assets[0].AssetContract {
		t.Fatalf("payments: %v %v", pay, err)
	}
	asset, _ := eth.ParseAddress(reg.Assets[0].AssetContract)
	for _, w := range []string{a, b} {
		addr, _ := eth.ParseAddress(w)
		data, _ := eth.Call("balanceOf(address)", addr)
		res, err := rpc.Call(t.Context(), eth.Msg{To: asset, Data: data})
		if err != nil || new(big.Int).SetBytes(res).Uint64() != 20_000_000 {
			t.Fatalf("balance of %s: %x %v", w, res, err)
		}
	}
	if !strings.Contains(log.String(), "--registry "+filepath.Join(out, "registry.json")) || !strings.Contains(log.String(), "dlg-a") {
		t.Fatalf("next steps not printed: %s", log.String())
	}
	// Again on the same anvil: refused (its token is set up). Into another
	// directory on it, or into the same directory on a fresh anvil: the
	// first because of the chain, the second because of the old files.
	other := filepath.Join(t.TempDir(), "p2b")
	if err := run(t.Context(), append(args[:6:6], append([]string{"--out", other}, args[8:]...)...), io.Discard); err == nil || !strings.Contains(err.Error(), "already has code") {
		t.Fatalf("a second run on the same anvil: %v", err)
	}
	fresh := ethtest.Anvil(t)
	again := append([]string{"--rpc", fresh.URL}, args[2:]...)
	if err := run(t.Context(), again, io.Discard); err == nil || !strings.Contains(err.Error(), "earlier chain") {
		t.Fatalf("the same directory on a fresh anvil: %v", err)
	}
	if err := run(t.Context(), []string{"--rpc", "https://sepolia.base.org", "--wallet-a", a, "--wallet-b", b}, io.Discard); err == nil {
		t.Fatal("a public RPC was accepted")
	}
}

// scripts/demo/p2-dev.sh's configuration is one the Signer takes: the two
// wallets, the fixture asset and merchant, the delegations, the approvers
// on localhost:8402, and the reservation section of the local chain.
func TestDevSignerConfig(t *testing.T) {
	dir := t.TempDir()
	pay := filepath.Join(dir, "payments.json")
	os.WriteFile(pay, []byte(`{"rpc":"http://127.0.0.1:8545","chain_id":84532,"confirmations":1,"token":"0x4444444444444444444444444444444444444444",
		"token_name":"USDC","token_version":"2","pay_to":"0x3333333333333333333333333333333333333333","budget_contract":"0xe7f1725e7734ce288f8367e1bb143e90bb3f0512"}`), 0o600)
	out := filepath.Join(dir, "signer.json")
	a, b := "0x"+strings.Repeat("a1", 20), "0x"+strings.Repeat("b2", 20)
	cmd := exec.Command("python3", "-I", filepath.Join(ethtest.Root(), "scripts/demo/p2-signer-config.py"))
	cmd.Env = append(os.Environ(), "A="+a, "B="+b, "SSOCK=/tmp/p2-dev-test.sock", "PAY="+pay, "OUT="+out, "UID_=501")
	if msg, err := cmd.CombinedOutput(); err != nil {
		t.Skip("no python3 here:", err, string(msg))
	}
	raw, _ := os.ReadFile(out)
	if fi, err := os.Stat(out); err != nil || fi.Mode().Perm() != 0o600 {
		t.Fatalf("mode: %v %v", fi, err)
	}
	cfg, err := signer.ParseConfig(raw)
	if err != nil {
		t.Fatal(err)
	}
	if r := cfg.Reservation; r == nil || r.BudgetContract != "0xe7f1725e7734ce288f8367e1bb143e90bb3f0512" || r.ChainID != "84532" || r.Confirmations != 1 {
		t.Fatalf("reservation: %+v", cfg.Reservation)
	}
	if len(cfg.Wallets) != 2 || cfg.Wallets[0].Address != a || cfg.Wallets[1].Address != b || len(cfg.Delegations) != 2 || cfg.MaxWindowSeconds < 300 {
		t.Fatalf("config: %+v", cfg)
	}
}
