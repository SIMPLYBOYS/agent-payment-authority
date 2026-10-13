package bbs

import (
	"bytes"
	"crypto/ed25519"
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"errors"
	"maps"
	"testing"
	"time"

	"github.com/IBM/idemix/bbs"
)

// Fixed synthetic inputs: test keys from fixed seeds, no real identity.
const now = 1791900000

var (
	issuerSeed = sha256.Sum256([]byte("c1-spike issuer test seed"))
	holderSeed = sha256.Sum256([]byte("c1-spike holder test seed"))
	otherSeed  = sha256.Sum256([]byte("c1-spike other test seed"))
	thiefSeed  = sha256.Sum256([]byte("c1-spike thief test seed"))
)

type fx struct {
	t      *testing.T
	is     *Issuer
	holder ed25519.PrivateKey
	cred   Credential
	sig    []byte
	v      *Verifier
}

func holderField(k ed25519.PrivateKey) string {
	return "ed25519:" + hex.EncodeToString(k.Public().(ed25519.PublicKey))
}

func newFX(t testing.TB) *fx {
	t.Helper()
	is, err := NewIssuer("issuer-demo", issuerSeed[:])
	if err != nil {
		t.Fatal(err)
	}
	holder := ed25519.NewKeyFromSeed(holderSeed[:])
	cred := Credential{"schema": Schema, "issuer": is.ID, "task_id": "show-a-01", "consumer_id": "consumer-a",
		"c0": "0x" + hex.EncodeToString(make([]byte, 32)), "expiry": "1791903600", "holder": holderField(holder), "status": "7"}
	sig, err := is.Issue(cred)
	if err != nil {
		t.Fatal(err)
	}
	return &fx{is: is, holder: holder, cred: cred, sig: sig, v: &Verifier{ID: "policy-a", IssuerID: is.ID, IssuerPK: is.PK, Revoked: map[string]bool{}}}
}

func (f *fx) req() Request {
	return Request{VerifierID: "policy-a", Challenge: f.v.NewChallenge(), IntentHash: "sha256:intent-1", Disclose: Mandatory}
}

func (f *fx) present(t testing.TB, r Request) Presentation {
	t.Helper()
	p, err := Present(f.cred, f.sig, f.is.PK, f.holder, r)
	if err != nil {
		t.Fatal(err)
	}
	return p
}

// signRaw signs c as the trusted issuer without Issue's format checks: a
// verifier must refuse what a faulty issuer could sign.
func (f *fx) signRaw(t testing.TB, c Credential) []byte {
	m, err := c.messages()
	if err != nil {
		t.Fatal(err)
	}
	sig, err := bbs.New(curve).Sign(m, f.is.sk)
	if err != nil {
		t.Fatal(err)
	}
	return sig
}

func code(err error) string {
	if r, ok := errors.AsType[Refusal](err); ok {
		return string(r)
	}
	if err == nil {
		return "<nil>"
	}
	return "error: " + err.Error()
}

func TestIssueDeriveVerify(t *testing.T) {
	f := newFX(t)
	r := f.req()
	p := f.present(t, r)
	if _, ok := p.Disclosed["consumer_id"]; ok {
		t.Fatal("consumer_id disclosed")
	}
	if _, ok := p.Disclosed["c0"]; ok {
		t.Fatal("c0 disclosed")
	}
	res, err := f.v.Verify(r, p, now)
	if err != nil {
		t.Fatalf("verify: %v", err)
	}
	// The result names what was established, not a bare "verified".
	want := Result{Issuer: "issuer-demo", Schema: Schema, TaskID: "show-a-01", Holder: f.cred["holder"], IntentHash: "sha256:intent-1", Status: "7", Expiry: 1791903600}
	if res != want {
		t.Fatalf("result %+v, want %+v", res, want)
	}
	m, _ := f.cred.messages()
	if err := bbs.New(curve).Verify(m, f.sig, f.is.PK); err != nil {
		t.Fatalf("signature: %v", err)
	}
}

// Each negative changes one thing and expects one fixed code.
func TestRefusals(t *testing.T) {
	thief := ed25519.NewKeyFromSeed(thiefSeed[:])
	for name, c := range map[string]struct {
		mut  func(t *testing.T, f *fx, r *Request, p *Presentation)
		want string
	}{
		"wrong issuer key": {func(t *testing.T, f *fx, r *Request, p *Presentation) {
			other, _ := NewIssuer("issuer-demo", otherSeed[:])
			f.v.IssuerPK = other.PK
		}, "PROOF_INVALID"},
		"signed by another issuer": {func(t *testing.T, f *fx, r *Request, p *Presentation) {
			other, _ := NewIssuer("issuer-demo", otherSeed[:])
			sig, _ := other.Issue(f.cred)
			*p, _ = Present(f.cred, sig, other.PK, f.holder, *r)
		}, "PROOF_INVALID"},
		"tampered disclosed task_id": {func(t *testing.T, f *fx, r *Request, p *Presentation) { p.Disclosed["task_id"] = "show-a-02" }, "PROOF_INVALID"},
		"tampered disclosed expiry":  {func(t *testing.T, f *fx, r *Request, p *Presentation) { p.Disclosed["expiry"] = "1999999999" }, "PROOF_INVALID"},
		"tampered proof byte":        {func(t *testing.T, f *fx, r *Request, p *Presentation) { p.Proof[len(p.Proof)-1] ^= 1 }, "PROOF_INVALID"},
		// Review must-fix: the proof reveals fewer indexes than the request;
		// the rest of Disclosed is forged (holder, status, expiry, task).
		"proof reveals a subset; forged holder and status": {func(t *testing.T, f *fx, r *Request, p *Presentation) {
			m, _ := f.cred.messages()
			nonce := r.Nonce()
			proof, err := bbs.New(curve).DeriveProof(m, f.sig, nonce, f.is.PK, []int{0, 1, 2, 5})
			if err != nil {
				t.Fatal(err)
			}
			f.v.Revoked["7"] = true
			*p = Presentation{Disclosed: map[string]string{"schema": Schema, "issuer": "issuer-demo", "task_id": "show-a-01", "expiry": "1791903600",
				"holder": holderField(thief), "status": "999"}, Proof: proof, HolderSig: ed25519.Sign(thief, holderPayload(nonce, proof))}
		}, "PROOF_INVALID"},
		"proof reveals only the schema": {func(t *testing.T, f *fx, r *Request, p *Presentation) {
			m, _ := f.cred.messages()
			proof, _ := bbs.New(curve).DeriveProof(m, f.sig, r.Nonce(), f.is.PK, []int{0})
			p.Proof = proof
			p.Disclosed["task_id"], p.Disclosed["expiry"] = "ANY-TASK", "9999999999"
		}, "PROOF_INVALID"},
		"proof claims 65535 messages": {func(t *testing.T, f *fx, r *Request, p *Presentation) {
			binary.BigEndian.PutUint16(p.Proof, 0xFFFF)
		}, "PROOF_INVALID"},
		"proof length field out of range": {func(t *testing.T, f *fx, r *Request, p *Presentation) {
			binary.BigEndian.PutUint32(p.Proof[4+3*48:], 0xFFFFFFF0) // after the 4-byte header and three G1 points
		}, "PROOF_INVALID"},
		"short proof": {func(t *testing.T, f *fx, r *Request, p *Presentation) { p.Proof = p.Proof[:5] }, "PROOF_INVALID"},
		"other verifier's request": {func(t *testing.T, f *fx, r *Request, p *Presentation) {
			r.VerifierID = "policy-b" // the holder made it for policy-b
			*p, _ = Present(f.cred, f.sig, f.is.PK, f.holder, *r)
			r.VerifierID = "policy-a" // presented to policy-a
		}, "PROOF_INVALID"},
		"other intent": {func(t *testing.T, f *fx, r *Request, p *Presentation) { r.IntentHash = "sha256:intent-2" }, "PROOF_INVALID"},
		"proof made for another issued challenge": {func(t *testing.T, f *fx, r *Request, p *Presentation) { r.Challenge = f.v.NewChallenge() }, "PROOF_INVALID"},
		"challenge never issued":                  {func(t *testing.T, f *fx, r *Request, p *Presentation) { r.Challenge = "chal-made-up" }, "CHALLENGE_UNKNOWN"},
		"request for another verifier":            {func(t *testing.T, f *fx, r *Request, p *Presentation) { r.VerifierID = "policy-b" }, "WRONG_VERIFIER"},
		"mandatory field hidden": {func(t *testing.T, f *fx, r *Request, p *Presentation) {
			r.Disclose = []string{"schema", "issuer", "task_id", "expiry", "holder"}
			*p, _ = Present(f.cred, f.sig, f.is.PK, f.holder, *r)
		}, "FIELD_MISSING"},
		"unknown field requested":             {func(t *testing.T, f *fx, r *Request, p *Presentation) { r.Disclose = append(r.Disclose, "nonexistent") }, "DISCLOSURE_MISMATCH"},
		"repeated field requested":            {func(t *testing.T, f *fx, r *Request, p *Presentation) { r.Disclose = append(r.Disclose, "status") }, "DISCLOSURE_MISMATCH"},
		"field dropped from the presentation": {func(t *testing.T, f *fx, r *Request, p *Presentation) { delete(p.Disclosed, "status") }, "DISCLOSURE_MISMATCH"},
		"extra field disclosed":               {func(t *testing.T, f *fx, r *Request, p *Presentation) { p.Disclosed["consumer_id"] = "consumer-a" }, "DISCLOSURE_MISMATCH"},
		"holder signature by another key": {func(t *testing.T, f *fx, r *Request, p *Presentation) {
			p.HolderSig = ed25519.Sign(thief, holderPayload(r.Nonce(), p.Proof))
		}, "HOLDER_BINDING_INVALID"},
		"holder signature over another proof": {func(t *testing.T, f *fx, r *Request, p *Presentation) {
			p.HolderSig = f.present(t, *r).HolderSig
		}, "HOLDER_BINDING_INVALID"},
		"expired": {func(t *testing.T, f *fx, r *Request, p *Presentation) {
			f.cred["expiry"] = "1791900000" // = now
			f.sig, _ = f.is.Issue(f.cred)
			*p = f.present(t, *r)
		}, "CREDENTIAL_EXPIRED"},
		"revoked":             {func(t *testing.T, f *fx, r *Request, p *Presentation) { f.v.Revoked["7"] = true }, "CREDENTIAL_REVOKED"},
		"untrusted issuer id": {func(t *testing.T, f *fx, r *Request, p *Presentation) { f.v.IssuerID = "issuer-other" }, "ISSUER_UNTRUSTED"},
		"unknown schema": {func(t *testing.T, f *fx, r *Request, p *Presentation) {
			*p = f.rawPresent(t, *r, "schema", "apa.credential.other")
		}, "SCHEMA_UNKNOWN"},
		"expiry with a plus": {func(t *testing.T, f *fx, r *Request, p *Presentation) {
			*p = f.rawPresent(t, *r, "expiry", "+1791903600")
		}, "EXPIRY_INVALID"},
		"expiry with a leading zero": {func(t *testing.T, f *fx, r *Request, p *Presentation) {
			*p = f.rawPresent(t, *r, "expiry", "01791903600")
		}, "EXPIRY_INVALID"},
		"holder without its prefix": {func(t *testing.T, f *fx, r *Request, p *Presentation) {
			*p = f.rawPresent(t, *r, "holder", f.cred["holder"][len("ed25519:"):])
		}, "HOLDER_BINDING_INVALID"},
		"status with a leading zero": {func(t *testing.T, f *fx, r *Request, p *Presentation) {
			f.v.Revoked["7"] = true
			*p = f.rawPresent(t, *r, "status", "07")
		}, "STATUS_INVALID"},
	} {
		t.Run(name, func(t *testing.T) {
			f := newFX(t)
			r := f.req()
			p := f.present(t, r)
			c.mut(t, f, &r, &p)
			start := time.Now()
			if _, err := f.v.Verify(r, p, now); code(err) != c.want {
				t.Fatalf("%s: %s, want %s", name, code(err), c.want)
			}
			if d := time.Since(start); d > 2*time.Second {
				t.Fatalf("%s: took %v", name, d)
			}
		})
	}
}

// rawPresent presents a credential with one field changed, signed by the
// trusted issuer without Issue's checks.
func (f *fx) rawPresent(t *testing.T, r Request, field, value string) Presentation {
	c := maps.Clone(f.cred)
	c[field] = value
	p, err := Present(c, f.signRaw(t, c), f.is.PK, f.holder, r)
	if err != nil {
		t.Fatal(err)
	}
	return p
}

// Issue refuses what the verifier would refuse.
func TestIssueFormats(t *testing.T) {
	f := newFX(t)
	for field, value := range map[string]string{"expiry": "+1791903600", "holder": "ED25519:00", "status": "07"} {
		c := maps.Clone(f.cred)
		c[field] = value
		if _, err := f.is.Issue(c); err == nil {
			t.Fatalf("issued %s=%q", field, value)
		}
	}
}

func TestReplay(t *testing.T) {
	f := newFX(t)
	r := f.req()
	p := f.present(t, r)
	if _, err := f.v.Verify(r, p, now); err != nil {
		t.Fatal(err)
	}
	if _, err := f.v.Verify(r, p, now); code(err) != "CHALLENGE_UNKNOWN" {
		t.Fatalf("replay: %s", code(err))
	}
	// A failed presentation does not use the challenge up.
	r2 := f.req()
	bad := f.present(t, r2)
	bad.HolderSig = bad.HolderSig[:10]
	if _, err := f.v.Verify(r2, bad, now); code(err) != "HOLDER_BINDING_INVALID" {
		t.Fatalf("bad: %s", code(err))
	}
	if _, err := f.v.Verify(r2, f.present(t, r2), now); err != nil {
		t.Fatalf("retry after a refusal: %v", err)
	}
}

func TestPresentRefusesBadRequests(t *testing.T) {
	f := newFX(t)
	for _, d := range [][]string{{"nonexistent"}, nil, {"status", "status"}} {
		if _, err := Present(f.cred, f.sig, f.is.PK, f.holder, Request{VerifierID: "policy-a", Challenge: "c", IntentHash: "i", Disclose: d}); err == nil {
			t.Fatalf("presented %v", d)
		}
	}
	delete(f.cred, "status")
	if _, err := f.is.Issue(f.cred); err == nil {
		t.Fatal("a credential missing a field was issued")
	}
}

// Review note: a NUL inside a value cannot make two requests share a nonce.
func TestNonceUnambiguous(t *testing.T) {
	a := Request{VerifierID: "policy-a", Challenge: "x", IntentHash: "a\x00b"}
	b := Request{VerifierID: "policy-a", Challenge: "x\x00a", IntentHash: "b"}
	if bytes.Equal(a.Nonce(), b.Nonce()) {
		t.Fatal("two requests share a nonce")
	}
}

// Two proofs of one credential are not equal (the proof bytes do not link;
// the disclosed fields, the holder key among them, still do).
func TestProofsDiffer(t *testing.T) {
	f := newFX(t)
	r := f.req()
	if bytes.Equal(f.present(t, r).Proof, f.present(t, r).Proof) {
		t.Fatal("two derived proofs are identical")
	}
}

// trustbloc v1.0.4 rewrote the input proof in VerifyProof; idemix v0.3.1
// copies first (bbs/utils.go). Asserted, so a regression shows.
func TestLibraryInput(t *testing.T) {
	f := newFX(t)
	r := f.req()
	p := f.present(t, r)
	m, _ := f.cred.messages()
	var msgs [][]byte
	for _, i := range []int{0, 1, 2, 5, 6, 7} {
		msgs = append(msgs, m[i])
	}
	before := bytes.Clone(p.Proof)
	if err := bbs.New(curve).VerifyProof(msgs, p.Proof, r.Nonce(), f.is.PK); err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(before, p.Proof) {
		t.Fatal("VerifyProof rewrote its input proof")
	}
}

func BenchmarkIssue(b *testing.B) {
	f := newFX(b)
	for b.Loop() {
		f.is.Issue(f.cred)
	}
}

func BenchmarkDerive(b *testing.B) {
	f := newFX(b)
	r := f.req()
	for b.Loop() {
		Present(f.cred, f.sig, f.is.PK, f.holder, r)
	}
}

func BenchmarkVerify(b *testing.B) {
	f := newFX(b)
	r := f.req()
	p := f.present(b, r)
	for b.Loop() {
		f.v.outstanding[r.Challenge] = true
		f.v.Verify(r, p, now)
	}
}
