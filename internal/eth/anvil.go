package eth

import (
	"encoding/hex"
	"fmt"
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
