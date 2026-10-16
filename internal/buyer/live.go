package buyer

import (
	"context"
	"fmt"
	"strings"
	"time"
)

// The live run settings, shared by every live entry point (apa-buyer run
// --live, probe-init, and the demo host's live mode, docs/cli-poc.md 55) so
// none of them can carry a looser copy.

// ApprovedCLIVersion is the `claude --version` output a live run requires
// (2.1.287 recorded 2026-10-02; Aaron re-approved 2.1.288 on 2026-10-03,
// 2.1.289 on 2026-10-04 and 2.1.293 on 2026-10-08, each after a local
// update; 2.1.293 is the version Aaron gave, not one this repo queried;
// 2.1.301 on 2026-10-17, 「我想直接從 2.1.301 重新開始」, after the updater
// had removed 2.1.293, re-qualified by an init probe before any model run).
// Another version stops the run; nothing upgrades.
const ApprovedCLIVersion = "2.1.301 (Claude Code)"

// EvidenceCLIVersions are the versions a live evidence bundle may name:
// each was the approved one when bundles were made (M1's live bundle is
// 2.1.293's). A new live run still needs ApprovedCLIVersion itself.
var EvidenceCLIVersions = []string{"2.1.293 (Claude Code)", ApprovedCLIVersion}

// LiveAcceptAuth is the apiKeySource values a live init may state, for
// every live run, strict or the dev demo exception (Aaron 2026-10-08, L1-d;
// docs/live-purchase-execution-decision.md 9.3). Only "none". The value
// alone proves no login or billing: the basis is that the live environment
// carries no API key or token (CLIEnv) and the CLI uses the APA-only
// config directory, which Aaron confirmed is his own claude.ai
// subscription login (docs/cli-poc.md 48.1). Nothing widens the list.
var LiveAcceptAuth = []string{"none"}

// VersionLimits bound a --version check.
var VersionLimits = Limits{Stdout: 4 << 10, Stderr: 4 << 10, Total: 5 * time.Second, Grace: time.Second}

// CheckLiveVersion requires the approved version exactly, under the
// runner's rules.
func CheckLiveVersion(ctx context.Context, path string, env []string, want string) error {
	out, err := Version(ctx, path, env, VersionLimits)
	if err != nil {
		return fmt.Errorf("version check of %s failed (%v); nothing was started", path, err)
	}
	if got := strings.TrimSpace(out); got != want {
		// The CLI's output is not repeated: it is CLI text.
		return fmt.Errorf("%s reports a version other than the approved %q; nothing was started or upgraded", path, want)
	}
	return nil
}
