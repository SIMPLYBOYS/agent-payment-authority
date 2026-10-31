package credential

import (
	"crypto/ed25519"
	"errors"
	"os"
	"path/filepath"
	"slices"
	"testing"
)

func newDir(t *testing.T) (*Dir, string) {
	t.Helper()
	path := filepath.Join(t.TempDir(), "cred")
	if err := InitDir(path, "issuer-demo", []string{"consumer-a", "consumer-b"}, now); err != nil {
		t.Fatal(err)
	}
	d, err := OpenDir(path)
	if err != nil {
		t.Fatal(err)
	}
	return d, path
}

func TestInitAndRoundTrip(t *testing.T) {
	d, _ := newDir(t)
	k, err := d.IssuerKey()
	if err != nil {
		t.Fatal(err)
	}
	id, pk, err := d.IssuerPublic()
	if err != nil || id != "issuer-demo" || string(pk) != string(k.PK) {
		t.Fatalf("issuer public: %s %v", id, err)
	}
	holders, err := d.Holders()
	if err != nil || len(holders) != 2 || holders["consumer-a"] == holders["consumer-b"] {
		t.Fatalf("holders %v %v", holders, err)
	}
	hk, err := d.HolderKey("consumer-a")
	if err != nil || HolderField(hk.Public().(ed25519.PublicKey)) != holders["consumer-a"] {
		t.Fatalf("holder key: %v", err)
	}
	idx, err := d.ReserveStatusIndex()
	if err != nil || idx != "0" {
		t.Fatalf("first index %q %v", idx, err)
	}
	if idx, _ := d.ReserveStatusIndex(); idx != "1" {
		t.Fatalf("second index %q", idx)
	}
	c := Credential{"schema": Schema, "issuer": "issuer-demo", "task_id": "show-a-01", "consumer_id": "consumer-a", "c0": C0Placeholder,
		"expiry": "1791903600", "holder": holders["consumer-a"], "status": "0"}
	sig, err := k.Issue(c)
	if err != nil {
		t.Fatal(err)
	}
	if err := d.WriteCredential("show-a-01", Stored{Credential: c, Signature: sig}); err != nil {
		t.Fatal(err)
	}
	s, err := d.ReadCredential("show-a-01")
	if err != nil || s.Credential["status"] != "0" {
		t.Fatalf("read credential: %v", err)
	}
	fp, _ := d.StatusKeyFingerprint()
	l, _, err := d.ReadStatus("issuer-demo", fp, now)
	if err != nil || l.ListVersion != 1 || len(l.Revoked) != 0 {
		t.Fatalf("status %+v %v", l, err)
	}
}

func TestDirPermissions(t *testing.T) {
	_, path := newDir(t)
	if err := os.Chmod(path, 0o755); err != nil {
		t.Fatal(err)
	}
	if _, err := OpenDir(path); err == nil {
		t.Fatal("opened a 0755 directory")
	}
	os.Chmod(path, 0o700)
	d, _ := OpenDir(path)
	os.Chmod(filepath.Join(path, "issuer.key"), 0o644)
	if _, err := d.IssuerKey(); err == nil {
		t.Fatal("read a 0644 key")
	}
	link := filepath.Join(t.TempDir(), "link")
	if err := os.Symlink(path, link); err != nil {
		t.Fatal(err)
	}
	if _, err := OpenDir(link); err == nil {
		t.Fatal("opened through a symlink")
	}
}

// Design 6: init never resets an existing list; a refresh never drops a
// revocation; a missing or invalid list is not signed over; the status
// key is the task's.
func TestStatusListWriter(t *testing.T) {
	d, path := newDir(t)
	if err := InitDir(path, "issuer-demo", []string{"consumer-a"}, now); err == nil {
		t.Fatal("re-initialised over an existing list")
	}
	if err := d.Revoke("4", now+1); err != nil {
		t.Fatal(err)
	}
	if err := d.Refresh(now + 2); err != nil {
		t.Fatal(err)
	}
	fp, _ := d.StatusKeyFingerprint()
	l, sum, err := d.ReadStatus("issuer-demo", fp, now+3)
	if err != nil || !slices.Contains(l.Revoked, "4") || l.ListVersion != 3 || sum == "" {
		t.Fatalf("after revoke and refresh: %+v %v", l, err)
	}
	if _, _, err := d.ReadStatus("issuer-demo", "other-fingerprint", now+3); err == nil {
		t.Fatal("read with a status key that is not the task's")
	}
	if _, _, err := d.ReadStatus("issuer-other", fp, now+3); err == nil {
		t.Fatal("read another issuer's list")
	}
	if _, _, err := d.ReadStatus("issuer-demo", fp, now+2+MaxStatusWindow); err == nil {
		t.Fatal("read a stale list")
	}
	if _, _, err := d.ReadStatus("issuer-demo", fp, now+2-MaxStatusSkew-1); err == nil {
		t.Fatal("read a list from the future")
	}
	// A corrupted list is neither read nor signed over.
	os.WriteFile(filepath.Join(path, "status.json"), []byte("{"), 0o600)
	if _, _, err := d.ReadStatus("issuer-demo", fp, now+3); err == nil {
		t.Fatal("read a corrupt list")
	}
	if err := d.Refresh(now + 4); err == nil {
		t.Fatal("signed over a corrupt list")
	}
	os.Remove(filepath.Join(path, "status.json"))
	if err := d.Refresh(now + 4); err == nil {
		t.Fatal("signed over a missing list")
	}
}

// Design 5: a missing or invalid counter refuses to issue, never restarts.
func TestCounterRefuses(t *testing.T) {
	d, path := newDir(t)
	os.Remove(filepath.Join(path, "status.counter"))
	if _, err := d.ReserveStatusIndex(); err == nil {
		t.Fatal("issued from a missing counter")
	}
	os.WriteFile(filepath.Join(path, "status.counter"), []byte("07"), 0o600)
	if _, err := d.ReserveStatusIndex(); err == nil {
		t.Fatal("issued from a malformed counter")
	}
}

// Design 7 step 3: missing, corrupt or unregistered holder material is
// ErrHolderMaterial; the registered holder presents.
func TestPresentHolderMaterial(t *testing.T) {
	d, path := newDir(t)
	if _, ok, err := d.IssueTask("show-a-01", "consumer-a", now+3600); !ok || err != nil {
		t.Fatalf("issue: %t %v", ok, err)
	}
	if _, ok, err := d.IssueTask("show-c-01", "consumer-c", now+3600); ok || err != nil {
		t.Fatalf("an unregistered consumer was issued: %v", err)
	}
	r := Request{VerifierID: "host-1", ChallengeID: "chl-1", IntentHash: "sha256:intent-1", Disclose: Mandatory}
	if _, err := d.Present("show-a-01", "consumer-a", r); err != nil {
		t.Fatal(err)
	}
	for name, mut := range map[string]func(){
		"no credential": func() { os.Remove(filepath.Join(path, "holder", "show-a-01.cred")) },
		"a corrupt key": func() { os.WriteFile(filepath.Join(path, "holder", "consumer-a.key"), []byte("{"), 0o600) },
		"a corrupt signature": func() {
			s, _ := d.ReadCredential("show-a-01")
			s.Signature[len(s.Signature)/2] ^= 1
			d.WriteCredential("show-a-01", s)
		},
		"another holder's key": func() {
			b, _ := os.ReadFile(filepath.Join(path, "holder", "consumer-b.key"))
			os.WriteFile(filepath.Join(path, "holder", "consumer-a.key"), b, 0o600)
		},
	} {
		t.Run(name, func(t *testing.T) {
			d, path = newDir(t)
			d.IssueTask("show-a-01", "consumer-a", now+3600)
			mut()
			if _, err := d.Present("show-a-01", "consumer-a", r); !errors.Is(err, ErrHolderMaterial) {
				t.Fatalf("%s: %v", name, err)
			}
		})
	}
}

func TestInitRefusesConsumerIDs(t *testing.T) {
	for _, cs := range [][]string{{"../escape"}, {"consumer-a", "consumer-a"}, {""}} {
		if err := InitDir(filepath.Join(t.TempDir(), "cred"), "issuer-demo", cs, now); err == nil {
			t.Errorf("initialised with %q", cs)
		}
	}
}

// v2 (docs/commercial-venues.md 3.1): with subjects.json the directory issues apa.credential.v2; a venue's
// request discloses only the attributes it names, and Verify reports them as the claims.
func TestSubjectsIssueV2AndDiscloseOnlyWhatIsAsked(t *testing.T) {
	d, _ := newDir(t)
	if _, ok, err := d.Subjects(); ok || err != nil {
		t.Fatalf("no subjects.json: v1 (%v %v)", ok, err)
	}
	if s, _, err := d.IssueTask("show-a-01", "consumer-a", now+3600); err != nil || s.Credential["schema"] != Schema {
		t.Fatalf("without subjects: %v %v", s.Credential["schema"], err)
	}
	for _, bad := range []map[string]map[string]string{
		{"consumer-a": {"buyer_type": "business", "country": "TW"}},                                       // one missing
		{"consumer-a": {"buyer_type": "business", "country": "tw", "age_over_18": "true"}},                // lowercase country
		{"consumer-a": {"buyer_type": "company", "country": "TW", "age_over_18": "true"}},                 // not a value
		{"consumer-a": {"buyer_type": "business", "country": "TW", "age_over_18": "yes"}},                 // not a boolean
		{"Consumer A": {"buyer_type": "business", "country": "TW", "age_over_18": "true"}},                // not an ID
	} {
		if d.SetSubjects(bad) == nil {
			t.Fatalf("accepted %v", bad)
		}
	}
	subjects := map[string]map[string]string{"consumer-a": {"buyer_type": "business", "country": "TW", "age_over_18": "true"},
		"consumer-b": {"buyer_type": "business", "country": "TW", "age_over_18": "false"}}
	if err := d.SetSubjects(subjects); err != nil {
		t.Fatal(err)
	}
	s, ok, err := d.IssueTask("show-b-02", "consumer-b", now+3600)
	if err != nil || !ok || s.Credential["schema"] != SchemaV2 || s.Credential["age_over_18"] != "false" {
		t.Fatalf("v2: %v %v %v", s.Credential, ok, err)
	}
	holders, _ := d.Holders()
	_, pk, _ := d.IssuerPublic()
	e := Expect{IssuerID: "issuer-demo", IssuerPK: pk, Holder: holders["consumer-b"], TaskID: "show-b-02", Expiry: s.Credential["expiry"], Status: s.Credential["status"]}
	list, sum, _ := d.ReadStatus("issuer-demo", mustFP(t, d), now)
	for _, c := range []struct {
		attrs  []string
		claims string
	}{{nil, ""}, {[]string{"age_over_18"}, "age_over_18=false"}, {[]string{"buyer_type", "country"}, "buyer_type=business,country=TW"}} {
		r := Request{VerifierID: "host-1", ChallengeID: "chl-1", IntentHash: "0x" + "ab", Disclose: slices.Concat(Mandatory, c.attrs), Schema: SchemaV2}
		p, err := d.Present("show-b-02", "consumer-b", r)
		if err != nil {
			t.Fatal(err)
		}
		if len(p.Disclosed) != len(Mandatory)+len(c.attrs) || p.Disclosed["consumer_id"] != "" {
			t.Fatalf("disclosed %v", p.Disclosed)
		}
		if o := Verify(r, p, e, Status{List: list, SHA256: sum}, now); o.Result != Valid || o.Claims != c.claims {
			t.Fatalf("%v: %+v", c.attrs, o)
		}
		// The same proof checked as a v1 request: the message count and the schema do not match.
		v1 := r
		v1.Schema, v1.Disclose = "", Mandatory
		if o := Verify(v1, p, e, Status{List: list, SHA256: sum}, now); o.Result != Invalid {
			t.Fatalf("a v2 proof passed as v1: %+v", o)
		}
	}
	// A v1 credential cannot answer for an attribute.
	if _, err := d.Present("show-a-01", "consumer-a", Request{VerifierID: "host-1", ChallengeID: "chl-2", IntentHash: "0xab", Disclose: slices.Concat(Mandatory, []string{"age_over_18"}), Schema: SchemaV2}); err == nil {
		t.Fatal("a v1 credential presented an attribute")
	}
	// A consumer the subjects do not name gets no credential at all (never v1 beside v2).
	delete(subjects, "consumer-a")
	if err := d.SetSubjects(subjects); err != nil {
		t.Fatal(err)
	}
	if _, _, err := d.IssueTask("show-a-02", "consumer-a", now+3600); err == nil {
		t.Fatal("issued without the consumer's attributes")
	}
}

func mustFP(t *testing.T, d *Dir) string {
	t.Helper()
	fp, err := d.StatusKeyFingerprint()
	if err != nil {
		t.Fatal(err)
	}
	return fp
}
