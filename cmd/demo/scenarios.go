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

	digest     string   // sha256 of this entry and its scenario file's bytes
	candidates []string // the consumer's demo tasks (show-*), in registry order
}

type catalog struct {
	Description string     `json:"description"`
	Scenarios   []scenario `json:"scenarios"`
}

var scenarioID = regexp.MustCompile(`^[a-z0-9-]{1,32}$`)

// loadScenarios reads the catalog strictly. Each consumer must be in the
// trusted registry and have demo tasks to allocate; each scenario file must
// exist. The digest records exactly what a run was accepted with.
func loadScenarios(path string, reg *contracts.Registry, regCfg contracts.RegistryConfig) (map[string]*scenario, error) {
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
		if len(sc.candidates) == 0 {
			return nil, fmt.Errorf("scenarios %s: %s: consumer %q has no show-* task in the registry", path, sc.ID, sc.ConsumerID)
		}
		if sc.Scenario, err = filepath.Abs(sc.Scenario); err != nil {
			return nil, err
		}
		body, err := os.ReadFile(sc.Scenario)
		if err != nil {
			return nil, fmt.Errorf("scenarios %s: %s: scenario file: %w", path, sc.ID, err)
		}
		entry, _ := json.Marshal(struct{ ID, ConsumerID, BudgetInitial string }{sc.ID, sc.ConsumerID, string(sc.BudgetInitial)})
		h := sha256.New()
		h.Write(entry)
		h.Write([]byte{0})
		h.Write(body)
		sc.digest = "sha256:" + hex.EncodeToString(h.Sum(nil))
		out[sc.ID] = &sc
	}
	return out, nil
}
