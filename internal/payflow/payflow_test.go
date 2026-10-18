package payflow

// The worker against the real Store and stand-ins for the relayer, the
// Signer, the shop and the chain: every path of docs/commercial-p2-host.md
// 3 and 7, and never a second payment.

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/SIMPLYBOYS/agent-payment-authority/internal/budget"
	"github.com/SIMPLYBOYS/agent-payment-authority/internal/contracts"
	"github.com/SIMPLYBOYS/agent-payment-authority/internal/merchant"
	"github.com/SIMPLYBOYS/agent-payment-authority/internal/paycontract"
	"github.com/SIMPLYBOYS/agent-payment-authority/internal/policy"
	"github.com/SIMPLYBOYS/agent-payment-authority/internal/reconcile"
	"github.com/SIMPLYBOYS/agent-payment-authority/internal/signer"
	"github.com/SIMPLYBOYS/agent-payment-authority/internal/signer/signertest"
	"github.com/SIMPLYBOYS/agent-payment-authority/internal/store"
	"github.com/SIMPLYBOYS/agent-payment-authority/internal/x402"
)

const t0 = 1790726450

var bg = context.Background()

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

// world is a real Store whose task-a is apa.pay.v1 with allowed purchases
// po-1..po-n, a clock, and the stand-ins.
type world struct {
	t                            *testing.T
	askSession, askIntent, askPO string
	s                            *store.Store
	now                          uint64
	relayer                      *fakeRelayer
	signer                       *fakeSigner
	shop                         *fakeShop
	chain                        *fakeChain
}

func newWorld(t *testing.T, n int) *world {
	t.Helper()
	w := &world{t: t, now: t0, relayer: &fakeRelayer{state: map[string]string{}}, signer: &fakeSigner{calls: map[string]int{}},
		shop: &fakeShop{}, chain: &fakeChain{state: "pending"}}
	var reg contracts.RegistryConfig
	readJSON(t, "testdata/contracts/registry.json", &reg)
	var p policy.Profile
	readJSON(t, "testdata/policy/m1-demo-profile.json", &p)
	offers := store.OfferBook{}
	var orders []signertest.Order
	for i := range n + 1 { // the last: an Ask, over allow_max
		amount := map[bool]string{true: "500000", false: "50000"}[i == n]
		o := signertest.Order{Consumer: "consumer-a", Agent: "agent-a", Wallet: "wallet-a", Task: "task-a", From: reg.Wallets[0].From,
			PayTo: signertest.Merchant, Amount: amount, N: i + 1, Now: t0, Window: 300}
		k := store.OfferKey{Method: "GET", URL: signertest.URL}
		offers[k] = append(offers[k], o.Quote())
		orders = append(orders, o)
	}
	dir := filepath.Join(t.TempDir(), "db")
	os.Mkdir(dir, 0o700)
	s, err := store.Open(store.Config{Path: filepath.Join(dir, "apa.db"), Registry: reg, Offers: offers, Clock: func() uint64 { return w.now }})
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
	for i, o := range orders {
		r, err := s.Submit(contracts.Session{ConsumerID: "consumer-a", AgentID: "agent-a", TaskID: "task-a"}, o.Raw())
		want := map[bool]string{true: "Ask", false: "Allow"}[i == n]
		if err != nil || r.Purchase.Decision.PolicyVerdict != want {
			t.Fatalf("submit %d: %+v %v", o.N, r.Purchase.Decision, err)
		}
		if i == n {
			w.askSession, w.askIntent, w.askPO = r.Purchase.Approval.SessionID, r.Purchase.IntentHash, r.Purchase.PurchaseOrderID
		}
	}
	w.s = s
	return w
}

// approveAsk has a person approve the Ask (a verified webauthn approval, hap-1).
func (w *world) approveAsk() {
	w.t.Helper()
	if _, err := w.s.BeginApprovalAttempt(store.AttemptRequest{ConsumerID: "consumer-a", SessionID: w.askSession, RequestID: "att-req-00000001", Decision: "approve"}); err != nil {
		w.t.Fatal(err)
	}
	for _, u := range []store.AttemptUpdate{{From: []string{"requested"}, State: "challenged", ChallengeID: "chl-1"},
		{From: []string{"challenged"}, State: "verifying", VerifyRequestID: "ver-1"}} {
		u.ConsumerID, u.RequestID = "consumer-a", "att-req-00000001"
		if _, err := w.s.UpdateApprovalAttempt(u); err != nil {
			w.t.Fatal(err)
		}
	}
	r, err := w.s.ResolveWebAuthn(store.WebAuthnResult{ConsumerID: "consumer-a", RequestID: "att-req-00000001", Decision: "approve", SignerApprovalID: "hap-1",
		ApproverID: "approver-a", CredentialRef: "sha256:" + strings.Repeat("c", 64), IntentHash: w.askIntent, ExpiresAt: t0 + 300})
	if err != nil || r.Outcome != "approved" {
		w.t.Fatalf("approve: %+v %v", r, err)
	}
}

func (w *world) worker() *Worker {
	return &Worker{Store: w.s, Relayer: w.relayer, Signer: w.signer, Shop: w.shop, Chain: w.chain, Clock: func() uint64 { return w.now }, Margin: 10}
}

func (w *world) start(po string) store.Payment {
	w.t.Helper()
	p, _, err := w.s.StartPayment(store.PaymentStart{ConsumerID: "consumer-a", TaskID: "task-a", PurchaseOrderID: po, DelegationRef: "dlg-a@1"})
	if err != nil {
		w.t.Fatal(err)
	}
	return p
}

func (w *world) payment(po string) store.Payment {
	w.t.Helper()
	p, err := w.s.PaymentOf("consumer-a", "task-a", po)
	if err != nil {
		w.t.Fatal(err)
	}
	return p
}

// fakeRelayer reserves once per intent; reserveErr is its next Reserve's
// error; lose makes the next Reserve reserve and lose the answer.
type fakeRelayer struct {
	state      map[string]string
	reserves   int // reservations made (tx#1 sent)
	calls      int
	reserveErr error
	lookupErr  error
	lose       bool
}

func (r *fakeRelayer) evidence(intent string) budget.Evidence {
	return budget.Evidence{ChainID: 84532, IntentHash: intent, Amount: 50000, TxHash: "0x" + strings.Repeat("1", 64), BlockNumber: 7}
}

func (r *fakeRelayer) Reserve(_ context.Context, order json.RawMessage) (budget.Evidence, error) {
	r.calls++
	var o paycontract.PurchaseOrder
	contracts.Decode(order, &o)
	intent, _ := o.IntentHash()
	if err := r.reserveErr; err != nil {
		r.reserveErr = nil
		return budget.Evidence{}, err
	}
	if r.state[intent] == "" {
		r.reserves++
		r.state[intent] = "reserved"
	}
	if r.lose {
		r.lose = false
		return budget.Evidence{}, errors.New("connection reset")
	}
	return r.evidence(intent), nil
}

func (r *fakeRelayer) Lookup(_ context.Context, intent string) (budget.Evidence, string, error) {
	if err := r.lookupErr; err != nil {
		r.lookupErr = nil
		return budget.Evidence{}, "", err
	}
	if r.state[intent] == "reserved" {
		return r.evidence(intent), "reserved", nil
	}
	return budget.Evidence{}, r.state[intent], nil
}

// fakeSigner signs the order's binding; err is its next answer's error,
// state its next answer's state (unsigned), other makes it sign another nonce.
type fakeSigner struct {
	calls map[string]int
	last  signer.SignRequest
	err   error
	state string
	other bool
}

func (f *fakeSigner) Sign(_ context.Context, r signer.SignRequest) (signer.Reply, error) {
	f.calls[r.RequestID]++
	f.last = r
	if err := f.err; err != nil {
		f.err = nil
		return signer.Reply{}, err
	}
	if st := f.state; st != "" {
		f.state = ""
		return signer.Reply{State: st}, nil
	}
	var o paycontract.PurchaseOrder
	contracts.Decode(r.Order, &o)
	b := o.PaymentBinding
	if f.other {
		b.Nonce = "0x" + strings.Repeat("9", 64)
	}
	return signer.Reply{State: "signed", Authorization: &signer.SignedAuthorization{Transfer: signer.Transfer{From: b.From, To: b.PayTo, Value: string(b.Amount),
		ValidAfter: b.ValidAfter, ValidBefore: b.ValidBefore, Nonce: b.Nonce}, Signature: "0x" + strings.Repeat("ab", 65)}}, nil
}

// fakeShop answers from a script, then repeats its last answer; it keeps
// every PAYMENT-SIGNATURE it was sent.
type fakeShop struct {
	answers []ShopAnswer
	err     error
	sent    []string
}

func (f *fakeShop) Pay(_ context.Context, _, sig string) (ShopAnswer, error) {
	f.sent = append(f.sent, sig)
	if err := f.err; err != nil {
		f.err = nil
		return ShopAnswer{}, err
	}
	a := f.answers[0]
	if len(f.answers) > 1 {
		f.answers = f.answers[1:]
	}
	return a, nil
}

var (
	paid     = ShopAnswer{Status: 200, Response: &merchant.SettlementResponse{Success: true, Transaction: "0x" + strings.Repeat("2", 64), Network: "eip155:84532"}, Body: []byte(`{"canary":"APA-PROTECTED"}`)}
	pending  = ShopAnswer{Status: 202}
	verify   = ShopAnswer{Status: 402, Response: &merchant.SettlementResponse{ErrorReason: "insufficient_funds"}}
	settleKO = ShopAnswer{Status: 402, Response: &merchant.SettlementResponse{ErrorReason: "settlement_failed"}}
)

type fakeChain struct {
	state string
	err   error
}

func (f *fakeChain) Status(context.Context, reconcile.Authorization) (reconcile.Result, error) {
	if f.err != nil {
		return reconcile.Result{}, f.err
	}
	r := reconcile.Result{State: f.state, Block: 9}
	r.Tx[0] = 0x22
	return r, nil
}

func (w *world) step(po string) (store.Payment, error) {
	w.t.Helper()
	return w.worker().Step(bg, w.payment(po))
}

func (w *world) oneSignature() {
	w.t.Helper()
	for _, s := range w.shop.sent {
		if s != w.shop.sent[0] {
			w.t.Fatal("two different PAYMENT-SIGNATUREs for one payment")
		}
	}
}

// An Allow paid end to end in one step: reserved once, signed once under
// the delegation, sent, settled once the chain confirms, delivered.
func TestPaidEndToEnd(t *testing.T) {
	w := newWorld(t, 1)
	w.start("po-1")
	w.shop.answers = []ShopAnswer{paid}
	w.chain.state = "settled"
	p, err := w.step("po-1")
	if err != nil || p.State != "delivered" {
		t.Fatalf("step %+v %v", p, err)
	}
	if w.relayer.reserves != 1 || len(w.signer.calls) != 1 || len(w.shop.sent) != 1 {
		t.Fatalf("reserves %d, sign requests %d, sent %d", w.relayer.reserves, len(w.signer.calls), len(w.shop.sent))
	}
	if a := w.signer.last.Authorization; a.Method != "delegation" || a.DelegationID != "dlg-a" || a.DelegationVersion != 1 || len(w.signer.last.Reservation) == 0 {
		t.Fatalf("sign request %+v", w.signer.last)
	}
	if again, err := w.step("po-1"); err != nil || again.State != "delivered" || w.relayer.calls != 1 {
		t.Fatalf("a delivered payment moved: %+v %v", again, err)
	}
}

// The shop's 200 alone is not "paid": nothing is settled until the chain
// confirms; then the same signature again delivers.
func TestPaidOnlyWhenTheChainSaysSo(t *testing.T) {
	w := newWorld(t, 1)
	w.start("po-1")
	w.shop.answers = []ShopAnswer{paid}
	if p, err := w.step("po-1"); !errors.Is(err, ErrPending) || p.State != "submitted" {
		t.Fatalf("unconfirmed: %+v %v", p, err)
	}
	w.chain.state = "settled"
	if p, err := w.step("po-1"); err != nil || p.State != "delivered" {
		t.Fatalf("confirmed: %+v %v", p, err)
	}
	w.oneSignature()
}

// Lost answers resume the same payment: a reservation whose answer was lost
// is found by Lookup (never a second tx#1); a Signer answer lost is asked
// again with the same request ID; a shop answer lost is unknown, sent again
// with the same signature.
func TestLostAnswersNeverPayTwice(t *testing.T) {
	w := newWorld(t, 1)
	w.start("po-1")
	w.relayer.lose = true
	if p, err := w.step("po-1"); !errors.Is(err, ErrPending) || p.State != "requested" {
		t.Fatalf("lost reservation answer: %+v %v", p, err)
	}
	w.signer.err = errors.New("socket closed")
	if p, err := w.step("po-1"); !errors.Is(err, ErrPending) || p.State != "reserved" || w.relayer.reserves != 1 || w.relayer.calls != 1 {
		t.Fatalf("after the lookup: %+v %v, reserves %d calls %d", p, err, w.relayer.reserves, w.relayer.calls)
	}
	w.shop.err = errors.New("timeout")
	if p, err := w.step("po-1"); !errors.Is(err, ErrPending) || p.State != "unknown" {
		t.Fatalf("lost shop answer: %s %v", p.State, err)
	}
	w.shop.answers = []ShopAnswer{paid}
	w.chain.state = "settled"
	if p, err := w.step("po-1"); err != nil || p.State != "delivered" {
		t.Fatalf("again: %+v %v", p, err)
	}
	for id, n := range w.signer.calls {
		if n != 2 || id != SignerRequestID(w.payment("po-1").IntentHash) {
			t.Fatalf("sign requests %v", w.signer.calls)
		}
	}
	w.oneSignature()
	if w.relayer.reserves != 1 {
		t.Fatalf("%d reservations", w.relayer.reserves)
	}
}

// A restart with a payment submitted (the Host stopped right after
// recording it): unknown, then moved by the chain with the same signature.
func TestRecoverSubmitted(t *testing.T) {
	w := newWorld(t, 1)
	p := w.start("po-1")
	ev, _ := json.Marshal(w.relayer.evidence(p.IntentHash))
	w.relayer.state[p.IntentHash] = "reserved"
	w.s.PaymentReserved(p.IntentHash, store.Reservation{Evidence: ev, TxHash: "0x" + strings.Repeat("1", 64), Block: 7})
	reply, _ := w.signer.Sign(bg, signer.SignRequest{Order: p.Order})
	auth, _ := json.Marshal(reply.Authorization)
	w.s.PaymentSigned(p.IntentHash, auth)
	if _, err := w.s.PaymentSubmitted(p.IntentHash); err != nil {
		t.Fatal(err)
	}
	if err := w.worker().Recover(bg); err != nil {
		t.Fatal(err)
	}
	if got := w.payment("po-1"); got.State != "unknown" {
		t.Fatalf("after recover: %s", got.State)
	}
	w.shop.answers = []ShopAnswer{paid}
	w.chain.state = "settled"
	if got, err := w.step("po-1"); err != nil || got.State != "delivered" || w.relayer.calls != 0 {
		t.Fatalf("resumed: %s %v, relayer calls %d", got.State, err, w.relayer.calls)
	}
}

// The task's end overtakes a payment not yet reserved: TASK_ENDED, the
// relayer never asked to reserve. Once reserved, it is carried through.
func TestTaskEndedBeforeAndAfterTheReservation(t *testing.T) {
	w := newWorld(t, 2)
	w.start("po-1")
	w.start("po-2")
	w.signer.err = signer.Refusal("SIGNER_BUSY")
	w.step("po-2") // reserved, then the Signer is busy
	if err := w.s.Cancel("task-a"); err != nil {
		t.Fatal(err)
	}
	if p, err := w.step("po-1"); err != nil || p.State != "not_paid_ended" || w.relayer.state[p.IntentHash] != "" {
		t.Fatalf("not reserved: %+v %v", p, err)
	}
	w.shop.answers = []ShopAnswer{paid}
	w.chain.state = "settled"
	if p, err := w.step("po-2"); err != nil || p.State != "delivered" {
		t.Fatalf("reserved before the cancel: %+v %v", p, err)
	}
}

// Refusals: the relayer's ends a payment not paid; the Signer's ends one
// reserved for review; the shop's verification failure ends it for review,
// never sent again.
func TestRefusals(t *testing.T) {
	w := newWorld(t, 3)
	for _, po := range []string{"po-1", "po-2", "po-3"} {
		w.start(po)
	}
	w.relayer.reserveErr = signer.Refusal("OVER_BUDGET")
	if p, _ := w.step("po-1"); p.State != "reserve_failed" {
		t.Fatalf("relayer refusal: %+v", p)
	}
	w.signer.err = signer.Refusal("OVER_PER_TX")
	if p, _ := w.step("po-2"); p.State != "needs_review" || p.Code != "RESERVED_NOT_PAID" {
		t.Fatalf("Signer refusal: %+v", p)
	}
	w.shop.answers = []ShopAnswer{verify}
	if p, _ := w.step("po-3"); p.State != "needs_review" || p.Code != "VERIFICATION_FAILED" {
		t.Fatalf("shop verification: %+v", p)
	}
	sent := len(w.shop.sent)
	w.step("po-3")
	if len(w.shop.sent) != sent {
		t.Fatal("sent again after a verification failure")
	}
}

// A settlement not known in time is settled, or ended, only by the chain:
// pending waits (never paid again); expired unused is NOT_SETTLED_EXPIRED;
// a front-run (settlement_failed, paid on chain) is paid and not delivered.
func TestUnknownMovedByTheChain(t *testing.T) {
	w := newWorld(t, 2)
	w.start("po-1")
	w.start("po-2")
	w.shop.answers = []ShopAnswer{pending}
	if p, err := w.step("po-1"); !errors.Is(err, ErrPending) || p.State != "submitted" {
		t.Fatalf("202: %+v %v", p, err)
	}
	w.chain.state = "not_settled_expired"
	if p, _ := w.step("po-1"); p.State != "needs_review" || p.Code != "NOT_SETTLED_EXPIRED" {
		t.Fatalf("expired unused: %+v", p)
	}
	w.shop.answers = []ShopAnswer{settleKO}
	w.chain.state = "settled"
	if p, _ := w.step("po-2"); p.State != "needs_review" || p.Code != "DELIVERY_FAILED" || p.SettleTx == "" {
		t.Fatalf("front-run: %+v", p)
	}
	if v := statusOf(t, w, "po-2"); v != "paid" {
		t.Fatalf("payment_status %s", v)
	}
}

func statusOf(t *testing.T, w *world, po string) string {
	v, err := w.s.Task(contracts.Session{ConsumerID: "consumer-a", AgentID: "agent-a", TaskID: "task-a"})
	if err != nil {
		t.Fatal(err)
	}
	for _, p := range v.Purchases {
		if p.PurchaseOrderID == po && p.Payment != nil {
			return p.Payment.Status
		}
	}
	return ""
}

// Too late to start: not reserved past valid_before - margin; reserved and
// too late to sign is for review.
func TestTooLate(t *testing.T) {
	w := newWorld(t, 2)
	w.start("po-1")
	w.start("po-2")
	w.signer.err = signer.Refusal("SIGNER_BUSY")
	w.step("po-2") // reserved
	w.now = t0 + 300 - 5
	if p, _ := w.step("po-1"); p.State != "not_paid_expired" || w.relayer.calls != 1 {
		t.Fatalf("too late to reserve: %+v, relayer calls %d", p, w.relayer.calls)
	}
	w.signer.err = signer.Refusal("SIGNER_BUSY")
	if p, _ := w.step("po-2"); p.State != "needs_review" || p.Code != "RESERVED_NOT_PAID" {
		t.Fatalf("too late to sign: %+v", p)
	}
}

// An Ask a person approved is paid under that approval: the Signer gets
// its approval ID, never a delegation.
func TestWebAuthnAskPaid(t *testing.T) {
	w := newWorld(t, 0)
	w.approveAsk()
	p, _, err := w.s.StartPayment(store.PaymentStart{ConsumerID: "consumer-a", TaskID: "task-a", PurchaseOrderID: w.askPO})
	if err != nil {
		t.Fatal(err)
	}
	w.shop.answers = []ShopAnswer{paid}
	w.chain.state = "settled"
	if got, err := w.worker().Step(bg, p); err != nil || got.State != "delivered" {
		t.Fatalf("step %s %v", got.State, err)
	}
	if a := w.signer.last.Authorization; a.Method != "webauthn" || a.ApprovalID != "hap-1" || a.DelegationID != "" {
		t.Fatalf("sign authorization %+v", a)
	}
}

// Run recovers and moves every open payment until it is stopped.
func TestRun(t *testing.T) {
	w := newWorld(t, 2)
	w.start("po-1")
	w.start("po-2")
	w.shop.answers = []ShopAnswer{paid}
	w.chain.state = "settled"
	wk := w.worker()
	wk.Every = 10 * time.Millisecond
	ctx, cancel := context.WithCancel(bg)
	done := make(chan error, 1)
	go func() { done <- wk.Run(ctx) }()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) && (w.payment("po-1").State != "delivered" || w.payment("po-2").State != "delivered") {
		wk.Kick()
		time.Sleep(10 * time.Millisecond)
	}
	cancel()
	if err := <-done; err != nil {
		t.Fatal(err)
	}
	if w.payment("po-1").State != "delivered" || w.payment("po-2").State != "delivered" || w.relayer.reserves != 2 {
		t.Fatalf("po-1 %s, po-2 %s, reserves %d", w.payment("po-1").State, w.payment("po-2").State, w.relayer.reserves)
	}
}

// The PAYMENT-SIGNATURE is one the real shop reads: for its offer (M1's
// shop answers 402 M1_PAYMENT_UNAVAILABLE, never 400), with a payload the
// settling shop parses.
func TestPaymentSignatureTheShopReads(t *testing.T) {
	mux := http.NewServeMux()
	srv := httptest.NewServer(mux)
	defer srv.Close()
	shop, err := merchant.New(srv.URL)
	if err != nil {
		t.Fatal(err)
	}
	shop.Register(mux)
	offer := shop.Offers()[0]
	o := paycontract.PurchaseOrder{Quote: offer.Quote, Intent: paycontract.Intent{ResourceURL: offer.URL}}
	a := signer.SignedAuthorization{Transfer: signer.Transfer{From: "0x" + strings.Repeat("1", 40), To: signertest.Merchant, Value: "50000",
		ValidAfter: t0 - 1, ValidBefore: t0 + 300, Nonce: "0x" + strings.Repeat("0", 63) + "1"}, Signature: "0x" + strings.Repeat("ab", 65)}
	sig, err := paymentSignature(o, a)
	if err != nil {
		t.Fatal(err)
	}
	ans, err := HTTPShop{Client: srv.Client()}.Pay(bg, offer.URL, sig)
	var body struct{ Error string }
	json.Unmarshal(ans.Body, &body)
	if err != nil || ans.Status != 402 || body.Error != "M1_PAYMENT_UNAVAILABLE" || ans.Response != nil {
		t.Fatalf("shop: %+v %v", ans, err)
	}
	var pp x402.PaymentPayload
	if err := x402.Decode(sig, &pp); err != nil {
		t.Fatal(err)
	}
	if err := merchant.ParsePayment(pp.Payload); err != nil {
		t.Fatal(err)
	}
}

// What the relayer already has decides first: sent waits (never a second
// Reserve), failed ends it, an error waits; a busy task waits, then reserves.
func TestRelayerStates(t *testing.T) {
	w := newWorld(t, 3)
	for _, po := range []string{"po-1", "po-2", "po-3"} {
		w.start(po)
	}
	w.relayer.state[w.payment("po-1").IntentHash] = "sent"
	if p, err := w.step("po-1"); !errors.Is(err, ErrPending) || p.State != "requested" || w.relayer.calls != 0 {
		t.Fatalf("sent: %+v %v, Reserve calls %d", p, err, w.relayer.calls)
	}
	w.relayer.state[w.payment("po-2").IntentHash] = "failed"
	if p, _ := w.step("po-2"); p.State != "reserve_failed" || w.relayer.calls != 0 {
		t.Fatalf("failed: %+v", p)
	}
	w.relayer.lookupErr = errors.New("relayer down")
	if p, err := w.step("po-3"); !errors.Is(err, ErrPending) || p.State != "requested" || w.relayer.calls != 0 {
		t.Fatalf("lookup error: %+v %v", p, err)
	}
	w.relayer.reserveErr = signer.Refusal("TASK_BUSY")
	if p, err := w.step("po-3"); !errors.Is(err, ErrPending) || p.State != "requested" {
		t.Fatalf("busy: %+v %v", p, err)
	}
	w.signer.err = signer.Refusal("SIGNER_BUSY")
	if p, _ := w.step("po-3"); p.State != "reserved" || w.relayer.reserves != 1 {
		t.Fatalf("after busy: %+v, reserves %d", p, w.relayer.reserves)
	}
}

// A Signer attempt closed unsigned, or one signing another authorization,
// is for review, never sent; one still open waits until too late.
func TestSignerAnswers(t *testing.T) {
	w := newWorld(t, 3)
	for _, po := range []string{"po-1", "po-2", "po-3"} {
		w.start(po)
	}
	w.signer.state = "abandoned"
	if p, _ := w.step("po-1"); p.State != "needs_review" || p.Code != "RESERVED_NOT_PAID" {
		t.Fatalf("abandoned: %+v", p)
	}
	w.signer.other = true
	if p, _ := w.step("po-2"); p.State != "needs_review" || p.Code != "RESERVED_NOT_PAID" || len(w.shop.sent) != 0 {
		t.Fatalf("another nonce: %+v, sent %d", p, len(w.shop.sent))
	}
	w.signer.other = false
	w.signer.state = "signing"
	if p, err := w.step("po-3"); !errors.Is(err, ErrPending) || p.State != "reserved" {
		t.Fatalf("open attempt: %+v %v", p, err)
	}
	w.now = t0 + 300 - 5
	w.signer.state = "signing"
	if p, _ := w.step("po-3"); p.State != "needs_review" || p.Code != "RESERVED_NOT_PAID" {
		t.Fatalf("open attempt, too late: %+v", p)
	}
}

// The shop's answers: a 5xx is unknown, a 429 says nothing (the chain
// does), a 400 is a refusal; a nonce the shop calls used is the chain's.
func TestShopAnswers(t *testing.T) {
	w := newWorld(t, 4)
	for _, po := range []string{"po-1", "po-2", "po-3", "po-4"} {
		w.start(po)
	}
	w.shop.answers = []ShopAnswer{{Status: 503}}
	if p, err := w.step("po-1"); !errors.Is(err, ErrPending) || p.State != "unknown" {
		t.Fatalf("503: %+v %v", p, err)
	}
	w.shop.answers = []ShopAnswer{{Status: 429}}
	if p, err := w.step("po-2"); !errors.Is(err, ErrPending) || p.State != "submitted" {
		t.Fatalf("429: %+v %v", p, err)
	}
	w.shop.answers = []ShopAnswer{{Status: 400}}
	if p, _ := w.step("po-3"); p.State != "needs_review" || p.Code != "VERIFICATION_FAILED" {
		t.Fatalf("400: %+v", p)
	}
	w.shop.answers = []ShopAnswer{{Status: 402, Response: &merchant.SettlementResponse{ErrorReason: "invalid_exact_evm_payload_authorization_nonce_used"}}}
	w.chain.state = "settled"
	if p, _ := w.step("po-4"); p.State != "needs_review" || p.Code != "DELIVERY_FAILED" || p.SettleTx == "" {
		t.Fatalf("nonce used, paid: %+v", p)
	}
}

// The chain's answers: an error waits, needs_review is for review.
func TestChainAnswers(t *testing.T) {
	w := newWorld(t, 2)
	w.start("po-1")
	w.start("po-2")
	w.shop.answers = []ShopAnswer{pending}
	w.chain.err = errors.New("node down")
	if p, err := w.step("po-1"); !errors.Is(err, ErrPending) || p.State != "submitted" {
		t.Fatalf("node down: %+v %v", p, err)
	}
	w.chain.err = nil
	w.chain.state = "needs_review"
	if p, _ := w.step("po-2"); p.State != "needs_review" || p.Code != "RECONCILE_NEEDS_REVIEW" {
		t.Fatalf("needs_review: %+v", p)
	}
}

// Settled stays settled: a later disagreeing chain answer waits; the
// content comes on a later round; a shop refusing to deliver is
// DELIVERY_FAILED.
func TestSettledThenDelivered(t *testing.T) {
	w := newWorld(t, 2)
	w.start("po-1")
	w.start("po-2")
	w.shop.answers = []ShopAnswer{pending}
	w.chain.state = "settled"
	for _, po := range []string{"po-1", "po-2"} {
		if p, err := w.step(po); !errors.Is(err, ErrPending) || p.State != "settled" {
			t.Fatalf("%s: %+v %v", po, p, err)
		}
	}
	w.chain.state = "not_settled_expired"
	if p, err := w.step("po-1"); !errors.Is(err, ErrPending) || p.State != "settled" {
		t.Fatalf("disagreeing chain: %+v %v", p, err)
	}
	w.chain.state = "settled"
	w.shop.answers = []ShopAnswer{paid}
	if p, err := w.step("po-1"); err != nil || p.State != "delivered" {
		t.Fatalf("later round: %+v %v", p, err)
	}
	w.shop.answers = []ShopAnswer{verify}
	if p, _ := w.step("po-2"); p.State != "needs_review" || p.Code != "DELIVERY_FAILED" {
		t.Fatalf("402 once settled: %+v", p)
	}
}

// HTTPShop reads PAYMENT-RESPONSE and the body; a body over MaxBody is no answer.
func TestHTTPShop(t *testing.T) {
	size := 10
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get(x402.HeaderPaymentSignature) != "sig" {
			w.WriteHeader(400)
			return
		}
		h, _ := x402.Encode(merchant.SettlementResponse{Success: true, Transaction: "0x" + strings.Repeat("2", 64), Network: "eip155:84532"})
		w.Header().Set(merchant.HeaderPaymentResponse, h)
		w.Write([]byte(strings.Repeat("x", size)))
	}))
	defer srv.Close()
	shop := HTTPShop{Client: srv.Client()}
	ans, err := shop.Pay(bg, srv.URL, "sig")
	if err != nil || ans.Status != 200 || ans.Response == nil || !ans.Response.Success || string(ans.Body) != "xxxxxxxxxx" {
		t.Fatalf("%+v %v", ans, err)
	}
	size = MaxBody + 1
	if _, err := shop.Pay(bg, srv.URL, "sig"); err == nil {
		t.Fatal("a body over MaxBody was an answer")
	}
}
