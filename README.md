# hq-lib-http-go

![made with go](https://img.shields.io/badge/made%20with-Go-1E90FF.svg) [![go reference](https://pkg.go.dev/badge/github.com/hueristiq/hq-lib-http-go.svg)](https://pkg.go.dev/github.com/hueristiq/hq-lib-http-go) [![license](https://img.shields.io/badge/license-MIT-gray.svg?color=1E90FF)](https://github.com/hueristiq/hq-lib-http-go/blob/main/LICENSE) ![maintenance](https://img.shields.io/badge/maintained%3F-yes-1E90FF.svg) [![open issues](https://github.com/hueristiq/hq-lib-http-go/issues?q=is:issue+is:open) [![closed issues](https://github.com/hueristiq/hq-lib-http-go/issues?q=is:issue+is:closed)](https://github.com/hueristiq/hq-lib-http-go/issues?q=is:issue+is:closed)

`hq-lib-http-go` adds retries, protocol fallback, rate limiting, and bounded lifecycle handling to standard Go HTTP requests.

## Features

- Standard `*http.Request` input and `*http.Response` output.
- HTTP/1.1, HTTP/2, h2c fallback, and injected HTTP/3 clients.
- Idempotent retries with `Retry-After`, typed transport failures, and attempt history.
- Execution, attempt, and response-body timeouts.
- Explicit rate-limiter, retry-policy, logger, callback, and transport seams.
- No shared default client, global registration, or client-stored request context.

## Installation

```bash
go get github.com/hueristiq/hq-lib-http-go@latest
```

Requires Go 1.26.0 or later.

## Basic request

```go
package main

import (
	"context"
	"io"
	"log"
	"net/http"

	hqgohttp "github.com/hueristiq/hq-lib-http-go"
)

func main() {
	client, err := hqgohttp.New()
	if err != nil {
		log.Fatal(err)
	}
	defer client.Close()

	req, err := http.NewRequestWithContext(context.Background(), http.MethodGet, "https://example.com", nil)
	if err != nil {
		log.Fatal(err)
	}

	res, err := client.Do(req)
	if err != nil {
		log.Fatal(err)
	}
	defer res.Body.Close()

	_, err = io.Copy(io.Discard, res.Body)
	if err != nil {
		log.Fatal(err)
	}
}
```

`Client.Do` follows standard HTTP status semantics. A final `503` is a response, not a Go error. Transport exhaustion returns `*RetryError`; `errors.Is(err, ErrRetriesExhausted)` classifies it. `RetryError.History` records attempts. See `examples/basic/main.go` for a local, runnable version.

The default client uses pooled HTTP/1.1 and HTTP/2, environment proxying, and a 30-second bound through response headers. It retries idempotent methods three times with exponential backoff. GET, HEAD, OPTIONS, TRACE, PUT, DELETE, and QUERY are idempotent. Set `WithRetryIdempotent(false)` only when repeating another method is safe.

Standard request bodies created from `bytes.Buffer`, `bytes.Reader`, or `strings.Reader` support `GetBody` and replay on retries. A streaming body without `GetBody` is never resent. After an HTTP response, the client returns that response if it cannot replay the body; after a transport failure, it returns an error matching `ErrBodyNotReplayable`.

## Context and timeouts

Put each request's context on the request. The client does not store a default context.

```go
ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
defer cancel()

req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
```

`WithTimeout` bounds the whole execution through response headers, including retry waits. `WithAttemptTimeout` bounds one attempt. `WithResponseBodyTimeout` starts when the response body is handed out and stops when the caller closes or drains it. The request context still governs body reads. `Close` cancels active requests and rejects later calls; `CloseIdleConnections` preserves the client for later requests.

## Retries and hooks

```go
client, err := hqgohttp.New(
	hqgohttp.WithRetryMaxAttempts(4),
	hqgohttp.WithAttemptTimeout(5*time.Second),
	hqgohttp.WithOnRetry(func(event hqgohttp.RetryEvent) {
		log.Printf("attempt %d failed: %v; retrying in %s", event.Attempt, event.Err, event.Wait)
	}),
	hqgohttp.WithPrepareRetry(func(_ context.Context, req *http.Request, _ int) error {
		req.Header.Set("Authorization", "Bearer "+token())
		return nil
	}),
)
```

`DefaultRetryPolicy` retries transient transport errors, `408`, `429`, and `5xx` except `501`. It does not retry canceled contexts, exhausted redirect chains, unsupported schemes, certificate failures, or NXDOMAIN. `WithRetryPolicy` replaces the policy. A `Retry-After` header overrides backoff up to the two-second cap.

Callbacks execute synchronously and must be safe for concurrent use when shared. `OnResponse` must not consume the response body. `OnRetry` runs after the failed body has been drained and closed. `PrepareRetry` runs after the wait and before the next request, so it can refresh credentials or signatures.

## Rate limiting

`WithRateLimiter` accepts a small interface:

```go
type RateLimiter interface {
	Wait(ctx context.Context, req *http.Request) error
}
```

The request argument supports per-host and per-endpoint policies. Adapt a `*limiter.Limiter` from `hq-lib-limiter-go` with `RateLimiterFunc(func(ctx context.Context, _ *http.Request) error { return limiter.Wait(ctx) })`. The limiter runs before each attempt; protocol fallback within one attempt does not wait again. A wait error stops the execution before transport I/O.

## Transport and protocols

`WithHTTPClient` injects a complete custom client. Configure its transport, proxy, TLS, and connection pooling directly. `WithTransportWrapper` decorates library-built transports while retaining protocol selection. `WithTLSConfig`, `WithProxy`, `WithCheckRedirect`, and `WithCookieJar` configure built clients. Contradictory combinations fail in `New`; options are never ignored silently.

`WithProtocol` selects `ProtocolAuto`, `ProtocolAutoHTTP3`, `ProtocolHTTP1`, `ProtocolHTTP2`, or `ProtocolHTTP3`. HTTP/3 requires an application-supplied client:

```go
client, err := hqgohttp.New(
	hqgohttp.WithProtocol(hqgohttp.ProtocolAutoHTTP3),
	hqgohttp.WithHTTP3Client(h3Client),
)
```

The application owns HTTP/3 transport and QUIC configuration. Auto mode falls back to HTTP/2 or HTTP/1.1 after transient failures and caches failed hosts for `DefaultHTTP3FallbackTTL`. Forced HTTP/3 rejects proxied requests with `ErrHTTP3ProxyUnsupported`. Auto HTTP/3 uses the standard fallback when a proxy applies.

`StripSensitiveHeadersOnRedirect` removes application credentials after a host change or HTTPS downgrade, including multi-hop chains. Go's client already protects standard credentials on cross-host redirects.

## Logging

`WithLogger` accepts the slog-compatible method set, so `*slog.Logger` works directly. `WithRequestLogger` overrides it for one execution. The client logs methods, redacted URLs, attempts, statuses, durations, and errors; it does not log headers, bodies, or cookies.

## Contributing

Contributions are welcome. Open an issue or pull request in the repository.

## Licensing

This package is licensed under the [MIT license](https://opensource.org/license/mit).
