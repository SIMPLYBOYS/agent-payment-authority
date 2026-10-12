// Package policy is the deterministic M1 Policy. Given a purchase, trusted
// host config, trusted state and an injected clock it returns Ask, Deny or
// Allow-unavailable. It deducts no budget, issues no grant and cannot pay; it
// reads no clock, file or network and keeps no state. Persistence, retries and
// the event stream are B1-05's second step.
package policy

import (
	"fmt"
	"regexp"
	"slices"

	"github.com/SIMPLYBOYS/agent-payment-authority/internal/contracts"
)

// Profile is the versioned M1 demo profile: trusted host config, not the
// second batch's allow_max decision. Amounts are base units of AssetID.
type Profile struct {
	ProfileID    string           `json:"profile_id"`
	EvidenceMode string           `json:"evidence_mode"`
	AssetID      string           `json:"asset_id"`
	AllowMax     contracts.Amount `json:"allow_max"` // at or below: Allow, which M1 cannot pay
	TaskCap      contracts.Amount `json:"task_cap"`
	Merchants    []string         `json:"merchants"` // allowed payTo addresses
	Mode         string           `json:"mode"`      // interactive | unattended
}

// Credential is a consumer's authorization condition. In M1 it is a fixture:
// nothing here verifies a BBS+ proof.
type Credential struct {
	ConsumerID   string `json:"consumer_id"`
	ValidUntil   uint64 `json:"valid_until"` // valid while Now < ValidUntil
	EvidenceMode string `json:"evidence_mode"`
}

// Budget is what the task may still spend, from trusted state. It defaults to
// the profile's task cap; a trusted fixture may set it lower for a test
// scenario. M1 decisions never deduct from it.
type Budget struct {
	TaskID    string
	Remaining contracts.Amount
}

// Input is everything one decision depends on. Session comes from the
// authenticated wrapper, never from the purchase body. A nil Profile or
// Credential is missing configuration and ends in Deny CONFIG_MISSING.
type Input struct {
	Session    contracts.Session
	Purchase   contracts.PurchaseOrder
	TaskStatus string // runtime status, not the registry snapshot
	Profile    *Profile
	Credential *Credential
	Budget     Budget
	Now        uint64 // Unix seconds from the trusted host clock
}

var address = regexp.MustCompile(`^0x[0-9a-f]{40}$`) // contracts' address format

// Validate checks a profile is complete and coherent. LoadProfile calls it;
// Decide refuses an invalid one rather than guessing.
func (p Profile) Validate() error {
	allow, errA := p.AllowMax.Units()
	limit, errC := p.TaskCap.Units()
	switch {
	case p.ProfileID == "" || p.AssetID == "":
		return fmt.Errorf("profile_id and asset_id are required")
	case p.EvidenceMode != "fixture":
		return fmt.Errorf("M1 profiles are fixtures")
	case errA != nil || errC != nil || allow > limit:
		return fmt.Errorf("allow_max and task_cap must be amounts with allow_max <= task_cap")
	case p.Merchants == nil || slices.ContainsFunc(p.Merchants, func(m string) bool { return !address.MatchString(m) }):
		return fmt.Errorf("merchants must be a list of lowercase 0x addresses")
	case p.Mode != "interactive" && p.Mode != "unattended":
		return fmt.Errorf("mode must be interactive or unattended")
	}
	return nil
}

// LoadProfile decodes a profile strictly (contracts.Decode) and validates it.
func LoadProfile(raw []byte) (Profile, error) {
	var p Profile
	if err := contracts.Decode(raw, &p); err != nil {
		return Profile{}, err
	}
	return p, p.Validate()
}

// Outcome kinds of Rules.
const (
	Denied         = "deny"
	WithinAllowMax = "within_allow_max"
	NeedsApproval  = "needs_approval"
)

// RuleInput is what the shared business rules need, every value already
// validated by the contract's own entry (docs/commercial-transaction-flow.md
// 10.1): the profile and credential (nil: missing), whether the purchase's
// asset is a trusted registry asset and which, the payee, the amount and the
// remaining budget in base units, the intent's validity window and the
// trusted time.
type RuleInput struct {
	Profile                 *Profile
	Credential              *Credential
	AssetTrusted            bool
	AssetID, PayTo          string
	Amount, Remaining       uint64
	ValidAfter, ValidBefore uint64
	Now                     uint64
}

// RuleOutcome is a kind and a reason code: Denied with the first failing
// check's code, WithinAllowMax (WITHIN_ALLOW_MAX), or NeedsApproval
// (HUMAN_APPROVAL_REQUIRED). Each contract maps it to its own decision.
type RuleOutcome struct{ Kind, Reason string }

// Rules is the contract-neutral judgement both decision entries share, in
// this order: CONFIG_MISSING, ASSET_NOT_ALLOWED, MERCHANT_NOT_ALLOWED,
// AUTHORIZATION_EXPIRED, INSUFFICIENT_BUDGET; then within allow_max; then
// an unattended profile is APPROVER_UNAVAILABLE; else a human must approve.
// It parses nothing, hashes nothing, decides no payment capability and
// reads no clock.
func Rules(in RuleInput) RuleOutcome {
	p := in.Profile
	allowMax := uint64(0)
	if p != nil {
		allowMax, _ = p.AllowMax.Units()
	}
	switch {
	case p == nil || in.Credential == nil:
		return RuleOutcome{Denied, "CONFIG_MISSING"}
	case !in.AssetTrusted || in.AssetID != p.AssetID: // budgets are in the profile's asset only
		return RuleOutcome{Denied, "ASSET_NOT_ALLOWED"}
	case !slices.Contains(p.Merchants, in.PayTo):
		return RuleOutcome{Denied, "MERCHANT_NOT_ALLOWED"}
	// The intent window follows EIP-3009: valid only while valid_after < now < valid_before.
	case in.Now >= in.Credential.ValidUntil || in.Now <= in.ValidAfter || in.Now >= in.ValidBefore:
		return RuleOutcome{Denied, "AUTHORIZATION_EXPIRED"}
	case in.Amount > in.Remaining:
		return RuleOutcome{Denied, "INSUFFICIENT_BUDGET"}
	case in.Amount <= allowMax:
		return RuleOutcome{WithinAllowMax, "WITHIN_ALLOW_MAX"}
	case p.Mode == "unattended":
		return RuleOutcome{Denied, "APPROVER_UNAVAILABLE"}
	}
	return RuleOutcome{NeedsApproval, "HUMAN_APPROVAL_REQUIRED"}
}

// Decide returns the decision for one purchase, or an error when there is
// nothing to decide: an invalid purchase, a session that does not own it
// (OWNERSHIP_MISMATCH), an ended task (TASK_NOT_ACTIVE; no decision may follow
// a task's end), or trusted inputs that contradict each other.
//
// Deny wins over everything; among Deny reasons the first failing check, in
// this order, names the decision: CONFIG_MISSING, ASSET_NOT_ALLOWED,
// MERCHANT_NOT_ALLOWED, AUTHORIZATION_EXPIRED, INSUFFICIENT_BUDGET. Then an
// amount within allow_max is Allow (PAYMENT_CAPABILITY_UNAVAILABLE), an
// unattended profile is Deny APPROVER_UNAVAILABLE, and everything else is Ask.
func Decide(reg *contracts.Registry, in Input) (contracts.PolicyDecision, error) {
	if err := reg.ValidatePurchase(in.Session, in.Purchase); err != nil {
		return contracts.PolicyDecision{}, err
	}
	if err := contracts.CheckTaskActive(in.TaskStatus); err != nil {
		return contracts.PolicyDecision{}, err
	}
	i := in.Purchase.Intent
	hash, err := i.Hash()
	if err != nil {
		return contracts.PolicyDecision{}, err
	}
	if in.Budget.TaskID != in.Session.TaskID {
		return contracts.PolicyDecision{}, fmt.Errorf("budget belongs to task %q, not %q", in.Budget.TaskID, in.Session.TaskID)
	}
	if c := in.Credential; c != nil && (c.ConsumerID != in.Session.ConsumerID || c.EvidenceMode != "fixture") {
		return contracts.PolicyDecision{}, fmt.Errorf("credential is not this consumer's M1 fixture")
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
	amount, _ := i.Amount.Units() // ValidatePurchase checked it

	asset, trusted := reg.TrustedAsset(i.ChainID, i.AssetContract)
	out := Rules(RuleInput{Profile: in.Profile, Credential: in.Credential, AssetTrusted: trusted, AssetID: asset.AssetID, PayTo: i.PayTo,
		Amount: amount, Remaining: remaining, ValidAfter: i.ValidAfter, ValidBefore: i.ValidBefore, Now: in.Now})
	d := contracts.PolicyDecision{Version: contracts.Version, IntentHash: hash, PaymentStatus: "unavailable", PolicyVerdict: "Deny", ApprovalStatus: "not_requested", Reason: out.Reason}
	switch out.Kind {
	case WithinAllowMax: // M1 cannot pay: an Allow says so
		d.PolicyVerdict, d.Reason = "Allow", "PAYMENT_CAPABILITY_UNAVAILABLE"
	case NeedsApproval:
		d.PolicyVerdict, d.ApprovalStatus = "Ask", "pending"
	}
	if err := d.Validate(); err != nil {
		return contracts.PolicyDecision{}, err // unreachable: every branch above is a valid M1 decision
	}
	return d, nil
}
