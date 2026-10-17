// Package budget is P2's budget relayer (P2-D2, docs/commercial-p2.md): it
// registers tasks and reserves an order's amount from its task's private
// budget on the Z2 contract (tx#1), and it is the payment Signer's check
// that a reservation is on chain (Verifier). Local anvil until a testnet
// is approved.
package budget

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"math/big"
	"strings"

	"github.com/SIMPLYBOYS/agent-payment-authority/internal/eth"
)

// Labels and events of contracts/src/Budget.sol.
var (
	taskLabel   = []byte("agent-payment-authority/zk/v2/task")
	policyLabel = eth.Keccak256([]byte("agent-payment-authority/zk/v2/policy"))
	spentTopic  = eth.Topic("Spent(uint256,bytes32,uint256,uint256,uint256)")
)

const spendSig = "spend(string,uint256,uint256,uint256,uint256,bytes32,uint256,bytes,bytes)"

// TaskField is the contract's taskField(taskID): the first 31 bytes of
// SHA-256(label ‖ chain ID (32 bytes) ‖ contract ‖ task ID).
func TaskField(chainID uint64, contract eth.Address, taskID string) *big.Int {
	h := sha256.New()
	h.Write(taskLabel)
	h.Write(new(big.Int).SetUint64(chainID).FillBytes(make([]byte, 32)))
	h.Write(contract[:])
	h.Write([]byte(taskID))
	return new(big.Int).Rsh(new(big.Int).SetBytes(h.Sum(nil)), 8)
}

// IntentBytes is an apa.pay intent hash ("sha256:" + 64 hex) as the
// contract's bytes32.
func IntentBytes(intent string) (eth.Bytes32, error) {
	var out eth.Bytes32
	b, err := hex.DecodeString(strings.TrimPrefix(intent, "sha256:"))
	if err != nil || len(b) != 32 || !strings.HasPrefix(intent, "sha256:") {
		return out, errors.New("an intent hash is sha256: + 64 hex")
	}
	copy(out[:], b)
	return out, nil
}

// PolicyDigest is the contract's policyDigest, computed here: the policy
// key never signs a digest an RPC node chose.
func PolicyDigest(chainID uint64, contract eth.Address, task *big.Int, intent eth.Bytes32, amount uint64, nullifier, cNew *big.Int,
	deadline uint64, epoch *big.Int) ([]byte, error) {
	var label eth.Bytes32
	copy(label[:], policyLabel)
	enc, err := eth.Encode(label, chainID, contract, task, intent, amount, nullifier, cNew, deadline, epoch)
	if err != nil {
		return nil, err
	}
	return eth.Keccak256(enc), nil
}
