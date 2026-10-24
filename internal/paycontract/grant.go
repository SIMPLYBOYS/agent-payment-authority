package paycontract

import (
	"slices"

	"github.com/SIMPLYBOYS/agent-payment-authority/internal/contracts"
)

// Delegation is what a consumer confirmed before a task (section 3.2): the
// need, the merchants, the asset, the task budget and allow_max, all from
// the host's trusted catalog and profile; the confirmer only confirms. In
// P1-a the confirmer is the fake-only simulated approver; from gap 4 a host
// that pays takes the person's registered device only (webauthn), which the
// Signer verified and keeps (docs/commercial-gap4.md).
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
	ConfirmMethod           string           `json:"confirm_method"` // simulated (P1-a) | webauthn (gap 4)
	ConfirmedAt             uint64           `json:"confirmed_at"`
	ExpiresAt               uint64           `json:"expires_at"`
	// webauthn only: the Signer's confirmation (every payment names it) and
	// the credential's reference (sha256 of its ID).
	SignerConfirmationID string `json:"signer_confirmation_id,omitempty"`
	CredentialRef        string `json:"credential_ref,omitempty"`
}

// DelegationID, SessionID and GrantID are apa.pay.v1's (Spec has every
// version's): a resend names the same delegation; a purchase has one
// session; a session can never have two grants.
func DelegationID(requestID string) string { return v1.DelegationID(requestID) }

func SessionID(taskID, purchaseOrderID string) string { return v1.SessionID(taskID, purchaseOrderID) }

func GrantID(sessionID string) string { return v1.GrantID(sessionID) }

func (d Delegation) Validate() error {
	budget, errB := d.TaskBudget.Units()
	allow, errA := d.AllowMax.Units()
	switch {
	case !Supported(d.Version) || !ids(d.DelegationID, d.ConsumerID, d.Scenario, d.AssetID, d.ConfirmedBy):
		return coded("INVALID_DELEGATION", "version or IDs")
	case d.Purpose == "" || len(d.Purpose) > 200:
		return coded("INVALID_DELEGATION", "purpose")
	case len(d.Merchants) == 0 || slices.ContainsFunc(d.Merchants, func(m string) bool { return !addressPattern.MatchString(m) }):
		return coded("INVALID_DELEGATION", "merchants must be lowercase 0x addresses")
	case errB != nil || errA != nil || budget == 0 || allow > budget:
		return coded("INVALID_DELEGATION", "task_budget must be positive and allow_max within it")
	case d.ApprovalDeadlineSeconds == 0 || d.ApprovalDeadlineSeconds > 600:
		return coded("INVALID_DELEGATION", "approval deadline must be 1 to 600 seconds")
	case d.ConfirmMethod == "simulated" && (d.SignerConfirmationID != "" || d.CredentialRef != ""):
		return coded("INVALID_DELEGATION", "a simulated confirmation is the host's, with no Signer confirmation")
	case d.ConfirmMethod == "webauthn" && (!ids(d.SignerConfirmationID) || !digestPattern.MatchString(d.CredentialRef)):
		return coded("INVALID_DELEGATION", "a webauthn confirmation names the Signer's confirmation and the credential")
	case d.ConfirmMethod != "simulated" && d.ConfirmMethod != "webauthn":
		return coded("INVALID_DELEGATION", "confirmed by simulation or by the person's device (webauthn)")
	case !timestamp(d.ConfirmedAt) || d.ExpiresAt <= d.ConfirmedAt:
		return coded("INVALID_DELEGATION", "times")
	}
	return nil
}

// AuthorizationGrant is the single-use authorization an approval produces
// (section 3.2). A simulated grant (P1-a) is unsigned: it names an approval
// and gives no payment capability (the Signer stub refuses it). A webauthn
// grant (A2) is the Host's mirror of a decision the Signer verified: the
// Host records it, it does not issue it (Issuer then names the recorder,
// Recorder), and it carries the Signer's approval ID, the credential's
// reference and amr. Neither pays in A2.
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
	ExpiresAt           uint64 `json:"expires_at"` // never past the payment binding's valid_before
	// webauthn only.
	SignerApprovalID string   `json:"signer_approval_id,omitempty"`
	CredentialRef    string   `json:"credential_ref,omitempty"` // sha256 of the credential ID
	AMR              []string `json:"amr,omitempty"`            // ["webauthn"], or ["webauthn", "voice"] (V1)
	// V1: the voice challenge's outcome, when the Signer asked for one:
	// verified, or unavailable with its code (a degraded approval).
	Voice       string `json:"voice,omitempty"`
	VoiceReason string `json:"voice_reason,omitempty"`
}

// Issuer is the demo host's approval service (simulated grants); Recorder
// is the Host recording the Signer's verified decision (webauthn grants).
const (
	Issuer   = "apa-demo-host/approval"
	Recorder = "apa-demo-host/signer-approval-mirror"
)

func (g AuthorizationGrant) Validate() error {
	switch {
	case !Supported(g.Version) || g.JTI != specs[g.Version].GrantID(g.ApprovalSessionID) || !ids(g.ApprovalSessionID, g.ApproverID) || !digestPattern.MatchString(g.IntentHash):
		return coded("INVALID_GRANT", "version, IDs or intent hash")
	case g.Decision != "approve" || g.Audience != "signer":
		return coded("INVALID_GRANT", "decision or audience")
	case !timestamp(g.IssuedAt) || g.ExpiresAt <= g.IssuedAt:
		return coded("INVALID_GRANT", "times")
	}
	switch g.AuthorizationMethod {
	case "simulated":
		if g.Issuer != Issuer || g.SignerApprovalID != "" || g.CredentialRef != "" || g.AMR != nil || g.Voice != "" || g.VoiceReason != "" {
			return coded("INVALID_GRANT", "a simulated grant is the host's, with no Signer approval")
		}
	case "webauthn":
		amr := []string{"webauthn"}
		if g.Voice == "verified" {
			amr = []string{"webauthn", "voice"}
		}
		if g.Issuer != Recorder || !ids(g.SignerApprovalID) || !digestPattern.MatchString(g.CredentialRef) || !slices.Equal(g.AMR, amr) {
			return coded("INVALID_GRANT", "a webauthn grant mirrors a Signer approval: recorder, approval ID, credential reference and amr")
		}
		switch {
		case g.Voice == "" || g.Voice == "verified":
			if g.VoiceReason != "" {
				return coded("INVALID_GRANT", "only an unavailable voice has a reason")
			}
		case g.Voice != "unavailable" || !slices.Contains(VoiceUnavailable, g.VoiceReason):
			return coded("INVALID_GRANT", "a grant's voice is verified, or unavailable with its code")
		}
	default:
		return coded("INVALID_GRANT", "method is simulated or webauthn")
	}
	return nil
}
