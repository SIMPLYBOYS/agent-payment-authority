// Package config loads and validates runtime settings from the environment.
package config

import (
	"errors"
	"fmt"
	"net"
	"strconv"
)

// Config is the validated runtime configuration.
type Config struct {
	Addr string // loopback ip:port the demo server binds
}

// Load reads settings through getenv (os.Getenv in production).
func Load(getenv func(string) string) (Config, error) {
	addr := getenv("APA_ADDR")
	if addr == "" {
		return Config{}, errors.New("APA_ADDR is required (e.g. 127.0.0.1:8402); copy .env.example to .env or export it")
	}
	host, port, err := net.SplitHostPort(addr)
	if err != nil {
		return Config{}, fmt.Errorf("APA_ADDR %q must be ip:port: %v", addr, err)
	}
	// IP literal only: no DNS lookup decides where we bind.
	if ip := net.ParseIP(host); ip == nil || !ip.IsLoopback() {
		return Config{}, fmt.Errorf("APA_ADDR host %q must be a loopback IP (127.0.0.1 or ::1); the demo never binds other interfaces", host)
	}
	if _, err := strconv.ParseUint(port, 10, 16); err != nil {
		return Config{}, fmt.Errorf("APA_ADDR port %q must be a number in 0-65535", port)
	}

	if v := getenv("APA_LIVE_CLI"); v != "" {
		live, err := strconv.ParseBool(v)
		if err != nil {
			return Config{}, fmt.Errorf("APA_LIVE_CLI %q must be true or false", v)
		}
		// ponytail: no LiveCLI field until a reviewed live run exists to consume it.
		if live {
			return Config{}, errors.New("APA_LIVE_CLI=true is not available to the demo server: live runs go through apa-buyer run --live (docs/cli-poc.md); unset it or set it to false")
		}
	}
	return Config{Addr: addr}, nil
}
