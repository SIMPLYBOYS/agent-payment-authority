package signer

// The person's own confirmation of a delegation (gap 4;
// docs/commercial-gap4.md 2.1): a one-time WebAuthn challenge bound to the
// delegation's terms, verified here as a purchase's approval is (the same
// tables, checks and commit), under its own tag and jti so neither can ever
// stand for the other. No voice (G4). A verified one is kept as a
// delegation confirmation, which every payment then names (sign.go's
// checks, S3).

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"strings"

	"github.com/SIMPLYBOYS/agent-payment-authority/internal/contracts"
	"github.com/SIMPLYBOYS/agent-payment-authority/internal/paycontract"
	"github.com/go-webauthn/webauthn/protocol"
	"github.com/go-webauthn/webauthn/webauthn"
)

const delegationSchema = `
CREATE TABLE IF NOT EXISTS delegation_confirmations (
  confirmation_id  TEXT PRIMARY KEY REFERENCES human_approvals (approval_id),
  delegation_id    TEXT NOT NULL UNIQUE,
  terms_hash       TEXT NOT NULL,
  terms            TEXT NOT NULL,
  consumer_id      TEXT NOT NULL,
  wallet_id        TEXT NOT NULL,
  approver_id      TEXT NOT NULL,
  approver_version INTEGER NOT NULL,
  credential_id    TEXT NOT NULL,
  task_budget      INTEGER NOT NULL,
  allow_max        INTEGER NOT NULL,
  confirmed_at     INTEGER NOT NULL,
  valid_until      INTEGER NOT NULL,
  task_id          TEXT
) STRICT;
CREATE TABLE IF NOT EXISTS delegation_revocations (
  revocation_id TEXT PRIMARY KEY REFERENCES human_approvals (approval_id),
  delegation_id TEXT NOT NULL UNIQUE REFERENCES delegation_confirmations (delegation_id),
  revoked_at    INTEGER NOT NULL,
  receipt       TEXT
) STRICT;
`

// migrateReceipt adds the receipt column to a delegation_revocations table
// from before 7.1 (its rows keep none).
func migrateReceipt(ctx context.Context, db *sql.DB) error {
	var n int
	if err := db.QueryRowContext(ctx, `SELECT count(*) FROM pragma_table_info('delegation_revocations') WHERE name = 'receipt'`).Scan(&n); err != nil || n > 0 {
		return err
	}
	_, err := db.ExecContext(ctx, `ALTER TABLE delegation_revocations ADD COLUMN receipt TEXT`)
	return err
}

const (
	delegationTag    = "agent-payment-authority/pay/v1/delegation-challenge"
	delegationReq    = "agent-payment-authority/pay/v1/delegation-challenge-request"
	delegationVerify = "agent-payment-authority/pay/v1/delegation-verify-request"
	// delegationJTI names a delegation's one confirmation; a purchase's
	// is "grant-…", so a confirmation is never a purchase's approval.
	delegationJTI = "dconf-"
	// DelegationPayTTL: how long after the confirmation the Signer pays
	// under it (docs/commercial-gap4.md 4; valid_seconds only bounds the start).
	DelegationPayTTL = 2 * 3600
)

// DelegationChallengeRequest is POST /v1/approvals/delegations/challenge.
type DelegationChallengeRequest struct {
	RequestID    string          `json:"request_id"`
	Terms        json.RawMessage `json:"terms"`
	HostDeadline uint64          `json:"host_deadline"`
}

// DelegationVerifyRequest is POST /v1/approvals/delegations/verify.
type DelegationVerifyRequest struct {
	VerifyRequestID string          `json:"verify_request_id"`
	ChallengeID     string          `json:"challenge_id"`
	Terms           json.RawMessage `json:"terms"`
	Assertion       json.RawMessage `json:"assertion"`
}

// readTerms decodes and checks a delegation's terms, and hashes them.
func readTerms(raw json.RawMessage) (*paycontract.DelegationTerms, string, error) {
	var t paycontract.DelegationTerms
	if err := contracts.Decode(raw, &t); err != nil {
		return nil, "", Refusal("TERMS_INVALID")
	}
	h, err := t.Hash()
	if err != nil {
		return nil, "", Refusal("TERMS_INVALID")
	}
	return &t, h, nil
}

func delegationJTIOf(delegationID string) string {
	s := sha256.Sum256([]byte(delegationID))
	return delegationJTI + hex.EncodeToString(s[:16])
}

// DelegationChallenge is the challenge of a delegation's confirmation: one
// per Host request ID (a resend answers the same one), one verified
// confirmation per delegation, ever.
func (c *ApprovalCore) DelegationChallenge(ctx context.Context, r DelegationChallengeRequest) (ApprovalReply, error) {
	if !requestIDPattern.MatchString(r.RequestID) || len(r.Terms) == 0 || r.HostDeadline == 0 {
		return ApprovalReply{}, Refusal("BAD_REQUEST")
	}
	terms, err := contracts.Canonical(r.Terms)
	if err != nil {
		return ApprovalReply{}, Refusal("TERMS_INVALID")
	}
	fp, _, err := digestOf(delegationReq, map[string]any{"terms": json.RawMessage(terms), "host_deadline": r.HostDeadline})
	if err != nil {
		return ApprovalReply{}, Refusal("BAD_REQUEST")
	}
	if a, found, err := c.challengeBy(ctx, "request_id", r.RequestID); err != nil {
		return ApprovalReply{}, err
	} else if found {
		if a.fingerprint != fp {
			return ApprovalReply{}, Refusal("REQUEST_ID_CONFLICT")
		}
		return c.answerChallenge(ctx, a)
	}
	t, hash, err := readTerms(r.Terms)
	if err != nil {
		return ApprovalReply{}, err
	}
	w := c.cfg.wallet(t.WalletID)
	ap := c.cfg.approverOf(t.ConsumerID)
	switch {
	case w == nil:
		return ApprovalReply{}, Refusal("WALLET_NOT_REGISTERED")
	case w.ConsumerID != t.ConsumerID || w.ChainID != t.ChainID:
		return ApprovalReply{}, Refusal("CONSUMER_MISMATCH")
	case c.cfg.asset(t.ChainID, t.AssetContract) == nil:
		return ApprovalReply{}, Refusal("ASSET_UNKNOWN")
	case ap == nil || !containsString(ap.WalletIDs, w.WalletID):
		return ApprovalReply{}, Refusal("APPROVER_UNKNOWN")
	}
	rec := challengeRecord{Version: 1, Kind: "delegation", ApprovalSessionID: t.DelegationID, Decision: "approve",
		ConsumerID: t.ConsumerID, WalletID: w.WalletID, IntentHash: hash, JTI: delegationJTIOf(t.DelegationID),
		ApproverID: ap.ApproverID, ApproverVersion: ap.Version, ConfigHash: c.cfg.hash}
	id, err := c.issue(ctx, delegationTag, rec, r.RequestID, fp, r.HostDeadline, terms, "DELEGATION_CONFIRMED") // one confirmation per delegation, ever
	if errors.Is(err, Refusal("REQUEST_ID_RACE")) {
		return c.DelegationChallenge(ctx, r)
	}
	if err != nil {
		return ApprovalReply{}, err
	}
	return c.challengeQuery(ctx, "challenge_id", id)
}

// issue keeps a new challenge of one of a delegation's two ceremonies (its
// confirmation, its revocation) under tag: none while another of the same
// jti is open, none once one was verified (done). It returns its ID.
func (c *ApprovalCore) issue(ctx context.Context, tag string, rec challengeRecord, requestID, fp string, hostDeadline uint64, display []byte, done Refusal) (string, error) {
	u, allow, err := c.allowed(ctx, rec.ApproverID)
	if err != nil {
		return "", err
	}
	now := c.clock()
	expires := min(now+c.cfg.WebAuthn.ChallengeTTLSeconds, hostDeadline)
	if expires <= now {
		return "", Refusal("AUTHORIZATION_EXPIRED")
	}
	id, err := newID("chl-")
	if err != nil {
		return "", err
	}
	rnd := make([]byte, 32)
	if _, err := rand.Read(rnd); err != nil {
		return "", err
	}
	rec.ChallengeID, rec.Random, rec.ExpiresAt = id, b64(rnd), expires
	_, canon, err := digestOf(tag, rec)
	if err != nil {
		return "", err
	}
	h := sha256.New()
	h.Write([]byte(tag + "\x00"))
	h.Write(canon)
	value := h.Sum(nil)
	opts, session, err := c.rp.BeginLogin(u, webauthn.WithChallenge(value), webauthn.WithAllowedCredentials(allow),
		webauthn.WithUserVerification(protocol.VerificationRequired))
	if err != nil {
		return "", err
	}
	sess, _ := json.Marshal(session)
	optsJSON, _ := json.Marshal(opts)
	return id, c.inTx(ctx, func(tx *sql.Tx, now uint64) error {
		var n int
		if err := tx.QueryRowContext(ctx, `SELECT count(*) FROM approval_challenges WHERE request_id = ?`, requestID).Scan(&n); err != nil {
			return err
		} else if n > 0 {
			return Refusal("REQUEST_ID_RACE")
		}
		if err := tx.QueryRowContext(ctx, `SELECT count(*) FROM human_approvals WHERE jti = ?`, rec.JTI).Scan(&n); err != nil {
			return err
		} else if n > 0 {
			return done
		}
		if _, err := tx.ExecContext(ctx, `UPDATE approval_challenges SET state = 'expired', closed_at = ? WHERE jti = ? AND state = 'issued' AND ? >= expires_at`, now, rec.JTI, now); err != nil {
			return err
		}
		if err := tx.QueryRowContext(ctx, `SELECT count(*) FROM approval_challenges WHERE jti = ? AND state = 'issued'`, rec.JTI).Scan(&n); err != nil {
			return err
		} else if n > 0 {
			return Refusal("CHALLENGE_OPEN")
		}
		if now >= expires {
			return Refusal("AUTHORIZATION_EXPIRED")
		}
		_, err := tx.ExecContext(ctx, `INSERT INTO approval_challenges (challenge_id, request_id, fingerprint, challenge, record, approval_session_id, decision,
			consumer_id, wallet_id, intent_hash, jti, approver_id, approver_version, config_hash, session, options, display, expires_at, state, created_at)
			VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, 'issued', ?)`,
			id, requestID, fp, b64(value), string(canon), rec.ApprovalSessionID, rec.Decision, rec.ConsumerID, rec.WalletID, rec.IntentHash, rec.JTI,
			rec.ApproverID, rec.ApproverVersion, c.cfg.hash, string(sess), string(optsJSON), string(display), expires, now)
		return err
	})
}

// DelegationVerify verifies the person's assertion over a delegation's
// terms and keeps the confirmation, as Verify keeps a purchase's decision:
// a resend answers what was saved; refusals are saved and counted.
func (c *ApprovalCore) DelegationVerify(ctx context.Context, r DelegationVerifyRequest) (ApprovalReply, error) {
	if !requestIDPattern.MatchString(r.VerifyRequestID) || !requestIDPattern.MatchString(r.ChallengeID) || len(r.Terms) == 0 || len(r.Assertion) == 0 {
		return ApprovalReply{}, Refusal("BAD_REQUEST")
	}
	terms, err := contracts.Canonical(r.Terms)
	if err != nil {
		return ApprovalReply{}, Refusal("TERMS_INVALID")
	}
	ts := sha256.Sum256(terms)
	as := sha256.Sum256(r.Assertion)
	fp, _, err := digestOf(delegationVerify, map[string]string{"challenge_id": r.ChallengeID, "terms_sha256": hex.EncodeToString(ts[:]), "assertion_sha256": hex.EncodeToString(as[:])})
	if err != nil {
		return ApprovalReply{}, Refusal("BAD_REQUEST")
	}
	if rep, found, err := c.verifyAnswer(ctx, r.VerifyRequestID, fp); err != nil || found {
		return rep, err
	}
	vr := VerifyRequest{VerifyRequestID: r.VerifyRequestID, ChallengeID: r.ChallengeID, Assertion: r.Assertion} // what refuse and commit keep of it
	a, found, err := c.challengeBy(ctx, "challenge_id", r.ChallengeID)
	if err != nil {
		return ApprovalReply{}, err
	}
	if !found {
		return c.refuse(ctx, vr, fp, nil, "CHALLENGE_UNKNOWN", false)
	}
	t, hash, _ := readTerms(r.Terms) // bad terms match no challenge: INTENT_MISMATCH below
	if a.jti != delegationJTIOf(a.sessionID) || t != nil && a.sessionID != t.DelegationID {
		hash = "" // a purchase's challenge, or another delegation's: never confirmed here
	}
	code, cred, parsed := c.check(ctx, a, hash, r.Assertion)
	if code != "" {
		return c.refuse(ctx, vr, fp, &a, code, true)
	}
	if err := at("delegation-checked", a.id); err != nil {
		return ApprovalReply{}, err
	}
	budget, _ := t.TaskBudget.Units()
	allow, _ := t.AllowMax.Units()
	return c.commit(ctx, vr, fp, a, cred, parsed, nil, func(now uint64) uint64 { return now + DelegationPayTTL },
		func(tx *sql.Tx, now uint64, approvalID, credID string) error {
			_, err := tx.ExecContext(ctx, `INSERT INTO delegation_confirmations (confirmation_id, delegation_id, terms_hash, terms, consumer_id, wallet_id,
				approver_id, approver_version, credential_id, task_budget, allow_max, confirmed_at, valid_until)
				VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
				approvalID, t.DelegationID, hash, string(terms), t.ConsumerID, t.WalletID, a.approver, a.approverVersion, credID, budget, allow, now, now+DelegationPayTTL)
			return err
		})
}

// DelegationConfirmation answers GET /v1/approvals/delegations/{id}: the
// delegation's confirmation, if the person confirmed it.
func (c *ApprovalCore) DelegationConfirmation(ctx context.Context, delegationID string) (ApprovalReply, error) {
	if !paycontract.ValidID(delegationID) {
		return ApprovalReply{}, Refusal("BAD_REQUEST")
	}
	var id string
	err := c.db.QueryRowContext(ctx, `SELECT confirmation_id FROM delegation_confirmations WHERE delegation_id = ?`, delegationID).Scan(&id)
	if errors.Is(err, sql.ErrNoRows) {
		return ApprovalReply{}, Refusal("DELEGATION_NOT_CONFIRMED")
	} else if err != nil {
		return ApprovalReply{}, err
	}
	r, err := c.approvalBy(ctx, "approval_id", id)
	if err != nil {
		return r, err
	}
	var receipt sql.NullString
	err = c.db.QueryRowContext(ctx, `SELECT revocation_id, revoked_at, receipt FROM delegation_revocations WHERE delegation_id = ?`, delegationID).Scan(&r.RevocationID, &r.RevokedAt, &receipt)
	if errors.Is(err, sql.ErrNoRows) {
		return r, nil
	} else if err != nil {
		return r, err
	}
	r.Receipt, err = readReceipt(receipt)
	return r, err
}

func readReceipt(s sql.NullString) (*contracts.RevocationReceipt, error) {
	if !s.Valid {
		return nil, nil
	}
	var r contracts.RevocationReceipt
	return &r, json.Unmarshal([]byte(s.String), &r)
}

// The person taking a delegation back before it ends (docs/commercial-gap4.md
// 7): a second WebAuthn ceremony over the same terms, decision deny,
// under its own tag and jti. Once verified the Signer signs nothing more
// under that delegation, and an attempt it accepted and has not signed is
// abandoned in the same transaction. A Host can relay it or not, but never
// make one: only the person's device signs it.
const (
	revokeTag    = "agent-payment-authority/pay/v1/delegation-revoke-challenge"
	revokeReq    = "agent-payment-authority/pay/v1/delegation-revoke-request"
	revokeVerify = "agent-payment-authority/pay/v1/delegation-revoke-verify-request"
	revokeJTI    = "drev-"
)

// DelegationRevokeRequest is POST /v1/approvals/delegations/revoke/challenge.
type DelegationRevokeRequest struct {
	RequestID    string `json:"request_id"`
	DelegationID string `json:"delegation_id"`
	ConsumerID   string `json:"consumer_id"` // the Host's caller: another consumer's delegation is refused
	HostDeadline uint64 `json:"host_deadline"`
}

// DelegationRevokeVerifyRequest is POST /v1/approvals/delegations/revoke/verify.
type DelegationRevokeVerifyRequest struct {
	VerifyRequestID string          `json:"verify_request_id"`
	ChallengeID     string          `json:"challenge_id"`
	DelegationID    string          `json:"delegation_id"`
	Assertion       json.RawMessage `json:"assertion"`
}

func revokeJTIOf(delegationID string) string {
	s := sha256.Sum256([]byte(delegationID))
	return revokeJTI + hex.EncodeToString(s[:16])
}

type confirmedRow struct {
	consumer, wallet, approver, hash, terms string
	revoked                                 bool
}

func (c *ApprovalCore) confirmed(ctx context.Context, delegationID string) (confirmedRow, error) {
	var d confirmedRow
	err := c.db.QueryRowContext(ctx, `SELECT d.consumer_id, d.wallet_id, d.approver_id, d.terms_hash, d.terms, r.delegation_id IS NOT NULL
		FROM delegation_confirmations d LEFT JOIN delegation_revocations r ON r.delegation_id = d.delegation_id WHERE d.delegation_id = ?`, delegationID).
		Scan(&d.consumer, &d.wallet, &d.approver, &d.hash, &d.terms, &d.revoked)
	if errors.Is(err, sql.ErrNoRows) {
		return d, Refusal("DELEGATION_NOT_CONFIRMED")
	}
	return d, err
}

// DelegationRevokeChallenge is the challenge of a delegation's revocation,
// to the approver who confirmed it: one per Host request ID, one verified
// revocation per delegation, ever.
func (c *ApprovalCore) DelegationRevokeChallenge(ctx context.Context, r DelegationRevokeRequest) (ApprovalReply, error) {
	if !requestIDPattern.MatchString(r.RequestID) || !paycontract.ValidID(r.DelegationID) || !paycontract.ValidID(r.ConsumerID) || r.HostDeadline == 0 {
		return ApprovalReply{}, Refusal("BAD_REQUEST")
	}
	fp, _, err := digestOf(revokeReq, map[string]any{"delegation_id": r.DelegationID, "consumer_id": r.ConsumerID, "host_deadline": r.HostDeadline})
	if err != nil {
		return ApprovalReply{}, Refusal("BAD_REQUEST")
	}
	if a, found, err := c.challengeBy(ctx, "request_id", r.RequestID); err != nil {
		return ApprovalReply{}, err
	} else if found {
		if a.fingerprint != fp {
			return ApprovalReply{}, Refusal("REQUEST_ID_CONFLICT")
		}
		return c.answerChallenge(ctx, a)
	}
	d, err := c.confirmed(ctx, r.DelegationID)
	switch {
	case err != nil:
		return ApprovalReply{}, err
	case d.consumer != r.ConsumerID:
		return ApprovalReply{}, Refusal("CONSUMER_MISMATCH")
	case d.revoked:
		return ApprovalReply{}, Refusal("DELEGATION_REVOKED")
	}
	ap := c.cfg.approver(d.approver)
	if ap == nil {
		return ApprovalReply{}, Refusal("APPROVER_CHANGED") // and so it pays nothing already (confirmation)
	}
	rec := challengeRecord{Version: 1, Kind: "delegation-revoke", ApprovalSessionID: r.DelegationID, Decision: "deny",
		ConsumerID: d.consumer, WalletID: d.wallet, IntentHash: d.hash, JTI: revokeJTIOf(r.DelegationID),
		ApproverID: ap.ApproverID, ApproverVersion: ap.Version, ConfigHash: c.cfg.hash}
	id, err := c.issue(ctx, revokeTag, rec, r.RequestID, fp, r.HostDeadline, []byte(d.terms), "DELEGATION_REVOKED")
	if errors.Is(err, Refusal("REQUEST_ID_RACE")) {
		return c.DelegationRevokeChallenge(ctx, r)
	}
	if err != nil {
		return ApprovalReply{}, err
	}
	return c.challengeQuery(ctx, "challenge_id", id)
}

// DelegationRevokeVerify verifies the person's assertion over the
// revocation and keeps it, as DelegationVerify keeps a confirmation.
func (c *ApprovalCore) DelegationRevokeVerify(ctx context.Context, r DelegationRevokeVerifyRequest) (ApprovalReply, error) {
	if !requestIDPattern.MatchString(r.VerifyRequestID) || !requestIDPattern.MatchString(r.ChallengeID) || !paycontract.ValidID(r.DelegationID) || len(r.Assertion) == 0 {
		return ApprovalReply{}, Refusal("BAD_REQUEST")
	}
	as := sha256.Sum256(r.Assertion)
	fp, _, err := digestOf(revokeVerify, map[string]string{"challenge_id": r.ChallengeID, "delegation_id": r.DelegationID, "assertion_sha256": hex.EncodeToString(as[:])})
	if err != nil {
		return ApprovalReply{}, Refusal("BAD_REQUEST")
	}
	if rep, found, err := c.verifyAnswer(ctx, r.VerifyRequestID, fp); err != nil || found {
		if err != nil {
			return rep, err
		}
		return c.withReceipt(ctx, rep)
	}
	vr := VerifyRequest{VerifyRequestID: r.VerifyRequestID, ChallengeID: r.ChallengeID, Assertion: r.Assertion}
	a, found, err := c.challengeBy(ctx, "challenge_id", r.ChallengeID)
	if err != nil {
		return ApprovalReply{}, err
	}
	if !found {
		return c.refuse(ctx, vr, fp, nil, "CHALLENGE_UNKNOWN", false)
	}
	hash := "" // a confirmation's challenge, a purchase's, or another delegation's: never a revocation here
	if a.jti == revokeJTIOf(a.sessionID) && a.sessionID == r.DelegationID {
		d, err := c.confirmed(ctx, r.DelegationID)
		if err != nil {
			return ApprovalReply{}, err
		}
		hash = d.hash
	}
	code, cred, parsed := c.check(ctx, a, hash, r.Assertion)
	if code != "" {
		return c.refuse(ctx, vr, fp, &a, code, true)
	}
	rep, err := c.commit(ctx, vr, fp, a, cred, parsed, nil, func(now uint64) uint64 { return now },
		func(tx *sql.Tx, now uint64, approvalID, _ string) error {
			var pays int // an approvals-only database has no attempts
			if err := tx.QueryRowContext(ctx, `SELECT count(*) FROM sqlite_master WHERE type = 'table' AND name = 'attempts'`).Scan(&pays); err != nil {
				return err
			}
			var signed []string
			if pays > 0 {
				if _, err := tx.ExecContext(ctx, `UPDATE attempts SET state = 'abandoned', closed_at = ? WHERE state = 'accepted'
					AND confirmation_id = (SELECT confirmation_id FROM delegation_confirmations WHERE delegation_id = ?)`, now, a.sessionID); err != nil {
					return err
				}
				// What it had signed under the delegation, read in the same write transaction: nothing signs meanwhile.
				rows, err := tx.QueryContext(ctx, `SELECT intent_hash FROM attempts WHERE state = 'signed'
					AND confirmation_id = (SELECT confirmation_id FROM delegation_confirmations WHERE delegation_id = ?) ORDER BY intent_hash`, a.sessionID)
				if err != nil {
					return err
				}
				defer rows.Close()
				for rows.Next() {
					var h string
					if err := rows.Scan(&h); err != nil {
						return err
					}
					signed = append(signed, h)
				}
				if err := rows.Err(); err != nil {
					return err
				}
			}
			receipt, err := c.receipt(a.wallet, a.sessionID, approvalID, now, signed)
			if err != nil {
				return err
			}
			_, err = tx.ExecContext(ctx, `INSERT INTO delegation_revocations (revocation_id, delegation_id, revoked_at, receipt) VALUES (?, ?, ?, ?)`, approvalID, a.sessionID, now, receipt)
			return err
		})
	if err != nil {
		return rep, err
	}
	return c.withReceipt(ctx, rep)
}

// receipt is the revocation's receipt (7.1), as stored: signed with the
// wallet's key when this Signer holds it (a payment Signer), else none.
func (c *ApprovalCore) receipt(walletID, delegationID, revocationID string, now uint64, signed []string) (any, error) {
	w := c.cfg.wallet(walletID)
	if w == nil || c.keys[w.Address] == nil {
		return nil, nil
	}
	r := contracts.RevocationReceipt{DelegationID: delegationID, RevocationID: revocationID, RevokedAt: now, ChainID: w.ChainID, Wallet: strings.ToLower(w.Address),
		SignedIntents: append([]string{}, signed...)}
	d, err := paycontract.RevocationDigest(r)
	if err != nil {
		return nil, err
	}
	r.Signature = "0x" + hex.EncodeToString(signDigest(c.keys[w.Address], d))
	b, err := json.Marshal(r)
	return string(b), err
}

// withReceipt adds a verified revocation's receipt to its answer.
func (c *ApprovalCore) withReceipt(ctx context.Context, rep ApprovalReply) (ApprovalReply, error) {
	var receipt sql.NullString
	if err := c.db.QueryRowContext(ctx, `SELECT receipt FROM delegation_revocations WHERE revocation_id = ?`, rep.ApprovalID).Scan(&receipt); err != nil {
		return rep, err
	}
	var err error
	rep.Receipt, err = readReceipt(receipt)
	return rep, err
}

// confirms: this Signer takes the person's approvals (webauthn), so it pays
// only under a delegation the person confirmed (G3; docs/commercial-gap4.md
// 2.2). A payment-only Signer has no approver to confirm with.
func (c *Config) confirms() bool { return c.WebAuthn != nil && len(c.Approvers) > 0 }

// confirmation is a payment's check against the delegation the person
// confirmed (docs/commercial-gap4.md 2.2): the same consumer and wallet,
// still valid, the confirming device still active and the approver
// unchanged, the payee among the merchants and the asset the same, an
// Allow within allow_max, this task's (or none yet) and within the task
// budget with what was accepted under it. q is the DB, or the accepting
// transaction (which then binds the task).
func (c *Core) confirmation(ctx context.Context, q querier, au Authorization, po *paycontract.PurchaseOrder, amount, now uint64) error {
	id := au.DelegationConfirmationID
	if id == "" {
		return Refusal("DELEGATION_NOT_CONFIRMED")
	}
	var termsJSON, consumer, wallet, approverID, cred string
	var version, allowMax, budget, validUntil uint64
	var task sql.NullString
	var revoked bool
	// Only a confirmation still verified (review: an abandoned one is gone).
	err := q.QueryRowContext(ctx, `SELECT d.terms, d.consumer_id, d.wallet_id, d.approver_id, d.approver_version, d.allow_max, d.task_budget, d.valid_until, d.task_id,
		coalesce(cr.state, ''), rv.delegation_id IS NOT NULL FROM delegation_confirmations d JOIN human_approvals h ON h.approval_id = d.confirmation_id AND h.state = 'verified'
		LEFT JOIN credentials cr ON cr.credential_id = d.credential_id LEFT JOIN delegation_revocations rv ON rv.delegation_id = d.delegation_id WHERE d.confirmation_id = ?`, id).
		Scan(&termsJSON, &consumer, &wallet, &approverID, &version, &allowMax, &budget, &validUntil, &task, &cred, &revoked)
	if errors.Is(err, sql.ErrNoRows) {
		return Refusal("DELEGATION_NOT_CONFIRMED")
	} else if err != nil {
		return err
	}
	var t paycontract.DelegationTerms
	if err := json.Unmarshal([]byte(termsJSON), &t); err != nil {
		return err
	}
	i, b := po.Intent, po.PaymentBinding
	ap := c.cfg.approver(approverID)
	switch {
	case consumer != i.ConsumerID || wallet != i.WalletID:
		return Refusal("CONSUMER_MISMATCH")
	case revoked: // the person took it back (DelegationRevokeVerify)
		return Refusal("DELEGATION_REVOKED")
	case now >= validUntil:
		return Refusal("DELEGATION_EXPIRED")
	case cred != "active": // a device revoked takes its confirmations with it
		return Refusal("CREDENTIAL_REVOKED")
	case ap == nil || ap.Version != version || ap.ConsumerID != consumer || !containsString(ap.WalletIDs, wallet): // as humanApproval (review)
		return Refusal("APPROVER_CHANGED")
	case b.ChainID != t.ChainID || b.AssetContract != t.AssetContract:
		return Refusal("ASSET_NOT_ALLOWED")
	case !containsString(t.Merchants, b.PayTo):
		return Refusal("MERCHANT_NOT_ALLOWED")
	case au.Method == "delegation" && amount > allowMax: // an Allow; an Ask was approved by the person for this payment
		return Refusal("OVER_ALLOW_MAX")
	case task.Valid && task.String != i.TaskID:
		return Refusal("DELEGATION_OTHER_TASK")
	}
	var spent uint64
	if err := q.QueryRowContext(ctx, `SELECT coalesce(sum(amount), 0) FROM attempts WHERE confirmation_id = ? AND state IN ('accepted', 'signed')`, id).Scan(&spent); err != nil {
		return err
	}
	if spent+amount > budget {
		return Refusal("OVER_TASK_BUDGET")
	}
	return nil
}
