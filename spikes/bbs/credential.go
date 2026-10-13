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
	"crypto/rand"
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"errors"
	"fmt"
	"regexp"
	"slices"
	"strconv"
	"sync"

	"github.com/IBM/idemix/bbs"
	ml "github.com/IBM/mathlib"
)

var curve = ml.Curves[ml.BLS12_381_BBS]

// Fields is the credential's fixed message order (roadmap C2: task_id, C₀,
// expiry, holder binding). Each message is "name=value", so a value cannot
// be moved to another field's position.
var Fields = []string{"schema", "issuer", "task_id", "consumer_id", "c0", "expiry", "holder", "status"}

// Mandatory are disclosed in every presentation, whatever a verifier is
// configured with (review: a revocation check must not depend on settings).
var Mandatory = []string{"schema", "issuer", "task_id", "expiry", "holder", "status"}

// Schema is the only schema this spike accepts.
const Schema = "apa.credential.spike.v0"

// The issuer-signed formats, one spelling each (review: no "+", leading
// zeros or optional prefixes).
var format = map[string]*regexp.Regexp{
	"expiry": regexp.MustCompile(`^[1-9][0-9]{0,17}$`),
	"holder": regexp.MustCompile(`^ed25519:[0-9a-f]{64}$`),
	"status": regexp.MustCompile(`^(0|[1-9][0-9]{0,8})$`),
}

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
	return &Issuer{ID: id, sk: skb, PK: pkb}, nil
}

// Issue signs all messages of c (its issuer field is the issuer's ID).
func (is *Issuer) Issue(c Credential) ([]byte, error) {
	if c["issuer"] != is.ID || c["schema"] != Schema {
		return nil, errors.New("issue: issuer or schema")
	}
	for f, rx := range format {
		if !rx.MatchString(c[f]) {
			return nil, fmt.Errorf("issue: %s format", f)
		}
	}
	m, err := c.messages()
	if err != nil {
		return nil, err
	}
	return bbs.New(curve).Sign(m, is.sk)
}

// Request is what a verifier asks for: which fields to disclose, its own
// ID, a challenge it issued and the purchase intent the proof is for.
type Request struct {
	VerifierID, Challenge, IntentHash string
	Disclose                          []string
}

// Nonce binds a proof to one verifier, one challenge and one intent; each
// part is length-prefixed, so no two requests share a nonce.
func (r Request) Nonce() []byte {
	h := sha256.New()
	for _, p := range []string{"agent-payment-authority/c1-spike/presentation/v1", r.VerifierID, r.Challenge, r.IntentHash} {
		h.Write(binary.BigEndian.AppendUint32(nil, uint32(len(p))))
		h.Write([]byte(p))
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

// indexes is Disclose as sorted message indexes; false for an unknown
// field or a repeated one.
func indexes(disclose []string) ([]int, bool) {
	var idx []int
	for _, f := range disclose {
		i := slices.Index(Fields, f)
		if i < 0 || slices.Contains(idx, i) {
			return nil, false
		}
		idx = append(idx, i)
	}
	slices.Sort(idx)
	return idx, true
}

// Present derives a proof disclosing exactly req.Disclose.
func Present(c Credential, sig, issuerPK []byte, holder ed25519.PrivateKey, req Request) (Presentation, error) {
	m, err := c.messages()
	if err != nil {
		return Presentation{}, err
	}
	idx, ok := indexes(req.Disclose)
	if !ok {
		return Presentation{}, errors.New("present: unknown or repeated field")
	}
	disclosed := map[string]string{}
	for _, i := range idx {
		disclosed[Fields[i]] = c[Fields[i]]
	}
	nonce := req.Nonce()
	proof, err := bbs.New(curve).DeriveProof(m, sig, nonce, issuerPK, idx)
	if err != nil {
		return Presentation{}, err
	}
	return Presentation{Disclosed: disclosed, Proof: proof, HolderSig: ed25519.Sign(holder, holderPayload(nonce, proof))}, nil
}

func holderPayload(nonce, proof []byte) []byte {
	p := sha256.Sum256(proof)
	return append(append([]byte("agent-payment-authority/c1-spike/holder/v1\x00"), nonce...), p[:]...)
}

// Verifier is one verifier's trust and state: the issuer it trusts,
// revoked status indexes, the challenges it issued and has not seen used.
type Verifier struct {
	ID       string
	IssuerID string
	IssuerPK []byte
	Revoked  map[string]bool

	mu          sync.Mutex
	outstanding map[string]bool // ponytail: in memory; a persisted challenge store is C2's (C1-D4)
}

// NewChallenge issues a one-time challenge.
func (v *Verifier) NewChallenge() string {
	b := make([]byte, 16)
	rand.Read(b)
	c := hex.EncodeToString(b)
	v.mu.Lock()
	defer v.mu.Unlock()
	if v.outstanding == nil {
		v.outstanding = map[string]bool{}
	}
	v.outstanding[c] = true
	return c
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
// fixed code; the proof is checked against this verifier's own nonce and
// against the exact disclosed set and message count. A challenge is used
// up only by a presentation that passes (a failed one may be retried).
func (v *Verifier) Verify(req Request, p Presentation, now int64) (Result, error) {
	if req.VerifierID != v.ID {
		return Result{}, Refusal("WRONG_VERIFIER")
	}
	v.mu.Lock()
	defer v.mu.Unlock()
	if !v.outstanding[req.Challenge] {
		return Result{}, Refusal("CHALLENGE_UNKNOWN") // never issued, or already used
	}
	idx, ok := indexes(req.Disclose)
	if !ok {
		return Result{}, Refusal("DISCLOSURE_MISMATCH")
	}
	// Exactly the requested fields, every mandatory one among them.
	if len(p.Disclosed) != len(idx) {
		return Result{}, Refusal("DISCLOSURE_MISMATCH")
	}
	for _, i := range idx {
		if _, ok := p.Disclosed[Fields[i]]; !ok {
			return Result{}, Refusal("DISCLOSURE_MISMATCH")
		}
	}
	for _, f := range Mandatory {
		if _, ok := p.Disclosed[f]; !ok {
			return Result{}, Refusal("FIELD_MISSING")
		}
	}
	// The proof must reveal exactly these indexes of exactly len(Fields)
	// messages (review must-fix: the library takes both from the proof).
	if pl, err := bbs.ParsePoKPayload(p.Proof); err != nil || pl.MessagesCount != len(Fields) || !slices.Equal(pl.Revealed, idx) {
		return Result{}, Refusal("PROOF_INVALID")
	}
	msgs := make([][]byte, 0, len(idx))
	for _, i := range idx {
		msgs = append(msgs, []byte(Fields[i]+"="+p.Disclosed[Fields[i]]))
	}
	nonce := req.Nonce()
	hp := holderPayload(nonce, p.Proof)
	if err := bbs.New(curve).VerifyProof(msgs, bytes.Clone(p.Proof), nonce, v.IssuerPK); err != nil {
		return Result{}, Refusal("PROOF_INVALID")
	}
	d := p.Disclosed
	if d["schema"] != Schema {
		return Result{}, Refusal("SCHEMA_UNKNOWN")
	}
	if d["issuer"] != v.IssuerID {
		return Result{}, Refusal("ISSUER_UNTRUSTED")
	}
	if !format["holder"].MatchString(d["holder"]) {
		return Result{}, Refusal("HOLDER_BINDING_INVALID")
	}
	holder, _ := hex.DecodeString(d["holder"][len("ed25519:"):])
	if !ed25519.Verify(holder, hp, p.HolderSig) {
		return Result{}, Refusal("HOLDER_BINDING_INVALID")
	}
	if !format["expiry"].MatchString(d["expiry"]) {
		return Result{}, Refusal("EXPIRY_INVALID")
	}
	exp, _ := strconv.ParseInt(d["expiry"], 10, 64)
	if now >= exp {
		return Result{}, Refusal("CREDENTIAL_EXPIRED")
	}
	if !format["status"].MatchString(d["status"]) {
		return Result{}, Refusal("STATUS_INVALID")
	}
	if v.Revoked[d["status"]] {
		return Result{}, Refusal("CREDENTIAL_REVOKED")
	}
	delete(v.outstanding, req.Challenge)
	return Result{Issuer: d["issuer"], Schema: d["schema"], TaskID: d["task_id"], Holder: d["holder"], IntentHash: req.IntentHash, Status: d["status"], Expiry: exp}, nil
}
