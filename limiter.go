package http

import (
	"context"
)

// RateLimiter paces request attempts. It is the minimal surface the client
// needs from a rate limiter: *Limiter from github.com/hueristiq/hq-lib-limiter-go
// satisfies it, and so does any compatible implementation — no module
// dependency is imposed. Use [RateLimiterFunc] to adapt a plain function.
//
// Methods:
//   - Wait(ctx context.Context) (err error): blocks until the next attempt may
//     proceed or ctx is done.
type RateLimiter interface {
	// Wait blocks until the next attempt may proceed or ctx is done.
	//
	// Parameters:
	//   - ctx (context.Context): the context bounding the wait.
	//
	// Returns:
	//   - err (error): non-nil when ctx ends before the wait completes.
	Wait(ctx context.Context) (err error)
}

// RateLimiterFunc adapts an ordinary function to a [RateLimiter], so a caller
// can plug in a limiter whose method set does not match the interface or wrap
// one with additional behavior.
type RateLimiterFunc func(ctx context.Context) (err error)

// Wait calls f(ctx).
//
// Parameters:
//   - ctx (context.Context): the context bounding the wait.
//
// Returns:
//   - err (error): whatever f returns.
func (f RateLimiterFunc) Wait(ctx context.Context) (err error) {
	return f(ctx)
}
