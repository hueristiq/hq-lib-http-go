# hq-lib-http-go

![made with go](https://img.shields.io/badge/made%20with-Go-1E90FF.svg) [![go reference](https://pkg.go.dev/badge/github.com/hueristiq/hq-lib-http-go.svg)](https://pkg.go.dev/github.com/hueristiq/hq-lib-http-go) [![license](https://img.shields.io/badge/license-MIT-gray.svg?color=1E90FF)](https://github.com/hueristiq/hq-lib-http-go/blob/main/LICENSE) ![maintenance](https://img.shields.io/badge/maintained%3F-yes-1E90FF.svg) [![open issues](https://img.shields.io/github/issues-raw/hueristiq/hq-lib-http-go.svg?style=flat&color=1E90FF)](https://github.com/hueristiq/hq-lib-http-go/issues?q=is:issue+is:open) [![closed issues](https://img.shields.io/github/issues-closed-raw/hueristiq/hq-lib-http-go.svg?style=flat&color=1E90FF)](https://github.com/hueristiq/hq-lib-http-go/issues?q=is:issue+is:closed)

`hq-lib-http-go` is a [Go](https://golang.org/) package for resilient, configurable HTTP communication.

## Resources

- [Features](#features)
- [Installation](#installation)
- [Usage](#usage)
	- [Client](#client)
	- [Fluent request builder](#fluent-request-builder)
	- [Response handling](#response-handling)
	- [Retries](#retries)
	- [Rate limiting](#rate-limiting)
	- [URL parser hook](#url-parser-hook)
	- [Logging](#logging)
	- [Standard client adapter](#standard-client-adapter)
	- [Transport and TLS](#transport-and-tls)
- [Contributing](#contributing)
- [Licensing](#licensing)

## Features

- **HTTP/1.1, HTTP/2, and HTTP/3:** `WithProtocol` selects auto negotiation, HTTP/3-first fallback, or a forced version; HTTP/3 uses an application-supplied client.
- **Fluent request builder:** `Request` chains `Method`, `URL`, `Param`, headers, `JSON`, `XML`, `Form`, `Multipart`, `Body`, `Context`, and `Do`.
- **Response helpers:** `Response.Body` exposes `Bytes`, `String`, `JSON`, `XML`, `UTF8`, `Stream`, `SSE`, and status classifiers.
- **Retries:** idempotent requests retry connection errors, `408`, `429`, and `5xx` through hq-lib-retrier-go, with typed `RetryError` results and exhaustion hooks.
- **Hooks:** `WithLogger`, `WithRateLimiter`, and `WithURLParser` integrate logging, rate limiting, and URL parsing without core dependencies.
- **Transport control:** `WithTransport`, `WithTransportBuilder`, `WithTransportWrapper`, `WithTLSHandshakeTimeout`, `WithDialer`, `WithProxy`, `WithCheckRedirect`, and `WithCookieJar` cover custom transports and policies.
- **Lifecycle:** `Close` cancels in-flight requests, closes idle connections, and rejects later executions with `ErrClientClosed`.

## Installation

To install `hq-lib-http-go`, run the following command in your Go project:

```bash
go get github.com/hueristiq/hq-lib-http-go@latest
```

Requires Go 1.26.3 or later.

## Usage

The examples import the root package under the `hqgohttp` alias. Subpackage examples use the `hqgohttp`-prefixed aliases `hqgohttpheader`, `hqgohttpmethod`, `hqgohttpmime`, and `hqgohttprequest`.

### Client

`New` takes functional options. Every execution is bounded by a 30-second default timeout, and retryable idempotent failures are attempted three times with exponential backoff. Override either default with `WithTimeout` and `WithRetryMaxAttempts`.

```go
package main

import (
	"fmt"
	"log"
	"time"

	hqgohttp "github.com/hueristiq/hq-lib-http-go"
)

func main() {
	client := hqgohttp.New(
		hqgohttp.WithTimeout(10*time.Second),
		hqgohttp.WithRetryMaxAttempts(3),
	)
	defer client.Close()

	res, err := client.Get("https://example.com")
	if err != nil {
		log.Fatal(err)
	}
	defer res.Body.Close()

	body, err := res.Body.String()
	if err != nil {
		log.Fatal(err)
	}

	fmt.Printf("%s: %d bytes\n", res.Status, len(body))
}
```

`WithProtocol` chooses the HTTP version: `ProtocolAuto` (the default), `ProtocolAutoHTTP3` (HTTP/3 first, fallback to auto), or a forced `ProtocolHTTP1`/`ProtocolHTTP2`/`ProtocolHTTP3`. HTTP/3 requires an application-supplied client; `ProtocolHTTP3` without one returns `ErrHTTP3ClientRequired`, and `ProtocolAutoHTTP3` without one behaves as `ProtocolAuto`.

```go
// The application builds the HTTP/3 client; quic-go is only an example.
h3Client := &http.Client{Transport: &http3.Transport{
	TLSClientConfig: &tls.Config{RootCAs: roots},
}}

client := hqgohttp.New(
	hqgohttp.WithProtocol(hqgohttp.ProtocolAutoHTTP3),
	hqgohttp.WithHTTP3Client(h3Client),
	hqgohttp.WithHTTP3FallbackTTL(time.Minute),
)
```

`Close` shuts the client down. It cancels in-flight executions with `ErrClientClosed`, closes idle connections, and makes later requests fail. `CloseIdleConnections` is the non-destructive alternative.

### Fluent request builder

`Request` starts a chainable builder. The client executes the built request with its retry engine, rate limiter, and callbacks.

```go
package main

import (
	"log"

	hqgohttp "github.com/hueristiq/hq-lib-http-go"
	hqgohttpheader "github.com/hueristiq/hq-lib-http-go/header"
	hqgohttpmethod "github.com/hueristiq/hq-lib-http-go/method"
	hqgohttpmime "github.com/hueristiq/hq-lib-http-go/mime"
)

func main() {
	client := hqgohttp.New()
	defer client.Close()

	res, err := client.Request().
		Method(hqgohttpmethod.POST).
		URL("https://api.example.com/users").
		Param("page", "1").
		SetHeader(hqgohttpheader.ContentType.String(), hqgohttpmime.JSON.String()).
		JSON(map[string]any{"name": "ada"}).
		Do()
	if err != nil {
		log.Fatal(err)
	}
	defer res.Body.Close()
}
```

`JSON`, `XML`, `Form`, and `Multipart` encode the body when the request is built and set `Content-Type` only when the final request does not already carry one. `Body` accepts raw strings, bytes, and readers. `BodyStream` sends large bodies without buffering them; a streamed body has no replay function, so a retry approved after the first attempt stops with `ErrBodyNotReplayable`.

A context applies per client (`WithContext`) or per request (`RequestBuilder.Context`). An explicit `context.Background()` or `Context(nil)` also overrides the client default.

### Response handling

`Do` returns `*hqgohttp.Response`, which embeds `*http.Response` and adds an enhanced `Body`.

```go
res, err := client.Get("https://api.example.com/users/ada")
if err != nil {
	log.Fatal(err)
}
defer res.Body.Close()

if !res.IsSuccess() {
	log.Fatalf("unexpected status: %s", res.Status)
}

var user User
if err := res.Body.JSON(&user); err != nil {
	log.Fatal(err)
}
```

`Bytes`, `String`, `JSON`, `XML`, and `UTF8` read and close the body on first use, then cache it. `Stream` returns a buffered reader for streaming, and `SSE` dispatches Server-Sent Events. `IsInformational`, `IsSuccess`, `IsRedirection`, `IsClientError`, `IsServerError`, and `IsError` classify the status. `WithMaxResponseBodySize` caps what the buffering helpers retain; the cap returns `ErrResponseBodyTooLarge` with the prefix read so far.

### Retries

The retry policy decides, after each attempt, whether the outcome is worth retrying. `WithRetryMaxAttempts` decides how many attempts to make, and the engine uses exponential backoff with jitter. `WithOnRetry` observes every retry, `WithPrepareRetry` re-signs before the next attempt, and `WithAttemptTimeout` bounds one attempt through the headers.

```go
client := hqgohttp.New(
	hqgohttp.WithRetryMaxAttempts(4),
	hqgohttp.WithOnRetry(func(event hqgohttp.RetryEvent) {
		log.Printf("attempt %d failed after %s: %v; retrying in %s",
			event.Attempt, event.Duration, event.Err, event.Wait)
	}),
	hqgohttp.WithPrepareRetry(func(_ context.Context, req *http.Request, _ int) error {
		req.Header.Set("Authorization", "Bearer "+token())

		return nil
	}),
)
```

Retries are idempotent-only by default: GET, HEAD, OPTIONS, TRACE, PUT, DELETE, and QUERY are retried; POST and PATCH are not. Pass `WithRetryIdempotent(false)` to retry every method.

When retries run out, `Do` returns a `*RetryError` carrying the attempt count, the per-attempt history, the last status and headers, and the last failure. A bounded copy of the final response body is included only when capture is enabled with `WithRetryErrorBodyLimit`.

```go
res, err := client.Get("https://api.example.com/users",
	hqgohttp.WithRequestRetryErrorBodyLimit(hqgohttp.RecommendedRetryErrorBodyLimit),
)
if err != nil {
	var retryErr *hqgohttp.RetryError

	if errors.As(err, &retryErr) {
		log.Printf("gave up after %d attempts; status=%d", retryErr.Attempts, retryErr.StatusCode)
	}

	log.Fatal(err)
}
defer res.Body.Close()
```

`errors.Is(err, hqgohttp.ErrRetriesExhausted)` classifies an exhaustion without inspecting the message. `WithOnRetryExhaustion` can hand the final response, with its body open, to the caller instead of the error, so a `5xx` that is data rather than a failure stays readable.

### Rate limiting

`WithRateLimiter` accepts any value with a `Wait(ctx) error` method. A `*limiter.Limiter` from [hq-lib-limiter-go](https://github.com/hueristiq/hq-lib-limiter-go) is the suggested implementation.

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

### URL parser hook

`WithURLParser` accepts any value with a `Parse(raw string) (*url.URL, error)` method. The default is `net/url.Parse`, which is strict: no default scheme and no escape repair. An adapter around [hq-lib-url-go](https://github.com/hueristiq/hq-lib-url-go) restores the scheme-defaulting and lenient behavior.

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

res, err := client.Get("example.com/users")
```

`WithRequestURLParser` overrides it per request. The packages do not import each other; the dependency belongs to the application.

### Logging

`WithLogger` accepts any value with slog-compatible `Debug`, `Info`, `Warn`, and `Error` methods. A `*slog.Logger` works directly, and [hq-lib-logger-go](https://github.com/hueristiq/hq-lib-logger-go) plugs in through its `SlogHandler`. `WithRequestLogger` overrides the logger for one execution.

```go
logger := slog.New(slog.NewTextHandler(os.Stderr, nil))
client := hqgohttp.New(hqgohttp.WithLogger(logger))

res, err := client.Get("https://api.example.com/users",
	hqgohttp.WithRequestLogger(slog.New(slog.NewJSONHandler(os.Stderr, nil))),
)
```

The client logs `http request` and `http response` at debug, approved retries at warn, and terminal failures at error. Method, redacted URL, attempt, status, duration, and error are included. Headers, bodies, and cookies are not. A nil logger disables logging.

### Standard client adapter

`Std()` returns an `*http.Client` whose transport runs the same execution path, and `RoundTrip` implements `http.RoundTripper` directly.

```go
std := hqgohttp.New(hqgohttp.WithRetryMaxAttempts(4)).Std()

res, err := std.Do(req)
```

The returned client stops at the first response (`http.ErrUseLastResponse`), so redirects remain governed by the hq client. The client also satisfies `http.RoundTripper`, which lets `oauth2.Transport` and similar wrappers use it directly.

### Transport and TLS

`WithTransport` replaces the library-built transport. `WithTransportWrapper` decorates it and keeps protocol selection, TLS, and proxy settings. `WithDialContext` (or `WithDialer`) sets the dial function for built HTTP/1.x and HTTP/2 transports, `WithTLSHandshakeTimeout` bounds the handshake, and `WithTransportBuilder` customizes the built `*http.Transport`. The library does not build HTTP/3 clients: construct the client with your HTTP/3 implementation and pass it whole with `WithHTTP3Client`. The application owns its transport, dial function, QUIC settings, and TLS configuration.

`WithCheckRedirect` and `WithCookieJar` set the redirect policy and cookie jar. Both also apply to shallow copies of a `WithHTTPClient` or `WithHTTP3Client` client, leaving the caller's client unmodified.

## Contributing

Contributions are welcome and encouraged! Feel free to submit [Pull Requests](https://github.com/hueristiq/hq-lib-http-go/pulls) or report [Issues](https://github.com/hueristiq/hq-lib-http-go/issues).

A big thank you to all the [contributors](https://github.com/hueristiq/hq-lib-http-go/graphs/contributors) for your ongoing support!

![contributors](https://contrib.rocks/image?repo=hueristiq/hq-lib-http-go&max=500)

## Licensing

This package is licensed under the [MIT license](https://opensource.org/license/mit). You are free to use, modify, and distribute it, as long as you follow the terms of the license. You can find the full license text in the repository - [Full MIT license text](https://github.com/hueristiq/hq-lib-http-go/blob/main/LICENSE).
