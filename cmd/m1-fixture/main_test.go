package main

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"slices"
	"strings"
	"testing"

	"github.com/SIMPLYBOYS/agent-payment-authority/internal/contracts"
)

func manifest(t *testing.T) contracts.CaseManifest {
	var m contracts.CaseManifest
	raw, err := os.ReadFile("../../testdata/contracts/cases.json")
	if err != nil || json.Unmarshal(raw, &m) != nil || m.RequireAccepted() != nil {
		t.Fatalf("case manifest: %v", err)
	}
	return m
}

// fixture makes a run directory as a passing run leaves it, with the given
// changes, and the test outputs (go test -json lines).
func fixture(t *testing.T, cases contracts.CaseManifest, change func(dir string, out map[string]map[string]string)) (string, map[string][]byte) {
	dir := t.TempDir()
	tests := map[string]map[string]string{"tests/contracts.jsonl": {}, "tests/policy.jsonl": {}, "tests/store.jsonl": {}, "tests/e2e.jsonl": {}}
	os.MkdirAll(filepath.Join(dir, "e2e"), 0o700)
	os.MkdirAll(filepath.Join(dir, "tests"), 0o700)
	os.MkdirAll(filepath.Join(dir, "provenance"), 0o700)
	os.WriteFile(filepath.Join(dir, "provenance/git-status.txt"), []byte("?? demo\n"), 0o600)
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
			b, _ := json.Marshal(map[string]any{"case_id": c.CaseID, "layer": "host_e2e", "verdict": "PASS",
				"observed": map[string]any{"result": c.Expected.Result, "budget_deducted": "0", "forbidden_events_found": nil}})
			os.WriteFile(filepath.Join(dir, "e2e", c.CaseID+".json"), b, 0o600)
		}
	}
	if change != nil {
		change(dir, tests)
	}
	outputs := map[string][]byte{}
	for name, results := range tests {
		var b strings.Builder
		for test, action := range results {
			fmt.Fprintf(&b, `{"Action":"run","Test":%q}`+"\n"+`{"Action":%q,"Test":%q,"Elapsed":0.01}`+"\n", test, action, test)
		}
		outputs[name] = []byte(b.String())
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
// host_e2e (with the Store test beside it), the other integration cases
// store_integration (and policy_test), the contracts cases contracts_test.
func TestLayers(t *testing.T) {
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
			want, e2e = []string{"host_e2e", "host_e2e", "store_integration"}, e2e+1
		case c.Layer == "integration" && c.Operation == "policy":
			want, store = []string{"store_integration", "policy_test"}, store+1
		case c.Layer == "integration":
			want, store = []string{"store_integration"}, store+1
		default:
			want = []string{"contracts_test"}
		}
		if !slices.Equal(got, want) {
			t.Errorf("%s (%s): evidence sources %v, want %v", c.CaseID, c.Layer, got, want)
		}
	}
	if e2e != 13 || store != 3 {
		t.Errorf("%d host_e2e and %d Store-only integration cases, want 13 and 3 (docs/m1-evidence.md)", e2e, store)
	}
}

// Reverse checks: a missing or mislabelled E2E file, a failing or missing
// test, a file outside the source list.
func TestAssembleReverse(t *testing.T) {
	cases := manifest(t)
	for name, tc := range map[string]struct {
		change func(dir string, out map[string]map[string]string)
		cid    string
		want   string
	}{
		"e2e file missing": {func(d string, _ map[string]map[string]string) { os.Remove(filepath.Join(d, "e2e/ask.json")) }, "ask", "INCONCLUSIVE"},
		"e2e file labelled store": {func(d string, _ map[string]map[string]string) {
			os.WriteFile(filepath.Join(d, "e2e/ask.json"), []byte(`{"layer":"store_integration","verdict":"PASS"}`), 0o600)
		}, "ask", "INCONCLUSIVE"},
		"e2e file says FAIL": {func(d string, _ map[string]map[string]string) {
			os.WriteFile(filepath.Join(d, "e2e/ask.json"), []byte(`{"layer":"host_e2e","verdict":"FAIL"}`), 0o600)
		}, "ask", "FAIL"},
		"e2e test failed": {func(_ string, o map[string]map[string]string) {
			o["tests/e2e.jsonl"]["TestM1FixtureCases/restart"] = "fail"
		}, "restart", "FAIL"},
		"e2e test missing": {func(_ string, o map[string]map[string]string) {
			delete(o["tests/e2e.jsonl"], "TestM1FixtureCases/restart")
		}, "restart", "INCONCLUSIVE"},
		"store test failed": {func(_ string, o map[string]map[string]string) {
			o["tests/store.jsonl"]["TestAcceptedIntegrationCases/deny-expired"] = "fail"
		}, "deny-expired", "FAIL"},
		"store test skipped": {func(_ string, o map[string]map[string]string) {
			o["tests/store.jsonl"]["TestAcceptedIntegrationCases/deny-expired"] = "skip"
		}, "deny-expired", "INCONCLUSIVE"},
		"policy test failed": {func(_ string, o map[string]map[string]string) {
			o["tests/policy.jsonl"]["TestAcceptedPolicyCases/allow-unavailable"] = "fail"
		}, "allow-unavailable", "FAIL"},
		"contracts test missing": {func(_ string, o map[string]map[string]string) {
			delete(o["tests/contracts.jsonl"], "TestContractCasesFromManifest/consumer-b")
		}, "consumer-b", "INCONCLUSIVE"},
		"e2e case, store test failed": {func(_ string, o map[string]map[string]string) {
			o["tests/store.jsonl"]["TestAcceptedIntegrationCases/deny-asset"] = "fail"
		}, "deny-asset", "FAIL"},
	} {
		t.Run(name, func(t *testing.T) {
			if !slices.ContainsFunc(cases.Cases, func(c contracts.AcceptanceCase) bool { return c.CaseID == tc.cid }) {
				t.Fatalf("no case %s", tc.cid)
			}
			dir, outputs := fixture(t, cases, tc.change)
			m := build(t, cases, dir, outputs, "run-1")
			if got := results(m)[tc.cid].Verdict; got != tc.want {
				t.Fatalf("%s: %s, want %s", tc.cid, got, tc.want)
			}
			if err := m.ValidateAgainst(cases); err != nil {
				t.Fatalf("still a valid index: %v", err)
			}
		})
	}
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
// sha256: digest or a 0x chain value; the end marker carries the
// manifest's digest.
func TestSecretScanAndEndMarker(t *testing.T) {
	tok := strings.Repeat("0123456789abcdef", 4)
	dir := t.TempDir()
	os.WriteFile(filepath.Join(dir, "ok.json"), []byte(`{"d":"sha256:`+tok+`","tx":"0x`+tok+`","c":"`+tok[:40]+`"}`), 0o600)
	if leaks := scanSecrets(dir); len(leaks) != 0 {
		t.Fatalf("flagged %v", leaks)
	}
	for _, s := range []string{`{"token":"` + tok + `"}`, tok, "Bearer " + tok + "\n"} {
		os.WriteFile(filepath.Join(dir, "bad.log"), []byte(s), 0o600)
		if leaks := scanSecrets(dir); !slices.Equal(leaks, []string{"bad.log"}) {
			t.Fatalf("%q: %v", s, leaks)
		}
	}
	writeEnd(dir, "run-1", []byte("manifest"), "complete")
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
