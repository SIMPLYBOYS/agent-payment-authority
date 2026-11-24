package shtest

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

// The warm-up leaves no trace of body; a run without APA_WARM runs body.
func TestScript(t *testing.T) {
	dir := t.TempDir()
	ran := filepath.Join(dir, "ran")
	p := filepath.Join(dir, "cli")
	Script(t, p, "echo x >> '"+ran+"'\necho done\n", 0o755)
	if _, err := os.Stat(ran); !os.IsNotExist(err) {
		t.Fatal("the warm-up ran the body")
	}
	out, err := exec.Command(p).Output()
	if err != nil || string(out) != "done\n" {
		t.Fatalf("the script: %q %v", out, err)
	}
	if raw, _ := os.ReadFile(ran); string(raw) != "x\n" {
		t.Fatalf("the body ran %q", raw)
	}
}
