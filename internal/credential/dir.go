package credential

import (
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

// InitDir creates a new credential directory with test keys for issuer
// and one holder per consumer, the first status list and its counter. It
// refuses an existing status list: re-initialising would reset the list
// version (design 6).
func InitDir(path, issuerID string, consumers []string, now int64) error {
	if len(consumers) == 0 {
		return errors.New("init: no consumer")
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
	sig, err := k.Issue(c)
	if err != nil {
		return s, false, err
	}
	s = Stored{Credential: c, Signature: sig}
	return s, true, d.WriteCredential(task, s)
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
