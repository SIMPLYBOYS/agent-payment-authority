// Package fakecli is a stand-in for the Claude Code CLI that runs no model.
// It accepts exactly the argv the buyer host builds, starts the MCP server
// from --mcp-config like the CLI does, speaks MCP to it over stdio, and
// prints stream-json, following a scripted scenario (testdata/cli/*.json).
// Fault steps produce the failures the host must survive.
//
// Its stream-json follows the documented message shapes (system/init,
// assistant, user, result, system/api_retry); it is not a recording of the
// real CLI. It does not emulate the CLI's permission layer: every scripted
// tool call reaches the bridge, so the host's own checks are what is tested.
package fakecli

import (
	"bufio"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"os/signal"
	"strings"
	"syscall"
	"time"
)

// Version is what --version prints. The buyer host refuses a --fake-cli
// whose version line lacks "fake-claude", so the real CLI is never run by
// mistake in fake mode.
const Version = "0.0.0 (fake-claude: no model, no network, not Claude Code)"

// Env names. Only the scenario is set in normal use; the other two let
// tests observe processes and capabilities.
const (
	EnvScenario = "APA_FAKE_CLI_SCENARIO"
	EnvPIDFile  = "APA_FAKE_CLI_PIDFILE"
	EnvCapFile  = "APA_FAKE_CLI_CAPFILE"
)

// Scenario is one scripted run.
type Scenario struct {
	Description string `json:"description"`
	Steps       []Step `json:"steps"`
}

// Step is one scripted action:
//
//	tool    call Tool (bare MCP name) with Args over MCP; print tool_use/tool_result
//	text    print an assistant text message
//	claim   print an assistant tool_use for Tool without calling MCP
//	result  print the result message (IsError, Subtype; default success);
//	        $CAPABILITY in Text becomes the run's capability (a leaking CLI)
//	retry   print system/api_retry with Error
//	raw     write Text to stdout as is
//	split   write Text to stdout in Bytes-sized chunks, 10 ms apart
//	flood   write one stream-json line of Bytes bytes
//	stderr  write Bytes bytes (or Text) to stderr
//	sleep   wait MS milliseconds
//	hang    block forever; IgnoreTerm ignores SIGTERM first
//	holder  start a grandchild that holds stdout and stderr open (Detach: new session)
//	leak    print the run's capability in an assistant message
//	savecap write the capability to $APA_FAKE_CLI_CAPFILE
//	exit    exit with Code now
type Step struct {
	Do         string          `json:"do"`
	Tool       string          `json:"tool,omitempty"`
	Args       json.RawMessage `json:"args,omitempty"`
	Text       string          `json:"text,omitempty"`
	IsError    bool            `json:"is_error,omitempty"`
	Subtype    string          `json:"subtype,omitempty"`
	Error      string          `json:"error,omitempty"`
	Bytes      int             `json:"bytes,omitempty"`
	MS         int             `json:"ms,omitempty"`
	Code       int             `json:"code,omitempty"`
	IgnoreTerm bool            `json:"ignore_term,omitempty"`
	Detach     bool            `json:"detach,omitempty"`
}

type run struct {
	out, errw  io.Writer
	enc        *json.Encoder
	getenv     func(string) string
	session    string
	turns      int
	server     *server
	capability string
	sawResult  bool
}

// Main is the fake CLI's main; it returns the exit code.
func Main(args []string, getenv func(string) string, stdout, stderr io.Writer) int {
	if len(args) == 1 && args[0] == "--version" {
		fmt.Fprintln(stdout, Version)
		return 0
	}
	cfgPath, err := parseArgs(args)
	if err != nil {
		fmt.Fprintln(stderr, "error:", err)
		return 1
	}
	raw, err := os.ReadFile(getenv(EnvScenario))
	if err != nil {
		fmt.Fprintln(stderr, "fake-claude: no scenario:", EnvScenario)
		return 1
	}
	var sc Scenario
	dec := json.NewDecoder(strings.NewReader(string(raw)))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&sc); err != nil {
		fmt.Fprintln(stderr, "fake-claude: bad scenario:", err)
		return 1
	}
	r := &run{out: stdout, errw: stderr, enc: json.NewEncoder(stdout), getenv: getenv, session: fmt.Sprintf("fake-%d", os.Getpid())}
	if r.server, r.capability, err = startServer(cfgPath); err != nil {
		fmt.Fprintln(stderr, "fake-claude: mcp server:", err)
		return 1
	}
	defer r.server.close()
	tools, err := r.server.handshake()
	if err != nil {
		fmt.Fprintln(stderr, "fake-claude: mcp handshake:", err)
		return 1
	}
	r.emit(map[string]any{"type": "system", "subtype": "init", "session_id": r.session, "model": "fake", "tools": tools,
		"mcp_servers": []any{map[string]any{"name": "apa", "status": "connected"}}, "permissionMode": "dontAsk", "apiKeySource": "none"})
	for i, s := range sc.Steps {
		if code, done := r.step(s); done {
			return code
		} else if code != 0 {
			fmt.Fprintf(stderr, "fake-claude: step %d (%s) failed\n", i, s.Do)
			return code
		}
	}
	return 0
}

// parseArgs accepts the host's argv and nothing else, and returns the MCP
// config path. Unknown flags fail like the real CLI's option parser.
func parseArgs(args []string) (string, error) {
	want := map[string]string{"--output-format": "stream-json", "--permission-mode": "dontAsk", "--tools": ""}
	valued := map[string]bool{"-p": true, "--mcp-config": true, "--allowedTools": true, "--setting-sources": true}
	flags := map[string]bool{"--verbose": true, "--strict-mcp-config": true, "--no-session-persistence": true, "--disable-slash-commands": true}
	seen := map[string]string{}
	for i := 0; i < len(args); i++ {
		a := args[i]
		_, isWant := want[a]
		switch {
		case flags[a]:
			seen[a] = ""
		case isWant || valued[a]:
			if i+1 >= len(args) {
				return "", fmt.Errorf("option '%s' argument missing", a)
			}
			i++
			seen[a] = args[i]
		default:
			return "", fmt.Errorf("unknown option '%s'", a)
		}
	}
	for k, v := range want {
		if got, ok := seen[k]; !ok || got != v {
			return "", fmt.Errorf("fake-claude needs %s %q", k, v)
		}
	}
	for _, k := range []string{"-p", "--mcp-config", "--verbose", "--strict-mcp-config"} {
		if _, ok := seen[k]; !ok {
			return "", fmt.Errorf("fake-claude needs %s", k)
		}
	}
	return seen["--mcp-config"], nil
}

func (r *run) emit(v any) { r.enc.Encode(v) }

func (r *run) assistant(content ...any) {
	r.turns++
	r.emit(map[string]any{"type": "assistant", "session_id": r.session,
		"message": map[string]any{"id": fmt.Sprintf("msg_fake_%d", r.turns), "type": "message", "role": "assistant", "model": "fake", "content": content}})
}

// step runs one step. done means exit now with code.
func (r *run) step(s Step) (code int, done bool) {
	switch s.Do {
	case "tool", "claim":
		id := fmt.Sprintf("toolu_fake_%d", r.turns+1)
		input := s.Args
		if len(input) == 0 {
			input = json.RawMessage(`{}`)
		}
		r.assistant(map[string]any{"type": "tool_use", "id": id, "name": "mcp__apa__" + s.Tool, "input": input})
		if s.Do == "claim" {
			return 0, false // printed only; never sent to the bridge
		}
		content, isError := r.server.callTool(s.Tool, input)
		r.emit(map[string]any{"type": "user", "session_id": r.session, "message": map[string]any{"role": "user",
			"content": []any{map[string]any{"type": "tool_result", "tool_use_id": id, "content": content, "is_error": isError}}}})
	case "text":
		r.assistant(map[string]any{"type": "text", "text": s.Text})
	case "leak":
		r.assistant(map[string]any{"type": "text", "text": "capability " + r.capability})
	case "savecap":
		if err := os.WriteFile(r.getenv(EnvCapFile), []byte(r.capability), 0o600); err != nil {
			return 1, false
		}
	case "result":
		sub := s.Subtype
		if sub == "" {
			sub = "success"
			if s.IsError {
				sub = "error_during_execution"
			}
		}
		r.emit(map[string]any{"type": "result", "subtype": sub, "is_error": s.IsError, "result": strings.ReplaceAll(s.Text, "$CAPABILITY", r.capability),
			"num_turns": r.turns, "session_id": r.session, "duration_ms": 1})
	case "retry":
		r.emit(map[string]any{"type": "system", "subtype": "api_retry", "attempt": 1, "max_retries": 1, "retry_delay_ms": 1, "error": s.Error})
	case "raw":
		io.WriteString(r.out, s.Text)
	case "split":
		for t := s.Text; t != ""; {
			n := min(max(s.Bytes, 1), len(t))
			io.WriteString(r.out, t[:n])
			t = t[n:]
			time.Sleep(10 * time.Millisecond)
		}
	case "flood":
		pad := max(s.Bytes-40, 0)
		fmt.Fprintf(r.out, `{"type":"assistant","pad":"%s"}`+"\n", strings.Repeat("x", pad))
	case "stderr":
		text := s.Text
		if s.Bytes > 0 {
			text = strings.Repeat("e", s.Bytes)
		}
		io.WriteString(r.errw, text)
	case "sleep":
		time.Sleep(time.Duration(s.MS) * time.Millisecond)
	case "hang":
		if s.IgnoreTerm {
			signal.Ignore(syscall.SIGTERM)
		}
		for { // not select{}: the runtime would end a goroutine-free block as a deadlock
			time.Sleep(time.Hour)
		}
	case "holder":
		return r.holder(s.Detach), false
	case "exit":
		return s.Code, true
	default:
		fmt.Fprintln(r.errw, "fake-claude: unknown step", s.Do)
		return 1, true
	}
	return 0, false
}

// holder starts a grandchild that inherits stdout and stderr and sleeps, the
// way a stray subprocess keeps a pipe open after its parent exits.
func (r *run) holder(detach bool) int {
	cmd := exec.Command("/bin/sleep", "300")
	cmd.Stdout, cmd.Stderr = os.Stdout, os.Stderr
	if detach {
		cmd.SysProcAttr = &syscall.SysProcAttr{Setsid: true}
	}
	if err := cmd.Start(); err != nil {
		return 1
	}
	if p := r.getenv(EnvPIDFile); p != "" {
		f, err := os.OpenFile(p, os.O_WRONLY|os.O_APPEND|os.O_CREATE, 0o600)
		if err != nil {
			return 1
		}
		fmt.Fprintln(f, cmd.Process.Pid)
		f.Close()
	}
	cmd.Process.Release()
	return 0
}

// server is the MCP server the CLI starts from --mcp-config.
type server struct {
	cmd  *exec.Cmd
	in   io.WriteCloser
	out  *bufio.Reader
	next int
}

func startServer(cfgPath string) (*server, string, error) {
	raw, err := os.ReadFile(cfgPath)
	if err != nil {
		return nil, "", err
	}
	var cfg struct {
		MCPServers map[string]struct {
			Type    string            `json:"type"`
			Command string            `json:"command"`
			Args    []string          `json:"args"`
			Env     map[string]string `json:"env"`
		} `json:"mcpServers"`
	}
	if err := json.Unmarshal(raw, &cfg); err != nil {
		return nil, "", err
	}
	sc, ok := cfg.MCPServers["apa"]
	if len(cfg.MCPServers) != 1 || !ok || sc.Type != "stdio" {
		return nil, "", errors.New("want exactly one stdio server named apa")
	}
	cmd := exec.Command(sc.Command, sc.Args...)
	cmd.Env = os.Environ()
	for k, v := range sc.Env {
		cmd.Env = append(cmd.Env, k+"="+v)
	}
	cmd.Stderr = os.Stderr
	in, err := cmd.StdinPipe()
	if err != nil {
		return nil, "", err
	}
	out, err := cmd.StdoutPipe()
	if err != nil {
		return nil, "", err
	}
	if err := cmd.Start(); err != nil {
		return nil, "", err
	}
	return &server{cmd: cmd, in: in, out: bufio.NewReader(out)}, sc.Env["APA_BRIDGE_CAPABILITY"], nil
}

type rpcErr struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
}

func (s *server) request(method string, params any) (json.RawMessage, *rpcErr, error) {
	s.next++
	msg, _ := json.Marshal(map[string]any{"jsonrpc": "2.0", "id": s.next, "method": method, "params": params})
	if _, err := s.in.Write(append(msg, '\n')); err != nil {
		return nil, nil, err
	}
	for {
		line, err := s.out.ReadBytes('\n')
		if err != nil {
			return nil, nil, err
		}
		var resp struct {
			ID     *int            `json:"id"`
			Result json.RawMessage `json:"result"`
			Error  *rpcErr         `json:"error"`
		}
		if err := json.Unmarshal(line, &resp); err != nil {
			return nil, nil, err
		}
		if resp.ID != nil && *resp.ID == s.next {
			return resp.Result, resp.Error, nil
		}
	}
}

func (s *server) handshake() ([]string, error) {
	if _, e, err := s.request("initialize", map[string]any{"protocolVersion": "2025-06-18", "capabilities": map[string]any{},
		"clientInfo": map[string]any{"name": "fake-claude", "version": "0"}}); err != nil || e != nil {
		return nil, fmt.Errorf("initialize: %v %v", err, e)
	}
	note, _ := json.Marshal(map[string]any{"jsonrpc": "2.0", "method": "notifications/initialized"})
	if _, err := s.in.Write(append(note, '\n')); err != nil {
		return nil, err
	}
	res, e, err := s.request("tools/list", map[string]any{})
	if err != nil || e != nil {
		return nil, fmt.Errorf("tools/list: %v %v", err, e)
	}
	var list struct{ Tools []struct{ Name string } }
	if err := json.Unmarshal(res, &list); err != nil {
		return nil, err
	}
	var names []string
	for _, t := range list.Tools {
		names = append(names, "mcp__apa__"+t.Name)
	}
	return names, nil
}

// callTool returns the tool_result content the CLI would hand the model.
func (s *server) callTool(name string, args json.RawMessage) (any, bool) {
	res, e, err := s.request("tools/call", map[string]any{"name": name, "arguments": args})
	if err != nil {
		return "MCP server unavailable", true
	}
	if e != nil {
		return fmt.Sprintf("MCP error %d: %s", e.Code, e.Message), true
	}
	var r struct {
		Content json.RawMessage `json:"content"`
		IsError bool            `json:"isError"`
	}
	json.Unmarshal(res, &r)
	return r.Content, r.IsError
}

func (s *server) close() {
	s.in.Close()
	done := make(chan struct{})
	go func() { s.cmd.Wait(); close(done) }()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		s.cmd.Process.Kill()
		<-done
	}
}
