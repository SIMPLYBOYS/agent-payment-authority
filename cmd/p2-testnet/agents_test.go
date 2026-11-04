package main

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/SIMPLYBOYS/agent-payment-authority/internal/contracts"
	"github.com/SIMPLYBOYS/agent-payment-authority/internal/eth"
	"github.com/SIMPLYBOYS/agent-payment-authority/internal/eth/ethtest"
)

// I1 (docs/commercial-8004.md) on a local anvil, TestAgentRegistry standing
// in for ERC-8004's: nothing is sent without funds or "yes"; the issuer
// registers both Agents, each transaction journaled before it is sent, each
// file on chain as a data: URI naming its own agentId and payment wallet, no
// agentWallet left; the registry names each consumer's Agent. Run again,
// nothing is sent; stopped after a register, it goes on and never registers
// twice; another registration's file is refused.
func TestAgents(t *testing.T) {
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
	var reg contracts.RegistryConfig
	if err := readJSON("../../testdata/demo/registry.json", &reg); err != nil {
		t.Fatal(err)
	}
	writeJSON(filepath.Join(host, "registry.json"), reg)
	jpath := filepath.Join(host, "agents.json")
	args := []string{"--dir", dir, "--rpc", c.URL, "--identity-registry", registry.Hex()}
	nonce := func() uint64 { n, _ := c.PendingNonce(ctx, r["issuer"]); return n }
	call := func(sig string, args ...any) []byte {
		d, _ := eth.Call(sig, args...)
		b, err := c.Call(ctx, eth.Msg{To: registry, Data: d})
		if err != nil {
			t.Fatalf("%s: %v", sig, err)
		}
		return b
	}

	var out strings.Builder
	if err := agents(ctx, args, &out, &answers{}); err == nil || !strings.Contains(err.Error(), "less than the plan") {
		t.Fatalf("no funds: %v", err)
	}
	ethtest.RPC(t, c, "anvil_setBalance", r["issuer"].Hex(), "0xde0b6b3a7640000")
	if err := agents(ctx, args, &out, &answers{line: "no", pass: pass}); err == nil || !strings.Contains(err.Error(), "not confirmed") || nonce() != 0 {
		t.Fatalf("not yes: %v, nonce %d", err, nonce())
	}
	if !strings.Contains(out.String(), `"agentId": <the agentId register mints>`) || !strings.Contains(out.String(), "孩子的購物 Agent") {
		t.Fatalf("the plan shows no file:\n%s", out.String())
	}
	journaled := 0
	beforeSend = func(hash string) {
		if raw, _ := os.ReadFile(jpath); strings.Contains(string(raw), hash) {
			journaled++
		}
	}
	defer func() { beforeSend = nil }()
	if err := agents(ctx, args, &out, &answers{line: "yes", pass: pass}); err != nil {
		t.Fatalf("%v\n%s", err, out.String())
	}
	if journaled != 6 || nonce() != 6 {
		t.Fatalf("%d of 6 journaled before they were sent; nonce %d", journaled, nonce())
	}
	var j agentsFile
	if err := readJSON(jpath, &j); err != nil {
		t.Fatal(err)
	}
	for i, e := range j.Agents {
		id := uint64(i + 1)
		if e.AgentID != id || e.Ref != fmt.Sprintf("eip155:84532/erc721:%s/%d", registry.Hex(), id) || e.PaymentWallet != reg.Wallets[i].From {
			t.Fatalf("agent %d: %+v", i, e)
		}
		uri, err := abiText(call("tokenURI(uint256)", id), 1<<16)
		raw, err2 := base64.StdEncoding.DecodeString(strings.TrimPrefix(uri, "data:application/json;base64,"))
		var f registration
		if err != nil || err2 != nil || json.Unmarshal(raw, &f) != nil || f.Registrations[0].AgentID != id || f.APA.PaymentWallet != e.PaymentWallet || f.APA.PaymentWalletProven {
			t.Fatalf("agent %d's file on chain: %q %v %v", id, uri, err, err2)
		}
		if w, _ := eth.Word(call("getAgentWallet(uint256)", id), 0); string(w) != string(make([]byte, 32)) {
			t.Fatalf("agent %d's agentWallet %x", id, w)
		}
	}
	var got contracts.RegistryConfig
	if readJSON(filepath.Join(host, "registry.json"), &got); got.Consumers[0].AgentRegistryRef != j.Agents[0].Ref || got.Consumers[1].AgentRegistryRef != j.Agents[1].Ref {
		t.Fatalf("the registry's refs: %+v", got.Consumers)
	}

	a := &answers{}
	if err := agents(ctx, args, &out, a); err != nil || a.asked != 0 || nonce() != 6 {
		t.Fatalf("again: %v, asked %d, nonce %d", err, a.asked, nonce())
	}
	j.Agents[1].URITx, j.Agents[1].UnsetTx = "", "" // stopped after B's register
	writeJSON(jpath, j)
	if err := agents(ctx, args, &out, &answers{line: "yes", pass: pass}); err != nil || nonce() != 8 {
		t.Fatalf("on from B's register: %v, nonce %d\n%s", err, nonce(), out.String())
	}
	if n, _ := eth.Uint(call("register()"), 0); n.Uint64() != 3 {
		t.Fatalf("registered again: the next agentId is %d", n)
	}
	j.Owner = eth.Address{0x99}.Hex()
	writeJSON(jpath, j)
	if err := agents(ctx, args, &out, &answers{line: "yes", pass: pass}); err == nil || !strings.Contains(err.Error(), "another registration's") || nonce() != 8 {
		t.Fatalf("another registration's file: %v", err)
	}
}
