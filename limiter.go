package http

import (
	"context"
	"errors"
	"net/http"
)

// RateLimiter paces request attempts. Wait receives the request so an
// implementation can apply per-host or per-endpoint policy. Shared
// implementations must be safe for concurrent use.
type RateLimiter interface {
	Wait(ctx context.Context, req *http.Request) (err error)
}

// RateLimiterFunc adapts a function to [RateLimiter]. Use it to adapt limiters
// that only accept a context or to inspect the outgoing request.
type RateLimiterFunc func(ctx context.Context, req *http.Request) (err error)

var _ RateLimiter = RateLimiterFunc(nil)

// Wait calls f(ctx, req), or returns an error when f is nil.
func (f RateLimiterFunc) Wait(ctx context.Context, req *http.Request) (err error) {
	if f == nil {
		return errors.New("http.RateLimiterFunc: nil limiter")
	}

	return f(ctx, req)
}
