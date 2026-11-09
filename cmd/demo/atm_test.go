package main

// The street's ATM (docs/commercial-atm.md) on a fresh local anvil with
// TestUSDC deployed from key 0, as p2-local deploys it: a visit mints once per
// request, 20 a UTC day at most; the testnet's (no owner) only reads. Skips
// without var/tools.

import (
	"io"
	"testing"

	"github.com/SIMPLYBOYS/agent-payment-authority/internal/eth"
	"github.com/SIMPLYBOYS/agent-payment-authority/internal/eth/ethtest"
)

func TestATM(t *testing.T) {
	node := ethtest.Anvil(t)
	token := ethtest.Deploy(t, node, "TestUSDC.sol/TestUSDC")
	wallet, _ := eth.ParseAddress("0x00000000000000000000000000000000000a7a01") // a wallet with no key: the ATM only pays into it
	m := &atm{node: node, chainID: ethtest.Chain, token: token, owner: ethtest.Key(t, 0),
		wallets: map[string]eth.Address{"consumer-a": wallet}, out: io.Discard, done: map[string]map[string]any{}}
	now := uint64(1_762_646_400) // 2025-11-09 00:00 UTC
	m.clock = func() uint64 { return now }

	code, v := m.visit(t.Context(), "atm-req-1", "consumer-a")
	if code != 200 || v["mode"] != "mint" || v["minted"] != "10000000" || v["balance"] != "10000000" || v["address"] != wallet.Hex() || v["tx"] == "" {
		t.Fatalf("the first visit: %d %v", code, v)
	}
	if code, again := m.visit(t.Context(), "atm-req-1", "consumer-a"); code != 200 || again["balance"] != "10000000" || again["tx"] != v["tx"] {
		t.Fatalf("a resend mints again: %d %v", code, again)
	}
	if code, v := m.visit(t.Context(), "atm-req-2", "consumer-a"); code != 200 || v["balance"] != "20000000" {
		t.Fatalf("the second visit: %d %v", code, v)
	}
	// The day's most taken: the ATM says so, mints nothing; the next UTC day it mints again.
	now += 86399
	if code, v := m.visit(t.Context(), "atm-req-3a", "consumer-a"); code != 200 || v["mode"] != "limit" || v["limit"] != "20000000" || v["balance"] != "20000000" || v["minted"] != nil {
		t.Fatalf("over the day's most: %d %v", code, v)
	}
	now++
	if code, v := m.visit(t.Context(), "atm-req-3b", "consumer-a"); code != 200 || v["mode"] != "mint" || v["balance"] != "30000000" {
		t.Fatalf("the next day: %d %v", code, v)
	}
	if code, v := m.visit(t.Context(), "atm-req-3", "consumer-x"); code != 404 || v["error"] != "unknown_consumer" {
		t.Fatalf("an unknown consumer: %d %v", code, v)
	}

	// The testnet's: no owner, it reads only, each time.
	m.owner = nil
	if code, v := m.visit(t.Context(), "atm-req-4", "consumer-a"); code != 200 || v["mode"] != "watch" || v["balance"] != "30000000" || v["minted"] != nil {
		t.Fatalf("watch: %d %v", code, v)
	}
	// Not TestUSDC's owner: the mint reverts, the visit says so.
	m.owner = ethtest.Key(t, 1)
	if code, v := m.visit(t.Context(), "atm-req-5", "consumer-a"); code != 503 || v["error"] != "mint_failed" {
		t.Fatalf("a mint by another key: %d %v", code, v)
	}
}
