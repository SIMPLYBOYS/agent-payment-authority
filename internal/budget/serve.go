package budget

// apa-relayer's service over a Unix socket (docs/commercial-p2b.md B2,
// H-D5): the Host reaches the relayer's three keys only through these
// routes, from a uid in callers. A refusal crosses as its code; any other
// error as RELAYER_UNAVAILABLE (the Host asks again; Lookup decides), never
// its text. Requests are at most 64 KiB; a reservation may wait for its
// receipt (Relayer.wait), so answers may take minutes.

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"slices"
	"strings"
	"time"

	"github.com/SIMPLYBOYS/agent-payment-authority/internal/signer"
)

// Service is what the socket serves (a *Relayer).
type Service interface {
	RegisterTask(ctx context.Context, taskID, consumerID string, balance uint64) error
	Reserve(ctx context.Context, order json.RawMessage) (Evidence, error)
	Lookup(ctx context.Context, intent string) (Evidence, string, error)
}

// Health is GET /v1/health: what the Host checks against its own
// configuration before it pays through this relayer.
type Health struct {
	State      string `json:"state"` // ready
	ChainID    uint64 `json:"chain_id"`
	Contract   string `json:"contract"`
	DailyTxCap uint64 `json:"daily_tx_cap"`
}

const (
	maxBody     = 64 << 10
	answerLimit = 4 * time.Minute // longer than Relayer.wait
)

type uidKey struct{}

// Handler is the fixed set of routes; every request first passes the caller check.
func Handler(s Service, h Health, callers []uint32, log func(string)) http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /v1/health", func(w http.ResponseWriter, _ *http.Request) { reply(w, h, nil) })
	mux.HandleFunc("POST /v1/register", func(w http.ResponseWriter, req *http.Request) {
		var in struct {
			TaskID     string `json:"task_id"`
			ConsumerID string `json:"consumer_id"`
			Balance    uint64 `json:"balance"`
		}
		if !decode(w, req, &in) {
			return
		}
		err := s.RegisterTask(req.Context(), in.TaskID, in.ConsumerID, in.Balance)
		log("register " + in.TaskID + " " + codeOf(err))
		reply(w, struct{}{}, err)
	})
	mux.HandleFunc("POST /v1/reserve", func(w http.ResponseWriter, req *http.Request) {
		var in struct {
			Order json.RawMessage `json:"order"`
		}
		if !decode(w, req, &in) {
			return
		}
		ev, err := s.Reserve(req.Context(), in.Order)
		log("reserve " + ev.IntentHash + " " + codeOf(err))
		reply(w, ev, err)
	})
	mux.HandleFunc("POST /v1/lookup", func(w http.ResponseWriter, req *http.Request) {
		var in struct {
			Intent string `json:"intent"`
		}
		if !decode(w, req, &in) {
			return
		}
		ev, state, err := s.Lookup(req.Context(), in.Intent)
		reply(w, lookup{ev, state}, err)
	})
	return http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		uid, ok := req.Context().Value(uidKey{}).(int64)
		if !ok || uid < 0 || !slices.Contains(callers, uint32(uid)) {
			log("caller refused")
			reply(w, nil, signer.Refusal("CALLER_NOT_ALLOWED"))
			return
		}
		mux.ServeHTTP(w, req)
	})
}

type lookup struct {
	Evidence Evidence `json:"evidence"`
	State    string   `json:"state"`
}

func codeOf(err error) string {
	var r signer.Refusal
	switch {
	case err == nil:
		return "ok"
	case errors.As(err, &r):
		return string(r)
	}
	return "RELAYER_UNAVAILABLE"
}

func decode(w http.ResponseWriter, req *http.Request, v any) bool {
	d := json.NewDecoder(http.MaxBytesReader(w, req.Body, maxBody))
	d.DisallowUnknownFields()
	if err := d.Decode(v); err != nil || d.More() {
		reply(w, nil, signer.Refusal("BAD_REQUEST"))
		return false
	}
	return true
}

func reply(w http.ResponseWriter, v any, err error) {
	w.Header().Set("Content-Type", "application/json")
	switch c := codeOf(err); c {
	case "ok":
		json.NewEncoder(w).Encode(v)
	case "RELAYER_UNAVAILABLE":
		w.WriteHeader(http.StatusServiceUnavailable)
		json.NewEncoder(w).Encode(map[string]string{"code": c})
	default:
		w.WriteHeader(http.StatusConflict)
		json.NewEncoder(w).Encode(map[string]string{"code": c})
	}
}

// Serve serves h on ln until ctx ends, each connection tagged with its peer's uid.
func Serve(ctx context.Context, ln net.Listener, h http.Handler) error {
	srv := &http.Server{Handler: h, ReadTimeout: 10 * time.Second, WriteTimeout: answerLimit, ReadHeaderTimeout: 5 * time.Second,
		ConnContext: func(ctx context.Context, c net.Conn) context.Context {
			uc, ok := c.(*net.UnixConn)
			if !ok {
				return context.WithValue(ctx, uidKey{}, int64(-1))
			}
			uid, err := signer.PeerUID(uc)
			if err != nil {
				return context.WithValue(ctx, uidKey{}, int64(-1))
			}
			return context.WithValue(ctx, uidKey{}, int64(uid))
		}}
	go func() { <-ctx.Done(); srv.Close() }()
	if err := srv.Serve(ln); !errors.Is(err, http.ErrServerClosed) {
		return err
	}
	return nil
}

// Client is the Host's side of the socket: a Service whose refusals are
// the relayer's codes and whose other failures are plain errors.
type Client struct{ hc *http.Client }

// NewClient talks to the relayer on socket.
func NewClient(socket string) *Client {
	return &Client{hc: &http.Client{Timeout: answerLimit, Transport: &http.Transport{
		DialContext: func(ctx context.Context, _, _ string) (net.Conn, error) {
			return (&net.Dialer{}).DialContext(ctx, "unix", socket)
		}}}}
}

func (c *Client) call(ctx context.Context, method, path string, in, out any) error {
	var body io.Reader
	if in != nil {
		raw, err := json.Marshal(in)
		if err != nil {
			return err
		}
		body = strings.NewReader(string(raw))
	}
	req, err := http.NewRequestWithContext(ctx, method, "http://relayer"+path, body)
	if err != nil {
		return err
	}
	res, err := c.hc.Do(req)
	if err != nil {
		return fmt.Errorf("relayer: %w", err)
	}
	defer res.Body.Close()
	d := json.NewDecoder(io.LimitReader(res.Body, maxBody))
	if res.StatusCode == http.StatusConflict {
		var e struct{ Code string }
		if err := d.Decode(&e); err != nil || e.Code == "" {
			return errors.New("relayer: a refusal without its code")
		}
		return signer.Refusal(e.Code)
	}
	if res.StatusCode != http.StatusOK {
		return fmt.Errorf("relayer: status %d", res.StatusCode)
	}
	return d.Decode(out)
}

func (c *Client) Health(ctx context.Context) (Health, error) {
	var h Health
	return h, c.call(ctx, http.MethodGet, "/v1/health", nil, &h)
}

func (c *Client) RegisterTask(ctx context.Context, taskID, consumerID string, balance uint64) error {
	return c.call(ctx, http.MethodPost, "/v1/register", map[string]any{"task_id": taskID, "consumer_id": consumerID, "balance": balance}, &struct{}{})
}

func (c *Client) Reserve(ctx context.Context, order json.RawMessage) (Evidence, error) {
	var ev Evidence
	return ev, c.call(ctx, http.MethodPost, "/v1/reserve", map[string]any{"order": order}, &ev)
}

func (c *Client) Lookup(ctx context.Context, intent string) (Evidence, string, error) {
	var l lookup
	err := c.call(ctx, http.MethodPost, "/v1/lookup", map[string]any{"intent": intent}, &l)
	return l.Evidence, l.State, err
}
