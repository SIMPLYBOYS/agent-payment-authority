package main

// A2: the person's approval through serve-approvals
// (docs/commercial-human-approval-design.md 5-8). The Host relays: the
// Signer issues the challenge, verifies the assertion and keeps the
// decision; the Store keeps the attempt (each ID written before its call)
// and, from the Signer's answer only, the session's result. A viewer token
// of the session's consumer may read, ask for a challenge and relay an
// assertion; it can never approve or deny by itself. host, operator and
// the simulated approver get no challenge. These routes answer on the
// trusted name only (localhost:<port>, decision D3). Nothing here keeps an
// assertion, and logs carry fixed codes and IDs only. Payment stays off:
// serve-approvals has no payment route.

import (
	"crypto/rand"
	"crypto/subtle"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"slices"
	"strings"
	"sync"

	"github.com/SIMPLYBOYS/agent-payment-authority/internal/paycontract"
	"github.com/SIMPLYBOYS/agent-payment-authority/internal/signer"
	"github.com/SIMPLYBOYS/agent-payment-authority/internal/signerclient"
	"github.com/SIMPLYBOYS/agent-payment-authority/internal/store"
	"github.com/SIMPLYBOYS/agent-payment-authority/web"
)

// approvalStore is the Store's part of the relay (tests wrap it to make a
// write fail).
type approvalStore interface {
	BeginApprovalAttempt(store.AttemptRequest) (store.AttemptStart, error)
	UpdateApprovalAttempt(store.AttemptUpdate) (store.ApprovalAttempt, error)
	ApprovalAttemptOf(consumer, requestID string) (store.ApprovalAttempt, store.AttemptStart, error)
	ResolveWebAuthn(store.WebAuthnResult) (store.ApprovalResult, error)
	ResolveVoiceFailure(consumer, requestID, code string) (store.ApprovalResult, error)
	ApprovalOf(consumer, session string) (store.ApprovalItem, error)
}

// approvalSigner is the Signer's part (signerclient.Approvals; tests make
// one call fail).
type approvalSigner interface {
	Challenge(signer.ChallengeRequest) signerclient.Answer
	ChallengeByRequest(id string) signerclient.Answer
	ChallengeByID(id string) signerclient.Answer
	Close(challengeID, closeRequestID string) signerclient.Answer
	Verify(signer.VerifyRequest) signerclient.Answer
	ByVerifyRequest(id string) signerclient.Answer
	Approval(id string) signerclient.Answer
	Abandon(id string) signerclient.Answer
	Health() signerclient.Answer
	EnrollRelay(id string, body map[string]any) (int, json.RawMessage)
}

type approvals struct {
	st     approvalStore
	signer approvalSigner
	source string // device | virtual: what the operator says verifies (a label, not evidence)
	out    io.Writer
	mu     sync.Mutex // ponytail: one approval operation at a time on this host; per-session locks if it ever matters
}

// sources are the three origins every answer names apart (design 2).
type sources struct {
	Agent         string `json:"agent"`         // fake
	Authenticator string `json:"authenticator"` // device | virtual
	Payment       string `json:"payment"`       // disabled
}

func (h *approvals) sources() sources {
	return sources{Agent: "fake", Authenticator: h.source, Payment: "disabled"}
}

type challengeView struct {
	ChallengeID       string          `json:"challenge_id"`
	ConsumerID        string          `json:"consumer_id"`
	ApprovalSessionID string          `json:"approval_session_id"`
	IntentHash        string          `json:"intent_hash"`
	Decision          string          `json:"decision"`
	ApproverID        string          `json:"approver_id"`
	ExpiresAt         uint64          `json:"expires_at"`
	Options           json.RawMessage `json:"options"`
	Display           *signer.Display `json:"display,omitempty"`
}

// approvalReply is every approval route's answer to the page.
type approvalReply struct {
	Attempt       *store.ApprovalAttempt `json:"attempt,omitempty"`
	Kind          string                 `json:"kind,omitempty"`
	Challenge     *challengeView         `json:"challenge,omitempty"`
	Outcome       string                 `json:"outcome,omitempty"`
	Item          *store.ApprovalItem    `json:"item,omitempty"`
	Error         string                 `json:"error,omitempty"`
	Code          string                 `json:"code,omitempty"` // the Signer's fixed code
	StateNotSaved bool                   `json:"state_not_saved,omitempty"`
	Sources       sources                `json:"sources"`
}

func (a *api) registerApprovals(mux *http.ServeMux) {
	mux.HandleFunc("GET /api/approvals", a.approvalList)
	mux.HandleFunc("GET /api/approvals/{session_id}", a.approvalItem)
	mux.HandleFunc("POST /api/approvals/{session_id}/challenge", a.approvalChallenge)
	mux.HandleFunc("POST /api/approvals/{session_id}/assertion", a.approvalAssertion)
	mux.HandleFunc("POST /api/approvals/{session_id}/attempts/{request_id}/cancel", a.approvalCancel)
	mux.HandleFunc("GET /api/approvals/{session_id}/attempts/{request_id}", a.approvalStatus)
	mux.HandleFunc("GET /approve/enroll", a.enrollPage)
	mux.HandleFunc("POST /api/enrollments/{id}/options", a.enrollRelay)
	mux.HandleFunc("POST /api/enrollments/{id}", a.enrollRelay)
}

// approvalGuard: the trusted name (Host, and Origin on a POST), then the
// bearer. viewerOnly routes admit a viewer token alone (its consumer);
// the others also the host token (read only, any consumer).
func (a *api) approvalGuard(w http.ResponseWriter, r *http.Request, post, viewerOnly bool, limit int64, dst any) (grant, bool) {
	if r.Host != a.alias {
		fail(w, http.StatusForbidden, "forbidden_host")
		return grant{}, false
	}
	if o := r.Header.Get("Origin"); (post || o != "") && o != "http://"+a.alias {
		fail(w, http.StatusForbidden, "forbidden_origin")
		return grant{}, false
	}
	tok, _ := strings.CutPrefix(r.Header.Get("Authorization"), "Bearer ")
	var g grant
	found := false
	for _, t := range a.tokens {
		if subtle.ConstantTimeCompare([]byte(tok), []byte(t.Token)) == 1 {
			g, found = t.grant, true
		}
	}
	if !found { // the simulated approver's capability is not a token either
		fail(w, http.StatusUnauthorized, "unauthenticated")
		return grant{}, false
	}
	if g.Role == "operator" || g.Role == "host" && viewerOnly || g.Role == "viewer" && g.ConsumerID == "" {
		fail(w, http.StatusForbidden, "forbidden_role")
		return grant{}, false
	}
	if post && !strictJSONLimit(w, r, dst, limit) {
		return grant{}, false
	}
	return g, true
}

// consumerOf: a viewer's own consumer; the host names one (?consumer=).
func consumerOf(g grant, r *http.Request) string {
	if g.Role == "viewer" {
		return g.ConsumerID
	}
	return r.URL.Query().Get("consumer")
}

func (a *api) approvalList(w http.ResponseWriter, r *http.Request) {
	g, ok := a.approvalGuard(w, r, false, false, 0, nil)
	if !ok {
		return
	}
	consumer := consumerOf(g, r)
	if c := r.URL.Query().Get("consumer"); g.Role == "viewer" && c != "" && c != consumer {
		fail(w, http.StatusNotFound, "not_found")
		return
	}
	items, err := a.st.Approvals(consumer, 10)
	if err != nil {
		fail(w, http.StatusServiceUnavailable, "store_unavailable")
		return
	}
	signerState := "unavailable"
	if ans := a.approve.signer.Health(); ans.Kind == signerclient.KindOK && (ans.Reply.State == "approvals-only" || ans.Reply.State == "ready") {
		signerState = ans.Reply.State // ready: payments are signed too (P2)
	}
	reply(w, http.StatusOK, map[string]any{"consumer_id": consumer, "served_at": a.clock(), "approvals": items, "sources": a.approve.sources(),
		"signer": signerState, "payment_capability": a.approvalsCapability()})
}

func (a *api) approvalItem(w http.ResponseWriter, r *http.Request) {
	g, ok := a.approvalGuard(w, r, false, false, 0, nil)
	if !ok {
		return
	}
	it, err := a.st.ApprovalOf(consumerOf(g, r), r.PathValue("session_id"))
	if store.Code(err) == "OWNERSHIP_MISMATCH" {
		fail(w, http.StatusNotFound, "not_found")
		return
	} else if err != nil {
		fail(w, http.StatusServiceUnavailable, "store_unavailable")
		return
	}
	reply(w, http.StatusOK, map[string]any{"item": it, "served_at": a.clock(), "sources": a.approve.sources()})
}

func (a *api) approvalChallenge(w http.ResponseWriter, r *http.Request) {
	var req struct {
		RequestID string `json:"request_id"`
		Decision  string `json:"decision"`
	}
	g, ok := a.approvalGuard(w, r, true, true, 1024, &req)
	if !ok {
		return
	}
	h := a.approve
	h.mu.Lock()
	defer h.mu.Unlock()
	status, out := h.challenge(g.ConsumerID, r.PathValue("session_id"), req.RequestID, req.Decision)
	h.answer(w, "challenge", g.ConsumerID, r.PathValue("session_id"), status, out)
}

func (a *api) approvalAssertion(w http.ResponseWriter, r *http.Request) {
	var req struct {
		RequestID       string              `json:"request_id"`
		VerifyRequestID string              `json:"verify_request_id"`
		Assertion       json.RawMessage     `json:"assertion"`
		Voice           *signer.VoiceAnswer `json:"voice,omitempty"` // V1: relayed as it is; the Signer judges it
	}
	g, ok := a.approvalGuard(w, r, true, true, 512<<10, &req) // V1: a recording of up to 10 s, base64
	if !ok {
		return
	}
	h := a.approve
	h.mu.Lock()
	defer h.mu.Unlock()
	status, out := h.assertion(g.ConsumerID, r.PathValue("session_id"), req.RequestID, req.VerifyRequestID, req.Assertion, req.Voice)
	h.answer(w, "assertion", g.ConsumerID, r.PathValue("session_id"), status, out)
}

func (a *api) approvalCancel(w http.ResponseWriter, r *http.Request) {
	var req struct{}
	g, ok := a.approvalGuard(w, r, true, true, 1024, &req)
	if !ok {
		return
	}
	h := a.approve
	h.mu.Lock()
	defer h.mu.Unlock()
	status, out := h.cancel(g.ConsumerID, r.PathValue("session_id"), r.PathValue("request_id"))
	h.answer(w, "cancel", g.ConsumerID, r.PathValue("session_id"), status, out)
}

func (a *api) approvalStatus(w http.ResponseWriter, r *http.Request) {
	g, ok := a.approvalGuard(w, r, false, true, 0, nil)
	if !ok {
		return
	}
	h := a.approve
	h.mu.Lock()
	defer h.mu.Unlock()
	status, out := h.status(g.ConsumerID, r.PathValue("session_id"), r.PathValue("request_id"))
	h.answer(w, "status", g.ConsumerID, r.PathValue("session_id"), status, out)
}

// answer adds the item as stored now (when it can be read) and the
// sources, logs one fixed line and replies.
func (h *approvals) answer(w http.ResponseWriter, op, consumer, session string, status int, out approvalReply) {
	if out.Error != "not_found" && out.Error != "bad_request" {
		if it, err := h.st.ApprovalOf(consumer, session); err == nil {
			out.Item = &it
		}
	}
	out.Sources = h.sources()
	state := ""
	if out.Attempt != nil {
		state = out.Attempt.State
	}
	fmt.Fprintf(h.out, "demo: approval %s %d %s %s %s\n", op, status, state, out.Error, out.Code)
	reply(w, status, out)
}

func newRequestID(prefix string) string {
	b := make([]byte, 12)
	rand.Read(b)
	return prefix + hex.EncodeToString(b)
}

func attemptPtr(a store.ApprovalAttempt) *store.ApprovalAttempt { return &a }

// update moves the attempt; a failure is reported as state_not_saved.
func (h *approvals) update(consumer string, a store.ApprovalAttempt, from []string, state string, u store.AttemptUpdate) (store.ApprovalAttempt, bool) {
	u.ConsumerID, u.RequestID, u.From, u.State = consumer, a.RequestID, from, state
	b, err := h.st.UpdateApprovalAttempt(u)
	if err != nil {
		return a, false
	}
	return b, true
}

var openStates = []string{"requested", "challenged", "verifying", "unknown", "refused"}

// challenge is POST .../challenge: the Store's attempt first (new, its own
// resend, the session's open attempt of the same decision, or a
// conflict), then the Signer.
func (h *approvals) challenge(consumer, session, requestID, decision string) (int, approvalReply) {
	if !requestIDFormat.MatchString(requestID) || !paycontract.ValidID(session) {
		return http.StatusBadRequest, approvalReply{Error: "bad_request"}
	}
	st, err := h.st.BeginApprovalAttempt(store.AttemptRequest{ConsumerID: consumer, SessionID: session, RequestID: requestID, Decision: decision})
	switch store.Code(err) {
	case "":
	case "OWNERSHIP_MISMATCH":
		return http.StatusNotFound, approvalReply{Error: "not_found"}
	case "DECISION_CONFLICT":
		return http.StatusConflict, approvalReply{Error: "decision_conflict", Attempt: attemptPtr(st.Attempt)}
	case "SESSION_CLOSED":
		if st.Attempt.Open() {
			return h.closeOnSession(consumer, st)
		}
		return http.StatusConflict, approvalReply{Error: "session_closed"}
	case "IDEMPOTENCY_CONFLICT":
		return http.StatusConflict, approvalReply{Error: "request_id_conflict"}
	case "TASK_ALREADY_ENDED": // a CLI timeout or a Deny: nobody decides this Ask now (cli-timeout-pending)
		return http.StatusConflict, approvalReply{Error: "task_ended"}
	case "INVALID_APPROVAL":
		return http.StatusBadRequest, approvalReply{Error: "bad_request"}
	default:
		return http.StatusServiceUnavailable, approvalReply{Error: "store_unavailable"}
	}
	status, out := h.drive(consumer, st, st.Kind == "new")
	out.Kind = st.Kind
	return status, out
}

// drive brings an attempt as far as the Signer's authoritative state
// allows, without a new assertion: an issued challenge's options, a used
// one's decision, a closed or expired one's end.
func (h *approvals) drive(consumer string, st store.AttemptStart, fresh bool) (int, approvalReply) {
	a := st.Attempt
	switch {
	case !a.Open():
		return http.StatusOK, approvalReply{Attempt: &a}
	case st.SessionState != "pending":
		return h.closeOnSession(consumer, st)
	case a.State == "verifying" || a.State == "unknown" && a.VerifyRequestID != "":
		return h.recoverVerify(consumer, st)
	case a.ChallengeID != "":
		return h.onChallenge(consumer, st, h.signer.ChallengeByID(a.ChallengeID), false)
	}
	if !fresh { // a challenge may have been made by a request whose answer was lost (breakpoint 1)
		if ans := h.signer.ChallengeByRequest(a.RequestID); ans.Kind != signerclient.KindNotFound {
			return h.onChallenge(consumer, st, ans, false)
		}
	}
	return h.onChallenge(consumer, st, h.signer.Challenge(signer.ChallengeRequest{RequestID: a.RequestID, ApprovalSessionID: a.SessionID,
		Decision: a.Decision, Order: st.Order, HostDeadline: st.Deadline}), true)
}

// onChallenge applies the Signer's answer about the attempt's challenge.
// issuing: it answered the request to make one.
func (h *approvals) onChallenge(consumer string, st store.AttemptStart, ans signerclient.Answer, issuing bool) (int, approvalReply) {
	a := st.Attempt
	switch ans.Kind {
	case signerclient.KindOK:
	case signerclient.KindRefused:
		if !issuing {
			return http.StatusBadGateway, approvalReply{Error: "signer_refused", Code: ans.Code, Attempt: &a}
		}
		b, ok := h.update(consumer, a, []string{"requested", "unknown"}, "refused", store.AttemptUpdate{ErrCode: ans.Code})
		return http.StatusConflict, approvalReply{Error: "signer_refused", Code: ans.Code, Attempt: &b, StateNotSaved: !ok}
	case signerclient.KindNotFound:
		return http.StatusBadGateway, approvalReply{Error: "signer_inconsistent", Code: ans.Code, Attempt: &a}
	default:
		if !issuing { // a failed read: the attempt stays as it is
			return http.StatusServiceUnavailable, approvalReply{Error: "result_unknown", Attempt: &a}
		}
		b, ok := h.update(consumer, a, []string{"requested", "unknown"}, "unknown", store.AttemptUpdate{})
		return http.StatusServiceUnavailable, approvalReply{Error: "result_unknown", Attempt: &b, StateNotSaved: !ok}
	}
	r := ans.Reply
	if r.RequestID != a.RequestID || r.ApprovalSessionID != a.SessionID || r.Decision != a.Decision || r.IntentHash != st.IntentHash || r.ConsumerID != consumer {
		// Not this attempt's challenge: never shown, never prompted for.
		b, ok := h.update(consumer, a, openStates, "refused", store.AttemptUpdate{ErrCode: "SIGNER_MISMATCH"})
		return http.StatusBadGateway, approvalReply{Error: "signer_mismatch", Attempt: &b, StateNotSaved: !ok}
	}
	switch r.State {
	case "issued":
		b, ok := h.update(consumer, a, openStates, "challenged", store.AttemptUpdate{ChallengeID: r.ChallengeID})
		if !ok {
			return http.StatusServiceUnavailable, approvalReply{Error: "store_unavailable", Attempt: &a, StateNotSaved: true}
		}
		return http.StatusOK, approvalReply{Attempt: &b, Challenge: &challengeView{ChallengeID: r.ChallengeID, ConsumerID: r.ConsumerID,
			ApprovalSessionID: r.ApprovalSessionID, IntentHash: r.IntentHash, Decision: r.Decision, ApproverID: r.ApproverID, ExpiresAt: r.ExpiresAt,
			Options: r.Options, Display: r.Display}}
	case "used":
		return h.resolveUsed(consumer, st, r.ApprovalID)
	case "closed", "expired":
		b, ok := h.update(consumer, a, openStates, r.State, store.AttemptUpdate{ChallengeID: r.ChallengeID})
		return http.StatusOK, approvalReply{Attempt: &b, StateNotSaved: !ok}
	}
	return http.StatusBadGateway, approvalReply{Error: "signer_inconsistent", Attempt: &a}
}

// assertion is POST .../assertion: the verify request ID is written
// before the call; the Signer verifies; only its decision moves the
// session.
func (h *approvals) assertion(consumer, session, requestID, verifyID string, assertion json.RawMessage, voice *signer.VoiceAnswer) (int, approvalReply) {
	if !requestIDFormat.MatchString(requestID) || !requestIDFormat.MatchString(verifyID) || len(assertion) == 0 {
		return http.StatusBadRequest, approvalReply{Error: "bad_request"}
	}
	a, st, err := h.st.ApprovalAttemptOf(consumer, requestID)
	switch {
	case store.Code(err) == "OWNERSHIP_MISMATCH" || err == nil && a.SessionID != session:
		return http.StatusNotFound, approvalReply{Error: "not_found"}
	case err != nil:
		return http.StatusServiceUnavailable, approvalReply{Error: "store_unavailable"}
	case a.State == "verified" && a.VerifyRequestID == verifyID: // a resend: the saved result, asked again by the same ID
		return h.onVerify(consumer, st, h.signer.ByVerifyRequest(verifyID))
	case a.Open() && st.SessionState != "pending": // nothing is sent to the Signer (review R3)
		return h.closeOnSession(consumer, st)
	case !a.Open() || a.ChallengeID == "":
		return http.StatusConflict, approvalReply{Error: "attempt_not_ready", Attempt: &a}
	case a.VerifyRequestID != "" && a.VerifyRequestID != verifyID && (a.State == "verifying" || a.State == "unknown"):
		return http.StatusConflict, approvalReply{Error: "attempt_busy", Attempt: &a} // another assertion's result is not known yet
	}
	b, ok := h.update(consumer, a, []string{"challenged", "refused", "verifying", "unknown"}, "verifying", store.AttemptUpdate{VerifyRequestID: verifyID})
	if !ok { // nothing was sent
		return http.StatusServiceUnavailable, approvalReply{Error: "store_unavailable", Attempt: &a}
	}
	st.Attempt = b
	return h.onVerify(consumer, st, h.signer.Verify(signer.VerifyRequest{VerifyRequestID: verifyID, ChallengeID: b.ChallengeID, Order: st.Order, Assertion: assertion, Voice: voice}))
}

// onVerify applies the Signer's answer to a verify request (sent now, or
// found again by its ID).
func (h *approvals) onVerify(consumer string, st store.AttemptStart, ans signerclient.Answer) (int, approvalReply) {
	a := st.Attempt
	switch ans.Kind {
	case signerclient.KindOK:
		return h.resolve(consumer, st, ans.Reply)
	case signerclient.KindRefused:
		if slices.Contains(paycontract.VoiceFailures, ans.Code) { // V1: a failed voice challenge denies the session
			res, err := h.st.ResolveVoiceFailure(consumer, a.RequestID, ans.Code)
			if err != nil {
				fmt.Fprintf(h.out, "demo: voice failure not recorded: %s\n", store.CodeOf(err))
				return http.StatusServiceUnavailable, approvalReply{Error: "store_unavailable", Code: ans.Code, Attempt: &a, StateNotSaved: true}
			}
			b, _, _ := h.st.ApprovalAttemptOf(consumer, a.RequestID)
			return http.StatusOK, approvalReply{Attempt: &b, Outcome: res.Outcome, Code: ans.Code}
		}
		switch ans.Code {
		case "CHALLENGE_CLOSED", "CHALLENGE_EXPIRED":
			state := map[string]string{"CHALLENGE_CLOSED": "closed", "CHALLENGE_EXPIRED": "expired"}[ans.Code]
			b, ok := h.update(consumer, a, openStates, state, store.AttemptUpdate{})
			return http.StatusConflict, approvalReply{Error: "signer_refused", Code: ans.Code, Attempt: &b, StateNotSaved: !ok}
		case "CHALLENGE_USED": // another verify request used it: that decision is the session's
			if c := h.signer.ChallengeByID(a.ChallengeID); c.Kind == signerclient.KindOK && c.Reply.State == "used" {
				return h.resolveUsed(consumer, st, c.Reply.ApprovalID)
			}
			return http.StatusServiceUnavailable, approvalReply{Error: "result_unknown", Attempt: &a}
		}
		b, ok := h.update(consumer, a, openStates, "refused", store.AttemptUpdate{ErrCode: ans.Code})
		return http.StatusConflict, approvalReply{Error: "signer_refused", Code: ans.Code, Attempt: &b, StateNotSaved: !ok}
	case signerclient.KindNotFound:
		return http.StatusBadGateway, approvalReply{Error: "signer_inconsistent", Code: ans.Code, Attempt: &a}
	}
	b, ok := h.update(consumer, a, openStates, "unknown", store.AttemptUpdate{})
	return http.StatusServiceUnavailable, approvalReply{Error: "result_unknown", Attempt: &b, StateNotSaved: !ok}
}

// resolve records the Signer's verified decision. When the session closed
// first (or the answer is not this attempt's), a verified approve is
// abandoned at the Signer.
func (h *approvals) resolve(consumer string, st store.AttemptStart, r signer.ApprovalReply) (int, approvalReply) {
	a := st.Attempt
	res, err := h.st.ResolveWebAuthn(store.WebAuthnResult{ConsumerID: consumer, RequestID: a.RequestID, Decision: r.Decision, SignerApprovalID: r.ApprovalID,
		ApproverID: r.ApproverID, CredentialRef: r.CredentialRef, IntentHash: r.IntentHash, ExpiresAt: r.ExpiresAt, Voice: voiceOf(r).Result, VoiceReason: voiceOf(r).Reason})
	if err != nil { // the Signer verified it; the Host could not record it yet: the status route asks again by the same IDs
		return http.StatusServiceUnavailable, approvalReply{Error: "store_unavailable", Attempt: &a, StateNotSaved: true}
	}
	if res.Outcome != "approved" && res.Outcome != "denied" && r.Decision == "approve" && r.State == "verified" {
		ab := h.signer.Abandon(r.ApprovalID)
		fmt.Fprintf(h.out, "demo: approval abandon %s %s\n", ab.Kind, ab.Code)
	}
	b, _, _ := h.st.ApprovalAttemptOf(consumer, a.RequestID)
	return http.StatusOK, approvalReply{Attempt: &b, Outcome: res.Outcome}
}

// voiceOf is the Signer's voice outcome of an approval (none: the zero value).
func voiceOf(r signer.ApprovalReply) signer.VoiceResult {
	if r.VoiceResult == nil {
		return signer.VoiceResult{}
	}
	return *r.VoiceResult
}

// resolveUsed: the challenge was used; its decision, read by approval ID.
func (h *approvals) resolveUsed(consumer string, st store.AttemptStart, approvalID string) (int, approvalReply) {
	ans := h.signer.Approval(approvalID)
	if ans.Kind != signerclient.KindOK {
		return http.StatusServiceUnavailable, approvalReply{Error: "result_unknown", Attempt: &st.Attempt}
	}
	return h.resolve(consumer, st, ans.Reply)
}

// recoverVerify: an assertion was sent and its result is not known
// (breakpoint 2). Its verify request ID is asked first; nothing found
// means only that nothing was found yet, so the challenge is closed and
// its authoritative state decides: closed (the attempt is lost), used
// (that decision), expired.
func (h *approvals) recoverVerify(consumer string, st store.AttemptStart) (int, approvalReply) {
	a := st.Attempt
	ans := h.signer.ByVerifyRequest(a.VerifyRequestID)
	switch ans.Kind {
	case signerclient.KindOK, signerclient.KindRefused:
		return h.onVerify(consumer, st, ans)
	case signerclient.KindNotFound:
		return h.closeChallenge(consumer, st, true)
	}
	return http.StatusServiceUnavailable, approvalReply{Error: "result_unknown", Attempt: &a}
}

// closeChallenge closes the attempt's challenge at the Signer (its close
// request ID written first) and applies the authoritative state. lost: an
// assertion may have been sent, so a confirmed close is "lost".
func (h *approvals) closeChallenge(consumer string, st store.AttemptStart, lost bool) (int, approvalReply) {
	a := st.Attempt
	b, ok := h.update(consumer, a, []string{a.State}, a.State, store.AttemptUpdate{CloseRequestID: newRequestID("close-")})
	if !ok {
		return http.StatusServiceUnavailable, approvalReply{Error: "store_unavailable", Attempt: &a}
	}
	st.Attempt = b
	ans := h.signer.Close(b.ChallengeID, b.CloseRequestID)
	if ans.Kind != signerclient.KindOK {
		return http.StatusServiceUnavailable, approvalReply{Error: "result_unknown", Attempt: &b}
	}
	switch ans.Reply.State {
	case "used":
		return h.resolveUsed(consumer, st, ans.Reply.ApprovalID)
	case "closed", "expired":
		state := ans.Reply.State
		if state == "closed" && lost {
			state = "lost"
		}
		c, ok := h.update(consumer, b, openStates, state, store.AttemptUpdate{})
		return http.StatusOK, approvalReply{Attempt: &c, StateNotSaved: !ok}
	}
	return http.StatusBadGateway, approvalReply{Error: "signer_inconsistent", Attempt: &b}
}

// cancel is 「取消這次驗證」: the challenge is closed (or found never made);
// the session stays pending unless the Signer says the challenge was used.
func (h *approvals) cancel(consumer, session, requestID string) (int, approvalReply) {
	a, st, err := h.st.ApprovalAttemptOf(consumer, requestID)
	switch {
	case store.Code(err) == "OWNERSHIP_MISMATCH" || err == nil && a.SessionID != session:
		return http.StatusNotFound, approvalReply{Error: "not_found"}
	case err != nil:
		return http.StatusServiceUnavailable, approvalReply{Error: "store_unavailable"}
	case !a.Open():
		return http.StatusOK, approvalReply{Attempt: &a}
	}
	return h.closeAttempt(consumer, st)
}

// closeOnSession (review R3): the session closed (cancelled, expired,
// superseded) while an attempt was open. Its challenge is closed, never
// shown or verified; a challenge the Signer had already used still
// resolves (to the session's closed outcome, its approval abandoned).
func (h *approvals) closeOnSession(consumer string, st store.AttemptStart) (int, approvalReply) {
	status, out := h.closeAttempt(consumer, st)
	if status == http.StatusOK && out.Outcome == "" {
		status, out.Error = http.StatusConflict, "session_closed"
	}
	return status, out
}

// closeAttempt closes an open attempt's challenge (or finds it never made).
func (h *approvals) closeAttempt(consumer string, st store.AttemptStart) (int, approvalReply) {
	a := st.Attempt
	if a.ChallengeID == "" {
		ans := h.signer.ChallengeByRequest(a.RequestID)
		switch ans.Kind {
		case signerclient.KindNotFound: // never made: nothing to close
			b, ok := h.update(consumer, a, []string{"requested", "unknown"}, "closed", store.AttemptUpdate{})
			return http.StatusOK, approvalReply{Attempt: &b, StateNotSaved: !ok}
		case signerclient.KindOK:
			b, ok := h.update(consumer, a, []string{a.State}, a.State, store.AttemptUpdate{ChallengeID: ans.Reply.ChallengeID})
			if !ok {
				return http.StatusServiceUnavailable, approvalReply{Error: "store_unavailable", Attempt: &a}
			}
			st.Attempt = b
		default:
			return http.StatusServiceUnavailable, approvalReply{Error: "result_unknown", Attempt: &a}
		}
	}
	return h.closeChallenge(consumer, st, st.Attempt.VerifyRequestID != "" && (st.Attempt.State == "verifying" || st.Attempt.State == "unknown"))
}

// status is GET .../attempts/{request_id}: the recovery of an attempt by
// its own IDs only; it never makes a new challenge for a known one or
// sends an assertion.
func (h *approvals) status(consumer, session, requestID string) (int, approvalReply) {
	a, st, err := h.st.ApprovalAttemptOf(consumer, requestID)
	switch {
	case store.Code(err) == "OWNERSHIP_MISMATCH" || err == nil && a.SessionID != session:
		return http.StatusNotFound, approvalReply{Error: "not_found"}
	case err != nil:
		return http.StatusServiceUnavailable, approvalReply{Error: "store_unavailable"}
	}
	return h.drive(consumer, st, false)
}

// enrollPage is /approve/enroll (E1): the person's side of an enrollment
// the administrator began at the Signer's terminal. Trusted name only.
func (a *api) enrollPage(w http.ResponseWriter, r *http.Request) {
	if r.Host != a.alias {
		fail(w, http.StatusForbidden, "forbidden_host")
		return
	}
	body, err := web.FS.ReadFile("enroll.html")
	if err != nil {
		fail(w, http.StatusInternalServerError, "page_unavailable")
		return
	}
	pageHeaders(w.Header(), "text/html; charset=utf-8")
	w.Write(body)
}

// enrollRelay relays the enrollment page's two requests to the Signer as
// they are (the code travels in the body). No token: the Signer's one-time
// code is the check (decision D2 accepts that the Host is trusted here).
func (a *api) enrollRelay(w http.ResponseWriter, r *http.Request) {
	if r.Host != a.alias {
		fail(w, http.StatusForbidden, "forbidden_host")
		return
	}
	if r.Header.Get("Origin") != "http://"+a.alias {
		fail(w, http.StatusForbidden, "forbidden_origin")
		return
	}
	var req struct {
		Code       string          `json:"code"`
		Credential json.RawMessage `json:"credential,omitempty"`
	}
	options := strings.HasSuffix(r.URL.Path, "/options")
	if !strictJSONLimit(w, r, &req, 48<<10) {
		return
	}
	if !requestIDFormat.MatchString(r.PathValue("id")) || len(req.Code) == 0 || len(req.Code) > 16 || options == (len(req.Credential) > 0) {
		fail(w, http.StatusBadRequest, "bad_request")
		return
	}
	body := map[string]any{"code": req.Code}
	if !options {
		body["credential"] = req.Credential
	}
	status, out := a.approve.signer.EnrollRelay(r.PathValue("id"), body)
	if status == 0 {
		fail(w, http.StatusServiceUnavailable, "signer_unavailable")
		return
	}
	fmt.Fprintf(a.out, "demo: enrollment relay %d\n", status)
	h := w.Header()
	h.Set("Content-Type", "application/json")
	h.Set("Cache-Control", "no-store")
	h.Set("X-Content-Type-Options", "nosniff")
	w.WriteHeader(status)
	w.Write(out)
}

// approvalsCapability: a person's approval pays with --payments (P2-a);
// otherwise it is not a payment.
func (a *api) approvalsCapability() string {
	if a.payments {
		return LocalAnvil
	}
	return paycontract.PaymentNotEnabled
}
