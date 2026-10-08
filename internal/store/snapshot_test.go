package store

import (
	"crypto/sha256"
	"os"
	"path/filepath"
	"slices"
	"testing"
)

// OpenSnapshot reads a recorded Store through a private copy: the given
// file and its directory are left exactly as they were (no WAL, shm, lock
// or expiry event, even for an Ask long past its deadline); reads match
// the original; done removes the copy.
func TestOpenSnapshot(t *testing.T) {
	e := newEnv(t)
	s := e.open()
	e.start(s, "task-a")
	submit(t, s, sessA, e.base) // an Ask with a deadline
	want, err := s.ReadTask("task-a", func(string) bool { return true })
	if err != nil {
		t.Fatal(err)
	}
	if err := s.Close(); err != nil {
		t.Fatal(err)
	}
	// The recorded file alone, in a directory of its own.
	dir := t.TempDir()
	rec := filepath.Join(dir, "apa-live.db")
	raw, err := os.ReadFile(e.path)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(rec, raw, 0o600); err != nil {
		t.Fatal(err)
	}
	sum := sha256.Sum256(raw)

	e.clock.Add(10 * 86400) // far past the Ask's deadline: a normal Open would expire it
	snap, done, err := OpenSnapshot(rec)
	if err != nil {
		t.Fatal(err)
	}
	// Only the recorded file next to it, while the snapshot is open (an open
	// WAL database has its -wal and -shm beside it) and after it is closed.
	alone := func(when string) {
		t.Helper()
		names, _ := os.ReadDir(dir)
		var left []string
		for _, n := range names {
			left = append(left, n.Name())
		}
		if !slices.Equal(left, []string{"apa-live.db"}) {
			t.Errorf("%s: files next to the recorded database: %v", when, left)
		}
	}
	got, err := snap.ReadTask("task-a", func(string) bool { return true })
	if err != nil || len(got.Events) != len(want.Events) || got.Task.Status != want.Task.Status || got.Task.AsOfSequence != want.Task.AsOfSequence {
		t.Fatalf("snapshot read %+v (%v), want %+v", got.Task, err, want.Task)
	}
	list, err := snap.ListTasks("", 10)
	if err != nil || len(list) != 1 || list[0].TaskID != "task-a" {
		t.Fatalf("list %+v (%v)", list, err)
	}
	last, err := snap.LastObservedAt()
	if err != nil || last != want.Events[len(want.Events)-1].ObservedAt {
		t.Fatalf("last observed %d (%v)", last, err)
	}
	alone("open")
	copyDir := filepath.Dir(snap.path)
	if err := done(); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(copyDir); !os.IsNotExist(err) {
		t.Errorf("the copy was not removed: %v", err)
	}
	after, _ := os.ReadFile(rec)
	if sha256.Sum256(after) != sum {
		t.Error("the recorded file changed")
	}
	alone("closed")

	if _, _, err := OpenSnapshot(filepath.Join(dir, "missing.db")); err == nil {
		t.Error("a missing file was opened")
	}
	// A file with a -wal or -shm beside it may still be open in a Store:
	// refused, and the side file is left as it was.
	for _, side := range []string{"-wal", "-shm"} {
		open := t.TempDir()
		p := filepath.Join(open, "apa-live.db")
		os.WriteFile(p, raw, 0o600)
		os.WriteFile(p+side, []byte("x"), 0o600)
		if _, _, err := OpenSnapshot(p); err == nil {
			t.Errorf("opened with a %s beside it", side)
		}
		if b, _ := os.ReadFile(p + side); string(b) != "x" {
			t.Errorf("the %s was changed", side)
		}
	}
	link := filepath.Join(dir, "link.db")
	os.Symlink(rec, link)
	if _, _, err := OpenSnapshot(link); err == nil {
		t.Error("a symlink was opened")
	}
	os.Remove(link)
}
