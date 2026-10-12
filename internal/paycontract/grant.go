package paycontract

import (
	"crypto/sha256"
	"encoding/hex"
	"slices"

	"github.com/SIMPLYBOYS/agent-payment-authority/internal/contracts"
)

// Delegation is what a consumer confirmed before a task (section 3.2): the
// need, the merchants, the asset, the task budget and allow_max, all from
// the host's trusted catalog and profile; the confirmer only confirms. In
// P1-a the confirmer is the fake-only simulated approver.
type Delegation struct {
	Version                 string           `json:"version"`
	DelegationID            string           `json:"delegation_id"`
	ConsumerID              string           `json:"consumer_id"`
	Scenario                string           `json:"scenario"`
	Purpose                 string           `json:"purpose"`
	Merchants               []string         `json:"merchants"`
	AssetID                 string           `json:"asset_id"`
	TaskBudget              contracts.Amount `json:"task_budget"`
	AllowMax                contracts.Amount `json:"allow_max"`
	ApprovalDeadlineSeconds uint64           `json:"approval_deadline_seconds"`
	ConfirmedBy             string           `json:"confirmed_by"`
	ConfirmMethod           string           `json:"confirm_method"` // simulated (P1-a)
	ConfirmedAt             uint64           `json:"confirmed_at"`
	ExpiresAt               uint64           `json:"expires_at"`
}

// DelegationID is derived from the confirming request, so a resend names
// the same delegation.
func DelegationID(requestID string) string { return "dlg-" + short(requestID) }

// SessionID is the one approval session of a task's purchase.
func SessionID(taskID, purchaseOrderID string) string { return "aps-" + short(taskID+"\x00"+purchaseOrderID) }

// GrantID (the grant's jti) is derived from its session: a session can
// never have two grants.
func GrantID(sessionID string) string { return "grant-" + short(sessionID) }

func short(s string) string {
	sum := sha256.Sum256([]byte(Domain + "\x00" + s))
	return hex.EncodeToString(sum[:16])
}

func (d Delegation) Validate() error {
	budget, errB := d.TaskBudget.Units()
	allow, errA := d.AllowMax.Units()
	switch {
	case d.Version != Version || !ids(d.DelegationID, d.ConsumerID, d.Scenario, d.AssetID, d.ConfirmedBy):
		return coded("INVALID_DELEGATION", "version or IDs")
	case d.Purpose == "" || len(d.Purpose) > 200:
		return coded("INVALID_DELEGATION", "purpose")
	case len(d.Merchants) == 0 || slices.ContainsFunc(d.Merchants, func(m string) bool { return !addressPattern.MatchString(m) }):
		return coded("INVALID_DELEGATION", "merchants must be lowercase 0x addresses")
	case errB != nil || errA != nil || budget == 0 || allow > budget:
		return coded("INVALID_DELEGATION", "task_budget must be positive and allow_max within it")
	case d.ApprovalDeadlineSeconds == 0 || d.ApprovalDeadlineSeconds > 600:
		return coded("INVALID_DELEGATION", "approval deadline must be 1 to 600 seconds")
	case d.ConfirmMethod != "simulated":
		return coded("INVALID_DELEGATION", "P1-a confirms by simulation only")
	case !timestamp(d.ConfirmedAt) || d.ExpiresAt <= d.ConfirmedAt:
		return coded("INVALID_DELEGATION", "times")
	}
	return nil
}

// AuthorizationGrant is the single-use authorization an approval produces
// (section 3.2). P1-a makes simulated grants only, unsigned: they name an
// approval and give no payment capability (the Signer stub refuses them).
type AuthorizationGrant struct {
	Version             string `json:"version"`
	JTI                 string `json:"jti"`
	IntentHash          string `json:"intent_hash"`
	ApprovalSessionID   string `json:"approval_session_id"`
	Decision            string `json:"decision"`             // approve
	AuthorizationMethod string `json:"authorization_method"` // simulated
	Issuer              string `json:"issuer"`
	Audience            string `json:"audience"` // signer
	ApproverID          string `json:"approver_id"`
	IssuedAt            uint64 `json:"issued_at"`
	ExpiresAt           uint64 `json:"expires_at"` // the payment binding's valid_before: never outlives the intent
}

// Issuer is the demo host's approval service.
const Issuer = "apa-demo-host/approval"

func (g AuthorizationGrant) Validate() error {
	switch {
	case g.Version != Version || g.JTI != GrantID(g.ApprovalSessionID) || !ids(g.ApprovalSessionID, g.ApproverID) || !digestPattern.MatchString(g.IntentHash):
		return coded("INVALID_GRANT", "version, IDs or intent hash")
	case g.Decision != "approve" || g.Issuer != Issuer || g.Audience != "signer":
		return coded("INVALID_GRANT", "decision, issuer or audience")
	case g.AuthorizationMethod != "simulated":
		return coded("INVALID_GRANT", "P1-a issues simulated grants only")
	case !timestamp(g.IssuedAt) || g.ExpiresAt <= g.IssuedAt:
		return coded("INVALID_GRANT", "times")
	}
	return nil
}
