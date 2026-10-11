package toolbridge

import (
	"bufio"
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"net"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/SIMPLYBOYS/agent-payment-authority/internal/contracts"
)

// MaxCalls bounds the tool calls of one run.
const MaxCalls = 8

const callTimeout = 20 * time.Second

// Binding is one run's trusted identity, fixed by the host before the CLI
// starts. The model never supplies any of it.
//
// It is also the run's authority, which only moves forward:
//
//	idle     the CLI is starting; business tools are refused (RUN_NOT_READY)
//	armed    the host has checked the CLI's init and prepared the Store
//	stopped  the run is stopping; nothing new starts or commits
//	ended    revoked; calls in flight were cancelled and waited for
type Binding struct {
	Session  contracts.Session
	WalletID string
	From     string

	mu       sync.Mutex // guards the fields below and is held across a Commit
	state    runState
	calls    int // attempts that reached the budget, refused ones included
	notReady int // business calls refused before the run was armed
	log      []CallRecord
	ctx      context.Context // cancelled when the run stops
	stop     context.CancelFunc
	inflight sync.WaitGroup
}

type runState int

const (
	idle runState = iota
	armed
	stopped
	ended
)

var (
	// ErrRunEnded is Commit's refusal once the run is stopping or has ended.
	ErrRunEnded = errors.New("run has ended")
	// ErrNotReady is Commit's refusal before the run is armed.
	ErrNotReady = errors.New("run is not ready")
)

// Arm lets business tools run. It fails, and changes nothing, unless the
// run is still idle: a stopping or ended run is never armed again.
func (b *Binding) Arm() bool {
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.state != idle {
		return false
	}
	b.state = armed
	return true
}

// Disarm stops the run for good: no new call starts, no write commits, and
// calls in flight are cancelled. It waits for a write in progress (Commit
// holds the lock); Revoke later waits for the calls themselves.
func (b *Binding) Disarm() {
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.state < stopped {
		b.state = stopped
	}
	if b.stop != nil {
		b.stop()
	}
}

// begin admits one call; done must follow when it is admitted.
func (b *Binding) begin() (context.Context, string) {
	b.mu.Lock()
	defer b.mu.Unlock()
	switch {
	case b.ctx == nil || b.state >= stopped:
		return nil, "RUN_NOT_ACTIVE"
	case b.state == idle:
		b.notReady++
		return nil, "RUN_NOT_READY"
	}
	b.inflight.Add(1) // never after end has started waiting: the state moves first, under mu
	return b.ctx, ""
}

// Commit runs write, a call's one durable effect, only while the run is
// armed. Disarm and end take the same lock, so a run either stops before
// the write starts (and nothing is written) or after it has finished.
func (b *Binding) Commit(write func() error) error {
	b.mu.Lock()
	defer b.mu.Unlock()
	switch b.state {
	case idle:
		return ErrNotReady
	case stopped, ended:
		return ErrRunEnded
	}
	return write()
}

// Stopped reports whether the run is stopping or has ended.
func (b *Binding) Stopped() bool {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.state >= stopped
}

// end stops the run and returns once every call it admitted has finished.
func (b *Binding) end() {
	b.mu.Lock()
	b.state = ended
	if b.stop != nil {
		b.stop()
	}
	b.mu.Unlock()
	b.inflight.Wait()
}

// NotReady is the number of business calls refused before the run was armed.
func (b *Binding) NotReady() int {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.notReady
}

// Attempts is the number of tool calls the run tried, refused ones included.
func (b *Binding) Attempts() int {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.calls
}

// CallRecord is the run trace entry for one tool call: no arguments other
// than the validated product ID, no result text.
type CallRecord struct {
	Tool      string `json:"tool"`
	ProductID string `json:"product_id,omitempty"`
	Outcome   string `json:"outcome"` // a decision, "ok", or a refusal code
	// When the host took the call and when it had its answer, and the call's
	// number in this run (docs/multi-agent-demo-design.md 6.2). They are for
	// the host's timing record only and never part of the trace.
	ID        int       `json:"-"`
	Started   time.Time `json:"-"`
	Completed time.Time `json:"-"`
}

// Calls returns a copy of the run's call records: at most MaxCalls+1, the
// last one TOO_MANY_CALLS; later attempts are only counted (Attempts).
func (b *Binding) Calls() []CallRecord {
	b.mu.Lock()
	defer b.mu.Unlock()
	return append([]CallRecord(nil), b.log...)
}

func (b *Binding) record(r CallRecord) {
	b.mu.Lock()
	b.log = append(b.log, r)
	b.mu.Unlock()
}

// Executor runs a validated tool call for a binding.
type Executor interface {
	Call(ctx context.Context, b *Binding, tool, productID string) (ToolResult, CallRecord)
}

type relayRequest struct {
	Capability string          `json:"capability"`
	Tool       string          `json:"tool"`
	Arguments  json.RawMessage `json:"arguments"`
}

type relayResponse struct {
	Result ToolResult `json:"result"`
}

// Server is the host end: a Unix socket in a private directory. A request
// is honoured only for a registered, unrevoked capability, and only within
// that run's call budget.
type Server struct {
	exec Executor
	dir  string
	ln   net.Listener
	wg   sync.WaitGroup

	mu   sync.Mutex
	runs map[string]*Binding // sha256(capability) -> binding
}

// Listen starts the host end in a fresh 0700 directory under the system
// temporary directory.
func Listen(exec Executor) (*Server, error) {
	dir, err := os.MkdirTemp("", "apa-bridge-")
	if err != nil {
		return nil, err
	}
	ln, err := net.Listen("unix", filepath.Join(dir, "bridge.sock"))
	if err != nil {
		os.RemoveAll(dir)
		return nil, err
	}
	s := &Server{exec: exec, dir: dir, ln: ln, runs: map[string]*Binding{}}
	s.wg.Go(s.accept)
	return s, nil
}

// SocketPath is where the bridge process connects.
func (s *Server) SocketPath() string { return s.ln.Addr().String() }

func capKey(capability string) string {
	sum := sha256.Sum256([]byte(capability))
	return hex.EncodeToString(sum[:])
}

// Register starts accepting calls for b and returns its capability: 32
// random bytes, never logged, never shown to the model.
func (s *Server) Register(b *Binding) (string, error) {
	raw := make([]byte, 32)
	if _, err := rand.Read(raw); err != nil {
		return "", err
	}
	capability := hex.EncodeToString(raw)
	b.mu.Lock()
	b.ctx, b.stop = context.WithCancel(context.Background())
	b.mu.Unlock()
	s.mu.Lock()
	s.runs[capKey(capability)] = b
	s.mu.Unlock()
	return capability, nil
}

// Revoke ends the capability's run: later calls are refused, calls in
// flight are cancelled, and Revoke returns only once they have finished, so
// the run's records are complete and nothing more can be written for it.
// A registered run starts idle; the host arms it (Binding.Arm).
func (s *Server) Revoke(capability string) {
	s.mu.Lock()
	b := s.runs[capKey(capability)]
	delete(s.runs, capKey(capability))
	s.mu.Unlock()
	if b != nil {
		b.end()
	}
}

// Close ends every run, stops accepting, waits for calls in flight and
// removes the socket.
func (s *Server) Close() error {
	s.mu.Lock()
	runs := s.runs
	s.runs = map[string]*Binding{}
	s.mu.Unlock()
	for _, b := range runs {
		b.end()
	}
	err := s.ln.Close()
	s.wg.Wait()
	return errors.Join(err, os.RemoveAll(s.dir))
}

func (s *Server) accept() {
	for {
		conn, err := s.ln.Accept()
		if err != nil {
			return
		}
		s.wg.Go(func() { s.handle(conn) })
	}
}

func refusal(code, msg string) ToolResult {
	return textResult(map[string]string{"refused": code, "message": msg}, true)
}

func (s *Server) handle(conn net.Conn) {
	defer conn.Close()
	conn.SetDeadline(time.Now().Add(callTimeout))
	line, err := ReadLine(bufio.NewReader(conn), MaxMessageBytes)
	if err != nil {
		return
	}
	var req relayRequest
	result := refusal("BAD_REQUEST", "malformed relay request")
	if json.Unmarshal(line, &req) == nil {
		result = s.serve(req)
	}
	b, _ := json.Marshal(relayResponse{result})
	conn.Write(append(b, '\n'))
}

func (s *Server) serve(req relayRequest) ToolResult {
	s.mu.Lock()
	b := s.runs[capKey(req.Capability)]
	s.mu.Unlock()
	if b == nil {
		return refusal("RUN_NOT_ACTIVE", "this run has ended or was never started")
	}
	runCtx, refused := b.begin()
	switch refused {
	case "RUN_NOT_ACTIVE":
		return refusal(refused, "this run has ended or was never started")
	case "RUN_NOT_READY": // not recorded and not on the budget; counted (NotReady)
		return refusal(refused, "the host has not finished starting this run")
	}
	defer b.inflight.Done()
	started := time.Now() // monotonic: comparable with the host's other times
	name := req.Tool      // traced, so only ever a known name
	if !known(name) {
		name = "unknown"
	}
	b.mu.Lock()
	b.calls++
	n := b.calls
	b.mu.Unlock()
	record := func(r CallRecord) {
		r.ID, r.Started, r.Completed = n, started, time.Now()
		b.record(r)
	}
	if n > MaxCalls {
		if n == MaxCalls+1 { // one record says the budget ran out; the rest are only counted
			record(CallRecord{Tool: name, Outcome: "TOO_MANY_CALLS"})
		}
		return refusal("TOO_MANY_CALLS", "this run has used all its tool calls")
	}
	if name == "unknown" {
		record(CallRecord{Tool: name, Outcome: "UNKNOWN_TOOL"})
		return refusal("UNKNOWN_TOOL", "no such tool")
	}
	product, err := parseArgs(name, req.Arguments)
	if err != nil {
		record(CallRecord{Tool: name, Outcome: "INVALID_ARGUMENTS"})
		return refusal("INVALID_ARGUMENTS", err.Error())
	}
	ctx, cancel := context.WithTimeout(runCtx, callTimeout)
	defer cancel()
	result, rec := s.exec.Call(ctx, b, name, product)
	record(rec)
	return result
}

// The host passes these to the bridge process through the MCP config's env.
const (
	EnvSocket     = "APA_BRIDGE_SOCKET"
	EnvCapability = "APA_BRIDGE_CAPABILITY"
)

// RunBridge is the bridge process (apa-buyer mcp-bridge): MCP on stdin and
// stdout, every tool call relayed to the host with the run's capability.
func RunBridge(ctx context.Context, stdin io.Reader, stdout io.Writer, getenv func(string) string) error {
	socket, capability := getenv(EnvSocket), getenv(EnvCapability)
	if socket == "" || capability == "" {
		return errors.New("mcp-bridge: no host socket or capability; only the buyer host starts the bridge")
	}
	return Serve(ctx, stdin, stdout, func(ctx context.Context, name string, args json.RawMessage) (ToolResult, error) {
		return Dial(ctx, socket, capability, name, args)
	})
}

// Dial is the bridge-process end: one request per connection.
func Dial(ctx context.Context, socket, capability, tool string, args json.RawMessage) (ToolResult, error) {
	var d net.Dialer
	ctx, cancel := context.WithTimeout(ctx, callTimeout)
	defer cancel()
	conn, err := d.DialContext(ctx, "unix", socket)
	if err != nil {
		return ToolResult{}, err
	}
	defer conn.Close()
	if dl, ok := ctx.Deadline(); ok {
		conn.SetDeadline(dl)
	}
	if len(args) == 0 {
		args = json.RawMessage(`{}`)
	}
	b, err := json.Marshal(relayRequest{capability, tool, args})
	if err != nil {
		return ToolResult{}, err
	}
	if _, err := conn.Write(append(b, '\n')); err != nil {
		return ToolResult{}, err
	}
	line, err := ReadLine(bufio.NewReader(conn), MaxMessageBytes)
	if err != nil {
		return ToolResult{}, err
	}
	var resp relayResponse
	if err := json.Unmarshal(line, &resp); err != nil {
		return ToolResult{}, err
	}
	return resp.Result, nil
}
