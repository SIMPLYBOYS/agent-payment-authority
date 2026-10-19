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
	"strings"
	"sync"
	"time"
)

type ctxKey struct{}

const (
	maxBody = 64 << 10
	// V1: a verify request with a recording (a 10 s WAV, base64, and the assertion).
	maxVerifyBody = 512 << 10
	maxConns      = 16
	timeout       = 5 * time.Second
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

// Serve serves a Core on a listener; log gets one fixed line per request.
func Serve(ctx context.Context, ln net.Listener, core *Core, log func(string)) error {
	return serveHandler(ctx, ln, Handler(core, log), timeout)
}

// ServeAll is the P2 Signer (P2-D1): the payment routes and, when ac is
// not nil, the approval and enrollment routes on the same socket, both
// cores on the one signer.db. Health is the approvals': ready (payments
// too) once a reservation verifier is configured, else approvals-only.
func ServeAll(ctx context.Context, ln net.Listener, core *Core, ac *ApprovalCore, log func(string)) error {
	if ac == nil {
		return Serve(ctx, ln, core, log)
	}
	pay, appr := Handler(core, log), ApprovalsHandler(ac, log)
	h := http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		if strings.HasPrefix(req.URL.Path, "/v1/approvals") || strings.HasPrefix(req.URL.Path, "/v1/enrollments/") || req.URL.Path == "/v1/health" {
			appr.ServeHTTP(w, req)
			return
		}
		pay.ServeHTTP(w, req)
	})
	return serveHandler(ctx, ln, h, core.cfg.requestTimeout())
}

// ServeApprovals serves serve-approvals' ApprovalCore the same way.
func ServeApprovals(ctx context.Context, ln net.Listener, core *ApprovalCore, log func(string)) error {
	return serveHandler(ctx, ln, ApprovalsHandler(core, log), core.cfg.requestTimeout())
}

func serveHandler(ctx context.Context, ln net.Listener, h http.Handler, t time.Duration) error {
	srv := &http.Server{Handler: h, ReadTimeout: t, WriteTimeout: t, ReadHeaderTimeout: timeout,
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
	return callers(core.cfg, timeout, mux, func(w http.ResponseWriter, req *http.Request) {
		answer(w, req, "caller", Reply{}, Refusal("CALLER_NOT_ALLOWED"))
	})
}

// callers admits the configured uids only, each request under timeout.
func callers(cfg *Config, t time.Duration, mux http.Handler, refuse http.HandlerFunc) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		uid, ok := req.Context().Value(ctxKey{}).(int64)
		if !ok || uid < 0 || !cfg.allows(uint32(uid)) {
			refuse(w, req)
			return
		}
		ctx, cancel := context.WithTimeout(req.Context(), t)
		defer cancel()
		mux.ServeHTTP(w, req.WithContext(ctx))
	})
}

// ApprovalsHandler is serve-approvals' fixed set of routes (design 6.1,
// 6.4): enrollment relay, challenge, close, verify, the queries and
// abandon. Every payment route answers 409 PAYMENT_MODE_OFF, whatever the
// request; health says approvals-only. Logs carry fixed codes and IDs only.
func ApprovalsHandler(core *ApprovalCore, log func(string)) http.Handler {
	mux := http.NewServeMux()
	answer := func(w http.ResponseWriter, req *http.Request, route string, r any, err error) {
		status := http.StatusOK
		if err != nil {
			var ref Refusal
			switch {
			case errors.As(err, &ref):
				status = http.StatusConflict
				switch ref {
				case "CHALLENGE_UNKNOWN", "VERIFY_UNKNOWN", "APPROVAL_UNKNOWN", "ENROLLMENT_UNKNOWN":
					status = http.StatusNotFound
				case "CALLER_NOT_ALLOWED":
					status = http.StatusForbidden
				case "BAD_REQUEST":
					status = http.StatusBadRequest
				}
				switch v := r.(type) {
				case *ApprovalReply:
					v.Error = string(ref)
				case *EnrollReply:
					v.Error = string(ref)
				}
			case req.Context().Err() != nil:
				r, status = &ApprovalReply{Error: "SIGNER_TIMEOUT"}, http.StatusServiceUnavailable
			default:
				r, status = &ApprovalReply{Error: "SIGNER_ERROR"}, http.StatusServiceUnavailable
			}
		}
		line := route
		switch v := r.(type) {
		case *ApprovalReply:
			line += " " + v.State + " " + v.Error + " " + v.ChallengeID + " " + v.ApprovalID
		case *EnrollReply:
			line += " " + v.State + " " + v.Error + " " + v.EnrollmentID
		}
		log(line)
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(status)
		json.NewEncoder(w).Encode(r)
	}
	read := func(w http.ResponseWriter, req *http.Request, dst any) bool {
		limit := int64(maxBody)
		if req.URL.Path == "/v1/approvals/verify" {
			limit = maxVerifyBody // V1: a verify request may carry a recording of up to 10 s
		}
		body, err := io.ReadAll(http.MaxBytesReader(w, req.Body, limit))
		return err == nil && json.Unmarshal(body, dst) == nil
	}
	approval := func(route string, f func(ctx context.Context, req *http.Request) (ApprovalReply, error)) http.HandlerFunc {
		return func(w http.ResponseWriter, req *http.Request) {
			r, err := f(req.Context(), req)
			answer(w, req, route, &r, err)
		}
	}
	mux.HandleFunc("POST /v1/approvals/challenge", func(w http.ResponseWriter, req *http.Request) {
		var cr ChallengeRequest
		if !read(w, req, &cr) {
			answer(w, req, "challenge", &ApprovalReply{}, Refusal("BAD_REQUEST"))
			return
		}
		r, err := core.Challenge(req.Context(), cr)
		answer(w, req, "challenge", &r, err)
	})
	mux.HandleFunc("GET /v1/approvals/challenge", approval("challenge-get", func(ctx context.Context, req *http.Request) (ApprovalReply, error) {
		q := req.URL.Query()
		if id := q.Get("challenge_id"); id != "" {
			return core.ChallengeByID(ctx, id)
		}
		return core.ChallengeByRequest(ctx, q.Get("request_id"))
	}))
	mux.HandleFunc("POST /v1/approvals/challenges/{challenge_id}/close", func(w http.ResponseWriter, req *http.Request) {
		var cr struct {
			CloseRequestID string `json:"close_request_id"`
		}
		if !read(w, req, &cr) {
			answer(w, req, "close", &ApprovalReply{}, Refusal("BAD_REQUEST"))
			return
		}
		r, err := core.CloseChallenge(req.Context(), req.PathValue("challenge_id"), cr.CloseRequestID)
		answer(w, req, "close", &r, err)
	})
	mux.HandleFunc("POST /v1/approvals/verify", func(w http.ResponseWriter, req *http.Request) {
		var vr VerifyRequest
		if !read(w, req, &vr) {
			answer(w, req, "verify", &ApprovalReply{}, Refusal("BAD_REQUEST"))
			return
		}
		r, err := core.Verify(req.Context(), vr)
		answer(w, req, "verify", &r, err)
	})
	mux.HandleFunc("GET /v1/approvals", approval("approval-get", func(ctx context.Context, req *http.Request) (ApprovalReply, error) {
		return core.ApprovalByVerifyRequest(ctx, req.URL.Query().Get("verify_request_id"))
	}))
	mux.HandleFunc("GET /v1/approvals/{approval_id}", approval("approval-get", func(ctx context.Context, req *http.Request) (ApprovalReply, error) {
		return core.ApprovalByID(ctx, req.PathValue("approval_id"))
	}))
	mux.HandleFunc("POST /v1/approvals/{approval_id}/abandon", approval("abandon", func(ctx context.Context, req *http.Request) (ApprovalReply, error) {
		return core.Abandon(ctx, req.PathValue("approval_id"))
	}))
	// Enrollment relay: the code travels in the body, never a URL.
	mux.HandleFunc("POST /v1/enrollments/{id}/options", func(w http.ResponseWriter, req *http.Request) {
		var er struct {
			Code string `json:"code"`
		}
		if !read(w, req, &er) {
			answer(w, req, "enroll-options", &EnrollReply{}, Refusal("BAD_REQUEST"))
			return
		}
		r, err := core.EnrollOptions(req.Context(), req.PathValue("id"), er.Code)
		answer(w, req, "enroll-options", &r, err)
	})
	mux.HandleFunc("POST /v1/enrollments/{id}", func(w http.ResponseWriter, req *http.Request) {
		var er struct {
			Code       string          `json:"code"`
			Credential json.RawMessage `json:"credential"`
		}
		if !read(w, req, &er) {
			answer(w, req, "enroll", &EnrollReply{}, Refusal("BAD_REQUEST"))
			return
		}
		r, err := core.EnrollFinish(req.Context(), req.PathValue("id"), er.Code, er.Credential)
		answer(w, req, "enroll", &r, err)
	})
	off := func(w http.ResponseWriter, req *http.Request) {
		answer(w, req, "payment", &ApprovalReply{}, Refusal("PAYMENT_MODE_OFF"))
	}
	for _, route := range []string{"POST /v1/sign", "GET /v1/attempts", "GET /v1/attempts/{id}", "POST /v1/attempts/{id}/abandon"} {
		mux.HandleFunc(route, off)
	}
	mux.HandleFunc("GET /v1/health", func(w http.ResponseWriter, req *http.Request) {
		h := &ApprovalReply{State: "approvals-only", Payment: "unavailable"}
		if core.pays {
			h = &ApprovalReply{State: "ready", Payment: "available"}
		}
		answer(w, req, "health", h, nil)
	})
	return callers(core.cfg, core.cfg.requestTimeout(), mux, func(w http.ResponseWriter, req *http.Request) {
		answer(w, req, "caller", &ApprovalReply{}, Refusal("CALLER_NOT_ALLOWED"))
	})
}

// PeerUID is the uid of the process at the other end of a Unix socket, for
// the Host's own socket services (apa-relayer).
func PeerUID(c *net.UnixConn) (uint32, error) { return peerUID(c) }
