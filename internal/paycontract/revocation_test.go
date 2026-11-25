package paycontract

import (
	"encoding/hex"
	"strings"
	"testing"

	"github.com/SIMPLYBOYS/agent-payment-authority/internal/contracts"
	"github.com/decred/dcrd/dcrec/secp256k1/v4"
	"github.com/decred/dcrd/dcrec/secp256k1/v4/ecdsa"
)

// SignReceipt signs r with key as the Signer does (r ‖ s ‖ v), for tests.
func SignReceipt(t *testing.T, key *secp256k1.PrivateKey, r contracts.RevocationReceipt) contracts.RevocationReceipt {
	t.Helper()
	d, err := RevocationDigest(r)
	if err != nil {
		t.Fatal(err)
	}
	c := ecdsa.SignCompact(key, d, false)
	r.Signature = "0x" + hex.EncodeToString(append(append([]byte{}, c[1:65]...), c[0]))
	return r
}

func addressOf(key *secp256k1.PrivateKey) string {
	return "0x" + hex.EncodeToString(keccak(key.PubKey().SerializeUncompressed()[1:])[12:])
}

// The receipt (docs/commercial-gap4.md 7.1): signed by the wallet's key, any
// change to what it lists changes who signed it.
func TestRevocationReceipt(t *testing.T) {
	key, _ := secp256k1.GeneratePrivateKey()
	in := func(c string) string { return "sha256:" + strings.Repeat(c, 64) }
	r := SignReceipt(t, key, contracts.RevocationReceipt{DelegationID: "dlg-1", RevocationID: "hap-1", RevokedAt: 1790726450, ChainID: "84532",
		Wallet: addressOf(key), SignedIntents: []string{in("a"), in("b")}})
	if by, err := ReceiptSigner(r); err != nil || by != addressOf(key) {
		t.Fatalf("signer %s %v", by, err)
	}
	more := r
	more.SignedIntents = []string{in("a"), in("b"), in("c")}
	if by, err := ReceiptSigner(more); err == nil && by == addressOf(key) {
		t.Error("an intent added after signing still verifies")
	}
	for name, bad := range map[string]func(*contracts.RevocationReceipt){
		"unsorted":         func(x *contracts.RevocationReceipt) { x.SignedIntents = []string{in("b"), in("a")} },
		"twice":            func(x *contracts.RevocationReceipt) { x.SignedIntents = []string{in("a"), in("a")} },
		"not an intent":    func(x *contracts.RevocationReceipt) { x.SignedIntents = []string{"0x" + strings.Repeat("a", 64)} },
		"upper-case":       func(x *contracts.RevocationReceipt) { x.Wallet = strings.ToUpper(x.Wallet) },
		"a short sig":      func(x *contracts.RevocationReceipt) { x.Signature = x.Signature[:20] },
		"no delegation ID": func(x *contracts.RevocationReceipt) { x.DelegationID = "" },
	} {
		x := r
		bad(&x)
		if _, err := ReceiptSigner(x); err == nil {
			t.Errorf("%s: accepted", name)
		}
	}
	// The event carries it only naming its own delegation, revocation and the task's wallet.
	var cfg contracts.RegistryConfig
	fixture(t, "testdata/contracts/registry.json", &cfg)
	reg, err := contracts.NewRegistry(cfg)
	if err != nil {
		t.Fatal(err)
	}
	_, o := base(t)
	rev := contracts.EventPayload{DelegationID: "dlg-1", AuthorizationMethod: "webauthn", SignerApprovalID: "hap-1"}
	es := chainOf(t, o, step{"TASK_STARTED", contracts.EventPayload{}, false}, step{"DELEGATION_BOUND", contracts.EventPayload{DelegationID: "dlg-1"}, false},
		step{"DELEGATION_REVOKED", rev, false})
	_, w, _ := reg.Binding(o.Intent.TaskID)
	ok := contracts.RevocationReceipt{DelegationID: "dlg-1", RevocationID: "hap-1", RevokedAt: 1, ChainID: w.ChainID, Wallet: strings.ToLower(w.From), Signature: "0x00"}
	for name, c := range map[string]struct {
		edit func(*contracts.RevocationReceipt)
		ok   bool
	}{
		"the task's":           {func(*contracts.RevocationReceipt) {}, true},
		"another wallet":       {func(x *contracts.RevocationReceipt) { x.Wallet = addressOf(key) }, false},
		"another revocation":   {func(x *contracts.RevocationReceipt) { x.RevocationID = "hap-2" }, false},
		"another chain":        {func(x *contracts.RevocationReceipt) { x.ChainID = "1" }, false},
		"another delegation's": {func(x *contracts.RevocationReceipt) { x.DelegationID = "dlg-2" }, false},
	} {
		x := ok
		c.edit(&x)
		e := es[2]
		e.Payload.Receipt = &x
		if err := ValidateEvent(reg, e, nil); (err == nil) != c.ok {
			t.Errorf("%s: %v", name, err)
		}
	}
	bound := es[1]
	bound.Payload.Receipt = &ok
	if ValidateEvent(reg, bound, nil) == nil {
		t.Error("DELEGATION_BOUND took a receipt")
	}
}
