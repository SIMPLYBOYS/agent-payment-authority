package zk

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"math/big"

	"github.com/consensys/gnark-crypto/ecc"
	"github.com/consensys/gnark/backend/groth16"
	groth16bn254 "github.com/consensys/gnark/backend/groth16/bn254"
	"github.com/consensys/gnark/backend/groth16/bn254/mpcsetup"
	"github.com/consensys/gnark/backend/solidity"
	cs "github.com/consensys/gnark/constraint/bn254"
	"github.com/consensys/gnark/frontend"
	"github.com/consensys/gnark/frontend/cs/r1cs"
)

// Compile is the circuit's R1CS over BN254.
func Compile() (*cs.R1CS, error) {
	ccs, err := frontend.Compile(ecc.BN254.ScalarField(), r1cs.NewBuilder, &Spend{})
	if err != nil {
		return nil, err
	}
	r, ok := ccs.(*cs.R1CS)
	if !ok {
		return nil, errors.New("not a BN254 R1CS")
	}
	return r, nil
}

// Transcript is the ceremony's public record (Z2-D3): what each step
// produced, by hash. The serialized steps themselves are not kept, so a
// third party can check the keys against these hashes but cannot re-verify
// the contributions without them. It holds no secret: each contributor's
// toxic waste lived only in this process's memory.
type Transcript struct {
	Contributors     string   `json:"contributors"`
	Entropy          string   `json:"entropy"`
	Trust            string   `json:"trust"`
	Constraints      int      `json:"constraints"`
	DomainSize       uint64   `json:"domain_size"`
	Phase1           []string `json:"phase1_sha256"`
	Beacon1          string   `json:"beacon1"`
	Phase2           []string `json:"phase2_sha256"`
	Beacon2          string   `json:"beacon2"`
	ProvingKeySHA256 string   `json:"proving_key_sha256"`
	VerifyingKey     string   `json:"verifying_key_sha256"`
}

func digestOf(w io.WriterTo) (string, []byte, error) {
	var b bytes.Buffer
	if _, err := w.WriteTo(&b); err != nil {
		return "", nil, err
	}
	s := sha256.Sum256(b.Bytes())
	return hex.EncodeToString(s[:]), b.Bytes(), nil
}

// Ceremony runs gnark's MPC setup in this process: phase 1 then phase 2,
// n contributions each (all by this host: Z1-D4), each serialized,
// re-read and verified in order before the beacons seal it. Contributions
// draw their randomness from the operating system (crypto/rand).
func Ceremony(ccs *cs.R1CS, n int, beacon1, beacon2 []byte) (groth16.ProvingKey, groth16.VerifyingKey, Transcript, error) {
	t := Transcript{Contributors: fmt.Sprintf("%d contributions per phase, all by this host in one process", n),
		Entropy:     "crypto/rand (the operating system) for each contribution; the beacons are fixed strings, not a public randomness beacon",
		Trust:       "信任等同信任主持人 (Z1-D4): whoever ran this process could have kept the toxic waste and forge proofs",
		Constraints: ccs.GetNbConstraints(), DomainSize: ecc.NextPowerOfTwo(uint64(ccs.GetNbConstraints())),
		Beacon1: string(beacon1), Beacon2: string(beacon2)}
	reread := func(raw []byte, v io.ReaderFrom) error {
		m, err := v.ReadFrom(bytes.NewReader(raw))
		if err == nil && int(m) != len(raw) {
			err = errors.New("short read")
		}
		return err
	}
	var p1 mpcsetup.Phase1
	p1.Initialize(t.DomainSize)
	phase1 := make([]*mpcsetup.Phase1, n)
	for i := range n {
		p1.Contribute()
		d, raw, err := digestOf(&p1)
		if err != nil {
			return nil, nil, t, err
		}
		t.Phase1 = append(t.Phase1, d)
		phase1[i] = new(mpcsetup.Phase1)
		if err := reread(raw, phase1[i]); err != nil {
			return nil, nil, t, err
		}
	}
	commons, err := mpcsetup.VerifyPhase1(t.DomainSize, beacon1, phase1...)
	if err != nil {
		return nil, nil, t, fmt.Errorf("phase 1: %w", err)
	}
	var p2 mpcsetup.Phase2
	p2.Initialize(ccs, &commons)
	phase2 := make([]*mpcsetup.Phase2, n)
	for i := range n {
		p2.Contribute()
		d, raw, err := digestOf(&p2)
		if err != nil {
			return nil, nil, t, err
		}
		t.Phase2 = append(t.Phase2, d)
		phase2[i] = new(mpcsetup.Phase2)
		if err := reread(raw, phase2[i]); err != nil {
			return nil, nil, t, err
		}
	}
	pk, vk, err := mpcsetup.VerifyPhase2(ccs, &commons, beacon2, phase2...)
	if err != nil {
		return nil, nil, t, fmt.Errorf("phase 2: %w", err)
	}
	if t.ProvingKeySHA256, _, err = digestOf(pk); err != nil {
		return nil, nil, t, err
	}
	if t.VerifyingKey, _, err = digestOf(vk); err != nil {
		return nil, nil, t, err
	}
	return pk, vk, t, nil
}

// ExportVerifier writes the Solidity verifier for vk (gnark's template; it
// refuses public inputs not below r).
func ExportVerifier(vk groth16.VerifyingKey, w io.Writer) error {
	return vk.ExportSolidity(w, solidity.WithPragmaVersion("0.8.37"))
}

// Proof is one spend as the contract takes it.
type Proof struct {
	Words  [8]*big.Int // A, B, C in the verifier's order
	Inputs [7]*big.Int // task, C_old, amount, C_new, N, intent_hi, intent_lo
}

// Prove proves a, checks it against vk, and returns it in calldata form.
func Prove(ccs *cs.R1CS, pk groth16.ProvingKey, vk groth16.VerifyingKey, a *Spend) (Proof, error) {
	var out Proof
	w, err := frontend.NewWitness(a, ecc.BN254.ScalarField())
	if err != nil {
		return out, err
	}
	pub, err := w.Public()
	if err != nil {
		return out, err
	}
	p, err := groth16.Prove(ccs, pk, w)
	if err != nil {
		return out, err
	}
	if err := groth16.Verify(p, vk, pub); err != nil {
		return out, err
	}
	raw := p.(*groth16bn254.Proof).MarshalSolidity()
	if len(raw) != 256 {
		return out, fmt.Errorf("proof of %d bytes", len(raw))
	}
	for i := range out.Words {
		out.Words[i] = new(big.Int).SetBytes(raw[32*i : 32*i+32])
	}
	vals := []frontend.Variable{a.Task, a.COld, a.Amount, a.CNew, a.Nullifier, a.IntentHi, a.IntentLo}
	for i, v := range vals {
		out.Inputs[i], err = toBig(v)
		if err != nil {
			return out, err
		}
	}
	return out, nil
}

func toBig(v frontend.Variable) (*big.Int, error) {
	switch x := v.(type) {
	case *big.Int:
		return new(big.Int).Set(x), nil
	case uint64:
		return new(big.Int).SetUint64(x), nil
	case int:
		return big.NewInt(int64(x)), nil
	}
	return nil, fmt.Errorf("public input of type %T", v)
}
