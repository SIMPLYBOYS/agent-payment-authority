package payflow

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/SIMPLYBOYS/agent-payment-authority/internal/contracts"
	"github.com/SIMPLYBOYS/agent-payment-authority/internal/credential"
	"github.com/SIMPLYBOYS/agent-payment-authority/internal/merchant"
	"github.com/SIMPLYBOYS/agent-payment-authority/internal/paycontract"
	"github.com/SIMPLYBOYS/agent-payment-authority/internal/policy"
	"github.com/SIMPLYBOYS/agent-payment-authority/internal/signer/signertest"
	"github.com/SIMPLYBOYS/agent-payment-authority/internal/signerclient"
	"github.com/SIMPLYBOYS/agent-payment-authority/internal/store"
)

// E1a-3 (docs/commercial-e1a.md 4.3): an apa.pay.v2 purchase, its BBS+
// credential verified, paid on a local anvil by every real part and
// delivered; one intent from the purchase to the receipt, each event labeled
// by the task's run conditions (fake Agent, local anvil).
func TestV2PaidOnAnvil(t *testing.T) {
	w := newAnvilWorld(t)
	credDir := filepath.Join(t.TempDir(), "cred")
	if err := credential.InitDir(credDir, "issuer-test", []string{"consumer-a"}, int64(w.now)); err != nil {
		t.Fatal(err)
	}
	creds, err := credential.OpenDir(credDir)
	if err != nil {
		t.Fatal(err)
	}
	var reg contracts.RegistryConfig
	readJSON(t, "testdata/contracts/registry.json", &reg)
	reg.Wallets[0].From, reg.Assets[0].AssetContract = w.addrs[0], w.usdc.Hex()
	var p policy.Profile
	readJSON(t, "testdata/policy/m1-demo-profile.json", &p)
	p.AllowMax = "500000" // kr-market is an Allow here
	k := store.OfferKey{Method: w.offer.Method, URL: w.offer.URL}
	dir := filepath.Join(t.TempDir(), "db")
	os.Mkdir(dir, 0o700)
	s, err := store.Open(store.Config{Path: filepath.Join(dir, "apa.db"), Registry: reg, Offers: store.OfferBook{k: {w.offer.Quote}}, Clock: w.clock,
		Credentials: creds, VerifierID: "host-test"})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { s.Close() })
	d, _, err := s.ConfirmDelegation(store.DelegationRequest{ContractVersion: paycontract.V2, RequestID: "dlg-req-v2-task-a-00000", ConsumerID: "consumer-a", Scenario: "pay",
		Purpose: "a brief", AssetID: p.AssetID, Merchants: p.Merchants, TaskBudget: "5000000", AllowMax: "500000", ApprovalDeadlineSeconds: 30, ValidSeconds: 600,
		ConfirmedBy: "simulated:consumer-a:host-test"})
	if err != nil {
		t.Fatal(err)
	}
	setup := store.TaskSetup{TaskID: "task-a", Profile: &p, ContractVersion: paycontract.V2, DelegationID: d.DelegationID,
		Run: &paycontract.RunConditions{Agent: "fake", Chain: "local_anvil", Authenticator: "none"}}
	st, ok, err := creds.IssueTask("task-a", "consumer-a", int64(w.now+86400))
	if err != nil || !ok {
		t.Fatalf("issue: %v %v", ok, err)
	}
	if setup.BBS, err = store.NewBBSCredential(creds, st); err != nil {
		t.Fatal(err)
	}
	if err := s.StartTask(setup); err != nil {
		t.Fatal(err)
	}
	po := signertest.Order{Consumer: "consumer-a", Agent: "agent-a", Wallet: "wallet-a", Task: "task-a", From: w.addrs[0], PayTo: signertest.Merchant,
		Amount: "500000", N: 1, Now: w.now, Window: merchant.SettledTimeout, Contract: paycontract.V2}.PO()
	sp, _ := paycontract.SpecOf(paycontract.V2)
	po.Quote, po.Intent.ResourceURL, po.PaymentBinding.AssetContract = w.offer.Quote, w.offer.URL, w.usdc.Hex()
	po.Intent.ResourceHash, _ = sp.ResourceHash("GET", w.offer.URL)
	po.Intent.PaymentRequirementHash, _ = sp.RequirementHash(w.offer.Quote)
	order, _ := json.Marshal(po)
	sess := contracts.Session{ConsumerID: "consumer-a", AgentID: "agent-a", TaskID: "task-a"}
	ch, ok, err := s.IssueChallenge(sess, order)
	if err != nil || !ok {
		t.Fatalf("challenge: %v %v", ok, err)
	}
	hk, err := creds.HolderKey("consumer-a")
	if err != nil {
		t.Fatal(err)
	}
	_, pk, _ := creds.IssuerPublic()
	pres, err := credential.Present(st.Credential, st.Signature, pk, hk, ch.Request())
	if err != nil {
		t.Fatal(err)
	}
	r, err := s.SubmitV2(sess, order, store.Presented{Presentation: &pres})
	if err != nil || r.Purchase.Decision.PolicyVerdict != "Allow" || r.Purchase.Credential == nil || r.Purchase.Credential.Result != "valid" {
		t.Fatalf("submit: %+v %+v %v", r.Purchase.Decision, r.Purchase.Credential, err)
	}

	if err := w.host.RegisterTask(bg, "task-a", "consumer-a", 5_000_000); err != nil {
		t.Fatal(err)
	}
	pay, _, err := s.StartPayment(store.PaymentStart{ConsumerID: "consumer-a", TaskID: "task-a", PurchaseOrderID: "po-1", DelegationRef: "dlg-a@1"})
	if err != nil {
		t.Fatal(err)
	}
	wk := &Worker{Store: s, Relayer: w.host, Signer: signerclient.New(w.socket, s), Shop: HTTPShop{Client: w.srv.Client()},
		Chain: reconcileToken(w), Clock: w.clock, Margin: 30}
	deadline := time.Now().Add(90 * time.Second)
	for pay.State != "delivered" && time.Now().Before(deadline) {
		if pay, err = wk.Step(bg, pay); err != nil && !errors.Is(err, ErrPending) {
			t.Fatalf("step %s: %v", pay.State, err)
		}
		if pay.State != "delivered" {
			time.Sleep(200 * time.Millisecond)
		}
	}
	if pay.State != "delivered" || pay.SettleTx == "" || balanceOf(t, w.rpc, w.usdc, w.payTo) != 500_000 {
		t.Fatalf("not delivered: %+v", pay)
	}

	if want, _ := po.IntentHash(); po.Intent.Domain != paycontract.DomainV2 || pay.IntentHash != want {
		t.Fatalf("not v2's domain and intent: %s %s", po.Intent.Domain, pay.IntentHash)
	}
	// One intent from the purchase to the receipt; each event's label.
	events, err := s.Events(sess)
	if err != nil {
		t.Fatal(err)
	}
	var got []string
	for _, e := range events {
		if e.Scope.PurchaseOrderID != "" && e.Scope.IntentHash != pay.IntentHash {
			t.Errorf("%s names intent %s, not %s", e.EventType, e.Scope.IntentHash, pay.IntentHash)
		}
		got = append(got, e.EventType+"="+e.EvidenceMode+"/"+e.Source)
	}
	want := "TASK_STARTED=fixture/host DELEGATION_BOUND=fixture/simulated PURCHASE_PROPOSED=fixture/fake_cli CREDENTIAL_VERIFIED=fixture/test_issuer POLICY_DECIDED=fixture/host " +
		"PAYMENT_REQUESTED=fixture/local_anvil BUDGET_RESERVED=fixture/local_anvil PAYMENT_SIGNED=fixture/local_anvil PAYMENT_SUBMITTED=fixture/local_anvil " +
		"PAYMENT_SETTLED=fixture/local_anvil RESOURCE_DELIVERED=fixture/host_merchant RECEIPT_ISSUED=fixture/host_merchant"
	if strings.Join(got, " ") != want {
		t.Fatalf("events:\n got %s\nwant %s", strings.Join(got, " "), want)
	}
}
