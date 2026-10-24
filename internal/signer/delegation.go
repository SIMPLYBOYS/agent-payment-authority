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
`

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
	u, allow, err := c.allowed(ctx, ap.ApproverID)
	if err != nil {
		return ApprovalReply{}, err
	}
	now := c.clock()
	expires := min(now+c.cfg.WebAuthn.ChallengeTTLSeconds, r.HostDeadline)
	if expires <= now {
		return ApprovalReply{}, Refusal("AUTHORIZATION_EXPIRED")
	}
	id, err := newID("chl-")
	if err != nil {
		return ApprovalReply{}, err
	}
	rnd := make([]byte, 32)
	if _, err := rand.Read(rnd); err != nil {
		return ApprovalReply{}, err
	}
	rec := challengeRecord{Version: 1, Kind: "delegation", ChallengeID: id, Random: b64(rnd), ApprovalSessionID: t.DelegationID, Decision: "approve",
		ConsumerID: t.ConsumerID, WalletID: w.WalletID, IntentHash: hash, JTI: delegationJTIOf(t.DelegationID),
		ApproverID: ap.ApproverID, ApproverVersion: ap.Version, ConfigHash: c.cfg.hash, ExpiresAt: expires}
	_, canon, err := digestOf(delegationTag, rec)
	if err != nil {
		return ApprovalReply{}, err
	}
	h := sha256.New()
	h.Write([]byte(delegationTag + "\x00"))
	h.Write(canon)
	value := h.Sum(nil)
	opts, session, err := c.rp.BeginLogin(u, webauthn.WithChallenge(value), webauthn.WithAllowedCredentials(allow),
		webauthn.WithUserVerification(protocol.VerificationRequired))
	if err != nil {
		return ApprovalReply{}, err
	}
	sess, _ := json.Marshal(session)
	optsJSON, _ := json.Marshal(opts)
	err = c.inTx(ctx, func(tx *sql.Tx, now uint64) error {
		var n int
		if err := tx.QueryRowContext(ctx, `SELECT count(*) FROM approval_challenges WHERE request_id = ?`, r.RequestID).Scan(&n); err != nil {
			return err
		} else if n > 0 {
			return Refusal("REQUEST_ID_RACE")
		}
		if err := tx.QueryRowContext(ctx, `SELECT count(*) FROM human_approvals WHERE jti = ?`, rec.JTI).Scan(&n); err != nil {
			return err
		} else if n > 0 {
			return Refusal("DELEGATION_CONFIRMED") // one confirmation per delegation, ever
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
			VALUES (?, ?, ?, ?, ?, ?, 'approve', ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, 'issued', ?)`,
			id, r.RequestID, fp, b64(value), string(canon), t.DelegationID, t.ConsumerID, w.WalletID, hash, rec.JTI,
			ap.ApproverID, ap.Version, c.cfg.hash, string(sess), string(optsJSON), string(terms), expires, now)
		return err
	})
	if errors.Is(err, Refusal("REQUEST_ID_RACE")) {
		return c.DelegationChallenge(ctx, r)
	}
	if err != nil {
		return ApprovalReply{}, err
	}
	return c.challengeQuery(ctx, "challenge_id", id)
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
	return c.approvalBy(ctx, "approval_id", id)
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
	err := q.QueryRowContext(ctx, `SELECT d.terms, d.consumer_id, d.wallet_id, d.approver_id, d.approver_version, d.allow_max, d.task_budget, d.valid_until, d.task_id,
		coalesce(cr.state, '') FROM delegation_confirmations d LEFT JOIN credentials cr ON cr.credential_id = d.credential_id WHERE d.confirmation_id = ?`, id).
		Scan(&termsJSON, &consumer, &wallet, &approverID, &version, &allowMax, &budget, &validUntil, &task, &cred)
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
	case now >= validUntil:
		return Refusal("DELEGATION_EXPIRED")
	case cred != "active": // a device revoked takes its confirmations with it
		return Refusal("CREDENTIAL_REVOKED")
	case ap == nil || ap.Version != version:
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
