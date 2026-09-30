package contracts

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"regexp"
	"slices"
	"strconv"
	"strings"
)

var idPattern = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._:-]{0,127}$`)
var addressPattern = regexp.MustCompile(`^0x[0-9a-f]{40}$`)
var noncePattern = regexp.MustCompile(`^0x[0-9a-f]{64}$`)
var digestPattern = regexp.MustCompile(`^sha256:[0-9a-f]{64}$`)
var decimalPattern = regexp.MustCompile(`^(0|[1-9][0-9]*)$`)

// CodedError carries a stable reason code. Callers and the B1-07 checker
// compare Code, never the message text.
type CodedError struct{ Code, Detail string }

func (e *CodedError) Error() string { return e.Code + ": " + e.Detail }
func coded(code, detail string) error { return &CodedError{code, detail} }

// ErrorCode returns the stable code carried by err, or "" when it has none.
func ErrorCode(err error) string {
	var c *CodedError
	if errors.As(err, &c) {
		return c.Code
	}
	return ""
}

// ReasonCodes is the closed M1 v2 list of outcome and rejection reasons used by
// decisions, events and expected case outcomes. Adding one needs a new version.
var ReasonCodes = []string{
	// Policy outcomes.
	"HUMAN_APPROVAL_REQUIRED", "INSUFFICIENT_BUDGET", "MERCHANT_NOT_ALLOWED", "AUTHORIZATION_EXPIRED",
	"CONFIG_MISSING", "APPROVER_UNAVAILABLE", "ASSET_NOT_ALLOWED", "TASK_NOT_ACTIVE", "PAYMENT_CAPABILITY_UNAVAILABLE",
	// Approval and task lifecycle.
	"APPROVAL_TIMEOUT", "HOST_CANCELLED", "CLI_TIMEOUT", "PERSISTED_BINDING",
	// Contract guards.
	"SAME_PAYLOAD", "NEW_REQUEST", "IDEMPOTENCY_CONFLICT", "VALID_BINDING", "OWNERSHIP_MISMATCH",
	"VALID_DECISION", "M1_PAYMENT_UNAVAILABLE", "INVALID_DECISION", "VALID_PURCHASE",
	"UNSUPPORTED_TRANSFER_METHOD", "AMBIGUOUS_KEY", "INVALID_EVENT_ORDER", "EVENT_CHAIN_BROKEN",
	"PURCHASE_INTENT_CHANGED",
}

var policyVerdicts = []string{"Ask", "Deny", "Allow"}
var approvalStatuses = []string{"pending", "not_requested", "expired", "withdrawn"}
var evidenceModes = []string{"fixture", "live", "replay"}
var denyReasons = []string{"INSUFFICIENT_BUDGET", "MERCHANT_NOT_ALLOWED", "AUTHORIZATION_EXPIRED", "CONFIG_MISSING", "APPROVER_UNAVAILABLE", "ASSET_NOT_ALLOWED", "TASK_NOT_ACTIVE"}

// Amount is an unsigned uint64 base-unit quantity serialized ONLY as a string.
// USDC fixture precision is six decimals. It is not a uint256 wire codec.
type Amount string

func (a Amount) Units() (uint64, error) {
	if !decimalPattern.MatchString(string(a)) {
		return 0, fmt.Errorf("amount must be an unsigned decimal string")
	}
	v, err := strconv.ParseUint(string(a), 10, 64)
	if err != nil {
		return 0, fmt.Errorf("amount exceeds uint64")
	}
	return v, nil
}
func (a *Amount) UnmarshalJSON(raw []byte) error {
	var s string
	if err := json.Unmarshal(raw, &s); err != nil {
		return fmt.Errorf("amount must be a JSON string")
	}
	n := Amount(s)
	if _, err := n.Units(); err != nil {
		return err
	}
	*a = n
	return nil
}
func (a Amount) MarshalJSON() ([]byte, error) {
	if _, err := a.Units(); err != nil {
		return nil, err
	}
	return json.Marshal(string(a))
}

type Consumer struct {
	ConsumerID string `json:"consumer_id"`
	AgentID    string `json:"agent_id"`
	WalletID   string `json:"wallet_id"`
}
type WalletRef struct {
	WalletID     string `json:"wallet_id"`
	ChainID      string `json:"chain_id"`
	From         string `json:"from"`
	EvidenceMode string `json:"evidence_mode"`
}
type Task struct {
	Version    string `json:"version"`
	TaskID     string `json:"task_id"`
	ConsumerID string `json:"consumer_id"`
	AgentID    string `json:"agent_id"`
	WalletID   string `json:"wallet_id"`
	Status     string `json:"status"`
	CreatedAt  uint64 `json:"created_at"`
}
type AuthorizationIntent struct {
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
	ChainID                string `json:"chain_id"`
	AssetContract          string `json:"asset_contract"`
	From                   string `json:"from"`
	PayTo                  string `json:"pay_to"`
	Amount                 Amount `json:"amount"`
	ValidAfter             uint64 `json:"valid_after"`
	ValidBefore            uint64 `json:"valid_before"`
	EIP3009Nonce           string `json:"eip3009_nonce"`
}

// Quote binds the complete selected requirement and complete outer extensions.
// These are raw objects, not lossy subsets of a future SDK's wire structures.
type Quote struct {
	X402Version         uint64          `json:"x402_version"`
	SelectedRequirement json.RawMessage `json:"selected_requirement"`
	Extensions          json.RawMessage `json:"extensions"`
}
type PurchaseOrder struct {
	Version   string              `json:"version"`
	RequestID string              `json:"request_id"`
	Intent    AuthorizationIntent `json:"intent"`
	Quote     Quote               `json:"quote"`
}

// PolicyDecision records the policy's verdict. An Ask keeps its verdict for
// life; expired/withdrawn close its approval request in a later event
// (APPROVAL_EXPIRED / APPROVAL_WITHDRAWN) without rewriting the original.
type PolicyDecision struct {
	Version        string `json:"version"`
	IntentHash     string `json:"intent_hash"`
	PolicyVerdict  string `json:"policy_verdict"`
	ApprovalStatus string `json:"approval_status"`
	PaymentStatus  string `json:"payment_status"`
	Reason         string `json:"reason"`
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
func chain(v string) bool     { a, err := Amount(v).Units(); return err == nil && a > 0 }

// object canonicalizes a raw quote part, requires a JSON object and rejects
// keys that differ only by case anywhere inside it.
func object(raw []byte) error {
	c, err := Canonical(raw)
	if err != nil {
		return err
	}
	if len(c) == 0 || c[0] != '{' {
		return fmt.Errorf("expected JSON object")
	}
	var v any
	if err := json.Unmarshal(c, &v); err != nil {
		return err
	}
	return foldUnique(v)
}

// foldUnique rejects "payTo" next to "payto": encoding/json matches struct
// fields case-insensitively, so a later wire decoder could read a different
// value than the one this contract checked.
// ponytail: pairwise EqualFold per object is O(n²) keys; quote objects are small.
func foldUnique(v any) error {
	switch x := v.(type) {
	case map[string]any:
		keys := make([]string, 0, len(x))
		for k, child := range x {
			for _, seen := range keys {
				if strings.EqualFold(k, seen) {
					return coded("AMBIGUOUS_KEY", fmt.Sprintf("keys %q and %q differ only by case", seen, k))
				}
			}
			keys = append(keys, k)
			if err := foldUnique(child); err != nil {
				return err
			}
		}
	case []any:
		for _, child := range x {
			if err := foldUnique(child); err != nil {
				return err
			}
		}
	}
	return nil
}
func ResourceHash(method, resource string) (string, error) {
	return digestValue(FixtureDomain+"/resource", map[string]string{"http_method": method, "resource_url": resource})
}
func (q Quote) Hash() (string, error) {
	if q.X402Version != 2 {
		return "", fmt.Errorf("M1 fixture supports version 2 only")
	}
	if err := object(q.SelectedRequirement); err != nil {
		return "", err
	}
	if err := object(q.Extensions); err != nil {
		return "", err
	}
	return digestValue(FixtureDomain+"/requirement", q)
}
func (i AuthorizationIntent) Validate() error {
	if i.Version != Version || i.Domain != FixtureDomain {
		return fmt.Errorf("unsupported version/domain (M1 fixture only)")
	}
	if !ids(i.ConsumerID, i.AgentID, i.WalletID, i.TaskID, i.PurchaseOrderID) {
		return fmt.Errorf("invalid intent IDs")
	}
	if i.HTTPMethod != "GET" && i.HTTPMethod != "POST" {
		return fmt.Errorf("unsupported method")
	}
	// Any '#' is refused: url.Parse reports an empty fragment ("…#") as none.
	u, err := url.Parse(i.ResourceURL)
	if err != nil || u.Hostname() == "" || u.User != nil || strings.Contains(i.ResourceURL, "#") || (u.Scheme != "http" && u.Scheme != "https") {
		return fmt.Errorf("invalid resource URL")
	}
	rh, err := ResourceHash(i.HTTPMethod, i.ResourceURL)
	if err != nil || rh != i.ResourceHash {
		return fmt.Errorf("resource hash mismatch")
	}
	if !digestPattern.MatchString(i.PaymentRequirementHash) || i.X402Version != 2 || i.Scheme != "exact" || !chain(i.ChainID) {
		return fmt.Errorf("invalid requirement metadata")
	}
	if !addressPattern.MatchString(i.From) || !addressPattern.MatchString(i.PayTo) || !addressPattern.MatchString(i.AssetContract) || i.From == i.PayTo {
		return fmt.Errorf("invalid address or payer equals merchant")
	}
	n, err := i.Amount.Units()
	if err != nil || n == 0 {
		return fmt.Errorf("payment amount must be positive uint64")
	}
	if !timestamp(i.ValidAfter) || !timestamp(i.ValidBefore) || i.ValidBefore <= i.ValidAfter || !noncePattern.MatchString(i.EIP3009Nonce) {
		return fmt.Errorf("invalid validity or nonce")
	}
	return nil
}
func (i AuthorizationIntent) Hash() (string, error) {
	if err := i.Validate(); err != nil {
		return "", err
	}
	return digestValue(i.Domain+"/intent", i)
}

// Validate checks shape, quote/intent consistency and the transfer path. It
// says nothing about Policy: an unlisted asset or payee still passes here and
// must reach the deterministic Deny in B1-05.
func (p PurchaseOrder) Validate() error {
	if p.Version != Version || !ids(p.RequestID) {
		return fmt.Errorf("invalid purchase order")
	}
	if err := p.Intent.Validate(); err != nil {
		return err
	}
	h, err := p.Quote.Hash()
	if err != nil {
		return err
	}
	if h != p.Intent.PaymentRequirementHash || p.Quote.X402Version != p.Intent.X402Version {
		return fmt.Errorf("requirement hash mismatch")
	}
	var req map[string]json.RawMessage
	if err := json.Unmarshal(p.Quote.SelectedRequirement, &req); err != nil {
		return err
	}
	for _, pair := range [][2]string{{"scheme", p.Intent.Scheme}, {"network", "eip155:" + p.Intent.ChainID}, {"asset", p.Intent.AssetContract}, {"payTo", p.Intent.PayTo}, {"amount", string(p.Intent.Amount)}} {
		k, want := pair[0], pair[1]
		var got string
		if err := json.Unmarshal(req[k], &got); err != nil || got != want {
			return fmt.Errorf("requirement %s mismatch", k)
		}
	}
	// x402 picks the transfer path from extra.assetTransferMethod (default
	// eip3009). An M1 intent carries an EIP-3009 nonce, so any other path fails.
	if raw, ok := req["extra"]; ok {
		var extra map[string]json.RawMessage
		if err := json.Unmarshal(raw, &extra); err != nil {
			return coded("UNSUPPORTED_TRANSFER_METHOD", "requirement extra must be an object")
		}
		if m, ok := extra["assetTransferMethod"]; ok {
			var method string
			if json.Unmarshal(m, &method) != nil || method != "eip3009" {
				return coded("UNSUPPORTED_TRANSFER_METHOD", "M1 accepts only assetTransferMethod eip3009 or none")
			}
		}
	}
	return nil
}
func (d PolicyDecision) Validate() error {
	if d.Version != Version || !digestPattern.MatchString(d.IntentHash) {
		return coded("INVALID_DECISION", "version or intent hash")
	}
	if d.PaymentStatus != "unavailable" {
		return coded("M1_PAYMENT_UNAVAILABLE", "M1 cannot pay; payment_status must be unavailable")
	}
	ok := false
	switch d.PolicyVerdict {
	case "Ask":
		ok = d.ApprovalStatus == "pending" && d.Reason == "HUMAN_APPROVAL_REQUIRED" ||
			d.ApprovalStatus == "expired" && d.Reason == "APPROVAL_TIMEOUT" ||
			d.ApprovalStatus == "withdrawn" && d.Reason == "HOST_CANCELLED"
	case "Deny":
		ok = d.ApprovalStatus == "not_requested" && slices.Contains(denyReasons, d.Reason)
	case "Allow":
		ok = d.ApprovalStatus == "not_requested" && d.Reason == "PAYMENT_CAPABILITY_UNAVAILABLE"
	}
	if !ok {
		return coded("INVALID_DECISION", "verdict, approval status and reason do not form an M1 decision; M1 cannot approve or settle")
	}
	return nil
}
