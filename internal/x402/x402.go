// Package x402 encodes and decodes the x402 v2 HTTP headers the M1 fixture
// shop and its clients exchange. It verifies and settles nothing.
//
// Checked against the coinbase/x402 primary spec on 2026-10-01:
// specs/transports-v2/http.md at aeef1bff (2026-03-05) and
// specs/x402-specification-v2.md at dd927a26 (2026-04-21), protocol v2.0.
// The spec says "Base64-encoded" without naming an alphabet; its examples are
// standard, padded base64 (RFC 4648 §4), which is the only form accepted here.
package x402

import (
	"encoding/base64"
	"encoding/json"
	"fmt"

	"github.com/SIMPLYBOYS/agent-payment-authority/internal/contracts"
)

const (
	Version                = 2
	HeaderPaymentRequired  = "PAYMENT-REQUIRED"
	HeaderPaymentSignature = "PAYMENT-SIGNATURE"
)

// Error codes from the spec's list that the shop returns.
const (
	ErrInvalidPayload      = "invalid_payload"
	ErrInvalidVersion      = "invalid_x402_version"
	ErrInvalidScheme       = "invalid_scheme"
	ErrInvalidNetwork      = "invalid_network"
	ErrInvalidRequirements = "invalid_payment_requirements"
)

type ResourceInfo struct {
	URL         string `json:"url"`
	Description string `json:"description,omitempty"`
	MimeType    string `json:"mimeType,omitempty"`
}

// Requirement is one PaymentRequirements entry as a server builds it.
type Requirement struct {
	Scheme            string            `json:"scheme"`
	Network           string            `json:"network"`
	Amount            contracts.Amount  `json:"amount"`
	Asset             string            `json:"asset"`
	PayTo             string            `json:"payTo"`
	MaxTimeoutSeconds uint64            `json:"maxTimeoutSeconds"`
	Extra             map[string]string `json:"extra,omitempty"`
}

// PaymentRequired keeps accepts entries and extensions as raw JSON, so a quote
// hashes the complete objects, members this package does not know included.
type PaymentRequired struct {
	X402Version uint64            `json:"x402Version"`
	Error       string            `json:"error,omitempty"`
	Resource    ResourceInfo      `json:"resource"`
	Accepts     []json.RawMessage `json:"accepts"`
	Extensions  json.RawMessage   `json:"extensions,omitempty"`
}

type PaymentPayload struct {
	X402Version uint64          `json:"x402Version"`
	Resource    *ResourceInfo   `json:"resource,omitempty"`
	Accepted    json.RawMessage `json:"accepted"`
	Payload     json.RawMessage `json:"payload"`
	Extensions  json.RawMessage `json:"extensions,omitempty"`
}

// Encode returns padded standard base64 of v's canonical JSON.
func Encode(v any) (string, error) {
	raw, err := json.Marshal(v)
	if err != nil {
		return "", err
	}
	c, err := contracts.Canonical(raw)
	if err != nil {
		return "", err
	}
	return base64.StdEncoding.EncodeToString(c), nil
}

// Decode reverses Encode and fails closed: padded standard base64 only, then
// contracts.Decode (at most contracts.MaxJSONBytes; duplicate, unknown or null
// members and trailing data are rejected).
func Decode(header string, dst any) error {
	raw, err := base64.StdEncoding.Strict().DecodeString(header)
	if err != nil {
		return fmt.Errorf("header is not padded standard base64")
	}
	return contracts.Decode(raw, dst)
}

// Validate checks what M1 relies on. Whether the offer is acceptable is for
// the buyer and Policy to decide, not this package.
func (p PaymentRequired) Validate() error {
	if p.X402Version != Version {
		return fmt.Errorf("x402Version must be %d", Version)
	}
	if p.Resource.URL == "" || len(p.Accepts) == 0 {
		return fmt.Errorf("resource.url and at least one accepts entry are required")
	}
	return nil
}

// Quote turns accepts[i] into the contracts quote a purchase order binds.
// Absent extensions and {} are the same quote. The resource URL is not part
// of the quote: the buyer must check it against the URL it requested.
func (p PaymentRequired) Quote(i int) (contracts.Quote, error) {
	if err := p.Validate(); err != nil {
		return contracts.Quote{}, err
	}
	if i < 0 || i >= len(p.Accepts) {
		return contracts.Quote{}, fmt.Errorf("no accepts entry %d", i)
	}
	ext := p.Extensions
	if ext == nil {
		ext = json.RawMessage(`{}`)
	}
	q := contracts.Quote{X402Version: p.X402Version, SelectedRequirement: p.Accepts[i], Extensions: ext}
	if _, err := q.Hash(); err != nil {
		return contracts.Quote{}, err
	}
	return q, nil
}
