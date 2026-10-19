// Command apa-relayer is the Host's budget relayer as its own process
// (docs/commercial-p2b.md B1, B2; H-D5, P2-D2): it holds the gas, issuer
// and policy keys and registers tasks and reserves orders (tx#1) for the
// Host over a Unix socket. keygen also makes the shop's key (P2b-D3, D4).
//
//	apa-relayer keygen --dir DIR --role relayer-gas|issuer|policy|shop
//	apa-relayer serve --dir DIR
//
// keygen makes one new key file under DIR/keys; the passphrase is read
// twice from this terminal, never from a flag, a variable or a file. It
// prints the role and the address only.
//
// serve reads DIR/relayer.json (relayerConfig), asks the passphrase once
// (the three keys share it), checks the chain, then serves on the
// configured socket (this user's only) to the configured uids until interrupted. Its
// database is DIR/relayer.db; DIR and DIR/keys are this user's alone.
package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"net"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"
	"time"

	"github.com/SIMPLYBOYS/agent-payment-authority/internal/budget"
	"github.com/SIMPLYBOYS/agent-payment-authority/internal/eth"
	"github.com/SIMPLYBOYS/agent-payment-authority/internal/keytool"
)

func main() {
	if err := run(os.Args[1:], os.Stderr, openTTY); err != nil {
		fmt.Fprintln(os.Stderr, "apa-relayer:", err)
		os.Exit(1)
	}
}

func openTTY() (*os.File, error) {
	tty, err := os.OpenFile("/dev/tty", os.O_RDWR, 0)
	if err != nil {
		return nil, errors.New("the passphrase is read from a terminal (/dev/tty), and there is none")
	}
	return tty, nil
}

func run(args []string, out io.Writer, tty func() (*os.File, error)) error {
	usage := errors.New("usage: apa-relayer keygen --dir DIR --role relayer-gas|issuer|policy|shop | serve --dir DIR")
	if len(args) > 0 && args[0] == "serve" {
		fs := flag.NewFlagSet("serve", flag.ContinueOnError)
		fs.SetOutput(io.Discard)
		dir := fs.String("dir", "", "the relayer's private directory")
		if err := fs.Parse(args[1:]); err != nil || *dir == "" || fs.NArg() != 0 {
			return usage
		}
		t, err := tty()
		if err != nil {
			return err
		}
		defer t.Close()
		ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
		defer stop()
		return serve(ctx, *dir, func() ([]byte, error) { return keytool.ReadPassphrase(t, "apa-relayer", "passphrase: ") }, out)
	}
	if len(args) == 0 || args[0] != "keygen" {
		return usage
	}
	fs := flag.NewFlagSet("keygen", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	dir := fs.String("dir", "", "the private directory of the Host's keys")
	role := fs.String("role", "", "the key's role")
	if err := fs.Parse(args[1:]); err != nil || *dir == "" || fs.NArg() != 0 {
		return usage
	}
	label, ok := keytool.Role(*role)
	if !ok {
		return usage
	}
	t, err := tty()
	if err != nil {
		return err
	}
	defer t.Close()
	prompts := []string{"new passphrase: ", "the same passphrase again: "}
	addr, err := keytool.Keygen(*dir, label, *role, func() ([]byte, error) {
		p := prompts[0]
		prompts = prompts[1:]
		return keytool.ReadPassphrase(t, "apa-relayer", p)
	})
	if err != nil {
		return err
	}
	fmt.Fprintf(out, "apa-relayer: %s address %s\n", *role, addr)
	return nil
}

// relayerConfig is DIR/relayer.json. It holds no key.
type relayerConfig struct {
	Socket         string   `json:"socket"`
	Callers        []uint32 `json:"callers"` // uids that may call (the Host's)
	RPC            string   `json:"rpc"`
	ChainID        uint64   `json:"chain_id"`
	BudgetContract string   `json:"budget_contract"`
	Confirmations  uint64   `json:"confirmations"`
	PolicyTTL      uint64   `json:"policy_ttl"`
	Zkbudget       string   `json:"zkbudget"`
	ZkSetupDir     string   `json:"zk_setup_dir"`
	DailyTxCap     uint64   `json:"daily_tx_cap"`
}

func readConfig(path string) (relayerConfig, error) {
	var c relayerConfig
	f, err := os.Open(path)
	if err != nil {
		return c, err
	}
	defer f.Close()
	d := json.NewDecoder(io.LimitReader(f, 64<<10))
	d.DisallowUnknownFields()
	if err := d.Decode(&c); err != nil || d.More() {
		return c, fmt.Errorf("%s: not a relayer configuration", path)
	}
	if !filepath.IsAbs(c.Socket) || len(c.Callers) == 0 || c.RPC == "" || c.ChainID == 0 || c.Confirmations == 0 ||
		c.PolicyTTL == 0 || c.PolicyTTL > 600 || c.Zkbudget == "" || c.ZkSetupDir == "" || c.DailyTxCap == 0 {
		return c, fmt.Errorf("%s: socket (absolute), callers, rpc, chain_id, confirmations, policy_ttl (1-600 s), zkbudget, zk_setup_dir and daily_tx_cap are required", path)
	}
	return c, nil
}

// serve opens the keys, the chain and the database, then serves until ctx ends.
func serve(ctx context.Context, dir string, passphrase func() ([]byte, error), out io.Writer) error {
	if err := keytool.Private(dir); err != nil {
		return err
	}
	if err := keytool.Private(filepath.Join(dir, "keys")); err != nil {
		return err
	}
	c, err := readConfig(filepath.Join(dir, "relayer.json"))
	if err != nil {
		return err
	}
	contract, err := eth.ParseAddress(c.BudgetContract)
	if err != nil {
		return fmt.Errorf("budget_contract: %w", err)
	}
	if _, err := os.Lstat(c.Socket); !errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("%s exists: another relayer may be running; stop it (or remove the file if nothing runs)", c.Socket)
	}
	pass, err := passphrase()
	if err != nil {
		return err
	}
	defer clear(pass)
	var keys budget.Keys
	for _, k := range []struct {
		role string
		to   **eth.Key
	}{{"relayer-gas", &keys.Gas}, {"issuer", &keys.Issuer}, {"policy", &keys.Policy}} {
		if *k.to, err = keytool.Load(dir, keytool.RelayerLabel, k.role, pass); err != nil {
			return err
		}
	}
	rpc := eth.NewClient(c.RPC)
	cctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	id, err := rpc.ChainID(cctx)
	cancel()
	if err != nil || !id.IsUint64() || id.Uint64() != c.ChainID {
		return fmt.Errorf("the chain at rpc is not chain %d (or did not answer)", c.ChainID)
	}
	r, err := budget.Open(ctx, budget.Config{ChainID: c.ChainID, Contract: contract, Confirmations: c.Confirmations, PolicyTTL: c.PolicyTTL,
		Zkbudget: c.Zkbudget, SetupDir: c.ZkSetupDir, DailyTxCap: c.DailyTxCap}, keys, rpc, filepath.Join(dir, "relayer.db"), func() uint64 { return uint64(time.Now().Unix()) })
	if err != nil {
		return err
	}
	defer r.Close()
	old := syscall.Umask(0o077)
	ln, err := net.Listen("unix", c.Socket)
	syscall.Umask(old)
	if err != nil {
		return err
	}
	defer os.Remove(c.Socket)
	defer ln.Close()
	fmt.Fprintf(out, "apa-relayer: serving on %s (chain %d, budget %s, gas %s, issuer %s, policy %s, at most %d transactions a day)\n",
		c.Socket, c.ChainID, contract.Hex(), keys.Gas.Addr.Hex(), keys.Issuer.Addr.Hex(), keys.Policy.Addr.Hex(), c.DailyTxCap)
	h := budget.Handler(r, budget.Health{State: "ready", ChainID: c.ChainID, Contract: contract.Hex(), DailyTxCap: c.DailyTxCap}, c.Callers,
		func(line string) { fmt.Fprintln(out, "apa-relayer:", line) })
	return budget.Serve(ctx, ln, h)
}
