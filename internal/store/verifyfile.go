package store

// VerifyFile and RestoreCopy (docs/demo-operation-design.md sections
// 13.11.2, 13.13 and 13.14.1). SQLite never opens the source: its bytes are
// read as plain files under the source's own lock and checked on a
// throwaway copy.
//
// <db>.lock is a cooperative lock: it binds only processes that use the same
// path and lock file. Holding it does not prove that no other SQLite writer
// exists. Verify only an offline source; stop every tool that may write it
// first.

import (
	"crypto/sha256"
	"database/sql"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"syscall"
)

// verifyFileHook runs at "copied" (after the copy, before the second
// fingerprint); restoreHook at "copy" and "lock". Tests only; nil otherwise.
var (
	verifyFileHook func(stage string) error
	restoreHook    func(stage string) error
	verifyFn       = VerifyFile // RestoreCopy's call, counted by tests
)

func hookAt(h func(string) error, stage string) error {
	if h == nil {
		return nil
	}
	return h(stage)
}

type fileStamp struct {
	exists bool
	size   int64
	mtime  int64
	sum    [32]byte
}

func fingerprintOf(path string) (fileStamp, error) {
	f, err := os.Open(path)
	if errors.Is(err, os.ErrNotExist) {
		return fileStamp{}, nil
	} else if err != nil {
		return fileStamp{}, err
	}
	defer f.Close()
	fi, err := f.Stat()
	if err != nil {
		return fileStamp{}, err
	}
	h := sha256.New()
	if _, err := io.Copy(h, f); err != nil {
		return fileStamp{}, err
	}
	fp := fileStamp{exists: true, size: fi.Size(), mtime: fi.ModTime().UnixNano()}
	copy(fp.sum[:], h.Sum(nil))
	return fp, nil
}

func copyFile(src, dst string) error {
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()
	out, err := os.OpenFile(dst, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
	if err != nil {
		return err
	}
	if _, err := io.Copy(out, in); err != nil {
		out.Close()
		return err
	}
	return out.Close()
}

// VerifyFile checks an offline database file without changing it: it takes
// the source's existing <db>.lock (never created, never another file), reads
// the database and its -wal as bytes, refuses if anything changed while
// reading, and runs the schema 1, 2 or 3 recovery check on a throwaway copy.
// It returns the schema version. It never falls back to opening the source.
func VerifyFile(path string) (int, error) {
	abs, err := filepath.Abs(path)
	if err != nil {
		return 0, err
	}
	lock, err := os.OpenFile(abs+".lock", os.O_RDWR, 0) // no O_CREATE: a missing lock refuses
	if err != nil {
		return 0, fmt.Errorf("cannot open the database's lock %s.lock (%v); refused, nothing was created", abs, errors.Unwrap(err))
	}
	defer lock.Close()
	if err := syscall.Flock(int(lock.Fd()), syscall.LOCK_EX|syscall.LOCK_NB); err != nil {
		return 0, fmt.Errorf("database %s is in use (its lock is held); refused", abs)
	}
	defer syscall.Flock(int(lock.Fd()), syscall.LOCK_UN)
	files := []string{abs, abs + "-wal", abs + "-shm"}
	before := make([]fileStamp, len(files))
	for i, f := range files {
		if before[i], err = fingerprintOf(f); err != nil {
			return 0, err
		}
	}
	if !before[0].exists {
		return 0, fmt.Errorf("database %s does not exist", abs)
	}
	tmp, err := os.MkdirTemp("", "apa-verify-")
	if err != nil {
		return 0, err
	}
	defer os.RemoveAll(tmp)
	cp := filepath.Join(tmp, "x.db")
	if err := copyFile(abs, cp); err != nil {
		return 0, err
	}
	if before[1].exists { // -shm is an index SQLite rebuilds from the WAL: not copied
		if err := copyFile(abs+"-wal", cp+"-wal"); err != nil {
			return 0, err
		}
	}
	if err := hookAt(verifyFileHook, "copied"); err != nil {
		return 0, err
	}
	for i, f := range files {
		after, err := fingerprintOf(f)
		if err != nil {
			return 0, err
		}
		if after != before[i] {
			return 0, fmt.Errorf("%s changed while it was read; the copy was not used, refused", f)
		}
	}
	db, err := sql.Open("sqlite", cp+"?_pragma=busy_timeout(5000)")
	if err != nil {
		return 0, err
	}
	defer db.Close()
	var v int
	if err := db.QueryRow(`PRAGMA user_version`).Scan(&v); err != nil {
		return 0, err
	}
	if v < 1 || v > schemaVersion {
		return v, &CorruptError{Check: "schema", Detail: fmt.Sprintf("schema version %d is not 1 to %d", v, schemaVersion)}
	}
	return v, (&Store{r: db}).verify(v)
}

// RestoreCopy makes a restore copy of backup in the new directory dir
// (section 13.14.1), stopping at the first failure: dir must not exist; the
// database is copied without overwriting; its own lock is created as a new
// file; only then is the copy verified. It returns the new database's path
// and version. A partial copy is left as it is and never reported restored.
func RestoreCopy(backup, dir string) (string, int, error) {
	if err := os.Mkdir(dir, 0o700); err != nil {
		return "", 0, fmt.Errorf("the restore directory must be new: %w; stopped", err)
	}
	db := filepath.Join(dir, "demo.db")
	for _, f := range []string{db, db + "-wal", db + "-shm", db + ".lock"} {
		if _, err := os.Lstat(f); !errors.Is(err, os.ErrNotExist) {
			return "", 0, fmt.Errorf("%s already exists; stopped, nothing overwritten", f)
		}
	}
	if err := hookAt(restoreHook, "copy"); err != nil {
		return "", 0, err
	}
	if err := copyFile(backup, db); err != nil {
		return "", 0, fmt.Errorf("copy the backup: %w; stopped, not verified", err)
	}
	if err := hookAt(restoreHook, "lock"); err != nil {
		return "", 0, err
	}
	l, err := os.OpenFile(db+".lock", os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
	if err != nil {
		return "", 0, fmt.Errorf("create the copy's lock: %w; stopped, not verified", err)
	}
	if err := l.Close(); err != nil {
		return "", 0, err
	}
	v, err := verifyFn(db)
	return db, v, err
}
