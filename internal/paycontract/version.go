package paycontract

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"slices"

	"github.com/SIMPLYBOYS/agent-payment-authority/internal/contracts"
)

// The pay family has one rule set per version (C2a,
// docs/commercial-c2-design.md 3.1). An order, a delegation, a grant, a
// decision or an event is checked and digested by the spec of its own
// version field. apa.pay.v1's spec is the constants above, unchanged (its
// golden values are frozen); apa.pay.v2 adds the BBS+ credential (C2):
// another domain, so no v1 digest or ID can name a v2 object, and three
// more Deny reasons.
const (
	V2       = "apa.pay.v2"
	DomainV2 = "agent-payment-authority/pay/v2"
)

// CredentialReasons are apa.pay.v2's Deny reasons for a credential that
// does not verify, is revoked, or whose status cannot be read.
var CredentialReasons = []string{"CREDENTIAL_INVALID", "CREDENTIAL_REVOKED", "CREDENTIAL_STATUS_UNAVAILABLE"}

// CredentialDeny is the Deny reason each non-valid verification result
// must decide (design 3.3): no credential is CONFIG_MISSING (as in M1), an
// expired one AUTHORIZATION_EXPIRED; "valid" has none.
var CredentialDeny = map[string]string{"absent": "CONFIG_MISSING", "invalid": "CREDENTIAL_INVALID", "revoked": "CREDENTIAL_REVOKED",
	"status_unavailable": "CREDENTIAL_STATUS_UNAVAILABLE", "expired": "AUTHORIZATION_EXPIRED"}

// CredentialDetails are the fixed detail codes of a verification record.
var CredentialDetails = []string{"PRESENTATION_FORMAT", "DISCLOSURE_MISMATCH", "FIELD_MISSING", "FORMAT_INVALID", "PROOF_INVALID",
	"ISSUER_UNTRUSTED", "HOLDER_MISMATCH", "HOLDER_BINDING_INVALID", "SNAPSHOT_MISMATCH", "STATUS_LIST_UNAVAILABLE", "STATUS_LIST_ROLLBACK",
	"CHALLENGE_MISSING", "CHALLENGE_EXPIRED", "CHALLENGE_MISMATCH", "INTENT_MISMATCH", "HOLDER_MATERIAL_UNAVAILABLE"}

// NoChallenge are the details of a record made without a stored challenge.
var NoChallenge = []string{"HOLDER_MATERIAL_UNAVAILABLE", "CHALLENGE_MISSING"}

// Spec is one version's rules.
type Spec struct {
	Version, Domain string
	reasons, deny   []string
	askClosures     [][2]string // (approval_status, reason) pairs an Ask may record
}

// askV1 is apa.pay.v1's Ask table, as it was before versions.
var askV1 = [][2]string{{"pending", "HUMAN_APPROVAL_REQUIRED"}, {"expired", "APPROVAL_TIMEOUT"}, {"withdrawn", "HOST_CANCELLED"},
	{"approved", "SIMULATED_APPROVAL"}, {"approved", "WEBAUTHN_APPROVAL"}, {"denied", "APPROVER_DENIED"}, {"superseded", "QUOTE_CHANGED"}}

// apa.pay.v2 also withdraws an Ask whose credential is revoked, whose
// status cannot be read or which expired before an approval landed
// (docs/commercial-c2-design.md 3.2, 7.1).
var askV2 = slices.Concat(askV1, [][2]string{{"withdrawn", "CREDENTIAL_REVOKED"}, {"withdrawn", "CREDENTIAL_STATUS_UNAVAILABLE"}, {"withdrawn", "AUTHORIZATION_EXPIRED"}})

var specs = map[string]Spec{
	Version: {Version, Domain, ReasonCodes, denyReasons, askV1},
	V2:      {V2, DomainV2, slices.Concat(ReasonCodes, CredentialReasons), slices.Concat(denyReasons, CredentialReasons), askV2},
}

// SpecOf is the spec of a pay-family version.
func SpecOf(version string) (Spec, bool) { s, ok := specs[version]; return s, ok }

// Supported reports whether version is a pay-family version.
func Supported(version string) bool { _, ok := specs[version]; return ok }

// ReasonCodes are this version's decision and closure reasons.
func (s Spec) ReasonCodes() []string { return slices.Clone(s.reasons) }

func (s Spec) digest(kind string, v any) (string, error) {
	raw, err := json.Marshal(v)
	if err != nil {
		return "", err
	}
	return contracts.Digest(s.Domain+"/"+kind, raw)
}

func (s Spec) short(x string) string {
	sum := sha256.Sum256([]byte(s.Domain + "\x00" + x))
	return hex.EncodeToString(sum[:16])
}

// DelegationID is derived from the confirming request, so a resend names
// the same delegation.
func (s Spec) DelegationID(requestID string) string { return "dlg-" + s.short(requestID) }

// SessionID is the one approval session of a task's purchase.
func (s Spec) SessionID(taskID, purchaseOrderID string) string {
	return "aps-" + s.short(taskID+"\x00"+purchaseOrderID)
}

// GrantID (the grant's jti) is derived from its session.
func (s Spec) GrantID(sessionID string) string { return "grant-" + s.short(sessionID) }

// ResourceHash binds method and URL under this version's domain.
func (s Spec) ResourceHash(method, resource string) (string, error) {
	return s.digest("resource", map[string]string{"http_method": method, "resource_url": resource})
}

// RequirementHash is the quote's digest under this version's domain, after
// the M1 quote checks (version 2, objects, no case-ambiguous keys).
func (s Spec) RequirementHash(q contracts.Quote) (string, error) {
	if _, err := q.Hash(); err != nil {
		return "", err
	}
	return s.digest("requirement", q)
}

// EventHash and GenesisHash: a chain under one version never links to
// another's.
func (s Spec) EventHash(e contracts.AuditEvent) (string, error) { return s.digest("event", e) }

func (s Spec) GenesisHash(taskID string) (string, error) {
	return s.digest("event-genesis", map[string]string{"task_id": taskID})
}

var v1 = specs[Version]
