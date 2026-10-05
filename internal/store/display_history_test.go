package store

// Store-built histories for the Dashboard's adapter (B1-06 delivery 2, R1):
// legal multi-purchase tasks written through the real Store, saved in the
// shape GET /api/tasks/{id} and /events return (cmd/demo), and read by
// web/test/adapter.test.mjs. Regenerate with:
//   go test ./internal/store -run TestDisplayHistories -update-histories
// TestSeedDisplayDB writes such a task into a demo database for the browser
// test, only when APA_SEED_DB names the file.

import (
	"encoding/json"
	"flag"
	"os"
	"testing"
	"time"

	"github.com/SIMPLYBOYS/agent-payment-authority/internal/contracts"
)

var updateHistories = flag.Bool("update-histories", false, "rewrite testdata/demo/display-histories.json")

const historiesFile = "../../testdata/demo/display-histories.json"

// displayEvent and displayTask mirror cmd/demo's eventView and taskReply.
type displayEvent struct {
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

type displayTask struct {
	TaskID             string         `json:"task_id"`
	ConsumerID         string         `json:"consumer_id"`
	AgentID            string         `json:"agent_id"`
	WalletID           string         `json:"wallet_id"`
	Payer              string         `json:"payer"`
	WalletEvidenceMode string         `json:"wallet_evidence_mode"`
	Status             string         `json:"status"`
	DenyReason         string         `json:"deny_reason,omitempty"`
	AsOfSequence       uint64         `json:"as_of_sequence"`
	AsOfObservedAt     uint64         `json:"as_of_observed_at"`
	ServedAt           uint64         `json:"served_at"`
	Purchases          []PurchaseView `json:"purchases"`
	PaymentCapability  string         `json:"payment_capability"`
}

type displayHistory struct {
	Note   string      `json:"note"`
	Task   displayTask `json:"task"`
	Events struct {
		TaskID       string         `json:"task_id"`
		AsOfSequence uint64         `json:"as_of_sequence"`
		Events       []displayEvent `json:"events"`
	} `json:"events"`
}

func display(t *testing.T, s *Store, task string, now uint64, note string) displayHistory {
	t.Helper()
	rec, err := s.ReadTask(task, func(string) bool { return true })
	if err != nil {
		t.Fatal(err)
	}
	b, v := rec.Binding, rec.Task
	h := displayHistory{Note: note}
	h.Task = displayTask{TaskID: b.TaskID, ConsumerID: b.ConsumerID, AgentID: b.AgentID, WalletID: b.WalletID, Payer: b.Payer,
		WalletEvidenceMode: b.WalletEvidenceMode, Status: v.Status, DenyReason: v.DenyReason, AsOfSequence: v.AsOfSequence,
		AsOfObservedAt: v.AsOfObservedAt, ServedAt: now, Purchases: v.Purchases, PaymentCapability: "unavailable"}
	h.Events.TaskID = b.TaskID
	h.Events.Events = []displayEvent{}
	for _, e := range rec.Events {
		d := displayEvent{EventID: e.EventID, EventType: e.EventType, Sequence: e.Sequence, PrevEventHash: e.PrevEventHash,
			Code: e.Payload.Code, Decision: e.Payload.Decision, EvidenceMode: e.EvidenceMode, Source: e.Source, ObservedAt: e.ObservedAt}
		if e.Scope != nil {
			d.PurchaseOrderID, d.IntentHash = e.Scope.PurchaseOrderID, e.Scope.IntentHash
		}
		h.Events.Events = append(h.Events.Events, d)
		h.Events.AsOfSequence = e.Sequence
	}
	return h
}

// twoAsks writes purchase 1 (Ask), then purchase 2 (Ask); expireFirst
// chooses whether purchase 1 expires before purchase 2 is submitted
// (expired_then_ask) or after (ask_ask_then_expire). Times are relative to
// the clock's start; deadline is the task's approval deadline.
func twoAsks(t *testing.T, e *env, s *Store, deadline uint64, expireFirst bool) {
	t.Helper()
	setup := e.setup("task-a")
	setup.ApprovalDeadlineSeconds = deadline
	setup.Credential.ValidUntil = e.clock.Now() + 86400 // valid for a day of this clock, whatever its start
	if err := s.StartTask(setup); err != nil {
		t.Fatal(err)
	}
	if r := submit(t, s, sessA, e.base); r.Purchase.Decision.PolicyVerdict != "Ask" {
		t.Fatalf("purchase 1: %+v", r.Purchase.Decision)
	}
	if expireFirst {
		e.clock.Add(deadline + 1)
		if err := s.ExpireDue(); err != nil {
			t.Fatal(err)
		}
		submit(t, s, sessA, variant(t, e.base, 2))
		return
	}
	e.clock.Add(deadline * 2 / 3)
	submit(t, s, sessA, variant(t, e.base, 2))
	e.clock.Add(deadline/3 + 1) // past purchase 1's deadline only
	if err := s.ExpireDue(); err != nil {
		t.Fatal(err)
	}
}

func TestDisplayHistories(t *testing.T) {
	out := map[string]displayHistory{}
	for name, first := range map[string]bool{"expired_then_ask": true, "ask_ask_then_expire": false} {
		e := newEnv(t)
		s := e.open()
		twoAsks(t, e, s, 30, first)
		h := display(t, s, "task-a", e.clock.Now(), "written through the Store by internal/store/display_history_test.go")
		ps := h.Task.Purchases
		if len(ps) != 2 || h.Task.Status != "awaiting_approval" || ps[0].ApprovalClosed != "expired" || ps[1].ApprovalClosed != "" || ps[1].Decision.PolicyVerdict != "Ask" {
			t.Fatalf("%s: %+v", name, h.Task)
		}
		out[name] = h
	}
	got, _ := json.MarshalIndent(out, "", "  ")
	if *updateHistories {
		if err := os.WriteFile(historiesFile, append(got, '\n'), 0o644); err != nil {
			t.Fatal(err)
		}
		return
	}
	want, err := os.ReadFile(historiesFile)
	if err != nil || string(want) != string(got)+"\n" {
		t.Fatalf("%s is not what the Store writes; rerun with -update-histories (%v)", historiesFile, err)
	}
}

// TestSeedDisplayDB writes task-a with purchase 1 expired and purchase 2 an
// open Ask (approval deadline 600 s, purchase 2 decided 79 s ago), for the
// browser test. It runs only with APA_SEED_DB set. The fixture purchase's
// EIP-3009 window (valid_after..valid_before, 600 s) is fixed in the past, so
// each purchase gets a window of the same width around the time it is
// submitted; nothing else of the fixture changes.
func TestSeedDisplayDB(t *testing.T) {
	path := os.Getenv("APA_SEED_DB")
	if path == "" {
		t.Skip("APA_SEED_DB not set")
	}
	now := uint64(time.Now().Unix())
	e := newEnv(t)
	e.path = path
	e.clock.Set(now - 681)
	s, err := Open(Config{Path: e.path, Registry: e.reg, Offers: e.offers, Clock: e.clock.clock()})
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	window := func(p contracts.PurchaseOrder, after uint64) contracts.PurchaseOrder {
		p.Intent.ValidAfter, p.Intent.ValidBefore = after, after+600
		return p
	}
	setup := e.setup("task-a")
	setup.ApprovalDeadlineSeconds = 600
	setup.Credential.ValidUntil = now + 86400
	if err := s.StartTask(setup); err != nil {
		t.Fatal(err)
	}
	if r := submit(t, s, sessA, window(e.base, now-700)); r.Purchase.Decision.PolicyVerdict != "Ask" {
		t.Fatalf("purchase 1: %+v", r.Purchase.Decision)
	}
	e.clock.Set(now - 80) // past purchase 1's deadline (now - 81)
	if err := s.ExpireDue(); err != nil {
		t.Fatal(err)
	}
	e.clock.Set(now - 79)
	if r := submit(t, s, sessA, window(variant(t, e.base, 2), now-100)); r.Purchase.Decision.PolicyVerdict != "Ask" {
		t.Fatalf("purchase 2: %+v", r.Purchase.Decision)
	}
	if v := task(t, s, sessA); v.Status != "awaiting_approval" || len(v.Purchases) != 2 || v.Purchases[0].ApprovalClosed != "expired" {
		t.Fatalf("seeded: %+v", v)
	}
}
