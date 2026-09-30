// Command demo runs the local M1 demo server: health and the fixture data shop
// (catalog and HTTP 402). It binds loopback only, has no payment capability,
// and never starts the live Claude Code CLI.
package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"net"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/SIMPLYBOYS/agent-payment-authority/internal/config"
	"github.com/SIMPLYBOYS/agent-payment-authority/internal/merchant"
)

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	if err := run(ctx, os.Getenv, nil); err != nil {
		fmt.Fprintln(os.Stderr, "demo:", err)
		os.Exit(1)
	}
}

// run serves until ctx is cancelled, then shuts down and releases the port.
// ready, if non-nil, receives the bound address once the listener is up.
func run(ctx context.Context, getenv func(string) string, ready chan<- string) error {
	cfg, err := config.Load(getenv)
	if err != nil {
		return err
	}
	ln, err := net.Listen("tcp", cfg.Addr)
	if err != nil {
		if errors.Is(err, syscall.EADDRINUSE) {
			return fmt.Errorf("%s is already in use by another process; it was left untouched, stop it yourself or change APA_ADDR", cfg.Addr)
		}
		return fmt.Errorf("listen on %s: %w", cfg.Addr, err)
	}
	// Re-check what we actually bound, not just what was configured.
	if tcp, ok := ln.Addr().(*net.TCPAddr); !ok || !tcp.IP.IsLoopback() {
		ln.Close()
		return fmt.Errorf("refusing to serve on non-loopback address %s", ln.Addr())
	}

	// The shop states URLs on the address actually bound, never on the Host
	// header a client sends.
	shop, err := merchant.New("http://" + ln.Addr().String())
	if err != nil {
		ln.Close()
		return err
	}
	srv := &http.Server{Handler: newMux(shop), ReadHeaderTimeout: 5 * time.Second}
	errc := make(chan error, 1)
	go func() { errc <- srv.Serve(ln) }()
	log.Printf("listening on http://%s (fixture mode, live CLI off, no payment capability)", ln.Addr())
	if ready != nil {
		ready <- ln.Addr().String()
	}

	select {
	case err := <-errc:
		return err
	case <-ctx.Done():
	}
	shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	return srv.Shutdown(shutdownCtx)
}

func newMux(shop *merchant.Merchant) *http.ServeMux {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /healthz", handleHealth)
	shop.Register(mux)
	return mux
}

// health reports that the process is up, and states outright what that does
// not imply: no Claude login check has run and nothing here can pay.
type health struct {
	Status            string `json:"status"`
	Mode              string `json:"mode"`
	LiveCLI           bool   `json:"live_cli"`
	ClaudeLogin       string `json:"claude_login"`
	PaymentCapability string `json:"payment_capability"`
}

func handleHealth(w http.ResponseWriter, _ *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(health{
		Status:            "ok",
		Mode:              "fixture",
		LiveCLI:           false,
		ClaudeLogin:       "not_checked",
		PaymentCapability: "unavailable",
	})
}
