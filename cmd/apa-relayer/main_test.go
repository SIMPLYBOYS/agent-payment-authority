package main

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/SIMPLYBOYS/agent-payment-authority/internal/budget"
	"github.com/SIMPLYBOYS/agent-payment-authority/internal/keytool"
)

func TestUsage(t *testing.T) {
	noTTY := func() (*os.File, error) { t.Fatal("asked for a terminal"); return nil, nil }
	for _, args := range [][]string{nil, {"serve"}, {"keygen", "--dir", "d"}, {"keygen", "--dir", "d", "--role", "wallet-a"}, {"keygen", "--role", "shop"}} {
		if err := run(args, io.Discard, noTTY); err == nil {
			t.Fatalf("%v accepted", args)
		}
	}
	// A valid role without a terminal writes nothing.
	dir := filepath.Join(t.TempDir(), "d")
	os.MkdirAll(filepath.Join(dir, "keys"), 0o700)
	os.Chmod(dir, 0o700)
	none := func() (*os.File, error) { return nil, errors.New("no terminal") }
	if err := run([]string{"keygen", "--dir", dir, "--role", "shop"}, io.Discard, none); err == nil {
		t.Fatal("keygen without a terminal")
	}
	if _, err := os.Stat(filepath.Join(dir, "keys", "shop.key")); !os.IsNotExist(err) {
		t.Fatalf("a key file was written: %v", err)
	}
}

// serve: the three keys under one passphrase, the chain checked, then the
// socket (this user's only) answers health for this uid; and its refusals before any
// key is read (docs/commercial-p2b.md B2).
func TestServe(t *testing.T) {
	dir, err := os.MkdirTemp("", "ar") // short: a socket path is at most 104 bytes
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(dir)
	os.Chmod(dir, 0o700)
	os.Mkdir(filepath.Join(dir, "keys"), 0o700)
	const pass = "a long enough passphrase"
	for _, role := range []string{"relayer-gas", "issuer", "policy"} {
		label, _ := keytool.Role(role)
		if _, err := keytool.Keygen(dir, label, role, func() ([]byte, error) { return []byte(pass), nil }); err != nil {
			t.Fatal(err)
		}
	}
	rpc := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		io.WriteString(w, `{"jsonrpc":"2.0","id":1,"result":"0x14a34"}`) // 84532
	}))
	defer rpc.Close()
	sock := filepath.Join(dir, "r.sock")
	good := relayerConfig{Socket: sock, Callers: []uint32{uint32(os.Getuid())}, RPC: rpc.URL, ChainID: 84532,
		BudgetContract: "0x" + strings.Repeat("4", 40), Confirmations: 2, PolicyTTL: 600, Zkbudget: "/opt/zkbudget", ZkSetupDir: "/opt/zk", DailyTxCap: 12}
	write := func(c relayerConfig) {
		raw, _ := json.Marshal(c)
		os.WriteFile(filepath.Join(dir, "relayer.json"), raw, 0o600)
	}
	asked := 0
	ask := func() ([]byte, error) { asked++; return []byte(pass), nil }
	for name, cs := range map[string]struct {
		change func(*relayerConfig)
		want   string
	}{
		"no cap":            {func(c *relayerConfig) { c.DailyTxCap = 0 }, "daily_tx_cap"},
		"a relative prover": {func(c *relayerConfig) { c.Zkbudget = "zkbudget" }, "absolute paths"},
		"another caller":    {func(c *relayerConfig) { c.Callers = []uint32{uint32(os.Getuid()) + 1} }, "this user's uid"},
		"a long policy ttl": {func(c *relayerConfig) { c.PolicyTTL = 601 }, "policy_ttl"},
		"another chain":     {func(c *relayerConfig) { c.ChainID = 8453 }, "not chain 8453"},
		"a socket in place": {func(c *relayerConfig) { os.WriteFile(sock, nil, 0o600) }, "exists"},
	} {
		c := good
		cs.change(&c)
		write(c)
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second) // one served by mistake ends, and fails here
		if err := serve(ctx, dir, ask, io.Discard); err == nil || !strings.Contains(err.Error(), cs.want) {
			t.Errorf("%s: %v, want %q", name, err, cs.want)
		}
		cancel()
		os.Remove(sock)
	}
	if asked != 1 && !t.Failed() { // only "another chain" got as far as the keys
		t.Fatalf("the passphrase was asked %d times", asked)
	}
	write(good)
	ctx, cancel := context.WithCancel(context.Background())
	var out strings.Builder
	done := make(chan error, 1)
	go func() { done <- serve(ctx, dir, ask, &out) }()
	c := budget.NewClient(sock)
	var h budget.Health
	for end := time.Now().Add(60 * time.Second); time.Now().Before(end); time.Sleep(50 * time.Millisecond) { // three scrypt opens first
		if h, err = c.Health(context.Background()); err == nil {
			break
		}
	}
	if err != nil || h.State != "ready" || h.ChainID != 84532 || h.Contract != good.BudgetContract || h.DailyTxCap != 12 || h.Confirmations != 2 {
		cancel()
		t.Fatalf("health %+v %v", h, err)
	}
	if fi, err := os.Stat(sock); err != nil || fi.Mode().Perm()&0o077 != 0 {
		t.Fatalf("socket %v %v", fi, err)
	}
	cancel()
	if err := <-done; err != nil {
		t.Fatal(err)
	}
	if _, err := os.Lstat(sock); !os.IsNotExist(err) {
		t.Fatal("the socket was left behind")
	}
	if strings.Contains(out.String(), pass) || !strings.Contains(out.String(), "at most 12 transactions a day") {
		t.Fatalf("output %q", out.String())
	}
}
