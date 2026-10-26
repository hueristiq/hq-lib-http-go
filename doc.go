// Package http provides an HTTP client for resilient, configurable HTTP
// communication.
//
// It suits applications that call HTTP services and must absorb transient
// failure — connection errors, rate limiting (429), and server errors (5xx) —
// without hand-rolling retry loops, and tooling that needs control over the
// finer points: request building, header management, transport selection,
// retry diagnostics, and User-Agent generation.
//
// # Entry points
//
// [New] creates a [Client]. Requests are executed with
// [Client.Do], with the convenience methods ([Client.Get], [Client.Post],
// [Client.Query], and friends), or assembled fluently with [Client.Request]
// — a [RequestBuilder] chain such as:
//
//	res, err := client.Request().
//		Method(method.GET).
//		URL("https://api.example.com/users").
//		Param("page", "1").
//		SetHeader(header.Accept.String(), mime.JSON.String()).
//		Body(payload).
//		Context(ctx).
//		Do()
//
// Do returns a [Response] wrapper with a [Body]. The body caches content for
// [Body.Bytes], [Body.String], [Body.JSON], [Body.XML], and [Body.UTF8], and
// supports streaming through [Body.Stream] and Server-Sent Events through
// [Body.SSE]; SSE keeps the last event ID and retry time across events. An
// empty body yields an empty string from [Body.UTF8], and [Body.XML] honors the
// encoding declaration of the document. [WithMaxResponseBodySize] caps how much
// the caching helpers buffer. The wrapper keeps status classification
// ([Response.IsSuccess] and friends) and exposes the raw response through
// [Response.Std]. Request bodies can be supplied raw with [RequestBuilder.Body],
// encoded with [RequestBuilder.JSON], [RequestBuilder.XML],
// [RequestBuilder.Form], or [RequestBuilder.Multipart], or streamed with
// [RequestBuilder.BodyStream] and [MultipartForm.Stream]. A reader passed to
// [RequestBuilder.Body] is buffered when the setter is called, so repeated
// executions replay it, and an empty buffered body is sent with
// Content-Length: 0. Buffered bodies are replayable on retries;
// [WithMaxRequestBodySize] caps them. [RequestBuilder.Param] appends to the raw
// query without rewriting existing pairs. [RequestBuilder.Context] sets a
// per-request context; [WithContext] sets a client-level default.
//
// [Client.Close] shuts a client down: it cancels in-flight executions with
// [ErrClientClosed], closes idle connections, and rejects later executions.
// [Client.CloseIdleConnections] releases pooled connections without disabling
// the client.
//
// [Client.Std] returns a standard [http.Client] whose transport is the retry
// engine, and [Client.RoundTrip] implements [http.RoundTripper] directly. Both
// preserve the client's redirect policy. A received response, including a 5xx,
// is returned with a nil error as the standard interfaces require, except for a
// redirect-policy abort, which is passed through with its error.
//
// The package-level shortcuts ([Get], [Post], and friends) run the same
// requests against a shared default client built lazily on first use.
//
// # Configuration
//
// Client behavior is set through functional options passed to [New]:
// [WithContext], [WithTimeout], [WithAttemptTimeout],
// [WithHTTPClient], [WithTransport], [WithTransportBuilder],
// [WithHTTP3TransportBuilder], [WithTransportWrapper], [WithDialContext],
// [WithDialer], [WithTLSHandshakeTimeout], [WithProxy], [WithCheckRedirect],
// [WithCookieJar], [WithHeader] (including a User-Agent default), the retry
// knobs ([WithRetryMax], [WithRetryWaitMin], [WithRetryWaitMax],
// [WithRetryBackoff], [WithRetryPolicy], [WithRetryIdempotent],
// [WithRetryErrorBodyLimit]), [WithOnRetryExhaustion], [WithPrepareRetry],
// [WithRateLimiter], the observability callbacks ([WithOnRequest],
// [WithOnResponse], [WithOnRetry]), [WithMaxRequestsBeforeIdleClose],
// [WithResponseBodyTimeout], [WithMaxResponseBodySize],
// [WithMaxRequestBodySize], [WithResponseReadLimit], and
// [WithHTTP3FallbackTTL]. Unset options use their defaults; invalid values are
// normalized. The execution timeout, per-attempt timeout, response-body
// timeout, response-body cap, request-body cap, and retry-error body capture
// are all off by default.
//
// [WithHTTPClient] wins over the protocol and transport-building options.
// [WithTransport] replaces the built transport and bypasses [WithProtocol],
// [WithTLSConfig], [WithProxy], [WithTransportBuilder],
// [WithHTTP3TransportBuilder], and [WithTransportWrapper]. For a library-built
// transport the order is TLS configuration, dial and TLS-handshake settings,
// [WithTransportBuilder] (or [WithHTTP3TransportBuilder]), [WithProxy], then
// [WithTransportWrapper].
//
// A single request execution overrides the retry knobs, the rate limiter, the
// attempt timeout, the retry hook, the per-attempt callbacks, the pre-retry
// hook, the exhaustion handler, the idempotency restriction, the response-body
// cap, and the drain and snapshot bounds through [RequestOptionFunc] values
// ([WithRequestRetryMax] and friends) passed to [Client.Do], the convenience
// methods, or [RequestBuilder.WithOptions]. The request-body cap is
// client-wide. The client configuration stays untouched.
//
// The rate limiter registered with [WithRateLimiter], or overridden for one
// request with [WithRequestRateLimiter], is any value whose Wait method honors
// a context — for example a *limiter.Limiter from
// github.com/hueristiq/hq-lib-limiter-go. [RateLimiterFunc] adapts a plain
// function or a limiter with a different method set. The limiter is waited on
// before every retry-engine attempt; the internal HTTP/2 or HTTP/3 fallback
// resend belongs to the same attempt and does not wait again. A wait error
// stops the execution before the attempt.
//
// # Retries
//
// The retry engine is [github.com/hueristiq/hq-lib-retrier-go]: attempts are
// bounded by [WithRetryMax], delayed by the [WithRetryBackoff] strategy
// between [WithRetryWaitMin] and [WithRetryWaitMax]. After each attempt the
// [RetryPolicy] sees the attempt request, the response, the error, and the
// 1-based attempt number, so policies can react to status codes and method
// idempotency; [DefaultRetryPolicy] retries connection and transport errors,
// 408, 429, and 5xx responses (except 501), and never retries a canceled
// context, a response paired with an error (such as a redirect-policy abort),
// an exhausted redirect limit, an unsupported scheme, a TLS certificate
// verification failure, or a permanent DNS failure such as NXDOMAIN. A
// Retry-After header on a retried response is honored as the wait before the
// next attempt, capped at the maximum retry wait. [WithOnRetry] reports each
// approved retry with its attempt, response, error, duration, and computed
// wait. [WithPrepareRetry] runs after the wait and before the next attempt, so
// a caller can re-sign a request; returning an error stops the execution.
//
// Retries are idempotent-only by default: GET, HEAD, OPTIONS, TRACE, PUT,
// DELETE, and QUERY are retried; POST and PATCH are not. QUERY is safe and
// idempotent per RFC 10008. The restriction also covers the internal HTTP/2 or
// HTTP/3 fallback resend. Use [WithRetryIdempotent] with false, or
// [WithRequestRetryIdempotent] with false, to retry every method, accepting
// that a write may be sent more than once.
//
// When a retry is approved for an idempotent request, a body that cannot be
// replayed stops the execution with the last outcome instead of another
// attempt: [Client.Do] returns a [RetryError] built from the response that
// could not be retried, and the error matches [ErrBodyNotReplayable] with
// errors.Is. When retries are exhausted, [Client.Do] returns a [RetryError]
// carrying the attempt count, the attempt history, the last status and
// headers, and the last failure; it matches [ErrRetriesExhausted] with
// errors.Is, except for the final, non-replayed outcome above. A bounded copy
// of the final response body is included only when capture is enabled with
// [WithRetryErrorBodyLimit]; a read error from that capture is reported in
// [RetryError.BodyErr]. [WithRetryMax] with zero disables additional attempts
// but not this classification, so a persistent 429 or 5xx still produces a
// [RetryError]. [WithOnRetryExhaustion] replaces that default and can hand the
// final response, body open, to the caller.
//
// # HTTP protocols
//
// [WithProtocol] selects the HTTP version:
//
//   - [ProtocolAuto] (the default) negotiates HTTP/2 over TLS and falls back
//     to HTTP/1.1. Cleartext HTTP/2 servers that answer an HTTP/1.x request
//     with HTTP/2 frames are retried once on an h2c fallback.
//   - [ProtocolAutoHTTP3] tries HTTP/3 first and falls back to [ProtocolAuto]
//     on a transient transport error. A response paired with an error (a
//     redirect-policy abort) is final and does not trigger the fallback, and
//     permanent failures (certificate, redirect limit, unsupported scheme,
//     NXDOMAIN) are not retried on another protocol. When a proxy applies to
//     the request, the attempt goes directly to the auto client: HTTP/3 does
//     not support proxies. The fallback resends reusable bodies; a body
//     exposing neither a reusable reader nor GetBody skips the fallback. A host
//     that failed HTTP/3 is sent through the auto client for
//     [DefaultHTTP3FallbackTTL], or the value from [WithHTTP3FallbackTTL].
//   - [ProtocolHTTP1] forces HTTP/1.x.
//   - [ProtocolHTTP2] forces HTTP/2 over TLS and h2c prior knowledge for
//     http:// URLs.
//   - [ProtocolHTTP3] forces HTTP/3. It requires https:// URLs, uses UDP, and
//     does not honor HTTP proxies; a request to which a proxy applies fails
//     with [ErrHTTP3ProxyUnsupported] rather than bypassing the proxy.
//
// The proxy that applies is resolved per request from [WithProxy] when
// supplied, and from the environment otherwise; a selector returning nil
// sends the request directly and permits HTTP/3. [WithTLSConfig] configures
// transports the library builds and is cloned before use. [WithHTTPClient]
// takes precedence, so a custom client must configure its own TLS and
// protocol. [WithTransport] supplies a transport directly.
// [WithTransportBuilder], [WithDialContext], [WithDialer], and
// [WithTLSHandshakeTimeout] customize library-built HTTP/1.x and HTTP/2
// transports; [WithHTTP3TransportBuilder] customizes a library-built HTTP/3
// transport, including its Dial function, QUICConfig, and additional settings.
// [WithTransportWrapper] decorates any built transport and preserves protocol
// selection. HTTP/3 ignores the TCP dial and TLS-handshake options. The
// fallback clients clone the primary client's redirect policy, cookie jar, and
// timeout.
//
// # Context
//
// Context is resolved per execution. [Client.Do] uses the request's context
// when it is not [context.Background]; otherwise the [WithContext] client
// default applies, followed by [context.Background]. [RequestBuilder.Context]
// sets a per-request context and overrides the client default. The execution —
// every attempt and every wait, through receipt of the response headers — is
// bounded by that context. [WithTimeout] adds a client-level bound when
// configured; it is off by default, and a non-positive value disables it. A
// per-request timeout needs no option: derive it with context.WithTimeout.
// [WithAttemptTimeout] bounds each attempt through receipt of the headers,
// keeping one slow attempt from consuming the whole budget; body reads remain
// tied to the execution context and [WithResponseBodyTimeout]. Closing or
// draining the body releases the request context.
//
// The package-level shortcuts take an explicit context because they run against
// the shared default client.
//
// [Client.Do] applies the client's default headers and uses the request URL as
// given. Invalid percent escapes are repaired by the URL parser (a lone "%"
// becomes "%25"), while valid escapes and raw query text are preserved. It
// copies the request and its header map, so the caller's request is not
// mutated.
//
// # Concurrency
//
// A [Client] is safe for concurrent use. It is immutable after construction
// except for the counter driving the periodic idle-connection close, the HTTP/3
// failure cache, and the shutdown state, which are synchronized. [Client.Close]
// may be called concurrently with in-flight executions. Callbacks, policies,
// and the exhaustion handler are invoked synchronously on the goroutine running
// the attempt and must themselves be safe for concurrent use. A
// [RequestBuilder] is not safe for concurrent use.
//
// # Errors
//
// Errors are wrapped with the producing function ("http.Client.Do: ...") and
// %w, so errors.Is and errors.As keep working; a context that ends an
// execution surfaces as context.Canceled or context.DeadlineExceeded. Retry
// exhaustion surfaces as a [*RetryError] whose Unwrap exposes the last
// failure; an outcome stopped by a body that cannot be replayed also returns a
// [*RetryError], matching [ErrBodyNotReplayable] instead of
// [ErrRetriesExhausted]. The package exports sentinels for the common failures:
// [ErrNilRequest], [ErrNilRequestURL], [ErrNoURL], [ErrBodyNotReplayable],
// [ErrRequestBodyTooLarge], [ErrResponseBodyTooLarge],
// [ErrHTTP3ProxyUnsupported], [ErrRetriesExhausted], and [ErrClientClosed].
//
// # Out of scope
//
// TLS/browser impersonation is deliberately not supported: it requires a
// browser-grade TLS stack. Byte-exact raw HTTP exchanges are also out of
// scope; use the standard library or a dedicated tool. HTTP/1.x and HTTP/2
// use the standard library; HTTP/3 uses quic-go.
//
// The implementation relies on the Go standard library, plus
// github.com/hueristiq/hq-lib-url-go (URL parsing with scheme defaulting),
// github.com/hueristiq/hq-lib-retrier-go (the retry engine),
// github.com/quic-go/quic-go/http3 (HTTP/3), and golang.org/x/net/html/charset
// (UTF-8 conversion).
package http
