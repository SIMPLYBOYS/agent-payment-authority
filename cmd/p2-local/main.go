// Command p2-local sets up a fresh local anvil for the P2-a demo
// (docs/commercial-p2-host.md 6, 12.3): it deploys the Z2 verifier, the
// budget contract (issuer and policy: anvil keys 1 and 2) and TestUSDC,
// puts TestUSDC's code at the registry's fixture asset address (so the
// registry, profile, scenarios and signer.json keep naming it), mints test
// USDC to the two payer wallets, and writes into --out the demo's
// --payments file and a registry naming those wallets. It prints the
// signer.json reservation section. Local anvil only, anvil's public keys
// only; it reads and writes no other key.
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
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/SIMPLYBOYS/agent-payment-authority/internal/contracts"
	"github.com/SIMPLYBOYS/agent-payment-authority/internal/eth"
)

func main() {
	if err := run(context.Background(), os.Args[1:], os.Stdout); err != nil {
		fmt.Fprintln(os.Stderr, "p2-local:", err)
		os.Exit(1)
	}
}

// chainID is the registry fixture's chain, the local anvil's --chain-id.
const chainID = 84532

func run(ctx context.Context, args []string, out io.Writer) error {
	fs := flag.NewFlagSet("p2-local", flag.ContinueOnError)
	rpcURL := fs.String("rpc", "http://127.0.0.1:8545", "the local anvil (started with --chain-id 84532)")
	walletA := fs.String("wallet-a", "", "wallet-a's address (apa-signer keygen printed it)")
	walletB := fs.String("wallet-b", "", "wallet-b's address")
	outDir := fs.String("out", "var/p2-local", "where payments.json, registry.json and the relayer and shop databases go (created 0700)")
	contractsOut := fs.String("contracts", "var/contracts/out", "the contract build")
	zkbudget := fs.String("zkbudget", "var/p2-local/zkbudget", "the zkbudget binary the relayer runs")
	registryPath := fs.String("registry", "testdata/demo/registry.json", "the demo registry the written one is made from (its show-* tasks are the runs')")
	mint := fs.Uint64("mint", 20_000_000, "test USDC units minted to each wallet")
	if err := fs.Parse(args); err != nil || fs.NArg() != 0 {
		return errors.New("usage: p2-local --wallet-a 0x... --wallet-b 0x... [--rpc URL] [--out DIR]")
	}
	if err := eth.LocalRPC(*rpcURL); err != nil {
		return err
	}
	// A previous chain's databases name tasks and transactions this chain
	// never saw (the same task IDs and contract addresses on a fresh anvil).
	for _, f := range []string{"payments.json", "relayer.db", "settle.db"} {
		if _, err := os.Lstat(filepath.Join(*outDir, f)); !errors.Is(err, os.ErrNotExist) {
			return fmt.Errorf("%s already holds %s from an earlier chain: remove the directory yourself, or name another (--out / P2_LOCAL_DIR)", *outDir, f)
		}
	}
	a, errA := eth.ParseAddress(*walletA)
	b, errB := eth.ParseAddress(*walletB)
	if errors.Join(errA, errB) != nil || a == b {
		return errors.New("--wallet-a and --wallet-b are two different addresses")
	}
	var reg contracts.RegistryConfig
	raw, err := os.ReadFile(*registryPath)
	if err != nil {
		return err
	}
	if err := contracts.Decode(raw, &reg); err != nil {
		return fmt.Errorf("registry: %w", err)
	}
	wallet := map[string]int{}
	for i, w := range reg.Wallets {
		wallet[w.WalletID] = i
	}
	ia, okA := wallet["wallet-a"]
	ib, okB := wallet["wallet-b"]
	if !okA || !okB || len(reg.Assets) != 1 {
		return errors.New("the registry names wallet-a, wallet-b and one asset")
	}
	asset, err := eth.ParseAddress(reg.Assets[0].AssetContract)
	if err != nil {
		return err
	}
	rpc := eth.NewClient(*rpcURL)
	if id, err := rpc.ChainID(ctx); err != nil || !id.IsUint64() || id.Uint64() != chainID {
		return fmt.Errorf("the node at %s is not a chain %d anvil (or did not answer)", *rpcURL, chainID)
	}
	if err := eth.Anvil(ctx, rpc); err != nil {
		return err
	}
	var code string
	if err := rpc.Raw(ctx, "eth_getCode", &code, asset.Hex(), "latest"); err != nil {
		return err
	}
	if code != "0x" {
		return fmt.Errorf("%s already has code: this anvil is set up; start a fresh one", asset.Hex())
	}
	keys := make([]*eth.Key, 3)
	for i := range keys {
		if keys[i], err = eth.AnvilKey(i); err != nil {
			return err
		}
	}
	verifier, err := deploy(ctx, rpc, keys[0], filepath.Join(*contractsOut, "Verifier.sol/Verifier.json"), nil)
	if err != nil {
		return err
	}
	ctor, _ := eth.Encode(verifier, keys[1].Addr, keys[2].Addr)
	budget, err := deploy(ctx, rpc, keys[0], filepath.Join(*contractsOut, "Budget.sol/Budget.json"), ctor)
	if err != nil {
		return err
	}
	usdc, err := deploy(ctx, rpc, keys[0], filepath.Join(*contractsOut, "TestUSDC.sol/TestUSDC.json"), nil)
	if err != nil {
		return err
	}
	// The deployed runtime code (its owner, key 0, is in it) at the fixture
	// address: the token's EIP-712 domain reads its own address.
	if err := rpc.Raw(ctx, "eth_getCode", &code, usdc.Hex(), "latest"); err != nil {
		return err
	}
	if err := rpc.Raw(ctx, "anvil_setCode", nil, asset.Hex(), code); err != nil {
		return err
	}
	// The shop's gas key too (anvil_keys.shop_gas, key 4): the local pay_to has no key, so the shop's refunds
	// (docs/commercial-refund.md) come from the key that sends its transfers, as on the testnet (there it is pay_to).
	shopGas, err := eth.AnvilKey(4)
	if err != nil {
		return err
	}
	for _, w := range []eth.Address{a, b, shopGas.Addr} {
		data, _ := eth.Call("mint(address,uint256)", w, *mint)
		if _, err := send(ctx, rpc, keys[0], &eth.Tx{To: &asset, Gas: 200_000, Data: data}); err != nil {
			return fmt.Errorf("mint (start a fresh anvil to set up again): %w", err)
		}
	}
	block, err := rpc.BlockNumber(ctx)
	if err != nil {
		return err
	}

	if err := os.MkdirAll(*outDir, 0o700); err != nil {
		return err
	}
	if err := os.Chmod(*outDir, 0o700); err != nil {
		return err
	}
	root, _ := filepath.Abs(".")
	pay := map[string]any{"rpc": *rpcURL, "chain_id": chainID, "confirmations": 1, "token": asset.Hex(), "token_name": reg.Assets[0].EIP712Name,
		"token_version": reg.Assets[0].EIP712Version, "token_from_block": 0, "pay_to": reg.MerchantPayTo, "budget_contract": budget.Hex(),
		"policy_ttl": 600, "zkbudget": abs(root, *zkbudget), "zk_setup_dir": filepath.Join(root, "zk/setup"),
		"anvil_keys": map[string]int{"relayer_gas": 3, "issuer": 1, "policy": 2, "shop_gas": 4},
		// FA2 (docs/commercial-facilitator.md): the shop settles through apa-facilitator, which present.sh starts first.
		"facilitator_url": "http://127.0.0.1:8403",
		"dir":             abs(root, *outDir),
		"delegations":     map[string]string{"consumer-a": "dlg-a@1", "consumer-b": "dlg-b@1"}}
	reg.Wallets[ia].From, reg.Wallets[ib].From = a.Hex(), b.Hex()
	if _, err := contracts.NewRegistry(reg); err != nil {
		return fmt.Errorf("the written registry: %w", err)
	}
	for name, v := range map[string]any{"payments.json": pay, "registry.json": reg} {
		raw, _ := json.MarshalIndent(v, "", "  ")
		if err := os.WriteFile(filepath.Join(*outDir, name), append(raw, '\n'), 0o600); err != nil {
			return err
		}
	}
	section, _ := json.MarshalIndent(map[string]any{"reservation": map[string]any{"rpc": *rpcURL, "chain_id": fmt.Sprint(chainID),
		"budget_contract": budget.Hex(), "confirmations": 1}}, "", "  ")
	fmt.Fprintf(out, "p2-local: chain %d at %s (block %d): budget %s, verifier %s, TestUSDC at %s; minted %d to %s and %s\n",
		chainID, *rpcURL, block, budget.Hex(), verifier.Hex(), asset.Hex(), *mint, a.Hex(), b.Hex())
	fmt.Fprintf(out, "p2-local: wrote %s and %s\n", filepath.Join(*outDir, "payments.json"), filepath.Join(*outDir, "registry.json"))
	fmt.Fprintf(out, "p2-local: add to signer.json (max_window_seconds at least 300; delegations dlg-a and dlg-b, version 1, for consumer-a and consumer-b, asset %s, merchant %s):\n%s\n",
		asset.Hex(), reg.MerchantPayTo, section)
	fmt.Fprintf(out, "p2-local: start the demo with --registry %s --payments %s, --approvals-signer on apa-signer serve, and a new APA_DB_PATH\n",
		filepath.Join(*outDir, "registry.json"), filepath.Join(*outDir, "payments.json"))
	return nil
}

func abs(root, p string) string {
	if filepath.IsAbs(p) {
		return p
	}
	return filepath.Join(root, p)
}

// deploy sends the artifact's creation code with its constructor arguments.
func deploy(ctx context.Context, rpc *eth.Client, k *eth.Key, artifact string, ctor []byte) (eth.Address, error) {
	raw, err := os.ReadFile(artifact)
	if err != nil {
		return eth.Address{}, fmt.Errorf("the contract build: %w", err)
	}
	var a struct{ Bytecode struct{ Object string } }
	if err := json.Unmarshal(raw, &a); err != nil {
		return eth.Address{}, err
	}
	code, err := hex.DecodeString(strings.TrimPrefix(a.Bytecode.Object, "0x"))
	if err != nil {
		return eth.Address{}, err
	}
	rc, err := send(ctx, rpc, k, &eth.Tx{Gas: 6_000_000, Data: append(code, ctor...)})
	if err != nil {
		return eth.Address{}, fmt.Errorf("deploy %s: %w", filepath.Base(artifact), err)
	}
	if rc.ContractAddress == nil {
		return eth.Address{}, fmt.Errorf("deploy %s: no address", filepath.Base(artifact))
	}
	return *rc.ContractAddress, nil
}

// send signs tx from k (chain, nonce and fees filled in) and waits for its
// successful receipt.
func send(ctx context.Context, rpc *eth.Client, k *eth.Key, tx *eth.Tx) (*eth.Receipt, error) {
	tx.ChainID, tx.Value = big.NewInt(chainID), new(big.Int)
	var err error
	if tx.Nonce, err = rpc.PendingNonce(ctx, k.Addr); err != nil {
		return nil, err
	}
	h, err := rpc.LatestHeader(ctx)
	if err != nil {
		return nil, err
	}
	tx.MaxPriorityFeePerGas, tx.MaxFeePerGas = big.NewInt(1e9), new(big.Int).Add(new(big.Int).Mul(h.BaseFee, big.NewInt(2)), big.NewInt(1e9))
	signed, hash, err := tx.Sign(k)
	if err != nil {
		return nil, err
	}
	if _, err := rpc.SendRaw(ctx, signed); err != nil {
		return nil, err
	}
	for range 200 {
		if rc, _ := rpc.Receipt(ctx, hash); rc != nil {
			if rc.Status != 1 {
				return nil, errors.New("reverted")
			}
			return rc, nil
		}
		time.Sleep(50 * time.Millisecond)
	}
	return nil, errors.New("not mined")
}
