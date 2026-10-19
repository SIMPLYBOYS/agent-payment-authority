// Command apa-signer is the independent Signer of P1-b
// (docs/commercial-p1b-design.md): it holds the payment keys, checks every
// request against its own configuration and signs one EIP-3009
// authorization per accepted attempt.
//
//	apa-signer serve --dir DIR
//	apa-signer keygen --dir DIR --wallet ID
//	apa-signer serve-approvals --dir DIR        (A2: human approval only, approvals.go)
//	apa-signer approver enroll|revoke|list ...  (A2: the administrator's commands)
//
// DIR (private to its owner) holds signer.json, keys/<wallet_id>.key and
// signer.db; the passphrase is read from this terminal. It is meant to run
// as its own OS user (apa-signer; docs/commercial-p1b-os-isolation.md).
// Until P2 its reservation verifier refuses every order, so it signs
// nothing; no flag or variable changes that. keygen makes one new key file
// (decision D4 of the OS isolation plan: a one-time test key).
package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"net"
	"os"
	"os/signal"
	"path/filepath"
	"strconv"
	"syscall"
	"time"

	"github.com/SIMPLYBOYS/agent-payment-authority/internal/budget"
	"github.com/SIMPLYBOYS/agent-payment-authority/internal/eth"
	"github.com/SIMPLYBOYS/agent-payment-authority/internal/keytool"
	"github.com/SIMPLYBOYS/agent-payment-authority/internal/signer"
)

func main() {
	if err := run(os.Args[1:], os.Stderr); err != nil {
		fmt.Fprintln(os.Stderr, "apa-signer:", err)
		os.Exit(1)
	}
}

func run(args []string, out io.Writer) error {
	usage := errors.New("usage: apa-signer serve --dir DIR | keygen --dir DIR --wallet ID | serve-approvals --dir DIR | approver ...")
	if len(args) > 0 && args[0] == "approver" {
		return approver(args[1:], out, askTTY)
	}
	if len(args) > 0 && args[0] == "serve-approvals" {
		fs := flag.NewFlagSet("serve-approvals", flag.ContinueOnError)
		fs.SetOutput(io.Discard)
		dir := fs.String("dir", "", "the Signer's private directory")
		if err := fs.Parse(args[1:]); err != nil || *dir == "" || fs.NArg() != 0 {
			return usage
		}
		return serveApprovals(*dir, out) // no terminal, no passphrase, no key
	}
	if len(args) == 0 || args[0] != "serve" && args[0] != "keygen" {
		return usage
	}
	fs := flag.NewFlagSet(args[0], flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	dir := fs.String("dir", "", "the Signer's private directory")
	var wallet string
	if args[0] == "keygen" {
		fs.StringVar(&wallet, "wallet", "", "the wallet ID of the new key")
	}
	if err := fs.Parse(args[1:]); err != nil || *dir == "" || fs.NArg() != 0 || args[0] == "keygen" && wallet == "" {
		return usage
	}
	tty, err := os.OpenFile("/dev/tty", os.O_RDWR, 0)
	if err != nil {
		return errors.New("the passphrase is read from a terminal (/dev/tty), and there is none")
	}
	defer tty.Close()
	if args[0] == "keygen" {
		prompts := []string{"new passphrase: ", "the same passphrase again: "}
		return keygen(*dir, wallet, func() ([]byte, error) {
			p := prompts[0]
			prompts = prompts[1:]
			return readPassphrase(tty, p)
		}, out)
	}
	core, cfg, err := assemble(*dir, func() ([]byte, error) { return readPassphrase(tty, "passphrase: ") }, func() uint64 { return uint64(time.Now().Unix()) })
	if err != nil {
		return err
	}
	defer core.Close()
	// P2-D1: with approvers configured, the same process serves the
	// approvals on the same signer.db.
	var ac *signer.ApprovalCore
	if cfg.WebAuthn != nil && len(cfg.Approvers) > 0 {
		if ac, err = core.Approvals(context.Background(), *dir, unixClock); err != nil {
			return err
		}
	}
	if _, err := os.Lstat(cfg.Socket); err == nil {
		return fmt.Errorf("%s exists: another Signer may be serving; remove it yourself if not", cfg.Socket)
	}
	ln, err := net.Listen("unix", cfg.Socket)
	if err != nil {
		return err
	}
	defer os.Remove(cfg.Socket)
	if err := os.Chmod(cfg.Socket, 0o660); err != nil {
		return err
	}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	approvals := ""
	if ac != nil {
		approvals = "; approvals on the same socket"
	}
	reservations := "reservation verification unavailable: nothing is signed"
	if cfg.Reservation != nil {
		reservations = "reservations checked on chain " + cfg.Reservation.ChainID
	}
	fmt.Fprintf(out, "apa-signer: serving on %s (%s%s)\n", cfg.Socket, reservations, approvals)
	return signer.ServeAll(ctx, ln, core, ac, func(line string) { fmt.Fprintln(out, "apa-signer:", line) })
}

// recoveryTimeout bounds the startup recovery (signer.NewCore).
const recoveryTimeout = 60 * time.Second

// assemble is the production assembly: the directory's configuration,
// keys and DB; reservations are checked on the configured chain, and
// without a reservation section nothing is signed (Unavailable).
func assemble(dir string, passphrase func() ([]byte, error), clock signer.Clock) (*signer.Core, *signer.Config, error) {
	if err := keytool.Private(dir); err != nil {
		return nil, nil, err
	}
	if err := signer.Unmigrated(dir); err != nil {
		return nil, nil, err
	}
	raw, err := os.ReadFile(filepath.Join(dir, "signer.json"))
	if err != nil {
		return nil, nil, err
	}
	cfg, err := signer.ParseConfig(raw)
	if err != nil {
		return nil, nil, err
	}
	if !filepath.IsAbs(cfg.Socket) {
		return nil, nil, errors.New("config: the socket path must be absolute")
	}
	if err := keytool.Private(filepath.Join(dir, "keys")); err != nil {
		return nil, nil, err
	}
	pass, err := passphrase()
	if err != nil {
		return nil, nil, err
	}
	defer clear(pass)
	keys, err := signer.LoadKeys(filepath.Join(dir, "keys"), cfg, pass)
	if err != nil {
		return nil, nil, err
	}
	// The recovery of attempts left accepted runs before serving, bounded.
	ctx, cancel := context.WithTimeout(context.Background(), recoveryTimeout)
	defer cancel()
	var res signer.ReservationVerifier = signer.Unavailable{}
	if r := cfg.Reservation; r != nil { // P2: reservations checked on chain (ParseConfig checked the section)
		chain, _ := strconv.ParseUint(r.ChainID, 10, 64)
		contract, _ := eth.ParseAddress(r.BudgetContract)
		res = &budget.Verifier{RPC: eth.NewClient(r.RPC), ChainID: chain, Contract: contract, Confirmations: r.Confirmations}
	}
	// Made 0600 before SQLite opens it (its WAL and shm follow the file's
	// mode): the approver commands refuse a signer.db readable by others.
	path := filepath.Join(dir, "signer.db")
	if err := privateFile(path); err != nil {
		return nil, nil, err
	}
	core, err := signer.NewCore(ctx, cfg, keys, path, clock, res)
	return core, cfg, err
}

// keygen writes one new payment wallet key file DIR/keys/<walletID>.key
// (keytool.Keygen under the Signer's label) and prints the wallet ID and the
// address only.
func keygen(dir, walletID string, passphrase func() ([]byte, error), out io.Writer) error {
	if !signer.ValidWalletID(walletID) {
		return fmt.Errorf("wallet ID %q is not valid", walletID)
	}
	addr, err := keytool.Keygen(dir, signer.SignerLabel, walletID, passphrase)
	if err != nil {
		return err
	}
	fmt.Fprintf(out, "apa-signer: wallet %s address %s\n", walletID, addr)
	return nil
}

func readPassphrase(tty *os.File, prompt string) ([]byte, error) {
	return keytool.ReadPassphrase(tty, "apa-signer", prompt)
}
