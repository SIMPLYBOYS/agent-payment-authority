package main

// P2 payments in the demo (docs/commercial-p2-host.md 6, 12.3;
// docs/commercial-p2b.md B2, B3): on a local anvil, the budget relayer and
// the settling shop run in this process with anvil's public test keys,
// named by index (H-D5), or apa-relayer and the shop's key file; on Base
// Sepolia always the latter, with daily caps; the
// Signer is the one on --approvals-signer's socket (payments and approvals,
// P2-D1); the worker pays one payment at a time (H-D1 A), and before each
// round the starter registers new tasks' budgets (H-D4 A) and starts the
// payments of purchases decided for payment.

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"time"

	"github.com/SIMPLYBOYS/agent-payment-authority/internal/budget"
	"github.com/SIMPLYBOYS/agent-payment-authority/internal/eth"
	"github.com/SIMPLYBOYS/agent-payment-authority/internal/keytool"
	"github.com/SIMPLYBOYS/agent-payment-authority/internal/merchant"
	"github.com/SIMPLYBOYS/agent-payment-authority/internal/payflow"
	"github.com/SIMPLYBOYS/agent-payment-authority/internal/reconcile"
	"github.com/SIMPLYBOYS/agent-payment-authority/internal/signerclient"
	"github.com/SIMPLYBOYS/agent-payment-authority/internal/store"
	"github.com/SIMPLYBOYS/agent-payment-authority/internal/x402"
)

// payConfig is --payments' file: the chain's deployment and where the
// relayer's and shop's databases live. It holds no key.
type payConfig struct {
	// Environment: local_anvil (the default; P2-a) or base_sepolia
	// (docs/commercial-p2b.md B3): the Base Sepolia testnet, which takes
	// no anvil key, apa-relayer on relayer_socket, the shop's key file in
	// shop_key_dir and the caps.
	Environment    string `json:"environment,omitempty"`
	RPC            string `json:"rpc"`
	ChainID        uint64 `json:"chain_id"`
	Confirmations  uint64 `json:"confirmations"`
	Token          string `json:"token"`
	TokenName      string `json:"token_name"`
	TokenVersion   string `json:"token_version"`
	TokenFromBlock uint64 `json:"token_from_block"`
	PayTo          string `json:"pay_to"`
	BudgetContract string `json:"budget_contract"`
	PolicyTTL      uint64 `json:"policy_ttl"`
	Zkbudget       string `json:"zkbudget"`
	ZkSetupDir     string `json:"zk_setup_dir"`
	AnvilKeys      *struct {
		RelayerGas int `json:"relayer_gas"`
		Issuer     int `json:"issuer"`
		Policy     int `json:"policy"`
		ShopGas    int `json:"shop_gas"`
	} `json:"anvil_keys,omitempty"`
	// RelayerSocket: apa-relayer's socket (docs/commercial-p2b.md B2); the
	// relayer then runs there with its own keys and database.
	RelayerSocket string `json:"relayer_socket,omitempty"`
	// ShopKeyDir: the shop's key file DIR/keys/shop.key (apa-relayer
	// keygen --role shop), opened with a passphrase asked at start.
	ShopKeyDir     string `json:"shop_key_dir,omitempty"`
	ShopDailyTxCap uint64 `json:"shop_daily_tx_cap,omitempty"`
	// Test402Offer: test402's 402 as saved (p2-testnet test402 --save;
	// B5): the data shop lists test402 as an outside product with that
	// quote. base_sepolia only.
	Test402Offer string            `json:"test402_offer,omitempty"`
	Dir          string            `json:"dir"`         // relayer.db and settle.db, a private directory
	Delegations  map[string]string `json:"delegations"` // consumer ID -> its Signer delegation, id@version
}

// BaseSepolia is the payment capability on the Base Sepolia testnet
// (docs/commercial-p2b.md B3); LocalAnvil the one on a local anvil.
const BaseSepolia = "base_sepolia"

// baseSepoliaChain is Base Sepolia's chain ID.
const baseSepoliaChain = 84532

// testnetOnLoopback lets a test give base_sepolia an http URL (its stub
// node); never set outside tests. A node answering as anvil is refused anyway.
var testnetOnLoopback = false

// hostRelayer is the budget relayer as the Host uses it: in this process
// (*budget.Relayer) or apa-relayer's socket (*budget.Client).
type hostRelayer interface {
	payflow.Relayer
	RegisterTask(ctx context.Context, taskID, consumerID string, balance uint64) error
}

// payments is the demo's payment side once opened.
type payments struct {
	cfg     payConfig
	rpc     *eth.Client
	relayer hostRelayer
	closeR  func() error
	settler *merchant.Settler
	shopFor time.Duration // the worker's wait for the shop: above the settler's own wait
	out     io.Writer
}

// checkEnvironment is the file's environment rules, before anything is opened.
func checkEnvironment(c *payConfig) error {
	switch c.Environment {
	case "", LocalAnvil:
		c.Environment = LocalAnvil
		if err := eth.LocalRPC(c.RPC); err != nil {
			return err
		}
		if c.AnvilKeys == nil && (c.RelayerSocket == "" || c.ShopKeyDir == "") {
			return errors.New("anvil_keys, unless both relayer_socket and shop_key_dir are given")
		}
		if k := c.AnvilKeys; k != nil && c.RelayerSocket == "" && len(map[int]bool{k.RelayerGas: true, k.Issuer: true, k.Policy: true, k.ShopGas: true}) != 4 {
			return errors.New("the four anvil keys are four different ones (each has its own nonces)")
		}
	case BaseSepolia:
		u, err := url.Parse(c.RPC)
		switch {
		case c.ChainID != baseSepoliaChain:
			return fmt.Errorf("base_sepolia is chain %d", baseSepoliaChain)
		case err != nil || u.User != nil || u.Host == "" || u.Scheme != "https" && !(testnetOnLoopback && u.Scheme == "http"):
			return errors.New("base_sepolia's rpc is an https URL without user info")
		case eth.LocalRPC("http://"+u.Host) == nil && !testnetOnLoopback:
			return errors.New("base_sepolia's rpc is a public node, not this machine")
		case c.AnvilKeys != nil:
			return errors.New("base_sepolia takes no anvil key")
		case c.RelayerSocket == "" || c.ShopKeyDir == "":
			return errors.New("base_sepolia needs relayer_socket (apa-relayer) and shop_key_dir")
		case c.Confirmations < 2 || c.ShopDailyTxCap == 0 || c.TokenFromBlock == 0:
			return errors.New("base_sepolia needs at least 2 confirmations, shop_daily_tx_cap and token_from_block (the deployment's block)")
		}
	default:
		return fmt.Errorf("environment %q is local_anvil or base_sepolia", c.Environment)
	}
	if c.Test402Offer != "" && c.Environment != BaseSepolia {
		return errors.New("test402_offer is base_sepolia's only (test402 is on Base Sepolia)")
	}
	if c.PolicyTTL == 0 || c.PolicyTTL > 600 {
		return errors.New("policy_ttl is 1 to 600 seconds (a reservation's authorization expires within it)")
	}
	return nil
}

// test402Product is test402's 402 as saved (B5-D2) as the data shop's
// outside product: its resource and quote exactly as that 402 said, read as
// the bridge reads a 402 (one offer, its resource the URL asked).
func test402Product(path string) (merchant.External, error) {
	var f struct {
		ResourceURL     string `json:"resource_url"`
		PaymentRequired string `json:"payment_required"`
		FetchedAt       string `json:"fetched_at"`
	}
	if err := decodeFile(path, &f); err != nil {
		return merchant.External{}, err
	}
	u, err := url.Parse(f.ResourceURL)
	if err != nil || u.Scheme != "https" || u.Host != "test402.com" {
		return merchant.External{}, errors.New("not a test402.com https resource")
	}
	var pr x402.PaymentRequired
	if err := x402.Decode(f.PaymentRequired, &pr); err != nil {
		return merchant.External{}, err
	}
	if pr.Resource.URL != f.ResourceURL || len(pr.Accepts) != 1 {
		return merchant.External{}, errors.New("its 402 is not one offer for its resource")
	}
	q, err := pr.Quote(0)
	if err != nil {
		return merchant.External{}, err
	}
	return merchant.External{ID: "test402", Title: "test402.com：x402 相容性檢查（外部商店）",
		Summary:      "外部的 x402 測試資源，付 0.0001 測試 USDC 取得一份 JSON；用來證明與真的 x402 伺服器相容（報價取於 " + f.FetchedAt + "）。",
		AssetDisplay: "0.0001 test USDC", ResourceURL: f.ResourceURL, Quote: q}, nil
}

// checkChain: the RPC is the configured chain, and an anvil exactly when
// the environment is local_anvil (a loopback port may be a tunnel to a
// public node; a testnet must not be a local stand-in).
func checkChain(ctx context.Context, c payConfig, rpc *eth.Client) error {
	if id, err := rpc.ChainID(ctx); err != nil || !id.IsUint64() || id.Uint64() != c.ChainID {
		return fmt.Errorf("the chain at rpc is not chain %d (or did not answer)", c.ChainID)
	}
	anvil := eth.Anvil(ctx, rpc)
	switch {
	case c.Environment == LocalAnvil:
		return anvil
	case anvil == nil:
		return errors.New("base_sepolia's rpc answers as a local anvil")
	}
	return nil
}

// openPayments checks the file and the chain, then opens the relayer and
// the settler; baseURL is the shop's; shopPass asks the shop key's
// passphrase (shop_key_dir).
func openPayments(ctx context.Context, path, baseURL string, clock func() uint64, shopPass func() ([]byte, error), out io.Writer) (*payments, *merchant.Merchant, error) {
	var c payConfig
	if err := decodeFile(path, &c); err != nil {
		return nil, nil, fmt.Errorf("payments: %w", err)
	}
	if err := checkEnvironment(&c); err != nil {
		return nil, nil, fmt.Errorf("payments: %w", err)
	}
	var keys [4]*eth.Key // anvil's: relayer gas, issuer, policy, shop gas
	if k := c.AnvilKeys; k != nil {
		for i, n := range []int{k.RelayerGas, k.Issuer, k.Policy, k.ShopGas} {
			var err error
			if keys[i], err = eth.AnvilKey(n); err != nil {
				return nil, nil, fmt.Errorf("payments: %w", err)
			}
		}
	}
	token, err1 := eth.ParseAddress(c.Token)
	payTo, err2 := eth.ParseAddress(c.PayTo)
	contract, err3 := eth.ParseAddress(c.BudgetContract)
	if err := errors.Join(err1, err2, err3); err != nil || c.Confirmations == 0 || len(c.Delegations) == 0 {
		return nil, nil, errors.New("payments: token, pay_to and budget_contract addresses, confirmations and delegations")
	}
	if fi, err := os.Stat(c.Dir); err != nil || !fi.IsDir() || fi.Mode().Perm()&0o077 != 0 {
		return nil, nil, errors.New("payments: dir is an existing directory private to its owner (0700)")
	}
	var rc *budget.Client // apa-relayer: ready for this chain and budget before anything else
	if c.RelayerSocket != "" {
		if err := keytool.Private(filepath.Dir(c.RelayerSocket)); err != nil { // nobody else can stand in for apa-relayer there
			return nil, nil, fmt.Errorf("payments: relayer_socket's directory: %w", err)
		}
		rc = budget.NewClient(c.RelayerSocket)
		hctx, cancel := context.WithTimeout(ctx, 10*time.Second)
		h, err := rc.Health(hctx)
		cancel()
		if err != nil || h.State != "ready" || h.ChainID != c.ChainID || h.Contract != contract.Hex() || h.Confirmations < c.Confirmations {
			return nil, nil, fmt.Errorf("payments: apa-relayer on %s is not ready for chain %d, budget %s and %d confirmations (%+v %v); nothing was started",
				c.RelayerSocket, c.ChainID, contract.Hex(), c.Confirmations, h, err)
		}
	}
	rpc := eth.NewClient(c.RPC)
	if err := checkChain(ctx, c, rpc); err != nil {
		return nil, nil, fmt.Errorf("payments: %w", err)
	}
	if c.ShopKeyDir != "" {
		pass, err := shopPass()
		if err != nil {
			return nil, nil, fmt.Errorf("payments: the shop key's passphrase: %w", err)
		}
		keys[3], err = keytool.Load(c.ShopKeyDir, keytool.ShopLabel, "shop", pass)
		clear(pass)
		if err != nil {
			return nil, nil, fmt.Errorf("payments: %w", err)
		}
		if keys[3].Addr != payTo {
			return nil, nil, fmt.Errorf("payments: the shop key is %s, not pay_to %s (the shop pays its own gas and is paid; P2b-D3)", keys[3].Addr.Hex(), payTo.Hex())
		}
	}
	var relayer hostRelayer = rc
	closeR := func() error { return nil }
	if rc == nil {
		r, err := budget.Open(ctx, budget.Config{ChainID: c.ChainID, Contract: contract, Confirmations: c.Confirmations, PolicyTTL: c.PolicyTTL,
			Zkbudget: c.Zkbudget, SetupDir: c.ZkSetupDir}, budget.Keys{Gas: keys[0], Issuer: keys[1], Policy: keys[2]}, rpc, filepath.Join(c.Dir, "relayer.db"), clock)
		if err != nil {
			return nil, nil, err
		}
		relayer, closeR = r, r.Close
	}
	sc := merchant.SettleConfig{ChainID: c.ChainID, Token: token, TokenName: c.TokenName, TokenVersion: c.TokenVersion, PayTo: payTo,
		Confirmations: c.Confirmations, DailyTxCap: c.ShopDailyTxCap}
	shopFor := 30 * time.Second       // above the settler's default 20 s
	if c.Environment == BaseSepolia { // public blocks, a public node: ask less often, wait longer
		sc.Poll, sc.Wait = time.Second, 2*time.Minute
		shopFor = sc.Wait + 15*time.Second // the worker waits out the settler's wait, not the other way
	}
	settler, err := merchant.OpenSettler(ctx, sc, keys[3], rpc, filepath.Join(c.Dir, "settle.db"), clock)
	if err != nil {
		closeR()
		return nil, nil, err
	}
	var ext []merchant.External
	if c.Test402Offer != "" {
		x, err := test402Product(c.Test402Offer)
		if err != nil {
			closeR()
			settler.Close()
			return nil, nil, fmt.Errorf("payments: test402_offer: %w", err)
		}
		ext = append(ext, x)
	}
	shop, err := merchant.NewSettled(baseURL, settler, ext...)
	if err != nil {
		closeR()
		settler.Close()
		return nil, nil, err
	}
	return &payments{cfg: c, rpc: rpc, relayer: relayer, closeR: closeR, settler: settler, shopFor: shopFor, out: out}, shop, nil
}

func (p *payments) Close() error { return errors.Join(p.closeR(), p.settler.Close()) }

// paymentSigner: with --payments the Signer must sign payments (health
// ready: apa-signer serve with a reservation section). A Signer for
// approvals only would refuse each payment after its tx#1 was spent.
func paymentSigner(socket string) error {
	ans := signerclient.NewApprovals(socket).Health()
	if ans.Kind != signerclient.KindOK || ans.Reply.State != "ready" {
		return fmt.Errorf("--payments needs a Signer that signs payments (apa-signer serve with a reservation section) on %s; its health is %s %s; nothing was started",
			socket, ans.Kind, ans.Reply.State)
	}
	return nil
}

// errReconcileStore: the recorded authorizations could not be read (not the chain).
var errReconcileStore = errors.New("RECONCILE_STORE")

// reconciliation is the Host's reconciliation on this chain
// (docs/commercial-p2-host.md 7; AE1 acceptance 18): every confirmed
// transfer out of the payer wallets against every authorization the Store
// recorded as signed.
func (p *payments) reconciliation(ctx context.Context, st *store.Store, wallets []eth.Address, now uint64) (reconcile.Report, error) {
	known, err := payflow.Known(st)
	if err != nil {
		return reconcile.Report{}, fmt.Errorf("%w: %w", errReconcileStore, err)
	}
	token, _ := eth.ParseAddress(p.cfg.Token)
	t := reconcile.Token{RPC: p.rpc, Address: token, Confirmations: p.cfg.Confirmations, FromBlock: p.cfg.TokenFromBlock}
	return t.Report(ctx, p.cfg.Environment, p.cfg.ChainID, wallets, known, now) // checkChain: local_anvil is an anvil, base_sepolia is not
}

// logged is a payment error as the terminal says it: its code, or (a
// shop's refusal reason, say) its text, quoted and bounded: it may come
// from the shop's JSON.
func logged(err error) string {
	if store.Code(err) != "" {
		return store.CodeOf(err)
	}
	s := err.Error()
	if len(s) > 120 {
		s = s[:120]
	}
	return strconv.Quote(s)
}

// payMargin: no new step this close to valid_before; more than the shop's
// own 5 s plus signing.
const payMargin = 30

// run is the worker's loop until ctx ends, the Store being open.
func (p *payments) run(ctx context.Context, st *store.Store, socket string, clock func() uint64) {
	token, _ := eth.ParseAddress(p.cfg.Token)
	starter := &payflow.Starter{Store: st, Register: p.relayer.RegisterTask, Delegations: p.cfg.Delegations,
		Log: func(what string, err error) { fmt.Fprintln(p.out, "demo: payment", what, store.CodeOf(err)) }}
	w := &payflow.Worker{Store: st, Relayer: p.relayer, Signer: signerclient.New(socket, st),
		Shop:  payflow.HTTPShop{Client: &http.Client{Timeout: p.shopFor}},
		Chain: reconcile.Token{RPC: p.rpc, Address: token, Confirmations: p.cfg.Confirmations, FromBlock: p.cfg.TokenFromBlock},
		Clock: clock, Margin: payMargin, Before: starter.Start,
		Log: func(intent string, err error) { fmt.Fprintln(p.out, "demo: payment", intent, logged(err)) }}
	for { // Run returns only when its recovery failed (or ctx ended): try again
		err := w.Run(ctx)
		if err == nil || ctx.Err() != nil {
			return
		}
		fmt.Fprintln(p.out, "demo: payments: recovery", store.CodeOf(err))
		select {
		case <-ctx.Done():
			return
		case <-time.After(5 * time.Second):
		}
	}
}
