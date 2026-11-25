package x402

// The offer-receipt extension (Signed Offers & Receipts, docs/commercial-offer-receipt.md):
// a shop signs each 402 offer and each delivery's receipt, EIP-712 in the
// spec's own domains ("x402 offer" and "x402 receipt", version 1, chainId 1:
// an off-chain format only, so no token's domain and no transfer), and
// publishes its key in a did:web document at its origin. Pure functions: the
// shop signs with them, the Host and the checker verify with them.
//
// Read from x402-foundation/x402 specs/extensions/extension-offer-and-receipt.md
// v0.6 (2026-02-04) on 2026-11-25: offers at extensions["offer-receipt"].info.offers[],
// the receipt at the SettlementResponse's extensions["offer-receipt"].info.receipt.
// The EIP-712 type strings are built from that text, not checked against the
// reference SDK.

import (
	"encoding/hex"
	"encoding/json"
	"errors"
	"math/big"
	"net/url"
	"slices"
	"strings"

	"github.com/decred/dcrd/dcrec/secp256k1/v4"
	"github.com/decred/dcrd/dcrec/secp256k1/v4/ecdsa"
	"golang.org/x/crypto/sha3"
)

// OfferReceiptKey is the extension's key in extensions.
const OfferReceiptKey = "offer-receipt"

const (
	orDomainType = "EIP712Domain(string name,string version,uint256 chainId)"
	offerType    = "Offer(uint256 version,string resourceUrl,string scheme,string network,string asset,string payTo,string amount,uint256 validUntil)"
	receiptType  = "Receipt(uint256 version,string network,string resourceUrl,string payer,uint256 issuedAt,string transaction)"
)

// Signed is a signed offer or receipt: EIP-712 only here (format "eip712").
type Signed struct {
	Format      string          `json:"format"`
	Payload     json.RawMessage `json:"payload"`
	Signature   string          `json:"signature"`
	AcceptIndex *int            `json:"acceptIndex,omitempty"` // offers only; unsigned, never relied on
}

// OfferPayload is a signed offer's payload; ValidUntil 0 is absent.
type OfferPayload struct {
	Version     uint64 `json:"version"`
	ResourceURL string `json:"resourceUrl"`
	Scheme      string `json:"scheme"`
	Network     string `json:"network"`
	Asset       string `json:"asset"`
	PayTo       string `json:"payTo"`
	Amount      string `json:"amount"`
	ValidUntil  uint64 `json:"validUntil,omitempty"`
}

// ReceiptPayload is a signed receipt's payload; Transaction "" is absent.
type ReceiptPayload struct {
	Version     uint64 `json:"version"`
	Network     string `json:"network"`
	ResourceURL string `json:"resourceUrl"`
	Payer       string `json:"payer"`
	IssuedAt    uint64 `json:"issuedAt"`
	Transaction string `json:"transaction,omitempty"`
}

func keccak(parts ...[]byte) []byte {
	h := sha3.NewLegacyKeccak256()
	for _, p := range parts {
		h.Write(p)
	}
	return h.Sum(nil)
}

func uintWord(n uint64) []byte {
	out := make([]byte, 32)
	b := new(big.Int).SetUint64(n).Bytes()
	copy(out[32-len(b):], b)
	return out
}

func str(s string) []byte { return keccak([]byte(s)) }

func digest(name string, structHash []byte) []byte {
	sep := keccak(str(orDomainType), str(name), str("1"), uintWord(1))
	return keccak([]byte{0x19, 0x01}, sep, structHash)
}

// OfferDigest and ReceiptDigest are the typed digests their signatures sign.
func OfferDigest(p OfferPayload) []byte {
	return digest("x402 offer", keccak(str(offerType), uintWord(p.Version), str(p.ResourceURL), str(p.Scheme), str(p.Network), str(p.Asset), str(p.PayTo),
		str(p.Amount), uintWord(p.ValidUntil)))
}

func ReceiptDigest(p ReceiptPayload) []byte {
	return digest("x402 receipt", keccak(str(receiptType), uintWord(p.Version), str(p.Network), str(p.ResourceURL), str(p.Payer), uintWord(p.IssuedAt),
		str(p.Transaction)))
}

// Address is the lower-case Ethereum address of a public key.
func Address(pub *secp256k1.PublicKey) string {
	return "0x" + hex.EncodeToString(keccak(pub.SerializeUncompressed()[1:])[12:])
}

// SignOffer and SignReceipt sign a payload with the shop's key.
func SignOffer(key *secp256k1.PrivateKey, p OfferPayload) (Signed, error) {
	return sign(key, p, OfferDigest(p))
}

func SignReceipt(key *secp256k1.PrivateKey, p ReceiptPayload) (Signed, error) {
	return sign(key, p, ReceiptDigest(p))
}

func sign(key *secp256k1.PrivateKey, p any, d []byte) (Signed, error) {
	raw, err := json.Marshal(p)
	if err != nil {
		return Signed{}, err
	}
	c := ecdsa.SignCompact(key, d, false) // RFC 6979, low S; c[0] is 27 + recovery code
	return Signed{Format: "eip712", Payload: raw, Signature: "0x" + hex.EncodeToString(append(append([]byte{}, c[1:65]...), c[0]))}, nil
}

var secpN, _ = new(big.Int).SetString("fffffffffffffffffffffffffffffffebaaedce6af48a03bbfd25e8cd0364141", 16)

var halfN = new(big.Int).Rsh(secpN, 1)

func recoverAddress(sig string, d []byte) (string, error) {
	b, err := hex.DecodeString(strings.TrimPrefix(sig, "0x"))
	if err != nil || len(b) != 65 || !strings.HasPrefix(sig, "0x") || b[64] != 27 && b[64] != 28 || new(big.Int).SetBytes(b[32:64]).Cmp(halfN) > 0 {
		return "", errors.New("the signature is not r, s (low) and v")
	}
	pub, _, err := ecdsa.RecoverCompact(append([]byte{b[64]}, b[:64]...), d)
	if err != nil {
		return "", errors.New("the signature recovers no key")
	}
	return Address(pub), nil
}

// OpenOffer and OpenReceipt decode a signed artifact (strictly: no unknown
// members) and return its payload and the address that signed it.
func OpenOffer(s Signed) (OfferPayload, string, error) {
	var p OfferPayload
	if err := open(s, &p); err != nil {
		return p, "", err
	}
	if p.Version != 1 {
		return p, "", errors.New("an offer of version 1 only")
	}
	by, err := recoverAddress(s.Signature, OfferDigest(p))
	return p, by, err
}

func OpenReceipt(s Signed) (ReceiptPayload, string, error) {
	var p ReceiptPayload
	if err := open(s, &p); err != nil {
		return p, "", err
	}
	if p.Version != 1 {
		return p, "", errors.New("a receipt of version 1 only")
	}
	by, err := recoverAddress(s.Signature, ReceiptDigest(p))
	return p, by, err
}

func open(s Signed, dst any) error {
	if s.Format != "eip712" {
		return errors.New("an eip712 artifact only")
	}
	d := json.NewDecoder(strings.NewReader(string(s.Payload)))
	d.DisallowUnknownFields()
	return d.Decode(dst)
}

// extension is extensions["offer-receipt"] as the shop writes it.
type extension struct {
	Info struct {
		Offers  []Signed `json:"offers,omitempty"`
		Receipt *Signed  `json:"receipt,omitempty"`
	} `json:"info"`
}

// OfferExtensions is a 402's extensions carrying one signed offer.
func OfferExtensions(s Signed) (json.RawMessage, error) {
	var e extension
	e.Info.Offers = []Signed{s}
	return json.Marshal(map[string]extension{OfferReceiptKey: e})
}

// ReceiptExtensions is a settlement response's extensions carrying a receipt.
func ReceiptExtensions(s Signed) (json.RawMessage, error) {
	var e extension
	e.Info.Receipt = &s
	return json.Marshal(map[string]extension{OfferReceiptKey: e})
}

// OffersIn and ReceiptIn read the extension out of extensions: none (nil,
// no error) when it is absent; an error when it is there and malformed.
func OffersIn(ext json.RawMessage) ([]Signed, error) {
	e, ok, err := extOf(ext)
	if !ok || err != nil {
		return nil, err
	}
	return e.Info.Offers, nil
}

func ReceiptIn(ext json.RawMessage) (*Signed, error) {
	e, ok, err := extOf(ext)
	if !ok || err != nil {
		return nil, err
	}
	return e.Info.Receipt, nil
}

func extOf(ext json.RawMessage) (extension, bool, error) {
	var all map[string]json.RawMessage
	if len(ext) == 0 || string(ext) == "null" {
		return extension{}, false, nil
	}
	if err := json.Unmarshal(ext, &all); err != nil {
		return extension{}, false, errors.New("extensions is not an object")
	}
	raw, ok := all[OfferReceiptKey]
	if !ok {
		return extension{}, false, nil
	}
	var e extension
	if err := json.Unmarshal(raw, &e); err != nil {
		return extension{}, false, errors.New("the offer-receipt extension is malformed")
	}
	return e, true, nil
}

// MatchesOffer: a signed offer's payload says what the 402 says (its
// resource URL and the accepts entry it signs), and is not past validUntil.
func MatchesOffer(p OfferPayload, resourceURL string, r Requirement, now uint64) error {
	switch {
	case p.ResourceURL != resourceURL:
		return errors.New("the signed offer is for another resource")
	case p.Scheme != r.Scheme || p.Network != r.Network || !strings.EqualFold(p.Asset, r.Asset) || !strings.EqualFold(p.PayTo, r.PayTo) || p.Amount != string(r.Amount):
		return errors.New("the signed offer is not the 402's terms")
	case p.ValidUntil != 0 && now > p.ValidUntil:
		return errors.New("the signed offer has expired")
	}
	return nil
}

// MatchesReceipt: a signed receipt is for this resource, network, payer and
// settlement transaction (all compared without case where they are hex).
func MatchesReceipt(p ReceiptPayload, resourceURL, network, payer, tx string) error {
	switch {
	case p.ResourceURL != resourceURL || p.Network != network:
		return errors.New("the receipt is for another resource or network")
	case !strings.EqualFold(p.Payer, payer):
		return errors.New("the receipt names another payer")
	case p.Transaction == "" || !strings.EqualFold(p.Transaction, tx):
		return errors.New("the receipt names another settlement transaction")
	}
	return nil
}

// ShopReceipt checks the receipt in a delivered payment's PAYMENT-RESPONSE
// extensions against the purchase's signed offer (its quote's extensions):
// none is needed when the offer was not signed; else one, signed by the
// offer's key, for this purchase's resource, network, payer and tx#2.
func ShopReceipt(quoteExt, responseExt json.RawMessage, resourceURL, network, payer, tx string) (signed bool, err error) {
	offers, err := OffersIn(quoteExt)
	if err != nil || len(offers) == 0 {
		return false, err
	}
	_, offerKey, err := OpenOffer(offers[0])
	if err != nil {
		return true, err
	}
	r, err := ReceiptIn(responseExt)
	if err != nil {
		return true, err
	}
	if r == nil {
		return true, errors.New("a signed offer, and no signed receipt with the content")
	}
	p, by, err := OpenReceipt(*r)
	if err != nil {
		return true, err
	}
	if by != offerKey {
		return true, errors.New("the receipt is not signed by the offer's key")
	}
	return true, MatchesReceipt(p, resourceURL, network, payer, tx)
}

// DIDDocument is the part of a did:web document the offer's key is read from.
type DIDDocument struct {
	Context            []string             `json:"@context"`
	ID                 string               `json:"id"`
	VerificationMethod []VerificationMethod `json:"verificationMethod"`
	AssertionMethod    []string             `json:"assertionMethod"`
}

type VerificationMethod struct {
	ID                  string `json:"id"`
	Type                string `json:"type"`
	Controller          string `json:"controller"`
	BlockchainAccountID string `json:"blockchainAccountId"`
}

// DIDWeb is the did:web of an origin: its host, a port's colon encoded.
func DIDWeb(origin string) (string, error) {
	u, err := url.Parse(origin)
	if err != nil || u.Host == "" {
		return "", errors.New("not an origin")
	}
	return "did:web:" + strings.ReplaceAll(u.Host, ":", "%3A"), nil
}

// ShopDocument is the shop's DID document naming its signing address.
func ShopDocument(did, address string) DIDDocument {
	k := did + "#offer-receipt-1"
	return DIDDocument{Context: []string{"https://www.w3.org/ns/did/v1"}, ID: did,
		VerificationMethod: []VerificationMethod{{ID: k, Type: "EcdsaSecp256k1RecoveryMethod2020", Controller: did, BlockchainAccountID: "eip155:1:" + address}},
		AssertionMethod:    []string{k}}
}

// Authorizes: the document is did's and names address as an assertion key.
func (d DIDDocument) Authorizes(did, address string) bool {
	if d.ID != did {
		return false
	}
	for _, m := range d.VerificationMethod {
		if m.Controller == did && slices.Contains(d.AssertionMethod, m.ID) && strings.EqualFold(m.BlockchainAccountID, "eip155:1:"+address) {
			return true
		}
	}
	return false
}
