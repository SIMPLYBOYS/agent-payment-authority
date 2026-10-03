package buyer

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync/atomic"
	"time"

	"github.com/SIMPLYBOYS/agent-payment-authority/internal/contracts"
	"github.com/SIMPLYBOYS/agent-payment-authority/internal/toolbridge"
)

// C0 probe (docs/cli-poc.md section 17): start the CLI exactly as a live
// run would, but with no prompt and stream-json input held open and never
// written, and wait for its system/init. The run stays idle: MCP setup and
// tools/list are answered, no business tool runs, no Store is opened. What
// the init states is reported through a fixed whitelist; nothing it
// contains is kept as text.

// ProbeTimeout is how long the probe waits for the init. Cleanup after a
// stop takes up to two Grace periods more.
const ProbeTimeout = 30 * time.Second

// Probe statuses and the command's exit codes.
const (
	ProbeObserved = "observed"             // a strictly readable init stating the approved version (exit 0)
	ProbeNoInit   = "no_init_before_input" // none came before the wait ended (exit 3)
	ProbeFailed   = "failed"               // parse, version, process or host failure (exit 1)
)

// InitObserved stops a probe run on its init; it is the probe's own stop,
// not a contract code.
const InitObserved Outcome = "init_observed"

// ProbeArgv is Argv with no prompt and stream-json input: the CLI gets
// nothing to answer.
func ProbeArgv(mcpConfig string) []string {
	a := Argv("", mcpConfig)
	return append([]string{"-p", "--input-format", "stream-json"}, a[2:]...)
}

// ProbeSpec is everything the probe needs; the host builds all of it.
type ProbeSpec struct {
	CLI        string   // absolute path, version already checked
	BridgeCmd  []string // starts the MCP bridge
	Env        []string // the CLI's whole environment (CLIEnv)
	Limits     Limits   // Total is the wait for the init
	CLIVersion string   // approved claude_code_version, e.g. "2.1.288"
	TraceDir   string
}

// ProbeResult is all the probe reports, to the terminal and the trace.
// Every string is a fixed code or comes from a fixed list.
type ProbeResult struct {
	Status        string           `json:"status"`
	Code          string           `json:"code"`
	Outcome       Outcome          `json:"runner_outcome"`
	Detail        string           `json:"runner_detail"`
	ExitCode      int              `json:"cli_exit_code"`
	StdinWritten  int              `json:"stdin_bytes_written"` // by construction 0: nothing writes to it
	CallsRefused  int              `json:"business_calls_refused"`
	CallsExecuted int64            `json:"business_calls_executed"` // must stay 0
	Usage         string           `json:"usage"`
	Observation   *InitObservation `json:"observation"`
	Trace         string           `json:"trace,omitempty"`
}

// InitObservation is the whitelisted projection of an init.
type InitObservation struct {
	Fields         map[string]string `json:"fields"`        // known member -> absent | JSON type
	ExtraMembers   int               `json:"extra_members"` // members outside the known list, counted only
	ToolsCount     int               `json:"tools_count"`
	ToolsExpected  int               `json:"tools_expected"` // how many of the three apa tools are listed
	ToolsOther     int               `json:"tools_other"`
	MCPServers     int               `json:"mcp_servers_count"`
	APAServer      string            `json:"apa_server_status"`            // absent | SDK status | other
	PermissionMode string            `json:"permission_mode"`              // SDK mode | other | absent | not_string
	CLIVersion     string            `json:"cli_version"`                  // approved | other | absent | not_string
	Model          string            `json:"model"`                        // listed model | other | absent | not_string
	AuthSource     string            `json:"auth_source"`                  // display candidate | other | absent | not_string
	AuthSourceLen  int               `json:"auth_source_length,omitempty"` // only for "other" strings
	Plugins        *int              `json:"plugins_count"`                // nil: absent or not an array
	Skills         *int              `json:"skills_count"`
	Agents         *int              `json:"agents_count"`
}

// initMembers are the init members the probe reports on (Agent SDK
// reference, read 2026-10-03).
var initMembers = []string{"type", "subtype", "uuid", "session_id", "apiKeySource", "tools", "mcp_servers", "model",
	"permissionMode", "slash_commands", "output_style", "skills", "plugins", "agents", "claude_code_version", "cwd", "capabilities"}

// Fixed lists, from the Agent SDK TypeScript reference read 2026-10-03.
var (
	serverStatuses  = []string{"connected", "disconnected", "error"}
	permissionModes = []string{"default", "acceptEdits", "bypassPermissions", "plan", "dontAsk", "auto"}
	// authDisplayCandidates only decide what the probe may print; they are
	// never accepted (Gate.AcceptAuth stays empty for live). Source: the
	// first summary of the same SDK page listed these among ApiKeySource
	// values; the type could not be found when asked for verbatim, so
	// every value here is UNCONFIRMED.
	authDisplayCandidates = []string{"none", "environment", "keychain", "claude_app", "claude_app_org"}
)

func jsonType(raw json.RawMessage) string {
	switch t := strings.TrimSpace(string(raw)); {
	case t == "null":
		return "null"
	case t == "true" || t == "false":
		return "bool"
	case strings.HasPrefix(t, `"`):
		return "string"
	case strings.HasPrefix(t, "["):
		return "array"
	case strings.HasPrefix(t, "{"):
		return "object"
	default:
		return "number"
	}
}

// pick maps a member to a value from list, or a fixed label.
func pick(m map[string]json.RawMessage, k string, list []string) (string, int) {
	raw, ok := m[k]
	if !ok {
		return "absent", 0
	}
	var v string
	if json.Unmarshal(raw, &v) != nil {
		return "not_string", 0
	}
	if slices.Contains(list, v) {
		return v, 0
	}
	return "other", len(v)
}

func count(m map[string]json.RawMessage, k string) *int {
	var list []json.RawMessage
	if raw, ok := m[k]; !ok || json.Unmarshal(raw, &list) != nil || list == nil {
		return nil
	}
	n := len(list)
	return &n
}

// observe projects an init read by objectFields. No string of the init is
// copied out unless it equals an entry of a fixed list.
func observe(m map[string]json.RawMessage, version string) *InitObservation {
	o := &InitObservation{Fields: map[string]string{}}
	for _, k := range initMembers {
		if raw, ok := m[k]; ok {
			o.Fields[k] = jsonType(raw)
		} else {
			o.Fields[k] = "absent"
		}
	}
	for k := range m {
		if !slices.Contains(initMembers, k) {
			o.ExtraMembers++
		}
	}
	var tools []json.RawMessage
	if json.Unmarshal(m["tools"], &tools) == nil {
		o.ToolsCount = len(tools)
		want := initTools()
		seen := map[string]bool{}
		for _, raw := range tools {
			var t string
			if json.Unmarshal(raw, &t) == nil && slices.Contains(want, t) && !seen[t] {
				seen[t] = true
				o.ToolsExpected++
			} else {
				o.ToolsOther++
			}
		}
	}
	o.APAServer = "absent"
	var servers []json.RawMessage
	if json.Unmarshal(m["mcp_servers"], &servers) == nil {
		o.MCPServers = len(servers)
		for _, raw := range servers {
			f, err := objectFields(raw)
			var name string
			if err != nil || field(f, "name", &name) != nil || name != MCPServer {
				continue
			}
			o.APAServer, _ = pick(f, "status", serverStatuses)
		}
	}
	o.PermissionMode, _ = pick(m, "permissionMode", permissionModes)
	o.CLIVersion, _ = pick(m, "claude_code_version", []string{version})
	if o.CLIVersion == version {
		o.CLIVersion = "approved"
	}
	o.Model, _ = pick(m, "model", knownModels)
	o.AuthSource, o.AuthSourceLen = pick(m, "apiKeySource", authDisplayCandidates)
	o.Plugins, o.Skills, o.Agents = count(m, "plugins"), count(m, "skills"), count(m, "agents")
	return o
}

// refuseAll is the probe bridge's executor. The probe never arms its run,
// so the relay refuses business calls first; if this is ever reached, it
// refuses too, and the count shows it.
type refuseAll struct{ calls atomic.Int64 }

func (r *refuseAll) Call(_ context.Context, _ *toolbridge.Binding, tool, _ string) (toolbridge.ToolResult, toolbridge.CallRecord) {
	r.calls.Add(1)
	return toolbridge.ToolResult{Content: []toolbridge.TextContent{{Type: "text", Text: `{"refused":"PROBE_ONLY"}`}}, IsError: true},
		toolbridge.CallRecord{Tool: tool, Outcome: "PROBE_ONLY"}
}

// Probe runs the C0 probe once. It never retries, never sends input and
// never widens a check.
func Probe(ctx context.Context, p ProbeSpec) (ProbeResult, error) {
	res := ProbeResult{Usage: "unknown"}
	if ctx.Err() != nil {
		return res, errors.New("cancelled before the probe started; the CLI was not run")
	}
	exec := &refuseAll{}
	bridge, err := toolbridge.Listen(exec)
	if err != nil {
		return res, err
	}
	defer bridge.Close()
	dir, err := os.MkdirTemp("", "apa-probe-") // 0700
	if err != nil {
		return res, err
	}
	defer os.RemoveAll(dir)
	work := filepath.Join(dir, "work")
	if err := os.Mkdir(work, 0o700); err != nil {
		return res, err
	}
	b := &toolbridge.Binding{Session: contracts.Session{}} // no task: the probe buys nothing
	capability, err := bridge.Register(b)                  // idle, and never armed
	if err != nil {
		return res, err
	}
	defer bridge.Revoke(capability)
	cfg, _ := json.Marshal(map[string]any{"mcpServers": map[string]any{MCPServer: map[string]any{
		"type": "stdio", "command": p.BridgeCmd[0], "args": p.BridgeCmd[1:],
		"env": map[string]string{toolbridge.EnvSocket: bridge.SocketPath(), toolbridge.EnvCapability: capability},
	}}})
	cfgPath := filepath.Join(dir, "mcp.json")
	if err := os.WriteFile(cfgPath, cfg, 0o600); err != nil {
		return res, err
	}

	var obs *InitObservation
	watch := func(typ, subtype string, line []byte) (Outcome, string) {
		switch {
		case typ == "system" && strings.Contains(subtype, "hook"):
			return ConfigMismatch, "hook_message"
		case typ == "system" && subtype == "init":
			f, err := objectFields(line) // the reader already refused ambiguity
			if err != nil {
				return ParseError, "init_malformed"
			}
			obs = observe(f, p.CLIVersion)
			return InitObserved, "init"
		case typ == "system":
			return "", "" // other system notices: keep waiting
		default:
			return ConfigMismatch, "unexpected_message" // assistant, user, result or anything else before init
		}
	}
	lim := p.Limits
	lim.Assistant = 0 // any assistant message already stops the probe
	started := time.Now()
	rep := Run(ctx, Spec{Path: p.CLI, Args: ProbeArgv(cfgPath), Dir: work, Env: p.Env, Limits: lim,
		Watch: watch, Stopping: b.Disarm, HoldStdin: true})
	bridge.Revoke(capability) // waits for calls in flight
	res.Outcome, res.Detail, res.ExitCode = rep.Outcome, rep.Detail, rep.ExitCode
	res.CallsRefused, res.CallsExecuted = b.NotReady()+b.Attempts(), exec.calls.Load()
	res.Observation = obs

	switch {
	case rep.Outcome == InitObserved && obs != nil && obs.CLIVersion == "approved":
		res.Status, res.Code = ProbeObserved, "init_observed"
	case rep.Outcome == InitObserved:
		res.Status, res.Code = ProbeFailed, "init_cli_version"
	case rep.Outcome == Timeout:
		res.Status, res.Code = ProbeNoInit, "no_init_before_input"
	case rep.Detail != "":
		res.Status, res.Code = ProbeFailed, rep.Detail
	default:
		res.Status, res.Code = ProbeFailed, string(rep.Outcome)
	}
	res.Trace, err = probeTrace(p.TraceDir, started, res)
	return res, err
}

// probeTrace writes the result, the same projection the terminal shows.
func probeTrace(dir string, started time.Time, res ProbeResult) (string, error) {
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return "", err
	}
	path := filepath.Join(dir, fmt.Sprintf("probe-%d.jsonl", started.UnixNano()))
	f, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
	if err != nil {
		return "", err
	}
	enc := json.NewEncoder(f)
	errs := []error{enc.Encode(map[string]any{"event": "probe", "at": started.Unix(), "result": res}), f.Close()}
	return path, errors.Join(errs...)
}
