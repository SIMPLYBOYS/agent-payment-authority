package main

import (
	"context"
	"encoding/json"
	"net"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/SIMPLYBOYS/agent-payment-authority/internal/contracts"
	"github.com/SIMPLYBOYS/agent-payment-authority/internal/eth"
	"github.com/SIMPLYBOYS/agent-payment-authority/internal/eth/ethtest"
	"github.com/SIMPLYBOYS/agent-payment-authority/internal/signer"
	"github.com/SIMPLYBOYS/agent-payment-authority/internal/signer/signertest"
	"github.com/decred/dcrd/dcrec/secp256k1/v4"
)

// docs/commercial-8004.md §8 on a local anvil, TestAgentRegistry checking the wallet's EIP-712 signature as
// the registry's 2.0.0 does: after agents registered both Agents, a Signer holding the two wallets' keys signs
// each wallet's consent and the issuer sends setAgentWallet. Nothing is sent without "yes" or when the
// Signer's consent is for another agent; then both agentWallets are the consumers' own wallets, each
// journaled before it was sent; run again, nothing is asked or sent.
func TestAgentWallet(t *testing.T) {
	local = true
	defer func() { local = false }()
	confirmWait, confirmPoll = 60*time.Second, 50*time.Millisecond
	c := ethtest.Anvil(t)
	ctx, stop := context.WithCancel(context.Background())
	defer stop()
	go func() {
		for ctx.Err() == nil {
			c.Raw(ctx, "evm_mine", nil)
			time.Sleep(100 * time.Millisecond)
		}
	}()
	registry := ethtest.Deploy(t, c, "TestAgentRegistry.sol/TestAgentRegistry")
	dir, r := keysDir(t)
	host := filepath.Join(dir, "host")
	os.Mkdir(host, 0o700)
	keyA, _ := secp256k1.GeneratePrivateKey()
	keyB, _ := secp256k1.GeneratePrivateKey()
	addrA, addrB := signer.Address(keyA.PubKey()), signer.Address(keyB.PubKey())
	var reg contracts.RegistryConfig
	if err := readJSON("../../testdata/demo/registry.json", &reg); err != nil {
		t.Fatal(err)
	}
	reg.Wallets[0].From, reg.Wallets[1].From = addrA, addrB // the wallets whose keys the Signer below holds
	writeJSON(filepath.Join(host, "registry.json"), reg)
	ethtest.RPC(t, c, "anvil_setBalance", r["issuer"].Hex(), "0xde0b6b3a7640000")
	var out strings.Builder
	if err := agents(ctx, []string{"--dir", dir, "--rpc", c.URL, "--identity-registry", registry.Hex()}, &out, &answers{line: "yes", pass: pass}); err != nil {
		t.Fatalf("%v\n%s", err, out.String())
	}
	nonce := func() uint64 { n, _ := c.PendingNonce(ctx, r["issuer"]); return n }
	onChain := func(id uint64) string {
		d, _ := eth.Call("getAgentWallet(uint256)", id)
		b, _ := c.Call(ctx, eth.Msg{To: registry, Data: d})
		w, _ := eth.Word(b, 0)
		a, _ := eth.WordAddress(w)
		return strings.ToLower(a.Hex())
	}
	// serve: a Signer with the two wallets' keys, naming these agents (wallet-a's first, then wallet-b's).
	serve := func(ids ...uint64) string {
		sockDir, _ := os.MkdirTemp("", "aw") // short: a socket path is at most 104 bytes
		t.Cleanup(func() { os.RemoveAll(sockDir) })
		sock := filepath.Join(sockDir, "s")
		var cfg map[string]any
		json.Unmarshal(signertest.Config(sock, uint32(os.Getuid()), uint64(time.Now().Unix()), addrA, addrB), &cfg)
		var ags []map[string]any
		for i, id := range ids {
			ags = append(ags, map[string]any{"wallet_id": []string{"wallet-a", "wallet-b"}[i], "chain_id": signertest.Chain, "registry": strings.ToLower(registry.Hex()),
				"agent_id": id, "owner": strings.ToLower(r["issuer"].Hex())})
		}
		cfg["agents"] = ags
		raw, _ := json.Marshal(cfg)
		sc, err := signer.ParseConfig(raw)
		if err != nil {
			t.Fatal(err)
		}
		core, err := signer.NewCore(ctx, sc, map[string]*secp256k1.PrivateKey{addrA: keyA, addrB: keyB}, filepath.Join(t.TempDir(), "signer.db"),
			func() uint64 { return uint64(time.Now().Unix()) }, signer.Unavailable{})
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { core.Close() })
		ln, err := net.Listen("unix", sock)
		if err != nil {
			t.Fatal(err)
		}
		sctx, cancel := context.WithCancel(ctx)
		t.Cleanup(cancel)
		go signer.Serve(sctx, ln, core, func(string) {})
		return sock
	}
	args := func(sock string) []string { return []string{"--dir", dir, "--rpc", c.URL, "--signer", sock} }

	good := serve(1, 2)
	n0 := nonce()
	if err := agentWallet(ctx, args(good), &out, &answers{line: "no", pass: pass}); err == nil || !strings.Contains(err.Error(), "not confirmed") || nonce() != n0 {
		t.Fatalf("not yes: %v, nonce %d", err, nonce())
	}
	if err := agentWallet(ctx, args(serve(2, 1)), &out, &answers{line: "yes", pass: pass}); err == nil || !strings.Contains(err.Error(), "is not for agent 1") || nonce() != n0 {
		t.Fatalf("a consent for another agent: %v, nonce %d", err, nonce())
	}
	journaled := 0
	jpath := filepath.Join(host, "agents.json")
	beforeSend = func(hash string) {
		if raw, _ := os.ReadFile(jpath); strings.Contains(string(raw), hash) {
			journaled++
		}
	}
	defer func() { beforeSend = nil }()
	if err := agentWallet(ctx, args(good), &out, &answers{line: "yes", pass: pass}); err != nil {
		t.Fatalf("%v\n%s", err, out.String())
	}
	if journaled != 2 || nonce() != n0+2 || onChain(1) != addrA || onChain(2) != addrB {
		t.Fatalf("journaled %d, nonce %d (from %d), agentWallets %s %s", journaled, nonce(), n0, onChain(1), onChain(2))
	}
	var j agentsFile
	if readJSON(jpath, &j); j.Agents[0].WalletTx == "" || j.Agents[1].WalletTx == "" {
		t.Fatalf("agents.json: %+v", j.Agents)
	}
	a := &answers{}
	if err := agentWallet(ctx, args(good), &out, a); err != nil || a.asked != 0 || nonce() != n0+2 {
		t.Fatalf("again: %v, asked %d, nonce %d", err, a.asked, nonce())
	}
}
