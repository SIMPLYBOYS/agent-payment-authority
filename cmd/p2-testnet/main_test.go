package main

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/SIMPLYBOYS/agent-payment-authority/internal/eth"
	"github.com/SIMPLYBOYS/agent-payment-authority/internal/eth/ethtest"
	"github.com/SIMPLYBOYS/agent-payment-authority/internal/keytool"
)

// answers is a terminal: the lines, then the passphrase.
type answers struct {
	line, pass string
	asked      int
}

func (a *answers) Line(string) (string, error) { a.asked++; return a.line, nil }
func (a *answers) Passphrase(string) ([]byte, error) {
	a.asked++
	return []byte(a.pass), nil
}

const pass = "a long enough passphrase"

// keysDir is apa-relayer's directory with the four role keys.
func keysDir(t *testing.T) (string, roles) {
	t.Helper()
	dir := filepath.Join(t.TempDir(), "r")
	os.MkdirAll(filepath.Join(dir, "keys"), 0o700)
	os.Chmod(dir, 0o700)
	r := roles{}
	for _, role := range roleOrder {
		label, _ := keytool.Role(role)
		a, err := keytool.Keygen(dir, label, role, func() ([]byte, error) { return []byte(pass), nil })
		if err != nil {
			t.Fatal(err)
		}
		r[role], _ = eth.ParseAddress(a)
	}
	return dir, r
}

func TestABIString(t *testing.T) {
	enc, _ := eth.Encode("USDC")
	if s, err := abiString(enc); err != nil || s != "USDC" {
		t.Fatalf("%q %v", s, err)
	}
	for _, bad := range [][]byte{nil, make([]byte, 31), enc[:40]} {
		if _, err := abiString(bad); err == nil {
			t.Fatalf("decoded %x", bad)
		}
	}
}

// Off a local test, the node must be a public https one, and the token Circle's.
func TestRefusals(t *testing.T) {
	dir, _ := keysDir(t)
	w := []string{"--wallet-a", "0x" + strings.Repeat("1", 40), "--wallet-b", "0x" + strings.Repeat("2", 40)}
	for name, cs := range map[string]struct {
		args []string
		want string
	}{
		"no command":       {nil, "usage"},
		"no dir":           {append([]string{"preflight"}, w...), "usage"},
		"one wallet twice": {[]string{"preflight", "--dir", dir, "--wallet-a", w[1], "--wallet-b", w[1]}, "two payer wallets"},
		"an http node":     {append([]string{"preflight", "--dir", dir, "--rpc", "http://sepolia.base.org"}, w...), "public https node"},
		"this machine":     {append([]string{"preflight", "--dir", dir, "--rpc", "https://127.0.0.1:8545"}, w...), "public https node"},
		"a login":          {append([]string{"preflight", "--dir", dir, "--rpc", "https://u:p@sepolia.base.org"}, w...), "public https node"},
		"another token":    {append([]string{"preflight", "--dir", dir, "--token", "0x" + strings.Repeat("4", 40)}, w...), "Circle's"},
	} {
		if err := run(context.Background(), cs.args, os.Stdout, &answers{}); err == nil || !strings.Contains(err.Error(), cs.want) {
			t.Errorf("%s: %v, want %q", name, err, cs.want)
		}
	}
	os.Chmod(filepath.Join(dir, "keys"), 0o755)
	if err := run(context.Background(), append([]string{"preflight", "--dir", dir}, w...), os.Stdout, &answers{}); err == nil || !strings.Contains(err.Error(), "0700") {
		t.Fatalf("a keys directory others can read: %v", err)
	}
}

// On a local anvil standing in for Base Sepolia (tests only): preflight
// reads and says what is missing; deploy sends nothing without "yes", then
// the two contracts, 2 blocks deep, and writes the files; it never runs twice.
func TestPreflightAndDeploy(t *testing.T) {
	local = true
	defer func() { local = false }()
	confirmWait, confirmPoll = 60*time.Second, 50*time.Millisecond
	c := ethtest.Anvil(t)
	ctx, stop := context.WithCancel(context.Background())
	defer stop()
	go func() { // blocks keep coming, as on a chain: a transaction gets its depth
		for ctx.Err() == nil {
			c.Raw(ctx, "evm_mine", nil)
			time.Sleep(100 * time.Millisecond)
		}
	}()
	usdcAddr := ethtest.Deploy(t, c, "TestUSDC.sol/TestUSDC")
	dir, r := keysDir(t)
	wa, wb := eth.Address{0x11}, eth.Address{0x22}
	args := func(cmd string) []string {
		return []string{cmd, "--dir", dir, "--rpc", c.URL, "--token", usdcAddr.Hex(), "--wallet-a", wa.Hex(), "--wallet-b", wb.Hex(),
			"--contracts", filepath.Join(ethtest.Root(), "var/contracts/out"), "--registry", filepath.Join(ethtest.Root(), "testdata/demo/registry.json"),
			"--profile", filepath.Join(ethtest.Root(), "testdata/policy/m1-demo-profile.json")}
	}
	var out strings.Builder
	if err := run(context.Background(), args("preflight"), &out, &answers{}); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out.String(), `name "USDC", version "2", decimals 6`) || !strings.Contains(out.String(), "not ready for deploy: no test funds yet for relayer-gas, issuer, shop, wallet-a, wallet-b") {
		t.Fatalf("preflight before the faucets:\n%s", out.String())
	}
	if err := run(context.Background(), args("deploy"), &out, &answers{line: "yes", pass: pass}); err == nil || !strings.Contains(err.Error(), "no test funds") {
		t.Fatalf("deploy without funds: %v", err)
	}
	for _, role := range []string{"relayer-gas", "issuer", "shop"} {
		ethtest.RPC(t, c, "anvil_setBalance", r[role].Hex(), "0xde0b6b3a7640000") // 1 test ETH
	}
	for _, w := range []eth.Address{wa, wb} {
		data, _ := eth.Call("mint(address,uint256)", w, uint64(20_000_000))
		ethtest.Mine(t, c, ethtest.Key(t, 0), &eth.Tx{To: &usdcAddr, Gas: 200_000, Data: data})
	}
	out.Reset()
	if err := run(context.Background(), args("preflight"), &out, &answers{}); err != nil || !strings.Contains(out.String(), "ready for deploy\n") {
		t.Fatalf("preflight funded: %v\n%s", err, out.String())
	}
	nonce, _ := c.PendingNonce(context.Background(), r["issuer"])
	no := &answers{line: "no", pass: pass}
	if err := run(context.Background(), args("deploy"), &out, no); err == nil || !strings.Contains(err.Error(), "nothing was sent") || no.asked != 1 {
		t.Fatalf("deploy not confirmed: %v (asked %d)", err, no.asked)
	}
	if n, _ := c.PendingNonce(context.Background(), r["issuer"]); n != nonce {
		t.Fatal("an unconfirmed deploy sent a transaction")
	}
	if _, err := os.Lstat(filepath.Join(dir, "deploy.json")); !errors.Is(err, os.ErrNotExist) {
		t.Fatal("an unconfirmed deploy wrote its journal")
	}
	out.Reset()
	if err := run(context.Background(), args("deploy"), &out, &answers{line: "yes", pass: pass}); err != nil {
		t.Fatalf("deploy: %v\n%s", err, out.String())
	}
	if n, _ := c.PendingNonce(context.Background(), r["issuer"]); n != nonce+2 {
		t.Fatalf("deploy sent %d transactions", n-nonce)
	}
	var pay, rel, reg, prof map[string]any
	for path, v := range map[string]*map[string]any{"host/payments.json": &pay, "relayer.json": &rel, "host/registry.json": &reg, "host/profile.json": &prof} {
		p := filepath.Join(dir, path)
		if fi, err := os.Stat(p); err != nil || fi.Mode().Perm() != 0o600 {
			t.Fatalf("%s: %v %v", path, fi, err)
		}
		if err := readJSON(p, v); err != nil {
			t.Fatal(err)
		}
	}
	budget, _ := eth.ParseAddress(pay["budget_contract"].(string))
	var code string
	c.Raw(context.Background(), "eth_getCode", &code, budget.Hex(), "latest")
	if len(code) < 100 || pay["environment"] != "base_sepolia" || pay["pay_to"] != r["shop"].Hex() || pay["token_from_block"].(float64) == 0 ||
		rel["budget_contract"] != budget.Hex() || rel["daily_tx_cap"].(float64) != relayerCap || pay["shop_daily_tx_cap"].(float64) != shopCap {
		t.Fatalf("payments %v\nrelayer %v", pay, rel)
	}
	if d := pay["delegations"].(map[string]any); d["consumer-a"] != "dlg-ta@1" || d["consumer-b"] != "dlg-tb@1" {
		t.Fatalf("delegations %v", d)
	}
	raw, _ := json.Marshal(reg)
	if !strings.Contains(string(raw), wa.Hex()) || reg["merchant_pay_to"] != r["shop"].Hex() || !strings.Contains(string(raw), usdcAddr.Hex()) {
		t.Fatalf("registry %s", raw)
	}
	if m := prof["merchants"].([]any); len(m) != 1 || m[0] != r["shop"].Hex() || prof["asset_id"] != assetID {
		t.Fatalf("profile %v", prof)
	}
	var j journal
	readJSON(filepath.Join(dir, "deploy.json"), &j)
	if len(j.Txs) != 2 || j.From != r["issuer"].Hex() {
		t.Fatalf("journal %+v", j)
	}
	if strings.Contains(out.String(), pass) || strings.Contains(out.String(), c.URL) {
		t.Fatalf("the output names the passphrase or the node's URL:\n%s", out.String())
	}
	// Never twice: the journal is there.
	if err := run(context.Background(), args("deploy"), &out, &answers{line: "yes", pass: pass}); err == nil || !strings.Contains(err.Error(), "deploy has run") {
		t.Fatalf("a second deploy: %v", err)
	}
	if n, _ := c.PendingNonce(context.Background(), r["issuer"]); n != nonce+2 {
		t.Fatal("a second deploy sent a transaction")
	}
}
