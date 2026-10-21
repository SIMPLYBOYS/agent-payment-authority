package payflow

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/SIMPLYBOYS/agent-payment-authority/internal/contracts"
	"github.com/SIMPLYBOYS/agent-payment-authority/internal/credential"
	"github.com/SIMPLYBOYS/agent-payment-authority/internal/e1evidence"
	"github.com/SIMPLYBOYS/agent-payment-authority/internal/eth"
	"github.com/SIMPLYBOYS/agent-payment-authority/internal/eth/ethtest"
	"github.com/SIMPLYBOYS/agent-payment-authority/internal/merchant"
	"github.com/SIMPLYBOYS/agent-payment-authority/internal/paycontract"
	"github.com/SIMPLYBOYS/agent-payment-authority/internal/policy"
	"github.com/SIMPLYBOYS/agent-payment-authority/internal/signer/signertest"
	"github.com/SIMPLYBOYS/agent-payment-authority/internal/signerclient"
	"github.com/SIMPLYBOYS/agent-payment-authority/internal/store"
	"github.com/SIMPLYBOYS/agent-payment-authority/internal/verification"
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
	dbPath := filepath.Join(dir, "apa.db")
	s, err := store.Open(store.Config{Path: dbPath, Registry: reg, Offers: store.OfferBook{k: {w.offer.Quote}}, Clock: w.clock,
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

	// E1-a's start, before anything is registered or paid (§4.7).
	hostDir := t.TempDir()
	os.WriteFile(filepath.Join(hostDir, "payments.json"), []byte(`{"environment":"local_anvil","chain_id":`+strconv.FormatUint(ethtest.Chain, 10)+
		`,"token":"`+w.usdc.Hex()+`","pay_to":"`+w.payTo.Hex()+`","budget_contract":"`+w.budget.Hex()+`"}`), 0o600)
	regRaw, _ := json.Marshal(reg)
	os.WriteFile(filepath.Join(hostDir, "registry.json"), regRaw, 0o600)
	start, err := e1evidence.Start(bg, w.rpc, hostDir)
	if err != nil || start.Verifier != w.verify.Hex() {
		t.Fatalf("start %+v %v", start, err)
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

	// E1-a's pack of this run, checked with the chain itself: every claim holds.
	s.Close()
	if _, err := e1evidence.Start(bg, w.rpc, hostDir); err == nil {
		t.Error("a start after task-a was registered")
	}
	if start.ToBlock, err = w.rpc.BlockNumber(bg); err != nil {
		t.Fatal(err)
	}
	packDir := filepath.Join(t.TempDir(), "pack")
	if _, err := e1evidence.Pack(e1evidence.PackInput{DB: dbPath, Start: start, Tasks: []verification.E1aTask{{TaskID: "task-a", Scenario: "e1a-ask"}}}, packDir); err != nil {
		t.Fatal(err)
	}
	rep, err := verification.VerifyE1a(packDir, ethtest.Root(), w.rpc)
	if err != nil || rep.Verdict != verification.Pass {
		t.Fatalf("the pack: %v %+v", err, rep)
	}
	// What the chain alone can catch: another start, and a spend the Host never made.
	restart := func(change func(*verification.E1aStart)) string {
		dir := filepath.Join(t.TempDir(), "pack")
		if _, err := e1evidence.Pack(e1evidence.PackInput{DB: dbPath, Start: start, Tasks: []verification.E1aTask{{TaskID: "task-a", Scenario: "e1a-ask"}}}, dir); err != nil {
			t.Fatal(err)
		}
		var m verification.E1aManifest
		raw, _ := os.ReadFile(filepath.Join(dir, "manifest.json"))
		json.Unmarshal(raw, &m)
		change(&m.Start)
		raw, _ = json.Marshal(m)
		os.WriteFile(filepath.Join(dir, "manifest.json"), raw, 0o600)
		sum := sha256.Sum256(raw)
		end, _ := json.Marshal(verification.E1aEnd{RunID: m.RunID, State: "complete", ManifestSHA256: hex.EncodeToString(sum[:])})
		os.WriteFile(filepath.Join(dir, "run-end.json"), end, 0o600)
		return dir
	}
	for name, c := range map[string]struct {
		change func(*verification.E1aStart)
		claim  string
	}{
		"another verifier": {func(s *verification.E1aStart) { s.Verifier = w.payTo.Hex() }, "E4"},
		"another chain":    {func(s *verification.E1aStart) { s.ChainID = ethtest.Chain + 1 }, "E4"},
		"another budget":   {func(s *verification.E1aStart) { s.BudgetContract = w.usdc.Hex() }, "E4"},
	} {
		r, err := verification.VerifyE1a(restart(c.change), ethtest.Root(), w.rpc)
		if err != nil || r.Claims[c.claim] != verification.Fail {
			t.Errorf("%s: %s is %v (%v)", name, c.claim, r.Claims[c.claim], err)
		}
	}
	// The payer's key moves funds itself, no Signer, no purchase: unauthorized (E8).
	payerKey, _ := eth.NewKey(func() []byte { b := sha256.Sum256([]byte("payflow anvil test payer a")); return b[:] }())
	ethtest.RPC(t, w.rpc, "anvil_setBalance", payerKey.Addr.Hex(), "0xde0b6b3a7640000")
	stray, _ := eth.Call("transfer(address,uint256)", ethtest.Key(t, 4).Addr, uint64(1000))
	ethtest.Mine(t, w.rpc, payerKey, &eth.Tx{To: &w.usdc, Gas: 100_000, Data: stray})
	if r, _ := verification.VerifyE1a(packDir, ethtest.Root(), w.rpc); r.Verdict != verification.Pass {
		t.Fatalf("a transfer after the pack's end block counted: %+v", r.Findings)
	}
	later := restart(func(s *verification.E1aStart) { s.ToBlock, _ = w.rpc.BlockNumber(bg) })
	if r, _ := verification.VerifyE1a(later, ethtest.Root(), w.rpc); r.Claims["E8"] != verification.Fail || r.Claims["E4"] != verification.Pass {
		t.Fatalf("a transfer no purchase made: E8 %s, E4 %s", r.Claims["E8"], r.Claims["E4"])
	}
	before := restart(func(s *verification.E1aStart) { s.FromBlock = s.ToBlock })
	if r, _ := verification.VerifyE1a(before, ethtest.Root(), w.rpc); r.Claims["E8"] != verification.Fail {
		t.Fatalf("blocks that do not hold the transactions: E8 %s", r.Claims["E8"])
	}
}
