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

// Ledger models tx#1 (docs/commercial-demo-roadmap.md table row 7). It
// takes only the public values the caller submits, refuses any outside
// [0, r) (the Go verifier reduces them mod r, so value+r would verify:
// review M1), verifies the proof against exactly those values, and makes
// its decisions on the same canonical values: the task's current
// commitment, an unused nullifier, then C_new is stored. A model in Go, not
// a contract.
type Ledger struct {
	vk     groth16.VerifyingKey
	commit map[string]string // task -> C (decimal, canonical)
	used   map[string]bool
}

var (
	errNonCanonical = errors.New("PUBLIC_INPUT_NOT_CANONICAL")
	errProof        = errors.New("PROOF_INVALID")
	errStale        = errors.New("STALE_COMMITMENT")
	errNullifier    = errors.New("NULLIFIER_USED")
)

// Public is what a spend submits on chain: the seven public inputs only.
type Public struct{ Task, COld, Amount, CNew, Nullifier, IntentHi, IntentLo *big.Int }

func publicFrom(a *Spend) Public {
	b := func(v frontend.Variable) *big.Int {
		switch x := v.(type) {
		case *big.Int:
			return new(big.Int).Set(x)
		case int:
			return big.NewInt(int64(x))
		case uint64:
			return new(big.Int).SetUint64(x)
		}
		panic(fmt.Sprintf("public input of type %T", v))
	}
	return Public{b(a.Task), b(a.COld), b(a.Amount), b(a.CNew), b(a.Nullifier), b(a.IntentHi), b(a.IntentLo)}
}

func (l *Ledger) Apply(p groth16.Proof, in Public) error {
	vals := []*big.Int{in.Task, in.COld, in.Amount, in.CNew, in.Nullifier, in.IntentHi, in.IntentLo}
	for _, v := range vals {
		if v == nil || v.Sign() < 0 || v.Cmp(Field) >= 0 {
			return errNonCanonical
		}
	}
	w, err := frontend.NewWitness(&Spend{Task: in.Task, COld: in.COld, Amount: in.Amount, CNew: in.CNew, Nullifier: in.Nullifier, IntentHi: in.IntentHi, IntentLo: in.IntentLo},
		ecc.BN254.ScalarField(), frontend.PublicOnly())
	if err != nil {
		return errProof
	}
	if err := groth16.Verify(p, l.vk, w); err != nil {
		return errProof
	}
	task, n := in.Task.String(), in.Nullifier.String()
	switch {
	case l.commit[task] != in.COld.String():
		return errStale
	case l.used[n]:
		return errNullifier
	}
	l.used[n], l.commit[task] = true, in.CNew.String()
	return nil
}

func TestLedgerReplayAndStale(t *testing.T) {
	k := compiled(t)
	l := &Ledger{vk: k.vk, commit: map[string]string{taskA.String(): start.Commitment().String()}, used: map[string]bool{}}
	a1, s1, _ := Assignment(start, 500_000, r1, intent1)
	p1, _, err := prove(t, k, a1)
	if err != nil {
		t.Fatal(err)
	}
	in1 := publicFrom(a1)
	if err := l.Apply(p1, in1); err != nil {
		t.Fatalf("first spend: %v", err)
	}
	if err := l.Apply(p1, in1); !errors.Is(err, errStale) {
		t.Fatalf("replay: %v", err)
	}
	// A second proof from the same old state (another intent): its C_old is stale.
	a1b, _, _ := Assignment(start, 100_000, r2, intent2)
	p1b, _, _ := prove(t, k, a1b)
	if err := l.Apply(p1b, publicFrom(a1b)); !errors.Is(err, errStale) {
		t.Fatalf("a second spend of the old state: %v", err)
	}
	// Review M1's scenario on a fresh ledger: the proof's own C_old and
	// nullifier, only C_new changed. It is refused at the proof and 42 is
	// never stored.
	fresh := &Ledger{vk: k.vk, commit: map[string]string{taskA.String(): start.Commitment().String()}, used: map[string]bool{}}
	forged := in1
	forged.CNew = big.NewInt(42)
	if err := fresh.Apply(p1, forged); !errors.Is(err, errProof) || fresh.commit[taskA.String()] != start.Commitment().String() {
		t.Fatalf("a C_new the proof does not hold: %v (stored %s)", err, fresh.commit[taskA.String()])
	}
	// The next spend from the new state is accepted.
	a2, s2, _ := Assignment(s1, 400_000, r2, intent2)
	p2, _, _ := prove(t, k, a2)
	if err := l.Apply(p2, publicFrom(a2)); err != nil {
		t.Fatalf("second spend: %v", err)
	}
	// Task B registered with A's current commitment, on purpose, so the
	// stale check passes: A's proof submitted as B's is stopped only by the
	// proof (review S3).
	l.commit[taskB.String()] = s2.Commitment().String()
	a3, _, _ := Assignment(s2, 1, r0, intent1)
	p3, _, _ := prove(t, k, a3)
	asB := publicFrom(a3)
	asB.Task = taskB
	if err := l.Apply(p3, asB); !errors.Is(err, errProof) {
		t.Fatalf("A's proof as task B: %v", err)
	}
	// An unregistered task has no commitment to spend.
	c := State{Task: TaskField("show-c-01"), Balance: 10, R: r0}
	ac, _, _ := Assignment(c, 1, r1, intent1)
	pc, _, _ := prove(t, k, ac)
	if err := l.Apply(pc, publicFrom(ac)); !errors.Is(err, errStale) {
		t.Fatalf("an unregistered task: %v", err)
	}
	// The nullifier check alone: A's first commitment reinstated, the
	// nullifier used.
	l.commit[taskA.String()] = start.Commitment().String()
	if err := l.Apply(p1, in1); !errors.Is(err, errNullifier) {
		t.Fatalf("a used nullifier: %v", err)
	}
	// The same nullifier written as N + r: the Go verifier would accept it
	// (TestAliasedPublicInputs); the ledger refuses it as non-canonical.
	aliased := in1
	aliased.Nullifier = new(big.Int).Add(in1.Nullifier, Field)
	if err := l.Apply(p1, aliased); !errors.Is(err, errNonCanonical) {
		t.Fatalf("an aliased nullifier: %v", err)
	}
}

// Review M1: gnark's Go verifier reduces public inputs mod r, so a valid
// proof also verifies with value + r (and a zero amount with amount = r,
// the boundary). Recorded here so Z2's verifier and
// contract refuse inputs outside [0, r) themselves.
func TestAliasedPublicInputs(t *testing.T) {
	k := compiled(t)
	a, _, _ := Assignment(start, 500_000, r1, intent1)
	p, _, err := prove(t, k, a)
	if err != nil {
		t.Fatal(err)
	}
	zero, _, _ := Assignment(start, 0, r1, intent1) // section 8 item 10: a zero amount is allowed
	pz, _, err := prove(t, k, zero)
	if err != nil {
		t.Fatal(err)
	}
	in := publicFrom(a)
	proofs := map[string]groth16.Proof{"amount = r (a zero-amount proof)": pz}
	inputs := map[string]Public{"amount = r (a zero-amount proof)": publicFrom(zero)}
	for name, mut := range map[string]func(x *Public){
		"amount = r (a zero-amount proof)": func(x *Public) { x.Amount = new(big.Int).Set(Field) },
		"amount + r":                       func(x *Public) { x.Amount = new(big.Int).Add(x.Amount, Field) },
		"nullifier + r":                    func(x *Public) { x.Nullifier = new(big.Int).Add(x.Nullifier, Field) },
	} {
		x, pr := in, p
		if v, ok := inputs[name]; ok {
			x, pr = v, proofs[name]
		}
		mut(&x)
		w, err := frontend.NewWitness(&Spend{Task: x.Task, COld: x.COld, Amount: x.Amount, CNew: x.CNew, Nullifier: x.Nullifier, IntentHi: x.IntentHi, IntentLo: x.IntentLo},
			ecc.BN254.ScalarField(), frontend.PublicOnly())
		if err != nil {
			t.Fatal(err)
		}
		if err := groth16.Verify(pr, k.vk, w); err != nil {
			t.Errorf("%s: the library no longer reduces mod r (%v); update the report", name, err)
		}
		l := &Ledger{vk: k.vk, commit: map[string]string{taskA.String(): start.Commitment().String()}, used: map[string]bool{}}
		if err := l.Apply(pr, x); !errors.Is(err, errNonCanonical) {
			t.Errorf("%s: the ledger took it: %v", name, err)
		}
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
