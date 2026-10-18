// Package payflow is the Host's payment worker (P2, docs/commercial-p2-host.md
// 3, 7): one payment at a time (decision 7), each moved by its Store state
// through reserve (the budget relayer, tx#1), sign (the payment Signer),
// submit (the shop, tx#2), settle and deliver. Every step is recorded in
// the Store before its outside call, and every outside call is one the
// other side answers the same way when asked again, so a restart or a
// lost answer resumes the same payment: never a second one. A result not
// known is moved only by the chain (Chain, the Reconciler).
package payflow

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/SIMPLYBOYS/agent-payment-authority/internal/budget"
	"github.com/SIMPLYBOYS/agent-payment-authority/internal/contracts"
	"github.com/SIMPLYBOYS/agent-payment-authority/internal/eth"
	"github.com/SIMPLYBOYS/agent-payment-authority/internal/merchant"
	"github.com/SIMPLYBOYS/agent-payment-authority/internal/paycontract"
	"github.com/SIMPLYBOYS/agent-payment-authority/internal/reconcile"
	"github.com/SIMPLYBOYS/agent-payment-authority/internal/signer"
	"github.com/SIMPLYBOYS/agent-payment-authority/internal/store"
	"github.com/SIMPLYBOYS/agent-payment-authority/internal/x402"
)

// Relayer is the budget relayer (internal/budget): Reserve is idempotent
// per order; Lookup never reserves.
type Relayer interface {
	Reserve(ctx context.Context, order json.RawMessage) (budget.Evidence, error)
	Lookup(ctx context.Context, intent string) (budget.Evidence, string, error)
}

// Signer is the payment Signer's /v1/sign: the same request ID is answered
// as the same attempt.
type Signer interface {
	Sign(ctx context.Context, r signer.SignRequest) (signer.Reply, error)
}

// Shop sends one resource request with a PAYMENT-SIGNATURE: its status,
// its PAYMENT-RESPONSE (nil without one) and its body.
type Shop interface {
	Pay(ctx context.Context, url, paymentSignature string) (ShopAnswer, error)
}

type ShopAnswer struct {
	Status   int
	Response *merchant.SettlementResponse
	Body     []byte
}

// Chain is the Reconciler: an authorization's state at confirmed depth.
type Chain interface {
	Status(ctx context.Context, a reconcile.Authorization) (reconcile.Result, error)
}

// Worker moves payments. Margin is the seconds before valid_before after
// which nothing new is started for a payment (a reservation, a signature).
type Worker struct {
	Store   *store.Store
	Relayer Relayer
	Signer  Signer
	Shop    Shop
	Chain   Chain
	Clock   func() uint64
	Margin  uint64
	Every   time.Duration // Run's interval
	kick    chan struct{}
}

// open are the states a payment is still moved from.
var open = []string{"requested", "reserved", "signed", "submitted", "unknown", "settled"}

// Recover is the start (decision 5): a payment submitted when the Host
// stopped has an unknown result.
func (w *Worker) Recover(ctx context.Context) error {
	ps, err := w.Store.PaymentsIn("submitted")
	if err != nil {
		return err
	}
	for _, p := range ps {
		if _, err := w.Store.PaymentUnknown(p.IntentHash); err != nil {
			return err
		}
	}
	return nil
}

// Kick asks Run to look at the payments now.
func (w *Worker) Kick() {
	select {
	case w.kick <- struct{}{}:
	default:
	}
}

// Run recovers, then moves every open payment, one at a time, every Every
// or when kicked, until ctx ends. A payment's error stays with it: the
// next round asks again.
func (w *Worker) Run(ctx context.Context) error {
	if w.kick == nil {
		w.kick = make(chan struct{}, 1)
	}
	if err := w.Recover(ctx); err != nil {
		return err
	}
	for {
		ps, err := w.Store.PaymentsIn(open...)
		if err == nil {
			for _, p := range ps {
				w.Step(ctx, p)
			}
		}
		select {
		case <-ctx.Done():
			return nil
		case <-w.kick:
		case <-time.After(w.Every):
		}
	}
}

// ErrPending: the payment waits for something outside (a transaction to be
// confirmed, a node, the shop); asked again later it moves on.
var ErrPending = errors.New("payflow: pending")

// Step moves one payment as far as it can now, and returns where it is.
func (w *Worker) Step(ctx context.Context, p store.Payment) (store.Payment, error) {
	for {
		next, err := w.step(ctx, p)
		if err != nil || next.State == p.State || !slices.Contains(open, next.State) {
			return next, err
		}
		p = next
	}
}

func (w *Worker) step(ctx context.Context, p store.Payment) (store.Payment, error) {
	var o paycontract.PurchaseOrder
	if err := contracts.Decode(p.Order, &o); err != nil {
		return p, err
	}
	switch p.State {
	case "requested":
		return w.reserve(ctx, p, o)
	case "reserved":
		return w.sign(ctx, p, o)
	case "signed":
		// Recorded before the shop is asked: from here its result may be on chain.
		next, err := w.Store.PaymentSubmitted(p.IntentHash)
		if err != nil {
			return p, err
		}
		return w.submit(ctx, next, o)
	case "submitted", "unknown", "settled":
		return w.submit(ctx, p, o)
	}
	return p, nil
}

// late: no new step is started this close to the order's valid_before.
func (w *Worker) late(o paycontract.PurchaseOrder) bool {
	return w.Clock()+w.Margin >= o.PaymentBinding.ValidBefore
}

func refusal(err error) (signer.Refusal, bool) {
	var r signer.Refusal
	ok := errors.As(err, &r)
	return r, ok
}

// reserve is tx#1. The relayer is asked first what it already has for the
// intent (Lookup never reserves): only when it has nothing may the payment
// end as not paid; a reservation made before a restart is recorded, never
// made again.
func (w *Worker) reserve(ctx context.Context, p store.Payment, o paycontract.PurchaseOrder) (store.Payment, error) {
	ev, state, err := w.Relayer.Lookup(ctx, p.IntentHash)
	switch {
	case err != nil:
		return p, ErrPending
	case state == "reserved":
		return w.reserved(p, ev)
	case state == "failed":
		return w.Store.PaymentFailed(p.IntentHash, "RESERVE_FAILED")
	case state == "sent":
		return p, ErrPending
	}
	// Nothing at the relayer: a payment the task's end overtook, or one
	// too late to pay, ends here, nothing spent.
	if ended, err := w.Store.TaskEnded(p.TaskID); err != nil {
		return p, err
	} else if ended {
		return w.Store.PaymentFailed(p.IntentHash, "TASK_ENDED")
	}
	if w.late(o) {
		return w.Store.PaymentFailed(p.IntentHash, "NOT_PAID_EXPIRED")
	}
	ev, err = w.Relayer.Reserve(ctx, p.Order)
	if r, ok := refusal(err); ok {
		switch r {
		case "RESERVATION_PENDING", "TASK_BUSY", "RELAYER_NONCE_BLOCKED", "TASK_REGISTRATION_PENDING":
			return p, ErrPending
		}
		return w.Store.PaymentFailed(p.IntentHash, "RESERVE_FAILED") // refused before a transaction, or reverted: nothing spent
	} else if err != nil {
		return p, ErrPending // a node or the relayer itself: Lookup decides next time
	}
	return w.reserved(p, ev)
}

func (w *Worker) reserved(p store.Payment, ev budget.Evidence) (store.Payment, error) {
	raw, err := json.Marshal(ev)
	if err != nil {
		return p, err
	}
	return w.Store.PaymentReserved(p.IntentHash, store.Reservation{Evidence: raw, TxHash: ev.TxHash, Block: ev.BlockNumber})
}

// SignerRequestID is a payment's one Signer request ID, from its intent.
func SignerRequestID(intent string) string {
	h := sha256.Sum256([]byte("agent-payment-authority/signer-request\x00" + intent))
	return "pay-" + hex.EncodeToString(h[:16])
}

// signerRetry are the Signer's answers that mean "not now".
var signerRetry = []signer.Refusal{"SIGNER_BUSY", "SIGNER_TIMEOUT", "RESERVATION_PENDING", "RESERVATION_UNCONFIRMED", "RESERVATION_UNAVAILABLE"}

// sign asks the Signer under the payment's authorization: a delegation
// (id@version) or the person's approval (its Signer approval ID).
func (w *Worker) sign(ctx context.Context, p store.Payment, o paycontract.PurchaseOrder) (store.Payment, error) {
	auth := signer.Authorization{Method: p.Method}
	if p.Method == "delegation" {
		id, v, ok := strings.Cut(p.GrantRef, "@")
		n, err := strconv.ParseUint(v, 10, 64)
		if !ok || err != nil {
			return p, errors.New("payflow: a delegation reference is id@version")
		}
		auth.DelegationID, auth.DelegationVersion = id, n
	} else {
		auth.ApprovalID = p.GrantRef
	}
	reply, err := w.Signer.Sign(ctx, signer.SignRequest{RequestID: SignerRequestID(p.IntentHash), Authorization: auth, Order: p.Order, Reservation: p.Reservation})
	if r, ok := refusal(err); ok {
		if slices.Contains(signerRetry, r) && !w.late(o) {
			return p, ErrPending
		}
		return w.Store.PaymentFailed(p.IntentHash, "RESERVED_NOT_PAID") // tx#1 spent: a person's to look at (decision 6)
	} else if err != nil {
		if w.late(o) {
			return w.Store.PaymentFailed(p.IntentHash, "RESERVED_NOT_PAID")
		}
		return p, ErrPending // the same request ID next time: the same attempt
	}
	if reply.State != "signed" || reply.Authorization == nil {
		return p, ErrPending
	}
	raw, err := json.Marshal(reply.Authorization)
	if err != nil {
		return p, err
	}
	return w.Store.PaymentSigned(p.IntentHash, raw)
}

// paymentSignature is the x402 PAYMENT-SIGNATURE of the signed authorization
// for the order's quote: always the same for a payment.
func paymentSignature(o paycontract.PurchaseOrder, a signer.SignedAuthorization) (string, error) {
	payload, err := json.Marshal(map[string]any{"signature": a.Signature, "authorization": map[string]string{
		"from": a.From, "to": a.To, "value": a.Value, "validAfter": strconv.FormatUint(a.ValidAfter, 10),
		"validBefore": strconv.FormatUint(a.ValidBefore, 10), "nonce": a.Nonce}})
	if err != nil {
		return "", err
	}
	return x402.Encode(x402.PaymentPayload{X402Version: x402.Version, Resource: &x402.ResourceInfo{URL: o.Intent.ResourceURL},
		Accepted: o.Quote.SelectedRequirement, Payload: payload, Extensions: json.RawMessage(`{}`)})
}

func authorization(a signer.SignedAuthorization) (reconcile.Authorization, error) {
	from, err1 := eth.ParseAddress(a.From)
	to, err2 := eth.ParseAddress(a.To)
	value, err3 := strconv.ParseUint(a.Value, 10, 64)
	n, err4 := hex.DecodeString(strings.TrimPrefix(a.Nonce, "0x"))
	if err := errors.Join(err1, err2, err3, err4); err != nil || len(n) != 32 {
		return reconcile.Authorization{}, errors.New("payflow: the signed authorization does not read")
	}
	r := reconcile.Authorization{From: from, To: to, Value: value, ValidBefore: a.ValidBefore}
	copy(r.Nonce[:], n)
	return r, nil
}

// submit is tx#2 and the delivery: the same PAYMENT-SIGNATURE every time.
// Paid is only what the chain confirms (Chain.Status settled), never the
// shop's word alone; the content is recorded (its hash) only then. A
// result not known waits; once the authorization has expired, the chain
// decides how it ends: settled (then delivered, or DELIVERY_FAILED),
// NOT_SETTLED_EXPIRED, or RECONCILE_NEEDS_REVIEW.
func (w *Worker) submit(ctx context.Context, p store.Payment, o paycontract.PurchaseOrder) (store.Payment, error) {
	var a signer.SignedAuthorization
	if err := json.Unmarshal(p.Authorization, &a); err != nil {
		return p, err
	}
	ra, err := authorization(a)
	if err != nil {
		return p, err
	}
	sig, err := paymentSignature(o, a)
	if err != nil {
		return p, err
	}
	ans, err := w.Shop.Pay(ctx, o.Intent.ResourceURL, sig)
	if err != nil || ans.Status >= 500 {
		if p.State == "submitted" {
			next, err := w.Store.PaymentUnknown(p.IntentHash)
			if err != nil {
				return p, err
			}
			p = next
		}
		return w.byChain(ctx, p, ra, nil)
	}
	switch {
	case ans.Status == 200:
		return w.byChain(ctx, p, ra, &ans)
	case ans.Status == 202:
		return w.byChain(ctx, p, ra, nil)
	case ans.Status == 402 && (ans.Response == nil || ans.Response.ErrorReason != "settlement_failed") || ans.Status == 400:
		if p.State == "settled" {
			return w.Store.PaymentFailed(p.IntentHash, "DELIVERY_FAILED") // paid, and the shop does not deliver
		}
		// The shop refused it unsent: never paid again; a person's to look at.
		return w.Store.PaymentFailed(p.IntentHash, "VERIFICATION_FAILED")
	}
	// 402 settlement_failed (a front-run, say) or anything else: the chain says what happened.
	return w.byChain(ctx, p, ra, &ShopAnswer{Status: ans.Status})
}

// byChain moves a submitted, unknown or settled payment by the chain's
// state of its authorization; ans is the shop's answer when it had one.
func (w *Worker) byChain(ctx context.Context, p store.Payment, ra reconcile.Authorization, ans *ShopAnswer) (store.Payment, error) {
	r, err := w.Chain.Status(ctx, ra)
	if err != nil {
		return p, ErrPending
	}
	switch r.State {
	case "settled":
		if p.State != "settled" {
			var resp json.RawMessage
			if ans != nil && ans.Response != nil {
				resp, _ = json.Marshal(ans.Response)
			}
			next, err := w.Store.PaymentSettled(p.IntentHash, "0x"+hex.EncodeToString(r.Tx[:]), r.Block, resp)
			if err != nil {
				return p, err
			}
			p = next
		}
		switch {
		case ans != nil && ans.Status == 200:
			sum := sha256.Sum256(ans.Body)
			return w.Store.PaymentDelivered(p.IntentHash, hex.EncodeToString(sum[:]), len(ans.Body))
		case ans != nil && ans.Status != 202:
			return w.Store.PaymentFailed(p.IntentHash, "DELIVERY_FAILED") // paid on chain, the shop refused to deliver
		}
		return p, ErrPending // paid: the content on the next ask
	case "not_settled_expired":
		return w.Store.PaymentFailed(p.IntentHash, "NOT_SETTLED_EXPIRED")
	case "needs_review":
		return w.Store.PaymentFailed(p.IntentHash, "RECONCILE_NEEDS_REVIEW")
	}
	return p, ErrPending // pending: it may yet settle; never paid again
}
