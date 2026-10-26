package http

import (
	"context"
	"crypto/tls"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"reflect"
	"slices"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/quic-go/quic-go/http3"

	hqgohttpheader "github.com/hueristiq/hq-lib-http-go/header"
	hqgohttpmethod "github.com/hueristiq/hq-lib-http-go/method"
	hqgohttprequest "github.com/hueristiq/hq-lib-http-go/request"
)

// Client is the primary structure used to perform HTTP requests.
//
// It executes requests with retry logic, honoring the caller's context, a
// configurable retry policy that sees both the response and the error of each
// attempt, a rate limiter hook, and observability callbacks. It maintains two
// underlying clients: the primary one executing requests with the configured
// protocol, and an optional fallback used for HTTP/1.x-broken cleartext
// exchanges or for HTTP/3 attempts in [ProtocolAutoHTTP3]. It can also
// periodically close idle connections to bound resource usage in long-running
// processes.
//
// A Client is safe for concurrent use. It is immutable after construction
// except for the request counter driving the periodic idle-connection close,
// the HTTP/3 failure cache, and the shutdown state, which are synchronized
// atomically or with a mutex. [Client.Close] may be called concurrently with
// in-flight executions; it cancels them. Construct through [New]; the zero
// value is not usable.
//
// Fields:
//   - config (ClientOptions): the resolved configuration. It is private and
//     immutable after [New]; use [Client.Config] to inspect it.
//   - fallbackHTTPClient (*http.Client): the fallback client, nil when the
//     selected protocol has no fallback.
//   - fallbackOnHTTP3 (bool): whether fallback errors are HTTP/3 transport
//     errors rather than the HTTP/1.x-broken error.
//   - http3Primary (bool): whether the primary client speaks HTTP/3. It is
//     false when a custom client or transport supplies the protocol.
//   - http3Failures (sync.Map): hosts that recently failed an HTTP/3 attempt,
//     mapped to the failure time. Used by [ProtocolAutoHTTP3] to skip HTTP/3
//     for the configured TTL. Expired entries are pruned periodically.
//   - http3FailureWrites (atomic.Uint64): the number of failure-cache writes,
//     used to trigger the periodic prune.
//   - requestCount (atomic.Uint64): the number of completed executions; idle
//     connections are closed whenever the count reaches a multiple of the
//     configured threshold.
//   - closed (atomic.Bool): whether [Client.Close] has shut the client down.
//   - closeCtx (context.Context): canceled when the client closes; it cancels
//     in-flight executions.
//   - closeCancel (context.CancelCauseFunc): cancels closeCtx with
//     [ErrClientClosed].
type Client struct {
	config ClientOptions

	fallbackHTTPClient *http.Client
	fallbackOnHTTP3    bool
	http3Primary       bool
	http3Failures      sync.Map
	http3FailureWrites atomic.Uint64
	requestCount       atomic.Uint64
	closed             atomic.Bool
	closeCtx           context.Context //nolint:containedctx // The close context is the client lifecycle signal.
	closeCancel        context.CancelCauseFunc
}

// Request creates a [RequestBuilder] executing through the client.
//
// Returns:
//   - builder (*RequestBuilder): a new request builder.
func (c *Client) Request() (builder *RequestBuilder) {
	builder = &RequestBuilder{
		client: c,
		params: make(url.Values),
	}

	return builder
}

// Get is a convenience method for performing an HTTP GET request.
//
// Parameters:
//   - rawURL (string): the target URL. It must carry a scheme or a host.
//   - ofs (...RequestOptionFunc): per-request overrides of the execution
//     knobs.
//
// Returns:
//   - res (*Response): the response received from the server.
//   - err (error): non-nil when the request fails.
func (c *Client) Get(rawURL string, ofs ...RequestOptionFunc) (res *Response, err error) {
	res, err = c.Request().Method(hqgohttpmethod.GET).URL(rawURL).WithOptions(ofs...).Do()

	return res, err
}

// Head is a convenience method for performing an HTTP HEAD request.
//
// Parameters:
//   - rawURL (string): the target URL. It must carry a scheme or a host.
//   - ofs (...RequestOptionFunc): per-request overrides of the execution
//     knobs.
//
// Returns:
//   - res (*Response): the response received from the server.
//   - err (error): non-nil when the request fails.
func (c *Client) Head(rawURL string, ofs ...RequestOptionFunc) (res *Response, err error) {
	res, err = c.Request().Method(hqgohttpmethod.HEAD).URL(rawURL).WithOptions(ofs...).Do()

	return res, err
}

// Post is a convenience method for performing an HTTP POST request with a
// body.
//
// The body accepts the types supported by the request package: a string, a
// []byte, an io.Reader, an io.ReadSeeker, a *bytes.Buffer, a
// func() (io.Reader, error), or a *ReusableReadCloser.
//
// Parameters:
//   - rawURL (string): the target URL. It must carry a scheme or a host.
//   - body (any): the request payload; nil means no body.
//   - ofs (...RequestOptionFunc): per-request overrides of the execution
//     knobs.
//
// Returns:
//   - res (*Response): the response received from the server.
//   - err (error): non-nil when the request fails.
func (c *Client) Post(rawURL string, body any, ofs ...RequestOptionFunc) (res *Response, err error) {
	res, err = c.Request().Method(hqgohttpmethod.POST).URL(rawURL).Body(body).WithOptions(ofs...).Do()

	return res, err
}

// Query is a convenience method for performing an HTTP QUERY request with a
// body, as defined by RFC 10008. The query is carried in the request body.
//
// QUERY is safe and idempotent, so it is retried by default under
// [WithRetryIdempotent]. The body accepts the types supported by [Client.Post].
//
// Parameters:
//   - rawURL (string): the target URL. It must carry a scheme or a host.
//   - body (any): the request payload; nil means no body.
//   - ofs (...RequestOptionFunc): per-request overrides of the execution
//     knobs.
//
// Returns:
//   - res (*Response): the response received from the server.
//   - err (error): non-nil when the request fails.
func (c *Client) Query(rawURL string, body any, ofs ...RequestOptionFunc) (res *Response, err error) {
	res, err = c.Request().Method(hqgohttpmethod.QUERY).URL(rawURL).Body(body).WithOptions(ofs...).Do()

	return res, err
}

// Put is a convenience method for performing an HTTP PUT request with a body.
// See [Client.Post] for the accepted body types.
//
// Parameters:
//   - rawURL (string): the target URL. It must carry a scheme or a host.
//   - body (any): the request payload; nil means no body.
//   - ofs (...RequestOptionFunc): per-request overrides of the execution
//     knobs.
//
// Returns:
//   - res (*Response): the response received from the server.
//   - err (error): non-nil when the request fails.
func (c *Client) Put(rawURL string, body any, ofs ...RequestOptionFunc) (res *Response, err error) {
	res, err = c.Request().Method(hqgohttpmethod.PUT).URL(rawURL).Body(body).WithOptions(ofs...).Do()

	return res, err
}

// Patch is a convenience method for performing an HTTP PATCH request with a
// body. See [Client.Post] for the accepted body types.
//
// Parameters:
//   - rawURL (string): the target URL. It must carry a scheme or a host.
//   - body (any): the request payload; nil means no body.
//   - ofs (...RequestOptionFunc): per-request overrides of the execution
//     knobs.
//
// Returns:
//   - res (*Response): the response received from the server.
//   - err (error): non-nil when the request fails.
func (c *Client) Patch(rawURL string, body any, ofs ...RequestOptionFunc) (res *Response, err error) {
	res, err = c.Request().Method(hqgohttpmethod.PATCH).URL(rawURL).Body(body).WithOptions(ofs...).Do()

	return res, err
}

// Delete is a convenience method for performing an HTTP DELETE request.
//
// Parameters:
//   - rawURL (string): the target URL. It must carry a scheme or a host.
//   - ofs (...RequestOptionFunc): per-request overrides of the execution
//     knobs.
//
// Returns:
//   - res (*Response): the response received from the server.
//   - err (error): non-nil when the request fails.
func (c *Client) Delete(rawURL string, ofs ...RequestOptionFunc) (res *Response, err error) {
	res, err = c.Request().Method(hqgohttpmethod.DELETE).URL(rawURL).WithOptions(ofs...).Do()

	return res, err
}

// Options is a convenience method for performing an HTTP OPTIONS request.
//
// Parameters:
//   - rawURL (string): the target URL. It must carry a scheme or a host.
//   - ofs (...RequestOptionFunc): per-request overrides of the execution
//     knobs.
//
// Returns:
//   - res (*Response): the response received from the server.
//   - err (error): non-nil when the request fails.
func (c *Client) Options(rawURL string, ofs ...RequestOptionFunc) (res *Response, err error) {
	res, err = c.Request().Method(hqgohttpmethod.OPTIONS).URL(rawURL).WithOptions(ofs...).Do()

	return res, err
}

// ClientOptions holds the configurable knobs of a [Client], populated through
// [ClientOptionFunc] values passed to [New]. The resolved configuration is
// private to the client and immutable after construction; use [Client.Config]
// to inspect a snapshot.
//
// Fields:
//   - HTTPClient (*http.Client): the underlying client executing requests. Nil
//     means a client is built from Protocol, TLSConfig, Transport, and the
//     transport controls.
//   - Protocol (Protocol): the HTTP protocol mode. The zero value is
//     [ProtocolAuto].
//   - HTTP3FallbackTTL (time.Duration): how long a host that failed an HTTP/3
//     attempt skips HTTP/3 in [ProtocolAutoHTTP3]. Non-positive disables the
//     cache.
//   - TLSConfig (*tls.Config): the TLS configuration for library-built
//     transports. Nil uses the system defaults; ignored when HTTPClient or
//     Transport is set.
//   - Transport (http.RoundTripper): a caller-supplied transport replacing the
//     protocol-built one. It takes precedence over Protocol, TLSConfig, Proxy,
//     and TransportWrapper.
//   - TransportWrapper (func(http.RoundTripper) http.RoundTripper): the
//     decorator applied to a library-built transport. Ignored when Transport is
//     set.
//   - Proxy (func(*http.Request) (*url.URL, error)): the proxy selector for a
//     library-built *http.Transport. Ignored when Transport or HTTPClient is
//     set, and unsupported by HTTP/3.
//   - CheckRedirect (func(*http.Request, []*http.Request) error): the redirect
//     policy. A separate unexported flag records whether the option was
//     supplied, so nil can clear a custom client's policy.
//   - CookieJar (http.CookieJar): the cookie jar. A separate unexported flag
//     records whether the option was supplied, so nil can clear a custom
//     client's jar.
//   - Headers ([]HeaderField): default headers set on every request, unless the
//     request sets the same key itself. A Host entry maps to Request.Host; the
//     seeded User-Agent entry is replaceable the same way.
//   - Timeout (time.Duration): the bound on the execution through receipt of the
//     response headers, including retries and the waits between them.
//     Non-positive means no execution bound.
//   - DefaultContext (context.Context): the default execution context; nil means
//     [context.Background]. A request carrying an explicit context overrides it.
//   - ResponseBodyTimeout (time.Duration): the separate bound on reading a
//     handed-out response body. Non-positive means no separate bound; the
//     caller's context still applies.
//   - MaxResponseBodySize (int64): the cap on bytes buffered by the [Body]
//     helpers. Non-positive means unlimited.
//   - MaxRequestBodySize (int64): the cap on bytes buffered into a request body.
//     Non-positive means unlimited.
//   - AttemptTimeout (time.Duration): the bound on one attempt through receipt
//     of the response headers. Non-positive disables it.
//   - URLParser (hqgohttprequest.URLParser): the parser turning a raw URL into
//     an absolute URL when [RequestBuilder] builds a request. Nil selects
//     [net/url.Parse].
//   - Retrier (Retrier): the engine running the attempts of every execution.
//     Nil means a single attempt; the engine owns the attempt count, waits, and
//     backoff.
//   - RetryPolicy (RetryPolicy): the predicate deciding, per attempt, whether to
//     retry. Nil selects [DefaultRetryPolicy].
//   - RateLimiter (RateLimiter): the limiter waited on before every retry-engine
//     attempt. Nil means no limiting.
//   - OnRequest (OnRequestFunc): the callback invoked before every retry-engine
//     attempt.
//   - OnResponse (OnResponseFunc): the callback invoked after every retry-engine
//     attempt.
//   - OnRetry (OnRetryFunc): the callback invoked before each retry wait.
//   - PrepareRetry (PrepareRetryFunc): the hook invoked after the retry wait and
//     before the next attempt.
//   - RetryExhaustionHandler (RetryExhaustionHandler): decides what an execution
//     returns when retries run out. Nil returns a [*RetryError].
//   - IdempotentRetries (bool): whether only idempotent methods are retried.
//   - MaxRequestsBeforeIdleClose (int): the number of completed executions after
//     which idle connections are closed. Non-positive disables the periodic
//     closing.
//   - ResponseReadLimit (int64): the maximum number of bytes drained from a
//     retried response's body so the connection can be reused. The drain reads
//     one byte past the bound to detect EOF. Non-positive falls back to
//     [DefaultResponseReadLimit].
//   - RetryErrorBodyLimit (int64): the maximum number of bytes copied into a
//     [RetryError] from the final response body. Zero disables body capture; a
//     negative value is normalized to zero.
//   - TransportBuilder (TransportBuilder): customizes a library-built
//     *http.Transport. Ignored when Transport is set; HTTP/3 is not passed to it.
//   - HTTP3TransportBuilder (HTTP3TransportBuilder): customizes a library-built
//     *http3.Transport. Ignored when Transport is set.
//   - DialContext (func(context.Context, string, string) (net.Conn, error)):
//     the dial function applied to library-built *http.Transport values.
//   - TLSHandshakeTimeout (time.Duration): the TLS handshake timeout applied to
//     library-built *http.Transport values when the corresponding unexported
//     flag is set.
type ClientOptions struct {
	HTTPClient                 *http.Client
	Protocol                   Protocol
	HTTP3FallbackTTL           time.Duration
	TLSConfig                  *tls.Config
	Transport                  http.RoundTripper
	TransportWrapper           func(http.RoundTripper) http.RoundTripper
	TransportBuilder           TransportBuilder
	HTTP3TransportBuilder      HTTP3TransportBuilder
	DialContext                func(ctx context.Context, network, address string) (net.Conn, error)
	TLSHandshakeTimeout        time.Duration
	tlsHandshakeTimeoutSet     bool
	Proxy                      func(*http.Request) (*url.URL, error)
	CheckRedirect              func(*http.Request, []*http.Request) error
	checkRedirectSet           bool
	CookieJar                  http.CookieJar
	cookieJarSet               bool
	Headers                    []HeaderField
	Timeout                    time.Duration
	DefaultContext             context.Context //nolint:containedctx // WithContext stores a construction-time default.
	ResponseBodyTimeout        time.Duration
	MaxResponseBodySize        int64
	MaxRequestBodySize         int64
	AttemptTimeout             time.Duration
	URLParser                  hqgohttprequest.URLParser
	Retrier                    Retrier
	RetryPolicy                RetryPolicy
	RateLimiter                RateLimiter
	OnRequest                  OnRequestFunc
	OnResponse                 OnResponseFunc
	OnRetry                    OnRetryFunc
	PrepareRetry               PrepareRetryFunc
	RetryExhaustionHandler     RetryExhaustionHandler
	IdempotentRetries          bool
	MaxRequestsBeforeIdleClose int
	ResponseReadLimit          int64
	RetryErrorBodyLimit        int64
}

// normalize fills invalid values in the client options with their defaults,
// so a misconfigured client still behaves sanely.
func (opts *ClientOptions) normalize() {
	if opts.Protocol < ProtocolAuto || opts.Protocol > ProtocolHTTP3 {
		opts.Protocol = ProtocolAuto
	}

	if opts.Timeout < 0 {
		opts.Timeout = 0
	}

	if opts.ResponseBodyTimeout < 0 {
		opts.ResponseBodyTimeout = DefaultResponseBodyTimeout
	}

	if opts.MaxResponseBodySize < 0 {
		opts.MaxResponseBodySize = 0
	}

	if opts.MaxRequestBodySize < 0 {
		opts.MaxRequestBodySize = 0
	}

	if opts.AttemptTimeout < 0 {
		opts.AttemptTimeout = 0
	}

	if opts.RetryPolicy == nil {
		opts.RetryPolicy = DefaultRetryPolicy
	}

	if opts.ResponseReadLimit <= 0 {
		opts.ResponseReadLimit = DefaultResponseReadLimit
	}

	if opts.RetryErrorBodyLimit < 0 {
		opts.RetryErrorBodyLimit = 0
	}
}

// ClientOptionFunc configures a [Client] through the functional-options idiom.
//
// When the same option is supplied more than once, the last one wins.
//
// Parameters:
//   - opts (*ClientOptions): the options bag to mutate.
type ClientOptionFunc func(opts *ClientOptions)

// New creates a [Client] configured by the given options.
//
// Unset options fall back to defaults: [ProtocolAuto] over a pooled client,
// [DefaultUserAgent] as the default User-Agent header
// (replaceable with [WithHeader]), no execution timeout
// ([DefaultResponseBodyTimeout] adds no separate body-read bound), no
// per-attempt timeout, no retrier (a single attempt per execution),
// [DefaultRetryPolicy], idempotent-only retries, no rate limiter, no
// callbacks, no retry hook, no periodic idle-connection close,
// [DefaultResponseReadLimit] as the drain bound, retry-error body capture
// disabled ([DefaultRetryErrorBodyLimit] is zero), no response or request body
// cap, and [DefaultHTTP3FallbackTTL] for the HTTP/3 failure cache. A
// library-built client uses HTTP/1.x and HTTP/2 with environment proxying and
// no cookie jar; [WithTransport], [WithProxy], [WithCheckRedirect], and
// [WithCookieJar] change that. Invalid values are normalized the same way —
// see each option's documentation.
//
// Parameters:
//   - ofs (...ClientOptionFunc): functional options configuring the client.
//
// Returns:
//   - client (*Client): a new client, ready to execute requests.
func New(ofs ...ClientOptionFunc) (client *Client) {
	opts := &ClientOptions{
		Headers:             []HeaderField{{Key: hqgohttpheader.UserAgent.String(), Value: DefaultUserAgent}},
		Timeout:             DefaultTimeout,
		ResponseBodyTimeout: DefaultResponseBodyTimeout,
		RetryPolicy:         DefaultRetryPolicy,
		IdempotentRetries:   true,
		ResponseReadLimit:   DefaultResponseReadLimit,
		RetryErrorBodyLimit: DefaultRetryErrorBodyLimit,
		HTTP3FallbackTTL:    DefaultHTTP3FallbackTTL,
	}

	for _, of := range ofs {
		if of != nil {
			of(opts)
		}
	}

	opts.normalize()

	closeCtx, closeCancel := context.WithCancelCause(context.Background())

	client = &Client{
		config:      *opts,
		closeCtx:    closeCtx,
		closeCancel: closeCancel,
	}

	client.buildHTTPClients()

	return client
}

// Config returns a snapshot of the configuration resolved by [New].
//
// Mutating the snapshot does not affect the client. The Headers slice is
// copied; pointer, interface, and context fields are shared.
//
// Returns:
//   - options (ClientOptions): the resolved configuration.
func (c *Client) Config() (options ClientOptions) {
	options = c.config
	options.Headers = slices.Clone(c.config.Headers)

	return options
}

// WithHTTPClient returns a [ClientOptionFunc] that sets the underlying client
// executing requests.
//
// Supplying a custom client hands transport control (proxying, TLS settings,
// connection pooling, redirects) to the caller. The client is used as-is,
// unless [WithCheckRedirect] or [WithCookieJar] is supplied; those options
// clone it before applying their value, so the caller's client is never
// mutated. The fallback client is always a clone, so its redirect policy,
// cookie jar, and timeout survive. [WithTransport], [WithTransportWrapper],
// and [WithProxy] are ignored when a custom client is supplied.
//
// The client's Timeout field is left untouched. A non-zero value also bounds
// reading the returned response body, because net/http applies it to the whole
// exchange; use [WithTimeout] or a context deadline for an execution-only
// bound.
//
// Parameters:
//   - client (*http.Client): the client to execute requests with. Nil is
//     ignored, keeping the protocol-built client.
//
// Returns:
//   - f (ClientOptionFunc): a functional option that sets the httpClient field.
func WithHTTPClient(client *http.Client) (f ClientOptionFunc) {
	return func(opts *ClientOptions) {
		if client != nil {
			opts.HTTPClient = client
		}
	}
}

// WithHeader returns a [ClientOptionFunc] that registers a default header set
// on every request, replacing any previously registered default with the same
// key (case-insensitive). [Client.Do] applies the defaults to requests it
// receives directly, not only to requests built by [Client.Request].
//
// A request-level [RequestBuilder.SetHeader] replaces the default for the same
// key; [RequestBuilder.AddHeader] appends to it. Host maps to Request.Host;
// Content-Length, Transfer-Encoding, and connection-specific headers are
// managed by net/http and cannot be overridden this way. The User-Agent
// registered by [New] is an ordinary default header, so
// WithHeader("User-Agent", value) replaces it.
//
// Parameters:
//   - key (string): the header field name. Empty is ignored.
//   - value (string): the header field value.
//
// Returns:
//   - f (ClientOptionFunc): a functional option that registers the header.
func WithHeader(key, value string) (f ClientOptionFunc) {
	return func(opts *ClientOptions) {
		if key == "" {
			return
		}

		for i, h := range opts.Headers {
			if strings.EqualFold(h.Key, key) {
				opts.Headers[i].Value = value

				return
			}
		}

		opts.Headers = append(opts.Headers, HeaderField{Key: key, Value: value})
	}
}

// WithTimeout returns a [ClientOptionFunc] that bounds a request execution —
// the time through receipt of the response headers, including retries and the
// waits between them. It does not bound reading the returned response body;
// use [WithResponseBodyTimeout] for that. Use [WithAttemptTimeout] to bound a
// single attempt independently of the whole execution.
//
// The bound derives from the caller's context: an earlier deadline on that
// context still wins and applies to the body as well. A per-request timeout
// needs no option — derive it with context.WithTimeout on the context handed
// to the entry point.
//
// Parameters:
//   - timeout (time.Duration): the execution bound. A positive value sets the
//     bound; values less than or equal to 0 disable it. The execution is
//     unbounded by default; [RecommendedTimeout] restores the 30-second bound
//     earlier releases applied.
//
// Returns:
//   - f (ClientOptionFunc): a functional option that sets the timeout field.
func WithTimeout(timeout time.Duration) (f ClientOptionFunc) {
	return func(opts *ClientOptions) {
		opts.Timeout = timeout
	}
}

// WithContext returns a [ClientOptionFunc] that sets the default execution
// context. It applies when a request carries no explicit context. A context set
// on the request through [github.com/hueristiq/hq-lib-http-go/request.New] or
// [RequestBuilder.Context] overrides it, including an explicit
// [context.Background]. Passing nil to request.New leaves the client default in
// charge.
//
// A default with a deadline applies to every later execution, so prefer a
// per-request context for per-request deadlines. Passing nil leaves no
// default. When supplied more than once, the last option wins.
//
// Parameters:
//   - ctx (context.Context): the default execution context.
//
// Returns:
//   - f (ClientOptionFunc): a functional option that sets the defaultCtx field.
func WithContext(ctx context.Context) (f ClientOptionFunc) {
	return func(opts *ClientOptions) {
		opts.DefaultContext = ctx
	}
}

// WithResponseBodyTimeout returns a [ClientOptionFunc] that bounds reading a
// response body after [Client.Do] returns, independently of the execution
// timeout. The bound starts when the response body is handed out and stops
// when the body is closed or drained.
//
// The caller's context still bounds the body: an earlier deadline or a
// cancellation aborts reads. A non-positive value disables the separate
// bound, which is the default; body reads are then limited only by the
// caller's context.
//
// Parameters:
//   - timeout (time.Duration): the body-read bound. Values less than or equal
//     to 0 disable it.
//
// Returns:
//   - f (ClientOptionFunc): a functional option that sets the
//     responseBodyTimeout field.
func WithResponseBodyTimeout(timeout time.Duration) (f ClientOptionFunc) {
	return func(opts *ClientOptions) {
		opts.ResponseBodyTimeout = timeout
	}
}

// WithMaxResponseBodySize returns a [ClientOptionFunc] that caps the number of
// bytes [Body.Bytes], [Body.String], [Body.JSON], [Body.XML], and
// [Body.UTF8] read from a response. It guards against unexpectedly large or
// decompression-amplified bodies being buffered in memory.
//
// When the body exceeds the limit, the helpers return the prefix read up to
// the limit together with an error wrapping [ErrResponseBodyTooLarge]. The
// underlying body is closed either way. The limit applies only to the
// enhanced [Body] helpers; reading [Response.Std().Body] directly, or using
// [Client.RoundTrip] or [Client.Std], is unbounded.
//
// Parameters:
//   - maxResponseBodySize (int64): the maximum number of bytes to buffer.
//     Values less than or equal to 0 disable the cap, which is the default.
//
// Returns:
//   - f (ClientOptionFunc): a functional option that sets the
//     maxResponseBodySize field.
func WithMaxResponseBodySize(maxResponseBodySize int64) (f ClientOptionFunc) {
	return func(opts *ClientOptions) {
		opts.MaxResponseBodySize = maxResponseBodySize
	}
}

// WithMaxRequestBodySize returns a [ClientOptionFunc] that caps the number of
// bytes buffered into a request body. Request bodies are buffered in memory so
// they can be replayed on retries; this option rejects an oversized body
// instead of retaining it.
//
// Raw readers passed to [RequestBuilder.Body] are read through the cap. JSON and
// XML are encoded through a limit-aware writer; Form is checked after
// [net/url.Values.Encode]; multipart stops writing at the cap. A pre-built
// request with a known ContentLength is rejected by [Client.Do] and
// [Client.RoundTrip]. Streaming bodies with an unknown length are not capped;
// use [RequestBuilder.BodyStream] to avoid buffering entirely.
//
// Parameters:
//   - maxRequestBodySize (int64): the maximum buffered body size in bytes.
//     Values less than or equal to 0 disable the cap, which is the default.
//
// Returns:
//   - f (ClientOptionFunc): a functional option that sets the
//     maxRequestBodySize field.
func WithMaxRequestBodySize(maxRequestBodySize int64) (f ClientOptionFunc) {
	return func(opts *ClientOptions) {
		opts.MaxRequestBodySize = maxRequestBodySize
	}
}

// WithAttemptTimeout returns a [ClientOptionFunc] that bounds each retry-engine
// attempt through receipt of the response headers. It keeps one slow attempt
// from consuming the whole [WithTimeout] budget.
//
// The attempt bound starts after the rate-limiter wait and body preparation
// and covers the OnRequest callback, the round trip, and any protocol
// fallback. It stops when the response headers arrive, so reading the
// handed-out body is bounded by the caller's context and
// [WithResponseBodyTimeout], not by this value. An attempt that times out is
// retryable.
//
// The overall execution still obeys [WithTimeout] and the caller's context. A
// non-positive value disables the per-attempt bound, which is the default.
//
// Parameters:
//   - timeout (time.Duration): the per-attempt bound. Values less than or
//     equal to 0 disable it.
//
// Returns:
//   - f (ClientOptionFunc): a functional option that sets the attemptTimeout field.
func WithAttemptTimeout(timeout time.Duration) (f ClientOptionFunc) {
	return func(opts *ClientOptions) {
		if timeout > 0 {
			opts.AttemptTimeout = timeout
		} else {
			opts.AttemptTimeout = 0
		}
	}
}

// WithRetryPolicy returns a [ClientOptionFunc] that sets the policy deciding,
// after each attempt, whether the request is retried.
//
// The policy receives the overall execution context, the attempt request, the
// response, the error, and the 1-based attempt number. [WithRetryIdempotent]
// is enabled by default and restricts retries to idempotent methods without
// writing a policy.
//
// Parameters:
//   - retryPolicy (RetryPolicy): the policy. Nil selects [DefaultRetryPolicy],
//     which retries connection and transport errors, 429, and 5xx responses.
//
// Returns:
//   - f (ClientOptionFunc): a functional option that sets the retryPolicy field.
func WithRetryPolicy(retryPolicy RetryPolicy) (f ClientOptionFunc) {
	return func(opts *ClientOptions) {
		opts.RetryPolicy = retryPolicy
	}
}

// WithRetryIdempotent returns a [ClientOptionFunc] that restricts retries to
// idempotent methods, even when the retry policy approves another attempt.
// Idempotent methods are GET, HEAD, OPTIONS, TRACE, PUT, DELETE, and QUERY;
// POST and PATCH are not retried. It also restricts the internal HTTP/2 or
// HTTP/3 fallback resend.
//
// It is enabled by default. Pass false to retry every method, accepting that a
// POST or PATCH may be sent more than once.
//
// The policy still runs for every attempt, so it can inspect the outcome and
// return an abort error.
//
// Parameters:
//   - enabled (bool): true to retry only idempotent methods.
//
// Returns:
//   - f (ClientOptionFunc): a functional option that sets the idempotentRetries field.
func WithRetryIdempotent(enabled bool) (f ClientOptionFunc) {
	return func(opts *ClientOptions) {
		opts.IdempotentRetries = enabled
	}
}

// WithRateLimiter returns a [ClientOptionFunc] that registers a rate limiter
// waited on before every retry-engine attempt of every request. The internal
// HTTP/2 or HTTP/3 fallback resend belongs to the same attempt and does not
// wait again.
//
// The limiter is any value with a Wait method honoring a context — for
// example a *Limiter from github.com/hueristiq/hq-lib-limiter-go or a
// [RateLimiterFunc] — so no module dependency is imposed. A limiter shared
// across clients must be safe for concurrent use.
//
// Parameters:
//   - rateLimiter (RateLimiter): the limiter. Nil disables limiting.
//
// Returns:
//   - f (ClientOptionFunc): a functional option that sets the rateLimiter field.
func WithRateLimiter(rateLimiter RateLimiter) (f ClientOptionFunc) {
	return func(opts *ClientOptions) {
		opts.RateLimiter = rateLimiter
	}
}

// WithRetrier returns a [ClientOptionFunc] that registers the retry engine
// running the attempts of every request execution. The engine owns the attempt
// count, waits, and backoff; the client supplies the operation, its
// HTTP-specific hooks, and the parsed Retry-After wait through
// [RetrierOptions].
//
// The retrier is any value with a Retry method — for example an adapter around
// github.com/hueristiq/hq-lib-retrier-go built with [RetrierFunc] — so no
// module dependency is imposed. With no retrier, every execution makes a
// single attempt; a persistent 429 or 5xx outcome still becomes a
// [*RetryError] unless a policy or [WithOnRetryExhaustion] returns the final
// response. A retrier shared across clients must be safe for concurrent use.
//
// Parameters:
//   - retrier (Retrier): the retry engine. Nil disables retries.
//
// Returns:
//   - f (ClientOptionFunc): a functional option that sets the retrier field.
func WithRetrier(retrier Retrier) (f ClientOptionFunc) {
	return func(opts *ClientOptions) {
		opts.Retrier = retrier
	}
}

// WithURLParser returns a [ClientOptionFunc] that sets the parser
// [RequestBuilder] uses to turn a raw URL string into an absolute URL.
//
// The default is [net/url.Parse], which is strict: it applies no default
// scheme and repairs no invalid percent escapes. A parser adds either behavior
// — for example an adapter around github.com/hueristiq/hq-lib-url-go's parser
// built with [github.com/hueristiq/hq-lib-http-go/request.URLParserFunc] — so
// no module dependency is imposed. [WithRequestURLParser] overrides it for one
// request. A parser shared across clients must be safe for concurrent use.
//
// Parameters:
//   - parser (hqgohttprequest.URLParser): the URL parser. Nil keeps the
//     default.
//
// Returns:
//   - f (ClientOptionFunc): a functional option that sets the urlParser field.
func WithURLParser(parser hqgohttprequest.URLParser) (f ClientOptionFunc) {
	return func(opts *ClientOptions) {
		if parser != nil {
			opts.URLParser = parser
		}
	}
}

// WithOnRequest returns a [ClientOptionFunc] that registers the callback
// invoked with a [RequestEvent] before every retry-engine attempt of every
// request. The internal HTTP/2/HTTP/3 fallback resend does not trigger a
// second invocation. [WithRequestOnRequest] overrides it for one request.
//
// The callback is invoked synchronously on the goroutine running the attempt:
// a slow callback delays the attempt. It is observational — mutate the
// request only to add orthogonal metadata such as tracing headers. A callback
// must be safe for concurrent use, and a panicking callback propagates its
// panic to the caller of the entry point.
//
// Parameters:
//   - onRequest (OnRequestFunc): the callback. Nil drops the notification.
//
// Returns:
//   - f (ClientOptionFunc): a functional option that sets the onRequest field.
func WithOnRequest(onRequest OnRequestFunc) (f ClientOptionFunc) {
	return func(opts *ClientOptions) {
		opts.OnRequest = onRequest
	}
}

// WithOnResponse returns a [ClientOptionFunc] that registers the callback
// invoked with a [ResponseEvent] after every retry-engine attempt of every
// request, before the retry policy is consulted. The event carries the attempt
// number, the request, the response or error, and the attempt's duration. The
// internal HTTP/2 or HTTP/3 fallback resend is part of the same attempt; only
// its final outcome is reported. [WithRequestOnResponse] overrides it for one
// request.
//
// The callback is invoked synchronously on the goroutine running the attempt:
// a slow callback delays the retry decision. It is observational and must not
// close or drain the response body; the retry machinery owns the body until
// the final response is returned. A callback must be safe for concurrent use,
// and a panicking callback propagates its panic to the caller of the entry
// point.
//
// Parameters:
//   - onResponse (OnResponseFunc): the callback. Nil drops the notification.
//
// Returns:
//   - f (ClientOptionFunc): a functional option that sets the onResponse field.
func WithOnResponse(onResponse OnResponseFunc) (f ClientOptionFunc) {
	return func(opts *ClientOptions) {
		opts.OnResponse = onResponse
	}
}

// WithOnRetry returns a [ClientOptionFunc] that registers a callback invoked
// after the retry policy approves another attempt and before the wait starts.
//
// The callback runs synchronously on the retry loop's goroutine: a slow
// callback delays the next attempt. It must be safe for concurrent use across
// requests, and a panicking callback propagates its panic to the caller. When a
// response is retried, its body has already been drained or snapshotted and
// closed, so inspect the status and headers only.
//
// Parameters:
//   - onRetry (OnRetryFunc): the callback. Nil drops the notification.
//
// Returns:
//   - f (ClientOptionFunc): a functional option that sets the onRetry field.
func WithOnRetry(onRetry OnRetryFunc) (f ClientOptionFunc) {
	return func(opts *ClientOptions) {
		opts.OnRetry = onRetry
	}
}

// WithPrepareRetry returns a [ClientOptionFunc] that registers a hook invoked
// before each retry attempt, after the retry wait and after the request body
// has been rewound. Use it to re-sign a request whose signature or token
// expires between attempts; returning an error stops the execution and returns
// that error.
//
// [WithOnRetry] runs before the wait and is observational; PrepareRetry runs
// after the wait and may abort. A nil hook is ignored, and a panicking hook
// propagates its panic. [WithRequestPrepareRetry] overrides it for one request.
//
// Parameters:
//   - prepareRetry (PrepareRetryFunc): the hook. Nil drops it.
//
// Returns:
//   - f (ClientOptionFunc): a functional option that sets the prepareRetry field.
func WithPrepareRetry(prepareRetry PrepareRetryFunc) (f ClientOptionFunc) {
	return func(opts *ClientOptions) {
		if prepareRetry != nil {
			opts.PrepareRetry = prepareRetry
		}
	}
}

// WithOnRetryExhaustion returns a [ClientOptionFunc] that decides what an
// execution returns when retries run out. The handler receives the final
// response with its body open, the last failure, and the attempt count.
//
// Returning a response hands it to the caller, who owns its body; when the
// handler returns a different response, it also owns the original body.
// Returning a nil response closes any response body and returns the handler's
// error, or a [*RetryError] when the handler returns no error. The default,
// nil, returns a [*RetryError] with a bounded body snapshot.
//
// The handler runs synchronously on the retry loop's goroutine and must be
// safe for concurrent use. A panicking handler propagates its panic.
//
// Parameters:
//   - handler (RetryExhaustionHandler): the handler. Nil restores the default
//     behavior.
//
// Returns:
//   - f (ClientOptionFunc): a functional option that sets the
//     retryExhaustionHandler field.
func WithOnRetryExhaustion(handler RetryExhaustionHandler) (f ClientOptionFunc) {
	return func(opts *ClientOptions) {
		opts.RetryExhaustionHandler = handler
	}
}

// WithMaxRequestsBeforeIdleClose returns a [ClientOptionFunc] that sets how
// many request executions complete before the client closes its idle
// connections, freeing file descriptors held by the connection pool.
//
// Long-running processes making many requests to changing hosts can leak
// idle connections; a positive threshold periodically reclaims them.
//
// Parameters:
//   - n (int): the number of completed executions between idle-connection
//     closes. Values less than or equal to 0 disable the periodic closing.
//
// Returns:
//   - f (ClientOptionFunc): a functional option that sets the
//     maxRequestsBeforeIdleClose field.
func WithMaxRequestsBeforeIdleClose(n int) (f ClientOptionFunc) {
	return func(opts *ClientOptions) {
		opts.MaxRequestsBeforeIdleClose = n
	}
}

// WithResponseReadLimit returns a [ClientOptionFunc] that sets the maximum
// number of bytes drained from the body of a response that will be retried.
//
// Draining the body of a retried response lets the transport reuse the
// underlying connection; the limit bounds the work spent draining large
// bodies. The drain reads one byte past the bound so a body of exactly the
// bound reaches EOF and the connection can still be reused.
//
// Parameters:
//   - responseReadLimit (int64): the drain bound in bytes. Values less than
//     or equal to 0 fall back to [DefaultResponseReadLimit].
//
// Returns:
//   - f (ClientOptionFunc): a functional option that sets the responseReadLimit field.
func WithResponseReadLimit(responseReadLimit int64) (f ClientOptionFunc) {
	return func(opts *ClientOptions) {
		opts.ResponseReadLimit = responseReadLimit
	}
}

// WithRetryErrorBodyLimit returns a [ClientOptionFunc] that sets the maximum
// number of bytes from the final response body that a [RetryError] carries.
//
// The final response's body is closed when retries are exhausted, so this
// snapshot preserves the server's error payload. Body capture is disabled by
// default; a positive value enables it and bounds the memory retained by the
// error. Response payloads can contain credentials or personal data, so redact
// [RetryError.Body], [RetryError.Header], and secret query parameters in
// [RetryError.URL] before logging.
//
// Parameters:
//   - retryErrorBodyLimit (int64): the snapshot bound in bytes. A value of 0
//     disables body capture; a negative value is normalized to 0. Use
//     [RecommendedRetryErrorBodyLimit] for the conventional 64 KiB bound.
//
// Returns:
//   - f (ClientOptionFunc): a functional option that sets the retryErrorBodyLimit field.
func WithRetryErrorBodyLimit(retryErrorBodyLimit int64) (f ClientOptionFunc) {
	return func(opts *ClientOptions) {
		opts.RetryErrorBodyLimit = retryErrorBodyLimit
	}
}

// WithRequestRetrier returns a [RequestOptionFunc] that overrides the retry
// engine for a single request. The engine owns the attempt count, waits, and
// backoff, so the override is the way to change them per request.
//
// Unlike [WithRequestRateLimiter], nil is not ignored: it disables retries for
// that execution, so a request can opt out of a client-wide engine. Omit the
// option to keep the client configuration.
//
// Parameters:
//   - retrier (Retrier): the retry engine. Nil disables retries for the
//     request.
//
// Returns:
//   - f (RequestOptionFunc): a functional option that sets the retrier field.
func WithRequestRetrier(retrier Retrier) (f RequestOptionFunc) {
	return func(opts *RequestOptions) {
		opts.Retrier = retrier
	}
}

// WithRequestURLParser returns a [RequestOptionFunc] that overrides the URL
// parser for a single request built by [RequestBuilder]. A pre-built
// [github.com/hueristiq/hq-lib-http-go/request.Request] already carries its
// parsed URL, so the option has no effect on it.
//
// The default is [net/url.Parse]; a parser can add a default scheme, repair
// invalid percent escapes, or apply any other transformation. Nil is ignored,
// keeping the client configuration.
//
// Parameters:
//   - parser (hqgohttprequest.URLParser): the URL parser. Nil keeps the client
//     configuration.
//
// Returns:
//   - f (RequestOptionFunc): a functional option that sets the urlParser field.
func WithRequestURLParser(parser hqgohttprequest.URLParser) (f RequestOptionFunc) {
	return func(opts *RequestOptions) {
		if parser != nil {
			opts.URLParser = parser
		}
	}
}

// WithRequestRetryPolicy returns a [RequestOptionFunc] that overrides the
// retry policy for a single request.
//
// Parameters:
//   - retryPolicy (RetryPolicy): the policy. Nil is ignored, keeping the
//     client configuration.
//
// Returns:
//   - f (RequestOptionFunc): a functional option that sets the retryPolicy field.
func WithRequestRetryPolicy(retryPolicy RetryPolicy) (f RequestOptionFunc) {
	return func(opts *RequestOptions) {
		if retryPolicy != nil {
			opts.RetryPolicy = retryPolicy
		}
	}
}

// WithRequestRetryIdempotent returns a [RequestOptionFunc] that overrides the
// idempotency restriction for a single request. When enabled, only GET, HEAD,
// OPTIONS, TRACE, PUT, DELETE, and QUERY are retried. Pass false to retry POST
// or PATCH for this request, accepting that it may be sent more than once.
//
// Parameters:
//   - enabled (bool): true to retry only idempotent methods.
//
// Returns:
//   - f (RequestOptionFunc): a functional option that sets the idempotentRetries field.
func WithRequestRetryIdempotent(enabled bool) (f RequestOptionFunc) {
	return func(opts *RequestOptions) {
		opts.IdempotentRetries = enabled
	}
}

// WithRequestRateLimiter returns a [RequestOptionFunc] that overrides the
// rate limiter waited on before every retry-engine attempt of a single
// request. The internal fallback resend does not wait again. The limiter is
// any [RateLimiter], including a [RateLimiterFunc].
//
// Parameters:
//   - rateLimiter (RateLimiter): the limiter. Nil is ignored, keeping the
//     client configuration.
//
// Returns:
//   - f (RequestOptionFunc): a functional option that sets the rateLimiter field.
func WithRequestRateLimiter(rateLimiter RateLimiter) (f RequestOptionFunc) {
	return func(opts *RequestOptions) {
		if rateLimiter != nil {
			opts.RateLimiter = rateLimiter
		}
	}
}

// WithRequestOnRequest returns a [RequestOptionFunc] that overrides the
// pre-attempt callback for a single request.
//
// Parameters:
//   - onRequest (OnRequestFunc): the callback. Nil is ignored, keeping the
//     client configuration.
//
// Returns:
//   - f (RequestOptionFunc): a functional option that sets the onRequest field.
func WithRequestOnRequest(onRequest OnRequestFunc) (f RequestOptionFunc) {
	return func(opts *RequestOptions) {
		if onRequest != nil {
			opts.OnRequest = onRequest
		}
	}
}

// WithRequestOnResponse returns a [RequestOptionFunc] that overrides the
// post-attempt callback for a single request.
//
// Parameters:
//   - onResponse (OnResponseFunc): the callback. Nil is ignored, keeping the
//     client configuration.
//
// Returns:
//   - f (RequestOptionFunc): a functional option that sets the onResponse field.
func WithRequestOnResponse(onResponse OnResponseFunc) (f RequestOptionFunc) {
	return func(opts *RequestOptions) {
		if onResponse != nil {
			opts.OnResponse = onResponse
		}
	}
}

// WithRequestOnRetry returns a [RequestOptionFunc] that overrides the retry
// hook for a single request.
//
// Parameters:
//   - onRetry (OnRetryFunc): the callback. Nil is ignored, keeping the client
//     configuration.
//
// Returns:
//   - f (RequestOptionFunc): a functional option that sets the onRetry field.
func WithRequestOnRetry(onRetry OnRetryFunc) (f RequestOptionFunc) {
	return func(opts *RequestOptions) {
		if onRetry != nil {
			opts.OnRetry = onRetry
		}
	}
}

// WithRequestPrepareRetry returns a [RequestOptionFunc] that overrides the
// pre-retry hook for a single request.
//
// Parameters:
//   - prepareRetry (PrepareRetryFunc): the hook. Nil is ignored, keeping the
//     client configuration.
//
// Returns:
//   - f (RequestOptionFunc): a functional option that sets the prepareRetry field.
func WithRequestPrepareRetry(prepareRetry PrepareRetryFunc) (f RequestOptionFunc) {
	return func(opts *RequestOptions) {
		if prepareRetry != nil {
			opts.PrepareRetry = prepareRetry
		}
	}
}

// WithRequestOnRetryExhaustion returns a [RequestOptionFunc] that
// overrides the retry-exhaustion handler for a single request.
//
// Parameters:
//   - handler (RetryExhaustionHandler): the handler. Nil is ignored, keeping
//     the client configuration.
//
// Returns:
//   - f (RequestOptionFunc): a functional option that sets the
//     retryExhaustionHandler field.
func WithRequestOnRetryExhaustion(handler RetryExhaustionHandler) (f RequestOptionFunc) {
	return func(opts *RequestOptions) {
		if handler != nil {
			opts.RetryExhaustionHandler = handler
		}
	}
}

// WithRequestAttemptTimeout returns a [RequestOptionFunc] that overrides the
// per-attempt timeout for a single request. The bound covers the attempt
// through receipt of the response headers; body reads stay tied to the overall
// execution context and [WithResponseBodyTimeout].
//
// A non-positive value disables the per-attempt bound for the request, even
// when the client sets one.
//
// Parameters:
//   - timeout (time.Duration): the per-attempt bound.
//
// Returns:
//   - f (RequestOptionFunc): a functional option that sets the attemptTimeout field.
func WithRequestAttemptTimeout(timeout time.Duration) (f RequestOptionFunc) {
	return func(opts *RequestOptions) {
		if timeout > 0 {
			opts.AttemptTimeout = timeout
		} else {
			opts.AttemptTimeout = 0
		}
	}
}

// WithRequestMaxResponseBodySize returns a [RequestOptionFunc] that overrides
// the response-body cap for a single request. A value of 0 removes the cap for
// the request, even when the client configured one; a negative value is
// ignored, keeping the client configuration.
//
// Parameters:
//   - maxResponseBodySize (int64): the maximum number of bytes to buffer.
//
// Returns:
//   - f (RequestOptionFunc): a functional option that sets the
//     maxResponseBodySize field.
func WithRequestMaxResponseBodySize(maxResponseBodySize int64) (f RequestOptionFunc) {
	return func(opts *RequestOptions) {
		if maxResponseBodySize >= 0 {
			opts.MaxResponseBodySize = maxResponseBodySize
		}
	}
}

// WithRequestResponseReadLimit returns a [RequestOptionFunc] that overrides
// the maximum number of bytes drained from the body of a retried response of
// a single request. The drain reads one byte past the bound to detect EOF.
//
// Parameters:
//   - responseReadLimit (int64): the drain bound in bytes. Values less than
//     or equal to 0 are ignored, keeping the client configuration.
//
// Returns:
//   - f (RequestOptionFunc): a functional option that sets the responseReadLimit field.
func WithRequestResponseReadLimit(responseReadLimit int64) (f RequestOptionFunc) {
	return func(opts *RequestOptions) {
		if responseReadLimit > 0 {
			opts.ResponseReadLimit = responseReadLimit
		}
	}
}

// WithRequestRetryErrorBodyLimit returns a [RequestOptionFunc] that overrides
// the maximum number of bytes from the final response body that a
// [RetryError] carries for a single request.
//
// Parameters:
//   - retryErrorBodyLimit (int64): the snapshot bound in bytes. A value of 0
//     disables body capture for the request, even when the client enabled it;
//     a negative value is ignored, keeping the client configuration.
//
// Returns:
//   - f (RequestOptionFunc): a functional option that sets the retryErrorBodyLimit field.
func WithRequestRetryErrorBodyLimit(retryErrorBodyLimit int64) (f RequestOptionFunc) {
	return func(opts *RequestOptions) {
		if retryErrorBodyLimit >= 0 {
			opts.RetryErrorBodyLimit = retryErrorBodyLimit
		}
	}
}

// WithProtocol returns a [ClientOptionFunc] that selects the HTTP protocol
// mode. The zero value, [ProtocolAuto], is the default.
//
// When [WithHTTPClient] or [WithTransport] is also supplied, that client or
// transport takes precedence and the protocol mode is ignored. Use
// [WithTransportWrapper] to decorate a protocol-built transport without
// losing the selection.
//
// HTTP/3 does not support proxies. [ProtocolAutoHTTP3] sends a request to
// which a proxy applies through the auto client instead, and [ProtocolHTTP3]
// returns [ErrHTTP3ProxyUnsupported] for such a request.
//
// Parameters:
//   - protocol (Protocol): the protocol mode. Invalid values fall back to
//     [ProtocolAuto].
//
// Returns:
//   - f (ClientOptionFunc): a functional option that sets the protocol field.
func WithProtocol(protocol Protocol) (f ClientOptionFunc) {
	return func(opts *ClientOptions) {
		opts.Protocol = protocol
	}
}

// WithHTTP3FallbackTTL returns a [ClientOptionFunc] that sets how long a host
// whose HTTP/3 attempt failed is sent through the HTTP/1.1+HTTP/2 client
// before HTTP/3 is tried again. The cache applies only to
// [ProtocolAutoHTTP3]; forced HTTP/3 still tries HTTP/3 on every request.
//
// A host that succeeds over HTTP/3 is removed from the cache immediately. A
// non-positive value disables the cache, restoring a fresh HTTP/3 attempt per
// request.
//
// Parameters:
//   - ttl (time.Duration): the failure retention period.
//
// Returns:
//   - f (ClientOptionFunc): a functional option that sets the
//     http3FallbackTTL field.
func WithHTTP3FallbackTTL(ttl time.Duration) (f ClientOptionFunc) {
	return func(opts *ClientOptions) {
		opts.HTTP3FallbackTTL = ttl
	}
}

// RequestEvent describes an outgoing attempt, passed to an [OnRequestFunc].
//
// Fields:
//   - Attempt (int): the 1-based attempt number.
//   - Request (*http.Request): the request about to be sent.
type RequestEvent struct {
	Attempt int
	Request *http.Request
}

// OnRequestFunc is the callback invoked with the outgoing attempt before
// every retry-engine attempt of every request, registered with
// [WithOnRequest] or [WithRequestOnRequest]. The internal HTTP/2 or HTTP/3
// fallback resend is part of the same attempt and does not trigger a second
// callback.
//
// Parameters:
//   - event (RequestEvent): the attempt number and the request about to be
//     sent.
type OnRequestFunc func(event RequestEvent)

// ResponseEvent describes the outcome of an attempt, passed to an
// [OnResponseFunc].
//
// Fields:
//   - Attempt (int): the 1-based attempt number.
//   - Request (*http.Request): the attempt request.
//   - Response (*http.Response): the response received on the attempt, or nil.
//   - Err (error): the attempt's transport error, or nil. The retry policy
//     classifies a response's status after this callback returns.
//   - Duration (time.Duration): the elapsed time of the attempt, including an
//     internal protocol fallback resend.
type ResponseEvent struct {
	Attempt  int
	Request  *http.Request
	Response *http.Response
	Err      error
	Duration time.Duration
}

// OnResponseFunc is the callback invoked with the outcome of every
// retry-engine attempt of every request — before the retry policy is
// consulted — registered with [WithOnResponse] or
// [WithRequestOnResponse]. The internal HTTP/2 or HTTP/3 fallback resend is
// part of the same attempt; only the final outcome of that attempt is
// reported.
//
// Parameters:
//   - event (ResponseEvent): the attempt's outcome and duration.
type OnResponseFunc func(event ResponseEvent)

// RetryEvent describes one attempt that will be followed by another. It is
// passed to an [OnRetryFunc] after the retry policy approves the retry and the
// computed wait is known.
//
// Fields:
//   - Attempt (int): the 1-based number of the attempt that just failed.
//   - Request (*http.Request): the attempt request. Its attempt context is
//     canceled after the response body is released, so a hook that needs a live
//     context should clone it, for example req.Clone(context.Background()).
//   - Response (*http.Response): the attempt's response, or nil. When a
//     response is retried, its body has already been drained or snapshotted and
//     closed; inspect the status and headers only.
//   - Err (error): the attempt's failure, with the internal retry marker
//     removed.
//   - Wait (time.Duration): the delay before the next attempt, after clamping.
//   - Duration (time.Duration): the elapsed time of the failed attempt.
type RetryEvent struct {
	Attempt  int
	Request  *http.Request
	Response *http.Response
	Err      error
	Wait     time.Duration
	Duration time.Duration
}

// OnRetryFunc is the callback invoked before each retry wait, registered with
// [WithOnRetry] or [WithRequestOnRetry].
//
// The callback runs synchronously on the retry loop's goroutine, so a slow
// callback delays the next attempt. It must be safe for concurrent use across
// requests. A panicking callback propagates its panic to the caller.
//
// Parameters:
//   - event (RetryEvent): the failed attempt and the wait that follows it.
type OnRetryFunc func(event RetryEvent)

// PrepareRetryFunc is invoked immediately before a retry attempt, after the
// retry wait and after the request body has been rewound. It is registered
// with [WithPrepareRetry] or [WithRequestPrepareRetry].
//
// The hook is the place to re-sign a request (for example AWS SigV4 or a
// short-lived bearer token) because it runs after the wait and before the next
// send. Returning a non-nil error stops the execution and returns that error
// to the caller instead of a [RetryError].
//
// The hook runs synchronously on the retry loop's goroutine and must be safe
// for concurrent use across requests. A panicking hook propagates its panic.
//
// Parameters:
//   - ctx (context.Context): the overall execution context.
//   - req (*http.Request): the request about to be retried.
//   - attempt (int): the 1-based number of the attempt about to start.
//
// Returns:
//   - err (error): non-nil to abort the execution.
type PrepareRetryFunc func(ctx context.Context, req *http.Request, attempt int) error

// RetryExhaustionHandler decides what an execution returns when retries run
// out. It receives the final response with its body open, the last attempt's
// failure, and the number of attempts made.
//
// A returned response is handed to the caller, who owns its body. When the
// handler returns a different response, it also owns the original response
// body; the client does not close it. When the handler returns a nil
// response, the client closes any response body and returns the handler's
// error; when the handler also returns no error, the client returns a
// [*RetryError].
//
// The handler runs synchronously on the retry loop's goroutine and must be
// safe for concurrent use. A panicking handler propagates its panic.
//
// Parameters:
//   - resp (*http.Response): the final response, or nil when the last attempt
//     failed before a response arrived.
//   - err (error): the last attempt's failure.
//   - attempts (int): the number of attempts made, counting the first.
//
// Returns:
//   - handled (*http.Response): the response to hand to the caller, or nil.
//   - handledErr (error): the error to return with it.
type RetryExhaustionHandler func(resp *http.Response, err error, attempts int) (*http.Response, error)

// Do executes req with retries driven by the resolved configuration: the
// client configuration overlaid with ofs.
//
// Before the first attempt, Do applies the client's default headers, including
// the default User-Agent, to a copy of the request. A header already present
// on the request wins. The caller's request and header map are never mutated.
//
// The execution uses the request's context when it is not
// [context.Background]; otherwise the [WithContext] client default applies,
// followed by [context.Background]. Every attempt and every wait between them,
// through receipt of the response headers, is bounded by that context, tightened
// by the configured [WithTimeout] value when the context carries no earlier
// deadline. [WithAttemptTimeout] optionally bounds each attempt through receipt
// of the headers, so one slow attempt does not consume the whole execution
// budget. The returned body stays tied to the context after Do returns: the
// caller streams it, reads fail once the context ends, and
// [WithResponseBodyTimeout] optionally bounds the body independently of the
// execution timeout.
//
// Each attempt waits on the configured rate limiter, runs the OnRequest
// callback with a [RequestEvent], executes the request, runs the OnResponse
// callback with a [ResponseEvent] carrying the attempt number and duration,
// and consults the retry policy. Retries are idempotent-only by default, so a
// POST or PATCH stops after its first response unless
// WithRetryIdempotent(false) or WithRequestRetryIdempotent(false) opts
// out. Attempts run only through the [Retrier] configured with [WithRetrier];
// without one the execution makes a single attempt. When the policy approves a
// retry, the engine invokes the OnRetry callback before its wait. A request
// body built through the request package or
// [Client.Request] is reusable, so every attempt resends it in full; a body
// that cannot be rewound stops with the last outcome instead. When the primary
// client fails with the HTTP/1.x transport broken error, an idempotent request
// with a replayable body is retried once on the HTTP/2 fallback client before
// the policy is consulted. The fallback resend is internal: it does not
// trigger a second callback pair or rate-limiter wait.
//
// Outcomes:
//   - Success: the final response with its body open — the caller owns it and
//     must close it. Closing or draining the body releases the request
//     context; [WithResponseBodyTimeout] can bound the reads independently.
//   - Attempts exhausted on retryable failures: a nil response and a
//     [*RetryError] carrying the attempt count, status, headers, the attempt
//     history, and the last failure. A bounded body snapshot is included only
//     when body capture is enabled with [WithRetryErrorBodyLimit]; a read
//     error from that capture is exposed as [RetryError.BodyErr]. A configured
//     [RetryExhaustionHandler] replaces this outcome. With no retrier, the
//     single attempt still classifies this way, so a persistent 429 or 5xx
//     returns a [*RetryError].
//   - A body that cannot be replayed on an approved retry: a nil response and
//     a [*RetryError] built from the last response, without another attempt.
//     The error matches [ErrBodyNotReplayable] with errors.Is and does not
//     match [ErrRetriesExhausted]. A handler replaces it.
//   - Response with an error: the attempt's response — body open,
//     caller-owned — and the error. The default policy treats this as final, so
//     a redirect-policy abort is not retried.
//   - Context done: a nil response and the context's cause. A rate limiter
//     failing because the context ended reports the cause too.
//   - Retry policy abort (non-nil rerr): the attempt's response — body open,
//     caller-owned — and rerr.
//
// Parameters:
//   - req (*hqgohttprequest.Request): the request to execute.
//   - ofs (...RequestOptionFunc): per-request overrides of the execution
//     knobs; the client configuration stays untouched.
//
// Returns:
//   - res (*Response): the final response wrapper, or nil on failure (except a
//     policy abort or an exhaustion handler that returns a response). The raw
//     response remains available as res.Std().
//   - err (error): non-nil when the execution fails.
func (c *Client) Do(req *hqgohttprequest.Request, ofs ...RequestOptionFunc) (res *Response, err error) {
	opts := c.requestOptions(ofs...)

	res, err = c.doWrapped(req, opts)

	return
}

// doWrapped executes req with the resolved options and wraps the raw response.
// It backs [Client.Do] and [RequestBuilder.Do].
//
// Parameters:
//   - req (*hqgohttprequest.Request): the request to execute.
//   - opts (*RequestOptions): the resolved per-request configuration.
//
// Returns:
//   - res (*Response): the wrapped response, or nil on failure.
//   - err (error): non-nil when the execution fails.
func (c *Client) doWrapped(req *hqgohttprequest.Request, opts *RequestOptions) (res *Response, err error) {
	raw, doErr := c.do(req, opts) //nolint:bodyclose // The response body is handed to the caller.
	if raw == nil {
		return nil, doErr
	}

	return newResponse(raw, opts.MaxResponseBodySize), doErr
}

// do executes req with the resolved options and returns the raw response. It
// backs [Client.Do] and [Client.RoundTrip].
//
// Parameters:
//   - req (*hqgohttprequest.Request): the request to execute.
//   - opts (*RequestOptions): the resolved per-request configuration.
//
// Returns:
//   - res (*http.Response): the raw response, or nil on failure.
//   - err (error): non-nil when the execution fails.
func (c *Client) do(req *hqgohttprequest.Request, opts *RequestOptions) (res *http.Response, err error) {
	if req == nil || req.Request == nil {
		err = fmt.Errorf("http.Client.Do: %w", ErrNilRequest)

		return nil, err
	}

	if req.URL == nil {
		err = fmt.Errorf("http.Client.Do: %w", ErrNilRequestURL)

		return nil, err
	}

	if c.closed.Load() {
		err = fmt.Errorf("http.Client.Do: %w", ErrClientClosed)

		return nil, err
	}

	ctx := c.executionContext(req.Request)

	// reqCtx stays alive until the response body is closed or drained, so the
	// caller can stream the body after Do returns. The execution timer bounds
	// the time through the response headers; the body timer, when configured,
	// bounds reading the handed-out body independently.
	reqCtx, cancel := context.WithCancelCause(ctx)

	stopCloseWatch := func() bool {
		return true
	}

	if c.closeCtx != nil {
		stopCloseWatch = context.AfterFunc(c.closeCtx, func() {
			cancel(ErrClientClosed)
		})
	}

	var (
		execTimer         *time.Timer
		lastAttemptCancel context.CancelCauseFunc
	)

	if c.config.Timeout > 0 {
		execTimer = time.AfterFunc(c.config.Timeout, func() {
			cancel(context.DeadlineExceeded)
		})
	}

	// Release the context (and the execution timer) when no body is handed
	// out. A handed-out body owns the context until it is closed or drained.
	defer func() {
		if res == nil {
			if execTimer != nil {
				execTimer.Stop()
			}

			if lastAttemptCancel != nil {
				lastAttemptCancel(nil)
			}

			stopCloseWatch()

			cancel(nil)
		}
	}()

	httpRequest := req.WithContext(reqCtx)
	httpRequest.Header = req.Header.Clone()

	applyDefaultHeaders(httpRequest, c.config.Headers)

	if opts.MaxRequestBodySize > 0 && httpRequest.ContentLength > opts.MaxRequestBodySize {
		err = fmt.Errorf("http.Client.Do: %w (limit %d bytes)", ErrRequestBodyTooLarge, opts.MaxRequestBodySize)

		return nil, err
	}

	req = &hqgohttprequest.Request{Request: httpRequest}

	// HTTP/3 has no proxy support. Resolve the proxy that applies to this
	// request so an auto client skips H3, and a forced HTTP/3 client fails
	// instead of sending a request around the configured proxy.
	primaryClient, fallbackClient := c.config.HTTPClient, c.fallbackHTTPClient
	fallbackOnHTTP3 := c.fallbackOnHTTP3

	if c.http3Primary {
		proxyURL, perr := c.proxyFor(httpRequest)
		if perr != nil || proxyURL != nil {
			if !c.fallbackOnHTTP3 {
				if perr != nil {
					err = fmt.Errorf("http.Client.Do: %w", perr)

					return nil, err
				}

				err = fmt.Errorf("http.Client.Do: %w", ErrHTTP3ProxyUnsupported)

				return nil, err
			}

			// Auto mode: use the HTTP/1.1+HTTP/2 client for this request.
			primaryClient, fallbackClient = c.fallbackHTTPClient, nil
			fallbackOnHTTP3 = false
		}
	}

	// A host that recently failed an HTTP/3 attempt is sent through the auto
	// client for the duration of the fallback TTL, so an H3-disabled or
	// UDP-blocked host does not pay a QUIC handshake on every request.
	http3Attempt := fallbackOnHTTP3
	if http3Attempt && c.http3RecentlyFailed(httpRequest.URL.Host) {
		primaryClient, fallbackClient = c.fallbackHTTPClient, nil
		fallbackOnHTTP3 = false
		http3Attempt = false
	}

	// retryAfter carries the wait parsed from a Retry-After header of the
	// attempt that was approved for retry, exposed to the engine through
	// [RetrierOptions.RetryAfter]. retryAfterSet distinguishes an honored zero
	// wait from an absent header. Both run on the retry loop's goroutine, so
	// plain variables are safe.
	var (
		retryAfter    time.Duration
		retryAfterSet bool
	)

	// attempt counts the retry-engine attempts. A reusable body is reset on
	// the first attempt so a request reused after a partial send starts from
	// the beginning; later attempts reuse the body prepared after the previous
	// attempt was approved for retry.
	attempt := 0

	var (
		lastAttemptRequest  *http.Request
		lastAttemptResponse *http.Response
		lastAttemptDuration time.Duration
		lastResponseClosed  bool
		history             []RetryAttempt
	)

	operation := func() (attemptRes *http.Response, attemptErr error) {
		retryAfter, retryAfterSet = 0, false

		if opts.RateLimiter != nil {
			if werr := opts.RateLimiter.Wait(reqCtx); werr != nil {
				if cause := context.Cause(reqCtx); cause != nil {
					attemptErr = cause
				} else {
					attemptErr = fmt.Errorf("http.Client.Do: waiting on rate limiter: %w", werr)
				}

				return attemptRes, attemptErr
			}
		}

		attempt++

		if attempt == 1 {
			resetReusableRequestBody(req.Request)
		}

		attemptReq := req.Request

		var (
			attemptCancel context.CancelCauseFunc
			attemptTimer  *time.Timer
		)

		if opts.AttemptTimeout > 0 {
			attemptCtx, cancelAttempt := context.WithCancelCause(reqCtx)

			attemptCancel = cancelAttempt
			attemptReq = req.WithContext(attemptCtx)
			attemptTimer = time.AfterFunc(opts.AttemptTimeout, func() {
				cancelAttempt(context.DeadlineExceeded)
			})
		}

		lastAttemptCancel = attemptCancel
		lastAttemptRequest = attemptReq

		if attempt > 1 && opts.PrepareRetry != nil {
			if perr := opts.PrepareRetry(reqCtx, attemptReq, attempt); perr != nil {
				if attemptCancel != nil {
					attemptCancel(nil)
				}

				return nil, fmt.Errorf("http.Client.Do: preparing retry attempt %d: %w", attempt, perr)
			}
		}

		if opts.OnRequest != nil {
			opts.OnRequest(RequestEvent{Attempt: attempt, Request: attemptReq})
		}

		attemptStart := time.Now()

		attemptRes, attemptErr = primaryClient.Do(attemptReq)

		if http3Attempt {
			switch {
			case attemptErr == nil && attemptRes != nil:
				c.clearHTTP3Failure(httpRequest.URL.Host)
			case attemptRes == nil && attemptErr != nil && shouldFallback(true, attemptErr):
				c.recordHTTP3Failure(httpRequest.URL.Host)
			}
		}

		resend := attemptRes == nil && attemptErr != nil && fallbackClient != nil &&
			shouldFallback(fallbackOnHTTP3, attemptErr) &&
			canResendRequest(attemptReq, opts.IdempotentRetries)

		if resend {
			if rerr := rewindRequestBody(attemptReq); rerr != nil {
				attemptErr = errors.Join(attemptErr, rerr)
			} else {
				primaryErr := attemptErr

				attemptRes, attemptErr = fallbackClient.Do(attemptReq)
				if attemptErr != nil {
					attemptErr = errors.Join(primaryErr, attemptErr)
				}
			}
		}

		if attemptTimer != nil && !attemptTimer.Stop() {
			// The timer fired at the boundary. Do not hand out a response that
			// arrived after the attempt budget.
			if attemptRes != nil && attemptRes.Body != nil {
				_ = attemptRes.Body.Close()
			}

			attemptRes = nil
			attemptErr = context.DeadlineExceeded
		}

		lastAttemptDuration = time.Since(attemptStart)
		lastAttemptResponse = attemptRes
		lastResponseClosed = false

		if opts.OnResponse != nil {
			opts.OnResponse(ResponseEvent{
				Attempt:  attempt,
				Request:  attemptReq,
				Response: attemptRes,
				Err:      attemptErr,
				Duration: lastAttemptDuration,
			})
		}

		attemptRecord := RetryAttempt{
			Attempt:  attempt,
			Err:      attemptErr,
			Duration: lastAttemptDuration,
		}

		if attemptRes != nil {
			attemptRecord.StatusCode = attemptRes.StatusCode
			attemptRecord.Status = attemptRes.Status
		}

		history = append(history, attemptRecord)

		retry, rerr := opts.RetryPolicy(reqCtx, attemptReq, attemptRes, attemptErr, attempt)
		if rerr != nil {
			attemptErr = rerr

			if (attemptRes == nil || attemptRes.Body == nil) && attemptCancel != nil {
				attemptCancel(nil)
			}

			return attemptRes, attemptErr
		}

		if retry && opts.IdempotentRetries && !isIdempotentMethod(attemptReq.Method) {
			retry = false
		}

		final := false

		var replayErr error

		if retry {
			// Prepare the body while the current response is still in scope, so
			// a failed GetBody becomes a final outcome rather than a bare rewind
			// error on the next attempt.
			if rerr := rewindRequestBody(req.Request); rerr != nil {
				retry = false
				final = true
				replayErr = rerr
			}
		}

		if !retry && !final {
			if (attemptRes == nil || attemptRes.Body == nil) && attemptCancel != nil {
				attemptCancel(nil)
			}

			return attemptRes, attemptErr
		}

		if attemptRes != nil {
			if wait, ok := retryAfterWait(attemptRes); ok {
				retryAfter = wait
				retryAfterSet = true
			}
		}

		switch {
		case replayErr != nil:
			replayErr = fmt.Errorf("http.Client.Do: %w", replayErr)
			if attemptErr == nil {
				attemptErr = replayErr
			} else {
				attemptErr = errors.Join(attemptErr, replayErr)
			}
		case attemptErr == nil && attemptRes == nil:
			attemptErr = errors.New("http.Client.Do: attempt returned no response and no error")
		case attemptErr == nil:
			attemptErr = fmt.Errorf("http.Client.Do: unexpected HTTP status %s", attemptRes.Status)
		}

		marker := &retryableError{
			err:     attemptErr,
			resp:    attemptRes,
			attempt: attempt,
			final:   final,
		}

		// The engine owns the attempt count, so this outcome may or may not be
		// retried. Leave the response body open and its attempt context alive:
		// OnRetry releases them before the next attempt, while Do snapshots or
		// hands the body out when the engine stops.
		if attemptRes == nil || attemptRes.Body == nil {
			if attemptCancel != nil {
				attemptCancel(nil)
			}
		}

		attemptErr = marker

		return attemptRes, attemptErr
	}

	retrierOptions := RetrierOptions{
		RetryOn: isMarkedRetryable,
		OnRetry: func(event RetrierEvent) {
			attemptErr := event.Err

			// Release the failed attempt before the engine waits: the response
			// body is no longer needed, and closing it lets the connection be
			// reused while the per-attempt context can be canceled.
			var marker *retryableError
			if errors.As(attemptErr, &marker) {
				if marker.resp != nil && marker.resp.Body != nil {
					drainBody(marker.resp.Body, opts.ResponseReadLimit)

					if marker.resp == lastAttemptResponse {
						lastResponseClosed = true
					}
				}

				attemptErr = marker.err
			}

			if lastAttemptCancel != nil {
				lastAttemptCancel(nil)
			}

			if opts.OnRetry == nil {
				return
			}

			opts.OnRetry(RetryEvent{
				Attempt:  event.Attempt,
				Request:  lastAttemptRequest,
				Response: lastAttemptResponse,
				Err:      attemptErr,
				Wait:     event.Wait,
				Duration: lastAttemptDuration,
			})
		},
		RetryAfter: func() (wait time.Duration, ok bool) {
			return retryAfter, retryAfterSet
		},
	}

	if opts.Retrier != nil {
		res, err = opts.Retrier.Retry(reqCtx, operation, retrierOptions)
	} else {
		res, err = operation()
	}

	if res == nil && err == nil {
		err = errors.New("http.Client.Do: retrier returned no response and no error")
	}

	// The execution ends when the response headers arrive. Stop the timer; if
	// it already expired, record the deadline so a response is never handed
	// out with a canceled context.
	if execTimer != nil && !execTimer.Stop() {
		cancel(context.DeadlineExceeded)
	}

	c.closeIdleConnectionsIfDue()

	var marker *retryableError

	switch {
	case err == nil && reqCtx.Err() == nil:
		// The final response with its body open; the caller owns it.
	case reqCtx.Err() != nil:
		// The context ended the execution; the last response, if any, is in an
		// unknown body state, so it is not handed out.
		if res != nil && res.Body != nil {
			_ = res.Body.Close()
		}

		if res != nil && res == lastAttemptResponse {
			lastResponseClosed = true
		}

		res = nil

		if cause := context.Cause(reqCtx); cause != nil {
			err = cause
		}
	case errors.As(err, &marker):
		if opts.RetryExhaustionHandler != nil {
			handled, handledErr := opts.RetryExhaustionHandler(marker.resp, marker.err, marker.attempt)
			if handled != nil {
				res = handled
				err = handledErr

				break
			}

			if marker.resp != nil && marker.resp.Body != nil {
				if handledErr == nil {
					marker.body, marker.bodyTruncated, marker.bodyErr = snapshotBody(marker.resp.Body, opts.RetryErrorBodyLimit)
				} else {
					_ = marker.resp.Body.Close()
				}
			}

			if lastAttemptCancel != nil {
				lastAttemptCancel(nil)
			}

			res = nil

			if handledErr != nil {
				err = handledErr

				break
			}
		} else {
			// The engine stopped with the final response's body still open;
			// snapshot it for the RetryError and close it.
			if marker.resp != nil && marker.resp.Body != nil {
				marker.body, marker.bodyTruncated, marker.bodyErr = snapshotBody(marker.resp.Body, opts.RetryErrorBodyLimit)
			}

			if lastAttemptCancel != nil {
				lastAttemptCancel(nil)
			}
		}

		if marker.resp == lastAttemptResponse {
			lastResponseClosed = true
		}

		// Attempts exhausted: the last response's body has been consumed.
		res = nil

		requestURL := ""
		if req.URL != nil {
			requestURL = req.URL.Redacted()
		}

		retryErr := &RetryError{
			Attempts: marker.attempt,
			Method:   req.Method,
			URL:      requestURL,
			Err:      marker.err,
			History:  history,
			final:    marker.final,
		}

		if marker.resp != nil {
			retryErr.StatusCode = marker.resp.StatusCode
			retryErr.Status = marker.resp.Status
			retryErr.Header = marker.resp.Header.Clone()
			retryErr.Body = marker.body
			retryErr.BodyTruncated = marker.bodyTruncated
			retryErr.BodyErr = marker.bodyErr
		}

		err = retryErr
	}

	if res == nil {
		if !lastResponseClosed && lastAttemptResponse != nil && lastAttemptResponse.Body != nil {
			_ = lastAttemptResponse.Body.Close()
		}

		return nil, err
	}

	// The caller owns the body from here. Cancel the request context when the
	// body is closed or drained, and start the independent body-read timer.
	if res.Body == nil || res.Body == http.NoBody {
		if lastAttemptCancel != nil {
			lastAttemptCancel(nil)
		}

		stopCloseWatch()

		cancel(nil)

		return res, err
	}

	var bodyTimer *time.Timer

	if c.config.ResponseBodyTimeout > 0 {
		bodyTimer = time.AfterFunc(c.config.ResponseBodyTimeout, func() {
			cancel(context.DeadlineExceeded)
		})
	}

	res.Body = &responseBody{
		ReadCloser:     res.Body,
		cancel:         cancel,
		attemptCancel:  lastAttemptCancel,
		stopCloseWatch: stopCloseWatch,
		cause: func() error {
			return context.Cause(reqCtx)
		},
		timer: bodyTimer,
	}

	return res, err
}

// executionContext returns the context for one execution. A request context
// other than [context.Background] wins; otherwise the client's
// [WithContext] default applies, followed by [context.Background].
//
// Parameters:
//   - req (*http.Request): the request being executed.
//
// Returns:
//   - ctx (context.Context): the execution context.
func (c *Client) executionContext(req *http.Request) (ctx context.Context) {
	ctx = req.Context()
	if ctx != context.Background() {
		return ctx
	}

	if c.config.DefaultContext != nil {
		return c.config.DefaultContext
	}

	return ctx
}

// Compile-time guard ensuring [responseBody] satisfies [io.ReadCloser].
var _ io.ReadCloser = (*responseBody)(nil)

// Close shuts down the client. It cancels in-flight executions with
// [ErrClientClosed], closes idle connections, and rejects later executions.
// It is safe to call more than once and concurrently with requests.
func (c *Client) Close() {
	if c == nil {
		return
	}

	if !c.closed.CompareAndSwap(false, true) {
		return
	}

	if c.closeCancel != nil {
		c.closeCancel(ErrClientClosed)
	}

	if c.config.HTTPClient != nil {
		c.CloseIdleConnections()
	}
}

// CloseIdleConnections closes the idle connections of both underlying
// clients, freeing file descriptors held by their connection pools.
// In-flight requests are unaffected and the client remains usable. Use
// [Client.Close] for a full shutdown.
func (c *Client) CloseIdleConnections() {
	if c.config.HTTPClient != nil && c.config.HTTPClient.Transport != nil {
		c.config.HTTPClient.CloseIdleConnections()
	}

	if c.fallbackHTTPClient == nil || c.fallbackHTTPClient.Transport == nil {
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

// Compile-time guard ensuring [Client] satisfies [http.RoundTripper].
var _ http.RoundTripper = (*Client)(nil)

// RoundTrip implements [http.RoundTripper] using the client's retry engine. It
// executes req with its context and applies the client's default headers. A
// response produced by retry exhaustion is returned with a nil error even for a
// 5xx, as the RoundTripper contract requires; transport failures return an
// error. A redirect-policy abort or a custom policy that returns a response
// with an error is passed through unchanged.
//
// Redirects, cookies, and the execution timeout remain those of the client.
// The request URL is used as given.
//
// Parameters:
//   - req (*http.Request): the request to execute.
//
// Returns:
//   - res (*http.Response): the response, with a body the caller owns.
//   - err (error): non-nil when the execution fails.
func (c *Client) RoundTrip(req *http.Request) (res *http.Response, err error) {
	if req == nil {
		err = fmt.Errorf("http.Client.RoundTrip: %w", ErrNilRequest)

		return nil, err
	}

	if req.URL == nil {
		err = fmt.Errorf("http.Client.RoundTrip: %w", ErrNilRequestURL)

		return nil, err
	}

	opts := c.requestOptions(WithRequestOnRetryExhaustion(func(resp *http.Response, rerr error, _ int) (*http.Response, error) {
		if resp != nil {
			return resp, nil
		}

		return nil, rerr
	}))

	res, err = c.do(&hqgohttprequest.Request{Request: req}, opts)

	return res, err
}

// Std returns an [http.Client] whose Transport is the receiver. It lets code
// that expects a standard client, such as an OAuth2 transport wrapper, use the
// client's execution path, including its retrier.
//
// Redirects, cookies, and timeouts remain those of the receiver. The returned
// client stops at the first response it receives ([http.ErrUseLastResponse]),
// so an outer redirect policy cannot bypass a hop the receiver refused.
// Default headers, including the User-Agent, are applied by [Client.Do].
//
// Returns:
//   - client (*http.Client): a client executing through the receiver.
func (c *Client) Std() (client *http.Client) {
	client = &http.Client{
		Transport: c,
		CheckRedirect: func(*http.Request, []*http.Request) error {
			return http.ErrUseLastResponse
		},
	}

	return client
}

// sameRoundTripper reports whether a and b are the same transport value. It
// guards the double close when a fallback client reuses a custom transport
// that cannot be cloned.
//
// Parameters:
//   - a (http.RoundTripper): the first transport.
//   - b (http.RoundTripper): the second transport.
//
// Returns:
//   - same (bool): true when both interfaces hold the same comparable value.
func sameRoundTripper(a, b http.RoundTripper) (same bool) {
	if a == nil || b == nil {
		return a == nil && b == nil
	}

	aValue := reflect.ValueOf(a)
	bValue := reflect.ValueOf(b)

	if aValue.Type() != bValue.Type() || !aValue.Type().Comparable() {
		return false
	}

	same = aValue.Interface() == bValue.Interface()

	return same
}

// closeIdleConnectionsIfDue counts a completed execution and, once the count
// reaches a multiple of the configured threshold, closes idle connections. A
// non-positive threshold disables the periodic closing.
func (c *Client) closeIdleConnectionsIfDue() {
	if c.config.MaxRequestsBeforeIdleClose <= 0 {
		return
	}

	count := c.requestCount.Add(1)
	if count%uint64(c.config.MaxRequestsBeforeIdleClose) != 0 {
		return
	}

	c.CloseIdleConnections()
}

// WithOptions registers per-request overrides of the execution knobs, applied
// on top of the client configuration for this request only.
//
// Parameters:
//   - ofs (...RequestOptionFunc): the overrides.
//
// Returns:
//   - builder (*RequestBuilder): the receiver, for chaining.
func (b *RequestBuilder) WithOptions(ofs ...RequestOptionFunc) (builder *RequestBuilder) {
	b.requestOptions = append(b.requestOptions, ofs...)

	return b
}

// NewMultipartForm creates an empty multipart form.
//
// Returns:
//   - form (*MultipartForm): a new form.
func NewMultipartForm() (form *MultipartForm) {
	form = &MultipartForm{}

	return form
}

// WithTransport returns a [ClientOptionFunc] that replaces the transport the
// library builds for the selected protocol.
//
// It takes precedence over [WithProtocol], [WithTLSConfig], [WithProxy],
// [WithTransportBuilder], [WithDialContext], [WithDialer],
// [WithTLSHandshakeTimeout], and [WithTransportWrapper]: the supplied
// transport owns HTTP version selection, proxying, TLS, and connection
// pooling. It is ignored when [WithHTTPClient] is also supplied, because that
// client already carries a transport.
//
// Parameters:
//   - transport (http.RoundTripper): the transport to use. Nil is ignored.
//
// Returns:
//   - f (ClientOptionFunc): a functional option that sets the transport field.
func WithTransport(transport http.RoundTripper) (f ClientOptionFunc) {
	return func(opts *ClientOptions) {
		if transport != nil {
			opts.Transport = transport
		}
	}
}

// WithTransportWrapper returns a [ClientOptionFunc] that wraps the transport
// the library builds. It preserves protocol selection, TLS configuration,
// [WithTransportBuilder], and proxying while adding middleware such as
// authentication, tracing, or request signing. The wrapper runs after the
// builder and the proxy are applied.
//
// It is ignored when [WithHTTPClient] or [WithTransport] is supplied, because
// those options already provide a transport. A wrapper that returns nil leaves
// the original transport unchanged. A wrapper hides optional transport methods
// such as CloseIdleConnections; forward them when the base transport
// implements them and [Client.CloseIdleConnections] should reach them.
//
// Parameters:
//   - wrapper (func(http.RoundTripper) http.RoundTripper): the transport
//     decorator. Nil is ignored.
//
// Returns:
//   - f (ClientOptionFunc): a functional option that sets the
//     transportWrapper field.
func WithTransportWrapper(wrapper func(http.RoundTripper) http.RoundTripper) (f ClientOptionFunc) {
	return func(opts *ClientOptions) {
		if wrapper != nil {
			opts.TransportWrapper = wrapper
		}
	}
}

// TransportBuilder customizes a library-built HTTP/1.x or HTTP/2 transport. It
// receives the selected protocol and the transport the library built, and
// returns the transport to use. Returning nil keeps the input unchanged.
//
// The builder runs before [WithProxy] and [WithTransportWrapper] are applied,
// so the returned transport is proxied and wrapped too. A returned transport
// that is not an [*http.Transport] is not proxied.
type TransportBuilder func(protocol Protocol, transport *http.Transport) *http.Transport

// WithTransportBuilder returns a [ClientOptionFunc] that customizes the
// library-built HTTP/1.x and HTTP/2 transport. The builder receives the
// selected protocol and the configured transport.
//
// It is ignored when [WithHTTPClient] or [WithTransport] supplies the
// transport. HTTP/3 is not an [*http.Transport] and is not passed to the
// builder.
//
// Parameters:
//   - builder (TransportBuilder): the builder. Nil is ignored; a builder that
//     returns nil keeps the transport unchanged.
//
// Returns:
//   - f (ClientOptionFunc): a functional option that sets the
//     transportBuilder field.
func WithTransportBuilder(builder TransportBuilder) (f ClientOptionFunc) {
	return func(opts *ClientOptions) {
		if builder != nil {
			opts.TransportBuilder = builder
		}
	}
}

// HTTP3TransportBuilder customizes a library-built HTTP/3 transport. It
// receives the transport the library built and returns the transport to use.
// Returning nil keeps the input unchanged.
//
// It is the supported escape hatch for settings the client options do not
// expose, including http3.Transport.Dial, QUICConfig, AdditionalSettings, and
// MaxResponseHeaderBytes. WithDialContext and WithDialer remain TCP-only and do
// not apply to HTTP/3.
type HTTP3TransportBuilder func(transport *http3.Transport) *http3.Transport

// WithHTTP3TransportBuilder returns a [ClientOptionFunc] that customizes the
// library-built HTTP/3 transport. It applies to [ProtocolHTTP3] and the HTTP/3
// primary of [ProtocolAutoHTTP3]. It is ignored when [WithHTTPClient] or
// [WithTransport] supplies the transport.
//
// Parameters:
//   - builder (HTTP3TransportBuilder): the builder. Nil is ignored; a builder
//     that returns nil keeps the transport unchanged.
//
// Returns:
//   - f (ClientOptionFunc): a functional option that sets the
//     http3TransportBuilder field.
func WithHTTP3TransportBuilder(builder HTTP3TransportBuilder) (f ClientOptionFunc) {
	return func(opts *ClientOptions) {
		if builder != nil {
			opts.HTTP3TransportBuilder = builder
		}
	}
}

// WithDialContext returns a [ClientOptionFunc] that sets the dial function used
// by library-built HTTP/1.x and HTTP/2 transports. It supports custom
// resolvers, connect-to dialing, interface binding, and Unix sockets.
//
// It is ignored when [WithHTTPClient] or [WithTransport] is supplied, and the
// HTTP/3 transport does not use it.
//
// Parameters:
//   - dialContext (func(context.Context, string, string) (net.Conn, error)):
//     the dial function, matching [net.Dialer.DialContext]. Nil is ignored.
//
// Returns:
//   - f (ClientOptionFunc): a functional option that sets the dialContext field.
func WithDialContext(dialContext func(ctx context.Context, network, address string) (net.Conn, error)) (f ClientOptionFunc) {
	return func(opts *ClientOptions) {
		if dialContext != nil {
			opts.DialContext = dialContext
		}
	}
}

// WithDialer returns a [ClientOptionFunc] that sets the dial function from d,
// including its timeout, keep-alive, and local address settings.
//
// Parameters:
//   - dialer (*net.Dialer): the dialer. Nil is ignored.
//
// Returns:
//   - f (ClientOptionFunc): a functional option that sets the dialContext field.
func WithDialer(dialer *net.Dialer) (f ClientOptionFunc) {
	return func(opts *ClientOptions) {
		if dialer != nil {
			opts.DialContext = dialer.DialContext
		}
	}
}

// WithTLSHandshakeTimeout returns a [ClientOptionFunc] that sets the TLS
// handshake timeout of library-built HTTP/1.x and HTTP/2 transports. A
// non-positive value disables the timeout.
//
// The default is the transport's 10 seconds; the option applies only to built
// transports and is ignored with [WithHTTPClient] or [WithTransport].
//
// Parameters:
//   - timeout (time.Duration): the handshake bound. Non-positive disables it.
//
// Returns:
//   - f (ClientOptionFunc): a functional option that sets the
//     tlsHandshakeTimeout field.
func WithTLSHandshakeTimeout(timeout time.Duration) (f ClientOptionFunc) {
	return func(opts *ClientOptions) {
		if timeout < 0 {
			timeout = 0
		}

		opts.TLSHandshakeTimeout = timeout
		opts.tlsHandshakeTimeoutSet = true
	}
}

// WithProxy returns a [ClientOptionFunc] that sets the proxy function used by
// a library-built HTTP/1.x or HTTP/2 transport. It is ignored when
// [WithHTTPClient] or [WithTransport] is supplied.
//
// The selector is also consulted for [ProtocolAutoHTTP3] and [ProtocolHTTP3]
// requests: a non-nil result makes [ProtocolAutoHTTP3] use the auto client and
// [ProtocolHTTP3] return [ErrHTTP3ProxyUnsupported], because HTTP/3 does not
// support proxies. The selector must be side-effect-free.
//
// A non-nil selector that returns a nil URL disables environment proxying for
// that request; return nil unconditionally to disable it for every request.
// A nil URL also permits HTTP/3.
//
// Parameters:
//   - proxy (func(*http.Request) (*url.URL, error)): the proxy selector, in
//     the form [http.Transport.Proxy] expects. Nil is ignored.
//
// Returns:
//   - f (ClientOptionFunc): a functional option that sets the proxy field.
func WithProxy(proxy func(*http.Request) (*url.URL, error)) (f ClientOptionFunc) {
	return func(opts *ClientOptions) {
		if proxy != nil {
			opts.Proxy = proxy
		}
	}
}

// WithCheckRedirect returns a [ClientOptionFunc] that sets the redirect policy
// applied when a response is a redirect. The function follows the
// [http.Client.CheckRedirect] contract: returning [http.ErrUseLastResponse]
// stops following and returns the redirect response.
//
// The policy applies to library-built clients and to a cloned
// [WithHTTPClient] client. A nil function clears a custom client's policy,
// restoring net/http's default; it has no effect on a library-built client,
// which already uses the default.
//
// Parameters:
//   - checkRedirect (func(*http.Request, []*http.Request) error): the redirect
//     policy.
//
// Returns:
//   - f (ClientOptionFunc): a functional option that sets the checkRedirect field.
func WithCheckRedirect(checkRedirect func(*http.Request, []*http.Request) error) (f ClientOptionFunc) {
	return func(opts *ClientOptions) {
		opts.CheckRedirect = checkRedirect
		opts.checkRedirectSet = true
	}
}

// WithCookieJar returns a [ClientOptionFunc] that sets the cookie jar used by
// library-built clients and by a cloned [WithHTTPClient] client.
//
// A nil jar disables cookie storage for a custom client and has no effect on a
// library-built client, which has no jar by default.
//
// Parameters:
//   - cookieJar (http.CookieJar): the cookie jar.
//
// Returns:
//   - f (ClientOptionFunc): a functional option that sets the cookieJar field.
func WithCookieJar(cookieJar http.CookieJar) (f ClientOptionFunc) {
	return func(opts *ClientOptions) {
		opts.CookieJar = cookieJar
		opts.cookieJarSet = true
	}
}

// WithTLSConfig returns a [ClientOptionFunc] that sets the TLS configuration
// used by transports the library builds for [ProtocolAuto], [ProtocolAutoHTTP3],
// [ProtocolHTTP1], [ProtocolHTTP2], and [ProtocolHTTP3].
//
// The configuration is cloned before use, so later mutations of the caller's
// value never affect a built client. It is ignored when [WithHTTPClient] or
// [WithTransport] supplies a transport; configure TLS on that client or
// transport instead. [WithTransportWrapper] preserves this configuration
// because it wraps the built transport.
//
// Parameters:
//   - config (*tls.Config): the TLS configuration. Nil uses the system
//     defaults.
//
// Returns:
//   - f (ClientOptionFunc): a functional option that sets the tlsConfig field.
func WithTLSConfig(config *tls.Config) (f ClientOptionFunc) {
	return func(opts *ClientOptions) {
		opts.TLSConfig = config
	}
}

const (
	// DefaultTimeout is zero: no bound is placed on an execution through
	// receipt of the response headers. The caller's context still bounds every
	// attempt and wait.
	//
	// Use [WithTimeout] with a positive value to add a bound;
	// [RecommendedTimeout] is the value earlier releases applied by default. A
	// non-positive [WithTimeout] value keeps the execution unbounded.
	DefaultTimeout = 0 * time.Second

	// RecommendedTimeout is the 30-second execution bound earlier releases
	// applied by default. It is a suggested value for [WithTimeout], not a
	// default: pass it explicitly to restore that behavior.
	RecommendedTimeout = 30 * time.Second

	// DefaultResponseBodyTimeout is the default separate bound on reading a
	// handed-out response body. Zero means no separate bound: the caller's
	// context still governs reads, but the execution timeout does not.
	DefaultResponseBodyTimeout = 0 * time.Second

	// DefaultResponseReadLimit is the default maximum number of bytes drained
	// from the body of a response that will be retried, so the underlying
	// connection can be reused.
	//
	// It applies when [WithResponseReadLimit] is not supplied, and when a
	// supplied value less than or equal to 0 is normalized back to the default.
	DefaultResponseReadLimit = 4096

	// DefaultUserAgent is the default User-Agent header registered by
	// [New].
	//
	// It is an ordinary default header: replace it with
	// WithHeader("User-Agent", value), and a request-level User-Agent wins
	// over it.
	DefaultUserAgent = "hq-lib-http-go (https://github.com/hueristiq/hq-lib-http-go)"
)

// Protocol selects the HTTP version used by a [Client].
//
// The zero value, [ProtocolAuto], negotiates HTTP/2 over TLS and falls back to
// HTTP/1.1. The explicit modes force one protocol; [ProtocolAutoHTTP3] tries
// HTTP/3 first and falls back to [ProtocolAuto].
type Protocol int

const (
	// ProtocolAuto negotiates HTTP/2 over TLS and falls back to HTTP/1.1.
	// Cleartext HTTP/2 servers that answer an HTTP/1.x request with HTTP/2
	// frames are retried once on an h2c fallback.
	ProtocolAuto Protocol = iota
	// ProtocolAutoHTTP3 tries HTTP/3 first and falls back to [ProtocolAuto] on
	// a transient transport error. A response paired with an error is final,
	// and permanent failures are not retried on another protocol.
	ProtocolAutoHTTP3
	// ProtocolHTTP1 forces HTTP/1.0 or HTTP/1.1.
	ProtocolHTTP1
	// ProtocolHTTP2 forces HTTP/2, including h2c prior knowledge for http://
	// URLs.
	ProtocolHTTP2
	// ProtocolHTTP3 forces HTTP/3. It requires https:// URLs, uses UDP, and
	// does not honor HTTP proxies.
	ProtocolHTTP3
)

// String returns the protocol name.
//
// Returns:
//   - protocol (string): the protocol name.
func (p Protocol) String() (protocol string) {
	switch p {
	case ProtocolAuto:
		protocol = "auto"
	case ProtocolAutoHTTP3:
		protocol = "auto-http3"
	case ProtocolHTTP1:
		protocol = "http/1.1"
	case ProtocolHTTP2:
		protocol = "http/2"
	case ProtocolHTTP3:
		protocol = "http/3"
	default:
		protocol = "auto"
	}

	return protocol
}

// DefaultHTTP3FallbackTTL is how long a host that failed an HTTP/3 attempt is
// sent through the HTTP/1.1+HTTP/2 client in [ProtocolAutoHTTP3] before HTTP/3
// is tried again.
const DefaultHTTP3FallbackTTL = time.Minute

// buildHTTPClients selects the primary and fallback clients for the
// configured protocol. A custom client takes precedence over the protocol
// mode; the existing h2c fallback is installed for it.
func (c *Client) buildHTTPClients() {
	if c.config.HTTPClient != nil {
		if c.config.checkRedirectSet || c.config.cookieJarSet {
			c.config.HTTPClient = c.applyClientOptions(cloneHTTPClient(c.config.HTTPClient))
		}

		c.fallbackHTTPClient = newFallbackClient(c.config.HTTPClient, nil)

		return
	}

	switch c.config.Protocol {
	case ProtocolAuto:
		c.config.HTTPClient, c.fallbackHTTPClient = c.buildAutoClients()
	case ProtocolHTTP1:
		c.config.HTTPClient = newHTTP1Client(c.config.TLSConfig)
		c.config.HTTPClient.Transport = c.configureTransport(c.config.Protocol, c.config.HTTPClient.Transport)
		c.applyClientOptions(c.config.HTTPClient)
	case ProtocolHTTP2:
		c.config.HTTPClient = newHTTP2Client(c.config.TLSConfig)
		c.config.HTTPClient.Transport = c.configureTransport(c.config.Protocol, c.config.HTTPClient.Transport)
		c.applyClientOptions(c.config.HTTPClient)
	case ProtocolHTTP3:
		c.config.HTTPClient = newHTTP3Client(c.config.TLSConfig)

		if c.config.Transport == nil {
			c.config.HTTPClient.Transport = c.configureHTTP3Transport(c.config.HTTPClient.Transport)
		}

		c.config.HTTPClient.Transport = c.configureTransport(c.config.Protocol, c.config.HTTPClient.Transport)
		c.applyClientOptions(c.config.HTTPClient)
		c.http3Primary = c.config.Transport == nil
	case ProtocolAutoHTTP3:
		if c.config.Transport != nil {
			c.config.HTTPClient, c.fallbackHTTPClient = c.buildAutoClients()

			return
		}

		c.config.HTTPClient = newHTTP3Client(c.config.TLSConfig)
		c.config.HTTPClient.Transport = c.configureHTTP3Transport(c.config.HTTPClient.Transport)
		c.config.HTTPClient.Transport = c.applyTransportWrapper(c.config.HTTPClient.Transport)
		c.applyClientOptions(c.config.HTTPClient)
		c.fallbackHTTPClient = c.buildAutoClient()
		c.fallbackOnHTTP3 = true
		c.http3Primary = true
	default:
		c.config.HTTPClient, c.fallbackHTTPClient = c.buildAutoClients()
	}
}

// shouldFallback reports whether an attempt error should be retried on the
// fallback client.
//
// Parameters:
//   - fallbackOnHTTP3 (bool): whether the primary client speaks HTTP/3.
//   - err (error): the primary attempt's error.
//
// Returns:
//   - fallback (bool): whether the fallback client should be tried.
func shouldFallback(fallbackOnHTTP3 bool, err error) (fallback bool) {
	if fallbackOnHTTP3 {
		fallback = isHTTP3FallbackError(err)

		return fallback
	}

	fallback = isHTTP1BrokenTransportError(err)

	return fallback
}

// isHTTP3FallbackError reports whether a transient HTTP/3 transport error
// should be retried on HTTP/2 or HTTP/1.1. Context cancellation and deadline
// errors end the whole execution, and permanent failures — an exhausted
// redirect chain, an unsupported scheme, an untrusted certificate, or a name
// that does not resolve — cannot be fixed by another protocol.
//
// Parameters:
//   - err (error): the HTTP/3 attempt's error.
//
// Returns:
//   - fallback (bool): whether the fallback client should be tried.
func isHTTP3FallbackError(err error) (fallback bool) {
	if err == nil {
		return false
	}

	if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
		return false
	}

	fallback = !isPermanentTransportError(err)

	return fallback
}

// http3FailureSweepInterval is the number of failure-cache writes between
// expired-entry sweeps.
const http3FailureSweepInterval = 64

// http3RecentlyFailed reports whether host failed an HTTP/3 attempt within the
// configured fallback TTL. An expired entry is removed. A non-positive TTL or
// an empty host disables the cache.
//
// Parameters:
//   - host (string): the request URL's host, including a port when present.
//
// Returns:
//   - failed (bool): true when the host should skip HTTP/3 for now.
func (c *Client) http3RecentlyFailed(host string) (failed bool) {
	if c.config.HTTP3FallbackTTL <= 0 || host == "" {
		return false
	}

	value, ok := c.http3Failures.Load(host)
	if !ok {
		return false
	}

	failedAt, ok := value.(time.Time)
	if !ok || time.Since(failedAt) >= c.config.HTTP3FallbackTTL {
		c.http3Failures.Delete(host)

		return false
	}

	failed = true

	return failed
}

// recordHTTP3Failure remembers that host failed an HTTP/3 attempt. The entry
// expires after the configured fallback TTL. Expired entries are swept every
// [http3FailureSweepInterval] writes so unique failing hosts cannot grow the
// cache without bound.
//
// Parameters:
//   - host (string): the request URL's host, including a port when present.
func (c *Client) recordHTTP3Failure(host string) {
	if c.config.HTTP3FallbackTTL <= 0 || host == "" {
		return
	}

	c.http3Failures.Store(host, time.Now())

	if c.http3FailureWrites.Add(1)%http3FailureSweepInterval == 0 {
		c.pruneHTTP3Failures()
	}
}

// pruneHTTP3Failures removes expired entries from the HTTP/3 failure cache.
func (c *Client) pruneHTTP3Failures() {
	ttl := c.config.HTTP3FallbackTTL
	if ttl <= 0 {
		return
	}

	c.http3Failures.Range(func(key, value any) bool {
		failedAt, ok := value.(time.Time)
		if !ok || time.Since(failedAt) >= ttl {
			c.http3Failures.Delete(key)
		}

		return true
	})
}

// clearHTTP3Failure removes a host from the HTTP/3 failure cache after a
// successful HTTP/3 attempt.
//
// Parameters:
//   - host (string): the request URL's host, including a port when present.
func (c *Client) clearHTTP3Failure(host string) {
	if host == "" {
		return
	}

	c.http3Failures.Delete(host)
}
