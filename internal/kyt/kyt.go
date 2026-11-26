// Package kyt is the demo's minimal payee screening (docs/commercial-kyt.md):
// a list of flagged addresses read from a file, checked before a payment is
// decided (the Host's Policy) and, from the Signer's own configuration,
// before it is signed. It is a demo list kept in this repository, not a
// transaction-monitoring service: no provider, no risk score, no on-chain
// tracing of where funds came from or go.
package kyt

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"regexp"
	"strings"
)

// Version names the list's format.
const Version = "apa.kyt-demo-list.v1"

// List is a demo risk list: who keeps it, when, and the flagged addresses.
type List struct {
	Version   string  `json:"version"`
	Source    string  `json:"source"` // who keeps it and what it is not, said in the file itself
	UpdatedAt string  `json:"updated_at"`
	Addresses []Entry `json:"addresses"`
	sha       string
}

// Entry is one flagged address and why (free text, the list keeper's).
type Entry struct {
	Address string `json:"address"`
	Reason  string `json:"reason"`
}

var address = regexp.MustCompile(`^0x[0-9a-f]{40}$`)

// Load reads a list strictly: its version, a source, and lower-case
// addresses, each once.
func Load(path string) (*List, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	d := json.NewDecoder(strings.NewReader(string(raw)))
	d.DisallowUnknownFields()
	var l List
	if err := d.Decode(&l); err != nil {
		return nil, fmt.Errorf("%s: %w", path, err)
	}
	if l.Version != Version || l.Source == "" {
		return nil, fmt.Errorf("%s: not an %s list with its source", path, Version)
	}
	seen := map[string]bool{}
	for _, e := range l.Addresses {
		if !address.MatchString(e.Address) || seen[e.Address] {
			return nil, fmt.Errorf("%s: address %q is not a lower-case 0x address, or repeated", path, e.Address)
		}
		seen[e.Address] = true
	}
	sum := sha256.Sum256(raw)
	l.sha = hex.EncodeToString(sum[:])
	return &l, nil
}

// Flagged: the address (any case) is on the list. A nil list flags nothing.
func (l *List) Flagged(addr string) bool {
	if l == nil {
		return false
	}
	a := strings.ToLower(addr)
	for _, e := range l.Addresses {
		if e.Address == a {
			return true
		}
	}
	return false
}

// SHA256 is the list file's, for the Host's log line.
func (l *List) SHA256() string {
	if l == nil {
		return ""
	}
	return l.sha
}
