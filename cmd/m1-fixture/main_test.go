package main

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"slices"
	"strings"
	"testing"

	"github.com/SIMPLYBOYS/agent-payment-authority/internal/contracts"
	"github.com/SIMPLYBOYS/agent-payment-authority/internal/m1evidence"
)

func manifest(t *testing.T) contracts.CaseManifest {
	var m contracts.CaseManifest
	raw, err := os.ReadFile("../../testdata/contracts/cases.json")
	if err != nil || json.Unmarshal(raw, &m) != nil || m.RequireAccepted() != nil {
		t.Fatalf("case manifest: %v", err)
	}
	return m
}

// stubChecks replaces the structured-evidence checks: every record checks
// clean except the cases named in bad. The real checks run on real data in
// internal/m1evidence, internal/store and cmd/demo.
func stubChecks(t *testing.T, bad ...string) {
	ci, cc := checkIntegration, checkContract
	t.Cleanup(func() { checkIntegration, checkContract = ci, cc })
	checkIntegration = func(c contracts.AcceptanceCase, _ map[string]string, _ []m1evidence.Task) (m1evidence.Observed, []string) {
		if slices.Contains(bad, c.CaseID) {
			return m1evidence.Observed{}, []string{"stub problem"}
		}
		return m1evidence.Observed{Result: c.Expected.Result}, nil
	}
	checkContract = func(c contracts.AcceptanceCase, _ m1evidence.Contract) (m1evidence.ContractObserved, []string) {
		if slices.Contains(bad, c.CaseID) {
			return m1evidence.ContractObserved{}, []string{"stub problem"}
		}
		return m1evidence.ContractObserved{Result: c.Expected.Result}, nil
	}
}

// passing is what a passing run's tests report, by log.
func passing(cases contracts.CaseManifest) map[string]map[string]string {
	tests := map[string]map[string]string{"tests/contracts.jsonl": {}, "tests/policy.jsonl": {}, "tests/store.jsonl": {}, "tests/e2e.jsonl": {}}
	for _, c := range cases.Cases {
		if c.Layer != "integration" {
			tests["tests/contracts.jsonl"]["TestContractCasesFromManifest/"+c.CaseID] = "pass"
			continue
		}
		tests["tests/store.jsonl"]["TestAcceptedIntegrationCases/"+c.CaseID] = "pass"
		if c.Operation == "policy" {
			tests["tests/policy.jsonl"]["TestAcceptedPolicyCases/"+c.CaseID] = "pass"
		}
		if slices.Contains(e2eCases, c.CaseID) {
			tests["tests/e2e.jsonl"]["TestM1FixtureCases/"+c.CaseID] = "pass"
		}
	}
	return tests
}

func goTestJSON(results map[string]string) []byte {
	var b strings.Builder
	for test, action := range results {
		fmt.Fprintf(&b, `{"Action":"run","Test":%q}`+"\n"+`{"Action":%q,"Test":%q,"Elapsed":0.01}`+"\n", test, action, test)
	}
	return []byte(b.String())
}

// structured writes the case files a run's tests leave, under sub ("e2e",
// "store" or "contracts") of dir; the tasks are empty.
func structured(t *testing.T, cases contracts.CaseManifest, dir, sub string) {
	os.MkdirAll(filepath.Join(dir, sub), 0o700)
	for _, c := range cases.Cases {
		var v map[string]any
		switch {
		case sub == "e2e" && slices.Contains(e2eCases, c.CaseID):
			v = map[string]any{"case_id": c.CaseID, "layer": "host_e2e", "verdict": "PASS", "store": []any{}}
		case sub == "store" && c.Layer == "integration":
			v = map[string]any{"case_id": c.CaseID, "layer": "store_integration", "tasks": []any{}}
		case sub == "contracts" && c.Layer == "contracts":
			v = map[string]any{"case_id": c.CaseID, "layer": "contracts_test", "operation": c.Operation, "parameters": c.Parameters,
				"observed": map[string]string{"result": c.Expected.Result}}
		default:
			continue
		}
		b, _ := json.Marshal(v)
		if err := os.WriteFile(filepath.Join(dir, sub, c.CaseID+".json"), b, 0o600); err != nil {
			t.Fatal(err)
		}
	}
}

// fixture makes a run directory as a passing run leaves it, with the given
// changes, and the test outputs (go test -json lines).
func fixture(t *testing.T, cases contracts.CaseManifest, change func(dir string, out map[string]map[string]string)) (string, map[string][]byte) {
	dir := t.TempDir()
	tests := passing(cases)
	os.MkdirAll(filepath.Join(dir, "tests"), 0o700)
	os.MkdirAll(filepath.Join(dir, "provenance"), 0o700)
	os.WriteFile(filepath.Join(dir, "provenance/git-status.txt"), []byte("?? demo\n"), 0o600)
	for _, sub := range []string{"e2e", "store", "contracts"} {
		structured(t, cases, dir, sub)
	}
	if change != nil {
		change(dir, tests)
	}
	outputs := map[string][]byte{}
	for name, results := range tests {
		outputs[name] = goTestJSON(results)
		os.WriteFile(filepath.Join(dir, name), outputs[name], 0o600)
	}
	return dir, outputs
}

func build(t *testing.T, cases contracts.CaseManifest, dir string, outputs map[string][]byte, runID string) contracts.RunManifest {
	t.Helper()
	h, _ := cases.Hash()
	m, err := assemble(dir, cases, outputs, runID, strings.Repeat("a", 40), digest(nil), digest([]byte("c")), h,
		map[string]string{"go": "go1"}, []contracts.CommandResult{{Command: "go test", ExitCode: 0}})
	if err != nil {
		t.Fatal(err)
	}
	return m
}

func results(m contracts.RunManifest) map[string]contracts.ClaimResult {
	out := map[string]contracts.ClaimResult{}
	for _, r := range m.Results {
		out[r.CaseID] = r
	}
	return out
}

func TestAssemblePassesAndValidates(t *testing.T) {
	stubChecks(t)
	cases := manifest(t)
	dir, outputs := fixture(t, cases, nil)
	m := build(t, cases, dir, outputs, "run-1")
	if err := m.ValidateAgainst(cases); err != nil {
		t.Fatal(err)
	}
	for _, r := range m.Results {
		if r.Verdict != "PASS" {
			t.Errorf("%s: %s (%s)", r.CaseID, r.Verdict, r.Reason)
		}
	}
	if !reflect.DeepEqual(m.NotEvaluated, contracts.CapabilityIDs) {
		t.Errorf("not_evaluated %v", m.NotEvaluated)
	}
}

// The same inputs make the same manifest apart from the run ID.
func TestAssembleDeterministic(t *testing.T) {
	stubChecks(t)
	cases := manifest(t)
	dir, outputs := fixture(t, cases, nil)
	a := build(t, cases, dir, outputs, "run-a")
	b := build(t, cases, dir, outputs, "run-b")
	b.RunID = a.RunID
	ja, _ := json.Marshal(a)
	jb, _ := json.Marshal(b)
	if string(ja) != string(jb) {
		t.Fatal("two assemblies of the same run differ")
	}
}

// Each claim names evidence of its 3.1 layer only: the 13 E2E cases
// host_e2e (with the case's Store record beside it), the other integration
// cases store_integration (and the policy test), the contracts cases
// contracts_test.
func TestLayers(t *testing.T) {
	stubChecks(t)
	cases := manifest(t)
	dir, outputs := fixture(t, cases, nil)
	m := build(t, cases, dir, outputs, "run-1")
	src := map[string]string{}
	for _, e := range m.Evidence {
		src[e.EvidenceID] = e.Source
	}
	res := results(m)
	e2e, store := 0, 0
	for _, c := range cases.Cases {
		var got []string
		for _, id := range res[c.CaseID].EvidenceIDs {
			got = append(got, src[id])
		}
		var want []string
		switch {
		case c.Layer == "integration" && slices.Contains(e2eCases, c.CaseID):
			want, e2e = []string{"host_e2e", "host_e2e", "store_integration", "store_integration"}, e2e+1
		case c.Layer == "integration" && c.Operation == "policy":
			want, store = []string{"store_integration", "store_integration", "policy_test"}, store+1
		case c.Layer == "integration":
			want, store = []string{"store_integration", "store_integration"}, store+1
		default:
			want = []string{"contracts_test", "contracts_test"}
		}
		if !slices.Equal(got, want) {
			t.Errorf("%s (%s): evidence sources %v, want %v", c.CaseID, c.Layer, got, want)
		}
	}
	if e2e != 13 || store != 3 {
		t.Errorf("%d host_e2e and %d Store-only integration cases, want 13 and 3 (docs/m1-evidence.md)", e2e, store)
	}
}

// Reverse checks: a missing, mislabelled or unreadable record, a check
// that finds a problem, a failing or missing test, a file outside the
// source list.
func TestAssembleReverse(t *testing.T) {
	cases := manifest(t)
	write := func(rel, body string) func(string, map[string]map[string]string) {
		return func(d string, _ map[string]map[string]string) {
			os.WriteFile(filepath.Join(d, rel), []byte(body), 0o600)
		}
	}
	remove := func(rel string) func(string, map[string]map[string]string) {
		return func(d string, _ map[string]map[string]string) { os.Remove(filepath.Join(d, rel)) }
	}
	test := func(log, name, action string) func(string, map[string]map[string]string) {
		return func(_ string, o map[string]map[string]string) {
			if action == "" {
				delete(o[log], name)
			} else {
				o[log][name] = action
			}
		}
	}
	for name, tc := range map[string]struct {
		change func(dir string, out map[string]map[string]string)
		bad    string // the stub check finds a problem in this case
		cid    string
		want   string
	}{
		"e2e record missing":          {remove("e2e/ask.json"), "", "ask", "INCONCLUSIVE"},
		"e2e record labelled store":   {write("e2e/ask.json", `{"layer":"store_integration","verdict":"PASS","store":[]}`), "", "ask", "INCONCLUSIVE"},
		"e2e record unreadable":       {write("e2e/ask.json", `{"layer":`), "", "ask", "INCONCLUSIVE"},
		"e2e test recorded FAIL":      {write("e2e/ask.json", `{"layer":"host_e2e","verdict":"FAIL","store":[]}`), "", "ask", "FAIL"},
		"e2e test failed":             {test("tests/e2e.jsonl", "TestM1FixtureCases/restart", "fail"), "", "restart", "FAIL"},
		"e2e test missing":            {test("tests/e2e.jsonl", "TestM1FixtureCases/restart", ""), "", "restart", "INCONCLUSIVE"},
		"e2e case's store missing":    {remove("store/deny-asset.json"), "", "deny-asset", "INCONCLUSIVE"},
		"e2e case's store test fails": {test("tests/store.jsonl", "TestAcceptedIntegrationCases/deny-asset", "fail"), "", "deny-asset", "FAIL"},
		"store record missing":        {remove("store/deny-expired.json"), "", "deny-expired", "INCONCLUSIVE"},
		"store test skipped":          {test("tests/store.jsonl", "TestAcceptedIntegrationCases/deny-expired", "skip"), "", "deny-expired", "INCONCLUSIVE"},
		"policy test failed":          {test("tests/policy.jsonl", "TestAcceptedPolicyCases/allow-unavailable", "fail"), "", "allow-unavailable", "FAIL"},
		"contracts record missing":    {remove("contracts/consumer-b.json"), "", "consumer-b", "INCONCLUSIVE"},
		"contracts test missing":      {test("tests/contracts.jsonl", "TestContractCasesFromManifest/consumer-b", ""), "", "consumer-b", "INCONCLUSIVE"},
		"an integration check fails":  {nil, "cancel-no-purchase", "cancel-no-purchase", "FAIL"},
		"a store-only check fails":    {nil, "deny-missing-config", "deny-missing-config", "FAIL"},
		"a contracts check fails":     {nil, "cross-consumer", "cross-consumer", "FAIL"},
	} {
		t.Run(name, func(t *testing.T) {
			if !slices.ContainsFunc(cases.Cases, func(c contracts.AcceptanceCase) bool { return c.CaseID == tc.cid }) {
				t.Fatalf("no case %s", tc.cid)
			}
			stubChecks(t, tc.bad)
			dir, outputs := fixture(t, cases, tc.change)
			m := build(t, cases, dir, outputs, "run-1")
			if got := results(m)[tc.cid]; got.Verdict != tc.want {
				t.Fatalf("%s: %s, want %s (%s)", tc.cid, got.Verdict, tc.want, got.Reason)
			}
			if err := m.ValidateAgainst(cases); err != nil {
				t.Fatalf("still a valid index: %v", err)
			}
		})
	}
	stubChecks(t)
	dir, outputs := fixture(t, cases, nil)
	os.WriteFile(filepath.Join(dir, "stray.txt"), []byte("x"), 0o600)
	if _, err := assemble(dir, cases, outputs, "run-1", "", "", "", "", nil, nil); err == nil || !strings.Contains(err.Error(), "closed list") {
		t.Fatalf("a file outside the source list: %v", err)
	}
	dir, outputs = fixture(t, cases, nil)
	os.Symlink("/etc/hosts", filepath.Join(dir, "e2e/link.json"))
	if _, err := assemble(dir, cases, outputs, "run-1", "", "", "", "", nil, nil); err == nil {
		t.Fatal("a symlink in the run directory was indexed")
	}
}

// With the real checks, logs that all say PASS carry no claim: records
// without the stored data or the inputs give no PASS.
func TestPassingLogsAreNotEvidence(t *testing.T) {
	cases := manifest(t)
	dir, outputs := fixture(t, cases, nil)
	for _, r := range build(t, cases, dir, outputs, "run-1").Results {
		if r.Verdict == "PASS" {
			t.Errorf("%s: PASS from logs alone (%s)", r.CaseID, r.Reason)
		}
	}
}

func TestSubtests(t *testing.T) {
	out := []byte(`{"Action":"run","Test":"T/a"}
{"Action":"output","Test":"T/a","Output":"--- PASS"}
{"Action":"pass","Test":"T/a"}
{"Action":"fail","Test":"T/b"}
{"Action":"skip","Test":"T/c"}
not json
{"Action":"pass"}
`)
	if got, want := subtests(out), map[string]string{"T/a": "pass", "T/b": "fail", "T/c": "skip"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("%v", got)
	}
}

// The scan flags a bare 64-hex value (a demo token's shape) and not a
// sha256: digest or a 0x chain value, and cannot be passed by a file it
// cannot read; the end marker carries the manifest's digest.
func TestSecretScanAndEndMarker(t *testing.T) {
	tok := strings.Repeat("0123456789abcdef", 4)
	dir := t.TempDir()
	os.WriteFile(filepath.Join(dir, "ok.json"), []byte(`{"d":"sha256:`+tok+`","tx":"0x`+tok+`","c":"`+tok[:40]+`"}`), 0o600)
	if leaks, err := scanSecrets(dir); len(leaks) != 0 || err != nil {
		t.Fatalf("flagged %v %v", leaks, err)
	}
	for _, s := range []string{`{"token":"` + tok + `"}`, tok, "Bearer " + tok + "\n"} {
		os.WriteFile(filepath.Join(dir, "bad.log"), []byte(s), 0o600)
		if leaks, err := scanSecrets(dir); !slices.Equal(leaks, []string{"bad.log"}) || err != nil {
			t.Fatalf("%q: %v %v", s, leaks, err)
		}
	}
	os.Remove(filepath.Join(dir, "bad.log"))
	os.WriteFile(filepath.Join(dir, "unreadable.log"), []byte(tok), 0o000)
	if _, err := scanSecrets(dir); err == nil {
		t.Fatal("an unreadable file scanned as clean")
	}
	os.Remove(filepath.Join(dir, "unreadable.log"))
	if err := writeEnd(dir, "run-1", []byte("manifest"), "complete"); err != nil {
		t.Fatal(err)
	}
	var end map[string]string
	raw, _ := os.ReadFile(filepath.Join(dir, "run-end.json"))
	json.Unmarshal(raw, &end)
	if end["run_id"] != "run-1" || end["state"] != "complete" || end["manifest_digest"] != digest([]byte("manifest")) {
		t.Fatalf("end marker %v", end)
	}
	writeEnd(dir, "run-1", nil, "secret_found")
	raw, _ = os.ReadFile(filepath.Join(dir, "run-end.json"))
	end = nil
	json.Unmarshal(raw, &end)
	if end["state"] != "secret_found" || end["manifest_digest"] != "" {
		t.Fatalf("an incomplete end marker %v", end)
	}
	os.Remove(filepath.Join(dir, "run-end.json"))
	os.Mkdir(filepath.Join(dir, "run-end.json"), 0o700)
	if err := writeEnd(dir, "run-1", nil, "complete"); err == nil {
		t.Fatal("an end marker that was not written reported as written")
	}
}

func TestRequirementsIndex(t *testing.T) {
	outputs := map[string][]byte{
		"tests/requirements-go.jsonl": []byte(`{"Action":"pass","Test":"TestLiveRun"}
{"Action":"pass","Test":"TestIntegrationAskDenyAndAccess"}
{"Action":"pass","Test":"TestCancelRows"}
{"Action":"pass","Test":"TestCancelAfterCLITimeout"}
`),
		"tests/requirements-web.tap": []byte("ok 2 - a switch drops every late reply of the old viewer\nok 3 - duplicate events are shown once and animated once\nok 7 - hostile strings stay strings; nothing reads as paid\n# pass 3\n"),
		"browser/browser-ui.out":     []byte("PASS nothing says paid or delivered\nPASS hostile strings render as text only\nPASS refresh + sign-in restores task-a from the Store\nPASS 3.4: CLI timeout: its own state\n"),
	}
	status := func(idx []map[string]any) map[string]string {
		out := map[string]string{}
		for _, r := range idx {
			for _, c := range r["checks"].([]reqCheck) {
				out[c.Test] = c.Status
			}
		}
		return out
	}
	idx, ok := requirementsIndex(outputs, false)
	if !ok {
		t.Fatalf("%v", status(idx))
	}
	// Reverse: a failure, a missing test, a prefix with no match, the browser not run.
	outputs["tests/requirements-go.jsonl"] = append(outputs["tests/requirements-go.jsonl"], []byte(`{"Action":"fail","Test":"TestCancelConcurrent"}`+"\n")...)
	outputs["tests/requirements-web.tap"] = []byte("not ok 2 - a switch drops every late reply of the old viewer\nok 7 - hostile strings stay strings; nothing reads as paid\n")
	outputs["browser/browser-ui.out"] = []byte("PASS nothing says paid or delivered\nFAIL hostile strings render as text only\nPASS refresh + sign-in restores task-a from the Store\n")
	idx, ok = requirementsIndex(outputs, false)
	want := map[string]string{"TestCancel*": "fail", "a switch drops every late reply of the old viewer": "fail", "duplicate events are shown once and animated once": "not_found",
		"hostile strings render as text only": "fail", "3.4:*": "not_found", "TestLiveRun": "pass"}
	got := status(idx)
	for k, v := range want {
		if got[k] != v {
			t.Errorf("%s: %s, want %s", k, got[k], v)
		}
	}
	if ok {
		t.Error("failures reported as all passed")
	}
	if idx, ok = requirementsIndex(outputs, true); ok || status(idx)["nothing says paid or delivered"] != "not_run" {
		t.Errorf("skip-browser: %v %v", ok, status(idx))
	}
}

// fakeRun runs run() from the repository root with stand-ins for every
// command and provenance query: no test, browser or git runs. before is
// called with the new run directory before each step (by its evidence
// name) and before each query (by "query:<name>"); it may break the
// directory or fail the query.
func fakeRun(t *testing.T, failing string, before func(dir, what string) error) (code int, dir, stdout, stderr string) {
	t.Helper()
	stubChecks(t)
	cases := manifest(t)
	out := t.TempDir()
	t.Chdir("../..")
	rs, cp := runStep, capture
	t.Cleanup(func() { runStep, capture = rs, cp })
	runDir := func() string {
		entries, _ := os.ReadDir(out)
		if len(entries) != 1 {
			t.Fatalf("%d run directories", len(entries))
		}
		return filepath.Join(out, entries[0].Name())
	}
	capture = func(name string, args ...string) (string, error) {
		what := "query:" + name + " " + strings.Join(args, " ")
		if before != nil {
			if err := before(runDir(), what); err != nil {
				return "", err
			}
		}
		switch {
		case name == "git" && args[0] == "rev-parse":
			return strings.Repeat("c", 40) + "\n", nil
		case name == "go":
			return "go version test\n", nil
		case name == "node":
			return "v0-test\n", nil
		}
		return "", nil // no diff, a clean tree
	}
	tests := passing(cases)
	if failing != "" {
		tests["tests/e2e.jsonl"]["TestM1FixtureCases/"+failing] = "fail"
	}
	runStep = func(s step) ([]byte, int, error) {
		dir := runDir()
		if before != nil {
			if err := before(dir, s.name); err != nil {
				return nil, -1, err
			}
		}
		for _, e := range s.env {
			if v, ok := strings.CutPrefix(e, "APA_M1_RUN_DIR="); ok {
				structured(t, cases, dir, filepath.Base(v))
			}
		}
		switch s.name {
		case "tests/requirements-go.jsonl":
			return goTestJSON(map[string]string{"TestLiveRun": "pass", "TestIntegrationAskDenyAndAccess": "pass", "TestCancelRows": "pass"}), 0, nil
		case "tests/requirements-web.tap":
			return []byte("ok 1 - a switch drops every late reply of the old viewer\nok 2 - duplicate events are shown once and animated once\nok 3 - hostile strings stay strings; nothing reads as paid\n"), 0, nil
		case "browser/browser-ui.out":
			return []byte("PASS nothing says paid or delivered\nPASS hostile strings render as text only\nPASS refresh + sign-in restores task-a from the Store\nPASS 3.4: cancel\n"), 0, nil
		}
		code := 0
		for _, a := range tests[s.name] {
			if a == "fail" {
				code = 1
			}
		}
		return goTestJSON(tests[s.name]), code, nil
	}
	var so, se bytes.Buffer
	code = run([]string{"-out", out}, &so, &se)
	return code, runDir(), so.String(), se.String()
}

func endMarker(t *testing.T, dir string) map[string]string {
	t.Helper()
	raw, err := os.ReadFile(filepath.Join(dir, "run-end.json"))
	if err != nil {
		return nil
	}
	var end map[string]string
	if err := json.Unmarshal(raw, &end); err != nil {
		t.Fatalf("end marker: %v", err)
	}
	return end
}

// R2: a bundle is reported complete only when everything was saved; a save
// or provenance failure exits 2 without a completion summary and keeps
// what was saved.
func TestRunSaving(t *testing.T) {
	t.Run("complete and passing", func(t *testing.T) {
		code, dir, so, se := fakeRun(t, "", nil)
		end := endMarker(t, dir)
		manifest, _ := os.ReadFile(filepath.Join(dir, "manifest.json"))
		if code != exitPass || !strings.Contains(so, "(complete): 31 PASS") || end["state"] != "complete" || end["manifest_digest"] != digest(manifest) {
			t.Fatalf("exit %d, end %v\n%s\n%s", code, end, so, se)
		}
	})
	t.Run("complete with a failing case", func(t *testing.T) {
		code, dir, so, se := fakeRun(t, "restart", nil)
		if code != exitFailures || !strings.Contains(so, "(complete)") || endMarker(t, dir)["state"] != "complete" {
			t.Fatalf("exit %d\n%s\n%s", code, so, se)
		}
	})
	blockEnd := func(dir, what string) error {
		if what == "tests/contracts.jsonl" {
			return os.Mkdir(filepath.Join(dir, "run-end.json"), 0o700)
		}
		return nil
	}
	t.Run("the end marker cannot be written", func(t *testing.T) {
		code, dir, so, se := fakeRun(t, "", blockEnd)
		if code != exitIncomplete || so != "" || !strings.Contains(se, "INCOMPLETE") {
			t.Fatalf("exit %d\n%s\n%s", code, so, se)
		}
		if _, err := os.Stat(filepath.Join(dir, "manifest.json")); err != nil {
			t.Fatalf("what was saved is kept: %v", err)
		}
	})
	t.Run("an evidence file cannot be written", func(t *testing.T) {
		code, dir, so, se := fakeRun(t, "", func(dir, what string) error {
			if what == "tests/contracts.jsonl" {
				return os.Mkdir(filepath.Join(dir, "tests/policy.jsonl"), 0o700)
			}
			return nil
		})
		_, err := os.Stat(filepath.Join(dir, "manifest.json"))
		if code != exitIncomplete || so != "" || !errors.Is(err, os.ErrNotExist) || endMarker(t, dir)["state"] != "save_failed" {
			t.Fatalf("exit %d, manifest %v, end %v\n%s\n%s", code, err, endMarker(t, dir), so, se)
		}
		if _, err := os.Stat(filepath.Join(dir, "tests/contracts.jsonl")); err != nil {
			t.Fatalf("what was saved is kept: %v", err)
		}
	})
	for _, q := range []string{"git rev-parse HEAD", "git diff HEAD --binary", "git status --porcelain", "node --version"} {
		t.Run("the query "+q+" fails", func(t *testing.T) {
			ran := false
			code, dir, so, se := fakeRun(t, "", func(_, what string) error {
				if what == "query:"+q {
					return errors.New("query failed")
				}
				if !strings.HasPrefix(what, "query:") {
					ran = true
				}
				return nil
			})
			_, err := os.Stat(filepath.Join(dir, "manifest.json"))
			if code != exitIncomplete || so != "" || ran || !errors.Is(err, os.ErrNotExist) || endMarker(t, dir)["state"] != "provenance_failed" {
				t.Fatalf("exit %d, steps ran %v, end %v\n%s\n%s", code, ran, endMarker(t, dir), so, se)
			}
		})
	}
	t.Run("a failure whose marker cannot be written either", func(t *testing.T) {
		code, dir, so, se := fakeRun(t, "", func(dir, what string) error {
			if what == "query:git diff HEAD --binary" {
				os.Mkdir(filepath.Join(dir, "run-end.json"), 0o700)
				return errors.New("query failed")
			}
			return nil
		})
		if code != exitIncomplete || so != "" || endMarker(t, dir) != nil || !strings.Contains(se, "could not be written either") {
			t.Fatalf("exit %d\n%s\n%s", code, so, se)
		}
	})
}
