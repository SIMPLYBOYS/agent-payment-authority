package verification

import (
	"bytes"
	"encoding/json"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/SIMPLYBOYS/agent-payment-authority/internal/contracts"
)

const liveName = "m1-live-20261010T151404Z-9f8031"

var liveGolden = filepath.Join("..", "..", "testdata", "verification", "live-synthetic", liveName)

func copyLive(t *testing.T) string {
	t.Helper()
	dir := filepath.Join(t.TempDir(), liveName)
	err := filepath.WalkDir(liveGolden, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, _ := filepath.Rel(liveGolden, p)
		if d.IsDir() {
			return os.MkdirAll(filepath.Join(dir, rel), 0o700)
		}
		b, err := os.ReadFile(p)
		if err != nil {
			return err
		}
		return os.WriteFile(filepath.Join(dir, rel), b, 0o600)
	})
	if err != nil {
		t.Fatal(err)
	}
	return dir
}

func verifyLive(t *testing.T, dir string) *Report {
	t.Helper()
	r, err := Verify(dir, trusted(t), "live")
	if err != nil {
		t.Fatal(err)
	}
	return r
}

func editFile(t *testing.T, dir, rel string, f func(map[string]any)) {
	t.Helper()
	m := readMap(t, filepath.Join(dir, rel))
	f(m)
	writeMap(t, filepath.Join(dir, rel), m)
}

// editTrace changes the trace's lines (generic JSON objects).
func editTrace(t *testing.T, dir string, f func([]map[string]any) []map[string]any) {
	t.Helper()
	p := filepath.Join(dir, "live", "trace.jsonl")
	raw, _ := os.ReadFile(p)
	var lines []map[string]any
	for _, l := range bytes.Split(bytes.TrimSpace(raw), []byte("\n")) {
		d := json.NewDecoder(bytes.NewReader(l))
		d.UseNumber()
		var m map[string]any
		if err := d.Decode(&m); err != nil {
			t.Fatal(err)
		}
		lines = append(lines, m)
	}
	var out bytes.Buffer
	for _, m := range f(lines) {
		b, _ := json.Marshal(m)
		out.Write(append(b, '\n'))
	}
	os.WriteFile(p, out.Bytes(), 0o600)
}

func lineOf(lines []map[string]any, event string, n int) map[string]any {
	for _, l := range lines {
		if l["event"] == event {
			if n == 0 {
				return l
			}
			n--
		}
	}
	return nil
}

// drop removes a file from the bundle and from the index and the claim.
func drop(t *testing.T, dir, rel string) {
	os.Remove(filepath.Join(dir, rel))
	editFile(t, dir, "manifest.json", func(m map[string]any) {
		m["evidence"] = slices.DeleteFunc(m["evidence"].([]any), func(e any) bool { return e.(map[string]any)["path"] == rel })
		res := m["results"].([]any)[0].(map[string]any)
		res["evidence_ids"] = slices.DeleteFunc(res["evidence_ids"].([]any), func(id any) bool { return id == "ev-"+strings.ReplaceAll(rel, "/", "-") })
	})
}

// The synthetic live bundle passes exactly as its expected.json says, and
// is not changed by being read.
func TestLiveGolden(t *testing.T) {
	var want struct {
		Verdict  Verdict            `json:"verdict"`
		ExitCode int                `json:"exit_code"`
		Claims   map[string]Verdict `json:"claims"`
		Ask      struct {
			Verdict, Result, Reason string
			ApprovalStatus          string `json:"approval_status"`
		} `json:"ask"`
		GateMode     string   `json:"gate_mode"`
		GateResult   string   `json:"gate_result"`
		CLIVersion   string   `json:"cli_version"`
		Usage        string   `json:"usage"`
		NotEvaluated []string `json:"not_evaluated"`
	}
	b, err := os.ReadFile(filepath.Join(filepath.Dir(liveGolden), "expected.json"))
	if err != nil || json.Unmarshal(b, &want) != nil {
		t.Fatalf("expected.json: %v", err)
	}
	before := tree(t, liveGolden)
	r := verifyLive(t, liveGolden)
	if after := tree(t, liveGolden); len(after) != len(before) {
		t.Fatal("verifying changed the bundle")
	} else {
		for k, v := range before {
			if after[k] != v {
				t.Fatal("verifying changed the bundle")
			}
		}
	}
	if r.Verdict != want.Verdict || r.ExitCode() != want.ExitCode {
		t.Fatalf("%s exit %d: %v %v", r.Verdict, r.ExitCode(), r.Findings, r.Cases)
	}
	for k, v := range want.Claims {
		if r.Claims[k] != v {
			t.Errorf("claim %s: %s", k, r.Claims[k])
		}
	}
	if len(r.Cases) != 1 {
		t.Fatalf("%d cases; a live report has ask alone", len(r.Cases))
	}
	c := r.Cases[0]
	if c.CaseID != "ask" || string(c.Verdict) != want.Ask.Verdict || len(c.Observed) != 1 ||
		c.Observed[0].Result != want.Ask.Result || c.Observed[0].Reason != want.Ask.Reason || c.Observed[0].ApprovalStatus != want.Ask.ApprovalStatus {
		t.Fatalf("ask: %+v", c)
	}
	if !slices.Equal(r.NotEvaluated, want.NotEvaluated) {
		t.Errorf("not evaluated %v", r.NotEvaluated)
	}
	l := r.Live
	if l == nil || l.GateMode != want.GateMode || l.GateResult != want.GateResult || l.CLIVersion != want.CLIVersion || l.Usage != want.Usage ||
		!strings.HasPrefix(l.OriginalDecision, "Ask/HUMAN_APPROVAL_REQUIRED/pending") || l.StateAtSnapshot != "pending" || len(l.OutOfScope) != 6 ||
		!slices.Contains(l.ResidualRisks, "strict zero-plugin mode not verified") {
		t.Fatalf("live report %+v", l)
	}
	if !slices.ContainsFunc(r.Limits, func(s string) bool { return strings.Contains(s, "strict zero-plugin mode is not verified") }) {
		t.Fatal("the report does not state the residual plugin risk")
	}
}

// Semantic negatives on the live bundle, each reindexed (V1 stays PASS).
func TestLiveRecordsThatContradict(t *testing.T) {
	ask := func(edit func(*testing.T, *storedTask)) func(*testing.T, string) {
		return func(t *testing.T, dir string) {
			editTask(t, dir, "live/ask.json", 0, func(st *storedTask) { edit(t, st) })
		}
	}
	runFile := func(f func(map[string]any)) func(*testing.T, string) {
		return func(t *testing.T, dir string) { editFile(t, dir, "live/run.json", f) }
	}
	trace := func(f func([]map[string]any)) func(*testing.T, string) {
		return func(t *testing.T, dir string) {
			editTrace(t, dir, func(l []map[string]any) []map[string]any { f(l); return l })
		}
	}
	purchaseCall := func(l []map[string]any) map[string]any { return lineOf(l, "tool_call", 2) }
	for name, tc := range map[string]struct {
		change  func(*testing.T, string)
		claim   string
		verdict Verdict
	}{
		"no trace":        {func(t *testing.T, d string) { drop(t, d, "live/trace.jsonl") }, "V1", Inconclusive},
		"no Store export": {func(t *testing.T, d string) { drop(t, d, "live/ask.json") }, "V1", Inconclusive},
		"no run record":   {func(t *testing.T, d string) { drop(t, d, "live/run.json") }, "V1", Inconclusive},
		"the exception called strict in the run record": {runFile(func(m map[string]any) { m["gate_mode"] = "strict" }), "V6", Fail},
		"the exception called strict in the trace":      {trace(func(l []map[string]any) { lineOf(l, "run_start", 0)["gate_mode"] = "strict" }), "V6", Fail},
		"the residual risk not stated": {runFile(func(m map[string]any) {
			m["residual_risks"] = slices.DeleteFunc(m["residual_risks"].([]any), func(s any) bool { return s == "strict zero-plugin mode not verified" })
		}), "V6", Fail},
		"the trace's request_purchase says Deny": {func(t *testing.T, d string) {
			editTrace(t, d, func(l []map[string]any) []map[string]any { purchaseCall(l)["outcome"] = "Deny"; return l })
			editFile(t, d, "live/run.json", func(m map[string]any) {
				m["report"].(map[string]any)["tool_calls"].([]any)[2].(map[string]any)["outcome"] = "Deny"
			})
		}, "V3", Fail},
		"the run record's tool calls are not the trace's": {runFile(func(m map[string]any) {
			calls := m["report"].(map[string]any)["tool_calls"].([]any)
			m["report"].(map[string]any)["tool_calls"] = calls[:2]
		}), "V4", Fail},
		"another consumer's run":      {trace(func(l []map[string]any) { lineOf(l, "run_start", 0)["consumer_id"] = "consumer-b" }), "V7", Fail},
		"the Ask without its profile": {ask(func(t *testing.T, st *storedTask) { delete(st.Snapshots, "profile") }), "V3", Fail},
		"an event in the middle changed": {ask(func(t *testing.T, st *storedTask) {
			es := events(t, st)
			es[1].ObservedAt++
			b, _ := json.Marshal(es[1])
			h, _ := contracts.EventHash(es[1])
			st.Events[1] = stored{Body: string(b), Hash: h}
		}), "V4", Fail},
		"a payment recorded": {ask(func(t *testing.T, st *storedTask) {
			es := events(t, st)
			es[len(es)-1].Payload.Decision.PaymentStatus = "Settled"
			relink(st, es)
		}), "V5", Fail},
		"the snapshot names another last event": {runFile(func(m map[string]any) { m["snapshot"].(map[string]any)["last_sequence"] = json.Number("2") }), "V4", Fail},
		"a withdrawn Ask in an ok run": {func(t *testing.T, d string) {
			withClosure(t, d, "APPROVAL_WITHDRAWN", "withdrawn", 5)
		}, "V3", Fail},
		"an expiry before the deadline": {func(t *testing.T, d string) {
			withClosure(t, d, "APPROVAL_EXPIRED", "expired", 5)
		}, "V3", Fail},
		"other limits": {trace(func(l []map[string]any) {
			lineOf(l, "run_start", 0)["limits"].(map[string]any)["total_seconds"] = json.Number("600")
		}), "V4", Fail},
		"another prompt":      {trace(func(l []map[string]any) { lineOf(l, "run_start", 0)["prompt_sha256"] = strings.Repeat("0", 64) }), "V4", Fail},
		"the gate not passed": {trace(func(l []map[string]any) { lineOf(l, "init", 0)["gate"] = "plugins" }), "V4", Fail},
		// R2 (10/10 review): a failure the producer recorded is never
		// outweighed by the run's other fields.
		"only the producer error says the wrapper could not start": {runFile(func(m map[string]any) {
			m["producer_error"] = "apa-buyer could not be started"
		}), "V4", Fail},
		"recorded as interrupted": {runFile(func(m map[string]any) {
			m["termination"], m["producer_error"] = "interrupted", "interrupted by the operator (SIGINT or SIGTERM); the run was stopped"
		}), "V4", Fail},
		"recorded as stopped by the outer bound": {runFile(func(m map[string]any) {
			m["termination"], m["producer_error"] = "outer_bound", "the outer bound was reached; the run was stopped"
		}), "V4", Fail},
		"an unknown termination": {runFile(func(m map[string]any) { m["termination"] = "fine" }), "V4", Fail},
		"a version 1 run record": {runFile(func(m map[string]any) { m["version"] = "apa.m1.live.v1" }), "V4", Fail},
		"the gate not passed in both records": {func(t *testing.T, d string) {
			editTrace(t, d, func(l []map[string]any) []map[string]any { lineOf(l, "init", 0)["gate"] = "plugins"; return l })
			editFile(t, d, "live/run.json", func(m map[string]any) { m["gate_result"] = "plugins" })
		}, "V4", Fail},
		"an unknown trace member": {trace(func(l []map[string]any) { lineOf(l, "run_end", 0)["text"] = "SYNTHETIC-SENSITIVE-MARKER-7f3a" }), "V4", Fail},
	} {
		t.Run(name, func(t *testing.T) {
			dir := copyLive(t)
			tc.change(t, dir)
			reindex(t, dir)
			r := verifyLive(t, dir)
			if tc.claim != "V1" && r.Claims["V1"] != Pass {
				t.Fatalf("V1 %s: an outer check fired: %v", r.Claims["V1"], r.Findings)
			}
			if !has(r.Cases[0].Findings, tc.claim, tc.verdict) {
				t.Fatalf("no %s %s finding: %v", tc.claim, tc.verdict, r.Cases[0].Findings)
			}
			if worst(r.Verdict, tc.verdict) != r.Verdict || r.Cases[0].Verdict == Pass {
				t.Fatalf("ask %s, report %s", r.Cases[0].Verdict, r.Verdict)
			}
			if b, _ := json.Marshal(r); bytes.Contains(b, []byte("SYNTHETIC-SENSITIVE-MARKER")) {
				t.Fatal("the report repeats bundle text")
			}
		})
	}
}

// withClosure appends an approval closure at decided+offset seconds and
// moves the run's end and the snapshot past it, consistently.
func withClosure(t *testing.T, dir, typ, status string, offset uint64) uint64 {
	var at uint64
	editTask(t, dir, "live/ask.json", 0, func(st *storedTask) {
		es := events(t, st)
		decided := es[len(es)-1]
		x := decided
		x.EventType, x.Sequence, x.EventID = typ, decided.Sequence+1, "evt-closure-000000000000000000000000"
		d := *decided.Payload.Decision
		d.ApprovalStatus, d.Reason = status, map[string]string{"expired": "APPROVAL_TIMEOUT", "withdrawn": "HOST_CANCELLED"}[status]
		x.Payload = contracts.EventPayload{Decision: &d}
		x.ObservedAt = decided.ObservedAt + offset
		at = x.ObservedAt
		relink(st, append(es, x))
	})
	editFile(t, dir, "live/run.json", func(m map[string]any) {
		m["ended_at"] = json.Number(itoa(at + 1))
		s := m["snapshot"].(map[string]any)
		s["last_sequence"], s["last_observed_at"], s["exported_at"] = json.Number("4"), json.Number(itoa(at)), json.Number(itoa(at+2))
	})
	return at
}

func itoa(n uint64) string { b, _ := json.Marshal(n); return string(b) }

// A later expiry, at its deadline, while the host was running: the
// original Ask stands and the snapshot shows the expiry, apart.
func TestLiveLaterExpiry(t *testing.T) {
	dir := copyLive(t)
	at := withClosure(t, dir, "APPROVAL_EXPIRED", "expired", 30)
	reindex(t, dir)
	r := verifyLive(t, dir)
	if r.Verdict != Pass || r.Cases[0].Observed[0].Result != "Ask" {
		t.Fatalf("%s: %v %v", r.Verdict, r.Findings, r.Cases[0].Findings)
	}
	if !strings.HasPrefix(r.Live.OriginalDecision, "Ask/HUMAN_APPROVAL_REQUIRED/pending") || r.Live.StateAtSnapshot != "expired at "+itoa(at)+" (after the original pending decision)" {
		t.Fatalf("decision %q, snapshot %q", r.Live.OriginalDecision, r.Live.StateAtSnapshot)
	}
}

// The bundle's own claims and provenance in live mode.
func TestLiveBundleRules(t *testing.T) {
	manifest := func(f func(map[string]any)) func(*testing.T, string) {
		return func(t *testing.T, dir string) { editFile(t, dir, "manifest.json", f) }
	}
	for name, tc := range map[string]struct {
		change func(*testing.T, string)
		claim  string
	}{
		"a second case claimed": {manifest(func(m map[string]any) {
			m["results"] = append(m["results"].([]any), map[string]any{"case_id": "deny-budget", "verdict": "PASS", "evidence_ids": []any{"ev-live-run.json"}, "reason": "x"})
			m["not_evaluated"] = slices.DeleteFunc(m["not_evaluated"].([]any), func(s any) bool { return s == "deny-budget" })
		}), "V1"},
		"a case missing from not evaluated": {manifest(func(m map[string]any) {
			m["not_evaluated"] = slices.DeleteFunc(m["not_evaluated"].([]any), func(s any) bool { return s == "restart" })
		}), "V1"},
		"labelled fixture": {manifest(func(m map[string]any) {
			for _, e := range m["evidence"].([]any) {
				e.(map[string]any)["evidence_mode"] = "fixture"
			}
		}), "V6"},
		"a modified build": {func(t *testing.T, d string) {
			editFile(t, d, "provenance/build.json", func(m map[string]any) { m["vcs_modified"] = "true" })
		}, "V1"},
		"the run exited 1":    {manifest(func(m map[string]any) { m["commands"].([]any)[0].(map[string]any)["exit_code"] = json.Number("1") }), "V1"},
		"another CLI version": {manifest(func(m map[string]any) { m["versions"].(map[string]any)["cli"] = "2.1.999 (Claude Code)" }), "V1"},
		// 2.1.300 was never approved; the bundle and its run record must name one version (2026-10-17 pin change).
		"a version between the pins": {manifest(func(m map[string]any) { m["versions"].(map[string]any)["cli"] = "2.1.300 (Claude Code)" }), "V1"},
		"the run record's version is not the bundle's": {func(t *testing.T, d string) {
			editFile(t, d, "live/run.json", func(m map[string]any) { m["cli_version_required"] = "2.1.301 (Claude Code)" })
		}, "V1"},
		"the command without its gate flag": {manifest(func(m map[string]any) {
			c := m["commands"].([]any)[0].(map[string]any)
			c["command"] = strings.Replace(c["command"].(string), " --dev-demo-plugins", "", 1)
		}), "V6"},
	} {
		t.Run(name, func(t *testing.T) {
			dir := copyLive(t)
			tc.change(t, dir)
			reindex(t, dir)
			r := verifyLive(t, dir)
			if r.Claims[tc.claim] != Fail || r.ExitCode() != 1 {
				t.Fatalf("%s %s exit %d: %v", tc.claim, r.Claims[tc.claim], r.ExitCode(), r.Findings)
			}
		})
	}
	// The live bundle checked as fixture is no fixture PASS either.
	r, _ := Verify(liveGolden, trusted(t), "fixture")
	if r.Claims["V1"] != Fail || r.Claims["V6"] != Fail {
		t.Fatalf("the live bundle as fixture: %v", r.Claims)
	}
}
