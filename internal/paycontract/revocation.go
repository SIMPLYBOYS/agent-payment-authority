package paycontract

// The Signer's revocation receipt (docs/commercial-gap4.md 7.1): its digest
// and its signer, pure functions the Signer signs with and the checker
// verifies with. EIP-712, in a domain of its own (no token or registry is
// named so), so the payer wallet's signature over it authorizes no
// transfer and no consent.

import (
	"encoding/hex"
	"errors"
	"math/big"
	"slices"
	"strings"

	"github.com/SIMPLYBOYS/agent-payment-authority/internal/contracts"
	"github.com/decred/dcrd/dcrec/secp256k1/v4/ecdsa"
	"golang.org/x/crypto/sha3"
)

const (
	receiptDomainType = "EIP712Domain(string name,string version,uint256 chainId,address verifyingContract)"
	receiptType       = "DelegationRevoked(string delegationId,string revocationId,uint256 revokedAt,address wallet,bytes32 signedIntents)"
	// ReceiptDomainName: the domain's name, its verifying contract the zero address.
	ReceiptDomainName = "AgentPaymentAuthority Signer"
)

func keccak(parts ...[]byte) []byte {
	h := sha3.NewLegacyKeccak256()
	for _, p := range parts {
		h.Write(p)
	}
	return h.Sum(nil)
}

func word(b []byte) []byte {
	out := make([]byte, 32)
	copy(out[32-len(b):], b)
	return out
}

func addressBytes(s string) ([]byte, error) {
	b, err := hex.DecodeString(strings.TrimPrefix(s, "0x"))
	if err != nil || len(b) != 20 || !strings.HasPrefix(s, "0x") || strings.ToLower(s) != s {
		return nil, errors.New("not a lower-case address")
	}
	return b, nil
}

// RevocationDigest is the typed digest a receipt's signature signs. The
// intents must be sha256 intent hashes, sorted and distinct.
func RevocationDigest(r contracts.RevocationReceipt) ([]byte, error) {
	chain, ok := new(big.Int).SetString(r.ChainID, 10)
	wallet, err := addressBytes(r.Wallet)
	if !ok || chain.Sign() <= 0 || chain.BitLen() > 256 || err != nil || !ids(r.DelegationID, r.RevocationID) {
		return nil, errors.New("a receipt names a delegation, a revocation, a chain and a wallet")
	}
	if !slices.IsSorted(r.SignedIntents) || len(slices.Compact(slices.Clone(r.SignedIntents))) != len(r.SignedIntents) {
		return nil, errors.New("a receipt's intents are sorted and distinct")
	}
	var all []byte
	for _, in := range r.SignedIntents {
		h, err := hex.DecodeString(strings.TrimPrefix(in, "sha256:"))
		if err != nil || len(h) != 32 || !strings.HasPrefix(in, "sha256:") {
			return nil, errors.New("a receipt's intent is a sha256 intent hash")
		}
		all = append(all, h...)
	}
	sep := keccak(keccak([]byte(receiptDomainType)), keccak([]byte(ReceiptDomainName)), keccak([]byte("1")), word(chain.Bytes()), word(nil))
	at := new(big.Int).SetUint64(r.RevokedAt).Bytes()
	st := keccak(keccak([]byte(receiptType)), keccak([]byte(r.DelegationID)), keccak([]byte(r.RevocationID)), word(at), word(wallet), keccak(all))
	return keccak([]byte{0x19, 0x01}, sep, st), nil
}

// ReceiptSigner is the address whose key signed the receipt (r ‖ s ‖ v, v 27 or 28, low S).
func ReceiptSigner(r contracts.RevocationReceipt) (string, error) {
	d, err := RevocationDigest(r)
	if err != nil {
		return "", err
	}
	sig, err := hex.DecodeString(strings.TrimPrefix(r.Signature, "0x"))
	if err != nil || len(sig) != 65 || sig[64] != 27 && sig[64] != 28 || !strings.HasPrefix(r.Signature, "0x") {
		return "", errors.New("a receipt's signature is r, s and v")
	}
	halfN := new(big.Int).Rsh(ecdsaN, 1)
	if new(big.Int).SetBytes(sig[32:64]).Cmp(halfN) > 0 {
		return "", errors.New("a receipt's signature has a low S")
	}
	pub, _, err := ecdsa.RecoverCompact(append([]byte{sig[64]}, sig[:64]...), d)
	if err != nil {
		return "", errors.New("a receipt's signature recovers no key")
	}
	return "0x" + hex.EncodeToString(keccak(pub.SerializeUncompressed()[1:])[12:]), nil
}

var ecdsaN, _ = new(big.Int).SetString("fffffffffffffffffffffffffffffffebaaedce6af48a03bbfd25e8cd0364141", 16)
