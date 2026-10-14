package main

import (
	"io"
	"path/filepath"
	"slices"
	"testing"

	"github.com/SIMPLYBOYS/agent-payment-authority/internal/credential"
)

const now = 1791900000

func TestIssuerCommands(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "cred")
	at := func() int64 { return now }
	if err := run([]string{"init", "--dir", dir, "--issuer", "issuer-demo", "--consumers", "consumer-a,consumer-b"}, io.Discard, at); err != nil {
		t.Fatal(err)
	}
	if err := run([]string{"init", "--dir", dir, "--issuer", "issuer-demo", "--consumers", "consumer-a"}, io.Discard, at); err == nil {
		t.Fatal("re-initialised an existing directory")
	}
	d, err := credential.OpenDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	if _, ok, err := d.IssueTask("show-a-01", "consumer-a", now+3600); !ok || err != nil {
		t.Fatal(err)
	}
	for _, args := range [][]string{
		{"revoke", "--dir", dir, "--task", "show-a-01"},
		{"revoke", "--dir", dir, "--status", "7"},
		{"refresh", "--dir", dir},
	} {
		if err := run(args, io.Discard, at); err != nil {
			t.Fatalf("%v: %v", args, err)
		}
	}
	for _, args := range [][]string{
		{"revoke", "--dir", dir},
		{"revoke", "--dir", dir, "--task", "show-a-01", "--status", "1"},
		{"revoke", "--dir", dir, "--task", "show-z-09"},
		{"rotate", "--dir", dir},
		{"refresh"},
	} {
		if err := run(args, io.Discard, at); err == nil {
			t.Errorf("%v was accepted", args)
		}
	}
	fp, _ := d.StatusKeyFingerprint()
	l, _, err := d.ReadStatus("issuer-demo", fp, now)
	if err != nil || !slices.Equal(l.Revoked, []string{"0", "7"}) || l.ListVersion != 4 {
		t.Fatalf("list %+v %v", l, err)
	}
}
