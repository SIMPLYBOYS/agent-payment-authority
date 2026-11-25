package verification

import (
	"encoding/hex"
	"encoding/json"
	"github.com/SIMPLYBOYS/agent-payment-authority/internal/merchant"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/SIMPLYBOYS/agent-payment-authority/internal/contracts"
	"github.com/SIMPLYBOYS/agent-payment-authority/internal/paycontract"
	"github.com/SIMPLYBOYS/agent-payment-authority/internal/signer/signertest"
	"github.com/SIMPLYBOYS/agent-payment-authority/internal/x402"
	"github.com/decred/dcrd/dcrec/secp256k1/v4"
	"github.com/decred/dcrd/dcrec/secp256k1/v4/ecdsa"
	"golang.org/x/crypto/sha3"
)

// The checker's copy of the E1a-D4 table is its own (e1aMode never asks
// paycontract.Evidence) and says what the Store's says, for every event
// type, method and run condition the Store writes under.
func TestE1aModeIsTheStoresTable(t *testing.T) {
	types := append(slices.Clone(paycontract.EventTypes), "CREDENTIAL_VERIFIED")
	for _, a := range []string{"live", "fake"} {
		for _, c := range []string{"base_sepolia", "local_anvil", "none"} {
			for _, au := range []string{"device", "virtual", "none"} {
				r := contracts.RunConditions{Agent: a, Chain: c, Authenticator: au}
				for _, typ := range types {
					for _, m := range []string{"", "simulated", "webauthn", "other"} {
						e := contracts.AuditEvent{EventType: typ, Payload: contracts.EventPayload{AuthorizationMethod: m}}
						mode, src, ok := paycontract.Evidence(&r, e)
						gm, gs := e1aMode(r, e)
						if ok && (gm != mode || gs != src) || !ok && gm != "" && (typ == "APPROVED" || typ == "GRANT_ISSUED") {
							t.Errorf("%s (%q) under %+v: checker %s/%s, Store %s/%s %v", typ, m, r, gm, gs, mode, src, ok)
						}
					}
				}
			}
		}
	}
}

// E7 across runs: a grant or a delegation is one consumer's.
func TestE1aApart(t *testing.T) {
	var cfg contracts.RegistryConfig
	if err := readTrustedJSON("../..", "testdata/contracts/registry.json", &cfg); err != nil {
		t.Fatal(err)
	}
	reg, err := contracts.NewRegistry(cfg)
	if err != nil {
		t.Fatal(err)
	}
	ev := func(task, consumer string, p contracts.EventPayload) contracts.AuditEvent {
		return contracts.AuditEvent{Scope: &contracts.EventScope{TaskID: task, ConsumerID: consumer, IntentHash: "sha256:" + task}, Payload: p}
	}
	runs := func(b contracts.EventPayload) map[string]*e1aRun {
		return map[string]*e1aRun{
			"task-a": {task: E1aTask{TaskID: "task-a"}, events: []contracts.AuditEvent{ev("task-a", "consumer-a", contracts.EventPayload{DelegationID: "dlg-a", GrantID: "g-a"})}},
			"task-b": {task: E1aTask{TaskID: "task-b"}, events: []contracts.AuditEvent{ev("task-b", "consumer-b", b)}},
		}
	}
	var f findings
	e1aApart(&f, runs(contracts.EventPayload{DelegationID: "dlg-b", GrantID: "g-b"}), reg)
	if len(f) != 0 {
		t.Fatalf("apart: %+v", f)
	}
	for _, b := range []contracts.EventPayload{{DelegationID: "dlg-a"}, {GrantID: "g-a"}} {
		var f findings
		e1aApart(&f, runs(b), reg)
		if f.verdicts(E1aClaims)["E7"] != Fail {
			t.Errorf("%+v shared with consumer-a: %+v", b, f)
		}
	}
}

// Gap 4: "the delegation confirmed by the person" is off not_evaluated only
// when every run's DELEGATION_BOUND says the person's device.
func TestDelegationsByDevice(t *testing.T) {
	run := func(method string) *e1aRun {
		return &e1aRun{events: []contracts.AuditEvent{{EventType: "TASK_STARTED"}, {EventType: "DELEGATION_BOUND", Payload: contracts.EventPayload{AuthorizationMethod: method}}}}
	}
	for name, c := range map[string]struct {
		runs map[string]*e1aRun
		want bool
	}{
		"all by device":       {map[string]*e1aRun{"a": run("webauthn"), "b": run("webauthn")}, true},
		"one by the host":     {map[string]*e1aRun{"a": run("webauthn"), "b": run("")}, false},
		"no delegation event": {map[string]*e1aRun{"a": {events: []contracts.AuditEvent{{EventType: "TASK_STARTED"}}}}, false},
		"no run read":         {map[string]*e1aRun{}, false},
	} {
		if got := delegationsByDevice(c.runs); got != c.want {
			t.Errorf("%s: %v", name, got)
		}
	}
}

// The checker knows the town's live entries too (docs/commercial-live-town.md L4), each with its task and v2.
func TestE1aCatalogHasTheTown(t *testing.T) {
	c, err := e1aCatalog("../..", "")
	if err != nil {
		t.Fatal(err)
	}
	for _, id := range []string{"e1a-ask", "e1a-deny", "e1a-inject", "town-data-a", "town-data-b", "town-cafe-a", "town-cafe-b"} {
		if e, ok := c[id]; !ok || e.Task == "" || e.Contract != "apa.pay.v2" {
			t.Errorf("%s: %+v %v", id, e, ok)
		}
	}
}

// A pack is checked against the catalogs as they were at its commit (Aaron 11/3: the family's wording failed
// the S5 pack's E6): a task changed since keeps its older text; a commit this repository lacks, or no commit,
// reads them as they are now; a catalog not yet there at the commit has no entries.
func TestE1aCatalogAtThePacksCommit(t *testing.T) {
	repo := t.TempDir()
	git := func(args ...string) string {
		cmd := exec.Command("git", append([]string{"-C", repo, "-c", "user.email=t@t", "-c", "user.name=t", "-c", "commit.gpgsign=false"}, args...)...)
		out, err := cmd.CombinedOutput()
		if err != nil {
			t.Fatalf("git %v: %v %s", args, err, out)
		}
		return strings.TrimSpace(string(out))
	}
	write := func(task string) {
		dir := filepath.Join(repo, "testdata/demo")
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatal(err)
		}
		raw := `{"scenarios":[{"id":"town-data-a","task":"` + task + `","contract":"apa.pay.v2"}]}`
		if err := os.WriteFile(filepath.Join(dir, "scenarios-town-live.json"), []byte(raw), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	git("init", "-q")
	write("買一份韓國市場簡報。")
	git("add", ".")
	git("commit", "-q", "-m", "s5")
	old := git("rev-parse", "HEAD")
	write("幫我買一份首爾旅遊攻略。")
	git("commit", "-q", "-am", "family")
	write("working tree")
	if err := os.WriteFile(filepath.Join(repo, "testdata/demo/scenarios-e1a.json"), []byte(`{"scenarios":[]}`), 0o644); err != nil { // the other live catalog: never committed here
		t.Fatal(err)
	}
	for _, tc := range []struct{ commit, want string }{
		{old, "買一份韓國市場簡報。"},
		{git("rev-parse", "HEAD"), "幫我買一份首爾旅遊攻略。"},
		{strings.Repeat("0", 40), "working tree"}, // not in this repository
		{"", "working tree"},
		{"--output=x", "working tree"}, // never an option to git
	} {
		c, err := e1aCatalog(repo, tc.commit)
		if err != nil {
			t.Fatalf("%q: %v", tc.commit, err)
		}
		if got := c["town-data-a"].Task; got != tc.want {
			t.Errorf("%q: task %q, want %q", tc.commit, got, tc.want)
		}
	}
}

// E6 and E10 match an order to the trace's request_purchase by product: every product the shop sells, the
// town's cafe ones too (/scenes/cafe/lunch-set is cafe-lunch-set), reads back as its own ID from its URL.
func TestProductOfEveryOffer(t *testing.T) {
	shop, err := merchant.New("http://127.0.0.1:1")
	if err != nil {
		t.Fatal(err)
	}
	for _, o := range shop.Offers() {
		if got := productOf(o.URL); got != o.ProductID {
			t.Errorf("%s: %s reads as %q", o.ProductID, o.URL, got)
		}
	}
	if got := productOf("http://127.0.0.1:1/eval/kr-market-brief?v=1"); got != "kr-market-brief" {
		t.Errorf("an AE1 item: %q", got)
	}
}

// E3 and the person's revocation (docs/commercial-gap4.md 7): nothing is
// proposed or requested after it, and it is one, of the task's delegation,
// by the person's device.
func TestE1aChainAfterARevocation(t *testing.T) {
	const task = "show-a-01"
	sp, _ := paycontract.SpecOf(paycontract.Version)
	chain := func(steps ...contracts.AuditEvent) *e1aRun {
		prev, _ := sp.GenesisHash(task)
		for i := range steps {
			e := &steps[i]
			e.Version, e.EventID, e.Sequence, e.PrevEventHash = paycontract.Version, "evt-"+strings.Repeat("0", 10)+string(rune('a'+i)), uint64(i+1), prev
			e.Scope = &contracts.EventScope{TaskID: task}
			prev, _ = sp.EventHash(*e)
		}
		return &e1aRun{task: E1aTask{TaskID: task}, events: steps}
	}
	ev := func(typ string, p contracts.EventPayload) contracts.AuditEvent {
		return contracts.AuditEvent{EventType: typ, Payload: p}
	}
	started, bound := ev("TASK_STARTED", contracts.EventPayload{}), ev("DELEGATION_BOUND", contracts.EventPayload{DelegationID: "dlg-1"})
	rev := ev("DELEGATION_REVOKED", contracts.EventPayload{DelegationID: "dlg-1", AuthorizationMethod: "webauthn", SignerApprovalID: "hap-1"})
	for name, c := range map[string]struct {
		run  *e1aRun
		pass bool
	}{
		"revoked, then cancelled": {chain(started, bound, rev, ev("TASK_CANCELLED", contracts.EventPayload{Code: "HOST_CANCELLED"})), true},
		"a proposal after it":     {chain(started, bound, rev, ev("PURCHASE_PROPOSED", contracts.EventPayload{})), false},
		"a payment after it":      {chain(started, bound, rev, ev("PAYMENT_REQUESTED", contracts.EventPayload{})), false},
		"another delegation's":    {chain(started, bound, ev("DELEGATION_REVOKED", contracts.EventPayload{DelegationID: "dlg-2", AuthorizationMethod: "webauthn", SignerApprovalID: "hap-1"})), false},
		"not by the device":       {chain(started, bound, ev("DELEGATION_REVOKED", contracts.EventPayload{DelegationID: "dlg-1"})), false},
	} {
		var f findings
		e1aChain(&f, c.run)
		if (f.verdicts(E1aClaims)["E3"] != Fail) != c.pass {
			t.Errorf("%s: %+v", name, f)
		}
	}
}

// E2 with the Signer's receipt (docs/commercial-gap4.md 7.1): signed by the
// task's wallet, and every signed payment among what it lists.
func TestE1aRevocationReceipt(t *testing.T) {
	key, _ := secp256k1.GeneratePrivateKey()
	addr := "0x" + hex.EncodeToString(sha3Keccak(key.PubKey().SerializeUncompressed()[1:])[12:])
	other, _ := secp256k1.GeneratePrivateKey()
	raw, err := os.ReadFile("../../testdata/contracts/registry.json")
	if err != nil {
		t.Fatal(err)
	}
	var cfg contracts.RegistryConfig
	if err := json.Unmarshal(raw, &cfg); err != nil {
		t.Fatal(err)
	}
	for i := range cfg.Wallets {
		if cfg.Wallets[i].WalletID == "wallet-a" {
			cfg.Wallets[i].From = addr // wallet A is this test's key
		}
	}
	reg, err := contracts.NewRegistry(cfg)
	if err != nil {
		t.Fatal(err)
	}
	in := func(c string) string { return "sha256:" + strings.Repeat(c, 64) }
	sign := func(k *secp256k1.PrivateKey, intents ...string) *contracts.RevocationReceipt {
		r := contracts.RevocationReceipt{DelegationID: "dlg-1", RevocationID: "hap-1", RevokedAt: 1, ChainID: "84532", Wallet: addr, SignedIntents: intents}
		d, err := paycontract.RevocationDigest(r)
		if err != nil {
			t.Fatal(err)
		}
		c := ecdsa.SignCompact(k, d, false)
		r.Signature = "0x" + hex.EncodeToString(append(append([]byte{}, c[1:65]...), c[0]))
		return &r
	}
	run := func(receipt *contracts.RevocationReceipt, signed ...string) *e1aRun {
		es := []contracts.AuditEvent{{EventType: "TASK_STARTED"}, {EventType: "DELEGATION_BOUND", Payload: contracts.EventPayload{DelegationID: "dlg-1"}}}
		for _, s := range signed {
			es = append(es, contracts.AuditEvent{EventType: "PAYMENT_SIGNED", Scope: &contracts.EventScope{IntentHash: s}})
		}
		es = append(es, contracts.AuditEvent{EventType: "DELEGATION_REVOKED", Payload: contracts.EventPayload{DelegationID: "dlg-1", AuthorizationMethod: "webauthn",
			SignerApprovalID: "hap-1", Receipt: receipt}})
		return &e1aRun{task: E1aTask{TaskID: "task-a"}, events: es}
	}
	for name, c := range map[string]struct {
		run  *e1aRun
		pass bool
	}{
		"all it signed listed":        {run(sign(key, in("a"), in("b")), in("a"), in("b")), true},
		"no receipt: not evaluated":   {run(nil, in("a")), true},
		"one signed it does not list": {run(sign(key, in("a")), in("a"), in("b")), false},
		"signed by another key":       {run(sign(other, in("a")), in("a")), false},
		"an intent added after signing": {func() *e1aRun {
			r := sign(key, in("a"))
			r.SignedIntents = []string{in("a"), in("b")}
			return run(r, in("a"), in("b"))
		}(), false},
	} {
		var f findings
		e1aRevocation(&f, c.run, reg)
		if (f.verdicts(E1aClaims)["E2"] != Fail) != c.pass {
			t.Errorf("%s: %+v", name, f)
		}
	}
}

func sha3Keccak(b []byte) []byte {
	h := sha3.NewLegacyKeccak256()
	h.Write(b)
	return h.Sum(nil)
}

// E2 for a shop that signs (docs/commercial-offer-receipt.md): the offer
// holds for the order, and a delivery carries the receipt of the offer's key
// for this payer and tx#2; a shop that signs nothing is not asked.
func TestE1aShopSigned(t *testing.T) {
	key, _ := secp256k1.GeneratePrivateKey()
	other, _ := secp256k1.GeneratePrivateKey()
	tx2 := "0x" + strings.Repeat("2", 64)
	build := func(signOffer *secp256k1.PrivateKey, amount string, receipt *secp256k1.PrivateKey, receiptTx string, delivered bool) *e1aRun {
		po := signertest.Order{Consumer: "consumer-a", Agent: "agent-a", Wallet: "wallet-a", Task: "task-a", From: "0x1111111111111111111111111111111111111111",
			PayTo: signertest.Merchant, Amount: "500000", N: 1, Now: 1790726400, Window: 300}.PO()
		var req x402.Requirement
		json.Unmarshal(po.Quote.SelectedRequirement, &req)
		if signOffer != nil {
			s, _ := x402.SignOffer(signOffer, x402.OfferPayload{Version: 1, ResourceURL: po.Intent.ResourceURL, Scheme: req.Scheme, Network: req.Network, Asset: req.Asset,
				PayTo: req.PayTo, Amount: amount})
			po.Quote.Extensions, _ = x402.OfferExtensions(s)
			po.Intent.PaymentRequirementHash, _ = paycontract.RequirementHash(po.Quote) // the order binds its quote, extensions included
		}
		intent, err := po.IntentHash()
		if err != nil {
			t.Fatal(err)
		}
		ev := func(typ string, p *contracts.PaymentRecord) contracts.AuditEvent {
			return contracts.AuditEvent{EventType: typ, Scope: &contracts.EventScope{PurchaseOrderID: po.Intent.PurchaseOrderID, IntentHash: intent},
				Payload: contracts.EventPayload{Payment: p}}
		}
		run := &e1aRun{task: E1aTask{TaskID: "task-a"}, orders: []paycontract.PurchaseOrder{po}, events: []contracts.AuditEvent{ev("PAYMENT_SETTLED", &contracts.PaymentRecord{TxHash: tx2})}}
		if delivered {
			run.events = append(run.events, ev("RESOURCE_DELIVERED", &contracts.PaymentRecord{ContentSHA256: strings.Repeat("c", 64)}))
		}
		resp := x402.SettleResponse{Success: true, Transaction: tx2, Network: req.Network, Payer: po.PaymentBinding.From}
		if receipt != nil {
			s, _ := x402.SignReceipt(receipt, x402.ReceiptPayload{Version: 1, Network: req.Network, ResourceURL: po.Intent.ResourceURL, Payer: po.PaymentBinding.From,
				IssuedAt: 1790726450, Transaction: receiptTx})
			resp.Extensions, _ = x402.ReceiptExtensions(s)
		}
		run.responses = []e1aResponse{{IntentHash: intent, PaymentResponse: resp}}
		return run
	}
	for name, c := range map[string]struct {
		run  *e1aRun
		pass bool
	}{
		"signed offer, its key's receipt":        {build(key, "500000", key, tx2, true), true},
		"an unsigned shop":                       {build(nil, "", nil, "", true), true},
		"settled, not delivered: no receipt yet": {build(key, "500000", nil, "", false), true},
		"an offer for another amount":            {build(key, "1", key, tx2, true), false},
		"delivered without a receipt":            {build(key, "500000", nil, "", true), false},
		"a receipt by another key":               {build(key, "500000", other, tx2, true), false},
		"a receipt for another transaction":      {build(key, "500000", key, "0x"+strings.Repeat("3", 64), true), false},
	} {
		var f findings
		e1aShopSigned(&f, c.run)
		if (f.verdicts(E1aClaims)["E2"] != Fail) != c.pass {
			t.Errorf("%s: %+v", name, f)
		}
	}
}

// E9 lets a shop's signed offer or receipt keep its signature, and nothing
// else: a payment's signature, one outside offer-receipt, or one that is
// not r, s and v is still found.
func TestE1aUnsafeSignatures(t *testing.T) {
	sig := "0x" + strings.Repeat("ab", 65)
	for name, c := range map[string]struct {
		doc  string
		safe bool
	}{
		"an offer's":             {`{"extensions":{"offer-receipt":{"info":{"offers":[{"format":"eip712","payload":{"payTo":"0x33"},"signature":"` + sig + `"}]}}}}`, true},
		"a receipt's":            {`{"payment_response":{"transaction":"0x` + strings.Repeat("2", 64) + `","extensions":{"offer-receipt":{"info":{"receipt":{"signature":"` + sig + `"}}}}}}`, true},
		"a payment's":            {`{"authorization":{"signature":"` + sig + `"}}`, false},
		"beside offer-receipt":   {`{"extensions":{"offer-receipt":{},"other":{"signature":"` + sig + `"}}}`, false},
		"not r, s and v":         {`{"offer-receipt":{"signature":"0x` + strings.Repeat("1", 64) + `"}}`, false},
		"an object as signature": {`{"offer-receipt":{"signature":{"r":"1"}}}`, false},
		"a private key's shape":  {`{"offer-receipt":{"payload":{"amount":"0x` + strings.Repeat("1", 64) + `"}}}`, false},
	} {
		var v any
		if err := json.Unmarshal([]byte(c.doc), &v); err != nil {
			t.Fatal(name, err)
		}
		if got := e1aUnsafe(v); (len(got) == 0) != c.safe {
			t.Errorf("%s: %v", name, got)
		}
	}
}
