package signer

import "context"

// For the external tests of this package only (wiring_test.go); nothing
// outside this package's tests can reach them.

func SetHook(f func(stage, attemptID string) error) { hook = f }

// WithCaller is what Serve puts in a request's context for a peer uid.
func WithCaller(ctx context.Context, uid uint32) context.Context {
	return context.WithValue(ctx, ctxKey{}, int64(uid))
}
