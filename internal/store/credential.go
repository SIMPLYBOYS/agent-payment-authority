package store

// apa.pay.v2's BBS+ credential in the Store (C2b, docs/commercial-c2-design.md
// 3.3, 5, 7 and 8). The Store holds no issuer or holder key: the Host issues
// the credential before StartTask and presents it inside Commit; the Store
// keeps the task's snapshot, the challenges and the verification records,
// and verifies under its write transaction.

import (
	"encoding/hex"
	"errors"
	"regexp"
	"strconv"

	"github.com/SIMPLYBOYS/agent-payment-authority/internal/credential"
)

// MaxCredentialTTL caps a credential's expiry after its task starts (C2-D10).
const MaxCredentialTTL = 24 * 3600

var sha256Hex = regexp.MustCompile(`^[0-9a-f]{64}$`)

// BBSCredential is an apa.pay.v2 task's credential_bbs snapshot (design 5):
// the trust settings and the issued credential's fields, fixed when the task
// starts; files changed later do not change it.
type BBSCredential struct {
	Schema          string `json:"schema"`
	Issuer          string `json:"issuer"`
	IssuerPK        string `json:"issuer_pk"`         // hex
	StatusKeySHA256 string `json:"status_key_sha256"` // the status list's key, credential.StatusKeyFingerprint
	ConsumerID      string `json:"consumer_id"`
	Holder          string `json:"holder"` // the consumer's registered holder field
	TaskID          string `json:"task_id"`
	Expiry          uint64 `json:"expiry"`
	Status          string `json:"status"`           // the status list index
	SignatureSHA256 string `json:"signature_sha256"` // the issued signature's, for tracing only
}

// validate checks every field's spelling, by the envelope's own rules.
func (b BBSCredential) validate() error {
	c := credential.Credential{"schema": b.Schema, "issuer": b.Issuer, "task_id": b.TaskID, "consumer_id": b.ConsumerID, "c0": credential.C0Placeholder,
		"expiry": strconv.FormatUint(b.Expiry, 10), "holder": b.Holder, "status": b.Status}
	pk, err := hex.DecodeString(b.IssuerPK)
	switch {
	case c.Check() != nil:
		return c.Check()
	case err != nil || len(pk) == 0 || hex.EncodeToString(pk) != b.IssuerPK:
		return errors.New("credential_bbs: issuer_pk is lowercase hex")
	case !sha256Hex.MatchString(b.StatusKeySHA256) || !sha256Hex.MatchString(b.SignatureSHA256):
		return errors.New("credential_bbs: status_key_sha256 and signature_sha256 are SHA-256 in hex")
	}
	return nil
}

// bbs is an apa.pay.v2 task's stored credential snapshot; nil when the task
// started without one (absent).
func (t *taskTx) bbs() (*BBSCredential, error) {
	if !t.row.CredentialSnap.Valid {
		return nil, nil
	}
	b := new(BBSCredential)
	return b, decodeSnapshot(t.tx, t.row.CredentialSnap.String, "credential_bbs", b)
}

// NewBBSCredential is the snapshot of a credential issued from dir
// (credential.Dir.IssueTask), with dir's trust settings as they are now.
func NewBBSCredential(dir *credential.Dir, s credential.Stored) (*BBSCredential, error) {
	issuer, pk, err := dir.IssuerPublic()
	if err != nil {
		return nil, err
	}
	fp, err := dir.StatusKeyFingerprint()
	if err != nil {
		return nil, err
	}
	c := s.Credential
	expiry, err := strconv.ParseUint(c["expiry"], 10, 64)
	if err != nil || c["issuer"] != issuer {
		return nil, errors.New("credential_bbs: the credential is not this directory's issuer's")
	}
	b := &BBSCredential{Schema: c["schema"], Issuer: issuer, IssuerPK: hex.EncodeToString(pk), StatusKeySHA256: fp, ConsumerID: c["consumer_id"],
		Holder: c["holder"], TaskID: c["task_id"], Expiry: expiry, Status: c["status"], SignatureSHA256: credential.Fingerprint(s.Signature)}
	return b, b.validate()
}
