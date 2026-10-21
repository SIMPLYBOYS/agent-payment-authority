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
	for id, want := range map[string]string{"relayer-gas": RelayerLabel, "issuer": RelayerLabel, "policy": RelayerLabel, "shop": ShopLabel, "wallet-a": ""} {
		if got, ok := Role(id); got != want || ok != (want != "") {
			t.Fatalf("role %s: %q %v", id, got, ok)
		}
	}
	// Load refuses a missing file, another ID's file and a keys directory others can write.
	if _, err := Load(dir, RelayerLabel, "policy", []byte(pass)); err == nil {
		t.Fatal("a missing key file loaded")
	}
	os.Rename(filepath.Join(dir, "keys", "issuer.key"), filepath.Join(dir, "keys", "policy.key"))
	if _, err := Load(dir, RelayerLabel, "policy", []byte(pass)); err == nil {
		t.Fatal("the issuer's file loaded as the policy key")
	}
	os.Rename(filepath.Join(dir, "keys", "policy.key"), filepath.Join(dir, "keys", "issuer.key"))
	os.Chmod(filepath.Join(dir, "keys"), 0o777)
	if _, err := Load(dir, RelayerLabel, "issuer", []byte(pass)); err == nil || !strings.Contains(err.Error(), "0700") {
		t.Fatalf("a keys directory others can write: %v", err)
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

// Rekey (P2-b, a wallet made under another passphrase than the others):
// the same key and address under the new passphrase, the old one no longer
// opening it; a wrong current passphrase, new ones that differ or a short
// one change nothing.
func TestRekey(t *testing.T) {
	dir := keyDir(t)
	const old, next = "the first long passphrase", "the second long passphrase"
	addr, err := Keygen(dir, RelayerLabel, "issuer", answers(old, old))
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(dir, "keys", "issuer.key")
	before, _ := os.ReadFile(path)
	for name, c := range map[string]struct {
		cur  string
		news []string
		why  string
	}{
		"a wrong current passphrase":  {old + "x", []string{next, next}, "wrong passphrase"},
		"new passphrases that differ": {old, []string{next, next + "x"}, "differ"},
		"a short new passphrase":      {old, []string{"short", "short"}, "at least"},
	} {
		if _, err := Rekey(dir, RelayerLabel, "issuer", answers(c.cur), answers(c.news...)); err == nil || !strings.Contains(err.Error(), c.why) {
			t.Fatalf("%s: %v", name, err)
		}
		if now, _ := os.ReadFile(path); string(now) != string(before) {
			t.Fatalf("%s changed the file", name)
		}
	}
	got, err := Rekey(dir, RelayerLabel, "issuer", answers(old), answers(next, next))
	if err != nil || got != addr {
		t.Fatalf("rekey: %s %v", got, err)
	}
	if k, err := Load(dir, RelayerLabel, "issuer", []byte(next)); err != nil || k.Addr.Hex() != addr {
		t.Fatalf("the new passphrase: %v", err)
	}
	if _, err := Load(dir, RelayerLabel, "issuer", []byte(old)); err == nil {
		t.Fatal("the old passphrase still opens it")
	}
	if fi, _ := os.Stat(path); fi.Mode().Perm() != 0o600 {
		t.Fatalf("mode %v", fi.Mode())
	}
	if _, err := os.Lstat(path + ".rekey"); !os.IsNotExist(err) {
		t.Fatal("the temporary file was left")
	}
	// A .rekey left by a stopped rekey: refused, the file unchanged.
	os.WriteFile(path+".rekey", nil, 0o600)
	after, _ := os.ReadFile(path)
	if _, err := Rekey(dir, RelayerLabel, "issuer", answers(next), answers(old, old)); err == nil || !strings.Contains(err.Error(), ".rekey") {
		t.Fatalf("a leftover .rekey: %v", err)
	}
	if now, _ := os.ReadFile(path); string(now) != string(after) {
		t.Fatal("a leftover .rekey changed the file")
	}
	os.Remove(path + ".rekey")
	// Under another program's label it does not open, so it is not rekeyed.
	if _, err := Rekey(dir, signer.SignerLabel, "issuer", answers(next), answers(old, old)); err == nil {
		t.Fatal("rekeyed under another label")
	}
}
