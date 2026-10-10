package verification

import (
	"bufio"
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"regexp"
	"slices"
	"strings"

	"github.com/SIMPLYBOYS/agent-payment-authority/internal/buyer"
	"github.com/SIMPLYBOYS/agent-payment-authority/internal/contracts"
	"github.com/SIMPLYBOYS/agent-payment-authority/internal/toolbridge"
)

// The live single-Ask bundle (M1-E3; docs/m1-evidence.md section 16): one
// `apa-buyer run --live` of a clean build, the Store's record of its task,
// the run trace (the host's allow-listed record) and the run's record.
// Only ask is claimed. The trusted values it is held to are the shared
// live settings (buyer.ApprovedCLIVersion, LiveAcceptAuth, Prompt, Model,
// DefaultLimits, toolbridge.MaxCalls) and the frozen case list.

var liveRunIDPattern = regexp.MustCompile(`^m1-live-[0-9]{8}T[0-9]{6}Z-[0-9a-f]{6}$`)

// liveLimits are always in a live report (Aaron 2026-10-10: the development
// demo plugin exception is accepted for M1 live evidence with these stated).
var liveLimits = []string{
	"strict zero-plugin mode is not verified when the gate mode is dev_demo_plugins",
	"plugin names, the gate and the APA-only configuration directory do not prove a plugin's content, that it has no background activity, or operating-system isolation",
	"auth_source none alone proves no login or billing source; usage is the CLI's own report or unknown, not a billing record",
	"a live PASS shows the single Ask claim's evidence only: not the fixture cases, not B1-07 as a whole, not M1",
	"the bundle cannot prove by itself that the named model produced it (no signature or outside anchor)",
}

// fixtureOnly are the fixture bundle's integration requirements, not part
// of a live run's check (they are not rerun, and not claimed).
var fixtureOnly = func() []string {
	var out []string
	for _, r := range requirements {
		out = append(out, r.name)
	}
	return out
}()

// LiveReport is a live run's account in the report.
type LiveReport struct {
	GateMode         string   `json:"gate_mode"`
	GateResult       string   `json:"gate_result"`
	CLIVersion       string   `json:"cli_version"`
	Model            string   `json:"model"`
	Usage            string   `json:"usage"`
	OriginalDecision string   `json:"original_decision"` // the POLICY_DECIDED record
	StateAtSnapshot  string   `json:"state_at_snapshot"` // the approval as of the export
	ObservedUntil    string   `json:"observed_until"`    // the export's last event
	ResidualRisks    []string `json:"residual_risks"`    // as the bundle states them
	OutOfScope       []string `json:"out_of_scope"`      // the fixture's integration requirements
}

type liveToolCall struct {
	Tool      string `json:"tool"`
	ProductID string `json:"product_id,omitempty"`
	Outcome   string `json:"outcome"`
}

// liveRun is live/run.json (version apa.m1.live.v2: v1 had no
// termination, and recorded a normal end as the outer bound; see
// docs/m1-evidence.md 16.6), every member named.
type liveRun struct {
	Version            string   `json:"version"`
	RunID              string   `json:"run_id"`
	Command            string   `json:"command"`
	ExitCode           int      `json:"apa_buyer_exit_code"`
	Termination        string   `json:"termination"`
	ProducerError      string   `json:"producer_error"`
	StartedAt          uint64   `json:"started_at"`
	EndedAt            uint64   `json:"ended_at"`
	GateMode           string   `json:"gate_mode"`
	GateResult         string   `json:"gate_result"`
	Model              string   `json:"model"`
	CLI                string   `json:"cli"`
	CLIVersionRequired string   `json:"cli_version_required"`
	AuthAccepted       []string `json:"auth_accepted"`
	Report             struct {
		Mode             string         `json:"mode"`
		Outcome          string         `json:"outcome"`
		Detail           string         `json:"detail"`
		ExitCode         int            `json:"exit_code"`
		TaskStarted      bool           `json:"task_started"`
		ToolCalls        []liveToolCall `json:"tool_calls"`
		NotReadyRefusals int            `json:"not_ready_refusals"`
		StoreEnd         string         `json:"store_end"`
		StoreEndError    string         `json:"store_end_error"`
		ExpiryErrors     int            `json:"expiry_errors"`
	} `json:"report"`
	HostErrorLines     []string `json:"host_error_lines"`
	DroppedStderrLines int      `json:"dropped_stderr_lines"`
	TraceFiles         int      `json:"trace_files"`
	Database           struct {
		Digests  map[string]string `json:"digests"`
		Relation string            `json:"relation"`
	} `json:"database"`
	Snapshot struct {
		ExportedAt     uint64 `json:"exported_at"`
		LastSequence   uint64 `json:"last_sequence"`
		LastObservedAt uint64 `json:"last_observed_at"`
	} `json:"snapshot"`
	Usage         json.RawMessage `json:"usage"`
	ResidualRisks []string        `json:"residual_risks"`
}

// The run trace's lines (internal/buyer Host.trace), every member named.
type traceStart struct {
	Event        string `json:"event"`
	At           uint64 `json:"at"`
	TaskID       string `json:"task_id"`
	ConsumerID   string `json:"consumer_id"`
	CLI          string `json:"cli"`
	PromptSHA256 string `json:"prompt_sha256"`
	GateMode     string `json:"gate_mode"`
	Limits       struct {
		LineBytes         int     `json:"line_bytes"`
		StdoutBytes       int     `json:"stdout_bytes"`
		StderrBytes       int     `json:"stderr_bytes"`
		TotalSeconds      float64 `json:"total_seconds"`
		AssistantMessages int     `json:"assistant_messages"`
		ToolCalls         int     `json:"tool_calls"`
	} `json:"limits"`
}

type traceInit struct {
	Event          string `json:"event"`
	Gate           string `json:"gate"`
	RequestedModel string `json:"requested_model"`
	Model          string `json:"model"`
	TaskStarted    bool   `json:"task_started"`
	PrepareError   string `json:"prepare_error"`
}

type traceCall struct {
	Event string `json:"event"`
	liveToolCall
}

type traceEnd struct {
	Event            string          `json:"event"`
	Outcome          string          `json:"outcome"`
	Detail           string          `json:"detail"`
	ExitCode         int             `json:"exit_code"`
	Messages         int             `json:"messages"`
	AssistantMessage int             `json:"assistant_messages"`
	NumTurns         int             `json:"num_turns"`
	Usage            json.RawMessage `json:"usage"`
	StderrBytes      int             `json:"stderr_bytes"`
	ToolAttempts     int             `json:"tool_attempts"`
	NotReadyRefusals int             `json:"not_ready_refusals"`
	StoreEnd         string          `json:"store_end"`
	StoreEndError    string          `json:"store_end_error"`
	ResultChars      int             `json:"result_chars"`
}

// liveBundle: the live bundle's own provenance (a clean build of the
// named commit) and its one command.
func (v *verifier) liveBundle() {
	f, m := &v.f, v.m
	ev := []string{"manifest.json"}
	if m.Versions["cli"] != buyer.ApprovedCLIVersion {
		f.fail("V1", ev, "the CLI version %s is not the approved %s", quote(m.Versions["cli"]), buyer.ApprovedCLIVersion)
	}
	gate := m.Versions["gate_mode"]
	if gate != "dev_demo_plugins" && gate != "strict" {
		f.fail("V6", ev, "gate mode %s is neither strict nor the accepted dev_demo_plugins exception", quote(gate))
	}
	if len(m.Commands) != 1 {
		f.fail("V1", ev, "%d commands; a live bundle is one run", len(m.Commands))
	} else {
		c := m.Commands[0]
		switch {
		case !strings.HasPrefix(c.Command, "apa-buyer run --live --cli /") || !strings.Contains(c.Command, " --trace-dir "):
			f.fail("V1", ev, "the command is not one apa-buyer live run")
		case strings.Contains(c.Command, " --dev-demo-plugins") != (gate == "dev_demo_plugins"):
			f.fail("V6", ev, "the command's gate flag is not the stated gate mode %s", quote(gate))
		}
		if c.ExitCode != 0 {
			f.fail("V1", ev, "the live run exited %d", c.ExitCode)
		}
	}
	const p = "provenance/build.json"
	var b struct {
		Head        string   `json:"head"`
		Base        string   `json:"reviewed_base"`
		Untracked   []string `json:"untracked_in_repository"`
		Source      string   `json:"source"`
		Binary      string   `json:"binary"`
		VCSRevision string   `json:"vcs_revision"`
		VCSModified string   `json:"vcs_modified"`
		Go          string   `json:"go"`
		Producer    string   `json:"producer"`
	}
	if !v.readJSON(f, "V1", p, &b) {
		return
	}
	switch {
	case b.Head != m.CodeCommit || b.VCSRevision != m.CodeCommit:
		f.fail("V1", []string{p}, "the build is of %s (HEAD %s), not the bundle's commit %s", quote(b.VCSRevision), quote(b.Head), m.CodeCommit)
	case b.VCSModified != "false":
		f.fail("V1", []string{p}, "the build says vcs.modified=%s: not a clean build", quote(b.VCSModified))
	case !commitPattern.MatchString(b.Base):
		f.fail("V1", []string{p}, "no reviewed base commit")
	}
}

// liveCase checks the single Ask: the Store's record of the run's task
// (as the fixture cases are checked), the run trace and the run's record,
// against each other and the trusted live settings.
func (v *verifier) liveCase() {
	c := v.tr.Cases.Cases[slices.IndexFunc(v.tr.Cases.Cases, func(c contracts.AcceptanceCase) bool { return c.CaseID == "ask" })]
	var producer contracts.ClaimResult
	for _, res := range v.m.Results {
		if res.CaseID == "ask" {
			producer = res
		}
	}
	cr := CaseReport{CaseID: "ask", Layer: "live_cli", Expected: c.Expected, Observed: []Observed{}, ProducerVerdict: producer.Verdict}
	var f findings
	cited := map[string]bool{}
	for _, id := range producer.EvidenceIDs {
		cited[v.ids[id]] = true
	}
	present := map[string]bool{}
	for _, p := range []string{"live/ask.json", "live/run.json", "live/trace.jsonl"} {
		cr.Evidence = append(cr.Evidence, p)
		if _, ok := v.paths[p]; !ok {
			f.missing("V1", []string{p}, "the live record is not in the index")
			continue
		}
		present[p] = true
		if !cited[p] {
			f.fail("V6", []string{p}, "the ask claim does not cite it")
		}
	}
	lr := &LiveReport{GateMode: v.m.Versions["gate_mode"], CLIVersion: v.m.Versions["cli"], OutOfScope: fixtureOnly, ResidualRisks: []string{}}
	v.r.Live = lr
	var run liveRun
	runOK := present["live/run.json"] && v.readJSON(&f, "V4", "live/run.json", &run)
	var ts []*task
	if present["live/ask.json"] {
		var rec struct {
			CaseID string       `json:"case_id"`
			Layer  string       `json:"layer"`
			Tasks  []storedTask `json:"tasks"`
		}
		if v.readJSON(&f, "V2", "live/ask.json", &rec) {
			if rec.CaseID != "ask" || rec.Layer != "live_cli" {
				f.fail("V6", []string{"live/ask.json"}, "the record is %s %s, not the live ask record", quote(rec.CaseID), quote(rec.Layer))
			} else {
				var o Observed
				o, ts = v.integration(&f, c, "live/ask.json", "live_cli", rec.Tasks)
				cr.Observed = append(cr.Observed, o)
			}
		}
	}
	var t *task
	if len(ts) == 1 {
		t = ts[0]
	}
	if runOK {
		v.liveRunRecord(&f, run, t, lr)
	}
	if present["live/trace.jsonl"] {
		v.liveTrace(&f, run, runOK, t, lr)
	}
	cr.Findings = f
	if cr.Findings == nil {
		cr.Findings = []Finding{}
	}
	cr.Claims = f.verdicts([]string{"V2", "V3", "V4", "V5", "V6", "V7"})
	cr.Verdict = f.verdict()
	if string(cr.Verdict) != cr.ProducerVerdict {
		v.r.Differences = append(v.r.Differences, fmt.Sprintf("ask: the producer says %s, this checker %s", quote(cr.ProducerVerdict), cr.Verdict))
	}
	v.r.Cases = append(v.r.Cases, cr)
}

// liveRunRecord: the run's record against the manifest, the trusted live
// settings and the stored task (the snapshot's bounds).
func (v *verifier) liveRunRecord(f *findings, run liveRun, t *task, lr *LiveReport) {
	ev := []string{"live/run.json"}
	cmd := ""
	if len(v.m.Commands) == 1 {
		cmd = v.m.Commands[0].Command
	}
	switch {
	case run.Version != "apa.m1.live.v2":
		f.fail("V4", ev, "run record version %s", quote(run.Version))
	case run.RunID != v.m.RunID || run.Command != cmd || len(v.m.Commands) == 1 && run.ExitCode != v.m.Commands[0].ExitCode:
		f.fail("V4", ev, "the run record is not the manifest's run, command or exit code")
	}
	if run.GateMode != v.m.Versions["gate_mode"] {
		f.fail("V6", ev, "the run record's gate mode %s is not the bundle's %s", quote(run.GateMode), quote(v.m.Versions["gate_mode"]))
	}
	if run.CLIVersionRequired != buyer.ApprovedCLIVersion || !slices.Equal(run.AuthAccepted, buyer.LiveAcceptAuth) {
		f.fail("V1", ev, "the run record names another approved version or accepted authentication than the shared live settings")
	}
	// How the run ended, as the producer recorded it, against its exit code:
	// a live claim needs a run that ended by itself with 0 and no producer
	// error; a recorded failure is never outweighed by the other fields.
	switch t, code, perr := run.Termination, run.ExitCode, run.ProducerError; {
	case !slices.Contains([]string{"completed", "wrapper_exit_nonzero", "outer_bound", "interrupted", "start_failed"}, t):
		f.fail("V4", ev, "termination %s is not one the producer records", quote(t))
	case t == "completed" && (code != 0 || perr != ""), t == "wrapper_exit_nonzero" && (code == 0 || perr != ""),
		t == "start_failed" && code != -1, (t == "outer_bound" || t == "interrupted" || t == "start_failed") && perr == "":
		f.fail("V4", ev, "the run record contradicts itself: termination %s, exit %d, producer error %s", quote(t), code, quote(perr))
	case t != "completed" || perr != "":
		f.fail("V4", ev, "the run did not end by itself: termination %s (%s)", quote(t), quote(perr))
	}
	r := run.Report
	switch {
	case r.Mode != "live" || r.Outcome != "ok" || r.ExitCode != 0 || run.ExitCode != 0:
		f.fail("V4", ev, "the run did not end ok: mode %s, outcome %s, CLI exit %d, apa-buyer exit %d", quote(r.Mode), quote(r.Outcome), r.ExitCode, run.ExitCode)
	case !r.TaskStarted || r.StoreEnd != "" || r.StoreEndError != "":
		f.fail("V4", ev, "the run's task did not start, or ended with %s %s", quote(r.StoreEnd), quote(r.StoreEndError))
	}
	if run.GateMode == "dev_demo_plugins" && !slices.Contains(run.ResidualRisks, "strict zero-plugin mode not verified") {
		f.fail("V6", ev, "a dev_demo_plugins run that does not state that strict zero-plugin mode is not verified")
	}
	lr.GateResult, lr.Model, lr.ResidualRisks = run.GateResult, run.Model, nonNilStrings(run.ResidualRisks)
	lr.Usage = "unknown"
	var usage struct {
		Counts map[string]int64 `json:"counts"`
		Source string           `json:"source"`
	}
	switch {
	case string(run.Usage) == `"unknown"`:
	case json.Unmarshal(run.Usage, &usage) == nil && len(usage.Counts) > 0 && strings.HasPrefix(usage.Source, "self-reported"):
		lr.Usage = fmt.Sprintf("self-reported by the CLI: %v", usage.Counts)
	default:
		f.fail("V4", ev, "usage is neither unknown nor the CLI's own counts so labelled")
	}
	if t == nil {
		return
	}
	// The snapshot's bounds: the export's last event is the run's last, and
	// every event lies within the run.
	n := len(t.events)
	if n == 0 {
		return
	}
	last := t.events[n-1]
	if run.Snapshot.LastSequence != last.Sequence || run.Snapshot.LastObservedAt != last.ObservedAt || run.Snapshot.ExportedAt < last.ObservedAt {
		f.fail("V4", ev, "the snapshot names event %d at %d (exported %d); the export ends at event %d at %d",
			run.Snapshot.LastSequence, run.Snapshot.LastObservedAt, run.Snapshot.ExportedAt, last.Sequence, last.ObservedAt)
	}
	for _, e := range t.events {
		if e.ObservedAt+1 < run.StartedAt || e.ObservedAt > run.EndedAt+1 { // whole seconds
			f.fail("V4", ev, "event %d at %d lies outside the run (%d to %d)", e.Sequence, e.ObservedAt, run.StartedAt, run.EndedAt)
			break
		}
	}
	lr.ObservedUntil = fmt.Sprintf("event %d at %d (exported at %d)", last.Sequence, last.ObservedAt, run.Snapshot.ExportedAt)
	// The original decision and the approval as of the snapshot, apart: a
	// later expiry does not rewrite the Ask, and the Ask is not approvable
	// now because it was pending then.
	pes := t.purchaseEvents()
	for _, pe := range pes {
		if pe.decision == nil {
			continue
		}
		lr.OriginalDecision = fmt.Sprintf("%s/%s/%s at %d", pe.decision.PolicyVerdict, pe.decision.Reason, pe.decision.ApprovalStatus, pe.decidedAt)
		lr.StateAtSnapshot = pe.decision.ApprovalStatus
		switch pe.closure {
		case "withdrawn":
			f.fail("V3", ev, "the Ask was withdrawn in a run that ended ok without a cancel")
		case "expired":
			if pe.closedAt < pe.decidedAt+t.terms.ApprovalDeadlineSeconds {
				f.fail("V3", ev, "the Ask expired at %d, before its deadline %d", pe.closedAt, pe.decidedAt+t.terms.ApprovalDeadlineSeconds)
			}
			lr.StateAtSnapshot = fmt.Sprintf("expired at %d (after the original pending decision)", pe.closedAt)
		}
	}
}

// liveTrace: the run trace (the host's allow-listed record) against the
// run's record, the stored task and the trusted live settings.
func (v *verifier) liveTrace(f *findings, run liveRun, runOK bool, t *task, lr *LiveReport) {
	ev := []string{"live/trace.jsonl"}
	raw, err := v.b.read("live/trace.jsonl")
	if err != nil {
		v.readErr(f, "V4", "live/trace.jsonl", err)
		return
	}
	var (
		start      *traceStart
		init       *traceInit
		end        *traceEnd
		calls      []liveToolCall
		order      []string
		lineN      int
		unreadable bool
	)
	sc := bufio.NewScanner(bytes.NewReader(raw))
	sc.Buffer(make([]byte, 64<<10), 1<<20)
	for sc.Scan() {
		lineN++
		line := sc.Bytes()
		var head struct {
			Event string `json:"event"`
		}
		if json.Unmarshal(line, &head) != nil {
			unreadable = true
			break
		}
		order = append(order, head.Event)
		var err error
		switch head.Event {
		case "run_start":
			start = new(traceStart)
			err = strictJSON(line, start)
		case "init":
			init = new(traceInit)
			err = strictJSON(line, init)
		case "tool_call":
			var c traceCall
			err = strictJSON(line, &c)
			calls = append(calls, c.liveToolCall)
		case "run_end":
			end = new(traceEnd)
			err = strictJSON(line, end)
		default:
			err = fmt.Errorf("unknown trace event %s", quote(head.Event))
		}
		if err != nil {
			f.fail("V4", ev, "trace line %d: %v", lineN, err)
		}
	}
	if unreadable || sc.Err() != nil {
		f.missing("V4", ev, "the trace could not be read to its end")
		return
	}
	// The order: run_start, init, the tool calls, run_end; each once.
	want := []string{"run_start", "init"}
	for range calls {
		want = append(want, "tool_call")
	}
	want = append(want, "run_end")
	if !slices.Equal(order, want) || start == nil || init == nil || end == nil {
		f.fail("V4", ev, "the trace's lines are %v, not run_start, init, tool calls, run_end", order)
		return
	}
	prompt := sha256.Sum256([]byte(buyer.Prompt))
	lim := buyer.DefaultLimits
	switch {
	case start.GateMode != v.m.Versions["gate_mode"] || runOK && start.GateMode != run.GateMode:
		f.fail("V6", ev, "the trace's gate mode %s is not the bundle's %s", quote(start.GateMode), quote(v.m.Versions["gate_mode"]))
	case start.PromptSHA256 != hex.EncodeToString(prompt[:]):
		f.fail("V4", ev, "the run's prompt is not the host's fixed prompt")
	case start.Limits.TotalSeconds != lim.Total.Seconds() || start.Limits.AssistantMessages != lim.Assistant || start.Limits.ToolCalls != toolbridge.MaxCalls:
		f.fail("V4", ev, "the run's limits (%v s, %d messages, %d calls) are not the approved %v s, %d, %d",
			start.Limits.TotalSeconds, start.Limits.AssistantMessages, start.Limits.ToolCalls, lim.Total.Seconds(), lim.Assistant, toolbridge.MaxCalls)
	}
	if init.Gate != "passed" || !init.TaskStarted || init.RequestedModel != buyer.Model || init.PrepareError != "" {
		f.fail("V4", ev, "the trace's init: gate %s, task started %v, requested model %s, prepare error %s", quote(init.Gate), init.TaskStarted, quote(init.RequestedModel), quote(init.PrepareError))
	}
	if end.Outcome != "ok" || end.ExitCode != 0 || end.StoreEnd != "" || end.StoreEndError != "" {
		f.fail("V4", ev, "the trace's run ended %s (exit %d, store end %s)", quote(end.Outcome), end.ExitCode, quote(end.StoreEnd))
	}
	if runOK {
		r := run.Report
		switch {
		case init.Gate != run.GateResult || init.Model != run.Model || start.CLI != run.CLI:
			f.fail("V4", ev, "the trace's gate, model or CLI is not the run record's")
		case end.Outcome != r.Outcome || end.ExitCode != r.ExitCode || end.NotReadyRefusals != r.NotReadyRefusals:
			f.fail("V4", ev, "the trace's end is not the run record's report")
		case !slices.Equal(calls, r.ToolCalls) && !(len(calls) == 0 && len(r.ToolCalls) == 0):
			f.fail("V4", ev, "the trace's tool calls %v are not the run record's %v", calls, r.ToolCalls)
		}
		var traceUsage map[string]int64
		if json.Unmarshal(end.Usage, &traceUsage) == nil && len(traceUsage) > 0 {
			var u struct {
				Counts map[string]int64 `json:"counts"`
			}
			if json.Unmarshal(run.Usage, &u) != nil || !mapsEqual(u.Counts, traceUsage) {
				f.fail("V4", ev, "the run record's usage is not the trace's")
			}
		} else if string(run.Usage) != `"unknown"` {
			f.fail("V4", ev, "the trace reports no usage; the run record does")
		}
	}
	if t == nil {
		return
	}
	if start.TaskID != t.row.TaskID || start.ConsumerID != t.row.ConsumerID {
		f.fail("V7", ev, "the trace's run is task %s of %s, not the stored %s of %s", quote(start.TaskID), quote(start.ConsumerID), quote(t.row.TaskID), quote(t.row.ConsumerID))
	}
	// Each request_purchase that reports a decision made the stored
	// purchase of its product with that decision; a repeat of the same
	// product reports the same decision (the Store replays it); every
	// stored purchase was requested.
	pes := t.purchaseEvents()
	requested := map[string]bool{}
	for k, c := range calls {
		if c.Tool != "request_purchase" || !slices.Contains([]string{"Ask", "Deny", "Allow"}, c.Outcome) {
			continue
		}
		po := toolbridge.PurchaseOrderID(t.row.TaskID, c.ProductID)
		pe := pes[po]
		switch {
		case pe == nil || pe.decision == nil:
			f.fail("V4", ev, "tool call %d requested %s with %s; the Store holds no decided purchase %s", k+1, quote(c.ProductID), c.Outcome, quote(po))
		case c.Outcome != pe.decision.PolicyVerdict:
			f.fail("V3", ev, "tool call %d reports %s; the Store decided %s", k+1, c.Outcome, pe.decision.PolicyVerdict)
		}
		requested[po] = true
	}
	for po := range t.orders {
		if !requested[po] {
			f.fail("V4", ev, "the stored purchase %s has no request_purchase in the trace", quote(po))
		}
	}
}

func mapsEqual(a, b map[string]int64) bool {
	if len(a) != len(b) {
		return false
	}
	for k, x := range a {
		if y, ok := b[k]; !ok || x != y {
			return false
		}
	}
	return true
}

func nonNilStrings(s []string) []string {
	if s == nil {
		return []string{}
	}
	return s
}
