package main

// serve-approvals and the administrator's commands (A2; design 6.4, R2):
// no key is needed or touched. Since P2-D1 their data is signer.db.

import (
	"context"
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"github.com/SIMPLYBOYS/agent-payment-authority/internal/paycontract"
	"github.com/SIMPLYBOYS/agent-payment-authority/internal/signer"
	"github.com/SIMPLYBOYS/agent-payment-authority/internal/signer/signertest"
)

const testOrigin = "http://localhost:8402"

// approvalsDir is a private Signer directory with an approvals config and
// a keys/ directory nobody may read: if serve-approvals touched it, it
// would fail. The approvals live in signer.db (P2-D1).
func approvalsDir(t *testing.T) string {
	t.Helper()
	dir := filepath.Join(t.TempDir(), "signer")
	if err := os.MkdirAll(filepath.Join(dir, "keys"), 0o700); err != nil {
		t.Fatal(err)
	}
	os.Chmod(dir, 0o700)
	cfg := signertest.ApprovalsConfig(filepath.Join(dir, "s.sock"), uint32(os.Getuid()), t0,
		"0x1111111111111111111111111111111111111111", "0x2222222222222222222222222222222222222222", testOrigin)
	if err := os.WriteFile(filepath.Join(dir, "signer.json"), cfg, 0o600); err != nil {
		t.Fatal(err)
	}
	os.Chmod(filepath.Join(dir, "keys"), 0o000)
	t.Cleanup(func() { os.Chmod(filepath.Join(dir, "keys"), 0o700) })
	return dir
}

func TestServeApprovalsNeedsNoKey(t *testing.T) {
	dir := approvalsDir(t)
	now := uint64(t0)
	core, cfg, err := assembleApprovals(dir, func() uint64 { return now })
	if err != nil {
		t.Fatalf("assemble without keys: %v", err)
	}
	defer core.Close()
	if fi, err := os.Stat(filepath.Join(dir, "signer.db")); err != nil || fi.Mode().Perm() != 0o600 {
		t.Fatalf("signer.db: %v %v", fi, err)
	}
	// The whole flow works without a key: enroll, confirm, challenge, verify.
	ctx := context.Background()
	a := signertest.NewAuthenticator()
	id, code, _, err := core.EnrollBegin(ctx, "approver-a")
	if err != nil {
		t.Fatal(err)
	}
	o, _ := core.EnrollOptions(ctx, id, code)
	if _, err := core.EnrollFinish(ctx, id, code, a.Create(o.Options, testOrigin)); err != nil {
		t.Fatal(err)
	}
	core.EnrollConfirm(ctx, id)
	order := signertest.Order{Consumer: "consumer-a", Agent: "agent-a", Wallet: "wallet-a", From: "0x1111111111111111111111111111111111111111",
		PayTo: signertest.Merchant, Amount: "500000", N: 1, Now: now, Window: 600}
	po := order.PO()
	c, err := core.Challenge(ctx, signer.ChallengeRequest{RequestID: "req-1", ApprovalSessionID: paycontract.SessionID(po.Intent.TaskID, po.Intent.PurchaseOrderID),
		Decision: "approve", Order: order.Raw(), HostDeadline: now + 600})
	if err != nil {
		t.Fatal(err)
	}
	if r, err := core.Verify(ctx, signer.VerifyRequest{VerifyRequestID: "ver-1", ChallengeID: c.ChallengeID, Order: order.Raw(), Assertion: a.Get(c.Options, testOrigin)}); err != nil || r.State != "verified" {
		t.Fatalf("verify %+v %v", r, err)
	}
	if cfg.WebAuthn == nil {
		t.Fatal("no webauthn section")
	}
	// keys/ stayed unreadable throughout.
}

func TestServeApprovalsRefusesItsInputs(t *testing.T) {
	clock := func() uint64 { return t0 }
	dir := approvalsDir(t)
	os.WriteFile(filepath.Join(dir, "signer.db"), nil, 0o644)
	os.Chmod(filepath.Join(dir, "signer.db"), 0o644)
	if _, _, err := assembleApprovals(dir, clock); err == nil || !strings.Contains(err.Error(), "0600") {
		t.Fatalf("group-readable signer.db: %v", err)
	}
	os.Remove(filepath.Join(dir, "signer.db"))
	// A2's approvals.db, not migrated: every command but migrate refuses it.
	os.WriteFile(filepath.Join(dir, "approvals.db"), nil, 0o600)
	if _, _, err := assembleApprovals(dir, clock); err == nil || !strings.Contains(err.Error(), "approver migrate") {
		t.Fatalf("an unmigrated approvals.db: %v", err)
	}
	if err := approver([]string{"list", "--dir", dir}, io.Discard, nil); err == nil {
		t.Fatal("list read past an unmigrated approvals.db")
	}
	os.Remove(filepath.Join(dir, "approvals.db"))
	// A payment configuration without approvers is not an approvals one.
	sd, _ := signerDir(t)
	if _, _, err := assembleApprovals(sd, clock); err == nil {
		t.Fatal("a config without approvers served approvals")
	}
}

// The payment mode is unchanged: with approvers in its configuration, a
// missing key still refuses to start.
func TestServeStillNeedsEveryKey(t *testing.T) {
	dir, _ := signerDir(t)
	var c map[string]any
	raw, _ := os.ReadFile(filepath.Join(dir, "signer.json"))
	json.Unmarshal(raw, &c)
	c["approvers"] = []map[string]any{{"approver_id": "approver-a", "consumer_id": "consumer-a", "wallet_ids": []string{"wallet-a"}, "signer_max": "1000000", "signer_total": "2000000", "signer_window_seconds": 86400, "version": 1}}
	c["webauthn"] = map[string]any{"rp_id": "localhost", "origins": []string{testOrigin}, "challenge_ttl_seconds": 120, "approval_ttl_seconds": 300}
	raw, _ = json.Marshal(c)
	os.WriteFile(filepath.Join(dir, "signer.json"), raw, 0o600)
	os.Remove(filepath.Join(dir, "keys", "wallet-b.key"))
	if _, _, err := assemble(dir, passphrase, func() uint64 { return t0 }); err == nil {
		t.Fatal("serve started without wallet-b's key")
	}
}

func TestApproverCommands(t *testing.T) {
	dir := approvalsDir(t)
	var out strings.Builder
	if err := approver([]string{"enroll", "begin", "--dir", dir, "--approver", "approver-a"}, &out, nil); err != nil {
		t.Fatal(err)
	}
	m := regexp.MustCompile(`enrollment (enr-[0-9a-f]+) for approver-a; one-time code ([A-Z0-9]{8})`).FindStringSubmatch(out.String())
	if m == nil || !strings.Contains(out.String(), testOrigin+"/approve/enroll") {
		t.Fatalf("begin printed %q", out.String())
	}
	core, _, err := assembleApprovals(dir, unixClock)
	if err != nil {
		t.Fatal(err)
	}
	a := signertest.NewAuthenticator()
	o, err := core.EnrollOptions(context.Background(), m[1], m[2])
	if err != nil {
		t.Fatal(err)
	}
	fin, err := core.EnrollFinish(context.Background(), m[1], m[2], a.Create(o.Options, testOrigin))
	core.Close()
	if err != nil {
		t.Fatal(err)
	}
	// Anything but yes leaves it pending.
	out.Reset()
	if err := approver([]string{"enroll", "confirm", "--dir", dir, "--id", m[1]}, &out, func(string) (string, error) { return "no\n", nil }); err == nil {
		t.Fatal("confirmed without yes")
	}
	if !strings.Contains(out.String(), fin.Fingerprint) {
		t.Fatalf("confirm did not show the fingerprint: %q", out.String())
	}
	if err := approver([]string{"enroll", "confirm", "--dir", dir, "--id", m[1]}, &out, func(string) (string, error) { return "yes\n", nil }); err != nil {
		t.Fatal(err)
	}
	out.Reset()
	approver([]string{"list", "--dir", dir}, &out, nil)
	if !strings.Contains(out.String(), "approver-a "+fin.Fingerprint+" active") {
		t.Fatalf("list %q", out.String())
	}
	if err := approver([]string{"revoke", "--dir", dir, "--credential", fin.Fingerprint}, &out, nil); err != nil {
		t.Fatal(err)
	}
	out.Reset()
	approver([]string{"list", "--dir", dir}, &out, nil)
	if !strings.Contains(out.String(), fin.Fingerprint+" revoked") {
		t.Fatalf("after revoke %q", out.String())
	}
	if err := approver([]string{"revoke", "--dir", dir, "--credential", "0000000000000000"}, &out, nil); err == nil {
		t.Fatal("revoked an unknown credential")
	}
	if err := approver([]string{"enroll", "begin", "--dir", dir}, &out, nil); err == nil {
		t.Fatal("begin without an approver")
	}
}

// approver migrate moves A2's approvals.db into signer.db once (P2-D1);
// afterwards the commands and serve read signer.db, and serve refuses a
// directory that still has approvals.db.
func TestApproverMigrate(t *testing.T) {
	dir := approvalsDir(t)
	raw, _ := os.ReadFile(filepath.Join(dir, "signer.json"))
	cfg, err := signer.ParseConfig(raw)
	if err != nil {
		t.Fatal(err)
	}
	old, err := signer.NewApprovalCore(context.Background(), cfg, filepath.Join(dir, "approvals.db"), unixClock)
	if err != nil {
		t.Fatal(err)
	}
	if _, _, _, err := old.EnrollBegin(context.Background(), "approver-a"); err != nil {
		t.Fatal(err)
	}
	old.Close()
	if _, _, err := assemble(dir, passphrase, unixClock); err == nil || !strings.Contains(err.Error(), "approver migrate") {
		t.Fatalf("serve with an unmigrated approvals.db: %v", err)
	}
	var out strings.Builder
	if err := approver([]string{"migrate", "--dir", dir}, &out, nil); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out.String(), "enrollments 1 rows") || !strings.Contains(out.String(), "approvals.db.migrated") {
		t.Fatalf("migrate printed %q", out.String())
	}
	if fi, err := os.Stat(filepath.Join(dir, "signer.db")); err != nil || fi.Mode().Perm() != 0o600 {
		t.Fatalf("signer.db: %v %v", fi, err)
	}
	if err := approver([]string{"migrate", "--dir", dir}, io.Discard, nil); err == nil {
		t.Fatal("migrated twice")
	}
	if err := approver([]string{"list", "--dir", dir}, io.Discard, nil); err != nil {
		t.Fatalf("list after migrate: %v", err)
	}
}
