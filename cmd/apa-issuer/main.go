// Command apa-issuer is C2's test issuer (docs/commercial-c2-design.md 5,
// 6): it makes a credential directory of synthetic test keys and signs its
// status list. It holds no real identity, issuer or wallet.
//
//	apa-issuer init --dir D --issuer ID --consumers a,b   new keys, holders and status list version 1
//	apa-issuer revoke --dir D --task T                    revoke task T's issued credential
//	apa-issuer revoke --dir D --status N                  revoke status index N
//	apa-issuer refresh --dir D                            re-sign the list (a running Host does this every 300 s)
//	apa-issuer subjects --dir D --subjects 'consumer-a:buyer_type=business,country=TW,age_over_18=true;consumer-b:…'
//	                                                      the consumers' attributes: the directory then issues apa.credential.v2
//	                                                      (docs/commercial-venues.md 3.1; replaces subjects.json whole)
//	apa-issuer ekyc --dir D --consumer C --key-file F [--dependents C2,…] [--session ID | --scenario S] [--wait SECONDS]
//	                                                      C's country and age from an identity verification at Didit
//	                                                      (docs/commercial-ekyc.md); the dependents as C's children under 18
//
// Every write goes through the directory's lock: a revocation is never
// overwritten. Nothing secret is printed.
package main

import (
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"strings"
	"time"

	"github.com/SIMPLYBOYS/agent-payment-authority/internal/credential"
)

func main() {
	if err := run(os.Args[1:], os.Stdout, func() int64 { return time.Now().Unix() }); err != nil {
		fmt.Fprintln(os.Stderr, "apa-issuer:", err)
		os.Exit(1)
	}
}

const usage = "usage: apa-issuer init|revoke|refresh|subjects|ekyc --dir D ..."

func run(args []string, out io.Writer, now func() int64) error {
	if len(args) == 0 {
		return errors.New(usage)
	}
	fs := flag.NewFlagSet("apa-issuer "+args[0], flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	dir := fs.String("dir", "", "the credential directory")
	issuer := fs.String("issuer", "", "init: the issuer ID")
	consumers := fs.String("consumers", "", "init: the consumers given a holder, comma-separated")
	task := fs.String("task", "", "revoke: the task whose credential is revoked")
	status := fs.String("status", "", "revoke: the status index revoked")
	subjects := fs.String("subjects", "", "subjects: consumer:name=value,… for each consumer, ';'-separated")
	var e ekycOptions
	var dependents string
	var wait int
	fs.StringVar(&e.consumer, "consumer", "", "ekyc: the consumer verified")
	fs.StringVar(&e.keyFile, "key-file", "", "ekyc: the file (0600) holding Didit's API key")
	fs.StringVar(&dependents, "dependents", "", "ekyc: the consumers the verified one declares as its children under 18, comma-separated")
	fs.StringVar(&e.buyerType, "buyer-type", "individual", "ekyc: the verified consumer's buyer_type (no verification shows it)")
	fs.StringVar(&e.session, "session", "", "ekyc: a session already made, instead of a new one")
	fs.StringVar(&e.scenario, "scenario", "", "ekyc: a new sandbox session's scenario (approve, decline_document_expired, …)")
	fs.StringVar(&e.workflow, "workflow", "", "ekyc: a new session's workflow (default: the application's default KYC workflow)")
	fs.StringVar(&e.url, "ekyc-url", "https://verification.didit.me", "ekyc: Didit's API")
	fs.IntVar(&wait, "wait", 600, "ekyc: seconds to wait for the decision")
	if err := fs.Parse(args[1:]); err != nil || fs.NArg() != 0 || *dir == "" {
		return errors.New(usage)
	}
	if args[0] == "init" {
		if *issuer == "" || *consumers == "" {
			return errors.New("init needs --issuer and --consumers")
		}
		if err := credential.InitDir(*dir, *issuer, strings.Split(*consumers, ","), now()); err != nil {
			return err
		}
		fmt.Fprintf(out, "apa-issuer: %s initialised: issuer %s, holders for %s, status list version 1 (test keys only)\n", *dir, *issuer, *consumers)
		return nil
	}
	d, err := credential.OpenDir(*dir)
	if err != nil {
		return err
	}
	switch args[0] {
	case "ekyc":
		if dependents != "" {
			e.dependents = strings.Split(dependents, ",")
		}
		e.wait = time.Duration(wait) * time.Second
		return ekyc(d, e, out, now)
	case "subjects":
		m := map[string]map[string]string{}
		for _, one := range strings.Split(*subjects, ";") {
			c, attrs, ok := strings.Cut(one, ":")
			if !ok || m[c] != nil {
				return errors.New("subjects: consumer:name=value,… for each consumer, ';'-separated, each once")
			}
			m[c] = map[string]string{}
			for _, kv := range strings.Split(attrs, ",") {
				k, v, _ := strings.Cut(kv, "=")
				m[c][k] = v
			}
		}
		if err := d.SetSubjects(m); err != nil {
			return err
		}
		fmt.Fprintf(out, "apa-issuer: subjects.json written for %d consumer(s); new tasks get apa.credential.v2\n", len(m))
		return nil
	case "refresh":
		if err := d.Refresh(now()); err != nil {
			return err
		}
		fmt.Fprintln(out, "apa-issuer: status list re-signed")
		return nil
	case "revoke":
		index := *status
		if (*task == "") == (index == "") {
			return errors.New("revoke needs exactly one of --task and --status")
		}
		if *task != "" {
			s, err := d.ReadCredential(*task)
			if err != nil {
				return err
			}
			index = s.Credential["status"]
		}
		if err := d.Revoke(index, now()); err != nil {
			return err
		}
		fmt.Fprintf(out, "apa-issuer: status index %s revoked\n", index)
		return nil
	}
	return errors.New(usage)
}
