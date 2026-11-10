// Command e1-evidence makes E1-a's evidence pack (docs/commercial-e1a.md
// 4.5, 4.7; internal/e1evidence):
//
//	e1-evidence start --rpc URL --host HOSTDIR --out START.json
//	e1-evidence pack --db HOST.db --start START.json --task TASK=ENTRY[=AE1CASE]... \
//	    [--traces RUNS] [--reconciliation REPORT.json] [--ekyc EKYC.json] [--to-block N] [--commit SHA] --out artifacts/e1a/<new>
//
// start, before the runs, reads the chain only: the next block, the
// verifier, and that no task of the host directory's registry is
// registered yet. pack, after them, reads the Host's Store database read
// only, copies each live run's trace and the reconciliation report, and
// indexes every file. Neither decides anything: verify-demo --mode e1a does.
package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"strings"
	"time"

	"github.com/SIMPLYBOYS/agent-payment-authority/internal/e1evidence"
	"github.com/SIMPLYBOYS/agent-payment-authority/internal/eth"
	"github.com/SIMPLYBOYS/agent-payment-authority/internal/verification"
)

func main() {
	if err := run(os.Args[1:], os.Stdout); err != nil {
		fmt.Fprintln(os.Stderr, "e1-evidence:", err)
		os.Exit(1)
	}
}

type taskFlags []verification.E1aTask

func (t *taskFlags) String() string { return fmt.Sprint(*t) }
func (t *taskFlags) Set(v string) error {
	p := strings.Split(v, "=")
	if len(p) < 2 || len(p) > 3 || p[0] == "" || p[1] == "" {
		return errors.New("TASK=ENTRY or TASK=ENTRY=AE1CASE")
	}
	x := verification.E1aTask{TaskID: p[0], Scenario: p[1]}
	if len(p) == 3 {
		x.InjectionCase = p[2]
	}
	*t = append(*t, x)
	return nil
}

func run(args []string, out io.Writer) error {
	usage := errors.New("usage: e1-evidence start --rpc URL --host HOSTDIR --out START.json | pack --db HOST.db --start START.json --task TASK=ENTRY[=AE1CASE]... [--traces DIR] [--reconciliation FILE] [--commit SHA] --out DIR")
	if len(args) == 0 {
		return usage
	}
	fs := flag.NewFlagSet(args[0], flag.ContinueOnError)
	dst := fs.String("out", "", "the file (start) or new directory (pack) to write")
	switch args[0] {
	case "start":
		rpc := fs.String("rpc", "", "the chain's node (read only)")
		host := fs.String("host", "", "the host directory (p2-testnet e1a-host's)")
		if err := fs.Parse(args[1:]); err != nil || fs.NArg() != 0 || *rpc == "" || *host == "" || *dst == "" {
			return usage
		}
		if _, err := os.Lstat(*dst); err == nil {
			return fmt.Errorf("%s exists: a start is recorded once", *dst)
		}
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
		defer cancel()
		s, err := e1evidence.Start(ctx, eth.NewClient(*rpc), *host)
		if err != nil {
			return err
		}
		raw, _ := json.MarshalIndent(s, "", "  ")
		if err := os.WriteFile(*dst, append(raw, '\n'), 0o600); err != nil {
			return err
		}
		via := "the Host's shop itself"
		if s.Facilitator != "" {
			via = "the facilitator " + s.Facilitator
		}
		fmt.Fprintf(out, "e1-evidence: start recorded in %s: %s chain %d from block %d, %d wallets, settled by %s; no task of the registry is registered yet\n",
			*dst, s.Chain, s.ChainID, s.FromBlock, len(s.Wallets), via)
		return nil
	case "pack":
		var in e1evidence.PackInput
		fs.StringVar(&in.DB, "db", "", "the Host's Store database (read only)")
		start := fs.String("start", "", "the start recorded before the runs")
		fs.StringVar(&in.Traces, "traces", "", "the Host's trace directory (live runs)")
		fs.StringVar(&in.Reconciliation, "reconciliation", "", "the Host's reconciliation report")
		fs.StringVar(&in.Commit, "commit", "", "the code the runs ran")
		fs.StringVar(&in.EKYC, "ekyc", "", "the credential directory's ekyc.json (on record only)")
		toBlock := fs.Uint64("to-block", 0, "the last block of the runs (E8 counts to it); default the reconciliation report's")
		var tasks taskFlags
		fs.Var(&tasks, "task", "TASK=ENTRY[=AE1CASE], once per run")
		if err := fs.Parse(args[1:]); err != nil || fs.NArg() != 0 || in.DB == "" || *start == "" || *dst == "" || len(tasks) == 0 {
			return usage
		}
		raw, err := os.ReadFile(*start)
		if err != nil || json.Unmarshal(raw, &in.Start) != nil {
			return fmt.Errorf("--start %s is not a recorded start (%v)", *start, err)
		}
		in.Tasks = tasks
		if *toBlock != 0 {
			in.Start.ToBlock = *toBlock
		}
		id, err := e1evidence.Pack(in, *dst)
		if err != nil {
			return err
		}
		fmt.Fprintf(out, "e1-evidence: %s packed in %s; check it with go run ./cmd/verify-demo --mode e1a --run-dir %s --rpc <the node>\n", id, *dst, *dst)
		return nil
	}
	return usage
}

// readJSON is for this command's tests.
func readJSON(path string, v any) error {
	raw, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	return json.Unmarshal(raw, v)
}
