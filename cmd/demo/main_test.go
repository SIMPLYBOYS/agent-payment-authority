package main

import (
	"context"
	"encoding/json"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func envAddr(addr string) func(string) string {
	return func(k string) string {
		if k == "APA_ADDR" {
			return addr
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
	srv := httptest.NewServer(newMux())
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
