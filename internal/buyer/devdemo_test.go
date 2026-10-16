package buyer

import (
	"context"
	"encoding/json"
	"os"
	"strings"
	"testing"
)

// devInit is a passing init for Gate{CLIVersion: "2.1.301", AcceptAuth:
// ["none"]}, with members replaced (raw JSON) or removed (value "").
func devInit(t *testing.T, over map[string]string) []byte {
	t.Helper()
	m := map[string]json.RawMessage{
		"type": json.RawMessage(`"system"`), "subtype": json.RawMessage(`"init"`),
		"tools":               json.RawMessage(`["mcp__apa__list_products","mcp__apa__get_quote","mcp__apa__request_purchase"]`),
		"mcp_servers":         json.RawMessage(`[{"name":"apa","status":"connected"}]`),
		"permissionMode":      json.RawMessage(`"dontAsk"`),
		"apiKeySource":        json.RawMessage(`"none"`),
		"claude_code_version": json.RawMessage(`"2.1.301"`),
		"plugins":             json.RawMessage(`[{"name":"cc-plugin-agents-md","path":"p"},{"name":"cc-plugin-plugin-authoring","path":"p"}]`),
		"skills":              json.RawMessage(`[]`),
		"agents":              json.RawMessage(`[]`),
	}
	for k, v := range over {
		if v == "" {
			delete(m, k)
		} else {
			m[k] = json.RawMessage(v)
		}
	}
	raw, err := json.Marshal(m)
	if err != nil {
		t.Fatal(err)
	}
	return raw
}

// The development demo exception (docs/live-purchase-execution-decision.md
// 9): only with DevDemoPlugins does the gate accept a plugins list, and then
// only exactly the two named plugins, each once; every other check is the
// strict gate's. "none" in AcceptAuth applies to both modes.
func TestDevDemoGate(t *testing.T) {
	strict := Gate{CLIVersion: "2.1.301", AcceptAuth: []string{"none"}}
	dev := strict
	dev.DevDemoPlugins = true
	if strict.Mode() != "strict" || dev.Mode() != "dev_demo_plugins" {
		t.Fatalf("modes %q %q", strict.Mode(), dev.Mode())
	}
	for name, c := range map[string]struct {
		g    Gate
		over map[string]string
		code string
	}{
		// Strict mode is unchanged: the two plugins are refused; an empty or
		// absent list passes, and "none" is accepted there too.
		"strict: the two plugins": {strict, nil, "plugins"},
		"strict: no plugins":      {strict, map[string]string{"plugins": `[]`}, ""},
		"strict: plugins absent":  {strict, map[string]string{"plugins": ""}, ""},
		"strict: other auth":      {strict, map[string]string{"plugins": `[]`, "apiKeySource": `"ANTHROPIC_API_KEY"`}, "auth_unconfirmed"},
		// The exception: exactly the two, in either order.
		"dev: the two":           {dev, nil, ""},
		"dev: the two reversed":  {dev, map[string]string{"plugins": `[{"name":"cc-plugin-plugin-authoring"},{"name":"cc-plugin-agents-md"}]`}, ""},
		"dev: one more":          {dev, map[string]string{"plugins": `[{"name":"cc-plugin-agents-md"},{"name":"cc-plugin-plugin-authoring"},{"name":"cc-plugin-telemetry"}]`}, "plugins"},
		"dev: one missing":       {dev, map[string]string{"plugins": `[{"name":"cc-plugin-agents-md"}]`}, "plugins"},
		"dev: none":              {dev, map[string]string{"plugins": `[]`}, "plugins"},
		"dev: repeated":          {dev, map[string]string{"plugins": `[{"name":"cc-plugin-agents-md"},{"name":"cc-plugin-agents-md"}]`}, "plugins"},
		"dev: other name":        {dev, map[string]string{"plugins": `[{"name":"cc-plugin-agents-md"},{"name":"cc-plugin-telemetry"}]`}, "plugins"},
		"dev: other case":        {dev, map[string]string{"plugins": `[{"name":"CC-plugin-agents-md"},{"name":"cc-plugin-plugin-authoring"}]`}, "plugins"},
		"dev: member absent":     {dev, map[string]string{"plugins": ""}, "plugins"},
		"dev: names as strings":  {dev, map[string]string{"plugins": `["cc-plugin-agents-md","cc-plugin-plugin-authoring"]`}, "init_malformed"},
		"dev: name not a string": {dev, map[string]string{"plugins": `[{"name":1},{"name":"cc-plugin-plugin-authoring"}]`}, "init_malformed"},
		"dev: name missing":      {dev, map[string]string{"plugins": `[{"path":"p"},{"name":"cc-plugin-plugin-authoring"}]`}, "init_malformed"},
		"dev: not a list":        {dev, map[string]string{"plugins": `{"name":"cc-plugin-agents-md"}`}, "init_malformed"},
		"dev: null":              {dev, map[string]string{"plugins": `null`}, "init_malformed"},
		"dev: name twice in one": {dev, map[string]string{"plugins": `[{"name":"x","name":"cc-plugin-agents-md"},{"name":"cc-plugin-plugin-authoring"}]`}, "init_malformed"},
		// No other check is skipped under the exception.
		"dev: skills":       {dev, map[string]string{"skills": `["s"]`}, "skills"},
		"dev: agents":       {dev, map[string]string{"agents": `["Explore"]`}, "agents"},
		"dev: tool added":   {dev, map[string]string{"tools": `["mcp__apa__list_products","mcp__apa__get_quote","mcp__apa__request_purchase","Bash"]`}, "tools"},
		"dev: extra server": {dev, map[string]string{"mcp_servers": `[{"name":"apa","status":"connected"},{"name":"x","status":"connected"}]`}, "mcp_servers"},
		"dev: permission":   {dev, map[string]string{"permissionMode": `"default"`}, "permission_mode"},
		"dev: version":      {dev, map[string]string{"claude_code_version": `"2.1.289"`}, "cli_version"},
		"dev: other auth":   {dev, map[string]string{"apiKeySource": `"ANTHROPIC_API_KEY"`}, "auth_unconfirmed"},
		"dev: auth missing": {dev, map[string]string{"apiKeySource": ""}, "auth_missing"},
	} {
		if got := c.g.check(devInit(t, c.over)); got != c.code {
			t.Errorf("%s: %q, want %q", name, got, c.code)
		}
	}
}

// The run trace's run_start names the gate mode, so an exception run never
// passes for a strict one. Here, in the package of host.go, so the
// mutation check (which runs a mutation's tests in its file's package)
// reaches it.
func TestTraceGateMode(t *testing.T) {
	for _, dev := range []bool{false, true} {
		e := newHost(t)
		e.h.Gate.DevDemoPlugins = dev
		res := e.run(context.Background(), "task-a", "ask-mainline")
		raw, err := os.ReadFile(res.Trace)
		if err != nil {
			t.Fatal(err)
		}
		var start map[string]any
		json.Unmarshal([]byte(strings.SplitN(string(raw), "\n", 2)[0]), &start)
		want := map[bool]string{false: "strict", true: "dev_demo_plugins"}[dev]
		if start["event"] != "run_start" || start["gate_mode"] != want {
			t.Errorf("dev %v: run_start %v", dev, start)
		}
	}
}
