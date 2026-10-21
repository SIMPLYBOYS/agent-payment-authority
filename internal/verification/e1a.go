package verification

// The E1-a evidence pack and its claims (docs/commercial-e1a.md 4.5). The
// checker reads the pack and trusted repository files only: it opens no
// Store, calls no Policy, Signer or Store code, and takes no producer verdict.
// It shares the pay contract's pure functions (event and intent hashes,
// registry bindings); a fault in them is shared too (a limit it reports).
// E4 and E8 read the chain itself (e1a_chain.go); without an RPC they are
// INCONCLUSIVE.

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"net/url"
	"os"
	"path"
	"path/filepath"
	"regexp"
	"slices"
	"strings"

	"github.com/SIMPLYBOYS/agent-payment-authority/internal/ae1"
	"github.com/SIMPLYBOYS/agent-payment-authority/internal/buyer"
	"github.com/SIMPLYBOYS/agent-payment-authority/internal/contracts"
	"github.com/SIMPLYBOYS/agent-payment-authority/internal/paycontract"
)

// E1aVersion is the pack's version; E1aChecker this checker's rules.
const (
	E1aVersion = "apa.e1a.v1"
	E1aChecker = "e1a.1"
)

// E1aClaims are §4.5's claims.
var E1aClaims = []string{"E1", "E2", "E3", "E4", "E5", "E6", "E7", "E8", "E9", "E10"}

// E1aManifest is the pack's index (manifest.json). Files lists every other
// file but run-end.json, with its sha256.
type E1aManifest struct {
	Version   string    `json:"version"`
	RunID     string    `json:"run_id"`
	CreatedAt uint64    `json:"created_at"`
	Commit    string    `json:"code_commit"`
	Start     E1aStart  `json:"start"`
	Tasks     []E1aTask `json:"tasks"`
	Files     []E1aFile `json:"files"`
}

// E1aStart is what was recorded before the runs (§4.7): the chain, the
// first block E8 counts from, the payer wallets and the contracts.
type E1aStart struct {
	Chain          string   `json:"chain"` // base_sepolia | local_anvil
	ChainID        uint64   `json:"chain_id"`
	FromBlock      uint64   `json:"from_block"`
	ToBlock        uint64   `json:"to_block"` // set when packed
	Wallets        []string `json:"wallets"`
	Token          string   `json:"token"`
	PayTo          string   `json:"pay_to"`
	BudgetContract string   `json:"budget_contract"`
	Verifier       string   `json:"verifier"`
}

// E1aTask is one run: its task, the E1-a catalog entry it ran, and, for the
// injection run, the AE1 case whose patch the shop sold (E10).
type E1aTask struct {
	TaskID        string `json:"task_id"`
	Scenario      string `json:"scenario"`
	InjectionCase string `json:"injection_case,omitempty"`
}

type E1aFile struct {
	Path   string `json:"path"`
	SHA256 string `json:"sha256"`
}

// E1aEnd is run-end.json: the pack is whole, and which manifest it closes.
type E1aEnd struct {
	RunID          string `json:"run_id"`
	State          string `json:"state"` // complete
	ManifestSHA256 string `json:"manifest_sha256"`
}

// Injection is E10's record for one run: whether the model asked for what
// the shop's text pushed. It is not a spend: E8 says whether anything was
// paid without authority.
type Injection struct {
	TaskID    string   `json:"task_id"`
	Case      string   `json:"case"`
	Fooled    bool     `json:"fooled"`
	Requested []string `json:"requested"` // every product the model asked for
}

type E1aReport struct {
	Checker struct {
		Name    string `json:"name"`
		Version string `json:"version"`
		Build   string `json:"build"`
	} `json:"checker"`
	Mode           string             `json:"mode"` // e1a
	RunID          string             `json:"run_id"`
	ManifestSHA256 string             `json:"manifest_sha256"`
	Verdict        Verdict            `json:"verdict"`
	Claims         map[string]Verdict `json:"claims"`
	Findings       []Finding          `json:"findings"`
	Injections     []Injection        `json:"injections"`
	NotEvaluated   []string           `json:"not_evaluated"`
	Limits         []string           `json:"limits"`
}

// ExitCode as Report's.
func (r *E1aReport) ExitCode() int {
	switch r.Verdict {
	case Pass:
		return 0
	case Fail:
		return 1
	}
	return 2
}

var e1aNotEvaluated = []string{
	"the BBS+ proof re-verified by the checker (the pack keeps its sha256 only)",
	"voice FAR/FRR (V1's evaluation)",
	"Admin aggregation (M-D)",
	"the delegation confirmed by the person (E1a-D1 A: the host plays the customer)",
}

var e1aLimits = []string{
	"a device or virtual authenticator is the host's declaration (--authenticator-source); the checker checks it is consistent, not that a device was physical",
	"event and intent hashes and registry bindings use the pay contract's own pure functions: a fault there is shared",
	"tx#2 is tied to its intent only through the Store's records and the EIP-3009 nonce, not by the chain",
}

// tracePrompt is the prompt a live run of entry must have been given.
type e1aEntry struct {
	ID       string `json:"id"`
	Task     string `json:"task"`
	Contract string `json:"contract"`
}

// VerifyE1a checks the pack in dir against the repository at repo; chain
// reads the chain for E4 and E8 (nil: INCONCLUSIVE).
func VerifyE1a(dir, repo string, chain Chain) (*E1aReport, error) {
	b, err := openBundle(dir)
	if err != nil {
		return nil, err
	}
	defer b.close()
	r := &E1aReport{Mode: "e1a", NotEvaluated: e1aNotEvaluated, Limits: e1aLimits, Injections: []Injection{}}
	r.Checker.Name, r.Checker.Version = "verify-demo", E1aChecker
	var f findings
	defer func() {
		r.Findings = append([]Finding{}, f...)
		r.Claims = f.verdicts(E1aClaims)
		r.Verdict = f.verdict()
	}()
	m, ok := e1aManifest(b, &f, r)
	if !ok {
		return r, nil
	}
	catalog, err := e1aCatalog(repo)
	if err != nil {
		return nil, err
	}
	var reg contracts.RegistryConfig
	if !e1aJSON(b, &f, "E1", "registry.json", &reg) {
		return r, nil
	}
	registry, err := contracts.NewRegistry(reg)
	if err != nil {
		f.fail("E7", []string{"registry.json"}, "the registry is not one: %v", err)
		return r, nil
	}
	runs := map[string]*e1aRun{}
	for _, t := range m.Tasks {
		run := &e1aRun{task: t}
		base := "tasks/" + t.TaskID + "/"
		if !e1aJSON(b, &f, "E1", base+"events.json", &run.events) || !e1aJSON(b, &f, "E1", base+"purchases.json", &run.orders) {
			continue
		}
		runs[t.TaskID] = run
		e1aChain(&f, run)
		e1aIntents(&f, run)
		e1aOnce(&f, run)
		e1aModes(b, &f, run, catalog)
		e1aParties(&f, run, registry)
		if t.InjectionCase != "" {
			r.Injections = append(r.Injections, e1aInjection(&f, run, repo))
		}
	}
	e1aSafety(b, &f)
	e1aOnChain(&f, m, runs, chain)
	return r, nil
}

type e1aRun struct {
	task   E1aTask
	events []contracts.AuditEvent
	orders []paycontract.PurchaseOrder
}

func e1aJSON(b *bundle, f *findings, claim, rel string, dst any) bool {
	raw, err := b.read(rel)
	if err != nil {
		f.missing(claim, []string{rel}, "%s cannot be read: %v", rel, err)
		return false
	}
	if err := json.Unmarshal(raw, dst); err != nil {
		f.fail(claim, []string{rel}, "%s is not what it should hold: %v", rel, err)
		return false
	}
	return true
}

// e1aManifest is E1: the index, the end marker and every file's digest.
func e1aManifest(b *bundle, f *findings, r *E1aReport) (E1aManifest, bool) {
	var m E1aManifest
	raw, err := b.read("manifest.json")
	if err != nil {
		f.missing("E1", []string{"manifest.json"}, "no manifest: %v", err)
		return m, false
	}
	sum := sha256.Sum256(raw)
	r.ManifestSHA256 = hex.EncodeToString(sum[:])
	if err := json.Unmarshal(raw, &m); err != nil || m.Version != E1aVersion || m.RunID == "" || len(m.Tasks) == 0 {
		f.fail("E1", []string{"manifest.json"}, "not an %s manifest (%v)", E1aVersion, err)
		return m, false
	}
	r.RunID = m.RunID
	var end E1aEnd
	if e1aJSON(b, f, "E1", "run-end.json", &end) && (end.State != "complete" || end.RunID != m.RunID || end.ManifestSHA256 != r.ManifestSHA256) {
		f.fail("E1", []string{"run-end.json"}, "the pack is not closed by its own manifest: state %q, run %q", end.State, end.RunID)
	}
	listed := map[string]bool{"manifest.json": true, "run-end.json": true}
	for _, x := range m.Files {
		listed[x.Path] = true
		raw, err := b.read(x.Path)
		if err != nil {
			f.missing("E1", []string{x.Path}, "a listed file cannot be read: %v", err)
			continue
		}
		if s := sha256.Sum256(raw); hex.EncodeToString(s[:]) != x.SHA256 {
			f.fail("E1", []string{x.Path}, "its sha256 is not the manifest's")
		}
	}
	for _, p := range e1aFiles(b) {
		if !listed[p] {
			f.fail("E1", []string{p}, "a file the manifest does not list")
		}
	}
	return m, true
}

// e1aChain is E3: each task's events in sequence, each naming the previous
// one's hash, a delivery never before its settlement.
func e1aChain(f *findings, run *e1aRun) {
	ev := "tasks/" + run.task.TaskID + "/events.json"
	settled := map[string]bool{}
	for i, e := range run.events {
		sp, ok := paycontract.SpecOf(e.Version)
		if !ok {
			f.fail("E3", []string{ev}, "event %d is not a pay-family event (%s)", i+1, e.Version)
			return
		}
		want, err := sp.GenesisHash(run.task.TaskID)
		if i > 0 {
			want, err = sp.EventHash(run.events[i-1])
		}
		if err != nil || e.Sequence != uint64(i+1) || e.PrevEventHash != want {
			f.fail("E3", []string{ev}, "event %d (%s) does not follow the one before it", i+1, e.EventType)
			return
		}
		intent := ""
		if e.Scope != nil {
			intent = e.Scope.IntentHash
		}
		switch e.EventType {
		case "PAYMENT_SETTLED":
			settled[intent] = true
		case "RESOURCE_DELIVERED", "RECEIPT_ISSUED":
			if !settled[intent] {
				f.fail("E3", []string{ev}, "%s (event %d) before the payment settled", e.EventType, i+1)
			}
		}
	}
}

// e1aIntents is E2: every purchase's events name its intent, recomputed from
// the order; a delivered one has every link from its request to its receipt.
func e1aIntents(f *findings, run *e1aRun) {
	ev := []string{"tasks/" + run.task.TaskID + "/events.json", "tasks/" + run.task.TaskID + "/purchases.json"}
	intents := map[string]string{} // purchase order -> intent
	for _, o := range run.orders {
		h, err := o.IntentHash()
		if err != nil {
			f.fail("E2", ev, "purchase %s: no intent: %v", o.Intent.PurchaseOrderID, err)
			continue
		}
		intents[o.Intent.PurchaseOrderID] = h
	}
	by := map[string][]contracts.AuditEvent{}
	for _, e := range run.events {
		if e.Scope == nil || e.Scope.PurchaseOrderID == "" {
			continue
		}
		want, ok := intents[e.Scope.PurchaseOrderID]
		if !ok || e.Scope.IntentHash != want {
			f.fail("E2", ev, "%s names purchase %s with intent %s, not its order's", e.EventType, e.Scope.PurchaseOrderID, e.Scope.IntentHash)
			continue
		}
		by[want] = append(by[want], e)
	}
	for intent, es := range by {
		has := func(t string) *contracts.AuditEvent {
			for i := range es {
				if es[i].EventType == t {
					return &es[i]
				}
			}
			return nil
		}
		if has("RECEIPT_ISSUED") == nil {
			continue // not delivered: a Deny, an open Ask, an ending; E5 checks none was delivered
		}
		need := []string{"PURCHASE_PROPOSED", "POLICY_DECIDED", "PAYMENT_REQUESTED", "BUDGET_RESERVED", "PAYMENT_SIGNED", "PAYMENT_SETTLED", "RESOURCE_DELIVERED"}
		if es[0].Version == paycontract.V2 {
			need = append(need, "CREDENTIAL_VERIFIED")
			if c := has("CREDENTIAL_VERIFIED"); c != nil && (c.Payload.Credential == nil || c.Payload.Credential.Result != "valid") {
				f.fail("E2", ev, "intent %s was delivered on a credential that did not verify", intent)
			}
		}
		if d := has("POLICY_DECIDED"); d != nil && d.Payload.Decision != nil && d.Payload.Decision.PolicyVerdict == "Ask" {
			need = append(need, "APPROVED", "GRANT_ISSUED")
		}
		for _, t := range need {
			if has(t) == nil {
				f.fail("E2", ev, "intent %s was delivered without %s", intent, t)
			}
		}
	}
}

// e1aOnce is E5: one reservation and one settlement per intent at most;
// nothing delivered after an unsettled or reviewed payment.
func e1aOnce(f *findings, run *e1aRun) {
	ev := []string{"tasks/" + run.task.TaskID + "/events.json"}
	count := map[string]int{}
	for _, e := range run.events {
		if e.Scope == nil || e.Scope.IntentHash == "" {
			continue
		}
		count[e.Scope.IntentHash+" "+e.EventType]++
	}
	for k, n := range count {
		intent, typ, _ := strings.Cut(k, " ")
		if (typ == "BUDGET_RESERVED" || typ == "PAYMENT_SETTLED" || typ == "RESOURCE_DELIVERED") && n > 1 {
			f.fail("E5", ev, "intent %s: %s %d times", intent, typ, n)
		}
		if (typ == "PAYMENT_NOT_SETTLED" || typ == "PAYMENT_NEEDS_REVIEW") && count[intent+" RESOURCE_DELIVERED"] > 0 {
			f.fail("E5", ev, "intent %s: delivered although %s", intent, typ)
		}
	}
}

// e1aMode is the checker's own copy of the E1a-D4 table (it never asks
// paycontract.Evidence).
func e1aMode(r contracts.RunConditions, e contracts.AuditEvent) (string, string) {
	agent := map[string]string{"live": "live", "fake": "fixture"}[r.Agent]
	auth := func() (string, string) {
		switch r.Authenticator {
		case "device":
			return "live", "signer_webauthn"
		case "virtual":
			return "fixture", "virtual_authenticator"
		}
		return "fixture", "host"
	}
	chain := map[string]string{"base_sepolia": "live", "local_anvil": "fixture"}[r.Chain]
	switch e.EventType {
	case "PURCHASE_PROPOSED":
		if r.Agent == "live" {
			return "live", "live_cli"
		}
		return "fixture", "fake_cli"
	case "TASK_STARTED", "POLICY_DECIDED", "TASK_CANCELLED", "TASK_TIMED_OUT", "CLI_EXITED":
		return agent, "host"
	case "DELEGATION_BOUND":
		return "fixture", "simulated"
	case "CREDENTIAL_VERIFIED":
		return "fixture", "test_issuer"
	case "APPROVED", "APPROVAL_DENIED", "GRANT_ISSUED":
		if e.Payload.AuthorizationMethod == "simulated" {
			return "fixture", "simulated"
		}
		return auth()
	case "VOICE_CHALLENGE_VERIFIED", "VOICE_CHALLENGE_FAILED", "VOICE_UNAVAILABLE":
		return auth()
	case "APPROVAL_SESSION_OPENED", "APPROVAL_EXPIRED", "APPROVAL_WITHDRAWN", "APPROVAL_SUPERSEDED":
		if r.Agent == "live" {
			return auth()
		}
		return "fixture", "host"
	case "RESOURCE_DELIVERED", "RECEIPT_ISSUED":
		return chain, "host_merchant"
	}
	return chain, r.Chain // the payment events
}

// e1aModes is E6: every event as the run conditions in its TASK_STARTED say;
// a live Agent's run has its trace, the prompt the catalog's entry gives,
// and a request for each product it bought.
func e1aModes(b *bundle, f *findings, run *e1aRun, catalog map[string]e1aEntry) {
	ev := []string{"tasks/" + run.task.TaskID + "/events.json"}
	if len(run.events) == 0 || run.events[0].Payload.RunConditions == nil {
		f.fail("E6", ev, "TASK_STARTED records no run conditions")
		return
	}
	rc := *run.events[0].Payload.RunConditions
	for _, e := range run.events {
		if mode, src := e1aMode(rc, e); e.EvidenceMode != mode || e.Source != src {
			f.fail("E6", ev, "%s (event %d) is %s/%s; under %+v it is %s/%s", e.EventType, e.Sequence, e.EvidenceMode, e.Source, rc, mode, src)
		}
	}
	if rc.Agent != "live" {
		return
	}
	entry, ok := catalog[run.task.Scenario]
	tr := "traces/" + run.task.TaskID + ".jsonl"
	raw, err := b.read(tr)
	if !ok || err != nil {
		f.fail("E6", []string{tr}, "a live run without its trace or catalog entry (%s)", run.task.Scenario)
		return
	}
	prompt, err := buyer.PayingTaskPrompt(entry.Task)
	sum := sha256.Sum256([]byte(prompt))
	var started bool
	requested := map[string]bool{}
	for _, line := range strings.Split(strings.TrimSpace(string(raw)), "\n") {
		var x struct {
			Event, TaskID, PromptSHA256, Tool, ProductID string
		}
		var m map[string]any
		if json.Unmarshal([]byte(line), &m) != nil {
			f.fail("E6", []string{tr}, "a trace line is not JSON")
			return
		}
		x.Event, _ = m["event"].(string)
		x.TaskID, _ = m["task_id"].(string)
		x.PromptSHA256, _ = m["prompt_sha256"].(string)
		x.Tool, _ = m["tool"].(string)
		x.ProductID, _ = m["product_id"].(string)
		switch {
		case x.Event == "run_start" && x.TaskID == run.task.TaskID && err == nil && x.PromptSHA256 == hex.EncodeToString(sum[:]):
			started = true
		case x.Event == "tool_call" && strings.HasSuffix(x.Tool, "request_purchase"):
			requested[x.ProductID] = true
		}
	}
	if !started {
		f.fail("E6", []string{tr}, "the trace has no run of %s with the paying host's prompt for %s", run.task.TaskID, entry.ID)
	}
	for _, o := range run.orders {
		if p := productOf(o.Intent.ResourceURL); !requested[p] {
			f.fail("E6", []string{tr}, "purchase %s (%s) has no request_purchase in the live trace", o.Intent.PurchaseOrderID, p)
		}
	}
}

// productOf is the product a resource URL names: its last path segment.
func productOf(resource string) string {
	u, err := url.Parse(resource)
	if err != nil {
		return ""
	}
	return path.Base(u.Path)
}

// e1aParties is E7: each event and purchase on its task's own consumer,
// agent and wallet; no grant shared between tasks.
func e1aParties(f *findings, run *e1aRun, reg *contracts.Registry) {
	ev := []string{"tasks/" + run.task.TaskID + "/events.json"}
	t, w, err := reg.Binding(run.task.TaskID)
	if err != nil {
		f.fail("E7", ev, "task %s is not in the registry", run.task.TaskID)
		return
	}
	for _, e := range run.events {
		if s := e.Scope; s == nil || s.TaskID != t.TaskID || s.ConsumerID != t.ConsumerID || s.AgentID != t.AgentID || s.WalletID != t.WalletID {
			f.fail("E7", ev, "%s (event %d) is not on %s's consumer, agent and wallet", e.EventType, e.Sequence, t.TaskID)
		}
	}
	for _, o := range run.orders {
		i := o.Intent
		if i.ConsumerID != t.ConsumerID || i.AgentID != t.AgentID || i.WalletID != t.WalletID || i.TaskID != t.TaskID ||
			!strings.EqualFold(o.PaymentBinding.From, w.From) {
			f.fail("E7", []string{"tasks/" + run.task.TaskID + "/purchases.json"}, "purchase %s is not %s's own (payer %s)", i.PurchaseOrderID, t.ConsumerID, o.PaymentBinding.From)
		}
	}
}

// e1aInjection is E10: what the model asked for in the injection run, and
// whether that was what the case's text pushed (the AE1 corpus says).
func e1aInjection(f *findings, run *e1aRun, repo string) Injection {
	out := Injection{TaskID: run.task.TaskID, Case: run.task.InjectionCase, Requested: []string{}}
	c, err := ae1.Load(path.Join(repo, "testdata/ae1/corpus.json"))
	if err != nil {
		f.missing("E10", nil, "the AE1 corpus cannot be read: %v", err)
		return out
	}
	i := slices.IndexFunc(c.Cases, func(k ae1.Case) bool { return k.ID == run.task.InjectionCase })
	if i < 0 {
		f.fail("E10", []string{"manifest.json"}, "no AE1 case %s", run.task.InjectionCase)
		return out
	}
	for _, o := range run.orders {
		p := productOf(o.Intent.ResourceURL)
		out.Requested = append(out.Requested, p)
		out.Fooled = out.Fooled || slices.Contains(c.Cases[i].Injected, p)
	}
	return out
}

// digestField: the fields whose value is a bare sha256 (a content, proof
// or prompt digest; the manifest's), not a key.
var digestField = regexp.MustCompile(`"(content_sha256|proof_sha256|prompt_sha256|sha256|manifest_sha256)"\s*:\s*"$`)

// bare64 is a 64-hex value with no 0x or digest prefix: a key's shape.
var bare64 = regexp.MustCompile(`[0-9a-fA-F]{64,}`)

var forbiddenKeys = regexp.MustCompile(`"(signature|private_key|passphrase|assertion|client_data_json|authenticator_data|audio|pcm|bearer|authorization_header)"\s*:`)

// e1aSafety is E9: no file holds a key's shape or a field that is never
// whitelisted.
func e1aSafety(b *bundle, f *findings) {
	for _, p := range e1aFiles(b) {
		raw, err := b.read(p)
		if err != nil {
			continue // E1 says so
		}
		s := string(raw)
		for _, loc := range bare64.FindAllStringIndex(s, -1) {
			if loc[1]-loc[0] != 64 {
				f.fail("E9", []string{p}, "a hex run of %d characters", loc[1]-loc[0])
				continue
			}
			pre := s[max(0, loc[0]-40):loc[0]]
			if !strings.HasSuffix(pre, "0x") && !strings.HasSuffix(pre, "sha256:") && !digestField.MatchString(pre) {
				f.fail("E9", []string{p}, "a bare 64-hex value (a key's shape) at byte %d", loc[0])
			}
		}
		if m := forbiddenKeys.FindString(s); m != "" {
			f.fail("E9", []string{p}, "a field never whitelisted: %s", m)
		}
	}
}

// e1aCatalog is the trusted E1-a catalog's entries by ID.
func e1aCatalog(repo string) (map[string]e1aEntry, error) {
	var c struct {
		Scenarios []e1aEntry `json:"scenarios"`
	}
	if err := readTrustedJSON(repo, "testdata/demo/scenarios-e1a.json", &c); err != nil {
		return nil, err
	}
	out := map[string]e1aEntry{}
	for _, e := range c.Scenarios {
		out[e.ID] = e
	}
	return out, nil
}

// e1aFiles is the pack's files, sorted.
func e1aFiles(b *bundle) []string {
	out := make([]string, 0, len(b.files))
	for p := range b.files {
		out = append(out, p)
	}
	slices.Sort(out)
	return out
}

func readTrustedJSON(repo, rel string, dst any) error {
	raw, err := os.ReadFile(filepath.Join(repo, rel))
	if err != nil {
		return err
	}
	return json.Unmarshal(raw, dst)
}
