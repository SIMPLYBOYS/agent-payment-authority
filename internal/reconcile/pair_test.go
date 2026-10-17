package reconcile

import (
	"testing"

	"github.com/SIMPLYBOYS/agent-payment-authority/internal/eth"
)

// One transaction running two authorizations (a batch): each accounts for
// the Transfer right after its own AuthorizationUsed, and for no other.
func TestPairing(t *testing.T) {
	token, w := eth.Address{0x7}, eth.Address{0x1}
	x, shop := eth.Address{0xa}, eth.Address{0xb}
	na, nb := eth.Bytes32{0x11}, eth.Bytes32{0x22}
	used := func(i uint64, n eth.Bytes32) eth.Log {
		return eth.Log{Address: token, LogIndex: i, Topics: [][32]byte{usedTopic, word(w), n}}
	}
	transfer := func(i uint64, to eth.Address, v uint64) eth.Log {
		data, _ := eth.Encode(v)
		return eth.Log{Address: token, LogIndex: i, Topics: [][32]byte{transferTopic, word(w), word(to)}, Data: data}
	}
	rc := &eth.Receipt{Status: 1, Logs: []eth.Log{used(0, na), transfer(1, x, 5), used(2, nb), transfer(3, shop, 5), transfer(4, shop, 5)}}
	a := Authorization{From: w, To: shop, Value: 5, ValidBefore: 1, Nonce: na} // recorded to the shop; it paid x
	b := Authorization{From: w, To: shop, Value: 5, ValidBefore: 1, Nonce: nb}
	if l := pairedTransfer(rc, token, rc.Logs[0]); l == nil || l.LogIndex != 1 {
		t.Fatalf("A's transfer: %+v", l)
	}
	if l := pairedTransfer(rc, token, rc.Logs[2]); l == nil || l.LogIndex != 3 {
		t.Fatalf("B's transfer: %+v", l)
	}
	signed := map[[2][32]byte]Authorization{{word(w), na}: a, {word(w), nb}: b}
	// An AuthorizationUsed of another payer, or of another token, right before a Transfer accounts for nothing.
	otherPayer := eth.Log{Address: token, LogIndex: 5, Topics: [][32]byte{usedTopic, word(eth.Address{0x2}), nb}}
	otherToken := eth.Log{Address: eth.Address{0x8}, LogIndex: 7, Topics: [][32]byte{usedTopic, word(w), nb}}
	rc2 := &eth.Receipt{Status: 1, Logs: []eth.Log{otherPayer, transfer(6, shop, 5), otherToken, transfer(8, shop, 5)}}
	for _, i := range []int{1, 3} {
		if _, ok := accounted(rc2, token, w, rc2.Logs[i], signed); ok {
			t.Errorf("rc2 transfer %d: accounted", i)
		}
	}
	for i, want := range map[int]bool{1: false, 3: true, 4: false} {
		n, ok := accounted(rc, token, w, rc.Logs[i], signed)
		if ok != want {
			t.Errorf("transfer %d: accounted %v, want %v", i, ok, want)
		}
		if i == 4 && n != nil {
			t.Errorf("transfer 4 has no authorization of its own, got nonce %x", *n)
		}
	}
}
