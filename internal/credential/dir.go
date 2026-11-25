package credential

import (
	"bytes"
	"crypto/ed25519"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"syscall"

	"github.com/IBM/idemix/bbs"
)

// Dir is the credential directory (--credential-dir, design 5): the test
// issuer's keys, the status list and its counter, the holders' trust file,
// the holders' keys and issued credentials. 0700 and owned by this user,
// no symlinks anywhere (stricter than the Store's dbPath); every key file
// 0600.
type Dir struct{ path string }

// The status list's freshness window and the clock skew it tolerates.
const (
	MaxStatusWindow = 600
	MaxStatusSkew   = 60
)

// OpenDir checks the directory and its holder subdirectory.
func OpenDir(path string) (*Dir, error) {
	for _, p := range []string{path, filepath.Join(path, "holder")} {
		if err := checkPrivate(p, true); err != nil {
			return nil, err
		}
	}
	return &Dir{path}, nil
}

// checkPrivate: no symlink, owned by this user, 0700 (dir) or 0600 (file).
func checkPrivate(p string, dir bool) error {
	fi, err := os.Lstat(p)
	if err != nil {
		return fmt.Errorf("credential dir: %w", err)
	}
	st, ok := fi.Sys().(*syscall.Stat_t)
	want := os.FileMode(0o600)
	if dir {
		want = 0o700
	}
	switch {
	case fi.Mode()&os.ModeSymlink != 0:
		return fmt.Errorf("credential dir: %s is a symlink", p)
	case dir != fi.IsDir() || (!dir && !fi.Mode().IsRegular()):
		return fmt.Errorf("credential dir: %s has the wrong type", p)
	case fi.Mode().Perm() != want:
		return fmt.Errorf("credential dir: %s is %o, want %o", p, fi.Mode().Perm(), want)
	case !ok || int(st.Uid) != os.Getuid():
		return fmt.Errorf("credential dir: %s is not this user's", p)
	}
	return nil
}

func (d *Dir) file(name string) string { return filepath.Join(d.path, name) }

// read reads a private file after checking it.
func (d *Dir) read(name string) ([]byte, error) {
	p := d.file(name)
	if err := checkPrivate(p, false); err != nil {
		return nil, err
	}
	return os.ReadFile(p)
}

// write writes a private file atomically: temp file, fsync, rename, fsync
// the directory (design 6).
func (d *Dir) write(name string, data []byte) error {
	p := d.file(name)
	dir := filepath.Dir(p)
	f, err := os.CreateTemp(dir, ".tmp-*")
	if err != nil {
		return err
	}
	tmp := f.Name()
	ok := false
	defer func() {
		if !ok {
			os.Remove(tmp)
		}
	}()
	if err := f.Chmod(0o600); err != nil {
		f.Close()
		return err
	}
	if _, err := f.Write(data); err != nil {
		f.Close()
		return err
	}
	if err := f.Sync(); err != nil {
		f.Close()
		return err
	}
	if err := f.Close(); err != nil {
		return err
	}
	if err := os.Rename(tmp, p); err != nil {
		return err
	}
	ok = true
	df, err := os.Open(dir)
	if err != nil {
		return err
	}
	defer df.Close()
	return df.Sync()
}

type issuerFile struct {
	Version string `json:"version"`
	Issuer  string `json:"issuer"`
	Seed    string `json:"seed"`
}

type issuerPub struct {
	Version string `json:"version"`
	Issuer  string `json:"issuer"`
	PK      []byte `json:"pk"`
}

type holdersFile struct {
	Version string            `json:"version"`
	Holders map[string]string `json:"holders"` // consumer -> holder field
}

const fileVersion = "apa.credential-dir.v1"

// subjectsFile is the issuer's attributes of each consumer (docs/commercial-venues.md 3.1): with it, the
// directory issues apa.credential.v2; without it, v1 as before.
type subjectsFile struct {
	Version  string                       `json:"version"`
	Subjects map[string]map[string]string `json:"subjects"` // consumer -> attribute -> value
}

// Subjects reads subjects.json; ok false when the directory has none (it issues v1).
func (d *Dir) Subjects() (subjects map[string]map[string]string, ok bool, err error) {
	if _, err := os.Lstat(d.file("subjects.json")); errors.Is(err, os.ErrNotExist) {
		return nil, false, nil
	}
	raw, err := d.read("subjects.json")
	if err != nil {
		return nil, false, err
	}
	var f subjectsFile
	if json.Unmarshal(raw, &f) != nil || f.Version != fileVersion || checkSubjects(f.Subjects) != nil {
		return nil, false, errors.New("subjects.json: format")
	}
	return f.Subjects, true, nil
}

// SetSubjects writes subjects.json: every consumer with exactly the v2 attributes, each well formed. The
// attributes are the caller's word: an eKYC record of earlier ones (ekyc.json) is removed, never left to vouch
// for these.
func (d *Dir) SetSubjects(subjects map[string]map[string]string) error {
	return d.setSubjects(subjects, nil)
}

// EKYC records where the subjects' attributes came from (docs/commercial-ekyc.md 3): the vendor's session and
// what was derived from its decision. Never the person's name, birth date, document number or images: those
// stay with the vendor.
type EKYC struct {
	Version     string            `json:"version"`
	Provider    string            `json:"provider"`
	Environment string            `json:"environment"` // "sandbox": no real person was checked
	SessionID   string            `json:"session_id"`
	DecidedAt   string            `json:"decided_at"`
	Consumer    string            `json:"consumer"`   // who was verified
	Dependents  []string          `json:"dependents"` // whose attributes the verified consumer declared (a guardian's children)
	Derived     map[string]string `json:"derived"`    // the attributes taken from the decision
}

const ekycVersion = "apa.ekyc.v1"

// SetVerifiedSubjects writes subjects.json and, after it, ekyc.json: the attributes with the record of the
// verification they came from.
func (d *Dir) SetVerifiedSubjects(subjects map[string]map[string]string, e EKYC) error {
	if e.Provider == "" || e.Environment == "" || e.SessionID == "" || subjects[e.Consumer] == nil || len(e.Derived) == 0 {
		return errors.New("ekyc: provider, environment, session and a verified consumer among the subjects")
	}
	for a, v := range e.Derived {
		if subjects[e.Consumer][a] != v {
			return fmt.Errorf("ekyc: %s's %s is not the derived one", e.Consumer, a)
		}
	}
	e.Version = ekycVersion
	return d.setSubjects(subjects, &e)
}

// EKYC reads ekyc.json; nil when the directory has none (the subjects are the caller's word).
func (d *Dir) EKYC() (*EKYC, error) {
	if _, err := os.Lstat(d.file("ekyc.json")); errors.Is(err, os.ErrNotExist) {
		return nil, nil
	}
	raw, err := d.read("ekyc.json")
	if err != nil {
		return nil, err
	}
	var e EKYC
	if json.Unmarshal(raw, &e) != nil || e.Version != ekycVersion || e.Provider == "" || e.SessionID == "" || e.Consumer == "" {
		return nil, errors.New("ekyc.json: format")
	}
	return &e, nil
}

func (d *Dir) setSubjects(subjects map[string]map[string]string, e *EKYC) error {
	if err := checkSubjects(subjects); err != nil {
		return err
	}
	return d.locked(func() error {
		if err := os.Remove(d.file("ekyc.json")); err != nil && !errors.Is(err, os.ErrNotExist) {
			return err
		}
		raw, _ := json.Marshal(subjectsFile{Version: fileVersion, Subjects: subjects})
		if err := d.write("subjects.json", raw); err != nil || e == nil {
			return err
		}
		raw, _ = json.Marshal(e)
		return d.write("ekyc.json", raw)
	})
}

func checkSubjects(subjects map[string]map[string]string) error {
	if len(subjects) == 0 {
		return errors.New("subjects: none")
	}
	for c, attrs := range subjects {
		if !validField("consumer_id", c) || len(attrs) != len(Attributes) {
			return fmt.Errorf("subjects: %s: a consumer ID and exactly %v", c, Attributes)
		}
		for _, a := range Attributes {
			if !validField(a, attrs[a]) {
				return fmt.Errorf("subjects: %s: %s", c, a)
			}
		}
	}
	return nil
}

// InitDir creates a new credential directory with test keys for issuer
// and one holder per consumer, the first status list and its counter. It
// refuses an existing status list: re-initialising would reset the list
// version (design 6).
func InitDir(path, issuerID string, consumers []string, now int64) error {
	if len(consumers) == 0 {
		return errors.New("init: no consumer")
	}
	for i, c := range consumers { // each names a key file
		if !validField("consumer_id", c) || slices.Contains(consumers[:i], c) {
			return errors.New("init: a consumer ID is malformed or repeated")
		}
	}
	if err := os.MkdirAll(filepath.Join(path, "holder"), 0o700); err != nil {
		return err
	}
	for _, p := range []string{path, filepath.Join(path, "holder")} {
		if err := os.Chmod(p, 0o700); err != nil {
			return err
		}
	}
	d, err := OpenDir(path)
	if err != nil {
		return err
	}
	if _, err := os.Lstat(d.file("status.json")); err == nil {
		return errors.New("init: a status list exists already")
	}
	seed := make([]byte, 32)
	rand.Read(seed)
	k, err := NewIssuerKey(issuerID, seed)
	if err != nil {
		return err
	}
	_, statusKey, _ := ed25519.GenerateKey(rand.Reader)
	holders := holdersFile{Version: fileVersion, Holders: map[string]string{}}
	for _, c := range consumers {
		_, hk, _ := ed25519.GenerateKey(rand.Reader)
		if err := d.write(filepath.Join("holder", c+".key"), []byte(hex.EncodeToString(hk.Seed()))); err != nil {
			return err
		}
		holders.Holders[c] = HolderField(hk.Public().(ed25519.PublicKey))
	}
	for name, v := range map[string]any{
		"issuer.key":   issuerFile{fileVersion, issuerID, hex.EncodeToString(seed)},
		"issuer.pub":   issuerPub{fileVersion, issuerID, k.PK},
		"holders.json": holders,
	} {
		raw, _ := json.Marshal(v)
		if err := d.write(name, raw); err != nil {
			return err
		}
	}
	if err := d.write("status.key", []byte(hex.EncodeToString(statusKey.Seed()))); err != nil {
		return err
	}
	if err := d.write("status.pub", []byte(hex.EncodeToString(statusKey.Public().(ed25519.PublicKey)))); err != nil {
		return err
	}
	if err := d.write("status.counter", []byte("0")); err != nil {
		return err
	}
	return d.signStatus(StatusList{Version: statusVersion, Issuer: issuerID, ListVersion: 1, IssuedAt: now, NextUpdate: now + MaxStatusWindow, Revoked: []string{}}, statusKey)
}

// IssuerKey reads the issuer's private key.
func (d *Dir) IssuerKey() (*IssuerKey, error) {
	raw, err := d.read("issuer.key")
	if err != nil {
		return nil, err
	}
	var f issuerFile
	if json.Unmarshal(raw, &f) != nil || f.Version != fileVersion {
		return nil, errors.New("issuer.key: format")
	}
	seed, err := hex.DecodeString(f.Seed)
	if err != nil || len(seed) != 32 {
		return nil, errors.New("issuer.key: seed")
	}
	return NewIssuerKey(f.Issuer, seed)
}

// IssuerPublic reads the issuer's ID and public key.
func (d *Dir) IssuerPublic() (string, []byte, error) {
	raw, err := d.read("issuer.pub")
	if err != nil {
		return "", nil, err
	}
	var f issuerPub
	if json.Unmarshal(raw, &f) != nil || f.Version != fileVersion || len(f.PK) == 0 {
		return "", nil, errors.New("issuer.pub: format")
	}
	return f.Issuer, f.PK, nil
}

func (d *Dir) ed25519Seed(name string) (ed25519.PrivateKey, error) {
	raw, err := d.read(name)
	if err != nil {
		return nil, err
	}
	seed, err := hex.DecodeString(string(raw))
	if err != nil || len(seed) != ed25519.SeedSize {
		return nil, fmt.Errorf("%s: format", name)
	}
	return ed25519.NewKeyFromSeed(seed), nil
}

// StatusPublic reads the status list's public key.
func (d *Dir) StatusPublic() (ed25519.PublicKey, error) {
	raw, err := d.read("status.pub")
	if err != nil {
		return nil, err
	}
	pk, err := hex.DecodeString(string(raw))
	if err != nil || len(pk) != ed25519.PublicKeySize {
		return nil, errors.New("status.pub: format")
	}
	return pk, nil
}

// Holders reads the trust file: consumer -> holder field; every holder key
// distinct (design 5).
func (d *Dir) Holders() (map[string]string, error) {
	raw, err := d.read("holders.json")
	if err != nil {
		return nil, err
	}
	var f holdersFile
	if json.Unmarshal(raw, &f) != nil || f.Version != fileVersion {
		return nil, errors.New("holders.json: format")
	}
	seen := map[string]bool{}
	for c, h := range f.Holders {
		if !validField("consumer_id", c) || !validField("holder", h) || seen[h] {
			return nil, errors.New("holders.json: a consumer or holder key is malformed or repeated")
		}
		seen[h] = true
	}
	return f.Holders, nil
}

// HolderKey reads a consumer's holder key.
func (d *Dir) HolderKey(consumer string) (ed25519.PrivateKey, error) {
	if !validField("consumer_id", consumer) {
		return nil, errors.New("holder: consumer")
	}
	return d.ed25519Seed(filepath.Join("holder", consumer+".key"))
}

// Stored is an issued credential as the holder keeps it.
type Stored struct {
	Version    string     `json:"version"`
	Credential Credential `json:"credential"`
	Signature  []byte     `json:"signature"`
	// EKYC: the verification the attributes came from when this was issued (ekyc.json then; the consumer it
	// verified or one of its dependents). Beside the credential, not in it: the issuer's note of its own source,
	// which no verifier checks. Absent: the attributes were the issuer's own word.
	EKYC *EKYC `json:"ekyc,omitempty"`
}

// WriteCredential stores a task's issued credential (atomically).
func (d *Dir) WriteCredential(taskID string, s Stored) error {
	if !validField("task_id", taskID) {
		return errors.New("credential: task")
	}
	s.Version = fileVersion
	raw, err := json.Marshal(s)
	if err != nil {
		return err
	}
	return d.write(filepath.Join("holder", taskID+".cred"), raw)
}

// ReadCredential reads a task's issued credential.
func (d *Dir) ReadCredential(taskID string) (Stored, error) {
	var s Stored
	if !validField("task_id", taskID) {
		return s, errors.New("credential: task")
	}
	raw, err := d.read(filepath.Join("holder", taskID+".cred"))
	if err != nil {
		return s, err
	}
	if json.Unmarshal(raw, &s) != nil || s.Version != fileVersion || s.Credential.Check() != nil || len(s.Signature) == 0 {
		return s, errors.New("credential: format")
	}
	return s, nil
}

// IssueTask issues task's credential to consumer as the Host does before
// StartTask (design 5): reserve a status index, sign with the issuer key,
// write holder/<task>.cred. ok is false, and nothing is reserved, when the
// consumer has no registered holder: the task starts with an absent
// credential. A failed StartTask leaves the index unused and the file to be
// overwritten by the next issue for the task.
func (d *Dir) IssueTask(task, consumer string, expiry int64) (s Stored, ok bool, err error) {
	holders, err := d.Holders()
	if err != nil {
		return s, false, err
	}
	holder, ok := holders[consumer]
	if !ok {
		return s, false, nil
	}
	subjects, v2, err := d.Subjects()
	if err != nil {
		return s, false, err
	}
	if v2 && subjects[consumer] == nil {
		return s, false, fmt.Errorf("subjects.json: no attributes for %s", consumer) // fail closed: never a v1 credential beside v2 ones
	}
	k, err := d.IssuerKey()
	if err != nil {
		return s, false, err
	}
	idx, err := d.ReserveStatusIndex()
	if err != nil {
		return s, false, err
	}
	c := Credential{"schema": Schema, "issuer": k.ID, "task_id": task, "consumer_id": consumer, "c0": C0Placeholder,
		"expiry": strconv.FormatInt(expiry, 10), "holder": holder, "status": idx}
	if v2 {
		c["schema"] = SchemaV2
		for _, a := range Attributes {
			c[a] = subjects[consumer][a]
		}
	}
	sig, err := k.Issue(c)
	if err != nil {
		return s, false, err
	}
	s = Stored{Credential: c, Signature: sig}
	if e, err := d.EKYC(); err != nil {
		return s, false, err
	} else if e != nil && v2 && (consumer == e.Consumer || slices.Contains(e.Dependents, consumer)) {
		s.EKYC = e
	}
	return s, true, d.WriteCredential(task, s)
}

// ErrHolderMaterial is a holder whose stored credential or key is missing,
// corrupt, unreadable or not the registered one (design 7 step 3): a
// decided Deny, which a retry would not change.
var ErrHolderMaterial = errors.New("credential: holder material unavailable")

// Present answers r for task as consumer's holder, from the task's stored
// credential and the consumer's key, which must be the registered holder
// and the credential's. A material problem is ErrHolderMaterial; any other
// error is a transient failure to build the proof (C2-D12: nothing is
// submitted, the challenge stays issued).
func (d *Dir) Present(task, consumer string, r Request) (Presentation, error) {
	c, sig, pk, k, err := d.material(task, consumer)
	if err != nil {
		return Presentation{}, err
	}
	return Present(c, sig, pk, k, r)
}

// material is task's stored credential, its signature, the issuer's key and
// consumer's holder key, checked to be the registered holder's and sound.
func (d *Dir) material(task, consumer string) (Credential, []byte, []byte, ed25519.PrivateKey, error) {
	s, err := d.ReadCredential(task)
	if err != nil {
		return nil, nil, nil, nil, fmt.Errorf("%w: %v", ErrHolderMaterial, err)
	}
	k, err := d.HolderKey(consumer)
	if err != nil {
		return nil, nil, nil, nil, fmt.Errorf("%w: %v", ErrHolderMaterial, err)
	}
	holders, err := d.Holders()
	if err != nil {
		return nil, nil, nil, nil, fmt.Errorf("%w: %v", ErrHolderMaterial, err)
	}
	_, pk, err := d.IssuerPublic()
	if err != nil {
		return nil, nil, nil, nil, fmt.Errorf("%w: %v", ErrHolderMaterial, err)
	}
	c := s.Credential
	if h := HolderField(k.Public().(ed25519.PublicKey)); h != holders[consumer] || h != c["holder"] || c["consumer_id"] != consumer || c["task_id"] != task {
		return nil, nil, nil, nil, fmt.Errorf("%w: the key or credential is not this consumer's registered holder's", ErrHolderMaterial)
	}
	if err := bbs.New(curve).Verify(c.messages(), bytes.Clone(s.Signature), pk); err != nil { // a corrupt credential, not a failed proof
		return nil, nil, nil, nil, fmt.Errorf("%w: the stored signature does not verify", ErrHolderMaterial)
	}
	return c, s.Signature, pk, k, nil
}

const statusVersion = "apa.status-list.v1"

// StatusList is the issuer's signed revocation list (design 6).
type StatusList struct {
	Version     string   `json:"version"`
	Issuer      string   `json:"issuer"`
	ListVersion uint64   `json:"list_version"`
	IssuedAt    int64    `json:"issued_at"`
	NextUpdate  int64    `json:"next_update"`
	Revoked     []string `json:"revoked"`
}

type signedStatus struct {
	List StatusList `json:"list"`
	Sig  []byte     `json:"sig"`
}

func (d *Dir) signStatus(l StatusList, key ed25519.PrivateKey) error {
	body, err := json.Marshal(l)
	if err != nil {
		return err
	}
	raw, err := json.Marshal(signedStatus{l, ed25519.Sign(key, body)})
	if err != nil {
		return err
	}
	return d.write("status.json", raw)
}

// ReadStatus reads and verifies the status list at now, with no cache
// (design 6): it must exist, verify with status.pub, name issuer, be
// within its window and not from the future. The Store adds the version
// checks (monotonic; the same version, the same digest). The digest is of
// the file's bytes. keyFP is the status key's fingerprint in the task's
// snapshot: a status.pub swapped since the task started is refused.
func (d *Dir) ReadStatus(issuer, keyFP string, now int64) (*StatusList, string, error) {
	raw, err := d.read("status.json")
	if err != nil {
		return nil, "", err
	}
	pk, err := d.StatusPublic()
	if err != nil {
		return nil, "", err
	}
	if Fingerprint(pk) != keyFP {
		return nil, "", errors.New("status list: the status key is not the task's")
	}
	l, err := parseStatus(raw, pk)
	if err != nil {
		return nil, "", err
	}
	switch {
	case l.Issuer != issuer:
		return nil, "", errors.New("status list: another issuer")
	case l.NextUpdate-l.IssuedAt > MaxStatusWindow || l.NextUpdate <= l.IssuedAt:
		return nil, "", errors.New("status list: window")
	case now >= l.NextUpdate:
		return nil, "", errors.New("status list: stale")
	case l.IssuedAt > now+MaxStatusSkew:
		return nil, "", errors.New("status list: from the future")
	}
	return l, Fingerprint(raw), nil
}

func parseStatus(raw []byte, pk ed25519.PublicKey) (*StatusList, error) {
	var s signedStatus
	if err := json.Unmarshal(raw, &s); err != nil {
		return nil, errors.New("status list: format")
	}
	body, _ := json.Marshal(s.List)
	if s.List.Version != statusVersion || !ed25519.Verify(pk, body, s.Sig) {
		return nil, errors.New("status list: signature")
	}
	return &s.List, nil
}

// locked runs f under the exclusive status.lock (one writer of the status
// list and the counter at a time, across processes).
func (d *Dir) locked(f func() error) error {
	lf, err := os.OpenFile(d.file("status.lock"), os.O_RDWR|os.O_CREATE, 0o600)
	if err != nil {
		return err
	}
	defer lf.Close()
	if err := syscall.Flock(int(lf.Fd()), syscall.LOCK_EX); err != nil {
		return err
	}
	defer syscall.Flock(int(lf.Fd()), syscall.LOCK_UN)
	return f()
}

// update re-reads and verifies the current list, applies change (it may
// only add revocations) and signs the next version. A missing or invalid
// current list is refused, never treated as empty (design 6).
func (d *Dir) update(now int64, change func(*StatusList)) error {
	return d.locked(func() error {
		key, err := d.ed25519Seed("status.key")
		if err != nil {
			return err
		}
		raw, err := d.read("status.json")
		if err != nil {
			return fmt.Errorf("status list: refuse to sign over a missing list: %w", err)
		}
		cur, err := parseStatus(raw, key.Public().(ed25519.PublicKey))
		if err != nil {
			return fmt.Errorf("status list: refuse to sign over an invalid list: %w", err)
		}
		next := *cur
		next.Revoked = slices.Clone(cur.Revoked)
		change(&next)
		for _, r := range cur.Revoked {
			if !slices.Contains(next.Revoked, r) {
				return errors.New("status list: a revocation would be dropped")
			}
		}
		next.ListVersion, next.IssuedAt, next.NextUpdate = cur.ListVersion+1, now, now+MaxStatusWindow
		return d.signStatus(next, key)
	})
}

// Refresh signs the current revocations again with a new window.
func (d *Dir) Refresh(now int64) error { return d.update(now, func(*StatusList) {}) }

// Revoke adds a status index to the list.
func (d *Dir) Revoke(index string, now int64) error {
	if !validField("status", index) {
		return errors.New("revoke: status index")
	}
	return d.update(now, func(l *StatusList) {
		if !slices.Contains(l.Revoked, index) {
			l.Revoked = append(l.Revoked, index)
		}
	})
}

// ReserveStatusIndex allocates the next status index from status.counter
// under the lock; a missing or invalid counter is refused, never restarted
// from 0 (design 5).
func (d *Dir) ReserveStatusIndex() (string, error) {
	var out string
	err := d.locked(func() error {
		raw, err := d.read("status.counter")
		if err != nil {
			return fmt.Errorf("status counter: refuse to issue: %w", err)
		}
		n, err := strconv.ParseUint(string(raw), 10, 32)
		if err != nil || !validField("status", string(raw)) {
			return errors.New("status counter: refuse to issue: format")
		}
		if err := d.write("status.counter", []byte(strconv.FormatUint(n+1, 10))); err != nil {
			return err
		}
		out = strconv.FormatUint(n, 10)
		return nil
	})
	return out, err
}

// StatusKeyFingerprint names the status public key in a task snapshot.
func (d *Dir) StatusKeyFingerprint() (string, error) {
	pk, err := d.StatusPublic()
	if err != nil {
		return "", err
	}
	return Fingerprint(pk), nil
}
