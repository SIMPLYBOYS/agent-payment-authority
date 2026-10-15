package signer

// Enrollment (E1, decision D2; docs/commercial-human-approval-design.md
// 3.2): the administrator begins it at the Signer's terminal, which shows
// an enrollment ID and a one-time code; the person's browser creates the
// credential through the Host, which relays both ways; the Signer verifies
// the attestation (W3C 7.1) and keeps the credential pending; only the
// administrator's confirm at the terminal makes it active. Accepted risk
// (D2): during enrollment the Host and its page are trusted; the matching
// fingerprints on both sides are not an independent channel.

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"database/sql"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"

	"github.com/go-webauthn/webauthn/protocol"
	"github.com/go-webauthn/webauthn/webauthn"
)

const (
	enrollTTL      = 300                               // seconds
	enrollFailures = 5                                 // wrong codes before an enrollment closes
	codeAlphabet   = "ABCDEFGHJKMNPQRSTUVWXYZ23456789" // no 0/O, 1/I/L
)

// EnrollReply is an enrollment as the Host relays it: the creation options
// while it is pending, the new credential's fingerprint once finished.
type EnrollReply struct {
	EnrollmentID string          `json:"enrollment_id"`
	ApproverID   string          `json:"approver_id,omitempty"`
	State        string          `json:"state,omitempty"`
	Options      json.RawMessage `json:"options,omitempty"`
	Fingerprint  string          `json:"fingerprint,omitempty"` // the credential ID's first 8 bytes, hex
	Flags        string          `json:"flags,omitempty"`
	AAGUID       string          `json:"aaguid,omitempty"`
	ExpiresAt    uint64          `json:"expires_at,omitempty"`
	Error        string          `json:"error,omitempty"`
}

// Fingerprint is the short form of a credential ID both sides show.
func Fingerprint(id []byte) string {
	if len(id) > 8 {
		id = id[:8]
	}
	return hex.EncodeToString(id)
}

func codeHash(id, code string) string {
	s := sha256.Sum256([]byte("agent-payment-authority/pay/v1/enrollment-code\x00" + id + "\x00" + code))
	return hex.EncodeToString(s[:])
}

// EnrollBegin is the administrator's begin: a new enrollment for approver
// with a five-minute window. A previous unconfirmed one of the approver is
// closed. It returns the enrollment ID and the one-time code, for the
// terminal only.
func (c *ApprovalCore) EnrollBegin(ctx context.Context, approverID string) (string, string, uint64, error) {
	if c.cfg.approver(approverID) == nil {
		return "", "", 0, Refusal("APPROVER_UNKNOWN")
	}
	id, err := newID("enr-")
	if err != nil {
		return "", "", 0, err
	}
	raw := make([]byte, 8)
	if _, err := rand.Read(raw); err != nil {
		return "", "", 0, err
	}
	code := make([]byte, 8)
	for i := range code {
		code[i] = codeAlphabet[int(raw[i])%len(codeAlphabet)] // ponytail: modulo bias of 256 % 31, fine for a 5-minute, 5-try code
	}
	var expires uint64
	err = c.inTx(ctx, func(tx *sql.Tx, now uint64) error {
		var handle string
		err := tx.QueryRowContext(ctx, `SELECT user_handle FROM approver_handles WHERE approver_id = ?`, approverID).Scan(&handle)
		if errors.Is(err, sql.ErrNoRows) {
			h := make([]byte, 16) // random, no consumer ID or personal data (design 3.2)
			if _, err := rand.Read(h); err != nil {
				return err
			}
			handle = b64(h)
			if _, err := tx.ExecContext(ctx, `INSERT INTO approver_handles VALUES (?, ?)`, approverID, handle); err != nil {
				return err
			}
		} else if err != nil {
			return err
		}
		u, _, err := c.user(ctx, tx, approverID)
		if err != nil {
			return err
		}
		var exclude []protocol.CredentialDescriptor
		for _, cr := range u.creds {
			exclude = append(exclude, cr.Descriptor())
		}
		_, session, err := c.rp.BeginRegistration(u, webauthn.WithExclusions(exclude),
			webauthn.WithCredentialParameters([]protocol.CredentialParameter{{Type: protocol.PublicKeyCredentialType, Algorithm: -7}}),
			webauthn.WithConveyancePreference(protocol.PreferNoAttestation))
		if err != nil {
			return err
		}
		sess, err := json.Marshal(session)
		if err != nil {
			return err
		}
		// A finished one gives up its credential reference (its credential is revoked
		// just below): a closed enrollment names none (found in V1's device round).
		if _, err := tx.ExecContext(ctx, `UPDATE enrollments SET state = 'closed', credential_id = NULL WHERE approver_id = ? AND state IN ('pending', 'finished')`, approverID); err != nil {
			return err
		}
		if _, err := tx.ExecContext(ctx, `UPDATE credentials SET state = 'revoked', state_reason = 'ENROLLMENT_REPLACED', revoked_at = ?
			WHERE state = 'pending' AND approver_id = ?`, now, approverID); err != nil {
			return err
		}
		expires = now + enrollTTL
		_, err = tx.ExecContext(ctx, `INSERT INTO enrollments (enrollment_id, approver_id, code_hash, session, expires_at, state, created_at) VALUES (?, ?, ?, ?, ?, 'pending', ?)`,
			id, approverID, codeHash(id, string(code)), string(sess), expires, now)
		return err
	})
	if err != nil {
		return "", "", 0, err
	}
	return id, string(code), expires, nil
}

type enrollRow struct {
	id, approver, codeHash, session, state string
	credential                             sql.NullString
	expires                                uint64
	failures                               int
}

// openEnrollment checks the code of a pending enrollment, in a write
// transaction: a wrong code counts, and the enrollment closes after
// enrollFailures; a late one is recorded expired.
func (c *ApprovalCore) openEnrollment(ctx context.Context, tx *sql.Tx, now uint64, id, code string) (enrollRow, error) {
	var e enrollRow
	err := tx.QueryRowContext(ctx, `SELECT enrollment_id, approver_id, code_hash, session, state, credential_id, expires_at, failures FROM enrollments WHERE enrollment_id = ?`, id).
		Scan(&e.id, &e.approver, &e.codeHash, &e.session, &e.state, &e.credential, &e.expires, &e.failures)
	if errors.Is(err, sql.ErrNoRows) {
		return e, Refusal("ENROLLMENT_UNKNOWN")
	} else if err != nil {
		return e, err
	}
	if e.state == "pending" && now >= e.expires {
		if _, err := tx.ExecContext(ctx, `UPDATE enrollments SET state = 'expired' WHERE enrollment_id = ? AND state = 'pending'`, id); err != nil {
			return e, err
		}
		e.state = "expired"
	}
	if e.state != "pending" {
		return e, Refusal("ENROLLMENT_CLOSED")
	}
	if subtle.ConstantTimeCompare([]byte(codeHash(id, code)), []byte(e.codeHash)) != 1 {
		if _, err := tx.ExecContext(ctx, `UPDATE enrollments SET failures = failures + 1,
			state = CASE WHEN failures + 1 >= ? THEN 'closed' ELSE state END WHERE enrollment_id = ?`, enrollFailures, id); err != nil {
			return e, err
		}
		return e, Refusal("ENROLLMENT_CODE")
	}
	return e, nil
}

// commitRefusal keeps a wrong code's count although the call is refused.
func (c *ApprovalCore) enrollTx(ctx context.Context, fn func(tx *sql.Tx, now uint64) error) error {
	var refusal error
	err := c.inTx(ctx, func(tx *sql.Tx, now uint64) error {
		if err := fn(tx, now); err != nil {
			var ref Refusal
			if errors.As(err, &ref) {
				refusal = err
				return nil // commit the count or the expiry
			}
			return err
		}
		return nil
	})
	return errors.Join(err, refusal)
}

// EnrollOptions is the relayed GET of the creation options, by ID and code.
func (c *ApprovalCore) EnrollOptions(ctx context.Context, id, code string) (EnrollReply, error) {
	if !requestIDPattern.MatchString(id) {
		return EnrollReply{}, Refusal("BAD_REQUEST")
	}
	var out EnrollReply
	err := c.enrollTx(ctx, func(tx *sql.Tx, now uint64) error {
		e, err := c.openEnrollment(ctx, tx, now, id, code)
		if err != nil {
			return err
		}
		var s webauthn.SessionData
		if err := json.Unmarshal([]byte(e.session), &s); err != nil {
			return err
		}
		u, _, err := c.user(ctx, tx, e.approver)
		if err != nil {
			return err
		}
		opts, err := c.creationOptions(u, s)
		if err != nil {
			return err
		}
		out = EnrollReply{EnrollmentID: id, ApproverID: e.approver, State: "pending", Options: opts, ExpiresAt: e.expires}
		return nil
	})
	return out, err
}

// creationOptions rebuilds the options of a pending enrollment from its
// stored session: the same challenge and user handle.
func (c *ApprovalCore) creationOptions(u *rpUser, s webauthn.SessionData) (json.RawMessage, error) {
	ch, err := base64.RawURLEncoding.DecodeString(s.Challenge)
	if err != nil {
		return nil, err
	}
	var exclude []protocol.CredentialDescriptor
	for _, cr := range u.creds {
		exclude = append(exclude, cr.Descriptor())
	}
	opts := protocol.CredentialCreation{Response: protocol.PublicKeyCredentialCreationOptions{
		RelyingParty: protocol.RelyingPartyEntity{ID: c.cfg.WebAuthn.RPID, CredentialEntity: protocol.CredentialEntity{Name: "agent-payment-authority"}},
		User:         protocol.UserEntity{ID: protocol.URLEncodedBase64(u.handle), DisplayName: u.id, CredentialEntity: protocol.CredentialEntity{Name: u.id}},
		Challenge:    ch, Parameters: []protocol.CredentialParameter{{Type: protocol.PublicKeyCredentialType, Algorithm: -7}},
		AuthenticatorSelection: protocol.AuthenticatorSelection{UserVerification: protocol.VerificationRequired, ResidentKey: protocol.ResidentKeyRequirementPreferred},
		CredentialExcludeList:  exclude, Attestation: protocol.PreferNoAttestation, Timeout: enrollTTL * 1000}}
	return json.Marshal(opts)
}

// EnrollFinish verifies the relayed attestation (W3C 7.1: type, challenge,
// origin, RP ID hash, UP and UV, ES256, fmt none, a new credential ID) and
// keeps the credential pending until the administrator confirms it.
func (c *ApprovalCore) EnrollFinish(ctx context.Context, id, code string, attestation json.RawMessage) (EnrollReply, error) {
	if !requestIDPattern.MatchString(id) || len(attestation) == 0 {
		return EnrollReply{}, Refusal("BAD_REQUEST")
	}
	var out EnrollReply
	err := c.enrollTx(ctx, func(tx *sql.Tx, now uint64) error {
		e, err := c.openEnrollment(ctx, tx, now, id, code)
		if err != nil {
			return err
		}
		var s webauthn.SessionData
		if err := json.Unmarshal([]byte(e.session), &s); err != nil {
			return err
		}
		u, _, err := c.user(ctx, tx, e.approver)
		if err != nil {
			return err
		}
		parsed, err := protocol.ParseCredentialCreationResponseBytes(attestation)
		if err != nil {
			return Refusal("ATTESTATION_INVALID")
		}
		s.UserVerification = protocol.VerificationRequired
		cred, err := c.rp.CreateCredential(u, s, parsed)
		switch {
		case err != nil:
			return Refusal("ATTESTATION_INVALID")
		case cred.AttestationFormat != "none": // D6
			return Refusal("ATTESTATION_FORMAT")
		case !cred.Flags.UserPresent || !cred.Flags.UserVerified:
			return Refusal("ATTESTATION_INVALID")
		case len(cred.ID) == 0 || len(cred.ID) > 1023:
			return Refusal("ATTESTATION_INVALID")
		}
		rec, err := json.Marshal(cred)
		if err != nil {
			return err
		}
		credID := b64(cred.ID)
		var n int
		if err := tx.QueryRowContext(ctx, `SELECT count(*) FROM credentials WHERE credential_id = ?`, credID).Scan(&n); err != nil {
			return err
		} else if n > 0 {
			return Refusal("CREDENTIAL_EXISTS")
		}
		if _, err := tx.ExecContext(ctx, `INSERT INTO credentials (credential_id, approver_id, record, state, created_at) VALUES (?, ?, ?, 'pending', ?)`,
			credID, e.approver, string(rec), now); err != nil {
			return err
		}
		if _, err := tx.ExecContext(ctx, `UPDATE enrollments SET state = 'finished', credential_id = ? WHERE enrollment_id = ?`, credID, id); err != nil {
			return err
		}
		out = EnrollReply{EnrollmentID: id, ApproverID: e.approver, State: "finished", Fingerprint: Fingerprint(cred.ID),
			Flags: credFlags(cred), AAGUID: hex.EncodeToString(cred.Authenticator.AAGUID)}
		return nil
	})
	return out, err
}

func credFlags(cr *webauthn.Credential) string {
	b := func(on bool) string {
		if on {
			return "1"
		}
		return "0"
	}
	f := cr.Flags
	return "UP=" + b(f.UserPresent) + " UV=" + b(f.UserVerified) + " BE=" + b(f.BackupEligible) + " BS=" + b(f.BackupState)
}

// EnrollPending is what the administrator's confirm shows before asking.
func (c *ApprovalCore) EnrollPending(ctx context.Context, id string) (EnrollReply, error) {
	var e EnrollReply
	var credID sql.NullString
	err := c.db.QueryRowContext(ctx, `SELECT enrollment_id, approver_id, state, credential_id FROM enrollments WHERE enrollment_id = ?`, id).
		Scan(&e.EnrollmentID, &e.ApproverID, &e.State, &credID)
	if errors.Is(err, sql.ErrNoRows) {
		return e, Refusal("ENROLLMENT_UNKNOWN")
	} else if err != nil {
		return e, err
	}
	if e.State != "finished" {
		return e, Refusal("ENROLLMENT_NOT_FINISHED")
	}
	var rec string
	if err := c.db.QueryRowContext(ctx, `SELECT record FROM credentials WHERE credential_id = ?`, credID.String).Scan(&rec); err != nil {
		return e, err
	}
	var cr webauthn.Credential
	if err := json.Unmarshal([]byte(rec), &cr); err != nil {
		return e, err
	}
	e.Fingerprint, e.Flags, e.AAGUID = Fingerprint(cr.ID), credFlags(&cr), hex.EncodeToString(cr.Authenticator.AAGUID)
	return e, nil
}

// EnrollConfirm makes a finished enrollment's credential active (the
// administrator compared the fingerprints and answered yes).
func (c *ApprovalCore) EnrollConfirm(ctx context.Context, id string) error {
	return c.inTx(ctx, func(tx *sql.Tx, now uint64) error {
		var credID string
		err := tx.QueryRowContext(ctx, `SELECT credential_id FROM enrollments WHERE enrollment_id = ? AND state = 'finished'`, id).Scan(&credID)
		if errors.Is(err, sql.ErrNoRows) {
			return Refusal("ENROLLMENT_NOT_FINISHED")
		} else if err != nil {
			return err
		}
		res, err := tx.ExecContext(ctx, `UPDATE credentials SET state = 'active', activated_at = ? WHERE credential_id = ? AND state = 'pending'`, now, credID)
		if err != nil {
			return err
		}
		if n, _ := res.RowsAffected(); n != 1 {
			return Refusal("CREDENTIAL_NOT_PENDING")
		}
		_, err = tx.ExecContext(ctx, `UPDATE enrollments SET state = 'confirmed' WHERE enrollment_id = ?`, id)
		return err
	})
}

// CredentialInfo is one line of approver list.
type CredentialInfo struct {
	ApproverID, Fingerprint, State, Flags, AAGUID string
	CreatedAt                                     uint64
}

// Credentials lists every credential, oldest first.
func (c *ApprovalCore) Credentials(ctx context.Context) ([]CredentialInfo, error) {
	rows, err := c.db.QueryContext(ctx, `SELECT approver_id, record, state, created_at FROM credentials ORDER BY created_at, credential_id`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []CredentialInfo
	for rows.Next() {
		var ci CredentialInfo
		var rec string
		if err := rows.Scan(&ci.ApproverID, &rec, &ci.State, &ci.CreatedAt); err != nil {
			return nil, err
		}
		var cr webauthn.Credential
		if err := json.Unmarshal([]byte(rec), &cr); err != nil {
			return nil, err
		}
		ci.Fingerprint, ci.Flags, ci.AAGUID = Fingerprint(cr.ID), credFlags(&cr), hex.EncodeToString(cr.Authenticator.AAGUID)
		out = append(out, ci)
	}
	return out, rows.Err()
}

// Revoke revokes the one credential whose fingerprint is fp. Challenges
// already issued for it are refused at verify (the state is read again).
func (c *ApprovalCore) Revoke(ctx context.Context, fp string) error {
	var match []string
	rows, err := c.db.QueryContext(ctx, `SELECT credential_id FROM credentials`)
	if err != nil {
		return err
	}
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			rows.Close()
			return err
		}
		if raw, err := base64.RawURLEncoding.DecodeString(id); err == nil && Fingerprint(raw) == fp {
			match = append(match, id)
		}
	}
	if err := errors.Join(rows.Err(), rows.Close()); err != nil {
		return err
	}
	if len(match) != 1 {
		return Refusal("CREDENTIAL_UNKNOWN")
	}
	return c.inTx(ctx, func(tx *sql.Tx, now uint64) error {
		_, err := tx.ExecContext(ctx, `UPDATE credentials SET state = 'revoked', state_reason = 'ADMIN_REVOKED', revoked_at = ? WHERE credential_id = ? AND state <> 'revoked'`, now, match[0])
		return err
	})
}
