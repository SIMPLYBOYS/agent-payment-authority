package signerclient

// The client against the real Store and an in-process Signer (R1 of the
// P1-b review, 22e45ed). The Store's registry is a test-only copy of the
// frozen fixture with the two wallets moved to the test keys' addresses, so
// its purchases can be signed; the fixture itself is not changed and the
// payer check is not relaxed. The transport is the real socket behind the
// counting stand-in.

import (
	"database/sql"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/SIMPLYBOYS/agent-payment-authority/internal/contracts"
	"github.com/SIMPLYBOYS/agent-payment-authority/internal/paycontract"
	"github.com/SIMPLYBOYS/agent-payment-authority/internal/policy"
	"github.com/SIMPLYBOYS/agent-payment-authority/internal/signer"
	"github.com/SIMPLYBOYS/agent-payment-authority/internal/signer/signertest"
	"github.com/SIMPLYBOYS/agent-payment-authority/internal/store"
)

func readJSON(t *testing.T, path string, dst any) {
	t.Helper()
	raw, err := os.ReadFile(filepath.Join("..", "..", path))
	if err != nil {
		t.Fatal(err)
	}
	if err := contracts.Decode(raw, dst); err != nil {
		t.Fatalf("%s: %v", path, err)
	}
}

// realStore opens a Store whose task-a is apa.pay.v1 under a confirmed
// delegation, with allowed purchases po-1..po-n of 0.05 submitted.
func realStore(t *testing.T, n int) (*store.Store, string) {
	t.Helper()
	var reg contracts.RegistryConfig
	readJSON(t, "testdata/contracts/registry.json", &reg)
	reg.Wallets[0].From, reg.Wallets[1].From = signer.Address(keyA.PubKey()), signer.Address(keyB.PubKey())
	var p policy.Profile
	readJSON(t, "testdata/policy/m1-demo-profile.json", &p)
	orders := make([]signertest.Order, n)
	offers := store.OfferBook{}
	for i := range orders {
		orders[i] = signertest.Order{Consumer: "consumer-a", Agent: "agent-a", Wallet: "wallet-a", Task: "task-a", From: reg.Wallets[0].From,
			PayTo: signertest.Merchant, Amount: "50000", N: i + 1, Now: t0, Window: 60}
		k := store.OfferKey{Method: "GET", URL: signertest.URL}
		offers[k] = append(offers[k], orders[i].Quote())
	}
	dir := filepath.Join(t.TempDir(), "db")
	if err := os.Mkdir(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(dir, "apa.db")
	s, err := store.Open(store.Config{Path: path, Registry: reg, Offers: offers, Clock: func() uint64 { return t0 }})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { s.Close() })
	d, _, err := s.ConfirmDelegation(store.DelegationRequest{RequestID: "dlg-req-task-a-000000000", ConsumerID: "consumer-a", Scenario: "pay", Purpose: "a brief",
		AssetID: p.AssetID, Merchants: p.Merchants, TaskBudget: "5000000", AllowMax: "100000", ApprovalDeadlineSeconds: 30, ValidSeconds: 600,
		ConfirmedBy: "simulated:consumer-a:host-test"})
	if err != nil {
		t.Fatal(err)
	}
	if err := s.StartTask(store.TaskSetup{TaskID: "task-a", Profile: &p, Credential: &policy.Credential{ConsumerID: "consumer-a", ValidUntil: t0 + 86400, EvidenceMode: "fixture"},
		ContractVersion: paycontract.Version, DelegationID: d.DelegationID}); err != nil {
		t.Fatal(err)
	}
	for _, o := range orders {
		r, err := s.Submit(contracts.Session{ConsumerID: "consumer-a", AgentID: "agent-a", TaskID: "task-a"}, o.Raw())
		if err != nil || r.Purchase.Decision.PolicyVerdict != "Allow" {
			t.Fatalf("submit %d: %+v %v", o.N, r.Purchase.Decision, err)
		}
	}
	return s, path
}

func attempts(t *testing.T, path string) int {
	t.Helper()
	db, err := sql.Open("sqlite", path+"?_pragma=query_only(1)")
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	var n int
	if err := db.QueryRow(`SELECT count(*) FROM payment_attempts`).Scan(&n); err != nil {
		t.Fatal(err)
	}
	return n
}

func storePay(po string) store.PaymentRequest {
	return store.PaymentRequest{ConsumerID: "consumer-a", TaskID: "task-a", PurchaseOrderID: po, Method: "delegation", DelegationRef: "dlg-a@1"}
}

// inStore is the commit handshake against the Store file: the Signer is
// asked only for an intent whose payment attempt is already committed.
func inStore(path string) committed {
	return func(intent string) bool {
		db, err := sql.Open("sqlite", path+"?_pragma=query_only(1)&_pragma=busy_timeout(5000)")
		if err != nil {
			return false
		}
		defer db.Close()
		var n int
		err = db.QueryRow(`SELECT count(*) FROM payment_attempts WHERE intent_hash = ? AND state IN ('requested', 'unknown', 'rejected')`, intent).Scan(&n)
		return err == nil && n == 1
	}
}

func TestRealStoreCancel(t *testing.T) {
	s, path := realStore(t, 3)
	e := newEnvChecking(t, inStore(path))
	e.listen()
	c, tr := countingClient(e.socket, s)

	// po-1: paid once; the resend keeps its request ID and signature.
	paid, err := c.Pay(storePay("po-1"), nil)
	if err != nil || paid.Attempt.State != "signed" || paid.Authorization == nil {
		t.Fatalf("pay: %+v %v", paid, err)
	}
	again, err := c.Pay(storePay("po-1"), nil)
	if err != nil || again.Attempt.RequestID != paid.Attempt.RequestID || again.Authorization.Signature != paid.Authorization.Signature {
		t.Fatalf("resend: %+v %v", again, err)
	}
	// po-2: the sign request fails on the way: unknown in the Store.
	tr.fail = true
	lost, err := c.Pay(storePay("po-2"), nil)
	tr.fail = false
	if !errors.Is(err, Unknown) || lost.Attempt.State != "unknown" {
		t.Fatalf("lost: %+v %v", lost, err)
	}

	if err := s.Cancel("task-a"); err != nil {
		t.Fatal(err)
	}
	rows, signs := attempts(t, path), tr.signs()
	// po-3, never paid, and po-2's unknown attempt: refused, nothing sent.
	for _, po := range []string{"po-3", "po-2"} {
		if _, err := c.Pay(storePay(po), nil); store.Code(err) != "TASK_ALREADY_ENDED" || tr.signs() != signs || attempts(t, path) != rows {
			t.Fatalf("%s after cancel: %v; sign sent after cancel: %d -> %d, attempts %d -> %d", po, err, signs, tr.signs(), rows, attempts(t, path))
		}
	}
	// What exists is still asked about and reconciled by its own ID.
	if r, err := c.Resume("consumer-a", lost.Attempt.RequestID); err == nil || r.Attempt.State != "rejected" || r.Attempt.RequestID != lost.Attempt.RequestID {
		t.Fatalf("resume of the unknown attempt: %+v %v", r, err)
	}
	if r, err := c.Resume("consumer-a", paid.Attempt.RequestID); err != nil || r.Attempt.State != "signed" || r.Authorization.Signature != paid.Authorization.Signature {
		t.Fatalf("resume of the signed attempt: %+v %v", r, err)
	}
	if r, err := c.Abandon("consumer-a", paid.Attempt.RequestID); err == nil || err.Error() != "ATTEMPT_NOT_ACCEPTED" || r.Attempt.State != "signed" {
		t.Fatalf("abandon of the signed attempt: %+v %v", r, err)
	}
	if tr.signs() != signs {
		t.Fatalf("sign sent after cancel: %d -> %d", signs, tr.signs())
	}
}
