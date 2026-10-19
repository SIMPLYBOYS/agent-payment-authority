// Command apa-relayer holds the Host's own test keys on Base Sepolia
// (docs/commercial-p2b.md B1, P2b-D3, D4): the budget relayer's gas,
// issuer and policy keys, and the shop's gas key, each in an encrypted key
// file under DIR/keys made here.
//
//	apa-relayer keygen --dir DIR --role relayer-gas|issuer|policy|shop
//
// keygen makes one new key file; the passphrase is read twice from this
// terminal, never from a flag, a variable or a file. It prints the role and
// the address only.
package main

import (
	"errors"
	"flag"
	"fmt"
	"io"
	"os"

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
	usage := errors.New("usage: apa-relayer keygen --dir DIR --role relayer-gas|issuer|policy|shop")
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
	label, ok := keytool.Roles[*role]
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
