// Command m1-live makes the M1 live single-Ask evidence bundle (M1-E3;
// docs/m1-evidence.md section 16; make m1-live-cli). It is docs/cli-poc.md
// section 51's one-purchase script as a program: every precondition first
// (opt-in, the reviewed product base, a clean tracked tree, a clean local
// clone and its build), then exactly one `apa-buyer run --live` of that
// build against a new database and trace directory, then a read-only,
// consistent export into a new artifacts/m1/<run-id>/ with the existing
// RunManifest contract and an end marker.
//
// It runs a real model only through apa-buyer's own live path and guards
// (APA_LIVE_CLI=true, the approved version, the Gate); it has no fake
// switch. Tests give it a stand-in executable as --cli.
//
// Only the ask case is claimed; the other 30 cases and the seven
// capabilities are not evaluated. Nothing is retried: a run that fails is
// kept and reported as it ended.
package main

import (
	"bufio"
	"bytes"
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"io/fs"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"regexp"
	"runtime"
	"runtime/debug"
	"slices"
	"sort"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/SIMPLYBOYS/agent-payment-authority/internal/buyer"
	"github.com/SIMPLYBOYS/agent-payment-authority/internal/contracts"
	"github.com/SIMPLYBOYS/agent-payment-authority/internal/m1evidence"
	"github.com/SIMPLYBOYS/agent-payment-authority/internal/progress"
	"github.com/SIMPLYBOYS/agent-payment-authority/internal/store"
)

// main stops this run on SIGINT or SIGTERM: the first cancels it (the
// wrapper's group gets SIGTERM, then SIGKILL after the grace period, see
// internal/progress.Group) and the bundle records the interruption; later
// signals are only reported, so they cannot cut the cleanup short or
// restart its clock. SIGKILL cannot be caught.
func main() {
	ctx, cancel := context.WithCancel(context.Background())
	sigs := make(chan os.Signal, 4)
	signal.Notify(sigs, os.Interrupt, syscall.SIGTERM)
	go func() {
		for n := 0; ; n++ {
			s := <-sigs
			if n == 0 {
				fmt.Fprintf(os.Stderr, "m1-live: %v: stopping this run (no retry); the wrapper gets SIGTERM and its bounded cleanup runs\n", s)
				cancel()
			} else {
				fmt.Fprintf(os.Stderr, "m1-live: %v again: already stopping; the cleanup continues\n", s)
			}
		}
	}()
	os.Exit(run(ctx, os.Args[1:], os.Environ(), os.Stdout, os.Stderr))
}

// Exit codes: the bundle is complete and the ask claim passed; complete
// with the claim not passing (the run failed, or its record does not show
// an Ask); stopped before the CLI started, or the bundle not saved
// completely.
const (
	exitPass       = 0
	exitFailures   = 1
	exitIncomplete = 2
)

// configFiles are the configuration digest's files, in the documented
// order (docs/m1-evidence.md section 3), read from the clean clone.
var configFiles = []string{"testdata/contracts/cases.json", "testdata/contracts/registry.json", "testdata/contracts/purchase-input.json",
	"testdata/demo/registry.json", "testdata/policy/m1-demo-profile.json", "testdata/cli/ask-mainline.json"}

// Bounds on what is read of apa-buyer's output (its report and its own
// error lines); the CLI's output never reaches the producer.
const (
	maxStdout = 4 << 20
	maxStderr = 1 << 20
)

// run is the producer; env is its whole environment, which apa-buyer gets
// with APA_DB_PATH added (apa-buyer narrows the CLI's own). ctx ending is
// the operator's interruption.
func run(ctx context.Context, args []string, env []string, stdout, stderr io.Writer) int {
	getenv := func(k string) string {
		v := ""
		for _, kv := range env {
			if key, val, ok := strings.Cut(kv, "="); ok && key == k {
				v = val
			}
		}
		return v
	}
	fl := flag.NewFlagSet("m1-live", flag.ContinueOnError)
	fl.SetOutput(stderr)
	live := fl.Bool("live", false, "run the approved Claude Code CLI once (a real model); also needs APA_LIVE_CLI=true")
	cli := fl.String("cli", "", "absolute path of the approved Claude Code executable")
	base := fl.String("base", "", "the reviewed product commit; HEAD may differ from it in docs/ and README.md only")
	gate := fl.String("gate", "", "the gate mode, stated explicitly: dev_demo_plugins (the accepted exception) or strict")
	repo := fl.String("repo", ".", "the repository")
	out := fl.String("out", "artifacts/m1", "where the new bundle directory is made")
	workRoot := fl.String("work", "artifacts/m1-live-work", "where the run's private working directory (database, trace, build) is made")
	task := fl.String("task", "task-a", "the trusted registry task")
	wait := fl.Duration("wait", 10*time.Minute, "outer bound on the one run (apa-buyer's own limits are 180 s); then it is stopped, not retried")
	heartbeat := fl.Duration("heartbeat", 10*time.Second, "how often a quiet stage reports that it is still waiting")
	if fl.Parse(args) != nil || fl.NArg() != 0 {
		return exitIncomplete
	}
	stop := func(format string, a ...any) int {
		fmt.Fprintf(stderr, "m1-live: STOP (nothing was started): "+format+"\n", a...)
		return exitIncomplete
	}
	// Stages and heartbeats go to stderr; they name the producer's own
	// steps only, never what the CLI says.
	begun := time.Now()
	stage := func(n int, name string) {
		fmt.Fprintf(stderr, "m1-live: [%d/6] %s (%s, %s since start)\n", n, name, time.Now().UTC().Format("15:04:05Z"), time.Since(begun).Truncate(time.Second))
	}
	waiting := func(what string) func() {
		return progress.Heartbeat(stderr, *heartbeat, func(d time.Duration) string {
			return fmt.Sprintf("m1-live: ... %s: still waiting (%s); no new information", what, d)
		})
	}
	stage(1, "preconditions")
	// Preconditions: nothing is created, built or started before they pass.
	if on, err := strconv.ParseBool(getenv("APA_LIVE_CLI")); err != nil || !on || !*live {
		return stop("a live run needs both --live and APA_LIVE_CLI=true")
	}
	if *gate != "dev_demo_plugins" && *gate != "strict" {
		return stop("--gate must be stated: dev_demo_plugins or strict")
	}
	if !filepath.IsAbs(*cli) {
		return stop("--cli must be the absolute path of the approved Claude Code executable; no PATH lookup is done")
	}
	if fi, err := os.Stat(*cli); err != nil || !fi.Mode().IsRegular() || fi.Mode().Perm()&0o111 == 0 {
		return stop("--cli %s is not an executable file", *cli)
	}
	if *base == "" {
		return stop("--base (the reviewed product commit) is required")
	}
	repoAbs, err := filepath.Abs(*repo)
	if err != nil {
		return stop("%v", err)
	}
	git := func(args ...string) (string, error) {
		c := exec.Command("git", append([]string{"-C", repoAbs}, args...)...)
		b, err := c.Output()
		return strings.TrimSpace(string(b)), err
	}
	head, err := git("rev-parse", "--verify", "HEAD^{commit}")
	if err != nil || !regexp.MustCompile(`^[0-9a-f]{40}$`).MatchString(head) {
		return stop("git rev-parse HEAD failed")
	}
	status, err := git("status", "--porcelain", "--untracked-files=all")
	if err != nil {
		return stop("git status failed")
	}
	var untracked []string
	for l := range strings.SplitSeq(status, "\n") {
		switch {
		case l == "":
		case strings.HasPrefix(l, "?? "):
			untracked = append(untracked, strings.TrimPrefix(l, "?? "))
		default:
			return stop("the tracked tree has changes (%s); not a clean build", l)
		}
	}
	baseFull, err := git("rev-parse", "--verify", *base+"^{commit}")
	if err != nil {
		return stop("--base %s is not a commit here", *base)
	}
	if _, err := git("merge-base", "--is-ancestor", baseFull, head); err != nil {
		return stop("the reviewed base %s is not an ancestor of HEAD %s", baseFull, head)
	}
	if diff, err := git("diff", "--name-only", baseFull, head, "--", ".", ":(exclude)docs", ":(exclude)README.md"); err != nil || diff != "" {
		return stop("HEAD differs from the reviewed base %s outside docs/ and README.md: %s", baseFull, diff)
	}
	var cases contracts.CaseManifest
	casesRaw, err := os.ReadFile(filepath.Join(repoAbs, "testdata/contracts/cases.json"))
	if err != nil || contracts.Decode(casesRaw, &cases) != nil || cases.RequireAccepted() != nil {
		return stop("the accepted testdata/contracts/cases.json is needed")
	}
	askI := slices.IndexFunc(cases.Cases, func(c contracts.AcceptanceCase) bool { return c.CaseID == "ask" })
	if askI < 0 {
		return stop("the case list has no ask case")
	}
	caseDigest, _ := cases.Hash()
	for _, d := range []string{*out, *workRoot} {
		if err := os.MkdirAll(d, 0o700); err != nil {
			return stop("%v", err)
		}
		if fi, err := os.Lstat(d); err != nil || !fi.IsDir() {
			return stop("%s is not a plain directory", d)
		}
	}
	suffix := make([]byte, 3)
	rand.Read(suffix)
	runID := "m1-live-" + time.Now().UTC().Format("20060102T150405Z") + "-" + hex.EncodeToString(suffix)
	dir, work := filepath.Join(*out, runID), filepath.Join(*workRoot, runID)
	if err := os.Mkdir(dir, 0o700); err != nil { // new, never an old one
		return stop("%v", err)
	}
	if err := os.Mkdir(work, 0o700); err != nil {
		return stop("%v", err)
	}
	workAbs, err := filepath.Abs(work)
	if err != nil {
		return stop("%v", err)
	}

	// From here the bundle exists: a failure is recorded in its end marker.
	b := &bundle{dir: dir, runID: runID, stderr: stderr}
	for _, d := range []string{"provenance", "live"} {
		if err := os.Mkdir(filepath.Join(dir, d), 0o700); err != nil {
			return b.incomplete("save_failed", "", err)
		}
	}
	if err := b.save("provenance/git-status.txt", []byte(status+"\n")); err != nil {
		return b.incomplete("save_failed", "", err)
	}

	// A clean local clone at HEAD and its build; the build must say so.
	stage(2, "local clone")
	buildDir := filepath.Join(workAbs, "build")
	if err := os.Mkdir(buildDir, 0o700); err != nil {
		return b.incomplete("save_failed", "nothing was started", err)
	}
	defer os.RemoveAll(buildDir) // only what this run built
	src := filepath.Join(buildDir, "src")
	for _, c := range [][]string{{"clone", "--quiet", "--no-hardlinks", "--no-checkout", repoAbs, src}, {"-C", src, "checkout", "--quiet", "--detach", head}} {
		quiet := waiting("git " + c[0])
		gitOut, err := exec.CommandContext(ctx, "git", c...).CombinedOutput()
		quiet()
		if ctx.Err() != nil {
			return b.incomplete("interrupted", "nothing was started", ctx.Err())
		}
		if err != nil {
			return b.incomplete("provenance_failed", "nothing was started", fmt.Errorf("git %s: %v: %s", c[0], err, bytes.TrimSpace(gitOut)))
		}
	}
	afterClone(src)
	stage(3, "build and VCS check")
	bin := filepath.Join(buildDir, "apa-buyer")
	gb := exec.CommandContext(ctx, "go", "build", "-o", bin, "./cmd/apa-buyer")
	gb.Dir = src
	quiet := waiting("go build")
	buildOut, err := gb.CombinedOutput()
	quiet()
	if ctx.Err() != nil {
		return b.incomplete("interrupted", "nothing was started", ctx.Err())
	}
	if err != nil {
		return b.incomplete("provenance_failed", "nothing was started", fmt.Errorf("go build: %v: %s", err, bytes.TrimSpace(buildOut)))
	}
	vcs, err := exec.Command("go", "version", "-m", bin).Output()
	if err != nil {
		return b.incomplete("provenance_failed", "nothing was started", err)
	}
	rev, modified := setting(vcs, "vcs.revision"), setting(vcs, "vcs.modified")
	goVersion := strings.TrimSpace(firstLine(vcs))
	if rev != head || modified != "false" {
		return b.incomplete("provenance_failed", "nothing was started", fmt.Errorf("the build says revision %q modified %q; HEAD is %s", rev, modified, head))
	}
	configs := map[string]string{}
	var cfg strings.Builder
	for _, f := range configFiles {
		raw, err := os.ReadFile(filepath.Join(src, f))
		if err != nil {
			return b.incomplete("provenance_failed", "nothing was started", err)
		}
		configs[f] = digest(raw)
		fmt.Fprintf(&cfg, "%s\x00%s\n", f, configs[f])
	}
	producer := "unknown"
	if info, ok := debug.ReadBuildInfo(); ok {
		producer = info.Main.Version
		for _, s := range info.Settings {
			if s.Key == "vcs.revision" || s.Key == "vcs.modified" {
				producer += " " + s.Key + "=" + s.Value
			}
		}
	}
	if err := errors.Join(b.saveJSON("provenance/config-files.json", configs), b.saveJSON("provenance/build.json", map[string]any{
		"head": head, "reviewed_base": baseFull, "untracked_in_repository": untracked,
		"source": "a local git clone at HEAD (--no-hardlinks), checked out detached", "binary": "apa-buyer",
		"vcs_revision": rev, "vcs_modified": modified, "go": goVersion, "producer": producer,
	})); err != nil {
		return b.incomplete("save_failed", "nothing was started", err)
	}

	// The one run.
	dbPath := filepath.Join(workAbs, "db", "live.db") // db/ is made by the Store, private
	traceDir := filepath.Join(workAbs, "runs")
	cmdArgs := []string{"run", "--live", "--cli", *cli}
	if *gate == "dev_demo_plugins" {
		cmdArgs = append(cmdArgs, "--dev-demo-plugins")
	}
	cmdArgs = append(cmdArgs, "--task", *task, "--registry", filepath.Join(src, "testdata/contracts/registry.json"),
		"--profile", filepath.Join(src, "testdata/policy/m1-demo-profile.json"), "--trace-dir", traceDir)
	command := "apa-buyer " + strings.Join(cmdArgs, " ")
	if ctx.Err() != nil {
		return b.incomplete("interrupted", "nothing was started", ctx.Err())
	}
	beforeRun(bin)
	stage(4, "the wrapper run: waiting for apa-buyer to finish (no CLI text is read)")
	fmt.Fprintf(stderr, "m1-live: %s: LIVE: one run of %s (%s gate); no retry\n", runID, *cli, *gate)
	started := time.Now().UTC()
	runCtx, cancelRun := context.WithTimeout(ctx, *wait)
	c := exec.CommandContext(runCtx, bin, cmdArgs...)
	c.Dir = src
	c.Env = append(slices.Clone(env), "APA_DB_PATH="+dbPath)
	var group progress.Group
	group.Prepare(c, 30*time.Second) // SIGTERM to apa-buyer's group (it stops its run), SIGKILL to what is left of that group 30 s later
	var outBuf, errBuf bounded
	outBuf.max, errBuf.max = maxStdout, maxStderr
	c.Stdout, c.Stderr = &outBuf, &errBuf
	quiet = waiting("apa-buyer")
	runErr := c.Run()
	if err := group.Done(); err != nil { // a stop that could not empty the group
		fmt.Fprintf(stderr, "m1-live: %s: %v\n", runID, err)
	}
	quiet()
	// Why it ended, read before anything else is cancelled: the stop ran
	// (the operator, or the outer bound), it never started, or it exited by
	// itself (with its own code).
	termination := "completed"
	exitCode := -1
	if c.ProcessState != nil {
		exitCode = c.ProcessState.ExitCode()
	}
	switch {
	case group.Stopped() && ctx.Err() != nil:
		termination = "interrupted"
	case group.Stopped():
		termination = "outer_bound"
	case c.ProcessState == nil:
		termination = "start_failed"
	case exitCode != 0:
		termination = "wrapper_exit_nonzero"
	}
	cancelRun()
	_ = runErr // the termination and the exit code say it
	ended := time.Now().UTC()
	os.RemoveAll(buildDir)
	stage(5, "Store offline check and read-only export")

	// What the run left: apa-buyer's whitelisted report and its own error
	// lines, the trace, the Store.
	report := map[string]any{}
	if i := bytes.Index(outBuf.Bytes(), []byte("\n{")); i >= 0 {
		var full map[string]json.RawMessage
		if json.Unmarshal(outBuf.Bytes()[i+1:], &full) == nil {
			for _, k := range []string{"mode", "outcome", "detail", "exit_code", "task_started", "tool_calls", "not_ready_refusals", "store_end", "store_end_error", "expiry_errors"} {
				if v, ok := full[k]; ok {
					report[k] = v
				}
			}
		}
	}
	var hostLines []string
	dropped := 0
	sc := bufio.NewScanner(bytes.NewReader(errBuf.Bytes()))
	for sc.Scan() {
		l := sc.Text()
		if strings.HasPrefix(l, "apa-buyer: ") && len(l) <= 600 && !tokenLike.MatchString(l) {
			hostLines = append(hostLines, l)
		} else {
			dropped++
		}
	}
	traces, _ := filepath.Glob(filepath.Join(traceDir, "*.jsonl"))
	sort.Strings(traces)
	var traceRaw []byte
	if len(traces) == 1 {
		if traceRaw, err = os.ReadFile(traces[0]); err != nil {
			return b.incomplete("save_failed", "", err)
		}
		if err := b.save("live/trace.jsonl", traceRaw); err != nil {
			return b.incomplete("save_failed", "", err)
		}
	}
	var tasks []m1evidence.Task
	dbDigests := map[string]string{}
	snapshot := map[string]any{}
	if _, err := os.Lstat(dbPath); err == nil {
		// The writer has ended (apa-buyer exited). The Store's own check of
		// an offline record (store.OpenSnapshot: no -wal or -shm beside it,
		// copied unchanged, then hash chains, projections and snapshots
		// verified on the copy) must pass; then a private copy, checked
		// unchanged while copied, is what is exported.
		_, done, err := store.OpenSnapshot(dbPath)
		if err != nil {
			return b.incomplete("store_unreadable", "", fmt.Errorf("the run's database did not pass the Store's offline check: %w", err))
		}
		if err := done(); err != nil {
			return b.incomplete("store_unreadable", "", err)
		}
		snap := filepath.Join(workAbs, "snapshot")
		if err := os.Mkdir(snap, 0o700); err != nil {
			return b.incomplete("save_failed", "", err)
		}
		raw, err := os.ReadFile(dbPath)
		if err != nil {
			return b.incomplete("store_unreadable", "", err)
		}
		if again, err := os.ReadFile(dbPath); err != nil || !bytes.Equal(again, raw) {
			return b.incomplete("store_unreadable", "", errors.New("the database changed while it was copied"))
		}
		dbDigests["live.db"] = digest(raw)
		if err := os.WriteFile(filepath.Join(snap, "live.db"), raw, 0o600); err != nil {
			return b.incomplete("save_failed", "", err)
		}
		if tasks, err = m1evidence.Export(filepath.Join(snap, "live.db"), *task); err != nil {
			tasks = nil // no task: the run did not start one
		}
		snapshot["exported_at"] = time.Now().UTC().Unix()
		if len(tasks) == 1 {
			last := tasks[0].Events[len(tasks[0].Events)-1]
			var e contracts.AuditEvent
			if json.Unmarshal([]byte(last.Body), &e) == nil {
				snapshot["last_sequence"], snapshot["last_observed_at"] = e.Sequence, e.ObservedAt
			}
		}
	}
	usage := any("unknown")
	gateResult, model := "", ""
	for _, l := range bytes.Split(traceRaw, []byte("\n")) {
		var e map[string]json.RawMessage
		if json.Unmarshal(l, &e) != nil {
			continue
		}
		var ev string
		json.Unmarshal(e["event"], &ev)
		switch ev {
		case "init":
			json.Unmarshal(e["gate"], &gateResult)
			json.Unmarshal(e["model"], &model)
		case "run_end":
			var u map[string]int64
			if json.Unmarshal(e["usage"], &u) == nil && len(u) > 0 {
				usage = map[string]any{"counts": u, "source": "self-reported by the CLI; not a billing record"}
			}
		}
	}
	runRecord := map[string]any{
		"version": "apa.m1.live.v2", "run_id": runID, "command": command, "apa_buyer_exit_code": exitCode,
		"termination": termination, "producer_error": producerError[termination],
		"started_at": started.Unix(), "ended_at": ended.Unix(), "gate_mode": *gate, "gate_result": gateResult, "model": model,
		"cli": filepath.Base(*cli), "cli_version_required": buyer.ApprovedCLIVersion, "auth_accepted": buyer.LiveAcceptAuth,
		"report": report, "host_error_lines": nonNil(hostLines), "dropped_stderr_lines": dropped, "trace_files": len(traces),
		"database": map[string]any{"digests": dbDigests, "relation": "live/ask.json is the read-only export of a private copy of this file, taken after apa-buyer exited and the Store's offline check (store.OpenSnapshot: no -wal or -shm, verified) passed"},
		"snapshot": snapshot, "usage": usage,
		"residual_risks": []string{"strict zero-plugin mode not verified", "plugin names and the gate do not prove a plugin's content or that it has no background activity",
			"the APA-only configuration directory is not operating-system isolation", "auth_source none alone proves no login or billing source", "usage is the CLI's own report or unknown; not a billing record"},
	}
	if err := b.saveJSON("live/run.json", runRecord); err != nil {
		return b.incomplete("save_failed", "", err)
	}
	if tasks != nil {
		if err := b.saveJSON("live/ask.json", map[string]any{"case_id": "ask", "layer": "live_cli", "tasks": tasks}); err != nil {
			return b.incomplete("save_failed", "", err)
		}
	}

	// The producer's own verdict for ask (make verify-m1 checks independently).
	askCase := cases.Cases[askI]
	verdict, reason := "PASS", "the run ended ok and its stored record shows the Ask"
	switch {
	case termination == "interrupted" || termination == "outer_bound" || termination == "start_failed":
		verdict, reason = "FAIL", producerError[termination]
		if tasks == nil {
			verdict = "INCONCLUSIVE"
		}
	case tasks == nil:
		verdict, reason = "INCONCLUSIVE", "no task was stored: the run did not reach the Store"
	case exitCode != 0 || report["outcome"] == nil || string(report["outcome"].(json.RawMessage)) != `"ok"`:
		verdict, reason = "FAIL", fmt.Sprintf("apa-buyer exited %d", exitCode)
	case traceRaw == nil || gateResult != "passed":
		verdict, reason = "FAIL", "no trace with a passed gate"
	default:
		if _, problems := m1evidence.CheckIntegration(askCase, cases.Defaults, tasks); len(problems) > 0 {
			verdict, reason = "FAIL", strings.Join(problems, "; ")
		}
	}
	m := contracts.RunManifest{Version: contracts.Version, RunID: runID, CodeCommit: head, DirtyPatchDigest: digest(nil), ConfigDigest: digest([]byte(cfg.String())),
		CaseManifestDigest: caseDigest, Versions: map[string]string{"case_manifest": cases.ManifestID, "cli": buyer.ApprovedCLIVersion, "gate_mode": *gate,
			"go": goVersion, "os": runtime.GOOS + "/" + runtime.GOARCH},
		Commands: []contracts.CommandResult{{Command: command, ExitCode: exitCode}}, Evidence: []contracts.EvidenceRef{}}
	var paths []string
	err = filepath.WalkDir(dir, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.Type().IsRegular() {
			rel, _ := filepath.Rel(dir, p)
			paths = append(paths, filepath.ToSlash(rel))
		} else if !d.IsDir() {
			return fmt.Errorf("%s is not a regular file", p)
		}
		return nil
	})
	if err != nil {
		return b.incomplete("save_failed", "", err)
	}
	sort.Strings(paths)
	ids := map[string]string{}
	for _, p := range paths {
		raw, err := os.ReadFile(filepath.Join(dir, p))
		if err != nil {
			return b.incomplete("save_failed", "", err)
		}
		source := "live_cli"
		if strings.HasPrefix(p, "provenance/") {
			source = "provenance"
		}
		ids[p] = "ev-" + regexp.MustCompile(`[^A-Za-z0-9._:-]`).ReplaceAllString(p, "-")
		m.Evidence = append(m.Evidence, contracts.EvidenceRef{EvidenceID: ids[p], Path: p, Digest: digest(raw), EvidenceMode: "live", Source: source})
	}
	cited := []string{ids["live/run.json"]}
	for _, p := range []string{"live/ask.json", "live/trace.jsonl"} {
		if id, ok := ids[p]; ok {
			cited = append(cited, id)
		}
	}
	m.Results = []contracts.ClaimResult{{CaseID: "ask", Verdict: verdict, EvidenceIDs: cited, Reason: reason}}
	for _, c := range cases.Cases {
		if c.CaseID != "ask" {
			m.NotEvaluated = append(m.NotEvaluated, c.CaseID)
		}
	}
	m.NotEvaluated = append(m.NotEvaluated, contracts.CapabilityIDs...)
	stage(6, "saving the bundle")
	beforeManifest(dir)
	if leaks := scan(dir); len(leaks) > 0 {
		return b.incomplete("secret_found", "", fmt.Errorf("a bare token-like value in %v", leaks))
	}
	if err := m.ValidateAgainst(cases); err != nil {
		return b.incomplete("index_invalid", "", err)
	}
	body, err := json.MarshalIndent(m, "", "  ")
	if err == nil {
		body = append(body, '\n')
		err = b.save("manifest.json", body)
	}
	if err != nil {
		return b.incomplete("save_failed", "", err)
	}
	if err := writeEnd(dir, runID, body, "complete"); err != nil {
		fmt.Fprintf(stderr, "m1-live: %s: the end marker could not be written: %v; the bundle %s is INCOMPLETE\n", runID, err, dir)
		return exitIncomplete
	}
	fmt.Fprintf(stdout, "m1-live: %s (complete): apa-buyer exit %d; ask %s (%s); 30 cases and 7 capabilities not evaluated; working files %s (the producer's own verdict; make verify-m1 RUN_DIR=%s MODE=live checks independently)\n",
		dir, exitCode, verdict, reason, work, dir)
	if verdict != "PASS" {
		return exitFailures
	}
	return exitPass
}

// Test hooks; nothing else sets them. beforeManifest breaks the directory
// before the manifest and the end marker are written; afterClone changes
// the clone before it is built; beforeRun can remove the built wrapper.
var (
	beforeManifest = func(dir string) {}
	afterClone     = func(src string) {}
	beforeRun      = func(bin string) {}
)

// bundle saves into the new run directory and marks it incomplete when
// something could not be saved.
type bundle struct {
	dir, runID string
	stderr     io.Writer
}

func (b *bundle) save(rel string, data []byte) error {
	return os.WriteFile(filepath.Join(b.dir, rel), data, 0o600)
}

func (b *bundle) saveJSON(rel string, v any) error {
	raw, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		return err
	}
	return b.save(rel, append(raw, '\n'))
}

func (b *bundle) incomplete(state, note string, err error) int {
	if note != "" {
		note = " (" + note + ")"
	}
	fmt.Fprintf(b.stderr, "m1-live: %s: the bundle %s is INCOMPLETE (%s)%s: %v\n", b.runID, b.dir, state, note, err)
	if werr := writeEnd(b.dir, b.runID, nil, state); werr != nil {
		fmt.Fprintf(b.stderr, "m1-live: the end marker could not be written either: %v\n", werr)
	}
	return exitIncomplete
}

func writeEnd(dir, runID string, manifest []byte, state string) error {
	end := map[string]string{"run_id": runID, "state": state}
	if manifest != nil {
		end["manifest_digest"] = digest(manifest)
	}
	raw, err := json.MarshalIndent(end, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(filepath.Join(dir, "run-end.json"), append(raw, '\n'), 0o600)
}

func digest(b []byte) string {
	h := sha256.Sum256(b)
	return "sha256:" + hex.EncodeToString(h[:])
}

// tokenLike is a bare 64-hex value: the shape of a token or capability.
var tokenLike = regexp.MustCompile(`(^|[^0-9a-fA-Fx:])[0-9a-f]{64}([^0-9a-f]|$)`)

// promptDigest is the one bare 64-hex value a bundle may hold: the run
// trace's prompt_sha256 of the host's fixed prompt (buyer.Prompt). That
// exact member is set aside before the scan; any other 64-hex value fails.
func promptDigest() []byte {
	h := sha256.Sum256([]byte(buyer.Prompt))
	return []byte(`"prompt_sha256":"` + hex.EncodeToString(h[:]) + `"`)
}

func scan(dir string) []string {
	var leaks []string
	filepath.WalkDir(dir, func(p string, d fs.DirEntry, err error) error {
		if err == nil && d.Type().IsRegular() {
			raw, err := os.ReadFile(p)
			if err != nil || tokenLike.Match(bytes.ReplaceAll(raw, promptDigest(), nil)) {
				rel, _ := filepath.Rel(dir, p)
				leaks = append(leaks, rel)
			}
		}
		return nil
	})
	return leaks
}

func setting(versionM []byte, key string) string {
	for l := range strings.SplitSeq(string(versionM), "\n") {
		f := strings.Fields(l)
		if len(f) == 2 && f[0] == "build" && strings.HasPrefix(f[1], key+"=") {
			return strings.TrimPrefix(f[1], key+"=")
		}
	}
	return ""
}

func firstLine(b []byte) string {
	l, _, _ := strings.Cut(string(b), "\n")
	_, v, _ := strings.Cut(l, ": ")
	return v
}

// producerError is the producer's own account of a run that did not end by
// itself; "" when it did (its exit code, 0 or not, says the rest).
var producerError = map[string]string{
	"completed":            "",
	"wrapper_exit_nonzero": "",
	"outer_bound":          "the outer bound was reached; the run was stopped",
	"interrupted":          "interrupted by the operator (SIGINT or SIGTERM); the run was stopped",
	"start_failed":         "apa-buyer could not be started",
}

func nonNil(s []string) []string {
	if s == nil {
		return []string{}
	}
	return s
}

// bounded keeps at most max bytes and counts the rest.
type bounded struct {
	bytes.Buffer
	max  int
	over int
}

func (w *bounded) Write(p []byte) (int, error) {
	if room := w.max - w.Len(); room > 0 {
		if len(p) > room {
			w.over += len(p) - room
			w.Buffer.Write(p[:room])
		} else {
			w.Buffer.Write(p)
		}
	} else {
		w.over += len(p)
	}
	return len(p), nil
}
