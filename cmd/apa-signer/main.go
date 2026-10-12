// Command apa-signer is the independent Signer of P1-b
// (docs/commercial-p1b-design.md): it holds the payment keys, checks every
// request against its own configuration and signs one EIP-3009
// authorization per accepted attempt.
//
//	apa-signer serve --dir DIR
//
// DIR (private to its owner) holds signer.json, keys/<wallet_id>.key and
// signer.db; the passphrase is read from this terminal. It is meant to run
// as its own OS user (apa-signer); creating that user is a separate,
// not yet approved step. Until P2 its reservation verifier refuses every
// order, so it signs nothing; no flag or variable changes that.
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
	"syscall"
	"time"

	"github.com/SIMPLYBOYS/agent-payment-authority/internal/signer"
	"golang.org/x/sys/unix"
)

func main() {
	if err := run(os.Args[1:], os.Stderr); err != nil {
		fmt.Fprintln(os.Stderr, "apa-signer:", err)
		os.Exit(1)
	}
}

func run(args []string, out io.Writer) error {
	if len(args) == 0 || args[0] != "serve" {
		return errors.New("usage: apa-signer serve --dir DIR")
	}
	fs := flag.NewFlagSet("serve", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	dir := fs.String("dir", "", "the Signer's private directory")
	if err := fs.Parse(args[1:]); err != nil || *dir == "" || fs.NArg() != 0 {
		return errors.New("usage: apa-signer serve --dir DIR")
	}
	tty, err := os.OpenFile("/dev/tty", os.O_RDWR, 0)
	if err != nil {
		return errors.New("the passphrase is read from a terminal (/dev/tty), and there is none")
	}
	defer tty.Close()
	core, cfg, err := assemble(*dir, func() ([]byte, error) { return readPassphrase(tty) }, func() uint64 { return uint64(time.Now().Unix()) })
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
func readPassphrase(tty *os.File) ([]byte, error) {
	fmt.Fprint(tty, "apa-signer passphrase: ")
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
