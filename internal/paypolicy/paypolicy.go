// Package paypolicy is the apa.pay.v1 decision entry
// (docs/commercial-transaction-flow.md 10.1): it validates a v1 purchase,
// computes its v1 intent_hash, applies the shared rules (policy.Rules) and
// writes this version's decision. Like the M1 Policy it deducts nothing,
// issues no grant, reads no clock and keeps no state. P1-a has no payment
// capability: every decision says payment_status not_enabled, an Allow too.
package paypolicy

import (
	"fmt"

	"github.com/SIMPLYBOYS/agent-payment-authority/internal/contracts"
	"github.com/SIMPLYBOYS/agent-payment-authority/internal/paycontract"
	"github.com/SIMPLYBOYS/agent-payment-authority/internal/policy"
)

// Input is everything one decision depends on. ContractVersion is the
// stored task's, never a value of the purchase or the request.
type Input struct {
	Session         contracts.Session
	Purchase        paycontract.PurchaseOrder
	ContractVersion string
	TaskStatus      string
	Profile         *policy.Profile
	Credential      *policy.Credential         // apa.pay.v1: the marked fixture; nil in apa.pay.v2
	Check           *contracts.CredentialCheck // apa.pay.v2: the verifier's record of this purchase's presentation
	Budget          policy.Budget
	Now             uint64
	Recent          uint64 // the consumer's purchases decided Allow or Ask within the profile's velocity window (policy.RuleInput)
	PayeeFlagged    bool   // the payee is on the Host's risk list (docs/commercial-kyt.md)
}

// Decide returns the decision for one pay-family purchase, or an error
// when there is nothing to decide (another contract's task, an invalid or
// foreign purchase, an ended task, contradictory trusted inputs). In
// apa.pay.v2 the credential step comes first (docs/commercial-c2-design.md
// 3.3): a result other than valid decides Deny with its reason; a valid
// one gives Rules the credential's verified expiry.
func Decide(reg *contracts.Registry, in Input) (contracts.PolicyDecision, error) {
	if !paycontract.Supported(in.ContractVersion) || in.Purchase.Version != in.ContractVersion {
		return contracts.PolicyDecision{}, &paycontract.Error{Code: "CONTRACT_VERSION_MISMATCH", Detail: "the task is not a pay-family task of the purchase's version"}
	}
	if err := paycontract.ValidatePurchase(reg, in.Session, in.Purchase); err != nil {
		return contracts.PolicyDecision{}, err
	}
	if err := contracts.CheckTaskActive(in.TaskStatus); err != nil {
		return contracts.PolicyDecision{}, err
	}
	hash, err := in.Purchase.IntentHash()
	if err != nil {
		return contracts.PolicyDecision{}, err
	}
	if in.Budget.TaskID != in.Session.TaskID {
		return contracts.PolicyDecision{}, fmt.Errorf("budget belongs to task %q, not %q", in.Budget.TaskID, in.Session.TaskID)
	}
	var credentialDeny string
	if in.ContractVersion == paycontract.V2 {
		c := in.Check
		if in.Credential != nil || c == nil {
			return contracts.PolicyDecision{}, fmt.Errorf("an apa.pay.v2 decision takes the verifier's record, never a fixture credential")
		}
		if r := paycontract.CredentialOutcome(c); r == "valid" {
			in.Credential = &policy.Credential{ConsumerID: in.Session.ConsumerID, ValidUntil: c.Expiry, EvidenceMode: "bbs"}
		} else if credentialDeny = paycontract.CredentialDeny[r]; credentialDeny == "" {
			return contracts.PolicyDecision{}, fmt.Errorf("credential result %q", c.Result)
		}
	} else if c := in.Credential; in.Check != nil || c != nil && (c.ConsumerID != in.Session.ConsumerID || c.EvidenceMode != "fixture") {
		// apa.pay.v1 accepts the marked fixture credential only.
		return contracts.PolicyDecision{}, fmt.Errorf("credential is not this consumer's fixture")
	}
	remaining, err := in.Budget.Remaining.Units()
	if err != nil {
		return contracts.PolicyDecision{}, fmt.Errorf("remaining budget: %w", err)
	}
	if in.Profile != nil {
		if err := in.Profile.Validate(); err != nil {
			return contracts.PolicyDecision{}, err
		}
		if limit, _ := in.Profile.TaskCap.Units(); remaining > limit {
			return contracts.PolicyDecision{}, fmt.Errorf("remaining budget exceeds the task cap")
		}
	}
	if in.Now == 0 {
		return contracts.PolicyDecision{}, fmt.Errorf("no trusted clock")
	}
	b := in.Purchase.PaymentBinding
	amount, _ := b.Amount.Units() // Validate checked it
	asset, trusted := reg.TrustedAsset(b.ChainID, b.AssetContract)
	out := policy.Rules(policy.RuleInput{Profile: in.Profile, Credential: in.Credential, AssetTrusted: trusted, AssetID: asset.AssetID, PayTo: b.PayTo,
		Amount: amount, Remaining: remaining, ValidAfter: b.ValidAfter, ValidBefore: b.ValidBefore, Now: in.Now, Recent: in.Recent})
	d := contracts.PolicyDecision{Version: in.ContractVersion, IntentHash: hash, PaymentStatus: paycontract.PaymentNotEnabled,
		PolicyVerdict: "Deny", ApprovalStatus: "not_requested", Reason: out.Reason}
	switch {
	case in.PayeeFlagged: // screening first: a flagged payee is paid nothing, whatever else holds (docs/commercial-kyt.md)
		d.Reason = "PAYEE_FLAGGED"
	case credentialDeny != "": // the credential decides before the rules (design 3.3)
		d.Reason = credentialDeny
	case out.Kind == policy.WithinAllowMax: // an Allow is not a payment: no grant, no capability
		d.PolicyVerdict = "Allow"
	case out.Kind == policy.NeedsApproval:
		d.PolicyVerdict, d.ApprovalStatus = "Ask", "pending"
	}
	if err := paycontract.ValidateDecision(d); err != nil {
		return contracts.PolicyDecision{}, err // unreachable: every kind maps to a valid decision of its version
	}
	return d, nil
}
