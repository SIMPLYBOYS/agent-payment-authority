// Package ae1 is the AE1 evaluation of the buyer agent (docs/commercial-ae1.md):
// a frozen corpus of cases, the score of one run against its case, and the
// report over all runs. It decides nothing about a purchase: the Store's
// record of each run is what it scores, never what the model said.
package ae1

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"regexp"
	"slices"

	"github.com/SIMPLYBOYS/agent-payment-authority/internal/merchant"
)

// Kinds of case. Benign and injection cases run with a model (live) or a
// scripted stand-in (fake); crafted and fault cases are fake CLI scripts only.
const (
	Benign    = "benign"
	Injection = "injection"
	Crafted   = "crafted"
	Fault     = "fault"
)

// Req is one purchase the Store recorded: the product and the Policy verdict.
type Req struct {
	Product string `json:"product"`
	Verdict string `json:"verdict"` // Allow, Ask or Deny
}

// Outcome is an acceptable record of a run: its purchases, in order.
type Outcome struct {
	Requests []Req `json:"requests"`
}

// Case is one frozen evaluation case.
type Case struct {
	ID       string              `json:"id"`
	Kind     string              `json:"kind"`
	Category string              `json:"category"`
	Task     string              `json:"task"`    // the person's task (buyer.TaskPrompt)
	Catalog  string              `json:"catalog"` // a catalog of the corpus
	Patch    []merchant.EvalItem `json:"patch,omitempty"`
	// Accept lists the acceptable outcomes; the first is the canonical one
	// (its first request's verdict is the expected verdict).
	Accept []Outcome `json:"accept"`
	// Injected lists the products an injection case's shop text pushes:
	// requesting any of them is being fooled.
	Injected []string `json:"injected,omitempty"`
	// Script is a crafted or fault case's fake CLI steps; Infra its expected
	// CLI outcome ("" is ok).
	Script []json.RawMessage `json:"script,omitempty"`
	Infra  string            `json:"infra,omitempty"`
}

// Corpus is the frozen case set.
type Corpus struct {
	Version  string                         `json:"version"`
	Catalogs map[string][]merchant.EvalItem `json:"catalogs"`
	Cases    []Case                         `json:"cases"`
	SHA256   string                         `json:"-"` // of the file as read
}

var idPattern = regexp.MustCompile(`^[a-z0-9-]{1,64}$`)

// Load reads and checks a corpus file.
func Load(path string) (*Corpus, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.DisallowUnknownFields()
	var c Corpus
	if err := dec.Decode(&c); err != nil {
		return nil, fmt.Errorf("corpus %s: %w", path, err)
	}
	sum := sha256.Sum256(raw)
	c.SHA256 = hex.EncodeToString(sum[:])
	return &c, c.check()
}

func (c *Corpus) check() error {
	if c.Version == "" || len(c.Cases) == 0 {
		return fmt.Errorf("corpus: version and cases")
	}
	seen := map[string]bool{}
	for _, k := range c.Cases {
		if !idPattern.MatchString(k.ID) || seen[k.ID] {
			return fmt.Errorf("case %q: a distinct [a-z0-9-] ID", k.ID)
		}
		seen[k.ID] = true
		items, err := c.Items(k)
		if err != nil {
			return err
		}
		offered := map[string]bool{}
		for _, it := range items {
			offered[it.ID] = true
		}
		if len(k.Accept) == 0 {
			return fmt.Errorf("case %s: no acceptable outcome", k.ID)
		}
		for _, a := range k.Accept {
			for _, r := range a.Requests {
				if !offered[r.Product] || !slices.Contains([]string{"Allow", "Ask", "Deny"}, r.Verdict) {
					return fmt.Errorf("case %s: accepted request %+v", k.ID, r)
				}
			}
		}
		for _, p := range k.Injected {
			if !offered[p] {
				return fmt.Errorf("case %s: injected product %q is not in its catalog", k.ID, p)
			}
		}
		switch k.Kind {
		case Benign, Injection:
			if k.Task == "" || len(k.Script) != 0 || k.Infra != "" {
				return fmt.Errorf("case %s: a %s case has a task and no script", k.ID, k.Kind)
			}
			if (k.Kind == Injection) != (len(k.Injected) > 0 || k.Category == "cross-consumer") {
				return fmt.Errorf("case %s: injected products belong to injection cases", k.ID)
			}
		case Crafted, Fault:
			if len(k.Script) == 0 || len(k.Injected) != 0 {
				return fmt.Errorf("case %s: a %s case is a fake CLI script", k.ID, k.Kind)
			}
		default:
			return fmt.Errorf("case %s: kind %q", k.ID, k.Kind)
		}
	}
	return nil
}

// Items is a case's catalog: the named one with the case's patch applied
// (an item of the same ID is replaced, a new one appended).
func (c *Corpus) Items(k Case) ([]merchant.EvalItem, error) {
	base, ok := c.Catalogs[k.Catalog]
	if !ok {
		return nil, fmt.Errorf("case %s: no catalog %q", k.ID, k.Catalog)
	}
	out := slices.Clone(base)
	for _, p := range k.Patch {
		if i := slices.IndexFunc(out, func(it merchant.EvalItem) bool { return it.ID == p.ID }); i >= 0 {
			out[i] = p
		} else {
			out = append(out, p)
		}
	}
	return out, nil
}
