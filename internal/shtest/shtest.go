// Package shtest writes the shell scripts tests run in place of a CLI.
//
// macOS checks a new executable file the first time it is run. Measured on
// 2026-11-24 (docs/cli-poc.md 34.2): about a second on an idle machine and
// 9 to 14 s while the whole test suite runs; the same file's second run
// takes 10 ms, and /bin/echo is never slow. A test that writes a script and
// at once runs it under a limit of a few seconds (the version check's 5 s)
// therefore failed now and then. Script pays for that first run itself.
package shtest

import (
	"context"
	"os"
	"os/exec"
	"testing"
	"time"
)

// Script writes path as an sh script: a first line that ends it at once
// when APA_WARM=1, then body. It then runs the script once with APA_WARM=1,
// for at most a minute, so the run under test is not the file's first. The
// CLI's environment allowlist never carries APA_WARM (buyer.CLIEnv), so a
// run under test always goes on to body; the warm-up touches nothing body
// would.
func Script(t testing.TB, path, body string, mode os.FileMode) {
	t.Helper()
	if err := os.WriteFile(path, []byte("#!/bin/sh\n[ \"$APA_WARM\" = 1 ] && exit 0\n"+body), mode); err != nil {
		t.Fatal(err)
	}
	Warm(t, path)
}

// Warm runs a script Script wrote once with APA_WARM=1: for a test that
// rewrote the file, which makes it new to macOS again.
func Warm(t testing.TB, path string) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()
	warm := exec.CommandContext(ctx, path)
	warm.Env = append(os.Environ(), "APA_WARM=1")
	if err := warm.Run(); err != nil {
		t.Fatalf("the script %s does not start: %v", path, err)
	}
}
