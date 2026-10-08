package config

import (
	"strings"
	"testing"
)

func env(m map[string]string) func(string) string {
	return func(k string) string { return m[k] }
}

func TestLoad(t *testing.T) {
	tests := []struct {
		name    string
		env     map[string]string
		want    string // expected Addr on success
		wantErr string // substring of the error on failure
	}{
		{name: "ipv4 loopback", env: map[string]string{"APA_ADDR": "127.0.0.1:8402"}, want: "127.0.0.1:8402"},
		{name: "ipv6 loopback", env: map[string]string{"APA_ADDR": "[::1]:8402"}, want: "[::1]:8402"},
		{name: "live cli explicitly off", env: map[string]string{"APA_ADDR": "127.0.0.1:8402", "APA_LIVE_CLI": "false"}, want: "127.0.0.1:8402"},

		{name: "missing addr", env: nil, wantErr: "APA_ADDR is required"},
		{name: "no port", env: map[string]string{"APA_ADDR": "127.0.0.1"}, wantErr: "must be ip:port"},
		{name: "all interfaces", env: map[string]string{"APA_ADDR": "0.0.0.0:8402"}, wantErr: "loopback"},
		{name: "empty host", env: map[string]string{"APA_ADDR": ":8402"}, wantErr: "loopback"},
		{name: "lan ip", env: map[string]string{"APA_ADDR": "192.168.1.10:8402"}, wantErr: "loopback"},
		{name: "hostname", env: map[string]string{"APA_ADDR": "localhost:8402"}, wantErr: "loopback"},
		{name: "port not numeric", env: map[string]string{"APA_ADDR": "127.0.0.1:http"}, wantErr: "port"},
		{name: "port out of range", env: map[string]string{"APA_ADDR": "127.0.0.1:70000"}, wantErr: "port"},
		{name: "live cli garbage", env: map[string]string{"APA_ADDR": "127.0.0.1:8402", "APA_LIVE_CLI": "yes please"}, wantErr: "must be true or false"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := Load(env(tt.env))
			if tt.wantErr != "" {
				if err == nil {
					t.Fatalf("Load() = %+v, want error containing %q", got, tt.wantErr)
				}
				if !strings.Contains(err.Error(), tt.wantErr) {
					t.Fatalf("Load() error = %q, want it to contain %q", err, tt.wantErr)
				}
				return
			}
			if err != nil {
				t.Fatalf("Load() error = %v", err)
			}
			if got.Addr != tt.want {
				t.Fatalf("Addr = %q, want %q", got.Addr, tt.want)
			}
		})
	}
}

// APA_LIVE_CLI is read, not acted on: true is LiveCLI; the demo host
// decides (it refuses true without --live-cli).
func TestLoadLiveCLI(t *testing.T) {
	for v, want := range map[string]bool{"": false, "false": false, "0": false, "true": true, "1": true} {
		got, err := Load(env(map[string]string{"APA_ADDR": "127.0.0.1:8402", "APA_LIVE_CLI": v}))
		if err != nil || got.LiveCLI != want {
			t.Errorf("APA_LIVE_CLI=%q: %+v %v", v, got, err)
		}
	}
}
