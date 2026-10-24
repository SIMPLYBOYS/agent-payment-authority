package signer

import "encoding/json"

// The Signer's wire types (docs/commercial-p1b-design.md 4.2), shared with
// the Host's client.

// SignRequest asks for one EIP-3009 authorization. Order is the complete
// apa.pay.v1 purchase order; the Signer rebuilds everything it signs from it.
type SignRequest struct {
	RequestID     string          `json:"request_id"`
	Authorization Authorization   `json:"authorization"`
	Order         json.RawMessage `json:"order"`
	Reservation   json.RawMessage `json:"reservation,omitempty"` // P2: the on-chain reservation reference
}

// Authorization names what authorizes the payment. P1-b accepts a standing
// delegation only; "webauthn" is refused until A2 and "simulated" always.
type Authorization struct {
	Method            string `json:"method"`
	DelegationID      string `json:"delegation_id,omitempty"`
	DelegationVersion uint64 `json:"delegation_version,omitempty"`
	// ApprovalID is a webauthn payment's verified human approval (P2): the
	// Signer consumes it in the transaction that accepts the attempt.
	ApprovalID string `json:"approval_id,omitempty"`
	// DelegationConfirmationID (gap 4): the person's confirmation of the
	// delegation this payment falls under; a Signer that takes approvals
	// pays nothing without one (docs/commercial-gap4.md 2.2).
	DelegationConfirmationID string `json:"delegation_confirmation_id,omitempty"`
}

// SignedAuthorization is the EIP-3009 authorization: the message and its
// 65-byte signature r ‖ s ‖ v, hex.
type SignedAuthorization struct {
	Transfer
	Signature string `json:"signature"`
}

// Reply is an attempt as the Signer answers it. Authorization is present
// only when State is signed.
type Reply struct {
	AttemptID     string               `json:"attempt_id,omitempty"`
	RequestID     string               `json:"request_id,omitempty"`
	State         string               `json:"state,omitempty"` // accepted | signed | abandoned | expired_unsigned
	IntentHash    string               `json:"intent_hash,omitempty"`
	TypedDigest   string               `json:"typed_digest,omitempty"`
	AuthDeadline  uint64               `json:"auth_deadline,omitempty"`
	Authorization *SignedAuthorization `json:"authorization,omitempty"`
	Error         string               `json:"error,omitempty"` // a fixed code
}
