package signer_test

// R2 (review of fcc5095), the wiring: the Host client against the real
// Handler over a Unix socket. The request ends after the Signer accepted;
// the client must record and return unknown under the same request ID, send
// nothing more, and later read the attempt (accepted, then signed) by that
// ID. Test keys, a synthetic delegation, a test reservation verifier and a
// ledger stand-in only.

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"sync"
	"testing"

	"github.com/SIMPLYBOYS/agent-payment-authority/internal/paycontract"
	"github.com/SIMPLYBOYS/agent-payment-authority/internal/signer"
	"github.com/SIMPLYBOYS/agent-payment-authority/internal/signer/signertest"
	"github.com/SIMPLYBOYS/agent-payment-authority/internal/signerclient"
	"github.com/SIMPLYBOYS/agent-payment-authority/internal/store"
	"github.com/decred/dcrd/dcrec/secp256k1/v4"
)

const t0 = 1790726450

type anyReservation struct{}

func (anyReservation) Verify(string, json.RawMessage) error { return nil }

// ledger is a minimal stand-in for the Store's three methods.
type ledger struct {
	mu    sync.Mutex
	order json.RawMessage
	rows  map[string]*store.PaymentAttempt
	fail  error
}

func (l *ledger) BeginPayment(req store.PaymentRequest) (store.PaymentAttempt, bool, error) {
	l.mu.Lock()
	defer l.mu.Unlock()
	for _, r := range l.rows {
		return *r, true, nil
	}
	var o paycontract.PurchaseOrder
	json.Unmarshal(l.order, &o)
	intent, err := o.IntentHash()
	if err != nil {
		return store.PaymentAttempt{}, false, err
	}
	r := &store.PaymentAttempt{RequestID: "pay-" + fmt.Sprintf("%032d", 1), TaskID: req.TaskID, PurchaseOrderID: req.PurchaseOrderID, IntentHash: intent,
		Method: req.Method, DelegationRef: req.DelegationRef, State: "requested", Order: l.order}
	l.rows[r.RequestID] = r
	return *r, false, nil
}

func (l *ledger) UpdatePayment(id, state, attempt, code string) (store.PaymentAttempt, error) {
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.fail != nil {
		return store.PaymentAttempt{}, l.fail
	}
	r := l.rows[id]
	r.State, r.SignerAttemptID, r.ErrorCode = state, attempt, code
	return *r, nil
}

func (l *ledger) Payment(consumer, id string) (store.PaymentAttempt, error) {
	l.mu.Lock()
	defer l.mu.Unlock()
	if r := l.rows[id]; r != nil && consumer == "consumer-a" {
		return *r, nil
	}
	return store.PaymentAttempt{}, errors.New("UNKNOWN_PAYMENT")
}

func (l *ledger) state(id string) string { l.mu.Lock(); defer l.mu.Unlock(); return l.rows[id].State }

func key(name string) *secp256k1.PrivateKey {
	b := sha256.Sum256([]byte("signer wiring test wallet " + name))
	return secp256k1.PrivKeyFromBytes(b[:])
}

type wired struct {
	core   *signer.Core
	l      *ledger
	client *signerclient.Client
	mu     sync.Mutex
	cancel context.CancelFunc // the current request's
	signs  int
	answer string // the Handler's last sign answer: status and code
}

// recorder keeps what the Handler wrote.
type recorder struct {
	http.ResponseWriter
	status int
	body   []byte
}

func (r *recorder) WriteHeader(s int) { r.status = s; r.ResponseWriter.WriteHeader(s) }
func (r *recorder) Write(b []byte) (int, error) {
	r.body = append(r.body, b...)
	return r.ResponseWriter.Write(b)
}

// wire serves the real Handler on a Unix socket; each request's context is
// the caller's uid and a cancel the after-accept hook can use.
func wire(t *testing.T) *wired {
	t.Helper()
	dir, err := os.MkdirTemp("", "sw") // a socket path is at most 104 bytes
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.RemoveAll(dir) })
	ka, kb := key("a"), key("b")
	a, b := signer.Address(ka.PubKey()), signer.Address(kb.PubKey())
	cfg, err := signer.ParseConfig(signertest.Config(filepath.Join(dir, "s"), uint32(os.Getuid()), t0, a, b))
	if err != nil {
		t.Fatal(err)
	}
	w := &wired{l: &ledger{rows: map[string]*store.PaymentAttempt{},
		order: signertest.Order{Consumer: "consumer-a", Agent: "agent-a", Wallet: "wallet-a", From: a, PayTo: signertest.Merchant, Amount: "500000", N: 1, Now: t0, Window: 60}.Raw()}}
	if w.core, err = signer.NewCore(context.Background(), cfg, map[string]*secp256k1.PrivateKey{a: ka, b: kb}, filepath.Join(dir, "signer.db"),
		func() uint64 { return t0 }, anyReservation{}); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { w.core.Close() })
	ln, err := net.Listen("unix", filepath.Join(dir, "s"))
	if err != nil {
		t.Fatal(err)
	}
	h := signer.Handler(w.core, func(string) {})
	srv := &http.Server{Handler: http.HandlerFunc(func(rw http.ResponseWriter, req *http.Request) {
		ctx, cancel := context.WithCancel(signer.WithCaller(req.Context(), uint32(os.Getuid())))
		defer cancel()
		w.mu.Lock()
		w.cancel = cancel
		if req.Method == "POST" && req.URL.Path == "/v1/sign" {
			w.signs++
		}
		w.mu.Unlock()
		rec := &recorder{ResponseWriter: rw, status: http.StatusOK}
		h.ServeHTTP(rec, req.WithContext(ctx))
		if req.URL.Path == "/v1/sign" {
			var r signer.Reply
			json.Unmarshal(rec.body, &r)
			w.mu.Lock()
			w.answer = fmt.Sprintf("%d %s", rec.status, r.Error)
			w.mu.Unlock()
		}
	})}
	go srv.Serve(ln)
	t.Cleanup(func() { srv.Close() })
	w.client = signerclient.New(filepath.Join(dir, "s"), w.l)
	return w
}

// endAfterAccept makes the next sign request end right after the Signer
// commits its acceptance.
func (w *wired) endAfterAccept(t *testing.T) {
	signer.SetHook(func(stage, _ string) error {
		if stage == "after-accept" {
			w.mu.Lock()
			w.cancel()
			w.mu.Unlock()
		}
		return nil
	})
	t.Cleanup(func() { signer.SetHook(nil) })
}

func (w *wired) lastAnswer() string { w.mu.Lock(); defer w.mu.Unlock(); return w.answer }

func (w *wired) signCount() int { w.mu.Lock(); defer w.mu.Unlock(); return w.signs }

var pay = store.PaymentRequest{ConsumerID: "consumer-a", TaskID: "task-consumer-a", PurchaseOrderID: "po-1", Method: "delegation", DelegationRef: "dlg-a@1"}

func TestClientAfterAccept(t *testing.T) {
	w := wire(t)
	w.endAfterAccept(t)
	r, err := w.client.Pay(pay)
	signer.SetHook(nil)
	id := r.Attempt.RequestID
	if !errors.Is(err, signerclient.Unknown) || errors.Is(err, signerclient.NotSaved) {
		t.Fatalf("client took an accepted attempt for never accepted: %v", err)
	}
	if a := w.lastAnswer(); a != "503 SIGNER_TIMEOUT" {
		t.Fatalf("the Handler answered %q, want 503 SIGNER_TIMEOUT", a)
	}
	if r.Attempt.State != "unknown" || w.l.state(id) != "unknown" || w.signCount() != 1 {
		t.Fatalf("result %s, ledger %s, %d sign requests", r.Attempt.State, w.l.state(id), w.signCount())
	}
	// Asking does not sign: the same attempt, still accepted.
	got, err := w.client.Resume("consumer-a", id)
	if err != nil || got.Attempt.State != "accepted" || got.Attempt.RequestID != id || got.Authorization != nil || w.signCount() != 1 {
		t.Fatalf("resume: %+v %v, %d sign requests", got.Attempt, err, w.signCount())
	}
	// The existing same-ID path finishes it; then it reads back signed.
	done, err := w.client.Pay(pay)
	if err != nil || done.Attempt.State != "signed" || done.Attempt.RequestID != id || done.Attempt.SignerAttemptID != got.Attempt.SignerAttemptID {
		t.Fatalf("pay again: %+v %v", done.Attempt, err)
	}
	again, err := w.client.Resume("consumer-a", id)
	if err != nil || again.Authorization == nil || again.Authorization.Signature != done.Authorization.Signature {
		t.Fatalf("signed read back: %+v %v", again, err)
	}
}

// R3 stays: the request ends after acceptance and recording unknown fails.
func TestClientAfterAcceptNotSaved(t *testing.T) {
	w := wire(t)
	w.endAfterAccept(t)
	w.l.fail = errors.New("disk I/O error")
	r, err := w.client.Pay(pay)
	if !errors.Is(err, signerclient.Unknown) || !errors.Is(err, signerclient.NotSaved) {
		t.Fatalf("save failure hidden: %v", err)
	}
	if r.Attempt.State != "requested" || w.signCount() != 1 {
		t.Fatalf("claimed a saved unknown, or sent again: %s, %d sign requests", r.Attempt.State, w.signCount())
	}
}
