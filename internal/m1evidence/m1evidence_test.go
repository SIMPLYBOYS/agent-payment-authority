package m1evidence

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/SIMPLYBOYS/agent-payment-authority/internal/contracts"
)

func load(t *testing.T, name string, dst any) []byte {
	t.Helper()
	raw, err := os.ReadFile("../../testdata/contracts/" + name)
	if err != nil {
		t.Fatal(err)
	}
	if dst != nil {
		if err := contracts.Decode(raw, dst); err != nil {
			t.Fatal(err)
		}
	}
	return raw
}

func caseOf(t *testing.T, id string) contracts.AcceptanceCase {
	var m contracts.CaseManifest
	load(t, "cases.json", &m)
	i := slices.IndexFunc(m.Cases, func(c contracts.AcceptanceCase) bool { return c.CaseID == id })
	if i < 0 {
		t.Fatalf("no case %s", id)
	}
	return m.Cases[i]
}

func trusted(t *testing.T) Trusted {
	tr, err := LoadTrusted("../..")
	if err != nil {
		t.Fatal(err)
	}
	return tr
}

// asB is the trusted purchase made B's, as the contracts test makes it.
func asB(p contracts.PurchaseOrder) contracts.PurchaseOrder {
	p.Intent.ConsumerID, p.Intent.AgentID, p.Intent.WalletID, p.Intent.TaskID = "consumer-b", "agent-b", "wallet-b", "task-b"
	p.Intent.PurchaseOrderID, p.Intent.From = "po-b-1", "0x2222222222222222222222222222222222222222"
	return p
}

// record builds a contracts case's record the way TestContractCasesFromManifest
// (internal/contracts) does: the trusted fixtures with the case's change.
func record(t *testing.T, c contracts.AcceptanceCase) Contract {
	var p contracts.PurchaseOrder
	load(t, "purchase-input.json", &p)
	x := Contract{CaseID: c.CaseID, Layer: "contracts_test", Operation: c.Operation, Parameters: c.Parameters}
	marshal := func(v contracts.PurchaseOrder) json.RawMessage { b, _ := json.Marshal(v); return b }
	switch params := c.Parameters; c.Operation {
	case "compare-retry":
		in := p
		switch params["mutation"] {
		case "nonce":
			in.Intent.EIP3009Nonce = "0x" + strings.Repeat("b", 64)
		case "request_id":
			in.RequestID = "request-2"
		case "other_consumer":
			in = asB(p)
		}
		x.Purchase, x.Incoming = marshal(p), marshal(in)
	case "binding":
		x.Registry = load(t, "registry.json", nil)
		x.Session = &contracts.Session{ConsumerID: "consumer-a", AgentID: "agent-a", TaskID: "task-a"}
		if params["session"] == "b" {
			x.Session = &contracts.Session{ConsumerID: "consumer-b", AgentID: "agent-b", TaskID: "task-b"}
		}
		if params["purchase"] == "b" {
			p = asB(p)
		}
		b := asB(p)
		switch params["mutation"] {
		case "wallet_b":
			p.Intent.WalletID = b.Intent.WalletID
		case "from_b":
			p.Intent.From = b.Intent.From
		case "consumer_b":
			p.Intent.ConsumerID = b.Intent.ConsumerID
		case "agent_b":
			p.Intent.AgentID = b.Intent.AgentID
		}
		x.Purchase = marshal(p)
	case "validate-decision":
		h, _ := p.Intent.Hash()
		x.Decision = &contracts.PolicyDecision{Version: contracts.Version, IntentHash: h, PolicyVerdict: "Ask", ApprovalStatus: "pending",
			PaymentStatus: params["payment_status"], Reason: "HUMAN_APPROVAL_REQUIRED"}
	case "validate-purchase":
		var req map[string]any
		json.Unmarshal(p.Quote.SelectedRequirement, &req)
		switch params["mutation"] {
		case "transfer_method_permit2":
			req["extra"].(map[string]any)["assetTransferMethod"] = "permit2"
		case "transfer_method_erc7710":
			req["extra"].(map[string]any)["assetTransferMethod"] = "erc7710"
		case "case_variant_pay_to":
			req["payto"] = "0x5555555555555555555555555555555555555555"
		}
		p.Quote.SelectedRequirement, _ = json.Marshal(req)
		if h, err := p.Quote.Hash(); err == nil {
			p.Intent.PaymentRequirementHash = h
		}
		x.Purchase = marshal(p)
	default:
		t.Fatalf("no builder for %s", c.Operation)
	}
	o, err := RunContract(x)
	if err != nil {
		t.Fatal(err)
	}
	x.Observed = o
	return x
}

func contractCases(t *testing.T) []contracts.AcceptanceCase {
	var m contracts.CaseManifest
	load(t, "cases.json", &m)
	return slices.DeleteFunc(m.Cases, func(c contracts.AcceptanceCase) bool { return c.Layer != "contracts" })
}

// Every contracts case's record shows its own case, and no other: a record
// relabelled as another case (its case ID, parameters and observed result
// rewritten to that case's) is refused because its inputs are not that
// case's, even where the contract code gives the same result.
func TestCheckContractBindsInputsToTheCase(t *testing.T) {
	tr := trusted(t)
	cs := contractCases(t)
	if len(cs) != 15 {
		t.Fatalf("%d contracts cases", len(cs))
	}
	for _, c := range cs {
		if _, problems := CheckContract(c, record(t, c), tr); len(problems) > 0 {
			t.Errorf("%s: its own record refused: %v", c.CaseID, problems)
		}
	}
	for _, from := range cs {
		for _, as := range cs {
			if from.CaseID == as.CaseID {
				continue
			}
			x := record(t, from)
			x.CaseID, x.Operation, x.Parameters = as.CaseID, as.Operation, as.Parameters
			x.Observed = ContractObserved{Result: as.Expected.Result}
			if x.Observed.Result == "reject" {
				x.Observed.Code = as.Expected.Reason
			}
			if _, problems := CheckContract(as, x, tr); len(problems) == 0 {
				t.Errorf("%s's inputs accepted as %s", from.CaseID, as.CaseID)
			}
		}
	}
}

// The two records the review built from the delivered bundle: A's own
// inputs labelled consumer-b, and a nonce conflict labelled as a request ID
// conflict. Each gives the labelled case's result and code.
func TestCheckContractReviewCounterexamples(t *testing.T) {
	tr := trusted(t)
	for _, pair := range [][2]string{{"consumer-a", "consumer-b"}, {"retry-conflict", "retry-new-request-same-order"}} {
		x, as := record(t, caseOf(t, pair[0])), caseOf(t, pair[1])
		x.CaseID, x.Parameters = as.CaseID, as.Parameters
		if o, _ := RunContract(x); o.Result != as.Expected.Result || o.Result == "reject" && o.Code != as.Expected.Reason {
			t.Fatalf("%s: the counterexample no longer gives %s's outcome", pair[1], pair[1])
		}
		if _, problems := CheckContract(as, x, tr); len(problems) == 0 {
			t.Errorf("%s's inputs accepted as %s", pair[0], pair[1])
		}
	}
}

func TestCheckContract(t *testing.T) {
	tr := trusted(t)
	c := caseOf(t, "cross-consumer")
	// Reverse: the record does not show the case.
	for name, change := range map[string]func(*Contract){
		"recorded result differs from the inputs": func(x *Contract) { x.Observed = ContractObserved{Result: "accept"} },
		"the purchase made A's own":               func(x *Contract) { x.Purchase = record(t, caseOf(t, "consumer-a")).Purchase },
		"no purchase":                             func(x *Contract) { x.Purchase = nil },
		"no registry":                             func(x *Contract) { x.Registry = nil },
		"another registry": func(x *Contract) {
			x.Registry = json.RawMessage(strings.Replace(string(x.Registry), "agent-b", "agent-c", 1))
		},
		"no session": func(x *Contract) { x.Session = nil },
		"another field of the purchase changed": func(x *Contract) {
			x.Purchase = json.RawMessage(strings.Replace(string(x.Purchase), `"amount":"500000"`, `"amount":"400000"`, 1))
		},
		"another case's record":       func(x *Contract) { x.CaseID = "consumer-a" },
		"a parameter differs":         func(x *Contract) { x.Parameters = map[string]string{"session": "b", "purchase": "b"} },
		"not a contracts test record": func(x *Contract) { x.Layer = "host_e2e" },
	} {
		x := record(t, c)
		change(&x)
		if _, problems := CheckContract(c, x, tr); len(problems) == 0 {
			t.Errorf("%s: accepted", name)
		}
	}
	// A genuine record against an expected outcome it does not have (the
	// contract code behaving otherwise than the frozen case says).
	for name, change := range map[string]func(*contracts.AcceptanceCase){
		"another expected result": func(c *contracts.AcceptanceCase) { c.Expected.Result = "accept" },
		"another expected code":   func(c *contracts.AcceptanceCase) { c.Expected.Reason = "INVALID_DECISION" },
	} {
		other := c
		change(&other)
		if _, problems := CheckContract(other, record(t, c), tr); len(problems) == 0 {
			t.Errorf("%s: accepted", name)
		}
	}
	// The trusted fixtures must be readable to check anything.
	if _, problems := CheckContract(c, record(t, c), Trusted{}); len(problems) == 0 {
		t.Error("checked without the trusted fixtures")
	}
}

func TestCheckIntegrationNeedsItsTasks(t *testing.T) {
	c := caseOf(t, "restart")
	if _, problems := CheckIntegration(c, nil, []Task{{TaskID: "task-a"}}); len(problems) == 0 {
		t.Fatal("restart with one task accepted")
	}
	if _, problems := CheckIntegration(caseOf(t, "ask"), nil, nil); len(problems) == 0 {
		t.Fatal("no task accepted")
	}
	if _, problems := CheckIntegration(caseOf(t, "ask"), nil, []Task{{TaskID: "task-a"}}); len(problems) == 0 {
		t.Fatal("a task without snapshots accepted")
	}
}

// TestBundle recomputes a produced bundle (APA_M1_BUNDLE=<run directory>)
// from its files and the frozen case list only, read only: each evidence
// file's digest, each case's structured records and the producer's verdict
// against the recomputation. It is the producer side's probe, not B1-07.
func TestBundle(t *testing.T) {
	dir := os.Getenv("APA_M1_BUNDLE")
	if dir == "" {
		t.Skip("APA_M1_BUNDLE not set")
	}
	var cases contracts.CaseManifest
	load(t, "cases.json", &cases)
	var m contracts.RunManifest
	raw, err := os.ReadFile(filepath.Join(dir, "manifest.json"))
	if err != nil || json.Unmarshal(raw, &m) != nil {
		t.Fatalf("manifest: %v", err)
	}
	if err := m.ValidateAgainst(cases); err != nil {
		t.Fatal(err)
	}
	var end map[string]string
	if b, err := os.ReadFile(filepath.Join(dir, "run-end.json")); err != nil || json.Unmarshal(b, &end) != nil || end["state"] != "complete" || end["manifest_digest"] != fileDigest(raw) {
		t.Fatalf("end marker %v (%v)", end, err)
	}
	for _, e := range m.Evidence {
		b, err := os.ReadFile(filepath.Join(dir, e.Path))
		if err != nil || fileDigest(b) != e.Digest {
			t.Errorf("%s: digest mismatch (%v)", e.Path, err)
		}
	}
	read := func(rel string, dst any) bool {
		b, err := os.ReadFile(filepath.Join(dir, rel))
		if err == nil {
			err = json.Unmarshal(b, dst)
		}
		if err != nil {
			t.Errorf("%s: %v", rel, err)
		}
		return err == nil
	}
	verdicts := map[string]string{}
	for _, r := range m.Results {
		verdicts[r.CaseID] = r.Verdict
	}
	for _, c := range cases.Cases {
		var problems []string
		if c.Layer == "contracts" {
			var x Contract
			if !read("contracts/"+c.CaseID+".json", &x) {
				continue
			}
			_, problems = CheckContract(c, x, trusted(t))
		} else {
			var s struct{ Tasks []Task }
			if !read("store/"+c.CaseID+".json", &s) {
				continue
			}
			_, problems = CheckIntegration(c, cases.Defaults, s.Tasks)
			var e struct{ Store []Task }
			if _, err := os.Stat(filepath.Join(dir, "e2e", c.CaseID+".json")); err == nil && read("e2e/"+c.CaseID+".json", &e) {
				_, p := CheckIntegration(c, cases.Defaults, e.Store)
				problems = append(problems, p...)
			}
		}
		want := "PASS"
		if len(problems) > 0 {
			want = "FAIL"
		}
		if verdicts[c.CaseID] != want {
			t.Errorf("%s: the bundle says %s, recomputed %s %v", c.CaseID, verdicts[c.CaseID], want, problems)
		}
	}
}

func fileDigest(b []byte) string {
	h := sha256.Sum256(b)
	return "sha256:" + hex.EncodeToString(h[:])
}
