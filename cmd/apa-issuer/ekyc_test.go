package main

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/SIMPLYBOYS/agent-payment-authority/internal/credential"
)

const testKey = "test-key-never-printed"

// approvedDecision: the fields the issuer reads, as Didit's sandbox answered after its hosted page was walked
// (document and selfie; 11/10), with the mock's name and document number as the things never to be kept.
func approvedDecision(id, consumer, born string) map[string]any {
	return map[string]any{
		"session_id": id, "status": "Approved", "vendor_data": consumer, "environment": "sandbox",
		"decision_trace":   map[string]any{"status": "Approved", "decided_by": "automatic", "decided_at": "2026-11-10T16:01:43.339023Z"},
		"id_verifications": []any{map[string]any{"status": "Approved", "date_of_birth": born, "issuing_state": "BRA", "nationality": "BRA", "full_name": "Gustavo Matsumoto", "document_number": "YC531181"}},
		"liveness_checks":  []any{map[string]any{"status": "Approved", "score": 99.0}},
		"face_matches":     []any{map[string]any{"status": "Approved", "score": 95.0}},
	}
}

// fakeDidit answers as Didit: the workflows, a new session (Not Started for its first decision), the decision.
func fakeDidit(t *testing.T, dec map[string]any) *httptest.Server {
	asked := 0
	s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("x-api-key") != testKey {
			http.Error(w, `{"detail":"bad key `+r.Header.Get("x-api-key")+`"}`, 401)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		switch {
		case r.Method == "GET" && r.URL.Path == "/v3/workflows/":
			json.NewEncoder(w).Encode(map[string]any{"results": []any{
				map[string]any{"workflow_id": "wf-aml", "workflow_type": "kyc", "is_default": false},
				map[string]any{"workflow_id": "wf-basic", "workflow_type": "kyc", "is_default": true}}})
		case r.Method == "POST" && r.URL.Path == "/v3/session/":
			var b map[string]string
			json.NewDecoder(r.Body).Decode(&b)
			if b["workflow_id"] != "wf-basic" || b["vendor_data"] != dec["vendor_data"] {
				http.Error(w, `{"detail":"unexpected session"}`, 400)
				return
			}
			w.WriteHeader(201)
			json.NewEncoder(w).Encode(map[string]any{"session_id": dec["session_id"], "url": "https://verify.example/session/abc", "status": "Not Started"})
		case r.Method == "GET" && r.URL.Path == "/v3/session/"+dec["session_id"].(string)+"/decision/":
			if asked++; asked == 1 && r.URL.Query().Get("first") == "" && dec["status"] == "Approved" && dec["wait"] == true {
				json.NewEncoder(w).Encode(map[string]any{"session_id": dec["session_id"], "status": "In Progress", "vendor_data": dec["vendor_data"]})
				return
			}
			json.NewEncoder(w).Encode(dec)
		default:
			http.Error(w, `{"detail":"no such endpoint"}`, 404)
		}
	}))
	t.Cleanup(s.Close)
	return s
}

func ekycDir(t *testing.T) (dir, key string) {
	dir = filepath.Join(t.TempDir(), "cred")
	if err := run([]string{"init", "--dir", dir, "--issuer", "issuer-demo", "--consumers", "consumer-a,consumer-b"}, new(bytes.Buffer), func() int64 { return now }); err != nil {
		t.Fatal(err)
	}
	key = filepath.Join(t.TempDir(), "didit.key")
	if err := os.WriteFile(key, []byte(testKey+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	return dir, key
}

func TestEKYC(t *testing.T) {
	ekycPoll = 5 * time.Millisecond
	at := func() int64 { return now } // 2026-10-13 UTC
	const sid = "3466f016-4c1c-4f1f-9ff8-0bee1be446b2"

	// A new session: the page's URL is printed, the decision waited for, the attributes derived.
	dir, key := ekycDir(t)
	dec := approvedDecision(sid, "consumer-a", "1990-06-15")
	dec["wait"] = true
	srv := fakeDidit(t, dec)
	var out bytes.Buffer
	args := []string{"ekyc", "--dir", dir, "--consumer", "consumer-a", "--dependents", "consumer-b", "--key-file", key, "--ekyc-url", srv.URL, "--scenario", "approve"}
	if err := run(args, &out, at); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out.String(), "https://verify.example/session/abc") || !strings.Contains(out.String(), "country=BR age_over_18=true") {
		t.Errorf("output: %s", out.String())
	}
	d, _ := credential.OpenDir(dir)
	subjects, _, err := d.Subjects()
	if err != nil || subjects["consumer-a"]["country"] != "BR" || subjects["consumer-a"]["age_over_18"] != "true" || subjects["consumer-a"]["buyer_type"] != "individual" ||
		subjects["consumer-b"]["country"] != "BR" || subjects["consumer-b"]["age_over_18"] != "false" {
		t.Fatalf("subjects %v %v", subjects, err)
	}
	// Each task's stored credential notes the verification its attributes came from (read back as the Host does).
	if s, _, err := d.IssueTask("show-a-01", "consumer-a", now+3600); err != nil || s.Credential["country"] != "BR" || s.Credential["age_over_18"] != "true" || s.EKYC == nil || s.EKYC.SessionID != sid {
		t.Fatalf("credential %v %+v %v", s.Credential, s.EKYC, err)
	}
	if s, err := d.ReadCredential("show-a-01"); err != nil || s.EKYC == nil || s.EKYC.Consumer != "consumer-a" || s.EKYC.Provider != "didit" {
		t.Fatalf("stored credential %+v %v", s.EKYC, err)
	}
	rec, err := os.ReadFile(filepath.Join(dir, "ekyc.json"))
	var e credential.EKYC
	if err != nil || json.Unmarshal(rec, &e) != nil || e.Provider != "didit" || e.Environment != "sandbox" || e.SessionID != sid || e.Consumer != "consumer-a" || e.Derived["country"] != "BR" {
		t.Fatalf("ekyc.json %s %v", rec, err)
	}
	// Nothing of the person, nor the key, is kept or printed.
	for _, secret := range []string{"1990", "Gustavo", "Matsumoto", "YC531181", testKey} {
		if bytes.Contains(rec, []byte(secret)) || strings.Contains(out.String(), secret) {
			t.Errorf("%q kept or printed", secret)
		}
	}
	// Attributes set by hand afterwards are the caller's word: the record does not stay to vouch for them.
	if err := run([]string{"subjects", "--dir", dir, "--subjects", "consumer-a:buyer_type=individual,country=TW,age_over_18=true;consumer-b:buyer_type=individual,country=TW,age_over_18=true"}, &out, at); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(dir, "ekyc.json")); !os.IsNotExist(err) {
		t.Error("ekyc.json outlived subjects set by hand")
	}
	if s, _, err := d.IssueTask("show-a-02", "consumer-a", now+3600); err != nil || s.EKYC != nil || s.Credential["country"] != "TW" {
		t.Errorf("a credential of attributes set by hand notes a verification: %+v %v", s.EKYC, err)
	}

	// A session made earlier (--session); a birthday 18 years ago today is 18, a day later is not.
	for born, want := range map[string]string{"2008-10-13": "true", "2008-10-14": "false"} {
		dir, key := ekycDir(t)
		srv := fakeDidit(t, approvedDecision(sid, "consumer-a", born))
		if err := run([]string{"ekyc", "--dir", dir, "--consumer", "consumer-a", "--dependents", "consumer-b", "--key-file", key, "--ekyc-url", srv.URL, "--session", sid}, &out, at); err != nil {
			t.Fatal(err)
		}
		d, _ := credential.OpenDir(dir)
		if s, _, _ := d.Subjects(); s["consumer-a"]["age_over_18"] != want {
			t.Errorf("born %s: age_over_18 %s, want %s", born, s["consumer-a"]["age_over_18"], want)
		}
	}

	// What is refused, with nothing written.
	refused := map[string]func(dec map[string]any, args *[]string, key string){
		"declined": func(dec map[string]any, _ *[]string, _ string) { dec["status"] = "Declined" },
		// POST …/simulate/ {"new_status":"Approved"} with the issuer's own key, or the console's "Simulate an outcome".
		"approved by hand": func(dec map[string]any, _ *[]string, _ string) {
			dec["decision_trace"] = map[string]any{"status": "Approved", "decided_by": "manual", "actor": map[string]any{"actor_type": "API_KEY"}}
			dec["id_verifications"], dec["liveness_checks"], dec["face_matches"] = nil, nil, nil
		},
		"decided by hand with a document": func(dec map[string]any, _ *[]string, _ string) {
			dec["decision_trace"] = map[string]any{"status": "Approved", "decided_by": "manual"}
		},
		"no document": func(dec map[string]any, _ *[]string, _ string) { dec["id_verifications"] = nil },
		"no liveness": func(dec map[string]any, _ *[]string, _ string) { dec["liveness_checks"] = nil },
		"face not matched": func(dec map[string]any, _ *[]string, _ string) {
			dec["face_matches"] = []any{map[string]any{"status": "Declined"}}
		},
		"another consumer's": func(dec map[string]any, _ *[]string, _ string) { dec["vendor_data"] = "consumer-b" },
		"no country": func(dec map[string]any, _ *[]string, _ string) {
			dec["id_verifications"].([]any)[0].(map[string]any)["issuing_state"] = "D<<"
		},
		"no birth date": func(dec map[string]any, _ *[]string, _ string) {
			dec["id_verifications"].([]any)[0].(map[string]any)["date_of_birth"] = ""
		},
		"still in progress": func(dec map[string]any, a *[]string, _ string) {
			dec["status"] = "In Progress"
			*a = append(*a, "--wait", "0")
		},
		"a holder left out": func(_ map[string]any, a *[]string, _ string) {
			*a = (*a)[:len(*a)-2]
			*a = append(*a, "--session", sid)
		},
		"a key file others read": func(_ map[string]any, _ *[]string, key string) { os.Chmod(key, 0o644) },
		"a wrong key":            func(_ map[string]any, _ *[]string, key string) { os.WriteFile(key, []byte("another-key"), 0o600) },
	}
	for name, change := range refused {
		dir, key := ekycDir(t)
		dec := approvedDecision(sid, "consumer-a", "1990-06-15")
		args := []string{"ekyc", "--dir", dir, "--consumer", "consumer-a", "--key-file", key, "--session", sid, "--dependents", "consumer-b"}
		change(dec, &args, key)
		args = append(args, "--ekyc-url", fakeDidit(t, dec).URL)
		var out bytes.Buffer
		err := run(args, &out, at)
		if err == nil {
			t.Errorf("%s: accepted", name)
			continue
		}
		if strings.Contains(err.Error()+out.String(), testKey) || strings.Contains(err.Error()+out.String(), "another-key") {
			t.Errorf("%s: the key is in %q", name, err)
		}
		for _, f := range []string{"subjects.json", "ekyc.json"} {
			if _, serr := os.Stat(filepath.Join(dir, f)); !os.IsNotExist(serr) {
				t.Errorf("%s: %s written (%v)", name, f, err)
			}
		}
	}
}

func TestAlpha2(t *testing.T) {
	for a3, want := range map[string]string{"BRA": "BR", "TWN": "TW", "USA": "US", "DEU": "DE", "JPN": "JP", "D<<": "", "BR": "", "BRAB": "", "RBR": "", "A B": "", "": ""} {
		if got := alpha2(a3); got != want {
			t.Errorf("alpha2(%q) = %q, want %q", a3, got, want)
		}
	}
	if len(iso3166)%6 != 0 {
		t.Errorf("table length %d", len(iso3166))
	}
}
