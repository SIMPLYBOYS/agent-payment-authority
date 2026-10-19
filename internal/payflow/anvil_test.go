package payflow

// The worker with every real part on a local anvil (P2-a; virtual: one-time
// test keys made here, anvil's public keys for gas, nothing leaves this
// machine): the Store, the budget relayer and its Z2 reservation, the
// Signer over its socket checking that reservation on chain, the settling
// shop over HTTP, and the Reconciler. An Allow is paid and delivered once;
// asked again, nothing more is sent.

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"math/big"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/SIMPLYBOYS/agent-payment-authority/internal/budget"
	"github.com/SIMPLYBOYS/agent-payment-authority/internal/contracts"
	"github.com/SIMPLYBOYS/agent-payment-authority/internal/eth"
	"github.com/SIMPLYBOYS/agent-payment-authority/internal/eth/ethtest"
	"github.com/SIMPLYBOYS/agent-payment-authority/internal/merchant"
	"github.com/SIMPLYBOYS/agent-payment-authority/internal/paycontract"
	"github.com/SIMPLYBOYS/agent-payment-authority/internal/policy"
	"github.com/SIMPLYBOYS/agent-payment-authority/internal/reconcile"
	"github.com/SIMPLYBOYS/agent-payment-authority/internal/signer"
	"github.com/SIMPLYBOYS/agent-payment-authority/internal/signer/signertest"
	"github.com/SIMPLYBOYS/agent-payment-authority/internal/signerclient"
	"github.com/SIMPLYBOYS/agent-payment-authority/internal/store"
	"github.com/decred/dcrd/dcrec/secp256k1/v4"
)

func balanceOf(t *testing.T, c *eth.Client, token, who eth.Address) uint64 {
	t.Helper()
	data, _ := eth.Call("balanceOf(address)", who)
	out, err := c.Call(bg, eth.Msg{To: token, Data: data})
	if err != nil {
		t.Fatal(err)
	}
	return new(big.Int).SetBytes(out).Uint64()
}

func TestPaidOnAnvil(t *testing.T) {
	rpc := ethtest.Anvil(t)
	verifier := ethtest.Deploy(t, rpc, "Verifier.sol/Verifier")
	ctor, _ := eth.Encode(verifier, ethtest.Key(t, 1).Addr, ethtest.Key(t, 2).Addr)
	contract := ethtest.Deploy(t, rpc, "Budget.sol/Budget", ctor...)
	usdc := ethtest.Deploy(t, rpc, "TestUSDC.sol/TestUSDC")
	zkb := filepath.Join(t.TempDir(), "zkbudget")
	build := exec.Command("go", "build", "-o", zkb, "./cmd/zkbudget")
	build.Dir = filepath.Join(ethtest.Root(), "zk")
	if out, err := build.CombinedOutput(); err != nil {
		t.Fatalf("build zkbudget: %v %s", err, out)
	}
	clock := func() uint64 { return uint64(time.Now().Unix()) }
	now := clock()

	// The two payers: one-time test keys, wallet-a funded with test USDC.
	keys := map[string]*secp256k1.PrivateKey{}
	var addrs []string
	for _, x := range []string{"a", "b"} {
		b := sha256.Sum256([]byte("payflow anvil test payer " + x))
		k := secp256k1.PrivKeyFromBytes(b[:])
		addr := signer.Address(k.PubKey())
		keys[addr] = k
		addrs = append(addrs, addr)
	}
	payer, _ := eth.ParseAddress(addrs[0])
	payTo, _ := eth.ParseAddress(signertest.Merchant)
	mint, _ := eth.Call("mint(address,uint256)", payer, uint64(5_000_000))
	ethtest.Mine(t, rpc, ethtest.Key(t, 0), &eth.Tx{To: &usdc, Gas: 200_000, Data: mint})

	// The Signer on its socket, checking reservations on this chain.
	sockDir, err := os.MkdirTemp("", "pf") // a socket path is at most 104 bytes
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.RemoveAll(sockDir) })
	socket := filepath.Join(sockDir, "s")
	raw := strings.ReplaceAll(string(signertest.Config(socket, uint32(os.Getuid()), now, addrs[0], addrs[1])), signertest.Asset, usdc.Hex())
	scfg, err := signer.ParseConfig([]byte(raw))
	if err != nil {
		t.Fatal(err)
	}
	core, err := signer.NewCore(bg, scfg, keys, filepath.Join(t.TempDir(), "signer.db"), clock,
		&budget.Verifier{RPC: rpc, ChainID: ethtest.Chain, Contract: contract, Confirmations: 1})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { core.Close() })
	ln, err := net.Listen("unix", socket)
	if err != nil {
		t.Skip("no Unix socket listening here:", err)
	}
	ctx, stop := context.WithCancel(bg)
	served := make(chan error, 1)
	go func() { served <- signer.Serve(ctx, ln, core, func(string) {}) }()
	t.Cleanup(func() { stop(); <-served })

	// The relayer and the settling shop, anvil's public keys for gas.
	relayer, err := budget.Open(bg, budget.Config{ChainID: ethtest.Chain, Contract: contract, Confirmations: 1, PolicyTTL: 600, Zkbudget: zkb,
		SetupDir: filepath.Join(ethtest.Root(), "zk/setup")}, budget.Keys{Gas: ethtest.Key(t, 3), Issuer: ethtest.Key(t, 1), Policy: ethtest.Key(t, 2)},
		rpc, filepath.Join(t.TempDir(), "relayer.db"), clock)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { relayer.Close() })
	settler, err := merchant.OpenSettler(bg, merchant.SettleConfig{ChainID: ethtest.Chain, Token: usdc, TokenName: "USDC", TokenVersion: "2", PayTo: payTo,
		Confirmations: 1}, ethtest.Key(t, 4), rpc, filepath.Join(t.TempDir(), "settle.db"), clock)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { settler.Close() })
	mux := http.NewServeMux()
	srv := httptest.NewServer(mux)
	defer srv.Close()
	shop, err := merchant.NewSettled(srv.URL, settler)
	if err != nil {
		t.Fatal(err)
	}
	shop.Register(mux)
	offer := shop.Offers()[0] // kr-market, 500000

	// The Store: the registry's wallet-a is the test payer, its asset the
	// token deployed here; the offer book is the shop's.
	var reg contracts.RegistryConfig
	readJSON(t, "testdata/contracts/registry.json", &reg)
	reg.Wallets[0].From, reg.Assets[0].AssetContract = addrs[0], usdc.Hex()
	var p policy.Profile
	readJSON(t, "testdata/policy/m1-demo-profile.json", &p)
	p.AllowMax = "500000" // kr-market is an Allow here
	k := store.OfferKey{Method: offer.Method, URL: offer.URL}
	dir := filepath.Join(t.TempDir(), "db")
	os.Mkdir(dir, 0o700)
	s, err := store.Open(store.Config{Path: filepath.Join(dir, "apa.db"), Registry: reg, Offers: store.OfferBook{k: {offer.Quote}}, Clock: clock})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { s.Close() })
	d, _, err := s.ConfirmDelegation(store.DelegationRequest{RequestID: "dlg-req-task-a-000000000", ConsumerID: "consumer-a", Scenario: "pay", Purpose: "a brief",
		AssetID: p.AssetID, Merchants: p.Merchants, TaskBudget: "5000000", AllowMax: "500000", ApprovalDeadlineSeconds: 30, ValidSeconds: 600,
		ConfirmedBy: "simulated:consumer-a:host-test"})
	if err != nil {
		t.Fatal(err)
	}
	if err := s.StartTask(store.TaskSetup{TaskID: "task-a", Profile: &p, Credential: &policy.Credential{ConsumerID: "consumer-a", ValidUntil: now + 86400, EvidenceMode: "fixture"},
		ContractVersion: paycontract.Version, DelegationID: d.DelegationID}); err != nil {
		t.Fatal(err)
	}
	po := signertest.Order{Consumer: "consumer-a", Agent: "agent-a", Wallet: "wallet-a", Task: "task-a", From: addrs[0], PayTo: signertest.Merchant,
		Amount: "500000", N: 1, Now: now, Window: merchant.SettledTimeout}.PO()
	sp, _ := paycontract.SpecOf(paycontract.Version)
	po.Quote, po.Intent.ResourceURL, po.PaymentBinding.AssetContract = offer.Quote, offer.URL, usdc.Hex()
	po.Intent.ResourceHash, _ = sp.ResourceHash("GET", offer.URL)
	po.Intent.PaymentRequirementHash, _ = sp.RequirementHash(offer.Quote)
	order, _ := json.Marshal(po)
	r, err := s.Submit(contracts.Session{ConsumerID: "consumer-a", AgentID: "agent-a", TaskID: "task-a"}, order)
	if err != nil || r.Purchase.Decision.PolicyVerdict != "Allow" {
		t.Fatalf("submit: %+v %v", r.Purchase.Decision, err)
	}

	// H-D4: the task's budget registered; then the payment, end to end.
	if err := relayer.RegisterTask(bg, "task-a", "consumer-a", 5_000_000); err != nil {
		t.Fatal(err)
	}
	pay, _, err := s.StartPayment(store.PaymentStart{ConsumerID: "consumer-a", TaskID: "task-a", PurchaseOrderID: "po-1", DelegationRef: "dlg-a@1"})
	if err != nil {
		t.Fatal(err)
	}
	token := reconcile.Token{RPC: rpc, Address: usdc, Confirmations: 1}
	wk := &Worker{Store: s, Relayer: relayer, Signer: signerclient.New(socket, s), Shop: HTTPShop{Client: srv.Client()}, Chain: token,
		Clock: clock, Margin: 30}
	deadline := time.Now().Add(90 * time.Second)
	for pay.State != "delivered" && time.Now().Before(deadline) {
		if pay, err = wk.Step(bg, pay); err != nil && !errors.Is(err, ErrPending) {
			t.Fatalf("step %s: %v", pay.State, err)
		}
		if pay.State != "delivered" {
			time.Sleep(200 * time.Millisecond)
		}
	}
	if pay.State != "delivered" || pay.SettleTx == "" {
		t.Fatalf("not delivered: %+v", pay)
	}
	if got := balanceOf(t, rpc, usdc, payTo); got != 500_000 {
		t.Fatalf("payTo holds %d", got)
	}
	if got := balanceOf(t, rpc, usdc, payer); got != 4_500_000 {
		t.Fatalf("the payer holds %d", got)
	}
	// Asked again, nothing more is sent: the same balances, no unauthorized spend.
	if again, err := wk.Step(bg, pay); err != nil || again.State != "delivered" || balanceOf(t, rpc, usdc, payTo) != 500_000 {
		t.Fatalf("again: %+v %v", again, err)
	}
	// The reconciliation (AE1 acceptance 18): every Transfer out of the
	// payer is accounted for by an authorization the Store recorded.
	known, err := Known(s)
	if err != nil || len(known) != 1 {
		t.Fatalf("known: %v %v", known, err)
	}
	rep, err := token.Report(bg, "local_anvil", ethtest.Chain, []eth.Address{payer}, known, clock())
	if err != nil || rep.Version != reconcile.ReportVersion || rep.Known != 1 || len(rep.Unauthorized) != 0 || rep.ToBlock == 0 {
		t.Fatalf("reconciliation: %+v %v", rep, err)
	}
	if raw, _ := json.Marshal(rep); !strings.Contains(string(raw), `"unauthorized":[]`) {
		t.Fatalf("an empty list, never null: %s", raw)
	}
	if _, err := token.Report(bg, "local_anvil", ethtest.Chain, nil, known, clock()); err == nil {
		t.Fatal("a report of no wallet")
	}
	ahead := token
	ahead.FromBlock = rep.ToBlock + 1000 // a chain behind the token's deployment (a restarted anvil)
	if _, err := ahead.Report(bg, "local_anvil", ethtest.Chain, []eth.Address{payer}, known, clock()); err == nil {
		t.Fatal("a report of no block")
	}
	// A transfer the payer's key makes itself, no Signer: it is unauthorized.
	pk, _ := eth.NewKey(func() []byte { b := sha256.Sum256([]byte("payflow anvil test payer a")); return b[:] }())
	ethtest.RPC(t, rpc, "anvil_setBalance", pk.Addr.Hex(), "0xde0b6b3a7640000")
	stray, _ := eth.Call("transfer(address,uint256)", ethtest.Key(t, 4).Addr, uint64(1000))
	ethtest.Mine(t, rpc, pk, &eth.Tx{To: &usdc, Gas: 100_000, Data: stray})
	rep, err = token.Report(bg, "local_anvil", ethtest.Chain, []eth.Address{payer}, known, clock())
	if err != nil || len(rep.Unauthorized) != 1 || rep.Unauthorized[0].Value != "1000" || rep.Unauthorized[0].Nonce != "" {
		t.Fatalf("a transfer no authorization made: %+v %v", rep, err)
	}
}
