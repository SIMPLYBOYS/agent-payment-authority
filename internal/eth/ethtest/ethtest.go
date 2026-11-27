// Package ethtest starts the repository's local anvil and deploys its
// contract build, for tests on a local chain only (var/ is not in git:
// without it the tests skip). Its keys are anvil's documented public ones.
package ethtest

import (
	"bytes"
	"context"
	"encoding/hex"
	"encoding/json"
	"math/big"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"testing"
	"time"

	"github.com/SIMPLYBOYS/agent-payment-authority/internal/anviltest"
	"github.com/SIMPLYBOYS/agent-payment-authority/internal/eth"
)

// Chain is the chain ID the local anvil runs as (the test orders' 84532).
const Chain = 84532

// Key is anvil's i-th funded test key (0 deploys).
func Key(t testing.TB, i int) *eth.Key {
	k, err := eth.AnvilKey(i)
	if err != nil {
		t.Fatal(err)
	}
	return k
}

// Root is the repository's root.
func Root() string {
	_, file, _, _ := runtime.Caller(0)
	return filepath.Join(filepath.Dir(file), "../../..")
}

// Anvil starts a local anvil on a loopback port of its own, or skips the test.
func Anvil(t testing.TB) *eth.Client {
	t.Helper()
	return eth.NewClient(anviltest.Start(t, "--chain-id", strconv.Itoa(Chain)))
}

// Deploy deploys the contract build's artifact (e.g. "TestUSDC.sol/TestUSDC")
// from key 0 with its encoded constructor arguments.
func Deploy(t testing.TB, c *eth.Client, artifact string, ctor ...byte) eth.Address {
	t.Helper()
	raw, err := os.ReadFile(filepath.Join(Root(), "var/contracts/out", artifact+".json"))
	if err != nil {
		t.Skip("no contract build (var/contracts/out):", err)
	}
	var a struct{ Bytecode struct{ Object string } }
	json.Unmarshal(raw, &a)
	code, err := hex.DecodeString(a.Bytecode.Object[2:])
	if err != nil {
		t.Fatal(err)
	}
	rc := Mine(t, c, Key(t, 0), &eth.Tx{Gas: 6_000_000, Data: append(code, ctor...)})
	if rc.ContractAddress == nil {
		t.Fatalf("deploy %s: no address", artifact)
	}
	return *rc.ContractAddress
}

// Mine sends tx (chain, nonce and fees filled in) from k and returns its
// receipt once mined successfully.
func Mine(t testing.TB, c *eth.Client, k *eth.Key, tx *eth.Tx) *eth.Receipt {
	t.Helper()
	ctx := context.Background()
	tx.ChainID = big.NewInt(Chain)
	if tx.Value == nil {
		tx.Value = new(big.Int)
	}
	tx.Nonce, _ = c.PendingNonce(ctx, k.Addr)
	h, _ := c.LatestHeader(ctx)
	tx.MaxPriorityFeePerGas, tx.MaxFeePerGas = big.NewInt(1e9), new(big.Int).Add(new(big.Int).Mul(h.BaseFee, big.NewInt(2)), big.NewInt(1e9))
	signed, hash, err := tx.Sign(k)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := c.SendRaw(ctx, signed); err != nil {
		t.Fatal(err)
	}
	for range 200 {
		if rc, _ := c.Receipt(ctx, hash); rc != nil {
			if rc.Status != 1 {
				t.Fatalf("reverted: %+v", rc)
			}
			return rc
		}
		time.Sleep(50 * time.Millisecond)
	}
	t.Fatal("not mined")
	return nil
}

// RPC calls a node method the client has no wrapper for (anvil's own).
func RPC(t testing.TB, c *eth.Client, method string, params ...any) {
	t.Helper()
	if params == nil {
		params = []any{}
	}
	body, _ := json.Marshal(map[string]any{"jsonrpc": "2.0", "id": 1, "method": method, "params": params})
	res, err := http.Post(c.URL, "application/json", bytes.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	res.Body.Close()
}

// Now is the latest block's time.
func Now(t testing.TB, c *eth.Client) uint64 {
	h, err := c.LatestHeader(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	return h.Timestamp
}
