package verification

import (
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"math/big"
	"regexp"
	"strings"

	"github.com/SIMPLYBOYS/agent-payment-authority/internal/contracts"
	"github.com/SIMPLYBOYS/agent-payment-authority/internal/eth"
)

// E1aAgent is one consumer's Agent identity (ERC-8004, docs/commercial-8004.md
// I3): the pack registry's ref and what the chain answers for it ("" not
// read), listed, not a claim. The registration file's payment wallet is a
// declaration; the agentWallet is the registry's record of that wallet's
// own signature (setAgentWallet, docs/commercial-8004.md §8), or none.
type E1aAgent struct {
	ConsumerID     string `json:"consumer_id"`
	Ref            string `json:"agent_registry_ref"`
	Owner          string `json:"owner"`
	AgentWallet    string `json:"agent_wallet"`
	URISHA256      string `json:"agent_uri_sha256"`
	DeclaredWallet string `json:"declared_payment_wallet"`
	// DeclaredIsConsumers: the declared wallet is the registry's wallet for this consumer.
	DeclaredIsConsumers bool `json:"declared_is_consumer_wallet"`
	// AgentWalletIsConsumers: the chain's agentWallet is the registry's wallet for this consumer, which the
	// registry set only on that wallet's signature: the wallet's proof, not the platform's word.
	AgentWalletIsConsumers bool `json:"agent_wallet_is_consumer_wallet"`
}

var agentRef = regexp.MustCompile(`^eip155:([0-9]+)/erc721:(0x[0-9a-fA-F]{40})/([0-9]+)$`)

const dataJSON = "data:application/json;base64,"

// e1aAgents lists each registry consumer that names an Agent identity; with
// chain, what that chain (the ref's own) answers for it.
func e1aAgents(chain Chain, reg contracts.RegistryConfig) []E1aAgent {
	out := []E1aAgent{}
	ctx, cancel := context.WithTimeout(context.Background(), chainTimeout)
	defer cancel()
	var id *big.Int
	if chain != nil {
		id, _ = chain.ChainID(ctx)
	}
	wallets := map[string]string{}
	for _, w := range reg.Wallets {
		wallets[w.WalletID] = w.From
	}
	for _, c := range reg.Consumers {
		if c.AgentRegistryRef == "" {
			continue
		}
		a := E1aAgent{ConsumerID: c.ConsumerID, Ref: c.AgentRegistryRef}
		out = append(out, a)
		m := agentRef.FindStringSubmatch(c.AgentRegistryRef)
		if id == nil || m == nil || id.String() != m[1] {
			continue
		}
		registry, _ := eth.ParseAddress(m[2])
		agentID, _ := new(big.Int).SetString(m[3], 10)
		call := func(sig string) []byte {
			d, _ := eth.Call(sig, agentID)
			b, _ := chain.Call(ctx, eth.Msg{To: registry, Data: d})
			return b
		}
		address := func(sig string) string {
			w, err := eth.Word(call(sig), 0)
			if err != nil {
				return ""
			}
			o, err := eth.WordAddress(w)
			if err != nil {
				return ""
			}
			return o.Hex()
		}
		a.Owner, a.AgentWallet = address("ownerOf(uint256)"), address("getAgentWallet(uint256)")
		a.AgentWalletIsConsumers = a.AgentWallet != "" && a.AgentWallet != (eth.Address{}).Hex() && strings.EqualFold(a.AgentWallet, wallets[c.WalletID])
		if uri, ok := abiStringOf(call("tokenURI(uint256)")); ok {
			sum := sha256.Sum256([]byte(uri))
			a.URISHA256 = hex.EncodeToString(sum[:])
			var file struct {
				APA struct {
					PaymentWallet string `json:"payment_wallet"`
				} `json:"apa"`
			}
			if raw, err := base64.StdEncoding.DecodeString(strings.TrimPrefix(uri, dataJSON)); strings.HasPrefix(uri, dataJSON) && err == nil && json.Unmarshal(raw, &file) == nil {
				if w, err := eth.ParseAddress(file.APA.PaymentWallet); err == nil {
					a.DeclaredWallet, a.DeclaredIsConsumers = w.Hex(), strings.EqualFold(w.Hex(), wallets[c.WalletID])
				}
			}
		}
		out[len(out)-1] = a
	}
	return out
}

// abiStringOf is an ABI-encoded string return value (a token URI).
func abiStringOf(b []byte) (string, bool) {
	off, err := eth.Uint(b, 0)
	if err != nil || !off.IsUint64() || off.Uint64()+32 > uint64(len(b)) {
		return "", false
	}
	n := new(big.Int).SetBytes(b[off.Uint64() : off.Uint64()+32])
	if !n.IsUint64() || off.Uint64()+32+n.Uint64() > uint64(len(b)) {
		return "", false
	}
	return string(b[off.Uint64()+32 : off.Uint64()+32+n.Uint64()]), true
}
