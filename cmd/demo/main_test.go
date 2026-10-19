package main

import (
	"context"
	"encoding/json"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/SIMPLYBOYS/agent-payment-authority/internal/buyer/fakecli"
	"github.com/SIMPLYBOYS/agent-payment-authority/internal/merchant"
	"github.com/SIMPLYBOYS/agent-payment-authority/internal/toolbridge"
	"github.com/SIMPLYBOYS/agent-payment-authority/internal/x402"
)

// testRoot holds every test database and token file: no test touches the
// repository's var/.
var testRoot string

// TestMain also lets the test binary stand in for the fake CLI (when started
// as fake-claude) and the MCP bridge, as the demo starts them. Tests run
// from the repository root, where the default fixture paths resolve.
func TestMain(m *testing.M) {
	switch {
	case filepath.Base(os.Args[0]) == "fake-claude":
		os.Exit(fakecli.Main(os.Args[1:], os.Getenv, os.Stdout, os.Stderr))
	case len(os.Args) > 1 && os.Args[1] == "mcp-bridge":
		if err := toolbridge.RunBridge(context.Background(), os.Stdin, os.Stdout, os.Getenv); err != nil {
			os.Exit(1)
		}
		os.Exit(0)
	}
	if err := os.Chdir("../.."); err != nil {
		panic(err)
	}
	dir, err := os.MkdirTemp("", "apa-demo-test-")
	if err != nil {
		panic(err)
	}
	testRoot = dir
	code := m.Run()
	os.RemoveAll(dir)
	os.Exit(code)
}

// envAddr is the environment of one server: its address, and a database and
// token file of its own in a private directory.
func envAddr(addr string) func(string) string {
	dir, err := os.MkdirTemp(testRoot, "srv-")
	if err != nil {
		panic(err)
	}
	return func(k string) string {
		switch k {
		case "APA_ADDR":
			return addr
		case "APA_DB_PATH":
			return filepath.Join(dir, "demo.db")
		case "APA_DEMO_TOKEN_FILE":
			return filepath.Join(dir, "tokens.json")
		}
		return ""
	}
}

// start runs the server in the background and returns its bound address plus
// a stop func that cancels it and returns run's error.
func start(t *testing.T, addr string) (string, func() error) {
	t.Helper()
	ctx, cancel := context.WithCancel(context.Background())
	ready := make(chan string, 1)
	done := make(chan error, 1)
	go func() { done <- run(ctx, envAddr(addr), ready) }()
	stop := func() error {
		cancel()
		select {
		case err := <-done:
			return err
		case <-time.After(10 * time.Second):
			t.Fatal("run did not return after cancel")
			return nil
		}
	}
	select {
	case bound := <-ready:
		return bound, stop
	case err := <-done:
		cancel()
		t.Fatalf("run exited before listening: %v", err)
	case <-time.After(10 * time.Second):
		cancel()
		t.Fatal("run never became ready")
	}
	return "", nil
}

func TestHealthHandler(t *testing.T) {
	shop, err := merchant.New("http://127.0.0.1:8402")
	if err != nil {
		t.Fatal(err)
	}
	srv := httptest.NewServer(newMux(shop, "unavailable"))
	defer srv.Close()

	resp, err := http.Get(srv.URL + "/healthz")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want 200", resp.StatusCode)
	}
	if ct := resp.Header.Get("Content-Type"); ct != "application/json" {
		t.Fatalf("Content-Type = %q, want application/json", ct)
	}
	var got health
	if err := json.NewDecoder(resp.Body).Decode(&got); err != nil {
		t.Fatal(err)
	}
	want := health{Status: "ok", Mode: "fixture", LiveCLI: false, ClaudeLogin: "not_checked", PaymentCapability: "unavailable"}
	if got != want {
		t.Fatalf("health = %+v, want %+v", got, want)
	}

	post, err := http.Post(srv.URL+"/healthz", "application/json", strings.NewReader("{}"))
	if err != nil {
		t.Fatal(err)
	}
	post.Body.Close()
	if post.StatusCode != http.StatusMethodNotAllowed {
		t.Fatalf("POST status = %d, want 405", post.StatusCode)
	}
}

func TestRunServesAndReleasesPort(t *testing.T) {
	addr, stop := start(t, "127.0.0.1:0")

	resp, err := http.Get("http://" + addr + "/healthz")
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want 200", resp.StatusCode)
	}
	checkShop(t, addr)
	if err := stop(); err != nil {
		t.Fatalf("run returned %v after cancel, want nil", err)
	}

	// Second start on the exact same port must succeed: nothing left behind.
	again, stopAgain := start(t, addr)
	if again != addr {
		t.Fatalf("second start bound %s, want %s", again, addr)
	}
	if err := stopAgain(); err != nil {
		t.Fatalf("second run returned %v after cancel, want nil", err)
	}
}

func TestRunPortConflictLeavesOccupantAlone(t *testing.T) {
	occupant, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer occupant.Close()
	addr := occupant.Addr().String()

	err = run(context.Background(), envAddr(addr), nil)
	if err == nil || !strings.Contains(err.Error(), "already in use") {
		t.Fatalf("run error = %v, want one containing %q", err, "already in use")
	}

	conn, err := net.DialTimeout("tcp", addr, 2*time.Second)
	if err != nil {
		t.Fatalf("occupant stopped accepting after the conflict: %v", err)
	}
	conn.Close()
}

func TestRunMissingConfig(t *testing.T) {
	err := run(context.Background(), envAddr(""), nil)
	if err == nil || !strings.Contains(err.Error(), "APA_ADDR is required") {
		t.Fatalf("run error = %v, want one containing %q", err, "APA_ADDR is required")
	}
}

// checkShop walks the real server the way a buyer would: catalog, then the
// resource URL it lists, which must name the bound address and answer 402.
func checkShop(t *testing.T, addr string) {
	t.Helper()
	resp, err := http.Get("http://" + addr + "/catalog")
	if err != nil {
		t.Fatal(err)
	}
	var catalog struct {
		Products []struct {
			ResourceURL string `json:"resource_url"`
		} `json:"products"`
	}
	err = json.NewDecoder(resp.Body).Decode(&catalog)
	resp.Body.Close()
	if err != nil || resp.StatusCode != http.StatusOK || len(catalog.Products) == 0 {
		t.Fatalf("catalog: %d, %v, %+v", resp.StatusCode, err, catalog)
	}
	url := catalog.Products[0].ResourceURL
	if !strings.HasPrefix(url, "http://"+addr+"/") {
		t.Fatalf("resource_url %q is not on the bound address %s", url, addr)
	}
	resp, err = http.Get(url)
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	var pr x402.PaymentRequired
	if resp.StatusCode != http.StatusPaymentRequired {
		t.Fatalf("GET %s: %d, want 402", url, resp.StatusCode)
	}
	if err := x402.Decode(resp.Header.Get(x402.HeaderPaymentRequired), &pr); err != nil || pr.Resource.URL != url {
		t.Fatalf("PAYMENT-REQUIRED: %v, resource %q, want %q", err, pr.Resource.URL, url)
	}

	// HEAD gets the same quote with no body.
	head, err := http.Head(url)
	if err != nil {
		t.Fatal(err)
	}
	body, _ := io.ReadAll(head.Body)
	head.Body.Close()
	if head.StatusCode != http.StatusPaymentRequired || len(body) != 0 || head.Header.Get(x402.HeaderPaymentRequired) != resp.Header.Get(x402.HeaderPaymentRequired) {
		t.Fatalf("HEAD %s: %d, %d body bytes, same quote %v; want 402, no body, same quote", url, head.StatusCode, len(body),
			head.Header.Get(x402.HeaderPaymentRequired) == resp.Header.Get(x402.HeaderPaymentRequired))
	}
}
