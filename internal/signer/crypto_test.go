package signer

import (
	"bytes"
	"encoding/hex"
	"encoding/json"
	"strings"
	"testing"

	"github.com/decred/dcrd/dcrec/secp256k1/v4"
)

func unhex(t *testing.T, s string) []byte {
	t.Helper()
	b, err := hex.DecodeString(strings.TrimPrefix(s, "0x"))
	if err != nil {
		t.Fatal(err)
	}
	return b
}

// The EIP-712 specification's own example (assets/eip-712/Example.js in
// ethereum/EIPs: "Ether Mail", private key keccak256("cow")): its domain
// separator, final digest from the given Mail struct hash, signer address
// and v, r, s. These values come from the specification, not from this code.
func TestEIP712SpecVector(t *testing.T) {
	sep, err := DomainSeparator(Domain{Name: "Ether Mail", Version: "1", ChainID: "1", VerifyingContract: "0xcccccccccccccccccccccccccccccccccccccccc"})
	if err != nil || hex.EncodeToString(sep) != "f2cee375fa42b42143804025fc449deafd50cc031ca257e0b194a650a912090f" {
		t.Fatalf("domain separator %x %v", sep, err)
	}
	digest := TypedDigest(sep, unhex(t, "0xc52c0ee5d84264471806290a3f2c4cecfc5490626bf912d01f240d7a274b371e"))
	if hex.EncodeToString(digest) != "be609aee343fb3c4b28e1df9e632fca64fcfaede20f02e86244efddf30957bd2" {
		t.Fatalf("digest %x", digest)
	}
	key := secp256k1.PrivKeyFromBytes(keccak([]byte("cow")))
	if a := Address(key.PubKey()); a != "0xcd2a3d9f938e13cd947ec05abc7fe734df8dd826" {
		t.Fatalf("address %s", a)
	}
	sig := signDigest(key, digest)
	want := unhex(t, "0x4355c47d63924e8a72e509b65029052eb6c299d53a04e167c5775fd466751c9d"+"07299936d304c153f6443dfa05f40ff007d72911b6f72307f996231605b91562"+"1c")
	if !bytes.Equal(sig, want) {
		t.Fatalf("signature %x", sig)
	}
	if again := signDigest(key, digest); !bytes.Equal(again, sig) {
		t.Fatal("not deterministic")
	}
}

// The TransferWithAuthorization type hash is the constant Circle's
// FiatTokenV2 (EIP3009.sol) publishes; the domain type hash is the one the
// EIP-712 specification's example derives.
func TestTypeHashes(t *testing.T) {
	if h := hex.EncodeToString(keccak([]byte(transferType))); h != "7c7c6cdb67a18743f49ec6fa9b35f50d52ed05cbed4cc592e13b44501c1a2267" {
		t.Fatalf("transfer type hash %s", h)
	}
	if h := hex.EncodeToString(keccak([]byte(domainType))); h != "8b73c3c69bb8fe3d512ecc4cf759cc79239f7b179b0ffacaa9a75d522b39400f" {
		t.Fatalf("domain type hash %s", h)
	}
}

func TestKeyFile(t *testing.T) {
	priv := keccak([]byte("cow"))
	f, err := EncryptKey("wallet-a", priv, []byte("pass phrase"), 1<<14)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(f.Ciphertext, hex.EncodeToString(priv)) || f.Address != "0xcd2a3d9f938e13cd947ec05abc7fe734df8dd826" {
		t.Fatalf("file %+v", f)
	}
	k, err := DecryptKey(f, "wallet-a", []byte("pass phrase"))
	if err != nil || Address(k.PubKey()) != f.Address {
		t.Fatalf("decrypt: %v", err)
	}
	bad := map[string]func(*KeyFile) ([]byte, string){
		"wrong passphrase": func(x *KeyFile) ([]byte, string) { return []byte("other"), "wallet-a" },
		"another wallet":   func(x *KeyFile) ([]byte, string) { return []byte("pass phrase"), "wallet-b" },
		"changed byte": func(x *KeyFile) ([]byte, string) {
			c := []byte(x.Ciphertext)
			c[0] ^= 1
			if c[0] > 'f' || (c[0] > '9' && c[0] < 'a') {
				c[0] = '0'
			}
			x.Ciphertext = string(c)
			return []byte("pass phrase"), "wallet-a"
		},
		"other address": func(x *KeyFile) ([]byte, string) {
			x.Address = "0x" + strings.Repeat("1", 40)
			return []byte("pass phrase"), "wallet-a"
		},
		"other version":  func(x *KeyFile) ([]byte, string) { x.Version = 2; return []byte("pass phrase"), "wallet-a" },
		"not power of 2": func(x *KeyFile) ([]byte, string) { x.N = 1<<14 + 1; return []byte("pass phrase"), "wallet-a" },
	}
	for name, edit := range bad {
		x := f
		pass, wallet := edit(&x)
		if _, err := DecryptKey(x, wallet, pass); err == nil {
			t.Errorf("%s: accepted", name)
		}
	}
	// A file honestly made with a work factor under the bound.
	weak, err := EncryptKey("wallet-a", priv, []byte("pass phrase"), 1<<10)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := DecryptKey(weak, "wallet-a", []byte("pass phrase")); err == nil {
		t.Error("weak scrypt: accepted")
	}
}

// A wallet key file made before the keystore had labels (8157530: the
// fixed bytes 1..32, a test key) still opens as the Signer's, and only as
// the Signer's: /opt's wallets stay readable (docs/commercial-p2b.md B1).
func TestKeyFileFromBeforeLabels(t *testing.T) {
	var f KeyFile
	if err := json.Unmarshal([]byte(`{"version":1,"wallet_id":"wallet-a","address":"0x6370ef2f4db3611d657b90667de398a2cc2a370c","kdf":"scrypt","n":16384,"r":8,"p":1,"salt":"f759a023e8cb3a58f61048b087e729d9","nonce":"c91b52a17380470fb0e31b97","ciphertext":"9135862b32a9b6531de34c5999866bb2c8287d6b7cec8c5481f72c32aff807a601f191f8501701485321b97e8ddfae24"}`), &f); err != nil {
		t.Fatal(err)
	}
	pass := []byte("a fixed test passphrase")
	k, err := DecryptKey(f, "wallet-a", pass)
	if err != nil || Address(k.PubKey()) != "0x6370ef2f4db3611d657b90667de398a2cc2a370c" {
		t.Fatalf("an existing wallet key file: %v", err)
	}
	if _, err := DecryptKeyFor("apa-relayer-key/v1", f, "wallet-a", pass); err == nil {
		t.Fatal("a wallet key file opened under another label")
	}
	// And a file made under one label never opens under another.
	g, err := EncryptKeyFor("apa-relayer-key/v1", "issuer", k.Serialize(), pass, 1<<14)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := DecryptKeyFor("apa-shop-key/v1", g, "issuer", pass); err == nil {
		t.Fatal("a key opened under another program's label")
	}
	if _, err := DecryptKeyFor("apa-relayer-key/v1", g, "issuer", pass); err != nil {
		t.Fatal(err)
	}
}
