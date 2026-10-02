// Package toolbridge connects one Claude Code run to the host's three
// purchase tools. The CLI starts the bridge as a stdio MCP server (Serve);
// the bridge relays every tool call over a private Unix socket to the host
// (Server), which knows the run only by an unguessable capability and runs
// the call against that run's trusted binding (Shop). Nothing the model
// writes names a consumer, task, wallet, URL or price.
//
// MCP is implemented directly (JSON-RPC 2.0, one message per line on stdio,
// spec 2025-06-18); there is no SDK dependency. Only initialize, ping,
// tools/list and tools/call are served.
package toolbridge

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"slices"
)

// MaxMessageBytes bounds one JSON-RPC line in either direction.
const MaxMessageBytes = 64 << 10

var protocolVersions = []string{"2025-06-18", "2025-03-26", "2024-11-05"} // newest first

// ErrTooLong is ReadLine's error for a line over its limit.
var ErrTooLong = errors.New("line exceeds its size limit")

type rpcRequest struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      json.RawMessage `json:"id,omitempty"`
	Method  string          `json:"method"`
	Params  json.RawMessage `json:"params,omitempty"`
}

type rpcError struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
}

type rpcResponse struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      json.RawMessage `json:"id"`
	Result  any             `json:"result,omitempty"`
	Error   *rpcError       `json:"error,omitempty"`
}

// TextContent is the only content type the tools return.
type TextContent struct {
	Type string `json:"type"`
	Text string `json:"text"`
}

// ToolResult is an MCP tools/call result. Structured results are also sent
// as text, as the spec recommends.
type ToolResult struct {
	Content           []TextContent `json:"content"`
	StructuredContent any           `json:"structuredContent,omitempty"`
	IsError           bool          `json:"isError"`
}

func textResult(v any, isError bool) ToolResult {
	b, _ := json.Marshal(v)
	return ToolResult{Content: []TextContent{{"text", string(b)}}, StructuredContent: v, IsError: isError}
}

// Caller runs one tool call. An error means the call could not be relayed at
// all; refusals come back as results with IsError.
type Caller func(ctx context.Context, name string, args json.RawMessage) (ToolResult, error)

// ReadLine reads one '\n'-terminated line of at most max bytes. A last line
// without '\n' is io.ErrUnexpectedEOF: a message cut short, never a message.
func ReadLine(r *bufio.Reader, max int) ([]byte, error) {
	var line []byte
	for {
		chunk, err := r.ReadSlice('\n')
		line = append(line, chunk...)
		if len(line) > max+1 {
			return nil, ErrTooLong
		}
		switch {
		case err == nil:
			return bytes.TrimRight(line, "\r\n"), nil
		case errors.Is(err, bufio.ErrBufferFull):
			continue
		case errors.Is(err, io.EOF) && len(line) > 0:
			return nil, io.ErrUnexpectedEOF
		default:
			return nil, err
		}
	}
}

// Serve speaks MCP on r and w until r ends. A line longer than
// MaxMessageBytes ends the session.
func Serve(ctx context.Context, r io.Reader, w io.Writer, call Caller) error {
	br := bufio.NewReader(r)
	enc := json.NewEncoder(w) // one line per message; JSON never contains a raw newline
	initialized := false
	reply := func(id json.RawMessage, result any, e *rpcError) error {
		return enc.Encode(rpcResponse{JSONRPC: "2.0", ID: id, Result: result, Error: e})
	}
	for {
		line, err := ReadLine(br, MaxMessageBytes)
		if errors.Is(err, io.EOF) {
			return nil
		}
		if err != nil {
			return err
		}
		if len(bytes.TrimSpace(line)) == 0 {
			continue
		}
		var req rpcRequest
		if json.Unmarshal(line, &req) != nil || req.JSONRPC != "2.0" || req.Method == "" {
			if err := reply(json.RawMessage("null"), nil, &rpcError{-32700, "parse error"}); err != nil {
				return err
			}
			continue
		}
		if len(req.ID) == 0 {
			continue // notifications (initialized, cancelled) need no answer
		}
		var res any
		var rerr *rpcError
		switch {
		case req.Method == "initialize":
			var p struct {
				ProtocolVersion string `json:"protocolVersion"`
			}
			json.Unmarshal(req.Params, &p)
			version := protocolVersions[0]
			if slices.Contains(protocolVersions, p.ProtocolVersion) {
				version = p.ProtocolVersion
			}
			initialized = true
			res = map[string]any{
				"protocolVersion": version,
				"capabilities":    map[string]any{"tools": map[string]any{}},
				"serverInfo":      map[string]any{"name": "apa-buyer-bridge", "version": "m1"},
			}
		case req.Method == "ping":
			res = map[string]any{}
		case !initialized:
			rerr = &rpcError{-32600, "initialize first"}
		case req.Method == "tools/list":
			res = map[string]any{"tools": Tools}
		case req.Method == "tools/call":
			var p struct {
				Name      string          `json:"name"`
				Arguments json.RawMessage `json:"arguments"`
			}
			if json.Unmarshal(req.Params, &p) != nil || !known(p.Name) {
				rerr = &rpcError{-32602, "unknown tool"}
				break
			}
			result, err := call(ctx, p.Name, p.Arguments)
			if err != nil {
				rerr = &rpcError{-32603, "the host did not answer"}
				break
			}
			res = result
		default:
			rerr = &rpcError{-32601, "method not found"}
		}
		if err := reply(req.ID, res, rerr); err != nil {
			return err
		}
	}
}
