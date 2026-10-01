package store

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/SIMPLYBOYS/agent-payment-authority/internal/contracts"
)

// R1: the current offer book admits new purchases only. Accepted history
// replays after the offer is repriced or removed; conflicts stay conflicts.
func TestReplayIgnoresCurrentOfferBook(t *testing.T) {
	repriced := func(e *env) OfferBook {
		q := editQuote(t, e.base, "amount", "600000")
		return OfferBook{{q.Intent.HTTPMethod, q.Intent.ResourceURL}: {q.Quote}}
	}
	t.Run("repriced, restarted, replayed", func(t *testing.T) {
		e := newEnv(t)
		s := e.open()
		e.start(s, "task-a")
		first := submit(t, s, sessA, e.base)
		body := mustScalar(t, e.path, `SELECT body FROM purchases`)
		s.Close()
		e.offers = repriced(e)
		s = e.open()
		r := submit(t, s, sessA, e.base)
		if !r.Replay || r.Purchase.Decision != first.Purchase.Decision || r.Purchase.IntentHash != first.Purchase.IntentHash || mustScalar(t, e.path, `SELECT body FROM purchases`) != body {
			t.Fatalf("replay after repricing: %+v", r)
		}
	})
	t.Run("offer removed, task ended, restarted, replayed", func(t *testing.T) {
		e := newEnv(t)
		s := e.open()
		e.start(s, "task-a")
		denied := submit(t, s, sessA, e.denied)
		s.Close()
		e.offers = OfferBook{}
		s = e.open() // recovery does not consult the offer book
		before := fingerprint(t, e.path)
		r := submit(t, s, sessA, e.denied)
		if !r.Replay || r.Purchase.Decision != denied.Purchase.Decision || fingerprint(t, e.path) != before {
			t.Fatalf("replay of an ended task's purchase: %+v", r)
		}
	})
	t.Run("same key, new content outside the book: conflict", func(t *testing.T) {
		e := newEnv(t)
		s := e.open()
		e.start(s, "task-a")
		submit(t, s, sessA, e.base)
		before := fingerprint(t, e.path)
		_, err := s.Submit(sessA, raw(t, editQuote(t, e.base, "amount", "700000")))
		wantCode(t, err, "IDEMPOTENCY_CONFLICT")
		if fingerprint(t, e.path) != before {
			t.Fatal("conflict wrote")
		}
	})
	t.Run("new purchase of a removed offer: refused", func(t *testing.T) {
		e := newEnv(t)
		s := e.open()
		e.start(s, "task-a")
		submit(t, s, sessA, e.base)
		s.Close()
		e.offers = repriced(e)
		s = e.open()
		before := fingerprint(t, e.path)
		_, err := s.Submit(sessA, raw(t, variant(t, e.base, 11)))
		wantCode(t, err, "QUOTE_NOT_OFFERED")
		if fingerprint(t, e.path) != before {
			t.Fatal("refusal wrote")
		}
	})
	t.Run("A replays B's purchase", func(t *testing.T) {
		e := newEnv(t)
		s := e.open()
		e.start(s, "task-a")
		e.start(s, "task-b")
		pb := forTask(t, e.base, "task-b")
		submit(t, s, sessB, pb)
		e.clock.Add(40) // B's Ask is due; only B's own operations or the host may expire it
		before := fingerprint(t, e.path)
		_, err := s.Submit(sessA, raw(t, pb))
		wantCode(t, err, "OWNERSHIP_MISMATCH")
		if strings.Contains(err.Error(), "HUMAN_APPROVAL_REQUIRED") || strings.Contains(err.Error(), "Ask") || fingerprint(t, e.path) != before {
			t.Fatalf("A learned or changed B's purchase: %v", err)
		}
	})
}

// R2: a declared missing profile is the accepted CONFIG_MISSING case, kept
// across restarts; broken references are corruption, not missing config.
func TestMissingProfileLifecycle(t *testing.T) {
	absent := func(e *env) TaskSetup {
		x := e.setup("task-a")
		x.Profile, x.ProfileAbsent, x.BudgetInitial, x.AssetID = nil, true, "5000000", "fixture-usdc"
		return x
	}
	e := newEnv(t)
	s := e.open()
	if err := s.StartTask(absent(e)); err != nil {
		t.Fatal(err)
	}
	r := submit(t, s, sessA, e.base)
	if d := r.Purchase.Decision; d.PolicyVerdict != "Deny" || d.Reason != "CONFIG_MISSING" {
		t.Fatalf("decision %+v", d)
	}
	s.Close()
	s = e.open()
	before := fingerprint(t, e.path)
	if v := task(t, s, sessA); v.Status != "denied" || v.DenyReason != "CONFIG_MISSING" {
		t.Fatalf("after restart: %+v", v)
	}
	if again := submit(t, s, sessA, e.base); !again.Replay || again.Purchase.Decision != r.Purchase.Decision || fingerprint(t, e.path) != before {
		t.Fatalf("replay: %+v", again)
	}

	t.Run("missing credential", func(t *testing.T) {
		e := newEnv(t)
		s := e.open()
		x := e.setup("task-a")
		x.Credential, x.CredentialAbsent = nil, true
		if err := s.StartTask(x); err != nil {
			t.Fatal(err)
		}
		if d := submit(t, s, sessA, e.base).Purchase.Decision; d.Reason != "CONFIG_MISSING" {
			t.Fatalf("decision %+v", d)
		}
		s.Close()
		if v := task(t, e.open(), sessA); v.DenyReason != "CONFIG_MISSING" {
			t.Fatalf("after restart: %+v", v)
		}
	})

	broken := []struct{ name, sql string }{
		{"absent profile but a profile reference", `UPDATE tasks SET profile_snapshot = (SELECT snapshot_hash FROM config_snapshots WHERE kind = 'credential') WHERE task_id = 'task-a'`},
		{"terms of an absent-profile task altered", `UPDATE config_snapshots SET body = replace(body, '"budget_initial":"5000000"', '"budget_initial":"4000000"') WHERE kind = 'terms'`},
		{"credential snapshot altered", `UPDATE config_snapshots SET body = replace(body, '"evidence_mode":"fixture"', '"evidence_mode":"live"') WHERE kind = 'credential'`},
	}
	for _, b := range broken {
		t.Run(b.name, func(t *testing.T) {
			e := newEnv(t)
			s := e.open()
			if err := s.StartTask(absent(e)); err != nil {
				t.Fatal(err)
			}
			s.Close()
			if _, err := rawDB(t, e.path).Exec(b.sql); err != nil {
				t.Fatal(err)
			}
			var ce *CorruptError
			if err := openErr(e); !errors.As(err, &ce) || ce.Check != "snapshot" {
				t.Fatalf("Open: %v, want a snapshot corruption, not a legal missing config", err)
			}
		})
	}
}

// D4: only offered quotes are stored, unaltered; anything else is refused
// before a write without echoing it; queries show a whitelist.
func TestQuoteSafety(t *testing.T) {
	e := newEnv(t)
	s := e.open()
	e.start(s, "task-a")
	before := fingerprint(t, e.path)
	secrets := []string{"Bearer sk-live-abc123", "s3cr3t-seed", "tok-123", "<script>alert(1)</script>", "authorization", "privateKey", "walletSecret", "sessionToken"}

	leaky := editQuote(t, e.base, "authorization", "Bearer sk-live-abc123")
	leaky = editQuote(t, leaky, "privateKey", "0x"+strings.Repeat("ab", 32))
	leaky = editQuote(t, leaky, "walletSecret", map[string]any{"seed": "s3cr3t-seed"})
	leaky = editQuote(t, leaky, "note", "<script>alert(1)</script>\nX-Injected: 1")
	withExt := e.base
	withExt.Quote.Extensions = json.RawMessage(`{"sessionToken":{"info":{"token":"tok-123"},"schema":{}}}`)
	var err error
	if withExt.Intent.PaymentRequirementHash, err = withExt.Quote.Hash(); err != nil {
		t.Fatal(err)
	}
	for name, p := range map[string]contracts.PurchaseOrder{"secrets in extra": variant(t, leaky, 20), "secret extension": variant(t, withExt, 21)} {
		_, err := s.Submit(sessA, raw(t, p))
		wantCode(t, err, "QUOTE_NOT_OFFERED")
		for _, x := range secrets {
			if strings.Contains(err.Error(), x) {
				t.Fatalf("%s: error echoes %q", name, x)
			}
		}
	}
	// Contract refusals keep their code and drop details that would quote the request.
	var req map[string]any
	json.Unmarshal(e.base.Quote.SelectedRequirement, &req)
	req["payto"] = "0x1111111111111111111111111111111111111111"
	ambiguous := variant(t, e.base, 22)
	ambiguous.Quote.SelectedRequirement, _ = json.Marshal(req)
	_, err = s.Submit(sessA, raw(t, ambiguous))
	wantCode(t, err, "AMBIGUOUS_KEY")
	if strings.Contains(err.Error(), "payto") {
		t.Fatalf("error echoes the key: %v", err)
	}
	unknownField := strings.Replace(string(raw(t, variant(t, e.base, 23))), `{"version"`, `{"privateKey":"0xab","version"`, 1)
	_, err = s.Submit(sessA, []byte(unknownField))
	wantCode(t, err, "INVALID_PURCHASE")
	if strings.Contains(err.Error(), "privateKey") {
		t.Fatalf("error echoes the field: %v", err)
	}
	if fingerprint(t, e.path) != before {
		t.Fatal("a refused quote was written")
	}

	// The offered golden quote (with extra.future_term "資料 & terms" and an
	// unknown extension) is stored unaltered and still hashes after a restart.
	submit(t, s, sessA, e.base)
	s.Close()
	s = e.open()
	var stored contracts.PurchaseOrder
	if err := contracts.Decode([]byte(mustScalar(t, e.path, `SELECT body FROM purchases`)), &stored); err != nil {
		t.Fatal(err)
	}
	want, _ := canonical(e.base.Quote)
	got, _ := canonical(stored.Quote)
	if h, err := stored.Quote.Hash(); string(got) != string(want) || err != nil || h != stored.Intent.PaymentRequirementHash {
		t.Fatalf("stored quote changed or no longer hashes: %v", err)
	}
	view, _ := json.Marshal(task(t, s, sessA))
	events, _ := json.Marshal(mustEvents(t, s))
	for _, x := range []string{"future_term", "fixture-pricing", "資料", "extra", "selected_requirement", "extensions"} {
		if strings.Contains(string(view), x) || strings.Contains(string(events), x) {
			t.Fatalf("query output contains quote content %q", x)
		}
	}
}

func mustEvents(t *testing.T, s *Store) []contracts.AuditEvent {
	t.Helper()
	events, err := s.Events(sessA)
	if err != nil {
		t.Fatal(err)
	}
	return events
}

// The Store has no logging at all, so nothing it refuses can reach a log.
func TestStoreDoesNotLog(t *testing.T) {
	files, _ := filepath.Glob("*.go")
	for _, f := range files {
		if strings.HasSuffix(f, "_test.go") {
			continue
		}
		src, err := os.ReadFile(f)
		if err != nil {
			t.Fatal(err)
		}
		for _, banned := range []string{`"log"`, `"log/slog"`, "fmt.Print", "os.Stderr", "os.Stdout"} {
			if strings.Contains(string(src), banned) {
				t.Fatalf("%s uses %s", f, banned)
			}
		}
	}
}
