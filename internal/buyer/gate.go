package buyer

import (
	"bytes"
	"encoding/json"
	"errors"
	"slices"
	"strings"
	"unicode"

	"github.com/SIMPLYBOYS/agent-payment-authority/internal/toolbridge"
)

// Gate is what a run's system/init must state before the host opens the
// Store and arms the tools. It checks the CLI's own account of itself: it
// is not OS isolation, and no field it reads proves how the CLI is billed.
//
// Fields and their basis (docs/cli-poc.md section 6): tools, mcp_servers
// (name, status "connected"), permissionMode, apiKeySource,
// claude_code_version, and the optional plugins, skills and agents, as the
// Agent SDK reference documents the init message (read 2026-10-03). No
// recording of a real CLI init (2.1.287, 2.1.288 or 2.1.289) has been checked against it.
type Gate struct {
	// CLIVersion is the claude_code_version the init must state.
	CLIVersion string
	// AcceptAuth lists the apiKeySource values accepted, exactly; empty
	// accepts none. No documented value proves a subscription login (a
	// Console, OAuth-token or profile login need no API key either), so the
	// live list stays empty until a value is decided with a basis. Unknown,
	// missing and unlisted values are refused; nothing widens the list.
	AcceptAuth []string
}

// initTools are the tool names init must list: these, each exactly once.
func initTools() []string {
	names := make([]string, len(toolbridge.Tools))
	for i, t := range toolbridge.Tools {
		names[i] = "mcp__" + MCPServer + "__" + t.Name
	}
	return names
}

// check returns "" when the init passes, else the first failing check's
// code, in a fixed order. Codes are host diagnostics, not contract codes.
// It reads the init by the same strict rule as the stream reader.
func (g Gate) check(line []byte) string {
	m, err := objectFields(line)
	if err != nil {
		return "init_malformed"
	}
	var (
		tools               []string
		servers             []json.RawMessage
		perm, auth, version *string
	)
	if errors.Join(field(m, "tools", &tools), field(m, "mcp_servers", &servers), field(m, "permissionMode", &perm),
		field(m, "apiKeySource", &auth), field(m, "claude_code_version", &version)) != nil {
		return "init_malformed"
	}
	want := initTools()
	if len(tools) != len(want) {
		return "tools"
	}
	seen := map[string]bool{}
	for _, t := range tools {
		if !slices.Contains(want, t) || seen[t] {
			return "tools"
		}
		seen[t] = true
	}
	if len(servers) != 1 {
		return "mcp_servers"
	}
	srv, err := objectFields(servers[0])
	var name, status *string
	if err != nil || errors.Join(field(srv, "name", &name), field(srv, "status", &status)) != nil {
		return "init_malformed"
	}
	if name == nil || *name != MCPServer || status == nil || *status != "connected" {
		return "mcp_servers"
	}
	if perm == nil || *perm != "dontAsk" {
		return "permission_mode"
	}
	if version == nil || *version != g.CLIVersion {
		return "cli_version"
	}
	for _, k := range []string{"plugins", "skills", "agents"} { // none is approved
		raw, ok := m[k]
		if !ok {
			continue // absent does not prove the CLI loaded none
		}
		var list []json.RawMessage
		if json.Unmarshal(raw, &list) != nil || list == nil {
			return "init_malformed"
		}
		if len(list) > 0 {
			return k
		}
	}
	if auth == nil {
		return "auth_missing"
	}
	if !slices.Contains(g.AcceptAuth, *auth) {
		return "auth_unconfirmed"
	}
	return ""
}

var (
	errAmbiguous = errors.New("a JSON object repeats a member name, exactly or up to case")
	errNotObject = errors.New("not a JSON object")
)

// objectFields is the one way a stream-json message is read, for
// classifying it and for every check: it refuses a message in which any
// object, at any depth, names a member twice, exactly or up to case
// (encoding/json would match either spelling to the same field, keeping
// the last), and returns the top-level members by exact name.
func objectFields(raw []byte) (map[string]json.RawMessage, error) {
	if !json.Valid(raw) {
		return nil, errNotObject // not JSON at all: a plain parse error
	}
	if ambiguous(raw) {
		return nil, errAmbiguous
	}
	var m map[string]json.RawMessage
	if json.Unmarshal(raw, &m) != nil || m == nil {
		return nil, errNotObject
	}
	return m, nil
}

// field decodes member k of m into dst when present, by exact name only.
func field(m map[string]json.RawMessage, k string, dst any) error {
	if raw, ok := m[k]; ok {
		return json.Unmarshal(raw, dst)
	}
	return nil
}

// ambiguous reports invalid JSON, or any object that repeats a member name
// under Unicode simple case folding (which covers encoding/json's own
// case-insensitive matching).
func ambiguous(raw []byte) bool {
	d := json.NewDecoder(bytes.NewReader(raw))
	d.UseNumber()
	var value func(depth int) bool
	value = func(depth int) bool {
		t, err := d.Token()
		if err != nil || depth > 64 {
			return true
		}
		delim, ok := t.(json.Delim)
		if !ok {
			return false
		}
		switch delim {
		case '{':
			seen := map[string]bool{}
			for d.More() {
				k, err := d.Token()
				key, ok := k.(string)
				if err != nil || !ok || seen[foldKey(key)] {
					return true
				}
				seen[foldKey(key)] = true
				if value(depth + 1) {
					return true
				}
			}
		case '[':
			for d.More() {
				if value(depth + 1) {
					return true
				}
			}
		}
		_, err = d.Token() // the closing delimiter
		return err != nil
	}
	return value(0)
}

// foldKey maps every rune to the smallest rune of its simple-folding orbit,
// so two names are equal up to case exactly when their keys are equal.
func foldKey(s string) string {
	var b strings.Builder
	for _, r := range s {
		low := r
		for f := unicode.SimpleFold(r); f != r; f = unicode.SimpleFold(f) {
			low = min(low, f)
		}
		b.WriteRune(low)
	}
	return b.String()
}
