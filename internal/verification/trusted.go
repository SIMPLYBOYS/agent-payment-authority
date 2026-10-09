package verification

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"

	"github.com/SIMPLYBOYS/agent-payment-authority/internal/contracts"
)

// configFiles are the files a fixture bundle's config digest is made of, in
// the producer's documented order (docs/m1-evidence.md section 3).
var configFiles = []string{"testdata/contracts/cases.json", "testdata/contracts/registry.json", "testdata/contracts/purchase-input.json",
	"testdata/demo/registry.json", "testdata/policy/m1-demo-profile.json", "testdata/cli/ask-mainline.json"}

// Trusted is what the checker takes from the repository, never from the
// bundle: the frozen accepted case list (expected outcomes, parameters),
// the purchase and registry the contracts cases start from, and the
// configuration files' digests.
type Trusted struct {
	Cases       contracts.CaseManifest
	CasesDigest string
	Purchase    contracts.PurchaseOrder
	Registry    contracts.RegistryConfig
	registry    *contracts.Registry
	Files       map[string]string // path -> sha256 digest of the bytes
	missing     []string
}

func sha(b []byte) string {
	h := sha256.Sum256(b)
	return "sha256:" + hex.EncodeToString(h[:])
}

// LoadTrusted reads the trusted inputs from the repository at repo. The
// case list must be there and accepted; a missing fixture is recorded, and
// the claims that need it are INCONCLUSIVE.
func LoadTrusted(repo string) (Trusted, error) {
	t := Trusted{Files: map[string]string{}}
	raw := map[string][]byte{}
	for _, f := range configFiles {
		b, err := os.ReadFile(filepath.Join(repo, filepath.FromSlash(f)))
		if err != nil {
			t.missing = append(t.missing, f)
			continue
		}
		raw[f], t.Files[f] = b, sha(b)
	}
	cases, ok := raw["testdata/contracts/cases.json"]
	if !ok {
		return t, fmt.Errorf("the frozen case list testdata/contracts/cases.json is not in %s", repo)
	}
	if err := contracts.Decode(cases, &t.Cases); err != nil {
		return t, fmt.Errorf("case list: %w", err)
	}
	if err := t.Cases.RequireAccepted(); err != nil {
		return t, fmt.Errorf("case list: %w", err)
	}
	var err error
	if t.CasesDigest, err = t.Cases.Hash(); err != nil {
		return t, err
	}
	if b, ok := raw["testdata/contracts/purchase-input.json"]; ok {
		if err := contracts.Decode(b, &t.Purchase); err != nil {
			return t, fmt.Errorf("trusted purchase: %w", err)
		}
	}
	if b, ok := raw["testdata/contracts/registry.json"]; ok {
		if err := contracts.Decode(b, &t.Registry); err != nil {
			return t, fmt.Errorf("trusted registry: %w", err)
		}
		if t.registry, err = contracts.NewRegistry(t.Registry); err != nil {
			return t, fmt.Errorf("trusted registry: %w", err)
		}
	}
	return t, nil
}

// identity is a consumer's trusted binding: its agent, wallet and payer,
// from the trusted registry.
type identity struct{ consumer, agent, wallet, from string }

func (t Trusted) identity(consumer string) (identity, bool) {
	for _, c := range t.Registry.Consumers {
		if c.ConsumerID != consumer {
			continue
		}
		for _, w := range t.Registry.Wallets {
			if w.WalletID == c.WalletID {
				return identity{c.ConsumerID, c.AgentID, c.WalletID, w.From}, true
			}
		}
	}
	return identity{}, false
}
