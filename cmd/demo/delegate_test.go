package main

// Gap 4 (docs/commercial-gap4.md 2.3): the person confirms a delegation
// with their device through the Host, as an Ask is approved; a software
// authenticator stands in (not a device result).

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/SIMPLYBOYS/agent-payment-authority/internal/store"
)

func TestDelegationByDevice(t *testing.T) {
	f := startA2(t, 120)
	viewer := f.h.tokens["viewer:consumer-a"]
	body := map[string]string{"request_id": "dreq-000000000001", "scenario": "pay-a"}
	code, c := f.post("/api/delegations/challenge", viewer, body)
	ch, _ := c["challenge"].(map[string]any)
	terms, _ := c["terms"].(map[string]any)
	if code != 200 || ch == nil || ch["consumer_id"] != "consumer-a" || ch["decision"] != "approve" || terms["wallet_id"] != "wallet-a" ||
		terms["asset_contract"] != "0x4444444444444444444444444444444444444444" || ch["approval_session_id"] != terms["delegation_id"] {
		t.Fatalf("challenge %d %v", code, c)
	}
	// Only the consumer's own viewer: another consumer's scenario, the host token and the operator are refused.
	if code, r := f.post("/api/delegations/challenge", f.h.tokens["viewer:consumer-b"], body); code != 403 || r["error"] != "forbidden_consumer" {
		t.Fatalf("B on A's scenario: %d %v", code, r)
	}
	for _, role := range []string{"host", "operator"} {
		if code, r := f.post("/api/delegations/challenge", f.h.tokens[role], body); code != 403 || r["error"] != "forbidden_role" {
			t.Fatalf("%s: %d %v", role, code, r)
		}
	}
	opts, _ := json.Marshal(ch["options"])
	as := json.RawMessage(f.auth.Get(opts, f.origin))
	assertion := map[string]any{"request_id": "dreq-000000000001", "scenario": "pay-a", "challenge_id": ch["challenge_id"], "verify_request_id": "dver-000000000001", "assertion": as}
	code, d := f.post("/api/delegations/assertion", viewer, assertion)
	dl, _ := d["delegation"].(map[string]any)
	if code != 201 || dl["confirm_method"] != "webauthn" || dl["confirmed_by"] != "webauthn:approver-a" || !strings.HasPrefix(dl["signer_confirmation_id"].(string), "hap-") ||
		!strings.HasPrefix(dl["credential_ref"].(string), "sha256:") || dl["delegation_id"] != terms["delegation_id"] {
		t.Fatalf("assertion %d %v", code, d)
	}
	// The same verify request again: the Signer's kept answer, the Store's same record.
	if code, again := f.post("/api/delegations/assertion", viewer, assertion); code != 200 || again["delegation"].(map[string]any)["signer_confirmation_id"] != dl["signer_confirmation_id"] {
		t.Fatalf("resend %d %v", code, again)
	}
	// A run under it binds it, and DELEGATION_BOUND says the person's device confirmed it (a virtual one here).
	code, r := f.h.post("/api/host/runs", f.h.tokens["operator"], `{"request_id":"run-req-0000000001","scenario":"pay-a","delegation_id":"`+dl["delegation_id"].(string)+`"}`)
	if code != 202 {
		t.Fatalf("start %d %v", code, r)
	}
	run := f.h.waitRun(r["run_id"].(string), ended)
	_, raw, _ := f.h.get("/api/tasks/"+run["task_id"].(string)+"/events", viewer)
	if !strings.Contains(string(raw), `"event_type":"DELEGATION_BOUND"`) || !strings.Contains(string(raw), `"authorization_method":"webauthn","version":"apa.pay.v1","evidence_mode":"fixture","source":"virtual_authenticator"`) {
		t.Fatalf("DELEGATION_BOUND without the person's confirmation: %s", raw)
	}
	// The person takes it back with the same device (section 7): only their own, once.
	id := dl["delegation_id"].(string)
	rbody := map[string]string{"request_id": "rreq-000000000001", "delegation_id": id}
	if code, r := f.post("/api/delegations/revoke/challenge", f.h.tokens["viewer:consumer-b"], map[string]string{"request_id": "rreq-000000000002", "delegation_id": id}); code != 409 || r["code"] != "CONSUMER_MISMATCH" {
		t.Fatalf("B revoking A's: %d %v", code, r)
	}
	code, c = f.post("/api/delegations/revoke/challenge", viewer, rbody)
	ch, _ = c["challenge"].(map[string]any)
	if code != 200 || ch == nil || ch["decision"] != "deny" || ch["approval_session_id"] != id || c["terms"] == nil {
		t.Fatalf("revoke challenge %d %v", code, c)
	}
	opts, _ = json.Marshal(ch["options"])
	rv := map[string]any{"delegation_id": id, "challenge_id": ch["challenge_id"], "verify_request_id": "rver-000000000001", "assertion": json.RawMessage(f.auth.Get(opts, f.origin))}
	code, rr := f.post("/api/delegations/revoke/assertion", viewer, rv)
	rk, _ := rr["revoked"].(map[string]any)
	if code != 200 || rk == nil || rk["delegation_id"] != id || !strings.HasPrefix(rk["revocation_id"].(string), "hap-") || rr["stopped_run"] != "" ||
		rr["task_id"] != run["task_id"] || rr["store_error"] != nil {
		t.Fatalf("revoke %d %v", code, rr)
	}
	// The task's chain records it, by the person's (virtual) device; a resend adds nothing.
	revoked := `"event_type":"DELEGATION_REVOKED"`
	_, raw, _ = f.h.get("/api/tasks/"+run["task_id"].(string)+"/events", viewer)
	// (The view leaves the Signer's ID out, as for DELEGATION_BOUND; the contract requires it in the record.)
	if !strings.Contains(string(raw), revoked+`,"sequence":6`) || !strings.Contains(string(raw), `"delegation_id":"`+id+`","authorization_method":"webauthn","version":"apa.pay.v1","evidence_mode":"fixture","source":"virtual_authenticator"`) {
		t.Fatalf("no DELEGATION_REVOKED in the task: %s", raw)
	}
	if code, again := f.post("/api/delegations/revoke/assertion", viewer, rv); code != 200 || again["task_id"] != run["task_id"] {
		t.Fatalf("resend %d %v", code, again)
	}
	if _, raw, _ = f.h.get("/api/tasks/"+run["task_id"].(string)+"/events", viewer); strings.Count(string(raw), revoked) != 1 {
		t.Fatalf("recorded twice: %s", raw)
	}
	if code, r := f.post("/api/delegations/revoke/challenge", viewer, map[string]string{"request_id": "rreq-000000000003", "delegation_id": id}); code != 409 || r["code"] != "DELEGATION_REVOKED" {
		t.Fatalf("again: %d %v", code, r)
	}
}

// A revocation stops only the running run under that delegation, once.
func TestStopDelegation(t *testing.T) {
	stopped := 0
	lr := &liveRun{run: store.Run{RunID: "run-1", ConsumerID: "consumer-a", DelegationID: "dlg-1"}, state: "running", cancel: func() { stopped++ }}
	c := &control{active: map[string]*liveRun{"consumer-a": lr}}
	if id := c.stopDelegation("dlg-2"); id != "" || stopped != 0 {
		t.Fatalf("another delegation stopped %q", id)
	}
	if id := c.stopDelegation("dlg-1"); id != "run-1" || stopped != 1 || lr.state != "stopping" || lr.stopAt.IsZero() {
		t.Fatalf("stop %q %d %+v", id, stopped, lr)
	}
	if id := c.stopDelegation("dlg-1"); id != "" || stopped != 1 {
		t.Fatalf("a stopping run stopped again %q", id)
	}
}
