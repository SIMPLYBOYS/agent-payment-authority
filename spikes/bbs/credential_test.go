package bbs

import (
	"bytes"
	"crypto/ed25519"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"testing"
)

// Fixed synthetic inputs: test keys from fixed seeds, no real identity.
const now = 1791900000

var (
	issuerSeed = sha256.Sum256([]byte("c1-spike issuer test seed"))
	holderSeed = sha256.Sum256([]byte("c1-spike holder test seed"))
	otherSeed  = sha256.Sum256([]byte("c1-spike other test seed"))
	required   = []string{"schema", "issuer", "task_id", "expiry", "holder", "status"}
)

type fx struct {
	t      *testing.T
	is     *Issuer
	holder ed25519.PrivateKey
	cred   Credential
	sig    []byte
	v      *Verifier
}

func newFX(t *testing.T) *fx {
	t.Helper()
	is, err := NewIssuer("issuer-demo", issuerSeed[:])
	if err != nil {
		t.Fatal(err)
	}
	holder := ed25519.NewKeyFromSeed(holderSeed[:])
	cred := Credential{"schema": Schema, "issuer": is.ID, "task_id": "show-a-01", "consumer_id": "consumer-a",
		"c0": "0x" + hex.EncodeToString(make([]byte, 32)), "expiry": "1791903600",
		"holder": "ed25519:" + hex.EncodeToString(holder.Public().(ed25519.PublicKey)), "status": "7"}
	sig, err := is.Issue(cred)
	if err != nil {
		t.Fatal(err)
	}
	return &fx{t: t, is: is, holder: holder, cred: cred, sig: sig,
		v: &Verifier{ID: "policy-a", IssuerID: is.ID, IssuerPK: is.PK, Required: required, Revoked: map[string]bool{}}}
}

func (f *fx) req(challenge string) Request {
	return Request{VerifierID: "policy-a", Challenge: challenge, IntentHash: "sha256:intent-1", Disclose: required}
}

func (f *fx) present(r Request) Presentation {
	f.t.Helper()
	p, err := Present(f.cred, f.sig, f.is.PK, f.holder, r)
	if err != nil {
		f.t.Fatal(err)
	}
	return p
}

func code(err error) string {
	var r Refusal
	if errors.As(err, &r) {
		return string(r)
	}
	if err == nil {
		return "<nil>"
	}
	return "error: " + err.Error()
}

func TestIssueDeriveVerify(t *testing.T) {
	f := newFX(t)
	r := f.req("chal-1")
	p := f.present(r)
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
	// The signature itself verifies over all messages too.
	m, _ := f.cred.messages()
	if err := bbsplusNew().Verify(m, f.sig, f.is.PK); err != nil {
		t.Fatalf("signature: %v", err)
	}
}

// Each negative changes one thing and expects one fixed code.
func TestRefusals(t *testing.T) {
	for name, c := range map[string]struct {
		mut  func(f *fx, r *Request, p *Presentation)
		want string
	}{
		"wrong issuer key": {func(f *fx, r *Request, p *Presentation) {
			other, _ := NewIssuer("issuer-demo", otherSeed[:])
			f.v.IssuerPK = other.PK
		}, "PROOF_INVALID"},
		"signed by another issuer": {func(f *fx, r *Request, p *Presentation) {
			other, _ := NewIssuer("issuer-demo", otherSeed[:])
			sig, _ := other.Issue(f.cred)
			*p, _ = Present(f.cred, sig, other.PK, f.holder, *r)
		}, "PROOF_INVALID"},
		"tampered disclosed task_id": {func(f *fx, r *Request, p *Presentation) { p.Disclosed["task_id"] = "show-a-02" }, "PROOF_INVALID"},
		"tampered disclosed expiry":  {func(f *fx, r *Request, p *Presentation) { p.Disclosed["expiry"] = "1999999999" }, "PROOF_INVALID"},
		"tampered proof byte":        {func(f *fx, r *Request, p *Presentation) { p.Proof[len(p.Proof)-1] ^= 1 }, "PROOF_INVALID"},
		"other verifier's request": {func(f *fx, r *Request, p *Presentation) {
			r.VerifierID = "policy-b" // the holder made it for policy-b
			*p, _ = Present(f.cred, f.sig, f.is.PK, f.holder, *r)
			r.VerifierID = "policy-a" // presented to policy-a
		}, "PROOF_INVALID"},
		"other intent":                 {func(f *fx, r *Request, p *Presentation) { r.IntentHash = "sha256:intent-2" }, "PROOF_INVALID"},
		"other challenge":              {func(f *fx, r *Request, p *Presentation) { r.Challenge = "chal-other" }, "PROOF_INVALID"},
		"request for another verifier": {func(f *fx, r *Request, p *Presentation) { r.VerifierID = "policy-b" }, "WRONG_VERIFIER"},
		"required field hidden": {func(f *fx, r *Request, p *Presentation) {
			r.Disclose = []string{"schema", "issuer", "task_id", "expiry", "holder"}
			*p, _ = Present(f.cred, f.sig, f.is.PK, f.holder, *r)
		}, "FIELD_MISSING"},
		"field dropped from the presentation": {func(f *fx, r *Request, p *Presentation) { delete(p.Disclosed, "status") }, "DISCLOSURE_MISMATCH"},
		"extra field disclosed":               {func(f *fx, r *Request, p *Presentation) { p.Disclosed["consumer_id"] = "consumer-a" }, "DISCLOSURE_MISMATCH"},
		"holder signature by another key": {func(f *fx, r *Request, p *Presentation) {
			p.HolderSig = ed25519.Sign(ed25519.NewKeyFromSeed(otherSeed[:]), holderPayload(r.Nonce(), p.Proof))
		}, "HOLDER_BINDING_INVALID"},
		"holder signature over another proof": {func(f *fx, r *Request, p *Presentation) {
			q := f.present(*r)
			p.HolderSig = q.HolderSig
		}, "HOLDER_BINDING_INVALID"},
		"expired": {func(f *fx, r *Request, p *Presentation) {
			f.cred["expiry"] = "1791900000" // = now
			f.sig, _ = f.is.Issue(f.cred)
			*p = f.present(*r)
		}, "CREDENTIAL_EXPIRED"},
		"revoked":             {func(f *fx, r *Request, p *Presentation) { f.v.Revoked["7"] = true }, "CREDENTIAL_REVOKED"},
		"untrusted issuer id": {func(f *fx, r *Request, p *Presentation) { f.v.IssuerID = "issuer-other" }, "ISSUER_UNTRUSTED"},
		"unknown schema": {func(f *fx, r *Request, p *Presentation) {
			is, _ := NewIssuer("issuer-demo", issuerSeed[:])
			c := Credential{}
			for k, v := range f.cred {
				c[k] = v
			}
			c["schema"] = "apa.credential.other"
			m, _ := c.messages()
			sig, _ := bbsplusNew().Sign(m, is.sk) // signed by the trusted issuer, other schema
			*p, _ = Present(c, sig, is.PK, f.holder, *r)
		}, "SCHEMA_UNKNOWN"},
	} {
		t.Run(name, func(t *testing.T) {
			f := newFX(t)
			r := f.req("chal-1")
			p := f.present(r)
			c.mut(f, &r, &p)
			if _, err := f.v.Verify(r, p, now); code(err) != c.want {
				t.Fatalf("%s: %s, want %s", name, code(err), c.want)
			}
		})
	}
}

func TestReplay(t *testing.T) {
	f := newFX(t)
	r := f.req("chal-1")
	p := f.present(r)
	if _, err := f.v.Verify(r, p, now); err != nil {
		t.Fatal(err)
	}
	if _, err := f.v.Verify(r, p, now); code(err) != "CHALLENGE_REPLAYED" {
		t.Fatalf("replay: %s", code(err))
	}
	// A fresh challenge is a fresh proof.
	r2 := f.req("chal-2")
	if _, err := f.v.Verify(r2, f.present(r2), now); err != nil {
		t.Fatalf("fresh challenge: %v", err)
	}
}

func TestOutOfRangeAndNoDisclosure(t *testing.T) {
	f := newFX(t)
	if _, err := Present(f.cred, f.sig, f.is.PK, f.holder, Request{VerifierID: "policy-a", Challenge: "c", IntentHash: "i", Disclose: []string{"nonexistent"}}); err == nil {
		t.Fatal("an unknown field was disclosed")
	}
	if _, err := Present(f.cred, f.sig, f.is.PK, f.holder, Request{VerifierID: "policy-a", Challenge: "c", IntentHash: "i"}); err == nil {
		t.Fatal("a proof with nothing disclosed was made")
	}
	delete(f.cred, "status")
	if _, err := f.is.Issue(f.cred); err == nil {
		t.Fatal("a credential missing a field was issued")
	}
}

// Two proofs of one credential are not equal (unlinkability of the proof
// bytes; the disclosed fields themselves still link).
func TestProofsDiffer(t *testing.T) {
	f := newFX(t)
	r := f.req("chal-1")
	a, b := f.present(r), f.present(r)
	if string(a.Proof) == string(b.Proof) {
		t.Fatal("two derived proofs are identical")
	}
}

func BenchmarkIssue(b *testing.B) {
	f := newFX(&testing.T{})
	for b.Loop() {
		f.is.Issue(f.cred)
	}
}

func BenchmarkDerive(b *testing.B) {
	f := newFX(&testing.T{})
	r := f.req("c")
	for b.Loop() {
		Present(f.cred, f.sig, f.is.PK, f.holder, r)
	}
}

func BenchmarkVerify(b *testing.B) {
	f := newFX(&testing.T{})
	r := f.req("c")
	p := f.present(r)
	for b.Loop() {
		f.v.used = nil
		f.v.Verify(r, p, now)
	}
}

// What the library does to its input: the spike found the holder binding
// failing until the proof was hashed before VerifyProof. Recorded, not
// asserted either way, so a library change shows up in the log.
func TestLibraryInput(t *testing.T) {
	f := newFX(t)
	r := f.req("chal-1")
	p := f.present(r)
	m, _ := f.cred.messages()
	var msgs [][]byte
	for _, i := range []int{0, 1, 2, 5, 6, 7} {
		msgs = append(msgs, m[i])
	}
	before := bytes.Clone(p.Proof)
	err := bbsplusNew().VerifyProof(msgs, p.Proof, r.Nonce(), f.is.PK)
	t.Logf("VerifyProof: err %v; input proof rewritten: %v", err, !bytes.Equal(before, p.Proof))
}
