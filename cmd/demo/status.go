package main

import (
	"net/http"
	"runtime/debug"
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
	Runs              map[string]any   `json:"runs,omitempty"`           // with --scenarios: the active run and the interrupted ones
	Capacity          map[string]int   `json:"capacity,omitempty"`       // with --scenarios: the effective --max-runs and the consumers (a deployment setting)
	History           *historyInfo     `json:"history,omitempty"`        // history mode only
	Live              *liveInfo        `json:"live,omitempty"`           // live mode only
	Build             buildInfo        `json:"build"`                    // what this binary says of its source, as Go recorded it
	HumanApproval     *humanApproval   `json:"human_approval,omitempty"` // A2 (--approvals-signer) only
}

// humanApproval is what the page needs to offer the person's approval:
// the one origin it runs on and the sources it shows.
type humanApproval struct {
	Origin  string  `json:"origin"`
	Sources sources `json:"sources"`
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
		UnityBuild: unityBuildStatus{State: "missing", Files: []string{}}, PaymentCapability: a.capability(), ServedAt: a.clock(), History: a.historyFor(g), Live: a.live}
	if a.fake {
		out.FakeCLI.State = "ok"
	}
	out.Build = currentBuild()
	if a.live != nil {
		out.LiveCLI = "on" // its version and config directory passed at start; each run checks them again
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
	if a.approve != nil {
		out.HumanApproval = &humanApproval{Origin: "http://" + a.alias, Sources: a.approve.sources()}
	}
	if a.control != nil {
		out.Runs = a.control.summary()
		out.Capacity = map[string]int{"max_runs": a.control.maxRuns, "consumers": a.control.consumers}
	}
	reply(w, http.StatusOK, out)
}

// buildInfo is the binary's own record of its source (vcs.revision,
// vcs.modified, vcs.time), shown as recorded: a modified build says so.
// Empty when the binary carries none (go test, go run of a file).
type buildInfo struct {
	Revision string `json:"revision"`
	Modified string `json:"modified"` // "true" | "false" | ""
	Time     string `json:"time"`
}

func currentBuild() buildInfo {
	var b buildInfo
	if info, ok := debug.ReadBuildInfo(); ok {
		for _, s := range info.Settings {
			switch s.Key {
			case "vcs.revision":
				b.Revision = s.Value
			case "vcs.modified":
				b.Modified = s.Value
			case "vcs.time":
				b.Time = s.Value
			}
		}
	}
	return b
}

// capability is the host's payment capability: LocalAnvil with --payments.
func (a *api) capability() string {
	if a.payments {
		return LocalAnvil
	}
	return "unavailable"
}
