package store

import (
	"database/sql"
	"errors"
	"fmt"
	"os"
	"path/filepath"
)

// OpenSnapshot opens a recorded Store file for reading only, for the demo's
// history replay of a recorded run (docs/cli-poc.md 54). The file (and its
// WAL, if any) is copied into a new private temporary directory, checked
// unchanged while it was copied, and the copy is verified like any Store
// (hash chains, projections, snapshots); only the copy is ever opened, so
// the given file gets no lock, WAL, expiry event or any other write.
//
// The Store it returns answers ReadTask, ListTasks and LastObservedAt only:
// it has no writer, clock, registry or offers, and runs no expiry. done
// closes it and removes the copy.
func OpenSnapshot(path string) (s *Store, done func() error, err error) {
	abs, err := filepath.Abs(path)
	if err != nil {
		return nil, nil, err
	}
	if fi, err := os.Lstat(abs); err != nil || !fi.Mode().IsRegular() {
		return nil, nil, fmt.Errorf("history database %s is not a regular file", abs)
	}
	files := []string{abs, abs + "-wal", abs + "-shm"}
	before := make([]fileStamp, len(files))
	for i, f := range files {
		if before[i], err = fingerprintOf(f); err != nil {
			return nil, nil, err
		}
	}
	tmp, err := os.MkdirTemp("", "apa-history-") // 0700
	if err != nil {
		return nil, nil, err
	}
	defer func() {
		if err != nil {
			os.RemoveAll(tmp)
		}
	}()
	cp := filepath.Join(tmp, "history.db")
	if err := copyFile(abs, cp); err != nil {
		return nil, nil, err
	}
	if before[1].exists { // -shm is an index SQLite rebuilds from the WAL: not copied
		if err := copyFile(abs+"-wal", cp+"-wal"); err != nil {
			return nil, nil, err
		}
	}
	for i, f := range files {
		after, err := fingerprintOf(f)
		if err != nil {
			return nil, nil, err
		}
		if after != before[i] {
			return nil, nil, fmt.Errorf("%s changed while it was copied; refused", f)
		}
	}
	db, err := sql.Open("sqlite", cp+"?_pragma=busy_timeout(5000)&_pragma=query_only(1)")
	if err != nil {
		return nil, nil, err
	}
	var v int
	if err := db.QueryRow(`PRAGMA user_version`).Scan(&v); err != nil {
		db.Close()
		return nil, nil, err
	}
	if v != 2 {
		db.Close()
		return nil, nil, &CorruptError{Check: "schema", Detail: fmt.Sprintf("schema version %d is not 2", v)}
	}
	s = &Store{path: cp, r: db}
	if err := s.verify(v); err != nil {
		db.Close()
		return nil, nil, err
	}
	return s, func() error { return errors.Join(db.Close(), os.RemoveAll(tmp)) }, nil
}

// LastObservedAt is the observed_at of the latest stored event, 0 when
// there is none: the history replay's frozen "now".
func (s *Store) LastObservedAt() (uint64, error) {
	var at uint64
	err := s.r.QueryRow(`SELECT coalesce(max(observed_at), 0) FROM events`).Scan(&at)
	return at, err
}
