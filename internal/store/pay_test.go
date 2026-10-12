package store

// Tests of apa.pay.v1 in the Store (P1-a; docs/commercial-transaction-flow.md
// sections 7, 10, 11): delegation, the Ask's approval session, approve and
// deny in one transaction, resends, races with cancel and the deadline, a
// changed quote, the version boundary and the recovery check.

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/SIMPLYBOYS/agent-payment-authority/internal/contracts"
	"github.com/SIMPLYBOYS/agent-payment-authority/internal/m1evidence"
	"github.com/SIMPLYBOYS/agent-payment-authority/internal/paycontract"
)

func v1Of(t *testing.T, m contracts.PurchaseOrder) paycontract.PurchaseOrder {
	t.Helper()
	i := m.Intent
	rh, _ := paycontract.ResourceHash(i.HTTPMethod, i.ResourceURL)
	qh, err := paycontract.RequirementHash(m.Quote)
	if err != nil {
		t.Fatal(err)
	}
	return paycontract.PurchaseOrder{Version: paycontract.Version, RequestID: m.RequestID, Quote: m.Quote,
		Intent: paycontract.Intent{Version: paycontract.Version, Domain: paycontract.Domain, ConsumerID: i.ConsumerID, AgentID: i.AgentID, WalletID: i.WalletID,
			TaskID: i.TaskID, PurchaseOrderID: i.PurchaseOrderID, ResourceHash: rh, HTTPMethod: i.HTTPMethod, ResourceURL: i.ResourceURL, X402Version: i.X402Version,
			Scheme: i.Scheme, PaymentRequirementHash: qh},
		PaymentBinding: paycontract.PaymentBinding{Method: "eip3009", ChainID: i.ChainID, AssetContract: i.AssetContract, From: i.From, PayTo: i.PayTo,
			Amount: i.Amount, ValidAfter: i.ValidAfter, ValidBefore: i.ValidBefore, Nonce: "0x" + fmt.Sprintf("%064x", 7)}}
}

func (e *env) delegation(s *Store, consumer, req string, deadline uint64) string {
	e.t.Helper()
	d, _, err := s.ConfirmDelegation(DelegationRequest{RequestID: req, ConsumerID: consumer, Scenario: "pay", Purpose: "a brief", AssetID: e.profile.AssetID,
		Merchants: e.profile.Merchants, TaskBudget: "5000000", AllowMax: "100000", ApprovalDeadlineSeconds: deadline, ValidSeconds: 600, ConfirmedBy: "simulated:" + consumer + ":host-test"})
	if err != nil {
		e.t.Fatalf("ConfirmDelegation: %v", err)
	}
	return d.DelegationID
}

// payTask starts task as apa.pay.v1 with a fresh delegation and submits the
// base purchase as v1: an Ask with its session. It returns the session ID
// and intent hash.
func (e *env) payTask(s *Store, task string, deadline uint64) (string, string) {
	e.t.Helper()
	consumer := map[string]string{"task-a": "consumer-a", "task-a-next": "consumer-a", "task-b": "consumer-b"}[task]
	setup := e.setup(task)
	setup.ContractVersion, setup.DelegationID = paycontract.Version, e.delegation(s, consumer, "dlg-req-"+task+"-000000000", deadline)
	if err := s.StartTask(setup); err != nil {
		e.t.Fatalf("StartTask: %v", err)
	}
	o := v1Of(e.t, forTask(e.t, e.base, task))
	r := submitV1(e.t, s, contracts.Session{ConsumerID: consumer, AgentID: o.Intent.AgentID, TaskID: task}, o)
	if r.Purchase.Decision.PolicyVerdict != "Ask" || r.Purchase.Approval == nil || r.Purchase.Approval.State != "pending" {
		e.t.Fatalf("v1 Ask: %+v", r.Purchase)
	}
	return r.Purchase.Approval.SessionID, r.Purchase.IntentHash
}

func submitV1(t *testing.T, s *Store, sess contracts.Session, o paycontract.PurchaseOrder) Result {
	t.Helper()
	b, _ := json.Marshal(o)
	r, err := s.Submit(sess, b)
	if err != nil {
		t.Fatalf("Submit v1: %v", err)
	}
	return r
}

func approve(s *Store, session, intent, req, consumer, decision string) (ApprovalResult, error) {
	return s.ResolveApproval(ApprovalRequest{RequestID: req, SessionID: session, ConsumerID: consumer, IntentHash: intent, Decision: decision, Approver: "simulated:" + consumer + ":host-test"})
}

func TestPayAskApproveOnce(t *testing.T) {
	e := newEnv(t)
	s := e.open()
	id, intent := e.payTask(s, "task-a", 30)
	if types := eventTypes(t, s, sessA); fmt.Sprint(types) != "[TASK_STARTED DELEGATION_BOUND PURCHASE_PROPOSED POLICY_DECIDED APPROVAL_SESSION_OPENED]" {
		t.Fatalf("events %v", types)
	}
	r, err := approve(s, id, intent, "approve-req-000000001", "consumer-a", "approve")
	if err != nil || r.Outcome != "approved" || r.Grant == nil || r.Grant.AuthorizationMethod != "simulated" || r.Grant.JTI != paycontract.GrantID(id) {
		t.Fatalf("approve: %+v %v", r, err)
	}
	// Resend, and resend after a restart: the same answer, one grant.
	for i := 0; i < 2; i++ {
		if i == 1 {
			s.Close()
			s = e.open()
		}
		again, err := approve(s, id, intent, "approve-req-000000001", "consumer-a", "approve")
		if err != nil || !again.Replay || again.Outcome != "approved" || again.Grant == nil || again.Grant.JTI != r.Grant.JTI || count(t, e.path, "grants", "") != 1 {
			t.Fatalf("resend %d: %+v %v", i, again, err)
		}
	}
	// Another request for the decided session: closed, still one grant.
	if r, err := approve(s, id, intent, "approve-req-000000002", "consumer-a", "approve"); err != nil || r.Outcome != "closed" || count(t, e.path, "grants", "") != 1 {
		t.Fatalf("another request: %+v %v", r, err)
	}
	v, _ := s.Task(sessA)
	if v.ContractVersion != paycontract.Version || v.Status != "running" || v.Purchases[0].Approval.State != "approved" || v.Purchases[0].Decision.PaymentStatus != "not_enabled" {
		t.Fatalf("task view %+v", v)
	}
}

// A failure after the grant row is written leaves nothing: the session
// stays pending, no grant, no event (one transaction).
func TestPayApproveAtomic(t *testing.T) {
	e := newEnv(t)
	s := e.open()
	id, intent := e.payTask(s, "task-a", 30)
	before := fingerprint(t, e.path)
	s.hook = func(stage string) error {
		if stage == "after-grant" {
			return errors.New("injected")
		}
		return nil
	}
	if _, err := approve(s, id, intent, "approve-req-000000001", "consumer-a", "approve"); err == nil {
		t.Fatal("the injected failure did not fail")
	}
	s.hook = nil
	if fingerprint(t, e.path) != before || count(t, e.path, "grants", "") != 0 || count(t, e.path, "approval_sessions", "state = 'pending'") != 1 {
		t.Fatal("a half approval was written")
	}
	if r, err := approve(s, id, intent, "approve-req-000000001", "consumer-a", "approve"); err != nil || r.Outcome != "approved" {
		t.Fatalf("after the failure: %+v %v", r, err)
	}
}

func TestPayDenyAndOtherConsumer(t *testing.T) {
	e := newEnv(t)
	s := e.open()
	id, intent := e.payTask(s, "task-a", 30)
	if _, err := approve(s, id, intent, "approve-req-00000000b", "consumer-b", "approve"); Code(err) != "OWNERSHIP_MISMATCH" {
		t.Fatalf("B approves A's session: %v", err)
	}
	if _, err := approve(s, "aps-unknown", intent, "approve-req-00000000c", "consumer-a", "approve"); Code(err) != "OWNERSHIP_MISMATCH" {
		t.Fatalf("an unknown session: %v", err)
	}
	if _, err := approve(s, id, "sha256:"+fmt.Sprintf("%064x", 1), "approve-req-00000000d", "consumer-a", "approve"); Code(err) != "INTENT_MISMATCH" {
		t.Fatalf("another intent: %v", err)
	}
	r, err := approve(s, id, intent, "deny-req-0000000000001", "consumer-a", "deny")
	if err != nil || r.Outcome != "denied" || r.Grant != nil || count(t, e.path, "grants", "") != 0 {
		t.Fatalf("deny: %+v %v", r, err)
	}
	if _, err := approve(s, id, intent, "deny-req-0000000000001", "consumer-a", "approve"); Code(err) != "IDEMPOTENCY_CONFLICT" {
		t.Fatalf("the deny's ID asking to approve: %v", err)
	}
	if types := eventTypes(t, s, sessA); types[len(types)-1] != "APPROVAL_DENIED" {
		t.Fatalf("events %v", types)
	}
}

// The deadline is the backend's: one second before it an approval stands;
// at it the session expires in the approval's own transaction.
func TestPayDeadline(t *testing.T) {
	for _, c := range []struct {
		name    string
		advance uint64
		want    string
	}{{"just before", 29, "approved"}, {"at the deadline", 30, "closed"}} {
		t.Run(c.name, func(t *testing.T) {
			e := newEnv(t)
			s := e.open()
			id, intent := e.payTask(s, "task-a", 30)
			e.clock.Add(c.advance)
			r, err := approve(s, id, intent, "approve-req-000000001", "consumer-a", "approve")
			if err != nil || r.Outcome != c.want {
				t.Fatalf("%+v %v", r, err)
			}
			if c.want == "closed" && (r.Purchase.Approval.State != "expired" || count(t, e.path, "grants", "") != 0) {
				t.Fatalf("not expired: %+v", r.Purchase.Approval)
			}
		})
	}
}

// Approve and cancel at once: exactly one wins; never an approved session
// of a cancel that withdrew it, never a grant without its approval.
func TestPayApproveCancelRace(t *testing.T) {
	for n := 0; n < 10; n++ {
		e := newEnv(t)
		s := e.open()
		id, intent := e.payTask(s, "task-a", 30)
		var wg sync.WaitGroup
		var r ApprovalResult
		var aerr, cerr error
		wg.Go(func() { r, aerr = approve(s, id, intent, "race-req-0000000000001", "consumer-a", "approve") })
		wg.Go(func() { cerr = s.Cancel("task-a") })
		wg.Wait()
		if aerr != nil || cerr != nil {
			t.Fatalf("round %d: %v / %v", n, aerr, cerr)
		}
		state := map[string]string{"approved": "approved", "closed": "withdrawn"}[r.Outcome]
		grants := map[string]int{"approved": 1, "withdrawn": 0}[state]
		if state == "" || count(t, e.path, "approval_sessions", "state = ?", state) != 1 || count(t, e.path, "grants", "") != grants {
			t.Fatalf("round %d: outcome %s, sessions %d approved %d withdrawn, grants %d", n, r.Outcome,
				count(t, e.path, "approval_sessions", "state = 'approved'"), count(t, e.path, "approval_sessions", "state = 'withdrawn'"), count(t, e.path, "grants", ""))
		}
		s.Close()
		e.open() // the recovery check passes either way
	}
}

// A quote the host no longer offers supersedes the session at approval.
func TestPaySuperseded(t *testing.T) {
	e := newEnv(t)
	s := e.open()
	id, intent := e.payTask(s, "task-a", 30)
	s.Close()
	e.offers = OfferBook{} // the shop changed its quotes
	s = e.open()
	r, err := approve(s, id, intent, "approve-req-000000001", "consumer-a", "approve")
	if err != nil || r.Outcome != "superseded" || r.Purchase.Approval.State != "superseded" || count(t, e.path, "grants", "") != 0 {
		t.Fatalf("%+v %v", r, err)
	}
}

// The stored task's version picks the entry; neither body crosses over.
func TestPayVersionBoundary(t *testing.T) {
	e := newEnv(t)
	s := e.open()
	e.start(s, "task-a") // an M1 task
	b, _ := json.Marshal(v1Of(t, e.base))
	if _, err := s.Submit(sessA, b); Code(err) != "CONTRACT_VERSION_MISMATCH" {
		t.Fatalf("v1 body, M1 task: %v", err)
	}
	e.payTask(s, "task-b", 30)
	if _, err := s.Submit(sessB, raw(t, forTask(t, variant(t, e.base, 9), "task-b"))); Code(err) != "CONTRACT_VERSION_MISMATCH" {
		t.Fatalf("M1 body, v1 task: %v", err)
	}
	// The M1 task itself is unchanged: its Ask is M1's.
	r := submit(t, s, sessA, e.base)
	if r.Purchase.Decision.Version != contracts.Version || r.Purchase.Approval != nil || r.Purchase.Decision.PaymentStatus != "unavailable" {
		t.Fatalf("M1 purchase: %+v", r.Purchase)
	}
}

func TestPayDelegationGuards(t *testing.T) {
	e := newEnv(t)
	s := e.open()
	setup := e.setup("task-a")
	setup.ContractVersion = paycontract.Version
	if err := s.StartTask(setup); Code(err) != "DELEGATION_UNAVAILABLE" {
		t.Fatalf("no delegation: %v", err)
	}
	setup.DelegationID = e.delegation(s, "consumer-b", "dlg-req-b-0000000000001", 30)
	if err := s.StartTask(setup); Code(err) != "DELEGATION_UNAVAILABLE" {
		t.Fatalf("another consumer's delegation: %v", err)
	}
	m1 := e.setup("task-a")
	m1.DelegationID = setup.DelegationID
	if err := s.StartTask(m1); Code(err) != "INVALID_TASK_SETUP" {
		t.Fatalf("an M1 task with a delegation: %v", err)
	}
	wide, _, err := s.ConfirmDelegation(DelegationRequest{RequestID: "dlg-req-wide-00000000001", ConsumerID: "consumer-a", Scenario: "pay", Purpose: "x", AssetID: e.profile.AssetID,
		Merchants: e.profile.Merchants, TaskBudget: "9000000", AllowMax: "100000", ApprovalDeadlineSeconds: 30, ValidSeconds: 600, ConfirmedBy: "simulated:consumer-a:host-test"})
	if err != nil {
		t.Fatal(err)
	}
	setup.DelegationID = wide.DelegationID
	if err := s.StartTask(setup); Code(err) != "DELEGATION_UNAVAILABLE" {
		t.Fatalf("a delegation wider than the profile: %v", err)
	}
	// A delegation runs one task: bound once; expired is unavailable.
	id := e.delegation(s, "consumer-a", "dlg-req-a-0000000000001", 30)
	setup.DelegationID = id
	if err := s.StartTask(setup); err != nil {
		t.Fatal(err)
	}
	next := e.setup("task-a-next")
	next.ContractVersion, next.DelegationID = paycontract.Version, id
	if err := s.StartTask(next); Code(err) != "DELEGATION_UNAVAILABLE" {
		t.Fatalf("bound twice: %v", err)
	}
	next.DelegationID = e.delegation(s, "consumer-a", "dlg-req-a-0000000000002", 30)
	e.clock.Add(600)
	if err := s.StartTask(next); Code(err) != "DELEGATION_UNAVAILABLE" {
		t.Fatalf("expired: %v", err)
	}
	// The same request ID confirms the same delegation; other terms conflict.
	if again, replay, err := s.ConfirmDelegation(DelegationRequest{RequestID: "dlg-req-a-0000000000001", ConsumerID: "consumer-a", Scenario: "pay", Purpose: "a brief", AssetID: e.profile.AssetID,
		Merchants: e.profile.Merchants, TaskBudget: "5000000", AllowMax: "100000", ApprovalDeadlineSeconds: 30, ValidSeconds: 600, ConfirmedBy: "simulated:consumer-a:host-test"}); err != nil || !replay || again.DelegationID != id || again.TaskID != "task-a" {
		t.Fatalf("resend: %+v %t %v", again, replay, err)
	}
	if _, _, err := s.ConfirmDelegation(DelegationRequest{RequestID: "dlg-req-a-0000000000001", ConsumerID: "consumer-a", Scenario: "pay", Purpose: "another", AssetID: e.profile.AssetID,
		Merchants: e.profile.Merchants, TaskBudget: "5000000", AllowMax: "100000", ApprovalDeadlineSeconds: 30, ValidSeconds: 600, ConfirmedBy: "simulated:consumer-a:host-test"}); Code(err) != "IDEMPOTENCY_CONFLICT" {
		t.Fatalf("other terms: %v", err)
	}
	// The run record: a delegation goes with apa.pay.v1 only, the consumer's own, held by one open run.
	if _, _, err := s.AcceptRun(RunRequest{RequestID: "run-req-000000000000001", Scenario: "pay", ScenarioDigest: "d", ConsumerID: "consumer-a", Candidates: []string{"task-a-next"},
		ContractVersion: paycontract.Version, DelegationID: next.DelegationID}); Code(err) != "DELEGATION_UNAVAILABLE" {
		t.Fatalf("an expired delegation in a run: %v", err)
	}
	if _, _, err := s.AcceptRun(RunRequest{RequestID: "run-req-000000000000002", Scenario: "pay", ScenarioDigest: "d", ConsumerID: "consumer-a", Candidates: []string{"task-a-next"},
		ContractVersion: paycontract.Version}); Code(err) != "INVALID_RUN_RECORD" {
		t.Fatalf("a v1 run without delegation: %v", err)
	}
}

// The recovery check refuses a grant without its approval and a session
// whose intent is not its purchase's.
func TestPayRecoveryRefuses(t *testing.T) {
	for _, c := range []struct{ name, sql string }{
		{"a grant of a pending session", `UPDATE approval_sessions SET state = 'pending', closed_at = NULL, resolve_request_id = NULL, resolved_by = NULL`},
		{"a session with another intent", `UPDATE approval_sessions SET intent_hash = 'sha256:` + fmt.Sprintf("%064x", 5) + `'`},
		{"no grant for an approval", `DELETE FROM grants`},
		{"a session moved to an M1 task", `UPDATE tasks SET contract_version = 'apa.m1.v2'`},
	} {
		t.Run(c.name, func(t *testing.T) {
			e := newEnv(t)
			s := e.open()
			id, intent := e.payTask(s, "task-a", 30)
			if _, err := approve(s, id, intent, "approve-req-000000001", "consumer-a", "approve"); err != nil {
				t.Fatal(err)
			}
			s.Close()
			db := rawDB(t, e.path)
			if _, err := db.Exec(`PRAGMA foreign_keys = OFF; PRAGMA ignore_check_constraints = ON; ` + c.sql); err != nil {
				t.Fatal(err)
			}
			_, err := Open(Config{Path: e.path, Registry: e.reg, Offers: e.offers, Clock: e.clock.clock()})
			var ce *CorruptError
			if !errors.As(err, &ce) {
				t.Fatalf("Open: %v", err)
			}
		})
	}
}

// A schema 2 file (M1 tasks and a run) upgrades to 3 with --upgrade-db:
// backed up, the data unchanged, its tasks and run apa.m1.v2; without the
// flag it is refused and left as it was.
func TestUpgradeSchema2(t *testing.T) {
	e := newEnv(t)
	older(t, e, 2)
	before, _ := os.ReadFile(e.path)
	if _, err := openOpt(e, false); Code(err) != "SCHEMA_UPGRADE_REQUIRED" {
		t.Fatalf("without the flag: %v", err)
	}
	if after, _ := os.ReadFile(e.path); string(after) != string(before) {
		t.Fatal("a refused schema 2 file changed")
	}
	want := fingerprint(t, e.path)
	s, err := openOpt(e, true)
	if err != nil {
		t.Fatal(err)
	}
	r, found, err := s.RunByRequest(runReq("old-run", "consumer-a", "task-a").RequestID)
	s.Close()
	if userVersion(t, e.path) != 3 || fingerprint(t, e.path) != want || err != nil || !found || r.ContractVersion != contracts.Version || r.DelegationID != "" ||
		count(t, e.path, "tasks", "contract_version = 'apa.m1.v2'") != 1 {
		t.Fatalf("upgrade: v%d run %+v %v", userVersion(t, e.path), r, err)
	}
	if m, _ := filepath.Glob(e.path + ".schema2-*.bak"); len(m) != 1 {
		t.Fatalf("backups %v", m)
	}
}

// The M1 evidence export and checker refuse an apa.pay.v1 task by its own
// code; an M1 task of the same file still exports.
func TestM1EvidenceRefusesPayTask(t *testing.T) {
	e := newEnv(t)
	s := e.open()
	e.start(s, "task-a")
	submit(t, s, sessA, e.base)
	e.payTask(s, "task-b", 30)
	s.Close()
	if _, err := m1evidence.Export(e.path, "task-b"); contracts.ErrorCode(err) != "M1_CONTRACT_REQUIRED" {
		t.Fatalf("export of the v1 task: %v", err)
	}
	exported, err := m1evidence.Export(e.path, "task-a")
	if err != nil {
		t.Fatal(err)
	}
	var m contracts.CaseManifest
	readFixture(t, "testdata/contracts/cases.json", &m)
	x := exported[0]
	x.Events = append([]m1evidence.Stored{}, x.Events...)
	x.Events[0].Body = strings.Replace(x.Events[0].Body, `"version":"apa.m1.v2"`, `"version":"apa.pay.v1"`, 1)
	_, problems := m1evidence.CheckIntegration(m.Cases[0], m.Defaults, []m1evidence.Task{x})
	if !strings.Contains(fmt.Sprint(problems), "M1_CONTRACT_REQUIRED") {
		t.Fatalf("the checker on a v1 event: %v", problems)
	}
}
