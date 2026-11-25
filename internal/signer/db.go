package signer

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"os"
	"path/filepath"
)

// OpenDB opens the Signer's one database (P2-D1, docs/commercial-p2.md):
// the payment attempts and the human approvals live in the same SQLite
// file, so consuming an approval and creating its attempt is one
// transaction. WAL, full sync, foreign keys, immediate write locks, one
// connection.
func OpenDB(path string) (*sql.DB, error) {
	db, err := sql.Open("sqlite", path+"?_pragma=busy_timeout(5000)&_pragma=journal_mode(WAL)&_pragma=synchronous(FULL)&_pragma=foreign_keys(1)&_txlock=immediate")
	if err != nil {
		return nil, err
	}
	db.SetMaxOpenConns(1)
	return db, nil
}

// approvalTables are the human-approval tables, in an order their foreign
// keys allow copying.
var approvalTables = []string{"approver_handles", "enrollments", "credentials", "approval_challenges", "human_approvals", "delegation_confirmations", "delegation_revocations", "verify_requests", "voice_templates", "voice_results"}

// Unmigrated refuses a directory that still holds A2's separate
// approvals.db: its approvals must be moved into signer.db first
// (apa-signer approver migrate), never read from two places.
func Unmigrated(dir string) error {
	if _, err := os.Stat(filepath.Join(dir, "approvals.db")); err == nil {
		return fmt.Errorf("%s still holds approvals.db: run apa-signer approver migrate --dir %s first (P2-D1: the approvals move into signer.db)", dir, dir)
	} else if !errors.Is(err, os.ErrNotExist) {
		return err
	}
	return nil
}

// MigrateApprovals moves DIR/approvals.db's tables into DIR/signer.db in
// one transaction of signer.db, checks every table's row count, and only
// then renames approvals.db to approvals.db.migrated. The Signer must be
// stopped first (the caller checks its socket); a process with a
// transaction open on approvals.db makes it refuse, and the copy holds
// approvals.db's write lock. It refuses when signer.db already has other
// approval rows; when it has exactly approvals.db's (a migration that
// stopped before the rename), it only finishes the rename. It returns the
// rows per table.
func MigrateApprovals(ctx context.Context, dir string) (map[string]int, error) {
	old := filepath.Join(dir, "approvals.db")
	if _, err := os.Stat(old); err != nil {
		return nil, fmt.Errorf("no %s to migrate: %w", old, err)
	}
	// Fold a WAL a stopped process left into the file; a process still
	// reading or writing it makes the checkpoint busy.
	odb, err := OpenDB(old)
	if err != nil {
		return nil, err
	}
	var busy, frames, done int
	err = odb.QueryRowContext(ctx, `PRAGMA wal_checkpoint(TRUNCATE)`).Scan(&busy, &frames, &done)
	odb.Close()
	if err != nil {
		return nil, err
	} else if busy != 0 {
		return nil, fmt.Errorf("%s is in use: stop every process using it first", old)
	}
	db, err := OpenDB(filepath.Join(dir, "signer.db"))
	if err != nil {
		return nil, err
	}
	defer db.Close()
	if _, err := db.ExecContext(ctx, approvalsSchema+voiceSchema); err != nil {
		return nil, err
	}
	// Read-write: the immediate transaction below takes its write lock too.
	if _, err := db.ExecContext(ctx, `ATTACH DATABASE ? AS old`, old); err != nil {
		return nil, err
	}
	defer db.ExecContext(context.Background(), `DETACH DATABASE old`)
	moved := map[string]int{}
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	already, different := 0, false
	cols := map[string]string{} // "" when old has no such table
	for _, t := range approvalTables {
		var have, src, missing int
		if err := tx.QueryRowContext(ctx, `SELECT count(*) FROM main.`+t).Scan(&have); err != nil {
			return nil, err
		}
		cs, err := columns(ctx, tx, t)
		if err != nil {
			return nil, err
		}
		cols[t] = cs
		if cs != "" {
			if err := tx.QueryRowContext(ctx, `SELECT count(*) FROM old.`+t).Scan(&src); err != nil {
				return nil, err
			}
			if err := tx.QueryRowContext(ctx, `SELECT count(*) FROM (SELECT `+cs+` FROM old.`+t+` EXCEPT SELECT `+cs+` FROM main.`+t+`)`).Scan(&missing); err != nil {
				return nil, err
			}
		}
		already += have
		different = different || have != src || missing != 0
		moved[t] = src
	}
	if already > 0 {
		if different {
			return nil, fmt.Errorf("signer.db already has %d approval rows, not approvals.db's: nothing was moved", already)
		}
		// Every row is there already: a migration stopped before its rename.
		if err := tx.Rollback(); err != nil {
			return nil, err
		}
		if _, err := db.ExecContext(ctx, `DETACH DATABASE old`); err != nil {
			return nil, err
		}
		return moved, os.Rename(old, old+".migrated")
	}
	for _, t := range approvalTables {
		cols := cols[t]
		if cols == "" {
			continue // an approvals.db from before V1 has no voice tables
		}
		// By name: the copy never depends on the two files' column order.
		res, err := tx.ExecContext(ctx, `INSERT INTO main.`+t+` (`+cols+`) SELECT `+cols+` FROM old.`+t)
		if err != nil {
			return nil, fmt.Errorf("%s: %w", t, err)
		}
		if n, _ := res.RowsAffected(); int(n) != moved[t] {
			return nil, fmt.Errorf("%s: %d of %d rows moved", t, n, moved[t])
		}
	}
	if err := tx.Commit(); err != nil {
		return nil, err
	}
	if _, err := db.ExecContext(ctx, `DETACH DATABASE old`); err != nil {
		return nil, err
	}
	return moved, os.Rename(old, old+".migrated")
}

// columns is old.t's column list ("" when old has no such table).
func columns(ctx context.Context, tx *sql.Tx, t string) (string, error) {
	rows, err := tx.QueryContext(ctx, `SELECT name FROM pragma_table_info(?, 'old')`, t)
	if err != nil {
		return "", err
	}
	defer rows.Close()
	var out string
	for rows.Next() {
		var n string
		if err := rows.Scan(&n); err != nil {
			return "", err
		}
		if out != "" {
			out += ", "
		}
		out += `"` + n + `"`
	}
	return out, rows.Err()
}
