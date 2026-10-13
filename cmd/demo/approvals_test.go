package main

// A2 (docs/commercial-human-approval-design.md 5-9, layer I): the real Host
// and Store with the real serve-approvals handler on a test Unix socket,
// a fake Agent's Ask and a software authenticator (signertest). Nothing
// here is a device result or pays.

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/SIMPLYBOYS/agent-payment-authority/internal/signer"
	"github.com/SIMPLYBOYS/agent-payment-authority/internal/signer/signertest"
	"github.com/SIMPLYBOYS/agent-payment-authority/internal/signerclient"
	"github.com/SIMPLYBOYS/agent-payment-authority/internal/store"
)

type a2fx struct {
	relay               *approvals
	t                   *testing.T
	h                   *host
	alias, origin, sock string
	sdir                string
	core                *signer.ApprovalCore
	auth                *signertest.Authenticator
	stopSigner          func()
	failResolve         *failStore
}

// failStore makes the relay's next ResolveWebAuthn fail once, as a lost
// write would.
type failStore struct {
	approvalStore
	mu   sync.Mutex
	fail bool
}

func (f *failStore) ResolveWebAuthn(r store.WebAuthnResult) (store.ApprovalResult, error) {
	f.mu.Lock()
	fail := f.fail
	f.fail = false
	f.mu.Unlock()
	if fail {
		return store.ApprovalResult{}, errors.New("injected write failure")
	}
	return f.approvalStore.ResolveWebAuthn(r)
}

func startA2(t *testing.T, deadline int) *a2fx {
	t.Helper()
	sdir, err := os.MkdirTemp("", "a2s")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.RemoveAll(sdir) })
	f := &a2fx{t: t, sdir: sdir, sock: filepath.Join(sdir, "s.sock"), failResolve: &failStore{}}
	f.h = controlHost(t, hostOpts{expiryEvery: time.Hour, scenarios: payCatalog(t, deadline),
		args:        []string{"--approvals-signer", f.sock, "--authenticator-source", "virtual"},
		onApprovals: func(h *approvals) { f.relay = h; f.failResolve.approvalStore = h.st; h.st = f.failResolve }})
	t.Cleanup(func() { f.h.stop() })
	_, port, _ := net.SplitHostPort(f.h.addr)
	f.alias, f.origin = "localhost:"+port, "http://localhost:"+port
	f.startSigner()
	t.Cleanup(func() { f.stopSigner() })
	// Enrollment E1: begun by the administrator, made through the Host's
	// relay, confirmed by the administrator.
	id, code, _, err := f.core.EnrollBegin(context.Background(), "approver-a")
	if err != nil {
		t.Fatal(err)
	}
	f.auth = signertest.NewAuthenticator()
	st, o := f.post("/api/enrollments/"+id+"/options", "", map[string]any{"code": code})
	if st != 200 {
		t.Fatalf("enrollment options: %d %v", st, o)
	}
	opts, _ := json.Marshal(o["options"])
	st, fin := f.post("/api/enrollments/"+id, "", map[string]any{"code": code, "credential": json.RawMessage(f.auth.Create(opts, f.origin))})
	if st != 200 || fin["fingerprint"] != signer.Fingerprint(f.auth.ID) {
		t.Fatalf("enrollment: %d %v", st, fin)
	}
	if err := f.core.EnrollConfirm(context.Background(), id); err != nil {
		t.Fatal(err)
	}
	return f
}

func (f *a2fx) startSigner() {
	f.t.Helper()
	cfg, err := signer.ParseConfig(signertest.ApprovalsConfig(f.sock, uint32(os.Getuid()), uint64(time.Now().Unix()),
		"0x1111111111111111111111111111111111111111", "0x2222222222222222222222222222222222222222", f.origin))
	if err != nil {
		f.t.Fatal(err)
	}
	core, err := signer.NewApprovalCore(context.Background(), cfg, filepath.Join(f.sdir, "approvals.db"), func() uint64 { return uint64(time.Now().Unix()) })
	if err != nil {
		f.t.Fatal(err)
	}
	ln, err := net.Listen("unix", f.sock)
	if err != nil {
		f.t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() { signer.ServeApprovals(ctx, ln, core, func(string) {}); close(done) }()
	f.core = core
	f.stopSigner = func() { cancel(); <-done; core.Close(); f.stopSigner = func() {} }
}

// post and get are the page's requests on the trusted name.
func (f *a2fx) post(path, token string, body any, mod ...func(*http.Request)) (int, map[string]any) {
	f.t.Helper()
	raw, _ := json.Marshal(body)
	return f.h.post(path, token, string(raw), append([]func(*http.Request){func(r *http.Request) {
		r.Host = f.alias
		r.Header.Set("Origin", f.origin)
	}}, mod...)...)
}

func (f *a2fx) get(path, token string, mod ...func(*http.Request)) (int, map[string]any) {
	f.t.Helper()
	code, raw, _ := f.h.get(path, token, append([]func(*http.Request){func(r *http.Request) { r.Host = f.alias }}, mod...)...)
	var v map[string]any
	json.Unmarshal(raw, &v)
	return code, v
}

// ask runs consumer A's fake Agent to an Ask (the delegation confirmed by
// the simulated customer, as in P1-a) and returns its session and intent.
func (f *a2fx) ask(n string) (string, string, string) {
	f.t.Helper()
	sim := f.h.simulate("consumer-a")
	run := f.h.delegateAndRun(sim, "pay-a", n)
	code, list := f.get("/api/approvals", f.h.tokens["viewer:consumer-a"])
	if code != 200 || list["sources"].(map[string]any)["authenticator"] != "virtual" || list["signer"] != "approvals-only" {
		f.t.Fatalf("list: %d %v", code, list)
	}
	for _, x := range list["approvals"].([]any) {
		it := x.(map[string]any)
		p := it["purchase"].(map[string]any)
		if ap := p["approval"].(map[string]any); ap["state"] == "pending" && it["task_id"] == run["task_id"] {
			return ap["approval_session_id"].(string), p["intent_hash"].(string), it["task_id"].(string)
		}
	}
	f.t.Fatalf("no pending session: %v", list)
	return "", "", ""
}

func (f *a2fx) challenge(session, req, decision string) (int, map[string]any) {
	return f.post("/api/approvals/"+session+"/challenge", f.h.tokens["viewer:consumer-a"], map[string]string{"request_id": req, "decision": decision})
}

// sign answers the challenge of a 200 reply as the page would, after
// checking the challenge is this operation's.
func (f *a2fx) sign(r map[string]any, session, intent, decision string) json.RawMessage {
	f.t.Helper()
	c, _ := r["challenge"].(map[string]any)
	if c == nil || c["approval_session_id"] != session || c["intent_hash"] != intent || c["decision"] != decision || c["consumer_id"] != "consumer-a" {
		f.t.Fatalf("challenge not this operation's: %v", r)
	}
	opts, _ := json.Marshal(c["options"])
	return f.auth.Get(opts, f.origin)
}

func (f *a2fx) assert(session, req, verify string, as json.RawMessage) (int, map[string]any) {
	return f.post("/api/approvals/"+session+"/assertion", f.h.tokens["viewer:consumer-a"],
		map[string]any{"request_id": req, "verify_request_id": verify, "assertion": as})
}

func attempt(r map[string]any) map[string]any { a, _ := r["attempt"].(map[string]any); return a }

func sessionState(r map[string]any) string {
	it, _ := r["item"].(map[string]any)
	if it == nil {
		return ""
	}
	return it["purchase"].(map[string]any)["approval"].(map[string]any)["state"].(string)
}

func TestA2Approve(t *testing.T) {
	f := startA2(t, 60)
	sid, intent, task := f.ask("01")
	code, r := f.challenge(sid, "att-req-0000000000001", "approve")
	if code != 200 || attempt(r)["state"] != "challenged" || r["kind"] != "new" {
		t.Fatalf("challenge: %d %v", code, r)
	}
	d := r["challenge"].(map[string]any)["display"].(map[string]any)
	if d["consumer_id"] != "consumer-a" || d["wallet_id"] != "wallet-a" || d["amount"] == "" || d["pay_to"] == "" {
		t.Fatalf("display: %v", d)
	}
	as := f.sign(r, sid, intent, "approve")
	code, r = f.assert(sid, "att-req-0000000000001", "ver-req-0000000000001", as)
	if code != 200 || r["outcome"] != "approved" || attempt(r)["state"] != "verified" || sessionState(r) != "approved" {
		t.Fatalf("assertion: %d %v", code, r)
	}
	if h := f.h.grants(); h != 1 {
		t.Fatalf("grants %d", h)
	}
	ev := f.h.events(task).Events
	last := ev[len(ev)-2:]
	if last[0].EventType != "APPROVED" || last[0].AuthorizationMethod != "webauthn" || last[1].EventType != "GRANT_ISSUED" || last[1].AuthorizationMethod != "webauthn" {
		t.Fatalf("events %+v", last)
	}
	// A resend of the same verify request answers the same, without a second decision.
	if code, again := f.assert(sid, "att-req-0000000000001", "ver-req-0000000000001", as); code != 200 || sessionState(again) != "approved" || f.h.grants() != 1 {
		t.Fatalf("resend: %d %v", code, again)
	}
	// The Host keeps no assertion: its signature is nowhere in the database.
	var sig struct {
		Response struct{ Signature string } `json:"response"`
	}
	json.Unmarshal(as, &sig)
	for _, name := range []string{"demo.db", "demo.db-wal"} {
		if raw, err := os.ReadFile(filepath.Join(f.h.dir, name)); err == nil && bytes.Contains(raw, []byte(sig.Response.Signature)) {
			t.Fatalf("%s holds the assertion", name)
		}
	}
	if strings.Contains(f.h.out.String(), sig.Response.Signature) {
		t.Fatal("the log holds the assertion")
	}
	// After a restart the stored result is the same and passes recovery.
	f.h.stop()
	f.h = controlHost(t, hostOpts{dir: f.h.dir, expiryEvery: time.Hour, scenarios: payCatalog(t, 60), args: []string{"--approvals-signer", f.sock}})
	if v := f.h.task(task); v.Purchases[0].Approval.State != "approved" || v.Purchases[0].Approval.Grant.AuthorizationMethod != "webauthn" {
		t.Fatalf("after restart %+v", v.Purchases[0].Approval)
	}
}

func TestA2Deny(t *testing.T) {
	f := startA2(t, 60)
	sid, intent, _ := f.ask("01")
	_, r := f.challenge(sid, "att-req-0000000000001", "deny")
	code, r := f.assert(sid, "att-req-0000000000001", "ver-req-0000000000001", f.sign(r, sid, intent, "deny"))
	if code != 200 || r["outcome"] != "denied" || sessionState(r) != "denied" || f.h.grants() != 0 {
		t.Fatalf("deny: %d %v", code, r)
	}
}

func TestA2Permissions(t *testing.T) {
	f := startA2(t, 60)
	sid, intent, _ := f.ask("01")
	body := map[string]string{"request_id": "att-req-0000000000009", "decision": "approve"}
	path := "/api/approvals/" + sid + "/challenge"
	sim := f.h.simulate("consumer-a")
	for name, c := range map[string]struct {
		token string
		mod   func(*http.Request)
		want  int
	}{
		"no token":               {"", nil, 401},
		"the simulated approver": {sim, nil, 401},
		"host":                   {f.h.tokens["host"], nil, 403},
		"operator":               {f.h.tokens["operator"], nil, 403},
		"B's viewer":             {f.h.tokens["viewer:consumer-b"], nil, 404},
		"127.0.0.1":              {f.h.tokens["viewer:consumer-a"], func(r *http.Request) { r.Host = f.h.addr; r.Header.Set("Origin", "http://"+f.h.addr) }, 403},
		"other origin":           {f.h.tokens["viewer:consumer-a"], func(r *http.Request) { r.Header.Set("Origin", "http://evil.example") }, 403},
		"null origin":            {f.h.tokens["viewer:consumer-a"], func(r *http.Request) { r.Header.Set("Origin", "null") }, 403},
	} {
		mods := []func(*http.Request){}
		if c.mod != nil {
			mods = append(mods, c.mod)
		}
		if code, _ := f.post(path, c.token, body, mods...); code != c.want {
			t.Errorf("%s: %d, want %d", name, code, c.want)
		}
	}
	if code, _ := f.get("/api/approvals/"+sid, f.h.tokens["viewer:consumer-b"]); code != 404 {
		t.Errorf("B reads A's session: %d", code)
	}
	// The right viewer with no or a forged assertion: the Signer refuses; nothing changes.
	_, r := f.challenge(sid, "att-req-0000000000001", "approve")
	forged := signertest.NewAuthenticator()
	forged.ID = f.auth.ID
	opts, _ := json.Marshal(r["challenge"].(map[string]any)["options"])
	code, r := f.assert(sid, "att-req-0000000000001", "ver-req-0000000000001", forged.Get(opts, f.origin))
	if code != 409 || r["code"] != "ASSERTION_INVALID_SIGNATURE" || sessionState(r) != "pending" || f.h.grants() != 0 {
		t.Fatalf("forged: %d %v", code, r)
	}
	if code, r := f.assert(sid, "att-req-0000000000001", "ver-req-0000000000002", json.RawMessage(`{"id":"x"}`)); code != 409 || sessionState(r) != "pending" {
		t.Fatalf("malformed: %d %v", code, r)
	}
	// The host's own GET of a session is a read; it changes nothing.
	if code, v := f.get("/api/approvals/"+sid+"?consumer=consumer-a", f.h.tokens["host"]); code != 200 || v["item"] == nil {
		t.Fatalf("host read: %d %v", code, v)
	}
	_ = intent
}

func TestA2TwoPagesAndChangingTheDecision(t *testing.T) {
	f := startA2(t, 60)
	sid, intent, _ := f.ask("01")
	_, first := f.challenge(sid, "att-req-0000000000001", "approve")
	// Page 2, same decision: the same attempt and challenge.
	code, second := f.challenge(sid, "att-req-0000000000002", "approve")
	if code != 200 || second["kind"] != "shared" || attempt(second)["request_id"] != "att-req-0000000000001" ||
		second["challenge"].(map[string]any)["challenge_id"] != first["challenge"].(map[string]any)["challenge_id"] {
		t.Fatalf("shared: %d %v", code, second)
	}
	// Page 2, the other decision: a conflict and no challenge of either kind.
	code, other := f.challenge(sid, "att-req-0000000000003", "deny")
	if code != 409 || other["error"] != "decision_conflict" || other["challenge"] != nil || attempt(other)["decision"] != "approve" {
		t.Fatalf("other decision: %d %v", code, other)
	}
	// 取消這次驗證: the Signer closes the challenge; then deny is a new attempt.
	old := f.sign(first, sid, intent, "approve")
	code, c := f.post("/api/approvals/"+sid+"/attempts/att-req-0000000000001/cancel", f.h.tokens["viewer:consumer-a"], map[string]any{})
	if code != 200 || attempt(c)["state"] != "closed" || sessionState(c) != "pending" {
		t.Fatalf("cancel: %d %v", code, c)
	}
	// The old approve assertion arrives late: the Signer refuses it (closed).
	chID := first["challenge"].(map[string]any)["challenge_id"].(string)
	order, _ := f.h.st.ApprovalOf("consumer-a", sid)
	_ = order
	if _, err := f.core.Verify(context.Background(), signer.VerifyRequest{VerifyRequestID: "ver-late-00000001", ChallengeID: chID, Order: orderOf(t, f, "att-req-0000000000001"), Assertion: old}); err == nil || err.Error() != "CHALLENGE_CLOSED" {
		t.Fatalf("late verify: %v", err)
	}
	_, d := f.challenge(sid, "att-req-0000000000004", "deny")
	code, r := f.assert(sid, "att-req-0000000000004", "ver-req-0000000000004", f.sign(d, sid, intent, "deny"))
	if code != 200 || r["outcome"] != "denied" || f.h.grants() != 0 {
		t.Fatalf("deny after cancel: %d %v", code, r)
	}
}

func orderOf(t *testing.T, f *a2fx, req string) json.RawMessage {
	_, st, err := f.h.st.ApprovalAttemptOf("consumer-a", req)
	if err != nil {
		t.Fatal(err)
	}
	return st.Order
}

func (f *a2fx) status(sid, req string) (int, map[string]any) {
	return f.get("/api/approvals/"+sid+"/attempts/"+req, f.h.tokens["viewer:consumer-a"])
}

// Breakpoint 1: the Signer made the challenge, its answer was lost.
func TestA2ChallengeAnswerLost(t *testing.T) {
	f := startA2(t, 60)
	sid, _, _ := f.ask("01")
	st, err := f.h.st.BeginApprovalAttempt(store.AttemptRequest{ConsumerID: "consumer-a", SessionID: sid, RequestID: "att-req-0000000000001", Decision: "approve"})
	if err != nil {
		t.Fatal(err)
	}
	made, err := f.core.Challenge(context.Background(), signer.ChallengeRequest{RequestID: "att-req-0000000000001", ApprovalSessionID: sid, Decision: "approve", Order: st.Order, HostDeadline: st.Deadline})
	if err != nil {
		t.Fatal(err)
	}
	code, r := f.status(sid, "att-req-0000000000001")
	if code != 200 || r["challenge"].(map[string]any)["challenge_id"] != made.ChallengeID || attempt(r)["challenge_id"] != made.ChallengeID {
		t.Fatalf("recovered: %d %v", code, r)
	}
}

// Breakpoint 2: verifying saved; the assertion did or did not reach the Signer.
func TestA2VerifyAnswerLost(t *testing.T) {
	for _, delivered := range []bool{false, true} {
		f := startA2(t, 60)
		sid, intent, _ := f.ask("01")
		_, r := f.challenge(sid, "att-req-0000000000001", "approve")
		as := f.sign(r, sid, intent, "approve")
		chID := r["challenge"].(map[string]any)["challenge_id"].(string)
		if _, err := f.h.st.UpdateApprovalAttempt(store.AttemptUpdate{ConsumerID: "consumer-a", RequestID: "att-req-0000000000001", From: []string{"challenged"},
			State: "verifying", VerifyRequestID: "ver-req-0000000000001"}); err != nil {
			t.Fatal(err)
		}
		if delivered {
			if _, err := f.core.Verify(context.Background(), signer.VerifyRequest{VerifyRequestID: "ver-req-0000000000001", ChallengeID: chID,
				Order: orderOf(t, f, "att-req-0000000000001"), Assertion: as}); err != nil {
				t.Fatal(err)
			}
		}
		code, s := f.status(sid, "att-req-0000000000001")
		want := map[bool]string{false: "lost", true: "verified"}[delivered]
		if code != 200 || attempt(s)["state"] != want {
			t.Fatalf("delivered %v: %d %v", delivered, code, s)
		}
		if !delivered {
			// Lost: the challenge is closed; the same assertion is refused now; the session waits.
			if _, err := f.core.Verify(context.Background(), signer.VerifyRequest{VerifyRequestID: "ver-req-0000000000009", ChallengeID: chID,
				Order: orderOf(t, f, "att-req-0000000000001"), Assertion: as}); err == nil || err.Error() != "CHALLENGE_CLOSED" {
				t.Fatalf("after lost: %v", err)
			}
			if sessionState(s) != "pending" || f.h.grants() != 0 {
				t.Fatalf("lost changed the session: %v", s)
			}
		} else if sessionState(s) != "approved" || f.h.grants() != 1 {
			t.Fatalf("delivered: %v", s)
		}
	}
}

// The Signer is down: the result is unknown, recorded so, never retried
// with a new ID; once it is back the same IDs recover.
func TestA2SignerUnavailable(t *testing.T) {
	f := startA2(t, 60)
	sid, intent, _ := f.ask("01")
	f.stopSigner()
	code, r := f.challenge(sid, "att-req-0000000000001", "approve")
	if code != 503 || r["error"] != "result_unknown" || attempt(r)["state"] != "unknown" {
		t.Fatalf("down: %d %v", code, r)
	}
	if code, r := f.status(sid, "att-req-0000000000001"); code != 503 || attempt(r)["state"] != "unknown" {
		t.Fatalf("status while down: %d %v", code, r)
	}
	f.startSigner()
	code, r = f.status(sid, "att-req-0000000000001")
	if code != 200 || attempt(r)["state"] != "challenged" {
		t.Fatalf("recovered: %d %v", code, r)
	}
	// A failed Host write after the Signer verified: reported, then recovered by the same IDs.
	f.failResolve.fail = true
	code, r = f.assert(sid, "att-req-0000000000001", "ver-req-0000000000001", f.sign(r, sid, intent, "approve"))
	if code != 503 || r["state_not_saved"] != true || sessionState(r) != "pending" {
		t.Fatalf("write failed: %d %v", code, r)
	}
	if code, r := f.status(sid, "att-req-0000000000001"); code != 200 || sessionState(r) != "approved" || f.h.grants() != 1 {
		t.Fatalf("after the write failure: %d %v", code, r)
	}
}

// The host cancels the task while the person verifies: the session is
// withdrawn, the Signer's approval is abandoned, no grant.
func TestA2CancelledTask(t *testing.T) {
	f := startA2(t, 60)
	sid, intent, task := f.ask("01")
	_, r := f.challenge(sid, "att-req-0000000000001", "approve")
	as := f.sign(r, sid, intent, "approve")
	if code, c := f.h.post("/api/host/tasks/"+task+"/cancel", f.h.tokens["operator"], `{}`); code != 200 && code != 202 {
		t.Fatalf("cancel task: %d %v", code, c)
	}
	f.h.waitFor(task, func(v taskReply) bool { return v.Purchases[0].Approval.State == "withdrawn" })
	code, r := f.assert(sid, "att-req-0000000000001", "ver-req-0000000000001", as)
	if code != 200 || r["outcome"] != "closed" || sessionState(r) != "withdrawn" || f.h.grants() != 0 {
		t.Fatalf("after cancel: %d %v", code, r)
	}
	id := attempt(r)["signer_approval_id"].(string)
	if a, err := f.core.ApprovalByID(context.Background(), id); err != nil || a.State != "abandoned" {
		t.Fatalf("Signer approval %+v %v", a, err)
	}
}

// serve-approvals has no payment route; the Host says approvals-only.
func TestA2PaymentModeOff(t *testing.T) {
	f := startA2(t, 60)
	c := &http.Client{Transport: &http.Transport{DialContext: func(ctx context.Context, _, _ string) (net.Conn, error) {
		return (&net.Dialer{}).DialContext(ctx, "unix", f.sock)
	}}}
	for _, p := range [][2]string{{"POST", "/v1/sign"}, {"GET", "/v1/attempts?request_id=x"}, {"POST", "/v1/attempts/att-1/abandon"}} {
		req, _ := http.NewRequest(p[0], "http://signer"+p[1], strings.NewReader(`{"request_id":"r","authorization":{"method":"delegation"}}`))
		resp, err := c.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		var v map[string]any
		json.NewDecoder(resp.Body).Decode(&v)
		resp.Body.Close()
		if resp.StatusCode != 409 || v["error"] != "PAYMENT_MODE_OFF" {
			t.Fatalf("%v: %d %v", p, resp.StatusCode, v)
		}
	}
	code, raw, _ := f.h.get("/api/host/status", f.h.tokens["host"])
	var st statusReply
	json.Unmarshal(raw, &st)
	if code != 200 || st.HumanApproval == nil || st.HumanApproval.Origin != f.origin || st.HumanApproval.Sources.Payment != "disabled" || st.PaymentCapability != "unavailable" {
		t.Fatalf("status %d %s", code, raw)
	}
}

func TestA2EnrollmentRelayRefusals(t *testing.T) {
	f := startA2(t, 60)
	id, _, _, err := f.core.EnrollBegin(context.Background(), "approver-b")
	if err != nil {
		t.Fatal(err)
	}
	if code, v := f.post("/api/enrollments/"+id+"/options", "", map[string]any{"code": "WRONGCOD"}); code != 409 || v["error"] != "ENROLLMENT_CODE" {
		t.Fatalf("wrong code: %d %v", code, v)
	}
	if code, _ := f.post("/api/enrollments/"+id+"/options", "", map[string]any{"code": "WRONGCOD"}, func(r *http.Request) { r.Host = f.h.addr }); code != 403 {
		t.Fatalf("127.0.0.1: %d", code)
	}
	if code, _ := f.post("/api/enrollments/"+id+"/options", "", map[string]any{"code": "WRONGCOD"}, func(r *http.Request) { r.Header.Set("Origin", "null") }); code != 403 {
		t.Fatalf("null origin: %d", code)
	}
	if code, _, _ := f.h.get("/approve/enroll", "", func(r *http.Request) { r.Host = f.alias }); code != 200 {
		t.Fatalf("enroll page: %d", code)
	}
	if code, _, _ := f.h.get("/approve/enroll", ""); code != 403 {
		t.Fatalf("enroll page on 127.0.0.1: %d", code)
	}
}

// unreadable makes the Signer's answer to the verify request lookup fail
// (a read error), while everything else works.
type unreadable struct{ approvalSigner }

func (unreadable) ByVerifyRequest(string) signerclient.Answer {
	return signerclient.Answer{Kind: signerclient.KindUnknown}
}

// R3: a failed lookup of the verify request is not "not found": the
// attempt stays as it is, nothing is closed (design 7, row 4).
func TestA2VerifyLookupFails(t *testing.T) {
	f := startA2(t, 60)
	sid, _, _ := f.ask("01")
	_, r := f.challenge(sid, "att-req-0000000000001", "approve")
	chID := r["challenge"].(map[string]any)["challenge_id"].(string)
	if _, err := f.h.st.UpdateApprovalAttempt(store.AttemptUpdate{ConsumerID: "consumer-a", RequestID: "att-req-0000000000001", From: []string{"challenged"},
		State: "verifying", VerifyRequestID: "ver-req-0000000000001"}); err != nil {
		t.Fatal(err)
	}
	real := f.relay.signer
	f.relay.signer = unreadable{real}
	code, s := f.status(sid, "att-req-0000000000001")
	f.relay.signer = real
	if c, err := f.core.ChallengeByID(context.Background(), chID); err != nil || c.State != "issued" || code != 503 || attempt(s)["state"] != "verifying" {
		t.Fatalf("a failed read was taken for not found: %d %v (challenge %s %v)", code, s, c.State, err)
	}
}
