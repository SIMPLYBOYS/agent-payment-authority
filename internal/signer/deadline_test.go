package signer

// R2 of the P1-b review (22e45ed): the request's deadline bounds the work
// inside the Signer, the wallet lock and the database included. Waits are
// confirmed by handshakes (the wait-lock hook, the DB pool's wait count),
// never guessed with sleeps.

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"
	"time"
)

// signing runs Sign in the background; the channel gets its error.
func signing(c *Core, ctx context.Context, r SignRequest) chan error {
	done := make(chan error, 1)
	go func() { _, err := c.Sign(ctx, r); done <- err }()
	return done
}

func TestLockWaitHonorsContext(t *testing.T) {
	e := newEnv(t)
	c := e.core()
	unlock, err := c.lockWallet(bg, "wallet-a") // another signing holds wallet-a
	if err != nil {
		t.Fatal(err)
	}
	waiting := make(chan struct{}, 1)
	hook = func(s, _ string) error {
		if s == "wait-lock" {
			waiting <- struct{}{}
		}
		return nil
	}
	defer func() { hook = nil }()

	ctx, cancel := context.WithCancel(bg)
	done := signing(c, ctx, e.req("request-0000000001", a(1)))
	select {
	case <-waiting:
	case <-time.After(2 * time.Second):
		t.Fatal("never waited for the wallet lock")
	}
	cancel()
	select {
	case err := <-done:
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("cancelled wait: %v", err)
		}
	case <-time.After(time.Second): // lockWait is 3 s: only the cancellation ends the wait here
		t.Fatal("lock wait ignored its cancellation")
	}

	// A deadline shorter than lockWait ends the wait at the deadline.
	ctx, cancel = context.WithTimeout(bg, 100*time.Millisecond)
	defer cancel()
	start := time.Now()
	done = signing(c, ctx, e.req("request-0000000001", a(1)))
	<-waiting
	select {
	case err := <-done:
		if !errors.Is(err, context.DeadlineExceeded) || time.Since(start) >= lockWait {
			t.Fatalf("deadline: %v after %v", err, time.Since(start))
		}
	case <-time.After(time.Second):
		t.Fatal("lock wait ignored the request deadline")
	}
	if n := count(t, c, "1"); n != 0 {
		t.Fatalf("%d attempts kept by requests that never got the lock", n)
	}
	unlock()
	hook = nil
	if r, err := c.Sign(bg, e.req("request-0000000001", a(1))); err != nil || r.State != "signed" {
		t.Fatalf("the next request: %+v %v", r, err)
	}
}

// The handler's work waits for the Signer DB's only connection; its
// deadline ends that wait, with SIGNER_TIMEOUT, and the next request works.
func TestHandlerDeadline(t *testing.T) {
	e := newEnv(t)
	e.cfg.Callers = []uint32{uint32(os.Getuid())}
	c := e.core()
	h := Handler(c, func(string) {})
	caller := context.WithValue(bg, ctxKey{}, int64(os.Getuid()))
	conn, err := c.db.Conn(bg) // something else holds the DB
	if err != nil {
		t.Fatal(err)
	}
	released := false
	release := func() {
		if !released {
			released = true
			conn.Close()
		}
	}
	defer release()
	before := c.db.Stats().WaitCount
	ctx, cancel := context.WithTimeout(caller, 300*time.Millisecond)
	defer cancel()
	rec := httptest.NewRecorder()
	done := make(chan struct{})
	go func() {
		h.ServeHTTP(rec, httptest.NewRequest("GET", "/v1/attempts?request_id=request-0000000001", nil).WithContext(ctx))
		close(done)
	}()
	for deadline := time.Now().Add(2 * time.Second); c.db.Stats().WaitCount == before; time.Sleep(2 * time.Millisecond) {
		if time.Now().After(deadline) {
			t.Fatal("never waited for the DB")
		}
	}
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		release()
		<-done
		t.Fatal("handler outlived its deadline")
	}
	var r Reply
	json.Unmarshal(rec.Body.Bytes(), &r)
	if rec.Code != http.StatusServiceUnavailable || r.Error != "SIGNER_TIMEOUT" {
		t.Fatalf("after the deadline: %d %+v", rec.Code, r)
	}
	release()
	rec = httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest("GET", "/v1/attempts?request_id=request-0000000001", nil).WithContext(caller))
	if rec.Code != http.StatusNotFound {
		t.Fatalf("the next request: %d %s", rec.Code, rec.Body)
	}
}

// The deadline ends the request after the attempt was accepted: it stays
// accepted under its ID (no answer, not "never accepted"), and the same
// request ID finishes it.
func TestDeadlineAfterAccept(t *testing.T) {
	e := newEnv(t)
	c := e.core()
	ctx, cancel := context.WithCancel(bg)
	hook = func(s, _ string) error {
		if s == "after-accept" {
			cancel()
		}
		return nil
	}
	defer func() { hook = nil }()
	if r, err := c.Sign(ctx, e.req("request-0000000001", a(1))); !errors.Is(err, context.Canceled) || r.Authorization != nil {
		t.Fatalf("after accept: %+v %v", r, err)
	}
	hook = nil
	if g, err := c.Get(bg, "", "request-0000000001"); err != nil || g.State != "accepted" {
		t.Fatalf("the accepted attempt: %+v %v", g, err)
	}
	r, err := c.Sign(bg, e.req("request-0000000001", a(1)))
	if err != nil || r.State != "signed" || count(t, c, "1") != 1 {
		t.Fatalf("the same request ID: %+v %v", r, err)
	}
}
