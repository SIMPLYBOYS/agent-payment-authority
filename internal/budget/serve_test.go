package budget

import (
	"context"
	"encoding/json"
	"errors"
	"net"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/SIMPLYBOYS/agent-payment-authority/internal/signer"
)

// fake answers by task / intent: "refused" a refusal, "broken" an error
// whose text must not cross the socket.
type fake struct{ registered []string }

func (f *fake) RegisterTask(_ context.Context, taskID, consumerID string, balance uint64) error {
	switch taskID {
	case "refused":
		return signer.Refusal("RELAYER_DAILY_CAP")
	case "broken":
		return errors.New("rpc http://node.example/secret-key: dial failed")
	}
	f.registered = append(f.registered, taskID+"/"+consumerID)
	return nil
}

func (f *fake) Reserve(_ context.Context, order json.RawMessage) (Evidence, error) {
	if string(order) != `{"n":1}` {
		return Evidence{}, signer.Refusal("ORDER_INVALID")
	}
	return Evidence{IntentHash: "sha256:x", Amount: 5, TxHash: "0xab", BlockNumber: 7}, nil
}

// SentToday: today's count, as a *Relayer answers it (C4).
func (f *fake) SentToday(context.Context) (uint64, error) { return 7, nil }

func (f *fake) Lookup(_ context.Context, intent string) (Evidence, string, error) {
	if intent == "broken" {
		return Evidence{}, "", errors.New("node down")
	}
	return Evidence{IntentHash: intent}, "sent", nil
}

func socketPair(t *testing.T, s Service, callers []uint32) *Client {
	t.Helper()
	dir, err := os.MkdirTemp("", "rl") // short: a socket path is at most 104 bytes
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.RemoveAll(dir) })
	sock := filepath.Join(dir, "r.sock")
	ln, err := net.Listen("unix", sock)
	if err != nil {
		t.Skip("no Unix sockets here:", err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() {
		done <- Serve(ctx, ln, Handler(s, Health{State: "ready", ChainID: 84532, Contract: "0xc0", DailyTxCap: 12}, callers, func(string) {}))
	}()
	t.Cleanup(func() { cancel(); <-done })
	return NewClient(sock)
}

// The socket carries the relayer's answers unchanged: refusals as their
// codes (the worker decides on them), other failures as an opaque error
// (asked again), and only for the configured uids (docs/commercial-p2b.md B2).
func TestSocketCarriesTheRelayersAnswers(t *testing.T) {
	f := &fake{}
	c := socketPair(t, f, []uint32{uint32(os.Getuid())})
	if h, err := c.Health(bg); err != nil || h.SentToday == nil || *h.SentToday != 7 {
		t.Fatalf("health %+v %v", h, err)
	} else if h.SentToday = nil; h != (Health{State: "ready", ChainID: 84532, Contract: "0xc0", DailyTxCap: 12}) {
		t.Fatalf("health %+v", h)
	}
	if err := c.RegisterTask(bg, "show-a-01", "consumer-a", 100); err != nil || len(f.registered) != 1 || f.registered[0] != "show-a-01/consumer-a" {
		t.Fatalf("register %v %v", err, f.registered)
	}
	var r signer.Refusal
	if err := c.RegisterTask(bg, "refused", "consumer-a", 100); !errors.As(err, &r) || r != "RELAYER_DAILY_CAP" {
		t.Fatalf("a refusal crossed as %v", err)
	}
	err := c.RegisterTask(bg, "broken", "consumer-a", 100)
	if err == nil || errors.As(err, &r) || strings.Contains(err.Error(), "secret") {
		t.Fatalf("an error crossed as %v", err)
	}
	if ev, err := c.Reserve(bg, json.RawMessage(`{"n":1}`)); err != nil || ev.TxHash != "0xab" || ev.BlockNumber != 7 {
		t.Fatalf("reserve %+v %v", ev, err)
	}
	if _, err := c.Reserve(bg, json.RawMessage(`{"n":2}`)); !errors.As(err, &r) || r != "ORDER_INVALID" {
		t.Fatalf("reserve refusal %v", err)
	}
	if ev, state, err := c.Lookup(bg, "sha256:y"); err != nil || state != "sent" || ev.IntentHash != "sha256:y" {
		t.Fatalf("lookup %+v %q %v", ev, state, err)
	}
	if _, _, err := c.Lookup(bg, "broken"); err == nil || errors.As(err, &r) {
		t.Fatalf("lookup error crossed as %v", err)
	}
}

func TestSocketRefusesOtherCallers(t *testing.T) {
	f := &fake{}
	c := socketPair(t, f, []uint32{uint32(os.Getuid()) + 1})
	var r signer.Refusal
	if err := c.RegisterTask(bg, "show-a-01", "consumer-a", 100); !errors.As(err, &r) || r != "CALLER_NOT_ALLOWED" || len(f.registered) != 0 {
		t.Fatalf("another uid: %v %v", err, f.registered)
	}
	if _, err := c.Health(bg); !errors.As(err, &r) || r != "CALLER_NOT_ALLOWED" {
		t.Fatalf("health for another uid: %v", err)
	}
}
