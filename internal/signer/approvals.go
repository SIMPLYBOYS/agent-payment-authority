package signer

// Human approval (A2; docs/commercial-human-approval-design.md 4-7): the
// Signer issues a one-time WebAuthn challenge bound to one purchase and
// approval session, verifies the assertion itself and keeps the verified
// decision. The Host only relays; its records mirror these. Nothing here
// signs, pays or opens the payment database: approvals.db is its own file
// (decision D8), opened by serve-approvals and the administrator commands.
//
// Every write is one SQLite transaction (BEGIN IMMEDIATE) with the time
// taken inside it. A challenge is consumed only by a conditional
// issued -> used update that competes with close on the same row.

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"database/sql"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"regexp"
	"strings"
	"time"

	"github.com/SIMPLYBOYS/agent-payment-authority/internal/contracts"
	"github.com/SIMPLYBOYS/agent-payment-authority/internal/paycontract"
	"github.com/go-webauthn/webauthn/protocol"
	"github.com/go-webauthn/webauthn/webauthn"
)

const approvalsSchema = `
CREATE TABLE IF NOT EXISTS approver_handles (
  approver_id TEXT PRIMARY KEY,
  user_handle TEXT NOT NULL UNIQUE
) STRICT;
CREATE TABLE IF NOT EXISTS enrollments (
  enrollment_id TEXT PRIMARY KEY,
  approver_id   TEXT NOT NULL,
  code_hash     TEXT NOT NULL,
  session       TEXT NOT NULL,
  expires_at    INTEGER NOT NULL,
  failures      INTEGER NOT NULL DEFAULT 0,
  state         TEXT NOT NULL CHECK (state IN ('pending', 'finished', 'confirmed', 'expired', 'closed')),
  credential_id TEXT UNIQUE,
  created_at    INTEGER NOT NULL,
  CHECK ((state IN ('finished', 'confirmed')) = (credential_id IS NOT NULL))
) STRICT;
CREATE UNIQUE INDEX IF NOT EXISTS one_pending_enrollment ON enrollments (approver_id) WHERE state IN ('pending', 'finished');
CREATE TABLE IF NOT EXISTS credentials (
  credential_id TEXT PRIMARY KEY,
  approver_id   TEXT NOT NULL,
  record        TEXT NOT NULL,
  state         TEXT NOT NULL CHECK (state IN ('pending', 'active', 'suspect', 'revoked')),
  state_reason  TEXT,
  created_at    INTEGER NOT NULL,
  activated_at  INTEGER,
  revoked_at    INTEGER
) STRICT;
CREATE TABLE IF NOT EXISTS approval_challenges (
  challenge_id        TEXT PRIMARY KEY,
  request_id          TEXT NOT NULL UNIQUE,
  fingerprint         TEXT NOT NULL,
  challenge           TEXT NOT NULL UNIQUE,
  record              TEXT NOT NULL,
  approval_session_id TEXT NOT NULL,
  decision            TEXT NOT NULL CHECK (decision IN ('approve', 'deny')),
  consumer_id         TEXT NOT NULL,
  wallet_id           TEXT NOT NULL,
  intent_hash         TEXT NOT NULL,
  jti                 TEXT NOT NULL,
  approver_id         TEXT NOT NULL,
  approver_version    INTEGER NOT NULL,
  config_hash         TEXT NOT NULL,
  session             TEXT NOT NULL,
  options             TEXT NOT NULL,
  display             TEXT NOT NULL,
  expires_at          INTEGER NOT NULL,
  failures            INTEGER NOT NULL DEFAULT 0,
  state               TEXT NOT NULL CHECK (state IN ('issued', 'used', 'expired', 'closed')),
  created_at          INTEGER NOT NULL,
  closed_at           INTEGER,
  CHECK ((state IN ('expired', 'closed')) = (closed_at IS NOT NULL))
) STRICT;
CREATE UNIQUE INDEX IF NOT EXISTS one_issued_per_jti ON approval_challenges (jti) WHERE state = 'issued';
CREATE TABLE IF NOT EXISTS human_approvals (
  approval_id         TEXT PRIMARY KEY,
  challenge_id        TEXT NOT NULL UNIQUE REFERENCES approval_challenges,
  verify_request_id   TEXT NOT NULL UNIQUE,
  jti                 TEXT NOT NULL UNIQUE,
  decision            TEXT NOT NULL CHECK (decision IN ('approve', 'deny')),
  approval_session_id TEXT NOT NULL,
  intent_hash         TEXT NOT NULL,
  approver_id         TEXT NOT NULL,
  approver_version    INTEGER NOT NULL,
  credential_id       TEXT NOT NULL,
  flags               TEXT NOT NULL,
  sign_count          INTEGER NOT NULL,
  client_data_hash    TEXT NOT NULL,
  authenticator_data  TEXT NOT NULL,
  signature_hash      TEXT NOT NULL,
  verified_at         INTEGER NOT NULL,
  expires_at          INTEGER NOT NULL,
  state               TEXT NOT NULL CHECK (state IN ('verified', 'consumed', 'expired', 'revoked', 'abandoned')),
  closed_at           INTEGER
) STRICT;
CREATE TABLE IF NOT EXISTS verify_requests (
  verify_request_id TEXT PRIMARY KEY,
  fingerprint       TEXT NOT NULL,
  challenge_id      TEXT NOT NULL,
  outcome           TEXT NOT NULL,
  approval_id       TEXT,
  received_at       INTEGER NOT NULL,
  CHECK ((outcome = 'verified') = (approval_id IS NOT NULL))
) STRICT;
`

// ApprovalCore is the Signer of serve-approvals: approver enrollment,
// challenges, verification and queries. It holds no key.
type ApprovalCore struct {
	cfg   *Config
	rp    *webauthn.WebAuthn
	db    *sql.DB
	clock Clock
}

// challengeFailures closes a challenge after this many refused assertions.
const challengeFailures = 3

// NewApprovalCore opens approvals.db at dbPath. cfg must have approvers
// and a webauthn section.
func NewApprovalCore(ctx context.Context, cfg *Config, dbPath string, clock Clock) (*ApprovalCore, error) {
	w := cfg.WebAuthn
	if w == nil || len(cfg.Approvers) == 0 {
		return nil, errors.New("config: serve-approvals needs approvers and a webauthn section")
	}
	ttl := time.Duration(w.ChallengeTTLSeconds) * time.Second
	rp, err := webauthn.New(&webauthn.Config{RPID: w.RPID, RPDisplayName: "agent-payment-authority", RPOrigins: w.Origins,
		AttestationPreference: protocol.PreferNoAttestation,
		AuthenticatorSelection: protocol.AuthenticatorSelection{UserVerification: protocol.VerificationRequired,
			ResidentKey: protocol.ResidentKeyRequirementPreferred},
		Timeouts: webauthn.TimeoutsConfig{Login: webauthn.TimeoutConfig{Timeout: ttl, TimeoutUVD: ttl},
			Registration: webauthn.TimeoutConfig{Timeout: enrollTTL * time.Second, TimeoutUVD: enrollTTL * time.Second}}})
	if err != nil {
		return nil, err
	}
	db, err := sql.Open("sqlite", dbPath+"?_pragma=busy_timeout(5000)&_pragma=journal_mode(WAL)&_pragma=synchronous(FULL)&_pragma=foreign_keys(1)&_txlock=immediate")
	if err != nil {
		return nil, err
	}
	db.SetMaxOpenConns(1)
	if _, err := db.ExecContext(ctx, approvalsSchema); err != nil {
		db.Close()
		return nil, err
	}
	return &ApprovalCore{cfg: cfg, rp: rp, db: db, clock: clock}, nil
}

func (c *ApprovalCore) Close() error { return c.db.Close() }

// Display is the purchase as the Signer reads it from the order and its
// own configuration, for the page to show (design 4.7): it is not a
// trusted display, and the Host page stays trusted to show it.
type Display struct {
	ConsumerID      string           `json:"consumer_id"`
	WalletID        string           `json:"wallet_id"`
	Payer           string           `json:"payer"`
	PayTo           string           `json:"pay_to"`
	Amount          contracts.Amount `json:"amount"`
	AssetContract   string           `json:"asset_contract"`
	AssetName       string           `json:"asset_name,omitempty"`
	ChainID         string           `json:"chain_id"`
	HTTPMethod      string           `json:"http_method"`
	ResourceURL     string           `json:"resource_url"`
	TaskID          string           `json:"task_id"`
	PurchaseOrderID string           `json:"purchase_order_id"`
	ValidBefore     uint64           `json:"valid_before"`
}

// ChallengeRequest is POST /v1/approvals/challenge. The order is the whole
// apa.pay.v1 purchase; HostDeadline is the Host's session deadline (it can
// only make the challenge shorter).
type ChallengeRequest struct {
	RequestID         string          `json:"request_id"`
	ApprovalSessionID string          `json:"approval_session_id"`
	Decision          string          `json:"decision"`
	Order             json.RawMessage `json:"order"`
	HostDeadline      uint64          `json:"host_deadline"`
}

// VerifyRequest is POST /v1/approvals/verify; the assertion is the
// browser's PublicKeyCredential JSON, relayed as it is.
type VerifyRequest struct {
	VerifyRequestID string          `json:"verify_request_id"`
	ChallengeID     string          `json:"challenge_id"`
	Order           json.RawMessage `json:"order"`
	Assertion       json.RawMessage `json:"assertion"`
}

// ApprovalReply is the Signer's answer on the approval routes. On the
// challenge routes State is the challenge's (issued | used | expired |
// closed); on the approval routes it is the verified decision's (verified
// | abandoned ...). Options are given while the challenge is issued.
type ApprovalReply struct {
	ChallengeID       string          `json:"challenge_id,omitempty"`
	RequestID         string          `json:"request_id,omitempty"`
	State             string          `json:"state,omitempty"`
	ApprovalSessionID string          `json:"approval_session_id,omitempty"`
	Decision          string          `json:"decision,omitempty"`
	ConsumerID        string          `json:"consumer_id,omitempty"`
	WalletID          string          `json:"wallet_id,omitempty"`
	IntentHash        string          `json:"intent_hash,omitempty"`
	ApproverID        string          `json:"approver_id,omitempty"`
	ExpiresAt         uint64          `json:"expires_at,omitempty"`
	Options           json.RawMessage `json:"options,omitempty"`
	Display           *Display        `json:"display,omitempty"`
	ApprovalID        string          `json:"approval_id,omitempty"`
	VerifyRequestID   string          `json:"verify_request_id,omitempty"`
	CredentialRef     string          `json:"credential_ref,omitempty"` // sha256 of the credential ID
	Flags             string          `json:"flags,omitempty"`
	VerifiedAt        uint64          `json:"verified_at,omitempty"`
	Payment           string          `json:"payment,omitempty"` // health
	Error             string          `json:"error,omitempty"`   // a fixed code
}

// rpUser is an approver as the WebAuthn library sees it.
type rpUser struct {
	id     string
	handle []byte
	creds  []webauthn.Credential
}

func (u *rpUser) WebAuthnID() []byte                         { return u.handle }
func (u *rpUser) WebAuthnName() string                       { return u.id }
func (u *rpUser) WebAuthnDisplayName() string                { return u.id }
func (u *rpUser) WebAuthnCredentials() []webauthn.Credential { return u.creds }

type querier interface {
	QueryRowContext(ctx context.Context, q string, args ...any) *sql.Row
	QueryContext(ctx context.Context, q string, args ...any) (*sql.Rows, error)
}

// user is the approver with every credential it has (any state: the
// state is checked apart, so a revoked one is refused by name).
func (c *ApprovalCore) user(ctx context.Context, q querier, approverID string) (*rpUser, map[string]string, error) {
	var handle string
	err := q.QueryRowContext(ctx, `SELECT user_handle FROM approver_handles WHERE approver_id = ?`, approverID).Scan(&handle)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil, Refusal("CREDENTIAL_UNKNOWN")
	} else if err != nil {
		return nil, nil, err
	}
	h, err := base64.RawURLEncoding.DecodeString(handle)
	if err != nil {
		return nil, nil, err
	}
	u := &rpUser{id: approverID, handle: h}
	states := map[string]string{}
	rows, err := q.QueryContext(ctx, `SELECT credential_id, record, state FROM credentials WHERE approver_id = ? ORDER BY created_at`, approverID)
	if err != nil {
		return nil, nil, err
	}
	defer rows.Close()
	for rows.Next() {
		var id, rec, st string
		if err := rows.Scan(&id, &rec, &st); err != nil {
			return nil, nil, err
		}
		var cr webauthn.Credential
		if err := json.Unmarshal([]byte(rec), &cr); err != nil {
			return nil, nil, err
		}
		u.creds = append(u.creds, cr)
		states[id] = st
	}
	return u, states, rows.Err()
}

func b64(b []byte) string { return base64.RawURLEncoding.EncodeToString(b) }

// CredentialRef is the reference a record keeps of a credential ID.
func CredentialRef(id []byte) string {
	s := sha256.Sum256(id)
	return "sha256:" + hex.EncodeToString(s[:])
}

func flagString(f protocol.AuthenticatorFlags) string {
	b := func(on bool) string {
		if on {
			return "1"
		}
		return "0"
	}
	return "UP=" + b(f.UserPresent()) + " UV=" + b(f.UserVerified()) + " BE=" + b(f.HasBackupEligible()) + " BS=" + b(f.HasBackupState())
}

func newID(prefix string) (string, error) {
	b := make([]byte, 16)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return prefix + hex.EncodeToString(b), nil
}

// digestOf is SHA-256(tag ‖ 0x00 ‖ canonical(v)).
func digestOf(tag string, v any) (string, []byte, error) {
	raw, err := json.Marshal(v)
	if err != nil {
		return "", nil, err
	}
	canon, err := contracts.Canonical(raw)
	if err != nil {
		return "", nil, err
	}
	h := sha256.New()
	h.Write([]byte(tag + "\x00"))
	h.Write(canon)
	return "sha256:" + hex.EncodeToString(h.Sum(nil)), canon, nil
}

const (
	challengeTag = "agent-payment-authority/pay/v1/webauthn-challenge"
	challengeReq = "agent-payment-authority/pay/v1/approval-challenge-request"
	verifyReq    = "agent-payment-authority/pay/v1/approval-verify-request"
)

// order decodes and checks a whole pay-family order (its own version's
// rules) and recomputes its intent. The challenge tags below stay
// ".../pay/v1/..." for every version: the intent they carry is already
// domain-separated by version (docs/commercial-c2-design.md 2.2).
func readOrder(raw json.RawMessage) (*paycontract.PurchaseOrder, string, error) {
	var po paycontract.PurchaseOrder
	if err := contracts.Decode(raw, &po); err != nil || po.Validate() != nil {
		return nil, "", Refusal("ORDER_INVALID")
	}
	intent, err := po.IntentHash()
	if err != nil {
		return nil, "", Refusal("ORDER_INVALID")
	}
	return &po, intent, nil
}

// challengeRecord is what a challenge is the hash of (design 4.5).
type challengeRecord struct {
	Version           int    `json:"version"`
	ChallengeID       string `json:"challenge_id"`
	Random            string `json:"random"`
	ApprovalSessionID string `json:"approval_session_id"`
	Decision          string `json:"decision"`
	ConsumerID        string `json:"consumer_id"`
	WalletID          string `json:"wallet_id"`
	IntentHash        string `json:"intent_hash"`
	JTI               string `json:"jti"`
	ApproverID        string `json:"approver_id"`
	ApproverVersion   uint64 `json:"approver_version"`
	ConfigHash        string `json:"config_hash"`
	ExpiresAt         uint64 `json:"expires_at"`
}

var decisionPattern = regexp.MustCompile(`^(approve|deny)$`)

// Challenge is POST /v1/approvals/challenge: one challenge per Host
// request ID; a resend with the same content answers the same challenge.
func (c *ApprovalCore) Challenge(ctx context.Context, r ChallengeRequest) (ApprovalReply, error) {
	if !requestIDPattern.MatchString(r.RequestID) || !paycontract.ValidID(r.ApprovalSessionID) || !decisionPattern.MatchString(r.Decision) || len(r.Order) == 0 || r.HostDeadline == 0 {
		return ApprovalReply{}, Refusal("BAD_REQUEST")
	}
	order, err := contracts.Canonical(r.Order)
	if err != nil {
		return ApprovalReply{}, Refusal("ORDER_INVALID")
	}
	fp, _, err := digestOf(challengeReq, map[string]any{"approval_session_id": r.ApprovalSessionID, "decision": r.Decision,
		"order": json.RawMessage(order), "host_deadline": r.HostDeadline})
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
	po, intent, err := readOrder(r.Order)
	if err != nil {
		return ApprovalReply{}, err
	}
	i, b := po.Intent, po.PaymentBinding
	sp, _ := paycontract.SpecOf(po.Version) // readOrder accepted the version
	if r.ApprovalSessionID != sp.SessionID(i.TaskID, i.PurchaseOrderID) {
		return ApprovalReply{}, Refusal("SESSION_MISMATCH")
	}
	w := c.cfg.wallet(i.WalletID)
	ap := c.cfg.approverOf(i.ConsumerID)
	switch {
	case w == nil:
		return ApprovalReply{}, Refusal("WALLET_NOT_REGISTERED")
	case w.ConsumerID != i.ConsumerID || b.From != w.Address || b.ChainID != w.ChainID:
		return ApprovalReply{}, Refusal("CONSUMER_MISMATCH")
	case ap == nil || !containsString(ap.WalletIDs, w.WalletID):
		return ApprovalReply{}, Refusal("APPROVER_UNKNOWN")
	}
	u, states, err := c.user(ctx, c.db, ap.ApproverID)
	if err != nil {
		return ApprovalReply{}, err
	}
	var allow []protocol.CredentialDescriptor
	pending, suspect, revoked := false, false, false
	for _, cr := range u.creds {
		switch states[b64(cr.ID)] {
		case "active":
			allow = append(allow, cr.Descriptor())
		case "pending":
			pending = true
		case "suspect":
			suspect = true
		default:
			revoked = true
		}
	}
	switch {
	case len(allow) > 0:
	case pending:
		return ApprovalReply{}, Refusal("CREDENTIAL_PENDING")
	case suspect:
		return ApprovalReply{}, Refusal("CREDENTIAL_SUSPECT")
	case revoked:
		return ApprovalReply{}, Refusal("CREDENTIAL_REVOKED")
	default:
		return ApprovalReply{}, Refusal("CREDENTIAL_UNKNOWN")
	}
	now := c.clock()
	expires := min(now+c.cfg.WebAuthn.ChallengeTTLSeconds, b.ValidBefore, r.HostDeadline)
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
	rec := challengeRecord{Version: 1, ChallengeID: id, Random: b64(rnd), ApprovalSessionID: r.ApprovalSessionID, Decision: r.Decision,
		ConsumerID: i.ConsumerID, WalletID: w.WalletID, IntentHash: intent, JTI: sp.GrantID(r.ApprovalSessionID),
		ApproverID: ap.ApproverID, ApproverVersion: ap.Version, ConfigHash: c.cfg.hash, ExpiresAt: expires}
	_, canon, err := digestOf(challengeTag, rec)
	if err != nil {
		return ApprovalReply{}, err
	}
	h := sha256.New()
	h.Write([]byte(challengeTag + "\x00"))
	h.Write(canon)
	value := h.Sum(nil)
	opts, session, err := c.rp.BeginLogin(u, webauthn.WithChallenge(value), webauthn.WithAllowedCredentials(allow),
		webauthn.WithUserVerification(protocol.VerificationRequired))
	if err != nil {
		return ApprovalReply{}, err
	}
	sess, _ := json.Marshal(session)
	optsJSON, _ := json.Marshal(opts)
	disp := Display{ConsumerID: i.ConsumerID, WalletID: w.WalletID, Payer: b.From, PayTo: b.PayTo, Amount: b.Amount, AssetContract: b.AssetContract,
		ChainID: b.ChainID, HTTPMethod: i.HTTPMethod, ResourceURL: i.ResourceURL, TaskID: i.TaskID, PurchaseOrderID: i.PurchaseOrderID, ValidBefore: b.ValidBefore}
	if a := c.cfg.asset(b.ChainID, b.AssetContract); a != nil {
		disp.AssetName = a.EIP712Name
	}
	dispJSON, _ := json.Marshal(disp)
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
			return Refusal("SESSION_DECIDED") // one verified decision per session, ever
		}
		// An issued challenge of this session past its deadline is recorded
		// expired first; one still open stays the session's (CHALLENGE_OPEN):
		// the Host closes it before asking again (design 7).
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
			id, r.RequestID, fp, b64(value), string(canon), r.ApprovalSessionID, r.Decision, i.ConsumerID, w.WalletID, intent, rec.JTI,
			ap.ApproverID, ap.Version, c.cfg.hash, string(sess), string(optsJSON), string(dispJSON), expires, now)
		return err
	})
	if errors.Is(err, Refusal("REQUEST_ID_RACE")) {
		return c.Challenge(ctx, r)
	}
	if err != nil {
		return ApprovalReply{}, err
	}
	return ApprovalReply{ChallengeID: id, RequestID: r.RequestID, State: "issued", ApprovalSessionID: r.ApprovalSessionID, Decision: r.Decision,
		ConsumerID: i.ConsumerID, WalletID: w.WalletID, IntentHash: intent, ApproverID: ap.ApproverID, ExpiresAt: expires,
		Options: optsJSON, Display: &disp}, nil
}

// inTx runs fn in one write transaction with the time taken inside it.
func (c *ApprovalCore) inTx(ctx context.Context, fn func(tx *sql.Tx, now uint64) error) error {
	tx, err := c.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if err := fn(tx, c.clock()); err != nil {
		return err
	}
	return tx.Commit()
}

type challengeRow struct {
	id, requestID, fingerprint, challenge, sessionID, decision, consumer, wallet, intent, jti, approver, configHash string
	session, options, display, state                                                                                string
	approverVersion, expiresAt                                                                                      uint64
	failures                                                                                                        int
}

const challengeCols = `challenge_id, request_id, fingerprint, challenge, approval_session_id, decision, consumer_id, wallet_id, intent_hash, jti,
  approver_id, config_hash, session, options, display, state, approver_version, expires_at, failures`

func scanChallenge(row interface{ Scan(...any) error }) (challengeRow, bool, error) {
	var a challengeRow
	err := row.Scan(&a.id, &a.requestID, &a.fingerprint, &a.challenge, &a.sessionID, &a.decision, &a.consumer, &a.wallet, &a.intent, &a.jti,
		&a.approver, &a.configHash, &a.session, &a.options, &a.display, &a.state, &a.approverVersion, &a.expiresAt, &a.failures)
	if errors.Is(err, sql.ErrNoRows) {
		return a, false, nil
	}
	return a, err == nil, err
}

func (c *ApprovalCore) challengeBy(ctx context.Context, col, v string) (challengeRow, bool, error) {
	return scanChallenge(c.db.QueryRowContext(ctx, `SELECT `+challengeCols+` FROM approval_challenges WHERE `+col+` = ?`, v))
}

// answerChallenge is a stored challenge's answer. A query never answers
// issued for one past its deadline: that is recorded expired first, by a
// short write guarded by the state, so a concurrent used is never
// overwritten. A used one names its approval.
func (c *ApprovalCore) answerChallenge(ctx context.Context, a challengeRow) (ApprovalReply, error) {
	if a.state == "issued" {
		now := c.clock()
		if _, err := c.db.ExecContext(ctx, `UPDATE approval_challenges SET state = 'expired', closed_at = ? WHERE challenge_id = ? AND state = 'issued' AND ? >= expires_at`,
			now, a.id, now); err != nil {
			return ApprovalReply{}, err
		}
		var found bool
		var err error
		if a, found, err = c.challengeBy(ctx, "challenge_id", a.id); err != nil || !found {
			return ApprovalReply{}, errors.Join(err, errors.New("challenge vanished"))
		}
	}
	r := ApprovalReply{ChallengeID: a.id, RequestID: a.requestID, State: a.state, ApprovalSessionID: a.sessionID, Decision: a.decision,
		ConsumerID: a.consumer, WalletID: a.wallet, IntentHash: a.intent, ApproverID: a.approver, ExpiresAt: a.expiresAt}
	var d Display
	if json.Unmarshal([]byte(a.display), &d) == nil {
		r.Display = &d
	}
	switch a.state {
	case "issued": // the options as first given: the same challenge and allowed credentials
		r.Options = json.RawMessage(a.options)
	case "used":
		if err := c.db.QueryRowContext(ctx, `SELECT approval_id, verify_request_id FROM human_approvals WHERE challenge_id = ?`, a.id).Scan(&r.ApprovalID, &r.VerifyRequestID); err != nil {
			return ApprovalReply{}, err
		}
	}
	return r, nil
}

// ChallengeByRequest and ChallengeByID are the queries of a challenge
// (design 7, breakpoint 1 and close's lost reply).
func (c *ApprovalCore) ChallengeByRequest(ctx context.Context, requestID string) (ApprovalReply, error) {
	return c.challengeQuery(ctx, "request_id", requestID)
}

func (c *ApprovalCore) ChallengeByID(ctx context.Context, challengeID string) (ApprovalReply, error) {
	return c.challengeQuery(ctx, "challenge_id", challengeID)
}

func (c *ApprovalCore) challengeQuery(ctx context.Context, col, v string) (ApprovalReply, error) {
	if !requestIDPattern.MatchString(v) {
		return ApprovalReply{}, Refusal("BAD_REQUEST")
	}
	a, found, err := c.challengeBy(ctx, col, v)
	if err != nil { // the read failed: not "unknown"
		return ApprovalReply{}, err
	}
	if !found {
		return ApprovalReply{}, Refusal("CHALLENGE_UNKNOWN")
	}
	return c.answerChallenge(ctx, a)
}

// CloseChallenge is POST /v1/approvals/challenges/{id}/close: in one write
// transaction, with its time taken there, an issued challenge becomes
// expired (at or after its deadline) or closed (before it); used, closed
// and expired are never changed. It answers the authoritative state, as
// often as it is asked.
func (c *ApprovalCore) CloseChallenge(ctx context.Context, challengeID, closeRequestID string) (ApprovalReply, error) {
	if !requestIDPattern.MatchString(challengeID) || !requestIDPattern.MatchString(closeRequestID) {
		return ApprovalReply{}, Refusal("BAD_REQUEST")
	}
	err := c.inTx(ctx, func(tx *sql.Tx, now uint64) error {
		if _, err := tx.ExecContext(ctx, `UPDATE approval_challenges SET state = 'expired', closed_at = ? WHERE challenge_id = ? AND state = 'issued' AND ? >= expires_at`, now, challengeID, now); err != nil {
			return err
		}
		_, err := tx.ExecContext(ctx, `UPDATE approval_challenges SET state = 'closed', closed_at = ? WHERE challenge_id = ? AND state = 'issued' AND ? < expires_at`, now, challengeID, now)
		return err
	})
	if err != nil {
		return ApprovalReply{}, err
	}
	return c.ChallengeByID(ctx, challengeID)
}

// Verify is POST /v1/approvals/verify. A resend of a verify request with
// the same content answers what was saved, without verifying again or
// looking at the counter; the same ID with other content is a conflict.
// Refusals are saved too and leave the challenge issued, up to
// challengeFailures refusals.
func (c *ApprovalCore) Verify(ctx context.Context, r VerifyRequest) (ApprovalReply, error) {
	if !requestIDPattern.MatchString(r.VerifyRequestID) || !requestIDPattern.MatchString(r.ChallengeID) || len(r.Order) == 0 || len(r.Assertion) == 0 {
		return ApprovalReply{}, Refusal("BAD_REQUEST")
	}
	order, err := contracts.Canonical(r.Order)
	if err != nil {
		return ApprovalReply{}, Refusal("ORDER_INVALID")
	}
	os := sha256.Sum256(order)
	as := sha256.Sum256(r.Assertion)
	fp, _, err := digestOf(verifyReq, map[string]string{"challenge_id": r.ChallengeID, "order_fingerprint": hex.EncodeToString(os[:]),
		"assertion_sha256": hex.EncodeToString(as[:])})
	if err != nil {
		return ApprovalReply{}, Refusal("BAD_REQUEST")
	}
	if rep, found, err := c.verifyAnswer(ctx, r.VerifyRequestID, fp); err != nil || found {
		return rep, err
	}
	a, found, err := c.challengeBy(ctx, "challenge_id", r.ChallengeID)
	if err != nil {
		return ApprovalReply{}, err
	}
	if !found {
		return c.refuse(ctx, r, fp, nil, "CHALLENGE_UNKNOWN", false)
	}
	code, cred, parsed := c.check(ctx, a, r)
	if code != "" {
		return c.refuse(ctx, r, fp, &a, code, true)
	}
	// Tests only: a close (or anything) can happen here, between the
	// checks and the commit, as it could in production.
	if err := at("verify-checked", a.id); err != nil {
		return ApprovalReply{}, err
	}
	return c.commit(ctx, r, fp, a, cred, parsed)
}

// verifyAnswer is the saved outcome of a verify request, if there is one.
func (c *ApprovalCore) verifyAnswer(ctx context.Context, id, fp string) (ApprovalReply, bool, error) {
	var savedFP, challengeID, outcome string
	var approval sql.NullString
	err := c.db.QueryRowContext(ctx, `SELECT fingerprint, challenge_id, outcome, approval_id FROM verify_requests WHERE verify_request_id = ?`, id).
		Scan(&savedFP, &challengeID, &outcome, &approval)
	if errors.Is(err, sql.ErrNoRows) {
		return ApprovalReply{}, false, nil
	} else if err != nil {
		return ApprovalReply{}, false, err
	}
	if fp != "" && savedFP != fp {
		return ApprovalReply{}, true, Refusal("REQUEST_ID_CONFLICT")
	}
	if !approval.Valid {
		return ApprovalReply{ChallengeID: challengeID, VerifyRequestID: id, Error: outcome}, true, Refusal(outcome)
	}
	rep, err := c.approvalBy(ctx, "approval_id", approval.String)
	return rep, true, err
}

// check is the verification of the assertion against the challenge, the
// order and the credential, before anything is written (design 4.4).
func (c *ApprovalCore) check(ctx context.Context, a challengeRow, r VerifyRequest) (string, *webauthn.Credential, *protocol.ParsedCredentialAssertionData) {
	now := c.clock()
	switch {
	case a.state == "used":
		return "CHALLENGE_USED", nil, nil
	case a.state == "closed":
		return "CHALLENGE_CLOSED", nil, nil
	case a.state == "expired" || now >= a.expiresAt:
		return "CHALLENGE_EXPIRED", nil, nil
	}
	if _, intent, err := readOrder(r.Order); err != nil || intent != a.intent {
		return "INTENT_MISMATCH", nil, nil
	}
	ap := c.cfg.approver(a.approver)
	if ap == nil || ap.Version != a.approverVersion || c.cfg.hash != a.configHash {
		return "APPROVER_CHANGED", nil, nil
	}
	parsed, err := protocol.ParseCredentialRequestResponseBytes(r.Assertion)
	if err != nil {
		return "ASSERTION_INVALID", nil, nil
	}
	u, states, err := c.user(ctx, c.db, a.approver)
	if err != nil {
		return "ASSERTION_INVALID", nil, nil
	}
	var stored *webauthn.Credential
	for i := range u.creds {
		if string(u.creds[i].ID) == string(parsed.RawID) {
			stored = &u.creds[i]
		}
	}
	switch st := states[b64(parsed.RawID)]; {
	case stored == nil: // not this approver's: another consumer's, unknown, or a copied ID
		return "ASSERTION_INVALID", nil, nil
	case st == "pending":
		return "CREDENTIAL_PENDING", nil, nil
	case st == "suspect":
		return "CREDENTIAL_SUSPECT", nil, nil
	case st != "active":
		return "CREDENTIAL_REVOKED", nil, nil
	}
	var s webauthn.SessionData
	if err := json.Unmarshal([]byte(a.session), &s); err != nil {
		return "ASSERTION_INVALID", nil, nil
	}
	s.UserVerification = protocol.VerificationRequired
	if _, err := c.rp.ValidateLogin(u, s, parsed); err != nil {
		return assertionCode(err), nil, nil
	}
	return "", stored, parsed
}

// assertionCode reduces a library error to a fixed code: its own text,
// which can carry the expected and received challenge, is never kept.
func assertionCode(err error) string {
	var pe *protocol.Error
	if !errors.As(err, &pe) {
		return "ASSERTION_INVALID"
	}
	switch pe.Details {
	case "Error validating challenge":
		return "ASSERTION_INVALID_CHALLENGE"
	case "Error validating origin", "Error validating cross origin flag", "Error validating topOrigin", "Error validating top origin":
		return "ASSERTION_INVALID_ORIGIN"
	case "Error validating ceremony type":
		return "ASSERTION_INVALID_TYPE"
	// D6: BE is fixed at registration. The library compares it after the
	// signature is verified, so only the credential's holder can make it
	// change, and that marks the credential suspect (review R2: a check
	// before the signature let anyone with the credential ID do it).
	case "Backup Eligible flag inconsistency detected during login validation":
		return "ASSERTION_INVALID_FLAGS:suspect"
	case "Backup State Flag is true but Backup Eligible flag is false which is invalid":
		return "ASSERTION_INVALID_FLAGS"
	}
	switch {
	case strings.HasPrefix(pe.DevInfo, "RP Hash mismatch"):
		return "ASSERTION_INVALID_RPID"
	case pe.DevInfo == "User verification required but flag not set by authenticator", pe.DevInfo == "User presence required but flag not set by authenticator":
		return "ASSERTION_INVALID_FLAGS"
	case pe.Type == "invalid_signature":
		return "ASSERTION_INVALID_SIGNATURE"
	}
	return "ASSERTION_INVALID"
}

// refuse saves a refusal of verify request r and answers it. A refusal of
// the assertion counts against the challenge (counts): at
// challengeFailures the still issued challenge is closed. A code ending in
// ":suspect" also marks the credential suspect.
func (c *ApprovalCore) refuse(ctx context.Context, r VerifyRequest, fp string, a *challengeRow, code string, counts bool) (ApprovalReply, error) {
	code, suspect := strings.CutSuffix(code, ":suspect")
	if code == "CHALLENGE_USED" || code == "CHALLENGE_CLOSED" || code == "CHALLENGE_EXPIRED" || code == "CHALLENGE_UNKNOWN" {
		counts = false
	}
	var credID string
	if suspect {
		if p, err := protocol.ParseCredentialRequestResponseBytes(r.Assertion); err == nil {
			credID = b64(p.RawID)
		}
	}
	err := c.inTx(ctx, func(tx *sql.Tx, now uint64) error {
		var n int
		if err := tx.QueryRowContext(ctx, `SELECT count(*) FROM verify_requests WHERE verify_request_id = ?`, r.VerifyRequestID).Scan(&n); err != nil {
			return err
		} else if n > 0 {
			return Refusal("REQUEST_ID_RACE")
		}
		if _, err := tx.ExecContext(ctx, `INSERT INTO verify_requests (verify_request_id, fingerprint, challenge_id, outcome, received_at) VALUES (?, ?, ?, ?, ?)`,
			r.VerifyRequestID, fp, r.ChallengeID, code, now); err != nil {
			return err
		}
		if credID != "" {
			if _, err := tx.ExecContext(ctx, `UPDATE credentials SET state = 'suspect', state_reason = ? WHERE credential_id = ? AND state = 'active'`, code, credID); err != nil {
				return err
			}
		}
		if counts && a != nil {
			if _, err := tx.ExecContext(ctx, `UPDATE approval_challenges SET failures = failures + 1 WHERE challenge_id = ?`, a.id); err != nil {
				return err
			}
			_, err := tx.ExecContext(ctx, `UPDATE approval_challenges SET state = 'closed', closed_at = ? WHERE challenge_id = ? AND state = 'issued' AND failures >= ?`,
				now, a.id, challengeFailures)
			return err
		}
		return nil
	})
	if errors.Is(err, Refusal("REQUEST_ID_RACE")) {
		rep, _, err := c.verifyAnswer(ctx, r.VerifyRequestID, fp)
		return rep, err
	}
	if err != nil {
		return ApprovalReply{}, err
	}
	return ApprovalReply{ChallengeID: r.ChallengeID, VerifyRequestID: r.VerifyRequestID, Error: code}, Refusal(code)
}

// commit is design 6.1's verify commit: only an issued -> used update of
// exactly one row, before the deadline, with the time taken inside the
// transaction, writes the decision and the verify request, together. The
// credential's state and counter are read again inside it. Otherwise the
// row is read again in the same transaction and the refusal saved.
func (c *ApprovalCore) commit(ctx context.Context, r VerifyRequest, fp string, a challengeRow, cred *webauthn.Credential, p *protocol.ParsedCredentialAssertionData) (ApprovalReply, error) {
	ad := p.Response.AuthenticatorData
	approvalID, err := newID("hap-")
	if err != nil {
		return ApprovalReply{}, err
	}
	credID := b64(cred.ID)
	cdh := sha256.Sum256(p.Raw.AssertionResponse.ClientDataJSON)
	sig := sha256.Sum256(p.Response.Signature)
	var code string
	err = c.inTx(ctx, func(tx *sql.Tx, now uint64) error {
		code = ""
		var n int
		if err := tx.QueryRowContext(ctx, `SELECT count(*) FROM verify_requests WHERE verify_request_id = ?`, r.VerifyRequestID).Scan(&n); err != nil {
			return err
		} else if n > 0 {
			return Refusal("REQUEST_ID_RACE")
		}
		// The challenge first (review R1): this write transaction holds the
		// lock, so its state read here is the authoritative one. A
		// challenge no longer issued answers by its state, and nothing of
		// the credential is looked at or changed.
		var state string
		var expiresAt uint64
		if err := tx.QueryRowContext(ctx, `SELECT state, expires_at FROM approval_challenges WHERE challenge_id = ?`, a.id).Scan(&state, &expiresAt); err != nil {
			return err
		}
		if state != "issued" || now >= expiresAt {
			switch state {
			case "closed":
				code = "CHALLENGE_CLOSED"
			case "used":
				code = "CHALLENGE_USED"
			default: // issued past its deadline, or expired
				code = "CHALLENGE_EXPIRED"
				if _, err := tx.ExecContext(ctx, `UPDATE approval_challenges SET state = 'expired', closed_at = ? WHERE challenge_id = ? AND state = 'issued'`, now, a.id); err != nil {
					return err
				}
			}
			return c.saveRefusal(ctx, tx, now, r, fp, a.id, code, false)
		}
		var rec, st string
		if err := tx.QueryRowContext(ctx, `SELECT record, state FROM credentials WHERE credential_id = ?`, credID).Scan(&rec, &st); err != nil {
			return err
		}
		var fresh webauthn.Credential
		if err := json.Unmarshal([]byte(rec), &fresh); err != nil {
			return err
		}
		stored := fresh.Authenticator.SignCount
		switch {
		case st != "active":
			code = map[string]string{"suspect": "CREDENTIAL_SUSPECT", "pending": "CREDENTIAL_PENDING"}[st]
			if code == "" {
				code = "CREDENTIAL_REVOKED"
			}
		case fresh.Flags.BackupEligible != ad.Flags.HasBackupEligible():
			code = "ASSERTION_INVALID_FLAGS"
		case !(ad.Counter == 0 && stored == 0) && ad.Counter <= stored: // D6, this product's rule: 0/0 is accepted
			code = "ASSERTION_COUNTER"
		}
		if code != "" {
			if code == "ASSERTION_INVALID_FLAGS" || code == "ASSERTION_COUNTER" {
				if _, err := tx.ExecContext(ctx, `UPDATE credentials SET state = 'suspect', state_reason = ? WHERE credential_id = ? AND state = 'active'`, code, credID); err != nil {
					return err
				}
			}
			return c.saveRefusal(ctx, tx, now, r, fp, a.id, code, true)
		}
		res, err := tx.ExecContext(ctx, `UPDATE approval_challenges SET state = 'used' WHERE challenge_id = ? AND state = 'issued' AND ? < expires_at`, a.id, now)
		if err != nil {
			return err
		}
		if k, _ := res.RowsAffected(); k != 1 { // the state read above holds in this transaction
			return errors.New("challenge changed inside its write transaction")
		}
		// The approval never outlives the order's valid_before (the order's
		// intent was checked against the challenge's).
		po, _, err := readOrder(r.Order)
		if err != nil {
			return err
		}
		expires := min(now+c.cfg.WebAuthn.ApprovalTTLSeconds, po.PaymentBinding.ValidBefore)
		fresh.Authenticator.SignCount = ad.Counter
		fresh.Flags.BackupState = ad.Flags.HasBackupState()
		updated, err := json.Marshal(fresh)
		if err != nil {
			return err
		}
		if _, err := tx.ExecContext(ctx, `UPDATE credentials SET record = ? WHERE credential_id = ?`, string(updated), credID); err != nil {
			return err
		}
		if _, err := tx.ExecContext(ctx, `INSERT INTO human_approvals (approval_id, challenge_id, verify_request_id, jti, decision, approval_session_id, intent_hash,
			approver_id, approver_version, credential_id, flags, sign_count, client_data_hash, authenticator_data, signature_hash, verified_at, expires_at, state)
			VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, 'verified')`,
			approvalID, a.id, r.VerifyRequestID, a.jti, a.decision, a.sessionID, a.intent, a.approver, a.approverVersion, credID,
			flagString(ad.Flags), ad.Counter, hex.EncodeToString(cdh[:]), b64(p.Raw.AssertionResponse.AuthenticatorData), hex.EncodeToString(sig[:]), now, expires); err != nil {
			return err
		}
		_, err = tx.ExecContext(ctx, `INSERT INTO verify_requests (verify_request_id, fingerprint, challenge_id, outcome, approval_id, received_at) VALUES (?, ?, ?, 'verified', ?, ?)`,
			r.VerifyRequestID, fp, a.id, approvalID, now)
		return err
	})
	if errors.Is(err, Refusal("REQUEST_ID_RACE")) {
		rep, _, err := c.verifyAnswer(ctx, r.VerifyRequestID, fp)
		return rep, err
	}
	if err != nil {
		return ApprovalReply{}, err
	}
	if code != "" {
		return ApprovalReply{ChallengeID: a.id, VerifyRequestID: r.VerifyRequestID, Error: code}, Refusal(code)
	}
	return c.approvalBy(ctx, "approval_id", approvalID)
}

func (c *ApprovalCore) saveRefusal(ctx context.Context, tx *sql.Tx, now uint64, r VerifyRequest, fp, challengeID, code string, counts bool) error {
	if _, err := tx.ExecContext(ctx, `INSERT INTO verify_requests (verify_request_id, fingerprint, challenge_id, outcome, received_at) VALUES (?, ?, ?, ?, ?)`,
		r.VerifyRequestID, fp, challengeID, code, now); err != nil {
		return err
	}
	if !counts {
		return nil
	}
	if _, err := tx.ExecContext(ctx, `UPDATE approval_challenges SET failures = failures + 1 WHERE challenge_id = ?`, challengeID); err != nil {
		return err
	}
	_, err := tx.ExecContext(ctx, `UPDATE approval_challenges SET state = 'closed', closed_at = ? WHERE challenge_id = ? AND state = 'issued' AND failures >= ?`,
		now, challengeID, challengeFailures)
	return err
}

// approvalBy is a verified decision's answer.
func (c *ApprovalCore) approvalBy(ctx context.Context, col, v string) (ApprovalReply, error) {
	var r ApprovalReply
	var credID string
	err := c.db.QueryRowContext(ctx, `SELECT approval_id, challenge_id, verify_request_id, decision, approval_session_id, intent_hash, approver_id,
		credential_id, flags, verified_at, expires_at, state FROM human_approvals WHERE `+col+` = ?`, v).
		Scan(&r.ApprovalID, &r.ChallengeID, &r.VerifyRequestID, &r.Decision, &r.ApprovalSessionID, &r.IntentHash, &r.ApproverID,
			&credID, &r.Flags, &r.VerifiedAt, &r.ExpiresAt, &r.State)
	if errors.Is(err, sql.ErrNoRows) {
		return ApprovalReply{}, Refusal("APPROVAL_UNKNOWN")
	} else if err != nil {
		return ApprovalReply{}, err
	}
	id, err := base64.RawURLEncoding.DecodeString(credID)
	if err != nil {
		return ApprovalReply{}, err
	}
	r.CredentialRef = CredentialRef(id)
	return r, nil
}

// ApprovalByVerifyRequest answers GET /v1/approvals?verify_request_id=:
// the saved outcome, a decision or a refusal; VERIFY_UNKNOWN when none is
// saved (the read succeeded and found nothing).
func (c *ApprovalCore) ApprovalByVerifyRequest(ctx context.Context, id string) (ApprovalReply, error) {
	if !requestIDPattern.MatchString(id) {
		return ApprovalReply{}, Refusal("BAD_REQUEST")
	}
	rep, found, err := c.verifyAnswer(ctx, id, "")
	if err == nil && !found {
		return ApprovalReply{}, Refusal("VERIFY_UNKNOWN")
	}
	return rep, err
}

func (c *ApprovalCore) ApprovalByID(ctx context.Context, id string) (ApprovalReply, error) {
	if !requestIDPattern.MatchString(id) {
		return ApprovalReply{}, Refusal("BAD_REQUEST")
	}
	return c.approvalBy(ctx, "approval_id", id)
}

// Abandon gives up a verified approval the Host could not use (its session
// closed first): verified -> abandoned; any other state is answered as it
// is, with APPROVAL_NOT_VERIFIED.
func (c *ApprovalCore) Abandon(ctx context.Context, id string) (ApprovalReply, error) {
	if !requestIDPattern.MatchString(id) {
		return ApprovalReply{}, Refusal("BAD_REQUEST")
	}
	var n int64
	err := c.inTx(ctx, func(tx *sql.Tx, now uint64) error {
		res, err := tx.ExecContext(ctx, `UPDATE human_approvals SET state = 'abandoned', closed_at = ? WHERE approval_id = ? AND state = 'verified'`, now, id)
		if err != nil {
			return err
		}
		n, _ = res.RowsAffected()
		return nil
	})
	if err != nil {
		return ApprovalReply{}, err
	}
	r, err := c.approvalBy(ctx, "approval_id", id)
	if err == nil && n == 0 && r.State != "abandoned" {
		return r, Refusal("APPROVAL_NOT_VERIFIED")
	}
	return r, err
}
