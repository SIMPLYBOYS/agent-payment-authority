// Package signer is P1-a's Signer stub (docs/commercial-transaction-flow.md
// 7, 11): the acceptance rule only. It holds no key, signs nothing, keeps
// no state and consumes no jti. Its refusal of a simulated grant proves the
// stub's rule, not a real Signer's single use (P1-b).
package signer

import "github.com/SIMPLYBOYS/agent-payment-authority/internal/paycontract"

// Refusal codes.
const (
	MethodNotAccepted = "GRANT_METHOD_NOT_ACCEPTED" // a simulated grant never gets payment capability
	NotEnabled        = "SIGNER_NOT_ENABLED"        // P1-a has no Signer: any other grant is refused too
	InvalidGrant      = "INVALID_GRANT"
)

// Accept returns the code the stub refuses g with. It never accepts.
func Accept(g paycontract.AuthorizationGrant) string {
	switch {
	case g.AuthorizationMethod == "simulated":
		return MethodNotAccepted
	case g.Validate() != nil:
		return InvalidGrant
	}
	return NotEnabled
}
