package signer

import (
	"bytes"
	"encoding/hex"
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
		"weak scrypt":    func(x *KeyFile) ([]byte, string) { x.N = 1 << 10; return []byte("pass phrase"), "wallet-a" },
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
}
