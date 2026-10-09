// Command m1-fixture makes the M1 fixture evidence bundle (docs/m1-evidence.md;
// make m1-fixture): a new artifacts/m1/<run-id>/ with the existing
// RunManifest contract (internal/contracts/evidence.go), the evidence files
// it indexes and an end marker. It runs the fake CLI only (no model, no
// testnet, no payment) and writes nothing outside the new run directory.
//
// Layers, as docs/m1-closeout-gaps.md 3.1 sets them: each case claim names
// evidence of the highest layer it ran at (host_e2e, store_integration or
// contracts_test); a contracts test is never offered as end-to-end evidence.
// A claim rests on structured evidence the bundle itself can be recomputed
// from (internal/m1evidence); a test log is auxiliary and never carries a
// claim alone. The verdicts are the producer's own; B1-07 (make verify-m1)
// checks the bundle independently. RunManifest.ValidateAgainst checks the
// index only.
package main

import (
	"bufio"
	"bytes"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime"
	"slices"
	"sort"
	"strings"
	"time"

	"github.com/SIMPLYBOYS/agent-payment-authority/internal/contracts"
	"github.com/SIMPLYBOYS/agent-payment-authority/internal/m1evidence"
)

func main() {
	os.Exit(run(os.Args[1:], os.Stdout, os.Stderr))
}

// e2eCases are the integration cases the demo host runs end to end
// (cmd/demo TestM1FixtureCases); the other integration cases keep their
// Store-level evidence.
var e2eCases = []string{"ask", "deny-budget", "deny-merchant", "deny-asset", "deny-unattended", "cancel-pending", "cancel-no-purchase",
	"cli-timeout", "cli-timeout-pending", "approval-timeout", "approval-deadline-before", "approval-deadline-exact", "restart"}

// configFiles are the trusted fixture files the run depends on; their
// digests make the config digest. What each task was actually started with
// (a case's own variant included) is in its structured evidence.
var configFiles = []string{"testdata/contracts/cases.json", "testdata/contracts/registry.json", "testdata/contracts/purchase-input.json",
	"testdata/demo/registry.json", "testdata/policy/m1-demo-profile.json", "testdata/cli/ask-mainline.json"}

// sources is the closed list of evidence sources (EvidenceRef.source): the
// layer a file comes from. live_cli is reserved for M1-E3.
var sources = map[string]string{
	"provenance/":            "provenance",
	"tests/contracts":        "contracts_test",
	"contracts/":             "contracts_test",
	"tests/policy":           "policy_test",
	"tests/store":            "store_integration",
	"store/":                 "store_integration",
	"tests/e2e":              "host_e2e",
	"e2e/":                   "host_e2e",
	"tests/requirements-go":  "host_e2e",
	"tests/requirements-web": "web_adapter_test",
	"browser/":               "browser_ui",
	"requirements/":          "requirements_index",
}

func sourceOf(path string) (string, error) {
	for prefix, s := range sources {
		if strings.HasPrefix(path, prefix) {
			return s, nil
		}
	}
	return "", fmt.Errorf("evidence %s has no source in the closed list", path)
}

type step struct {
	name string   // the evidence file, relative to the run directory
	cmd  []string // run in the repository
	env  []string
}

// Exit codes: the bundle is complete and every claim and requirement
// passed; the bundle is complete with failures in it; the bundle could not
// be made or saved completely (its end marker says so when it can).
const (
	exitPass       = 0
	exitFailures   = 1
	exitIncomplete = 2
)

// runStep runs one command in the repository; capture runs a provenance
// query. Tests replace both.
var runStep = func(s step) ([]byte, int, error) {
	c := exec.Command(s.cmd[0], s.cmd[1:]...)
	c.Env = append(os.Environ(), s.env...)
	var buf bytes.Buffer
	c.Stdout, c.Stderr = &buf, &buf
	err := c.Run()
	if ee, ok := errors.AsType[*exec.ExitError](err); ok {
		return buf.Bytes(), ee.ExitCode(), nil
	}
	if err != nil {
		return buf.Bytes(), -1, err
	}
	return buf.Bytes(), 0, nil
}

var capture = func(name string, args ...string) (string, error) {
	out, err := exec.Command(name, args...).Output()
	if err != nil {
		return "", fmt.Errorf("%s %s: %w", name, strings.Join(args, " "), err)
	}
	return string(out), nil
}

func run(args []string, stdout, stderr io.Writer) int {
	fl := flag.NewFlagSet("m1-fixture", flag.ContinueOnError)
	fl.SetOutput(stderr)
	out := fl.String("out", "artifacts/m1", "where the new run directory is made")
	skipBrowser := fl.Bool("skip-browser", false, "do not run scripts/verify/browser-ui.mjs (its integration checks are then missing from the bundle)")
	if fl.Parse(args) != nil || fl.NArg() != 0 {
		return exitIncomplete
	}
	fail := func(format string, a ...any) int {
		fmt.Fprintf(stderr, "m1-fixture: "+format+"\n", a...)
		return exitIncomplete
	}
	var cases contracts.CaseManifest
	raw, err := os.ReadFile("testdata/contracts/cases.json")
	if err != nil || json.Unmarshal(raw, &cases) != nil || cases.RequireAccepted() != nil {
		return fail("run from the repository root; the accepted testdata/contracts/cases.json is needed")
	}
	caseDigest, err := cases.Hash()
	if err != nil {
		return fail("case manifest: %v", err)
	}
	suffix := make([]byte, 3)
	rand.Read(suffix)
	runID := "m1-fixture-" + time.Now().UTC().Format("20060102T150405Z") + "-" + hex.EncodeToString(suffix)
	if err := os.MkdirAll(*out, 0o700); err != nil {
		return fail("%v", err)
	}
	dir := filepath.Join(*out, runID)
	if err := os.Mkdir(dir, 0o700); err != nil { // a new directory, never an old one
		return fail("%v", err)
	}
	// incomplete stops: the bundle is not complete. The end marker says so
	// when it can be written; stderr and the exit code say so either way.
	// Whatever was saved stays.
	incomplete := func(state string, err error) int {
		fmt.Fprintf(stderr, "m1-fixture: %s: the bundle %s is INCOMPLETE (%s): %v\n", runID, dir, state, err)
		if werr := writeEnd(dir, runID, nil, state); werr != nil {
			fmt.Fprintf(stderr, "m1-fixture: the end marker could not be written either: %v\n", werr)
		}
		return exitIncomplete
	}
	abs, err := filepath.Abs(dir)
	if err != nil {
		return incomplete("save_failed", err)
	}
	for _, d := range []string{"provenance", "tests", "e2e", "store", "contracts", "requirements"} {
		if err := os.Mkdir(filepath.Join(dir, d), 0o700); err != nil {
			return incomplete("save_failed", err)
		}
	}
	save := func(rel string, b []byte) error { return os.WriteFile(filepath.Join(dir, rel), b, 0o600) }

	// Provenance: each query must succeed; an empty answer (no diff, a clean
	// tree) is only trusted from a query that did.
	q := map[string]string{}
	for _, c := range [][]string{{"commit", "git", "rev-parse", "HEAD"}, {"patch", "git", "diff", "HEAD", "--binary"}, {"status", "git", "status", "--porcelain"},
		{"go", "go", "version"}, {"node", "node", "--version"}} {
		v, err := capture(c[1], c[2:]...)
		if err != nil {
			return incomplete("provenance_failed", err)
		}
		q[c[0]] = v
	}
	commit := strings.TrimSpace(q["commit"])
	if !regexp.MustCompile(`^[0-9a-f]{40}$`).MatchString(commit) {
		return incomplete("provenance_failed", fmt.Errorf("git rev-parse HEAD gave %q", commit))
	}
	configs := map[string]string{}
	var cfg strings.Builder
	for _, f := range configFiles {
		b, err := os.ReadFile(f)
		if err != nil {
			return incomplete("provenance_failed", err)
		}
		configs[f] = digest(b)
		fmt.Fprintf(&cfg, "%s\x00%s\n", f, configs[f])
	}
	cj, err := json.MarshalIndent(configs, "", "  ")
	if err == nil {
		err = errors.Join(save("provenance/git-status.txt", []byte(q["status"])), save("provenance/config-files.json", append(cj, '\n')))
	}
	if err != nil {
		return incomplete("save_failed", err)
	}
	versions := map[string]string{"go": strings.TrimSpace(q["go"]), "node": strings.TrimSpace(q["node"]),
		"os": runtime.GOOS + "/" + runtime.GOARCH, "case_manifest": cases.ManifestID, "fake_cli": "fake-claude (no model, no network)"}

	// The commands, each with its exit code and output. The case tests write
	// their structured evidence into the run directory (APA_M1_RUN_DIR).
	into := func(sub string) []string { return []string{"APA_M1_RUN_DIR=" + filepath.Join(abs, sub)} }
	steps := []step{
		{"tests/contracts.jsonl", []string{"go", "test", "-count=1", "-json", "-run", "^TestContractCasesFromManifest$", "./internal/contracts/"}, into("contracts")},
		{"tests/policy.jsonl", []string{"go", "test", "-count=1", "-json", "-run", "^TestAcceptedPolicyCases$", "./internal/policy/"}, nil},
		{"tests/store.jsonl", []string{"go", "test", "-count=1", "-json", "-run", "^TestAcceptedIntegrationCases$", "./internal/store/"}, into("store")},
		{"tests/e2e.jsonl", []string{"go", "test", "-count=1", "-json", "-run", "^TestM1FixtureCases$", "./cmd/demo/"}, into("e2e")},
		{"tests/requirements-go.jsonl", []string{"go", "test", "-count=1", "-json", "-run", "^(TestIntegrationAskDenyAndAccess|TestCancel.*|TestLiveRun.*)$", "./cmd/demo/"}, nil},
		{"tests/requirements-web.tap", []string{"node", "--test", "web/test/adapter.test.mjs"}, nil},
	}
	if !*skipBrowser {
		steps = append(steps, step{"browser/browser-ui.out", []string{"node", "scripts/verify/browser-ui.mjs", "--out", filepath.Join(dir, "browser")}, nil})
	}
	var commands []contracts.CommandResult
	outputs := map[string][]byte{}
	for _, s := range steps {
		fmt.Fprintf(stderr, "m1-fixture: %s\n", strings.Join(s.cmd, " "))
		b, code, err := runStep(s)
		if err != nil { // it did not run: its claims have no evidence
			fmt.Fprintf(stderr, "m1-fixture: %v\n", err)
		}
		commands = append(commands, contracts.CommandResult{Command: strings.Join(s.cmd, " "), ExitCode: code})
		if err := os.MkdirAll(filepath.Dir(filepath.Join(dir, s.name)), 0o700); err != nil {
			return incomplete("save_failed", err)
		}
		if err := save(s.name, b); err != nil {
			return incomplete("save_failed", err)
		}
		outputs[s.name] = b
	}

	reqs, reqOK := requirementsIndex(outputs, *skipBrowser)
	rj, err := json.MarshalIndent(reqs, "", "  ")
	if err == nil {
		err = save("requirements/index.json", append(rj, '\n'))
	}
	if err != nil {
		return incomplete("save_failed", err)
	}

	m, err := assemble(dir, cases, outputs, runID, commit, digest([]byte(q["patch"])), digest([]byte(cfg.String())), caseDigest, versions, commands)
	if err != nil {
		return incomplete("save_failed", err)
	}
	leaks, err := scanSecrets(dir)
	if err != nil {
		return incomplete("save_failed", err)
	}
	if len(leaks) > 0 {
		return incomplete("secret_found", fmt.Errorf("a bare token-like value in %v", leaks))
	}
	if err := m.ValidateAgainst(cases); err != nil {
		return incomplete("index_invalid", err)
	}
	body, err := json.MarshalIndent(m, "", "  ")
	if err == nil {
		body = append(body, '\n')
		err = save("manifest.json", body)
	}
	if err != nil {
		return incomplete("save_failed", err)
	}
	if err := writeEnd(dir, runID, body, "complete"); err != nil {
		fmt.Fprintf(stderr, "m1-fixture: %s: the end marker could not be written: %v; the bundle %s is INCOMPLETE\n", runID, err, dir)
		return exitIncomplete
	}

	n := map[string]int{}
	for _, r := range m.Results {
		n[r.Verdict]++
	}
	fmt.Fprintf(stdout, "m1-fixture: %s (complete): %d PASS, %d FAIL, %d INCONCLUSIVE of %d cases; %d capabilities not_evaluated (the producer's own verdicts; make verify-m1 checks independently)\n",
		dir, n["PASS"], n["FAIL"], n["INCONCLUSIVE"], len(m.Results), len(m.NotEvaluated))
	fmt.Fprintf(stdout, "m1-fixture: integration requirements (docs/m1-closeout-gaps.md 3.2): all checks passed: %v\n", reqOK)
	if !reqOK || n["FAIL"]+n["INCONCLUSIVE"] > 0 || slices.ContainsFunc(commands, func(c contracts.CommandResult) bool { return c.ExitCode != 0 }) {
		return exitFailures
	}
	return exitPass
}

// The structured evidence is checked with these (tests replace them).
var (
	checkIntegration = m1evidence.CheckIntegration
	checkContract    = m1evidence.CheckContract
)

// assemble builds the RunManifest from the run directory's files and the
// test outputs; it is deterministic for the same inputs.
func assemble(dir string, cases contracts.CaseManifest, outputs map[string][]byte, runID, commit, patchDigest, configDigest, caseDigest string,
	versions map[string]string, commands []contracts.CommandResult) (contracts.RunManifest, error) {
	m := contracts.RunManifest{Version: contracts.Version, RunID: runID, CodeCommit: commit, DirtyPatchDigest: patchDigest, ConfigDigest: configDigest,
		CaseManifestDigest: caseDigest, Versions: versions, Commands: commands, Evidence: []contracts.EvidenceRef{}, Results: []contracts.ClaimResult{},
		NotEvaluated: slices.Clone(contracts.CapabilityIDs)}
	var files []string
	err := filepath.WalkDir(dir, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.Type()&fs.ModeSymlink != 0 {
			return fmt.Errorf("a symlink in the run directory: %s", p)
		}
		if d.Type().IsRegular() {
			rel, err := filepath.Rel(dir, p)
			if err != nil {
				return err
			}
			rel = filepath.ToSlash(rel)
			if rel != "manifest.json" && rel != "run-end.json" {
				files = append(files, rel)
			}
		}
		return nil
	})
	if err != nil {
		return m, err
	}
	sort.Strings(files)
	ids := map[string]string{}
	raws := map[string][]byte{}
	for _, f := range files {
		src, err := sourceOf(f)
		if err != nil {
			return m, err
		}
		b, err := os.ReadFile(filepath.Join(dir, f))
		if err != nil {
			return m, err
		}
		id := "ev-" + regexp.MustCompile(`[^A-Za-z0-9._:-]`).ReplaceAllString(f, "-")
		ids[f], raws[f] = id, b
		m.Evidence = append(m.Evidence, contracts.EvidenceRef{EvidenceID: id, Path: f, Digest: digest(b), EvidenceMode: "fixture", Source: src})
	}
	tests := map[string]map[string]string{}
	for name, out := range outputs {
		if strings.HasSuffix(name, ".jsonl") {
			tests[name] = subtests(out)
		}
	}
	g := grader{ids: ids, raws: raws, tests: tests}
	for _, c := range cases.Cases {
		// integration checks an E2E or Store file: the tasks as the Store
		// recorded them, recomputed against the case.
		integration := func(layer, file, log, test string) need {
			return need{file: file, log: log, test: test, check: func(raw []byte) (string, []string, error) {
				var ev struct {
					Layer   string            `json:"layer"`
					Verdict string            `json:"verdict"` // the E2E test's own
					Tasks   []m1evidence.Task `json:"tasks"`   // a Store file's export
					Store   []m1evidence.Task `json:"store"`   // an E2E file's export
				}
				if err := json.Unmarshal(raw, &ev); err != nil || ev.Layer != layer {
					return "", nil, fmt.Errorf("not %s evidence", layer)
				}
				tasks := ev.Tasks
				if layer == "host_e2e" {
					tasks = ev.Store
				}
				o, problems := checkIntegration(c, cases.Defaults, tasks)
				if layer == "host_e2e" && ev.Verdict != "PASS" {
					problems = append(problems, "the E2E test recorded "+ev.Verdict)
				}
				return fmt.Sprintf("%s/%s/%s, payment %s, budget_deducted %s, purchases %d, forbidden events %v",
					o.Result, o.Reason, o.ApprovalStatus, o.PaymentStatus, o.BudgetDeducted, o.Purchases, o.ForbiddenEventsFound), problems, nil
			}}
		}
		store := integration("store_integration", "store/"+c.CaseID+".json", "tests/store.jsonl", "TestAcceptedIntegrationCases/"+c.CaseID)
		var needs []need
		switch {
		case c.Layer == "integration" && slices.Contains(e2eCases, c.CaseID):
			needs = []need{integration("host_e2e", "e2e/"+c.CaseID+".json", "tests/e2e.jsonl", "TestM1FixtureCases/"+c.CaseID), store}
		case c.Layer == "integration":
			needs = []need{store}
			if c.Operation == "policy" {
				needs = append(needs, need{log: "tests/policy.jsonl", test: "TestAcceptedPolicyCases/" + c.CaseID})
			}
		default:
			needs = []need{{file: "contracts/" + c.CaseID + ".json", log: "tests/contracts.jsonl", test: "TestContractCasesFromManifest/" + c.CaseID,
				check: func(raw []byte) (string, []string, error) {
					var x m1evidence.Contract
					if err := json.Unmarshal(raw, &x); err != nil {
						return "", nil, err
					}
					o, problems := checkContract(c, x)
					return fmt.Sprintf("rerun from the recorded inputs: %s %s (contracts layer, not end-to-end)", o.Result, o.Code), problems, nil
				}}}
		}
		m.Results = append(m.Results, g.grade(c.CaseID, needs))
	}
	return m, nil
}

// need is one piece of evidence a claim rests on: a structured evidence
// file with its check, and the test log that goes with it. A log alone is
// auxiliary: it can fail a claim, never carry one.
type need struct {
	file  string
	check func(raw []byte) (summary string, problems []string, err error)
	log   string // tests/<x>.jsonl
	test  string // the subtest in it
}

type grader struct {
	ids   map[string]string
	raws  map[string][]byte
	tests map[string]map[string]string
}

// grade: INCONCLUSIVE when a file is missing or unreadable or a test did
// not run; FAIL when a check finds a problem or a test failed; PASS only
// when every file checks clean and every test passed.
func (g grader) grade(caseID string, needs []need) contracts.ClaimResult {
	r := contracts.ClaimResult{CaseID: caseID, Verdict: "PASS", EvidenceIDs: []string{}}
	var reasons []string
	for _, n := range needs {
		if n.file != "" {
			raw, ok := g.raws[n.file]
			if !ok {
				r.Verdict = worst(r.Verdict, "INCONCLUSIVE")
				reasons = append(reasons, n.file+": missing")
			} else {
				r.EvidenceIDs = append(r.EvidenceIDs, g.ids[n.file])
				summary, problems, err := n.check(raw)
				switch {
				case err != nil:
					r.Verdict = worst(r.Verdict, "INCONCLUSIVE")
					reasons = append(reasons, n.file+": unreadable: "+err.Error())
				case len(problems) > 0:
					r.Verdict = worst(r.Verdict, "FAIL")
					reasons = append(reasons, n.file+": "+strings.Join(problems, "; "))
				default:
					reasons = append(reasons, n.file+": recomputed "+summary)
				}
			}
		}
		if n.log != "" {
			action := g.tests[n.log][n.test]
			r.Verdict = worst(r.Verdict, verdict(action))
			if id := g.ids[n.log]; id != "" {
				r.EvidenceIDs = append(r.EvidenceIDs, id)
			}
			reasons = append(reasons, n.test+" "+word(action))
		}
	}
	r.Reason = strings.Join(reasons, "; ")
	return r
}

// requirements are docs/m1-closeout-gaps.md 3.2: what the cases do not
// name, each with the tests that show it. A name ending in * is a prefix.
var requirements = []struct {
	Requirement string
	Checks      [][2]string // evidence file, test
}{
	{"a page refresh restores from the Store", [][2]string{{"browser/browser-ui.out", "refresh + sign-in restores task-a from the Store"}}},
	{"hostile strings are shown as text only", [][2]string{{"tests/requirements-web.tap", "hostile strings stay strings; nothing reads as paid"}, {"browser/browser-ui.out", "hostile strings render as text only"}}},
	{"a model saying it paid shows no success", [][2]string{{"browser/browser-ui.out", "nothing says paid or delivered"}, {"tests/requirements-go.jsonl", "TestLiveRun"}}},
	{"a resent event is not animated twice", [][2]string{{"tests/requirements-web.tap", "duplicate events are shown once and animated once"}}},
	{"switching A/B mixes no data", [][2]string{{"tests/requirements-web.tap", "a switch drops every late reply of the old viewer"}, {"tests/requirements-go.jsonl", "TestIntegrationAskDenyAndAccess"}}},
	{"cancel and CLI timeout have their own states", [][2]string{{"tests/requirements-go.jsonl", "TestCancel*"}, {"browser/browser-ui.out", "3.4:*"}}},
}

type reqCheck struct {
	Evidence string `json:"evidence"`
	Test     string `json:"test"`
	Status   string `json:"status"` // pass | fail | not_found | not_run
}

// requirementsIndex looks each requirement's tests up in the outputs: go
// test -json results, node --test TAP lines (ok N - name) and browser-ui's
// log lines (PASS name). A prefix needs at least one match and no failure.
func requirementsIndex(outputs map[string][]byte, skipBrowser bool) ([]map[string]any, bool) {
	all := true
	var out []map[string]any
	for _, r := range requirements {
		var checks []reqCheck
		for _, c := range r.Checks {
			file, name := c[0], c[1]
			prefix := strings.HasSuffix(name, "*")
			name = strings.TrimSuffix(name, "*")
			passes, fails := 0, 0
			switch {
			case strings.HasSuffix(file, ".jsonl"):
				for test, action := range subtests(outputs[file]) {
					if !strings.Contains(test, "/") && (test == name || prefix && strings.HasPrefix(test, name)) {
						if action == "pass" {
							passes++
						} else {
							fails++
						}
					}
				}
			default:
				lines := outputs[file]
				pass, fail := "PASS ", "FAIL "
				if strings.HasSuffix(file, ".tap") {
					pass, fail = "ok ", "not ok "
				}
				for l := range strings.SplitSeq(string(lines), "\n") {
					l = strings.TrimSpace(l)
					if strings.HasSuffix(file, ".tap") { // ok 3 - name
						if i := strings.Index(l, " - "); i > 0 {
							l = l[:strings.LastIndex(l[:i], " ")+1] + l[i+3:]
						}
					}
					for _, w := range []struct {
						p string
						n *int
					}{{fail, &fails}, {pass, &passes}} {
						if rest, ok := strings.CutPrefix(l, w.p); ok && (rest == name || prefix && strings.HasPrefix(rest, name)) {
							*w.n++
							break
						}
					}
				}
			}
			st := "not_found"
			switch {
			case strings.HasPrefix(file, "browser/") && skipBrowser:
				st = "not_run"
			case fails > 0:
				st = "fail"
			case passes > 0:
				st = "pass"
			}
			all = all && st == "pass"
			checks = append(checks, reqCheck{file, c[1], st})
		}
		out = append(out, map[string]any{"requirement": r.Requirement, "checks": checks})
	}
	return out, all
}

// subtests reads `go test -json` output: test name -> pass | fail | skip.
func subtests(out []byte) map[string]string {
	res := map[string]string{}
	sc := bufio.NewScanner(bytes.NewReader(out))
	sc.Buffer(make([]byte, 1<<20), 1<<24)
	for sc.Scan() {
		var e struct{ Action, Test string }
		if json.Unmarshal(sc.Bytes(), &e) == nil && e.Test != "" && (e.Action == "pass" || e.Action == "fail" || e.Action == "skip") {
			res[e.Test] = e.Action
		}
	}
	return res
}

func verdict(action string) string {
	switch action {
	case "pass":
		return "PASS"
	case "fail":
		return "FAIL"
	}
	return "INCONCLUSIVE" // missing or skipped
}

func word(action string) string {
	if action == "" {
		return "not run"
	}
	return action + "ed"
}

func worst(a, b string) string {
	if a == "FAIL" || b == "FAIL" {
		return "FAIL"
	}
	if a == "INCONCLUSIVE" || b == "INCONCLUSIVE" {
		return "INCONCLUSIVE"
	}
	return "PASS"
}

// tokenLike is a bare 64-hex value: the shape of a demo token. Digests
// (sha256:…) and chain values (0x…) are not bare.
var tokenLike = regexp.MustCompile(`(^|[^0-9a-fA-Fx:])[0-9a-f]{64}([^0-9a-f]|$)`)

// scanSecrets lists the files of the run directory holding a bare
// token-like value; a bundle with one is never marked complete. A file it
// cannot read is an error, never a clean result.
func scanSecrets(dir string) ([]string, error) {
	var leaks []string
	err := filepath.WalkDir(dir, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if !d.Type().IsRegular() || strings.HasSuffix(p, ".png") {
			return nil
		}
		b, err := os.ReadFile(p)
		if err != nil {
			return err
		}
		if tokenLike.Match(b) {
			rel, err := filepath.Rel(dir, p)
			if err != nil {
				return err
			}
			leaks = append(leaks, rel)
		}
		return nil
	})
	return leaks, err
}

// writeEnd is the end marker, written last: the run ID, the manifest's
// file digest and whether the bundle is complete.
func writeEnd(dir, runID string, manifest []byte, state string) error {
	end := map[string]string{"run_id": runID, "state": state}
	if manifest != nil {
		end["manifest_digest"] = digest(manifest)
	}
	b, err := json.MarshalIndent(end, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(filepath.Join(dir, "run-end.json"), append(b, '\n'), 0o600)
}

func digest(b []byte) string {
	h := sha256.Sum256(b)
	return "sha256:" + hex.EncodeToString(h[:])
}
