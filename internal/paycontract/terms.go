package paycontract

import (
	"slices"
	"strconv"

	"github.com/SIMPLYBOYS/agent-payment-authority/internal/contracts"
)

// DelegationTerms (docs/commercial-gap4.md 2.1) is what the person confirms
// with their registered device and what the Signer then holds every payment
// to: the delegation's terms, named by the wallet, chain and asset contract
// the Signer knows (it has no asset_id). It has no time and no confirmer, so
// the same terms always hash the same.
type DelegationTerms struct {
	Version                 string           `json:"version"`
	DelegationID            string           `json:"delegation_id"`
	ConsumerID              string           `json:"consumer_id"`
	WalletID                string           `json:"wallet_id"`
	ChainID                 string           `json:"chain_id"`
	AssetContract           string           `json:"asset_contract"`
	Merchants               []string         `json:"merchants"`
	Purpose                 string           `json:"purpose"`
	TaskBudget              contracts.Amount `json:"task_budget"`
	AllowMax                contracts.Amount `json:"allow_max"`
	ApprovalDeadlineSeconds uint64           `json:"approval_deadline_seconds"`
	ValidSeconds            uint64           `json:"valid_seconds"` // how long the task may start after the confirmation
}

func (t DelegationTerms) Validate() error {
	budget, errB := t.TaskBudget.Units()
	allow, errA := t.AllowMax.Units()
	chain, errC := strconv.ParseUint(t.ChainID, 10, 64)
	switch {
	case !Supported(t.Version) || !ids(t.DelegationID, t.ConsumerID, t.WalletID):
		return coded("INVALID_DELEGATION_TERMS", "version or IDs")
	case errC != nil || chain == 0 || strconv.FormatUint(chain, 10) != t.ChainID || !addressPattern.MatchString(t.AssetContract):
		return coded("INVALID_DELEGATION_TERMS", "chain or asset contract")
	case len(t.Merchants) == 0 || slices.ContainsFunc(t.Merchants, func(m string) bool { return !addressPattern.MatchString(m) }):
		return coded("INVALID_DELEGATION_TERMS", "merchants must be lowercase 0x addresses")
	case t.Purpose == "" || len(t.Purpose) > 200:
		return coded("INVALID_DELEGATION_TERMS", "purpose")
	case errB != nil || errA != nil || budget == 0 || allow > budget:
		return coded("INVALID_DELEGATION_TERMS", "task_budget must be positive and allow_max within it")
	case t.ApprovalDeadlineSeconds == 0 || t.ApprovalDeadlineSeconds > 600 || t.ValidSeconds == 0 || t.ValidSeconds > 86400:
		return coded("INVALID_DELEGATION_TERMS", "deadlines")
	}
	return nil
}

// Hash is the terms' digest under their version's domain: what the
// person's device signs over and the Signer keeps.
func (t DelegationTerms) Hash() (string, error) {
	if err := t.Validate(); err != nil {
		return "", err
	}
	return specs[t.Version].digest("delegation-terms", t)
}
