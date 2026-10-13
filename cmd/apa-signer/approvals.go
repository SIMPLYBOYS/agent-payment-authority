package main

// A2's commands (docs/commercial-human-approval-design.md 3.2, 6.4):
//
//	apa-signer serve-approvals --dir DIR
//	apa-signer approver enroll begin --dir DIR --approver ID
//	apa-signer approver enroll confirm --dir DIR --id ENROLLMENT
//	apa-signer approver revoke --dir DIR --credential FINGERPRINT
//	apa-signer approver list --dir DIR
//
// serve-approvals reads signer.json only for the wallets' public
// information, the approvers and the relying party; it never opens keys/
// or signer.db, asks for no passphrase and has no payment route. Its data
// is DIR/approvals.db (0600). The administrator's commands open the same
// file and may run while it serves.

import (
	"bufio"
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"net"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	"github.com/SIMPLYBOYS/agent-payment-authority/internal/signer"
)

// approvalsDB opens (or creates, 0600) DIR/approvals.db for an
// ApprovalCore; nothing else in DIR is touched.
func assembleApprovals(dir string, clock signer.Clock) (*signer.ApprovalCore, *signer.Config, error) {
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
	path := filepath.Join(dir, "approvals.db")
	switch fi, err := os.Lstat(path); {
	case errors.Is(err, os.ErrNotExist):
		f, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
		if err != nil {
			return nil, nil, err
		}
		f.Close()
	case err != nil:
		return nil, nil, err
	case !fi.Mode().IsRegular() || fi.Mode().Perm()&0o077 != 0:
		return nil, nil, fmt.Errorf("%s must be a regular file private to its owner (0600)", path)
	}
	core, err := signer.NewApprovalCore(context.Background(), cfg, path, clock)
	return core, cfg, err
}

func unixClock() uint64 { return uint64(time.Now().Unix()) }

func serveApprovals(dir string, out io.Writer) error {
	core, cfg, err := assembleApprovals(dir, unixClock)
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
	fmt.Fprintf(out, "apa-signer: serving approvals only on %s (payment unavailable: no key is read, nothing is signed; origins %s)\n",
		cfg.Socket, strings.Join(cfg.WebAuthn.Origins, " "))
	return signer.ServeApprovals(ctx, ln, core, func(line string) { fmt.Fprintln(out, "apa-signer:", line) })
}

// approver runs the administrator's commands; ask reads the answer to the
// confirm question (the terminal in production).
func approver(args []string, out io.Writer, ask func(prompt string) (string, error)) error {
	usage := errors.New("usage: apa-signer approver enroll begin --dir DIR --approver ID | enroll confirm --dir DIR --id ENROLLMENT | revoke --dir DIR --credential FINGERPRINT | list --dir DIR")
	if len(args) == 0 {
		return usage
	}
	cmd := args[0]
	rest := args[1:]
	if cmd == "enroll" {
		if len(rest) == 0 || rest[0] != "begin" && rest[0] != "confirm" {
			return usage
		}
		cmd, rest = "enroll "+rest[0], rest[1:]
	}
	fs := flag.NewFlagSet(cmd, flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	dir := fs.String("dir", "", "the Signer's private directory")
	id := fs.String("approver", "", "")
	enr := fs.String("id", "", "")
	cred := fs.String("credential", "", "")
	if err := fs.Parse(rest); err != nil || *dir == "" || fs.NArg() != 0 {
		return usage
	}
	need := map[string]*string{"enroll begin": id, "enroll confirm": enr, "revoke": cred}[cmd]
	if cmd != "list" && (need == nil || *need == "") {
		return usage
	}
	core, cfg, err := assembleApprovals(*dir, unixClock)
	if err != nil {
		return err
	}
	defer core.Close()
	ctx := context.Background()
	switch cmd {
	case "enroll begin":
		eid, code, expires, err := core.EnrollBegin(ctx, *id)
		if err != nil {
			return err
		}
		fmt.Fprintf(out, "apa-signer: enrollment %s for %s; one-time code %s (valid until %s, %d minutes)\n", eid, *id, code,
			time.Unix(int64(expires), 0).Format(time.TimeOnly), int((time.Duration(expires-unixClock()) * time.Second).Minutes()))
		fmt.Fprintf(out, "apa-signer: open %s/approve/enroll, type the enrollment ID and the code, use the device; then run: apa-signer approver enroll confirm --dir %s --id %s\n",
			cfg.WebAuthn.Origins[0], *dir, eid)
		return nil
	case "enroll confirm":
		e, err := core.EnrollPending(ctx, *enr)
		if err != nil {
			return err
		}
		fmt.Fprintf(out, "apa-signer: enrollment %s of %s: credential %s, flags %s, AAGUID %s\n", e.EnrollmentID, e.ApproverID, e.Fingerprint, e.Flags, e.AAGUID)
		fmt.Fprintln(out, "apa-signer: the page shows the same credential fingerprint? (during enrollment the Host and its page are trusted: decision D2)")
		answer, err := ask("type yes to activate it: ")
		if err != nil {
			return err
		}
		if strings.TrimSpace(answer) != "yes" {
			return errors.New("not confirmed; the credential stays pending and cannot approve")
		}
		if err := core.EnrollConfirm(ctx, *enr); err != nil {
			return err
		}
		fmt.Fprintf(out, "apa-signer: credential %s of %s is active\n", e.Fingerprint, e.ApproverID)
		return nil
	case "revoke":
		if err := core.Revoke(ctx, *cred); err != nil {
			return err
		}
		fmt.Fprintf(out, "apa-signer: credential %s revoked\n", *cred)
		return nil
	default:
		list, err := core.Credentials(ctx)
		if err != nil {
			return err
		}
		for _, c := range list {
			fmt.Fprintf(out, "%s %s %s %s AAGUID %s created %s\n", c.ApproverID, c.Fingerprint, c.State, c.Flags, c.AAGUID,
				time.Unix(int64(c.CreatedAt), 0).UTC().Format(time.RFC3339))
		}
		if len(list) == 0 {
			fmt.Fprintln(out, "apa-signer: no credential")
		}
		return nil
	}
}

// askTTY asks on the terminal.
func askTTY(prompt string) (string, error) {
	tty, err := os.OpenFile("/dev/tty", os.O_RDWR, 0)
	if err != nil {
		return "", errors.New("the confirmation is read from a terminal (/dev/tty), and there is none")
	}
	defer tty.Close()
	fmt.Fprint(tty, "apa-signer "+prompt)
	line, err := bufio.NewReader(io.LimitReader(tty, 64)).ReadString('\n')
	if err != nil && line == "" {
		return "", errors.New("no answer")
	}
	return line, nil
}
