// Package signerclient is the Host's client of the Signer (P1-b;
// docs/commercial-p1b-design.md 4.2, 5.4, 6.4). Every request ID is made
// and committed by the Store before the Signer is called, and is the only
// key used for that purchase afterwards: a reply that never arrived is
// recorded unknown and resolved later by asking about the same ID, never
// by a new one. Only standing delegations are sent; the Store refuses any
// other method before writing. The Host keeps no signature: a signed
// authorization is returned to the caller and can be asked for again.
package signerclient

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/SIMPLYBOYS/agent-payment-authority/internal/signer"
	"github.com/SIMPLYBOYS/agent-payment-authority/internal/store"
)

// Ledger is the Store's part: payment_attempts.
type Ledger interface {
	BeginPayment(store.PaymentRequest) (store.PaymentAttempt, bool, error)
	UpdatePayment(requestID, state, signerAttemptID, errorCode string) (store.PaymentAttempt, error)
	Payment(consumerID, requestID string) (store.PaymentAttempt, error)
}

var _ Ledger = (*store.Store)(nil)

// Client calls the Signer on its Unix socket.
type Client struct {
	ledger Ledger
	http   *http.Client
}

// Timeout bounds one call: the Signer's own 5 seconds and its wallet lock.
const Timeout = 10 * time.Second

func New(socket string, ledger Ledger) *Client {
	return &Client{ledger: ledger, http: &http.Client{Timeout: Timeout, Transport: &http.Transport{
		DialContext: func(ctx context.Context, _, _ string) (net.Conn, error) {
			return (&net.Dialer{}).DialContext(ctx, "unix", socket)
		}}}}
}

// Unknown is a call whose outcome the Host does not know (no reply, or one
// it cannot read); the attempt is recorded unknown, for Resume.
var Unknown = errors.New("SIGNER_OUTCOME_UNKNOWN")

// Refused is the Signer's (or the Store's) fixed code.
type Refused string

func (r Refused) Error() string { return string(r) }

// Result is an attempt after a call; Authorization is present when signed.
type Result struct {
	Attempt       store.PaymentAttempt
	Authorization *signer.SignedAuthorization
}

// Pay records the request (or finds the purchase's existing one) and sends
// it to the Signer with that request ID.
func (c *Client) Pay(req store.PaymentRequest) (Result, error) {
	a, _, err := c.ledger.BeginPayment(req)
	if err != nil {
		return Result{}, err
	}
	if a.State == "abandoned" || a.State == "expired_unsigned" {
		return Result{Attempt: a}, nil
	}
	id, ver, _ := strings.Cut(a.DelegationRef, "@")
	v, err := strconv.ParseUint(ver, 10, 64)
	if err != nil {
		return Result{Attempt: a}, Refused("DELEGATION_REF_REQUIRED")
	}
	body, err := json.Marshal(signer.SignRequest{RequestID: a.RequestID, Order: a.Order,
		Authorization: signer.Authorization{Method: a.Method, DelegationID: id, DelegationVersion: v}})
	if err != nil {
		return Result{Attempt: a}, err
	}
	status, r, err := c.call("POST", "/v1/sign", body)
	if err == nil && status == http.StatusConflict && r.State == "" {
		// Refused with no attempt in the reply: ask whether one exists.
		code := r.Error
		if status, r, err = c.call("GET", "/v1/attempts?request_id="+url.QueryEscape(a.RequestID), nil); err == nil && status == http.StatusNotFound {
			return c.record(a, "rejected", "", code, nil)
		}
	}
	return c.settle(a, status, r, err)
}

// Resume asks the Signer about consumerID's request again, by its ID; it
// does not sign anything new.
func (c *Client) Resume(consumerID, requestID string) (Result, error) {
	a, err := c.ledger.Payment(consumerID, requestID)
	if err != nil {
		return Result{}, err
	}
	status, r, err := c.call("GET", "/v1/attempts?request_id="+url.QueryEscape(a.RequestID), nil)
	if err == nil && status == http.StatusNotFound && (a.State == "requested" || a.State == "unknown" || a.State == "rejected") {
		return c.record(a, "rejected", "", "ATTEMPT_UNKNOWN", nil) // the Signer never accepted it
	}
	return c.settle(a, status, r, err)
}

// Abandon gives up consumerID's request before it is signed.
func (c *Client) Abandon(consumerID, requestID string) (Result, error) {
	a, err := c.ledger.Payment(consumerID, requestID)
	if err != nil {
		return Result{}, err
	}
	attempt := a.SignerAttemptID
	if attempt == "" {
		res, err := c.Resume(consumerID, requestID)
		if err != nil || res.Attempt.SignerAttemptID == "" {
			return res, err
		}
		a, attempt = res.Attempt, res.Attempt.SignerAttemptID
	}
	status, r, err := c.call("POST", "/v1/attempts/"+url.PathEscape(attempt)+"/abandon", nil)
	return c.settle(a, status, r, err)
}

// settle records the Signer's answer: its attempt state when it gave one,
// unknown when there was no readable answer.
func (c *Client) settle(a store.PaymentAttempt, status int, r signer.Reply, err error) (Result, error) {
	switch {
	case err != nil || status != http.StatusOK && status != http.StatusConflict && status != http.StatusNotFound:
		if a.State == "requested" || a.State == "unknown" {
			c.ledger.UpdatePayment(a.RequestID, "unknown", a.SignerAttemptID, "")
		}
		return Result{Attempt: a}, Unknown
	case r.State == "":
		return Result{Attempt: a}, Refused(r.Error)
	case r.IntentHash != a.IntentHash || r.RequestID != a.RequestID:
		return Result{Attempt: a}, Refused("INTENT_MISMATCH") // not this purchase's attempt: nothing is recorded
	}
	res, rerr := c.record(a, r.State, r.AttemptID, r.Error, r.Authorization)
	if rerr == nil && r.Error != "" {
		rerr = Refused(r.Error)
	}
	return res, rerr
}

func (c *Client) record(a store.PaymentAttempt, state, attemptID, code string, auth *signer.SignedAuthorization) (Result, error) {
	if state == "rejected" && code == "" {
		code = "SIGNER_REFUSED"
	}
	updated, err := c.ledger.UpdatePayment(a.RequestID, state, attemptID, code)
	if err != nil {
		return Result{Attempt: a}, err
	}
	if state == "rejected" {
		return Result{Attempt: updated}, Refused(code)
	}
	return Result{Attempt: updated, Authorization: auth}, nil
}

func (c *Client) call(method, path string, body []byte) (int, signer.Reply, error) {
	req, err := http.NewRequest(method, "http://signer"+path, bytes.NewReader(body))
	if err != nil {
		return 0, signer.Reply{}, err
	}
	resp, err := c.http.Do(req)
	if err != nil {
		return 0, signer.Reply{}, err
	}
	defer resp.Body.Close()
	raw, err := io.ReadAll(io.LimitReader(resp.Body, 64<<10))
	if err != nil {
		return 0, signer.Reply{}, err
	}
	var r signer.Reply
	if err := json.Unmarshal(raw, &r); err != nil {
		return 0, signer.Reply{}, fmt.Errorf("signer reply: %w", err)
	}
	return resp.StatusCode, r, nil
}
