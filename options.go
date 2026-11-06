package http

import (
	"context"
	"crypto/tls"
	"fmt"
	"net"
	"net/http"
	"net/url"
	"strings"
	"time"

	hqgohttpheader "github.com/hueristiq/hq-lib-http-go/header"
	hqgohttprequest "github.com/hueristiq/hq-lib-http-go/request"
)

// ClientOptions holds the configurable knobs of a [Client], populated through
// [ClientOptionFunc] values passed to [New]. The resolved configuration stays
// private to the client and immutable after construction.
//
// Fields:
//   - HTTPClient (*http.Client): the underlying client executing requests. Nil
//     means a client is built from Protocol, TLSConfig, Transport, and the
//     transport controls.
//   - HTTP3Client (*http.Client): the application-supplied HTTP/3 client used by
//     [ProtocolHTTP3] and as the HTTP/3 primary of [ProtocolAutoHTTP3]. Nil
//     leaves HTTP/3 unimplemented: forced HTTP/3 fails with
//     [ErrHTTP3ClientRequired], and [ProtocolAutoHTTP3] behaves as
//     [ProtocolAuto].
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
//   - RetryMaxAttempts (int): the maximum attempts per execution, including
//     the first. Non-positive selects 3.
//   - Logger (Logger): the structured logger used by the execution. Nil
//     disables logging.
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
//     *http.Transport. Ignored when Transport is set; HTTP/3 clients are
//     application-supplied and never reach it.
//   - DialContext (func(context.Context, string, string) (net.Conn, error)):
//     the dial function applied to library-built *http.Transport values.
//   - TLSHandshakeTimeout (time.Duration): the TLS handshake timeout applied to
//     library-built *http.Transport values when the corresponding unexported
//     flag is set.
type ClientOptions struct {
	HTTPClient                 *http.Client
	HTTP3Client                *http.Client
	Protocol                   Protocol
	HTTP3FallbackTTL           time.Duration
	TLSConfig                  *tls.Config
	Transport                  http.RoundTripper
	TransportWrapper           func(http.RoundTripper) http.RoundTripper
	TransportBuilder           TransportBuilder
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
	RetryMaxAttempts           int
	Logger                     Logger
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

// validate rejects invalid option values; [New] returns the first error.
// Zero values are meaningful for several fields (for example, a zero timeout
// disables the bound), so only out-of-range and negative values are errors.
//
// Returns:
//   - err (error): non-nil when a value is invalid.
func (opts *ClientOptions) validate() (err error) {
	if opts.Protocol < ProtocolAuto || opts.Protocol > ProtocolHTTP3 {
		return fmt.Errorf("client.New: protocol %d is out of range", opts.Protocol)
	}

	if opts.Timeout < 0 {
		return fmt.Errorf("client.New: timeout must not be negative, got %v", opts.Timeout)
	}

	if opts.ResponseBodyTimeout < 0 {
		return fmt.Errorf("client.New: response body timeout must not be negative, got %v", opts.ResponseBodyTimeout)
	}

	if opts.MaxResponseBodySize < 0 {
		return fmt.Errorf("client.New: max response body size must not be negative, got %d", opts.MaxResponseBodySize)
	}

	if opts.MaxRequestBodySize < 0 {
		return fmt.Errorf("client.New: max request body size must not be negative, got %d", opts.MaxRequestBodySize)
	}

	if opts.AttemptTimeout < 0 {
		return fmt.Errorf("client.New: attempt timeout must not be negative, got %v", opts.AttemptTimeout)
	}

	if opts.RetryPolicy == nil {
		opts.RetryPolicy = DefaultRetryPolicy
	}

	if opts.RetryMaxAttempts <= 0 {
		return fmt.Errorf("client.New: retry max attempts must be positive, got %d", opts.RetryMaxAttempts)
	}

	if opts.ResponseReadLimit <= 0 {
		return fmt.Errorf("client.New: response read limit must be positive, got %d", opts.ResponseReadLimit)
	}

	if opts.RetryErrorBodyLimit < 0 {
		return fmt.Errorf("client.New: retry error body limit must not be negative, got %d", opts.RetryErrorBodyLimit)
	}

	return nil
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
// (replaceable with [WithHeader]), a 30-second execution timeout
// ([DefaultTimeout]), [DefaultResponseBodyTimeout] adding no separate body-read
// bound, no per-attempt timeout, the hq-lib-retrier-go engine (three attempts
// with exponential backoff and jitter; [WithRetryMaxAttempts] changes the
// count), [DefaultRetryPolicy], idempotent-only retries, no rate limiter, no
// logger, no callbacks, no retry hook, no periodic idle-connection close,
// [DefaultResponseReadLimit] as the drain bound, retry-error body capture
// disabled ([DefaultRetryErrorBodyLimit] is zero), no response or request body
// cap, and [DefaultHTTP3FallbackTTL] for the HTTP/3 failure cache. A
// library-built client uses HTTP/1.x and HTTP/2 with environment proxying and
// no cookie jar; [WithTransport], [WithProxy], [WithCheckRedirect], and
// [WithCookieJar] change that. HTTP/3 is not built by the library: supply a
// client with [WithHTTP3Client]. Invalid values are rejected: New returns an
// error for an out-of-range protocol or a negative duration, size, or limit.
//
// Parameters:
//   - ofs (...ClientOptionFunc): functional options configuring the client.
//
// Returns:
//   - client (*Client): a new client, ready to execute requests.
//   - err (error): non-nil when an option value is invalid.
//
// Unset options fall back to defaults: [ProtocolAuto] over a pooled client,
// [DefaultUserAgent] as the default User-Agent header
// (replaceable with [WithHeader]), a 30-second execution timeout
// ([DefaultTimeout]), [DefaultResponseBodyTimeout] adding no separate body-read
// bound, no per-attempt timeout, the hq-lib-retrier-go engine (three attempts
// with exponential backoff and jitter; [WithRetryMaxAttempts] changes the
// count), [DefaultRetryPolicy], idempotent-only retries, no rate limiter, no
// logger, no callbacks, no retry hook, no periodic idle-connection close,
// [DefaultResponseReadLimit] as the drain bound, retry-error body capture
// disabled ([DefaultRetryErrorBodyLimit] is zero), no response or request body
// cap, and [DefaultHTTP3FallbackTTL] for the HTTP/3 failure cache. A
// library-built client uses HTTP/1.x and HTTP/2 with environment proxying and
// no cookie jar; [WithTransport], [WithProxy], [WithCheckRedirect], and
// [WithCookieJar] change that. HTTP/3 is not built by the library: supply a
// client with [WithHTTP3Client]. Invalid values are rejected: New returns an
// error for an out-of-range protocol or a negative duration, size, or limit.
//
// Parameters:
//   - ofs (...ClientOptionFunc): functional options configuring the client.
//
// Returns:
//   - client (*Client): a new client, ready to execute requests.
//   - err (error): non-nil when an option value is invalid.
func New(ofs ...ClientOptionFunc) (client *Client, err error) {
	opts := &ClientOptions{
		Headers:             []HeaderField{{Key: hqgohttpheader.UserAgent.String(), Value: DefaultUserAgent}},
		Timeout:             DefaultTimeout,
		ResponseBodyTimeout: DefaultResponseBodyTimeout,
		RetryPolicy:         DefaultRetryPolicy,
		IdempotentRetries:   true,
		RetryMaxAttempts:    defaultRetryMaxAttempts,
		ResponseReadLimit:   DefaultResponseReadLimit,
		RetryErrorBodyLimit: DefaultRetryErrorBodyLimit,
		HTTP3FallbackTTL:    DefaultHTTP3FallbackTTL,
	}

	for _, of := range ofs {
		if of != nil {
			of(opts)
		}
	}

	if err := opts.validate(); err != nil {
		return nil, err
	}

	closeCtx, closeCancel := context.WithCancelCause(context.Background())

	client = &Client{
		config:      *opts,
		closeCtx:    closeCtx,
		closeCancel: closeCancel,
	}

	client.buildHTTPClients()

	return client, nil
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

// WithHTTP3Client returns a [ClientOptionFunc] that registers the
// application-supplied HTTP/3 client used by [ProtocolHTTP3] and as the HTTP/3
// primary of [ProtocolAutoHTTP3]. The library does not build an HTTP/3
// transport and imposes no QUIC dependency; the caller constructs the client
// with an HTTP/3 implementation such as quic-go and configures its transport,
// TLS settings, dialer, and QUIC parameters.
//
// [WithCheckRedirect] and [WithCookieJar] apply to a shallow copy of the
// client, so the caller's client is never mutated. [WithTransport],
// [WithTransportWrapper], [WithTransportBuilder], [WithProxy], and
// [WithTLSConfig] do not configure it. With no HTTP/3 client,
// [ProtocolHTTP3] requests fail with [ErrHTTP3ClientRequired] and
// [ProtocolAutoHTTP3] behaves as [ProtocolAuto].
//
// Parameters:
//   - client (*http.Client): the HTTP/3 client. Nil is ignored, leaving HTTP/3
//     unimplemented.
//
// Returns:
//   - f (ClientOptionFunc): a functional option that sets the http3Client field.
func WithHTTP3Client(client *http.Client) (f ClientOptionFunc) {
	return func(opts *ClientOptions) {
		if client != nil {
			opts.HTTP3Client = client
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
//     bound; values less than or equal to 0 disable it. The default is
//     [DefaultTimeout].
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

// WithRetryMaxAttempts returns a [ClientOptionFunc] that sets the maximum
// attempts per execution, including the first. Values less than or equal to
// zero select the default, 3.
//
// The engine itself is fixed: attempts run through hq-lib-retrier-go with
// exponential backoff and jitter, and a persistent 429 or 5xx outcome becomes a
// [*RetryError] unless a policy or [WithOnRetryExhaustion] returns the final
// response. A value of 1 makes a single attempt.
//
// Parameters:
//   - maxAttempts (int): the maximum attempts per execution.
//
// Returns:
//   - f (ClientOptionFunc): a functional option that sets the retry attempt count.
func WithRetryMaxAttempts(maxAttempts int) (f ClientOptionFunc) {
	return func(opts *ClientOptions) {
		opts.RetryMaxAttempts = maxAttempts
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
// [WithHTTPClient] takes precedence and the protocol mode is ignored.
// [WithTransport] applies to library-built HTTP/1.x and HTTP/2 transports; it
// does not configure the HTTP/3 client, which is supplied whole with
// [WithHTTP3Client]. Use [WithTransportWrapper] to decorate a protocol-built
// transport without losing the selection.
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
// [ProtocolAutoHTTP3] with an HTTP/3 client configured through
// [WithHTTP3Client]; forced HTTP/3 still tries HTTP/3 on every request.
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
// out. Attempts run through the hq-lib-retrier-go engine, which defaults to
// three attempts with exponential backoff and jitter; [WithRetryMaxAttempts]
// changes the count, and 1 makes a single attempt. When the policy approves a
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
//     [RetryExhaustionHandler] replaces this outcome. With retries disabled,
//     the single attempt still classifies this way, so a persistent 429 or 5xx
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
