package httpclient

import (
	"context"
	"net/http"

	hqgoerrors "github.com/hueristiq/hq-lib-errors-go"
)

// RateLimiter defines the contract for pacing request attempts made by a
// [Client].
//
// Implementations decide when an attempt may proceed: the client calls Wait
// before each attempt, including the first and every retry, and sends the
// request only after Wait returns nil. Wait receives the request so a limiter
// can apply per-host, per-endpoint, or per-method policy instead of one global
// rate. The returned request is the cloned attempt request; implementations
// must treat it as read-only.
//
// A limiter failure aborts that attempt: the client returns the failure to
// the caller and does not retry it as a transport error. Context cancellation
// is the common cause, and implementations should surface the context's cause
// rather than a bespoke timeout error so callers can match it.
//
// Implementations shared across clients or goroutines must be safe for
// concurrent use; the client invokes Wait from the goroutine running each
// attempt. A nil RateLimiter leaves attempts unpaced. [RateLimiterFunc] adapts
// a plain function, and [WithRateLimiter] and [WithRequestRateLimiter] install
// one at client and request scope respectively.
type RateLimiter interface {
	// Wait blocks until the limiter admits the attempt or the context ends.
	//
	// It is called once per attempt, immediately before the attempt's
	// callbacks and round trip. Returning nil admits the attempt; returning an
	// error cancels it and the error is returned to the caller of
	// [Client.Do].
	Wait(ctx context.Context, req *http.Request) (err error)
}

// RateLimiterFunc adapts a function to [RateLimiter]. Use it to adapt limiters
// that only accept a context or to inspect the outgoing request.
type RateLimiterFunc func(ctx context.Context, req *http.Request) (err error)

var _ RateLimiter = RateLimiterFunc(nil)

// Wait calls f(ctx, req), or returns an error when f is nil.
func (f RateLimiterFunc) Wait(ctx context.Context, req *http.Request) (err error) {
	if f == nil {
		return hqgoerrors.New("nil limiter")
	}

	return f(ctx, req)
}
