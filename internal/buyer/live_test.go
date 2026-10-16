package buyer

import (
	"context"
	"os"
	"slices"
	"strings"
	"testing"
)

// The shared live settings: the pin and the auth list are exactly what
// Aaron approved; the version check takes the approved output only, and its
// error never repeats the CLI's text. Every live entry point uses these.
func TestCheckLiveVersion(t *testing.T) {
	if ApprovedCLIVersion != "2.1.301 (Claude Code)" || !slices.Equal(LiveAcceptAuth, []string{"none"}) {
		t.Fatalf("live settings %q %q", ApprovedCLIVersion, LiveAcceptAuth)
	}
	env := CLIEnv(os.Getenv)
	ok := script(t, `echo '`+ApprovedCLIVersion+`'`)
	if err := CheckLiveVersion(context.Background(), ok, env, ApprovedCLIVersion); err != nil {
		t.Fatalf("the approved version: %v", err)
	}
	other := script(t, `echo '2.1.294 (Claude Code) CLITEXT7f3'`)
	err := CheckLiveVersion(context.Background(), other, env, ApprovedCLIVersion)
	if err == nil {
		t.Fatal("another version accepted")
	}
	if strings.Contains(err.Error(), "CLITEXT7f3") || strings.Contains(err.Error(), "2.1.294") {
		t.Errorf("the error repeats the CLI's text: %v", err)
	}
	if err := CheckLiveVersion(context.Background(), script(t, `exit 3`), env, ApprovedCLIVersion); err == nil {
		t.Error("a failing --version accepted")
	}
}
