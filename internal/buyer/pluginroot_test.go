package buyer

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// Option A (docs/cli-poc.md section 42): every live run and every probe
// gives the CLI a plugin root of its own, new and empty. A value from the
// parent never wins, a root that cannot be made starts no CLI, and the run's
// directory, and only it, is removed however the run ends. These fakes show
// what reaches the CLI process; they cannot show that a real CLI loads no
// plugins.

// pluginScript is a CLI that writes what it sees of its plugin root to out:
// the value, how many times the variable is set, the mode and the entry count.
// The mode comes from ls -ld, which reads the same on macOS and Linux; stat's
// flags differ between the two (the 086f640 container run failed on that).
func pluginScript(t *testing.T, out string) string {
	t.Helper()
	cli := script(t, `d="${`+EnvPluginRoot+`-unset}"
n=$(env | grep -c '^`+EnvPluginRoot+`=')
m=$(ls -ld "$d" 2>/dev/null | cut -c1-10)
c=$(ls -A "$d" 2>/dev/null | wc -l | tr -d ' ')
printf '%s|%s|%s|%s\n' "$d" "$n" "$m" "$c" > '`+out+`'`)
	// Ready first, as in TestCLIEnvReachesChild: a new script can take
	// seconds to start once under a loaded test run (docs/cli-poc.md 34.2).
	ready, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()
	if err := exec.CommandContext(ready, cli).Run(); err != nil {
		t.Fatalf("the script does not start: %v", err)
	}
	os.Remove(out)
	return cli
}

// parentWithRoot is a parent environment that sets its own plugin root, both
// as an inherited variable and as an extra entry of the CLI environment.
func parentWithRoot(t *testing.T) (env []string, home, elsewhere string) {
	t.Helper()
	home, elsewhere = t.TempDir(), t.TempDir()
	if err := os.WriteFile(filepath.Join(elsewhere, "keep"), []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}
	parent := map[string]string{"PATH": os.Getenv("PATH"), "HOME": home, EnvPluginRoot: elsewhere}
	return CLIEnv(func(k string) string { return parent[k] }, EnvPluginRoot+"="+elsewhere), home, elsewhere
}

// checkSeen reads what pluginScript saw and returns the root it was given.
func checkSeen(t *testing.T, out, runPrefix, home, elsewhere string) string {
	t.Helper()
	raw, err := os.ReadFile(out)
	if err != nil {
		t.Fatalf("the CLI did not run: %v", err)
	}
	f := strings.Split(strings.TrimSpace(string(raw)), "|")
	if len(f) != 4 {
		t.Fatalf("seen %q", raw)
	}
	d := f[0]
	if f[1] != "1" || f[2] != "drwx------" || f[3] != "0" {
		t.Errorf("root %s: set %s times, mode %s, %s entries; want once, drwx------, empty", d, f[1], f[2], f[3])
	}
	if filepath.Base(d) != "plugins" || !strings.HasPrefix(filepath.Base(filepath.Dir(d)), runPrefix) ||
		d == elsewhere || strings.HasPrefix(d, home) {
		t.Errorf("root %q is not the run's own directory", d)
	}
	return d
}

func checkGone(t *testing.T, root, elsewhere string) {
	t.Helper()
	if _, err := os.Stat(root); !errors.Is(err, os.ErrNotExist) {
		t.Errorf("the run's plugin root %s is still there (%v)", root, err)
	}
	if _, err := os.Stat(filepath.Join(elsewhere, "keep")); err != nil {
		t.Errorf("a directory the run does not own was touched: %v", err)
	}
}

// The environment handed to the runner names the root once, last: os/exec
// would also keep only the last of two, so the end-to-end tests below
// cannot see a duplicate; this one does.
func TestWithPluginRoot(t *testing.T) {
	dir := t.TempDir()
	env, err := withPluginRoot([]string{"HOME=/h", EnvPluginRoot + "=/elsewhere", "PATH=/bin"}, dir)
	want := []string{"HOME=/h", "PATH=/bin", EnvPluginRoot + "=" + filepath.Join(dir, "plugins")}
	if err != nil || strings.Join(env, "\n") != strings.Join(want, "\n") {
		t.Errorf("env %q (%v)", env, err)
	}
}

func TestPluginRootLive(t *testing.T) {
	e := newHost(t)
	out := filepath.Join(t.TempDir(), "seen")
	e.h.CLI = pluginScript(t, out)
	env, home, elsewhere := parentWithRoot(t)
	e.h.Env = env
	if _, err := e.h.Run(context.Background(), "task-a", "x", e.prepare("task-a")); err != nil {
		t.Fatal(err)
	}
	checkGone(t, checkSeen(t, out, "apa-run-", home, elsewhere), elsewhere)
}

func TestPluginRootProbe(t *testing.T) {
	p := newProbe(t, "ask-mainline", 10*time.Second)
	out := filepath.Join(t.TempDir(), "seen")
	p.spec.CLI = pluginScript(t, out)
	env, home, elsewhere := parentWithRoot(t)
	p.spec.Env = env
	p.run(context.Background())
	checkGone(t, checkSeen(t, out, "apa-probe-", home, elsewhere), elsewhere)
}

// A root that cannot be made starts no CLI, and nothing falls back to the
// parent's root or to the default one.
func TestPluginRootFailureStartsNoCLI(t *testing.T) {
	defer func() { pluginMkdir = os.Mkdir }()
	pluginMkdir = func(string, os.FileMode) error { return errors.New("no plugin root") }
	marker := filepath.Join(t.TempDir(), "started")
	cli := script(t, `touch '`+marker+`'`)
	env, _, _ := parentWithRoot(t)

	e := newHost(t)
	e.h.CLI, e.h.Env = cli, env
	if _, err := e.h.Run(context.Background(), "task-a", "x", e.prepare("task-a")); err == nil {
		t.Error("live: the run went on without its plugin root")
	}
	p := newProbe(t, "ask-mainline", 10*time.Second)
	p.spec.CLI, p.spec.Env = cli, env
	if _, err := Probe(context.Background(), p.spec); err == nil {
		t.Error("probe: the run went on without its plugin root")
	}
	if _, err := os.Stat(marker); !errors.Is(err, os.ErrNotExist) {
		t.Errorf("the CLI was started (%v)", err)
	}
}

// The root goes with the run's directory when the run is cancelled before
// the CLI starts and when the CLI cannot start.
func TestPluginRootCleanup(t *testing.T) {
	defer func() { pluginMkdir = os.Mkdir }()
	missing := filepath.Join(t.TempDir(), "no-such-cli")
	for _, c := range []struct {
		name   string
		cancel bool
		cli    string
	}{{"cancelled", true, ""}, {"start failed", false, missing}} {
		for _, entry := range []string{"live", "probe"} {
			ctx, cancel := context.WithCancel(context.Background())
			var root string
			pluginMkdir = func(name string, perm os.FileMode) error {
				root = name
				if c.cancel {
					cancel()
				}
				return os.Mkdir(name, perm)
			}
			env, _, elsewhere := parentWithRoot(t)
			cli := c.cli
			if cli == "" {
				cli = script(t, "exit 0")
			}
			if entry == "live" {
				e := newHost(t)
				e.h.CLI, e.h.Env = cli, env
				e.h.Run(ctx, "task-a", "x", e.prepare("task-a"))
			} else {
				p := newProbe(t, "ask-mainline", 10*time.Second)
				p.spec.CLI, p.spec.Env = cli, env
				Probe(ctx, p.spec)
			}
			cancel()
			if root == "" {
				t.Fatalf("%s %s: no plugin root was made", c.name, entry)
			}
			checkGone(t, root, elsewhere)
		}
	}
}
