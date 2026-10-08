package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"slices"
	"testing"
)

// Review R1 (docs/cli-poc.md 56.8): the ready file never replaces, truncates
// or writes through anything already there; on a fresh path it appears
// whole ({addr, pid}); only its own temporary file is cleaned up.
func TestWriteReadyNeverReplaces(t *testing.T) {
	state := func(p string) (string, os.FileMode, string) {
		fi, err := os.Lstat(p)
		if err != nil {
			return "", 0, ""
		}
		body, _ := os.ReadFile(p)
		target, _ := os.Readlink(p)
		return string(body), fi.Mode(), target
	}
	names := func(dir string) []string {
		entries, _ := os.ReadDir(dir)
		var out []string
		for _, e := range entries {
			out = append(out, e.Name())
		}
		return out
	}

	// An existing file at the path: refused, content and mode unchanged.
	dir := t.TempDir()
	path := filepath.Join(dir, "ready.json")
	os.WriteFile(path, []byte("someone else's"), 0o640)
	if err := writeReady(path, "127.0.0.1:1"); err == nil {
		t.Error("an existing file: no error")
	}
	if body, mode, _ := state(path); body != "someone else's" || mode.Perm() != 0o640 {
		t.Errorf("the existing file changed: %q %v", body, mode)
	}
	if got := names(dir); !slices.Equal(got, []string{"ready.json"}) {
		t.Errorf("left behind: %v", got)
	}

	// The old fixed temporary name is a symlink to a bystander: untouched.
	dir = t.TempDir()
	path = filepath.Join(dir, "ready.json")
	bystander := filepath.Join(t.TempDir(), "bystander")
	os.WriteFile(bystander, []byte("bystander"), 0o600)
	os.Symlink(bystander, path+".tmp")
	if err := writeReady(path, "127.0.0.1:2"); err != nil {
		t.Fatalf("a fresh path beside a symlinked .tmp: %v", err)
	}
	if body, mode, _ := state(bystander); body != "bystander" || mode.Perm() != 0o600 {
		t.Errorf("the bystander changed: %q %v", body, mode)
	}
	if _, _, target := state(path + ".tmp"); target != bystander {
		t.Errorf("the .tmp symlink changed: %q", target)
	}

	// The path is a symlink: refused, the link and its target unchanged.
	dir = t.TempDir()
	path = filepath.Join(dir, "ready.json")
	target := filepath.Join(t.TempDir(), "target")
	os.WriteFile(target, []byte("target"), 0o600)
	os.Symlink(target, path)
	if err := writeReady(path, "127.0.0.1:3"); err == nil {
		t.Error("a symlink at the path: no error")
	}
	if _, _, got := state(path); got != target {
		t.Errorf("the link changed: %q", got)
	}
	if body, _, _ := state(target); body != "target" {
		t.Errorf("the link's target changed: %q", body)
	}

	// A fresh path: the whole JSON, only it in the directory.
	dir = t.TempDir()
	path = filepath.Join(dir, "ready.json")
	if err := writeReady(path, "127.0.0.1:4"); err != nil {
		t.Fatal(err)
	}
	var r struct {
		Addr string `json:"addr"`
		PID  int    `json:"pid"`
	}
	raw, _ := os.ReadFile(path)
	if json.Unmarshal(raw, &r) != nil || r.Addr != "127.0.0.1:4" || r.PID != os.Getpid() {
		t.Errorf("ready %s", raw)
	}
	if got := names(dir); !slices.Equal(got, []string{"ready.json"}) {
		t.Errorf("left behind: %v", got)
	}
}
