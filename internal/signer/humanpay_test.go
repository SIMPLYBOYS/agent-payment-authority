package signer

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/SIMPLYBOYS/agent-payment-authority/internal/paycontract"
	"github.com/SIMPLYBOYS/agent-payment-authority/internal/signer/signertest"
	"github.com/decred/dcrd/dcrec/secp256k1/v4"
)

// hfx is the P2 Signer: the approval and payment cores on one signer.db.
type hfx struct {
	*afx
	pay   *Core
	db    *sql.DB
	dir   string
	auths map[string]*signertest.Authenticator // gap 4: each approver's device, to confirm its delegation
	confs map[string]confirmed
}

func newHFX(t *testing.T) *hfx {
	t.Helper()
	f := &hfx{afx: &afx{t: t, now: t0}, dir: t.TempDir()}
	a, b := Address(keyA.PubKey()), Address(keyB.PubKey())
	cfg, err := ParseConfig(signertest.ApprovalsConfig("/tmp/apa-test.sock", 501, f.now, a, b, origin))
	if err != nil {
		t.Fatal(err)
	}
	if f.db, err = OpenDB(filepath.Join(f.dir, "signer.db")); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { f.db.Close() })
	clock := func() uint64 { return f.now }
	if f.core, err = NewApprovalCoreDB(bg, cfg, f.db, f.dir, clock); err != nil {
		t.Fatal(err)
	}
	if f.pay, err = NewCoreDB(bg, cfg, map[string]*secp256k1.PrivateKey{a: keyA, b: keyB}, f.db, clock, reserved{}); err != nil {
		t.Fatal(err)
	}
	f.core.keys = f.pay.keys // as Core.Approvals wires them: a revocation's receipt is signed with the wallet's key
	return f
}

// orderA is consumer A's next purchase of amount, payable by keyA.
func (f *hfx) orderA(amount string) json.RawMessage {
	f.n++
	return signertest.Order{Consumer: "consumer-a", Agent: "agent-a", Wallet: "wallet-a", From: Address(keyA.PubKey()), PayTo: signertest.Merchant,
		Amount: amount, N: f.n, Now: f.now, Window: 600}.Raw()
}

// approve runs a challenge and its verified assertion for order.
func (f *hfx) approve(a *signertest.Authenticator, order json.RawMessage, decision string) ApprovalReply {
	f.t.Helper()
	f.n++
	c := f.mustChallenge("req-"+string(rune('a'+f.n)), order, decision)
	r, err := f.verify("ver-"+string(rune('a'+f.n)), c.ChallengeID, order, a.Get(c.Options, origin))
	if err != nil {
		f.t.Fatalf("verify: %v", err)
	}
	return r
}

// byApproval is a webauthn payment's authority under its consumer's
// confirmed delegation (gap 4): confirmed here once per consumer with its
// enrolled device (again once expired), with a budget wide enough that the
// approver's own limits are what these tests meet.
func (f *hfx) byApproval(id string) Authorization {
	consumer := "consumer-a" // an unknown approval: the payment's own refusal is what is tested
	f.db.QueryRow(`SELECT c.consumer_id FROM human_approvals h JOIN approval_challenges c ON c.challenge_id = h.challenge_id WHERE h.approval_id = ?`, id).Scan(&consumer)
	return Authorization{Method: "webauthn", ApprovalID: id, DelegationConfirmationID: f.confirmed(consumer)}
}

type confirmed struct {
	id    string
	until uint64
}

func (f *hfx) enroll(approver string, a *signertest.Authenticator) *signertest.Authenticator {
	a = f.afx.enroll(approver, a)
	if f.auths == nil {
		f.auths = map[string]*signertest.Authenticator{}
	}
	f.auths[approver] = a
	return a
}

func (f *hfx) confirmed(consumer string) string {
	f.t.Helper()
	if c, ok := f.confs[consumer]; ok && f.now < c.until {
		return c.id
	}
	x := strings.TrimPrefix(consumer, "consumer-")
	a := f.auths["approver-"+x]
	if a == nil {
		return "" // no device: refused as DELEGATION_NOT_CONFIRMED, after the payment's own checks
	}
	f.n++
	id := fmt.Sprintf("dlg-t%d", f.n)
	var tt paycontract.DelegationTerms
	json.Unmarshal(f.terms(x, id), &tt)
	tt.TaskBudget = "100000000"
	terms, _ := json.Marshal(tt)
	c, err := f.dchallenge("dreq-"+id, terms)
	if err != nil {
		f.t.Fatalf("delegation challenge: %v", err)
	}
	r, err := f.dverify("dver-"+id, c.ChallengeID, terms, a.Get(c.Options, origin))
	if err != nil {
		f.t.Fatalf("delegation verify: %v", err)
	}
	if f.confs == nil {
		f.confs = map[string]confirmed{}
	}
	f.confs[consumer] = confirmed{r.ApprovalID, r.ExpiresAt}
	return r.ApprovalID
}

func (f *hfx) approvalState(id string) string {
	var s string
	if err := f.db.QueryRow(`SELECT state FROM human_approvals WHERE approval_id = ?`, id).Scan(&s); err != nil {
		f.t.Fatal(err)
	}
	return s
}

// A verified approval pays once: consumed with its attempt, in one
// transaction; every refusal leaves it verified and writes no attempt.
func TestHumanApprovalPays(t *testing.T) {
	f := newHFX(t)
	a := f.enroll("approver-a", nil)
	order := f.orderA("500000")
	ap := f.approve(a, order, "approve")

	other := f.orderA("500000") // another purchase: another intent
	deny := f.orderA("500000")
	denied := f.approve(a, deny, "deny")
	big := f.orderA("1000001") // over signer_max
	bigAp := f.approve(a, big, "approve")
	for name, cs := range map[string]struct {
		r    SignRequest
		want string
	}{
		"no approval":             {SignRequest{RequestID: "request-0000000101", Authorization: f.byApproval(""), Order: order}, "APPROVAL_UNKNOWN"},
		"unknown approval":        {SignRequest{RequestID: "request-0000000102", Authorization: f.byApproval("hap-00"), Order: order}, "APPROVAL_UNKNOWN"},
		"another purchase":        {SignRequest{RequestID: "request-0000000103", Authorization: f.byApproval(ap.ApprovalID), Order: other}, "INTENT_MISMATCH"},
		"a deny":                  {SignRequest{RequestID: "request-0000000104", Authorization: f.byApproval(denied.ApprovalID), Order: deny}, "APPROVAL_DENIED"},
		"over the approver":       {SignRequest{RequestID: "request-0000000105", Authorization: f.byApproval(bigAp.ApprovalID), Order: big}, "OVER_PER_TX"},
		"mixed with a delegation": {SignRequest{RequestID: "request-0000000106", Authorization: Authorization{Method: "webauthn", ApprovalID: ap.ApprovalID, DelegationID: "dlg-a", DelegationVersion: 1}, Order: order}, "BAD_REQUEST"},
		"a delegation naming it":  {SignRequest{RequestID: "request-0000000107", Authorization: Authorization{Method: "delegation", ApprovalID: ap.ApprovalID, DelegationID: "dlg-a", DelegationVersion: 1}, Order: order}, "BAD_REQUEST"},
	} {
		if _, err := f.pay.Sign(bg, cs.r); code(err) != cs.want {
			t.Errorf("%s: %v, want %s", name, err, cs.want)
		}
	}
	if n := count(t, f.pay, "1"); n != 0 || f.approvalState(ap.ApprovalID) != "verified" {
		t.Fatalf("refusals wrote %d attempts, approval %s", n, f.approvalState(ap.ApprovalID))
	}

	rep, err := f.pay.Sign(bg, SignRequest{RequestID: "request-0000000110", Authorization: f.byApproval(ap.ApprovalID), Order: order})
	if err != nil || rep.State != "signed" {
		t.Fatalf("sign %+v %v", rep, err)
	}
	if f.approvalState(ap.ApprovalID) != "consumed" || count(t, f.pay, "approval_id = '"+ap.ApprovalID+"' AND delegation_id IS NULL") != 1 {
		t.Fatal("the approval was not consumed with its attempt")
	}
	// The same request again is its resend; another request with the same approval is refused.
	if again, err := f.pay.Sign(bg, SignRequest{RequestID: "request-0000000110", Authorization: f.byApproval(ap.ApprovalID), Order: order}); err != nil || again.AttemptID != rep.AttemptID {
		t.Fatalf("resend %+v %v", again, err)
	}
	if _, err := f.pay.Sign(bg, SignRequest{RequestID: "request-0000000111", Authorization: f.byApproval(ap.ApprovalID), Order: order}); code(err) != "APPROVAL_CONSUMED" {
		t.Fatalf("a second payment: %v", err)
	}
	// The A2 query sees the approval consumed.
	if r, err := f.core.ApprovalByID(bg, ap.ApprovalID); err != nil || r.State != "consumed" {
		t.Fatalf("approval query %+v %v", r, err)
	}
}

// E1-a review: an apa.pay.v2 purchase approved by the person's device is
// signed once, as a v1 one is: the grant and intent under v2's own domain.
func TestHumanApprovalPaysV2(t *testing.T) {
	f := newHFX(t)
	a := f.enroll("approver-a", nil)
	f.n++
	order := signertest.Order{Consumer: "consumer-a", Agent: "agent-a", Wallet: "wallet-a", From: Address(keyA.PubKey()), PayTo: signertest.Merchant,
		Amount: "500000", N: f.n, Now: f.now, Window: 600, Contract: paycontract.V2}.Raw()
	var po paycontract.PurchaseOrder
	json.Unmarshal(order, &po)
	sp, _ := paycontract.SpecOf(paycontract.V2)
	c, err := f.core.Challenge(bgA, ChallengeRequest{RequestID: "req-v2-pay", ApprovalSessionID: sp.SessionID(po.Intent.TaskID, po.Intent.PurchaseOrderID),
		Decision: "approve", Order: order, HostDeadline: f.now + 600})
	if err != nil {
		t.Fatalf("v2 challenge: %v", err)
	}
	ap, err := f.verify("ver-v2-pay", c.ChallengeID, order, a.Get(c.Options, origin))
	if err != nil {
		t.Fatalf("v2 verify: %v", err)
	}
	rep, err := f.pay.Sign(bg, SignRequest{RequestID: "request-0000000210", Authorization: f.byApproval(ap.ApprovalID), Order: order})
	if err != nil || rep.State != "signed" || f.approvalState(ap.ApprovalID) != "consumed" {
		t.Fatalf("sign %+v %v (approval %s)", rep, err, f.approvalState(ap.ApprovalID))
	}
}

// What changes after the approval: its expiry, the credential, the
// approver's version and wallets, the approval itself.
func TestHumanApprovalLapses(t *testing.T) {
	f := newHFX(t)
	a := f.enroll("approver-a", nil)
	o1, o2, o3, o4, o5 := f.orderA("500000"), f.orderA("500000"), f.orderA("500000"), f.orderA("500000"), f.orderA("500000")
	ap1, ap2 := f.approve(a, o1, "approve"), f.approve(a, o2, "approve")
	ap3, ap4, ap5 := f.approve(a, o3, "approve"), f.approve(a, o4, "approve"), f.approve(a, o5, "approve")

	if _, err := f.core.Abandon(bg, ap3.ApprovalID); err != nil {
		t.Fatal(err)
	}
	if _, err := f.pay.Sign(bg, SignRequest{RequestID: "request-0000000203", Authorization: f.byApproval(ap3.ApprovalID), Order: o3}); code(err) != "APPROVAL_NOT_VERIFIED" {
		t.Fatalf("abandoned: %v", err)
	}
	ar := &f.pay.cfg.Approvers[0]
	ar.WalletIDs = []string{"wallet-b"}
	if _, err := f.pay.Sign(bg, SignRequest{RequestID: "request-0000000204", Authorization: f.byApproval(ap4.ApprovalID), Order: o4}); code(err) != "CONSUMER_MISMATCH" {
		t.Fatalf("the wallet is no longer the approver's: %v", err)
	}
	ar.WalletIDs = []string{"wallet-a"}
	ar.Version++
	if _, err := f.pay.Sign(bg, SignRequest{RequestID: "request-0000000205", Authorization: f.byApproval(ap5.ApprovalID), Order: o5}); code(err) != "APPROVER_CHANGED" {
		t.Fatalf("another approver version: %v", err)
	}
	ar.Version--

	f.now += 300 // approval_ttl_seconds
	if _, err := f.pay.Sign(bg, SignRequest{RequestID: "request-0000000201", Authorization: f.byApproval(ap1.ApprovalID), Order: o1}); code(err) != "APPROVAL_EXPIRED" {
		t.Fatalf("expired: %v", err)
	}
	f.now -= 300
	if err := f.core.Revoke(bg, Fingerprint(a.ID)); err != nil {
		t.Fatal(err)
	}
	if _, err := f.pay.Sign(bg, SignRequest{RequestID: "request-0000000202", Authorization: f.byApproval(ap2.ApprovalID), Order: o2}); code(err) != "CREDENTIAL_REVOKED" {
		t.Fatalf("revoked: %v", err)
	}
	if count(t, f.pay, "1") != 0 {
		t.Fatal("an attempt was written")
	}
}

// The approval read again in the accepting transaction: abandoned or its
// credential revoked after verify and before accept, nothing is written.
func TestHumanApprovalChangesBeforeAccept(t *testing.T) {
	f := newHFX(t)
	a := f.enroll("approver-a", nil)
	for i, cs := range []struct {
		change func(ap ApprovalReply) error
		want   string
	}{
		{func(ap ApprovalReply) error { _, err := f.core.Abandon(bg, ap.ApprovalID); return err }, "APPROVAL_NOT_VERIFIED"},
		{func(ApprovalReply) error { return f.core.Revoke(bg, Fingerprint(a.ID)) }, "CREDENTIAL_REVOKED"},
	} {
		order := f.orderA("500000")
		ap := f.approve(a, order, "approve")
		hook = func(stage, _ string) error {
			if stage == "after-verify" {
				hook = nil
				return cs.change(ap)
			}
			return nil
		}
		_, err := f.pay.Sign(bg, SignRequest{RequestID: "request-00000006" + string(rune('0'+i)) + "0", Authorization: f.byApproval(ap.ApprovalID), Order: order})
		hook = nil
		if code(err) != cs.want || count(t, f.pay, "1") != 0 {
			t.Fatalf("%s: %v, %d attempts", cs.want, err, count(t, f.pay, "1"))
		}
	}
}

// Two requests racing on one approval (two request IDs, one order): one
// attempt, one consumption.
func TestHumanApprovalRace(t *testing.T) {
	f := newHFX(t)
	a := f.enroll("approver-a", nil)
	order := f.orderA("500000")
	ap := f.approve(a, order, "approve")
	var wg sync.WaitGroup
	errs := make([]error, 8)
	for i := range errs {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, errs[i] = f.pay.Sign(bg, SignRequest{RequestID: "request-00000003" + string(rune('0'+i)) + "0", Authorization: f.byApproval(ap.ApprovalID), Order: order})
		}()
	}
	wg.Wait()
	ok := 0
	for _, err := range errs {
		switch code(err) {
		case "":
			ok++
		case "APPROVAL_CONSUMED", "INTENT_IN_USE", "SIGNER_BUSY":
		default:
			t.Errorf("unexpected %v", err)
		}
	}
	if ok != 1 || count(t, f.pay, "1") != 1 {
		t.Fatalf("%d signed, %d attempts", ok, count(t, f.pay, "1"))
	}
}

// A2's approvals.db moves into signer.db once, every row counted; the
// moved approval then pays.
func TestMigrateApprovals(t *testing.T) {
	dir := t.TempDir()
	now := uint64(t0)
	a, b := Address(keyA.PubKey()), Address(keyB.PubKey())
	cfg, err := ParseConfig(signertest.ApprovalsConfig("/tmp/apa-test.sock", 501, now, a, b, origin))
	if err != nil {
		t.Fatal(err)
	}
	old, err := NewApprovalCore(bg, cfg, filepath.Join(dir, "approvals.db"), func() uint64 { return now })
	if err != nil {
		t.Fatal(err)
	}
	f := &hfx{afx: &afx{t: t, core: old, now: now}}
	auth := f.enroll("approver-a", nil)
	order := f.orderA("500000")
	ap := f.approve(auth, order, "approve")
	conf := f.confirmed("consumer-a") // gap 4: confirmed in approvals.db, moved with it
	// Still in use (a read open on it): refused, nothing moved.
	tx, err := old.db.BeginTx(bg, &sql.TxOptions{ReadOnly: true})
	if err != nil {
		t.Fatal(err)
	}
	var n int
	tx.QueryRow(`SELECT count(*) FROM human_approvals`).Scan(&n)
	if _, err := MigrateApprovals(bg, dir); err == nil || Unmigrated(dir) == nil {
		t.Fatalf("migrated an approvals.db in use: %v", err)
	}
	tx.Rollback()
	// Left open, not closed: its WAL is folded in by the migration.
	defer old.Close()

	if err := Unmigrated(dir); err == nil {
		t.Fatal("approvals.db not noticed")
	}
	// A P1-b signer.db with its old attempts table, one row kept.
	p1b, _ := OpenDB(filepath.Join(dir, "signer.db"))
	if _, err := p1b.Exec(`CREATE TABLE attempts (attempt_id TEXT PRIMARY KEY, request_id TEXT NOT NULL UNIQUE, fingerprint TEXT NOT NULL,
		intent_hash TEXT NOT NULL UNIQUE, wallet_id TEXT NOT NULL, chain_id TEXT NOT NULL, asset_contract TEXT NOT NULL, payer TEXT NOT NULL,
		nonce TEXT NOT NULL, delegation_id TEXT NOT NULL, amount INTEGER NOT NULL, state TEXT NOT NULL, accepted_at INTEGER NOT NULL,
		auth_deadline INTEGER NOT NULL, typed_digest TEXT NOT NULL, snapshot TEXT NOT NULL, signature TEXT, signed_at INTEGER, closed_at INTEGER) STRICT;
		INSERT INTO attempts VALUES ('att-old', 'request-old', 'fp', 'ih', 'wallet-a', 'c', 'x', 'p', 'n', 'dlg-a', 1, 'abandoned', 1, 2, 'd', '{}', NULL, NULL, 3)`); err != nil {
		t.Fatal(err)
	}
	p1b.Close()

	moved, err := MigrateApprovals(bg, dir)
	if err != nil || moved["human_approvals"] != 2 || moved["credentials"] != 1 || moved["approval_challenges"] != 2 || moved["delegation_confirmations"] != 1 {
		t.Fatalf("moved %v %v", moved, err)
	}
	if Unmigrated(dir) != nil {
		t.Fatal("approvals.db still there")
	}
	if _, err := os.Stat(filepath.Join(dir, "approvals.db.migrated")); err != nil {
		t.Fatal(err)
	}
	if _, err := MigrateApprovals(bg, dir); err == nil {
		t.Fatal("migrated twice")
	}
	// Stopped before its rename (approvals.db back in place): run again,
	// it only finishes the rename (nothing can
	// use signer.db meanwhile: Unmigrated refuses).
	os.Rename(filepath.Join(dir, "approvals.db.migrated"), filepath.Join(dir, "approvals.db"))
	if moved, err := MigrateApprovals(bg, dir); err != nil || moved["human_approvals"] != 2 || Unmigrated(dir) != nil {
		t.Fatalf("finishing the rename: %v %v", moved, err)
	}

	db, _ := OpenDB(filepath.Join(dir, "signer.db"))
	defer db.Close()
	pay, err := NewCoreDB(context.Background(), cfg, map[string]*secp256k1.PrivateKey{a: keyA, b: keyB}, db, func() uint64 { return now }, reserved{})
	if err != nil {
		t.Fatal(err)
	}
	if count(t, pay, "attempt_id = 'att-old' AND approval_id IS NULL") != 1 {
		t.Fatal("the P1-b attempt was lost")
	}
	if rep, err := pay.Sign(bg, SignRequest{RequestID: "request-0000000401", Authorization: Authorization{Method: "webauthn", ApprovalID: ap.ApprovalID, DelegationConfirmationID: conf}, Order: order}); err != nil || rep.State != "signed" {
		t.Fatalf("pay after migration %+v %v", rep, err)
	}
	// The old approvals.db again, now differing from signer.db with the
	// same row counts (the approval was consumed since): refused, nothing moved.
	os.Rename(filepath.Join(dir, "approvals.db.migrated"), filepath.Join(dir, "approvals.db"))
	if _, err := MigrateApprovals(bg, dir); err == nil || Unmigrated(dir) == nil {
		t.Fatalf("merged into a signer.db with other approvals: %v", err)
	}
}

// The P2 Signer over a real Unix socket: approvals and payments on one
// socket and one signer.db; the approval verified there pays there.
func TestServeAll(t *testing.T) {
	f := newHFX(t)
	f.pay.cfg.Callers = []uint32{uint32(os.Getuid())}
	ac, err := f.pay.Approvals(bg, f.dir, func() uint64 { return f.now })
	if err != nil {
		t.Fatal(err)
	}
	dir, err := os.MkdirTemp("", "sg") // short: a socket path is at most 104 bytes
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(dir)
	sock := filepath.Join(dir, "s")
	ln, err := net.Listen("unix", sock)
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(bg)
	done := make(chan error, 1)
	go func() { done <- ServeAll(ctx, ln, f.pay, ac, func(string) {}) }()
	defer func() { cancel(); <-done }()
	client := &http.Client{Transport: &http.Transport{DialContext: func(ctx context.Context, _, _ string) (net.Conn, error) {
		return (&net.Dialer{}).DialContext(ctx, "unix", sock)
	}}}
	do := func(method, path string, body any) (int, map[string]any) {
		raw, _ := json.Marshal(body)
		req, _ := http.NewRequest(method, "http://signer"+path, bytes.NewReader(raw))
		res, err := client.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		defer res.Body.Close()
		var m map[string]any
		json.NewDecoder(res.Body).Decode(&m)
		return res.StatusCode, m
	}
	if _, m := do("GET", "/v1/health", nil); m["state"] != "ready" || m["payment"] != "available" {
		t.Fatalf("health %v", m)
	}
	ac.pays = false // as without a reservation verifier
	if _, m := do("GET", "/v1/health", nil); m["state"] != "approvals-only" || m["payment"] != "unavailable" {
		t.Fatalf("health without payments %v", m)
	}
	ac.pays = true
	a := f.enroll("approver-a", nil)
	order := f.orderA("500000")
	_, c := do("POST", "/v1/approvals/challenge", ChallengeRequest{RequestID: "req-s1", ApprovalSessionID: sessionOf(order), Decision: "approve", Order: order, HostDeadline: f.now + 600})
	opts, _ := json.Marshal(c["options"])
	_, v := do("POST", "/v1/approvals/verify", VerifyRequest{VerifyRequestID: "ver-s1", ChallengeID: c["challenge_id"].(string), Order: order, Assertion: a.Get(opts, origin)})
	id, _ := v["approval_id"].(string)
	if v["state"] != "verified" || id == "" {
		t.Fatalf("verify %v", v)
	}
	if st, r := do("POST", "/v1/sign", SignRequest{RequestID: "request-0000000501", Authorization: f.byApproval(id), Order: order}); st != 200 || r["state"] != "signed" {
		t.Fatalf("sign %d %v", st, r)
	}
	if _, r := do("GET", "/v1/approvals/"+id, nil); r["state"] != "consumed" {
		t.Fatalf("approval %v", r)
	}
}

// The approver's cumulative cap (signer_total over signer_window_seconds):
// within the window the payments together stay under it; a refusal
// consumes nothing; past the window the approver pays again.
func TestHumanApprovalTotal(t *testing.T) {
	f := newHFX(t) // signer_total 2000000 per 86400 s
	a := f.enroll("approver-a", nil)
	for i := range 4 {
		order := f.orderA("500000")
		ap := f.approve(a, order, "approve")
		if _, err := f.pay.Sign(bg, SignRequest{RequestID: "request-00000007" + string(rune('0'+i)) + "0", Authorization: f.byApproval(ap.ApprovalID), Order: order}); err != nil {
			t.Fatalf("payment %d: %v", i, err)
		}
		f.now += 60
	}
	order := f.orderA("1")
	ap := f.approve(a, order, "approve")
	if _, err := f.pay.Sign(bg, SignRequest{RequestID: "request-0000000750", Authorization: f.byApproval(ap.ApprovalID), Order: order}); code(err) != "OVER_TOTAL_CAP" {
		t.Fatalf("over the total: %v", err)
	}
	if f.approvalState(ap.ApprovalID) != "verified" || count(t, f.pay, "1") != 4 {
		t.Fatal("the refusal consumed something")
	}
	// Consumer B's approver has its own total.
	b := f.enroll("approver-b", nil)
	f.n++
	ob := signertest.Order{Consumer: "consumer-b", Agent: "agent-b", Wallet: "wallet-b", From: Address(keyB.PubKey()), PayTo: signertest.Merchant,
		Amount: "500000", N: f.n, Now: f.now, Window: 600}.Raw()
	apb := f.approve(b, ob, "approve")
	if _, err := f.pay.Sign(bg, SignRequest{RequestID: "request-0000000760", Authorization: f.byApproval(apb.ApprovalID), Order: ob}); err != nil {
		t.Fatalf("B: %v", err)
	}
	// The first payment (accepted at t0) leaves the window exactly when the
	// window has passed: room for 500000 again.
	f.now = t0 + 86400
	order = f.orderA("500000")
	ap = f.approve(a, order, "approve")
	if _, err := f.pay.Sign(bg, SignRequest{RequestID: "request-0000000770", Authorization: f.byApproval(ap.ApprovalID), Order: order}); err != nil {
		t.Fatalf("after the window: %v", err)
	}
}

func TestApproverTotalConfig(t *testing.T) {
	a, b := Address(keyA.PubKey()), Address(keyB.PubKey())
	for name, edit := range map[string]func(ap map[string]any){
		"no total":        func(ap map[string]any) { delete(ap, "signer_total") },
		"total below max": func(ap map[string]any) { ap["signer_total"] = "999999" },
		"no window":       func(ap map[string]any) { delete(ap, "signer_window_seconds") },
	} {
		var c map[string]any
		json.Unmarshal(signertest.ApprovalsConfig("/tmp/apa-test.sock", 501, t0, a, b, origin), &c)
		edit(c["approvers"].([]any)[0].(map[string]any))
		raw, _ := json.Marshal(c)
		if _, err := ParseConfig(raw); err == nil {
			t.Errorf("%s: accepted", name)
		}
	}
}
