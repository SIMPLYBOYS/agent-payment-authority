package main

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"math/big"
	"net"
	"os"
	"path/filepath"
	"strings"

	"github.com/SIMPLYBOYS/agent-payment-authority/internal/contracts"
	"github.com/SIMPLYBOYS/agent-payment-authority/internal/eth"
	"github.com/SIMPLYBOYS/agent-payment-authority/internal/keytool"
)

// identityRegistry is ERC-8004's identity registry on Base Sepolia (its
// README; read on chain 11/4: AgentIdentity, 2.0.0; docs/commercial-8004.md 2).
const identityRegistry = "0x8004A818BFB912233c491871b3d84c89A494BD9e"

// family is who each of the town's two Agents shops for (docs/commercial-consumer.md).
var family = map[string]struct{ name, who, pays string }{
	"consumer-a": {"家長的購物 Agent（agent-payment-authority 展示）", "客人 A（家長）", "付款要經過委託、Policy 與本人核准"},
	"consumer-b": {"孩子的購物 Agent（agent-payment-authority 展示）", "客人 B（未滿 18 歲的孩子）", "超過自動範圍的購買由家長核准"},
}

// registration is an Agent's ERC-8004 registration file, on chain as its
// data: URI (E2 A). apa says the payment wallet: declared, not proven (E3 A).
type registration struct {
	Type           string            `json:"type"`
	Name           string            `json:"name"`
	Description    string            `json:"description"`
	Services       []struct{}        `json:"services"`
	X402Support    bool              `json:"x402Support"`
	Active         bool              `json:"active"`
	Registrations  []registrationRef `json:"registrations"`
	SupportedTrust []string          `json:"supportedTrust"`
	APA            struct {
		ConsumerID          string `json:"consumer_id"`
		PaymentWallet       string `json:"payment_wallet"`
		PaymentWalletProven bool   `json:"payment_wallet_proven"`
	} `json:"apa"`
}

type registrationRef struct {
	AgentID       uint64 `json:"agentId"`
	AgentRegistry string `json:"agentRegistry"`
}

func registrationFile(consumer, wallet, registry string, agentID uint64) []byte {
	f := registration{Type: "https://eips.ethereum.org/EIPS/eip-8004#registration-v1", Name: family[consumer].name,
		Description: family[consumer].who + "的 AI 購物助理。它只能提出請購；" + family[consumer].pays + "，由獨立的 Signer 簽名。由展示平台代為註冊；Base Sepolia 上的模擬，商品是示意的。",
		Services:    []struct{}{}, X402Support: true, Active: true, SupportedTrust: []string{},
		Registrations: []registrationRef{{agentID, fmt.Sprintf("eip155:%d:%s", chainID, registry)}}}
	f.APA.ConsumerID, f.APA.PaymentWallet = consumer, wallet
	raw, _ := json.Marshal(f)
	return raw
}

func agentURI(file []byte) string {
	return "data:application/json;base64," + base64.StdEncoding.EncodeToString(file)
}

// agentsFile is DIR/host/agents.json: each transaction is in it before it is
// sent, so a run that stopped goes on from it and never sends a step twice.
type agentsFile struct {
	ChainID  uint64       `json:"chain_id"`
	Registry string       `json:"identity_registry"`
	Owner    string       `json:"owner"`
	Agents   []agentEntry `json:"agents"`
}

type agentEntry struct {
	ConsumerID    string `json:"consumer_id"`
	PaymentWallet string `json:"payment_wallet"`
	AgentID       uint64 `json:"agent_id,omitempty"`           // once register is confirmed
	Ref           string `json:"agent_registry_ref,omitempty"` // CAIP-19: eip155:84532/erc721:<registry>/<agentId>
	RegisterTx    string `json:"register_tx,omitempty"`
	URITx         string `json:"uri_tx,omitempty"`
	UnsetTx       string `json:"unset_wallet_tx,omitempty"`
	URISHA256     string `json:"agent_uri_sha256,omitempty"` // of the agentURI string, as tokenURI returns it
}

// agents is p2-testnet agents (docs/commercial-8004.md I1).
func agents(ctx context.Context, args []string, out io.Writer, ask asker) error {
	fs := flag.NewFlagSet("agents", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	dir := fs.String("dir", "", "apa-relayer's directory, whose host/ deploy wrote")
	rpcURL := fs.String("rpc", defaultRPC, "the Base Sepolia node")
	regAddr := fs.String("identity-registry", identityRegistry, "ERC-8004's identity registry (tests only change it)")
	if err := fs.Parse(args); err != nil || fs.NArg() != 0 || *dir == "" {
		return errors.New("usage: p2-testnet agents --dir DIR [--rpc URL]")
	}
	if *regAddr != identityRegistry && !local {
		return errors.New("--identity-registry is ERC-8004's on Base Sepolia")
	}
	registry, err := eth.ParseAddress(*regAddr)
	if err != nil {
		return err
	}
	if err := keytool.Private(*dir); err != nil {
		return err
	}
	*dir, _ = filepath.Abs(*dir)
	r, err := addresses(*dir)
	if err != nil {
		return err
	}
	// The issuer sends (E1 A), and apa-relayer holds the issuer's key too: one sender of its nonces at a time.
	if c, err := net.Dial("unix", filepath.Join(*dir, "run", "relayer.sock")); err == nil {
		c.Close()
		return errors.New("apa-relayer runs: stop it first (it holds the issuer key, whose nonces this uses); nothing was sent")
	}
	host := filepath.Join(*dir, "host")
	var reg contracts.RegistryConfig
	if err := readJSON(filepath.Join(host, "registry.json"), &reg); err != nil {
		return err
	}
	wallets := map[string]string{}
	for _, w := range reg.Wallets {
		wallets[w.WalletID] = w.From
	}
	j := agentsFile{ChainID: chainID, Registry: *regAddr, Owner: r["issuer"].Hex()}
	for _, id := range []string{"consumer-a", "consumer-b"} {
		e := agentEntry{ConsumerID: id}
		for _, c := range reg.Consumers {
			if c.ConsumerID == id {
				e.PaymentWallet = wallets[c.WalletID]
			}
		}
		if _, err := eth.ParseAddress(e.PaymentWallet); err != nil {
			return fmt.Errorf("the registry has no wallet for %s", id)
		}
		j.Agents = append(j.Agents, e)
	}
	jpath := filepath.Join(host, "agents.json")
	if _, err := os.Lstat(jpath); err == nil {
		var was agentsFile
		if err := readJSON(jpath, &was); err != nil {
			return err
		}
		if was.ChainID != j.ChainID || !strings.EqualFold(was.Registry, j.Registry) || was.Owner != j.Owner || len(was.Agents) != 2 ||
			was.Agents[0].ConsumerID != "consumer-a" || was.Agents[1].ConsumerID != "consumer-b" {
			return fmt.Errorf("%s is another registration's: look at it before anything else; nothing was sent", jpath)
		}
		for i := range was.Agents {
			if was.Agents[i].PaymentWallet != j.Agents[i].PaymentWallet {
				return fmt.Errorf("%s names another payment wallet for %s; nothing was sent", jpath, was.Agents[i].ConsumerID)
			}
		}
		j = was
	}
	rpc, err := node(ctx, *rpcURL, out)
	if err != nil {
		return err
	}
	if b, err := rpc.Call(ctx, eth.Msg{To: registry, Data: eth.Selector("name()")}); err != nil {
		return fmt.Errorf("the identity registry %s: %w", *regAddr, err)
	} else if s, _ := abiString(b); s != "AgentIdentity" {
		return fmt.Errorf("%s is not ERC-8004's identity registry (name %q)", *regAddr, s)
	}
	steps := 0
	for _, e := range j.Agents {
		for _, tx := range []string{e.RegisterTx, e.URITx, e.UnsetTx} {
			if tx == "" {
				steps++
			}
		}
	}
	var key *eth.Key
	if steps > 0 {
		h, err1 := rpc.LatestHeader(ctx)
		tip, err2 := rpc.MaxPriorityFee(ctx)
		have, err3 := rpc.Balance(ctx, r["issuer"])
		if err := errors.Join(err1, err2, err3); err != nil {
			return err
		}
		// At most: register's ~92k gas, then the URI's storage (a word 22.1k) and the wallet's unset, each with room.
		var gas uint64
		for _, e := range j.Agents {
			if e.RegisterTx == "" {
				gas += 150_000
			}
			if e.URITx == "" {
				gas += 150_000 + 23_000*uint64((len(agentURI(registrationFile(e.ConsumerID, e.PaymentWallet, j.Registry, 99999)))+31)/32)
			}
			if e.UnsetTx == "" {
				gas += 100_000
			}
		}
		fee := new(big.Int).Add(new(big.Int).Mul(h.BaseFee, big.NewInt(2)), tip)
		most := new(big.Int).Mul(new(big.Int).Mul(fee, new(big.Int).SetUint64(gas)), big.NewInt(2)) // with Base's L1 data fee
		fmt.Fprintf(out, "p2-testnet: ERC-8004 registration plan on chain %d, registry %s, from the issuer %s (the owner of both):\n", chainID, j.Registry, r["issuer"].Hex())
		for _, e := range j.Agents {
			var todo []string
			for _, s := range []struct{ tx, what string }{{e.RegisterTx, "register()"}, {e.URITx, "setAgentURI (the file below, as a data: URI)"}, {e.UnsetTx, "unsetAgentWallet (it would be the issuer's)"}} {
				if s.tx == "" {
					todo = append(todo, s.what)
				}
			}
			if len(todo) == 0 {
				continue
			}
			fmt.Fprintf(out, "p2-testnet:   %s: %s\n", e.ConsumerID, strings.Join(todo, ", then "))
			if e.URITx == "" {
				var pretty bytes.Buffer
				json.Indent(&pretty, registrationFile(e.ConsumerID, e.PaymentWallet, j.Registry, 0), "      ", "  ")
				shown := strings.Replace(pretty.String(), `"agentId": 0`, `"agentId": <the agentId register mints>`, 1)
				fmt.Fprintf(out, "      %s\n", shown)
			}
		}
		fmt.Fprintf(out, "p2-testnet:   public on chain for good: a later setAgentURI does not take a file back\n")
		fmt.Fprintf(out, "p2-testnet:   at most %s test ETH in all (fee cap %s wei); the issuer holds %s\n", ether(most), fee, ether(have))
		if have.Cmp(most) < 0 {
			return errors.New("the issuer holds less than the plan can cost; nothing was sent")
		}
		if ans, err := ask.Line(fmt.Sprintf(`send these %d transactions? type "yes": `, steps)); err != nil || ans != "yes" {
			return errors.New("not confirmed; nothing was sent")
		}
		pass, err := ask.Passphrase("the issuer key's passphrase: ")
		if err != nil {
			return err
		}
		key, err = keytool.Load(*dir, keytool.RelayerLabel, "issuer", pass)
		clear(pass)
		if err != nil {
			return err
		}
		if key.Addr != r["issuer"] {
			return errors.New("the issuer key is not its file's address")
		}
	}
	// send journals the transaction, sends it and waits until it is deep enough.
	send := func(slot *string, data []byte) (*eth.Receipt, error) {
		gas, err := estimateTo(ctx, rpc, key.Addr, registry, data)
		if err != nil {
			return nil, fmt.Errorf("estimate (nothing was sent): %w", err)
		}
		n, err1 := rpc.PendingNonce(ctx, key.Addr)
		h, err2 := rpc.LatestHeader(ctx)
		tip, err3 := rpc.MaxPriorityFee(ctx)
		if err := errors.Join(err1, err2, err3); err != nil {
			return nil, err
		}
		fee := new(big.Int).Add(new(big.Int).Mul(h.BaseFee, big.NewInt(2)), tip)
		tx := &eth.Tx{ChainID: big.NewInt(chainID), Nonce: n, MaxPriorityFeePerGas: tip, MaxFeePerGas: fee, Gas: gas + gas/5, To: &registry, Value: new(big.Int), Data: data}
		raw, hash, err := tx.Sign(key)
		if err != nil {
			return nil, err
		}
		*slot = "0x" + hex.EncodeToString(hash)
		if err := writeJSON(jpath, j); err != nil { // before it is sent: never sent twice
			return nil, err
		}
		fmt.Fprintf(out, "p2-testnet: sending %s\n", *slot)
		if beforeSend != nil {
			beforeSend(*slot)
		}
		if _, err := rpc.SendRaw(ctx, raw); err != nil {
			return nil, fmt.Errorf("sending %s: %w (unknown: look it up before anything else; agents will not send it again)", *slot, err)
		}
		return confirmed(ctx, rpc, hash)
	}
	wait := func(tx string) (*eth.Receipt, error) {
		h, err := hex.DecodeString(strings.TrimPrefix(tx, "0x"))
		if err != nil {
			return nil, err
		}
		return confirmed(ctx, rpc, h)
	}
	registered, minted := eth.Topic("Registered(uint256,string,address)"), eth.Topic("Transfer(address,address,uint256)")
	for i := range j.Agents {
		e := &j.Agents[i]
		if e.AgentID == 0 {
			var rc *eth.Receipt
			if e.RegisterTx == "" {
				rc, err = send(&e.RegisterTx, eth.Selector("register()"))
			} else {
				rc, err = wait(e.RegisterTx)
			}
			if err != nil {
				return err
			}
			for _, l := range rc.Logs { // Registered(agentId, …, owner), or the ERC-721 mint's Transfer(0, owner, agentId)
				switch {
				case l.Address != registry:
				case len(l.Topics) == 3 && l.Topics[0] == registered:
					e.AgentID = new(big.Int).SetBytes(l.Topics[1][:]).Uint64()
				case len(l.Topics) == 4 && l.Topics[0] == minted && l.Topics[1] == [32]byte{}:
					e.AgentID = new(big.Int).SetBytes(l.Topics[3][:]).Uint64()
				}
			}
			if e.AgentID == 0 {
				return fmt.Errorf("%s registered no agentId the receipt says; look it up before anything else", e.RegisterTx)
			}
			e.Ref = fmt.Sprintf("eip155:%d/erc721:%s/%d", chainID, j.Registry, e.AgentID)
			if err := writeJSON(jpath, j); err != nil {
				return err
			}
			fmt.Fprintf(out, "p2-testnet: %s is agent %d\n", e.ConsumerID, e.AgentID)
		}
		uri := agentURI(registrationFile(e.ConsumerID, e.PaymentWallet, j.Registry, e.AgentID))
		sum := sha256.Sum256([]byte(uri))
		e.URISHA256 = hex.EncodeToString(sum[:])
		data, _ := eth.Call("setAgentURI(uint256,string)", e.AgentID, uri)
		if e.URITx == "" {
			_, err = send(&e.URITx, data)
		} else {
			_, err = wait(e.URITx)
		}
		if err != nil {
			return err
		}
		data, _ = eth.Call("unsetAgentWallet(uint256)", e.AgentID)
		if e.UnsetTx == "" {
			_, err = send(&e.UnsetTx, data)
		} else {
			_, err = wait(e.UnsetTx)
		}
		if err != nil {
			return err
		}
	}
	// What the chain holds now, not what was sent: the owner, the file, no agentWallet.
	for _, e := range j.Agents {
		id := new(big.Int).SetUint64(e.AgentID)
		get := func(sig string) []byte {
			d, _ := eth.Call(sig, id)
			b, _ := rpc.Call(ctx, eth.Msg{To: registry, Data: d})
			return b
		}
		owner, _ := eth.Word(get("ownerOf(uint256)"), 0)
		wallet, _ := eth.Word(get("getAgentWallet(uint256)"), 0)
		uri, _ := abiText(get("tokenURI(uint256)"), 1<<16)
		sum := sha256.Sum256([]byte(uri))
		if o, err := eth.WordAddress(owner); err != nil || o != r["issuer"] {
			return fmt.Errorf("agent %d's owner is not the issuer on chain", e.AgentID)
		}
		if w, err := eth.WordAddress(wallet); err != nil || w != (eth.Address{}) {
			return fmt.Errorf("agent %d still has an agentWallet on chain", e.AgentID)
		}
		if hex.EncodeToString(sum[:]) != e.URISHA256 {
			return fmt.Errorf("agent %d's tokenURI on chain is not its file", e.AgentID)
		}
	}
	if err := writeJSON(jpath, j); err != nil {
		return err
	}
	// The trusted registry names each consumer's Agent (docs/contracts.md 207: the host's, never the model's).
	for i, c := range reg.Consumers {
		for _, e := range j.Agents {
			if c.ConsumerID == e.ConsumerID {
				reg.Consumers[i].AgentRegistryRef = e.Ref
			}
		}
	}
	if _, err := contracts.NewRegistry(reg); err != nil {
		return err
	}
	if err := writeJSON(filepath.Join(host, "registry.json"), reg); err != nil {
		return err
	}
	for _, e := range j.Agents {
		fmt.Fprintf(out, "p2-testnet: %s: agent %d, %s (https://sepolia.basescan.org/nft/%s/%d)\n", e.ConsumerID, e.AgentID, e.Ref, j.Registry, e.AgentID)
	}
	fmt.Fprintf(out, "p2-testnet: wrote %s and the refs in %s\n", jpath, filepath.Join(host, "registry.json"))
	return nil
}

// estimateTo is a call's gas (estimate is a contract creation's).
func estimateTo(ctx context.Context, rpc *eth.Client, from, to eth.Address, data []byte) (uint64, error) {
	var g string
	if err := rpc.Raw(ctx, "eth_estimateGas", &g, map[string]any{"from": from.Hex(), "to": to.Hex(), "data": "0x" + hex.EncodeToString(data)}); err != nil {
		return 0, err
	}
	v, ok := new(big.Int).SetString(strings.TrimPrefix(g, "0x"), 16)
	if !ok || !v.IsUint64() {
		return 0, errors.New("eth_estimateGas: not a quantity")
	}
	return v.Uint64(), nil
}
