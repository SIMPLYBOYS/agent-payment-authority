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
	"strings"
	"time"

	"github.com/SIMPLYBOYS/agent-payment-authority/internal/contracts"
	"github.com/SIMPLYBOYS/agent-payment-authority/internal/store"
	"github.com/SIMPLYBOYS/agent-payment-authority/internal/toolbridge"
)

// MCPServer is the server name in the CLI's MCP config; tools appear to the
// model as mcp__apa__<tool>.
const MCPServer = "apa"

// Argv is the CLI's whole argv after the program name. Flags were checked
// against `claude --help` of CLI 2.1.287; docs/cli-poc.md lists which ones
// are only documented. The prompt is the host's, never the model's.
func Argv(prompt, mcpConfig string) []string {
	allowed := make([]string, len(toolbridge.Tools))
	for i, t := range toolbridge.Tools {
		allowed[i] = "mcp__" + MCPServer + "__" + t.Name
	}
	return []string{
		"-p", prompt,
		"--output-format", "stream-json", "--verbose",
		"--mcp-config", mcpConfig, "--strict-mcp-config",
		"--tools", "", // no built-in tools at all
		"--allowedTools", strings.Join(allowed, ","),
		"--permission-mode", "dontAsk",
		"--setting-sources", "project", // the working directory is empty
		"--no-session-persistence",
		"--disable-slash-commands",
	}
}

// envAllowlist is all the CLI inherits: no API keys, cloud-provider or
// proxy settings, and nothing of the host's own.
var envAllowlist = []string{"HOME", "USER", "LOGNAME", "PATH", "TMPDIR", "LANG", "LC_ALL", "TERM"}

// CLIEnv is the CLI's whole environment: the allowlisted variables from
// getenv, then extra.
func CLIEnv(getenv func(string) string, extra ...string) []string {
	var env []string
	for _, k := range envAllowlist {
		if v := getenv(k); v != "" {
			env = append(env, k+"="+v)
		}
	}
	return append(env, extra...)
}

// Host runs task runs. One run is one CLI process bound to one task.
type Host struct {
	Store     *store.Store
	Registry  *contracts.Registry
	Bridge    *toolbridge.Server
	CLI       string   // executable, absolute; fixed by the operator
	BridgeCmd []string // executable and args that start the MCP bridge
	Env       []string // the CLI's whole environment (CLIEnv)
	Limits    Limits
	TraceDir  string
}

// RunResult is the host's account of one run. The Store holds the
// purchases; nothing here comes from the CLI's word alone except Report.
type RunResult struct {
	Report   Report
	Calls    []toolbridge.CallRecord
	Attempts int    // tool calls tried, refused ones included
	StoreEnd string // TASK_TIMED_OUT or TASK_CANCELLED when the run recorded one
	EndError string // the Store's refusal code when recording the end failed
	Trace    string
}

// Run starts one CLI process for taskID, a task the host has already
// started in the Store. The run's capability is revoked as soon as the
// process is gone, before anything is recorded.
func (h *Host) Run(ctx context.Context, taskID, prompt string) (RunResult, error) {
	var res RunResult
	task, wallet, err := h.Registry.Binding(taskID)
	if err != nil {
		return res, fmt.Errorf("task %q is not in the trusted registry", taskID)
	}
	b := &toolbridge.Binding{
		Session:  contracts.Session{ConsumerID: task.ConsumerID, AgentID: task.AgentID, TaskID: task.TaskID},
		WalletID: wallet.WalletID, From: wallet.From,
	}
	if _, err := h.Store.Task(b.Session); err != nil {
		return res, fmt.Errorf("task %s is not started in the Store: %s", taskID, store.Code(err))
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

	started := time.Now()
	res.Report = Run(ctx, Spec{Path: h.CLI, Args: Argv(prompt, cfgPath), Dir: work, Env: h.Env, Limits: h.Limits})
	// Revoke returns once the run's calls in flight have finished; after it
	// nothing more is written for this run and its records are complete.
	h.Bridge.Revoke(capability)
	res.Calls, res.Attempts = b.Calls(), b.Attempts()

	// Only these two outcomes have Store semantics; other failures leave the
	// task as it is, with the diagnosis in the run trace.
	var end error
	switch res.Report.Outcome {
	case Timeout:
		res.StoreEnd, end = "TASK_TIMED_OUT", h.Store.CLITimeout(taskID)
	case Cancelled:
		res.StoreEnd, end = "TASK_CANCELLED", h.Store.Cancel(taskID)
	}
	if end != nil {
		res.StoreEnd, res.EndError = "", store.Code(end)
	}
	res.Trace, err = h.trace(b, prompt, capability, started, res)
	return res, err
}

// trace writes the run trace: one JSON object per line, kept apart from the
// AuditEvent chain. It holds no capability, environment, stderr content or
// prompt text; CLI text is cleaned and marked untrusted.
func (h *Host) trace(b *toolbridge.Binding, prompt, capability string, started time.Time, res RunResult) (string, error) {
	if err := os.MkdirAll(h.TraceDir, 0o700); err != nil {
		return "", err
	}
	path := filepath.Join(h.TraceDir, fmt.Sprintf("%s-%d.jsonl", b.Session.TaskID, started.UnixNano()))
	f, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
	if err != nil {
		return "", err
	}
	scrub := func(s string, max int) string {
		return toolbridge.Clean(strings.ReplaceAll(s, capability, "[capability]"), max)
	}
	sum := sha256.Sum256([]byte(prompt))
	usage := any("unknown")
	if res.Report.Usage != nil {
		usage = json.RawMessage(scrub(string(res.Report.Usage), 4096))
		if !json.Valid(usage.(json.RawMessage)) {
			usage = "unparseable"
		}
	}
	enc := json.NewEncoder(f)
	errs := []error{enc.Encode(map[string]any{
		"event": "run_start", "at": started.Unix(), "task_id": b.Session.TaskID, "consumer_id": b.Session.ConsumerID,
		"cli": filepath.Base(h.CLI), "prompt_sha256": hex.EncodeToString(sum[:]),
		"limits": map[string]any{"line_bytes": h.Limits.Line, "stdout_bytes": h.Limits.Stdout, "stderr_bytes": h.Limits.Stderr,
			"total_seconds": h.Limits.Total.Seconds(), "tool_calls": toolbridge.MaxCalls},
	})}
	for _, c := range res.Calls {
		errs = append(errs, enc.Encode(map[string]any{"event": "tool_call", "tool": c.Tool, "product_id": c.ProductID, "outcome": c.Outcome}))
	}
	errs = append(errs, enc.Encode(map[string]any{
		"event": "run_end", "outcome": res.Report.Outcome, "exit_code": res.Report.ExitCode, "messages": res.Report.Messages,
		"num_turns": res.Report.NumTurns, "usage": usage, "stderr_bytes": res.Report.StderrBytes, "tool_attempts": res.Attempts,
		"store_end": res.StoreEnd, "store_end_error": res.EndError,
		"untrusted_cli_result": scrub(res.Report.Result, 2000),
	}), f.Close())
	return path, errors.Join(errs...)
}
