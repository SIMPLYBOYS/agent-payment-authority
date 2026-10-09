// Command verify-demo is the M1 read-only evidence checker (B1-07, fixture
// scope; docs/m1-evidence.md section 15):
//
//	verify-demo --run-dir artifacts/m1/<run-id> [--repo .] [--mode fixture]
//
// It reads the run directory and the repository's frozen case list and
// fixtures, writes a JSON report to stdout and diagnostics to stderr, and
// writes nothing else. Exit codes: 0 every required claim PASS; 1 a FAIL;
// 2 no FAIL but INCONCLUSIVE; 3 the check could not be made or its report
// not written. (make and go run report a non-zero exit as their own.)
package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"os"
	"runtime/debug"

	"github.com/SIMPLYBOYS/agent-payment-authority/internal/verification"
)

func main() {
	os.Exit(run(os.Args[1:], os.Stdout, os.Stderr))
}

func run(args []string, stdout, stderr io.Writer) (code int) {
	defer func() {
		if p := recover(); p != nil {
			fmt.Fprintf(stderr, "verify-demo: internal error: %v; nothing was verified\n", p)
			code = 3
		}
	}()
	fl := flag.NewFlagSet("verify-demo", flag.ContinueOnError)
	fl.SetOutput(stderr)
	dir := fl.String("run-dir", "", "the run directory to check (artifacts/m1/<run-id>)")
	repo := fl.String("repo", ".", "the repository holding the frozen case list and fixtures")
	mode := fl.String("mode", "fixture", "fixture (live is M1-E3's)")
	if fl.Parse(args) != nil || fl.NArg() != 0 || *dir == "" {
		fmt.Fprintln(stderr, "usage: verify-demo --run-dir DIR [--repo .] [--mode fixture]")
		return 3
	}
	tr, err := verification.LoadTrusted(*repo)
	if err != nil {
		fmt.Fprintf(stderr, "verify-demo: trusted inputs: %v; nothing was verified\n", err)
		return 3
	}
	r, err := verification.Verify(*dir, tr, *mode)
	if err != nil {
		fmt.Fprintf(stderr, "verify-demo: %v; nothing was verified\n", err)
		return 3
	}
	r.Checker.Build = build()
	body, err := json.MarshalIndent(r, "", "  ")
	if err == nil {
		_, err = stdout.Write(append(body, '\n'))
	}
	if err != nil {
		fmt.Fprintf(stderr, "verify-demo: the report could not be written: %v\n", err)
		return 3
	}
	fmt.Fprintf(stderr, "verify-demo: %s: %s (exit %d)\n", r.Input.RunID, r.Verdict, r.ExitCode())
	return r.ExitCode()
}

// build names the checker's own source, as Go recorded it.
func build() string {
	info, ok := debug.ReadBuildInfo()
	if !ok {
		return "unknown"
	}
	rev, modified := "unknown", ""
	for _, s := range info.Settings {
		switch s.Key {
		case "vcs.revision":
			rev = s.Value
		case "vcs.modified":
			if s.Value == "true" {
				modified = " (modified)"
			}
		}
	}
	return rev + modified
}
