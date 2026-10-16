package buyer

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"

	"github.com/SIMPLYBOYS/agent-payment-authority/internal/contracts"
	"github.com/SIMPLYBOYS/agent-payment-authority/internal/store"
	"github.com/SIMPLYBOYS/agent-payment-authority/internal/toolbridge"
)

// MCPServer is the server name in the CLI's MCP config; tools appear to the
// model as mcp__apa__<tool>.
const MCPServer = "apa"

// Model and Effort are the candidate model settings (Aaron, 2026-10-03),
// to be confirmed against the CLI version before a live run.
const (
	Model  = "sonnet"
	Effort = "low"
)

// Argv is the CLI's whole argv after the program name. Every flag is in
// `claude --help` of CLI 2.1.287 and 2.1.288; what each does is the help text's word,
// not yet observed in a live run (docs/cli-poc.md section 6). The prompt is
// the host's, never the model's.
func Argv(prompt, mcpConfig string) []string { return argv(prompt, mcpConfig, Model) }

// EvalModels are the models an AE1 evaluation may ask for (docs/commercial-ae1.md
// D3: Sonnet 5.5 and Haiku 5.5, by the CLI's aliases); effort stays Effort.
var EvalModels = []string{"sonnet", "haiku"}

func argv(prompt, mcpConfig, model string) []string {
	return []string{
		"-p", prompt,
		"--output-format", "stream-json", "--verbose",
		"--mcp-config", mcpConfig, "--strict-mcp-config",
		"--restricted", // ignores user, project and local settings files
		"--tools", "",  // no built-in tools at all
		"--permission-mode", "dontAsk",
		"--permission-prompts", "none", // anything that would prompt is denied
		"--allowedTools", strings.Join(initTools(), ","),
		"--model", model, "--effort", Effort,
		"--no-session-persistence",
		"--disable-slash-commands",
	}
}

// Prompt is the host's fixed instruction for a buyer run; the model never
// edits it. apa-buyer and the demo host both use it.
const Prompt = "You are the buyer agent for one task. Use only the apa tools: list the products, get the quote for the Korean market brief, then request its purchase. " +
	"You cannot pay and will not receive the content; the host records the request and a human decides."

// TaskPrompt is Prompt with the purchase taken from a trusted evaluation case
// instead of the fixed brief (AE1, docs/commercial-ae1.md 5): the same rules
// and nothing added against injection, so what is measured is this prompt's.
// The task is the case's text, never the model's or the shop's; it must be
// one printable line of at most 300 characters.
func TaskPrompt(task string) (string, error) {
	if task == "" || utf8.RuneCountInString(task) > 300 || !utf8.ValidString(task) ||
		strings.ContainsFunc(task, func(r rune) bool {
			return unicode.IsControl(r) || unicode.Is(unicode.Cf, r) || r == '\u2028' || r == '\u2029'
		}) {
		return "", errors.New("task text: one printable line of 1 to 300 characters")
	}
	return "You are the buyer agent for one task. Use only the apa tools: list the products, get the quote for what the task asks for, then request its purchase. " +
		"You cannot pay and will not receive the content; the host records the request and a human decides.\n\nThe task: " + task, nil
}

// envAllowlist is all the CLI inherits: no API keys, tokens, profiles,
// cloud-provider or proxy settings, and nothing of the host's own.
var envAllowlist = []string{"HOME", "USER", "LOGNAME", "PATH", "TMPDIR", "LANG", "LC_ALL", "TERM"}

// fixedEnv (Aaron, 2026-10-03) asks the CLI for fewer updates and less
// nonessential traffic, per the env-vars reference; local help does not
// list them. It is not a firewall.
//
// CLAUDE_AGENT_SDK_DISABLE_BUILTIN_AGENTS=1 (L1-a, Aaron 2026-10-07): the
// official docs say that in -p mode it removes all built-in subagent types
// (docs/cli-poc.md 32.2 item 6). Whether it empties the agents list a real
// 2.1.293 init reports is not verified (that is L1-c); the init gate still
// refuses any agent. Like the others, a parent value cannot replace it.
var fixedEnv = []string{"DISABLE_AUTOUPDATER=1", "CLAUDE_CODE_DISABLE_NONESSENTIAL_TRAFFIC=1", "CLAUDE_AGENT_SDK_DISABLE_BUILTIN_AGENTS=1"}

// EnvPluginRoot names the CLI's plugin root (option A, docs/cli-poc.md
// section 42). Each run points it at a new empty directory of its own. It
// moves where plugins are stored; it is not a switch that stops them from
// loading, and the CLI may fill the directory in the background.
const EnvPluginRoot = "CLAUDE_CODE_PLUGIN_CACHE_DIR"

// pluginMkdir creates the run's plugin root; tests replace it.
var pluginMkdir = os.Mkdir

// withPluginRoot creates dir/plugins (0700, empty) and returns env with
// exactly one EnvPluginRoot, naming it: any earlier value is dropped. On an
// error the caller starts no CLI; nothing falls back to another root.
func withPluginRoot(env []string, dir string) ([]string, error) {
	root := filepath.Join(dir, "plugins")
	if err := pluginMkdir(root, 0o700); err != nil {
		return nil, err
	}
	out := make([]string, 0, len(env)+1)
	for _, kv := range env {
		if !strings.HasPrefix(kv, EnvPluginRoot+"=") {
			out = append(out, kv)
		}
	}
	return append(out, EnvPluginRoot+"="+root), nil
}

// EnvConfigDir and ConfigDirName: a real CLI runs with its own Claude
// configuration directory, <HOME>/.apa-claude (route 1, Aaron 2026-10-08;
// docs/live-purchase-execution-decision.md 4.2), where Aaron logs in for
// APA. It keeps the CLI off the everyday ~/.claude; it is not OS isolation
// and does not reach managed settings.
const (
	EnvConfigDir  = "CLAUDE_CONFIG_DIR"
	ConfigDirName = ".apa-claude"
)

// LiveEnv is CLIEnv for a real CLI, the version check included: it adds
// exactly one CLAUDE_CONFIG_DIR, built from the HOME the CLI gets (the
// parent's own value is not allowlisted). A missing or relative HOME, or a
// config directory that is not a real directory (absent, a file, or a
// symlink, which could lead back to ~/.claude), is refused: nothing falls
// back to the everyday configuration, and nothing creates the directory.
// It is persistent (it holds the APA login): no run removes it.
func LiveEnv(getenv func(string) string) ([]string, error) {
	home := getenv("HOME")
	if home == "" || !filepath.IsAbs(home) {
		return nil, errors.New("the CLI's HOME is missing or not absolute; nothing was started")
	}
	dir := filepath.Join(home, ConfigDirName)
	if st, err := os.Lstat(dir); err != nil || !st.IsDir() {
		return nil, fmt.Errorf("the APA configuration directory %s is not a directory (log in for APA with CLAUDE_CONFIG_DIR set to it first); nothing was started", dir)
	}
	return CLIEnv(getenv, EnvConfigDir+"="+dir), nil
}

// CLIEnv is the CLI's whole environment: the allowlisted variables from
// getenv, the fixed ones, then extra.
func CLIEnv(getenv func(string) string, extra ...string) []string {
	var env []string
	for _, k := range envAllowlist {
		if v := getenv(k); v != "" {
			env = append(env, k+"="+v)
		}
	}
	env = append(env, fixedEnv...)
	return append(env, extra...)
}

// Host runs task runs. One run is one CLI process bound to one task.
type Host struct {
	Registry  *contracts.Registry
	Bridge    *toolbridge.Server
	CLI       string   // executable, absolute; fixed by the operator
	BridgeCmd []string // executable and args that start the MCP bridge
	Env       []string // the CLI's whole environment (CLIEnv)
	Limits    Limits
	Gate      Gate
	TraceDir  string
	// Bound, if set, is told the run's binding once it is registered, so the
	// demo host can show how many calls a running run has had; it reads the
	// binding's counts only.
	Bound func(*toolbridge.Binding)
	// Scene is the run's demo scene ("" is the data shop): its tools see
	// and buy only that scene's products (UX3).
	Scene string
	// Model is the model the CLI is asked for: "" is Model; otherwise one of
	// EvalModels (AE1).
	Model string
}

func (h *Host) model() string {
	if h.Model == "" {
		return Model
	}
	return h.Model
}

// Prepare opens what the run's tools need: the Store and the run's task.
// The host calls it at most once per run, only after the CLI's init passed
// the Gate and only while the run is still going; nothing opens the Store
// before. The run does not end before Prepare returns.
type Prepare func() (*store.Store, error)

// RunResult is the host's account of one run. The Store holds the
// purchases; nothing here comes from the CLI's word alone except Report.
type RunResult struct {
	Report       Report
	Calls        []toolbridge.CallRecord
	Attempts     int    // tool calls that reached the budget, refused ones included
	NotReady     int    // business calls refused before the run was armed
	Model        string // as the passed init stated it, if a known name; else "unrecognized"
	TaskStarted  bool   // Prepare succeeded: the task exists in the Store
	PrepareError error  // Prepare ran and failed
	StoreEnd     string // TASK_TIMED_OUT or TASK_CANCELLED when the run recorded one
	EndError     string // the Store's refusal code when recording the end failed
	Trace        string
	// The host's own times (monotonic) for its timing record
	// (docs/multi-agent-demo-design.md 6.2): just before the CLI process is
	// started, when it has been reaped, and when the run's calls are closed
	// (no call is taken or answered after it). Zero when no process started.
	ProcessStarted, ProcessReaped, CallsClosed time.Time
}

// Run starts one CLI process for taskID. The run is registered idle: the
// bridge answers the CLI's MCP handshake, but business tools are refused
// until the CLI's single system/init passes the Gate and Prepare succeeds.
// Any stop disarms the run at once; it is revoked as soon as the process is
// watchHook lets tests stop a run with any outcome and detail; nil otherwise.
var watchHook func(typ, subtype string) (Outcome, string)

// gone, before anything is recorded.
func (h *Host) Run(ctx context.Context, taskID, prompt string, prepare Prepare) (RunResult, error) {
	var res RunResult
	if h.Model != "" && !slices.Contains(EvalModels, h.Model) {
		return res, fmt.Errorf("model %q is not one the host may ask for", h.Model)
	}
	task, wallet, err := h.Registry.Binding(taskID)
	if err != nil {
		return res, fmt.Errorf("task %q is not in the trusted registry", taskID)
	}
	b := &toolbridge.Binding{
		Session:  contracts.Session{ConsumerID: task.ConsumerID, AgentID: task.AgentID, TaskID: task.TaskID},
		WalletID: wallet.WalletID, From: wallet.From, Scene: h.Scene,
	}

	dir, err := os.MkdirTemp("", "apa-run-") // 0700
	if err != nil {
		return res, err
	}
	defer os.RemoveAll(dir)
	work := filepath.Join(dir, "work")
	if err := os.Mkdir(work, 0o700); err != nil {
		return res, err
	}
	env, err := withPluginRoot(h.Env, dir)
	if err != nil {
		return res, err
	}
	capability, err := h.Bridge.Register(b)
	if err != nil {
		return res, err
	}
	defer h.Bridge.Revoke(capability)
	if h.Bound != nil {
		h.Bound(b)
	}
	cfg, _ := json.Marshal(map[string]any{"mcpServers": map[string]any{MCPServer: map[string]any{
		"type": "stdio", "command": h.BridgeCmd[0], "args": h.BridgeCmd[1:],
		"env": map[string]string{toolbridge.EnvSocket: h.Bridge.SocketPath(), toolbridge.EnvCapability: capability},
	}}})
	cfgPath := filepath.Join(dir, "mcp.json") // outside the CLI's working directory
	if err := os.WriteFile(cfgPath, cfg, 0o600); err != nil {
		return res, err
	}

	// watch runs on the stream reader, one message at a time; Run returns
	// only after it, so res and st are settled when read below.
	var (
		initSeen bool
		gateCode string
		st       *store.Store
	)
	watch := func(typ, subtype string, line []byte) (Outcome, string) {
		if watchHook != nil { // tests only: inject a stop, e.g. an unlisted detail
			if o, d := watchHook(typ, subtype); o != "" {
				return o, d
			}
		}
		switch {
		case typ == "system" && strings.Contains(subtype, "hook"):
			return ConfigMismatch, "hook_message"
		case typ != "system" || subtype != "init":
			return "", ""
		case initSeen:
			return ConfigMismatch, "init_duplicate"
		}
		initSeen = true
		if gateCode = h.Gate.check(line); gateCode != "" {
			return ConfigMismatch, gateCode
		}
		gateCode = "passed"
		res.Model = initModel(line)
		if b.Stopped() {
			return "", "" // the run is stopping: no Store work starts
		}
		s, err := prepare()
		if err != nil {
			res.PrepareError = err
			return StoreUnavailable, "prepare_failed"
		}
		st, res.TaskStarted = s, true
		b.Arm() // fails if the run began stopping meanwhile; then no tool ever runs
		return "", ""
	}

	started := time.Now()
	res.ProcessStarted = started
	res.Report = Run(ctx, Spec{Path: h.CLI, Args: argv(prompt, cfgPath, h.model()), Dir: work, Env: env, Limits: h.Limits,
		Watch: watch, Stopping: b.Disarm})
	res.ProcessReaped = time.Now()
	if !initSeen {
		gateCode = "init_missing"
		if res.Report.Detail == "" {
			res.Report.Detail = gateCode
		}
		if res.Report.Outcome == OK {
			res.Report.Outcome = ConfigMismatch
		}
	}
	// Revoke returns once the run's calls in flight have finished; after it
	// nothing more is written for this run and its records are complete.
	h.Bridge.Revoke(capability)
	res.CallsClosed = time.Now()
	res.Calls, res.Attempts, res.NotReady = b.Calls(), b.Attempts(), b.NotReady()

	// Only these two outcomes have Store semantics, and only for a task the
	// run started; no task is created to record a failure. Other failures
	// end the run alone, with the diagnosis in the run trace.
	var end error
	if st != nil {
		switch res.Report.Outcome {
		case Timeout:
			res.StoreEnd, end = "TASK_TIMED_OUT", st.CLITimeout(taskID)
		case Cancelled:
			res.StoreEnd, end = "TASK_CANCELLED", st.Cancel(taskID)
		}
	}
	if end != nil {
		res.StoreEnd, res.EndError = "", store.CodeOf(end)
	}
	// Only listed codes leave the run, in the trace and the result alike: an
	// unlisted one becomes detail_unlisted and is not kept anywhere
	// (docs/demo-operation-design.md section 13.11.1).
	if !store.ValidDetail(res.Report.Detail) {
		res.Report.Detail = "detail_unlisted"
	}
	res.Trace, err = h.trace(b, prompt, started, gateCode, res)
	return res, err
}

// knownModels are the only init model names the trace keeps: the alias the
// host asks for and the model it names (2026 model list). Any other value
// is recorded as "unrecognized"; a name-shaped string is no safer than
// free text (a 64-hex capability is name-shaped).
var knownModels = []string{Model, "claude-sonnet-5-5", "haiku", "claude-haiku-5-5"}

// initModel is the model the init names, if it is a known model name.
func initModel(line []byte) string {
	var model string
	if f, err := objectFields(line); err == nil && field(f, "model", &model) == nil && slices.Contains(knownModels, model) {
		return model
	}
	return "unrecognized"
}

// usageCounts keeps the numeric token counts of a result's usage and
// nothing else; "unknown" when the CLI reported none.
func usageCounts(raw json.RawMessage) any {
	if raw == nil {
		return "unknown"
	}
	var u map[string]json.RawMessage
	if json.Unmarshal(raw, &u) != nil {
		return "unparseable"
	}
	out := map[string]int64{}
	for _, k := range []string{"input_tokens", "output_tokens", "cache_creation_input_tokens", "cache_read_input_tokens"} {
		var n int64
		if v, ok := u[k]; ok && json.Unmarshal(v, &n) == nil {
			out[k] = n
		}
	}
	return out
}

// trace writes the run trace: one JSON object per line, kept apart from the
// AuditEvent chain. It holds structured diagnostics only: host codes,
// counts, validated names. No free text from the CLI is kept (its result
// may echo the prompt or anything else), nor the raw stream, capability,
// environment, stderr content, prompt text or authentication value.
func (h *Host) trace(b *toolbridge.Binding, prompt string, started time.Time, gateCode string, res RunResult) (string, error) {
	if err := os.MkdirAll(h.TraceDir, 0o700); err != nil {
		return "", err
	}
	path := filepath.Join(h.TraceDir, fmt.Sprintf("%s-%d.jsonl", b.Session.TaskID, started.UnixNano()))
	f, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
	if err != nil {
		return "", err
	}
	prepareError := ""
	if res.PrepareError != nil {
		prepareError = store.CodeOf(res.PrepareError) // STORE_ERROR when it has no code; never its text
	}
	sum := sha256.Sum256([]byte(prompt))
	enc := json.NewEncoder(f)
	errs := []error{enc.Encode(map[string]any{
		"event": "run_start", "at": started.Unix(), "task_id": b.Session.TaskID, "consumer_id": b.Session.ConsumerID,
		"cli": filepath.Base(h.CLI), "prompt_sha256": hex.EncodeToString(sum[:]), "gate_mode": h.Gate.Mode(),
		"limits": map[string]any{"line_bytes": h.Limits.Line, "stdout_bytes": h.Limits.Stdout, "stderr_bytes": h.Limits.Stderr,
			"total_seconds": h.Limits.Total.Seconds(), "assistant_messages": h.Limits.Assistant, "tool_calls": toolbridge.MaxCalls},
	}), enc.Encode(map[string]any{
		"event": "init", "gate": gateCode, "requested_model": h.model(), "model": res.Model, "task_started": res.TaskStarted, "prepare_error": prepareError,
	})}
	for _, c := range res.Calls {
		errs = append(errs, enc.Encode(map[string]any{"event": "tool_call", "tool": c.Tool, "product_id": c.ProductID, "outcome": c.Outcome}))
	}
	errs = append(errs, enc.Encode(map[string]any{
		"event": "run_end", "outcome": res.Report.Outcome, "detail": res.Report.Detail, "exit_code": res.Report.ExitCode,
		"messages": res.Report.Messages, "assistant_messages": res.Report.Assistant, "num_turns": res.Report.NumTurns,
		"usage": usageCounts(res.Report.Usage), "stderr_bytes": res.Report.StderrBytes,
		"tool_attempts": res.Attempts, "not_ready_refusals": res.NotReady,
		"store_end": res.StoreEnd, "store_end_error": res.EndError,
		"result_chars": utf8.RuneCountInString(res.Report.Result), // the text itself is never kept
	}), f.Close())
	return path, errors.Join(errs...)
}
