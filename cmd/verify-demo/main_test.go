package main

import (
	"bytes"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

var golden = filepath.Join("..", "..", "testdata", "verification", "m1-fixture-20261009T162703Z-4ddb7a")

type failingWriter struct{}

func (failingWriter) Write([]byte) (int, error) { return 0, errors.New("disk full") }

// The exit codes: 0 PASS, 1 FAIL, 2 INCONCLUSIVE, 3 not checked or not
// reported; the report on stdout, diagnostics on stderr.
func TestExitCodes(t *testing.T) {
	repo := filepath.Join("..", "..")
	copyGolden := func(t *testing.T) string {
		dir := filepath.Join(t.TempDir(), filepath.Base(golden))
		filepath.Walk(golden, func(p string, info os.FileInfo, err error) error {
			rel, _ := filepath.Rel(golden, p)
			if info.IsDir() {
				return os.MkdirAll(filepath.Join(dir, rel), 0o700)
			}
			b, _ := os.ReadFile(p)
			return os.WriteFile(filepath.Join(dir, rel), b, 0o600)
		})
		return dir
	}
	failing := copyGolden(t)
	os.WriteFile(filepath.Join(failing, "stray.txt"), []byte("x"), 0o600) // a file the index does not list
	inconclusive := copyGolden(t)
	os.Remove(filepath.Join(inconclusive, "run-end.json"))
	for name, tc := range map[string]struct {
		args []string
		out  interface{ Write([]byte) (int, error) }
		code int
	}{
		"the golden bundle":      {[]string{"--run-dir", golden, "--repo", repo}, &bytes.Buffer{}, 0},
		"a FAIL":                 {[]string{"--run-dir", failing, "--repo", repo}, &bytes.Buffer{}, 1},
		"an INCONCLUSIVE":        {[]string{"--run-dir", inconclusive, "--repo", repo}, &bytes.Buffer{}, 2},
		"no run directory given": {[]string{"--repo", repo}, &bytes.Buffer{}, 3},
		"no such run directory":  {[]string{"--run-dir", filepath.Join(t.TempDir(), "none"), "--repo", repo}, &bytes.Buffer{}, 3},
		"no case list":           {[]string{"--run-dir", golden, "--repo", t.TempDir()}, &bytes.Buffer{}, 3},
		"the report not written": {[]string{"--run-dir", golden, "--repo", repo}, failingWriter{}, 3},
		"an unknown flag":        {[]string{"--run-dir", golden, "--output", "x"}, &bytes.Buffer{}, 3},
	} {
		t.Run(name, func(t *testing.T) {
			var stderr bytes.Buffer
			if code := run(tc.args, tc.out, &stderr); code != tc.code {
				t.Fatalf("exit %d, want %d: %s", code, tc.code, stderr.String())
			}
			if b, ok := tc.out.(*bytes.Buffer); ok && tc.code < 3 {
				var r struct{ Verdict string }
				if json.Unmarshal(b.Bytes(), &r) != nil || r.Verdict == "" {
					t.Fatalf("no report on stdout: %.200s", b.String())
				}
			}
			if tc.code == 3 && !strings.Contains(stderr.String(), "verify-demo") && !strings.Contains(stderr.String(), "usage") && !strings.Contains(stderr.String(), "flag") {
				t.Fatalf("no diagnostic: %s", stderr.String())
			}
		})
	}
}
