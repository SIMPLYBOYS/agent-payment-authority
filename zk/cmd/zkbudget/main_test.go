package main

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"
)

const testContract = "0x5FbDB2315678afecb367f032d93F642f64180aa3"

func runOut(t *testing.T, stdin string, args ...string) (map[string]any, error) {
	t.Helper()
	var out bytes.Buffer
	if err := run(args, strings.NewReader(stdin), &out); err != nil {
		return nil, err
	}
	var m map[string]any
	if err := json.Unmarshal(out.Bytes(), &m); err != nil {
		t.Fatal(err)
	}
	return m, nil
}

// The witness from stdin gives what the flags give; mixing the two, or a
// malformed witness, is refused.
func TestWitnessStdin(t *testing.T) {
	r := "0x" + strings.Repeat("0", 63) + "7"
	base := []string{"commit", "-contract", testContract, "-task", "show-a-01"}
	flags, err := runOut(t, "", append(base, "-balance", "5000000", "-r", r)...)
	if err != nil {
		t.Fatal(err)
	}
	stdin, err := runOut(t, `{"balance": 5000000, "r": "`+r+`"}`, append(base, "-witness-stdin")...)
	if err != nil || stdin["commitment"] != flags["commitment"] {
		t.Fatalf("stdin %v, flags %v, %v", stdin, flags, err)
	}
	for name, cs := range map[string]struct {
		stdin string
		args  []string
	}{
		"flags too":     {`{"balance": 5000000, "r": "` + r + `"}`, append(base, "-witness-stdin", "-r", r)},
		"no balance":    {`{"r": "` + r + `"}`, append(base, "-witness-stdin")},
		"another field": {`{"balance": 1, "r": "` + r + `", "x": 1}`, append(base, "-witness-stdin")},
		"not JSON":      {`balance=1`, append(base, "-witness-stdin")},
	} {
		if _, err := runOut(t, cs.stdin, cs.args...); err == nil {
			t.Errorf("%s: accepted", name)
		}
	}
}

// prove with the witness on stdin, against the repository's setup.
func TestProveWitnessStdin(t *testing.T) {
	if testing.Short() {
		t.Skip("a Groth16 proof")
	}
	r, rn := "0x"+strings.Repeat("0", 63)+"7", "0x"+strings.Repeat("0", 63)+"9"
	p, err := runOut(t, `{"balance": 5000000, "r": "`+r+`", "rnew": "`+rn+`"}`, "prove", "-dir", "../../setup", "-contract", testContract,
		"-task", "show-a-01", "-amount", "500000", "-intent", "0x"+strings.Repeat("ab", 32), "-witness-stdin")
	if err != nil || len(p["proof"].(string)) != 2+512 || len(p["inputs"].([]any)) != 7 {
		t.Fatalf("prove %v %v", p, err)
	}
}
