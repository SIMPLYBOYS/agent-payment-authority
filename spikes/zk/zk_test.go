package zk

import (
	"crypto/sha256"
	"errors"
	"fmt"
	"math/big"
	"sync"
	"testing"

	"github.com/consensys/gnark-crypto/ecc"
	"github.com/consensys/gnark/backend/groth16"
	"github.com/consensys/gnark/backend/witness"
	"github.com/consensys/gnark/constraint"
	"github.com/consensys/gnark/frontend"
	"github.com/consensys/gnark/frontend/cs/r1cs"
	"github.com/consensys/gnark/logger"
)

func init() { logger.Disable() }

// Fixed synthetic inputs (no real balance, wallet or identity). Groth16's
// setup and proofs draw their own randomness; the witnesses are fixed.
var (
	taskA   = TaskField("show-a-01")
	taskB   = TaskField("show-b-01")
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
		setup.ccs, serr = frontend.Compile(ecc.BN254.ScalarField(), r1cs.NewBuilder, &Spend{})
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
		return nil, nil, err
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
	all, _, _ := Assignment(start, start.Balance, r1, intent1)
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
		"wrong r_old (the commitment does not open)": func(a *Spend) { a.ROld = r2 },
		// Only the opening fails: the prover claims more than C_old holds,
		// with the right r_old and nullifier and a consistent C_new.
		"a balance above the committed one": func(a *Spend) {
			a.BalanceOld, a.CNew = 9_000_000, Hash(DomCommit, taskA, big.NewInt(8_500_000), r1)
		},
		"C_new not the rest": func(a *Spend) {
			a.CNew = Hash(DomCommit, taskA, big.NewInt(4_600_000), r1)
		},
		"another task's nullifier": func(a *Spend) { a.Nullifier = Hash(DomNullifier, taskB, r0) },
		"amount of 2^64":           func(a *Spend) { a.Amount = new(big.Int).Lsh(big.NewInt(1), 64) },
		// −1000 in the field: balance_new = balance_old + 1000 stays in
		// range, so only the amount's own range check refuses it.
		"a negative amount (the balance grows)": func(a *Spend) {
			a.Amount = new(big.Int).Sub(Field, big.NewInt(1000))
			a.CNew = Hash(DomCommit, taskA, big.NewInt(5_001_000), r1)
		},
		"intent limb of 2^128": func(a *Spend) { a.IntentHi = new(big.Int).Lsh(big.NewInt(1), 128) },
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

// Ledger models tx#1 (docs/commercial-demo-roadmap.md table row 7): verify
// the proof, require the task's current commitment and an unused
// nullifier, then store C_new. A model in Go, not a contract.
type Ledger struct {
	vk     groth16.VerifyingKey
	commit map[string]*big.Int
	used   map[string]bool
}

var (
	errProof     = errors.New("PROOF_INVALID")
	errStale     = errors.New("STALE_COMMITMENT")
	errNullifier = errors.New("NULLIFIER_USED")
)

func (l *Ledger) Apply(p groth16.Proof, a *Spend, pub witness.Witness) error {
	if err := groth16.Verify(p, l.vk, pub); err != nil {
		return errProof
	}
	task, cOld, n := fmt.Sprint(a.Task), fmt.Sprint(a.COld), fmt.Sprint(a.Nullifier)
	switch {
	case l.commit[task] == nil || l.commit[task].String() != cOld:
		return errStale
	case l.used[n]:
		return errNullifier
	}
	l.used[n], l.commit[task] = true, a.CNew.(*big.Int)
	return nil
}

func TestLedgerReplayAndStale(t *testing.T) {
	k := compiled(t)
	l := &Ledger{vk: k.vk, commit: map[string]*big.Int{fmt.Sprint(taskA): start.Commitment()}, used: map[string]bool{}}
	a1, s1, _ := Assignment(start, 500_000, r1, intent1)
	p1, pub1, err := prove(t, k, a1)
	if err != nil {
		t.Fatal(err)
	}
	if err := l.Apply(p1, a1, pub1); err != nil {
		t.Fatalf("first spend: %v", err)
	}
	if err := l.Apply(p1, a1, pub1); !errors.Is(err, errStale) {
		t.Fatalf("replay: %v", err)
	}
	// A second proof from the same old state (another intent): its C_old is stale.
	a1b, _, _ := Assignment(start, 100_000, r2, intent2)
	p1b, pub1b, _ := prove(t, k, a1b)
	if err := l.Apply(p1b, a1b, pub1b); !errors.Is(err, errStale) {
		t.Fatalf("a second spend of the old state: %v", err)
	}
	// The next spend from the new state is accepted.
	a2, _, _ := Assignment(s1, 400_000, r2, intent2)
	p2, pub2, _ := prove(t, k, a2)
	if err := l.Apply(p2, a2, pub2); err != nil {
		t.Fatalf("second spend: %v", err)
	}
	// Task B has no commitment here: A's state cannot be spent as B's.
	b := State{Task: taskB, Balance: s1.Balance, R: r2}
	ab, _, _ := Assignment(b, 1, r0, intent1)
	pb, pubb, _ := prove(t, k, ab)
	if err := l.Apply(pb, ab, pubb); !errors.Is(err, errStale) {
		t.Fatalf("another task: %v", err)
	}
	// The nullifier check alone: same commitment reinstated, nullifier used.
	l.commit[fmt.Sprint(taskA)] = start.Commitment()
	if err := l.Apply(p1, a1, pub1); !errors.Is(err, errNullifier) {
		t.Fatalf("a used nullifier: %v", err)
	}
}

// IntentLimbs is injective and each limb is below r; the native and
// circuit hashes agree (the positive proof above depends on it).
func TestIntentLimbs(t *testing.T) {
	var max [32]byte
	for i := range max {
		max[i] = 0xff
	}
	hi, lo := IntentLimbs(max)
	if hi.BitLen() != 128 || lo.BitLen() != 128 || hi.Cmp(Field) >= 0 {
		t.Fatal("limbs")
	}
	a, b := IntentLimbs(intent1)
	c, d := IntentLimbs(intent2)
	if a.Cmp(c) == 0 && b.Cmp(d) == 0 {
		t.Fatal("two intents, one pair of limbs")
	}
	if TaskField("show-a-01").Cmp(TaskField("show-a-02")) == 0 || TaskField("x").BitLen() > 248 {
		t.Fatal("task field")
	}
}
