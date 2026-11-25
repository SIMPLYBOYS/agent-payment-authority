package verification

// The E1-a evidence pack and its claims (docs/commercial-e1a.md 4.5). The
// checker reads the pack and trusted repository files only: it opens no
// Store, calls no Policy, Signer or Store code, and takes no producer verdict.
// It shares the pay contract's pure functions (event and intent hashes,
// registry bindings); a fault in them is shared too (a limit it reports).
// E4 and E8 read the chain itself (e1a_chain.go); without an RPC they are
// INCONCLUSIVE.

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"maps"
	"net/url"
	"os"
	"os/exec"
	"path"
	"path/filepath"
	"regexp"
	"slices"
	"strconv"
	"strings"

	"github.com/SIMPLYBOYS/agent-payment-authority/internal/ae1"
	"github.com/SIMPLYBOYS/agent-payment-authority/internal/buyer"
	"github.com/SIMPLYBOYS/agent-payment-authority/internal/contracts"
	"github.com/SIMPLYBOYS/agent-payment-authority/internal/merchant"
	"github.com/SIMPLYBOYS/agent-payment-authority/internal/paycontract"
	"github.com/SIMPLYBOYS/agent-payment-authority/internal/x402"
)

// E1aVersion is the pack's version; E1aChecker this checker's rules.
const (
	E1aVersion = "apa.e1a.v1"
	E1aChecker = "e1a.4"
)

// E1aClaims are §4.5's claims.
var E1aClaims = []string{"E1", "E2", "E3", "E4", "E5", "E6", "E7", "E8", "E9", "E10"}

// E1aManifest is the pack's index (manifest.json). Files lists every other
// file but run-end.json, with its sha256.
type E1aManifest struct {
	Version   string    `json:"version"`
	RunID     string    `json:"run_id"`
	CreatedAt uint64    `json:"created_at"`
	Commit    string    `json:"code_commit"`
	Start     E1aStart  `json:"start"`
	Tasks     []E1aTask `json:"tasks"`
	Files     []E1aFile `json:"files"`
}

// E1aStart is what was recorded before the runs (§4.7): the chain, the
// first block E8 counts from, the payer wallets and the contracts.
type E1aStart struct {
	Chain          string   `json:"chain"` // base_sepolia | local_anvil
	ChainID        uint64   `json:"chain_id"`
	FromBlock      uint64   `json:"from_block"`
	ToBlock        uint64   `json:"to_block"` // set when packed
	Wallets        []string `json:"wallets"`
	Token          string   `json:"token"`
	PayTo          string   `json:"pay_to"`
	BudgetContract string   `json:"budget_contract"`
	Verifier       string   `json:"verifier"`
	// Facilitator is the host directory's facilitator_url: who the shop settled through
	// (docs/commercial-facilitator.md FA4); none, the Host's shop settled itself.
	Facilitator string `json:"facilitator,omitempty"`
}

// E1aSettlement is one tx#2 and who sent it, as the chain has it ("" not
// read): listed, not a claim (E4 and E8 read the token's own events).
type E1aSettlement struct {
	PurchaseOrderID string `json:"purchase_order_id"`
	Transaction     string `json:"transaction"`
	SentBy          string `json:"sent_by"`
	// Refund: the shop's refund of this payment, settled and not delivered (docs/commercial-refund.md), as E4
	// read it on chain: its transaction and sender.
	Refund *E1aRefund `json:"refund,omitempty"`
}

// E1aRefund is a refund's transaction and who sent it ("" when it cannot be read).
type E1aRefund struct {
	Transaction string `json:"transaction"`
	SentBy      string `json:"sent_by"`
}

// E1aTask is one run: its task, the E1-a catalog entry it ran, and, for the
// injection run, the AE1 case whose patch the shop sold (E10).
type E1aTask struct {
	TaskID        string `json:"task_id"`
	Scenario      string `json:"scenario"`
	InjectionCase string `json:"injection_case,omitempty"`
}

type E1aFile struct {
	Path   string `json:"path"`
	SHA256 string `json:"sha256"`
}

// E1aEnd is run-end.json: the pack is whole, and which manifest it closes.
type E1aEnd struct {
	RunID          string `json:"run_id"`
	State          string `json:"state"` // complete
	ManifestSHA256 string `json:"manifest_sha256"`
}

// Injection is E10's record for one run: whether the model asked for what
// the shop's text pushed. It is not a spend: E8 says whether anything was
// paid without authority.
type Injection struct {
	TaskID    string   `json:"task_id"`
	Case      string   `json:"case"`
	Fooled    bool     `json:"fooled"`
	Requested []string `json:"requested"` // every product the model asked for
}

type E1aReport struct {
	Checker struct {
		Name    string `json:"name"`
		Version string `json:"version"`
		Build   string `json:"build"`
	} `json:"checker"`
	Mode           string             `json:"mode"` // e1a
	RunID          string             `json:"run_id"`
	ManifestSHA256 string             `json:"manifest_sha256"`
	Verdict        Verdict            `json:"verdict"`
	Claims         map[string]Verdict `json:"claims"`
	Findings       []Finding          `json:"findings"`
	Injections     []Injection        `json:"injections"`
	// Facilitator is the start's ("" the Host's shop settled itself); Settlements each tx#2's sender (with --rpc).
	Facilitator string          `json:"facilitator"`
	Settlements []E1aSettlement `json:"settlements"`
	// Agents: each consumer's ERC-8004 identity the registry names, as the chain has it (with --rpc).
	Agents       []E1aAgent `json:"agents"`
	NotEvaluated []string   `json:"not_evaluated"`
	Limits       []string   `json:"limits"`
}

// ExitCode as Report's.
func (r *E1aReport) ExitCode() int {
	switch r.Verdict {
	case Pass:
		return 0
	case Fail:
		return 1
	}
	return 2
}

var e1aNotEvaluated = []string{
	"the BBS+ proof re-verified by the checker (the pack keeps its sha256 only)",
	"voice FAR/FRR (V1's evaluation)",
	"Admin aggregation (M-D)",
	delegationNotEvaluated,
}

// delegationsByDevice (gap 4): every run's delegation was confirmed by the
// person's device, so that item is off not_evaluated (E6 labels it); one
// confirmed by the host, or none read, keeps it.
func delegationsByDevice(runs map[string]*e1aRun) bool {
	byDevice := len(runs) > 0
	for _, run := range runs {
		i := slices.IndexFunc(run.events, func(e contracts.AuditEvent) bool { return e.EventType == "DELEGATION_BOUND" })
		byDevice = byDevice && i >= 0 && run.events[i].Payload.AuthorizationMethod == "webauthn"
	}
	return byDevice
}

// delegationNotEvaluated is said only while a run's delegation was confirmed
// by the host for the customer; from gap 4 (e1a.2) one confirmed by the
// person's device is E6's to label, by its DELEGATION_BOUND.
const delegationNotEvaluated = "the delegation confirmed by the person (E1a-D1 A: the host plays the customer)"

var e1aLimits = []string{
	"a device or virtual authenticator is the host's declaration (--authenticator-source); the checker checks it is consistent, not that a device was physical",
	"a delegation confirmed by the person is read from its DELEGATION_BOUND (method webauthn, the Signer's confirmation named); the Signer's own record of it is not in the pack",
	"a shop's signed offer and receipt are checked to be one key's, and to say what the purchase and its settlement say; that the key is the shop's (its did:web document) was the Host's check when it took the quote: the pack has no copy of the document",
	"a revocation by the person is read from its DELEGATION_REVOKED (the Signer's revocation named): E3 checks nothing is requested after it; with the Signer's receipt, signed by the task's wallet, E2 checks every signed payment is one the Signer had signed before it; a Signer without the wallet's key gives no receipt, and then the order of a payment signed around the revocation is not evaluated",
	"event and intent hashes and registry bindings use the pay contract's own pure functions: a fault there is shared",
	"tx#2 is tied to its intent only through the Store's records and the EIP-3009 nonce, not by the chain",
	"the paying host's prompt is buyer.PayingTaskPrompt, the host's own function: a change to it moves the host and this check together",
	"the live catalogs are this repository's at the pack's code_commit (as they are now when the repository has no such commit): the pack names the commit its runs ran",
	"the secret scan finds key shapes (hex) and fields never whitelisted; a base64 secret is not recognized",
	"the facilitator is the host directory's setting at the start, and each tx#2's sender is the chain's; neither is a claim (a facilitator's address is not checked against its own list)",
	"an Agent's ERC-8004 identity is the registry's ref and what its registry answers, listed, not a claim: the payment wallet in its registration file is a declaration, and agent_wallet_is_consumer_wallet says whether the registry holds that wallet's own signed consent (setAgentWallet); ERC-8004 is a draft whose registry its owner can upgrade",
}

// tracePrompt is the prompt a live run of entry must have been given.
type e1aEntry struct {
	ID       string `json:"id"`
	Task     string `json:"task"`
	Contract string `json:"contract"`
}

// VerifyE1a checks the pack in dir against the repository at repo; chain
// reads the chain for E4 and E8 (nil: INCONCLUSIVE).
func VerifyE1a(dir, repo string, chain Chain) (*E1aReport, error) {
	b, err := openBundle(dir)
	if err != nil {
		return nil, err
	}
	defer b.close()
	r := &E1aReport{Mode: "e1a", NotEvaluated: e1aNotEvaluated, Limits: e1aLimits, Injections: []Injection{}, Settlements: []E1aSettlement{}, Agents: []E1aAgent{}}
	r.Checker.Name, r.Checker.Version = "verify-demo", E1aChecker
	var f findings
	defer func() {
		r.Findings = append([]Finding{}, f...)
		r.Claims = f.verdicts(E1aClaims)
		r.Verdict = f.verdict()
	}()
	m, ok := e1aManifest(b, &f, r)
	if !ok {
		return r, nil
	}
	catalog, err := e1aCatalog(repo, m.Commit)
	if err != nil {
		return nil, err
	}
	var reg contracts.RegistryConfig
	if !e1aJSON(b, &f, "E1", "registry.json", &reg) {
		return r, nil
	}
	registry, err := contracts.NewRegistry(reg)
	if err != nil {
		f.fail("E7", []string{"registry.json"}, "the registry is not one: %v", err)
		return r, nil
	}
	runs := map[string]*e1aRun{}
	for _, t := range m.Tasks {
		run := &e1aRun{task: t}
		base := "tasks/" + t.TaskID + "/"
		if !e1aJSON(b, &f, "E1", base+"events.json", &run.events) || !e1aJSON(b, &f, "E1", base+"purchases.json", &run.orders) {
			continue
		}
		if _, ok := b.files[base+"responses.json"]; ok && !e1aJSON(b, &f, "E1", base+"responses.json", &run.responses) {
			continue
		}
		runs[t.TaskID] = run
		e1aChain(&f, run)
		e1aShopSigned(&f, run)
		e1aRevocation(&f, run, registry)
		e1aIntents(&f, run)
		e1aOnce(&f, run)
		e1aModes(b, &f, run, catalog)
		e1aParties(&f, run, registry)
		if t.InjectionCase != "" {
			r.Injections = append(r.Injections, e1aInjection(&f, run, repo))
		}
	}
	if delegationsByDevice(runs) {
		r.NotEvaluated = slices.DeleteFunc(slices.Clone(r.NotEvaluated), func(s string) bool { return s == delegationNotEvaluated })
	}
	e1aApart(&f, runs, registry)
	e1aSafety(b, &f)
	r.Facilitator = m.Start.Facilitator
	r.Settlements = append(r.Settlements, e1aOnChain(&f, m, runs, chain, repo)...)
	slices.SortFunc(r.Settlements, func(a, b E1aSettlement) int { return strings.Compare(a.PurchaseOrderID, b.PurchaseOrderID) }) // runs are a map
	r.Agents = e1aAgents(chain, reg)
	return r, nil
}

type e1aRun struct {
	task      E1aTask
	events    []contracts.AuditEvent
	orders    []paycontract.PurchaseOrder
	responses []e1aResponse // the shop's PAYMENT-RESPONSE of each delivered payment (a pack from before 11/25 has none)
}

type e1aResponse struct {
	IntentHash      string              `json:"intent_hash"`
	PaymentResponse x402.SettleResponse `json:"payment_response"`
}

func e1aJSON(b *bundle, f *findings, claim, rel string, dst any) bool {
	raw, err := b.read(rel)
	if err != nil {
		f.fail(claim, []string{rel}, "%s is not in the pack as it should be: %v", rel, err)
		return false
	}
	if err := json.Unmarshal(raw, dst); err != nil {
		f.fail(claim, []string{rel}, "%s is not what it should hold: %v", rel, err)
		return false
	}
	return true
}

// e1aManifest is E1: the index, the end marker and every file's digest.
func e1aManifest(b *bundle, f *findings, r *E1aReport) (E1aManifest, bool) {
	var m E1aManifest
	raw, err := b.read("manifest.json")
	if err != nil {
		f.missing("E1", []string{"manifest.json"}, "no manifest: %v", err)
		return m, false
	}
	sum := sha256.Sum256(raw)
	r.ManifestSHA256 = hex.EncodeToString(sum[:])
	if err := json.Unmarshal(raw, &m); err != nil || m.Version != E1aVersion || m.RunID == "" || len(m.Tasks) == 0 {
		f.fail("E1", []string{"manifest.json"}, "not an %s manifest (%v)", E1aVersion, err)
		return m, false
	}
	r.RunID = m.RunID
	var end E1aEnd
	if e1aJSON(b, f, "E1", "run-end.json", &end) && (end.State != "complete" || end.RunID != m.RunID || end.ManifestSHA256 != r.ManifestSHA256) {
		f.fail("E1", []string{"run-end.json"}, "the pack is not closed by its own manifest: state %q, run %q", end.State, end.RunID)
	}
	listed := map[string]bool{"manifest.json": true, "run-end.json": true}
	for _, x := range m.Files {
		listed[x.Path] = true
		raw, err := b.read(x.Path)
		if err != nil {
			f.fail("E1", []string{x.Path}, "a listed file is not in the pack: %v", err)
			continue
		}
		if s := sha256.Sum256(raw); hex.EncodeToString(s[:]) != x.SHA256 {
			f.fail("E1", []string{x.Path}, "its sha256 is not the manifest's")
		}
	}
	for _, p := range e1aFiles(b) {
		if !listed[p] {
			f.fail("E1", []string{p}, "a file the manifest does not list")
		}
	}
	return m, true
}

// e1aChain is E3: each task's events in sequence, each naming the previous
// one's hash, a delivery never before its settlement; once the person
// revoked the delegation (docs/commercial-gap4.md 7), no purchase or
// payment is requested after it, and it names the task's delegation and the
// Signer's revocation.
func e1aChain(f *findings, run *e1aRun) {
	ev := "tasks/" + run.task.TaskID + "/events.json"
	settled := map[string]bool{}
	bound, revoked := "", false
	for i, e := range run.events {
		sp, ok := paycontract.SpecOf(e.Version)
		if !ok {
			f.fail("E3", []string{ev}, "event %d is not a pay-family event (%s)", i+1, e.Version)
			return
		}
		want, err := sp.GenesisHash(run.task.TaskID)
		if i > 0 {
			want, err = sp.EventHash(run.events[i-1])
		}
		if err != nil || e.Sequence != uint64(i+1) || e.PrevEventHash != want {
			f.fail("E3", []string{ev}, "event %d (%s) does not follow the one before it", i+1, e.EventType)
			return
		}
		intent := ""
		if e.Scope != nil {
			intent = e.Scope.IntentHash
		}
		switch e.EventType {
		case "DELEGATION_BOUND":
			bound = e.Payload.DelegationID
		case "DELEGATION_REVOKED":
			if revoked || bound == "" || e.Payload.DelegationID != bound || e.Payload.AuthorizationMethod != "webauthn" || e.Payload.SignerApprovalID == "" {
				f.fail("E3", []string{ev}, "DELEGATION_REVOKED (event %d) is not one revocation of the task's delegation by the person's device", i+1)
			}
			revoked = true
		case "PURCHASE_PROPOSED", "PAYMENT_REQUESTED":
			if revoked {
				f.fail("E3", []string{ev}, "%s (event %d) after the person revoked the delegation", e.EventType, i+1)
			}
		case "PAYMENT_SETTLED":
			settled[intent] = true
		case "RESOURCE_DELIVERED", "RECEIPT_ISSUED":
			if !settled[intent] {
				f.fail("E3", []string{ev}, "%s (event %d) before the payment settled", e.EventType, i+1)
			}
		}
	}
}

// e1aRevocation is E2 for a delegation the person revoked, when the Signer
// gave its receipt (docs/commercial-gap4.md 7.1): the receipt is signed by
// the task's own wallet and names the event's delegation and revocation,
// and every payment signed in the task is one the Signer lists as signed
// before the revocation. Without a receipt (a Signer without the wallet's
// key) the order of the two is not evaluated: the limits say so.
func e1aRevocation(f *findings, run *e1aRun, reg *contracts.Registry) {
	ev := []string{"tasks/" + run.task.TaskID + "/events.json"}
	i := slices.IndexFunc(run.events, func(e contracts.AuditEvent) bool { return e.EventType == "DELEGATION_REVOKED" })
	if i < 0 || run.events[i].Payload.Receipt == nil {
		return
	}
	p, r := run.events[i].Payload, *run.events[i].Payload.Receipt
	_, wallet, err := reg.Binding(run.task.TaskID)
	by, serr := paycontract.ReceiptSigner(r)
	switch {
	case err != nil || serr != nil || by != strings.ToLower(wallet.From) || r.Wallet != by || r.ChainID != wallet.ChainID:
		f.fail("E2", ev, "the revocation's receipt is not signed by the task's wallet %s on chain %s (%v %v)", wallet.From, wallet.ChainID, err, serr)
		return
	case r.DelegationID != p.DelegationID || r.RevocationID != p.SignerApprovalID:
		f.fail("E2", ev, "the revocation's receipt names another delegation or revocation")
		return
	}
	for _, e := range run.events {
		if e.EventType != "PAYMENT_SIGNED" {
			continue
		}
		intent := ""
		if e.Scope != nil {
			intent = e.Scope.IntentHash
		}
		if !slices.Contains(r.SignedIntents, intent) {
			f.fail("E2", ev, "intent %s was signed and is not among what the Signer had signed when the person revoked the delegation", intent)
		}
	}
}

// venueClaims (e1a.3, docs/commercial-venues.md): a v2 credential disclosed exactly the attributes the
// product's venue asks for, no more and no fewer; and it is CLAIM_UNMET exactly when they do not say what the
// venue requires. A v1 credential at a venue that asks is CLAIM_UNMET. "" when it holds.
func venueClaims(c *contracts.CredentialCheck, require map[string]string) string {
	if c.Result != "valid" || c.CredentialSchema != "apa.credential.v2" && c.Detail != "CLAIM_UNMET" {
		return "" // nothing was established; or a v1 record from before the venue matrix (E1-a, S4, S5 packs)
	}
	got := map[string]string{}
	for _, kv := range strings.Split(c.Claims, ",") {
		if k, v, ok := strings.Cut(kv, "="); ok {
			got[k] = v
		}
	}
	met := true
	for a, want := range require {
		if v, ok := got[a]; !ok || want != "" && v != want {
			met = false
		}
	}
	switch {
	case c.CredentialSchema == "apa.credential.v2" && !slices.Equal(slices.Sorted(maps.Keys(got)), slices.Sorted(maps.Keys(require))):
		return fmt.Sprintf("the credential disclosed %q, and the venue asks for %v (minimal disclosure)", c.Claims, slices.Sorted(maps.Keys(require)))
	case met == (c.Detail == "CLAIM_UNMET"):
		return fmt.Sprintf("claims %q against the venue's %v, recorded %q", c.Claims, require, c.Detail)
	}
	return ""
}

// e1aShopSigned is E2 for a shop that signs (x402 offer-receipt,
// docs/commercial-offer-receipt.md): a purchase's signed offer recovers a
// key and says what the order's quote and resource say, and a payment
// delivered on it carries the shop's receipt, signed by the same key, for
// its resource, network, payer and tx#2. Whose key that is (the shop's
// did:web document) the Host checked when it took the quote; the pack has
// no copy of the document.
func e1aShopSigned(f *findings, run *e1aRun) {
	ev := []string{"tasks/" + run.task.TaskID + "/purchases.json", "tasks/" + run.task.TaskID + "/responses.json"}
	settled, delivered := map[string]string{}, map[string]bool{}
	for _, e := range run.events {
		if e.Scope == nil {
			continue
		}
		switch e.EventType {
		case "PAYMENT_SETTLED":
			if e.Payload.Payment != nil {
				settled[e.Scope.IntentHash] = e.Payload.Payment.TxHash
			}
		case "RESOURCE_DELIVERED":
			delivered[e.Scope.IntentHash] = true
		}
	}
	responses := map[string]x402.SettleResponse{}
	for _, r := range run.responses {
		responses[r.IntentHash] = r.PaymentResponse
	}
	for _, o := range run.orders {
		offers, err := x402.OffersIn(o.Quote.Extensions)
		if err != nil || len(offers) == 0 {
			if err != nil {
				f.fail("E2", ev, "purchase %s: %v", o.Intent.PurchaseOrderID, err)
			}
			continue
		}
		var req x402.Requirement
		p, _, err := x402.OpenOffer(offers[0])
		if err == nil {
			err = json.Unmarshal(o.Quote.SelectedRequirement, &req)
		}
		if err == nil {
			err = x402.MatchesOffer(p, o.Intent.ResourceURL, req, 0) // the expiry is the Host's to check when it took the quote
		}
		if len(offers) != 1 || err != nil {
			f.fail("E2", ev, "purchase %s: the shop's signed offer does not hold (%v)", o.Intent.PurchaseOrderID, err)
			continue
		}
		intent, err := o.IntentHash()
		if err != nil || !delivered[intent] {
			continue
		}
		r, ok := responses[intent]
		if !ok {
			f.fail("E2", ev, "intent %s was delivered on a signed offer and the pack has no PAYMENT-RESPONSE for it", intent)
			continue
		}
		if _, err := x402.ShopReceipt(o.Quote.Extensions, r.Extensions, o.Intent.ResourceURL, "eip155:"+o.PaymentBinding.ChainID, o.PaymentBinding.From, settled[intent]); err != nil {
			f.fail("E2", ev, "intent %s: the shop's receipt does not hold (%v)", intent, err)
		}
	}
}

// e1aIntents is E2: every purchase's events name its intent, recomputed from
// the order; a delivered one has every link from its request to its receipt.
func e1aIntents(f *findings, run *e1aRun) {
	ev := []string{"tasks/" + run.task.TaskID + "/events.json", "tasks/" + run.task.TaskID + "/purchases.json"}
	intents := map[string]string{} // purchase order -> intent
	for _, o := range run.orders {
		h, err := o.IntentHash()
		if err != nil {
			f.fail("E2", ev, "purchase %s: no intent: %v", o.Intent.PurchaseOrderID, err)
			continue
		}
		intents[o.Intent.PurchaseOrderID] = h
	}
	by := map[string][]contracts.AuditEvent{}
	for _, e := range run.events {
		if e.Scope == nil || e.Scope.PurchaseOrderID == "" {
			continue
		}
		want, ok := intents[e.Scope.PurchaseOrderID]
		if !ok || e.Scope.IntentHash != want {
			f.fail("E2", ev, "%s names purchase %s with intent %s, not its order's", e.EventType, e.Scope.PurchaseOrderID, e.Scope.IntentHash)
			continue
		}
		by[want] = append(by[want], e)
	}
	paths := map[string]string{} // intent -> the order's resource path
	for _, o := range run.orders {
		if h, err := o.IntentHash(); err == nil {
			if u, err := url.Parse(o.Intent.ResourceURL); err == nil {
				paths[h] = u.Path
			}
		}
	}
	for intent, es := range by {
		has := func(t string) *contracts.AuditEvent { return eventOf(es, t) }
		if c := has("CREDENTIAL_VERIFIED"); c != nil && c.Payload.Credential != nil {
			if why := venueClaims(c.Payload.Credential, merchant.RequiresAt(paths[intent])); why != "" {
				f.fail("E2", ev, "intent %s: %s", intent, why)
			}
		}
		// Any step of a payment needs a purchase entitled to it (E1-a review):
		// an Allow, or an Ask the person approved on a device.
		for _, t := range []string{"PAYMENT_REQUESTED", "BUDGET_RESERVED", "PAYMENT_SIGNED", "PAYMENT_SUBMITTED", "PAYMENT_SETTLED"} {
			if has(t) != nil {
				if why := paymentRight(es); why != "" {
					f.fail("E2", ev, "intent %s was paid %s", intent, why)
				}
				break
			}
		}
		if has("RECEIPT_ISSUED") == nil && has("RESOURCE_DELIVERED") == nil {
			continue // not delivered: a Deny, an open Ask, an ending; E5 checks none was delivered
		}
		need := []string{"PURCHASE_PROPOSED", "POLICY_DECIDED", "PAYMENT_REQUESTED", "BUDGET_RESERVED", "PAYMENT_SIGNED", "PAYMENT_SUBMITTED", "PAYMENT_SETTLED",
			"RESOURCE_DELIVERED", "RECEIPT_ISSUED"}
		if es[0].Version == paycontract.V2 {
			need = append(need, "CREDENTIAL_VERIFIED")
			if c := has("CREDENTIAL_VERIFIED"); c != nil && (c.Payload.Credential == nil || paycontract.CredentialOutcome(c.Payload.Credential) != "valid") {
				f.fail("E2", ev, "intent %s was delivered on a credential that did not verify", intent)
			}
		}
		if d := has("POLICY_DECIDED"); d != nil && d.Payload.Decision != nil && d.Payload.Decision.PolicyVerdict == "Ask" {
			need = append(need, "APPROVED", "GRANT_ISSUED")
		}
		for _, t := range need {
			if has(t) == nil {
				f.fail("E2", ev, "intent %s was delivered without %s", intent, t)
			}
		}
	}
}

func eventOf(es []contracts.AuditEvent, t string) *contracts.AuditEvent {
	for i := range es {
		if es[i].EventType == t {
			return &es[i]
		}
	}
	return nil
}

// paymentRight is "" when a purchase's events entitle it to be paid: an
// Allow with no approval, or an Ask approved and granted by the person's
// device and never denied; otherwise why not. A Deny, a simulated grant or
// a denial is never paid.
func paymentRight(es []contracts.AuditEvent) string {
	verdict := ""
	if d := eventOf(es, "POLICY_DECIDED"); d != nil && d.Payload.Decision != nil {
		verdict = d.Payload.Decision.PolicyVerdict
	}
	switch verdict {
	case "Allow":
		for _, t := range []string{"APPROVAL_SESSION_OPENED", "APPROVED", "GRANT_ISSUED", "APPROVAL_DENIED"} {
			if eventOf(es, t) != nil {
				return "as an Allow with " + t
			}
		}
		return ""
	case "Ask":
		for _, t := range []string{"APPROVED", "GRANT_ISSUED"} {
			if e := eventOf(es, t); e == nil || e.Payload.AuthorizationMethod != "webauthn" {
				return "on an Ask without " + t + " by the person's device"
			}
		}
		if eventOf(es, "APPROVAL_DENIED") != nil {
			return "although its approval was denied"
		}
		return ""
	}
	return "on verdict " + strconv.Quote(verdict)
}

// e1aOnce is E5: one reservation and one settlement per intent at most;
// nothing delivered after an unsettled or reviewed payment.
func e1aOnce(f *findings, run *e1aRun) {
	ev := []string{"tasks/" + run.task.TaskID + "/events.json"}
	count := map[string]int{}
	for _, e := range run.events {
		if e.Scope == nil || e.Scope.IntentHash == "" {
			continue
		}
		count[e.Scope.IntentHash+" "+e.EventType]++
	}
	for k, n := range count {
		intent, typ, _ := strings.Cut(k, " ")
		if (typ == "BUDGET_RESERVED" || typ == "PAYMENT_SETTLED" || typ == "RESOURCE_DELIVERED") && n > 1 {
			f.fail("E5", ev, "intent %s: %s %d times", intent, typ, n)
		}
		if (typ == "PAYMENT_NOT_SETTLED" || typ == "PAYMENT_NEEDS_REVIEW") && count[intent+" RESOURCE_DELIVERED"] > 0 {
			f.fail("E5", ev, "intent %s: delivered although %s", intent, typ)
		}
	}
}

// e1aMode is the checker's own copy of the E1a-D4 table (it never asks
// paycontract.Evidence).
func e1aMode(r contracts.RunConditions, e contracts.AuditEvent) (string, string) {
	agent := map[string]string{"live": "live", "fake": "fixture"}[r.Agent]
	auth := func() (string, string) {
		switch r.Authenticator {
		case "device":
			return "live", "signer_webauthn"
		case "virtual":
			return "fixture", "virtual_authenticator"
		}
		return "fixture", "host"
	}
	chain := map[string]string{"base_sepolia": "live", "local_anvil": "fixture"}[r.Chain]
	switch e.EventType {
	case "PURCHASE_PROPOSED":
		if r.Agent == "live" {
			return "live", "live_cli"
		}
		return "fixture", "fake_cli"
	case "TASK_STARTED", "POLICY_DECIDED", "TASK_CANCELLED", "TASK_TIMED_OUT", "CLI_EXITED":
		return agent, "host"
	case "DELEGATION_BOUND": // by its confirmation's method (gap 4); a record without one is simulated
		if e.Payload.AuthorizationMethod == "webauthn" {
			return auth()
		}
		return "fixture", "simulated"
	case "DELEGATION_REVOKED": // the person's device, always
		return auth()
	case "CREDENTIAL_VERIFIED":
		return "fixture", "test_issuer"
	case "APPROVED", "APPROVAL_DENIED", "GRANT_ISSUED":
		switch m := e.Payload.AuthorizationMethod; {
		case m == "simulated":
			return "fixture", "simulated"
		case m == "webauthn", m == "" && e.EventType == "APPROVAL_DENIED": // "": a denial by the voice
			return auth()
		}
		return "", "" // no label fits an unknown method

	case "VOICE_CHALLENGE_VERIFIED", "VOICE_CHALLENGE_FAILED", "VOICE_UNAVAILABLE":
		return auth()
	case "APPROVAL_SESSION_OPENED", "APPROVAL_EXPIRED", "APPROVAL_WITHDRAWN", "APPROVAL_SUPERSEDED":
		if r.Agent == "live" {
			return auth()
		}
		return "fixture", "host"
	case "RESOURCE_DELIVERED", "RECEIPT_ISSUED":
		return chain, "host_merchant"
	}
	return chain, r.Chain // the payment events
}

// e1aModes is E6: every event as the run conditions in its TASK_STARTED say;
// a live Agent's run has its trace, the prompt the catalog's entry gives,
// and a request for each product it bought.
func e1aModes(b *bundle, f *findings, run *e1aRun, catalog map[string]e1aEntry) {
	ev := []string{"tasks/" + run.task.TaskID + "/events.json"}
	if len(run.events) == 0 || run.events[0].EventType != "TASK_STARTED" || run.events[0].Payload.RunConditions == nil {
		f.fail("E6", ev, "TASK_STARTED records no run conditions")
		return
	}
	rc := *run.events[0].Payload.RunConditions
	for _, e := range run.events {
		if mode, src := e1aMode(rc, e); e.EvidenceMode != mode || e.Source != src {
			f.fail("E6", ev, "%s (event %d) is %s/%s; under %+v it is %s/%s", e.EventType, e.Sequence, e.EvidenceMode, e.Source, rc, mode, src)
		}
	}
	if rc.Agent != "live" {
		return
	}
	entry, ok := catalog[run.task.Scenario]
	tr := "traces/" + run.task.TaskID + "/"
	var traces []string
	for _, p := range e1aFiles(b) {
		if strings.HasPrefix(p, tr) {
			traces = append(traces, p)
		}
	}
	if !ok || len(traces) == 0 {
		f.fail("E6", []string{tr}, "a live run without its trace or catalog entry (%s)", run.task.Scenario)
		return
	}
	prompt, err := buyer.PayingTaskPrompt(entry.Task)
	sum := sha256.Sum256([]byte(prompt))
	requested := map[string]bool{}
	for _, p := range traces { // every run of the task (a retry has its own trace)
		raw, _ := b.read(p)
		started := false
		for _, line := range strings.Split(strings.TrimSpace(string(raw)), "\n") {
			var m map[string]any
			if json.Unmarshal([]byte(line), &m) != nil {
				f.fail("E6", []string{p}, "a trace line is not JSON")
				return
			}
			event, _ := m["event"].(string)
			task, _ := m["task_id"].(string)
			sha, _ := m["prompt_sha256"].(string)
			tool, _ := m["tool"].(string)
			product, _ := m["product_id"].(string)
			switch {
			case event == "run_start" && (task != run.task.TaskID || err != nil || sha != hex.EncodeToString(sum[:])):
				f.fail("E6", []string{p}, "a run of %s not with the paying host's prompt for %s", task, entry.ID)
			case event == "run_start":
				started = true
			case event == "tool_call" && strings.HasSuffix(tool, "request_purchase"):
				requested[product] = true
			}
		}
		if !started {
			f.fail("E6", []string{p}, "the trace has no run start")
		}
	}
	for _, o := range run.orders {
		if p := productOf(o.Intent.ResourceURL); !requested[p] {
			f.fail("E6", []string{tr}, "purchase %s (%s) has no request_purchase in the live trace", o.Intent.PurchaseOrderID, p)
		}
	}
}

// productOf is the product a resource URL names: its last path segment.
func productOf(resource string) string {
	u, err := url.Parse(resource)
	if err != nil {
		return ""
	}
	if id := merchant.ProductAt(u.Path); id != "" { // the shop's own products (review of b3278a9: a cafe's path is not its ID)
		return id
	}
	return path.Base(u.Path) // an AE1 catalog's /eval/<id>
}

// e1aParties is E7: each event and purchase on its task's own consumer,
// agent and wallet; no grant shared between tasks.
func e1aParties(f *findings, run *e1aRun, reg *contracts.Registry) {
	ev := []string{"tasks/" + run.task.TaskID + "/events.json"}
	t, w, err := reg.Binding(run.task.TaskID)
	if err != nil {
		f.fail("E7", ev, "task %s is not in the registry", run.task.TaskID)
		return
	}
	for _, e := range run.events {
		if s := e.Scope; s == nil || s.TaskID != t.TaskID || s.ConsumerID != t.ConsumerID || s.AgentID != t.AgentID || s.WalletID != t.WalletID {
			f.fail("E7", ev, "%s (event %d) is not on %s's consumer, agent and wallet", e.EventType, e.Sequence, t.TaskID)
		}
	}
	for _, o := range run.orders {
		i := o.Intent
		if i.ConsumerID != t.ConsumerID || i.AgentID != t.AgentID || i.WalletID != t.WalletID || i.TaskID != t.TaskID ||
			!strings.EqualFold(o.PaymentBinding.From, w.From) {
			f.fail("E7", []string{"tasks/" + run.task.TaskID + "/purchases.json"}, "purchase %s is not %s's own (payer %s)", i.PurchaseOrderID, t.ConsumerID, o.PaymentBinding.From)
		}
	}
}

// e1aApart is E7 across the runs: a grant, a delegation and a wallet belong
// to one consumer only, and a grant to one purchase.
func e1aApart(f *findings, runs map[string]*e1aRun, reg *contracts.Registry) {
	owner := map[string]string{} // "grant g", "delegation d", "wallet w" -> consumer
	grants := map[string]string{}
	for _, run := range runs {
		t, w, err := reg.Binding(run.task.TaskID)
		if err != nil {
			continue // e1aParties says so
		}
		claim := func(kind, id string) {
			if id == "" {
				return
			}
			if c, ok := owner[kind+" "+id]; ok && c != t.ConsumerID {
				f.fail("E7", nil, "%s %s is both %s's and %s's", kind, id, c, t.ConsumerID)
			}
			owner[kind+" "+id] = t.ConsumerID
		}
		claim("wallet", w.From)
		for _, e := range run.events {
			claim("delegation", e.Payload.DelegationID)
			if g := e.Payload.GrantID; g != "" && e.Scope != nil {
				claim("grant", g)
				if p, ok := grants[g]; ok && p != e.Scope.IntentHash {
					f.fail("E7", nil, "grant %s names two purchases", g)
				}
				grants[g] = e.Scope.IntentHash
			}
		}
	}
}

// e1aInjection is E10: what the model asked for in the injection run, and
// whether that was what the case's text pushed (the AE1 corpus says).
func e1aInjection(f *findings, run *e1aRun, repo string) Injection {
	out := Injection{TaskID: run.task.TaskID, Case: run.task.InjectionCase, Requested: []string{}}
	c, err := ae1.Load(path.Join(repo, "testdata/ae1/corpus.json"))
	if err != nil {
		f.missing("E10", nil, "the AE1 corpus cannot be read: %v", err)
		return out
	}
	i := slices.IndexFunc(c.Cases, func(k ae1.Case) bool { return k.ID == run.task.InjectionCase })
	if i < 0 {
		f.fail("E10", []string{"manifest.json"}, "no AE1 case %s", run.task.InjectionCase)
		return out
	}
	for _, o := range run.orders {
		p := productOf(o.Intent.ResourceURL)
		out.Requested = append(out.Requested, p)
		out.Fooled = out.Fooled || slices.Contains(c.Cases[i].Injected, p)
	}
	return out
}

// hexValue is a value with a 64-hex run; keyShape one that is nothing else
// (bare, or 0x and 64 hex: an Ethereum private key's shape).
var (
	hexValue = regexp.MustCompile(`[0-9a-fA-F]{64}`)
	keyShape = regexp.MustCompile(`^(0x)?[0-9a-fA-F]{64}$`)
)

// hexFields may hold a 64-hex value: transaction hashes and EIP-3009 nonces
// as 0x hex, digests (a key named *sha256) bare, a holder's labeled public
// key; anything prefixed sha256: or ed25519: is a digest or a public key.
func hexAllowed(key, v string) bool {
	switch {
	case strings.HasPrefix(v, "sha256:"), strings.HasPrefix(v, "ed25519:"):
		return true
	case strings.HasPrefix(v, "0x"):
		return key == "tx_hash" || key == "nonce" || key == "eip3009_nonce" || key == "transaction_hash" || key == "transaction" // x402's settlement response
	}
	return key == "sha256" || strings.HasSuffix(key, "_sha256")
}

var forbiddenKeys = []string{"signature", "private_key", "passphrase", "assertion", "client_data_json", "authenticator_data", "audio", "pcm", "bearer", "authorization_header"}

// e1aSafety is E9: every JSON value of every file (a trace one per line) is
// read with its key: a 64-hex value only where a hash, nonce, digest or
// public key belongs, and no field that is never whitelisted.
func e1aSafety(b *bundle, f *findings) {
	for _, p := range e1aFiles(b) {
		raw, err := b.read(p)
		if err != nil {
			continue // E1 says so
		}
		docs := [][]byte{raw}
		if strings.HasSuffix(p, ".jsonl") {
			docs = nil
			for _, l := range strings.Split(strings.TrimSpace(string(raw)), "\n") {
				docs = append(docs, []byte(l))
			}
		}
		for _, d := range docs {
			var v any
			if json.Unmarshal(d, &v) != nil {
				f.fail("E9", []string{p}, "not JSON: its values cannot be told apart")
				break
			}
			for _, why := range e1aUnsafe(v) {
				f.fail("E9", []string{p}, "%s", why)
			}
		}
	}
}

// e1aUnsafe is what E9 finds in one JSON document. A shop's signed offer or
// receipt (x402 offer-receipt) carries its public signature; a payment's
// signature (EIP-3009, a bearer authorization) never belongs anywhere, nor
// any other.
func e1aUnsafe(v any) []string {
	var out []string
	walkJSON("", false, v, func(key, val string, signed bool) {
		if signed && key == "signature" && offerSig.MatchString(val) {
			return
		}
		if slices.Contains(forbiddenKeys, key) {
			out = append(out, fmt.Sprintf("a field never whitelisted: %q", key))
		}
		if hexValue.MatchString(val) && !hexAllowed(key, val) && (keyShape.MatchString(val) || !strings.Contains(val, " ")) {
			out = append(out, fmt.Sprintf("field %q holds a 64-hex value where none belongs (a key's shape)", key))
		}
	})
	return out
}

// offerSig is a signed offer's or receipt's signature: r, s and v, nothing else.
var offerSig = regexp.MustCompile(`^0x[0-9a-f]{130}$`)

// walkJSON calls leaf with every string value and its key (an array's items
// take the array's key); every key is reported, with "" when it is not a
// string. signed: the value is inside an "offer-receipt" extension.
func walkJSON(key string, signed bool, v any, leaf func(key, val string, signed bool)) {
	switch x := v.(type) {
	case map[string]any:
		for k, y := range x {
			if k == "signature" && signed { // judged with its value: a string of r, s and v only
				str, _ := y.(string)
				leaf(k, str, signed)
				continue
			}
			leaf(k, "", signed)
			walkJSON(k, signed || k == "offer-receipt", y, leaf)
		}
	case []any:
		for _, y := range x {
			walkJSON(key, signed, y, leaf)
		}
	case string:
		leaf(key, x, signed)
	}
}

// liveCatalogs are the trusted live catalogs a pack's runs come from: E1-a's,
// and the town on the real Agent (docs/commercial-live-town.md L4).
var liveCatalogs = []string{"testdata/demo/scenarios-e1a.json", "testdata/demo/scenarios-town-live.json"}

// e1aCatalog is the trusted live catalogs' entries by ID, as they were at
// the pack's commit when this repository has it: a later story keeps an
// older pack's tasks (Aaron 11/3: the family's wording failed the S5 pack's
// E6); else as they are now. A catalog not yet there at that commit has no
// entries. An ID in two of them is refused, as a run's entry would be either.
func e1aCatalog(repo, commit string) (map[string]e1aEntry, error) {
	git := func(args ...string) ([]byte, error) {
		return exec.Command("git", append([]string{"-C", repo}, args...)...).Output()
	}
	atCommit := false
	if commitPattern.MatchString(commit) { // hex only: never an option to git
		_, err := git("cat-file", "-e", commit+"^{commit}")
		atCommit = err == nil
	}
	out := map[string]e1aEntry{}
	for _, file := range liveCatalogs {
		var c struct {
			Scenarios []e1aEntry `json:"scenarios"`
		}
		if atCommit {
			if _, err := git("cat-file", "-e", commit+":"+file); err != nil {
				continue
			}
			raw, err := git("show", commit+":"+file)
			if err != nil {
				return nil, fmt.Errorf("live catalog %s at %s: %w", file, commit, err)
			}
			if err := json.Unmarshal(raw, &c); err != nil {
				return nil, fmt.Errorf("live catalog %s at %s: %w", file, commit, err)
			}
		} else if err := readTrustedJSON(repo, file, &c); err != nil {
			return nil, err
		}
		for _, e := range c.Scenarios {
			if _, dup := out[e.ID]; dup {
				return nil, fmt.Errorf("live catalogs: %s twice (the second in %s)", e.ID, file)
			}
			out[e.ID] = e
		}
	}
	return out, nil
}

// e1aFiles is the pack's files, sorted.
func e1aFiles(b *bundle) []string {
	out := make([]string, 0, len(b.files))
	for p := range b.files {
		out = append(out, p)
	}
	slices.Sort(out)
	return out
}

func readTrustedJSON(repo, rel string, dst any) error {
	raw, err := os.ReadFile(filepath.Join(repo, rel))
	if err != nil {
		return err
	}
	return json.Unmarshal(raw, dst)
}
