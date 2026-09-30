package contracts

import (
	"encoding/json"
	"fmt"
	"net/url"
	"regexp"
	"strconv"
)

var idPattern = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._:-]{0,127}$`)
var addressPattern = regexp.MustCompile(`^0x[0-9a-f]{40}$`)
var noncePattern = regexp.MustCompile(`^0x[0-9a-f]{64}$`)
var digestPattern = regexp.MustCompile(`^sha256:[0-9a-f]{64}$`)
var decimalPattern = regexp.MustCompile(`^(0|[1-9][0-9]*)$`)

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
func object(raw []byte) error {
	c, err := Canonical(raw)
	if err != nil {
		return err
	}
	if len(c) == 0 || c[0] != '{' {
		return fmt.Errorf("expected JSON object")
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
	u, err := url.Parse(i.ResourceURL)
	if err != nil || u.Hostname() == "" || u.User != nil || u.Fragment != "" || (u.Scheme != "http" && u.Scheme != "https") {
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
func (p PurchaseOrder) Validate() error {
	if p.Version != Version || !ids(p.RequestID) {
		return fmt.Errorf("invalid purchase order")
	}
	if err := p.Intent.Validate(); err != nil {
		return err
	}
	h, err := p.Quote.Hash()
	if err != nil || h != p.Intent.PaymentRequirementHash || p.Quote.X402Version != p.Intent.X402Version {
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
	return nil
}
func (d PolicyDecision) Validate() error {
	if d.Version != Version || !digestPattern.MatchString(d.IntentHash) || !ids(d.Reason) || d.PaymentStatus != "unavailable" {
		return fmt.Errorf("invalid M1 decision")
	}
	switch d.PolicyVerdict {
	case "Ask":
		if d.ApprovalStatus == "pending" {
			return nil
		}
	case "Deny":
		if d.ApprovalStatus == "not_requested" {
			return nil
		}
	case "Allow":
		if d.ApprovalStatus == "not_requested" && d.Reason == "PAYMENT_CAPABILITY_UNAVAILABLE" {
			return nil
		}
	}
	return fmt.Errorf("invalid verdict/approval combination; M1 cannot approve or settle")
}
