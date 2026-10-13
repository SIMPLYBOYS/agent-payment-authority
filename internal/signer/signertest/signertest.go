// Package signertest builds synthetic Signer material for tests: apa.pay.v1
// purchase orders and Signer configurations with test addresses. Nothing in
// it is a key, a real delegation or a reservation; production code does
// not import it.
package signertest

import (
	"encoding/json"
	"fmt"

	"github.com/SIMPLYBOYS/agent-payment-authority/internal/contracts"
	"github.com/SIMPLYBOYS/agent-payment-authority/internal/paycontract"
)

const (
	Chain    = "84532"
	Asset    = "0x4444444444444444444444444444444444444444"
	Merchant = "0x3333333333333333333333333333333333333333"
	URL      = "https://shop.example/resources/kr-market"
)

// Order describes one purchase; PO builds it as the Host's tool bridge
// does, with the quote matching the binding.
type Order struct {
	Consumer, Agent, Wallet, Task string
	From, PayTo, Amount           string
	N                             int    // purchase and nonce number
	Now, Window                   uint64 // valid_before = Now + Window
	DomainVersion                 string // the quote's EIP-712 version; "2" if empty
	Contract                      string // a pay-family version; apa.pay.v1 if empty
}

func (o Order) Quote() contracts.Quote {
	v := o.DomainVersion
	if v == "" {
		v = "2"
	}
	req, _ := json.Marshal(map[string]any{"scheme": "exact", "network": "eip155:" + Chain, "asset": Asset, "payTo": o.PayTo, "amount": o.Amount,
		"maxTimeoutSeconds": 60, "extra": map[string]string{"name": "USDC", "version": v}})
	return contracts.Quote{X402Version: 2, SelectedRequirement: req, Extensions: json.RawMessage(`{}`)}
}

func (o Order) PO() paycontract.PurchaseOrder {
	q := o.Quote()
	version := o.Contract
	if version == "" {
		version = paycontract.Version
	}
	sp, _ := paycontract.SpecOf(version)
	rh, _ := sp.ResourceHash("GET", URL)
	qh, _ := sp.RequirementHash(q)
	task := o.Task
	if task == "" {
		task = "task-" + o.Consumer
	}
	return paycontract.PurchaseOrder{Version: version, RequestID: fmt.Sprintf("req-%d", o.N), Quote: q,
		Intent: paycontract.Intent{Version: version, Domain: sp.Domain, ConsumerID: o.Consumer, AgentID: o.Agent, WalletID: o.Wallet,
			TaskID: task, PurchaseOrderID: fmt.Sprintf("po-%d", o.N), ResourceHash: rh, HTTPMethod: "GET", ResourceURL: URL,
			X402Version: 2, Scheme: "exact", PaymentRequirementHash: qh},
		PaymentBinding: paycontract.PaymentBinding{Method: "eip3009", ChainID: Chain, AssetContract: Asset, From: o.From, PayTo: o.PayTo,
			Amount: contracts.Amount(o.Amount), ValidAfter: o.Now - 1, ValidBefore: o.Now + o.Window, Nonce: fmt.Sprintf("0x%064x", o.N)}}
}

func (o Order) Raw() json.RawMessage {
	raw, _ := json.Marshal(o.PO())
	return raw
}

// Config is a Signer configuration with wallets wallet-a and wallet-b of
// consumers consumer-a and consumer-b at the given addresses, each with a
// delegation dlg-a / dlg-b (per transaction 600000, total 1000000, valid
// from now-100 to now+3600) to Merchant.
func Config(socket string, caller uint32, now uint64, addrA, addrB string) []byte {
	w := func(x, addr string) map[string]string {
		return map[string]string{"wallet_id": "wallet-" + x, "consumer_id": "consumer-" + x, "chain_id": Chain, "address": addr}
	}
	d := func(x string) map[string]any {
		return map[string]any{"delegation_id": "dlg-" + x, "version": 1, "consumer_id": "consumer-" + x, "wallet_id": "wallet-" + x, "chain_id": Chain,
			"asset_contract": Asset, "merchants": []string{Merchant}, "per_tx_max": "600000", "total_cap": "1000000", "valid_from": now - 100, "valid_until": now + 3600}
	}
	raw, _ := json.Marshal(map[string]any{"version": 1, "socket": socket, "callers": []uint32{caller}, "max_window_seconds": 600,
		"wallets":     []map[string]string{w("a", addrA), w("b", addrB)},
		"assets":      []map[string]string{{"chain_id": Chain, "asset_contract": Asset, "eip712_name": "USDC", "eip712_version": "2"}},
		"delegations": []map[string]any{d("a"), d("b")}})
	return raw
}
