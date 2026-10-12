package signer

// The Signer's trusted configuration (docs/commercial-p1b-design.md 3.5):
// owned by the Signer's user, changed by the administrator only. Requests
// never name a file, a key or a delegation's terms.

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"regexp"
	"slices"

	"github.com/SIMPLYBOYS/agent-payment-authority/internal/contracts"
)

var (
	idPattern      = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._:-]{0,127}$`)
	addressPattern = regexp.MustCompile(`^0x[0-9a-f]{40}$`)
)

type Wallet struct {
	WalletID   string `json:"wallet_id"`
	ConsumerID string `json:"consumer_id"`
	ChainID    string `json:"chain_id"`
	Address    string `json:"address"`
}

type Asset struct {
	ChainID       string `json:"chain_id"`
	AssetContract string `json:"asset_contract"`
	EIP712Name    string `json:"eip712_name"`
	EIP712Version string `json:"eip712_version"`
}

// Delegation is a standing delegation: the administrator's terms for
// automatic payments of one consumer's wallet. Its spend is counted by the
// Signer across every attempt under its ID, whatever its version.
type Delegation struct {
	DelegationID  string           `json:"delegation_id"`
	Version       uint64           `json:"version"`
	ConsumerID    string           `json:"consumer_id"`
	WalletID      string           `json:"wallet_id"`
	ChainID       string           `json:"chain_id"`
	AssetContract string           `json:"asset_contract"`
	Merchants     []string         `json:"merchants"`
	PerTxMax      contracts.Amount `json:"per_tx_max"`
	TotalCap      contracts.Amount `json:"total_cap"`
	ValidFrom     uint64           `json:"valid_from"`
	ValidUntil    uint64           `json:"valid_until"`
}

type Config struct {
	Version          int          `json:"version"` // 1
	Socket           string       `json:"socket"`  // absolute path of the Unix socket
	Callers          []uint32     `json:"callers"` // uids allowed to call
	MaxWindowSeconds uint64       `json:"max_window_seconds"`
	Wallets          []Wallet     `json:"wallets"`
	Assets           []Asset      `json:"assets"`
	Delegations      []Delegation `json:"delegations"`

	hash string // sha256 of the canonical configuration, in each snapshot
}

func units(a contracts.Amount) (uint64, bool) {
	n, err := a.Units()
	return n, err == nil && n > 0 && n <= math.MaxInt64 // amounts are summed as SQLite integers
}

// ParseConfig decodes a configuration strictly and checks it.
func ParseConfig(raw []byte) (*Config, error) {
	var c Config
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&c); err != nil || dec.More() {
		return nil, errors.New("config: not a signer configuration")
	}
	if c.Version != 1 || c.MaxWindowSeconds == 0 || c.MaxWindowSeconds > 3600 {
		return nil, errors.New("config: version 1 and a max window of 1 to 3600 seconds")
	}
	wallets, owners := map[string]Wallet{}, map[string]string{}
	for _, w := range c.Wallets {
		if !idPattern.MatchString(w.WalletID) || !idPattern.MatchString(w.ConsumerID) || !addressPattern.MatchString(w.Address) {
			return nil, fmt.Errorf("config: wallet %q", w.WalletID)
		}
		if _, dup := wallets[w.WalletID]; dup || owners[w.Address] != "" {
			return nil, fmt.Errorf("config: wallet %q or its address is listed twice", w.WalletID)
		}
		if _, ok := units(contracts.Amount(w.ChainID)); !ok {
			return nil, fmt.Errorf("config: wallet %q chain", w.WalletID)
		}
		wallets[w.WalletID], owners[w.Address] = w, w.ConsumerID
	}
	for _, a := range c.Assets {
		if _, ok := units(contracts.Amount(a.ChainID)); !ok || !addressPattern.MatchString(a.AssetContract) || a.EIP712Name == "" || a.EIP712Version == "" {
			return nil, fmt.Errorf("config: asset %q", a.AssetContract)
		}
	}
	seen := map[string]bool{}
	for _, d := range c.Delegations {
		w, ok := wallets[d.WalletID]
		per, ok1 := units(d.PerTxMax)
		total, ok2 := units(d.TotalCap)
		switch {
		case !idPattern.MatchString(d.DelegationID) || seen[d.DelegationID] || d.Version == 0:
			return nil, fmt.Errorf("config: delegation %q", d.DelegationID)
		case !ok || w.ConsumerID != d.ConsumerID || w.ChainID != d.ChainID:
			return nil, fmt.Errorf("config: delegation %q is not its consumer's wallet on its chain", d.DelegationID)
		case c.asset(d.ChainID, d.AssetContract) == nil:
			return nil, fmt.Errorf("config: delegation %q asset is not registered", d.DelegationID)
		case len(d.Merchants) == 0 || slices.ContainsFunc(d.Merchants, func(m string) bool { return !addressPattern.MatchString(m) }):
			return nil, fmt.Errorf("config: delegation %q merchants", d.DelegationID)
		case !ok1 || !ok2 || per > total || d.ValidUntil <= d.ValidFrom:
			return nil, fmt.Errorf("config: delegation %q limits or period", d.DelegationID)
		}
		seen[d.DelegationID] = true
	}
	canon, err := contracts.Canonical(raw)
	if err != nil {
		return nil, err
	}
	sum := sha256.Sum256(canon)
	c.hash = "sha256:" + hex.EncodeToString(sum[:])
	return &c, nil
}

func (c *Config) wallet(id string) *Wallet {
	for i := range c.Wallets {
		if c.Wallets[i].WalletID == id {
			return &c.Wallets[i]
		}
	}
	return nil
}

func (c *Config) asset(chain, contract string) *Asset {
	for i := range c.Assets {
		if c.Assets[i].ChainID == chain && c.Assets[i].AssetContract == contract {
			return &c.Assets[i]
		}
	}
	return nil
}

func (c *Config) delegation(id string) *Delegation {
	for i := range c.Delegations {
		if c.Delegations[i].DelegationID == id {
			return &c.Delegations[i]
		}
	}
	return nil
}

func (c *Config) allows(uid uint32) bool { return slices.Contains(c.Callers, uid) }
