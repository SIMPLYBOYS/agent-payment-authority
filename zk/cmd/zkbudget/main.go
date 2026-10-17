// Command zkbudget is Z2's host-side tool (docs/commercial-z2.md): the
// setup ceremony, commitments and proofs for the budget contract, and the
// fixtures of the contract's tests. Synthetic data only.
//
//	zkbudget setup -dir D [-n 3]              ceremony; writes D/{pk,vk}.bin, D/transcript.json and D/Verifier.sol
//	zkbudget random                           a full-entropy r below the field modulus (hex)
//	zkbudget commit -chain C -contract A -task T -balance B -r R
//	zkbudget prove -dir D -chain C -contract A -task T -balance B -r R -amount X -rnew R2 -intent H
//	zkbudget fixtures -dir D -out F.sol -deployer A -nonce N
//
// With -witness-stdin, commit and prove read the witness from stdin as
// {"balance": B, "r": R, "rnew": R2} instead (P2: never on the command
// line, where ps and shell history see it); -balance, -r and -rnew are
// then refused.
//
// Outputs are JSON on stdout (fixtures: a Solidity file). No key, balance
// or witness is written anywhere else.
package main

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"math/big"
	"os"
	"path/filepath"
	"strings"

	"github.com/consensys/gnark-crypto/ecc"
	"github.com/consensys/gnark/backend/groth16"
	cs "github.com/consensys/gnark/constraint/bn254"
	"github.com/consensys/gnark/logger"
	"golang.org/x/crypto/sha3"

	"github.com/SIMPLYBOYS/agent-payment-authority/zk"
)

func main() {
	logger.Disable()
	if err := run(os.Args[1:], os.Stdin, os.Stdout); err != nil {
		fmt.Fprintln(os.Stderr, "zkbudget:", err)
		os.Exit(1)
	}
}

func run(args []string, stdin io.Reader, stdout io.Writer) error {
	emit := func(v any) error { return emitTo(stdout, v) }
	if len(args) == 0 {
		return errors.New("usage: zkbudget setup|random|commit|prove|fixtures ...")
	}
	fs := flag.NewFlagSet(args[0], flag.ContinueOnError)
	dir := fs.String("dir", "", "setup directory")
	n := fs.Int("n", 3, "contributions per phase")
	chain := fs.Uint64("chain", 31337, "chain ID")
	contract := fs.String("contract", "", "budget contract address (0x…)")
	task := fs.String("task", "", "task ID")
	balance := fs.Uint64("balance", 0, "balance in base units")
	r := fs.String("r", "", "r (hex)")
	amount := fs.Uint64("amount", 0, "amount in base units")
	rNew := fs.String("rnew", "", "r of the new state (hex)")
	intent := fs.String("intent", "", "intent_hash (0x + 64 hex)")
	out := fs.String("out", "", "fixtures: the Solidity file")
	deployer := fs.String("deployer", "0x7FA9385bE102ac3EAc297483Dd6233D62b3e1496", "fixtures: the test contract")
	nonce := fs.Uint64("nonce", 2, "fixtures: the deployer's nonce of the budget contract")
	wstdin := fs.Bool("witness-stdin", false, "commit, prove: read the witness from stdin as JSON")
	if err := fs.Parse(args[1:]); err != nil {
		return err
	}
	if *wstdin {
		if *balance != 0 || *r != "" || *rNew != "" {
			return errors.New("-witness-stdin: the witness comes from stdin only, not -balance, -r or -rnew")
		}
		var w struct {
			Balance *uint64 `json:"balance"`
			R       string  `json:"r"`
			RNew    string  `json:"rnew"`
		}
		d := json.NewDecoder(io.LimitReader(stdin, 4096))
		d.DisallowUnknownFields()
		if err := d.Decode(&w); err != nil || w.Balance == nil {
			return errors.New("-witness-stdin: stdin must be {\"balance\": B, \"r\": R, \"rnew\": R2}")
		}
		*balance, *r, *rNew = *w.Balance, w.R, w.RNew
	}
	switch args[0] {
	case "setup":
		return setup(*dir, *n)
	case "random":
		v, err := randomField()
		if err != nil {
			return err
		}
		return emit(map[string]string{"r": hexOf(v)})
	case "commit":
		addr, err := address(*contract)
		if err != nil {
			return err
		}
		rv, err := field(*r)
		if err != nil {
			return err
		}
		s := zk.State{Task: zk.TaskField(*chain, addr, *task), Balance: *balance, R: rv}
		return emit(map[string]string{"task_field": s.Task.String(), "commitment": s.Commitment().String()})
	case "prove":
		addr, err := address(*contract)
		if err != nil {
			return err
		}
		rv, err := field(*r)
		if err != nil {
			return err
		}
		rn, err := field(*rNew)
		if err != nil {
			return err
		}
		ih, err := bytes32(*intent)
		if err != nil {
			return err
		}
		k, err := load(*dir)
		if err != nil {
			return err
		}
		s := zk.State{Task: zk.TaskField(*chain, addr, *task), Balance: *balance, R: rv}
		a, _, err := zk.Assignment(s, *amount, rn, ih)
		if err != nil {
			return err
		}
		p, err := zk.Prove(k.ccs, k.pk, k.vk, a)
		if err != nil {
			return err
		}
		return emit(proofJSON(p))
	case "fixtures":
		return fixtures(*dir, *out, *deployer, *nonce)
	}
	return fmt.Errorf("unknown command %q", args[0])
}

func emitTo(w io.Writer, v any) error {
	b, err := json.MarshalIndent(v, "", "  ")
	if err == nil {
		_, err = fmt.Fprintln(w, string(b))
	}
	return err
}

func hexOf(v *big.Int) string { return fmt.Sprintf("0x%064x", v) }

func randomField() (*big.Int, error) { return rand.Int(rand.Reader, zk.Field) }

func field(s string) (*big.Int, error) {
	v, ok := new(big.Int).SetString(strings.TrimPrefix(s, "0x"), 16)
	if !ok || v.Sign() < 0 || v.Cmp(zk.Field) >= 0 {
		return nil, errors.New("a field element in hex, below r")
	}
	return v, nil
}

func bytes32(s string) ([32]byte, error) {
	var out [32]byte
	b, err := hex.DecodeString(strings.TrimPrefix(s, "0x"))
	if err != nil || len(b) != 32 {
		return out, errors.New("32 bytes in hex")
	}
	copy(out[:], b)
	return out, nil
}

func address(s string) ([20]byte, error) {
	var out [20]byte
	b, err := hex.DecodeString(strings.TrimPrefix(s, "0x"))
	if err != nil || len(b) != 20 {
		return out, errors.New("an address: 0x + 40 hex")
	}
	copy(out[:], b)
	return out, nil
}

// proofBytes is the 256-byte proof the verifier takes.
func proofBytes(p zk.Proof) []byte {
	var out []byte
	for _, w := range p.Words {
		out = append(out, w.FillBytes(make([]byte, 32))...)
	}
	return out
}

func proofJSON(p zk.Proof) map[string]any {
	words, inputs := make([]string, 8), make([]string, 7)
	for i, w := range p.Words {
		words[i] = w.String()
	}
	for i, v := range p.Inputs {
		inputs[i] = v.String()
	}
	return map[string]any{"proof": fmt.Sprintf("0x%x", proofBytes(p)), "proof_words": words, "inputs": inputs}
}

func setup(dir string, n int) error {
	if dir == "" {
		return errors.New("setup needs -dir")
	}
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return err
	}
	ccs, err := zk.Compile()
	if err != nil {
		return err
	}
	pk, vk, t, err := zk.Ceremony(ccs, n, []byte("agent-payment-authority/zk/v2 beacon 1 (fixed, not a public beacon)"),
		[]byte("agent-payment-authority/zk/v2 beacon 2 (fixed, not a public beacon)"))
	if err != nil {
		return err
	}
	if err := writeTo(filepath.Join(dir, "pk.bin"), pk); err != nil {
		return err
	}
	if err := writeTo(filepath.Join(dir, "vk.bin"), vk); err != nil {
		return err
	}
	f, err := os.Create(filepath.Join(dir, "Verifier.sol"))
	if err != nil {
		return err
	}
	if err := errors.Join(zk.ExportVerifier(vk, f), f.Close()); err != nil {
		return err
	}
	b, _ := json.MarshalIndent(t, "", "  ")
	if err := os.WriteFile(filepath.Join(dir, "transcript.json"), append(b, '\n'), 0o644); err != nil {
		return err
	}
	return emitTo(os.Stdout, t)
}

func writeTo(path string, v io.WriterTo) error {
	f, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0o600)
	if err != nil {
		return err
	}
	_, err = v.WriteTo(f)
	return errors.Join(err, f.Close())
}

func readFrom(path string, v io.ReaderFrom) error {
	f, err := os.Open(path)
	if err != nil {
		return err
	}
	_, err = v.ReadFrom(f)
	return errors.Join(err, f.Close())
}

type keys struct {
	ccs *cs.R1CS
	pk  groth16.ProvingKey
	vk  groth16.VerifyingKey
}

// load reads a setup directory's keys and recompiles the circuit (the
// compile is deterministic; the keys are checked against each other by
// every proof Prove makes).
func load(dir string) (keys, error) {
	var k keys
	var err error
	if k.ccs, err = zk.Compile(); err != nil {
		return k, err
	}
	k.pk, k.vk = groth16.NewProvingKey(ecc.BN254), groth16.NewVerifyingKey(ecc.BN254)
	if err := readFrom(filepath.Join(dir, "pk.bin"), k.pk); err != nil {
		return k, err
	}
	return k, readFrom(filepath.Join(dir, "vk.bin"), k.vk)
}

// createAddress is CREATE's address for a deployer and a nonce below 128:
// keccak256(rlp([deployer, nonce]))[12:].
func createAddress(deployer [20]byte, nonce uint64) ([20]byte, error) {
	var out [20]byte
	if nonce >= 128 {
		return out, errors.New("nonce below 128 only")
	}
	n := byte(nonce)
	if nonce == 0 {
		n = 0x80
	}
	h := sha3.NewLegacyKeccak256()
	h.Write(append(append([]byte{0xd6, 0x94}, deployer[:]...), n))
	copy(out[:], h.Sum(nil)[12:])
	return out, nil
}

// fixtures writes the contract tests' constants: the budget contract's
// address in the test (from the deployer and nonce), two tasks' first
// commitments, and proofs for spends of them, all from fresh synthetic
// states and the setup in dir. The test asserts the address it deploys to.
func fixtures(dir, out, deployerHex string, nonce uint64) error {
	k, err := load(dir)
	if err != nil {
		return err
	}
	dep, err := address(deployerHex)
	if err != nil {
		return err
	}
	contract, err := createAddress(dep, nonce)
	if err != nil {
		return err
	}
	const chain = 31337
	rs := make([]*big.Int, 8)
	for i := range rs {
		if rs[i], err = randomField(); err != nil {
			return err
		}
	}
	intent := func(label string) [32]byte { return sha256.Sum256([]byte("synthetic intent " + label)) }
	a0 := zk.State{Task: zk.TaskField(chain, contract, "show-a-01"), Balance: 5_000_000, R: rs[0]}
	b0 := zk.State{Task: zk.TaskField(chain, contract, "show-b-01"), Balance: 5_000_000, R: rs[1]}
	var b strings.Builder
	fmt.Fprintf(&b, "// SPDX-License-Identifier: MIT\n// Generated by `zkbudget fixtures` from the setup in zk/setup; synthetic data. Do not edit.\npragma solidity 0.8.37;\n\nlibrary Fixtures {\n")
	fmt.Fprintf(&b, "    uint256 internal constant CHAIN = %d;\n    address internal constant BUDGET = %s;\n", chain, checksum(contract))
	fmt.Fprintf(&b, "    uint256 internal constant C0_A = %s;\n    uint256 internal constant C0_B = %s;\n", a0.Commitment(), b0.Commitment())
	emitSpend := func(name string, a *zk.Spend, label, note string) error {
		p, err := zk.Prove(k.ccs, k.pk, k.vk, a)
		if err != nil {
			return err
		}
		ih := intent(label)
		fmt.Fprintf(&b, "\n    // %s: %s\n", name, note)
		fmt.Fprintf(&b, "    bytes32 internal constant %s_INTENT = 0x%x;\n    uint256 internal constant %s_AMOUNT = %s;\n", name, ih, name, p.Inputs[2])
		fmt.Fprintf(&b, "    uint256 internal constant %s_C_OLD = %s;\n    uint256 internal constant %s_C_NEW = %s;\n    uint256 internal constant %s_N = %s;\n",
			name, p.Inputs[1], name, p.Inputs[3], name, p.Inputs[4])
		fmt.Fprintf(&b, "    bytes internal constant %s_PROOF = hex\"%x\";\n", name, proofBytes(p))
		return nil
	}
	spend := func(name string, s zk.State, amount uint64, r *big.Int, label string) (zk.State, error) {
		a, next, err := zk.Assignment(s, amount, r, intent(label))
		if err != nil {
			return next, err
		}
		return next, emitSpend(name, a, label, fmt.Sprintf("%d from a balance of %d", amount, s.Balance))
	}
	a1, err := spend("A1", a0, 500_000, rs[2], "a1")
	if err != nil {
		return err
	}
	if _, err := spend("A2", a1, 400_000, rs[3], "a2"); err != nil {
		return err
	}
	if _, err := spend("A1B", a0, 100_000, rs[4], "a1b"); err != nil { // a second spend of A's first state
		return err
	}
	if _, err := spend("B1", b0, 300_000, rs[5], "b1"); err != nil {
		return err
	}
	// Review S1: a valid proof from a state the issuer never registered for
	// A (2^64−1 base units, its own fresh nullifier): only the contract's
	// C_old == current check refuses it.
	forged := zk.State{Task: a0.Task, Balance: ^uint64(0), R: rs[6]}
	if _, err := spend("FORGED", forged, 1_000_000_000, rs[7], "forged"); err != nil {
		return err
	}
	// Review S2: the circuit accepts a zero amount (Assignment refuses it, so
	// the witness is built directly); only the contract refuses it (Z2-D4).
	hi, lo := zk.IntentLimbs(intent("zero"))
	same := zk.State{Task: a0.Task, Balance: a0.Balance, R: rs[7]}
	zero := &zk.Spend{Task: a0.Task, COld: a0.Commitment(), Amount: uint64(0), CNew: same.Commitment(), Nullifier: a0.Nullifier(), IntentHi: hi, IntentLo: lo,
		BalanceOld: a0.Balance, ROld: a0.R, RNew: same.R}
	if err := emitSpend("ZERO", zero, "zero", "0 from A's first state (the circuit allows it)"); err != nil {
		return err
	}
	b.WriteString("}\n")
	return os.WriteFile(out, []byte(b.String()), 0o644)
}

// checksum is an address in EIP-55 mixed case, as Solidity requires of a literal.
func checksum(a [20]byte) string {
	lower := hex.EncodeToString(a[:])
	h := sha3.NewLegacyKeccak256()
	h.Write([]byte(lower))
	sum := h.Sum(nil)
	out := []byte(lower)
	for i, c := range out {
		if c >= 'a' && (sum[i/2]>>(4*(1-uint(i%2))))&0xf >= 8 {
			out[i] = c - 32
		}
	}
	return "0x" + string(out)
}
