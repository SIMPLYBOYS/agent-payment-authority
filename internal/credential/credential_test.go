package credential

import (
	"crypto/ed25519"
	"crypto/sha256"
	"encoding/binary"
	"maps"
	"testing"

	"github.com/IBM/idemix/bbs"
)

// Fixed synthetic inputs: test keys from fixed seeds, no real identity.
const now = 1791900000

var (
	issuerSeed = sha256.Sum256([]byte("c2 credential issuer test seed"))
	holderSeed = sha256.Sum256([]byte("c2 credential holder test seed"))
	otherSeed  = sha256.Sum256([]byte("c2 credential other test seed"))
	thiefSeed  = sha256.Sum256([]byte("c2 credential thief test seed"))
)

type fx struct {
	is     *IssuerKey
	holder ed25519.PrivateKey
	cred   Credential
	sig    []byte
	req    Request
	exp    Expect
	st     Status
}

func newFX(t testing.TB) *fx {
	t.Helper()
	is, err := NewIssuerKey("issuer-demo", issuerSeed[:])
	if err != nil {
		t.Fatal(err)
	}
	holder := ed25519.NewKeyFromSeed(holderSeed[:])
	cred := Credential{"schema": Schema, "issuer": is.ID, "task_id": "show-a-01", "consumer_id": "consumer-a", "c0": C0Placeholder,
		"expiry": "1791903600", "holder": HolderField(holder.Public().(ed25519.PublicKey)), "status": "7"}
	sig, err := is.Issue(cred)
	if err != nil {
		t.Fatal(err)
	}
	return &fx{is: is, holder: holder, cred: cred, sig: sig,
		req: Request{VerifierID: "host-1", ChallengeID: "chl-1", IntentHash: "sha256:intent-1", Disclose: Mandatory},
		exp: Expect{IssuerID: is.ID, IssuerPK: is.PK, Holder: cred["holder"], TaskID: "show-a-01", Expiry: "1791903600", Status: "7"},
		st:  Status{List: &StatusList{Version: statusVersion, Issuer: "issuer-demo", ListVersion: 3, IssuedAt: now - 10, NextUpdate: now + 500, Revoked: []string{"2"}}, SHA256: "abc"}}
}

func (f *fx) present(t testing.TB) Presentation {
	t.Helper()
	p, err := Present(f.cred, f.sig, f.is.PK, f.holder, f.req)
	if err != nil {
		t.Fatal(err)
	}
	return p
}

func TestIssuePresentVerify(t *testing.T) {
	f := newFX(t)
	p := f.present(t)
	if _, ok := p.Disclosed["consumer_id"]; ok {
		t.Fatal("consumer_id disclosed")
	}
	if _, ok := p.Disclosed["c0"]; ok {
		t.Fatal("c0 disclosed")
	}
	o := Verify(f.req, p, f.exp, f.st, now)
	want := Outcome{Result: Valid, Issuer: "issuer-demo", Holder: f.cred["holder"], Status: "7", Expiry: 1791903600, ProofSHA256: Fingerprint(p.Proof),
		StatusListVersion: 3, StatusListIssuedAt: now - 10, StatusListSHA256: "abc"}
	if o != want {
		t.Fatalf("outcome %+v, want %+v", o, want)
	}
}

// Each negative changes one thing and expects one result and detail.
func TestRefusals(t *testing.T) {
	thief := ed25519.NewKeyFromSeed(thiefSeed[:])
	for name, c := range map[string]struct {
		mut            func(t *testing.T, f *fx, p *Presentation)
		result, detail string
	}{
		"wrong issuer key": {func(t *testing.T, f *fx, p *Presentation) {
			o, _ := NewIssuerKey("issuer-demo", otherSeed[:])
			f.exp.IssuerPK = o.PK
		}, Invalid, "PROOF_INVALID"},
		"tampered task_id":   {func(t *testing.T, f *fx, p *Presentation) { p.Disclosed["task_id"] = "show-a-02" }, Invalid, "PROOF_INVALID"},
		"tampered proof":     {func(t *testing.T, f *fx, p *Presentation) { p.Proof[len(p.Proof)-1] ^= 1 }, Invalid, "PROOF_INVALID"},
		"short proof":        {func(t *testing.T, f *fx, p *Presentation) { p.Proof = p.Proof[:5] }, Invalid, "PROOF_INVALID"},
		"65535 messages":     {func(t *testing.T, f *fx, p *Presentation) { binary.BigEndian.PutUint16(p.Proof, 0xFFFF) }, Invalid, "PROOF_INVALID"},
		"other intent":       {func(t *testing.T, f *fx, p *Presentation) { f.req.IntentHash = "sha256:intent-2" }, Invalid, "PROOF_INVALID"},
		"other verifier":     {func(t *testing.T, f *fx, p *Presentation) { f.req.VerifierID = "host-2" }, Invalid, "PROOF_INVALID"},
		"other challenge ID": {func(t *testing.T, f *fx, p *Presentation) { p.ChallengeID = "chl-2" }, Invalid, "PRESENTATION_FORMAT"},
		"wrong version":      {func(t *testing.T, f *fx, p *Presentation) { p.Version = "apa.presentation.v0" }, Invalid, "PRESENTATION_FORMAT"},
		"subset proof with forged holder and status": {func(t *testing.T, f *fx, p *Presentation) {
			proof, err := bbs.New(curve).DeriveProof(f.cred.messages(), f.sig, f.req.Nonce(), f.is.PK, []int{0, 1, 2, 5})
			if err != nil {
				t.Fatal(err)
			}
			th := HolderField(thief.Public().(ed25519.PublicKey))
			f.exp.Holder = th
			*p = Presentation{Version: PresentationVersion, ChallengeID: "chl-1", Disclosed: map[string]string{"schema": Schema, "issuer": "issuer-demo",
				"task_id": "show-a-01", "expiry": "1791903600", "holder": th, "status": "999"}, Proof: proof, HolderSig: ed25519.Sign(thief, holderPayload(f.req.Nonce(), proof))}
		}, Invalid, "PROOF_INVALID"},
		"extra field":             {func(t *testing.T, f *fx, p *Presentation) { p.Disclosed["consumer_id"] = "consumer-a" }, Invalid, "DISCLOSURE_MISMATCH"},
		"dropped field":           {func(t *testing.T, f *fx, p *Presentation) { delete(p.Disclosed, "status") }, Invalid, "DISCLOSURE_MISMATCH"},
		"unknown field requested": {func(t *testing.T, f *fx, p *Presentation) { f.req.Disclose = append(f.req.Disclose, "nonexistent") }, Invalid, "DISCLOSURE_MISMATCH"},
		"mandatory field hidden": {func(t *testing.T, f *fx, p *Presentation) {
			f.req.Disclose = []string{"schema", "issuer", "task_id", "expiry", "holder"}
			*p = f.present(t)
		}, Invalid, "FIELD_MISSING"},
		"untrusted issuer": {func(t *testing.T, f *fx, p *Presentation) { f.exp.IssuerID = "issuer-other" }, Invalid, "ISSUER_UNTRUSTED"},
		"not the registered holder": {func(t *testing.T, f *fx, p *Presentation) {
			f.exp.Holder = HolderField(thief.Public().(ed25519.PublicKey))
		}, Invalid, "HOLDER_MISMATCH"},
		"holder signature by another key": {func(t *testing.T, f *fx, p *Presentation) {
			p.HolderSig = ed25519.Sign(thief, holderPayload(f.req.Nonce(), p.Proof))
		}, Invalid, "HOLDER_BINDING_INVALID"},
		"another task's credential":      {func(t *testing.T, f *fx, p *Presentation) { f.exp.TaskID = "show-b-01" }, Invalid, "SNAPSHOT_MISMATCH"},
		"another credential of the task": {func(t *testing.T, f *fx, p *Presentation) { f.exp.Status = "8" }, Invalid, "SNAPSHOT_MISMATCH"},
		"no status list":                 {func(t *testing.T, f *fx, p *Presentation) { f.st = Status{Err: errMissing} }, StatusUnavailable, "STATUS_LIST_UNAVAILABLE"},
		"revoked":                        {func(t *testing.T, f *fx, p *Presentation) { f.st.List.Revoked = append(f.st.List.Revoked, "7") }, Revoked, ""},
		"expired": {func(t *testing.T, f *fx, p *Presentation) {
			f.cred["expiry"], f.exp.Expiry = "1791900000", "1791900000"
			f.sig, _ = f.is.Issue(f.cred)
			*p = f.present(t)
		}, Expired, ""},
		// Precedence: revoked before expired, status before both, invalid first.
		"revoked and expired": {func(t *testing.T, f *fx, p *Presentation) {
			f.cred["expiry"], f.exp.Expiry = "1791900000", "1791900000"
			f.sig, _ = f.is.Issue(f.cred)
			*p = f.present(t)
			f.st.List.Revoked = []string{"7"}
		}, Revoked, ""},
		"no status list and revoked": {func(t *testing.T, f *fx, p *Presentation) {
			f.st = Status{Err: errMissing}
		}, StatusUnavailable, "STATUS_LIST_UNAVAILABLE"},
	} {
		t.Run(name, func(t *testing.T) {
			f := newFX(t)
			p := f.present(t)
			c.mut(t, f, &p)
			if o := Verify(f.req, p, f.exp, f.st, now); o.Result != c.result || o.Detail != c.detail {
				t.Fatalf("%s: %s/%s, want %s/%s", name, o.Result, o.Detail, c.result, c.detail)
			}
		})
	}
}

var errMissing = errFor("status list: missing")

type errFor string

func (e errFor) Error() string { return string(e) }

func TestIssueRefusesMalformed(t *testing.T) {
	f := newFX(t)
	for field, value := range map[string]string{"expiry": "+1791903600", "holder": "ED25519:00", "status": "07", "c0": "0x00", "schema": "apa.credential.v0", "task_id": "bad id"} {
		c := maps.Clone(f.cred)
		c[field] = value
		if _, err := f.is.Issue(c); err == nil {
			t.Errorf("issued %s=%q", field, value)
		}
	}
	other, _ := NewIssuerKey("issuer-other", otherSeed[:])
	if _, err := other.Issue(f.cred); err == nil {
		t.Fatal("issued another issuer's credential")
	}
}

func TestNonceCoversTheDisclosureList(t *testing.T) {
	f := newFX(t)
	a := f.req
	b := f.req
	b.Disclose = []string{"schema", "issuer", "task_id", "expiry", "holder", "status", "consumer_id"}
	if string(a.Nonce()) == string(b.Nonce()) {
		t.Fatal("two disclosure lists share a nonce")
	}
	c := Request{VerifierID: "v", ChallengeID: "x", IntentHash: "a\x00b"}
	d := Request{VerifierID: "v", ChallengeID: "x\x00a", IntentHash: "b"}
	if string(c.Nonce()) == string(d.Nonce()) {
		t.Fatal("NUL makes two requests share a nonce")
	}
}
