package main

import (
	"context"
	"encoding/base64"
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
	"github.com/SIMPLYBOYS/agent-payment-authority/internal/signer"
	"github.com/SIMPLYBOYS/agent-payment-authority/internal/signer/signertest"
	"github.com/SIMPLYBOYS/agent-payment-authority/internal/x402"
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
	cwd, _ := os.Getwd()
	os.Chdir(ethtest.Root()) // zk/setup is the repo's, as when the script runs it
	defer os.Chdir(cwd)
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
	if err := run(context.Background(), args("deploy"), &out, &answers{line: "yes", pass: pass}); err == nil || !strings.Contains(err.Error(), "zkbudget is missing") {
		t.Fatalf("deploy without the prover: %v", err) // checked before anything is sent
	}
	os.WriteFile(filepath.Join(dir, "zkbudget"), []byte("#!/bin/sh\n"), 0o700)
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
	journaled := 0
	beforeSend = func(hash string) { // the journal holds each transaction before it is sent
		var j journal
		if err := readJSON(filepath.Join(dir, "deploy.json"), &j); err == nil && len(j.Txs) > 0 && j.Txs[len(j.Txs)-1].Hash == hash {
			journaled++
		}
	}
	defer func() { beforeSend = nil }()
	if err := run(context.Background(), args("deploy"), &out, &answers{line: "yes", pass: pass}); err != nil {
		t.Fatalf("deploy: %v\n%s", err, out.String())
	}
	if journaled != 2 {
		t.Fatalf("%d of 2 transactions were in the journal before they were sent", journaled)
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
	if len(j.Txs) != 2 || j.From != r["issuer"].Hex() || j.Txs[1].Contract != budget.Hex() || j.Txs[0].Block == 0 {
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
	// Stopped after both were confirmed, before the files: deploy resumes from
	// its journal and sends nothing.
	for _, p := range []string{"relayer.json", "host", "run"} {
		os.RemoveAll(filepath.Join(dir, p))
	}
	out.Reset()
	if err := run(context.Background(), args("deploy"), &out, &answers{}); err != nil || !strings.Contains(out.String(), "nothing is sent") {
		t.Fatalf("resume: %v\n%s", err, out.String())
	}
	if _, err := os.Stat(filepath.Join(dir, "host", "payments.json")); err != nil {
		t.Fatal("resume wrote no payments.json")
	}
	// A journal that did not finish is refused, nothing sent.
	for _, p := range []string{"relayer.json", "host", "run"} {
		os.RemoveAll(filepath.Join(dir, p))
	}
	j.Txs[1].Contract = ""
	writeJSON(filepath.Join(dir, "deploy.json"), j)
	if err := run(context.Background(), args("deploy"), &out, &answers{line: "yes", pass: pass}); err == nil || !strings.Contains(err.Error(), "did not finish") {
		t.Fatalf("an unfinished journal: %v", err)
	}
	if n, _ := c.PendingNonce(context.Background(), r["issuer"]); n != nonce+2 {
		t.Fatal("an unfinished journal sent a transaction")
	}
}

// test402's header is read leniently (members this tool does not know),
// every remote string quoted.
func TestDescribe402(t *testing.T) {
	tok, _ := eth.ParseAddress(circleUSDC)
	accept := json.RawMessage(`{"scheme":"exact","network":"eip155:84532","amount":"100","asset":"` + circleUSDC + `","payTo":"0x` + strings.Repeat("7", 40) +
		`","maxTimeoutSeconds":60,"extra":{"name":"USDC","version":"2","assetTransferMethod":"eip3009"},"future":"\u001b[31m"}`)
	h, err := x402.Encode(x402.PaymentRequired{X402Version: 2, Resource: x402.ResourceInfo{URL: "https://test402.com/api/x402"}, Accepts: []json.RawMessage{accept},
		Extensions: json.RawMessage(`{"x":{}}`)})
	if err != nil {
		t.Fatal(err)
	}
	var out strings.Builder
	if err := describe402("test402.com/api/x402", h, tok, &out); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out.String(), `amount "100"`) || !strings.Contains(out.String(), `payTo "0x777`) || strings.Contains(out.String(), "not Base Sepolia") {
		t.Fatalf("%s", out.String())
	}
	if err := describe402("x", "not base64", tok, &out); err == nil || !strings.Contains(err.Error(), `"not base64"`) {
		t.Fatalf("a broken header: %v", err) // shown, quoted, for the record
	}
	// x402 v1's shape (no resource, a string timeout, more members) is read too.
	v1h := base64.StdEncoding.EncodeToString([]byte(`{"x402Version":1,"error":"x","accepts":[{"scheme":"exact","network":"eip155:84532","maxAmountRequired":"100","maxTimeoutSeconds":"60","payTo":"0x1"}],"more":1}`))
	if err := describe402("x", v1h, tok, &out); err != nil {
		t.Fatalf("v1: %v", err)
	}
}

// opt-signer (B6): /opt's signer.json for the testnet, from its current one
// and deploy's files: what is kept, what is replaced, and the refusals.
func TestOptSigner(t *testing.T) {
	dir := t.TempDir()
	host := filepath.Join(dir, "host")
	os.Mkdir(host, 0o700)
	shop, wa, wb := "0x"+strings.Repeat("5", 40), "0x"+strings.Repeat("a", 40), "0x"+strings.Repeat("b", 40)
	writeJSON(filepath.Join(host, "payments.json"), map[string]any{"environment": "base_sepolia", "rpc": "https://sepolia.base.org", "chain_id": chainID,
		"confirmations": 2, "token": circleUSDC, "token_name": "USDC", "token_version": "2", "pay_to": shop, "budget_contract": "0x" + strings.Repeat("6", 40),
		"delegations": map[string]string{"consumer-a": "dlg-ta@1", "consumer-b": "dlg-tb@1"}})
	writeJSON(filepath.Join(host, "registry.json"), map[string]any{"wallets": []map[string]string{{"wallet_id": "wallet-a", "from": wa}, {"wallet_id": "wallet-b", "from": wb}}})
	var c map[string]any
	json.Unmarshal(signertest.Config("/opt/apa-signer/run/signer.sock", 501, 1790000000, "0x"+strings.Repeat("1", 40), "0x"+strings.Repeat("2", 40)), &c)
	// /opt's today is A2's: approvers without P2's caps, no voice section (V1's is staged apart).
	c["approvers"] = []map[string]any{{"approver_id": "approver-a", "consumer_id": "consumer-a", "wallet_ids": []string{"wallet-a"}, "signer_max": "500000", "version": 3}}
	c["webauthn"] = map[string]any{"rp_id": "localhost", "origins": []string{"http://localhost:8402"}, "challenge_ttl_seconds": 120, "approval_ttl_seconds": 300}
	voice := map[string]any{"socket": "/opt/apa-signer/voice-run/voice.sock", "timeout_ms": 8000, "speaker_threshold_milli": 365, "asr_model": "asr@t",
		"speaker_model": "spk@t", "decimals": 18, "merchants": []map[string]string{{"pay_to": "0x" + strings.Repeat("3", 40), "name": "測試商店", "pinyin": "ce shi shang dian"}}}
	staged := map[string]any{}
	for k, v := range c {
		staged[k] = v
	}
	staged["voice"] = voice
	base, v1 := filepath.Join(dir, "signer.json"), filepath.Join(dir, "v1-signer.json")
	writeJSON(base, c)
	writeJSON(v1, staged)
	dst := filepath.Join(dir, "new.json")
	var out strings.Builder
	// Without a voice section the approval's payee is not checked: refused.
	if err := run(context.Background(), []string{"opt-signer", "--dir", dir, "--base", base, "--out", dst}, &out, &answers{}); err == nil || !strings.Contains(err.Error(), "voice section") {
		t.Fatalf("no voice: %v", err)
	}
	args := []string{"opt-signer", "--dir", dir, "--base", base, "--voice", v1, "--out", dst, "--test402-pay-to", "0x" + strings.Repeat("7", 40)}
	if err := run(context.Background(), args, &out, &answers{}); err != nil {
		t.Fatal(err)
	}
	raw, _ := os.ReadFile(dst)
	cfg, err := signer.ParseConfig(raw)
	if err != nil {
		t.Fatal(err)
	}
	if fi, _ := os.Stat(dst); fi.Mode().Perm() != 0o600 {
		t.Fatalf("mode %v", fi.Mode())
	}
	if cfg.Socket != "/opt/apa-signer/run/signer.sock" || len(cfg.Callers) != 1 || cfg.Callers[0] != 501 { // kept
		t.Fatalf("socket and callers %+v", cfg)
	}
	if len(cfg.Wallets) != 2 || cfg.Wallets[0].Address != wa || cfg.Wallets[1].Address != wb || cfg.Assets[0].AssetContract != circleUSDC {
		t.Fatalf("wallets %+v assets %+v", cfg.Wallets, cfg.Assets)
	}
	d := cfg.Delegations
	if len(d) != 3 || d[0].DelegationID != "dlg-ta" || d[1].DelegationID != "dlg-tb" || d[0].Merchants[0] != shop || d[0].TotalCap != dlgTotal ||
		d[2].DelegationID != "dlg-t402" || d[2].WalletID != "wallet-b" || d[2].PerTxMax != t402Units || d[2].TotalCap != t402Units {
		t.Fatalf("delegations %+v", d)
	}
	if a := cfg.Approvers[0]; a.SignerMax != approverMax || a.SignerTotal != approverSum || a.SignerWindowSeconds != 86400 || a.Version != 4 {
		t.Fatalf("approver %+v", a)
	}
	if m := cfg.Voice.Merchants; len(m) != 1 || m[0].PayTo != shop || m[0].Name != "測試商店" || cfg.Voice.Decimals != 6 {
		t.Fatalf("voice %+v", cfg.Voice)
	}
	if r := cfg.Reservation; r == nil || r.ChainID != "84532" || r.Confirmations != 2 || r.BudgetContract != "0x"+strings.Repeat("6", 40) {
		t.Fatalf("reservation %+v", r)
	}
	if !strings.Contains(out.String(), "SHA-256 ") {
		t.Fatalf("output %s", out.String())
	}
	if err := run(context.Background(), args, &out, &answers{}); err == nil || !strings.Contains(err.Error(), "exists") {
		t.Fatalf("written over: %v", err)
	}
	// A base whose relying party is not the Host's origin is refused.
	c["webauthn"] = map[string]any{"rp_id": "localhost", "origins": []string{"http://localhost:9000"}, "challenge_ttl_seconds": 120, "approval_ttl_seconds": 300}
	writeJSON(base, c)
	if err := run(context.Background(), []string{"opt-signer", "--dir", dir, "--base", base, "--voice", v1, "--out", dst + "2"}, &out, &answers{}); err == nil || !strings.Contains(err.Error(), "origin") {
		t.Fatalf("another origin: %v", err)
	}
	// A voice section from elsewhere (its socket outside /opt/apa-signer) is refused.
	voice["socket"] = "/Users/someone/repo/var/v1-dev/voice.sock"
	writeJSON(v1, staged)
	if err := run(context.Background(), []string{"opt-signer", "--dir", dir, "--base", base, "--voice", v1, "--out", dst + "4"}, &out, &answers{}); err == nil || !strings.Contains(err.Error(), "not under the Signer's own") {
		t.Fatalf("a voice section from elsewhere: %v", err)
	}
	voice["socket"] = "/opt/apa-signer/voice-run/voice.sock"
	// Two voice merchants: which one is the shop is not guessed.
	voice["merchants"] = []map[string]string{{"pay_to": "0x" + strings.Repeat("3", 40), "name": "測試商店", "pinyin": "ce shi shang dian"},
		{"pay_to": "0x" + strings.Repeat("8", 40), "name": "街角咖啡", "pinyin": "jie jiao ka fei"}}
	writeJSON(v1, staged)
	if err := run(context.Background(), []string{"opt-signer", "--dir", dir, "--base", base, "--voice", v1, "--out", dst + "3"}, &out, &answers{}); err == nil || !strings.Contains(err.Error(), "exactly one merchant") {
		t.Fatalf("two voice merchants: %v", err)
	}
}
