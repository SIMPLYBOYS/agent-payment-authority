package signer

import (
	"bytes"
	"encoding/hex"
	"encoding/json"
	"io"
	"net/http"
	"os"
	"strings"
	"testing"

	"github.com/SIMPLYBOYS/agent-payment-authority/internal/signer/signertest"
)

const testRegistry = "0x8004a818bfb912233c491871b3d84c89a494bd9e"

var testOwner = "0x" + strings.Repeat("c", 40)

// docs/commercial-8004.md §8: the administrator names one agent per wallet, on the wallet's chain; anything
// else is not a configuration.
func TestAgentConfig(t *testing.T) {
	with := func(agents ...map[string]any) error {
		var c map[string]any
		json.Unmarshal(signertest.Config("/unused", 0, t0, Address(keyA.PubKey()), Address(keyB.PubKey())), &c)
		c["agents"] = agents
		raw, _ := json.Marshal(c)
		_, err := ParseConfig(raw)
		return err
	}
	ag := func(wallet, chain string, id uint64, registry string) map[string]any {
		return map[string]any{"wallet_id": wallet, "chain_id": chain, "registry": registry, "agent_id": id, "owner": testOwner}
	}
	if err := with(ag("wallet-a", signertest.Chain, 10573, testRegistry), ag("wallet-b", signertest.Chain, 10574, testRegistry)); err != nil {
		t.Fatal(err)
	}
	for name, agents := range map[string][]map[string]any{
		"an unknown wallet":        {ag("wallet-x", signertest.Chain, 1, testRegistry)},
		"two for one wallet":       {ag("wallet-a", signertest.Chain, 1, testRegistry), ag("wallet-a", signertest.Chain, 2, testRegistry)},
		"another chain":            {ag("wallet-a", "1", 1, testRegistry)},
		"no agent ID":              {ag("wallet-a", signertest.Chain, 0, testRegistry)},
		"a registry not lowercase": {ag("wallet-a", signertest.Chain, 1, "0x8004A818BFB912233c491871b3d84c89A494BD9e")},
	} {
		if err := with(agents...); err == nil || !strings.Contains(err.Error(), "agent of wallet") {
			t.Errorf("%s: %v", name, err)
		}
	}
}

// The consent: for the configured agent only, by its own wallet's key, in the registry's domain, its
// deadline within the registry's 5 minutes; nothing else is signed.
func TestAgentWallet(t *testing.T) {
	e := newEnv(t)
	e.cfg.Agents = []Agent{{WalletID: "wallet-a", ChainID: signertest.Chain, Registry: testRegistry, AgentID: 10573, Owner: testOwner}}
	e.cfg.Callers = []uint32{uint32(os.Getuid())}
	s := e.serve(t)
	post := func(r AgentWalletRequest) (int, AgentWalletReply) {
		t.Helper()
		body, _ := json.Marshal(r)
		req, _ := http.NewRequest("POST", "http://signer/v1/agent-wallet", bytes.NewReader(body))
		resp, err := s.client.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		defer resp.Body.Close()
		raw, _ := io.ReadAll(resp.Body)
		var out AgentWalletReply
		json.Unmarshal(raw, &out)
		return resp.StatusCode, out
	}
	st, r := post(AgentWalletRequest{WalletID: "wallet-a", Deadline: t0 + 240})
	wallet := Address(keyA.PubKey())
	if st != http.StatusOK || r.Wallet != wallet || r.AgentID != 10573 || r.Owner != testOwner || r.Registry != testRegistry || r.ChainID != signertest.Chain || r.Deadline != t0+240 {
		t.Fatalf("%d %+v", st, r)
	}
	digest, _ := AgentWalletDigest(e.cfg.Agents[0], wallet, t0+240)
	if r.TypedDigest != "0x"+hex.EncodeToString(digest) || recovered(t, r.Signature, r.TypedDigest) != wallet {
		t.Fatalf("not the wallet's signature over AgentWalletSet: %+v", r)
	}
	// Its domain is the registry's, not the token's: it is no transfer authorization.
	reg, _ := DomainSeparator(Domain{Name: "ERC8004IdentityRegistry", Version: "1", ChainID: signertest.Chain, VerifyingContract: testRegistry})
	tok, _ := DomainSeparator(Domain{Name: "USDC", Version: "2", ChainID: signertest.Chain, VerifyingContract: signertest.Asset})
	if bytes.Equal(reg, tok) || hex.EncodeToString(keccak([]byte(agentWalletType))) == hex.EncodeToString(keccak([]byte(transferType))) {
		t.Fatal("the consent's domain or type is the transfer's")
	}
	for name, c := range map[string]struct {
		r    AgentWalletRequest
		code string
	}{
		"a wallet with no agent": {AgentWalletRequest{WalletID: "wallet-b", Deadline: t0 + 240}, "AGENT_NOT_CONFIGURED"},
		"an unknown wallet":      {AgentWalletRequest{WalletID: "wallet-x", Deadline: t0 + 240}, "AGENT_NOT_CONFIGURED"},
		"a deadline now":         {AgentWalletRequest{WalletID: "wallet-a", Deadline: t0}, "DEADLINE_INVALID"},
		"past 5 minutes":         {AgentWalletRequest{WalletID: "wallet-a", Deadline: t0 + 301}, "DEADLINE_INVALID"},
	} {
		if st, r := post(c.r); st != http.StatusConflict || r.Error != c.code || r.Signature != "" {
			t.Errorf("%s: %d %+v", name, st, r)
		}
	}
	if st, r := post(AgentWalletRequest{WalletID: "wallet-a", Deadline: t0 + 300}); st != http.StatusOK || r.Signature == "" {
		t.Fatalf("5 minutes exactly: %d %+v", st, r)
	}
	// The logs carry the route, the code and the wallet ID, never the signature.
	s.mu.Lock()
	logs := strings.Join(s.logs, "\n")
	s.mu.Unlock()
	if strings.Contains(logs, strings.TrimPrefix(r.Signature, "0x")[:64]) || !strings.Contains(logs, "agent-wallet  wallet-a") || !strings.Contains(logs, "agent-wallet DEADLINE_INVALID wallet-a") {
		t.Fatalf("logs:\n%s", logs)
	}
}
