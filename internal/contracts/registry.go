package contracts

import (
	"fmt"
	"slices"
	"strings"
	"unicode"
)

// RegistryConfig is supplied by the host, never populated from model arguments.
// Persist this snapshot in B1-05; B1-02 only supplies validation and reconstruction.
type RegistryConfig struct {
	Version       string      `json:"version"`
	Consumers     []Consumer  `json:"consumers"`
	Wallets       []WalletRef `json:"wallets"`
	Assets        []Asset     `json:"assets"`
	Tasks         []Task      `json:"tasks"`
	MerchantPayTo string      `json:"merchant_pay_to"`
}

// Asset is host-trusted token metadata, the single authority for it. Policy
// (B1-05) decides whether a purchase may use an asset; test defaults only
// reference an asset_id.
type Asset struct {
	AssetID       string `json:"asset_id"`
	ChainID       string `json:"chain_id"`
	AssetContract string `json:"asset_contract"`
	Decimals      uint64 `json:"decimals"`
	EIP712Name    string `json:"eip712_name"`
	EIP712Version string `json:"eip712_version"`
}
type Registry struct {
	consumers map[string]Consumer
	wallets   map[string]WalletRef
	assets    map[string]Asset
	tasks     map[string]Task
	merchant  string
}

// Session is a trusted wrapper context, not an HTTP authentication mechanism.
// B1-04 must construct it AFTER authenticating the caller, never decode it
// from the same untrusted request body as the PurchaseOrder.
type Session struct{ ConsumerID, AgentID, TaskID string }

var taskStatuses = []string{"created", "running", "awaiting_approval", "denied", "cancelled", "failed"}

func label(s string) bool { return s != "" && len(s) <= 64 && strings.IndexFunc(s, unicode.IsControl) < 0 }

func NewRegistry(c RegistryConfig) (*Registry, error) {
	if c.Version != Version || !addressPattern.MatchString(c.MerchantPayTo) || len(c.Consumers) == 0 || len(c.Wallets) == 0 || len(c.Assets) == 0 {
		return nil, fmt.Errorf("invalid registry config")
	}
	r := &Registry{consumers: map[string]Consumer{}, wallets: map[string]WalletRef{}, assets: map[string]Asset{}, tasks: map[string]Task{}, merchant: c.MerchantPayTo}
	addresses, agents, owners := map[string]bool{}, map[string]bool{}, map[string]bool{}
	for _, w := range c.Wallets {
		if !ids(w.WalletID) || !chain(w.ChainID) || !addressPattern.MatchString(w.From) || w.From == r.merchant || w.EvidenceMode != "fixture" {
			return nil, fmt.Errorf("invalid fixture wallet")
		}
		if _, ok := r.wallets[w.WalletID]; ok || addresses[w.From] {
			return nil, fmt.Errorf("duplicate wallet ID/address")
		}
		r.wallets[w.WalletID], addresses[w.From] = w, true
	}
	contracts := map[string]bool{}
	for _, a := range c.Assets {
		_, duplicate := r.assets[a.AssetID]
		key := a.ChainID + "/" + a.AssetContract
		// 77 decimals is the most a uint256 amount can carry.
		if !ids(a.AssetID) || duplicate || contracts[key] || !chain(a.ChainID) || !addressPattern.MatchString(a.AssetContract) || a.Decimals > 77 || !label(a.EIP712Name) || !label(a.EIP712Version) {
			return nil, fmt.Errorf("invalid or duplicate trusted asset")
		}
		r.assets[a.AssetID], contracts[key] = a, true
	}
	for _, consumer := range c.Consumers {
		_, duplicate := r.consumers[consumer.ConsumerID]
		_, exists := r.wallets[consumer.WalletID]
		ref := consumer.AgentRegistryRef == "" || agentRefPattern.MatchString(consumer.AgentRegistryRef)
		if !ids(consumer.ConsumerID, consumer.AgentID, consumer.WalletID) || duplicate || !exists || agents[consumer.AgentID] || owners[consumer.WalletID] || !ref {
			return nil, fmt.Errorf("duplicate or invalid consumer binding")
		}
		r.consumers[consumer.ConsumerID] = consumer
		agents[consumer.AgentID], owners[consumer.WalletID] = true, true
	}
	if len(owners) != len(r.wallets) {
		return nil, fmt.Errorf("unowned wallet")
	}
	for _, task := range c.Tasks {
		owner, ok := r.consumers[task.ConsumerID]
		_, duplicate := r.tasks[task.TaskID]
		if !ok || duplicate || !ids(task.TaskID) || task.Version != Version || task.AgentID != owner.AgentID || task.WalletID != owner.WalletID || !timestamp(task.CreatedAt) {
			return nil, fmt.Errorf("invalid task binding")
		}
		if !slices.Contains(taskStatuses, task.Status) {
			return nil, fmt.Errorf("unsupported M1 task status")
		}
		r.tasks[task.TaskID] = task
	}
	return r, nil
}

// Binding returns copies. A caller cannot mutate the registry through a result.
func (r *Registry) Binding(taskID string) (Task, WalletRef, error) {
	t, ok := r.tasks[taskID]
	if !ok {
		return Task{}, WalletRef{}, fmt.Errorf("unknown task")
	}
	return t, r.wallets[t.WalletID], nil
}

// TrustedAsset returns the host-configured asset for a chain and contract.
// A miss is Policy's Deny (ASSET_NOT_ALLOWED) in B1-05, not a shape error.
func (r *Registry) TrustedAsset(chainID, contract string) (Asset, bool) {
	for _, a := range r.assets {
		if a.ChainID == chainID && a.AssetContract == contract {
			return a, true
		}
	}
	return Asset{}, false
}

// ValidatePurchase checks shape and ownership only. It deliberately ignores the
// task status so that historical events of a finished task stay verifiable;
// accepting a NEW purchase also needs CheckTaskActive on the runtime status.
func (r *Registry) ValidatePurchase(s Session, p PurchaseOrder) error {
	if err := p.Validate(); err != nil {
		return err
	}
	t, w, err := r.Binding(s.TaskID)
	if err != nil {
		return err
	}
	i := p.Intent
	if s.ConsumerID != t.ConsumerID || s.AgentID != t.AgentID || i.TaskID != t.TaskID || i.ConsumerID != t.ConsumerID || i.AgentID != t.AgentID || i.WalletID != w.WalletID || i.From != w.From || i.ChainID != w.ChainID {
		return coded("OWNERSHIP_MISMATCH", "session, task, wallet or payer do not belong together")
	}
	return nil
}

// CheckTaskActive is the rule for accepting a NEW purchase. B1-05 applies it to
// the persisted runtime status; a registry snapshot only holds the initial one.
func CheckTaskActive(status string) error {
	switch status {
	case "created", "running", "awaiting_approval":
		return nil
	case "denied", "cancelled", "failed":
		return coded("TASK_NOT_ACTIVE", "task is "+status+" and cannot accept a new purchase")
	}
	return fmt.Errorf("unknown task status %q", status)
}

// Merchant and asset allowlisting belong to B1-05: an unlisted but distinct
// payee or asset may be a valid purchase shape and must reach the Deny there.

func (p PurchaseOrder) RetryKey() (string, error) {
	if err := p.Validate(); err != nil {
		return "", err
	}
	return digestValue(FixtureDomain+"/retry", struct {
		Consumer string `json:"consumer_id"`
		Task     string `json:"task_id"`
		Wallet   string `json:"wallet_id"`
		Request  string `json:"request_id"`
	}{p.Intent.ConsumerID, p.Intent.TaskID, p.Intent.WalletID, p.RequestID})
}

// CompareRetry is pure; callers MUST validate ownership first and atomically
// persist the accepted key+payload in B1-05. It is not a concurrency-safe store.
//
// Same retry scope: identical canonical order is a replay, anything else a
// conflict. Different scope is new unless it reuses the task's
// purchase_order_id or the payer's EIP-3009 nonce, which are unique within
// (task_id) and (chain_id, asset_contract, from) respectively.
func CompareRetry(existing, incoming PurchaseOrder) (string, error) {
	a, err := existing.RetryKey()
	if err != nil {
		return "", err
	}
	b, err := incoming.RetryKey()
	if err != nil {
		return "", err
	}
	e, i := existing.Intent, incoming.Intent
	if a != b {
		if e.TaskID == i.TaskID && e.PurchaseOrderID == i.PurchaseOrderID {
			return "", coded("IDEMPOTENCY_CONFLICT", "purchase_order_id reused under another request_id")
		}
		if e.ChainID == i.ChainID && e.AssetContract == i.AssetContract && e.From == i.From && e.EIP3009Nonce == i.EIP3009Nonce {
			return "", coded("IDEMPOTENCY_CONFLICT", "eip3009_nonce reused by the same payer and asset")
		}
		return "new", nil
	}
	x, err := digestValue(FixtureDomain+"/purchase", existing)
	if err != nil {
		return "", err
	}
	y, err := digestValue(FixtureDomain+"/purchase", incoming)
	if err != nil {
		return "", err
	}
	if x != y {
		return "", coded("IDEMPOTENCY_CONFLICT", "same request_id with a different purchase")
	}
	return "replay", nil
}
