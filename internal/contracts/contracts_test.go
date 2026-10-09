package contracts

import (
	"encoding/json"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

func fixture(t *testing.T, name string, dst any) {
	t.Helper()
	raw, err := os.ReadFile("../../testdata/contracts/" + name)
	if err != nil {
		t.Fatal(err)
	}
	if err := Decode(raw, dst); err != nil {
		t.Fatal(err)
	}
}
func purchase(t *testing.T) PurchaseOrder {
	t.Helper()
	var p PurchaseOrder
	fixture(t, "purchase-input.json", &p)
	return p
}
func registry(t *testing.T) (*Registry, RegistryConfig) {
	t.Helper()
	var c RegistryConfig
	fixture(t, "registry.json", &c)
	r, err := NewRegistry(c)
	if err != nil {
		t.Fatal(err)
	}
	return r, c
}
func mustFail(t *testing.T, err error) {
	t.Helper()
	if err == nil {
		t.Fatal("expected rejection")
	}
}
func mustPass(t *testing.T, err error) {
	t.Helper()
	if err != nil {
		t.Fatal(err)
	}
}
// rehash is used by the review probes in artifacts/b1-02-review/_probe.
func rehash(t *testing.T, p *PurchaseOrder) {
	t.Helper()
	h, err := p.Quote.Hash()
	mustPass(t, err)
	p.Intent.PaymentRequirementHash = h
	h, err = ResourceHash(p.Intent.HTTPMethod, p.Intent.ResourceURL)
	mustPass(t, err)
	p.Intent.ResourceHash = h
}
func asB(p PurchaseOrder) PurchaseOrder {
	p.Intent.ConsumerID = "consumer-b"
	p.Intent.AgentID = "agent-b"
	p.Intent.WalletID = "wallet-b"
	p.Intent.TaskID = "task-b"
	p.Intent.PurchaseOrderID = "po-b-1"
	p.Intent.From = "0x2222222222222222222222222222222222222222"
	return p
}

func TestGoldenVectors(t *testing.T) {
	p := purchase(t)
	mustPass(t, p.Validate())
	var g struct {
		ResourceHash    string `json:"resource_hash"`
		RequirementHash string `json:"payment_requirement_hash"`
		IntentHash      string `json:"intent_hash"`
		Canonical       string `json:"canonical_intent"`
		Cases           []struct {
			Input     json.RawMessage `json:"input"`
			Canonical string          `json:"canonical"`
		} `json:"canonical_cases"`
		Genesis     string   `json:"event_genesis_task_a"`
		EventHashes []string `json:"event_hashes"`
		// Digest of the case manifest Aaron accepted (cases of b57b585,
		// review_status accepted). Any edit to cases.json must fail here and
		// needs a new acceptance.
		AcceptedCases string `json:"accepted_case_manifest_digest"`
	}
	fixture(t, "golden.json", &g)
	var m CaseManifest
	fixture(t, "cases.json", &m)
	mustPass(t, m.RequireAccepted())
	if h, err := m.Hash(); err != nil || h != g.AcceptedCases || m.Reviewer != "aaron" {
		t.Fatalf("cases.json differs from the accepted manifest %s (got %s, %v)", g.AcceptedCases, h, err)
	}
	genesis, err := GenesisHash("task-a")
	mustPass(t, err)
	events := chainFixture(t)
	if genesis != g.Genesis || len(events) != len(g.EventHashes) {
		t.Fatal("event genesis or chain length changed")
	}
	for i, e := range events {
		if h, err := EventHash(e); err != nil || h != g.EventHashes[i] {
			t.Fatalf("event %s hash changed", e.EventID)
		}
	}
	rh, err := ResourceHash(p.Intent.HTTPMethod, p.Intent.ResourceURL)
	mustPass(t, err)
	qh, err := p.Quote.Hash()
	mustPass(t, err)
	ih, err := p.Intent.Hash()
	mustPass(t, err)
	raw, err := json.Marshal(p.Intent)
	mustPass(t, err)
	canonical, err := Canonical(raw)
	mustPass(t, err)
	if rh != g.ResourceHash || qh != g.RequirementHash || ih != g.IntentHash || string(canonical) != g.Canonical {
		t.Fatal("fixed independent vectors changed")
	}
	for _, c := range g.Cases {
		got, err := Canonical(c.Input)
		mustPass(t, err)
		if string(got) != c.Canonical {
			t.Fatal("canonical vector mismatch")
		}
	}
}

func TestCanonicalStrictness(t *testing.T) {
	a, err := Canonical([]byte(`{"z":1,"a":{"b":2,"a":1}}`))
	mustPass(t, err)
	b, err := Canonical([]byte(` { "a": {"a":1,"b":2},"z":1 } `))
	mustPass(t, err)
	if string(a) != string(b) {
		t.Fatal("key order changed canonical bytes")
	}
	for _, raw := range []string{`{"x":1,"x":2}`, `{"x":1,"\u0078":2}`, `{"a":{"x":1,"x":2}}`, `1.0`, `1e2`, `-0`, `9007199254740992`, `{"x":"\ud800"}`, `{"x":"�"}`, `{} {}`, `{"x":NaN}`, strings.Repeat("[", 34) + strings.Repeat("]", 34)} {
		t.Run(raw, func(t *testing.T) { _, err := Canonical([]byte(raw)); mustFail(t, err) })
	}
	_, err = Canonical([]byte{0xff})
	mustFail(t, err)
	_, err = Canonical([]byte(`"` + strings.Repeat("a", MaxJSONBytes) + `"`))
	mustFail(t, err)
	var c Consumer
	mustFail(t, Decode([]byte(`{"consumer_id":"a","agent_id":"b","wallet_id":"c","authority":true}`), &c))
}

func TestAmountBoundaries(t *testing.T) {
	for _, raw := range []string{`0`, `0.5`, `"0.50"`, `"-1"`, `"01"`, `"1e6"`, `"18446744073709551616"`, `null`} {
		var a Amount
		mustFail(t, json.Unmarshal([]byte(raw), &a))
	}
	for _, s := range []string{"0", "500000", "18446744073709551615"} {
		var a Amount
		mustPass(t, json.Unmarshal([]byte(`"`+s+`"`), &a))
		if string(a) != s {
			t.Fatal("amount changed")
		}
	}
	p := purchase(t)
	p.Intent.Amount = "0"
	mustFail(t, p.Intent.Validate())
}

func TestIntentSensitivity(t *testing.T) {
	p := purchase(t)
	original, err := p.Intent.Hash()
	mustPass(t, err)
	mutations := map[string]func(*AuthorizationIntent){
		"consumer": func(i *AuthorizationIntent) { i.ConsumerID = "consumer-b" }, "agent": func(i *AuthorizationIntent) { i.AgentID = "agent-b" }, "wallet": func(i *AuthorizationIntent) { i.WalletID = "wallet-b" },
		"task": func(i *AuthorizationIntent) { i.TaskID = "task-a-next" }, "purchase": func(i *AuthorizationIntent) { i.PurchaseOrderID = "po-other" },
		"amount": func(i *AuthorizationIntent) { i.Amount = "500001" }, "payer": func(i *AuthorizationIntent) { i.From = "0x2222222222222222222222222222222222222222" },
		"payee": func(i *AuthorizationIntent) { i.PayTo = "0x5555555555555555555555555555555555555555" }, "asset": func(i *AuthorizationIntent) { i.AssetContract = "0x6666666666666666666666666666666666666666" },
		"chain": func(i *AuthorizationIntent) { i.ChainID = "1" }, "expiry": func(i *AuthorizationIntent) { i.ValidBefore++ }, "start": func(i *AuthorizationIntent) { i.ValidAfter++ },
		"nonce": func(i *AuthorizationIntent) { i.EIP3009Nonce = "0x" + strings.Repeat("b", 64) },
		"url": func(i *AuthorizationIntent) {
			i.ResourceURL += "&lang=en"
			i.ResourceHash, _ = ResourceHash(i.HTTPMethod, i.ResourceURL)
		},
		"method": func(i *AuthorizationIntent) {
			i.HTTPMethod = "POST"
			i.ResourceHash, _ = ResourceHash(i.HTTPMethod, i.ResourceURL)
		},
		"requirement": func(i *AuthorizationIntent) { i.PaymentRequirementHash = "sha256:" + strings.Repeat("b", 64) },
	}
	for name, mutate := range mutations {
		t.Run(name, func(t *testing.T) {
			i := p.Intent
			mutate(&i)
			h, err := i.Hash()
			mustPass(t, err)
			if h == original {
				t.Fatal("changed intent kept digest")
			}
		})
	}
	for _, mutate := range []func(*AuthorizationIntent){func(i *AuthorizationIntent) { i.Domain = "live" }, func(i *AuthorizationIntent) { i.Version = "v2" }, func(i *AuthorizationIntent) { i.X402Version = 1 }, func(i *AuthorizationIntent) { i.Scheme = "upto" }, func(i *AuthorizationIntent) { i.ValidBefore = i.ValidAfter }, func(i *AuthorizationIntent) { i.ValidBefore = 1790727000000 }, func(i *AuthorizationIntent) { i.From = "0x" + strings.Repeat("A", 40) }} {
		i := p.Intent
		mutate(&i)
		_, err := i.Hash()
		mustFail(t, err)
	}
}

func TestCompleteQuoteBinding(t *testing.T) {
	p := purchase(t)
	old, err := p.Quote.Hash()
	mustPass(t, err)
	for _, field := range []string{"selected", "outer"} {
		q := p.Quote
		if field == "selected" {
			q.SelectedRequirement = json.RawMessage(strings.Replace(string(q.SelectedRequirement), `"revision":1`, `"revision":2`, 1))
		} else {
			q.Extensions = json.RawMessage(`{"new-unknown-term":{"fee":"10"}}`)
		}
		h, err := q.Hash()
		mustPass(t, err)
		if h == old {
			t.Fatal("unknown field lost from hash")
		}
		changed := p
		changed.Quote = q
		mustFail(t, changed.Validate())
		changed.Intent.PaymentRequirementHash = h
		mustPass(t, changed.Validate())
		newIntent, err := changed.Intent.Hash()
		mustPass(t, err)
		oldIntent, _ := p.Intent.Hash()
		if newIntent == oldIntent {
			t.Fatal("extension not bound")
		}
	}
	p.Intent.Amount = "500001"
	mustFail(t, p.Validate())
}

func TestOwnershipAndReconstruction(t *testing.T) {
	r, c := registry(t)
	p := purchase(t)
	a := Session{"consumer-a", "agent-a", "task-a"}
	b := Session{"consumer-b", "agent-b", "task-b"}
	mustPass(t, r.ValidatePurchase(a, p))
	mustPass(t, r.ValidatePurchase(b, asB(p)))
	mustFail(t, r.ValidatePurchase(a, asB(p)))
	for _, mutate := range []func(*AuthorizationIntent){func(i *AuthorizationIntent) { i.TaskID = "task-b" }, func(i *AuthorizationIntent) { i.WalletID = "wallet-b" }, func(i *AuthorizationIntent) { i.From = "0x2222222222222222222222222222222222222222" }, func(i *AuthorizationIntent) { i.ConsumerID = "consumer-b" }, func(i *AuthorizationIntent) { i.AgentID = "agent-b" }} {
		copy := p
		mutate(&copy.Intent)
		mustFail(t, r.ValidatePurchase(a, copy))
	}
	_, w1, err := r.Binding("task-a")
	mustPass(t, err)
	raw, err := json.Marshal(c)
	mustPass(t, err)
	var loaded RegistryConfig
	mustPass(t, Decode(raw, &loaded))
	again, err := NewRegistry(loaded)
	mustPass(t, err)
	_, w2, err := again.Binding("task-a-next")
	mustPass(t, err)
	if w1 != w2 {
		t.Fatal("same consumer lost wallet across reconstruction/new task")
	}
	c.Wallets[1].From = c.Wallets[0].From
	_, err = NewRegistry(c)
	mustFail(t, err)
}

func TestRetryIsolation(t *testing.T) {
	p := purchase(t)
	out, err := CompareRetry(p, p)
	mustPass(t, err)
	if out != "replay" {
		t.Fatal(out)
	}
	b := asB(p)
	out, err = CompareRetry(p, b)
	mustPass(t, err)
	if out != "new" {
		t.Fatal("A/B request ID collision")
	}
	changed := p
	changed.Intent.EIP3009Nonce = "0x" + strings.Repeat("b", 64)
	_, err = CompareRetry(p, changed)
	mustFail(t, err)
}

func event(t *testing.T) (AuditEvent, PurchaseOrder, *Registry) {
	t.Helper()
	p := purchase(t)
	r, _ := registry(t)
	h, err := p.Intent.Hash()
	mustPass(t, err)
	genesis, err := GenesisHash("task-a")
	mustPass(t, err)
	return AuditEvent{Version: Version, EventID: "event-1", EventType: "POLICY_DECIDED", Scope: &EventScope{"consumer-a", "agent-a", "wallet-a", "task-a", "po-a-1", h}, Sequence: 1, PrevEventHash: genesis, Payload: EventPayload{Decision: &PolicyDecision{Version, h, "Ask", "pending", "unavailable", "HUMAN_APPROVAL_REQUIRED"}}, EvidenceMode: "fixture", Source: "policy", ObservedAt: 1790726450}, p, r
}
func TestEventAndStateBoundaries(t *testing.T) {
	e, p, r := event(t)
	mustPass(t, r.ValidateEvent(e, &p))
	for _, kind := range []string{"APPROVED", "GRANT_ISSUED", "SETTLED", "RESOURCE_DELIVERED"} {
		e, p, r := event(t)
		e.EventType = kind
		mustFail(t, r.ValidateEvent(e, &p))
	}
	for _, mutation := range []func(*AuditEvent){func(e *AuditEvent) { e.EvidenceMode = "" }, func(e *AuditEvent) { e.Source = "" }, func(e *AuditEvent) { e.Scope.ConsumerID = "consumer-b" }, func(e *AuditEvent) { e.Scope.IntentHash = "sha256:" + strings.Repeat("0", 64) }, func(e *AuditEvent) { e.Sequence = 0 }, func(e *AuditEvent) { e.Payload.Decision.ApprovalStatus = "approved" }, func(e *AuditEvent) { e.Payload.Decision.PaymentStatus = "Settled" }} {
		e, p, r := event(t)
		mutation(&e)
		mustFail(t, r.ValidateEvent(e, &p))
	}
	e, p, r = event(t)
	e.EventType = "CLI_EXITED"
	mustFail(t, r.ValidateEvent(e, &p))
	e.Payload = EventPayload{Code: "EXIT_0"}
	mustPass(t, r.ValidateEvent(e, &p))
	// Stream order and the hash chain are covered by TestEventChain and
	// TestEventTransitionRules.
	system := AuditEvent{Version: Version, EventID: "system-1", EventType: "SYSTEM_READY", EvidenceMode: "fixture", Source: "host", ObservedAt: 1790726450}
	mustPass(t, r.ValidateEvent(system, nil))
	system.Scope = e.Scope
	mustFail(t, r.ValidateEvent(system, nil))
}

func TestManifestReviewGate(t *testing.T) {
	var m CaseManifest
	fixture(t, "cases.json", &m)
	mustPass(t, m.Validate())
	// Aaron accepted the manifest on 2026-09-30 (pinned by TestGoldenVectors).
	// A pending copy must still fail the gate.
	mustPass(t, m.RequireAccepted())
	pending := m
	pending.ReviewStatus, pending.Reviewer = "pending", ""
	mustFail(t, pending.RequireAccepted())
	for _, c := range m.Cases {
		if _, err := os.Stat("../../testdata/contracts/" + c.InputRef); err != nil {
			t.Fatal(err)
		}
	}
	h, err := m.Hash()
	mustPass(t, err)
	r := RunManifest{Version: Version, RunID: "fixture-run", CodeCommit: "97ef4e9", DirtyPatchDigest: "sha256:" + strings.Repeat("0", 64), ConfigDigest: "sha256:" + strings.Repeat("1", 64), CaseManifestDigest: h, Versions: map[string]string{"go": "test"}, Commands: []CommandResult{{"test", 0}}, NotEvaluated: slices.Clone(CapabilityIDs)}
	for _, c := range m.Cases {
		r.Results = append(r.Results, ClaimResult{c.CaseID, "INCONCLUSIVE", []string{}, "not executed"})
	}
	mustPass(t, r.ValidateAgainst(m))
	r.Results[0].Verdict = "PASS"
	mustFail(t, r.ValidateAgainst(m))
	r.Results[0].Verdict = "INCONCLUSIVE"
	r.Evidence = []EvidenceRef{{"e-1", "../escape", "sha256:" + strings.Repeat("2", 64), "fixture", "test"}}
	mustFail(t, r.ValidateAgainst(m))
}

// The expected outcomes are read from the pre-policy manifest, never produced
// by a policy implementation. Integration rows intentionally aren't evaluated.
func TestContractCasesFromManifest(t *testing.T) {
	var m CaseManifest
	fixture(t, "cases.json", &m)
	for _, c := range m.Cases {
		if c.Layer != "contracts" {
			continue
		}
		t.Run(c.CaseID, func(t *testing.T) {
			p := purchase(t)
			r, cfg := registry(t)
			result := ""
			var err error
			// The record of what was called (M1-E1, internal/m1evidence.Contract).
			rec := map[string]any{"case_id": c.CaseID, "layer": "contracts_test", "operation": c.Operation, "parameters": c.Parameters}
			accept := func(e error) {
				err, result = e, "accept"
				if e != nil {
					result = "reject"
				}
			}
			switch c.Operation {
			case "compare-retry":
				incoming := p
				switch c.Parameters["mutation"] {
				case "nonce":
					incoming.Intent.EIP3009Nonce = "0x" + strings.Repeat("b", 64)
				case "request_id":
					incoming.RequestID = "request-2"
				case "other_consumer":
					incoming = asB(p) // same request_id, other consumer
				}
				rec["purchase"], rec["incoming"] = p, incoming
				result, err = CompareRetry(p, incoming)
				if err != nil {
					result = "reject"
				}
			case "binding":
				s := Session{"consumer-a", "agent-a", "task-a"}
				if c.Parameters["session"] == "b" {
					s = Session{"consumer-b", "agent-b", "task-b"}
				}
				if c.Parameters["purchase"] == "b" {
					p = asB(p)
				}
				b := asB(p)
				switch c.Parameters["mutation"] {
				case "wallet_b":
					p.Intent.WalletID = b.Intent.WalletID
				case "from_b":
					p.Intent.From = b.Intent.From
				case "consumer_b":
					p.Intent.ConsumerID = b.Intent.ConsumerID
				case "agent_b":
					p.Intent.AgentID = b.Intent.AgentID
				}
				rec["registry"], rec["session"], rec["purchase"] = cfg, s, p
				accept(r.ValidatePurchase(s, p))
			case "validate-decision":
				h, e := p.Intent.Hash()
				mustPass(t, e)
				d := PolicyDecision{Version, h, "Ask", "pending", c.Parameters["payment_status"], "HUMAN_APPROVAL_REQUIRED"}
				rec["decision"] = d
				accept(d.Validate())
			case "validate-purchase":
				mp := mutatePurchase(t, p, c.Parameters["mutation"])
				rec["purchase"] = mp
				accept(mp.Validate())
			default:
				t.Fatalf("unimplemented contract case operation %s", c.Operation)
			}
			if dir := os.Getenv("APA_M1_RUN_DIR"); dir != "" {
				observed := map[string]string{"result": result}
				if code := ErrorCode(err); code != "" {
					observed["code"] = code
				}
				rec["observed"] = observed
				body, merr := json.MarshalIndent(rec, "", "  ")
				if merr != nil {
					t.Fatal(merr)
				}
				mustPass(t, os.MkdirAll(dir, 0o700))
				mustPass(t, os.WriteFile(filepath.Join(dir, c.CaseID+".json"), append(body, '\n'), 0o600))
			}
			if result != c.Expected.Result {
				t.Fatalf("got %s, expected %s (%v)", result, c.Expected.Result, err)
			}
			if err != nil && ErrorCode(err) != c.Expected.Reason {
				t.Fatalf("reason code %q, expected %q (%v)", ErrorCode(err), c.Expected.Reason, err)
			}
		})
	}
}
