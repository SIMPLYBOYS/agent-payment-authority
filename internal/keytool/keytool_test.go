package keytool

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/SIMPLYBOYS/agent-payment-authority/internal/signer"
)

func answers(ps ...string) func() ([]byte, error) {
	return func() ([]byte, error) {
		p := ps[0]
		ps = ps[1:]
		return []byte(p), nil
	}
}

func keyDir(t *testing.T) string {
	dir := filepath.Join(t.TempDir(), "d")
	if err := os.MkdirAll(filepath.Join(dir, "keys"), 0o700); err != nil {
		t.Fatal(err)
	}
	os.Chmod(dir, 0o700)
	return dir
}

// A key file opens only under its own label and ID (docs/commercial-p2b.md
// B1): a relayer key is not a payment wallet, nor the shop's, and back.
func TestKeygenAndLoadByLabel(t *testing.T) {
	dir := keyDir(t)
	const pass = "a long enough passphrase"
	addr, err := Keygen(dir, RelayerLabel, "issuer", answers(pass, pass))
	if err != nil {
		t.Fatal(err)
	}
	k, err := Load(dir, RelayerLabel, "issuer", []byte(pass))
	if err != nil || k.Addr.Hex() != addr {
		t.Fatalf("load: %v %v %s", k, err, addr)
	}
	for _, label := range []string{signer.SignerLabel, ShopLabel} {
		if _, err := Load(dir, label, "issuer", []byte(pass)); err == nil {
			t.Fatalf("a relayer key opened as %s", label)
		}
	}
	if _, err := Load(dir, RelayerLabel, "issuer", []byte(pass+"x")); err == nil {
		t.Fatal("a wrong passphrase opened the key")
	}
	if fi, _ := os.Stat(filepath.Join(dir, "keys", "issuer.key")); fi.Mode().Perm() != 0o600 {
		t.Fatalf("mode %v", fi.Mode())
	}
	// A payment wallet's file (the Signer's label) does not open as the relayer's.
	if _, err := Keygen(dir, signer.SignerLabel, "wallet-a", answers(pass, pass)); err != nil {
		t.Fatal(err)
	}
	if _, err := Load(dir, RelayerLabel, "wallet-a", []byte(pass)); err == nil {
		t.Fatal("a wallet key opened as the relayer's")
	}
	if Roles["shop"] != ShopLabel || Roles["policy"] != RelayerLabel || len(Roles) != 4 {
		t.Fatalf("roles %v", Roles)
	}
}

func TestKeygenRefuses(t *testing.T) {
	dir := keyDir(t)
	const pass = "a long enough passphrase"
	for _, c := range []struct {
		id, why string
		ps      []string
	}{
		{"shop", "differ", []string{pass, pass + "x"}},
		{"shop", "at least", []string{"short", "short"}},
		{"../x", "not valid", []string{pass, pass}},
	} {
		if _, err := Keygen(dir, ShopLabel, c.id, answers(c.ps...)); err == nil || !strings.Contains(err.Error(), c.why) {
			t.Fatalf("%s: %v", c.why, err)
		}
	}
	if _, err := Keygen(dir, ShopLabel, "shop", answers(pass, pass)); err != nil {
		t.Fatal(err)
	}
	if _, err := Keygen(dir, ShopLabel, "shop", answers(pass, pass)); err == nil || !strings.Contains(err.Error(), "exists") {
		t.Fatalf("replaced: %v", err)
	}
	os.Chmod(dir, 0o755)
	if _, err := Keygen(dir, ShopLabel, "policy", answers(pass, pass)); err == nil {
		t.Fatal("a directory others can read")
	}
}
