package contracts

import (
	"fmt"
	"io/fs"
	"maps"
	"slices"
)

type ExpectedOutcome struct {
	Result          string   `json:"result"`
	Reason          string   `json:"reason"`
	ApprovalStatus  string   `json:"approval_status"`
	PaymentStatus   string   `json:"payment_status"`
	BudgetDeducted  Amount   `json:"budget_deducted"`
	ForbiddenEvents []string `json:"forbidden_events"`
}
type AcceptanceCase struct {
	CaseID     string            `json:"case_id"`
	Layer      string            `json:"layer"`
	InputRef   string            `json:"input_ref"`
	Operation  string            `json:"operation"`
	Parameters map[string]string `json:"parameters"`
	Expected   ExpectedOutcome   `json:"expected"`
}
type CaseManifest struct {
	Version      string            `json:"version"`
	ManifestID   string            `json:"manifest_id"`
	ReviewStatus string            `json:"review_status"`
	Reviewer     string            `json:"reviewer"`
	Defaults     map[string]string `json:"defaults"`
	Cases        []AcceptanceCase  `json:"cases"`
}

// CapabilityIDs are the capabilities M1 cannot evaluate. Every M1 run manifest
// lists all of them as not_evaluated and none may appear in results.
// signer_key_selection covers real key selection and key isolation;
// onchain_settlement covers on-chain payment and reconciliation.
var CapabilityIDs = []string{"credential_verification", "webauthn", "voice_approval", "zk_budget_proof", "signer_key_selection", "cross_consumer_grant_proof", "onchain_settlement"}

var forbiddenEarly = []string{"APPROVED", "GRANT_ISSUED", "SETTLED", "RESOURCE_DELIVERED"}

func oneOf(values ...string) func(string) bool {
	return func(v string) bool { return slices.Contains(values, v) }
}
func amountValue(v string) bool { _, err := Amount(v).Units(); return err == nil }
func positive(v string) bool    { n, err := Amount(v).Units(); return err == nil && n > 0 }
func clockValue(v string) bool  { n, err := Amount(v).Units(); return err == nil && timestamp(n) }
func idValue(v string) bool     { return ids(v) }

// policyParams are the manifest defaults: all eight keys, no others. asset is
// a registry asset_id (checked by ValidateRefs); "unlisted" is only a per-case
// override meaning "an asset the registry does not trust".
var policyParams = map[string]func(string) bool{
	"remaining": amountValue, "allow_max": amountValue, "merchant": oneOf("allowed", "not_allowed"),
	"credential": oneOf("valid", "expired"), "required_config": oneOf("present", "missing"),
	"mode": oneOf("interactive", "unattended"), "clock": clockValue, "asset": idValue,
}

type opSpec struct {
	layer    string
	params   map[string]func(string) bool
	required []string
}

// operations is the closed list of case operations with their layer, allowed
// parameters (key → value check) and required parameters.
var operations = map[string]opSpec{
	"policy":               {"integration", policyOverrides(), nil},
	"cancel":               {"integration", map[string]func(string) bool{"before": oneOf("no_purchase", "pending")}, []string{"before"}},
	"advance-clock":        {"integration", map[string]func(string) bool{"approval_deadline_seconds": positive, "advance_seconds": positive}, []string{"approval_deadline_seconds", "advance_seconds"}},
	"cli-timeout":          {"integration", map[string]func(string) bool{"phase": oneOf("before_purchase", "pending")}, []string{"phase"}},
	"restart-and-new-task": {"integration", map[string]func(string) bool{"consumer": idValue}, []string{"consumer"}},
	"compare-retry":        {"contracts", map[string]func(string) bool{"mutation": oneOf("none", "nonce", "request_id", "other_consumer")}, []string{"mutation"}},
	"binding": {"contracts", map[string]func(string) bool{
		"session": oneOf("a", "b"), "purchase": oneOf("a", "b"), "mutation": oneOf("none", "wallet_b", "from_b", "consumer_b", "agent_b"),
	}, []string{"session", "purchase"}},
	"validate-decision": {"contracts", map[string]func(string) bool{"payment_status": idValue}, []string{"payment_status"}},
	"validate-purchase": {"contracts", map[string]func(string) bool{"mutation": oneOf("transfer_method_permit2", "transfer_method_erc7710", "case_variant_pay_to")}, []string{"mutation"}},
}

func policyOverrides() map[string]func(string) bool {
	p := maps.Clone(policyParams)
	p["amount"] = positive
	p["asset"] = func(v string) bool { return v == "unlisted" || ids(v) }
	return p
}

// outcomes maps "operation/result" to the reasons that result allows.
var outcomes = map[string][]string{
	"policy/Ask":                       {"HUMAN_APPROVAL_REQUIRED"},
	"policy/Deny":                      denyReasons,
	"policy/Allow":                     {"PAYMENT_CAPABILITY_UNAVAILABLE"},
	"advance-clock/pending":            {"HUMAN_APPROVAL_REQUIRED"},
	"advance-clock/denied":             {"APPROVAL_TIMEOUT"},
	"cancel/cancelled":                 {"HOST_CANCELLED"},
	"cli-timeout/failed":               {"CLI_TIMEOUT"},
	"restart-and-new-task/same-wallet": {"PERSISTED_BINDING"},
	"compare-retry/replay":             {"SAME_PAYLOAD"},
	"compare-retry/new":                {"NEW_REQUEST"},
	"compare-retry/reject":             {"IDEMPOTENCY_CONFLICT"},
	"binding/accept":                   {"VALID_BINDING"},
	"binding/reject":                   {"OWNERSHIP_MISMATCH"},
	"validate-decision/accept":         {"VALID_DECISION"},
	"validate-decision/reject":         {"M1_PAYMENT_UNAVAILABLE", "INVALID_DECISION"},
	"validate-purchase/accept":         {"VALID_PURCHASE"},
	"validate-purchase/reject":         {"UNSUPPORTED_TRANSFER_METHOD", "AMBIGUOUS_KEY"},
}

// approvalFor is the only approval_status an expected outcome may state.
func approvalFor(c AcceptanceCase) string {
	switch {
	case c.Expected.Result == "Ask", c.Expected.Result == "pending",
		c.Operation == "cli-timeout" && c.Parameters["phase"] == "pending":
		return "pending"
	case c.Expected.Result == "denied":
		return "expired"
	case c.Operation == "cancel" && c.Parameters["before"] == "pending":
		return "withdrawn"
	}
	return "not_requested"
}

func (m CaseManifest) Validate() error {
	if m.Version != Version || !ids(m.ManifestID) || len(m.Cases) == 0 {
		return coded("INVALID_MANIFEST", "version, manifest_id or cases")
	}
	if m.ReviewStatus != "pending" && m.ReviewStatus != "accepted" || m.ReviewStatus == "accepted" && !ids(m.Reviewer) {
		return coded("INVALID_MANIFEST", "review status or reviewer")
	}
	for k := range m.Defaults {
		if policyParams[k] == nil {
			return coded("UNKNOWN_PARAMETER", "defaults."+k)
		}
	}
	for k, valid := range policyParams {
		if v, ok := m.Defaults[k]; !ok || !valid(v) {
			return coded("INVALID_PARAMETER", "defaults."+k)
		}
	}
	seen := map[string]bool{}
	for _, c := range m.Cases {
		if !ids(c.CaseID) || seen[c.CaseID] || slices.Contains(CapabilityIDs, c.CaseID) || !fs.ValidPath(c.InputRef) || c.InputRef == "." || c.Parameters == nil {
			return coded("INVALID_MANIFEST", "case "+c.CaseID+": id, input_ref or parameters")
		}
		seen[c.CaseID] = true
		spec, ok := operations[c.Operation]
		if !ok || spec.layer != c.Layer {
			return coded("UNKNOWN_OPERATION", "case "+c.CaseID+": "+c.Layer+"/"+c.Operation)
		}
		for k, v := range c.Parameters {
			valid := spec.params[k]
			if valid == nil {
				return coded("UNKNOWN_PARAMETER", "case "+c.CaseID+": "+k)
			}
			if !valid(v) {
				return coded("INVALID_PARAMETER", "case "+c.CaseID+": "+k+"="+v)
			}
		}
		for _, k := range spec.required {
			if _, ok := c.Parameters[k]; !ok {
				return coded("INVALID_PARAMETER", "case "+c.CaseID+": missing "+k)
			}
		}
		e := c.Expected
		reasons, ok := outcomes[c.Operation+"/"+e.Result]
		missing := slices.ContainsFunc(forbiddenEarly, func(f string) bool { return !slices.Contains(e.ForbiddenEvents, f) })
		if !ok || !slices.Contains(reasons, e.Reason) || e.ApprovalStatus != approvalFor(c) || e.PaymentStatus != "unavailable" || e.BudgetDeducted != "0" || missing {
			return coded("CONTRADICTORY_EXPECTATION", "case "+c.CaseID)
		}
	}
	return nil
}

// ValidateRefs checks references into the trusted registry: the default
// asset, per-case asset overrides and consumers must exist there.
func (m CaseManifest) ValidateRefs(r *Registry) error {
	if _, ok := r.assets[m.Defaults["asset"]]; !ok {
		return coded("INVALID_PARAMETER", "defaults.asset is not a registry asset_id")
	}
	for _, c := range m.Cases {
		if a, ok := c.Parameters["asset"]; ok && a != "unlisted" {
			if _, ok := r.assets[a]; !ok {
				return coded("INVALID_PARAMETER", "case "+c.CaseID+": unknown asset "+a)
			}
		}
		if id, ok := c.Parameters["consumer"]; ok {
			if _, ok := r.consumers[id]; !ok {
				return coded("INVALID_PARAMETER", "case "+c.CaseID+": unknown consumer "+id)
			}
		}
	}
	return nil
}
func (m CaseManifest) RequireAccepted() error {
	if err := m.Validate(); err != nil {
		return err
	}
	if m.ReviewStatus != "accepted" {
		return fmt.Errorf("case manifest requires independent reviewer acceptance")
	}
	return nil
}
func (m CaseManifest) Hash() (string, error) {
	if err := m.Validate(); err != nil {
		return "", err
	}
	return digestValue(FixtureDomain+"/cases", m)
}

type CommandResult struct {
	Command  string `json:"command"`
	ExitCode int    `json:"exit_code"`
}
type EvidenceRef struct {
	EvidenceID   string `json:"evidence_id"`
	Path         string `json:"path"`
	Digest       string `json:"digest"`
	EvidenceMode string `json:"evidence_mode"`
	Source       string `json:"source"`
}
type ClaimResult struct {
	CaseID      string   `json:"case_id"`
	Verdict     string   `json:"verdict"`
	EvidenceIDs []string `json:"evidence_ids"`
	Reason      string   `json:"reason"`
}
type RunManifest struct {
	Version            string            `json:"version"`
	RunID              string            `json:"run_id"`
	CodeCommit         string            `json:"code_commit"`
	DirtyPatchDigest   string            `json:"dirty_patch_digest"`
	ConfigDigest       string            `json:"config_digest"`
	CaseManifestDigest string            `json:"case_manifest_digest"`
	Versions           map[string]string `json:"versions"`
	Commands           []CommandResult   `json:"commands"`
	Evidence           []EvidenceRef     `json:"evidence"`
	Results            []ClaimResult     `json:"results"`
	NotEvaluated       []string          `json:"not_evaluated"`
}

// ValidateAgainst validates the INDEX only. B1-07 must open evidence safely,
// verify bytes/digests and compute verdicts independently of the tested engine.
func (m RunManifest) ValidateAgainst(c CaseManifest) error {
	if err := c.RequireAccepted(); err != nil {
		return err
	}
	digest, err := c.Hash()
	if err != nil {
		return err
	}
	if m.Version != Version || !ids(m.RunID, m.CodeCommit) || m.CaseManifestDigest != digest || !digestPattern.MatchString(m.ConfigDigest) || !digestPattern.MatchString(m.DirtyPatchDigest) || len(m.Commands) == 0 || len(m.Versions) == 0 {
		return fmt.Errorf("invalid run metadata")
	}
	for _, cmd := range m.Commands {
		if cmd.Command == "" {
			return fmt.Errorf("empty command")
		}
	}
	for k, v := range m.Versions {
		if !ids(k) || v == "" {
			return fmt.Errorf("invalid version evidence")
		}
	}
	evidence := map[string]bool{}
	for _, e := range m.Evidence {
		if !ids(e.EvidenceID, e.Source) || !fs.ValidPath(e.Path) || e.Path == "." || !digestPattern.MatchString(e.Digest) || evidence[e.EvidenceID] {
			return fmt.Errorf("invalid evidence reference")
		}
		if !slices.Contains(evidenceModes, e.EvidenceMode) {
			return fmt.Errorf("invalid evidence provenance")
		}
		evidence[e.EvidenceID] = true
	}
	cases, seen := map[string]bool{}, map[string]bool{}
	for _, c := range c.Cases {
		cases[c.CaseID] = true
	}
	for _, result := range m.Results {
		if slices.Contains(CapabilityIDs, result.CaseID) {
			return coded("CAPABILITY_CLAIM_INVALID", result.CaseID+" cannot be claimed in M1")
		}
		if !cases[result.CaseID] || seen[result.CaseID] || result.Reason == "" {
			return fmt.Errorf("invalid claim")
		}
		if result.Verdict != "PASS" && result.Verdict != "FAIL" && result.Verdict != "INCONCLUSIVE" {
			return fmt.Errorf("invalid claim verdict")
		}
		if result.Verdict != "INCONCLUSIVE" && len(result.EvidenceIDs) == 0 {
			return fmt.Errorf("conclusive claim requires evidence")
		}
		for _, id := range result.EvidenceIDs {
			if !evidence[id] {
				return fmt.Errorf("unknown evidence ID")
			}
		}
		seen[result.CaseID] = true
	}
	for _, id := range m.NotEvaluated {
		if !cases[id] && !slices.Contains(CapabilityIDs, id) {
			return coded("UNKNOWN_CLAIM", id)
		}
		if seen[id] {
			return fmt.Errorf("duplicate result/not_evaluated claim %s", id)
		}
		seen[id] = true
	}
	for _, id := range CapabilityIDs {
		if !seen[id] {
			return coded("CAPABILITY_CLAIM_INVALID", id+" must be listed as not_evaluated")
		}
	}
	for id := range cases {
		if !seen[id] {
			return fmt.Errorf("case missing result or not_evaluated: %s", id)
		}
	}
	return nil
}
