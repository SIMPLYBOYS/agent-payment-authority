package buyer

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"
)

// Route 1 (docs/live-purchase-execution-decision.md 4.2): a real CLI gets
// exactly one CLAUDE_CONFIG_DIR, <HOME>/.apa-claude of the HOME it gets,
// whatever the parent sets; a missing or relative HOME, or a config
// directory that is not a real directory, is refused. Test HOMEs only.
func TestLiveEnv(t *testing.T) {
	home := t.TempDir()
	dir := filepath.Join(home, ConfigDirName)
	if err := os.Mkdir(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	parent := map[string]string{"HOME": home, "PATH": "/bin", EnvConfigDir: "/elsewhere"}
	getenv := func(k string) string { return parent[k] }
	env, err := LiveEnv(getenv)
	if err != nil {
		t.Fatal(err)
	}
	want := append(CLIEnv(getenv), EnvConfigDir+"="+dir)
	if !slices.Equal(env, want) {
		t.Errorf("env %q, want %q", env, want)
	}
	n := 0
	for _, kv := range env {
		if strings.HasPrefix(kv, EnvConfigDir+"=") {
			n++
		}
	}
	if n != 1 {
		t.Errorf("%s set %d times", EnvConfigDir, n)
	}
	if _, err := os.Stat(dir); err != nil {
		t.Errorf("the config directory is gone: %v", err)
	}

	// A relative HOME is refused even when <HOME>/.apa-claude exists from
	// the working directory: only the absolute-path check can refuse it.
	t.Chdir(t.TempDir())
	if err := os.MkdirAll(filepath.Join("home", ConfigDirName), 0o700); err != nil {
		t.Fatal(err)
	}
	for name, h := range map[string]string{"no HOME": "", "relative HOME": "home"} {
		if _, err := LiveEnv(func(k string) string { return map[string]string{"HOME": h}[k] }); err == nil {
			t.Errorf("%s: accepted", name)
		}
	}
	for _, setup := range []string{"absent", "file", "symlink"} {
		h := t.TempDir()
		p := filepath.Join(h, ConfigDirName)
		switch setup {
		case "file":
			os.WriteFile(p, nil, 0o600)
		case "symlink":
			os.Symlink(t.TempDir(), p)
		}
		if _, err := LiveEnv(func(k string) string { return map[string]string{"HOME": h}[k] }); err == nil {
			t.Errorf("config dir %s: accepted", setup)
		}
		if setup == "absent" {
			if _, err := os.Lstat(p); err == nil {
				t.Error("LiveEnv created the config directory")
			}
		}
	}
}

// The value reaches the CLI process as the one CLAUDE_CONFIG_DIR.
func TestLiveEnvReachesChild(t *testing.T) {
	cli := script(t, `printf '%s|%s\n' "${CLAUDE_CONFIG_DIR-unset}" "$(env | grep -c '^CLAUDE_CONFIG_DIR=')"`)
	ready, cancel := context.WithTimeout(context.Background(), time.Minute) // as in TestCLIEnvReachesChild
	defer cancel()
	if err := exec.CommandContext(ready, cli).Run(); err != nil {
		t.Fatalf("the script does not start: %v", err)
	}
	home := t.TempDir()
	os.Mkdir(filepath.Join(home, ConfigDirName), 0o700)
	parent := map[string]string{"PATH": os.Getenv("PATH"), "HOME": home, EnvConfigDir: "/elsewhere"}
	env, err := LiveEnv(func(k string) string { return parent[k] })
	if err != nil {
		t.Fatal(err)
	}
	out, err := Version(context.Background(), cli, env, testLimits)
	if want := filepath.Join(home, ConfigDirName) + "|1\n"; err != nil || out != want {
		t.Fatalf("child saw %q (%v), want %q", out, err, want)
	}
}
