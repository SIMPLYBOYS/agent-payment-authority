package store

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/SIMPLYBOYS/agent-payment-authority/internal/kyt"
	"github.com/SIMPLYBOYS/agent-payment-authority/internal/paycontract"
)

// docs/commercial-kyt.md: with the Host's risk list, a purchase to a flagged
// payee is Deny PAYEE_FLAGGED before anything else is decided; the same
// purchase without the list is the Ask it always was.
func TestPayeeFlagged(t *testing.T) {
	e := newEnv(t)
	p := filepath.Join(t.TempDir(), "list.json")
	body := `{"version":"apa.kyt-demo-list.v1","source":"test","updated_at":"x","addresses":[{"address":"` + strings.ToLower(e.reg.MerchantPayTo) + `","reason":"test"}]}`
	if err := os.WriteFile(p, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	list, err := kyt.Load(p)
	if err != nil {
		t.Fatal(err)
	}
	for _, c := range []struct {
		list            *kyt.List
		verdict, reason string
	}{{list, "Deny", "PAYEE_FLAGGED"}, {nil, "Ask", "HUMAN_APPROVAL_REQUIRED"}} {
		e := newEnv(t)
		s, err := Open(Config{Path: e.path, Registry: e.reg, Offers: e.offers, Clock: e.clock.clock(), RiskList: c.list})
		if err != nil {
			t.Fatal(err)
		}
		setup := e.setup("task-a")
		setup.ContractVersion, setup.DelegationID = paycontract.Version, e.delegation(s, "consumer-a", "dlg-req-a-0000000000001", 30)
		if err := s.StartTask(setup); err != nil {
			t.Fatal(err)
		}
		d := submitV1(t, s, sessA, v1Of(t, e.base)).Purchase.Decision
		if d.PolicyVerdict != c.verdict || d.Reason != c.reason {
			t.Errorf("list %v: %+v", c.list != nil, d)
		}
		s.Close()
	}
}
