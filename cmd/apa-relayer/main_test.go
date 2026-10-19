package main

import (
	"io"
	"os"
	"testing"
)

func TestUsage(t *testing.T) {
	noTTY := func() (*os.File, error) { t.Fatal("asked for a terminal"); return nil, nil }
	for _, args := range [][]string{nil, {"serve"}, {"keygen", "--dir", "d"}, {"keygen", "--dir", "d", "--role", "wallet-a"}, {"keygen", "--role", "shop"}} {
		if err := run(args, io.Discard, noTTY); err == nil {
			t.Fatalf("%v accepted", args)
		}
	}
}
