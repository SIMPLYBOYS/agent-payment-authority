// Package zk is the Z1 spike (docs/commercial-z1.md): a gnark Groth16
// circuit on BN254 for one budget spend of a task. It proves, without
// showing the balance, that
//
//   - the old commitment opens: C_old = H(commit, task, balance_old, r_old);
//   - the amount fits the balance: balance_new = balance_old − amount, with
//     balance_old, amount and balance_new each in [0, 2^64) (no wrap-around);
//   - the new commitment holds the rest: C_new = H(commit, task, balance_new, r_new);
//   - the nullifier is the old state's: N = H(nullifier, task, r_old);
//   - intent_hash is bound as two 128-bit limbs (hi, lo), each range-checked.
//
// Public inputs, in order: task, C_old, amount, C_new, N, intent_hi,
// intent_lo. H is MiMC over the BN254 scalar field (gnark's std/hash/mimc
// and gnark-crypto's native one agree). Synthetic data only: no real
// balance, wallet, key or identity; setup is a local single-party Groth16
// setup, not a trusted ceremony. Not wired to Policy, Signer, Store or a
// chain.
package zk

import (
	"crypto/sha256"
	"errors"
	"math/big"

	"github.com/consensys/gnark-crypto/ecc"
	"github.com/consensys/gnark-crypto/ecc/bn254/fr"
	nmimc "github.com/consensys/gnark-crypto/ecc/bn254/fr/mimc"
	"github.com/consensys/gnark/frontend"
	"github.com/consensys/gnark/std/hash/mimc"
)

// Field is the BN254 scalar field modulus r (about 2^253.6).
var Field = ecc.BN254.ScalarField()

// domain derives a separation constant from a label (reduced mod r; it
// only has to differ between uses, not be injective).
func domain(label string) *big.Int {
	s := sha256.Sum256([]byte("agent-payment-authority/zk/v1/" + label))
	return new(big.Int).Mod(new(big.Int).SetBytes(s[:]), Field)
}

var (
	DomCommit    = domain("commit")
	DomNullifier = domain("nullifier")
)

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
	// The limbs take part in a constraint: a public input in no constraint
	// would have a zero verifying-key term and any value would verify.
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
		h.Write(b[:])
	}
	return new(big.Int).SetBytes(h.Sum(nil))
}

// TaskField maps a task ID to a field element: the first 31 bytes of
// SHA-256(domain ‖ task) (248 bits, below r). Collisions cost about 2^124.
func TaskField(task string) *big.Int {
	s := sha256.Sum256([]byte("agent-payment-authority/zk/v1/task\x00" + task))
	return new(big.Int).SetBytes(s[:31])
}

// IntentLimbs splits a 32-byte intent hash into hi and lo 128-bit limbs
// (option A of docs/commercial-z1.md section 4): injective, each below r.
func IntentLimbs(h [32]byte) (hi, lo *big.Int) {
	return new(big.Int).SetBytes(h[:16]), new(big.Int).SetBytes(h[16:])
}

// State is a task's private budget state and its commitment.
type State struct {
	Task    *big.Int
	Balance uint64
	R       *big.Int
}

func (s State) Commitment() *big.Int {
	return Hash(DomCommit, s.Task, new(big.Int).SetUint64(s.Balance), s.R)
}
func (s State) Nullifier() *big.Int { return Hash(DomNullifier, s.Task, s.R) }

// Assignment is the full witness for spending amount from s into a state
// with rNew, bound to intent. It refuses an amount above the balance (a
// prover holding the witness cannot make it hold anyway).
func Assignment(s State, amount uint64, rNew *big.Int, intent [32]byte) (*Spend, State, error) {
	if amount > s.Balance {
		return nil, State{}, errors.New("amount above the balance")
	}
	next := State{Task: s.Task, Balance: s.Balance - amount, R: rNew}
	hi, lo := IntentLimbs(intent)
	return &Spend{Task: s.Task, COld: s.Commitment(), Amount: amount, CNew: next.Commitment(), Nullifier: s.Nullifier(), IntentHi: hi, IntentLo: lo,
		BalanceOld: s.Balance, ROld: s.R, RNew: rNew}, next, nil
}
