# hq-lib-http-go

![made with go](https://img.shields.io/badge/made%20with-Go-1E90FF.svg) [![go reference](https://pkg.go.dev/badge/github.com/hueristiq/hq-lib-http-go.svg)](https://pkg.go.dev/github.com/hueristiq/hq-lib-http-go) [![license](https://img.shields.io/badge/license-MIT-gray.svg?color=1E90FF)](https://github.com/hueristiq/hq-lib-http-go/blob/main/LICENSE) ![maintenance](https://img.shields.io/badge/maintained%3F-yes-1E90FF.svg) [![open issues](https://img.shields.io/github/issues-raw/hueristiq/hq-lib-http-go.svg?style=flat&color=1E90FF)](https://github.com/hueristiq/hq-lib-http-go/issues?q=is:issue+is:open) [![closed issues](https://img.shields.io/github/issues-closed-raw/hueristiq/hq-lib-http-go.svg?style=flat&color=1E90FF)](https://github.com/hueristiq/hq-lib-http-go/issues?q=is:issue+is:closed) [![contribution](https://img.shields.io/badge/contributions-welcome-1E90FF.svg)](https://github.com/hueristiq/hq-lib-http-go/blob/main/CONTRIBUTING.md)

`hq-lib-http-go` is a [Go](https://golang.org/) package for resilient, configurable HTTP communication.

## Table of Contents

- [Features](#features)
- [Installation](#installation)
- [Usage](#usage)
	- [Client](#client)
	- [Fluent Request Builder](#fluent-request-builder)
	- [Response Handling](#response-handling)
	- [Retries](#retries)
	- [Rate Limiting](#rate-limiting)
	- [URL Parser Hook](#url-parser-hook)
	- [Retrier Hook](#retrier-hook)
	- [Standard Client Adapter](#standard-client-adapter)
	- [Transport and TLS](#transport-and-tls)
- [Contributing](#contributing)
- [Licensing](#licensing)

## Features

- **HTTP/1.1, HTTP/2, and HTTP/3:** `WithProtocol` selects auto negotiation (the default), HTTP/3-first with fallback, or a forced version. HTTP/3 uses quic-go, requires HTTPS over UDP, and does not honor proxies.
- **Fluent Request Builder:** Assemble requests with a chainable `Method().URL().Param().SetHeader().AddHeader().Body().Context(ctx).Do()` API.
- **Client Lifecycle:** `Close` cancels in-flight requests, closes idle connections, and rejects later executions with `ErrClientClosed`.
- **Typed Request Inputs and Response Helpers:** `JSON`, `XML`, `Form`, and `Multipart` encode bodies and set the matching `Content-Type`; the response `Body` exposes `Bytes`, `String`, `JSON`, `XML`, `UTF8`, `Stream`, `SSE`, and `Close`.
- **Idempotent-by-Default Retries:** The default policy retries connection and transport errors, `408`, `429`, and `5xx` responses, and skips permanent failures such as NXDOMAIN. Only idempotent methods are retried by default.
- **Retrier Hook:** `WithRetrier` accepts any engine with a `Retry` method — for example [hq-lib-retrier-go](https://github.com/hueristiq/hq-lib-retrier-go) adapted with `RetrierFunc` — without imposing a module dependency.
- **Typed Retry Errors:** When retries run out, `Do` returns a `*RetryError` with the attempt count, attempt history, status, headers, and the last failure.
- **Rate-Limiter Hook:** `WithRateLimiter` accepts any limiter with a `Wait(ctx) error` method — for example [hq-lib-limiter-go](https://github.com/hueristiq/hq-lib-limiter-go) — without imposing a module dependency.
- **URL-Parser Hook:** `WithURLParser` accepts any parser with `Parse(raw) (*url.URL, error)` — for example [hq-lib-url-go](https://github.com/hueristiq/hq-lib-url-go)'s parser adapted with `request.URLParserFunc`.
- **Transport and Dial Control:** `WithTransport`, `WithTransportBuilder`, `WithHTTP3TransportBuilder`, `WithDialContext`/`WithDialer`, `WithTLSHandshakeTimeout`, `WithTransportWrapper`, `WithProxy`, `WithCheckRedirect`, and `WithCookieJar` cover custom transports, middleware, proxies, redirects, and cookies.

## Installation

```bash
go get github.com/hueristiq/hq-lib-http-go@latest
```

Requires Go 1.26 or later.

## Usage

The examples import the package under the `hqgohttp` alias; subpackage examples use the `hqgohttp`-prefixed aliases (`hqgohttpheader`, `hqgohttpmethod`, `hqgohttpmime`, and `hqgohttprequest`):

```go
import hqgohttp "github.com/hueristiq/hq-lib-http-go"
```

### Client

`New` takes functional options; no execution timeout is applied by default — bound the call with a context or `WithTimeout`:

```go
client := hqgohttp.New(
	hqgohttp.WithTimeout(10*time.Second),
)

res, err := client.Get("https://example.com")
if err != nil {
	log.Fatalf("Request failed: %v", err)
}

defer res.Body.Close()

fmt.Println("Response status:", res.Status)
```

`WithProtocol` chooses the HTTP version: `ProtocolAuto` (the default), `ProtocolAutoHTTP3` (HTTP/3 first, fallback to auto), or a forced `ProtocolHTTP1`/`ProtocolHTTP2`/`ProtocolHTTP3`:

```go
client := hqgohttp.New(
	hqgohttp.WithProtocol(hqgohttp.ProtocolAutoHTTP3),
	hqgohttp.WithTLSConfig(&tls.Config{RootCAs: roots}),
)
```

`Close` shuts the client down — it cancels in-flight executions with `ErrClientClosed`, closes idle connections, and makes later requests fail. `CloseIdleConnections` is the non-destructive alternative.

### Fluent Request Builder

`Request` starts a chainable builder; the client executes the built request with its retrier, rate limiter, and callbacks:

```go
res, err := client.Request().
	Method(hqgohttpmethod.POST).
	URL("https://api.example.com/users").
	Param("page", "1").
	SetHeader(hqgohttpheader.ContentType.String(), hqgohttpmime.JSON.String()).
	JSON(map[string]any{"name": "ada"}).
	Do()
if err != nil {
	log.Fatalf("Request failed: %v", err)
}

defer res.Body.Close()
```

`JSON`, `XML`, `Form`, and `Multipart` encode the body when the request is built and set `Content-Type` only when the final request does not already carry one. `Body` accepts raw strings, bytes, and readers; `BodyStream` sends large bodies without buffering them — a streamed body has no replay function, so a retry approved after the first attempt stops with `ErrBodyNotReplayable`.

A context applies per client (`WithContext`) or per request (`RequestBuilder.Context`); an explicit `context.Background()` or `Context(nil)` also overrides the client default.

### Response Handling

`Do` returns `*hqgohttp.Response`, which embeds `*http.Response` and adds an enhanced `Body`:

```go
res, err := client.Get("https://api.example.com/users/ada")
if err != nil {
	log.Fatalf("Request failed: %v", err)
}

defer res.Body.Close()

if !res.IsSuccess() {
	log.Fatalf("Unexpected status: %s", res.Status)
}

var user User

if err := res.Body.JSON(&user); err != nil {
	log.Fatalf("Decoding the response failed: %v", err)
}
```

`Bytes`, `String`, `JSON`, `XML`, and `UTF8` read and close the body on first use, then cache it. `Stream` returns a buffered reader for streaming, and `SSE` dispatches Server-Sent Events. `IsInformational`, `IsSuccess`, `IsRedirection`, `IsClientError`, `IsServerError`, and `IsError` classify the status. `WithMaxResponseBodySize` caps what the buffering helpers retain; the cap returns `ErrResponseBodyTooLarge` with the prefix read so far.

### Retries

The retry policy decides, after each attempt, whether the outcome is worth retrying; the retrier decides how many attempts to make and how long to wait. `WithOnRetry` observes every retry, `WithPrepareRetry` re-signs before the next attempt, and `WithAttemptTimeout` bounds one attempt through the headers:

```go
client := hqgohttp.New(
	hqgohttp.WithRetrier(retrier),
	hqgohttp.WithOnRetry(func(event hqgohttp.RetryEvent) {
		log.Printf("attempt %d failed after %s: %v; retrying in %s",
			event.Attempt, event.Duration, event.Err, event.Wait)
	}),
	hqgohttp.WithPrepareRetry(func(_ context.Context, req *http.Request, _ int) error {
		req.Header.Set("Authorization", "Bearer "+token()) // re-sign after the wait

		return nil
	}),
)
```

Retries are idempotent-only by default: GET, HEAD, OPTIONS, TRACE, PUT, DELETE, and QUERY are retried; POST and PATCH are not. Pass `WithRetryIdempotent(false)` to retry every method.

When retries run out, `Do` returns a `*RetryError` carrying the attempt count, the per-attempt history, the last status and headers, and the last failure. A bounded copy of the final response body is included only when capture is enabled with `WithRetryErrorBodyLimit`:

```go
res, err := client.Get("https://api.example.com/users",
	hqgohttp.WithRequestRetryErrorBodyLimit(hqgohttp.RecommendedRetryErrorBodyLimit),
)
if err != nil {
	var retryErr *hqgohttp.RetryError

	if errors.As(err, &retryErr) {
		log.Printf("gave up after %d attempts; status=%d", retryErr.Attempts, retryErr.StatusCode)
	}

	log.Fatalf("Request failed: %v", err)
}

defer res.Body.Close()
```

`errors.Is(err, hqgohttp.ErrRetriesExhausted)` classifies an exhaustion without inspecting the message. `WithOnRetryExhaustion` can hand the final response — body open — to the caller instead of the error, so a `5xx` that is data rather than a failure stays readable.

### Rate Limiting

`WithRateLimiter` accepts any value with a `Wait(ctx) error` method; a `*limiter.Limiter` from [hq-lib-limiter-go](https://github.com/hueristiq/hq-lib-limiter-go) is the suggested implementation:

```go
limiter := hqgolimiter.New(
	hqgolimiter.WithRate(5, time.Second),
	hqgolimiter.WithBurst(2),
)

client := hqgohttp.New(
	hqgohttp.WithRateLimiter(limiter),
)
```

The limiter waits once before every retry-engine attempt, not once per request. `RateLimiterFunc` adapts a plain function when the method set does not match the interface.

### URL Parser Hook

`WithURLParser` accepts any value with a `Parse(raw string) (*url.URL, error)` method. The default is `net/url.Parse`, which is strict: no default scheme, no escape repair. An adapter around [hq-lib-url-go](https://github.com/hueristiq/hq-lib-url-go) restores the scheme-defaulting and lenient behavior:

```go
urlParser := hqgourlparser.New(
	hqgourlparser.WithDefaultScheme("http"),
	hqgourlparser.WithLenient(),
)

client := hqgohttp.New(
	hqgohttp.WithURLParser(hqgohttprequest.URLParserFunc(func(raw string) (*url.URL, error) {
		parsed, err := urlParser.Parse(raw)
		if err != nil {
			return nil, err
		}

		return parsed.URL, nil
	})),
)

res, err := client.Get("example.com/users") // defaulted to http://…
```

`WithRequestURLParser` overrides it per request. The packages do not import each other; the dependency belongs to the application.

### Retrier Hook

`WithRetrier` accepts any value with a `Retry` method. The engine owns the attempt count, waits, and backoff; the client supplies the operation and the `RetryOn`, `OnRetry`, and `RetryAfter` hooks. An adapter around [hq-lib-retrier-go](https://github.com/hueristiq/hq-lib-retrier-go) looks like this:

```go
retrier := hqgohttp.RetrierFunc(func(ctx context.Context, operation hqgohttp.RetryOperation, options hqgohttp.RetrierOptions) (*http.Response, error) {
	return hqgoretrier.RetryWithData(ctx, operation,
		hqgoretrier.WithMaxAttempts(4),
		hqgoretrier.WithWaitMin(500*time.Millisecond),
		hqgoretrier.WithWaitMax(10*time.Second),
		hqgoretrier.WithBackoff(func(minDelay, maxDelay time.Duration) hqgoretrierbackoff.Backoff {
			base := hqgoretrierbackoff.ExponentialWithDecorrelatedJitter(minDelay, maxDelay)

			return func(attempt int) time.Duration {
				if wait, ok := options.RetryAfter(); ok {
					return min(wait, maxDelay) // honor Retry-After, capped
				}

				return base(attempt)
			}
		}),
		hqgoretrier.WithRetryOn(options.RetryOn),
		hqgoretrier.WithOnRetry(func(event hqgoretrier.RetryEvent) {
			options.OnRetry(hqgohttp.RetrierEvent{Attempt: event.Attempt, Err: event.Err, Wait: event.Wait})
		}),
	)
})

client := hqgohttp.New(hqgohttp.WithRetrier(retrier))
```

With no retrier, every execution makes a single attempt, and a persistent `429` or `5xx` still returns a `*RetryError`. The package does not import hq-lib-retrier-go; the dependency belongs to the application.

### Standard Client Adapter

`Std()` returns an `*http.Client` whose transport runs the same execution path, and `RoundTrip` implements `http.RoundTripper` directly:

```go
std := hqgohttp.New(hqgohttp.WithRetrier(retrier)).Std()

res, err := std.Do(req) // retries, then returns the final response
```

The returned client stops at the first response (`http.ErrUseLastResponse`), so redirects remain governed by the hq client. The client also satisfies `http.RoundTripper`, which lets `oauth2.Transport` and similar wrappers use it directly.

### Transport and TLS

`WithTransport` replaces the library-built transport; `WithTransportWrapper` decorates it and keeps protocol selection, TLS, and proxy settings. `WithDialContext` (or `WithDialer`) sets the dial function for built HTTP/1.x and HTTP/2 transports, `WithTLSHandshakeTimeout` bounds the handshake, and `WithTransportBuilder` customizes the built `*http.Transport`. HTTP/3 uses its own transport — `WithHTTP3TransportBuilder` customizes it:

```go
client := hqgohttp.New(
	hqgohttp.WithProtocol(hqgohttp.ProtocolAutoHTTP3),
	hqgohttp.WithHTTP3FallbackTTL(time.Minute),
	hqgohttp.WithHTTP3TransportBuilder(func(transport *http3.Transport) *http3.Transport {
		transport.MaxResponseHeaderBytes = 64 << 10

		return transport
	}),
)
```

`WithCheckRedirect` and `WithCookieJar` set the redirect policy and cookie jar; both also apply to a cloned `WithHTTPClient` client.

## Contributing

Contributions are welcome and encouraged! Feel free to submit [Pull Requests](https://github.com/hueristiq/hq-lib-http-go/pulls) or report [Issues](https://github.com/hueristiq/hq-lib-http-go/issues). For more details, check out the [contribution guidelines](https://github.com/hueristiq/hq-lib-http-go/blob/main/CONTRIBUTING.md).

A big thank you to all the [contributors](https://github.com/hueristiq/hq-lib-http-go/graphs/contributors) for their ongoing support!

![contributors](https://contrib.rocks/image?repo=hueristiq/hq-lib-http-go&max=500)

## Licensing

This package is licensed under the [MIT license](https://opensource.org/license/mit). You are free to use, modify, and distribute it, as long as you follow the terms of the license. You can find the full license text in the repository - [Full MIT license text](https://github.com/hueristiq/hq-lib-http-go/blob/main/LICENSE).
