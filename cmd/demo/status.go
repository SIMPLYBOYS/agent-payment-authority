package main

import (
	"net/http"
	"sync"

	"github.com/SIMPLYBOYS/agent-payment-authority/internal/store"
)

// expiryHealth is what the host saw of its one expiry worker: the result
// and time of the last completed check, and the last error ever seen, kept
// apart so a recovery does not hide that an error happened. Nothing here
// runs expiry; the worker reports into it (Store.RunExpiryReporting).
type expiryHealth struct {
	mu          sync.Mutex
	checked     bool
	ok          bool
	checkedAt   uint64
	lastErr     string
	lastErrAt   uint64
	targetEvery float64
}

func (e *expiryHealth) observe(at uint64, err error) {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.checked, e.ok, e.checkedAt = true, err == nil, at
	if err != nil {
		code := store.Code(err)
		if code == "" {
			code = "STORE_ERROR" // an error without a Store code; its text is kept nowhere (the log prints this code too)
		}
		e.lastErr, e.lastErrAt = code, at
	}
}

// storeStatus: state is the latest completed check (unconfirmed until there
// is one); last_error is history and may be older than a later success.
type storeStatus struct {
	State               string  `json:"state"` // unconfirmed | ok | error
	CheckedAt           uint64  `json:"checked_at,omitempty"`
	LastError           string  `json:"last_error,omitempty"`
	LastErrorAt         uint64  `json:"last_error_at,omitempty"`
	TargetPeriodSeconds float64 `json:"target_period_seconds"` // a target, not a bound
}

func (e *expiryHealth) view() storeStatus {
	e.mu.Lock()
	defer e.mu.Unlock()
	s := storeStatus{State: "unconfirmed", LastError: e.lastErr, LastErrorAt: e.lastErrAt, TargetPeriodSeconds: e.targetEvery}
	if e.checked {
		s.State, s.CheckedAt = "error", e.checkedAt
		if e.ok {
			s.State = "ok"
		}
	}
	return s
}

type stateOnly struct {
	State string `json:"state"`
}

type unityBuildStatus struct {
	State string   `json:"state"` // ok | missing
	Files []string `json:"files"` // the listed build files present, names only
}

// statusReply is GET /api/host/status: fixed fields and codes only, no
// token, path or CLI text.
type statusReply struct {
	CallerRole        string           `json:"caller_role"` // host | operator: the caller's own role, no token
	HTTP              string           `json:"http"`
	Store             storeStatus      `json:"store"`
	FakeCLI           stateOnly        `json:"fake_cli"` // ok: its --version named fake-claude at start; off: no --fake-cli
	LiveCLI           string           `json:"live_cli"`
	UnityBuild        unityBuildStatus `json:"unity_build"`
	PaymentCapability string           `json:"payment_capability"`
	ServedAt          uint64           `json:"served_at"`
	Runs              map[string]any   `json:"runs,omitempty"`    // with --scenarios: the active run and the interrupted ones
	History           *historyInfo     `json:"history,omitempty"` // history mode only
}

// hostStatus answers the host and the operator tokens
// (docs/demo-operation-design.md sections 6.1, 13.8 R4). It reads what the host already knows and stats the build
// files; it calls nothing of the Store, so asking never writes.
func (a *api) hostStatus(w http.ResponseWriter, r *http.Request) {
	g, ok := a.authorize(w, r)
	if !ok {
		return
	}
	if g.Role != "host" && g.Role != "operator" {
		fail(w, http.StatusForbidden, "host_only")
		return
	}
	out := statusReply{CallerRole: g.Role, HTTP: "ok", Store: a.expiry.view(), FakeCLI: stateOnly{"off"}, LiveCLI: "off",
		UnityBuild: unityBuildStatus{State: "missing", Files: []string{}}, PaymentCapability: "unavailable", ServedAt: a.clock(), History: a.historyFor(g)}
	if a.fake {
		out.FakeCLI.State = "ok"
	}
	if a.unity != nil {
		for _, name := range []string{"WebGL.data", "WebGL.framework.js", "WebGL.loader.js", "WebGL.wasm"} {
			if fi, err := a.unity.Stat(name); err == nil && fi.Mode().IsRegular() {
				out.UnityBuild.Files = append(out.UnityBuild.Files, name)
			}
		}
		if len(out.UnityBuild.Files) == len(unityFiles) {
			out.UnityBuild.State = "ok"
		}
	}
	if a.control != nil {
		out.Runs = a.control.summary()
	}
	reply(w, http.StatusOK, out)
}
