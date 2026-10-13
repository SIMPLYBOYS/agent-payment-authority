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
	"bytes"
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

	"github.com/SIMPLYBOYS/agent-payment-authority/internal/signer"
	"github.com/decred/dcrd/dcrec/secp256k1/v4"
	"golang.org/x/sys/unix"
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
	fmt.Fprintf(out, "apa-signer: serving on %s (reservation verification unavailable until P2: nothing is signed)\n", cfg.Socket)
	return signer.Serve(ctx, ln, core, func(line string) { fmt.Fprintln(out, "apa-signer:", line) })
}

// recoveryTimeout bounds the startup recovery (signer.NewCore).
const recoveryTimeout = 60 * time.Second

// assemble is the production assembly: the directory's configuration,
// keys and DB, and the Unavailable reservation verifier, always.
func assemble(dir string, passphrase func() ([]byte, error), clock signer.Clock) (*signer.Core, *signer.Config, error) {
	if err := private(dir); err != nil {
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
	if err := private(filepath.Join(dir, "keys")); err != nil {
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
	core, err := signer.NewCore(ctx, cfg, keys, filepath.Join(dir, "signer.db"), clock, signer.Unavailable{})
	return core, cfg, err
}

// minPassphrase is the shortest passphrase keygen accepts.
const minPassphrase = 12

// keygen writes one new key file DIR/keys/<walletID>.key: a fresh random
// key, encrypted with a passphrase read twice (they must match). An existing
// file is never replaced. It prints the wallet ID and the address only.
func keygen(dir, walletID string, passphrase func() ([]byte, error), out io.Writer) error {
	if err := private(dir); err != nil {
		return err
	}
	keys := filepath.Join(dir, "keys")
	if err := private(keys); err != nil {
		return err
	}
	if !signer.ValidWalletID(walletID) {
		return fmt.Errorf("wallet ID %q is not valid", walletID)
	}
	path := filepath.Join(keys, walletID+".key")
	if _, err := os.Lstat(path); !errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("%s exists or cannot be checked; nothing was written", path)
	}
	pass, err := passphrase()
	if err != nil {
		return err
	}
	defer clear(pass)
	again, err := passphrase()
	if err != nil {
		return err
	}
	defer clear(again)
	switch {
	case !bytes.Equal(pass, again):
		return errors.New("the passphrases differ; nothing was written")
	case len(pass) < minPassphrase:
		return fmt.Errorf("the passphrase must be at least %d characters; nothing was written", minPassphrase)
	}
	key, err := secp256k1.GeneratePrivateKey()
	if err != nil {
		return err
	}
	defer key.Zero()
	priv := key.Serialize()
	defer clear(priv)
	f, err := signer.EncryptKey(walletID, priv, pass, signer.ScryptN)
	if err != nil {
		return err
	}
	if k, err := signer.DecryptKey(f, walletID, pass); err != nil || signer.Address(k.PubKey()) != f.Address {
		return errors.New("the new key file does not decrypt to its address; nothing was written")
	}
	raw, err := json.Marshal(f)
	if err != nil {
		return err
	}
	file, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
	if err != nil {
		return err
	}
	if _, err := file.Write(raw); err != nil {
		file.Close()
		os.Remove(path)
		return err
	}
	if err := errors.Join(file.Sync(), file.Close()); err != nil {
		os.Remove(path)
		return err
	}
	fmt.Fprintf(out, "apa-signer: wallet %s address %s\n", walletID, f.Address)
	return nil
}

// private refuses a directory that is not this user's alone.
func private(dir string) error {
	fi, err := os.Lstat(dir)
	if err != nil {
		return err
	}
	st, ok := fi.Sys().(*syscall.Stat_t)
	if !fi.IsDir() || fi.Mode().Perm()&0o077 != 0 || !ok || st.Uid != uint32(os.Getuid()) {
		return fmt.Errorf("%s must be a directory owned by this user, mode 0700", dir)
	}
	return nil
}

// readPassphrase reads one line from the terminal with echo off.
func readPassphrase(tty *os.File, prompt string) ([]byte, error) {
	fmt.Fprint(tty, "apa-signer "+prompt)
	fd := int(tty.Fd())
	old, err := unix.IoctlGetTermios(fd, getTermios)
	if err != nil {
		return nil, err
	}
	quiet := *old
	quiet.Lflag &^= unix.ECHO
	if err := unix.IoctlSetTermios(fd, setTermios, &quiet); err != nil {
		return nil, err
	}
	defer func() { unix.IoctlSetTermios(fd, setTermios, old); fmt.Fprintln(tty) }()
	var line []byte
	b := make([]byte, 1)
	for len(line) < 1024 {
		if n, err := tty.Read(b); err != nil || n == 0 {
			return nil, errors.New("no passphrase")
		}
		if b[0] == '\n' || b[0] == '\r' {
			return line, nil
		}
		line = append(line, b[0])
	}
	return nil, errors.New("passphrase too long")
}
