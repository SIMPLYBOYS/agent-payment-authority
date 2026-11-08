package signer

// The wallet's consent to be its ERC-8004 agent's wallet (docs/commercial-8004.md §8): the registry's
// setAgentWallet takes the new wallet's EIP-712 signature over AgentWalletSet. The Signer signs it only for
// the agent the administrator configured for that wallet, with the deadline the registry accepts, and
// keeps nothing: the registry itself refuses a signature past its deadline, and setting the same wallet
// again changes nothing.

import "encoding/hex"

// MaxAgentWalletDelay is the registry's MAX_DEADLINE_DELAY: a deadline at most 5 minutes ahead.
const MaxAgentWalletDelay = 300

// AgentWalletRequest names the wallet and the deadline (Unix seconds) of its consent.
type AgentWalletRequest struct {
	WalletID string `json:"wallet_id"`
	Deadline uint64 `json:"deadline"`
}

// AgentWalletReply is the signed consent: what was signed and the 65-byte signature r ‖ s ‖ v, hex.
type AgentWalletReply struct {
	ChainID     string `json:"chain_id,omitempty"`
	Registry    string `json:"registry,omitempty"`
	AgentID     uint64 `json:"agent_id,omitempty"`
	Owner       string `json:"owner,omitempty"`
	Wallet      string `json:"wallet,omitempty"`
	Deadline    uint64 `json:"deadline,omitempty"`
	TypedDigest string `json:"typed_digest,omitempty"`
	Signature   string `json:"signature,omitempty"`
	Error       string `json:"error,omitempty"`
}

// SignAgentWallet signs the consent of r's wallet to its configured agent: AGENT_NOT_CONFIGURED without
// one, DEADLINE_INVALID unless now < deadline <= now + 300.
func (c *Core) SignAgentWallet(r AgentWalletRequest) (AgentWalletReply, error) {
	var a *Agent
	for i := range c.cfg.Agents {
		if c.cfg.Agents[i].WalletID == r.WalletID {
			a = &c.cfg.Agents[i]
		}
	}
	w := c.cfg.wallet(r.WalletID)
	if a == nil || w == nil {
		return AgentWalletReply{}, Refusal("AGENT_NOT_CONFIGURED")
	}
	if now := c.clock(); r.Deadline <= now || r.Deadline > now+MaxAgentWalletDelay {
		return AgentWalletReply{}, Refusal("DEADLINE_INVALID")
	}
	digest, err := AgentWalletDigest(*a, w.Address, r.Deadline)
	if err != nil {
		return AgentWalletReply{}, err
	}
	return AgentWalletReply{ChainID: a.ChainID, Registry: a.Registry, AgentID: a.AgentID, Owner: a.Owner, Wallet: w.Address, Deadline: r.Deadline,
		TypedDigest: "0x" + hex.EncodeToString(digest), Signature: "0x" + hex.EncodeToString(signDigest(c.keys[w.Address], digest))}, nil
}
