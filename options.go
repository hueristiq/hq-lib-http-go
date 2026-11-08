package http

import (
	"context"
	"crypto/tls"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"time"

	hqgohttpheader "github.com/hueristiq/hq-lib-http-go/header"
)

// ClientOptions holds the configuration populated by [ClientOptionFunc].
// Applications should use the With options rather than mutate a client's copy.
//
// Fields:
//   - HTTPClient (*http.Client): an injected complete HTTP/1.x-HTTP/2 client;
//     nil lets [New] build one from the protocol and transport options.
//   - HTTP3Client (*http.Client): the injected HTTP/3 client required by
//     [ProtocolHTTP3] and used by [ProtocolAutoHTTP3].
//   - Protocol (Protocol): the selected HTTP version mode.
//   - protocolSet (bool): whether [WithProtocol] ran; distinguishes an
//     explicit [ProtocolAuto] from the zero value for conflict validation.
//   - HTTP3FallbackTTL (time.Duration): how long a failed host skips HTTP/3 in
//     auto mode; zero disables the failure cache.
//   - TLSConfig (*tls.Config): TLS configuration for library-built transports;
//     cloned before use.
//   - TransportWrapper (func(http.RoundTripper) http.RoundTripper): decorator
//     applied to each library-built transport.
//   - Proxy (func(*http.Request) (*url.URL, error)): proxy selector for
//     library-built HTTP/1.x-HTTP/2 transports.
//   - CheckRedirect (func(*http.Request, []*http.Request) error): redirect
//     policy applied to the client.
//   - checkRedirectSet (bool): whether [WithCheckRedirect] ran; distinguishes
//     an explicit nil policy from an unset one.
//   - CookieJar (http.CookieJar): the cookie jar applied to the client.
//   - cookieJarSet (bool): whether [WithCookieJar] ran; distinguishes an
//     explicit nil jar from an unset one.
//   - Headers ([]HeaderField): default headers sent when a request does not
//     carry them; request headers take precedence.
//   - Timeout (time.Duration): execution bound through response headers,
//     including retries and waits; zero disables it.
//   - ResponseBodyTimeout (time.Duration): bound on body reads after handout;
//     zero disables it.
//   - AttemptTimeout (time.Duration): per-attempt bound through response
//     headers; zero disables it.
//   - RetryMaxAttempts (int): total attempt count, including the first.
//   - Logger (Logger): the logger; nil disables logging.
//   - RetryPolicy (RetryPolicy): the retry predicate; nil selects
//     [DefaultRetryPolicy].
//   - RateLimiter (RateLimiter): the limiter waited on before each attempt.
//   - OnRequest (OnRequestFunc): callback run before each attempt.
//   - OnResponse (OnResponseFunc): callback run after each attempt and before
//     the retry decision; must not consume the response body.
//   - OnRetry (OnRetryFunc): callback run before each retry wait.
//   - PrepareRetry (PrepareRetryFunc): hook run after the retry wait and
//     before the next attempt; may update the request or abort.
//   - IdempotentRetries (bool): whether retries and protocol fallback resends
//     are restricted to idempotent methods; defaults to true.
//   - MaxRequestsBeforeIdleClose (int): completed-execution count that
//     triggers periodic idle-connection cleanup; zero disables it.
//   - ResponseReadLimit (int64): cap on bytes drained from retried response
//     bodies.
type ClientOptions struct {
	HTTPClient          *http.Client
	HTTP3Client         *http.Client
	Protocol            Protocol
	protocolSet         bool
	HTTP3FallbackTTL    time.Duration
	TLSConfig           *tls.Config
	TransportWrapper    func(http.RoundTripper) http.RoundTripper
	Proxy               func(*http.Request) (*url.URL, error)
	CheckRedirect       func(*http.Request, []*http.Request) error
	checkRedirectSet    bool
	CookieJar           http.CookieJar
	cookieJarSet        bool
	Headers             []HeaderField
	Timeout             time.Duration
	ResponseBodyTimeout time.Duration
	AttemptTimeout      time.Duration
	RetryMaxAttempts    int
	Logger              Logger
	RetryPolicy         RetryPolicy
	RateLimiter         RateLimiter
	OnRequest           OnRequestFunc
	OnResponse          OnResponseFunc
	OnRetry             OnRetryFunc
	PrepareRetry        PrepareRetryFunc
	IdempotentRetries   bool
	// MaxRequestsBeforeIdleClose controls periodic pool cleanup. Zero disables it.
	MaxRequestsBeforeIdleClose int
	ResponseReadLimit          int64
}

// validate checks the option combination for range errors and contradictions
// — negative durations, non-positive counts, and injected clients combined
// with the transport knobs they replace. [New] rejects invalid combinations
// instead of silently ignoring them.
//
// Returns:
//   - err (error): non-nil describing the first conflict found.
func (opts *ClientOptions) validate() (err error) {
	if opts.Protocol < ProtocolAuto || opts.Protocol > ProtocolHTTP3 {
		return fmt.Errorf("http.New: protocol %d is out of range", opts.Protocol)
	}

	if opts.Timeout < 0 || opts.ResponseBodyTimeout < 0 || opts.AttemptTimeout < 0 || opts.HTTP3FallbackTTL < 0 {
		return errors.New("http.New: timeouts and fallback TTL must not be negative")
	}

	if opts.RetryMaxAttempts <= 0 {
		return fmt.Errorf("http.New: retry max attempts must be positive, got %d", opts.RetryMaxAttempts)
	}

	if opts.ResponseReadLimit <= 0 {
		return fmt.Errorf("http.New: response read limit must be positive, got %d", opts.ResponseReadLimit)
	}

	if opts.MaxRequestsBeforeIdleClose < 0 {
		return errors.New("http.New: idle close threshold must not be negative")
	}

	if opts.HTTPClient != nil {
		switch {
		case opts.protocolSet:
			return errors.New("http.New: WithHTTPClient conflicts with WithProtocol; configure the custom client's transport")
		case opts.HTTP3Client != nil:
			return errors.New("http.New: WithHTTPClient conflicts with WithHTTP3Client")
		case opts.TLSConfig != nil:
			return errors.New("http.New: WithHTTPClient conflicts with WithTLSConfig; configure TLS on the custom transport")
		case opts.TransportWrapper != nil:
			return errors.New("http.New: WithHTTPClient conflicts with WithTransportWrapper; wrap the custom transport")
		case opts.Proxy != nil:
			return errors.New("http.New: WithHTTPClient conflicts with WithProxy; configure the custom transport")
		}
	}

	if opts.HTTP3Client != nil && opts.TransportWrapper != nil {
		return errors.New("http.New: WithHTTP3Client conflicts with WithTransportWrapper; wrap the HTTP/3 transport directly")
	}

	if opts.HTTP3Client != nil && (!opts.protocolSet || (opts.Protocol != ProtocolHTTP3 && opts.Protocol != ProtocolAutoHTTP3)) {
		return errors.New("http.New: WithHTTP3Client requires ProtocolHTTP3 or ProtocolAutoHTTP3")
	}

	if opts.protocolSet && opts.Protocol == ProtocolHTTP3 && opts.HTTP3Client == nil {
		return fmt.Errorf("http.New: %w", ErrHTTP3ClientRequired)
	}

	return nil
}

// ClientOptionFunc configures a [Client].
//
// Parameters:
//   - opts (*ClientOptions): the options under construction.
type ClientOptionFunc func(opts *ClientOptions)

// New constructs a client. The default uses pooled HTTP/1.1+HTTP/2, a 30-second
// execution timeout through response headers, three idempotent attempts with
// exponential backoff, environment proxying, and [DefaultUserAgent]. Body reads
// have no separate timeout by default. Callers own returned response bodies.
//
// Parameters:
//   - ofs (...ClientOptionFunc): configuration options; nil options are
//     ignored. Contradictory combinations fail validation.
//
// Returns:
//   - client (*Client): the constructed client. Close it with [Client.Close].
//   - err (error): non-nil when the option combination is invalid.
func New(ofs ...ClientOptionFunc) (client *Client, err error) {
	opts := &ClientOptions{
		Headers:             []HeaderField{{Key: hqgohttpheader.UserAgent.String(), Value: DefaultUserAgent}},
		Timeout:             DefaultTimeout,
		ResponseBodyTimeout: DefaultResponseBodyTimeout,
		RetryPolicy:         DefaultRetryPolicy,
		IdempotentRetries:   true,
		RetryMaxAttempts:    defaultRetryMaxAttempts,
		ResponseReadLimit:   DefaultResponseReadLimit,
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

// WithHTTPClient injects a complete HTTP/1.x or HTTP/2 client. The library
// clones the client before applying redirect or cookie options. Transport,
// protocol, TLS, proxy, and pooling configuration belong to the injected client.
//
// Parameters:
//   - client (*http.Client): the complete client to execute requests
//     with; it is cloned before redirect or cookie options are applied.
//
// Returns:
//   - f (ClientOptionFunc): a functional option that injects the client.
func WithHTTPClient(client *http.Client) (f ClientOptionFunc) {
	return func(opts *ClientOptions) {
		opts.HTTPClient = client
	}
}

// WithHTTP3Client injects the HTTP/3 client used by ProtocolHTTP3 and
// ProtocolAutoHTTP3. The application owns its QUIC and transport configuration.
//
// Parameters:
//   - client (*http.Client): the HTTP/3 client; the application owns its
//     QUIC and transport configuration.
//
// Returns:
//   - f (ClientOptionFunc): a functional option that injects the HTTP/3 client.
func WithHTTP3Client(client *http.Client) (f ClientOptionFunc) {
	return func(opts *ClientOptions) {
		opts.HTTP3Client = client
	}
}

// WithProtocol selects the HTTP protocol. It cannot configure an injected
// HTTPClient; New rejects that contradictory combination.
//
// Parameters:
//   - protocol (Protocol): the protocol mode, one of [ProtocolAuto],
//     [ProtocolAutoHTTP3], [ProtocolHTTP1], [ProtocolHTTP2], or
//     [ProtocolHTTP3].
//
// Returns:
//   - f (ClientOptionFunc): a functional option that sets the protocol.
func WithProtocol(protocol Protocol) (f ClientOptionFunc) {
	return func(opts *ClientOptions) {
		opts.Protocol = protocol
		opts.protocolSet = true
	}
}

// WithHTTP3FallbackTTL sets how long a failed host skips HTTP/3 in auto mode.
// Zero disables the failure cache.
//
// Parameters:
//   - ttl (time.Duration): how long a failed host skips HTTP/3; zero
//     disables the failure cache.
//
// Returns:
//   - f (ClientOptionFunc): a functional option that sets the fallback TTL.
func WithHTTP3FallbackTTL(ttl time.Duration) (f ClientOptionFunc) {
	return func(opts *ClientOptions) {
		opts.HTTP3FallbackTTL = ttl
	}
}

// WithTLSConfig configures library-built transports. The configuration is
// cloned before use.
//
// Parameters:
//   - config (*tls.Config): the TLS configuration; cloned before use.
//
// Returns:
//   - f (ClientOptionFunc): a functional option that sets the TLS configuration.
func WithTLSConfig(config *tls.Config) (f ClientOptionFunc) {
	return func(opts *ClientOptions) {
		opts.TLSConfig = config
	}
}

// WithTransportWrapper decorates each library-built transport. A nil result
// preserves the input transport. Custom HTTPClient and HTTP3Client transports
// must be wrapped by the application instead.
//
// Parameters:
//   - wrapper (func(http.RoundTripper) http.RoundTripper): the transport
//     decorator; a nil result preserves the input transport.
//
// Returns:
//   - f (ClientOptionFunc): a functional option that sets the transport wrapper.
func WithTransportWrapper(wrapper func(http.RoundTripper) http.RoundTripper) (f ClientOptionFunc) {
	return func(opts *ClientOptions) {
		opts.TransportWrapper = wrapper
	}
}

// WithProxy sets the proxy selector for library-built HTTP/1.x and HTTP/2
// transports. HTTP/3 rejects proxied requests or falls back in auto mode.
//
// Parameters:
//   - proxy (func(*http.Request) (*url.URL, error)): the proxy selector.
//
// Returns:
//   - f (ClientOptionFunc): a functional option that sets the proxy selector.
func WithProxy(proxy func(*http.Request) (*url.URL, error)) (f ClientOptionFunc) {
	return func(opts *ClientOptions) {
		opts.Proxy = proxy
	}
}

// WithCheckRedirect sets the redirect policy. A nil policy restores net/http's
// default on an injected client.
//
// Parameters:
//   - checkRedirect (func(*http.Request, []*http.Request) error): the
//     redirect policy; nil restores the net/http default on an injected
//     client.
//
// Returns:
//   - f (ClientOptionFunc): a functional option that sets the redirect policy.
func WithCheckRedirect(checkRedirect func(*http.Request, []*http.Request) error) (f ClientOptionFunc) {
	return func(opts *ClientOptions) {
		opts.CheckRedirect = checkRedirect
		opts.checkRedirectSet = true
	}
}

// WithCookieJar sets the cookie jar. A nil jar disables cookies on an injected
// client.
//
// Parameters:
//   - cookieJar (http.CookieJar): the cookie jar; nil disables cookies on
//     an injected client.
//
// Returns:
//   - f (ClientOptionFunc): a functional option that sets the cookie jar.
func WithCookieJar(cookieJar http.CookieJar) (f ClientOptionFunc) {
	return func(opts *ClientOptions) {
		opts.CookieJar = cookieJar
		opts.cookieJarSet = true
	}
}

// WithHeader sets one default header, replacing an earlier default with the
// same case-insensitive name. Request headers take precedence.
//
// Parameters:
//   - key (string): the header name; empty is ignored.
//   - value (string): the header value.
//
// Returns:
//   - f (ClientOptionFunc): a functional option that sets the default header.
func WithHeader(key, value string) (f ClientOptionFunc) {
	return func(opts *ClientOptions) {
		if key == "" {
			return
		}

		for index, header := range opts.Headers {
			if strings.EqualFold(header.Key, key) {
				opts.Headers[index].Value = value

				return
			}
		}

		opts.Headers = append(opts.Headers, HeaderField{Key: key, Value: value})
	}
}

// WithTimeout bounds an execution through response headers, including retries
// and waits. It does not bound the returned body; use WithResponseBodyTimeout
// or a request context deadline for that. Zero disables the execution bound.
//
// Parameters:
//   - timeout (time.Duration): the execution bound; zero disables it.
//
// Returns:
//   - f (ClientOptionFunc): a functional option that sets the execution timeout.
func WithTimeout(timeout time.Duration) (f ClientOptionFunc) {
	return func(opts *ClientOptions) {
		opts.Timeout = timeout
	}
}

// WithResponseBodyTimeout bounds body reads after a response is handed out.
// Zero disables the separate body bound; the request context still applies.
//
// Parameters:
//   - timeout (time.Duration): the body-read bound; zero disables it.
//
// Returns:
//   - f (ClientOptionFunc): a functional option that sets the body timeout.
func WithResponseBodyTimeout(timeout time.Duration) (f ClientOptionFunc) {
	return func(opts *ClientOptions) {
		opts.ResponseBodyTimeout = timeout
	}
}

// WithAttemptTimeout bounds one attempt through response headers. Zero
// disables the attempt bound.
//
// Parameters:
//   - timeout (time.Duration): the per-attempt bound; zero disables it.
//
// Returns:
//   - f (ClientOptionFunc): a functional option that sets the attempt timeout.
func WithAttemptTimeout(timeout time.Duration) (f ClientOptionFunc) {
	return func(opts *ClientOptions) {
		opts.AttemptTimeout = timeout
	}
}

// WithRetryMaxAttempts sets the total attempt count, including the first. The
// value must be positive.
//
// Parameters:
//   - maxAttempts (int): the total attempt count, including the first;
//     must be positive.
//
// Returns:
//   - f (ClientOptionFunc): a functional option that sets the attempt count.
func WithRetryMaxAttempts(maxAttempts int) (f ClientOptionFunc) {
	return func(opts *ClientOptions) {
		opts.RetryMaxAttempts = maxAttempts
	}
}

// WithRetryPolicy sets the retry predicate. Nil selects DefaultRetryPolicy.
//
// Parameters:
//   - retryPolicy (RetryPolicy): the retry predicate; nil selects
//     [DefaultRetryPolicy].
//
// Returns:
//   - f (ClientOptionFunc): a functional option that sets the retry policy.
func WithRetryPolicy(retryPolicy RetryPolicy) (f ClientOptionFunc) {
	return func(opts *ClientOptions) {
		opts.RetryPolicy = retryPolicy
	}
}

// WithRetryIdempotent restricts retries and protocol fallback resends to
// idempotent methods. It defaults to true. Set false only when repeating POST
// or PATCH is safe for the application.
//
// Parameters:
//   - enabled (bool): true restricts retries to idempotent methods.
//
// Returns:
//   - f (ClientOptionFunc): a functional option that sets the idempotency restriction.
func WithRetryIdempotent(enabled bool) (f ClientOptionFunc) {
	return func(opts *ClientOptions) {
		opts.IdempotentRetries = enabled
	}
}

// WithRateLimiter sets the limiter waited on before each attempt. A limiter
// shared across clients must be safe for concurrent use.
//
// Parameters:
//   - rateLimiter (RateLimiter): the limiter waited on before each
//     attempt; a shared limiter must be safe for concurrent use.
//
// Returns:
//   - f (ClientOptionFunc): a functional option that sets the rate limiter.
func WithRateLimiter(rateLimiter RateLimiter) (f ClientOptionFunc) {
	return func(opts *ClientOptions) {
		opts.RateLimiter = rateLimiter
	}
}

// WithOnRequest sets the callback run before each attempt.
//
// Parameters:
//   - onRequest (OnRequestFunc): the pre-attempt callback.
//
// Returns:
//   - f (ClientOptionFunc): a functional option that sets the request callback.
func WithOnRequest(onRequest OnRequestFunc) (f ClientOptionFunc) {
	return func(opts *ClientOptions) {
		opts.OnRequest = onRequest
	}
}

// WithOnResponse sets the callback run after each attempt and before the retry
// decision. It must not close or drain the response body.
//
// Parameters:
//   - onResponse (OnResponseFunc): the post-attempt callback; it must
//     not close or drain the response body.
//
// Returns:
//   - f (ClientOptionFunc): a functional option that sets the response callback.
func WithOnResponse(onResponse OnResponseFunc) (f ClientOptionFunc) {
	return func(opts *ClientOptions) {
		opts.OnResponse = onResponse
	}
}

// WithOnRetry sets the callback run before each retry wait. The failed
// response body has already been drained and closed.
//
// Parameters:
//   - onRetry (OnRetryFunc): the pre-retry-wait callback.
//
// Returns:
//   - f (ClientOptionFunc): a functional option that sets the retry callback.
func WithOnRetry(onRetry OnRetryFunc) (f ClientOptionFunc) {
	return func(opts *ClientOptions) {
		opts.OnRetry = onRetry
	}
}

// WithPrepareRetry sets the hook run after the retry wait and before the next
// attempt. Use it to refresh request signatures or tokens.
//
// Parameters:
//   - prepareRetry (PrepareRetryFunc): the pre-retry hook, used to
//     refresh request signatures or tokens.
//
// Returns:
//   - f (ClientOptionFunc): a functional option that sets the prepare hook.
func WithPrepareRetry(prepareRetry PrepareRetryFunc) (f ClientOptionFunc) {
	return func(opts *ClientOptions) {
		opts.PrepareRetry = prepareRetry
	}
}

// WithMaxRequestsBeforeIdleClose sets how many completed executions trigger an
// idle-connection cleanup. Zero disables periodic cleanup.
//
// Parameters:
//   - n (int): the completed-execution count triggering cleanup; zero
//     disables periodic cleanup.
//
// Returns:
//   - f (ClientOptionFunc): a functional option that sets the cleanup threshold.
func WithMaxRequestsBeforeIdleClose(n int) (f ClientOptionFunc) {
	return func(opts *ClientOptions) {
		opts.MaxRequestsBeforeIdleClose = n
	}
}

// WithResponseReadLimit caps bytes drained from retried response bodies. The
// value must be positive.
//
// Parameters:
//   - responseReadLimit (int64): the drain cap in bytes; must be
//     positive.
//
// Returns:
//   - f (ClientOptionFunc): a functional option that sets the drain limit.
func WithResponseReadLimit(responseReadLimit int64) (f ClientOptionFunc) {
	return func(opts *ClientOptions) {
		opts.ResponseReadLimit = responseReadLimit
	}
}

// WithRequestRetryPolicy overrides the retry policy for one execution.
//
// Parameters:
//   - retryPolicy (RetryPolicy): the retry predicate for this execution;
//     nil keeps the client configuration.
//
// Returns:
//   - f (RequestOptionFunc): a functional option that overrides the retry policy.
func WithRequestRetryPolicy(retryPolicy RetryPolicy) (f RequestOptionFunc) {
	return func(opts *RequestOptions) {
		if retryPolicy != nil {
			opts.RetryPolicy = retryPolicy
		}
	}
}

// WithRequestRetryIdempotent overrides the idempotency restriction for one
// execution.
//
// Parameters:
//   - enabled (bool): true restricts retries to idempotent methods for
//     this execution.
//
// Returns:
//   - f (RequestOptionFunc): a functional option that overrides the idempotency restriction.
func WithRequestRetryIdempotent(enabled bool) (f RequestOptionFunc) {
	return func(opts *RequestOptions) {
		opts.IdempotentRetries = enabled
	}
}

// WithRequestRateLimiter overrides the rate limiter for one execution.
//
// Parameters:
//   - rateLimiter (RateLimiter): the limiter for this execution; nil
//     keeps the client configuration.
//
// Returns:
//   - f (RequestOptionFunc): a functional option that overrides the rate limiter.
func WithRequestRateLimiter(rateLimiter RateLimiter) (f RequestOptionFunc) {
	return func(opts *RequestOptions) {
		if rateLimiter != nil {
			opts.RateLimiter = rateLimiter
		}
	}
}

// WithRequestOnRequest overrides the pre-attempt callback for one execution.
//
// Parameters:
//   - onRequest (OnRequestFunc): the pre-attempt callback for this
//     execution; nil keeps the client configuration.
//
// Returns:
//   - f (RequestOptionFunc): a functional option that overrides the request callback.
func WithRequestOnRequest(onRequest OnRequestFunc) (f RequestOptionFunc) {
	return func(opts *RequestOptions) {
		if onRequest != nil {
			opts.OnRequest = onRequest
		}
	}
}

// WithRequestOnResponse overrides the post-attempt callback for one execution.
//
// Parameters:
//   - onResponse (OnResponseFunc): the post-attempt callback for this
//     execution; nil keeps the client configuration.
//
// Returns:
//   - f (RequestOptionFunc): a functional option that overrides the response callback.
func WithRequestOnResponse(onResponse OnResponseFunc) (f RequestOptionFunc) {
	return func(opts *RequestOptions) {
		if onResponse != nil {
			opts.OnResponse = onResponse
		}
	}
}

// WithRequestOnRetry overrides the retry callback for one execution.
//
// Parameters:
//   - onRetry (OnRetryFunc): the pre-retry-wait callback for this
//     execution; nil keeps the client configuration.
//
// Returns:
//   - f (RequestOptionFunc): a functional option that overrides the retry callback.
func WithRequestOnRetry(onRetry OnRetryFunc) (f RequestOptionFunc) {
	return func(opts *RequestOptions) {
		if onRetry != nil {
			opts.OnRetry = onRetry
		}
	}
}

// WithRequestPrepareRetry overrides the pre-retry hook for one execution.
//
// Parameters:
//   - prepareRetry (PrepareRetryFunc): the pre-retry hook for this
//     execution; nil keeps the client configuration.
//
// Returns:
//   - f (RequestOptionFunc): a functional option that overrides the prepare hook.
func WithRequestPrepareRetry(prepareRetry PrepareRetryFunc) (f RequestOptionFunc) {
	return func(opts *RequestOptions) {
		if prepareRetry != nil {
			opts.PrepareRetry = prepareRetry
		}
	}
}

// WithRequestAttemptTimeout overrides the per-attempt timeout for one
// execution. Zero disables it.
//
// Parameters:
//   - timeout (time.Duration): the per-attempt bound for this execution;
//     zero disables it.
//
// Returns:
//   - f (RequestOptionFunc): a functional option that overrides the attempt timeout.
func WithRequestAttemptTimeout(timeout time.Duration) (f RequestOptionFunc) {
	return func(opts *RequestOptions) {
		opts.AttemptTimeout = timeout
	}
}

// WithRequestResponseReadLimit overrides the retry-drain limit for one
// execution. Non-positive values keep the client configuration.
//
// Parameters:
//   - responseReadLimit (int64): the drain cap for this execution;
//     non-positive keeps the client configuration.
//
// Returns:
//   - f (RequestOptionFunc): a functional option that overrides the drain limit.
func WithRequestResponseReadLimit(responseReadLimit int64) (f RequestOptionFunc) {
	return func(opts *RequestOptions) {
		if responseReadLimit > 0 {
			opts.ResponseReadLimit = responseReadLimit
		}
	}
}

// RequestEvent describes an outgoing attempt.
//
// Fields:
//   - Attempt (int): the 1-based attempt number.
//   - Request (*http.Request): the attempt request.
type RequestEvent struct {
	Attempt int
	Request *http.Request
}

// OnRequestFunc observes an outgoing attempt.
//
// Parameters:
//   - event (RequestEvent): the attempt descriptor.
type OnRequestFunc func(event RequestEvent)

// ResponseEvent describes an attempt outcome.
//
// Fields:
//   - Attempt (int): the 1-based attempt number.
//   - Request (*http.Request): the attempt request.
//   - Response (*http.Response): the attempt response, or nil when the
//     transport failed before any response arrived.
//   - Err (error): the attempt's transport error, or nil.
//   - Duration (time.Duration): the attempt's elapsed time.
type ResponseEvent struct {
	Attempt  int
	Request  *http.Request
	Response *http.Response
	Err      error
	Duration time.Duration
}

// OnResponseFunc observes an attempt outcome before retry classification. It
// must not close or drain the response body.
//
// Parameters:
//   - event (ResponseEvent): the outcome descriptor.
type OnResponseFunc func(event ResponseEvent)

// RetryEvent describes an approved retry and its wait.
//
// Fields:
//   - Attempt (int): the 1-based number of the failed attempt.
//   - Request (*http.Request): the failed attempt request.
//   - Response (*http.Response): the failed attempt response, already drained
//     and closed, or nil.
//   - Err (error): the failure that triggered the retry.
//   - Wait (time.Duration): the delay before the next attempt.
//   - Duration (time.Duration): the failed attempt's elapsed time.
type RetryEvent struct {
	Attempt  int
	Request  *http.Request
	Response *http.Response
	Err      error
	Wait     time.Duration
	Duration time.Duration
}

// OnRetryFunc observes an approved retry before its wait.
//
// Parameters:
//   - event (RetryEvent): the retry descriptor.
type OnRetryFunc func(event RetryEvent)

// PrepareRetryFunc runs after a retry wait and before the next attempt. Use
// it to refresh request signatures or tokens.
//
// Parameters:
//   - ctx (context.Context): the overall execution context.
//   - req (*http.Request): the request the next attempt will send; the hook
//     may update it.
//   - attempt (int): the 1-based number of the upcoming attempt.
//
// Returns:
//   - err (error): non-nil stops execution and the error is returned to the
//     caller.
type PrepareRetryFunc func(ctx context.Context, req *http.Request, attempt int) (err error)
