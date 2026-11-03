// Command apa-facilitator is the shop's x402 v2 facilitator as its own
// process (docs/commercial-facilitator.md FA2): POST /verify (read only),
// POST /settle and GET /supported, and GET /health (its transfers today),
// on this machine only. It holds the shop's gas key and its settlements;
// the Host's shop holds neither and asks it.
//
//	apa-facilitator serve --payments FILE [--listen 127.0.0.1:8403]
//
// FILE is the Host's payments file (the chain, the token, the payee, the
// shop's key): on base_sepolia the key in shop_key_dir, its passphrase asked
// on this terminal; on local_anvil anvil_keys.shop_gas. The settlements are
// in the file's dir, settle.db.
package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"
	"time"

	"github.com/SIMPLYBOYS/agent-payment-authority/internal/eth"
	"github.com/SIMPLYBOYS/agent-payment-authority/internal/facilitator"
	"github.com/SIMPLYBOYS/agent-payment-authority/internal/keytool"
	"github.com/SIMPLYBOYS/agent-payment-authority/internal/merchant"
)

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	if err := run(ctx, os.Args[1:], os.Stdout, passphrase); err != nil {
		fmt.Fprintln(os.Stderr, "apa-facilitator:", err)
		os.Exit(1)
	}
}

// config is what it reads of the Host's payments file.
type config struct {
	Environment    string `json:"environment"`
	RPC            string `json:"rpc"`
	ChainID        uint64 `json:"chain_id"`
	Confirmations  uint64 `json:"confirmations"`
	Token          string `json:"token"`
	TokenName      string `json:"token_name"`
	TokenVersion   string `json:"token_version"`
	PayTo          string `json:"pay_to"`
	ShopKeyDir     string `json:"shop_key_dir"`
	ShopDailyTxCap uint64 `json:"shop_daily_tx_cap"`
	AnvilKeys      *struct {
		ShopGas int `json:"shop_gas"`
	} `json:"anvil_keys"`
	Dir string `json:"dir"`
}

const baseSepolia = 84532

func run(ctx context.Context, args []string, out io.Writer, pass func() ([]byte, error)) error {
	fs := flag.NewFlagSet("apa-facilitator", flag.ContinueOnError)
	path := fs.String("payments", "", "the Host's payments file")
	listen := fs.String("listen", "127.0.0.1:8403", "this machine's address to serve on")
	if len(args) == 0 || args[0] != "serve" || fs.Parse(args[1:]) != nil || fs.NArg() != 0 || *path == "" {
		return errors.New("usage: apa-facilitator serve --payments FILE [--listen 127.0.0.1:8403]")
	}
	if host, _, err := net.SplitHostPort(*listen); err != nil || !net.ParseIP(host).IsLoopback() {
		return errors.New("--listen is a loopback address (the facilitator is this machine's only)")
	}
	raw, err := os.ReadFile(*path)
	var c config
	if err != nil || json.Unmarshal(raw, &c) != nil {
		return fmt.Errorf("--payments %s is not a payments file (%v)", *path, err)
	}
	sc, wait, err := c.settle()
	if err != nil {
		return err
	}
	if fi, err := os.Stat(c.Dir); err != nil || !fi.IsDir() || fi.Mode().Perm()&0o077 != 0 {
		return errors.New("the payments file's dir is an existing directory private to its owner (0700)")
	}
	rpc := eth.NewClient(c.RPC)
	if id, err := rpc.ChainID(ctx); err != nil || !id.IsUint64() || id.Uint64() != c.ChainID {
		return fmt.Errorf("the chain at rpc is not chain %d (or did not answer); nothing was started", c.ChainID)
	}
	// A testnet must not be a local stand-in, and a local chain is anvil (as the Host's checkChain).
	if isAnvil := eth.Anvil(ctx, rpc) == nil; isAnvil == (c.Environment == "base_sepolia") {
		return errors.New("base_sepolia's rpc is a public node and a local chain's is anvil; nothing was started")
	}
	key, err := c.key(pass)
	if err != nil {
		return err
	}
	if key.Addr != sc.PayTo {
		return fmt.Errorf("the shop key is %s, not pay_to %s (the shop pays its own gas and is paid; P2b-D3)", key.Addr.Hex(), sc.PayTo.Hex())
	}
	st, err := merchant.OpenSettler(ctx, sc, key, rpc, filepath.Join(c.Dir, "settle.db"), func() uint64 { return uint64(time.Now().Unix()) })
	if err != nil {
		return err
	}
	defer st.Close()
	mux := http.NewServeMux()
	facilitator.New(st, sc, key.Addr).Register(mux)
	mux.HandleFunc("GET /health", func(w http.ResponseWriter, r *http.Request) {
		n, err := st.SentToday(r.Context())
		h := map[string]any{"state": "ready", "chain_id": sc.ChainID, "pay_to": sc.PayTo.Hex(), "signer": key.Addr.Hex(), "daily_tx_cap": sc.DailyTxCap}
		if err == nil {
			h["sent_today"] = n
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(h)
	})
	ln, err := net.Listen("tcp", *listen)
	if err != nil {
		return err
	}
	// A /settle may wait out its transfer: the write deadline is above that wait.
	srv := &http.Server{Handler: mux, ReadHeaderTimeout: 10 * time.Second, ReadTimeout: 30 * time.Second, WriteTimeout: wait + 30*time.Second}
	fmt.Fprintf(out, "apa-facilitator: listening on http://%s (chain %d, token %s, payee %s, signer %s, %d confirmations)\n",
		ln.Addr(), sc.ChainID, sc.Token.Hex(), sc.PayTo.Hex(), key.Addr.Hex(), sc.Confirmations)
	done := make(chan error, 1)
	go func() { done <- srv.Serve(ln) }()
	select {
	case err := <-done:
		return err
	case <-ctx.Done():
		sctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		return srv.Shutdown(sctx)
	}
}

// settle is the settlement the file describes, and how long one /settle may
// wait for its transfer; the testnet's rules as the Host's (payments.go).
func (c config) settle() (merchant.SettleConfig, time.Duration, error) {
	token, err1 := eth.ParseAddress(c.Token)
	payTo, err2 := eth.ParseAddress(c.PayTo)
	if errors.Join(err1, err2) != nil || c.Confirmations == 0 || c.TokenName == "" || c.TokenVersion == "" {
		return merchant.SettleConfig{}, 0, errors.New("the payments file names the token (and its EIP-712 name and version), pay_to and confirmations")
	}
	sc := merchant.SettleConfig{ChainID: c.ChainID, Token: token, TokenName: c.TokenName, TokenVersion: c.TokenVersion, PayTo: payTo,
		Confirmations: c.Confirmations, DailyTxCap: c.ShopDailyTxCap}
	switch c.Environment {
	case "", "local_anvil":
		if err := eth.LocalRPC(c.RPC); err != nil {
			return sc, 0, err
		}
		return sc, 20 * time.Second, nil
	case "base_sepolia":
		u, err := url.Parse(c.RPC)
		switch {
		case c.ChainID != baseSepolia:
			return sc, 0, fmt.Errorf("base_sepolia is chain %d", baseSepolia)
		case err != nil || u.Scheme != "https" || u.User != nil || u.Host == "":
			return sc, 0, errors.New("base_sepolia's rpc is an https URL without user info")
		case c.Confirmations < 2 || c.ShopDailyTxCap == 0 || c.ShopKeyDir == "" || c.AnvilKeys != nil:
			return sc, 0, errors.New("base_sepolia needs at least 2 confirmations, shop_daily_tx_cap and shop_key_dir, and takes no anvil key")
		}
		sc.Poll, sc.Wait = time.Second, 2*time.Minute // public blocks, a public node: ask less often, wait longer
		return sc, sc.Wait, nil
	}
	return sc, 0, fmt.Errorf("environment %q is local_anvil or base_sepolia", c.Environment)
}

// key is the shop's gas key: its key file (a passphrase), or anvil's shop_gas.
func (c config) key(pass func() ([]byte, error)) (*eth.Key, error) {
	if c.ShopKeyDir == "" {
		if c.AnvilKeys == nil {
			return nil, errors.New("the payments file names shop_key_dir, or anvil_keys.shop_gas on a local anvil")
		}
		return eth.AnvilKey(c.AnvilKeys.ShopGas)
	}
	p, err := pass()
	if err != nil {
		return nil, fmt.Errorf("the shop key's passphrase: %w", err)
	}
	defer clear(p)
	return keytool.Load(c.ShopKeyDir, keytool.ShopLabel, "shop", p)
}

// passphrase asks the shop key's passphrase on this terminal (P2b-D4).
func passphrase() ([]byte, error) {
	tty, err := os.OpenFile("/dev/tty", os.O_RDWR, 0)
	if err != nil {
		return nil, errors.New("it is read from a terminal (/dev/tty), and there is none")
	}
	defer tty.Close()
	return keytool.ReadPassphrase(tty, "apa-facilitator", "shop key passphrase: ")
}
