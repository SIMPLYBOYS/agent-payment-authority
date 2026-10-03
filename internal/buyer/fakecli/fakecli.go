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
	"crypto/sha256"
	"encoding/hex"
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

// InitVersion and AuthSource are the fake's own values for its init's
// claude_code_version and apiKeySource. They are labels of this fake, not
// values any real CLI is known to send.
const (
	InitVersion = "0.0.0" // the first field of Version, as a live gate derives it
	AuthSource  = "fake-claude-no-auth"
)

// Env names. Only the scenario is set in normal use; the other two let
// tests observe processes and capabilities. Without EnvScenario the fake
// reads <its argv[0]>.scenario.json, so a test can run it with the exact
// environment a live run gets.
const (
	EnvScenario = "APA_FAKE_CLI_SCENARIO"
	EnvPIDFile  = "APA_FAKE_CLI_PIDFILE"
	EnvCapFile  = "APA_FAKE_CLI_CAPFILE"
	// EnvStdinFile, when set, names a file that always holds the number of
	// stdin bytes the fake has received so far (C0: proof of zero input).
	EnvStdinFile = "APA_FAKE_CLI_STDINFILE"
	// EnvStdinMsgFile, when set, names a file holding what the fake made of
	// the complete stdin lines received so far: "lines=N user_messages=M
	// content_sha256=<hex of the first message's content>" (C0′).
	EnvStdinMsgFile = "APA_FAKE_CLI_STDINMSGFILE"
)

// Scenario is one scripted run. The init message is printed after the MCP
// handshake unless ManualInit, in which case only "init" steps print it.
type Scenario struct {
	Description string `json:"description"`
	ManualInit  bool   `json:"manual_init,omitempty"`
	IgnoreTerm  bool   `json:"ignore_term,omitempty"`   // from process start: only SIGKILL ends the fake
	NoStdinRead bool   `json:"no_stdin_read,omitempty"` // never read stdin: a CLI that ignores its input
	Steps       []Step `json:"steps"`
}

// Step is one scripted action:
//
//	tool    call Tool (bare MCP name) with Args over MCP; print tool_use/tool_result
//	text    print an assistant text message
//	claim   print an assistant tool_use for Tool without calling MCP
//	silent_tool  call Tool over MCP without printing anything (a CLI that
//	        calls tools on its own, outside any model turn)
//	result  print the result message (IsError, Subtype; default success)
//	retry   print system/api_retry with Error
//	raw     write Text to stdout as is
//	split   write Text to stdout in Bytes-sized chunks, 10 ms apart
//	flood   write one stream-json line of Bytes bytes
//	stderr  write Bytes bytes (or Text) to stderr
//	sleep   wait MS milliseconds
//	hang    block forever; IgnoreTerm ignores SIGTERM first
//	ignore_term  ignore SIGTERM from now on (only SIGKILL ends the fake)
//	holder  start a grandchild that holds stdout and stderr open (Detach: new session)
//	leak    print the run's capability in an assistant message
//	savecap write the capability to $APA_FAKE_CLI_CAPFILE
//	init    print the init message; Init members replace the defaults (null removes one)
//	wait_input  block until stdin delivers a byte (then go on) or ends (then exit 0
//	        without output): a CLI that waits for a user message before its init
//	wait_message  the same, but until one complete stdin line has arrived
//	close_stdin  close stdin (a CLI that closes its input)
//	exit    exit with Code now
//
// $CAPABILITY and $PROMPT in a result's Text, a tool's Args or an init
// override become the run's capability and the -p prompt (a CLI that leaks
// or echoes them).
//
// After the init is printed, a tool call refused with RUN_NOT_READY is sent
// again every 20 ms for up to 5 s, as a real CLI's first model round trip
// would give the host time to get ready.
type Step struct {
	Do         string                     `json:"do"`
	Tool       string                     `json:"tool,omitempty"`
	Args       json.RawMessage            `json:"args,omitempty"`
	Text       string                     `json:"text,omitempty"`
	IsError    bool                       `json:"is_error,omitempty"`
	Subtype    string                     `json:"subtype,omitempty"`
	Error      string                     `json:"error,omitempty"`
	Bytes      int                        `json:"bytes,omitempty"`
	MS         int                        `json:"ms,omitempty"`
	Code       int                        `json:"code,omitempty"`
	IgnoreTerm bool                       `json:"ignore_term,omitempty"`
	Detach     bool                       `json:"detach,omitempty"`
	Init       map[string]json.RawMessage `json:"init,omitempty"`
}

type run struct {
	out, errw  io.Writer
	enc        *json.Encoder
	getenv     func(string) string
	session    string
	turns      int
	server     *server
	capability string
	tools      []string
	input      <-chan struct{} // closed when stdin delivers its first byte
	message    <-chan struct{} // closed when stdin delivers its first complete line
	inputEnd   <-chan struct{} // closed when stdin ends
	model      string
	prompt     string
	permission string
	initDone   bool
}

// Main is the fake CLI's main; it returns the exit code.
func Main(args []string, getenv func(string) string, stdout, stderr io.Writer) int {
	if len(args) == 1 && args[0] == "--version" {
		fmt.Fprintln(stdout, Version)
		return 0
	}
	opts, err := parseArgs(args)
	if err != nil {
		fmt.Fprintln(stderr, "error:", err)
		return 1
	}
	scenarioPath := getenv(EnvScenario)
	if scenarioPath == "" {
		scenarioPath = os.Args[0] + ".scenario.json"
	}
	raw, err := os.ReadFile(scenarioPath)
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
	if sc.IgnoreTerm {
		signal.Ignore(syscall.SIGTERM)
	}
	r := &run{out: stdout, errw: stderr, enc: json.NewEncoder(stdout), getenv: getenv, session: fmt.Sprintf("fake-%d", os.Getpid()),
		model: opts["--model"], prompt: opts["prompt"], permission: opts["--permission-mode"]}
	if sc.NoStdinRead {
		r.input, r.message, r.inputEnd = make(chan struct{}), make(chan struct{}), make(chan struct{})
		if f := getenv(EnvStdinFile); f != "" {
			os.WriteFile(f, []byte("0"), 0o600)
		}
	} else {
		r.input, r.message, r.inputEnd = watchStdin(getenv(EnvStdinFile), getenv(EnvStdinMsgFile))
	}
	if r.server, r.capability, err = startServer(opts["--mcp-config"]); err != nil {
		fmt.Fprintln(stderr, "fake-claude: mcp server:", err)
		return 1
	}
	defer r.server.close()
	if r.tools, err = r.server.handshake(); err != nil {
		fmt.Fprintln(stderr, "fake-claude: mcp handshake:", err)
		return 1
	}
	if !sc.ManualInit {
		r.init(nil)
	}
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

// parseArgs accepts the host's argv and nothing else, and returns the
// values given. Unknown flags fail like the real CLI's option parser.
func parseArgs(args []string) (map[string]string, error) {
	want := map[string]string{"--output-format": "stream-json", "--permission-mode": "dontAsk", "--tools": "", "--permission-prompts": "none"}
	valued := map[string]bool{"--mcp-config": true, "--allowedTools": true, "--model": true, "--effort": true, "--input-format": true}
	flags := map[string]bool{"-p": true, "--verbose": true, "--strict-mcp-config": true, "--restricted": true, "--no-session-persistence": true, "--disable-slash-commands": true}
	seen := map[string]string{}
	for i := 0; i < len(args); i++ {
		a := args[i]
		_, isWant := want[a]
		switch {
		case flags[a]:
			seen[a] = ""
		case !strings.HasPrefix(a, "-"): // the prompt, as the CLI takes it after -p
			if _, dup := seen["prompt"]; dup {
				return nil, fmt.Errorf("unexpected argument '%s'", a)
			}
			seen["prompt"] = a
		case isWant || valued[a]:
			if i+1 >= len(args) {
				return nil, fmt.Errorf("option '%s' argument missing", a)
			}
			i++
			seen[a] = args[i]
		default:
			return nil, fmt.Errorf("unknown option '%s'", a)
		}
	}
	for k, v := range want {
		if got, ok := seen[k]; !ok || got != v {
			return nil, fmt.Errorf("fake-claude needs %s %q", k, v)
		}
	}
	if f, ok := seen["--input-format"]; ok && f != "stream-json" {
		return nil, fmt.Errorf("fake-claude takes --input-format stream-json only")
	}
	for _, k := range []string{"-p", "--mcp-config", "--verbose", "--strict-mcp-config", "--restricted"} {
		if _, ok := seen[k]; !ok {
			return nil, fmt.Errorf("fake-claude needs %s", k)
		}
	}
	return seen, nil
}

// init prints the init message: the fake's defaults, with overrides
// replacing members (a JSON null removes one). Field names follow the Agent
// SDK reference; the values are this fake's.
func (r *run) init(overrides map[string]json.RawMessage) {
	m := map[string]any{"type": "system", "subtype": "init", "session_id": r.session, "model": r.model, "tools": r.tools,
		"mcp_servers": []any{map[string]any{"name": "apa", "status": "connected"}}, "permissionMode": r.permission,
		"apiKeySource": AuthSource, "claude_code_version": InitVersion, "slash_commands": []any{}, "cwd": ".", "output_style": "default"}
	for k, v := range overrides {
		if string(v) == "null" {
			delete(m, k)
		} else {
			m[k] = json.RawMessage(r.subst(string(v)))
		}
	}
	r.emit(m)
	r.initDone = true
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
		input := json.RawMessage(r.subst(string(s.Args)))
		if len(input) == 0 {
			input = json.RawMessage(`{}`)
		}
		r.assistant(map[string]any{"type": "tool_use", "id": id, "name": "mcp__apa__" + s.Tool, "input": input})
		if s.Do == "claim" {
			return 0, false // printed only; never sent to the bridge
		}
		content, isError := r.server.callTool(s.Tool, input)
		for wait := 0; r.initDone && isError && notReady(content) && wait < 250; wait++ {
			time.Sleep(20 * time.Millisecond)
			content, isError = r.server.callTool(s.Tool, input)
		}
		r.emit(map[string]any{"type": "user", "session_id": r.session, "message": map[string]any{"role": "user",
			"content": []any{map[string]any{"type": "tool_result", "tool_use_id": id, "content": content, "is_error": isError}}}})
	case "silent_tool":
		input := json.RawMessage(r.subst(string(s.Args)))
		if len(input) == 0 {
			input = json.RawMessage(`{}`)
		}
		r.server.callTool(s.Tool, input)
	case "init":
		r.init(s.Init)
	case "wait_input":
		select {
		case <-r.input:
		case <-r.inputEnd:
			select {
			case <-r.input: // input came, then the end
			default:
				return 0, true
			}
		}
	case "wait_message":
		select {
		case <-r.message:
		case <-r.inputEnd:
			select {
			case <-r.message:
			default:
				return 0, true
			}
		}
	case "close_stdin":
		os.Stdin.Close()
	case "text":
		r.assistant(map[string]any{"type": "text", "text": s.Text})
	case "leak":
		r.assistant(map[string]any{"type": "text", "text": "capability " + r.capability})
	case "savecap": // to $APA_FAKE_CLI_CAPFILE, or <argv[0]>.cap where the environment is a live run's
		file := r.getenv(EnvCapFile)
		if file == "" {
			file = os.Args[0] + ".cap"
		}
		if err := os.WriteFile(file, []byte(r.capability), 0o600); err != nil {
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
		text := r.subst(s.Text)
		r.emit(map[string]any{"type": "result", "subtype": sub, "is_error": s.IsError, "result": text,
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
	case "ignore_term":
		signal.Ignore(syscall.SIGTERM)
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

// watchStdin reads stdin for the whole run, counting bytes into file (if
// set) after every read, starting with 0.
func watchStdin(file, msgFile string) (<-chan struct{}, <-chan struct{}, <-chan struct{}) {
	first, firstLine, end := make(chan struct{}), make(chan struct{}), make(chan struct{})
	record := func(n int) {
		if file != "" {
			os.WriteFile(file, []byte(fmt.Sprint(n)), 0o600)
		}
	}
	lines, users, digest := 0, 0, ""
	recordLines := func() {
		if msgFile != "" {
			os.WriteFile(msgFile, []byte(fmt.Sprintf("lines=%d user_messages=%d content_sha256=%s", lines, users, digest)), 0o600)
		}
	}
	record(0)
	recordLines()
	go func() {
		total, buf, pending := 0, make([]byte, 4096), []byte{}
		for {
			n, err := os.Stdin.Read(buf)
			if n > 0 {
				if total == 0 {
					close(first)
				}
				total += n
				record(total)
				pending = append(pending, buf[:n]...)
				for {
					i := strings.IndexByte(string(pending), '\n')
					if i < 0 {
						break
					}
					line := pending[:i]
					pending = pending[i+1:]
					if lines == 0 {
						close(firstLine)
					}
					lines++
					if content, ok := userMessage(line); ok {
						if users == 0 {
							sum := sha256.Sum256([]byte(content))
							digest = hex.EncodeToString(sum[:])
						}
						users++
					}
					recordLines()
				}
			}
			if err != nil {
				close(end)
				return
			}
		}
	}()
	return first, firstLine, end
}

// userMessage reports whether line is a stream-json user message of the
// SDKUserMessage shape (type "user", message.role "user", a string
// content, parent_tool_use_id present and null), and its content.
func userMessage(line []byte) (string, bool) {
	var m struct {
		Type    string `json:"type"`
		Message *struct {
			Role    string `json:"role"`
			Content string `json:"content"`
		} `json:"message"`
		Parent json.RawMessage `json:"parent_tool_use_id"`
	}
	if json.Unmarshal(line, &m) != nil || m.Type != "user" || m.Message == nil || m.Message.Role != "user" || string(m.Parent) != "null" {
		return "", false
	}
	return m.Message.Content, true
}

// subst puts the run's capability and prompt where a scenario says
// $CAPABILITY or $PROMPT: a CLI that leaks or echoes them.
func (r *run) subst(s string) string {
	return strings.NewReplacer("$CAPABILITY", r.capability, "$PROMPT", r.prompt).Replace(s)
}

// notReady reports a tool result refused with RUN_NOT_READY.
func notReady(content any) bool {
	b, _ := json.Marshal(content)
	return strings.Contains(string(b), `RUN_NOT_READY`)
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
