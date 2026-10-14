package zk

import (
	"crypto/sha256"
	"math/big"
	"sync"
	"testing"

	"github.com/consensys/gnark-crypto/ecc"
	"github.com/consensys/gnark/backend/groth16"
	"github.com/consensys/gnark/backend/witness"
	"github.com/consensys/gnark/constraint"
	"github.com/consensys/gnark/frontend"
	"github.com/consensys/gnark/logger"
)

func init() { logger.Disable() }

// Fixed synthetic inputs (no real balance, wallet or identity). Groth16's
// setup and proofs draw their own randomness; the witnesses are fixed.
var (
	budget  = [20]byte{0x2e, 0x23, 0x4d, 0xae} // any address: the task field only has to be this deployment's
	taskA   = TaskField(31337, budget, "show-a-01")
	taskB   = TaskField(31337, budget, "show-b-01")
	intent1 = sha256.Sum256([]byte("synthetic intent 1"))
	intent2 = sha256.Sum256([]byte("synthetic intent 2"))
	r0      = big.NewInt(1001)
	r1      = big.NewInt(1002)
	r2      = big.NewInt(1003)
	start   = State{Task: taskA, Balance: 5_000_000, R: r0} // 5.00 test USDC in base units
)

type keys struct {
	ccs constraint.ConstraintSystem
	pk  groth16.ProvingKey
	vk  groth16.VerifyingKey
}

var (
	once  sync.Once
	setup keys
	serr  error
)

// compiled is the circuit and a local single-party setup, made once.
func compiled(t testing.TB) keys {
	t.Helper()
	once.Do(func() {
		setup.ccs, serr = Compile()
		if serr == nil {
			setup.pk, setup.vk, serr = groth16.Setup(setup.ccs)
		}
	})
	if serr != nil {
		t.Fatal(serr)
	}
	return setup
}

func prove(t testing.TB, k keys, a *Spend) (groth16.Proof, witness.Witness, error) {
	t.Helper()
	w, err := frontend.NewWitness(a, ecc.BN254.ScalarField())
	if err != nil {
		t.Fatalf("witness: %v", err) // a refusal must come from the prover, never from building the witness (review S1)
	}
	pub, err := w.Public()
	if err != nil {
		t.Fatal(err)
	}
	p, err := groth16.Prove(k.ccs, k.pk, w)
	return p, pub, err
}

func publicOf(t testing.TB, a *Spend) witness.Witness {
	t.Helper()
	w, err := frontend.NewWitness(a, ecc.BN254.ScalarField(), frontend.PublicOnly())
	if err != nil {
		t.Fatal(err)
	}
	return w
}

func TestSpendVerifies(t *testing.T) {
	k := compiled(t)
	a, next, err := Assignment(start, 500_000, r1, intent1)
	if err != nil {
		t.Fatal(err)
	}
	p, pub, err := prove(t, k, a)
	if err != nil {
		t.Fatal(err)
	}
	if err := groth16.Verify(p, k.vk, pub); err != nil {
		t.Fatalf("a valid spend: %v", err)
	}
	if next.Balance != 4_500_000 {
		t.Fatalf("balance %d", next.Balance)
	}
	// The whole balance may be spent: balance_new = 0 is in range.
	all, _, err := Assignment(start, start.Balance, r1, intent1)
	if err != nil {
		t.Fatal(err)
	}
	if p, pub, err := prove(t, k, all); err != nil || groth16.Verify(p, k.vk, pub) != nil {
		t.Fatalf("the whole balance: %v", err)
	}
}

// Each witness breaks one statement; the prover cannot make a proof.
func TestProverRefuses(t *testing.T) {
	k := compiled(t)
	good, _, _ := Assignment(start, 500_000, r1, intent1)
	over := State{Task: taskA, Balance: 300_000, R: r0}
	for name, mut := range map[string]func(a *Spend){
		"over budget (balance_new wraps)": func(a *Spend) {
			a.BalanceOld, a.Amount, a.COld, a.Nullifier = over.Balance, 500_000, over.Commitment(), over.Nullifier()
			wrapped := new(big.Int).Mod(big.NewInt(300_000-500_000), Field)
			a.CNew = Hash(DomCommit, taskA, wrapped, r1)
		},
		"wrong r_old (opening and nullifier both fail)": func(a *Spend) { a.ROld = r2 },
		// Only the opening fails: the prover claims more than C_old holds,
		// with the right r_old and nullifier and a consistent C_new.
		"a balance above the committed one": func(a *Spend) {
			a.BalanceOld, a.CNew = 9_000_000, Hash(DomCommit, taskA, big.NewInt(8_500_000), r1)
		},
		"C_new not the rest": func(a *Spend) {
			a.CNew = Hash(DomCommit, taskA, big.NewInt(4_600_000), r1)
		},
		"another task's nullifier":               func(a *Spend) { a.Nullifier = Hash(DomNullifier, taskB, r0) },
		"amount of 2^64 (balance_new wraps too)": func(a *Spend) { a.Amount = new(big.Int).Lsh(big.NewInt(1), 64) },
		// Review S2: each of these fails one constraint only.
		"a committed balance of 2^64": func(a *Spend) { // balance_new = 2^63 stays in range
			big64, big63 := new(big.Int).Lsh(big.NewInt(1), 64), new(big.Int).Lsh(big.NewInt(1), 63)
			a.BalanceOld, a.Amount = big64, big63
			a.COld, a.CNew = Hash(DomCommit, taskA, big64, r0), Hash(DomCommit, taskA, big63, r1)
		},
		"intent lo limb of 2^128": func(a *Spend) { a.IntentLo = new(big.Int).Lsh(big.NewInt(1), 128) },
		"a commitment of another task": func(a *Spend) { // the opening binds the task; the nullifier is still A's
			a.COld = Hash(DomCommit, taskB, big.NewInt(5_000_000), r0)
		},
		// −1000 in the field: balance_new = balance_old + 1000 stays in
		// range, so only the amount's own range check refuses it.
		"a negative amount (the balance grows)": func(a *Spend) {
			a.Amount = new(big.Int).Sub(Field, big.NewInt(1000))
			a.CNew = Hash(DomCommit, taskA, big.NewInt(5_001_000), r1)
		},
		"intent hi limb of 2^128": func(a *Spend) { a.IntentHi = new(big.Int).Lsh(big.NewInt(1), 128) },
	} {
		t.Run(name, func(t *testing.T) {
			a := *good
			mut(&a)
			if _, _, err := prove(t, k, &a); err == nil {
				t.Fatalf("%s: a proof was made", name)
			}
		})
	}
	if _, _, err := Assignment(over, 500_000, r1, intent1); err == nil {
		t.Fatal("the native side assigned an over-budget spend")
	}
}

// A valid proof checked against changed public inputs is refused.
func TestTamperedPublicInputs(t *testing.T) {
	k := compiled(t)
	good, _, _ := Assignment(start, 500_000, r1, intent1)
	p, _, err := prove(t, k, good)
	if err != nil {
		t.Fatal(err)
	}
	hi2, lo2 := IntentLimbs(intent2)
	stale := State{Task: taskA, Balance: 4_000_000, R: r0}
	for name, mut := range map[string]func(a *Spend){
		"another amount":           func(a *Spend) { a.Amount = 400_000 },
		"another task":             func(a *Spend) { a.Task = taskB },
		"a stale C_old":            func(a *Spend) { a.COld = stale.Commitment() },
		"another C_new":            func(a *Spend) { a.CNew = Hash(DomCommit, taskA, big.NewInt(4_500_000), r2) },
		"another nullifier":        func(a *Spend) { a.Nullifier = Hash(DomNullifier, taskA, r2) },
		"another intent (hi limb)": func(a *Spend) { a.IntentHi = hi2 },
		"another intent (lo limb)": func(a *Spend) { a.IntentLo = lo2 },
	} {
		t.Run(name, func(t *testing.T) {
			a := *good
			mut(&a)
			if err := groth16.Verify(p, k.vk, publicOf(t, &a)); err == nil {
				t.Fatalf("%s: verified", name)
			}
		})
	}
}

// Z2-D4: a zero amount is refused natively (the contract refuses it too).
func TestZeroAmount(t *testing.T) {
	if _, _, err := Assignment(start, 0, r1, intent1); err == nil {
		t.Fatal("a zero amount was assigned")
	}
}

// Z2-D2: the task field is one deployment's: another chain, contract or
// task gives another field, each below 2^248.
func TestTaskFieldBindsTheDeployment(t *testing.T) {
	other := budget
	other[19] = 1
	for name, f := range map[string]*big.Int{
		"another chain":    TaskField(1, budget, "show-a-01"),
		"another contract": TaskField(31337, other, "show-a-01"),
		"another task":     TaskField(31337, budget, "show-a-02"),
	} {
		if f.Cmp(taskA) == 0 || f.BitLen() > 248 {
			t.Errorf("%s: %v", name, f)
		}
	}
}

// Z2-D3: the ceremony verifies every contribution in order and its keys
// prove and verify a spend; the transcript records each step by hash.
func TestCeremony(t *testing.T) {
	ccs, err := Compile()
	if err != nil {
		t.Fatal(err)
	}
	pk, vk, tr, err := Ceremony(ccs, 2, []byte("test beacon 1"), []byte("test beacon 2"))
	if err != nil {
		t.Fatal(err)
	}
	if len(tr.Phase1) != 2 || len(tr.Phase2) != 2 || tr.ProvingKeySHA256 == "" || tr.VerifyingKey == "" || tr.Constraints != ccs.GetNbConstraints() {
		t.Fatalf("transcript %+v", tr)
	}
	a, _, _ := Assignment(start, 500_000, r1, intent1)
	p, err := Prove(ccs, pk, vk, a)
	if err != nil {
		t.Fatal(err)
	}
	if p.Inputs[0].Cmp(taskA) != 0 || p.Inputs[2].Uint64() != 500_000 || p.Words[0].Sign() == 0 {
		t.Fatalf("calldata %+v", p.Inputs)
	}
	// A proof of another setup does not verify under this one's key.
	k := compiled(t)
	if _, err := Prove(ccs, k.pk, vk, a); err == nil {
		t.Fatal("a proof from another setup verified")
	}
}
