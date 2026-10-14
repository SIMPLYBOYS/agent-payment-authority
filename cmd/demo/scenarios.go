package main

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/SIMPLYBOYS/agent-payment-authority/internal/contracts"
	"github.com/SIMPLYBOYS/agent-payment-authority/internal/paycontract"
	"github.com/SIMPLYBOYS/agent-payment-authority/internal/policy"
)

// scenario is one entry of the host's trusted scenario catalog
// (testdata/demo/scenarios.json; docs/demo-operation-design.md section 4).
// A start request names an id only; consumer, scenario file and budget
// come from here.
type scenario struct {
	ID            string           `json:"id"`
	ConsumerID    string           `json:"consumer_id"`
	Scenario      string           `json:"scenario"`
	BudgetInitial contracts.Amount `json:"budget_initial,omitempty"`
	Label         string           `json:"label"`
	// Contract is the task's contract version ("" = apa.m1.v2). An
	// apa.pay.v1 entry has the Delegation the consumer is asked to confirm
	// (docs/commercial-transaction-flow.md section 10 row 1); fake only.
	Contract   string           `json:"contract,omitempty"`
	Delegation *delegationTerms `json:"delegation,omitempty"`

	digest     string   // sha256 of this entry and its scenario file's bytes
	candidates []string // the consumer's demo tasks (show-*), in registry order
}

// delegationTerms are a delegation's host-set terms; the merchants and the
// asset come from the host's profile. allow_max and task_budget only narrow
// the profile's.
type delegationTerms struct {
	Purpose                 string           `json:"purpose"`
	TaskBudget              contracts.Amount `json:"task_budget"`
	AllowMax                contracts.Amount `json:"allow_max"`
	ApprovalDeadlineSeconds uint64           `json:"approval_deadline_seconds"`
	ValidSeconds            uint64           `json:"valid_seconds"` // how long a confirmed delegation may start a run
}

type catalog struct {
	Description string     `json:"description"`
	Scenarios   []scenario `json:"scenarios"`
}

var scenarioID = regexp.MustCompile(`^[a-z0-9-]{1,32}$`)

// loadScenarios reads the catalog strictly. Each consumer must be in the
// trusted registry and have demo tasks to allocate; each scenario file must
// exist. The digest records exactly what a run was accepted with.
//
// In live mode (docs/cli-poc.md 55) an entry names no scenario file: the
// real CLI runs, not a script; a live catalog with one, or a fake catalog
// without one, is refused.
func loadScenarios(path string, reg *contracts.Registry, regCfg contracts.RegistryConfig, live bool) (map[string]*scenario, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.DisallowUnknownFields()
	var c catalog
	if err := dec.Decode(&c); err != nil || dec.More() || len(c.Scenarios) == 0 {
		return nil, fmt.Errorf("scenarios %s: not a scenario catalog", path)
	}
	out := map[string]*scenario{}
	for i := range c.Scenarios {
		sc := c.Scenarios[i]
		if !scenarioID.MatchString(sc.ID) || out[sc.ID] != nil {
			return nil, fmt.Errorf("scenarios %s: entry %d: id %q is not a distinct [a-z0-9-] name", path, i, sc.ID)
		}
		if sc.BudgetInitial != "" {
			if _, err := sc.BudgetInitial.Units(); err != nil {
				return nil, fmt.Errorf("scenarios %s: %s: budget_initial is not an amount", path, sc.ID)
			}
		}
		for _, t := range regCfg.Tasks {
			if t.ConsumerID == sc.ConsumerID && strings.HasPrefix(t.TaskID, "show-") {
				if _, _, err := reg.Binding(t.TaskID); err == nil {
					sc.candidates = append(sc.candidates, t.TaskID)
				}
			}
		}
		switch sc.Contract {
		case "", contracts.Version:
			if sc.Delegation != nil {
				return nil, fmt.Errorf("scenarios %s: %s: only a pay-family entry has a delegation", path, sc.ID)
			}
		case paycontract.Version, paycontract.V2:
			d := sc.Delegation
			if live || d == nil || sc.BudgetInitial != "" || d.Purpose == "" || len(d.Purpose) > 200 || d.ApprovalDeadlineSeconds == 0 || d.ApprovalDeadlineSeconds > 600 ||
				d.ValidSeconds == 0 || d.ValidSeconds > 86400 || !amount(d.TaskBudget) || !amount(d.AllowMax) {
				return nil, fmt.Errorf("scenarios %s: %s: a pay-family entry is fake only and needs a whole delegation (no budget_initial)", path, sc.ID)
			}
		default:
			return nil, fmt.Errorf("scenarios %s: %s: unknown contract %q", path, sc.ID, sc.Contract)
		}
		if len(sc.candidates) == 0 {
			return nil, fmt.Errorf("scenarios %s: %s: consumer %q has no show-* task in the registry", path, sc.ID, sc.ConsumerID)
		}
		var body []byte
		switch {
		case live && sc.Scenario != "":
			return nil, fmt.Errorf("scenarios %s: %s: a live catalog names no scenario file (the real CLI runs)", path, sc.ID)
		case !live && sc.Scenario == "":
			return nil, fmt.Errorf("scenarios %s: %s: no scenario file", path, sc.ID)
		case !live:
			if sc.Scenario, err = filepath.Abs(sc.Scenario); err != nil {
				return nil, err
			}
			if body, err = os.ReadFile(sc.Scenario); err != nil {
				return nil, fmt.Errorf("scenarios %s: %s: scenario file: %w", path, sc.ID, err)
			}
		}
		// An M1 entry's digest is what it always was; the contract and the
		// delegation join it only when set.
		entry, _ := json.Marshal(struct {
			ID, ConsumerID, BudgetInitial string
			Contract                      string           `json:",omitempty"`
			Delegation                    *delegationTerms `json:",omitempty"`
		}{sc.ID, sc.ConsumerID, string(sc.BudgetInitial), sc.Contract, sc.Delegation})
		h := sha256.New()
		h.Write(entry)
		h.Write([]byte{0})
		h.Write(body)
		sc.digest = "sha256:" + hex.EncodeToString(h.Sum(nil))
		out[sc.ID] = &sc
	}
	return out, nil
}

func amount(a contracts.Amount) bool { n, err := a.Units(); return err == nil && n > 0 }

// version is the entry's contract version.
func (sc *scenario) version() string {
	if sc.Contract == "" {
		return contracts.Version
	}
	return sc.Contract
}

// checkDelegations refuses an apa.pay.v1 entry whose delegation is wider
// than the host's profile: the Store would refuse it at the task's start.
func checkDelegations(scs map[string]*scenario, p policy.Profile) error {
	limit, _ := p.TaskCap.Units()
	allow, _ := p.AllowMax.Units()
	for _, sc := range scs {
		if d := sc.Delegation; d != nil {
			b, _ := d.TaskBudget.Units()
			a, _ := d.AllowMax.Units()
			if b > limit || a > allow || a > b {
				return fmt.Errorf("scenario %s: the delegation's task_budget and allow_max must be within the profile's (and allow_max within the budget)", sc.ID)
			}
		}
	}
	return nil
}
