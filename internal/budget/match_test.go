package budget

import (
	"context"
	"math/big"
	"path/filepath"
	"testing"

	"github.com/SIMPLYBOYS/agent-payment-authority/internal/eth"
)

// spentFor without a chain: each field of a Spent log on its own.
func TestSpentFor(t *testing.T) {
	contract, other := eth.Address{1}, eth.Address{2}
	task := big.NewInt(42)
	intent := eth.Bytes32{0xab}
	log := func(addr eth.Address, topic0 [32]byte, task *big.Int, intent eth.Bytes32, amount uint64, removed bool) eth.Log {
		var t1 [32]byte
		task.FillBytes(t1[:])
		data, _ := eth.Encode(intent, amount, uint64(1), uint64(2))
		return eth.Log{Address: addr, Topics: [][32]byte{topic0, t1}, Data: data, Removed: removed}
	}
	if !spentFor([]eth.Log{log(contract, spentTopic, task, intent, 5, false)}, contract, task, intent, 5) {
		t.Fatal("the reservation's own Spent")
	}
	for name, l := range map[string]eth.Log{
		"another contract": log(other, spentTopic, task, intent, 5, false),
		"another event":    log(contract, eth.Topic("TaskRegistered(uint256,string,uint256)"), task, intent, 5, false),
		"another task":     log(contract, spentTopic, big.NewInt(43), intent, 5, false),
		"another intent":   log(contract, spentTopic, task, eth.Bytes32{0xac}, 5, false),
		"another amount":   log(contract, spentTopic, task, intent, 6, false),
		"removed (reorg)":  log(contract, spentTopic, task, intent, 5, true),
	} {
		if spentFor([]eth.Log{l}, contract, task, intent, 5) {
			t.Errorf("%s: accepted", name)
		}
	}
}

func TestOpenRefuses(t *testing.T) {
	k := func(b byte) *eth.Key {
		s := make([]byte, 32)
		s[31] = b
		key, _ := eth.NewKey(s)
		return key
	}
	for name, cs := range map[string]struct {
		cfg  Config
		keys Keys
	}{
		"no confirmation":            {Config{}, Keys{Gas: k(1), Issuer: k(2), Policy: k(3)}},
		"one key for gas and issuer": {Config{Confirmations: 1}, Keys{Gas: k(1), Issuer: k(1), Policy: k(3)}},
		"no policy key":              {Config{Confirmations: 1}, Keys{Gas: k(1), Issuer: k(2)}},
	} {
		if _, err := Open(context.Background(), cs.cfg, cs.keys, nil, filepath.Join(t.TempDir(), "b.db"), nil); err == nil {
			t.Errorf("%s: opened", name)
		}
	}
}
