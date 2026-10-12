package signer

// The service over a real Unix socket, in one process and one uid: it
// shows the caller check reads the kernel's peer uid and refuses a uid not
// configured. It is not evidence of the separation between two OS users
// (docs/commercial-p1b-design.md 9).

import (
	"bytes"
	"context"
	"encoding/hex"
	"encoding/json"
	"io"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
)

type served struct {
	client *http.Client
	mu     sync.Mutex
	logs   []string
}

func (e *env) serve(t *testing.T) *served {
	t.Helper()
	dir, err := os.MkdirTemp("", "sg") // short: a socket path is at most 104 bytes
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.RemoveAll(dir) })
	sock := filepath.Join(dir, "s")
	ln, err := net.Listen("unix", sock)
	if err != nil {
		t.Fatal(err)
	}
	s := &served{client: &http.Client{Transport: &http.Transport{DialContext: func(ctx context.Context, _, _ string) (net.Conn, error) {
		return (&net.Dialer{}).DialContext(ctx, "unix", sock)
	}}}}
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	c := e.core()
	go func() {
		done <- Serve(ctx, ln, c, func(l string) { s.mu.Lock(); s.logs = append(s.logs, l); s.mu.Unlock() })
	}()
	t.Cleanup(func() {
		cancel()
		if err := <-done; err != nil {
			t.Error(err)
		}
	})
	return s
}

func (s *served) do(t *testing.T, method, path string, body []byte) (int, Reply) {
	t.Helper()
	req, _ := http.NewRequest(method, "http://signer"+path, bytes.NewReader(body))
	resp, err := s.client.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(resp.Body)
	var r Reply
	json.Unmarshal(raw, &r)
	return resp.StatusCode, r
}

func TestServerCaller(t *testing.T) {
	e := newEnv(t)
	e.cfg.Callers = []uint32{uint32(os.Getuid()) + 1}
	s := e.serve(t)
	body, _ := json.Marshal(e.req("request-0000000001", a(1)))
	if st, r := s.do(t, "POST", "/v1/sign", body); st != http.StatusForbidden || r.Error != "CALLER_NOT_ALLOWED" {
		t.Fatalf("a uid not configured: %d %+v", st, r)
	}
	if st, r := s.do(t, "GET", "/v1/health", nil); st != http.StatusForbidden || r.State != "" {
		t.Fatalf("health without the caller check: %d %+v", st, r)
	}
}

func TestServer(t *testing.T) {
	e := newEnv(t)
	e.cfg.Callers = []uint32{uint32(os.Getuid())}
	s := e.serve(t)
	body, _ := json.Marshal(e.req("request-0000000001", a(1)))
	st, signed := s.do(t, "POST", "/v1/sign", body)
	if st != http.StatusOK || signed.State != "signed" || signed.Authorization == nil {
		t.Fatalf("sign: %d %+v", st, signed)
	}
	if st, r := s.do(t, "GET", "/v1/attempts/"+signed.AttemptID, nil); st != http.StatusOK || r.Authorization.Signature != signed.Authorization.Signature {
		t.Fatalf("get: %d %+v", st, r)
	}
	if st, r := s.do(t, "GET", "/v1/attempts?request_id=request-0000000001", nil); st != http.StatusOK || r.AttemptID != signed.AttemptID {
		t.Fatalf("get by request: %d %+v", st, r)
	}
	if st, r := s.do(t, "POST", "/v1/attempts/"+signed.AttemptID+"/abandon", nil); st != http.StatusConflict || r.Error != "ATTEMPT_NOT_ACCEPTED" || r.State != "signed" {
		t.Fatalf("abandon signed: %d %+v", st, r)
	}
	if st, r := s.do(t, "GET", "/v1/attempts/att-none", nil); st != http.StatusNotFound || r.Error != "ATTEMPT_UNKNOWN" {
		t.Fatalf("unknown: %d %+v", st, r)
	}
	big := append([]byte(`{"request_id":"`), bytes.Repeat([]byte("x"), maxBody)...)
	if st, r := s.do(t, "POST", "/v1/sign", big); st != http.StatusBadRequest || r.Error != "BAD_REQUEST" {
		t.Fatalf("over 64 KiB: %d %+v", st, r)
	}
	for _, route := range [][2]string{{"GET", "/v1/sign"}, {"POST", "/v1/keys"}, {"GET", "/v1/config"}, {"DELETE", "/v1/attempts/" + signed.AttemptID}} {
		if st, r := s.do(t, route[0], route[1], nil); st != http.StatusNotFound && st != http.StatusMethodNotAllowed || r.AttemptID != "" {
			t.Fatalf("%s %s: %d %+v", route[0], route[1], st, r)
		}
	}
	// The logs carry fixed codes and IDs, never the signature or a key.
	s.mu.Lock()
	logs := strings.Join(s.logs, "\n")
	s.mu.Unlock()
	sig := strings.TrimPrefix(signed.Authorization.Signature, "0x")
	for _, secret := range []string{sig, sig[:64], hex.EncodeToString(keyA.Serialize()), hex.EncodeToString(keyB.Serialize())} {
		if strings.Contains(logs, secret) {
			t.Fatalf("a signature or key in the logs:\n%s", logs)
		}
	}
	if !strings.Contains(logs, "sign signed  "+signed.AttemptID) {
		t.Fatalf("no fixed log line:\n%s", logs)
	}
}
