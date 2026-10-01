package policy

import (
	"encoding/json"
	"maps"
	"os"
	"reflect"
	"strconv"
	"testing"

	"github.com/SIMPLYBOYS/agent-payment-authority/internal/contracts"
)

func read(t *testing.T, path string, dst any) {
	t.Helper()
	raw, err := os.ReadFile("../../" + path)
	if err != nil {
		t.Fatal(err)
	}
	if err := contracts.Decode(raw, dst); err != nil {
		t.Fatalf("%s: %v", path, err)
	}
}

func registry(t *testing.T) (*contracts.Registry, contracts.RegistryConfig) {
	t.Helper()
	var c contracts.RegistryConfig
	read(t, "testdata/contracts/registry.json", &c)
	r, err := contracts.NewRegistry(c)
	if err != nil {
		t.Fatal(err)
	}
	return r, c
}

func profile(t *testing.T) Profile {
	t.Helper()
	raw, err := os.ReadFile("../../testdata/policy/m1-demo-profile.json")
	if err != nil {
		t.Fatal(err)
	}
	p, err := LoadProfile(raw)
	if err != nil {
		t.Fatal(err)
	}
	return p
}

func purchase(t *testing.T) contracts.PurchaseOrder {
	t.Helper()
	var p contracts.PurchaseOrder
	read(t, "testdata/contracts/purchase-input.json", &p)
	return p
}

// edit rewrites one field in both the intent and the quoted requirement, then
// recomputes the requirement hash, as a trusted host would for a new quote.
func edit(t *testing.T, p contracts.PurchaseOrder, field, value string) contracts.PurchaseOrder {
	t.Helper()
	var req map[string]any
	if err := json.Unmarshal(p.Quote.SelectedRequirement, &req); err != nil {
		t.Fatal(err)
	}
	switch field {
	case "amount":
		p.Intent.Amount, req["amount"] = contracts.Amount(value), value
	case "asset":
		p.Intent.AssetContract, req["asset"] = value, value
	case "payTo":
		p.Intent.PayTo, req["payTo"] = value, value
	default:
		t.Fatalf("edit: unknown field %s", field)
	}
	raw, err := json.Marshal(req)
	if err != nil {
		t.Fatal(err)
	}
	p.Quote.SelectedRequirement = raw
	if p.Intent.PaymentRequirementHash, err = p.Quote.Hash(); err != nil {
		t.Fatal(err)
	}
	return p
}

const clock = 1790726450 // cases.json defaults.clock

// baseline is the accepted Ask: task-a buys the 0.50 quote under the demo profile.
func baseline(t *testing.T) Input {
	t.Helper()
	p := profile(t)
	return Input{
		Session:    contracts.Session{ConsumerID: "consumer-a", AgentID: "agent-a", TaskID: "task-a"},
		Purchase:   purchase(t),
		TaskStatus: "running",
		Profile:    &p,
		Credential: &Credential{ConsumerID: "consumer-a", ValidUntil: clock + 86400, EvidenceMode: "fixture"},
		Budget:     Budget{TaskID: "task-a", Remaining: p.TaskCap},
		Now:        clock,
	}
}

func decide(t *testing.T, reg *contracts.Registry, in Input) contracts.PolicyDecision {
	t.Helper()
	d, err := Decide(reg, in)
	if err != nil {
		t.Fatalf("Decide: %v", err)
	}
	if err := d.Validate(); err != nil {
		t.Fatalf("decision %+v is not a valid M1 decision: %v", d, err)
	}
	want, _ := in.Purchase.Intent.Hash()
	if d.IntentHash != want || d.PaymentStatus != "unavailable" {
		t.Fatalf("decision %+v: intent hash or payment status wrong", d)
	}
	return d
}

func outcome(d contracts.PolicyDecision) string {
	return d.PolicyVerdict + "/" + d.Reason + "/" + d.ApprovalStatus
}

// The accepted policy cases decide the expected outcomes. Their answers were
// written before this package existed; nothing here derives them from Decide.
func TestAcceptedPolicyCases(t *testing.T) {
	var m contracts.CaseManifest
	read(t, "testdata/contracts/cases.json", &m)
	if err := m.RequireAccepted(); err != nil {
		t.Fatal(err)
	}
	reg, cfg := registry(t)
	ran := 0
	for _, c := range m.Cases {
		if c.Operation != "policy" {
			continue
		}
		ran++
		t.Run(c.CaseID, func(t *testing.T) {
			if c.InputRef != "purchase-input.json" {
				t.Fatalf("unexpected input_ref %s", c.InputRef)
			}
			params := maps.Clone(m.Defaults)
			maps.Copy(params, c.Parameters)
			in := baseline(t)
			p := *in.Profile
			in.Profile = &p
			for k, v := range params {
				switch k {
				case "remaining":
					in.Budget.Remaining = contracts.Amount(v)
				case "allow_max":
					p.AllowMax = contracts.Amount(v)
				case "merchant":
					p.Merchants = map[string][]string{"allowed": {cfg.MerchantPayTo}, "not_allowed": {}}[v]
				case "credential":
					in.Credential.ValidUntil = map[string]uint64{"valid": clock + 86400, "expired": clock}[v]
				case "required_config":
					if v == "missing" {
						in.Profile = nil
					}
				case "mode":
					p.Mode = v
				case "clock":
					n, err := strconv.ParseUint(v, 10, 64)
					if err != nil || n != clock {
						t.Fatalf("clock %s: the purchase fixture's validity window is built around %d", v, clock)
					}
					in.Now = n
				case "asset":
					if v == "unlisted" {
						in.Purchase = edit(t, in.Purchase, "asset", "0x5555555555555555555555555555555555555555")
					} else if v != p.AssetID {
						t.Fatalf("asset %s is not the profile asset", v)
					}
				case "amount":
					in.Purchase = edit(t, in.Purchase, "amount", v)
				default:
					t.Fatalf("parameter %s has no mapping; add one before trusting this test", k)
				}
			}
			d := decide(t, reg, in)
			e := c.Expected
			if got, want := outcome(d), e.Result+"/"+e.Reason+"/"+e.ApprovalStatus; got != want {
				t.Fatalf("got %s, want %s", got, want)
			}
			if e.PaymentStatus != d.PaymentStatus || e.BudgetDeducted != "0" {
				t.Fatalf("payment %s / deducted %s", d.PaymentStatus, e.BudgetDeducted)
			}
		})
	}
	if ran != 8 {
		t.Fatalf("ran %d policy cases, the accepted manifest has 8", ran)
	}
}

// The demo profile is the manifest's default policy configuration.
func TestProfileFixtureMatchesCaseDefaults(t *testing.T) {
	var m contracts.CaseManifest
	read(t, "testdata/contracts/cases.json", &m)
	_, cfg := registry(t)
	p := profile(t)
	d := m.Defaults
	if string(p.AllowMax) != d["allow_max"] || string(p.TaskCap) != d["remaining"] || p.AssetID != d["asset"] || p.Mode != d["mode"] {
		t.Fatalf("profile %+v does not match defaults %v", p, d)
	}
	if !reflect.DeepEqual(p.Merchants, []string{cfg.MerchantPayTo}) {
		t.Fatalf("merchants %v, registry merchant %s", p.Merchants, cfg.MerchantPayTo)
	}
}

// Every Deny check is switched on, then cleared one at a time: each step must
// expose the next reason in the documented order.
func TestDenyPrecedence(t *testing.T) {
	reg, _ := registry(t)
	in := baseline(t)
	in.Purchase = edit(t, in.Purchase, "asset", "0x5555555555555555555555555555555555555555")
	in.Purchase = edit(t, in.Purchase, "payTo", "0x6666666666666666666666666666666666666666")
	in.Credential.ValidUntil = clock
	in.Budget.Remaining = "1"
	p := *in.Profile
	p.Mode = "unattended"
	in.Profile = nil

	steps := []struct {
		want string
		fix  func()
	}{
		{"Deny/CONFIG_MISSING/not_requested", func() { in.Profile = &p }},
		{"Deny/ASSET_NOT_ALLOWED/not_requested", func() { in.Purchase = edit(t, in.Purchase, "asset", "0x4444444444444444444444444444444444444444") }},
		{"Deny/MERCHANT_NOT_ALLOWED/not_requested", func() { p.Merchants = []string{"0x6666666666666666666666666666666666666666"} }},
		{"Deny/AUTHORIZATION_EXPIRED/not_requested", func() { in.Credential.ValidUntil = clock + 1 }},
		{"Deny/INSUFFICIENT_BUDGET/not_requested", func() { in.Budget.Remaining = p.TaskCap }},
		{"Deny/APPROVER_UNAVAILABLE/not_requested", func() { p.Mode = "interactive" }},
		{"Ask/HUMAN_APPROVAL_REQUIRED/pending", nil},
	}
	for _, s := range steps {
		if got := outcome(decide(t, reg, in)); got != s.want {
			t.Fatalf("got %s, want %s", got, s.want)
		}
		if s.fix != nil {
			s.fix()
		}
	}
	// A missing credential is missing configuration too.
	in.Credential = nil
	if got := outcome(decide(t, reg, in)); got != "Deny/CONFIG_MISSING/not_requested" {
		t.Fatalf("no credential: %s", got)
	}
}

func TestBoundaries(t *testing.T) {
	reg, _ := registry(t)
	cases := []struct {
		name string
		edit func(*testing.T, *Input)
		want string
	}{
		{"amount equals allow_max", func(t *testing.T, in *Input) { in.Purchase = edit(t, in.Purchase, "amount", "100000") }, "Allow/PAYMENT_CAPABILITY_UNAVAILABLE/not_requested"},
		{"amount one over allow_max", func(t *testing.T, in *Input) { in.Purchase = edit(t, in.Purchase, "amount", "100001") }, "Ask/HUMAN_APPROVAL_REQUIRED/pending"},
		{"allow_max zero", func(t *testing.T, in *Input) {
			in.Profile.AllowMax = "0"
			in.Purchase = edit(t, in.Purchase, "amount", "1")
		}, "Ask/HUMAN_APPROVAL_REQUIRED/pending"},
		{"remaining equals amount", func(t *testing.T, in *Input) { in.Budget.Remaining = "500000" }, "Ask/HUMAN_APPROVAL_REQUIRED/pending"},
		{"remaining one short", func(t *testing.T, in *Input) { in.Budget.Remaining = "499999" }, "Deny/INSUFFICIENT_BUDGET/not_requested"},
		{"budget also bounds Allow", func(t *testing.T, in *Input) {
			in.Purchase = edit(t, in.Purchase, "amount", "50000")
			in.Budget.Remaining = "49999"
		}, "Deny/INSUFFICIENT_BUDGET/not_requested"},
		{"unattended within allow_max", func(t *testing.T, in *Input) {
			in.Profile.Mode = "unattended"
			in.Purchase = edit(t, in.Purchase, "amount", "50000")
		}, "Allow/PAYMENT_CAPABILITY_UNAVAILABLE/not_requested"},
		{"credential expires at now", func(t *testing.T, in *Input) { in.Credential.ValidUntil = clock }, "Deny/AUTHORIZATION_EXPIRED/not_requested"},
		{"credential expires a second later", func(t *testing.T, in *Input) { in.Credential.ValidUntil = clock + 1 }, "Ask/HUMAN_APPROVAL_REQUIRED/pending"},
		{"now at valid_after", func(t *testing.T, in *Input) { in.Now = in.Purchase.Intent.ValidAfter }, "Deny/AUTHORIZATION_EXPIRED/not_requested"},
		{"now just after valid_after", func(t *testing.T, in *Input) { in.Now = in.Purchase.Intent.ValidAfter + 1 }, "Ask/HUMAN_APPROVAL_REQUIRED/pending"},
		{"now just before valid_before", func(t *testing.T, in *Input) { in.Now = in.Purchase.Intent.ValidBefore - 1 }, "Ask/HUMAN_APPROVAL_REQUIRED/pending"},
		{"now at valid_before", func(t *testing.T, in *Input) { in.Now = in.Purchase.Intent.ValidBefore }, "Deny/AUTHORIZATION_EXPIRED/not_requested"},
		{"empty merchant list", func(t *testing.T, in *Input) { in.Profile.Merchants = []string{} }, "Deny/MERCHANT_NOT_ALLOWED/not_requested"},
		{"trusted asset outside the profile", func(t *testing.T, in *Input) { in.Profile.AssetID = "other-usdc" }, "Deny/ASSET_NOT_ALLOWED/not_requested"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			in := baseline(t)
			c.edit(t, &in)
			if got := outcome(decide(t, reg, in)); got != c.want {
				t.Fatalf("got %s, want %s", got, c.want)
			}
		})
	}
}

// With nothing to decide, Decide returns an error and no decision at all.
func TestRefusalsWithoutDecision(t *testing.T) {
	reg, _ := registry(t)
	cases := []struct {
		name string
		edit func(*Input)
		code string
	}{
		{"session B buys with A's purchase", func(in *Input) {
			in.Session = contracts.Session{ConsumerID: "consumer-b", AgentID: "agent-b", TaskID: "task-b"}
			in.Budget.TaskID = "task-b"
		}, "OWNERSHIP_MISMATCH"},
		{"agent of another consumer", func(in *Input) { in.Session.AgentID = "agent-b" }, "OWNERSHIP_MISMATCH"},
		{"task denied", func(in *Input) { in.TaskStatus = "denied" }, "TASK_NOT_ACTIVE"},
		{"task cancelled", func(in *Input) { in.TaskStatus = "cancelled" }, "TASK_NOT_ACTIVE"},
		{"task failed", func(in *Input) { in.TaskStatus = "failed" }, "TASK_NOT_ACTIVE"},
		{"unknown task status", func(in *Input) { in.TaskStatus = "paid" }, ""},
		{"tampered purchase", func(in *Input) { in.Purchase.Intent.Amount = "1" }, ""},
		{"budget of another task", func(in *Input) { in.Budget.TaskID = "task-a-next" }, ""},
		{"B's credential for A", func(in *Input) { in.Credential.ConsumerID = "consumer-b" }, ""},
		{"live credential", func(in *Input) { in.Credential.EvidenceMode = "live" }, ""},
		{"invalid profile", func(in *Input) { in.Profile.Mode = "auto" }, ""},
		{"remaining above the cap", func(in *Input) { in.Budget.Remaining = "5000001" }, ""},
		{"remaining not an amount", func(in *Input) { in.Budget.Remaining = "0.5" }, ""},
		{"no clock", func(in *Input) { in.Now = 0 }, ""},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			in := baseline(t)
			c.edit(&in)
			d, err := Decide(reg, in)
			if err == nil || d != (contracts.PolicyDecision{}) {
				t.Fatalf("got %+v, %v; want an error and no decision", d, err)
			}
			if c.code != "" && contracts.ErrorCode(err) != c.code {
				t.Fatalf("error %v, want code %s", err, c.code)
			}
		})
	}
}

func TestDecideIsDeterministicAndPure(t *testing.T) {
	reg, _ := registry(t)
	in := baseline(t)
	before := *in.Profile
	merchants := append([]string(nil), in.Profile.Merchants...)
	first := decide(t, reg, in)
	for range 3 {
		if again := decide(t, reg, in); again != first {
			t.Fatalf("decision changed: %+v then %+v", first, again)
		}
	}
	if !reflect.DeepEqual(*in.Profile, before) || !reflect.DeepEqual(in.Profile.Merchants, merchants) || in.Budget.Remaining != before.TaskCap {
		t.Fatal("Decide changed its inputs")
	}
}

func TestLoadProfile(t *testing.T) {
	good := `{"profile_id":"p","evidence_mode":"fixture","asset_id":"fixture-usdc","allow_max":"1","task_cap":"2","merchants":[],"mode":"interactive"}`
	if _, err := LoadProfile([]byte(good)); err != nil {
		t.Fatalf("control: %v", err)
	}
	bad := map[string]string{
		"unknown field":     `{"profile_id":"p","evidence_mode":"fixture","asset_id":"a","allow_max":"1","task_cap":"2","merchants":[],"mode":"interactive","approver":"me"}`,
		"missing merchants": `{"profile_id":"p","evidence_mode":"fixture","asset_id":"a","allow_max":"1","task_cap":"2","mode":"interactive"}`,
		"null merchants":    `{"profile_id":"p","evidence_mode":"fixture","asset_id":"a","allow_max":"1","task_cap":"2","merchants":null,"mode":"interactive"}`,
		"live profile":      `{"profile_id":"p","evidence_mode":"live","asset_id":"a","allow_max":"1","task_cap":"2","merchants":[],"mode":"interactive"}`,
		"allow over cap":    `{"profile_id":"p","evidence_mode":"fixture","asset_id":"a","allow_max":"3","task_cap":"2","merchants":[],"mode":"interactive"}`,
		"float amount":      `{"profile_id":"p","evidence_mode":"fixture","asset_id":"a","allow_max":0.1,"task_cap":"2","merchants":[],"mode":"interactive"}`,
		"uppercase address": `{"profile_id":"p","evidence_mode":"fixture","asset_id":"a","allow_max":"1","task_cap":"2","merchants":["0x333333333333333333333333333333333333333A"],"mode":"interactive"}`,
		"unknown mode":      `{"profile_id":"p","evidence_mode":"fixture","asset_id":"a","allow_max":"1","task_cap":"2","merchants":[],"mode":"auto"}`,
		"empty profile id":  `{"profile_id":"","evidence_mode":"fixture","asset_id":"a","allow_max":"1","task_cap":"2","merchants":[],"mode":"interactive"}`,
	}
	for name, raw := range bad {
		if _, err := LoadProfile([]byte(raw)); err == nil {
			t.Errorf("%s: accepted", name)
		}
	}
}
