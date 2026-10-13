// Package paycontract is the apa.pay.v1 contract (docs/commercial-transaction-flow.md
// sections 3, 10 and 10.1): a purchase whose business intent and payment
// binding are hashed together, its decision, a delegation, an approval
// session's closures and the simulated AuthorizationGrant of P1-a.
//
// It never touches apa.m1.v2: its own digest domain, its own validation,
// and the M1 contract (internal/contracts) refuses every value made here.
// Nothing here can pay: in P1-a every decision says payment_status
// not_enabled, and a simulated grant gives no payment capability.
package paycontract

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"regexp"
	"slices"
	"strings"

	"github.com/SIMPLYBOYS/agent-payment-authority/internal/contracts"
)

// Version names this contract; Domain prefixes every digest it makes.
const (
	Version = "apa.pay.v1"
	Domain  = "agent-payment-authority/pay/v1"
)

// PaymentNotEnabled is the only payment_status of a P1-a decision.
const PaymentNotEnabled = "not_enabled"

var (
	idPattern      = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._:-]{0,127}$`)
	addressPattern = regexp.MustCompile(`^0x[0-9a-f]{40}$`)
	noncePattern   = regexp.MustCompile(`^0x[0-9a-f]{64}$`)
	digestPattern  = regexp.MustCompile(`^sha256:[0-9a-f]{64}$`)
)

// Error carries a stable code, like contracts.CodedError.
type Error struct{ Code, Detail string }

func (e *Error) Error() string { return e.Code + ": " + e.Detail }

func coded(code, detail string) error { return &Error{code, detail} }

// Code returns err's code from this package or the M1 contract, or "".
func Code(err error) string {
	var e *Error
	if errors.As(err, &e) {
		return e.Code
	}
	return contracts.ErrorCode(err)
}

// ReasonCodes are this version's decision and closure reasons. The deny
// reasons and HUMAN_APPROVAL_REQUIRED are the shared rules' (policy.Rules);
// the rest are new with apa.pay.v1.
var ReasonCodes = []string{
	"HUMAN_APPROVAL_REQUIRED", "WITHIN_ALLOW_MAX",
	"INSUFFICIENT_BUDGET", "MERCHANT_NOT_ALLOWED", "AUTHORIZATION_EXPIRED", "CONFIG_MISSING", "APPROVER_UNAVAILABLE", "ASSET_NOT_ALLOWED",
	"APPROVAL_TIMEOUT", "HOST_CANCELLED", "SIMULATED_APPROVAL", "APPROVER_DENIED", "QUOTE_CHANGED",
	"WEBAUTHN_APPROVAL", // A2: approved by a Signer-verified assertion
}

var denyReasons = []string{"INSUFFICIENT_BUDGET", "MERCHANT_NOT_ALLOWED", "AUTHORIZATION_EXPIRED", "CONFIG_MISSING", "APPROVER_UNAVAILABLE", "ASSET_NOT_ALLOWED"}

// Intent is the business intent: who buys what from which resource, under
// which quote. The payment fields live once, in PaymentBinding.
type Intent struct {
	Version                string `json:"version"`
	Domain                 string `json:"domain"`
	ConsumerID             string `json:"consumer_id"`
	AgentID                string `json:"agent_id"`
	WalletID               string `json:"wallet_id"`
	TaskID                 string `json:"task_id"`
	PurchaseOrderID        string `json:"purchase_order_id"`
	ResourceHash           string `json:"resource_hash"`
	HTTPMethod             string `json:"http_method"`
	ResourceURL            string `json:"resource_url"`
	X402Version            uint64 `json:"x402_version"`
	Scheme                 string `json:"scheme"`
	PaymentRequirementHash string `json:"payment_requirement_hash"`
}

// PaymentBinding is what a payment would be signed for (ADR: EOA +
// EIP-3009). Changing any field gives another intent_hash.
type PaymentBinding struct {
	Method        string           `json:"method"` // eip3009
	ChainID       string           `json:"chain_id"`
	AssetContract string           `json:"asset_contract"`
	From          string           `json:"from"`
	PayTo         string           `json:"pay_to"`
	Amount        contracts.Amount `json:"amount"`
	ValidAfter    uint64           `json:"valid_after"`
	ValidBefore   uint64           `json:"valid_before"`
	Nonce         string           `json:"nonce"`
}

// PurchaseOrder is an apa.pay.v1 purchase request.
type PurchaseOrder struct {
	Version        string          `json:"version"`
	RequestID      string          `json:"request_id"`
	Intent         Intent          `json:"intent"`
	PaymentBinding PaymentBinding  `json:"payment_binding"`
	Quote          contracts.Quote `json:"quote"`
}

func ids(values ...string) bool {
	for _, v := range values {
		if !idPattern.MatchString(v) {
			return false
		}
	}
	return true
}

func timestamp(v uint64) bool { return v > 0 && v <= 253402300799 }

func positive(v string) bool { n, err := contracts.Amount(v).Units(); return err == nil && n > 0 }

// ResourceHash and RequirementHash are apa.pay.v1's (Spec has every version's).
func ResourceHash(method, resource string) (string, error) { return v1.ResourceHash(method, resource) }

func RequirementHash(q contracts.Quote) (string, error) { return v1.RequirementHash(q) }

// specOf is the spec of an object's own version field, or the error code
// its validation reports.
func specOf(version, code, what string) (Spec, error) {
	s, ok := SpecOf(version)
	if !ok {
		return Spec{}, coded(code, "not a pay-family "+what)
	}
	return s, nil
}

// Validate checks shape and quote/intent/binding consistency. Like the M1
// contract it says nothing about Policy: an unlisted payee still passes.
func (p PurchaseOrder) Validate() error {
	i, b := p.Intent, p.PaymentBinding
	sp, err := specOf(p.Version, "INVALID_PURCHASE", "purchase order")
	if err != nil || !ids(p.RequestID) {
		return coded("INVALID_PURCHASE", "not a pay-family purchase order")
	}
	if i.Version != p.Version || i.Domain != sp.Domain {
		return coded("INVALID_PURCHASE", "intent version or domain")
	}
	if !ids(i.ConsumerID, i.AgentID, i.WalletID, i.TaskID, i.PurchaseOrderID) {
		return coded("INVALID_PURCHASE", "intent IDs")
	}
	if i.HTTPMethod != "GET" && i.HTTPMethod != "POST" {
		return coded("INVALID_PURCHASE", "method")
	}
	u, err := url.Parse(i.ResourceURL)
	if err != nil || u.Hostname() == "" || u.User != nil || strings.Contains(i.ResourceURL, "#") || (u.Scheme != "http" && u.Scheme != "https") {
		return coded("INVALID_PURCHASE", "resource URL")
	}
	if rh, err := sp.ResourceHash(i.HTTPMethod, i.ResourceURL); err != nil || rh != i.ResourceHash {
		return coded("INVALID_PURCHASE", "resource hash")
	}
	if b.Method != "eip3009" {
		return coded("UNSUPPORTED_TRANSFER_METHOD", "apa.pay.v1 binds EIP-3009 only (payment account ADR)")
	}
	if !positive(b.ChainID) || !addressPattern.MatchString(b.From) || !addressPattern.MatchString(b.PayTo) || !addressPattern.MatchString(b.AssetContract) || b.From == b.PayTo {
		return coded("INVALID_PURCHASE", "binding chain or addresses")
	}
	if !positive(string(b.Amount)) {
		return coded("INVALID_PURCHASE", "binding amount must be a positive uint64")
	}
	if !timestamp(b.ValidAfter) || !timestamp(b.ValidBefore) || b.ValidBefore <= b.ValidAfter || !noncePattern.MatchString(b.Nonce) {
		return coded("INVALID_PURCHASE", "binding validity or nonce")
	}
	h, err := sp.RequirementHash(p.Quote)
	if err != nil || h != i.PaymentRequirementHash || p.Quote.X402Version != i.X402Version || i.X402Version != 2 || i.Scheme != "exact" {
		return coded("INVALID_PURCHASE", "requirement hash or metadata")
	}
	var req map[string]json.RawMessage
	if err := json.Unmarshal(p.Quote.SelectedRequirement, &req); err != nil {
		return coded("INVALID_PURCHASE", "selected requirement")
	}
	for _, pair := range [][2]string{{"scheme", i.Scheme}, {"network", "eip155:" + b.ChainID}, {"asset", b.AssetContract}, {"payTo", b.PayTo}, {"amount", string(b.Amount)}} {
		var got string
		if err := json.Unmarshal(req[pair[0]], &got); err != nil || got != pair[1] {
			return coded("INVALID_PURCHASE", "requirement "+pair[0]+" differs from the binding")
		}
	}
	if raw, ok := req["extra"]; ok {
		var extra map[string]json.RawMessage
		if json.Unmarshal(raw, &extra) != nil {
			return coded("UNSUPPORTED_TRANSFER_METHOD", "requirement extra must be an object")
		}
		if m, ok := extra["assetTransferMethod"]; ok {
			var method string
			if json.Unmarshal(m, &method) != nil || method != "eip3009" {
				return coded("UNSUPPORTED_TRANSFER_METHOD", "apa.pay.v1 accepts assetTransferMethod eip3009 or none")
			}
		}
	}
	return nil
}

// IntentHash covers the business intent and the payment binding together.
func (p PurchaseOrder) IntentHash() (string, error) {
	if err := p.Validate(); err != nil {
		return "", err
	}
	return specs[p.Version].digest("intent", struct {
		Intent         Intent         `json:"intent"`
		PaymentBinding PaymentBinding `json:"payment_binding"`
	}{p.Intent, p.PaymentBinding})
}

// ValidatePurchase adds ownership to Validate: the trusted session, the
// registry's task, wallet, payer and chain must all be the order's.
func ValidatePurchase(reg *contracts.Registry, s contracts.Session, p PurchaseOrder) error {
	if err := p.Validate(); err != nil {
		return err
	}
	t, w, err := reg.Binding(s.TaskID)
	if err != nil {
		return coded("OWNERSHIP_MISMATCH", "unknown task")
	}
	i, b := p.Intent, p.PaymentBinding
	if s.ConsumerID != t.ConsumerID || s.AgentID != t.AgentID || i.TaskID != t.TaskID || i.ConsumerID != t.ConsumerID || i.AgentID != t.AgentID ||
		i.WalletID != w.WalletID || b.From != w.From || b.ChainID != w.ChainID {
		return coded("OWNERSHIP_MISMATCH", "session, task, wallet or payer do not belong together")
	}
	return nil
}

// RetryKey names one request of one task's consumer and wallet.
func (p PurchaseOrder) RetryKey() (string, error) {
	if err := p.Validate(); err != nil {
		return "", err
	}
	return specs[p.Version].digest("retry", map[string]string{"consumer_id": p.Intent.ConsumerID, "task_id": p.Intent.TaskID, "wallet_id": p.Intent.WalletID, "request_id": p.RequestID})
}

// CompareRetry is contracts.CompareRetry for this version: "replay" for the
// same canonical order, "new" for an unrelated one, IDEMPOTENCY_CONFLICT
// for a reused request ID, purchase_order_id or payer nonce.
func CompareRetry(existing, incoming PurchaseOrder) (string, error) {
	if existing.Version != incoming.Version { // a task's orders are of one version; retry keys of two never meet
		return "", coded("IDEMPOTENCY_CONFLICT", "orders of two contract versions")
	}
	a, err := existing.RetryKey()
	if err != nil {
		return "", err
	}
	b, err := incoming.RetryKey()
	if err != nil {
		return "", err
	}
	e, i := existing, incoming
	if a != b {
		if e.Intent.TaskID == i.Intent.TaskID && e.Intent.PurchaseOrderID == i.Intent.PurchaseOrderID {
			return "", coded("IDEMPOTENCY_CONFLICT", "purchase_order_id reused under another request_id")
		}
		eb, ib := e.PaymentBinding, i.PaymentBinding
		if eb.ChainID == ib.ChainID && eb.AssetContract == ib.AssetContract && eb.From == ib.From && eb.Nonce == ib.Nonce {
			return "", coded("IDEMPOTENCY_CONFLICT", "nonce reused by the same payer and asset")
		}
		return "new", nil
	}
	x, err := specs[existing.Version].digest("purchase", existing)
	if err != nil {
		return "", err
	}
	y, err := specs[incoming.Version].digest("purchase", incoming)
	if err != nil {
		return "", err
	}
	if x != y {
		return "", coded("IDEMPOTENCY_CONFLICT", "same request_id with a different purchase")
	}
	return "replay", nil
}

// ValidateDecision is this version's decision rule. The shape is the M1
// wire shape (contracts.PolicyDecision), with its own version, reasons and
// approval states, and payment_status not_enabled: an Allow or an approval
// is not a payment.
func ValidateDecision(d contracts.PolicyDecision) error {
	sp, ok := SpecOf(d.Version)
	if !ok || !digestPattern.MatchString(d.IntentHash) {
		return coded("INVALID_DECISION", "version or intent hash")
	}
	if d.PaymentStatus != PaymentNotEnabled {
		return coded("INVALID_DECISION", "payment_status must be not_enabled: P1-a has no payment capability")
	}
	ok = false
	switch d.PolicyVerdict {
	case "Ask":
		ok = slices.Contains(sp.askClosures, [2]string{d.ApprovalStatus, d.Reason})
	case "Deny":
		ok = d.ApprovalStatus == "not_requested" && slices.Contains(sp.deny, d.Reason)
	case "Allow":
		ok = d.ApprovalStatus == "not_requested" && d.Reason == "WITHIN_ALLOW_MAX"
	}
	if !ok {
		return coded("INVALID_DECISION", "verdict, approval status and reason do not form an apa.pay.v1 decision")
	}
	return nil
}

// Validate reports whether s is a well-formed ID of this contract.
func ValidID(s string) bool { return ids(s) }

func errf(format string, a ...any) error { return coded("INVALID_EVENT", fmt.Sprintf(format, a...)) }
