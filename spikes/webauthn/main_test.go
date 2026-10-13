package main

// The spike's server rules against a software authenticator (P-256, fmt
// none). It shows the verification side only: what a real browser and
// device do is what the manual A1 run records, and a virtual or software
// authenticator never counts as a real-device result.

import (
	"bytes"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/binary"
	"encoding/json"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/fxamacker/cbor/v2"
)

const origin = "http://localhost:8123"

const (
	fUP, fUV, fBE, fBS, fAT = 0x01, 0x04, 0x08, 0x10, 0x40
)

var b64 = base64.RawURLEncoding

type authn struct {
	key   *ecdsa.PrivateKey
	id    []byte
	flags byte // UP|UV unless a case changes it
	count uint32
}

func newAuthn(t *testing.T) *authn {
	k, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	id := make([]byte, 16)
	rand.Read(id)
	return &authn{key: k, id: id, flags: fUP | fUV}
}

func (a *authn) authData(extra []byte) []byte {
	rp := sha256.Sum256([]byte(rpID))
	ad := append(rp[:], a.flags)
	ad = binary.BigEndian.AppendUint32(ad, a.count)
	return append(ad, extra...)
}

func clientData(typ, challenge, org string, more map[string]any) []byte {
	m := map[string]any{"type": typ, "challenge": challenge, "origin": org}
	for k, v := range more {
		m[k] = v
	}
	b, _ := json.Marshal(m)
	return b
}

// create makes a registration; packed gives a valid self attestation
// instead of fmt none.
func (a *authn) create(t *testing.T, challenge string, packed ...bool) []byte {
	pub, _ := a.key.PublicKey.ECDH()
	raw := pub.Bytes() // 0x04 || X || Y
	cose, err := cbor.Marshal(map[int]any{1: 2, 3: -7, -1: 1, -2: raw[1:33], -3: raw[33:]})
	if err != nil {
		t.Fatal(err)
	}
	att := make([]byte, 16) // AAGUID zero, as attestation=none gives
	att = binary.BigEndian.AppendUint16(att, uint16(len(a.id)))
	att = append(append(att, a.id...), cose...)
	f := a.flags
	a.flags |= fAT
	ad := a.authData(att)
	a.flags = f
	cd := clientData("webauthn.create", challenge, origin, nil)
	fmtName, stmt := "none", map[string]any{}
	if len(packed) > 0 && packed[0] {
		h := sha256.Sum256(cd)
		d := sha256.Sum256(append(append([]byte(nil), ad...), h[:]...))
		sig, _ := ecdsa.SignASN1(rand.Reader, a.key, d[:])
		fmtName, stmt = "packed", map[string]any{"alg": -7, "sig": sig}
	}
	obj, _ := cbor.Marshal(map[string]any{"fmt": fmtName, "attStmt": stmt, "authData": ad})
	b, _ := json.Marshal(map[string]any{"id": b64.EncodeToString(a.id), "rawId": b64.EncodeToString(a.id), "type": "public-key",
		"clientExtensionResults": map[string]any{},
		"response": map[string]any{"clientDataJSON": b64.EncodeToString(cd),
			"attestationObject": b64.EncodeToString(obj)}})
	return b
}

func (a *authn) get(t *testing.T, challenge, org string, more map[string]any) json.RawMessage {
	cd := clientData("webauthn.get", challenge, org, more)
	ad := a.authData(nil)
	h := sha256.Sum256(cd)
	d := sha256.Sum256(append(append([]byte(nil), ad...), h[:]...))
	sig, err := ecdsa.SignASN1(rand.Reader, a.key, d[:])
	if err != nil {
		t.Fatal(err)
	}
	b, _ := json.Marshal(map[string]any{"id": b64.EncodeToString(a.id), "rawId": b64.EncodeToString(a.id), "type": "public-key",
		"clientExtensionResults": map[string]any{},
		"response": map[string]any{"clientDataJSON": b64.EncodeToString(cd), "authenticatorData": b64.EncodeToString(ad),
			"signature": b64.EncodeToString(sig)}})
	return b
}

type fixture struct {
	t   *testing.T
	s   *spike
	out *bytes.Buffer
	now time.Time
}

func newFixture(t *testing.T) *fixture {
	f := &fixture{t: t, out: &bytes.Buffer{}, now: time.Unix(1790726450, 0)}
	s, err := newSpike("8123", "virtual", f.out, func() time.Time { return f.now })
	if err != nil {
		t.Fatal(err)
	}
	f.s = s
	return f
}

func (f *fixture) post(path string, body any) (int, map[string]any) {
	f.t.Helper()
	raw, ok := body.([]byte)
	if !ok {
		raw, _ = json.Marshal(body)
	}
	r := httptest.NewRequest("POST", path, bytes.NewReader(raw))
	r.Host = "localhost:8123"
	r.Header.Set("Origin", origin)
	r.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	f.s.ServeHTTP(w, r)
	var v map[string]any
	json.Unmarshal(w.Body.Bytes(), &v)
	return w.Code, v
}

func (f *fixture) register(a *authn) {
	f.t.Helper()
	_, o := f.post("/api/register/begin", nil)
	ch := o["publicKey"].(map[string]any)["challenge"].(string)
	if code, v := f.post("/api/register/finish", a.create(f.t, ch)); code != 200 {
		f.t.Fatalf("register: %d %v", code, v)
	}
}

// begin returns the challenge ID and the challenge to sign.
func (f *fixture) begin(session, uv string) (string, string) {
	f.t.Helper()
	code, v := f.post("/api/approve/begin", map[string]string{"session": session, "uv": uv})
	if code != 200 {
		f.t.Fatalf("begin: %d %v", code, v)
	}
	return v["challenge_id"].(string), v["options"].(map[string]any)["publicKey"].(map[string]any)["challenge"].(string)
}

func (f *fixture) finish(id string, cred json.RawMessage) (int, string) {
	f.t.Helper()
	code, v := f.post("/api/approve/finish", map[string]any{"challenge_id": id, "credential": cred, "step": "test"})
	if code == 200 {
		return code, "verified"
	}
	c, _ := v["code"].(string)
	return code, c
}

func (f *fixture) approve(a *authn) (int, string) {
	id, ch := f.begin("s1", "required")
	return f.finish(id, a.get(f.t, ch, origin, nil))
}

func TestApproveAndRefusals(t *testing.T) {
	f := newFixture(t)
	a := newAuthn(t)
	f.register(a)

	// The challenge is the hash of its record: it binds the session and the intent.
	_, v := f.post("/api/approve/begin", map[string]string{"session": "s1", "uv": "required"})
	var rec record
	rb, _ := json.Marshal(v["record"])
	json.Unmarshal(rb, &rec)
	if got := v["options"].(map[string]any)["publicKey"].(map[string]any)["challenge"]; got != b64.EncodeToString(rec.challenge()) ||
		rec.IntentHash != intentHash || rec.SessionID != "spike-session-s1" {
		t.Fatalf("challenge not derived from its record: %v %+v", got, rec)
	}

	id, ch := f.begin("s1", "required")
	ok := a.get(t, ch, origin, nil)
	if code, c := f.finish(id, ok); c != "verified" {
		t.Fatalf("approve: %d %s", code, c)
	}
	if _, c := f.finish(id, ok); c != "CHALLENGE_USED" {
		t.Fatalf("replay: %s", c)
	}

	// Signed for s1, sent to s2: refused, and neither challenge is consumed.
	id1, ch1 := f.begin("s1", "required")
	id2, _ := f.begin("s2", "required")
	s1 := a.get(t, ch1, origin, nil)
	if _, c := f.finish(id2, s1); c != "ASSERTION_INVALID" {
		t.Fatalf("wrong challenge: %s", c)
	}
	if _, c := f.finish(id1, s1); c != "verified" {
		t.Fatalf("the right challenge after a wrong one: %s", c)
	}

	for name, c := range map[string]struct {
		org  string
		more map[string]any
		uv   bool
	}{
		"wrong origin":       {"http://127.0.0.1:8123", nil, true},
		"other port":         {"http://localhost:8402", nil, true},
		"topOrigin":          {origin, map[string]any{"topOrigin": "http://evil.example"}, true},
		"crossOrigin":        {origin, map[string]any{"crossOrigin": true, "topOrigin": origin}, true},
		"no UV":              {origin, nil, false},
		"no UV, discouraged": {origin, nil, false},
	} {
		uv := "required"
		if strings.HasSuffix(name, "discouraged") {
			uv = "discouraged"
		}
		id, ch := f.begin("s1", uv)
		if !c.uv {
			a.flags = fUP
		}
		code, got := f.finish(id, a.get(t, ch, c.org, c.more))
		a.flags = fUP | fUV
		if got != "ASSERTION_INVALID" {
			t.Fatalf("%s: %d %s", name, code, got)
		}
	}

	// Expiry is the spike's own clock, at the boundary included.
	id, ch = f.begin("s1", "required")
	late := a.get(t, ch, origin, nil)
	f.now = f.now.Add(challengeTTL)
	if _, c := f.finish(id, late); c != "CHALLENGE_EXPIRED" {
		t.Fatalf("expired: %s", c)
	}
	if _, c := f.finish("ch-unknown", late); c != "CHALLENGE_UNKNOWN" {
		t.Fatalf("unknown: %s", c)
	}
}

func TestCounterAndBackupFlags(t *testing.T) {
	// 0/0 is accepted every time (synced passkeys).
	f := newFixture(t)
	a := newAuthn(t)
	f.register(a)
	for i := 0; i < 2; i++ {
		if _, c := f.approve(a); c != "verified" {
			t.Fatalf("0/0 #%d: %s", i, c)
		}
	}

	// A counter that goes back is refused and the credential is suspect.
	f = newFixture(t)
	a = newAuthn(t)
	a.count = 5
	f.register(a)
	a.count = 7
	if _, c := f.approve(a); c != "verified" {
		t.Fatalf("counter up: %s", c)
	}
	early, earlyCh := f.begin("s2", "required") // issued before the credential turns suspect
	a.count = 7
	if _, c := f.approve(a); c != "ASSERTION_COUNTER" {
		t.Fatalf("counter equal: %s", c)
	}
	a.count = 9
	if _, c := f.finish(early, a.get(t, earlyCh, origin, nil)); c != "CREDENTIAL_SUSPECT" {
		t.Fatalf("suspect credential on an earlier challenge: %s", c)
	}
	if _, v := f.post("/api/approve/begin", map[string]string{"session": "s1", "uv": "required"}); v["code"] != "NO_CREDENTIAL" {
		t.Fatalf("new challenge for a suspect credential: %v", v)
	}

	// BE is fixed at registration; BS may change while BE=1.
	f = newFixture(t)
	a = newAuthn(t)
	a.flags = fUP | fUV | fBE
	f.register(a)
	a.flags = fUP | fUV | fBE | fBS
	if _, c := f.approve(a); c != "verified" {
		t.Fatalf("BS change: %s", c)
	}
	a.flags = fUP | fUV
	if _, c := f.approve(a); c != "ASSERTION_INVALID_FLAGS" {
		t.Fatalf("BE change: %s", c)
	}
}

func TestRegistrationAndRequests(t *testing.T) {
	f := newFixture(t)
	if code, v := f.post("/api/approve/begin", map[string]string{"session": "s1", "uv": "required"}); v["code"] != "NO_CREDENTIAL" {
		t.Fatalf("approve before register: %d %v", code, v)
	}
	a := newAuthn(t)
	a.flags = fUP
	_, o := f.post("/api/register/begin", nil)
	ch := o["publicKey"].(map[string]any)["challenge"].(string)
	if _, v := f.post("/api/register/finish", a.create(t, ch)); v["code"] != "REGISTRATION_INVALID" {
		t.Fatalf("register without UV: %v", v)
	}
	// D6: attestation=none only, even for a valid packed self attestation.
	a.flags = fUP | fUV
	_, o = f.post("/api/register/begin", nil)
	ch = o["publicKey"].(map[string]any)["challenge"].(string)
	if _, v := f.post("/api/register/finish", a.create(t, ch, true)); v["code"] != "REGISTRATION_ATTESTATION" {
		t.Fatalf("packed attestation: %v", v)
	}
	// One finish per begin.
	if _, v := f.post("/api/register/finish", a.create(t, ch)); v["code"] != "NO_REGISTRATION" {
		t.Fatalf("second finish: %v", v)
	}

	// Host and Origin are an exact allow-list; anything else is not served.
	for name, edit := range map[string]func(r *httptestReq){
		"host":         func(r *httptestReq) { r.host = "evil.example:8123" },
		"origin":       func(r *httptestReq) { r.origin = "http://evil.example" },
		"no origin":    func(r *httptestReq) { r.origin = "" },
		"https origin": func(r *httptestReq) { r.origin = "https://localhost:8123" },
		"content type": func(r *httptestReq) { r.ctype = "text/plain" },
	} {
		q := &httptestReq{host: "localhost:8123", origin: origin, ctype: "application/json"}
		edit(q)
		r := httptest.NewRequest("POST", "/api/register/begin", strings.NewReader("{}"))
		r.Host = q.host
		if q.origin != "" {
			r.Header.Set("Origin", q.origin)
		}
		r.Header.Set("Content-Type", q.ctype)
		w := httptest.NewRecorder()
		f.s.ServeHTTP(w, r)
		if w.Code == 200 {
			t.Fatalf("%s: served", name)
		}
	}
}

type httptestReq struct{ host, origin, ctype string }

func TestResultsFile(t *testing.T) {
	f := newFixture(t)
	a := newAuthn(t)
	f.register(a)
	f.approve(a)
	f.post("/api/report", map[string]string{"step": "register", "name": "SecurityError", "message": "rp id"})
	lines := strings.Split(strings.TrimSpace(f.out.String()), "\n")
	if len(lines) != 3 {
		t.Fatalf("%d result lines: %q", len(lines), f.out.String())
	}
	for _, l := range lines {
		var r result
		if err := json.Unmarshal([]byte(l), &r); err != nil || r.Authenticator != "virtual" || r.Step == "" {
			t.Fatalf("result line %q: %v", l, err)
		}
		if strings.Contains(l, b64.EncodeToString(a.id)) {
			t.Fatalf("raw credential ID kept: %s", l)
		}
	}
	var last result
	json.Unmarshal([]byte(lines[2]), &last)
	if last.Step != "browser:register" || last.OK || last.Code != "BROWSER_REPORTED" {
		t.Fatalf("browser report recorded as %+v", last)
	}
}
