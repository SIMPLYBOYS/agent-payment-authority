// Package bbs is C1's spike (docs/commercial-c1.md): can a maintained Go
// BBS+ library carry the commercial demo's credential — issue, derive a
// proof that discloses only chosen fields, verify it bound to one verifier,
// one challenge and one purchase intent, with a holder binding, expiry and
// revocation? Synthetic data and software test keys only. Nothing here
// takes part in Policy, the Store, the Host or the Signer (that is C2).
package bbs

import (
	"bytes"
	"crypto/ed25519"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"slices"
	"strconv"
	"strings"

	bbsplus "github.com/trustbloc/bbs-signature-go/bbs12381g2pub"
)

// Fields is the credential's fixed message order (roadmap C2: task_id, C₀,
// expiry, holder binding). Each message is "name=value", so a value cannot
// be moved to another field's position.
var Fields = []string{"schema", "issuer", "task_id", "consumer_id", "c0", "expiry", "holder", "status"}

// Schema is the only schema this spike accepts.
const Schema = "apa.credential.spike.v0"

// Credential is what the issuer signs.
type Credential map[string]string

func (c Credential) messages() ([][]byte, error) {
	out := make([][]byte, len(Fields))
	for i, f := range Fields {
		v, ok := c[f]
		if !ok || v == "" {
			return nil, fmt.Errorf("credential: missing %s", f)
		}
		out[i] = []byte(f + "=" + v)
	}
	return out, nil
}

// Issuer holds a BBS+ key pair (BLS12-381, public key in G2).
type Issuer struct {
	ID string
	sk []byte
	PK []byte
}

func NewIssuer(id string, seed []byte) (*Issuer, error) {
	pk, sk, err := bbsplus.GenerateKeyPair(sha256.New, seed)
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
	return &Issuer{ID: id, sk: skb, PK: pkb}, nil
}

// Issue signs all messages of c (its issuer field is the issuer's ID).
func (is *Issuer) Issue(c Credential) ([]byte, error) {
	if c["issuer"] != is.ID || c["schema"] != Schema {
		return nil, errors.New("issue: issuer or schema")
	}
	m, err := c.messages()
	if err != nil {
		return nil, err
	}
	return bbsplus.New().Sign(m, is.sk)
}

// Request is what a verifier asks for: which fields to disclose, its own
// ID, a one-time challenge and the purchase intent the proof is for.
type Request struct {
	VerifierID, Challenge, IntentHash string
	Disclose                          []string
}

// Nonce binds a proof to one verifier, one challenge and one intent.
func (r Request) Nonce() []byte {
	h := sha256.New()
	for _, p := range []string{"agent-payment-authority/c1-spike/presentation/v0", r.VerifierID, r.Challenge, r.IntentHash} {
		h.Write([]byte(p))
		h.Write([]byte{0})
	}
	return h.Sum(nil)
}

// Presentation is what the holder sends: the disclosed fields, the BBS+
// derived proof and the holder's signature over the nonce and the proof
// (the holder binding: the disclosed holder field is that key).
type Presentation struct {
	Disclosed map[string]string
	Proof     []byte
	HolderSig []byte
}

// Present derives a proof disclosing exactly req.Disclose.
func Present(c Credential, sig, issuerPK []byte, holder ed25519.PrivateKey, req Request) (Presentation, error) {
	m, err := c.messages()
	if err != nil {
		return Presentation{}, err
	}
	var idx []int
	disclosed := map[string]string{}
	for _, f := range req.Disclose {
		i := slices.Index(Fields, f)
		if i < 0 {
			return Presentation{}, fmt.Errorf("present: no field %q", f)
		}
		idx = append(idx, i)
		disclosed[f] = c[f]
	}
	nonce := req.Nonce()
	proof, err := bbsplus.New().DeriveProof(m, sig, nonce, issuerPK, idx)
	if err != nil {
		return Presentation{}, err
	}
	return Presentation{Disclosed: disclosed, Proof: proof, HolderSig: ed25519.Sign(holder, holderPayload(nonce, proof))}, nil
}

func holderPayload(nonce, proof []byte) []byte {
	p := sha256.Sum256(proof)
	return append(append([]byte("agent-payment-authority/c1-spike/holder/v0\x00"), nonce...), p[:]...)
}

// Verifier is one verifier's trust and state: the issuer it trusts, the
// fields it requires, revoked status indexes, used challenges.
type Verifier struct {
	ID       string
	IssuerID string
	IssuerPK []byte
	Required []string
	Revoked  map[string]bool
	used     map[string]bool
}

// Result is what a verified presentation establishes, field by field;
// never a bare "verified".
type Result struct {
	Issuer, Schema, TaskID, Holder, IntentHash, Status string
	Expiry                                             int64
}

// Refusal is a fixed code.
type Refusal string

func (r Refusal) Error() string { return string(r) }

// Verify checks a presentation for req at time now. Every check is a
// fixed code; the proof is checked against this verifier's own nonce.
func (v *Verifier) Verify(req Request, p Presentation, now int64) (Result, error) {
	if req.VerifierID != v.ID {
		return Result{}, Refusal("WRONG_VERIFIER")
	}
	if v.used[req.Challenge] {
		return Result{}, Refusal("CHALLENGE_REPLAYED")
	}
	// Exactly the requested fields, all of them required by this verifier.
	got := make([]string, 0, len(p.Disclosed))
	for f := range p.Disclosed {
		got = append(got, f)
	}
	want := slices.Clone(req.Disclose)
	slices.Sort(got)
	slices.Sort(want)
	if !slices.Equal(got, want) {
		return Result{}, Refusal("DISCLOSURE_MISMATCH")
	}
	for _, f := range v.Required {
		if _, ok := p.Disclosed[f]; !ok {
			return Result{}, Refusal("FIELD_MISSING")
		}
	}
	// The disclosed messages, in field order, as signed.
	var idx []int
	for _, f := range req.Disclose {
		idx = append(idx, slices.Index(Fields, f))
	}
	slices.Sort(idx)
	msgs := make([][]byte, 0, len(idx))
	for _, i := range idx {
		msgs = append(msgs, []byte(Fields[i]+"="+p.Disclosed[Fields[i]]))
	}
	nonce := req.Nonce()
	hp := holderPayload(nonce, p.Proof) // before the library sees the proof: it may rewrite its input (credential_test.go TestLibraryInput)
	if err := bbsplus.New().VerifyProof(msgs, bytes.Clone(p.Proof), nonce, v.IssuerPK); err != nil {
		return Result{}, Refusal("PROOF_INVALID")
	}
	d := p.Disclosed
	if d["schema"] != Schema {
		return Result{}, Refusal("SCHEMA_UNKNOWN")
	}
	if d["issuer"] != v.IssuerID {
		return Result{}, Refusal("ISSUER_UNTRUSTED")
	}
	holder, err := hex.DecodeString(strings.TrimPrefix(d["holder"], "ed25519:"))
	if err != nil || len(holder) != ed25519.PublicKeySize || !ed25519.Verify(holder, hp, p.HolderSig) {
		return Result{}, Refusal("HOLDER_BINDING_INVALID")
	}
	exp, err := strconv.ParseInt(d["expiry"], 10, 64)
	if err != nil {
		return Result{}, Refusal("EXPIRY_INVALID")
	}
	if now >= exp {
		return Result{}, Refusal("CREDENTIAL_EXPIRED")
	}
	if v.Revoked[d["status"]] {
		return Result{}, Refusal("CREDENTIAL_REVOKED")
	}
	if v.used == nil {
		v.used = map[string]bool{}
	}
	v.used[req.Challenge] = true // ponytail: in-memory single use, a persisted challenge store is C2's
	return Result{Issuer: d["issuer"], Schema: d["schema"], TaskID: d["task_id"], Holder: d["holder"], IntentHash: req.IntentHash, Status: d["status"], Expiry: exp}, nil
}
