package main

// Tests of GET /api/tasks (B1-06 slice 3.3, docs/demo-operation-design.md
// section 14): who sees which tasks, the order, tasks the current registry
// no longer names, and that listing writes nothing.

import (
	"encoding/json"
	"net/http"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/SIMPLYBOYS/agent-payment-authority/internal/contracts"
)

func (h *host) taskList(token string, mod ...func(*http.Request)) (int, []string) {
	h.t.Helper()
	code, body, _ := h.get("/api/tasks", token, mod...)
	var out struct {
		Tasks []struct {
			TaskID     string `json:"task_id"`
			ConsumerID string `json:"consumer_id"`
			Status     string `json:"status"`
		} `json:"tasks"`
	}
	json.Unmarshal(body, &out)
	var ids []string
	for _, t := range out.Tasks {
		ids = append(ids, t.TaskID)
	}
	return code, ids
}

func TestTaskList(t *testing.T) {
	h := startHost(t, hostOpts{runs: []planRun{{TaskID: "task-a", Scenario: mainline(t)}, {TaskID: "task-b", Scenario: mainline(t), BudgetInitial: "250000"}},
		expiryEvery: time.Hour})
	h.waitRuns()
	for name, c := range map[string]struct {
		token string
		mod   func(*http.Request)
		code  int
		ids   []string
	}{
		"viewer A":                 {h.tokens["viewer:consumer-a"], nil, 200, []string{"task-a"}},
		"viewer B":                 {h.tokens["viewer:consumer-b"], nil, 200, []string{"task-b"}},
		"operator":                 {h.tokens["operator"], nil, 403, nil},
		"no token":                 {"", nil, 401, nil},
		"token in the URL":         {"", func(r *http.Request) { r.URL.RawQuery = "token=" + h.tokens["viewer:consumer-a"] }, 401, nil},
		"viewer A, foreign Origin": {h.tokens["viewer:consumer-a"], func(r *http.Request) { r.Header.Set("Origin", "http://evil.example") }, 403, nil},
	} {
		var mods []func(*http.Request)
		if c.mod != nil {
			mods = append(mods, c.mod)
		}
		code, ids := h.taskList(c.token, mods...)
		if code != c.code || !slices.Equal(ids, c.ids) {
			t.Errorf("%s: %d %v, want %d %v", name, code, ids, c.code, c.ids)
		}
	}
	if code, ids := h.taskList(h.tokens["host"]); code != 200 || len(ids) != 2 || !slices.Contains(ids, "task-a") || !slices.Contains(ids, "task-b") {
		t.Errorf("host: %d %v", code, ids)
	}
	// Listing writes nothing.
	before := map[string]string{}
	for _, id := range []string{"task-a", "task-b"} {
		_, b, _ := h.get("/api/tasks/"+id+"/events", h.tokens["host"])
		before[id] = string(b)
	}
	for range 20 {
		h.taskList(h.tokens["viewer:consumer-a"])
		h.taskList(h.tokens["host"])
	}
	for id, b := range before {
		if _, now, _ := h.get("/api/tasks/"+id+"/events", h.tokens["host"]); string(now) != b {
			t.Errorf("%s changed after listing", id)
		}
	}
	h.stop()

	// A newer task comes first; a task the current registry no longer names
	// stays listed for its own consumer, by its stored owner.
	h = startHost(t, hostOpts{dir: h.dir, runs: []planRun{{TaskID: "task-a-next", Scenario: mainline(t)}}, expiryEvery: time.Hour})
	h.waitRuns()
	if _, ids := h.taskList(h.tokens["viewer:consumer-a"]); !slices.Equal(ids, []string{"task-a-next", "task-a"}) {
		t.Errorf("newest first: %v", ids)
	}
	h.stop()
	var reg contracts.RegistryConfig
	raw, _ := os.ReadFile("testdata/contracts/registry.json")
	contracts.Decode(raw, &reg)
	var keep []contracts.Task
	for _, tk := range reg.Tasks {
		if tk.TaskID != "task-a-next" {
			keep = append(keep, tk)
		}
	}
	reg.Tasks = keep
	small, _ := json.Marshal(reg)
	p := filepath.Join(t.TempDir(), "registry.json")
	os.WriteFile(p, small, 0o600)
	h = startHost(t, hostOpts{dir: h.dir, expiryEvery: time.Hour, args: []string{"--registry", p}})
	defer h.stop()
	if _, ids := h.taskList(h.tokens["viewer:consumer-a"]); !slices.Equal(ids, []string{"task-a-next", "task-a"}) {
		t.Errorf("after the registry dropped task-a-next: %v", ids)
	}
	if _, ids := h.taskList(h.tokens["viewer:consumer-b"]); strings.Contains(strings.Join(ids, ","), "task-a") {
		t.Errorf("viewer B sees A's tasks: %v", ids)
	}
}
