package kyt

import (
	"os"
	"path/filepath"
	"testing"
)

func write(t *testing.T, body string) string {
	p := filepath.Join(t.TempDir(), "list.json")
	if err := os.WriteFile(p, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	return p
}

// The demo list: strict, its source said, addresses compared without case.
func TestList(t *testing.T) {
	l, err := Load("../../testdata/kyt/demo-risk-list.json")
	if err != nil || len(l.Addresses) != 1 || len(l.SHA256()) != 64 {
		t.Fatalf("the repository's list: %+v %v", l, err)
	}
	if !l.Flagged("0x6666666666666666666666666666666666666666") || !l.Flagged("0X6666666666666666666666666666666666666666") || l.Flagged("0x3333333333333333333333333333333333333333") {
		t.Error("flagged the wrong address")
	}
	var none *List
	if none.Flagged("0x6666666666666666666666666666666666666666") || none.SHA256() != "" {
		t.Error("no list flags something")
	}
	for name, body := range map[string]string{
		"no source":       `{"version":"apa.kyt-demo-list.v1","source":"","updated_at":"x","addresses":[]}`,
		"another version": `{"version":"v0","source":"s","updated_at":"x","addresses":[]}`,
		"upper-case":      `{"version":"apa.kyt-demo-list.v1","source":"s","updated_at":"x","addresses":[{"address":"0xAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA","reason":""}]}`,
		"twice":           `{"version":"apa.kyt-demo-list.v1","source":"s","updated_at":"x","addresses":[{"address":"0x6666666666666666666666666666666666666666","reason":""},{"address":"0x6666666666666666666666666666666666666666","reason":""}]}`,
		"unknown member":  `{"version":"apa.kyt-demo-list.v1","source":"s","updated_at":"x","addresses":[],"score":1}`,
	} {
		if _, err := Load(write(t, body)); err == nil {
			t.Errorf("%s: loaded", name)
		}
	}
}
