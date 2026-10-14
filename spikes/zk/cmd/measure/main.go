// Command measure compiles the Z1 circuit, runs a local Groth16 setup and
// times proving and verifying a synthetic spend; it prints one JSON report
// (no witness values). Synthetic data only.
//
//	go run ./cmd/measure [-n 10]
package main

import (
	"bytes"
	"crypto/sha256"
	"encoding/json"
	"flag"
	"fmt"
	"math/big"
	"os"
	"runtime"
	"runtime/debug"
	"slices"
	"syscall"
	"time"

	"github.com/consensys/gnark-crypto/ecc"
	"github.com/consensys/gnark/backend/groth16"
	"github.com/consensys/gnark/frontend"
	"github.com/consensys/gnark/frontend/cs/r1cs"
	"github.com/consensys/gnark/logger"

	"github.com/SIMPLYBOYS/agent-payment-authority/spikes/zk"
)

func main() {
	n := flag.Int("n", 10, "proofs to time")
	flag.Parse()
	logger.Disable()
	if err := run(*n); err != nil {
		fmt.Fprintln(os.Stderr, "measure:", err)
		os.Exit(1)
	}
}

func ms(d time.Duration) float64 { return float64(d.Microseconds()) / 1000 }

func run(n int) error {
	rep := map[string]any{"go": runtime.Version(), "goos": runtime.GOOS, "goarch": runtime.GOARCH, "cpus": runtime.NumCPU(), "curve": "BN254", "backend": "groth16",
		"setup_kind": "local single-party groth16.Setup (not a trusted ceremony)"}
	if bi, ok := debug.ReadBuildInfo(); ok {
		for _, d := range bi.Deps {
			if d.Path == "github.com/consensys/gnark" || d.Path == "github.com/consensys/gnark-crypto" {
				rep[d.Path] = d.Version
			}
		}
	}
	t0 := time.Now()
	ccs, err := frontend.Compile(ecc.BN254.ScalarField(), r1cs.NewBuilder, &zk.Spend{})
	if err != nil {
		return err
	}
	rep["compile_ms"] = ms(time.Since(t0))
	rep["constraints"], rep["public_inputs"], rep["secret_inputs"] = ccs.GetNbConstraints(), ccs.GetNbPublicVariables()-1, ccs.GetNbSecretVariables()
	t0 = time.Now()
	pk, vk, err := groth16.Setup(ccs)
	if err != nil {
		return err
	}
	rep["setup_ms"] = ms(time.Since(t0))
	var buf bytes.Buffer
	pk.WriteTo(&buf)
	rep["proving_key_bytes"] = buf.Len()
	buf.Reset()
	vk.WriteTo(&buf)
	rep["verifying_key_bytes"] = buf.Len()

	s := zk.State{Task: zk.TaskField("show-a-01"), Balance: 5_000_000, R: big.NewInt(1001)}
	var proves, verifies []float64
	var ms0, ms1 runtime.MemStats
	runtime.ReadMemStats(&ms0)
	for i := range n {
		a, _, err := zk.Assignment(s, 500_000, big.NewInt(int64(2000+i)), sha256.Sum256(fmt.Append(nil, "synthetic intent ", i)))
		if err != nil {
			return err
		}
		w, err := frontend.NewWitness(a, ecc.BN254.ScalarField())
		if err != nil {
			return err
		}
		pub, _ := w.Public()
		t0 = time.Now()
		p, err := groth16.Prove(ccs, pk, w)
		if err != nil {
			return err
		}
		proves = append(proves, ms(time.Since(t0)))
		t0 = time.Now()
		if err := groth16.Verify(p, vk, pub); err != nil {
			return err
		}
		verifies = append(verifies, ms(time.Since(t0)))
		if i == 0 {
			buf.Reset()
			p.WriteTo(&buf)
			rep["proof_bytes"] = buf.Len()
		}
	}
	runtime.ReadMemStats(&ms1)
	rep["proofs"] = n
	stats := func(x []float64) map[string]float64 {
		s := slices.Sorted(slices.Values(x))
		sum := 0.0
		for _, v := range s {
			sum += v
		}
		return map[string]float64{"min": s[0], "median": s[len(s)/2], "max": s[len(s)-1], "mean": sum / float64(len(s))}
	}
	rep["prove_ms"], rep["verify_ms"] = stats(proves), stats(verifies)
	rep["alloc_per_proof_mb"] = float64(ms1.TotalAlloc-ms0.TotalAlloc) / float64(n) / (1 << 20)
	var ru syscall.Rusage
	if syscall.Getrusage(syscall.RUSAGE_SELF, &ru) == nil {
		rep["max_rss_mb"] = float64(ru.Maxrss) / (1 << 20) // bytes on darwin
	}
	out, _ := json.MarshalIndent(rep, "", "  ")
	fmt.Println(string(out))
	return nil
}
