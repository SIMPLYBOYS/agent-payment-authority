package main

// The production assembly (design 7): with a valid configuration, unlocked
// test keys and a valid order under a delegation, it still signs nothing,
// because its reservation verifier is Unavailable until P2. The keys are
// one-time test keys made here; nothing is sent anywhere.

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/SIMPLYBOYS/agent-payment-authority/internal/signer"
	"github.com/SIMPLYBOYS/agent-payment-authority/internal/signer/signertest"
	"github.com/decred/dcrd/dcrec/secp256k1/v4"
)

const t0 = 1790726450

var pass = []byte("test passphrase, not a real one")

var bg = context.Background()

// signerDir writes a private Signer directory with two encrypted test keys
// and returns it with wallet-a's address.
func signerDir(t *testing.T) (string, string) {
	t.Helper()
	dir := filepath.Join(t.TempDir(), "signer")
	if err := os.MkdirAll(filepath.Join(dir, "keys"), 0o700); err != nil {
		t.Fatal(err)
	}
	os.Chmod(dir, 0o700)
	addr := map[string]string{}
	for _, w := range []string{"a", "b"} {
		priv := sha256.Sum256([]byte("apa-signer assembly test wallet " + w))
		f, err := signer.EncryptKey("wallet-"+w, priv[:], pass, 1<<14)
		if err != nil {
			t.Fatal(err)
		}
		addr[w] = signer.Address(secp256k1.PrivKeyFromBytes(priv[:]).PubKey())
		raw, _ := json.Marshal(f)
		if err := os.WriteFile(filepath.Join(dir, "keys", "wallet-"+w+".key"), raw, 0o600); err != nil {
			t.Fatal(err)
		}
	}
	cfg := signertest.Config(filepath.Join(dir, "s.sock"), uint32(os.Getuid()), t0, addr["a"], addr["b"])
	if err := os.WriteFile(filepath.Join(dir, "signer.json"), cfg, 0o600); err != nil {
		t.Fatal(err)
	}
	return dir, addr["a"]
}

func passphrase() ([]byte, error) { return append([]byte(nil), pass...), nil }

func TestAssemblyRefusesEveryOrder(t *testing.T) {
	dir, a := signerDir(t)
	core, _, err := assemble(dir, passphrase, func() uint64 { return t0 })
	if err != nil {
		t.Fatal(err)
	}
	defer core.Close()
	order := signertest.Order{Consumer: "consumer-a", Agent: "agent-a", Wallet: "wallet-a", From: a, PayTo: signertest.Merchant, Amount: "500000",
		N: 1, Now: t0, Window: 60}.Raw()
	r, err := core.Sign(bg, signer.SignRequest{RequestID: "request-0000000001",
		Authorization: signer.Authorization{Method: "delegation", DelegationID: "dlg-a", DelegationVersion: 1}, Order: order})
	if err == nil || err.Error() != "RESERVATION_UNAVAILABLE" || r.Authorization != nil {
		t.Fatalf("the production assembly signed: %+v %v", r, err)
	}
	if _, err := core.Get(bg, "", "request-0000000001"); err == nil || err.Error() != "ATTEMPT_UNKNOWN" {
		t.Fatalf("an attempt was kept: %v", err)
	}
}

func TestAssemblyRefusesItsInputs(t *testing.T) {
	clock := func() uint64 { return t0 }
	dir, _ := signerDir(t)
	if _, _, err := assemble(dir, func() ([]byte, error) { return []byte("another passphrase"), nil }, clock); err == nil || !strings.Contains(err.Error(), "wrong passphrase") {
		t.Fatalf("wrong passphrase: %v", err)
	}
	os.Chmod(filepath.Join(dir, "keys", "wallet-b.key"), 0o640)
	if _, _, err := assemble(dir, passphrase, clock); err == nil || !strings.Contains(err.Error(), "private to its owner") {
		t.Fatalf("group-readable key file: %v", err)
	}
	os.Chmod(filepath.Join(dir, "keys", "wallet-b.key"), 0o600)
	os.Chmod(dir, 0o750)
	if _, _, err := assemble(dir, passphrase, clock); err == nil || !strings.Contains(err.Error(), "mode 0700") {
		t.Fatalf("group-readable directory: %v", err)
	}
	os.Chmod(dir, 0o700)
	// Wallet b's file swapped in for wallet a: the associated data binds it.
	raw, _ := os.ReadFile(filepath.Join(dir, "keys", "wallet-b.key"))
	os.WriteFile(filepath.Join(dir, "keys", "wallet-a.key"), raw, 0o600)
	if _, _, err := assemble(dir, passphrase, clock); err == nil {
		t.Fatal("a key file of another wallet was accepted")
	}
}
