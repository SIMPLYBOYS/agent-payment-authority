package main

// The person's own confirmation of a delegation (gap 4;
// docs/commercial-gap4.md 2.3): on a host that pays, a customer confirms
// the host's terms for a pay-family scenario with their registered device,
// through the Signer, as an Ask is approved (A2): the page asks for a
// challenge, the device signs it, the Signer verifies and keeps the
// confirmation, and the Store records it (confirm_method webauthn) with the
// Signer's confirmation, which every payment of the task then names. Only
// the consumer's own viewer token; nothing in a body sets a term.

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strings"

	"github.com/SIMPLYBOYS/agent-payment-authority/internal/paycontract"
	"github.com/SIMPLYBOYS/agent-payment-authority/internal/signer"
	"github.com/SIMPLYBOYS/agent-payment-authority/internal/signerclient"
	"github.com/SIMPLYBOYS/agent-payment-authority/internal/store"
)

// delegationHold bounds a delegation challenge on the Host's side: the
// Signer's own TTL (challenge_ttl_seconds) is shorter still.
const delegationHold = 300

// delegationTerms are the host's terms for consumer's scenario, named as
// the Signer knows them (wallet, chain, asset contract); the delegation ID
// is the one the Store derives from the same request ID. "" or a fixed
// error code.
func (a *api) delegationTerms(consumer, scenarioID, requestID string) (*scenario, paycontract.DelegationTerms, string) {
	sc := a.control.scenarios[scenarioID]
	switch {
	case !requestIDFormat.MatchString(requestID):
		return nil, paycontract.DelegationTerms{}, "bad_request"
	case sc == nil || sc.Delegation == nil:
		return nil, paycontract.DelegationTerms{}, "unknown_scenario"
	case sc.ConsumerID != consumer:
		return nil, paycontract.DelegationTerms{}, "forbidden_consumer"
	}
	d, p := sc.Delegation, a.control.profile
	wallet, chain, asset := a.registryNames(consumer, p.AssetID)
	if asset == "" {
		return nil, paycontract.DelegationTerms{}, "delegation_unavailable" // the registry names no wallet or asset for it
	}
	sp, _ := paycontract.SpecOf(sc.version())
	return sc, paycontract.DelegationTerms{Version: sc.version(), DelegationID: sp.DelegationID(requestID), ConsumerID: consumer, WalletID: wallet,
		ChainID: chain, AssetContract: asset, Merchants: p.Merchants, Purpose: d.Purpose, TaskBudget: d.TaskBudget, AllowMax: d.AllowMax,
		ApprovalDeadlineSeconds: d.ApprovalDeadlineSeconds, ValidSeconds: d.ValidSeconds}, ""
}

// registryNames: the consumer's wallet, its chain and the asset's contract
// on it, as the trusted registry has them; "" where it has none.
func (a *api) registryNames(consumer, assetID string) (wallet, chain, asset string) {
	for _, c := range a.registry.Consumers {
		if c.ConsumerID == consumer {
			wallet = c.WalletID
		}
	}
	for _, w := range a.registry.Wallets {
		if w.WalletID == wallet && wallet != "" {
			chain = w.ChainID
		}
	}
	for _, x := range a.registry.Assets {
		if x.AssetID == assetID && x.ChainID == chain && chain != "" {
			asset = strings.ToLower(x.AssetContract)
		}
	}
	return wallet, chain, asset
}

func (a *api) registerDelegations(mux *http.ServeMux) {
	mux.HandleFunc("POST /api/delegations/challenge", a.delegationChallenge)
	mux.HandleFunc("POST /api/delegations/assertion", a.delegationAssertion)
	mux.HandleFunc("POST /api/delegations/revoke/challenge", a.revokeChallenge)
	mux.HandleFunc("POST /api/delegations/revoke/assertion", a.revokeAssertion)
}

// revokeChallenge is POST /api/delegations/revoke/challenge {request_id,
// delegation_id} (docs/commercial-gap4.md 7): the Signer's challenge to
// take back one of the consumer's own delegations; the Signer refuses
// another consumer's. Asked once, as a confirmation's is.
func (a *api) revokeChallenge(w http.ResponseWriter, r *http.Request) {
	var req struct {
		RequestID    string `json:"request_id"`
		DelegationID string `json:"delegation_id"`
	}
	g, ok := a.approvalGuard(w, r, true, true, 1024, &req)
	if !ok {
		return
	}
	if !requestIDFormat.MatchString(req.RequestID) || !paycontract.ValidID(req.DelegationID) {
		fail(w, http.StatusBadRequest, "bad_request")
		return
	}
	ans := a.approve.signer.DelegationRevokeChallenge(signer.DelegationRevokeRequest{RequestID: req.RequestID, DelegationID: req.DelegationID,
		ConsumerID: g.ConsumerID, HostDeadline: a.clock() + delegationHold})
	a.delegationAnswer(w, "revoke-challenge", ans, func(rep signer.ApprovalReply) any {
		return map[string]any{"challenge": challengeView{ChallengeID: rep.ChallengeID, ConsumerID: rep.ConsumerID, ApprovalSessionID: rep.ApprovalSessionID,
			IntentHash: rep.IntentHash, Decision: rep.Decision, ApproverID: rep.ApproverID, ExpiresAt: rep.ExpiresAt, Options: rep.Options}, "terms": rep.Terms}
	})
}

// revokeAssertion is POST /api/delegations/revoke/assertion {delegation_id,
// challenge_id, verify_request_id, assertion}: the Signer verifies and
// keeps the revocation, the Store records it in the delegation's task, then
// a run still going under it is stopped as 停止 Agent stops it. A resend
// answers what the Signer kept and writes nothing new.
func (a *api) revokeAssertion(w http.ResponseWriter, r *http.Request) {
	var req struct {
		DelegationID    string          `json:"delegation_id"`
		ChallengeID     string          `json:"challenge_id"`
		VerifyRequestID string          `json:"verify_request_id"`
		Assertion       json.RawMessage `json:"assertion"`
	}
	g, ok := a.approvalGuard(w, r, true, true, 64<<10, &req)
	if !ok {
		return
	}
	ans := a.approve.signer.DelegationRevokeVerify(signer.DelegationRevokeVerifyRequest{VerifyRequestID: req.VerifyRequestID, ChallengeID: req.ChallengeID,
		DelegationID: req.DelegationID, Assertion: req.Assertion})
	if ans.Kind != signerclient.KindOK || ans.Reply.State != "verified" || ans.Reply.Decision != "deny" || ans.Reply.ApprovalSessionID != req.DelegationID {
		a.delegationAnswer(w, "revoke-assertion", ans, nil)
		return
	}
	// The task's own record first (its DELEGATION_REVOKED, for the checker),
	// then the stop, whose TASK_CANCELLED follows it. The Signer already
	// refuses either way: a Store not written is said, not retried here
	// (the same request again writes it).
	task, err := a.st.RevokeDelegation(req.DelegationID, ans.Reply.ApprovalID)
	stopped := ""
	if a.control != nil {
		stopped = a.control.stopDelegation(req.DelegationID)
	}
	fmt.Fprintf(a.out, "demo: delegation revoked by the person's device: %s %s task=%q store=%s stopped_run=%q\n", g.ConsumerID, req.DelegationID, task, store.CodeOf(err), stopped)
	out := map[string]any{"revoked": map[string]any{"delegation_id": req.DelegationID, "revocation_id": ans.Reply.ApprovalID,
		"revoked_at": ans.Reply.VerifiedAt}, "stopped_run": stopped, "task_id": task}
	if err != nil {
		out["store_error"] = store.CodeOf(err)
	}
	reply(w, http.StatusOK, out)
}

func termsRefused(w http.ResponseWriter, code string) {
	status := http.StatusBadRequest
	if code == "forbidden_consumer" {
		status = http.StatusForbidden
	} else if code == "delegation_unavailable" {
		status = http.StatusConflict
	}
	fail(w, status, code)
}

// delegationChallenge is POST /api/delegations/challenge {request_id,
// scenario}: the Signer's challenge over the terms. The Host's deadline is
// part of the request, so a resend later is a conflict: the page asks once
// per confirmation (review).
func (a *api) delegationChallenge(w http.ResponseWriter, r *http.Request) {
	var req struct {
		RequestID string `json:"request_id"`
		Scenario  string `json:"scenario"`
	}
	g, ok := a.approvalGuard(w, r, true, true, 1024, &req)
	if !ok {
		return
	}
	_, terms, bad := a.delegationTerms(g.ConsumerID, req.Scenario, req.RequestID)
	if bad != "" {
		termsRefused(w, bad)
		return
	}
	raw, _ := json.Marshal(terms)
	ans := a.approve.signer.DelegationChallenge(signer.DelegationChallengeRequest{RequestID: req.RequestID, Terms: raw, HostDeadline: a.clock() + delegationHold})
	a.delegationAnswer(w, "challenge", ans, func(rep signer.ApprovalReply) any {
		return map[string]any{"challenge": challengeView{ChallengeID: rep.ChallengeID, ConsumerID: rep.ConsumerID, ApprovalSessionID: rep.ApprovalSessionID,
			IntentHash: rep.IntentHash, Decision: rep.Decision, ApproverID: rep.ApproverID, ExpiresAt: rep.ExpiresAt, Options: rep.Options}, "terms": terms}
	})
}

// delegationAssertion is POST /api/delegations/assertion {request_id (the
// challenge's), scenario, challenge_id, verify_request_id, assertion}: the
// Signer verifies, then the Store records the confirmation. A resend of
// the same verify request answers what the Signer kept, and the Store's
// record is written again the same: a write lost after the Signer's
// verification is recovered by the same request.
func (a *api) delegationAssertion(w http.ResponseWriter, r *http.Request) {
	var req struct {
		RequestID       string          `json:"request_id"`
		Scenario        string          `json:"scenario"`
		ChallengeID     string          `json:"challenge_id"`
		VerifyRequestID string          `json:"verify_request_id"`
		Assertion       json.RawMessage `json:"assertion"`
	}
	g, ok := a.approvalGuard(w, r, true, true, 64<<10, &req)
	if !ok {
		return
	}
	sc, terms, bad := a.delegationTerms(g.ConsumerID, req.Scenario, req.RequestID)
	if bad != "" {
		termsRefused(w, bad)
		return
	}
	raw, _ := json.Marshal(terms)
	ans := a.approve.signer.DelegationVerify(signer.DelegationVerifyRequest{VerifyRequestID: req.VerifyRequestID, ChallengeID: req.ChallengeID, Terms: raw, Assertion: req.Assertion})
	if ans.Kind != signerclient.KindOK || ans.Reply.State != "verified" || ans.Reply.ApprovalSessionID != terms.DelegationID {
		a.delegationAnswer(w, "assertion", ans, nil)
		return
	}
	d, p := sc.Delegation, a.control.profile
	view, replay, err := a.st.ConfirmDelegation(store.DelegationRequest{ContractVersion: sc.version(), RequestID: req.RequestID, ConsumerID: g.ConsumerID, Scenario: sc.ID,
		Purpose: d.Purpose, AssetID: p.AssetID, Merchants: p.Merchants, TaskBudget: d.TaskBudget, AllowMax: d.AllowMax, ApprovalDeadlineSeconds: d.ApprovalDeadlineSeconds,
		ValidSeconds: d.ValidSeconds, ConfirmedBy: "webauthn:" + ans.Reply.ApproverID, ConfirmMethod: "webauthn",
		SignerConfirmationID: ans.Reply.ApprovalID, CredentialRef: ans.Reply.CredentialRef})
	fmt.Fprintf(a.out, "demo: delegation confirmed by the person's device: %s %s %s\n", g.ConsumerID, terms.DelegationID, store.CodeOf(err))
	switch code := store.Code(err); {
	case code == "IDEMPOTENCY_CONFLICT":
		fail(w, http.StatusConflict, "request_id_conflict")
	case code == "INVALID_DELEGATION":
		fail(w, http.StatusBadRequest, "invalid_delegation")
	case err != nil:
		reply(w, http.StatusServiceUnavailable, map[string]string{"error": "store_unavailable", "code": store.CodeOf(err)}) // the Signer kept it: the same request again records it
	default:
		status := http.StatusCreated
		if replay {
			status = http.StatusOK
		}
		reply(w, status, map[string]any{"delegation": view, "replay": replay})
	}
}

// delegationAnswer relays the Signer's answer: its fixed code on a refusal;
// signer_unknown when no answer could be read (the same request again asks
// for the same thing).
func (a *api) delegationAnswer(w http.ResponseWriter, op string, ans signerclient.Answer, ok func(signer.ApprovalReply) any) {
	fmt.Fprintf(a.out, "demo: delegation %s %s %s\n", op, ans.Kind, ans.Code)
	switch {
	case ans.Kind == signerclient.KindOK && ok != nil:
		reply(w, http.StatusOK, ok(ans.Reply))
	case ans.Kind == signerclient.KindRefused, ans.Kind == signerclient.KindNotFound:
		reply(w, http.StatusConflict, map[string]string{"error": "signer_refused", "code": ans.Code})
	default:
		reply(w, http.StatusServiceUnavailable, map[string]string{"error": "signer_unknown"})
	}
}
