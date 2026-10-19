package main

// P2-a payments in the demo (docs/commercial-p2-host.md 6, 12.3): on a
// local anvil only, the budget relayer and the settling shop run in this
// process with anvil's public test keys, named by index (H-D5); the
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
	"os"
	"path/filepath"
	"time"

	"github.com/SIMPLYBOYS/agent-payment-authority/internal/budget"
	"github.com/SIMPLYBOYS/agent-payment-authority/internal/eth"
	"github.com/SIMPLYBOYS/agent-payment-authority/internal/merchant"
	"github.com/SIMPLYBOYS/agent-payment-authority/internal/payflow"
	"github.com/SIMPLYBOYS/agent-payment-authority/internal/reconcile"
	"github.com/SIMPLYBOYS/agent-payment-authority/internal/signerclient"
	"github.com/SIMPLYBOYS/agent-payment-authority/internal/store"
)

// payConfig is --payments' file: the local chain's deployment and where
// the relayer's and shop's databases live. It holds no key.
type payConfig struct {
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
	AnvilKeys      struct {
		RelayerGas int `json:"relayer_gas"`
		Issuer     int `json:"issuer"`
		Policy     int `json:"policy"`
		ShopGas    int `json:"shop_gas"`
	} `json:"anvil_keys"`
	Dir         string            `json:"dir"`         // relayer.db and settle.db, a private directory
	Delegations map[string]string `json:"delegations"` // consumer ID -> its Signer delegation, id@version
}

// payments is the demo's payment side once opened.
type payments struct {
	cfg     payConfig
	rpc     *eth.Client
	relayer *budget.Relayer
	settler *merchant.Settler
	out     io.Writer
}

// openPayments checks the file and the chain, then opens the relayer and the
// settler; baseURL is the shop's.
func openPayments(ctx context.Context, path, baseURL string, clock func() uint64, out io.Writer) (*payments, *merchant.Merchant, error) {
	var c payConfig
	if err := decodeFile(path, &c); err != nil {
		return nil, nil, fmt.Errorf("payments: %w", err)
	}
	if err := eth.LocalRPC(c.RPC); err != nil {
		return nil, nil, fmt.Errorf("payments: %w", err)
	}
	k := c.AnvilKeys
	if idx := map[int]bool{k.RelayerGas: true, k.Issuer: true, k.Policy: true, k.ShopGas: true}; len(idx) != 4 {
		return nil, nil, errors.New("payments: the four anvil keys are four different ones (each has its own nonces)")
	}
	var keys [4]*eth.Key
	for i, n := range []int{k.RelayerGas, k.Issuer, k.Policy, k.ShopGas} {
		var err error
		if keys[i], err = eth.AnvilKey(n); err != nil {
			return nil, nil, fmt.Errorf("payments: %w", err)
		}
	}
	token, err1 := eth.ParseAddress(c.Token)
	payTo, err2 := eth.ParseAddress(c.PayTo)
	contract, err3 := eth.ParseAddress(c.BudgetContract)
	if err := errors.Join(err1, err2, err3); err != nil || c.Confirmations == 0 || c.PolicyTTL == 0 || len(c.Delegations) == 0 {
		return nil, nil, errors.New("payments: token, pay_to and budget_contract addresses, confirmations, policy_ttl and delegations")
	}
	if fi, err := os.Stat(c.Dir); err != nil || !fi.IsDir() || fi.Mode().Perm()&0o077 != 0 {
		return nil, nil, errors.New("payments: dir is an existing directory private to its owner (0700)")
	}
	rpc := eth.NewClient(c.RPC)
	if id, err := rpc.ChainID(ctx); err != nil || !id.IsUint64() || id.Uint64() != c.ChainID {
		return nil, nil, fmt.Errorf("payments: the chain at rpc is not chain %d (or did not answer)", c.ChainID)
	}
	if err := eth.Anvil(ctx, rpc); err != nil { // a loopback port may be a tunnel to a public node
		return nil, nil, fmt.Errorf("payments: %w", err)
	}
	relayer, err := budget.Open(ctx, budget.Config{ChainID: c.ChainID, Contract: contract, Confirmations: c.Confirmations, PolicyTTL: c.PolicyTTL,
		Zkbudget: c.Zkbudget, SetupDir: c.ZkSetupDir}, budget.Keys{Gas: keys[0], Issuer: keys[1], Policy: keys[2]}, rpc, filepath.Join(c.Dir, "relayer.db"), clock)
	if err != nil {
		return nil, nil, err
	}
	settler, err := merchant.OpenSettler(ctx, merchant.SettleConfig{ChainID: c.ChainID, Token: token, TokenName: c.TokenName, TokenVersion: c.TokenVersion,
		PayTo: payTo, Confirmations: c.Confirmations}, keys[3], rpc, filepath.Join(c.Dir, "settle.db"), clock)
	if err != nil {
		relayer.Close()
		return nil, nil, err
	}
	shop, err := merchant.NewSettled(baseURL, settler)
	if err != nil {
		relayer.Close()
		settler.Close()
		return nil, nil, err
	}
	return &payments{cfg: c, rpc: rpc, relayer: relayer, settler: settler, out: out}, shop, nil
}

func (p *payments) Close() error { return errors.Join(p.relayer.Close(), p.settler.Close()) }

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

// reconciliation is the Host's reconciliation on this local chain
// (docs/commercial-p2-host.md 7; AE1 acceptance 18): every confirmed
// transfer out of the payer wallets against every authorization the Store
// recorded as signed.
func (p *payments) reconciliation(ctx context.Context, st *store.Store, wallets []eth.Address, now uint64) (reconcile.Report, error) {
	known, err := payflow.Known(st)
	if err != nil {
		return reconcile.Report{}, err
	}
	token, _ := eth.ParseAddress(p.cfg.Token)
	t := reconcile.Token{RPC: p.rpc, Address: token, Confirmations: p.cfg.Confirmations, FromBlock: p.cfg.TokenFromBlock}
	return t.Report(ctx, "local_anvil", p.cfg.ChainID, wallets, known, now) // P2-a: openPayments took a local anvil only
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
		Shop:  payflow.HTTPShop{Client: &http.Client{Timeout: 30 * time.Second}},
		Chain: reconcile.Token{RPC: p.rpc, Address: token, Confirmations: p.cfg.Confirmations, FromBlock: p.cfg.TokenFromBlock},
		Clock: clock, Margin: payMargin, Before: starter.Start,
		Log: func(intent string, err error) { fmt.Fprintln(p.out, "demo: payment", intent, store.CodeOf(err)) }}
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
