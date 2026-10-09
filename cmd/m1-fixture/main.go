// Command m1-fixture makes the M1 fixture evidence bundle (docs/m1-evidence.md;
// make m1-fixture): a new artifacts/m1/<run-id>/ with the existing
// RunManifest contract (internal/contracts/evidence.go), the evidence files
// it indexes and an end marker. It runs the fake CLI only (no model, no
// testnet, no payment) and writes nothing outside the new run directory.
//
// Layers, as docs/m1-closeout-gaps.md 3.1 sets them: each case claim names
// evidence of the highest layer it ran at (host_e2e, store_integration or
// contracts_test); a contracts test is never offered as end-to-end evidence.
// The verdicts here are the producer's own; B1-07 (make verify-m1) checks
// the bundle independently. RunManifest.ValidateAgainst checks the index
// only.
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
// digests make the config digest.
var configFiles = []string{"testdata/contracts/cases.json", "testdata/contracts/registry.json", "testdata/demo/registry.json",
	"testdata/policy/m1-demo-profile.json", "testdata/cli/ask-mainline.json"}

// sources is the closed list of evidence sources (EvidenceRef.source): the
// layer a file comes from. live_cli is reserved for M1-E3.
var sources = map[string]string{
	"provenance/":            "provenance",
	"tests/contracts":        "contracts_test",
	"tests/policy":           "policy_test",
	"tests/store":            "store_integration",
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

func run(args []string, stdout, stderr io.Writer) int {
	fl := flag.NewFlagSet("m1-fixture", flag.ContinueOnError)
	fl.SetOutput(stderr)
	out := fl.String("out", "artifacts/m1", "where the new run directory is made")
	skipBrowser := fl.Bool("skip-browser", false, "do not run scripts/verify/browser-ui.mjs (its integration checks are then missing from the bundle)")
	if fl.Parse(args) != nil || fl.NArg() != 0 {
		return 2
	}
	fail := func(format string, a ...any) int { fmt.Fprintf(stderr, "m1-fixture: "+format+"\n", a...); return 2 }
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
	for _, d := range []string{"provenance", "tests", "e2e"} {
		os.Mkdir(filepath.Join(dir, d), 0o700)
	}

	// Provenance.
	commit := strings.TrimSpace(capture("git", "rev-parse", "HEAD"))
	patch := capture("git", "diff", "HEAD", "--binary")
	status := capture("git", "status", "--porcelain")
	os.WriteFile(filepath.Join(dir, "provenance/git-status.txt"), []byte(status), 0o600)
	configs := map[string]string{}
	var cfg strings.Builder
	for _, f := range configFiles {
		b, err := os.ReadFile(f)
		if err != nil {
			return fail("config %s: %v", f, err)
		}
		configs[f] = digest(b)
		fmt.Fprintf(&cfg, "%s\x00%s\n", f, configs[f])
	}
	cj, _ := json.MarshalIndent(configs, "", "  ")
	os.WriteFile(filepath.Join(dir, "provenance/config-files.json"), append(cj, '\n'), 0o600)
	versions := map[string]string{"go": strings.TrimSpace(capture("go", "version")), "node": strings.TrimSpace(capture("node", "--version")),
		"os": runtime.GOOS + "/" + runtime.GOARCH, "case_manifest": cases.ManifestID, "fake_cli": "fake-claude (no model, no network)"}

	// The commands, each with its exit code and output.
	steps := []step{
		{"tests/contracts.jsonl", []string{"go", "test", "-count=1", "-json", "-run", "^TestContractCasesFromManifest$", "./internal/contracts/"}, nil},
		{"tests/policy.jsonl", []string{"go", "test", "-count=1", "-json", "-run", "^TestAcceptedPolicyCases$", "./internal/policy/"}, nil},
		{"tests/store.jsonl", []string{"go", "test", "-count=1", "-json", "-run", "^TestAcceptedIntegrationCases$", "./internal/store/"}, nil},
		{"tests/e2e.jsonl", []string{"go", "test", "-count=1", "-json", "-run", "^TestM1FixtureCases$", "./cmd/demo/"}, []string{"APA_M1_RUN_DIR=" + mustAbs(filepath.Join(dir, "e2e"))}},
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
		c := exec.Command(s.cmd[0], s.cmd[1:]...)
		c.Env = append(os.Environ(), s.env...)
		var buf bytes.Buffer
		c.Stdout, c.Stderr = &buf, &buf
		err := c.Run()
		code := 0
		if err != nil {
			code = -1
			if ee, ok := errors.AsType[*exec.ExitError](err); ok {
				code = ee.ExitCode()
			}
		}
		commands = append(commands, contracts.CommandResult{Command: strings.Join(s.cmd, " "), ExitCode: code})
		os.MkdirAll(filepath.Dir(filepath.Join(dir, s.name)), 0o700)
		os.WriteFile(filepath.Join(dir, s.name), buf.Bytes(), 0o600)
		outputs[s.name] = buf.Bytes()
	}

	reqs, reqOK := requirementsIndex(outputs, *skipBrowser)
	rj, _ := json.MarshalIndent(reqs, "", "  ")
	os.Mkdir(filepath.Join(dir, "requirements"), 0o700)
	os.WriteFile(filepath.Join(dir, "requirements/index.json"), append(rj, '\n'), 0o600)

	m, err := assemble(dir, cases, outputs, runID, commit, digest([]byte(patch)), digest([]byte(cfg.String())), caseDigest, versions, commands)
	if err != nil {
		return fail("%v", err)
	}
	if leaks := scanSecrets(dir); len(leaks) > 0 {
		writeEnd(dir, runID, nil, "secret_found")
		return fail("a bare token-like value in %v; the bundle is marked incomplete", leaks)
	}
	if err := m.ValidateAgainst(cases); err != nil {
		writeEnd(dir, runID, nil, "index_invalid")
		return fail("the manifest index does not validate: %v", err)
	}
	body, _ := json.MarshalIndent(m, "", "  ")
	body = append(body, '\n')
	if err := os.WriteFile(filepath.Join(dir, "manifest.json"), body, 0o600); err != nil {
		return fail("%v", err)
	}
	writeEnd(dir, runID, body, "complete")

	n := map[string]int{}
	for _, r := range m.Results {
		n[r.Verdict]++
	}
	fmt.Fprintf(stdout, "m1-fixture: %s: %d PASS, %d FAIL, %d INCONCLUSIVE of %d cases; %d capabilities not_evaluated (the producer's own verdicts; make verify-m1 checks independently)\n",
		dir, n["PASS"], n["FAIL"], n["INCONCLUSIVE"], len(m.Results), len(m.NotEvaluated))
	fmt.Fprintf(stdout, "m1-fixture: integration requirements (docs/m1-closeout-gaps.md 3.2): all checks passed: %v\n", reqOK)
	for _, c := range commands {
		if c.ExitCode != 0 {
			return 1
		}
	}
	if !reqOK {
		return 1
	}
	if n["FAIL"]+n["INCONCLUSIVE"] > 0 {
		return 1
	}
	return 0
}

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
			rel, _ := filepath.Rel(dir, p)
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
		ids[f] = id
		m.Evidence = append(m.Evidence, contracts.EvidenceRef{EvidenceID: id, Path: f, Digest: digest(b), EvidenceMode: "fixture", Source: src})
	}
	tests := map[string]map[string]string{}
	for name, out := range outputs {
		if strings.HasSuffix(name, ".jsonl") {
			tests[name] = subtests(out)
		}
	}
	for _, c := range cases.Cases {
		r := contracts.ClaimResult{CaseID: c.CaseID, EvidenceIDs: []string{}}
		switch {
		case c.Layer == "integration" && slices.Contains(e2eCases, c.CaseID):
			r = e2eResult(c, dir, tests["tests/e2e.jsonl"]["TestM1FixtureCases/"+c.CaseID], tests["tests/store.jsonl"]["TestAcceptedIntegrationCases/"+c.CaseID], ids)
		case c.Layer == "integration":
			st := tests["tests/store.jsonl"]["TestAcceptedIntegrationCases/"+c.CaseID]
			r.Verdict, r.Reason = verdict(st), "store_integration only (this host has no trusted setting for the case): TestAcceptedIntegrationCases/"+c.CaseID+" "+word(st)
			r.EvidenceIDs = []string{ids["tests/store.jsonl"]}
			if c.Operation == "policy" {
				pt := tests["tests/policy.jsonl"]["TestAcceptedPolicyCases/"+c.CaseID]
				r.Reason += "; policy_test: TestAcceptedPolicyCases/" + c.CaseID + " " + word(pt)
				r.EvidenceIDs = append(r.EvidenceIDs, ids["tests/policy.jsonl"])
				if verdict(pt) != "PASS" {
					r.Verdict = worst(r.Verdict, verdict(pt))
				}
			}
		default:
			ct := tests["tests/contracts.jsonl"]["TestContractCasesFromManifest/"+c.CaseID]
			r.Verdict, r.Reason, r.EvidenceIDs = verdict(ct), "contracts_test only (not end-to-end evidence): TestContractCasesFromManifest/"+c.CaseID+" "+word(ct), []string{ids["tests/contracts.jsonl"]}
		}
		r.CaseID = c.CaseID
		if r.Verdict == "INCONCLUSIVE" {
			r.EvidenceIDs = slices.DeleteFunc(r.EvidenceIDs, func(s string) bool { return s == "" })
		}
		m.Results = append(m.Results, r)
	}
	return m, nil
}

// e2eResult: PASS needs the host_e2e evidence file's own PASS, its test and
// the case's Store test passing; a missing file or test is INCONCLUSIVE, any
// failure FAIL.
func e2eResult(c contracts.AcceptanceCase, dir, e2eTest, storeTest string, ids map[string]string) contracts.ClaimResult {
	r := contracts.ClaimResult{CaseID: c.CaseID}
	f := "e2e/" + c.CaseID + ".json"
	var ev struct {
		Verdict  string         `json:"verdict"`
		Layer    string         `json:"layer"`
		Observed map[string]any `json:"observed"`
	}
	raw, err := os.ReadFile(filepath.Join(dir, f))
	switch {
	case err != nil || json.Unmarshal(raw, &ev) != nil || ev.Layer != "host_e2e" || ids[f] == "":
		r.Verdict, r.Reason, r.EvidenceIDs = "INCONCLUSIVE", "host_e2e evidence "+f+" missing or unreadable; TestM1FixtureCases/"+c.CaseID+" "+word(e2eTest), []string{}
	case ev.Verdict == "PASS" && e2eTest == "pass":
		r.Verdict = "PASS"
	case ev.Verdict == "FAIL" || e2eTest == "fail":
		r.Verdict = "FAIL"
	default: // the test not run or skipped
		r.Verdict = "INCONCLUSIVE"
	}
	r.Verdict = worst(r.Verdict, verdict(storeTest)) // the claim cites the Store test too
	if r.Reason == "" {
		o := ev.Observed
		r.Reason = fmt.Sprintf("host_e2e: observed %v/%v/%v, budget_deducted %v, forbidden events %v; TestM1FixtureCases/%s %s; store_integration TestAcceptedIntegrationCases/%s %s",
			o["result"], o["reason"], o["approval_status"], o["budget_deducted"], o["forbidden_events_found"], c.CaseID, word(e2eTest), c.CaseID, word(storeTest))
		r.EvidenceIDs = []string{ids[f], ids["tests/e2e.jsonl"], ids["tests/store.jsonl"]}
	}
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
// token-like value; a bundle with one is never marked complete.
func scanSecrets(dir string) []string {
	var leaks []string
	filepath.WalkDir(dir, func(p string, d fs.DirEntry, err error) error {
		if err == nil && d.Type().IsRegular() && !strings.HasSuffix(p, ".png") {
			if b, err := os.ReadFile(p); err == nil && tokenLike.Match(b) {
				rel, _ := filepath.Rel(dir, p)
				leaks = append(leaks, rel)
			}
		}
		return nil
	})
	return leaks
}

// writeEnd is the end marker, written last: the run ID, the manifest's
// file digest and whether the bundle is complete.
func writeEnd(dir, runID string, manifest []byte, state string) {
	end := map[string]string{"run_id": runID, "state": state}
	if manifest != nil {
		end["manifest_digest"] = digest(manifest)
	}
	b, _ := json.MarshalIndent(end, "", "  ")
	os.WriteFile(filepath.Join(dir, "run-end.json"), append(b, '\n'), 0o600)
}

func digest(b []byte) string {
	h := sha256.Sum256(b)
	return "sha256:" + hex.EncodeToString(h[:])
}

func capture(name string, args ...string) string {
	out, _ := exec.Command(name, args...).Output()
	return string(out)
}

func mustAbs(p string) string {
	a, err := filepath.Abs(p)
	if err != nil {
		panic(err)
	}
	return a
}
