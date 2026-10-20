// Command p2-testnet prepares the P2 payment loop on the Base Sepolia
// testnet (docs/commercial-p2b.md B4; its §6 steps 1 and 4, each run only
// once Aaron approves it).
//
//	p2-testnet preflight  --dir DIR --wallet-a 0x... --wallet-b 0x... [--rpc URL] [--test402 URL]
//	p2-testnet deploy     --dir DIR --wallet-a 0x... --wallet-b 0x... [--rpc URL]
//	p2-testnet test402    URL [--save FILE]
//	p2-testnet opt-signer --dir DIR --base SIGNER.JSON [--voice SIGNER.JSON] --out NEW.JSON
//
// opt-signer (B6, §6 step 5) writes /opt's new signer.json from its current
// one (--base: the socket, callers, approvers and webauthn are kept; A2's,
// before P2's approver caps, is read too) and the voice section (--base's,
// or --voice's: V1's staged signer.json; a config without one is refused:
// voice is the Signer's only check of an approval's payee)
// and deploy's DIR/host files: the two testnet wallets, Circle's USDC, the
// delegations dlg-ta and dlg-tb (and dlg-t402, when deploy wrote test402's line, for exactly one test402
// payment), the approvers' caps (P2b-D5), the voice merchant at the shop's
// address and the reservation section. It reads no key and reaches no node.
//
// DIR is apa-relayer's directory: DIR/keys holds the relayer-gas, issuer,
// policy and shop key files (apa-relayer keygen). Their addresses are read
// from the files, which hold them in the clear; no key is decrypted except
// the issuer's, by deploy, which sends from it (the issuer is the deployer,
// P2b-D3), with a passphrase asked on this terminal.
//
// test402 asks test402's resource once (one GET, no redirect, no key, no
// payment) and prints its PAYMENT-REQUIRED: §6 step 1's part B5 needs.
// --save keeps the header as received (an Offer file, B5-D2): the Host's
// trusted quote for test402, never re-read from a 402 it happens to get.
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
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"slices"
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

// beforeSend runs just before a transaction is sent (tests only; nil otherwise).
var beforeSend func(hash string)

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
	if len(args) > 0 && args[0] == "opt-signer" {
		return optSigner(args[1:], out)
	}
	if len(args) > 0 && args[0] == "test402" {
		save := ""
		switch {
		case len(args) == 4 && args[2] == "--save":
			save = args[3]
			if _, err := os.Lstat(save); !errors.Is(err, os.ErrNotExist) {
				return fmt.Errorf("%s exists; nothing was asked", save)
			}
		case len(args) != 2:
			return errors.New("usage: p2-testnet test402 URL [--save FILE]")
		}
		tok, _ := eth.ParseAddress(circleUSDC)
		return test402(ctx, args[1], tok, out, save)
	}
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
			if err := test402(ctx, o.test402, tok.addr, out, ""); err != nil {
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
	fmt.Fprintf(out, "p2-testnet: chain %d at %s (%q), block %d\n", chainID, u.Host, client, block) // the host only: a URL may carry a key
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
func test402(ctx context.Context, resource string, tok eth.Address, out io.Writer, save string) error {
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
	header := res.Header.Get(x402.HeaderPaymentRequired)
	if err := describe402(u.Host+u.Path, header, tok, out); err != nil || save == "" {
		return err
	}
	f := Offer{ResourceURL: resource, PaymentRequired: header, FetchedAt: time.Now().UTC().Format(time.RFC3339)}
	raw, _ := json.MarshalIndent(f, "", "  ")
	w, err := os.OpenFile(save, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
	if err != nil {
		return err
	}
	if _, err := w.Write(append(raw, '\n')); err != nil {
		w.Close()
		return err
	}
	if err := w.Close(); err != nil {
		return err
	}
	fmt.Fprintf(out, "p2-testnet: saved the PAYMENT-REQUIRED as received in %s\n", save)
	return nil
}

// Offer is a saved external 402 (B5-D2): the resource asked and its
// PAYMENT-REQUIRED header exactly as received.
type Offer struct {
	ResourceURL     string `json:"resource_url"`
	PaymentRequired string `json:"payment_required"`
	FetchedAt       string `json:"fetched_at"`
}

// describe402 prints a PAYMENT-REQUIRED's accepted payments, each read
// leniently (a server may add members), every remote string quoted.
func describe402(where, header string, tok eth.Address, out io.Writer) error {
	shown := header
	if len(shown) > 600 {
		shown = shown[:600]
	}
	raw, err := base64.StdEncoding.DecodeString(header)
	var pr struct {
		X402Version json.RawMessage   `json:"x402Version"`
		Accepts     []json.RawMessage `json:"accepts"`
	}
	if err != nil || json.Unmarshal(raw, &pr) != nil || len(pr.Accepts) == 0 {
		return fmt.Errorf("test402: a PAYMENT-REQUIRED this cannot read (kept for the record): %q", shown)
	}
	for _, raw := range pr.Accepts {
		var a struct {
			Scheme, Network, Amount, Asset, PayTo string
			MaxTimeoutSeconds                     json.RawMessage
			Extra                                 json.RawMessage
		}
		if err := json.Unmarshal(raw, &a); err != nil {
			return fmt.Errorf("test402: an accepted payment this cannot read: %q", raw)
		}
		fmt.Fprintf(out, "p2-testnet: test402 %q: x402 %s, %q on %q, amount %q, asset %q, payTo %q, timeout %s, extra %q\n",
			where, pr.X402Version, a.Scheme, a.Network, a.Amount, a.Asset, a.PayTo, a.MaxTimeoutSeconds, a.Extra)
		if as, _ := eth.ParseAddress(a.Asset); a.Network != "eip155:84532" || as != tok {
			fmt.Fprintln(out, "p2-testnet: test402: this one is not Base Sepolia USDC")
		}
	}
	return nil
}

// journal is DIR/deploy.json: every deploy transaction, written before it
// is sent, then its contract and block once it is confirmed.
type journal struct {
	Chain uint64      `json:"chain_id"`
	From  string      `json:"from"`
	Txs   []journalTx `json:"txs"`
}

type journalTx struct {
	Hash     string `json:"hash"`
	Contract string `json:"contract,omitempty"`
	Block    uint64 `json:"block,omitempty"`
}

// deploy sends the verifier and the budget contract from the issuer, then
// writes the files apa-relayer and the Host start from. Everything that
// can be checked is checked before "yes"; a journal with both contracts
// confirmed (the files not yet written) is finished without sending.
func deploy(ctx context.Context, rpc *eth.Client, tok token, r roles, bal []balance, o options, ask asker, out io.Writer) error {
	jpath := filepath.Join(o.dir, "deploy.json")
	host, run := filepath.Join(o.dir, "host"), filepath.Join(o.dir, "run")
	for _, p := range []string{filepath.Join(o.dir, "relayer.json"), host, run} {
		if _, err := os.Lstat(p); !errors.Is(err, os.ErrNotExist) {
			return fmt.Errorf("%s exists: deploy has run (or apa-relayer runs); nothing was sent", p)
		}
	}
	files, err := prepare(o, tok, r)
	if err != nil {
		return err
	}
	var j journal
	if _, err := os.Lstat(jpath); !errors.Is(err, os.ErrNotExist) {
		return resume(ctx, rpc, jpath, r, files, out)
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
	gv, gb = gv+gv/5, gb+gb/5 // the limits sent
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
	// The node checks limit x fee cap, plus Base's L1 data fee, against the
	// balance for each: twice the limits' cost covers that data fee here.
	most := new(big.Int).Mul(new(big.Int).Mul(fee, new(big.Int).SetUint64(gv+gb)), big.NewInt(2))
	var have *big.Int
	for _, b := range bal {
		if b.who == "issuer" {
			have = b.eth
		}
	}
	fmt.Fprintf(out, "p2-testnet: deploy plan on chain %d, from the issuer %s:\n", chainID, from.Hex())
	fmt.Fprintf(out, "p2-testnet:   1. the Z2 verifier (nonce %d, gas limit %d)\n", nonce, gv)
	fmt.Fprintf(out, "p2-testnet:   2. the budget contract, issuer %s, policy signer %s (nonce %d, gas limit %d)\n", r["issuer"].Hex(), r["policy"].Hex(), nonce+1, gb)
	fmt.Fprintf(out, "p2-testnet:   at most %s test ETH in all, with room for the L1 data fee (fee cap %s wei); the issuer holds %s\n", ether(most), fee, ether(have))
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
	// Fees may have moved while the questions were answered: a cap under
	// 1.5x the base fee now could leave the verifier stuck in a pool.
	if now, err := rpc.LatestHeader(ctx); err != nil {
		return err
	} else if new(big.Int).Mul(fee, big.NewInt(2)).Cmp(new(big.Int).Mul(now.BaseFee, big.NewInt(3))) < 0 {
		return fmt.Errorf("the base fee rose to %s wei since the plan (fee cap %s); nothing was sent: run deploy again for a new plan", now.BaseFee, fee)
	}
	j = journal{Chain: chainID, From: from.Hex()}
	send := func(n uint64, data []byte, gas uint64) (*eth.Receipt, error) {
		tx := &eth.Tx{ChainID: big.NewInt(chainID), Nonce: n, MaxPriorityFeePerGas: tip, MaxFeePerGas: fee, Gas: gas, Value: new(big.Int), Data: data}
		raw, hash, err := tx.Sign(key)
		if err != nil {
			return nil, err
		}
		j.Txs = append(j.Txs, journalTx{Hash: "0x" + hex.EncodeToString(hash)})
		if err := writeJSON(jpath, j); err != nil { // written before it is sent: deploy never sends it twice
			return nil, err
		}
		fmt.Fprintf(out, "p2-testnet: sending %s\n", j.Txs[len(j.Txs)-1].Hash)
		if beforeSend != nil {
			beforeSend(j.Txs[len(j.Txs)-1].Hash)
		}
		if _, err := rpc.SendRaw(ctx, raw); err != nil {
			return nil, fmt.Errorf("sending %s: %w (unknown: look it up before anything else; deploy will not send again)", j.Txs[len(j.Txs)-1].Hash, err)
		}
		rc, err := confirmed(ctx, rpc, hash)
		if err != nil {
			return nil, err
		}
		if rc.ContractAddress == nil {
			return nil, fmt.Errorf("%s: the receipt has no contract address", j.Txs[len(j.Txs)-1].Hash)
		}
		last := &j.Txs[len(j.Txs)-1]
		last.Contract, last.Block = rc.ContractAddress.Hex(), rc.BlockNumber
		return rc, writeJSON(jpath, j)
	}
	vr, err := send(nonce, vcode, gv)
	if err != nil {
		return err
	}
	if _, err := send(nonce+1, ctor(*vr.ContractAddress), gb); err != nil {
		return err
	}
	return finish(ctx, rpc, j, r, files, out)
}

// resume finishes a deploy whose two transactions the journal holds
// confirmed: their receipts are read again and nothing is sent.
func resume(ctx context.Context, rpc *eth.Client, jpath string, r roles, files hostFiles, out io.Writer) error {
	var j journal
	if err := readJSON(jpath, &j); err != nil {
		return err
	}
	if j.Chain != chainID || j.From != r["issuer"].Hex() || len(j.Txs) != 2 || j.Txs[0].Contract == "" || j.Txs[1].Contract == "" {
		return fmt.Errorf("%s holds a deploy that did not finish: look its transactions up before anything else; nothing was sent", jpath)
	}
	for _, t := range j.Txs {
		h, _ := hex.DecodeString(strings.TrimPrefix(t.Hash, "0x"))
		rc, err := confirmed(ctx, rpc, h)
		if err != nil {
			return err
		}
		if rc.ContractAddress == nil || rc.ContractAddress.Hex() != t.Contract {
			return fmt.Errorf("%s: its receipt is not the journal's contract", t.Hash)
		}
	}
	fmt.Fprintln(out, "p2-testnet: the journal's deploy is confirmed; writing its files (nothing is sent)")
	return finish(ctx, rpc, j, r, files, out)
}

// finish checks the deployed budget contract, then writes the files.
func finish(ctx context.Context, rpc *eth.Client, j journal, r roles, files hostFiles, out io.Writer) error {
	verifier, _ := eth.ParseAddress(j.Txs[0].Contract)
	budget, _ := eth.ParseAddress(j.Txs[1].Contract)
	// The deployed contract names the verifier, issuer and policy signer
	// planned; a node behind on the newest block is asked again.
	for sig, want := range map[string]eth.Address{"verifier()": verifier, "issuer()": r["issuer"], "policySigner()": r["policy"]} {
		var got eth.Address
		for i := 0; i < 10 && got != want; i++ {
			if i > 0 {
				time.Sleep(confirmPoll)
			}
			if b, err := rpc.Call(ctx, eth.Msg{To: budget, Data: eth.Selector(sig)}); err == nil {
				if w, err := eth.Word(b, 0); err == nil {
					got, _ = eth.WordAddress(w)
				}
			}
		}
		if got != want {
			return fmt.Errorf("the budget contract's %s is not %s (ask again: deploy resumes from its journal)", sig, want.Hex())
		}
	}
	fmt.Fprintf(out, "p2-testnet: verifier %s (block %d, %s), budget %s (block %d, %s)\n", verifier.Hex(), j.Txs[0].Block, j.Txs[0].Hash, budget.Hex(), j.Txs[1].Block, j.Txs[1].Hash)
	return files.write(budget, j.Txs[0].Block, out)
}

// hostFiles are the files deploy writes, all but the deployment checked
// before anything is sent.
type hostFiles struct {
	o    options
	tok  token
	shop eth.Address
	reg  contracts.RegistryConfig
	prof map[string]any
	zk   string
	// t402, t402Offer: test402's payee and its saved 402 (DIR/test402-offer.json,
	// test402 --save; B5), when that file is there.
	t402      eth.Address
	t402Offer string
}

// prepare builds and checks the registry and profile, and the paths the
// written files name, before anything is sent.
func prepare(o options, tok token, r roles) (hostFiles, error) {
	f := hostFiles{o: o, tok: tok, shop: r["shop"]}
	root, _ := filepath.Abs(".")
	f.zk = filepath.Join(root, "zk/setup")
	for _, p := range []string{o.zkbudget, filepath.Join(f.zk, "pk.bin"), filepath.Join(f.zk, "vk.bin")} {
		if fi, err := os.Stat(p); err != nil || !fi.Mode().IsRegular() {
			return f, fmt.Errorf("%s is missing (run from the repo; build zkbudget first); nothing was sent", p)
		}
	}
	if err := readJSON(o.registry, &f.reg); err != nil {
		return f, err
	}
	var ia, ib = -1, -1
	for i, w := range f.reg.Wallets {
		switch w.WalletID {
		case "wallet-a":
			ia = i
		case "wallet-b":
			ib = i
		}
	}
	if ia < 0 || ib < 0 || len(f.reg.Assets) != 1 {
		return f, errors.New("the registry has no wallet-a, wallet-b or one asset")
	}
	f.reg.Wallets[ia].From, f.reg.Wallets[ib].From = o.walletA.Hex(), o.walletB.Hex()
	a := &f.reg.Assets[0]
	a.AssetID, a.AssetContract, a.EIP712Name, a.EIP712Version = assetID, tok.addr.Hex(), tok.name, tok.version
	f.reg.MerchantPayTo = f.shop.Hex()
	if _, err := contracts.NewRegistry(f.reg); err != nil {
		return f, fmt.Errorf("the registry to write: %w", err)
	}
	if err := readJSON(o.profile, &f.prof); err != nil {
		return f, err
	}
	merchants := []string{f.shop.Hex()}
	if p := filepath.Join(o.dir, "test402-offer.json"); fileExists(p) {
		var off Offer
		var pr x402.PaymentRequired
		var req struct{ PayTo, Asset string }
		if err := readJSON(p, &off); err != nil || x402.Decode(off.PaymentRequired, &pr) != nil || len(pr.Accepts) != 1 || json.Unmarshal(pr.Accepts[0], &req) != nil {
			return f, fmt.Errorf("%s is not test402's saved 402", p)
		}
		a, err := eth.ParseAddress(strings.ToLower(req.PayTo))
		if as, _ := eth.ParseAddress(strings.ToLower(req.Asset)); err != nil || as != tok.addr || a == f.shop {
			return f, fmt.Errorf("%s: its payTo or asset is not test402's Circle USDC payment", p)
		}
		f.t402, f.t402Offer = a, p
		merchants = append(merchants, a.Hex()) // the policy allows test402 as a payee too
	}
	f.prof["asset_id"], f.prof["merchants"] = assetID, merchants
	raw, _ := json.Marshal(f.prof)
	if _, err := policy.LoadProfile(raw); err != nil {
		return f, fmt.Errorf("the profile to write: %w", err)
	}
	return f, nil
}

// delegations are the Host's lines consumer/payee -> Signer delegation.
func (f hostFiles) delegations() map[string]string {
	d := map[string]string{"consumer-a/" + f.shop.Hex(): "dlg-ta@1", "consumer-b/" + f.shop.Hex(): "dlg-tb@1"}
	if f.t402Offer != "" {
		d["consumer-b/"+f.t402.Hex()] = "dlg-t402@1" // exactly one 0.0001 payment, wallet-b (P2b-D5, D7)
	}
	return d
}

func fileExists(p string) bool { _, err := os.Lstat(p); return err == nil }

// write writes apa-relayer's relayer.json and the Host's files.
func (f hostFiles) write(budget eth.Address, from uint64, out io.Writer) error {
	o := f.o
	host, run := filepath.Join(o.dir, "host"), filepath.Join(o.dir, "run")
	for _, d := range []string{host, run} {
		if err := os.Mkdir(d, 0o700); err != nil {
			return err
		}
	}
	sock := filepath.Join(run, "relayer.sock")
	pay := map[string]any{"environment": "base_sepolia", "rpc": o.rpc, "chain_id": chainID, "confirmations": confirmations, "token": f.tok.addr.Hex(),
		"token_name": f.tok.name, "token_version": f.tok.version, "token_from_block": from, "pay_to": f.shop.Hex(), "budget_contract": budget.Hex(),
		"policy_ttl": policyTTL, "zkbudget": o.zkbudget, "zk_setup_dir": f.zk, "relayer_socket": sock,
		"shop_key_dir": o.dir, "shop_daily_tx_cap": shopCap, "dir": host,
		// By consumer and payee (B5): a purchase from a payee no line names starts
		// no payment. New IDs: /opt's signer.db may know dlg-a/dlg-b (P2b-D5).
		"delegations": f.delegations()}
	if f.t402Offer != "" {
		pay["test402_offer"] = f.t402Offer
	}
	rel := map[string]any{"socket": sock, "callers": []int{os.Getuid()}, "rpc": o.rpc, "chain_id": chainID, "budget_contract": budget.Hex(),
		"confirmations": confirmations, "policy_ttl": policyTTL, "zkbudget": o.zkbudget, "zk_setup_dir": f.zk, "daily_tx_cap": relayerCap}
	for path, v := range map[string]any{filepath.Join(o.dir, "relayer.json"): rel, filepath.Join(host, "payments.json"): pay,
		filepath.Join(host, "registry.json"): f.reg, filepath.Join(host, "profile.json"): f.prof} {
		if err := writeJSON(path, v); err != nil {
			return err
		}
	}
	fmt.Fprintf(out, "p2-testnet: wrote %s and %s/{payments,registry,profile}.json; the reconciliation starts at block %d\n", filepath.Join(o.dir, "relayer.json"), host, from)
	fmt.Fprintf(out, "p2-testnet: next: apa-relayer serve --dir %s, then make present-testnet SIGNER=<the Signer's socket>\n", o.dir)
	return nil
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
	return nil, fmt.Errorf("0x%x is not %d blocks deep after %s: unknown; look it up before anything else (deploy will not send again)", hash, confirmations, confirmWait)
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
	// A new file renamed into place: a crash never leaves a half-written journal.
	tmp := path + ".tmp"
	f, err := os.OpenFile(tmp, os.O_WRONLY|os.O_CREATE|os.O_TRUNC|syscall.O_NOFOLLOW, 0o600)
	if err != nil {
		return err
	}
	if _, err := f.Write(append(raw, '\n')); err != nil {
		f.Close()
		return err
	}
	if err := errors.Join(f.Sync(), f.Close()); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}

// The testnet Signer's caps (P2b-D5), in USDC units.
const (
	dlgPerTx, dlgTotal       = "100000", "300000"   // 0.10 a payment, 0.30 in all
	approverMax, approverSum = "1000000", "1500000" // 1.00 a payment, 1.50 in 24 hours
	t402Units                = "100"                // exactly one 0.0001 payment
	hostOrigin               = "http://localhost:8402"
)

// optSigner writes /opt's signer.json for the testnet (B6).
func optSigner(args []string, out io.Writer) error {
	fs := flag.NewFlagSet("opt-signer", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	dir := fs.String("dir", "", "p2-testnet's directory (DIR/host: deploy wrote it)")
	base := fs.String("base", "", "/opt's current signer.json (a copy)")
	dst := fs.String("out", "", "the new signer.json (must not exist)")
	voiceFrom := fs.String("voice", "", "a signer.json whose voice section is used (V1's staged one), when --base has none")
	if err := fs.Parse(args); err != nil || fs.NArg() != 0 || *dir == "" || *base == "" || *dst == "" {
		return errors.New("usage: p2-testnet opt-signer --dir DIR --base SIGNER.JSON [--voice SIGNER.JSON] --out NEW.JSON")
	}
	if _, err := os.Lstat(*dst); !errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("%s exists; nothing was written", *dst)
	}
	// Read strictly but not checked yet: /opt's may be A2's, without the
	// approver caps this fills in; the result is checked whole below.
	strict := func(path string) (*signer.Config, error) {
		raw, err := os.ReadFile(path)
		if err != nil {
			return nil, err
		}
		var c signer.Config
		d := json.NewDecoder(bytes.NewReader(raw))
		d.DisallowUnknownFields()
		if err := d.Decode(&c); err != nil || d.More() {
			return nil, fmt.Errorf("%s: not a signer configuration", path)
		}
		return &c, nil
	}
	cfg, err := strict(*base)
	if err != nil {
		return err
	}
	if *voiceFrom != "" {
		if cfg.Voice != nil {
			return errors.New("--base has a voice section already: no --voice")
		}
		v, err := strict(*voiceFrom)
		if err != nil {
			return err
		}
		cfg.Voice = v.Voice
	}
	if cfg.Voice == nil || len(cfg.Voice.Merchants) != 1 {
		return errors.New("the voice section (--base's or --voice's) with exactly one merchant, the shop, is needed: without it an approval's payee is not checked")
	}
	if home := filepath.Dir(filepath.Dir(cfg.Socket)); !strings.HasPrefix(cfg.Voice.Socket, home+"/") {
		return fmt.Errorf("the voice socket %q is not under the Signer's own %s (another machine's or the repo's voice section?)", cfg.Voice.Socket, home)
	}
	var pay struct {
		RPC            string            `json:"rpc"`
		ChainID        uint64            `json:"chain_id"`
		Confirmations  uint64            `json:"confirmations"`
		Token          string            `json:"token"`
		TokenName      string            `json:"token_name"`
		TokenVersion   string            `json:"token_version"`
		PayTo          string            `json:"pay_to"`
		BudgetContract string            `json:"budget_contract"`
		Environment    string            `json:"environment"`
		Delegations    map[string]string `json:"delegations"`
	}
	var reg contracts.RegistryConfig
	host := filepath.Join(*dir, "host")
	if err := errors.Join(readJSON(filepath.Join(host, "payments.json"), &pay), readJSON(filepath.Join(host, "registry.json"), &reg)); err != nil {
		return fmt.Errorf("deploy's files: %w", err)
	}
	shopKey := strings.ToLower(pay.PayTo)
	var t402 string // test402's payee, when deploy wrote its line (B5)
	for k, v := range pay.Delegations {
		if c, payee, _ := strings.Cut(k, "/"); v == "dlg-t402@1" && c == "consumer-b" && payee != shopKey {
			t402 = payee
		}
	}
	if pay.Environment != "base_sepolia" || pay.ChainID != chainID || pay.Delegations["consumer-a/"+shopKey] != "dlg-ta@1" || pay.Delegations["consumer-b/"+shopKey] != "dlg-tb@1" ||
		len(pay.Delegations) != map[bool]int{true: 3, false: 2}[t402 != ""] ||
		!strings.EqualFold(pay.Token, circleUSDC) && !local || pay.TokenName != "USDC" || !strings.HasPrefix(pay.RPC, "https://") {
		return errors.New("DIR/host/payments.json is not p2-testnet deploy's (base_sepolia, Circle's USDC, an https node)")
	}
	if w := cfg.WebAuthn; w == nil || w.RPID != "localhost" || !slices.Contains(w.Origins, hostOrigin) {
		return fmt.Errorf("--base's webauthn is not rp_id localhost with origin %s (make present-testnet's)", hostOrigin)
	}
	chain := fmt.Sprint(chainID)
	cfg.Wallets = nil
	for _, w := range reg.Wallets {
		if w.WalletID == "wallet-a" || w.WalletID == "wallet-b" {
			cfg.Wallets = append(cfg.Wallets, signer.Wallet{WalletID: w.WalletID, ConsumerID: "consumer-" + w.WalletID[len(w.WalletID)-1:], ChainID: chain, Address: strings.ToLower(w.From)})
		}
	}
	if len(cfg.Wallets) != 2 {
		return errors.New("the registry has no wallet-a and wallet-b")
	}
	cfg.Assets = []signer.Asset{{ChainID: chain, AssetContract: strings.ToLower(pay.Token), EIP712Name: pay.TokenName, EIP712Version: pay.TokenVersion}}
	now := uint64(time.Now().Unix())
	dlg := func(id, x, merchant string, perTx, total string) signer.Delegation {
		return signer.Delegation{DelegationID: id, Version: 1, ConsumerID: "consumer-" + x, WalletID: "wallet-" + x, ChainID: chain, AssetContract: strings.ToLower(pay.Token),
			Merchants: []string{strings.ToLower(merchant)}, PerTxMax: contracts.Amount(perTx), TotalCap: contracts.Amount(total), ValidFrom: now - 60, ValidUntil: now + 7*86400}
	}
	cfg.Delegations = []signer.Delegation{dlg("dlg-ta", "a", pay.PayTo, dlgPerTx, dlgTotal), dlg("dlg-tb", "b", pay.PayTo, dlgPerTx, dlgTotal)}
	if t402 != "" {
		a, err := eth.ParseAddress(t402)
		if err != nil {
			return fmt.Errorf("payments.json's test402 payee: %w", err)
		}
		cfg.Delegations = append(cfg.Delegations, dlg("dlg-t402", "b", a.Hex(), t402Units, t402Units))
	}
	for i := range cfg.Approvers {
		a := &cfg.Approvers[i]
		a.SignerMax, a.SignerTotal, a.SignerWindowSeconds = approverMax, approverSum, 86400
		a.Version++ // the entry changed: a challenge made under the old one is refused (APPROVER_CHANGED)
	}
	v := cfg.Voice
	v.Merchants[0].PayTo = strings.ToLower(pay.PayTo)     // the shop's spoken name, now at its testnet address
	v.Decimals = 6                                        // Circle's USDC (deploy read it)
	cfg.MaxWindowSeconds = max(cfg.MaxWindowSeconds, 600) // the settling shop's 300 s window fits
	cfg.Reservation = &signer.Reservation{RPC: pay.RPC, ChainID: chain, BudgetContract: strings.ToLower(pay.BudgetContract), Confirmations: pay.Confirmations}
	b, err := json.MarshalIndent(cfg, "", " ")
	if err != nil {
		return err
	}
	if _, err := signer.ParseConfig(b); err != nil {
		return fmt.Errorf("the new signer.json: %w", err)
	}
	f, err := os.OpenFile(*dst, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
	if err != nil {
		return err
	}
	if _, err := f.Write(append(b, '\n')); err != nil {
		f.Close()
		return err
	}
	if err := errors.Join(f.Sync(), f.Close()); err != nil {
		return err
	}
	sum := sha256.Sum256(append(b, '\n'))
	fmt.Fprintf(out, "p2-testnet: wrote %s (SHA-256 %x): wallets %s and %s, delegations %d, approvers %d, reservation on chain %s at %s; voice socket %q, models %q and %q\n",
		*dst, sum, cfg.Wallets[0].Address, cfg.Wallets[1].Address, len(cfg.Delegations), len(cfg.Approvers), chain, cfg.Reservation.BudgetContract, v.Socket, v.ASRModel, v.SpeakerModel)
	return nil
}
