// Package store is B1-05's persistence: one SQLite database holds the trusted
// config snapshots, purchases, decisions, each task's event chain and the task
// projection, and every change is one transaction. The design, including the
// order of every check, is docs/persistence-design.md.
//
// The Store never logs. Errors carry a code and fixed text, never request
// content.
package store

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/binary"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"time"

	"github.com/SIMPLYBOYS/agent-payment-authority/internal/contracts"
	"github.com/SIMPLYBOYS/agent-payment-authority/internal/credential"
	"github.com/SIMPLYBOYS/agent-payment-authority/internal/paycontract"
	"github.com/SIMPLYBOYS/agent-payment-authority/internal/policy"
	_ "modernc.org/sqlite" // CGo-free SQLite; pinned with modernc.org/libc in go.mod
)

// 1: tasks, purchases, events, snapshots; 2 adds host_runs
// (docs/demo-operation-design.md section 13); 3 adds the contract version of
// tasks and runs, delegations, approval sessions and grants
// (docs/commercial-transaction-flow.md section 10); 4 adds payment_attempts
// (docs/commercial-p1b-design.md 6.4); 5 lets grants mirror a
// Signer-verified webauthn decision and adds approval_attempts
// (docs/commercial-human-approval-design.md 6.2); 6 adds apa.pay.v2 (C2b);
// 7 adds P2's payments and receipts (docs/commercial-p2-host.md 4).
const schemaVersion = 7
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
	// UpgradeDB lets Open migrate a schema 1, 2, 3 or 4 database to 5 (demo
	// --upgrade-db): verify as the old version, back up with VACUUM INTO,
	// migrate, verify as 4. Without it an older database is refused and
	// left untouched.
	UpgradeDB bool
	// Credentials is the credential directory (--credential-dir) whose
	// status list apa.pay.v2 verifications read, and VerifierID names this
	// Host in each presentation's nonce. Without them no v2 task starts.
	Credentials *credential.Dir
	VerifierID  string
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

// CorruptError is a failed recovery check on stored content. The Store then
// refuses service. Err, when set, is the underlying decode or validation error.
type CorruptError struct {
	Check, TaskID, Detail string
	Err                   error
}

func (e *CorruptError) Error() string {
	return fmt.Sprintf("store corrupt: %s check failed for task %q: %s", e.Check, e.TaskID, e.Detail)
}
func (e *CorruptError) Unwrap() error { return e.Err }

// ReadError is a database read that failed (a query, a row or the end of a
// result set), so the Store cannot tell whether what it would have checked is
// sound. Recovery refuses service on it, as on corruption, without claiming
// the data is damaged.
type ReadError struct {
	Check, TaskID string
	Err           error
}

func (e *ReadError) Error() string {
	return fmt.Sprintf("store read failed during the %s check for task %q: %v", e.Check, e.TaskID, e.Err)
}
func (e *ReadError) Unwrap() error { return e.Err }

func readErr(check, task string, err error) error {
	var re *ReadError
	if errors.As(err, &re) {
		return err
	}
	return &ReadError{Check: check, TaskID: task, Err: err}
}

// drained reports a result set's iteration error together with its close
// error: rows.Next returning false ends the loop on EOF and on a failed read
// alike, and only Err tells them apart.
func drained(rows *sql.Rows) error { return errors.Join(rows.Err(), rows.Close()) }

// openReader opens the recovery and query handle. Tests replace it to inject
// read failures; nothing outside the package can.
var openReader = func(dsn string) (*sql.DB, error) { return sql.Open("sqlite", dsn) }

// upgradeHook runs at each step of an upgrade (tests only; nil in
// production): "verified<old>", "backed-up", "migrate" (before commit),
// "verified4". An error fails that step as a real failure would.
var upgradeHook func(stage string) error

func upgradeAt(stage string) error {
	if upgradeHook == nil {
		return nil
	}
	return upgradeHook(stage)
}

type Store struct {
	path     string // the database file, absolute
	w, r     *sql.DB
	registry *contracts.Registry
	legacy   bool // reading a schema 1 or 2 file (verify before an upgrade, a history snapshot): tasks have no contract_version column
	regBody  []byte
	offers   map[OfferKey][][]byte // canonical quotes
	clock    Clock
	creds    *credential.Dir
	verifier string
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
	s := &Store{path: path, registry: reg, regBody: regBody, offers: map[OfferKey][][]byte{}, clock: cfg.Clock, creds: cfg.Credentials, verifier: cfg.VerifierID}
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
	// The version decides before any SQLite connection touches the file: a
	// writable connection would replay and, on close, checkpoint a leftover
	// WAL, so a refused schema 1 file would not be left as it was.
	v, err := fileVersion(path)
	if err != nil {
		return nil, err
	}
	switch {
	case v >= 1 && v < schemaVersion && !cfg.UpgradeDB:
		return nil, upgradeRequired(path, v)
	case v < 0 || v > schemaVersion:
		return nil, &CorruptError{Check: "schema", Detail: fmt.Sprintf("schema version %d is not %d", v, schemaVersion)}
	}
	// Each _pragma runs on every new connection (driver: applyQueryParams in
	// newConn), so no pooled connection misses one.
	const common = "?_pragma=busy_timeout(5000)&_pragma=foreign_keys(1)&_pragma=synchronous(FULL)"
	if s.w, err = sql.Open("sqlite", path+common+"&_txlock=immediate"); err != nil {
		return nil, err
	}
	s.w.SetMaxOpenConns(1)
	if s.r, err = openReader(path + common + "&_pragma=query_only(1)"); err != nil {
		s.w.Close()
		return nil, err
	}
	if err := s.migrate(cfg.UpgradeDB); err != nil {
		s.Close()
		return nil, err
	}
	if err := s.verify(schemaVersion); err != nil {
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

// fileVersion is the schema version of the database at path, read without
// SQLite opening it: from the header (user_version, bytes 60-63) when no
// WAL waits to be replayed, otherwise from a throwaway copy of the database
// and its WAL (whose frames may hold a newer header). 0 for a missing or
// empty file.
func fileVersion(path string) (int, error) {
	f, err := os.Open(path)
	if errors.Is(err, os.ErrNotExist) {
		return 0, nil
	} else if err != nil {
		return 0, err
	}
	defer f.Close()
	h := make([]byte, 100)
	n, err := io.ReadFull(f, h)
	if n == 0 && (err == io.EOF || err == io.ErrUnexpectedEOF) {
		return 0, nil
	}
	if err != nil || string(h[:16]) != "SQLite format 3\x00" {
		return 0, &CorruptError{Check: "schema", Detail: "not an SQLite database"}
	}
	if fi, err := os.Stat(path + "-wal"); err != nil || fi.Size() == 0 {
		return int(int32(binary.BigEndian.Uint32(h[60:64]))), nil
	}
	tmp, err := os.MkdirTemp("", "apa-version-")
	if err != nil {
		return 0, err
	}
	defer os.RemoveAll(tmp)
	cp := filepath.Join(tmp, "x.db")
	if err := copyFile(path, cp); err != nil {
		return 0, err
	}
	if err := copyFile(path+"-wal", cp+"-wal"); err != nil {
		return 0, err
	}
	db, err := sql.Open("sqlite", cp)
	if err != nil {
		return 0, err
	}
	defer db.Close()
	var v int
	err = db.QueryRow(`PRAGMA user_version`).Scan(&v)
	return v, err
}

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

// runsSchema is schema 2's addition: the demo host's run coordination
// (docs/demo-operation-design.md sections 13.2, 13.8, 13.9). It never holds
// events and takes no part in a task's status. The binding and scenario are
// the trusted values at acceptance, so nothing here needs the current
// registry. The CHECKs give each state's shape; the lists of codes and the
// combinations are checked in Go (validRun), on write and on open.
const runsSchema = `
CREATE TABLE host_runs (
  run_id          TEXT PRIMARY KEY,
  request_id      TEXT NOT NULL UNIQUE,
  scenario        TEXT NOT NULL,
  scenario_digest TEXT NOT NULL,
  budget_initial  TEXT NOT NULL,
  task_id         TEXT NOT NULL UNIQUE,
  consumer_id     TEXT NOT NULL,
  agent_id        TEXT NOT NULL,
  wallet_id       TEXT NOT NULL,
  payer           TEXT NOT NULL,
  state           TEXT NOT NULL CHECK (state IN ('accepted', 'interrupted', 'ended')),
  accepted_at     INTEGER NOT NULL,
  interrupted_at  INTEGER,
  ended_at        INTEGER,
  outcome         TEXT,
  detail          TEXT,
  task_started    INTEGER CHECK (task_started IN (0, 1)),
  store_end       TEXT CHECK (store_end IN ('', 'TASK_CANCELLED', 'TASK_TIMED_OUT')),
  store_end_error TEXT,
  CHECK ((state = 'interrupted') = (interrupted_at IS NOT NULL)),
  CHECK ((state = 'ended') = (ended_at IS NOT NULL)),
  CHECK (state = 'ended' OR (outcome IS NULL AND detail IS NULL AND task_started IS NULL AND store_end IS NULL AND store_end_error IS NULL)),
  CHECK (state <> 'ended' OR (outcome IS NOT NULL AND detail IS NOT NULL AND task_started IS NOT NULL AND store_end IS NOT NULL))
) STRICT;
`

// schema3 is schema 3's addition (docs/commercial-transaction-flow.md
// section 10): the contract version of every task and run (existing rows
// are apa.m1.v2), the delegation a run starts with, and the apa.pay.v1
// delegations, approval sessions and grants. The CHECKs give each row's
// shape; the contract rules are checked in Go on write and on open.
const schema3 = `
ALTER TABLE tasks ADD COLUMN contract_version TEXT NOT NULL DEFAULT 'apa.m1.v2' CHECK (contract_version IN ('apa.m1.v2', 'apa.pay.v1'));
CREATE TABLE delegations (
  delegation_id TEXT PRIMARY KEY,
  request_id    TEXT NOT NULL UNIQUE,
  consumer_id   TEXT NOT NULL,
  body          TEXT NOT NULL,
  confirmed_at  INTEGER NOT NULL,
  expires_at    INTEGER NOT NULL,
  task_id       TEXT UNIQUE REFERENCES tasks,
  bound_at      INTEGER,
  CHECK ((task_id IS NULL) = (bound_at IS NULL))
) STRICT;
ALTER TABLE host_runs ADD COLUMN contract_version TEXT NOT NULL DEFAULT 'apa.m1.v2' CHECK (contract_version IN ('apa.m1.v2', 'apa.pay.v1'));
ALTER TABLE host_runs ADD COLUMN delegation_id TEXT REFERENCES delegations;
CREATE TABLE approval_sessions (
  approval_session_id TEXT PRIMARY KEY,
  task_id             TEXT NOT NULL,
  purchase_order_id   TEXT NOT NULL,
  consumer_id         TEXT NOT NULL,
  intent_hash         TEXT NOT NULL,
  opened_at           INTEGER NOT NULL,
  expires_at          INTEGER NOT NULL,
  state               TEXT NOT NULL CHECK (state IN ('pending', 'approved', 'denied', 'expired', 'withdrawn', 'superseded')),
  resolve_request_id  TEXT UNIQUE,
  resolved_by         TEXT,
  closed_at           INTEGER,
  UNIQUE (task_id, purchase_order_id),
  FOREIGN KEY (task_id, purchase_order_id) REFERENCES purchases,
  CHECK ((state = 'pending') = (closed_at IS NULL)),
  CHECK ((state IN ('approved', 'denied')) = (resolve_request_id IS NOT NULL)),
  CHECK ((resolve_request_id IS NULL) = (resolved_by IS NULL))
) STRICT;
CREATE TABLE grants (
  jti                  TEXT PRIMARY KEY,
  approval_session_id  TEXT NOT NULL UNIQUE REFERENCES approval_sessions,
  intent_hash          TEXT NOT NULL,
  authorization_method TEXT NOT NULL CHECK (authorization_method = 'simulated'),
  body                 TEXT NOT NULL,
  issued_at            INTEGER NOT NULL
) STRICT;
`

func upgradeRequired(path string, v int) error {
	return refuse("SCHEMA_UPGRADE_REQUIRED", fmt.Sprintf("database %s is schema %d and this version needs schema %d: use a new APA_DB_PATH, or back it up and start with --upgrade-db; nothing was changed", path, v, schemaVersion))
}

// migrate sets WAL (a property of the file) and brings the schema to the
// current version, never down. A new file gets schema 6 in one transaction.
// A schema 1 to 5 file is refused untouched unless upgrade is set; then
// upgradeFrom runs. Any other version is refused.
func (s *Store) migrate(upgrade bool) error {
	var v int
	if err := s.w.QueryRow("PRAGMA user_version").Scan(&v); err != nil {
		return err
	}
	switch v {
	case schemaVersion:
	case 0:
	case 1, 2, 3, 4, 5, 6:
		if !upgrade {
			return upgradeRequired(s.path, v)
		}
	default:
		return &CorruptError{Check: "schema", Detail: fmt.Sprintf("schema version %d is not %d", v, schemaVersion)}
	}
	var mode string
	if err := s.w.QueryRow("PRAGMA journal_mode=WAL").Scan(&mode); err != nil || mode != "wal" {
		return fmt.Errorf("journal_mode WAL not set (%q, %v)", mode, err)
	}
	switch v {
	case 1, 2, 3, 4, 5, 6:
		return s.upgradeFrom(v)
	case 0:
		return s.schemaTx(func(tx *sql.Tx) error {
			if err := tx.QueryRow("PRAGMA user_version").Scan(&v); err != nil || v != 0 {
				return fmt.Errorf("schema changed while opening (%d, %v)", v, err)
			}
			if _, err := tx.Exec(schema + runsSchema + schema3 + schema4 + schema5); err != nil {
				return err
			}
			if err := schema6(tx); err != nil {
				return err
			}
			if _, err := tx.Exec(schema7); err != nil {
				return err
			}
			_, err := tx.Exec(fmt.Sprintf("PRAGMA user_version = %d", schemaVersion))
			return err
		})
	}
	return nil
}

// schemaTx runs a schema change with foreign keys off: schema 6 rebuilds
// tables that other tables reference (tasks, config_snapshots), and DROP
// TABLE would otherwise delete through them. PRAGMA foreign_keys is a
// no-op inside a transaction and the writer's DSN turns it on for every
// new connection, so one connection is pinned, the pragma is set and read
// back before BEGIN, foreign_key_check must be empty before COMMIT, and
// the pragma is restored after (docs/commercial-c2-design.md 8).
func (s *Store) schemaTx(f func(*sql.Tx) error) error {
	ctx := context.Background()
	conn, err := s.w.Conn(ctx)
	if err != nil {
		return err
	}
	defer conn.Close()
	if _, err := conn.ExecContext(ctx, "PRAGMA foreign_keys = OFF"); err != nil {
		return err
	}
	defer conn.ExecContext(ctx, "PRAGMA foreign_keys = ON")
	var fk int
	if err := conn.QueryRowContext(ctx, "PRAGMA foreign_keys").Scan(&fk); err != nil || fk != 0 {
		return fmt.Errorf("schema change: foreign keys still on (%d, %v)", fk, err)
	}
	tx, err := conn.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if err := f(tx); err != nil {
		return err
	}
	rows, err := tx.Query("PRAGMA foreign_key_check")
	if err != nil {
		return err
	}
	broken := rows.Next()
	if err := drained(rows); err != nil {
		return err
	}
	if broken {
		return errors.New("schema change: foreign_key_check reports a broken reference")
	}
	return tx.Commit()
}

// schema6 is schema 6's change (C2b, docs/commercial-c2-design.md 8): the
// BBS+ credential snapshot kind and contract apa.pay.v2 in three CHECKs
// (SQLite cannot change a CHECK: each table is rebuilt from its own stored
// definition, so columns added by earlier ALTERs come along), then the
// challenges, the verification records and the status list versions seen.
func schema6(tx *sql.Tx) error {
	if err := rebuildChecks(tx, schema6Checks, false); err != nil {
		return err
	}
	_, err := tx.Exec(schema6Tables)
	return err
}

var schema6Checks = [][3]string{
	{"config_snapshots", "kind IN ('registry', 'profile', 'credential', 'terms')", "kind IN ('registry', 'profile', 'credential', 'terms', 'credential_bbs')"},
	{"tasks", "contract_version IN ('apa.m1.v2', 'apa.pay.v1')", "contract_version IN ('apa.m1.v2', 'apa.pay.v1', 'apa.pay.v2')"},
	{"host_runs", "contract_version IN ('apa.m1.v2', 'apa.pay.v1')", "contract_version IN ('apa.m1.v2', 'apa.pay.v1', 'apa.pay.v2')"},
}

// rebuildChecks replaces, in each table's stored definition, one CHECK
// text by another (reverse: the other way), copying the rows and the
// table's indexes. Each text must occur exactly once.
func rebuildChecks(tx *sql.Tx, checks [][3]string, reverse bool) error {
	for _, c := range checks {
		table, from, to := c[0], c[1], c[2]
		if reverse {
			from, to = to, from
		}
		var ddl string
		if err := tx.QueryRow(`SELECT sql FROM sqlite_master WHERE type = 'table' AND name = ?`, table).Scan(&ddl); err != nil {
			return fmt.Errorf("rebuild %s: %w", table, err)
		}
		// A table renamed by an earlier rebuild is stored with its name quoted.
		head := "CREATE TABLE " + table + " ("
		if q := `CREATE TABLE "` + table + `" (`; strings.HasPrefix(ddl, q) {
			head = q
		}
		if !strings.HasPrefix(ddl, head) || strings.Count(ddl, from) != 1 {
			return fmt.Errorf("rebuild %s: unexpected definition", table)
		}
		rows, err := tx.Query(`SELECT sql FROM sqlite_master WHERE tbl_name = ? AND type IN ('index', 'trigger') AND sql IS NOT NULL`, table)
		if err != nil {
			return err
		}
		var extra []string
		for rows.Next() {
			var x string
			if err := rows.Scan(&x); err != nil {
				rows.Close()
				return err
			}
			extra = append(extra, x)
		}
		if err := drained(rows); err != nil {
			return err
		}
		tmp := table + "_rebuild"
		stmts := []string{
			"CREATE TABLE " + tmp + " (" + strings.Replace(strings.TrimPrefix(ddl, head), from, to, 1),
			"INSERT INTO " + tmp + " SELECT * FROM " + table,
			"DROP TABLE " + table,
			"ALTER TABLE " + tmp + " RENAME TO " + table,
		}
		for _, q := range append(stmts, extra...) {
			if _, err := tx.Exec(q); err != nil {
				return fmt.Errorf("rebuild %s: %w", table, err)
			}
		}
	}
	return nil
}

const schema6Tables = `
CREATE TABLE credential_challenges (
  challenge_id      TEXT PRIMARY KEY,
  task_id           TEXT NOT NULL REFERENCES tasks,
  purchase_order_id TEXT NOT NULL,
  intent_hash       TEXT NOT NULL,
  disclose          TEXT NOT NULL,
  verifier_id       TEXT NOT NULL,
  issued_at         INTEGER NOT NULL,
  expires_at        INTEGER NOT NULL,
  state             TEXT NOT NULL CHECK (state IN ('issued', 'used', 'expired'))
) STRICT;
CREATE UNIQUE INDEX one_issued_challenge ON credential_challenges (task_id, purchase_order_id) WHERE state = 'issued';
CREATE TABLE credential_verifications (
  task_id               TEXT NOT NULL,
  purchase_order_id     TEXT NOT NULL,
  challenge_id          TEXT REFERENCES credential_challenges,
  result                TEXT NOT NULL CHECK (result IN ('valid', 'absent', 'invalid', 'status_unavailable', 'revoked', 'expired')),
  detail                TEXT NOT NULL,
  issuer                TEXT NOT NULL,
  holder                TEXT NOT NULL,
  status_index          TEXT NOT NULL,
  expiry                INTEGER NOT NULL,
  status_list_version   INTEGER NOT NULL,
  status_list_issued_at INTEGER NOT NULL,
  status_list_sha256    TEXT NOT NULL,
  proof_sha256          TEXT NOT NULL,
  verifier              TEXT NOT NULL,
  verified_at           INTEGER NOT NULL,
  PRIMARY KEY (task_id, purchase_order_id),
  FOREIGN KEY (task_id, purchase_order_id) REFERENCES purchases
) STRICT;
CREATE TABLE credential_status_seen (
  issuer           TEXT PRIMARY KEY,
  max_list_version INTEGER NOT NULL,
  sha256           TEXT NOT NULL
) STRICT;
`

// upgradeFrom is --upgrade-db (section 13.8 R2, kept for schemas 3 to 6): verify
// as the old version (read only), back up with VACUUM INTO, add what each
// later version adds in one transaction, verify as schema 5. A failure
// before the commit leaves the old version and keeps the backup; a failure
// of the last verify leaves schema 5 and Open serves nothing: restore the
// backup to a NEW path (section 13.14.1). Hook stages: verified<old>,
// backed-up, migrate, verified<new>.
func (s *Store) upgradeFrom(from int) error {
	if err := s.verify(from); err != nil {
		return err
	}
	if err := upgradeAt(fmt.Sprintf("verified%d", from)); err != nil {
		return err
	}
	backup := fmt.Sprintf("%s.schema%d-%s.bak", s.path, from, time.Now().UTC().Format("20060102T150405Z"))
	if _, err := s.w.Exec(`VACUUM INTO ?`, backup); err != nil {
		return fmt.Errorf("backup %s before the upgrade: %w; nothing was changed", backup, err)
	}
	if err := os.Chmod(backup, 0o600); err != nil {
		return err
	}
	if err := upgradeAt("backed-up"); err != nil {
		return err
	}
	err := s.schemaTx(func(tx *sql.Tx) error {
		var v int
		if err := tx.QueryRow("PRAGMA user_version").Scan(&v); err != nil || v != from {
			return fmt.Errorf("schema changed during the upgrade (%d, %v)", v, err)
		}
		ddl := map[int]string{1: runsSchema + schema3 + schema4 + schema5, 2: schema3 + schema4 + schema5, 3: schema4 + schema5, 4: schema5, 5: "", 6: ""}[from]
		if _, err := tx.Exec(ddl); err != nil {
			return err
		}
		if from < 6 {
			if err := schema6(tx); err != nil {
				return err
			}
		}
		if _, err := tx.Exec(schema7); err != nil {
			return err
		}
		if _, err := tx.Exec(fmt.Sprintf("PRAGMA user_version = %d", schemaVersion)); err != nil {
			return err
		}
		return upgradeAt("migrate")
	})
	if err != nil {
		return err
	}
	if err := s.verify(schemaVersion); err != nil {
		return fmt.Errorf("schema %d check failed after the upgrade (backup %s; restore it to a new path): %w", schemaVersion, backup, err)
	}
	return upgradeAt(fmt.Sprintf("verified%d", schemaVersion))
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
	// ContractVersion is the trusted catalog's ("" = apa.m1.v2). An
	// apa.pay.v1 task needs its confirmed DelegationID; the delegation then
	// sets the budget, allow_max, merchants and approval deadline, within
	// the profile (docs/commercial-transaction-flow.md section 10).
	ContractVersion string
	DelegationID    string
	// BBS is an apa.pay.v2 task's issued credential (Credential stays nil;
	// CredentialAbsent declares a consumer without a registered holder).
	BBS *BBSCredential
	// Run is a pay task's run conditions (E1-a, docs/commercial-e1a.md 4.1),
	// recorded in its TASK_STARTED; nil keeps its events fixture/host, as
	// before E1-a. Never an M1 task's.
	Run *paycontract.RunConditions
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
