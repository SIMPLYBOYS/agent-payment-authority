package signer

// The Signer's service over a Unix socket (docs/commercial-p1b-design.md
// 4.2): the caller's uid must be in the configuration; requests are at
// most 64 KiB and 5 seconds; connections are bounded; only the fixed
// routes exist. Logs carry fixed codes and IDs only, never a key, a
// passphrase or a signature.

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net"
	"net/http"
	"sync"
	"time"
)

type ctxKey struct{}

const (
	maxBody  = 64 << 10
	maxConns = 16
	timeout  = 5 * time.Second
)

// limited bounds the connections a listener hands out at once.
type limited struct {
	net.Listener
	slots chan struct{}
}

func (l *limited) Accept() (net.Conn, error) {
	l.slots <- struct{}{}
	c, err := l.Listener.Accept()
	if err != nil {
		<-l.slots
		return nil, err
	}
	return &slotConn{Conn: c, release: func() { <-l.slots }}, nil
}

type slotConn struct {
	net.Conn
	release func()
	once    sync.Once
}

func (c *slotConn) Close() error {
	c.once.Do(c.release)
	return c.Conn.Close()
}

// Server serves a Core on a listener; log gets one fixed line per request.
func Serve(ctx context.Context, ln net.Listener, core *Core, log func(string)) error {
	srv := &http.Server{Handler: Handler(core, log), ReadTimeout: timeout, WriteTimeout: timeout, ReadHeaderTimeout: timeout,
		ConnContext: func(ctx context.Context, c net.Conn) context.Context {
			if s, ok := c.(*slotConn); ok {
				c = s.Conn
			}
			uc, ok := c.(*net.UnixConn)
			if !ok {
				return context.WithValue(ctx, ctxKey{}, int64(-1))
			}
			uid, err := peerUID(uc)
			if err != nil {
				return context.WithValue(ctx, ctxKey{}, int64(-1))
			}
			return context.WithValue(ctx, ctxKey{}, int64(uid))
		}}
	go func() { <-ctx.Done(); srv.Close() }()
	err := srv.Serve(&limited{Listener: ln, slots: make(chan struct{}, maxConns)})
	if errors.Is(err, http.ErrServerClosed) {
		return nil
	}
	return err
}

// Handler is the fixed set of routes. Every request first passes the
// caller check.
func Handler(core *Core, log func(string)) http.Handler {
	mux := http.NewServeMux()
	answer := func(w http.ResponseWriter, req *http.Request, route string, r Reply, err error) {
		status := http.StatusOK
		if err != nil {
			var ref Refusal
			if errors.As(err, &ref) {
				r.Error, status = string(ref), http.StatusConflict
				switch ref {
				case "ATTEMPT_UNKNOWN":
					status = http.StatusNotFound
				case "CALLER_NOT_ALLOWED":
					status = http.StatusForbidden
				case "BAD_REQUEST":
					status = http.StatusBadRequest
				}
			} else if req.Context().Err() != nil {
				// the request's deadline ended the work; an attempt
				// accepted before it stays as it is, for the same request ID
				r = Reply{Error: "SIGNER_TIMEOUT"}
				status = http.StatusServiceUnavailable
			} else {
				r = Reply{Error: "SIGNER_ERROR"}
				status = http.StatusServiceUnavailable
			}
		}
		log(route + " " + r.State + " " + r.Error + " " + r.AttemptID + " " + r.IntentHash)
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(status)
		json.NewEncoder(w).Encode(r)
	}
	mux.HandleFunc("POST /v1/sign", func(w http.ResponseWriter, req *http.Request) {
		var sr SignRequest
		body, err := io.ReadAll(http.MaxBytesReader(w, req.Body, maxBody))
		if err != nil || json.Unmarshal(body, &sr) != nil {
			answer(w, req, "sign", Reply{}, Refusal("BAD_REQUEST"))
			return
		}
		r, err := core.Sign(req.Context(), sr)
		answer(w, req, "sign", r, err)
	})
	mux.HandleFunc("GET /v1/attempts/{id}", func(w http.ResponseWriter, req *http.Request) {
		r, err := core.Get(req.Context(), req.PathValue("id"), "")
		answer(w, req, "get", r, err)
	})
	mux.HandleFunc("GET /v1/attempts", func(w http.ResponseWriter, req *http.Request) {
		r, err := core.Get(req.Context(), "", req.URL.Query().Get("request_id"))
		answer(w, req, "get", r, err)
	})
	mux.HandleFunc("POST /v1/attempts/{id}/abandon", func(w http.ResponseWriter, req *http.Request) {
		r, err := core.Abandon(req.Context(), req.PathValue("id"))
		answer(w, req, "abandon", r, err)
	})
	mux.HandleFunc("GET /v1/health", func(w http.ResponseWriter, req *http.Request) {
		answer(w, req, "health", Reply{State: "unlocked"}, nil)
	})
	return http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		uid, ok := req.Context().Value(ctxKey{}).(int64)
		if !ok || uid < 0 || !core.cfg.allows(uint32(uid)) {
			answer(w, req, "caller", Reply{}, Refusal("CALLER_NOT_ALLOWED"))
			return
		}
		ctx, cancel := context.WithTimeout(req.Context(), timeout)
		defer cancel()
		mux.ServeHTTP(w, req.WithContext(ctx))
	})
}
