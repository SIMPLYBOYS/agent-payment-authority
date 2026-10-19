package main

// A2's commands (docs/commercial-human-approval-design.md 3.2, 6.4):
//
//	apa-signer serve-approvals --dir DIR
//	apa-signer approver enroll begin --dir DIR --approver ID
//	apa-signer approver enroll confirm --dir DIR --id ENROLLMENT
//	apa-signer approver revoke --dir DIR --credential FINGERPRINT
//	apa-signer approver list --dir DIR
//	apa-signer approver voice-enroll --dir DIR --approver ID WAV WAV WAV...  (V1: at least three recordings)
//	apa-signer approver voice-delete --dir DIR --approver ID
//	apa-signer approver voice-stats --dir DIR  (V1: stored voice outcomes by result and reason)
//	apa-signer approver migrate --dir DIR  (P2-D1: once, A2's approvals.db into signer.db)
//
// serve-approvals reads signer.json only for the wallets' public
// information, the approvers and the relying party; it never opens keys/,
// asks for no passphrase and has no payment route. Its data is the
// approval tables of DIR/signer.db (0600; P2-D1: before P2 they were
// DIR/approvals.db, which every command but migrate now refuses). The
// administrator's commands open the same file and may run while it serves.

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
	"sort"
	"strings"
	"syscall"
	"time"

	"github.com/SIMPLYBOYS/agent-payment-authority/internal/keytool"
	"github.com/SIMPLYBOYS/agent-payment-authority/internal/signer"
)

// assembleApprovals opens (or creates, 0600) DIR/signer.db for an
// ApprovalCore; nothing else in DIR is touched.
func assembleApprovals(dir string, clock signer.Clock) (*signer.ApprovalCore, *signer.Config, error) {
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
	path := filepath.Join(dir, "signer.db")
	if err := privateFile(path); err != nil {
		return nil, nil, err
	}
	core, err := signer.NewApprovalCore(context.Background(), cfg, path, clock)
	return core, cfg, err
}

// privateFile creates path empty (0600) when missing, and refuses one
// that is not a regular file private to its owner.
func privateFile(path string) error {
	switch fi, err := os.Lstat(path); {
	case errors.Is(err, os.ErrNotExist):
		f, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
		if err != nil {
			return err
		}
		return f.Close()
	case err != nil:
		return err
	case !fi.Mode().IsRegular() || fi.Mode().Perm()&0o077 != 0:
		return fmt.Errorf("%s must be a regular file private to its owner (0600)", path)
	}
	return nil
}

// migrate moves DIR/approvals.db into DIR/signer.db (P2-D1), once, with
// no Signer serving (its socket is there while one does).
func migrate(dir string, out io.Writer) error {
	if err := keytool.Private(dir); err != nil {
		return err
	}
	raw, err := os.ReadFile(filepath.Join(dir, "signer.json"))
	if err != nil {
		return err
	}
	cfg, err := signer.ParseConfig(raw)
	if err != nil {
		return err
	}
	if _, err := os.Lstat(cfg.Socket); err == nil {
		return fmt.Errorf("%s exists: stop the Signer first (remove it yourself if none is serving)", cfg.Socket)
	}
	if err := privateFile(filepath.Join(dir, "signer.db")); err != nil {
		return err
	}
	moved, err := signer.MigrateApprovals(context.Background(), dir)
	if err != nil {
		return err
	}
	tables := make([]string, 0, len(moved))
	for t := range moved {
		tables = append(tables, t)
	}
	sort.Strings(tables)
	for _, t := range tables {
		fmt.Fprintf(out, "apa-signer: %s %d rows\n", t, moved[t])
	}
	fmt.Fprintf(out, "apa-signer: approvals moved into %s; the old file is %s\n", filepath.Join(dir, "signer.db"), filepath.Join(dir, "approvals.db.migrated"))
	return nil
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
	usage := errors.New("usage: apa-signer approver enroll begin --dir DIR --approver ID | enroll confirm --dir DIR --id ENROLLMENT | revoke --dir DIR --credential FINGERPRINT | list --dir DIR | voice-enroll --dir DIR --approver ID WAV... | voice-delete --dir DIR --approver ID | voice-stats --dir DIR | migrate --dir DIR")
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
	if err := fs.Parse(rest); err != nil || *dir == "" || (fs.NArg() != 0) != (cmd == "voice-enroll") {
		return usage
	}
	need := map[string]*string{"enroll begin": id, "enroll confirm": enr, "revoke": cred, "voice-enroll": id, "voice-delete": id}[cmd]
	if cmd != "list" && cmd != "voice-stats" && cmd != "migrate" && (need == nil || *need == "") {
		return usage
	}
	if cmd == "migrate" {
		return migrate(*dir, out)
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
	case "voice-enroll":
		// The recordings are read here and given to the sidecar; nothing of them is written.
		var wavs [][]byte
		for _, p := range fs.Args() {
			b, err := os.ReadFile(p)
			if err != nil {
				return err
			}
			wavs = append(wavs, b)
		}
		n, err := core.EnrollVoice(ctx, *id, wavs)
		if err != nil {
			return err
		}
		fmt.Fprintf(out, "apa-signer: voice template of %s from %d recordings (sealed in signer.db; key %s)\n", *id, n, filepath.Join(*dir, "voice.key"))
		return nil
	case "voice-stats":
		st, err := core.VoiceStats(ctx)
		if err != nil {
			return err
		}
		total, degraded := 0, 0
		for k, n := range st {
			total += n
			if strings.HasPrefix(k, "unavailable") {
				degraded += n
			}
		}
		keys := make([]string, 0, len(st))
		for k := range st {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		for _, k := range keys {
			fmt.Fprintf(out, "%s %d\n", k, st[k])
		}
		if total > 0 {
			fmt.Fprintf(out, "apa-signer: %d voice outcomes; degraded (unavailable) %d, %.1f%%\n", total, degraded, 100*float64(degraded)/float64(total))
		} else {
			fmt.Fprintln(out, "apa-signer: no voice outcome yet")
		}
		return nil
	case "voice-delete":
		ok, err := core.DeleteVoice(ctx, *id)
		if err != nil {
			return err
		}
		if !ok {
			return fmt.Errorf("%s has no voice template", *id)
		}
		fmt.Fprintf(out, "apa-signer: voice template of %s deleted\n", *id)
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
