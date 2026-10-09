package verification

import (
	"encoding/json"
	"fmt"
	"reflect"
	"sort"
	"strings"

	"github.com/SIMPLYBOYS/agent-payment-authority/internal/contracts"
)

// contractRecord is a contracts case's record (internal/contracts
// TestContractCasesFromManifest): what was called and what it gave.
type contractRecord struct {
	CaseID     string                    `json:"case_id"`
	Layer      string                    `json:"layer"`
	Operation  string                    `json:"operation"`
	Parameters map[string]string         `json:"parameters"`
	Registry   json.RawMessage           `json:"registry,omitempty"`
	Session    *contracts.Session        `json:"session,omitempty"`
	Purchase   json.RawMessage           `json:"purchase,omitempty"`
	Incoming   json.RawMessage           `json:"incoming,omitempty"`
	Decision   *contracts.PolicyDecision `json:"decision,omitempty"`
	Observed   struct {
		Result string `json:"result"`
		Code   string `json:"code,omitempty"`
	} `json:"observed"`
}

// contractRecord checks a contracts case: its inputs are the trusted
// fixtures with exactly the case's changes (V2, V7 for identities), the
// contract code called again on them gives the frozen expected outcome
// (V3), and the record's own observed agrees (else it contradicts itself).
func (v *verifier) contractRecord(f *findings, c contracts.AcceptanceCase, p string) Observed {
	ev := []string{p}
	o := Observed{PaymentStatus: "unavailable", BudgetDeducted: "0", ForbiddenFound: []string{}}
	var rec contractRecord
	if !v.readJSON(f, "V2", p, &rec) {
		return o
	}
	if rec.CaseID != c.CaseID || rec.Layer != "contracts_test" || rec.Operation != c.Operation {
		f.fail("V6", ev, "the record is %s %s %s, not this case's contracts record", quote(rec.CaseID), quote(rec.Layer), quote(rec.Operation))
		return o
	}
	if !reflect.DeepEqual(rec.Parameters, c.Parameters) && !(len(rec.Parameters) == 0 && len(c.Parameters) == 0) {
		f.fail("V2", ev, "the record's parameters %v are not the case's %v", rec.Parameters, c.Parameters)
	}
	if v.tr.registry == nil || v.tr.Purchase.Version == "" {
		f.missing("V2", ev, "the trusted purchase or registry fixture is not in the repository")
		return o
	}
	purchase := func(name string, raw json.RawMessage) *contracts.PurchaseOrder {
		if len(raw) == 0 {
			f.fail("V2", ev, "no %s recorded", name)
			return nil
		}
		var po contracts.PurchaseOrder
		if err := strictJSON(raw, &po); err != nil {
			f.fail("V2", ev, "%s: %v", name, err)
			return nil
		}
		return &po
	}
	base := v.tr.Purchase
	params := c.Parameters
	var res, code string
	var err error
	outcome := func(e error) {
		res, err = "accept", e
		if e != nil {
			res = "reject"
		}
	}
	switch c.Operation {
	case "binding":
		s := v.party(f, ev, params["session"])
		owner := v.party(f, ev, params["purchase"])
		po := purchase("purchase", rec.Purchase)
		if s == nil || owner == nil || po == nil {
			return o
		}
		if rec.Session == nil || *rec.Session != (contracts.Session{ConsumerID: s.consumer, AgentID: s.agent, TaskID: s.task}) {
			f.fail("V7", ev, "the session is not %s's", quote(params["session"]))
		}
		allowed := v.as(owner, base)
		if m := params["mutation"]; m != "" {
			b := v.party(f, ev, "b")
			field := map[string]string{"wallet_b": "intent.wallet_id", "from_b": "intent.from", "consumer_b": "intent.consumer_id", "agent_b": "intent.agent_id"}[m]
			if field == "" || b == nil {
				f.fail("V2", ev, "mutation %s is not one this checker knows", quote(m))
				return o
			}
			value := map[string]string{"wallet_b": b.wallet, "from_b": b.from, "consumer_b": b.consumer, "agent_b": b.agent}[m]
			allowed[field] = equals(value)
		}
		changes(f, ev, "purchase", *po, base, allowed)
		var cfg contracts.RegistryConfig
		if strictJSON(rec.Registry, &cfg) != nil || !reflect.DeepEqual(cfg, v.tr.Registry) {
			f.fail("V2", ev, "the recorded registry is not the trusted one")
		}
		if rec.Session != nil {
			outcome(v.tr.registry.ValidatePurchase(*rec.Session, *po)) // the trusted registry, not the recorded one
		}
	case "compare-retry":
		existing, incoming := purchase("existing purchase", rec.Purchase), purchase("incoming purchase", rec.Incoming)
		if existing == nil || incoming == nil {
			return o
		}
		changes(f, ev, "existing purchase", *existing, base, nil)
		var allowed map[string]func(string) bool
		switch m := params["mutation"]; m {
		case "none":
		case "nonce":
			allowed = map[string]func(string) bool{"intent.eip3009_nonce": differs(fmt.Sprint(base.Intent.EIP3009Nonce))}
		case "request_id":
			allowed = map[string]func(string) bool{"request_id": differs(base.RequestID)}
		case "other_consumer":
			b := v.party(f, ev, "b")
			if b == nil {
				return o
			}
			allowed = v.as(b, base)
		default:
			f.fail("V2", ev, "mutation %s is not one this checker knows", quote(m))
			return o
		}
		changes(f, ev, "incoming purchase", *incoming, base, allowed)
		res, err = contracts.CompareRetry(*existing, *incoming)
		if err != nil {
			res = "reject"
		}
	case "validate-decision":
		d := rec.Decision
		if d == nil {
			f.fail("V2", ev, "no decision recorded")
			return o
		}
		h, _ := base.Intent.Hash()
		if d.IntentHash != h || d.PaymentStatus != params["payment_status"] {
			f.fail("V2", ev, "the decision is not the trusted purchase's with payment status %s", quote(params["payment_status"]))
		}
		// The case's field is the only defect: set right, the decision is valid.
		right := *d
		right.PaymentStatus = "unavailable"
		if e := right.Validate(); e != nil {
			f.fail("V2", ev, "the decision has another defect than its payment status: %v", e)
		}
		outcome(d.Validate())
	case "validate-purchase":
		po := purchase("purchase", rec.Purchase)
		if po == nil {
			return o
		}
		var allowed map[string]func(string) bool
		switch m := params["mutation"]; m {
		case "transfer_method_erc7710", "transfer_method_permit2":
			allowed = map[string]func(string) bool{"requirement.extra.assetTransferMethod": equals(strings.TrimPrefix(m, "transfer_method_"))}
		case "case_variant_pay_to":
			allowed = map[string]func(string) bool{"requirement.payto": func(s string) bool { return s != "" && s != absent }}
		default:
			f.fail("V2", ev, "mutation %s is not one this checker knows", quote(m))
			return o
		}
		// The requirement digest follows the quote where it can be computed.
		if h, e := po.Quote.Hash(); e == nil {
			allowed["intent.payment_requirement_hash"] = equals(h)
		}
		changes(f, ev, "purchase", *po, base, allowed)
		outcome(po.Validate())
	default:
		f.missing("V3", ev, "operation %s is not one this checker knows", quote(c.Operation))
		return o
	}
	if err != nil {
		code = contracts.ErrorCode(err)
	}
	o.Result, o.Reason, o.ApprovalStatus = res, code, "not_requested"
	if res != c.Expected.Result {
		f.fail("V3", ev, "the contract code gives %s on these inputs, expected %s", quote(res), quote(c.Expected.Result))
	}
	if res == "reject" && code != c.Expected.Reason {
		f.fail("V3", ev, "rejected with %s, expected %s", quote(code), quote(c.Expected.Reason))
	}
	if res != "reject" {
		o.Reason = c.Expected.Reason // an accepted result has no code; the case list names it
	}
	if rec.Observed.Result != res || rec.Observed.Code != code {
		f.fail("V3", ev, "the record says %s %s; its own inputs give %s %s", quote(rec.Observed.Result), quote(rec.Observed.Code), quote(res), quote(code))
	}
	return o
}

// party is the identity a case's "a" or "b" names: task-a's or task-b's
// binding in the trusted registry.
type partyID struct{ consumer, agent, wallet, from, task string }

func (v *verifier) party(f *findings, ev []string, letter string) *partyID {
	t, w, err := v.tr.registry.Binding("task-" + letter)
	if (letter != "a" && letter != "b") || err != nil || t.ConsumerID != "consumer-"+letter {
		f.fail("V2", ev, "%s names no identity in the trusted registry", quote(letter))
		return nil
	}
	return &partyID{t.ConsumerID, t.AgentID, w.WalletID, w.From, t.TaskID}
}

// as are the changes that make the trusted purchase the party's: its
// identity fields, and another purchase order ID when it is not A's.
func (v *verifier) as(who *partyID, base contracts.PurchaseOrder) map[string]func(string) bool {
	out := map[string]func(string) bool{}
	i := base.Intent
	for field, pair := range map[string][2]string{"intent.consumer_id": {who.consumer, i.ConsumerID}, "intent.agent_id": {who.agent, i.AgentID},
		"intent.wallet_id": {who.wallet, i.WalletID}, "intent.from": {who.from, i.From}, "intent.task_id": {who.task, i.TaskID}} {
		if pair[0] != pair[1] {
			out[field] = equals(pair[0])
		}
	}
	if len(out) > 0 {
		out["intent.purchase_order_id"] = differs(i.PurchaseOrderID)
	}
	return out
}

const absent = "\x00absent"

func equals(want string) func(string) bool { return func(s string) bool { return s == want } }
func differs(base string) func(string) bool {
	return func(s string) bool { return s != base && s != absent }
}

// fields lists a purchase's fields by name with their values (absent for
// a requirement member that is not there): the request ID, each intent
// field, the quote's version and extensions, and the selected
// requirement's members, extra's one by one.
func fields(p contracts.PurchaseOrder) map[string]string {
	out := map[string]string{"request_id": p.RequestID, "version": p.Version, "quote.x402_version": fmt.Sprint(p.Quote.X402Version)}
	iv, it := reflect.ValueOf(p.Intent), reflect.TypeOf(p.Intent)
	for k := 0; k < it.NumField(); k++ {
		name, _, _ := strings.Cut(it.Field(k).Tag.Get("json"), ",")
		out["intent."+name] = fmt.Sprint(iv.Field(k).Interface())
	}
	ext, _ := contracts.Canonical(p.Quote.Extensions)
	out["quote.extensions"] = string(ext)
	var req map[string]json.RawMessage
	if json.Unmarshal(p.Quote.SelectedRequirement, &req) != nil {
		out["requirement"] = "\x00unreadable"
		return out
	}
	for k, raw := range req {
		if k == "extra" {
			var extra map[string]json.RawMessage
			if json.Unmarshal(raw, &extra) == nil {
				for ek, eraw := range extra {
					c, _ := contracts.Canonical(eraw)
					out["requirement.extra."+ek] = string(c)
				}
				continue
			}
		}
		c, _ := contracts.Canonical(raw)
		out["requirement."+k] = string(c)
	}
	return out
}

// changes: every field of got is base's, except the allowed ones, which
// must hold their change.
func changes(f *findings, ev []string, name string, got, base contracts.PurchaseOrder, allowed map[string]func(string) bool) {
	g, b := fields(got), fields(base)
	names := map[string]bool{}
	for k := range g {
		names[k] = true
	}
	for k := range b {
		names[k] = true
	}
	for k := range allowed {
		names[k] = true
	}
	var keys []string
	for k := range names {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	for _, k := range keys {
		gv, ok := g[k]
		if !ok {
			gv = absent
		}
		bv, ok := b[k]
		if !ok {
			bv = absent
		}
		// A requirement member's string value is compared as JSON text.
		if ok := allowed[k]; ok != nil {
			if !ok(unquote(gv)) {
				f.fail("V2", ev, "%s %s is %s, not the case's change", name, k, quote(gv))
			}
		} else if gv != bv {
			f.fail("V2", ev, "%s %s is %s; the trusted fixture's is %s and the case does not change it", name, k, quote(gv), quote(bv))
		}
	}
}

func unquote(s string) string {
	var out string
	if json.Unmarshal([]byte(s), &out) == nil {
		return out
	}
	return s
}
