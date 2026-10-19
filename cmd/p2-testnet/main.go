// Command p2-testnet prepares the P2 payment loop on the Base Sepolia
// testnet (docs/commercial-p2b.md B4; its §6 steps 1 and 4, each run only
// once Aaron approves it).
//
//	p2-testnet preflight --dir DIR --wallet-a 0x... --wallet-b 0x... [--rpc URL] [--test402 URL]
//	p2-testnet deploy    --dir DIR --wallet-a 0x... --wallet-b 0x... [--rpc URL]
//
// DIR is apa-relayer's directory: DIR/keys holds the relayer-gas, issuer,
// policy and shop key files (apa-relayer keygen). Their addresses are read
// from the files, which hold them in the clear; no key is decrypted except
// the issuer's, by deploy, which sends from it (the issuer is the deployer,
// P2b-D3), with a passphrase asked on this terminal.
//
// preflight reads only: the chain (84532, not an anvil), Circle's USDC
// (name, version, decimals), every address's balance, and, with --test402,
// one GET of test402's resource and its PAYMENT-REQUIRED. deploy runs the
// same checks, prints the two transactions (the Z2 verifier and the budget
// contract) with their sender, nonces and most they can cost, and sends
// them only after "yes"; each is written to DIR/deploy.json before it is
// sent, so deploy never runs twice. Once both are 2 blocks deep it writes
// DIR/relayer.json and DIR/host/ (payments.json, registry.json,
// profile.json) for apa-relayer and make present-testnet.
package main

import (
	"context"
	"encoding/hex"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"math/big"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	"github.com/SIMPLYBOYS/agent-payment-authority/internal/contracts"
	"github.com/SIMPLYBOYS/agent-payment-authority/internal/eth"
	"github.com/SIMPLYBOYS/agent-payment-authority/internal/keytool"
	"github.com/SIMPLYBOYS/agent-payment-authority/internal/policy"
	"github.com/SIMPLYBOYS/agent-payment-authority/internal/signer"
	"github.com/SIMPLYBOYS/agent-payment-authority/internal/x402"
)

const (
	chainID       = 84532
	defaultRPC    = "https://sepolia.base.org"                   // P2b-D1 A
	circleUSDC    = "0x036cbd53842c5426634e7929541ec2318f3dcf7e" // Circle's Base Sepolia USDC (docs/commercial-p2b.md 2)
	confirmations = 2                                            // P2-D6
	relayerCap    = 12                                           // P2b-D5
	shopCap       = 4
	policyTTL     = 600
	assetID       = "usdc-base-sepolia"
)

// local lets a test use a local anvil (http, loopback, answering as anvil)
// and its own token; never set outside tests.
var local = false

func main() {
	err := run(context.Background(), os.Args[1:], os.Stdout, terminal{})
	if err != nil {
		fmt.Fprintln(os.Stderr, "p2-testnet:", err)
		os.Exit(1)
	}
}

// asker is the terminal: a passphrase (echo off) and a line.
type asker interface {
	Passphrase(prompt string) ([]byte, error)
	Line(prompt string) (string, error)
}

type terminal struct{}

func (terminal) tty() (*os.File, error) {
	t, err := os.OpenFile("/dev/tty", os.O_RDWR, 0)
	if err != nil {
		return nil, errors.New("this asks on a terminal (/dev/tty), and there is none")
	}
	return t, nil
}

func (t terminal) Passphrase(prompt string) ([]byte, error) {
	f, err := t.tty()
	if err != nil {
		return nil, err
	}
	defer f.Close()
	return keytool.ReadPassphrase(f, "p2-testnet", prompt)
}

func (t terminal) Line(prompt string) (string, error) {
	f, err := t.tty()
	if err != nil {
		return "", err
	}
	defer f.Close()
	fmt.Fprint(f, "p2-testnet "+prompt)
	var line []byte
	b := make([]byte, 1)
	for len(line) < 64 {
		if n, err := f.Read(b); err != nil || n == 0 || b[0] == '\n' {
			break
		}
		line = append(line, b[0])
	}
	return strings.TrimSpace(string(line)), nil
}

type options struct {
	dir, rpc, test402, contracts, zkbudget, registry, profile, token string
	walletA, walletB                                                 eth.Address
}

func run(ctx context.Context, args []string, out io.Writer, ask asker) error {
	usage := errors.New("usage: p2-testnet preflight|deploy --dir DIR --wallet-a 0x... --wallet-b 0x... [--rpc URL] [--test402 URL]")
	if len(args) == 0 || args[0] != "preflight" && args[0] != "deploy" {
		return usage
	}
	fs := flag.NewFlagSet(args[0], flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	var o options
	var a, b string
	fs.StringVar(&o.dir, "dir", "", "apa-relayer's directory (DIR/keys: apa-relayer keygen)")
	fs.StringVar(&a, "wallet-a", "", "wallet-a's address (apa-signer keygen printed it)")
	fs.StringVar(&b, "wallet-b", "", "wallet-b's address")
	fs.StringVar(&o.rpc, "rpc", defaultRPC, "the Base Sepolia node")
	fs.StringVar(&o.test402, "test402", "", "preflight: test402's resource URL, asked once")
	fs.StringVar(&o.contracts, "contracts", "var/contracts/out", "the contract build")
	fs.StringVar(&o.zkbudget, "zkbudget", "", "the zkbudget binary apa-relayer runs (default DIR/zkbudget)")
	fs.StringVar(&o.registry, "registry", "testdata/demo/registry.json", "the demo registry the written one is made from")
	fs.StringVar(&o.profile, "profile", "testdata/policy/m1-demo-profile.json", "the profile the written one is made from")
	fs.StringVar(&o.token, "token", circleUSDC, "the USDC contract (tests only change it)")
	if err := fs.Parse(args[1:]); err != nil || fs.NArg() != 0 || o.dir == "" {
		return usage
	}
	var err1, err2 error
	o.walletA, err1 = eth.ParseAddress(a)
	o.walletB, err2 = eth.ParseAddress(b)
	if errors.Join(err1, err2) != nil || o.walletA == o.walletB {
		return errors.New("--wallet-a and --wallet-b are the two payer wallets' addresses")
	}
	if o.token != circleUSDC && !local {
		return errors.New("--token is Circle's Base Sepolia USDC")
	}
	if err := keytool.Private(o.dir); err != nil {
		return err
	}
	o.dir, _ = filepath.Abs(o.dir)
	if o.zkbudget == "" {
		o.zkbudget = filepath.Join(o.dir, "zkbudget")
	}
	o.zkbudget, _ = filepath.Abs(o.zkbudget)
	roles, err := addresses(o.dir)
	if err != nil {
		return err
	}
	rpc, err := node(ctx, o.rpc, out)
	if err != nil {
		return err
	}
	tok, err := usdc(ctx, rpc, o.token, out)
	if err != nil {
		return err
	}
	bal, err := balances(ctx, rpc, tok.addr, roles, o, out)
	if err != nil {
		return err
	}
	if args[0] == "preflight" {
		if o.test402 != "" {
			if err := test402(ctx, o.test402, tok.addr, out); err != nil {
				return err
			}
		}
		if missing := short(bal); missing != "" {
			fmt.Fprintln(out, "p2-testnet: not ready for deploy:", missing)
		} else {
			fmt.Fprintln(out, "p2-testnet: ready for deploy")
		}
		return nil
	}
	return deploy(ctx, rpc, tok, roles, bal, o, ask, out)
}

// roles are the Host's key addresses, read from their files (not decrypted).
type roles map[string]eth.Address

var roleOrder = []string{"relayer-gas", "issuer", "policy", "shop"}

func addresses(dir string) (roles, error) {
	if err := keytool.Private(filepath.Join(dir, "keys")); err != nil {
		return nil, err
	}
	r := roles{}
	seen := map[eth.Address]bool{}
	for _, role := range roleOrder {
		f, err := signer.ReadKeyFile(filepath.Join(dir, "keys", role+".key"))
		if err != nil {
			return nil, fmt.Errorf("%s: %w (make it with apa-relayer keygen --dir %s --role %s)", role, err, dir, role)
		}
		a, err := eth.ParseAddress(f.Address)
		if err != nil || f.WalletID != role || seen[a] {
			return nil, fmt.Errorf("%s: not that role's key file, or the address of another role", role)
		}
		r[role], seen[a] = a, true
	}
	return r, nil
}

// node is the Base Sepolia node: https, not this machine, chain 84532, not an anvil.
func node(ctx context.Context, raw string, out io.Writer) (*eth.Client, error) {
	u, err := url.Parse(raw)
	if !local && (err != nil || u.Scheme != "https" || u.User != nil || u.Host == "" || eth.LocalRPC("http://"+u.Host) == nil) {
		return nil, errors.New("--rpc is a public https node without user info")
	}
	rpc := eth.NewClient(raw)
	id, err := rpc.ChainID(ctx)
	if err != nil || !id.IsUint64() || id.Uint64() != chainID {
		return nil, fmt.Errorf("the node is not chain %d (or did not answer)", chainID)
	}
	var client string
	if err := rpc.Raw(ctx, "web3_clientVersion", &client); err != nil {
		return nil, err
	}
	if strings.HasPrefix(client, "anvil/") && !local {
		return nil, errors.New("the node answers as a local anvil, not Base Sepolia")
	}
	block, err := rpc.BlockNumber(ctx)
	if err != nil {
		return nil, err
	}
	fmt.Fprintf(out, "p2-testnet: chain %d at %s (%s), block %d\n", chainID, u.Host, client, block) // the host only: a URL may carry a key
	return rpc, nil
}

type token struct {
	addr          eth.Address
	name, version string
}

// usdc reads the token's EIP-712 name and version and its decimals: the
// Signer signs under exactly that domain.
func usdc(ctx context.Context, rpc *eth.Client, raw string, out io.Writer) (token, error) {
	t := token{}
	var err error
	if t.addr, err = eth.ParseAddress(raw); err != nil {
		return t, err
	}
	var code string
	if err := rpc.Raw(ctx, "eth_getCode", &code, t.addr.Hex(), "latest"); err != nil || len(code) <= 2 {
		return t, fmt.Errorf("no contract at %s (%v)", t.addr.Hex(), err)
	}
	str := func(sig string) (string, error) {
		b, err := rpc.Call(ctx, eth.Msg{To: t.addr, Data: eth.Selector(sig)})
		if err != nil {
			return "", err
		}
		return abiString(b)
	}
	if t.name, err = str("name()"); err != nil {
		return t, err
	}
	if t.version, err = str("version()"); err != nil {
		return t, err
	}
	b, err := rpc.Call(ctx, eth.Msg{To: t.addr, Data: eth.Selector("decimals()")})
	if err != nil {
		return t, err
	}
	d, err := eth.Uint(b, 0)
	if err != nil {
		return t, err
	}
	fmt.Fprintf(out, "p2-testnet: USDC %s: name %q, version %q, decimals %s\n", t.addr.Hex(), t.name, t.version, d)
	if t.name != "USDC" || t.version == "" || d.Cmp(big.NewInt(6)) != 0 {
		return t, errors.New("the token is not USDC with 6 decimals and a version: stop (docs/commercial-p2b.md 2)")
	}
	return t, nil
}

// abiString decodes one ABI-encoded string return value.
func abiString(b []byte) (string, error) {
	off, err := eth.Uint(b, 0)
	if err != nil || !off.IsUint64() || off.Uint64()%32 != 0 || off.Uint64()+32 > uint64(len(b)) {
		return "", errors.New("not an ABI string")
	}
	o := off.Uint64()
	n := new(big.Int).SetBytes(b[o : o+32])
	if !n.IsUint64() || n.Uint64() > 256 || o+32+n.Uint64() > uint64(len(b)) {
		return "", errors.New("not an ABI string")
	}
	return string(b[o+32 : o+32+n.Uint64()]), nil
}

type balance struct {
	who string
	eth *big.Int // the gas accounts'
	usd *big.Int // the payer wallets'
}

// balances prints each address's balance: test ETH for the three that
// send, test USDC for the two payers (P2b-D3, D6).
func balances(ctx context.Context, rpc *eth.Client, tok eth.Address, r roles, o options, out io.Writer) ([]balance, error) {
	var bs []balance
	for _, role := range roleOrder {
		w, err := rpc.Balance(ctx, r[role])
		if err != nil {
			return nil, err
		}
		need := "needs test ETH"
		if role == "policy" {
			need = "signs only, sends nothing"
		}
		fmt.Fprintf(out, "p2-testnet: %-11s %s  %s ETH (%s)\n", role, r[role].Hex(), ether(w), need)
		if role != "policy" {
			bs = append(bs, balance{who: role, eth: w})
		}
	}
	for _, w := range []struct {
		id   string
		addr eth.Address
	}{{"wallet-a", o.walletA}, {"wallet-b", o.walletB}} {
		data, _ := eth.Call("balanceOf(address)", w.addr)
		b, err := rpc.Call(ctx, eth.Msg{To: tok, Data: data})
		if err != nil {
			return nil, err
		}
		v, err := eth.Uint(b, 0)
		if err != nil {
			return nil, err
		}
		fmt.Fprintf(out, "p2-testnet: %-11s %s  %s USDC (needs test USDC)\n", w.id, w.addr.Hex(), units(v, 6))
		bs = append(bs, balance{who: w.id, usd: v})
	}
	return bs, nil
}

func short(bs []balance) string {
	var missing []string
	for _, b := range bs {
		if b.eth != nil && b.eth.Sign() == 0 || b.usd != nil && b.usd.Sign() == 0 {
			missing = append(missing, b.who)
		}
	}
	if len(missing) == 0 {
		return ""
	}
	return "no test funds yet for " + strings.Join(missing, ", ") + " (P2b-D6: from the faucets, once each)"
}

func ether(w *big.Int) string { return units(w, 18) }

// units is v with dec decimals, at most 6 shown.
func units(v *big.Int, dec int) string {
	s := new(big.Rat).SetFrac(v, new(big.Int).Exp(big.NewInt(10), big.NewInt(int64(dec)), nil)).FloatString(6)
	return s
}

// test402 asks test402's resource once and prints its PAYMENT-REQUIRED:
// the B5 product is made from what it says now (docs/commercial-p2b.md B5).
func test402(ctx context.Context, resource string, tok eth.Address, out io.Writer) error {
	u, err := url.Parse(resource)
	if err != nil || u.Scheme != "https" || u.Host != "test402.com" {
		return errors.New("--test402 is an https URL on test402.com")
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, resource, nil)
	if err != nil {
		return err
	}
	c := &http.Client{Timeout: 20 * time.Second, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	res, err := c.Do(req)
	if err != nil {
		return fmt.Errorf("test402: %w", err)
	}
	res.Body.Close()
	if res.StatusCode != http.StatusPaymentRequired {
		return fmt.Errorf("test402: status %d, not 402", res.StatusCode)
	}
	var pr struct {
		X402Version int `json:"x402Version"`
		Accepts     []struct {
			Scheme  string          `json:"scheme"`
			Network string          `json:"network"`
			Amount  string          `json:"amount"`
			Asset   string          `json:"asset"`
			PayTo   string          `json:"payTo"`
			Extra   json.RawMessage `json:"extra"`
		} `json:"accepts"`
	}
	if err := x402.Decode(res.Header.Get(x402.HeaderPaymentRequired), &pr); err != nil {
		return fmt.Errorf("test402: PAYMENT-REQUIRED: %w", err)
	}
	for _, a := range pr.Accepts {
		fmt.Fprintf(out, "p2-testnet: test402 %s: x402 v%d, %s on %s, amount %s, asset %s, payTo %s, extra %s\n",
			u.Host+u.Path, pr.X402Version, a.Scheme, a.Network, a.Amount, a.Asset, a.PayTo, a.Extra)
		if as, _ := eth.ParseAddress(a.Asset); a.Network != "eip155:84532" || as != tok {
			fmt.Fprintln(out, "p2-testnet: test402: this one is not Base Sepolia USDC")
		}
	}
	if len(pr.Accepts) == 0 {
		return errors.New("test402: no accepted payment")
	}
	return nil
}

// journal is DIR/deploy.json: every deploy transaction, written before it is sent.
type journal struct {
	Chain uint64   `json:"chain_id"`
	From  string   `json:"from"`
	Txs   []string `json:"txs"` // hashes, in order
}

// deploy sends the verifier and the budget contract from the issuer, then
// writes the files apa-relayer and the Host start from.
func deploy(ctx context.Context, rpc *eth.Client, tok token, r roles, bal []balance, o options, ask asker, out io.Writer) error {
	jpath := filepath.Join(o.dir, "deploy.json")
	for _, p := range []string{jpath, filepath.Join(o.dir, "relayer.json"), filepath.Join(o.dir, "host"), filepath.Join(o.dir, "run", "relayer.sock")} {
		if _, err := os.Lstat(p); !errors.Is(err, os.ErrNotExist) {
			return fmt.Errorf("%s exists: deploy has run (or apa-relayer runs); nothing was sent", p)
		}
	}
	if missing := short(bal); missing != "" {
		return errors.New(missing)
	}
	code := func(name string) ([]byte, error) {
		raw, err := os.ReadFile(filepath.Join(o.contracts, name+".sol", name+".json"))
		if err != nil {
			return nil, fmt.Errorf("the contract build: %w", err)
		}
		var a struct{ Bytecode struct{ Object string } }
		if err := json.Unmarshal(raw, &a); err != nil {
			return nil, err
		}
		return hex.DecodeString(strings.TrimPrefix(a.Bytecode.Object, "0x"))
	}
	vcode, err1 := code("Verifier")
	bcode, err2 := code("Budget")
	if err := errors.Join(err1, err2); err != nil {
		return err
	}
	ctor := func(verifier eth.Address) []byte {
		c, _ := eth.Encode(verifier, r["issuer"], r["policy"])
		return append(append([]byte{}, bcode...), c...)
	}
	from := r["issuer"]
	gv, err1 := estimate(ctx, rpc, from, vcode)
	gb, err2 := estimate(ctx, rpc, from, ctor(from)) // the constructor only stores the verifier's address: the same gas
	if err := errors.Join(err1, err2); err != nil {
		return fmt.Errorf("estimate: %w", err)
	}
	nonce, err := rpc.PendingNonce(ctx, from)
	if err != nil {
		return err
	}
	h, err := rpc.LatestHeader(ctx)
	if err != nil {
		return err
	}
	tip, err := rpc.MaxPriorityFee(ctx)
	if err != nil {
		return err
	}
	fee := new(big.Int).Add(new(big.Int).Mul(h.BaseFee, big.NewInt(2)), tip)
	most := new(big.Int).Mul(fee, new(big.Int).SetUint64(gv+gb))
	var have *big.Int
	for _, b := range bal {
		if b.who == "issuer" {
			have = b.eth
		}
	}
	fmt.Fprintf(out, "p2-testnet: deploy plan on chain %d, from the issuer %s:\n", chainID, from.Hex())
	fmt.Fprintf(out, "p2-testnet:   1. the Z2 verifier (nonce %d, gas %d)\n", nonce, gv)
	fmt.Fprintf(out, "p2-testnet:   2. the budget contract, issuer %s, policy signer %s (nonce %d, gas %d)\n", r["issuer"].Hex(), r["policy"].Hex(), nonce+1, gb)
	fmt.Fprintf(out, "p2-testnet:   at most %s test ETH in all (fee cap %s wei); the issuer holds %s\n", ether(most), fee, ether(have))
	if have.Cmp(most) < 0 {
		return errors.New("the issuer holds less than the plan can cost; nothing was sent")
	}
	if ans, err := ask.Line(`send these 2 transactions? type "yes": `); err != nil || ans != "yes" {
		return errors.New("not confirmed; nothing was sent")
	}
	pass, err := ask.Passphrase("the issuer key's passphrase: ")
	if err != nil {
		return err
	}
	key, err := keytool.Load(o.dir, keytool.RelayerLabel, "issuer", pass)
	clear(pass)
	if err != nil {
		return err
	}
	if key.Addr != from {
		return errors.New("the issuer key is not its file's address")
	}
	j := journal{Chain: chainID, From: from.Hex()}
	send := func(n uint64, data []byte, gas uint64) (*eth.Receipt, error) {
		tx := &eth.Tx{ChainID: big.NewInt(chainID), Nonce: n, MaxPriorityFeePerGas: tip, MaxFeePerGas: fee, Gas: gas, Value: new(big.Int), Data: data}
		raw, hash, err := tx.Sign(key)
		if err != nil {
			return nil, err
		}
		j.Txs = append(j.Txs, "0x"+hex.EncodeToString(hash))
		if err := writeJSON(jpath, j); err != nil { // written before it is sent: deploy never sends it twice
			return nil, err
		}
		fmt.Fprintf(out, "p2-testnet: sending %s\n", j.Txs[len(j.Txs)-1])
		if _, err := rpc.SendRaw(ctx, raw); err != nil {
			return nil, fmt.Errorf("sending %s: %w (unknown: look it up before anything else; deploy will not run again)", j.Txs[len(j.Txs)-1], err)
		}
		return confirmed(ctx, rpc, hash)
	}
	vr, err := send(nonce, vcode, gv+gv/5)
	if err != nil {
		return err
	}
	if vr.ContractAddress == nil {
		return errors.New("the verifier's receipt has no contract address")
	}
	br, err := send(nonce+1, ctor(*vr.ContractAddress), gb+gb/5)
	if err != nil {
		return err
	}
	if br.ContractAddress == nil {
		return errors.New("the budget contract's receipt has no contract address")
	}
	budget := *br.ContractAddress
	// The deployed contract names the verifier, issuer and policy signer this plan named.
	for sig, want := range map[string]eth.Address{"verifier()": *vr.ContractAddress, "issuer()": r["issuer"], "policySigner()": r["policy"]} {
		b, err := rpc.Call(ctx, eth.Msg{To: budget, Data: eth.Selector(sig)})
		if err != nil {
			return err
		}
		w, _ := eth.Word(b, 0)
		if got, err := eth.WordAddress(w); err != nil || got != want {
			return fmt.Errorf("the budget contract's %s is not %s", sig, want.Hex())
		}
	}
	fmt.Fprintf(out, "p2-testnet: verifier %s (block %d, %s), budget %s (block %d, %s)\n", vr.ContractAddress.Hex(), vr.BlockNumber, j.Txs[0], budget.Hex(), br.BlockNumber, j.Txs[1])
	return writeFiles(o, tok, r, budget, vr.BlockNumber, out)
}

func estimate(ctx context.Context, rpc *eth.Client, from eth.Address, data []byte) (uint64, error) {
	var g string
	if err := rpc.Raw(ctx, "eth_estimateGas", &g, map[string]any{"from": from.Hex(), "data": "0x" + hex.EncodeToString(data)}); err != nil {
		return 0, err
	}
	v, ok := new(big.Int).SetString(strings.TrimPrefix(g, "0x"), 16)
	if !ok || !v.IsUint64() {
		return 0, errors.New("eth_estimateGas: not a quantity")
	}
	return v.Uint64(), nil
}

// confirmWait bounds the wait for one transaction's depth.
var confirmWait, confirmPoll = 5 * time.Minute, 2 * time.Second

// confirmed waits until the transaction is `confirmations` blocks deep and succeeded.
func confirmed(ctx context.Context, rpc *eth.Client, hash []byte) (*eth.Receipt, error) {
	end := time.Now().Add(confirmWait)
	for time.Now().Before(end) {
		rc, err := rpc.Receipt(ctx, hash)
		if err == nil && rc != nil {
			if rc.Status != 1 {
				return nil, fmt.Errorf("0x%x reverted (block %d); stop and look", hash, rc.BlockNumber)
			}
			if n, err := rpc.BlockNumber(ctx); err == nil && n+1 >= rc.BlockNumber+confirmations {
				return rc, nil
			}
		}
		time.Sleep(confirmPoll)
	}
	return nil, fmt.Errorf("0x%x is not %d blocks deep after %s: unknown; look it up before anything else (deploy will not run again)", hash, confirmations, confirmWait)
}

// writeFiles writes apa-relayer's relayer.json and the Host's files.
func writeFiles(o options, tok token, r roles, budget eth.Address, from uint64, out io.Writer) error {
	root, _ := filepath.Abs(".")
	host, run := filepath.Join(o.dir, "host"), filepath.Join(o.dir, "run")
	for _, d := range []string{host, run} {
		if err := os.Mkdir(d, 0o700); err != nil {
			return err
		}
	}
	var reg contracts.RegistryConfig
	if err := readJSON(o.registry, &reg); err != nil {
		return err
	}
	var ia, ib = -1, -1
	for i, w := range reg.Wallets {
		switch w.WalletID {
		case "wallet-a":
			ia = i
		case "wallet-b":
			ib = i
		}
	}
	if ia < 0 || ib < 0 || len(reg.Assets) != 1 {
		return errors.New("the registry has no wallet-a, wallet-b or one asset")
	}
	reg.Wallets[ia].From, reg.Wallets[ib].From = o.walletA.Hex(), o.walletB.Hex()
	reg.Assets[0].AssetID, reg.Assets[0].AssetContract, reg.Assets[0].EIP712Name, reg.Assets[0].EIP712Version = assetID, tok.addr.Hex(), tok.name, tok.version
	reg.MerchantPayTo = r["shop"].Hex()
	if _, err := contracts.NewRegistry(reg); err != nil {
		return fmt.Errorf("the written registry: %w", err)
	}
	var prof map[string]any
	if err := readJSON(o.profile, &prof); err != nil {
		return err
	}
	prof["asset_id"], prof["merchants"] = assetID, []string{r["shop"].Hex()}
	raw, _ := json.Marshal(prof)
	if _, err := policy.LoadProfile(raw); err != nil {
		return fmt.Errorf("the written profile: %w", err)
	}
	sock := filepath.Join(run, "relayer.sock")
	pay := map[string]any{"environment": "base_sepolia", "rpc": o.rpc, "chain_id": chainID, "confirmations": confirmations, "token": tok.addr.Hex(),
		"token_name": tok.name, "token_version": tok.version, "token_from_block": from, "pay_to": r["shop"].Hex(), "budget_contract": budget.Hex(),
		"policy_ttl": policyTTL, "zkbudget": o.zkbudget, "zk_setup_dir": filepath.Join(root, "zk/setup"), "relayer_socket": sock,
		"shop_key_dir": o.dir, "shop_daily_tx_cap": shopCap, "dir": host,
		"delegations": map[string]string{"consumer-a": "dlg-ta@1", "consumer-b": "dlg-tb@1"}} // new IDs: /opt's signer.db may know dlg-a/dlg-b (P2b-D5)
	rel := map[string]any{"socket": sock, "callers": []int{os.Getuid()}, "rpc": o.rpc, "chain_id": chainID, "budget_contract": budget.Hex(),
		"confirmations": confirmations, "policy_ttl": policyTTL, "zkbudget": o.zkbudget, "zk_setup_dir": filepath.Join(root, "zk/setup"), "daily_tx_cap": relayerCap}
	for path, v := range map[string]any{filepath.Join(o.dir, "relayer.json"): rel, filepath.Join(host, "payments.json"): pay,
		filepath.Join(host, "registry.json"): reg, filepath.Join(host, "profile.json"): prof} {
		if err := writeJSON(path, v); err != nil {
			return err
		}
	}
	fmt.Fprintf(out, "p2-testnet: wrote %s and %s/{payments,registry,profile}.json; the reconciliation starts at block %d\n", filepath.Join(o.dir, "relayer.json"), host, from)
	fmt.Fprintf(out, "p2-testnet: next: apa-relayer serve --dir %s, then make present-testnet SIGNER=<the Signer's socket>\n", o.dir)
	return nil
}

func readJSON(path string, v any) error {
	raw, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	return json.Unmarshal(raw, v)
}

// writeJSON writes v to path, this user's only.
func writeJSON(path string, v any) error {
	raw, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		return err
	}
	f, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_TRUNC|syscall.O_NOFOLLOW, 0o600)
	if err != nil {
		return err
	}
	if _, err := f.Write(append(raw, '\n')); err != nil {
		f.Close()
		return err
	}
	return errors.Join(f.Sync(), f.Close())
}
