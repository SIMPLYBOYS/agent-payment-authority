// Package eth is the least of Ethereum P2 needs (docs/commercial-p2.md,
// P2-D4): a JSON-RPC client, ABI encoding of the few types its contracts
// use, and EIP-1559 transactions signed in process with the secp256k1 the
// payment Signer already uses. No go-ethereum; keys never leave the
// process that holds them.
package eth

import (
	"encoding/hex"
	"errors"
	"fmt"
	"strings"

	"github.com/decred/dcrd/dcrec/secp256k1/v4"
	"github.com/decred/dcrd/dcrec/secp256k1/v4/ecdsa"
	"golang.org/x/crypto/sha3"
)

// Keccak256 is Ethereum's hash (the legacy Keccak, not SHA3-256).
func Keccak256(parts ...[]byte) []byte {
	h := sha3.NewLegacyKeccak256()
	for _, p := range parts {
		h.Write(p)
	}
	return h.Sum(nil)
}

// Address is a 20-byte account address.
type Address [20]byte

// ParseAddress reads a 0x-prefixed 40-hex address (any case).
func ParseAddress(s string) (Address, error) {
	var a Address
	b, err := hex.DecodeString(strings.TrimPrefix(strings.TrimPrefix(s, "0x"), "0X"))
	if err != nil || len(b) != 20 || !strings.HasPrefix(strings.ToLower(s), "0x") {
		return a, fmt.Errorf("address %q: 0x and 40 hex digits", s)
	}
	copy(a[:], b)
	return a, nil
}

// Hex is the address in lower-case hex with 0x.
func (a Address) Hex() string { return "0x" + hex.EncodeToString(a[:]) }

// Key is a secp256k1 private key with its address.
type Key struct {
	priv *secp256k1.PrivateKey
	Addr Address
}

// NewKey wraps a 32-byte private key.
func NewKey(secret []byte) (*Key, error) {
	if len(secret) != 32 {
		return nil, errors.New("a private key is 32 bytes")
	}
	priv := secp256k1.PrivKeyFromBytes(secret)
	if priv.Key.IsZero() {
		return nil, errors.New("a zero private key")
	}
	k := &Key{priv: priv}
	copy(k.Addr[:], Keccak256(priv.PubKey().SerializeUncompressed()[1:])[12:])
	return k, nil
}

// Sign returns r ‖ s and the recovery id (0 or 1) of a 32-byte digest:
// RFC 6979, low S.
func (k *Key) Sign(digest []byte) (rs [64]byte, recovery byte, err error) {
	if len(digest) != 32 {
		return rs, 0, errors.New("a digest is 32 bytes")
	}
	c := ecdsa.SignCompact(k.priv, digest, false) // c[0] = 27 + recovery id
	copy(rs[:], c[1:65])
	return rs, c[0] - 27, nil
}

// SignPersonal is an EIP-191 signature r ‖ s ‖ v (v 27 or 28) of a 32-byte
// digest, as the budget contract's policy check recovers it.
func (k *Key) SignPersonal(digest []byte) ([]byte, error) {
	rs, rec, err := k.Sign(Keccak256([]byte("\x19Ethereum Signed Message:\n32"), digest))
	if err != nil {
		return nil, err
	}
	return append(rs[:], 27+rec), nil
}
