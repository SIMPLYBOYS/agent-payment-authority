package signer

// The Signer core (docs/commercial-p1b-design.md 4.3, 5): validation, the
// one-time reservation of an attempt with its immutable snapshot, the final
// decision that alone lets a signature out, abandon, query and recovery.
// Its DB is the authority for consumption; every write is one SQLite
// transaction (single writer, BEGIN IMMEDIATE).

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/SIMPLYBOYS/agent-payment-authority/internal/contracts"
	"github.com/SIMPLYBOYS/agent-payment-authority/internal/paycontract"
	"github.com/decred/dcrd/dcrec/secp256k1/v4"
	_ "modernc.org/sqlite"
)

// ReservationVerifier confirms that the order's intent has been reserved on
// chain (Z2/P2). The only implementation in this package refuses every
// order: before P2 the Signer signs nothing outside tests.
type ReservationVerifier interface {
	Verify(intentHash string, reservation json.RawMessage) error
}

// Unavailable is the production verifier until P2.
type Unavailable struct{}

func (Unavailable) Verify(string, json.RawMessage) error { return Refusal("RESERVATION_UNAVAILABLE") }

// Refusal is a fixed reason code; it never carries request content.
type Refusal string

func (r Refusal) Error() string { return string(r) }

// Clock is the trusted time in Unix seconds.
type Clock func() uint64

// hook runs at the named points of an attempt (tests only; nil in
// production): "after-accept", "after-sign", "after-final", and
// "wait-lock" (with the wallet ID) just before waiting for a wallet's lock.
// An error stops there, as a crash would.
var hook func(stage, attemptID string) error

func at(stage, id string) error {
	if hook == nil {
		return nil
	}
	return hook(stage, id)
}

type Core struct {
	cfg   *Config
	keys  map[string]*secp256k1.PrivateKey // by address
	db    *sql.DB
	clock Clock
	res   ReservationVerifier

	mu      sync.Mutex
	wallets map[string]chan struct{} // the per-wallet signing lock
}

// lockWait bounds the wait for a wallet's signing lock (design 5.6).
var lockWait = 3 * time.Second

const schema = `
CREATE TABLE IF NOT EXISTS attempts (
  attempt_id     TEXT PRIMARY KEY,
  request_id     TEXT NOT NULL UNIQUE,
  fingerprint    TEXT NOT NULL,
  intent_hash    TEXT NOT NULL UNIQUE,
  wallet_id      TEXT NOT NULL,
  chain_id       TEXT NOT NULL,
  asset_contract TEXT NOT NULL,
  payer          TEXT NOT NULL,
  nonce          TEXT NOT NULL,
  delegation_id  TEXT NOT NULL,
  amount         INTEGER NOT NULL CHECK (amount > 0),
  state          TEXT NOT NULL CHECK (state IN ('accepted', 'signed', 'abandoned', 'expired_unsigned')),
  accepted_at    INTEGER NOT NULL,
  auth_deadline  INTEGER NOT NULL,
  typed_digest   TEXT NOT NULL,
  snapshot       TEXT NOT NULL,
  signature      TEXT,
  signed_at      INTEGER,
  closed_at      INTEGER,
  UNIQUE (chain_id, asset_contract, payer, nonce),
  CHECK ((state = 'signed') = (signature IS NOT NULL AND signed_at IS NOT NULL)),
  CHECK ((state IN ('abandoned', 'expired_unsigned')) = (closed_at IS NOT NULL))
) STRICT;
CREATE INDEX IF NOT EXISTS spend ON attempts (delegation_id, state);
`

// NewCore opens the Signer DB at dbPath with an unlocked key per configured
// wallet. A wallet without its key is refused. Accepted attempts left by a
// previous process are finished or expired before it serves (recover);
// ctx bounds that recovery.
func NewCore(ctx context.Context, cfg *Config, keys map[string]*secp256k1.PrivateKey, dbPath string, clock Clock, res ReservationVerifier) (*Core, error) {
	for _, w := range cfg.Wallets {
		if keys[w.Address] == nil {
			return nil, fmt.Errorf("no key for wallet %s", w.WalletID)
		}
	}
	db, err := sql.Open("sqlite", dbPath+"?_pragma=busy_timeout(5000)&_pragma=journal_mode(WAL)&_pragma=synchronous(FULL)&_txlock=immediate")
	if err != nil {
		return nil, err
	}
	db.SetMaxOpenConns(1)
	if _, err := db.ExecContext(ctx, schema); err != nil {
		db.Close()
		return nil, err
	}
	c := &Core{cfg: cfg, keys: keys, db: db, clock: clock, res: res, wallets: map[string]chan struct{}{}}
	if err := c.recover(ctx); err != nil {
		db.Close()
		return nil, err
	}
	return c, nil
}

func (c *Core) Close() error { return c.db.Close() }

// lockWallet takes the wallet's signing lock, or refuses SIGNER_BUSY after
// lockWait; it gives up sooner when ctx ends (the request's own deadline,
// never a fresh one). It serializes accepting and signing only; abandon and
// queries never take it.
func (c *Core) lockWallet(ctx context.Context, id string) (unlock func(), err error) {
	c.mu.Lock()
	ch := c.wallets[id]
	if ch == nil {
		ch = make(chan struct{}, 1)
		c.wallets[id] = ch
	}
	c.mu.Unlock()
	t := time.NewTimer(lockWait)
	defer t.Stop()
	if err := at("wait-lock", id); err != nil {
		return nil, err
	}
	select {
	case ch <- struct{}{}:
		return func() { <-ch }, nil
	case <-ctx.Done():
		return nil, ctx.Err()
	case <-t.C:
		return nil, Refusal("SIGNER_BUSY")
	}
}

// snapshot is written once, at acceptance, and only read afterwards.
type snapshot struct {
	Version       int             `json:"snapshot_version"`
	RequestID     string          `json:"request_id"`
	Fingerprint   string          `json:"fingerprint"`
	Order         json.RawMessage `json:"order"`
	Authorization Authorization   `json:"authorization"`
	DelegationSHA string          `json:"delegation_hash"`
	IntentHash    string          `json:"intent_hash"`
	Domain        Domain          `json:"domain"`
	Message       Transfer        `json:"message"`
	TypedDigest   string          `json:"typed_digest"`
	WalletID      string          `json:"wallet_id"`
	Address       string          `json:"address"`
	AuthDeadline  uint64          `json:"auth_deadline"`
	ConfigHash    string          `json:"config_hash"`
	Reservation   json.RawMessage `json:"reservation,omitempty"`
	AcceptedAt    uint64          `json:"accepted_at"`
}

// fingerprint covers what a request asks for, canonically, version 1.
func fingerprint(r SignRequest) (string, error) {
	raw, err := json.Marshal(struct {
		V             int             `json:"fingerprint_version"`
		Order         json.RawMessage `json:"order"`
		Authorization Authorization   `json:"authorization"`
		Reservation   json.RawMessage `json:"reservation,omitempty"`
	}{1, r.Order, r.Authorization, r.Reservation})
	if err != nil {
		return "", err
	}
	c, err := contracts.Canonical(raw)
	if err != nil {
		return "", err
	}
	h := sha256.New()
	h.Write([]byte("agent-payment-authority/signer/v1/request\x00"))
	h.Write(c)
	return "sha256:" + hex.EncodeToString(h.Sum(nil)), nil
}

var requestIDPattern = idPattern

// Sign is POST /v1/sign: a new request, or the resend of one already seen.
func (c *Core) Sign(ctx context.Context, r SignRequest) (Reply, error) {
	if !requestIDPattern.MatchString(r.RequestID) || len(r.Order) == 0 {
		return Reply{}, Refusal("BAD_REQUEST")
	}
	fp, err := fingerprint(r)
	if err != nil {
		return Reply{}, Refusal("BAD_REQUEST")
	}
	if a, found, err := c.byRequest(ctx, r.RequestID); err != nil {
		return Reply{}, err
	} else if found {
		if a.fingerprint != fp {
			return Reply{}, Refusal("REQUEST_ID_CONFLICT")
		}
		return c.resume(ctx, a)
	}
	var po struct {
		Intent struct {
			WalletID string `json:"wallet_id"`
		} `json:"intent"`
	}
	if json.Unmarshal(r.Order, &po) != nil || c.cfg.wallet(po.Intent.WalletID) == nil {
		if _, _, _, err := c.verify(r, fp, c.clock()); err != nil { // its own code, under no lock
			return Reply{}, err
		}
		return Reply{}, Refusal("WALLET_NOT_REGISTERED")
	}
	unlock, err := c.lockWallet(ctx, po.Intent.WalletID)
	if err != nil {
		return Reply{}, err
	}
	snap, d, amount, err := c.verify(r, fp, c.clock())
	if err != nil {
		unlock()
		return Reply{}, err
	}
	id, err := c.accept(ctx, snap, d, amount)
	if errors.Is(err, Refusal("REQUEST_ID_RACE")) { // a twin of this request was accepted meanwhile: answer as its resend
		unlock()
		return c.Sign(ctx, r)
	}
	defer unlock()
	if err != nil {
		return Reply{}, err
	}
	if err := at("after-accept", id); err != nil {
		return Reply{}, err
	}
	return c.finish(ctx, id)
}

// verify is design 4.3, steps 1 to 7: nothing is written.
func (c *Core) verify(r SignRequest, fp string, now uint64) (*snapshot, *Delegation, uint64, error) {
	var po paycontract.PurchaseOrder
	if err := contracts.Decode(r.Order, &po); err != nil || po.Validate() != nil {
		return nil, nil, 0, Refusal("ORDER_INVALID")
	}
	intent, err := po.IntentHash()
	if err != nil {
		return nil, nil, 0, Refusal("ORDER_INVALID")
	}
	switch r.Authorization.Method {
	case "delegation":
	case "webauthn":
		return nil, nil, 0, Refusal("HUMAN_APPROVAL_UNAVAILABLE") // A2
	default:
		return nil, nil, 0, Refusal("GRANT_METHOD_NOT_ACCEPTED") // simulated, and anything else
	}
	d := c.cfg.delegation(r.Authorization.DelegationID)
	i, b := po.Intent, po.PaymentBinding
	switch {
	case d == nil:
		return nil, nil, 0, Refusal("DELEGATION_UNKNOWN")
	case d.Version != r.Authorization.DelegationVersion:
		return nil, nil, 0, Refusal("DELEGATION_VERSION")
	case now < d.ValidFrom || now >= d.ValidUntil:
		return nil, nil, 0, Refusal("DELEGATION_EXPIRED")
	case d.ConsumerID != i.ConsumerID || d.WalletID != i.WalletID:
		return nil, nil, 0, Refusal("CONSUMER_MISMATCH")
	}
	w := c.cfg.wallet(i.WalletID)
	switch {
	case w == nil:
		return nil, nil, 0, Refusal("WALLET_NOT_REGISTERED")
	case w.ConsumerID != i.ConsumerID || b.From != w.Address || b.ChainID != w.ChainID:
		return nil, nil, 0, Refusal("CONSUMER_MISMATCH")
	}
	asset := c.cfg.asset(b.ChainID, b.AssetContract)
	if asset == nil || b.AssetContract != d.AssetContract || b.ChainID != d.ChainID || !quoteDomainIs(po.Quote, asset) {
		return nil, nil, 0, Refusal("ASSET_NOT_ALLOWED")
	}
	amount, ok := units(b.Amount)
	per, _ := units(d.PerTxMax)
	switch {
	case !containsString(d.Merchants, b.PayTo):
		return nil, nil, 0, Refusal("MERCHANT_NOT_ALLOWED")
	case !ok || amount > per:
		return nil, nil, 0, Refusal("OVER_PER_TX")
	case !(b.ValidAfter < now && now < b.ValidBefore) || b.ValidBefore-now > c.cfg.MaxWindowSeconds:
		return nil, nil, 0, Refusal("WINDOW_INVALID")
	}
	deadline := min(d.ValidUntil, b.ValidBefore)
	if err := c.res.Verify(intent, r.Reservation); err != nil {
		var ref Refusal
		if errors.As(err, &ref) {
			return nil, nil, 0, ref
		}
		return nil, nil, 0, Refusal("RESERVATION_MISMATCH")
	}
	dom := Domain{Name: asset.EIP712Name, Version: asset.EIP712Version, ChainID: b.ChainID, VerifyingContract: b.AssetContract}
	msg := Transfer{From: b.From, To: b.PayTo, Value: string(b.Amount), ValidAfter: b.ValidAfter, ValidBefore: b.ValidBefore, Nonce: b.Nonce}
	sep, err1 := DomainSeparator(dom)
	sh, err2 := TransferHash(msg)
	if errors.Join(err1, err2) != nil {
		return nil, nil, 0, Refusal("ORDER_INVALID")
	}
	order, _ := contracts.Canonical(r.Order)
	dh, _ := json.Marshal(d)
	dsum := sha256.Sum256(dh)
	return &snapshot{Version: 1, RequestID: r.RequestID, Fingerprint: fp, Order: order, Authorization: r.Authorization,
		DelegationSHA: "sha256:" + hex.EncodeToString(dsum[:]), IntentHash: intent, Domain: dom, Message: msg,
		TypedDigest: "0x" + hex.EncodeToString(TypedDigest(sep, sh)), WalletID: w.WalletID, Address: w.Address,
		AuthDeadline: deadline, ConfigHash: c.cfg.hash, Reservation: r.Reservation}, d, amount, nil
}

func containsString(list []string, s string) bool {
	for _, x := range list {
		if x == s {
			return true
		}
	}
	return false
}

// quoteDomainIs: the quote's extra name and version are the registered
// EIP-712 domain's.
func quoteDomainIs(q contracts.Quote, a *Asset) bool {
	var req struct {
		Extra map[string]string `json:"extra"`
	}
	if json.Unmarshal(q.SelectedRequirement, &req) != nil {
		return false
	}
	return req.Extra["name"] == a.EIP712Name && req.Extra["version"] == a.EIP712Version
}

// accept reserves the request ID, intent, nonce and the delegation's
// spend in one write transaction, with the snapshot. The time is taken
// after the write lock and must still be before the deadline.
func (c *Core) accept(ctx context.Context, s *snapshot, d *Delegation, amount uint64) (string, error) {
	tx, err := c.db.BeginTx(ctx, nil)
	if err != nil {
		return "", err
	}
	defer tx.Rollback()
	now := c.clock()
	if now >= s.AuthDeadline {
		return "", Refusal("AUTHORIZATION_EXPIRED")
	}
	var n int
	if err := tx.QueryRowContext(ctx, `SELECT count(*) FROM attempts WHERE request_id = ?`, s.RequestID).Scan(&n); err != nil {
		return "", err
	} else if n > 0 {
		return "", Refusal("REQUEST_ID_RACE")
	}
	m := s.Message
	for _, q := range []struct {
		code, sql string
		args      []any
	}{
		{"INTENT_IN_USE", `SELECT count(*) FROM attempts WHERE intent_hash = ?`, []any{s.IntentHash}},
		{"NONCE_IN_USE", `SELECT count(*) FROM attempts WHERE chain_id = ? AND asset_contract = ? AND payer = ? AND nonce = ?`,
			[]any{s.Domain.ChainID, s.Domain.VerifyingContract, m.From, m.Nonce}},
	} {
		if err := tx.QueryRowContext(ctx, q.sql, q.args...).Scan(&n); err != nil {
			return "", err
		} else if n > 0 {
			return "", Refusal(q.code)
		}
	}
	var spent uint64
	if err := tx.QueryRowContext(ctx, `SELECT coalesce(sum(amount), 0) FROM attempts WHERE delegation_id = ? AND state IN ('accepted', 'signed')`, d.DelegationID).Scan(&spent); err != nil {
		return "", err
	}
	if total, _ := units(d.TotalCap); spent+amount > total {
		return "", Refusal("OVER_TOTAL_CAP")
	}
	b := make([]byte, 16)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	id := "att-" + hex.EncodeToString(b)
	s.AcceptedAt = now
	body, err := json.Marshal(s)
	if err != nil {
		return "", err
	}
	canon, err := contracts.Canonical(body)
	if err != nil {
		return "", err
	}
	if _, err := tx.ExecContext(ctx, `INSERT INTO attempts (attempt_id, request_id, fingerprint, intent_hash, wallet_id, chain_id, asset_contract, payer, nonce,
		delegation_id, amount, state, accepted_at, auth_deadline, typed_digest, snapshot) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, 'accepted', ?, ?, ?, ?)`,
		id, s.RequestID, s.Fingerprint, s.IntentHash, s.WalletID, s.Domain.ChainID, s.Domain.VerifyingContract, m.From, m.Nonce,
		d.DelegationID, amount, now, s.AuthDeadline, s.TypedDigest, string(canon)); err != nil {
		return "", err
	}
	return id, tx.Commit()
}

type attempt struct {
	id, requestID, fingerprint, state, signature string
	acceptedAt, deadline                         uint64
	snap                                         snapshot
}

const attemptCols = `attempt_id, request_id, fingerprint, state, coalesce(signature, ''), accepted_at, auth_deadline, snapshot`

func scanAttempt(row interface{ Scan(...any) error }) (attempt, bool, error) {
	var a attempt
	var body string
	err := row.Scan(&a.id, &a.requestID, &a.fingerprint, &a.state, &a.signature, &a.acceptedAt, &a.deadline, &body)
	if errors.Is(err, sql.ErrNoRows) {
		return a, false, nil
	} else if err != nil {
		return a, false, err
	}
	if err := json.Unmarshal([]byte(body), &a.snap); err != nil {
		return a, false, err
	}
	return a, true, nil
}

func (c *Core) byRequest(ctx context.Context, id string) (attempt, bool, error) {
	return scanAttempt(c.db.QueryRowContext(ctx, `SELECT `+attemptCols+` FROM attempts WHERE request_id = ?`, id))
}

func (c *Core) byID(ctx context.Context, id string) (attempt, bool, error) {
	return scanAttempt(c.db.QueryRowContext(ctx, `SELECT `+attemptCols+` FROM attempts WHERE attempt_id = ?`, id))
}

// reply is an attempt's answer; a signed attempt carries its saved
// authorization, every time it is asked for.
func reply(a attempt) Reply {
	r := Reply{AttemptID: a.id, RequestID: a.requestID, State: a.state, IntentHash: a.snap.IntentHash, TypedDigest: a.snap.TypedDigest, AuthDeadline: a.deadline}
	if a.state == "signed" {
		r.Authorization = &SignedAuthorization{Transfer: a.snap.Message, Signature: a.signature}
	}
	return r
}

// resume answers a resend: an accepted attempt is finished (or expires) as
// a new one would be; any other state is answered as saved, without
// checking the authorization's validity again.
func (c *Core) resume(ctx context.Context, a attempt) (Reply, error) {
	if a.state != "accepted" {
		return reply(a), nil
	}
	unlock, err := c.lockWallet(ctx, a.snap.WalletID)
	if err != nil {
		return Reply{}, err
	}
	defer unlock()
	return c.finish(ctx, a.id)
}

// finish signs an accepted attempt from its snapshot and lets the
// signature out only through the final decision (design 5.3): in its own
// write transaction, with the time taken after the write lock, the row
// must still be accepted and before its deadline. The caller holds the
// wallet's signing lock.
func (c *Core) finish(ctx context.Context, id string) (Reply, error) {
	a, found, err := c.byID(ctx, id)
	if err != nil || !found {
		return Reply{}, errors.Join(err, Refusal("ATTEMPT_UNKNOWN"))
	}
	if a.state != "accepted" {
		return reply(a), nil
	}
	key := c.keys[a.snap.Address]
	if key == nil || Address(key.PubKey()) != a.snap.Address {
		return Reply{}, Refusal("KEY_VERSION_MISMATCH") // stays accepted, for the administrator
	}
	digest, err := hex.DecodeString(strings.TrimPrefix(a.snap.TypedDigest, "0x"))
	if err != nil || len(digest) != 32 {
		return Reply{}, errors.New("snapshot digest unreadable")
	}
	sig := "0x" + hex.EncodeToString(signDigest(key, digest))
	if err := at("after-sign", id); err != nil {
		return Reply{}, err
	}
	tx, err := c.db.BeginTx(ctx, nil)
	if err != nil {
		return Reply{}, err
	}
	defer tx.Rollback()
	now := c.clock()
	if now < a.acceptedAt {
		return Reply{}, Refusal("CLOCK_REGRESSION")
	}
	res, err := tx.ExecContext(ctx, `UPDATE attempts SET state = 'signed', signature = ?, signed_at = ? WHERE attempt_id = ? AND state = 'accepted' AND ? < auth_deadline`,
		sig, now, id, now)
	if err != nil {
		return Reply{}, err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		if _, err := tx.ExecContext(ctx, `UPDATE attempts SET state = 'expired_unsigned', closed_at = ? WHERE attempt_id = ? AND state = 'accepted'`, now, id); err != nil {
			return Reply{}, err
		}
	}
	if err := tx.Commit(); err != nil {
		return Reply{}, err
	}
	if err := at("after-final", id); err != nil {
		return Reply{}, err
	}
	a, _, err = c.byID(ctx, id)
	if err != nil {
		return Reply{}, err
	}
	switch a.state {
	case "signed":
		return reply(a), nil
	case "abandoned":
		return reply(a), Refusal("ATTEMPT_ABANDONED") // the signature computed here is dropped
	default:
		return reply(a), Refusal("ATTEMPT_EXPIRED")
	}
}

// Get is a query: it does not sign. An accepted attempt past its deadline
// is recorded expired by a short write guarded by its state, so a
// concurrent signed is never overwritten. It takes no wallet lock.
func (c *Core) Get(ctx context.Context, attemptID, requestID string) (Reply, error) {
	var a attempt
	var found bool
	var err error
	if attemptID != "" {
		a, found, err = c.byID(ctx, attemptID)
	} else {
		a, found, err = c.byRequest(ctx, requestID)
	}
	if err != nil {
		return Reply{}, err
	}
	if !found {
		return Reply{}, Refusal("ATTEMPT_UNKNOWN")
	}
	if a.state == "accepted" {
		now := c.clock()
		if _, err := c.db.ExecContext(ctx, `UPDATE attempts SET state = 'expired_unsigned', closed_at = ? WHERE attempt_id = ? AND state = 'accepted' AND ? >= auth_deadline`,
			now, a.id, now); err != nil {
			return Reply{}, err
		}
		if a, _, err = c.byID(ctx, a.id); err != nil {
			return Reply{}, err
		}
	}
	return reply(a), nil
}

// Abandon gives up an accepted attempt before it is signed. It takes no
// wallet lock: the conditional update on the row decides against a
// concurrent final decision. Past its deadline the attempt expires instead.
func (c *Core) Abandon(ctx context.Context, attemptID string) (Reply, error) {
	tx, err := c.db.BeginTx(ctx, nil)
	if err != nil {
		return Reply{}, err
	}
	defer tx.Rollback()
	now := c.clock()
	res, err := tx.ExecContext(ctx, `UPDATE attempts SET state = 'abandoned', closed_at = ? WHERE attempt_id = ? AND state = 'accepted' AND ? < auth_deadline`, now, attemptID, now)
	if err != nil {
		return Reply{}, err
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		if _, err := tx.ExecContext(ctx, `UPDATE attempts SET state = 'expired_unsigned', closed_at = ? WHERE attempt_id = ? AND state = 'accepted'`, now, attemptID); err != nil {
			return Reply{}, err
		}
	}
	if err := tx.Commit(); err != nil {
		return Reply{}, err
	}
	a, found, err := c.byID(ctx, attemptID)
	switch {
	case err != nil:
		return Reply{}, err
	case !found:
		return Reply{}, Refusal("ATTEMPT_UNKNOWN")
	case n == 0:
		return reply(a), Refusal("ATTEMPT_NOT_ACCEPTED")
	}
	return reply(a), nil
}

// recover finishes or expires the attempts a previous process left
// accepted, each from its snapshot, before the Core serves.
func (c *Core) recover(ctx context.Context) error {
	rows, err := c.db.QueryContext(ctx, `SELECT attempt_id, wallet_id FROM attempts WHERE state = 'accepted' ORDER BY accepted_at`)
	if err != nil {
		return err
	}
	var pending [][2]string
	for rows.Next() {
		var id, w string
		if err := rows.Scan(&id, &w); err != nil {
			rows.Close()
			return err
		}
		pending = append(pending, [2]string{id, w})
	}
	if err := errors.Join(rows.Err(), rows.Close()); err != nil {
		return err
	}
	for _, p := range pending {
		unlock, err := c.lockWallet(ctx, p[1])
		if err != nil {
			return err
		}
		_, err = c.finish(ctx, p[0])
		unlock()
		var ref Refusal
		if err != nil && !errors.As(err, &ref) {
			return err
		}
	}
	return nil
}
