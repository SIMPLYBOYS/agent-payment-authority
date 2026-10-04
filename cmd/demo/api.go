package main

import (
	"crypto/rand"
	"crypto/subtle"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strings"

	"github.com/SIMPLYBOYS/agent-payment-authority/internal/contracts"
	"github.com/SIMPLYBOYS/agent-payment-authority/internal/store"
)

// grant is what a demo token may read: one consumer's tasks, or every task
// for the host role. It is the host's mapping, never a request's claim.
type grant struct {
	Role       string `json:"role"`                  // viewer | host
	ConsumerID string `json:"consumer_id,omitempty"` // viewers only
}

func (g grant) sees(consumerID string) bool { return g.Role == "host" || g.ConsumerID == consumerID }

type demoToken struct {
	grant
	Token string `json:"token"`
}

// newTokens makes a fresh token per registry consumer (viewer) and one for
// the host, and writes them to path (directory 0700, file 0600) by a rename,
// so no reader sees half a file. Tokens never go to the terminal or a log.
// The rename never replaces one of db's files: checked again just before
// it, when the database, its WAL and the lock exist.
func newTokens(cfg contracts.RegistryConfig, path, db string) ([]demoToken, error) {
	var out []demoToken
	add := func(g grant) error {
		b := make([]byte, 32)
		if _, err := rand.Read(b); err != nil {
			return err
		}
		out = append(out, demoToken{grant: g, Token: hex.EncodeToString(b)})
		return nil
	}
	for _, c := range cfg.Consumers {
		if err := add(grant{Role: "viewer", ConsumerID: c.ConsumerID}); err != nil {
			return nil, err
		}
	}
	if err := add(grant{Role: "host"}); err != nil {
		return nil, err
	}
	abs, err := filepath.Abs(path)
	if err != nil {
		return nil, err
	}
	dir := filepath.Dir(abs)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return nil, err
	}
	if fi, err := os.Stat(dir); err != nil || fi.Mode().Perm()&0o077 != 0 {
		return nil, fmt.Errorf("token directory %s must be private (0700)", dir)
	}
	body, _ := json.MarshalIndent(map[string]any{
		"note":   "Demo capabilities for this run of the demo host only; new ones are made at each start. Not a user login.",
		"tokens": out,
	}, "", "  ")
	tmp, err := os.CreateTemp(dir, ".demo-tokens-*")
	if err != nil {
		return nil, err
	}
	defer os.Remove(tmp.Name()) // a no-op after the rename
	if _, err := tmp.Write(body); err != nil {
		tmp.Close()
		return nil, err
	}
	if err := tmp.Close(); err != nil {
		return nil, err
	}
	if err := checkTokenPath(db, abs); err != nil {
		return nil, err
	}
	if err := os.Rename(tmp.Name(), abs); err != nil { // CreateTemp made it 0600
		return nil, err
	}
	return out, nil
}

// api is the authenticated read-only task API. Every handler reads the
// Store only: no StartTask, Submit, Cancel or ExpireDue.
type api struct {
	st     *store.Store
	tokens []demoToken
	addr   string // the bound address, the only Host and Origin accepted
	clock  store.Clock
	unity  *os.Root // the Unity Web build's Build/ directory; nil without a build
}

func (a *api) register(mux *http.ServeMux) {
	mux.HandleFunc("GET /api/tasks/{task_id}", a.task)
	mux.HandleFunc("GET /api/tasks/{task_id}/events", a.events)
}

func reply(w http.ResponseWriter, status int, v any) {
	h := w.Header()
	h.Set("Content-Type", "application/json")
	h.Set("Cache-Control", "no-store")
	h.Set("X-Content-Type-Options", "nosniff")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(v)
}

func fail(w http.ResponseWriter, status int, code string) {
	reply(w, status, map[string]string{"error": code})
}

// authorize checks Host, Origin and the bearer token, in that order. Only
// the Authorization header counts; nothing in the URL, the body or another
// header names who the caller is.
func (a *api) authorize(w http.ResponseWriter, r *http.Request) (grant, bool) {
	if r.Host != a.addr {
		fail(w, http.StatusForbidden, "forbidden_host")
		return grant{}, false
	}
	if o := r.Header.Get("Origin"); o != "" && o != "http://"+a.addr {
		fail(w, http.StatusForbidden, "forbidden_origin")
		return grant{}, false
	}
	tok, ok := strings.CutPrefix(r.Header.Get("Authorization"), "Bearer ")
	if ok {
		for _, t := range a.tokens {
			if subtle.ConstantTimeCompare([]byte(tok), []byte(t.Token)) == 1 {
				return t.grant, true
			}
		}
	}
	fail(w, http.StatusUnauthorized, "unauthenticated")
	return grant{}, false
}

// record reads the path's task as stored, for the caller's grant. The
// grant is applied to the task's stored consumer, so a task the current
// registry no longer lists stays readable by the host and by its own
// consumer's viewer only. Unknown, not visible and not started all answer
// the same 404, so a viewer learns nothing about another consumer's tasks.
func (a *api) record(w http.ResponseWriter, r *http.Request) (store.TaskRecord, bool) {
	g, ok := a.authorize(w, r)
	if !ok {
		return store.TaskRecord{}, false
	}
	rec, err := a.st.ReadTask(r.PathValue("task_id"), g.sees)
	if err != nil {
		if store.Code(err) == "OWNERSHIP_MISMATCH" {
			fail(w, http.StatusNotFound, "not_found")
		} else { // its text stays here
			fail(w, http.StatusServiceUnavailable, "store_unavailable")
		}
		return store.TaskRecord{}, false
	}
	return rec, true
}

// taskReply is GET /api/tasks/{task_id}: the task's stored binding and its
// committed projection, from one read, and fixed statements of what M1
// cannot do. Nothing comes from the current registry.
type taskReply struct {
	TaskID             string               `json:"task_id"`
	ConsumerID         string               `json:"consumer_id"`
	AgentID            string               `json:"agent_id"`
	WalletID           string               `json:"wallet_id"`
	Payer              string               `json:"payer"`
	WalletEvidenceMode string               `json:"wallet_evidence_mode"` // fixture: a reference, not a key
	Status             string               `json:"status"`
	DenyReason         string               `json:"deny_reason,omitempty"`
	AsOfSequence       uint64               `json:"as_of_sequence"`
	AsOfObservedAt     uint64               `json:"as_of_observed_at"`
	ServedAt           uint64               `json:"served_at"`
	Purchases          []store.PurchaseView `json:"purchases"`
	PaymentCapability  string               `json:"payment_capability"`
}

func (a *api) task(w http.ResponseWriter, r *http.Request) {
	rec, ok := a.record(w, r)
	if !ok {
		return
	}
	b, v := rec.Binding, rec.Task
	reply(w, http.StatusOK, taskReply{TaskID: b.TaskID, ConsumerID: b.ConsumerID, AgentID: b.AgentID, WalletID: b.WalletID, Payer: b.Payer,
		WalletEvidenceMode: b.WalletEvidenceMode, Status: v.Status, DenyReason: v.DenyReason, AsOfSequence: v.AsOfSequence,
		AsOfObservedAt: v.AsOfObservedAt, ServedAt: a.clock(), Purchases: v.Purchases, PaymentCapability: "unavailable"})
}

// eventView is the whitelisted projection of one stored event, copied as
// stored: nothing is recomputed or renamed.
type eventView struct {
	EventID         string                    `json:"event_id"`
	EventType       string                    `json:"event_type"`
	Sequence        uint64                    `json:"sequence"`
	PrevEventHash   string                    `json:"prev_event_hash,omitempty"`
	PurchaseOrderID string                    `json:"purchase_order_id,omitempty"`
	IntentHash      string                    `json:"intent_hash,omitempty"`
	Code            string                    `json:"code,omitempty"`
	Decision        *contracts.PolicyDecision `json:"decision,omitempty"`
	EvidenceMode    string                    `json:"evidence_mode"`
	Source          string                    `json:"source"`
	ObservedAt      uint64                    `json:"observed_at"`
}

type eventsReply struct {
	TaskID       string      `json:"task_id"`
	AsOfSequence uint64      `json:"as_of_sequence"`
	Events       []eventView `json:"events"`
}

func (a *api) events(w http.ResponseWriter, r *http.Request) {
	rec, ok := a.record(w, r)
	if !ok {
		return
	}
	out := eventsReply{TaskID: rec.Binding.TaskID, Events: []eventView{}}
	for _, e := range rec.Events {
		v := eventView{EventID: e.EventID, EventType: e.EventType, Sequence: e.Sequence, PrevEventHash: e.PrevEventHash,
			Code: e.Payload.Code, Decision: e.Payload.Decision, EvidenceMode: e.EvidenceMode, Source: e.Source, ObservedAt: e.ObservedAt}
		if e.Scope != nil {
			v.PurchaseOrderID, v.IntentHash = e.Scope.PurchaseOrderID, e.Scope.IntentHash
		}
		out.Events = append(out.Events, v)
		out.AsOfSequence = e.Sequence
	}
	reply(w, http.StatusOK, out)
}
