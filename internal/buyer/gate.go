package buyer

import (
	"bytes"
	"encoding/json"
	"slices"

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
// recording of a real CLI 2.1.287 init has been checked against it.
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
func (g Gate) check(line []byte) string {
	if badJSON(line) {
		return "init_malformed"
	}
	var m struct {
		Tools          []string          `json:"tools"`
		MCPServers     []json.RawMessage `json:"mcp_servers"`
		PermissionMode *string           `json:"permissionMode"`
		APIKeySource   *string           `json:"apiKeySource"`
		Version        *string           `json:"claude_code_version"`
	}
	var present map[string]json.RawMessage
	if json.Unmarshal(line, &m) != nil || json.Unmarshal(line, &present) != nil {
		return "init_malformed"
	}
	want := initTools()
	if len(m.Tools) != len(want) {
		return "tools"
	}
	seen := map[string]bool{}
	for _, t := range m.Tools {
		if !slices.Contains(want, t) || seen[t] {
			return "tools"
		}
		seen[t] = true
	}
	if len(m.MCPServers) != 1 {
		return "mcp_servers"
	}
	var srv struct{ Name, Status *string }
	if json.Unmarshal(m.MCPServers[0], &srv) != nil {
		return "init_malformed"
	}
	if srv.Name == nil || *srv.Name != MCPServer || srv.Status == nil || *srv.Status != "connected" {
		return "mcp_servers"
	}
	if m.PermissionMode == nil || *m.PermissionMode != "dontAsk" {
		return "permission_mode"
	}
	if m.Version == nil || *m.Version != g.CLIVersion {
		return "cli_version"
	}
	for _, k := range []string{"plugins", "skills", "agents"} { // none is approved
		raw, ok := present[k]
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
	if m.APIKeySource == nil {
		return "auth_missing"
	}
	if !slices.Contains(g.AcceptAuth, *m.APIKeySource) {
		return "auth_unconfirmed"
	}
	return ""
}

// badJSON reports invalid JSON or any object that repeats a member name;
// encoding/json would silently keep the last copy.
func badJSON(raw []byte) bool {
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
				if err != nil || !ok || seen[key] {
					return true
				}
				seen[key] = true
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
