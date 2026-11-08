package main

// agent-wallet (docs/commercial-8004.md §8): each Agent's agentWallet set to its consumer's payment wallet on
// that wallet's own signature. The Signer signs the consent (AgentWalletSet, for the agent /opt's signer.json
// names for the wallet), the issuer, the Agents' owner, sends setAgentWallet within the consent's deadline.

import (
	"bytes"
	"context"
	"encoding/hex"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"math/big"
	"net"
	"net/http"
	"path/filepath"
	"strings"

	"github.com/SIMPLYBOYS/agent-payment-authority/internal/contracts"
	"github.com/SIMPLYBOYS/agent-payment-authority/internal/eth"
	"github.com/SIMPLYBOYS/agent-payment-authority/internal/keytool"
	"github.com/SIMPLYBOYS/agent-payment-authority/internal/signer"
)

// optSignerSocket is /opt's Signer (scripts/demo/demo-testnet.sh).
const optSignerSocket = "/opt/apa-signer/run/signer.sock"

// consentDelay: the consent's deadline after the latest block's time, inside the registry's 5 minutes.
const consentDelay = 240

// consent asks the Signer on socket for wallet's consent until deadline.
func consent(ctx context.Context, socket, wallet string, deadline uint64) (signer.AgentWalletReply, error) {
	c := &http.Client{Transport: &http.Transport{DialContext: func(ctx context.Context, _, _ string) (net.Conn, error) {
		return (&net.Dialer{}).DialContext(ctx, "unix", socket)
	}}}
	body, _ := json.Marshal(signer.AgentWalletRequest{WalletID: wallet, Deadline: deadline})
	req, _ := http.NewRequestWithContext(ctx, "POST", "http://signer/v1/agent-wallet", bytes.NewReader(body))
	resp, err := c.Do(req)
	if err != nil {
		return signer.AgentWalletReply{}, fmt.Errorf("the Signer at %s: %w", socket, err)
	}
	defer resp.Body.Close()
	var r signer.AgentWalletReply
	if err := json.NewDecoder(io.LimitReader(resp.Body, 64<<10)).Decode(&r); err != nil {
		return r, fmt.Errorf("the Signer answered %d, not its reply", resp.StatusCode)
	}
	if resp.StatusCode != http.StatusOK || r.Error != "" {
		return r, fmt.Errorf("the Signer refused: %d %s (its signer.json names this wallet's agent? the new apa-signer installed?)", resp.StatusCode, r.Error)
	}
	return r, nil
}

func agentWallet(ctx context.Context, args []string, out io.Writer, ask asker) error {
	fs := flag.NewFlagSet("agent-wallet", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	dir := fs.String("dir", "", "apa-relayer's directory, whose host/agents.json agents wrote")
	rpcURL := fs.String("rpc", defaultRPC, "the Base Sepolia node")
	socket := fs.String("signer", optSignerSocket, "the Signer's socket")
	if err := fs.Parse(args); err != nil || fs.NArg() != 0 || *dir == "" {
		return errors.New("usage: p2-testnet agent-wallet --dir DIR [--rpc URL] [--signer SOCKET]")
	}
	if err := keytool.Private(*dir); err != nil {
		return err
	}
	*dir, _ = filepath.Abs(*dir)
	r, err := addresses(*dir)
	if err != nil {
		return err
	}
	if c, err := net.Dial("unix", filepath.Join(*dir, "run", "relayer.sock")); err == nil {
		c.Close()
		return errors.New("apa-relayer runs: stop it first (it holds the issuer key, whose nonces this uses); nothing was sent")
	}
	host := filepath.Join(*dir, "host")
	jpath := filepath.Join(host, "agents.json")
	var j agentsFile
	var reg contracts.RegistryConfig
	if err := errors.Join(readJSON(jpath, &j), readJSON(filepath.Join(host, "registry.json"), &reg)); err != nil {
		return fmt.Errorf("agents' files (p2-testnet agents first): %w", err)
	}
	if !strings.EqualFold(j.Registry, identityRegistry) && !local || j.ChainID != chainID || j.Owner != r["issuer"].Hex() {
		return fmt.Errorf("%s is not agents' registration on Base Sepolia by the issuer; nothing was sent", jpath)
	}
	registry, err := eth.ParseAddress(strings.ToLower(j.Registry))
	if err != nil {
		return err
	}
	walletOf := map[string]string{} // consumer -> its wallet ID
	for _, c := range reg.Consumers {
		walletOf[c.ConsumerID] = c.WalletID
	}
	rpc, err := node(ctx, *rpcURL, out)
	if err != nil {
		return err
	}
	onChain := func(id uint64) eth.Address {
		d, _ := eth.Call("getAgentWallet(uint256)", id)
		b, _ := rpc.Call(ctx, eth.Msg{To: registry, Data: d})
		w, _ := eth.Word(b, 0)
		a, _ := eth.WordAddress(w)
		return a
	}
	var todo []int
	for i, e := range j.Agents {
		if e.AgentID == 0 || walletOf[e.ConsumerID] == "" {
			return fmt.Errorf("%s: %s has no agent or no wallet yet (p2-testnet agents first); nothing was sent", jpath, e.ConsumerID)
		}
		w, err := eth.ParseAddress(strings.ToLower(e.PaymentWallet))
		if err != nil {
			return err
		}
		if e.WalletTx != "" && onChain(e.AgentID) != w { // sent before: its outcome first
			h, _ := hex.DecodeString(strings.TrimPrefix(e.WalletTx, "0x"))
			rc, err := rpc.Receipt(ctx, h)
			if err != nil || rc == nil {
				return fmt.Errorf("%s's %s has no receipt yet: look it up before anything else; nothing was sent", e.ConsumerID, e.WalletTx)
			}
			if rc.Status == 1 {
				return fmt.Errorf("%s's %s succeeded but agent %d's agentWallet is not %s on chain: stop and look", e.ConsumerID, e.WalletTx, e.AgentID, w.Hex())
			}
			fmt.Fprintf(out, "p2-testnet: %s's %s reverted (nothing changed): its consent is asked for again\n", e.ConsumerID, e.WalletTx)
		}
		if onChain(e.AgentID) == w {
			fmt.Fprintf(out, "p2-testnet: %s: agent %d's agentWallet is %s already\n", e.ConsumerID, e.AgentID, w.Hex())
			continue
		}
		todo = append(todo, i)
	}
	if len(todo) == 0 {
		return nil
	}
	h, err1 := rpc.LatestHeader(ctx)
	tip, err2 := rpc.MaxPriorityFee(ctx)
	have, err3 := rpc.Balance(ctx, r["issuer"])
	if err := errors.Join(err1, err2, err3); err != nil {
		return err
	}
	fee := new(big.Int).Add(new(big.Int).Mul(h.BaseFee, big.NewInt(2)), tip)
	most := new(big.Int).Mul(new(big.Int).Mul(fee, big.NewInt(int64(150_000*len(todo)))), big.NewInt(2)) // with Base's L1 data fee
	fmt.Fprintf(out, "p2-testnet: agentWallet plan on chain %d, registry %s, sent by the issuer %s (the owner):\n", chainID, j.Registry, r["issuer"].Hex())
	for _, i := range todo {
		e := j.Agents[i]
		fmt.Fprintf(out, "p2-testnet:   %s: agent %d's agentWallet := %s (%s), on that wallet's own signature from the Signer at %s\n", e.ConsumerID, e.AgentID, e.PaymentWallet, walletOf[e.ConsumerID], *socket)
	}
	fmt.Fprintf(out, "p2-testnet:   the registry file on chain is left as it is (it still says payment_wallet_proven false; the agentWallet is the proof)\n")
	fmt.Fprintf(out, "p2-testnet:   at most %s test ETH in all (fee cap %s wei); the issuer holds %s\n", ether(most), fee, ether(have))
	if have.Cmp(most) < 0 {
		return errors.New("the issuer holds less than the plan can cost; nothing was sent")
	}
	if ans, err := ask.Line(fmt.Sprintf(`send these %d transactions? type "yes": `, len(todo))); err != nil || ans != "yes" {
		return errors.New("not confirmed; nothing was sent")
	}
	pass, err := ask.Passphrase("the issuer key's passphrase: ")
	if err != nil {
		return err
	}
	key, err := keytool.Load(*dir, keytool.RelayerLabel, "issuer", pass)
	clear(pass)
	if err != nil {
		return err
	}
	if key.Addr != r["issuer"] {
		return errors.New("the issuer key is not its file's address")
	}
	for _, i := range todo {
		e := &j.Agents[i]
		h, err := rpc.LatestHeader(ctx)
		if err != nil {
			return err
		}
		deadline := h.Timestamp + consentDelay
		c, err := consent(ctx, *socket, walletOf[e.ConsumerID], deadline)
		if err != nil {
			return err
		}
		sig, err := hex.DecodeString(strings.TrimPrefix(c.Signature, "0x"))
		// What the Signer signed is what is sent: this agent, registry, owner and wallet, this deadline.
		if err != nil || len(sig) != 65 || c.AgentID != e.AgentID || !strings.EqualFold(c.Registry, j.Registry) || !strings.EqualFold(c.Owner, j.Owner) ||
			!strings.EqualFold(c.Wallet, e.PaymentWallet) || c.Deadline != deadline || c.ChainID != fmt.Sprint(chainID) {
			return fmt.Errorf("the Signer's consent for %s is not for agent %d, %s and its owner on chain %d (its signer.json?); nothing was sent", e.ConsumerID, e.AgentID, e.PaymentWallet, chainID)
		}
		w, _ := eth.ParseAddress(strings.ToLower(e.PaymentWallet))
		data, _ := eth.Call("setAgentWallet(uint256,address,uint256,bytes)", e.AgentID, w, deadline, sig)
		if _, err := sendJournaled(ctx, rpc, key, registry, data, &e.WalletTx, func() error { return writeJSON(jpath, j) }, out); err != nil {
			return err
		}
		if got := onChain(e.AgentID); got != w {
			return fmt.Errorf("agent %d's agentWallet is %s on chain after %s, not %s: stop and look", e.AgentID, got.Hex(), e.WalletTx, w.Hex())
		}
		fmt.Fprintf(out, "p2-testnet: %s: agent %d's agentWallet is %s, on its own signature (%s)\n", e.ConsumerID, e.AgentID, w.Hex(), e.WalletTx)
	}
	return nil
}
