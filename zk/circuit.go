// Package zk is Z2's budget circuit, its setup ceremony and prover
// (docs/commercial-z2.md). One spend of a task's budget is proven without
// showing the balance:
//
//   - the old commitment opens: C_old = H(commit, task, balance_old, r_old);
//   - balance_old, amount and balance_new = balance_old − amount are each in
//     [0, 2^64) (no wrap-around, no negative amount);
//   - the new commitment holds the rest: C_new = H(commit, task, balance_new, r_new);
//   - the nullifier is the old state's: N = H(nullifier, task, r_old);
//   - intent_hash is bound as two 128-bit limbs, each range-checked (Z1-D1).
//
// Public inputs, in order: task, C_old, amount, C_new, N, intent_hi,
// intent_lo. H is MiMC over the BN254 scalar field (Z1-D3). The task field
// is bound to the chain and the contract (Z2-D2), so a proof is valid on one
// deployment only. Synthetic data; the setup is the host's own (Z1-D4).
package zk

import (
	"crypto/sha256"
	"encoding/binary"
	"errors"
	"math/big"

	"github.com/consensys/gnark-crypto/ecc"
	"github.com/consensys/gnark-crypto/ecc/bn254/fr"
	nmimc "github.com/consensys/gnark-crypto/ecc/bn254/fr/mimc"
	"github.com/consensys/gnark/frontend"
	"github.com/consensys/gnark/std/hash/mimc"
)

// Field is the BN254 scalar field modulus r.
var Field = ecc.BN254.ScalarField()

func domain(label string) *big.Int {
	s := sha256.Sum256([]byte("agent-payment-authority/zk/v2/" + label))
	return new(big.Int).Mod(new(big.Int).SetBytes(s[:]), Field)
}

var (
	DomCommit    = domain("commit")
	DomNullifier = domain("nullifier")
)

// TaskLabel prefixes the task field's preimage; the contract uses the same
// bytes (contracts/src/Budget.sol).
const TaskLabel = "agent-payment-authority/zk/v2/task"

// Spend is the circuit.
type Spend struct {
	Task      frontend.Variable `gnark:",public"`
	COld      frontend.Variable `gnark:",public"`
	Amount    frontend.Variable `gnark:",public"`
	CNew      frontend.Variable `gnark:",public"`
	Nullifier frontend.Variable `gnark:",public"`
	IntentHi  frontend.Variable `gnark:",public"`
	IntentLo  frontend.Variable `gnark:",public"`

	BalanceOld frontend.Variable
	ROld, RNew frontend.Variable
}

func hash(api frontend.API, in ...frontend.Variable) (frontend.Variable, error) {
	h, err := mimc.NewMiMC(api)
	if err != nil {
		return nil, err
	}
	h.Write(in...)
	return h.Sum(), nil
}

// Define states the constraints listed in the package comment.
func (c *Spend) Define(api frontend.API) error {
	api.ToBinary(c.BalanceOld, 64)
	api.ToBinary(c.Amount, 64)
	balanceNew := api.Sub(c.BalanceOld, c.Amount)
	api.ToBinary(balanceNew, 64) // non-negative: a wrapped difference is about 2^254
	old, err := hash(api, DomCommit, c.Task, c.BalanceOld, c.ROld)
	if err != nil {
		return err
	}
	api.AssertIsEqual(c.COld, old)
	next, err := hash(api, DomCommit, c.Task, balanceNew, c.RNew)
	if err != nil {
		return err
	}
	api.AssertIsEqual(c.CNew, next)
	n, err := hash(api, DomNullifier, c.Task, c.ROld)
	if err != nil {
		return err
	}
	api.AssertIsEqual(c.Nullifier, n)
	// Every public input takes part in a constraint: one that does not
	// would verify with any value (Z1 finding 1).
	api.ToBinary(c.IntentHi, 128)
	api.ToBinary(c.IntentLo, 128)
	return nil
}

// Hash is H natively: MiMC over the field elements in order.
func Hash(in ...*big.Int) *big.Int {
	h := nmimc.NewMiMC()
	for _, x := range in {
		var e fr.Element
		e.SetBigInt(x)
		b := e.Bytes()
		h.Write(b[:]) // canonical 32 bytes below r: MiMC's Write cannot refuse them
	}
	return new(big.Int).SetBytes(h.Sum(nil))
}

// TaskField is the task's field element on one deployment (Z1-D2, Z2-D2):
// the first 31 bytes of SHA-256(TaskLabel ‖ chain_id (32 bytes) ‖ contract
// (20 bytes) ‖ task_id), as Solidity's abi.encodePacked lays them out.
func TaskField(chainID uint64, contract [20]byte, task string) *big.Int {
	var chain [32]byte
	binary.BigEndian.PutUint64(chain[24:], chainID)
	h := sha256.New()
	h.Write([]byte(TaskLabel))
	h.Write(chain[:])
	h.Write(contract[:])
	h.Write([]byte(task))
	return new(big.Int).SetBytes(h.Sum(nil)[:31])
}

// IntentLimbs splits a 32-byte intent hash into hi and lo 128-bit limbs (Z1-D1).
func IntentLimbs(h [32]byte) (hi, lo *big.Int) {
	return new(big.Int).SetBytes(h[:16]), new(big.Int).SetBytes(h[16:])
}

// State is a task's private budget state.
type State struct {
	Task    *big.Int
	Balance uint64
	R       *big.Int // full entropy, below r (Z1 review S4 (e))
}

func (s State) Commitment() *big.Int {
	return Hash(DomCommit, s.Task, new(big.Int).SetUint64(s.Balance), s.R)
}
func (s State) Nullifier() *big.Int { return Hash(DomNullifier, s.Task, s.R) }

// Assignment is the full witness for spending amount from s into a state
// with rNew, bound to intent. It refuses an amount of zero (Z2-D4) or above
// the balance.
func Assignment(s State, amount uint64, rNew *big.Int, intent [32]byte) (*Spend, State, error) {
	switch {
	case amount == 0:
		return nil, State{}, errors.New("a zero amount is refused (Z2-D4)")
	case amount > s.Balance:
		return nil, State{}, errors.New("amount above the balance")
	}
	next := State{Task: s.Task, Balance: s.Balance - amount, R: rNew}
	hi, lo := IntentLimbs(intent)
	return &Spend{Task: s.Task, COld: s.Commitment(), Amount: amount, CNew: next.Commitment(), Nullifier: s.Nullifier(), IntentHi: hi, IntentLo: lo,
		BalanceOld: s.Balance, ROld: s.R, RNew: rNew}, next, nil
}
