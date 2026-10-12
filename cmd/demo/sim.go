package main

// The fake-only simulated approver of P1-a (docs/commercial-transaction-flow.md
// sections 7 and 9). The operator asks to act as one consumer (POST
// /api/host/simulate); the host maps the consumer from its registry and
// makes a short-lived capability bound to that consumer and this host
// process, held in the page's memory only. One consumer at a time: a new
// one replaces the old. With it, and only with it, the page confirms that
// consumer's delegation and approves or denies that consumer's approval
// sessions. viewer, host and operator tokens never do; a live host refuses
// all of it (simulation_unavailable). It is not a person's verification:
// every record says simulated, and a simulated grant cannot pay.

import (
	"crypto/rand"
	"crypto/subtle"
	"encoding/hex"
	"fmt"
	"net/http"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/SIMPLYBOYS/agent-payment-authority/internal/paycontract"
	"github.com/SIMPLYBOYS/agent-payment-authority/internal/signer"
	"github.com/SIMPLYBOYS/agent-payment-authority/internal/store"
)

const simTTL = 30 * time.Minute

type simulator struct {
	mu        sync.Mutex
	token     string
	consumer  string
	expires   time.Time // monotonic
	consumers []string  // the registry's
	now       func() time.Time
	instance  string
}

func (s *simulator) issue(consumer string) (string, time.Time, error) {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		return "", time.Time{}, err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.token, s.consumer, s.expires = hex.EncodeToString(b), consumer, s.now().Add(simTTL)
	return s.token, s.expires, nil
}

// holder is the consumer tok acts for, or "".
func (s *simulator) holder(tok string) string {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.token == "" || s.now().After(s.expires) || subtle.ConstantTimeCompare([]byte(tok), []byte(s.token)) != 1 {
		return ""
	}
	return s.consumer
}

// approver names who acted in every record: the consumer, simulated, by
// this host process.
func (s *simulator) approver(consumer string) string {
	return "simulated:" + consumer + ":host-" + s.instance[:12]
}

func (a *api) registerSim(mux *http.ServeMux) {
	mux.HandleFunc("POST /api/host/simulate", a.simulate)
	mux.HandleFunc("GET /api/sim/approvals", a.simApprovals)
	mux.HandleFunc("POST /api/sim/delegations", a.simDelegation)
	mux.HandleFunc("POST /api/sim/approvals/{session_id}", a.simResolve)
}

// simulate is POST /api/host/simulate {consumer_id}: the operator, same
// origin, fake mode only.
func (a *api) simulate(w http.ResponseWriter, r *http.Request) {
	var req struct {
		ConsumerID string `json:"consumer_id"`
	}
	if !postGuard(w, r, a, &req) {
		return
	}
	if a.sim == nil {
		fail(w, http.StatusForbidden, "simulation_unavailable")
		return
	}
	if !slices.Contains(a.sim.consumers, req.ConsumerID) {
		fail(w, http.StatusBadRequest, "unknown_consumer")
		return
	}
	tok, until, err := a.sim.issue(req.ConsumerID)
	if err != nil {
		fail(w, http.StatusServiceUnavailable, "host_error")
		return
	}
	fmt.Fprintf(a.out, "demo: simulated approver: %s (fake only; not a person's verification)\n", req.ConsumerID)
	reply(w, http.StatusOK, map[string]any{"token": tok, "consumer_id": req.ConsumerID, "expires_in_seconds": int(time.Until(until).Seconds()),
		"note": "角色模擬：主持人扮演，不是本人驗證；只在 fake 模式；不會付款"})
}

// simGuard admits the simulated approver's capability only: Host, Origin
// (required on a POST), then the bearer. A demo token is the wrong role; a
// live host has no simulation at all.
func (a *api) simGuard(w http.ResponseWriter, r *http.Request, post bool, dst any) (string, bool) {
	if r.Host != a.addr {
		fail(w, http.StatusForbidden, "forbidden_host")
		return "", false
	}
	if o := r.Header.Get("Origin"); (post || o != "") && o != "http://"+a.addr {
		fail(w, http.StatusForbidden, "forbidden_origin")
		return "", false
	}
	if a.sim == nil {
		fail(w, http.StatusForbidden, "simulation_unavailable")
		return "", false
	}
	tok, _ := strings.CutPrefix(r.Header.Get("Authorization"), "Bearer ")
	consumer := a.sim.holder(tok)
	if consumer == "" {
		for _, t := range a.tokens {
			if subtle.ConstantTimeCompare([]byte(tok), []byte(t.Token)) == 1 {
				fail(w, http.StatusForbidden, "forbidden_role") // viewer, host and operator never approve
				return "", false
			}
		}
		fail(w, http.StatusUnauthorized, "unauthenticated")
		return "", false
	}
	if post && !strictJSON(w, r, dst) {
		return "", false
	}
	return consumer, true
}

// simApprovals is GET /api/sim/approvals: the simulated consumer's approval
// sessions, newest first, with what each was opened for.
func (a *api) simApprovals(w http.ResponseWriter, r *http.Request) {
	consumer, ok := a.simGuard(w, r, false, nil)
	if !ok {
		return
	}
	items, err := a.st.Approvals(consumer, 10)
	if err != nil {
		fail(w, http.StatusServiceUnavailable, "store_unavailable")
		return
	}
	reply(w, http.StatusOK, map[string]any{"consumer_id": consumer, "approver": a.sim.approver(consumer), "served_at": a.clock(), "approvals": items,
		"payment_capability": paycontract.PaymentNotEnabled})
}

// simDelegation is POST /api/sim/delegations {request_id, scenario}: the
// simulated consumer confirms the host's terms for an apa.pay.v1 scenario of
// that consumer. Nothing in the body sets a term.
func (a *api) simDelegation(w http.ResponseWriter, r *http.Request) {
	var req struct {
		RequestID string `json:"request_id"`
		Scenario  string `json:"scenario"`
	}
	consumer, ok := a.simGuard(w, r, true, &req)
	if !ok {
		return
	}
	sc := a.control.scenarios[req.Scenario]
	switch {
	case !requestIDFormat.MatchString(req.RequestID):
		fail(w, http.StatusBadRequest, "bad_request")
		return
	case sc == nil || sc.Delegation == nil:
		fail(w, http.StatusBadRequest, "unknown_scenario")
		return
	case sc.ConsumerID != consumer:
		fail(w, http.StatusForbidden, "forbidden_consumer")
		return
	}
	d, p := sc.Delegation, a.control.profile
	view, replay, err := a.st.ConfirmDelegation(store.DelegationRequest{RequestID: req.RequestID, ConsumerID: consumer, Scenario: sc.ID, Purpose: d.Purpose,
		AssetID: p.AssetID, Merchants: p.Merchants, TaskBudget: d.TaskBudget, AllowMax: d.AllowMax, ApprovalDeadlineSeconds: d.ApprovalDeadlineSeconds,
		ValidSeconds: d.ValidSeconds, ConfirmedBy: a.sim.approver(consumer)})
	switch code := store.Code(err); {
	case code == "IDEMPOTENCY_CONFLICT":
		fail(w, http.StatusConflict, "request_id_conflict")
	case code == "INVALID_DELEGATION":
		fail(w, http.StatusBadRequest, "invalid_delegation")
	case err != nil:
		reply(w, http.StatusServiceUnavailable, map[string]string{"error": "store_unavailable", "code": store.CodeOf(err)})
	default:
		status := http.StatusCreated
		if replay {
			status = http.StatusOK
		}
		reply(w, status, map[string]any{"delegation": view, "replay": replay})
	}
}

// simResolve is POST /api/sim/approvals/{session_id} {request_id,
// intent_hash, decision}: approve or deny one session of the simulated
// consumer. The Store decides everything in one transaction.
func (a *api) simResolve(w http.ResponseWriter, r *http.Request) {
	var req struct {
		RequestID  string `json:"request_id"`
		IntentHash string `json:"intent_hash"`
		Decision   string `json:"decision"`
	}
	consumer, ok := a.simGuard(w, r, true, &req)
	if !ok {
		return
	}
	if !requestIDFormat.MatchString(req.RequestID) || (req.Decision != "approve" && req.Decision != "deny") || !paycontract.ValidID(r.PathValue("session_id")) {
		fail(w, http.StatusBadRequest, "bad_request")
		return
	}
	res, err := a.st.ResolveApproval(store.ApprovalRequest{RequestID: req.RequestID, SessionID: r.PathValue("session_id"), ConsumerID: consumer,
		IntentHash: req.IntentHash, Decision: req.Decision, Approver: a.sim.approver(consumer)})
	switch code := store.Code(err); {
	case code == "OWNERSHIP_MISMATCH": // unknown and another consumer's look the same
		fail(w, http.StatusNotFound, "not_found")
	case code == "INTENT_MISMATCH":
		fail(w, http.StatusConflict, "intent_mismatch")
	case code == "IDEMPOTENCY_CONFLICT":
		fail(w, http.StatusConflict, "request_id_conflict")
	case code == "INVALID_APPROVAL":
		fail(w, http.StatusBadRequest, "bad_request")
	case err != nil:
		reply(w, http.StatusServiceUnavailable, map[string]string{"error": "store_unavailable", "code": store.CodeOf(err)})
	case res.Outcome == "closed" || res.Outcome == "superseded":
		reply(w, http.StatusConflict, map[string]any{"error": "approval_" + res.Outcome, "result": res})
	default:
		out := map[string]any{"result": res, "payment_capability": paycontract.PaymentNotEnabled}
		if res.Grant != nil { // the stub's rule only: no key, no signature, no payment
			out["signer_stub"] = signer.Accept(*res.Grant)
		}
		reply(w, http.StatusOK, out)
	}
}
