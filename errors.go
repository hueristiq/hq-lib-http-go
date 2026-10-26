package http

import (
	"errors"

	hqgohttprequest "github.com/hueristiq/hq-lib-http-go/request"
)

// Sentinel errors classify the failures callers are expected to inspect with
// [errors.Is]. Errors returned by the client wrap them with %w.
var (
	// ErrNilRequest reports a nil request passed to [Client.Do] or
	// [Client.RoundTrip].
	ErrNilRequest = errors.New("request is nil")

	// ErrNilRequestURL reports a request without a URL passed to [Client.Do]
	// or [Client.RoundTrip].
	ErrNilRequestURL = errors.New("request URL is nil")

	// ErrNoURL reports a [RequestBuilder.Do] without a URL.
	ErrNoURL = errors.New("no URL configured")

	// ErrBodyNotReplayable reports a request body that cannot be rewound for
	// another attempt. The client stops before scheduling the retry and
	// returns a [*RetryError] built from the last response. The error matches
	// this sentinel with errors.Is and, because the retry budget was not
	// exhausted, does not match [ErrRetriesExhausted].
	ErrBodyNotReplayable = errors.New("request body cannot be replayed")

	// ErrRequestBodyTooLarge reports a request body that exceeds the bound
	// configured with [WithMaxRequestBodySize]. It aliases
	// [github.com/hueristiq/hq-lib-http-go/request.ErrBodyTooLarge], so
	// errors.Is matches either sentinel.
	ErrRequestBodyTooLarge = hqgohttprequest.ErrBodyTooLarge

	// ErrHTTP3ProxyUnsupported reports a forced HTTP/3 request to which a
	// proxy applies.
	ErrHTTP3ProxyUnsupported = errors.New("HTTP/3 does not support proxies")

	// ErrRetriesExhausted reports that the retry budget was exhausted. A
	// [*RetryError] matches it unless the outcome was stopped by a body that
	// could not be replayed, which matches [ErrBodyNotReplayable] instead.
	ErrRetriesExhausted = errors.New("retries exhausted")

	// ErrResponseBodyTooLarge reports a response body that exceeded the bound
	// configured with [WithMaxResponseBodySize]. The body helpers return the
	// prefix read up to the limit alongside this error.
	ErrResponseBodyTooLarge = errors.New("response body exceeds the configured limit")

	// ErrClientClosed reports an execution through a client after [Client.Close].
	ErrClientClosed = errors.New("client is closed")
)
