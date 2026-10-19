package main

import (
	"crypto/rand"
	"crypto/subtle"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"

	"github.com/SIMPLYBOYS/agent-payment-authority/internal/contracts"
	"github.com/SIMPLYBOYS/agent-payment-authority/internal/paycontract"
	"github.com/SIMPLYBOYS/agent-payment-authority/internal/store"
)

// grant is what a demo token may do: read one consumer's tasks (viewer),
// read every task (host), or control runs and read no task (operator,
// docs/demo-operation-design.md section 13.1). It is the host's mapping,
// never a request's claim.
type grant struct {
	Role       string `json:"role"`                  // viewer | host | operator
	ConsumerID string `json:"consumer_id,omitempty"` // viewers only
}

func (g grant) sees(consumerID string) bool {
	return g.Role == "host" || g.Role == "viewer" && g.ConsumerID == consumerID
}

type demoToken struct {
	grant
	Token string `json:"token"`
}

// newTokens makes a fresh token per registry consumer (viewer), one for the
// host and one for the operator, and writes them to path (directory 0700, file 0600) by a rename,
// so no reader sees half a file. Tokens never go to the terminal or a log.
// The rename never replaces one of db's files: checked again just before
// it, when the database, its WAL and the lock exist.
func newTokens(cfg contracts.RegistryConfig, path, db string, inputs ...string) ([]demoToken, error) {
	var out []demoToken
	add := func(g grant) error {
		b := make([]byte, 32)
		if _, err := rand.Read(b); err != nil {
			return err
		}
		out = append(out, demoToken{grant: g, Token: hex.EncodeToString(b)})
		return nil
	}
	for _, c := range cfg.Consumers {
		if err := add(grant{Role: "viewer", ConsumerID: c.ConsumerID}); err != nil {
			return nil, err
		}
	}
	if err := add(grant{Role: "host"}); err != nil {
		return nil, err
	}
	if err := add(grant{Role: "operator"}); err != nil {
		return nil, err
	}
	abs, err := filepath.Abs(path)
	if err != nil {
		return nil, err
	}
	dir := filepath.Dir(abs)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return nil, err
	}
	if fi, err := os.Stat(dir); err != nil || fi.Mode().Perm()&0o077 != 0 {
		return nil, fmt.Errorf("token directory %s must be private (0700)", dir)
	}
	body, _ := json.MarshalIndent(map[string]any{
		"note":   "Demo capabilities for this run of the demo host only; new ones are made at each start. Not a user login.",
		"tokens": out,
	}, "", "  ")
	tmp, err := os.CreateTemp(dir, ".demo-tokens-*")
	if err != nil {
		return nil, err
	}
	defer os.Remove(tmp.Name()) // a no-op after the rename
	if _, err := tmp.Write(body); err != nil {
		tmp.Close()
		return nil, err
	}
	if err := tmp.Close(); err != nil {
		return nil, err
	}
	if err := checkTokenPath(db, abs, inputs...); err != nil {
		return nil, err
	}
	if err := os.Rename(tmp.Name(), abs); err != nil { // CreateTemp made it 0600
		return nil, err
	}
	return out, nil
}

// api is the authenticated read-only task API. Every handler reads the
// Store only: no StartTask, Submit, Cancel or ExpireDue.
type api struct {
	st       *store.Store
	tokens   []demoToken
	addr     string     // the bound address, the Host and Origin accepted (with alias)
	alias    string     // A2: localhost:<port>, the trusted name WebAuthn needs (decision D3); "" otherwise
	approve  *approvals // A2: the human approval relay (--approvals-signer); nil otherwise
	clock    store.Clock
	unity    *os.Root // the Unity Web build's Build/ directory; nil without a build
	expiry   *expiryHealth
	fake     bool         // a fake CLI passed its --version check at start
	control  *control     // host-started runs (--scenarios); nil otherwise
	history  *historyInfo // history mode (--history-db); nil otherwise
	live     *liveInfo    // live mode (--live-cli); nil otherwise
	traceDir string       // live mode: where the run traces are
	pairing  *pairing     // --pairing: the host page's one-time code; nil otherwise
	sim      *simulator   // fake mode with --scenarios: the simulated approver (P1-a); nil otherwise
	out      io.Writer    // the host's log (fixed lines only)
	payments bool         // P2-a: --payments (LocalAnvil)
}

func (a *api) register(mux *http.ServeMux) {
	mux.HandleFunc("GET /api/tasks", a.tasks)
	mux.HandleFunc("GET /api/tasks/{task_id}", a.task)
	mux.HandleFunc("GET /api/tasks/{task_id}/events", a.events)
}

func reply(w http.ResponseWriter, status int, v any) {
	h := w.Header()
	h.Set("Content-Type", "application/json")
	h.Set("Cache-Control", "no-store")
	h.Set("X-Content-Type-Options", "nosniff")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(v)
}

func fail(w http.ResponseWriter, status int, code string) {
	reply(w, status, map[string]string{"error": code})
}

// ourHost and ourOrigin are an exact list: the bound address and, with
// A2, localhost on the same port; never a wildcard or a null origin.
func (a *api) ourHost(h string) bool { return h == a.addr || a.alias != "" && h == a.alias }

func (a *api) ourOrigin(o string) bool {
	return o == "http://"+a.addr || a.alias != "" && o == "http://"+a.alias
}

// authorize checks Host, Origin and the bearer token, in that order. Only
// the Authorization header counts; nothing in the URL, the body or another
// header names who the caller is.
func (a *api) authorize(w http.ResponseWriter, r *http.Request) (grant, bool) {
	if !a.ourHost(r.Host) {
		fail(w, http.StatusForbidden, "forbidden_host")
		return grant{}, false
	}
	if o := r.Header.Get("Origin"); o != "" && !a.ourOrigin(o) {
		fail(w, http.StatusForbidden, "forbidden_origin")
		return grant{}, false
	}
	tok, ok := strings.CutPrefix(r.Header.Get("Authorization"), "Bearer ")
	if ok {
		for _, t := range a.tokens {
			if subtle.ConstantTimeCompare([]byte(tok), []byte(t.Token)) == 1 {
				return t.grant, true
			}
		}
	}
	fail(w, http.StatusUnauthorized, "unauthenticated")
	return grant{}, false
}

// record reads the path's task as stored, for the caller's grant. The
// grant is applied to the task's stored consumer, so a task the current
// registry no longer lists stays readable by the host and by its own
// consumer's viewer only. Unknown, not visible and not started all answer
// the same 404, so a viewer learns nothing about another consumer's tasks.
func (a *api) record(w http.ResponseWriter, r *http.Request) (store.TaskRecord, bool) {
	g, ok := a.authorize(w, r)
	if !ok {
		return store.TaskRecord{}, false
	}
	if g.Role == "operator" { // controls runs, reads no task content
		fail(w, http.StatusForbidden, "forbidden_role")
		return store.TaskRecord{}, false
	}
	rec, err := a.st.ReadTask(r.PathValue("task_id"), g.sees)
	if err != nil {
		if store.Code(err) == "OWNERSHIP_MISMATCH" {
			fail(w, http.StatusNotFound, "not_found")
		} else { // its text stays here
			fail(w, http.StatusServiceUnavailable, "store_unavailable")
		}
		return store.TaskRecord{}, false
	}
	return rec, true
}

// taskReply is GET /api/tasks/{task_id}: the task's stored binding and its
// committed projection, from one read, and fixed statements of what M1
// cannot do. Nothing comes from the current registry.
type taskReply struct {
	TaskID             string               `json:"task_id"`
	ContractVersion    string               `json:"contract_version"`
	DelegationID       string               `json:"delegation_id,omitempty"`
	ConsumerID         string               `json:"consumer_id"`
	AgentID            string               `json:"agent_id"`
	WalletID           string               `json:"wallet_id"`
	Payer              string               `json:"payer"`
	WalletEvidenceMode string               `json:"wallet_evidence_mode"` // fixture: a reference, not a key
	Status             string               `json:"status"`
	DenyReason         string               `json:"deny_reason,omitempty"`
	AsOfSequence       uint64               `json:"as_of_sequence"`
	AsOfObservedAt     uint64               `json:"as_of_observed_at"`
	ServedAt           uint64               `json:"served_at"`
	Purchases          []store.PurchaseView `json:"purchases"`
	PaymentCapability  string               `json:"payment_capability"`
}

func (a *api) task(w http.ResponseWriter, r *http.Request) {
	rec, ok := a.record(w, r)
	if !ok {
		return
	}
	b, v := rec.Binding, rec.Task
	capability := "unavailable" // M1 cannot pay
	if paycontract.Supported(v.ContractVersion) {
		capability = paycontract.PaymentNotEnabled // P1-a: an approval, simulated, is not a payment
		if a.payments {
			capability = LocalAnvil
		}
	}
	reply(w, http.StatusOK, taskReply{TaskID: b.TaskID, ContractVersion: v.ContractVersion, DelegationID: v.DelegationID, ConsumerID: b.ConsumerID,
		AgentID: b.AgentID, WalletID: b.WalletID, Payer: b.Payer,
		WalletEvidenceMode: b.WalletEvidenceMode, Status: v.Status, DenyReason: v.DenyReason, AsOfSequence: v.AsOfSequence,
		AsOfObservedAt: v.AsOfObservedAt, ServedAt: a.clock(), Purchases: v.Purchases, PaymentCapability: capability})
}

// eventView is the whitelisted projection of one stored event, copied as
// stored: nothing is recomputed or renamed.
type eventView struct {
	EventID         string                    `json:"event_id"`
	EventType       string                    `json:"event_type"`
	Sequence        uint64                    `json:"sequence"`
	PrevEventHash   string                    `json:"prev_event_hash,omitempty"`
	PurchaseOrderID string                    `json:"purchase_order_id,omitempty"`
	IntentHash      string                    `json:"intent_hash,omitempty"`
	Code            string                    `json:"code,omitempty"`
	Decision        *contracts.PolicyDecision `json:"decision,omitempty"`
	// apa.pay.v1 events only.
	DelegationID        string `json:"delegation_id,omitempty"`
	ApprovalSessionID   string `json:"approval_session_id,omitempty"`
	GrantID             string `json:"grant_id,omitempty"`
	AuthorizationMethod string `json:"authorization_method,omitempty"`
	// apa.pay.v2's CREDENTIAL_VERIFIED: the verification record as stored.
	Credential   *contracts.CredentialCheck `json:"credential,omitempty"`
	Version      string                     `json:"version,omitempty"`
	EvidenceMode string                     `json:"evidence_mode"`
	Source       string                     `json:"source"`
	ObservedAt   uint64                     `json:"observed_at"`
}

type eventsReply struct {
	TaskID       string      `json:"task_id"`
	AsOfSequence uint64      `json:"as_of_sequence"`
	Events       []eventView `json:"events"`
}

func (a *api) events(w http.ResponseWriter, r *http.Request) {
	rec, ok := a.record(w, r)
	if !ok {
		return
	}
	out := eventsReply{TaskID: rec.Binding.TaskID, Events: []eventView{}}
	for _, e := range rec.Events {
		v := eventView{EventID: e.EventID, EventType: e.EventType, Sequence: e.Sequence, PrevEventHash: e.PrevEventHash,
			Code: e.Payload.Code, Decision: e.Payload.Decision, EvidenceMode: e.EvidenceMode, Source: e.Source, ObservedAt: e.ObservedAt,
			DelegationID: e.Payload.DelegationID, ApprovalSessionID: e.Payload.ApprovalSessionID, GrantID: e.Payload.GrantID, AuthorizationMethod: e.Payload.AuthorizationMethod,
			Credential: e.Payload.Credential}
		if paycontract.Supported(e.Version) { // an M1 event's view is what it always was
			v.Version = e.Version
		}
		if e.Scope != nil {
			v.PurchaseOrderID, v.IntentHash = e.Scope.PurchaseOrderID, e.Scope.IntentHash
		}
		out.Events = append(out.Events, v)
		out.AsOfSequence = e.Sequence
	}
	reply(w, http.StatusOK, out)
}

// tasksReply is GET /api/tasks: the caller's visible tasks, newest first
// (docs/demo-operation-design.md section 14). A viewer gets its own
// consumer's tasks by their stored consumer; the host gets all; the
// operator, which reads no task, is refused.
type tasksReply struct {
	Tasks    []store.TaskSummary `json:"tasks"`
	ServedAt uint64              `json:"served_at"`
}

const taskListLimit = 50 // ponytail: newest 50, no paging; page when a demo keeps more

func (a *api) tasks(w http.ResponseWriter, r *http.Request) {
	g, ok := a.authorize(w, r)
	if !ok {
		return
	}
	consumer := ""
	switch g.Role {
	case "viewer":
		if consumer = g.ConsumerID; consumer == "" { // "" would list every consumer's
			fail(w, http.StatusForbidden, "forbidden_role")
			return
		}
	case "host":
	default:
		fail(w, http.StatusForbidden, "forbidden_role")
		return
	}
	list, err := a.st.ListTasks(consumer, taskListLimit)
	if err != nil {
		fail(w, http.StatusServiceUnavailable, "store_unavailable")
		return
	}
	reply(w, http.StatusOK, tasksReply{Tasks: list, ServedAt: a.clock()})
}
