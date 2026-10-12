package signerclient

// The Host client against an in-process Signer on a temporary Unix socket,
// with one-time test keys, synthetic delegations and a test reservation
// verifier. The ledger is a recording stand-in for the Store's three
// methods (the Store's own tests cover them): the synthetic orders it holds
// are signable by the test keys, which the Store's registry payers are not.

import (
	"bufio"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/SIMPLYBOYS/agent-payment-authority/internal/paycontract"
	"github.com/SIMPLYBOYS/agent-payment-authority/internal/signer"
	"github.com/SIMPLYBOYS/agent-payment-authority/internal/signer/signertest"
	"github.com/SIMPLYBOYS/agent-payment-authority/internal/store"
	"github.com/decred/dcrd/dcrec/secp256k1/v4"
	"github.com/decred/dcrd/dcrec/secp256k1/v4/ecdsa"
)

const t0 = 1790726450

var bg = context.Background()

func testKey(name string) *secp256k1.PrivateKey {
	b := sha256.Sum256([]byte("signerclient test wallet " + name))
	return secp256k1.PrivKeyFromBytes(b[:])
}

var keyA, keyB = testKey("a"), testKey("b")

type row struct {
	store.PaymentAttempt
	consumer string
}

// ledger stands in for the Store: a committed row per purchase, consumer
// scoped reads, and the Store's update rules that matter here.
type ledger struct {
	mu     sync.Mutex
	orders map[string]json.RawMessage // purchase order ID -> order
	rows   map[string]*row            // request ID -> row
	n      int
	fail   error // UpdatePayment fails with it, writing nothing
}

func (l *ledger) BeginPayment(req store.PaymentRequest) (store.PaymentAttempt, bool, error) {
	l.mu.Lock()
	defer l.mu.Unlock()
	for _, r := range l.rows {
		if r.PurchaseOrderID == req.PurchaseOrderID {
			return r.PaymentAttempt, true, nil
		}
	}
	var o paycontract.PurchaseOrder
	json.Unmarshal(l.orders[req.PurchaseOrderID], &o)
	intent, err := o.IntentHash()
	if err != nil {
		return store.PaymentAttempt{}, false, err
	}
	l.n++
	a := store.PaymentAttempt{RequestID: fmt.Sprintf("pay-%032d", l.n), TaskID: req.TaskID, PurchaseOrderID: req.PurchaseOrderID, IntentHash: intent,
		Method: req.Method, DelegationRef: req.DelegationRef, State: "requested", Order: l.orders[req.PurchaseOrderID]}
	l.rows[a.RequestID] = &row{a, req.ConsumerID}
	return a, false, nil
}

func (l *ledger) UpdatePayment(id, state, attempt, code string) (store.PaymentAttempt, error) {
	l.mu.Lock()
	defer l.mu.Unlock()
	r := l.rows[id]
	if l.fail != nil {
		return store.PaymentAttempt{}, l.fail
	}
	if r.SignerAttemptID != "" && attempt != r.SignerAttemptID {
		return r.PaymentAttempt, errors.New("IDEMPOTENCY_CONFLICT")
	}
	r.State, r.SignerAttemptID, r.ErrorCode = state, attempt, code
	return r.PaymentAttempt, nil
}

func (l *ledger) Payment(consumer, id string) (store.PaymentAttempt, error) {
	l.mu.Lock()
	defer l.mu.Unlock()
	if r := l.rows[id]; r != nil && r.consumer == consumer {
		return r.PaymentAttempt, nil
	}
	return store.PaymentAttempt{}, errors.New("UNKNOWN_PAYMENT")
}

// committed is the test reservation verifier, and the handshake that the
// Host committed the request before calling: the Signer finds the row
// already written for the intent it is asked to sign.
type committed func(intent string) bool

func (c committed) Verify(intent string, _ json.RawMessage) error {
	if !c(intent) {
		return signer.Refusal("REQUEST_NOT_COMMITTED")
	}
	return nil
}

// committed is the handshake against the ledger stand-in.
func (l *ledger) committed(intent string) bool {
	l.mu.Lock()
	defer l.mu.Unlock()
	for _, r := range l.rows {
		if r.IntentHash == intent && (r.State == "requested" || r.State == "unknown" || r.State == "rejected") {
			return true
		}
	}
	return false
}

type env struct {
	t      *testing.T
	l      *ledger
	core   *signer.Core
	socket string
}

func newEnv(t *testing.T) *env { return newEnvChecking(t, nil) }

// newEnvChecking takes the commit handshake; nil checks the ledger stand-in.
func newEnvChecking(t *testing.T, check committed) *env {
	t.Helper()
	dir, err := os.MkdirTemp("", "sc") // a socket path is at most 104 bytes
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.RemoveAll(dir) })
	a, b := signer.Address(keyA.PubKey()), signer.Address(keyB.PubKey())
	cfg, err := signer.ParseConfig(signertest.Config(filepath.Join(dir, "s"), uint32(os.Getuid()), t0, a, b))
	if err != nil {
		t.Fatal(err)
	}
	e := &env{t: t, l: &ledger{orders: map[string]json.RawMessage{}, rows: map[string]*row{}}, socket: filepath.Join(dir, "s")}
	if check == nil {
		check = e.l.committed
	}
	if e.core, err = signer.NewCore(bg, cfg, map[string]*secp256k1.PrivateKey{a: keyA, b: keyB}, filepath.Join(dir, "signer.db"),
		func() uint64 { return t0 }, check); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { e.core.Close() })
	for n, amount := range map[int]string{1: "500000", 2: "700000"} {
		e.l.orders[fmt.Sprintf("po-%d", n)] = signertest.Order{Consumer: "consumer-a", Agent: "agent-a", Wallet: "wallet-a", From: a,
			PayTo: signertest.Merchant, Amount: amount, N: n, Now: t0, Window: 60}.Raw()
	}
	return e
}

// serve runs the Signer on ln until the test ends.
func (e *env) serve(ln net.Listener) {
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- signer.Serve(ctx, ln, e.core, func(string) {}) }()
	e.t.Cleanup(func() { cancel(); <-done })
}

func (e *env) listen() {
	ln, err := net.Listen("unix", e.socket)
	if err != nil {
		e.t.Fatal(err)
	}
	e.serve(ln)
}

func pay(po string) store.PaymentRequest {
	return store.PaymentRequest{ConsumerID: "consumer-a", TaskID: "task-consumer-a", PurchaseOrderID: po, Method: "delegation", DelegationRef: "dlg-a@1"}
}

func recovers(t *testing.T, auth *signer.SignedAuthorization, digest string) string {
	t.Helper()
	sig, _ := hex.DecodeString(strings.TrimPrefix(auth.Signature, "0x"))
	d, _ := hex.DecodeString(strings.TrimPrefix(digest, "0x"))
	pub, _, err := ecdsa.RecoverCompact(append([]byte{sig[64]}, sig[:64]...), d)
	if err != nil {
		t.Fatal(err)
	}
	return signer.Address(pub)
}

func TestPay(t *testing.T) {
	e := newEnv(t)
	e.listen()
	c := New(e.socket, e.l)
	r, err := c.Pay(pay("po-1"))
	if err != nil || r.Attempt.State != "signed" || r.Attempt.SignerAttemptID == "" || r.Authorization == nil {
		t.Fatalf("pay: %+v %v", r, err)
	}
	got, _ := e.core.Get(bg, r.Attempt.SignerAttemptID, "")
	if recovers(t, r.Authorization, got.TypedDigest) != signer.Address(keyA.PubKey()) || got.RequestID != r.Attempt.RequestID {
		t.Fatal("the authorization is not wallet-a's for this request")
	}
	again, err := c.Pay(pay("po-1"))
	if err != nil || again.Attempt.RequestID != r.Attempt.RequestID || again.Authorization.Signature != r.Authorization.Signature || len(e.l.rows) != 1 {
		t.Fatalf("pay again: %+v %v", again, err)
	}
	// Over the delegation's per-transaction limit: refused, recorded rejected.
	if r, err := c.Pay(pay("po-2")); err == nil || err.Error() != "OVER_PER_TX" || r.Attempt.State != "rejected" || r.Attempt.ErrorCode != "OVER_PER_TX" {
		t.Fatalf("over the limit: %+v %v", r, err)
	}
	// Abandon after signing: refused, the row stays signed.
	if r, err := c.Abandon("consumer-a", r.Attempt.RequestID); err == nil || err.Error() != "ATTEMPT_NOT_ACCEPTED" || r.Attempt.State != "signed" {
		t.Fatalf("abandon a signed attempt: %+v %v", r, err)
	}
	// Consumer scope: B cannot resume or abandon A's request.
	for _, f := range []func(string, string) (Result, error){c.Resume, c.Abandon} {
		if _, err := f("consumer-b", r.Attempt.RequestID); err == nil || err.Error() != "UNKNOWN_PAYMENT" {
			t.Fatalf("B reached A's attempt: %v", err)
		}
	}
}

// No Signer listening: unknown, never a new ID; once it serves, the same
// request ID is resolved and then signed.
func TestUnknownThenResume(t *testing.T) {
	e := newEnv(t)
	c := New(e.socket, e.l)
	r, err := c.Pay(pay("po-1"))
	if !errors.Is(err, Unknown) || errors.Is(err, NotSaved) || r.Attempt.State != "unknown" || e.l.rows[r.Attempt.RequestID].State != "unknown" {
		t.Fatalf("no Signer: %+v %v", r, err)
	}
	id := r.Attempt.RequestID
	e.listen()
	if r, err := c.Resume("consumer-a", id); err == nil || r.Attempt.State != "rejected" || r.Attempt.ErrorCode != "ATTEMPT_UNKNOWN" {
		t.Fatalf("resume of a request the Signer never saw: %+v %v", r, err)
	}
	if r, err := c.Pay(pay("po-1")); err != nil || r.Attempt.RequestID != id || r.Attempt.State != "signed" {
		t.Fatalf("pay after: %+v %v", r, err)
	}
	if len(e.l.rows) != 1 {
		t.Fatal("a second request ID")
	}
}

// The Signer signed, but the reply was lost on the way back: the Host
// records unknown, and Resume finds the saved authorization under the same
// request ID.
func TestReplyLost(t *testing.T) {
	e := newEnv(t)
	inner := e.socket + "-signer"
	ln, err := net.Listen("unix", inner)
	if err != nil {
		t.Fatal(err)
	}
	e.serve(ln)
	front, err := net.Listen("unix", e.socket)
	if err != nil {
		t.Fatal(err)
	}
	defer front.Close()
	go func() { // forwards one request, lets the Signer answer, drops the answer
		cc, err := front.Accept()
		if err != nil {
			return
		}
		defer cc.Close()
		req, err := http.ReadRequest(bufio.NewReader(cc))
		if err != nil {
			return
		}
		sc, err := net.Dial("unix", inner)
		if err != nil {
			return
		}
		defer sc.Close()
		req.Write(sc)
		if resp, err := http.ReadResponse(bufio.NewReader(sc), req); err == nil {
			io.Copy(io.Discard, resp.Body)
			resp.Body.Close()
		}
	}()
	c := New(e.socket, e.l)
	r, err := c.Pay(pay("po-1"))
	if !errors.Is(err, Unknown) || e.l.rows[r.Attempt.RequestID].State != "unknown" {
		t.Fatalf("lost reply: %+v %v", r, err)
	}
	direct := New(inner, e.l)
	res, err := direct.Resume("consumer-a", r.Attempt.RequestID)
	if err != nil || res.Attempt.State != "signed" || res.Authorization == nil || res.Attempt.RequestID != r.Attempt.RequestID {
		t.Fatalf("resume: %+v %v", res, err)
	}
}

// counting counts the sign requests a client sends, and can fail them as a
// broken transport would (a controlled stand-in for the socket).
type counting struct {
	rt   http.RoundTripper
	mu   sync.Mutex
	sign int
	fail bool
}

func (c *counting) RoundTrip(req *http.Request) (*http.Response, error) {
	c.mu.Lock()
	fail := false
	if req.Method == "POST" && req.URL.Path == "/v1/sign" {
		c.sign++
		fail = c.fail
	}
	c.mu.Unlock()
	if fail {
		return nil, errors.New("injected transport failure")
	}
	return c.rt.RoundTrip(req)
}

func (c *counting) signs() int { c.mu.Lock(); defer c.mu.Unlock(); return c.sign }

func countingClient(socket string, l Ledger) (*Client, *counting) {
	c := New(socket, l)
	tr := &counting{rt: c.http.Transport}
	c.http.Transport = tr
	return c, tr
}

// R3 of the P1-b review (22e45ed): the outcome is unknown AND recording
// unknown failed; the caller sees both, nothing claims unknown was saved,
// nothing is sent again, and once the Store works the same request ID is
// resolved.
func TestUnknownSaveFailure(t *testing.T) {
	e := newEnv(t)
	c, tr := countingClient(e.socket, e.l) // no Signer listening
	e.l.fail = errors.New("disk I/O error")
	r, err := c.Pay(pay("po-1"))
	if !errors.Is(err, Unknown) || !errors.Is(err, NotSaved) {
		t.Fatalf("save failure hidden: %v", err)
	}
	id := r.Attempt.RequestID
	if r.Attempt.State != "requested" || e.l.rows[id].State != "requested" || tr.signs() != 1 {
		t.Fatalf("claimed a saved unknown, or sent again: %+v, ledger %s, %d sign requests", r.Attempt, e.l.rows[id].State, tr.signs())
	}
	e.l.fail = nil
	e.listen()
	if r, err := c.Resume("consumer-a", id); err == nil || r.Attempt.State != "rejected" || r.Attempt.RequestID != id {
		t.Fatalf("resume: %+v %v", r, err)
	}
	if r, err := c.Pay(pay("po-1")); err != nil || r.Attempt.RequestID != id || r.Attempt.State != "signed" || len(e.l.rows) != 1 {
		t.Fatalf("pay after: %+v %v", r, err)
	}
}
