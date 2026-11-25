// Package payflow is the Host's payment worker (P2, docs/commercial-p2-host.md
// 3, 7): each payment moved on its own, beside the others (decision 7 as
// revised 11/14: no consumer's payment waits for another's), by its Store state
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
	"io"
	"net/http"
	"slices"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/SIMPLYBOYS/agent-payment-authority/internal/budget"
	"github.com/SIMPLYBOYS/agent-payment-authority/internal/contracts"
	"github.com/SIMPLYBOYS/agent-payment-authority/internal/eth"
	"github.com/SIMPLYBOYS/agent-payment-authority/internal/merchant"
	"github.com/SIMPLYBOYS/agent-payment-authority/internal/paycontract"
	"github.com/SIMPLYBOYS/agent-payment-authority/internal/reconcile"
	"github.com/SIMPLYBOYS/agent-payment-authority/internal/signer"
	"github.com/SIMPLYBOYS/agent-payment-authority/internal/signerclient"
	"github.com/SIMPLYBOYS/agent-payment-authority/internal/store"
	"github.com/SIMPLYBOYS/agent-payment-authority/internal/x402"
)

// Relayer is the budget relayer (internal/budget): Reserve is idempotent
// per order; Lookup never reserves.
type Relayer interface {
	Reserve(ctx context.Context, order json.RawMessage) (budget.Evidence, error)
	Lookup(ctx context.Context, intent string) (budget.Evidence, string, error)
}

// Signer is the Host's Signer client (signerclient.Client, docs/commercial-p2-host.md
// 3): Pay records the request in payment_attempts (its request ID made and
// committed by the Store before the call, the same one ever after) and sends
// it with the reservation; asked again it resends that request ID.
type Signer interface {
	Pay(req store.PaymentRequest, reservation json.RawMessage) (signerclient.Result, error)
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

// Known are the authorizations the Store recorded as signed, for the
// reconciliation (reconcile.Token.Report).
func Known(st *store.Store) ([]reconcile.Authorization, error) {
	raws, err := st.SignedAuthorizations()
	if err != nil {
		return nil, err
	}
	out := make([]reconcile.Authorization, 0, len(raws))
	for _, raw := range raws {
		var a signer.SignedAuthorization
		if err := json.Unmarshal(raw, &a); err != nil {
			return nil, err
		}
		ra, err := authorization(a)
		if err != nil {
			return nil, err
		}
		out = append(out, ra)
	}
	return out, nil
}

// HTTPShop is the shop over HTTP: a GET with the PAYMENT-SIGNATURE, never
// redirected (a 3xx is no answer, the chain decides). A body over MaxBody
// is ErrTooLarge (its hash would not be the content's). Client is
// required, with a timeout.
type HTTPShop struct{ Client *http.Client }

// MaxBody bounds a delivered resource.
const MaxBody = 1 << 20

// ErrTooLarge: the shop answered with more than MaxBody.
var ErrTooLarge = errors.New("payflow: the shop's answer is over MaxBody")

func (h HTTPShop) Pay(ctx context.Context, url, paymentSignature string) (ShopAnswer, error) {
	req, err := http.NewRequestWithContext(ctx, "GET", url, nil)
	if err != nil {
		return ShopAnswer{}, err
	}
	req.Header.Set(x402.HeaderPaymentSignature, paymentSignature)
	c := *h.Client
	c.CheckRedirect = func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }
	resp, err := c.Do(req)
	if err != nil {
		return ShopAnswer{}, err
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(io.LimitReader(resp.Body, MaxBody+1))
	if err != nil {
		return ShopAnswer{}, err
	} else if len(body) > MaxBody {
		return ShopAnswer{}, ErrTooLarge
	}
	ans := ShopAnswer{Status: resp.StatusCode, Body: body}
	if v := resp.Header.Get(merchant.HeaderPaymentResponse); v != "" {
		var r merchant.SettlementResponse
		if x402.Decode(v, &r) == nil {
			ans.Response = &r
		}
	}
	return ans, nil
}

// Chain is the Reconciler: an authorization's state at confirmed depth.
type Chain interface {
	Status(ctx context.Context, a reconcile.Authorization) (reconcile.Result, error)
}

var (
	_ Relayer = (*budget.Relayer)(nil)
	_ Chain   = reconcile.Token{}
)

// Worker moves payments. Margin is the seconds before valid_before after
// which nothing new is started for a payment (a reservation, a signature):
// more than the shop's own margin (5 s) plus the time to sign, or the shop
// refuses a payment already reserved and signed. Run moves each payment in
// its own goroutine and never one payment twice at once (a second caller of
// Step is still safe, every outside call being idempotent). Log, when set,
// gets each payment error
// other than ErrPending (intent "" for the Store's list). Before, when set,
// runs in Run's goroutine before each round: the Host's payment starter.
type Worker struct {
	Store   *store.Store
	Relayer Relayer
	Signer  Signer
	Shop    Shop
	Chain   Chain
	Clock   func() uint64
	Margin  uint64
	Every   time.Duration // Run's interval; a second when 0
	Log     func(intent string, err error)
	Before  func(context.Context)
	once    sync.Once
	kick    chan struct{}
	mu      sync.Mutex
	asked   map[string][2]uint64 // intent -> the next time the shop is asked again, the wait before it
	moving  map[string]bool      // intents a goroutine of Run is moving now
}

// claim marks intent as being moved; false when it already is.
func (w *Worker) claim(intent string) bool {
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.moving == nil {
		w.moving = map[string]bool{}
	}
	if w.moving[intent] {
		return false
	}
	w.moving[intent] = true
	return true
}

func (w *Worker) release(intent string) {
	w.mu.Lock()
	defer w.mu.Unlock()
	delete(w.moving, intent)
}

// shopDue: a payment sent and not yet seen settled asks the shop again (the
// same PAYMENT-SIGNATURE) after 2 s, then twice as long each time, at most a
// minute; the chain is read every round between. An outside shop (test402)
// is not sent the same request every second for the authorization's life.
func (w *Worker) shopDue(intent string) bool {
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.asked == nil {
		w.asked = map[string][2]uint64{}
	}
	now, s := w.Clock(), w.asked[intent]
	if now < s[0] {
		return false
	}
	wait := min(max(s[1]*2, 2), 60)
	w.asked[intent] = [2]uint64{now + wait, wait}
	return true
}

func (w *Worker) kicks() chan struct{} {
	w.once.Do(func() { w.kick = make(chan struct{}, 1) })
	return w.kick
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
	case w.kicks() <- struct{}{}:
	default:
	}
}

// Run recovers, then, every Every or when kicked until ctx ends, starts
// moving each open payment not being moved already, each in its own
// goroutine: one payment waiting for its chain or its shop never holds up
// another. It returns once the moves under way have. A payment's error
// stays with it: the next round asks again.
func (w *Worker) Run(ctx context.Context) error {
	kick, every := w.kicks(), w.Every
	if every == 0 {
		every = time.Second
	}
	if err := w.Recover(ctx); err != nil {
		return err
	}
	var moves sync.WaitGroup
	defer moves.Wait()
	for {
		if w.Before != nil {
			w.Before(ctx)
		}
		ps, err := w.Store.PaymentsIn(open...)
		if err != nil {
			w.log("", err)
		}
		for _, q := range ps {
			if !w.claim(q.IntentHash) {
				continue
			}
			moves.Go(func() {
				defer w.release(q.IntentHash)
				// Read again: the round's list may be older than a move that just ended.
				p, err := w.Store.PaymentOf(q.ConsumerID, q.TaskID, q.PurchaseOrderID)
				if err == nil && slices.Contains(open, p.State) {
					_, err = w.Step(ctx, p)
				}
				if err != nil && !errors.Is(err, ErrPending) {
					w.log(q.IntentHash, err)
				}
			})
		}
		select {
		case <-ctx.Done():
			return nil
		case <-kick:
		case <-time.After(every):
		}
	}
}

func (w *Worker) log(intent string, err error) {
	if w.Log != nil {
		w.Log(intent, err)
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
		w.shopDue(p.IntentHash) // the first send starts the waits
		return w.submit(ctx, next, o)
	case "settled":
		return w.submit(ctx, p, o) // paid: the shop is asked for the content at once
	case "submitted", "unknown":
		if !w.shopDue(p.IntentHash) {
			return w.chainOnly(ctx, p)
		}
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
		w.log(p.IntentHash, err) // the relayer's reason (RELAYER_DAILY_CAP, ...) is said on the Host's terminal (10/24: it was found only from the chain)
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

// storeRetry are the Store's refusals of a request that mean "not now": its
// credential's status list or its clock, a moment away from being read.
var storeRetry = []string{"CREDENTIAL_STATUS_UNAVAILABLE", "CLOCK_REGRESSION"}

// signerRetry are the Signer's answers that mean "not now".
// PAYMENT_MODE_OFF: a Signer restarted for approvals only; it may come back.
var signerRetry = []signer.Refusal{"SIGNER_BUSY", "SIGNER_TIMEOUT", "RESERVATION_PENDING", "RESERVATION_UNCONFIRMED", "RESERVATION_UNAVAILABLE", "PAYMENT_MODE_OFF"}

// sign asks the Signer through the Host's client under the payment's
// authorization: a delegation (id@version), or the person's approval (the
// Store reads its grant's Signer approval ID).
func (w *Worker) sign(ctx context.Context, p store.Payment, o paycontract.PurchaseOrder) (store.Payment, error) {
	req := store.PaymentRequest{ConsumerID: p.ConsumerID, TaskID: p.TaskID, PurchaseOrderID: p.PurchaseOrderID, Method: p.Method}
	if p.Method == "delegation" {
		req.DelegationRef = p.GrantRef
	}
	res, err := w.Signer.Pay(req, p.Reservation)
	var r signerclient.Refused
	switch {
	case errors.Is(err, signerclient.Unknown):
		// No readable answer (a 503, say), recorded unknown or not: the same
		// request ID next time, whatever else was joined to it.
		if w.late(o) {
			return w.Store.PaymentFailed(p.IntentHash, "RESERVED_NOT_PAID")
		}
		return p, ErrPending
	case errors.As(err, &r):
		if slices.Contains(signerRetry, signer.Refusal(r)) && !w.late(o) {
			return p, ErrPending
		}
		w.log(p.IntentHash, err) // the Signer's reason on the terminal (10/30: DELEGATION_EXPIRED was found only in the Store)
		return w.Store.PaymentFailed(p.IntentHash, "RESERVED_NOT_PAID") // tx#1 spent: a person's to look at (decision 6)
	case slices.Contains(storeRetry, store.Code(err)) && !w.late(o):
		return p, ErrPending
	case store.Code(err) != "":
		w.log(p.IntentHash, err)
		return w.Store.PaymentFailed(p.IntentHash, "RESERVED_NOT_PAID") // the Store refused the request: never sent
	case err != nil:
		if w.late(o) {
			return w.Store.PaymentFailed(p.IntentHash, "RESERVED_NOT_PAID")
		}
		return p, ErrPending // unknown, or the Store not written: the same request ID next time
	}
	switch st := res.Attempt.State; {
	case st == "abandoned" || st == "expired_unsigned":
		return w.Store.PaymentFailed(p.IntentHash, "RESERVED_NOT_PAID") // the attempt closed unsigned: never signed now
	case st != "signed" || res.Authorization == nil:
		if w.late(o) {
			return w.Store.PaymentFailed(p.IntentHash, "RESERVED_NOT_PAID")
		}
		return p, ErrPending
	}
	if b, a := o.PaymentBinding, res.Authorization; !strings.EqualFold(a.From, b.From) || !strings.EqualFold(a.To, b.PayTo) || a.Value != string(b.Amount) ||
		a.ValidAfter != b.ValidAfter || a.ValidBefore != b.ValidBefore || a.Nonce != b.Nonce {
		return w.Store.PaymentFailed(p.IntentHash, "RESERVED_NOT_PAID") // not this order's authorization: never sent
	}
	raw, err := json.Marshal(res.Authorization)
	if err != nil {
		return p, err
	}
	return w.Store.PaymentSigned(p.IntentHash, raw)
}

// paymentSignature is the x402 PAYMENT-SIGNATURE of the signed authorization
// for the order's quote: always the same for a payment.
func paymentSignature(o paycontract.PurchaseOrder, a signer.SignedAuthorization) (string, error) {
	// `to` as the quote writes payTo (a 402 may use EIP-55's case, B5-D1): a
	// server comparing the two strings as they are finds them equal; the
	// signature covers the address, not its case.
	to := a.To
	var req struct {
		PayTo string `json:"payTo"`
	}
	if json.Unmarshal(o.Quote.SelectedRequirement, &req) == nil && strings.EqualFold(req.PayTo, a.To) {
		to = req.PayTo
	}
	payload, err := json.Marshal(map[string]any{"signature": a.Signature, "authorization": map[string]string{
		"from": a.From, "to": to, "value": a.Value, "validAfter": strconv.FormatUint(a.ValidAfter, 10),
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
	r := reconcile.Authorization{From: from, To: to, Value: value, ValidAfter: a.ValidAfter, ValidBefore: a.ValidBefore}
	copy(r.Nonce[:], n)
	return r, nil
}

// chainOnly reads a sent payment's state on chain without asking the shop.
func (w *Worker) chainOnly(ctx context.Context, p store.Payment) (store.Payment, error) {
	var a signer.SignedAuthorization
	if err := json.Unmarshal(p.Authorization, &a); err != nil {
		return p, err
	}
	ra, err := authorization(a)
	if err != nil {
		return p, err
	}
	return w.byChain(ctx, p, ra, nil)
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
	if p.State == "settled" {
		// Paid at confirmed depth, recorded: the shop's answer alone decides
		// the delivery; the chain is not asked again.
		switch {
		case err == nil && ans.Status == 200:
			return w.delivered(p, ans)
		case err == nil && (ans.Status == 400 || ans.Status == 402) || errors.Is(err, ErrTooLarge):
			return w.Store.PaymentFailed(p.IntentHash, "DELIVERY_FAILED") // paid, and the shop does not deliver (or not within MaxBody)
		}
		return p, ErrPending
	}
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
	case ans.Status == 402 && ans.Response != nil && (ans.Response.Success || ans.Response.ErrorReason == "settlement_failed" || ans.Response.ErrorReason == "invalid_exact_evm_payload_authorization_nonce_used"):
		// Settled by the shop's word and nothing delivered (the post office's parcel,
		// docs/commercial-post-hotel.md N3), failed in settlement (a front-run, say),
		// or a nonce the shop says is used: whether it was paid is the chain's to say.
		return w.byChain(ctx, p, ra, &ShopAnswer{Status: ans.Status})
	case (ans.Status == 402 || ans.Status == 400) && p.State == "unknown":
		// An earlier send's result is not known: tx#2 may be on chain, whatever
		// the shop says now. The chain says how it ends.
		return w.byChain(ctx, p, ra, nil)
	case ans.Status == 402 || ans.Status == 400:
		// The shop refused it unsent: never paid again; a person's to look at.
		if w.Log != nil && ans.Response != nil && ans.Response.ErrorReason != "" {
			w.Log(p.IntentHash, errors.New("shop: "+ans.Response.ErrorReason)) // settlement_daily_cap, say: told apart on the terminal
		}
		return w.Store.PaymentFailed(p.IntentHash, "VERIFICATION_FAILED")
	}
	// Anything else (408, 429, a proxy's 404): not an answer about the payment; the chain says.
	return w.byChain(ctx, p, ra, nil)
}

// byChain moves a submitted or unknown payment by the chain's state of its
// authorization; ans is the shop's answer when it had one (a 200, or a 402
// that failed in settlement).
func (w *Worker) byChain(ctx context.Context, p store.Payment, ra reconcile.Authorization, ans *ShopAnswer) (store.Payment, error) {
	r, err := w.Chain.Status(ctx, ra)
	if err != nil {
		return p, ErrPending
	}
	switch r.State {
	case "settled":
		var resp json.RawMessage
		if ans != nil && ans.Response != nil {
			resp, _ = json.Marshal(ans.Response)
		}
		next, err := w.Store.PaymentSettled(p.IntentHash, "0x"+hex.EncodeToString(r.Tx[:]), r.Block, resp)
		if err != nil {
			return p, err
		}
		switch {
		case ans != nil && ans.Status == 200:
			return w.delivered(next, *ans)
		case ans != nil && ans.Status == 402:
			return w.Store.PaymentFailed(p.IntentHash, "DELIVERY_FAILED") // paid on chain, the shop refused to deliver
		}
		return next, ErrPending // paid: the content on the next ask
	case "not_settled_expired":
		return w.Store.PaymentFailed(p.IntentHash, "NOT_SETTLED_EXPIRED")
	case "needs_review":
		return w.Store.PaymentFailed(p.IntentHash, "RECONCILE_NEEDS_REVIEW")
	}
	return p, ErrPending // pending: it may yet settle; never paid again
}

// delivered records the content, keeping the answer's PAYMENT-RESPONSE with
// the shop's signed receipt in it; a receipt that does not match the
// purchase's signed offer is said on the terminal and recorded as it came:
// the content was delivered, and the checker fails it (E2).
func (w *Worker) delivered(p store.Payment, ans ShopAnswer) (store.Payment, error) {
	sum := sha256.Sum256(ans.Body)
	var resp, ext json.RawMessage
	if ans.Response != nil {
		resp, _ = json.Marshal(ans.Response)
		ext = ans.Response.Extensions
	}
	var o paycontract.PurchaseOrder
	if err := json.Unmarshal(p.Order, &o); err == nil {
		if _, err := x402.ShopReceipt(o.Quote.Extensions, ext, o.Intent.ResourceURL, "eip155:"+o.PaymentBinding.ChainID, o.PaymentBinding.From, p.SettleTx); err != nil && w.Log != nil {
			w.Log(p.IntentHash, errors.New("shop receipt: "+err.Error()))
		}
	}
	return w.Store.PaymentDeliveredWith(p.IntentHash, hex.EncodeToString(sum[:]), len(ans.Body), resp)
}

// Starter starts payments (H-D1 A, docs/commercial-p2-host.md 12.3), as
// Run's Before: each open pay task's budget registered (H-D4 A; Register
// finishes one left unconfirmed), and each purchase decided for payment
// started once per process, an Allow under its Signer delegation
// (Delegations, id@version: "consumer/payee" first, then the consumer's
// own; docs/commercial-p2b.md B5), an approved Ask under its grant,
// once the task's registration has succeeded (or was refused for good: the
// payment then fails where it is seen). Each registration runs in its own
// goroutine (it waits for its transaction): Start never waits for one, and
// a task's payments start as soon as its own has. A Store refusal is
// logged, not asked again until a restart; a registration not refused (a
// node, or one still pending), or a start that failed without a code, is
// asked again next round.
type Starter struct {
	Store       *store.Store
	Register    func(ctx context.Context, taskID, consumerID string, budget uint64) error
	Delegations map[string]string
	Log         func(what string, err error)
	mu          sync.Mutex
	tried       map[string]bool
	registered  map[string]bool
	registering map[string]bool
	wg          sync.WaitGroup // the registrations under way (tests wait for them)
}

func (s *Starter) Start(ctx context.Context) {
	ts, err := s.Store.PayTasks()
	if err != nil {
		s.log("payments", err)
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.tried == nil {
		s.tried, s.registered, s.registering = map[string]bool{}, map[string]bool{}, map[string]bool{}
	}
	for _, t := range ts {
		switch {
		case s.registered[t.TaskID]:
			s.beginAll(t)
		case !s.registering[t.TaskID]:
			// not registered yet: a payment now would end TASK_NOT_REGISTERED
			s.registering[t.TaskID] = true
			s.wg.Add(1)
			go s.register(ctx, t)
		}
	}
}

func (s *Starter) register(ctx context.Context, t store.PayTask) {
	defer s.wg.Done()
	b, err := t.BudgetInitial.Units()
	if err == nil {
		err = s.Register(ctx, t.TaskID, t.ConsumerID, b)
	}
	if err != nil && ctx.Err() == nil {
		s.log("registration "+t.TaskID, err)
	}
	r, refused := refusal(err)
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.registering, t.TaskID)
	if err == nil || refused && r != "TASK_REGISTRATION_PENDING" && r != "RELAYER_NONCE_BLOCKED" {
		s.registered[t.TaskID] = true
		if ctx.Err() == nil {
			s.beginAll(t)
		}
	}
}

// beginAll starts the task's decided purchases (under s.mu).
func (s *Starter) beginAll(t store.PayTask) {
	for _, po := range t.Allowed {
		s.begin(t, po, s.delegation(t.ConsumerID, t.PayTo[po]))
	}
	for _, po := range t.Approved {
		s.begin(t, po, "")
	}
}

// delegation is an Allow's delegation: the one for its consumer and payee
// (test402's dlg-t402, say), else its consumer's. None: "" (the Store
// refuses it; nothing is reserved).
func (s *Starter) delegation(consumer, payTo string) string {
	if d, ok := s.Delegations[consumer+"/"+payTo]; ok {
		return d
	}
	return s.Delegations[consumer]
}

func (s *Starter) begin(t store.PayTask, po, delegation string) {
	key := t.TaskID + "/" + po
	if s.tried[key] {
		return
	}
	_, _, err := s.Store.StartPayment(store.PaymentStart{ConsumerID: t.ConsumerID, TaskID: t.TaskID, PurchaseOrderID: po, DelegationRef: delegation})
	s.tried[key] = err == nil || store.Code(err) != "" // a Store refusal; anything else is asked again
	if err != nil {
		s.log("start "+key, err)
	}
}

func (s *Starter) log(what string, err error) {
	if s.Log != nil {
		s.Log(what, err)
	}
}
