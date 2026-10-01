// Package store is B1-05's persistence: one SQLite database holds the trusted
// config snapshots, purchases, decisions, each task's event chain and the task
// projection, and every change is one transaction. The design, including the
// order of every check, is docs/persistence-design.md.
//
// The Store never logs. Errors carry a code and fixed text, never request
// content.
package store

import (
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"

	"github.com/SIMPLYBOYS/agent-payment-authority/internal/contracts"
	"github.com/SIMPLYBOYS/agent-payment-authority/internal/policy"
	_ "modernc.org/sqlite" // CGo-free SQLite; pinned with modernc.org/libc in go.mod
)

const schemaVersion = 1
const defaultDeadline = 30 // seconds an Ask waits for approval

// Clock is the trusted host clock in Unix seconds; never a caller's value.
type Clock func() uint64

// OfferKey names a resource the trusted shop sells.
type OfferKey struct{ Method, URL string }

// OfferBook is the host's current trusted quotes. It admits new purchases
// only; stored history is never judged against it.
type OfferBook map[OfferKey][]contracts.Quote

// Config is host configuration; nothing in it comes from a request.
type Config struct {
	Path     string // APA_DB_PATH: a file path, never a DSN
	Registry contracts.RegistryConfig
	Offers   OfferBook
	Clock    Clock
}

// Error is a refused operation. Code is the contract reason when a contract
// check refused (OWNERSHIP_MISMATCH, IDEMPOTENCY_CONFLICT, TASK_NOT_ACTIVE,
// ...) or a store code (QUOTE_NOT_OFFERED, CLOCK_REGRESSION, ...). Store codes
// are never written into events.
type Error struct{ Code, Detail string }

func (e *Error) Error() string { return e.Code + ": " + e.Detail }

func refuse(code, detail string) error { return &Error{code, detail} }

// Code returns the code of a Store or contract error, or "".
func Code(err error) string {
	var e *Error
	if errors.As(err, &e) {
		return e.Code
	}
	return contracts.ErrorCode(err)
}

// CorruptError is a failed recovery check. The Store then refuses service.
type CorruptError struct{ Check, TaskID, Detail string }

func (e *CorruptError) Error() string {
	return fmt.Sprintf("store corrupt: %s check failed for task %q: %s", e.Check, e.TaskID, e.Detail)
}

type Store struct {
	w, r     *sql.DB
	registry *contracts.Registry
	regBody  []byte
	offers   map[OfferKey][][]byte // canonical quotes
	clock    Clock
	hook     func(stage string) error // tests only; nil in production
}

// Open checks the path, opens or creates the database, verifies everything
// stored (section 8 of the design) and records Asks that expired while the
// Store was closed. It returns a *CorruptError and serves nothing if any
// check fails.
func Open(cfg Config) (*Store, error) {
	if cfg.Clock == nil {
		return nil, errors.New("store needs the trusted host clock")
	}
	path, err := dbPath(cfg.Path)
	if err != nil {
		return nil, err
	}
	reg, err := contracts.NewRegistry(cfg.Registry)
	if err != nil {
		return nil, fmt.Errorf("registry: %w", err)
	}
	regBody, err := canonical(cfg.Registry)
	if err != nil {
		return nil, err
	}
	s := &Store{registry: reg, regBody: regBody, offers: map[OfferKey][][]byte{}, clock: cfg.Clock}
	for k, quotes := range cfg.Offers {
		for _, q := range quotes {
			if _, err := q.Hash(); err != nil {
				return nil, fmt.Errorf("offer book entry for %s %s: %w", k.Method, k.URL, err)
			}
			c, err := canonical(q)
			if err != nil {
				return nil, err
			}
			s.offers[k] = append(s.offers[k], c)
		}
	}
	// Each _pragma runs on every new connection (driver: applyQueryParams in
	// newConn), so no pooled connection misses one.
	const common = "?_pragma=busy_timeout(5000)&_pragma=foreign_keys(1)&_pragma=synchronous(FULL)"
	if s.w, err = sql.Open("sqlite", path+common+"&_txlock=immediate"); err != nil {
		return nil, err
	}
	s.w.SetMaxOpenConns(1)
	if s.r, err = sql.Open("sqlite", path+common+"&_pragma=query_only(1)"); err != nil {
		s.w.Close()
		return nil, err
	}
	if err := s.migrate(); err != nil {
		s.Close()
		return nil, err
	}
	if err := s.verify(); err != nil {
		s.Close()
		return nil, err
	}
	if err := s.ExpireDue(); err != nil {
		s.Close()
		return nil, err
	}
	return s, nil
}

func (s *Store) Close() error { return errors.Join(s.r.Close(), s.w.Close()) }

// dbPath accepts a file path only (never a DSN), resolves a relative one
// against the working directory, and keeps the database in a private
// directory: the directory guards the -wal and -shm files too.
func dbPath(p string) (string, error) {
	if p == "" || strings.ContainsAny(p, "?#") || strings.HasPrefix(p, "file:") {
		return "", errors.New("APA_DB_PATH must be a plain file path, not a DSN or URI")
	}
	abs, err := filepath.Abs(p)
	if err != nil {
		return "", err
	}
	dir := filepath.Dir(abs)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return "", err
	}
	fi, err := os.Stat(dir)
	if err != nil {
		return "", err
	}
	if fi.Mode().Perm()&0o077 != 0 {
		return "", fmt.Errorf("database directory %s must be private (0700), is %v", dir, fi.Mode().Perm())
	}
	if fi, err := os.Lstat(abs); err == nil && !fi.Mode().IsRegular() {
		return "", fmt.Errorf("database path %s is not a regular file", abs)
	} else if err != nil && !errors.Is(err, os.ErrNotExist) {
		return "", err
	}
	return abs, nil
}

const schema = `
CREATE TABLE config_snapshots (
  snapshot_hash TEXT PRIMARY KEY,
  kind          TEXT NOT NULL CHECK (kind IN ('registry', 'profile', 'credential', 'terms')),
  body          TEXT NOT NULL,
  recorded_at   INTEGER NOT NULL
) STRICT;
CREATE TABLE tasks (
  task_id             TEXT PRIMARY KEY,
  consumer_id         TEXT NOT NULL,
  agent_id            TEXT NOT NULL,
  wallet_id           TEXT NOT NULL,
  payer               TEXT NOT NULL,
  registry_snapshot   TEXT NOT NULL REFERENCES config_snapshots,
  profile_snapshot    TEXT REFERENCES config_snapshots,
  credential_snapshot TEXT REFERENCES config_snapshots,
  terms_snapshot      TEXT NOT NULL REFERENCES config_snapshots,
  budget_remaining    TEXT NOT NULL,
  status              TEXT NOT NULL,
  deny_reason         TEXT,
  next_sequence       INTEGER NOT NULL,
  chain_head          TEXT NOT NULL,
  started_at          INTEGER NOT NULL
) STRICT;
CREATE TABLE purchases (
  task_id           TEXT NOT NULL REFERENCES tasks,
  purchase_order_id TEXT NOT NULL,
  request_id        TEXT NOT NULL,
  retry_key         TEXT NOT NULL UNIQUE,
  chain_id          TEXT NOT NULL,
  asset_contract    TEXT NOT NULL,
  payer             TEXT NOT NULL,
  eip3009_nonce     TEXT NOT NULL,
  body              TEXT NOT NULL,
  intent_hash       TEXT NOT NULL,
  policy_verdict    TEXT NOT NULL,
  approval_status   TEXT NOT NULL,
  reason            TEXT NOT NULL,
  decided_at        INTEGER NOT NULL,
  approval_closed   TEXT,
  closed_at         INTEGER,
  PRIMARY KEY (task_id, purchase_order_id),
  UNIQUE (chain_id, asset_contract, payer, eip3009_nonce)
) STRICT;
CREATE TABLE events (
  task_id           TEXT NOT NULL REFERENCES tasks,
  sequence          INTEGER NOT NULL,
  event_id          TEXT NOT NULL UNIQUE,
  event_type        TEXT NOT NULL,
  purchase_order_id TEXT,
  observed_at       INTEGER NOT NULL,
  body              TEXT NOT NULL,
  event_hash        TEXT NOT NULL,
  PRIMARY KEY (task_id, sequence),
  FOREIGN KEY (task_id, purchase_order_id) REFERENCES purchases
) STRICT;
CREATE UNIQUE INDEX one_decision ON events (task_id, purchase_order_id) WHERE event_type = 'POLICY_DECIDED';
CREATE UNIQUE INDEX one_closure  ON events (task_id, purchase_order_id) WHERE event_type IN ('APPROVAL_EXPIRED', 'APPROVAL_WITHDRAWN');
CREATE UNIQUE INDEX one_end      ON events (task_id) WHERE event_type IN ('TASK_CANCELLED', 'TASK_TIMED_OUT');
`

// migrate sets WAL (a property of the file) and creates the schema once, in
// one transaction. A newer or unknown version is refused, never downgraded.
func (s *Store) migrate() error {
	var mode string
	if err := s.w.QueryRow("PRAGMA journal_mode=WAL").Scan(&mode); err != nil || mode != "wal" {
		return fmt.Errorf("journal_mode WAL not set (%q, %v)", mode, err)
	}
	tx, err := s.w.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	var v int
	if err := tx.QueryRow("PRAGMA user_version").Scan(&v); err != nil {
		return err
	}
	switch v {
	case schemaVersion:
		return nil
	case 0:
		if _, err := tx.Exec(schema); err != nil {
			return err
		}
		if _, err := tx.Exec(fmt.Sprintf("PRAGMA user_version = %d", schemaVersion)); err != nil {
			return err
		}
		return tx.Commit()
	}
	return &CorruptError{Check: "schema", Detail: fmt.Sprintf("schema version %d is not %d", v, schemaVersion)}
}

func canonical(v any) ([]byte, error) {
	raw, err := json.Marshal(v)
	if err != nil {
		return nil, err
	}
	return contracts.Canonical(raw)
}

func digest(body []byte) string {
	sum := sha256.Sum256(body)
	return "sha256:" + hex.EncodeToString(sum[:])
}

// eventID is derived from task and sequence, so recomputing a chain needs no
// random input.
func eventID(taskID string, seq uint64) string {
	sum := sha256.Sum256(fmt.Appendf(nil, "%s\x00%d", taskID, seq))
	return "evt-" + hex.EncodeToString(sum[:16])
}

// write runs fn as one write transaction, following the shared skeleton:
// BEGIN IMMEDIATE takes the write lock, the clock is sampled once after it,
// and a clock behind anything already recorded aborts before any write. Any
// error rolls everything back, expiry included.
func (s *Store) write(fn func(tx *sql.Tx, now uint64) error) error {
	if err := s.at("before-begin"); err != nil {
		return err
	}
	tx, err := s.w.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	now := s.clock()
	var high sql.NullInt64
	if err := tx.QueryRow(`SELECT max(m) FROM (SELECT max(observed_at) AS m FROM events UNION ALL SELECT max(recorded_at) FROM config_snapshots)`).Scan(&high); err != nil {
		return err
	}
	if now == 0 || now > 253402300799 {
		return refuse("CLOCK_INVALID", "trusted clock is outside Unix seconds 1..253402300799")
	}
	if high.Valid && now < uint64(high.Int64) {
		return refuse("CLOCK_REGRESSION", "trusted clock is behind the latest recorded time; nothing was written")
	}
	if err := fn(tx, now); err != nil {
		return err
	}
	if err := s.at("before-commit"); err != nil {
		return err
	}
	return tx.Commit()
}

func (s *Store) at(stage string) error {
	if s.hook == nil {
		return nil
	}
	return s.hook(stage)
}

// terms is the per-task snapshot of trusted settings that are not Policy's.
type terms struct {
	TaskID                  string           `json:"task_id"`
	ProfileState            string           `json:"profile_state"`    // present | absent
	CredentialState         string           `json:"credential_state"` // present | absent
	BudgetInitial           contracts.Amount `json:"budget_initial"`
	AssetID                 string           `json:"asset_id"`
	ApprovalDeadlineSeconds uint64           `json:"approval_deadline_seconds"`
}

func (t terms) validate(cfg contracts.RegistryConfig, p *policy.Profile) error {
	budget, err := t.BudgetInitial.Units()
	switch {
	case !slices.Contains([]string{"present", "absent"}, t.ProfileState) || !slices.Contains([]string{"present", "absent"}, t.CredentialState):
		return errors.New("profile_state and credential_state must be present or absent")
	case err != nil:
		return errors.New("budget_initial must be an amount")
	case t.ApprovalDeadlineSeconds == 0:
		return errors.New("approval_deadline_seconds must be positive")
	case !slices.ContainsFunc(cfg.Assets, func(a contracts.Asset) bool { return a.AssetID == t.AssetID }):
		return errors.New("asset_id is not a trusted registry asset")
	case (p != nil) != (t.ProfileState == "present"):
		return errors.New("profile_state does not match the profile")
	}
	if p != nil {
		limit, _ := p.TaskCap.Units()
		if budget > limit || t.AssetID != p.AssetID {
			return errors.New("budget_initial must be within the task cap, in the profile's asset")
		}
	}
	return nil
}

// TaskSetup is the trusted host's configuration for one task. A missing
// profile or credential must be declared, never inferred from a nil.
type TaskSetup struct {
	TaskID                  string
	Profile                 *policy.Profile
	ProfileAbsent           bool
	Credential              *policy.Credential
	CredentialAbsent        bool
	BudgetInitial           contracts.Amount // "" = the profile's task cap
	AssetID                 string           // "" = the profile's asset
	ApprovalDeadlineSeconds uint64           // 0 = 30
}

func (s *Store) snapshot(tx *sql.Tx, kind string, v any, now uint64) (string, error) {
	body, err := canonical(v)
	if err != nil {
		return "", err
	}
	h := digest(body)
	_, err = tx.Exec(`INSERT INTO config_snapshots VALUES (?, ?, ?, ?) ON CONFLICT (snapshot_hash) DO NOTHING`, h, kind, string(body), now)
	return h, err
}
