package verification

import (
	"bytes"
	"encoding/json"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/SIMPLYBOYS/agent-payment-authority/internal/contracts"
)

const goldenName = "m1-fixture-20261009T162703Z-4ddb7a"

var golden = filepath.Join("..", "..", "testdata", "verification", goldenName)

func trusted(t *testing.T) Trusted {
	t.Helper()
	tr, err := LoadTrusted(filepath.Join("..", ".."))
	if err != nil {
		t.Fatal(err)
	}
	return tr
}

// tree is a directory's files and their digests, to show nothing changed.
func tree(t *testing.T, dir string) map[string]string {
	out := map[string]string{}
	filepath.WalkDir(dir, func(p string, d fs.DirEntry, err error) error {
		if err == nil && d.Type().IsRegular() {
			b, _ := os.ReadFile(p)
			rel, _ := filepath.Rel(dir, p)
			out[rel] = sha(b)
		}
		return nil
	})
	return out
}

// copyGolden copies the golden bundle into a new directory of the same name.
func copyGolden(t *testing.T) string {
	t.Helper()
	dir := filepath.Join(t.TempDir(), goldenName)
	err := filepath.WalkDir(golden, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, _ := filepath.Rel(golden, p)
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

func verify(t *testing.T, dir string) *Report {
	t.Helper()
	r, err := Verify(dir, trusted(t), "fixture")
	if err != nil {
		t.Fatal(err)
	}
	return r
}

// readMap and writeMap edit a JSON file as a generic document (numbers kept).
func readMap(t *testing.T, path string) map[string]any {
	t.Helper()
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	d := json.NewDecoder(bytes.NewReader(b))
	d.UseNumber()
	var v map[string]any
	if err := d.Decode(&v); err != nil {
		t.Fatal(err)
	}
	return v
}

func writeMap(t *testing.T, path string, v any) {
	t.Helper()
	b, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, append(b, '\n'), 0o600); err != nil {
		t.Fatal(err)
	}
}

// reindex rewrites every indexed digest and the end marker's manifest
// digest, as a producer that recomputed everything would: a negative case
// must then be caught by its own check, not by an outer hash.
func reindex(t *testing.T, dir string) {
	t.Helper()
	m := readMap(t, filepath.Join(dir, "manifest.json"))
	for _, e := range m["evidence"].([]any) {
		ev := e.(map[string]any)
		if b, err := os.ReadFile(filepath.Join(dir, ev["path"].(string))); err == nil {
			ev["digest"] = sha(b)
		}
	}
	writeMap(t, filepath.Join(dir, "manifest.json"), m)
	b, _ := os.ReadFile(filepath.Join(dir, "manifest.json"))
	end := readMap(t, filepath.Join(dir, "run-end.json"))
	end["manifest_digest"] = sha(b)
	writeMap(t, filepath.Join(dir, "run-end.json"), end)
}

// editTask changes one stored task of a record (store/ or e2e/), then the
// caller relinks and reindexes as needed.
func editTask(t *testing.T, dir, rel string, i int, f func(*storedTask)) {
	t.Helper()
	path := filepath.Join(dir, rel)
	rec := readMap(t, path)
	key := "tasks"
	if strings.HasPrefix(rel, "e2e/") {
		key = "store"
	}
	list := rec[key].([]any)
	raw, _ := json.Marshal(list[i])
	var st storedTask
	if err := json.Unmarshal(raw, &st); err != nil {
		t.Fatal(err)
	}
	f(&st)
	raw, _ = json.Marshal(st)
	var back any
	json.Unmarshal(raw, &back)
	list[i] = back
	writeMap(t, path, rec)
}

// events and setEvents read and write a task's typed events; relink
// recomputes each stored hash and the prev chain from the genesis.
func events(t *testing.T, st *storedTask) []contracts.AuditEvent {
	var out []contracts.AuditEvent
	for _, s := range st.Events {
		var e contracts.AuditEvent
		if err := json.Unmarshal([]byte(s.Body), &e); err != nil {
			t.Fatal(err)
		}
		out = append(out, e)
	}
	return out
}

func relink(st *storedTask, es []contracts.AuditEvent) {
	prev, _ := contracts.GenesisHash(st.TaskID)
	st.Events = nil
	for _, e := range es {
		e.PrevEventHash = prev
		b, _ := json.Marshal(e)
		prev, _ = contracts.EventHash(e)
		st.Events = append(st.Events, stored{Body: string(b), Hash: prev})
	}
}

// editPurchase changes the task's only purchase, stores its new intent
// digest and carries it into the events, relinked.
func editPurchase(t *testing.T, st *storedTask, f func(*contracts.PurchaseOrder)) {
	for po, s := range st.Purchases {
		var order contracts.PurchaseOrder
		json.Unmarshal([]byte(s.Body), &order)
		f(&order)
		h, _ := order.Intent.Hash()
		b, _ := json.Marshal(order)
		st.Purchases[po] = stored{Body: string(b), Hash: h}
		es := events(t, st)
		for k := range es {
			if es[k].Scope != nil && es[k].Scope.PurchaseOrderID == po {
				es[k].Scope.IntentHash = h
			}
			if d := es[k].Payload.Decision; d != nil {
				d.IntentHash = h
			}
		}
		relink(st, es)
	}
}

// resnap edits a snapshot and stores its new digest.
func resnap(t *testing.T, st *storedTask, kind string, f func(map[string]any)) {
	s := st.Snapshots[kind]
	var v map[string]any
	d := json.NewDecoder(strings.NewReader(s.Body))
	d.UseNumber()
	d.Decode(&v)
	f(v)
	b, _ := json.Marshal(v)
	st.Snapshots[kind] = stored{Body: string(b), Hash: sha(b)}
}

func caseIn(t *testing.T, r *Report, id string) CaseReport {
	t.Helper()
	i := slices.IndexFunc(r.Cases, func(c CaseReport) bool { return c.CaseID == id })
	if i < 0 {
		t.Fatalf("no case %s in the report", id)
	}
	return r.Cases[i]
}

func has(fs []Finding, claim string, v Verdict) bool {
	return slices.ContainsFunc(fs, func(f Finding) bool { return f.Claim == claim && f.Verdict == v })
}

// expectCase: the case has a finding of the claim and verdict, and the
// whole report is at least as bad.
func expectCase(t *testing.T, r *Report, id, claim string, v Verdict) {
	t.Helper()
	c := caseIn(t, r, id)
	if !has(c.Findings, claim, v) {
		t.Fatalf("%s: no %s %s finding; %s %v", id, claim, v, c.Verdict, c.Findings)
	}
	if worst(r.Verdict, v) != r.Verdict {
		t.Fatalf("the report says %s with a %s finding", r.Verdict, v)
	}
}

func expectBundle(t *testing.T, r *Report, claim string, v Verdict) {
	t.Helper()
	if !has(r.Findings, claim, v) {
		t.Fatalf("no bundle %s %s finding: %v", claim, v, r.Findings)
	}
	if worst(r.Verdict, v) != r.Verdict {
		t.Fatalf("the report says %s with a %s finding", r.Verdict, v)
	}
}

// The golden bundle passes exactly as testdata/verification/expected.json
// says, and is not changed by being read.
func TestGoldenBundle(t *testing.T) {
	var want struct {
		Verdict      Verdict            `json:"verdict"`
		ExitCode     int                `json:"exit_code"`
		Claims       map[string]Verdict `json:"claims"`
		NotEvaluated []string           `json:"not_evaluated"`
		Cases        []struct {
			CaseID         string  `json:"case_id"`
			Layer          string  `json:"layer"`
			Verdict        Verdict `json:"verdict"`
			Records        int     `json:"records"`
			Result         string  `json:"result"`
			Reason         string  `json:"reason"`
			ApprovalStatus string  `json:"approval_status"`
		} `json:"cases"`
	}
	b, err := os.ReadFile(filepath.Join("..", "..", "testdata", "verification", "expected.json"))
	if err != nil || json.Unmarshal(b, &want) != nil {
		t.Fatalf("expected.json: %v", err)
	}
	before := tree(t, golden)
	r := verify(t, golden)
	if after := tree(t, golden); len(before) != len(after) || !func() bool {
		for k, v := range before {
			if after[k] != v {
				return false
			}
		}
		return true
	}() {
		t.Fatal("verifying changed the bundle")
	}
	if r.Verdict != want.Verdict || r.ExitCode() != want.ExitCode {
		t.Fatalf("%s exit %d; findings %v", r.Verdict, r.ExitCode(), r.Findings)
	}
	for k, v := range want.Claims {
		if r.Claims[k] != v {
			t.Errorf("claim %s: %s", k, r.Claims[k])
		}
	}
	if !slices.Equal(r.NotEvaluated, want.NotEvaluated) {
		t.Errorf("not evaluated %v", r.NotEvaluated)
	}
	if len(r.Cases) != len(want.Cases) || len(want.Cases) != 31 {
		t.Fatalf("%d cases", len(r.Cases))
	}
	for _, w := range want.Cases {
		c := caseIn(t, r, w.CaseID)
		if c.Layer != w.Layer || c.Verdict != w.Verdict || len(c.Observed) != w.Records {
			t.Errorf("%s: %s %s %d records; %v", w.CaseID, c.Layer, c.Verdict, len(c.Observed), c.Findings)
			continue
		}
		for _, o := range c.Observed {
			if o.Result != w.Result || o.Reason != w.Reason || o.ApprovalStatus != w.ApprovalStatus {
				t.Errorf("%s: observed %s/%s/%s", w.CaseID, o.Result, o.Reason, o.ApprovalStatus)
			}
		}
	}
	if len(r.Differences) != 0 {
		t.Errorf("differences %v", r.Differences)
	}
	for _, q := range r.Requirements {
		if q.Verdict != Pass {
			t.Errorf("requirement %s: %s", q.Requirement, q.Verdict)
		}
	}
}

// Semantic negatives on the Store records: each made consistent inside
// (digests, chain, index, end marker) so only its own check can catch it.
func TestRecordsThatContradictTheirCase(t *testing.T) {
	for name, tc := range map[string]struct {
		rel, caseID, claim string
		edit               func(*testing.T, *storedTask)
	}{
		"a Deny reported as Allow": {"store/deny-budget.json", "deny-budget", "V3", func(t *testing.T, st *storedTask) {
			es := events(t, st)
			for k := range es {
				if d := es[k].Payload.Decision; d != nil {
					d.PolicyVerdict, d.Reason = "Allow", "PAYMENT_CAPABILITY_UNAVAILABLE"
				}
			}
			relink(st, es)
		}},
		"the amount changed": {"store/ask.json", "ask", "V2", func(t *testing.T, st *storedTask) {
			editPurchase(t, st, func(p *contracts.PurchaseOrder) { p.Intent.Amount = "400000" })
		}},
		"the payee changed": {"store/ask.json", "ask", "V2", func(t *testing.T, st *storedTask) {
			editPurchase(t, st, func(p *contracts.PurchaseOrder) { p.Intent.PayTo = "0x5555555555555555555555555555555555555555" })
		}},
		"the wrong payer": {"store/ask.json", "ask", "V7", func(t *testing.T, st *storedTask) {
			st.Payer = "0x2222222222222222222222222222222222222222"
		}},
		"A's events placed under B": {"store/ask.json", "ask", "V7", func(t *testing.T, st *storedTask) {
			es := events(t, st)
			for k := range es {
				es[k].Scope.ConsumerID, es[k].Scope.AgentID, es[k].Scope.WalletID = "consumer-b", "agent-b", "wallet-b"
			}
			relink(st, es)
		}},
		"an Ask without its profile":    {"store/ask.json", "ask", "V3", func(t *testing.T, st *storedTask) { delete(st.Snapshots, "profile") }},
		"an Ask without its credential": {"store/ask.json", "ask", "V3", func(t *testing.T, st *storedTask) { delete(st.Snapshots, "credential") }},
		"an Ask without either": {"store/ask.json", "ask", "V3", func(t *testing.T, st *storedTask) {
			delete(st.Snapshots, "profile")
			delete(st.Snapshots, "credential")
		}},
		"terms that disagree with the snapshots": {"store/ask.json", "ask", "V3", func(t *testing.T, st *storedTask) {
			resnap(t, st, "terms", func(v map[string]any) { v["profile_state"] = "absent" })
		}},
		// Consistently forged: settings absent everywhere, the Ask unchanged.
		"an Ask with its configuration declared absent": {"store/ask.json", "ask", "V3", func(t *testing.T, st *storedTask) {
			resnap(t, st, "terms", func(v map[string]any) { v["profile_state"] = "absent" })
			delete(st.Snapshots, "profile")
		}},
		"an invalid profile": {"store/ask.json", "ask", "V3", func(t *testing.T, st *storedTask) {
			resnap(t, st, "profile", func(v map[string]any) { v["evidence_mode"] = "live" })
		}},
		"another consumer's credential": {"store/ask.json", "ask", "V3", func(t *testing.T, st *storedTask) {
			resnap(t, st, "credential", func(v map[string]any) { v["consumer_id"] = "consumer-b" })
		}},
		"a forbidden event": {"store/ask.json", "ask", "V5", func(t *testing.T, st *storedTask) {
			es := events(t, st)
			x := es[len(es)-1]
			x.EventType, x.Sequence, x.EventID, x.Payload = "SETTLED", x.Sequence+1, "evt-settled-0000000000000000000000", contracts.EventPayload{}
			relink(st, append(es, x))
		}},
		"an event in the middle changed": {"store/ask.json", "ask", "V4", func(t *testing.T, st *storedTask) {
			es := events(t, st)
			es[1].ObservedAt++
			b, _ := json.Marshal(es[1])
			h, _ := contracts.EventHash(es[1])
			st.Events[1] = stored{Body: string(b), Hash: h} // its own hash right; the next event's link broken
		}},
		"a gap in the sequence": {"store/ask.json", "ask", "V4", func(t *testing.T, st *storedTask) {
			es := events(t, st)
			relink(st, append(es[:1], es[2:]...))
		}},
		"the budget deducted": {"store/ask.json", "ask", "V3", func(t *testing.T, st *storedTask) { st.BudgetRemaining = "4500000" }},
		"a deny-merchant whose merchant is allowed": {"store/deny-merchant.json", "deny-merchant", "V3", func(t *testing.T, st *storedTask) {
			resnap(t, st, "profile", func(v map[string]any) {
				v["merchants"] = []any{"0x3333333333333333333333333333333333333333"}
			})
		}},
	} {
		t.Run(name, func(t *testing.T) {
			dir := copyGolden(t)
			editTask(t, dir, tc.rel, 0, func(st *storedTask) { tc.edit(t, st) })
			reindex(t, dir)
			r := verify(t, dir)
			expectCase(t, r, tc.caseID, tc.claim, Fail)
			if r.Claims["V1"] != Pass { // reindexed: only the case's own check may catch it
				t.Fatalf("an outer check fired: %v", r.Findings)
			}
		})
	}
}

// A contracts record relabelled as another case keeps its own inputs: it is
// refused even though the contract code gives the labelled case's result.
func TestContractRecordsRelabelled(t *testing.T) {
	cases := trusted(t).Cases.Cases
	for _, pair := range [][2]string{{"consumer-a", "consumer-b"}, {"retry-conflict", "retry-new-request-same-order"}} {
		t.Run(pair[0]+" as "+pair[1], func(t *testing.T) {
			dir := copyGolden(t)
			as := cases[slices.IndexFunc(cases, func(c contracts.AcceptanceCase) bool { return c.CaseID == pair[1] })]
			rec := readMap(t, filepath.Join(dir, "contracts", pair[0]+".json"))
			rec["case_id"], rec["parameters"] = as.CaseID, as.Parameters
			obs := map[string]any{"result": as.Expected.Result}
			if as.Expected.Result == "reject" {
				obs["code"] = as.Expected.Reason
			}
			rec["observed"] = obs
			writeMap(t, filepath.Join(dir, "contracts", pair[1]+".json"), rec)
			reindex(t, dir)
			r := verify(t, dir)
			expectCase(t, r, pair[1], "V2", Fail)
			if c := caseIn(t, r, pair[1]); len(c.Observed) != 1 || c.Observed[0].Result != as.Expected.Result {
				t.Fatalf("the relabelled inputs no longer give %s's result: %v", pair[1], c.Observed)
			}
		})
	}
}

// The producer's verdicts are compared, never taken.
func TestProducerVerdicts(t *testing.T) {
	t.Run("a correct case the producer calls FAIL", func(t *testing.T) {
		dir := copyGolden(t)
		m := readMap(t, filepath.Join(dir, "manifest.json"))
		for _, res := range m["results"].([]any) {
			if res := res.(map[string]any); res["case_id"] == "ask" {
				res["verdict"] = "FAIL"
			}
		}
		writeMap(t, filepath.Join(dir, "manifest.json"), m)
		reindex(t, dir)
		r := verify(t, dir)
		if c := caseIn(t, r, "ask"); c.Verdict != Pass || c.ProducerVerdict != "FAIL" || len(r.Differences) != 1 {
			t.Fatalf("%s %s %v", c.Verdict, c.ProducerVerdict, r.Differences)
		}
	})
	t.Run("a wrong case the producer calls PASS", func(t *testing.T) {
		dir := copyGolden(t)
		editTask(t, dir, "store/ask.json", 0, func(st *storedTask) { st.BudgetRemaining = "1" })
		reindex(t, dir)
		r := verify(t, dir)
		if c := caseIn(t, r, "ask"); c.Verdict != Fail || c.ProducerVerdict != "PASS" || r.ExitCode() != 1 {
			t.Fatalf("%s %s", c.Verdict, c.ProducerVerdict)
		}
	})
}

// Missing or unusable data is INCONCLUSIVE (exit 2) where nothing
// contradicts; contradictions are FAIL.
func TestMissingAndContradictory(t *testing.T) {
	t.Run("no end marker", func(t *testing.T) {
		dir := copyGolden(t)
		os.Remove(filepath.Join(dir, "run-end.json"))
		r := verify(t, dir)
		expectBundle(t, r, "V1", Inconclusive)
		if r.ExitCode() != 2 {
			t.Fatalf("exit %d", r.ExitCode())
		}
	})
	t.Run("an end marker for another manifest", func(t *testing.T) {
		dir := copyGolden(t)
		end := readMap(t, filepath.Join(dir, "run-end.json"))
		end["manifest_digest"] = sha([]byte("other"))
		writeMap(t, filepath.Join(dir, "run-end.json"), end)
		expectBundle(t, verify(t, dir), "V1", Fail)
	})
	t.Run("a required record missing", func(t *testing.T) {
		dir := copyGolden(t)
		os.Remove(filepath.Join(dir, "store", "deny-expired.json"))
		m := readMap(t, filepath.Join(dir, "manifest.json"))
		m["evidence"] = slices.DeleteFunc(m["evidence"].([]any), func(e any) bool { return e.(map[string]any)["path"] == "store/deny-expired.json" })
		for _, res := range m["results"].([]any) {
			res := res.(map[string]any)
			res["evidence_ids"] = slices.DeleteFunc(res["evidence_ids"].([]any), func(id any) bool { return id == "ev-store-deny-expired.json" })
		}
		writeMap(t, filepath.Join(dir, "manifest.json"), m)
		reindex(t, dir)
		r := verify(t, dir)
		if c := caseIn(t, r, "deny-expired"); c.Verdict != Inconclusive || r.ExitCode() != 2 {
			t.Fatalf("%s exit %d %v", c.Verdict, r.ExitCode(), c.Findings)
		}
		expectCase(t, r, "deny-expired", "V1", Inconclusive)
	})
	t.Run("a truncated record", func(t *testing.T) {
		dir := copyGolden(t)
		p := filepath.Join(dir, "e2e", "ask.json")
		b, _ := os.ReadFile(p)
		os.WriteFile(p, b[:len(b)/2], 0o600)
		reindex(t, dir)
		r := verify(t, dir)
		expectCase(t, r, "ask", "V2", Inconclusive)
		if r.ExitCode() != 2 {
			t.Fatalf("exit %d: %v", r.ExitCode(), r.Findings)
		}
	})
	t.Run("no trusted contracts fixtures", func(t *testing.T) {
		repo := t.TempDir()
		for _, f := range configFiles {
			if f == "testdata/contracts/purchase-input.json" {
				continue
			}
			b, _ := os.ReadFile(filepath.Join("..", "..", f))
			os.MkdirAll(filepath.Join(repo, filepath.Dir(f)), 0o700)
			os.WriteFile(filepath.Join(repo, f), b, 0o600)
		}
		tr, err := LoadTrusted(repo)
		if err != nil {
			t.Fatal(err)
		}
		r, _ := Verify(golden, tr, "fixture")
		expectCase(t, r, "consumer-a", "V2", Inconclusive)
		if r.ExitCode() != 2 {
			t.Fatalf("exit %d", r.ExitCode())
		}
		if _, err := LoadTrusted(t.TempDir()); err == nil {
			t.Fatal("no case list accepted")
		}
	})
	t.Run("a claim listed twice", func(t *testing.T) {
		dir := copyGolden(t)
		m := readMap(t, filepath.Join(dir, "manifest.json"))
		m["results"] = append(m["results"].([]any), m["results"].([]any)[0])
		writeMap(t, filepath.Join(dir, "manifest.json"), m)
		reindex(t, dir)
		expectBundle(t, verify(t, dir), "V1", Fail)
	})
	t.Run("a duplicate JSON member", func(t *testing.T) {
		dir := copyGolden(t)
		p := filepath.Join(dir, "store", "ask.json")
		b, _ := os.ReadFile(p)
		os.WriteFile(p, bytes.Replace(b, []byte(`"case_id": "ask",`), []byte(`"case_id": "ask", "case_id": "ask",`), 1), 0o600)
		reindex(t, dir)
		expectCase(t, verify(t, dir), "ask", "V2", Fail)
	})
	t.Run("a non-zero command", func(t *testing.T) {
		dir := copyGolden(t)
		m := readMap(t, filepath.Join(dir, "manifest.json"))
		m["commands"].([]any)[3].(map[string]any)["exit_code"] = 1
		writeMap(t, filepath.Join(dir, "manifest.json"), m)
		reindex(t, dir)
		expectBundle(t, verify(t, dir), "V1", Fail)
	})
}

// Modes and layers: no fixture passes as live, no layer as another.
func TestModesAndLayers(t *testing.T) {
	evidence := func(path string, f func(map[string]any)) func(*testing.T, string) {
		return func(t *testing.T, dir string) {
			m := readMap(t, filepath.Join(dir, "manifest.json"))
			for _, e := range m["evidence"].([]any) {
				if e := e.(map[string]any); e["path"] == path {
					f(e)
				}
			}
			writeMap(t, filepath.Join(dir, "manifest.json"), m)
		}
	}
	for name, change := range map[string]func(*testing.T, string){
		"an unknown source":         evidence("store/ask.json", func(e map[string]any) { e["source"] = "magic" }),
		"a fixture labelled live":   evidence("e2e/ask.json", func(e map[string]any) { e["evidence_mode"] = "live" }),
		"contracts evidence as E2E": evidence("contracts/consumer-a.json", func(e map[string]any) { e["source"] = "host_e2e" }),
		"a contracts record as E2E": func(t *testing.T, dir string) {
			p := filepath.Join(dir, "contracts", "consumer-a.json")
			rec := readMap(t, p)
			rec["layer"] = "host_e2e"
			writeMap(t, p, rec)
		},
	} {
		t.Run(name, func(t *testing.T) {
			dir := copyGolden(t)
			change(t, dir)
			reindex(t, dir)
			r := verify(t, dir)
			if r.Claims["V6"] != Fail || r.ExitCode() != 1 {
				t.Fatalf("V6 %s exit %d", r.Claims["V6"], r.ExitCode())
			}
		})
	}
	// The fixture bundle checked as live is no live PASS: its run ID and its
	// evidence mode are a fixture's.
	r, err := Verify(golden, trusted(t), "live")
	if err != nil || r.Claims["V1"] != Fail || r.Claims["V6"] != Fail || r.ExitCode() != 1 {
		t.Fatalf("the fixture bundle as live: %v %v %d", err, r.Claims, r.ExitCode())
	}
	// An unknown mode checks nothing.
	r, err = Verify(golden, trusted(t), "other")
	if err != nil || r.Claims["V6"] != Inconclusive || r.ExitCode() != 2 || len(r.Cases) != 0 {
		t.Fatalf("an unknown mode: %v %v %d", err, r.Claims, r.ExitCode())
	}
}

// Paths: nothing outside the run directory is read, nothing that is not a
// plain regular file inside it.
func TestPaths(t *testing.T) {
	outside := func(t *testing.T, dir string) string {
		p := filepath.Join(filepath.Dir(dir), "outside.json")
		os.WriteFile(p, []byte(`{"marker":"BYSTANDER-CONTENT"}`), 0o600)
		return p
	}
	t.Run("an index path that climbs out", func(t *testing.T) {
		dir := copyGolden(t)
		outside(t, dir)
		m := readMap(t, filepath.Join(dir, "manifest.json"))
		m["evidence"].([]any)[0].(map[string]any)["path"] = "../outside.json"
		writeMap(t, filepath.Join(dir, "manifest.json"), m)
		reindex(t, dir)
		r := verify(t, dir)
		expectBundle(t, r, "V1", Fail)
		b, _ := json.Marshal(r)
		if bytes.Contains(b, []byte("BYSTANDER")) {
			t.Fatal("the outside file was read into the report")
		}
	})
	t.Run("a symbolic link", func(t *testing.T) {
		dir := copyGolden(t)
		p := filepath.Join(dir, "store", "ask.json")
		os.Remove(p)
		os.Symlink(outside(t, dir), p)
		expectBundle(t, verify(t, dir), "V1", Fail)
	})
	t.Run("a linked directory", func(t *testing.T) {
		dir := copyGolden(t)
		real := filepath.Join(filepath.Dir(dir), "store-real")
		os.Rename(filepath.Join(dir, "store"), real)
		os.Symlink(real, filepath.Join(dir, "store"))
		r := verify(t, dir)
		expectBundle(t, r, "V1", Fail)
		expectCase(t, r, "ask", "V1", Fail)
	})
	t.Run("a FIFO", func(t *testing.T) {
		dir := copyGolden(t)
		p := filepath.Join(dir, "store", "ask.json")
		os.Remove(p)
		if err := syscall.Mkfifo(p, 0o600); err != nil {
			t.Skip("no FIFO here:", err)
		}
		done := make(chan *Report, 1)
		go func() { r, _ := Verify(dir, trusted(t), "fixture"); done <- r }()
		select {
		case r := <-done:
			expectBundle(t, r, "V1", Fail)
		case <-time.After(10 * time.Second):
			t.Fatal("reading the bundle blocked on a FIFO")
		}
	})
}

// Integration requirements are shown by the test records, not the index;
// every case is required; capabilities stay not evaluated.
func TestRequirementsAndClaims(t *testing.T) {
	t.Run("the index says pass, the test is not in the record", func(t *testing.T) {
		dir := copyGolden(t)
		p := filepath.Join(dir, "browser", "browser-ui.out")
		b, _ := os.ReadFile(p)
		os.WriteFile(p, bytes.ReplaceAll(b, []byte("PASS nothing says paid or delivered"), []byte("note: nothing says paid or delivered")), 0o600)
		reindex(t, dir)
		r := verify(t, dir)
		expectBundle(t, r, "R", Fail)
	})
	t.Run("the index says pass, the test was skipped", func(t *testing.T) {
		dir := copyGolden(t)
		p := filepath.Join(dir, "tests", "requirements-go.jsonl")
		b, _ := os.ReadFile(p)
		os.WriteFile(p, bytes.ReplaceAll(b, []byte(`"Action":"pass","Package":"github.com/SIMPLYBOYS/agent-payment-authority/cmd/demo","Test":"TestLiveRun",`),
			[]byte(`"Action":"skip","Package":"github.com/SIMPLYBOYS/agent-payment-authority/cmd/demo","Test":"TestLiveRun",`)), 0o600)
		reindex(t, dir)
		expectBundle(t, verify(t, dir), "R", Fail)
	})
	t.Run("a required case listed as not evaluated", func(t *testing.T) {
		dir := copyGolden(t)
		m := readMap(t, filepath.Join(dir, "manifest.json"))
		m["results"] = slices.DeleteFunc(m["results"].([]any), func(r any) bool { return r.(map[string]any)["case_id"] == "restart" })
		m["not_evaluated"] = append(m["not_evaluated"].([]any), "restart")
		writeMap(t, filepath.Join(dir, "manifest.json"), m)
		reindex(t, dir)
		expectBundle(t, verify(t, dir), "V1", Fail)
	})
	t.Run("a capability claimed PASS", func(t *testing.T) {
		dir := copyGolden(t)
		m := readMap(t, filepath.Join(dir, "manifest.json"))
		m["results"] = append(m["results"].([]any), map[string]any{"case_id": "onchain_settlement", "verdict": "PASS", "evidence_ids": []any{"ev-e2e-ask.json"}, "reason": "x"})
		m["not_evaluated"] = slices.DeleteFunc(m["not_evaluated"].([]any), func(s any) bool { return s == "onchain_settlement" })
		writeMap(t, filepath.Join(dir, "manifest.json"), m)
		reindex(t, dir)
		expectBundle(t, verify(t, dir), "V1", Fail)
	})
}

// Each check below is the only one that can catch its case.
func TestSingleChecks(t *testing.T) {
	manifest := func(f func(map[string]any)) func(*testing.T, string) {
		return func(t *testing.T, dir string) {
			m := readMap(t, filepath.Join(dir, "manifest.json"))
			f(m)
			writeMap(t, filepath.Join(dir, "manifest.json"), m)
			reindex(t, dir)
		}
	}
	task := func(rel string, i int, edit func(*testing.T, *storedTask)) func(*testing.T, string) {
		return func(t *testing.T, dir string) {
			editTask(t, dir, rel, i, func(st *storedTask) { edit(t, st) })
			reindex(t, dir)
		}
	}
	for name, tc := range map[string]struct {
		change         func(*testing.T, string)
		caseID, claim  string // caseID "" for the bundle's own findings
		verdict        Verdict
		reasonContains string
	}{
		"a file changed after indexing": {func(t *testing.T, dir string) {
			os.WriteFile(filepath.Join(dir, "tests", "policy.jsonl"), []byte("{}\n"), 0o600) // not reindexed
		}, "", "V1", Fail, "digest"},
		"a file the index does not list": {func(t *testing.T, dir string) {
			os.WriteFile(filepath.Join(dir, "e2e", "extra.json"), []byte("{}"), 0o600)
		}, "", "V1", Fail, "does not list"},
		"a link nobody references": {func(t *testing.T, dir string) {
			os.Symlink("/etc/hosts", filepath.Join(dir, "provenance", "link"))
		}, "", "V1", Fail, "symbolic link"},
		"an index path that climbs out": {manifest(func(m map[string]any) {
			m["evidence"] = append(m["evidence"].([]any), map[string]any{"evidence_id": "ev-out", "path": "../x.json", "digest": sha(nil), "evidence_mode": "fixture", "source": "host_e2e"})
		}), "", "V1", Fail, "unsafe path"},
		"a config digest not made of the listed files": {manifest(func(m map[string]any) { m["config_digest"] = sha([]byte("other")) }), "", "V1", Fail, "config digest"},
		"another case list":                            {manifest(func(m map[string]any) { m["case_manifest_digest"] = sha([]byte("other")) }), "", "V1", Inconclusive, "another case list"},
		"the Store clock is not the case's": {task("store/ask.json", 0, func(t *testing.T, st *storedTask) {
			es := events(t, st)
			for k := range es {
				es[k].ObservedAt++
			}
			relink(st, es)
		}), "ask", "V2", Fail, "clock"},
		"the API's events are not the stored ones": {func(t *testing.T, dir string) {
			p := filepath.Join(dir, "e2e", "ask.json")
			rec := readMap(t, p)
			for _, v := range rec["events"].(map[string]any) {
				v.(map[string]any)["events"].([]any)[0].(map[string]any)["observed_at"] = json.Number("1")
			}
			writeMap(t, p, rec)
			reindex(t, dir)
		}, "ask", "V4", Fail, "API's events"},
		"another case's Store record": {func(t *testing.T, dir string) {
			rec := readMap(t, filepath.Join(dir, "store", "ask.json"))
			rec["case_id"] = "allow-unavailable"
			rec["parameters"] = map[string]any{}
			writeMap(t, filepath.Join(dir, "store", "allow-unavailable.json"), rec)
			reindex(t, dir)
		}, "allow-unavailable", "V3", Fail, "expected"},
		"terms that call a missing profile present": {task("store/deny-missing-config.json", 0, func(t *testing.T, st *storedTask) {
			resnap(t, st, "terms", func(v map[string]any) { v["profile_state"] = "present" })
		}), "deny-missing-config", "V3", Fail, "terms call the profile"},
		"an expiry at another time": {task("store/approval-timeout.json", 0, func(t *testing.T, st *storedTask) {
			es := events(t, st)
			es[len(es)-1].ObservedAt += 9
			relink(st, es)
		}), "approval-timeout", "V3", Fail, "expired at"},
		"a decision recording a payment": {task("store/ask.json", 0, func(t *testing.T, st *storedTask) {
			es := events(t, st)
			es[len(es)-1].Payload.Decision.PaymentStatus = "Settled"
			relink(st, es)
		}), "ask", "V5", Fail, "payment status"},
		"a task without purchases under another payer": {task("store/restart.json", 1, func(t *testing.T, st *storedTask) {
			st.Payer = "0x2222222222222222222222222222222222222222"
		}), "restart", "V7", Fail, "binding"},
	} {
		t.Run(name, func(t *testing.T) {
			dir := copyGolden(t)
			tc.change(t, dir)
			r := verify(t, dir)
			fs := r.Findings
			if tc.caseID != "" {
				fs = caseIn(t, r, tc.caseID).Findings
			}
			if !slices.ContainsFunc(fs, func(f Finding) bool {
				return f.Claim == tc.claim && f.Verdict == tc.verdict && strings.Contains(f.Reason, tc.reasonContains)
			}) {
				t.Fatalf("no %s %s finding about %q: %v", tc.claim, tc.verdict, tc.reasonContains, fs)
			}
			if worst(r.Verdict, tc.verdict) != r.Verdict {
				t.Fatalf("the report says %s", r.Verdict)
			}
		})
	}
}

// Contracts cases: the frozen expected decides, and the record may not
// contradict what its own inputs give.
func TestContractOutcomes(t *testing.T) {
	t.Run("an expected outcome the inputs do not give", func(t *testing.T) {
		tr := trusted(t)
		for i := range tr.Cases.Cases {
			if tr.Cases.Cases[i].CaseID == "cross-consumer" {
				tr.Cases.Cases[i].Expected.Result = "accept" // as if the contract code behaved otherwise than frozen
			}
		}
		r, err := Verify(golden, tr, "fixture")
		if err != nil {
			t.Fatal(err)
		}
		expectCase(t, r, "cross-consumer", "V3", Fail)
	})
	t.Run("a record contradicting its own inputs", func(t *testing.T) {
		dir := copyGolden(t)
		p := filepath.Join(dir, "contracts", "cross-consumer.json")
		rec := readMap(t, p)
		rec["observed"] = map[string]any{"result": "reject", "code": "AMBIGUOUS_KEY"}
		writeMap(t, p, rec)
		reindex(t, dir)
		expectCase(t, verify(t, dir), "cross-consumer", "V3", Fail)
	})
}

// A failing requirement test fails the requirement, whatever the index says.
func TestFailingRequirementTest(t *testing.T) {
	dir := copyGolden(t)
	p := filepath.Join(dir, "browser", "browser-ui.out")
	b, _ := os.ReadFile(p)
	os.WriteFile(p, bytes.ReplaceAll(b, []byte("PASS nothing says paid or delivered"), []byte("FAIL nothing says paid or delivered")), 0o600)
	idx := filepath.Join(dir, "requirements", "index.json")
	ib, _ := os.ReadFile(idx)
	os.WriteFile(idx, bytes.Replace(ib, []byte(`"test": "nothing says paid or delivered",
        "status": "pass"`), []byte(`"test": "nothing says paid or delivered",
        "status": "fail"`), 1), 0o600)
	reindex(t, dir)
	r := verify(t, dir)
	if !slices.ContainsFunc(r.Findings, func(f Finding) bool {
		return f.Claim == "R" && f.Verdict == Fail && strings.Contains(f.Reason, "failed")
	}) {
		t.Fatalf("%v", r.Findings)
	}
}

// R2 (10/10 review): an E2E case's run records (host run, trace, API
// replies) must be there and agree with the Store. Each change is reindexed;
// V1 stays PASS, so only the cross-checks can catch it.
func TestE2ERunRecords(t *testing.T) {
	edit := func(rel string, f func(map[string]any)) func(*testing.T, string) {
		return func(t *testing.T, dir string) {
			p := filepath.Join(dir, rel)
			rec := readMap(t, p)
			f(rec)
			writeMap(t, p, rec)
		}
	}
	firstPurchase := func(rec map[string]any) map[string]any {
		return rec["tasks"].([]any)[0].(map[string]any)["purchases"].([]any)[0].(map[string]any)
	}
	requestPurchase := func(rec map[string]any, task string) map[string]any {
		calls := rec["traces"].(map[string]any)[task].([]any)[0].(map[string]any)["tool_calls"].([]any)
		for _, c := range calls {
			if c := c.(map[string]any); c["tool"] == "request_purchase" {
				return c
			}
		}
		return nil
	}
	for name, tc := range map[string]struct {
		change  func(*testing.T, string)
		caseID  string
		claim   string
		verdict Verdict
	}{
		// The review's five.
		"no traces":    {edit("e2e/ask.json", func(r map[string]any) { delete(r, "traces") }), "ask", "V4", Inconclusive},
		"no host runs": {edit("e2e/ask.json", func(r map[string]any) { delete(r, "host_runs") }), "ask", "V4", Inconclusive},
		"the API's decision is a Deny": {edit("e2e/ask.json", func(r map[string]any) {
			d := firstPurchase(r)["decision"].(map[string]any)
			d["policy_verdict"], d["approval_status"], d["reason"] = "Deny", "not_requested", "INSUFFICIENT_BUDGET"
		}), "ask", "V3", Fail},
		"the API's amount is 1":                  {edit("e2e/ask.json", func(r map[string]any) { firstPurchase(r)["amount"] = "1" }), "ask", "V2", Fail},
		"the trace's request_purchase says Deny": {edit("e2e/ask.json", func(r map[string]any) { requestPurchase(r, "show-a-01")["outcome"] = "Deny" }), "ask", "V3", Fail},
		// More of the same relations.
		"the trace names other events": {edit("e2e/ask.json", func(r map[string]any) {
			requestPurchase(r, "show-a-01")["event_sequences"] = []any{json.Number("2")}
		}), "ask", "V4", Fail},
		"the host run timed out; the Store says ok": {edit("e2e/ask.json", func(r map[string]any) {
			r["host_runs"].(map[string]any)["runs"].([]any)[0].(map[string]any)["outcome"] = "timeout"
		}), "ask", "V4", Fail},
		"a cancelled run that ended before the cancel": {edit("e2e/cancel-no-purchase.json", func(r map[string]any) {
			run := r["host_runs"].(map[string]any)["runs"].([]any)[0].(map[string]any)
			run["ended_at"] = json.Number("1")
		}), "cancel-no-purchase", "V4", Fail},
		"the API's approval closure is missing": {edit("e2e/cancel-pending.json", func(r map[string]any) {
			p := firstPurchase(r)
			p["approval_closed"], p["closed_at"] = "", json.Number("0")
		}), "cancel-pending", "V3", Fail},
		"no trace for the task after the restart": {edit("e2e/restart.json", func(r map[string]any) {
			delete(r["traces"].(map[string]any), "show-a-02")
		}), "restart", "V4", Inconclusive},
	} {
		t.Run(name, func(t *testing.T) {
			dir := copyGolden(t)
			tc.change(t, dir)
			reindex(t, dir)
			r := verify(t, dir)
			expectCase(t, r, tc.caseID, tc.claim, tc.verdict)
			if r.Claims["V1"] != Pass {
				t.Fatalf("V1 %s: an outer check fired: %v", r.Claims["V1"], r.Findings)
			}
			if c := caseIn(t, r, tc.caseID); c.Verdict != tc.verdict {
				t.Fatalf("%s: %s, want %s: %v", tc.caseID, c.Verdict, tc.verdict, c.Findings)
			}
		})
	}
	// Positive: the golden's lawful flows pass their run records, the
	// purchaseless and the cancelled-after-the-run ones included.
	r := verify(t, golden)
	for _, id := range []string{"ask", "cancel-no-purchase", "cli-timeout", "cancel-pending", "cli-timeout-pending", "restart"} {
		if c := caseIn(t, r, id); c.Verdict != Pass {
			t.Errorf("%s: %s %v", id, c.Verdict, c.Findings)
		}
	}
}

// R3 (10/10 review): a log the scanner cannot read to its end does not
// pass on what it read before; a failure it read stays a failure.
func TestRequirementLogReadToTheEnd(t *testing.T) {
	long := "# " + strings.Repeat("x", 16<<20+1) + "\n"
	const failing = "not ok 999 - hostile strings stay strings; nothing reads as paid\n"
	for name, tc := range map[string]struct {
		appended string
		verdict  Verdict
	}{
		"a line over the limit":                  {long, Inconclusive},
		"a failure after a line over the limit":  {long + failing, Inconclusive},
		"a failure before a line over the limit": {failing + long, Fail},
	} {
		t.Run(name, func(t *testing.T) {
			dir := copyGolden(t)
			p := filepath.Join(dir, "tests", "requirements-web.tap")
			b, _ := os.ReadFile(p)
			os.WriteFile(p, append(b, tc.appended...), 0o600)
			reindex(t, dir)
			r := verify(t, dir)
			if r.Claims["V1"] != Pass || r.Claims["R"] != tc.verdict || r.Verdict != tc.verdict {
				t.Fatalf("V1 %s, R %s, verdict %s; want R and verdict %s: %v", r.Claims["V1"], r.Claims["R"], r.Verdict, tc.verdict, r.Findings)
			}
		})
	}
}
