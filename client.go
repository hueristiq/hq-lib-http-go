package http

import (
	"context"
	"fmt"
	"net/http"
	"sync"
	"sync/atomic"
)

// Client executes standard HTTP requests with retries, protocol fallback,
// bounded lifetimes, rate limiting, and observability hooks.
//
// A Client is safe for concurrent use. Construct it with [New]; the zero value
// is not usable. Configuration is immutable after construction. [Client.Close]
// may run concurrently with in-flight requests and cancels them.
type Client struct {
	config ClientOptions

	fallbackHTTPClient *http.Client
	fallbackOnHTTP3    bool
	http3Primary       bool
	http3Failures      sync.Map
	http3FailureWrites atomic.Uint64
	requestCount       atomic.Uint64
	closed             atomic.Bool
	closeCtx           context.Context //nolint:containedctx // This context owns the client lifetime.
	closeCancel        context.CancelCauseFunc
}

// Do executes req using its context and returns a standard HTTP response.
// HTTP statuses, including 4xx and 5xx after retries, are responses rather than
// Go errors. The caller owns and must close a returned body.
//
// Transport exhaustion returns a [*RetryError]. A retry-policy abort preserves
// the response and error from the policy. The input request and its header map
// are not mutated. Per-request options override client defaults.
func (c *Client) Do(req *http.Request, ofs ...RequestOptionFunc) (res *http.Response, err error) {
	return c.do(req, c.requestOptions(ofs...))
}

// RoundTrip implements [http.RoundTripper] through the same path as [Client.Do].
func (c *Client) RoundTrip(req *http.Request) (res *http.Response, err error) {
	return c.Do(req)
}

var _ http.RoundTripper = (*Client)(nil)

// Close shuts down the client, cancels in-flight executions with
// [ErrClientClosed], closes idle connections, and rejects later executions.
// It is safe to call more than once.
func (c *Client) Close() {
	if c == nil || !c.closed.CompareAndSwap(false, true) {
		return
	}

	if c.closeCancel != nil {
		c.closeCancel(ErrClientClosed)
	}

	if c.config.HTTPClient != nil {
		c.CloseIdleConnections()
	}
}

// CloseIdleConnections releases pooled idle connections without disabling the
// client. In-flight requests continue.
func (c *Client) CloseIdleConnections() {
	if c.config.HTTPClient != nil {
		c.config.HTTPClient.CloseIdleConnections()
	}

	if c.fallbackHTTPClient == nil {
		return
	}

	var primary http.RoundTripper
	if c.config.HTTPClient != nil {
		primary = c.config.HTTPClient.Transport
	}

	if !sameRoundTripper(primary, c.fallbackHTTPClient.Transport) {
		c.fallbackHTTPClient.CloseIdleConnections()
	}
}

func (c *Client) closeIdleConnectionsIfDue() {
	if c.config.MaxRequestsBeforeIdleClose <= 0 {
		return
	}

	count := c.requestCount.Add(1)
	if count%uint64(c.config.MaxRequestsBeforeIdleClose) == 0 {
		c.CloseIdleConnections()
	}
}

func (c *Client) validateRequest(req *http.Request) (err error) {
	if req == nil {
		return fmt.Errorf("http.Client.Do: %w", ErrNilRequest)
	}

	if req.URL == nil {
		return fmt.Errorf("http.Client.Do: %w", ErrNilRequestURL)
	}

	if c.closed.Load() {
		return fmt.Errorf("http.Client.Do: %w", ErrClientClosed)
	}

	if c.config.HTTPClient == nil {
		return fmt.Errorf("http.Client.Do: %w", ErrHTTP3ClientRequired)
	}

	return nil
}
