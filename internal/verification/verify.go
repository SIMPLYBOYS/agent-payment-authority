package verification

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"path/filepath"
	"regexp"
	"slices"
	"sort"
	"strings"

	"github.com/SIMPLYBOYS/agent-payment-authority/internal/contracts"
)

// e2eCases are the integration cases whose highest accepted layer is the
// demo host end to end (M1-E1 acceptance, docs/m1-evidence.md section 4);
// the other integration cases are Store layer, the rest contracts layer.
var e2eCases = []string{"ask", "deny-budget", "deny-merchant", "deny-asset", "deny-unattended", "cancel-pending", "cancel-no-purchase",
	"cli-timeout", "cli-timeout-pending", "approval-timeout", "approval-deadline-before", "approval-deadline-exact", "restart"}

func layerOf(c contracts.AcceptanceCase) string {
	switch {
	case c.Layer == "integration" && slices.Contains(e2eCases, c.CaseID):
		return "host_e2e"
	case c.Layer == "integration":
		return "store_integration"
	}
	return "contracts_test"
}

// sourceFor is the checker's own table of where each kind of evidence lives
// and which source (execution layer) it must be labelled with.
func sourceFor(p string) (string, bool) {
	for _, r := range []struct{ prefix, source string }{
		{"provenance/", "provenance"}, {"contracts/", "contracts_test"}, {"tests/contracts.jsonl", "contracts_test"},
		{"tests/policy.jsonl", "policy_test"}, {"store/", "store_integration"}, {"tests/store.jsonl", "store_integration"},
		{"e2e/", "host_e2e"}, {"tests/e2e.jsonl", "host_e2e"}, {"tests/requirements-go.jsonl", "host_e2e"},
		{"tests/requirements-web.tap", "web_adapter_test"}, {"browser/", "browser_ui"}, {"requirements/", "requirements_index"},
	} {
		if p == r.prefix || strings.HasSuffix(r.prefix, "/") && strings.HasPrefix(p, r.prefix) {
			return r.source, true
		}
	}
	return "", false
}

// commands are the producer's steps (cmd/m1-fixture), each expected once
// with exit code 0; the browser step names its own output directory.
var commands = []string{
	"go test -count=1 -json -run ^TestContractCasesFromManifest$ ./internal/contracts/",
	"go test -count=1 -json -run ^TestAcceptedPolicyCases$ ./internal/policy/",
	"go test -count=1 -json -run ^TestAcceptedIntegrationCases$ ./internal/store/",
	"go test -count=1 -json -run ^TestM1FixtureCases$ ./cmd/demo/",
	"go test -count=1 -json -run ^(TestIntegrationAskDenyAndAccess|TestCancel.*|TestLiveRun.*)$ ./cmd/demo/",
	"node --test web/test/adapter.test.mjs",
	"node scripts/verify/browser-ui.mjs --out ",
}

var (
	runIDPattern  = regexp.MustCompile(`^m1-fixture-[0-9]{8}T[0-9]{6}Z-[0-9a-f]{6}$`)
	commitPattern = regexp.MustCompile(`^[0-9a-f]{40}$`)
	digestPattern = regexp.MustCompile(`^sha256:[0-9a-f]{64}$`)
)

// verifier holds one run's state while it is checked.
type verifier struct {
	b      *bundle
	tr     Trusted
	r      *Report
	f      findings // the bundle's own findings
	m      contracts.RunManifest
	ids    map[string]string // evidence ID -> path
	paths  map[string]string // path -> evidence ID
	digest map[string]bool   // paths whose bytes matched their digest
}

// Verify checks the run directory dir against the trusted inputs. mode is
// "fixture" (the full case bundle) or "live" (the single-Ask bundle,
// section 16). An error means the check could not be made at all (the
// directory cannot be opened).
func Verify(dir string, tr Trusted, mode string) (*Report, error) {
	b, err := openBundle(dir)
	if err != nil {
		return nil, err
	}
	defer b.close()
	v := &verifier{b: b, tr: tr, r: &Report{Mode: mode, NotEvaluated: slices.Clone(contracts.CapabilityIDs), Differences: []string{}},
		ids: map[string]string{}, paths: map[string]string{}, digest: map[string]bool{}}
	v.r.Checker.Name, v.r.Checker.Version = "verify-demo", Version
	v.r.Trusted.CaseManifestID, v.r.Trusted.CaseManifestDigest, v.r.Trusted.Files = tr.Cases.ManifestID, tr.CasesDigest, tr.Files
	v.r.Input.Files = len(b.files)
	v.r.Limits = []string{
		fmt.Sprintf("files are read only as regular files inside the run directory, at most %d files, %d bytes each, %d in all", MaxFiles, MaxFileBytes, MaxTotalBytes),
		"hashes and the event chain are recomputed from the bundle itself: a bundle forged consistently throughout cannot be told apart without an outside anchor",
		"integration requirements are shown by the test records in the bundle; the checker does not operate a browser",
		"provenance being complete does not prove that the named commit produced the data",
	}
	defer func() { v.r.Input.Bytes = b.total }()

	if mode == "live" {
		v.r.NotEvaluated = nil
		for _, c := range tr.Cases.Cases {
			if c.CaseID != "ask" {
				v.r.NotEvaluated = append(v.r.NotEvaluated, c.CaseID)
			}
		}
		v.r.NotEvaluated = append(v.r.NotEvaluated, contracts.CapabilityIDs...)
		v.r.Limits = append(v.r.Limits, liveLimits...)
	}
	switch mode {
	case "fixture", "live":
		v.bundleChecks(dir, mode)
	default:
		v.f.missing("V6", nil, "mode %s is not one this checker knows (fixture, live)", quote(mode))
	}
	switch {
	case v.m.Version == "":
	case mode == "fixture":
		v.cases()
		v.requirements()
	case mode == "live":
		v.liveCase()
	}
	v.r.Findings = v.f
	if v.r.Findings == nil {
		v.r.Findings = []Finding{}
	}
	v.r.finish()
	return v.r, nil
}

// readJSON reads and strictly decodes one evidence file; it records why it
// could not, and says whether it did.
func (v *verifier) readJSON(f *findings, claim, p string, dst any) bool {
	raw, err := v.b.read(p)
	if err != nil {
		v.readErr(f, claim, p, err)
		return false
	}
	if err := strictJSON(raw, dst); err != nil {
		var syn *json.SyntaxError
		if errors.As(err, &syn) || errors.Is(err, io.ErrUnexpectedEOF) {
			f.missing(claim, []string{p}, "%s is not a complete JSON document: %v", p, err)
		} else {
			f.fail(claim, []string{p}, "%s is ambiguous or not in the expected format: %v", p, err)
		}
		return false
	}
	return true
}

func (v *verifier) readErr(f *findings, claim, p string, err error) {
	switch {
	case errors.Is(err, errUnsafe):
		f.fail("V1", []string{p}, "%v", err)
	case errors.Is(err, errMissing):
		f.missing(claim, []string{p}, "%s is missing", p)
	default:
		f.missing(claim, []string{p}, "%s could not be read: %v", p, err)
	}
}

// bundleChecks: V1 (inventory, manifest, end marker, provenance, commands,
// index and digests) and V6 (modes and sources).
func (v *verifier) bundleChecks(dir, mode string) {
	f := &v.f
	for _, x := range v.b.violations {
		f.fail("V1", nil, "the run directory holds what may not be read: %s", x)
	}
	if v.b.tooMany {
		f.missing("V1", nil, "the run directory holds more than %d files", MaxFiles)
	}
	raw, err := v.b.read("manifest.json")
	if err != nil {
		v.readErr(f, "V1", "manifest.json", err)
		return
	}
	v.r.Input.ManifestDigest = sha(raw)
	var m contracts.RunManifest
	if !v.readJSON(f, "V1", "manifest.json", &m) {
		return
	}
	v.m = m
	v.r.Input.RunID = m.RunID

	// The end marker: written last, naming this run and this manifest.
	var end struct {
		RunID          string `json:"run_id"`
		State          string `json:"state"`
		ManifestDigest string `json:"manifest_digest"`
	}
	if endRaw, err := v.b.read("run-end.json"); err != nil {
		v.readErr(f, "V1", "run-end.json", err)
	} else if v.r.Input.EndMarkerDigest = sha(endRaw); v.readJSON(f, "V1", "run-end.json", &end) {
		switch {
		case end.RunID != m.RunID:
			f.fail("V1", []string{"run-end.json"}, "the end marker names run %s, the manifest %s", quote(end.RunID), quote(m.RunID))
		case end.State != "complete":
			f.missing("V1", []string{"run-end.json"}, "the producer marked the bundle %s", quote(end.State))
		case end.ManifestDigest != v.r.Input.ManifestDigest:
			f.fail("V1", []string{"run-end.json", "manifest.json"}, "the end marker's manifest digest is not the manifest's")
		}
	}

	// Provenance.
	base := filepath.Base(filepath.Clean(dir))
	switch {
	case m.Version != contracts.Version:
		f.fail("V1", []string{"manifest.json"}, "manifest version %s", quote(m.Version))
	case !map[string]*regexp.Regexp{"fixture": runIDPattern, "live": liveRunIDPattern}[mode].MatchString(m.RunID) || m.RunID != base:
		f.fail("V1", []string{"manifest.json"}, "run ID %s is not a %s run ID or not the directory's name %s", quote(m.RunID), mode, quote(base))
	}
	if !commitPattern.MatchString(m.CodeCommit) || !digestPattern.MatchString(m.DirtyPatchDigest) || !digestPattern.MatchString(m.ConfigDigest) {
		f.fail("V1", []string{"manifest.json"}, "code commit, dirty patch digest or config digest malformed")
	}
	keys := make([]string, 0, len(m.Versions))
	for k, val := range m.Versions {
		if strings.TrimSpace(val) == "" {
			f.fail("V1", []string{"manifest.json"}, "version %s is empty", k)
		}
		keys = append(keys, k)
	}
	sort.Strings(keys)
	wantKeys := map[string][]string{"fixture": {"case_manifest", "fake_cli", "go", "node", "os"}, "live": {"case_manifest", "cli", "gate_mode", "go", "os"}}[mode]
	if !slices.Equal(keys, wantKeys) {
		f.fail("V1", []string{"manifest.json"}, "versions %v, want %v", keys, wantKeys)
	} else if m.Versions["case_manifest"] != v.tr.Cases.ManifestID {
		f.fail("V1", []string{"manifest.json"}, "versions name case list %s, not %s", quote(m.Versions["case_manifest"]), v.tr.Cases.ManifestID)
	}
	if m.CaseManifestDigest != v.tr.CasesDigest {
		f.missing("V1", []string{"manifest.json"}, "the bundle was made for another case list (%s); this checker has the frozen %s (%s): unsupported version, not checked",
			quote(m.CaseManifestDigest), v.tr.Cases.ManifestID, v.tr.CasesDigest)
		v.m.Version = "" // nothing case-level can be judged
		return
	}
	v.configChecks()
	if _, err := v.b.read("provenance/git-status.txt"); err != nil {
		v.readErr(f, "V1", "provenance/git-status.txt", err)
	}
	if mode == "live" {
		v.liveBundle()
	} else {
		v.fixtureCommands()
	}
	v.index(mode)
}

// fixtureCommands: each producer step once, exit 0, nothing else.
func (v *verifier) fixtureCommands() {
	f, m := &v.f, v.m
	seen := make([]int, len(commands))
	for _, c := range m.Commands {
		i := slices.IndexFunc(commands, func(want string) bool {
			if strings.HasSuffix(want, "--out ") {
				return strings.HasPrefix(c.Command, want) && strings.HasSuffix(c.Command, "/browser")
			}
			return c.Command == want
		})
		if i < 0 {
			f.fail("V1", []string{"manifest.json"}, "unexpected command %s", quote(c.Command))
			continue
		}
		seen[i]++
		if c.ExitCode != 0 {
			f.fail("V1", []string{"manifest.json"}, "command %s exited %d", quote(c.Command), c.ExitCode)
		}
	}
	for i, n := range seen {
		switch {
		case n == 0:
			f.missing("V1", []string{"manifest.json"}, "no record of command %s", quote(commands[i]))
		case n > 1:
			f.fail("V1", []string{"manifest.json"}, "command %s recorded %d times", quote(commands[i]), n)
		}
	}
}

// index: unique, plain relative paths, the mode's source for each path and
// its evidence mode, the bytes' digest; nothing in the directory left out;
// the mode's claims (fixture: every case; live: ask alone) and the rest
// not evaluated.
func (v *verifier) index(mode string) {
	f, m := &v.f, v.m
	for _, e := range m.Evidence {
		ev := []string{e.Path}
		if _, dup := v.ids[e.EvidenceID]; dup {
			f.fail("V1", ev, "evidence ID %s listed twice", quote(e.EvidenceID))
		}
		if _, dup := v.paths[e.Path]; dup {
			f.fail("V1", ev, "evidence path listed twice")
		}
		v.ids[e.EvidenceID], v.paths[e.Path] = e.Path, e.EvidenceID
		if e.EvidenceMode != mode {
			f.fail("V6", ev, "evidence mode %s in a %s bundle", quote(e.EvidenceMode), mode)
		}
		if want, ok := v.source(e.Path, mode); !ok {
			f.fail("V6", ev, "a file outside the %s layout", mode)
		} else if e.Source != want {
			f.fail("V6", ev, "labelled source %s; this file is %s evidence", quote(e.Source), want)
		}
		raw, err := v.b.read(e.Path)
		if err != nil {
			v.readErr(f, "V1", e.Path, err)
			continue
		}
		if sha(raw) != e.Digest {
			f.fail("V1", ev, "the bytes' digest is not the indexed digest")
		} else {
			v.digest[e.Path] = true
		}
	}
	for p := range v.b.files {
		if _, ok := v.paths[p]; !ok && p != "manifest.json" && p != "run-end.json" {
			f.fail("V1", []string{p}, "a file in the run directory that the index does not list")
		}
	}
	if err := m.ValidateAgainst(v.tr.Cases); err != nil {
		f.fail("V1", []string{"manifest.json"}, "the index does not validate: %v", err)
	}
	// Claims: each case once, the seven capabilities not evaluated, every
	// cited evidence indexed.
	count := map[string]int{}
	for _, res := range m.Results {
		count[res.CaseID]++
		for _, id := range res.EvidenceIDs {
			if _, ok := v.ids[id]; !ok {
				f.fail("V1", []string{"manifest.json"}, "claim %s cites evidence %s that the index does not list", quote(res.CaseID), quote(id))
			}
		}
		if slices.Contains(contracts.CapabilityIDs, res.CaseID) {
			f.fail("V1", []string{"manifest.json"}, "capability %s is claimed %s; M1 does not implement it", res.CaseID, quote(res.Verdict))
		}
	}
	for id, n := range count {
		if n > 1 {
			f.fail("V1", []string{"manifest.json"}, "claim %s listed %d times", quote(id), n)
		}
	}
	// fixture: every case is required; live: ask alone, every other case
	// not evaluated (docs/m1-closeout-gaps.md 3.3).
	want := slices.Clone(contracts.CapabilityIDs)
	for _, c := range v.tr.Cases.Cases {
		required := mode == "fixture" || c.CaseID == "ask"
		switch {
		case required && count[c.CaseID] == 0:
			f.fail("V1", []string{"manifest.json"}, "required claim %s has no result (a %s bundle evaluates %s)", c.CaseID, mode, map[string]string{"fixture": "every case", "live": "ask"}[mode])
		case required && slices.Contains(m.NotEvaluated, c.CaseID):
			f.fail("V1", []string{"manifest.json"}, "required claim %s is listed as not evaluated", c.CaseID)
		case !required && count[c.CaseID] > 0:
			f.fail("V1", []string{"manifest.json"}, "a live bundle claims %s; it shows the single Ask only", c.CaseID)
		case !required:
			want = append(want, c.CaseID)
		}
	}
	ne := slices.Clone(m.NotEvaluated)
	sort.Strings(ne)
	sort.Strings(want)
	if !slices.Equal(ne, want) {
		f.fail("V1", []string{"manifest.json"}, "not_evaluated %v must be exactly %v", m.NotEvaluated, want)
	}
}

// source is the evidence source a path must carry in the mode's layout.
func (v *verifier) source(p, mode string) (string, bool) {
	if mode == "live" {
		switch {
		case strings.HasPrefix(p, "provenance/"):
			return "provenance", true
		case strings.HasPrefix(p, "live/"):
			return "live_cli", true
		}
		return "", false
	}
	return sourceFor(p)
}

// configChecks: the bundle's configuration digests are the trusted files',
// and the config digest is made of them in the documented order.
func (v *verifier) configChecks() {
	f := &v.f
	const p = "provenance/config-files.json"
	var listed map[string]string
	if !v.readJSON(f, "V1", p, &listed) {
		return
	}
	keys := make([]string, 0, len(listed))
	for k := range listed {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	want := slices.Clone(configFiles)
	sort.Strings(want)
	if !slices.Equal(keys, want) {
		f.fail("V1", []string{p}, "configuration files %v, want %v", keys, want)
		return
	}
	var b strings.Builder
	for _, file := range configFiles {
		fmt.Fprintf(&b, "%s\x00%s\n", file, listed[file])
		switch trusted, ok := v.tr.Files[file]; {
		case !ok:
			f.missing("V1", []string{p}, "the trusted %s is not in the repository", file)
		case trusted != listed[file]:
			f.missing("V1", []string{p}, "the bundle was made with another %s than this checker's trusted one: unsupported version", file)
		}
	}
	if sha([]byte(b.String())) != v.m.ConfigDigest {
		f.fail("V1", []string{p, "manifest.json"}, "the config digest is not made of the listed configuration digests")
	}
}

// cases checks every frozen case at its accepted layer.
func (v *verifier) cases() {
	producer := map[string]contracts.ClaimResult{}
	for _, res := range v.m.Results {
		producer[res.CaseID] = res
	}
	for _, c := range v.tr.Cases.Cases {
		cr := CaseReport{CaseID: c.CaseID, Layer: layerOf(c), Expected: c.Expected, Observed: []Observed{}, ProducerVerdict: producer[c.CaseID].Verdict}
		var f findings
		need := map[string][]string{"host_e2e": {"e2e/", "store/"}, "store_integration": {"store/"}, "contracts_test": {"contracts/"}}[cr.Layer]
		cited := map[string]bool{}
		for _, id := range producer[c.CaseID].EvidenceIDs {
			p := v.ids[id]
			cited[p] = true
			if s, ok := sourceFor(p); ok && cr.Layer != "host_e2e" && s == "host_e2e" || cr.Layer == "contracts_test" && s == "store_integration" {
				f.fail("V6", []string{p}, "a %s case cites %s evidence", cr.Layer, s)
			}
		}
		for _, dir := range need {
			p := dir + c.CaseID + ".json"
			cr.Evidence = append(cr.Evidence, p)
			if _, ok := v.paths[p]; !ok {
				f.missing("V1", []string{p}, "the record is not in the index")
				continue
			}
			if !cited[p] { // the record is there, and the claim rests on something else
				f.fail("V6", []string{p}, "the claim does not cite its %s record", strings.TrimSuffix(dir, "/"))
			}
			switch dir {
			case "e2e/":
				cr.Observed = append(cr.Observed, v.e2eRecord(&f, c, p))
			case "store/":
				cr.Observed = append(cr.Observed, v.storeRecord(&f, c, p))
			case "contracts/":
				cr.Observed = append(cr.Observed, v.contractRecord(&f, c, p))
			}
		}
		cr.Findings = f
		if cr.Findings == nil {
			cr.Findings = []Finding{}
		}
		cr.Claims = f.verdicts([]string{"V2", "V3", "V4", "V5", "V6", "V7"})
		cr.Verdict = f.verdict()
		if string(cr.Verdict) != cr.ProducerVerdict {
			v.r.Differences = append(v.r.Differences, fmt.Sprintf("%s: the producer says %s, this checker %s", c.CaseID, quote(cr.ProducerVerdict), cr.Verdict))
		}
		v.r.Cases = append(v.r.Cases, cr)
	}
}
