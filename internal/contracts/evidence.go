package contracts

import (
	"fmt"
	"io/fs"
)

type ExpectedOutcome struct {
	Result          string   `json:"result"`
	Reason          string   `json:"reason"`
	ApprovalStatus  string   `json:"approval_status"`
	PaymentStatus   string   `json:"payment_status"`
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

func (m CaseManifest) Validate() error {
	if m.Version != Version || !ids(m.ManifestID) || len(m.Cases) == 0 {
		return fmt.Errorf("invalid case manifest")
	}
	if m.ReviewStatus != "pending" && m.ReviewStatus != "accepted" {
		return fmt.Errorf("invalid review status")
	}
	if m.ReviewStatus == "accepted" && !ids(m.Reviewer) {
		return fmt.Errorf("accepted manifest needs reviewer")
	}
	for _, key := range []string{"remaining", "allow_max", "merchant", "credential", "required_config", "mode", "clock"} {
		if m.Defaults[key] == "" {
			return fmt.Errorf("missing case default %s", key)
		}
	}
	seen := map[string]bool{}
	for _, c := range m.Cases {
		if !ids(c.CaseID, c.Operation, c.Expected.Result, c.Expected.Reason) || seen[c.CaseID] || !fs.ValidPath(c.InputRef) || c.InputRef == "." || c.Parameters == nil {
			return fmt.Errorf("invalid case")
		}
		if c.Layer != "contracts" && c.Layer != "integration" {
			return fmt.Errorf("invalid case layer")
		}
		if c.Expected.PaymentStatus != "unavailable" || (c.Expected.ApprovalStatus != "pending" && c.Expected.ApprovalStatus != "not_requested") {
			return fmt.Errorf("invalid M1 outcome")
		}
		for _, forbidden := range []string{"APPROVED", "GRANT_ISSUED", "SETTLED", "RESOURCE_DELIVERED"} {
			found := false
			for _, v := range c.Expected.ForbiddenEvents {
				found = found || v == forbidden
			}
			if !found {
				return fmt.Errorf("missing forbidden event %s", forbidden)
			}
		}
		seen[c.CaseID] = true
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
		if e.EvidenceMode != "fixture" && e.EvidenceMode != "live" && e.EvidenceMode != "replay" {
			return fmt.Errorf("invalid evidence provenance")
		}
		evidence[e.EvidenceID] = true
	}
	cases, seen := map[string]bool{}, map[string]bool{}
	for _, c := range c.Cases {
		cases[c.CaseID] = true
	}
	for _, result := range m.Results {
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
		if !ids(id) || seen[id] {
			return fmt.Errorf("duplicate result/not_evaluated claim")
		}
		seen[id] = true
	}
	for id := range cases {
		if !seen[id] {
			return fmt.Errorf("case missing result or not_evaluated: %s", id)
		}
	}
	return nil
}
