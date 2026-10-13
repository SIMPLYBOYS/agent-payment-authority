package store

// A2's human approval in the Store (docs/commercial-human-approval-design.md
// 6.2, 7): the Host's attempts at a Signer-verified approval of one
// session, and the session's result once the Signer has verified it. The
// Signer is the authority: an attempt records the IDs the Host made before
// each call (request, verify request, close request) and what the Signer
// answered; a webauthn grant is the mirror of the Signer's decision. A
// simulated grant can never become a webauthn one (the CHECK needs the
// Signer's approval ID).

import (
	"database/sql"
	"encoding/json"
	"errors"
	"slices"

	"github.com/SIMPLYBOYS/agent-payment-authority/internal/contracts"
	"github.com/SIMPLYBOYS/agent-payment-authority/internal/paycontract"
)

// schema5 is schema 5's addition: grants take method webauthn with the
// Signer's approval ID (the table is rebuilt: SQLite cannot change a
// CHECK), and approval_attempts. At most one attempt of a session is open:
// requested, challenged, verifying, unknown, or refused while its
// challenge may still be issued.
const schema5 = `
CREATE TABLE grants_v5 (
  jti                  TEXT PRIMARY KEY,
  approval_session_id  TEXT NOT NULL UNIQUE REFERENCES approval_sessions,
  intent_hash          TEXT NOT NULL,
  authorization_method TEXT NOT NULL CHECK (authorization_method IN ('simulated', 'webauthn')),
  body                 TEXT NOT NULL,
  issued_at            INTEGER NOT NULL,
  signer_approval_id   TEXT UNIQUE,
  CHECK ((authorization_method = 'webauthn') = (signer_approval_id IS NOT NULL))
) STRICT;
INSERT INTO grants_v5 (jti, approval_session_id, intent_hash, authorization_method, body, issued_at)
  SELECT jti, approval_session_id, intent_hash, authorization_method, body, issued_at FROM grants;
DROP TABLE grants;
ALTER TABLE grants_v5 RENAME TO grants;
CREATE TABLE approval_attempts (
  request_id          TEXT PRIMARY KEY,
  approval_session_id TEXT NOT NULL REFERENCES approval_sessions,
  decision            TEXT NOT NULL CHECK (decision IN ('approve', 'deny')),
  challenge_id        TEXT UNIQUE,
  verify_request_id   TEXT UNIQUE,
  close_request_id    TEXT UNIQUE,
  signer_approval_id  TEXT UNIQUE,
  state               TEXT NOT NULL CHECK (state IN ('requested', 'challenged', 'verifying', 'verified', 'refused', 'unknown', 'lost', 'expired', 'closed')),
  error_code          TEXT,
  requested_at        INTEGER NOT NULL,
  updated_at          INTEGER NOT NULL,
  CHECK ((state = 'verified') = (signer_approval_id IS NOT NULL)),
  CHECK (state <> 'challenged' OR challenge_id IS NOT NULL),
  CHECK (state <> 'verifying' OR verify_request_id IS NOT NULL),
  CHECK (state <> 'lost' OR close_request_id IS NOT NULL),
  CHECK (state <> 'refused' OR error_code IS NOT NULL)
) STRICT;
CREATE UNIQUE INDEX one_open_attempt ON approval_attempts (approval_session_id)
  WHERE state IN ('requested', 'challenged', 'verifying', 'unknown') OR (state = 'refused' AND challenge_id IS NOT NULL);
`

// ApprovalAttempt is one approval_attempts row.
type ApprovalAttempt struct {
	RequestID        string `json:"request_id"`
	SessionID        string `json:"approval_session_id"`
	Decision         string `json:"decision"`
	ChallengeID      string `json:"challenge_id,omitempty"`
	VerifyRequestID  string `json:"verify_request_id,omitempty"`
	CloseRequestID   string `json:"close_request_id,omitempty"`
	SignerApprovalID string `json:"signer_approval_id,omitempty"`
	State            string `json:"state"`
	ErrorCode        string `json:"error_code,omitempty"`
	RequestedAt      uint64 `json:"requested_at"`
	UpdatedAt        uint64 `json:"updated_at"`
}

// Open says whether the attempt still holds its session: one whose
// challenge the Signer may still accept.
func (a ApprovalAttempt) Open() bool {
	switch a.State {
	case "requested", "challenged", "verifying", "unknown":
		return true
	case "refused":
		return a.ChallengeID != ""
	}
	return false
}

const attemptCols = `request_id, approval_session_id, decision, coalesce(challenge_id, ''), coalesce(verify_request_id, ''), coalesce(close_request_id, ''),
  coalesce(signer_approval_id, ''), state, coalesce(error_code, ''), requested_at, updated_at`

func scanAttempt(row interface{ Scan(...any) error }) (ApprovalAttempt, bool, error) {
	var a ApprovalAttempt
	err := row.Scan(&a.RequestID, &a.SessionID, &a.Decision, &a.ChallengeID, &a.VerifyRequestID, &a.CloseRequestID, &a.SignerApprovalID,
		&a.State, &a.ErrorCode, &a.RequestedAt, &a.UpdatedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return a, false, nil
	}
	return a, err == nil, err
}

type rowQuerier interface {
	QueryRow(string, ...any) *sql.Row
}

func attemptBy(q rowQuerier, col, v string) (ApprovalAttempt, bool, error) {
	return scanAttempt(q.QueryRow(`SELECT `+attemptCols+` FROM approval_attempts WHERE `+col+` = ?`, v))
}

// latestAttempt is the session's newest attempt.
func latestAttempt(q rowQuerier, session string) (ApprovalAttempt, bool, error) {
	return scanAttempt(q.QueryRow(`SELECT `+attemptCols+` FROM approval_attempts WHERE approval_session_id = ? ORDER BY requested_at DESC, rowid DESC LIMIT 1`, session))
}

// AttemptRequest starts or resumes an attempt: the viewer's consumer, the
// session, the Host page's request ID and the decision.
type AttemptRequest struct {
	ConsumerID, SessionID, RequestID, Decision string
}

// AttemptStart is what the Host sends the Signer for an attempt: the
// stored v1 order (canonical), the session's deadline and intent. Kind is
// new, shared (the session's open attempt of the same decision: its
// request ID is the authoritative one) or replay (this request ID's own).
// SessionState is the session's state when read: an open attempt of a
// session no longer pending is closed by the Host, never driven (review R3).
type AttemptStart struct {
	Attempt      ApprovalAttempt `json:"attempt"`
	Kind         string          `json:"kind"`
	SessionState string          `json:"session_state"`
	Order        json.RawMessage `json:"-"`
	Deadline     uint64          `json:"deadline"`
	IntentHash   string          `json:"intent_hash"`
	TaskID       string          `json:"task_id"`
}

// sessionFor reads a session of consumer (OWNERSHIP_MISMATCH for another's
// or an unknown one) with its task, in t's transaction.
func (s *Store) sessionFor(tx *sql.Tx, now uint64, consumer, session string) (*taskTx, *purchaseRow, error) {
	ss, err := scanSession(tx.QueryRow(`SELECT `+sessionCols+` FROM approval_sessions WHERE approval_session_id = ?`, session))
	if errors.Is(err, sql.ErrNoRows) || err == nil && ss.ConsumerID != consumer {
		return nil, nil, refuse("OWNERSHIP_MISMATCH", "no such approval session for this consumer")
	} else if err != nil {
		return nil, nil, err
	}
	t, err := s.openTask(tx, now, ss.TaskID)
	if err != nil {
		return nil, nil, err
	}
	p := t.purchases[ss.PO]
	if p == nil || p.Session == nil || !t.pay() {
		return nil, nil, &CorruptError{Check: "approval", TaskID: ss.TaskID, Detail: "session without its apa.pay.v1 purchase"}
	}
	return t, p, nil
}

// BeginApprovalAttempt is design 6.3's challenge request on the Host side,
// before the Signer is called: a resend of a request ID answers its own
// attempt; the session's open attempt of the same decision is shared; an
// open attempt of the other decision is DECISION_CONFLICT (the result
// carries it); otherwise, for a session still pending, a new attempt is
// written. Deadlines reached are recorded first.
func (s *Store) BeginApprovalAttempt(req AttemptRequest) (AttemptStart, error) {
	var out AttemptStart
	if !requestIDPattern.MatchString(req.RequestID) || (req.Decision != "approve" && req.Decision != "deny") {
		return out, refuse("INVALID_APPROVAL", "request_id or decision")
	}
	var conflict error
	err := s.write(func(tx *sql.Tx, now uint64) error {
		t, p, err := s.sessionFor(tx, now, req.ConsumerID, req.SessionID)
		if err != nil {
			return err
		}
		out.Order, out.Deadline, out.IntentHash, out.TaskID = json.RawMessage(p.Body), p.Session.ExpiresAt, p.Session.IntentHash, t.row.TaskID
		out.SessionState = p.Session.State
		if a, found, err := attemptBy(tx, "request_id", req.RequestID); err != nil {
			return err
		} else if found {
			if a.SessionID != req.SessionID || a.Decision != req.Decision {
				return refuse("IDEMPOTENCY_CONFLICT", "this request ID is another attempt's")
			}
			out.Attempt, out.Kind = a, "replay"
			return nil
		}
		if err := t.expire(); err != nil {
			return err
		}
		out.SessionState = p.Session.State
		if cur, found, err := latestAttempt(tx, req.SessionID); err != nil {
			return err
		} else if found && cur.Open() {
			out.Attempt = cur
			switch {
			case p.Session.State != "pending": // the Host closes it (review R3); nothing is shared
				conflict = refuse("SESSION_CLOSED", "the session is "+p.Session.State)
			case cur.Decision != req.Decision:
				conflict = refuse("DECISION_CONFLICT", "the session's open attempt has the other decision")
			default:
				out.Kind = "shared"
			}
			return t.finish()
		}
		if p.Session.State != "pending" {
			conflict = refuse("SESSION_CLOSED", "the session is "+p.Session.State)
			return t.finish() // the deadline just recorded, if any
		}
		if _, err := tx.Exec(`INSERT INTO approval_attempts (request_id, approval_session_id, decision, state, requested_at, updated_at) VALUES (?, ?, ?, 'requested', ?, ?)`,
			req.RequestID, req.SessionID, req.Decision, now, now); err != nil {
			if uniqueViolation(err) {
				return refuse("IDEMPOTENCY_CONFLICT", "the session already has an open attempt")
			}
			return err
		}
		out.Attempt = ApprovalAttempt{RequestID: req.RequestID, SessionID: req.SessionID, Decision: req.Decision, State: "requested", RequestedAt: now, UpdatedAt: now}
		out.Kind = "new"
		return t.finish()
	})
	if err == nil {
		err = conflict
	}
	return out, err
}

// AttemptUpdate moves an attempt of consumer from one of From to State,
// setting the IDs given (non-empty). A challenge ID, once set, never
// changes.
type AttemptUpdate struct {
	ConsumerID, RequestID                                 string
	From                                                  []string
	State                                                 string
	ChallengeID, VerifyRequestID, CloseRequestID, ErrCode string
}

func (s *Store) UpdateApprovalAttempt(u AttemptUpdate) (ApprovalAttempt, error) {
	var out ApprovalAttempt
	err := s.write(func(tx *sql.Tx, now uint64) error {
		a, err := s.ownAttempt(tx, u.ConsumerID, u.RequestID)
		if err != nil {
			return err
		}
		if !slices.Contains(u.From, a.State) {
			return refuse("ATTEMPT_STATE", "the attempt is "+a.State)
		}
		if u.ChallengeID != "" && a.ChallengeID != "" && a.ChallengeID != u.ChallengeID {
			return refuse("ATTEMPT_STATE", "the attempt has another challenge")
		}
		set := func(cur, v string) any {
			if v != "" {
				return v
			}
			if cur == "" {
				return nil
			}
			return cur
		}
		code := any(nil) // a refused attempt keeps its code unless a new one is given
		if u.ErrCode != "" {
			code = u.ErrCode
		} else if u.State == "refused" && a.ErrorCode != "" {
			code = a.ErrorCode
		}
		if _, err := tx.Exec(`UPDATE approval_attempts SET state = ?, challenge_id = ?, verify_request_id = ?, close_request_id = ?, error_code = ?, updated_at = ?
			WHERE request_id = ? AND state = ?`, u.State, set(a.ChallengeID, u.ChallengeID), set(a.VerifyRequestID, u.VerifyRequestID),
			set(a.CloseRequestID, u.CloseRequestID), code, now, u.RequestID, a.State); err != nil {
			if uniqueViolation(err) {
				return refuse("IDEMPOTENCY_CONFLICT", "an ID of this update is another attempt's")
			}
			return err
		}
		out, _, err = attemptBy(tx, "request_id", u.RequestID)
		return err
	})
	return out, err
}

// ownAttempt is consumer's attempt (OWNERSHIP_MISMATCH otherwise).
func (s *Store) ownAttempt(tx *sql.Tx, consumer, requestID string) (ApprovalAttempt, error) {
	a, found, err := attemptBy(tx, "request_id", requestID)
	if err != nil {
		return a, err
	}
	var owner string
	if found {
		if err := tx.QueryRow(`SELECT consumer_id FROM approval_sessions WHERE approval_session_id = ?`, a.SessionID).Scan(&owner); err != nil {
			return a, err
		}
	}
	if !found || owner != consumer {
		return a, refuse("OWNERSHIP_MISMATCH", "no such attempt for this consumer")
	}
	return a, nil
}

// ApprovalAttemptOf reads consumer's attempt, read only.
func (s *Store) ApprovalAttemptOf(consumer, requestID string) (ApprovalAttempt, AttemptStart, error) {
	tx, err := s.r.Begin()
	if err != nil {
		return ApprovalAttempt{}, AttemptStart{}, err
	}
	defer tx.Rollback()
	a, found, err := attemptBy(tx, "request_id", requestID)
	if err != nil {
		return a, AttemptStart{}, &ReadError{Check: "approval", Err: err}
	}
	var st AttemptStart
	if found {
		ss, err := scanSession(tx.QueryRow(`SELECT `+sessionCols+` FROM approval_sessions WHERE approval_session_id = ?`, a.SessionID))
		if err != nil {
			return a, st, &ReadError{Check: "approval", Err: err}
		}
		if ss.ConsumerID == consumer {
			var body string
			if err := tx.QueryRow(`SELECT body FROM purchases WHERE task_id = ? AND purchase_order_id = ?`, ss.TaskID, ss.PO).Scan(&body); err != nil {
				return a, st, &ReadError{Check: "approval", Err: err}
			}
			st = AttemptStart{Attempt: a, Order: json.RawMessage(body), Deadline: ss.ExpiresAt, IntentHash: ss.IntentHash, TaskID: ss.TaskID, SessionState: ss.State}
			return a, st, nil
		}
	}
	return a, st, refuse("OWNERSHIP_MISMATCH", "no such attempt for this consumer")
}

// WebAuthnResult is a decision the Signer verified for an attempt: its
// decision, approval ID, approver, the credential's reference, the intent
// it verified and the approval's expiry (Signer's).
type WebAuthnResult struct {
	ConsumerID, RequestID                                             string
	Decision, SignerApprovalID, ApproverID, CredentialRef, IntentHash string
	ExpiresAt                                                         uint64
}

// ResolveWebAuthn writes the Signer's verified decision for an attempt, in
// one write transaction: the attempt becomes verified with the Signer's
// approval ID; then, only if the decision is the attempt's, the intent is
// the session's, the session is still pending (deadlines recorded first)
// and the quote still offered, the session closes approved (with the
// webauthn grant and GRANT_ISSUED) or denied. Outcome says what happened:
// approved | denied | closed (the session was closed first: the caller
// abandons the Signer's approval) | superseded | mismatch (the Signer's
// answer is not this attempt's: nothing of the session changes).
func (s *Store) ResolveWebAuthn(r WebAuthnResult) (ApprovalResult, error) {
	var out ApprovalResult
	if !paycontract.ValidID(r.SignerApprovalID) || !paycontract.ValidID(r.ApproverID) || (r.Decision != "approve" && r.Decision != "deny") {
		return out, refuse("INVALID_APPROVAL", "the Signer's result")
	}
	err := s.write(func(tx *sql.Tx, now uint64) error {
		a, err := s.ownAttempt(tx, r.ConsumerID, r.RequestID)
		if err != nil {
			return err
		}
		t, p, err := s.sessionFor(tx, now, r.ConsumerID, a.SessionID)
		if err != nil {
			return err
		}
		out.TaskID = t.row.TaskID
		if a.State == "verified" {
			if a.SignerApprovalID != r.SignerApprovalID {
				return refuse("IDEMPOTENCY_CONFLICT", "the attempt was verified with another Signer approval")
			}
			out.Replay, out.Purchase = true, t.view(p)
			out.Outcome = "closed"
			if p.Session.ResolveRequestID.String == a.RequestID {
				out.Outcome = p.Session.State
			}
			out.Grant, err = grantOf(p.Session)
			return err
		}
		if !slices.Contains([]string{"requested", "challenged", "verifying", "unknown", "refused"}, a.State) {
			return refuse("ATTEMPT_STATE", "the attempt is "+a.State)
		}
		if r.Decision != a.Decision || r.IntentHash != p.Session.IntentHash {
			out.Outcome, out.Purchase = "mismatch", t.view(p)
			_, err := tx.Exec(`UPDATE approval_attempts SET state = 'refused', error_code = 'DECISION_MISMATCH', updated_at = ? WHERE request_id = ?`, now, a.RequestID)
			return err
		}
		if _, err := tx.Exec(`UPDATE approval_attempts SET state = 'verified', signer_approval_id = ?, error_code = NULL, updated_at = ? WHERE request_id = ?`,
			r.SignerApprovalID, now, a.RequestID); err != nil {
			if uniqueViolation(err) {
				return refuse("IDEMPOTENCY_CONFLICT", "this Signer approval is another attempt's")
			}
			return err
		}
		if err := t.expire(); err != nil {
			return err
		}
		by := &resolver{a.RequestID, "webauthn:" + r.ApproverID, "webauthn", r.SignerApprovalID}
		switch {
		case p.Session.State != "pending":
			out.Outcome = "closed"
		case !s.offered(p.V1.Intent.HTTPMethod, p.V1.Intent.ResourceURL, p.V1.Quote):
			if err := t.close(p, "APPROVAL_SUPERSEDED", "superseded", "QUOTE_CHANGED", nil); err != nil {
				return err
			}
			out.Outcome = "superseded"
		case r.Decision == "deny":
			if err := t.close(p, "APPROVAL_DENIED", "denied", "APPROVER_DENIED", by); err != nil {
				return err
			}
			out.Outcome = "denied"
		case min(r.ExpiresAt, p.V1.PaymentBinding.ValidBefore) <= now:
			out.Outcome = "closed" // the Signer's approval has already expired: nothing to record
		default:
			if err := t.close(p, "APPROVED", "approved", "WEBAUTHN_APPROVAL", by); err != nil {
				return err
			}
			g := paycontract.AuthorizationGrant{Version: t.row.ContractVersion, JTI: t.spec().GrantID(p.Session.ID), IntentHash: p.Session.IntentHash,
				ApprovalSessionID: p.Session.ID, Decision: "approve", AuthorizationMethod: "webauthn", Issuer: paycontract.Recorder, Audience: "signer",
				ApproverID: by.by, IssuedAt: now, ExpiresAt: min(r.ExpiresAt, p.V1.PaymentBinding.ValidBefore),
				SignerApprovalID: r.SignerApprovalID, CredentialRef: r.CredentialRef, AMR: []string{"webauthn"}}
			if err := g.Validate(); err != nil {
				return err
			}
			body, err := canonical(g)
			if err != nil {
				return err
			}
			if _, err := tx.Exec(`INSERT INTO grants (jti, approval_session_id, intent_hash, authorization_method, body, issued_at, signer_approval_id) VALUES (?, ?, ?, ?, ?, ?, ?)`,
				g.JTI, g.ApprovalSessionID, g.IntentHash, g.AuthorizationMethod, string(body), now, g.SignerApprovalID); uniqueViolation(err) {
				return refuse("IDEMPOTENCY_CONFLICT", "this session already has a grant")
			} else if err != nil {
				return err
			}
			if err := s.at("after-grant"); err != nil {
				return err
			}
			if err := t.emit("GRANT_ISSUED", p, contracts.EventPayload{ApprovalSessionID: p.Session.ID, GrantID: g.JTI, AuthorizationMethod: "webauthn",
				SignerApprovalID: g.SignerApprovalID}); err != nil {
				return err
			}
			p.Session.Grant = &GrantView{JTI: g.JTI, AuthorizationMethod: g.AuthorizationMethod, Audience: g.Audience, IssuedAt: g.IssuedAt, ExpiresAt: g.ExpiresAt}
			out.Outcome, out.Grant = "approved", &g
		}
		if err := t.finish(); err != nil {
			return err
		}
		out.Purchase = t.view(p)
		return nil
	})
	return out, err
}

// ApprovalOf is one approval item of consumer, read only; its latest
// attempt included.
func (s *Store) ApprovalOf(consumer, session string) (ApprovalItem, error) {
	tx, err := s.r.Begin()
	if err != nil {
		return ApprovalItem{}, err
	}
	defer tx.Rollback()
	var task string
	err = tx.QueryRow(`SELECT task_id FROM approval_sessions WHERE approval_session_id = ? AND consumer_id = ?`, session, consumer).Scan(&task)
	if errors.Is(err, sql.ErrNoRows) {
		return ApprovalItem{}, refuse("OWNERSHIP_MISMATCH", "no such approval session for this consumer")
	} else if err != nil {
		return ApprovalItem{}, &ReadError{Check: "approval", Err: err}
	}
	items, err := s.itemsOf(tx, task, consumer)
	if err != nil {
		return ApprovalItem{}, err
	}
	for _, it := range items {
		if it.Purchase.Approval != nil && it.Purchase.Approval.SessionID == session {
			return it, nil
		}
	}
	return ApprovalItem{}, &CorruptError{Check: "approval", TaskID: task, Detail: "session " + session + " not in its task"}
}

// attemptOf attaches a session's latest attempt to an item (schema 5).
func attemptOf(tx *sql.Tx, it *ApprovalItem) error {
	if it.Purchase.Approval == nil {
		return nil
	}
	a, found, err := latestAttempt(tx, it.Purchase.Approval.SessionID)
	if err != nil {
		return &ReadError{Check: "approval", Err: err}
	}
	if found {
		it.Attempt = &a
	}
	return nil
}

// verifyApprovals is schema 5's recovery check of approval_attempts and
// webauthn grants against the sessions: a session resolved by webauthn
// was resolved by one verified attempt of the same decision; a webauthn
// grant names that attempt's Signer approval; a simulated grant names
// none and its session was not resolved by webauthn.
func verifyApprovals(tx *sql.Tx) error {
	bad := func(why string) error { return &CorruptError{Check: "approval", Detail: why} }
	for _, c := range []struct{ q, why string }{
		{`SELECT count(*) FROM approval_sessions s WHERE s.resolved_by LIKE 'webauthn:%' AND NOT EXISTS (SELECT 1 FROM approval_attempts a
		  WHERE a.request_id = s.resolve_request_id AND a.approval_session_id = s.approval_session_id AND a.state = 'verified'
		  AND ((a.decision = 'approve' AND s.state = 'approved') OR (a.decision = 'deny' AND s.state = 'denied')))`,
			"a session resolved by webauthn has no verified attempt of its decision"},
		{`SELECT count(*) FROM grants g JOIN approval_sessions s USING (approval_session_id) WHERE g.authorization_method = 'webauthn' AND (s.resolved_by NOT LIKE 'webauthn:%'
		  OR NOT EXISTS (SELECT 1 FROM approval_attempts a WHERE a.request_id = s.resolve_request_id AND a.signer_approval_id = g.signer_approval_id))`,
			"a webauthn grant does not mirror its session's verified attempt"},
		{`SELECT count(*) FROM grants g JOIN approval_sessions s USING (approval_session_id) WHERE g.authorization_method = 'simulated' AND s.resolved_by LIKE 'webauthn:%'`,
			"a simulated grant of a session resolved by webauthn"},
		{`SELECT count(*) FROM approval_attempts a JOIN approval_sessions s USING (approval_session_id) WHERE s.resolve_request_id = a.request_id AND a.state <> 'verified'`,
			"a session was resolved by an attempt that is not verified"},
	} {
		var n int
		if err := tx.QueryRow(c.q).Scan(&n); err != nil {
			return readErr("approval", "", err)
		}
		if n > 0 {
			return bad(c.why)
		}
	}
	rows, err := tx.Query(`SELECT body, coalesce(signer_approval_id, '') FROM grants`)
	if err != nil {
		return readErr("approval", "", err)
	}
	defer rows.Close()
	for rows.Next() {
		var body, signer string
		if err := rows.Scan(&body, &signer); err != nil {
			return readErr("approval", "", err)
		}
		var g paycontract.AuthorizationGrant
		if contracts.Decode([]byte(body), &g) != nil || g.SignerApprovalID != signer {
			return bad("a grant's signer_approval_id and body disagree")
		}
	}
	if err := drained(rows); err != nil {
		return readErr("approval", "", err)
	}
	return nil
}
