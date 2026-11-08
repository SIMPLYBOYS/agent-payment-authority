package signer

// EIP-712 typed data for EIP-3009 TransferWithAuthorization
// (docs/commercial-p1b-design.md 4.3): the only payment this Signer signs. The
// domain and message are rebuilt from a validated apa.pay.v1 payment binding
// and the trusted asset registry; no digest is taken from a caller. The one
// other thing it signs is a wallet's ERC-8004 consent (AgentWalletSet,
// agentwallet.go), in the registry's domain, for the configured agent only.

import (
	"encoding/hex"
	"errors"
	"math/big"
	"strings"

	"github.com/decred/dcrd/dcrec/secp256k1/v4"
	"github.com/decred/dcrd/dcrec/secp256k1/v4/ecdsa"
	"golang.org/x/crypto/sha3"
)

func keccak(parts ...[]byte) []byte {
	h := sha3.NewLegacyKeccak256()
	for _, p := range parts {
		h.Write(p)
	}
	return h.Sum(nil)
}

const (
	domainType   = "EIP712Domain(string name,string version,uint256 chainId,address verifyingContract)"
	transferType = "TransferWithAuthorization(address from,address to,uint256 value,uint256 validAfter,uint256 validBefore,bytes32 nonce)"
)

// Domain is the EIP-712 domain of a token contract.
type Domain struct {
	Name              string `json:"name"`
	Version           string `json:"version"`
	ChainID           string `json:"chain_id"`
	VerifyingContract string `json:"verifying_contract"`
}

// Transfer is the TransferWithAuthorization message.
type Transfer struct {
	From        string `json:"from"`
	To          string `json:"to"`
	Value       string `json:"value"`
	ValidAfter  uint64 `json:"valid_after"`
	ValidBefore uint64 `json:"valid_before"`
	Nonce       string `json:"nonce"`
}

func word(b []byte) []byte { // left-pad to 32 bytes
	out := make([]byte, 32)
	copy(out[32-len(b):], b)
	return out
}

func uintWord(dec string) ([]byte, error) {
	n, ok := new(big.Int).SetString(dec, 10)
	if !ok || n.Sign() < 0 || n.BitLen() > 256 {
		return nil, errors.New("not a uint256")
	}
	return word(n.Bytes()), nil
}

func hexBytes(s string, n int) ([]byte, error) {
	b, err := hex.DecodeString(strings.TrimPrefix(s, "0x"))
	if err != nil || len(b) != n || !strings.HasPrefix(s, "0x") {
		return nil, errors.New("bad hex length")
	}
	return b, nil
}

// DomainSeparator is hashStruct(EIP712Domain).
func DomainSeparator(d Domain) ([]byte, error) {
	chain, err := uintWord(d.ChainID)
	if err != nil {
		return nil, err
	}
	addr, err := hexBytes(d.VerifyingContract, 20)
	if err != nil {
		return nil, err
	}
	return keccak(keccak([]byte(domainType)), keccak([]byte(d.Name)), keccak([]byte(d.Version)), chain, word(addr)), nil
}

// TransferHash is hashStruct(TransferWithAuthorization).
func TransferHash(m Transfer) ([]byte, error) {
	from, err1 := hexBytes(m.From, 20)
	to, err2 := hexBytes(m.To, 20)
	nonce, err3 := hexBytes(m.Nonce, 32)
	value, err4 := uintWord(m.Value)
	if err := errors.Join(err1, err2, err3, err4); err != nil {
		return nil, err
	}
	after, _ := uintWord(new(big.Int).SetUint64(m.ValidAfter).String())
	before, _ := uintWord(new(big.Int).SetUint64(m.ValidBefore).String())
	return keccak(keccak([]byte(transferType)), word(from), word(to), value, after, before, nonce), nil
}

// agentWalletType is ERC-8004's AgentWalletSet (IdentityRegistryUpgradeable 2.0.0, setAgentWallet), in the
// registry's own domain: ERC8004IdentityRegistry, version 1, its chain and address. No token's domain is
// that, so the signature authorizes no transfer.
const agentWalletType = "AgentWalletSet(uint256 agentId,address newWallet,address owner,uint256 deadline)"

// AgentWalletDigest is the typed digest of the consent of wallet to be a's agent's wallet until deadline.
func AgentWalletDigest(a Agent, wallet string, deadline uint64) ([]byte, error) {
	sep, err := DomainSeparator(Domain{Name: "ERC8004IdentityRegistry", Version: "1", ChainID: a.ChainID, VerifyingContract: a.Registry})
	if err != nil {
		return nil, err
	}
	w, err1 := hexBytes(wallet, 20)
	owner, err2 := hexBytes(a.Owner, 20)
	if err := errors.Join(err1, err2); err != nil {
		return nil, err
	}
	id, _ := uintWord(new(big.Int).SetUint64(a.AgentID).String())
	dl, _ := uintWord(new(big.Int).SetUint64(deadline).String())
	return TypedDigest(sep, keccak(keccak([]byte(agentWalletType)), id, word(w), word(owner), dl)), nil
}

// TypedDigest is keccak256(0x19 0x01 ‖ domainSeparator ‖ structHash).
func TypedDigest(domainSep, structHash []byte) []byte {
	return keccak([]byte{0x19, 0x01}, domainSep, structHash)
}

// Address is the Ethereum address of a public key: the last 20 bytes of
// keccak256 of the uncompressed point without its prefix byte.
func Address(pub *secp256k1.PublicKey) string {
	return "0x" + hex.EncodeToString(keccak(pub.SerializeUncompressed()[1:])[12:])
}

// signDigest returns the 65-byte Ethereum signature r ‖ s ‖ v (v = 27 or
// 28) of a 32-byte digest. decred's SignCompact is RFC 6979 deterministic
// with a low S (BIP 62); its first byte is 27 + the recovery code for an
// uncompressed key, which is Ethereum's v.
func signDigest(key *secp256k1.PrivateKey, digest []byte) []byte {
	c := ecdsa.SignCompact(key, digest, false)
	return append(append([]byte{}, c[1:65]...), c[0])
}
