// Package verification is the M1 read-only evidence checker (B1-07, the
// fixture scope of M1-E2; docs/m1-evidence.md section 15). It reads a run
// directory and the repository's frozen case list and fixtures, and decides
// each claim itself: it never runs the bundle's commands, never opens a
// Store, and never takes the producer's observed values, verdicts or log
// success words as results. The pure contract functions it shares are
// listed in the document; policy.Decide and internal/m1evidence are not
// used.
package verification

import (
	"fmt"
	"slices"
	"strings"
)

// Version names this checker's rules; a report carries it.
const Version = "m1-e2.1"

type Verdict string

const (
	Pass         Verdict = "PASS"
	Fail         Verdict = "FAIL"
	Inconclusive Verdict = "INCONCLUSIVE"
)

func worst(a, b Verdict) Verdict {
	switch {
	case a == Fail || b == Fail:
		return Fail
	case a == Inconclusive || b == Inconclusive:
		return Inconclusive
	}
	return Pass
}

// Claims are V1 to V7 (docs/m1-closeout-gaps.md section 3) and R, the
// integration requirements of section 3.2.
var Claims = []string{"V1", "V2", "V3", "V4", "V5", "V6", "V7", "R"}

// Finding is one problem: which claim it falls under, FAIL (the evidence
// contradicts the claim) or INCONCLUSIVE (the evidence is missing or not
// usable), why, and the evidence paths it concerns.
type Finding struct {
	Claim    string   `json:"claim"`
	Verdict  Verdict  `json:"verdict"`
	Reason   string   `json:"reason"`
	Evidence []string `json:"evidence,omitempty"`
}

type findings []Finding

func (f *findings) add(v Verdict, claim string, evidence []string, format string, a ...any) {
	r := fmt.Sprintf(format, a...)
	if len(r) > 400 { // reasons quote evidence values; keep them short
		r = r[:400] + "…"
	}
	*f = append(*f, Finding{Claim: claim, Verdict: v, Reason: r, Evidence: evidence})
}

func (f *findings) fail(claim string, evidence []string, format string, a ...any) {
	f.add(Fail, claim, evidence, format, a...)
}

func (f *findings) missing(claim string, evidence []string, format string, a ...any) {
	f.add(Inconclusive, claim, evidence, format, a...)
}

// verdicts is each claim's verdict from the findings: PASS where none.
func (f findings) verdicts(claims []string) map[string]Verdict {
	out := map[string]Verdict{}
	for _, c := range claims {
		out[c] = Pass
	}
	for _, x := range f {
		out[x.Claim] = worst(out[x.Claim], x.Verdict)
	}
	return out
}

func (f findings) verdict() Verdict {
	v := Pass
	for _, x := range f {
		v = worst(v, x.Verdict)
	}
	return v
}

// Observed is what the checker derived from the records.
type Observed struct {
	Result         string   `json:"result"`
	Reason         string   `json:"reason"`
	ApprovalStatus string   `json:"approval_status"`
	PaymentStatus  string   `json:"payment_status"`
	BudgetDeducted string   `json:"budget_deducted"`
	Purchases      int      `json:"purchases"`
	ForbiddenFound []string `json:"forbidden_events_found"`
}

type CaseReport struct {
	CaseID          string             `json:"case_id"`
	Layer           string             `json:"layer"` // host_e2e | store_integration | contracts_test
	Verdict         Verdict            `json:"verdict"`
	Claims          map[string]Verdict `json:"claims"`
	Expected        any                `json:"expected"` // the frozen case's
	Observed        []Observed         `json:"observed"` // one per record the case rests on
	Findings        []Finding          `json:"findings"`
	Evidence        []string           `json:"evidence"`
	ProducerVerdict string             `json:"producer_verdict"`
}

type RequirementReport struct {
	Requirement string   `json:"requirement"`
	Verdict     Verdict  `json:"verdict"`
	Checks      []string `json:"checks"`
}

type Report struct {
	Checker struct {
		Name    string `json:"name"`
		Version string `json:"version"`
		Build   string `json:"build"`
	} `json:"checker"`
	Mode  string `json:"mode"`
	Input struct {
		RunID           string `json:"run_id"`
		ManifestDigest  string `json:"manifest_digest"`
		EndMarkerDigest string `json:"end_marker_digest"`
		Files           int    `json:"files"`
		Bytes           int64  `json:"bytes_read"`
	} `json:"input"`
	Trusted struct {
		CaseManifestID     string            `json:"case_manifest_id"`
		CaseManifestDigest string            `json:"case_manifest_digest"`
		Files              map[string]string `json:"files"`
	} `json:"trusted"`
	Verdict      Verdict             `json:"verdict"`
	Claims       map[string]Verdict  `json:"claims"`
	Findings     []Finding           `json:"findings"` // the bundle's own (not a case's)
	Cases        []CaseReport        `json:"cases"`
	Requirements []RequirementReport `json:"requirements"`
	NotEvaluated []string            `json:"not_evaluated"`
	Differences  []string            `json:"differences_from_producer"`
	Limits       []string            `json:"limits"`
}

// finish sets the claims and the verdict from every finding.
func (r *Report) finish() {
	all := findings(slices.Clone(r.Findings))
	for _, c := range r.Cases {
		all = append(all, c.Findings...)
	}
	r.Claims = all.verdicts(Claims)
	r.Verdict = all.verdict()
}

// ExitCode: 0 every required claim passed; 1 a FAIL; 2 no FAIL but an
// INCONCLUSIVE. (3, the checker could not finish, is the command's.)
func (r *Report) ExitCode() int {
	switch r.Verdict {
	case Pass:
		return 0
	case Fail:
		return 1
	}
	return 2
}

func quote(s string) string {
	if len(s) > 120 {
		s = s[:120] + "…"
	}
	return fmt.Sprintf("%q", s)
}

func join(parts ...string) string { return strings.Join(parts, "; ") }
