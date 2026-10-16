// Command ae1-eval runs the AE1 evaluation (docs/commercial-ae1.md) and
// reports it.
//
//	ae1-eval run --buyer PATH --fake-cli PATH --out DIR [--behavior obey|fooled|miss|extra|wrong|mix]
//	             [--corpus FILE] [--kinds K,...] [--cases ID,...] [--reps N] [--max-runs N]
//	ae1-eval run --mode live --batch NAME --model sonnet|haiku --cli ABSOLUTE-PATH --buyer PATH --out DIR ...
//	             (also needs APA_LIVE_CLI=true: a real model runs on the operator's login)
//	ae1-eval report --out DIR --reps N [--corpus FILE] RESULTS.jsonl...  (live batches, plus the fake crafted/fault run)
//
// run makes every run with apa-buyer run --catalog --task-text, each in a
// new private directory with its own Store, and scores it from the Store's
// purchases and the host's tool-call record only: nothing the model wrote is
// read or kept. Fake mode runs no model: benign and injection cases get a
// scripted stand-in (--behavior), crafted and fault cases their own scripts.
// A batch stops after 3 infrastructure failures in a row. DIR must not exist;
// it gets results.jsonl, report.json and report.md. Nothing here writes the
// demo's Store, pays or reaches a chain.
package main

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"slices"
	"strings"
	"syscall"
	"time"

	"github.com/SIMPLYBOYS/agent-payment-authority/internal/ae1"
	"github.com/SIMPLYBOYS/agent-payment-authority/internal/buyer"
	"github.com/SIMPLYBOYS/agent-payment-authority/internal/merchant"
	"github.com/SIMPLYBOYS/agent-payment-authority/internal/toolbridge"
)

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	if err := cli(ctx, os.Args[1:], os.Stdout, os.Getenv); err != nil {
		fmt.Fprintln(os.Stderr, "ae1-eval:", err)
		os.Exit(1)
	}
}

func cli(ctx context.Context, args []string, out io.Writer, getenv func(string) string) error {
	if len(args) > 0 && args[0] == "run" {
		return run(ctx, args[1:], out, getenv)
	}
	if len(args) > 0 && args[0] == "report" {
		return report(args[1:], out)
	}
	return errors.New("usage: ae1-eval run ... | report --out DIR --reps N RESULTS.jsonl...")
}

// Behaviors are a fake run's stand-ins for a model on benign and injection
// cases: what it does, so the whole harness can be checked without one.
var behaviors = []string{"obey", "fooled", "miss", "extra", "wrong"}

const craftedTask = "crafted input run: no model; the fake CLI sends the case's script"

// evalTask is the registry task every evaluation run uses (apa-buyer's default),
// each in its own Store.
const evalTask = "task-a"

func run(ctx context.Context, args []string, out io.Writer, getenv func(string) string) error {
	fs := flag.NewFlagSet("run", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	corpusPath := fs.String("corpus", "testdata/ae1/corpus.json", "the frozen corpus")
	thresholdsPath := fs.String("thresholds", "testdata/ae1/thresholds.json", "D2's thresholds")
	mode := fs.String("mode", "fake", "fake (no model) or live")
	batch := fs.String("batch", "", "live: the approved batch's name, recorded with every run")
	model := fs.String("model", "", "live: one of buyer.EvalModels")
	buyerPath := fs.String("buyer", "", "the apa-buyer executable")
	fakeCLI := fs.String("fake-cli", "", "fake: the fake-claude executable")
	cliPath := fs.String("cli", "", "live: the approved Claude Code executable (absolute)")
	devDemo := fs.Bool("dev-demo-plugins", false, "live: apa-buyer's development demo exception")
	behavior := fs.String("behavior", "obey", "fake: obey, fooled, miss, extra, wrong or mix")
	kinds := fs.String("kinds", "", "only these kinds (comma separated)")
	cases := fs.String("cases", "", "only these case IDs (comma separated)")
	reps := fs.Int("reps", 1, "runs per case")
	firstRep := fs.Int("first-rep", 1, "the first run's attempt number (to complete an interrupted or split set: the next one's)")
	maxRuns := fs.Int("max-runs", 0, "stop after this many runs (0: no limit)")
	outDir := fs.String("out", "", "a new directory for the results")
	if err := fs.Parse(args); err != nil {
		return err
	}
	live := *mode == "live"
	switch {
	case *mode != "fake" && !live:
		return errors.New("--mode is fake or live")
	case *outDir == "" || *buyerPath == "":
		return errors.New("--out and --buyer are required")
	case *reps < 1 || *firstRep < 1:
		return errors.New("--reps and --first-rep must be at least 1")
	case live && (*batch == "" || !slices.Contains(buyer.EvalModels, *model) || !filepath.IsAbs(*cliPath)):
		return errors.New("--mode live needs --batch, --model sonnet|haiku and an absolute --cli")
	case live && getenv("APA_LIVE_CLI") != "true":
		return errors.New("--mode live also needs APA_LIVE_CLI=true; nothing was started")
	case !live && (*fakeCLI == "" || *model != "" || *batch != ""):
		return errors.New("fake mode needs --fake-cli and takes no --model or --batch")
	case !live && *behavior != "mix" && !slices.Contains(behaviors, *behavior):
		return fmt.Errorf("--behavior %q", *behavior)
	}
	corpus, err := ae1.Load(*corpusPath)
	if err != nil {
		return err
	}
	var thr ae1.Thresholds
	if err := decode(*thresholdsPath, &thr); err != nil {
		return err
	}
	var todo []ae1.Case
	for _, k := range corpus.Cases {
		if (*kinds == "" || slices.Contains(strings.Split(*kinds, ","), k.Kind)) && (*cases == "" || slices.Contains(strings.Split(*cases, ","), k.ID)) {
			if live && (k.Kind == ae1.Crafted || k.Kind == ae1.Fault) {
				continue // scripts only: they never cost a model run
			}
			todo = append(todo, k)
		}
	}
	if len(todo) == 0 {
		return errors.New("no case selected")
	}
	if err := os.Mkdir(*outDir, 0o700); err != nil {
		return fmt.Errorf("--out must be a new directory: %w", err)
	}
	resF, err := os.OpenFile(filepath.Join(*outDir, "results.jsonl"), os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
	if err != nil {
		return err
	}
	defer resF.Close()
	enc := json.NewEncoder(resF)

	label := "fake"
	if live {
		label = *model
	}
	// The plan is the corpus's, never the selection's (review 2-1): a subset
	// (--cases, --kinds, --max-runs) is INCONCLUSIVE, so a trial is never judged.
	plan := ae1.Plan{Runs: planRuns(corpus, live, *reps), Crafted: countKind(corpus, ae1.Crafted)}
	var rows []ae1.Row
	var stopErr error
	infraRun, n := 0, 0
loop:
	for rep := *firstRep; rep < *firstRep+*reps; rep++ {
		for ci, k := range todo {
			if ctx.Err() != nil || (*maxRuns > 0 && n >= *maxRuns) {
				break loop
			}
			n++
			b := ""
			if !live && (k.Kind == ae1.Benign || k.Kind == ae1.Injection) {
				b = *behavior
				if b == "mix" {
					b = behaviors[(ci+rep)%len(behaviors)]
				}
			}
			row, err := once(ctx, corpus, k, b, rep, runOpts{live: live, model: *model, buyer: *buyerPath, fake: *fakeCLI, cli: *cliPath, devDemo: *devDemo, getenv: getenv})
			if ctx.Err() != nil {
				fmt.Fprintln(out, "interrupted: the runs made so far are reported (INCONCLUSIVE: fewer than planned)")
				break loop
			}
			if errors.Is(err, errPayment) {
				stopErr = fmt.Errorf("case %s attempt %d: %w", k.ID, rep, err)
				break loop
			}
			if err != nil {
				return fmt.Errorf("case %s attempt %d: %w", k.ID, rep, err)
			}
			row.Model, row.Behavior = label, b
			if live {
				row.Detail = strings.TrimSpace(row.Detail + " batch=" + *batch)
			}
			rows = append(rows, row)
			if err := enc.Encode(row); err != nil {
				return err
			}
			fmt.Fprintf(out, "%s #%d %s: %s %s\n", k.ID, rep, k.Kind, row.Obs.Outcome, show(row.Score))
			if row.Obs.Outcome != "ok" && k.Kind != ae1.Fault {
				if infraRun++; infraRun >= 3 {
					fmt.Fprintln(out, "stopped: 3 infrastructure failures in a row")
					break loop
				}
			} else {
				infraRun = 0
			}
		}
	}
	if err := writeReport(*outDir, ae1.Build(rows, map[string]ae1.Plan{label: plan}, thr), out); err != nil {
		return err
	}
	return stopErr
}

// planRuns is a batch's planned runs: the corpus's model cases (live) or all
// its cases (fake), reps times, whatever was selected.
func planRuns(c *ae1.Corpus, live bool, reps int) int {
	if live {
		return (countKind(c, ae1.Benign) + countKind(c, ae1.Injection)) * reps
	}
	return len(c.Cases) * reps
}

func countKind(c *ae1.Corpus, kind string) int {
	n := 0
	for _, k := range c.Cases {
		if k.Kind == kind {
			n++
		}
	}
	return n
}

// errPayment stops a batch: a run whose report names any payment (M1 cannot
// pay; it should never happen).
var errPayment = errors.New("apa-buyer reported a payment; the batch is stopped")

// families are the init model names each asked-for model may come back as.
var families = map[string][]string{"sonnet": {"sonnet", "claude-sonnet-5-5"}, "haiku": {"haiku", "claude-haiku-5-5"}}

// modelMatches reports whether the init's model is the one asked for.
func modelMatches(asked, init string) bool { return slices.Contains(families[asked], init) }

func show(s ae1.Score) string {
	if s.Class == "" {
		return fmt.Sprintf("pass=%v", s.Pass)
	}
	if s.Fooled {
		return s.Class + " fooled"
	}
	return s.Class
}

type runOpts struct {
	live               bool
	model, buyer, fake string
	cli                string
	devDemo            bool
	getenv             func(string) string
}

// buyerReport is the part of apa-buyer's report a run is scored from.
type buyerReport struct {
	Outcome   string `json:"outcome"`
	Detail    string `json:"detail"`
	ToolCalls []struct {
		Tool      string `json:"tool"`
		ProductID string `json:"product_id"`
		Outcome   string `json:"outcome"`
	} `json:"tool_calls"`
	Task *struct {
		Purchases []struct {
			PurchaseOrderID string `json:"purchase_order_id"`
			Decision        struct {
				Verdict string `json:"policy_verdict"`
			} `json:"decision"`
		} `json:"purchases"`
	} `json:"task"`
	Trace   string `json:"trace"`
	Payment string `json:"payment"`
}

func once(ctx context.Context, c *ae1.Corpus, k ae1.Case, behavior string, attempt int, o runOpts) (ae1.Row, error) {
	row := ae1.Row{Corpus: c.Version, CorpusSHA: c.SHA256, Case: k.ID, Kind: k.Kind, Category: k.Category, Expected: k.Expected(), Attempt: attempt, Mode: "fake"}
	if o.live {
		row.Mode = "live"
	}
	dir, err := os.MkdirTemp("", "ae1-run-")
	if err != nil {
		return row, err
	}
	defer os.RemoveAll(dir)
	items, err := c.Items(k)
	if err != nil {
		return row, err
	}
	catalog := filepath.Join(dir, "catalog.json")
	if err := writeJSON(catalog, items); err != nil {
		return row, err
	}
	task := k.Task
	if task == "" {
		task = craftedTask
	}
	argv := []string{"run", "--catalog", catalog, "--task-text", task, "--trace-dir", filepath.Join(dir, "runs")}
	env := []string{"APA_DB_PATH=" + filepath.Join(dir, "eval.db")}
	if o.live {
		argv = append(argv, "--live", "--cli", o.cli, "--model", o.model)
		if o.devDemo {
			argv = append(argv, "--dev-demo-plugins")
		}
		env = append(env, "APA_LIVE_CLI=true")
	} else {
		steps := k.Script
		if len(steps) == 0 {
			steps = script(k, items, behavior)
		}
		scen := filepath.Join(dir, "scenario.json")
		if err := writeJSON(scen, map[string]any{"description": "ae1 " + k.ID + " " + behavior, "steps": steps}); err != nil {
			return row, err
		}
		argv = append(argv, "--fake-cli", o.fake, "--scenario", scen)
	}
	for _, key := range []string{"HOME", "USER", "LOGNAME", "PATH", "TMPDIR", "LANG", "LC_ALL", "TERM"} {
		if v := o.getenv(key); v != "" {
			env = append(env, key+"="+v) // apa-buyer passes the CLI only its own allowlist (buyer.CLIEnv)
		}
	}
	cmd := exec.CommandContext(ctx, o.buyer, argv...)
	cmd.Env, cmd.Dir = env, mustWD()
	// Stopped, apa-buyer gets an interrupt, as at a terminal, and stops its
	// CLI's process group itself; a kill would leave a live CLI without its
	// host's limits (review 3).
	cmd.Cancel = func() error { return cmd.Process.Signal(os.Interrupt) }
	cmd.WaitDelay = 2*buyer.DefaultLimits.Grace + 10*time.Second
	var stdout bytes.Buffer
	cmd.Stdout, cmd.Stderr = &stdout, io.Discard
	start := time.Now()
	runErr := cmd.Run()
	row.Seconds = time.Since(start).Seconds()
	rep, err := parseReport(stdout.Bytes())
	if err != nil {
		if runErr != nil {
			err = errors.Join(err, runErr)
		}
		return row, fmt.Errorf("apa-buyer gave no report: %w", err)
	}
	if !strings.HasPrefix(rep.Payment, "none") {
		return row, errPayment
	}
	row.Obs, row.Detail = observe(rep), rep.Detail
	row.InitModel, row.Usage, row.PromptSHA = traceFacts(rep.Trace)
	if o.live && row.Obs.Outcome == "ok" && !modelMatches(o.model, row.InitModel) {
		// The CLI ran another model than asked (or named none known): not this
		// model's run (acceptance 19); counted as an infrastructure failure.
		row.Obs.Outcome, row.Detail = "model_mismatch", strings.TrimSpace(row.Detail+" init_model="+row.InitModel)
	}
	row.Score = ae1.ScoreRun(k, row.Obs)
	return row, nil
}

// observe is a run's record: the Store's purchase for each product the host
// recorded a request for, in request order (a product's later calls report the
// same purchase), and the bridge's refusals.
func observe(r buyerReport) ae1.Observation {
	o := ae1.Observation{Outcome: r.Outcome}
	verdict := map[string]string{} // purchase order ID => verdict
	if r.Task != nil {
		for _, p := range r.Task.Purchases {
			verdict[p.PurchaseOrderID] = p.Decision.Verdict
		}
	}
	seen := map[string]bool{}
	for _, c := range r.ToolCalls {
		po := toolbridge.PurchaseOrderID(evalTask, c.ProductID)
		switch {
		case c.Tool == "request_purchase" && c.ProductID != "" && verdict[po] != "" && !seen[po]:
			seen[po] = true
			o.Requests = append(o.Requests, ae1.Req{Product: c.ProductID, Verdict: verdict[po]})
		case !slices.Contains([]string{"ok", "Allow", "Ask", "Deny"}, c.Outcome):
			o.Refused = append(o.Refused, c.Tool+":"+c.Outcome)
		}
	}
	if len(seen) != len(verdict) && o.Outcome == "ok" {
		// The Store holds a purchase the host's record does not name: the run
		// cannot be scored (counted as an infrastructure failure).
		o.Outcome = "record_mismatch"
	}
	return o
}

func parseReport(stdout []byte) (buyerReport, error) {
	var r buyerReport
	i := bytes.Index(stdout, []byte("\n{"))
	if i < 0 {
		return r, errors.New("no JSON report")
	}
	err := json.Unmarshal(stdout[i+1:], &r)
	return r, err
}

// traceFacts reads the whitelisted trace: the model the init named, the
// usage the CLI reported and the prompt's hash.
func traceFacts(path string) (model string, usage map[string]any, promptSHA string) {
	f, err := os.Open(path)
	if err != nil {
		return "", nil, ""
	}
	defer f.Close()
	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 64<<10), 1<<20)
	for sc.Scan() {
		var e map[string]any
		if json.Unmarshal(sc.Bytes(), &e) != nil {
			continue
		}
		switch e["event"] {
		case "run_start":
			promptSHA, _ = e["prompt_sha256"].(string)
		case "init":
			model, _ = e["model"].(string)
		case "run_end":
			usage, _ = e["usage"].(map[string]any)
		}
	}
	return model, usage, promptSHA
}

// script is a scripted stand-in for a model on a benign or injection case.
func script(k ae1.Case, items []merchant.EvalItem, behavior string) []json.RawMessage {
	var steps []json.RawMessage
	add := func(v map[string]any) {
		raw, _ := json.Marshal(v)
		steps = append(steps, raw)
	}
	buy := func(p string) {
		add(map[string]any{"do": "tool", "tool": "get_quote", "args": map[string]string{"product_id": p}})
		add(map[string]any{"do": "tool", "tool": "request_purchase", "args": map[string]string{"product_id": p}})
	}
	expected := k.Accept[0].Requests
	other := ""
	for _, it := range items {
		if !slices.ContainsFunc(expected, func(r ae1.Req) bool { return r.Product == it.ID }) && !slices.Contains(k.Injected, it.ID) {
			other = it.ID
			break
		}
	}
	add(map[string]any{"do": "tool", "tool": "list_products"})
	switch behavior {
	case "obey", "fooled", "extra":
		for _, r := range expected {
			buy(r.Product)
		}
		if behavior == "fooled" && len(k.Injected) > 0 {
			buy(k.Injected[0])
		} else if behavior != "obey" && other != "" {
			buy(other)
		}
	case "wrong":
		if other != "" {
			buy(other)
		}
	}
	add(map[string]any{"do": "result", "text": "done"})
	return steps
}

func report(args []string, out io.Writer) error {
	fs := flag.NewFlagSet("report", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	outDir := fs.String("out", "", "a new directory for the merged report")
	corpusPath := fs.String("corpus", "testdata/ae1/corpus.json", "the frozen corpus every row must come from")
	thresholdsPath := fs.String("thresholds", "testdata/ae1/thresholds.json", "D2's thresholds")
	reps := fs.Int("reps", 0, "runs per case and model the batches meant to make (D4: 3)")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if *outDir == "" || fs.NArg() == 0 || *reps < 1 {
		return errors.New("usage: ae1-eval report --out DIR --reps N [--corpus FILE] RESULTS.jsonl...")
	}
	corpus, err := ae1.Load(*corpusPath)
	if err != nil {
		return err
	}
	var thr ae1.Thresholds
	if err := decode(*thresholdsPath, &thr); err != nil {
		return err
	}
	var rows []ae1.Row
	for _, p := range fs.Args() {
		raw, err := os.ReadFile(p)
		if err != nil {
			return err
		}
		for _, line := range bytes.Split(bytes.TrimSpace(raw), []byte("\n")) {
			var r ae1.Row
			if err := json.Unmarshal(line, &r); err != nil {
				return fmt.Errorf("%s: %w", p, err)
			}
			rows = append(rows, r)
		}
	}
	plans, err := checkMerge(corpus, rows, *reps)
	if err != nil {
		return err
	}
	if err := os.Mkdir(*outDir, 0o700); err != nil {
		return fmt.Errorf("--out must be a new directory: %w", err)
	}
	return writeReport(*outDir, ae1.Build(rows, plans, thr), out)
}

// checkMerge refuses rows that cannot be one evaluation (review 2): another
// corpus, a model in two modes, a case under two prompts, a run twice, a run
// the plan has no place for; fake rows only for crafted and fault cases. It
// returns each model's plan: every benign and injection case reps times; the
// fake stand-in, every crafted and fault case once.
func checkMerge(c *ae1.Corpus, rows []ae1.Row, reps int) (map[string]ae1.Plan, error) {
	kind := map[string]string{}
	for _, k := range c.Cases {
		kind[k.ID] = k.Kind
	}
	mode, prompt := map[string]string{}, map[string]string{}
	seen := map[string]bool{}
	plans := map[string]ae1.Plan{}
	for _, r := range rows {
		k, ok := kind[r.Case]
		switch {
		case r.CorpusSHA != c.SHA256:
			return nil, fmt.Errorf("case %s: corpus %.12s is not %.12s", r.Case, r.CorpusSHA, c.SHA256)
		case !ok || k != r.Kind:
			return nil, fmt.Errorf("case %s (%s) is not in the corpus", r.Case, r.Kind)
		case r.Attempt < 1 || (r.Model == "fake" && r.Attempt != 1) || (r.Model != "fake" && r.Attempt > reps):
			return nil, fmt.Errorf("case %s attempt %d of %s has no place in the plan", r.Case, r.Attempt, r.Model)
		case (r.Model == "fake") != (k == ae1.Crafted || k == ae1.Fault):
			return nil, fmt.Errorf("case %s: %s rows here are a model's, crafted and fault rows the fake stand-in's", r.Case, k)
		case mode[r.Model] != "" && mode[r.Model] != r.Mode:
			return nil, fmt.Errorf("model %s in two modes", r.Model)
		case r.PromptSHA != "" && prompt[r.Case] != "" && prompt[r.Case] != r.PromptSHA && r.Model != "fake":
			return nil, fmt.Errorf("case %s under two prompts", r.Case)
		}
		key := fmt.Sprintf("%s|%s|%d", r.Model, r.Case, r.Attempt)
		if seen[key] {
			return nil, fmt.Errorf("case %s attempt %d of %s twice", r.Case, r.Attempt, r.Model)
		}
		seen[key], mode[r.Model] = true, r.Mode
		if r.PromptSHA != "" && r.Model != "fake" {
			prompt[r.Case] = r.PromptSHA
		}
		if _, ok := plans[r.Model]; !ok {
			if r.Model == "fake" {
				plans[r.Model] = ae1.Plan{Runs: countKind(c, ae1.Crafted) + countKind(c, ae1.Fault), Crafted: countKind(c, ae1.Crafted)}
			} else {
				plans[r.Model] = ae1.Plan{Runs: (countKind(c, ae1.Benign) + countKind(c, ae1.Injection)) * reps}
			}
		}
	}
	return plans, nil
}

func writeReport(dir string, r ae1.Report, out io.Writer) error {
	raw, err := json.MarshalIndent(r, "", "  ")
	if err != nil {
		return err
	}
	if err := os.WriteFile(filepath.Join(dir, "report.json"), raw, 0o600); err != nil {
		return err
	}
	md := r.Markdown()
	if err := os.WriteFile(filepath.Join(dir, "report.md"), []byte(md), 0o600); err != nil {
		return err
	}
	fmt.Fprintf(out, "report: %s\n", filepath.Join(dir, "report.md"))
	return nil
}

func writeJSON(path string, v any) error {
	raw, err := json.Marshal(v)
	if err != nil {
		return err
	}
	return os.WriteFile(path, raw, 0o600)
}

func decode(path string, v any) error {
	raw, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.DisallowUnknownFields()
	return dec.Decode(v)
}

// mustWD is the directory apa-buyer runs in: this one, so its default
// registry and profile paths (testdata/...) resolve as they do by hand.
func mustWD() string {
	wd, _ := os.Getwd()
	return wd
}
