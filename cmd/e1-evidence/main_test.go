package main

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/SIMPLYBOYS/agent-payment-authority/internal/buyer"
	"github.com/SIMPLYBOYS/agent-payment-authority/internal/contracts"
	"github.com/SIMPLYBOYS/agent-payment-authority/internal/merchant"
	"github.com/SIMPLYBOYS/agent-payment-authority/internal/paycontract"
	"github.com/SIMPLYBOYS/agent-payment-authority/internal/policy"
	"github.com/SIMPLYBOYS/agent-payment-authority/internal/signer/signertest"
	"github.com/SIMPLYBOYS/agent-payment-authority/internal/store"
	"github.com/SIMPLYBOYS/agent-payment-authority/internal/verification"
)

const root = "../.."

func fixture(t *testing.T, rel string, v any) {
	t.Helper()
	if err := readJSON(filepath.Join(root, rel), v); err != nil {
		t.Fatal(err)
	}
}

// paidStore is a Store whose task-a (apa.pay.v1, fake Agent, local anvil)
// has an Allow paid and delivered, its steps as the payment worker records
// them (no chain here: E4 and E8 are not this test's).
func paidStore(t *testing.T, agent string) string {
	t.Helper()
	var reg contracts.RegistryConfig
	fixture(t, "testdata/contracts/registry.json", &reg)
	var p policy.Profile
	fixture(t, "testdata/policy/m1-demo-profile.json", &p)
	p.AllowMax = "500000"
	shop, err := merchant.New("http://127.0.0.1:8402")
	if err != nil {
		t.Fatal(err)
	}
	offer := shop.Offers()[0] // kr-market, 0.50
	dir := filepath.Join(t.TempDir(), "db")
	os.Mkdir(dir, 0o700)
	path := filepath.Join(dir, "host.db")
	now := uint64(time.Now().Unix())
	s, err := store.Open(store.Config{Path: path, Registry: reg, Offers: store.OfferBook{{Method: offer.Method, URL: offer.URL}: {offer.Quote}},
		Clock: func() uint64 { return now }})
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	d, _, err := s.ConfirmDelegation(store.DelegationRequest{RequestID: "dlg-req-task-a-000000000", ConsumerID: "consumer-a", Scenario: "pay", Purpose: "a brief",
		AssetID: p.AssetID, Merchants: p.Merchants, TaskBudget: "5000000", AllowMax: "500000", ApprovalDeadlineSeconds: 30, ValidSeconds: 600,
		ConfirmedBy: "simulated:consumer-a:host-test"})
	if err != nil {
		t.Fatal(err)
	}
	if err := s.StartTask(store.TaskSetup{TaskID: "task-a", Profile: &p, Credential: &policy.Credential{ConsumerID: "consumer-a", ValidUntil: now + 86400, EvidenceMode: "fixture"},
		ContractVersion: paycontract.Version, DelegationID: d.DelegationID, Run: &paycontract.RunConditions{Agent: agent, Chain: "local_anvil", Authenticator: "none"}}); err != nil {
		t.Fatal(err)
	}
	po := signertest.Order{Consumer: "consumer-a", Agent: "agent-a", Wallet: "wallet-a", Task: "task-a", From: reg.Wallets[0].From, PayTo: reg.MerchantPayTo,
		Amount: "500000", N: 1, Now: now, Window: merchant.SettledTimeout}.PO()
	sp, _ := paycontract.SpecOf(paycontract.Version)
	po.Quote, po.Intent.ResourceURL, po.PaymentBinding.AssetContract = offer.Quote, offer.URL, reg.Assets[0].AssetContract
	po.Intent.ResourceHash, _ = sp.ResourceHash("GET", offer.URL)
	po.Intent.PaymentRequirementHash, _ = sp.RequirementHash(offer.Quote)
	order, _ := json.Marshal(po)
	r, err := s.Submit(contracts.Session{ConsumerID: "consumer-a", AgentID: "agent-a", TaskID: "task-a"}, order)
	if err != nil || r.Purchase.Decision.PolicyVerdict != "Allow" {
		t.Fatalf("submit: %+v %v", r.Purchase.Decision, err)
	}
	intent := r.Purchase.IntentHash
	if _, _, err := s.StartPayment(store.PaymentStart{ConsumerID: "consumer-a", TaskID: "task-a", PurchaseOrderID: po.Intent.PurchaseOrderID, DelegationRef: "dlg-a@1"}); err != nil {
		t.Fatal(err)
	}
	tx1, tx2 := "0x"+strings.Repeat("1", 64), "0x"+strings.Repeat("2", 64)
	sum := sha256.Sum256([]byte("the brief"))
	for _, step := range []func() (store.Payment, error){
		func() (store.Payment, error) {
			return s.PaymentReserved(intent, store.Reservation{Evidence: json.RawMessage(`{"tx_hash":"` + tx1 + `"}`), TxHash: tx1, Block: 7})
		},
		func() (store.Payment, error) {
			return s.PaymentSigned(intent, json.RawMessage(`{"from":"`+reg.Wallets[0].From+`","signature":"0xab"}`))
		},
		func() (store.Payment, error) { return s.PaymentSubmitted(intent) },
		func() (store.Payment, error) {
			return s.PaymentSettled(intent, tx2, 9, json.RawMessage(`{"success":true}`))
		},
		func() (store.Payment, error) { return s.PaymentDelivered(intent, hex.EncodeToString(sum[:]), 9) },
	} {
		if _, err := step(); err != nil {
			t.Fatal(err)
		}
	}
	return path
}

func pack(t *testing.T, db string, tasks ...string) string { return packTraced(t, db, "", tasks...) }

func packTraced(t *testing.T, db, traces string, tasks ...string) string {
	t.Helper()
	start := filepath.Join(t.TempDir(), "start.json")
	os.WriteFile(start, []byte(`{"chain":"local_anvil","chain_id":31337,"from_block":1,"wallets":["0x1111111111111111111111111111111111111111"]}`), 0o600)
	out := filepath.Join(t.TempDir(), "pack")
	args := []string{"pack", "--db", db, "--start", start, "--out", out, "--traces", traces}
	for _, x := range tasks {
		args = append(args, "--task", x)
	}
	if err := run(args, io.Discard); err != nil {
		t.Fatal(err)
	}
	return out
}

func check(t *testing.T, dir string) *verification.E1aReport {
	t.Helper()
	r, err := verification.VerifyE1a(dir, root, nil)
	if err != nil {
		t.Fatal(err)
	}
	return r
}

// repack rewrites rel through change and re-indexes the pack (the
// manifest's digest and run-end), so E1 still holds: what is left to catch
// the change is the claim about its meaning.
func repack(t *testing.T, dir, rel string, change func([]byte) []byte) {
	t.Helper()
	p := filepath.Join(dir, rel)
	raw, _ := os.ReadFile(p)
	raw = change(raw)
	os.WriteFile(p, raw, 0o600)
	var m verification.E1aManifest
	readJSON(filepath.Join(dir, "manifest.json"), &m)
	for i := range m.Files {
		if m.Files[i].Path == rel {
			s := sha256.Sum256(raw)
			m.Files[i].SHA256 = hex.EncodeToString(s[:])
		}
	}
	mb, _ := json.MarshalIndent(m, "", "  ")
	os.WriteFile(filepath.Join(dir, "manifest.json"), mb, 0o600)
	s := sha256.Sum256(mb)
	eb, _ := json.Marshal(verification.E1aEnd{RunID: m.RunID, State: "complete", ManifestSHA256: hex.EncodeToString(s[:])})
	os.WriteFile(filepath.Join(dir, "run-end.json"), eb, 0o600)
}

// events changes task-a's events and links them again (sequence and
// previous hash), so E3 still holds.
func events(t *testing.T, dir string, change func([]contracts.AuditEvent) []contracts.AuditEvent) {
	repack(t, dir, "tasks/task-a/events.json", func(raw []byte) []byte {
		var es []contracts.AuditEvent
		json.Unmarshal(raw, &es)
		es = change(es)
		sp, _ := paycontract.SpecOf(paycontract.Version)
		for i := range es {
			es[i].Sequence = uint64(i + 1)
			if i == 0 {
				es[i].PrevEventHash, _ = sp.GenesisHash("task-a")
			} else {
				es[i].PrevEventHash, _ = sp.EventHash(es[i-1])
			}
		}
		out, _ := json.Marshal(es)
		return out
	})
}

func index(es []contracts.AuditEvent, typ string) int {
	for i, e := range es {
		if e.EventType == typ {
			return i
		}
	}
	return -1
}

// E1-a (docs/commercial-e1a.md 4.5): a whole pack passes every claim the
// pack alone can decide (E4 and E8 need the chain); each single change,
// with every digest made again, is caught by the claim about its meaning.
func TestPackAndCheck(t *testing.T) {
	db := paidStore(t, "fake")
	good := check(t, pack(t, db, "task-a=e1a-ask"))
	for _, c := range verification.E1aClaims {
		want := verification.Pass
		if c == "E4" || c == "E8" {
			want = verification.Inconclusive
		}
		if good.Claims[c] != want {
			t.Errorf("%s: %s, want %s (%+v)", c, good.Claims[c], want, good.Findings)
		}
	}
	if good.Verdict != verification.Inconclusive || good.ExitCode() != 2 {
		t.Fatalf("verdict %s", good.Verdict)
	}

	for name, c := range map[string]struct {
		claim  string
		change func(dir string)
	}{
		"a file changed after it was indexed": {"E1", func(dir string) {
			raw, _ := os.ReadFile(filepath.Join(dir, "registry.json"))
			os.WriteFile(filepath.Join(dir, "registry.json"), append(raw, ' '), 0o600) // the same meaning, another file
		}},
		"an event not linked to the one before": {"E3", func(dir string) {
			repack(t, dir, "tasks/task-a/events.json", func(raw []byte) []byte {
				var es []contracts.AuditEvent
				json.Unmarshal(raw, &es)
				es[3].PrevEventHash = "sha256:" + strings.Repeat("0", 64)
				out, _ := json.Marshal(es)
				return out
			})
		}},
		"delivered without the signature": {"E2", func(dir string) {
			events(t, dir, func(es []contracts.AuditEvent) []contracts.AuditEvent {
				i := index(es, "PAYMENT_SIGNED")
				return append(es[:i], es[i+1:]...)
			})
		}},
		"settled twice": {"E5", func(dir string) {
			events(t, dir, func(es []contracts.AuditEvent) []contracts.AuditEvent {
				i := index(es, "PAYMENT_SETTLED")
				return append(es[:i+1], es[i:]...)
			})
		}},
		"a fake request labeled live": {"E6", func(dir string) {
			events(t, dir, func(es []contracts.AuditEvent) []contracts.AuditEvent {
				i := index(es, "PURCHASE_PROPOSED")
				es[i].EvidenceMode, es[i].Source = "live", "live_cli"
				return es
			})
		}},
		"an event on another wallet": {"E7", func(dir string) {
			events(t, dir, func(es []contracts.AuditEvent) []contracts.AuditEvent {
				es[len(es)-1].Scope.WalletID = "wallet-b"
				return es
			})
		}},
		"a key's shape": {"E9", func(dir string) {
			repack(t, dir, "registry.json", func(raw []byte) []byte {
				return []byte(strings.Replace(string(raw), "{", `{"note":"`+strings.Repeat("ab", 32)+`",`, 1))
			})
		}},
	} {
		dir := pack(t, db, "task-a=e1a-ask")
		c.change(dir)
		r := check(t, dir)
		if r.Claims[c.claim] != verification.Fail {
			t.Errorf("%s: %s is %s (%+v)", name, c.claim, r.Claims[c.claim], r.Findings)
		}
		for _, other := range verification.E1aClaims {
			if other != c.claim && r.Claims[other] == verification.Fail {
				t.Errorf("%s: %s failed too (%+v)", name, other, r.Findings)
			}
		}
	}

	// E10: the injection run's record: what was asked for, and whether the case pushed it.
	r := check(t, pack(t, db, "task-a=e1a-inject=i-summary-premium"))
	if len(r.Injections) != 1 || r.Injections[0].Fooled || r.Injections[0].Requested[0] != "kr-market" || r.Claims["E10"] != verification.Pass {
		t.Fatalf("injection %+v %s", r.Injections, r.Claims["E10"])
	}
	dir := pack(t, db, "task-a=e1a-inject=i-summary-premium")
	repack(t, dir, "tasks/task-a/purchases.json", func(raw []byte) []byte {
		return []byte(strings.ReplaceAll(string(raw), "/resources/kr-market", "/eval/kr-premium-bundle"))
	})
	if r := check(t, dir); len(r.Injections) != 1 || !r.Injections[0].Fooled {
		t.Fatalf("a pushed product asked for: %+v", r.Injections)
	}
}

// E6 for a live run: its trace has the run with the paying host's prompt for
// the catalog's entry and a request for what it bought; without the trace,
// or with another prompt, the live label is not supported.
func TestLiveTrace(t *testing.T) {
	db := paidStore(t, "live")
	prompt, _ := buyer.PayingTaskPrompt("買一份韓國市場簡報第 1 期。") // scenarios-e1a.json's e1a-ask
	for name, c := range map[string]struct {
		prompt string
		want   verification.Verdict
	}{"its trace": {prompt, verification.Pass}, "another prompt": {buyer.Prompt, verification.Fail}, "no trace": {"", verification.Fail}} {
		traces := t.TempDir()
		if c.prompt != "" {
			sum := sha256.Sum256([]byte(c.prompt))
			os.WriteFile(filepath.Join(traces, "task-a-1.jsonl"), []byte(`{"event":"run_start","task_id":"task-a","prompt_sha256":"`+hex.EncodeToString(sum[:])+`"}
{"event":"tool_call","tool":"request_purchase","product_id":"kr-market","outcome":"recorded"}
`), 0o600)
		}
		r := check(t, packTraced(t, db, traces, "task-a=e1a-ask"))
		if r.Claims["E6"] != c.want {
			t.Errorf("%s: E6 %s, want %s (%+v)", name, r.Claims["E6"], c.want, r.Findings)
		}
	}
}
