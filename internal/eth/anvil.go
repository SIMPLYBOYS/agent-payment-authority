package eth

import (
	"context"
	"encoding/hex"
	"errors"
	"fmt"
	"net"
	"net/url"
	"strings"
)

// anvilKeys are anvil's first default keys (its documented test mnemonic):
// public to everyone, for a local chain only.
var anvilKeys = []string{
	"ac0974bec39a17e36ba4a6b4d238ff944bacb478cbed5efcae784d7bf4f2ff80",
	"59c6995e998f97a5a0044966f0945389dc9e86dae88c7a8412f4603b6b78690d",
	"5de4111afa1a4b94908f83103eb1f1706367c2e68ca870fc3fb9f804cdab365a",
	"7c852118294e51e653712a81e05800f419141751be58f605c371e15141b007a6",
	"47e179ec197488593b187f80a00eb0da91f1b9d0b13f8733639f19c30a34926a",
}

// AnvilKey is anvil's i-th public test key. P2-a's demo names its keys by
// this index (H-D5): no key is ever read from a file there.
func AnvilKey(i int) (*Key, error) {
	if i < 0 || i >= len(anvilKeys) {
		return nil, fmt.Errorf("eth: anvil key %d: 0 to %d", i, len(anvilKeys)-1)
	}
	b, _ := hex.DecodeString(anvilKeys[i])
	return NewKey(b)
}

// Anvil refuses a node that is not anvil (web3_clientVersion anvil/...).
func Anvil(ctx context.Context, c *Client) error {
	var v string
	if err := c.Raw(ctx, "web3_clientVersion", &v); err != nil || !strings.HasPrefix(v, "anvil/") {
		return fmt.Errorf("the node is not a local anvil (client %q)", v)
	}
	return nil
}

// LocalRPC refuses any RPC but a local node's over plain http (P2-a: a
// local anvil only; nothing reaches a public chain).
func LocalRPC(raw string) error {
	u, err := url.Parse(raw)
	if err != nil || u.Scheme != "http" || u.User != nil {
		return errors.New("rpc is http://127.0.0.1:<port> (a local anvil)")
	}
	if ip := net.ParseIP(u.Hostname()); u.Hostname() != "localhost" && (ip == nil || !ip.IsLoopback()) {
		return errors.New("rpc is a loopback address (P2-a runs on a local anvil only)")
	}
	return nil
}
