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
func Argv(prompt, mcpConfig string) []string {
	return []string{
		"-p", prompt,
		"--output-format", "stream-json", "--verbose",
		"--mcp-config", mcpConfig, "--strict-mcp-config",
		"--restricted", // ignores user, project and local settings files
		"--tools", "",  // no built-in tools at all
		"--permission-mode", "dontAsk",
		"--permission-prompts", "none", // anything that would prompt is denied
		"--allowedTools", strings.Join(initTools(), ","),
		"--model", Model, "--effort", Effort,
		"--no-session-persistence",
		"--disable-slash-commands",
	}
}

// Prompt is the host's fixed instruction for a buyer run; the model never
// edits it. apa-buyer and the demo host both use it.
const Prompt = "You are the buyer agent for one task. Use only the apa tools: list the products, get the quote for the Korean market brief, then request its purchase. " +
	"You cannot pay and will not receive the content; the host records the request and a human decides."

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
// 2.1.289 init reports is not verified (that is L1-c); the init gate still
// refuses any agent. Like the others, a parent value cannot replace it.
var fixedEnv = []string{"DISABLE_AUTOUPDATER=1", "CLAUDE_CODE_DISABLE_NONESSENTIAL_TRAFFIC=1", "CLAUDE_AGENT_SDK_DISABLE_BUILTIN_AGENTS=1"}

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
	task, wallet, err := h.Registry.Binding(taskID)
	if err != nil {
		return res, fmt.Errorf("task %q is not in the trusted registry", taskID)
	}
	b := &toolbridge.Binding{
		Session:  contracts.Session{ConsumerID: task.ConsumerID, AgentID: task.AgentID, TaskID: task.TaskID},
		WalletID: wallet.WalletID, From: wallet.From,
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
	capability, err := h.Bridge.Register(b)
	if err != nil {
		return res, err
	}
	defer h.Bridge.Revoke(capability)
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
	res.Report = Run(ctx, Spec{Path: h.CLI, Args: Argv(prompt, cfgPath), Dir: work, Env: h.Env, Limits: h.Limits,
		Watch: watch, Stopping: b.Disarm})
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
var knownModels = []string{Model, "claude-sonnet-5-5"}

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
		"cli": filepath.Base(h.CLI), "prompt_sha256": hex.EncodeToString(sum[:]),
		"limits": map[string]any{"line_bytes": h.Limits.Line, "stdout_bytes": h.Limits.Stdout, "stderr_bytes": h.Limits.Stderr,
			"total_seconds": h.Limits.Total.Seconds(), "assistant_messages": h.Limits.Assistant, "tool_calls": toolbridge.MaxCalls},
	}), enc.Encode(map[string]any{
		"event": "init", "gate": gateCode, "requested_model": Model, "model": res.Model, "task_started": res.TaskStarted, "prepare_error": prepareError,
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
