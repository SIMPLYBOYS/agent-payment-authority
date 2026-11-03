package main

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/SIMPLYBOYS/agent-payment-authority/internal/eth"
	"github.com/SIMPLYBOYS/agent-payment-authority/internal/eth/ethtest"
	"github.com/SIMPLYBOYS/agent-payment-authority/internal/x402"
)

// lines is an io.Writer that keeps what was written, safely across goroutines.
type lines struct {
	mu sync.Mutex
	b  strings.Builder
}

func (l *lines) Write(p []byte) (int, error) { l.mu.Lock(); defer l.mu.Unlock(); return l.b.Write(p) }
func (l *lines) String() string              { l.mu.Lock(); defer l.mu.Unlock(); return l.b.String() }

func noPass() ([]byte, error) { return nil, errors.New("no terminal in a test") }

func payments(t *testing.T, c map[string]any) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "payments.json")
	raw, _ := json.Marshal(c)
	if err := os.WriteFile(path, raw, 0o600); err != nil {
		t.Fatal(err)
	}
	return path
}

// On a local anvil with anvil's shop key: it serves /supported and /health on this machine, and stops when asked.
func TestServesOnALocalAnvil(t *testing.T) {
	rpc := ethtest.Anvil(t)
	usdc := ethtest.Deploy(t, rpc, "TestUSDC.sol/TestUSDC")
	shop, _ := eth.AnvilKey(4)
	dir := filepath.Join(t.TempDir(), "p2")
	os.Mkdir(dir, 0o700)
	path := payments(t, map[string]any{"environment": "local_anvil", "rpc": rpc.URL, "chain_id": ethtest.Chain, "confirmations": 1,
		// As p2-local writes it: the registry's payee (no key) paid, anvil's key 4 paying the gas.
		"token": usdc.Hex(), "token_name": "USDC", "token_version": "2", "pay_to": "0x3333333333333333333333333333333333333333", "anvil_keys": map[string]int{"shop_gas": 4},
		"shop_daily_tx_cap": 5, "dir": dir})
	ctx, cancel := context.WithCancel(context.Background())
	out := &lines{}
	done := make(chan error, 1)
	go func() {
		done <- run(ctx, []string{"serve", "--payments", path, "--listen", "127.0.0.1:0"}, out, noPass)
	}()
	var addr string
	for end := time.Now().Add(10 * time.Second); addr == "" && time.Now().Before(end); time.Sleep(50 * time.Millisecond) {
		if m := regexp.MustCompile(`listening on (http://127\.0\.0\.1:\d+)`).FindStringSubmatch(out.String()); m != nil {
			addr = m[1]
		}
	}
	if addr == "" {
		cancel()
		t.Fatalf("not listening: %s %v", out, <-done)
	}
	var s x402.SupportedResponse
	if res, err := http.Get(addr + "/supported"); err != nil || json.NewDecoder(res.Body).Decode(&s) != nil || len(s.Kinds) != 1 || s.Kinds[0].Network != "eip155:84532" ||
		s.Signers["eip155:*"][0] != shop.Addr.Hex() {
		t.Fatalf("supported: %+v %v", s, err)
	}
	var h struct {
		State      string  `json:"state"`
		SentToday  *uint64 `json:"sent_today"`
		DailyTxCap uint64  `json:"daily_tx_cap"`
	}
	if res, err := http.Get(addr + "/health"); err != nil || json.NewDecoder(res.Body).Decode(&h) != nil || h.State != "ready" || h.SentToday == nil || *h.SentToday != 0 || h.DailyTxCap != 5 {
		t.Fatalf("health: %+v %v", h, err)
	}
	if fi, err := os.Stat(filepath.Join(dir, "settle.db")); err != nil || fi.Mode().Perm() != 0o600 {
		t.Fatalf("its settlements: %v %v", fi, err)
	}
	cancel()
	if err := <-done; err != nil {
		t.Fatalf("stopped: %v", err)
	}
}

// Nothing is started for an address off this machine, a file it cannot use, or the testnet's rules unmet.
func TestRefusesToStart(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "p2")
	os.Mkdir(dir, 0o700)
	good := func() map[string]any {
		return map[string]any{"environment": "base_sepolia", "rpc": "https://sepolia.base.org", "chain_id": 84532, "confirmations": 2,
			"token": "0x036cbd53842c5426634e7929541ec2318f3dcf7e", "token_name": "USDC", "token_version": "2", "pay_to": "0x" + strings.Repeat("3", 40),
			"shop_key_dir": dir, "shop_daily_tx_cap": 4, "dir": dir}
	}
	for name, tc := range map[string]struct {
		args   []string
		change func(map[string]any)
		want   string
	}{
		"no command":          {[]string{}, nil, "usage"},
		"another address":     {[]string{"--listen", "0.0.0.0:8403"}, nil, "loopback"},
		"one confirmation":    {nil, func(c map[string]any) { c["confirmations"] = 1 }, "at least 2 confirmations"},
		"no daily cap":        {nil, func(c map[string]any) { delete(c, "shop_daily_tx_cap") }, "shop_daily_tx_cap"},
		"an http node":        {nil, func(c map[string]any) { c["rpc"] = "http://sepolia.base.org" }, "https URL"},
		"another chain":       {nil, func(c map[string]any) { c["chain_id"] = 8453 }, "is chain 84532"},
		"anvil keys":          {nil, func(c map[string]any) { c["anvil_keys"] = map[string]int{"shop_gas": 4} }, "takes no anvil key"},
		"no token name":       {nil, func(c map[string]any) { delete(c, "token_name") }, "EIP-712 name"},
		"another environment": {nil, func(c map[string]any) { c["environment"] = "base_mainnet" }, "local_anvil or base_sepolia"},
		"a shared dir": {nil, func(c map[string]any) {
			shared := filepath.Join(t.TempDir(), "s")
			os.Mkdir(shared, 0o755)
			c["dir"] = shared
		}, "private to its owner"},
	} {
		c := good()
		if tc.change != nil {
			tc.change(c)
		}
		args := tc.args
		if args == nil || len(args) > 0 {
			args = append([]string{"serve", "--payments", payments(t, c)}, args...)
		}
		if err := run(context.Background(), args, &lines{}, noPass); err == nil || !strings.Contains(err.Error(), tc.want) {
			t.Errorf("%s: %v, want %q", name, err, tc.want)
		}
	}
}
