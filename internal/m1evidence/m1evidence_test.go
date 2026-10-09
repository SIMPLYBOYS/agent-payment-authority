package m1evidence

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"os"
	"path/filepath"
	"slices"
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

// binding builds the record of a binding case as the contracts test makes
// it: the frozen registry, session A and the frozen purchase, as A's or B's.
func binding(t *testing.T, id string, asB bool) Contract {
	c := caseOf(t, id)
	var p contracts.PurchaseOrder
	load(t, "purchase-input.json", &p)
	if asB {
		p.Intent.ConsumerID, p.Intent.AgentID, p.Intent.WalletID, p.Intent.TaskID = "consumer-b", "agent-b", "wallet-b", "task-b"
		p.Intent.PurchaseOrderID, p.Intent.From = "po-b-1", "0x2222222222222222222222222222222222222222"
	}
	raw, _ := json.Marshal(p)
	x := Contract{CaseID: id, Layer: "contracts_test", Operation: c.Operation, Parameters: c.Parameters, Registry: load(t, "registry.json", nil),
		Session: &contracts.Session{ConsumerID: "consumer-a", AgentID: "agent-a", TaskID: "task-a"}, Purchase: raw}
	o, err := RunContract(x)
	if err != nil {
		t.Fatal(err)
	}
	x.Observed = o
	return x
}

func TestCheckContract(t *testing.T) {
	for _, tc := range []struct {
		id  string
		asB bool
		obs ContractObserved
	}{{"consumer-a", false, ContractObserved{Result: "accept"}}, {"cross-consumer", true, ContractObserved{Result: "reject", Code: "OWNERSHIP_MISMATCH"}}} {
		x := binding(t, tc.id, tc.asB)
		o, problems := CheckContract(caseOf(t, tc.id), x)
		if len(problems) > 0 || o != tc.obs {
			t.Fatalf("%s: %+v %v", tc.id, o, problems)
		}
	}
	// Reverse: the record does not show the case.
	for name, change := range map[string]func(*Contract){
		"recorded result differs from the inputs": func(x *Contract) { x.Observed = ContractObserved{Result: "accept"} },
		"the purchase made A's own":               func(x *Contract) { *x = binding(t, "cross-consumer", false) },
		"no purchase":                             func(x *Contract) { x.Purchase = nil },
		"no registry":                             func(x *Contract) { x.Registry = nil },
		"another case's record":                   func(x *Contract) { x.CaseID = "consumer-a" },
		"a parameter differs":                     func(x *Contract) { x.Parameters = map[string]string{"session": "b", "purchase": "b"} },
		"not a contracts test record":             func(x *Contract) { x.Layer = "host_e2e" },
	} {
		x := binding(t, "cross-consumer", true)
		change(&x)
		if _, problems := CheckContract(caseOf(t, "cross-consumer"), x); len(problems) == 0 {
			t.Errorf("%s: accepted", name)
		}
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
			_, problems = CheckContract(c, x)
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
