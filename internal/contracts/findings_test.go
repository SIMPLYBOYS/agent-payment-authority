package contracts

// Regression tests for the B1-02 review findings (C1–C10, SA1) and the v2
// decisions. Each test fails while its finding is still present; see the
// vault note "B1-02 契約 Review" for the finding text.

import (
	"encoding/json"
	"maps"
	"slices"
	"strings"
	"testing"
)

// chainFixture decodes testdata/contracts/events.json: a valid task-a chain
// TASK_STARTED → PURCHASE_PROPOSED → POLICY_DECIDED(Ask) → APPROVAL_EXPIRED.
func chainFixture(t *testing.T) []AuditEvent {
	t.Helper()
	var events []AuditEvent
	fixture(t, "events.json", &events)
	return events
}

// editQuote changes the selected requirement and re-hashes when the quote
// still hashes, so only the edited property can cause a rejection.
func editQuote(t *testing.T, p PurchaseOrder, edit func(req map[string]any)) PurchaseOrder {
	t.Helper()
	var req map[string]any
	mustPass(t, json.Unmarshal(p.Quote.SelectedRequirement, &req))
	edit(req)
	raw, err := json.Marshal(req)
	mustPass(t, err)
	p.Quote.SelectedRequirement = raw
	if h, err := p.Quote.Hash(); err == nil {
		p.Intent.PaymentRequirementHash = h
	}
	return p
}

// mutatePurchase applies one validate-purchase mutation from cases.json.
func mutatePurchase(t *testing.T, p PurchaseOrder, mutation string) PurchaseOrder {
	t.Helper()
	return editQuote(t, p, func(req map[string]any) {
		switch mutation {
		case "transfer_method_permit2":
			req["extra"].(map[string]any)["assetTransferMethod"] = "permit2"
		case "transfer_method_erc7710":
			req["extra"].(map[string]any)["assetTransferMethod"] = "erc7710"
		case "case_variant_pay_to":
			req["payto"] = "0x5555555555555555555555555555555555555555"
		default:
			t.Fatalf("unknown mutation %s", mutation)
		}
	})
}

func intentHash(t *testing.T, p PurchaseOrder) string {
	t.Helper()
	h, err := p.Intent.Hash()
	mustPass(t, err)
	return h
}
func decision(h, verdict, approval, reason string) *PolicyDecision {
	return &PolicyDecision{Version, h, verdict, approval, "unavailable", reason}
}

// taskEvent builds an unlinked task-a event; purchase events get po-a-1.
func taskEvent(h, id, typ, code string, d *PolicyDecision) AuditEvent {
	s := &EventScope{ConsumerID: "consumer-a", AgentID: "agent-a", WalletID: "wallet-a", TaskID: "task-a"}
	if !slices.Contains([]string{"TASK_STARTED", "TASK_CANCELLED", "TASK_TIMED_OUT", "CLI_EXITED"}, typ) {
		s.PurchaseOrderID, s.IntentHash = "po-a-1", h
	}
	return AuditEvent{Version: Version, EventID: id, EventType: typ, Scope: s, Payload: EventPayload{Code: code, Decision: d}, EvidenceMode: "fixture", Source: "test", ObservedAt: 1790726450}
}

// linked assigns sequence and prev_event_hash so the events form one valid
// chain per task, leaving only the transition rules to be judged.
func linked(t *testing.T, events ...AuditEvent) []AuditEvent {
	t.Helper()
	prev, seq := map[string]string{}, map[string]uint64{}
	for i := range events {
		e := &events[i]
		task := e.Scope.TaskID
		if prev[task] == "" {
			g, err := GenesisHash(task)
			mustPass(t, err)
			prev[task] = g
		}
		seq[task]++
		e.Sequence, e.PrevEventHash = seq[task], prev[task]
		h, err := EventHash(*e)
		mustPass(t, err)
		prev[task] = h
	}
	return events
}
func orderFor(e AuditEvent, p *PurchaseOrder) *PurchaseOrder {
	if e.Scope != nil && e.Scope.PurchaseOrderID != "" {
		return p
	}
	return nil
}
func wantCode(t *testing.T, err error, code string) {
	t.Helper()
	if ErrorCode(err) != code {
		t.Fatalf("got %v, want code %s", err, code)
	}
}

// Decision 1: v2 is its own fixture contract; v1 documents are refused.
func TestV2SupersedesV1(t *testing.T) {
	if Version != "apa.m1.v2" || !strings.Contains(FixtureDomain, "/m1-fixture/v2/") {
		t.Fatal("version or fixture domain not v2")
	}
	p := purchase(t)
	old := p
	old.Version = "apa.m1.v1"
	mustFail(t, old.Validate())
	old = p
	old.Intent.Version, old.Intent.Domain = "apa.m1.v1", strings.Replace(FixtureDomain, "/v2/", "/v1/", 1)
	mustFail(t, old.Intent.Validate())
	_, c := registry(t)
	c.Version = "apa.m1.v1"
	_, err := NewRegistry(c)
	mustFail(t, err)
}

// C1: an Ask keeps its verdict; expiry and withdrawal are later events with
// their own approval states, and neither is a fake approval.
func TestC1ApprovalExpiryAndWithdrawalKeepAsk(t *testing.T) {
	p := purchase(t)
	r, _ := registry(t)
	h := intentHash(t, p)
	for _, c := range []struct {
		verdict, approval, reason string
		ok                        bool
	}{
		{"Ask", "pending", "HUMAN_APPROVAL_REQUIRED", true},
		{"Ask", "expired", "APPROVAL_TIMEOUT", true},
		{"Ask", "withdrawn", "HOST_CANCELLED", true},
		{"Ask", "expired", "HOST_CANCELLED", false},
		{"Ask", "not_requested", "HUMAN_APPROVAL_REQUIRED", false},
		{"Ask", "approved", "HUMAN_APPROVAL_REQUIRED", false},
		{"Deny", "expired", "APPROVAL_TIMEOUT", false},
		{"Deny", "withdrawn", "HOST_CANCELLED", false},
		{"Deny", "not_requested", "APPROVAL_TIMEOUT", false},
		{"Allow", "expired", "PAYMENT_CAPABILITY_UNAVAILABLE", false},
	} {
		err := decision(h, c.verdict, c.approval, c.reason).Validate()
		if (err == nil) != c.ok || (err != nil && ErrorCode(err) != "INVALID_DECISION") {
			t.Fatalf("%+v: got %v", c, err)
		}
	}

	events := chainFixture(t)
	mustPass(t, ValidateEventOrder(events))
	for _, e := range events {
		mustPass(t, r.ValidateEvent(e, orderFor(e, &p)))
	}
	if d := events[2].Payload.Decision; d.PolicyVerdict != "Ask" || d.ApprovalStatus != "pending" {
		t.Fatal("original Ask was rewritten")
	}
	if d := events[3].Payload.Decision; events[3].EventType != "APPROVAL_EXPIRED" || d.PolicyVerdict != "Ask" || d.ApprovalStatus != "expired" {
		t.Fatal("expiry must be a separate event that keeps the Ask verdict")
	}

	// Each decision event accepts only its own approval states.
	wrong := events[3]
	wrong.EventType = "POLICY_DECIDED"
	wantCode(t, r.ValidateEvent(wrong, &p), "INVALID_DECISION")
	wrong = events[2]
	wrong.EventType = "APPROVAL_EXPIRED"
	wantCode(t, r.ValidateEvent(wrong, &p), "INVALID_DECISION")

	start := taskEvent(h, "e1", "TASK_STARTED", "", nil)
	prop := taskEvent(h, "e2", "PURCHASE_PROPOSED", "", nil)
	ask := taskEvent(h, "e3", "POLICY_DECIDED", "", decision(h, "Ask", "pending", "HUMAN_APPROVAL_REQUIRED"))
	deny := taskEvent(h, "e3", "POLICY_DECIDED", "", decision(h, "Deny", "not_requested", "INSUFFICIENT_BUDGET"))
	cancel := taskEvent(h, "e4", "TASK_CANCELLED", "HOST_CANCELLED", nil)
	withdrawn := taskEvent(h, "e5", "APPROVAL_WITHDRAWN", "", decision(h, "Ask", "withdrawn", "HOST_CANCELLED"))
	expired := taskEvent(h, "e6", "APPROVAL_EXPIRED", "", decision(h, "Ask", "expired", "APPROVAL_TIMEOUT"))

	// Host cancels while the Ask is pending: the request is withdrawn after.
	legal := linked(t, start, prop, ask, cancel, withdrawn)
	mustPass(t, ValidateEventOrder(legal))
	for _, e := range legal {
		mustPass(t, r.ValidateEvent(e, orderFor(e, &p)))
	}
	wantCode(t, ValidateEventOrder(linked(t, start, prop, ask, withdrawn, expired)), "INVALID_EVENT_ORDER")
	wantCode(t, ValidateEventOrder(linked(t, start, prop, deny, expired)), "INVALID_EVENT_ORDER")

	// TASK_TIMED_OUT is the CLI timeout; it cannot carry the approval timeout.
	bad := linked(t, start, taskEvent(h, "e2", "TASK_TIMED_OUT", "APPROVAL_TIMEOUT", nil))
	mustFail(t, r.ValidateEvent(bad[1], nil))
}

// C2: transition rules live in ValidateEventOrder; historical events of a
// finished task stay verifiable; new purchases are refused by CheckTaskActive.
func TestC2EventTransitionRules(t *testing.T) {
	p := purchase(t)
	h := intentHash(t, p)
	start := taskEvent(h, "e1", "TASK_STARTED", "", nil)
	prop := taskEvent(h, "e2", "PURCHASE_PROPOSED", "", nil)
	decided := func(id, verdict, approval, reason string) AuditEvent {
		return taskEvent(h, id, "POLICY_DECIDED", "", decision(h, verdict, approval, reason))
	}
	ask, deny, allow := decided("e3", "Ask", "pending", "HUMAN_APPROVAL_REQUIRED"), decided("e3", "Deny", "not_requested", "INSUFFICIENT_BUDGET"), decided("e4", "Allow", "not_requested", "PAYMENT_CAPABILITY_UNAVAILABLE")
	cancel := taskEvent(h, "e5", "TASK_CANCELLED", "HOST_CANCELLED", nil)
	timeout := taskEvent(h, "e6", "TASK_TIMED_OUT", "CLI_TIMEOUT", nil)
	for name, events := range map[string][]AuditEvent{
		"Deny then Ask":             {start, prop, deny, decided("e4", "Ask", "pending", "HUMAN_APPROVAL_REQUIRED")},
		"Ask then Allow":            {start, prop, ask, allow},
		"decision without proposal": {start, ask},
		"purchase after cancel":     {start, cancel, prop},
		"decision after timeout":    {start, prop, timeout, ask},
		"proposal twice":            {start, prop, taskEvent(h, "e7", "PURCHASE_PROPOSED", "", nil)},
		"no TASK_STARTED first":     {prop, ask},
		"TASK_STARTED twice":        {start, taskEvent(h, "e8", "TASK_STARTED", "", nil)},
		"second task end":           {start, cancel, timeout},
	} {
		if err := ValidateEventOrder(linked(t, events...)); ErrorCode(err) != "INVALID_EVENT_ORDER" {
			t.Fatalf("%s: got %v", name, err)
		}
	}
	// A pending Ask may still expire after the CLI timed out (CLI timeout does
	// not change the purchase verdict).
	expired := taskEvent(h, "e9", "APPROVAL_EXPIRED", "", decision(h, "Ask", "expired", "APPROVAL_TIMEOUT"))
	mustPass(t, ValidateEventOrder(linked(t, start, prop, ask, timeout, expired)))

	// History of a task that is now cancelled still validates.
	_, c := registry(t)
	for i := range c.Tasks {
		if c.Tasks[i].TaskID == "task-a" {
			c.Tasks[i].Status = "cancelled"
		}
	}
	r, err := NewRegistry(c)
	mustPass(t, err)
	events := chainFixture(t)
	mustPass(t, ValidateEventOrder(events))
	for _, e := range events {
		mustPass(t, r.ValidateEvent(e, orderFor(e, &p)))
	}
	for _, s := range []string{"created", "running", "awaiting_approval"} {
		mustPass(t, CheckTaskActive(s))
	}
	for _, s := range []string{"denied", "cancelled", "failed"} {
		wantCode(t, CheckTaskActive(s), "TASK_NOT_ACTIVE")
	}
	if err := CheckTaskActive("paused"); err == nil || ErrorCode(err) != "" {
		t.Fatal("unknown status must be a plain error")
	}
}

// C3: purchase_order_id is unique per task and the EIP-3009 nonce per payer
// and asset, across request IDs.
func TestC3RetryUniqueness(t *testing.T) {
	// Each case isolates one rule, so neither can mask the other.
	p := purchase(t)
	fresh := "0x" + strings.Repeat("b", 64)
	q := p
	q.RequestID, q.Intent.EIP3009Nonce = "request-2", fresh // same order, fresh nonce
	_, err := CompareRetry(p, q)
	wantCode(t, err, "IDEMPOTENCY_CONFLICT")

	q = p
	q.RequestID, q.Intent.PurchaseOrderID = "request-2", "po-a-2" // new order, same nonce
	_, err = CompareRetry(p, q)
	wantCode(t, err, "IDEMPOTENCY_CONFLICT")

	q.Intent.EIP3009Nonce = fresh
	if out, err := CompareRetry(p, q); err != nil || out != "new" {
		t.Fatalf("fresh order and nonce: %s %v", out, err)
	}
	if out, err := CompareRetry(p, asB(p)); err != nil || out != "new" {
		t.Fatalf("A and B with the same request_id: %s %v", out, err)
	}
}

// C4: keys differing only by case are rejected anywhere in a quote.
func TestC4CaseVariantKeysRejected(t *testing.T) {
	p := purchase(t)
	bad := mutatePurchase(t, p, "case_variant_pay_to")
	wantCode(t, bad.Validate(), "AMBIGUOUS_KEY")
	q := p.Quote
	q.Extensions = json.RawMessage(`{"pricing":{"Fee":"1","fee":"2"}}`)
	_, err := q.Hash()
	wantCode(t, err, "AMBIGUOUS_KEY")
	q.Extensions = json.RawMessage(`{"pricing":{"fee":"1","feeCap":"2"}}`)
	_, err = q.Hash()
	mustPass(t, err)
}

// C5: trusted assets live in the registry; the contract validates entries and
// exposes lookup, Policy (B1-05) denies unlisted ones.
func TestC5TrustedAssets(t *testing.T) {
	r, c := registry(t)
	a, ok := r.TrustedAsset("84532", "0x4444444444444444444444444444444444444444")
	if !ok || a.AssetID != "fixture-usdc" || a.Decimals != 6 || a.EIP712Name != "USDC" || a.EIP712Version != "2" {
		t.Fatalf("fixture asset: %+v %v", a, ok)
	}
	unlisted := "0x9999999999999999999999999999999999999999"
	if _, ok := r.TrustedAsset("84532", unlisted); ok {
		t.Fatal("unlisted asset found")
	}
	// Shape validation passes; it is not a Policy allow.
	p := editQuote(t, purchase(t), func(req map[string]any) { req["asset"] = unlisted })
	p.Intent.AssetContract = unlisted
	mustPass(t, r.ValidatePurchase(Session{"consumer-a", "agent-a", "task-a"}, p))
	if !slices.Contains(denyReasons, "ASSET_NOT_ALLOWED") {
		t.Fatal("ASSET_NOT_ALLOWED must be a Deny reason")
	}

	good := c.Assets[0]
	for name, assets := range map[string][]Asset{
		"none":                    {},
		"duplicate id":            {good, func() Asset { a := good; a.AssetContract = unlisted; return a }()},
		"duplicate chain+address": {good, func() Asset { a := good; a.AssetID = "other"; return a }()},
		"decimals 78":             {func() Asset { a := good; a.Decimals = 78; return a }()},
		"empty name":              {func() Asset { a := good; a.EIP712Name = ""; return a }()},
		"control char version":    {func() Asset { a := good; a.EIP712Version = "2\n"; return a }()},
		"bad address":             {func() Asset { a := good; a.AssetContract = "0xABC"; return a }()},
		"zero chain":              {func() Asset { a := good; a.ChainID = "0"; return a }()},
	} {
		bad := c
		bad.Assets = assets
		if _, err := NewRegistry(bad); err == nil {
			t.Fatalf("%s: registry accepted", name)
		}
	}
	edge := c
	edge.Assets = []Asset{func() Asset { a := good; a.Decimals = 77; return a }()}
	_, err := NewRegistry(edge)
	mustPass(t, err)
	// An Agent's on-chain identity (docs/commercial-8004.md): optional, CAIP-19 when given.
	for ref, ok := range map[string]bool{
		"eip155:84532/erc721:0x8004A818BFB912233c491871b3d84c89A494BD9e/10569": true,
		"eip155:84532:0x8004A818BFB912233c491871b3d84c89A494BD9e#10569":        false,
		"eip155:84532/erc721:0x8004/10569":                                     false,
		"eip155:84532/erc721:0x8004A818BFB912233c491871b3d84c89A494BD9e/":      false,
	} {
		named := c
		named.Consumers = slices.Clone(c.Consumers)
		named.Consumers[0].AgentRegistryRef = ref
		if _, err := NewRegistry(named); (err == nil) != ok {
			t.Fatalf("agent ref %q: %v", ref, err)
		}
	}

	var m CaseManifest
	fixture(t, "cases.json", &m)
	mustPass(t, m.ValidateRefs(r))
	m.Defaults["asset"] = "unknown-asset"
	wantCode(t, m.ValidateRefs(r), "INVALID_PARAMETER")
}

// C6: the reviewer-facing manifest carries the ownership, cancel, timeout and
// asset cases (contract-layer ones run in TestContractCasesFromManifest).
func TestC6ManifestCoversRequiredCases(t *testing.T) {
	var m CaseManifest
	fixture(t, "cases.json", &m)
	var got []string
	for _, c := range m.Cases {
		got = append(got, c.CaseID)
	}
	for _, id := range []string{"cross-consumer", "cross-consumer-wallet", "cross-consumer-payer", "altered-consumer-id", "altered-agent-id", "retry-same-request-id-a-b", "retry-new-request-same-order", "cancel-pending", "cancel-no-purchase", "approval-deadline-before", "approval-deadline-exact", "approval-timeout", "cli-timeout", "cli-timeout-pending", "deny-asset", "restart"} {
		if !slices.Contains(got, id) {
			t.Fatalf("manifest lacks case %s", id)
		}
	}
}

// C7: reasons are a closed list and errors carry them as codes.
func TestC7ReasonCodes(t *testing.T) {
	if len(slices.Compact(slices.Sorted(slices.Values(ReasonCodes)))) != len(ReasonCodes) {
		t.Fatal("duplicate reason code")
	}
	for key, reasons := range outcomes {
		for _, r := range reasons {
			if !slices.Contains(ReasonCodes, r) {
				t.Fatalf("%s uses %s outside ReasonCodes", key, r)
			}
		}
	}
	if ErrorCode(nil) != "" {
		t.Fatal("nil error has a code")
	}
	p := purchase(t)
	err := PolicyDecision{Version, intentHash(t, p), "Ask", "pending", "Settled", "HUMAN_APPROVAL_REQUIRED"}.Validate()
	wantCode(t, err, "M1_PAYMENT_UNAVAILABLE")
}

// C8: manifest and run-manifest validation rules, one violation per case.
func TestC8ManifestValidationRules(t *testing.T) {
	load := func() CaseManifest {
		var m CaseManifest
		fixture(t, "cases.json", &m)
		return m
	}
	byID := func(m CaseManifest, id string) *AcceptanceCase {
		for i := range m.Cases {
			if m.Cases[i].CaseID == id {
				return &m.Cases[i]
			}
		}
		t.Fatalf("no case %s", id)
		return nil
	}
	mustPass(t, load().Validate())
	for name, tc := range map[string]struct {
		edit func(CaseManifest)
		code string
	}{
		"unknown operation":         {func(m CaseManifest) { byID(m, "ask").Operation = "teleport" }, "UNKNOWN_OPERATION"},
		"operation in wrong layer":  {func(m CaseManifest) { byID(m, "retry-same").Layer = "integration" }, "UNKNOWN_OPERATION"},
		"misspelled parameter":      {func(m CaseManifest) { byID(m, "deny-budget").Parameters["remianing"] = "1" }, "UNKNOWN_PARAMETER"},
		"parameter out of domain":   {func(m CaseManifest) { byID(m, "cancel-pending").Parameters["before"] = "approved" }, "INVALID_PARAMETER"},
		"missing required param":    {func(m CaseManifest) { delete(byID(m, "cancel-pending").Parameters, "before") }, "INVALID_PARAMETER"},
		"extra defaults key":        {func(m CaseManifest) { m.Defaults["color"] = "red" }, "UNKNOWN_PARAMETER"},
		"missing defaults key":      {func(m CaseManifest) { delete(m.Defaults, "asset") }, "INVALID_PARAMETER"},
		"Ask not pending":           {func(m CaseManifest) { byID(m, "ask").Expected.ApprovalStatus = "not_requested" }, "CONTRADICTORY_EXPECTATION"},
		"Deny pending":              {func(m CaseManifest) { byID(m, "deny-budget").Expected.ApprovalStatus = "pending" }, "CONTRADICTORY_EXPECTATION"},
		"Allow other reason":        {func(m CaseManifest) { byID(m, "allow-unavailable").Expected.Reason = "HUMAN_APPROVAL_REQUIRED" }, "CONTRADICTORY_EXPECTATION"},
		"result not for operation":  {func(m CaseManifest) { byID(m, "ask").Expected.Result = "replay" }, "CONTRADICTORY_EXPECTATION"},
		"reason outside list":       {func(m CaseManifest) { byID(m, "deny-budget").Expected.Reason = "TOO_EXPENSIVE" }, "CONTRADICTORY_EXPECTATION"},
		"missing forbidden event":   {func(m CaseManifest) { e := &byID(m, "ask").Expected; e.ForbiddenEvents = e.ForbiddenEvents[1:] }, "CONTRADICTORY_EXPECTATION"},
		"budget deducted":           {func(m CaseManifest) { byID(m, "ask").Expected.BudgetDeducted = "1" }, "CONTRADICTORY_EXPECTATION"},
		"withdrawn written as none": {func(m CaseManifest) { byID(m, "cancel-pending").Expected.ApprovalStatus = "not_requested" }, "CONTRADICTORY_EXPECTATION"},
		"case id is a capability":   {func(m CaseManifest) { byID(m, "ask").CaseID = "webauthn" }, "INVALID_MANIFEST"},
	} {
		m := load()
		tc.edit(m)
		if err := m.Validate(); ErrorCode(err) != tc.code {
			t.Fatalf("%s: got %v, want %s", name, err, tc.code)
		}
	}

	m := load()
	m.ReviewStatus, m.Reviewer = "accepted", "test-reviewer"
	digest, err := m.Hash()
	mustPass(t, err)
	run := func() RunManifest {
		r := RunManifest{Version: Version, RunID: "run", CodeCommit: "c0ffee", DirtyPatchDigest: "sha256:" + strings.Repeat("0", 64), ConfigDigest: "sha256:" + strings.Repeat("1", 64), CaseManifestDigest: digest, Versions: map[string]string{"go": "test"}, Commands: []CommandResult{{"test", 0}}, NotEvaluated: slices.Clone(CapabilityIDs)}
		for _, c := range m.Cases {
			r.Results = append(r.Results, ClaimResult{c.CaseID, "INCONCLUSIVE", nil, "not executed"})
		}
		return r
	}
	mustPass(t, run().ValidateAgainst(m))
	r := run()
	r.NotEvaluated = append(r.NotEvaluated, "ask-typo")
	wantCode(t, r.ValidateAgainst(m), "UNKNOWN_CLAIM")
	r = run()
	r.NotEvaluated = slices.DeleteFunc(r.NotEvaluated, func(id string) bool { return id == "signer_key_selection" })
	wantCode(t, r.ValidateAgainst(m), "CAPABILITY_CLAIM_INVALID")
	r = run()
	r.Evidence = []EvidenceRef{{"e-1", "log.txt", "sha256:" + strings.Repeat("2", 64), "fixture", "test"}}
	r.Results = append(r.Results, ClaimResult{"onchain_settlement", "PASS", []string{"e-1"}, "claimed"})
	wantCode(t, r.ValidateAgainst(m), "CAPABILITY_CLAIM_INVALID")
}

// C9: an optional string is omitted or meaningful, as in the schema.
func TestC9EmptyOptionalStringsRejected(t *testing.T) {
	events := chainFixture(t)
	raw, err := json.Marshal(events[0])
	mustPass(t, err)
	var ok AuditEvent
	mustPass(t, Decode(raw, &ok))
	for _, patch := range []func(map[string]any){
		func(e map[string]any) { e["payload"] = map[string]any{"code": ""} },
		func(e map[string]any) { e["scope"].(map[string]any)["purchase_order_id"] = "" },
		func(e map[string]any) { e["prev_event_hash"] = "" },
	} {
		var e map[string]any
		mustPass(t, json.Unmarshal(raw, &e))
		patch(e)
		b, err := json.Marshal(e)
		mustPass(t, err)
		var got AuditEvent
		mustFail(t, Decode(b, &got))
	}
}

// C10: nesting counts containers only and stops at MaxJSONDepth; any '#' in a
// resource URL is refused.
func TestC10NestingAndFragment(t *testing.T) {
	nest := func(n int, inner string) []byte {
		return []byte(strings.Repeat("[", n) + inner + strings.Repeat("]", n))
	}
	_, err := Canonical(nest(MaxJSONDepth, "1"))
	mustPass(t, err)
	_, err = Canonical(nest(MaxJSONDepth+1, ""))
	mustFail(t, err)
	_, err = Canonical([]byte(strings.Repeat(`{"a":`, MaxJSONDepth) + "1" + strings.Repeat("}", MaxJSONDepth)))
	mustPass(t, err)
	p := purchase(t)
	for _, u := range []string{p.Intent.ResourceURL + "#", p.Intent.ResourceURL + "#part"} {
		i := p.Intent
		i.ResourceURL = u
		i.ResourceHash, _ = ResourceHash(i.HTTPMethod, u)
		mustFail(t, i.Validate())
	}
}

// SA1: an M1 intent carries an EIP-3009 nonce, so only that path is accepted.
func TestSA1TransferMethod(t *testing.T) {
	p := purchase(t)
	mustPass(t, p.Validate()) // fixture omits assetTransferMethod: default eip3009
	set := func(v any) PurchaseOrder {
		return editQuote(t, p, func(req map[string]any) { req["extra"].(map[string]any)["assetTransferMethod"] = v })
	}
	mustPass(t, set("eip3009").Validate())
	for _, v := range []any{"permit2", "erc7710", "", "EIP3009", 1} {
		wantCode(t, set(v).Validate(), "UNSUPPORTED_TRANSFER_METHOD")
	}
	notObject := editQuote(t, p, func(req map[string]any) { req["extra"] = "eip3009" })
	wantCode(t, notObject.Validate(), "UNSUPPORTED_TRANSFER_METHOD")
}

// Decision 5: prev_event_hash chain per task, genesis bound to the task.
func TestEventChain(t *testing.T) {
	events := chainFixture(t)
	mustPass(t, ValidateEventOrder(events))
	ga, err := GenesisHash("task-a")
	mustPass(t, err)
	gb, err := GenesisHash("task-b")
	mustPass(t, err)
	if ga == gb || events[0].PrevEventHash != ga {
		t.Fatal("genesis must be per task and start the chain")
	}
	// The hash covers prev_event_hash; an event has no field for its own hash.
	e := events[0]
	h1, _ := EventHash(e)
	e.PrevEventHash = gb
	h2, _ := EventHash(e)
	if h1 == h2 {
		t.Fatal("event hash ignores prev_event_hash")
	}

	edit := func(f func([]AuditEvent) []AuditEvent) error { return ValidateEventOrder(f(chainFixture(t))) }
	wantCode(t, edit(func(es []AuditEvent) []AuditEvent { es[1].ObservedAt++; return es }), "EVENT_CHAIN_BROKEN")
	wantCode(t, edit(func(es []AuditEvent) []AuditEvent { es[2].Payload.Decision.Reason = "APPROVAL_TIMEOUT"; return es }), "EVENT_CHAIN_BROKEN")
	wantCode(t, edit(func(es []AuditEvent) []AuditEvent { es[0].PrevEventHash = gb; return es }), "EVENT_CHAIN_BROKEN")
	wantCode(t, edit(func(es []AuditEvent) []AuditEvent { return slices.Delete(es, 1, 2) }), "INVALID_EVENT_ORDER")
	wantCode(t, edit(func(es []AuditEvent) []AuditEvent { es[1], es[2] = es[2], es[1]; return es }), "INVALID_EVENT_ORDER")
	wantCode(t, edit(func(es []AuditEvent) []AuditEvent {
		dup := es[1]
		dup.EventID = "evt-a-inserted"
		return slices.Insert(es, 2, dup)
	}), "INVALID_EVENT_ORDER")

	// Known limits without an external checkpoint: a truncated tail and a fully
	// recomputed chain both pass. Persistence and anchoring are B1-05's job.
	mustPass(t, edit(func(es []AuditEvent) []AuditEvent { return es[:2] }))
	mustPass(t, edit(func(es []AuditEvent) []AuditEvent {
		es[1].ObservedAt++
		return linked(t, es...)
	}))

	r, _ := registry(t)
	p := purchase(t)
	noPrev := events[1]
	noPrev.PrevEventHash = ""
	mustFail(t, r.ValidateEvent(noPrev, &p))
	system := AuditEvent{Version: Version, EventID: "system-1", EventType: "SYSTEM_READY", EvidenceMode: "fixture", Source: "host", ObservedAt: 1790726450, PrevEventHash: ga}
	mustFail(t, r.ValidateEvent(system, nil))
}

// variant is a valid copy of the fixture purchase with another order ID and
// nonce; the quote is unchanged, so its snapshot validates on its own.
func variant(t *testing.T, p PurchaseOrder, po, nonce string) PurchaseOrder {
	t.Helper()
	p.Intent.PurchaseOrderID, p.Intent.EIP3009Nonce = po, nonce
	mustPass(t, p.Validate())
	return p
}

// orderEvent is taskEvent for purchase p (its order ID) under intent h.
func orderEvent(p PurchaseOrder, h, id, typ string, d *PolicyDecision) AuditEvent {
	e := taskEvent(h, id, typ, "", d)
	e.Scope.PurchaseOrderID = p.Intent.PurchaseOrderID
	return e
}

// validEach requires every event to pass ValidateEvent with the snapshot of
// the intent it names, so a stream failure can only come from ordering rules.
func validEach(t *testing.T, r *Registry, events []AuditEvent, snapshots map[string]*PurchaseOrder) {
	t.Helper()
	for _, e := range events {
		var order *PurchaseOrder
		if e.Scope.PurchaseOrderID != "" {
			order = snapshots[e.Scope.IntentHash]
		}
		if err := r.ValidateEvent(e, order); err != nil {
			t.Fatalf("event %s is not valid on its own: %v", e.EventID, err)
		}
	}
}

// Round 2 (1): each purchase keeps the intent_hash of its first proposal.
func TestIntentBindingAcrossEvents(t *testing.T) {
	r, _ := registry(t)
	p1 := purchase(t)
	p1b := variant(t, p1, "po-a-1", "0x"+strings.Repeat("b", 64)) // same order, other content
	h1, h2 := intentHash(t, p1), intentHash(t, p1b)
	snapshots := map[string]*PurchaseOrder{h1: &p1, h2: &p1b}
	start := taskEvent(h1, "e1", "TASK_STARTED", "", nil)
	prop := taskEvent(h1, "e2", "PURCHASE_PROPOSED", "", nil)
	ask := func(h string) AuditEvent {
		return taskEvent(h, "e3", "POLICY_DECIDED", "", decision(h, "Ask", "pending", "HUMAN_APPROVAL_REQUIRED"))
	}
	expired := func(h string) AuditEvent {
		return taskEvent(h, "e4", "APPROVAL_EXPIRED", "", decision(h, "Ask", "expired", "APPROVAL_TIMEOUT"))
	}
	withdrawn := func(h string) AuditEvent {
		return taskEvent(h, "e4", "APPROVAL_WITHDRAWN", "", decision(h, "Ask", "withdrawn", "HOST_CANCELLED"))
	}
	for _, ok := range [][]AuditEvent{{start, prop, ask(h1), expired(h1)}, {start, prop, ask(h1), withdrawn(h1)}} {
		events := linked(t, ok...)
		validEach(t, r, events, snapshots)
		mustPass(t, ValidateEventOrder(events))
	}
	for _, bad := range []struct {
		name   string
		events []AuditEvent
	}{
		{"proposal H1, decision H2", []AuditEvent{start, prop, ask(h2)}},
		{"proposal H1, Ask H1, expired H2", []AuditEvent{start, prop, ask(h1), expired(h2)}},
		{"proposal H1, Ask H1, withdrawn H2", []AuditEvent{start, prop, ask(h1), withdrawn(h2)}},
	} {
		events := linked(t, bad.events...)
		validEach(t, r, events, snapshots)
		mustPass(t, ValidateEventOrder(events[:len(events)-1])) // prefix and chain are valid
		err := ValidateEventOrder(events)
		wantCode(t, err, "PURCHASE_INTENT_CHANGED")
		t.Logf("%s: guard hit: %v", bad.name, err)
	}
}

// Round 2 (2): a Deny ends the task. No purchase or decision follows; an Ask
// already open may still expire or be withdrawn; history stays verifiable.
func TestDenyEndsTask(t *testing.T) {
	r, c := registry(t)
	p1 := purchase(t)
	p2 := variant(t, p1, "po-a-2", "0x"+strings.Repeat("c", 64)) // another order, new nonce
	h1, h2 := intentHash(t, p1), intentHash(t, p2)
	snapshots := map[string]*PurchaseOrder{h1: &p1, h2: &p2}
	start := taskEvent(h1, "e1", "TASK_STARTED", "", nil)
	prop := func(p PurchaseOrder, h, id string) AuditEvent { return orderEvent(p, h, id, "PURCHASE_PROPOSED", nil) }
	decide := func(p PurchaseOrder, h, id, verdict, approval, reason string) AuditEvent {
		return orderEvent(p, h, id, "POLICY_DECIDED", decision(h, verdict, approval, reason))
	}
	deny := func(p PurchaseOrder, h, id string) AuditEvent {
		return decide(p, h, id, "Deny", "not_requested", "INSUFFICIENT_BUDGET")
	}
	askOpen := decide(p1, h1, "e3", "Ask", "pending", "HUMAN_APPROVAL_REQUIRED")

	for _, bad := range []struct {
		name, guard string
		events      []AuditEvent
	}{
		{"proposal of another order after Deny", "no new purchase after the task ended",
			[]AuditEvent{start, prop(p1, h1, "e2"), deny(p1, h1, "e3"), prop(p2, h2, "e4")}},
		{"Ask on another order after Deny", "no decision after the task ended",
			[]AuditEvent{start, prop(p1, h1, "e2"), prop(p2, h2, "e3"), deny(p1, h1, "e4"), decide(p2, h2, "e5", "Ask", "pending", "HUMAN_APPROVAL_REQUIRED")}},
		{"Deny on another order after Deny", "no decision after the task ended",
			[]AuditEvent{start, prop(p1, h1, "e2"), prop(p2, h2, "e3"), deny(p1, h1, "e4"), deny(p2, h2, "e5")}},
	} {
		events := linked(t, bad.events...)
		validEach(t, r, events, snapshots)
		mustPass(t, ValidateEventOrder(events[:len(events)-1]))
		err := ValidateEventOrder(events)
		if ErrorCode(err) != "INVALID_EVENT_ORDER" || !strings.Contains(err.Error(), bad.guard) {
			t.Fatalf("%s: got %v, want guard %q", bad.name, err, bad.guard)
		}
		t.Logf("%s: guard hit: %v", bad.name, err)
	}

	// The Ask on po-a-1 was open when po-a-2 was denied: it can still close.
	closings := []AuditEvent{
		orderEvent(p1, h1, "e6", "APPROVAL_EXPIRED", decision(h1, "Ask", "expired", "APPROVAL_TIMEOUT")),
		orderEvent(p1, h1, "e6", "APPROVAL_WITHDRAWN", decision(h1, "Ask", "withdrawn", "HOST_CANCELLED")),
	}
	for _, closing := range closings {
		events := linked(t, start, prop(p1, h1, "e2"), askOpen, prop(p2, h2, "e4"), deny(p2, h2, "e5"), closing)
		validEach(t, r, events, snapshots)
		mustPass(t, ValidateEventOrder(events))
	}

	// A CLI timeout after the Deny is still recorded once (it changes no verdict).
	timeout := taskEvent(h1, "e4", "TASK_TIMED_OUT", "CLI_TIMEOUT", nil)
	cancel := taskEvent(h1, "e5", "TASK_CANCELLED", "HOST_CANCELLED", nil)
	denied := linked(t, start, prop(p1, h1, "e2"), deny(p1, h1, "e3"), timeout)
	mustPass(t, ValidateEventOrder(denied))
	err := ValidateEventOrder(linked(t, start, prop(p1, h1, "e2"), deny(p1, h1, "e3"), timeout, cancel))
	if ErrorCode(err) != "INVALID_EVENT_ORDER" || !strings.Contains(err.Error(), "one TASK_CANCELLED or TASK_TIMED_OUT per task") {
		t.Fatalf("second lifecycle end: %v", err)
	}

	// History of the denied task validates against a registry that now says so.
	for i := range c.Tasks {
		if c.Tasks[i].TaskID == "task-a" {
			c.Tasks[i].Status = "denied"
		}
	}
	rDenied, err := NewRegistry(c)
	mustPass(t, err)
	validEach(t, rDenied, denied, snapshots)
	wantCode(t, CheckTaskActive("denied"), "TASK_NOT_ACTIVE")
}

// Round 3: advance-clock expectations must follow elapsed >= deadline. Each
// edit keeps result, reason and approval consistent with one another, so only
// the deadline arithmetic can reject it.
func TestDeadlineManifestConsistency(t *testing.T) {
	load := func() CaseManifest {
		var m CaseManifest
		fixture(t, "cases.json", &m)
		return m
	}
	byID := func(m CaseManifest, id string) *AcceptanceCase {
		for i := range m.Cases {
			if m.Cases[i].CaseID == id {
				return &m.Cases[i]
			}
		}
		t.Fatalf("no case %s", id)
		return nil
	}
	pending := func(c *AcceptanceCase) {
		c.Expected.Result, c.Expected.Reason, c.Expected.ApprovalStatus = "pending", "HUMAN_APPROVAL_REQUIRED", "pending"
	}
	expired := func(c *AcceptanceCase) {
		c.Expected.Result, c.Expected.Reason, c.Expected.ApprovalStatus = "denied", "APPROVAL_TIMEOUT", "expired"
	}
	m := load()
	mustPass(t, m.Validate())
	for _, id := range []string{"approval-deadline-before", "approval-deadline-exact", "approval-timeout"} {
		c := byID(m, id)
		if got := deadlineResult(c.Parameters); got != c.Expected.Result {
			t.Fatalf("%s: fixture says %s, rule says %s", id, c.Expected.Result, got)
		}
	}
	for name, edit := range map[string]func(CaseManifest){
		"30 of 30 seconds written as pending": func(m CaseManifest) { pending(byID(m, "approval-deadline-exact")) },
		"29 of 30 seconds written as expired": func(m CaseManifest) { expired(byID(m, "approval-deadline-before")) },
		"31 changed to 29, still expired":     func(m CaseManifest) { byID(m, "approval-timeout").Parameters["advance_seconds"] = "29" },
	} {
		m := load()
		edit(m)
		err := m.Validate()
		if ErrorCode(err) != "CONTRADICTORY_EXPECTATION" || !strings.Contains(err.Error(), "elapsed >= deadline") {
			t.Fatalf("%s: got %v", name, err)
		}
		t.Logf("%s: guard hit: %v", name, err)
	}
}

// keysOf is a sorted key list for comparing Go tables with schema enums.
func keysOf[V any](m map[string]V) []string { return slices.Sorted(maps.Keys(m)) }
