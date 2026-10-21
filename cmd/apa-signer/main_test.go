package main

// The production assembly (design 7): with a valid configuration, unlocked
// test keys and a valid order under a delegation, it signs nothing without
// a reservation section (Unavailable), and with one checks the reservation
// on chain (P2). The keys are one-time test keys made here; nothing is sent
// anywhere (the RPC configured is a closed local port).

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"io"
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

// keygen (D4 of docs/commercial-p1b-os-isolation.md): one new key file, the
// passphrase twice, never a replacement; the production assembly loads it.
func keyDir(t *testing.T) string {
	t.Helper()
	dir := filepath.Join(t.TempDir(), "signer")
	if err := os.MkdirAll(filepath.Join(dir, "keys"), 0o700); err != nil {
		t.Fatal(err)
	}
	os.Chmod(dir, 0o700)
	return dir
}

// answers gives the passphrases in order and counts the questions.
func answers(n *int, ps ...string) func() ([]byte, error) {
	return func() ([]byte, error) {
		*n++
		if len(ps) == 0 {
			return nil, errors.New("no passphrase")
		}
		p := ps[0]
		ps = ps[1:]
		return []byte(p), nil
	}
}

const newPass = "one-time test passphrase"

func TestKeygen(t *testing.T) {
	dir := keyDir(t)
	var out strings.Builder
	n := 0
	if err := keygen(dir, "wallet-test", answers(&n, newPass, newPass), &out); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(dir, "keys", "wallet-test.key")
	fi, err := os.Stat(path)
	if err != nil || fi.Mode().Perm() != 0o600 {
		t.Fatalf("key file mode: %v %v", fi, err)
	}
	raw, _ := os.ReadFile(path)
	var f signer.KeyFile
	json.Unmarshal(raw, &f)
	k, err := signer.DecryptKey(f, "wallet-test", []byte(newPass))
	if err != nil || signer.Address(k.PubKey()) != f.Address {
		t.Fatalf("decrypt: %v", err)
	}
	if got, want := out.String(), "apa-signer: wallet wallet-test address "+f.Address+"\n"; got != want || strings.Contains(got, newPass) {
		t.Fatalf("output %q, want %q", got, want)
	}
	// The real serve assembly loads it (no delegations: nothing can be signed).
	cfg, _ := json.Marshal(map[string]any{"version": 1, "socket": filepath.Join(dir, "s.sock"), "callers": []int{os.Getuid()}, "max_window_seconds": 600,
		"wallets": []map[string]string{{"wallet_id": "wallet-test", "consumer_id": "consumer-test", "chain_id": "84532", "address": f.Address}},
		"assets":  []any{}, "delegations": []any{}})
	os.WriteFile(filepath.Join(dir, "signer.json"), cfg, 0o600)
	core, _, err := assemble(dir, func() ([]byte, error) { return []byte(newPass), nil }, func() uint64 { return t0 })
	if err != nil {
		t.Fatalf("assemble with the new key: %v", err)
	}
	core.Close()

	// Never a replacement: the file stays, no passphrase is asked.
	n = 0
	if err := keygen(dir, "wallet-test", answers(&n, newPass, newPass), io.Discard); err == nil || n != 0 {
		t.Fatalf("an existing key file was replaced: %v, %d questions", err, n)
	}
	if again, _ := os.ReadFile(path); string(again) != string(raw) {
		t.Fatal("an existing key file was replaced")
	}
}

func TestKeygenRefuses(t *testing.T) {
	for name, c := range map[string]struct {
		wallet string
		ps     []string
		edit   func(dir string)
	}{
		"passphrases differ": {"wallet-x", []string{newPass, newPass + "!"}, nil},
		"short passphrase":   {"wallet-x", []string{"short", "short"}, nil},
		"wallet ID":          {"../wallet-x", []string{newPass, newPass}, nil},
		"directory mode":     {"wallet-x", []string{newPass, newPass}, func(dir string) { os.Chmod(dir, 0o750) }},
		"keys mode":          {"wallet-x", []string{newPass, newPass}, func(dir string) { os.Chmod(filepath.Join(dir, "keys"), 0o755) }},
	} {
		t.Run(name, func(t *testing.T) {
			dir := keyDir(t)
			if c.edit != nil {
				c.edit(dir)
			}
			n := 0
			if err := keygen(dir, c.wallet, answers(&n, c.ps...), io.Discard); err == nil {
				t.Fatalf("%s: accepted", name)
			}
			entries, _ := os.ReadDir(filepath.Join(dir, "keys"))
			if len(entries) != 0 {
				t.Fatalf("%s: a file was written", name)
			}
		})
	}
}

// With a reservation section the assembly checks reservations on its chain
// (budget.Verifier), no longer Unavailable; a bad section is refused.
func TestAssemblyChecksReservationsOnChain(t *testing.T) {
	dir, a := signerDir(t)
	path := filepath.Join(dir, "signer.json")
	raw, _ := os.ReadFile(path)
	write := func(r map[string]any) {
		var cfg map[string]any
		json.Unmarshal(raw, &cfg)
		cfg["reservation"] = r
		b, _ := json.Marshal(cfg)
		os.WriteFile(path, b, 0o600)
	}
	good := func() map[string]any {
		return map[string]any{"rpc": "http://127.0.0.1:1", "chain_id": signertest.Chain, "budget_contract": "0x" + strings.Repeat("4", 40), "confirmations": 1}
	}
	for name, change := range map[string]func(map[string]any){
		"another scheme":     func(r map[string]any) { r["rpc"] = "file:///tmp/x" },
		"credentials in URL": func(r map[string]any) { r["rpc"] = "http://u:p@127.0.0.1:1" },
		"an unknown chain":   func(r map[string]any) { r["chain_id"] = "1" },
		"no confirmations":   func(r map[string]any) { r["confirmations"] = 0 },
		"not an address":     func(r map[string]any) { r["budget_contract"] = "0x12" },
	} {
		r := good()
		change(r)
		write(r)
		if _, _, err := assemble(dir, passphrase, func() uint64 { return t0 }); err == nil || !strings.Contains(err.Error(), "reservation") {
			t.Errorf("%s: %v", name, err)
		}
	}
	write(good())
	core, _, err := assemble(dir, passphrase, func() uint64 { return t0 })
	if err != nil {
		t.Fatal(err)
	}
	defer core.Close()
	order := signertest.Order{Consumer: "consumer-a", Agent: "agent-a", Wallet: "wallet-a", From: a, PayTo: signertest.Merchant, Amount: "500000",
		N: 1, Now: t0, Window: 60}.Raw()
	r, err := core.Sign(bg, signer.SignRequest{RequestID: "request-0000000001",
		Authorization: signer.Authorization{Method: "delegation", DelegationID: "dlg-a", DelegationVersion: 1}, Order: order})
	if err == nil || err.Error() != "RESERVATION_MISSING" || r.Authorization != nil {
		t.Fatalf("no reservation: %+v %v", r, err)
	}
	// signer.db is made private: the approver commands open it too (P2 H6).
	if fi, err := os.Stat(filepath.Join(dir, "signer.db")); err != nil || fi.Mode().Perm() != 0o600 {
		t.Fatalf("signer.db mode: %v %v", fi, err)
	}
}

// rekey: wallet-b made under another passphrase is made to open with
// wallet-a's, so serve's one passphrase opens both; the address is printed.
func TestRekeyWallet(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "d")
	os.MkdirAll(filepath.Join(dir, "keys"), 0o700)
	os.Chmod(dir, 0o700)
	const a, b = "wallet a passphrase", "wallet b passphrase"
	ans := func(ps ...string) func() ([]byte, error) {
		return func() ([]byte, error) { p := ps[0]; ps = ps[1:]; return []byte(p), nil }
	}
	if err := keygen(dir, "wallet-b", ans(b, b), io.Discard); err != nil {
		t.Fatal(err)
	}
	var out strings.Builder
	if err := rekey(dir, "wallet-b", ans(b), ans(a, a), &out); err != nil {
		t.Fatal(err)
	}
	f, _ := signer.ReadKeyFile(filepath.Join(dir, "keys", "wallet-b.key"))
	if _, err := signer.DecryptKey(f, "wallet-b", []byte(a)); err != nil || !strings.Contains(out.String(), f.Address) || strings.Contains(out.String(), a) {
		t.Fatalf("%v %q", err, out.String())
	}
	if err := run([]string{"rekey", "--dir", dir}, io.Discard); err == nil || !strings.Contains(err.Error(), "usage") {
		t.Fatalf("no wallet: %v", err)
	}
}
