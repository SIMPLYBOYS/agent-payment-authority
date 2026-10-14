package main

// Tests of C2's Host side (docs/commercial-c2-design.md 5 to 7): a v2
// scenario issues the task's credential at its start, each purchase is
// verified inside Commit and recorded with CREDENTIAL_VERIFIED, and an
// approval after a revocation withdraws the session. Fake CLI, test keys.

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/SIMPLYBOYS/agent-payment-authority/internal/contracts"
	"github.com/SIMPLYBOYS/agent-payment-authority/internal/credential"
	"github.com/SIMPLYBOYS/agent-payment-authority/internal/policy"
)

// v2Catalog: an apa.pay.v2 entry per consumer; the main line asks (0.50 is
// above allow_max 0.10).
func v2Catalog(t *testing.T) string {
	t.Helper()
	d := map[string]any{"purpose": "買一份韓國市場簡報", "task_budget": "5000000", "allow_max": "100000", "approval_deadline_seconds": 60, "valid_seconds": 1800}
	raw, _ := json.Marshal(map[string]any{"description": "test", "scenarios": []map[string]any{
		{"id": "v2-a", "consumer_id": "consumer-a", "contract": "apa.pay.v2", "scenario": mainline(t), "label": "v2 A", "delegation": d},
		{"id": "v2-b", "consumer_id": "consumer-b", "contract": "apa.pay.v2", "scenario": mainline(t), "label": "v2 B", "delegation": d},
	}})
	p := filepath.Join(t.TempDir(), "scenarios.json")
	os.WriteFile(p, raw, 0o600)
	return p
}

// credDir is a credential directory of test keys for consumers.
func credDir(t *testing.T, consumers ...string) (string, *credential.Dir) {
	t.Helper()
	p := filepath.Join(t.TempDir(), "cred")
	if err := credential.InitDir(p, "issuer-test", consumers, time.Now().Unix()); err != nil {
		t.Fatal(err)
	}
	d, err := credential.OpenDir(p)
	if err != nil {
		t.Fatal(err)
	}
	return p, d
}

func types(r eventsReply) string { return strings.Join(eventTypes(r), ",") }

func TestC2MainLine(t *testing.T) {
	path, dir := credDir(t, "consumer-a", "consumer-b")
	h := controlHost(t, hostOpts{expiryEvery: time.Hour, scenarios: v2Catalog(t), args: []string{"--credential-dir", path}})
	defer h.stop()

	// A: verified at the purchase (valid), then revoked while it waits: the
	// approval withdraws the session with CREDENTIAL_REVOKED, no grant.
	simA := h.simulate("consumer-a")
	task := h.delegateAndRunAs(simA, "v2-a", "01", "apa.pay.v2")["task_id"].(string)
	r := h.events(task)
	evs := r.Events
	if want := "TASK_STARTED,DELEGATION_BOUND,PURCHASE_PROPOSED,CREDENTIAL_VERIFIED,POLICY_DECIDED,APPROVAL_SESSION_OPENED"; types(r) != want {
		t.Fatalf("events %s, want %s", types(r), want)
	}
	if c := evs[3].Credential; c == nil || c.Result != "valid" || c.Verifier != verifierID || c.ChallengeID == "" || evs[3].Version != "apa.pay.v2" {
		t.Fatalf("CREDENTIAL_VERIFIED %+v", evs[3])
	}
	id, intent := pending(t, h.approvals(simA))
	issued, err := dir.ReadCredential(task)
	if err != nil {
		t.Fatal(err)
	}
	if err := dir.Revoke(issued.Credential["status"], time.Now().Unix()); err != nil {
		t.Fatal(err)
	}
	code, res := h.post("/api/sim/approvals/"+id, simA, resolveBody("approve-req-00000001", intent, "approve"))
	if code != 409 || res["error"] != "approval_closed" || h.grants() != 0 {
		t.Fatalf("approve after revocation: %d %v", code, res)
	}
	evs = h.events(task).Events
	last := evs[len(evs)-1]
	if last.EventType != "APPROVAL_WITHDRAWN" || last.Decision == nil || last.Decision.Reason != "CREDENTIAL_REVOKED" {
		t.Fatalf("last event %+v", last)
	}

	// B: still valid: approved with its grant.
	simB := h.simulate("consumer-b")
	h.delegateAndRunAs(simB, "v2-b", "02", "apa.pay.v2")
	id, intent = pending(t, h.approvals(simB))
	if code, res := h.post("/api/sim/approvals/"+id, simB, resolveBody("approve-req-00000002", intent, "approve")); code != 200 || h.grants() != 1 {
		t.Fatalf("approve: %d %v", code, res)
	}
}

// A consumer without a registered holder runs with an absent credential:
// its purchase is denied CONFIG_MISSING, recorded as absent.
func TestC2AbsentHolder(t *testing.T) {
	path, _ := credDir(t, "consumer-a")
	h := controlHost(t, hostOpts{expiryEvery: time.Hour, scenarios: v2Catalog(t), args: []string{"--credential-dir", path}})
	defer h.stop()
	sim := h.simulate("consumer-b")
	task := h.delegateAndRunAs(sim, "v2-b", "01", "apa.pay.v2")["task_id"].(string)
	for _, e := range h.events(task).Events {
		switch {
		case e.EventType == "CREDENTIAL_VERIFIED" && (e.Credential == nil || e.Credential.Result != "absent" || e.Credential.ChallengeID != ""):
			t.Fatalf("verification %+v", e.Credential)
		case e.EventType == "POLICY_DECIDED" && (e.Decision.PolicyVerdict != "Deny" || e.Decision.Reason != "CONFIG_MISSING"):
			t.Fatalf("decision %+v", e.Decision)
		}
	}
}

// Without --credential-dir a v2 catalog does not start; a broken status
// list stops the start too.
func TestC2NeedsCredentialDir(t *testing.T) {
	try := func(args ...string) error {
		dir, _ := os.MkdirTemp(testRoot, "c2-")
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		exe, _ := os.Executable()
		return serve(ctx, append([]string{"--trace-dir", filepath.Join(dir, "runs"), "--registry", "testdata/demo/registry.json", "--fake-cli", exe,
			"--scenarios", v2Catalog(t)}, args...), deps{getenv: hostEnv(dir, "127.0.0.1:0"), self: exe, out: &lockedBuffer{}, clock: wallClock, ready: make(chan string, 1)})
	}
	if err := try(); err == nil || !strings.Contains(err.Error(), "--credential-dir") {
		t.Errorf("no credential directory: %v", err)
	}
	path, _ := credDir(t, "consumer-a")
	os.WriteFile(filepath.Join(path, "status.json"), []byte("{"), 0o600)
	if err := try("--credential-dir", path); err == nil || !strings.Contains(err.Error(), "status list") {
		t.Errorf("a broken status list: %v", err)
	}
}

// C2-D4 (a): the running Host re-signs the list each period, through the
// writer that keeps revocations, and stops with its context.
func TestRefreshStatus(t *testing.T) {
	_, d := credDir(t, "consumer-a")
	if err := d.Revoke("3", time.Now().Unix()); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() {
		refreshStatus(ctx, d, 20*time.Millisecond, deps{clock: wallClock, out: &lockedBuffer{}})
		close(done)
	}()
	fp, _ := d.StatusKeyFingerprint()
	for end := time.Now().Add(10 * time.Second); ; time.Sleep(20 * time.Millisecond) {
		l, _, err := d.ReadStatus("issuer-test", fp, time.Now().Unix())
		if err == nil && l.ListVersion >= 4 {
			if len(l.Revoked) != 1 || l.Revoked[0] != "3" {
				t.Fatalf("a refresh dropped the revocation: %v", l.Revoked)
			}
			break
		}
		if time.Now().After(end) {
			t.Fatalf("not refreshed: %+v %v", l, err)
		}
	}
	cancel()
	<-done
}

// The shipped C2 catalog (make present-c2) loads in fake mode, fits the
// demo profile, and is refused as a live catalog (C2-D9: v2 is fake only).
func TestC2ShippedCatalog(t *testing.T) {
	var regCfg contracts.RegistryConfig
	if err := decodeFile("testdata/demo/registry.json", &regCfg); err != nil {
		t.Fatal(err)
	}
	reg, _ := contracts.NewRegistry(regCfg)
	scs, err := loadScenarios("testdata/demo/scenarios-c2.json", reg, regCfg, false)
	if err != nil || len(scs) != 2 || scs["v2-a"].version() != "apa.pay.v2" || scs["v2-b"].ConsumerID != "consumer-b" {
		t.Fatalf("catalog: %v %v", scs, err)
	}
	raw, _ := os.ReadFile("testdata/policy/m1-demo-profile.json")
	if p, err := policy.LoadProfile(raw); err != nil || checkDelegations(scs, p) != nil {
		t.Fatalf("profile: %v", err)
	}
	if _, err := loadScenarios("testdata/demo/scenarios-c2.json", reg, regCfg, true); err == nil {
		t.Fatal("a v2 catalog loaded as live")
	}
}
