package credential

// A presentation to an x402 shop (boundary 4, docs/commercial-credential-ext.md 4):
// derived afresh for each payment, disclosing the schema, the issuer and the
// attributes the shop's 402 asks for, nothing else. No holder, task, expiry
// or status: those are stable values a shop could match across payments and
// shops, so they stay with the Host, which checks them in its own complete
// presentation (Present, Verify). The proof is bound to what the shop itself
// sees of the payment: its did:web, the resource, the accepts entry and the
// EIP-3009 nonce. What the shop cannot check is that the holder presents it
// (no holder binding without disclosing the holder): a link secret signed
// blind would be the next step.

import (
	"bytes"
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"errors"
	"slices"

	"github.com/IBM/idemix/bbs"
)

// ShopPresentationVersion names a presentation to a shop.
const ShopPresentationVersion = "apa.shop-presentation.v1"

// ErrNoAttributes: the credential cannot answer the shop (a v1 credential
// has no attributes, or it lacks one asked for); the payment goes without a
// presentation, which the shop refuses.
var ErrNoAttributes = errors.New("present: the credential has no such attributes to show a shop")

// ShopBinding is what a presentation to a shop is bound to: what the shop
// sees of the payment, so it can recompute the nonce itself.
type ShopBinding struct {
	Verifier     string // the shop's did:web
	ResourceURL  string
	Accepted     []byte // the accepts entry, as the 402 gave it
	PaymentNonce string // the EIP-3009 authorization's nonce
}

// ShopPresentation is the disclosed fields and the BBS+ derived proof.
type ShopPresentation struct {
	Version   string            `json:"version"`
	Disclosed map[string]string `json:"disclosed"`
	Proof     []byte            `json:"proof"`
}

// ShopFields are what a v2 presentation to a shop discloses for requires:
// the schema, the issuer and the asked attributes, in message order.
func ShopFields(requires map[string]string) ([]string, error) {
	out := []string{"schema", "issuer"}
	for _, a := range Attributes {
		if _, ok := requires[a]; ok {
			out = append(out, a)
		}
	}
	if len(out)-2 != len(requires) {
		return nil, errors.New("a shop asks for an attribute the credential does not have")
	}
	return out, nil
}

func (b ShopBinding) nonce(fields []string) []byte {
	acc := sha256.Sum256(b.Accepted)
	h := sha256.New()
	for _, p := range append([]string{"agent-payment-authority/credential/v1/shop-presentation", b.Verifier, b.ResourceURL, hex.EncodeToString(acc[:]), b.PaymentNonce}, fields...) {
		h.Write(binary.BigEndian.AppendUint32(nil, uint32(len(p))))
		h.Write([]byte(p))
	}
	return h.Sum(nil)
}

// PresentToShop derives a presentation of c to a shop for requires.
func PresentToShop(c Credential, sig, issuerPK []byte, b ShopBinding, requires map[string]string) (ShopPresentation, error) {
	if err := c.Check(); err != nil {
		return ShopPresentation{}, err
	}
	if c["schema"] != SchemaV2 {
		return ShopPresentation{}, ErrNoAttributes
	}
	fields, err := ShopFields(requires)
	if err != nil {
		return ShopPresentation{}, ErrNoAttributes
	}
	idx, _ := indexes(SchemaV2, fields)
	disclosed := map[string]string{}
	for _, f := range fields {
		disclosed[f] = c[f]
	}
	proof, err := bbs.New(curve).DeriveProof(c.messages(), sig, b.nonce(fields), issuerPK, idx)
	if err != nil {
		return ShopPresentation{}, err
	}
	return ShopPresentation{Version: ShopPresentationVersion, Disclosed: disclosed, Proof: proof}, nil
}

// PresentToShop answers a shop for task as consumer's holder, from the same
// material and checks as Present.
func (d *Dir) PresentToShop(task, consumer string, b ShopBinding, requires map[string]string) (ShopPresentation, error) {
	c, sig, pk, _, err := d.material(task, consumer)
	if err != nil {
		return ShopPresentation{}, err
	}
	return PresentToShop(c, sig, pk, b, requires)
}

// VerifyAtShop is the shop's check: exactly the fields it asked for, a
// proof over them under its own binding and the issuer's key, the issuer
// it trusts, and the values it requires. It says nothing of the holder,
// the credential's expiry or its revocation (the Host's).
func VerifyAtShop(p ShopPresentation, b ShopBinding, issuerID string, issuerPK []byte, requires map[string]string) error {
	fields, err := ShopFields(requires)
	if err != nil {
		return err
	}
	if p.Version != ShopPresentationVersion || len(p.Disclosed) != len(fields) {
		return errors.New("PRESENTATION_FORMAT")
	}
	msgs := make([][]byte, 0, len(fields))
	for _, f := range fields {
		v, ok := p.Disclosed[f]
		if !ok || !validField(f, v) {
			return errors.New("DISCLOSURE_MISMATCH")
		}
		msgs = append(msgs, []byte(f+"="+v))
	}
	idx, _ := indexes(SchemaV2, fields)
	if pl, err := bbs.ParsePoKPayload(p.Proof); err != nil || pl.MessagesCount != len(fieldsOf(SchemaV2)) || !slices.Equal(pl.Revealed, idx) {
		return errors.New("PROOF_INVALID")
	}
	if err := bbs.New(curve).VerifyProof(msgs, bytes.Clone(p.Proof), b.nonce(fields), issuerPK); err != nil {
		return errors.New("PROOF_INVALID")
	}
	switch {
	case p.Disclosed["schema"] != SchemaV2:
		return errors.New("SCHEMA_MISMATCH")
	case p.Disclosed["issuer"] != issuerID:
		return errors.New("ISSUER_UNTRUSTED")
	}
	for k, want := range requires {
		if want != "" && p.Disclosed[k] != want {
			return errors.New("CLAIM_UNMET")
		}
	}
	return nil
}
