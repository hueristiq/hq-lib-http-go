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
type ClientOptionFunc func(opts *ClientOptions)

// New constructs a client. The default uses pooled HTTP/1.1+HTTP/2, a 30-second
// execution timeout through response headers, three idempotent attempts with
// exponential backoff, environment proxying, and [DefaultUserAgent]. Body reads
// have no separate timeout by default. Callers own returned response bodies.
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
func WithHTTPClient(client *http.Client) (f ClientOptionFunc) {
	return func(opts *ClientOptions) {
		opts.HTTPClient = client
	}
}

// WithHTTP3Client injects the HTTP/3 client used by ProtocolHTTP3 and
// ProtocolAutoHTTP3. The application owns its QUIC and transport configuration.
func WithHTTP3Client(client *http.Client) (f ClientOptionFunc) {
	return func(opts *ClientOptions) {
		opts.HTTP3Client = client
	}
}

// WithProtocol selects the HTTP protocol. It cannot configure an injected
// HTTPClient; New rejects that contradictory combination.
func WithProtocol(protocol Protocol) (f ClientOptionFunc) {
	return func(opts *ClientOptions) {
		opts.Protocol = protocol
		opts.protocolSet = true
	}
}

// WithHTTP3FallbackTTL sets how long a failed host skips HTTP/3 in auto mode.
// Zero disables the failure cache.
func WithHTTP3FallbackTTL(ttl time.Duration) (f ClientOptionFunc) {
	return func(opts *ClientOptions) {
		opts.HTTP3FallbackTTL = ttl
	}
}

// WithTLSConfig configures library-built transports. The configuration is
// cloned before use.
func WithTLSConfig(config *tls.Config) (f ClientOptionFunc) {
	return func(opts *ClientOptions) {
		opts.TLSConfig = config
	}
}

// WithTransportWrapper decorates each library-built transport. A nil result
// preserves the input transport. Custom HTTPClient and HTTP3Client transports
// must be wrapped by the application instead.
func WithTransportWrapper(wrapper func(http.RoundTripper) http.RoundTripper) (f ClientOptionFunc) {
	return func(opts *ClientOptions) {
		opts.TransportWrapper = wrapper
	}
}

// WithProxy sets the proxy selector for library-built HTTP/1.x and HTTP/2
// transports. HTTP/3 rejects proxied requests or falls back in auto mode.
func WithProxy(proxy func(*http.Request) (*url.URL, error)) (f ClientOptionFunc) {
	return func(opts *ClientOptions) {
		opts.Proxy = proxy
	}
}

// WithCheckRedirect sets the redirect policy. A nil policy restores net/http's
// default on an injected client.
func WithCheckRedirect(checkRedirect func(*http.Request, []*http.Request) error) (f ClientOptionFunc) {
	return func(opts *ClientOptions) {
		opts.CheckRedirect = checkRedirect
		opts.checkRedirectSet = true
	}
}

// WithCookieJar sets the cookie jar. A nil jar disables cookies on an injected
// client.
func WithCookieJar(cookieJar http.CookieJar) (f ClientOptionFunc) {
	return func(opts *ClientOptions) {
		opts.CookieJar = cookieJar
		opts.cookieJarSet = true
	}
}

// WithHeader sets one default header, replacing an earlier default with the
// same case-insensitive name. Request headers take precedence.
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
func WithTimeout(timeout time.Duration) (f ClientOptionFunc) {
	return func(opts *ClientOptions) {
		opts.Timeout = timeout
	}
}

// WithResponseBodyTimeout bounds body reads after a response is handed out.
// Zero disables the separate body bound; the request context still applies.
func WithResponseBodyTimeout(timeout time.Duration) (f ClientOptionFunc) {
	return func(opts *ClientOptions) {
		opts.ResponseBodyTimeout = timeout
	}
}

// WithAttemptTimeout bounds one attempt through response headers. Zero
// disables the attempt bound.
func WithAttemptTimeout(timeout time.Duration) (f ClientOptionFunc) {
	return func(opts *ClientOptions) {
		opts.AttemptTimeout = timeout
	}
}

// WithRetryMaxAttempts sets the total attempt count, including the first. The
// value must be positive.
func WithRetryMaxAttempts(maxAttempts int) (f ClientOptionFunc) {
	return func(opts *ClientOptions) {
		opts.RetryMaxAttempts = maxAttempts
	}
}

// WithRetryPolicy sets the retry predicate. Nil selects DefaultRetryPolicy.
func WithRetryPolicy(retryPolicy RetryPolicy) (f ClientOptionFunc) {
	return func(opts *ClientOptions) {
		opts.RetryPolicy = retryPolicy
	}
}

// WithRetryIdempotent restricts retries and protocol fallback resends to
// idempotent methods. It defaults to true. Set false only when repeating POST
// or PATCH is safe for the application.
func WithRetryIdempotent(enabled bool) (f ClientOptionFunc) {
	return func(opts *ClientOptions) {
		opts.IdempotentRetries = enabled
	}
}

// WithRateLimiter sets the limiter waited on before each attempt. A limiter
// shared across clients must be safe for concurrent use.
func WithRateLimiter(rateLimiter RateLimiter) (f ClientOptionFunc) {
	return func(opts *ClientOptions) {
		opts.RateLimiter = rateLimiter
	}
}

// WithOnRequest sets the callback run before each attempt.
func WithOnRequest(onRequest OnRequestFunc) (f ClientOptionFunc) {
	return func(opts *ClientOptions) {
		opts.OnRequest = onRequest
	}
}

// WithOnResponse sets the callback run after each attempt and before the retry
// decision. It must not close or drain the response body.
func WithOnResponse(onResponse OnResponseFunc) (f ClientOptionFunc) {
	return func(opts *ClientOptions) {
		opts.OnResponse = onResponse
	}
}

// WithOnRetry sets the callback run before each retry wait. The failed
// response body has already been drained and closed.
func WithOnRetry(onRetry OnRetryFunc) (f ClientOptionFunc) {
	return func(opts *ClientOptions) {
		opts.OnRetry = onRetry
	}
}

// WithPrepareRetry sets the hook run after the retry wait and before the next
// attempt. Use it to refresh request signatures or tokens.
func WithPrepareRetry(prepareRetry PrepareRetryFunc) (f ClientOptionFunc) {
	return func(opts *ClientOptions) {
		opts.PrepareRetry = prepareRetry
	}
}

// WithMaxRequestsBeforeIdleClose sets how many completed executions trigger an
// idle-connection cleanup. Zero disables periodic cleanup.
func WithMaxRequestsBeforeIdleClose(n int) (f ClientOptionFunc) {
	return func(opts *ClientOptions) {
		opts.MaxRequestsBeforeIdleClose = n
	}
}

// WithResponseReadLimit caps bytes drained from retried response bodies. The
// value must be positive.
func WithResponseReadLimit(responseReadLimit int64) (f ClientOptionFunc) {
	return func(opts *ClientOptions) {
		opts.ResponseReadLimit = responseReadLimit
	}
}

// WithRequestRetryPolicy overrides the retry policy for one execution.
func WithRequestRetryPolicy(retryPolicy RetryPolicy) (f RequestOptionFunc) {
	return func(opts *RequestOptions) {
		if retryPolicy != nil {
			opts.RetryPolicy = retryPolicy
		}
	}
}

// WithRequestRetryIdempotent overrides the idempotency restriction for one
// execution.
func WithRequestRetryIdempotent(enabled bool) (f RequestOptionFunc) {
	return func(opts *RequestOptions) {
		opts.IdempotentRetries = enabled
	}
}

// WithRequestRateLimiter overrides the rate limiter for one execution.
func WithRequestRateLimiter(rateLimiter RateLimiter) (f RequestOptionFunc) {
	return func(opts *RequestOptions) {
		if rateLimiter != nil {
			opts.RateLimiter = rateLimiter
		}
	}
}

// WithRequestOnRequest overrides the pre-attempt callback for one execution.
func WithRequestOnRequest(onRequest OnRequestFunc) (f RequestOptionFunc) {
	return func(opts *RequestOptions) {
		if onRequest != nil {
			opts.OnRequest = onRequest
		}
	}
}

// WithRequestOnResponse overrides the post-attempt callback for one execution.
func WithRequestOnResponse(onResponse OnResponseFunc) (f RequestOptionFunc) {
	return func(opts *RequestOptions) {
		if onResponse != nil {
			opts.OnResponse = onResponse
		}
	}
}

// WithRequestOnRetry overrides the retry callback for one execution.
func WithRequestOnRetry(onRetry OnRetryFunc) (f RequestOptionFunc) {
	return func(opts *RequestOptions) {
		if onRetry != nil {
			opts.OnRetry = onRetry
		}
	}
}

// WithRequestPrepareRetry overrides the pre-retry hook for one execution.
func WithRequestPrepareRetry(prepareRetry PrepareRetryFunc) (f RequestOptionFunc) {
	return func(opts *RequestOptions) {
		if prepareRetry != nil {
			opts.PrepareRetry = prepareRetry
		}
	}
}

// WithRequestAttemptTimeout overrides the per-attempt timeout for one
// execution. Zero disables it.
func WithRequestAttemptTimeout(timeout time.Duration) (f RequestOptionFunc) {
	return func(opts *RequestOptions) {
		opts.AttemptTimeout = timeout
	}
}

// WithRequestResponseReadLimit overrides the retry-drain limit for one
// execution. Non-positive values keep the client configuration.
func WithRequestResponseReadLimit(responseReadLimit int64) (f RequestOptionFunc) {
	return func(opts *RequestOptions) {
		if responseReadLimit > 0 {
			opts.ResponseReadLimit = responseReadLimit
		}
	}
}

// RequestEvent describes an outgoing attempt.
type RequestEvent struct {
	Attempt int
	Request *http.Request
}

// OnRequestFunc observes an outgoing attempt.
type OnRequestFunc func(event RequestEvent)

// ResponseEvent describes an attempt outcome.
type ResponseEvent struct {
	Attempt  int
	Request  *http.Request
	Response *http.Response
	Err      error
	Duration time.Duration
}

// OnResponseFunc observes an attempt outcome before retry classification.
type OnResponseFunc func(event ResponseEvent)

// RetryEvent describes an approved retry and its wait.
type RetryEvent struct {
	Attempt  int
	Request  *http.Request
	Response *http.Response
	Err      error
	Wait     time.Duration
	Duration time.Duration
}

// OnRetryFunc observes an approved retry before its wait.
type OnRetryFunc func(event RetryEvent)

// PrepareRetryFunc runs after a retry wait and before the next attempt.
type PrepareRetryFunc func(ctx context.Context, req *http.Request, attempt int) error
