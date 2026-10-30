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

func TestIssuerSubjects(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "cred")
	at := func() int64 { return now }
	if err := run([]string{"init", "--dir", dir, "--issuer", "issuer-demo", "--consumers", "consumer-a,consumer-b"}, io.Discard, at); err != nil {
		t.Fatal(err)
	}
	for _, bad := range []string{"", "consumer-a", "consumer-a:buyer_type=business", "consumer-a:buyer_type=business,country=TW,age_over_18=true;consumer-a:buyer_type=business,country=TW,age_over_18=true"} {
		if run([]string{"subjects", "--dir", dir, "--subjects", bad}, io.Discard, at) == nil {
			t.Fatalf("accepted %q", bad)
		}
	}
	ok := "consumer-a:buyer_type=business,country=TW,age_over_18=true;consumer-b:buyer_type=business,country=TW,age_over_18=false"
	if err := run([]string{"subjects", "--dir", dir, "--subjects", ok}, io.Discard, at); err != nil {
		t.Fatal(err)
	}
	d, _ := credential.OpenDir(dir)
	if s, _, err := d.IssueTask("show-b-01", "consumer-b", now+3600); err != nil || s.Credential["schema"] != credential.SchemaV2 || s.Credential["age_over_18"] != "false" {
		t.Fatalf("%v %v", s.Credential, err)
	}
}
