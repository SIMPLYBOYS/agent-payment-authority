package contracts

import "fmt"

// RegistryConfig is supplied by the host, never populated from model arguments.
// Persist this snapshot in B1-05; B1-02 only supplies validation and reconstruction.
type RegistryConfig struct {
	Version       string      `json:"version"`
	Consumers     []Consumer  `json:"consumers"`
	Wallets       []WalletRef `json:"wallets"`
	Tasks         []Task      `json:"tasks"`
	MerchantPayTo string      `json:"merchant_pay_to"`
}
type Registry struct {
	consumers map[string]Consumer
	wallets   map[string]WalletRef
	tasks     map[string]Task
	merchant  string
}

// Session is a trusted wrapper context, not an HTTP authentication mechanism.
// B1-04 must construct it AFTER authenticating the caller, never decode it
// from the same untrusted request body as the PurchaseOrder.
type Session struct{ ConsumerID, AgentID, TaskID string }

func NewRegistry(c RegistryConfig) (*Registry, error) {
	if c.Version != Version || !addressPattern.MatchString(c.MerchantPayTo) || len(c.Consumers) == 0 || len(c.Wallets) == 0 {
		return nil, fmt.Errorf("invalid registry config")
	}
	r := &Registry{consumers: map[string]Consumer{}, wallets: map[string]WalletRef{}, tasks: map[string]Task{}, merchant: c.MerchantPayTo}
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
	for _, consumer := range c.Consumers {
		_, duplicate := r.consumers[consumer.ConsumerID]
		_, exists := r.wallets[consumer.WalletID]
		if !ids(consumer.ConsumerID, consumer.AgentID, consumer.WalletID) || duplicate || !exists || agents[consumer.AgentID] || owners[consumer.WalletID] {
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
		switch task.Status {
		case "created", "running", "awaiting_approval", "denied", "cancelled", "failed":
		default:
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
		return fmt.Errorf("OWNERSHIP_MISMATCH")
	}
	return nil
}

// Merchant allowlisting belongs to B1-05: an unlisted but distinct payee may be
// a valid purchase shape and must reach the deterministic Deny test there.

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
func CompareRetry(existing, incoming PurchaseOrder) (string, error) {
	a, err := existing.RetryKey()
	if err != nil {
		return "", err
	}
	b, err := incoming.RetryKey()
	if err != nil {
		return "", err
	}
	if a != b {
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
		return "", fmt.Errorf("IDEMPOTENCY_CONFLICT")
	}
	return "replay", nil
}
