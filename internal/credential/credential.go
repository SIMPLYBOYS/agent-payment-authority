// Package credential is C2's BBS+ credential (docs/commercial-c2-design.md
// 4-6; selected in C1, docs/commercial-c1.md): the apa.credential.v1
// envelope, the issuer's signature, the holder's derived proof and the
// verifier's checks. Old BBS+ (IBM idemix bbs, bbs12381g2pub), not W3C
// bbs-2023. Issuer, holder and verifier run in one host process here: C2
// proves the wiring and the rules, not independent assurance (design 5).
package credential

import (
	"bytes"
	"crypto/ed25519"
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"errors"
	"fmt"
	"regexp"
	"slices"
	"strconv"
	"strings"

	"github.com/IBM/idemix/bbs"
	ml "github.com/IBM/mathlib"

	"github.com/SIMPLYBOYS/agent-payment-authority/internal/paycontract"
)

var curve = ml.Curves[ml.BLS12_381_BBS]

// Schema names the envelope; it is also the credential's version.
const Schema = "apa.credential.v1"

// SchemaV2 adds the subject's attributes (docs/commercial-venues.md 3.1):
// a venue asks for some of them, and only those are disclosed.
const SchemaV2 = "apa.credential.v2"

// PresentationVersion names the presentation envelope.
const PresentationVersion = "apa.presentation.v1"

// Fields is the fixed message order; each message is "name=value".
var Fields = []string{"schema", "issuer", "task_id", "consumer_id", "c0", "expiry", "holder", "status"}

// Attributes are v2's subject attributes, after Fields, in this order. age_over_18 is a boolean the issuer
// signed, disclosed as it is: not a range proof, and no birth date is in the credential at all.
var Attributes = []string{"buyer_type", "country", "age_over_18"}

// fieldsOf is a schema's message order (v1's for any other name: Check refuses it).
func fieldsOf(schema string) []string {
	if schema == SchemaV2 {
		return slices.Concat(Fields, Attributes)
	}
	return Fields
}

// Mandatory are disclosed in every presentation (consumer_id and c0 never).
var Mandatory = []string{"schema", "issuer", "task_id", "expiry", "holder", "status"}

// C0Placeholder is c0 until Z2 defines C₀ (C2-D5): never disclosed, never
// decided on.
const C0Placeholder = "0x0000000000000000000000000000000000000000000000000000000000000000"

var formats = map[string]*regexp.Regexp{
	"schema": regexp.MustCompile(`^apa\.credential\.v[12]$`),
	"issuer": regexp.MustCompile(`^[a-z0-9-]{1,64}$`),
	"c0":     regexp.MustCompile(`^0x[0-9a-f]{64}$`),
	"expiry": regexp.MustCompile(`^[1-9][0-9]{0,17}$`),
	"holder": regexp.MustCompile(`^ed25519:[0-9a-f]{64}$`),
	"status": regexp.MustCompile(`^(0|[1-9][0-9]{0,8})$`),
	// v2's attributes
	"buyer_type":  regexp.MustCompile(`^(business|individual)$`),
	"country":     regexp.MustCompile(`^[A-Z]{2}$`),
	"age_over_18": regexp.MustCompile(`^(true|false)$`),
}

// validField is the one spelling a field's value may have.
func validField(name, value string) bool {
	if name == "task_id" || name == "consumer_id" {
		return paycontract.ValidID(value)
	}
	rx := formats[name]
	return rx != nil && rx.MatchString(value)
}

// HolderField names an Ed25519 public key as the holder field.
func HolderField(pub ed25519.PublicKey) string { return "ed25519:" + hex.EncodeToString(pub) }

// Credential is what the issuer signs.
type Credential map[string]string

// Check reports a missing or misspelled field.
func (c Credential) Check() error {
	fs := fieldsOf(c["schema"])
	if len(c) != len(fs) {
		return errors.New("credential: exactly the schema's fields")
	}
	for _, f := range fs {
		if !validField(f, c[f]) {
			return fmt.Errorf("credential: %s", f)
		}
	}
	return nil
}

func (c Credential) messages() [][]byte {
	fs := fieldsOf(c["schema"])
	out := make([][]byte, len(fs))
	for i, f := range fs {
		out[i] = []byte(f + "=" + c[f])
	}
	return out
}

// Fingerprint is a public key's (or signature's) SHA-256, in hex.
func Fingerprint(b []byte) string { s := sha256.Sum256(b); return hex.EncodeToString(s[:]) }

// IssuerKey is the issuer's BBS+ key pair (BLS12-381, public key in G2).
type IssuerKey struct {
	ID string
	sk []byte
	PK []byte
}

// NewIssuerKey derives a key pair from seed (32 random bytes at init; a
// fixed seed in tests).
func NewIssuerKey(id string, seed []byte) (*IssuerKey, error) {
	if !formats["issuer"].MatchString(id) {
		return nil, errors.New("issuer: ID")
	}
	pk, sk, err := bbs.NewBBSLib(curve).GenerateKeyPair(sha256.New, seed)
	if err != nil {
		return nil, err
	}
	skb, err := sk.Marshal()
	if err != nil {
		return nil, err
	}
	pkb, err := pk.Marshal()
	if err != nil {
		return nil, err
	}
	return &IssuerKey{ID: id, sk: skb, PK: pkb}, nil
}

// Issue signs a well-formed credential of this issuer.
func (k *IssuerKey) Issue(c Credential) ([]byte, error) {
	if err := c.Check(); err != nil {
		return nil, err
	}
	if c["issuer"] != k.ID {
		return nil, errors.New("issue: another issuer's credential")
	}
	return bbs.New(curve).Sign(c.messages(), k.sk)
}

// Request is a challenge as the verifier stored it: its ID, the verifier,
// the purchase intent it is for and the fields to disclose (design 7);
// Schema the task's credential's ("" is v1).
type Request struct {
	VerifierID, ChallengeID, IntentHash string
	Disclose                            []string
	Schema                              string
}

// indexes is Disclose as sorted message indexes; false for an unknown or
// repeated field.
func indexes(schema string, disclose []string) ([]int, bool) {
	fs := fieldsOf(schema)
	var idx []int
	for _, f := range disclose {
		i := slices.Index(fs, f)
		if i < 0 || slices.Contains(idx, i) {
			return nil, false
		}
		idx = append(idx, i)
	}
	slices.Sort(idx)
	return idx, true
}

// Nonce binds a proof to one verifier, one challenge, one intent and one
// disclosure list; every part is length-prefixed.
func (r Request) Nonce() []byte {
	fs := fieldsOf(r.Schema)
	idx, _ := indexes(r.Schema, r.Disclose)
	list := make([]string, 0, len(idx))
	for _, i := range idx {
		list = append(list, fs[i])
	}
	h := sha256.New()
	for _, p := range append([]string{"agent-payment-authority/credential/v1/presentation", r.VerifierID, r.ChallengeID, r.IntentHash}, list...) {
		h.Write(binary.BigEndian.AppendUint32(nil, uint32(len(p))))
		h.Write([]byte(p))
	}
	return h.Sum(nil)
}

// Presentation is what the holder sends: the disclosed fields, the BBS+
// derived proof and the holder's Ed25519 signature over the nonce and the
// proof (the holder binding; the holder field is that key, so a
// presentation to this verifier is linkable by it, C1-D2).
type Presentation struct {
	Version     string            `json:"version"`
	ChallengeID string            `json:"challenge_id"`
	Disclosed   map[string]string `json:"disclosed"`
	Proof       []byte            `json:"proof"`
	HolderSig   []byte            `json:"holder_sig"`
}

func holderPayload(nonce, proof []byte) []byte {
	p := sha256.Sum256(proof)
	return slices.Concat([]byte("agent-payment-authority/credential/v1/holder\x00"), nonce, p[:])
}

// Present derives a proof disclosing exactly r.Disclose.
func Present(c Credential, sig, issuerPK []byte, holder ed25519.PrivateKey, r Request) (Presentation, error) {
	if err := c.Check(); err != nil {
		return Presentation{}, err
	}
	if schemaOf(r.Schema) != c["schema"] {
		return Presentation{}, errors.New("present: the request is for another schema's credential")
	}
	fs := fieldsOf(c["schema"])
	idx, ok := indexes(c["schema"], r.Disclose)
	if !ok || len(idx) == 0 {
		return Presentation{}, errors.New("present: unknown, repeated or no field")
	}
	disclosed := map[string]string{}
	for _, i := range idx {
		disclosed[fs[i]] = c[fs[i]]
	}
	nonce := r.Nonce()
	proof, err := bbs.New(curve).DeriveProof(c.messages(), sig, nonce, issuerPK, idx)
	if err != nil {
		return Presentation{}, err
	}
	return Presentation{Version: PresentationVersion, ChallengeID: r.ChallengeID, Disclosed: disclosed, Proof: proof,
		HolderSig: ed25519.Sign(holder, holderPayload(nonce, proof))}, nil
}

// schemaOf is a request's schema, v1 when it names none (a challenge stored before v2).
func schemaOf(s string) string {
	if s == "" {
		return Schema
	}
	return s
}

// Expect is what this task's verification must find, from the trusted
// configuration and the task's credential snapshot (design 4, 5).
type Expect struct {
	IssuerID string
	IssuerPK []byte
	Holder   string // the consumer's registered holder field
	TaskID   string
	Expiry   string // the snapshot's
	Status   string // the snapshot's
}

// Results of a verification, in precedence order (design 4).
const (
	Valid             = "valid"
	Absent            = "absent"
	Invalid           = "invalid"
	StatusUnavailable = "status_unavailable"
	Revoked           = "revoked"
	Expired           = "expired"
)

// Outcome is a verification's result, its fixed detail code and what the
// presentation established (empty fields when it established nothing).
type Outcome struct {
	Result, Detail     string
	Issuer, Holder     string
	Status             string
	Expiry             int64
	ProofSHA256        string
	Claims             string // v2: the disclosed attributes, "name=value" joined by "," in Attributes' order
	StatusListVersion  uint64
	StatusListIssuedAt int64
	StatusListSHA256   string
}

func invalid(detail string) Outcome { return Outcome{Result: Invalid, Detail: detail} }

// Status is the status list a verification read, or why none could be
// used (the reader's error makes the result status_unavailable).
type Status struct {
	List   *StatusList
	SHA256 string
	Err    error
}

// Verify checks a presentation against the stored request and what this
// task expects, then the status list, then expiry. The proof bytes are
// hashed before the library sees them and passed as a copy.
func Verify(r Request, p Presentation, e Expect, st Status, now int64) Outcome {
	if p.Version != PresentationVersion || p.ChallengeID != r.ChallengeID {
		return invalid("PRESENTATION_FORMAT")
	}
	fs := fieldsOf(schemaOf(r.Schema))
	idx, ok := indexes(schemaOf(r.Schema), r.Disclose)
	if !ok || len(p.Disclosed) != len(idx) {
		return invalid("DISCLOSURE_MISMATCH")
	}
	for _, i := range idx {
		if _, ok := p.Disclosed[fs[i]]; !ok {
			return invalid("DISCLOSURE_MISMATCH")
		}
	}
	for _, f := range Mandatory {
		if _, ok := p.Disclosed[f]; !ok {
			return invalid("FIELD_MISSING")
		}
	}
	for f, v := range p.Disclosed {
		if !validField(f, v) {
			return invalid("FORMAT_INVALID")
		}
	}
	// The proof reveals exactly these indexes of exactly the schema's
	// messages: the library takes both from the proof (C1 review).
	if pl, err := bbs.ParsePoKPayload(p.Proof); err != nil || pl.MessagesCount != len(fs) || !slices.Equal(pl.Revealed, idx) {
		return invalid("PROOF_INVALID")
	}
	msgs := make([][]byte, 0, len(idx))
	for _, i := range idx {
		msgs = append(msgs, []byte(fs[i]+"="+p.Disclosed[fs[i]]))
	}
	nonce := r.Nonce()
	hp := holderPayload(nonce, p.Proof)
	if err := bbs.New(curve).VerifyProof(msgs, bytes.Clone(p.Proof), nonce, e.IssuerPK); err != nil {
		return invalid("PROOF_INVALID")
	}
	d := p.Disclosed
	switch {
	case d["schema"] != schemaOf(r.Schema):
		return invalid("SCHEMA_MISMATCH")
	case d["issuer"] != e.IssuerID:
		return invalid("ISSUER_UNTRUSTED")
	case d["holder"] != e.Holder:
		return invalid("HOLDER_MISMATCH")
	}
	holder, _ := hex.DecodeString(d["holder"][len("ed25519:"):])
	if !ed25519.Verify(holder, hp, p.HolderSig) {
		return invalid("HOLDER_BINDING_INVALID")
	}
	if d["task_id"] != e.TaskID || d["expiry"] != e.Expiry || d["status"] != e.Status {
		return invalid("SNAPSHOT_MISMATCH")
	}
	exp, _ := strconv.ParseInt(d["expiry"], 10, 64)
	var claims []string
	for _, a := range Attributes {
		if v, ok := d[a]; ok {
			claims = append(claims, a+"="+v)
		}
	}
	o := Outcome{Result: Valid, Issuer: d["issuer"], Holder: d["holder"], Status: d["status"], Expiry: exp, ProofSHA256: Fingerprint(p.Proof),
		Claims: strings.Join(claims, ",")}
	if st.Err != nil || st.List == nil {
		o.Result, o.Detail = StatusUnavailable, "STATUS_LIST_UNAVAILABLE"
		return o
	}
	o.StatusListVersion, o.StatusListIssuedAt, o.StatusListSHA256 = st.List.ListVersion, st.List.IssuedAt, st.SHA256
	switch {
	case slices.Contains(st.List.Revoked, d["status"]):
		o.Result = Revoked
	case now >= exp:
		o.Result = Expired
	}
	return o
}
